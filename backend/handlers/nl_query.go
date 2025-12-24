package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ryanjewik/incident_commander/backend/models"
)

// NLQueryRequest represents the incoming chat message
type NLQueryRequest struct {
	Message      string                 `json:"message" binding:"required"`
	IncidentId   string                 `json:"incident_id"`
	IncidentData map[string]interface{} `json:"incident_data"`
}

// NLQueryResponse represents the response to the chat
type NLQueryResponse struct {
	Response   string `json:"response"`
	Success    bool   `json:"success"`
	IncidentID string `json:"incident_id,omitempty"`
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

	// Get user from context (set by auth middleware)
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	// Get user details to get organization_id and user name
	user, err := a.UserService.GetUser(context.Background(), userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user details"})
		return
	}

	if user.OrganizationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User not part of an organization"})
		return
	}

	ctx := context.Background()

	// Create a new incident for this NL query
	now := time.Now()
	incident := &models.Incident{
		ID:             uuid.New().String(),
		OrganizationID: user.OrganizationID,
		Title:          truncateTitle(req.Message),
		Status:         "New",
		Date:           now.Format(time.RFC3339),
		Type:           "NL Query",
		Description:    req.Message,
		CreatedBy:      user.ID,
		CreatedAt:      now,
		UpdatedAt:      now,
		Metadata:       req.IncidentData,
	}

	// Save incident to Firestore
	_, err = a.FirebaseService.Firestore.Collection("incidents").Doc(incident.ID).Set(ctx, incident)
	if err != nil {
		log.Printf("Failed to create incident: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to create incident",
			"details": err.Error(),
			"success": false,
		})
		return
	}

	log.Printf("Created incident %s for NL query from user %s", incident.ID, user.Email)

	// Create the initial message for this incident
	message := models.Message{
		ID:             uuid.New().String(),
		OrganizationID: user.OrganizationID,
		IncidentID:     incident.ID,
		UserID:         user.ID,
		UserName:       user.FirstName + " " + user.LastName,
		Text:           req.Message,
		MentionsBot:    strings.Contains(req.Message, "@assistant"),
		CreatedAt:      now,
	}

	// Save message to Firestore
	_, err = a.FirebaseService.Firestore.Collection("messages").Doc(message.ID).Set(ctx, message)
	if err != nil {
		log.Printf("Failed to save initial message: %v", err)
		// Don't fail the request if message save fails, incident is already created
	}

	// Check if KafkaService is available
	if a.KafkaService == nil {
		log.Printf("Kafka service not initialized, skipping message publish")
	} else {
		// Publish incident to Kafka incident-bus topic
		// Create the message to publish
		incidentMessage := map[string]interface{}{
			"message":       req.Message,
			"incident_id":   incident.ID,
			"incident_data": req.IncidentData,
		}

		messageJSON, err := json.Marshal(incidentMessage)
		if err != nil {
			log.Printf("Failed to marshal incident message: %v", err)
		} else {
			// Publish to incident-bus topic
			err = a.KafkaService.PublishMessage(ctx, "incident-bus", string(messageJSON))
			if err != nil {
				log.Printf("Failed to publish to Kafka: %v", err)
			} else {
				log.Printf("Successfully published incident %s to Kafka topic: incident-bus", incident.ID)
			}
		}
	}

	// Return success response
	response := NLQueryResponse{
		Response:   "Incident created successfully",
		Success:    true,
		IncidentID: incident.ID,
	}

	c.JSON(http.StatusOK, response)
}

// truncateTitle creates a title from the message, truncating if necessary
func truncateTitle(message string) string {
	maxLen := 100
	if len(message) <= maxLen {
		return message
	}
	return message[:maxLen] + "..."
}
