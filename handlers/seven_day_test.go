package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"weathering-with-go/services"
)

const currentWeatherJSON = `{"coord":{"lon":-0.13,"lat":51.51},"weather":[{"main":"Clouds","description":"scattered clouds","icon":"03d"}],"main":{"temp":15.5,"feels_like":14.8,"pressure":1013,"humidity":72},"wind":{"speed":3.6,"deg":230},"clouds":{"all":40},"dt":1234567890,"sys":{"country":"GB"},"name":"London","cod":200}`

const forecastJSON = `{"cod":"200","message":0,"cnt":2,"list":[{"dt":1772000000,"main":{"temp":18.0,"temp_min":15.0,"temp_max":21.0,"feels_like":18.0,"pressure":1012,"humidity":70},"weather":[{"main":"Rain","description":"light rain","icon":"10d"}],"clouds":{"all":80},"wind":{"speed":5.0,"deg":90},"rain":{"3h":1.2},"sys":{"pod":"d"},"dt_txt":"2026-02-25 12:00:00"},{"dt":1772010800,"main":{"temp":19.0,"temp_min":16.0,"temp_max":22.0,"feels_like":19.0,"pressure":1011,"humidity":65},"weather":[{"main":"Clear","description":"clear sky","icon":"01d"}],"clouds":{"all":0},"wind":{"speed":3.0,"deg":100},"sys":{"pod":"d"},"dt_txt":"2026-02-25 15:00:00"}],"city":{"id":2643743,"name":"London","coord":{"lon":-0.13,"lat":51.51},"country":"GB","timezone":0}}`

// forecastUTCMidnight anchors the forecast route fixtures. The route groups slots by
// the UTC date of a timestamp, so a UTC anchor is what makes a fixture land on
// exactly one day on every host, whatever the test process's own zone is.
var forecastUTCMidnight = time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)

// forecastSlot renders one raw three hour slot. A member left out of members is
// left out of the body, which is how a fixture says the upstream never sent it.
// dt_txt is the UTC rendering of the slot's own dt, as the upstream sends it.
func forecastSlot(offsetHours int, members map[string]any) map[string]any {
	dt := forecastUTCMidnight.Add(time.Duration(offsetHours) * time.Hour).Unix()
	members["dt"] = dt
	members["dt_txt"] = time.Unix(dt, 0).UTC().Format("2006-01-02 15:04:05")
	members["sys"] = map[string]any{"pod": "d"}
	return members
}

func forecastRouteBody(t *testing.T, slots ...map[string]any) string {
	t.Helper()
	rendered := make([]string, 0, len(slots))
	for _, slot := range slots {
		body, err := json.Marshal(slot)
		if err != nil {
			t.Fatalf("failed to render a forecast fixture: %v", err)
		}
		rendered = append(rendered, string(body))
	}
	// message is the upstream's calculation time and arrives as a float, such as
	// 0.0117, so every forecast fixture carries that shape.
	return fmt.Sprintf(`{"cod":"200","message":0.0117,"cnt":%d,"list":[%s],"city":{"id":2643743,"name":"London","coord":{"lon":-0.13,"lat":51.51},"country":"GB","population":7556900,"timezone":0,"sunrise":1771999200,"sunset":1772030400}}`,
		len(slots), strings.Join(rendered, ","))
}

// forecastRoutePopJSON is a /data/2.5/forecast body whose two slots carry pop,
// visibility and, on the first, a 3h rain window, so the daily rollup and the raw
// slots can both be asserted from one fixture.
func forecastRoutePopJSON(t *testing.T) string {
	t.Helper()
	return forecastRouteBody(t,
		forecastSlot(0, map[string]any{
			"main":    map[string]any{"temp": 18.0, "feels_like": 17.2, "temp_min": 15.0, "temp_max": 21.0, "pressure": 1012, "humidity": 70, "sea_level": 1010, "grnd_level": 1005},
			"weather": []any{map[string]any{"id": 500, "main": "Rain", "description": "light rain", "icon": "10d"}},
			"clouds":  map[string]any{"all": 80},
			"wind":    map[string]any{"speed": 5.0, "deg": 90, "gust": 7.5},
			"rain":    map[string]any{"3h": 1.2},
			"pop":     0.2, "visibility": 10000,
		}),
		forecastSlot(3, map[string]any{
			"main":    map[string]any{"temp": 19.0, "feels_like": 18.0, "temp_min": 16.0, "temp_max": 22.0, "pressure": 1011, "humidity": 65},
			"weather": []any{map[string]any{"id": 800, "main": "Clear", "description": "clear sky", "icon": "01d"}},
			"clouds":  map[string]any{"all": 0},
			"wind":    map[string]any{"speed": 3.0, "deg": 100},
			"pop":     0.8, "visibility": 9000,
		}),
	)
}

// forecastRouteZeroRainJSON is the same shape with one slot reporting a rain
// window the upstream genuinely measured as 0 and one reporting no rain block at
// all. Those are different readings, and the response has to keep them apart.
func forecastRouteZeroRainJSON(t *testing.T) string {
	t.Helper()
	return forecastRouteBody(t,
		forecastSlot(0, map[string]any{
			"main":    map[string]any{"temp": 12.0, "feels_like": 10.4, "temp_min": 11.0, "temp_max": 13.0, "pressure": 1008, "humidity": 90},
			"weather": []any{map[string]any{"id": 500, "main": "Rain", "description": "light rain", "icon": "10d"}},
			"clouds":  map[string]any{"all": 90},
			"wind":    map[string]any{"speed": 4.1, "deg": 80},
			"rain":    map[string]any{"3h": 0},
			"pop":     0.9, "visibility": 6000,
		}),
		forecastSlot(3, map[string]any{
			"main":    map[string]any{"temp": 13.0, "feels_like": 11.4, "temp_min": 12.0, "temp_max": 14.0, "pressure": 1008, "humidity": 88},
			"weather": []any{map[string]any{"id": 801, "main": "Clouds", "description": "scattered clouds", "icon": "03d"}},
			"clouds":  map[string]any{"all": 75},
			"wind":    map[string]any{"speed": 3.4, "deg": 120},
			"pop":     0.4, "visibility": 8000,
		}),
	)
}

