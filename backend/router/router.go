package router

import (
	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/handlers"
)

func Register(r *gin.Engine, app *handlers.App) {
	r.GET("/health", app.Health)

}
