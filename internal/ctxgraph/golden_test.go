// Ver 2026-09-02 00:00, by pi-agent

package ctxgraph

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"vmr/internal/audit"
)

// goldenRecord is a deliberately dense fixture: it touches every field
// BuildManifest extracts (endpoints, served-endpoint attribution, usage,
// degraded-estimate absence, trace id, metadata SessKey, SysHash/LeadSys,
// EstIn/EstOut-eligible fields) plus one Anthropic cache_control marker —
// the exact input whose hashing hashMsgJSON strips. It exists so that ANY
// change to the extraction logic or the serialized Manifest shape shows up
// here instead of silently invalidating (or, worse, silently reusing)
// on-disk .parse-cache entries.
func goldenRecord() audit.Record {
	hdr := http.Header{}
	hdr.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	body := map[string]any{
		"model":    "claude",
		"metadata": map[string]any{"user_id": "session_abc123"},
		"messages": []any{
			map[string]any{"role": "system", "content": "you are a helpful router test fixture"},
			map[string]any{"role": "user", "content": "hello there"},
			map[string]any{"role": "assistant", "content": "hi"},
			map[string]any{
				"role":    "user",
				"content": "continue",
				// A client-side cache-routing marker, not conversation
				// content: must NOT influence Keys (hashMsgJSON strips it).
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
	}
	return audit.Record{
		TS:           time.Date(2026, 9, 2, 8, 30, 0, 0, time.UTC),
		Model:        "claude",
		Protocol:     "anthropic-messages",
		Outcome:      "ok",
		Stream:       true,
		DurMS:        1234,
		TTFTMS:       210,
		ClientKeyTag: "k1",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/messages", Headers: hdr, Body: body},
			Response: &audit.Message{Status: 200, Body: map[string]any{"usage": map[string]any{"input_tokens": 100.0, "output_tokens": 42.0}}},
		},
		Attempts: []audit.Attempt{
			{Endpoint: "anthropic-messages:prov-b:model-b", Protocol: "anthropic-messages",
				Error: "network: dial timeout", ErrorClass: "network",
				Request: audit.Message{Headers: http.Header{}}},
			{Endpoint: "anthropic-messages:prov-a:model-a", Protocol: "anthropic-messages",
				Request:  audit.Message{Headers: http.Header{}},
				Response: &audit.Message{Status: 200, Headers: http.Header{}}},
		},
	}
}

// TestManifestJSONGolden pins the serialized Manifest shape produced by
// BuildManifest for a fixed input record. This is the guard for
// CacheSchemaVersion's manual bump (internal/report's Facts payload shares
// this version through the same cache file — see cache.go's doc comment):
// if this test fails, EITHER you changed extraction logic unintentionally
// (fix the regression), OR you intentionally changed it and MUST bump
// CacheSchemaVersion in internal/ctxgraph/cache.go and update this golden —
// otherwise stale .parse-cache entries keep silently serving output from
// the old logic with no error anywhere.
func TestManifestJSONGolden(t *testing.T) {
	t.Parallel()
	rec := goldenRecord()
	m, ok := BuildManifest(&rec, "vmr-audit-2026-09-02.jsonl", 7)
	if !ok {
		t.Fatal("BuildManifest failed on the golden fixture")
	}
	got, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"path":"vmr-audit-2026-09-02.jsonl","line":7,"req":"vmr-audit-2026-09-02.jsonl:7","ts":"2026-09-02T08:30:00Z","model":"claude","protocol":"anthropic-messages","outcome":"ok","endpoint":"anthropic-messages:prov-a:model-a","served_endpoint":"anthropic-messages:prov-a:model-a","stream":true,"dur_ms":1234,"ttft_ms":210,"usage":{"In":100,"Out":42,"CacheRead":0,"CacheWrite":0,"Reasoning":0},"usage_in_ok":true,"usage_out_ok":true,"client_key_tag":"k1","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","sys_hash":"41bb0151b24cb51793b08c05ba006c6f","has_sys":true,"lead_sys":1,"keys":["9a0bc12dd8f3a89551a7916ec9dde591","53d72162da0600d52f2e7bd974a05b79","223496d5d1ce6299c719cf35c17d60c4"],"msg_idx":[1,2,3],"sess_key":"meta:session_abc123"}`
	if string(got) != want {
		t.Fatalf(`serialized Manifest shape changed:

 got: %s
want: %s

If this change is INTENTIONAL (extraction logic legitimately changed), bump
CacheSchemaVersion in internal/ctxgraph/cache.go AND update this golden —
a version bump without this golden (or this golden without a bump) leaves
stale .parse-cache entries silently serving old-logic output.
If it is NOT intentional, fix the extraction regression instead.`,
			got, want)
	}
}

// TestBuildManifest_IgnoresCacheControl pins hashMsgJSON's stripping
// directly: the same message with and without a cache_control marker (on
// the message itself or on a nested content block) must produce the same
// Key — a client moving its breakpoint between turns is not a content edit.
func TestBuildManifest_IgnoresCacheControl(t *testing.T) {
	t.Parallel()
	plain := map[string]any{"role": "user", "content": "same content"}
	marked := map[string]any{
		"role": "user", "content": "same content",
		"cache_control": map[string]any{"type": "ephemeral"},
	}
	blockMarked := map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{"type": "text", "text": "same content"},
			map[string]any{"type": "text", "text": "same content",
				"cache_control": map[string]any{"type": "ephemeral"}},
		},
	}
	blockPlain := map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{"type": "text", "text": "same content"},
			map[string]any{"type": "text", "text": "same content"},
		},
	}
	h := func(msg map[string]any) Hash {
		rec := mkAuditRec(time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
			map[string]any{"messages": []any{msg}})
		m, ok := BuildManifest(&rec, "a.jsonl", 1)
		if !ok || len(m.Keys) != 1 {
			t.Fatal("BuildManifest failed")
		}
		return m.Keys[0]
	}
	// The two marked forms must hash equal to the plain form — and the
	// blockMarked case must also have left the caller's body untouched
	// (hashing must never mutate the shared decoded record).
	if h(plain) != h(marked) || h(blockPlain) != h(blockMarked) {
		t.Error("cache_control markers changed the message hash — hashMsgJSON's stripping regressed")
	}
	if _, ok := blockMarked["content"].([]any)[1].(map[string]any)["cache_control"]; !ok {
		t.Error("hashing mutated the record's own content — stripCacheControl must copy, not mutate")
	}
}
