# OpenWeatherMap Field Fidelity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Map every documented OpenWeatherMap field on all three weather routes, so `chance_of_rain` reports real values instead of a permanent `0`, and every field the upstream cannot supply is reported as `null` rather than invented.

**Architecture:** One response type per route, each carrying a pointer-field mirror of its own upstream schema alongside the existing response keys, populated by a single mapper so an alias and its faithful twin cannot drift. Opt-in One Call blocks are stripped by copying the cached value and clearing one pointer, never mutating the shared cached object. Upstream structs stay decode targets; new response structs carry the pointer semantics.

**Tech Stack:** Go 1.25, Gin, `encoding/json`, `net/http/httptest` with a custom `RoundTripper` for stubbing upstream.

**Spec:** `docs/superpowers/specs/2026-09-27-openweather-field-fidelity-design.md`

## Global Constraints

- Tests must never touch the network. Stub upstream by replacing `WeatherService.HTTPClient` with a `RoundTripper`, per the existing `transportRedirect` pattern in `services/seven_day_test.go`. The base URLs in `services/weather.go` are `const` and cannot be pointed elsewhere.
- `WeatherService.now` stays a `func` field. Tests advance it for TTL expiry. Never replace it with `time.Now()`.
- The cache key in `weatherCacheKey` (`services/cache.go:32`) does **not** change in this plan. Its component list stays endpoint, normalized location, units, days, api-key hash.
- `cachedFetchTracked` returns the **same pointer** to every concurrent caller. Never mutate a value obtained from it or from `GetCurrentWeather`/`GetWeatherForecast`/`GetSevenDayForecast`. Copy first.
- Every upstream call still routes through `cachedFetch`/`cachedFetchTracked`. No new direct HTTP call is permitted.
- The `/forecast` route must keep costing exactly one upstream call. `TestForecastHandlerDoesNotCallCurrentEndpoint` continues to guard this.
- Response bodies are wrapped by `utils.SendCachedSuccess` as `{"success":true,"data":{...}}`. The new response types are what goes in `data`.
- A field the upstream genuinely measures as `0` serialises as `0`. Only a field with no value serialises as `null`. `omitempty` is permitted on pointer fields, where it drops only `nil` and therefore cannot discard a measured zero, and forbidden on value fields, where it would drop one. On a pointer field the tag means "this endpoint does not document this key".
- Pointer fields represent "may be absent". Non-pointer fields are reserved for values the route always produces.
- Toolchain: if `go` fails with "No version is set for shim", prefix commands with `mise exec go@1.25.7 --`.

## Review Focus

Five input classes the spec implies that are most likely to bite. Each has its test assigned to the owning task below.

1. **A slot reporting `rain: {"3h": 0}`.** The existing `models.Rain` carries `omitempty` on both fields, so a genuine measured zero is indistinguishable from an absent reading and would vanish from the response. Test in Task 3.
2. **Two concurrent requests for the same location, one with `?blocks=hourly` and one without.** They share one cache entry and therefore one pointer; stripping blocks for the lean request must not corrupt the fat one. Test in Task 5.
3. **A forecast whose three-hour slots straddle a UTC midnight.** Slots are grouped by `time.Unix(item.Dt, 0).Format("2006-01-02")`, which is UTC, while the response carries a city `timezone` offset. A local day can split across two groups. Test in Task 3.
4. **Some slots carrying `pop` and others omitting it.** `pop_mean` computed over all slots is diluted by the ones with no value; `pop_max` is unaffected. Test in Task 3.
5. **One Call returning 8 `daily[]` entries while the legacy `forecast[]` is capped at 7.** A client comparing the two arrays finds a length mismatch with no explanation. Test in Task 4.

---

### Task 1: Complete the upstream decode models

The upstream structs in `models/openweather.go` are decode targets. They stay non-pointer: a zero there is fine because the mapper decides what the response reports. This task only adds fields that are currently decoded and then thrown away.

