package middleware

import (
	"context"
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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			c.Abort()
			return
		}

		decodedToken, err := userService.VerifyToken(c.Request.Context(), token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		user, err := userService.GetUser(c.Request.Context(), decodedToken.UID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			c.Abort()
			return
		}

		ctx := context.WithValue(c.Request.Context(), UserIDKey, user.ID)
		ctx = context.WithValue(ctx, UserOrgKey, user.OrganizationID)
		c.Request = c.Request.WithContext(ctx)

		c.Set("user_id", user.ID)
		c.Set("org_id", user.OrganizationID)

		c.Next()
	}
}

func GetUserID(c *gin.Context) string {
	userID, _ := c.Get("user_id")
	return userID.(string)
}

func GetOrgID(c *gin.Context) string {
	orgID, _ := c.Get("org_id")
	return orgID.(string)
}