package services

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"weathering-with-go/models"
)

func TestMapCurrentWeather(t *testing.T) {
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

	data := svc.mapCurrentWeather(owm, models.CurrentWeatherPayload{})
	if data.Location.Name != "Testville" {
		t.Fatalf("expected location name Testville got %s", data.Location.Name)
	}
	if data.Current.Condition != "Clear" {
		t.Fatalf("expected condition Clear got %s", data.Current.Condition)
	}
}

// Fixed slots for ordering tests, so results never depend on the wall clock.
var (
	firstFeb  = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC).Unix()
	secondFeb = time.Date(2026, 2, 2, 12, 0, 0, 0, time.UTC).Unix()
	thirdFeb  = time.Date(2026, 2, 3, 12, 0, 0, 0, time.UTC).Unix()
)

// mustSlotJSON renders one raw three hour slot. A fixture states only the members
// it cares about, so a slot that omits pop is a slot the upstream did not measure
// a probability for, which is not the same as a slot reporting 0.
func mustSlotJSON(t *testing.T, slot map[string]any) string {
	t.Helper()
	body, err := json.Marshal(slot)
	if err != nil {
		t.Fatalf("failed to render a slot fixture: %v", err)
	}
	return string(body)
}

// forecastBody wraps rendered slots in a /data/2.5/forecast envelope.
func forecastBody(t *testing.T, slots ...string) string {
	t.Helper()
	// message is the upstream's calculation time and arrives as a float, such as
	// 0.0117. Every fixture here carries that shape so a decode that reverted the
	// field to an int would fail this whole package rather than one test.
	return `{"cod":"200","message":0.0117,"cnt":` + strconv.Itoa(len(slots)) +
		`,"list":[` + strings.Join(slots, ",") +
		`],"city":{"id":2643743,"name":"London","coord":{"lon":-0.13,"lat":51.51},"country":"GB","population":7556900,"timezone":0,"sunrise":1771999200,"sunset":1772030400}}`
}

// decodeForecastBody decodes a body into both structs the mapper takes, exactly as
// fetchWeatherForecast does. Going through the raw bytes is what lets a fixture
// distinguish a member the upstream sent as 0 from one it never sent.
func decodeForecastBody(t *testing.T, body string) (models.OpenWeatherMapForecastResponse, models.ForecastPayload) {
	t.Helper()
	var owm models.OpenWeatherMapForecastResponse
	if err := json.Unmarshal([]byte(body), &owm); err != nil {
		t.Fatalf("failed to decode the fixture into the upstream struct: %v", err)
	}
	var payload models.ForecastPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("failed to decode the fixture into the payload: %v", err)
	}
	return owm, payload
}

func mapForecastBody(t *testing.T, body string, days int) *models.ForecastResponse {
	t.Helper()
	owm, payload := decodeForecastBody(t, body)
	return NewWeatherService("dummy").mapForecast(owm, payload, days)
}

// assertClose compares a derived float against the value a reader expects, with
// enough tolerance to absorb the representation error of an arithmetic mean.
func assertClose(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("expected %s to be %v, got %v", what, want, got)
	}
}

// forecastUTCMidnight anchors the forecast fixtures. The route groups slots by the
// UTC date of a timestamp, so a UTC anchor is what makes a fixture land on exactly
// one day on every host, whatever the test process's own zone happens to be.
var forecastUTCMidnight = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

// popSlots renders a day of count three hour slots, carrying only dt and the members
// a pop test names. The slot count and the pop map are separate arguments on purpose:
// index i of pops is the slot that carries pop, so omitting an index leaves a slot
// the upstream measured no probability for instead of removing the slot. A helper
// that emitted one slot per pop could not express that day at all, which is how the
// rule this task exists to implement went untested.
func popSlots(t *testing.T, at time.Time, count int, pops map[int]float64) string {
	t.Helper()
	slots := make([]string, 0, count)
	for i := range count {
		slot := map[string]any{"dt": at.Add(time.Duration(3*i) * time.Hour).Unix()}
		if pop, ok := pops[i]; ok {
			slot["pop"] = pop
		}
		slots = append(slots, mustSlotJSON(t, slot))
	}
	return forecastBody(t, slots...)
}

