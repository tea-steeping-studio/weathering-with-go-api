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

// OpenWeatherMapForecastResponse represents the 5-day forecast response
type OpenWeatherMapForecastResponse struct {
	Cod     string         `json:"cod"`
	Message int            `json:"message"`
	Cnt     int            `json:"cnt"`
	List    []ForecastItem `json:"list"`
	City    City           `json:"city"`
}

// ForecastItem represents a single forecast item
type ForecastItem struct {
	Dt      int64       `json:"dt"`
	Main    Main        `json:"main"`
	Weather []Weather   `json:"weather"`
	Clouds  Clouds      `json:"clouds"`
	Wind    Wind        `json:"wind"`
	Rain    Rain        `json:"rain,omitempty"`
	Snow    Snow        `json:"snow,omitempty"`
	Sys     ForecastSys `json:"sys"`
	DtTxt   string      `json:"dt_txt"`
}

// ForecastSys represents forecast system information
type ForecastSys struct {
	Pod string `json:"pod"`
}

// City represents city information in forecast response
type City struct {
	ID       int         `json:"id"`
	Name     string      `json:"name"`
	Coord    Coordinates `json:"coord"`
	Country  string      `json:"country"`
	Timezone int         `json:"timezone"`
	Sunrise  int64       `json:"sunrise"`
	Sunset   int64       `json:"sunset"`
}

