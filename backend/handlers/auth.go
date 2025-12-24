package handlers

import (
	"log"
	"net/http"
	"regexp"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/middleware"
	"github.com/ryanjewik/incident_commander/backend/models"
	"github.com/ryanjewik/incident_commander/backend/services"
)

type AuthHandler struct {
	userService *services.UserService
}

// GetOrgUsersById returns all users for a given organization ID (admin or member view)
func (h *AuthHandler) GetOrgUsersById(c *gin.Context) {
	orgID := c.Param("orgId")
	ctx := c.Request.Context()

	users, err := h.userService.GetUsersByOrg(ctx, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if users == nil {
		users = []*models.User{}
	}
	c.JSON(http.StatusOK, users)
}

// isPasswordValid checks if password meets security requirements
func isPasswordValid(password string) bool {
	if len(password) < 8 || len(password) > 128 {
		return false
	}

	var (
		hasUpper   = false
		hasLower   = false
		hasNumber  = false
		hasSpecial = false
	)

	specialChars := regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>\/?]`)

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		}
	}

	hasSpecial = specialChars.MatchString(password)

	return hasUpper && hasLower && hasNumber && hasSpecial
}

func NewAuthHandler(userService *services.UserService) *AuthHandler {
	handler := AuthHandler{
		userService: userService,
	}
	return &handler
}

type CreateUserRequest struct {
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8,max=128"`
	FirstName string `json:"first_name" binding:"required"`
	LastName  string `json:"last_name" binding:"required"`
}

func (h *AuthHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": err.Error()},
		)
		return
	}

	// Additional password validation
	if !isPasswordValid(req.Password) {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "Password must contain at least one uppercase letter, one lowercase letter, one number, and one special character"},
		)
		return
	}

	ctx := c.Request.Context()
	user, err := h.userService.CreateUser(
		ctx,
		req.Email,
		req.Password,
		req.FirstName,
		req.LastName,
	)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}
	c.JSON(http.StatusCreated, user)
}

func (h *AuthHandler) GetMe(c *gin.Context) {
	userID := middleware.GetUserID(c)
	ctx := c.Request.Context()
	user, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		// User doesn't exist in Firestore, create them
		authUser, err := h.userService.GetAuthUser(ctx, userID)
		if err != nil {
			c.JSON(
				http.StatusInternalServerError,
				gin.H{"error": "failed to get user from auth"},
			)
			return
		}

		// Create Firestore record for existing Firebase Auth user
		displayName := authUser.DisplayName
		if displayName == "" {
			// Use email username as display name if not set
			if authUser.Email != "" {
				for idx := 0; idx < len(authUser.Email); idx++ {
					if authUser.Email[idx] == '@' {
						displayName = authUser.Email[:idx]
						break
					}
				}
			}
			if displayName == "" {
				displayName = "User"
			}
		}

		user, err = h.userService.CreateUserRecord(ctx, userID, authUser.Email, displayName)
		if err != nil {
			c.JSON(
				http.StatusInternalServerError,
				gin.H{"error": "failed to create user record"},
			)
			return
		}
	}

	c.JSON(http.StatusOK, user)
}

type CreateOrgRequest struct {
	Name string `json:"name" binding:"required"`
}

func (h *AuthHandler) CreateOrganization(c *gin.Context) {
	var req CreateOrgRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": err.Error()},
		)
		return
	}
	// Prevent naming organization "default"
	if req.Name == "default" || req.Name == "Default" || req.Name == "DEFAULT" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "organization name 'default' is reserved"},
		)
		return
	}
	userID := middleware.GetUserID(c)

	ctx := c.Request.Context()
	org, err := h.userService.CreateOrganization(ctx, req.Name, userID)
	if err != nil {
		log.Printf("[CreateOrganization] Error creating organization: %v", err)
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}
	c.JSON(http.StatusCreated, org)
}

func (h *AuthHandler) GetOrgUsers(c *gin.Context) {
	userID := middleware.GetUserID(c)

	ctx := c.Request.Context()
	user, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	if user.OrganizationID == "" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "user is not part of an organization"},
		)
		return
	}

	log.Printf("[DEBUG] GetOrgUsers: fetching users for org %s", user.OrganizationID)
	users, err := h.userService.GetUsersByOrg(ctx, user.OrganizationID)
	if err != nil {
		log.Printf("[ERROR] GetOrgUsers: %v", err)
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}
	if users == nil {
		users = []*models.User{}
	}
	log.Printf("[DEBUG] GetOrgUsers: found %d users", len(users))
	c.JSON(http.StatusOK, users)
}

type AddMemberRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required"`
}

