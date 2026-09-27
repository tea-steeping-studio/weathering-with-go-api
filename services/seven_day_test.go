package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"weathering-with-go/models"
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
	requireFloat(t, "the first max_temperature", first.MaxTemp, 20)
	requireFloat(t, "the first min_temperature", first.MinTemp, 10)
	requireFloat(t, "the first avg_temperature", first.AvgTemp, 15)
	if first.Condition != "Clouds" || first.Icon != "03d" {
		t.Fatalf("unexpected condition %+v", first)
	}
	requireInt(t, "chance_of_rain", first.ChanceOfRain, 25)
	if first.Precipitation != 1.5 {
		t.Fatalf("expected precipitation 1.5, got %v", first.Precipitation)
	}
	requireFloat(t, "the first uv_index", first.UVIndex, 3.5)
	requireInt(t, "the first humidity", first.Humidity, 60)
	requireFloat(t, "the first wind_speed", first.WindSpeed, 4)
	if first.Description != "Scattered Clouds" {
		t.Fatalf("expected title-cased description, got %q", first.Description)
	}

	// The faithful twin of the same day reads from the same upstream entry, so the
	// legacy key and the faithful key cannot report two different numbers.
	day := data.OneCall.Daily[0]
	if day.Temp == nil || day.Temp.Max == nil || *day.Temp.Max != 20 {
		t.Fatalf("expected the faithful daily temp max 20, got %#v", day.Temp)
	}
	if day.Pop == nil || *day.Pop != 0.25 {
		t.Fatalf("expected the faithful daily pop 0.25, got %#v", day.Pop)
	}

	if got := atomic.LoadInt32(&stub.geocodeCalls); got != 1 {
		t.Fatalf("expected 1 geocode call, got %d", got)
	}
	if got := atomic.LoadInt32(&stub.oneCallCalls); got != 1 {
		t.Fatalf("expected 1 one call request, got %d", got)
	}
}

// requireFloat asserts a pointer-shaped legacy reading against the value the upstream
// sent. Every legacy numeric key on this route is a pointer, because a day the
// upstream could not measure has to report null rather than a fabricated zero.
func requireFloat(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected %s to be %v, got null", what, want)
	}
	if *got != want {
		t.Fatalf("expected %s to be %v, got %v", what, want, *got)
	}
}

func requireInt(t *testing.T, what string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected %s to be %d, got null", what, want)
	}
	if *got != want {
		t.Fatalf("expected %s to be %d, got %d", what, want, *got)
	}
}

// requireInt64 compares an epoch member, which the faithful blocks carry as the
// upstream sends it rather than as a rendered timestamp.
func requireInt64(t *testing.T, what string, got *int64, want int64) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected %s to be %d, got null", what, want)
	}
	if *got != want {
		t.Fatalf("expected %s to be %d, got %d", what, want, *got)
	}
}

func requireString(t *testing.T, what string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected %s to be %q, got null", what, want)
	}
	if *got != want {
		t.Fatalf("expected %s to be %q, got %q", what, want, *got)
	}
}

