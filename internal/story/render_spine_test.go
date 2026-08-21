// Ver 2026-08-05, by Sonnet 5

// Direct unit tests for render_spine.go's decision-spine layer
// (renderOverviewCard/timelineNodes/structuralTags, renderDecisionSpine,
// stepRoleTag, renderToolTimeline, padRight) — previously only exercised
// indirectly via golden_test.go's no-tool-call, no-Finding fixture, which
// never touched any of the populated code paths. Fixtures reuse this
// package's existing helpers: journeyOf/journeyOfTasks/tc from
// compare_test.go/findings_test.go, not a new fixture style.
// render_spine_args_test.go covers toolCallLine/scalarSummary/capFull, the
// argument-renderer half split into render_spine_args.go.
package story

import (
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
)

// --- shared helpers ----------------------------------------------------

// mkManifest builds the minimal *ctxgraph.Manifest timelineNodes/
// renderOverviewCard need — every Step they read dereferences s.Manifest.TS,
// so a literal Step fixture (journeyOf's style) must set this or the call
// panics on a nil pointer.
func mkManifest(ts time.Time) *ctxgraph.Manifest {
	return &ctxgraph.Manifest{TS: ts}
}

// errorEvent builds a NewEvents entry carrying isErrorMarker — the exact
// literal chatmsg.RenderPart embeds for an is_error tool_result, per
// metrics.go's isErrorMarker doc comment. Using the constant (not a guessed
// string) keeps this test from silently drifting from the real marker.
func errorEvent() *Event {
	return &Event{Msg: chatmsg.Message{Role: "tool", Text: isErrorMarker + " boom"}}
}

// capture returns a w func(string, ...any) writer (the signature every
// render_spine.go render func takes) plus the buffer it writes into.
func capture() (func(string, ...any), *strings.Builder) {
	var b strings.Builder
	w := func(format string, args ...any) {
		if len(args) == 0 {
			b.WriteString(format)
			return
		}
		// Every render_spine.go call site uses "%s" with a single already-
		// formatted string argument — no need for full fmt.Sprintf support.
		for _, a := range args {
			if s, ok := a.(string); ok {
				b.WriteString(s)
			}
		}
	}
	return w, &b
}

var baseTime = time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)

func at(minOffset int) time.Time { return baseTime.Add(time.Duration(minOffset) * time.Minute) }

// --- structuralTags / timelineNodes / renderOverviewCard ----------------

