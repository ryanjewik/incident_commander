package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

type KafkaService struct {
	brokers []string
	writer  *kafka.Writer
}

type IncidentAnalysisRequest struct {
	QueryID        string                 `json:"query_id"`
	Message        string                 `json:"message"`
	IncidentID     string                 `json:"incident_id"`
	Intent         string                 `json:"intent"`
	Agents         []string               `json:"agents"`
	IncidentData   map[string]interface{} `json:"incident_data"`
	AgentInputs    map[string]interface{} `json:"agent_inputs,omitempty"`
	Timestamp      time.Time              `json:"timestamp"`
	OrganizationID string                 `json:"organization_id,omitempty"`
}

type AgentResponse struct {
	QueryID   string                 `json:"query_id"`
	AgentName string                 `json:"agent_name"`
	Response  map[string]interface{} `json:"response"`
	Success   bool                   `json:"success"`
	Error     string                 `json:"error,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

func NewKafkaService(brokers []string) *KafkaService {
	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		Async:        false,
	}

	return &KafkaService{
		brokers: brokers,
		writer:  writer,
	}
}

func (k *KafkaService) PublishAnalysisRequest(ctx context.Context, topic string, req IncidentAnalysisRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	msg := kafka.Message{
		Topic: topic,
		Key:   []byte(req.QueryID),
		Value: data,
		Time:  time.Now(),
	}

	err = k.writer.WriteMessages(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to write message to kafka: %w", err)
	}

	log.Printf("Published analysis request to topic %s: queryID=%s", topic, req.QueryID)
	return nil
}

func (k *KafkaService) ReadAgentResponse(ctx context.Context, topic string, queryID string, timeout time.Duration) (*AgentResponse, error) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        k.brokers,
		Topic:          topic,
		GroupID:        fmt.Sprintf("backend-consumer-%s", queryID),
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
		StartOffset:    kafka.LastOffset,
	})
	defer reader.Close()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if err == context.DeadlineExceeded {
				return nil, fmt.Errorf("timeout waiting for agent response")
			}
			return nil, fmt.Errorf("failed to fetch message: %w", err)
		}

		var response AgentResponse
		if err := json.Unmarshal(msg.Value, &response); err != nil {
			log.Printf("Failed to unmarshal response: %v", err)
			reader.CommitMessages(ctx, msg)
			continue
		}

		if response.QueryID == queryID {
			reader.CommitMessages(ctx, msg)
			log.Printf("Received agent response: queryID=%s, agent=%s", response.QueryID, response.AgentName)
			return &response, nil
		}

		reader.CommitMessages(ctx, msg)
	}
}

func (k *KafkaService) Close() error {
	if k.writer != nil {
		return k.writer.Close()
	}
	return nil
}
