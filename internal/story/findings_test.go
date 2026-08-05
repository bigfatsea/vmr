// Ver 2026-08-05, by Sonnet 5

package story

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
)

// journeyOf wraps steps into a single-Task Journey — findings.go's
// detectors only read Task/Step data, never Manifest/Rec, so a minimal
// literal fixture (no full Build() pipeline) is enough for these tests.
func journeyOf(steps ...*Step) *Journey {
	return &Journey{Tasks: []*Task{{Title: "t", Steps: steps}}}
}

func tc(name, args string) chatmsg.ToolCall {
	return chatmsg.ToolCall{ID: name, Name: name, Args: args}
}

func TestDetectExactRepeatToolCall(t *testing.T) {
	mk := func(n int) *Journey {
		var steps []*Step
		for i := 1; i <= n; i++ {
			steps = append(steps, &Step{Seq: i, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"go build"}`)}})
		}
		return journeyOf(steps...)
	}

	t.Run("below threshold: no finding", func(t *testing.T) {
		got := ComputeFindings(mk(exactRepeatThreshold-1), i18n.EN)
		for _, f := range got {
			if f.Code == FindingExactRepeatToolCall {
				t.Fatalf("unexpected finding below threshold: %+v", f)
			}
		}
	})

	t.Run("at threshold: fires once, located at the last occurrence", func(t *testing.T) {
		got := ComputeFindings(mk(exactRepeatThreshold), i18n.EN)
		var hits []Finding
		for _, f := range got {
			if f.Code == FindingExactRepeatToolCall {
				hits = append(hits, f)
			}
		}
		if len(hits) != 1 {
			t.Fatalf("got %d exact_repeat_tool_call findings, want 1: %+v", len(hits), hits)
		}
		if hits[0].StepSeq != exactRepeatThreshold {
			t.Errorf("StepSeq = %d, want %d (the last occurrence)", hits[0].StepSeq, exactRepeatThreshold)
		}
		wantRelated := make([]int, exactRepeatThreshold-1)
		for i := range wantRelated {
			wantRelated[i] = i + 1
		}
		if !reflect.DeepEqual(hits[0].RelatedSeq, wantRelated) {
			t.Errorf("RelatedSeq = %v, want %v", hits[0].RelatedSeq, wantRelated)
		}
	})

	t.Run("different args never counts as a repeat", func(t *testing.T) {
		var steps []*Step
		for i := 1; i <= exactRepeatThreshold+2; i++ {
			steps = append(steps, &Step{Seq: i, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"echo `+string(rune('a'+i))+`"}`)}})
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingExactRepeatToolCall {
				t.Fatalf("unexpected finding: every call had distinct args: %+v", f)
			}
		}
	})
}

func TestDetectNarrationWithoutAction(t *testing.T) {
	similarSteps := func(n int) []*Step {
		var steps []*Step
		for i := 1; i <= n; i++ {
			steps = append(steps, &Step{Seq: i, RespText: "let me now assemble the final document for you"})
		}
		return steps
	}

	t.Run("run at threshold fires", func(t *testing.T) {
		got := ComputeFindings(journeyOf(similarSteps(narrationMinRun)...), i18n.EN)
		var hits int
		for _, f := range got {
			if f.Code == FindingNarrationWithoutAction {
				hits++
			}
		}
		if hits != 1 {
			t.Fatalf("got %d narration_without_action findings, want 1", hits)
		}
	})

	t.Run("below threshold does not fire", func(t *testing.T) {
		got := ComputeFindings(journeyOf(similarSteps(narrationMinRun-1)...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingNarrationWithoutAction {
				t.Fatalf("unexpected finding below run threshold: %+v", f)
			}
		}
	})

	t.Run("a tool call breaks the run", func(t *testing.T) {
		steps := similarSteps(narrationMinRun)
		steps[1].ToolCalls = []chatmsg.ToolCall{tc("write", "{}")}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingNarrationWithoutAction {
				t.Fatalf("unexpected finding: a tool call should break the narration run: %+v", f)
			}
		}
	})

	t.Run("dissimilar text does not fire", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, RespText: "checking the database schema now"},
			{Seq: 2, RespText: "the weather today looks fine"},
			{Seq: 3, RespText: "let's talk about something entirely different"},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingNarrationWithoutAction {
				t.Fatalf("unexpected finding: consecutive texts share no vocabulary: %+v", f)
			}
		}
	})
}

