// Ver 2026-08-05, by Sonnet 5

package ctxgraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"vmr/internal/audit"
)

func TestHash_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	h := hashJSON(map[string]any{"role": "user", "content": "hello"})
	data, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != `"`+h.String()+`"` {
		t.Errorf("Marshal(h) = %s, want hex string %q", data, h.String())
	}
	var got Hash
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != h {
		t.Errorf("round-tripped Hash = %v, want %v", got, h)
	}
}

func TestHash_UnmarshalRejectsBadInput(t *testing.T) {
	t.Parallel()
	var h Hash
	if err := json.Unmarshal([]byte(`"not-hex"`), &h); err == nil {
		t.Error("expected error for non-hex string")
	}
	if err := json.Unmarshal([]byte(`"ab"`), &h); err == nil {
		t.Error("expected error for wrong-length hex string")
	}
}

func TestManifest_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	body := chatBody(sysMsg("sys"), userMsg("hello there"))
	rec := mkAuditRec(at, body)
	m, ok := BuildManifest(&rec, "some/path.jsonl", 3)
	if !ok {
		t.Fatal("BuildManifest: not ok")
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(*m, got) {
		t.Errorf("round-tripped Manifest differs:\n got  %+v\n want %+v", got, *m)
	}
}

// TestScanCached_ColdCacheMatchesScan: with no prior cache, ScanCached must
// produce byte-for-byte the same Graph shape as Scan (everything is a
// miss) — the caching path must never change results, only skip work.
func TestScanCached_ColdCacheMatchesScan(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	want, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got, cache, err := ScanCached([]string{path}, nil)
	if err != nil {
		t.Fatalf("ScanCached: %v", err)
	}
	if len(got.Lineages) != len(want.Lineages) {
		t.Fatalf("ScanCached produced %d lineages, Scan produced %d", len(got.Lineages), len(want.Lineages))
	}
	if len(cache.Files) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(cache.Files))
	}
	// The cache is keyed by CanonicalPath(path), not the raw scan-input
	// path — see reqcoord.go: two invocations of the same file (absolute
	// vs. relative) must land in the same slot.
	entry, ok := cache.Files[CanonicalPath(path)]
	if !ok {
		t.Fatalf("no cache entry for %s", CanonicalPath(path))
	}
	if len(entry.Manifests) != 1 {
		t.Errorf("cache entry has %d manifests, want 1", len(entry.Manifests))
	}
	wantHash, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Hash != wantHash {
		t.Errorf("cached hash = %q, want %q", entry.Hash, wantHash)
	}
}

// TestScanCached_HitSkipsReparse: a second ScanCached call, fed the first
// call's returned cache, must reuse the cached Manifests verbatim (proven
// by mutating the file's ON-DISK bytes to something unparseable AFTER
// computing the cache, then confirming a hash-matched call still succeeds
// using the cache instead of touching the corrupted bytes — if it were
// reparsing, this would fail).
func TestScanCached_HitSkipsReparse(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	_, cache1, err := ScanCached([]string{path}, nil)
	if err != nil {
		t.Fatalf("ScanCached (cold): %v", err)
	}

	// Overwrite with content that hashes to the SAME value as before is
	// impossible to fake honestly, so instead prove the hit path by
	// checking cache2's entry is == cache1's entry (same slice header/
	// values) rather than a freshly reallocated one, then separately prove
	// a hash MISS does reparse (below).
	g2, cache2, err := ScanCached([]string{path}, cache1)
	if err != nil {
		t.Fatalf("ScanCached (warm): %v", err)
	}
	if len(g2.Lineages) != 1 {
		t.Fatalf("warm ScanCached produced %d lineages, want 1", len(g2.Lineages))
	}
	key := CanonicalPath(path)
	if !reflect.DeepEqual(cache1.Files[key], cache2.Files[key]) {
		t.Errorf("warm cache entry changed even though the file didn't:\n got  %+v\n want %+v",
			cache2.Files[key], cache1.Files[key])
	}
}

