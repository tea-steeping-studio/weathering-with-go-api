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
- The cache is per-process, so on Cloud Run N instances mean up to N× the calls.
- The `current` block on both forecast routes is derived from data already present in the same upstream response (`list[0]` for the 5-day route, the One Call `current` object for the 7-day route). Never add a second upstream call to fill it; `TestForecastHandlerDoesNotCallCurrentEndpoint` guards that.

## Testing quirks

- Tests never hit the network. `handlers/weather_test.go` stubs OpenWeatherMap by replacing `WeatherService.HTTPClient` with a custom `RoundTripper` that redirects to an `httptest` server, because `services.OpenWeatherMapBaseURL` is a `const`. Changing that URL requires updating the test's prefix check.
- The 7-day route uses two more `const` base URLs (`GeocodingBaseURL`, `OneCallBaseURL`) and its tests route on `r.URL.Path` in the stub server.
- `services.convertForecastResponse` groups 3-hour items into a Go map and iterates it, so forecast day order is nondeterministic. Sort before asserting order. The 7-day path (`convertOneCallResponse`) is unaffected: it iterates the ordered `daily` slice.

## Repo gotchas

- Default branch is `master`. CI (`.github/workflows/go.yml`) runs build + `go test -v ./...` on pushes/PRs to `master` and publishes GitHub releases on `v*` tags using a binary built with the `OPENWEATHERMAP_API_KEY` secret.
- `middleware.RateLimit()` is a no-op and is never registered in `main.go`, despite README claims of built-in rate limiting.
- `fyne.io/fyne/v2` is a stale direct dependency (the GUI under gitignored `cmd/ui` was removed). `go mod tidy` will drop it; there is currently no reason to add GUI code. `golang.org/x/sync` is a real direct dep (singleflight) and was promoted by hand — do not let a `go mod tidy` churn hide that.
- Docker image targets Cloud Run: static `CGO_ENABLED=0` build, distroless nonroot runtime, binary at `/app/main`, listens on `0.0.0.0:$PORT`.
- README drift beyond the above: it claims Go 1.19 while the project requires 1.25.1.
