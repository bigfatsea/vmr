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

// htmlTestJourney builds a small journey whose bodies carry sentinels
// (SECRET-LEAK in the instruction, TOKEN=abc in a tool result) and whose
// two identical read_file calls trip the exact-repeat Finding — enough to
// exercise every dashboard section and the redaction path.
func htmlTestJourney(t *testing.T) (*Journey, Metrics, []Finding) {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "audit the auth module for SECRET-LEAK issues")

	call := func(id string) any {
		return map[string]any{"id": id, "function": map[string]any{"name": "read_file", "arguments": `{"path":"auth.go"}`}}
	}
	aCall := func(id string) map[string]any {
		return map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"auth.go"}`}},
		}}
	}
	res := func(id string) map[string]any {
		return map[string]any{"role": "tool", "tool_call_id": id, "content": "package auth // TOKEN=abc"}
	}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{call("c1")}))
	r2 := mkRec(at(1), "", []any{sys, u1, aCall("c1"), res("c1")}, sseToolCalls([]any{call("c2")}))
	r3 := mkRec(at(2), "", []any{sys, u1, aCall("c1"), res("c1"), aCall("c2"), res("c2")}, sseToolCalls([]any{call("c3")}))
	r4 := mkRec(at(3), "", []any{sys, u1, aCall("c1"), res("c1"), aCall("c2"), res("c2"), aCall("c3"), res("c3")}, sseText("done reviewing"))

	l := onlyLineage(t, writeJSONL(t, []audit.Record{r1, r2, r3, r4}))
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)
	f := ComputeFindings(j, i18n.EN)
	return j, m, f
}

func TestRenderHTML_DashboardStructure(t *testing.T) {
	j, m, f := htmlTestJourney(t)
	out := RenderHTML(j, m, f, CostFact{}, i18n.EN, false)

	for _, want := range []string{
		"<!doctype html>",
		"<title>Journey j-",
		`id="structure"`,
		`id="metrics"`,
		`id="findings"`,
		`id="step-1"`,
		`id="task-1"`,       // task anchor, observed by the scroll-spy
		`.task[id]`,         // scroll-spy watches task containers, not just sections/steps
		`class="stat"`,      // metrics grid
		"<polyline",         // context sparkline
		"read_file",         // tool chip
		`href="../details/`, // per-step detail link (non-redact)
		"audit the auth module",
		"<style>",
		"IntersectionObserver",
		"FLIGHT RECORDER",   // recorder bar
		"PROBABLE CAUSE",    // verdict panel
		`class="verdict v-`, // severity class
		"CRITICAL",          // fixture trips exact_repeat_tool_call
		`class="damage"`,    // step/time/token tally
		"tokens processed",  // damage line copy
		// exact_repeat_tool_call is critical but not a structural PONR signal:
		// the strip must NOT claim the run "stayed on its rails".
		`class="ponr ponr-diffuse"`,
		"degraded gradually",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard HTML missing %q", want)
		}
	}
	if len(f) == 0 {
		t.Fatal("fixture should trip the exact-repeat Finding")
	}
	if strings.Contains(out, "stayed on its rails") {
		t.Error("critical verdict must not also claim the run stayed on its rails")
	}
	if !strings.Contains(out, string(f[0].Code)) {
		t.Errorf("Findings section did not render finding code %q", f[0].Code)
	}
	if m := regexp.MustCompile(`(?:src|href)\s*=\s*"https?://`).FindString(out); m != "" {
		t.Errorf("dashboard references an external resource: %q", m)
	}
	if strings.Contains(out, `<link rel="stylesheet"`) || strings.Contains(out, "<script src=") {
		t.Error("dashboard loads an external stylesheet or script")
	}
}

