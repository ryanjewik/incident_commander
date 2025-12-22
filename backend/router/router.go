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
		AllowOrigins:     []string{"http://localhost:5173"},
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
		protected.GET("/users", authHandler.GetOrgUsers)
		protected.POST("/organizations/members", authHandler.AddMember)
	}
}
