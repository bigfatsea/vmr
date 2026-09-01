// Ver 2026-07-28 15:40, by Opus 5
package router

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/core"
)

// The field that carries the whole feature: a rejected reload must not
// advance OKAt, because OKAt is what "is the file on disk still what I'm
// serving" is measured against. If a rejection moved it, the rejection
// would erase its own evidence.
func TestRecordReloadRejectionDoesNotAdvanceOKAt(t *testing.T) {
	rt := New(nil)

	rt.RecordReload("SIGHUP", nil)
	first := rt.ReloadState()
	if !first.OK || first.Count != 1 || first.OKAt.IsZero() {
		t.Fatalf("after one accepted reload: %+v", first)
	}

	time.Sleep(2 * time.Millisecond)
	rt.RecordReload("fsnotify", errors.New("bad yaml"))
	second := rt.ReloadState()

	if second.OK || second.Err != "bad yaml" || second.Trigger != "fsnotify" {
		t.Errorf("rejection not recorded: %+v", second)
	}
	if second.Count != 1 {
		t.Errorf("count = %d, want 1 — a rejected reload was never applied", second.Count)
	}
	if !second.OKAt.Equal(first.OKAt) {
		t.Errorf("OKAt moved on rejection: %v -> %v", first.OKAt, second.OKAt)
	}
	if !second.At.After(first.At) {
		t.Errorf("At did not advance: %v -> %v", first.At, second.At)
	}

	// Recovery clears the error and moves both timestamps again.
	rt.RecordReload("fsnotify", nil)
	third := rt.ReloadState()
	if !third.OK || third.Err != "" || third.Count != 2 || !third.OKAt.After(first.OKAt) {
		t.Errorf("after recovery: %+v", third)
	}
}

func TestReloadStateZeroValueMeansNeverReloaded(t *testing.T) {
	if got := New(nil).ReloadState(); !got.At.IsZero() || got.Count != 0 {
		t.Errorf("fresh router: %+v, want the zero value", got)
	}
}

func TestConfigStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("listen: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	mtime := fi.ModTime()

	if stale, got := ConfigStale(path, mtime.Add(time.Second)); stale || !got.Equal(mtime) {
		t.Errorf("loaded after the file was written: stale=%v mtime=%v, want stale=false", stale, got)
	}
	if stale, _ := ConfigStale(path, mtime.Add(-time.Second)); !stale {
		t.Error("file modified after the config was loaded must report stale")
	}
	// A missing file is not evidence of a stale one — an editor that
	// replaces the inode makes it vanish for a moment, and that must not
	// flash a warning at whoever runs status right then.
	if stale, got := ConfigStale(filepath.Join(dir, "gone.yaml"), mtime); stale || !got.IsZero() {
		t.Errorf("missing file: stale=%v mtime=%v, want false/zero", stale, got)
	}
}

// A hot reload that drops an endpoint must drop its health state with it —
// otherwise a stale failure count and cooldown keep showing up in /status
// for an endpoint the config no longer knows about, and would apply again
// if the endpoint were ever re-added. Same contract Quota.Prune already had.
func TestInstallPrunesHealthOfRemovedEndpoints(t *testing.T) {
	const base = `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k1}
  - {name: p2, base_url: {openai-completions: https://example.com/v1}, api_key: k2}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1]}
      - {protocol: openai-completions, providers: [p2], models: [m2]}
`
	const shrunk = `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai-completions: https://example.com/v1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai-completions, providers: [p1], models: [m1]}
`
	rt := New(nil)
	snap := mustSnapshot(t, mustConfig(t, base))
	rt.Install(snap)

	keys := snap.HealthKeys()
	if len(keys) != 2 {
		t.Fatalf("HealthKeys = %v, want both endpoints", keys)
	}
	var kept, dropped string
	for k := range keys {
		if strings.Contains(k, "p1") {
			kept = k
		} else {
			dropped = k
		}
	}
	now := time.Now()
	rt.Health.ReportFailure(kept, core.ErrTransient, 0, now)
	rt.Health.ReportFailure(dropped, core.ErrTransient, 0, now)

	rt.Install(mustSnapshot(t, mustConfig(t, shrunk)))

	if st := rt.Health.Status(kept, now); st.Fails == 0 {
		t.Errorf("still-configured endpoint %q lost its health state: %+v", kept, st)
	}
	if st := rt.Health.Status(dropped, now); st.Fails != 0 {
		t.Errorf("removed endpoint %q kept health state after reload: %+v", dropped, st)
	}
}