func TestDetectUnverifiedSuccess(t *testing.T) {
	errEvent := &Event{Msg: chatmsg.Message{Text: "tool result: " + isErrorMarker + " build failed"}}

	t.Run("error then no verification then task ends: fires", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"go build"}`)}},
			{Seq: 2, NewEvents: []*Event{errEvent}, Finish: "stop"},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		var found *Finding
		for i := range got {
			if got[i].Code == FindingUnverifiedSuccess {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatal("expected error_then_unverified_success finding")
		}
		if found.StepSeq != 2 {
			t.Errorf("StepSeq = %d, want 2", found.StepSeq)
		}
		if !reflect.DeepEqual(found.RelatedSeq, []int{2}) {
			t.Errorf("RelatedSeq = %v, want [2] (the erroring step)", found.RelatedSeq)
		}
	})

	t.Run("a read-shaped call after the error disarms it", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, NewEvents: []*Event{errEvent}},
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("read_file", `{"path":"/a"}`)}},
			{Seq: 3, Finish: "stop"},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingUnverifiedSuccess {
				t.Fatalf("unexpected finding: a verification-shaped call should disarm it: %+v", f)
			}
		}
	})

	t.Run("no error at all: never fires", func(t *testing.T) {
		steps := []*Step{{Seq: 1, Finish: "stop"}}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingUnverifiedSuccess {
				t.Fatalf("unexpected finding with no error marker: %+v", f)
			}
		}
	})
}

func TestDetectReasoningActionMismatch(t *testing.T) {
	t.Run("reasoning names a file the call never touches: fires", func(t *testing.T) {
		steps := []*Step{{
			Seq:       1,
			Reasoning: "I need to check config.go carefully before making any changes to the router logic",
			ToolCalls: []chatmsg.ToolCall{tc("write_file", `{"path":"router.go","content":"..."}`)},
		}}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		var found bool
		for _, f := range got {
			if f.Code == FindingReasoningActionMismatch && f.StepSeq == 1 {
				found = true
			}
		}
		if !found {
			t.Fatal("expected reasoning_action_mismatch finding")
		}
	})

	t.Run("reasoning and action agree: no finding", func(t *testing.T) {
		steps := []*Step{{
			Seq:       1,
			Reasoning: "I need to check config.go carefully before making any changes to it",
			ToolCalls: []chatmsg.ToolCall{tc("write_file", `{"path":"config.go","content":"..."}`)},
		}}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingReasoningActionMismatch {
				t.Fatalf("unexpected finding: entity is present in the call args: %+v", f)
			}
		}
	})

	t.Run("only the final sentence is scanned: an earlier plan item is not a mismatch", func(t *testing.T) {
		// Calibration regression (logs/vmr-audit-2026-07-25): reasoning
		// narrating a numbered plan across several files, where this
		// Step's call only touches the LAST one, must not flag the
		// earlier-mentioned files as a mismatch — they're later steps,
		// not evidence this call ignored its own justification.
		steps := []*Step{{
			Seq:       1,
			Reasoning: "Plan: 1. check AGENTS.md first. 2. then check SOUL.md. Let me read SOUL.md now to confirm it loaded correctly and completely",
			ToolCalls: []chatmsg.ToolCall{tc("read_file", `{"path":"/home/user/.hermes/SOUL.md"}`)},
		}}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingReasoningActionMismatch {
				t.Fatalf("unexpected finding: AGENTS.md is an earlier plan step, not this turn's justification: %+v", f)
			}
		}
	})

	t.Run("path-prefix differences don't count as a mismatch", func(t *testing.T) {
		// Calibration regression: the same file gets extracted as two
		// different entity strings depending on where the regex's word
		// boundary starts ("~/.hermes/SOUL.md" scans as "hermes/SOUL.md",
		// "/home/user/.hermes/SOUL.md" scans as "home/user/.hermes/SOUL.md")
		// — substring-tolerant matching must treat these as the same file.
		steps := []*Step{{
			Seq:       1,
			Reasoning: "Let me check whether ~/.hermes/SOUL.md loaded correctly and completely this time around",
			ToolCalls: []chatmsg.ToolCall{tc("read_file", `{"path":"/home/user/.hermes/SOUL.md"}`)},
		}}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingReasoningActionMismatch {
				t.Fatalf("unexpected finding: same file, different path prefix: %+v", f)
			}
		}
	})

	t.Run("short reasoning is below the noise floor", func(t *testing.T) {
		steps := []*Step{{
			Seq:       1,
			Reasoning: "check a.go",
			ToolCalls: []chatmsg.ToolCall{tc("write_file", `{"path":"b.go"}`)},
		}}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingReasoningActionMismatch {
				t.Fatalf("unexpected finding: reasoning text is under reasoningMinChars: %+v", f)
			}
		}
	})
}

func TestDetectPlanExecutionMisalignment(t *testing.T) {
	t.Run("one of two plan items never referenced again: fires", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Reasoning: "Plan:\n1. read config.go and understand it\n2. update deploy.yaml with the new settings"},
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"config.go"}`)}},
			{Seq: 3, RespText: "done reading config.go"},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		var found *Finding
		for i := range got {
			if got[i].Code == FindingPlanExecutionMisalignment {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatal("expected plan_execution_misalignment finding (deploy.yaml item never referenced again)")
		}
		if found.StepSeq != 1 {
			t.Errorf("StepSeq = %d, want 1 (the Task's opening step)", found.StepSeq)
		}
	})

	t.Run("first plan item executed in the SAME turn it was announced: no finding for it", func(t *testing.T) {
		// Calibration regression (logs/vmr-audit-2026-07-27): a plan and
		// its first action routinely land in the same Step (the model
		// states the plan, then immediately issues the first tool call in
		// that same response) — the first version only scanned
		// task.Steps[1:], so it never saw the announcing Step's own
		// ToolCalls and flagged same-turn execution as "never referenced".
		steps := []*Step{
			{Seq: 1, Reasoning: "Plan:\n1. read config.go and understand it\n2. update deploy.yaml with the new settings",
				ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"config.go"}`)}},
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("write", `{"path":"deploy.yaml"}`)}},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingPlanExecutionMisalignment {
				t.Fatalf("unexpected finding: both plan items were executed, one in the same turn as the plan: %+v", f)
			}
		}
	})

	t.Run("both plan items executed: no finding", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Reasoning: "Plan:\n1. read config.go\n2. update deploy.yaml"},
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"config.go"}`)}},
			{Seq: 3, ToolCalls: []chatmsg.ToolCall{tc("write", `{"path":"deploy.yaml"}`)}},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingPlanExecutionMisalignment {
				t.Fatalf("unexpected finding: both items were later referenced: %+v", f)
			}
		}
	})

	t.Run("no numbered list: silently skipped", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Reasoning: "I'll go read the config file and then figure out what to do next."},
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"config.go"}`)}},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingPlanExecutionMisalignment {
				t.Fatalf("unexpected finding: no numbered plan was ever stated: %+v", f)
			}
		}
	})

	t.Run("a long numbered list is treated as a document, not a plan", func(t *testing.T) {
		// Calibration regression (logs/vmr-audit-2026-07-1x): numbered
		// lists beyond maxPlanItems were almost always a written report
		// or essay, not an execution checklist — "every single item
		// unmatched" dominated those hits and was pure noise (see
		// maxPlanItems's doc comment).
		var lines []string
		for i := 1; i <= maxPlanItems+2; i++ {
			// Sequentially numbered (1, 2, 3, ...) so this stays ONE
			// contiguous run under lastNumberedList — repeating "1." would
			// instead read as maxPlanItems+2 separate one-item runs and
			// exercise minPlanItems, not the cap this test is for.
			lines = append(lines, strconv.Itoa(i)+". item")
		}
		steps := []*Step{{Seq: 1, Reasoning: strings.Join(lines, "\n")}}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingPlanExecutionMisalignment {
				t.Fatalf("unexpected finding: list exceeds maxPlanItems: %+v", f)
			}
		}
	})

	t.Run("two separate numbered lists: only the last one is the plan", func(t *testing.T) {
		// Calibration regression (logs/vmr-audit-2026-07-27/28, real
		// production traffic): reasoning that first enumerates a topic
		// breakdown ("here's how I read the request: 1... 2...") and then,
		// separately, the actual plan ("let me plan the approach: 1...
		// 2...") must only be scored against the second list — the first
		// version concatenated both into one 8-item plan and produced a
		// "6/8 items unmatched" finding purely because the topic-breakdown
		// items were never meant to be executed at all.
		steps := []*Step{
			{Seq: 1, Reasoning: "Breaking down the request:\n" +
				"1. Data Collection: gather everything\n" +
				"2. Analysis: find patterns\n\n" +
				"Let me plan the approach:\n" +
				"1. read config.go and understand it\n" +
				"2. update deploy.yaml with the new settings"},
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"config.go"}`)}},
			{Seq: 3, RespText: "done reading config.go"},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		var found *Finding
		for i := range got {
			if got[i].Code == FindingPlanExecutionMisalignment {
				found = &got[i]
			}
		}
		if found == nil {
			t.Fatal("expected plan_execution_misalignment finding (deploy.yaml, from the SECOND list, never referenced again)")
		}
		if !strings.Contains(found.Finding, "of the 2 plan items") {
			t.Errorf("finding should score against the 2-item actual plan, not the 4 combined items from both lists: %q", found.Finding)
		}
	})

	t.Run("a single numbered sentence is not a plan", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Reasoning: "Note: 1. this is just an aside, not a real multi-step plan."},
		}
		got := ComputeFindings(journeyOf(steps...), i18n.EN)
		for _, f := range got {
			if f.Code == FindingPlanExecutionMisalignment {
				t.Fatalf("unexpected finding: minPlanItems requires >= 2 items: %+v", f)
			}
		}
	})
}