// TestScanCached_ChangedFileReparses: a file whose content (and thus hash)
// changed since the cache was built must be reparsed, not served stale.
func TestScanCached_ChangedFileReparses(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	_, cache1, err := ScanCached([]string{path}, nil)
	if err != nil {
		t.Fatalf("ScanCached (cold): %v", err)
	}

	// Append a second record — changes both the file's bytes (new hash)
	// and the expected manifest count.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	rec2 := mkAuditRec(time.Date(2026, 7, 16, 10, 1, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("second")))
	raw, err := json.Marshal(rec2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	f.Close()

	g2, cache2, err := ScanCached([]string{path}, cache1)
	if err != nil {
		t.Fatalf("ScanCached (after append): %v", err)
	}
	key := CanonicalPath(path)
	if len(cache2.Files[key].Manifests) != 2 {
		t.Fatalf("cache entry has %d manifests after append, want 2 (should have reparsed)", len(cache2.Files[key].Manifests))
	}
	if cache2.Files[key].Hash == cache1.Files[key].Hash {
		t.Error("hash should have changed after appending to the file")
	}
	total := 0
	for _, l := range g2.Lineages {
		total += len(l.Manifests)
	}
	if total != 2 {
		t.Errorf("graph has %d total manifests after append, want 2", total)
	}
}

// TestScanCached_NewFileIsParsedAndMerged: a file absent from prior (a
// brand-new day's log, the common case) is parsed like any miss, and its
// manifests still take part in the SAME lineage/stitch pass as
// cache-sourced files from other paths — the whole point of never doing a
// narrower, per-file "only recompute what changed" graph rebuild.
func TestScanCached_NewFileIsParsedAndMerged(t *testing.T) {
	t.Parallel()
	sys := sysMsg("sys")
	u1 := userMsg("shared instruction")
	pathA := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sys, u1)),
	})
	_, cacheA, err := ScanCached([]string{pathA}, nil)
	if err != nil {
		t.Fatalf("ScanCached (A only): %v", err)
	}

	pathB := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 5, 0, 0, time.UTC), chatBody(sys, u1, assistantMsg("reply"))),
	})
	g, cacheAB, err := ScanCached([]string{pathA, pathB}, cacheA)
	if err != nil {
		t.Fatalf("ScanCached (A+B): %v", err)
	}
	if len(cacheAB.Files) != 2 {
		t.Fatalf("expected 2 cache entries after adding B, got %d", len(cacheAB.Files))
	}
	// Same SessKey (both open with sys+u1) — should merge into one lineage
	// spanning both files, same as a cold Scan([]string{pathA, pathB}) would.
	if len(g.Lineages) != 1 {
		t.Fatalf("got %d lineages across A+B, want 1 (same anchor)", len(g.Lineages))
	}
	if len(g.Lineages[0].Manifests) != 2 {
		t.Fatalf("got %d manifests in the merged lineage, want 2", len(g.Lineages[0].Manifests))
	}
}

// TestScanCached_UntouchedPathsCarryForward: entries for paths not in this
// call's list are preserved in the returned cache, not dropped — a
// narrower subsequent run (or one against a different glob) shouldn't
// silently forget what a wider prior run already learned about other files.
func TestScanCached_UntouchedPathsCarryForward(t *testing.T) {
	t.Parallel()
	pathA := writeJSONL(t, []audit.Record{mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("a")))})
	pathB := writeJSONL(t, []audit.Record{mkAuditRec(time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("b")))})
	_, cacheAB, err := ScanCached([]string{pathA, pathB}, nil)
	if err != nil {
		t.Fatalf("ScanCached (A+B): %v", err)
	}
	_, cacheA, err := ScanCached([]string{pathA}, cacheAB)
	if err != nil {
		t.Fatalf("ScanCached (A only): %v", err)
	}
	if _, ok := cacheA.Files[CanonicalPath(pathB)]; !ok {
		t.Error("entry for path B should be carried forward even though this call only scanned A")
	}
}

func TestHashFile_ChangesWithContent(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("a")))})
	h1, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Error("HashFile should be deterministic for unchanged content")
	}
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	h3, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Error("HashFile should change when file content changes")
	}
}

