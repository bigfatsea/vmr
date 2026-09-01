//go:build !windows

package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// lockFileName is the advisory-lock file inside the audit directory. 0600,
// like everything else in log_dir.
const lockFileName = ".vmr-audit.lock"

// acquireDirLock takes an exclusive non-blocking advisory lock on dir, so a
// second vmr process pointing at the same log_dir refuses to start instead of
// concurrently appending to the same JSONL and interleaving two housekeeping
// passes (zstd streams into one archive = unrecoverable corruption). The
// returned file must stay open for the process lifetime — the lock dies with
// the fd; Close releases it explicitly. The error names the occupier's pid.
//
// flock(LOCK_EX|LOCK_NB) rather than a pidfile: a pidfile outlives a crashed
// process and permanently wedges startup, and checking whether the recorded
// pid is alive races pid reuse. The kernel drops an flock automatically.
func acquireDirLock(dir string) (*os.File, error) {
	path := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("audit: log_dir %s is held by another vmr process (pid %s) — two processes must not share one log_dir; give this instance its own log_dir", dir, occupierPid(path))
	}
	if err := f.Truncate(0); err == nil {
		fmt.Fprintf(f, "%d\n", os.Getpid())
	}
	return f, nil
}

// occupierPid reads the pid the lock holder wrote into path; "unknown" when
// it can't be read (the holder may predate the pid-writing lock, or the file
// may be unreadable — never worth failing the error message over).
func occupierPid(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	pid := strings.TrimSpace(string(data))
	if _, err := strconv.Atoi(pid); err != nil {
		return "unknown"
	}
	return pid
}
