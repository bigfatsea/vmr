// Ver 2026-08-20 17:45, by Sonnet 5

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/dashboard"
	"vmr/internal/i18n"
	"vmr/internal/journey"
	"vmr/internal/report"
)

// TestCmdAnalyze_ProducesFullSuiteInOneOutputRoot covers P6.5's actual
// user-facing promise: one call, one output directory, both halves'
// products present, and the story half's journeys actually rendered (not
// just listed) — not literally a single scan, see cmd_analyze.go's own
// doc comment for why that tradeoff was made.
func TestCmdAnalyze_ProducesFullSuiteInOneOutputRoot(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 8, 20, 10, m, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "analyze fixture opening instruction")
	recs := []audit.Record{
		journeyRec(at(0), []any{sys, u1}, journeySSE("ok")),
		journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "ok"), journeyMsg("user", "continue")}, journeySSE("ok again")),
	}
	path := writeJourneyJSONL(t, recs)

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze: %v", err)
	}

	for _, want := range []string{
		"vmr-report.md", "manifest.json", filepath.Join("macro", "summary.json"),
		filepath.Join("requests", "index.json"),
		filepath.Join("journeys", "index.md"), filepath.Join("journeys", "index.json"),
	} {
		if _, err := os.Stat(filepath.Join(outDir, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	// The story half must have actually rendered the candidate journey
	// (analyze's default suite renders every category=task candidate,
	// P9.2 — this fixture's journey has no cron/heartbeat/subagent title
	// marker, so it classifies as task and gets rendered by default), not
	// just listed it — otherwise the requests-index -> journey edge
	// (P6.2c) has nothing to link to.
	entries, err := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	if err != nil {
		t.Fatalf("ReadDir(journeys/details): %v", err)
	}
	var sawJourney bool
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "j-") && strings.HasSuffix(e.Name(), ".md") {
			sawJourney = true
		}
	}
	if !sawJourney {
		t.Error("no j-*.md rendered — analyze's default suite should render category=task candidates")
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
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "ordering fixture opening instruction")
	recs := []audit.Record{
		journeyRec(at(0), []any{sys, u1}, journeySSE("ok")),
		journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "ok"), journeyMsg("user", "continue")}, journeySSE("ok again")),
	}
	path := writeJourneyJSONL(t, recs)

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "vmr-report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "journeys/index.md") {
		t.Error("vmr-report.md doesn't link journeys/index.md after a single analyze call — story must run before report")
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
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "default -o fixture")
	recs := []audit.Record{
		journeyRec(at, []any{sys, u1}, journeySSE("ok")),
		journeyRec(at.Add(time.Minute), []any{sys, u1, journeyMsg("assistant", "ok"), journeyMsg("user", "more")}, journeySSE("ok2")),
	}
	// writeJourneyJSONL puts the fixture under t.TempDir(), not cwd — pass
	// its absolute path so resolveInputPaths' glob still finds it after
	// the Chdir above.
	path := writeJourneyJSONL(t, recs)

	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{path}) }); err != nil {
		t.Fatalf("cmdAnalyze: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reports", "vmr-report.md")); err != nil {
		t.Errorf("report half didn't land in default ./reports: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reports", "journeys", "index.md")); err != nil {
		t.Errorf("story half didn't land in the SAME default ./reports: %v", err)
	}
}

