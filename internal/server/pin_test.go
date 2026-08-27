// Ver 2026-08-23, by Gemini

// End-to-end proof that the routing-pin headers (X-VMR-Provider /
// X-VMR-Target-Model) steer a request onto a specific backend through the
// live router. The router package unit-tests the filter itself; what can
// only be verified here is the full path: raw request header → blocklist
// (must NOT leak upstream) → Serve → pinned candidate → X-VMR-Endpoint
// reflecting the pin, overriding the priority/order normal routing would
// have picked.
package server

import (
	"net/http"
	"strings"
	"testing"
)

// fourEndpointYAML has p1..p4 all at implicit priority 0 (config order).
// For pin tests we want a clear "normal routing would pick p1, but the pin
// forces p2" shape — use a fixture where p1 outranks p2, then pin p2.
func pinYAML(u1, u2 string) string {
	return `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: ` + u1 + `}, api_key: k1}
  - {name: p2, base_url: {openai-completions: ` + u2 + `}, api_key: k2}
models:
  vm:
    sticky: false
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1], priority: 1}
      - {protocol: openai-completions, providers: [p2], models: [m2], priority: 2}
`
}

// Pin by provider beats the higher-priority endpoint.
func TestPinProviderOverridesPriority(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, pinYAML(u1.srv.URL, u2.srv.URL))

	// No pin: normal routing picks p1 (priority 1).
	resp, _ := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai-completions/p1/m1" {
		t.Fatalf("unpinned: status=%d ep=%s, want p1", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}

	// Pin provider p2: must force p2 despite lower priority.
	resp, body := chat(t, ts, simpleReq, map[string]string{"X-VMR-Provider": "p2"})
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai-completions/p2/m2" {
		t.Fatalf("pinned: status=%d body=%s ep=%s, want p2", resp.StatusCode, body, resp.Header.Get("X-VMR-Endpoint"))
	}
	if got := resp.Header.Get("X-VMR-Route-Reason"); !strings.Contains(got, "pin=provider=p2") {
		t.Errorf("X-VMR-Route-Reason = %q, want pin=provider=p2", got)
	}
	if u1.hits.Load() != 1 {
		t.Errorf("p1 hits = %d, want 1 (only the unpinned request)", u1.hits.Load())
	}
	if u2.hits.Load() != 1 {
		t.Errorf("p2 hits = %d, want 1", u2.hits.Load())
	}
}

// Pin by target model selects the exact upstream model.
func TestPinTargetModel(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, pinYAML(u1.srv.URL, u2.srv.URL))

	resp, _ := chat(t, ts, simpleReq, map[string]string{"X-VMR-Target-Model": "m2"})
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai-completions/p2/m2" {
		t.Fatalf("model pin: status=%d ep=%s, want p2/m2", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
}

// Both axes together select the intersection.
func TestPinProviderAndModel(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, pinYAML(u1.srv.URL, u2.srv.URL))

	resp, _ := chat(t, ts, simpleReq, map[string]string{"X-VMR-Provider": "p2", "X-VMR-Target-Model": "m2"})
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai-completions/p2/m2" {
		t.Fatalf("both pin: status=%d ep=%s, want p2/m2", resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"))
	}
}

// A pin matching nothing fails with the pin named, and tries nothing.
func TestPinNoMatch(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, pinYAML(u1.srv.URL, u2.srv.URL))

	resp, body := chat(t, ts, simpleReq, map[string]string{"X-VMR-Provider": "nope"})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("no-match pin: status=%d, want 503", resp.StatusCode)
	}
	if !strings.Contains(body, "pin=provider=nope") {
		t.Errorf("body = %q, want pin named", body)
	}
	if u1.hits.Load() != 0 || u2.hits.Load() != 0 {
		t.Errorf("no-match pin must not try any endpoint: p1=%d p2=%d", u1.hits.Load(), u2.hits.Load())
	}
}

// The pin headers must never reach the upstream — they're internal control
// headers, consumed by the router and stripped by the blocklist.
func TestPinHeadersDoNotLeakUpstream(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	ts := newRouterServer(t, pinYAML(u1.srv.URL, u2.srv.URL))

	_, _ = chat(t, ts, simpleReq, map[string]string{"X-VMR-Provider": "p2", "X-VMR-Target-Model": "m2"})
	hdrs, _ := u2.lastHeaders.Load().(http.Header)
	if hdrs == nil {
		t.Fatal("no headers captured by upstream")
	}
	for _, k := range []string{"X-VMR-Provider", "X-VMR-Target-Model", "x-vmr-provider", "x-vmr-target-model"} {
		if hdrs.Get(k) != "" {
			t.Errorf("upstream received internal pin header %q = %q", k, hdrs.Get(k))
		}
	}
}
