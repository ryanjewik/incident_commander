package handlers

import (
	"github.com/ryanjewik/incident_commander/backend/config"
	"github.com/ryanjewik/incident_commander/backend/services"
)

type App struct {
	Cfg          config.Config
	KafkaService *services.KafkaService
}

func NewApp(
	cfg config.Config,
	kafkaService *services.KafkaService,
) *App {
	return &App{
		Cfg:          cfg,
		KafkaService: kafkaService,
	}
}