// journeyFileNames lists the rendered journey-*.md basenames in dir — shared
// by the P9.2 scope tests below.
func journeyFileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "details"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "j-") && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestCmdAnalyze_DefaultSuiteExcludesHeartbeat covers P14.1 (originally
// P9.2, narrowed by P14.1/journey.IsNoiseCategory — see
// TestCmdAnalyze_DefaultSuiteRendersCronAndSubagent for the categories that
// changed): the default suite (no selector, no -render-all) excludes only
// heartbeat candidates — a heartbeat-titled candidate stays in the index
// but doesn't get a journey-*.md until -render-all (or a targeted -journey)
// asks for it.
func TestCmdAnalyze_DefaultSuiteExcludesHeartbeat(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	taskU1 := journeyMsg("user", "调研一下 A 股新股打新收益")
	taskR1 := journeyRec(at(0), []any{sys, taskU1}, journeySSE("开工"))
	taskR2 := journeyRec(at(1), []any{sys, taskU1, journeyMsg("assistant", "done")}, journeySSE("完成"))

	// [OpenClaw heartbeat poll] is the literal title-marker classifyJourney
	// checks for (internal/journey/candidates.go) — resolveTaskProfile()
	// defaults to OpenClawAware, and P7.2's bracket-stripping regexes only
	// touch timestamp/message_id markers, not this one, so it survives into
	// the derived title unchanged.
	hbU1 := journeyMsg("user", "[OpenClaw heartbeat poll] check in")
	hbR1 := journeyRec(at(10), []any{sys, hbU1}, journeySSE("ack"))
	hbR2 := journeyRec(at(11), []any{sys, hbU1, journeyMsg("assistant", "ack")}, journeySSE("ack2"))

	path := writeJourneyJSONL(t, []audit.Record{taskR1, taskR2, hbR1, hbR2})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze (default suite): %v", err)
	}

	idx := journey.LoadJourneyIndex(filepath.Join(outDir, "journeys", "index.json"))
	if len(idx.Journeys) != 2 {
		t.Fatalf("index should list both candidates regardless of render scope, got %d: %+v", len(idx.Journeys), idx.Journeys)
	}
	var sawTask, sawHeartbeat bool
	for _, row := range idx.Journeys {
		switch row.Category {
		case journey.CategoryTask:
			sawTask = true
		case journey.CategoryHeartbeat:
			sawHeartbeat = true
		}
	}
	if !sawTask || !sawHeartbeat {
		t.Fatalf("expected one task and one heartbeat candidate in the index, got: %+v", idx.Journeys)
	}

	got := journeyFileNames(t, filepath.Join(outDir, "journeys"))
	if len(got) != 1 {
		t.Fatalf("default suite should render exactly the 1 task candidate, got %d: %v", len(got), got)
	}

	// -render-all opts back into full materialization — both candidates.
	outDir2 := filepath.Join(t.TempDir(), "out2")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir2, "-render-all", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -render-all: %v", err)
	}
	got2 := journeyFileNames(t, filepath.Join(outDir2, "journeys"))
	if len(got2) != 2 {
		t.Fatalf("-render-all should render both candidates, got %d: %v", len(got2), got2)
	}
}

