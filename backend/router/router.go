package router

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/handlers"
	"github.com/ryanjewik/incident_commander/backend/middleware"
	"github.com/ryanjewik/incident_commander/backend/services"
)

func Register(r *gin.Engine, app *handlers.App, userService *services.UserService) {
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

	// Health check
	r.GET("/health", app.Health)

	// NL Query endpoint
	r.POST("/NL_query", app.NLQuery)

	// Initialize auth handler
	authHandler := handlers.NewAuthHandler(userService)

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
	}
}