// TestComputeFindingsIsDeterministic mirrors
// internal/report/aggregate_test.go's TestBuildFindingsIsDeterministic: the
// SELECTION of findings (Code/StepSeq/RelatedSeq) must be identical whether
// ComputeFindings is called with EN (feeding journey-<id>.json) or a
// different lang (feeding the rendered Markdown) — only the text may vary.
// If a detector's selection logic ever accidentally reads i18n text instead
// of raw data, this catches it.
func TestComputeFindingsIsDeterministic(t *testing.T) {
	steps := []*Step{
		{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"go build"}`)}},
		{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"go build"}`)}},
		{Seq: 3, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"go build"}`)},
			NewEvents: []*Event{{Msg: chatmsg.Message{Text: isErrorMarker}}}},
		{Seq: 4, RespText: "let me now assemble the final document for you"},
		{Seq: 5, RespText: "let me now assemble the final document for you"},
		{Seq: 6, RespText: "let me now assemble the final document for you", Finish: "stop"},
	}
	j := journeyOf(steps...)

	type key struct {
		Code    FindingCode
		StepSeq int
	}
	keysOf := func(fs []Finding) map[key]bool {
		out := make(map[key]bool, len(fs))
		for _, f := range fs {
			out[key{f.Code, f.StepSeq}] = true
		}
		return out
	}

	en := keysOf(ComputeFindings(j, i18n.EN))
	zh := keysOf(ComputeFindings(j, i18n.ZH))
	if len(en) == 0 {
		t.Fatal("test fixture produced no findings at all — fixture is not exercising the detectors")
	}
	if !reflect.DeepEqual(en, zh) {
		t.Fatalf("EN and ZH selected different findings — selection must not depend on lang:\nEN: %v\nZH: %v", en, zh)
	}
}
