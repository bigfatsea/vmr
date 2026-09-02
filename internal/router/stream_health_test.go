// Ver 2026-09-02 12:00, by pi-agent
//
// Health-reporting semantics of the streaming success path (forwardSuccess):
// a 200 header followed by a mid-stream cut must reach the health state
// machine as a failure, a client-side cancel must not, and the half-open
// probe slot must be released in every outcome. See TASK R01/R04/R05.
package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vmr/internal/core"
	"vmr/internal/quota"
)

// truncatingUpstream sends 200 + headers, then drops the connection
// mid-body — the relay layer's most common failure shape.
func truncatingUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server must support hijacking")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n")
		io.WriteString(conn, "data: {\"id\":\"partial\"}\n\n")
		conn.Close()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestServe_TruncatedStreamPenalizesEndpoint pins R01: an upstream that
// commits 200 and then cuts the stream must deepen the health state (the
// response is already committed, so failover can't react — the state machine
// is the only mechanism that sees this failure at all). The old code
// reported success as forwardSuccess's first statement and never corrected
// it.
func TestServe_TruncatedStreamPenalizesEndpoint(t *testing.T) {
	srv := truncatingUpstream(t)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1]}
`, srv.URL))
	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))
	snap := rt.Snapshot()
	key := snap.Models["openai-completions"]["vm"].Endpoints[0].HealthKey()

	defer func() {
		if r := recover(); r != http.ErrAbortHandler {
			t.Fatalf("recover() = %v, want http.ErrAbortHandler", r)
		}
		st := rt.Health.Status(key, time.Now())
		if st.Fails != 1 {
			t.Errorf("truncated stream: Fails=%d, want 1", st.Fails)
		}
		if st.Available {
			t.Error("truncated stream must put the endpoint into cooldown")
		}
		if st.LastError != core.ErrTransient.String() {
			t.Errorf("truncated stream: LastError=%q, want transient", st.LastError)
		}
	}()
	serveReq(rt, "vm", []byte(`{"model":"vm"}`))
	t.Fatal("forwardSuccess returned without aborting on a truncated stream")
}

// failingWriter is an http.ResponseWriter whose Write always fails — the
// client disconnected mid-stream.
type failingWriter struct{ header http.Header }

func (fw *failingWriter) Header() http.Header {
	if fw.header == nil {
		fw.header = http.Header{}
	}
	return fw.header
}
func (fw *failingWriter) WriteHeader(int) {}
func (fw *failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("connection reset by peer")
}

// TestServe_ClientCancelDoesNotPenalizeEndpoint pins the R15 distinction
// inside the new stream-outcome reporting: a client-side write failure is a
// CANCELED, not an upstream TRUNCATED — the endpoint must come out of it
// with no failure depth and no cooldown.
func TestServe_ClientCancelDoesNotPenalizeEndpoint(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"ok","model":"m1"}`)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1]}
`, u.srv.URL))
	rt := New(nil)
	rt.Install(mustSnapshot(t, cfg))
	snap := rt.Snapshot()
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vm"}`)))
	w := &failingWriter{}
	rt.Serve(w, req, &core.CanonicalRequest{Model: "vm", Raw: []byte(`{"model":"vm"}`)}, "openai-completions", nil)

	st := rt.Health.Status(ep.HealthKey(), time.Now())
	if st.Fails != 0 || st.Available == false || st.Probing {
		t.Errorf("client cancel must leave health untouched: %+v", st)
	}
	if u.hits != 1 {
		t.Errorf("upstream hits=%d, want 1", u.hits)
	}
}

// TestTryOne_CompleteStreamReleasesSlotAndClearsFails pins the regression
// half of R01/R05: a full response from a half-open endpoint clears the
// failure depth (real traffic, not a probe) and releases the probe slot in
// every case.
func TestTryOne_CompleteStreamReleasesSlotAndClearsFails(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"ok","model":"m1"}`)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1]}
`, u.srv.URL))
	snap := mustSnapshot(t, cfg)
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]
	key := ep.HealthKey()

	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()

	rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now().Add(-10*time.Second))
	if !rt.Health.Acquire(key, time.Now()) {
		t.Fatal("Acquire should succeed after expired cooldown")
	}
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	done, _, success := rt.tryOne(w, req, &core.CanonicalRequest{Model: "vm", Raw: []byte(`{}`)}, ep, snap, 1, time.Now(), nil)
	if !done || !success {
		t.Fatalf("done=%v success=%v, want true true", done, success)
	}
	st := rt.Health.Status(key, time.Now())
	if st.Fails != 0 || !st.Serving || st.Probing {
		t.Errorf("complete real stream must fully clear health: %+v", st)
	}
}

// TestTryOne_TruncatedReleasesSlotAndDeepens pins that a truncated stream
// from a half-open attempt releases the probe slot AND deepens the backoff
// from the pre-existing failure depth (fails 1 → 2).
func TestTryOne_TruncatedReleasesSlotAndDeepens(t *testing.T) {
	srv := truncatingUpstream(t)
	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: %s}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1]}
