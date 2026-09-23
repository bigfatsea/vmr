// Ver 2026-09-23 01:51, by Sonnet 5

// Agent Guard's inbound mount point (M4, narrowed by ADR-15), end to end
// through Serve — reuses router_serve_test.go's mustConfig/mustSnapshot/
// newMockUpstream helpers rather than building a parallel scaffold. Every
// scenario here confirms the post-ADR-15 shape: the inbound side only ever
// changes bytes (Unicode-steganography sanitization), never the status
// code, never the stream's completion — there is no "block" path left to
// test.
package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vmr/internal/guard"

	_ "vmr/internal/adapter/openai"
)

func mustTestGuard(t *testing.T) *guard.Guard {
	t.Helper()
	eng, err := guard.NewEngine(guard.DefaultRules(), guard.RulesVersion)
	if err != nil {
		t.Fatal(err)
	}
	return guard.NewGuard(eng)
}

func guardedYAML(u string, guardBlock string) string {
	return `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: ` + u + `}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [upstream-model]}
guard:
` + guardBlock
}

// TestServe_InboundGuard_OpaqueResponseSkipsSanitizationAndPassesThrough
// confirms a genuinely compressed (opaque) response is relayed unchanged —
// ADR-15 removed the default-block policy entirely; there is no policy
// left to configure, opaque bytes just can't be sanitized so sanitization
// is skipped.
func TestServe_InboundGuard_OpaqueResponseSkipsSanitizationAndPassesThrough(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"x","choices":[]}`)
	u.hdr.Set("Content-Encoding", "br") // upstream ignored negotiation and replied compressed (K7/ADR-9)
	cfg := mustConfig(t, guardedYAML(u.srv.URL, "  inbound: {sanitize_invisible_runes: true}\n"))
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Guard = mustTestGuard(t)
	rt.Install(snap)

	w := serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`))
	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (opaque responses are never blocked post-ADR-15)", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"choices":[]`) {
		t.Errorf("body = %q, want the upstream payload relayed unchanged (nothing readable to sanitize)", w.Body.String())
	}
}

// TestServe_InboundGuard_NonOpaqueUnaffected confirms an ordinary
// (non-compressed) response with nothing to sanitize is completely
// unaffected by guard: being configured — the inbound mount point's
// pure-passthrough path.
func TestServe_InboundGuard_NonOpaqueUnaffected(t *testing.T) {
	u := newMockUpstream(t, 200, `{"id":"x","choices":[{"message":{"content":"hello"}}]}`)
	cfg := mustConfig(t, guardedYAML(u.srv.URL, "  outbound: {mode: audit_only}\n"))
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Guard = mustTestGuard(t)
	rt.Install(snap)

	w := serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "hello") {
		t.Errorf("status=%d body=%q, want the normal response untouched", w.Code, w.Body.String())
	}
}

// TestServe_InboundGuard_AbsentConfigNeverWraps confirms guard: absent from
// config means the inbound mount point never engages at all, even though
// rt.Guard is wired.
func TestServe_InboundGuard_AbsentConfigNeverWraps(t *testing.T) {
	upstreamJSON := `{"id":"x","choices":[{"message":{"content":"hello​world"}}]}`
	u := newMockUpstream(t, 200, upstreamJSON)
	cfg := mustConfig(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: `+u.srv.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [upstream-model]}
`)
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Guard = mustTestGuard(t) // wired, but guard: absent from THIS config
	rt.Install(snap)

	w := serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`))
	if w.Body.String() != upstreamJSON {
		t.Errorf("body = %q, want the upstream body byte-identical (guard: absent means zero code path, even the ZWSP survives)", w.Body.String())
	}
}

// TestServe_InboundGuard_NonStream_RuneSanitized verifies online rune
// sanitization on non-streaming responses -- the one online inbound
// capability ADR-15 left in place.
func TestServe_InboundGuard_NonStream_RuneSanitized(t *testing.T) {
	upstreamJSON := `{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"hello󠀀safe‮text"}}]}`
	u := newMockUpstream(t, 200, upstreamJSON)
	u.hdr.Set("Content-Type", "application/json")
	cfg := mustConfig(t, guardedYAML(u.srv.URL, "  inbound: {sanitize_invisible_runes: true}\n"))
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Guard = mustTestGuard(t)
	rt.Install(snap)

	w := serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "hellosafetext") {
		t.Errorf("body = %q, want runes stripped to hellosafetext", body)
	}
	if strings.Contains(body, `󠀀`) || strings.Contains(body, `‮`) {
		t.Errorf("body = %q, want invisible runes removed", body)
	}
}

// TestServe_InboundGuard_Stream_RuneSanitized is the streaming counterpart:
// an SSE response carrying an invisible rune gets it stripped before
// reaching the client, exercising the real re-framing path through Serve
// end to end (not just the guard package's own unit tests).
func TestServe_InboundGuard_Stream_RuneSanitized(t *testing.T) {
	sse := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\\u200bworld\"}}]}\n\n" +
		"data: [DONE]\n\n"
	u := newMockUpstream(t, 200, sse)
	u.hdr.Set("Content-Type", "text/event-stream")
	cfg := mustConfig(t, guardedYAML(u.srv.URL, "  inbound: {sanitize_invisible_runes: true}\n"))
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Guard = mustTestGuard(t)
	rt.Install(snap)

	w := serveReq(rt, "vm", []byte(`{"model":"vm","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 OK", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "helloworld") {
		t.Errorf("body = %q, want the ZWSP stripped to helloworld", body)
	}
	if strings.Contains(body, "​") {
		t.Errorf("body = %q, want no ZWSP left in the streamed output", body)
	}
}

