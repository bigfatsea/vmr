// Ver 2026-07-30 00:10, by Sonnet 5

// Tests for the nine-indicator behavior taskseg.
package story

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// TestComputeMetrics_TimeSplitAndRatio covers F10's gap classification:
// a tool-loop continuation's gap is agent-side execution
// time, a new-instruction step's gap is human idle time, and NetWorkingMS
// excludes the latter.
func TestComputeMetrics_TimeSplitAndRatio(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "task A: investigate X")
	a1 := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
		map[string]any{"id": "c1", "function": map[string]any{"name": "search", "arguments": "{}"}},
	}}
	t1 := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "results"}
	u2 := msg("user", "task B: now summarize")

	// step1 @ t=0 (first step, HumanInitiated by construction)
	// step2 @ t=+5min: pure tool-loop continuation (no new user message) -> agent-side gap
	// step3 @ t=+9min: a genuinely new instruction (u2) -> human-idle gap
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("ok"))
	r2 := mkRec(at(5), "", []any{sys, u1, a1, t1}, sseText("ok2"))
	r3 := mkRec(at(9), "", []any{sys, u1, a1, t1, u2}, sseText("ok3"))

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	steps := journeySteps(j)
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}
	if steps[1].HumanInitiated {
		t.Fatal("test setup: step 2 (pure tool-loop continuation) should not be HumanInitiated")
	}
	if !steps[2].HumanInitiated {
		t.Fatal("test setup: step 3 (new instruction u2) should be HumanInitiated")
	}

	m := ComputeMetrics(j)
	wantModel := int64(3 * 100) // mkRec's fixed DurMS
	if m.ModelMS != wantModel {
		t.Errorf("ModelMS = %d, want %d", m.ModelMS, wantModel)
	}
	wantAgent := int64(5*60*1000 - 100)
	if m.AgentExecMS != wantAgent {
		t.Errorf("AgentExecMS = %d, want %d (step2's gap)", m.AgentExecMS, wantAgent)
	}
	wantIdle := int64(4*60*1000 - 100)
	if m.HumanIdleMS != wantIdle {
		t.Errorf("HumanIdleMS = %d, want %d (step3's gap)", m.HumanIdleMS, wantIdle)
	}
	if m.NetWorkingMS != m.ModelMS+m.AgentExecMS {
		t.Errorf("NetWorkingMS = %d, want ModelMS+AgentExecMS = %d", m.NetWorkingMS, m.ModelMS+m.AgentExecMS)
	}
	wantRatio := float64(m.ModelMS) / float64(m.AgentExecMS)
	if m.ModelToToolRatio != wantRatio {
		t.Errorf("ModelToToolRatio = %v, want %v", m.ModelToToolRatio, wantRatio)
	}
}

// TestHumanInitiated_StitchBoundaryWithGenuinelyNewInstruction covers the
// atStitchBoundary branch of buildFrom's HumanInitiated wiring: a stitch
// boundary whose opening carries a genuinely new instruction (never shown
// by the predecessor) must be HumanInitiated, the mirror image of
// TestStitchedJourney_EndToEnd's assertion (in stitch_test.go) that a
// boundary with NOTHING new is not.
func TestHumanInitiated_StitchBoundaryWithGenuinelyNewInstruction(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "investigate and summarize")

	var recs []audit.Record
	predMsgs := []any{sys, u1}
	for i := 0; i < 6; i++ {
		recs = append(recs, mkRec(at(i), "", append([]any{}, predMsgs...), sseText("ok")))
		predMsgs = append(predMsgs, msg("assistant", "checked stuff "+strconv.Itoa(i)))
		if i >= 4 {
			predMsgs = append(predMsgs, msg("tool", "tool output "+strconv.Itoa(i)))
		}
	}
	// Stitch boundary: shared anchor u1 plus the two most recent replies
	// (3+ shared distinct keys) PLUS a genuinely new instruction the
	// predecessor never showed.
	newInstr := msg("user", "now also check the deployment logs")
	succMsgs := []any{msg("system", "sys v2"), u1, msg("assistant", "checked stuff 4"), msg("tool", "tool output 4"),
		newInstr}
	recs = append(recs, mkRec(at(30), "", succMsgs, sseText("continuing")))

	path := writeJSONL(t, recs)
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2", len(g.Lineages))
	}
	ctxgraph.StitchGraph(g)
	byIdx := ctxgraph.LineageIndex(g)
	second := g.Lineages[1]
	chain := ctxgraph.ChainFrom(second, byIdx)
	if len(chain) != 2 {
		t.Fatalf("chain length = %d, want 2 (stitch should have succeeded)", len(chain))
	}

	j, err := BuildChain(chain, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("BuildChain: %v", err)
	}
	steps := journeySteps(j)
	last := steps[len(steps)-1]
	if !last.HumanInitiated {
		t.Error("stitch-boundary step with a genuinely new instruction should be HumanInitiated")
	}
}

