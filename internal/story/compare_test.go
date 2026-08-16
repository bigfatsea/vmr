// Ver 2026-07-30 12:00, by Sonnet 5

package story

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// mkExtrasRec builds a single-step audit.Record with everything
// ComputeComparisonExtras reads that mkRec/mkRecWithUsage don't set:
// Attempts[].Endpoint, a JSON (non-streaming) response carrying usage/cache
// fields, and an optional tool_calls array — so a single record is enough to
// exercise endpoint/cache/deliverable extraction without a multi-step
// fixture.
func mkExtrasRec(ts time.Time, sysText, userText, endpoint string, in, out, cacheRead int64, finish string, toolCalls []map[string]any) audit.Record {
	msgs := []any{msg("system", sysText), msg("user", userText)}
	body := map[string]any{"model": "agent", "stream": false, "messages": msgs}
	respMsg := map[string]any{"role": "assistant", "content": ""}
	if len(toolCalls) > 0 {
		tcs := make([]any, len(toolCalls))
		for i, tc := range toolCalls {
			tcs[i] = tc
		}
		respMsg["tool_calls"] = tcs
	}
	respBody := map[string]any{
		"model": "real-model",
		"usage": map[string]any{
			"prompt_tokens":         in,
			"completion_tokens":     out,
			"prompt_tokens_details": map[string]any{"cached_tokens": cacheRead},
		},
		"choices": []any{
			map[string]any{"index": 0, "finish_reason": finish, "message": respMsg},
		},
	}
	return audit.Record{
		TS: ts, DurMS: 100, Model: "agent", Protocol: "openai", Stream: false, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: map[string][]string{}, Body: body},
			Response: &audit.Message{Status: 200, Headers: map[string][]string{}, Body: respBody},
		},
		Attempts: []audit.Attempt{{Endpoint: endpoint}},
	}
}

// writeToolCall builds an OpenAI tool_calls entry whose arguments look like
// a file write (path + content) — the generic, framework-agnostic shape
// deliverableStats scans for (see compare.go's rejection of hardcoding a
// specific tool name). Args is properly JSON-encoded (not hand-concatenated)
// so a content string containing newlines/quotes round-trips correctly —
// exactly what a real `write` tool call's arguments look like.
func writeToolCall(name, path, content string) map[string]any {
	args, err := json.Marshal(map[string]string{"path": path, "content": content})
	if err != nil {
		panic(err)
	}
	return map[string]any{
		"id":       "call_1",
		"type":     "function",
		"function": map[string]any{"name": name, "arguments": string(args)},
	}
}

