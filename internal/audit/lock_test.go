// Ver 2026-09-02 12:10, by pi-agent

package audit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNew_DirLockRejectsSecondInstance: two vmr processes sharing one log_dir
// concurrently append to the same JSONL and interleave two housekeeping
// passes — unrecoverable archive corruption. The second New must refuse.
func TestNew_DirLockRejectsSecondInstance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no flock on windows; acquireDirLock is a deliberate no-op there")
	}
	dir := t.TempDir()
	l1, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	l2, err := New(dir)
	if err == nil {
		l2.Close()
		t.Fatal("second audit.New on the same log_dir must fail")
	}
	if !strings.Contains(err.Error(), "pid") {
		t.Errorf("error should name the occupier's pid, got: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, lockFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("lock file mode = %v, want 0600", info.Mode().Perm())
	}
	if err := l1.Close(); err != nil {
		t.Fatal(err)
	}
	// The lock must be re-acquirable once the holder is gone.
	l3, err := New(dir)
	if err != nil {
		t.Fatalf("lock not re-acquirable after holder closed: %v", err)
	}
	l3.Close()
}

// TestDirLockOccupier reports whether the directory is actively held.
func TestDirLockOccupier(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no flock on windows")
	}
	dir := t.TempDir()
	held, _ := DirLockOccupier(dir)
	if held {
		t.Fatal("empty dir should not be locked")
	}
	l1, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()

	held, pid := DirLockOccupier(dir)
	if !held {
		t.Fatal("dir with active audit.Logger should be reported as held")
	}
	if pid == "" {
		t.Fatal("held dir should report non-empty pid")
	}
}

// TestNew_DirLockIndependentDirs: separate log_dirs never interfere.
func TestNew_DirLockIndependentDirs(t *testing.T) {
	l1, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()
	l2, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
}
