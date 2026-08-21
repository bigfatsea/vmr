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
	"vmr/internal/i18n"
	"vmr/internal/report"
	"vmr/internal/story"
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
	// (analyze's default suite renders every category=task candidate,
	// P9.2 — this fixture's journey has no cron/heartbeat/subagent title
	// marker, so it classifies as task and gets rendered by default), not
	// just listed it — otherwise the requests-index -> journey edge
	// (P6.2c) has nothing to link to.
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
		t.Error("no journey-*.md rendered — analyze's default suite should render category=task candidates")
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

// journeyFileNames lists the rendered journey-*.md basenames in dir — shared
// by the P9.2 scope tests below.
func journeyFileNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "journey-") && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestCmdAnalyze_DefaultSuiteScopeIsTaskOnly covers P9.2: the default suite
// (no selector, no -render-all) only materializes category=task candidates
// — a heartbeat-titled candidate stays in the index but doesn't get a
// journey-*.md until -render-all (or a targeted -journey) asks for it.
func TestCmdAnalyze_DefaultSuiteScopeIsTaskOnly(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")

	taskU1 := storyMsg("user", "调研一下 A 股新股打新收益")
	taskR1 := storyRec(at(0), []any{sys, taskU1}, storySSE("开工"))
	taskR2 := storyRec(at(1), []any{sys, taskU1, storyMsg("assistant", "done")}, storySSE("完成"))

	// [OpenClaw heartbeat poll] is the literal title-marker classifyJourney
	// checks for (internal/story/candidates.go) — resolveTaskProfile()
	// defaults to OpenClawAware, and P7.2's bracket-stripping regexes only
	// touch timestamp/message_id markers, not this one, so it survives into
	// the derived title unchanged.
	hbU1 := storyMsg("user", "[OpenClaw heartbeat poll] check in")
	hbR1 := storyRec(at(10), []any{sys, hbU1}, storySSE("ack"))
	hbR2 := storyRec(at(11), []any{sys, hbU1, storyMsg("assistant", "ack")}, storySSE("ack2"))

	path := writeStoryJSONL(t, []audit.Record{taskR1, taskR2, hbR1, hbR2})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, path}) }); err != nil {
		t.Fatalf("cmdAnalyze (default suite): %v", err)
	}

	idx := story.LoadStoryIndex(filepath.Join(outDir, "stories", "vmr-stories.json"))
	if len(idx.Journeys) != 2 {
		t.Fatalf("index should list both candidates regardless of render scope, got %d: %+v", len(idx.Journeys), idx.Journeys)
	}
	var sawTask, sawHeartbeat bool
	for _, row := range idx.Journeys {
		switch row.Category {
		case story.CategoryTask:
			sawTask = true
		case story.CategoryHeartbeat:
			sawHeartbeat = true
		}
	}
	if !sawTask || !sawHeartbeat {
		t.Fatalf("expected one task and one heartbeat candidate in the index, got: %+v", idx.Journeys)
	}

	got := journeyFileNames(t, filepath.Join(outDir, "stories"))
	if len(got) != 1 {
		t.Fatalf("default suite should render exactly the 1 task candidate, got %d: %v", len(got), got)
	}

	// -render-all opts back into full materialization — both candidates.
	outDir2 := filepath.Join(t.TempDir(), "out2")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir2, "-render-all", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -render-all: %v", err)
	}
	got2 := journeyFileNames(t, filepath.Join(outDir2, "stories"))
	if len(got2) != 2 {
		t.Fatalf("-render-all should render both candidates, got %d: %v", len(got2), got2)
	}
}

// TestCmdAnalyze_JourneySelectorRunsStoryHalfOnly covers P9.1: a zoom
// selector routes into that one story-side view and does NOT also run the
// macro report half — "选中其一时行为等价于今天 vmr story 的对应模式", not
// the default suite with an extra filter.
func TestCmdAnalyze_JourneySelectorRunsStoryHalfOnly(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "single candidate for -journey selector test")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("开工"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("完成"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, "-journey", "*", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -journey '*': %v", err)
	}
	if got := journeyFileNames(t, filepath.Join(outDir, "stories")); len(got) != 1 {
		t.Fatalf("want exactly 1 rendered journey, got %d: %v", len(got), got)
	}
	if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); !os.IsNotExist(err) {
		t.Errorf("-journey should not also run the report half; vmr-report.md stat = %v", err)
	}
}