func TestDailyPopIsMaxAcrossSlots(t *testing.T) {
	body := popSlots(t, forecastUTCMidnight, 4, map[int]float64{0: 0.1, 1: 0.8, 2: 0.3, 3: 0.2})

	day := mapForecastBody(t, body, 5).Forecast[0]

	if day.Pop == nil {
		t.Fatal("expected a day pop, got none")
	}
	assertClose(t, "the day pop", *day.Pop, 0.8)
	if day.PopMin == nil {
		t.Fatal("expected a day pop_min, got none")
	}
	assertClose(t, "the day pop_min", *day.PopMin, 0.1)
	if day.PopMean == nil {
		t.Fatal("expected a day pop_mean, got none")
	}
	assertClose(t, "the day pop_mean", *day.PopMean, 0.35)
}

// The day genuinely has four slots and only two of them carry pop, so a mean
// divided by every slot would answer 0.35 rather than 0.7 and the exclusion rule
// would be caught. The fixture asserts the day really did carry all four, so this
// cannot quietly go back to a two slot day.
func TestDailyPopIgnoresSlotsWithoutPop(t *testing.T) {
	body := popSlots(t, forecastUTCMidnight, 4, map[int]float64{0: 0.5, 2: 0.9})

	day := mapForecastBody(t, body, 5).Forecast[0]

	if len(day.Hourly) != 4 {
		t.Fatalf("expected the day to carry all 4 slots, got %d", len(day.Hourly))
	}
	if day.Hourly[1].Pop != nil || day.Hourly[3].Pop != nil {
		t.Fatalf("expected slots 1 and 3 to report no pop, got %v and %v", day.Hourly[1].Pop, day.Hourly[3].Pop)
	}
	if day.Pop == nil {
		t.Fatal("expected a day pop, got none")
	}
	assertClose(t, "the day pop", *day.Pop, 0.9)
	if day.PopMin == nil {
		t.Fatal("expected a day pop_min, got none")
	}
	assertClose(t, "the day pop_min", *day.PopMin, 0.5)
	if day.PopMean == nil {
		t.Fatal("expected a day pop_mean, got none")
	}
	// The two pop-less slots are excluded from the mean rather than counted as zero,
	// which would have diluted it to 0.35.
	assertClose(t, "the day pop_mean", *day.PopMean, 0.7)
}

// chance_of_rain is the day's pop as a whole percentage, and it has to be rounded.
// 0.29 is 28.999999999999996 in binary floating point and 0.425 is exactly 42.5, so
// a truncating conversion answers 28 and 42 for days the upstream called 29 and 43
// percent. The number this key reports is the one the reported bug was about, so a
// truncation bug in it is not a rounding detail.
func TestLegacyChanceOfRainMatchesDailyPop(t *testing.T) {
	for _, tc := range []struct {
		pop  float64
		want int
	}{
		{pop: 0.42, want: 42},
		{pop: 0.29, want: 29},
		{pop: 0.425, want: 43},
		{pop: 0.0, want: 0},
	} {
		body := popSlots(t, forecastUTCMidnight, 1, map[int]float64{0: tc.pop})

		day := mapForecastBody(t, body, 5).Forecast[0]

		if day.Pop == nil {
			t.Fatalf("pop %v: expected a day pop, got none", tc.pop)
		}
		assertClose(t, "the day pop", *day.Pop, tc.pop)
		if day.ChanceOfRain == nil {
			t.Fatalf("pop %v: expected a legacy chance_of_rain, got none", tc.pop)
		}
		if *day.ChanceOfRain != tc.want {
			t.Fatalf("pop %v: expected chance_of_rain %d, got %d", tc.pop, tc.want, *day.ChanceOfRain)
		}
	}
}

