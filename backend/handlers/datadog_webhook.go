package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/models"
	"github.com/ryanjewik/incident_commander/backend/services"

	"github.com/ryanjewik/incident_commander/backend/middleware"

	//confluent test
	"bufio"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type DatadogWebhookHandler struct {
	incidentService *services.IncidentService
	firebaseService *services.FirebaseService
	ddService       *services.DatadogService
	chatHandler     *ChatHandler
}

func NewDatadogWebhookHandler(incidentService *services.IncidentService, firebaseService *services.FirebaseService, ddService *services.DatadogService, chatHandler *ChatHandler) *DatadogWebhookHandler {
	return &DatadogWebhookHandler{incidentService: incidentService, firebaseService: firebaseService, ddService: ddService, chatHandler: chatHandler}
}

func ReadConfig() kafka.ConfigMap {
	// reads the client configuration from client.properties
	// and returns it as a key-value map
	m := kafka.ConfigMap{}

	file, err := os.Open("client.properties")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open file: %s", err)
		os.Exit(1)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			m[key] = val
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read client.properties: %v", err)
		os.Exit(1)
	}
	return m
}

// ensureTopicExists attempts to create the topic if it does not exist using the AdminClient.
// This is a best-effort operation and will not fail webhook handling if it errors.
func ensureTopicExists(topic string, config kafka.ConfigMap) error {
	// create admin client
	a, err := kafka.NewAdminClient(&config)
	if err != nil {
		return fmt.Errorf("failed to create admin client: %w", err)
	}
	defer a.Close()

	// Topic spec: set defaults suitable for Confluent Cloud (replication factor 3).
	// These can be overridden via client.properties if you add support for that.
	spec := kafka.TopicSpecification{
		Topic:             topic,
		NumPartitions:     3,
		ReplicationFactor: 3,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := a.CreateTopics(ctx, []kafka.TopicSpecification{spec}, kafka.SetAdminOperationTimeout(30*time.Second))
	if err != nil {
		// Some client libs return a non-nil err for per-topic failures; log and continue
		return fmt.Errorf("create topics call failed: %w", err)
	}
	// inspect results and ignore 'topic already exists' errors
	for _, r := range results {
		if r.Error.Code() != kafka.ErrNoError && r.Error.Code() != kafka.ErrTopicAlreadyExists {
			// return first non-OK/non-already-exists error
			return fmt.Errorf("topic create error for %s: %s", r.Topic, r.Error.String())
		}
	}
	return nil
}

// produce sends a single message to Kafka using the provided config.
// This is a lightweight helper used by handlers that don't have a long-lived
// producer instance available in the handler scope.
func produce(topic string, key []byte, value []byte, config kafka.ConfigMap) {
	p, err := kafka.NewProducer(&config)
	if err != nil {
		log.Printf("[kafka] failed to create producer: %v", err)
		return
	}
	defer p.Close()

	go func() {
		for e := range p.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					log.Printf("[kafka] delivery failed: %v", ev.TopicPartition.Error)
				} else {
					log.Printf("[kafka] delivered message to %v", ev.TopicPartition)
				}
			}
		}
	}()

	msg := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          value,
		Key:            key,
	}

	if err := p.Produce(msg, nil); err != nil {
		log.Printf("[kafka] produce error: %v", err)
	}

	// Flush for up to 5s
	remaining := p.Flush(5000)
	if remaining > 0 {
		log.Printf("[kafka] producer has %d undelivered messages after flush", remaining)
	}
}

