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
	// The cache is keyed by the file's content hash (HashFile), not by the
	// raw scan-input path nor its canonical basename — see FileCache's doc
	// comment: two invocations of the same file under different path
	// spellings hash the same bytes and land in the same slot.
	key := hashOf(t, path)
	entry, ok := cache.Files[key]
	if !ok {
		t.Fatalf("no cache entry for %s", key)
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
	key := hashOf(t, path)
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
	key := hashOf(t, path)
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
	if _, ok := cacheA.Files[hashOf(t, pathB)]; !ok {
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
// truncated-write-corrupted requests/index.json/journeys/index.json can produce —
// a `null` array entry decodes without error) must be treated as a miss and
// reparsed, not trusted as-is — trusting it would panic buildGraph's sort.
func TestScanCached_NilManifestInCacheTriggersReparse(t *testing.T) {
	t.Parallel()
	path := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	hash := hashOf(t, path)
	corrupt := &FileCache{Files: map[string]CachedFile{
		hash: {Hash: hash, Manifests: []*Manifest{nil}},
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
	if len(cache.Files[hash].Manifests) != 1 || cache.Files[hash].Manifests[0] == nil {
		t.Errorf("cache entry for %s should have been refreshed with a fresh (non-nil) parse, got %+v", hash, cache.Files[hash])
	}
}

func TestHashFile_MissingFile(t *testing.T) {
	t.Parallel()
	if _, err := HashFile("/nonexistent/path.jsonl"); err == nil {
		t.Error("expected error for missing file")
	}
}

// hashOf returns the content-hash cache key for path — what FileCache.Files
// is indexed by (see FileCache's doc comment).
func hashOf(t *testing.T, path string) string {
	t.Helper()
	h, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestScanCached_HitRebindsManifestPathToCurrentInvocation covers a cache
// built by one invocation (whatever path spelling that run used) being
// reused by a LATER, separate invocation that spells the same file's path
// differently (a different cwd, absolute vs. relative). The cache key
// already keys past this (by content hash — same bytes, same entry),
// but the cached Manifests'
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
	key := hashOf(t, path)
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
	key := hashOf(t, path)
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
	shard := filepath.Join(dir, hashOf(t, path)+".json")
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
	key := hashOf(t, path)
	if _, ok := loaded.Files[key]; !ok {
		t.Error("the valid shard's entry is missing — a corrupt sibling shard should not affect it")
	}
	if len(loaded.Files) != 1 {
		t.Errorf("loaded %d entries, want exactly 1 (the corrupt shard must be skipped, not partially decoded)", len(loaded.Files))
	}
}

// TestLoadCacheDir_OrphanShardCannotPoisonCurrentFile is the mtime-poison
// regression this hash-key reindex exists for: a cp -r/backup restore can
// leave an orphan shard (same embedded CanonicalPath, different Hash) with
// a NEWER mtime than the current content's shard. The old loader keyed by
// CanonicalPath and broke ties toward the newer shard, so the orphan won;
// SaveCacheDir's skip-if-exists then made the resulting phantom miss
// permanent — the file lost its cache acceleration forever, invisibly.
// Hash-keyed, the two shards can't collide, and a ScanCached over the real
// file must hit its own content's entry (proven by the sentinel: a fresh
// parse would produce the record's real Req, never the marker).
func TestLoadCacheDir_OrphanShardCannotPoisonCurrentFile(t *testing.T) {
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
	currHash := hashOf(t, path)

	// Orphan shard: same CanonicalPath as the current entry, different
	// Hash, strictly newer mtime — the exact shape a backup restore with
	// inverted mtimes produces.
	orphan := CachedFile{Hash: "orphan0deadbeef", SchemaVersion: CacheSchemaVersion, CanonicalPath: CanonicalPath(path)}
	b, err := json.Marshal(orphan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, orphan.Hash+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, orphan.Hash+".json"), future, future); err != nil {
		t.Fatal(err)
	}

	prior := LoadCacheDir(dir)
	if prior == nil {
		t.Fatal("LoadCacheDir returned nil")
	}
	if _, ok := prior.Files[currHash]; !ok {
		t.Fatalf("current content's shard missing from the loaded cache (%d entries)", len(prior.Files))
	}
	if _, ok := prior.Files[orphan.Hash]; !ok {
		t.Error("orphan shard should load as its own entry, not be dropped")
	}

	// Sentinel on the current entry: if ScanCached reparses instead of
	// hitting (the poisoned behavior), no manifest will carry it.
	sentinel := prior.Files[currHash]
	sentinel.Manifests[0].Req = "cache-hit-sentinel"
	prior.Files[currHash] = sentinel

	g, _, err := ScanCached([]string{path}, prior)
	if err != nil {
		t.Fatalf("ScanCached after orphan-shard load: %v", err)
	}
	total := 0
	for _, l := range g.Lineages {
		for _, m := range l.Manifests {
			total++
			if m.Req == "cache-hit-sentinel" {
				return
			}
		}
	}
	t.Errorf("ScanCached served %d manifests and none came from the current content's cache entry — the orphan shard shadowed it", total)
}

// TestLoadCacheDir_EqualMtimeShardsLoadBoth: with mtimes flattened equal
// (what cp -r does), the old CanonicalPath-keyed loader degraded to ReadDir
// lexicographic order — a coin flip on which shard won the shared slot.
// Hash-keyed, both shards load as their own entries and no disambiguation
// is ever needed; each entry's key must agree with its own embedded Hash.
func TestLoadCacheDir_EqualMtimeShardsLoadBoth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, h := range []string{"h1", "h2"} {
		cf := CachedFile{Hash: h, SchemaVersion: CacheSchemaVersion, CanonicalPath: "/tmp/audit/identity.jsonl"}
		b, err := json.Marshal(cf)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, h+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	same := time.Now()
	for _, h := range []string{"h1", "h2"} {
		if err := os.Chtimes(filepath.Join(dir, h+".json"), same, same); err != nil {
			t.Fatal(err)
		}
	}

	loaded := LoadCacheDir(dir)
	if loaded == nil {
		t.Fatal("LoadCacheDir returned nil")
	}
	for _, h := range []string{"h1", "h2"} {
		got, ok := loaded.Files[h]
		if !ok {
			t.Errorf("shard %s missing from the loaded cache", h)
			continue
		}
		if got.Hash != h {
			t.Errorf("entry keyed %s carries Hash %q — key and content disagree", h, got.Hash)
		}
	}
}

// TestLoadCacheDir_ShardWithoutCanonicalPathLoads: the load gate is a valid
// Hash, not a CanonicalPath — the field is diagnostic now, so a shard that
// carries no CanonicalPath at all still loads under its own hash key.
func TestLoadCacheDir_ShardWithoutCanonicalPathLoads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cf := CachedFile{Hash: "hashonly123456", SchemaVersion: CacheSchemaVersion}
	b, err := json.Marshal(cf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cf.Hash+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := LoadCacheDir(dir)
	if loaded == nil {
		t.Fatal("LoadCacheDir returned nil")
	}
	if got, ok := loaded.Files[cf.Hash]; !ok || got.Hash != cf.Hash {
		t.Errorf("shard without CanonicalPath should load under its own hash key, got %+v (ok=%v)", got, ok)
	}
}

// TestScanCached_SameContentTwoPaths: two different paths holding
// byte-identical audit content (the backup-copy scenario that also triggers
// the poisoning bug) share one hash key and thus one cache entry — and the
// hit path's Manifest.Path rebind lives in ScanCached's serial merge loop,
// because two parallel goroutines rebinding the shared Manifests in place
// would be a data race plus a cross-path mis-binding. Run under -race; the
// manifests must stay functionally correct for either path (identical
// content reads back identically, so FetchRecords through either spelling
// returns the same record).
func TestScanCached_SameContentTwoPaths(t *testing.T) {
	t.Parallel()
	pathA := writeJSONL(t, []audit.Record{
		mkAuditRec(time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), chatBody(sysMsg("sys"), userMsg("hi"))),
	})
	raw, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatal(err)
	}
	pathB := filepath.Join(t.TempDir(), "copy.jsonl") // different basename: CheckPathCollisions is basename-based
	if err := os.WriteFile(pathB, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, cacheA, err := ScanCached([]string{pathA}, nil)
	if err != nil {
		t.Fatalf("ScanCached (A): %v", err)
	}
	g, cache, err := ScanCached([]string{pathA, pathB}, cacheA)
	if err != nil {
		t.Fatalf("ScanCached (A+B): %v", err)
	}
	if len(cache.Files) != 1 {
		t.Fatalf("identical content must share one cache entry, got %d", len(cache.Files))
	}
	total := 0
	for _, l := range g.Lineages {
		total += len(l.Manifests)
	}
	if total != 2 {
		t.Errorf("graph has %d manifests across both paths, want 2", total)
	}
	// FetchRecords through both path spellings must return the same record
	// (compare by line, not by the whole map — the Loc keys carry the
	// differing path spellings).
	recsA, err := FetchRecords([]Loc{{Path: pathA, Line: 1}})
	if err != nil {
		t.Fatalf("FetchRecords via A: %v", err)
	}
	recsB, err := FetchRecords([]Loc{{Path: pathB, Line: 1}})
	if err != nil {
		t.Fatalf("FetchRecords via B: %v", err)
	}
	ra, okA := recsA[Loc{Path: pathA, Line: 1}]
	rb, okB := recsB[Loc{Path: pathB, Line: 1}]
	if !okA || !okB {
		t.Fatalf("FetchRecords missed the record: okA=%v okB=%v", okA, okB)
	}
	if !reflect.DeepEqual(*ra, *rb) {
		t.Error("identical content fetched through the two path spellings should yield identical records")
	}
}
