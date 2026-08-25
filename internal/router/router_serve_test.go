// Ver 2026-07-30, by Sonnet 5
//
// Direct unit tests for the router's Serve failover loop, copyFlush
// watchdog, parseRetryAfter, IngressPath, and 3xx redirect passthrough.
// These complement the server-package integration tests by exercising
// router internals at the unit level — no full HTTP server stack needed.
package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
)

// --- helpers ---

func mustConfig(t *testing.T, yaml string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func mustSnapshot(t *testing.T, cfg *config.Config) *Snapshot {
	t.Helper()
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// endpointFor rebuilds the *core.Endpoint a config's models.<name>.endpoints[0]
// resolves to, the same way BuildSnapshot would — used by tests that need to
// probe rt.Health directly (Available/HealthKey) rather than through Serve.
func endpointFor(t *testing.T, cfg *config.Config, protocol, virtualModel string) *core.Endpoint {
	t.Helper()
	eg := cfg.Models[virtualModel].Endpoints[0]
	p, ok := cfg.ProviderByName(eg.Providers[0])
	if !ok {
		t.Fatalf("provider %q not found", eg.Providers[0])
	}
	return &core.Endpoint{
		Provider:    eg.Providers[0],
		AdapterType: protocol,
		BaseURL:     p.BaseURL[protocol],
		APIKey:      p.APIKey,
		Model:       eg.Models[0],
	}
}

// mockUpstream is a scriptable upstream for router-level Serve tests.
type mockUpstream struct {
	srv    *httptest.Server
	hits   int
	status int
	body   string
	hdr    http.Header
}

func newMockUpstream(t *testing.T, status int, body string) *mockUpstream {
	t.Helper()
	u := &mockUpstream{status: status, body: body, hdr: http.Header{}}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits++
		for k, vs := range u.hdr {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(u.status)
		fmt.Fprint(w, u.body)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func serveReq(rt *Router, model string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	rt.Serve(w, req, &core.CanonicalRequest{Model: model, Raw: body}, "openai", nil)
	return w
}

// --- Serve: multi-endpoint failover sequence ---

func TestServe_MultiEndpointFailoverSequence(t *testing.T) {
	u1 := newMockUpstream(t, 500, `{"error":"e1"}`)
	u2 := newMockUpstream(t, 503, `{"error":"e2"}`)
	u3 := newMockUpstream(t, 200, `{"id":"ok","model":"m3"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
  - {name: p3, base_url: {openai: %s}, api_key: k3}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
      - {protocol: openai, providers: [p2], models: [m2]}
      - {protocol: openai, providers: [p3], models: [m3]}
`, u1.srv.URL, u2.srv.URL, u3.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if got := w.Header().Get("X-VMR-Endpoint"); got != "openai/p3/m3" {
		t.Errorf("endpoint=%s, want openai/p3/m3", got)
	}
	if got := w.Header().Get("X-VMR-Attempts"); got != "3" {
		t.Errorf("attempts=%s, want 3", got)
	}
	if u1.hits != 1 || u2.hits != 1 || u3.hits != 1 {
		t.Errorf("hits: u1=%d u2=%d u3=%d, want 1/1/1", u1.hits, u2.hits, u3.hits)
	}
}

// --- Serve: model not found ---

func TestServe_ModelNotFound(t *testing.T) {
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com/v1}, api_key: k}
models:
  real: {endpoints: [{protocol: openai, providers: [p1], models: [m]}]}
`)
	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "nonexistent", []byte(`{"model":"nonexistent"}`))
	if w.Code != 404 {
		t.Fatalf("status=%d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not_found_error") {
		t.Errorf("body should contain not_found_error: %s", w.Body)
	}
}

// TestServe_NoSnapshotInstalled locks in the defensive nil check added
// handling uninitialized router snapshots gracefully:
// a Router that never had Install called on it (unreachable in the real
// cmd_start.go startup sequence, which always installs before the HTTP
// server starts listening, but possible from a test or a future refactor
// mistake) must return a clean 503, not panic on snap.Models.
func TestServe_NoSnapshotInstalled(t *testing.T) {
	rt := New(nil)
	w := serveReq(rt, "whatever", []byte(`{"model":"whatever"}`))
	if w.Code != 503 {
		t.Fatalf("status=%d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "service_unavailable") {
		t.Errorf("body should contain service_unavailable: %s", w.Body)
	}
}

// --- Serve: wrong protocol hint ---

func TestServe_WrongProtocolHint(t *testing.T) {
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com/v1}, api_key: k}
  - {name: p2, base_url: {anthropic: https://example.com/v1}, api_key: k}
models:
  coding: {endpoints: [{protocol: openai, providers: [p1], models: [m]}]}
  claude: {endpoints: [{protocol: anthropic, providers: [p2], models: [m]}]}
`)
	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Request "claude" via openai protocol — should hint at anthropic.
	w := serveReq(rt, "claude", []byte(`{"model":"claude"}`))
	if w.Code != 404 {
		t.Fatalf("status=%d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "anthropic") || !strings.Contains(w.Body.String(), "/v1/messages") {
		t.Errorf("body should hint at anthropic /v1/messages: %s", w.Body)
	}
}

// --- Serve: no available endpoints (all cooling down) ---

func TestServe_NoAvailableEndpoints(t *testing.T) {
	u := newMockUpstream(t, 500, `{"error":"e"}`)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, providers: [p1], models: [m]}]}
`, u.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// First request: hits the endpoint, gets 500, cooldown starts.
	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 500 {
		t.Fatalf("first request: status=%d, want 500", w.Code)
	}

	// Second request: endpoint is in cooldown, no candidates available.
	w = serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 503 {
		t.Fatalf("second request: status=%d, want 503 (no candidates)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "vmr_no_candidates") {
		t.Errorf("body should contain vmr_no_candidates: %s", w.Body)
	}
}

// --- copyFlush: idle timeout ---

func TestCopyFlush_IdleTimeout(t *testing.T) {
	// A reader that never sends data — copyFlush must abort after the
	// idle timeout, not block forever.
	slowReader := &blockedReader{}
	w := httptest.NewRecorder()
	err := copyFlush(context.Background(), w, slowReader, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "idle") && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error should mention idle/timeout: %v", err)
	}
}

// blockedReader never returns data or EOF; it blocks forever on Read.
type blockedReader struct{}

func (br *blockedReader) Read(p []byte) (int, error) {
	time.Sleep(10 * time.Second) // far beyond any test timeout
	return 0, nil
}

// --- copyFlush: client disconnect (write error) ---

func TestCopyFlush_ClientDisconnect(t *testing.T) {
	// A ResponseWriter whose Write returns an error (simulating client
	// disconnect). copyFlush should propagate the write error.
	src := io.NopCloser(strings.NewReader(strings.Repeat("x", 10000)))
	w := &errorWriter{}
	err := copyFlush(context.Background(), w, src, 5*time.Second)
	if err == nil {
		t.Fatal("expected write error, got nil")
	}
}

// errorWriter is an http.ResponseWriter whose Write always fails.
type errorWriter struct {
	header http.Header
}

func (ew *errorWriter) Header() http.Header {
	if ew.header == nil {
		ew.header = http.Header{}
	}
	return ew.header
}
func (ew *errorWriter) WriteHeader(int) {}
func (ew *errorWriter) Write(p []byte) (int, error) {
	return 0, errors.New("connection reset by peer")
}
func (ew *errorWriter) Flush() {}

// --- parseRetryAfter ---

func TestParseRetryAfter(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", 0},
		{"seconds", "5", 5 * time.Second},
		{"zero seconds", "0", 0},
		{"negative seconds", "-1", 0},
		{"http date future", "Wed, 21 Oct 2099 07:28:00 GMT", 0}, // positive duration, just verify > 0
		{"http date past", "Wed, 21 Oct 2000 07:28:00 GMT", 0},   // past → 0
		{"garbage", "not-a-date", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			if c.value != "" {
				h.Set("Retry-After", c.value)
			}
			got := parseRetryAfter(h)
			if c.name == "http date future" {
				if got <= 0 {
					t.Errorf("future date: got %v, want > 0", got)
				}
				return
			}
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// --- IngressPath ---

func TestIngressPath(t *testing.T) {
	if got := IngressPath("openai"); got != "/v1/chat/completions" {
		t.Errorf("openai: got %q, want /v1/chat/completions", got)
	}
	if got := IngressPath("anthropic"); got != "/v1/messages" {
		t.Errorf("anthropic: got %q, want /v1/messages", got)
	}
	if got := IngressPath("openai-responses"); got != "/v1/responses" {
		t.Errorf("openai-responses: got %q, want /v1/responses", got)
	}
	// Unknown protocol defaults to the OpenAI path.
	if got := IngressPath("unknown"); got != "/v1/chat/completions" {
		t.Errorf("unknown: got %q, want /v1/chat/completions", got)
	}
}

// --- 3xx redirect passthrough ---

func TestRedirect_NotFollowed(t *testing.T) {
	redirected := newMockUpstream(t, 301, "Moved Permanently")
	redirected.hdr.Set("Location", "https://other.example.com/v1/chat/completions")

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k}
models:
  vm: {endpoints: [{protocol: openai, providers: [p1], models: [m]}]}
`, redirected.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 301 {
		t.Fatalf("status=%d, want 301 (redirect must not be followed)", w.Code)
	}
	if got := w.Header().Get("Location"); got != "https://other.example.com/v1/chat/completions" {
		t.Errorf("Location: got %q, want the upstream's redirect target", got)
	}
	if !strings.Contains(w.Body.String(), "Moved Permanently") {
		t.Errorf("body should contain upstream redirect body: %s", w.Body)
	}
	if redirected.hits != 1 {
		t.Errorf("upstream hits=%d, want 1 (no follow-up request)", redirected.hits)
	}
}

