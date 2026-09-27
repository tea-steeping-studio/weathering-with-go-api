package services

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// Cache endpoint identifiers used as the first part of a cache key.
const (
	currentCacheEndpoint  = "current"
	forecastCacheEndpoint = "forecast"
	sevenDayCacheEndpoint = "forecast7day"
	geocodeCacheEndpoint  = "geocode"
)

// CacheTTL is how long a cached upstream response is served before it is refetched.
const CacheTTL = 15 * time.Minute

// cacheMaxEntries bounds memory use; cache keys are derived from user input.
const cacheMaxEntries = 500

type cacheEntry struct {
	data    any
	expires time.Time
}

// weatherCacheKey builds a cache key that is stable for equivalent requests and
// distinct per api key, endpoint, location, units and day count.
func weatherCacheKey(apikey, endpoint, location, units string, days int) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(location), " "))
	sum := sha256.Sum256([]byte(apikey))
	keyHash := hex.EncodeToString(sum[:])[:16]
	return strings.Join([]string{endpoint, normalized, units, strconv.Itoa(days), keyHash}, "|")
}

// cachedFetch returns the cached value for key, calling fetch on a miss.
func cachedFetch[T any](w *WeatherService, key string, fetch func() (T, error)) (T, error) {
	data, _, err := cachedFetchTracked(w, key, fetch)
	return data, err
}

// cachedFetchTracked behaves like cachedFetch and also reports whether the value
// was served from cache. A caller that joined an in-flight fetch is a miss, not a hit.
func cachedFetchTracked[T any](w *WeatherService, key string, fetch func() (T, error)) (T, bool, error) {
	if data, ok := w.cacheLookup(key); ok {
		return data.(T), true, nil
	}

	value, err, _ := w.group.Do(key, func() (any, error) {
		if data, ok := w.cacheLookup(key); ok {
			return data, nil
		}
		data, err := fetch()
		if err == nil {
			w.cacheStore(key, data)
		}
		return data, err
	})
	if err != nil {
		var zero T
		return zero, false, err
	}

	return value.(T), false, nil
}

func (w *WeatherService) cacheLookup(key string) (any, bool) {
	w.mu.RLock()
	entry, ok := w.cache[key]
	w.mu.RUnlock()
	if !ok || !w.now().Before(entry.expires) {
		return nil, false
	}
	return entry.data, true
}

func (w *WeatherService) cacheStore(key string, data any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.cache) >= cacheMaxEntries {
		w.evictLocked()
	}
	w.cache[key] = cacheEntry{data: data, expires: w.now().Add(CacheTTL)}
}

// evictLocked drops expired entries, and clears the cache if that frees nothing.
func (w *WeatherService) evictLocked() {
	now := w.now()
	for existing, entry := range w.cache {
		if !now.Before(entry.expires) {
			delete(w.cache, existing)
		}
	}
	if len(w.cache) >= cacheMaxEntries {
		clear(w.cache)
	}
}