func TestStructuralTags(t *testing.T) {
	et := i18n.Spine(i18n.EN)

	t.Run("tool-intensive at threshold", func(t *testing.T) {
		tags := structuralTags(Metrics{ToolCallCount: toolIntensiveThreshold}, et)
		if !containsStr(tags, et.TagToolIntensive) {
			t.Errorf("tags = %v, want TagToolIntensive at ToolCallCount=%d", tags, toolIntensiveThreshold)
		}
	})
	t.Run("below tool-intensive threshold: no tag", func(t *testing.T) {
		tags := structuralTags(Metrics{ToolCallCount: toolIntensiveThreshold - 1}, et)
		if containsStr(tags, et.TagToolIntensive) {
			t.Errorf("tags = %v, unexpected TagToolIntensive below threshold", tags)
		}
	})
	t.Run("retry-heavy at threshold", func(t *testing.T) {
		tags := structuralTags(Metrics{DuplicateActionRate: retryHeavyThreshold}, et)
		if !containsStr(tags, et.TagRetryHeavy) {
			t.Errorf("tags = %v, want TagRetryHeavy at DuplicateActionRate=%v", tags, retryHeavyThreshold)
		}
	})
	t.Run("below retry-heavy threshold: no tag", func(t *testing.T) {
		tags := structuralTags(Metrics{DuplicateActionRate: retryHeavyThreshold - 0.01}, et)
		if containsStr(tags, et.TagRetryHeavy) {
			t.Errorf("tags = %v, unexpected TagRetryHeavy below threshold", tags)
		}
	})
	t.Run("compaction count > 0", func(t *testing.T) {
		tags := structuralTags(Metrics{CompactionCount: 1}, et)
		if !containsStr(tags, et.TagContextCompacted) {
			t.Errorf("tags = %v, want TagContextCompacted", tags)
		}
	})
	t.Run("zero-value Metrics: no tags", func(t *testing.T) {
		tags := structuralTags(Metrics{}, et)
		if len(tags) != 0 {
			t.Errorf("tags = %v, want none for zero-value Metrics", tags)
		}
	})
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestTimelineNodes(t *testing.T) {
	et := i18n.Spine(i18n.EN)

	t.Run("zero Steps: nil, no panic", func(t *testing.T) {
		j := journeyOfTasks() // no Tasks at all
		nodes := timelineNodes(j, et)
		if nodes != nil {
			t.Errorf("nodes = %v, want nil for a Journey with zero Steps", nodes)
		}
	})

	t.Run("single step: start and end only, no error/transition", func(t *testing.T) {
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)), Finish: "stop"}
		j := journeyOf(s)
		nodes := timelineNodes(j, et)
		if len(nodes) != 2 {
			t.Fatalf("nodes = %v, want exactly 2 (start, end)", nodes)
		}
		wantStart := et.OverviewStart(at(0).Format("15:04:05"))
		wantEnd := et.OverviewEnd(1, "stop", at(0).Format("15:04:05"))
		if nodes[0] != wantStart {
			t.Errorf("nodes[0] = %q, want %q", nodes[0], wantStart)
		}
		if nodes[1] != wantEnd {
			t.Errorf("nodes[1] = %q, want %q", nodes[1], wantEnd)
		}
	})

	t.Run("first error marker reported, only the first one", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Manifest: mkManifest(at(0))},
			{Seq: 2, Manifest: mkManifest(at(1)), NewEvents: []*Event{errorEvent()}},
			{Seq: 3, Manifest: mkManifest(at(2)), NewEvents: []*Event{errorEvent()}},
		}
		j := journeyOf(steps...)
		nodes := timelineNodes(j, et)
		wantErr := et.OverviewFirstError(2, at(1).Format("15:04:05"))
		wantLaterErr := et.OverviewFirstError(3, at(2).Format("15:04:05"))
		found := 0
		for _, n := range nodes {
			if n == wantErr {
				found++
			}
			if n == wantLaterErr {
				t.Errorf("timelineNodes reported the later (Step 3) error too: %v", nodes)
			}
		}
		if found != 1 {
			t.Errorf("nodes = %v, want exactly one first-error line %q", nodes, wantErr)
		}
	})

	t.Run("first non-Append transition reported via Edge", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Manifest: mkManifest(at(0))},
			{Seq: 2, Manifest: mkManifest(at(1)), Edge: &ctxgraph.Edit{Kind: ctxgraph.ReplaceTail}},
		}
		j := journeyOf(steps...)
		nodes := timelineNodes(j, et)
		want := et.OverviewTransition(2, ctxgraph.ReplaceTail.String(), at(1).Format("15:04:05"))
		if !containsStr(nodes, want) {
			t.Errorf("nodes = %v, want to contain %q", nodes, want)
		}
	})

	t.Run("Append edge is not a transition", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Manifest: mkManifest(at(0))},
			{Seq: 2, Manifest: mkManifest(at(1)), Edge: &ctxgraph.Edit{Kind: ctxgraph.Append}},
		}
		j := journeyOf(steps...)
		nodes := timelineNodes(j, et)
		for _, n := range nodes {
			if strings.Contains(n, "transition") {
				t.Errorf("nodes = %v, an Append edge must not be reported as a transition", nodes)
			}
		}
	})

	t.Run("StitchEdge transition takes priority and uses its own Kind", func(t *testing.T) {
		steps := []*Step{
			{Seq: 1, Manifest: mkManifest(at(0))},
			{Seq: 2, Manifest: mkManifest(at(1)), StitchEdge: &ctxgraph.StitchEdge{Kind: ctxgraph.StitchSameChat}},
		}
		j := journeyOf(steps...)
		nodes := timelineNodes(j, et)
		want := et.OverviewTransition(2, ctxgraph.StitchSameChat.String(), at(1).Format("15:04:05"))
		if !containsStr(nodes, want) {
			t.Errorf("nodes = %v, want to contain stitch transition %q", nodes, want)
		}
	})
}

func TestRenderOverviewCard(t *testing.T) {
	t.Run("zero Steps, zero-value Metrics: writes nothing, no panic", func(t *testing.T) {
		w, buf := capture()
		j := journeyOfTasks()
		renderOverviewCard(w, j, Metrics{}, i18n.EN)
		if buf.Len() != 0 {
			t.Errorf("expected no output, got %q", buf.String())
		}
	})

	t.Run("tags alone (no Steps) still render the card", func(t *testing.T) {
		w, buf := capture()
		j := journeyOfTasks()
		renderOverviewCard(w, j, Metrics{ToolCallCount: toolIntensiveThreshold}, i18n.EN)
		et := i18n.Spine(i18n.EN)
		out := buf.String()
		if !strings.Contains(out, et.OverviewTitle) {
			t.Errorf("output missing OverviewTitle: %q", out)
		}
		if !strings.Contains(out, et.TagToolIntensive) {
			t.Errorf("output missing TagToolIntensive: %q", out)
		}
	})

	t.Run("nodes and tags both render together", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)), Finish: "stop"}
		j := journeyOf(s)
		renderOverviewCard(w, j, Metrics{DuplicateActionRate: retryHeavyThreshold}, i18n.EN)
		et := i18n.Spine(i18n.EN)
		out := buf.String()
		if !strings.Contains(out, et.OverviewStart(at(0).Format("15:04:05"))) {
			t.Errorf("output missing start line: %q", out)
		}
		if !strings.Contains(out, et.TagRetryHeavy) {
			t.Errorf("output missing TagRetryHeavy: %q", out)
		}
	})
}

