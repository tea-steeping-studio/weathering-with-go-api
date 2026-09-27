package handlers

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// currentAliasJSON is the /data/2.5/weather body the alias table reads the current
// route from, a private variant on the same pattern as forecastAliasJSON. It carries
// the readings of the shared currentWeatherJSON and adds the two the shared fixture
// does not send, which are exactly the two whose rows cannot exist without them.
//
//   - wind.gust, for the legacy current.wind_gust and its faithful twin wind.gust.
//     Both are read from the same datum, payload.Wind.Gust, at services/weather.go
//     :766 and :708, so the pair is expressible the moment the upstream sends a gust.
//     Without one the legacy key carries omitempty and is absent while the faithful
//     one is null, and there is no pair to compare.
//   - visibility, for the legacy current.visibility and the faithful top-level
//     visibility. This is not a missing row but a vacuous one, and it is worth
//     naming: OpenWeatherMapResponse.Visibility is a plain int, so a body that sends
//     no visibility decodes to 0 and both vocabularies faithfully report that 0. The
//     row then compares a fabricated zero with its twin and passes, which is the
//     exact defect this table exists to catch, committed inside the table. A fixture
//     that reports a real reading is the only way the row can mean anything.
//
// The readings are pairwise distinct, so a mapper that swaps any two of them fails
// the row rather than passing it.
const currentAliasJSON = `{"coord":{"lon":-0.13,"lat":51.51},"weather":[{"id":802,"main":"Clouds","description":"scattered clouds","icon":"03d"}],"base":"stations","main":{"temp":15.5,"feels_like":14.8,"temp_min":14.0,"temp_max":17.0,"pressure":1013,"humidity":72,"temp_kf":0.6},"visibility":10000,"wind":{"speed":3.6,"deg":230,"gust":6.1},"clouds":{"all":40},"dt":1234567890,"sys":{"type":2,"id":5081,"country":"GB","sunrise":1771960000,"sunset":1772010000},"id":2643743,"timezone":0,"name":"London","cod":200}`

// forecastAliasJSON is the /data/2.5/forecast body the alias table reads the
// forecast route from. It is a variant rather than the shared forecastJSON because
// that fixture cannot make two of the six forecast day rows observable: it carries
// no pop on either slot, so the day reports chance_of_rain and pop as null together
// and the pair has no number to compare, and it carries rain on one slot and no snow
// at all, so the day's snow is null and rain plus snow is not a sum of two numbers.
//
// It also carries the two readings the legacy current block on this route needs and
// the shared fixture does not send: a wind gust and a visibility, both on slot 0. The
// legacy block is derived from list[0] and the faithful mirror of that same slot is
// at hourly[0], so those two rows are only expressible when the slot reports them.
//
// The two slots report different volumes of different things and different
// probabilities, so the day reports both precipitation blocks as numbers. The day's
// probability is the higher of the two slots', so slot 0 carries 0.29 and slot 3
// carries 0.12.
//
// 0.29 is the probability that makes the rounding row mean something. 0.29*100 is
// 28.999999999999996 in binary floating point, so a mapper that truncates answers 28
// for a day the upstream called 29 percent, and a mapper that rounds answers 29. A
// probability whose percentage is exact in binary floating point, such as 0.25 or
// 0.5, would let both rules through and the row would assert nothing about the
// rounding at all. See aliasPercent.
func forecastAliasJSON(t *testing.T) string {
	t.Helper()
	return forecastRouteBody(t,
		forecastSlot(0, map[string]any{
			"main":    map[string]any{"temp": 18.0, "feels_like": 17.2, "temp_min": 15.0, "temp_max": 21.0, "pressure": 1012, "humidity": 70},
			"weather": []any{map[string]any{"id": 500, "main": "Rain", "description": "light rain", "icon": "10d"}},
			"clouds":  map[string]any{"all": 80},
			"wind":    map[string]any{"speed": 5.0, "deg": 90, "gust": 7.5},
			"rain":    map[string]any{"3h": 1.2},
			"pop":     0.29,
			// The legacy current block on this route is derived from list[0], so a
			// reading only slot 0 carries is the one place the faithful mirror of that
			// block can be read from.
			"visibility": 10000,
		}),
		forecastSlot(3, map[string]any{
			"main":    map[string]any{"temp": 19.0, "feels_like": 18.0, "temp_min": 16.0, "temp_max": 22.0, "pressure": 1011, "humidity": 65},
			"weather": []any{map[string]any{"id": 600, "main": "Snow", "description": "light snow", "icon": "13d"}},
			"clouds":  map[string]any{"all": 0},
			"wind":    map[string]any{"speed": 3.0, "deg": 100},
			"snow":    map[string]any{"3h": 0.8},
			"pop":     0.12,
		}),
	)
}