**Files:**
- Modify: `models/openweather.go`
- Test: `models/models_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `models.Main.TempKF float64`; `models.ForecastItem.Pop float64`, `models.ForecastItem.Visibility int`; `models.City.Population int`; `models.DailyForecast.DewPoint float64`, `models.DailyForecast.WindGust float64`, `models.DailyForecast.Sunrise int64`, `models.DailyForecast.Sunset int64`, `models.DailyForecast.Moonrise int64`, `models.DailyForecast.Moonset int64`, `models.DailyForecast.MoonPhase float64`; `models.OneCallResponse.TimezoneOffset int`; new types `models.Minutely{Dt int64, Precipitation float64}`, `models.Hourly{Dt, Sunrise, Sunset int64, Temp, FeelsLike, DewPoint, Uvi, WindSpeed, WindGust, Pop, Rain, Snow float64, Pressure, Humidity, Clouds, WindDeg, Visibility int, Weather []Weather}`, `models.Alert{SenderName, Event, Description string, Start, End int64, Tags []string}`; new fields `models.OneCallResponse.Minutely []Minutely`, `.Hourly []Hourly`, `.Alerts []Alert`.

- [ ] **Step 1: Write the failing test**

Add to `models/models_test.go` a test that decodes a JSON fixture containing every newly added field and asserts each binds:

```go
func TestUpstreamModelsBindAllDocumentedFields(t *testing.T) {
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
}
```

Add a second test decoding a One Call body with `timezone_offset`, one `minutely`, one `hourly` and one `alerts` entry, asserting `Pop`, `DewPoint`, `WindGust`, `MoonPhase` and `TimezoneOffset` bind. Name it `TestOneCallModelsBindDocumentedFields`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec go@1.25.7 -- go test ./models/ -run 'TestUpstreamModelsBind|TestOneCallModelsBind' -v`
Expected: FAIL to compile, unknown fields `TempKF`, `Pop`, `TimezoneOffset`, `Minutely`.

- [ ] **Step 3: Add the fields**

Add each field to its struct in `models/openweather.go` with the exact name and type from the Interfaces block, using the upstream JSON tag: `temp_kf`, `pop`, `visibility`, `population`, `dew_point`, `wind_gust`, `sunrise`, `sunset`, `moonrise`, `moonset`, `moon_phase`, `timezone_offset`, `precipitation`, `sender_name`, `start`, `end`, `tags`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `mise exec go@1.25.7 -- go test ./models/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add models/openweather.go models/models_test.go
git commit -m "feat: bind all documented upstream fields in the decode models"
```

---

### Task 2: Faithful `/weather/current` response

Introduces the first per-route response type and migrates the current route onto it. `models.WeatherData` is still in use by the other two routes and stays until Task 4.

**Files:**
- Create: `models/responses.go`
- Modify: `services/weather.go` (`convertCurrentWeatherResponse` and `GetCurrentWeather`)
- Modify: `handlers/weather.go` (`GetCurrentWeather`, `PostCurrentWeather`)
- Test: `services/services_test.go`, `handlers/weather_test.go`

**Interfaces:**
- Consumes: upstream fields from Task 1
- Produces: `models.CurrentWeatherResponse`; the block types `models.MainBlock`, `models.WindBlock`, `models.CloudsBlock`, `models.RainBlock`, `models.SnowBlock`, `models.SysBlock`; the pointer-shaped faithful payload `models.CurrentWeatherPayload`; service method `func (w *WeatherService) GetCurrentWeather(location, units, apikey string) (*models.CurrentWeatherResponse, bool, error)`; mapper `func (w *WeatherService) mapCurrentWeather(owm models.OpenWeatherMapResponse, payload models.CurrentWeatherPayload) *models.CurrentWeatherResponse`.

**Design note that governs this task:** the response block types are new, not the existing `models.Main`/`Wind`/`Rain`/`Snow`. The existing ones carry `omitempty`, which would drop a genuine measured `0` and break the "measured zero is not absence" rule. The new block types have no `omitempty` and use pointer fields.

