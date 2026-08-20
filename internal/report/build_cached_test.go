// Ver 2026-08-05, by Sonnet 5

package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/ctxgraph"
	"vmr/internal/taskseg"
)

// TestBuild_LogsClientEndpointRowCount is the lock-in: §5.5 has no Top-N
// cap by design, so this progress line is the one observable signal an
// operator gets that it's grown large — must actually appear when
// ClientEndpoints is non-empty, and name both dimensions (clients, rows).
func TestBuild_LogsClientEndpointRowCount(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	var progress bytes.Buffer
	if _, _, err := Build([]string{path}, time.Now(), &progress, nil, nil, nil); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(progress.String(), "§5.5: 1 client(s) x 1 endpoint row(s)") {
		t.Errorf("progress output missing the §5.5 row-count line, got:\n%s", progress.String())
	}
}

// TestBuildCached_ColdMatchesBuild: with no prior cache, BuildCached must
// produce byte-identical output to Build (everything is a miss) — the
// caching path must never change results, only skip work.
func TestBuildCached_ColdMatchesBuild(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	now := time.Now()

	want, _, err := Build([]string{path}, now, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got, _, cache, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("BuildCached (cold) differs from Build:\nBuild:       %s\nBuildCached: %s", wantJSON, gotJSON)
	}
	if len(cache.Files) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(cache.Files))
	}
}

// TestBuildCached_WarmMatchesBuild: feeding a prior run's cache back in
// (the normal repeat-invocation path) must still produce byte-identical
// output to an uncached Build — this is the actual regression guard that
// the cache is transparent, not a shortcut that changes results.
func TestBuildCached_WarmMatchesBuild(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	now := time.Now()

	want, _, err := Build([]string{path}, now, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, _, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached (cold): %v", err)
	}
	got, _, cache2, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, cache1, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached (warm): %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("BuildCached (warm) differs from Build:\nBuild:       %s\nBuildCached: %s", wantJSON, gotJSON)
	}
	key := ctxgraph.CanonicalPath(path)
	if cache2.Files[key].Hash != cache1.Files[key].Hash {
		t.Error("warm cache entry's hash should be unchanged (file didn't change)")
	}
}

// TestBuildCached_WarmPopulatesFactsCache proves BuildCached's cold run
// leaves behind a non-empty Facts entry — the precondition
// factscache_test.go's TestScanFiles_CacheHitNeverOpensFile relies on to
// test the hit path in isolation, and by itself already enough to prove
// storeCachedFacts actually ran (a bug there would silently leave every
// future run paying the pre-P3.6 cost, without ever failing this loudly).
func TestBuildCached_WarmPopulatesFactsCache(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	now := time.Now()

	_, _, cache, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached (cold): %v", err)
	}
	key := ctxgraph.CanonicalPath(path)
	if len(cache.Files[key].Facts) == 0 {
		t.Error("cold run should have populated the file's Facts cache entry")
	}
}

// TestAnalyzeSessionsCached_NilProfileErrors pins the fail-fast guard for a
// nil taskseg.Profile: without it, collect() only calls prof.RealUserText
// once inside one of AnalyzeSessionsCached's per-file worker goroutines
// (none of which recover a panic), so a nil prof would crash the whole
// process instead of returning a clean error from this call.
func TestAnalyzeSessionsCached_NilProfileErrors(t *testing.T) {
	if _, _, err := AnalyzeSessionsCached(nil, nil, nil); err == nil {
		t.Error("AnalyzeSessionsCached with a nil Profile should return an error, not panic")
	}
}

func TestAnalyzeSessionsCached_ColdCacheMatchesAnalyzeSessions(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())

	want, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatalf("AnalyzeSessions: %v", err)
	}
	got, cache, err := AnalyzeSessionsCached([]string{path}, nil, taskseg.OpenClawAware)
	if err != nil {
		t.Fatalf("AnalyzeSessionsCached: %v", err)
	}
	if len(got.Sessions) != len(want.Sessions) {
		t.Errorf("AnalyzeSessionsCached produced %d sessions, AnalyzeSessions produced %d", len(got.Sessions), len(want.Sessions))
	}
	if len(got.Recs) != len(want.Recs) {
		t.Errorf("AnalyzeSessionsCached produced %d recs, AnalyzeSessions produced %d", len(got.Recs), len(want.Recs))
	}
	if len(cache.Files) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(cache.Files))
	}
}

// TestWriteRequestsJSON_RoundTripsRows covers WriteRequestsJSON's own
// remaining job — the parse cache used to round-trip through this same
// file (a "files" section) but has since moved to its own
// content-hash-sharded directory; see ctxgraph's own
// TestSaveCacheDir_LoadCacheDir_RoundTrip for that half now.
func TestWriteRequestsJSON_RoundTripsRows(t *testing.T) {
	dir := t.TempDir()
	rows := []RequestRow{{TS: "2026-07-24T00:00:00Z", Outcome: "ok"}, {TS: "2026-07-24T00:01:00Z", Outcome: "error"}}

	outPath := filepath.Join(dir, "vmr-requests.json")
	n, err := WriteRequestsJSON(rows, outPath)
	if err != nil {
		t.Fatalf("WriteRequestsJSON: %v", err)
	}
	if n != 2 {
		t.Errorf("n = %d, want 2", n)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var idx RequestsIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("vmr-requests.json is not valid JSON: %v\n%s", err, data)
	}
	if len(idx.Requests) != 2 {
		t.Errorf("got %d requests, want 2", len(idx.Requests))
	}
	if strings.Contains(string(data), `"files"`) {
		t.Error("vmr-requests.json should no longer embed a \"files\" cache section")
	}
}

// TestBuildCached_ChangedFileReparses mirrors ctxgraph's own
// TestScanCached_ChangedFileReparses at the report layer: appending to the
// input file must be picked up, not served stale from the prior cache.
func TestBuildCached_ChangedFileReparses(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords()[:2])
	now := time.Now()

	_, sess1, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached (cold): %v", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	extra := smallAuditRecords()[2]
	b, err := json.Marshal(extra)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	f.Close()

	rep2, sess2, cache2, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, cache1, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached (after append): %v", err)
	}
	key := ctxgraph.CanonicalPath(path)
	if cache2.Files[key].Hash == cache1.Files[key].Hash {
		t.Error("hash should differ after appending")
	}
	if rep2.Meta.Records != 3 {
		t.Errorf("rep2.Meta.Records = %d, want 3", rep2.Meta.Records)
	}
	if len(sess2.Recs) <= len(sess1.Recs) {
		t.Errorf("sess2 should have picked up the new record: sess1=%d sess2=%d", len(sess1.Recs), len(sess2.Recs))
	}
}
