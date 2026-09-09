//go:build windows

package livestats

import "os"

// Windows has no flock, and the portable substitutes were deliberately not
// used (see internal/audit's lock_windows.go for the reasoning). livestats
// therefore forgoes cross-instance exclusion on Windows, which is not a
// target deployment platform. acquireDirLock is a deliberate no-op.
func acquireDirLock(dir string) (*os.File, error) {
	return nil, nil
}
