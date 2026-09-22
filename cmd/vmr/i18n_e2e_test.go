// Ver 2026-09-21 23:30, by Sonnet 5

// End-to-end tests for the multi-language report/journey design (see
// docs/VirtualModelRouter_Design_v4_Analytics.md's output-language section):
// drives cmdAnalyze exactly as the CLI does (flag parsing included), not
// internal/report's or internal/journey's package-level API directly — the
// thing being tested is the whole -lang/report.yaml wiring through cmd/vmr,
// which no single package's own tests can see end to end.
package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/journey"
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
		"attempts": []any{map[string]any{"endpoint": "openai-completions:p:agent", "dur_ms": 100, "response": map[string]any{"status": 200}}},
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

// reportEfficiencyJSON is the slice of macro/summary.json this test needs —
// deliberately narrow (not report.Report2) so this test breaks only when
// the actual fields it checks change shape, not on every unrelated schema
// addition.
type reportEfficiencyJSON struct {
	Efficiency []struct {
		Code    string            `json:"code"`
		Finding string            `json:"finding"`
		Params  map[string]string `json:"params"`
	} `json:"efficiency"`
}

func readReportJSON(t *testing.T, outDir string) reportEfficiencyJSON {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outDir, "macro", "summary.json"))
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
// change: with no -lang and no report.yaml anywhere analyze looks,
// vmr-report.md renders in English.
func TestE2E_ReportDefaultsToEnglish(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdAnalyze([]string{"-macro-only", "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -macro-only: %v", err)
	}
	md := readReportMD(t, outDir)
	if !strings.Contains(md, "# VMR Usage Report") || !strings.Contains(md, "## §0 Summary") {
		t.Errorf("default output should be English:\n%s", md)
	}
	if strings.Contains(md, "用量报告") {
		t.Errorf("default output should not contain Chinese section chrome:\n%s", md)
	}
}

// TestE2E_ReportLangFlagZh_EfficiencyStaysEnglish covers -lang zh end to
// end: vmr-report.md switches to Chinese but macro/summary.json's
// efficiency[].finding stays the English baseline (R1, codebase-weight-
// analysis doc §7 — language is a render-time concern, never baked into
// the data product; the persisted JSON must be reproducible in either
// language without re-aggregating). Finding.Params carries the raw values
// a consumer would need to build the Chinese sentence itself. This test
// used to pin the opposite (JSON follows -lang) — reversed back, not
// deleted, so the fact that this was a deliberate policy correction (not
// an accidental regression) stays visible in history.
func TestE2E_ReportLangFlagZh_EfficiencyStaysEnglish(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdAnalyze([]string{"-macro-only", "-lang", "zh", "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -macro-only: %v", err)
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
			if f.Finding != "Tool schema waste" {
				t.Errorf("efficiency[].finding for tool_schema_waste = %q, want the English baseline %q even under -lang zh", f.Finding, "Tool schema waste")
			}
			if f.Params["shape"] == "" {
				t.Errorf("efficiency[].params should carry the raw shape value, got %+v", f.Params)
			}
		}
	}
	if !found {
		t.Fatal("fixture should trigger the tool_schema_waste finding (code missing from macro/summary.json entirely)")
	}
}

