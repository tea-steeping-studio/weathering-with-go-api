package models

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestAPIResponseMarshal(t *testing.T) {
	resp := APIResponse{
		Success: true,
		Data:    map[string]interface{}{"foo": "bar"},
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var out APIResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !out.Success {
		t.Fatalf("expected success true")
	}
}

func TestWeatherRequestTags(t *testing.T) {
	rt := reflect.TypeOf(WeatherRequest{})
	f, ok := rt.FieldByName("Location")
	if !ok {
		t.Fatalf("Location field missing")
	}
	jsonTag := f.Tag.Get("json")
	formTag := f.Tag.Get("form")
	if jsonTag != "location" || formTag != "location" {
		t.Fatalf("unexpected tags: json=%s form=%s", jsonTag, formTag)
	}
}

// assertJSONTags fails the test unless every named field carries exactly the
// expected json tag and is a plain value type. A decode-only assertion cannot
// catch a missing tag on a lower-case name, because encoding/json matches field
// names case-insensitively, and the exact tag string also rules out omitempty.
// The type guard covers slice element types too, so a pointer smuggled in as
// []*T is rejected just like a bare *T field.
func assertJSONTags(t *testing.T, v any, want map[string]string) {
	t.Helper()
	rt := reflect.TypeOf(v)
	for field, tag := range want {
		f, ok := rt.FieldByName(field)
		if !ok {
			t.Errorf("%s.%s: field missing", rt.Name(), field)
			continue
		}
		if got := f.Tag.Get("json"); got != tag {
			t.Errorf("%s.%s: json tag = %q, want %q", rt.Name(), field, got, tag)
		}
		if f.Type.Kind() == reflect.Pointer {
			t.Errorf("%s.%s: must be a plain value type, got %s", rt.Name(), field, f.Type)
		}
		if f.Type.Kind() == reflect.Slice && f.Type.Elem().Kind() == reflect.Pointer {
			t.Errorf("%s.%s: slice element must be a plain value type, got %s", rt.Name(), field, f.Type)
		}
	}
}

func TestUpstreamModelsBindAllDocumentedFields(t *testing.T) {
	assertJSONTags(t, Main{}, map[string]string{"TempKF": "temp_kf"})
	assertJSONTags(t, ForecastItem{}, map[string]string{"Pop": "pop", "Visibility": "visibility"})
	assertJSONTags(t, City{}, map[string]string{"Population": "population"})

	body := `{"coord":{"lon":-0.13,"lat":51.51},"weather":[{"id":500,"main":"Rain","description":"light rain","icon":"10d"}],
	"base":"stations","main":{"temp":280.32,"feels_like":278.1,"temp_min":279.15,"temp_max":281.15,"pressure":1012,
	"humidity":81,"sea_level":1010,"grnd_level":1005,"temp_kf":0.4},"visibility":10000,
	"wind":{"speed":4.1,"deg":80,"gust":6.1},"clouds":{"all":90},"rain":{"1h":0.4},"snow":{"1h":0},
	"dt":1485789600,"sys":{"type":1,"id":5091,"country":"GB","sunrise":1485762037,"sunset":1485794875},
	"id":2643743,"timezone":0,"name":"London","cod":200}`

	var resp OpenWeatherMapResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp.Main.TempKF != 0.4 {
		t.Fatalf("expected temp_kf 0.4, got %v", resp.Main.TempKF)
	}
	if resp.Rain.OneHour != 0.4 {
		t.Fatalf("expected rain 1h 0.4, got %v", resp.Rain.OneHour)
	}

	forecastBody := `{"cod":"200","message":0,"cnt":1,
	"list":[{"dt":1485789600,"main":{"temp":280.32,"temp_kf":0.3},"weather":[{"id":500,"main":"Rain","description":"light rain","icon":"10d"}],
	"clouds":{"all":90},"wind":{"speed":4.1,"deg":80,"gust":6.1},"visibility":10000,"pop":0.32,
	"rain":{"3h":0.5},"snow":{"3h":0.1},"sys":{"pod":"d"},"dt_txt":"2017-01-30 18:00:00"}],
	"city":{"id":2643743,"name":"London","coord":{"lon":-0.13,"lat":51.51},"country":"GB","population":1000000,
	"timezone":0,"sunrise":1485762037,"sunset":1485794875}}`

	var forecast OpenWeatherMapForecastResponse
	if err := json.Unmarshal([]byte(forecastBody), &forecast); err != nil {
		t.Fatalf("forecast decode failed: %v", err)
	}
	if len(forecast.List) != 1 {
		t.Fatalf("expected 1 list item, got %d", len(forecast.List))
	}
	if forecast.List[0].Pop != 0.32 {
		t.Fatalf("expected list pop 0.32, got %v", forecast.List[0].Pop)
	}
	if forecast.List[0].Visibility != 10000 {
		t.Fatalf("expected list visibility 10000, got %v", forecast.List[0].Visibility)
	}
	if forecast.List[0].Main.TempKF != 0.3 {
		t.Fatalf("expected list main temp_kf 0.3, got %v", forecast.List[0].Main.TempKF)
	}
	if forecast.City.Population != 1000000 {
		t.Fatalf("expected city population 1000000, got %v", forecast.City.Population)
	}
}

func TestOneCallModelsBindDocumentedFields(t *testing.T) {
	assertJSONTags(t, OneCallResponse{}, map[string]string{
		"TimezoneOffset": "timezone_offset",
		"Minutely":       "minutely",
		"Hourly":         "hourly",
		"Alerts":         "alerts",
	})
	assertJSONTags(t, Minutely{}, map[string]string{
		"Dt":            "dt",
		"Precipitation": "precipitation",
	})
	assertJSONTags(t, Hourly{}, map[string]string{
		"Dt": "dt", "Sunrise": "sunrise", "Sunset": "sunset", "Temp": "temp", "FeelsLike": "feels_like",
		"Pressure": "pressure", "Humidity": "humidity", "DewPoint": "dew_point", "Uvi": "uvi",
		"Clouds": "clouds", "Visibility": "visibility", "WindSpeed": "wind_speed", "WindDeg": "wind_deg",
		"WindGust": "wind_gust", "Pop": "pop", "Rain": "rain", "Snow": "snow", "Weather": "weather",
	})
	assertJSONTags(t, DailyForecast{}, map[string]string{
		"DewPoint": "dew_point", "WindGust": "wind_gust", "Sunrise": "sunrise", "Sunset": "sunset",
		"Moonrise": "moonrise", "Moonset": "moonset", "MoonPhase": "moon_phase",
	})
	assertJSONTags(t, Alert{}, map[string]string{
		"SenderName": "sender_name", "Event": "event", "Start": "start", "End": "end",
		"Description": "description", "Tags": "tags",
	})

	body := `{"lat":33.44,"lon":-94.04,"timezone":"America/Chicago","timezone_offset":-18000,
	"current":{"dt":1595243443,"sunrise":1595243663,"sunset":1595294958,"temp":299.41,"feels_like":300.32,
	"pressure":1014,"humidity":89,"dew_point":297.15,"uvi":8.53,"clouds":75,"visibility":10000,
	"wind_speed":4.12,"wind_deg":210,"wind_gust":8.2,
	"weather":[{"id":500,"main":"Rain","description":"light rain","icon":"10d"}]},
	"minutely":[{"dt":1595243460,"precipitation":0.02}],
	"hourly":[{"dt":1595242800,"sunrise":1595243663,"sunset":1595294958,"temp":299.41,"feels_like":300.32,
	"pressure":1014,"humidity":89,"dew_point":297.15,"uvi":5.53,"clouds":75,"visibility":10000,
	"wind_speed":3.12,"wind_deg":210,"wind_gust":6.2,"pop":0.32,"rain":0.12,"snow":0.2,
	"weather":[{"id":500,"main":"Rain","description":"light rain","icon":"10d"}]}],
	"daily":[{"dt":1595242800,"sunrise":1595243663,"sunset":1595294958,"moonrise":1595245402,"moonset":1595293300,
	"moon_phase":0.07,"temp":{"day":299.02,"min":288.79,"max":300.19,"night":289.58,"morn":292.15,"eve":296.5},
	"feels_like":{"day":298.77,"night":285.88,"eve":295.1,"morn":290.15},"pressure":1015,"humidity":65,
	"dew_point":292.35,"wind_speed":5.68,"wind_deg":225,"wind_gust":9.68,"clouds":40,"pop":0.62,
	"rain":1.25,"snow":0.35,"uvi":8.53,"weather":[{"id":500,"main":"Rain","description":"light rain","icon":"10d"}]}],
	"alerts":[{"sender_name":"NWS Tulsa","event":"Heat Advisory","start":1595246400,"end":1595293200,
	"description":"HEAT ADVISORY REMAINS IN EFFECT UNTIL 9 PM CDT.","tags":["Extreme temperature value","Heat"]}]}`

	var resp OneCallResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if resp.TimezoneOffset != -18000 {
		t.Fatalf("expected timezone_offset -18000, got %v", resp.TimezoneOffset)
	}

	if len(resp.Minutely) != 1 {
		t.Fatalf("expected 1 minutely entry, got %d", len(resp.Minutely))
	}
	if resp.Minutely[0].Dt != 1595243460 {
		t.Fatalf("expected minutely dt 1595243460, got %v", resp.Minutely[0].Dt)
	}
	if resp.Minutely[0].Precipitation != 0.02 {
		t.Fatalf("expected minutely precipitation 0.02, got %v", resp.Minutely[0].Precipitation)
	}

	if len(resp.Hourly) != 1 {
		t.Fatalf("expected 1 hourly entry, got %d", len(resp.Hourly))
	}
	h := resp.Hourly[0]
	if h.Dt != 1595242800 || h.Sunrise != 1595243663 || h.Sunset != 1595294958 {
		t.Fatalf("unexpected hourly timestamps: dt=%v sunrise=%v sunset=%v", h.Dt, h.Sunrise, h.Sunset)
	}
	if h.Temp != 299.41 || h.FeelsLike != 300.32 || h.DewPoint != 297.15 {
		t.Fatalf("unexpected hourly temps: temp=%v feels_like=%v dew_point=%v", h.Temp, h.FeelsLike, h.DewPoint)
	}
	if h.Uvi != 5.53 || h.Pop != 0.32 || h.Rain != 0.12 || h.Snow != 0.2 {
		t.Fatalf("unexpected hourly float fields: uvi=%v pop=%v rain=%v snow=%v", h.Uvi, h.Pop, h.Rain, h.Snow)
	}
	if h.WindSpeed != 3.12 || h.WindGust != 6.2 {
		t.Fatalf("unexpected hourly wind: speed=%v gust=%v", h.WindSpeed, h.WindGust)
	}
	if h.Pressure != 1014 || h.Humidity != 89 || h.Clouds != 75 || h.WindDeg != 210 || h.Visibility != 10000 {
		t.Fatalf("unexpected hourly int fields: pressure=%v humidity=%v clouds=%v wind_deg=%v visibility=%v",
			h.Pressure, h.Humidity, h.Clouds, h.WindDeg, h.Visibility)
	}
	if len(h.Weather) != 1 || h.Weather[0].ID != 500 {
		t.Fatalf("unexpected hourly weather: %+v", h.Weather)
	}

	if len(resp.Daily) != 1 {
		t.Fatalf("expected 1 daily entry, got %d", len(resp.Daily))
	}
	d := resp.Daily[0]
	if d.DewPoint != 292.35 {
		t.Fatalf("expected daily dew_point 292.35, got %v", d.DewPoint)
	}
	if d.WindGust != 9.68 {
		t.Fatalf("expected daily wind_gust 9.68, got %v", d.WindGust)
	}
	if d.Sunrise != 1595243663 || d.Sunset != 1595294958 {
		t.Fatalf("unexpected daily sun times: sunrise=%v sunset=%v", d.Sunrise, d.Sunset)
	}
	if d.Moonrise != 1595245402 || d.Moonset != 1595293300 {
		t.Fatalf("unexpected daily moon times: moonrise=%v moonset=%v", d.Moonrise, d.Moonset)
	}
	if d.MoonPhase != 0.07 {
		t.Fatalf("expected daily moon_phase 0.07, got %v", d.MoonPhase)
	}
	if d.Pop != 0.62 {
		t.Fatalf("expected daily pop 0.62, got %v", d.Pop)
	}
	if d.Snow != 0.35 {
		t.Fatalf("expected daily snow 0.35, got %v", d.Snow)
	}

	if len(resp.Alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(resp.Alerts))
	}
	a := resp.Alerts[0]
	if a.SenderName != "NWS Tulsa" {
		t.Fatalf("expected alert sender_name NWS Tulsa, got %v", a.SenderName)
	}
	if a.Event != "Heat Advisory" {
		t.Fatalf("expected alert event Heat Advisory, got %v", a.Event)
	}
	if a.Start != 1595246400 || a.End != 1595293200 {
		t.Fatalf("unexpected alert times: start=%v end=%v", a.Start, a.End)
	}
	if a.Description != "HEAT ADVISORY REMAINS IN EFFECT UNTIL 9 PM CDT." {
		t.Fatalf("unexpected alert description: %v", a.Description)
	}
	if len(a.Tags) != 2 || a.Tags[0] != "Extreme temperature value" || a.Tags[1] != "Heat" {
		t.Fatalf("unexpected alert tags: %+v", a.Tags)
	}
}
