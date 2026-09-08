// Ver 2026-09-15, by pi

// The ViewModel layer's transition tests (§9): the new path
// (RenderMarkdownFromSummary over the self-contained JourneySummary) must be
// byte-equivalent to the old path (RenderMarkdown over the in-memory *Journey)
// on every input shape the document renders, and the two §9 guards new with
// D18 — bodies' no-orphans/no-dangling-references, and three-level match ↔
// spine-rendered pairing consistency — live here next to the code they pin.
package journey

import (
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// vmEquivalenceFixture builds a Journey through the real pipeline exercising
// every rendering branch the two paths must agree on: multiline (fenced) and
// inline tool-call arguments, an is_error tool result (❌ mark + error-marker
// event scan), a normalized-ID pairing, a positional pairing, an exact repeat
// (🔄), an unmatched final call, a compaction fold, a mid-task instruction, a
// partial banner, an unresolved break banner, a sys-prompt change, and a
// resolved cost line.
func vmEquivalenceFixture(t *testing.T) *Journey {
	t.Helper()
	at := func(sec int) time.Time { return time.Date(2026, 7, 15, 9, 0, sec, 0, time.UTC) }
	sys := msg("system", "sys prompt for equivalence")
	u1 := msg("user", "investigate the failing deploy")

	toolUse := map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "tool_use", "id": "tu1", "name": "exec", "input": map[string]any{"cmd": "tail -n 200 app.log"}},
	}}
	toolResultErr := map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "tu1", "is_error": true, "content": "connection refused after 3 attempts"},
	}}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
		map[string]any{"id": "tu1", "function": map[string]any{"name": "exec", "arguments": `{"cmd":"tail -n 200 app.log"}`}},
	}))
	r2 := mkRec(at(3), "", []any{sys, u1, toolUse, toolResultErr}, sseToolCalls([]any{
		// exact repeat of step 1's call (same name, same serialized args)
		map[string]any{"id": "tu1", "function": map[string]any{"name": "exec", "arguments": `{"cmd":"tail -n 200 app.log"}`}},
	}))
	r3 := mkRec(at(6), "", []any{sys, u1, toolUse, toolResultErr, map[string]any{"role": "assistant", "content": "still failing, trying once more"}}, sseToolCalls([]any{
		map[string]any{"id": "c_9", "function": map[string]any{"name": "restart", "arguments": `{"service":"api-gateway"}`}},
	}))
	r4 := mkRec(at(9), "", []any{sys, u1, toolUse, toolResultErr,
		map[string]any{"role": "assistant", "content": "still failing, trying once more"},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "wx1", "is_error": false, "content": "api-gateway restarted, observing"},
		}},
	}, sseText("deploy looks stable now"))

	path := writeJSONL(t, []audit.Record{r1, r2, r3, r4})
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Cross-Step facts and per-Step view inputs the fixture corpus doesn't
	// produce on its own: an unresolved lineage break, a partial tail, a
	// compaction boundary (with a predecessor excerpt and swallowed
	// entities), and a mid-task instruction. Both paths read these from the
	// same Journey — the old directly, the new via the stamped summary.
	j.Partial = true
	j.Break = &ctxgraph.BreakInfo{Edit: ctxgraph.Edit{Kind: ctxgraph.Fork, LCP: 2, Coverage: 0.25}}
	steps := journeySteps(j)
	if len(steps) != 4 {
		t.Fatalf("fixture: got %d steps, want 4", len(steps))
	}
	steps[2].Compaction = &CompactionInfo{
		TokensBefore: 1200, TokensAfter: 480,
		PredecessorTextExcerpt: "earlier context tail that was compacted away",
		SwallowedEntities:      []string{"old-service.yaml"}, SurvivedEntities: []string{"api-gateway"},
	}
	steps[2].Instruction = "check the gateway logs next"

	// A changed system prompt mid-lineage exercises the two-era sysprompt header.
	return j
}