// TestCmdAnalyze_DefaultSuiteRendersCronAndSubagent: cron/subagent
// candidates once appeared in
// the index but the default suite never rendered them, so their index row
// linked to a journey-*.md that was never written — real-corpus measurement
// found both categories had double-digit-request candidates (subagent's
// largest was the biggest journey in the whole corpus), so folding them out
// of the default render scope was hiding legitimate work, not noise. Only
// heartbeat stays unrendered by default.
func TestCmdAnalyze_DefaultSuiteRendersCronAndSubagent(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	// [cron:job-id ...] and "... [Subagent Context] ..." are the literal
	// title markers classifyJourney (internal/journey/candidates.go) checks
	// for — resolveTaskProfile() defaults to OpenClawAware, whose
	// bracket-stripping regexes don't touch either marker.
	cronU1 := journeyMsg("user", "[cron:daily-report 0 9 * * *] generate the report")
	cronR1 := journeyRec(at(0), []any{sys, cronU1}, journeySSE("start"))
	cronR2 := journeyRec(at(1), []any{sys, cronU1, journeyMsg("assistant", "done")}, journeySSE("done"))

	subU1 := journeyMsg("user", "[Subagent Context] investigate the failing test")
	subR1 := journeyRec(at(20), []any{sys, subU1}, journeySSE("start"))
	subR2 := journeyRec(at(21), []any{sys, subU1, journeyMsg("assistant", "done")}, journeySSE("done"))

	hbU1 := journeyMsg("user", "[OpenClaw heartbeat poll] check in")
	hbR1 := journeyRec(at(30), []any{sys, hbU1}, journeySSE("ack"))
	hbR2 := journeyRec(at(31), []any{sys, hbU1, journeyMsg("assistant", "ack")}, journeySSE("ack2"))

	path := writeJourneyJSONL(t, []audit.Record{cronR1, cronR2, subR1, subR2, hbR1, hbR2})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze (default suite): %v", err)
	}

	idx := journey.LoadJourneyIndex(filepath.Join(outDir, "journeys", "index.json"))
	if len(idx.Journeys) != 3 {
		t.Fatalf("index should list all three candidates, got %d: %+v", len(idx.Journeys), idx.Journeys)
	}

	got := journeyFileNames(t, filepath.Join(outDir, "journeys"))
	if len(got) != 2 {
		t.Fatalf("default suite should render the cron and subagent candidates (2), got %d: %v", len(got), got)
	}

	var cronRendered, subagentRendered, heartbeatRendered bool
	for _, row := range idx.Journeys {
		switch row.Category {
		case journey.CategoryCron:
			cronRendered = row.Rendered != ""
		case journey.CategorySubagent:
			subagentRendered = row.Rendered != ""
		case journey.CategoryHeartbeat:
			heartbeatRendered = row.Rendered != ""
		}
	}
	if !cronRendered || !subagentRendered {
		t.Errorf("cron and subagent rows should both have a Rendered link, got cron=%v subagent=%v", cronRendered, subagentRendered)
	}
	if heartbeatRendered {
		t.Error("heartbeat row should stay unrendered by default")
	}
}

// detailFileCount counts the entries under {dir}/details, treating a
// missing directory as 0 — the P13.5 guard needs to distinguish "not
// created at all" from "created but empty" from "populated", and both the
// first two count as compliant with "batch mode does not materialize".
func detailFileCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "requests", "details"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// TestCmdAnalyze_DefaultSuiteJourneyHasNoDeadDetailLinks: the default
// suite (no selector, no -render-all) must NOT materialize detail pages,
// and — since it doesn't — its journey reports must render each Step's
// "→ detail" pointer as an inline `file:line` coordinate, never a Markdown
// link that would 404. This discipline regressed repeatedly before this
// test existed because nothing asserted it. -render-all opts back into
// full materialization + real links.
func TestCmdAnalyze_DefaultSuiteJourneyHasNoDeadDetailLinks(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "12-B guard fixture opening instruction")
	r1 := journeyRec(at(0), []any{sys, u1}, journeySSE("开工"))
	r2 := journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "done")}, journeySSE("完成"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze (default suite): %v", err)
	}
	if n := detailFileCount(t, outDir); n != 0 {
		t.Errorf("default suite materialized %d detail file(s), want 0 (batch mode should only reference, not generate — P13.1)", n)
	}
	got := journeyFileNames(t, filepath.Join(outDir, "journeys"))
	if len(got) != 1 {
		t.Fatalf("default suite should still render the 1 task candidate's journey report, got %d: %v", len(got), got)
	}
	md, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", got[0]))
	if err != nil {
		t.Fatal(err)
	}
	s := string(md)
	if strings.Contains(s, "](../details/") {
		t.Errorf("default-suite journey report has a dead ../details/ link (B10), want inline coordinates:\n%s", s)
	}
	if strings.Contains(s, "](../evidence/") {
		t.Errorf("default-suite journey report has a dead ../evidence/ link (B10):\n%s", s)
	}
	if !strings.Contains(s, "`audit.jsonl:1`") {
		t.Errorf("default-suite spine should reference Step 1 by its `file:line` coordinate:\n%s", s)
	}

	// -render-all is an explicit "materialize everything" ask — opts back
	// into writing the detail files AND real Markdown links.
	outDir2 := filepath.Join(t.TempDir(), "out2")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir2, "-render-all", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -render-all: %v", err)
	}
	if n := detailFileCount(t, outDir2); n == 0 {
		t.Error("-render-all should materialize detail files, got 0")
	}
	got2 := journeyFileNames(t, filepath.Join(outDir2, "journeys"))
	md2, err := os.ReadFile(filepath.Join(outDir2, "journeys", "details", got2[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md2), "](../details/") {
		t.Errorf("-render-all journey report should carry real ../details/ links:\n%s", md2)
	}
}