// aliasCombine is how a faithful value becomes the quantity the legacy key claims to
// be an alias of. Most rows need none of these: the two vocabularies are given the
// same local by the mapper, so the two keys serialise to the same value and the row
// is a plain comparison. The rest are rows where the legacy key is the same
// measurement in a different rendering of it, which is the only reason a legacy key
// and its faithful twin can be spelled differently and still be one reading.
type aliasCombine int

const (
	// aliasSame is the plain case: the faithful value is the legacy value. It is a
	// numeric comparison of one path against one path.
	aliasSame aliasCombine = iota
	// aliasText is the plain case for a string, which is what a condition and an icon
	// are. It exists as its own kind only so that a text row cannot be satisfied by a
	// number and a numeric row cannot be satisfied by a string.
	aliasText
	// aliasInstant is an instant the legacy vocabulary renders as RFC3339 in UTC and
	// the faithful one reports as the upstream's epoch. Same reading, two renderings.
	aliasInstant
	// aliasPercent is a probability the legacy vocabulary reports as a whole
	// percentage, so the row compares that percentage against the faithful
	// probability scaled the way the mapper scales it.
	//
	// The rule is int(math.Round(pop*100)) and it has to be that, rounding and not
	// truncation. Both mappers round, and the reason is the one this whole plan
	// exists for: 0.29*100 is 28.999999999999996 in binary floating point, so a plain
	// int() call answers 28 for a day the upstream called 29 percent. A percentage
	// that is quietly wrong by one is the same class of defect as the permanent
	// chance_of_rain of 0 this project was built to fix, and it is one nobody can see
	// from the response.
	//
	// Both probability rows are therefore driven by a fixture whose pop is 0.29, so
	// that a mapper rounding to truncation fails the row rather than passing it. A
	// pop whose percentage is exact in binary floating point, which is what these
	// fixtures used to carry, cannot tell the two rules apart and pins neither.
	aliasPercent
	// aliasSum is a total the legacy vocabulary reports as one number and the
	// faithful one reports as two volumes. A null volume contributes nothing, which
	// is the reading both mappers give it, so the sum is over the volumes that carry
	// a reading.
	aliasSum
)

// aliasRow is one legacy key and the faithful key that has to carry the same reading.
// The faithful side is a list because a total is built from two volumes.
type aliasRow struct {
	route    string
	legacy   string
	faithful []string
	combine  aliasCombine
}

