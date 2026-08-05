// Ver 2026-08-05, by Sonnet 5

package ctxgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

// CachedFile is one audit file's already-parsed scan result, keyed by its
// own content hash — see FileCache and ScanCached. Manifest carries no
// message bodies (hashes + a Path/Line pointer back to the original record
// for on-demand re-fetch — see records.go), so caching it is cheap: what's
// expensive to redo is BuildManifest's JSON decode + per-message hashing,
// not storing its (small) output.
type CachedFile struct {
	Hash      string      `json:"hash"`
	Manifests []*Manifest `json:"manifests,omitempty"`
	NoBody    int         `json:"no_body,omitempty"`
}

// FileCache is a persisted, content-hash-keyed store of every audit file's
// CachedFile, indexed by path. It is never authoritative on its own — a
// missing or stale entry (path absent, or present with a different Hash)
// just means ScanCached falls back to parsing that one file fresh, exactly
// as Scan always has. internal/story and internal/report each persist a
// FileCache as the "files" section of their own index artifact
// (vmr-stories.json / vmr-requests.json) rather than a separate hidden
// cache file — see those packages for the (de)serialization to a specific
// named file; this package only knows how to use the in-memory value.
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
	hash, err := HashFile(path)
	if err != nil {
		return fileCacheResult{path: path, err: err}
	}
	if prior != nil {
		if cached, ok := prior.Files[path]; ok && cached.Hash == hash && !hasNilManifest(cached.Manifests) {
			return fileCacheResult{path: path, entry: cached}
		}
	}
	res := scanFile(path)
	if res.err != nil {
		return fileCacheResult{path: path, err: res.err}
	}
	return fileCacheResult{path: path, entry: CachedFile{Hash: hash, Manifests: res.manifests, NoBody: res.noBody}}
}

func hasNilManifest(ms []*Manifest) bool {
	for _, m := range ms {
		if m == nil {
			return true
		}
	}
	return false
}
