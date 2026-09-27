package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// transportRedirect rewrites upstream requests to the test server.
type transportRedirect struct {
	target string
}

func (t *transportRedirect) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := new(http.Request)
	*r2 = *req
	if r2.URL.Host != "" {
		target := strings.TrimPrefix(t.target, "http://")
		r2.URL.Scheme = "http"
		r2.URL.Host = target
	}
	return http.DefaultTransport.RoundTrip(r2)
}

const londonGeocodeJSON = `[{"name":"London","lat":51.5074,"lon":-0.1278,"country":"GB","state":"England"}]`

func dailyJSON() string {
	days := make([]string, 0, 8)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		dt := base.AddDate(0, 0, i).Unix()
		days = append(days, fmt.Sprintf(`{"dt":%d,"temp":{"day":%d.0,"min":%d.0,"max":%d.0,"night":10.0,"feels_like_day":18.0},"feels_like":{"day":18.0},"pressure":1015,"humidity":%d,"wind_speed":4.0,"wind_deg":200,"clouds":40,"pop":0.25,"rain":1.5,"snow":0,"uvi":3.5,"weather":[{"id":801,"main":"Clouds","description":"scattered clouds","icon":"03d"}]}`,
			dt, 15+i, 10+i, 20+i, 60+i))
	}
	return fmt.Sprintf(`{"lat":51.5074,"lon":-0.1278,"timezone":"Europe/London","daily":[%s]}`, strings.Join(days, ","))
}

type sevenDayStub struct {
	geocodeCalls  int32
	oneCallCalls  int32
	geocodeStatus int
}

func newSevenDayService(t *testing.T, stub *sevenDayStub) *WeatherService {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/geo/1.0/direct"):
			atomic.AddInt32(&stub.geocodeCalls, 1)
			if stub.geocodeStatus != 0 && stub.geocodeStatus != http.StatusOK {
				w.WriteHeader(stub.geocodeStatus)
				fmt.Fprintln(w, `{"cod":404,"message":"not found"}`)
				return
			}
			if r.URL.Query().Get("q") == "London,UK" {
				fmt.Fprintln(w, londonGeocodeJSON)
				return
			}
			fmt.Fprintln(w, `[]`)
		case strings.HasPrefix(r.URL.Path, "/data/3.0/onecall"):
			atomic.AddInt32(&stub.oneCallCalls, 1)
			fmt.Fprintln(w, dailyJSON())
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	svc := NewWeatherService("dummy")
	svc.HTTPClient = &http.Client{Transport: &transportRedirect{target: srv.URL}}
	return svc
}

func TestGetSevenDayForecastReturnsSevenDays(t *testing.T) {
	stub := &sevenDayStub{}
	svc := newSevenDayService(t, stub)

	data, hit, err := svc.GetSevenDayForecast("London,UK", "metric", "user-key")
	if err != nil {
		t.Fatalf("GetSevenDayForecast returned error: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss on first call")
	}
	if len(data.Forecast) != 7 {
		t.Fatalf("expected 7 forecast days, got %d", len(data.Forecast))
	}
	if data.Location.Name != "London" || data.Location.Country != "GB" || data.Location.Region != "England" {
		t.Fatalf("unexpected geocoded location %+v", data.Location)
	}
	if data.Location.Latitude != 51.5074 || data.Location.Longitude != -0.1278 {
		t.Fatalf("unexpected geocoded coordinates %+v", data.Location)
	}

	first := data.Forecast[0]
	wantDate := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if !first.Date.Equal(wantDate) {
		t.Fatalf("expected first day %s got %s", wantDate, first.Date)
	}
	if first.MaxTemp != 20 || first.MinTemp != 10 || first.AvgTemp != 15 {
		t.Fatalf("unexpected temperatures %+v", first)
	}
	if first.Condition != "Clouds" || first.Icon != "03d" {
		t.Fatalf("unexpected condition %+v", first)
	}
	if first.ChanceOfRain != 25 {
		t.Fatalf("expected chance_of_rain 25 from pop 0.25, got %d", first.ChanceOfRain)
	}
	if first.Precipitation != 1.5 {
		t.Fatalf("expected precipitation 1.5, got %v", first.Precipitation)
	}
	if first.UVIndex != 3.5 {
		t.Fatalf("expected uv_index 3.5, got %v", first.UVIndex)
	}
	if first.Humidity != 60 || first.WindSpeed != 4 {
		t.Fatalf("unexpected humidity/wind %+v", first)
	}
	if first.Description != "Scattered Clouds" {
		t.Fatalf("expected title-cased description, got %q", first.Description)
	}

	if got := atomic.LoadInt32(&stub.geocodeCalls); got != 1 {
		t.Fatalf("expected 1 geocode call, got %d", got)
	}
	if got := atomic.LoadInt32(&stub.oneCallCalls); got != 1 {
		t.Fatalf("expected 1 one call request, got %d", got)
	}
}

