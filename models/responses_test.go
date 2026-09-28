package models

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
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
//
// time.Time is a leaf by name rather than by shape. It is a struct with eleven exported
// members, so the walk would descend into it and report loc, wall and the rest as if a
// body carried them, but it marshals as a single RFC3339 string and carries no JSON
// member of its own at all.
func jsonMemberStruct(t reflect.Type) (reflect.Type, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return nil, false
	}
	if t.Kind() != reflect.Struct || t.NumField() == 0 {
		return nil, false
	}
	return t, true
}

// timeType is the one struct that is a leaf by name. See jsonMemberStruct.
var timeType = reflect.TypeOf(time.Time{})

// withoutLegacyVocabulary removes from a walk the members that are this project's own
// vocabulary rather than names the upstream sends. The legacy blocks are siblings of
// the faithful mirror inside one response struct, so a walk of the whole struct picks
// them up and the "invented a name" check would fail on every one of them.
//
// It is a named list rather than a rule, because the legacy vocabulary is exactly a
// list and a member added to it has to be added here too: that is the point of naming
// it, the same as naming the allowlist entries above.
func withoutLegacyVocabulary(paths map[string]bool) {
	for prefix, vocabulary := range map[string]reflect.Type{
		"location": reflect.TypeOf(Location{}),
		"current":  reflect.TypeOf(Current{}),
	} {
		for name := range jsonMemberPaths(vocabulary) {
			delete(paths, prefix+"."+name)
		}
	}
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
//
// visibility is not here any more and its removal is the point of the exercise: it
// was allowlisted as alwaysSent, which was a claim the upstream does not honour. A
// body that omits visibility decoded to 0 and both vocabularies reported a
// fabricated 0, and the two agreed with each other so nothing noticed. The payload
// declares it now, and the test still passes without the allowlist entry, which is
// what an allowlist entry is supposed to be checked against.
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

	// The payload must not invent a name the upstream never sends. Two are
	// deliberate: city and clouds are tracked for presence, and the walk
	// reports the upstream block as its members rather than as a member of its own.
	for _, name := range []string{"city", "list.clouds"} {
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

// The payload guard above is not enough, and its own error message is why. A member
// added to the upstream struct and allowlisted rather than declared leaves the payload
// check green, and the response then drops the member silently: the guard says "add it
// to ForecastPayload", a developer adds it there, and hourly[] still has no such key.
// This compares the faithful response body against the upstream, so a member that
// reaches neither is named at the type that is missing it.
//
// The faithful body is ForecastResponse plus the members of one raw slot, because that
// is where the upstream's list[] is reported: a walk of the response type alone treats
// forecast[] as a leaf, and the slots are most of what this check is about. The slots
// are collected under the upstream's own "list" prefix, since what is compared is one
// slot's member list against the upstream's and not the path each is served at. The
// day rollup is not walked at all: it is a derivation and reports no upstream name of
// its own, so a walk of it would be entirely invented names.
func TestForecastResponseCoversUpstreamMembers(t *testing.T) {
	upstream := jsonMemberPaths(reflect.TypeOf(OpenWeatherMapForecastResponse{}))
	collectJSONMemberPaths(reflect.TypeOf(ForecastItem{}), "list", upstream)
	// The array itself is not a member of either body: the walk reports the upstream's
	// list[] as the leaf "list" and the response's forecast[] as the leaf "forecast", so
	// the two names are removed here and the members underneath are what is compared.
	delete(upstream, "list")

	response := jsonMemberPaths(reflect.TypeOf(ForecastResponse{}))
	collectJSONMemberPaths(reflect.TypeOf(ForecastSlot{}), "list", response)
	withoutLegacyVocabulary(response)
	delete(response, "request_time")
	// The day rollup, served at the key the walk reported as a leaf.
	delete(response, "forecast")

	if len(upstream) == 0 {
		t.Fatal("the upstream walk found no members, so this check would pass on anything")
	}
	if len(response) == 0 {
		t.Fatal("the response walk found no members, so this check would pass on anything")
	}

	var missing []string
	for name := range upstream {
		if response[name] || forecastPayloadNarrowed[name] != "" {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("OpenWeatherMapForecastResponse declares members the faithful forecast body omits and the allowlist does not cover: %s\n"+
			"add each one to the response type that reports it, or allowlist it with the reason it is deliberately narrower",
			strings.Join(missing, ", "))
	}

	// Every allowlisted member is a claim about the response, and the two kinds of claim
	// are opposite. An "always sent" entry says the response reports the member from
	// the decode struct instead of from a presence view, so the response has to declare
	// it; without that check, adding a member to the upstream struct and allowlisting it
	// silences both guards at once and the response drops the member silently. A "not
	// documented here" entry says the opposite: the endpoint has no such member, so the
	// response omits the key, and a response that declared it would be inventing one.
	var unbacked, overreported []string
	for name, reason := range forecastPayloadNarrowed {
		if reason == forecastNoHourWindow {
			if response[name] {
				overreported = append(overreported, name)
			}
			continue
		}
		if !response[name] {
			unbacked = append(unbacked, name)
		}
	}
	sort.Strings(unbacked)
	if len(unbacked) > 0 {
		t.Errorf("the allowlist claims the faithful forecast body reports these upstream members, and it does not: %s\n"+
			"add each one to the response type that reports it, or drop the allowlist entry and declare it in the payload",
			strings.Join(unbacked, ", "))
	}
	sort.Strings(overreported)
	if len(overreported) > 0 {
		t.Errorf("the allowlist says this endpoint does not document these members, and the faithful forecast body reports them anyway: %s\n"+
			"drop each one from the response type, or declare the endpoint as documenting it and bind it in the payload",
			strings.Join(overreported, ", "))
	}

	// The faithful body must not invent a name the upstream never sends. None are
	// expected: every key in it is a key in the /data/2.5/forecast schema or a member
	// of one of its blocks, which the walk reports as their members rather than as a
	// block of its own.
	var invented []string
	for name := range response {
		if !upstream[name] {
			invented = append(invented, name)
		}
	}
	sort.Strings(invented)
	if len(invented) > 0 {
		t.Errorf("the faithful forecast body declares members the upstream never sends: %s", strings.Join(invented, ", "))
	}
}

// The same pair as the seven day route's two guards, on the current route. Both are
// needed, and the payload guard's error message is the reason: a member added to the
// upstream struct and allowlisted rather than declared leaves the payload check green
// while the response drops the member. A member added to the payload and forgotten in
// the response type is the same failure one step later along.
func TestCurrentResponseCoversUpstreamMembers(t *testing.T) {
	upstream := jsonMemberPaths(reflect.TypeOf(OpenWeatherMapResponse{}))
	response := jsonMemberPaths(reflect.TypeOf(CurrentWeatherResponse{}))
	withoutLegacyVocabulary(response)
	delete(response, "request_time")
	// timezone is bound by the payload and declared nowhere else: OpenWeatherMapResponse
	// does not carry it because no legacy field is derived from it, and an unused decode
	// field is worse than a missing one. The payload guard is what covers it.
	delete(response, "timezone")

	if len(upstream) == 0 {
		t.Fatal("the upstream walk found no members, so this check would pass on anything")
	}
	if len(response) == 0 {
		t.Fatal("the response walk found no members, so this check would pass on anything")
	}

	var missing []string
	for name := range upstream {
		if response[name] || currentPayloadNarrowed[name] != "" {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("OpenWeatherMapResponse declares members the faithful current body omits and the allowlist does not cover: %s\n"+
			"add each one to the response type that reports it, or allowlist it with the reason it is deliberately narrower",
			strings.Join(missing, ", "))
	}

	// Every allowlisted member is a claim that the response reports it from the decode
	// struct rather than from a presence view, so the response has to declare it. Both
	// keys this route does not document are declared here, unlike the forecast route:
	// the shared RainBlock and SnowBlock carry the 3h window with omitempty, so a nil
	// pointer drops the key and a key that could only ever be null is never emitted.
	var unbacked []string
	for name := range currentPayloadNarrowed {
		if !response[name] {
			unbacked = append(unbacked, name)
		}
	}
	sort.Strings(unbacked)
	if len(unbacked) > 0 {
		t.Errorf("the allowlist claims the faithful current body reports these upstream members, and it does not: %s\n"+
			"add each one to the response type that reports it, or drop the allowlist entry and declare it in the payload",
			strings.Join(unbacked, ", "))
	}

	// The faithful body must not invent a name the upstream never sends. None are
	// expected once the legacy vocabulary is out of the way: every remaining key is a
	// key in the /data/2.5/weather schema.
	var invented []string
	for name := range response {
		if !upstream[name] {
			invented = append(invented, name)
		}
	}
	sort.Strings(invented)
	if len(invented) > 0 {
		t.Errorf("the faithful current body declares members the upstream never sends: %s", strings.Join(invented, ", "))
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
)

// forecastPayloadNarrowed lists every JSON member OpenWeatherMapForecastResponse
// declares that ForecastPayload deliberately does not, each with the reason. An
// entry is a claim that the upstream always sends the member, so remove the entry and
// let the test fail if that ever stops being true.
//
// There is no entry for list.main.temp_kf, and its absence is the point. It was
// allowlisted with the reason "not documented for a /data/2.5/forecast slot, so the
// day and slot blocks report it as null", which is false: the OpenWeatherMap
// documentation for /data/2.5/forecast does document list.main.temp_kf. A false claim
// in an allowlist is worse than no claim at all, because it silences the guard that
// would have caught it and suppresses the field: every slot reported a permanent null
// whatever the upstream sent. The member is declared in ForecastPayloadMain and read
// by the slot mapper now, and this list carries only claims about members the upstream
// is documented as sending.
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
	"list.clouds.all":      forecastSlotSent,
	"list.wind.speed":      forecastSlotSent,
	"list.wind.deg":        forecastSlotSent,
	"list.rain.1h":         forecastNoHourWindow,
	"list.snow.1h":         forecastNoHourWindow,
}

// The one call route has the same drift hazard as the other two, and a larger
// surface: it reports the current block, the whole daily array and all three opt-in
// arrays, so a member added to any decode struct and forgotten here is reported as
// null forever. OneCallResponse and SevenDayPayload are the pair to keep in step.
//
// The walk covers every block. The three opt-in arrays used to be allowlisted whole,
// because their response types were the upstream decode types and no payload could
// have told the mapper anything those types could act on. They are pointer shaped
// now, so there is nothing left to allowlist on them: the payload is the full mirror
// of every block the response reports, and the only remaining entries are claims
// about members the upstream documents as unconditionally sent.
//
// The opt-in arrays are walked from the response point types on both sides of the
// comparison, which is what they are decoded into on both sides: OneCallResponse no
// longer carries them at all, so the two value-typed decode structs that used to hold
// them are gone rather than merely unread. Nothing is lost by that. A member added to
// OneCallHourlyPoint and forgotten in the payload is a compile error, not a silent
// drop, which is the whole failure mode a duplicated pair invites.
// oneCallUpstreamPaths is the full member list of the /data/3.0/onecall body, with the
// current block walked out into its members and the three opt-in arrays and the daily
// array collected under their own prefix. Both guards below start from it, so they
// cannot disagree about what the upstream declares.
func oneCallUpstreamPaths() map[string]bool {
	upstream := jsonMemberPaths(reflect.TypeOf(OneCallResponse{}))
	// jsonMemberPaths reports a slice as a leaf and descends into a block, so the
	// current block is walked as current.dt and so on by the call above, while the
	// three opt-in arrays and the daily array each need their element type collected
	// under their own prefix. The arrays are most of what this guard is for: the
	// envelope is four scalars.
	collectJSONMemberPaths(reflect.TypeOf(OneCallMinutelyPoint{}), "minutely", upstream)
	collectJSONMemberPaths(reflect.TypeOf(OneCallHourlyPoint{}), "hourly", upstream)
	collectJSONMemberPaths(reflect.TypeOf(OneCallAlertPoint{}), "alerts", upstream)
	collectJSONMemberPaths(reflect.TypeOf(DailyForecast{}), "daily", upstream)
	return upstream
}

// oneCallPayloadPaths is the same walk over the payload.
func oneCallPayloadPaths() map[string]bool {
	// jsonMemberPaths already walked the current block as its members and reported the
	// four arrays as leaves. The three opt-in arrays are the response point types
	// themselves now, so their element types are the ones to collect.
	payload := jsonMemberPaths(reflect.TypeOf(SevenDayPayload{}))
	collectJSONMemberPaths(reflect.TypeOf(OneCallMinutelyPoint{}), "minutely", payload)
	collectJSONMemberPaths(reflect.TypeOf(OneCallHourlyPoint{}), "hourly", payload)
	collectJSONMemberPaths(reflect.TypeOf(OneCallAlertPoint{}), "alerts", payload)
	collectJSONMemberPaths(reflect.TypeOf(SevenDayPayloadDaily{}), "daily", payload)
	return payload
}

// oneCallEnvelopePaths is the same walk over the faithful response body, which is
// what the second guard below checks.
func oneCallEnvelopePaths() map[string]bool {
	envelope := jsonMemberPaths(reflect.TypeOf(OneCallEnvelope{}))
	collectJSONMemberPaths(reflect.TypeOf(OneCallMinutelyPoint{}), "minutely", envelope)
	collectJSONMemberPaths(reflect.TypeOf(OneCallHourlyPoint{}), "hourly", envelope)
	collectJSONMemberPaths(reflect.TypeOf(OneCallAlertPoint{}), "alerts", envelope)
	collectJSONMemberPaths(reflect.TypeOf(OneCallDailyPoint{}), "daily", envelope)
	return envelope
}

// The payload is the presence authority: a member the upstream declares and it does
// not is a member the response reports as null forever, whatever the response types
// say about it. A field added to the upstream struct and allowed for here, without
// being declared, is the case this exists for.
func TestSevenDayPayloadCoversDecodedFields(t *testing.T) {
	upstream := oneCallUpstreamPaths()
	payload := oneCallPayloadPaths()

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

	// The payload must not invent a name the upstream never sends. Two of the
	// deliberate ones are blocks tracked for presence, and the walk reports the
	// upstream block as its members rather than as a member of its own. Three are the
	// opt-in arrays: OneCallResponse no longer declares them, because nothing read
	// them, and the walk now collects each array's members from the response point
	// types instead. The array names themselves are what the endpoint sends, so they
	// look invented to a walk that has been told to compare member lists.
	for _, name := range []string{"daily.temp", "daily.feels_like", "minutely", "hourly", "alerts"} {
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

// The payload guard alone is not enough, and its own error message is why. A member
// added to the upstream struct and allowlisted rather than declared leaves the
// payload check green, and the response then drops the member silently: the guard says
// "add it to SevenDayPayload", a developer adds it there, and onecall.daily[] still has
// no such key. This compares the faithful response body itself against the upstream,
// so a member that reaches neither is named at the type that is missing it.
//
// The allowlist is the same one. Every entry in it is a claim about a member the
// upstream sends unconditionally, and the response does report all of those: the
// entries are the presence view's narrowing, not the response's.
func TestSevenDayResponseCoversUpstreamMembers(t *testing.T) {
	upstream := oneCallUpstreamPaths()
	envelope := oneCallEnvelopePaths()

	if len(upstream) == 0 {
		t.Fatal("the upstream walk found no members, so this check would pass on anything")
	}
	if len(envelope) == 0 {
		t.Fatal("the response walk found no members, so this check would pass on anything")
	}

	var missing []string
	for name := range upstream {
		if envelope[name] || sevenDayPayloadNarrowed[name] != "" {
			continue
		}
		missing = append(missing, name)
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("OneCallResponse declares members the faithful onecall body omits and the allowlist does not cover: %s\n"+
			"add each one to the response type that reports it, or allowlist it with the reason it is deliberately narrower",
			strings.Join(missing, ", "))
	}

	// Every allowlisted member is a claim that the faithful body reports it from the
	// decode struct instead of from a presence view, so the body has to declare it.
	// Without this second check, adding a member to the upstream struct and allowlisting
	// it silences both guards at once and the response drops the member silently, which
	// is the failure this whole test exists to prevent.
	var unbacked []string
	for name := range sevenDayPayloadNarrowed {
		if !envelope[name] {
			unbacked = append(unbacked, name)
		}
	}
	sort.Strings(unbacked)
	if len(unbacked) > 0 {
		t.Errorf("the allowlist claims the faithful onecall body reports these upstream members, and it does not: %s\n"+
			"add each one to the response type that reports it, or drop the allowlist entry and declare it in the payload",
			strings.Join(unbacked, ", "))
	}

	// The faithful body must not invent a name the upstream never sends. None are
	// expected: every key in it is a key in the One Call schema. Two of the deletions
	// are the daily breakdown blocks, reported as their members rather than as a member
	// of their own, and three are the opt-in array names, which the upstream walk
	// collects from the response point types rather than from a decode struct that no
	// longer declares them.
	for _, name := range []string{"daily.temp", "daily.feels_like", "minutely", "hourly", "alerts"} {
		delete(envelope, name)
	}
	var invented []string
	for name := range envelope {
		if !upstream[name] {
			invented = append(invented, name)
		}
	}
	sort.Strings(invented)
	if len(invented) > 0 {
		t.Errorf("the faithful onecall body declares members the upstream never sends: %s", strings.Join(invented, ", "))
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
}

// ParseBlocks is the whole allowlist, and the parse is the only place a caller can
// be told its typo was a typo. The two states that matter are the zero value an
// absent parameter produces and the error a token outside the set produces; a
// boolean or a silently ignored unknown would make a misspelled "alerts" look like
// a working request for a lean body.
func TestParseBlocks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		want    Blocks
		wantErr string
	}{
		// An absent or empty parameter is not a request for anything, and must not be
		// an error either: a client library sending blocks="" is not broken.
		{"absent", "", Blocks{}, ""},
		{"whitespace only", "  ", Blocks{}, ""},
		{"single", "hourly", Blocks{Hourly: true}, ""},
		{"all three", "hourly,minutely,alerts", Blocks{Hourly: true, Minutely: true, Alerts: true}, ""},
		{"subset", "hourly,alerts", Blocks{Hourly: true, Alerts: true}, ""},
		// Case and spacing are the two ways a correct request is written that is not
		// byte-identical to a doc example, and neither is a reason to fail.
		{"upper case", "HOURLY", Blocks{Hourly: true}, ""},
		{"padded", " Hourly , ALERTS ", Blocks{Hourly: true, Alerts: true}, ""},
		// A repeated name is the same request asked twice, not an error.
		{"repeated", "hourly,hourly", Blocks{Hourly: true}, ""},
		// The error names the offending token, because "invalid blocks parameter" on
		// its own tells a caller nothing about which of the three to fix.
		{"unknown", "bogus", Blocks{}, "bogus"},
		// A list fails whole. Honouring the valid part would return a body missing the
		// very block that was misspelled, with no sign that anything went wrong.
		{"partly valid", "hourly,bogus", Blocks{}, "bogus"},
		// An empty token is a trailing or doubled comma, which is a typo too, and the
		// empty string is not one of the three names.
		{"trailing comma", "hourly,", Blocks{}, `""`},
		{"doubled comma", "hourly,,alerts", Blocks{}, `""`},
		{"comma only", ",", Blocks{}, `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseBlocks(tc.raw)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseBlocks(%q) returned %v", tc.raw, err)
				}
			} else {
				if err == nil {
					t.Fatalf("ParseBlocks(%q) accepted it as a block list, want an error", tc.raw)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseBlocks(%q) error %q does not name %q", tc.raw, err, tc.wantErr)
				}
			}
			if got != tc.want {
				t.Fatalf("ParseBlocks(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}

	// The error message advertises blockNames and the parse accepts the literals in its
	// own switch, so the two are pinned against each other here. A block added to one
	// and not the other would make a 400 promise something the parser then rejects.
	for _, name := range blockNames {
		got, err := ParseBlocks(name)
		if err != nil {
			t.Errorf("blockNames advertises %q but ParseBlocks rejects it: %v", name, err)
			continue
		}
		if got == (Blocks{}) {
			t.Errorf("blockNames advertises %q but ParseBlocks selects no block for it", name)
		}
	}
}
