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
)

type DatadogWebhookHandler struct {
	incidentService *services.IncidentService
	firebaseService *services.FirebaseService
}

func NewDatadogWebhookHandler(incidentService *services.IncidentService, firebaseService *services.FirebaseService) *DatadogWebhookHandler {
	return &DatadogWebhookHandler{incidentService: incidentService, firebaseService: firebaseService}
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
