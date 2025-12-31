package main

import (
	"log"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/ryanjewik/incident_commander/backend/config"
	"github.com/ryanjewik/incident_commander/backend/handlers"
	"github.com/ryanjewik/incident_commander/backend/router"
	"github.com/ryanjewik/incident_commander/backend/services"
)

func main() {
	godotenv.Load()

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

	// Migration code removed - timestamps are now handled as time.Time directly

	// start moderator decision consumer (if KAFKA_BOOTSTRAP_SERVERS is set)
	if err := services.StartModeratorConsumer(firebaseService); err != nil {
		log.Printf("failed to start moderator consumer: %v", err)
	}

	userService := services.NewUserService(firebaseService)
	incidentService := services.NewIncidentService(firebaseService)
	incidentHandler := handlers.NewIncidentHandler(incidentService)

	app := handlers.NewApp(cfg, nil, userService, firebaseService)

	r := gin.New()

	router.Register(r, app, userService, incidentHandler, firebaseService, incidentService)

	log.Printf("Starting server on port %s", cfg.Port)
	r.Run(":" + cfg.Port)
}
