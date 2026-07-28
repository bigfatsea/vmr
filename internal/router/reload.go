// Ver 2026-07-28 14:25, by Opus 5

// Hot-reload outcome tracking, exposed through /admin/status.
//
// The failure mode this closes: a rejected reload is, by design, silent to
// everyone except the log. `vmr start`'s reload closure logs "rejected,
// keeping current config" and keeps serving the previous snapshot — which
// is the right behavior (a bad config must never replace a good one) but
// leaves a process happily serving a config that no longer matches the file
// on disk, with no way to notice from the outside. `vmr status` showed
// endpoint health that looked perfectly fine.
//
// So the state is recorded here, next to the snapshot it explains, and
// reported alongside it. Comparing config_mtime against last_reload_at is
// the actual diagnostic: a config file newer than the last *successful*
// reload means what you edited is not what is running.
package router

import (
	"os"
	"sync"
	"time"
)

// ReloadState is the last-attempt record. Zero value = no reload has been
// attempted since start, which is the normal steady state, not an error.
type ReloadState struct {
	// At is when the last reload attempt finished; Trigger is what caused
	// it ("fsnotify" / "SIGHUP"), matching the log line's wording.
	At      time.Time
	Trigger string
	// OK reports whether that attempt replaced the snapshot. Err is the
	// rejection reason, non-empty only when OK is false.
	OK  bool
	Err string
	// Count is successful reloads since process start; OKAt is when the
	// last *successful* one happened. Tracked separately from At because
	// the two diverge exactly in the case worth reporting: a rejected
	// reload updates At but leaves OKAt (and therefore "when was the
	// running config actually read") where it was.
	Count int
	OKAt  time.Time
}

type reloadTracker struct {
	mu    sync.RWMutex
	state ReloadState
}

// RecordReload is called by the reload closure in cmd_start for every
// attempt, accepted or rejected. Guarded by a mutex rather than an
// atomic.Pointer swap because it is a genuinely rare write (a human editing
// a config file) — the lock-free copy-on-write idiom this project uses for
// its init-time registries buys nothing here and would only obscure that
// Count is a read-modify-write.
func (rt *Router) RecordReload(trigger string, err error) {
	rt.reloads.mu.Lock()
	defer rt.reloads.mu.Unlock()
	rt.reloads.state.At = time.Now()
	rt.reloads.state.Trigger = trigger
	rt.reloads.state.OK = err == nil
	if err != nil {
		rt.reloads.state.Err = err.Error()
	} else {
		rt.reloads.state.Err = ""
		rt.reloads.state.Count++
		rt.reloads.state.OKAt = rt.reloads.state.At
	}
}

// ReloadState returns a copy of the last reload attempt's outcome.
func (rt *Router) ReloadState() ReloadState {
	rt.reloads.mu.RLock()
	defer rt.reloads.mu.RUnlock()
	return rt.reloads.state
}

// ConfigStale reports whether the config file has been modified more
// recently than the last successful config load, along with the file's
// mtime. This is the signal that actually matters — "the file on disk is
// not what I am serving" — and it catches both failure shapes: a reload
// that was attempted and rejected, and an edit that never triggered a
// reload at all (fsnotify unavailable, an editor that replaced the inode,
// a file edited before the watch was established).
//
// loadedAt is when the running snapshot's config was read: process start,
// or the last accepted reload. A stat error (file deleted or unreadable)
// returns false — a missing file is not evidence of a stale one, and this
// must never turn into a scary status line for a transient editor rename.
func ConfigStale(path string, loadedAt time.Time) (stale bool, mtime time.Time) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, time.Time{}
	}
	return fi.ModTime().After(loadedAt), fi.ModTime()
}
