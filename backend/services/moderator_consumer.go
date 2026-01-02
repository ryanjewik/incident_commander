package services

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/google/uuid"
	"github.com/ryanjewik/incident_commander/backend/models"
)

type ModeratorDecisionMessage struct {
	Agent          string                 `json:"agent"`
	IncidentID     string                 `json:"incident_id"`
	OrganizationID string                 `json:"organization_id"`
	Timestamp      string                 `json:"timestamp"`
	Status         string                 `json:"status"`
	Result         map[string]interface{} `json:"result"`
}

// StartModeratorConsumer starts a background goroutine that consumes the
// `moderator_decision` Kafka topic and updates matching incident documents
// in Firestore with a moderator timestamp and result.
// StartModeratorConsumer consumes moderator_decision messages, updates incidents,
// writes a chat message to Firestore, and optionally broadcasts the decision via
// the provided broadcaster callback (used to push over WebSocket).
func StartModeratorConsumer(firebaseService *FirebaseService, broadcaster func(orgID string, payload interface{})) error {
	// Load Kafka client properties from CLIENT_PROPERTIES_PATH (or default)
	propsPath := os.Getenv("CLIENT_PROPERTIES_PATH")
	if propsPath == "" {
		propsPath = "backend/client.properties"
	}

	file, err := os.Open(propsPath)
	if err != nil {
		log.Printf("moderator_consumer: could not open client properties %s: %v", propsPath, err)
		return nil
	}
	defer file.Close()

	props := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		props[key] = val
	}
	if err := scanner.Err(); err != nil {
		log.Printf("moderator_consumer: error reading properties: %v", err)
		return nil
	}

	// Allow overriding SASL credentials from env (Confluent secrets)
	if u := os.Getenv("CONFLUENT_API_KEY"); u != "" {
		props["sasl.username"] = u
	}
	if p := os.Getenv("CONFLUENT_SECRET"); p != "" {
		props["sasl.password"] = p
	}

	cfg := kafka.ConfigMap{}
	// copy known properties into kafka.ConfigMap
	for k, v := range props {
		cfg[k] = v
	}
	// ensure basic consumer settings
	cfg["group.id"] = "backend-moderator-consumer"
	if _, ok := cfg["auto.offset.reset"]; !ok {
		cfg["auto.offset.reset"] = "latest"
	}

	consumer, err := kafka.NewConsumer(&cfg)
	if err != nil {
		return err
	}

	topic := "moderator_decision"
	if err := consumer.SubscribeTopics([]string{topic}, nil); err != nil {
		consumer.Close()
		return err
	}

	go func() {
		defer consumer.Close()

		for {
			ev := consumer.Poll(1000)
			if ev == nil {
				continue
			}

			switch e := ev.(type) {
			case *kafka.Message:
				var msg ModeratorDecisionMessage
				if err := json.Unmarshal(e.Value, &msg); err != nil {
					log.Printf("moderator_consumer: failed to unmarshal message: %v", err)
					continue
				}

				// parse timestamp
				var parsedTime time.Time
				if msg.Timestamp != "" {
					t, err := time.Parse(time.RFC3339, msg.Timestamp)
					if err != nil {
						parsedTime = time.Now()
					} else {
						parsedTime = t
					}
				} else {
					parsedTime = time.Now()
				}

				updates := []firestore.Update{
					{Path: "moderator_timestamp", Value: parsedTime},
					{Path: "moderator_result", Value: msg.Result},
					{Path: "updated_at", Value: time.Now()},
				}

				// Append to moderator_history so previous analyses are preserved.
				historyEntry := map[string]interface{}{
					"timestamp": parsedTime,
					"agent":     msg.Agent,
					"result":    msg.Result,
				}
				updates = append(updates, firestore.Update{Path: "moderator_history", Value: firestore.ArrayUnion(historyEntry)})

				// If moderator provided a severity_guess, write it as a top-level field for easier querying
				if msg.Result != nil {
					if sgRaw, ok := msg.Result["severity_guess"]; ok {
						if sgStr, ok2 := sgRaw.(string); ok2 && sgStr != "" {
							updates = append(updates, firestore.Update{Path: "severity_guess", Value: sgStr})
						}
					}
				}

				ctx := context.Background()
				_, err := firebaseService.Firestore.Collection("incidents").Doc(msg.IncidentID).Update(ctx, updates)
				if err != nil {
					log.Printf("moderator_consumer: failed to update incident %s: %v", msg.IncidentID, err)
					continue
				}

				log.Printf("moderator_consumer: updated incident %s with moderator decision", msg.IncidentID)

				// Create a chat message record summarizing the moderator decision
				// Prefer explicit incident_summary or summary fields from the result
				summaryText := "Moderator decision available."
				if msg.Result != nil {
					if s, ok := msg.Result["incident_summary"].(string); ok && s != "" {
						summaryText = s
					} else if s2, ok2 := msg.Result["summary"].(string); ok2 && s2 != "" {
						summaryText = s2
					} else {
						// fallback: marshal a compact JSON summary of top-level keys
						if b, err := json.Marshal(msg.Result); err == nil {
							summaryText = string(b)
						}
					}
				}

				// Build message object
				chatMsg := models.Message{
					ID:             uuid.New().String(),
					OrganizationID: msg.OrganizationID,
					IncidentID:     msg.IncidentID,
					// canonical moderator user id so clients can reliably detect moderator messages
					UserID:             "moderator",
					UserName:           "Moderator Bot",
					Text:               summaryText,
					MentionsBot:        false,
					CreatedAt:          parsedTime,
					ModeratorResult:    msg.Result,
					ModeratorTimestamp: parsedTime.Format(time.RFC3339),
				}

				// Persist chat message including the moderator analysis so clients
				// retrieving messages get the per-message result directly.
				msgCtx := context.Background()
				if _, err := firebaseService.Firestore.Collection("messages").Doc(chatMsg.ID).Set(msgCtx, chatMsg); err != nil {
					log.Printf("moderator_consumer: failed to write chat message for incident %s: %v", msg.IncidentID, err)
				} else {
					log.Printf("moderator_consumer: wrote chat message for incident %s", msg.IncidentID)
					// Broadcast a structured moderator_decision event so frontend
					// can update the incident UI (moderator_result) immediately
					if broadcaster != nil {
						go func() {
							defer func() { recover() }()
							payload := map[string]interface{}{
								"type":                "moderator_decision",
								"incident_id":         msg.IncidentID,
								"organization_id":     msg.OrganizationID,
								"moderator_result":    msg.Result,
								"moderator_timestamp": parsedTime.Format(time.RFC3339),
								"chat_message":        chatMsg,
							}
							broadcaster(msg.OrganizationID, payload)
						}()
					}
				}

			case kafka.Error:
				log.Printf("moderator_consumer kafka error: %v", e)
			default:
				// ignore other events
			}
		}
	}()

	return nil
}

