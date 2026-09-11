// Ver 2026-09-11, by Claude Sonnet 5

//go:build !windows

package quota

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockFileName is quota's advisory-lock file inside log_dir. Separate from
// audit's .vmr-audit.lock and livestats' .vmr-stats.lock on the same
// rationale livestats already documents: with -audit=false the audit lock is
// never taken, and "keep no request bodies but still meter quota" is a
// first-class scenario — quota must not parasitize a lock that scenario
// deliberately skips.
const lockFileName = ".vmr-quota.lock"

// acquireDirLock takes an exclusive non-blocking advisory lock on dir so a
// second vmr process (another `vmr start`, or `vmr replay`) pointing at the
// same log_dir cannot concurrently read-modify-write the same vmr-quota.json
// — without it, two processes each doing load-then-CreateTemp+Rename race
// and the later writer silently discards the other's counters. The returned
// file must stay open for the Registry's lifetime — the lock dies with the
// fd; there is no separate Close, since the process holding it exits (and
// the kernel drops the flock) at the same time the lock would otherwise be
// released. Same mechanism as internal/livestats' dir lock, kept a private
// copy for the same reason: no cross-package dependency for six lines of
// syscall.
func acquireDirLock(dir string) (*os.File, error) {
	path := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("quota: open lock file %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("quota: log_dir %s is held by another vmr process — quota state will stay in-memory only for this instance", dir)
	}
	return f, nil
}