func sevenDayJSON() string {
	days := make([]string, 0, 8)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		days = append(days, fmt.Sprintf(`{"dt":%d,"temp":{"day":%d.0,"min":%d.0,"max":%d.0},"pressure":1015,"humidity":%d,"wind_speed":4.0,"wind_deg":200,"clouds":40,"pop":0.25,"uvi":3.5,"weather":[{"id":801,"main":"Clouds","description":"scattered clouds","icon":"03d"}]}`,
			base.AddDate(0, 0, i).Unix(), 15+i, 10+i, 20+i, 60+i))
	}
	return fmt.Sprintf(`{"lat":51.5074,"lon":-0.1278,"timezone":"Europe/London","current":{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,"temp":11.5,"feels_like":10.2,"pressure":1009,"humidity":78,"dew_point":7.7,"uvi":1.8,"clouds":75,"visibility":8000,"wind_speed":6.2,"wind_deg":240,"wind_gust":9.1,"weather":[{"id":803,"main":"Clouds","description":"broken clouds","icon":"04d"}]},"daily":[%s]}`, strings.Join(days, ","))
}

// The seven day route has no grouping step: the upstream sends a daily entry per
// day and the response reports them in the order it received them. That makes a
// fixed UTC anchor sufficient here, where the forecast route needs one because it
// groups slots by date. Noon UTC keeps every day on its own calendar date in every
// host zone.
var sevenDayRouteBase = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// sevenDayRouteDayCount is the number of daily entries the shared seven day
// fixtures carry. It is deliberately more than the legacy cap of 7: the faithful
// daily[] is uncapped, so a fixture with more entries than the legacy array is the
// only thing that makes the difference visible, and a test that asserts 8 is what
// stops the fixture from quietly shrinking to 7.
const sevenDayRouteDayCount = 8

// sevenDayDay renders one raw /data/3.0/onecall daily entry. A member absent from
// members is absent from the body, which is how a fixture says the upstream never
// sent it, as distinct from sending it as 0.
func sevenDayDay(at time.Time, members map[string]any) map[string]any {
	day := map[string]any{
		"dt":         at.Unix(),
		"sunrise":    at.Add(-6 * time.Hour).Unix(),
		"sunset":     at.Add(6 * time.Hour).Unix(),
		"moonrise":   at.Add(-9 * time.Hour).Unix(),
		"moonset":    at.Add(9 * time.Hour).Unix(),
		"moon_phase": 0.42,
		"pressure":   1015,
		"clouds":     40,
		"weather":    []any{map[string]any{"id": 801, "main": "Clouds", "description": "scattered clouds", "icon": "03d"}},
	}
	for name, value := range members {
		day[name] = value
	}
	return day
}

// oneCallCurrentBlock is the upstream current block the seven day fixtures carry.
// It is a literal rather than a map because no fixture omits a member of it.
const oneCallCurrentBlock = `{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,` +
	`"temp":11.5,"feels_like":10.2,"pressure":1009,"humidity":78,"dew_point":7.7,"uvi":1.8,` +
	`"clouds":75,"visibility":8000,"wind_speed":6.2,"wind_deg":240,"wind_gust":9.1,` +
	`"weather":[{"id":803,"main":"Clouds","description":"broken clouds","icon":"04d"}]}`

// oneCallOptInBlocks are the three blocks the route used to ask the upstream to
// exclude. They are here so the tests can assert the mapper carries them through.
const oneCallOptInBlocks = `,"minutely":[{"dt":1772000060,"precipitation":0.12}],` +
	`"hourly":[{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,"temp":11.5,` +
	`"feels_like":10.2,"pressure":1009,"humidity":78,"dew_point":7.7,"uvi":1.8,"clouds":75,` +
	`"visibility":8000,"wind_speed":6.2,"wind_deg":240,"wind_gust":9.1,"pop":0.2,"rain":0,` +
	`"snow":0,"weather":[{"id":803,"main":"Clouds","description":"broken clouds","icon":"04d"}]}],` +
	`"alerts":[{"sender_name":"Met Office","event":"Flood warning","start":1772000000,` +
	`"end":1772600000,"description":"Flooding is possible.","tags":["Flood"]}]`

// mustOneCallBody renders a whole /data/3.0/onecall body around rendered days. An
// empty current block leaves the member out of the body entirely, which is how a
// fixture says the upstream sent no current conditions at all, as distinct from
// sending a current block with members missing from it.
func mustOneCallBody(t *testing.T, current string, days []string) string {
	t.Helper()
	body := `{"lat":51.5074,"lon":-0.1278,"timezone":"Europe/London","timezone_offset":0,`
	if current != "" {
		body += `"current":` + current + `,`
	}
	return body + `"daily":[` + strings.Join(days, ",") + `]` + oneCallOptInBlocks + `}`
}

// sevenDayRouteFullJSON carries sevenDayRouteDayCount days with every member the
// One Call daily block documents, so the faithful mirror has something to mirror.
// Day i is offset by i on every reading, so a test can tell one day from another.
func sevenDayRouteFullJSON(t *testing.T) string {
	t.Helper()
	days := make([]string, 0, sevenDayRouteDayCount)
	for i := range sevenDayRouteDayCount {
		day := sevenDayDay(sevenDayRouteBase.AddDate(0, 0, i), map[string]any{
			"temp": map[string]any{
				"day":   float64(15 + i),
				"min":   float64(10 + i),
				"max":   float64(20 + i),
				"night": 9.0,
				"morn":  11.0,
				"eve":   16.0,
			},
			"feels_like": map[string]any{"day": 14.0, "night": 8.0, "morn": 10.0, "eve": 15.0},
			"humidity":   60 + i,
			"dew_point":  7.7,
			"wind_speed": 4.0,
			"wind_deg":   200,
			"wind_gust":  9.1,
			// 0.29 is the probability whose percentage truncates: 0.29*100 is
			// 28.999999999999996 in binary floating point, so chance_of_rain has to
			// be rounded to answer 29 rather than 28.
			"pop":  0.29,
			"rain": 1.5,
			"snow": 0,
			"uvi":  3.5,
		})
		rendered, err := json.Marshal(day)
		if err != nil {
			t.Fatalf("failed to render a seven day fixture: %v", err)
		}
		days = append(days, string(rendered))
	}
	return mustOneCallBody(t, oneCallCurrentBlock, days)
}

