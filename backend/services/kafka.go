package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

type KafkaService struct {
	producer *kafka.Producer
	consumer *kafka.Consumer
}

type IncidentAnalysisRequest struct {
	QueryID      string                 `json:"query_id"`
	Message      string                 `json:"message"`
	IncidentID   string                 `json:"incident_id"`
	Intent       string                 `json:"intent"`
	Agents       []string               `json:"agents"`
	IncidentData map[string]interface{} `json:"incident_data"`
	Timestamp    time.Time              `json:"timestamp"`
}

type AgentResponse struct {
	QueryID   string                 `json:"query_id"`
	AgentName string                 `json:"agent_name"`
	Response  map[string]interface{} `json:"response"`
	Success   bool                   `json:"success"`
	Error     string                 `json:"error,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

func NewKafkaService(config kafka.ConfigMap) (*KafkaService, error) {
	producer, err := kafka.NewProducer(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to create producer: %w", err)
	}

	consumerConfig := kafka.ConfigMap{}
	for k, v := range config {
		consumerConfig[k] = v
	}
	consumerConfig["group.id"] = "backend-consumer-group"
	consumerConfig["auto.offset.reset"] = "latest"

	consumer, err := kafka.NewConsumer(&consumerConfig)
	if err != nil {
		producer.Close()
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	go func() {
		for e := range producer.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					log.Printf("Delivery failed: %v\n", ev.TopicPartition)
				} else {
					log.Printf("Delivered message to %v\n", ev.TopicPartition)
				}
			}
		}
	}()

	return &KafkaService{
		producer: producer,
		consumer: consumer,
	}, nil
}

func (k *KafkaService) PublishAnalysisRequest(ctx context.Context, topic string, req IncidentAnalysisRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	err = k.producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          data,
		Key:            []byte(req.QueryID),
	}, nil)

	if err != nil {
		return fmt.Errorf("failed to produce message: %w", err)
	}

	return nil
}

// PublishMessage publishes a simple string message to a Kafka topic
func (k *KafkaService) PublishMessage(ctx context.Context, topic string, message string) error {
	err := k.producer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          []byte(message),
	}, nil)

	if err != nil {
		return fmt.Errorf("failed to produce message: %w", err)
	}

	// Wait for message to be delivered
	k.producer.Flush(5000)

	return nil
}

func (k *KafkaService) ReadAgentResponse(ctx context.Context, topic string, queryID string, timeout time.Duration) (*AgentResponse, error) {
	err := k.consumer.Subscribe(topic, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe: %w", err)
	}
	defer k.consumer.Unsubscribe()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		ev := k.consumer.Poll(1000)
		if ev == nil {
			continue
		}

		switch e := ev.(type) {
		case *kafka.Message:
			var response AgentResponse
			if err := json.Unmarshal(e.Value, &response); err != nil {
				log.Printf("Failed to unmarshal response: %v", err)
				continue
			}

			if response.QueryID == queryID {
				return &response, nil
			}

		case kafka.Error:
			log.Printf("Kafka error: %v", e)
			if e.Code() == kafka.ErrAllBrokersDown {
				return nil, fmt.Errorf("all brokers are down")
			}
		}
	}

	return nil, fmt.Errorf("timeout waiting for response")
}

func (k *KafkaService) Close() {
	if k.producer != nil {
		k.producer.Close()
	}
	if k.consumer != nil {
		k.consumer.Close()
	}
}