// requireTime compares instants rather than renderings, so it says nothing about the
// zone the value carries. The UTC rendering of a legacy day is pinned by its own test
// in the handlers package, where the JSON is what is read.
func requireTime(t *testing.T, what string, got *time.Time, want time.Time) {
	t.Helper()
	if got == nil {
		t.Fatalf("expected %s to be %s, got null", what, want)
	}
	if !got.Equal(want) {
		t.Fatalf("expected %s to be %s, got %s", what, want, *got)
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

// The exclude parameter is gone. Asking for all of minutely, hourly and alerts
// costs no extra request, and it is the only way the mapper can carry them.
func TestGetSevenDayForecastNoLongerSendsExclude(t *testing.T) {
	stub := &sevenDayStub{}
	svc := newSevenDayService(t, stub)

	var query string
	svc.HTTPClient.Transport = &recordingTransport{inner: svc.HTTPClient.Transport, query: &query}

	if _, _, err := svc.GetSevenDayForecast("London,UK", "metric", "user-key"); err != nil {
		t.Fatalf("GetSevenDayForecast returned error: %v", err)
	}

	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatalf("failed to parse the forwarded query %q: %v", query, err)
	}
	if values.Has("exclude") {
		t.Fatalf("expected no exclude parameter upstream, got %q", values.Get("exclude"))
	}
	// The rest of the query is unchanged, so the fixture cannot pass on a request
	// that lost its coordinates or its key along with the parameter.
	if values.Get("lat") != "51.5074" || values.Get("lon") != "-0.1278" {
		t.Fatalf("expected the geocoded coordinates to be forwarded, got lat %q lon %q", values.Get("lat"), values.Get("lon"))
	}
	if values.Get("appid") != "user-key" || values.Get("units") != "metric" {
		t.Fatalf("expected the caller key and units to be forwarded, got appid %q units %q", values.Get("appid"), values.Get("units"))
	}
}

// oneCallBodyWithBlocks is a /data/3.0/onecall body carrying one entry in each of
// the three blocks the route used to exclude, so the mapper can be asked what it
// does with them.
const oneCallBodyWithBlocks = `{"lat":51.5074,"lon":-0.1278,"timezone":"Europe/London","timezone_offset":0,` +
	`"current":{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,"temp":11.5,"feels_like":10.2,` +
	`"pressure":1009,"humidity":78,"dew_point":7.7,"uvi":1.8,"clouds":75,"visibility":8000,` +
	`"wind_speed":6.2,"wind_deg":240,"wind_gust":9.1,"weather":[{"id":803,"main":"Clouds","description":"broken clouds","icon":"04d"}]},` +
	`"minutely":[{"dt":1772000060,"precipitation":0.12}],` +
	`"hourly":[{"dt":1772000000,"temp":11.5,"pop":0.2,"rain":0,"snow":0,"weather":[{"id":803,"main":"Clouds","description":"broken clouds","icon":"04d"}]}],` +
	`"alerts":[{"sender_name":"Met Office","event":"Flood warning","start":1772000000,"end":1772600000,` +
	`"description":"Flooding is possible.","tags":["Flood"]}],` +
	`"daily":[{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,"moonrise":1771970000,` +
	`"moonset":1772040000,"moon_phase":0.42,"temp":{"day":15.0,"min":10.0,"max":20.0,"night":9.0,"morn":11.0,"eve":16.0},` +
	`"feels_like":{"day":14.0,"night":8.0,"morn":10.0,"eve":15.0},"pressure":1015,"humidity":60,"dew_point":7.7,` +
	`"wind_speed":4.0,"wind_deg":200,"wind_gust":9.1,"clouds":40,"pop":0.29,"rain":1.5,"snow":0,"uvi":3.5,` +
	`"weather":[{"id":801,"main":"Clouds","description":"scattered clouds","icon":"03d"}]}]}`

// decodeOneCallBody decodes a body into both structs the mapper takes, exactly as
// fetchSevenDayForecast does. Going through the raw bytes is what lets a fixture
// distinguish a member the upstream sent as 0 from one it never sent.
func decodeOneCallBody(t *testing.T, body string) (models.OneCallResponse, models.SevenDayPayload) {
	t.Helper()
	var owm models.OneCallResponse
	if err := json.Unmarshal([]byte(body), &owm); err != nil {
		t.Fatalf("failed to decode the fixture into the upstream struct: %v", err)
	}
	var payload models.SevenDayPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("failed to decode the fixture into the payload: %v", err)
	}
	return owm, payload
}

func mapOneCallBody(t *testing.T, body string) *models.SevenDayResponse {
	t.Helper()
	owm, payload := decodeOneCallBody(t, body)
	return NewWeatherService("dummy").mapOneCall(owm, payload, &models.Location{Name: "London", Country: "GB"})
}

