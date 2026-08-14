// Ver 2026-08-13 16:39, by Gemini 3.6 Flash

package quota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultFlushInterval is the standard background persistence period for quota counters.
const DefaultFlushInterval = 10 * time.Second

// fileVersion is bumped whenever the on-disk shape changes incompatibly —
// left at 1 for the whole of P1 (see the design doc's Persistence section).
const fileVersion = 1

// fileFormat is quota state's on-disk shape: <log_dir>/vmr-quota.json.
// Deliberately mirrors Registry.accounts field-for-field rather than adding
// a translation layer — there is exactly one shape, used both in memory and
// on disk.
type fileFormat struct {
	Version  int                           `json:"version"`
	Accounts map[string]map[string]*bucket `json:"accounts"`
}

// Load reads Registry's persisted state from its path, if any. A missing
// file is not an error (the first run on a fresh install) and returns nil.
// A present-but-corrupt file DOES return an error — but per the design
// doc's Persistence section, a statistics helper must never be able to
// stall routing, so the caller (cmd_start.go) only logs this and proceeds
// with an empty Registry either way; Load itself never mutates state on the
// error path.
func (r *Registry) Load() error {
	if r.path == "" {
		return nil
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var ff fileFormat
	if err := json.Unmarshal(data, &ff); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ff.Accounts != nil {
		r.accounts = ff.Accounts
	}
	return nil
}

// Flush atomically writes the current state to Registry's path. A no-op
// when nothing has changed since the last successful Flush (Charge is the
// only thing that sets dirty) or when Registry has no path (tests, or a
// Registry never wired up by cmd_start). Same temp-file + rename pattern as
// internal/imgprep/cache.go's cacheStore — a concurrent reader can never
// observe a partially-written file — written 0600, the same permission
// class as the audit log this file's counters are derived from.
func (r *Registry) Flush() error {
	if r.path == "" {
		return nil
	}
	r.mu.Lock()
	if !r.dirty {
		r.mu.Unlock()
		return nil
	}
	ff := fileFormat{Version: fileVersion, Accounts: r.accounts}
	data, err := json.MarshalIndent(&ff, "", "  ")
	// Cleared under the lock regardless of what happens below: a transient
	// write failure (full disk, permission change) shouldn't wedge every
	// later Flush into silently no-op'ing forever just because dirty never
	// got reset — the next Charge sets it dirty again on its own anyway, so
	// there is no data loss from clearing it here, only a retry on the next
	// tick instead of an immediate one.
	r.dirty = false
	r.mu.Unlock()
	if err != nil {
		return err
	}

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".vmr-quota-*.tmp")
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
	if err := os.Rename(tmpName, r.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Bucket is bucket exported read-only for an offline consumer (vmr report's
// §2.5 quota-vs-consumption table via LoadFile) — the JSON tags are shared
// verbatim with the unexported bucket this package uses in memory and on
// disk, per store.go's own "there is exactly one shape" rule (see
// fileFormat's doc comment): a second, parallel type here would be exactly
// the duplication that rule exists to avoid.
type Bucket struct {
	PeriodStart   int64    `json:"period_start"`
	C             Counters `json:"counters"`
	Estimated     float64  `json:"estimated"`
	EstimatedCost float64  `json:"estimated_cost,omitempty"`
}

// PeriodStartTime is PeriodStart converted back to a time.Time (in
// time.Local, per time.Unix's own contract — which zone this prints in is
// irrelevant here: the original Charge/Used call sites always stored a
// Unix-seconds value already computed in fmtutil.DisplayZone via
// quota.PeriodStart, and comparing two time.Time values for equality
// doesn't care which zone either one prints in, only that they name the
// same instant).
func (b Bucket) PeriodStartTime() time.Time {
	return time.Unix(b.PeriodStart, 0)
}

// LoadFile reads path (a vmr-quota.json) and returns its accounts read-only,
// without constructing a Registry, taking any lock, or ever writing back —
// the shape an offline consumer like `vmr report` needs, as opposed to
// Registry.Load's in-place mutation of a live Registry. A missing file
// returns (nil, nil): the normal case for an instance that hasn't
// accumulated any quota state yet, or whose log_dir was never wired up with
// quota tracking — not an error a caller needs to branch on beyond checking
// for a nil map.
func LoadFile(path string) (map[string]map[string]Bucket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ff struct {
		Version  int                          `json:"version"`
		Accounts map[string]map[string]Bucket `json:"accounts"`
	}
	if err := json.Unmarshal(data, &ff); err != nil {
		return nil, err
	}
	return ff.Accounts, nil
}

// StartFlusher launches a background goroutine that calls Flush every
// interval, and returns a stop function. stop signals the goroutine to
// exit and BLOCKS until it has actually done so — cmd_start.go's shutdown
// sequence calls stop() and then one final Flush(); if stop() returned
// before the goroutine's own possibly-in-flight Flush had finished, the two
// could race on the same file (both doing CreateTemp+Write+Rename
// concurrently). Safe to call stop() more than once. A no-op stop when
// Registry has no path — nothing was ever started.
func (r *Registry) StartFlusher(interval time.Duration) (stop func()) {
	if r.path == "" {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.Flush()
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-stopped
	}
}
