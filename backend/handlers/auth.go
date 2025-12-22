package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
    "github.com/ryanjewik/incident_commander/backend/middleware"
    "github.com/ryanjewik/incident_commander/backend/services"

)
type AuthHandler struct {
	userService *services.UserService
}
func NewAuthHandler(userService *services.UserService) *AuthHandler {
	handler := AuthHandler{
		userService: userService,
	}
	return &handler
}

type CreateUserRequest struct {
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=6"`
	DisplayName string `json:"display_name" binding:"required"`
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

	ctx := c.Request.Context()
	user, err := h.userService.CreateUser(
		ctx,
		req.Email,
		req.Password,
		req.DisplayName,
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
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
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

	userID := middleware.GetUserID(c)

	ctx := c.Request.Context()
	org, err := h.userService.CreateOrganization(ctx, req.Name, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}
	c.JSON(http.StatusCreated, org)
}


func (h *AuthHandler) GetOrgUsers(c *gin.Context) {
	orgID := middleware.GetOrgID(c)

	ctx := c.Request.Context()
	users, err := h.userService.GetUsersByOrg(ctx, orgID)
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
	orgID := middleware.GetOrgID(c)

	ctx := c.Request.Context()
	currentUser, err := h.userService.GetUser(ctx, userID)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
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

	targetUser, err := h.userService.GetUserByEmail(ctx, req.Email)
	if err != nil {
		c.JSON(
			http.StatusNotFound,
			gin.H{"error": "user not found"},
		)
		return
	}

	if targetUser.OrganizationID != "" {
		c.JSON(
			http.StatusBadRequest,
			gin.H{"error": "user already belongs to an organization"},
		)
		return
	}

	err = h.userService.AddUserToOrg(ctx, targetUser.ID, orgID, req.Role)
	if err != nil {
		c.JSON(
			http.StatusInternalServerError,
			gin.H{"error": err.Error()},
		)
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "member added successfully"})
}