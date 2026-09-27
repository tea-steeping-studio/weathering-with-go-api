package services

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheStaysWithinCapacity(t *testing.T) {
	svc := NewWeatherService("dummy")

	for i := 0; i < cacheMaxEntries+50; i++ {
		key := fmt.Sprintf("current|city%d|metric", i)
		fetch := func() (int, error) { return i, nil }
		if _, err := cachedFetch(svc, key, fetch); err != nil {
			t.Fatalf("fetch %d returned error: %v", i, err)
		}
	}

	if got := len(svc.cache); got > cacheMaxEntries {
		t.Fatalf("expected at most %d cached entries, got %d", cacheMaxEntries, got)
	}
}

func TestCachedFetchDoesNotCacheErrors(t *testing.T) {
	svc := NewWeatherService("dummy")
	calls := 0
	fetch := func() (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("upstream boom")
		}
		return 5, nil
	}

	if _, err := cachedFetch(svc, "forecast7day|Nowhere", fetch); err == nil {
		t.Fatal("expected first call to return the upstream error")
	}

	value, err := cachedFetch(svc, "forecast7day|Nowhere", fetch)
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if value != 5 {
		t.Fatalf("expected retried value 5, got %d", value)
	}
	if calls != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", calls)
	}
}

func TestCachedFetchCollapsesConcurrentIdenticalCalls(t *testing.T) {
	svc := NewWeatherService("dummy")

	var calls int32
	release := make(chan struct{})
	fetch := func() (int, error) {
		atomic.AddInt32(&calls, 1)
		<-release
		return 7, nil
	}

	const concurrency = 8
	values := make([]int, concurrency)
	errs := make([]error, concurrency)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			values[i], errs[i] = cachedFetch(svc, "current|London|metric", fetch)
		}(i)
	}
	close(start)

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt32(&calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 upstream call for %d concurrent identical requests, got %d", concurrency, got)
	}
	for i := 0; i < concurrency; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d returned error: %v", i, errs[i])
		}
		if values[i] != 7 {
			t.Fatalf("request %d expected 7 got %d", i, values[i])
		}
	}
}

func TestWeatherCacheKeyPartitionsByAPIKey(t *testing.T) {
	alice := weatherCacheKey("key-a", currentCacheEndpoint, "London,UK", "metric", 0)
	bob := weatherCacheKey("key-b", currentCacheEndpoint, "London,UK", "metric", 0)

	if alice == bob {
		t.Fatal("expected different cache keys for different api keys")
	}
	if strings.Contains(alice, "key-a") {
		t.Fatalf("expected api key to be hashed, got %q", alice)
	}
}

func TestWeatherCacheKeyIsStableForSameRequest(t *testing.T) {
	first := weatherCacheKey("key-a", currentCacheEndpoint, "London,UK", "metric", 0)
	second := weatherCacheKey("key-a", currentCacheEndpoint, "  london,uk  ", "metric", 0)
	if first != second {
		t.Fatalf("expected normalized location to produce the same cache key: %q vs %q", first, second)
	}
}

func TestWeatherCacheKeySeparatesEndpointsAndDays(t *testing.T) {
	current := weatherCacheKey("key-a", currentCacheEndpoint, "London,UK", "metric", 0)
	forecastThree := weatherCacheKey("key-a", forecastCacheEndpoint, "London,UK", "metric", 3)
	forecastFive := weatherCacheKey("key-a", forecastCacheEndpoint, "London,UK", "metric", 5)

	if current == forecastThree || forecastThree == forecastFive {
		t.Fatal("expected cache keys to differ across endpoints and day counts")
	}
}

func TestCachedFetchRefetchesAfterTTL(t *testing.T) {
	svc := NewWeatherService("dummy")
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return start }

	calls := 0
	fetch := func() (int, error) {
		calls++
		return calls, nil
	}

	first, err := cachedFetch(svc, "current|London|metric", fetch)
	if err != nil {
		t.Fatalf("first fetch returned error: %v", err)
	}

	svc.now = func() time.Time { return start.Add(CacheTTL - time.Second) }
	cached, err := cachedFetch(svc, "current|London|metric", fetch)
	if err != nil {
		t.Fatalf("fetch inside TTL returned error: %v", err)
	}
	if cached != first {
		t.Fatalf("expected cached value %d inside TTL, got %d", first, cached)
	}
	if calls != 1 {
		t.Fatalf("expected 1 upstream call inside TTL, got %d", calls)
	}

	svc.now = func() time.Time { return start.Add(CacheTTL) }
	refreshed, err := cachedFetch(svc, "current|London|metric", fetch)
	if err != nil {
		t.Fatalf("fetch after TTL returned error: %v", err)
	}
	if refreshed == first {
		t.Fatalf("expected refetch after TTL, still got cached value %d", first)
	}
	if calls != 2 {
		t.Fatalf("expected 2 upstream calls after TTL, got %d", calls)
	}
}

func TestCachedFetchServesSecondCallFromCache(t *testing.T) {
	svc := NewWeatherService("dummy")
	calls := 0
	fetch := func() (int, error) {
		calls++
		return 42, nil
	}

	first, err := cachedFetch(svc, "current|London|metric", fetch)
	if err != nil {
		t.Fatalf("first fetch returned error: %v", err)
	}
	if first != 42 {
		t.Fatalf("expected 42 got %d", first)
	}

	second, err := cachedFetch(svc, "current|London|metric", fetch)
	if err != nil {
		t.Fatalf("second fetch returned error: %v", err)
	}
	if second != 42 {
		t.Fatalf("expected 42 got %d", second)
	}
	if calls != 1 {
		t.Fatalf("expected upstream to be called once, got %d", calls)
	}
}
