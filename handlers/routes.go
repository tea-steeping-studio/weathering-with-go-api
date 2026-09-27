package handlers

import (
	"github.com/gin-gonic/gin"
	"weathering-with-go/services"
)

// SetupRoutes configures all the API routes
func SetupRoutes(router *gin.Engine, weatherService *services.WeatherService) {
	// Create handlers
	weatherHandler := NewWeatherHandler(weatherService)

	// API version group
	v1 := router.Group("/api/v1")
	{
		// Weather routes
		weather := v1.Group("/weather")
		{
			// GET routes
			weather.GET("/current", weatherHandler.GetCurrentWeather)
			weather.GET("/forecast", weatherHandler.GetWeatherForecast)
			weather.GET("/forecast/7day", weatherHandler.GetSevenDayForecast)

			// POST routes (for JSON body requests)
			weather.POST("/current", weatherHandler.PostCurrentWeather)
			weather.POST("/forecast", weatherHandler.PostWeatherForecast)
			weather.POST("/forecast/7day", weatherHandler.PostSevenDayForecast)
		}

		// Health check
		v1.GET("/health", weatherHandler.HealthCheck)
	}

	// Root health check
	router.GET("/health", weatherHandler.HealthCheck)

	// Root endpoint for API info
	router.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message":     "Welcome to Weathering with Go API",
			"version":     "v1.0.0",
			"description": "A simple weather API built with Go and Gin",
			"endpoints": gin.H{
				"health":           "/health",
				"current_weather":  "/api/v1/weather/current?location={location}&units={units}",
				"weather_forecast": "/api/v1/weather/forecast?location={location}&units={units}&days={days}",
				"forecast_7day":    "/api/v1/weather/forecast/7day?location={location}&units={units}",
			},
			"docs": "https://github.com/tea-LZL/weathering-with-go",
		})
	})
}
