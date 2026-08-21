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
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

// generatedAtRE strips vmr-report.json's Meta.generated_at wall-clock
// timestamp — the one field two independent runs of the same command are
// never byte-identical on (see runReport's own now := time.Now()) — before
// comparing two report.json bodies produced by separate cmdReport/
// cmdAnalyze invocations a few milliseconds apart.
var generatedAtRE = regexp.MustCompile(`"generated_at": "[^"]*"`)

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
// directories share by name — used by both P15.1 crosschecks below, since
// "same file set, same bytes" is exactly what "-macro-only/-list-only are
// equivalent to the alias they stand in for" means.
func assertDirsByteIdentical(t *testing.T, gotDir, wantDir string) {
	t.Helper()
	gotNames := dirFileNames(t, gotDir)
	wantNames := dirFileNames(t, wantDir)
	if len(gotNames) != len(wantNames) {
		t.Fatalf("file count mismatch: got %v, want %v", gotNames, wantNames)
	}
	for _, name := range gotNames {
		got, err := os.ReadFile(filepath.Join(gotDir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		want, err := os.ReadFile(filepath.Join(wantDir, name))
		if err != nil {
			t.Fatalf("%s exists in gotDir but not wantDir: %v", name, err)
		}
		if name == "vmr-report.json" {
			got = generatedAtRE.ReplaceAll(got, nil)
			want = generatedAtRE.ReplaceAll(want, nil)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
		}
	}
}

// TestCmdAnalyze_MacroOnlyMatchesReport covers P15.1: `vmr analyze
// -macro-only` must produce exactly what bare `vmr report` produces for the
// same input — same file set (notably: no stories/ directory at all,
// per dispatchAnalyze's doc comment on why -macro-only skips
// setupStoryRun rather than running it and discarding the result), same
// bytes modulo the wall-clock generated_at field.
func TestCmdAnalyze_MacroOnlyMatchesReport(t *testing.T) {
	path := crossCheckFixture(t)
	root := t.TempDir()
	macroOut := filepath.Join(root, "macro-out")
	reportOut := filepath.Join(root, "report-out")

	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", macroOut, "-macro-only", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -macro-only: %v", err)
	}
	if err := captureStdoutErr(t, func() error { return cmdReport([]string{"-o", reportOut, path}) }); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}

	if _, err := os.Stat(filepath.Join(macroOut, "stories")); !os.IsNotExist(err) {
		t.Errorf("-macro-only should never create a stories/ directory (bare `vmr report` never does), stat err = %v", err)
	}
	assertDirsByteIdentical(t, macroOut, reportOut)
}

// TestCmdAnalyze_ListOnlyMatchesStory covers P15.1: `vmr analyze -list-only`
// must produce exactly what bare `vmr story` (no selector) produces for the
// same input — the candidate listing, written into stories/, with no
// journey-*.md rendered.
func TestCmdAnalyze_ListOnlyMatchesStory(t *testing.T) {
	path := crossCheckFixture(t)
	root := t.TempDir()
	listOut := filepath.Join(root, "list-out")
	storyOut := filepath.Join(root, "story-out")

	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", listOut, "-list-only", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -list-only: %v", err)
	}
	if err := captureStdoutErr(t, func() error { return cmdStory([]string{"-o", storyOut, path}) }); err != nil {
		t.Fatalf("cmdStory (bare): %v", err)
	}

	if got := journeyFileNames(t, filepath.Join(listOut, "stories")); len(got) != 0 {
		t.Errorf("-list-only should render no journey-*.md, got %v", got)
	}
	assertDirsByteIdentical(t, filepath.Join(listOut, "stories"), filepath.Join(storyOut, "stories"))
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
		{"list-only + corpus", []string{"-list-only", "-corpus"}},
		{"list-only + details", []string{"-list-only", "-details"}},
		{"story-only + macro-only", []string{"-story-only", "-macro-only"}},
		{"story-only + list-only", []string{"-story-only", "-list-only"}},
		{"story-only + journey", []string{"-story-only", "-journey", "j-anything"}},
		{"story-only + corpus", []string{"-story-only", "-corpus"}},
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