// --- renderDecisionSpine -------------------------------------------------

func TestRenderDecisionSpine(t *testing.T) {
	et := i18n.Spine(i18n.EN)

	t.Run("no tool calls anywhere: still renders the spine, with a report line (P1.2 coverage)", func(t *testing.T) {
		w, buf := capture()
		j := journeyOfTasks(
			&Task{Title: "t1", Steps: []*Step{{Seq: 1, Manifest: mkManifest(at(0)), RespText: "just talk"}}},
		)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if !strings.Contains(out, et.SpineTitle) {
			t.Errorf("expected the spine title even for a Journey with no tool calls, got %q", out)
		}
		if !strings.Contains(out, et.SpineTaskLine(1, "t1")) {
			t.Errorf("expected the Task line even with no tool calls, got %q", out)
		}
		if !strings.Contains(out, et.SpineReportLine("just talk")) {
			t.Errorf("expected a report one-liner for the no-tool-call Step, got %q", out)
		}
	})

	t.Run("mid-task instruction: HumanInitiated Step after the first gets an instruction line, not a report line", func(t *testing.T) {
		w, buf := capture()
		s1 := &Step{Seq: 1, Manifest: mkManifest(at(0)), HumanInitiated: true, RespText: "sure"}
		s2 := &Step{Seq: 2, Manifest: mkManifest(at(1)), HumanInitiated: true, Instruction: "actually also check b.go",
			NewEvents: []*Event{{Msg: chatmsg.Message{Role: "user", Text: "actually also check b.go"}}}}
		j := journeyOfTasks(&Task{Title: "t1", Steps: []*Step{s1, s2}})
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		// s1 is the Task's own opening Step — HumanInitiated is skipped there
		// (the Task title already carries the instruction), but it still gets
		// its own report line for RespText, same as any other no-tool-call Step.
		if !strings.Contains(out, et.SpineReportLine("sure")) {
			t.Errorf("expected the Task's opening Step to still get its report line, got %q", out)
		}
		// s2 is mid-task and HumanInitiated — it gets the instruction line
		// instead of (not in addition to) a report line, since it has no
		// RespText of its own.
		if !strings.Contains(out, et.SpineInstructionLine("actually also check b.go")) {
			t.Errorf("expected an instruction line for the mid-task HumanInitiated Step, got %q", out)
		}
	})

	t.Run("mid-task instruction that ALSO triggers a tool call still gets an instruction line", func(t *testing.T) {
		w, buf := capture()
		s1 := &Step{Seq: 1, Manifest: mkManifest(at(0)), HumanInitiated: true, RespText: "sure"}
		s2 := &Step{Seq: 2, Manifest: mkManifest(at(1)), HumanInitiated: true, Instruction: "actually also check b.go",
			ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"b.go"}`)}}
		j := journeyOfTasks(&Task{Title: "t1", Steps: []*Step{s1, s2}})
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		// s2 has a tool call, so it renders via renderSpineStep (not
		// renderSpineBriefStep) — the instruction line must still appear;
		// this is the case a mid-task instruction most commonly falls into
		// in real usage (an instruction almost always triggers a tool call).
		if !strings.Contains(out, et.SpineInstructionLine("actually also check b.go")) {
			t.Errorf("expected an instruction line for the mid-task, tool-calling Step, got %q", out)
		}
	})

	t.Run("a Step with nothing at all still renders its header, no fabricated content", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0))}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		want := "**" + stepRoleTag(s, false, et) + " Step 1 · " + at(0).Format("15:04:05") + "**"
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want the Step header %q even with nothing to summarize", out, want)
		}
		if strings.Contains(out, "💬") {
			t.Errorf("output = %q, must not fabricate an instruction/report line when the Step has none", out)
		}
	})

	t.Run("final deliverable section renders when the last write-shaped tool call is found", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)),
			ToolCalls: []chatmsg.ToolCall{tc("write_file", `{"path":"report.md","content":"final answer"}`)}}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if !strings.Contains(out, et.SpineFinalDeliverableTitle) {
			t.Errorf("output = %q, want the final deliverable title", out)
		}
		if !strings.Contains(out, "final answer") {
			t.Errorf("output = %q, want the deliverable excerpt", out)
		}
	})

	t.Run("no write-shaped tool call: no final deliverable section", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)), ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"x"}`)}}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if strings.Contains(out, et.SpineFinalDeliverableTitle) {
			t.Errorf("output = %q, must not render a final deliverable section when none was found", out)
		}
	})

	t.Run("one block per Step, grouped by Task, Finding marker only on the referenced Step's header", func(t *testing.T) {
		w, buf := capture()
		s1 := &Step{Seq: 1, Manifest: mkManifest(at(0)), ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a.go"}`)}}
		s2 := &Step{Seq: 2, Manifest: mkManifest(at(1)), ToolCalls: []chatmsg.ToolCall{tc("write", `{"path":"b.go"}`)}}
		task1 := &Task{Title: "first task", Steps: []*Step{s1}}
		task2 := &Task{Title: "second task", Steps: []*Step{s2}}
		j := journeyOfTasks(task1, task2)
		findings := []Finding{{Code: FindingExactRepeatToolCall, StepSeq: 2}}
		renderDecisionSpine(w, j, findings, i18n.EN)
		out := buf.String()

		if !strings.Contains(out, et.SpineTaskLine(1, "first task")) {
			t.Errorf("output missing task 1 header: %q", out)
		}
		if !strings.Contains(out, et.SpineTaskLine(2, "second task")) {
			t.Errorf("output missing task 2 header: %q", out)
		}
		if !strings.Contains(out, toolCallLine(tc("read", `{"path":"a.go"}`), et)) {
			t.Errorf("output missing the read tool call: %q", out)
		}
		if !strings.Contains(out, toolCallLine(tc("write", `{"path":"b.go"}`), et)) {
			t.Errorf("output missing the write tool call: %q", out)
		}

		step1Header := "**" + stepRoleTag(s1, false, et) + " Step 1 · " + at(0).Format("15:04:05") + "**"
		step2Header := "**" + stepRoleTag(s2, false, et) + " Step 2 · " + at(1).Format("15:04:05") + "**"
		if !strings.Contains(out, step1Header+"\n\n") {
			t.Errorf("output should contain the unmarked Step 1 header %q, got %q", step1Header, out)
		}
		if !strings.Contains(out, step2Header+et.SpineFindingTag) {
			t.Errorf("output should contain the Finding-marked Step 2 header %q, got %q", step2Header+et.SpineFindingTag, out)
		}
	})

	t.Run("RelatedSeq also earns the Finding marker, on the Step header", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 5, Manifest: mkManifest(at(0)), ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"x"}`)}}
		j := journeyOf(s)
		findings := []Finding{{Code: FindingUnverifiedSuccess, StepSeq: 99, RelatedSeq: []int{5}}}
		renderDecisionSpine(w, j, findings, i18n.EN)
		out := buf.String()
		want := "**" + stepRoleTag(s, false, et) + " Step 5 · " + at(0).Format("15:04:05") + "**" + et.SpineFindingTag
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain the RelatedSeq-marked header %q", out, want)
		}
	})

	t.Run("non-consecutive exact repeat: each Step still gets its own block, role-tagged as a repeat", func(t *testing.T) {
		w, buf := capture()
		s1 := &Step{Seq: 1, Manifest: mkManifest(at(0)), ToolCalls: []chatmsg.ToolCall{tc("exec", `{"command":"go build"}`)}}
		s2 := &Step{Seq: 2, Manifest: mkManifest(at(1)), ToolCalls: []chatmsg.ToolCall{tc("exec", `{"command":"go test"}`)}}
		s3 := &Step{Seq: 3, Manifest: mkManifest(at(2)), ToolCalls: []chatmsg.ToolCall{tc("exec", `{"command":"go build"}`)}} // repeats Step 1
		j := journeyOf(s1, s2, s3)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()

		n := strings.Count(out, "Step 1 ·") + strings.Count(out, "Step 2 ·") + strings.Count(out, "Step 3 ·")
		if n != 3 {
			t.Fatalf("expected exactly 3 separate Step blocks (not collapsed), got %d:\n%s", n, out)
		}
		normalHeader := "**" + et.StepTagAction + " Step 1 · " + at(0).Format("15:04:05") + "**"
		if !strings.Contains(out, normalHeader) {
			t.Errorf("output = %q, want Step 1 role-tagged as a normal action (%s)", out, et.StepTagAction)
		}
		repeatHeader := "**" + et.StepTagRetry + " Step 3 · " + at(2).Format("15:04:05") + "**"
		if !strings.Contains(out, repeatHeader) {
			t.Errorf("output = %q, want Step 3 role-tagged as a repeat (%s) of Step 1's identical call", out, et.StepTagRetry)
		}
	})

	t.Run("multiple tool calls in one Step all render under that Step's single header", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 3, Manifest: mkManifest(at(0)), ToolCalls: []chatmsg.ToolCall{
			tc("web_fetch", `{"url":"https://example.com"}`),
			tc("web_search", `{"query":"a"}`),
			tc("web_search", `{"query":"b"}`),
		}}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if strings.Count(out, "Step 3 ·") != 1 {
			t.Errorf("output = %q, want exactly one Step 3 header even though it made 3 tool calls", out)
		}
		if !strings.Contains(out, "`web_fetch`") {
			t.Errorf("output missing web_fetch call: %q", out)
		}
		if strings.Count(out, "`web_search`") != 2 {
			t.Errorf("output = %q, want both web_search calls to appear", out)
		}
	})

	t.Run("RespText renders as the why-line, in full", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)), RespText: "先试试东方财富接口", ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"x"}`)}}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if !strings.Contains(out, "> 先试试东方财富接口\n\n") {
			t.Errorf("output = %q, want the RespText why-line", out)
		}
	})

	t.Run("Reasoning renders as a 🤔-prefixed why-line only when RespText is empty", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)), Reasoning: "let me check the docs first", ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"x"}`)}}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if !strings.Contains(out, "> 🤔 let me check the docs first\n\n") {
			t.Errorf("output = %q, want the Reasoning why-line", out)
		}
	})

	t.Run("RespText wins over Reasoning when both are present", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)), RespText: "stated plan", Reasoning: "inner reasoning", ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"x"}`)}}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if !strings.Contains(out, "> stated plan\n\n") {
			t.Errorf("output = %q, want RespText's why-line", out)
		}
		if strings.Contains(out, "inner reasoning") {
			t.Errorf("output = %q, must not also show Reasoning when RespText is present", out)
		}
	})

	t.Run("neither RespText nor Reasoning: no why-line at all", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)), ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"x"}`)}}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if strings.Contains(out, ">") {
			t.Errorf("output = %q, must not invent a why-line when the Step said nothing", out)
		}
	})

	// The following lock in KNOWN_ISSUES §1.37/P12.2-3 for every raw-text
	// insertion point in the spine that isn't already covered by
	// TestToolCallLine (render_spine_args_test.go): task titles, mid-task
	// instructions, report lines and why-lines are all user/model-derived
	// text written straight into the document body — real corpus content
	// (an HTML comment header) has actually triggered silent content loss
	// through exactly this path.
	adversarial := "<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> real content after"

	t.Run("task title is escaped", func(t *testing.T) {
		w, buf := capture()
		j := journeyOfTasks(&Task{Title: adversarial, Steps: []*Step{{Seq: 1, Manifest: mkManifest(at(0)), RespText: "ok"}}})
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if strings.Contains(out, "<!--") {
			t.Errorf("task title leaked a raw HTML comment marker: %q", out)
		}
		if !strings.Contains(out, "&lt;!--") || !strings.Contains(out, "real content after") {
			t.Errorf("output = %q, want the task title escaped with its trailing content preserved", out)
		}
	})

	t.Run("mid-task instruction line is escaped", func(t *testing.T) {
		w, buf := capture()
		s1 := &Step{Seq: 1, Manifest: mkManifest(at(0)), HumanInitiated: true, RespText: "sure"}
		s2 := &Step{Seq: 2, Manifest: mkManifest(at(1)), HumanInitiated: true, Instruction: adversarial}
		j := journeyOfTasks(&Task{Title: "t1", Steps: []*Step{s1, s2}})
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if strings.Contains(out, "<!--") {
			t.Errorf("instruction line leaked a raw HTML comment marker: %q", out)
		}
		if !strings.Contains(out, "&lt;!--") {
			t.Errorf("output = %q, want the instruction escaped", out)
		}
	})

	t.Run("brief Step's report line (RespText) is escaped", func(t *testing.T) {
		w, buf := capture()
		j := journeyOfTasks(&Task{Title: "t1", Steps: []*Step{{Seq: 1, Manifest: mkManifest(at(0)), RespText: adversarial}}})
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if strings.Contains(out, "<!--") {
			t.Errorf("report line leaked a raw HTML comment marker: %q", out)
		}
		if !strings.Contains(out, "&lt;!--") {
			t.Errorf("output = %q, want the report line escaped", out)
		}
	})

	t.Run("why-line (RespText on a tool-calling Step) is escaped", func(t *testing.T) {
		w, buf := capture()
		s := &Step{Seq: 1, Manifest: mkManifest(at(0)), RespText: adversarial, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"x"}`)}}
		j := journeyOf(s)
		renderDecisionSpine(w, j, nil, i18n.EN)
		out := buf.String()
		if strings.Contains(out, "<!--") {
			t.Errorf("why-line leaked a raw HTML comment marker: %q", out)
		}
		if !strings.Contains(out, "&lt;!--") {
			t.Errorf("output = %q, want the why-line escaped", out)
		}
	})
}

