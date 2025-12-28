package main

import (
	"log"

	"github.com/gin-gonic/gin"

	"github.com/ryanjewik/incident_commander/backend/config"
	"github.com/ryanjewik/incident_commander/backend/handlers"
	"github.com/ryanjewik/incident_commander/backend/router"
	"github.com/ryanjewik/incident_commander/backend/services"
)

func main() {
	// Load .env file - this must come BEFORE config.Load()
	// if err := godotenv.Load(); err != nil {
	// 	log.Println("No .env file found, using environment variables")
	// }

	cfg := config.Load()

	if cfg.Port == "" {
		cfg.Port = "8080"
		log.Println("defaulting:8080")
	}

	firebaseService, err := services.NewFirebaseService(cfg.FirebaseCredentialsPath)
	if err != nil {
		panic(err)
	}
	defer firebaseService.Close()

	userService := services.NewUserService(firebaseService)
	incidentService := services.NewIncidentService(firebaseService)

	app := handlers.NewApp(cfg, nil, userService, firebaseService)

	r := gin.Default()

	router.Register(r, app, userService, incidentService, firebaseService)

	log.Printf("Starting server on port %s", cfg.Port)
	r.Run(":" + cfg.Port)
}