// TestCmdAnalyze_StoryOnly covers -story-only (P15.1, added after an
// independent review caught that the first cut of this fix — an
// analyzeRun-only field cmdAnalyze itself could never set — violated
// analyze's own "single entry point, strictly covers every alias behavior"
// premise): alone, it must run the story half only (non-noise scope, no
// report files); combined with -render-all, it must be `vmr analyze`'s own
// directly-reachable equivalent of `vmr story -render-all` — no report
// files, every candidate rendered, byte-identical to the alias.
func TestCmdAnalyze_StoryOnly(t *testing.T) {
	path := crossCheckFixture(t)

	t.Run("alone: non-noise scope, no report half", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "out")
		if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, "-story-only", path}) }); err != nil {
			t.Fatalf("cmdAnalyze -story-only: %v", err)
		}
		if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); !os.IsNotExist(err) {
			t.Errorf("-story-only should never write vmr-report.md, stat err = %v", err)
		}
		if got := journeyFileNames(t, filepath.Join(outDir, "stories")); len(got) == 0 {
			t.Error("-story-only should still render the default suite's non-noise candidates")
		}
	})

	t.Run("with -render-all: matches `vmr story -render-all` exactly", func(t *testing.T) {
		root := t.TempDir()
		analyzeOut := filepath.Join(root, "analyze-out")
		storyOut := filepath.Join(root, "story-out")
		if err := captureStdoutErr(t, func() error {
			return cmdAnalyze([]string{"-o", analyzeOut, "-story-only", "-render-all", path})
		}); err != nil {
			t.Fatalf("cmdAnalyze -story-only -render-all: %v", err)
		}
		if err := captureStdoutErr(t, func() error { return cmdStory([]string{"-o", storyOut, "-render-all", path}) }); err != nil {
			t.Fatalf("cmdStory -render-all: %v", err)
		}
		assertDirsByteIdentical(t, analyzeOut, storyOut)
		assertDirsByteIdentical(t, filepath.Join(analyzeOut, "stories"), filepath.Join(storyOut, "stories"))
	})
}

// TestCmdStory_RenderAllAlone_NeverWritesReportHalf is a regression test for
// a bug introduced and caught within P15.2's own execution (not by any
// pre-existing test — see this ActionPlan's execution record): once
// cmdStory started forwarding into dispatchAnalyze's shared default-suite
// branch, `vmr story -render-all` (no other selector) would have started
// also writing vmr-report.{json,md}/vmr-requests*, because that branch is
// shared with bare `vmr analyze`, which has always run both halves. The
// original `vmr story -render-all` never touched the report half —
// analyzeRun.skipMacroReport exists specifically to keep that true. The
// story-half output itself (stories/) must still match `vmr analyze
// -render-all`'s (see TestCmdAnalyze_RenderAllBare_StillRunsReportHalf for
// the converse: analyze's own -render-all keeps the report half).
func TestCmdStory_RenderAllAlone_NeverWritesReportHalf(t *testing.T) {
	path := crossCheckFixture(t)
	root := t.TempDir()
	storyOut := filepath.Join(root, "story-out")
	analyzeOut := filepath.Join(root, "analyze-out")
	if err := captureStdoutErr(t, func() error { return cmdStory([]string{"-o", storyOut, "-render-all", path}) }); err != nil {
		t.Fatalf("cmdStory -render-all: %v", err)
	}
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", analyzeOut, "-render-all", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -render-all: %v", err)
	}
	for _, name := range []string{"vmr-report.json", "vmr-report.md", "vmr-requests.json", "vmr-requests.md", "vmr-requests-failed.jsonl", "vmr-requests-failed.md"} {
		if _, err := os.Stat(filepath.Join(storyOut, name)); !os.IsNotExist(err) {
			t.Errorf("`vmr story -render-all` should never write %s (report half), stat err = %v", name, err)
		}
	}
	if got := journeyFileNames(t, filepath.Join(storyOut, "stories")); len(got) == 0 {
		t.Error("`vmr story -render-all` should still render every candidate journey")
	}
	assertDirsByteIdentical(t, filepath.Join(storyOut, "stories"), filepath.Join(analyzeOut, "stories"))
}

// TestCmdAnalyze_RenderAllBare_StillRunsReportHalf is the direct converse of
// the regression above: `vmr analyze -render-all` (called directly, not
// through the `vmr story` alias) must keep running BOTH halves — P9's
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