// TestE2E_ReportConfigFileZh covers report.yaml (via -report-config, so the
// test doesn't have to chdir): language: zh with no -lang flag at all must
// still switch vmr-report.md's language — the whole point of report.yaml
// being auto-loaded rather than requiring -lang on every invocation.
// macro/summary.json's efficiency[] stays the English baseline regardless
// (R1) — same split TestE2E_ReportLangFlagZh_EfficiencyStaysEnglish pins
// for -lang, checked here too so a report.yaml-only language choice isn't
// assumed to behave the same from the -lang-flag test alone.
func TestE2E_ReportConfigFileZh(t *testing.T) {
	path := e2eReportFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	rcPath := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(rcPath, []byte("language: zh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdAnalyze([]string{"-macro-only", "-report-config", rcPath, "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -macro-only: %v", err)
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
			if f.Finding != "Tool schema waste" {
				t.Errorf("report.yaml language: zh should NOT localize macro/summary.json's efficiency[].finding (R1), got %q", f.Finding)
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
	if err := cmdAnalyze([]string{"-macro-only", "-report-config", rcPath, "-lang", "en", "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -macro-only: %v", err)
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
	if err := cmdAnalyze([]string{"-macro-only", "-lang", "fr", "-o", outDir, path}); err == nil {
		t.Error("cmdAnalyze -macro-only -lang fr should return an error, not silently default")
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
		if err := cmdAnalyze([]string{"-macro-only", "-report-config", rcPath, "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -macro-only: %v", err)
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

// TestE2E_ReportExplicitConfigFileMissingIsError covers the case an explicit
// -report-config path doesn't exist: unlike the auto-detected ./report.yaml
// (silently absent is normal there), a path the user typed themselves is a
// hard error — R89 removed the old warn-and-degrade, which silently disabled
// every configured setting on a typo'd path.
func TestE2E_ReportExplicitConfigFileMissingIsError(t *testing.T) {
	path := e2eReportFixture(t)
	rcPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	origFatal := reportConfigFatal
	defer func() { reportConfigFatal = origFatal }()
	reportConfigFatal = func(_ io.Writer, err error) { panic(err) }
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Errorf("an explicit -report-config pointing at a missing file must fail the run, got a successful one")
				return
			}
			err, ok := r.(error)
			if !ok || !strings.Contains(err.Error(), rcPath) {
				t.Errorf("fatal error should name the missing config path, got %v", r)
			}
		}()
		_ = cmdAnalyze([]string{"-macro-only", "-report-config", rcPath, "-o", filepath.Join(t.TempDir(), "out"), path})
	}()
}

// --- vmr analyze -journey ---

// e2eStoryFixture writes two independent two-turn journeys (distinct
// opening instructions, so ListCandidates offers both — and ≥2 manifests
// per chain, the bar ListCandidates applies before a lineage counts as a
// candidate at all) for -render-all/-compare end-to-end coverage.
func e2eStoryFixture(t *testing.T) string {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := journeyMsg("system", "sys")
	uA := journeyMsg("user", "research topic A")
	uB := journeyMsg("user", "research topic B")
	recA1 := journeyRec(at(0), []any{sys, uA}, journeySSE("ok"))
	recA2 := journeyRec(at(1), []any{sys, uA, journeyMsg("assistant", "done")}, journeySSE("done A"))
	recB1 := journeyRec(at(10), []any{sys, uB}, journeySSE("ok"))
	recB2 := journeyRec(at(11), []any{sys, uB, journeyMsg("assistant", "done")}, journeySSE("done B"))
	return writeJourneyJSONL(t, []audit.Record{recA1, recA2, recB1, recB2})
}

// TestE2E_JourneyRenderAllDefaultsToEnglish covers vmr analyze -render-all with
// no -lang/report.yaml: journey-*.md must render in English.
func TestE2E_JourneyRenderAllDefaultsToEnglish(t *testing.T) {
	path := e2eStoryFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdAnalyze([]string{"-render-all", "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -render-all: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "j-") && strings.HasSuffix(e.Name(), ".md") {
			found = true
			data, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), " 轮 ") {
				t.Errorf("%s should not contain Chinese chrome by default:\n%s", e.Name(), data)
			}
		}
	}
	if !found {
		t.Fatal("expected at least one j-*.md to be rendered")
	}
}

// TestE2E_JourneyRenderAllLangZh covers -lang zh flowing through cmdAnalyze into
// journey.BuildAll/RenderMarkdown.
func TestE2E_JourneyRenderAllLangZh(t *testing.T) {
	path := e2eStoryFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")
	if err := cmdAnalyze([]string{"-render-all", "-lang", "zh", "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -render-all: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(outDir, "journeys", "details"))
	if err != nil {
		t.Fatal(err)
	}
	sawTurnsWord := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "j-") && strings.HasSuffix(e.Name(), ".md") {
			data, err := os.ReadFile(filepath.Join(outDir, "journeys", "details", e.Name()))
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

// TestE2E_JourneyCompareLangZh_JSONLabelStaysEnglish covers -compare's
// JSON/Markdown split (R1): compare-*.md's metric table switches to
// Chinese under -lang zh, but compare-*.json's rows[].label (MetricDiff.Label) stays the English
// baseline Compare persisted — Markdown reconstructs the actual language
// at render time from rows[].metric via i18n.MetricLabel
// (render_compare.go) rather than reading Label back. This test used to
// pin the opposite (JSON followed -lang) — reversed back, not deleted, so
// the fact that this was a deliberate policy correction (not an accidental
// regression) stays visible in history.
func TestE2E_JourneyCompareLangZh_JSONLabelStaysEnglish(t *testing.T) {
	path := e2eStoryFixture(t)
	outDir := filepath.Join(t.TempDir(), "out")

	// Discover both candidate ids the way a user would: list first.
	listing := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-list-only", "-o", outDir, path}); err != nil {
			t.Fatalf("cmdAnalyze -list-only: %v", err)
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

	if err := cmdAnalyze([]string{"-compare", ids[0] + "," + ids[1], "-lang", "zh", "-o", outDir, path}); err != nil {
		t.Fatalf("cmdAnalyze -compare: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(outDir, "compares"))
	if err != nil {
		t.Fatal(err)
	}
	var mdPath, jsonPath string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "compare-") {
			if strings.HasSuffix(e.Name(), ".md") {
				mdPath = filepath.Join(outDir, "compares", e.Name())
			}
			if strings.HasSuffix(e.Name(), ".json") {
				jsonPath = filepath.Join(outDir, "compares", e.Name())
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
			if r.Label != "Model Time" {
				t.Errorf("compare-*.json rows[].label for model_ms = %q, want the English baseline %q (R1) even under -lang zh", r.Label, "Model Time")
			}
		}
	}
	if !foundModelMS {
		t.Fatal("compare-*.json should carry a model_ms row")
	}
}

// TestE2E_LangZh_AllThreeJSONOutputsAreLangInvariant is the cross-check
// TestE2E_ReportLangFlagZh_EfficiencyStaysEnglish and
// TestE2E_JourneyCompareLangZh_JSONLabelStaysEnglish each individually can't
// provide: one test function checking all three JSON outputs
// (macro/summary.json, j-<id>.json, compare-*.json) under the SAME -lang zh
// run, so a future regression in any one of them surfaces here instead of
// only in an isolated per-output test (P8, json_lang_policy_plan_sonnet-5.md
// §3.5 — "each package's own tests passing individually is exactly how the
// inconsistency this policy fixes went unnoticed for as long as it did").
// All three are now language-invariant (R1) — this test only checks that
// they parse and carry the expected rows; TestSlicesAreLangInvariant (report) and
// TestJourneySummaryIsLangInvariant (journey) are the byte-level machine
// judges for each half, so this one stays a light cross-output sanity
// check rather than duplicating either. Each output uses its own existing
// fixture rather than one shared audit log — the point is same-run
// consistency, not that the three outputs describe the same data.
func TestE2E_LangZh_AllThreeJSONOutputsAreLangInvariant(t *testing.T) {
	// macro/summary.json: efficiency[].finding — English baseline regardless
	// of -lang (R1).
	reportPath := e2eReportFixture(t)
	reportOut := filepath.Join(t.TempDir(), "out")
	if err := cmdAnalyze([]string{"-macro-only", "-lang", "zh", "-o", reportOut, reportPath}); err != nil {
		t.Fatalf("cmdAnalyze -macro-only: %v", err)
	}
	rep := readReportJSON(t, reportOut)
	foundReport := false
	for _, f := range rep.Efficiency {
		if f.Code == "tool_schema_waste" {
			foundReport = true
			if f.Finding != "Tool schema waste" {
				t.Errorf("macro/summary.json efficiency[].finding = %q, want the English baseline %q (R1)", f.Finding, "Tool schema waste")
			}
		}
	}
	if !foundReport {
		t.Fatal("macro/summary.json: expected tool_schema_waste finding")
	}

	// j-<id>.json: language-invariant now too (R1) — this block only
	// confirms the JSON parses with the right id; finding-text invariance
	// itself is TestJourneySummaryIsLangInvariant's job, not this one's.
	journeyPath := e2eStoryFixture(t)
	journeyOut := filepath.Join(t.TempDir(), "out")
	listing := captureStdout(t, func() {
		if err := cmdAnalyze([]string{"-list-only", "-o", journeyOut, journeyPath}); err != nil {
			t.Fatalf("cmdAnalyze -list-only: %v", err)
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
	if err := cmdAnalyze([]string{"-journey", ids[0], "-lang", "zh", "-o", journeyOut, journeyPath}); err != nil {
		t.Fatalf("cmdAnalyze -journey: %v", err)
	}
	journeyData, err := os.ReadFile(filepath.Join(journeyOut, "journeys", "details", strings.TrimSuffix(journey.JourneyReportFile(ids[0]), ".md")+".json"))
	if err != nil {
		t.Fatalf("journey json not written: %v", err)
	}
	var journey struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(journeyData, &journey); err != nil || journey.ID != ids[0] {
		t.Fatalf("journey-%s.json did not parse as expected: err=%v, id=%q", ids[0], err, journey.ID)
	}

	// compare-*.json: rows[].label is the English baseline now too (R1).
	if err := cmdAnalyze([]string{"-compare", ids[0] + "," + ids[1], "-lang", "zh", "-o", journeyOut, journeyPath}); err != nil {
		t.Fatalf("cmdAnalyze -compare: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(journeyOut, "compares"))
	if err != nil {
		t.Fatal(err)
	}
	var comparePath string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "compare-") && strings.HasSuffix(e.Name(), ".json") {
			comparePath = filepath.Join(journeyOut, "compares", e.Name())
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
			if r.Label != "Model Time" {
				t.Errorf("compare-*.json rows[].label for model_ms = %q, want the English baseline %q (R1)", r.Label, "Model Time")
			}
		}
	}
	if !foundCompare {
		t.Fatal("compare-*.json: expected a model_ms row")
	}
}
