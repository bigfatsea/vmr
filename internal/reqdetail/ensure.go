// Ver 2026-08-20 00:00, by Sonnet 5

package reqdetail

import (
	"bufio"
	"io"
	"os"
	"path/filepath"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// EnsureRendered writes rec's detail page under dir unless a pre-existing
// file there already carries the fingerprint (renderFingerprint) that the
// requested (lang, evidenceDir != "") combination would produce, returning
// the (always-computable) filename either way. The filename alone is NOT
// enough to know whether a pre-existing file is still current: it's a pure
// function of (rec.TS, rec.Model, RealModel(rec), rec.Outcome, req), but
// Render is a pure function of (rec, m, prev, prof, lang, linkEvidence) —
// two more axes the name doesn't carry. An existence-only check therefore
// used to skip re-rendering a file that was actually stale — proven on
// real corpus data on both axes: a language switch left old-language
// detail pages linked from a new-language journey report, and deleting
// evidence/ then re-running left 45 links permanently dead because the
// "file exists" branch never got to the evidence-write call below.
// Reading and comparing the fingerprint
// (readRenderFingerprint) replaces that assumption with an actual check,
// while keeping the property the callers rely on — the filename itself needs no
// I/O to compute — so this is what lets both `vmr report` (every record,
// in one pass) and a future per-Journey caller (only the records it
// touches) call this same function and never race or duplicate work.
//
// evidenceDir, when non-empty, is where this record's system prompt and
// declared tool set (if any) get written as shared evidence blobs — see
// evidence.go — before Render links to them instead of inlining them;
// pass "" to render fully inline (e.g. from a caller with no evidence/
// directory of its own, or a test that doesn't care). The two Ensure*
// evidence calls run BEFORE the fingerprint check, unconditionally
// whenever linkEvidence is true — not just on the branch that goes on to
// re-render the detail page. This is deliberate, not an optimization left
// for later: evidence files are addressed by their own content hash, not
// by this record's coordinate, so a detail page whose fingerprint already
// matches (nothing to re-render) can still be pointing at an evidence file
// that was separately deleted — the exact (delete evidence/, rerun)
// scenario this function exists to fix. Running these
// calls only on the "must re-render" branch reproduces that same bug one
// level down; running them unconditionally is what actually closes it,
// and their own existence checks (EnsureSysPromptEvidence/
// EnsureToolsEvidence) keep the common case (both already present) to one
// cheap stat apiece.
//
// The detail page's write itself goes through a temp-file-then-rename
// (same pattern as internal/quota's Registry.Flush) so a killed process
// never leaves a half-written file that a later run's fingerprint check
// would wrongly treat as done.
func EnsureRendered(dir string, rec *audit.Record, path string, line int, m, prev *ctxgraph.Manifest, prof taskseg.Profile, lang i18n.Lang, evidenceDir string) (filename string, err error) {
	filename = FileNameForRecord(rec, path, line)
	target := filepath.Join(dir, filename)
	linkEvidence := evidenceDir != ""

	if linkEvidence {
		if _, err := EnsureSysPromptEvidence(evidenceDir, rec); err != nil {
			return "", err
		}
		if _, err := EnsureToolsEvidence(evidenceDir, rec); err != nil {
			return "", err
		}
	}

	got, err := readRenderFingerprint(target)
	if err != nil {
		return "", err
	}
	if got == renderFingerprint(lang, linkEvidence) {
		return filename, nil
	}

	md := Render(rec, path, line, m, prev, prof, lang, linkEvidence)
	if err := writeFileAtomic(dir, target, []byte(md)); err != nil {
		return "", err
	}
	return filename, nil
}

// readRenderFingerprint reads just target's first line — a bounded read,
// not the whole file, since EnsureRendered calls this on every invocation
// including ones that end up skipping, and a detail page can run to
// several MB (see the architecture doc's §7.6c). Returns ("", nil), not an
// error, when target doesn't exist or is empty: the caller's fingerprint
// comparison naturally fails either way and falls through to rendering,
// which is exactly the desired behavior for "no file yet" — no separate
// existence check needed.
func readRenderFingerprint(target string) (string, error) {
	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return line, nil
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
