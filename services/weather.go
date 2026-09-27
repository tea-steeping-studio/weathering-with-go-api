package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"weathering-with-go/models"
)

const (
	OpenWeatherMapBaseURL  = "https://api.openweathermap.org/data/2.5"
	CurrentWeatherEndpoint = "/weather"
	ForecastEndpoint       = "/forecast"
	GeocodingBaseURL       = "https://api.openweathermap.org/geo/1.0"
	GeocodingEndpoint      = "/direct"
	OneCallBaseURL         = "https://api.openweathermap.org/data/3.0"
	OneCallEndpoint        = "/onecall"
	OneCallExcludes        = "minutely,hourly,alerts"
	SevenDayCount          = 7
	DefaultUnits           = "metric"
	DefaultTimeout         = 10 * time.Second
)

// WeatherService handles weather data operations
type WeatherService struct {
	APIKey     string
	HTTPClient *http.Client

	now   func() time.Time
	mu    sync.RWMutex
	cache map[string]cacheEntry
	group singleflight.Group
}

// NewWeatherService creates a new weather service instance
func NewWeatherService(apiKey string) *WeatherService {
	return &WeatherService{
		APIKey: apiKey,
		HTTPClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		now:   time.Now,
		cache: make(map[string]cacheEntry),
	}
}

// GetCurrentWeather fetches current weather data for a given location
func (w *WeatherService) GetCurrentWeather(location, units, apikey string) (*models.CurrentWeatherResponse, bool, error) {
	if location == "" {
		return nil, false, fmt.Errorf("location cannot be empty")
	}

	if units == "" {
		units = DefaultUnits
	}

	key := weatherCacheKey(apikey, currentCacheEndpoint, location, units, 0)
	return cachedFetchTracked(w, key, func() (*models.CurrentWeatherResponse, error) {
		return w.fetchCurrentWeather(location, units, apikey)
	})
}

