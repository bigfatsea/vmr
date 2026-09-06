// Ver 2026-09-03, by pi-agent

package journey

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// metricTestJourney builds a small journey whose repeated read_file calls
// trip a Finding — enough for the behavior-indicator block to render.
func metricTestJourney(t *testing.T) (*Journey, Metrics, []Finding) {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "audit the auth module")

	call := func(id string) any {
		return map[string]any{"id": id, "function": map[string]any{"name": "read_file", "arguments": `{"path":"auth.go"}`}}
	}
	aCall := func(id string) map[string]any {
		return map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"auth.go"}`}},
		}}
	}
	res := func(id string) map[string]any {
		return map[string]any{"role": "tool", "tool_call_id": id, "content": "package auth"}
	}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{call("c1")}))
	r2 := mkRec(at(1), "", []any{sys, u1, aCall("c1"), res("c1")}, sseToolCalls([]any{call("c2")}))
	r3 := mkRec(at(2), "", []any{sys, u1, aCall("c1"), res("c1"), aCall("c2"), res("c2")}, sseText("done reviewing"))

	l := onlyLineage(t, writeJSONL(t, []audit.Record{r1, r2, r3}))
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)
	f := ComputeFindings(j, i18n.EN)
	return j, m, f
}

// TestJourneyIndicatorSet_MatchMD pins the shared-list contract: the single-
// journey behavior-indicator block must show exactly the unified
// journeyMetrics slice (compare_metrics.go) — if the renderer ever
// hand-rolls its list again, the extracted set diverges and this test goes
// red. Order may differ — only the set is pinned.
func TestJourneyIndicatorSet_MatchMD(t *testing.T) {
	j, m, f := metricTestJourney(t)
	lang := i18n.EN

	sum := NewJourneySummary(j, m, f, nil, nil)
	md := RenderMarkdownFromSummary(&sum, lang, false, true)

	wantCodes := make(map[string]bool, len(journeyMetrics))
	for _, jm := range journeyMetrics {
		wantCodes[string(jm.Code)] = true
	}
	if len(wantCodes) == 0 {
		t.Fatal("journeyMetrics is empty — the unification regressed to nothing")
	}

	// md rows are "| Label | value |" lines inside the indicators table;
	// extract the label set and compare it against the codes' i18n labels
	// (order-free).
	mdRows := regexp.MustCompile(`(?m)^\| ([^|]+) \| [^|]+ \|$`).FindAllStringSubmatch(md, -1)
	if len(mdRows) == 0 {
		t.Fatalf("no indicator table rows found in Markdown output:\n%s", md)
	}

	labelOf := func(code string) string {
		return i18n.MetricLabel(lang, code)
	}
	mdLabels := map[string]bool{}
	for _, r := range mdRows {
		label := strings.TrimSpace(r[1])
		if label == "Metric" { // the table header row, not a data row
			continue
		}
		mdLabels[label] = true
	}

	for code := range wantCodes {
		label := labelOf(code)
		if !mdLabels[label] {
			t.Errorf("Markdown output missing indicator %q (code %s)", label, code)
		}
	}
	// And nothing extra beyond the unified set.
	if len(mdLabels) != len(wantCodes) {
		t.Errorf("Markdown shows %d indicator labels, want exactly %d (the unified set): %v", len(mdLabels), len(wantCodes), mdLabels)
	}
}
