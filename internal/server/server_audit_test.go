// Ver 2026-07-07 17:45, by Fable 5

// Audit integration: every chat request produces one JSONL line with both
// layers (client exchange + per-attempt upstream trail).
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/router"
)

func newSSEUpstream(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 2; i++ {
			fmt.Fprintf(w, "data: {\"chunk\":%d}\n\n", i)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newAuditedServer(t *testing.T, yaml string) (*httptest.Server, *audit.Logger) {
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { al.Close() })
	ts := httptest.NewServer(New(rt, al).Handler())
	t.Cleanup(ts.Close)
	return ts, al
}

func readRecords(t *testing.T, al *audit.Logger) []audit.Record {
	data, err := os.ReadFile(al.Path())
	if err != nil {
		t.Fatal(err)
	}
	var recs []audit.Record
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r audit.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad jsonl line: %v\n%s", err, line)
		}
		recs = append(recs, r)
	}
	return recs
}

func TestAuditRecordsFailoverBothLayers(t *testing.T) {
	u1, u2 := newUpstream(t), newUpstream(t)
	u1.status.Store(500)
	ts, al := newAuditedServer(t, twoEndpointYAML(u1.srv.URL, u2.srv.URL, ""))

	chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer sk-client-secret-1234"})

	recs := readRecords(t, al)
	if len(recs) != 1 {
		t.Fatalf("records: %d", len(recs))
	}
	r := recs[0]
	if r.Model != "vm" || r.Protocol != "openai" || r.Outcome != "ok" || r.Stream {
		t.Errorf("meta: %+v", r)
	}
	// Client layer: request body recorded as JSON, credential masked, response captured.
	if got := r.Client.Request.Headers.Get("Authorization"); got != "Bearer ***1234" {
		t.Errorf("client auth header not masked: %q", got)
	}
	var reqBody struct {
		Model string `json:"model"`
	}
	raw, _ := json.Marshal(r.Client.Request.Body)
	json.Unmarshal(raw, &reqBody)
	if reqBody.Model != "vm" {
		t.Errorf("client request body: %v", r.Client.Request.Body)
	}
	if r.Client.Response == nil || r.Client.Response.Status != 200 || r.Client.Response.Body == nil {
		t.Fatalf("client response: %+v", r.Client.Response)
	}
	// Attempt layer: failed attempt has error body; success attempt omits body.
	if len(r.Attempts) != 2 {
		t.Fatalf("attempts: %d", len(r.Attempts))
	}
	a1, a2 := r.Attempts[0], r.Attempts[1]
	if a1.Endpoint != "openai:p1:model-one" || a1.Error != "transient" || a1.ErrorClass != "transient" || a1.Response == nil || a1.Response.Status != 500 || a1.Response.Body == nil {
		t.Errorf("failed attempt: %+v", a1)
	}
	if a1.Protocol != "openai" || a1.Provider != "p1" || a1.Model != "model-one" {
		t.Errorf("failed attempt protocol/provider/model: %+v", a1)
	}
	if got := a1.Request.Headers.Get("Authorization"); got != "Bearer ***" && !strings.HasPrefix(got, "Bearer ***") {
		t.Errorf("upstream auth not masked: %q", got)
	}
	if !strings.HasSuffix(a1.URL, "/chat/completions") {
		t.Errorf("attempt url: %s", a1.URL)
	}
	if a2.Endpoint != "openai:p2:model-two" || a2.Error != "" || a2.ErrorClass != "" || a2.Response == nil || a2.Response.Status != 200 {
		t.Errorf("success attempt: %+v", a2)
	}
	if a2.Protocol != "openai" || a2.Provider != "p2" || a2.Model != "model-two" {
		t.Errorf("success attempt protocol/provider/model: %+v", a2)
	}
	if a2.Response.Body != nil {
		t.Error("success attempt body must be omitted (identical to client response body)")
	}
	// Outbound body has the rewritten model.
	raw, _ = json.Marshal(a2.Request.Body)
	var out struct {
		Model string `json:"model"`
	}
	json.Unmarshal(raw, &out)
	if out.Model != "model-two" {
		t.Errorf("outbound body model: %v", a2.Request.Body)
	}
}

func TestAuditRecordsRejectedAndErrorRequests(t *testing.T) {
	u := newUpstream(t)
	ts, al := newAuditedServer(t, twoEndpointYAML(u.srv.URL, u.srv.URL, "api_key: sk-vmr"))

	chat(t, ts, simpleReq, nil)                                                  // 401 unauthorized
	chat(t, ts, `not json`, map[string]string{"Authorization": "Bearer sk-vmr"}) // 400 bad json

	recs := readRecords(t, al)
	if len(recs) != 2 {
		t.Fatalf("records: %d", len(recs))
	}
	if recs[0].Outcome != "error" || recs[0].Client.Response.Status != 401 || recs[0].Model != "" {
		t.Errorf("unauthorized record: %+v", recs[0])
	}
	if recs[1].Outcome != "error" || recs[1].Client.Response.Status != 400 {
		t.Errorf("bad json record: %+v", recs[1])
	}
	if recs[1].Client.Request.Body.(string) != "not json" {
		t.Errorf("non-json body should be recorded as string: %v", recs[1].Client.Request.Body)
	}
}

func TestAuditRecordsStreaming(t *testing.T) {
	sse := newSSEUpstream(t)
	ts, al := newAuditedServer(t, twoEndpointYAML(sse.URL, sse.URL, ""))

	chat(t, ts, `{"model":"vm","stream":true,"messages":[]}`, nil)

	recs := readRecords(t, al)
	r := recs[0]
	if !r.Stream || r.Outcome != "ok" {
		t.Errorf("meta: %+v", r)
	}
	body, _ := r.Client.Response.Body.(string)
	if !strings.Contains(body, "[DONE]") {
		t.Errorf("streamed body not captured: %v", r.Client.Response.Body)
	}
}
