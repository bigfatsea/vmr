// Ver 2026-09-11, by Claude Sonnet 5

//go:build windows

package quota

import "os"

// Windows has no flock, and the portable substitutes were deliberately not
// used (see internal/audit's lock_windows.go for the reasoning, mirrored by
// internal/livestats). quota therefore forgoes cross-instance exclusion on
// Windows, which is not a target deployment platform. acquireDirLock is a
// deliberate no-op — Load proceeds unlocked, exactly as it did before this
// lock existed.
func acquireDirLock(dir string) (*os.File, error) {
	return nil, nil
}