func consume(topic string, config kafka.ConfigMap) {
	// sets the consumer group ID and offset
	config["group.id"] = "go-group-1"
	config["auto.offset.reset"] = "earliest"

	// creates a new consumer and subscribes to your topic
	consumer, _ := kafka.NewConsumer(&config)
	consumer.SubscribeTopics([]string{topic}, nil)

	run := true
	for run {
		// consumes messages from the subscribed topic and prints them to the console
		e := consumer.Poll(1000)
		switch ev := e.(type) {
		case *kafka.Message:
			// application-specific processing
			fmt.Printf("Consumed event from topic %s: key = %-10s value = %s\n",
				*ev.TopicPartition.Topic, string(ev.Key), string(ev.Value))
		case kafka.Error:
			fmt.Fprintf(os.Stderr, "%% Error: %v\n", ev)
			run = false
		}
	}

	// closes the consumer connection
	consumer.Close()
}

// HandleDatadogWebhook accepts Datadog alert webhooks, validates a shared secret,
// and creates an incident record in Firestore.
func (h *DatadogWebhookHandler) HandleDatadogWebhook(c *gin.Context) {
	// Accept either a shared webhook secret header OR an Authorization bearer token.
	secretHeader := c.GetHeader("X-Webhook-Secret")

	// Optionally check X-Datadog-Source header
	ddSource := c.GetHeader("X-Datadog-Source")
	if ddSource == "" {
		log.Printf("[webhook] missing X-Datadog-Source header")
	}

	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		log.Printf("[webhook] failed to bind json: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json payload"})
		return
	}

	// Extract useful fields (best-effort)
	title := "Datadog Alert"
	description := ""
	if ev, ok := payload["event"].(map[string]interface{}); ok {
		if t, ok := ev["title"].(string); ok && t != "" {
			title = t
		}
		if m, ok := ev["message"].(string); ok && m != "" {
			description = m
		}
		if d, ok := ev["text_only_message"].(string); ok && description == "" {
			description = d
		}
	}
	if mon, ok := payload["monitor"].(map[string]interface{}); ok {
		if t, ok := mon["alert_title"].(string); ok && t != "" {
			title = t
		}
		if pr, ok := mon["alert_priority"].(string); ok && description == "" {
			description = pr
		}
	}

	// fallbacks
	if title == "" {
		title = "Datadog Alert"
	}
	if description == "" {
		// try stringify short fields
		if s, ok := payload["message"].(string); ok {
			description = s
		}
	}

	// Require explicit organization_id in payload
	orgID := ""
	if v, ok := payload["organization_id"].(string); ok && v != "" {
		orgID = v
	}
	if orgID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "organization_id is required in payload"})
		return
	}

	// If a secret header was provided, validate against the organization's stored secret
	webhookAuth := false
	if secretHeader != "" {
		if h.firebaseService != nil {
			ctx2 := context.Background()
			if doc, err := h.firebaseService.Firestore.Collection("organizations").Doc(orgID).Get(ctx2); err == nil && doc.Exists() {
				d := doc.Data()
				if dsRaw, ok := d["datadog_secrets"]; ok {
					if dsMap, ok2 := dsRaw.(map[string]interface{}); ok2 {
						// support both legacy plaintext (`webhook_secret`) and new hashed (`webhookSecret`) values
						if ws, ok3 := dsMap["webhook_secret"].(string); ok3 && ws != "" {
							if ws == secretHeader || services.CheckSecretHash(secretHeader, ws) {
								webhookAuth = true
							}
						}
						if ws2, ok4 := dsMap["webhookSecret"].(string); ok4 && ws2 != "" {
							if ws2 == secretHeader || services.CheckSecretHash(secretHeader, ws2) {
								webhookAuth = true
							}
						}
					}
				}
			}
		}
		// fallback to global env secret
		if !webhookAuth {
			if envSecret := os.Getenv("DATADOG_WEBHOOK_SECRET"); envSecret != "" && envSecret == secretHeader {
				webhookAuth = true
			}
		}
		if !webhookAuth {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid webhook secret"})
			return
		}
	}

	// If webhookAuth is true we skip token validation. Otherwise validate Authorization: Bearer <token>
	skipAuth := webhookAuth
	if !skipAuth {
		// Dev/test bypass: if X-Debug-Bypass: true and not running in production,
		// skip the OAuth token validation so we can trigger test spikes locally.
		if c.GetHeader("X-Debug-Bypass") == "true" && os.Getenv("ENV") != "production" {
			skipAuth = true
			log.Printf("[webhook] debug bypass enabled (skipping token validation) for org=%s", orgID)
		}
	}

	if !skipAuth {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
			return
		}
		// expect "Bearer <token>"
		var token string
		if n, _ := fmt.Sscanf(authHeader, "Bearer %s", &token); n != 1 || token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid Authorization header"})
			return
		}

		// Lookup token doc in firestore
		if h.firebaseService == nil {
			log.Printf("[webhook] firebase service not available for token lookup")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server misconfiguration"})
			return
		}
		ctx := context.Background()
		tdoc, err := h.firebaseService.Firestore.Collection("oauth_tokens").Doc(token).Get(ctx)
		if err != nil || !tdoc.Exists() {
			log.Printf("[webhook] token not found: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		var tokData map[string]interface{}
		if err := tdoc.DataTo(&tokData); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server error"})
			return
		}
		// check expiry
		if exp, ok := tokData["expires_at"].(time.Time); ok {
			if time.Now().After(exp) {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "token expired"})
				return
			}
		}
		// ensure token belongs to same org as payload
		tokenOrg, _ := tokData["organization"].(string)
		if tokenOrg == "" || tokenOrg != orgID {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "token organization mismatch"})
			return
		}
	}

	// Build Incident with requested mapping
	now := time.Now()
	// event.id may be under payload["event"]["id"]
	var eventID string
	if ev, ok := payload["event"].(map[string]interface{}); ok {
		if idv, ok2 := ev["id"].(string); ok2 && idv != "" {
			eventID = idv
		} else if idv2, ok3 := ev["id"]; ok3 {
			eventID = fmt.Sprintf("%v", idv2)
		}
		// prefer title/message from event
		if t, ok := ev["title"].(string); ok && t != "" {
			title = t
		}
		if m, ok := ev["message"].(string); ok && m != "" {
			description = m
		}
	}

	// extract alert_id; created_by should be monitor.alert_id if available
	var createdBy string
	var alertID string
	if mon, ok := payload["monitor"].(map[string]interface{}); ok {
		if aid, ok2 := mon["alert_id"].(string); ok2 && aid != "" {
			alertID = aid
			createdBy = aid
		} else if aid2, ok3 := mon["alert_id"]; ok3 {
			alertID = fmt.Sprintf("%v", aid2)
			createdBy = alertID
		}
	}

	incident := &models.Incident{
		ID:             eventID,
		OrganizationID: orgID,
		AlertID:        alertID,
		Title:          title,
		Status:         "New",
		Date:           now.Format(time.RFC3339),
		Type:           "Incident Response",
		Description:    description,
		CreatedBy:      createdBy,
		CreatedAt:      now,
		UpdatedAt:      now,
		Metadata:       payload,
	}

	// Persist to Firestore directly
	if h.firebaseService == nil {
		log.Printf("[webhook] firebase service not available for writing incident")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server misconfiguration"})
		return
	}
	ctx := context.Background()
	// If eventID is empty, generate a new doc (auto-id)
	var err error
	if incident.ID == "" {
		_, _, err = h.firebaseService.Firestore.Collection("incidents").Add(ctx, incident)
	} else {
		_, err = h.firebaseService.Firestore.Collection("incidents").Doc(incident.ID).Set(ctx, incident)
	}
	if err != nil {
		log.Printf("[webhook] failed to write incident to firestore: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create incident"})
		return
	}

	// Determine initial severity guess (if present) and set on incident
	severityGuess := ""
	if mon, ok := payload["monitor"].(map[string]interface{}); ok {
		if pr, ok := mon["alert_priority"].(string); ok {
			severityGuess = pr
		}
	}
	// fallback to priority
	if severityGuess == "" {
		if s, _ := payload["priority"].(string); s != "" {
			severityGuess = s
		}
	}
	if severityGuess != "" {
		incident.SeverityGuess = severityGuess
	}

	// Build a lightweight log entry and push to DatadogService cache and websocket subscribers
	logEntry := map[string]interface{}{
		"time":        incident.CreatedAt.Format(time.RFC3339),
		"level":       "INFO",
		"message":     incident.Title,
		"service":     "datadog_webhook",
		"incident_id": incident.ID,
	}
	if h.ddService != nil {
		h.ddService.AppendRecentLog(orgID, logEntry)
	}
	if h.chatHandler != nil {
		// broadcast a simple wrapper so clients can differentiate event types
		h.chatHandler.BroadcastEvent(orgID, map[string]interface{}{"type": "datadog_webhook", "payload": logEntry})
	}

	// publish to kafka/confluent: build minimal incident event (JSON)
	go func(inc *models.Incident, raw map[string]interface{}) {
		// extract a few helpful fields
		severity := ""
		if mon, ok := raw["monitor"].(map[string]interface{}); ok {
			if pr, ok := mon["alert_priority"].(string); ok {
				severity = pr
			}
		}
		if s, _ := raw["priority"].(string); severity == "" && s != "" {
			severity = s
		}

		// no monitor_id included; keep event small

		var tags []string
		if t, ok := raw["tags"].([]interface{}); ok {
			for _, it := range t {
				tags = append(tags, fmt.Sprintf("%v", it))
			}
		}

		// minimal event schema — keep payload small; include raw payload reference
		event := map[string]interface{}{
			"timestamp":       inc.CreatedAt.Format(time.RFC3339),
			"incident_id":     inc.ID,
			"organization_id": inc.OrganizationID,
			"title":           inc.Title,
			"type":            "datadog_webhook",
			"alert_id":        inc.AlertID,
			"tags":            tags,
			// include full webhook payload as the raw reference
			"raw_payload_ref": raw,
		}

		b, err := json.Marshal(event)
		if err != nil {
			log.Printf("[webhook] failed to marshal kafka event: %v", err)
			return
		}
		topic := "incidents_created"
		kafkaconfig := ReadConfig()
		go produce(topic, []byte(inc.OrganizationID), b, kafkaconfig)
	}(incident, payload)

	// Also persist raw payload to local file for manual review
	go func(p map[string]interface{}) {
		b, err := json.Marshal(p)
		if err != nil {
			log.Printf("[webhook] failed to marshal payload for local save: %v", err)
			return
		}
		// ensure backend directory exists
		path := "backend/webhook_payloads.jsonl"
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Printf("[webhook] failed to open local payload file: %v", err)
			return
		}
		defer f.Close()
		if _, err := f.Write(append(b, '\n')); err != nil {
			log.Printf("[webhook] failed to write payload to file: %v", err)
			return
		}
	}(payload)

	// Append a lightweight recent log entry to the Datadog cache and broadcast over websocket
	go func() {
		defer func() { recover() }()
		entry := map[string]interface{}{
			"time":    incident.CreatedAt.Format(time.RFC3339),
			"level":   "INFO",
			"message": incident.Title,
			"service": "datadog_webhook",
		}
		if h.ddService != nil {
			h.ddService.AppendRecentLog(incident.OrganizationID, entry)
		}
		// Broadcast to websocket clients so UI updates in real time
		if h.chatHandler != nil {
			payload := map[string]interface{}{"type": "datadog_webhook", "payload": entry, "incident": incident}
			go func() {
				defer func() { recover() }()
				h.chatHandler.BroadcastEvent(incident.OrganizationID, payload)
			}()
		}
	}()

	c.JSON(http.StatusCreated, incident)
}