// The three hour endpoint has no time of day breakdown, so the faithful half of
// the day reports null for every member it has no source for. temp.min and
// temp.max are the exception and they are derived, not measured.
func TestForecastDayHasNoTimeOfDayBreakdown(t *testing.T) {
	at := forecastUTCMidnight
	slots := []string{
		mustSlotJSON(t, map[string]any{
			"dt":   at.Unix(),
			"main": map[string]any{"temp": 18.0, "feels_like": 17.2, "temp_min": 15.0, "temp_max": 21.0, "pressure": 1012, "humidity": 70},
		}),
		mustSlotJSON(t, map[string]any{
			"dt":   at.Add(3 * time.Hour).Unix(),
			"main": map[string]any{"temp": 19.0, "feels_like": 18.0, "temp_min": 16.0, "temp_max": 22.0, "pressure": 1011, "humidity": 65},
		}),
	}

	day := mapForecastBody(t, forecastBody(t, slots...), 5).Forecast[0]

	if day.Temp == nil {
		t.Fatal("expected a faithful temp block, got none")
	}
	for _, absent := range []struct {
		name  string
		value *float64
	}{
		{"temp.day", day.Temp.Day},
		{"temp.night", day.Temp.Night},
		{"temp.morn", day.Temp.Morn},
		{"temp.eve", day.Temp.Eve},
	} {
		if absent.value != nil {
			t.Fatalf("expected %s to be nil, the three hour endpoint has no breakdown, got %v", absent.name, *absent.value)
		}
	}
	if day.Temp.Min == nil || day.Temp.Max == nil {
		t.Fatalf("expected derived temp.min and temp.max, got %#v", day.Temp)
	}
	assertClose(t, "temp.min", *day.Temp.Min, 18.0)
	assertClose(t, "temp.max", *day.Temp.Max, 19.0)

	if day.FeelsLike == nil {
		t.Fatal("expected a faithful feels_like block, got none")
	}
	for _, absent := range []struct {
		name  string
		value *float64
	}{
		{"feels_like.day", day.FeelsLike.Day},
		{"feels_like.night", day.FeelsLike.Night},
		{"feels_like.morn", day.FeelsLike.Morn},
		{"feels_like.eve", day.FeelsLike.Eve},
	} {
		if absent.value != nil {
			t.Fatalf("expected %s to be nil, the three hour endpoint has no breakdown, got %v", absent.name, *absent.value)
		}
	}
	if day.Uvi != nil {
		t.Fatalf("expected uvi to be nil, the three hour endpoint reports none, got %v", *day.Uvi)
	}
}

// Slots are grouped by the UTC date of their timestamp, so a slot at 23:00 UTC and
// one at 01:00 UTC the next morning are two days. The two instants here are fixed,
// and the expected day keys are the literal UTC dates they fall on rather than
// anything this process's own zone would format them as, so the test fails if the
// .UTC() in the grouping is removed and the suite is run anywhere but UTC.
func TestForecastDayGroupsSlotsAcrossUtcMidnight(t *testing.T) {
	// Pin the process zone to a non-zero offset so this test bites on every host,
	// including the UTC hosts CI runs, where removing the .UTC() from the grouping is
	// otherwise a semantic no-op and nothing can detect it. Restored on the way out,
	// and safe because no test in this package runs in parallel.
	original := time.Local
	time.Local = time.FixedZone("forecast-test", 3600)
	t.Cleanup(func() { time.Local = original })

	late := time.Date(2026, 2, 1, 23, 0, 0, 0, time.UTC)
	slots := []string{
		mustSlotJSON(t, map[string]any{"dt": late.Unix(), "main": map[string]any{"temp": 4.0}}),
		mustSlotJSON(t, map[string]any{"dt": late.Add(2 * time.Hour).Unix(), "main": map[string]any{"temp": 2.0}}),
	}

	days := mapForecastBody(t, forecastBody(t, slots...), 5).Forecast

	if len(days) != 2 {
		t.Fatalf("expected the UTC midnight to split the slots into 2 days, got %d: %+v", len(days), days)
	}
	want := []string{"2026-02-01", "2026-02-02"}
	for i, day := range days {
		if got := day.Date.Format("2006-01-02"); got != want[i] {
			t.Fatalf("expected day %d to be %s got %s", i, want[i], got)
		}
		if len(day.Hourly) != 1 {
			t.Fatalf("expected day %d to carry its own 1 slot, got %d", i, len(day.Hourly))
		}
	}
	// The slots stayed with their own day rather than being merged, so each day's
	// derived extremes come from one slot.
	assertClose(t, "the first day's derived min", *days[0].Temp.Min, 4.0)
	assertClose(t, "the second day's derived min", *days[1].Temp.Min, 2.0)
}

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

