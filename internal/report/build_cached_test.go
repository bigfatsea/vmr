// Ver 2026-08-05, by Sonnet 5

package report

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"vmr/internal/ctxgraph"
	"vmr/internal/taskseg"
)

// hashKey returns the content-hash cache key for path — what the FileCache
// is indexed by now (see ctxgraph.FileCache's doc comment).
func hashKey(t *testing.T, path string) string {
	t.Helper()
	h, err := ctxgraph.HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestBuild_LogsClientEndpointRowCount is the lock-in: §5.5 has no Top-N
// cap by design, so this progress line is the one observable signal an
// operator gets that it's grown large — must actually appear when
// ClientEndpoints is non-empty, and name both dimensions (clients, rows).
func TestBuild_LogsClientEndpointRowCount(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	var progress bytes.Buffer
	if _, _, _, err := BuildCached([]string{path}, time.Now(), &progress, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil); err != nil {
		t.Fatalf("BuildCached: %v", err)
	}
	if !strings.Contains(progress.String(), "§5.5: 1 client(s) x 1 endpoint row(s)") {
		t.Errorf("progress output missing the §5.5 row-count line, got:\n%s", progress.String())
	}
}

// TestBuildCached_ColdPopulatesOneCacheEntry: with no prior cache, every
// path is a miss — BuildCached must still return a usable Report2 and
// populate exactly one cache entry per scanned file.
func TestBuildCached_ColdPopulatesOneCacheEntry(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	now := time.Now()

	_, _, cache, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCached: %v", err)
	}
	if len(cache.Files) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(cache.Files))
	}
}

// TestBuildCached_WarmMatchesCold: feeding a prior run's cache back in (the
// normal repeat-invocation path) must produce byte-identical output to a
// from-scratch cold run — the regression guard that the cache is
// transparent, not a shortcut that changes results.
func TestBuildCached_WarmMatchesCold(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	now := time.Now()

	want, _, cache1, err := BuildCached([]string{path}, now, nil, nil, nil, nil, taskseg.OpenClawAware, nil, nil, nil)
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
		t.Errorf("BuildCached (warm) differs from BuildCached (cold):\ncold: %s\nwarm: %s", wantJSON, gotJSON)
	}
	key := hashKey(t, path)
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
	key := hashKey(t, path)
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

func TestAnalyzeSessionsCached_ColdCachePopulatesOneEntry(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())

	got, cache, err := AnalyzeSessionsCached([]string{path}, nil, taskseg.OpenClawAware)
	if err != nil {
		t.Fatalf("AnalyzeSessionsCached: %v", err)
	}
	if len(got.Sessions) == 0 {
		t.Error("AnalyzeSessionsCached produced 0 sessions")
	}
	if len(cache.Files) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(cache.Files))
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

	// oldKey must be taken BEFORE the append: FileCache.Files keys by content
	// hash, so after the append the same lookup is a different (new-content)
	// key cache1 never saw — a lookup that returns the zero CachedFile and
	// makes the comparison below vacuously true.
	oldKey := hashKey(t, path)

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
	newKey := hashKey(t, path)
	if newKey == oldKey {
		t.Fatal("appending a record did not change the file's content hash")
	}
	if _, ok := cache2.Files[newKey]; !ok {
		t.Fatalf("no cache2 entry for the appended content (key %s) — the changed file was not rescanned", newKey)
	}
	if _, ok := cache1.Files[oldKey]; !ok {
		t.Fatalf("no cache1 entry for initial content (key %s) — cold build failed to index", oldKey)
	}
	if cache2.Files[newKey].Hash == cache1.Files[oldKey].Hash {
		t.Error("hash should differ after appending")
	}
	if rep2.Meta.Records != 3 {
		t.Errorf("rep2.Meta.Records = %d, want 3", rep2.Meta.Records)
	}
	if len(sess2.Recs) <= len(sess1.Recs) {
		t.Errorf("sess2 should have picked up the new record: sess1=%d sess2=%d", len(sess1.Recs), len(sess2.Recs))
	}
}
