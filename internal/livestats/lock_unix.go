//go:build !windows

package livestats

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockFileName is livestats' advisory-lock file inside log_dir. Separate from
// audit's .vmr-audit.lock on purpose: livestats does not parasitize audit's
// lock, because with -audit=false that lock is never taken and "keep no
// bodies but still monitor" is a first-class scenario (design §1.2/§3.2).
const lockFileName = ".vmr-stats.lock"

// acquireDirLock takes an exclusive non-blocking advisory lock on dir so a
// second vmr process pointing at the same log_dir cannot concurrently append
// to the slim/rollup files (interleaved O_APPEND lines, a rollup rolled twice
// from divergent slim reads). The returned file must stay open for the
// aggregator's lifetime — the lock dies with the fd; Close releases it. Same
// mechanism as internal/audit's dir lock, kept a private copy because
// livestats is a zero-internal-dependency leaf and cannot import it.
//
// flock(LOCK_EX|LOCK_NB) rather than a pidfile: a pidfile outlives a crashed
// process and permanently wedges startup. The kernel drops an flock
// automatically.
func acquireDirLock(dir string) (*os.File, error) {
	path := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, fmt.Errorf("livestats: open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("livestats: log_dir %s is held by another vmr process — two processes must not share one log_dir; give this instance its own log_dir", dir)
	}
	return f, nil
}
