// Ver 2026-07-10 00:00, by Sonnet 5

package imgprep

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"vmr/internal/rundir"
)

// CacheDir resolves the on-disk downscale cache directory: $VMR_IMG_CACHE_DIR
// if set (used exactly as given), else a vmr_image_cache subdirectory of the
// system temp dir — see internal/rundir for the full fallback chain, shared
// with audit.Dir so dev mode and service mode always agree on the default
// without vmr.sh keeping its own copy of this formula.
func CacheDir() string {
	return rundir.Resolve("VMR_IMG_CACHE_DIR", "vmr_image_cache", "image_cache")
}

// cacheFileName is the cache key: sha256 of the original (pre-downscale)
// image bytes, plus the target maxPx — the same source image downscaled for
// two different models (different maxPx overrides, §7) produces two
// different outputs and must not collide.
func cacheFileName(hash [32]byte, maxPx int) string {
	return hex.EncodeToString(hash[:]) + "-" + strconv.Itoa(maxPx) + ".jpg"
}

// cacheLookup returns the cached downscaled bytes for (hash, maxPx), if
// present. A hit's mtime is bumped to now so a long-running conversation
// that keeps re-sending the same image doesn't let TTL eviction (which
// looks at mtime, not creation time — sweepCacheDir below) expire an entry
// that is, in fact, still in active use. Best-effort: a failed touch doesn't
// invalidate the hit.
func cacheLookup(dir string, hash [32]byte, maxPx int) ([]byte, bool) {
	path := filepath.Join(dir, cacheFileName(hash, maxPx))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return data, true
}

// cacheStore best-effort writes data under (hash, maxPx). Failures (missing
// permissions, full disk) are silently ignored — the cache is purely an
// optimization, and the request has already succeeded with data in hand
// regardless of whether it lands on disk. Written to a uniquely-named temp
// file and renamed into place so a concurrent request computing the same
// entry can never observe a partially-written file.
func cacheStore(dir string, hash [32]byte, maxPx int, data []byte) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	name := cacheFileName(hash, maxPx)
	tmp, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, werr := tmp.Write(data); werr != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if cerr := tmp.Close(); cerr != nil {
		os.Remove(tmpName)
		return
	}
	if rerr := os.Rename(tmpName, filepath.Join(dir, name)); rerr != nil {
		os.Remove(tmpName)
	}
}

// sweepState throttles the TTL sweep to at most once per calendar day per
// cache directory — cheap enough that no dedicated ticker/goroutine is
// worth the extra lifetime management, mirroring the audit package's
// rotation-triggered housekeeping (internal/audit/housekeep.go), which is
// event-triggered rather than timer-driven for the same reason. Keyed by
// directory (not a single global flag) so tests using different t.TempDir()
// cache dirs never contend on each other's throttle state.
var sweepState sync.Map // dir string -> *sweepEntry

type sweepEntry struct {
	mu       sync.Mutex
	lastDate string
}

// maybeSweepCache kicks off an async sweepCacheDir at most once per day per
// dir. The date is recorded before the goroutine starts (not after it
// finishes) so concurrent requests within the same day never pile up
// retrying the check.
func maybeSweepCache(dir string, ttlDays int, now time.Time) {
	v, _ := sweepState.LoadOrStore(dir, &sweepEntry{})
	st := v.(*sweepEntry)
	today := now.Format("2006-01-02")
	st.mu.Lock()
	if st.lastDate == today {
		st.mu.Unlock()
		return
	}
	st.lastDate = today
	st.mu.Unlock()
	go sweepCacheDir(dir, ttlDays, now)
}

// sweepCacheDir deletes cache entries whose mtime is older than ttlDays, plus
// any stray ".tmp-" temp file left behind by a cacheStore that crashed
// mid-write (cacheStore's rename is atomic, but a kill -9 between CreateTemp
// and Rename leaks the temp file forever otherwise). Unlike audit's
// housekeeping, cache filenames carry no date (they're content hashes), so
// this is a single os.ReadDir followed by per-entry Info() — bounded by the
// number of cached images, which in practice is small (distinct source
// images actually seen, times the number of distinct maxPx values in use).
// Best-effort throughout: an unreadable/unremovable entry is skipped, not
// fatal. ttlDays<=0 disables the sweep (entries are kept forever).
func sweepCacheDir(dir string, ttlDays int, now time.Time) {
	if ttlDays <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // most common cause: cache dir doesn't exist yet (nothing cached so far)
	}
	cutoff := now.AddDate(0, 0, -ttlDays)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jpg") && !strings.Contains(name, ".tmp-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, name))
		}
	}
}