// TestServe_InboundGuard_NonStream_IdleUpstreamDoesNotHangForever covers
// the independent review's finding: prepareNonStreamGuard's io.ReadAll had
// no timeout of its own, unlike the streaming path (bounded by copyFlush's
// own idle timer) -- an upstream that commits a 200 header, writes a few
// bytes, then never sends more would park the request's handler goroutine
// indefinitely once non-stream Guard sanitization is active. The fix
// reuses timeouts.stream_idle (set very short here) to bound the buffering
// read the same way copyFlush already bounds a streaming one.
func TestServe_InboundGuard_NonStream_IdleUpstreamDoesNotHangForever(t *testing.T) {
	unblock := make(chan struct{})
	defer close(unblock) // let the handler goroutine exit even if the watchdog somehow didn't fire
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"x",`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-unblock // hang past any sane test deadline -- the watchdog, not the upstream, must end this
	}))
	t.Cleanup(srv.Close)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
timeouts:
  stream_idle: 200ms
providers:
  - {name: p1, base_url: {openai-completions: `+srv.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [upstream-model]}
guard:
  inbound: {sanitize_invisible_runes: true}
`)
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Guard = mustTestGuard(t)
	rt.Install(snap)

	done := make(chan any, 1)
	go func() {
		// A watchdog-forced close makes this a genuine mid-response cut
		// (TRUNCATED), which abortIfBrokenStream (guard.go) turns into the
		// standard net/http.ErrAbortHandler panic -- the same pattern
		// stream_health_test.go's truncatingUpstream tests already use; a
		// real net/http.Server recovers this silently, serveReq's direct
		// call does not, so this goroutine must.
		defer func() { done <- recover() }()
		serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`))
	}()
	select {
	case r := <-done:
		if r != nil && r != http.ErrAbortHandler {
			t.Fatalf("recover() = %v, want nil or http.ErrAbortHandler", r)
		}
		// Either outcome proves the watchdog worked: the handler goroutine
		// didn't hang past timeouts.stream_idle.
	case <-time.After(3 * time.Second):
		t.Fatal("Serve never returned -- the non-stream guard buffering read hung past its idle timeout")
	}
}

// TestServe_InboundGuard_NonStream_WatchdogIdleReset tests that the
// non-streaming guard watchdog is an idle timeout (resets on n > 0 bytes
// read), not a total-duration deadline. A slow healthy upstream sending
// chunks at intervals < idle but with total time > idle must complete
// successfully with no TRUNCATED error, whereas a pause > idle must still truncate.
func TestServe_InboundGuard_NonStream_WatchdogIdleReset(t *testing.T) {
	const idle = 100 * time.Millisecond

	t.Run("continuous writes exceeding total idle succeed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			// Write 4 chunks with idle/2 (50ms) intervals:
			// total duration across sleeps = 150ms > idle (100ms).
			chunks := []string{
				`{"id":"x",`,
				`"model":"upstream-model",`,
				`"choices":[{"message":{"role":"assistant",`,
				`"content":"slow-but-healthy"}}]}`,
			}
			for i, ch := range chunks {
				if i > 0 {
					time.Sleep(idle / 2)
				}
				w.Write([]byte(ch))
				if flusher != nil {
					flusher.Flush()
				}
			}
		}))
		t.Cleanup(srv.Close)

		cfg := mustConfig(t, `
listen: 127.0.0.1:0
timeouts:
  stream_idle: 100ms
providers:
  - {name: p1, base_url: {openai-completions: `+srv.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [upstream-model]}
guard:
  inbound: {sanitize_invisible_runes: true}
`)
		snap := mustSnapshot(t, cfg)
		rt := New(nil)
		rt.Guard = mustTestGuard(t)
		rt.Install(snap)

		w := serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 OK", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "slow-but-healthy") {
			t.Errorf("body = %q, want slow-but-healthy", body)
		}
		key := snap.Models["openai-completions"]["vm"].Endpoints[0].HealthKey()
		st := rt.Health.Status(key, time.Now())
		if st.Fails != 0 {
			t.Errorf("endpoint had %d failures, want 0 (healthy slow stream must not be penalized)", st.Fails)
		}
	})

	t.Run("pause exceeding idle is truncated", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			w.Write([]byte(`{"id":"x",`))
			if flusher != nil {
				flusher.Flush()
			}
			// Pause exceeding idle (200ms > 100ms).
			time.Sleep(200 * time.Millisecond)
			w.Write([]byte(`"model":"upstream-model"}`))
		}))
		t.Cleanup(srv.Close)

		cfg := mustConfig(t, `
listen: 127.0.0.1:0
timeouts:
  stream_idle: 100ms
providers:
  - {name: p1, base_url: {openai-completions: `+srv.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [upstream-model]}
guard:
  inbound: {sanitize_invisible_runes: true}
`)
		snap := mustSnapshot(t, cfg)
		rt := New(nil)
		rt.Guard = mustTestGuard(t)
		rt.Install(snap)

		done := make(chan any, 1)
		go func() {
			defer func() { done <- recover() }()
			serveReq(rt, "vm", []byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`))
		}()
		select {
		case r := <-done:
			if r != http.ErrAbortHandler {
				t.Fatalf("recover() = %v, want http.ErrAbortHandler on truncated stream", r)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Serve never returned -- stalled stream was not truncated by idle watchdog")
		}

		key := snap.Models["openai-completions"]["vm"].Endpoints[0].HealthKey()
		st := rt.Health.Status(key, time.Now())
		if st.Fails != 1 {
			t.Errorf("endpoint had %d failures, want 1 (truncated stream must record transient failure)", st.Fails)
		}
	})
}
