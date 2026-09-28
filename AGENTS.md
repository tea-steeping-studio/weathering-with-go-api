# AGENTS.md

## Project

Go 1.25 + Gin REST API wrapping OpenWeatherMap. Single module with the bare path `weathering-with-go` — local imports look like `weathering-with-go/services`. Entrypoint is `main.go`; packages `config`, `handlers`, `middleware`, `models`, `services`, `utils`. API is under `/api/v1`; health is at both `/health` and `/api/v1/health`. Weather routes are `current`, `forecast` (5 day, `days` 1-5) and `forecast/7day` (One Call 3.0, no `days` param, geocodes `location` first).

## Commands

- `go test ./...`, `go build ./...`, `go vet ./...`, `go run .`
- Toolchain is pinned to 1.25.1 in `go.mod`, CI, and the Dockerfile. If `go` fails with "No version is set for shim", the mise shim has no default; prefix commands: `mise exec go@1.25.7 -- go test ./...`.
- Production/CI build embeds config via ldflags:
  `go build -ldflags "-X 'weathering-with-go/config.BuildTimeAPIKey=$KEY' -X 'weathering-with-go/config.Environment=production'" -o weathering-with-go .`
- `go build` in the repo root writes a `weathering-with-go` binary that is **not** covered by `.gitignore`. Delete it or use `-o /tmp/...`; there is an untracked 29 MB binary in the working tree already.

## API key handling (easy to miss)

- Server-side key comes from `OPENWEATHERMAP_API_KEY` at runtime, falling back to `config.BuildTimeAPIKey` injected with ldflags. Runtime env wins over build-time key.
- `config.Environment` is also ldflag-injected, and if set it takes precedence over the `ENVIRONMENT` env var.
- Requests may override the key: `X-API-Key` header on all weather routes, `?key=` query on GET, and the `"keys"` JSON field on POST. Empty means fall back to the server key. The README still says the key is server-side only — do not trust that.
- `config.Load()` logs the resolved API key, including build-time keys. Keep that in mind before pasting logs into issues.

## Caching (services/cache.go)

- Every upstream call goes through `cachedFetch`/`cachedFetchTracked` in `services/cache.go`. `CacheTTL` is 15 min; `cacheMaxEntries` is 500. The whole point is to protect the OpenWeatherMap free-tier quota, so a new upstream call must not bypass the cache.
- Cache keys are built by `weatherCacheKey(endpoint, location, units, days, apikey)` and deliberately include a hash of the api key: per-user keys get per-user entries. Do not "simplify" the key by dropping the key hash — that would spend one caller's quota for everyone.
- `singleflight` collapses concurrent identical misses into one upstream call. A caller that joins an in-flight fetch is reported as a miss, not a hit.
- Only 200s are cached. Errors propagate to singleflight waiters but are never stored, so a bad location or 401 is retried instead of being pinned for 15 min.
- `WeatherService.now` is a func field, not `time.Now()` calls. Tests advance it to exercise TTL expiry; never bypass it or the expiry tests stop being deterministic.
- Handlers surface cache state as `X-Cache: HIT|MISS` via `utils.SendCachedSuccess`. On a hit, `request_time` intentionally stays at the original fetch time.
- The cache is per-process, so on Cloud Run N instances mean up to N× the calls. A cached response now also retains the `minutely`, `hourly` and `alerts` blocks, so the per-process footprint grew to roughly 15–30 MB at `cacheMaxEntries`.
- The `current` block on both forecast routes is derived from data already present in the same upstream response (`list[0]` for the 5-day route, the One Call `current` object for the 7-day route). Never add a second upstream call to fill it; `TestForecastHandlerDoesNotCallCurrentEndpoint` guards that. On the 5-day route the block marks a *forecast slot*, not an observation, and because the list starts at the next 3-hour boundary its `last_updated` can sit ahead of `request_time`.

## Testing quirks

