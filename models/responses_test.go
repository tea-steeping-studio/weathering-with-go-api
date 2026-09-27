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

// The one call route has the same drift hazard as the other two, and one more
// surface: it reports the current block and the whole daily array, so a member
// added to either decode struct and forgotten here is reported as null forever.
// OneCallResponse and SevenDayPayload are the pair to keep in step.
//
// The walk covers every block, not just the two the payload is pointer-shaped for.
// The three opt-in arrays are reported through their upstream decode types, so
// every member of them is a deliberate narrowing and each one is named below rather
// than left to a paragraph in a comment.
func TestSevenDayPayloadCoversDecodedFields(t *testing.T) {
	upstream := jsonMemberPaths(reflect.TypeOf(OneCallResponse{}))
	// jsonMemberPaths reports a slice as a leaf and descends into a block, so the
	// current block is walked as current.dt and so on by the call above, while the
	// three opt-in arrays and the daily array each need their element type collected
	// under their own prefix. The arrays are most of what this guard is for: the
	// envelope is four scalars.
	collectJSONMemberPaths(reflect.TypeOf(Minutely{}), "minutely", upstream)
	collectJSONMemberPaths(reflect.TypeOf(Hourly{}), "hourly", upstream)
	collectJSONMemberPaths(reflect.TypeOf(Alert{}), "alerts", upstream)
	collectJSONMemberPaths(reflect.TypeOf(DailyForecast{}), "daily", upstream)

	// The same two passes over the payload: jsonMemberPaths already walked the
	// current block as its members and reported the daily array as a leaf.
	payload := jsonMemberPaths(reflect.TypeOf(SevenDayPayload{}))
	collectJSONMemberPaths(reflect.TypeOf(SevenDayPayloadDaily{}), "daily", payload)

	if len(upstream) == 0 {
		t.Fatal("the upstream walk found no members, so this check would pass on anything")
	}

	var uncovered []string
	for name := range upstream {
		if payload[name] || sevenDayPayloadNarrowed[name] != "" {
			continue
		}
		uncovered = append(uncovered, name)
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("OneCallResponse declares members the seven day payload omits and the allowlist does not cover: %s\n"+
			"add each one to SevenDayPayload, or allowlist it with the reason it is deliberately narrower",
			strings.Join(uncovered, ", "))
	}

	// The payload must not invent a name the upstream never sends. Two are
	// deliberate: the temp and feels_like blocks are tracked for presence, and the
	// walk reports a decode-side struct as its members rather than as a member of its
	// own, so the two block names look invented.
	for _, name := range []string{"daily.temp", "daily.feels_like"} {
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
		t.Errorf("SevenDayPayload declares members the upstream never sends: %s", strings.Join(invented, ", "))
	}
}

const (
	// oneCallEnvelopeSent names a member of the /data/3.0/onecall envelope the
	// upstream documents as unconditionally sent. A latitude of 0 and a timezone
	// offset of 0 are both real readings there, so the values are read from the
	// decode struct and no second view is kept.
	oneCallEnvelopeSent = "documented as always sent in the /data/3.0/onecall envelope, so its zero is a real reading, not an absent member"
	// oneCallTimeSent names a timestamp or a phase the upstream documents as sent
	// on every current and daily entry. A moonrise of 0 means the moon does not
	// rise on that day at that latitude, which is a reading rather than a gap.
	oneCallTimeSent = "documented as sent on every current and daily entry, so its zero is a real reading, not an absent member"
	// oneCallBreakdownSent names a member the upstream fills inside the daily
	// temp and feels_like blocks. The payload tracks those two blocks for presence
	// as a whole, because the response reports them as pointer-shaped blocks whose
	// six and four members are unconditionally sent.
	oneCallBreakdownSent = "unconditionally sent inside the documented daily temp or feels_like block, whose presence the payload tracks as a whole"
	// oneCallWeatherArray names a weather array. The response reports it as a slice
	// with no omitempty, so an upstream that sent none is already a null array and
	// the payload needs no second view of it.
	oneCallWeatherArray = "reported as a slice the response leaves null when the upstream sent no entry, so the payload needs no second view"
	// oneCallDailySent names a daily reading the upstream documents as sent on every
	// entry and that no legacy key on this route is built from. The two questions
	// SevenDayPayload asks of a member both come out no, so the mapper reads the
	// value from the decode struct and its zero is a real reading.
	oneCallDailySent = "documented as sent on every /data/3.0/onecall daily entry, and read by no legacy key on this route, so its zero is a real reading, not an absent member"
	// oneCallOptInBlock names a member of the three opt-in arrays. The brief fixes
	// their response types as the upstream decode types, which cannot tell a
	// measured zero from an absent member, so every one of these is a narrowing the
	// guard records rather than one it can close. It is the one place on this route
	// where a measured zero and an absent member are reported identically.
	oneCallOptInBlock = "reported through its upstream decode type, which cannot tell a measured zero from an absent member; only the current and daily blocks are pointer-shaped"
)

