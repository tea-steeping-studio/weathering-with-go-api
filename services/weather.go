package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"weathering-with-go/models"
)

const (
	OpenWeatherMapBaseURL  = "https://api.openweathermap.org/data/2.5"
	CurrentWeatherEndpoint = "/weather"
	ForecastEndpoint       = "/forecast"
	DefaultUnits           = "metric"
	DefaultTimeout         = 10 * time.Second
)

// WeatherService handles weather data operations
type WeatherService struct {
	APIKey     string
	HTTPClient *http.Client
}

// NewWeatherService creates a new weather service instance
func NewWeatherService(apiKey string) *WeatherService {
	return &WeatherService{
		APIKey: apiKey,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// GetCurrentWeather fetches current weather data for a given location
func (w *WeatherService) GetCurrentWeather(location, units string, apikey string) (*models.WeatherData, error) {
	if location == "" {
		return nil, fmt.Errorf("location cannot be empty")
	}

	if units == "" {
		units = DefaultUnits
	}

	// Build URL
	endpoint := fmt.Sprintf("%s%s", OpenWeatherMapBaseURL, CurrentWeatherEndpoint)
	params := url.Values{}
	params.Add("q", location)
	if apikey == "" {
		params.Add("appid", w.APIKey)
	} else {
		params.Add("appid", apikey)
	}
	params.Add("units", units)

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	// Make HTTP request
	resp, err := w.HTTPClient.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch weather data: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var owmResp models.OpenWeatherMapResponse
	if err := json.NewDecoder(resp.Body).Decode(&owmResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Convert to our internal model
	weatherData := w.convertCurrentWeatherResponse(owmResp)
	return weatherData, nil
}

// GetWeatherForecast fetches weather forecast data for a given location
func (w *WeatherService) GetWeatherForecast(location, units string, days int, apikey string) (*models.WeatherData, error) {
	if location == "" {
		return nil, fmt.Errorf("location cannot be empty")
	}

	if units == "" {
		units = DefaultUnits
	}

	if days <= 0 || days > 5 {
		days = 5 // OpenWeatherMap free tier supports up to 5 days
	}

	// Build URL
	endpoint := fmt.Sprintf("%s%s", OpenWeatherMapBaseURL, ForecastEndpoint)
	params := url.Values{}
	params.Add("q", location)
	if apikey == "" {
		params.Add("appid", w.APIKey)
	} else {
		params.Add("appid", apikey)
	}
	params.Add("units", units)

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	// Make HTTP request
	resp, err := w.HTTPClient.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch forecast data: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var owmResp models.OpenWeatherMapForecastResponse
	if err := json.NewDecoder(resp.Body).Decode(&owmResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Convert to our internal model
	weatherData := w.convertForecastResponse(owmResp, days)
	return weatherData, nil
}

// convertCurrentWeatherResponse converts OpenWeatherMap response to our internal model
func (w *WeatherService) convertCurrentWeatherResponse(owm models.OpenWeatherMapResponse) *models.WeatherData {
	var condition, description, icon string
	if len(owm.Weather) > 0 {
		condition = owm.Weather[0].Main
		description = owm.Weather[0].Description
		icon = owm.Weather[0].Icon
	}

	return &models.WeatherData{
		Location: models.Location{
			Name:      owm.Name,
			Country:   owm.Sys.Country,
			Latitude:  owm.Coord.Lat,
			Longitude: owm.Coord.Lon,
		},
		Current: models.Current{
			Temperature:   owm.Main.Temp,
			FeelsLike:     owm.Main.FeelsLike,
			Humidity:      owm.Main.Humidity,
			Pressure:      float64(owm.Main.Pressure),
			WindSpeed:     owm.Wind.Speed,
			WindDirection: owm.Wind.Deg,
			WindGust:      owm.Wind.Gust,
			Visibility:    float64(owm.Visibility),
			Condition:     condition,
			Description:   strings.Title(description),
			Icon:          icon,
			CloudCover:    owm.Clouds.All,
			LastUpdated:   time.Unix(owm.Dt, 0),
		},
		RequestTime: time.Now(),
	}
}

// convertForecastResponse converts OpenWeatherMap forecast response to our internal model
func (w *WeatherService) convertForecastResponse(owm models.OpenWeatherMapForecastResponse, days int) *models.WeatherData {
	// Group forecast items by date
	forecastMap := make(map[string][]models.ForecastItem)

	for _, item := range owm.List {
		date := time.Unix(item.Dt, 0).Format("2006-01-02")
		forecastMap[date] = append(forecastMap[date], item)
	}

	// Convert to daily forecasts
	var forecasts []models.Forecast
	count := 0

	for date, items := range forecastMap {
		if count >= days {
			break
		}

		// Calculate daily averages/extremes
		forecast := w.calculateDailyForecast(date, items)
		forecasts = append(forecasts, forecast)
		count++
	}

	return &models.WeatherData{
		Location: models.Location{
			Name:      owm.City.Name,
			Country:   owm.City.Country,
			Latitude:  owm.City.Coord.Lat,
			Longitude: owm.City.Coord.Lon,
		},
		Forecast:    forecasts,
		RequestTime: time.Now(),
	}
}

// calculateDailyForecast calculates daily forecast from 3-hour intervals
func (w *WeatherService) calculateDailyForecast(dateStr string, items []models.ForecastItem) models.Forecast {
	date, _ := time.Parse("2006-01-02", dateStr)

	if len(items) == 0 {
		return models.Forecast{Date: date}
	}

	var minTemp, maxTemp, avgTemp, totalTemp float64
	var totalHumidity, totalWind float64
	var condition, description, icon string
	var precipitation float64

	minTemp = items[0].Main.TempMin
	maxTemp = items[0].Main.TempMax

	for i, item := range items {
		if item.Main.TempMin < minTemp {
			minTemp = item.Main.TempMin
		}
		if item.Main.TempMax > maxTemp {
			maxTemp = item.Main.TempMax
		}

		totalTemp += item.Main.Temp
		totalHumidity += float64(item.Main.Humidity)
		totalWind += item.Wind.Speed

		if item.Rain.ThreeHour > 0 {
			precipitation += item.Rain.ThreeHour
		}
		if item.Snow.ThreeHour > 0 {
			precipitation += item.Snow.ThreeHour
		}

		// Use the middle of the day for main condition
		if i == len(items)/2 && len(item.Weather) > 0 {
			condition = item.Weather[0].Main
			description = item.Weather[0].Description
			icon = item.Weather[0].Icon
		}
	}

	count := float64(len(items))
	avgTemp = totalTemp / count

	return models.Forecast{
		Date:          date,
		MaxTemp:       maxTemp,
		MinTemp:       minTemp,
		AvgTemp:       avgTemp,
		Condition:     condition,
		Description:   strings.Title(description),
		Icon:          icon,
		Humidity:      int(totalHumidity / count),
		WindSpeed:     totalWind / count,
		Precipitation: precipitation,
	}
}
