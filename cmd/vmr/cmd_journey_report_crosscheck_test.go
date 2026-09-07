// Ver 2026-08-20, by Sonnet 5

// Cross-command integration coverage for P5.2's core invariant: a detail
// page `vmr analyze -journey` materializes (via journey.EnsureJourneyDetails, driven
// from the decision spine's "→ detail" links) must be byte-identical to
// the one `vmr analyze -details` writes for the SAME audit record — the P2
// guarantee internal/report/detail_test.go's TestBuildOnRecordMatchesWriteDetails
// locks on the report side alone. This is the one place both halves'
// production entry points (cmdAnalyze) run against the same source
// file and get diffed, including a stitch-boundary record: Step.PrevManifest
// must stay nil there, or the two commands would silently disagree.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/audit"
)

// crossCheckFixture builds an s231-style two-Lineage source file: five
// Append records, then one Contract record (history collapses, opening
// instruction survives) — the same shape internal/journey/stitch_test.go's
// s231StyleFixture uses to force a Stitch. Returns the file path.
func crossCheckFixture(t *testing.T) string {
	t.Helper()
	at := func(m int) time.Time { return time.Date(2026, 8, 20, 9, m, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "cross-check fixture opening instruction")

	var recs []audit.Record
	msgsList := []any{sys, u1}
	for i := 0; i < 5; i++ {
		recs = append(recs, journeyRec(at(i), append([]any{}, msgsList...), journeySSE("ok")))
		msgsList = append(msgsList, journeyMsg("assistant", fmt.Sprintf("step reply %d", i)))
		if i >= 2 {
			msgsList = append(msgsList, journeyMsg("tool", fmt.Sprintf("tool output %d", i)))
		}
	}
	// Contract: history collapses to [sys v2, u1, step reply 3, tool output 3]
	// — 3 shared distinct keys, clearing stitchMinAbsOverlap, with its OWN
	// new system prompt (same shape TestSystemPromptEras_StitchBoundaryChange
	// in internal/journey exercises).
	recs = append(recs, journeyRec(at(30), []any{journeyMsg("system", "sys v2"), u1,
		journeyMsg("assistant", "step reply 3"), journeyMsg("tool", "tool output 3"),
		journeyMsg("assistant", "post-break reply")}, journeySSE("continuing")))
	return writeJourneyJSONL(t, recs)
}

// TestEnsureJourneyDetails_MatchesReportDetails runs `vmr analyze -journey-only -render-all` and
// `vmr analyze -macro-only -details` against the same source file (into separate output
// directories) and asserts every detail page is byte-identical for the same record —
// covering both an ordinary same-Lineage Step (prev != nil) and the
// stitch-boundary Step (prev must be nil on both sides).
func TestEnsureJourneyDetails_MatchesReportDetails(t *testing.T) {
	path := crossCheckFixture(t)
	root := t.TempDir()
	journeyOut := filepath.Join(root, "journey-out")
	reportOut := filepath.Join(root, "report-out")

	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-journey-only", "-render-all", "-o", journeyOut, path}) }); err != nil {
		t.Fatalf("cmdAnalyze -journey-only -render-all: %v", err)
	}
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-macro-only", "-details", "-o", reportOut, path}) }); err != nil {
		t.Fatalf("cmdAnalyze -macro-only -details: %v", err)
	}

	journeyDetails := filepath.Join(journeyOut, "requests", "details")
	reportDetails := filepath.Join(reportOut, "requests", "details")
	entries, err := os.ReadDir(journeyDetails)
	if err != nil {
		t.Fatalf("ReadDir(journey details): %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("materialized %d detail pages, want 6 (one per record)", len(entries))
	}

	compared := 0
	for _, e := range entries {
		journeyBody, err := os.ReadFile(filepath.Join(journeyDetails, e.Name()))
		if err != nil {
			t.Fatalf("reading journey detail %s: %v", e.Name(), err)
		}
		reportPath := filepath.Join(reportDetails, e.Name())
		reportBody, err := os.ReadFile(reportPath)
		if err != nil {
			t.Fatalf("report half never wrote a same-named file for %s (filenames should be a pure "+
				"function of the record's own coordinate, identical regardless of which command computed "+
				"it): %v", e.Name(), err)
		}
		if string(journeyBody) != string(reportBody) {
			t.Errorf("detail page %s differs between journey and report -details — this is exactly "+
				"the P2 byte-identical invariant breaking:\n--- journey ---\n%s\n--- report ---\n%s",
				e.Name(), journeyBody, reportBody)
		}
		compared++
	}
	if compared != 6 {
		t.Fatalf("compared %d detail pages, want 6", compared)
	}
}

