package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

// TestGetCached_ConcurrentAccess verifies that getCached's double-checked
// caching executes fetch outside metricsMu, avoids deadlocks, and coordinates
// concurrent callers. The double check protects the stored entry, not the
// fetch — on a cold key every caller can miss before the first store lands —
// so no tight upper bound on the cold wave's fetch count is assertable. The
// cache's real promise is the warm path: within the TTL, no caller (concurrent
// or serial) fetches again, and that is pinned here deterministically.
func TestGetCached_ConcurrentAccess(t *testing.T) {
	var fetchCount atomic.Int64
	key := fmt.Sprintf("test-key-%d", time.Now().UnixNano())
	fetch := func() uint64 {
		fetchCount.Add(1)
		time.Sleep(5 * time.Millisecond) // simulate disk I/O
		return 42
	}
	runWave := func(n int) {
		var wg sync.WaitGroup
		results := make([]uint64, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results[idx] = getCached(key, fetch)
			}(i)
		}
		wg.Wait()
		for idx, val := range results {
			if val != 42 {
				t.Errorf("routine %d got val %d, want 42", idx, val)
			}
		}
	}

	const goroutines = 20
	runWave(goroutines)
	coldFetches := fetchCount.Load()

	// Warm concurrent wave: pure cache hits, zero fetches.
	runWave(goroutines)
	if count := fetchCount.Load(); count != coldFetches {
		t.Errorf("warm concurrent wave refetched: fetchCount %d -> %d, want unchanged", coldFetches, count)
	}

	// Serial cache hit likewise must not re-fetch.
	getCached(key, fetch)
	if count := fetchCount.Load(); count != coldFetches {
		t.Errorf("serial cache hit refetched: fetchCount %d -> %d, want unchanged", coldFetches, count)
	}
}

// TestAdminStatus_ConcurrentRequests tests that concurrent /status queries
// do not race or deadlock on metrics collection.
func TestAdminStatus_ConcurrentRequests(t *testing.T) {
	const yaml = `
listen: 127.0.0.1:18800
providers:
  - {name: p1, base_url: {openai-completions: http://127.0.0.1:1}, api_key: k}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)

	srv := New(rt, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const reqs = 30
	var wg sync.WaitGroup
	errCh := make(chan error, reqs)

	for i := 0; i < reqs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(ts.URL + "/status")
			if err != nil {
				errCh <- err
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errCh <- fmt.Errorf("status = %d, want 200", resp.StatusCode)
				return
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				errCh <- err
				return
			}
			if _, ok := body["system"]; !ok {
				errCh <- fmt.Errorf("missing system block in status response")
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent /status error: %v", err)
	}
}