// aliasRows is the table: every legacy key on the legacy Current and legacy Forecast
// blocks of all three routes, paired with the faithful key carrying the same
// reading. Every row is asserted, and a row whose keys are absent or unreadable
// fails rather than skips, because a key that quietly stopped being emitted is one
// of the two failure modes this table exists to catch.
//
// The legacy Location block and request_time are out of scope, the first because the
// audit was scoped to the Current and Forecast blocks and the second because it comes
// from the clock rather than from the upstream, so it has no faithful twin on any
// route.
//
// The kinds of pair deliberately absent, each absent for a reason rather than by
// oversight. They are listed in full in the task 6 report.
//
//   - description, on all three routes. The legacy vocabulary title cases it, as it
//     has always done, and the faithful mirror reports the upstream string verbatim.
//     The two are one reading in two renderings, and the renderings are different on
//     purpose, so no row can assert them equal.
//   - max_temperature and min_temperature on /current and on /forecast/7day. Neither
//     route measures extremes in its current block, so both legacy keys are null, and
//     the upstream's own temp_max and temp_min are a different measurement: the
//     window the upstream measured, not the route's.
//   - humidity and wind_speed on the /forecast day, and date on that day. On this
//     route the legacy and faithful vocabularies share one JSON key, because there is
//     no upstream daily block to name the measurement differently. A row would
//     compare a value with itself and assert nothing.
//   - uv_index on the /forecast day. The three hour endpoint documents no uvi, so the
//     route has no ultraviolet reading to alias and both keys are null by design.
var aliasRows = []aliasRow{
	// ---- /current. The faithful block is the /data/2.5/weather mirror at the top
	// level of the same data object.
	{aliasCurrentRoute, "data.current.temperature", []string{"data.main.temp"}, aliasSame},
	{aliasCurrentRoute, "data.current.feels_like", []string{"data.main.feels_like"}, aliasSame},
	{aliasCurrentRoute, "data.current.humidity", []string{"data.main.humidity"}, aliasSame},
	{aliasCurrentRoute, "data.current.pressure", []string{"data.main.pressure"}, aliasSame},
	{aliasCurrentRoute, "data.current.visibility", []string{"data.visibility"}, aliasSame},
	{aliasCurrentRoute, "data.current.wind_speed", []string{"data.wind.speed"}, aliasSame},
	{aliasCurrentRoute, "data.current.wind_direction", []string{"data.wind.deg"}, aliasSame},
	{aliasCurrentRoute, "data.current.wind_gust", []string{"data.wind.gust"}, aliasSame},
	{aliasCurrentRoute, "data.current.cloud_cover", []string{"data.clouds.all"}, aliasSame},
	{aliasCurrentRoute, "data.current.condition", []string{"data.weather[0].main"}, aliasText},
	{aliasCurrentRoute, "data.current.icon", []string{"data.weather[0].icon"}, aliasText},
	{aliasCurrentRoute, "data.current.last_updated", []string{"data.dt"}, aliasInstant},

	// ---- /forecast, legacy current block. The block is derived from list[0] and the
	// faithful mirror of that same slot is carried raw at hourly[0], so every row
	// below reads slot 0 rather than the day rollup. The rollup is a different
	// measurement: its temperature is a minimum, a maximum and a mean, not the slot's.
	{aliasForecastRoute, "data.current.temperature", []string{"data.forecast[0].hourly[0].main.temp"}, aliasSame},
	{aliasForecastRoute, "data.current.feels_like", []string{"data.forecast[0].hourly[0].main.feels_like"}, aliasSame},
	{aliasForecastRoute, "data.current.humidity", []string{"data.forecast[0].hourly[0].main.humidity"}, aliasSame},
	{aliasForecastRoute, "data.current.pressure", []string{"data.forecast[0].hourly[0].main.pressure"}, aliasSame},
	{aliasForecastRoute, "data.current.visibility", []string{"data.forecast[0].hourly[0].visibility"}, aliasSame},
	{aliasForecastRoute, "data.current.wind_speed", []string{"data.forecast[0].hourly[0].wind.speed"}, aliasSame},
	{aliasForecastRoute, "data.current.wind_direction", []string{"data.forecast[0].hourly[0].wind.deg"}, aliasSame},
	{aliasForecastRoute, "data.current.wind_gust", []string{"data.forecast[0].hourly[0].wind.gust"}, aliasSame},
	{aliasForecastRoute, "data.current.cloud_cover", []string{"data.forecast[0].hourly[0].clouds.all"}, aliasSame},
	{aliasForecastRoute, "data.current.max_temperature", []string{"data.forecast[0].hourly[0].main.temp_max"}, aliasSame},
	{aliasForecastRoute, "data.current.min_temperature", []string{"data.forecast[0].hourly[0].main.temp_min"}, aliasSame},
	{aliasForecastRoute, "data.current.condition", []string{"data.forecast[0].hourly[0].weather[0].main"}, aliasText},
	{aliasForecastRoute, "data.current.icon", []string{"data.forecast[0].hourly[0].weather[0].icon"}, aliasText},
	{aliasForecastRoute, "data.current.last_updated", []string{"data.forecast[0].hourly[0].dt"}, aliasInstant},

	// ---- /forecast, legacy day block. Here the two vocabularies are two field
	// vocabularies over one rollup, so the faithful twin of a day key is in the same
	// day's object rather than in a second namespace.
	{aliasForecastRoute, "data.forecast[0].max_temperature", []string{"data.forecast[0].temp.max"}, aliasSame},
	{aliasForecastRoute, "data.forecast[0].min_temperature", []string{"data.forecast[0].temp.min"}, aliasSame},
	{aliasForecastRoute, "data.forecast[0].condition", []string{"data.forecast[0].weather[0].main"}, aliasText},
	{aliasForecastRoute, "data.forecast[0].icon", []string{"data.forecast[0].weather[0].icon"}, aliasText},
	{aliasForecastRoute, "data.forecast[0].chance_of_rain", []string{"data.forecast[0].pop"}, aliasPercent},
	// The brief for this row reads "data.forecast[0].rain + data.forecast[0].snow",
	// which is not expressible: those two paths are objects, not numbers. On this
	// route the faithful day reports each as a block with a 3h window inside it,
	// ForecastRainBlock and ForecastSnowBlock, because /data/2.5/forecast documents
	// the 3h window and not the 1h one. The row therefore reads the 3h members,
	// which is the same sum the mapper performs. Do not "fix" this back to the
	// brief's paths: the arithmetic is right, the paths in the brief were not.
	{aliasForecastRoute, "data.forecast[0].precipitation", []string{"data.forecast[0].rain.3h", "data.forecast[0].snow.3h"}, aliasSum},

	// ---- /forecast/7day, legacy current block, against the faithful One Call
	// current object. The faithful data is namespaced under one key, because the
	// legacy current block and the faithful current object both want the JSON key
	// current and two Go fields cannot share one tag.
	//
	// Nine of the twelve rows below are also asserted by
	// TestSevenDayLegacyCurrentAliasesFaithfulCurrent, which covers the numeric pairs
	// of this same block against this same fixture. They are repeated here on
	// purpose: that test is the guard on this block, this table is the guard on the
	// two vocabularies agreeing across all three routes, and a pair that is only
	// pinned by one of the two disappears if the other is deleted or narrowed. The
	// three rows that test does not have are condition, icon and last_updated, so
	// this table is not a copy of it.
	{aliasSevenDayRoute, "data.current.temperature", []string{"data.onecall.current.temp"}, aliasSame},
	{aliasSevenDayRoute, "data.current.feels_like", []string{"data.onecall.current.feels_like"}, aliasSame},
	{aliasSevenDayRoute, "data.current.humidity", []string{"data.onecall.current.humidity"}, aliasSame},
	{aliasSevenDayRoute, "data.current.pressure", []string{"data.onecall.current.pressure"}, aliasSame},
	{aliasSevenDayRoute, "data.current.visibility", []string{"data.onecall.current.visibility"}, aliasSame},
	{aliasSevenDayRoute, "data.current.wind_speed", []string{"data.onecall.current.wind_speed"}, aliasSame},
	{aliasSevenDayRoute, "data.current.wind_direction", []string{"data.onecall.current.wind_deg"}, aliasSame},
	{aliasSevenDayRoute, "data.current.wind_gust", []string{"data.onecall.current.wind_gust"}, aliasSame},
	{aliasSevenDayRoute, "data.current.cloud_cover", []string{"data.onecall.current.clouds"}, aliasSame},
	{aliasSevenDayRoute, "data.current.condition", []string{"data.onecall.current.weather[0].main"}, aliasText},
	{aliasSevenDayRoute, "data.current.icon", []string{"data.onecall.current.weather[0].icon"}, aliasText},
	{aliasSevenDayRoute, "data.current.last_updated", []string{"data.onecall.current.dt"}, aliasInstant},

	// ---- /forecast/7day, legacy day block, against the faithful One Call daily
	// entry. Here the two vocabularies are separate namespaces, so a legacy day key
	// and its faithful twin live at different paths on the same day.
	{aliasSevenDayRoute, "data.forecast[0].max_temperature", []string{"data.onecall.daily[0].temp.max"}, aliasSame},
	{aliasSevenDayRoute, "data.forecast[0].min_temperature", []string{"data.onecall.daily[0].temp.min"}, aliasSame},
	{aliasSevenDayRoute, "data.forecast[0].avg_temperature", []string{"data.onecall.daily[0].temp.day"}, aliasSame},
	{aliasSevenDayRoute, "data.forecast[0].humidity", []string{"data.onecall.daily[0].humidity"}, aliasSame},
	{aliasSevenDayRoute, "data.forecast[0].wind_speed", []string{"data.onecall.daily[0].wind_speed"}, aliasSame},
	{aliasSevenDayRoute, "data.forecast[0].condition", []string{"data.onecall.daily[0].weather[0].main"}, aliasText},
	{aliasSevenDayRoute, "data.forecast[0].icon", []string{"data.onecall.daily[0].weather[0].icon"}, aliasText},
	// On this route the two volumes are plain numbers on the faithful day, not the
	// windowed blocks the five day route uses, because One Call reports a total for
	// the day and has no window to describe.
	{aliasSevenDayRoute, "data.forecast[0].precipitation", []string{"data.onecall.daily[0].rain", "data.onecall.daily[0].snow"}, aliasSum},
	{aliasSevenDayRoute, "data.forecast[0].chance_of_rain", []string{"data.onecall.daily[0].pop"}, aliasPercent},
	{aliasSevenDayRoute, "data.forecast[0].uv_index", []string{"data.onecall.daily[0].uvi"}, aliasSame},
	{aliasSevenDayRoute, "data.forecast[0].date", []string{"data.onecall.daily[0].dt"}, aliasInstant},
}