// TestCmdAnalyze_JourneySelectorMaterializesOnlyItsOwnDetails covers P13.5
// (independent review's F-03): a targeted -journey render must materialize
// exactly the named journey's own Step details, not every candidate's —
// distinguishing "the default suite's implicit batch skips details"
// (P13.1) from "a -journey selector always materializes, even multi-match"
// (this file's §0.4 judgment call) requires both directions to hold, not
// just the batch-skips-it half P13.5's other test already covers.
func TestCmdAnalyze_JourneySelectorMaterializesOnlyItsOwnDetails(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	aU1 := journeyMsg("user", "candidate A for the F-03 targeted-materialization test")
	aR1 := journeyRec(at(0), []any{sys, aU1}, journeySSE("开工 A"))
	aR2 := journeyRec(at(1), []any{sys, aU1, journeyMsg("assistant", "done A")}, journeySSE("完成 A"))

	bU1 := journeyMsg("user", "candidate B for the F-03 targeted-materialization test")
	bR1 := journeyRec(at(10), []any{sys, bU1}, journeySSE("开工 B"))
	bR2 := journeyRec(at(11), []any{sys, bU1, journeyMsg("assistant", "done B")}, journeySSE("完成 B"))

	path := writeJourneyJSONL(t, []audit.Record{aR1, aR2, bR1, bR2})
	outDir := filepath.Join(t.TempDir(), "out")

	su, err := setupJourneyRun([]string{path}, outDir, false, "", nil, false, i18n.EN)
	if err != nil {
		t.Fatalf("setupJourneyRun: %v", err)
	}
	if len(su.chains) != 2 {
		t.Fatalf("want 2 independent candidates, got %d", len(su.chains))
	}
	idA := journey.ID(su.chains[0])

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-journey", idA, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -journey %s: %v", idA, err)
	}
	if n := detailFileCount(t, outDir); n != 2 {
		t.Errorf("named journey has 2 records — want exactly 2 detail files (only its own), got %d", n)
	}
}

// TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists covers
// a P13.1 regression an independent review of this phase's ActionPlan
// caught before it shipped: ensureJourneyFile's "journey-<id>.md already
// exists, nothing to do" early return predates P13.1, back when a
// journey's .md existing WAS proof its Step details existed too (every
// write always materialized both). P13.1 broke that assumption — the
// default suite can leave a journey-<id>.md on disk with none of its
// details/. Running the default suite first, then -compare naming one of
// those same candidates, must still materialize that journey's details —
// not silently leave every "→ detail" link 404 forever because the .md
// already existed.
func TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	aU1 := journeyMsg("user", "candidate A for the F-01 regression test")
	aR1 := journeyRec(at(0), []any{sys, aU1}, journeySSE("开工 A"))
	aR2 := journeyRec(at(1), []any{sys, aU1, journeyMsg("assistant", "done A")}, journeySSE("完成 A"))

	bU1 := journeyMsg("user", "candidate B for the F-01 regression test")
	bR1 := journeyRec(at(10), []any{sys, bU1}, journeySSE("开工 B"))
	bR2 := journeyRec(at(11), []any{sys, bU1, journeyMsg("assistant", "done B")}, journeySSE("完成 B"))

	path := writeJourneyJSONL(t, []audit.Record{aR1, aR2, bR1, bR2})
	outDir := filepath.Join(t.TempDir(), "out")

	// Step 1: the default suite renders both (task-classified) candidates'
	// journey-*.md but — per P13.1 — materializes neither one's details/.
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze (default suite): %v", err)
	}
	if n := detailFileCount(t, outDir); n != 0 {
		t.Fatalf("precondition failed: default suite already materialized %d detail file(s)", n)
	}

	su, err := setupJourneyRun([]string{path}, outDir, false, "", nil, false, i18n.EN)
	if err != nil {
		t.Fatalf("setupJourneyRun: %v", err)
	}
	if len(su.chains) != 2 {
		t.Fatalf("want 2 independent candidates, got %d", len(su.chains))
	}
	idA, idB := journey.ID(su.chains[0]), journey.ID(su.chains[1])

	// Step 1b: those pre-existing j-*.md carry inline coordinates, not
	// links (default suite, 12-B).
	preGot := journeyFileNames(t, filepath.Join(outDir, "journeys"))
	preMD, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", preGot[0]))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(preMD), "](../details/") {
		t.Fatalf("precondition failed: default-suite journey report already has ../details/ links")
	}

	// Step 2: -compare names two candidates whose j-*.md ALREADY
	// exists from step 1. Their details/ must still get materialized now,
	// AND their j-*.md must be re-rendered with real links (not left
	// stale on coordinates) — ensureJourneyFile no longer early-returns.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-compare", idA + "," + idB, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -compare: %v", err)
	}
	if n := detailFileCount(t, outDir); n == 0 {
		t.Error("-compare left both named journeys' details/ empty even though their j-*.md pre-existed (F-01 regression)")
	}
	postMD, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", preGot[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(postMD), "](../details/") {
		t.Errorf("-compare should have re-rendered the pre-existing journey report with real ../details/ links, got:\n%s", postMD)
	}
}

// TestCmdAnalyze_JourneySelectorRunsStoryHalfOnly covers P9.1: a zoom
// selector routes into that one story-side view and does NOT also run the
// macro report half — "选中其一时行为等价于今天 vmr story 的对应模式", not
// the default suite with an extra filter.
func TestCmdAnalyze_JourneySelectorRunsStoryHalfOnly(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "single candidate for -journey selector test")
	r1 := journeyRec(at(0), []any{sys, u1}, journeySSE("开工"))
	r2 := journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "done")}, journeySSE("完成"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, "-journey", "*", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -journey '*': %v", err)
	}
	if got := journeyFileNames(t, filepath.Join(outDir, "journeys")); len(got) != 1 {
		t.Fatalf("want exactly 1 rendered journey, got %d: %v", len(got), got)
	}
	if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); !os.IsNotExist(err) {
		t.Errorf("-journey should not also run the report half; vmr-report.md stat = %v", err)
	}
}

// TestCmdAnalyze_BenchmarkSelectorRunsStoryHalfOnly mirrors the -journey case
// for -benchmark.
func TestCmdAnalyze_BenchmarkSelectorRunsStoryHalfOnly(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	u1 := journeyMsg("user", "single candidate for -benchmark selector test")
	r1 := journeyRec(at(0), []any{sys, u1}, journeySSE("开工"))
	r2 := journeyRec(at(1), []any{sys, u1, journeyMsg("assistant", "done")}, journeySSE("完成"))
	path := writeJourneyJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, "-benchmark", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -benchmark: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "journeys", "benchmarks.md")); err != nil {
		t.Errorf("expected benchmarks.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); !os.IsNotExist(err) {
		t.Errorf("-benchmark should not also run the report half; vmr-report.md stat = %v", err)
	}
}

