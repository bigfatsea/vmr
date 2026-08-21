// Ver 2026-08-05, by Sonnet 5

package ctxgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// CacheSchemaVersion gates every CachedFile's freshness alongside its
// content hash: a hash match alone only proves the *input bytes* haven't
// changed, not that the *extraction logic* that produced Manifests/Facts
// from those bytes hasn't. Bump this whenever that logic changes (either
// BuildManifest's own fields, or internal/report's per-record Facts
// payload — see that package's cache.go) — a version mismatch is treated
// exactly like a hash mismatch (scanCachedFile falls back to a fresh
// parse), so bumping it is always safe: the cache is a fully re-derivable
// artifact, and silently reusing output from retired logic is the failure
// mode this constant exists to prevent, not the rebuild cost.
const CacheSchemaVersion = 1

// CachedFile is one audit file's already-parsed scan result, keyed by its
// own content hash — see FileCache and ScanCached. Manifest carries no
// message bodies (hashes + a Path/Line pointer back to the original record
// for on-demand re-fetch — see records.go), so caching it is cheap: what's
// expensive to redo is BuildManifest's JSON decode + per-message hashing,
// not storing its (small) output.
type CachedFile struct {
	Hash          string `json:"hash"`
	SchemaVersion int    `json:"schema_version,omitempty"`
	// CanonicalPath is this entry's FileCache.Files map key (CanonicalPath
	// of whatever path spelling produced it), stored redundantly with the
	// map key itself so a sharded on-disk copy (see LoadCacheDir —
	// SaveCacheDir names each shard by Hash, not by this) can be loaded
	// back into the map without needing a separate index file: read every
	// shard, key it by its own embedded CanonicalPath.
	CanonicalPath string      `json:"canonical_path,omitempty"`
	Manifests     []*Manifest `json:"manifests,omitempty"`
	NoBody        int         `json:"no_body,omitempty"`
	// Facts is internal/report's own per-record aggregation payload
	// (its cache.go's fileFacts, marshaled) — opaque to this package on
	// purpose: ctxgraph knows the shared cache file's shape (so it can
	// round-trip this field on every read/write, including from
	// `vmr story`, which never populates or reads it), not
	// report-specific bucketing semantics. nil/absent means "no facts
	// cached yet for this file" (e.g. only `vmr story` has scanned it so
	// far, or report support predates this file's cache entry).
	Facts json.RawMessage `json:"facts,omitempty"`
}

// FileCache is a persisted, content-hash-keyed store of every audit file's
// CachedFile, indexed by path. It is never authoritative on its own — a
// missing or stale entry (path absent, or present with a different Hash)
// just means ScanCached falls back to parsing that one file fresh, exactly
// as Scan always has. Persisted as one file per entry under a shared
// .parse-cache/ directory — see LoadCacheDir/SaveCacheDir — so
// internal/story and internal/report (and any other caller sharing the
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
type fileCacheResult struct {
	path  string
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
// vmr-stories.json section), so this only ever skips the expensive
// per-file parse step, never any file from the graph itself.
//
// prior may be nil (no cache yet — everything is a miss, identical to
// calling Scan). The returned FileCache is prior's map with every path in
// this call's paths list overwritten (hit: same value; miss: the freshly
// computed one) — entries for paths NOT in this call are carried forward
// untouched, so a cache built from a wider (or different) file set doesn't
// lose those entries just because this call loaded fewer files.
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
		all = append(all, res.entry.Manifests...)
		noBody += res.entry.NoBody
		next.Files[res.path] = res.entry
	}
	return buildGraph(all, noBody), next, nil
}

// scanCachedFile resolves one path: a hash match against prior reuses its
// cached Manifests, anything else falls through to a fresh scanFile. A
// cache hit whose Manifests contain a nil element (a hand-edited or
// truncated-write-corrupted vmr-requests.json/vmr-stories.json can
// syntactically decode a `null` array entry without erroring) is treated
// as a miss rather than trusted as-is — buildGraph's sort dereferences
// every element, so a nil here would panic the whole scan instead of
// costing one file's worth of re-parse.
func scanCachedFile(path string, prior *FileCache) fileCacheResult {
	// key is the FileCache's identity for this file — CanonicalPath(path),
	// never the raw path used for I/O below: two runs of the same log file
	// invoked with an absolute path once and a relative path another time
	// must land in the same cache slot, or the cache accumulates duplicate
	// entries for what is, on disk, one file (see reqcoord.go's doc
	// comment). hash/scanFile still take the real path — they open it.
	key := CanonicalPath(path)
	hash, err := HashFile(path)
	if err != nil {
		return fileCacheResult{path: key, err: err}
	}
	if prior != nil {
		if cached, ok := prior.Files[key]; ok && cached.Hash == hash && cached.SchemaVersion == CacheSchemaVersion && !hasNilManifest(cached.Manifests) {
			// Rebind each Manifest's I/O path to this run's own path string
			// — a cache hit loaded from a prior run's persisted
			// vmr-requests.json/vmr-stories.json carries whatever path
			// spelling THAT run used (absolute, relative, a different
			// cwd...), and records.go's FetchRecords later opens
			// Manifest.Path directly to recover original record content.
			// Req is untouched: it's already
			// CanonicalPath(path)-based (see ReqCoord), so it's identical
			// under any path spelling and never needs rebinding.
			for _, m := range cached.Manifests {
				m.Path = path
			}
			cached.CanonicalPath = key
			return fileCacheResult{path: key, entry: cached}
		}
	}
	res := scanFile(path)
	if res.err != nil {
		return fileCacheResult{path: key, err: res.err}
	}
	return fileCacheResult{path: key, entry: CachedFile{Hash: hash, SchemaVersion: CacheSchemaVersion, CanonicalPath: key, Manifests: res.manifests, NoBody: res.noBody}}
}

func hasNilManifest(ms []*Manifest) bool {
	for _, m := range ms {
		if m == nil {
			return true
		}
	}
	return false
}

// LoadCacheDir reads dir (a shared .parse-cache/ directory — see
// FileCache's doc comment) into a FileCache: one CachedFile per <hash>.json
// shard, keyed by each shard's own embedded CanonicalPath rather than by
// the shard's filename. Best-effort the same way report's own now-removed
// file-cache loader used to be: a missing dir, or a shard that fails to
// parse, is skipped rather than failing the whole load — the cache is a
// fully re-derivable artifact, so a corrupt shard just costs that one
// file's worth of re-parse on the next scan, not a hard error.
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
		if err := json.Unmarshal(data, &cf); err != nil || cf.CanonicalPath == "" {
			continue
		}
		fc.Files[cf.CanonicalPath] = cf
	}
	if len(fc.Files) == 0 {
		return nil
	}
	return fc
}

// SaveCacheDir writes cache's entries into dir, one compact-encoded
// <hash>.json shard per entry — content-addressed by CachedFile.Hash, so a
// rerun over an unchanged file always names the same shard and (since
// shard content is a pure function of that hash) skip-if-exists is a
// correct, not approximate, dedup: two entries can never share a Hash
// with different content short of a SHA-256 collision. Stale shards from a
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
		if _, err := os.Stat(target); err == nil {
			continue
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
	tmp, err := os.CreateTemp(dir, ".parse-cache-*.tmp")
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
