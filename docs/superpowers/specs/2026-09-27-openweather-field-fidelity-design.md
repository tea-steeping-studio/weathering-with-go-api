# OpenWeatherMap Field Fidelity Design

Date: 2026-09-27
Status: draft for review

## Problem

`GET /api/v1/weather/forecast` reports `chance_of_rain: 0` for every day, always.
The cause is two independent defects:

1. `models.ForecastItem` has no `Pop` field, so `encoding/json` discards `list.pop`
   from every three-hour slot in the `/data/2.5/forecast` response. The field is
   documented by OpenWeatherMap; the model drops it.
2. `calculateDailyForecast` in `services/weather.go` builds a `models.Forecast`
   with `Precipitation` set but `ChanceOfRain` omitted, so the struct literal
   leaves it at Go's zero value.

Either defect alone is sufficient to produce a permanent `0`.

The same class of loss affects other documented fields. `Main.temp_kf`,
`City.population`, `ForecastItem.visibility`, `DailyForecast.dew_point` and
`DailyForecast.wind_gust` are all documented upstream and all currently dropped.
The `/data/3.0/onecall` request sends `exclude=minutely,hourly,alerts`, discarding
three whole upstream blocks.

A related semantic error: `models.Current` exposes `max_temperature` and
`min_temperature`, populated from `main.temp_max` and `main.temp_min`. On the
`/data/2.5` endpoints those are documented as the minimum and maximum temperature
in the city *at the moment of calculation*, not the day's extremes. The labels are
wrong.

## Goals

- Map every documented OpenWeatherMap field, on every route, without inventing values.
- Report precipitation probability accurately on every route that has an upstream
  source for it, and represent its absence honestly where it does not.
- Preserve the existing response keys so current consumers keep working.
- Do not increase upstream call volume.

## Non-goals

- Changing which OpenWeatherMap endpoints back which route.
- Adding a second upstream call to the forecast routes to obtain real current
  conditions. `TestForecastHandlerDoesNotCallCurrentEndpoint` continues to guard this.
- Client-side aggregation of the three-hour slots into daily values by callers.

## Vocabulary and representation rules

These three rules govern every field in the response. They are the rules a consumer
cannot infer from the JSON itself, and all three go in the README.

1. **Faithful keys** are exactly the fields documented for *that route's* upstream
   endpoint. Nothing more, nothing less. A field absent from that endpoint's schema
   gets no faithful key.
2. **Legacy keys** are the existing public vocabulary. They are always present. A
   legacy key the route cannot supply serialises as `null`.
3. **Omitted is not the same as null.** A field documented for the route that the
   upstream has no value for is `null`. An opt-in block the caller did not request
   is omitted entirely. Omitting a block the caller asked for is not permitted.

Pointers represent the difference between "measured zero" and "not measured". A
measured `0` serialises as `0`; a gap serialises as `null`. This is what makes the
original bug detectable rather than silent.

## What the upstream endpoints actually provide

| Field | `/data/2.5/weather` | `/data/2.5/forecast` | `/data/3.0/onecall` |
|---|---|---|---|
| `pop` | absent | per three-hour slot | `daily[]` only |
| `temp.day/night/morn/eve` | absent | absent | `daily[]` only |
| `temp.min/max` | `temp_min`/`temp_max`, city-moment | `temp_min`/`temp_max`, city-moment | true daily extremes |
| `feels_like` breakdown | absent | absent | `daily[]` |
| `rain`/`snow` | `1h` | `3h` | daily volume |
| `uvi`, `dew_point` | absent | absent | `current` and `daily[]` |

`chance_of_rain` therefore has no upstream source on `/weather/current`, and no
`chance_of_rain` key is emitted there in either vocabulary. This is not an
exception to rule 2: rule 2 governs legacy keys that already exist, and the legacy
`Current` struct has never had a `chance_of_rain` field. Adding one that is
permanently `null` would be a new key carrying no information. The `/forecast` and
`/forecast/7day` routes both have real upstream sources and both report accurately.

