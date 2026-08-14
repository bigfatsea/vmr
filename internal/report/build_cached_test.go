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
	got, _, cache, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil)
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
	_, _, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached (cold): %v", err)
	}
	got, _, cache2, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, cache1, nil)
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
	if cache2.Files[path].Hash != cache1.Files[path].Hash {
		t.Error("warm cache entry's hash should be unchanged (file didn't change)")
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

func TestWriteRequestsJSON_RoundTripsFilesAndRows(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	_, _, cache, err := BuildCached([]string{path}, time.Now(), nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}
	rows := []RequestRow{{TS: "2026-07-24T00:00:00Z", Outcome: "ok"}, {TS: "2026-07-24T00:01:00Z", Outcome: "error"}}

	outPath := filepath.Join(dir, "vmr-requests.json")
	n, err := WriteRequestsJSON(rows, cache, outPath)
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
	if len(idx.Files.Files) != 1 {
		t.Errorf("got %d file cache entries, want 1", len(idx.Files.Files))
	}

	loaded := LoadRequestsFileCache(outPath)
	if loaded == nil || len(loaded.Files) != 1 {
		t.Errorf("LoadRequestsFileCache = %+v, want the same 1-entry cache", loaded)
	}
	if loaded.Files[path].Hash != cache.Files[path].Hash {
		t.Errorf("loaded hash %q != original %q", loaded.Files[path].Hash, cache.Files[path].Hash)
	}
}

func TestWriteRequestsJSON_NilCacheWritesEmptyFilesSection(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "vmr-requests.json")
	if _, err := WriteRequestsJSON(nil, nil, outPath); err != nil {
		t.Fatalf("WriteRequestsJSON: %v", err)
	}
	loaded := LoadRequestsFileCache(outPath)
	if loaded == nil || len(loaded.Files) != 0 {
		t.Errorf("expected a non-nil, empty cache, got %+v", loaded)
	}
}

func TestLoadRequestsFileCache_MissingOrCorruptDegradesToNil(t *testing.T) {
	dir := t.TempDir()
	if got := LoadRequestsFileCache(filepath.Join(dir, "does-not-exist.json")); got != nil {
		t.Errorf("missing file should return nil, got %+v", got)
	}
	corruptPath := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("{not valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := LoadRequestsFileCache(corruptPath); got != nil {
		t.Errorf("corrupt file should return nil, got %+v", got)
	}
}

// TestBuildCached_ChangedFileReparses mirrors ctxgraph's own
// TestScanCached_ChangedFileReparses at the report layer: appending to the
// input file must be picked up, not served stale from the prior cache.
func TestBuildCached_ChangedFileReparses(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords()[:2])
	now := time.Now()

	_, sess1, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil)
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

	rep2, sess2, cache2, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, cache1, nil)
	if err != nil {
		t.Fatalf("BuildCached (after append): %v", err)
	}
	if cache2.Files[path].Hash == cache1.Files[path].Hash {
		t.Error("hash should differ after appending")
	}
	if rep2.Meta.Records != 3 {
		t.Errorf("rep2.Meta.Records = %d, want 3", rep2.Meta.Records)
	}
	if len(sess2.Recs) <= len(sess1.Recs) {
		t.Errorf("sess2 should have picked up the new record: sess1=%d sess2=%d", len(sess1.Recs), len(sess2.Recs))
	}
}
