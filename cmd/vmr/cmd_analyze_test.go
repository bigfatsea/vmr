// Ver 2026-08-20 17:45, by Sonnet 5

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
)

// TestCmdAnalyze_ProducesFullSuiteInOneOutputRoot covers P6.5's actual
// user-facing promise: one call, one output directory, both halves'
// products present, and the story half's journeys actually rendered (not
// just listed) — not literally a single scan, see cmd_analyze.go's own
// doc comment for why that tradeoff was made.
func TestCmdAnalyze_ProducesFullSuiteInOneOutputRoot(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 20, 10, m, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "analyze fixture opening instruction")
	recs := []audit.Record{
		storyRec(at(0), []any{sys, u1}, storySSE("ok")),
		storyRec(at(1), []any{sys, u1, storyMsg("assistant", "ok"), storyMsg("user", "continue")}, storySSE("ok again")),
	}
	path := writeStoryJSONL(t, recs)

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze: %v", err)
	}

	for _, want := range []string{
		"vmr-report.md", "vmr-report.json", "vmr-requests.md",
		filepath.Join("stories", "vmr-stories.md"), filepath.Join("stories", "vmr-stories.json"),
	} {
		if _, err := os.Stat(filepath.Join(outDir, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	// The story half must have actually rendered the candidate journey
	// (analyze forces -render-all), not just listed it — otherwise the
	// requests-index -> journey edge (P6.2c) has nothing to link to.
	entries, err := os.ReadDir(filepath.Join(outDir, "stories"))
	if err != nil {
		t.Fatalf("ReadDir(stories): %v", err)
	}
	var sawJourney bool
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "journey-") && strings.HasSuffix(e.Name(), ".md") {
			sawJourney = true
		}
	}
	if !sawJourney {
		t.Error("no journey-*.md rendered — analyze should force -render-all")
	}
}

// TestCmdAnalyze_ReportLinksStoriesOnFirstCall locks in the story-before-
// report ordering inside cmdAnalyze: vmr-report.md must link to
// stories/vmr-stories.md (P6.2a) after a SINGLE `vmr analyze` call, not
// only from a second run onward — the ordering choice cmd_analyze.go's
// own comment explains (report.Markdown only links the index when it
// already exists at render time).
func TestCmdAnalyze_ReportLinksStoriesOnFirstCall(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 20, 11, m, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "ordering fixture opening instruction")
	recs := []audit.Record{
		storyRec(at(0), []any{sys, u1}, storySSE("ok")),
		storyRec(at(1), []any{sys, u1, storyMsg("assistant", "ok"), storyMsg("user", "continue")}, storySSE("ok again")),
	}
	path := writeStoryJSONL(t, recs)

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "vmr-report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "stories/vmr-stories.md") {
		t.Error("vmr-report.md doesn't link stories/vmr-stories.md after a single analyze call — story must run before report")
	}
}

// TestCmdAnalyze_ShareSameOutputDefault covers the "same -o for both
// halves, without the user having to pass it twice" half of P6.5 — no -o
// at all still lands both halves' products in the same place, since both
// cmdReport and cmdStory independently fall through to the identical
// "reports" default.
func TestCmdAnalyze_ShareSameOutputDefault(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	at := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "default -o fixture")
	recs := []audit.Record{
		storyRec(at, []any{sys, u1}, storySSE("ok")),
		storyRec(at.Add(time.Minute), []any{sys, u1, storyMsg("assistant", "ok"), storyMsg("user", "more")}, storySSE("ok2")),
	}
	// writeStoryJSONL puts the fixture under t.TempDir(), not cwd — pass
	// its absolute path so resolveInputPaths' glob still finds it after
	// the Chdir above.
	path := writeStoryJSONL(t, recs)

	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{path}) }); err != nil {
		t.Fatalf("cmdAnalyze: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reports", "vmr-report.md")); err != nil {
		t.Errorf("report half didn't land in default ./reports: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reports", "stories", "vmr-stories.md")); err != nil {
		t.Errorf("story half didn't land in the SAME default ./reports: %v", err)
	}
}