// sevenDayRouteSparseDayJSON is the pair of states the faithful mirror has to keep
// apart. Day 0 reports no temp, feels_like, humidity, dew_point, wind, pop, rain,
// snow or uvi at all, so every one of those is a key with no value and the response
// has to say null. Day 1 reports a rain volume the upstream genuinely measured as 0
// and no snow, so 0 and an absent member are two different readings on the same day
// array.
func sevenDayRouteSparseDayJSON(t *testing.T) string {
	t.Helper()
	sparse, err := json.Marshal(sevenDayDay(sevenDayRouteBase, nil))
	if err != nil {
		t.Fatalf("failed to render the sparse day fixture: %v", err)
	}
	zeroRain, err := json.Marshal(sevenDayDay(sevenDayRouteBase.AddDate(0, 0, 1), map[string]any{
		"temp":       map[string]any{"day": 16.0, "min": 11.0, "max": 21.0, "night": 9.0, "morn": 11.0, "eve": 16.0},
		"feels_like": map[string]any{"day": 14.0, "night": 8.0, "morn": 10.0, "eve": 15.0},
		"humidity":   61,
		"dew_point":  7.7,
		"wind_speed": 4.0,
		"wind_deg":   200,
		"wind_gust":  9.1,
		"rain":       0,
		"uvi":        3.5,
	}))
	if err != nil {
		t.Fatalf("failed to render the zero rain day fixture: %v", err)
	}
	return mustOneCallBody(t, oneCallCurrentBlock, []string{string(sparse), string(zeroRain)})
}

// sevenDayRouteNoCurrentJSON is a body whose current block is missing entirely. The
// two vocabularies both have to say so: the faithful one can report the block as null
// and the legacy one, now that it is pointer shaped, reports every member as null
// rather than as a row of zeroes and a date in 1970.
func sevenDayRouteNoCurrentJSON(t *testing.T) string {
	t.Helper()
	day, err := json.Marshal(sevenDayDay(sevenDayRouteBase, map[string]any{
		"temp":       map[string]any{"day": 15.0, "min": 10.0, "max": 20.0, "night": 9.0, "morn": 11.0, "eve": 16.0},
		"feels_like": map[string]any{"day": 14.0, "night": 8.0, "morn": 10.0, "eve": 15.0},
		"humidity":   60,
		"dew_point":  7.7,
		"wind_speed": 4.0,
		"wind_deg":   200,
		"wind_gust":  9.1,
		"pop":        0.29,
		"rain":       1.5,
		"snow":       0,
		"uvi":        3.5,
	}))
	if err != nil {
		t.Fatalf("failed to render the no current fixture: %v", err)
	}
	return mustOneCallBody(t, "", []string{string(day)})
}

// newSevenDayRouteRouter serves one fixed upstream body to the seven day route, so
// the faithful mirror can be asserted member by member. It answers the geocoding
// call too, because this route geocodes before it fetches, and it builds its own
// server rather than reusing the shared stub because the shared fixture carries
// neither the opt-in blocks nor the members this route's tests read.
func newSevenDayRouteRouter(t *testing.T, upstream string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/geo/1.0/direct"):
			fmt.Fprintln(w, `[{"name":"London","lat":51.5074,"lon":-0.1278,"country":"GB","state":"England"}]`)
		case strings.HasPrefix(r.URL.Path, "/data/3.0/onecall"):
			fmt.Fprintln(w, upstream)
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	svc := services.NewWeatherService("dummy")
	svc.HTTPClient = &http.Client{Transport: &transportRedirect{target: srv.URL}}

	router := gin.New()
	router.GET("/api/v1/weather/forecast/7day", NewWeatherHandler(svc).GetSevenDayForecast)
	return router
}

// requestSevenDayRoute returns the whole data object, so a test can read the
// faithful onecall namespace and the legacy keys from the same body.
func requestSevenDayRoute(t *testing.T, router *gin.Engine) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast/7day?location=London,UK&units=metric", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response %q: %v", w.Body.String(), err)
	}
	if !resp.Success {
		t.Fatalf("expected a success response, got %q", w.Body.String())
	}
	return resp.Data
}

// array returns an array member as a slice of objects.
func array(t *testing.T, parent map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("expected an array at %q, got %#v", key, parent[key])
	}
	entries := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		object, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("expected an object in %q, got %#v", key, entry)
		}
		entries = append(entries, object)
	}
	return entries
}

type upstreamStub struct {
	currentCalls  int32
	forecastCalls int32
	geocodeCalls  int32
	oneCallCalls  int32
	lastQuery     atomic.Value
}

func newStubbedRouter(t *testing.T, stub *upstreamStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		stub.lastQuery.Store(r.URL.RawQuery)
		switch {
		case strings.HasPrefix(r.URL.Path, "/geo/1.0/direct"):
			atomic.AddInt32(&stub.geocodeCalls, 1)
			if r.URL.Query().Get("q") == "London,UK" {
				fmt.Fprintln(w, `[{"name":"London","lat":51.5074,"lon":-0.1278,"country":"GB","state":"England"}]`)
				return
			}
			fmt.Fprintln(w, `[]`)
		case strings.HasPrefix(r.URL.Path, "/data/3.0/onecall"):
			atomic.AddInt32(&stub.oneCallCalls, 1)
			fmt.Fprintln(w, sevenDayJSON())
		case strings.HasPrefix(r.URL.Path, "/data/2.5/forecast"):
			atomic.AddInt32(&stub.forecastCalls, 1)
			fmt.Fprintln(w, forecastJSON)
		case strings.HasPrefix(r.URL.Path, "/data/2.5/weather"):
			atomic.AddInt32(&stub.currentCalls, 1)
			fmt.Fprintln(w, currentWeatherJSON)
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	svc := services.NewWeatherService("server-key")
	svc.HTTPClient = &http.Client{Transport: &transportRedirect{target: srv.URL}}

	router := gin.New()
	SetupRoutes(router, svc)
	return router
}

func decodeForecastCount(t *testing.T, body *bytes.Buffer) int {
	t.Helper()
	return len(decodeForecast(t, body))
}

func decodeForecast(t *testing.T, body *bytes.Buffer) []map[string]any {
	t.Helper()
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Forecast []map[string]any `json:"forecast"`
		} `json:"data"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response %q: %v", body.String(), err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %q", body.String())
	}
	return resp.Data.Forecast
}

func decodeCurrent(t *testing.T, body *bytes.Buffer) map[string]any {
	t.Helper()
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Current map[string]any `json:"current"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response %q: %v", body.String(), err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %q", body.String())
	}
	return resp.Data.Current
}

