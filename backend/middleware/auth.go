package middleware

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/services"
)

type contextKey string

const (
	UserIDKey  contextKey = "user_id"
	UserOrgKey contextKey = "user_org"
)

func AuthMiddleware(userService *services.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			log.Println("[Auth] Missing authorization header")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			log.Println("[Auth] Invalid authorization format")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		log.Printf("[Auth] Verifying token (first 20 chars): %s...", token[:min(20, len(token))])
		decodedToken, err := userService.VerifyToken(c.Request.Context(), token)
		if err != nil {
			log.Printf("[Auth] Token verification failed: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		log.Printf("[Auth] Token verified successfully for UID: %s", decodedToken.UID)
		// Store Firebase UID - handlers can check if user exists in Firestore
		ctx := context.WithValue(c.Request.Context(), UserIDKey, decodedToken.UID)
		c.Request = c.Request.WithContext(ctx)
		c.Set("user_id", decodedToken.UID)

		c.Next()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func GetUserID(c *gin.Context) string {
	userID, exists := c.Get("user_id")
	if !exists || userID == nil {
		return ""
	}
	return userID.(string)
}

func GetOrgID(c *gin.Context) string {
	orgID, exists := c.Get("org_id")
	if !exists || orgID == nil {
		return ""
	}
	return orgID.(string)
}
