// Ver 2026-07-30, by Sonnet 5
//
// probe_mode: active — the goal this whole mode exists for is that real
// client traffic never waits on, and is never diverted for longer than a
// heartbeat by, another endpoint's recovery check (see docs/
// ActiveProbeAndFailoverFix_Sonnet5.md and reports/incident-20260718-
// console-go-400-failover_Sonnet5.md §2.4). These tests pin that contract
// down the same way server_probe_test.go pins the passive contract.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// driveHalfOpenViaFailover is driveHalfOpen's two-endpoint counterpart: with
// a healthy second candidate present, u's 429 fails over instead of passing
// through, so the setup request's own status isn't asserted — only that u
// ends up half-open (fails=1, cooldown expired) afterward.
func driveHalfOpenViaFailover(t *testing.T, ts *httptest.Server, u *probeUpstream) {
	t.Helper()
	u.mode.Store("429")
	chat(t, ts, simpleReq, nil)
	time.Sleep(1100 * time.Millisecond) // Retry-After: 1 → half-open now
}

// TestActiveProbe_HalfOpenEndpointExcludedFromRealTraffic: a single-endpoint
// route where that one endpoint is half-open and currently parked (would
// hang if a real request reached it). The real request must come back fast
// with "no candidates" rather than hang waiting on — or being served
// through — the half-open endpoint. active is the config default, so no
// probe_mode override is set here on purpose (this is what a default install
// does).
func TestActiveProbe_HalfOpenEndpointExcludedFromRealTraffic(t *testing.T) {
	t.Parallel()
	u := newProbeUpstream(t)
	ts := newRouterServer(t, fmt.Sprintf(`
listen: 127.0.0.1:0
probe_timeout: 200ms
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [model-one]}
`, u.srv.URL))
	driveHalfOpen(t, ts, u) // leaves fails=1, cooldown expired (passive contract, unrelated to probe_mode)

	u.mode.Store("block") // if real traffic reached p1, this would hang until released/timed out

	start := time.Now()
	resp, _ := chat(t, ts, simpleReq, nil)
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503 (no candidates — the only endpoint is half-open)", resp.StatusCode)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("request took %s — it must return immediately, not wait on the half-open endpoint's probe (probe_timeout=200ms)", elapsed)
	}

	select {
	case <-u.entered:
		// good: the real request's arrival triggered a background probe.
	case <-time.After(1 * time.Second):
		t.Error("no probe request reached the half-open endpoint — active mode should have launched one")
	}
}

// TestActiveProbe_RealTrafficUnaffectedByBackgroundProbeLatency: two
// endpoints, p1 half-open and parked (slow/hung), p2 healthy. A real request
// must be served fast by p2 — never diverted for as long as p1's background
// probe takes to resolve.
func TestActiveProbe_RealTrafficUnaffectedByBackgroundProbeLatency(t *testing.T) {
	t.Parallel()
	u1 := newProbeUpstream(t)
	u2 := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, "probe_timeout: 300ms"))
	driveHalfOpenViaFailover(t, ts, u1) // (not driveHalfOpen: with p2 present, p1's 429 fails over to p2 rather than passing through)

	u1.mode.Store("block")

	start := time.Now()
	resp, _ := chat(t, ts, simpleReq, nil)
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want 200 via p2", resp.StatusCode)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p2/model-two" {
		t.Errorf("endpoint=%s, want p2 (p1 is half-open and must be skipped, not waited on)", got)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("request took %s — real traffic must not be slowed by p1's 300ms probe timeout", elapsed)
	}
	if u2.hits.Load() != 2 { // 1 from driveHalfOpenViaFailover's setup request, 1 from this one
		t.Errorf("p2 hits=%d, want 2", u2.hits.Load())
	}

	select {
	case <-u1.entered:
	case <-time.After(1 * time.Second):
		t.Error("no background probe reached p1")
	}
}

