package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ryanjewik/incident_commander/backend/services"
)

// OAuthHandler issues opaque tokens for Datadog webhook auth (client_credentials)
type OAuthHandler struct {
	firebaseService *services.FirebaseService
}

func NewOAuthHandler(firebaseService *services.FirebaseService) *OAuthHandler {
	return &OAuthHandler{firebaseService: firebaseService}
}

// Token issues a token for client_credentials grant. Datadog will POST here
// with HTTP Basic auth or form body. We expect client_id and client_secret.
func (h *OAuthHandler) Token(c *gin.Context) {
	// Accept form or basic auth
	clientID, clientSecret, ok := c.Request.BasicAuth()
	if !ok {
		clientID = c.PostForm("client_id")
		clientSecret = c.PostForm("client_secret")
	}
	if clientID == "" || clientSecret == "" {
		log.Printf("[oauth] missing client_id or client_secret (basic present=%v)", ok)
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_id and client_secret required"})
		return
	}

	// Find organization by datadog_secrets.client_id (snake_case) first, then try camelCase
	ctx := context.Background()
	// Query organizations where datadog_secrets.client_id == clientID
	iter := h.firebaseService.Firestore.Collection("organizations").Where("datadog_secrets.client_id", "==", clientID).Limit(1).Documents(ctx)
	doc, err := iter.Next()
	matchedField := "datadog_secrets.client_id"
	if err != nil {
		log.Printf("[oauth] primary org lookup by datadog_secrets.client_id=%s returned: %v", clientID, err)
		// Try camelCase field used by older codepaths
		iter2 := h.firebaseService.Firestore.Collection("organizations").Where("datadog_secrets.clientId", "==", clientID).Limit(1).Documents(ctx)
		doc2, err2 := iter2.Next()
		if err2 != nil {
			log.Printf("[oauth] secondary org lookup by datadog_secrets.clientId=%s returned: %v", clientID, err2)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid client credentials"})
			return
		}
		doc = doc2
		matchedField = "datadog_secrets.clientId"
	}
	orgID := doc.Ref.ID
	log.Printf("[oauth] client_id=%s matched org=%s via %s", clientID, orgID, matchedField)

	// Check stored hashed secret
	var dat map[string]interface{}
	if err := doc.DataTo(&dat); err != nil {
		log.Printf("[oauth] failed to read org data for client_id=%s: %v", clientID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server error"})
		return
	}
	ddSecretsRaw, _ := dat["datadog_secrets"].(map[string]interface{})
	// Use the existing webhookSecret as the client secret for OAuth
	storedHash, _ := ddSecretsRaw["webhookSecret"].(string)
	if storedHash == "" {
		log.Printf("[oauth] webhookSecret not configured for client_id=%s org=%s", clientID, orgID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "client credentials not configured (webhookSecret missing)"})
		return
	}
	if !services.CheckSecretHash(clientSecret, storedHash) {
		log.Printf("[oauth] client_secret mismatch for client_id=%s org=%s", clientID, orgID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid client credentials"})
		return
	}

	// Issue opaque token and store in collection oauth_tokens with expiry
	token := uuid.New().String()
	expiresIn := 3600
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	tokenDoc := map[string]interface{}{
		"token":        token,
		"organization": orgID,
		"expires_at":   expiresAt,
		"created_at":   time.Now(),
	}
	// store keyed by token as doc id for quick lookup
	_, err = h.firebaseService.Firestore.Collection("oauth_tokens").Doc(token).Set(ctx, tokenDoc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store token"})
		return
	}

	resp := map[string]interface{}{
		"access_token": token,
		"token_type":   "bearer",
		"expires_in":   expiresIn,
		"scope":        "alerts.webhook",
	}
	c.JSON(http.StatusOK, resp)
}
