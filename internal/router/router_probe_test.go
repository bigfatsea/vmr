// Ver 2026-08-02 12:30, by Sonnet 5
//
// runProbe protocol dispatch test:
// Regression test: ensures Responses endpoints send appropriate probe bodies,
// preventing healthy endpoints from being misclassified and stuck half-open.
package router

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "vmr/internal/adapter/openairesponses"
)

// recordingUpstream captures the raw body of every request it receives and
// answers with a fixed 200 JSON body — enough for runProbe to record a
// success, which is all these tests need (they assert on the outbound
// probe body, not the health outcome).
func recordingUpstream(t *testing.T, captured *[]byte, respBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*captured = b
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunProbe_ResponsesProtocolSendsResponsesShapedBody(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := recordingUpstream(t, &captured, `{"id":"resp_1","model":"m","output":[]}`)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
probe_timeout: 2s
providers:
  - {name: p1, base_url: {openai-responses: `+upstream.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-responses, provider: p1, models: [model-one]}
`)
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()
	ep := snap.Models["openai-responses"]["vm"].Endpoints[0]

	rt.runProbe(ep, snap)

	if captured == nil {
		t.Fatal("probe never reached the upstream")
	}
	if bytes.Contains(captured, []byte(`"messages"`)) {
		t.Errorf("probe body must not be Chat-Completions-shaped for a Responses endpoint: %s", captured)
	}
	if !bytes.Contains(captured, []byte(`"input"`)) {
		t.Errorf("probe body must carry the top-level \"input\" field Responses requires: %s", captured)
	}
}

func TestRunProbe_ChatCompletionsProtocolUnaffected(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := recordingUpstream(t, &captured, `{"id":"x","choices":[]}`)

	cfg := mustConfig(t, `
listen: 127.0.0.1:0
probe_timeout: 2s
providers:
  - {name: p1, base_url: {openai: `+upstream.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [model-one]}
`)
	snap := mustSnapshot(t, cfg)
	rt := New(nil)
	rt.Install(snap)
	snap = rt.Snapshot()
	ep := snap.Models["openai"]["vm"].Endpoints[0]

	rt.runProbe(ep, snap)

	if captured == nil {
		t.Fatal("probe never reached the upstream")
	}
	if !bytes.Contains(captured, []byte(`"messages"`)) {
		t.Errorf("the existing Chat Completions probe shape must be unchanged: %s", captured)
	}
}