// StartIncidentBusListener listens for `incident-bus` messages and records
// the expected agents for each created incident into Firestore. This allows
// the moderator to read the agents list from the incident document instead
// of relying on an environment variable.
func StartIncidentBusListener(firebaseService *FirebaseService) error {
	// Load Kafka client properties from CLIENT_PROPERTIES_PATH (or default)
	propsPath := os.Getenv("CLIENT_PROPERTIES_PATH")
	if propsPath == "" {
		propsPath = "backend/client.properties"
	}

	file, err := os.Open(propsPath)
	if err != nil {
		log.Printf("incident_bus_listener: could not open client properties %s: %v", propsPath, err)
		return nil
	}
	defer file.Close()

	props := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		props[key] = val
	}
	if err := scanner.Err(); err != nil {
		log.Printf("incident_bus_listener: error reading properties: %v", err)
		return nil
	}

	if u := os.Getenv("CONFLUENT_API_KEY"); u != "" {
		props["sasl.username"] = u
	}
	if p := os.Getenv("CONFLUENT_SECRET"); p != "" {
		props["sasl.password"] = p
	}

	cfg := kafka.ConfigMap{}
	for k, v := range props {
		cfg[k] = v
	}
	cfg["group.id"] = "backend-incidentbus-consumer"
	if _, ok := cfg["auto.offset.reset"]; !ok {
		cfg["auto.offset.reset"] = "earliest"
	}

	consumer, err := kafka.NewConsumer(&cfg)
	if err != nil {
		return err
	}

	topic := "incident-bus"
	if err := consumer.SubscribeTopics([]string{topic}, nil); err != nil {
		consumer.Close()
		return err
	}

	go func() {
		defer consumer.Close()

		for {
			ev := consumer.Poll(1000)
			if ev == nil {
				continue
			}

			switch e := ev.(type) {
			case *kafka.Message:
				var payload map[string]interface{}
				if err := json.Unmarshal(e.Value, &payload); err != nil {
					log.Printf("incident_bus_listener: failed to unmarshal message: %v", err)
					continue
				}

				incidentIDRaw, ok := payload["incident_id"]
				if !ok {
					// nothing to do
					continue
				}
				incidentID, ok := incidentIDRaw.(string)
				if !ok || incidentID == "" {
					continue
				}

				// Extract agents list if present
				var agentsList []string
				if agentsRaw, ok := payload["agents"]; ok {
					if arr, ok2 := agentsRaw.([]interface{}); ok2 {
						for _, a := range arr {
							if s, ok3 := a.(string); ok3 {
								agentsList = append(agentsList, s)
							}
						}
					}
				}

				// Optionally record the source/type
				if tRaw, ok := payload["type"]; ok {
					if tStr, ok2 := tRaw.(string); ok2 && tStr != "" {
						// write type to incident for easier agent branching
						updates := []firestore.Update{{Path: "type", Value: tStr}}
						if len(agentsList) > 0 {
							updates = append(updates, firestore.Update{Path: "expected_agents", Value: agentsList})
						}
						ctx := context.Background()
						if _, err := firebaseService.Firestore.Collection("incidents").Doc(incidentID).Update(ctx, updates); err != nil {
							log.Printf("incident_bus_listener: failed to update incident %s: %v", incidentID, err)
						} else {
							log.Printf("incident_bus_listener: updated incident %s with type=%s and expected_agents=%v", incidentID, tStr, agentsList)
						}
						continue
					}
				}

				if len(agentsList) > 0 {
					ctx := context.Background()
					if _, err := firebaseService.Firestore.Collection("incidents").Doc(incidentID).Update(ctx, []firestore.Update{{Path: "expected_agents", Value: agentsList}}); err != nil {
						log.Printf("incident_bus_listener: failed to update incident %s: %v", incidentID, err)
					} else {
						log.Printf("incident_bus_listener: updated incident %s with expected_agents=%v", incidentID, agentsList)
					}
				}

			case kafka.Error:
				log.Printf("incident_bus_listener kafka error: %v", e)
			default:
				// ignore other events
			}
		}
	}()

	return nil
}