// The mapper is what produces every block; whether a block reaches a caller is a
// later decision. A mapper that dropped one would be invisible, because the legacy
// array does not carry any of the three.
// oneCallBodySparseCurrent is a /data/3.0/onecall body whose current block carries
// no dew_point, uvi or wind_gust. Those three arrived after the first release of the
// API, so a response body from older coverage is missing them, and reading their zero
// out of a non-pointer decode field would report a dew point of 0 degrees and a
// measured gust of 0 m/s: a real reading nobody made.
const oneCallBodySparseCurrent = `{"lat":51.5074,"lon":-0.1278,"timezone":"Europe/London","timezone_offset":0,` +
	`"current":{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,"temp":11.5,"feels_like":10.2,` +
	`"pressure":1009,"humidity":78,"clouds":75,"visibility":8000,"wind_speed":6.2,"wind_deg":240,` +
	`"weather":[{"id":803,"main":"Clouds","description":"broken clouds","icon":"04d"}]},` +
	`"daily":[{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,"moonrise":1771970000,` +
	`"moonset":1772040000,"moon_phase":0.42,"temp":{"day":15.0,"min":10.0,"max":20.0,"night":9.0,"morn":11.0,"eve":16.0},` +
	`"feels_like":{"day":14.0,"night":8.0,"morn":10.0,"eve":15.0},"pressure":1015,"humidity":60,` +
	`"wind_speed":4.0,"wind_deg":200,"clouds":40,"uvi":3.5,` +
	`"weather":[{"id":801,"main":"Clouds","description":"scattered clouds","icon":"03d"}]}]}`

// The three late additions to the current block are optional in practice, so the
// faithful half reports them null when the upstream left them out. The legacy half has
// no way to say null for a gust, so its key is absent instead, which is the same
// statement in the vocabulary it has always used.
func TestMapOneCallCurrentReportsAbsentMeasurementsAsNull(t *testing.T) {
	data := mapOneCallBody(t, oneCallBodySparseCurrent)

	if data.OneCall == nil || data.OneCall.Current == nil {
		t.Fatal("expected the current block to be reported, got none")
	}
	current := data.OneCall.Current
	if current.DewPoint != nil {
		t.Fatalf("expected a null current dew_point, got %v", *current.DewPoint)
	}
	if current.Uvi != nil {
		t.Fatalf("expected a null current uvi, got %v", *current.Uvi)
	}
	if current.WindGust != nil {
		t.Fatalf("expected a null current wind_gust, got %v", *current.WindGust)
	}
	// The members the fixture did send are still reported, so this is a block with
	// three gaps rather than an empty one.
	if current.Temp == nil || *current.Temp != 11.5 {
		t.Fatalf("expected the current temp 11.5, got %#v", current.Temp)
	}
	if current.Visibility == nil || *current.Visibility != 8000 {
		t.Fatalf("expected the current visibility 8000, got %#v", current.Visibility)
	}
	// The legacy gust is nil and carries omitempty, so an absent gust is an absent
	// key. The JSON is what it was before the block became pointer shaped, when a
	// zero was dropped by the same tag; what changed is that the null now comes from
	// the missing member rather than from a value the upstream never sent.
	if data.Current.WindGust != nil {
		t.Fatalf("expected no legacy wind_gust on a body that sent none, got %v", *data.Current.WindGust)
	}
	// The day's own late additions are optional too, and this body omits dew_point,
	// wind_gust, pop, rain and snow on its one day.
	day := data.OneCall.Daily[0]
	for _, member := range []struct {
		name string
		gone bool
	}{
		{"dew_point", day.DewPoint == nil},
		{"wind_gust", day.WindGust == nil},
		{"pop", day.Pop == nil},
		{"rain", day.Rain == nil},
		{"snow", day.Snow == nil},
	} {
		if !member.gone {
			t.Fatalf("expected a null daily %s on a body that omitted it, got a value", member.name)
		}
	}
	if legacy := data.Forecast[0]; legacy.ChanceOfRain != nil || legacy.UVIndex == nil {
		t.Fatalf("expected a null legacy chance_of_rain and a reported uv_index, got %#v and %#v", legacy.ChanceOfRain, legacy.UVIndex)
	}
}

