package main

import (
	"github.com/gin-gonic/gin"
	"github.com/ryanjewik/incident_commander/backend/handlers"
	"github.com/ryanjewik/incident_commander/backend/router"
)

func main() {

	app := handlers.NewApp()

	r := gin.Default() // Creates a router with default middleware (logger and recovery)

	router.Register(r, app)

	r.Run(":8080") // Listen and serve on port 8080
}