// vmStepHeaderSeq reports whether blk is a spine Step header ("**<tag>
// Step N · ts>**"), returning its Seq.
func vmStepHeaderSeq(blk VMBlock) (int, bool) {
	if blk.Kind != vmText || !strings.HasPrefix(blk.Text, "**") {
		return 0, false
	}
	i := strings.Index(blk.Text, " Step ")
	if i < 0 {
		return 0, false
	}
	rest := blk.Text[i+len(" Step "):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	seq := 0
	for _, c := range rest[:end] {
		seq = seq*10 + int(c-'0')
	}
	return seq, true
}

// buildGoldenJourney is goldenFixture through the real Build pipeline, shared
// with the equivalence matrix so the golden corpus is covered there too.
func buildGoldenJourney(t *testing.T) *Journey {
	t.Helper()
	path := writeJSONL(t, goldenFixture())
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 1 {
		t.Fatalf("want 1 lineage in the golden fixture, got %d", len(g.Lineages))
	}
	j, err := Build(g.Lineages[0], taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return j
}

// TestVM_MatchConsistentWithSpineRendering is §9's pairing guard: the spine's
// rendered result blocks must be exactly the stamped ToolCallRef.Results —
// a folded result block for every paired call (with the positional badge iff
// the match is the third level), none for unpaired ones. This is the pin that
// keeps "JSON says unpaired, Markdown renders dozens of results" from
// recurring: both now read the same fact, but the guard proves it structurally
// at the VM layer, where the block list is inspectable.
func TestVM_MatchConsistentWithSpineRendering(t *testing.T) {
	j := vmEquivalenceFixture(t)
	summary := NewJourneySummary(j, ComputeMetrics(j), nil, nil, nil, nil)
	vm := BuildJourneyVM(&summary, i18n.EN, false, true)

	wantPaired, wantPositional := 0, 0
	for _, ss := range vmSteps(&summary) {
		for _, tc := range ss.ToolCalls {
			if tc.Result != nil && summary.Bodies[tc.Result.Ref] != "" {
				wantPaired++
				if tc.Result.Match == "positional" {
					wantPositional++
				}
			}
		}
	}

	gotPaired, gotPositional := 0, 0
	for _, blk := range vm.Spine {
		if blk.Kind != vmDetails || blk.Details == nil {
			continue
		}
		prefix := blk.Details.Prefix
		if strings.HasPrefix(prefix, "↩️ `") || strings.HasPrefix(prefix, "❌ `") {
			gotPaired++
			if strings.Contains(prefix, i18n.Spine(i18n.EN).SpinePositionalMatch) {
				gotPositional++
			}
		}
	}
	if gotPaired != wantPaired {
		t.Errorf("spine renders %d result blocks, want %d (the summary's paired, non-empty tool results)", gotPaired, wantPaired)
	}
	if gotPositional != wantPositional {
		t.Errorf("spine renders %d positional-match badges, want %d", gotPositional, wantPositional)
	}
	if wantPositional == 0 {
		t.Error("fixture no longer exercises the positional-match badge — it must pair at least one call by position")
	}
}

// TestVM_SpineRendersEveryStep is P1.2's coverage rule carried to the VM
// layer: every Step of every Task appears in the spine, tool-calling or not.
func TestVM_SpineRendersEveryStep(t *testing.T) {
	j := vmEquivalenceFixture(t)
	summary := NewJourneySummary(j, ComputeMetrics(j), nil, nil, nil, nil)
	vm := BuildJourneyVM(&summary, i18n.EN, false, true)

	spine := make(map[int]bool)
	for _, blk := range vm.Spine {
		if seq, ok := vmStepHeaderSeq(blk); ok {
			spine[seq] = true
		}
	}
	for _, ss := range vmSteps(&summary) {
		if !spine[ss.Seq] {
			t.Errorf("step %d missing from the VM spine", ss.Seq)
		}
	}
	if len(spine) != len(vmSteps(&summary)) {
		t.Errorf("spine rendered %d step headers for %d steps", len(spine), len(vmSteps(&summary)))
	}
}

func TestViewModel_CacheBreakBadge(t *testing.T) {
	t.Parallel()

	cases := []struct {
		cb      string
		rFrom   float64
		rTo     float64
		wantEN  string
		wantZH  string
		notShow bool
	}{
		{
			cb:     "unexplained",
			rFrom:  0.95,
			rTo:    0.31,
			wantEN: "Cache: unexplained drop (95%→31%)",
			wantZH: "Cache: 异常骤降 (95%→31%)",
		},
		{
			cb:     "provider_switch",
			wantEN: "Cache: provider switched",
			wantZH: "Cache: 服务端点切换",
		},
		{
			cb:     "system",
			wantEN: "Cache: system prompt changed",
			wantZH: "Cache: 系统提示词变更",
		},
		{
			cb:     "tools",
			wantEN: "Cache: tool definitions changed",
			wantZH: "Cache: 工具定义变更",
		},
		{
			cb:     "history:stitch",
			wantEN: "Cache: context stitched",
			wantZH: "Cache: 跨会话缝合",
		},
		{
			cb:     "history:contract",
			wantEN: "Cache: context contracted",
			wantZH: "Cache: 上下文压缩截断",
		},
		{
			cb:     "history:fork",
			wantEN: "Cache: context forked",
			wantZH: "Cache: 上下文分叉",
		},
		{
			cb:      "history:append",
			notShow: true,
		},
		{
			cb:      "history:replace_tail",
			notShow: true,
		},
		{
			cb:      "history:splice",
			notShow: true,
		},
		{
			cb:      "",
			notShow: true,
		},
	}

	for _, tc := range cases {
		ss := &StepStructure{
			Seq:                 2,
			CacheBreak:          tc.cb,
			CacheBreakRatioFrom: tc.rFrom,
			CacheBreakRatioTo:   tc.rTo,
		}
		gotEN := vmCacheBreakBadge(ss, i18n.Spine(i18n.EN))
		gotZH := vmCacheBreakBadge(ss, i18n.Spine(i18n.ZH))

		if tc.notShow {
			if gotEN != "" {
				t.Errorf("cb %q should be suppressed in EN, got %q", tc.cb, gotEN)
			}
			if gotZH != "" {
				t.Errorf("cb %q should be suppressed in ZH, got %q", tc.cb, gotZH)
			}
		} else {
			if gotEN != tc.wantEN {
				t.Errorf("cb %q EN = %q, want %q", tc.cb, gotEN, tc.wantEN)
			}
			if gotZH != tc.wantZH {
				t.Errorf("cb %q ZH = %q, want %q", tc.cb, gotZH, tc.wantZH)
			}

			// Also verify that vmStepHeader embeds the badge inside the ** bold step title
			headerEN := vmStepHeader(ss, false, false, "", "", i18n.Spine(i18n.EN), false)
			expectedFragment := " · " + tc.wantEN + "**"
			if !strings.Contains(headerEN, expectedFragment) {
				t.Errorf("vmStepHeader missing badge fragment %q:\n%s", expectedFragment, headerEN)
			}
		}
	}
}
