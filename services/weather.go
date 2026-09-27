package services

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
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
func (w *WeatherService) GetWeatherForecast(location, units string, days int, apikey string) (*models.ForecastResponse, bool, error) {
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
	return cachedFetchTracked(w, key, func() (*models.ForecastResponse, error) {
		return w.fetchWeatherForecast(location, units, days, apikey)
	})
}

func (w *WeatherService) fetchWeatherForecast(location, units string, days int, apikey string) (*models.ForecastResponse, error) {
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

	// Read the body once and decode it twice: once into the upstream structs and
	// once into the pointer view of the members they cannot express, which is where
	// a measured zero and an absent member are told apart. This is one upstream
	// request, not two.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Parse response
	var owmResp models.OpenWeatherMapForecastResponse
	if err := json.Unmarshal(body, &owmResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	var payload models.ForecastPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Convert to our internal model
	weatherData := w.mapForecast(owmResp, payload, days)
	return weatherData, nil
}

// GetSevenDayForecast fetches a seven day daily forecast for a location.
// The location is geocoded first because One Call 3.0 works on coordinates.
func (w *WeatherService) GetSevenDayForecast(location, units, apikey string) (*models.SevenDayResponse, bool, error) {
	if location == "" {
		return nil, false, fmt.Errorf("location cannot be empty")
	}

	if units == "" {
		units = DefaultUnits
	}

	key := weatherCacheKey(apikey, sevenDayCacheEndpoint, location, units, SevenDayCount)
	return cachedFetchTracked(w, key, func() (*models.SevenDayResponse, error) {
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

func (w *WeatherService) fetchSevenDayForecast(loc *models.Location, units, apikey string) (*models.SevenDayResponse, error) {
	params := url.Values{}
	params.Add("lat", strconv.FormatFloat(loc.Latitude, 'f', -1, 64))
	params.Add("lon", strconv.FormatFloat(loc.Longitude, 'f', -1, 64))
	params.Add("units", units)
	// No exclude parameter. Asking for minutely, hourly and alerts costs no extra
	// request and no extra quota on this endpoint, and dropping them is what left the
	// route unable to serve anything the 5 day route cannot.
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

	// Read the body once and decode it twice: once into the upstream structs and
	// once into the pointer view of the members they cannot express, which is where
	// a measured zero and an absent member are told apart. This is one upstream
	// request, not two.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	var owmResp models.OneCallResponse
	if err := json.Unmarshal(body, &owmResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	var payload models.SevenDayPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	return w.mapOneCall(owmResp, payload, loc), nil
}

// mapOneCall builds the seven day route's response: a faithful mirror of the
// /data/3.0/onecall body under the onecall key, and the legacy vocabulary beside it.
// Values come from owm, presence from payload.
//
// The faithful daily array is every day the upstream sent, because that is what the
// upstream measured. The legacy array is the first seven of them, because that cap is
// part of the contract consumers already read. The two are built from the same
// presence value for the same day, so a legacy reading and its faithful twin cannot
// disagree even where they overlap.
func (w *WeatherService) mapOneCall(owm models.OneCallResponse, payload models.SevenDayPayload, loc *models.Location) *models.SevenDayResponse {
	envelope := &models.OneCallEnvelope{
		Lat:            owm.Lat,
		Lon:            owm.Lon,
		Timezone:       owm.Timezone,
		TimezoneOffset: owm.TimezoneOffset,
		// The three opt-in blocks are carried as the upstream sent them. Whether a
		// caller sees one is a decision taken after this body is built, so the cached
		// response always holds them all and one upstream call serves every variant.
		Minutely: owm.Minutely,
		Hourly:   owm.Hourly,
		Alerts:   owm.Alerts,
	}

	if owm.Current != nil {
		current := oneCallCurrentPoint(*owm.Current, payload.Current)
		envelope.Current = &current
	}

	for i, day := range owm.Daily {
		envelope.Daily = append(envelope.Daily, oneCallDailyPoint(day, oneCallPresence(payload, i)))
	}

	forecasts := make([]models.Forecast, 0, min(len(owm.Daily), SevenDayCount))
	for i, day := range owm.Daily {
		if len(forecasts) >= SevenDayCount {
			break
		}
		forecasts = append(forecasts, oneCallLegacyDay(day, oneCallPresence(payload, i)))
	}

	return &models.SevenDayResponse{
		OneCall: envelope,
		// The legacy location is the geocoder's answer verbatim, as it has always
		// been, while onecall.lat and onecall.lon are the upstream's echo of the same
		// coordinates. Two sources for one pair of numbers, so they are read from
		// where each has always come from rather than being made to agree here.
		Location:    *loc,
		Current:     currentFromOneCall(owm.Current),
		Forecast:    forecasts,
		RequestTime: w.now(),
	}
}

// oneCallPresence is the presence view of the i-th daily entry. The two decodes walk
// one array in the same order, so the indexes line up; a payload shorter than the
// upstream array is not something one body can produce, and a missing presence view
// is treated as no presence rather than as a panic.
func oneCallPresence(payload models.SevenDayPayload, i int) models.SevenDayPayloadDaily {
	if i < len(payload.Daily) {
		return payload.Daily[i]
	}
	return models.SevenDayPayloadDaily{}
}

// oneCallCurrentPoint maps the upstream current block. The timestamps and the weather
// array are read from the decode struct, where the upstream documents them as sent
// and a zero is a real reading; every measurement comes from the payload, so an
// absent one is null. A nil presence view reports the block with all of them null
// rather than dropping the block, since the block itself was there.
func oneCallCurrentPoint(current models.OneCallCurrent, presence *models.SevenDayPayloadCurrent) models.OneCallCurrentPoint {
	dt, sunrise, sunset := current.Dt, current.Sunrise, current.Sunset

	point := models.OneCallCurrentPoint{
		Dt:      &dt,
		Sunrise: &sunrise,
		Sunset:  &sunset,
		Weather: current.Weather,
	}
	if presence == nil {
		return point
	}

	point.Temp = presence.Temp
	point.FeelsLike = presence.FeelsLike
	point.Pressure = presence.Pressure
	point.Humidity = presence.Humidity
	point.DewPoint = presence.DewPoint
	point.Uvi = presence.Uvi
	point.Clouds = presence.Clouds
	point.Visibility = presence.Visibility
	point.WindSpeed = presence.WindSpeed
	point.WindDeg = presence.WindDeg
	point.WindGust = presence.WindGust

	return point
}

// oneCallDailyPoint maps one upstream daily entry.
//
// The timestamps, the lunar members, the weather array, the pressure and the cloud
// cover are read from the decode struct: the upstream documents all of them as sent
// on every day, so their zeroes are readings. A moonrise of 0 is the moon not rising
// on that day at that latitude, which is a reading and not a gap.
//
// The two breakdown blocks are gated on the payload and filled from the decode
// struct, because their members are documented as unconditionally sent and the block
// itself is the only thing that can be missing. Everything else is taken from the
// payload, value and presence together, so a day the upstream could not measure
// reports null rather than a fabricated zero.
func oneCallDailyPoint(day models.DailyForecast, presence models.SevenDayPayloadDaily) models.OneCallDailyPoint {
	dt, sunrise, sunset := day.Dt, day.Sunrise, day.Sunset
	moonrise, moonset, moonPhase := day.Moonrise, day.Moonset, day.MoonPhase
	pressure, clouds := day.Pressure, day.Clouds

	point := models.OneCallDailyPoint{
		Dt:        &dt,
		Sunrise:   &sunrise,
		Sunset:    &sunset,
		Moonrise:  &moonrise,
		Moonset:   &moonset,
		MoonPhase: &moonPhase,
		Pressure:  &pressure,
		Clouds:    &clouds,
		Weather:   day.Weather,

		Humidity:  presence.Humidity,
		DewPoint:  presence.DewPoint,
		WindSpeed: presence.WindSpeed,
		WindDeg:   presence.WindDeg,
		WindGust:  presence.WindGust,
		Pop:       presence.Pop,
		Rain:      presence.Rain,
		Snow:      presence.Snow,
		Uvi:       presence.Uvi,
	}

	if presence.Temp != nil {
		dayTemp, min, max := day.Temp.Day, day.Temp.Min, day.Temp.Max
		night, morn, eve := day.Temp.Night, day.Temp.Morn, day.Temp.Eve
		point.Temp = &models.TempPoint{
			Day:   &dayTemp,
			Min:   &min,
			Max:   &max,
			Night: &night,
			Morn:  &morn,
			Eve:   &eve,
		}
	}
	if presence.FeelsLike != nil {
		dayFeels, night, morn, eve := day.FeelsLike.Day, day.FeelsLike.Night, day.FeelsLike.Morn, day.FeelsLike.Eve
		point.FeelsLike = &models.FeelsLikePoint{
			Day:   &dayFeels,
			Night: &night,
			Morn:  &morn,
			Eve:   &eve,
		}
	}

	return point
}

// oneCallLegacyDay builds the legacy day from the same upstream entry and the same
// presence value the faithful day is built from, so the two vocabularies report one
// set of readings between them.
//
// Every reading the route may be unable to measure is a pointer on models.Forecast,
// which is what keeps a day the upstream sent without a temp, pop or uvi from
// becoming a row of zeroes. The condition, description and icon come from the first
// weather entry, as they always have. Precipitation is the sum of the two volumes the
// upstream reported, and a day that reported neither is a total of 0 rather than a
// null, because nothing falling on a day is an answer and not an absence.
func oneCallLegacyDay(day models.DailyForecast, presence models.SevenDayPayloadDaily) models.Forecast {
	var condition, description, icon string
	if len(day.Weather) > 0 {
		condition = day.Weather[0].Main
		description = day.Weather[0].Description
		icon = day.Weather[0].Icon
	}

	legacy := models.Forecast{
		Date:        time.Unix(day.Dt, 0),
		Condition:   condition,
		Description: strings.Title(description),
		Icon:        icon,
		Humidity:    presence.Humidity,
		WindSpeed:   presence.WindSpeed,
		// The ultraviolet index is the day's uvi when the upstream sent one and null
		// when it did not, the same rule the faithful day follows and the opposite of
		// the permanent 0 this key used to carry.
		UVIndex: presence.Uvi,
	}

	if presence.Temp != nil {
		max, min, avg := day.Temp.Max, day.Temp.Min, day.Temp.Day
		legacy.MaxTemp = &max
		legacy.MinTemp = &min
		legacy.AvgTemp = &avg
	}

	if presence.Pop != nil {
		// Rounded, and not truncated, because a truncation reports a probability the
		// upstream never gave: 0.29 is 28.999999999999996 in binary floating point, so
		// a plain int() call would answer 28 for a day the upstream called 29 percent.
		// The five day route rounds the same way, and a probability that reads
		// differently on two routes is a bug whichever route is right.
		chanceOfRain := int(math.Round(*presence.Pop * 100))
		legacy.ChanceOfRain = &chanceOfRain
	}

	if presence.Rain != nil {
		legacy.Precipitation += *presence.Rain
	}
	if presence.Snow != nil {
		legacy.Precipitation += *presence.Snow
	}

	return legacy
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

// mapForecast builds the forecast route's response: a faithful mirror of the
// /data/2.5/forecast envelope, the daily rollup this route has always reported, and
// the raw three hour slots behind each day. Values come from owm, presence from
// payload, and every key is assigned in one struct literal from the same locals, so
// a legacy key cannot drift away from the faithful value it aliases.
//
// The legacy current block is still derived from list[0] rather than observed; see
// currentFromForecast. Filling it costs no upstream call, and the route must not
// start making one.
func (w *WeatherService) mapForecast(owm models.OpenWeatherMapForecastResponse, payload models.ForecastPayload, days int) *models.ForecastResponse {
	// Group by the UTC date of a slot's timestamp. The two decodes read the same
	// array in the same order, so index i of payload.List is the presence view of
	// owm.List[i].
	//
	// The .UTC() is load bearing. time.Unix returns local time, so without it the
	// same upstream body yields a different number of days depending on where the
	// process runs: a slot at 23:00 UTC and one at 01:00 UTC the next morning are
	// two days in one zone and one in another. The city's own offset is not applied
	// either, so a London day is a UTC day; that is a separate question from this
	// one and is not settled here.
	slotsByDate := make(map[string][]models.ForecastSlot)
	for i, item := range owm.List {
		var presence models.ForecastPayloadItem
		if i < len(payload.List) {
			presence = payload.List[i]
		}
		date := time.Unix(item.Dt, 0).UTC().Format("2006-01-02")
		slotsByDate[date] = append(slotsByDate[date], mapForecastSlot(item, presence))
	}

	// Walk the dates in chronological order so the returned days are deterministic.
	dates := make([]string, 0, len(slotsByDate))
	for date := range slotsByDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	// Convert to daily forecasts. days takes the earliest days, as it always has.
	forecasts := make([]models.ForecastDay, 0, min(len(dates), days))
	for _, date := range dates {
		if len(forecasts) >= days {
			break
		}
		forecasts = append(forecasts, forecastDay(date, slotsByDate[date]))
	}

	// The faithful city block and the legacy location read the same four members.
	name, country := owm.City.Name, owm.City.Country
	lat, lon := owm.City.Coord.Lat, owm.City.Coord.Lon
	city := owm.City
	var cityBlock *models.City
	if payload.City != nil {
		cityBlock = &city
	}

	return &models.ForecastResponse{
		Cod:     owm.Cod,
		Message: owm.Message,
		Cnt:     owm.Cnt,
		City:    cityBlock,
		Location: models.Location{
			Name:      name,
			Country:   country,
			Latitude:  lat,
			Longitude: lon,
		},
		Current:     currentFromForecast(owm.List),
		Forecast:    forecasts,
		RequestTime: w.now(),
	}
}

// mapForecastSlot maps one raw three hour slot. A block the upstream did not send
// stays nil, so an absent block, a block whose members are all null and a block
// measuring zero are three different things, exactly as on the current route.
func mapForecastSlot(item models.ForecastItem, presence models.ForecastPayloadItem) models.ForecastSlot {
	slot := models.ForecastSlot{
		Dt:      item.Dt,
		Weather: item.Weather,
		DtTxt:   item.DtTxt,
		// pop and visibility are read from the payload rather than the upstream
		// struct. The endpoint does not send pop on every slot, and reading its zero
		// from a non-pointer field would report a probability nobody measured.
		Pop:        presence.Pop,
		Visibility: presence.Visibility,
	}

	temp, feelsLike := item.Main.Temp, item.Main.FeelsLike
	tempMin, tempMax := item.Main.TempMin, item.Main.TempMax
	pressure, humidity := item.Main.Pressure, item.Main.Humidity
	if presence.Main != nil {
		slot.Main = &models.MainBlock{
			Temp:      &temp,
			FeelsLike: &feelsLike,
			TempMin:   &tempMin,
			TempMax:   &tempMax,
			Pressure:  &pressure,
			Humidity:  &humidity,
			SeaLevel:  presence.Main.SeaLevel,
			GrndLevel: presence.Main.GrndLevel,
			// TempKF stays null: /data/2.5/forecast does not document it for a slot.
		}
	}

	speed, deg := item.Wind.Speed, item.Wind.Deg
	if presence.Wind != nil {
		slot.Wind = &models.WindBlock{Speed: &speed, Deg: &deg, Gust: presence.Wind.Gust}
	}

	if presence.Clouds != nil {
		cloudCover := item.Clouds.All
		slot.Clouds = &models.CloudsBlock{All: &cloudCover}
	}

	// The forecast block types carry only the 3h window, so there is no 1h key to
	// report: this endpoint does not document it. The window has no omitempty
	// because here it is documented, so a block upstream sent empty reports null
	// and a measured zero reports 0.
	if presence.Rain != nil {
		slot.Rain = &models.ForecastRainBlock{ThreeHour: presence.Rain.ThreeHour}
	}
	if presence.Snow != nil {
		slot.Snow = &models.ForecastSnowBlock{ThreeHour: presence.Snow.ThreeHour}
	}
	if presence.Sys != nil {
		sys := item.Sys
		slot.Sys = &sys
	}

	return slot
}

// forecastDay rolls one UTC date of slots up into the day the route reports.
//
// Nearly everything a day carries is a derivation, because the three hour endpoint
// reports per slot and nothing per day. Both vocabularies are assigned from the
// same accumulators, so chance_of_rain cannot drift away from pop and
// max_temperature cannot drift away from temp.max.
//
// A mean is taken over the slots that carry the member, not over every slot: a day
// whose first slot reports no pop has a probability stated over the slots that have
// one, and saying null would lose the reading entirely.
//
// dateStr and slots always come from mapForecast's grouping, so slots is never
// empty: a day is created from a slot, never the other way round.
func forecastDay(dateStr string, slots []models.ForecastSlot) models.ForecastDay {
	date, _ := time.Parse("2006-01-02", dateStr)
	day := models.ForecastDay{Date: date, Hourly: slots}
	// The endpoint reports no feels_like breakdown at all, so the block is present
	// with every member null. Its presence must not depend on whether the slots
	// carried a main block: the key is documented, the readings are not.
	day.FeelsLike = &models.FeelsLikePoint{}

	var (
		tempSum, tempMin, tempMax float64
		humiditySum, pressureSum  float64
		windSum, fastest, gustMax float64
		cloudSum, visibilitySum   float64
		popSum, popMin, popMax    float64
		rainSum, snowSum          float64
		tempCount, humidityCount  int
		pressureCount             int
		windCount, cloudCount     int
		visibilityCount, popCount int
		windDeg                   int
		tempSeen, fastestSeen     bool
		gustSeen, popSeen         bool
		rainSeen, snowSeen        bool
	)

	for _, slot := range slots {
		if main := slot.Main; main != nil {
			temp := *main.Temp
			if !tempSeen || temp < tempMin {
				tempMin = temp
			}
			if !tempSeen || temp > tempMax {
				tempMax = temp
			}
			tempSum += temp
			tempSeen = true
			tempCount++

			humiditySum += float64(*main.Humidity)
			humidityCount++

			pressureSum += float64(*main.Pressure)
			pressureCount++
		}

		if wind := slot.Wind; wind != nil {
			speed := *wind.Speed
			windSum += speed
			windCount++
			// deg is the direction relevant to the maximum wind speed, which is what
			// the daily endpoints document it as, so the day reports the direction of
			// its fastest slot rather than a mean of headings.
			if !fastestSeen || speed > fastest {
				fastest = speed
				windDeg = *wind.Deg
				fastestSeen = true
			}
			if wind.Gust != nil && (!gustSeen || *wind.Gust > gustMax) {
				gustMax = *wind.Gust
				gustSeen = true
			}
		}

		if clouds := slot.Clouds; clouds != nil {
			cloudSum += float64(*clouds.All)
			cloudCount++
		}

		if slot.Visibility != nil {
			visibilitySum += float64(*slot.Visibility)
			visibilityCount++
		}

		if slot.Pop != nil {
			pop := *slot.Pop
			if !popSeen || pop > popMax {
				popMax = pop
			}
			if !popSeen || pop < popMin {
				popMin = pop
			}
			popSum += pop
			popSeen = true
			popCount++
		}

		if rain := slot.Rain; rain != nil && rain.ThreeHour != nil {
			rainSum += *rain.ThreeHour
			rainSeen = true
		}

		if snow := slot.Snow; snow != nil && snow.ThreeHour != nil {
			snowSum += *snow.ThreeHour
			snowSeen = true
		}
	}

	if tempSeen {
		avgTemp := tempSum / float64(tempCount)
		day.AvgTemp = &avgTemp
		// Derived, not measured: the extremes of the day's slot temperatures. A
		// slot's own main.temp_min and main.temp_max are a different measurement, the
		// upstream's own three hour window, and the two are not interchangeable.
		// The three hour endpoint has no time of day breakdown, no feels_like
		// breakdown and no uvi, so those members stay null.
		day.Temp = &models.TempPoint{Min: &tempMin, Max: &tempMax}
		// The legacy aliases of the same two numbers, so a consumer of the old
		// response and a reader of the faithful one never see different values. They
		// are pointers, so a day whose slots reported no main block reports all three
		// as null rather than as three zeroes beside a null temp.
		day.MinTemp = &tempMin
		day.MaxTemp = &tempMax
	}

	if humidityCount > 0 {
		// Truncated rather than rounded, which is what the legacy value has always
		// been, so the JSON of this key is unchanged.
		humidity := int(humiditySum / float64(humidityCount))
		day.Humidity = &humidity
	}

	if windCount > 0 {
		windSpeed := windSum / float64(windCount)
		day.WindSpeed = &windSpeed
		day.WindDeg = &windDeg
	}
	if gustSeen {
		day.WindGust = &gustMax
	}
	// clouds, visibility and pressure round their means; humidity truncates. The
	// three have no legacy counterpart, so nothing constrains them and the nearest
	// integer is the honest answer, while humidity's legacy field is an int that has
	// always truncated and changing it would move a number consumers already read for
	// no accuracy. See the ForecastDay doc. wind_speed stays a float mean, unrounded,
	// which is the legacy value unchanged.
	if cloudCount > 0 {
		clouds := int(math.Round(cloudSum / float64(cloudCount)))
		day.Clouds = &clouds
	}
	if visibilityCount > 0 {
		visibility := int(math.Round(visibilitySum / float64(visibilityCount)))
		day.Visibility = &visibility
	}
	if pressureCount > 0 {
		pressure := int(math.Round(pressureSum / float64(pressureCount)))
		day.Pressure = &pressure
	}

	if popSeen {
		// The mean of the fractional probabilities, not of their percentages, and the
		// raw arithmetic mean: quantising it to four places would report a mean of one
		// third as 0.3333, which is a small lie about precision in the one number this
		// route exists to get right. Every slot with no pop is excluded from all three,
		// so a day is not diluted by slots nobody measured.
		popMean := popSum / float64(popCount)
		day.Pop = &popMax
		day.PopMin = &popMin
		day.PopMean = &popMean
		// The legacy alias of the same maximum, so the two cannot disagree. Rounded,
		// because a truncation reports a probability the upstream never gave: 0.29 is
		// 28.999999999999996 in binary floating point, so a plain int() call would
		// answer 28 for a day the upstream called 29 percent.
		chanceOfRain := int(math.Round(popMax * 100))
		day.ChanceOfRain = &chanceOfRain
	}

	if rainSeen {
		day.Rain = &models.ForecastRainBlock{ThreeHour: &rainSum}
	}
	if snowSeen {
		day.Snow = &models.ForecastSnowBlock{ThreeHour: &snowSum}
	}
	// The legacy precipitation is the same two sums as one number, so it is 0 for a
	// day no slot reported precipitation for: a total of zero is a reading, and the
	// legacy key has always been a number rather than an absent member.
	day.Precipitation = rainSum + snowSum

	// weather comes from the middle slot, as the route always has, and the legacy
	// condition, description and icon are that same entry rather than a second read
	// of it.
	day.Weather = slots[len(slots)/2].Weather
	if len(day.Weather) > 0 {
		day.Condition = day.Weather[0].Main
		day.Description = strings.Title(day.Weather[0].Description)
		day.Icon = day.Weather[0].Icon
	}

	// uv_index and uvi stay null: the three hour endpoint reports no uvi, and the
	// decode struct has no field that could hold one.
	return day
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
