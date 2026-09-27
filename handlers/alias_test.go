package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// forecastAliasJSON is the /data/2.5/forecast body the alias table reads the
// forecast route from. It is a variant rather than the shared forecastJSON because
// that fixture cannot make two of the four forecast rows observable: it carries no
// pop on either slot, so the day reports chance_of_rain and pop as null together and
// the pair has no number to compare, and it carries rain on one slot and no snow at
// all, so the day's snow is null and rain plus snow is not a sum of two numbers.
//
// The two slots here report different volumes of different things and different
// probabilities, so the day reports both precipitation blocks as numbers and one
// probability that is neither slot's. The probabilities are 0.25 and 0.5 because
// those are values whose *100 is exact in binary floating point, so the percentage
// the table derives by truncation is the percentage the mapper produced by rounding,
// and the row tests the alias rather than the rounding rule. See aliasPercent for why
// that is a limitation of the row rather than a strength of it.
func forecastAliasJSON(t *testing.T) string {
	t.Helper()
	return forecastRouteBody(t,
		forecastSlot(0, map[string]any{
			"main":    map[string]any{"temp": 18.0, "feels_like": 17.2, "temp_min": 15.0, "temp_max": 21.0, "pressure": 1012, "humidity": 70},
			"weather": []any{map[string]any{"id": 500, "main": "Rain", "description": "light rain", "icon": "10d"}},
			"clouds":  map[string]any{"all": 80},
			"wind":    map[string]any{"speed": 5.0, "deg": 90},
			"rain":    map[string]any{"3h": 1.2},
			"pop":     0.25,
		}),
		forecastSlot(3, map[string]any{
			"main":    map[string]any{"temp": 19.0, "feels_like": 18.0, "temp_min": 16.0, "temp_max": 22.0, "pressure": 1011, "humidity": 65},
			"weather": []any{map[string]any{"id": 600, "main": "Snow", "description": "light snow", "icon": "13d"}},
			"clouds":  map[string]any{"all": 0},
			"wind":    map[string]any{"speed": 3.0, "deg": 100},
			"snow":    map[string]any{"3h": 0.8},
			"pop":     0.5,
		}),
	)
}

// aliasCombine is how a faithful value becomes the number the legacy key claims to
// be an alias of. Most rows need none of these: both vocabularies are given the same
// local by the mapper, so the two keys serialise to the same number and the row is a
// plain comparison. The two that do need one are the rows where the legacy key is
// not the same quantity as its faithful twin, only a different rendering of it.
type aliasCombine int

const (
	// aliasSame is the plain case: the faithful value is the legacy value.
	aliasSame aliasCombine = iota
	// aliasPercent is a probability the legacy vocabulary reports as a whole
	// percentage. The rule is written here the way the table writes it, as a
	// truncation, and the fixture is built so the truncation and the mapper's
	// rounding cannot differ.
	//
	// That is a limitation of the row and worth stating plainly. Both mappers round
	// rather than truncate, because 0.29 is 28.999999999999996 in binary floating
	// point, so a plain int() call would answer 28 for a day the upstream called 29
	// percent. A fixture carrying a probability whose percentage is not exactly
	// representable would therefore fail this row for a reason that has nothing to do
	// with the alias. The rule the mappers follow is math.Round and the row is worth
	// restating in those terms; until it is, the probabilities in forecastAliasJSON
	// and in the shared sevenDayJSON are chosen to keep the two rules
	// indistinguishable.
	aliasPercent
	// aliasSum is a total the legacy vocabulary reports as one number and the
	// faithful one reports as two blocks of volumes. A null block contributes nothing,
	// which is the reading both mappers give it, so the sum is over the blocks that
	// carry a volume.
	aliasSum
)

// aliasRow is one legacy key and the faithful key that has to carry the same reading.
// The faithful side is a list because a total is built from two blocks.
type aliasRow struct {
	route    string
	legacy   string
	faithful []string
	combine  aliasCombine
}