`, srv.URL))
	snap := mustSnapshot(t, cfg)
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]
	key := ep.HealthKey()

	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()

	rt.Health.ReportFailure(key, core.ErrTransient, 0, time.Now().Add(-10*time.Second))
	if !rt.Health.Acquire(key, time.Now()) {
		t.Fatal("Acquire should succeed after expired cooldown")
	}
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	func() {
		defer func() {
			if r := recover(); r != http.ErrAbortHandler {
				t.Fatalf("recover() = %v, want http.ErrAbortHandler", r)
			}
		}()
		rt.tryOne(w, req, &core.CanonicalRequest{Model: "vm", Raw: []byte(`{}`)}, ep, snap, 1, time.Now(), nil)
	}()
	st := rt.Health.Status(key, time.Now())
	if st.Fails != 2 {
		t.Errorf("truncated half-open attempt: Fails=%d, want 2 (slot released, deepened)", st.Fails)
	}
	if st.Available {
		t.Error("truncated half-open attempt must re-enter cooldown")
	}
}

// panicReader panics on the first Read, standing in for a malformed stream
// blowing up respnorm's state machine inside copyFlush's reader goroutine.
type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("malformed upstream bytes") }

// TestCopyFlush_ReaderPanicReturnedAsUpstreamError pins the R02 fix: a
// panic in the reader goroutine must come back as a plain error (NOT a
// clientWriteError, so the caller takes the TRUNCATED path) instead of
// killing the process.
func TestCopyFlush_ReaderPanicReturnedAsUpstreamError(t *testing.T) {
	w := httptest.NewRecorder()
	err := copyFlush(context.Background(), w, panicReader{}, 5*time.Second)
	if err == nil {
		t.Fatal("expected an error from a panicking reader, got nil")
	}
	if isClientWriteError(err) {
		t.Errorf("reader panic must surface as an upstream read error, not a client write error: %v", err)
	}
	if !bytes.Contains([]byte(err.Error()), []byte("panic")) {
		t.Errorf("error should mention the panic: %v", err)
	}
}

// TestRunProbe_ChargesRequestQuota pins R08: a probe consumes one real
// upstream request and must land in the quota ledger for request-metered
// accounts.
func TestRunProbe_ChargesRequestQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"VMR-PROBE-x"}}]}`)
	}))
	t.Cleanup(srv.Close)

	cfg := mustConfig(t, fmt.Sprintf(`
listen: 127.0.0.1:0
probe_timeout: 2s
providers:
  - name: p1
    base_url: {openai-completions: %s}
    api_key: k1
    quota:
      limits: [{metric: requests, every: 1mo, since: 2026-01-01, amount: 1000}]
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1]}
`, srv.URL))
	snap := mustSnapshot(t, cfg)
	ep := snap.Models["openai-completions"]["vm"].Endpoints[0]
	if ep.Quota == nil {
		t.Fatal("endpoint must carry the configured quota")
	}

	rt := New(nil)
	rt.Quota = quota.NewRegistry("")
	rt.Install(snap)
	snap = rt.Snapshot()

	rt.runProbe(ep, snap)

	l := requestsLimit(1000)
	l.Since = time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)
	ps := quota.PeriodStart(l, time.Now())
	used, _ := rt.Quota.Used("p1", "requests/1mo", ps)
	if used.Requests != 1 {
		t.Errorf("probe must charge 1 request, got %+v", used)
	}
	// And the probe slot must have been resolved: probe success decays but
	// the slot is never left held.
	if rt.Health.Status(ep.HealthKey(), time.Now()).Probing {
		t.Error("probe slot left held after runProbe")
	}
}

type chunkReader struct {
	chunks int
	data   []byte
}

func (cr *chunkReader) Read(p []byte) (int, error) {
	if cr.chunks <= 0 {
		return 0, io.EOF
	}
	cr.chunks--
	n := copy(p, cr.data)
	return n, nil
}

type nopFlushingWriter struct{}

func (nopFlushingWriter) Header() http.Header         { return http.Header{} }
func (nopFlushingWriter) Write(p []byte) (int, error) { return len(p), nil }
func (nopFlushingWriter) WriteHeader(int)             {}
func (nopFlushingWriter) Flush()                      {}

// BenchmarkCopyFlush verifies that long streams incur minimal allocations
// per chunk thanks to the double-buffering pool.
func BenchmarkCopyFlush(b *testing.B) {
	chunk := bytes.Repeat([]byte("a"), 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := &chunkReader{chunks: 500, data: chunk}
		w := &nopFlushingWriter{}
		if err := copyFlush(context.Background(), w, r, 5*time.Second); err != nil {
			b.Fatal(err)
		}
	}
}