// Every day value that is not a date is a rollup over the day's slots, and a mean
// is taken over the slots that actually carry the member. Two cases: a full day, and
// a day whose second slot reported no main and no clouds block, so the means are
// taken over the one slot that has them rather than being diluted by a zero.
func TestForecastDayRollsUpSlotMeans(t *testing.T) {
	full := mapForecastBody(t, forecastBody(t,
		mustSlotJSON(t, map[string]any{
			"dt":         forecastUTCMidnight.Unix(),
			"main":       map[string]any{"temp": 18.0, "pressure": 1012, "humidity": 70},
			"clouds":     map[string]any{"all": 80},
			"wind":       map[string]any{"speed": 5.0, "deg": 90, "gust": 7.5},
			"visibility": 10000,
		}),
		mustSlotJSON(t, map[string]any{
			"dt":         forecastUTCMidnight.Add(3 * time.Hour).Unix(),
			"main":       map[string]any{"temp": 19.0, "pressure": 1011, "humidity": 65},
			"clouds":     map[string]any{"all": 0},
			"wind":       map[string]any{"speed": 3.0, "deg": 100},
			"visibility": 9000,
		}),
	), 5).Forecast[0]

	if full.Humidity == nil || full.Pressure == nil || full.Clouds == nil || full.Visibility == nil || full.WindSpeed == nil {
		t.Fatalf("expected every mean to be reported when both slots carry it, got %+v", full)
	}
	// Both means land on a half here, so this fixture is where the rounding policy
	// shows: pressure rounds to 1012 and clouds and visibility have no legacy
	// counterpart to constrain them, while humidity truncates to 67 because its legacy
	// field is an int that has always truncated.
	if *full.Pressure != 1012 {
		t.Fatalf("expected the day pressure mean rounded to 1012, got %d", *full.Pressure)
	}
	if *full.Humidity != 67 {
		t.Fatalf("expected the day humidity mean truncated to 67, got %d", *full.Humidity)
	}
	if *full.Clouds != 40 || *full.Visibility != 9500 {
		t.Fatalf("expected the day cloud and visibility means 40 and 9500, got %d and %d", *full.Clouds, *full.Visibility)
	}
	assertClose(t, "the day wind speed mean", *full.WindSpeed, 4.0)
	// wind_gust is the max, and wind_deg is the direction of the day's strongest
	// wind, not a mean of headings: the fastest slot here is the first, at deg 90.
	if full.WindGust == nil || *full.WindGust != 7.5 {
		t.Fatalf("expected the day wind_gust max 7.5, got %v", full.WindGust)
	}
	if full.WindDeg == nil || *full.WindDeg != 90 {
		t.Fatalf("expected the day wind_deg 90 from the fastest slot, got %v", full.WindDeg)
	}

	partial := mapForecastBody(t, forecastBody(t,
		mustSlotJSON(t, map[string]any{
			"dt":     forecastUTCMidnight.Unix(),
			"main":   map[string]any{"temp": 18.0, "pressure": 1012, "humidity": 70},
			"clouds": map[string]any{"all": 80},
		}),
		mustSlotJSON(t, map[string]any{"dt": forecastUTCMidnight.Add(3 * time.Hour).Unix()}),
	), 5).Forecast[0]

	if partial.Pressure == nil || *partial.Pressure != 1012 {
		t.Fatalf("expected the mean over the one slot that reported pressure, got %v", partial.Pressure)
	}
	if partial.Humidity == nil || *partial.Humidity != 70 {
		t.Fatalf("expected the mean over the one slot that reported humidity, got %v", partial.Humidity)
	}
	// Nothing reported a wind, so there is no wind to average and none is invented.
	if partial.WindSpeed != nil || partial.WindDeg != nil || partial.WindGust != nil {
		t.Fatalf("expected no wind rollup when no slot reported wind, got %+v", partial)
	}
}

