package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"weathering-with-go/services"

	"github.com/gin-gonic/gin"
)

// transportRedirect rewrites requests to point to the test server URL
type transportRedirect struct {
	target string
}

func (t *transportRedirect) RoundTrip(req *http.Request) (*http.Response, error) {
	// clone request
	r2 := new(http.Request)
	*r2 = *req
	// rewrite to target
	if strings.HasPrefix(r2.URL.String(), services.OpenWeatherMapBaseURL) || r2.URL.Host != "" {
		// replace host+scheme with target
		target := strings.TrimPrefix(t.target, "http://")
		r2.URL.Scheme = "http"
		r2.URL.Host = target
	}
	return http.DefaultTransport.RoundTrip(r2)
}

func TestGetCurrentWeatherHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// start a test server to simulate OpenWeatherMap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"coord":{"lon":4.56,"lat":1.23},"weather":[{"main":"Clear","description":"clear sky","icon":"01d"}],"main":{"temp":10.5,"feels_like":9,"pressure":1012,"humidity":80},"wind":{"speed":3.4,"deg":180},"clouds":{"all":0},"dt":1234567890,"sys":{"country":"GB"},"name":"Testville","cod":200}`)
	}))
	defer srv.Close()

	svc := services.NewWeatherService("dummy")
	svc.HTTPClient = &http.Client{Transport: &transportRedirect{target: srv.URL}}

	wh := NewWeatherHandler(svc)
	router.GET("/api/v1/weather/current", wh.GetCurrentWeather)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/current?location=Testville&units=metric", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK got %d body=%s", w.Code, w.Body.String())
	}
}

// currentRouteMirroredJSON is a /data/2.5/weather body that omits every optional
// member: no rain, no snow, no wind.gust, no sea_level, no grnd_level.
const currentRouteMirroredJSON = `{"coord":{"lon":-0.1257,"lat":51.5085},"weather":[{"id":802,"main":"Clouds","description":"scattered clouds","icon":"03d"}],"base":"stations","main":{"temp":15.5,"feels_like":14.8,"temp_min":14.0,"temp_max":17.0,"pressure":1013,"humidity":72,"temp_kf":0.6},"visibility":10000,"wind":{"speed":3.6,"deg":230},"clouds":{"all":40},"dt":1772000000,"sys":{"type":2,"id":5081,"country":"GB","sunrise":1771960000,"sunset":1772010000},"id":2643743,"timezone":0,"name":"London","cod":200}`

// currentRouteZeroRainJSON is the same body with a rain block the upstream
// genuinely measured as 0, which is not the same as sending no rain block.
const currentRouteZeroRainJSON = `{"coord":{"lon":-0.1257,"lat":51.5085},"weather":[{"id":500,"main":"Rain","description":"light rain","icon":"10d"}],"base":"stations","main":{"temp":12.0,"feels_like":10.4,"temp_min":11.0,"temp_max":13.0,"pressure":1008,"humidity":90},"visibility":6000,"wind":{"speed":4.1,"deg":80,"gust":6.1},"clouds":{"all":90},"rain":{"1h":0},"dt":1772000000,"sys":{"type":2,"id":5081,"country":"GB","sunrise":1771960000,"sunset":1772010000},"id":2643743,"timezone":0,"name":"London","cod":200}`

// newCurrentRouteRouter serves one fixed upstream body to the current route, so
// the response can be asserted field by field without touching the network.
func newCurrentRouteRouter(t *testing.T, upstream string) *gin.Engine {
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
	router.GET("/api/v1/weather/current", NewWeatherHandler(svc).GetCurrentWeather)
	return router
}

func requestCurrentRoute(t *testing.T, router *gin.Engine) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/weather/current?location=London,UK&units=metric", nil)
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

// block returns the nested object at key, failing when it is not an object.
func block(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	nested, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("expected an object at %q, got %#v", key, parent[key])
	}
	return nested
}

// nullKey asserts key is present and null, which is how a field the upstream had
// no value for is reported. An absent key would be a different thing.
func nullKey(t *testing.T, parent map[string]any, key string) {
	t.Helper()
	value, ok := parent[key]
	if !ok {
		t.Fatalf("expected key %q to be present, got %#v", key, parent)
	}
	if value != nil {
		t.Fatalf("expected key %q to be null, got %#v", key, value)
	}
}

// The current route carries a faithful mirror of the /data/2.5/weather schema
// alongside the unchanged legacy keys. A member the upstream did not send is
// reported as null, never dropped and never invented.
func TestCurrentRouteMirrorsUpstreamAndNullsAbsentFields(t *testing.T) {
	data := requestCurrentRoute(t, newCurrentRouteRouter(t, currentRouteMirroredJSON))

	main := block(t, data, "main")
	if main["temp"] != 15.5 {
		t.Fatalf("expected main.temp 15.5, got %#v", main["temp"])
	}
	if main["temp_kf"] != 0.6 {
		t.Fatalf("expected main.temp_kf 0.6, got %#v", main["temp_kf"])
	}
	nullKey(t, main, "sea_level")
	nullKey(t, main, "grnd_level")

	nullKey(t, data, "rain")
	nullKey(t, data, "snow")

	wind := block(t, data, "wind")
	if wind["speed"] != 3.6 {
		t.Fatalf("expected wind.speed 3.6, got %#v", wind["speed"])
	}
	nullKey(t, wind, "gust")

	if timezone, ok := data["timezone"]; !ok || timezone != 0.0 {
		t.Fatalf("expected timezone 0 to be reported as a number, got %#v", data["timezone"])
	}

	// The legacy vocabulary still carries the values it always carried.
	location := block(t, data, "location")
	if location["name"] != "London" {
		t.Fatalf("expected legacy location.name London, got %#v", location["name"])
	}
	current := block(t, data, "current")
	if current["temperature"] != 15.5 {
		t.Fatalf("expected legacy current.temperature 15.5, got %#v", current["temperature"])
	}

	// /data/2.5/weather has no pop, so the route reports no chance of rain at all.
	if _, ok := current["chance_of_rain"]; ok {
		t.Fatalf("expected no chance_of_rain key in the legacy block, got %#v", current)
	}
}

// A rain block the upstream measured as 0 is a reading, not an absence, and must
// not collapse into the null that a missing block reports.
func TestCurrentRouteReportsMeasuredZeroRain(t *testing.T) {
	data := requestCurrentRoute(t, newCurrentRouteRouter(t, currentRouteZeroRainJSON))

	rain := block(t, data, "rain")
	oneHour, ok := rain["1h"]
	if !ok {
		t.Fatalf("expected a rain.1h key, got %#v", rain)
	}
	if oneHour != 0.0 {
		t.Fatalf("expected a measured rain.1h of 0, got %#v", oneHour)
	}
}
