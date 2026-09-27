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
	// The legacy block is pointer shaped, so a reading is reached through its pointer.
	// The value is the one this test asserted before the change.
	if data.Current.Condition == nil || *data.Current.Condition != "Clear" {
		t.Fatalf("expected condition Clear got %v", data.Current.Condition)
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
	// with the faithful pair. It is a pointer, so it is reached through one.
	if summed.Precipitation == nil {
		t.Fatal("expected a legacy precipitation total, got null")
	}
	assertClose(t, "the legacy precipitation", *summed.Precipitation, 1.8)

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
	// The measured zero is a real total, so the legacy key is 0 rather than null.
	if measuredZero.Precipitation == nil {
		t.Fatal("expected a legacy precipitation total for a measured zero, got null")
	}
	assertClose(t, "the legacy precipitation for a measured zero", *measuredZero.Precipitation, 0.0)

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

	// No slot reported precipitation at all: both faithful blocks are null, and so is
	// the legacy total, because a sum of nothing measured is not a measurement of
	// nothing. This is the change from a real 0, which claimed the day was dry.
	unreported := mapForecastBody(t, forecastBody(t,
		slotAt(0, map[string]any{}),
		slotAt(1, map[string]any{}),
	), 5).Forecast[0]
	if unreported.Rain != nil || unreported.Snow != nil {
		t.Fatalf("expected no day precipitation blocks, got %#v and %#v", unreported.Rain, unreported.Snow)
	}
	if unreported.Precipitation != nil {
		t.Fatalf("expected no legacy precipitation total for an unreported day, got %v", *unreported.Precipitation)
	}
}

