// Ver 2026-07-12 12:00, by Fable 5

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

// Resolve applies the four-tier default, most to least specific:
//
//  1. $<envVar>, if set — used exactly as given, no subdir appended. An
//     explicit path is the caller's own choice; adding a subdir under it
//     would surprise anyone who set the variable expecting it to be the
//     directory, not a parent of it.
//  2. ~/.vmr/<homeSubdir> — the common case when the variable is unset.
//     A persistent per-user dotdir, NOT the system temp dir: macOS purges
//     $TMPDIR entries not accessed for ~3 days (and on reboot), which
//     silently deleted audit history in practice — fatal for data whose
//     whole point is long-term cost accounting (§9.5: audit files are the
//     only data source for vmr report).
//  3. os.TempDir()/<tmpSubdir> — only when the home directory cannot be
//     resolved (no $HOME in a stripped-down service environment).
//     Namespaced under a vmr_-prefixed subdir because the system temp dir
//     is shared with every other process on the machine.
//  4. <cwd>/<pwdSubdir> — only reached if os.TempDir() itself returns "",
//     which none of Go's supported platforms actually do. Kept as a
//     defensive last resort, not a realistic path in practice.
func Resolve(envVar, homeSubdir, tmpSubdir, pwdSubdir string) string {
	return resolve(os.Getenv(envVar), home(), homeSubdir, os.TempDir(), tmpSubdir, cwd(), pwdSubdir)
}

func resolve(envVal, homeDir, homeSubdir, tmpDir, tmpSubdir, wd, pwdSubdir string) string {
	if envVal != "" {
		return envVal
	}
	if homeDir != "" {
		return filepath.Join(homeDir, ".vmr", homeSubdir)
	}
	if tmpDir != "" {
		return filepath.Join(tmpDir, tmpSubdir)
	}
	return filepath.Join(wd, pwdSubdir)
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

func cwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
