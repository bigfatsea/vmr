// Ver 2026-09-23 04:15, by Claude Opus 5.5

package ctxgraph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// CacheSchemaVersion gates every CachedFile's Manifest freshness: a shard
// stamped with any other version is ignored and re-parsed. Bump it whenever
// BuildManifest's output changes meaning or shape for the same input bytes
// (a new field, a changed hashing or attribution rule) — a stale shard would
// otherwise keep serving the old answer for as long as the audit file is
// unchanged. report's own payload is versioned separately (FactsVersion).
const CacheSchemaVersion = 15

// CachedFile is one audit file's already-parsed scan result, keyed by its
// own content hash — see FileCache and ScanCached. Manifest carries no
// message bodies (hashes + a Path/Line pointer back to the original record
// for on-demand re-fetch — see records.go), so caching it is cheap: what's
// expensive to redo is BuildManifest's JSON decode + per-message hashing,
// not storing its (small) output.
type CachedFile struct {
	Hash          string `json:"hash"`
	SchemaVersion int    `json:"schema_version,omitempty"`
	// FactsVersion is internal/report's own payload version for the Facts
	// blob — stamped by report's FactsSchemaVersion, validated by report on
	// load. Kept separate from SchemaVersion (which gates the whole entry
	// including Manifests) so a report-side extraction change can invalidate
	// only the facts payload without forcing a manifest reparse.
	FactsVersion int `json:"facts_version,omitempty"`
	// CanonicalPath is diagnostic only: this run's CanonicalPath spelling
	// of the source file (see reqcoord.go). It is no longer the map key —
	// FileCache.Files keys by Hash — and is kept only so a sharded on-disk
	// copy (see LoadCacheDir — SaveCacheDir names each shard by Hash, not
	// by this) still says which audit file it was built from. It goes stale
	// the moment the same content is re-scanned under a different path
	// spelling, so nothing may branch on it.
	CanonicalPath string      `json:"canonical_path,omitempty"`
	Manifests     []*Manifest `json:"manifests,omitempty"`
	NoBody        int         `json:"no_body,omitempty"`
	// Facts is internal/report's own per-record aggregation payload
	// (its cache.go's fileFacts, marshaled) — opaque to this package on
	// purpose: ctxgraph knows the shared cache file's shape (so it can
	// round-trip this field on every read/write, including from
	// the journey half, which never populates or reads it), not
	// report-specific bucketing semantics. nil/absent means "no facts
	// cached yet for this file" (e.g. only the journey half has scanned it so
	// far, or report support predates this file's cache entry).
	Facts json.RawMessage `json:"facts,omitempty"`
}

// FileCache is a persisted store of every audit file's CachedFile: Files
// maps HashFile(path)'s hex sha256 to that file's entry. Keying by content
// — not by path spelling, and not by mtime — is what makes a cp -r/backup
// restore unable to poison the cache: a stale shard hashes differently and
// gets its own key, so it can never shadow the current content's entry (the
// mtime disambiguation this keying replaced degenerated to a coin flip when
// mtimes were equal, and picked the wrong winner when a restore inverted
// them). It is never authoritative on its own — a missing or stale entry
// just means ScanCached falls back to parsing that one file fresh, exactly
// as Scan always has. Persisted as one file per entry under a shared
// .cache/parse/ directory — see LoadCacheDir/SaveCacheDir — so
// internal/journey and internal/report (and any other caller sharing the
// same output directory) read and write the exact same on-disk cache
// instead of each keeping an independent copy.
type FileCache struct {
	Files map[string]CachedFile `json:"files"`
}

// HashFile returns the hex sha256 of path's raw on-disk bytes — deliberately
// the bytes as stored, not the decompressed logical content: a plain
// .jsonl that housekeeping later recompresses to .jsonl.zst gets a new path
// and new bytes, so it naturally misses the cache once and reparses (a
// one-time cost at the moment of rotation) rather than needing this cache
// to understand compression at all. Cheap relative to what a cache hit
// skips: a single streaming pass, no JSON decode/allocation.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fileCacheResult is one path's ScanCached outcome — parsed fresh or
// reused from prior — before merging into the run's full Manifest set.
// path is kept alongside the hash key: the merge loop needs the real path
// (for Manifest.Path rebinding), not just the cache key.
type fileCacheResult struct {
	path  string
	hash  string
	entry CachedFile
	err   error
}

