package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/kyenel64/invosit-api/internal/db"
)

func main() {

	// Load env
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	// Connect to db
	database, err := db.Open(databaseURL)
	if err != nil {
		log.Fatalf("could not connect to database: %v", err)
	}
	defer database.Close()

	// Gin router
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	registerRoutes(r, database)

	log.Printf("starting server on: %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func registerRoutes(r *gin.Engine, database *sql.DB) {
	api := r.Group("/api/v1")

	api.GET("/health", func(c *gin.Context) {
		if err := database.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "error",
				"error":  "database unreachable",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})
}