- [ ] **Step 1: Write the failing test**

In `handlers/weather_test.go`, add `TestCurrentRouteMirrorsUpstreamAndNullsAbsentFields`. Request `/api/v1/weather/current?location=London,UK`, decode `data`, and assert:

- `data.main.temp` equals the fixture's value
- `data.main.temp_kf` is present
- `data.rain` is `nil` in the decoded `map[string]any` sense — the key exists with a `nil` value, not absent
- `data.wind.gust` is `nil` when the fixture omits `gust`
- `data.location.name` and `data.current.temperature` still carry the legacy values
- no `chance_of_rain` key exists anywhere in `data.current`

Then add `TestCurrentRouteReportsMeasuredZeroRain` with a fixture whose body contains `"rain":{"1h":0}` and assert `data.rain` decodes to a map whose `1h` key is `0.0`, not `nil`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec go@1.25.7 -- go test ./handlers/ -run 'TestCurrentRoute' -v`
Expected: FAIL, `data.main` is an absent key.

- [ ] **Step 3: Define the response type and block types**

In `models/responses.go`, define `MainBlock` with pointer fields for `temp`, `feels_like`, `temp_min`, `temp_max`, `pressure`, `humidity`, `sea_level`, `grnd_level`, `temp_kf`; `WindBlock` with `speed`, `deg`, `gust`; `CloudsBlock` with `all`; `RainBlock` and `SnowBlock` with keys `1h` and `3h`; `SysBlock` with `type`, `id`, `country`, `sunrise`, `sunset`. Reuse the existing `Coordinates` and `Weather` types, which carry no `omitempty` and are always fully populated upstream.

Define `CurrentWeatherResponse` with the faithful keys `coord`, `weather`, `base`, `main`, `visibility`, `wind`, `clouds`, `rain`, `snow`, `dt`, `sys`, `id`, `timezone`, `name`, `cod`, plus the legacy keys `location Location`, `current Current`, `request_time time.Time`.

- [ ] **Step 4: Write the mapper**

Replace `convertCurrentWeatherResponse` with `mapCurrentWeather`, returning `*CurrentWeatherResponse`. Set every faithful field from the corresponding `owm` field, and build the legacy `location` and `current` from the same local variables in the same struct literal so the two cannot disagree. Keep the existing title-casing of `description` and the `LastUpdated: time.Unix(owm.Dt, 0)` behaviour.

- [ ] **Step 5: Switch the route over**

Change `GetCurrentWeather` to return `*models.CurrentWeatherResponse` and have both handlers pass it to `utils.SendCachedSuccess` unchanged.

- [ ] **Step 6: Run tests to verify they pass**

Run: `mise exec go@1.25.7 -- go test ./... -v`
Expected: PASS, including the pre-existing `TestCurrentWeatherHandlerCachesWithinTTL`.

- [ ] **Step 7: Commit**

```bash
git add models/responses.go services/weather.go handlers/weather.go handlers/weather_test.go services/services_test.go
git commit -m "feat: mirror the current weather response onto the upstream schema"
```

---

### Task 3: Faithful `/weather/forecast` response with an honest daily `pop`

The reported bug lives here. The daily rollup is retained; `pop` becomes real, and each day carries its raw slots.

**Files:**
- Modify: `models/responses.go`
- Modify: `services/weather.go` (`convertForecastResponse`, `calculateDailyForecast`, `currentFromForecast`)
- Modify: `handlers/weather.go` (`GetWeatherForecast`, `PostWeatherForecast`)
- Test: `services/services_test.go`, `handlers/seven_day_test.go`

**Interfaces:**
- Consumes: `models.ForecastItem.Pop`, `.Visibility` from Task 1; block types from Task 2
- Produces: `models.TempPoint{Day, Min, Max, Night, Morn, Eve *float64}`, `models.FeelsLikePoint{Day, Night, Morn, Eve *float64}`, `models.ForecastSlot`, `models.ForecastDay`, `models.ForecastResponse`; service method `func (w *WeatherService) GetWeatherForecast(location, units string, days int, apikey string) (*models.ForecastResponse, bool, error)`.

**Key collisions to resolve in this task.** The day object carries both vocabularies flat, and three JSON keys are wanted by both: `date`, `humidity`, `wind_speed`. A single field serves each pair. `Humidity` is typed `*int` and holds the rounded daily mean, which is what the legacy `Forecast.Humidity` already was, so the JSON is byte-identical to today's. `WindSpeed` is `*float64` holding the daily mean, identical to the legacy value. `Date` is `time.Time`, shared unchanged. Do not add a second field for any of these; two fields cannot share one JSON tag.

- [ ] **Step 1: Write the failing tests**

Add to `services/services_test.go`:

- `TestDailyPopIsMaxAcrossSlots` — a fixture with four slots in one day carrying `pop` of `0.1`, `0.8`, `0.3`, `0.2`. Assert day `Pop` is `0.8`, `PopMin` is `0.1`, `PopMean` is `0.35`.
- `TestDailyPopIgnoresSlotsWithoutPop` — four slots where only two carry `pop` (`0.5` and `0.9`). Assert `Pop` is `0.9` and `PopMean` is `0.7`, computed over the two slots that have a value rather than over all four.
- `TestLegacyChanceOfRainMatchesDailyPop` — assert the day object's `chance_of_rain` equals `int(Pop*100)` for a fixture with `pop` `0.42`.
- `TestForecastDayHasNoTimeOfDayBreakdown` — assert `temp.day`, `temp.night`, `temp.morn`, `temp.eve` and every `feels_like` member are `nil`, while `temp.min` and `temp.max` hold the day's extremes computed across the slots.
- `TestForecastDayGroupsSlotsAcrossUtcMidnight` — a fixture whose slots span `23:00` on one UTC date and `01:00` on the next. Assert two day objects are produced and the boundary is the UTC date, documenting the behaviour the current `Format("2006-01-02")` grouping already has.

Add to `handlers/seven_day_test.go`:

- `TestForecastRouteExposesRawSlotsWithPop` — assert each day carries an `hourly` array whose entries have `pop`, `visibility`, `dt_txt` and `rain["3h"]` populated from the fixture.
- `TestForecastRouteReportsMeasuredZeroRain` — a fixture with `"rain":{"3h":0}` on a slot; assert the `hourly` entry's `rain["3h"]` is `0`, not `null` and not absent.

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec go@1.25.7 -- go test ./services/ ./handlers/ -run 'Pop|TimeOfDay|UtcMidnight|RawSlots|MeasuredZeroRain' -v`
Expected: FAIL, `chance_of_rain` is `0` and no `hourly` key exists.