// ScanCached is Scan, plus a file-hash-keyed fast path: for each path whose
// current on-disk hash matches prior's recorded hash, the cached Manifests
// are reused as-is and BuildManifest never runs for that file; every other
// path (hash mismatch, or simply absent from prior) is parsed exactly like
// Scan does. Either way, ALL paths' Manifests (cache-sourced or freshly
// parsed) are merged into one set before buildGraph runs — this is
// deliberate, not an optimization left on the table: bucketing/lineage-
// splitting/stitching need to see the whole graph to be correct (same
// reason a narrower file selection based on a Journey id's own embedded
// timestamp isn't safe — see docs/VirtualModelRouter_Design_v4_Analytics.md's
// journeys/index.json section), so this only ever skips the expensive
// per-file parse step, never any file from the graph itself.
//
// prior may be nil (no cache yet — everything is a miss, identical to
// calling Scan). The returned FileCache is prior's map with one entry per
// distinct content hash among this call's paths overwritten (hit: same
// value; miss: the freshly computed one) — entries whose hash is not among
// this call's files are carried forward untouched, so a cache built from a
// wider (or different) file set doesn't lose those entries just because
// this call loaded fewer files.
func ScanCached(paths []string, prior *FileCache) (*Graph, *FileCache, error) {
	if err := CheckPathCollisions(paths); err != nil {
		return nil, nil, err
	}
	next := &FileCache{Files: make(map[string]CachedFile, len(paths))}
	if prior != nil {
		for k, v := range prior.Files {
			next.Files[k] = v
		}
	}

	results := make([]fileCacheResult, len(paths))
	sem := make(chan struct{}, scanWorkerCount(len(paths)))
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = scanCachedFile(path, prior)
		}(i, path)
	}
	wg.Wait()

	var all []*Manifest
	noBody := 0
	for _, res := range results {
		if res.err != nil {
			return nil, nil, res.err
		}
		// Rebind each cached Manifest's I/O path to this run's own path
		// spelling — a cache hit loaded from a prior run's persisted cache
		// carries whatever path spelling THAT run used (absolute, relative,
		// a different cwd...), and records.go's FetchRecords later opens
		// Manifest.Path directly to recover original record content. Done
		// here in the serial merge loop, not in the parallel scan: two
		// same-content paths share one hash key/entry, and each goroutine
		// rebinding the shared Manifests in place would be a data race plus
		// a cross-path mis-binding. Identical content reads back identically
		// through either path, so the last-writer binding is functionally
		// correct; a fresh parse already carries its own real path, so this
		// rebinding is a harmless no-op for it. Req is untouched: it's
		// already CanonicalPath(path)-based (see ReqCoord), so it's
		// identical under any path spelling and never needs rebinding.
		for _, m := range res.entry.Manifests {
			m.Path = res.path
		}
		all = append(all, res.entry.Manifests...)
		noBody += res.entry.NoBody
		next.Files[res.hash] = res.entry
	}
	return buildGraph(all, noBody), next, nil
}

// scanCachedFile resolves one path: a hash match against prior reuses its
// cached Manifests, anything else falls through to a fresh scanFile. A
// cache hit whose Manifests contain a nil element (a hand-edited or
// truncated-write-corrupted requests/index.json/journeys/index.json can
// syntactically decode a `null` array entry without erroring) is treated
// as a miss rather than trusted as-is — buildGraph's sort dereferences
// every element, so a nil here would panic the whole scan instead of
// costing one file's worth of re-parse.
func scanCachedFile(path string, prior *FileCache) fileCacheResult {
	// key is the FileCache's identity for this file: HashFile's hex sha256
	// of the on-disk bytes — content, not path spelling and not mtime. Two
	// invocations of the same file under different path spellings hash the
	// same bytes and share one slot; a cp -r/backup-restored stale copy
	// hashes different bytes and gets its own slot instead of shadowing
	// the current content's entry. hash/scanFile still take the real path
	// — they open it.
	hash, err := HashFile(path)
	if err != nil {
		return fileCacheResult{path: path, err: err}
	}
	if prior != nil {
		if cached, ok := prior.Files[hash]; ok && cached.SchemaVersion == CacheSchemaVersion && !hasNilManifest(cached.Manifests) {
			// Manifest.Path rebinding happens in ScanCached's serial merge
			// loop, not here — same-content paths share this entry, and
			// rebinding the shared Manifests from a parallel goroutine
			// would race. cached is a map-value copy, so stamping the
			// diagnostic CanonicalPath here touches nothing shared.
			cached.CanonicalPath = CanonicalPath(path)
			return fileCacheResult{path: path, hash: hash, entry: cached}
		}
	}
	res := scanFile(path)
	if res.err != nil {
		return fileCacheResult{path: path, hash: hash, err: res.err}
	}
	return fileCacheResult{path: path, hash: hash, entry: CachedFile{Hash: hash, SchemaVersion: CacheSchemaVersion, CanonicalPath: CanonicalPath(path), Manifests: res.manifests, NoBody: res.noBody}}
}