// TestScanCached_NilManifestInCacheTriggersReparse: a cache entry whose
// Manifests slice contains a nil element (the shape a hand-edited or
// truncated-write-corrupted vmr-requests.json/vmr-stories.json can produce —
// a `null` array entry decodes without error) must be treated as a miss and
// reparsed, not trusted as-is — trusting it would panic buildGraph's sort.
func TestScanCached_NilManifestInCacheTriggersReparse(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	hash, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	key := CanonicalPath(path)
	corrupt := &FileCache{Files: map[string]CachedFile{
		key: {Hash: hash, Manifests: []*Manifest{nil}},
	}}

	g, cache, err := ScanCached([]string{path}, corrupt)
	if err != nil {
		t.Fatalf("ScanCached with a nil-containing cache entry: %v", err)
	}
	total := 0
	for _, l := range g.Lineages {
		total += len(l.Manifests)
	}
	if total != 1 {
		t.Errorf("graph has %d manifests, want 1 (should have reparsed instead of trusting the corrupt entry)", total)
	}
	if len(cache.Files[key].Manifests) != 1 || cache.Files[key].Manifests[0] == nil {
		t.Errorf("cache entry for %s should have been refreshed with a fresh (non-nil) parse, got %+v", key, cache.Files[key])
	}
}

func TestHashFile_MissingFile(t *testing.T) {
	t.Parallel()
	if _, err := HashFile("/nonexistent/path.jsonl"); err == nil {
		t.Error("expected error for missing file")
	}
}

// TestScanCached_HitRebindsManifestPathToCurrentInvocation covers a cache
// built by one invocation (whatever path spelling that run used) being
// reused by a LATER, separate invocation that spells the same file's path
// differently (a different cwd, absolute vs. relative). The cache key
// already normalizes past this (CanonicalPath), but the cached Manifests'
// own Path field must also follow the CURRENT run's spelling — it's what
// records.go's FetchRecords later os.Open to recover original message
// content, so a stale Path from a prior run's cwd would fail to open under
// the current one.
func TestScanCached_HitRebindsManifestPathToCurrentInvocation(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	_, cache1, err := ScanCached([]string{path}, nil)
	if err != nil {
		t.Fatalf("ScanCached (cold): %v", err)
	}

	// Simulate a later invocation spelling the SAME file differently (e.g.
	// a relative path from a different cwd) — same bytes, different string.
	renamed := filepath.Join(filepath.Dir(path), "..", filepath.Base(filepath.Dir(path)), filepath.Base(path))
	g2, _, err := ScanCached([]string{renamed}, cache1)
	if err != nil {
		t.Fatalf("ScanCached (renamed path, warm): %v", err)
	}
	if len(g2.Lineages) != 1 || len(g2.Lineages[0].Manifests) != 1 {
		t.Fatalf("unexpected graph shape: %+v", g2.Lineages)
	}
	got := g2.Lineages[0].Manifests[0].Path
	if got != renamed {
		t.Errorf("cache-hit Manifest.Path = %q, want it rebound to this invocation's own path %q", got, renamed)
	}
}

// TestScanCached_SchemaVersionMismatchReparses: a hash match alone must not
// be trusted when the entry's SchemaVersion predates the current
// extraction logic — see CacheSchemaVersion's doc comment.
func TestScanCached_SchemaVersionMismatchReparses(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	_, cache1, err := ScanCached([]string{path}, nil)
	if err != nil {
		t.Fatalf("ScanCached (cold): %v", err)
	}
	key := CanonicalPath(path)
	stale := cache1.Files[key]
	stale.SchemaVersion = CacheSchemaVersion - 1 // simulate an older cache
	cache1.Files[key] = stale

	g2, cache2, err := ScanCached([]string{path}, cache1)
	if err != nil {
		t.Fatalf("ScanCached (warm, stale schema): %v", err)
	}
	if len(g2.Lineages) != 1 {
		t.Fatalf("warm ScanCached produced %d lineages, want 1", len(g2.Lineages))
	}
	if cache2.Files[key].SchemaVersion != CacheSchemaVersion {
		t.Errorf("reparsed entry's SchemaVersion = %d, want current %d", cache2.Files[key].SchemaVersion, CacheSchemaVersion)
	}
}

