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
	log.Printf("[IncidentHandler] ===== GetIncident START =====")
	log.Printf("[IncidentHandler] Request method: %s, Path: %s", c.Request.Method, c.Request.URL.Path)
	log.Printf("[IncidentHandler] Request ID: %p", c.Request)

	incidentID := c.Param("id")
	log.Printf("[IncidentHandler] Incident ID from params: %s", incidentID)

	log.Printf("[IncidentHandler] All Gin context keys present:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - Key: %s, Value: %v, Type: %T", key, val, val)
	}

	log.Printf("[IncidentHandler] Attempting to get organizationID from context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] organizationID exists: %v, value: %v, type: %T", exists, organizationID, organizationID)

	if !exists {
		log.Printf("[IncidentHandler] ERROR: organizationID NOT FOUND in context")
		log.Printf("[IncidentHandler] c.Keys map has %d entries", len(c.Keys))
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	log.Printf("[IncidentHandler] Type assertion result - ok: %v, orgIDStr: '%s'", ok, orgIDStr)
	if !ok {
		log.Printf("[IncidentHandler] ERROR: organizationID type assertion to string failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization ID type"})
		return
	}

	if orgIDStr == "" {
		log.Printf("[IncidentHandler] ERROR: organizationID is empty string")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Getting incident with ID=%s for orgID=%s", incidentID, orgIDStr)
	incident, err := h.incidentService.GetIncident(
		c.Request.Context(),
		incidentID,
		orgIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] Failed to get incident: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Incident retrieved successfully: ID=%s", incident.ID)
	log.Printf("[IncidentHandler] ===== GetIncident END =====")
	c.JSON(http.StatusOK, incident)
}

func (h *IncidentHandler) GetIncidents(c *gin.Context) {
	log.Printf("[IncidentHandler] ===== GetIncidents START =====")
	log.Printf("[IncidentHandler] Request method: %s, Path: %s", c.Request.Method, c.Request.URL.Path)
	log.Printf("[IncidentHandler] Request ID: %p", c.Request)

	log.Printf("[IncidentHandler] All Gin context keys present:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - Key: %s, Value: %v, Type: %T", key, val, val)
	}

	log.Printf("[IncidentHandler] Attempting to get organizationID from context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] organizationID exists: %v, value: %v, type: %T", exists, organizationID, organizationID)

	if !exists {
		log.Printf("[IncidentHandler] ERROR: organizationID NOT FOUND in context")
		log.Printf("[IncidentHandler] c.Keys map has %d entries", len(c.Keys))
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	log.Printf("[IncidentHandler] Type assertion result - ok: %v, orgIDStr: '%s'", ok, orgIDStr)
	if !ok {
		log.Printf("[IncidentHandler] ERROR: organizationID type assertion to string failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization ID type"})
		return
	}

	if orgIDStr == "" {
		log.Printf("[IncidentHandler] ERROR: organizationID is empty string")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Calling incidentService.GetIncidentsByOrganization with orgID=%s", orgIDStr)
	incidents, err := h.incidentService.GetIncidentsByOrganization(
		c.Request.Context(),
		orgIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] ERROR: Failed to get incidents: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Successfully retrieved %d incidents", len(incidents))
	log.Printf("[IncidentHandler] ===== GetIncidents END =====")
	c.JSON(http.StatusOK, incidents)
}

func (h *IncidentHandler) UpdateIncident(c *gin.Context) {
	log.Printf("[IncidentHandler] ===== UpdateIncident START =====")
	log.Printf("[IncidentHandler] Request method: %s, Path: %s", c.Request.Method, c.Request.URL.Path)

	incidentID := c.Param("id")
	log.Printf("[IncidentHandler] Incident ID from params: %s", incidentID)

	var req models.UpdateIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[IncidentHandler] Failed to bind JSON: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] All Gin context keys present:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - Key: %s, Value: %v, Type: %T", key, val, val)
	}

	log.Printf("[IncidentHandler] Attempting to get organizationID from context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] organizationID exists: %v, value: %v, type: %T", exists, organizationID, organizationID)

	if !exists {
		log.Printf("[IncidentHandler] ERROR: organizationID NOT FOUND in context")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	if !ok {
		log.Printf("[IncidentHandler] ERROR: organizationID type assertion to string failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization ID type"})
		return
	}

	if orgIDStr == "" {
		log.Printf("[IncidentHandler] ERROR: organizationID is empty string")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Updating incident ID=%s for orgID=%s", incidentID, orgIDStr)
	incident, err := h.incidentService.UpdateIncident(
		c.Request.Context(),
		incidentID,
		&req,
		orgIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] Failed to update incident: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Incident updated successfully: ID=%s", incident.ID)
	log.Printf("[IncidentHandler] ===== UpdateIncident END =====")
	c.JSON(http.StatusOK, incident)
}

func (h *IncidentHandler) DeleteIncident(c *gin.Context) {
	log.Printf("[IncidentHandler] ===== DeleteIncident START =====")
	log.Printf("[IncidentHandler] Request method: %s, Path: %s", c.Request.Method, c.Request.URL.Path)

	incidentID := c.Param("id")
	log.Printf("[IncidentHandler] Incident ID from params: %s", incidentID)

	log.Printf("[IncidentHandler] All Gin context keys present:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - Key: %s, Value: %v, Type: %T", key, val, val)
	}

	log.Printf("[IncidentHandler] Attempting to get organizationID from context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] organizationID exists: %v, value: %v, type: %T", exists, organizationID, organizationID)

	if !exists {
		log.Printf("[IncidentHandler] ERROR: organizationID NOT FOUND in context")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	if !ok {
		log.Printf("[IncidentHandler] ERROR: organizationID type assertion to string failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization ID type"})
		return
	}

	if orgIDStr == "" {
		log.Printf("[IncidentHandler] ERROR: organizationID is empty string")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Deleting incident ID=%s for orgID=%s", incidentID, orgIDStr)
	err := h.incidentService.DeleteIncident(
		c.Request.Context(),
		incidentID,
		orgIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] Failed to delete incident: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Incident deleted successfully: ID=%s", incidentID)
	log.Printf("[IncidentHandler] ===== DeleteIncident END =====")
	c.JSON(http.StatusOK, gin.H{"message": "incident deleted successfully"})
}
