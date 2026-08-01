// Ver 2026-07-29 18:15, by Sonnet 5

package story

import (
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/story/profile"
)

func TestRenderMarkdown_BasicStructure(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "调研一下 A 股新股打新收益")
	a1 := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
		map[string]any{"id": "c1", "function": map[string]any{"name": "web_search", "arguments": "{}"}},
	}}
	t1 := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "search results here"}

	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("开工"))
	r2 := mkRec(at(1), "", []any{sys, u1, a1, t1}, sseText("完成"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, profile.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	md := RenderMarkdown(j, i18n.EN)

	for _, want := range []string{
		"# Journey j-",
		"调研一下 A 股新股打新收益",
		"t01 ·",
		"Step 1 ·",
		"Step 2 ·",
		"web_search",
		"finish: `stop`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered Markdown missing %q\n--- full output ---\n%s", want, md)
		}
	}
}

// TestRenderMarkdown_LLMResponseSection locks in the Messages/LLM Response
// split (design-doc review follow-up: a real reader found the old renderer
// collapsed a tool-calling step into a bare "🔧 调用工具: read, read" — no
// arguments, no ids, no reasoning, and even that much only showed up one
// step later once the reply became history). The response's reasoning and
// each tool call's full id+arguments (pretty-printed, "json"-tagged) must
// show up in the step that actually produced them, under "**LLM
// Response**" — separate from "**Messages**", which stays the delta of
// what's newly entering context.
func TestRenderMarkdown_LLMResponseSection(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "read both files and compare")
	resp := map[string]any{
		"model": "agent",
		"choices": []any{map[string]any{
			"finish_reason": "tool_calls",
			"message": map[string]any{
				"role":              "assistant",
				"content":           "",
				"reasoning_content": "I should read both files first.",
				"tool_calls": []any{
					map[string]any{"id": "call_1", "function": map[string]any{"name": "read", "arguments": `{"path":"/a.md"}`}},
					map[string]any{"id": "call_2", "function": map[string]any{"name": "read", "arguments": `{"path":"/b.md"}`}},
				},
			},
		}},
	}
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("ok"))
	r2 := mkRec(at(1), "", []any{sys, u1, msg("assistant", "ack")}, resp)

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, profile.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	md := RenderMarkdown(j, i18n.EN)

	for _, want := range []string{
		"**Messages**",
		"**LLM Response**",
		"🤔 reasoning · ",
		"I should read both files first.",
		"finish: tool_calls (read, read)",
		"🔧 **tool_call** `read` [id=call_1]",
		"```json",
		`"path": "/a.md"`,
		"🔧 **tool_call** `read` [id=call_2]",
		`"path": "/b.md"`,
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered Markdown missing %q\n--- full output ---\n%s", want, md)
		}
	}
}

// TestRenderMarkdown_EmbeddedBackticksDontBreakTheFence locks in a fix: a
// fixed ``` fence around event content breaks the moment the content itself
// contains a triple-backtick (an agent quoting code, a tool result
// containing a Markdown snippet) — the embedded backticks close the fence
// early and corrupt everything rendered after it. codeFence's fence must be
// longer than any backtick run actually present in the content.
func TestRenderMarkdown_EmbeddedBackticksDontBreakTheFence(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	codeBlock := "here's the file:\n```python\nprint('hi')\n```\ndone"
	u1 := msg("user", codeBlock)
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("ok"))
	r2 := mkRec(at(1), "", []any{sys, u1, msg("assistant", "reply")}, sseText("ok2"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, profile.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	md := RenderMarkdown(j, i18n.EN)
	if !strings.Contains(md, "````\n") {
		t.Errorf("expected a 4-backtick fence to safely wrap content containing a 3-backtick run:\n%s", md)
	}
	if !strings.Contains(md, codeBlock) {
		t.Errorf("embedded code block content should survive verbatim inside the wider fence:\n%s", md)
	}
}

func TestRenderMarkdown_BreakWarning(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 16, 15, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "深入调研内存涨价")
	msgs := []any{sys, u1}
	var recs []audit.Record
	for i := 0; i < 8; i++ {
		recs = append(recs, mkRec(at(i), "", append([]any{}, msgs...), sseText("ok")))
		msgs = append(msgs, msg("assistant", "step"))
	}
	// Contract: history collapses to just [sys2, u1] — same opening
	// instruction, drastically shorter.
	recs = append(recs, mkRec(at(30), "", []any{msg("system", "sys v2"), u1}, sseText("continuing")))

	path := writeJSONL(t, recs)
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2", len(g.Lineages))
	}
	second := g.Lineages[1]
	if second.BrokeFrom == nil {
		t.Fatal("second lineage should have BrokeFrom set")
	}
	j, err := Build(second, profile.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	md := RenderMarkdown(j, i18n.EN)
	if !strings.Contains(md, "⚠️") || !strings.Contains(md, "context was sharply contracted") {
		t.Errorf("rendered Markdown missing break warning:\n%s", md)
	}
}

func TestRenderMarkdown_BreakWarning_Fork(t *testing.T) {
	at := func(min int) time.Time { return time.Date(2026, 7, 16, 15, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "shared opening instruction")
	// Lineage 1: grows via appends (3 manifests, each with new content).
	msgs := []any{sys, u1}
	var recs []audit.Record
	for i := 0; i < 3; i++ {
		recs = append(recs, mkRec(at(i), "", append([]any{}, msgs...), sseText("ok")))
		msgs = append(msgs, msg("assistant", "step"+string(rune('a'+i))))
	}
	// Lineage 2: same anchor (u1 survives), but drastically different content
	// after it — low coverage against lineage 1's last manifest → Fork.
	recs = append(recs, mkRec(at(30), "",
		[]any{msg("system", "sys"), u1,
			msg("user", "brand new sub-task"), msg("assistant", "ack"), msg("user", "yet another")},
		sseText("branching off")))

	path := writeJSONL(t, recs)
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2", len(g.Lineages))
	}
	second := g.Lineages[1]
	if second.BrokeFrom == nil {
		t.Fatal("second lineage should have BrokeFrom set")
	}
	if second.BrokeFrom.Edit.Kind != ctxgraph.Fork {
		t.Fatalf("break edit kind = %v, want Fork", second.BrokeFrom.Edit.Kind)
	}
	j, err := Build(second, profile.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	md := RenderMarkdown(j, i18n.EN)
	if !strings.Contains(md, "content barely overlaps with the previous segment") {
		t.Errorf("rendered Markdown missing Fork warning:\n%s", md)
	}
}
