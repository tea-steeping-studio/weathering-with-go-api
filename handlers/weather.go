package handlers

import (
	"net/http"
	"strconv"

	"weathering-with-go/models"
	"weathering-with-go/services"
	"weathering-with-go/utils"

	"github.com/gin-gonic/gin"
)

// WeatherHandler handles weather-related HTTP requests
type WeatherHandler struct {
	weatherService *services.WeatherService
}

// NewWeatherHandler creates a new weather handler instance
func NewWeatherHandler(weatherService *services.WeatherService) *WeatherHandler {
	return &WeatherHandler{
		weatherService: weatherService,
	}
}

// GetCurrentWeather handles GET /weather/current requests
func (h *WeatherHandler) GetCurrentWeather(c *gin.Context) {
	location := c.Query("location")
	if err := utils.ValidateLocation(location); err != nil {
		utils.SendError(c, err)
		return
	}

	units := c.DefaultQuery("units", "metric")
	if err := utils.ValidateUnits(units); err != nil {
		utils.SendError(c, err)
		return
	}

	apikey := requestAPIKey(c)

	weatherData, cacheHit, err := h.weatherService.GetCurrentWeather(location, units, apikey)
	if err != nil {
		utils.SendError(c, utils.HandleWeatherAPIError(err))
		return
	}

	utils.SendCachedSuccess(c, weatherData, cacheHit)
}

// GetWeatherForecast handles GET /weather/forecast requests
func (h *WeatherHandler) GetWeatherForecast(c *gin.Context) {
	location := c.Query("location")
	if err := utils.ValidateLocation(location); err != nil {
		utils.SendError(c, err)
		return
	}

	units := c.DefaultQuery("units", "metric")
	if err := utils.ValidateUnits(units); err != nil {
		utils.SendError(c, err)
		return
	}

	daysStr := c.DefaultQuery("days", "5")
	days, err := strconv.Atoi(daysStr)
	if err != nil {
		utils.SendError(c, utils.NewAPIError(http.StatusBadRequest, "Invalid days parameter", "Must be a valid number"))
		return
	}

	if err := utils.ValidateDays(days); err != nil {
		utils.SendError(c, err)
		return
	}

	apikey := requestAPIKey(c)

	weatherData, cacheHit, err := h.weatherService.GetWeatherForecast(location, units, days, apikey)
	if err != nil {
		utils.SendError(c, utils.HandleWeatherAPIError(err))
		return
	}

	utils.SendCachedSuccess(c, weatherData, cacheHit)
}

// PostCurrentWeather handles POST /weather/current requests with JSON body
func (h *WeatherHandler) PostCurrentWeather(c *gin.Context) {
	var req models.WeatherRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, utils.NewAPIError(http.StatusBadRequest, "Invalid request body", err.Error()))
		return
	}

	if err := utils.ValidateLocation(req.Location); err != nil {
		utils.SendError(c, err)
		return
	}

	units := req.Units
	if units == "" {
		units = "metric"
	}

	if err := utils.ValidateUnits(units); err != nil {
		utils.SendError(c, err)
		return
	}

	if req.Keys == "" {
		req.Keys = c.GetHeader("X-API-Key")
	}

	weatherData, cacheHit, err := h.weatherService.GetCurrentWeather(req.Location, units, req.Keys)
	if err != nil {
		utils.SendError(c, utils.HandleWeatherAPIError(err))
		return
	}

	utils.SendCachedSuccess(c, weatherData, cacheHit)
}

// PostWeatherForecast handles POST /weather/forecast requests with JSON body
func (h *WeatherHandler) PostWeatherForecast(c *gin.Context) {
	var req models.WeatherRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, utils.NewAPIError(http.StatusBadRequest, "Invalid request body", err.Error()))
		return
	}

	if err := utils.ValidateLocation(req.Location); err != nil {
		utils.SendError(c, err)
		return
	}

	units := req.Units
	if units == "" {
		units = "metric"
	}

	if err := utils.ValidateUnits(units); err != nil {
		utils.SendError(c, err)
		return
	}

	days := req.Days
	if days == 0 {
		days = 5
	}

	if err := utils.ValidateDays(days); err != nil {
		utils.SendError(c, err)
		return
	}

	if req.Keys == "" {
		req.Keys = c.GetHeader("X-API-Key")
	}

	weatherData, cacheHit, err := h.weatherService.GetWeatherForecast(req.Location, units, days, req.Keys)
	if err != nil {
		utils.SendError(c, utils.HandleWeatherAPIError(err))
		return
	}

	utils.SendCachedSuccess(c, weatherData, cacheHit)
}

// GetSevenDayForecast handles GET /weather/forecast/7day requests
func (h *WeatherHandler) GetSevenDayForecast(c *gin.Context) {
	location := c.Query("location")
	if err := utils.ValidateLocation(location); err != nil {
		utils.SendError(c, err)
		return
	}

	units := c.DefaultQuery("units", "metric")
	if err := utils.ValidateUnits(units); err != nil {
		utils.SendError(c, err)
		return
	}

	weatherData, cacheHit, err := h.weatherService.GetSevenDayForecast(location, units, requestAPIKey(c))
	if err != nil {
		utils.SendError(c, utils.HandleWeatherAPIError(err))
		return
	}

	utils.SendCachedSuccess(c, weatherData, cacheHit)
}

// PostSevenDayForecast handles POST /weather/forecast/7day requests with JSON body
func (h *WeatherHandler) PostSevenDayForecast(c *gin.Context) {
	var req models.WeatherRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, utils.NewAPIError(http.StatusBadRequest, "Invalid request body", err.Error()))
		return
	}

	if err := utils.ValidateLocation(req.Location); err != nil {
		utils.SendError(c, err)
		return
	}

	units := req.Units
	if units == "" {
		units = "metric"
	}

	if err := utils.ValidateUnits(units); err != nil {
		utils.SendError(c, err)
		return
	}

	if req.Keys == "" {
		req.Keys = c.GetHeader("X-API-Key")
	}

	weatherData, cacheHit, err := h.weatherService.GetSevenDayForecast(req.Location, units, req.Keys)
	if err != nil {
		utils.SendError(c, utils.HandleWeatherAPIError(err))
		return
	}

	utils.SendCachedSuccess(c, weatherData, cacheHit)
}

// requestAPIKey resolves the caller supplied api key, if any.
func requestAPIKey(c *gin.Context) string {
	if apikey := c.GetHeader("X-API-Key"); apikey != "" {
		return apikey
	}
	return c.DefaultQuery("key", "")
}

// HealthCheck handles GET /health requests
func (h *WeatherHandler) HealthCheck(c *gin.Context) {
	response := gin.H{
		"status":    "healthy",
		"service":   "weathering-with-go",
		"timestamp": gin.H{"unix": gin.H{}},
	}
	c.JSON(http.StatusOK, response)
}