func (h *AuthHandler) AddMember(c *gin.Context) {
	var req AddMemberRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": err.Error()},
		)
		return
	}

	userID := middleware.GetUserID(c)

	ctx := c.Request.Context()
	currentUser, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	if currentUser.OrganizationID == "" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "user is not part of an organization"},
		)
		return
	}

	membership, err := h.userService.GetMembership(ctx, userID, currentUser.OrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if membership == nil || membership.Role != "admin" {
		c.JSON(
			http.StatusForbidden,
			gin.H{"error": "only admins can add members"},
		)
		return
	}

	// First check if user exists in Firebase Auth by email
	authUser, err := h.userService.GetAuthUserByEmail(ctx, req.Email)
	if err != nil {
		c.JSON(
			http.StatusNotFound,
			gin.H{"error": "no Firebase user found with that email. User must sign up first."},
		)
		return
	}

	// Try to get existing Firestore user record
	targetUser, err := h.userService.GetUser(ctx, authUser.UID)
	if err != nil {
		// User doesn't exist in Firestore yet, create them
		log.Printf("[AddMember] User %s not in Firestore, creating record", authUser.Email)
		targetUser, err = h.userService.CreateUserRecord(ctx, authUser.UID, authUser.Email, authUser.DisplayName)
		if err != nil {
			log.Printf("[AddMember] Error creating user record: %v", err)
			c.JSON(
				http.StatusInternalServerError,
				gin.H{"error": "failed to create user record"},
			)
			return
		}
	}

	// Allow adding membership even if user belongs to other organizations
	err = h.userService.AddUserToOrg(ctx, targetUser.ID, currentUser.OrganizationID, req.Role)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "member added successfully"})
}

func (h *AuthHandler) RemoveMember(c *gin.Context) {
	userID := middleware.GetUserID(c)
	targetUserID := c.Param("userId")

	if targetUserID == "" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "user ID is required"},
		)
		return
	}

	ctx := c.Request.Context()
	currentUser, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	if currentUser.OrganizationID == "" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "user is not part of an organization"},
		)
		return
	}

	membership, err := h.userService.GetMembership(ctx, userID, currentUser.OrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if membership == nil || membership.Role != "admin" {
		c.JSON(
			http.StatusForbidden,
			gin.H{"error": "only admins can remove members"},
		)
		return
	}

	// Don't allow admin to remove themselves
	if targetUserID == userID {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "cannot remove yourself from the organization"},
		)
		return
	}

	// Remove membership for current admin's organization
	err = h.userService.RemoveUserFromOrg(ctx, targetUserID, currentUser.OrganizationID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "member removed successfully"})
}

// List organizations the current user belongs to
func (h *AuthHandler) GetMyOrganizations(c *gin.Context) {
	userID := middleware.GetUserID(c)
	ctx := c.Request.Context()
	orgs, err := h.userService.GetUserOrganizations(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, orgs)
}

// Set active organization for current user
func (h *AuthHandler) SetActiveOrganization(c *gin.Context) {
	userID := middleware.GetUserID(c)
	var req struct {
		OrganizationID string `json:"organization_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx := c.Request.Context()
	if err := h.userService.SetActiveOrganization(ctx, userID, req.OrganizationID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "active organization updated"})
}

func (h *AuthHandler) LeaveOrganization(c *gin.Context) {
	userID := middleware.GetUserID(c)

	ctx := c.Request.Context()
	user, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	if user.OrganizationID == "" || user.OrganizationID == "default" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "you are not part of an organization"},
		)
		return
	}

	err = h.userService.RemoveUserFromOrg(ctx, userID, user.OrganizationID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "successfully left organization"})
}

func (h *AuthHandler) SearchOrganizations(c *gin.Context) {
	query := c.Query("q")
	ctx := c.Request.Context()

	organizations, err := h.userService.SearchOrganizations(ctx, query)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, organizations)
}

func (h *AuthHandler) CreateJoinRequest(c *gin.Context) {
	userID := middleware.GetUserID(c)

	var req struct {
		OrganizationID string `json:"organization_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": err.Error()},
		)
		return
	}

	ctx := c.Request.Context()
	joinRequest, err := h.userService.CreateJoinRequest(ctx, userID, req.OrganizationID)
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusCreated, joinRequest)
}

func (h *AuthHandler) GetJoinRequests(c *gin.Context) {
	userID := middleware.GetUserID(c)
	ctx := c.Request.Context()

	requests, err := h.userService.GetUserJoinRequests(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, requests)
}