// TestComputeMetrics_ToolCallDistributionAndDuplicateRate covers the tool
// call distribution (name + token share) and the duplicate-action rate
// (repeated identical (name, args) pairs).
func TestComputeMetrics_ToolCallDistributionAndDuplicateRate(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "read files repeatedly")
	readA := map[string]any{"id": "c1", "function": map[string]any{"name": "read", "arguments": `{"path":"/a.md"}`}}
	readADup := map[string]any{"id": "c2", "function": map[string]any{"name": "read", "arguments": `{"path":"/a.md"}`}}
	writeB := map[string]any{"id": "c3", "function": map[string]any{"name": "write", "arguments": `{"path":"/b.md","content":"hello"}`}}

	a1 := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{readA}}
	a2 := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{readADup, writeB}}
	t1 := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "a contents"}
	t2a := map[string]any{"role": "tool", "tool_call_id": "c2", "content": "a contents again"}
	t2b := map[string]any{"role": "tool", "tool_call_id": "c3", "content": "written"}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("ok"))
	r2 := mkRec(at(1), "", []any{sys, u1, a1, t1}, sseText("ok2"))
	r3 := mkRec(at(2), "", []any{sys, u1, a1, t1, a2, t2a, t2b}, sseText("ok3"))

	// r1's response is the assistant's a1 (issuing readA); r2's is a2
	// (issuing readADup + writeB) — mkRec's response body is independent of
	// the request messages, so wire the tool_calls into the response bodies
	// directly instead of via sseText.
	r1.Client.Response.Body = sseToolCalls([]any{readA})
	r2.Client.Response.Body = sseToolCalls([]any{readADup, writeB})

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)

	if m.ToolCallCount != 3 {
		t.Fatalf("ToolCallCount = %d, want 3", m.ToolCallCount)
	}
	byName := map[string]ToolCallStat{}
	for _, s := range m.ToolCallDist {
		byName[s.Name] = s
	}
	if byName["read"].Count != 2 {
		t.Errorf("read count = %d, want 2", byName["read"].Count)
	}
	if byName["write"].Count != 1 {
		t.Errorf("write count = %d, want 1", byName["write"].Count)
	}
	if byName["read"].TokenShare <= 0 || byName["write"].TokenShare <= 0 {
		t.Errorf("expected both tools to have a positive token share, got %+v", m.ToolCallDist)
	}

	// 1 duplicate ((read, {"path":"/a.md"}) called twice) out of 3 total calls.
	wantDup := 1.0 / 3.0
	if m.DuplicateActionRate != wantDup {
		t.Errorf("DuplicateActionRate = %v, want %v", m.DuplicateActionRate, wantDup)
	}
}

// sseToolCalls builds a non-streaming response body whose assistant message
// issues the given openai-shaped tool_calls — same request/response
// independence render_md_test.go's TestRenderMarkdown_LLMResponseSection
// relies on.
func sseToolCalls(toolCalls []any) map[string]any {
	return map[string]any{
		"model": "agent",
		"choices": []any{map[string]any{
			"finish_reason": "tool_calls",
			"message":       map[string]any{"role": "assistant", "content": "", "tool_calls": toolCalls},
		}},
	}
}

// TestComputeMetrics_ErrorRecoveryCount covers the is_error -> retry proxy:
// an Anthropic-shaped tool_result with is_error=true, followed in the same
// step by the agent issuing its own tool call, counts as a recovery
// attempt.
func TestComputeMetrics_ErrorRecoveryCount(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "run the build")
	toolUse := map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "tool_use", "id": "tu1", "name": "bash", "input": map[string]any{"cmd": "go build"}},
	}}
	toolResultErr := map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "tu1", "is_error": true, "content": "build failed"},
	}}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
		map[string]any{"id": "tu1", "function": map[string]any{"name": "bash", "arguments": `{"cmd":"go build"}`}},
	}))
	// step 2's NewEvents include toolResultErr (the error); its own response
	// retries with another bash call.
	r2 := mkRec(at(1), "", []any{sys, u1, toolUse, toolResultErr}, sseToolCalls([]any{
		map[string]any{"id": "tu2", "function": map[string]any{"name": "bash", "arguments": `{"cmd":"go build -v"}`}},
	}))
	// step 3 has no preceding error -> must not be counted.
	r3 := mkRec(at(2), "", []any{sys, u1, toolUse, toolResultErr, msg("assistant", "build ok now")}, sseText("done"))

	path := writeJSONL(t, []audit.Record{r1, r2, r3})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)
	if m.ErrorRecoveryCount != 1 {
		t.Errorf("ErrorRecoveryCount = %d, want 1", m.ErrorRecoveryCount)
	}
}

// TestComputeMetrics_PlanExecRatio covers the plan/execution split: steps
// with no tool call in their response are "plan", the rest are "exec".
func TestComputeMetrics_PlanExecRatio(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "let's think, then act")
	a1 := msg("assistant", "let me think about this first")

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("thinking out loud")) // plan
	r2 := mkRec(at(1), "", []any{sys, u1, a1}, sseToolCalls([]any{
		map[string]any{"id": "c1", "function": map[string]any{"name": "run", "arguments": "{}"}},
	})) // exec

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)
	if m.PlanExecRatio != 0.5 {
		t.Errorf("PlanExecRatio = %v, want 0.5", m.PlanExecRatio)
	}
}