// TestActiveProbe_RecoversInBackgroundThenServesRealTraffic is the
// end-to-end story: p1 goes half-open, a real request lands on p2 instead
// (proving p1 wasn't used) while triggering a background probe of p1 that
// succeeds; once that probe lands, a later real request is served by p1
// again — recovery happened off the request path, not by any client "being"
// the probe.
func TestActiveProbe_RecoversInBackgroundThenServesRealTraffic(t *testing.T) {
	t.Parallel()
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(429)
	u1.retryAfter = "1"
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, "probe_timeout: 2s"))

	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("setup: status=%d ep=%s", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
	time.Sleep(1100 * time.Millisecond) // Retry-After: 1 → half-open now
	u1.status.Store(200)                // p1 would succeed if actually tried

	// This request must still land on p2 — p1 is half-open, active mode
	// never lets real traffic touch it directly, no matter how fast p1
	// would itself have answered.
	resp, _ = chat(t, ts, simpleReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p2/model-two" {
		t.Fatalf("endpoint=%s, want p2 (p1 must stay excluded until its background probe reports success)", got)
	}

	// The background probe launched by that request should reach p1 shortly
	// (hits goes 1 -> 2: the first hit was the original 429 that drove it
	// into cooldown).
	deadline := time.Now().Add(2 * time.Second)
	for u1.hits.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("background probe never reached p1 (hits=%d)", u1.hits.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Now p1 must be back in rotation and win by priority (p1: priority 1 <
	// p2: priority 2) exactly as passive recovery would have — the only
	// difference is which request got there first.
	deadline = time.Now().Add(2 * time.Second)
	for {
		resp, _ = chat(t, ts, simpleReq, nil)
		if resp.Header.Get("X-VMR-Endpoint") == "openai/p1/model-one" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("p1 never recovered after its background probe succeeded, last endpoint=%s", resp.Header.Get("X-VMR-Endpoint"))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestActiveProbe_FailedProbeReleasesSlot mirrors server_probe_test.go's
// passive-mode probe-slot-release tests, for the async path: a probe that
// draws an ErrClient-classified response must still resolve via
// ReportNeutral, or the endpoint would stay "probing" forever and never be
// probed again.
func TestActiveProbe_FailedProbeReleasesSlot(t *testing.T) {
	t.Parallel()
	u1 := newProbeUpstream(t)
	u2 := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, "probe_timeout: 2s"))
	driveHalfOpenViaFailover(t, ts, u1)

	u1.mode.Store("400") // ErrClient-classified: request-specific, no cooldown, but the probe slot must still be released
	resp, _ := chat(t, ts, simpleReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p2/model-two" {
		t.Fatalf("endpoint=%s, want p2 while p1's probe is/was in flight", got)
	}

	deadline := time.Now().Add(2 * time.Second)
	for u1.hits.Load() < 2 { // 1 = the setup 429, 2 = the background probe hitting "400" mode
		if time.Now().After(deadline) {
			t.Fatalf("background probe never reached p1 (hits=%d)", u1.hits.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The endpoint must be probeable again immediately — ReportNeutral, not
	// a fresh failure, so this isn't gated behind a new cooldown.
	u1.mode.Store("ok")
	deadline = time.Now().Add(2 * time.Second)
	for {
		resp, _ = chat(t, ts, simpleReq, nil)
		if resp.Header.Get("X-VMR-Endpoint") == "openai/p1/model-one" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("p1 locked out after its probe drew a client-classified error, last endpoint=%s", resp.Header.Get("X-VMR-Endpoint"))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestActiveProbe_UpstreamFailureGoesToReportFailure closes the one gap the
// other active-probe tests leave open: TestActiveProbe_FailedProbeReleasesSlot
// only proves the ErrClient/ReportNeutral branch of runProbe's status>=400
// handling. This test drives the *other* branch — a relay/gateway reporting
// its own forwarding failure (the literal body from reports/incident-
// 20260718-console-go-400-failover_Sonnet5.md, classified ErrEndpoint by
// classify.go's upstreamHint) — and asserts the probe goroutine calls
// ReportFailure, not ReportNeutral: the fail count actually increases and the
// endpoint gets ErrEndpoint's long cooldown. Without this, a future edit that
// widened runProbe's "no cooldown" branch (e.g. adding ErrEndpoint to the
// `class == core.ErrContent || class == core.ErrClient` check) would silently
// stop cooling down an endpoint that keeps failing this way, and nothing
// here or in TestUpstreamGatewayFailureContinuesFailover (server_test.go,
// which covers the same body on the *synchronous* tryOne path) would catch
// it.
func TestActiveProbe_UpstreamFailureGoesToReportFailure(t *testing.T) {
	t.Parallel()
	u1 := newProbeUpstream(t)
	u2 := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, "probe_timeout: 2s"))
	driveHalfOpenViaFailover(t, ts, u1) // fails=1 (429/rate_limit), cooldown expired

	u1.mode.Store("console_go")
	resp, _ := chat(t, ts, simpleReq, nil)
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p2/model-two" {
		t.Fatalf("endpoint=%s, want p2 while p1's probe is/was in flight", got)
	}

	deadline := time.Now().Add(2 * time.Second)
	for u1.hits.Load() < 2 { // 1 = the setup 429, 2 = the background probe hitting "console_go" mode
		if time.Now().After(deadline) {
			t.Fatalf("background probe never reached p1 (hits=%d)", u1.hits.Load())
		}
		time.Sleep(20 * time.Millisecond)
	}

	statusResp, err := http.Get(ts.URL + "/admin/status")
	if err != nil {
		t.Fatal(err)
	}
	defer statusResp.Body.Close()
	var out struct {
		Models map[string][]struct {
			Endpoint      string    `json:"endpoint"`
			Available     bool      `json:"available"`
			Fails         int       `json:"consecutive_failures"`
			LastError     string    `json:"last_error"`
			CooldownUntil time.Time `json:"cooldown_until"`
		} `json:"models"`
	}
	if err := json.NewDecoder(statusResp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	var p1 *struct {
		Endpoint      string    `json:"endpoint"`
		Available     bool      `json:"available"`
		Fails         int       `json:"consecutive_failures"`
		LastError     string    `json:"last_error"`
		CooldownUntil time.Time `json:"cooldown_until"`
	}
	for i, ep := range out.Models["vm [openai]"] {
		if ep.Endpoint == "openai/p1/model-one" {
			p1 = &out.Models["vm [openai]"][i]
		}
	}
	if p1 == nil {
		t.Fatal("p1 missing from /admin/status")
	}
	// fails must have grown past the initial rate-limit failure (proves
	// ReportFailure ran, not ReportNeutral, which would have left fails
	// untouched or reset by an intervening success) and last_error must be
	// "endpoint" (core.ErrEndpoint.String()), not "client".
	if p1.Available || p1.Fails < 2 || p1.LastError != "endpoint" {
		t.Errorf("p1 health = %+v, want cooling down, fails>=2, last_error=endpoint", p1)
	}
	if wantMin := time.Now().Add(5 * time.Minute); p1.CooldownUntil.Before(wantMin) {
		t.Errorf("p1 cooldown_until=%s, want at least 5min out (ErrEndpoint's long cooldown — a short one would mean the probe's failure was misclassified or under-penalized)", p1.CooldownUntil)
	}
}
