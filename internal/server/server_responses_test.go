// Ver 2026-08-02 12:30, by Sonnet 5
//
// End-to-end coverage for the POST /v1/responses ingress route: proves the
// third protocol is wired all the way from the HTTP entry point through
// chatHandler/router.Serve/the openai-responses Adapter to a real (mock)
// upstream and back, the same level newUpstream/chat already cover for
// POST /v1/chat/completions.
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "vmr/internal/adapter/openairesponses"
)

// postResponses is chat's POST /v1/responses counterpart.
func postResponses(t *testing.T, ts *httptest.Server, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

// newResponsesUpstream is newUpstream's Responses-shaped counterpart: reads
// the top-level "model" field the same way (it's still a top-level key,
// same as Chat Completions — see adapter.RewriteModel), but answers with a
// Responses-shaped body (top-level "output", no "choices") and records the
// raw request body so tests can assert on "input" vs "messages".
type responsesUpstream struct {
	srv        *httptest.Server
	lastModel  string
	lastRawReq []byte
}

func newResponsesUpstream(t *testing.T) *responsesUpstream {
	t.Helper()
	u := &responsesUpstream{}
	u.srv = newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.lastRawReq = body
		u.lastModel = extractModelField(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"resp_1","object":"response","model":"` + u.lastModel + `","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}`))
	})
	return u
}

const simpleResponsesReq = `{"model":"vm","input":"hi"}`

func TestResponses_RoutesToUpstreamAndRewritesModel(t *testing.T) {
	t.Parallel()
	u := newResponsesUpstream(t)
	ts := newRouterServer(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-responses: `+u.srv.URL+`}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-responses, provider: p1, models: [real-model-one]}
`)
	resp, body := postResponses(t, ts, simpleResponsesReq)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, body=%s", resp.StatusCode, body)
	}
	if u.lastModel != "real-model-one" {
		t.Errorf("upstream received model=%q, want the real upstream model (virtual name rewritten)", u.lastModel)
	}
	if !strings.Contains(string(u.lastRawReq), `"input":"hi"`) {
		t.Errorf("upstream request body lost the Responses \"input\" field: %s", u.lastRawReq)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("client response is not valid JSON: %v (%s)", err, body)
	}
	var clientModel string
	json.Unmarshal(m["model"], &clientModel)
	if clientModel != "vm" {
		t.Errorf("client-facing response model=%q, want the virtual model name restored", clientModel)
	}
}

func TestResponses_WrongProtocolHint(t *testing.T) {
	t.Parallel()
	// A model that only exists on the openai (Chat Completions) protocol
	// face must not be reachable via POST /v1/responses — same "wrong
	// entry point" 404 the openai/anthropic pair already gets, extended to
	// the third protocol (router.otherProtocolFor/IngressPath).
	ts := newRouterServer(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, provider: p1, models: [m]}
`)
	resp, body := postResponses(t, ts, simpleResponsesReq)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404: %s", resp.StatusCode, body)
	}
	if !strings.Contains(body, "/v1/chat/completions") {
		t.Errorf("404 should redirect to the correct entry point: %s", body)
	}
}

func TestResponses_ModelsListIncludesThirdProtocol(t *testing.T) {
	t.Parallel()
	ts := newRouterServer(t, `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-responses: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-responses, provider: p1, models: [m]}
`)
	req, _ := http.NewRequest("GET", ts.URL+"/v1/models", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"vmr_protocol":"openai-responses"`) {
		t.Errorf("GET /v1/models should list the openai-responses face: %s", b)
	}
}
