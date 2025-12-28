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

	//confluent test
	"bufio"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type DatadogWebhookHandler struct {
	incidentService *services.IncidentService
	firebaseService *services.FirebaseService
}

func NewDatadogWebhookHandler(incidentService *services.IncidentService, firebaseService *services.FirebaseService) *DatadogWebhookHandler {
	return &DatadogWebhookHandler{incidentService: incidentService, firebaseService: firebaseService}
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
		if !strings.HasPrefix(line, "#") && len(line) != 0 {
			kv := strings.Split(line, "=")
			parameter := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			m[parameter] = value
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("Failed to read file: %s", err)
		os.Exit(1)
	}

	return m
}

func produce(topic string, key []byte, value []byte, config kafka.ConfigMap) {
	// creates a new producer instance
	// ensure topic exists (best effort)
	if err := ensureTopicExists(topic, config); err != nil {
		log.Printf("[kafka] ensureTopicExists warning: %v", err)
	}

	p, err := kafka.NewProducer(&config)
	if err != nil {
		fmt.Printf("failed to create kafka producer: %v\n", err)
		return
	}

	// go-routine to handle message delivery reports and possibly other events
	go func() {
		for e := range p.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					fmt.Printf("Failed to deliver message: %v\n", ev.TopicPartition)
				} else {
					fmt.Printf("Produced event to topic %s: key = %-10s\n",
						*ev.TopicPartition.Topic, string(ev.Key))
				}
			}
		}
	}()

	// produce the provided message
	p.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            key,
		Value:          value,
	}, nil)

	// flush and close — check remaining messages to help diagnose delivery issues
	remaining := p.Flush(30 * 1000)
	if remaining > 0 {
		log.Printf("[kafka] producer flush returned %d undelivered messages", remaining)
		// try a short additional flush
		remaining = p.Flush(10 * 1000)
		if remaining > 0 {
			log.Printf("[kafka] producer still has %d undelivered messages after retries", remaining)
		}
	}
	p.Close()
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
	// Validate secret header
	secret := c.GetHeader("X-Webhook-Secret")
	if secret == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing webhook secret header"})
		return
	}

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

	// Dev/test bypass: if X-Debug-Bypass: true and not running in production,
	// skip the OAuth token validation so we can trigger test spikes locally.
	skipAuth := false
	if c.GetHeader("X-Debug-Bypass") == "true" && os.Getenv("ENV") != "production" {
		skipAuth = true
		log.Printf("[webhook] debug bypass enabled (skipping token validation) for org=%s", orgID)
	}

	// If not skipping auth (normal operation), validate Authorization: Bearer <token>
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
	} else {
		log.Printf("[webhook] skipping token validation due to debug bypass for org=%s", orgID)
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

	// created_by should be monitor.alert_id
	var createdBy string
	if mon, ok := payload["monitor"].(map[string]interface{}); ok {
		if aid, ok2 := mon["alert_id"].(string); ok2 && aid != "" {
			createdBy = aid
		} else if aid2, ok3 := mon["alert_id"]; ok3 {
			createdBy = fmt.Sprintf("%v", aid2)
		}
	}

	incident := &models.Incident{
		ID:             eventID,
		OrganizationID: orgID,
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

		var monitorID string
		if mon, ok := raw["monitor"].(map[string]interface{}); ok {
			if idv, ok := mon["id"].(string); ok && idv != "" {
				monitorID = idv
			} else if aid, ok := mon["alert_id"]; ok {
				monitorID = fmt.Sprintf("%v", aid)
			}
		}

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
			"monitor_id":      monitorID,
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

	c.JSON(http.StatusCreated, incident)
}
