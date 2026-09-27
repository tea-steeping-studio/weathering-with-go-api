# Weathering with Go 🌤️

A modern, high-performance weather API built with Go and the Gin web framework. Get current weather conditions and forecasts for any location worldwide using the OpenWeatherMap API.

## ✨ Features

- **Current Weather**: Get real-time weather data for any location
- **Weather Forecasts**: 5-day weather forecasts with 3-hour intervals, plus a 7-day daily forecast
- **Response Caching**: 15-minute in-memory cache to protect your OpenWeatherMap quota
- **Multiple Units**: Support for metric, imperial, and Kelvin units
- **RESTful API**: Clean, well-documented REST endpoints
- **Error Handling**: Comprehensive error handling with detailed responses
- **CORS Support**: Cross-origin resource sharing enabled
- **Health Checks**: Monitor API health and status
- **Middleware**: Security headers, logging, and request tracking

## 🚀 Quick Start

### Prerequisites

- Go 1.25.1 or higher (the `go` directive in `go.mod` rejects 1.25.0)
- OpenWeatherMap API key (free at [openweathermap.org](https://openweathermap.org/api))

### Installation

1. **Clone the repository**
   ```bash
   git clone https://github.com/tea-LZL/weathering-with-go.git
   cd weathering-with-go
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Set environment variables**
   ```bash
   export OPENWEATHERMAP_API_KEY="your_api_key_here"
   export PORT="8080"  # Optional, defaults to 8080
   ```

4. **Run the server**
   ```bash
   go run main.go
   ```

The API will be available at `http://localhost:8080`

## 📖 API Documentation

### Base URL
```
http://localhost:8080/api/v1
```

### Authentication
No authentication required. The OpenWeatherMap API key is configured server-side, and a request may
override it (see [API key per request](#api-key-per-request)).

### Endpoints

#### GET /health
Health check endpoint to verify the API is running. Also served at `/api/v1/health`.

**Response:**
```json
{
  "status": "healthy",
  "service": "weathering-with-go",
  "timestamp": {
    "unix": {}
  }
}
```

#### GET /weather/current
Get current weather for a location.

**Parameters:**
- `location` (required): City name, state code, and country code (e.g., "London,UK" or "New York,NY,US")
- `units` (optional): Temperature units - `metric` (default), `imperial`, or `kelvin`

**Example:**
```bash
curl "http://localhost:8080/api/v1/weather/current?location=London,UK&units=metric"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "location": {
      "name": "London",
      "country": "GB",
      "latitude": 51.5074,
      "longitude": -0.1278
    },
    "current": {
      "temperature": 15.5,
      "feels_like": 14.8,
      "humidity": 72,
      "pressure": 1013.2,
      "visibility": 10000,
      "wind_speed": 3.6,
      "wind_direction": 230,
      "condition": "Clouds",
      "description": "Scattered Clouds",
      "icon": "03d",
      "max_temperature": null,
      "min_temperature": null,
      "cloud_cover": 40,
      "last_updated": "2025-09-21T10:30:00Z"
    },
    "request_time": "2025-09-21T10:30:15Z"
  }
}
```

Abridged: `data` also carries a faithful mirror of the OpenWeatherMap `/data/2.5/weather` schema
alongside the keys above — `coord`, `weather[]`, `base`, `main{}`, `visibility`, `wind{}`, `clouds{}`,
`rain{}`, `snow{}`, `dt`, `sys{}`, `id`, `timezone`, `name`, `cod`. See
[Response schema](#response-schema).

`current.wind_gust` is absent from the example because OpenWeatherMap sent no gust, and it is absent
from the response for the same reason. `current.max_temperature` and `current.min_temperature` are
`null` because this endpoint reports no daily extremes and the route has never measured any.

#### POST /weather/current
Get current weather using JSON request body.

**Request Body:**
```json
{
  "location": "London,UK",
  "units": "metric"
}
```

**Response:** Same as GET endpoint

#### GET /weather/forecast
Get weather forecast for a location.

**Parameters:**
- `location` (required): City name, state code, and country code
- `units` (optional): Temperature units - `metric` (default), `imperial`, or `kelvin`
- `days` (optional): Number of forecast days (1-5, default: 5)

**Example:**
```bash
curl "http://localhost:8080/api/v1/weather/forecast?location=Tokyo,JP&units=metric&days=3"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "cod": "200",
    "message": 0.0117,
    "cnt": 40,
    "city": {
      "id": 1850147,
      "name": "Tokyo",
      "coord": { "lon": 139.6917, "lat": 35.6895 },
      "country": "JP",
      "population": 8336599,
      "timezone": 32400,
      "sunrise": 1759000000,
      "sunset": 1759040000
    },
    "location": {
      "name": "Tokyo",
      "country": "JP",
      "latitude": 35.6895,
      "longitude": 139.6917
    },
    "current": {
      "temperature": 19.4,
      "feels_like": 18.9,
      "humidity": 68,
      "pressure": 1011.0,
      "visibility": 10000,
      "wind_speed": 4.2,
      "wind_direction": 180,
      "condition": "Clouds",
      "description": "Scattered Clouds",
      "icon": "03d",
      "max_temperature": 21.0,
      "min_temperature": 18.0,
      "cloud_cover": 40,
      "last_updated": "2025-09-21T12:00:00Z"
    },
    "forecast": [
      {
        "date": "2025-09-22T00:00:00Z",
        "max_temperature": 24.5,
        "min_temperature": 18.2,
        "avg_temperature": 21.3,
        "condition": "Clear",
        "description": "Clear Sky",
        "icon": "01d",
        "humidity": 65,
        "wind_speed": 2.1,
        "precipitation": null,
        "chance_of_rain": 12,
        "uv_index": null,
        "pop": 0.12,
        "pop_min": 0.0,
        "pop_mean": 0.05,
        "temp": { "day": null, "min": 18.2, "max": 24.5, "night": null, "morn": null, "eve": null },
        "feels_like": { "day": null, "night": null, "morn": null, "eve": null },
        "wind_deg": 180,
        "wind_gust": 4.4,
        "clouds": 40,
        "visibility": 10000,
        "pressure": 1011,
        "rain": null,
        "snow": null,
        "uvi": null,
        "weather": [{ "id": 800, "main": "Clear", "description": "clear sky", "icon": "01d" }],
        "hourly": []
      }
    ],
    "request_time": "2025-09-21T10:30:15Z"
  }
}
```

Abridged: the `hourly` array of each day — the raw three-hour slots behind the rollup — is shown empty
here. See [Derived daily values on /weather/forecast](#derived-daily-values-on-weatherforecast).

The `current` block on this route is derived from the first three-hour slot of the same upstream
response, so it costs no extra OpenWeatherMap call. Two consequences: it is a slot's readings rather
than a live observation, so `last_updated` is that slot's timestamp and can sit either side of
`request_time` (this example's is 90 minutes ahead, because `list` starts at the next three-hour
boundary); and its `max_temperature` and `min_temperature` are that slot's own `main.temp_max` and
`main.temp_min`, which OpenWeatherMap documents as extremes in the city at the moment of calculation
rather than the day's extremes. Use `/weather/current` when you need a live reading.

#### POST /weather/forecast
Get weather forecast using JSON request body.

**Request Body:**
```json
{
  "location": "Tokyo,JP",
  "units": "metric",
  "days": 3
}
```

**Response:** Same as GET endpoint

#### GET /weather/forecast/7day
Get a 7-day daily forecast. Backed by the OpenWeatherMap **One Call 3.0** API, which needs its own
"One Call by Call" subscription (separate from the free 5-day/3-hour plan). The location name is
geocoded first, so each uncached request costs one Geocoding call plus one One Call call.

**Parameters:**
- `location` (required): City name, state code, and country code
- `units` (optional): Temperature units - `metric` (default), `imperial`, or `kelvin`
- `blocks` (optional): Comma-separated opt-in blocks, any of `hourly`, `minutely`, `alerts`.
  Defaults to none. See [Opt-in blocks](#opt-in-blocks).

**Example:**
```bash
curl -i "http://localhost:8080/api/v1/weather/forecast/7day?location=Tokyo,JP&units=metric"
```

**Response:**
```json
{
  "success": true,
  "data": {
    "onecall": {
      "lat": 35.6895,
      "lon": 139.6917,
      "timezone": "Asia/Tokyo",
      "timezone_offset": 32400,
      "current": {
        "dt": 1772000000,
        "sunrise": 1771960000,
        "sunset": 1772010000,
        "temp": 16.2,
        "feels_like": 15.8,
        "pressure": 1014,
        "humidity": 61,
        "dew_point": 9.4,
        "uvi": 1.2,
        "clouds": 40,
        "visibility": 10000,
        "wind_speed": 3.1,
        "wind_deg": 190,
        "wind_gust": 5.2,
        "weather": [{ "id": 802, "main": "Clouds", "description": "scattered clouds", "icon": "03d" }]
      }
    },
    "location": {
      "name": "Tokyo",
      "country": "JP",
      "region": "Tokyo",
      "latitude": 35.6895,
      "longitude": 139.6917
    },
    "current": {
      "temperature": 16.2,
      "feels_like": 15.8,
      "humidity": 61,
      "pressure": 1014.0,
      "visibility": 10000,
      "wind_speed": 3.1,
      "wind_direction": 190,
      "wind_gust": 5.2,
      "condition": "Clouds",
      "description": "Scattered Clouds",
      "icon": "03d",
      "max_temperature": null,
      "min_temperature": null,
      "cloud_cover": 40,
      "last_updated": "2026-03-01T06:00:00Z"
    },
    "forecast": [
      {
        "date": "2026-03-01T12:00:00Z",
        "max_temperature": 20.0,
        "min_temperature": 10.0,
        "avg_temperature": 15.0,
        "condition": "Clouds",
        "description": "Scattered Clouds",
        "icon": "03d",
        "humidity": 60,
        "wind_speed": 4,
        "precipitation": 1.5,
        "chance_of_rain": 25,
        "uv_index": 3.5
      }
    ],
    "request_time": "2026-03-01T06:00:00Z"
  }
}
```

Abridged: the `onecall.daily` array is elided here rather than shown empty — it has no `omitempty`,
so an upstream that sent no day at all would render it as `null`, never `[]`. `onecall` is the faithful
One Call 3.0 response; the `location`, `current`, `forecast` and `request_time` keys beside it are the
legacy vocabulary. See [Response schema](#response-schema).

The `current` block comes from the `current` object in the same One Call response, so it is genuinely
current — an observation, not a forecast slot — and also costs no extra call. One Call reports no
daily extremes in that block, so `max_temperature` and `min_temperature` are `null` here. The day's own
extremes are on the `onecall.daily` entries and on the `forecast` array.

#### POST /weather/forecast/7day
Same as the GET endpoint, with a JSON body. `days` is not accepted: this route always returns 7 days.
`blocks` is accepted as a body field, with the same allowlist and the same `400` on an unrecognised
value.

```bash
curl -X POST "http://localhost:8080/api/v1/weather/forecast/7day" \
  -H "Content-Type: application/json" \
  -d '{"location":"Tokyo,JP","units":"metric","blocks":"hourly,alerts"}'
```

### Response schema

Every weather response carries two vocabularies side by side. Three rules govern both, and they are
the rules a consumer cannot infer from the JSON itself.

1. **Faithful keys** mirror the documented schema of that route's own upstream endpoint, and nothing
   else. A field absent from that endpoint's schema gets no faithful key on that route. The one
   exception is `wind.gust` on `/current` and `forecast[].hourly[].wind.gust` on `/forecast`: both are
   emitted, because OpenWeatherMap does send a gust in practice on the two `/data/2.5` endpoints even
   though only the daily endpoints document it, and dropping a reading the upstream actually sent
   would be the same defect as inventing one.
2. **Legacy keys** are the original public vocabulary. Every one of them is always present, with three
   exceptions that carry `omitempty` and are therefore *absent* rather than `null` when the route
   cannot supply them: `current.wind_gust`; `location.region`, which only the 7-day route populates
   and only when the geocoder returned a state or province; and `location.timezone`, which no route
   populates at all — the faithful `timezone` on `/current` and `onecall.timezone` on `/forecast/7day`
   are the upstream's own value and are the keys to read. A legacy key outside that list that the
   route cannot supply serialises as `null`.
3. **Omitted is not the same as null.** A field the route's endpoint documents but the upstream had
   no value for is `null`. An opt-in block the caller did not request is left out of the response
   entirely. A block the caller asked for is never omitted.

Pointers are what make the third rule work: a measured `0` serialises as `0`, a gap serialises as
`null`, and a nil block is a third state distinct from both — an absent block, rather than a block
whose every member is missing. The `omitempty` keys in rule 2 are the one place a gap shows up as an
absent key, so a reader looking for `null` on `current.wind_gust` will not find it and should look
for the key instead.

#### The `current` block and its faithful twin

`current` is the legacy block on all three routes, and each legacy key carries the same reading as the
faithful key in the same column. `null` means the route measures no such value — except `wind_gust`,
which is the one key here that is absent rather than `null` when there is no gust, and which only
appears at all when the upstream reported one.

| Legacy key | `/current` | `/forecast` | `/forecast/7day` |
|---|---|---|---|
| `temperature` | `main.temp` | `forecast[0].hourly[0].main.temp` | `onecall.current.temp` |
| `feels_like` | `main.feels_like` | `forecast[0].hourly[0].main.feels_like` | `onecall.current.feels_like` |
| `humidity` | `main.humidity` | `forecast[0].hourly[0].main.humidity` | `onecall.current.humidity` |
| `pressure` | `main.pressure` | `forecast[0].hourly[0].main.pressure` | `onecall.current.pressure` |
| `visibility` | `visibility` | `forecast[0].hourly[0].visibility` | `onecall.current.visibility` |
| `wind_speed` | `wind.speed` | `forecast[0].hourly[0].wind.speed` | `onecall.current.wind_speed` |
| `wind_direction` | `wind.deg` | `forecast[0].hourly[0].wind.deg` | `onecall.current.wind_deg` |
| `wind_gust` † | `wind.gust` | `forecast[0].hourly[0].wind.gust` | `onecall.current.wind_gust` |
| `cloud_cover` | `clouds.all` | `forecast[0].hourly[0].clouds.all` | `onecall.current.clouds` |
| `condition` | `weather[0].main` | `forecast[0].hourly[0].weather[0].main` | `onecall.current.weather[0].main` |
| `description` | `weather[0].description` | `forecast[0].hourly[0].weather[0].description` | `onecall.current.weather[0].description` |
| `icon` | `weather[0].icon` | `forecast[0].hourly[0].weather[0].icon` | `onecall.current.weather[0].icon` |
| `last_updated` | `dt` | `forecast[0].hourly[0].dt` | `onecall.current.dt` |
| `max_temperature` | `null` | `forecast[0].hourly[0].main.temp_max` | `null` |
| `min_temperature` | `null` | `forecast[0].hourly[0].main.temp_min` | `null` |

† `wind_gust` is the one row that is absent rather than `null` when there is no gust, on all three
routes: the legacy key carries `omitempty`, and a measured `0` is a non-nil pointer to zero so it is
reported rather than dropped.

Four of those rows are one reading in two renderings rather than the same number twice.
`description` is the upstream's lowercase string title-cased on the legacy side, as it always has.
`pressure` and `visibility` are integers on the faithful side and floats on the legacy one, widened
rather than reinterpreted. `last_updated` is the faithful epoch timestamp rendered as RFC3339 in UTC —
the same instant as before, in a rendering that no longer depends on the server's time zone.

The `/forecast` column reads `hourly[0]` rather than the day's rollup because the block is derived
from the first raw slot of the day. That is also why its `max_temperature` and `min_temperature` are
not the day's extremes — see [Derived daily values on /weather/forecast](#derived-daily-values-on-weatherforecast).

#### Derived daily values on /weather/forecast

The three-hour endpoint reports per slot and nothing per day, so nearly every value on a
`/weather/forecast` day is a rollup this API derives from that day's slots. The slots themselves are
carried raw under `hourly[]`, so a client that wants a single period rather than a daily rollup does
not have to aggregate anything.

| Key | How it is derived |
|---|---|
| `temp.min`, `temp.max` | The minimum and maximum of the day's slot `main.temp` values |
| `avg_temperature` | The mean of the day's slot `main.temp` values |
| `humidity` | The mean of the slots' `main.humidity`, truncated |
| `pressure`, `clouds`, `visibility` | The means of the slots' `main.pressure`, `clouds.all` and `visibility`, rounded |
| `wind_speed` | The mean of the slots' `wind.speed` |
| `wind_deg` | The `wind.deg` of the slot with the day's highest `wind.speed`, which is what `deg` means on the daily endpoints |
| `wind_gust` | The maximum of the slots' `wind.gust` |
| `pop` | The maximum of the slots' `pop` |
| `pop_min`, `pop_mean` | The minimum, and the arithmetic mean, of the slots' fractional `pop` values — the mean of the probabilities, not of their percentages |
| `rain.3h`, `snow.3h` | The sums of the slots' `3h` windows |
| `precipitation` | The sum of those two totals, or `null` when no slot reported a window |
| `chance_of_rain` | `pop` as a whole percentage, rounded |
| `weather[]`, `condition`, `description`, `icon` | The middle slot's weather entry |
| `temp.day`, `temp.night`, `temp.morn`, `temp.eve` | Always `null`: this endpoint has no time-of-day breakdown |
| `feels_like` | Every member always `null`: this endpoint has no apparent-temperature breakdown |
| `uvi`, `uv_index` | Always `null`: this endpoint reports no ultraviolet index |

A mean is taken over the slots that carry the member, not over every slot. A day whose first slot
reports no `pop` has a probability stated over the slots that have one, rather than `null`.

**The mixed rounding policy is deliberate.** `humidity` truncates because the legacy field is an
`int` that has always truncated, and changing it would move a number existing consumers already read
for no accuracy — a mean of 67.5% is 67 either way as far as that key is concerned. `pressure`,
`clouds` and `visibility` have no legacy counterpart, so nothing constrains them and they carry the
nearest integer. `chance_of_rain` rounds, because a truncation reports a probability the upstream
never gave: `0.29 * 100` is `28.999999999999996` in binary floating point, so truncating would answer
28 for a day OpenWeatherMap called 29 percent.

#### A slot's temperature is not the day's temperature

Within `hourly[]`, each slot's `main.temp_min` and `main.temp_max` are faithful to the upstream: they
are the extremes in the city at the moment that slot was calculated. At the day level, `temp.min` and
`temp.max` are this API's own derivation of the extremes of the day's slot temperatures. Both are
correct, they measure different things, and they are not interchangeable. The same distinction is why
`current.max_temperature` and `current.min_temperature` on `/forecast` carry a slot's numbers rather
than the day's.

#### Opt-in blocks

`?blocks=hourly,minutely,alerts` on `/weather/forecast/7day`, and `"blocks": "..."` in the POST body,
expose the three large One Call blocks. The parameter controls what is *exposed*, never what is
fetched: the upstream call always asks for everything, so asking for a block costs no extra call and
no extra quota, and the cache key does not carry the parameter.

The value is checked against an allowlist, so a typo is a `400` rather than a silently missing block:

```json
{
  "success": false,
  "error": {
    "error": "Bad Request",
    "code": 400,
    "message": "unknown block \"hourley\": want one of hourly, minutely, alerts"
  }
}
```

The allowlist is a set, so `blocks=hourly` and `blocks=hourly,minutely,alerts` are two different
requests. A block the caller asked for and the upstream reported as an empty array comes back as an
empty array, not as a missing key. The parameter is read only on `/weather/forecast/7day`: on
`/current` and `/forecast`, on both the GET and the POST route, it is silently ignored rather than
rejected.

#### The `onecall` block

The faithful One Call data lives under a single `onecall` key rather than at the top level, because the
legacy `current` block and the faithful One Call `current` object both want the JSON key `current`.
`onecall` is always present, so a caller who asks for no blocks still receives `lat`, `lon`,
`timezone`, `timezone_offset`, `current` and `daily`. The three opt-in members appear only when
requested:

```
onecall{lat, lon, timezone, timezone_offset, current{...}, daily[]{...}}
onecall.minutely[]{dt, precipitation}                      # opt-in
onecall.hourly[]{dt, sunrise, sunset, temp, feels_like, pressure, humidity, dew_point,
                 uvi, clouds, visibility, wind_speed, wind_deg, wind_gust, pop, rain,
                 snow, weather[]}                           # opt-in
onecall.alerts[]{sender_name, event, start, end, description, tags[]}   # opt-in
```

`onecall.daily[]{...}` carries `dt`, `sunrise`, `sunset`, `moonrise`, `moonset`, `moon_phase`,
`temp{day,min,max,night,morn,eve}`, `feels_like{day,night,morn,eve}`, `pressure`, `humidity`,
`dew_point`, `wind_speed`, `wind_deg`, `wind_gust`, `weather[]`, `clouds`, `pop`, `rain`, `snow` and
`uvi`. `onecall.daily[]` is uncapped: it holds every day the upstream sent, which may be more than the
seven in the legacy `forecast[]` beside it. `rain` and `snow` there are plain millimetre totals for the
day, not the windowed `{1h}`/`{3h}` blocks the `/data/2.5` endpoints use, and `daily` has no
`omitempty`, so an upstream that sent no day at all renders it as `null`.

#### No `chance_of_rain` on /weather/current

`/weather/current` emits no `chance_of_rain` key in either vocabulary, because `/data/2.5/weather` has
no `pop` field to mirror. This is not a gap in the response: a key that could only ever be `null`
carries no information. `/forecast` and `/forecast/7day` both have a real upstream source and both
report an accurate probability.

#### The 5-day route's `current` block is derived, not observed

`/forecast/7day` is not in this category. Its `current` block is the One Call `current` object, which
is an observation of conditions at the moment the upstream calculated it, and the route is genuinely
current.

`/forecast` is. Its block comes from the first three-hour slot of the forecast payload, not from a
second upstream call, which is why the route cannot also be reporting live conditions. `last_updated`
is that slot's timestamp, and it can sit on either side of `request_time`: OpenWeatherMap's `list`
starts at the *next* three-hour boundary, so shortly after an hour boundary `list[0]` is in the future
by up to three hours, and shortly before one it can be an hour or more old. Read it as "this is the
slot this reading came from", not as an age.

### Breaking changes

The faithful blocks are additive, and every legacy key is still present. These keys however changed
*value* against their previous readings, and this was deliberate — a field that is present, plausible
and quietly wrong is worse than a missing one. Consumers that read any of them need to be checked.

| Key | Was | Is now |
|---|---|---|
| `/forecast` day `max_temperature`, `min_temperature` | The extremes of the day's slots' upstream `temp_max`/`temp_min`, which OpenWeatherMap documents as extremes in the city at the moment of calculation | The derived extremes of the day's slot temperatures, which is what the names claim |
| `/forecast` day `chance_of_rain` | Always `0` | The rounded daily probability, or `null` when no slot reported one |
| `/forecast` day `uv_index` | Always `0` | Always `null`: this endpoint reports no ultraviolet index |
| `/forecast` day `precipitation` | Always a number, `0` when nothing fell or nothing was measured | `null` when no slot reported a precipitation window, and the sum when one did |
| `/forecast/7day` day `precipitation` | Same as above | Same as above |
| `/forecast` day `condition`, `description`, `icon` | `""` when the middle slot carried no weather entry | `null`, matching what the other three blocks have always reported for that case |
| `/forecast/7day` day `chance_of_rain` | Truncated percentage | Rounded percentage, and `null` when the day carried no `pop` |
| `current.max_temperature`, `min_temperature` on `/current` and `/forecast/7day` | Always `0` | Always `null`: neither route measures daily extremes in its current block |
| `current.visibility` on `/forecast` | Always `0` | The first slot's `visibility`, or `null` when that slot reports none |
| `current.wind_gust` | A measured gust of `0` was indistinguishable from no gust and was dropped, because the key carried `omitempty` on a plain number | A measured `0` is reported as `0`; a gust the upstream did not send leaves the key absent |
| `forecast` key on both forecast routes | Absent when the upstream sent no days | Always present, as `[]` |
| `date` on `/forecast/7day`, and `last_updated` and `request_time` on all three routes | Rendered in the server's local zone | Rendered in UTC, so the same body serialises identically on every host |

A legacy `condition`, `description` or `icon` is `null` on every block of every route where the
upstream carried no weather entry — both forecast `current` blocks, the 7-day day block and the
5-day day block alike. `last_updated` is `null` on the forecast routes if the upstream sent no current
block at all, where it was previously `1970-01-01T00:00:00Z`.

### API key per request

The server-side key is used by default, but any weather route accepts a caller-supplied key:

| Where | How |
|-------|-----|
| All weather routes | `X-API-Key: <key>` header |
| GET routes | `?key=<key>` query parameter |
| POST routes | `"keys": "<key>"` in the JSON body |

### Caching

Weather responses are cached in memory for 15 minutes to keep OpenWeatherMap usage inside the free
tier quota.

- Cache keys include the api key actually used for the request (hashed), so each key has its own
  entries and a caller's key is never used to serve another caller's request.
- Concurrent identical requests collapse into a single upstream call, so a cold-cache burst costs
  one call, not one per request.
- Only successful responses are cached; errors are retried on the next request.
- Every weather response carries `X-Cache: HIT` or `X-Cache: MISS` so you can verify the savings.
- A cache hit returns the original `request_time`, which is when the data was actually fetched.
- The cache lives in the process, so every running instance has its own: N instances means up to N times
  the upstream calls. On Cloud Run that is the dominant cost of this design.
- A cached 7-day entry holds the `minutely`, `hourly` and `alerts` blocks even when no caller asked
  for them, so one upstream fetch serves every `?blocks=` variant. That is what keeps the parameter off
  the cache key, and it is also why the per-process footprint grew: a full One Call body is tens of
  kilobytes of structs, against a 500-entry ceiling.

### Error Responses

All errors follow a consistent format:

```json
{
  "success": false,
  "error": {
    "error": "Bad Request",
    "code": 400,
    "message": "Location is required"
  }
}
```

The error object has three keys and nothing else, and no error carries a fourth. Internally some
errors carry a `details` string, but the response writer copies only the message across, so **the
detail is dropped rather than surfaced**: a `days=6` request gets `"Invalid days parameter"` and never
learns that the limit is 5. The `?blocks=` `400` is the one case that carries a diagnostic, because
that handler writes the text into the message itself rather than into the detail slot.

## ⚙️ Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `OPENWEATHERMAP_API_KEY` | Yes | - | Your OpenWeatherMap API key |
| `PORT` | No | `8080` | Server port |
| `HOST` | No | `0.0.0.0` | Server host |
| `ENVIRONMENT` | No | `development` | Environment (development/production) |
| `LOG_LEVEL` | No | `info` | Log level (debug/info/warn/error) |

### Example .env file
```env
OPENWEATHERMAP_API_KEY=your_api_key_here
PORT=8080
HOST=0.0.0.0
ENVIRONMENT=development
LOG_LEVEL=info
```

## 🏗️ Project Structure

```
weathering-with-go/
├── config/
│   └── config.go           # Configuration management
├── handlers/
│   ├── routes.go          # Route definitions
│   └── weather.go         # Weather request handlers
├── middleware/
│   └── middleware.go      # HTTP middleware
├── models/
│   ├── openweather.go     # OpenWeatherMap API models
│   ├── responses.go       # Response schema, the blocks and the opt-in block allowlist
│   └── weather.go         # Internal data models
├── services/
│   ├── cache.go           # 15 minute response cache with request collapsing
│   └── weather.go         # Weather service logic
├── utils/
│   └── errors.go          # Error handling utilities
├── go.mod                 # Go module dependencies
├── go.sum                 # Dependency checksums
├── main.go               # Application entry point
└── README.md             # This file
```

## 🔧 Development

### Running Tests
```bash
go test ./...
```

### Building for Production
```bash
go build -o weathering-with-go main.go
```

### Docker Support
```bash
# Build image
docker build -t weathering-with-go .

# Run container
docker run -p 8080:8080 -e OPENWEATHERMAP_API_KEY=your_key weathering-with-go
```

## 📊 API Limits

- **OpenWeatherMap Free Tier**: 1,000 calls/day, 60 calls/minute
- **Forecast**: Up to 5 days (OpenWeatherMap limitation)

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 🙏 Acknowledgments

- [OpenWeatherMap](https://openweathermap.org/) for providing the weather data API
- [Gin Framework](https://gin-gonic.com/) for the excellent HTTP web framework
- [Go Community](https://golang.org/) for the amazing programming language