// dirFileNames lists the base names of every file directly under dir
// (non-recursive — both fixtures here are flat single-level outputs, so
// nesting would only hide a real filename mismatch behind a deeper walk).
func dirFileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// assertDirsByteIdentical compares every top-level file two output
// directories share by name — used by the P15.1 crosschecks, since
// "same file set, same bytes" is exactly what equivalent flags must mean.
// manifest.json is skipped: it carries the run's wall-clock generated_at
// and the absolute input paths (temp dirs differ between runs).
func assertDirsByteIdentical(t *testing.T, gotDir, wantDir string) {
	t.Helper()
	gotNames := dirFileNames(t, gotDir)
	wantNames := dirFileNames(t, wantDir)
	if len(gotNames) != len(wantNames) {
		t.Fatalf("file count mismatch: got %v, want %v", gotNames, wantNames)
	}
	for _, name := range gotNames {
		if name == "manifest.json" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(gotDir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		want, err := os.ReadFile(filepath.Join(wantDir, name))
		if err != nil {
			t.Fatalf("%s exists in gotDir but not wantDir: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
		}
	}
}

// TestCmdAnalyze_MacroOnlyDeterminism covers P15.1: `vmr analyze
// -macro-only` must produce deterministic output without creating a journeys/ directory.
func TestCmdAnalyze_MacroOnlyDeterminism(t *testing.T) {
	path := crossCheckFixture(t)
	root := t.TempDir()
	out1 := filepath.Join(root, "out1")
	out2 := filepath.Join(root, "out2")

	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", out1, "-macro-only", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -macro-only 1: %v", err)
	}
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", out2, "-macro-only", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -macro-only 2: %v", err)
	}

	if _, err := os.Stat(filepath.Join(out1, "journeys")); !os.IsNotExist(err) {
		t.Errorf("-macro-only should never create a journeys/ directory, stat err = %v", err)
	}
	assertDirsByteIdentical(t, out1, out2)
}

// TestCmdAnalyze_ListOnly covers P15.1: `vmr analyze -list-only`
// produces the candidate listing in journeys/index.{json,md}, with no j-*.md rendered.
func TestCmdAnalyze_ListOnly(t *testing.T) {
	path := crossCheckFixture(t)
	root := t.TempDir()
	listOut := filepath.Join(root, "list-out")

	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", listOut, "-list-only", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -list-only: %v", err)
	}

	if got := journeyFileNames(t, filepath.Join(listOut, "journeys")); len(got) != 0 {
		t.Errorf("-list-only should render no j-*.md, got %v", got)
	}
	if _, err := os.Stat(filepath.Join(listOut, "journeys", "index.json")); err != nil {
		t.Errorf("journeys/index.json should be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(listOut, "journeys", "index.md")); err != nil {
		t.Errorf("journeys/index.md should be written: %v", err)
	}
}