const (
	aliasCurrentRoute  = "/current"
	aliasForecastRoute = "/forecast"
	aliasSevenDayRoute = "/forecast/7day"
)

// digAlias walks a dotted path from a decoded data object, where a segment may carry
// a [n] index. It fails rather than returning nil at the end of a path it could not
// walk, because a row whose key is missing is a failure and not a row with nothing
// to say.
//
// A path that runs into a null partway through resolves to null rather than failing.
// That is what a null volume means to a sum, which has to be able to ask about a day
// nothing fell on and be told nothing, and it cannot turn a row into a false pass:
// every row requires both of its values to be a real reading, so null against null is
// not a comparison this table can make.
func digAlias(t *testing.T, data map[string]any, path string) any {
	t.Helper()
	var current any = data
	for _, segment := range strings.Split(strings.TrimPrefix(path, "data."), ".") {
		if current == nil {
			return nil
		}
		name := segment
		index := -1
		if open := strings.IndexByte(segment, '['); open >= 0 {
			name = segment[:open]
			parsed, err := strconv.Atoi(strings.Trim(segment[open:], "[]"))
			if err != nil {
				t.Fatalf("path %q has an unparseable index: %v", path, err)
			}
			index = parsed
		}

		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("path %q: expected an object before %q, got %#v", path, name, current)
		}
		value, ok := object[name]
		if !ok {
			t.Fatalf("path %q: expected key %q, got %#v", path, name, object)
		}
		current = value

		if index >= 0 {
			list, ok := value.([]any)
			if !ok {
				t.Fatalf("path %q: expected an array at %q, got %#v", path, name, value)
			}
			if index >= len(list) {
				t.Fatalf("path %q: expected at least %d entries at %q, got %d", path, index+1, name, len(list))
			}
			current = list[index]
		}
	}
	return current
}

