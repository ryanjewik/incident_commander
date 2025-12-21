package main

import (
	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default() // Creates a router with default middleware (logger and recovery)

	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello, Gin!",
		})
	})

	r.Run(":8080") // Listen and serve on port 8080
}
