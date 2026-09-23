// Ver 2026-09-23 03:00, by Claude Opus 5.5

package report

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
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
	if _, _, _, err := Build(BuildOptions{Paths: []string{path}, Progress: &progress}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(progress.String(), "§5.5: 1 client(s) x 1 endpoint row(s)") {
		t.Errorf("progress output missing the §5.5 row-count line, got:\n%s", progress.String())
	}
}

// TestBuild_ColdPopulatesOneCacheEntry: with no prior cache, every
// path is a miss — Build must still return a usable Report and
// populate exactly one cache entry per scanned file.
func TestBuild_ColdPopulatesOneCacheEntry(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	now := time.Now()

	_, _, cache, err := Build(BuildOptions{Paths: []string{path}, Now: now})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(cache.Files) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(cache.Files))
	}
}

// TestBuild_WarmMatchesCold: feeding a prior run's cache back in (the
// normal repeat-invocation path) must produce byte-identical output to a
// from-scratch cold run — the regression guard that the cache is
// transparent, not a shortcut that changes results.
func TestBuild_WarmMatchesCold(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	now := time.Now()

	want, _, cache1, err := Build(BuildOptions{Paths: []string{path}, Now: now})
	if err != nil {
		t.Fatalf("Build (cold): %v", err)
	}
	got, _, cache2, err := Build(BuildOptions{Paths: []string{path}, Now: now, PriorCache: cache1})
	if err != nil {
		t.Fatalf("Build (warm): %v", err)
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
		t.Errorf("Build (warm) differs from Build (cold):\ncold: %s\nwarm: %s", wantJSON, gotJSON)
	}
	key := hashKey(t, path)
	if cache2.Files[key].Hash != cache1.Files[key].Hash {
		t.Error("warm cache entry's hash should be unchanged (file didn't change)")
	}
}

// TestBuild_WarmMatchesColdFullFeature is the collect-side extension of
// TestBuild_WarmMatchesCold: feeding a prior run's cache back in must produce
// a session analysis that matches a cold run on every grouping/feature field
// collect() used to own (session/task counts, compaction links, tool shapes,
// role distribution) — not just the JSON report.
func TestBuild_WarmMatchesColdFullFeature(t *testing.T) {
	path, _ := fixture(t)
	coldSess, _, err := AnalyzeSessionsCached([]string{path}, nil, taskseg.OpenClawAware)
	if err != nil {
		t.Fatalf("AnalyzeSessionsCached (cold): %v", err)
	}
	warmSess, _, err := AnalyzeSessionsCached([]string{path}, priorFor(t, path), taskseg.OpenClawAware)
	if err != nil {
		t.Fatalf("AnalyzeSessionsCached (warm): %v", err)
	}

	if len(coldSess.Sessions) != len(warmSess.Sessions) {
		t.Errorf("session count differs: cold %d, warm %d", len(coldSess.Sessions), len(warmSess.Sessions))
	}
	if len(coldSess.Compactions) != len(warmSess.Compactions) {
		t.Errorf("compaction count differs: cold %d, warm %d", len(coldSess.Compactions), len(warmSess.Compactions))
	}
	if len(coldSess.Ungrouped) != len(warmSess.Ungrouped) {
		t.Errorf("ungrouped count differs: cold %d, warm %d", len(coldSess.Ungrouped), len(warmSess.Ungrouped))
	}

	for i, cs := range coldSess.Sessions {
		ws := warmSess.Sessions[i]
		if cs.ID != ws.ID {
			t.Errorf("session[%d].ID differs: cold %q, warm %q", i, cs.ID, ws.ID)
		}
		if len(cs.Tasks) != len(ws.Tasks) {
			t.Errorf("session[%d] task count differs: cold %d, warm %d", i, len(cs.Tasks), len(ws.Tasks))
			continue
		}
		for j, ct := range cs.Tasks {
			if len(ct.Recs) != len(ws.Tasks[j].Recs) {
				t.Errorf("session[%d].tasks[%d] rec count differs: cold %d, warm %d", i, j, len(ct.Recs), len(ws.Tasks[j].Recs))
			}
		}
	}

	if len(coldSess.Recs) != len(warmSess.Recs) {
		t.Fatalf("rec count differs: cold %d, warm %d", len(coldSess.Recs), len(warmSess.Recs))
	}

	// Per-record feature comparison (tool shapes, role distribution, compaction links).
	type recSnap struct {
		Model, Outcome, Protocol string
		SessionID, TaskID        string
		TaskSeq                  int
		Compaction               bool
		Summarizes, ContinuesTo  string
		ToolsSig                 string
		ToolsDeclared, Tags      string
		UsageIn, UsageOut        int64
		UsageInOK, UsageOutOK    bool
		RoleChars, RoleTokens    map[string]int64
		NoReply                  bool
		TraceID, ChatID          string
	}
	snap := func(r *ReqInfo) recSnap {
		return recSnap{
			Model: r.Model, Outcome: r.Outcome, Protocol: r.Protocol,
			SessionID: r.SessionID, TaskID: r.TaskID, TaskSeq: r.TaskSeq,
			Compaction: r.Compaction, Summarizes: r.Summarizes, ContinuesTo: r.ContinuesTo,
			ToolsSig: r.ToolsSig, ToolsDeclared: strings.Join(r.ToolsDeclared, ","), Tags: strings.Join(r.Tags, ","),
			UsageIn: r.Usage.In, UsageOut: r.Usage.Out,
			UsageInOK: r.UsageInOK, UsageOutOK: r.UsageOutOK,
			RoleChars: r.RoleChars, RoleTokens: r.RoleTokens,
			NoReply: r.NoReply, TraceID: r.TraceID, ChatID: r.ChatID,
		}
	}
	for i, cr := range coldSess.Recs {
		if !reflect.DeepEqual(snap(cr), snap(warmSess.Recs[i])) {
			t.Errorf("rec[%d] differs between cold and warm:\ncold: %+v\nwarm: %+v", i, snap(cr), snap(warmSess.Recs[i]))
		}
	}
}