// newForecastRouteRouter serves one fixed upstream body to the forecast route, so
// the day rollup and the raw slots can be asserted field by field. It builds its
// own server rather than reusing the shared stub, because the shared fixture is
// also what the cache tests count calls against and carries neither pop nor
// visibility on its slots.
func newForecastRouteRouter(t *testing.T, upstream string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, upstream)
	}))
	t.Cleanup(srv.Close)

	svc := services.NewWeatherService("dummy")
	svc.HTTPClient = &http.Client{Transport: &transportRedirect{target: srv.URL}}

	router := gin.New()
	router.GET("/api/v1/weather/forecast", NewWeatherHandler(svc).GetWeatherForecast)
	return router
}

// requestForecastRoute returns the whole data object and its forecast days, so a
// test can assert the faithful envelope and the legacy block from the same body.
func requestForecastRoute(t *testing.T, router *gin.Engine) (map[string]any, []map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast?location=London,UK&units=metric", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response %q: %v", w.Body.String(), err)
	}
	if !resp.Success {
		t.Fatalf("expected a success response, got %q", w.Body.String())
	}

	raw, ok := resp.Data["forecast"].([]any)
	if !ok {
		t.Fatalf("expected a forecast array, got %#v", resp.Data["forecast"])
	}
	days := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		day, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("expected an object in forecast, got %#v", entry)
		}
		days = append(days, day)
	}
	return resp.Data, days
}

func hourlySlots(t *testing.T, day map[string]any) []map[string]any {
	t.Helper()
	slots, ok := day["hourly"].([]any)
	if !ok {
		t.Fatalf("expected an hourly array on the day, got %#v", day["hourly"])
	}
	mapped := make([]map[string]any, 0, len(slots))
	for _, slot := range slots {
		entry, ok := slot.(map[string]any)
		if !ok {
			t.Fatalf("expected an object in hourly, got %#v", slot)
		}
		mapped = append(mapped, entry)
	}
	return mapped
}

// The forecast route must expose the slots it rolled up, raw: a client can read
// pop, visibility and the 3h precipitation window for a single three hour period,
// which no daily mean can recover.
func TestForecastRouteExposesRawSlotsWithPop(t *testing.T) {
	envelope, days := requestForecastRoute(t, newForecastRouteRouter(t, forecastRoutePopJSON(t)))

	if len(days) != 1 {
		t.Fatalf("expected 1 day from the fixture, got %d: %v", len(days), days)
	}
	if envelope["cod"] != "200" {
		t.Fatalf("expected the faithful envelope to mirror cod 200, got %#v", envelope["cod"])
	}
	if envelope["cnt"] != 2.0 {
		t.Fatalf("expected the faithful envelope to mirror cnt 2, got %#v", envelope["cnt"])
	}
	// The upstream sends message as a float carrying the calculation time. Binding it
	// as an int fails the whole decode, so this assertion is the guard against that
	// coming back: the fixture's value is fractional on purpose.
	if envelope["message"] != 0.0117 {
		t.Fatalf("expected the faithful envelope to mirror message 0.0117, got %#v", envelope["message"])
	}
	city := block(t, envelope, "city")
	if city["population"] != 7556900.0 {
		t.Fatalf("expected faithful city.population 7556900, got %#v", city["population"])
	}
	if location := block(t, envelope, "location"); location["name"] != "London" {
		t.Fatalf("expected the legacy location.name London, got %#v", location)
	}

	slots := hourlySlots(t, days[0])
	if len(slots) != 2 {
		t.Fatalf("expected 2 slots on the day, got %d", len(slots))
	}
	// dt_txt is the upstream's UTC rendering of the slot's own timestamp, so the two
	// have to agree rather than merely both be present.
	dt, ok := slots[0]["dt"].(float64)
	if !ok {
		t.Fatalf("expected a numeric dt on the slot, got %#v", slots[0]["dt"])
	}
	if want := time.Unix(int64(dt), 0).UTC().Format("2006-01-02 15:04:05"); slots[0]["dt_txt"] != want {
		t.Fatalf("expected dt_txt %q to match the slot's dt, got %#v", want, slots[0]["dt_txt"])
	}
	if slots[0]["pop"] != 0.2 || slots[1]["pop"] != 0.8 {
		t.Fatalf("expected the fixture pop on each slot, got %#v and %#v", slots[0]["pop"], slots[1]["pop"])
	}
	if slots[0]["visibility"] != 10000.0 || slots[1]["visibility"] != 9000.0 {
		t.Fatalf("expected the fixture visibility on each slot, got %#v and %#v", slots[0]["visibility"], slots[1]["visibility"])
	}
	rain := block(t, slots[0], "rain")
	if rain["3h"] != 1.2 {
		t.Fatalf("expected the fixture rain 3h of 1.2, got %#v", rain["3h"])
	}
	// A slot that reported no rain block is not a slot reporting 1.2mm of rain and
	// not a slot reporting zero rain.
	nullKey(t, slots[1], "rain")

	// The rollup and the slots cannot disagree: the day pop is the highest slot
	// pop, and the legacy chance_of_rain is that same number as a percentage.
	if days[0]["pop"] != 0.8 {
		t.Fatalf("expected the day pop 0.8, got %#v", days[0]["pop"])
	}
	if days[0]["chance_of_rain"] != 80.0 {
		t.Fatalf("expected chance_of_rain 80 from pop 0.8, got %#v", days[0]["chance_of_rain"])
	}
}

