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
	log.Printf("[IncidentHandler] Initiating incident creation request")
	log.Printf("[IncidentHandler] HTTP method: %s, endpoint: %s", c.Request.Method, c.Request.URL.Path)

	var req models.CreateIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[IncidentHandler] JSON binding failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Parsed request: Title=%s, Type=%s, Status=%s", req.Title, req.Type, req.Status)

	log.Printf("[IncidentHandler] Retrieving user authentication from context...")
	log.Printf("[IncidentHandler] Available context keys:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - %s: %v (type: %T)", key, val, val)
	}

	userID, exists := c.Get("userID")
	log.Printf("[IncidentHandler] UserID lookup: exists=%v, value=%v, type=%T", exists, userID, userID)
	if !exists {
		log.Printf("[IncidentHandler] Authentication failure: userID not found in context")
		log.Printf("[IncidentHandler] Context contains %d key-value pairs", len(c.Keys))
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	userIDStr, ok := userID.(string)
	if !ok || userIDStr == "" {
		log.Printf("[IncidentHandler] Type conversion failed or empty value. ok=%v, value='%s'", ok, userIDStr)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	log.Printf("[IncidentHandler] Retrieving organization context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] OrganizationID lookup: exists=%v, value=%v, type=%T", exists, organizationID, organizationID)
	if !exists {
		log.Printf("[IncidentHandler] Organization validation failure: organizationID not found in context")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	if !ok || orgIDStr == "" {
		log.Printf("[IncidentHandler] Organization ID type conversion failed or empty. ok=%v, value='%s'", ok, orgIDStr)
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Proceeding with incident creation: userID=%s, organizationID=%s", userIDStr, orgIDStr)
	incident, err := h.incidentService.CreateIncident(
		c.Request.Context(),
		&req,
		orgIDStr,
		userIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] Incident creation failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Incident successfully created with ID=%s", incident.ID)
	c.JSON(http.StatusCreated, incident)
}

func (h *IncidentHandler) GetIncident(c *gin.Context) {
	log.Printf("[IncidentHandler] ===== Single Incident Retrieval Started =====")
	log.Printf("[IncidentHandler] HTTP method: %s, endpoint: %s", c.Request.Method, c.Request.URL.Path)
	log.Printf("[IncidentHandler] Request pointer: %p", c.Request)

	incidentID := c.Param("id")
	log.Printf("[IncidentHandler] Target incident ID from URL parameters: %s", incidentID)

	log.Printf("[IncidentHandler] Available context keys:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - %s: %v (type: %T)", key, val, val)
	}

	log.Printf("[IncidentHandler] Retrieving organization context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] OrganizationID lookup result: exists=%v, value=%v, type=%T", exists, organizationID, organizationID)

	if !exists {
		log.Printf("[IncidentHandler] CRITICAL: organizationID missing from context")
		log.Printf("[IncidentHandler] Total context entries: %d", len(c.Keys))
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	log.Printf("[IncidentHandler] Type assertion outcome - success: %v, value: '%s'", ok, orgIDStr)
	if !ok {
		log.Printf("[IncidentHandler] CRITICAL: organizationID type assertion to string failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization ID type"})
		return
	}

	if orgIDStr == "" {
		log.Printf("[IncidentHandler] CRITICAL: organizationID is empty string")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Fetching incident: ID=%s, organizationID=%s", incidentID, orgIDStr)
	incident, err := h.incidentService.GetIncident(
		c.Request.Context(),
		incidentID,
		orgIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] Incident retrieval failed: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Incident successfully retrieved: ID=%s", incident.ID)
	log.Printf("[IncidentHandler] ===== Single Incident Retrieval Completed =====")
	c.JSON(http.StatusOK, incident)
}

func (h *IncidentHandler) GetIncidents(c *gin.Context) {
	log.Printf("[IncidentHandler] ===== Organization Incidents Retrieval Started =====")
	log.Printf("[IncidentHandler] HTTP method: %s, endpoint: %s", c.Request.Method, c.Request.URL.Path)
	log.Printf("[IncidentHandler] Request pointer: %p", c.Request)

	log.Printf("[IncidentHandler] Available context keys:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - %s: %v (type: %T)", key, val, val)
	}

	log.Printf("[IncidentHandler] Retrieving organization context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] OrganizationID lookup result: exists=%v, value=%v, type=%T", exists, organizationID, organizationID)

	if !exists {
		log.Printf("[IncidentHandler] CRITICAL: organizationID missing from context")
		log.Printf("[IncidentHandler] Total context entries: %d", len(c.Keys))
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	log.Printf("[IncidentHandler] Type assertion outcome - success: %v, value: '%s'", ok, orgIDStr)
	if !ok {
		log.Printf("[IncidentHandler] CRITICAL: organizationID type assertion to string failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization ID type"})
		return
	}

	if orgIDStr == "" {
		log.Printf("[IncidentHandler] CRITICAL: organizationID is empty string")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Invoking incident service for organizationID=%s", orgIDStr)

	incidents, err := h.incidentService.GetIncidentsByOrganization(
		c.Request.Context(),
		orgIDStr,
	)

	if err != nil {
		log.Printf("[IncidentHandler] CRITICAL: Incident retrieval failed: %v", err)
		log.Printf("[IncidentHandler] Error type: %T", err)
		log.Printf("[IncidentHandler] Detailed error information: %+v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if incidents == nil {
		log.Printf("[IncidentHandler] WARNING: Service returned nil incidents slice, initializing empty slice")
		incidents = []*models.Incident{}
	}

	log.Printf("[IncidentHandler] Successfully retrieved %d incident(s)", len(incidents))
	log.Printf("[IncidentHandler] ===== Organization Incidents Retrieval Completed =====")
	c.JSON(http.StatusOK, incidents)
}

func (h *IncidentHandler) UpdateIncident(c *gin.Context) {
	log.Printf("[IncidentHandler] ===== Incident Update Operation Started =====")
	log.Printf("[IncidentHandler] HTTP method: %s, endpoint: %s", c.Request.Method, c.Request.URL.Path)

	incidentID := c.Param("id")
	log.Printf("[IncidentHandler] Target incident ID from URL parameters: %s", incidentID)

	var req models.UpdateIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[IncidentHandler] JSON binding failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Available context keys:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - %s: %v (type: %T)", key, val, val)
	}

	log.Printf("[IncidentHandler] Retrieving organization context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] OrganizationID lookup result: exists=%v, value=%v, type=%T", exists, organizationID, organizationID)

	if !exists {
		log.Printf("[IncidentHandler] CRITICAL: organizationID missing from context")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	if !ok {
		log.Printf("[IncidentHandler] CRITICAL: organizationID type assertion to string failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization ID type"})
		return
	}

	if orgIDStr == "" {
		log.Printf("[IncidentHandler] CRITICAL: organizationID is empty string")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Executing update for incident ID=%s, organizationID=%s", incidentID, orgIDStr)
	incident, err := h.incidentService.UpdateIncident(
		c.Request.Context(),
		incidentID,
		&req,
		orgIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] Incident update failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Incident successfully updated: ID=%s", incident.ID)
	log.Printf("[IncidentHandler] ===== Incident Update Operation Completed =====")
	c.JSON(http.StatusOK, incident)
}

func (h *IncidentHandler) DeleteIncident(c *gin.Context) {
	log.Printf("[IncidentHandler] ===== Incident Deletion Operation Started =====")
	log.Printf("[IncidentHandler] HTTP method: %s, endpoint: %s", c.Request.Method, c.Request.URL.Path)

	incidentID := c.Param("id")
	log.Printf("[IncidentHandler] Target incident ID from URL parameters: %s", incidentID)

	log.Printf("[IncidentHandler] Available context keys:")
	for key := range c.Keys {
		val, _ := c.Get(key)
		log.Printf("[IncidentHandler]   - %s: %v (type: %T)", key, val, val)
	}

	log.Printf("[IncidentHandler] Retrieving organization context...")
	organizationID, exists := c.Get("organizationID")
	log.Printf("[IncidentHandler] OrganizationID lookup result: exists=%v, value=%v, type=%T", exists, organizationID, organizationID)

	if !exists {
		log.Printf("[IncidentHandler] CRITICAL: organizationID missing from context")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	orgIDStr, ok := organizationID.(string)
	if !ok {
		log.Printf("[IncidentHandler] CRITICAL: organizationID type assertion to string failed")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid organization ID type"})
		return
	}

	if orgIDStr == "" {
		log.Printf("[IncidentHandler] CRITICAL: organizationID is empty string")
		c.JSON(http.StatusBadRequest, gin.H{"error": "user must belong to an organization"})
		return
	}

	log.Printf("[IncidentHandler] Executing deletion for incident ID=%s, organizationID=%s", incidentID, orgIDStr)
	err := h.incidentService.DeleteIncident(
		c.Request.Context(),
		incidentID,
		orgIDStr,
	)
	if err != nil {
		log.Printf("[IncidentHandler] Incident deletion failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("[IncidentHandler] Incident successfully deleted: ID=%s", incidentID)
	log.Printf("[IncidentHandler] ===== Incident Deletion Operation Completed =====")
	c.JSON(http.StatusOK, gin.H{"message": "incident deleted successfully"})
}
