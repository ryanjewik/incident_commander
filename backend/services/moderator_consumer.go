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
func StartModeratorConsumer(firebaseService *FirebaseService) error {
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

				ctx := context.Background()
				_, err := firebaseService.Firestore.Collection("incidents").Doc(msg.IncidentID).Update(ctx, updates)
				if err != nil {
					log.Printf("moderator_consumer: failed to update incident %s: %v", msg.IncidentID, err)
					continue
				}

				log.Printf("moderator_consumer: updated incident %s with moderator decision", msg.IncidentID)

			case kafka.Error:
				log.Printf("moderator_consumer kafka error: %v", e)
			default:
				// ignore other events
			}
		}
	}()

	return nil
}
