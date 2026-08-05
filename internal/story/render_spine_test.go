// Ver 2026-08-05, by Sonnet 5

// Direct unit tests for render_spine.go's decision-spine layer
// (renderOverviewCard/timelineNodes/structuralTags, renderDecisionSpine,
// stepRoleTag, renderToolTimeline, argPreview/padRight) — previously only
// exercised indirectly via golden_test.go's no-tool-call, no-Finding
// fixture, which never touched any of the populated code paths. Fixtures
// reuse this package's existing helpers: journeyOf/journeyOfTasks/tc from
// compare_test.go/findings_test.go, not a new fixture style.
package story

import (
	"strings"
	"testing"
	"time"

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

	t.Run("no tool calls anywhere: renders nothing", func(t *testing.T) {
		w, buf := capture()
		j := journeyOfTasks(
			&Task{Title: "t1", Steps: []*Step{{Seq: 1, RespText: "just talk"}}},
		)
		renderDecisionSpine(w, j, nil, i18n.EN)
		if buf.Len() != 0 {
			t.Errorf("expected no output for a Journey with no tool calls, got %q", buf.String())
		}
	})

	t.Run("one line per tool_call, grouped by Task, Finding marker only on the referenced Step", func(t *testing.T) {
		w, buf := capture()
		task1 := &Task{Title: "first task", Steps: []*Step{
			{Seq: 1, ToolCalls: []chatmsg.ToolCall{tc("read", `{"path":"a.go"}`)}},
		}}
		task2 := &Task{Title: "second task", Steps: []*Step{
			{Seq: 2, ToolCalls: []chatmsg.ToolCall{tc("write", `{"path":"b.go"}`)}},
		}}
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

		readLine := et.SpineActionLine("read", argPreview(`{"path":"a.go"}`))
		writeLine := et.SpineActionLine("write", argPreview(`{"path":"b.go"}`))
		if !strings.Contains(out, readLine) || strings.Contains(out, readLine+et.SpineFindingTag) {
			t.Errorf("output should contain the unmarked read line %q, got %q", readLine, out)
		}
		if !strings.Contains(out, writeLine+et.SpineFindingTag) {
			t.Errorf("output should contain the Finding-marked write line %q, got %q", writeLine+et.SpineFindingTag, out)
		}
	})

	t.Run("RelatedSeq also earns the Finding marker", func(t *testing.T) {
		w, buf := capture()
		j := journeyOf(&Step{Seq: 5, ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"x"}`)}})
		findings := []Finding{{Code: FindingUnverifiedSuccess, StepSeq: 99, RelatedSeq: []int{5}}}
		renderDecisionSpine(w, j, findings, i18n.EN)
		out := buf.String()
		want := et.SpineActionLine("bash", argPreview(`{"cmd":"x"}`)) + et.SpineFindingTag
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain RelatedSeq-marked line %q", out, want)
		}
	})
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

// --- argPreview / padRight -------------------------------------------------

func TestArgPreview(t *testing.T) {
	t.Run("short input: unchanged", func(t *testing.T) {
		in := "short args"
		if got := argPreview(in); got != in {
			t.Errorf("argPreview(%q) = %q, want unchanged", in, got)
		}
	})

	t.Run("collapses internal whitespace/newlines", func(t *testing.T) {
		got := argPreview("a\n  b\tc")
		if got != "a b c" {
			t.Errorf("argPreview whitespace collapse = %q, want %q", got, "a b c")
		}
	})

	t.Run("truncates at spineArgPreviewLen runes with an ellipsis", func(t *testing.T) {
		long := strings.Repeat("x", spineArgPreviewLen+10)
		got := argPreview(long)
		wantPrefix := strings.Repeat("x", spineArgPreviewLen)
		if got != wantPrefix+"…" {
			t.Errorf("argPreview(long) = %q, want %d x's + ellipsis", got, spineArgPreviewLen)
		}
		if len([]rune(got)) != spineArgPreviewLen+1 {
			t.Errorf("argPreview(long) rune length = %d, want %d", len([]rune(got)), spineArgPreviewLen+1)
		}
	})

	t.Run("exactly at the limit: no truncation", func(t *testing.T) {
		exact := strings.Repeat("y", spineArgPreviewLen)
		if got := argPreview(exact); got != exact {
			t.Errorf("argPreview(exact-length) = %q, want unchanged %q", got, exact)
		}
	})
}

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