func hasNilManifest(ms []*Manifest) bool {
	for _, m := range ms {
		if m == nil {
			return true
		}
	}
	return false
}

// LoadCacheDir reads dir (a shared .cache/parse/ directory — see
// FileCache's doc comment) into a FileCache: one CachedFile per <hash>.json
// shard, keyed by each shard's own embedded Hash — the same key SaveCacheDir
// names the shard file by, so the map and the directory stay aligned.
// Best-effort the same way report's own now-removed file-cache loader used
// to be: a missing dir, or a shard that fails to parse, is skipped rather
// than failing the whole load — the cache is a fully re-derivable artifact,
// so a corrupt shard just costs that one file's worth of re-parse on the
// next scan, not a hard error. No cross-shard disambiguation is needed (or
// performed): shards never share a Hash key, so an orphan shard left behind
// by a cp -r/backup restore simply loads as its own entry and can never
// shadow the current content's shard — the worst an orphan can cause is a
// hash miss (one re-parse), not a poisoned hit.
func LoadCacheDir(dir string) *FileCache {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	fc := &FileCache{Files: map[string]CachedFile{}}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var cf CachedFile
		if err := json.Unmarshal(data, &cf); err != nil || cf.Hash == "" {
			continue
		}
		fc.Files[cf.Hash] = cf
	}
	if len(fc.Files) == 0 {
		return nil
	}
	return fc
}

// SaveCacheDir writes cache's entries into dir, one compact-encoded
// <hash>.json shard per entry — content-addressed by CachedFile.Hash, so a
// rerun over an unchanged file always names the same shard. A shard that is
// already on disk with the same SchemaVersion/FactsVersion/Facts is skipped
// (the common unchanged-file repeat run); a shard whose Facts half is
// missing or stale (e.g. written by the journey half before report ran, or
// written under an older FactsSchemaVersion) is refreshed in place —
// report's facts are a per-half cache slot, not a pure function of the
// audit file bytes alone. Stale shards from a
// since-rotated/renamed input are deliberately never deleted here — see
// this package's doc comment on FileCache: the directory is a fully
// re-derivable, unreferenced-counted cache, and a few orphaned KB-scale
// shards cost nothing worth a cleanup pass for.
func SaveCacheDir(dir string, cache *FileCache) error {
	if cache == nil || len(cache.Files) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, cf := range cache.Files {
		if cf.Hash == "" {
			continue // nothing to name the shard after; shouldn't happen for a real entry
		}
		target := filepath.Join(dir, cf.Hash+".json")
		if data, err := os.ReadFile(target); err == nil {
			var diskCF CachedFile
			if json.Unmarshal(data, &diskCF) == nil &&
				diskCF.SchemaVersion == cf.SchemaVersion &&
				diskCF.FactsVersion == cf.FactsVersion &&
				bytes.Equal(diskCF.Facts, cf.Facts) {
				continue
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		data, err := json.Marshal(cf)
		if err != nil {
			return err
		}
		if err := writeCacheShardAtomic(dir, target, data); err != nil {
			return err
		}
	}
	return nil
}

// writeCacheShardAtomic is the same temp-file-then-rename pattern
// internal/quota's Registry.Flush and internal/reqdetail's writeFileAtomic
// use, reimplemented locally (not imported — internal/reqdetail depends on
// this package, not the other way around) so a killed process never leaves
// a half-written shard that a later LoadCacheDir would trip over.
func writeCacheShardAtomic(dir, target string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "parse-shard-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