- [ ] **Step 3: Define the types**

Add to `models/responses.go`: `TempPoint` and `FeelsLikePoint` with pointer fields; `ForecastSlot` mirroring a raw three-hour slot with `Dt int64`, `Main *MainBlock`, `Weather []Weather`, `Clouds *CloudsBlock`, `Wind *WindBlock`, `Visibility *int`, `Pop *float64`, `Rain *RainBlock`, `Snow *SnowBlock`, `Sys *ForecastSys`, `DtTxt string`; `ForecastDay`; and `ForecastResponse`.

`ForecastDay` holds the legacy flat keys `date`, `max_temperature`, `min_temperature`, `avg_temperature`, `condition`, `description`, `icon`, `uv_index`, `precipitation`, `chance_of_rain`, plus faithful `pop`, `pop_min`, `pop_mean`, `temp`, `feels_like`, `wind_deg`, `wind_gust`, `clouds`, `visibility`, `rain`, `snow`, `uvi`, `weather`, `hourly`, plus the three shared keys `humidity` and `wind_speed` described above.

`ForecastResponse` holds faithful `cod`, `message`, `cnt`, `city` and legacy `location`, `current`, `request_time`.

- [ ] **Step 4: Write the mapper**

Replace `convertForecastResponse` and `calculateDailyForecast` with a single per-route mapper. Keep the existing date grouping and the `sort.Strings` on the date keys. Inside each day:

- `Pop` is the max over slots that have a `Pop`; `PopMin` the min over those; `PopMean` the arithmetic mean of those fractional values, not of their percentages. Slots with no `Pop` are excluded from all three.
- `temp.min` and `temp.max` are the extremes of `item.Main.Temp` across the day's slots. This is a derivation, not an upstream value; comment it as such. Leave `temp.day`, `temp.night`, `temp.morn`, `temp.eve` nil.
- `humidity` is the rounded mean of `item.Main.Humidity`; `wind_speed` the mean of `item.Wind.Speed`; `clouds` and `visibility` means; `wind_gust` the max.
- `wind_deg` is the `item.Wind.Deg` of the slot with the day's highest `item.Wind.Speed`, matching the daily endpoints' documented meaning of `deg`.
- `rain` and `snow` are the sums of the slots' `3h` values, `nil` when no slot reports any.
- `weather` comes from the middle slot, as today.
- `hourly` is every slot of that day, mapped straight through.

Keep `currentFromForecast` producing the legacy `current` block from `list[0]`, unchanged, including its existing comment explaining that the block is approximate.

- [ ] **Step 5: Switch the route over**

Change `GetWeatherForecast` to return `*models.ForecastResponse` and have both handlers pass it through.

- [ ] **Step 6: Run tests to verify they pass**

Run: `mise exec go@1.25.7 -- go test ./... -v`
Expected: PASS, including `TestForecastHandlerDoesNotCallCurrentEndpoint` and `TestForecastHandlerCachesPerDayCount`.

- [ ] **Step 7: Commit**

```bash
git add models/responses.go services/weather.go services/services_test.go handlers/weather.go handlers/seven_day_test.go
git commit -m "fix: report real precipitation probability on the forecast route"
```

---

### Task 4: Faithful `/weather/forecast/7day` response, and retire `WeatherData`

**Files:**
- Modify: `models/responses.go`
- Modify: `models/weather.go` (remove `WeatherData`)
- Modify: `services/weather.go` (`convertOneCallResponse`, `currentFromOneCall`, `fetchSevenDayForecast`)
- Modify: `handlers/weather.go` (`GetSevenDayForecast`, `PostSevenDayForecast`)
- Test: `services/seven_day_test.go`, `handlers/seven_day_test.go`

**Interfaces:**
- Consumes: One Call models from Task 1; block types from Task 2
- Produces: `models.OneCallCurrentPoint`, `models.OneCallDailyPoint`, `models.OneCallEnvelope`, `models.SevenDayResponse`; service method `func (w *WeatherService) GetSevenDayForecast(location, units, apikey string) (*models.SevenDayResponse, bool, error)`. `models.WeatherData` ceases to exist.

**Spec amendment, resolved during plan self-review.** The spec placed the faithful
One Call keys at the top level. That collides: the legacy `current` block and the
faithful One Call `current` object both want the JSON key `current`, and two Go
fields cannot share one tag. The faithful One Call data is therefore namespaced
under a single `onecall` key, which removes the collision entirely and leaves the
legacy `location`, `current`, `forecast` and `request_time` keys byte-identical to
today. The three opt-in blocks live at `onecall.minutely`, `onecall.hourly` and
`onecall.alerts`. Task 7 documents this.

- [ ] **Step 1: Write the failing tests**

Add to `handlers/seven_day_test.go`:

- `TestSevenDayRouteExposesFullDailyBreakdown` — assert `data.onecall.daily[0].temp` has non-nil `day`, `min`, `max`, `night`, `morn`, `eve`, and that `data.onecall.daily[0].feels_like` has all four members non-nil.
- `TestSevenDayRouteExposesDewPointAndWindGust` — assert `data.onecall.daily[0].dew_point` and `data.onecall.daily[0].wind_gust` are present and equal the fixture values.
- `TestSevenDayDailyMayExceedLegacyForecastLength` — with the existing 8-entry fixture, assert `len(data.onecall.daily) == 8` while `len(data.forecast) == 7`.
- `TestSevenDayRouteAlwaysFetchesEveryBlock` — assert the upstream query recorded by the stub contains no `exclude` parameter.

