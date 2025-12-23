package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/segmentio/kafka-go"
	"google.golang.org/api/option"
)

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

type RCAAgent struct {
	genaiClient *genai.Client
	writer      *kafka.Writer
}

func NewRCAAgent(apiKey string, kafkaBrokers []string) (*RCAAgent, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create genai client: %w", err)
	}

	writer := &kafka.Writer{
		Addr:         kafka.TCP(kafkaBrokers...),
		Topic:        "agent-responses",
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
	}

	return &RCAAgent{
		genaiClient: client,
		writer:      writer,
	}, nil
}

func (agent *RCAAgent) analyzeIncident(ctx context.Context, req IncidentAnalysisRequest) (map[string]interface{}, error) {
	model := agent.genaiClient.GenerativeModel("gemini-1.5-flash")

	incidentJSON, _ := json.MarshalIndent(req.IncidentData, "", "  ")

	var agentInputsSection string
	if len(req.AgentInputs) > 0 {
		agentInputsJSON, _ := json.MarshalIndent(req.AgentInputs, "", "  ")
		agentInputsSection = fmt.Sprintf(`

ANALYSIS FROM OTHER AGENTS:
%s

Please incorporate the above findings from other specialized agents in your root cause analysis.`, string(agentInputsJSON))
	}

	prompt := fmt.Sprintf(`You are a Root Cause Analysis (RCA) agent for an incident response system.

USER QUERY: %s

INCIDENT DATA:
%s
%s

Provide a comprehensive root cause analysis including:
1. Primary root cause
2. Contributing factors
3. Timeline reconstruction
4. Recommendations for resolution
5. Prevention strategies

Format your response as a structured analysis.`, req.Message, string(incidentJSON), agentInputsSection)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return nil, fmt.Errorf("failed to generate content: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no response generated")
	}

	responseText := fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0])

	return map[string]interface{}{
		"summary":     responseText,
		"agent":       "RCA",
		"query_id":    req.QueryID,
		"incident_id": req.IncidentID,
		"analyzed_at": time.Now(),
	}, nil
}

func (agent *RCAAgent) processRequest(ctx context.Context, req IncidentAnalysisRequest) {
	log.Printf("Processing request: queryID=%s, intent=%s", req.QueryID, req.Intent)

	shouldProcess := false
	for _, agentName := range req.Agents {
		if strings.ToUpper(agentName) == "RCA" || strings.ToUpper(agentName) == "MODERATOR" {
			shouldProcess = true
			break
		}
	}

	if !shouldProcess {
		log.Printf("RCA agent not required for this query, skipping")
		return
	}

	analysis, err := agent.analyzeIncident(ctx, req)

	response := AgentResponse{
		QueryID:   req.QueryID,
		AgentName: "RCA",
		Timestamp: time.Now(),
	}

	if err != nil {
		log.Printf("Analysis failed for queryID=%s: %v", req.QueryID, err)
		response.Success = false
		response.Error = err.Error()
		response.Response = map[string]interface{}{}
	} else {
		log.Printf("Analysis completed for queryID=%s", req.QueryID)
		response.Success = true
		response.Response = analysis
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		log.Printf("Failed to marshal response: %v", err)
		return
	}

	msg := kafka.Message{
		Key:   []byte(req.QueryID),
		Value: responseData,
		Time:  time.Now(),
	}

	if err := agent.writer.WriteMessages(ctx, msg); err != nil {
		log.Printf("Failed to write response to Kafka: %v", err)
		return
	}

	log.Printf("Published response for queryID=%s", req.QueryID)
}

func (agent *RCAAgent) Start(ctx context.Context) error {
	kafkaBrokers := os.Getenv("KAFKA_BROKERS")
	if kafkaBrokers == "" {
		kafkaBrokers = "kafka:29092"
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        strings.Split(kafkaBrokers, ","),
		Topic:          "incident-analysis-requests",
		GroupID:        "rca-agent-group",
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: time.Second,
	})
	defer reader.Close()

	log.Println("RCA Agent started, waiting for messages...")

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down RCA agent...")
			return nil
		default:
			msg, err := reader.FetchMessage(ctx)
			if err != nil {
				if err == context.Canceled {
					return nil
				}
				log.Printf("Error fetching message: %v", err)
				time.Sleep(time.Second)
				continue
			}

			var req IncidentAnalysisRequest
			if err := json.Unmarshal(msg.Value, &req); err != nil {
				log.Printf("Failed to unmarshal request: %v", err)
				reader.CommitMessages(ctx, msg)
				continue
			}

			agent.processRequest(ctx, req)
			reader.CommitMessages(ctx, msg)
		}
	}
}

func (agent *RCAAgent) Close() {
	if agent.writer != nil {
		agent.writer.Close()
	}
	if agent.genaiClient != nil {
		agent.genaiClient.Close()
	}
}

func main() {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("gemini api key not found")
	}

	kafkaBrokers := os.Getenv("KAFKA_BROKERS")
	if kafkaBrokers == "" {
		kafkaBrokers = "kafka:29092"
	}

	agent, err := NewRCAAgent(apiKey, strings.Split(kafkaBrokers, ","))
	if err != nil {
		log.Fatalf("Failed making RCA agent: %v", err)
	}
	defer agent.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("shutdown signal")
		cancel()
	}()

	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Agent failed: %v", err)
	}
}