// Old: ApproveJoinRequest (kept for backward compatibility, but should be deprecated)
func (h *AuthHandler) ApproveJoinRequest(c *gin.Context) {
	userID := middleware.GetUserID(c)
	requestID := c.Param("id")

	ctx := c.Request.Context()
	user, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	if user.OrganizationID == "" || user.OrganizationID == "default" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "you are not part of an organization"},
		)
		return
	}

	membership, err := h.userService.GetMembership(ctx, userID, user.OrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if membership == nil || membership.Role != "admin" {
		c.JSON(
			http.StatusForbidden,
			gin.H{"error": "only admins can approve join requests"},
		)
		return
	}

	err = h.userService.ApproveJoinRequest(ctx, requestID, user.OrganizationID)
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "join request approved"})
}

// New: ApproveJoinRequestForOrg (uses orgId from route)
func (h *AuthHandler) ApproveJoinRequestForOrg(c *gin.Context) {
	userID := middleware.GetUserID(c)
	orgID := c.Param("orgId")
	requestID := c.Param("id")

	ctx := c.Request.Context()
	// Check admin membership for this org
	membership, err := h.userService.GetMembership(ctx, userID, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if membership == nil || membership.Role != "admin" {
		c.JSON(
			http.StatusForbidden,
			gin.H{"error": "only admins can approve join requests for this organization"},
		)
		return
	}

	err = h.userService.ApproveJoinRequest(ctx, requestID, orgID)
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "join request approved"})
}

func (h *AuthHandler) RejectJoinRequest(c *gin.Context) {
	userID := middleware.GetUserID(c)
	requestID := c.Param("id")

	ctx := c.Request.Context()
	user, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	if user.OrganizationID == "" || user.OrganizationID == "default" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "you are not part of an organization"},
		)
		return
	}

	membership, err := h.userService.GetMembership(ctx, userID, user.OrganizationID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if membership == nil || membership.Role != "admin" {
		c.JSON(
			http.StatusForbidden,
			gin.H{"error": "only admins can reject join requests"},
		)
		return
	}

	err = h.userService.RejectJoinRequest(ctx, requestID, user.OrganizationID)
	if err != nil {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "join request rejected"})
}

// CleanupJoinRequests removes all non-pending (approved/rejected) join requests.
// Admin-only action to purge legacy entries so users can re-request cleanly.
func (h *AuthHandler) CleanupJoinRequests(c *gin.Context) {
	userID := middleware.GetUserID(c)
	ctx := c.Request.Context()

	user, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if user.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin access required"})
		return
	}

	count, err := h.userService.CleanupJoinRequests(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deleted": count})
}

// GetOrgJoinRequests returns all pending join requests for a specific organization (admin only)
func (h *AuthHandler) GetOrgJoinRequests(c *gin.Context) {
	orgID := c.Param("orgId")
	ctx := c.Request.Context()

	requests, err := h.userService.GetOrgJoinRequests(ctx, orgID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, requests)
}

// SaveDatadogSecrets saves masked Datadog secrets and dashboard settings for an organization.
func (h *AuthHandler) SaveDatadogSecrets(c *gin.Context) {
	userID := middleware.GetUserID(c)
	orgID := c.Param("orgId")

	// Parse incoming payload (allow flexible shape)
	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Ensure caller is admin of the target org
	membership, err := h.userService.GetMembership(ctx, userID, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if membership == nil || membership.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "only admins can update organization secrets"})
		return
	}

	// Extract secrets and settings from payload
	var secrets map[string]interface{}
	var settings map[string]interface{}
	// Prefer `secrets` object if provided
	if v, ok := payload["secrets"]; ok {
		if s, ok2 := v.(map[string]interface{}); ok2 {
			secrets = s
		}
	}
	// backward compatibility: allow top-level fields, but only if any are present
	if secrets == nil {
		// only create secrets map if top-level keys are present
		hasAny := false
		tmp := map[string]interface{}{}
		if v, ok := payload["apiKey"]; ok {
			tmp["apiKey"] = v
			hasAny = true
		}
		if v, ok := payload["appKey"]; ok {
			tmp["appKey"] = v
			hasAny = true
		}
		if v, ok := payload["webhookSecret"]; ok {
			tmp["webhookSecret"] = v
			hasAny = true
		}
		if hasAny {
			secrets = tmp
		}
	}

	if v, ok := payload["settings"]; ok {
		if s, ok2 := v.(map[string]interface{}); ok2 {
			settings = s
		}
	}
	if settings == nil {
		settings = map[string]interface{}{}
		// copy known toggles if present
		for _, k := range []string{"systemMetrics", "alertStatus", "activityFeed", "securitySignals", "liveLogs"} {
			if val, ok := payload[k]; ok {
				settings[k] = val
			}
		}
	}

	// Save to organization document. Pass the original secrets only when provided
	// so the service can perform encryption/hashing and not accidentally clear values.
	if err := h.userService.UpdateOrganizationDatadog(ctx, orgID, secrets, settings); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "datadog secrets saved"})
}