// --- foldWhyLine / toolResultLine escaping (KNOWN_ISSUES §1.37/P12.2-3) ---

// TestFoldWhyLine_Escapes covers both of foldWhyLine's branches directly —
// inline (short enough to stay on one line) and folded (<details><summary>,
// over capLen) — against the real-corpus adversarial shape that used to
// get silently swallowed by a Markdown renderer.
func TestFoldWhyLine_Escapes(t *testing.T) {
	adversarial := "<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> keywords"

	t.Run("inline branch", func(t *testing.T) {
		got := foldWhyLine("> ", adversarial, spineWhyRespCap)
		if strings.Contains(got, "<!--") {
			t.Errorf("foldWhyLine(inline) leaked a raw HTML comment marker: %q", got)
		}
		if !strings.Contains(got, "&lt;!--") {
			t.Errorf("foldWhyLine(inline) = %q, want the escaped form", got)
		}
	})

	t.Run("folded branch: summary escaped, fenced full text left raw", func(t *testing.T) {
		long := adversarial + strings.Repeat(" filler", 100)
		got := foldWhyLine("> ", long, spineWhyReasoningCap)
		summaryEnd := strings.Index(got, "</summary>")
		if summaryEnd < 0 {
			t.Fatalf("foldWhyLine(folded) missing a <summary> block: %q", got)
		}
		if strings.Contains(got[:summaryEnd], "<!--") {
			t.Errorf("foldWhyLine(folded) summary leaked a raw HTML comment marker: %q", got[:summaryEnd])
		}
		if !strings.Contains(got[:summaryEnd], "&lt;!--") {
			t.Errorf("foldWhyLine(folded) summary = %q, want the escaped form", got[:summaryEnd])
		}
		if !strings.Contains(got[summaryEnd:], "<!-- Ver 2026-07-24 14:45, by Sonnet 5 -->") {
			t.Errorf("foldWhyLine(folded) fenced body should keep the raw, unescaped full text: %q", got[summaryEnd:])
		}
	})
}