// TestCmdAnalyze_CorpusSelectorRunsStoryHalfOnly mirrors the -journey case
// for -corpus.
func TestCmdAnalyze_CorpusSelectorRunsStoryHalfOnly(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "single candidate for -corpus selector test")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("开工"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("完成"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})

	outDir := filepath.Join(t.TempDir(), "out")
	if err := captureStdoutErr(t, func() error { return cmdAnalyze([]string{"-o", outDir, "-corpus", path}) }); err != nil {
		t.Fatalf("cmdAnalyze -corpus: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "stories", "vmr-story-corpus.md")); err != nil {
		t.Errorf("expected vmr-story-corpus.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "vmr-report.md")); !os.IsNotExist(err) {
		t.Errorf("-corpus should not also run the report half; vmr-report.md stat = %v", err)
	}
}

// TestCmdAnalyze_SelectorsAreMutuallyExclusive and
// TestCmdAnalyze_RenderAllRejectsSelector cover the CLI-level validation
// cmdAnalyze adds on top of what cmdStory already enforced (P9.1's design:
// fail loud on a combination that looks like a mistake, rather than
// silently letting one selector win, the way pre-P9 cmdStory did for
// -journey + -render-all together).
func TestCmdAnalyze_SelectorsAreMutuallyExclusive(t *testing.T) {
	path := writeStoryJSONL(t, []audit.Record{storyRec(time.Now(), []any{storyMsg("user", "x")}, storySSE("y"))})
	outDir := filepath.Join(t.TempDir(), "out")
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-journey", "*", "-corpus", path})
	})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want a mutually-exclusive error, got: %v", err)
	}
}

func TestCmdAnalyze_RenderAllRejectsSelector(t *testing.T) {
	path := writeStoryJSONL(t, []audit.Record{storyRec(time.Now(), []any{storyMsg("user", "x")}, storySSE("y"))})
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
	sys := storyMsg("system", "sys")

	aU1 := storyMsg("user", "candidate A for -compare selector test")
	aR1 := storyRec(at(0), []any{sys, aU1}, storySSE("开工 A"))
	aR2 := storyRec(at(1), []any{sys, aU1, storyMsg("assistant", "done A")}, storySSE("完成 A"))

	bU1 := storyMsg("user", "candidate B for -compare selector test")
	bR1 := storyRec(at(10), []any{sys, bU1}, storySSE("开工 B"))
	bR2 := storyRec(at(11), []any{sys, bU1, storyMsg("assistant", "done B")}, storySSE("完成 B"))

	path := writeStoryJSONL(t, []audit.Record{aR1, aR2, bR1, bR2})
	outDir := filepath.Join(t.TempDir(), "out")

	// Discover the two candidates' real content-addressed ids the same way
	// setupStoryRun (and therefore cmdAnalyze itself) computes them, rather
	// than guessing/hardcoding a hash — same package, so this internal
	// helper is directly callable from the test.
	su, err := setupStoryRun([]string{path}, outDir, false, "", nil, false, i18n.EN)
	if err != nil {
		t.Fatalf("setupStoryRun: %v", err)
	}
	if len(su.chains) != 2 {
		t.Fatalf("want 2 independent candidates, got %d", len(su.chains))
	}
	idA, idB := story.ID(su.chains[0]), story.ID(su.chains[1])

	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-compare", idA + "," + idB, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -compare: %v", err)
	}
	compareFiles, err := filepath.Glob(filepath.Join(outDir, "stories", "compare-*.md"))
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

// TestCmdAnalyze_LLMAddrRejectedInDefaultSuite covers the batch-mode
// -llm-addr rejection cmdStory already enforced for -render-all/-corpus —
// cmdAnalyze's default suite is the equivalent batch shape and must reject
// it the same way (one LLM call per journey makes no sense against a
// suite-wide render).
func TestCmdAnalyze_LLMAddrRejectedInDefaultSuite(t *testing.T) {
	path := writeStoryJSONL(t, []audit.Record{storyRec(time.Now(), []any{storyMsg("user", "x")}, storySSE("y"))})
	outDir := filepath.Join(t.TempDir(), "out")
	err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-llm-addr", "127.0.0.1:1", "-llm-model", "agent", path})
	})
	if err == nil || !strings.Contains(err.Error(), "-llm-addr") {
		t.Fatalf("want an -llm-addr/batch-mode rejection error, got: %v", err)
	}
}