func (w *WeatherService) fetchCurrentWeather(location, units, apikey string) (*models.CurrentWeatherResponse, error) {
	// Build URL
	endpoint := fmt.Sprintf("%s%s", OpenWeatherMapBaseURL, CurrentWeatherEndpoint)
	params := url.Values{}
	params.Add("q", location)
	params.Add("appid", w.resolveAPIKey(apikey))
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

	// Read the body once and decode it twice: once into the upstream structs and
	// once into the pointer view of the members they cannot express, which is
	// where a measured zero and an absent member are told apart. This is one
	// upstream request, not two.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Parse response
	var owmResp models.OpenWeatherMapResponse
	if err := json.Unmarshal(body, &owmResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	var payload models.CurrentWeatherPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Convert to our internal model
	weatherData := w.mapCurrentWeather(owmResp, payload)
	return weatherData, nil
}

// GetWeatherForecast fetches weather forecast data for a given location
func (w *WeatherService) GetWeatherForecast(location, units string, days int, apikey string) (*models.WeatherData, bool, error) {
	if location == "" {
		return nil, false, fmt.Errorf("location cannot be empty")
	}

	if units == "" {
		units = DefaultUnits
	}

	if days <= 0 || days > 5 {
		days = 5 // OpenWeatherMap free tier supports up to 5 days
	}

	key := weatherCacheKey(apikey, forecastCacheEndpoint, location, units, days)
	return cachedFetchTracked(w, key, func() (*models.WeatherData, error) {
		return w.fetchWeatherForecast(location, units, days, apikey)
	})
}

func (w *WeatherService) fetchWeatherForecast(location, units string, days int, apikey string) (*models.WeatherData, error) {
	// Build URL
	endpoint := fmt.Sprintf("%s%s", OpenWeatherMapBaseURL, ForecastEndpoint)
	params := url.Values{}
	params.Add("q", location)
	params.Add("appid", w.resolveAPIKey(apikey))
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

// GetSevenDayForecast fetches a seven day daily forecast for a location.
// The location is geocoded first because One Call 3.0 works on coordinates.
func (w *WeatherService) GetSevenDayForecast(location, units, apikey string) (*models.WeatherData, bool, error) {
	if location == "" {
		return nil, false, fmt.Errorf("location cannot be empty")
	}

	if units == "" {
		units = DefaultUnits
	}

	key := weatherCacheKey(apikey, sevenDayCacheEndpoint, location, units, SevenDayCount)
	return cachedFetchTracked(w, key, func() (*models.WeatherData, error) {
		loc, err := w.geocodeLocation(location, apikey)
		if err != nil {
			return nil, err
		}
		return w.fetchSevenDayForecast(loc, units, apikey)
	})
}

// geocodeLocation resolves a location name to coordinates, caching the result.
func (w *WeatherService) geocodeLocation(location, apikey string) (*models.Location, error) {
	key := weatherCacheKey(apikey, geocodeCacheEndpoint, location, "", 0)
	loc, err := cachedFetch(w, key, func() (*models.Location, error) {
		return w.fetchGeocodedLocation(location, apikey)
	})
	return loc, err
}

func (w *WeatherService) fetchGeocodedLocation(location, apikey string) (*models.Location, error) {
	params := url.Values{}
	params.Add("q", location)
	params.Add("limit", "1")
	params.Add("appid", w.resolveAPIKey(apikey))

	fullURL := fmt.Sprintf("%s%s?%s", GeocodingBaseURL, GeocodingEndpoint, params.Encode())

	resp, err := w.HTTPClient.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to geocode %q: %w", location, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var results []models.GeocodingResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to parse geocoding response: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("API request failed with status 404: location %q not found", location)
	}

	result := results[0]
	return &models.Location{
		Name:      result.Name,
		Country:   result.Country,
		Region:    result.State,
		Latitude:  result.Lat,
		Longitude: result.Lon,
	}, nil
}

func (w *WeatherService) fetchSevenDayForecast(loc *models.Location, units, apikey string) (*models.WeatherData, error) {
	params := url.Values{}
	params.Add("lat", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
	params.Add("lon", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
	params.Add("units", units)
	params.Add("exclude", OneCallExcludes)
	params.Add("appid", w.resolveAPIKey(apikey))

	fullURL := fmt.Sprintf("%s%s?%s", OneCallBaseURL, OneCallEndpoint, params.Encode())

	resp, err := w.HTTPClient.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch forecast data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var owmResp models.OneCallResponse
	if err := json.NewDecoder(resp.Body).Decode(&owmResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	return w.convertOneCallResponse(owmResp, loc), nil
}

// convertOneCallResponse converts the One Call 3.0 daily forecast to our internal model
func (w *WeatherService) convertOneCallResponse(owm models.OneCallResponse, loc *models.Location) *models.WeatherData {
	forecasts := make([]models.Forecast, 0, SevenDayCount)
	for i, day := range owm.Daily {
		if i >= SevenDayCount {
			break
		}

		var condition, description, icon string
		if len(day.Weather) > 0 {
			condition = day.Weather[0].Main
			description = day.Weather[0].Description
			icon = day.Weather[0].Icon
		}

		forecasts = append(forecasts, models.Forecast{
			Date:          time.Unix(day.Dt, 0),
			MaxTemp:       day.Temp.Max,
			MinTemp:       day.Temp.Min,
			AvgTemp:       day.Temp.Day,
			Condition:     condition,
			Description:   strings.Title(description),
			Icon:          icon,
			Humidity:      day.Humidity,
			WindSpeed:     day.WindSpeed,
			Precipitation: day.Rain + day.Snow,
			ChanceOfRain:  int(day.Pop * 100),
			UVIndex:       day.Uvi,
		})
	}

	return &models.WeatherData{
		Location:    *loc,
		Current:     currentFromOneCall(owm.Current),
		Forecast:    forecasts,
		RequestTime: w.now(),
	}
}

// currentFromOneCall maps the One Call current block onto our model. The block is
// absent on some responses, in which case the zero value is returned.
func currentFromOneCall(current *models.OneCallCurrent) models.Current {
	if current == nil {
		return models.Current{}
	}

	condition, description, icon := "", "", ""
	if len(current.Weather) > 0 {
		condition = current.Weather[0].Main
		description = current.Weather[0].Description
		icon = current.Weather[0].Icon
	}

	return models.Current{
		Temperature:   current.Temp,
		FeelsLike:     current.FeelsLike,
		Humidity:      current.Humidity,
		Pressure:      float64(current.Pressure),
		Visibility:    float64(current.Visibility),
		WindSpeed:     current.WindSpeed,
		WindDirection: current.WindDeg,
		WindGust:      current.WindGust,
		Condition:     condition,
		Description:   strings.Title(description),
		Icon:          icon,
		CloudCover:    current.Clouds,
		LastUpdated:   time.Unix(current.Dt, 0),
	}
}

// resolveAPIKey prefers a caller supplied key over the server side key.
func (w *WeatherService) resolveAPIKey(apikey string) string {
	if apikey != "" {
		return apikey
	}
	return w.APIKey
}

// mapCurrentWeather builds the current route's response: a faithful mirror of
// the /data/2.5/weather schema plus the legacy vocabulary. Both are assigned in
// one struct literal from the same locals, so a legacy key cannot drift away from
// the faithful value it aliases. Values come from owm; presence, and therefore
// the difference between a measured zero and an absent member, comes from
// payload.
//
// A block the upstream did not send is left nil, so an absent block and a block
// whose members are all null stay two different things, and the window this
// endpoint does not document is omitted from a precipitation block. The legacy
// blocks keep their own omitempty, so an absent gust stays absent in
// current.wind_gust while the faithful wind.gust reports it null. The legacy
// current block's max_temperature and min_temperature stay at 0, as they have
// always been on this route: changing them is a change to the legacy vocabulary,
// not a faithfulness fix.
func (w *WeatherService) mapCurrentWeather(owm models.OpenWeatherMapResponse, payload models.CurrentWeatherPayload) *models.CurrentWeatherResponse {
	var condition, description, icon string
	if len(owm.Weather) > 0 {
		condition = owm.Weather[0].Main
		description = owm.Weather[0].Description
		icon = owm.Weather[0].Icon
	}

	name := owm.Name
	country := owm.Sys.Country
	lat, lon := owm.Coord.Lat, owm.Coord.Lon
	temp, feelsLike := owm.Main.Temp, owm.Main.FeelsLike
	tempMin, tempMax := owm.Main.TempMin, owm.Main.TempMax
	pressure, humidity := owm.Main.Pressure, owm.Main.Humidity
	visibility := owm.Visibility
	speed, deg, gust := owm.Wind.Speed, owm.Wind.Deg, owm.Wind.Gust
	cloudCover := owm.Clouds.All
	dt := owm.Dt
	sysType, sysID := owm.Sys.Type, owm.Sys.ID
	sunrise, sunset := owm.Sys.Sunrise, owm.Sys.Sunset

	var main *models.MainBlock
	if payload.Main != nil {
		main = &models.MainBlock{
			Temp:      &temp,
			FeelsLike: &feelsLike,
			TempMin:   &tempMin,
			TempMax:   &tempMax,
			Pressure:  &pressure,
			Humidity:  &humidity,
			SeaLevel:  payload.Main.SeaLevel,
			GrndLevel: payload.Main.GrndLevel,
			TempKF:    payload.Main.TempKF,
		}
	}
	var wind *models.WindBlock
	if payload.Wind != nil {
		wind = &models.WindBlock{
			Speed: &speed,
			Deg:   &deg,
			Gust:  payload.Wind.Gust,
		}
	}
	var clouds *models.CloudsBlock
	if payload.Clouds != nil {
		clouds = &models.CloudsBlock{All: &cloudCover}
	}
	var sys *models.SysBlock
	if payload.Sys != nil {
		sys = &models.SysBlock{
			Type:    &sysType,
			ID:      &sysID,
			Country: &country,
			Sunrise: &sunrise,
			Sunset:  &sunset,
		}
	}
	var rain *models.RainBlock
	if payload.Rain != nil {
		rain = &models.RainBlock{OneHour: payload.Rain.OneHour}
	}
	var snow *models.SnowBlock
	if payload.Snow != nil {
		snow = &models.SnowBlock{OneHour: payload.Snow.OneHour}
	}

	return &models.CurrentWeatherResponse{
		Coord:      models.Coordinates{Lon: lon, Lat: lat},
		Weather:    owm.Weather,
		Base:       owm.Base,
		Main:       main,
		Visibility: visibility,
		Wind:       wind,
		Clouds:     clouds,
		Rain:       rain,
		Snow:       snow,
		Dt:         dt,
		Sys:        sys,
		ID:         owm.ID,
		Timezone:   payload.Timezone,
		Name:       name,
		Cod:        owm.Cod,
		Location: models.Location{
			Name:      name,
			Country:   country,
			Latitude:  lat,
			Longitude: lon,
		},
		Current: models.Current{
			Temperature:   temp,
			FeelsLike:     feelsLike,
			Humidity:      humidity,
			Pressure:      float64(pressure),
			WindSpeed:     speed,
			WindDirection: deg,
			WindGust:      gust,
			Visibility:    float64(visibility),
			Condition:     condition,
			Description:   strings.Title(description),
			Icon:          icon,
			CloudCover:    cloudCover,
			LastUpdated:   time.Unix(dt, 0),
		},
		RequestTime: w.now(),
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

	// Walk the dates in chronological order so the returned days are deterministic.
	dates := make([]string, 0, len(forecastMap))
	for date := range forecastMap {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	// Convert to daily forecasts
	forecasts := make([]models.Forecast, 0, min(len(dates), days))
	for _, date := range dates {
		if len(forecasts) >= days {
			break
		}
		forecasts = append(forecasts, w.calculateDailyForecast(date, forecastMap[date]))
	}

	return &models.WeatherData{
		Location: models.Location{
			Name:      owm.City.Name,
			Country:   owm.City.Country,
			Latitude:  owm.City.Coord.Lat,
			Longitude: owm.City.Coord.Lon,
		},
		Current:     currentFromForecast(owm.List),
		Forecast:    forecasts,
		RequestTime: w.now(),
	}
}

// currentFromForecast maps the nearest forecast slot onto our model. The 5 day
// endpoint returns 3 hour slots rather than true current conditions, and it carries
// no visibility, so that field stays 0. The slot timestamp is reported as
// last_updated so callers can see how stale the reading is.
func currentFromForecast(items []models.ForecastItem) models.Current {
	if len(items) == 0 {
		return models.Current{}
	}

	// OpenWeatherMap returns list ascending from the current 3 hour boundary.
	nearest := items[0]

	condition, description, icon := "", "", ""
	if len(nearest.Weather) > 0 {
		condition = nearest.Weather[0].Main
		description = nearest.Weather[0].Description
		icon = nearest.Weather[0].Icon
	}

	return models.Current{
		Temperature:   nearest.Main.Temp,
		FeelsLike:     nearest.Main.FeelsLike,
		Humidity:      nearest.Main.Humidity,
		Pressure:      float64(nearest.Main.Pressure),
		WindSpeed:     nearest.Wind.Speed,
		WindDirection: nearest.Wind.Deg,
		WindGust:      nearest.Wind.Gust,
		Condition:     condition,
		Description:   strings.Title(description),
		Icon:          icon,
		MaxTemp:       nearest.Main.TempMax,
		MinTemp:       nearest.Main.TempMin,
		CloudCover:    nearest.Clouds.All,
		LastUpdated:   time.Unix(nearest.Dt, 0),
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
