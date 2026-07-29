// Ver 2026-07-29 23:45, by Sonnet 5

// Tests for T2.3 (design doc Appendix C.4): system-change marking,
// compaction information-loss metrics, and the F11 "revision" relation.
package story

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/story/profile"
)

// mkRecWithUsage is mkRec plus an embedded usage chunk in the SSE stream,
// for tests that need to control the recorded prompt_tokens precisely
// (CompactionInfo reads Usage.In straight from it).
func mkRecWithUsage(ts time.Time, msgsList []any, respText string, promptTokens, completionTokens int64) audit.Record {
	body := map[string]any{"model": "agent", "stream": true, "messages": msgsList}
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"` + respText + `"}}],"model":"agent"}
data: {"choices":[{"index":0,"finish_reason":"stop","delta":{}}],"usage":{"prompt_tokens":` +
		strconv.FormatInt(promptTokens, 10) + `,"completion_tokens":` + strconv.FormatInt(completionTokens, 10) + `}}
data: [DONE]`
	return audit.Record{
		TS: ts, DurMS: 100, Model: "agent", Protocol: "openai", Stream: true, Outcome: "ok",
		Client: audit.Exchange{
			Request:  audit.Message{Method: "POST", Path: "/v1/chat/completions", Headers: map[string][]string{}, Body: body},
			Response: &audit.Message{Status: 200, Headers: map[string][]string{}, Body: sse},
		},
	}
}

// TestSysChanged_WithinLineage covers the plain (non-stitched) case: two
// consecutive manifests in the same lineage with different system prompts.
func TestSysChanged_WithinLineage(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 9, 10, m, 0, 0, time.UTC) }
	u1 := msg("user", "keep going")
	r1 := mkRec(at(0), "", []any{msg("system", "sys v1"), u1}, sseText("ok"))
	r2 := mkRec(at(1), "", []any{msg("system", "sys v2 — tool set changed"), u1, msg("assistant", "reply")}, sseText("ok2"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, profile.Generic)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	steps := allSteps(j)
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	if steps[0].SysChanged {
		t.Error("step 1 (no predecessor) should not be flagged SysChanged")
	}
	if !steps[1].SysChanged {
		t.Error("step 2 (different system prompt than step 1) should be flagged SysChanged")
	}
}

// TestSysChanged_SameSystemPromptStaysFalse is the negative control: an
// identical system prompt across turns must not be flagged.
func TestSysChanged_SameSystemPromptStaysFalse(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 9, 10, m, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "keep going")
	r1 := mkRec(at(0), "", []any{sys, u1}, sseText("ok"))
	r2 := mkRec(at(1), "", []any{sys, u1, msg("assistant", "reply")}, sseText("ok2"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	j, err := Build(l, profile.Generic)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, s := range allSteps(j) {
		if s.SysChanged {
			t.Errorf("step %d: SysChanged should be false when the system prompt never changes", s.Seq)
		}
	}
}

// TestCompactionInfo_TokensAndEntities covers the information-loss summary
// at a stitch boundary: token counts come from recorded Usage, entities
// from the rough file-path/URL regex scan.
func TestCompactionInfo_TokensAndEntities(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "read AGENTS.md and https://example.com/docs and summarize")

	predMsgs := []any{sys, u1}
	var recs []audit.Record
	const preBreakTurns = 6
	for i := 0; i < preBreakTurns; i++ {
		recs = append(recs, mkRecWithUsage(at(i), predMsgs, "ok", 1000+int64(i)*100, 50))
		predMsgs = append(predMsgs, msg("assistant", "checked AGENTS.md and README.md, see https://example.com/docs"))
	}
	// Contract: keeps the opening instruction, drops everything else —
	// including the mention of README.md (swallowed) but AGENTS.md/the URL
	// survive because they're restated in the successor's own opening.
	succMsgs := []any{msg("system", "sys v2"), u1, msg("assistant", "resuming — will re-check AGENTS.md and https://example.com/docs")}
	recs = append(recs, mkRecWithUsage(at(30), succMsgs, "continuing", 500, 20))

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

	j, err := BuildChain(chain, profile.Generic)
	if err != nil {
		t.Fatalf("BuildChain: %v", err)
	}
	steps := allSteps(j)
	last := steps[len(steps)-1]
	if last.Compaction == nil {
		t.Fatal("the stitch-boundary step should carry CompactionInfo")
	}
	c := last.Compaction
	wantTokensBefore := int64(1000 + (preBreakTurns-1)*100)
	if c.TokensBefore != wantTokensBefore {
		t.Errorf("TokensBefore = %d, want %d (predecessor's last manifest)", c.TokensBefore, wantTokensBefore)
	}
	if c.TokensAfter != 500 {
		t.Errorf("TokensAfter = %d, want 500 (this step's own usage)", c.TokensAfter)
	}
	joinedSurvived := strings.Join(c.SurvivedEntities, ",")
	if !strings.Contains(joinedSurvived, "AGENTS.md") {
		t.Errorf("AGENTS.md should be in SurvivedEntities (mentioned in both), got %v", c.SurvivedEntities)
	}
	if !strings.Contains(joinedSurvived, "example.com") {
		t.Errorf("the URL should be in SurvivedEntities, got %v", c.SurvivedEntities)
	}
	joinedSwallowed := strings.Join(c.SwallowedEntities, ",")
	if !strings.Contains(joinedSwallowed, "README.md") {
		t.Errorf("README.md (never restated) should be in SwallowedEntities, got %v", c.SwallowedEntities)
	}
}

// TestRevision_SpliceEdgeTagsTheReplacedMessage covers F11's "revision"
// relation: a Splice edge's divergence point must be tagged as revising the
// message it replaced, not rendered as an unrelated new Event.
func TestRevision_SpliceEdgeTagsTheReplacedMessage(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 9, 10, m, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "opening instruction")

	// prev: 10 messages after [sys, u1] — a1..a9 filler, then the tail that
	// will resurface after the splice. "dropped" only exists in prev (never
	// restated in cur) so the gap between LCP and len(prev.Keys) is 3, wide
	// enough to clear tailSlack (2) and reach the ReplaceTail/Splice check
	// instead of falling through to the default Append case.
	var prevMsgs []any = []any{sys, u1}
	for i := 0; i < 8; i++ {
		prevMsgs = append(prevMsgs, msg("assistant", "filler "+string(rune('a'+i))))
	}
	sharedPrefix := append([]any{}, prevMsgs...)
	dropped := msg("assistant", "dropped junk")
	tailA := msg("assistant", "tail A")
	tailB := msg("assistant", "tail B")
	prevMsgs = append(prevMsgs, dropped, tailA, tailB)
	r1 := mkRec(at(0), "", prevMsgs, sseText("ok"))

	// cur: same prefix (sys, u1, 8 filler), then ONE new "revising" message
	// (replacing "dropped"), then tailA/tailB reappear verbatim — Splice
	// per T2.1's判据.
	curMsgs := append([]any{}, sharedPrefix...)
	revising := msg("assistant", "revised summary absorbing the above")
	curMsgs = append(curMsgs, revising, tailA, tailB)
	r2 := mkRec(at(1), "", curMsgs, sseText("ok2"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	if len(l.Edges) != 1 || l.Edges[0].Kind != ctxgraph.Splice {
		t.Fatalf("test setup: want a single Splice edge, got %+v", l.Edges)
	}

	j, err := Build(l, profile.Generic)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	steps := allSteps(j)
	last := steps[len(steps)-1]
	if len(last.NewEvents) == 0 {
		t.Fatal("the Splice step should introduce at least one new event")
	}
	first := last.NewEvents[0]
	if first.Revises == nil {
		t.Fatal("the first new event at a Splice boundary should have Revises set")
	}
	if first.Msg.Text != "revised summary absorbing the above" {
		t.Errorf("unexpected first new event: %q", first.Msg.Text)
	}
}
