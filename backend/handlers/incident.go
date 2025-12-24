package handlers

import (
	"net/http"

	"log"

	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/models"
	"github.com/ryanjewik/incident_commander/backend/services"
)

type IncidentHandler struct {
	incidentService *services.IncidentService
}

func NewIncidentHandler(incidentService *services.IncidentService) *IncidentHandler {
	return &IncidentHandler{
		incidentService: incidentService,
	}
}

func (h *IncidentHandler) CreateIncident(c *gin.Context) {
	log.Printf("[IncidentHandler] CreateIncident called")
	log.Printf("[IncidentHandler] Request method: %s, Path: %s", c.Request.Method, c.Request.URL.Path)

	var req models.CreateIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[IncidentHandler] Failed to bind JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Request data: Title=%s, Type=%s, Status=%s", req.Title, req.Type, req.Status)

	log.Printf("[IncidentHandler] Attempting to get userID from context...")
	log.Printf("[IncidentHandler] All Gin context keys present:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - Key: %s, Value: %v, Type: %T", key, val, val)
	}

	userID, exists := c.Get("userID")
	log.Printf("[IncidentHandler] userID exists: %v, value: %v, type: %T", exists, userID, userID)
	if !exists {
		log.Printf("[IncidentHandler] userID NOT FOUND in context")
		log.Printf("[IncidentHandler] c.Keys map has %d entries", len(c.Keys))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
		log.Printf("[IncidentHandler] userID type assertion failed or empty string. ok=%v, value='%s'", ok, userIDStr)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	log.Printf("[IncidentHandler] Attempting to get organizationID from context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] organizationID exists: %v, value: %v, type: %T", exists, organizationID, organizationID)
	if !exists {
		log.Printf("[IncidentHandler] organizationID NOT FOUND in context")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	if !ok || orgIDStr == "" {
		log.Printf("[IncidentHandler] organizationID type assertion failed or empty string. ok=%v, value='%s'", ok, orgIDStr)
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Creating incident with userID=%s, orgID=%s", userIDStr, orgIDStr)
	incident, err := h.incidentService.CreateIncident(
		c.Request.Context(),
		&req,
		orgIDStr,
		userIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] Failed to create incident: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Incident created successfully: ID=%s", incident.ID)
	c.JSON(http.StatusCreated, incident)
}

func (h *IncidentHandler) GetIncident(c *gin.Context) {
	incidentID := c.Param("id")

	organizationID, exists := c.Get("organizationID")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	incident, err := h.incidentService.GetIncident(
		c.Request.Context(),
		incidentID,
		organizationID.(string),
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, incident)
}

func (h *IncidentHandler) GetIncidents(c *gin.Context) {
	organizationID, exists := c.Get("organizationID")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	incidents, err := h.incidentService.GetIncidentsByOrganization(
		c.Request.Context(),
		organizationID.(string),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, incidents)
}

func (h *IncidentHandler) UpdateIncident(c *gin.Context) {
	incidentID := c.Param("id")

	var req models.UpdateIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	organizationID, exists := c.Get("organizationID")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	incident, err := h.incidentService.UpdateIncident(
		c.Request.Context(),
		incidentID,
		&req,
		organizationID.(string),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, incident)
}

func (h *IncidentHandler) DeleteIncident(c *gin.Context) {
	incidentID := c.Param("id")

	organizationID, exists := c.Get("organizationID")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	err := h.incidentService.DeleteIncident(
		c.Request.Context(),
		incidentID,
		organizationID.(string),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "incident deleted successfully"})
}