// TestComputeMetrics_ContextUtilization covers the S-D-scenario indicator:
// an entity mentioned again later counts as "referenced"; one mentioned
// only once never does.
func TestComputeMetrics_ContextUtilization(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "read AGENTS.md and NEVERSEENAGAIN.md")
	a1 := msg("assistant", "AGENTS.md says to run tests before committing")

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("ok"))
	r2 := mkRec(at(1), "", []any{sys, u1, a1}, sseText("done"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	m := ComputeMetrics(j)
	if m.ContextUtilization <= 0 || m.ContextUtilization >= 1 {
		t.Errorf("ContextUtilization = %v, want strictly between 0 and 1 (AGENTS.md referenced again, NEVERSEENAGAIN.md is not)", m.ContextUtilization)
	}
}

// TestComputeMetrics_CompactionTotals reuses the same stitched-chain shape
// TestCompactionInfo_TokensAndEntities builds, verifying ComputeMetrics
// rolls up per-step CompactionInfo into a Journey-level total.
func TestComputeMetrics_CompactionTotals(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "investigate and summarize")

	predMsgs := []any{sys, u1}
	var recs []audit.Record
	const preBreakTurns = 6
	for i := 0; i < preBreakTurns; i++ {
		recs = append(recs, mkRecWithUsage(at(i), predMsgs, "ok", 1000+int64(i)*100, 50))
		predMsgs = append(predMsgs, msg("assistant", "checked stuff "+strconv.Itoa(i)))
		if i >= 4 {
			predMsgs = append(predMsgs, msg("tool", "tool output "+strconv.Itoa(i)))
		}
	}
	succMsgs := []any{msg("system", "sys v2"), u1, msg("assistant", "checked stuff 4"), msg("tool", "tool output 4"),
		msg("assistant", "resuming")}
	recs = append(recs, mkRecWithUsage(at(30), succMsgs, "continuing", 500, 20))

	path := writeJSONL(t, recs)
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	ctxgraph.StitchGraph(g)
	byIdx := ctxgraph.LineageIndex(g)
	second := g.Lineages[1]
	chain := ctxgraph.ChainFrom(second, byIdx)

	j, err := BuildChain(chain, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("BuildChain: %v", err)
	}
	m := ComputeMetrics(j)
	if m.CompactionCount != 1 {
		t.Fatalf("CompactionCount = %d, want 1", m.CompactionCount)
	}
	wantLoss := int64(1000+(preBreakTurns-1)*100) - 500
	if m.CompactionLossTokens != wantLoss {
		t.Errorf("CompactionLossTokens = %d, want %d", m.CompactionLossTokens, wantLoss)
	}
}

// TestSummarize covers JourneySummary's own construction: identity fields
// copied straight from the Journey, Metrics computed fresh (not just
// zero-valued) — the shape journey-<id>.json actually serializes and Step
// 4's 4d module (Compare) consumes.
func TestSummarize(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "调研一下")
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("开工"))
	r2 := mkRec(at(1), "", []any{sys, u1, msg("assistant", "done")}, sseText("完成"))
	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	s := Summarize(j, i18n.EN)
	if s.ID != j.ID || s.Title != j.Title || !s.From.Equal(j.From) || !s.To.Equal(j.To) {
		t.Errorf("Summarize identity fields = %+v, want them copied from Journey %+v", s, j)
	}
	want := ComputeMetrics(j)
	if s.Metrics.ModelMS != want.ModelMS || s.Metrics.NetWorkingMS != want.NetWorkingMS {
		t.Errorf("Summarize.Metrics = %+v, want it to match a fresh ComputeMetrics(j) = %+v", s.Metrics, want)
	}
}

func TestSummarize_WithLLMFindings(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "test")
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("start"))
	path := writeJSONL(t, []audit.Record{r1})
	l := onlyLineage(t, path)
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	s := Summarize(j, i18n.EN)
	dataNoLLM, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(dataNoLLM), "llm_findings") {
		t.Errorf("expected no llm_findings in JSON when LLMFindings is empty, got: %s", string(dataNoLLM))
	}

	s.LLMFindings = []Finding{
		{
			Code:           FindingToolResultMisinterpretation,
			StepSeq:        1,
			Source:         SourceLLMInferred,
			Confidence:     ConfidenceHigh,
			EvidenceAnchor: "error returned",
			Finding:        "AI finding",
		},
	}
	dataWithLLM, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal with LLM: %v", err)
	}
	if !strings.Contains(string(dataWithLLM), "llm_findings") {
		t.Errorf("expected llm_findings in JSON when LLMFindings is populated, got: %s", string(dataWithLLM))
	}
	if !strings.Contains(string(dataWithLLM), "llm_inferred") || !strings.Contains(string(dataWithLLM), "HIGH") {
		t.Errorf("expected source and confidence serialized in llm_findings, got: %s", string(dataWithLLM))
	}
}