// TestCmdAnalyze_SelectorsAreMutuallyExclusive and
// TestCmdAnalyze_RenderAllRejectsSelector cover the CLI-level validation
// cmdAnalyze adds on top of what cmdStory already enforced (P9.1's design:
// fail loud on a combination that looks like a mistake, rather than
// silently letting one selector win, the way pre-P9 cmdStory did for
// -journey + -render-all together).
func TestCmdAnalyze_SelectorsAreMutuallyExclusive(t *testing.T) {
	path := writeJourneyJSONL(t, []audit.Record{journeyRec(time.Now(), []any{journeyMsg("user", "x")}, journeySSE("y"))})
	outDir := filepath.Join(t.TempDir(), "out")
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-journey", "*", "-benchmark", path})
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want a mutually-exclusive error, got: %v", err)
	}
}

func TestCmdAnalyze_RenderAllRejectsSelector(t *testing.T) {
	path := writeJourneyJSONL(t, []audit.Record{journeyRec(time.Now(), []any{journeyMsg("user", "x")}, journeySSE("y"))})
	outDir := filepath.Join(t.TempDir(), "out")
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-journey", "*", "-render-all", path})
	})
	if err == nil || !strings.Contains(err.Error(), "-render-all") {
		t.Fatalf("want a -render-all/selector conflict error, got: %v", err)
	}
}

// TestCmdAnalyze_CompareSelectorRunsStoryHalfOnly mirrors the -journey case
// for -compare: two independent candidates, diffed, with the macro report
// half never invoked.
func TestCmdAnalyze_CompareSelectorRunsStoryHalfOnly(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	aU1 := journeyMsg("user", "candidate A for -compare selector test")
	aR1 := journeyRec(at(0), []any{sys, aU1}, journeySSE("开工 A"))
	aR2 := journeyRec(at(1), []any{sys, aU1, journeyMsg("assistant", "done A")}, journeySSE("完成 A"))

	bU1 := journeyMsg("user", "candidate B for -compare selector test")
	bR1 := journeyRec(at(10), []any{sys, bU1}, journeySSE("开工 B"))
	bR2 := journeyRec(at(11), []any{sys, bU1, journeyMsg("assistant", "done B")}, journeySSE("完成 B"))

	path := writeJourneyJSONL(t, []audit.Record{aR1, aR2, bR1, bR2})
	outDir := filepath.Join(t.TempDir(), "out")

	// Discover the two candidates' real content-addressed ids the same way
	// setupJourneyRun (and therefore cmdAnalyze itself) computes them, rather
	// than guessing/hardcoding a hash — same package, so this internal
	// helper is directly callable from the test.
	su, err := setupJourneyRun([]string{path}, outDir, false, "", nil, false, i18n.EN)
	if err != nil {
		t.Fatalf("setupJourneyRun: %v", err)
	}
	if len(su.chains) != 2 {
		t.Fatalf("want 2 independent candidates, got %d", len(su.chains))
	}
	idA, idB := journey.ID(su.chains[0]), journey.ID(su.chains[1])

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-compare", idA + "," + idB, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -compare: %v", err)
	}
	compareFiles, err := filepath.Glob(filepath.Join(outDir, "compares", "compare-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(compareFiles) == 0 {
		t.Error("expected a compare-*.md to be written")
	}
	if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); !os.IsNotExist(err) {
		t.Errorf("-compare should not also run the report half; vmr-report.md stat = %v", err)
	}
}

