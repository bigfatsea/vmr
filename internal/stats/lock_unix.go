//go:build !windows

package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockFileName is the statistics directory's advisory lock. It is separate
// from audit's lock because statistics keep working with -audit=false, where
// that lock is never taken.
const lockFileName = ".lock"

// acquireDirLock takes an exclusive non-blocking flock on dir so a second
// vmr process pointing at the same log_dir cannot interleave appends into
// the same day file. The returned file must stay open for the store's
// lifetime; the kernel drops the lock with the fd, so a crash never wedges
// the next start the way a pidfile would. A private copy of audit's
// mechanism: this package is a zero-internal-dependency leaf.
func acquireDirLock(dir string) (*os.File, error) {
	path := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, fmt.Errorf("open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s is held by another vmr process — two processes must not share one log_dir", dir)
	}
	return f, nil
}
