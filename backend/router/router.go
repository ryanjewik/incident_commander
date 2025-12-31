package router

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/config"
	"github.com/ryanjewik/incident_commander/backend/handlers"
	"github.com/ryanjewik/incident_commander/backend/middleware"
	"github.com/ryanjewik/incident_commander/backend/services"
)

func Register(r *gin.Engine, app *handlers.App, userService *services.UserService, incidentHandler *handlers.IncidentHandler, firebaseService *services.FirebaseService, incidentService *services.IncidentService) *handlers.ChatHandler {
	// Configure CORS
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Root endpoint
	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello, Gin!",
		})
	})

	// Initialize auth handler
	authHandler := handlers.NewAuthHandler(userService)

	// Initialize chat handler
	chatHandler := handlers.NewChatHandler(userService, firebaseService, app.KafkaService)
	// Initialize datadog poller & handler
	ddService := services.NewDatadogService(config.Load(), firebaseService)
	ddHandler := handlers.NewDatadogHandler(ddService)
	// Initialize datadog webhook handler (public endpoint)
	ddWebhookHandler := handlers.NewDatadogWebhookHandler(incidentService, firebaseService, ddService, chatHandler)

	// wire broadcaster so DatadogService can push poll updates to websocket clients
	ddService.SetBroadcaster(func(orgID string, payload interface{}) {
		// payload is expected to be a map[string]interface{}
		if chatHandler != nil {
			chatHandler.BroadcastEvent(orgID, payload)
		}
	})
	// OAuth token endpoint
	oauthHandler := handlers.NewOAuthHandler(firebaseService)

	// Public routes don't need auth
	public := r.Group("/api/auth")
	{
		public.POST("/users", authHandler.CreateUser)
	}

	// Protected routes need auth
	protected := r.Group("/api/auth")
	protected.Use(middleware.AuthMiddleware(userService))
	{
		protected.GET("/me", authHandler.GetMe)
		protected.POST("/organizations", authHandler.CreateOrganization)
		protected.POST("/organizations/leave", authHandler.LeaveOrganization)
		protected.GET("/organizations/search", authHandler.SearchOrganizations)
		protected.GET("/users", authHandler.GetOrgUsers)
		protected.GET("/organizations/:orgId/users", authHandler.GetOrgUsersById)
		protected.POST("/organizations/members", authHandler.AddMember)
		protected.DELETE("/organizations/members/:userId", authHandler.RemoveMember)
		protected.POST("/organizations/join-requests", authHandler.CreateJoinRequest)
		protected.GET("/organizations/join-requests", authHandler.GetJoinRequests)
		protected.GET("/organizations/:orgId/join-requests", authHandler.GetOrgJoinRequests)
		protected.PUT("/organizations/join-requests/:id/approve", authHandler.ApproveJoinRequest) // legacy
		protected.PUT("/organizations/:orgId/join-requests/:id/approve", authHandler.ApproveJoinRequestForOrg)
		protected.PUT("/organizations/join-requests/:id/reject", authHandler.RejectJoinRequest)
		protected.DELETE("/organizations/join-requests/cleanup", authHandler.CleanupJoinRequests)
		protected.GET("/my-organizations", authHandler.GetMyOrganizations)
		protected.POST("/organizations/active", authHandler.SetActiveOrganization)
		protected.POST("/organizations/:orgId/datadog-secrets", authHandler.SaveDatadogSecrets)
		protected.GET("/organizations/:orgId", authHandler.GetOrganization)
	}

	// Protected incident routes
	incidents := r.Group("/api/incidents")
	incidents.Use(middleware.AuthMiddleware(userService))
	{
		incidents.POST("", incidentHandler.CreateIncident)
		incidents.GET("", incidentHandler.GetIncidents)
		incidents.GET("/:id", incidentHandler.GetIncident)
		incidents.PUT("/:id", incidentHandler.UpdateIncident)
		incidents.DELETE("/:id", incidentHandler.DeleteIncident)
	}

	// Protected chat routes
	chat := r.Group("/api/chat")
	chat.Use(middleware.AuthMiddleware(userService))
	{
		chat.POST("/messages", chatHandler.SendMessage)
		chat.GET("/messages", chatHandler.GetMessages)
	}

	// Datadog data endpoints (protected)
	dd := r.Group("/api/datadog")
	dd.Use(middleware.AuthMiddleware(userService))
	{
		dd.GET("/:orgId/overview", ddHandler.GetOverview)
		dd.GET("/:orgId/timeline", ddHandler.GetTimeline)
		dd.GET("/:orgId/recent_logs", ddHandler.GetRecentLogs)
		dd.GET("/:orgId/status_distribution", ddHandler.GetStatusDistribution)
		dd.GET("/:orgId/monitors", ddHandler.GetMonitors)
	}

	// Public debug route for Datadog overview (bypasses auth) to help troubleshooting
	r.GET("/api/datadog/:orgId/overview_public", ddHandler.GetOverview)
	// Additional public debug routes for status distribution and recent logs
	r.GET("/api/datadog/:orgId/status_distribution_public", ddHandler.GetStatusDistribution)
	r.GET("/api/datadog/:orgId/recent_logs_public", ddHandler.GetRecentLogs)
	// Alternate public debug routes (different path) in case clients prefer a namespaced public path
	r.GET("/api/datadog/public/:orgId/status_distribution", ddHandler.GetStatusDistribution)
	r.GET("/api/datadog/public/:orgId/recent_logs", ddHandler.GetRecentLogs)

	// WebSocket endpoint (auth handled in handler via token query param)
	r.GET("/api/chat/ws", chatHandler.HandleWebSocket)

	// Datadog webhook endpoint (no auth middleware; validates secret header)
	r.POST("/webhook/datadog", ddWebhookHandler.HandleDatadogWebhook)
	// Alias for legacy path — accept Datadog configured with /datadog/webhook
	r.POST("/datadog/webhook", ddWebhookHandler.HandleDatadogWebhook)

	// Internal endpoint for agents to fetch decrypted organization secrets.
	// Protected by Firebase auth middleware (agents must present a valid Firebase ID token).
	internal := r.Group("/internal")
	internal.Use(middleware.AuthMiddleware(userService))
	{
		internal.GET("/orgs/:orgId/secrets", ddWebhookHandler.GetOrgSecrets)
	}

	// OAuth token endpoint for client_credentials
	r.POST("/oauth/token", oauthHandler.Token)

	// NL Query endpoint - now protected
	nlQuery := r.Group("")
	nlQuery.Use(middleware.AuthMiddleware(userService))
	{
		nlQuery.POST("/NL_query", app.NLQuery)
	}
	return chatHandler
}