// TestCmdAnalyze_LLMKeyExcludesSelfTrafficFromBothHalves covers P9.5:
// KNOWN_ISSUES §1.34's "self-traffic input asymmetry" (cmd_story.go could
// take an explicit -llm-key override, cmd_report.go had no such flag and
// only ever read report.yaml's llm_key) is closed by the unified flag set
// — an -llm-key passed to `vmr analyze` (not present in report.yaml at
// all) must exclude the same self-traffic candidate from BOTH the story
// half's candidate list and the report half's totals, since cmdAnalyze
// resolves llmKey once and feeds it to both setupStoryRun and runReport's
// excludeClientTags.
func TestCmdAnalyze_LLMKeyExcludesSelfTrafficFromBothHalves(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")

	selfKey := "sk-analysis-key-not-in-report-yaml"
	selfTag := audit.KeyTag(selfKey)

	selfU1 := storyMsg("user", "self-analysis interpretation call")
	selfR1 := storyRec(at(0), []any{sys, selfU1}, storySSE("interpreting"))
	selfR1.ClientKeyTag = selfTag
	selfR2 := storyRec(at(1), []any{sys, selfU1, storyMsg("assistant", "done")}, storySSE("done interpreting"))
	selfR2.ClientKeyTag = selfTag

	workU1 := storyMsg("user", "real workload task")
	workR1 := storyRec(at(10), []any{sys, workU1}, storySSE("working"))
	workR2 := storyRec(at(11), []any{sys, workU1, storyMsg("assistant", "done")}, storySSE("done working"))

	path := writeStoryJSONL(t, []audit.Record{selfR1, selfR2, workR1, workR2})
	outDir := filepath.Join(t.TempDir(), "out")

	// -llm-key only on the command line — report.yaml doesn't exist in
	// this temp dir at all, so this proves the flag itself (not a
	// report.yaml fallback) reaches both halves.
	if err := captureStdoutErr(t, func() error {
		return cmdAnalyze([]string{"-o", outDir, "-llm-key", selfKey, path})
	}); err != nil {
		t.Fatalf("cmdAnalyze -llm-key: %v", err)
	}

	idx := story.LoadStoryIndex(filepath.Join(outDir, "stories", "vmr-stories.json"))
	if len(idx.Journeys) != 1 {
		t.Fatalf("story half: want 1 candidate (self-traffic excluded), got %d: %+v", len(idx.Journeys), idx.Journeys)
	}

	repData, err := os.ReadFile(filepath.Join(outDir, "vmr-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep report.Report2
	if err := json.Unmarshal(repData, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Meta.SelfTrafficExcluded != 2 {
		t.Errorf("report half: meta.self_traffic_excluded = %d, want 2 (the self-analysis pair)", rep.Meta.SelfTrafficExcluded)
	}
}

// TestCmdReportCmdStory_PrintDeprecationHint covers P9.3: both standalone
// commands keep producing identical output to before, but now also print a
// one-line stderr migration hint naming `vmr analyze`.
func TestCmdReportCmdStory_PrintDeprecationHint(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 8, 21, 9, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	u1 := storyMsg("user", "deprecation hint fixture")
	r1 := storyRec(at(0), []any{sys, u1}, storySSE("开工"))
	r2 := storyRec(at(1), []any{sys, u1, storyMsg("assistant", "done")}, storySSE("完成"))
	path := writeStoryJSONL(t, []audit.Record{r1, r2})

	t.Run("report", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "out")
		stderr := captureStderr(t, func() {
			if err := cmdReport([]string{"-o", outDir, path}); err != nil {
				t.Fatalf("cmdReport: %v", err)
			}
		})
		if !strings.Contains(stderr, "vmr analyze") {
			t.Errorf("expected a migration hint naming vmr analyze on stderr, got: %q", stderr)
		}
	})

	t.Run("story", func(t *testing.T) {
		outDir := filepath.Join(t.TempDir(), "out")
		stderr := captureStderr(t, func() {
			if err := cmdStory([]string{"-o", outDir, path}); err != nil {
				t.Fatalf("cmdStory: %v", err)
			}
		})
		if !strings.Contains(stderr, "vmr analyze") {
			t.Errorf("expected a migration hint naming vmr analyze on stderr, got: %q", stderr)
		}
	})
}