// TestCmdAnalyze_CompareWildcard covers F-2: -compare resolves each side
// through journeyPatternMatches (same as -journey), so a shell glob that
// pins a journey by its content-hash suffix — something a plain prefix can
// never express — works on both sides.
func TestCmdAnalyze_CompareWildcard(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 22, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	aU1 := journeyMsg("user", "candidate A for -compare wildcard test")
	aR1 := journeyRec(at(0), []any{sys, aU1}, journeySSE("开工 A"))
	aR2 := journeyRec(at(1), []any{sys, aU1, journeyMsg("assistant", "done A")}, journeySSE("完成 A"))

	bU1 := journeyMsg("user", "candidate B for -compare wildcard test")
	bR1 := journeyRec(at(10), []any{sys, bU1}, journeySSE("开工 B"))
	bR2 := journeyRec(at(11), []any{sys, bU1, journeyMsg("assistant", "done B")}, journeySSE("完成 B"))

	path := writeJourneyJSONL(t, []audit.Record{aR1, aR2, bR1, bR2})
	outDir := filepath.Join(t.TempDir(), "out")

	su, err := setupJourneyRun([]string{path}, outDir, false, "", nil, false, i18n.EN)
	if err != nil {
		t.Fatalf("setupJourneyRun: %v", err)
	}
	if len(su.chains) != 2 {
		t.Fatalf("want 2 independent candidates, got %d", len(su.chains))
	}
	idA, idB := journey.ID(su.chains[0]), journey.ID(su.chains[1])
	patA, patB := "*"+idA[len(idA)-8:], "*"+idB[len(idB)-8:]

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-compare", patA + "," + patB, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -compare (wildcard): %v", err)
	}
	compareFiles, err := filepath.Glob(filepath.Join(outDir, "compares", "compare-*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(compareFiles) == 0 {
		t.Error("expected a compare-*.md from a wildcard -compare selector")
	}
}

// TestCmdAnalyze_WritesSkeletonPages pins §5.4's implementation discipline:
// every analyze invocation idempotently refreshes the six skeleton
// dashboard pages into the output root — the default suite, the zoom modes,
// everything. The pages carry zero business data (data is fetched
// client-side from the JSON slices), so a skeleton at the root is always
// safe to overwrite and always in sync with the running binary.
func TestCmdAnalyze_WritesSkeletonPages(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 9, 1, 10, min, 0, 0, time.UTC) }
	path := writeJourneyJSONL(t, []audit.Record{
		journeyRec(at(0), []any{journeyMsg("system", "sys"), journeyMsg("user", "skeleton probe")}, journeySSE("开工")),
	})

	assertSkeletons := func(t *testing.T, outDir string) {
		t.Helper()
		names, err := dashboard.AssetNames()
		if err != nil {
			t.Fatalf("dashboard.AssetNames: %v", err)
		}
		if len(names) == 0 {
			t.Fatal("dashboard.AssetNames returned no pages")
		}
		for _, name := range names {
			fi, err := os.Stat(filepath.Join(outDir, name))
			if err != nil {
				t.Errorf("skeleton page %s missing at output root: %v", name, err)
				continue
			}
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("skeleton page %s mode = %v, want 0600", name, fi.Mode().Perm())
			}
		}
	}

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze (default suite): %v", err)
	}
	assertSkeletons(t, outDir)

	// A second run over the same output root must refresh in place —
	// idempotent, never an error.
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze rerun: %v", err)
	}
	assertSkeletons(t, outDir)

	// A zoom mode (no macro report half at all) refreshes too — "every
	// analyze invocation" means every mode, not just the default suite.
	zoomDir := filepath.Join(t.TempDir(), "zoom")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", zoomDir, "-macro-only", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -macro-only: %v", err)
	}
	assertSkeletons(t, zoomDir)
}

// TestCmdAnalyze_LLMAddrRejectedInDefaultSuite covers the batch-mode
// -llm-addr rejection cmdStory already enforced for -render-all/-corpus —
// cmdAnalyze's default suite is the equivalent batch shape and must reject
// it the same way (one LLM call per journey makes no sense against a
// suite-wide render).
func TestCmdAnalyze_LLMAddrRejectedInDefaultSuite(t *testing.T) {
	path := writeJourneyJSONL(t, []audit.Record{journeyRec(time.Now(), []any{journeyMsg("user", "x")}, journeySSE("y"))})
	outDir := filepath.Join(t.TempDir(), "out")
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", path})
	})
	if err == nil || !strings.Contains(err.Error(), "-llm-addr") {
		t.Fatalf("want an -llm-addr/batch-mode rejection error, got: %v", err)
	}
}