Add to `services/seven_day_test.go`:

- `TestSevenDayMapperKeepsEveryBlock` — assert the returned response's `OneCall` is non-nil and carries the fixture's `minutely`, `hourly` and `alerts` entries.
- `TestGetSevenDayForecastNoLongerSendsExclude` — assert the forwarded query has no `exclude` key.

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec go@1.25.7 -- go test ./services/ ./handlers/ -run 'SevenDay' -v`
Expected: FAIL, no `daily` key in the response and `exclude` still present upstream.

- [ ] **Step 3: Define the types**

Add `OneCallCurrentPoint` and `OneCallDailyPoint` with pointer fields for every documented member of their One Call blocks, including `sunrise`, `sunset`, `moonrise`, `moonset`, `moon_phase` and the nested `temp`/`feels_like` pointers.

Add `OneCallEnvelope` with `Lat float64`, `Lon float64`, `Timezone string`, `TimezoneOffset int`, `Current *OneCallCurrentPoint`, `Daily []OneCallDailyPoint`, and the three opt-in members `Minutely []Minutely`, `Hourly []Hourly`, `Alerts []Alert`, the last three carrying `omitempty` so an unrequested block is absent rather than `null`.

Add `SevenDayResponse` with `OneCall *OneCallEnvelope json:"onecall"` plus the unchanged legacy `Location Location`, `Current Current`, `Forecast []Forecast`, `RequestTime time.Time`.

`OneCallEnvelope` is always present, including when no blocks are requested, so a caller who omits `blocks` still gets faithful `lat`, `lon`, `current` and `daily`.

- [ ] **Step 4: Write the mapper**

Replace `convertOneCallResponse` with a mapper producing `*SevenDayResponse`. Populate `OneCall.Daily` from every entry upstream returns, with no cap. Populate the legacy `Forecast` from the first seven entries only, preserving today's values including `ChanceOfRain` as `int(day.Pop*100)`. Populate `OneCall.Current` from the upstream `current` block and `OneCall.Minutely`, `.Hourly`, `.Alerts` from their upstream counterparts. Keep `currentFromOneCall` populating the legacy `current` block unchanged.

Build the legacy `Current` and the faithful `OneCall.Current` in the same mapper from the same upstream value so the two cannot disagree.

Delete the `OneCallExcludes` constant and stop adding the `exclude` parameter in `fetchSevenDayForecast`.

- [ ] **Step 5: Remove `WeatherData`**

Delete the `WeatherData` type from `models/weather.go`. Leave `Current`, `Forecast` and `WeatherRequest` in place.

- [ ] **Step 6: Switch the route over and verify the build**

Change `GetSevenDayForecast` to return `*models.SevenDayResponse`, update both handlers, then run:

`mise exec go@1.25.7 -- go build ./... && mise exec go@1.25.7 -- go vet ./...`
Expected: both clean, no remaining reference to `WeatherData`.

- [ ] **Step 7: Run tests to verify they pass**

Run: `mise exec go@1.25.7 -- go test ./... -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add models/responses.go models/weather.go services/weather.go services/seven_day_test.go handlers/weather.go handlers/seven_day_test.go
git commit -m "feat: mirror the seven day response and fetch every one call block"
```

---

### Task 5: Opt-in block exposure without mutating the cache

**Files:**
- Modify: `models/responses.go`
- Modify: `handlers/weather.go`
- Modify: `models/weather.go` (`WeatherRequest`)
- Test: `handlers/seven_day_test.go`

**Interfaces:**
- Consumes: `models.OneCallEnvelope` and its three opt-in members from Task 4
- Produces: `models.Blocks{Hourly, Minutely, Alerts bool}`; `models.ParseBlocks(raw string) (Blocks, error)`; `func (r *SevenDayResponse) Project(b models.Blocks) models.SevenDayResponse`; `WeatherRequest.Blocks string`

`ParseBlocks` splits on commas, trims space, lowercases, accepts only `hourly`, `minutely` and `alerts`, and returns an error naming the offending token. An empty or absent value returns the zero `Blocks` and no error.

`Project` returns a value copy. It never writes to the receiver. Because `OneCallEnvelope` is always present, `Project` clears the three unrequested members inside a copy of it rather than removing a whole subtree, so `lat`, `lon`, `current` and `daily` survive a lean request.

- [ ] **Step 1: Write the failing tests**

Add to `handlers/seven_day_test.go`:

- `TestSevenDayRouteOmitsUnrequestedBlocks` — no `blocks` param; assert `data.onecall` has no `hourly`, `minutely` or `alerts` key, while `data.onecall.daily` is still present.
- `TestSevenDayRouteIncludesRequestedBlocks` — `?blocks=hourly`; assert `data.onecall.hourly` is a non-empty array and `data.onecall.minutely` and `data.onecall.alerts` are absent.
- `TestSevenDayRouteRejectsUnknownBlock` — `?blocks=bogus`; assert `400`.
- `TestSevenDayRouteAcceptsMultipleBlocks` — `?blocks=hourly,alerts`; assert both present.
- `TestSevenDayRouteBlocksDoNotCostAnUpstreamCall` — issue a lean request then a `?blocks=hourly` request for the same location and key; assert the stub counted exactly one One Call call and that the second response carried `X-Cache: HIT`.
- `TestConcurrentBlockRequestsDoNotCorruptEachOther` — fire a lean and a fat request concurrently with `sync.WaitGroup`, collecting both bodies and any errors under a mutex; assert the fat body's `onecall.hourly` is present. This pins the aliasing invariant: both requests share one cached pointer.
- `TestPostSevenDayForecastAcceptsBlocksField` — POST body `{"location":"London,UK","units":"metric","blocks":"hourly"}`; assert `onecall.hourly` present.
- `TestPostSevenDayForecastRejectsUnknownBlockField` — POST with `"blocks":"bogus"`; assert `400`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec go@1.25.7 -- go test ./handlers/ -run 'Blocks|Block' -v`
Expected: FAIL, `onecall.hourly` is absent and `blocks=bogus` is currently ignored.

