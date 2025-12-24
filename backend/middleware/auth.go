package middleware

import (
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/services"
)

type contextKey string

const (
	UserIDKey  contextKey = "userID"
	UserOrgKey contextKey = "organizationID"
)

func AuthMiddleware(userService *services.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		log.Printf("[Auth] Method: %s, Path: %s", c.Request.Method, c.Request.URL.Path)

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

		c.Set("userID", decodedToken.UID)

		log.Printf("[Auth] Context values set - userID: %s", decodedToken.UID)

		testUserID, existsUser := c.Get("userID")
		log.Printf("[Auth] Verification after c.Set - userID exists: %v (value: %v)",
			existsUser, testUserID)

		log.Printf("[Auth] Calling c.Next()...")

		c.Next()

		log.Printf("[Auth] After c.Next() - Response Status: %d", c.Writer.Status())
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func GetUserID(c *gin.Context) string {
	userID, exists := c.Get("userID")
	if !exists || userID == nil {
		return ""
	}
	return userID.(string)
}

func GetOrgID(c *gin.Context) string {
	orgID, exists := c.Get("organizationID")
	if !exists || orgID == nil {
		return ""
	}
	return orgID.(string)
}
