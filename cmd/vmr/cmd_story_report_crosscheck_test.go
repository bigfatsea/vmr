// Ver 2026-08-20, by Sonnet 5

// Cross-command integration coverage for P5.2's core invariant: a detail
// page `vmr story` materializes (via story.EnsureJourneyDetails, driven
// from the decision spine's "→ detail" links) must be byte-identical to
// the one `vmr report -details` writes for the SAME audit record — the P2
// guarantee internal/report/detail_test.go's TestBuildOnRecordMatchesWriteDetails
// locks on the report side alone. This is the one place both commands'
// production entry points (cmdStory/cmdReport) run against the same source
// file and get diffed, including a stitch-boundary record: Step.PrevManifest
// must stay nil there, or the two commands would silently disagree.
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/audit"
)

// crossCheckFixture builds an s231-style two-Lineage source file: five
// Append records, then one Contract record (history collapses, opening
// instruction survives) — the same shape internal/story/stitch_test.go's
// s231StyleFixture uses to force a Stitch. Returns the file path.
func crossCheckFixture(t *testing.T) string {
	t.Helper()
	at := func(m int) time.Time { return time.Date(2026, 8, 20, 9, m, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "cross-check fixture opening instruction")

	var recs []audit.Record
	msgsList := []any{sys, u1}
	for i := 0; i < 5; i++ {
		recs = append(recs, storyRec(at(i), append([]any{}, msgsList...), storySSE("ok")))
		msgsList = append(msgsList, storyMsg("assistant", "step reply"))
	}
	// Contract: history collapses to just [sys v2, u1] — a stitch boundary
	// with its OWN new system prompt, same shape TestSystemPromptEras_
	// StitchBoundaryChange (internal/story) exercises.
	recs = append(recs, storyRec(at(30), []any{storyMsg("system", "sys v2"), u1, storyMsg("assistant", "post-break reply")}, storySSE("continuing")))
	return writeStoryJSONL(t, recs)
}

// TestEnsureJourneyDetails_MatchesReportDetails runs `vmr story` and
// `vmr report -details` against the same source file (into separate output
// directories) and asserts every detail page `vmr story` wrote is
// byte-identical to `vmr report -details`'s own page for the same record —
// covering both an ordinary same-Lineage Step (prev != nil) and the
// stitch-boundary Step (prev must be nil on both sides).
func TestEnsureJourneyDetails_MatchesReportDetails(t *testing.T) {
	path := crossCheckFixture(t)
	root := t.TempDir()
	storyOut := filepath.Join(root, "story-out")
	reportOut := filepath.Join(root, "report-out")

	if err := captureStdoutErr(t, func() error { return cmdStory([]string{"-o", storyOut, "-render-all", path}) }); err != nil {
		t.Fatalf("cmdStory -render-all: %v", err)
	}
	if err := captureStdoutErr(t, func() error { return cmdReport([]string{"-o", reportOut, "-details", path}) }); err != nil {
		t.Fatalf("cmdReport -details: %v", err)
	}

	storyDetails := filepath.Join(storyOut, "details")
	reportDetails := filepath.Join(reportOut, "details")
	entries, err := os.ReadDir(storyDetails)
	if err != nil {
		t.Fatalf("ReadDir(story details): %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("vmr story materialized %d detail pages, want 6 (one per record)", len(entries))
	}

	compared := 0
	for _, e := range entries {
		storyBody, err := os.ReadFile(filepath.Join(storyDetails, e.Name()))
		if err != nil {
			t.Fatalf("reading story detail %s: %v", e.Name(), err)
		}
		reportPath := filepath.Join(reportDetails, e.Name())
		reportBody, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatalf("vmr report -details never wrote a same-named file for %s (filenames should be a pure "+
				"function of the record's own coordinate, identical regardless of which command computed "+
				"it): %v", e.Name(), err)
		}
		if string(storyBody) != string(reportBody) {
			t.Errorf("detail page %s differs between `vmr story` and `vmr report -details` — this is exactly "+
				"the P2 byte-identical invariant breaking:\n--- story ---\n%s\n--- report ---\n%s",
				e.Name(), storyBody, reportBody)
		}
		compared++
	}
	if compared != 6 {
		t.Fatalf("compared %d detail pages, want 6", compared)
	}
}
