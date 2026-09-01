// Ver 2026-08-13 16:39, by Gemini 3.6 Flash

package quota

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vmr/internal/core"
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
// error path. Corruption here means syntactic OR structural: a version that
// isn't the current one, a nil account map, or a null bucket is rejected
// wholesale (R43/R66), so a structurally damaged file can't smuggle a nil
// bucket past the loader into a later resetIfStaleLocked panic.
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
	if err := validateLoadedShape(ff.Version, ff.Accounts); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ff.Accounts != nil {
		r.accounts = ff.Accounts
	}
	return nil
}

// Prune removes in-memory and persisted bucket entries that no longer match
// any configured Limit in valid, keeping on-disk state from accumulating
// orphan keys across config edits. Returns the number of pruned keys.
func (r *Registry) Prune(valid map[string][]core.Limit) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	pruned := 0
	for provider, limits := range r.accounts {
		configured, ok := valid[provider]
		if !ok || len(configured) == 0 {
			pruned += len(limits)
			delete(r.accounts, provider)
			r.dirty = true
			continue
		}
		for key := range limits {
			keep := false
			for _, l := range configured {
				if !PerModel(l) {
					if key == LimitKey(l, "") {
						keep = true
						break
					}
				} else if model, ok := ExtractModel(l, key); ok {
					if IsWildcardModels(l.Models) || AppliesToModel(l, model) {
						keep = true
						break
					}
				}
			}
			if !keep {
				delete(limits, key)
				pruned++
				r.dirty = true
			}
		}
		if len(limits) == 0 {
			delete(r.accounts, provider)
		}
	}
	return pruned
}

// Flush atomically writes the current state to Registry's path. A no-op
// when nothing has changed since the last successful Flush (Charge is the
// only thing that sets dirty) or when Registry has no path (tests, or a
// Registry never wired up by cmd_start). Same temp-file + rename pattern as
// internal/imgprep/cache.go's cacheStore — a concurrent reader can never
// observe a partially-written file — written 0600, the same permission
// class as the audit log this file's counters are derived from.
func (r *Registry) Flush() (err error) {
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
	// Snapshot and dirty-clearing are atomic under the lock, so a Charge
	// racing the write re-sets dirty on its own and the next tick persists
	// it. On any failure below the defer restores dirty, so a transient
	// write error (full disk, permission change, NaN value) is retried on
	// the next tick instead of silently dropping the unsaved state (R47).
	r.dirty = false
	r.mu.Unlock()
	defer func() {
		if err != nil {
			r.mu.Lock()
			r.dirty = true
			r.mu.Unlock()
		}
	}()
	if err != nil {
		return err
	}

	dir := filepath.Dir(r.path)
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".vmr-quota-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err = tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	// fsync before close: temp+rename keeps a concurrent reader from ever
	// seeing a half-written file, but only a sync orders the data ahead of
	// the rename's metadata on the disk itself — without it a crash/power
	// loss can surface the rename with a zero-length or stale-tail file and
	// the whole period's counters read as lost (R52).
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err = os.Rename(tmpName, r.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Bucket represents a snapshot of quota state exported for offline consumers (vmr report's
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
// for a nil map. A structurally damaged file (bad version, nil account map,
// null bucket) returns an error, wholesale rather than partially adopted —
// same contract as Registry.Load (R43/R66): silently dropping one provider's
// ledger is more dangerous than failing the read.
func LoadFile(path string) (map[string]map[string]Bucket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// Decode into pointer buckets first so a JSON null is observable — the
	// returned map is then materialized into value Buckets for the caller.
	var ff struct {
		Version  int                           `json:"version"`
		Accounts map[string]map[string]*Bucket `json:"accounts"`
	}
	if err := json.Unmarshal(data, &ff); err != nil {
		return nil, err
	}
	if err := validateLoadedShape(ff.Version, ff.Accounts); err != nil {
		return nil, err
	}
	accounts := make(map[string]map[string]Bucket, len(ff.Accounts))
	for provider, byLimit := range ff.Accounts {
		out := make(map[string]Bucket, len(byLimit))
		for key, b := range byLimit {
			out[key] = *b
		}
		accounts[provider] = out
	}
	return accounts, nil
}

// validateLoadedShape rejects a structurally damaged quota ledger: a version
// other than the current one, a nil account map, or a null bucket. Either
// full adoption or full rejection — never a partial take, so an anomaly in
// one provider's slice can't silently drop another provider's ledger
// (R43/R66). Shared by Registry.Load and LoadFile so the two readers agree
// on what "damaged" means.
func validateLoadedShape[T any](version int, accounts map[string]map[string]*T) error {
	if version != fileVersion {
		return fmt.Errorf("quota state version %d != expected %d (refusing to adopt an unknown on-disk shape)", version, fileVersion)
	}
	for provider, byLimit := range accounts {
		if byLimit == nil {
			return fmt.Errorf("quota state: account %q has a nil bucket map", provider)
		}
		for key, b := range byLimit {
			if b == nil {
				return fmt.Errorf("quota state: account %q limit %q is a null bucket", provider, key)
			}
		}
	}
	return nil
}

// StartFlusher launches a background goroutine that calls Flush every
// interval, and returns a stop function. Flush errors go to the logger
// SetLogger wired (none if unset), deduplicated so a persistent failure
// (full disk, permission change) logs once plus every 10th repeat instead
// of one line per tick (R47). stop signals the goroutine to exit and BLOCKS until it has
// actually done so — cmd_start.go's shutdown sequence calls stop() and then
// one final Flush(); if stop() returned before the goroutine's own
// possibly-in-flight Flush had finished, the two could race on the same
// file (both doing CreateTemp+Write+Rename concurrently). Safe to call
// stop() more than once. A no-op stop when Registry has no path — nothing
// was ever started.
func (r *Registry) StartFlusher(interval time.Duration) (stop func()) {
	if r.path == "" {
		return func() {}
	}
	r.mu.Lock()
	fl := &flushLog{logger: r.logger}
	r.mu.Unlock()
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := r.Flush(); err != nil {
					fl.Error(err)
				}
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

// flushLog throttles repeated identical Flush failures — a full disk or
// permission change is persistent, so one line per 10s tick would bury the
// log under the same message; first occurrence logs, then every 10th repeat
// with a running consecutive-failure count (R47).
type flushLog struct {
	logger   *log.Logger
	lastMsg  string
	failures int
}

func (f *flushLog) Error(err error) {
	if f.logger == nil {
		return
	}
	msg := err.Error()
	if msg == f.lastMsg {
		f.failures++
		if f.failures%10 != 0 {
			return
		}
		f.logger.Printf("WARN quota flush: %s (still failing; %d consecutive)", msg, f.failures)
		return
	}
	f.lastMsg = msg
	f.failures = 1
	f.logger.Printf("WARN quota flush: %s", msg)
}