- [ ] **Step 3: Implement `Blocks` and `ParseBlocks`**

In `models/responses.go`. The allowlist is a fixed set; a token outside it is an error rather than a silent no-op, so a typo cannot masquerade as a working request.

- [ ] **Step 4: Implement `Project`**

Per the spec's code block, adapted to the namespace: copy the receiver by value, copy `OneCall` by value, then set the three unrequested members on that copy to `nil`. Never assign through the receiver's fields. Keep the doc comment stating that the cached response is shared by every concurrent caller.

- [ ] **Step 5: Wire the handlers**

In both seven-day handlers, resolve the raw value from `c.Query("blocks")` for GET and from `req.Blocks` for POST, call `models.ParseBlocks`, return `utils.NewAPIError(http.StatusBadRequest, ...)` on error, then send `resp.Project(blocks)` to `utils.SendCachedSuccess`. Add `Blocks string \`json:"blocks,omitempty" form:"blocks"\`` to `WeatherRequest`.

Do not touch `weatherCacheKey`. The cached value is always complete, so one entry serves every variant.

- [ ] **Step 6: Run tests to verify they pass**

Run: `mise exec go@1.25.7 -- go test ./... -v`
Expected: PASS, with the cache tests still green.

- [ ] **Step 7: Commit**

```bash
git add models/responses.go models/weather.go handlers/weather.go handlers/seven_day_test.go
git commit -m "feat: expose one call blocks on request without mutating the cache"
```

---

### Task 6: Pin every legacy alias to its faithful twin

The structural guard against the class of bug that produced `chance_of_rain: 0`: a field that is quietly wrong rather than absent.

