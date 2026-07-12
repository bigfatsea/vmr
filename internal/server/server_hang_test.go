// Ver 2026-07-08 22:00, by Fable 5
//
// Liveness regression tests: no upstream behavior may park a request forever.
// ResponseHeaderTimeout covers time-to-headers; these tests pin down the two
// paths that used to have no timeout at all after the headers arrived —
// an error body that stalls (blocking the whole failover walk), and a
// non-SSE 200 body that stalls (the SSE path had the stream_idle watchdog,
// the io.Copy path had none).
package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stallingUpstream writes headers (and optionally a body prefix), flushes,
// then parks until the test tears it down.
func stallingUpstream(t *testing.T, status int, bodyPrefix string) *httptest.Server {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if bodyPrefix != "" {
			fmt.Fprint(w, bodyPrefix)
		}
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(srv.Close)                 // runs second (LIFO)
	t.Cleanup(func() { close(release) }) // runs first: unparks handlers so Close returns
	return srv
}

func TestErrorBodyStallStillFailsOver(t *testing.T) {
	// u1 sends 500 headers then stalls the error body. The error-body read
	// must give up within stream_idle and move on to u2 — before the fix it
	// parked the whole failover walk until the client gave up.
	u1 := stallingUpstream(t, 500, "")
	u2 := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u1.URL, u2.srv.URL, "timeouts: {stream_idle: 300ms}"))

	start := time.Now()
	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai/p2/model-two" {
		t.Fatalf("expected failover to p2, got status=%d ep=%s body=%s",
			resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"), body)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("failover took %s; the stalled error body should be abandoned within ~stream_idle", elapsed)
	}
}

func TestNonSSEBodyStallAborts(t *testing.T) {
	// A 200 whose (non-SSE) body stalls mid-transfer: the stream_idle
	// watchdog must abort the copy so the request finishes — before the fix
	// the io.Copy path had no watchdog and the request hung until the
	// client's own timeout.
	u := stallingUpstream(t, 200, `{"id":"x","model":"m","choices":`)
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
timeouts: {stream_idle: 300ms}
providers:
  openai:
    p1: {base_url: %s, api_key: k1}
models:
  openai:
    vm:
      endpoints:
        - {provider: p1, model: model-one}
`, u.URL)
	ts := newRouterServer(t, yaml)

	client := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := client.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(simpleReq))
	if err != nil {
		t.Fatalf("request itself failed: %v", err)
	}
	io.Copy(io.Discard, resp.Body) // read to the (aborted) end; must not hang
	resp.Body.Close()
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("stalled non-SSE body held the request for %s; want abort within ~stream_idle", elapsed)
	}
}
