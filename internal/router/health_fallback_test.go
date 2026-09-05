// Ver 2026-09-05, by Sonnet 5

// Tests for the health-filter last-resort fallback (T2-b, see
// docs/KNOWN_ISSUES.md §2.85): when every endpoint for a virtual model is
// either hard-cooling or merely half-open, buildCandidates releases the
// shallowest-backoff half-open one as a real candidate instead of failing
// the request locally with a synthetic 503 — see candidates.go's
// healthFilter doc comment for the full mechanism.
package router

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vmr/internal/core"
)

func TestHealthFallback_SingleEndpointRecovers(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"ok"}`)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k}
models:
  vm: {endpoints: {openai-completions: [{providers: [p1], models: [m]}]}}
`, u.srv.URL))

	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]
	key := ep.HealthKey()

	// Half-open: failed once before, cooldown already expired, never re-verified.
	rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now().Add(-10*time.Second))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200 (the last-resort candidate should have let the real request through)", w.Code)
	}
	if u.hits != 1 {
		t.Fatalf("upstream hits=%d, want 1", u.hits)
	}
	if reason := w.Header().Get("X-VMR-Route-Reason"); !strings.Contains(reason, "health_fallback=1") {
		t.Errorf("X-VMR-Route-Reason=%q, want it to contain health_fallback=1", reason)
	}
	if got := rt.Health.Status(key, time.Now()).Fails; got != 0 {
		t.Errorf("Fails=%d after the real success, want 0 (ReportSuccess clears outright, unlike the probe path's one-step decay)", got)
	}
}

func TestHealthFallback_PicksShallowestFails(t *testing.T) {
	uShallow := newMockUpstream(t, 200, `{"id":"shallow"}`)
	uDeep := newMockUpstream(t, 200, `{"id":"deep"}`)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: pshallow, base_url: {openai-completions: %s}, api_key: k}
  - {name: pdeep, base_url: {openai-completions: %s}, api_key: k}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [pshallow], models: [m]}
        - {providers: [pdeep], models: [m]}
`, uShallow.srv.URL, uDeep.srv.URL))

	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()
	route := snap.Models["openai-completions"]["vm"]
	epShallow, epDeep := route.Endpoints[0], route.Endpoints[1]

	past := time.Now().Add(-time.Hour) // cooldown must be long-expired for both, at every backoff depth below
	rt.Health.ReportFailure(epShallow.HealthKey(), core.ErrTransient, 0, past)
	for i := 0; i < 3; i++ {
		rt.Health.ReportFailure(epDeep.HealthKey(), core.ErrTransient, 0, past)
	}

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if got := w.Header().Get("X-VMR-Endpoint"); !strings.Contains(got, "pshallow") {
		t.Errorf("X-VMR-Endpoint=%q, want the shallower-backoff endpoint (pshallow)", got)
	}
	if uShallow.hits != 1 {
		t.Errorf("shallow upstream hits=%d, want 1 (should have been the last-resort pick)", uShallow.hits)
	}
	// uDeep still gets its own background probe dispatched (unchanged
	// behavior — only the single last-resort pick skips one), so a hit count
	// there isn't asserted: it would race the goroutine. The X-VMR-Endpoint
	// check above already establishes the real request went to pshallow, not
	// pdeep — that's the invariant this test exists for.
	_ = uDeep
}

func TestHealthFallback_NotUsedWhenHealthyEndpointExists(t *testing.T) {
	uHealthy := newMockUpstream(t, 200, `{"id":"healthy"}`)
	uHalfOpen := newMockUpstream(t, 200, `{"id":"halfopen"}`)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: phealthy, base_url: {openai-completions: %s}, api_key: k}
  - {name: phalf, base_url: {openai-completions: %s}, api_key: k}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [phealthy], models: [m]}
        - {providers: [phalf], models: [m]}
`, uHealthy.srv.URL, uHalfOpen.srv.URL))

	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()
	route := snap.Models["openai-completions"]["vm"]
	epHalf := route.Endpoints[1]
	rt.Health.ReportFailure(epHalf.HealthKey(), core.ErrTransient, 0, time.Now().Add(-time.Hour))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	if got := w.Header().Get("X-VMR-Endpoint"); !strings.Contains(got, "phealthy") {
		t.Errorf("X-VMR-Endpoint=%q, want the already-healthy endpoint (phealthy)", got)
	}
	if reason := w.Header().Get("X-VMR-Route-Reason"); strings.Contains(reason, "health_fallback=1") {
		t.Errorf("health_fallback must not fire while a healthy endpoint exists: reason=%q", reason)
	}
	// uHalfOpen's own background probe (still dispatched normally) may or may
	// not have completed by now — not asserted here to avoid a timing race;
	// the point of this test is that the REAL request never had a chance to
	// pick phalf, which the route-reason and endpoint-header checks above
	// already establish.
	_ = uHalfOpen
}