// singleFaithful is the one faithful path a row may name when it is comparing one
// value against one. Those rows read the first path and would silently ignore a
// second, so a second is a failure rather than a typo nobody notices.
func singleFaithful(t *testing.T, spec aliasRow) string {
	t.Helper()
	if len(spec.faithful) != 1 {
		t.Fatalf("%s: this row compares one value against one, so it may name exactly one faithful key, got %d: %v",
			aliasLabel(spec), len(spec.faithful), spec.faithful)
	}
	return spec.faithful[0]
}

// aliasNumber requires a value to be a reading rather than a gap. Every row is on a
// fixture that reports the upstream measurement, so a null on either side is the
// route failing to report something it was sent, and saying so beats comparing two
// nulls and calling it agreement.
func aliasNumber(t *testing.T, spec aliasRow, value any) any {
	t.Helper()
	if _, ok := value.(float64); !ok {
		t.Fatalf("%s: expected a number, got %#v", aliasLabel(spec), value)
	}
	return value
}

// aliasTextValue is aliasNumber for a string, so that a text row cannot be satisfied by a
// number and a number cannot be satisfied by a string.
func aliasTextValue(t *testing.T, spec aliasRow, value any) any {
	t.Helper()
	if _, ok := value.(string); !ok {
		t.Fatalf("%s: expected a string, got %#v", aliasLabel(spec), value)
	}
	return value
}

