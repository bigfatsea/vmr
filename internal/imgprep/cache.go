// Ver 2026-07-13 02:00, by Fable 5

package imgprep

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The cache directory comes from config.yaml's image_cache_dir (default:
// the persistent ~/.vmr/image_cache, resolved in config.applyDefaults via
// internal/rundir) and reaches this file through Options.CacheDir — there
// is no environment variable for it anymore. Persistent rather than the
// system temp dir on purpose: the cache's whole value is byte-stable reuse
// across days (upstream prompt caches key on exact bytes), and macOS
// purges temp entries after ~3 days of no access.

// cacheFileName is the cache key: sha256 of the original (pre-downscale)
// image bytes, plus the target maxPx — the same source image downscaled for
// two different models (different maxPx overrides) produces two
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

// sweepState throttles sweepCacheDir (TTL eviction plus the capacity cap) to
// at most once per calendar day per cache directory — cheap enough that no
// dedicated ticker/goroutine is worth the extra lifetime management,
// mirroring the audit package's rotation-triggered housekeeping
// (internal/audit/housekeep.go), which is event-triggered rather than
// timer-driven for the same reason. Keyed by directory (not a single global
// flag) so tests using different t.TempDir() cache dirs never contend on
// each other's throttle state.
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

// defaultCacheCapBytes is the total size cap (50MB) for the image cache
// directory. When the accumulated size of surviving cache entries exceeds
// this bound, the oldest entries by mtime are evicted until the total size
// is under the cap — independently of ttlDays below, since a user who
// disables time-based eviction (kept-forever) is exactly the case that most
// needs a disk-space backstop.
const defaultCacheCapBytes int64 = 50 << 20

// sweepCacheDir deletes cache entries whose mtime is older than ttlDays, plus
// any stray ".tmp-" temp file left behind by a cacheStore that crashed
// mid-write (cacheStore's rename is atomic, but a kill -9 between CreateTemp
// and Rename leaks the temp file forever otherwise), and enforces
// defaultCacheCapBytes by evicting the oldest entries once accumulated size
// exceeds it. Best-effort throughout: an unreadable/unremovable entry is
// skipped, not fatal. ttlDays<=0 disables only the time-based eviction
// (entries are kept forever by mtime); the capacity cap still applies. Runs
// at most once per calendar day per dir (maybeSweepCache's throttle), so the
// cap is an eventually-enforced bound, not a real-time one — a burst of
// distinct new images within a day can push the directory over 50MB until
// the next triggered sweep catches up.
func sweepCacheDir(dir string, ttlDays int, now time.Time) {
	sweepCacheDirWithCap(dir, ttlDays, now, defaultCacheCapBytes)
}

func sweepCacheDirWithCap(dir string, ttlDays int, now time.Time, capBytes int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // most common cause: cache dir doesn't exist yet (nothing cached so far)
	}
	ttlEnabled := ttlDays > 0
	var cutoff time.Time
	if ttlEnabled {
		cutoff = now.AddDate(0, 0, -ttlDays)
	}
	type fileItem struct {
		name  string
		size  int64
		mtime time.Time
	}
	var valid []fileItem
	var totalBytes int64

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
		path := filepath.Join(dir, name)
		if strings.Contains(name, ".tmp-") {
			if ttlEnabled && info.ModTime().Before(cutoff) {
				os.Remove(path)
			}
			continue
		}
		if ttlEnabled && info.ModTime().Before(cutoff) {
			os.Remove(path)
			continue
		}
		valid = append(valid, fileItem{name: name, size: info.Size(), mtime: info.ModTime()})
		totalBytes += info.Size()
	}

	if capBytes > 0 && totalBytes > capBytes {
		sort.Slice(valid, func(i, j int) bool {
			return valid[i].mtime.Before(valid[j].mtime)
		})
		for _, f := range valid {
			if totalBytes <= capBytes {
				break
			}
			if err := os.Remove(filepath.Join(dir, f.name)); err == nil {
				totalBytes -= f.size
			}
		}
	}
}
