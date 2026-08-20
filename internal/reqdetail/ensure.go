// Ver 2026-08-20 00:00, by Sonnet 5

package reqdetail

import (
	"os"
	"path/filepath"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// EnsureRendered writes rec's detail page under dir if it doesn't already
// exist there, returning the (always-computable) filename either way. A
// pre-existing file with this exact name is guaranteed byte-identical to
// what Render would produce now — the name is a pure function of (rec.TS,
// rec.Model, RealModel(rec), rec.Outcome, req) and Render is a pure
// function of (rec, m, prev, prof, lang, linkEvidence) — so an existence
// check is a correct and sufficient skip condition, not an approximation:
// this is what lets both `vmr report` (every record, in one pass) and a
// future per-Journey caller (only the records it touches) call this same
// function and never race or duplicate work.
//
// evidenceDir, when non-empty, is where this record's system prompt and
// declared tool set (if any) get written as shared evidence blobs — see
// evidence.go — before Render links to them instead of inlining them;
// pass "" to render fully inline (e.g. from a caller with no evidence/
// directory of its own, or a test that doesn't care). Skipped entirely on
// a cache hit (target already exists): the existing page's evidence links
// were already satisfied when it was first written, and the evidence
// directory is a fully re-derivable cache with no reference counting of
// its own (see the architecture doc's §7.6c) — this is not the place to
// reconcile the two if one was wiped independently of the other.
//
// The detail page's write itself goes through a temp-file-then-rename
// (same pattern as internal/quota's Registry.Flush) so a killed process
// never leaves a half-written file that a later run's existence check
// would wrongly treat as done.
func EnsureRendered(dir string, rec *audit.Record, path string, line int, m, prev *ctxgraph.Manifest, prof taskseg.Profile, lang i18n.Lang, evidenceDir string) (filename string, err error) {
	filename = FileNameForRecord(rec, path, line)
	target := filepath.Join(dir, filename)
	if _, err := os.Stat(target); err == nil {
		return filename, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	linkEvidence := evidenceDir != ""
	if linkEvidence {
		if _, err := EnsureSysPromptEvidence(evidenceDir, rec); err != nil {
			return "", err
		}
		if _, err := EnsureToolsEvidence(evidenceDir, rec); err != nil {
			return "", err
		}
	}

	md := Render(rec, path, line, m, prev, prof, lang, linkEvidence)
	if err := writeFileAtomic(dir, target, []byte(md)); err != nil {
		return "", err
	}
	return filename, nil
}

// writeFileAtomic writes data to target via a temp file in dir followed by
// a rename, so a concurrent reader (or a killed process's next run) never
// observes a partially-written file — see EnsureRendered's doc comment.
// 0600: detail pages carry the same full conversation bodies as the 0600
// audit log they're derived from.
func writeFileAtomic(dir, target string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".reqdetail-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