// GetOrgSecrets returns decrypted secrets for an organization.
// Protected by a simple bearer token (AGENT_AUTH_TOKEN) to authenticate agents.
func (h *DatadogWebhookHandler) GetOrgSecrets(c *gin.Context) {
	// Allow Firebase-authenticated callers. If caller belongs to the org, allow.
	// Otherwise, allow when the caller UID is explicitly mapped to one or more orgs via
	// AGENT_UID_ORG_MAP (format: "uid1:orgA|orgB,uid2:orgC") or via AGENT_DEFAULT_ORG/AGENT_ID_UID.
	requesterOrg := middleware.GetOrgID(c)
	callerUID := middleware.GetUserID(c)
	orgId := c.Param("orgId")
	if orgId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing orgId"})
		return
	}

	allowed := false
	if requesterOrg != "" && requesterOrg == orgId {
		allowed = true
	}

	// If not allowed yet, consult environment mappings for agent UIDs
	if !allowed {
		// Check AGENT_UID_ORG_MAP env var: comma-separated uid:orgList pairs
		mapEnv := os.Getenv("AGENT_UID_ORG_MAP")
		if mapEnv != "" && callerUID != "" {
			for _, pair := range strings.Split(mapEnv, ",") {
				kv := strings.SplitN(strings.TrimSpace(pair), ":", 2)
				if len(kv) != 2 {
					continue
				}
				uid := kv[0]
				orgList := kv[1]
				if uid != callerUID {
					continue
				}
				// orgList may be separated by | or ; or ,
				var orgs []string
				if strings.Contains(orgList, "|") {
					orgs = strings.Split(orgList, "|")
				} else if strings.Contains(orgList, ";") {
					orgs = strings.Split(orgList, ";")
				} else if strings.Contains(orgList, ",") {
					orgs = strings.Split(orgList, ",")
				} else {
					orgs = []string{orgList}
				}
				for _, o := range orgs {
					if strings.TrimSpace(o) == orgId {
						allowed = true
						break
					}
				}
				if allowed {
					break
				}
			}
		}

		// Allow a globally trusted agent UID (AGENT_ID_UID) to fetch any org's secrets.
		agentUID := os.Getenv("AGENT_ID_UID")
		if agentUID != "" && callerUID == agentUID {
			allowed = true
		}

		// Also allow a list of trusted UIDs via AGENT_TRUSTED_UIDS (comma-separated)
		trustedList := os.Getenv("AGENT_TRUSTED_UIDS")
		if !allowed && trustedList != "" && callerUID != "" {
			for _, t := range strings.Split(trustedList, ",") {
				if strings.TrimSpace(t) == callerUID {
					allowed = true
					break
				}
			}
		}
	}

	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden - caller not authorized for requested organization"})
		return
	}
	if orgId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing orgId"})
		return
	}

	ctx := context.Background()
	// lookup organization document (stored under `organizations` by admin flows)
	doc, err := h.firebaseService.Firestore.Collection("organizations").Doc(orgId).Get(ctx)
	if err != nil || !doc.Exists() {
		c.JSON(http.StatusNotFound, gin.H{"error": "organization not found"})
		return
	}

	data := doc.Data()
	dsRaw, ok := data["datadog_secrets"]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "datadog secrets not configured"})
		return
	}

	dsMap, ok := dsRaw.(map[string]interface{})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "malformed secrets document"})
		return
	}

	encryptedAPI, _ := dsMap["encrypted_apiKey"].(string)
	encryptedApp, _ := dsMap["encrypted_appKey"].(string)
	wrappedDEK, _ := dsMap["encrypted_apiKeyDek"].(string)

	if encryptedAPI == "" && encryptedApp == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no encrypted keys present"})
		return
	}

	apiPlain, appPlain, err := services.DecryptData(encryptedAPI, encryptedApp, wrappedDEK)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decrypt failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"datadog_api_key": apiPlain,
		"datadog_app_key": appPlain,
	})
}
