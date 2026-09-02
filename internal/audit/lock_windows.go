//go:build windows

package audit

import "os"

// Windows has no flock, and the portable substitutes (LockFileEx on a lock
// file, or a pidfile) were deliberately not used: LockFileEx locks do not
// follow the same open-file-description semantics, and a pidfile outlives a
// crashed process, permanently wedging startup on a stale pid. The Windows
// side therefore forgoes cross-instance exclusion and relies on
// compressOne's unique temp names alone — two processes sharing a log_dir
// there still corrupt interleaved JSONL appends, just not the archives.
// acquireDirLock on Windows is a deliberate no-op.
func acquireDirLock(dir string) (*os.File, error) {
	return nil, nil
}

// DirLockOccupier on Windows is a deliberate no-op.
func DirLockOccupier(dir string) (bool, string) {
	return false, ""
}
