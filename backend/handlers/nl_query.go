package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/services"
)

// NLQueryRequest represents the incoming chat message
type NLQueryRequest struct {
	Message    string `json:"message" binding:"required"`
	IncidentId string `json:"incident_id"`
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

	// TODO: Process the message with your NL query logic
	// For now, echo back the message
	response := NLQueryResponse{
		Response: "Intent detected: " + intent + ", Original message: " + req.Message + ", Agents: " + strings.Join(agents, ", "),
		Success:  true,
	}

	c.JSON(http.StatusOK, response)
}
