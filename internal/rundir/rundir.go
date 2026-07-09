// Ver 2026-07-10 00:00, by Sonnet 5

// Package rundir resolves the on-disk directories vmr picks by default when
// the caller hasn't named one explicitly (audit logs, image-downscale
// cache). One formula, shared by both, is the point: dev mode (vmr.sh
// start) and service mode (launchd/systemd) must land on the exact same
// directory given the exact same environment, or `vmr.sh logs` tails the
// wrong file and a downscale cache primed under one mode is invisible to
// the other. vmr.sh queries the resolved path via `vmr dirs log`/`vmr dirs
// cache` instead of keeping its own copy of this logic in bash.
package rundir

import (
	"os"
	"path/filepath"
)

// Resolve applies the three-tier default, most to least specific:
//
//  1. $<envVar>, if set — used exactly as given, no subdir appended. An
//     explicit path is the caller's own choice; adding a subdir under it
//     would surprise anyone who set the variable expecting it to be the
//     directory, not a parent of it.
//  2. os.TempDir()/<tmpSubdir> — the common case when the variable is
//     unset. Namespaced under a vmr_-prefixed subdir because the system
//     temp dir is shared with every other process on the machine.
//  3. <cwd>/<pwdSubdir> — only reached if os.TempDir() itself returns "",
//     which none of Go's supported platforms actually do (unix falls back
//     to /tmp, Windows/Plan9 have their own built-in defaults). Kept as a
//     defensive last resort, not a realistic path in practice.
func Resolve(envVar, tmpSubdir, pwdSubdir string) string {
	return resolve(os.Getenv(envVar), os.TempDir(), tmpSubdir, cwd(), pwdSubdir)
}

func resolve(envVal, tmpDir, tmpSubdir, wd, pwdSubdir string) string {
	if envVal != "" {
		return envVal
	}
	if tmpDir != "" {
		return filepath.Join(tmpDir, tmpSubdir)
	}
	return filepath.Join(wd, pwdSubdir)
}

func cwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
