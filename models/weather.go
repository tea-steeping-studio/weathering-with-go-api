package models

import "time"

// Location represents geographical location information
type Location struct {
	Name      string  `json:"name"`
	Country   string  `json:"country"`
	Region    string  `json:"region,omitempty"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone,omitempty"`
}

// Current represents current weather conditions
type Current struct {
	Temperature   float64 `json:"temperature"`
	FeelsLike     float64 `json:"feels_like"`
	Humidity      int     `json:"humidity"`
	Pressure      float64 `json:"pressure"`
	Visibility    float64 `json:"visibility"`
	WindSpeed     float64 `json:"wind_speed"`
	WindDirection int     `json:"wind_direction"`
	WindGust      float64 `json:"wind_gust,omitempty"`
	Condition     string  `json:"condition"`
	Description   string  `json:"description"`
	Icon          string  `json:"icon"`
	// UVIndex       float64   `json:"uv_index"`
	MaxTemp     float64   `json:"max_temperature"`
	MinTemp     float64   `json:"min_temperature"`
	CloudCover  int       `json:"cloud_cover"`
	LastUpdated time.Time `json:"last_updated"`
}

// Forecast represents weather forecast for a specific day.
//
// This is the legacy vocabulary of the seven day route, and every reading on it is
// a pointer except the two that are not measurements. The route can fail to measure
// any of them: a daily entry the upstream sent without a temp block has no
// temperature, one sent without pop has no probability, and one sent without uvi has
// no ultraviolet index. As values those three cases would have been a row of zeroes,
// which reads as a forecast of exactly average weather rather than as a gap, so they
// are pointers and an unmeasurable day reports null. For a real body the JSON is byte
// identical, because a non-nil pointer to a value serialises as that value.
//
// Date, Condition, Description and Icon stay values: a timestamp and three strings
// the upstream sends whenever it sends a day. Precipitation stays a value for the
// reason ForecastDay.Precipitation does: a total of zero is a real answer for a day
// nothing fell on, not an absence, and the legacy key has always been a number.
type Forecast struct {
	Date          time.Time `json:"date"`
	MaxTemp       *float64  `json:"max_temperature"`
	MinTemp       *float64  `json:"min_temperature"`
	AvgTemp       *float64  `json:"avg_temperature"`
	Condition     string    `json:"condition"`
	Description   string    `json:"description"`
	Icon          string    `json:"icon"`
	Humidity      *int      `json:"humidity"`
	WindSpeed     *float64  `json:"wind_speed"`
	Precipitation float64   `json:"precipitation"`
	ChanceOfRain  *int      `json:"chance_of_rain"`
	UVIndex       *float64  `json:"uv_index"`
}

// WeatherRequest represents incoming API request parameters
type WeatherRequest struct {
	Location string `json:"location" form:"location" binding:"required"`
	Days     int    `json:"days,omitempty" form:"days"`
	Units    string `json:"units,omitempty" form:"units"` // metric, imperial, kelvin
	Keys     string `json:"keys,omitempty" form:"keys"`
}

// ErrorResponse represents API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// APIResponse represents a generic API response wrapper
type APIResponse struct {
	Success bool           `json:"success"`
	Data    interface{}    `json:"data,omitempty"`
	Error   *ErrorResponse `json:"error,omitempty"`
}
