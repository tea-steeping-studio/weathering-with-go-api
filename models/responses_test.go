package models

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// jsonMemberPaths returns every JSON member name of a decode struct, with nested
// structs joined by dots: main.sea_level, wind.gust, rain.1h. A slice, a map, a
// primitive, or a struct with no members of its own counts as a leaf, so a name is
// reported the way the wire reports it. An untagged field is reported under its Go
// name, and a field tagged "-" is skipped, both matching encoding/json.
//
// The walk does not flatten embedded structs. Decode structs here are flat, and if
// one starts embedding, the check fails loudly on the promoted field's wrong path
// rather than passing quietly.
func jsonMemberPaths(t reflect.Type) map[string]bool {
	paths := make(map[string]bool)
	collectJSONMemberPaths(t, "", paths)
	return paths
}

func collectJSONMemberPaths(t reflect.Type, prefix string, paths map[string]bool) {
	for i := range t.NumField() {
		name := jsonMemberName(t.Field(i))
		if name == "" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if inner, ok := jsonMemberStruct(t.Field(i).Type); ok {
			collectJSONMemberPaths(inner, path, paths)
			continue
		}
		paths[path] = true
	}
}

func jsonMemberName(field reflect.StructField) string {
	tag, ok := field.Tag.Lookup("json")
	if !ok {
		return field.Name
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "-" {
		return ""
	}
	if name == "" {
		return field.Name
	}
	return name
}

// jsonMemberStruct unwraps a pointer to a struct that has JSON members of its own
// and reports it as one. Everything else, including an empty struct used to mark a
// block that is only tracked for presence, is a leaf.
func jsonMemberStruct(t reflect.Type) (reflect.Type, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t.NumField() == 0 {
		return nil, false
	}
	return t, true
}

// A member added to the upstream decode struct and not to the pointer-shaped
// payload would be reported as null forever, and nothing else in the suite would
// notice. This walks both schemas and fails on the first name that only the
// upstream struct declares. Tasks 3 and 4 reuse jsonMemberPaths for their own
// payload pairs.
func TestCurrentPayloadCoversDecodedFields(t *testing.T) {
	upstream := jsonMemberPaths(reflect.TypeOf(OpenWeatherMapResponse{}))
	payload := jsonMemberPaths(reflect.TypeOf(CurrentWeatherPayload{}))

	if len(upstream) == 0 {
		t.Fatal("the upstream walk found no members, so this check would pass on anything")
	}

	var uncovered []string
	for name := range upstream {
		if payload[name] || currentPayloadNarrowed[name] != "" {
			continue
		}
		uncovered = append(uncovered, name)
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("OpenWeatherMapResponse declares members the current payload omits and the allowlist does not cover: %s\n"+
			"add each one to CurrentWeatherPayload, or allowlist it with the reason it is deliberately narrower",
			strings.Join(uncovered, ", "))
	}

	// The payload must not invent a name the upstream never sends. Three are
	// deliberate: timezone is bound only here, and clouds and sys are tracked for
	// presence with their members left on the upstream struct.
	for _, name := range []string{"timezone", "clouds", "sys"} {
		delete(payload, name)
	}
	var invented []string
	for name := range payload {
		if !upstream[name] {
			invented = append(invented, name)
		}
	}
	sort.Strings(invented)
	if len(invented) > 0 {
		t.Errorf("CurrentWeatherPayload declares members the upstream never sends: %s", strings.Join(invented, ", "))
	}
}

const (
	// alwaysSent names a member /data/2.5/weather documents as unconditionally
	// sent, so its zero in the upstream struct is a real reading and never an
	// absent member. The mapper reads it from the upstream struct and the payload
	// needs no second view of it.
	alwaysSent = "documented as always sent by /data/2.5/weather, so its zero is a real reading, not an absent member"
	// notOnThisEndpoint names a member the upstream decode struct declares for the
	// shared schema of the /data/2.5 endpoints but this endpoint does not report.
	// The response omits the key entirely rather than reporting it null.
	notOnThisEndpoint = "not documented for /data/2.5/weather, which reports the 1h window only"
)

// currentPayloadNarrowed lists every JSON member OpenWeatherMapResponse declares
// that CurrentWeatherPayload deliberately does not, each with the reason. An entry
// is a claim that the upstream always sends the member, so remove the entry and let
// the test fail if that ever stops being true.
var currentPayloadNarrowed = map[string]string{
	"coord.lon":       alwaysSent,
	"coord.lat":       alwaysSent,
	"weather":         alwaysSent,
	"base":            alwaysSent,
	"main.temp":       alwaysSent,
	"main.feels_like": alwaysSent,
	"main.temp_min":   alwaysSent,
	"main.temp_max":   alwaysSent,
	"main.pressure":   alwaysSent,
	"main.humidity":   alwaysSent,
	"visibility":      alwaysSent,
	"wind.speed":      alwaysSent,
	"wind.deg":        alwaysSent,
	"clouds.all":      alwaysSent,
	"rain.3h":         notOnThisEndpoint,
	"snow.3h":         notOnThisEndpoint,
	"dt":              alwaysSent,
	"sys.type":        alwaysSent,
	"sys.id":          alwaysSent,
	"sys.country":     alwaysSent,
	"sys.sunrise":     alwaysSent,
	"sys.sunset":      alwaysSent,
	"id":              alwaysSent,
	"name":            alwaysSent,
	"cod":             alwaysSent,
}

// The forecast route has the same drift hazard as the current route, and more of
// it: a slot whose rain window measures 0 must not collapse into the same report as
// a slot that never sent a rain block. OpenWeatherMapForecastResponse and
// ForecastPayload are the pair to keep in step.
//
// The scope is those two decode-side types. A response-side field that neither
// declares, such as the day's rolled up pressure, is out of this walk by
// construction: it appears in no decode struct, so nothing can drift relative to
// here. It is covered where it is produced, in TestForecastDayRollsUpSlotMeans.
func TestForecastPayloadCoversDecodedFields(t *testing.T) {
	upstream := jsonMemberPaths(reflect.TypeOf(OpenWeatherMapForecastResponse{}))
	// jsonMemberPaths reports a slice as a leaf, and list is a slice, so the slot
	// members are walked separately. They are most of what this payload is for: the
	// envelope is five unconditional members and a city block, the slots are the
	// body.
	collectJSONMemberPaths(reflect.TypeOf(ForecastItem{}), "list", upstream)

	payload := jsonMemberPaths(reflect.TypeOf(ForecastPayload{}))
	collectJSONMemberPaths(reflect.TypeOf(ForecastPayloadItem{}), "list", payload)

	if len(upstream) == 0 {
		t.Fatal("the upstream walk found no members, so this check would pass on anything")
	}

	var uncovered []string
	for name := range upstream {
		if payload[name] || forecastPayloadNarrowed[name] != "" {
			continue
		}
		uncovered = append(uncovered, name)
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("OpenWeatherMapForecastResponse declares members the forecast payload omits and the allowlist does not cover: %s\n"+
			"add each one to ForecastPayload, or allowlist it with the reason it is deliberately narrower",
			strings.Join(uncovered, ", "))
	}

	// The payload must not invent a name the upstream never sends. Three are
	// deliberate: city, clouds and sys are tracked for presence, and the walk
	// reports the upstream block as its members rather than as a member of its own.
	for _, name := range []string{"city", "list.clouds", "list.sys"} {
		delete(payload, name)
	}
	var invented []string
	for name := range payload {
		if !upstream[name] {
			invented = append(invented, name)
		}
	}
	sort.Strings(invented)
	if len(invented) > 0 {
		t.Errorf("ForecastPayload declares members the upstream never sends: %s", strings.Join(invented, ", "))
	}
}

const (
	// forecastEnvelopeSent names a member of the /data/2.5/forecast envelope the
	// upstream documents as unconditionally sent, so its zero in the decode struct
	// is a real reading and never an absent member.
	forecastEnvelopeSent = "documented as always sent in the /data/2.5/forecast envelope, so its zero is a real reading, not an absent member"
	// forecastCitySent names a member of the city block the upstream documents as
	// unconditionally sent. The payload records only whether the block was there.
	forecastCitySent = "documented as always sent in the /data/2.5/forecast city block, whose members the payload tracks for presence only"
	// forecastSlotSent names a member the upstream documents as unconditionally sent
	// on a three hour slot.
	forecastSlotSent = "documented as always sent on a /data/2.5/forecast slot, so its zero is a real reading, not an absent member"
	// forecastNoHourWindow names a member this endpoint does not document. The
	// response omits the key entirely rather than reporting it null.
	forecastNoHourWindow = "not documented for /data/2.5/forecast, which reports the 3h precipitation window only"
	// forecastNoKelvinFactor names main.temp_kf, which the current weather endpoint
	// documents and the three hour endpoint does not.
	forecastNoKelvinFactor = "not documented for a /data/2.5/forecast slot, so the day and slot blocks report it as null"
)

// forecastPayloadNarrowed lists every JSON member OpenWeatherMapForecastResponse
// declares that ForecastPayload deliberately does not, each with the reason. An
// entry is a claim that the upstream always sends the member, so remove the entry
// and let the test fail if that ever stops being true.
var forecastPayloadNarrowed = map[string]string{
	"cod":             forecastEnvelopeSent,
	"message":         forecastEnvelopeSent,
	"cnt":             forecastEnvelopeSent,
	"city.id":         forecastCitySent,
	"city.name":       forecastCitySent,
	"city.coord.lon":  forecastCitySent,
	"city.coord.lat":  forecastCitySent,
	"city.country":    forecastCitySent,
	"city.population": forecastCitySent,
	"city.timezone":   forecastCitySent,
	"city.sunrise":    forecastCitySent,
	"city.sunset":     forecastCitySent,

	"list.dt":              forecastSlotSent,
	"list.dt_txt":          forecastSlotSent,
	"list.weather":         forecastSlotSent,
	"list.main.temp":       forecastSlotSent,
	"list.main.feels_like": forecastSlotSent,
	"list.main.temp_min":   forecastSlotSent,
	"list.main.temp_max":   forecastSlotSent,
	"list.main.pressure":   forecastSlotSent,
	"list.main.humidity":   forecastSlotSent,
	"list.main.temp_kf":    forecastNoKelvinFactor,
	"list.clouds.all":      forecastSlotSent,
	"list.wind.speed":      forecastSlotSent,
	"list.wind.deg":        forecastSlotSent,
	"list.rain.1h":         forecastNoHourWindow,
	"list.snow.1h":         forecastNoHourWindow,
	"list.sys.pod":         forecastSlotSent,
}