- Tests never hit the network. `handlers/weather_test.go` stubs OpenWeatherMap by replacing `WeatherService.HTTPClient` with a custom `RoundTripper` that redirects to an `httptest` server, because `services.OpenWeatherMapBaseURL` is a `const`. Changing that URL requires updating the test's prefix check.
- The 7-day route uses two more `const` base URLs (`GeocodingBaseURL`, `OneCallBaseURL`) and its tests route on `r.URL.Path` in the stub server.
- The 5-day route groups 3-hour items by **UTC** date into a Go map, then sorts the date keys so the returned days are chronological and `days=<n>` returns the *earliest* n days. It needs sorting defensively in tests that assert order across a date boundary. The grouping is `.UTC()` on purpose: without it the day boundary shifts with the server's `TZ`. The 7-day path needs no sorting: it iterates the ordered `daily` slice.

## Repo gotchas

- Default branch is `master`. CI (`.github/workflows/go.yml`) runs build + `go test -v ./...` on pushes/PRs to `master` and publishes GitHub releases on `v*` tags using a binary built with the `OPENWEATHERMAP_API_KEY` secret.
- `middleware.RateLimit()` is a no-op and is never registered in `main.go`. The README's rate-limiting claims were removed rather than the middleware implemented.
- `fyne.io/fyne/v2` is a stale direct dependency (the GUI under gitignored `cmd/ui` was removed). `go mod tidy` will drop it; there is currently no reason to add GUI code. `golang.org/x/sync` is a real direct dep (singleflight) and was promoted by hand — do not let a `go mod tidy` churn hide that.
- Docker image targets Cloud Run: static `CGO_ENABLED=0` build, distroless nonroot runtime, binary at `/app/main`, listens on `0.0.0.0:$PORT`.
- `APIError.Details` is populated by `utils.NewAPIError` but never rendered: `models.ErrorResponse` has no `details` field, and `utils.SendError` copies only `Message`. Every error response in the API drops its detail. Only the `blocks` 400s work around it by putting the text in the message.
- `models.Location.Timezone` is populated by no route. It is honestly absent rather than wrong.

## Response mapping (do not break this)

The API returns two vocabularies per route: the pre-existing keys consumers depend on,
and faithful keys mirroring that route's own upstream schema. One invariant governs all
of it:

- **A field the upstream measured as `0` serialises as `0`. Only a field with no value
  serialises as `null`.** This is the rule the whole `field-fidelity` work exists to
  enforce, and it was violated nine separate times after the first fix. Pointers
  represent "may be absent". Never read a faithful field from a non-pointer decode
  struct — that is precisely how a measured zero becomes indistinguishable from absence.
- `omitempty` is permitted on **pointer** fields, where it drops only `nil`, and
  forbidden on **value** fields, where it drops a measured zero.
- Three drift guards enforce this and must stay green: `TestCurrentPayloadCoversDecodedFields`,
  `TestForecastPayloadCoversDecodedFields` and `TestSevenDayPayloadCoversDecodedFields`
  (plus their `*ResponseCoversUpstreamMembers` counterparts) check that every documented
  upstream field is bound by its payload rather than allowlisted away, and
  `TestLegacyAliasesMatchFaithfulFields` in `handlers/alias_test.go` pins 55 legacy keys to
  their faithful twins. A guard that passes vacuously is worse than no guard — each of
  these has been proven non-vacuous by mutation, and re-prove it after changing one.
- The payload guards failed silently once because a false allowlist entry
  (`forecastNoKelvinFactor`) suppressed a field the documentation *does* define
  (`list.main.temp_kf` on `/data/2.5/forecast`). **Before allowlisting a field as
  "not documented", check the OpenWeatherMap docs.** The cost of a false entry is that the
  field reports `null` forever with both guards green.
- `Project` (on `SevenDayResponse`) must never mutate its receiver: the cache hands the
  same pointer to every concurrent caller, and stripping a block in place corrupts a
  concurrent fat request. It copies, then clears. A concurrency test guards this.
- The cache key intentionally does **not** include the `blocks` opt-in parameter; the
  cached value is always complete so one upstream call serves every variant. Adding it
  would spend free-tier quota to save a struct copy.
