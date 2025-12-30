package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/services"
)

type DatadogHandler struct {
	dd *services.DatadogService
}

func NewDatadogHandler(dd *services.DatadogService) *DatadogHandler {
	return &DatadogHandler{dd: dd}
}

// helper to extract orgId param and ensure poller started
func (h *DatadogHandler) getOrgCached(c *gin.Context) *services.CachedDatadog {
	orgId := c.Param("orgId")
	if orgId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "orgId required"})
		return nil
	}
	// ensure polling (start background poller for this org)
	// use 60s interval to poll Datadog every minute
	h.dd.StartPollingOrg(orgId, 60*time.Second)
	// also request an immediate background fetch to refresh any stale cache
	// (StartPollingOrg only starts the poller when not already running)
	h.dd.ForceFetchOrg(orgId)
	// return cached (may be nil until first poll completes)
	return h.dd.GetCachedForOrg(orgId)
}

func (h *DatadogHandler) GetOverview(c *gin.Context) {
	cached := h.getOrgCached(c)
	if cached == nil {
		c.JSON(http.StatusOK, gin.H{"metrics": nil, "last_updated": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"metrics": cached.Metrics, "last_updated": cached.LastUpdated})
}

func (h *DatadogHandler) GetTimeline(c *gin.Context) {
	cached := h.getOrgCached(c)
	if cached == nil {
		c.JSON(http.StatusOK, gin.H{"timeline": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"timeline": cached.Timeline})
}

func (h *DatadogHandler) GetRecentLogs(c *gin.Context) {
	cached := h.getOrgCached(c)
	if cached == nil {
		c.JSON(http.StatusOK, gin.H{"recent_logs": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"recent_logs": cached.RecentLogs})
}

func (h *DatadogHandler) GetStatusDistribution(c *gin.Context) {
	cached := h.getOrgCached(c)
	if cached == nil {
		c.JSON(http.StatusOK, gin.H{"status_distribution": []interface{}{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status_distribution": cached.StatusDistribution})
}

func (h *DatadogHandler) GetMonitors(c *gin.Context) {
	orgId := c.Param("orgId")
	if orgId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "orgId required"})
		return
	}
	// try to fetch fresh monitors; fallback to cached
	mons, err := h.dd.FetchMonitorsForOrg(c.Request.Context(), orgId)
	if err != nil {
		cached := h.getOrgCached(c)
		if cached == nil {
			c.JSON(http.StatusOK, gin.H{"monitors": []interface{}{}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"monitors": cached.Monitors})
		return
	}
	c.JSON(http.StatusOK, gin.H{"monitors": mons})
}