// A summed precipitation is an answer only when there was something to sum, and this
// is the same rule the seven day route follows for its own day. Three days from one
// fixture each: a day whose slots report two volumes, a day whose single slot reports
// a volume the upstream measured as 0, and a day whose slots report no volume at all.
// The three are three readings and the middle one is the only zero among them.
func TestForecastDayPrecipitationSeparatesAMeasuredZeroFromNoReading(t *testing.T) {
	slotAt := func(i int, members map[string]any) string {
		members["dt"] = forecastUTCMidnight.Add(time.Duration(3*i) * time.Hour).Unix()
		return mustSlotJSON(t, members)
	}
	plainSlot := func(temp float64, humidity int) map[string]any {
		return map[string]any{
			"main":    map[string]any{"temp": temp, "pressure": 1012, "humidity": humidity},
			"weather": []any{map[string]any{"main": "Clear", "description": "clear sky", "icon": "01d"}},
			"clouds":  map[string]any{"all": 0},
		}
	}

	days := mapForecastBody(t, forecastBody(t,
		slotAt(0, map[string]any{"rain": map[string]any{"3h": 1.2}}),
		slotAt(1, map[string]any{"rain": map[string]any{"3h": 0.4}, "snow": map[string]any{"3h": 0.2}}),
		slotAt(2, map[string]any{}),
		slotAt(3, map[string]any{}),
		slotAt(4, map[string]any{}),
		// 24:00 falls on the next UTC date, so this is a second day: one slot, with a
		// volume the upstream measured as 0, and nothing else on it.
		slotAt(8, map[string]any{"rain": map[string]any{"3h": 0}}),
		slotAt(9, map[string]any{}),
		slotAt(10, map[string]any{}),
		slotAt(11, map[string]any{}),
		slotAt(12, map[string]any{}),
		slotAt(13, map[string]any{}),
		slotAt(14, map[string]any{}),
		slotAt(15, map[string]any{}),
		// 48:00 falls on the third UTC date. Its slots carry everything else a day
		// carries, so the day is a real day and the precipitation gap is one gap in it
		// rather than a day of nothing.
		slotAt(16, plainSlot(18.0, 70)),
		slotAt(17, plainSlot(19.0, 72)),
		slotAt(18, plainSlot(20.0, 74)),
		slotAt(19, plainSlot(21.0, 76)),
		slotAt(20, plainSlot(22.0, 78)),
		slotAt(21, plainSlot(23.0, 80)),
		slotAt(22, plainSlot(24.0, 82)),
		slotAt(23, plainSlot(25.0, 84)),
	), 5).Forecast

	// The fixture's own shape, so a test that cannot express the three states cannot
	// pass by rendering one day instead of three.
	if len(days) != 3 {
		t.Fatalf("expected 3 days from the fixture, got %d", len(days))
	}

	// Day 0: two volumes reported, so the total is their sum and it is a reading.
	if days[0].Precipitation == nil {
		t.Fatal("expected a legacy precipitation total for a day with volumes, got null")
	}
	assertClose(t, "the day total for reported volumes", *days[0].Precipitation, 1.8)
	if days[0].Rain == nil || days[0].Snow == nil {
		t.Fatalf("expected both faithful blocks beside the total, got rain %#v snow %#v", days[0].Rain, days[0].Snow)
	}

	// Day 1: one slot reported a volume the upstream measured as 0. That is a reading
	// of nothing falling, so the total is a real 0 rather than a null, and it is the
	// only 0 among the three days.
	if days[1].Precipitation == nil {
		t.Fatal("expected a legacy precipitation total for a measured zero, got null")
	}
	assertClose(t, "the day total for a measured zero", *days[1].Precipitation, 0)
	if days[1].Rain == nil || days[1].Rain.ThreeHour == nil {
		t.Fatalf("expected a faithful rain block for a measured zero, got %#v", days[1].Rain)
	}
	assertClose(t, "the faithful day rain for a measured zero", *days[1].Rain.ThreeHour, 0)
	// No slot reported snow on that day, so there is no snow sum beside the rain one.
	if days[1].Snow != nil {
		t.Fatalf("expected no snow block when no slot reported snow, got %#v", days[1].Snow)
	}

	// Day 2: no slot reported a volume, so the faithful blocks are null and the legacy
	// total is null too. As a value this was 0, which said the day was dry and
	// measured at the same time.
	if days[2].Rain != nil || days[2].Snow != nil {
		t.Fatalf("expected no faithful blocks on an unreported day, got rain %#v snow %#v", days[2].Rain, days[2].Snow)
	}
	if days[2].Precipitation != nil {
		t.Fatalf("expected no legacy precipitation total for an unreported day, got %v", *days[2].Precipitation)
	}
	// The rest of the day is unaffected, so this is a day with one gap rather than an
	// empty object: eight slots carried a main block and a weather array, and every
	// other rollup is a reading.
	if days[2].Humidity == nil {
		t.Fatal("expected a humidity on an unreported day, got null")
	}
	assertClose(t, "the humidity on an unreported day", float64(*days[2].Humidity), 77)
	// MaxTemp is the day's derived extreme of the slot temperatures, 18 to 25.
	requireFloat(t, "the max_temperature on an unreported day", days[2].MaxTemp, 25)
	requireFloat(t, "the min_temperature on an unreported day", days[2].MinTemp, 18)
	if days[2].Condition != "Clear" {
		t.Fatalf("expected the condition from the middle slot, got %q", days[2].Condition)
	}
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

	// The expected values are the ones this test asserted before models.Current became
	// pointer shaped. Only the comparison changed: a reading is now reached through
	// its pointer, so a value that disappears fails here as loudly as it ever did.
	got := data.Current
	requireFloat(t, "the current temperature", got.Temperature, 18.0)
	requireFloat(t, "the current feels_like", got.FeelsLike, 17.2)
	requireInt(t, "the current humidity", got.Humidity, 70)
	requireFloat(t, "the current pressure", got.Pressure, 1012)
	requireFloat(t, "the current wind_speed", got.WindSpeed, 5.0)
	requireInt(t, "the current wind_direction", got.WindDirection, 90)
	requireFloat(t, "the current wind_gust", got.WindGust, 7.5)
	requireInt(t, "the current cloud_cover", got.CloudCover, 80)
	requireString(t, "the current condition", got.Condition, "Rain")
	requireString(t, "the current icon", got.Icon, "10d")
	requireString(t, "the current description", got.Description, "Light Rain")
	requireFloat(t, "the current min_temperature", got.MinTemp, 15.0)
	requireFloat(t, "the current max_temperature", got.MaxTemp, 21.0)
	requireTime(t, "the current last_updated", got.LastUpdated, time.Unix(1772000000, 0))
}

