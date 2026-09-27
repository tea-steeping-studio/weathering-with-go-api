package services

import (
	"testing"
	"time"

	"weathering-with-go/models"
)

func TestConvertCurrentWeatherResponse(t *testing.T) {
	svc := NewWeatherService("dummy")
	owm := models.OpenWeatherMapResponse{
		Coord:   models.Coordinates{Lat: 1.23, Lon: 4.56},
		Weather: []models.Weather{{Main: "Clear", Description: "clear sky", Icon: "01d"}},
		Main:    models.Main{Temp: 10.5, FeelsLike: 9.0, Pressure: 1012, Humidity: 80},
		Wind:    models.Wind{Speed: 3.4, Deg: 180},
		Clouds:  models.Clouds{All: 0},
		Dt:      time.Now().Unix(),
		Sys:     models.Sys{Country: "GB"},
		Name:    "Testville",
	}

	data := svc.mapCurrentWeather(owm, currentOptional{})
	if data.Location.Name != "Testville" {
		t.Fatalf("expected location name Testville got %s", data.Location.Name)
	}
	if data.Current.Condition != "Clear" {
		t.Fatalf("expected condition Clear got %s", data.Current.Condition)
	}
}

func TestCalculateDailyForecastEmpty(t *testing.T) {
	svc := NewWeatherService("dummy")
	f := svc.calculateDailyForecast("2025-01-02", nil)
	if f.Date.IsZero() {
		t.Fatalf("expected non-zero date")
	}
}

// Fixed slots for ordering tests, so results never depend on the wall clock.
var (
	firstFeb  = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC).Unix()
	secondFeb = time.Date(2026, 2, 2, 12, 0, 0, 0, time.UTC).Unix()
	thirdFeb  = time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC).Unix()
)

func forecastResponseFixture() models.OpenWeatherMapForecastResponse {
	return models.OpenWeatherMapForecastResponse{
		List: []models.ForecastItem{
			{
				Dt:      1772000000,
				Main:    models.Main{Temp: 18.0, FeelsLike: 17.2, TempMin: 15.0, TempMax: 21.0, Pressure: 1012, Humidity: 70},
				Weather: []models.Weather{{Main: "Rain", Description: "light rain", Icon: "10d"}},
				Clouds:  models.Clouds{All: 80},
				Wind:    models.Wind{Speed: 5.0, Deg: 90, Gust: 7.5},
			},
			{
				Dt:      1772010800,
				Main:    models.Main{Temp: 19.0, FeelsLike: 18.0, TempMin: 16.0, TempMax: 22.0, Pressure: 1011, Humidity: 65},
				Weather: []models.Weather{{Main: "Clear", Description: "clear sky", Icon: "01d"}},
				Wind:    models.Wind{Speed: 3.0, Deg: 100},
			},
		},
		City: models.City{Name: "London", Country: "GB", Coord: models.Coordinates{Lat: 51.51, Lon: -0.13}},
	}
}

func TestConvertForecastResponsePopulatesCurrent(t *testing.T) {
	svc := NewWeatherService("dummy")

	data := svc.convertForecastResponse(forecastResponseFixture(), 5)

	wantUpdated := time.Unix(1772000000, 0)
	got := data.Current
	if got.Temperature != 18.0 || got.FeelsLike != 17.2 {
		t.Fatalf("unexpected current temperatures %+v", got)
	}
	if got.Humidity != 70 || got.Pressure != 1012 {
		t.Fatalf("unexpected current humidity/pressure %+v", got)
	}
	if got.WindSpeed != 5.0 || got.WindDirection != 90 || got.WindGust != 7.5 {
		t.Fatalf("unexpected current wind %+v", got)
	}
	if got.CloudCover != 80 {
		t.Fatalf("expected cloud_cover 80, got %d", got.CloudCover)
	}
	if got.Condition != "Rain" || got.Icon != "10d" || got.Description != "Light Rain" {
		t.Fatalf("unexpected current condition %+v", got)
	}
	if got.MinTemp != 15.0 || got.MaxTemp != 21.0 {
		t.Fatalf("unexpected current min/max %+v", got)
	}
	if !got.LastUpdated.Equal(wantUpdated) {
		t.Fatalf("expected last_updated %s got %s", wantUpdated, got.LastUpdated)
	}
}

