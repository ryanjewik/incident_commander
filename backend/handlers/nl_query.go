package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ryanjewik/incident_commander/backend/services"
)

// NLQueryRequest represents the incoming chat message
type NLQueryRequest struct {
	Message      string                 `json:"message" binding:"required"`
	IncidentId   string                 `json:"incident_id"`
	IncidentData map[string]interface{} `json:"incident_data"`
}

// NLQueryResponse represents the response to the chat
type NLQueryResponse struct {
	Response string `json:"response"`
	Success  bool   `json:"success"`
}

func (a *App) NLQuery(c *gin.Context) {
	var req NLQueryRequest

	// Bind and validate the JSON request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request format",
			"details": err.Error(),
			"success": false,
		})
		return
	}

	intent, agents := services.IntentRouter(req.Message, req.IncidentId)

	// --- Kafka-backed async analysis path ---
	if a.KafkaService != nil {
		queryID := uuid.New().String()

		analysisReq := services.IncidentAnalysisRequest{
			QueryID:      queryID,
			Message:      req.Message,
			IncidentID:   req.IncidentId,
			Intent:       intent,
			Agents:       agents,
			IncidentData: req.IncidentData,
			Timestamp:    time.Now(),
		}

		ctx := context.Background()

		if err := a.KafkaService.PublishAnalysisRequest(ctx, "incident-analysis-requests", analysisReq); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to publish analysis request",
				"details": err.Error(),
				"success": false,
			})
			return
		}

		agentResponse, err := a.KafkaService.ReadAgentResponse(ctx, "agent-responses", queryID, 30*time.Second)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to get agent response",
				"details": err.Error(),
				"success": false,
			})
			return
		}

		if !agentResponse.Success {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Agent failed to process request",
				"details": agentResponse.Error,
				"success": false,
			})
			return
		}

		responseText := "Analysis completed"
		if summary, ok := agentResponse.Response["summary"].(string); ok {
			responseText = summary
		}

		response := NLQueryResponse{
			Response: responseText,
			Success:  true,
		}

		c.JSON(http.StatusOK, response)
		return
	}

	// TODO: Process the message with your NL query logic
	// For now, echo back the message
	response := NLQueryResponse{
		Response: "Intent detected: " + intent +
			", Original message: " + req.Message +
			", Agents: " + strings.Join(agents, ", "),
		Success: true,
	}

	c.JSON(http.StatusOK, response)
}