## Architecture

One response type per route. Each type carries its faithful block and its legacy
aliases in the same struct, populated by one mapper function that builds the whole
value in a single struct literal, so an alias and its faithful twin are assigned
from the same local variable and cannot drift.

A test walks each response and asserts every legacy alias equals its faithful twin.
This is the structural guard against the class of bug that produced
`chance_of_rain: 0`.

The alternative considered was a faithful struct plus a `MarshalJSON` that derives
legacy keys at serialisation time. That makes drift structurally impossible but
leaves the JSON shape described by no type, which is a poor trade for a project
whose reported defect was a quietly wrong field.

## Response shapes

### `GET|POST /api/v1/weather/current`

Faithful block, a pointer-field mirror of `/data/2.5/weather`:
`coord`, `weather[]`, `base`, `main{temp, feels_like, temp_min, temp_max, pressure,
humidity, sea_level, grnd_level, temp_kf}`, `visibility`, `wind{speed, deg, gust}`,
`clouds{all}`, `rain{1h}`, `snow{1h}`, `dt`, `sys{type, id, country, sunrise,
sunset}`, `id`, `timezone`, `name`, `cod`.

`rain`, `snow`, `wind.gust`, `main.sea_level`, `main.grnd_level` and
`main.temp_kf` are `null` when the upstream omits them.

Legacy block, unchanged: `location`, `current`, `request_time`.

### `GET|POST /api/v1/weather/forecast`

Daily rollup is retained. Per day:

- `date`
- `pop` (max across the day's slots), `pop_min`, `pop_mean` (arithmetic mean of the
  slots' fractional `pop` values, not of their percentages)
- `temp{min, max}` — the true daily extremes, **derived** by scanning the day's
  slots. `temp.day`, `temp.night`, `temp.morn`, `temp.eve` are `null`: the
  three-hour endpoint has no time-of-day breakdown.
- `feels_like{day, night, morn, eve}` — all `null` on this route
- `humidity`, `pressure`, `wind_speed`, `clouds`, `visibility` — means
- `wind_deg` — the `wind.deg` of the slot with that day's highest `wind.speed`,
  matching the documented semantic of `deg` on the daily endpoints, which is the
  direction relevant to the maximum wind speed
- `wind_gust` — max
- `rain`, `snow` — sums of `3h`
- `uvi` — `null`
- `weather[]` — from the middle slot, as today
- `hourly[]` — the day's slots, raw and faithful: `dt`, `main{...}`, `weather[]`,
  `clouds`, `wind`, `visibility`, `pop`, `rain{3h}`, `snow{3h}`, `sys{pod}`,
  `dt_txt`

Faithful envelope: `cod`, `message`, `cnt`, `city{...}` with `population` added.

Legacy block: `location`, `current` (still derived from `list[0]`), and per day `date`,
`max_temperature`, `min_temperature`, `avg_temperature`, `condition`, `description`,
`icon`, `humidity`, `wind_speed`, `precipitation`, `chance_of_rain`, `uv_index`.

**Four legacy keys change value, not shape.** This is a breaking change for existing
consumers and is deliberate:

- `max_temperature` and `min_temperature` previously carried the upstream `temp_max` and
  `temp_min`, which on the `/data/2.5` endpoints are documented as the extremes *in the
  city at the moment of calculation*, not the day's extremes. Labelling them
  `max_temperature`/`min_temperature` was a mislabel. They now carry the same derived
  day-level extremes as `temp.max` and `temp.min`, which is what the names claim.
- `chance_of_rain` was a permanent `0`; it is now the rounded daily probability.
- `uv_index` was a fabricated `0`; the three-hour endpoint has no `uvi`, so it is now
  `null`.
- On `/weather/current` and `/weather/forecast/7day`, `current.max_temperature` and
  `current.min_temperature` move from `0` to `null`. Neither was ever a real reading:
  the upstream `temp_min`/`temp_max` on the `/data/2.5` endpoints are documented as
  extremes in the city at the moment of calculation, not daily extremes, so labelling
  them `min_temperature`/`max_temperature` was a mislabel. `null` says the route does not
  report a daily extreme there. `/weather/forecast` is unaffected, because that route
  derives real day-level extremes.
- The legacy `date` field renders in UTC rather than the server's local zone, so the day
  component of the rendered string no longer shifts with the server's `TZ`.

`humidity` deliberately keeps its truncating mean, because the legacy field is an `int`
and truncating is what it has always done. `pressure`, `clouds` and `visibility` have no
legacy counterpart and are rounded. `chance_of_rain` is rounded. This mixed policy is
stated on the `ForecastDay` type and in the README so it does not read as an oversight.

Within `hourly[]`, each slot's `main.temp_min` and `main.temp_max` stay faithful to
the upstream city-moment measurement. At the day level, `temp.min` and `temp.max`
are our own derivation of the day's extremes. Both are correct and they measure
different things; the README states which is which.

### `GET|POST /api/v1/weather/forecast/7day`

The `exclude` parameter is dropped, so `minutely`, `hourly` and `alerts` are all
fetched. This adds no upstream calls, only cached payload.

Faithful block, namespaced under a single `onecall` key so it cannot collide with
the legacy `current` key: `onecall{lat, lon, timezone, timezone_offset,
current{dt, sunrise, sunset, temp, feels_like, pressure, humidity, dew_point,
uvi, clouds, visibility, wind_speed, wind_deg, wind_gust, weather[]},
daily[]{dt, sunrise, sunset, moonrise, moonset, moon_phase, temp{day, min, max,
night, morn, eve}, feels_like{day, night, morn, eve}, pressure, humidity,
dew_point, wind_speed, wind_deg, wind_gust, clouds, pop, rain, snow, uvi,
weather[]}}`.

The namespace is an amendment to the original design, which placed these keys at the
top level. The legacy `current` block and the faithful One Call `current` object
both want the JSON key `current`, and two Go fields cannot share one tag. Grouping
the faithful data under `onecall` removes the collision and leaves every legacy key
byte-identical to its current form.

`onecall` is always present, so a caller who requests no blocks still receives
`lat`, `lon`, `timezone`, `current` and `daily`. The three opt-in members
`onecall{minutely[], hourly[], alerts[]}` appear only when requested.

Legacy block, unchanged: `location`, `current`, `forecast[]` capped at 7 days,
`request_time`. Faithful `daily[]` returns whatever upstream sends, which may
exceed 7 entries; the README notes the difference.

## Opt-in blocks

Query parameter `?blocks=hourly,minutely,alerts` on the 7-day route, and a
`blocks` field on the POST body. An explicit allowlist rather than a boolean, so an
unrecognised value is a `400` instead of a silently ignored typo.

The parameter controls *exposure only*. The upstream request always asks for
everything, and the mapper always produces every block.

### Cache interaction

The cache key does not change.

`cachedFetchTracked` stores a pointer and hands the same pointer to every caller for
the life of the entry. Mutating the cached response to honour a lean request would
corrupt it for a concurrent request that asked for more. Two concurrent London
requests, one with `?blocks=hourly` and one without, share one pointer; stripping
`hourly` for the lean one breaks the fat one. Silent and intermittent.

Projection therefore copies the value and clears one pointer:

```go
// Project returns a copy with the opt-in blocks the caller did not ask for
// removed. The cached response is shared by every concurrent caller, so this
// must never mutate the receiver.
func (r *SevenDayResponse) Project(blocks Blocks) SevenDayResponse {
    out := *r
    if out.OneCall == nil {
        return out
    }
    onecall := *out.OneCall
    if !blocks.Hourly {
        onecall.Hourly = nil
    }
    if !blocks.Minutely {
        onecall.Minutely = nil
    }
    if !blocks.Alerts {
        onecall.Alerts = nil
    }
    out.OneCall = &onecall
    return out
}
```

The top-level copy is a few words. Nested slices are shared but never written.

Because the cached value is always complete, one upstream fetch serves every
variant. Putting `blocks` in the cache key was rejected: it would create a second
entry and a second upstream call, spending free-tier quota to save a struct copy.
Caching raw bytes and mapping per request was also rejected: it is the most robust
option against aliasing, but it reworks the generic `cachedFetchTracked` pattern
the service is built on and pays a decode per request, which is the wrong trade
while upstream quota is the scarce resource.

### Memory

A full One Call response including `hourly` and `minutely` is tens of kilobytes of
structs. At `cacheMaxEntries` of 500 that is roughly 15–30 MB per instance, before
the per-instance multiplier on Cloud Run. Acceptable at current scale, and recorded
here because it is a real change to the cache's footprint.

## File changes

**models/openweather.go** — additions only; it is already a faithful mirror and
the loss is not in parsing.
- `Main` gains `temp_kf`
- `ForecastItem` gains `pop`, `visibility`
- `City` gains `population`
- `DailyForecast` gains `dew_point`, `wind_gust`
- `OneCallResponse` gains `timezone_offset`, and new `Minutely`, `Hourly`, `Alert`
  types plus their slices
- `OneCallCurrent` and `DailyForecast` gain the opt-in block fields

**models/responses.go** — new file. `CurrentWeatherResponse`, `ForecastResponse`,
`SevenDayResponse`, `OneCallEnvelope`, `Blocks`, and the pointer-field block types
`MainBlock`, `WindBlock`, `CloudsBlock`, `RainBlock`, `SnowBlock`, `SysBlock`,
`TempPoint`, `FeelsLikePoint`. Reuses `Coordinates` and `Weather` from the upstream
models; the block types are new rather than the existing `Main`, `Wind`, `Rain` and
`Snow`, because those carry `omitempty` and would drop a genuine measured zero.

**models/weather.go** — `WeatherData` is removed. `Current` and `Forecast` survive
unchanged as the legacy alias blocks. Removing an exported type is a breaking change
for any importer; nothing in this repository imports it, and the alternative,
keeping it as a deprecated alias, would leave two competing envelope types with only
one of them maintained.

**services/weather.go** — the three converters are replaced by three per-route
mappers. `calculateDailyForecast` and `currentFromForecast` fold into the forecast
mapper. The pop rollup and the temperature derivation live here, each carrying a
comment that names it as derived. `OneCallExcludes` is removed.

**handlers/weather.go** — parse and validate `blocks` against the allowlist,
returning `400` on an unrecognised value, then call `Project`.

**models/weather.go** (second change) — `WeatherRequest` gains a `Blocks string`
field so the 7-day POST route can carry the same opt-in as the GET route.

**README.md** — document the three representation rules, which daily values are
derived, the per-slot versus day-level temperature distinction, the `blocks`
parameter, and that faithful `daily[]` may exceed the legacy 7-day cap.

## Testing

Test-first, using the existing stub `RoundTripper` so no test touches the network.

- `pop` on three-hour slots yields daily `pop` as the max, correct `pop_min` and
  `pop_mean`, and legacy `chance_of_rain` equal to daily `pop`
- a route with no upstream `pop` yields no `chance_of_rain` key on `/current`, and
  a genuinely absent value elsewhere yields `null` and never `0`. This is the
  regression test for the reported bug.
- absent `rain`/`snow`/`gust` serialise as `null`; a present `rain: {"3h": 0}`
  serialises as `0`
- `temp.day/night/morn/eve` are `null` on `/forecast` and populated on
  `/forecast/7day`
- alias consistency: a table walk asserting every legacy key equals its faithful
  twin on all three routes
- concurrency: a lean and a fat `?blocks=` request in parallel, asserting the fat
  response retains `hourly`
- `blocks=bogus` returns `400`
- the existing suite stays green, including
  `TestForecastHandlerDoesNotCallCurrentEndpoint` and the cache TTL tests, which
  depend on `WeatherService.now` remaining a func field

## Open items

None. All design decisions were resolved during brainstorming.
