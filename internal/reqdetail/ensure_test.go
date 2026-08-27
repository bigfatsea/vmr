// Ver 2026-08-20 00:00, by Sonnet 5

package reqdetail

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func minimalRec(ts time.Time) *audit.Record {
	return &audit.Record{TS: ts, Model: "agent", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Body: map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai-completions:minimax:MiniMax-M3", Model: "MiniMax-M3"}}}
}

func TestEnsureRendered_WritesOnce(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := minimalRec(ts)

	name1, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, "")
	if err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(filepath.Join(dir, name1))
	if err != nil {
		t.Fatal(err)
	}

	name2, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, "")
	if err != nil {
		t.Fatal(err)
	}
	if name2 != name1 {
		t.Fatalf("filename changed across calls: %q vs %q", name1, name2)
	}
	fi2, err := os.Stat(filepath.Join(dir, name2))
	if err != nil {
		t.Fatal(err)
	}
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Errorf("mtime changed across calls (%v -> %v): second call re-rendered instead of skipping", fi1.ModTime(), fi2.ModTime())
	}
}

// TestEnsureRendered_RewritesAFileWithoutAMatchingFingerprint locks in the
// fix for KNOWN_ISSUES §1.41: a pre-existing file at the exact right name
// is NOT assumed correct anymore — its content is only trusted once its
// first-line fingerprint (renderFingerprint) matches what the current call
// would produce. A file with no fingerprint at all (pre-P12 detail pages,
// or any other foreign content sharing this coordinate) is the degenerate
// case: it must be treated as stale and rewritten, not preserved.
func TestEnsureRendered_RewritesAFileWithoutAMatchingFingerprint(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := minimalRec(ts)

	name := FileNameForRecord(rec, "audit.jsonl", 1)
	sentinel := []byte("not what Render would have produced")
	if err := os.WriteFile(filepath.Join(dir, name), sentinel, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != name {
		t.Fatalf("filename = %q, want %q", got, name)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == string(sentinel) {
		t.Errorf("EnsureRendered kept a fingerprint-less pre-existing file instead of rewriting it — a file with no matching fingerprint must never be trusted")
	}
	if !strings.Contains(string(data), renderFingerprint(i18n.EN, false)) {
		t.Errorf("rewritten file missing the expected fingerprint line, got:\n%s", data)
	}
}

// TestEnsureRendered_RewritesOnStaleTemplateVersion covers renderFingerprint's
// third axis — renderTemplateVersion — the mechanism P13's planned content
// reductions (KNOWN_ISSUES §1.36) rely on to invalidate every existing
// detail page after a change to Render's output shape that doesn't touch
// lang/evidence mode at all. Doesn't (and can't, without a settable
// version seam) bump the real constant; instead writes a file whose first
// line is a fingerprint claiming an older version than
// renderTemplateVersion actually is, and confirms that alone triggers a
// rewrite — the same mismatch path a real version bump would hit.
func TestEnsureRendered_RewritesOnStaleTemplateVersion(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := minimalRec(ts)

	name := FileNameForRecord(rec, "audit.jsonl", 1)
	stale := "<!-- reqdetail:v" + strconv.Itoa(renderTemplateVersion-1) + " lang=en evidence=false -->\nstale content from a lower template version\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, ""); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale content from a lower template version") {
		t.Errorf("a fingerprint naming an older template version was not treated as stale, got:\n%s", data)
	}
	if !strings.Contains(string(data), renderFingerprint(i18n.EN, false)) {
		t.Errorf("rewritten file missing the current fingerprint, got:\n%s", data)
	}
}

// TestEnsureRendered_RewritesOnLanguageChange is the direct regression test
// for the real-corpus evidence behind §1.41: rendering the same record
// first in English then in Chinese, into the same dir, must leave the
// Chinese content on disk — not silently keep serving the English page
// under a link a Chinese journey report now points at.
func TestEnsureRendered_RewritesOnLanguageChange(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := minimalRec(ts)

	name, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, "")
	if err != nil {
		t.Fatal(err)
	}
	enData, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(enData), "① Client → VMR Request") {
		t.Fatalf("EN render missing its section title, got:\n%s", enData)
	}

	name2, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.ZH, "")
	if err != nil {
		t.Fatal(err)
	}
	if name2 != name {
		t.Fatalf("filename changed across a language switch: %q vs %q — the coordinate, not lang, must drive the name", name, name2)
	}
	zhData, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(zhData), "① Client → VMR 请求") {
		t.Errorf("second call with i18n.ZH did not rewrite the file to Chinese — still got:\n%s", zhData)
	}
	if strings.Contains(string(zhData), "① Client → VMR Request") {
		t.Errorf("rewritten file still contains the old English section title:\n%s", zhData)
	}
}

