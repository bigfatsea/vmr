// Ver 2026-09-13, by Sonnet 5

package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/ctxgraph"
)

// statAuditPathArg classifies the raw AuditPath argument -req needs to
// treat differently from -ts/-line: "" (omitted entirely), a directory (a
// hint to search, not the file itself), or an exact file path (the
// existing strict-consistency-check behavior, unchanged). A path that
// doesn't exist yet at all is left to the eventual open call to report —
// this only distinguishes "is it a directory", so a typo'd file path
// still gets loadRecordByLine's own, more specific error.
func statAuditPathArg(raw string) (path string, isDir bool, err error) {
	if raw == "" {
		return "", false, nil
	}
	if fi, statErr := os.Stat(raw); statErr == nil && fi.IsDir() {
		return raw, true, nil
	}
	return raw, false, nil
}

// ResolveAuditPath finds the file a "basename:line" coordinate's basename
// refers to, searching — in order — a direct stat of basename itself
// (handles an absolute or already-resolvable relative path, including one
// a hand-typed coordinate carries with a leading directory component
// rather than the bare form requests/index.json publishes), dirHint (when
// given — every caller only ever passes "" or an already-confirmed
// directory; see statAuditPathArg and cmd_diff.go's LoadRecord callers),
// the current directory, "./logs" when it exists, and config.yaml's
// log_dir. Each directory is tried against the literal basename first and
// (only when it differs) filepath.Base(basename) too, each plain and with
// a ".zst" suffix (plain-first: a live/current-day file is far more
// commonly what's being inspected than an already-rotated one).
//
// This is the single resolver both `vmr replay -req` and `vmr diff` use —
// see KNOWN_ISSUES §2.138 for why a second, independently-drifted copy of
// this search used to live in cmd/vmr/cmd_replay.go.
func ResolveAuditPath(basename, dirHint, configPath string) (string, error) {
	if fi, err := os.Stat(basename); err == nil && !fi.IsDir() {
		return basename, nil
	}
	if fi, err := os.Stat(basename + ".zst"); err == nil && !fi.IsDir() {
		return basename + ".zst", nil
	}

	var dirs []string
	if dirHint != "" {
		dirs = append(dirs, dirHint)
	}
	dirs = append(dirs, ".")
	if fi, err := os.Stat("logs"); err == nil && fi.IsDir() {
		dirs = append(dirs, "logs")
	}
	if cfg, err := config.Load(configPath); err == nil && cfg.LogDir != "" {
		dirs = append(dirs, cfg.LogDir)
	}

	variants := []string{basename, basename + ".zst"}
	if base := filepath.Base(basename); base != basename {
		variants = append(variants, base, base+".zst")
	}
	for _, dir := range dirs {
		for _, name := range variants {
			p := filepath.Join(dir, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("couldn't find %q (or its .zst variant) under %v", basename, dirs)
}

// LoadRecord resolves a "basename:line" coordinate (as ParseReqCoord parses
// it) via ResolveAuditPath and loads the full audit.Record at that line —
// the one-shot "give me the whole record this coordinate names" a caller
// that needs more than replay's own lean recordView reaches for. vmr diff's
// two-sided comparison (cmd/vmr/cmd_diff.go) is the first such caller.
func LoadRecord(req, dirHint, configPath string) (*audit.Record, string, int, error) {
	basename, line, err := ctxgraph.ParseReqCoord(req)
	if err != nil {
		return nil, "", 0, err
	}
	path, err := ResolveAuditPath(basename, dirHint, configPath)
	if err != nil {
		return nil, "", 0, err
	}
	raw, err := audit.LineAt(path, line)
	if err != nil {
		return nil, path, line, err
	}
	var rec audit.Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return nil, path, line, fmt.Errorf("%s:%d: unmarshal audit record: %w", path, line, err)
	}
	return &rec, path, line, nil
}
