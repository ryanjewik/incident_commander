package handlers

import (
	"github.com/ryanjewik/incident_commander/backend/config"
)

type App struct {
	Cfg config.Config
}

func NewApp(cfg config.Config) *App {
	return &App{
		Cfg: cfg,
	}
}