// aliasRows is the table. Every row is asserted, and a row whose keys are absent or
// unreadable fails rather than skips, because a key that quietly stopped being
// emitted is one of the two failure modes this table exists to catch.
var aliasRows = []aliasRow{
	// /current
	{aliasCurrentRoute, "data.current.temperature", []string{"data.main.temp"}, aliasSame},
	{aliasCurrentRoute, "data.current.feels_like", []string{"data.main.feels_like"}, aliasSame},
	{aliasCurrentRoute, "data.current.humidity", []string{"data.main.humidity"}, aliasSame},
	{aliasCurrentRoute, "data.current.pressure", []string{"data.main.pressure"}, aliasSame},
	{aliasCurrentRoute, "data.current.wind_speed", []string{"data.wind.speed"}, aliasSame},
	{aliasCurrentRoute, "data.current.wind_direction", []string{"data.wind.deg"}, aliasSame},
	{aliasCurrentRoute, "data.current.cloud_cover", []string{"data.clouds.all"}, aliasSame},

	// /forecast. The faithful twin of a probability and of a total lives in the same
	// day's object here, because on this route the two vocabularies are two field
	// vocabularies over one rollup rather than two namespaces.
	{aliasForecastRoute, "data.forecast[0].chance_of_rain", []string{"data.forecast[0].pop"}, aliasPercent},
	{aliasForecastRoute, "data.forecast[0].max_temperature", []string{"data.forecast[0].temp.max"}, aliasSame},
	{aliasForecastRoute, "data.forecast[0].min_temperature", []string{"data.forecast[0].temp.min"}, aliasSame},
	{aliasForecastRoute, "data.forecast[0].precipitation", []string{"data.forecast[0].rain.3h", "data.forecast[0].snow.3h"}, aliasSum},

	// /forecast/7day. The faithful One Call data is namespaced under one key, because
	// the legacy current block and the faithful current object both want the JSON key
	// current and two Go fields cannot share one tag.
	{aliasSevenDayRoute, "data.forecast[0].chance_of_rain", []string{"data.onecall.daily[0].pop"}, aliasPercent},
	{aliasSevenDayRoute, "data.forecast[0].max_temperature", []string{"data.onecall.daily[0].temp.max"}, aliasSame},
	{aliasSevenDayRoute, "data.current.temperature", []string{"data.onecall.current.temp"}, aliasSame},
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
// That is what a null block means to the sum, which has to be able to ask about a
// day nothing fell on and be told nothing, and it cannot turn a row into a false pass:
// every row below also requires both of its values to be numbers, so null against
// null is not a comparison this table can make.
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

// aliasFaithful renders the faithful side of a row as the number the legacy key
// claims to be an alias of.
func aliasFaithful(t *testing.T, data map[string]any, spec aliasRow) any {
	t.Helper()
	switch spec.combine {
	case aliasPercent:
		return float64(int(aliasNumber(t, spec, digAlias(t, data, spec.faithful[0])) * 100))
	case aliasSum:
		total := 0.0
		contributing := 0
		for _, path := range spec.faithful {
			value := digAlias(t, data, path)
			if value == nil {
				// A null block is a day nothing of that kind fell on, and it adds
				// nothing to the total, which is the reading both mappers give it.
				continue
			}
			total += aliasNumber(t, spec, value)
			contributing++
		}
		if contributing == 0 {
			t.Fatalf("the fixture reports no precipitation volume at all, so %s and %s are both null and their sum is not a number",
				spec.faithful[0], spec.faithful[1])
		}
		return total
	default:
		return digAlias(t, data, spec.faithful[0])
	}
}

// aliasNumber requires a value to be a reading rather than a gap. Every row is on a
// fixture that reports the upstream measurement, so a null on either side is the
// route failing to report something it was sent, and saying so beats comparing two
// nulls and calling it agreement.
func aliasNumber(t *testing.T, spec aliasRow, value any) float64 {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("%s: expected a number, got %#v", aliasLabel(spec), value)
	}
	return number
}

// aliasLabel renders a row the way the table writes it, for the subtest name and for
// the failure messages.
func aliasLabel(spec aliasRow) string {
	faithful := strings.Join(spec.faithful, " + ")
	if spec.combine == aliasPercent {
		faithful += "*100"
	}
	return spec.route + " " + spec.legacy + " == " + faithful
}

// Every legacy key a consumer already reads has to be the same reading as the
// faithful key that mirrors the upstream schema, on every route. The class of bug
// this guards is the one that produced a permanent chance_of_rain of 0: a field
// present, plausible and wrong, which a caller cannot tell from a field that is
// right, and a missing field at least announces itself.
//
// The three routes answer from two servers and never from the network. Each route is
// fetched once and every row for it is read from that one body, because a row that
// fetched a copy of its own could pass against a cached body and fail against a fresh
// one and tell nobody which of the two it had seen.
func TestLegacyAliasesMatchFaithfulFields(t *testing.T) {
	// The shared stub serves the current and the seven day fixtures. The forecast
	// route gets its own server because its fixture is the variant built above.
	shared := newStubbedRouter(t, &upstreamStub{})

	bodies := map[string]map[string]any{}
	for _, route := range []string{aliasCurrentRoute, aliasSevenDayRoute} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/weather"+route+"?location=London,UK&units=metric", nil)
		w := httptest.NewRecorder()
		shared.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200 got %d body=%s", route, w.Code, w.Body.String())
		}
		bodies[route] = decodeData(t, w.Body)
	}
	forecastData, _ := requestForecastRoute(t, newForecastRouteRouter(t, forecastAliasJSON(t)))
	bodies[aliasForecastRoute] = forecastData

	for _, spec := range aliasRows {
		t.Run(aliasLabel(spec), func(t *testing.T) {
			body, ok := bodies[spec.route]
			if !ok {
				t.Fatalf("no body was fetched for route %q", spec.route)
			}
			legacy := aliasNumber(t, spec, digAlias(t, body, spec.legacy))
			faithful := aliasNumber(t, spec, aliasFaithful(t, body, spec))
			if legacy != faithful {
				t.Fatalf("expected %s and %s to be the same reading, got %v and %v",
					spec.legacy, aliasLabel(spec), legacy, faithful)
			}
		})
	}
}