// --- Serve: all endpoints fail, last upstream error returned ---

func TestServe_AllFailReturnsLastUpstreamError(t *testing.T) {
	u1 := newMockUpstream(t, 500, `{"error":"first"}`)
	u2 := newMockUpstream(t, 429, `{"error":"rate-limited"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
      - {protocol: openai, providers: [p2], models: [m2]}
`, u1.srv.URL, u2.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	// Both endpoints fail: the last upstream error (429 from u2) is
	// returned verbatim to the client.
	if w.Code != 429 {
		t.Fatalf("status=%d, want 429 (last upstream error)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "rate-limited") {
		t.Errorf("body should contain last upstream error body: %s", w.Body)
	}
}

// --- Serve: client error (400) does not fail over ---

func TestServe_ClientErrorDoesNotFailover(t *testing.T) {
	u1 := newMockUpstream(t, 400, `{"error":{"message":"invalid request"}}`)
	u2 := newMockUpstream(t, 200, `{"id":"ok"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
      - {protocol: openai, providers: [p2], models: [m2]}
`, u1.srv.URL, u2.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 400 {
		t.Fatalf("status=%d, want 400 (client error returned as-is)", w.Code)
	}
	if u2.hits != 0 {
		t.Errorf("u2 hits=%d, want 0 (client errors must not failover)", u2.hits)
	}
}

// --- Serve: content-policy rejection fails over without cooldown ---

func TestServe_ContentFlagFailsOverWithoutCooldown(t *testing.T) {
	u1 := newMockUpstream(t, 400, `{"error":{"message":"content_filter"}}`)
	u2 := newMockUpstream(t, 200, `{"id":"ok","model":"m2"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
      - {protocol: openai, providers: [p2], models: [m2]}
`, u1.srv.URL, u2.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200 (content flag should failover to u2)", w.Code)
	}
	if got := w.Header().Get("X-VMR-Endpoint"); got != "openai/p2/m2" {
		t.Errorf("endpoint=%s, want openai/p2/m2", got)
	}
	// u1 must still be available (no cooldown applied for content flags).
	endpoint := endpointFor(t, cfg, "openai", "vm")
	if !rt.Health.Available(endpoint.HealthKey(), time.Now()) {
		t.Error("u1 should still be available after a content flag (no cooldown)")
	}
}

// --- Serve: vendor protocol-quirk rejection fails over without cooldown ---

// TestServe_VendorQuirkFailsOverWithoutCooldown pins the 2026-08-25
// incident's dominant failure mode: a healthy endpoint rejecting a
// conversation whose history lacks the previous turn's reasoning_content
// (DeepSeek thinking-mode) must fail over to the next candidate WITHOUT
// cooling the endpoint down — the history shape is wrong for THIS vendor's
// rules, which says nothing about endpoint health. The literal body from
// audit line 513.
func TestServe_VendorQuirkFailsOverWithoutCooldown(t *testing.T) {
	u1 := newMockUpstream(t, 400, "The `reasoning_content` in the thinking mode must be passed back to the API.")
	u2 := newMockUpstream(t, 200, `{"id":"ok","model":"m2"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
      - {protocol: openai, providers: [p2], models: [m2]}
`, u1.srv.URL, u2.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200 (quirk rejection should failover to u2)", w.Code)
	}
	if got := w.Header().Get("X-VMR-Endpoint"); got != "openai/p2/m2" {
		t.Errorf("endpoint=%s, want openai/p2/m2", got)
	}
	// u1 must still be available (no cooldown applied for quirk rejections).
	endpoint := endpointFor(t, cfg, "openai", "vm")
	if !rt.Health.Available(endpoint.HealthKey(), time.Now()) {
		t.Error("u1 should still be available after a quirk rejection (no cooldown)")
	}
}

// --- Serve: context-window overflow fails over without cooldown ---

// TestServe_ContextLimitFailsOverWithoutCooldown pins P0-B: a long-context
// request that overflows an endpoint's context window must still failover
// to a candidate with more room, and must not cool the overflowing
// endpoint down (its window size is a static property, not evidence it's
// unhealthy) — the mirror image of TestServe_ContentFlagFailsOverWithoutCooldown.
func TestServe_ContextLimitFailsOverWithoutCooldown(t *testing.T) {
	u1 := newMockUpstream(t, 400, `{"error":{"message":"This model's maximum context length is 8192 tokens. However, you requested 10000 tokens (9000 in the messages, 1000 in the completion). Please reduce the length of the messages or completion.","type":"invalid_request_error","code":"context_length_exceeded"}}`)
	u2 := newMockUpstream(t, 200, `{"id":"ok","model":"m2"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
  - {name: p2, base_url: {openai: %s}, api_key: k2}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
      - {protocol: openai, providers: [p2], models: [m2]}
`, u1.srv.URL, u2.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	w := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w.Code != 200 {
		t.Fatalf("status=%d, want 200 (context-limit rejection should failover to u2)", w.Code)
	}
	if got := w.Header().Get("X-VMR-Endpoint"); got != "openai/p2/m2" {
		t.Errorf("endpoint=%s, want openai/p2/m2", got)
	}
	// u1 must still be available (no cooldown applied for context-limit rejections).
	endpoint := endpointFor(t, cfg, "openai", "vm")
	if !rt.Health.Available(endpoint.HealthKey(), time.Now()) {
		t.Error("u1 should still be available after a context-limit rejection (no cooldown)")
	}
}

// --- Serve: BuildRequest failure does not cool the endpoint down ---

// TestServe_BuildErrorDoesNotCooldownEndpoint locks in tryOne's build-error
// branch reporting ReportNeutral (not ReportFailure) to health, matching
// runProbe's treatment of the same error class: a malformed client body is
// vmr/the client's problem, not the endpoint's, and must not cool the
// endpoint down for every other client's traffic.
func TestServe_BuildErrorDoesNotCooldownEndpoint(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"ok"}`)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
`, u.srv.URL))

	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))

	// Not valid JSON at all — jsonscan.RewriteModel's fallback path fails to
	// unmarshal it, so OpenAI.BuildRequest returns an error before any HTTP
	// call is made (u.hits stays 0).
	w := serveReq(rt, "vm", []byte(`not json`))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503 (build error, no upstream response to return)", w.Code)
	}
	if u.hits != 0 {
		t.Errorf("upstream hits=%d, want 0 (BuildRequest must fail before any HTTP call)", u.hits)
	}

	endpoint := endpointFor(t, cfg, "openai", "vm")
	if !rt.Health.Available(endpoint.HealthKey(), time.Now()) {
		t.Error("endpoint should still be available after a build error (no cooldown)")
	}

	// A subsequent, well-formed request must reach the upstream normally —
	// proof the endpoint was never actually cooled down, not just that
	// Available() says so in isolation.
	w2 := serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	if w2.Code != http.StatusOK {
		t.Fatalf("second request status=%d, want 200", w2.Code)
	}
	if u.hits != 1 {
		t.Errorf("upstream hits=%d, want 1 (second request should reach the upstream)", u.hits)
	}
}

// --- copyFlush: normal data flows through ---

func TestCopyFlush_NormalData(t *testing.T) {
	data := "Hello, world!\n"
	src := io.NopCloser(strings.NewReader(data))
	w := httptest.NewRecorder()
	err := copyFlush(context.Background(), w, src, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Body.String() != data {
		t.Errorf("body=%q, want %q", w.Body.String(), data)
	}
}

// --- context cancellation during copyFlush ---

func TestCopyFlush_ContextCanceledMidStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := &slowReader{delay: 2 * time.Second}
	w := httptest.NewRecorder()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := copyFlush(ctx, w, src, 5*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

type slowReader struct {
	delay time.Duration
}

func (sr *slowReader) Read(p []byte) (int, error) {
	time.Sleep(sr.delay)
	return 0, io.EOF
}