// aliasEpoch renders the legacy side of an instant row as the same number the
// faithful epoch is, so the two can be compared. It parses rather than stringifies
// the faithful side, which keeps the row honest: the legacy rendering is the thing
// under test, and comparing two renderings of it would assert nothing.
func aliasEpoch(t *testing.T, spec aliasRow, value any) any {
	t.Helper()
	rendered, ok := value.(string)
	if !ok {
		t.Fatalf("%s: expected a rendered timestamp, got %#v", aliasLabel(spec), value)
	}
	instant, err := time.Parse(time.RFC3339, rendered)
	if err != nil {
		t.Fatalf("%s: expected an RFC3339 timestamp, got %q: %v", aliasLabel(spec), rendered, err)
	}
	return float64(instant.Unix())
}

// aliasPair renders both sides of a row into the form the row compares: the legacy
// reading and the faithful reading, or what the faithful reading has to be rendered
// into for the two to be one measurement.
func aliasPair(t *testing.T, data map[string]any, spec aliasRow) (any, any) {
	t.Helper()
	legacy := digAlias(t, data, spec.legacy)
	switch spec.combine {
	case aliasText:
		return aliasTextValue(t, spec, legacy), aliasTextValue(t, spec, digAlias(t, data, singleFaithful(t, spec)))
	case aliasInstant:
		return aliasEpoch(t, spec, legacy), aliasNumber(t, spec, digAlias(t, data, singleFaithful(t, spec)))
	case aliasPercent:
		// The legacy side is already a whole percentage. The faithful side is the
		// probability, rounded and scaled the way both mappers scale and round it.
		probability := aliasNumber(t, spec, digAlias(t, data, singleFaithful(t, spec)))
		return aliasNumber(t, spec, legacy), float64(int(math.Round(probability.(float64) * 100)))
	case aliasSum:
		return aliasNumber(t, spec, legacy), aliasNumber(t, spec, aliasSumValue(t, data, spec))
	default:
		return aliasNumber(t, spec, legacy), aliasNumber(t, spec, digAlias(t, data, singleFaithful(t, spec)))
	}
}