// TestToolResultLine_EscapesSummary covers the one remaining <summary>
// injection point named in KNOWN_ISSUES §1.37 that isn't reachable through
// TestToolCallLine (which covers the call side, not the paired result).
func TestToolResultLine_EscapesSummary(t *testing.T) {
	et := i18n.Spine(i18n.EN)
	r := chatmsg.ToolResult{CallID: "1", Text: "<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> real content after"}
	got := toolResultLine("read", r, false, et)
	summaryEnd := strings.Index(got, "</summary>")
	if summaryEnd < 0 {
		t.Fatalf("toolResultLine missing a <summary> block: %q", got)
	}
	if strings.Contains(got[:summaryEnd], "<!--") {
		t.Errorf("toolResultLine summary leaked a raw HTML comment marker: %q", got[:summaryEnd])
	}
	if !strings.Contains(got[:summaryEnd], "&lt;!--") || !strings.Contains(got[:summaryEnd], "real content after") {
		t.Errorf("toolResultLine summary = %q, want the escaped form with the trailing content preserved", got[:summaryEnd])
	}
}

// --- stepRoleTag ----------------------------------------------------------

func TestStepRoleTag(t *testing.T) {
	et := i18n.Spine(i18n.EN)

	cases := []struct {
		name     string
		s        *Step
		isRepeat bool
		want     string
	}{
		{
			name: "compaction wins over everything, including an error marker and tool calls",
			s: &Step{
				StitchEdge: &ctxgraph.StitchEdge{Kind: ctxgraph.StitchSameChat},
				NewEvents:  []*Event{errorEvent()},
				ToolCalls:  []chatmsg.ToolCall{tc("bash", "{}")},
			},
			isRepeat: true,
			want:     et.StepTagCompaction,
		},
		{
			name: "error wins over retry when no compaction",
			s: &Step{
				NewEvents: []*Event{errorEvent()},
			},
			isRepeat: true,
			want:     et.StepTagError,
		},
		{
			name:     "retry wins over action when no compaction/error",
			s:        &Step{ToolCalls: []chatmsg.ToolCall{tc("bash", "{}")}},
			isRepeat: true,
			want:     et.StepTagRetry,
		},
		{
			name: "action: tool calls present, nothing else",
			s:    &Step{ToolCalls: []chatmsg.ToolCall{tc("bash", "{}")}},
			want: et.StepTagAction,
		},
		{
			name: "plan: no tool calls, numbered-list-shaped Reasoning",
			s:    &Step{Reasoning: "1. do a\n2. do b"},
			want: et.StepTagPlan,
		},
		{
			name: "plan: no tool calls, numbered-list-shaped RespText",
			s:    &Step{RespText: "1. do a\n2. do b"},
			want: et.StepTagPlan,
		},
		{
			name: "report: plain RespText, no list shape",
			s:    &Step{RespText: "here is a summary of what happened"},
			want: et.StepTagReport,
		},
		{
			name: "observe: nothing at all",
			s:    &Step{},
			want: et.StepTagObserve,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stepRoleTag(c.s, c.isRepeat, et)
			if got != c.want {
				t.Errorf("stepRoleTag() = %q, want %q", got, c.want)
			}
		})
	}
}