// priorFor builds a populated prior cache for path, simulating an earlier
// run over the same file.
func priorFor(t *testing.T, path string) *ctxgraph.FileCache {
	t.Helper()
	_, cache, err := AnalyzeSessionsCached([]string{path}, nil, taskseg.OpenClawAware)
	if err != nil {
		t.Fatalf("AnalyzeSessionsCached (prior): %v", err)
	}
	return cache
}

// TestAnalyzeSessions_WarmCacheNeverOpensAuditFile is the direct proof of
// the "hot run no-decode" guarantee. Setup: cold-run the fixture, then
// replace the file on disk with unparseable garbage and re-key the cache
// entry under the GARBAGE bytes' hash (so the hash gate matches). If
// analyzeFile still opens/decodes the file, the garbage decodes to zero
// records and the assertions below fail; success proves the cached facts
// were served without decoding the file.
func TestAnalyzeSessions_WarmCacheNeverOpensAuditFile(t *testing.T) {
	path, _ := fixture(t)

	coldSess, cache, err := AnalyzeSessionsCached([]string{path}, nil, taskseg.OpenClawAware)
	if err != nil {
		t.Fatalf("AnalyzeSessionsCached (cold): %v", err)
	}
	origKey := hashKey(t, path)

	// Corrupt the file on disk and re-key the cache entry under the garbage
	// bytes' hash, keeping the cold run's facts/manifests intact.
	if err := os.WriteFile(path, []byte("this is not JSON\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	garbageHash, err := ctxgraph.HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range cache.Files {
		if k == origKey {
			v.Hash = garbageHash
			cache.Files[garbageHash] = v
			delete(cache.Files, k)
			break
		}
	}

	// Warm run must succeed entirely from cache: the garbage on disk would
	// decode to zero records if it were opened, but the cached facts carry
	// the original 6 records.
	warmSess, _, err := AnalyzeSessionsCached([]string{path}, cache, taskseg.OpenClawAware)
	if err != nil {
		t.Fatalf("AnalyzeSessionsCached (warm over garbage bytes): %v", err)
	}
	if len(warmSess.Recs) != len(coldSess.Recs) {
		t.Fatalf("warm run record count = %d, want %d — the audit file was decoded despite a valid cache", len(warmSess.Recs), len(coldSess.Recs))
	}
	if len(warmSess.Sessions) != len(coldSess.Sessions) {
		t.Errorf("warm run session count = %d, want %d", len(warmSess.Sessions), len(coldSess.Sessions))
	}
}

// TestBuild_WarmPopulatesFactsCache proves Build's cold run
// leaves behind a non-empty Facts entry — the precondition
// factscache_test.go's TestScanFiles_CacheHitNeverOpensFile relies on to
// test the hit path in isolation, and by itself already enough to prove
// storeCachedFacts actually ran (a bug there would silently leave every
// future run paying the pre-P3.6 cost, without ever failing this loudly).
func TestBuild_WarmPopulatesFactsCache(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords())
	now := time.Now()

	_, _, cache, err := Build(BuildOptions{Paths: []string{path}, Now: now})
	if err != nil {
		t.Fatalf("Build (cold): %v", err)
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

// TestBuild_ChangedFileReparses mirrors ctxgraph's own
// TestScanCached_ChangedFileReparses at the report layer: appending to the
// input file must be picked up, not served stale from the prior cache.
func TestBuild_ChangedFileReparses(t *testing.T) {
	dir := t.TempDir()
	path := writeTempJSONL(t, dir, smallAuditRecords()[:2])
	now := time.Now()

	_, sess1, cache1, err := Build(BuildOptions{Paths: []string{path}, Now: now})
	if err != nil {
		t.Fatalf("Build (cold): %v", err)
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

	rep2, sess2, cache2, err := Build(BuildOptions{Paths: []string{path}, Now: now, PriorCache: cache1})
	if err != nil {
		t.Fatalf("Build (after append): %v", err)
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
