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
	}

	// The three opt-in blocks are the response types themselves, decoded from the same
	// body, so there is no mapping step and no second copy of a member list to keep in
	// step. Each is a pointer to a slice: a nil payload member is an absent block and
	// a non-nil empty one is an empty array, which is the distinction a caller who
	// asked for the block needs and cannot get from a plain slice with omitempty.
	//
	// Whether a caller sees one is a decision taken after this body is built, so the
	// cached response always holds them all and one upstream call serves every variant.
	if payload.Minutely != nil {
		envelope.Minutely = &payload.Minutely
	}
	if payload.Hourly != nil {
		envelope.Hourly = &payload.Hourly
	}
	if payload.Alerts != nil {
		envelope.Alerts = &payload.Alerts
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
		Location: *loc,
		Current:  currentFromOneCall(owm.Current, payload.Current),
		Forecast: forecasts,
		// UTC, like last_updated and the legacy date: the response is cached per process,
		// so a member that renders in the server's zone means the same cache entry
		// serialises differently on two hosts.
		RequestTime: w.now().UTC(),
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
// weather entry and are null when the day carries none.
//
// Precipitation is the sum of the two volumes the upstream reported, and it is reported
// only when at least one volume was reported. A day whose upstream measured a rain
// volume of 0 has a total of 0, which is an answer; a day that reported neither rain
// nor snow has no total, which is a gap, and reporting 0 for it said nothing fell and
// measured nothing at the same time. The five day route's ForecastDay.Precipitation
// follows the same rule.
func oneCallLegacyDay(day models.DailyForecast, presence models.SevenDayPayloadDaily) models.Forecast {
	var condition, description, icon string
	if len(day.Weather) > 0 {
		condition = day.Weather[0].Main
		// Title cased, as it has always been on this route. The trio becoming pointers
		// is about nullability, not about the text.
		description = strings.Title(day.Weather[0].Description)
		icon = day.Weather[0].Icon
	}

	legacy := models.Forecast{
		// UTC, not the server's zone. The upstream documents a daily dt in UTC, and a
		// time.Time built from time.Unix renders in the process's zone, so the day
		// component of this string would shift with TZ and the same upstream body
		// would serialise differently on two hosts. The instant is unchanged either
		// way; only the rendering is pinned.
		Date:      time.Unix(day.Dt, 0).UTC(),
		Humidity:  presence.Humidity,
		WindSpeed: presence.WindSpeed,
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

	// A day sent with no weather entry has no condition, so the trio stays null rather
	// than reporting three empty strings, which is the same rule the current block
	// follows for the same upstream condition.
	if len(day.Weather) > 0 {
		legacy.Condition = &condition
		legacy.Description = &description
		legacy.Icon = &icon
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

	// The total is reported only when the upstream reported at least one volume. A day
	// that reported a measured 0 and a day that reported nothing have no answer in
	// common, and reporting 0 for both is the same key claiming two different things.
	if presence.Rain != nil || presence.Snow != nil {
		total := 0.0
		if presence.Rain != nil {
			total += *presence.Rain
		}
		if presence.Snow != nil {
			total += *presence.Snow
		}
		legacy.Precipitation = &total
	}

	return legacy
}

// currentFromOneCall maps the One Call current block onto our model. The block is
// absent on some responses, in which case the zero value is returned.
//
// The zero value of a pointer-shaped block is every member null, which is the point
// of the change: an absent block used to report a temperature of 0, a pressure of 0,
// a visibility of 0 and a last_updated of 1970, beside a faithful block that
// correctly said null. Every reading here comes from the presence view the faithful
// block is built from, so the two vocabularies report one set of readings and cannot
// disagree about whether a reading exists.
func currentFromOneCall(current *models.OneCallCurrent, presence *models.SevenDayPayloadCurrent) models.Current {
	if current == nil || presence == nil {
		return models.Current{}
	}

	condition, description, icon := "", "", ""
	if len(current.Weather) > 0 {
		condition = current.Weather[0].Main
		// The legacy description has always been title cased, and it stays that way:
		// the change here is to the nullability of the field, not to its text.
		description = strings.Title(current.Weather[0].Description)
		icon = current.Weather[0].Icon
	}

	// UTC, so the rendered timestamp is the same on every host. It is an instant
	// rather than a calendar day, so this is not a correctness question, but the
	// response is cached per process and host dependent output is wrong in a cacheable
	// API.
	updated := time.Unix(current.Dt, 0).UTC()

	legacy := models.Current{
		Temperature:   presence.Temp,
		FeelsLike:     presence.FeelsLike,
		Humidity:      presence.Humidity,
		WindSpeed:     presence.WindSpeed,
		WindDirection: presence.WindDeg,
		WindGust:      presence.WindGust,
		CloudCover:    presence.Clouds,
		LastUpdated:   &updated,
	}
	// Pressure and visibility are ints on the block and floats here, so they are
	// widened rather than reinterpreted. An absent reading stays absent through the
	// conversion.
	if presence.Pressure != nil {
		pressure := float64(*presence.Pressure)
		legacy.Pressure = &pressure
	}
	if presence.Visibility != nil {
		visibility := float64(*presence.Visibility)
		legacy.Visibility = &visibility
	}
	// The condition, description and icon are the first weather entry's. This block
	// documents no extremes of its own, so max_temperature and min_temperature stay
	// null: the day carries them, as a time of day breakdown.
	if len(current.Weather) > 0 {
		legacy.Condition = &condition
		legacy.Description = &description
		legacy.Icon = &icon
	}

	return legacy
}

// resolveAPIKey prefers a caller supplied key over the server side key.
func (w *WeatherService) resolveAPIKey(apikey string) string {
	if apikey != "" {
		return apikey
	}
	return w.APIKey
}

// mapCurrentWeather builds the current route's response: a faithful mirror of
// the /data/2.5/weather schema plus the legacy vocabulary. Both vocabularies are
// assigned from the same locals, so a legacy key cannot drift away from the
// faithful value it aliases. Values come from owm; presence, and therefore the
// difference between a measured zero and an absent member, comes from payload.
//
// A block the upstream did not send is left nil, so an absent block and a block
// whose members are all null stay two different things, and the window this
// endpoint does not document is omitted from a precipitation block.
//
// The legacy block is gated on the same three blocks as the faithful one, for the
// same reason. It used to be assigned in one literal from locals read straight off
// the decode struct, so a body with no wind reported "wind": null beside
// current.wind_speed: 0 and current.wind_direction: 0, a body with no main
// reported "main": null beside four fabricated zeroes, and a body with no clouds
// reported "clouds": null beside current.cloud_cover: 0. The two vocabularies
// contradicting each other about whether a reading exists is the one thing the
// pointer work was for, and this half of the block had been left out of it.
// currentFromOneCall and forecastDay both read the block this way.
//
// The gust is read from the payload rather than from the decode struct, because
// wind.gust is not documented for this endpoint and the decode field's zero means
// "no gust" rather than "a measured 0 m/s". Its legacy key carries an omitempty
// that drops only nil, so the member is left nil for an absent gust and the key
// goes with it, while a measured 0 is a non-nil pointer to zero and is reported.
// Taking the decode field's address made the pointer never nil and fabricated the
// 0; that was the same defect the forecast route carried.
//
// The legacy current block's max_temperature and min_temperature are null, since
// this endpoint reports no extremes and the route has never measured any. They
// were a fabricated 0 until models.Current became pointer shaped.
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
	speed, deg := owm.Wind.Speed, owm.Wind.Deg
	cloudCover := owm.Clouds.All
	dt := owm.Dt
	sysType, sysID := owm.Sys.Type, owm.Sys.ID
	sunrise, sunset := owm.Sys.Sunrise, owm.Sys.Sunset
	// The legacy current block reads the same locals by address, so the two
	// vocabularies cannot disagree about a value the upstream measured.
	pressureFloat := float64(pressure)
	// UTC, for the same reason as the seven day route's current block: the same body
	// must serialise the same way on two hosts.
	dtTime := time.Unix(dt, 0).UTC()
	description = strings.Title(description)

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

	// The legacy block is built from the same locals as the faithful one, so a legacy
	// key cannot drift away from the faithful value it aliases, and every reading that
	// lives inside a block is assigned under the same gate the faithful block uses.
	// Taking the address of a local the faithful block decided to discard is how a
	// fabricated zero gets in beside a null: the gate computes the right answer and
	// then a second, ungated assignment throws it away, and the code reads as if the
	// case were handled. Three separate gates, one per block, and the condition trio
	// is a fourth under the same rule.
	legacy := models.Current{
		// The two members read from the payload rather than from the decode struct, the
		// gust through the wind gate below and the visibility here, are covered by the
		// function comment above.
		Visibility: floatPtr(payload.Visibility),
		// dt is documented as always sent by this endpoint, which is what the payload
		// allowlist claims for it, so the timestamp is not gated. UTC, like the
		// faithful dt and the seven day route's last_updated: the same body must
		// serialise the same way on two hosts.
		LastUpdated: &dtTime,
		// max_temperature and min_temperature stay null. This endpoint reports no
		// extremes and the route has never measured any, so the 0 they used to carry
		// was a reading nobody made.
	}
	if payload.Main != nil {
		legacy.Temperature = &temp
		legacy.FeelsLike = &feelsLike
		legacy.Humidity = &humidity
		legacy.Pressure = &pressureFloat
	}
	if payload.Wind != nil {
		legacy.WindSpeed = &speed
		legacy.WindDirection = &deg
		legacy.WindGust = payload.Wind.Gust
	}
	if payload.Clouds != nil {
		legacy.CloudCover = &cloudCover
	}
	// The condition, description and icon are the first weather entry's, and a body
	// the upstream sent with no weather entry has none, so the trio stays null rather
	// than reporting three empty strings. One route, one case, one rule: the same
	// shape models.Forecast and ForecastDay take on the two forecast routes.
	if len(owm.Weather) > 0 {
		legacy.Condition = &condition
		legacy.Description = &description
		legacy.Icon = &icon
	}

	return &models.CurrentWeatherResponse{
		Coord:      models.Coordinates{Lon: lon, Lat: lat},
		Weather:    owm.Weather,
		Base:       owm.Base,
		Main:       main,
		Visibility: payload.Visibility,
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
		Current: legacy,
		// UTC, like the seven day route's request_time: a cache entry that renders in
		// the server's zone is host dependent output.
		RequestTime: w.now().UTC(),
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
		Current:  currentFromForecast(owm.List, forecastPresence(payload, 0)),
		Forecast: forecasts,
		// UTC, like the seven day route's request_time: a cache entry that renders in
		// the server's zone is host dependent output.
		RequestTime: w.now().UTC(),
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
			// temp_kf is documented on this endpoint's slots and was suppressed for a
			// long time on a false claim that it was not, so a slot that reports one
			// carries it and a slot that reports none is null rather than a permanent
			// null either way.
			TempKF: presence.Main.TempKF,
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
	// pod is a pointer, so a body of "sys":{} reports a block with a null pod rather
	// than a fabricated empty string, which is the same rule the condition, description
	// and icon members were converted to null for.
	if presence.Sys != nil {
		slot.Sys = &models.ForecastSlotSys{Pod: presence.Sys.Pod}
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
//
// The three block reads below dereference a member of a slot's main, wind and clouds
// block with no check of its own. That is safe because mapForecastSlot populates all
// three blocks unconditionally whenever the presence view says the upstream sent them,
// and leaves them nil otherwise, so a block that is non-nil always has all of its
// members bound. The invariant is this function's to depend on and the mapper's to keep,
// so it is stated here rather than left implicit. The guard is the second half of that:
// a block that is somehow not fully populated skips the slot rather than panicking on a
// nil member, because a rollup that skips one reading is wrong in a way a caller can
// see and a 500 is not.
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
		// Each block is skipped whole when it is not fully populated, rather than each
		// member being skipped. A half-accumulated mean is a number derived from fewer
		// readings than it claims to be, which is worse than a null for a member the
		// route could not measure.
		if main := slot.Main; main != nil && main.Temp != nil && main.Humidity != nil && main.Pressure != nil {
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

		if wind := slot.Wind; wind != nil && wind.Speed != nil && wind.Deg != nil {
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

		if clouds := slot.Clouds; clouds != nil && clouds.All != nil {
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
	// The legacy precipitation is the same two sums as one number, and it is reported
	// only when at least one slot carried a window. A slot reporting a measured 0 gives
	// a total of 0, which is an answer; no slot reporting a window gives no total, which
	// is a gap. This is the same rule the seven day route's Forecast.Precipitation
	// follows, and the two keys now answer one question the same way.
	if rainSeen || snowSeen {
		total := rainSum + snowSum
		day.Precipitation = &total
	}

	// weather comes from the middle slot, as the route always has, and the legacy
	// condition, description and icon are that same entry rather than a second read
	// of it. A slot the upstream sent with no weather entry has no condition, so the
	// trio is null rather than three empty strings, which is the rule the legacy
	// current block on this route and the seven day route's day block both follow for
	// the same upstream condition.
	day.Weather = slots[len(slots)/2].Weather
	if len(day.Weather) > 0 {
		condition, description, icon := day.Weather[0].Main, day.Weather[0].Description, day.Weather[0].Icon
		// Title cased, as the legacy value has always been on this route. The trio
		// becoming pointers is about nullability, not about the text.
		title := strings.Title(description)
		day.Condition = &condition
		day.Description = &title
		day.Icon = &icon
	}

	// uv_index and uvi stay null: the three hour endpoint reports no uvi, and the
	// decode struct has no field that could hold one.
	return day
}

// forecastPresence is the presence view of the i-th slot, mirroring oneCallPresence on
// the seven day route. The two decodes read the same array in the same order, so index
// i of payload.List is the presence view of index i of items[i].
func forecastPresence(payload models.ForecastPayload, i int) models.ForecastPayloadItem {
	if i < len(payload.List) {
		return payload.List[i]
	}
	return models.ForecastPayloadItem{}
}

// floatPtr widens an int reading to the float the legacy block carries, keeping nil nil
// so an absent reading stays absent through the conversion. models.Current.Pressure and
// .Visibility are floats where the upstream reports ints, which is the only reason this
// exists.
func floatPtr(reading *int) *float64 {
	if reading == nil {
		return nil
	}
	widened := float64(*reading)
	return &widened
}

// currentFromForecast maps the nearest forecast slot onto our model. The 5 day
// endpoint returns 3 hour slots rather than true current conditions, so this block is
// a slot's readings under a name that promises otherwise; the slot timestamp is
// reported as last_updated so callers can see how stale the reading is.
//
// presence is the payload view of the same slot, and every member whose zero in the
// decode struct could be an absent member is read through it. Four are read that way
// here: visibility, and the main, wind and clouds blocks as blocks.
//
// Visibility is one: the endpoint documents list.visibility, so the faithful hourly slot
// and this legacy block were reporting the same datum two ways, the slot populated and
// the block null. A slot the upstream sent without one leaves it null here, rather than
// a visibility of 0 metres, which is what reading the non-pointer field would have
// reported.
//
// The main and wind blocks are the other two, and the rule is the same one the
// faithful slot and currentFromOneCall follow: a reading that lives inside a block the
// upstream did not send is not a reading. The faithful slot reports such a slot's main
// block as null, so a legacy block carrying four zeroes beside it said the two
// vocabularies disagree about whether the upstream measured anything. The gates are
// written out per block rather than folded into one predicate, because each block gates
// a different set of keys and a single shared guard is what made the block look
// handled while three of its readings were assigned anyway.
//
// WindGust is the other member of the wind block, and the opposite way round. The
// endpoint does not document wind.gust for a three hour slot, so the decode field's
// zero means "no gust" rather than "a measured 0 m/s", and taking its address
// fabricated the 0. Reading the payload gives the three states their three answers: no
// gust leaves the member nil and omitempty drops the key, a measured 0 is a non-nil
// pointer to zero and is reported as 0, and a gust is that number.
func currentFromForecast(items []models.ForecastItem, presence models.ForecastPayloadItem) models.Current {
	if len(items) == 0 {
		return models.Current{}
	}

	// OpenWeatherMap returns list ascending from the current 3 hour boundary.
	nearest := items[0]

	condition, description, icon := "", "", ""
	if len(nearest.Weather) > 0 {
		condition = nearest.Weather[0].Main
		// Title cased, as it has always been on this route. The change here is to the
		// nullability of the field, not to its text.
		description = strings.Title(nearest.Weather[0].Description)
		icon = nearest.Weather[0].Icon
	}

	// The values are the ones this function has always read, taken by address because
	// models.Current is pointer shaped, and gated below on the same blocks the
	// faithful slot is gated on. An empty list returns the all-null zero value above,
	// which is what the block is now able to say.
	temp, feelsLike := nearest.Main.Temp, nearest.Main.FeelsLike
	humidity, pressure := nearest.Main.Humidity, float64(nearest.Main.Pressure)
	speed, deg := nearest.Wind.Speed, nearest.Wind.Deg
	maxTemp, minTemp := nearest.Main.TempMax, nearest.Main.TempMin
	cloudCover := nearest.Clouds.All
	// UTC, as on the other two routes: the same body must serialise the same way on
	// two hosts.
	updated := time.Unix(nearest.Dt, 0).UTC()

	legacy := models.Current{
		// dt is documented as always sent on a slot, which is what the payload
		// allowlist claims for list.dt, so the timestamp is not gated on anything.
		LastUpdated: &updated,
		// The documented list.visibility of the slot, through the presence view rather
		// than the decode struct's zero, so the same reading reaches the faithful
		// hourly slot and this block, and an absent one is null in both.
		Visibility: floatPtr(presence.Visibility),
	}
	if presence.Main != nil {
		// The extremes are members of the same main block as the temperature, so they
		// are gated with it rather than beside it. A slot the upstream sent with no
		// main block has no temp_max to report, and reporting 0 is the same
		// fabrication the other four keys were.
		legacy.Temperature = &temp
		legacy.FeelsLike = &feelsLike
		legacy.Humidity = &humidity
		legacy.Pressure = &pressure
		legacy.MaxTemp = &maxTemp
		legacy.MinTemp = &minTemp
	}
	if presence.Wind != nil {
		legacy.WindSpeed = &speed
		legacy.WindDirection = &deg
		legacy.WindGust = presence.Wind.Gust
	}
	if presence.Clouds != nil {
		legacy.CloudCover = &cloudCover
	}
	// The condition, description and icon are the first weather entry's, and they are
	// only set when there is one: a slot with no weather entry reports them null rather
	// than as three empty strings, which is the rule the legacy current block follows
	// for the same upstream condition.
	if len(nearest.Weather) > 0 {
		legacy.Condition = &condition
		legacy.Description = &description
		legacy.Icon = &icon
	}

	return legacy
}
