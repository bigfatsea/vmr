// Ver 2026-08-20 00:00, by Sonnet 5

package report

import (
	"encoding/json"
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
	st := newAggState(rep, &SessionAnalysis{}, nil)
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
	st := newAggState(rep, &SessionAnalysis{}, nil)
	onRecord := func(*audit.Record, *ReqInfo) {}
	if err := st.scanFiles([]string{path}, nil, onRecord, cache); err == nil {
		t.Error("expected an error opening a nonexistent file when onRecord forces a decode, even with a valid Facts cache present")
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