// TestComputeComparisonExtras covers ComputeComparisonExtras' full rule
// layer end to end: two single-step Journeys with different endpoints,
// cache ratios, and tool calls (one with a write-shaped call, one without).
func TestComputeComparisonExtras(t *testing.T) {
	atA := time.Date(2026, 7, 28, 0, 5, 44, 0, time.UTC)
	recA := mkExtrasRec(atA, "system prompt A", "do the research", "openai:opencode:deepseek-v4-pro",
		1000, 200, 800, "tool_calls", []map[string]any{writeToolCall("exec", "", "")})
	pathA := writeJSONL(t, []audit.Record{recA})
	jA, err := Build(onlyLineage(t, pathA), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build A: %v", err)
	}

	atB := time.Date(2026, 7, 28, 0, 5, 49, 0, time.UTC)
	recB := mkExtrasRec(atB, "system prompt B", "do the research", "openai:minimax:MiniMax-M3",
		2000, 300, 360, "stop", []map[string]any{writeToolCall("write", "report.md", "# Report\nfindings here")})
	pathB := writeJSONL(t, []audit.Record{recB})
	jB, err := Build(onlyLineage(t, pathB), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build B: %v", err)
	}

	ma, mb := ComputeMetrics(jA), ComputeMetrics(jB)
	ex := ComputeComparisonExtras(jA, jB, ma, mb)

	if len(ex.Endpoints.A) != 1 || ex.Endpoints.A[0] != "openai:opencode:deepseek-v4-pro" {
		t.Errorf("Endpoints.A = %v, want [openai:opencode:deepseek-v4-pro]", ex.Endpoints.A)
	}
	if len(ex.Endpoints.B) != 1 || ex.Endpoints.B[0] != "openai:minimax:MiniMax-M3" {
		t.Errorf("Endpoints.B = %v, want [openai:minimax:MiniMax-M3]", ex.Endpoints.B)
	}
	if ex.Endpoints.Same {
		t.Error("Endpoints.Same should be false: A and B used different endpoints")
	}

	if want := 0.8; ex.Cache.A.FirstRatio != want {
		t.Errorf("Cache.A.FirstRatio = %v, want %v (800/1000)", ex.Cache.A.FirstRatio, want)
	}
	if len(ex.Cache.A.Series) != 1 || ex.Cache.A.SteadyMean != 0 {
		t.Errorf("Cache.A single-step series/SteadyMean = %+v, want len 1 series and 0 steady mean", ex.Cache.A)
	}

	if ex.SysPrompt.A.Changes != 0 || ex.SysPrompt.B.Changes != 0 {
		t.Errorf("SysPrompt changes should be 0 for a Journey's first step (no predecessor to diff against): got A=%d B=%d",
			ex.SysPrompt.A.Changes, ex.SysPrompt.B.Changes)
	}
	if !strings.Contains(ex.SysPrompt.A.Excerpt, "system prompt A") {
		t.Errorf("SysPrompt.A.Excerpt = %q, want it to contain the system text", ex.SysPrompt.A.Excerpt)
	}
	if ex.SysPrompt.A.Truncated {
		t.Error("a short system prompt should not be marked truncated")
	}

	if ex.FinalContext.A.Seq != 1 || ex.FinalContext.B.Seq != 1 {
		t.Errorf("FinalContext seq = A:%d B:%d, want 1/1 (single-step journeys)", ex.FinalContext.A.Seq, ex.FinalContext.B.Seq)
	}

	if ex.Duration.ATermination != "tool_calls" || ex.Duration.BTermination != "stop" {
		t.Errorf("Termination = A:%q B:%q, want tool_calls/stop", ex.Duration.ATermination, ex.Duration.BTermination)
	}

	if ex.Deliverable.A.Found {
		t.Errorf("Deliverable.A should not be found (exec call has no path/content args): %+v", ex.Deliverable.A)
	}
	if !ex.Deliverable.B.Found {
		t.Fatal("Deliverable.B should be found (write call has path+content args)")
	}
	if ex.Deliverable.B.ToolName != "write" || !strings.Contains(ex.Deliverable.B.Excerpt, "findings here") {
		t.Errorf("Deliverable.B = %+v, want tool_name=write and excerpt containing the content", ex.Deliverable.B)
	}
}

// TestSysPromptStats_ExcerptTruncation covers the sysPromptExcerptChars
// bound: a system prompt longer than the limit must be cut and flagged, not
// silently truncated without a signal a reader (or the LLM prompt) can act
// on.
func TestSysPromptStats_ExcerptTruncation(t *testing.T) {
	long := strings.Repeat("x", sysPromptExcerptChars+500)
	at := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	rec := mkExtrasRec(at, long, "hi", "openai:p:m", 100, 10, 0, "stop", nil)
	path := writeJSONL(t, []audit.Record{rec})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)
	ex := ComputeComparisonExtras(j, j, m, m)
	if !ex.SysPrompt.A.Truncated {
		t.Error("long system prompt should be marked Truncated")
	}
	if len(ex.SysPrompt.A.Excerpt) >= len(long) {
		t.Errorf("excerpt should be shorter than the original (%d chars): got %d", len(long), len(ex.SysPrompt.A.Excerpt))
	}
}

