// Ver 2026-08-20 00:00, by Sonnet 5

package report

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

// cacheWithFacts builds a *ctxgraph.FileCache holding one entry (keyed by
// path's canonical basename) whose Facts already contain n synthetic
// records — enough for scanFiles' cache-hit branch to engage without any
// real file needing to exist.
func cacheWithFacts(t *testing.T, path string, n int) *ctxgraph.FileCache {
	t.Helper()
	var ff fileFacts
	for i := 1; i <= n; i++ {
		ff.Records = append(ff.Records, recordFacts{Line: i, TS: time.Now(), Model: "m1", Outcome: "ok"})
	}
	data, err := json.Marshal(ff)
	if err != nil {
		t.Fatal(err)
	}
	key := ctxgraph.CanonicalPath(path)
	return &ctxgraph.FileCache{Files: map[string]ctxgraph.CachedFile{
		key: {Hash: "irrelevant-for-this-test", SchemaVersion: ctxgraph.CacheSchemaVersion, Facts: data},
	}}
}

// TestScanFiles_CacheHitNeverOpensFile is the direct, mechanism-level
// proof P3.6 exists for: given a valid Facts cache entry and onRecord ==
// nil (the -details=false path), scanFiles must never call
// audit.OpenLogFile — proven here by pointing it at a path that does not
// exist on disk at all and confirming it still succeeds and ingests every
// cached record.
func TestScanFiles_CacheHitNeverOpensFile(t *testing.T) {
	const path = "/definitely/does/not/exist/vmr-audit-2026-01-01.jsonl"
	cache := cacheWithFacts(t, path, 3)

	rep := &Report2{}
	st := newAggState(rep, &SessionAnalysis{}, nil, nil)
	if err := st.scanFiles([]string{path}, nil, nil, cache); err != nil {
		t.Fatalf("scanFiles with a valid Facts cache hit should never touch the filesystem, got: %v", err)
	}
	if rep.Meta.Records != 3 {
		t.Errorf("Meta.Records = %d, want 3", rep.Meta.Records)
	}
	if len(rep.requests) != 3 {
		t.Errorf("got %d ingested requests, want 3", len(rep.requests))
	}
}

// TestScanFiles_DetailsPathIgnoresFactsCache is
// TestScanFiles_CacheHitNeverOpensFile's negative: with onRecord non-nil
// (-details=true — it needs the raw audit.Record to render), the same
// valid Facts cache must NOT be trusted to skip the file open, since
// onRecord requires the actual record body. Proven by the inverse
// assertion: pointed at the same nonexistent path, it must fail trying to
// open it, not silently succeed off the cache.
func TestScanFiles_DetailsPathIgnoresFactsCache(t *testing.T) {
	const path = "/definitely/does/not/exist/vmr-audit-2026-01-01.jsonl"
	cache := cacheWithFacts(t, path, 3)

	rep := &Report2{}
	st := newAggState(rep, &SessionAnalysis{}, nil, nil)
	onRecord := func(*audit.Record, *ReqInfo) {}
	if err := st.scanFiles([]string{path}, nil, onRecord, cache); err == nil {
		t.Error("expected an error opening a nonexistent file when onRecord forces a decode, even with a valid Facts cache present")
	}
}

// TestRecordFactsJSONGolden pins the serialized fileFacts shape produced by
// extractRecordFacts for a fixed input record. This is the report-side
// guard for CacheSchemaVersion's manual bump (ctxgraph's Manifest golden
// covers the other half — see golden_test.go over there): if this test
// fails, EITHER you changed extraction logic unintentionally (fix the
// regression), OR you intentionally changed it and MUST bump
// CacheSchemaVersion in internal/ctxgraph/cache.go AND update this golden
// — otherwise stale .parse-cache entries keep silently serving output from
// the old logic with no error anywhere.
func TestRecordFactsJSONGolden(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	body := map[string]any{
		"model":    "claude",
		"metadata": map[string]any{"user_id": "session_abc123"},
		"messages": []any{
			map[string]any{"role": "system", "content": "you are a helpful router test fixture"},
			map[string]any{"role": "user", "content": "hello there"},
		},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "description": "read a file", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}}},
		},
	}
	arec := audit.Record{
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
				Request: audit.Message{Headers: http.Header{}}, DurMS: 300},
			{Endpoint: "anthropic-messages:prov-a:model-a", Protocol: "anthropic-messages",
				Request:  audit.Message{Headers: http.Header{}},
				Response: &audit.Message{Status: 200, Headers: http.Header{}}, DurMS: 900},
		},
	}
	rf := extractRecordFacts(&arec, 7)
	ff := fileFacts{Records: []recordFacts{rf}}
	got, err := json.Marshal(ff)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"records":[{"line":7,"ts":"2026-09-02T08:30:00Z","model":"claude","protocol":"anthropic-messages","outcome":"ok","stream":true,"dur_ms":1234,"ttft_ms":210,"bytes_in":340,"bytes_out":49,"tool_decl_count":1,"tool_decl_bytes":152,"endpoint":"anthropic-messages:prov-a:model-a","client_key":"k1","fallbacks_raw":1,"est_in_fresh":101,"attempts":[{"endpoint":"anthropic-messages:prov-b:model-b","error":"network: dial timeout","error_class":"network","dur_ms":300},{"endpoint":"anthropic-messages:prov-a:model-a","has_response":true,"status":200,"dur_ms":900}]}]}`
	if string(got) != want {
		t.Fatalf(`serialized fileFacts shape changed:

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

// TestLoadCachedFacts_RejectsStaleSchemaVersion covers the defensive
// SchemaVersion re-check loadCachedFacts does on top of
// ctxgraph.ScanCached's own — see that function's doc comment for why a
// wrong answer here would silently corrupt aggregated numbers rather than
// just cost a slower rerun.
func TestLoadCachedFacts_RejectsStaleSchemaVersion(t *testing.T) {
	cache := cacheWithFacts(t, "audit.jsonl", 1)
	key := ctxgraph.CanonicalPath("audit.jsonl")
	stale := cache.Files[key]
	stale.SchemaVersion = ctxgraph.CacheSchemaVersion - 1
	cache.Files[key] = stale

	if _, ok := loadCachedFacts(cache, key); ok {
		t.Error("loadCachedFacts should reject an entry with a stale SchemaVersion")
	}
}

func TestLoadCachedFacts_NilCache(t *testing.T) {
	if _, ok := loadCachedFacts(nil, "x"); ok {
		t.Error("loadCachedFacts(nil, ...) should report ok=false")
	}
}

func TestStoreCachedFacts_NilCacheIsNoop(t *testing.T) {
	storeCachedFacts(nil, "x", fileFacts{}) // must not panic
}
