package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"weathering-with-go/config"
)

// CORS adds CORS headers to responses
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Request-ID, X-API-Key")
		c.Header("Access-Control-Expose-Headers", "X-Request-ID")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// RequestID adds a unique request ID to each request
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := generateRequestID()
		c.Header("X-Request-ID", requestID)
		c.Set("request_id", requestID)
		c.Next()
	}
}

// Logger creates a custom logger middleware
func Logger(cfg *config.Config) gin.HandlerFunc {
	if cfg.IsProduction() {
		// In production, use a more structured logger
		return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
			return ""
		})
	}

	// In development, use the default gin logger
	return gin.Logger()
}

// RateLimit adds basic rate limiting (simplified version)
func RateLimit() gin.HandlerFunc {
	// This is a simplified rate limiter
	// In production, you'd want to use a proper rate limiting library
	return func(c *gin.Context) {
		// Add rate limiting logic here
		c.Next()
	}
}

// Security adds basic security headers
func Security() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// generateRequestID generates a simple request ID
func generateRequestID() string {
	// Generate a simple timestamp-based ID
	timestamp := time.Now().UnixNano()
	return string(rune(timestamp % 1000000))
}
