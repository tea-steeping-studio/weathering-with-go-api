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

// Current represents current weather conditions.
//
// Every member is a pointer, and that is the one change this type has had since it
// was written. All three routes derive this block rather than observing it, and when
// the upstream sends no current block at all the old zero value reported a
// temperature of 0 degrees, a pressure of 0 hPa, a visibility of 0 metres and a
// last_updated of 1970: five fabrications and one invented date, beside a faithful
// block that correctly said null. The keys are all still there and a non-nil pointer
// to a value serialises as that value, so on a real body the JSON is byte identical
// with two exceptions: max_temperature and min_temperature are null on the two routes
// that never measured them, where they were a fabricated 0, and the members of a block
// the upstream did not send are null where they were 0 as well.
//
// WindGust keeps its omitempty, which on a pointer drops only nil. A measured gust
// of 0 m/s is a reading and is reported; an upstream that sent no gust leaves the key
// absent, as it always has.
type Current struct {
	Temperature   *float64 `json:"temperature"`
	FeelsLike     *float64 `json:"feels_like"`
	Humidity      *int     `json:"humidity"`
	Pressure      *float64 `json:"pressure"`
	Visibility    *float64 `json:"visibility"`
	WindSpeed     *float64 `json:"wind_speed"`
	WindDirection *int     `json:"wind_direction"`
	WindGust      *float64 `json:"wind_gust,omitempty"`
	Condition     *string  `json:"condition"`
	Description   *string  `json:"description"`
	Icon          *string  `json:"icon"`
	// UVIndex       float64   `json:"uv_index"`
	MaxTemp     *float64   `json:"max_temperature"`
	MinTemp     *float64   `json:"min_temperature"`
	CloudCover  *int       `json:"cloud_cover"`
	LastUpdated *time.Time `json:"last_updated"`
}

// Forecast represents weather forecast for a specific day.
//
// This is the legacy vocabulary of the seven day route, and every member of it is a
// pointer except Date. The route can fail to measure any of the others: a daily entry
// the upstream sent without a temp block has no temperature, one sent without pop has
// no probability, and one sent without uvi has no ultraviolet index. As values those
// cases would have been a row of zeroes, which reads as a forecast of exactly average
// weather rather than as a gap, so they are pointers and an unmeasurable day reports
// null. For a real body the JSON is byte identical, because a non-nil pointer to a
// value serialises as that value.
//
// Date is the one member that stays a value: it is a timestamp the upstream sends on
// every daily entry, and a time.Time marshals to the same string either way.
//
// Precipitation is a pointer, and it is the case that separates two things a value
// type cannot. A day whose upstream reported a rain volume of 0 and no snow has a
// total of 0 and that is an answer. A day whose upstream reported neither has no total
// at all, and reporting 0 for it says the same thing as a dry day with a measurement
// behind it, right beside rain: null and snow: null on the faithful twin. The pointer
// separates them: the first day reports 0, the second reports null.
//
// Condition, Description and Icon are pointers for the same reason currentFromOneCall
// nulls the same trio for the same upstream condition: a day sent with no weather
// entry has no condition, and "" is a value rule 2 says should be null. One route, one
// case, one rule.
type Forecast struct {
	Date          time.Time `json:"date"`
	MaxTemp       *float64  `json:"max_temperature"`
	MinTemp       *float64  `json:"min_temperature"`
	AvgTemp       *float64  `json:"avg_temperature"`
	Condition     *string   `json:"condition"`
	Description   *string   `json:"description"`
	Icon          *string   `json:"icon"`
	Humidity      *int      `json:"humidity"`
	WindSpeed     *float64  `json:"wind_speed"`
	Precipitation *float64  `json:"precipitation"`
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
