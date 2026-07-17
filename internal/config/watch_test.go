// Ver 2026-07-16, by Fable 5
package config

import (
	"os"
	"path/filepath"
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
	stop, err := Watch(path, func() { ch <- struct{}{} })
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
	stop, err := Watch(path, func() { ch <- struct{}{} })
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
	stop, err := Watch(path, func() { ch <- struct{}{} })
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