// oneCallBodyZeroAndAbsent is the body that decides whether the opt-in blocks are
// pointer shaped. Its two hourly entries are the same hour with two different
// upstream answers: the first measured a rain volume of 0 and sent no snow, the
// second sent neither. A decode struct cannot tell those apart, so a response built
// from one reports 0 mm for a dry hour that was never asked about. The minutely
// entry is the same statement about a probability, and the alert with no sender is
// the same statement about a string.
const oneCallBodyZeroAndAbsent = `{"lat":51.5074,"lon":-0.1278,"timezone":"Europe/London","timezone_offset":0,` +
	`"current":{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,"temp":11.5,"feels_like":10.2,` +
	`"pressure":1009,"humidity":78,"dew_point":7.7,"uvi":1.8,"clouds":75,"visibility":8000,` +
	`"wind_speed":6.2,"wind_deg":240,"wind_gust":9.1,"weather":[{"id":803,"main":"Clouds","description":"broken clouds","icon":"04d"}]},` +
	`"minutely":[{"dt":1772000060,"precipitation":0},{"dt":1772000120}],` +
	`"hourly":[{"dt":1772000000,"temp":11.5,"pop":0,"rain":0,"weather":[{"id":803,"main":"Clouds","description":"broken clouds","icon":"04d"}]},` +
	`{"dt":1772003600,"temp":11.0,"pop":0.4,"weather":[{"id":800,"main":"Clear","description":"clear sky","icon":"01d"}]}],` +
	`"alerts":[{"event":"Flood warning","start":1772000000,"end":1772600000,"description":"Flooding is possible.","tags":[]}],` +
	`"daily":[{"dt":1772000000,"sunrise":1771960000,"sunset":1772010000,"moonrise":1771970000,` +
	`"moonset":1772040000,"moon_phase":0.42,"temp":{"day":15.0,"min":10.0,"max":20.0,"night":9.0,"morn":11.0,"eve":16.0},` +
	`"feels_like":{"day":14.0,"night":8.0,"morn":10.0,"eve":15.0},"pressure":1015,"humidity":60,` +
	`"wind_speed":4.0,"wind_deg":200,"clouds":40,"uvi":3.5,` +
	`"weather":[{"id":801,"main":"Clouds","description":"scattered clouds","icon":"03d"}]}]}`

// Inside an opt-in block a measured zero and an absent member have to be two
// different readings, exactly as they are on the current and daily blocks. Before
// these blocks were pointer shaped the response reported 0 mm of rain on an hour the
// upstream never mentioned rain on, and a probability of 0 for a minute it sent no
// reading for.
func TestSevenDayOptInBlocksTellAZeroFromAnAbsentMember(t *testing.T) {
	data := mapOneCallBody(t, oneCallBodyZeroAndAbsent)

	if data.OneCall == nil {
		t.Fatal("expected the onecall envelope, got none")
	}
	if len(data.OneCall.Hourly) != 2 || len(data.OneCall.Minutely) != 2 {
		t.Fatalf("expected 2 hourly and 2 minutely entries from the fixture, got %d and %d",
			len(data.OneCall.Hourly), len(data.OneCall.Minutely))
	}

	// Entry 0 measured a rain volume of 0 and sent no snow. Entry 1 sent neither.
	measured, absent := data.OneCall.Hourly[0], data.OneCall.Hourly[1]
	requireFloat(t, "the measured zero hourly rain", measured.Rain, 0)
	if measured.Snow != nil {
		t.Fatalf("expected no snow reading on the first hourly entry, got %v", *measured.Snow)
	}
	// This is the pair the old decode-type response collapsed: 0 and null.
	if absent.Rain != nil || absent.Snow != nil {
		t.Fatalf("expected no precipitation reading on the second hourly entry, got rain %v snow %v",
			absent.Rain, absent.Snow)
	}
	// A probability the upstream measured as 0 is a reading, on the hourly block and
	// on the minutely one.
	requireFloat(t, "the measured zero hourly pop", measured.Pop, 0)
	requireFloat(t, "the first minutely precipitation", data.OneCall.Minutely[0].Precipitation, 0)
	// A member the upstream left out of an entry that is otherwise there is null.
	if data.OneCall.Minutely[1].Precipitation != nil {
		t.Fatalf("expected a null minutely precipitation on the second entry, got %v", *data.OneCall.Minutely[1].Precipitation)
	}
	// The entries the upstream did send are still reported, so entry 1 is an entry
	// with two gaps rather than an empty object.
	requireFloat(t, "the second hourly temp", absent.Temp, 11.0)
	requireFloat(t, "the second hourly pop", absent.Pop, 0.4)

	// The same rule for a string: an alert whose sender the upstream omitted is null,
	// not the empty string. A government warning with no sender is worth telling
	// apart from one whose sender is blank.
	if len(data.OneCall.Alerts) != 1 {
		t.Fatalf("expected the one alert from the fixture, got %d", len(data.OneCall.Alerts))
	}
	alert := data.OneCall.Alerts[0]
	if alert.SenderName != nil {
		t.Fatalf("expected a null alert sender_name, got %q", *alert.SenderName)
	}
	requireString(t, "the alert event", alert.Event, "Flood warning")
	// An empty tag list is an empty list, and it is not the same as no list.
	if alert.Tags == nil {
		t.Fatal("expected an empty tag array, got null")
	}
	if len(alert.Tags) != 0 {
		t.Fatalf("expected no tags, got %v", alert.Tags)
	}
}