// The day's rain and snow are sums of the slots' 3h windows, and the four states
// have to stay apart: a sum, a measured zero, a block the upstream sent with no
// window in it, and no block at all.
func TestForecastDaySumsSlotPrecipitation(t *testing.T) {
	slotAt := func(i int, members map[string]any) string {
		members["dt"] = forecastUTCMidnight.Add(time.Duration(3*i) * time.Hour).Unix()
		return mustSlotJSON(t, members)
	}

	summed := mapForecastBody(t, forecastBody(t,
		slotAt(0, map[string]any{"rain": map[string]any{"3h": 1.2}}),
		slotAt(1, map[string]any{"rain": map[string]any{"3h": 0.4}, "snow": map[string]any{"3h": 0.2}}),
		slotAt(2, map[string]any{}),
	), 5).Forecast[0]

	if summed.Rain == nil || summed.Rain.ThreeHour == nil {
		t.Fatalf("expected a day rain sum, got %#v", summed.Rain)
	}
	assertClose(t, "the day rain sum", *summed.Rain.ThreeHour, 1.6)
	if summed.Snow == nil || summed.Snow.ThreeHour == nil {
		t.Fatalf("expected a day snow sum, got %#v", summed.Snow)
	}
	assertClose(t, "the day snow sum", *summed.Snow.ThreeHour, 0.2)
	// The legacy total is the same two sums as one number, so it can never disagree
	// with the faithful pair.
	assertClose(t, "the legacy precipitation", summed.Precipitation, 1.8)

	// A window the upstream measured as 0 is a reading: the block is present and the
	// day is 0, not null.
	measuredZero := mapForecastBody(t, forecastBody(t,
		slotAt(0, map[string]any{"rain": map[string]any{"3h": 0}}),
	), 5).Forecast[0]
	if measuredZero.Rain == nil || measuredZero.Rain.ThreeHour == nil {
		t.Fatalf("expected a rain block for a measured zero, got %#v", measuredZero.Rain)
	}
	assertClose(t, "a measured zero day rain", *measuredZero.Rain.ThreeHour, 0.0)
	if measuredZero.Snow != nil {
		t.Fatalf("expected no snow block when no slot reported snow, got %#v", measuredZero.Snow)
	}
	assertClose(t, "the legacy precipitation for a measured zero", measuredZero.Precipitation, 0.0)

	// A block the upstream sent with no window in it stays three states apart on the
	// slot: the block is present and its window is null. The day is null, because a
	// slot that reported no window contributes nothing to a sum of windows and there
	// is no total to report.
	emptyWindow := mapForecastBody(t, forecastBody(t,
		slotAt(0, map[string]any{"rain": map[string]any{}}),
	), 5).Forecast[0]
	if len(emptyWindow.Hourly) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(emptyWindow.Hourly))
	}
	if slot := emptyWindow.Hourly[0]; slot.Rain == nil {
		t.Fatal("expected a rain block on the slot for an upstream sent empty block, got none")
	} else if slot.Rain.ThreeHour != nil {
		t.Fatalf("expected a null window on the slot, got %v", *slot.Rain.ThreeHour)
	}
	if emptyWindow.Rain != nil {
		t.Fatalf("expected no day rain sum when no slot reported a window, got %#v", emptyWindow.Rain)
	}

	// No slot reported precipitation at all: both faithful blocks are null, and the
	// legacy total is a real zero rather than a null.
	unreported := mapForecastBody(t, forecastBody(t,
		slotAt(0, map[string]any{}),
		slotAt(1, map[string]any{}),
	), 5).Forecast[0]
	if unreported.Rain != nil || unreported.Snow != nil {
		t.Fatalf("expected no day precipitation blocks, got %#v and %#v", unreported.Rain, unreported.Snow)
	}
	assertClose(t, "the legacy precipitation for an unreported day", unreported.Precipitation, 0.0)
}

// pop_mean is the raw arithmetic mean, not a value quantised to a fixed number of
// decimal places. The three probabilities here sum to 0.4 over three slots, so the
// mean is a repeating decimal; reporting 0.1333 for it would be a small lie about
// precision in the one number this route exists to get right, and a tolerance based
// assertion is what catches that rather than hiding it.
func TestDailyPopMeanIsNotQuantised(t *testing.T) {
	body := popSlots(t, forecastUTCMidnight, 3, map[int]float64{0: 0.1, 1: 0.1, 2: 0.2})

	day := mapForecastBody(t, body, 5).Forecast[0]

	if day.PopMean == nil {
		t.Fatal("expected a day pop_mean, got none")
	}
	assertClose(t, "the day pop_mean", *day.PopMean, 0.4/3.0)
}