// aliasSumValue adds the volumes a total is built from. A null volume is a day nothing of
// that kind fell on and adds nothing, which is the reading both mappers give it.
func aliasSumValue(t *testing.T, data map[string]any, spec aliasRow) float64 {
	t.Helper()
	total := 0.0
	contributing := 0
	for _, path := range spec.faithful {
		value := digAlias(t, data, path)
		if value == nil {
			continue
		}
		total += aliasNumber(t, spec, value).(float64)
		contributing++
	}
	if contributing == 0 {
		t.Fatalf("the fixture reports no precipitation volume at all, so %s and %s are both null and their sum is not a number",
			spec.faithful[0], spec.faithful[1])
	}
	return total
}

// aliasLabel renders a row the way the table writes it, for the subtest name and for
// the failure messages. The label is a diagnostic, so it has to name the rule the row
// actually applies: a percent row says it rounds, because a reader debugging "expected
// 29, got 28" and seeing a truncating formula in the message would blame the test
// when the mapper is at fault.
//
// The route is trimmed of its slashes so that the subtest name contains no path
// separator. testing splits -run patterns on "/", so a name beginning with one cannot
// be selected by -run at all, and the inner slash of /forecast/7day would split that
// name in two as well.
func aliasLabel(spec aliasRow) string {
	faithful := strings.Join(spec.faithful, " + ")
	switch spec.combine {
	case aliasPercent:
		faithful = "round(" + faithful + "*100)"
	case aliasInstant:
		faithful = "epoch(" + faithful + ")"
	}
	route := strings.ReplaceAll(strings.TrimPrefix(spec.route, "/"), "/", "-")
	return route + " " + spec.legacy + " == " + faithful
}

// Every legacy key a consumer already reads has to be the same reading as the
// faithful key that mirrors the upstream schema, on every route. The class of bug
// this guards is the one that produced a permanent chance_of_rain of 0: a field
// present, plausible and wrong, which a caller cannot tell from a field that is
// right, and a missing field at least announces itself.
//
// The table is complete over the legacy Current and legacy Forecast blocks of all
// three routes. Every pair that is expressible is here; the ones that are not are
// listed with their reasons in the comment on aliasRows, so that a reader can tell
// an omission from an oversight.
//
// The three routes answer from three servers and never from the network. Each route
// is fetched once and every row for it is read from that one body, because a row that
// fetched a copy of its own could pass against a cached body and fail against a fresh
// one and tell nobody which of the two it had seen.
func TestLegacyAliasesMatchFaithfulFields(t *testing.T) {
	bodies := map[string]map[string]any{}

	// /current uses the variant above rather than the shared currentWeatherJSON, for
	// the same reason /forecast does: the shared fixture sends no gust and no
	// visibility, and the rows reading those two cannot exist on it.
	bodies[aliasCurrentRoute] = requestCurrentRoute(t, newCurrentRouteRouter(t, currentAliasJSON))

	// /forecast needs the variant built above, because the shared forecastJSON
	// carries no pop, no snow, no gust and no visibility.
	forecastData, _ := requestForecastRoute(t, newForecastRouteRouter(t, forecastAliasJSON(t)))
	bodies[aliasForecastRoute] = forecastData

	// /forecast/7day uses sevenDayRouteFullJSON rather than the shared sevenDayJSON
	// for one value. The shared fixture reports 0.25 on every day, and 0.25*100 is 25
	// exactly in binary floating point, so rounding and truncating agree on it and
	// the probability row would pass against either mapper. The full fixture already
	// reports 0.29, the value that tells the two rules apart, so this reuses an
	// existing fixture rather than adding another variant.
	bodies[aliasSevenDayRoute] = requestSevenDayRoute(t, newSevenDayRouteRouter(t, sevenDayRouteFullJSON(t)))

	for _, spec := range aliasRows {
		t.Run(aliasLabel(spec), func(t *testing.T) {
			body, ok := bodies[spec.route]
			if !ok {
				t.Fatalf("no body was fetched for route %q", spec.route)
			}
			legacy, faithful := aliasPair(t, body, spec)
			if legacy != faithful {
				t.Fatalf("expected the legacy and faithful readings to agree, got %v and %v respectively",
					legacy, faithful)
			}
		})
	}
}
