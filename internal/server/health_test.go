// Ver 2026-08-15 23:40, by Opus 5

// GET /health — the unauthenticated liveness endpoint. The tests that
// matter here are the negative ones: that it stays reachable without a
// credential and from a non-loopback address (otherwise it cannot serve a
// container probe at all), and that its body never grows an instance
// detail (otherwise it becomes an unauthenticated /status).
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

const healthYAML = `
listen: 127.0.0.1:18801
api_keys: [test-key-0123456789]
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
`

func healthServer(t *testing.T) *Server {
	t.Helper()
	cfg, err := config.Parse([]byte(healthYAML))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	return New(rt, nil).WithInstance("/etc/vmr/config.yaml", time.Now().Add(-90*time.Second))
}

// getHealth drives one request through the real mux at remoteAddr, so the
// loopback question is actually exercised rather than assumed.
func getHealth(t *testing.T, s *Server, remoteAddr string) (*http.Response, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("GET", "/health", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	resp := w.Result()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /health body: %v", err)
	}
	return resp, body
}

// api_keys is configured in healthYAML, and no request here sends one: a
// probe has no credential to present, so a 401 would make the endpoint
// useless for the job it exists to do.
func TestHealth_NoAuthRequired(t *testing.T) {
	resp, body := getHealth(t, healthServer(t), "127.0.0.1:5555")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %v, want ok", body["status"])
	}
}

// /health is reachable without credentials from any network; /status enforces
// auth when api_keys is configured.
func TestHealth_NonLoopbackAllowed(t *testing.T) {
	resp, _ := getHealth(t, healthServer(t), "10.244.3.17:41000")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d from non-loopback, want 200", resp.StatusCode)
	}
	// The contrast that gives the previous line its meaning.
	req := httptest.NewRequest("GET", "/status", nil)
	req.RemoteAddr = "10.244.3.17:41000"
	w := httptest.NewRecorder()
	healthServer(t).Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("/status from non-loopback without key = %d, want 401", w.Code)
	}
}

// A fixed body is indistinguishable from a cached one — the moving fields
// are the whole reason this endpoint returns JSON instead of "ok".
func TestHealth_BodyMovesAndIsNotCacheable(t *testing.T) {
	s := healthServer(t)
	resp, body := getHealth(t, s, "127.0.0.1:5555")
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	ts, ok := body["time"].(string)
	if !ok {
		t.Fatalf("time field = %v (%T), want an RFC3339 string", body["time"], body["time"])
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("time field %q does not parse as RFC3339: %v", ts, err)
	}
	// WithInstance backdated startedAt by 90s, but /health measures from
	// New() — so this only asserts the field is present and sane, not that
	// it tracks inst.startedAt (it deliberately doesn't).
	up, ok := body["uptime_seconds"].(float64)
	if !ok || up < 0 {
		t.Errorf("uptime_seconds = %v, want a non-negative number", body["uptime_seconds"])
	}
}

// The guard that matters most: this endpoint is unauthenticated and
// reachable from anywhere, so every field in it is public. /status
// stays auth-gated precisely because it answers questions this one must
// never answer — adding any of them here silently removes that protection.
// If you are here because this test failed, the fix is almost certainly to
// put your new field in /status instead.
func TestHealth_LeaksNoInstanceDetail(t *testing.T) {
	_, body := getHealth(t, healthServer(t), "203.0.113.9:41000")
	want := map[string]bool{"status": true, "time": true, "uptime_seconds": true}
	for k := range body {
		if !want[k] {
			t.Errorf("unexpected field %q in /health body: this endpoint is public", k)
		}
	}
	for k := range want {
		if _, ok := body[k]; !ok {
			t.Errorf("missing field %q in /health body", k)
		}
	}
}
