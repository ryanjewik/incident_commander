package handlers

import (
	"github.com/ryanjewik/incident_commander/backend/config"
	"github.com/ryanjewik/incident_commander/backend/services"
)

type App struct {
	Cfg             config.Config
	KafkaService    *services.KafkaService
	UserService     *services.UserService
	FirebaseService *services.FirebaseService
}

func NewApp(
	cfg config.Config,
	kafkaService *services.KafkaService,
	userService *services.UserService,
	firebaseService *services.FirebaseService,
) *App {
	return &App{
		Cfg:             cfg,
		KafkaService:    kafkaService,
		UserService:     userService,
		FirebaseService: firebaseService,
	}
}
