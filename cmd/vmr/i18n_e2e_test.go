// Ver 2026-08-02, by Sonnet 5

// End-to-end tests for the multi-language report/story design (see
// docs/VirtualModelRouter_Design_v4_Analytics.md's output-language section):
// drives cmdReport/cmdStory exactly as the CLI does (flag parsing included), not
// internal/report's or internal/story's package-level API directly — the
// thing being tested is the whole -lang/report.yaml wiring through cmd/vmr,
// which no single package's own tests can see end to end.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
)

// e2eReportFixture writes a one-record audit log guaranteed to trigger the
// §7 "tool schema waste" finding (a tool declared but never called, well
// under the 20% utilization threshold) — the cheapest reliable way to
// exercise Report2.Efficiency (and therefore Finding.Code/Finding) via the
// real cmdReport path, without depending on internal/report's own
// unexported test fixtures.
func e2eReportFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "vmr-audit-2026-07-08.jsonl")
	rec := map[string]any{
		"ts": "2026-07-08T10:00:00Z", "dur_ms": 100, "model": "agent", "protocol": "openai-completions", "outcome": "ok",
		"client": map[string]any{
			"request": map[string]any{"body": map[string]any{
				"model": "agent",
				"tools": []any{
					map[string]any{"type": "function", "function": map[string]any{"name": "read", "description": "read a file", "parameters": map[string]any{}}},
					map[string]any{"type": "function", "function": map[string]any{"name": "write", "description": "write a file", "parameters": map[string]any{}}},
				},
				"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			}},
			"response": map[string]any{"status": 200, "body": map[string]any{
				"model": "agent",
				"choices": []any{map[string]any{"finish_reason": "stop",
					"message": map[string]any{"role": "assistant", "content": "hello"}}},
				"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 5},
			}},
		},
		"attempts": []any{map[string]any{"endpoint": "openai:p:agent", "dur_ms": 100, "response": map[string]any{"status": 200}}},
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// reportEfficiencyJSON is the slice of vmr-report.json this test needs —
// deliberately narrow (not report.Report2) so this test breaks only when
// the actual fields it checks change shape, not on every unrelated schema
// addition.
type reportEfficiencyJSON struct {
	Efficiency []struct {
		Code    string `json:"code"`
		Finding string `json:"finding"`
	} `json:"efficiency"`
}

func readReportJSON(t *testing.T, outDir string) reportEfficiencyJSON {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "vmr-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep reportEfficiencyJSON
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

func readReportMD(t *testing.T, outDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "vmr-report.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestE2E_ReportDefaultsToEnglish covers the design's headline behavior
// change: with no -lang and no report.yaml anywhere cmdReport looks,
// vmr-report.md renders in English.
func TestE2E_ReportDefaultsToEnglish(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdReport([]string{"-o", outDir, path}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	md := readReportMD(t, outDir)
	if !strings.Contains(md, "# VMR Usage Report") || !strings.Contains(md, "## §0 Summary") {
		t.Errorf("default output should be English:\n%s", md)
	}
	if strings.Contains(md, "用量报告") {
		t.Errorf("default output should not contain Chinese section chrome:\n%s", md)
	}
}

// TestE2E_ReportLangFlagZh_EfficiencyFollowsLang covers -lang zh end to
// end: vmr-report.md AND vmr-report.json's efficiency[].finding must both
// switch to Chinese (P8, docs/VirtualModelRouter_Design_v4_Analytics.md
// §4.3 — Build always computes the English default internally, but
// cmd_report.go calls report.LocalizeEfficiency(rep, lang) before
// WriteJSON to overwrite it with the report's actual display language).
// This test used to pin the opposite (JSON stays English regardless of
// -lang) — reversed, not deleted, so the fact that this was a deliberate
// policy change (not an accidental regression) stays visible in history.
func TestE2E_ReportLangFlagZh_EfficiencyFollowsLang(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdReport([]string{"-o", outDir, "-lang", "zh", path}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	md := readReportMD(t, outDir)
	if !strings.Contains(md, "VMR 用量报告") || !strings.Contains(md, "§0 摘要") {
		t.Errorf("-lang zh output should be Chinese:\n%s", md)
	}

	rep := readReportJSON(t, outDir)
	found := false
	for _, f := range rep.Efficiency {
		if f.Code == "tool_schema_waste" {
			found = true
			if f.Finding != "工具 schema 浪费" {
				t.Errorf("efficiency[].finding for tool_schema_waste = %q, want the Chinese %q under -lang zh", f.Finding, "工具 schema 浪费")
			}
		}
	}
	if !found {
		t.Fatal("fixture should trigger the tool_schema_waste finding (code missing from vmr-report.json entirely)")
	}
}

// TestE2E_ReportConfigFileZh covers report.yaml (via -report-config, so the
// test doesn't have to chdir): language: zh with no -lang flag at all must
// still switch the output language — the whole point of report.yaml being
// auto-loaded rather than requiring -lang on every invocation. Checks both
// vmr-report.md and vmr-report.json's efficiency[] (P8) — cmd_report.go
// resolves lang exactly once (resolveLanguage) and feeds that same value to
// both LocalizeEfficiency and Markdown, so a report.yaml-only language
// choice reaches the JSON output the same way -lang does; this pins that
// rather than assuming it from the -lang-flag tests alone.
func TestE2E_ReportConfigFileZh(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	rcPath := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(rcPath, []byte("language: zh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdReport([]string{"-o", outDir, "-report-config", rcPath, path}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	md := readReportMD(t, outDir)
	if !strings.Contains(md, "VMR 用量报告") {
		t.Errorf("report.yaml language: zh should switch the output language:\n%s", md)
	}
	rep := readReportJSON(t, outDir)
	found := false
	for _, f := range rep.Efficiency {
		if f.Code == "tool_schema_waste" {
			found = true
			if f.Finding != "工具 schema 浪费" {
				t.Errorf("report.yaml language: zh should also localize vmr-report.json's efficiency[].finding, got %q", f.Finding)
			}
		}
	}
	if !found {
		t.Fatal("fixture should trigger the tool_schema_waste finding")
	}
}

// TestE2E_ReportLangFlagOverridesConfigFile covers the documented priority
// order: -lang wins over report.yaml when both are given.
func TestE2E_ReportLangFlagOverridesConfigFile(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	rcPath := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(rcPath, []byte("language: zh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdReport([]string{"-o", outDir, "-report-config", rcPath, "-lang", "en", path}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	md := readReportMD(t, outDir)
	if !strings.Contains(md, "# VMR Usage Report") {
		t.Errorf("-lang en should override report.yaml's language: zh:\n%s", md)
	}
}

// TestE2E_ReportInvalidLangFlag covers that an explicitly-typed bad -lang
// value is a hard error (not a silent fallback — the user typed it, this
// isn't the best-effort report.yaml path).
func TestE2E_ReportInvalidLangFlag(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdReport([]string{"-o", outDir, "-lang", "fr", path}); err == nil {
		t.Error("cmdReport -lang fr should return an error, not silently default")
	}
}

// TestE2E_ReportConfigFileInvalidLanguageDegradesToEnglish covers
// report.yaml's best-effort contract: an invalid language value in the file
// must warn, not fail the command — a display-language preference is not
// worth blocking a report run over.
func TestE2E_ReportConfigFileInvalidLanguageDegradesToEnglish(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	rcPath := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(rcPath, []byte("language: klingon\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := cmdReport([]string{"-o", outDir, "-report-config", rcPath, path}); err != nil {
			t.Fatalf("cmdReport: %v", err)
		}
	})
	if !strings.Contains(out, "warning") {
		t.Errorf("an invalid report.yaml language should print a warning, got:\n%s", out)
	}
	md := readReportMD(t, outDir)
	if !strings.Contains(md, "# VMR Usage Report") {
		t.Errorf("invalid report.yaml language should degrade to English, not fail:\n%s", md)
	}
}

// TestE2E_ReportExplicitConfigFileMissingWarns covers the case an explicit
// -report-config path doesn't exist: unlike the auto-detected ./report.yaml
// (silently absent is normal there), a path the user typed themselves is
// likely a typo, so it must warn even though the run still degrades to
// English rather than failing outright.
func TestE2E_ReportExplicitConfigFileMissingWarns(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	rcPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	out := captureStdout(t, func() {
		if err := cmdReport([]string{"-o", outDir, "-report-config", rcPath, path}); err != nil {
			t.Fatalf("cmdReport: %v", err)
		}
	})
	if !strings.Contains(out, "warning") || !strings.Contains(out, rcPath) {
		t.Errorf("an explicit -report-config pointing at a missing file should warn by name, got:\n%s", out)
	}
	md := readReportMD(t, outDir)
	if !strings.Contains(md, "# VMR Usage Report") {
		t.Errorf("a missing explicit -report-config should degrade to English, not fail:\n%s", md)
	}
}

// --- vmr story ---

// e2eStoryFixture writes two independent two-turn journeys (distinct
// opening instructions, so ListCandidates offers both — and ≥2 manifests
// per chain, the bar ListCandidates applies before a lineage counts as a
// candidate at all) for -render-all/-compare end-to-end coverage.
func e2eStoryFixture(t *testing.T) string {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := storyMsg("system", "sys")
	uA := storyMsg("user", "research topic A")
	uB := storyMsg("user", "research topic B")
	recA1 := storyRec(at(0), []any{sys, uA}, storySSE("ok"))
	recA2 := storyRec(at(1), []any{sys, uA, storyMsg("assistant", "done")}, storySSE("done A"))
	recB1 := storyRec(at(10), []any{sys, uB}, storySSE("ok"))
	recB2 := storyRec(at(11), []any{sys, uB, storyMsg("assistant", "done")}, storySSE("done B"))
	return writeStoryJSONL(t, []audit.Record{recA1, recA2, recB1, recB2})
}

// TestE2E_StoryRenderAllDefaultsToEnglish covers vmr story -render-all with
// no -lang/report.yaml: journey-*.md must render in English.
func TestE2E_StoryRenderAllDefaultsToEnglish(t *testing.T) {
	path := e2eStoryFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdStory([]string{"-o", outDir, "-render-all", path}); err != nil {
		t.Fatalf("cmdStory -render-all: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(outDir, "stories"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "journey-") && strings.HasSuffix(e.Name(), ".md") {
			found = true
			data, err := os.ReadFile(filepath.Join(outDir, "stories", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), " 轮 ") {
				t.Errorf("%s should not contain Chinese chrome by default:\n%s", e.Name(), data)
			}
		}
	}
	if !found {
		t.Fatal("expected at least one journey-*.md to be rendered")
	}
}

// TestE2E_StoryRenderAllLangZh covers -lang zh flowing through cmdStory into
// story.BuildAll/RenderMarkdown.
func TestE2E_StoryRenderAllLangZh(t *testing.T) {
	path := e2eStoryFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdStory([]string{"-o", outDir, "-render-all", "-lang", "zh", path}); err != nil {
		t.Fatalf("cmdStory -render-all: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(outDir, "stories"))
	if err != nil {
		t.Fatal(err)
	}
	sawTurnsWord := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "journey-") && strings.HasSuffix(e.Name(), ".md") {
			data, err := os.ReadFile(filepath.Join(outDir, "stories", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), " 轮 ") {
				sawTurnsWord = true
			}
		}
	}
	if !sawTurnsWord {
		t.Error("-lang zh should render journey markdown with Chinese chrome (轮)")
	}
}

// TestE2E_StoryCompareLangZh_JSONLabelsFollowLang covers -compare's
// JSON/Markdown consistency (P8, docs/VirtualModelRouter_Design_v4_Analytics.md
// §4.3): compare-*.md and compare-*.json's rows[].label — MetricDiff.Label,
// produced by Compare(a, b, lang) — must both switch to Chinese under
// -lang zh, using the same i18n.MetricLabel lookup. This test used to pin
// the opposite (JSON stayed English regardless of -lang) — reversed, not
// deleted, so the fact that this was a deliberate policy change (not an
// accidental regression) stays visible in history.
func TestE2E_StoryCompareLangZh_JSONLabelsFollowLang(t *testing.T) {
	path := e2eStoryFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	// Discover both candidate ids the way a user would: list first.
	listing := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", outDir, path}); err != nil {
			t.Fatalf("cmdStory list: %v", err)
		}
	})
	var ids []string
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "j-") {
			ids = append(ids, strings.Fields(line)[0])
		}
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 candidate journeys, got %d from listing:\n%s", len(ids), listing)
	}

	if err := cmdStory([]string{"-o", outDir, "-compare", ids[0] + "," + ids[1], "-lang", "zh", path}); err != nil {
		t.Fatalf("cmdStory -compare: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(outDir, "stories"))
	if err != nil {
		t.Fatal(err)
	}
	var mdPath, jsonPath string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "compare-") {
			if strings.HasSuffix(e.Name(), ".md") {
				mdPath = filepath.Join(outDir, "stories", e.Name())
			}
			if strings.HasSuffix(e.Name(), ".json") {
				jsonPath = filepath.Join(outDir, "stories", e.Name())
			}
		}
	}
	if mdPath == "" || jsonPath == "" {
		t.Fatalf("expected compare-*.md and compare-*.json, entries: %v", entries)
	}

	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "模型时间") {
		t.Errorf("-lang zh compare markdown should render the Chinese metric label:\n%s", md)
	}

	var cmp struct {
		Rows []struct {
			Metric string `json:"metric"`
			Label  string `json:"label"`
		} `json:"rows"`
	}
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(jsonData, &cmp); err != nil {
		t.Fatal(err)
	}
	foundModelMS := false
	for _, r := range cmp.Rows {
		if r.Metric == "model_ms" {
			foundModelMS = true
			if r.Label != "模型时间" {
				t.Errorf("compare-*.json rows[].label for model_ms = %q, want the Chinese %q under -lang zh", r.Label, "模型时间")
			}
		}
	}
	if !foundModelMS {
		t.Fatal("compare-*.json should carry a model_ms row")
	}
}

// TestE2E_LangZh_AllThreeJSONOutputsAgree is the cross-check
// TestE2E_ReportLangFlagZh_EfficiencyFollowsLang and
// TestE2E_StoryCompareLangZh_JSONLabelsFollowLang each individually can't
// provide: one test function asserting all three JSON outputs
// (vmr-report.json, journey-<id>.json, compare-*.json) are in Chinese
// under the SAME -lang zh run, so a future regression in any one of them
// surfaces here instead of only in an isolated per-output test (P8,
// json_lang_policy_plan_sonnet-5.md §3.5 — "each package's own tests
// passing individually is exactly how the inconsistency this policy fixes
// went unnoticed for as long as it did"). Each output uses its own
// existing fixture rather than one shared audit log — the point is
// same-run language agreement, not that the three outputs describe the
// same data.
func TestE2E_LangZh_AllThreeJSONOutputsAgree(t *testing.T) {
	// vmr-report.json: efficiency[].finding.
	reportPath := e2eReportFixture(t)
	reportOut := filepath.Join(t.TempDir(), "out")
	if err := cmdReport([]string{"-o", reportOut, "-lang", "zh", reportPath}); err != nil {
		t.Fatalf("cmdReport: %v", err)
	}
	rep := readReportJSON(t, reportOut)
	foundReport := false
	for _, f := range rep.Efficiency {
		if f.Code == "tool_schema_waste" {
			foundReport = true
			if f.Finding != "工具 schema 浪费" {
				t.Errorf("vmr-report.json efficiency[].finding = %q, want %q", f.Finding, "工具 schema 浪费")
			}
		}
	}
	if !foundReport {
		t.Fatal("vmr-report.json: expected tool_schema_waste finding")
	}

	// journey-<id>.json: parses cleanly under -lang zh — its narrative
	// fields already followed lang before P8 (TestE2E_StoryRenderAllLangZh
	// covers the Markdown side); this just confirms it's still true
	// alongside the other two outputs in the same run.
	storyPath := e2eStoryFixture(t)
	storyOut := filepath.Join(t.TempDir(), "out")
	listing := captureStdout(t, func() {
		if err := cmdStory([]string{"-o", storyOut, storyPath}); err != nil {
			t.Fatalf("cmdStory list: %v", err)
		}
	})
	var ids []string
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "j-") {
			ids = append(ids, strings.Fields(line)[0])
		}
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 candidate journeys, got %d from listing:\n%s", len(ids), listing)
	}
	if err := cmdStory([]string{"-o", storyOut, "-journey", ids[0], "-lang", "zh", storyPath}); err != nil {
		t.Fatalf("cmdStory -journey: %v", err)
	}
	journeyData, err := os.ReadFile(filepath.Join(storyOut, "stories", "journey-"+ids[0]+".json"))
	if err != nil {
		t.Fatalf("journey-%s.json not written: %v", ids[0], err)
	}
	var journey struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(journeyData, &journey); err != nil || journey.ID != ids[0] {
		t.Fatalf("journey-%s.json did not parse as expected: err=%v, id=%q", ids[0], err, journey.ID)
	}

	// compare-*.json: rows[].label, same fixture/ids as above.
	if err := cmdStory([]string{"-o", storyOut, "-compare", ids[0] + "," + ids[1], "-lang", "zh", storyPath}); err != nil {
		t.Fatalf("cmdStory -compare: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(storyOut, "stories"))
	if err != nil {
		t.Fatal(err)
	}
	var comparePath string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "compare-") && strings.HasSuffix(e.Name(), ".json") {
			comparePath = filepath.Join(storyOut, "stories", e.Name())
		}
	}
	if comparePath == "" {
		t.Fatalf("expected compare-*.json, entries: %v", entries)
	}
	compareData, err := os.ReadFile(comparePath)
	if err != nil {
		t.Fatal(err)
	}
	var cmp struct {
		Rows []struct {
			Metric string `json:"metric"`
			Label  string `json:"label"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(compareData, &cmp); err != nil {
		t.Fatal(err)
	}
	foundCompare := false
	for _, r := range cmp.Rows {
		if r.Metric == "model_ms" {
			foundCompare = true
			if r.Label != "模型时间" {
				t.Errorf("compare-*.json rows[].label for model_ms = %q, want %q", r.Label, "模型时间")
			}
		}
	}
	if !foundCompare {
		t.Fatal("compare-*.json: expected a model_ms row")
	}
}