// TestCmdStory_MatchesAnalyzeEquivalent covers P15.2/P15.3: `vmr story`'s
// selector-driven call shapes (the ones that route through dispatchAnalyze's
// switch without hitting the default-suite/skipMacroReport special case —
// see TestCmdStory_RenderAllAlone_NeverWritesReportHalf for -render-all's
// own, deliberately-different comparison) must produce output byte-identical
// to calling `vmr analyze` directly with the documented equivalent flags —
// locking in "the alias's routing can't drift from analyze's" as an
// executable fact, not just a doc comment.
func TestCmdStory_MatchesAnalyzeEquivalent(t *testing.T) {
	cases := []struct {
		name        string
		storyArgs   []string
		analyzeArgs []string
	}{
		{"bare (list-only)", nil, []string{"-list-only"}},
		{"corpus", []string{"-corpus"}, []string{"-corpus"}},
		// "*" resolves to crossCheckFixture's single stitched candidate —
		// exercises the len(targets)==1 branch (renderJourney), the one
		// -journey shape that also accepts -llm-addr.
		{"journey (single match)", []string{"-journey", "*"}, []string{"-journey", "*"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := crossCheckFixture(t)
			root := t.TempDir()
			storyOut := filepath.Join(root, "story-out")
			analyzeOut := filepath.Join(root, "analyze-out")

			storyArgs := append(append([]string{"-o", storyOut}, c.storyArgs...), path)
			if err := captureStdoutErr(t, func() error { return cmdStory(storyArgs) }); err != nil {
				t.Fatalf("cmdStory(%v): %v", storyArgs, err)
			}
			analyzeArgs := append(append([]string{"-o", analyzeOut}, c.analyzeArgs...), path)
			if err := captureStdoutErr(t, func() error { return cmdAnalyze(analyzeArgs) }); err != nil {
				t.Fatalf("cmdAnalyze(%v): %v", analyzeArgs, err)
			}
			assertDirsByteIdentical(t, storyOut, analyzeOut)
			assertDirsByteIdentical(t, filepath.Join(storyOut, "stories"), filepath.Join(analyzeOut, "stories"))
		})
	}
}

// TestCmdReport_LLMKeyMatchesAnalyzeMacroOnly covers P15.3 (KNOWN_ISSUES
// §1.34/§1.38): before this, cmdReport had no -llm-key flag at all, so
// `vmr report`'s self-traffic exclusion could only ever read report.yaml's
// llm_key — never override it per-call like `vmr analyze`/`vmr story`
// could. One record is tagged as if it came from an -llm-addr self-analysis
// call under "secret-key"; both commands must exclude exactly that record
// when given -llm-key secret-key, and neither may exclude it without the
// flag.
func TestCmdReport_LLMKeyMatchesAnalyzeMacroOnly(t *testing.T) {
	const llmKey = "secret-key"
	at := func(m int) time.Time { return time.Date(2026, 8, 21, 9, m, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "ordinary request")
	ordinary := storyRec(at(0), []any{sys, u1}, storySSE("ok"))
	selfTraffic := storyRec(at(1), []any{sys, storyMsg("user", "self-analysis call")}, storySSE("ok"))
	selfTraffic.ClientKeyTag = audit.KeyTag(llmKey)
	path := writeStoryJSONL(t, []audit.Record{ordinary, selfTraffic})

	loadMeta := func(dir string) int {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(dir, "vmr-report.json"))
		if err != nil {
			t.Fatalf("reading vmr-report.json: %v", err)
		}
		var rep struct {
			Meta struct {
				SelfTrafficExcluded int `json:"self_traffic_excluded"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(body, &rep); err != nil {
			t.Fatalf("unmarshal vmr-report.json: %v", err)
		}
		return rep.Meta.SelfTrafficExcluded
	}

	root := t.TempDir()
	reportOut := filepath.Join(root, "report-out")
	analyzeOut := filepath.Join(root, "analyze-out")
	noKeyOut := filepath.Join(root, "nokey-out")

	if err := captureStdoutErr(t, func() error { return cmdReport([]string{"-o", reportOut, "-llm-key", llmKey, path}) }); err != nil {
		t.Fatalf("cmdReport -llm-key: %v", err)
	}
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", analyzeOut, "-macro-only", "-llm-key", llmKey, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -macro-only -llm-key: %v", err)
	}
	if err := captureStdoutErr(t, func() error { return cmdReport([]string{"-o", noKeyOut, path}) }); err != nil {
		t.Fatalf("cmdReport (no -llm-key): %v", err)
	}

	if got := loadMeta(reportOut); got != 1 {
		t.Errorf("cmdReport -llm-key excluded %d records, want 1", got)
	}
	if got := loadMeta(analyzeOut); got != 1 {
		t.Errorf("cmdAnalyze -macro-only -llm-key excluded %d records, want 1", got)
	}
	if got := loadMeta(noKeyOut); got != 0 {
		t.Errorf("cmdReport without -llm-key excluded %d records, want 0 (the tag shouldn't match anything by accident)", got)
	}
}
