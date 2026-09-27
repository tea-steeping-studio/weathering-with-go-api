package models

// OpenWeatherMapResponse represents the response from OpenWeatherMap API
type OpenWeatherMapResponse struct {
	Coord      Coordinates `json:"coord"`
	Weather    []Weather   `json:"weather"`
	Base       string      `json:"base"`
	Main       Main        `json:"main"`
	Visibility int         `json:"visibility"`
	Wind       Wind        `json:"wind"`
	Clouds     Clouds      `json:"clouds"`
	Rain       Rain        `json:"rain,omitempty"`
	Snow       Snow        `json:"snow,omitempty"`
	Dt         int64       `json:"dt"`
	Sys        Sys         `json:"sys"`
	ID         int         `json:"id"`
	Name       string      `json:"name"`
	Cod        int         `json:"cod"`
}

// Coordinates represents geographical coordinates
type Coordinates struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

// Weather represents weather condition information
type Weather struct {
	ID          int    `json:"id"`
	Main        string `json:"main"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}

// Main represents main weather parameters
type Main struct {
	Temp      float64 `json:"temp"`
	FeelsLike float64 `json:"feels_like"`
	TempMin   float64 `json:"temp_min"`
	TempMax   float64 `json:"temp_max"`
	Pressure  int     `json:"pressure"`
	Humidity  int     `json:"humidity"`
	SeaLevel  int     `json:"sea_level,omitempty"`
	GrndLevel int     `json:"grnd_level,omitempty"`
	TempKF    float64 `json:"temp_kf"`
}

// Wind represents wind information
type Wind struct {
	Speed float64 `json:"speed"`
	Deg   int     `json:"deg"`
	Gust  float64 `json:"gust,omitempty"`
}

// Clouds represents cloud information
type Clouds struct {
	All int `json:"all"`
}

// Rain represents precipitation information
type Rain struct {
	OneHour   float64 `json:"1h,omitempty"`
	ThreeHour float64 `json:"3h,omitempty"`
}

// Snow represents snow information
type Snow struct {
	OneHour   float64 `json:"1h,omitempty"`
	ThreeHour float64 `json:"3h,omitempty"`
}

// Sys represents system information
type Sys struct {
	Type    int    `json:"type,omitempty"`
	ID      int    `json:"id,omitempty"`
	Country string `json:"country"`
	Sunrise int64  `json:"sunrise"`
	Sunset  int64  `json:"sunset"`
}

// OpenWeatherMapForecastResponse represents the 5-day forecast response.
// Message is a float because that is what the endpoint sends: it carries the
// calculation time, such as 0.0117, and a JSON float cannot decode into an int,
// so an int here fails the whole body and the route answers 500 on a real
// response. It is not a status code, and it is not the cod member, which is the
// string above.
type OpenWeatherMapForecastResponse struct {
	Cod     string         `json:"cod"`
	Message float64        `json:"message"`
	Cnt     int            `json:"cnt"`
	List    []ForecastItem `json:"list"`
	City    City           `json:"city"`
}

// ForecastItem represents a single forecast item
type ForecastItem struct {
	Dt         int64       `json:"dt"`
	Main       Main        `json:"main"`
	Weather    []Weather   `json:"weather"`
	Clouds     Clouds      `json:"clouds"`
	Wind       Wind        `json:"wind"`
	Rain       Rain        `json:"rain,omitempty"`
	Snow       Snow        `json:"snow,omitempty"`
	Sys        ForecastSys `json:"sys"`
	DtTxt      string      `json:"dt_txt"`
	Pop        float64     `json:"pop"`
	Visibility int         `json:"visibility"`
}

// ForecastSys represents forecast system information
type ForecastSys struct {
	Pod string `json:"pod"`
}

// City represents city information in forecast response
type City struct {
	ID         int         `json:"id"`
	Name       string      `json:"name"`
	Coord      Coordinates `json:"coord"`
	Country    string      `json:"country"`
	Population int         `json:"population"`
	Timezone   int         `json:"timezone"`
	Sunrise    int64       `json:"sunrise"`
	Sunset     int64       `json:"sunset"`
}

// GeocodingResult represents a single OpenWeatherMap geocoding result
type GeocodingResult struct {
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	Country string  `json:"country"`
	State   string  `json:"state"`
}

// OneCallResponse represents the One Call 3.0 response
type OneCallResponse struct {
	Lat            float64         `json:"lat"`
	Lon            float64         `json:"lon"`
	Timezone       string          `json:"timezone"`
	TimezoneOffset int             `json:"timezone_offset"`
	Current        *OneCallCurrent `json:"current"`
	Minutely       []Minutely      `json:"minutely"`
	Hourly         []Hourly        `json:"hourly"`
	Daily          []DailyForecast `json:"daily"`
	Alerts         []Alert         `json:"alerts"`
}

// OneCallCurrent represents the current conditions block of the One Call 3.0 response.
// It is a pointer on OneCallResponse because the block is absent on some responses.
type OneCallCurrent struct {
	Dt         int64     `json:"dt"`
	Sunrise    int64     `json:"sunrise"`
	Sunset     int64     `json:"sunset"`
	Temp       float64   `json:"temp"`
	FeelsLike  float64   `json:"feels_like"`
	Pressure   int       `json:"pressure"`
	Humidity   int       `json:"humidity"`
	DewPoint   float64   `json:"dew_point"`
	Uvi        float64   `json:"uvi"`
	Clouds     int       `json:"clouds"`
	Visibility int       `json:"visibility"`
	WindSpeed  float64   `json:"wind_speed"`
	WindDeg    int       `json:"wind_deg"`
	WindGust   float64   `json:"wind_gust"`
	Weather    []Weather `json:"weather"`
}

// Minutely represents one entry from the One Call 3.0 minutely forecast
type Minutely struct {
	Dt            int64   `json:"dt"`
	Precipitation float64 `json:"precipitation"`
}

// Hourly represents one entry from the One Call 3.0 hourly forecast
type Hourly struct {
	Dt         int64     `json:"dt"`
	Sunrise    int64     `json:"sunrise"`
	Sunset     int64     `json:"sunset"`
	Temp       float64   `json:"temp"`
	FeelsLike  float64   `json:"feels_like"`
	Pressure   int       `json:"pressure"`
	Humidity   int       `json:"humidity"`
	DewPoint   float64   `json:"dew_point"`
	Uvi        float64   `json:"uvi"`
	Clouds     int       `json:"clouds"`
	Visibility int       `json:"visibility"`
	WindSpeed  float64   `json:"wind_speed"`
	WindDeg    int       `json:"wind_deg"`
	WindGust   float64   `json:"wind_gust"`
	Pop        float64   `json:"pop"`
	Rain       float64   `json:"rain"`
	Snow       float64   `json:"snow"`
	Weather    []Weather `json:"weather"`
}

// DailyForecast represents one day from the One Call 3.0 daily forecast
type DailyForecast struct {
	Dt        int64          `json:"dt"`
	Sunrise   int64          `json:"sunrise"`
	Sunset    int64          `json:"sunset"`
	Moonrise  int64          `json:"moonrise"`
	Moonset   int64          `json:"moonset"`
	MoonPhase float64        `json:"moon_phase"`
	Temp      DailyTemp      `json:"temp"`
	FeelsLike DailyFeelsLike `json:"feels_like"`
	Pressure  int            `json:"pressure"`
	Humidity  int            `json:"humidity"`
	DewPoint  float64        `json:"dew_point"`
	WindSpeed float64        `json:"wind_speed"`
	WindDeg   int            `json:"wind_deg"`
	WindGust  float64        `json:"wind_gust"`
	Weather   []Weather      `json:"weather"`
	Clouds    int            `json:"clouds"`
	Pop       float64        `json:"pop"`
	Rain      float64        `json:"rain"`
	Snow      float64        `json:"snow"`
	Uvi       float64        `json:"uvi"`
}

// DailyTemp represents the temperature breakdown for a forecast day
type DailyTemp struct {
	Day   float64 `json:"day"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Night float64 `json:"night"`
	Morn  float64 `json:"morn"`
	Eve   float64 `json:"eve"`
}

// DailyFeelsLike represents the apparent temperature breakdown for a forecast day
type DailyFeelsLike struct {
	Day   float64 `json:"day"`
	Night float64 `json:"night"`
	Eve   float64 `json:"eve"`
	Morn  float64 `json:"morn"`
}

// Alert represents one government weather alert from the One Call 3.0 response
type Alert struct {
	SenderName  string   `json:"sender_name"`
	Event       string   `json:"event"`
	Start       int64    `json:"start"`
	End         int64    `json:"end"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}