**Files:**
- Test: `handlers/weather_test.go` (new file `handlers/alias_test.go` is also acceptable)

**Interfaces:**
- Consumes: all three response types from Tasks 2–4
- Produces: no production code

- [ ] **Step 1: Write the test**

Add a table-driven test walking all three routes. For each, assert these pairs agree:

| Route | Legacy key | Faithful key |
|---|---|---|
| `/current` | `data.current.temperature` | `data.main.temp` |
| `/current` | `data.current.feels_like` | `data.main.feels_like` |
| `/current` | `data.current.humidity` | `data.main.humidity` |
| `/current` | `data.current.pressure` | `data.main.pressure` |
| `/current` | `data.current.wind_speed` | `data.wind.speed` |
| `/current` | `data.current.wind_direction` | `data.wind.deg` |
| `/current` | `data.current.cloud_cover` | `data.clouds.all` |
| `/forecast` | `data.forecast[0].chance_of_rain` | `int(data.forecast[0].pop*100)` |
| `/forecast` | `data.forecast[0].max_temperature` | `data.forecast[0].temp.max` |
| `/forecast` | `data.forecast[0].min_temperature` | `data.forecast[0].temp.min` |
| `/forecast` | `data.forecast[0].precipitation` | `data.forecast[0].rain + data.forecast[0].snow` |
| `/forecast/7day` | `data.forecast[0].chance_of_rain` | `int(data.onecall.daily[0].pop*100)` |
| `/forecast/7day` | `data.forecast[0].max_temperature` | `data.onecall.daily[0].temp.max` |
| `/forecast/7day` | `data.current.temperature` | `data.onecall.current.temp` |

The 7-day rows read from the `onecall` namespace, which is where Task 4 places the faithful One Call data so it does not collide with the legacy `current` key.

- [ ] **Step 2: Run the test to verify it passes**

Run: `mise exec go@1.25.7 -- go test ./handlers/ -run 'Alias' -v`
Expected: PASS. This test should pass on first run, because Tasks 2–4 assign both keys from the same local variables. If any row fails, the mapper has drifted and the fix belongs in the mapper, not in the test.

- [ ] **Step 3: Commit**

```bash
git add handlers/alias_test.go
git commit -m "test: pin every legacy alias to its faithful twin"
```

---

### Task 7: Document the representation rules

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the shipped response shapes
- Produces: no code

- [ ] **Step 1: Write the documentation**

Add a response-schema section covering, in the spec's own terms:

- the three representation rules: faithful keys mirror one endpoint's documented schema; legacy keys are always present and `null` when the route cannot supply them; an omitted opt-in block is not the same as a `null` field
- which daily values on `/forecast` are derived rather than upstream: `temp.min`, `temp.max`, `humidity`, `wind_speed`, `clouds`, `visibility`, `wind_gust`, `rain`, `snow`, and `pop`/`pop_min`/`pop_mean`
- that a slot's `main.temp_min`/`temp_max` are city-moment measurements while the day-level `temp.min`/`max` are the day's extremes, and that the two are not interchangeable
- the `?blocks=hourly,minutely,alerts` parameter, that an unrecognised value returns `400`, and that on the 7-day route the faithful One Call data lives under a single `onecall` key whose `minutely`, `hourly` and `alerts` members appear only when requested
- that faithful `daily[]` may hold more entries than the legacy 7-entry `forecast[]`
- that `/weather/current` emits no `chance_of_rain` key in either vocabulary, because the endpoint has no `pop`
- that the `current` block on the forecast routes is derived from the forecast payload, not observed
- that the cache is per-process, so N Cloud Run instances mean up to N times the upstream calls, and that a cached response now retains the `minutely`, `hourly` and `alerts` blocks

- [ ] **Step 2: Verify the documented shapes against the code**

Run: `mise exec go@1.25.7 -- go test ./... -v`
Expected: PASS, confirming the README describes what the code actually emits.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document the weather response representation rules"
```