// TestDeliverableStats_PicksLastWriteLikeCall covers the reverse-scan order:
// when multiple write-shaped calls happen, the LAST one wins (the real
// final deliverable, not an earlier scratch file).
func TestDeliverableStats_PicksLastWriteLikeCall(t *testing.T) {
	at1 := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	at2 := time.Date(2026, 7, 28, 0, 1, 0, 0, time.UTC)
	rec1 := mkExtrasRec(at1, "sys", "go", "openai:p:m", 100, 10, 0, "tool_calls",
		[]map[string]any{writeToolCall("write", "draft.md", "scratch draft")})
	rec2 := mkExtrasRec(at2, "sys", "go", "openai:p:m", 110, 10, 0, "stop",
		[]map[string]any{writeToolCall("write", "final.md", "the real final content")})
	path := writeJSONL(t, []audit.Record{rec1, rec2})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)
	ex := ComputeComparisonExtras(j, j, m, m)
	if !ex.Deliverable.A.Found || !strings.Contains(ex.Deliverable.A.Excerpt, "the real final content") {
		t.Errorf("Deliverable should pick the LAST write-shaped call: got %+v", ex.Deliverable.A)
	}
}

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
	if len(cmp.Rows) != 14 {
		t.Fatalf("rows = %d, want 14 (one per Metrics field, incl. MetricModelSwitchCount and MetricOutputRepetitionRate)", len(cmp.Rows))
	}

	var modelRow *MetricDiff
	for i := range cmp.Rows {
		if cmp.Rows[i].Metric == MetricModelMS {
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
		if r.Metric == MetricModelMS || r.Metric == MetricNetWorkingMS {
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

// TestCompare_ModelSwitchCount_Row  locks in the registration of
// MetricModelSwitchCount as Compare's 13th row: its A/B values must be
// len(Metrics.ModelSwitches), not the switches' own content.
func TestCompare_ModelSwitchCount_Row(t *testing.T) {
	a := JourneySummary{Metrics: Metrics{ModelSwitches: []ModelSwitch{{StepSeq: 2, From: "p1:m1", To: "p1:m2"}}}}
	b := JourneySummary{Metrics: Metrics{ModelSwitches: []ModelSwitch{
		{StepSeq: 3, From: "p1:m1", To: "p2:m1"},
		{StepSeq: 7, From: "p2:m1", To: "p1:m1"},
		{StepSeq: 9, From: "p1:m1", To: "p2:m1"},
	}}}
	cmp := Compare(a, b)
	var row *MetricDiff
	for i := range cmp.Rows {
		if cmp.Rows[i].Metric == MetricModelSwitchCount {
			row = &cmp.Rows[i]
		}
	}
	if row == nil {
		t.Fatal("MetricModelSwitchCount row missing from Compare's output")
	}
	if row.A != 1 || row.B != 3 {
		t.Errorf("MetricModelSwitchCount A/B = %v/%v, want 1/3", row.A, row.B)
	}
	if row.Kind != KindCount {
		t.Errorf("MetricModelSwitchCount Kind = %v, want KindCount", row.Kind)
	}
}

func TestCompare_OutputRepetitionRate_Row(t *testing.T) {
	a := JourneySummary{Metrics: Metrics{OutputRepetitionRate: 0.12}}
	b := JourneySummary{Metrics: Metrics{OutputRepetitionRate: 0.65}}
	cmp := Compare(a, b)
	var row *MetricDiff
	for i := range cmp.Rows {
		if cmp.Rows[i].Metric == MetricOutputRepetitionRate {
			row = &cmp.Rows[i]
		}
	}
	if row == nil {
		t.Fatal("MetricOutputRepetitionRate row missing from Compare's output")
	}
	if row.A != 0.12 || row.B != 0.65 {
		t.Errorf("MetricOutputRepetitionRate A/B = %v/%v, want 0.12/0.65", row.A, row.B)
	}
	if row.Kind != KindRatio {
		t.Errorf("MetricOutputRepetitionRate Kind = %v, want KindRatio", row.Kind)
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
		if r.Metric == MetricToolCallCount && r.Notable {
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
	md := RenderComparisonMarkdown(Compare(a, b), i18n.EN)

	for _, want := range []string{"j-a", "j-b", "跑A股研究", "跑B股研究", "Model Time", "⚠️", "read"} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered comparison missing %q:\n%s", want, md)
		}
	}
	// Without Extras (the existing lightweight JourneySummary-only path),
	// none of the new rule-layer sections should appear — Extras is purely
	// additive, never a requirement.
	for _, absent := range []string{"## Model & Endpoint Check", "## Prompt Cache Hit Rate", "## System Prompt Size & Stability", "## Final Deliverable Comparison", "## Evidence Provenance"} {
		if strings.Contains(md, absent) {
			t.Errorf("rendered comparison without Extras should not contain %q:\n%s", absent, md)
		}
	}
}

// TestRenderComparisonMarkdown_WithExtras covers the ComparisonExtras
// rendered sections end to end via a real ComputeComparisonExtras call,
// not hand-built fixture data.
func TestRenderComparisonMarkdown_WithExtras(t *testing.T) {
	atA := time.Date(2026, 7, 28, 0, 5, 44, 0, time.UTC)
	recA := mkExtrasRec(atA, "system prompt A", "research", "openai:opencode:deepseek-v4-pro",
		1000, 200, 800, "tool_calls", []map[string]any{writeToolCall("exec", "", "")})
	jA, err := Build(onlyLineage(t, writeJSONL(t, []audit.Record{recA})), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build A: %v", err)
	}
	atB := time.Date(2026, 7, 28, 0, 5, 49, 0, time.UTC)
	recB := mkExtrasRec(atB, "system prompt B", "research", "openai:minimax:MiniMax-M3",
		2000, 300, 360, "stop", []map[string]any{writeToolCall("write", "report.md", "# Report\nfindings here")})
	jB, err := Build(onlyLineage(t, writeJSONL(t, []audit.Record{recB})), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build B: %v", err)
	}

	sa, sb := Summarize(jA), Summarize(jB)
	cmp := Compare(sa, sb)
	extras := ComputeComparisonExtras(jA, jB, sa.Metrics, sb.Metrics)
	cmp.Extras = &extras

	md := RenderComparisonMarkdown(cmp, i18n.EN)
	for _, want := range []string{
		"## Model & Endpoint Check", "openai:opencode:deepseek-v4-pro", "openai:minimax:MiniMax-M3", "differ",
		"## Prompt Cache Hit Rate", "80%",
		"## System Prompt Size & Stability", "system prompt A", "system prompt B",
		"## Final Deliverable Comparison", "findings here", "no comparable final deliverable identified",
		"Net Working Time",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered comparison (with Extras) missing %q:\n%s", want, md)
		}
	}
	// Sources wasn't set on this Extras (ComputeComparisonExtras never sets
	// it itself — see TestRenderComparisonMarkdown_WithSources for the
	// populated case), so the evidence-provenance section must not render a
	// heading with nothing under it.
	if strings.Contains(md, "## Evidence Provenance") {
		t.Errorf("rendered comparison without Extras.Sources should not contain the Evidence Provenance section:\n%s", md)
	}
}

// TestRenderComparisonMarkdown_WithSources covers the evidence-provenance
// section: Extras.Sources, when the caller sets it (cmd_story.go does, from
// resolveInputPaths — ComputeComparisonExtras
// itself never touches this field), must render as a "证据溯源" section
// listing every source path so a reader can independently re-open the exact
// audit records every number in the report was computed from.
func TestRenderComparisonMarkdown_WithSources(t *testing.T) {
	a := JourneySummary{ID: "j-a", Title: "A", From: time.Now(), To: time.Now()}
	b := JourneySummary{ID: "j-b", Title: "B", From: time.Now(), To: time.Now()}
	cmp := Compare(a, b)
	extras := ComparisonExtras{Sources: []string{"logs/vmr-audit-2026-07-28.jsonl.zst", "logs/vmr-audit-2026-07-29.jsonl"}}
	cmp.Extras = &extras

	md := RenderComparisonMarkdown(cmp, i18n.EN)
	for _, want := range []string{"## Evidence Provenance", "logs/vmr-audit-2026-07-28.jsonl.zst", "logs/vmr-audit-2026-07-29.jsonl"} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered comparison with Extras.Sources missing %q:\n%s", want, md)
		}
	}
}

