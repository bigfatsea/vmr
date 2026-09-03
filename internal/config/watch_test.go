// Ver 2026-07-16, by Fable 5
package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// waitChange asserts the watcher fires within a generous timeout (fsnotify +
// the 300ms debounce are asynchronous; CI machines can be slow).
func waitChange(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("watcher never fired after %s", what)
	}
}

// TestWatchFiresOnWrite covers the hot-reload entry point's common case: an
// in-place write to the watched config file triggers the (debounced)
// callback.
func TestWatchFiresOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ch := make(chan struct{}, 8)
	stop, err := Watch(path, func() { ch <- struct{}{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if err := os.WriteFile(path, []byte("a: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitChange(t, ch, "in-place write")
}

// TestWatchFiresOnAtomicReplace covers why Watch monitors the parent
// directory rather than the file itself: editors and tools typically write a
// temp file and rename it over the config, which never generates a Write
// event on the original inode.
func TestWatchFiresOnAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ch := make(chan struct{}, 8)
	stop, err := Watch(path, func() { ch <- struct{}{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	tmp := filepath.Join(dir, ".config.yaml.tmp")
	if err := os.WriteFile(tmp, []byte("a: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	waitChange(t, ch, "atomic replace (rename over the config)")
}

// TestWatchIgnoresSiblingFiles: the watcher listens on the whole directory
// (see above), so it must filter events down to the config file itself —
// churn on unrelated siblings (audit logs, editor droppings) must not
// trigger reloads.
func TestWatchIgnoresSiblingFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ch := make(chan struct{}, 8)
	stop, err := Watch(path, func() { ch <- struct{}{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Longer than the 300ms debounce: a spurious callback would have fired.
	select {
	case <-ch:
		t.Fatal("watcher fired for an unrelated sibling file")
	case <-time.After(700 * time.Millisecond):
	}
}

// TestWatchOnErrorNotCalledDuringNormalOperation locks in that onError is
// wired without disturbing the normal onChange path: a real fsnotify error
// (exhausted inotify handles, the watched directory disappearing) can't be
// triggered portably in a unit test, but this at least confirms passing a
// non-nil onError doesn't change behavior for the common case, and that it
// stays silent when nothing has gone wrong.
func TestWatchOnErrorNotCalledDuringNormalOperation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ch := make(chan struct{}, 8)
	stop, err := Watch(path, func() { ch <- struct{}{} }, func(err error) {
		t.Errorf("onError fired unexpectedly: %v", err)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	if err := os.WriteFile(path, []byte("a: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitChange(t, ch, "in-place write")
}

// TestWatchStopCancelsArmedTimer pins the lifecycle fix: a write event that
// arms the debounce timer followed IMMEDIATELY by stop() must leave onChange
// unfired — the timer runs on its own goroutine, so without the stop function
// cancelling it, a reload callback would still run once after the caller had
// already shut down (a stray reload after the routing table was torn down).
// The race-free shape: write arms the timer asynchronously; whether the
// goroutine has armed it before or after stop() runs, the ~300ms debounce
// means the stop-side timer.Stop() always lands first, so onChange cannot
// fire. A longer sleep (400ms, past the debounce window) then proves nothing
// fired.
func TestWatchStopCancelsArmedTimer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var fired int32
	stop, err := Watch(path, func() { atomic.AddInt32(&fired, 1) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("a: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Stop immediately — while the debounce timer (if armed) is still pending.
	if err := stop(); err != nil {
		t.Fatalf("stop(): %v", err)
	}
	// Outlive the 300ms debounce: a stray onChange would have fired by now.
	time.Sleep(400 * time.Millisecond)
	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Errorf("onChange fired %d time(s) after stop(), want 0 — the debounce timer must be cancelled by stop", n)
	}
}