func TestSevenDayMapperKeepsEveryBlock(t *testing.T) {
	data := mapOneCallBody(t, oneCallBodyWithBlocks)

	if data.OneCall == nil {
		t.Fatal("expected the onecall envelope to be present, got none")
	}
	// The same values the fixture sent, reached through the pointers the opt-in
	// blocks are now built from.
	if len(data.OneCall.Minutely) != 1 {
		t.Fatalf("expected the upstream minutely entry, got %#v", data.OneCall.Minutely)
	}
	minutely := data.OneCall.Minutely[0]
	requireFloat(t, "the minutely precipitation", minutely.Precipitation, 0.12)
	requireInt64(t, "the minutely dt", minutely.Dt, 1772000060)
	if len(data.OneCall.Hourly) != 1 {
		t.Fatalf("expected the upstream hourly entry, got %#v", data.OneCall.Hourly)
	}
	hourly := data.OneCall.Hourly[0]
	requireFloat(t, "the hourly temp", hourly.Temp, 11.5)
	requireFloat(t, "the hourly pop", hourly.Pop, 0.2)
	// A precipitation volume the upstream measured as 0 has to survive as 0 rather
	// than being dropped by an omitempty anywhere along the way.
	requireFloat(t, "the measured zero hourly rain", hourly.Rain, 0)
	requireFloat(t, "the measured zero hourly snow", hourly.Snow, 0)
	if len(data.OneCall.Alerts) != 1 {
		t.Fatalf("expected the upstream alert, got %#v", data.OneCall.Alerts)
	}
	requireString(t, "the alert event", data.OneCall.Alerts[0].Event, "Flood warning")
	requireString(t, "the alert sender", data.OneCall.Alerts[0].SenderName, "Met Office")
	if len(data.OneCall.Daily) != 1 {
		t.Fatalf("expected the one upstream day, got %d", len(data.OneCall.Daily))
	}
	if data.OneCall.Current == nil {
		t.Fatal("expected the faithful current block, got none")
	}
	if data.OneCall.Lat != 51.5074 || data.OneCall.Lon != -0.1278 || data.OneCall.Timezone != "Europe/London" {
		t.Fatalf("expected the envelope scalars to be mirrored, got %#v", data.OneCall)
	}
}

