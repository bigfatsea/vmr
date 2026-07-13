// Ver 2026-07-13 04:00, by Sonnet 5
//
// Tests that every outcome of a half-open probe request releases its slot.
// Acquire hands the one probe slot of a half-open endpoint to a real
// request; every outcome of that request must release the slot (success,
// failure, neutral — including client cancel mid-probe and an
// ErrClient-classified upstream response). Missing any of these leaves
// probing=true forever and locks the endpoint out until process restart.
package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// probeUpstream is a mock with four modes: rate-limit (429 Retry-After: 1,
// drives the endpoint into a 1s cooldown), client-error (400), block (park
// until released — lets the test cancel the client mid-probe), and ok.
type probeUpstream struct {
	srv     *httptest.Server
	mode    atomic.Value  // "429" | "400" | "block" | "ok"
	entered chan struct{} // signaled when a "block" request has arrived
	release chan struct{} // closed by the test to unpark "block" handlers
}

func newProbeUpstream(t *testing.T) *probeUpstream {
	u := &probeUpstream{entered: make(chan struct{}, 4), release: make(chan struct{})}
	u.mode.Store("ok")
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch u.mode.Load() {
		case "429":
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
		case "400":
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"message":"bad request"}}`)
		case "block":
			select {
			case u.entered <- struct{}{}:
			default:
			}
			// Park until the test releases us. Waiting on r.Context() alone
			// would deadlock the cleanup: a handler that never reads the
			// request body isn't guaranteed to see the connection close as
			// a context cancellation, so httptest.Server.Close would wait
			// on this handler forever.
			select {
			case <-r.Context().Done():
			case <-u.release:
			}
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"x","model":"m","choices":[]}`)
		}
	}))
	t.Cleanup(u.srv.Close)
	t.Cleanup(func() { close(u.release) }) // runs BEFORE srv.Close (LIFO): unparks stragglers
	return u
}

func singleEndpointYAML(u string) string {
	return fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: %s, api_key: k1}
models:
  openai:
    vm:
      endpoints:
        - {provider: p1, model: model-one}
`, u)
}

// driveHalfOpen sends one rate-limited request and waits out the cooldown,
// leaving the endpoint half-open (fails=1, cooldown expired, probe slot free).
func driveHalfOpen(t *testing.T, ts *httptest.Server, u *probeUpstream) {
	t.Helper()
	u.mode.Store("429")
	if resp, _ := chat(t, ts, simpleReq, nil); resp.StatusCode != 429 {
		t.Fatalf("setup: expected the 429 passed through, got %d", resp.StatusCode)
	}
	time.Sleep(1100 * time.Millisecond) // Retry-After: 1 → half-open now
}

func TestProbeSlotReleasedOnClientCancel(t *testing.T) {
	u := newProbeUpstream(t)
	ts := newRouterServer(t, singleEndpointYAML(u.srv.URL))
	driveHalfOpen(t, ts, u)

	// The probe request: upstream parks it, then the client walks away.
	u.mode.Store("block")
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/v1/chat/completions", strings.NewReader(simpleReq))
	req.Header.Set("Content-Type", "application/json")
	go func() {
		select {
		case <-u.entered:
		case <-time.After(5 * time.Second):
		}
		cancel()
	}()
	if _, err := http.DefaultClient.Do(req); err == nil {
		t.Fatal("expected the canceled probe request to error client-side")
	}

	// The endpoint must be acquirable again: a healthy request goes through.
	u.mode.Store("ok")
	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, body := chat(t, ts, simpleReq, nil)
		if resp.StatusCode == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("endpoint locked out after canceled probe: status=%d body=%s", resp.StatusCode, body)
		}
		time.Sleep(50 * time.Millisecond) // cancel propagation to ReportNeutral may lag a tick
	}
}

func TestProbeSlotReleasedOnClientError(t *testing.T) {
	u := newProbeUpstream(t)
	ts := newRouterServer(t, singleEndpointYAML(u.srv.URL))
	driveHalfOpen(t, ts, u)

	// The probe request draws an ErrClient-classified 400 from the upstream:
	// returned to the client as-is, no failover, and — the point — the probe
	// slot must be released, because the verdict is about the request, not
	// the endpoint.
	u.mode.Store("400")
	if resp, _ := chat(t, ts, simpleReq, nil); resp.StatusCode != 400 {
		t.Fatalf("expected the 400 passed through, got %d", resp.StatusCode)
	}

	u.mode.Store("ok")
	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("endpoint locked out after ErrClient probe: status=%d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-VMR-Endpoint"); got != "openai/p1/model-one" {
		t.Errorf("expected the recovered endpoint to serve, got %q", got)
	}
}
