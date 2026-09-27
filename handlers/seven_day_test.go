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

func sevenDayJSON() string {
	days := make([]string, 0, 8)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		days = append(days, fmt.Sprintf(`{"dt":%d,"temp":{"day":%d.0,"min":%d.0,"max":%d.0},"pressure":1015,"humidity":%d,"wind_speed":4.0,"wind_deg":200,"clouds":40,"pop":0.25,"uvi":3.5,"weather":[{"id":801,"main":"Clouds","description":"scattered clouds","icon":"03d"}]}`,
			base.AddDate(0, 0, i).Unix(), 15+i, 10+i, 20+i, 60+i))
	}
	return fmt.Sprintf(`{"lat":51.5074,"lon":-0.1278,"timezone":"Europe/London","daily":[%s]}`, strings.Join(days, ","))
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
	return len(resp.Data.Forecast)
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