// TestCmdAnalyze_MacroOnlyListOnly_MutualExclusion locks in
// validateAnalyzeModeFlags' rejection rules (P15.1) — each combination
// listed here must fail fast rather than silently pick one flag over the
// other.
func TestCmdAnalyze_MacroOnlyListOnly_MutualExclusion(t *testing.T) {
	path := crossCheckFixture(t)
	cases := []struct {
		name string
		args []string
	}{
		{"macro-only + list-only", []string{"-macro-only", "-list-only"}},
		{"macro-only + journey", []string{"-macro-only", "-journey", "j-anything"}},
		{"macro-only + render-all", []string{"-macro-only", "-render-all"}},
		{"list-only + benchmark", []string{"-list-only", "-benchmark"}},
		{"list-only + details", []string{"-list-only", "-details"}},
		{"journey-only + macro-only", []string{"-journey-only", "-macro-only"}},
		{"journey-only + list-only", []string{"-journey-only", "-list-only"}},
		{"journey-only + journey", []string{"-journey-only", "-journey", "j-anything"}},
		{"journey-only + benchmark", []string{"-journey-only", "-benchmark"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			outDir := filepath.Join(t.TempDir(), "out")
			args := append(append([]string{"-o", outDir}, c.args...), path)
			err := captureStdoutErr(t, func() error { return cmdAnalyze(args) })
			if err == nil {
				t.Errorf("cmdAnalyze(%v) should have failed validation, got nil error", args)
			}
		})
	}
}

// TestCmdAnalyze_JourneyOnly covers -journey-only (P15.1):
// alone, it must run the journey half only (non-noise scope, no report files);
// combined with -render-all, it renders every candidate.
func TestCmdAnalyze_JourneyOnly(t *testing.T) {
	path := crossCheckFixture(t)

	t.Run("alone: non-noise scope, no report half", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "out")
		if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, "-journey-only", path}) }); err != nil {
			t.Fatalf("cmdAnalyze -journey-only: %v", err)
		}
		if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); !os.IsNotExist(err) {
			t.Errorf("-journey-only should never write vmr-report.md, stat err = %v", err)
		}
		if got := journeyFileNames(t, filepath.Join(outDir, "journeys")); len(got) == 0 {
			t.Error("-journey-only should still render the default suite's non-noise candidates")
		}
	})

	t.Run("with -render-all", func(t *testing.T) {
		root := t.TempDir()
		analyzeOut := filepath.Join(root, "analyze-out")
		if err := captureStdoutErr(t, func() error {
			return cmdAnalyze([]string{"-o", analyzeOut, "-journey-only", "-render-all", path})
		}); err != nil {
			t.Fatalf("cmdAnalyze -journey-only -render-all: %v", err)
		}
		if _, err := os.Stat(filepath.Join(analyzeOut, "vmr-report.md")); !os.IsNotExist(err) {
			t.Errorf("-journey-only -render-all should never write vmr-report.md: %v", err)
		}
		if got := journeyFileNames(t, filepath.Join(analyzeOut, "journeys")); len(got) == 0 {
			t.Error("-journey-only -render-all should render all candidates")
		}
	})
}

// TestCmdAnalyze_RenderAllAlone_NeverWritesReportHalf verifies -journey-only -render-all
// never touches the report half.
func TestCmdAnalyze_RenderAllAlone_NeverWritesReportHalf(t *testing.T) {
	path := crossCheckFixture(t)
	root := t.TempDir()
	journeyOut := filepath.Join(root, "journey-out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", journeyOut, "-journey-only", "-render-all", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -journey-only -render-all: %v", err)
	}
	for _, name := range []string{"vmr-report.md", filepath.Join("macro", "summary.json"), "requests/index.json"} {
		if _, err := os.Stat(filepath.Join(journeyOut, name)); !os.IsNotExist(err) {
			t.Errorf("`-journey-only -render-all` should never write %s (report half), stat err = %v", name, err)
		}
	}
	if got := journeyFileNames(t, filepath.Join(journeyOut, "journeys")); len(got) == 0 {
		t.Error("`-journey-only -render-all` should still render every candidate journey")
	}
}