// The whole response is built from two decodes of one body, so a body that has
// members the mapper cannot see any more still answers. Nothing here is a crash
// path: the route has to return something, and what it returns has to say which
// readings it does not have.
func TestMapOneCallWithoutAPayloadStillReportsEveryDay(t *testing.T) {
	owm, payload := decodeOneCallBody(t, oneCallBodyWithBlocks)
	payload = models.SevenDayPayload{}

	data := NewWeatherService("dummy").mapOneCall(owm, payload, &models.Location{Name: "London"})

	if data.OneCall == nil {
		t.Fatal("expected the onecall envelope to be present, got none")
	}
	if len(data.OneCall.Daily) != 1 {
		t.Fatalf("expected the one upstream day, got %d", len(data.OneCall.Daily))
	}
	day := data.OneCall.Daily[0]
	if day.Dt == nil || *day.Dt != 1772000000 {
		t.Fatalf("expected the daily dt with no payload, got %#v", day.Dt)
	}
	// The timestamps are unconditionally sent, so they survive with no payload. Every
	// measurement reports null, and the day is still a day. The members are checked
	// through their own nil test rather than through an any, because a nil pointer
	// stored in an interface is not itself nil.
	for _, member := range []struct {
		name string
		gone bool
	}{
		{"temp", day.Temp == nil},
		{"feels_like", day.FeelsLike == nil},
		{"humidity", day.Humidity == nil},
		{"dew_point", day.DewPoint == nil},
		{"wind_speed", day.WindSpeed == nil},
		{"wind_deg", day.WindDeg == nil},
		{"wind_gust", day.WindGust == nil},
		{"pop", day.Pop == nil},
		{"rain", day.Rain == nil},
		{"snow", day.Snow == nil},
		{"uvi", day.Uvi == nil},
	} {
		if !member.gone {
			t.Fatalf("expected a null daily %s with no payload, got a value", member.name)
		}
	}
	// The members the allowlist covers are read from the decode struct rather than
	// from a presence view, so they keep their values with no payload at all. That is
	// the whole claim an allowlist entry makes, and the cascade is the honest answer:
	// the upstream documents them as sent, so there is a reading to report.
	if day.Dt == nil || day.Pressure == nil || day.Clouds == nil || day.MoonPhase == nil || day.Weather == nil {
		t.Fatalf("expected the always-sent daily members to survive a missing payload, got %#v", day)
	}
	if *day.Pressure != 1015 || *day.Clouds != 40 {
		t.Fatalf("expected the always-sent daily readings to come from the decode struct, got pressure %v clouds %v",
			*day.Pressure, *day.Clouds)
	}
	// The same thirteen readings on the current block, and the block itself is still
	// there: the upstream sent it, so the response says so and reports its
	// measurements as null.
	if data.OneCall.Current == nil {
		t.Fatal("expected the current block to survive a missing payload, got none")
	}
	for _, member := range []struct {
		name string
		gone bool
	}{
		{"temp", data.OneCall.Current.Temp == nil},
		{"feels_like", data.OneCall.Current.FeelsLike == nil},
		{"pressure", data.OneCall.Current.Pressure == nil},
		{"humidity", data.OneCall.Current.Humidity == nil},
		{"dew_point", data.OneCall.Current.DewPoint == nil},
		{"uvi", data.OneCall.Current.Uvi == nil},
		{"clouds", data.OneCall.Current.Clouds == nil},
		{"visibility", data.OneCall.Current.Visibility == nil},
		{"wind_speed", data.OneCall.Current.WindSpeed == nil},
		{"wind_deg", data.OneCall.Current.WindDeg == nil},
		{"wind_gust", data.OneCall.Current.WindGust == nil},
	} {
		if !member.gone {
			t.Fatalf("expected a null current %s with no payload, got a value", member.name)
		}
	}
	if data.OneCall.Current.Dt == nil || *data.OneCall.Current.Dt != 1772000000 {
		t.Fatalf("expected the current dt with no payload, got %#v", data.OneCall.Current.Dt)
	}

	// The legacy array is still capped and still present. With no payload the route
	// can measure nothing, so every legacy numeric reading is null rather than 0.
	if len(data.Forecast) != 1 {
		t.Fatalf("expected 1 legacy day, got %d", len(data.Forecast))
	}
	legacy := data.Forecast[0]
	if legacy.MaxTemp != nil || legacy.MinTemp != nil || legacy.AvgTemp != nil ||
		legacy.Humidity != nil || legacy.WindSpeed != nil ||
		legacy.ChanceOfRain != nil || legacy.UVIndex != nil {
		t.Fatalf("expected null legacy readings with no payload, got %#v", legacy)
	}
	// The legacy condition comes from the weather array, which needs no payload.
	if legacy.Condition != "Clouds" || legacy.Icon != "03d" {
		t.Fatalf("expected the legacy condition from the weather array, got %#v", legacy)
	}
	if legacy.Description != "Scattered Clouds" {
		t.Fatalf("expected a title cased legacy description, got %q", legacy.Description)
	}
	// The legacy current block is built from the same presence view as the faithful
	// one, so with no payload it is all nulls too rather than a row of readings read
	// out of the decode struct. The two vocabularies cannot disagree about whether a
	// reading exists, and sharing the view is what makes that true.
	if data.Current.Temperature != nil || data.Current.Humidity != nil || data.Current.LastUpdated != nil {
		t.Fatalf("expected a null legacy current with no payload, got %#v", data.Current)
	}
}