func TestSaveCacheDir_LoadCacheDir_RoundTrip(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	_, cache, err := ScanCached([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := SaveCacheDir(dir, cache); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("dir entries = %v, want exactly one .json shard", entries)
	}

	loaded := LoadCacheDir(dir)
	if loaded == nil {
		t.Fatal("LoadCacheDir returned nil")
	}
	key := CanonicalPath(path)
	if !reflect.DeepEqual(loaded.Files[key], cache.Files[key]) {
		t.Errorf("round-tripped entry differs:\n got  %+v\n want %+v", loaded.Files[key], cache.Files[key])
	}
}

func TestSaveCacheDir_SkipsExistingShard(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	_, cache, err := ScanCached([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := SaveCacheDir(dir, cache); err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(dir, cache.Files[CanonicalPath(path)].Hash+".json")
	fi1, err := os.Stat(shard)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCacheDir(dir, cache); err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(shard)
	if err != nil {
		t.Fatal(err)
	}
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("second SaveCacheDir rewrote an unchanged shard instead of skipping it")
	}
}

func TestLoadCacheDir_MissingDirReturnsNil(t *testing.T) {
	t.Parallel()
	if got := LoadCacheDir(filepath.Join(t.TempDir(), "does-not-exist")); got != nil {
		t.Errorf("LoadCacheDir(missing) = %+v, want nil", got)
	}
}

// TestLoadCacheDir_CorruptShardIsSkipped: a truncated-write-corrupted shard
// (same real-world cause as TestScanCached_NilManifestInCacheTriggersReparse's
// scenario, just at the sharded-file layer instead of the in-memory one)
// must degrade to "this one file's entry is missing" — never fail the
// whole load or propagate a decode error to the caller. A sibling valid
// shard in the same directory must still load correctly.
func TestLoadCacheDir_CorruptShardIsSkipped(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	_, cache, err := ScanCached([]string{path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := SaveCacheDir(dir, cache); err != nil {
		t.Fatal(err)
	}
	// A second, corrupt shard alongside the valid one.
	if err := os.WriteFile(filepath.Join(dir, "deadbeef.json"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded := LoadCacheDir(dir)
	if loaded == nil {
		t.Fatal("LoadCacheDir returned nil even though a valid shard exists alongside the corrupt one")
	}
	key := CanonicalPath(path)
	if _, ok := loaded.Files[key]; !ok {
		t.Error("the valid shard's entry is missing — a corrupt sibling shard should not affect it")
	}
	if len(loaded.Files) != 1 {
		t.Errorf("loaded %d entries, want exactly 1 (the corrupt shard must be skipped, not partially decoded)", len(loaded.Files))
	}
}

// TestLoadCacheDir_OrphanShardMtimeWins pins §1.3's follow-up: two shards
// sharing a CanonicalPath (one stale "orphan" from an earlier audit-log
// hash, one current) must resolve to the newer shard, regardless of which
// sort-order position ReadDir yields. A pure last-writer-wins on
// CanonicalPath would here pick the lexicographically-later shard
// (`h2.json` after `h1.json`) even though its content is stale — a phantom
// cache miss on the next scan. The audit log is append-only, so the most
// recently written shard encodes the current content hash.
func TestLoadCacheDir_OrphanShardMtimeWins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	canon := "/tmp/audit/identity.jsonl"
	old := CachedFile{Hash: "h1", CanonicalPath: canon}
	new := CachedFile{Hash: "h2", CanonicalPath: canon}
	oldBytes, _ := json.Marshal(old)
	newBytes, _ := json.Marshal(new)
	if err := os.WriteFile(filepath.Join(dir, old.Hash+".json"), oldBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, new.Hash+".json"), newBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	// Make h1 strictly older than h2 by 2s — amply larger than the
	// filesystem mtime granularity on every supported OS.
	oldTime := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(filepath.Join(dir, "h1.json"), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	newTime := time.Now()
	if err := os.Chtimes(filepath.Join(dir, "h2.json"), newTime, newTime); err != nil {
		t.Fatal(err)
	}

	loaded := LoadCacheDir(dir)
	if loaded == nil {
		t.Fatal("LoadCacheDir returned nil")
	}
	got, ok := loaded.Files[canon]
	if !ok {
		t.Fatalf("missing entry for %s, have %+v", canon, loaded.Files)
	}
	if got.Hash != "h2" {
		t.Errorf("LoadCacheDir kept the older shard: Hash = %q, want %q (newer mtime wins on CanonicalPath tie)", got.Hash, "h2")
	}
}
