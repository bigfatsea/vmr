// Ver 2026-07-13 04:00, by Sonnet 5

// Package rundir resolves the on-disk directories vmr uses by default when
// config.yaml doesn't name one explicitly (log_dir / image_cache_dir). One
// formula, shared by both, is the point: every mode of running vmr must
// land on the exact same directory, or `vmr.sh logs` tails the wrong file
// and a downscale cache primed under one mode is invisible to the other.
// Explicit overrides live in config.yaml — there is no environment-variable
// tier here; reference ${VAR} in the config to feed a value from the
// environment.
package rundir

import (
	"os"
	"path/filepath"
)

// Resolve applies the three-tier default, most to least specific:
//
//  1. ~/.vmr/<homeSubdir> — the common case. A persistent per-user dotdir,
//     NOT the system temp dir: macOS purges $TMPDIR entries not accessed
//     for ~3 days (and on reboot), which would silently delete audit data —
//     fatal for data whose whole point is long-term cost accounting (§9.5:
//     audit files are the only data source for vmr report).
//  2. os.TempDir()/<tmpSubdir> — only when the home directory cannot be
//     resolved (no $HOME in a stripped-down service environment).
//     Namespaced under a vmr_-prefixed subdir because the system temp dir
//     is shared with every other process on the machine.
//  3. <cwd>/<pwdSubdir> — only reached if os.TempDir() itself returns "",
//     which none of Go's supported platforms actually do. Kept as a
//     defensive last resort, not a realistic path in practice.
func Resolve(homeSubdir, tmpSubdir, pwdSubdir string) string {
	return resolve(home(), homeSubdir, os.TempDir(), tmpSubdir, cwd(), pwdSubdir)
}

func resolve(homeDir, homeSubdir, tmpDir, tmpSubdir, wd, pwdSubdir string) string {
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