func TestGetSevenDayForecastSendsUnitsAndKey(t *testing.T) {
	stub := &sevenDayStub{}
	svc := newSevenDayService(t, stub)

	var query string
	svc.HTTPClient.Transport = &recordingTransport{inner: svc.HTTPClient.Transport, query: &query}

	if _, _, err := svc.GetSevenDayForecast("London,UK", "imperial", "user-key"); err != nil {
		t.Fatalf("GetSevenDayForecast returned error: %v", err)
	}

	if !strings.Contains(query, "appid=user-key") {
		t.Fatalf("expected the caller key to be forwarded, got %q", query)
	}
	if !strings.Contains(query, "units=imperial") {
		t.Fatalf("expected units to be forwarded, got %q", query)
	}
}

type recordingTransport struct {
	inner http.RoundTripper
	query *string
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.HasPrefix(req.URL.Path, "/data/3.0/onecall") {
		*r.query = req.URL.RawQuery
	}
	return r.inner.RoundTrip(req)
}

func TestGetSevenDayForecastRejectsEmptyLocation(t *testing.T) {
	svc := NewWeatherService("dummy")
	if _, _, err := svc.GetSevenDayForecast("", "metric", ""); err == nil {
		t.Fatal("expected an error for an empty location")
	}
}

func TestGetSevenDayForecastReportsUnknownLocation(t *testing.T) {
	stub := &sevenDayStub{}
	svc := newSevenDayService(t, stub)

	_, _, err := svc.GetSevenDayForecast("Nowhere", "metric", "user-key")
	if err == nil {
		t.Fatal("expected an error for an unknown location")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("expected a 404 flavoured error for the handler to map, got %v", err)
	}
}

func TestGetSevenDayForecastCachesWithinTTL(t *testing.T) {
	stub := &sevenDayStub{}
	svc := newSevenDayService(t, stub)

	first, hit, err := svc.GetSevenDayForecast("London,UK", "metric", "user-key")
	if err != nil {
		t.Fatalf("first call returned error: %v", err)
	}
	if hit {
		t.Fatal("expected miss on first call")
	}

	second, hit, err := svc.GetSevenDayForecast("London,UK", "metric", "user-key")
	if err != nil {
		t.Fatalf("second call returned error: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit on second call")
	}
	if !second.RequestTime.Equal(first.RequestTime) {
		t.Fatal("expected cached data to keep its original request time")
	}

	if got := atomic.LoadInt32(&stub.oneCallCalls); got != 1 {
		t.Fatalf("expected 1 one call request, got %d", got)
	}
	if got := atomic.LoadInt32(&stub.geocodeCalls); got != 1 {
		t.Fatalf("expected geocoding to be cached too, got %d calls", got)
	}
}

func TestGetSevenDayForecastCachesGeocodingAcrossUnits(t *testing.T) {
	stub := &sevenDayStub{}
	svc := newSevenDayService(t, stub)

	if _, _, err := svc.GetSevenDayForecast("London,UK", "metric", "user-key"); err != nil {
		t.Fatalf("metric call returned error: %v", err)
	}
	if _, _, err := svc.GetSevenDayForecast("London,UK", "imperial", "user-key"); err != nil {
		t.Fatalf("imperial call returned error: %v", err)
	}

	if got := atomic.LoadInt32(&stub.geocodeCalls); got != 1 {
		t.Fatalf("expected geocode call to be shared across units, got %d", got)
	}
	if got := atomic.LoadInt32(&stub.oneCallCalls); got != 2 {
		t.Fatalf("expected 2 one call requests for different units, got %d", got)
	}
}

func TestGetSevenDayForecastDoesNotCacheUnknownLocation(t *testing.T) {
	stub := &sevenDayStub{}
	svc := newSevenDayService(t, stub)

	for i := 0; i < 2; i++ {
		if _, _, err := svc.GetSevenDayForecast("Nowhere", "metric", "user-key"); err == nil {
			t.Fatalf("attempt %d: expected an error", i+1)
		}
	}

	if got := atomic.LoadInt32(&stub.geocodeCalls); got != 2 {
		t.Fatalf("expected a fresh geocode call per attempt, got %d", got)
	}
}
