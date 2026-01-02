package main

import (
	"bufio"
	"log"
	"os"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/ryanjewik/incident_commander/backend/config"
	"github.com/ryanjewik/incident_commander/backend/handlers"
	"github.com/ryanjewik/incident_commander/backend/router"
	"github.com/ryanjewik/incident_commander/backend/services"
)

func main() {
	// Load environment variables from .env if present
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, relying on environment variables")
	}

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

	userService := services.NewUserService(firebaseService)
	incidentService := services.NewIncidentService(firebaseService)
	incidentHandler := handlers.NewIncidentHandler(incidentService)

	// Attempt to initialize KafkaService from CLIENT_PROPERTIES_PATH (or default)
	var kafkaService *services.KafkaService
	propsPath := os.Getenv("CLIENT_PROPERTIES_PATH")
	if propsPath == "" {
		propsPath = "backend/client.properties"
	}
	if f, err := os.Open(propsPath); err != nil {
		log.Printf("Kafka client properties not found at %s: %v; continuing without Kafka", propsPath, err)
	} else {
		cfgMap := kafka.ConfigMap{}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			cfgMap[key] = val
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			log.Printf("error reading kafka client properties: %v", err)
		} else {
			if ks, err := services.NewKafkaService(cfgMap); err != nil {
				log.Printf("failed to create KafkaService: %v", err)
			} else {
				kafkaService = ks
				// ensure we close producer/consumer on exit
				defer kafkaService.Close()
			}
		}
	}

	app := handlers.NewApp(cfg, kafkaService, userService, firebaseService)

	r := gin.New()

	// Add CORS middleware with comprehensive origins list
	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{
			"http://localhost:5173",
			"http://localhost:3000",
			"http://34.169.180.116",
			"http://34.169.180.116:5173",
			"http://34.169.180.116:8080",
			"https://incident-commander.duckdns.org",
			"http://incident-commander.duckdns.org",
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	// Register routes and get the ChatHandler so we can wire broadcaster callbacks
	chatHandler := router.Register(r, app, userService, incidentHandler, firebaseService, incidentService)

	// Start moderator consumer (with broadcaster) and incident-bus listener after router registration
	// so that we can broadcast moderator decisions to WebSocket clients in real-time.
	if err := services.StartModeratorConsumer(firebaseService, func(orgID string, payload interface{}) {
		if chatHandler != nil {
			chatHandler.BroadcastEvent(orgID, payload)
		}
	}); err != nil {
		log.Printf("failed to start moderator consumer: %v", err)
	}

	// Start incident-bus listener to persist expected_agents/type for new incidents
	if err := services.StartIncidentBusListener(firebaseService); err != nil {
		log.Printf("failed to start incident-bus listener: %v", err)
	}

	log.Printf("Starting server on port %s", cfg.Port)
	r.Run("0.0.0.0:" + cfg.Port)
}