func TestRenderHTML_CostAndPointOfNoReturn(t *testing.T) {
	j := journeyOf(
		&Step{Seq: 1},
		&Step{Seq: 2, Compaction: &CompactionInfo{TokensBefore: 64000, TokensAfter: 8000,
			SwallowedEntities: []string{"internal/auth/policy.go", "docs/spec.md"}}},
		&Step{Seq: 3},
	)
	m := Metrics{NetWorkingMS: 252000, ModelUsage: []ModelUsageStat{{TokensIn: 1_100_000, TokensOut: 90_000}}}
	cost := CostFact{Currency: "USD", TotalUSD: 4.8, Resolved: true, PricedSteps: 3, TotalSteps: 3}

	out := RenderHTML(j, m, nil, cost, i18n.EN, false)
	for _, want := range []string{
		"POINT OF NO RETURN", "Step 2", "64.0K", "8.0K", "2 named constraints dropped",
		"≈ $4.80", "NOMINAL", // no findings -> clean verdict, but a fatal compaction still shows the PONR strip
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cost/PONR dashboard missing %q", want)
		}
	}

	// redact keeps the PONR strip (counts only) and the $ figure (a number,
	// not a body), drops nothing structural.
	red := RenderHTML(j, m, nil, cost, i18n.EN, true)
	for _, want := range []string{"POINT OF NO RETURN", "constraints dropped", "≈ $4.80"} {
		if !strings.Contains(red, want) {
			t.Errorf("redacted cost/PONR dashboard missing %q", want)
		}
	}
	// entity names themselves must not leak even though the count does.
	if strings.Contains(red, "policy.go") {
		t.Error("redacted PONR strip leaked a dropped entity name")
	}
}

// The compaction-reconstruction layer sometimes can't measure one side of a
// boundary (a 0). The PONR strip must not then render "79.8K → 0 tokens".
func TestRenderHTML_PointOfNoReturn_UnmeasuredCompactionSize(t *testing.T) {
	j := journeyOf(
		&Step{Seq: 1, Compaction: &CompactionInfo{TokensBefore: 79800, TokensAfter: 0,
			SwallowedEntities: []string{"a.go", "b.go", "c.go"}}},
	)
	out := RenderHTML(j, Metrics{}, nil, CostFact{}, i18n.EN, false)
	if !strings.Contains(out, "POINT OF NO RETURN") ||
		!strings.Contains(out, "dropped 3 named constraints") {
		t.Errorf("want the constraint-count phrasing, got:\n%s", out)
	}
	if strings.Contains(out, "→ 0 tokens") {
		t.Error(`PONR strip rendered an unmeasured "→ 0 tokens"`)
	}
}

func TestRenderHTML_DashboardRedactLeaksNothing(t *testing.T) {
	j, m, f := htmlTestJourney(t)
	out := RenderHTML(j, m, f, CostFact{}, i18n.EN, true)

	for _, secret := range []string{"SECRET-LEAK", "TOKEN=abc", "package auth", "done reviewing", "read_file\":", "auth.go"} {
		if strings.Contains(out, secret) {
			t.Errorf("redacted dashboard leaked content: %q", secret)
		}
	}
	// structure + metrics + verdict survive; detail links + finding prose do not.
	for _, want := range []string{"read_file", "‹text:", `id="step-1"`, `class="stat"`, "<polyline",
		"PROBABLE CAUSE", "CRITICAL", `class="damage"`, "text redacted"} {
		if !strings.Contains(out, want) {
			t.Errorf("redacted dashboard missing structural element %q", want)
		}
	}
	if strings.Contains(out, `href="../details/`) {
		t.Error("redact mode still linked to ../details/ (0600, not shared)")
	}
	if strings.Contains(out, "<pre>") {
		t.Error("redact mode still emitted a raw <pre> content block")
	}
	// finding codes stay, finding narrative text does not.
	if !strings.Contains(out, string(f[0].Code)) {
		t.Error("redact mode should still show finding codes + step anchors")
	}
	if f[0].Finding != "" && strings.Contains(out, f[0].Finding) {
		t.Errorf("redact mode leaked finding narrative text: %q", f[0].Finding)
	}
}

func TestRenderHTML_DashboardZHChrome(t *testing.T) {
	j, m, f := htmlTestJourney(t)
	out := RenderHTML(j, m, f, CostFact{}, i18n.ZH, false)
	if !strings.Contains(out, `<html lang="zh">`) || !strings.Contains(out, "结构") || !strings.Contains(out, "指标") {
		t.Errorf("ZH chrome not applied:\n%s", out[:min(len(out), 600)])
	}
}

func TestRenderHTML_NoStepsDoesNotPanic(t *testing.T) {
	j := &Journey{ID: "j-empty", Title: "x", From: time.Now(), To: time.Now()}
	_ = RenderHTML(j, Metrics{}, nil, CostFact{}, i18n.EN, false)
}
