// Ver 2026-07-29 14:00, by Sonnet 5

// Input-path resolution shared by cmd_report.go and cmd_story.go: both take
// the same "<audit.jsonl|glob>..." positional argument convention and the
// same default when it's omitted.
package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"sort"

	"vmr/internal/config"
)

// resolveInputPaths returns the sorted, de-duplicated set of files fs's
// positional arguments (each glob-expanded) resolve to. If no positional
// arguments were given, it falls back to "<log_dir>/vmr-audit-*" — vmr's
// own audit-file naming convention, matching both live .jsonl files and the
// housekeeping sweep's compressed .jsonl.zst ones with a single glob —
// loading configPath only in that fallback case (config.Load already
// applies log_dir's usual rundir.Resolve default when config.yaml doesn't
// set one explicitly, so this stays correct even for an instance that never
// set log_dir).
func resolveInputPaths(fs *flag.FlagSet, configPath string) ([]string, error) {
	args := fs.Args()
	if len(args) == 0 {
		cfg, err := config.Load(configPath)
		if err != nil {
			return nil, fmt.Errorf("no input files given and failed to load %s for its log_dir default: %w", configPath, err)
		}
		args = []string{filepath.Join(cfg.LogDir, "vmr-audit-*")}
	}
	seen := map[string]bool{}
	var paths []string
	for _, arg := range args {
		matches, err := filepath.Glob(arg)
		if err != nil {
			return nil, fmt.Errorf("bad pattern %q: %w", arg, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no files match %q", arg)
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				paths = append(paths, m)
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}