// --- renderToolTimeline ---------------------------------------------------

func TestRenderToolTimeline(t *testing.T) {
	et := i18n.Spine(i18n.EN)

	t.Run("zero Steps: TimelineNoData, not a malformed grid", func(t *testing.T) {
		w, buf := capture()
		j := journeyOfTasks()
		renderToolTimeline(w, j, i18n.EN)
		out := buf.String()
		if !strings.Contains(out, et.TimelineTitle) || !strings.Contains(out, et.TimelineNoData) {
			t.Errorf("output = %q, want Title+NoData for zero Steps", out)
		}
		if strings.Contains(out, et.TimelineLegend) {
			t.Errorf("output = %q, must not print the legend/grid when there are no Steps", out)
		}
	})

	t.Run("Steps with no tool calls at all: TimelineNoData", func(t *testing.T) {
		w, buf := capture()
		j := journeyOf(&Step{Seq: 1, Manifest: mkManifest(at(0)), RespText: "hi"})
		renderToolTimeline(w, j, i18n.EN)
		out := buf.String()
		if !strings.Contains(out, et.TimelineNoData) {
			t.Errorf("output = %q, want TimelineNoData when no Step has a tool call", out)
		}
	})

	t.Run("grid rows/columns, repeat and error symbols", func(t *testing.T) {
		w, buf := capture()
		steps := []*Step{
			{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"a"}`)}},
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"a"}`)}}, // exact repeat of step 1
			{Seq: 3, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"x"}`)},
				NewEvents: []*Event{errorEvent()}}, // read + error marker on the same Step
		}
		j := journeyOf(steps...)
		renderToolTimeline(w, j, i18n.EN)
		out := buf.String()

		if !strings.Contains(out, et.TimelineLegend) {
			t.Errorf("output missing legend: %q", out)
		}
		// "bash" and "read" are both 4 runes, so padRight is a no-op — the
		// exact row text is predictable: one column per Step, in Seq order.
		if !strings.Contains(out, "bash ●🔄·") {
			t.Errorf("output missing expected bash row (normal, repeat, absent): %q", out)
		}
		if !strings.Contains(out, "read ··❌") {
			t.Errorf("output missing expected read row (absent, absent, error): %q", out)
		}
	})

	t.Run("repeat symbol without an error marker on that Step stays 🔄, not ❌", func(t *testing.T) {
		w, buf := capture()
		steps := []*Step{
			{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"a"}`)}},
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"a"}`)}},
		}
		j := journeyOf(steps...)
		renderToolTimeline(w, j, i18n.EN)
		out := buf.String()
		if !strings.Contains(out, "bash ●🔄") {
			t.Errorf("output = %q, want a plain repeat (🔄) row with no error", out)
		}
		// The legend line itself always mentions ❌ ("❌ step carries an
		// error marker") regardless of whether any Step actually has one —
		// only the grid row (inside the fenced block) must stay clean.
		gridStart := strings.Index(out, "```")
		if gridStart < 0 {
			t.Fatalf("output missing the fenced grid block: %q", out)
		}
		grid := out[gridStart:]
		if strings.Contains(grid, "❌") {
			t.Errorf("grid = %q, must not print ❌ when no Step carries an error marker", grid)
		}
	})
}

