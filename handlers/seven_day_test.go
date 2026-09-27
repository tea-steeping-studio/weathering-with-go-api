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

// forecastLocalMidnight anchors the forecast route fixtures. The route groups slots
// by the date a timestamp formats to in this process's zone, so a fixed UTC instant
// would land on two grouped days on some hosts. Anchoring on local midnight keeps
// every fixture on exactly one day wherever the tests run.
var forecastLocalMidnight = time.Date(2026, 2, 25, 0, 0, 0, 0, time.Local)

// forecastSlot renders one raw three hour slot. A member left out of members is
// left out of the body, which is how a fixture says the upstream never sent it.
func forecastSlot(offsetHours int, members map[string]any) map[string]any {
	dt := forecastLocalMidnight.Add(time.Duration(offsetHours) * time.Hour).Unix()
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
	return fmt.Sprintf(`{"cod":"200","message":0,"cnt":%d,"list":[%s],"city":{"id":2643743,"name":"London","coord":{"lon":-0.13,"lat":51.51},"country":"GB","population":7556900,"timezone":0,"sunrise":1771999200,"sunset":1772030400}}`,
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
	// The 1h window is not documented for /data/2.5/forecast, so it can never carry
	// a reading here. The shared RainBlock tags it as a required key, so it reports
	// null instead of being absent, and this assertion records that as a pending
	// ruling on whether the forecast route needs its own precipitation block types.
	// It is not an endorsement of the key.
	if value, ok := rain["1h"]; !ok || value != nil {
		t.Fatalf("expected rain.1h to be present and carry no reading, got %#v (present %t)", value, ok)
	}
	nullKey(t, slots[1], "rain")
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