// TestCmdAnalyze_EmptyLLMAddrNotRejectedInDefaultSuite: an explicit
// `-llm-addr ""` is how you suppress a report.yaml llm_addr for one run —
// it resolves to the empty string, fires no LLM call, and so must NOT trip
// the batch-mode rejection above (which gates on the resolved value being
// non-empty, not merely on the flag having been typed).
func TestCmdAnalyze_EmptyLLMAddrNotRejectedInDefaultSuite(t *testing.T) {
	path := writeJourneyJSONL(t, []audit.Record{journeyRec(time.Now(), []any{journeyMsg("user", "x")}, journeySSE("y"))})
	outDir := filepath.Join(t.TempDir(), "out")
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-llm-addr", "", path})
	})
	if err != nil && strings.Contains(err.Error(), "-llm-addr") {
		t.Fatalf("-llm-addr \"\" must not be rejected as a batch-mode LLM call, got: %v", err)
	}
}

// TestCmdAnalyze_LLMKeyExcludesSelfTrafficFromBothHalves: the
// "self-traffic input asymmetry" (cmd_journey.go could
// take an explicit -llm-key override, cmd_report.go had no such flag and
// only ever read report.yaml's llm_key) is closed by the unified flag set
// — an -llm-key passed to `vmr analyze` (not present in report.yaml at
// all) must exclude the same self-traffic candidate from BOTH the story
// half's candidate list and the report half's totals, since cmdAnalyze
// resolves llmKey once and feeds it to both setupJourneyRun and runReport's
// excludeClientTags.
func TestCmdAnalyze_LLMKeyExcludesSelfTrafficFromBothHalves(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")

	selfKey := "sk-analysis-key-not-in-report-yaml"
	selfTag := audit.KeyTag(selfKey)

	selfU1 := journeyMsg("user", "self-analysis interpretation call")
	selfR1 := journeyRec(at(0), []any{sys, selfU1}, journeySSE("interpreting"))
	selfR1.ClientKeyTag = selfTag
	selfR2 := journeyRec(at(1), []any{sys, selfU1, journeyMsg("assistant", "done")}, journeySSE("done interpreting"))
	selfR2.ClientKeyTag = selfTag

	workU1 := journeyMsg("user", "real workload task")
	workR1 := journeyRec(at(10), []any{sys, workU1}, journeySSE("working"))
	workR2 := journeyRec(at(11), []any{sys, workU1, journeyMsg("assistant", "done")}, journeySSE("done working"))

	path := writeJourneyJSONL(t, []audit.Record{selfR1, selfR2, workR1, workR2})
	outDir := filepath.Join(t.TempDir(), "out")

	// -llm-key only on the command line — report.yaml doesn't exist in
	// this temp dir at all, so this proves the flag itself (not a
	// report.yaml fallback) reaches both halves.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-llm-key", selfKey, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -llm-key: %v", err)
	}

	idx := journey.LoadJourneyIndex(filepath.Join(outDir, "journeys", "index.json"))
	if len(idx.Journeys) != 1 {
		t.Fatalf("story half: want 1 candidate (self-traffic excluded), got %d: %+v", len(idx.Journeys), idx.Journeys)
	}

	repData, err := os.ReadFile(filepath.Join(outDir, "macro", "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sum report.SummarySlice
	if err := json.Unmarshal(repData, &sum); err != nil {
		t.Fatal(err)
	}
	if sum.Meta == nil || sum.Meta.SelfTrafficExcluded != 2 {
		t.Errorf("report half: meta.self_traffic_excluded = %v, want 2 (the self-analysis pair)", sum.Meta)
	}
}