// --- positionalToolResults (P1.1 level-3 pairing fallback) -----------------

// TestPositionalToolResults covers the render-only third pairing level:
// self-generated ids (e.g. cliproxy:gemini's "exec<epoch-micros>" shape —
// architecture doc §5.3) that neither exact nor normalized id matching can
// ever recover, paired by position ONLY when the unresolved-call count
// equals the unclaimed-result count.
func TestPositionalToolResults(t *testing.T) {
	nextStepBody := map[string]any{"messages": []any{
		map[string]any{"role": "tool", "tool_call_id": "exec1786691703864731", "content": "result A"},
		map[string]any{"role": "tool", "tool_call_id": "exec1786691703864999", "content": "result B"},
	}}
	nextStep := &Step{Rec: &audit.Record{Client: audit.Exchange{Request: audit.Message{Body: nextStepBody}}}}

	t.Run("same-count leftover pairs by position, in order", func(t *testing.T) {
		tc1 := chatmsg.ToolCall{ID: "call_self_made_1", Name: "bash"}
		tc2 := chatmsg.ToolCall{ID: "call_self_made_2", Name: "bash"}
		s := &Step{ToolCalls: []chatmsg.ToolCall{tc1, tc2}}
		steps := []*Step{s, nextStep}
		got := positionalToolResults(steps, 0, map[string]chatmsg.ToolResult{})
		if len(got) != 2 {
			t.Fatalf("got %d positional matches, want 2: %+v", len(got), got)
		}
		if got[tc1.ID].Text != "result A" {
			t.Errorf("got[%q].Text = %q, want %q", tc1.ID, got[tc1.ID].Text, "result A")
		}
		if got[tc2.ID].Text != "result B" {
			t.Errorf("got[%q].Text = %q, want %q", tc2.ID, got[tc2.ID].Text, "result B")
		}
	})

	t.Run("count mismatch: no guess, returns nil", func(t *testing.T) {
		tc1 := chatmsg.ToolCall{ID: "call_only_one", Name: "bash"}
		s := &Step{ToolCalls: []chatmsg.ToolCall{tc1}}
		steps := []*Step{s, nextStep} // nextStep still has 2 unclaimed results
		if got := positionalToolResults(steps, 0, map[string]chatmsg.ToolResult{}); got != nil {
			t.Errorf("got %v, want nil on a 1-call/2-result count mismatch", got)
		}
	})

	t.Run("already resolved via byID: nothing left to guess, returns nil", func(t *testing.T) {
		tc1 := chatmsg.ToolCall{ID: "call_x", Name: "bash"}
		s := &Step{ToolCalls: []chatmsg.ToolCall{tc1}}
		byID := map[string]chatmsg.ToolResult{"call_x": {CallID: "call_x", Text: "already matched"}}
		steps := []*Step{s, nextStep}
		if got := positionalToolResults(steps, 0, byID); got != nil {
			t.Errorf("got %v, want nil — no unresolved calls left to pair", got)
		}
	})

	t.Run("last Step (no following request): returns nil, no panic", func(t *testing.T) {
		tc1 := chatmsg.ToolCall{ID: "call_x", Name: "bash"}
		s := &Step{ToolCalls: []chatmsg.ToolCall{tc1}}
		steps := []*Step{s}
		if got := positionalToolResults(steps, 0, map[string]chatmsg.ToolResult{}); got != nil {
			t.Errorf("got %v, want nil for the Journey's last Step", got)
		}
	})

	// TestPositionalToolResults_ScopedToDelta covers a real bug caught in
	// review before this shipped: every chat API resends the FULL
	// conversation on each turn, so steps[i+1]'s request body contains not
	// just its own new messages but every earlier Step's tool results too.
	// An unscoped scan would count an unrelated, already-resolved HISTORICAL
	// result as "leftover" for the CURRENT Step's unresolved call — and if
	// the counts happen to coincide, silently attribute that stale result
	// to the wrong call. positionalToolResults must bound its scan to
	// steps[i+1].DeltaStart onward (the messages THAT step actually
	// introduced), not the whole cumulative body.
	t.Run("does not attribute an earlier Step's already-resolved historical result to this Step's unresolved call", func(t *testing.T) {
		s1 := &Step{ToolCalls: []chatmsg.ToolCall{{ID: "self_made_x", Name: "bash"}}}
		// steps[2].Rec is the cumulative request for the step AFTER s1 —
		// index 0 is Step 0's own (already-resolved-elsewhere) historical
		// tool result, index 1 is genuinely new content Step 1 introduced
		// (no tool result at all — the client dropped answering
		// self_made_x). DeltaStart=1 says "new content starts at index 1".
		polluted := &Step{
			DeltaStart: 1,
			Rec: &audit.Record{Client: audit.Exchange{Request: audit.Message{Body: map[string]any{
				"messages": []any{
					map[string]any{"role": "tool", "tool_call_id": "call_real_1", "content": "real answer from an earlier Step"},
					map[string]any{"role": "user", "content": "continue"},
				},
			}}}},
		}
		steps := []*Step{{}, s1, polluted}
		got := positionalToolResults(steps, 1, map[string]chatmsg.ToolResult{})
		if got != nil {
			t.Errorf("got %v, want nil — the only leftover entry is HISTORICAL (outside the delta region), must not be guessed as self_made_x's answer", got)
		}
	})

	t.Run("still finds a genuinely new leftover result within the delta region, ignoring older history before it", func(t *testing.T) {
		s1 := &Step{ToolCalls: []chatmsg.ToolCall{{ID: "self_made_2", Name: "bash"}}}
		nextStep := &Step{
			DeltaStart: 2, // new content (this Step's own answer) starts at index 2
			Rec: &audit.Record{Client: audit.Exchange{Request: audit.Message{Body: map[string]any{
				"messages": []any{
					map[string]any{"role": "tool", "tool_call_id": "old_hist_result", "content": "stale historical result"},
					map[string]any{"role": "assistant", "content": ""},
					map[string]any{"role": "tool", "tool_call_id": "exec_new_result_id", "content": "fresh step-2 result"},
				},
			}}}},
		}
		steps := []*Step{{}, s1, nextStep}
		got := positionalToolResults(steps, 1, map[string]chatmsg.ToolResult{})
		if len(got) != 1 {
			t.Fatalf("got %d matches, want exactly 1: %+v", len(got), got)
		}
		if got["self_made_2"].Text != "fresh step-2 result" {
			t.Errorf("got %+v, want self_made_2 paired with the fresh in-delta result, not the stale historical one", got)
		}
	})
}

// --- padRight ---------------------------------------------------------------

func TestPadRight(t *testing.T) {
	t.Run("pads a short string to n", func(t *testing.T) {
		got := padRight("ab", 5)
		if got != "ab   " {
			t.Errorf("padRight(\"ab\", 5) = %q, want %q", got, "ab   ")
		}
	})
	t.Run("no-op when already >= n runes", func(t *testing.T) {
		if got := padRight("abcdef", 4); got != "abcdef" {
			t.Errorf("padRight(\"abcdef\", 4) = %q, want unchanged", got)
		}
		if got := padRight("abcd", 4); got != "abcd" {
			t.Errorf("padRight(\"abcd\", 4) = %q, want unchanged", got)
		}
	})
}