// The legacy current block is slot zero's readings, and visibility is one of them:
// the endpoint documents list.visibility, so the faithful hourly slot and the legacy
// block were reporting the same datum two ways. A slot that carries one has to reach
// both, and a slot the upstream sent without one has to be null in both rather than a
// visibility of 0 metres, which is what reading the non-pointer field reports.
func TestCurrentFromForecastCarriesTheFirstSlotVisibility(t *testing.T) {
	slot := func(visibility any) string {
		members := map[string]any{
			"dt":      1772000000,
			"main":    map[string]any{"temp": 18.0, "feels_like": 17.2, "temp_min": 15.0, "temp_max": 21.0, "pressure": 1012, "humidity": 70},
			"weather": []any{map[string]any{"main": "Rain", "description": "light rain", "icon": "10d"}},
			"clouds":  map[string]any{"all": 80},
			"wind":    map[string]any{"speed": 5.0, "deg": 90},
		}
		if visibility != nil {
			members["visibility"] = visibility
		}
		return mustSlotJSON(t, members)
	}

	// The first slot reports a visibility. The legacy block and the faithful slot have
	// to agree, and the value is a reading rather than a derived one.
	reported := mapForecastBody(t, forecastBody(t, slot(9000), slot(8000)), 5)
	requireFloat(t, "the legacy current visibility", reported.Current.Visibility, 9000)
	if slotVisibility := reported.Forecast[0].Hourly[0].Visibility; slotVisibility == nil {
		t.Fatal("expected the faithful slot to report its visibility, got null")
	} else if *slotVisibility != 9000 {
		t.Fatalf("expected the faithful slot visibility 9000, got %d", *slotVisibility)
	}
	if legacy := reported.Current.Visibility; legacy == nil || *legacy != float64(*reported.Forecast[0].Hourly[0].Visibility) {
		t.Fatalf("expected the legacy block and the faithful slot to report one visibility, got %v and %d", legacy, *reported.Forecast[0].Hourly[0].Visibility)
	}

	// The first slot carries no visibility at all. Both go null: 0 metres of
	// visibility is a reading nobody made, and the legacy key has never had a way to
	// say null other than by being absent.
	absent := mapForecastBody(t, forecastBody(t, slot(nil), slot(8000)), 5)
	if absent.Current.Visibility != nil {
		t.Fatalf("expected a null legacy visibility for a slot that sent none, got %v", *absent.Current.Visibility)
	}
	if absent.Forecast[0].Hourly[0].Visibility != nil {
		t.Fatalf("expected a null faithful slot visibility for a slot that sent none, got %d",
			*absent.Forecast[0].Hourly[0].Visibility)
	}

	// A slot that reports a visibility the upstream measured as 0 is still a reading,
	// and a non-nil pointer is what says so. The legacy key carries no omitempty, so
	// 0 reaches the JSON rather than being dropped.
	measuredZero := mapForecastBody(t, forecastBody(t, slot(0)), 5)
	requireFloat(t, "a measured zero visibility", measuredZero.Current.Visibility, 0)
}