// A rain window the upstream measured as 0 is a reading, not an absence, and must
// not collapse into the null that a slot with no rain block reports.
func TestForecastRouteReportsMeasuredZeroRain(t *testing.T) {
	_, days := requestForecastRoute(t, newForecastRouteRouter(t, forecastRouteZeroRainJSON(t)))

	if len(days) != 1 {
		t.Fatalf("expected 1 day from the fixture, got %d: %v", len(days), days)
	}
	slots := hourlySlots(t, days[0])
	if len(slots) != 2 {
		t.Fatalf("expected 2 slots on the day, got %d", len(slots))
	}

	rain := block(t, slots[0], "rain")
	threeHour, ok := rain["3h"]
	if !ok {
		t.Fatalf("expected a rain.3h key, got %#v", rain)
	}
	if threeHour != 0.0 {
		t.Fatalf("expected a measured rain.3h of 0, got %#v", threeHour)
	}
	// The 1h window is not documented for /data/2.5/forecast, so the key must be
	// absent. It was briefly present and null, which is a key the endpoint never
	// documented carrying no information; the forecast block types fix that by not
	// declaring the member at all.
	if value, ok := rain["1h"]; ok {
		t.Fatalf("expected no 1h key on a forecast rain block, got %#v", value)
	}
	nullKey(t, slots[1], "rain")

	// The day repeats the distinction. A slot that measured a zero gives a day
	// window of zero, and no snow was reported, so the day's snow block is null
	// rather than a block of zeroes.
	day := days[0]
	dayRain := block(t, day, "rain")
	if dayRain["3h"] != 0.0 {
		t.Fatalf("expected a day rain sum of 0, got %#v", dayRain["3h"])
	}
	if value, ok := dayRain["1h"]; ok {
		t.Fatalf("expected no 1h key on the day's rain, got %#v", value)
	}
	nullKey(t, day, "snow")
	// The legacy total is the sum of the two, so a measured zero is a real 0 and
	// never a null: the day did have an answer.
	if day["precipitation"] != 0.0 {
		t.Fatalf("expected the legacy precipitation 0, got %#v", day["precipitation"])
	}
}

func TestForecastHandlerReturnsCurrentData(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast?location=London,UK&units=metric", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	current := decodeCurrent(t, w.Body)
	if current == nil {
		t.Fatal("expected a current block in the forecast response")
	}
	if current["temperature"] != 18.0 {
		t.Fatalf("expected current temperature 18 got %v", current["temperature"])
	}
	if current["condition"] != "Rain" || current["description"] != "Light Rain" {
		t.Fatalf("unexpected current condition %v", current)
	}
	if updated, _ := current["last_updated"].(string); strings.HasPrefix(updated, "0001-01-01") {
		t.Fatalf("expected a real last_updated, got %q", updated)
	}
}

// The forecast route must keep costing exactly one upstream call: current data is
// derived from the forecast payload, never fetched separately.
func TestForecastHandlerDoesNotCallCurrentEndpoint(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast?location=London,UK&units=metric", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&stub.forecastCalls); got != 1 {
		t.Fatalf("expected 1 forecast call, got %d", got)
	}
	if got := atomic.LoadInt32(&stub.currentCalls); got != 0 {
		t.Fatalf("expected no current weather calls, got %d", got)
	}
}

func TestSevenDayForecastHandlerReturnsCurrentData(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast/7day?location=London,UK&units=metric", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}

	current := decodeCurrent(t, w.Body)
	if current == nil {
		t.Fatal("expected a current block in the seven day response")
	}
	if current["temperature"] != 11.5 || current["humidity"] != 78.0 {
		t.Fatalf("unexpected current conditions %v", current)
	}
	if current["visibility"] != 8000.0 {
		t.Fatalf("expected current visibility 8000, got %v", current["visibility"])
	}
	if current["condition"] != "Clouds" {
		t.Fatalf("expected current condition Clouds, got %v", current["condition"])
	}
	if got := atomic.LoadInt32(&stub.currentCalls); got != 0 {
		t.Fatalf("expected no current weather calls, got %d", got)
	}
}

func TestGetSevenDayForecastHandler(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	for attempt, wantHeader := range []string{"MISS", "HIT"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast/7day?location=London,UK&units=metric", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected 200 got %d body=%s", attempt+1, w.Code, w.Body.String())
		}
		if got := decodeForecastCount(t, w.Body); got != 7 {
			t.Fatalf("attempt %d: expected 7 forecast days got %d", attempt+1, got)
		}
		if got := w.Header().Get("X-Cache"); got != wantHeader {
			t.Fatalf("attempt %d: expected X-Cache %s got %q", attempt+1, wantHeader, got)
		}
	}

	if got := atomic.LoadInt32(&stub.oneCallCalls); got != 1 {
		t.Fatalf("expected 1 one call request across 2 attempts, got %d", got)
	}
}

