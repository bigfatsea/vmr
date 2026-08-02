// Ver 2026-08-02, by Sonnet 5
//
// Shared mock upstream + half-open setup helper for the background-probe
// tests in server_active_probe_test.go: every outcome of a probe request
// must release its single-flight slot (success, failure, neutral —
// including client cancel mid-probe and an ErrClient-classified upstream
// response), or probing=true forever locks the endpoint out until process
// restart.
package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	hits    atomic.Int32  // total requests received, any mode — for tests that need to know a request (e.g. a background probe) landed without it being "block" mode
}

func newProbeUpstream(t *testing.T) *probeUpstream {
	t.Helper()
	u := &probeUpstream{entered: make(chan struct{}, 4), release: make(chan struct{})}
	u.mode.Store("ok")
	u.srv = newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		switch u.mode.Load() {
		case "429":
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
		case "400":
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"message":"bad request"}}`)
		case "console_go":
			// The literal body from reports/incident-20260718-console-go-400-
			// failover_Sonnet5.md: classifies as ErrEndpoint (a relay/gateway
			// reporting its own forwarding failure), not ErrClient — must
			// deepen the endpoint's cooldown via ReportFailure, unlike "400"
			// above which is a genuine bad request (ReportNeutral only).
			w.WriteHeader(400)
			fmt.Fprint(w, `{"message":"Error from provider (Console Go): Upstream request failed","type":"invalid_request_error","param":null,"code":"invalid_request_error"}`)
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
	})
	t.Cleanup(func() { close(u.release) }) // runs BEFORE srv.Close (LIFO): unparks stragglers
	return u
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
