//go:build windows

package stats

import "os"

// Windows has no flock and is not a target deployment platform (see
// internal/audit's lock_windows.go); cross-instance exclusion is skipped.
func acquireDirLock(dir string) (*os.File, error) {
	return nil, nil
}
