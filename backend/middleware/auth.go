package middleware

import (
	"log"
	"net/http"
	"os"
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
		log.Printf("[Auth] ===== AUTH MIDDLEWARE START =====")
		log.Printf("[Auth] Method: %s, Path: %s", c.Request.Method, c.Request.URL.Path)
		log.Printf("[Auth] Request ID: %p", c.Request)

		log.Printf("[Auth] ========== NEW REQUEST ==========")
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
		log.Printf("[Auth] About to c.Set userID...")

		c.Set("userID", decodedToken.UID)

		log.Printf("[Auth] c.Set('userID', '%s') completed", decodedToken.UID)

		// Fetch user data to get organization ID
		log.Printf("[Auth] Fetching user from Firestore for UID: %s", decodedToken.UID)
		user, err := userService.GetUser(c.Request.Context(), decodedToken.UID)
		if err != nil {
			log.Printf("[Auth] User not found in Firestore: %v. Attempting to create user record...", err)

			// Get Firebase Auth user details to create Firestore record
			authUser, authErr := userService.GetAuthUser(c.Request.Context(), decodedToken.UID)
			if authErr != nil {
				log.Printf("[Auth] Failed to fetch Firebase Auth user: %v", authErr)
				c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
				c.Abort()
				return
			}

			// Create user record in Firestore
			user, err = userService.CreateUserRecord(c.Request.Context(), authUser.UID, authUser.Email, authUser.DisplayName)
			if err != nil {
				log.Printf("[Auth] Failed to create user record: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user record"})
				c.Abort()
				return
			}
			log.Printf("[Auth] User record created for UID: %s", authUser.UID)
		}

		log.Printf("[Auth] User fetched - UID: %s, Email: %s, OrganizationID: '%s'", user.ID, user.Email, user.OrganizationID)
		log.Printf("[Auth] OrganizationID length: %d, is empty: %v, equals 'default': %v",
			len(user.OrganizationID),
			user.OrganizationID == "",
			user.OrganizationID == "default")

		// If the Firestore user has no organization set, allow mapping agent UIDs to orgs
		if user.OrganizationID == "" || user.OrganizationID == "default" {
			// Map format: AGENT_UID_ORG_MAP="uid1:org1,uid2:org2"
			mapEnv := os.Getenv("AGENT_UID_ORG_MAP")
			mapped := ""
			if mapEnv != "" {
				for _, pair := range strings.Split(mapEnv, ",") {
					kv := strings.SplitN(strings.TrimSpace(pair), ":", 2)
					if len(kv) == 2 && kv[0] == decodedToken.UID {
						mapped = kv[1]
						break
					}
				}
			}
			// Fallback: if AGENT_ID_UID and AGENT_DEFAULT_ORG are set and UID matches, use it
			// Note: AGENT_DEFAULT_ORG fallback removed — prefer explicit AGENT_UID_ORG_MAP or
			// Firestore user records to grant organization membership to non-interactive agents.

			if mapped != "" {
				user.OrganizationID = mapped
				log.Printf("[Auth] Mapped agent UID %s to organizationID: %s via env", decodedToken.UID, mapped)
			}
		}

		// Set organization ID in context if user belongs to an organization
		if user.OrganizationID != "" && user.OrganizationID != "default" {
			log.Printf("[Auth] Setting organizationID in context: '%s'", user.OrganizationID)
			c.Set("organizationID", user.OrganizationID)
			log.Printf("[Auth] c.Set('organizationID', '%s') completed", user.OrganizationID)
			log.Printf("[Auth] Context values set - userID: %s, organizationID: %s", decodedToken.UID, user.OrganizationID)
		} else {
			log.Printf("[Auth] NOT setting organizationID (value: '%s')", user.OrganizationID)
			log.Printf("[Auth] Context values set - userID: %s (no organization)", decodedToken.UID)
		}

		log.Printf("[Auth] Verifying context after c.Set...")
		testUserID, existsUser := c.Get("userID")
		testOrgID, existsOrg := c.Get("organizationID")
		log.Printf("[Auth] Verification after c.Set - userID exists: %v (value: %v, type: %T), organizationID exists: %v (value: %v, type: %T)",
			existsUser, testUserID, testUserID, existsOrg, testOrgID, testOrgID)

		log.Printf("[Auth] All context keys before c.Next():")
		for key := range c.Keys {
			val, _ := c.Get(key)
			log.Printf("[Auth]   - Key: %s, Value: %v, Type: %T", key, val, val)
		}

		log.Printf("[Auth] Calling c.Next()...")

		c.Next()

		log.Printf("[Auth] After c.Next() - Response Status: %d", c.Writer.Status())
		log.Printf("[Auth] ===== AUTH MIDDLEWARE END =====")

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
