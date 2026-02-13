package main

import (
	"log"

	"weathering-with-go/config"
	"weathering-with-go/handlers"
	"weathering-with-go/middleware"
	"weathering-with-go/services"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Printf("Configuration warning: %v", err)
	}

	// Set gin mode based on environment
	if cfg.IsProduction() {
		log.Println("Running in production mode")
		gin.SetMode(gin.ReleaseMode)
	}

	// Create weather service
	weatherService := services.NewWeatherService(cfg.OpenWeatherMapAPIKey)

	// Create gin router
	router := gin.Default()

	// Add middleware
	setupMiddleware(router, cfg)

	// Setup routes
	handlers.SetupRoutes(router, weatherService)

	// Start server
	log.Printf("Starting server on %s", cfg.GetServerAddress())
	log.Printf("Environment: %s", cfg.Environment)
	log.Printf("Log Level: %s", cfg.LogLevel)

	// Start the server (blocking call)
	if err := router.Run(cfg.GetServerAddress()); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// setupMiddleware configures middleware for the gin router
func setupMiddleware(router *gin.Engine, cfg *config.Config) {
	// Security headers
	router.Use(middleware.Security())

	// CORS middleware
	router.Use(middleware.CORS())

	// Request ID middleware
	router.Use(middleware.RequestID())

	// Logger middleware
	router.Use(middleware.Logger(cfg))

	// Recovery middleware
	router.Use(gin.Recovery())
}