// wind.gust is not documented for a three hour slot, so its zero in the decode struct
// means "no gust" rather than "a measured 0 m/s". Taking its address fabricated the 0,
// and the legacy key has an omitempty that drops a nil, so the three states have to come
// out as three answers: the key absent, a 0, and the number.
func TestCurrentFromForecastReportsTheGustTheWayTheUpstreamMeant(t *testing.T) {
	slotWith := func(wind map[string]any) string {
		return mustSlotJSON(t, map[string]any{
			"dt":      1772000000,
			"main":    map[string]any{"temp": 18.0, "pressure": 1012, "humidity": 70},
			"weather": []any{map[string]any{"main": "Rain", "description": "light rain", "icon": "10d"}},
			"clouds":  map[string]any{"all": 80},
			"wind":    wind,
		})
	}

	// The upstream sent a wind block with no gust in it. The key is absent, which is the
	// only honest answer: 0 m/s would be a measurement nobody made, and the decode
	// field's zero is exactly that zero read as a reading.
	noGust := mapForecastBody(t, forecastBody(t,
		slotWith(map[string]any{"speed": 5.0, "deg": 90}),
	), 5)
	if noGust.Current.WindGust != nil {
		t.Fatalf("expected no legacy gust for a slot that sent none, got %v", *noGust.Current.WindGust)
	}
	// The faithful slot agrees: the same absent member, null there and omitted here,
	// which is the one place the two vocabularies differ in shape rather than in value.
	if noGust.Forecast[0].Hourly[0].Wind == nil || noGust.Forecast[0].Hourly[0].Wind.Gust != nil {
		t.Fatalf("expected a null faithful gust for a slot that sent none, got %#v", noGust.Forecast[0].Hourly[0].Wind)
	}

	// The upstream measured 0 m/s. That is a reading, and a non-nil pointer to zero is
	// what distinguishes it from the case above: the key is present and says 0.
	measuredZero := mapForecastBody(t, forecastBody(t,
		slotWith(map[string]any{"speed": 5.0, "deg": 90, "gust": 0}),
	), 5)
	if measuredZero.Current.WindGust == nil {
		t.Fatal("expected a legacy gust for a measured 0, got null")
	}
	if *measuredZero.Current.WindGust != 0 {
		t.Fatalf("expected a measured 0 gust to serialise as 0, got %v", *measuredZero.Current.WindGust)
	}
	if slot := measuredZero.Forecast[0].Hourly[0].Wind; slot == nil || slot.Gust == nil || *slot.Gust != 0 {
		t.Fatalf("expected a faithful gust of 0, got %#v", slot)
	}

	// The ordinary case: a gust is a gust.
	measured := mapForecastBody(t, forecastBody(t,
		slotWith(map[string]any{"speed": 5.0, "deg": 90, "gust": 7.5}),
	), 5)
	requireFloat(t, "a measured gust", measured.Current.WindGust, 7.5)
	if slot := measured.Forecast[0].Hourly[0].Wind; slot == nil || slot.Gust == nil || *slot.Gust != 7.5 {
		t.Fatalf("expected a faithful gust of 7.5, got %#v", slot)
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

// An empty response has no slot to read a current block from, so every member of it
// is null. Before the block was pointer shaped this test asserted zeroes, and the
// 0 of last_updated was 0001-01-01 rather than a date anybody measured.
func TestMapForecastWithNoItemsLeavesCurrentNull(t *testing.T) {
	svc := NewWeatherService("dummy")

	data := svc.mapForecast(models.OpenWeatherMapForecastResponse{}, models.ForecastPayload{}, 5)

	if data.Current.Temperature != nil || data.Current.Condition != nil {
		t.Fatalf("expected a null current for an empty response, got %+v", data.Current)
	}
	if data.Current.LastUpdated != nil {
		t.Fatalf("expected a null last_updated for an empty response, got %s", data.Current.LastUpdated)
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

	// The legacy block and the faithful one are both built from this presence view, so
	// every reading the test asserts has to be declared in it. The three timestamps
	// and the weather array are read from the decode struct, which is why they are
	// not here.
	temp, feelsLike, dewPoint, uvi, speed, gust := 12.4, 11.0, 7.7, 2.1, 4.2, 6.0
	pressure, humidity, clouds, visibility, deg := 1008, 64, 30, 9000, 210
	payload := models.SevenDayPayload{Current: &models.SevenDayPayloadCurrent{
		Temp:       &temp,
		FeelsLike:  &feelsLike,
		Pressure:   &pressure,
		Humidity:   &humidity,
		DewPoint:   &dewPoint,
		Uvi:        &uvi,
		Clouds:     &clouds,
		Visibility: &visibility,
		WindSpeed:  &speed,
		WindDeg:    &deg,
		WindGust:   &gust,
	}}

	data := svc.mapOneCall(owm, payload, &models.Location{Name: "London"})

	// Same expected values as before the block became pointer shaped; the comparison
	// is what changed. This route's legacy block and its faithful twin are built from
	// one presence view, so neither can report a reading the other has not got.
	got := data.Current
	requireFloat(t, "the current temperature", got.Temperature, 12.4)
	requireFloat(t, "the current feels_like", got.FeelsLike, 11.0)
	requireFloat(t, "the current pressure", got.Pressure, 1008)
	requireInt(t, "the current humidity", got.Humidity, 64)
	requireFloat(t, "the current visibility", got.Visibility, 9000)
	requireInt(t, "the current cloud_cover", got.CloudCover, 30)
	requireFloat(t, "the current wind_speed", got.WindSpeed, 4.2)
	requireInt(t, "the current wind_direction", got.WindDirection, 210)
	requireFloat(t, "the current wind_gust", got.WindGust, 6.0)
	requireString(t, "the current condition", got.Condition, "Clouds")
	requireString(t, "the current icon", got.Icon, "04d")
	requireString(t, "the current description", got.Description, "Broken Clouds")
	requireTime(t, "the current last_updated", got.LastUpdated, time.Unix(1772000000, 0))
	// The faithful block is built from the same presence view, so the two read alike.
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

	// Every member of the legacy block is null, not 0. The old zero value reported a
	// temperature of 0 degrees and a last_updated of 1970 beside a faithful block
	// that said null, which is five fabrications and one invented date.
	if data.Current.Temperature != nil || data.Current.Condition != nil {
		t.Fatalf("expected a null legacy current when the block is absent, got %+v", data.Current)
	}
	if data.Current.LastUpdated != nil {
		t.Fatalf("expected a null last_updated when the block is absent, got %s", data.Current.LastUpdated)
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