// journeyOfTasks builds a Journey directly from Task literals — 6a/6b's
// divergence detection compares two independent Journeys position-by-
// position, so these tests need full control over each side's Task/Step
// shape without going through the audit-record Build() pipeline (findings_
// test.go's journeyOf covers the single-Task case; divergence tests need
// multiple Tasks on at least one side).
func journeyOfTasks(tasks ...*Task) *Journey { return &Journey{Tasks: tasks} }

func TestComputeDivergence(t *testing.T) {
	t.Run("identical shared prefix: not found", func(t *testing.T) {
		a := journeyOf(
			&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a"}`)}},
			&Step{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("write", `{"path":"b"}`)}},
		)
		b := journeyOf(
			&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a"}`)}},
			&Step{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("write", `{"path":"b"}`)}},
		)
		d := computeDivergence(a, b)
		if d.Found {
			t.Fatalf("expected no divergence for identical sequences, got %+v", d)
		}
	})

	t.Run("different tool chosen: heavy, at the first differing position", func(t *testing.T) {
		a := journeyOf(
			&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a"}`)}},
			&Step{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("write", `{"path":"b"}`)}},
		)
		b := journeyOf(
			&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a"}`)}},
			&Step{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("delete", `{"path":"b"}`)}},
		)
		d := computeDivergence(a, b)
		if !d.Found || d.Severity != DivergenceHeavy || d.Index != 1 {
			t.Fatalf("got %+v, want Found=true Severity=heavy Index=1", d)
		}
		if d.AStepSeq != 2 || d.BStepSeq != 2 {
			t.Errorf("AStepSeq/BStepSeq = %d/%d, want 2/2", d.AStepSeq, d.BStepSeq)
		}
	})

	t.Run("one side has a tool call, the other doesn't: heavy", func(t *testing.T) {
		a := journeyOf(&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a"}`)}})
		b := journeyOf(&Step{Seq: 1, RespText: "just thinking, no action yet"})
		d := computeDivergence(a, b)
		if !d.Found || d.Severity != DivergenceHeavy || d.Index != 0 {
			t.Fatalf("got %+v, want Found=true Severity=heavy Index=0", d)
		}
		buf, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if !strings.Contains(string(buf), `"index":0`) {
			t.Errorf("Index=0 must survive JSON marshaling (regression: `omitempty` silently drops it), got %s", buf)
		}
	})

	t.Run("same tool, different args: light", func(t *testing.T) {
		a := journeyOf(&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a"}`)}})
		b := journeyOf(&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"different"}`)}})
		d := computeDivergence(a, b)
		if !d.Found || d.Severity != DivergenceLight {
			t.Fatalf("got %+v, want Found=true Severity=light", d)
		}
	})

	t.Run("shorter side bounds the comparison, task title comes from A", func(t *testing.T) {
		a := journeyOfTasks(
			&Task{Title: "investigate", Steps: []*Step{{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{}`)}}}},
			&Task{Title: "fix it", Steps: []*Step{{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("write", `{}`)}}}},
		)
		b := journeyOf(&Step{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{}`)}})
		d := computeDivergence(a, b)
		if d.Found {
			t.Fatalf("comparison should stop at B's shorter length without ever reaching A's second Task: %+v", d)
		}
	})
}
