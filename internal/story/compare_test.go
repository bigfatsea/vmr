// Ver 2026-07-30 12:00, by Sonnet 5

package story

import (
	"strings"
	"testing"
	"time"
)

func TestCompareBasicDiff(t *testing.T) {
	a := JourneySummary{
		ID: "j-a", Title: "session A",
		From: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 7, 1, 1, 0, 0, 0, time.UTC),
		Metrics: Metrics{
			ModelMS: 1000, AgentExecMS: 500, HumanIdleMS: 200, NetWorkingMS: 1500,
			ToolCallCount: 10, DuplicateActionRate: 0.1, ErrorRecoveryCount: 1,
			PlanExecRatio: 0.2, ContextUtilization: 0.5,
			CompactionCount: 1, CompactionLossTokens: 1000,
			ToolCallDist: []ToolCallStat{{Name: "read", Count: 8}, {Name: "write", Count: 2}},
		},
	}
	b := JourneySummary{
		ID: "j-b", Title: "session B",
		From: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 7, 2, 1, 0, 0, 0, time.UTC),
		Metrics: Metrics{
			ModelMS: 5000, AgentExecMS: 500, HumanIdleMS: 200, NetWorkingMS: 5500,
			ToolCallCount: 12, DuplicateActionRate: 0.1, ErrorRecoveryCount: 1,
			PlanExecRatio: 0.2, ContextUtilization: 0.5,
			CompactionCount: 1, CompactionLossTokens: 1000,
			ToolCallDist: []ToolCallStat{{Name: "read", Count: 8}, {Name: "exec", Count: 4}},
		},
	}

	cmp := Compare(a, b)
	if cmp.A.ID != "j-a" || cmp.B.ID != "j-b" {
		t.Fatalf("journey refs = %q/%q, want j-a/j-b", cmp.A.ID, cmp.B.ID)
	}
	if len(cmp.Rows) != 12 {
		t.Fatalf("rows = %d, want 12 (one per Metrics field)", len(cmp.Rows))
	}

	var modelRow *MetricDiff
	for i := range cmp.Rows {
		if cmp.Rows[i].Label == "模型时间" {
			modelRow = &cmp.Rows[i]
		}
	}
	if modelRow == nil {
		t.Fatal("missing 模型时间 row")
	}
	if modelRow.A != 1000 || modelRow.B != 5000 {
		t.Errorf("模型时间 A/B = %v/%v, want 1000/5000", modelRow.A, modelRow.B)
	}
	// (5000-1000)/max(1000,5000) = 0.8
	if modelRow.DeltaRel != 0.8 {
		t.Errorf("模型时间 DeltaRel = %v, want 0.8", modelRow.DeltaRel)
	}
	if !modelRow.Notable {
		t.Error("模型时间 should be notable: 4000ms absolute delta clears the 2s floor, 80% clears the 30% threshold")
	}

	// Rows that are identical between A and B must never be notable. 净工作时长
	// (NetWorkingMS = ModelMS+AgentExecMS) legitimately co-varies with 模型时间
	// here, so it's excluded from this identical-rows check too.
	for _, r := range cmp.Rows {
		if r.Label == "模型时间" || r.Label == "净工作时长" {
			continue
		}
		if r.Notable {
			t.Errorf("row %q unexpectedly notable (A=%v B=%v)", r.Label, r.A, r.B)
		}
	}

	if len(cmp.Tools) != 3 {
		t.Fatalf("tools = %d, want 3 (read, write, exec)", len(cmp.Tools))
	}
	if cmp.Tools[0].Name != "read" || cmp.Tools[0].ACalls != 8 || cmp.Tools[0].BCalls != 8 {
		t.Errorf("tools[0] = %+v, want read 8/8", cmp.Tools[0])
	}
	if cmp.Tools[1].Name != "write" || cmp.Tools[1].BCalls != 0 {
		t.Errorf("tools[1] = %+v, want write with BCalls=0 (B never called it)", cmp.Tools[1])
	}
	if cmp.Tools[2].Name != "exec" || cmp.Tools[2].ACalls != 0 {
		t.Errorf("tools[2] = %+v, want exec with ACalls=0 (A never called it)", cmp.Tools[2])
	}
}

// TestCompareSmallDeltaNotNotable covers the notableFloor guard: a Journey
// with 0 tool calls vs 1 is a mathematically infinite relative change, but
// shouldn't be flagged — one call either way is noise, not a signal.
func TestCompareSmallDeltaNotNotable(t *testing.T) {
	a := JourneySummary{Metrics: Metrics{ToolCallCount: 0}}
	b := JourneySummary{Metrics: Metrics{ToolCallCount: 1}}
	cmp := Compare(a, b)
	for _, r := range cmp.Rows {
		if r.Label == "工具调用次数" && r.Notable {
			t.Error("0 vs 1 tool call should not be notable (below the count floor)")
		}
	}
}

// TestCompareZeroZeroNotNotable covers the other DeltaRel edge case: both
// sides exactly 0 must not divide by zero or register as notable.
func TestCompareZeroZeroNotNotable(t *testing.T) {
	a := JourneySummary{Metrics: Metrics{CompactionCount: 0, CompactionLossTokens: 0}}
	b := JourneySummary{Metrics: Metrics{CompactionCount: 0, CompactionLossTokens: 0}}
	cmp := Compare(a, b)
	for _, r := range cmp.Rows {
		if r.DeltaRel != 0 {
			t.Errorf("row %q: DeltaRel = %v for two zero values, want 0", r.Label, r.DeltaRel)
		}
		if r.Notable {
			t.Errorf("row %q: two zero values should never be notable", r.Label)
		}
	}
}

func TestRenderComparisonMarkdown(t *testing.T) {
	a := JourneySummary{ID: "j-a", Title: "跑A股研究", From: time.Now(), To: time.Now(),
		Metrics: Metrics{ModelMS: 1000, ToolCallDist: []ToolCallStat{{Name: "read", Count: 5}}}}
	b := JourneySummary{ID: "j-b", Title: "跑B股研究", From: time.Now(), To: time.Now(),
		Metrics: Metrics{ModelMS: 9000, ToolCallDist: []ToolCallStat{{Name: "read", Count: 1}}}}
	md := RenderComparisonMarkdown(Compare(a, b))

	for _, want := range []string{"j-a", "j-b", "跑A股研究", "跑B股研究", "模型时间", "⚠️", "read"} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered comparison missing %q:\n%s", want, md)
		}
	}
}