func TestPostSevenDayForecastHandler(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	body := bytes.NewBufferString(`{"location":"London,UK","units":"metric"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/weather/forecast/7day", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if got := decodeForecastCount(t, w.Body); got != 7 {
		t.Fatalf("expected 7 forecast days got %d", got)
	}
}

func TestSevenDayForecastHandlerRejectsMissingLocation(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast/7day", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&stub.geocodeCalls); got != 0 {
		t.Fatalf("expected no geocode call for invalid input, got %d", got)
	}
}

func TestSevenDayForecastHandlerMapsUnknownLocationTo404(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast/7day?location=Nowhere", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Location not found") {
		t.Fatalf("expected a location not found message, got %s", w.Body.String())
	}
}

func TestSevenDayForecastHandlerForwardsCallerKey(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast/7day?location=London,UK&key=caller-key", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	query, _ := stub.lastQuery.Load().(string)
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("failed to parse upstream query %q: %v", query, err)
	}
	if values.Get("appid") != "caller-key" {
		t.Fatalf("expected caller key to be forwarded, got %q", values.Get("appid"))
	}
}

// The seven day route must mirror the whole One Call daily block, not a rollup of
// it. The time of day breakdown is the part a rollup throws away, and it is the
// part a client reading a forecast for a picnic or a commute actually needs.
func TestSevenDayRouteExposesFullDailyBreakdown(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	onecall := block(t, data, "onecall")
	days := array(t, onecall, "daily")
	// Both numbers are literals, not the constant that drives the fixture. A
	// self-referential assertion shrinks with the fixture and proves nothing, which
	// is the failure mode this test is here to catch on the production side.
	if sevenDayRouteDayCount != 8 {
		t.Fatalf("expected the fixture to carry 8 days, got %d", sevenDayRouteDayCount)
	}
	if len(days) != 8 {
		t.Fatalf("expected the faithful daily[] to carry all 8 days the fixture sent, got %d", len(days))
	}

	day := days[0]
	temp := block(t, day, "temp")
	for name, want := range map[string]float64{
		"day": 15, "min": 10, "max": 20, "night": 9, "morn": 11, "eve": 16,
	} {
		if temp[name] != want {
			t.Fatalf("expected daily[0].temp.%s to be %v, got %#v", name, want, temp[name])
		}
	}

	feelsLike := block(t, day, "feels_like")
	for name, want := range map[string]float64{
		"day": 14, "night": 8, "morn": 10, "eve": 15,
	} {
		if feelsLike[name] != want {
			t.Fatalf("expected daily[0].feels_like.%s to be %v, got %#v", name, want, feelsLike[name])
		}
	}

	// The lunar members are documented for this endpoint and are part of the day.
	if day["moon_phase"] != 0.42 {
		t.Fatalf("expected daily[0].moon_phase 0.42, got %#v", day["moon_phase"])
	}
	moonrise := sevenDayRouteBase.Add(-9 * time.Hour).Unix()
	moonset := sevenDayRouteBase.Add(9 * time.Hour).Unix()
	if day["moonrise"] != float64(moonrise) || day["moonset"] != float64(moonset) {
		t.Fatalf("expected daily[0] moonrise %d and moonset %d, got %#v and %#v", moonrise, moonset, day["moonrise"], day["moonset"])
	}
	if day["dt"] != float64(sevenDayRouteBase.Unix()) {
		t.Fatalf("expected daily[0].dt %d, got %#v", sevenDayRouteBase.Unix(), day["dt"])
	}
}

// dew_point and wind_gust are the two members the daily endpoint gained after the
// first release of the API, so a mirror built from an older field list drops them
// silently and nothing else notices.
func TestSevenDayRouteExposesDewPointAndWindGust(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	days := array(t, block(t, data, "onecall"), "daily")
	day := days[0]
	if day["dew_point"] != 7.7 {
		t.Fatalf("expected daily[0].dew_point 7.7, got %#v", day["dew_point"])
	}
	if day["wind_gust"] != 9.1 {
		t.Fatalf("expected daily[0].wind_gust 9.1, got %#v", day["wind_gust"])
	}
}

// The faithful daily array is whatever the upstream sent. The legacy array keeps
// its cap of seven, because that is part of the contract consumers already read.
func TestSevenDayDailyMayExceedLegacyForecastLength(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	onecall := block(t, data, "onecall")
	days := array(t, onecall, "daily")
	legacy := array(t, data, "forecast")
	// Literals again, and the two are different numbers on purpose: that difference is
	// the whole claim, and an assertion that read one constant for both would pass on
	// a mapper that capped both.
	if len(days) != 8 {
		t.Fatalf("expected the faithful daily[] to carry all 8 days the fixture sent, got %d", len(days))
	}
	if len(legacy) != 7 {
		t.Fatalf("expected the legacy forecast[] to stay capped at 7, got %d", len(legacy))
	}
	// The eighth day is only in the faithful array, and it is a day the legacy cut
	// would have taken had it not stopped.
	if days[7]["dt"] == legacy[6]["dt"] {
		t.Fatal("expected the eighth day to be absent from the capped legacy array")
	}
}

// The envelope scalars are the coordinates the route geocoded and the zone the
// upstream reports, and they are always present: the onecall key is not an opt-in.
func TestSevenDayRouteMirrorsOneCallEnvelope(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	onecall := block(t, data, "onecall")
	if onecall["lat"] != 51.5074 || onecall["lon"] != -0.1278 {
		t.Fatalf("expected the onecall coordinates from the upstream body, got %#v and %#v", onecall["lat"], onecall["lon"])
	}
	if onecall["timezone"] != "Europe/London" || onecall["timezone_offset"] != 0.0 {
		t.Fatalf("expected the onecall timezone, got %#v at offset %#v", onecall["timezone"], onecall["timezone_offset"])
	}

	current := block(t, onecall, "current")
	if current["temp"] != 11.5 || current["dew_point"] != 7.7 || current["uvi"] != 1.8 || current["wind_gust"] != 9.1 {
		t.Fatalf("expected the faithful current block to mirror the upstream, got %#v", current)
	}
	// The current block documents no probability, so the response must not invent
	// one beside the block.
	if _, ok := current["pop"]; ok {
		t.Fatalf("expected no pop on the faithful current block, got %#v", current["pop"])
	}

	// The legacy block keeps its own vocabulary at the top level, alongside the
	// namespace rather than inside it.
	if location := block(t, data, "location"); location["name"] != "London" {
		t.Fatalf("expected the legacy location.name London, got %#v", location)
	}
	if current := block(t, data, "current"); current["temperature"] != 11.5 {
		t.Fatalf("expected the legacy current.temperature 11.5, got %#v", current)
	}
	if _, ok := data["request_time"]; !ok {
		t.Fatalf("expected a request_time key, got %#v", data)
	}
}

// The three opt-in blocks used to be excluded upstream. The route has to ask for
// all of them: a missing exclude costs no extra request and no extra quota, and the
// blocks are the whole reason a client would choose this route over the 5 day one.
func TestSevenDayRouteAlwaysFetchesEveryBlock(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/forecast/7day?location=London,UK&units=metric", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", w.Code, w.Body.String())
	}
	if got := atomic.LoadInt32(&stub.oneCallCalls); got != 1 {
		t.Fatalf("expected 1 one call request, got %d", got)
	}

	query, _ := stub.lastQuery.Load().(string)
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("failed to parse upstream query %q: %v", query, err)
	}
	if values.Has("exclude") {
		t.Fatalf("expected no exclude parameter upstream, got %q", values.Get("exclude"))
	}
}

// Every block the mapper is given has to reach the response. minutely, hourly and
// alerts are opt-in at the route level, which is an exposure decision taken after
// the mapper runs, not a reason for the mapper to drop them.
func TestSevenDayRouteCarriesEveryUpstreamBlock(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	onecall := block(t, data, "onecall")
	minutely := array(t, onecall, "minutely")
	if len(minutely) != 1 || minutely[0]["precipitation"] != 0.12 {
		t.Fatalf("expected the fixture's single minutely entry, got %#v", minutely)
	}
	// minutely[].precipitation is a probability, not a volume, and dt is the minute
	// it applies to. A test that only counted the entries would pass on a mapper
	// that swapped them.
	if minutely[0]["dt"] != 1772000060.0 {
		t.Fatalf("expected minutely[0].dt 1772000060, got %#v", minutely[0]["dt"])
	}

	hourly := array(t, onecall, "hourly")
	if len(hourly) != 1 || hourly[0]["temp"] != 11.5 || hourly[0]["pop"] != 0.2 {
		t.Fatalf("expected the fixture's single hourly entry, got %#v", hourly)
	}

	alerts := array(t, onecall, "alerts")
	if len(alerts) != 1 || alerts[0]["event"] != "Flood warning" || alerts[0]["sender_name"] != "Met Office" {
		t.Fatalf("expected the fixture's single alert, got %#v", alerts)
	}
}

// A day the upstream could not measure reports null, in both vocabularies. The
// failure this guards against is a day that reported nothing emitting a row of
// zeroes, which reads as a forecast of exactly average weather rather than as a
// gap.
func TestSevenDayRouteNullsUnmeasurableDailyMembers(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteSparseDayJSON(t)))

	days := array(t, block(t, data, "onecall"), "daily")
	legacy := array(t, data, "forecast")
	if len(days) != 2 || len(legacy) != 2 {
		t.Fatalf("expected 2 days from the fixture, got %d faithful and %d legacy", len(days), len(legacy))
	}

	// Day 0 sent no temp, feels_like, humidity, dew_point, wind, pop, rain, snow
	// or uvi. Every one of those is a key the schema documents, so each is present
	// and null rather than dropped or zeroed.
	sparse := days[0]
	nullKey(t, sparse, "temp")
	nullKey(t, sparse, "feels_like")
	for _, name := range []string{
		"humidity", "dew_point", "wind_speed", "wind_deg", "wind_gust",
		"pop", "rain", "snow", "uvi",
	} {
		nullKey(t, sparse, name)
	}
	// The members the fixture did send are still reported, so the day is a day with
	// gaps rather than an empty one.
	if sparse["dt"] != float64(sevenDayRouteBase.Unix()) || sparse["pressure"] != 1015.0 || sparse["clouds"] != 40.0 {
		t.Fatalf("expected the sparse day to report the members it sent, got %#v", sparse)
	}

	// The legacy half of the same day is null in exactly the same places. These are
	// the keys that used to be a value type, so a mapper that assigned a zeroed
	// local instead of a pointer would compile and emit a fabricated reading.
	legacySparse := legacy[0]
	for _, name := range []string{
		"max_temperature", "min_temperature", "avg_temperature",
		"humidity", "wind_speed", "chance_of_rain", "uv_index",
	} {
		nullKey(t, legacySparse, name)
	}
	// A total of zero is a reading rather than an absence, so precipitation stays a
	// number: nothing fell, and that is an answer.
	if legacySparse["precipitation"] != 0.0 {
		t.Fatalf("expected a legacy precipitation of 0 for a day with no precipitation, got %#v", legacySparse["precipitation"])
	}

	// Day 1 measured a rain volume of 0 and sent no snow, so the day repeats the
	// difference: a measured zero and an absent member are two readings.
	measured := days[1]
	if measured["rain"] != 0.0 {
		t.Fatalf("expected a measured daily rain of 0, got %#v", measured["rain"])
	}
	nullKey(t, measured, "snow")
	// It sent no pop, so the legacy percentage is null rather than 0: 0 would claim
	// the upstream measured a zero chance of rain.
	nullKey(t, legacy[1], "chance_of_rain")
	if legacy[1]["max_temperature"] != 21.0 {
		t.Fatalf("expected day 1 max_temperature 21, got %#v", legacy[1]["max_temperature"])
	}
}

// The legacy current block and the faithful One Call current block are the same
// upstream object read twice. If they can disagree, one of them is lying.
func TestSevenDayLegacyCurrentAliasesFaithfulCurrent(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	legacy := block(t, data, "current")
	current := block(t, block(t, data, "onecall"), "current")

	for _, pair := range [][2]string{
		{"temperature", "temp"},
		{"feels_like", "feels_like"},
		{"humidity", "humidity"},
		{"pressure", "pressure"},
		{"visibility", "visibility"},
		{"wind_speed", "wind_speed"},
		{"wind_direction", "wind_deg"},
		{"wind_gust", "wind_gust"},
		{"cloud_cover", "clouds"},
	} {
		if legacy[pair[0]] != current[pair[1]] {
			t.Fatalf("expected legacy current.%s and onecall.current.%s to be the same reading, got %#v and %#v",
				pair[0], pair[1], legacy[pair[0]], current[pair[1]])
		}
	}

	// The legacy current temperatures are null on this route, and were 0 before the
	// block became pointer shaped. The One Call current block reports no min or max,
	// so the 0 was a reading nobody made, and an unmeasurable legacy key now says so
	// instead. The faithful twin of the two is a time of day breakdown on the day,
	// not on the current block.
	nullKey(t, legacy, "max_temperature")
	nullKey(t, legacy, "min_temperature")
	if _, ok := current["temp_max"]; ok {
		t.Fatalf("expected no max_temperature on the faithful current block, got %#v", current["temp_max"])
	}
}

// 0.29 is the probability whose percentage truncates in binary floating point. A
// truncation reports 28 for a day the upstream called 29 percent, and the 5 day
// route rounds, so the same probability would read differently on two routes.
func TestSevenDayChanceOfRainRoundsTheDailyProbability(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	days := array(t, block(t, data, "onecall"), "daily")
	legacy := array(t, data, "forecast")
	if days[0]["pop"] != 0.29 {
		t.Fatalf("expected the fixture pop 0.29, got %#v", days[0]["pop"])
	}
	if legacy[0]["chance_of_rain"] != 29.0 {
		t.Fatalf("expected chance_of_rain 29 from pop 0.29, got %#v", legacy[0]["chance_of_rain"])
	}
}

// Both vocabularies read the current block from one upstream object, so when there
// is no object they have to agree that there is nothing. The legacy block used to
// answer with a temperature of 0, a pressure of 0, a visibility of 0, a last_updated
// of 1970 and an empty condition beside a faithful block that said null: five
// fabrications and one invented date, and nothing in the body said so.
func TestSevenDayRouteNullsLegacyCurrentWhenBlockAbsent(t *testing.T) {
	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteNoCurrentJSON(t)))

	nullKey(t, block(t, data, "onecall"), "current")

	legacy := block(t, data, "current")
	for _, name := range []string{
		"temperature", "feels_like", "humidity", "pressure", "visibility",
		"wind_speed", "wind_direction", "cloud_cover", "condition", "description",
		"icon", "max_temperature", "min_temperature", "last_updated",
	} {
		nullKey(t, legacy, name)
	}
	// wind_gust is the one key that carries omitempty, so an absent gust is an absent
	// key rather than a null one. That is unchanged from before: a zero was dropped
	// by the same tag, and a nil is dropped by it now.
	if value, ok := legacy["wind_gust"]; ok {
		t.Fatalf("expected no wind_gust key on a current block that was never sent, got %#v", value)
	}

	// The rest of the response is unaffected: the day was there and the day is still
	// reported, with the same rounded probability the full fixture produces.
	legacyDays := array(t, data, "forecast")
	if len(legacyDays) != 1 || legacyDays[0]["chance_of_rain"] != 29.0 {
		t.Fatalf("expected the day to be reported with a rounded probability, got %#v", legacyDays)
	}
	days := array(t, block(t, data, "onecall"), "daily")
	if len(days) != 1 || days[0]["temp"] == nil {
		t.Fatalf("expected the faithful day to be reported, got %#v", days)
	}
}

// The legacy date is the one rendering on this route that depends on the server's
// zone. The upstream documents a daily dt in UTC, so the day component of this string
// has to be the UTC one on every host: without .UTC() the same upstream body
// serialises a different date in Berlin than in Auckland.
func TestSevenDayLegacyDateRendersInUTC(t *testing.T) {
	// The process zone is pinned for the duration of this test and restored after it.
	// Without the pin the test passes in TZ=UTC whatever the code does, and CI runs
	// UTC, so the guard would be inert where it matters most. Safe because no test in
	// this package calls t.Parallel, so nothing else can observe the swap.
	original := time.Local
	time.Local = time.FixedZone("seven-day-test", 5*60*60)
	t.Cleanup(func() { time.Local = original })

	data := requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	days := array(t, data, "forecast")
	if len(days) != 7 {
		t.Fatalf("expected the 7 legacy days from the fixture, got %d", len(days))
	}
	// sevenDayRouteBase is 2026-03-01 12:00:00 UTC, and this is that instant written
	// out literally rather than derived from the same expression the mapper uses. In
	// the pinned +05:00 zone the day without .UTC() renders as 17:00+05:00.
	if want := "2026-03-01T12:00:00Z"; days[0]["date"] != want {
		t.Fatalf("expected the legacy date to render as %q, got %#v", want, days[0]["date"])
	}
	// The faithful side has no date member of its own: the upstream's dt is reported as
	// the epoch, and this test pins the rendering of the legacy alias only.
	if days[1]["date"] != "2026-03-02T12:00:00Z" {
		t.Fatalf("expected the second legacy date to render as %q, got %#v", "2026-03-02T12:00:00Z", days[1]["date"])
	}
}

func TestCurrentWeatherHandlerCachesWithinTTL(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	for attempt, wantHeader := range []string{"MISS", "HIT"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/current?location=London,UK&units=metric", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected 200 got %d body=%s", attempt+1, w.Code, w.Body.String())
		}
		if got := w.Header().Get("X-Cache"); got != wantHeader {
			t.Fatalf("attempt %d: expected X-Cache %s got %q", attempt+1, wantHeader, got)
		}
	}

	if got := atomic.LoadInt32(&stub.currentCalls); got != 1 {
		t.Fatalf("expected 1 current weather call across 2 attempts, got %d", got)
	}
}

func TestCurrentWeatherHandlerDoesNotShareCacheAcrossKeys(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	first := httptest.NewRequest(http.MethodGet, "/api/v1/weather/current?location=London,UK", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, first)

	second := httptest.NewRequest(http.MethodGet, "/api/v1/weather/current?location=London,UK&key=other-key", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, second)

	if got := w.Header().Get("X-Cache"); got != "MISS" {
		t.Fatalf("expected a different key to miss the cache, got %q", got)
	}
	if got := atomic.LoadInt32(&stub.currentCalls); got != 2 {
		t.Fatalf("expected 2 upstream calls for different keys, got %d", got)
	}
}

func TestForecastHandlerCachesPerDayCount(t *testing.T) {
	stub := &upstreamStub{}
	router := newStubbedRouter(t, stub)

	for _, target := range []string{
		"/api/v1/weather/forecast?location=London,UK&days=1",
		"/api/v1/weather/forecast?location=London,UK&days=1",
		"/api/v1/weather/forecast?location=London,UK&days=5",
	} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200 got %d body=%s", target, w.Code, w.Body.String())
		}
	}

	if got := atomic.LoadInt32(&stub.forecastCalls); got != 2 {
		t.Fatalf("expected 2 upstream calls for 2 distinct day counts, got %d", got)
	}
}
