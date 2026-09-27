package utils

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"weathering-with-go/models"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   string `json:"value,omitempty"`
}

// APIError represents a structured API error
type APIError struct {
	Code       int                 `json:"code"`
	Message    string             `json:"message"`
	Details    string             `json:"details,omitempty"`
	Validation []ValidationError  `json:"validation,omitempty"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API Error %d: %s", e.Code, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message, value string) ValidationError {
	return ValidationError{
		Field:   field,
		Message: message,
		Value:   value,
	}
}

// NewAPIError creates a new API error
func NewAPIError(code int, message string, details ...string) *APIError {
	apiErr := &APIError{
		Code:    code,
		Message: message,
	}
	
	if len(details) > 0 {
		apiErr.Details = details[0]
	}
	
	return apiErr
}

// AddValidationError adds a validation error to the API error
func (e *APIError) AddValidationError(field, message, value string) {
	if e.Validation == nil {
		e.Validation = make([]ValidationError, 0)
	}
	e.Validation = append(e.Validation, NewValidationError(field, message, value))
}

// SendError sends a structured error response
func SendError(c *gin.Context, err error) {
	var statusCode int
	var apiErr *APIError

	switch e := err.(type) {
	case *APIError:
		statusCode = e.Code
		apiErr = e
	default:
		statusCode = http.StatusInternalServerError
		apiErr = NewAPIError(statusCode, "Internal server error", err.Error())
	}

	response := models.APIResponse{
		Success: false,
		Error: &models.ErrorResponse{
			Error:   http.StatusText(statusCode),
			Code:    statusCode,
			Message: apiErr.Message,
		},
	}

	c.JSON(statusCode, response)
}

// SendSuccess sends a successful response
func SendSuccess(c *gin.Context, data interface{}) {
	response := models.APIResponse{
		Success: true,
		Data:    data,
	}
	c.JSON(http.StatusOK, response)
}

// SendCachedSuccess sends a successful response and reports whether the data was
// served from cache, so callers can observe upstream call savings.
func SendCachedSuccess(c *gin.Context, data interface{}, cacheHit bool) {
	if cacheHit {
		c.Header("X-Cache", "HIT")
	} else {
		c.Header("X-Cache", "MISS")
	}
	SendSuccess(c, data)
}

// ValidateLocation validates a location parameter
func ValidateLocation(location string) error {
	if location == "" {
		return NewAPIError(http.StatusBadRequest, "Location is required")
	}
	
	if len(location) < 2 {
		return NewAPIError(http.StatusBadRequest, "Location must be at least 2 characters long")
	}
	
	if len(location) > 100 {
		return NewAPIError(http.StatusBadRequest, "Location must be less than 100 characters")
	}
	
	return nil
}

// ValidateUnits validates a units parameter
func ValidateUnits(units string) error {
	if units == "" {
		return nil // Will use default
	}
	
	validUnits := []string{"metric", "imperial", "kelvin"}
	for _, validUnit := range validUnits {
		if units == validUnit {
			return nil
		}
	}
	
	apiErr := NewAPIError(http.StatusBadRequest, "Invalid units parameter")
	apiErr.AddValidationError("units", fmt.Sprintf("Must be one of: %s", strings.Join(validUnits, ", ")), units)
	return apiErr
}

// ValidateDays validates a days parameter
func ValidateDays(days int) error {
	if days < 1 {
		apiErr := NewAPIError(http.StatusBadRequest, "Invalid days parameter")
		apiErr.AddValidationError("days", "Must be at least 1", fmt.Sprintf("%d", days))
		return apiErr
	}
	
	if days > 5 {
		apiErr := NewAPIError(http.StatusBadRequest, "Invalid days parameter")
		apiErr.AddValidationError("days", "Must be 5 or less (OpenWeatherMap limitation)", fmt.Sprintf("%d", days))
		return apiErr
	}
	
	return nil
}

// HandleWeatherAPIError handles errors from the weather service
func HandleWeatherAPIError(err error) error {
	errMsg := err.Error()
	
	// Check for common API errors
	if strings.Contains(errMsg, "status 401") {
		return NewAPIError(http.StatusUnauthorized, "Invalid API key", "Please check your OpenWeatherMap API key")
	}
	
	if strings.Contains(errMsg, "status 404") {
		return NewAPIError(http.StatusNotFound, "Location not found", "The specified location could not be found")
	}
	
	if strings.Contains(errMsg, "status 429") {
		return NewAPIError(http.StatusTooManyRequests, "Rate limit exceeded", "Please try again later")
	}
	
	if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "connection") {
		return NewAPIError(http.StatusServiceUnavailable, "Weather service temporarily unavailable", "Please try again later")
	}
	
	// Default to internal server error
	return NewAPIError(http.StatusInternalServerError, "Weather service error", errMsg)
}