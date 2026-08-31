// Ver 2026-08-28, by Sonnet 5

package story

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func cmpFixture(t *testing.T) Comparison {
	t.Helper()
	atA := time.Date(2026, 7, 28, 0, 5, 44, 0, time.UTC)
	recA := mkExtrasRec(atA, "system prompt A SECRET-A", "research SECRET-TASK", "openai-completions:opencode:deepseek-v4-pro",
		1000, 200, 800, "tool_calls", []map[string]any{writeToolCall("exec", "", "")})
	jA, err := Build(onlyLineage(t, writeJSONL(t, []audit.Record{recA})), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build A: %v", err)
	}
	atB := time.Date(2026, 7, 28, 0, 5, 49, 0, time.UTC)
	recB := mkExtrasRec(atB, "system prompt B SECRET-B", "research SECRET-TASK", "openai-completions:minimax:MiniMax-M3",
		2000, 300, 360, "stop", []map[string]any{writeToolCall("write", "report.md", "# Report SECRET-DELIV\nfindings here")})
	jB, err := Build(onlyLineage(t, writeJSONL(t, []audit.Record{recB})), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build B: %v", err)
	}
	sa, sb := Summarize(jA, i18n.EN), Summarize(jB, i18n.EN)
	cmp := Compare(sa, sb, i18n.EN)
	ex := ComputeComparisonExtras(jA, jB, sa.Metrics, sb.Metrics, nil, "")
	cmp.Extras = &ex
	return cmp
}

func TestRenderComparisonHTML_Structure(t *testing.T) {
	cmp := cmpFixture(t)
	out := RenderComparisonHTML(cmp, CompareLLMResult{}, i18n.EN, false)

	for _, want := range []string{
		"<!doctype html>", `id="sides"`, `id="diff"`,
		`<table class="abtbl"`, "deepseek-v4-pro", "MiniMax-M3",
		"findings here",        // un-redacted deliverable excerpt present
		"research SECRET-TASK", // divergence headline shows the task title un-redacted
		`<a href="journey-`,    // side card links out to the per-journey report
	} {
		if !strings.Contains(out, want) {
			t.Errorf("comparison dashboard missing %q", want)
		}
	}
	if strings.Contains(out, `id="llm"`) {
		t.Error("LLM section rendered with no LLM result")
	}
	if regexp.MustCompile(`(?:src|href)\s*=\s*"https?://`).FindString(out) != "" {
		t.Error("comparison dashboard references an external resource")
	}
}

func TestRenderComparisonHTML_NoExtras(t *testing.T) {
	cmp := cmpFixture(t)
	cmp.Extras = nil
	out := RenderComparisonHTML(cmp, CompareLLMResult{}, i18n.EN, false)
	if !strings.Contains(out, `<table class="abtbl"`) {
		t.Error("metric table should still render without Extras")
	}
}

// TestRenderComparisonHTML_CostFootnoteAndDeliverableSkip covers F-3 (a
// one-sided-pricing footnote under the cost fact) and F-6 (no deliverable
// fact row at all when neither side produced one).
func TestRenderComparisonHTML_CostFootnoteAndDeliverableSkip(t *testing.T) {
	cmp := cmpFixture(t)
	cmp.Extras.Cost = CostPair{
		A: CostFact{Resolved: true, Total: fp(3.5)},
		B: CostFact{Resolved: false},
	}
	cmp.Extras.Deliverable = DeliverableFact{} // both sides Found=false

	out := RenderComparisonHTML(cmp, CompareLLMResult{}, i18n.EN, false)
	if !strings.Contains(out, "A $3.50 · B —") {
		t.Errorf("cost fact missing the one-priced-one-dash pair:\n%s", out)
	}
	if !strings.Contains(out, "pricing not on file") {
		t.Errorf("one-sided pricing must draw the footnote (F-3):\n%s", out)
	}
	if strings.Contains(out, "Final deliverable") {
		t.Errorf("neither side has a deliverable — the fact row must be skipped (F-6):\n%s", out)
	}
}

func TestRenderComparisonHTML_LLMBlock(t *testing.T) {
	cmp := cmpFixture(t)
	llm := CompareLLMResult{
		Model:          "agent",
		Overall:        InterpretResult{Text: "## Root cause\n- A used `exec`\n- **B** wrote the file", Cached: true},
		Divergence:     InterpretResult{Text: "the two paths split at the write step"},
		DivergenceUsed: cmp.Extras.Divergence.Found,
	}
	out := RenderComparisonHTML(cmp, llm, i18n.EN, false)
	if !strings.Contains(out, `id="llm"`) || !strings.Contains(out, "<strong>B</strong>") || !strings.Contains(out, "<code>exec</code>") {
		t.Errorf("LLM block / md rendering missing:\n%s", out)
	}
	if !strings.Contains(out, "(cached)") {
		t.Error("cached LLM result should be marked")
	}
}

func TestRenderComparisonHTML_RedactLeaksNothing(t *testing.T) {
	cmp := cmpFixture(t)
	llm := CompareLLMResult{Model: "agent", Overall: InterpretResult{Text: "SECRET-LLM analysis"}}
	out := RenderComparisonHTML(cmp, llm, i18n.EN, true)

	for _, secret := range []string{"SECRET-A", "SECRET-B", "SECRET-DELIV", "SECRET-LLM", "SECRET-TASK", "findings here"} {
		if strings.Contains(out, secret) {
			t.Errorf("redacted comparison dashboard leaked %q", secret)
		}
	}
	if strings.Contains(out, `id="llm"`) {
		t.Error("redact mode must drop the LLM section entirely")
	}
	// The sibling journey-<id>.md is un-redacted (0600, not for sharing) —
	// redact mode keeps the filename as text but must not link to it.
	if strings.Contains(out, `<a href="journey-`) {
		t.Error("redacted comparison dashboard links to the un-redacted per-journey report")
	}
	// metric numbers + structure survive
	for _, want := range []string{`<table class="abtbl"`, "‹text:", "deepseek-v4-pro"} {
		if !strings.Contains(out, want) {
			t.Errorf("redacted comparison dashboard missing %q", want)
		}
	}
}

func TestRenderComparisonHTML_ZHChrome(t *testing.T) {
	cmp := cmpFixture(t)
	out := RenderComparisonHTML(cmp, CompareLLMResult{}, i18n.ZH, false)
	if !strings.Contains(out, `<html lang="zh">`) || !strings.Contains(out, "两侧") || !strings.Contains(out, "分岔与差异") {
		t.Error("ZH chrome not applied")
	}
}