// TestCmdAnalyze_RenderAllBare_StillRunsReportHalf is the direct converse of
// the regression above: `vmr analyze -render-all` (called directly, not
// through the legacy alias) must keep running BOTH halves — P9's
// original default-suite contract — since analyzeRun.skipMacroReport is an
// internal-only field cmdAnalyze's own flag set never sets.
func TestCmdAnalyze_RenderAllBare_StillRunsReportHalf(t *testing.T) {
	path := crossCheckFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, "-render-all", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -render-all: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); err != nil {
		t.Errorf("`vmr analyze -render-all` should still write vmr-report.md: %v", err)
	}
}

// TestCmdAnalyze_MatchesDispatchShape locks in analyze's dispatch equivalence:
// equivalent flags must produce byte-identical output. -journey '*' and -list-only
// are the two shapes that share dispatch paths.
func TestCmdAnalyze_MatchesDispatchShape(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"list-only", []string{"-list-only"}},
		{"benchmark", []string{"-benchmark"}},
		// "*" resolves to crossCheckFixture's single stitched candidate —
		// exercises the len(targets)==1 branch (renderJourney), the one
		// -journey shape that also accepts -llm-addr.
		{"journey (single match)", []string{"-journey", "*"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := crossCheckFixture(t)
			root := t.TempDir()
			out1 := filepath.Join(root, "out1")
			out2 := filepath.Join(root, "out2")

			args1 := append(append([]string{"-o", out1}, c.args...), path)
			if err := captureStdoutErr(t, func() error { return cmdAnalyze(args1) }); err != nil {
				t.Fatalf("cmdAnalyze(%v): %v", args1, err)
			}
			args2 := append(append([]string{"-o", out2}, c.args...), path)
			if err := captureStdoutErr(t, func() error { return cmdAnalyze(args2) }); err != nil {
				t.Fatalf("cmdAnalyze(%v): %v", args2, err)
			}
			assertDirsByteIdentical(t, out1, out2)
			assertDirsByteIdentical(t, filepath.Join(out1, "journeys"), filepath.Join(out2, "journeys"))
		})
	}
}

// TestCmdReport_LLMKeyMatchesAnalyzeMacroOnly: -llm-key identifies
// self-analysis traffic. One record is tagged as if it came from an -llm-addr self-analysis
// call under "secret-key"; it must be excluded exactly when given -llm-key secret-key.
func TestCmdReport_LLMKeyMatchesAnalyzeMacroOnly(t *testing.T) {
	const llmKey = "secret-key"
	at := func(m int) time.Time { return time.Date(2026, 8, 21, 9, m, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "ordinary request")
	ordinary := journeyRec(at(0), []any{sys, u1}, journeySSE("ok"))
	selfTraffic := journeyRec(at(1), []any{sys, journeyMsg("user", "self-analysis call")}, journeySSE("ok"))
	selfTraffic.ClientKeyTag = audit.KeyTag(llmKey)
	path := writeJourneyJSONL(t, []audit.Record{ordinary, selfTraffic})

	loadMeta := func(dir string) int {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(dir, "macro", "summary.json"))
		if err != nil {
			t.Fatalf("reading macro/summary.json: %v", err)
		}
		var sum struct {
			Meta struct {
				SelfTrafficExcluded int `json:"self_traffic_excluded"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(body, &sum); err != nil {
			t.Fatalf("unmarshal macro/summary.json: %v", err)
		}
		return sum.Meta.SelfTrafficExcluded
	}

	root := t.TempDir()
	analyzeOut := filepath.Join(root, "analyze-out")
	noKeyOut := filepath.Join(root, "nokey-out")

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", analyzeOut, "-macro-only", "-llm-key", llmKey, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -macro-only -llm-key: %v", err)
	}
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", noKeyOut, "-macro-only", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -macro-only (no -llm-key): %v", err)
	}

	if got := loadMeta(analyzeOut); got != 1 {
		t.Errorf("cmdAnalyze -macro-only -llm-key excluded %d records, want 1", got)
	}
	if got := loadMeta(noKeyOut); got != 0 {
		t.Errorf("cmdAnalyze -macro-only without -llm-key excluded %d records, want 0 (the tag shouldn't match anything by accident)", got)
	}
}
