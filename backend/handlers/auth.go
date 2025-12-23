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

	users, err := h.userService.GetUsersByOrg(ctx, user.OrganizationID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}
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

	if currentUser.Role != "admin" {
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

	if targetUser.OrganizationID != "" && targetUser.OrganizationID != "default" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "user already belongs to an organization"},
		)
		return
	}

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

	if currentUser.Role != "admin" {
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

	// Verify target user is in the same organization
	targetUser, err := h.userService.GetUser(ctx, targetUserID)
	if err != nil {
		c.JSON(
			http.StatusNotFound,
			gin.H{"error": "user not found"},
		)
		return
	}

	if targetUser.OrganizationID != currentUser.OrganizationID {
		c.JSON(
			http.StatusForbidden,
			gin.H{"error": "user is not in your organization"},
		)
		return
	}

	err = h.userService.RemoveUserFromOrg(ctx, targetUserID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "member removed successfully"})
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

	err = h.userService.RemoveUserFromOrg(ctx, userID)
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

	user, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	// If user is admin, get organization's pending requests
	// Otherwise, get user's own requests
	var requests []*models.JoinRequest
	if user.Role == "admin" && user.OrganizationID != "" && user.OrganizationID != "default" {
		requests, err = h.userService.GetOrgJoinRequests(ctx, user.OrganizationID)
	} else {
		requests, err = h.userService.GetUserJoinRequests(ctx, userID)
	}

	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, requests)
}

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

	if user.Role != "admin" {
		c.JSON(
			http.StatusForbidden,
			gin.H{"error": "only admins can approve join requests"},
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

	if user.Role != "admin" {
		c.JSON(
			http.StatusForbidden,
			gin.H{"error": "only admins can reject join requests"},
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
