package config

import (
	"log"
	"os"
	"strconv"
)

// BuildTimeAPIKey can be set at build time using -ldflags
var BuildTimeAPIKey string
var Environment string

// Config holds all configuration for the application
type Config struct {
	// Server configuration
	Port string
	Host string

	// API configuration
	OpenWeatherMapAPIKey string

	// Application configuration
	Environment string // development, production, testing
	LogLevel    string // debug, info, warn, error
}

// Load loads configuration from environment variables with defaults
func Load() *Config {
	apiKey := getEnv("OPENWEATHERMAP_API_KEY", "")
	// If no runtime API key is found, use build-time API key
	if apiKey == "" && BuildTimeAPIKey != "" {
		apiKey = BuildTimeAPIKey
	}
	if apiKey != "" {
		log.Print("====> current api key is: [" + apiKey + "]")
	}

	if Environment == "" {
		Environment = getEnv("ENVIRONMENT", "development")
	}
	return &Config{
		// Server configuration
		Port: getEnv("PORT", "8080"),
		Host: getEnv("HOST", "0.0.0.0"),

		// API configuration
		OpenWeatherMapAPIKey: apiKey,

		// Application configuration
		Environment: Environment,
		LogLevel:    getEnv("LOG_LEVEL", "info"),
	}
}

// Validate checks if all required configuration is present
func (c *Config) Validate() error {
	if c.OpenWeatherMapAPIKey == "" {
		return &ConfigError{
			Field:   "OPENWEATHERMAP_API_KEY",
			Message: "OpenWeatherMap API key is required unless provided per request",
		}
	}

	return nil
}

// IsProduction returns true if running in production environment
func (c *Config) IsProduction() bool {
	return c.Environment == "production"
}

// IsDevelopment returns true if running in development environment
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development"
}

// GetServerAddress returns the complete server address
func (c *Config) GetServerAddress() string {
	return c.Host + ":" + c.Port
}

// ConfigError represents a configuration error
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return "Configuration error [" + e.Field + "]: " + e.Message
}

// getEnv gets an environment variable with a fallback default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets an environment variable as integer with a fallback default value
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsBool gets an environment variable as boolean with a fallback default value
func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}
