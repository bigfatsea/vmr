// Ver 2026-07-30, by Sonnet 5
//
// Liveness tests: no upstream behavior may park a request forever.
// ResponseHeaderTimeout covers time-to-headers; these tests pin down the two
// paths after the headers arrive that also need a bound — an error body
// that stalls (blocking the whole failover walk), and a non-SSE 200 body
// that stalls (stream_idle must watch both the SSE and non-SSE body paths,
// not just SSE).
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
	t.Helper()
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
	// must give up within stream_idle and move on to u2, instead of parking
	// the whole failover walk until the client gives up.
	u1 := stallingUpstream(t, 500, "")
	u2 := newUpstream(t)
	ts := newRouterServer(t, twoEndpointYAML(u1.URL, u2.srv.URL, "timeouts: {stream_idle: 300ms}"))

	start := time.Now()
	resp, body := chat(t, ts, simpleReq, nil)
	if resp.StatusCode != 200 || resp.Header.Get("X-VMR-Endpoint") != "openai-completions/p2/model-two" {
		t.Fatalf("expected failover to p2, got status=%d ep=%s body=%s",
			resp.StatusCode, resp.Header.Get("X-VMR-Endpoint"), body)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("failover took %s; the stalled error body should be abandoned within ~stream_idle", elapsed)
	}
}

func TestNonSSEBodyStallAborts(t *testing.T) {
	// A 200 whose (non-SSE) body stalls mid-transfer: the stream_idle
	// watchdog must abort the copy so the request finishes, instead of
	// hanging until the client's own timeout.
	u := stallingUpstream(t, 200, `{"id":"x","model":"m","choices":`)
	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
timeouts: {stream_idle: 300ms}
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [model-one]}
`, u.URL)
	ts := newRouterServer(t, yaml)

	client := &http.Client{Timeout: 10 * time.Second}
	start := time.Now()
	resp, err := client.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(simpleReq))
	// After B1, a body that stalls mid-transfer is treated as a truncated
	// response: respnorm flushes the bytes it has and the handler aborts the
	// connection (http.ErrAbortHandler) so the client SDK sees a broken
	// transfer rather than a clean partial 200. Either outcome is fine here —
	// a connection error, or a 200 whose body ends early — as long as the
	// request doesn't hang past ~stream_idle.
	if err == nil {
		io.Copy(io.Discard, resp.Body) // read to the (aborted) end; must not hang
		resp.Body.Close()
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("stalled non-SSE body held the request for %s; want abort within ~stream_idle", elapsed)
	}
}

// TestBufferedTruncationAbortsAndFlushes locks in B1 end-to-end: when a
// buffered (non-SSE) 200's upstream connection dies mid-body, vmr must hand
// the client the bytes it received and then abort the connection — the
// client sees a broken transfer, not a clean empty/partial 200 — while the
// audit trail records the attempt as truncated.
func TestBufferedTruncationAbortsAndFlushes(t *testing.T) {
	partial := `{"id":"x","model":"model-one","choices":[{"message":{"content":"half a sen`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, partial)
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // kill the connection without a terminating chunk
	}))
	defer up.Close()

	yaml := fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [model-one]}
`, up.URL)
	ts, al := newAuditedServer(t, yaml)

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(simpleReq))
	if err == nil {
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		// The flushed partial bytes must reach the client (not dropped, and
		// with the model field rewritten to the virtual name)...
		if !strings.Contains(string(body), "half a sen") {
			t.Fatalf("flushed partial bytes missing: status=%d body=%q", resp.StatusCode, body)
		}
		if strings.Contains(string(body), "model-one") || !strings.Contains(string(body), `"model":"vm"`) {
			t.Errorf("model field not rewritten on the flushed partial body: %q", body)
		}
		// ...and the truncation must be visible: an aborted chunked stream
		// has no terminating chunk, so ReadAll reports an unexpected EOF
		// rather than a clean end.
		if readErr == nil {
			t.Errorf("client read the aborted response to a clean EOF — truncation was invisible (B1)")
		}
	}

	recs := readRecords(t, al)
	if len(recs) == 0 {
		t.Fatal("no audit record written")
	}
	r := recs[len(recs)-1]
	if len(r.Attempts) == 0 || r.Attempts[len(r.Attempts)-1].Error == "" {
		t.Errorf("expected the last attempt to carry a truncation error, got %+v", r.Attempts)
	}
}