// sevenDayPayloadNarrowed lists every JSON member OneCallResponse declares that
// SevenDayPayload deliberately does not, each with the reason. An entry is a claim,
// so remove the entry and let the test fail if that ever stops being true.
var sevenDayPayloadNarrowed = map[string]string{
	"lat":              oneCallEnvelopeSent,
	"lon":              oneCallEnvelopeSent,
	"timezone":         oneCallEnvelopeSent,
	"timezone_offset":  oneCallEnvelopeSent,
	"current.dt":       oneCallTimeSent,
	"current.sunrise":  oneCallTimeSent,
	"current.sunset":   oneCallTimeSent,
	"current.weather":  oneCallWeatherArray,
	"daily.dt":         oneCallTimeSent,
	"daily.sunrise":    oneCallTimeSent,
	"daily.sunset":     oneCallTimeSent,
	"daily.moonrise":   oneCallTimeSent,
	"daily.moonset":    oneCallTimeSent,
	"daily.moon_phase": oneCallTimeSent,
	"daily.weather":    oneCallWeatherArray,
	"daily.pressure":   oneCallDailySent,
	"daily.clouds":     oneCallDailySent,

	"daily.temp.day":         oneCallBreakdownSent,
	"daily.temp.min":         oneCallBreakdownSent,
	"daily.temp.max":         oneCallBreakdownSent,
	"daily.temp.night":       oneCallBreakdownSent,
	"daily.temp.morn":        oneCallBreakdownSent,
	"daily.temp.eve":         oneCallBreakdownSent,
	"daily.feels_like.day":   oneCallBreakdownSent,
	"daily.feels_like.night": oneCallBreakdownSent,
	"daily.feels_like.morn":  oneCallBreakdownSent,
	"daily.feels_like.eve":   oneCallBreakdownSent,

	"minutely":               oneCallOptInBlock,
	"minutely.dt":            oneCallOptInBlock,
	"minutely.precipitation": oneCallOptInBlock,
	"hourly":                 oneCallOptInBlock,
	"hourly.dt":              oneCallOptInBlock,
	"hourly.sunrise":         oneCallOptInBlock,
	"hourly.sunset":          oneCallOptInBlock,
	"hourly.temp":            oneCallOptInBlock,
	"hourly.feels_like":      oneCallOptInBlock,
	"hourly.pressure":        oneCallOptInBlock,
	"hourly.humidity":        oneCallOptInBlock,
	"hourly.dew_point":       oneCallOptInBlock,
	"hourly.uvi":             oneCallOptInBlock,
	"hourly.clouds":          oneCallOptInBlock,
	"hourly.visibility":      oneCallOptInBlock,
	"hourly.wind_speed":      oneCallOptInBlock,
	"hourly.wind_deg":        oneCallOptInBlock,
	"hourly.wind_gust":       oneCallOptInBlock,
	"hourly.pop":             oneCallOptInBlock,
	"hourly.rain":            oneCallOptInBlock,
	"hourly.snow":            oneCallOptInBlock,
	"hourly.weather":         oneCallOptInBlock,
	"alerts":                 oneCallOptInBlock,
	"alerts.sender_name":     oneCallOptInBlock,
	"alerts.event":           oneCallOptInBlock,
	"alerts.start":           oneCallOptInBlock,
	"alerts.end":             oneCallOptInBlock,
	"alerts.description":     oneCallOptInBlock,
	"alerts.tags":            oneCallOptInBlock,
}