func TestConvertForecastResponseWithNoItemsLeavesCurrentZero(t *testing.T) {
	svc := NewWeatherService("dummy")

	data := svc.convertForecastResponse(models.OpenWeatherMapForecastResponse{}, 5)

	if data.Current.Temperature != 0 || data.Current.Condition != "" {
		t.Fatalf("expected zero current for an empty response, got %+v", data.Current)
	}
	if !data.Current.LastUpdated.IsZero() {
		t.Fatalf("expected zero last_updated, got %s", data.Current.LastUpdated)
	}
}

func TestConvertForecastResponseReturnsEarliestDaysInOrder(t *testing.T) {
	svc := NewWeatherService("dummy")
	owm := models.OpenWeatherMapForecastResponse{
		List: []models.ForecastItem{
			{Dt: thirdFeb, Main: models.Main{Temp: 13}},
			{Dt: firstFeb, Main: models.Main{Temp: 11}},
			{Dt: secondFeb, Main: models.Main{Temp: 12}},
		},
	}

	data := svc.convertForecastResponse(owm, 2)

	if len(data.Forecast) != 2 {
		t.Fatalf("expected 2 forecast days, got %d", len(data.Forecast))
	}
	want := []string{"2026-02-01", "2026-02-02"}
	for i, day := range data.Forecast {
		if got := day.Date.Format("2006-01-02"); got != want[i] {
			t.Fatalf("expected day %d to be %s got %s", i, want[i], got)
		}
	}
}

func TestConvertOneCallResponsePopulatesCurrent(t *testing.T) {
	svc := NewWeatherService("dummy")
	owm := models.OneCallResponse{
		Current: &models.OneCallCurrent{
			Dt:         1772000000,
			Temp:       12.4,
			FeelsLike:  11.0,
			Pressure:   1008,
			Humidity:   64,
			Uvi:        2.1,
			Clouds:     30,
			Visibility: 9000,
			WindSpeed:  4.2,
			WindDeg:    210,
			WindGust:   6.0,
			Weather:    []models.Weather{{Main: "Clouds", Description: "broken clouds", Icon: "04d"}},
		},
	}

	data := svc.convertOneCallResponse(owm, &models.Location{Name: "London"})

	wantUpdated := time.Unix(1772000000, 0)
	got := data.Current
	if got.Temperature != 12.4 || got.FeelsLike != 11.0 {
		t.Fatalf("unexpected current temperatures %+v", got)
	}
	if got.Pressure != 1008 || got.Humidity != 64 {
		t.Fatalf("unexpected current pressure/humidity %+v", got)
	}
	if got.Visibility != 9000 || got.CloudCover != 30 {
		t.Fatalf("unexpected current visibility/clouds %+v", got)
	}
	if got.WindSpeed != 4.2 || got.WindDirection != 210 || got.WindGust != 6.0 {
		t.Fatalf("unexpected current wind %+v", got)
	}
	if got.Condition != "Clouds" || got.Icon != "04d" || got.Description != "Broken Clouds" {
		t.Fatalf("unexpected current condition %+v", got)
	}
	if !got.LastUpdated.Equal(wantUpdated) {
		t.Fatalf("expected last_updated %s got %s", wantUpdated, got.LastUpdated)
	}
}

func TestConvertOneCallResponseWithoutCurrentBlock(t *testing.T) {
	svc := NewWeatherService("dummy")

	data := svc.convertOneCallResponse(models.OneCallResponse{}, &models.Location{Name: "London"})

	if data.Current.Temperature != 0 || data.Current.Condition != "" {
		t.Fatalf("expected zero current when the block is absent, got %+v", data.Current)
	}
}