func TestMapForecastPopulatesCurrent(t *testing.T) {
	owm, payload := decodeForecastBody(t, forecastBody(t,
		mustSlotJSON(t, map[string]any{
			"dt":      1772000000,
			"main":    map[string]any{"temp": 18.0, "feels_like": 17.2, "temp_min": 15.0, "temp_max": 21.0, "pressure": 1012, "humidity": 70},
			"weather": []any{map[string]any{"main": "Rain", "description": "light rain", "icon": "10d"}},
			"clouds":  map[string]any{"all": 80},
			"wind":    map[string]any{"speed": 5.0, "deg": 90, "gust": 7.5},
		}),
		mustSlotJSON(t, map[string]any{
			"dt":      1772010800,
			"main":    map[string]any{"temp": 19.0, "feels_like": 18.0, "temp_min": 16.0, "temp_max": 22.0, "pressure": 1011, "humidity": 65},
			"weather": []any{map[string]any{"main": "Clear", "description": "clear sky", "icon": "01d"}},
			"wind":    map[string]any{"speed": 3.0, "deg": 100},
		}),
	))

	data := NewWeatherService("dummy").mapForecast(owm, payload, 5)

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

// The mapper is handed a payload shorter than the upstream list, which cannot
// happen from a real body but must not panic or index out of range if it ever does.
// An empty payload leaves every slot reporting no presence, and the day then says
// so rather than rolling up zeroes: no slot reported a main block, so there is no
// temperature to derive and no humidity to average.
func TestMapForecastWithoutAPayloadStillRollsUpDays(t *testing.T) {
	owm := models.OpenWeatherMapForecastResponse{
		List: []models.ForecastItem{
			{Dt: forecastUTCMidnight.Unix(), Main: models.Main{Temp: 11}, Clouds: models.Clouds{All: 20}},
			{Dt: forecastUTCMidnight.Add(12 * time.Hour).Unix(), Main: models.Main{Temp: 12}, Clouds: models.Clouds{All: 40}},
		},
		City: models.City{Name: "London", Country: "GB", Coord: models.Coordinates{Lat: 51.51, Lon: -0.13}},
	}

	data := NewWeatherService("dummy").mapForecast(owm, models.ForecastPayload{}, 5)

	if len(data.Forecast) != 1 {
		t.Fatalf("expected the two slots to make 1 day, got %d: %+v", len(data.Forecast), data.Forecast)
	}
	day := data.Forecast[0]
	if day.Temp != nil {
		t.Fatalf("expected no temp block when no slot reported a main block, got %#v", day.Temp)
	}
	// The legacy aliases are null in the same case. As value fields they reported
	// three fabricated zeroes beside a null temp block, which is the one combination
	// of an absent reading and an invented number this project exists to remove.
	if day.MaxTemp != nil || day.MinTemp != nil || day.AvgTemp != nil {
		t.Fatalf("expected null legacy temperatures when no slot reported a main block, got max %v, min %v, avg %v",
			day.MaxTemp, day.MinTemp, day.AvgTemp)
	}
	if day.Humidity != nil || day.WindSpeed != nil || day.Clouds != nil || day.Visibility != nil {
		t.Fatalf("expected a mean over no members to be null, got %+v", day)
	}
	// The feels_like block is documented on the daily schema, so its presence does
	// not depend on what the slots carried.
	if day.FeelsLike == nil {
		t.Fatal("expected the documented feels_like block to be present, got none")
	}
	// The slots themselves are still reported, with every block the upstream did not
	// send reported as null.
	if len(day.Hourly) != 2 {
		t.Fatalf("expected 2 slots on the day, got %d", len(day.Hourly))
	}
	if day.Hourly[0].Main != nil || day.Hourly[0].Clouds != nil || day.Hourly[0].Pop != nil {
		t.Fatalf("expected no blocks on a slot with no presence, got %#v", day.Hourly[0])
	}
	if data.Location.Name != "London" || data.Location.Country != "GB" {
		t.Fatalf("unexpected legacy location %+v", data.Location)
	}
	if data.Location.Latitude != 51.51 || data.Location.Longitude != -0.13 {
		t.Fatalf("expected the city coordinates, got %+v", data.Location)
	}
}

func TestMapForecastWithNoItemsLeavesCurrentZero(t *testing.T) {
	svc := NewWeatherService("dummy")

	data := svc.mapForecast(models.OpenWeatherMapForecastResponse{}, models.ForecastPayload{}, 5)

	if data.Current.Temperature != 0 || data.Current.Condition != "" {
		t.Fatalf("expected zero current for an empty response, got %+v", data.Current)
	}
	if !data.Current.LastUpdated.IsZero() {
		t.Fatalf("expected zero last_updated, got %s", data.Current.LastUpdated)
	}
	if len(data.Forecast) != 0 {
		t.Fatalf("expected no days for an empty response, got %d", len(data.Forecast))
	}
}

func TestMapForecastReturnsEarliestDaysInOrder(t *testing.T) {
	owm := models.OpenWeatherMapForecastResponse{
		List: []models.ForecastItem{
			{Dt: thirdFeb, Main: models.Main{Temp: 13}},
			{Dt: firstFeb, Main: models.Main{Temp: 11}},
			{Dt: secondFeb, Main: models.Main{Temp: 12}},
		},
	}
	payload := models.ForecastPayload{List: []models.ForecastPayloadItem{{}, {}, {}}}

	data := NewWeatherService("dummy").mapForecast(owm, payload, 2)

	if len(data.Forecast) != 2 {
		t.Fatalf("expected 2 forecast days, got %d", len(data.Forecast))
	}
	// The three slots are noon UTC on three consecutive UTC dates, and the route
	// groups by UTC date, so these are exact on every host.
	want := []string{"2026-02-01", "2026-02-02"}
	for i, day := range data.Forecast {
		if got := day.Date.Format("2006-01-02"); got != want[i] {
			t.Fatalf("expected day %d to be %s got %s", i, want[i], got)
		}
	}
}

func TestMapOneCallPopulatesCurrent(t *testing.T) {
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

	// The legacy block is derived from the decode struct alone. The faithful twin is
	// built from the same upstream object and needs a presence view to read a
	// measurement, which is why the payload is passed here even though the legacy
	// assertions above would pass without one.
	temp, humidity := 12.4, 64
	payload := models.SevenDayPayload{Current: &models.SevenDayPayloadCurrent{Temp: &temp, Humidity: &humidity}}

	data := svc.mapOneCall(owm, payload, &models.Location{Name: "London"})

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
	// The legacy block is derived from the decode struct alone, so the faithful twin
	// is built from the same value and reports the same readings beside it.
	if data.OneCall == nil || data.OneCall.Current == nil {
		t.Fatal("expected the faithful current block to be present, got none")
	}
	faithful := data.OneCall.Current
	if faithful.Temp == nil || *faithful.Temp != 12.4 {
		t.Fatalf("expected the faithful current temp 12.4, got %#v", faithful.Temp)
	}
	if faithful.Humidity == nil || *faithful.Humidity != 64 {
		t.Fatalf("expected the faithful current humidity 64, got %#v", faithful.Humidity)
	}
}

func TestMapOneCallWithoutCurrentBlock(t *testing.T) {
	svc := NewWeatherService("dummy")

	data := svc.mapOneCall(models.OneCallResponse{}, models.SevenDayPayload{}, &models.Location{Name: "London"})

	if data.Current.Temperature != 0 || data.Current.Condition != "" {
		t.Fatalf("expected zero current when the block is absent, got %+v", data.Current)
	}
	// The faithful block is the one place the route can say outright that the
	// upstream reported no current conditions, which the legacy vocabulary has no
	// way to express.
	if data.OneCall == nil {
		t.Fatal("expected the onecall envelope to be present, got none")
	}
	if data.OneCall.Current != nil {
		t.Fatalf("expected no faithful current block, got %#v", data.OneCall.Current)
	}
}