// recWithSysPrompt builds a record whose request has a leading system
// message — the shape both TestEnsureRendered_RewritesOnEvidenceModeChange
// and TestEnsureRendered_RebuildsDeletedEvidence need to exercise the
// evidence-linking path at all (EnsureSysPromptEvidence is a no-op for a
// record with no system message).
func recWithSysPrompt(ts time.Time) *audit.Record {
	return &audit.Record{TS: ts, Model: "agent", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Body: map[string]any{"messages": []any{
				map[string]any{"role": "system", "content": "you are a helpful assistant"},
				map[string]any{"role": "user", "content": "hi"},
			}}},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai-completions:minimax:MiniMax-M3", Model: "MiniMax-M3"}}}
}

// TestEnsureRendered_RewritesOnEvidenceModeChange covers the fingerprint's
// second real axis: switching a record from fully-inline rendering to
// evidence-linked rendering (or back) must actually change the file on
// disk, not silently keep serving whichever mode wrote it first — the
// real-corpus failure mode was the opposite direction (evidence/ deleted,
// detail pages never regenerate their links to it — see
// TestEnsureRendered_RebuildsDeletedEvidence below for that exact case),
// but the underlying defect is symmetric: the skip predicate didn't know
// evidence mode was an input to Render at all.
func TestEnsureRendered_RewritesOnEvidenceModeChange(t *testing.T) {
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := recWithSysPrompt(ts)

	name, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, "")
	if err != nil {
		t.Fatal(err)
	}
	inlineData, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(inlineData), "you are a helpful assistant") {
		t.Fatalf("inline render should show the system prompt text directly, got:\n%s", inlineData)
	}

	if _, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, evidenceDir); err != nil {
		t.Fatal(err)
	}
	linkedData, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(linkedData), "you are a helpful assistant") {
		t.Errorf("switching to evidence mode did not rewrite the file — system prompt still inlined:\n%s", linkedData)
	}
	entries, err := os.ReadDir(evidenceDir)
	if err != nil {
		t.Fatalf("ReadDir(evidenceDir): %v", err)
	}
	if len(entries) == 0 {
		t.Errorf("switching to evidence mode should have written a sysprompt evidence file, found none")
	}
}

// TestEnsureRendered_RebuildsDeletedEvidence is the direct regression test
// for KNOWN_ISSUES §1.41's actual M9 scenario: evidence mode stays true
// across both calls (the detail page's own fingerprint never changes), but
// evidence/ gets deleted in between. An earlier version of this fix
// checked the fingerprint before ensuring evidence existed, so a fingerprint
// match short-circuited the function before the evidence calls ever ran —
// reproducing the exact bug this function exists to close, one level down.
// Evidence must be rebuilt on every call where linkEvidence is true,
// regardless of whether the detail page itself needs re-rendering.
func TestEnsureRendered_RebuildsDeletedEvidence(t *testing.T) {
	dir := t.TempDir()
	evidenceDir := filepath.Join(dir, "evidence")
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := recWithSysPrompt(ts)

	if _, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, evidenceDir); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(evidenceDir); err != nil || len(entries) == 0 {
		t.Fatalf("first call should have written at least one evidence file, got entries=%v err=%v", entries, err)
	}

	if err := os.RemoveAll(evidenceDir); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, evidenceDir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(evidenceDir)
	if err != nil {
		t.Fatalf("ReadDir(evidenceDir) after rebuild: %v", err)
	}
	if len(entries) == 0 {
		t.Error("evidence/ was deleted and the record re-rendered with the same evidence mode, but no evidence file was rebuilt — the linked detail page now points at a permanently dead link")
	}
}

func TestEnsureRendered_NoLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := minimalRec(ts)

	if _, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, ""); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want exactly 1 (the .md, no leftover .tmp): %v", len(entries), entries)
	}
}
