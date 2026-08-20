// Ver 2026-07-29 23:45, by Sonnet 5

// Tests for system-change marking, compaction information-loss metrics,
// and the "revision" relation.
package story

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
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
	j, err := Build(l, taskseg.Generic, i18n.EN)
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
	j, err := Build(l, taskseg.Generic, i18n.EN)
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

	j, err := BuildChain(chain, taskseg.Generic, i18n.EN)
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
	if c.PredecessorTextExcerpt == "" || !strings.Contains(c.PredecessorTextExcerpt, "AGENTS.md") {
		t.Errorf("PredecessorTextExcerpt should contain predecessor text, got %q", c.PredecessorTextExcerpt)
	}

	// The struct-level assertions above don't prove renderCompactionInfo
	// actually surfaces any of this in what a reader opens — RenderMarkdown
	// is the only thing a human (or the compare/LLM evidence pack) ever
	// reads, so it needs its own assertion, not just CompactionInfo's field
	// values.
	md := RenderMarkdown(j, ComputeMetrics(j), ComputeFindings(j, i18n.EN), i18n.EN)
	for _, want := range []string{"Information loss", "README.md", "AGENTS.md"} {
		if !strings.Contains(md, want) {
			t.Errorf("RenderMarkdown output missing %q for the compaction boundary step:\n%s", want, md)
		}
	}
}

// TestBuildCompactionInfo_EntityBeyondCapStillCountsAsSurvived is a
// regression test for a real false-positive: chatmsg.ExtractEntities caps at
// MaxEntities (30) distinct entities per text, independently for the
// predecessor and the successor. The old implementation tested survival by
// membership in the SUCCESSOR's own capped set — so an entity mentioned in
// the predecessor that genuinely still appears in the successor, but only as
// the successor's 31st+ distinct entity (i.e. past ITS cap), got dropped
// from that capped set and misreported as swallowed even though it's right
// there in the text. That's an assertion of a false fact, not just an
// omission — the fix tests literal presence in the successor's full,
// uncapped text instead.
func TestBuildCompactionInfo_EntityBeyondCapStillCountsAsSurvived(t *testing.T) {
	predBody := map[string]any{"messages": []any{msg("user", "please check target.md")}}
	predRec := &audit.Record{Client: audit.Exchange{Request: audit.Message{Body: predBody}}}

	var b strings.Builder
	for i := 0; i < 40; i++ { // well past chatmsg.MaxEntities (30)
		b.WriteString("file" + strconv.Itoa(i) + ".md ")
	}
	b.WriteString("and also target.md")
	curMsgs := []chatmsg.Message{{Role: "assistant", Text: b.String()}}

	info := buildCompactionInfo(predRec, &ctxgraph.Manifest{}, &ctxgraph.Manifest{}, curMsgs)

	if strings.Contains(strings.Join(info.SwallowedEntities, ","), "target.md") {
		t.Errorf("target.md is present in the successor text (just past its own entity cap) and must NOT be reported as swallowed: %v", info.SwallowedEntities)
	}
	if !strings.Contains(strings.Join(info.SurvivedEntities, ","), "target.md") {
		t.Errorf("target.md should be reported as survived, got survived=%v swallowed=%v", info.SurvivedEntities, info.SwallowedEntities)
	}
}

// TestRevision_SpliceEdgeTagsTheReplacedMessage covers the "revision"
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
	// (replacing "dropped"), then tailA/tailB reappear verbatim — this is
	// what triggers Splice classification.
	curMsgs := append([]any{}, sharedPrefix...)
	revising := msg("assistant", "revised summary absorbing the above")
	curMsgs = append(curMsgs, revising, tailA, tailB)
	r2 := mkRec(at(1), "", curMsgs, sseText("ok2"))

	path := writeJSONL(t, []audit.Record{r1, r2})
	l := onlyLineage(t, path)
	if len(l.Edges) != 1 || l.Edges[0].Kind != ctxgraph.Splice {
		t.Fatalf("test setup: want a single Splice edge, got %+v", l.Edges)
	}

	j, err := Build(l, taskseg.Generic, i18n.EN)
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

	// P5.1 removed renderEvent (the fact-layer's per-message renderer,
	// which used to carry a 🔄[revises …] marker on the Markdown message
	// list itself) — that marker existed to stop a Splice's rewritten
	// message from reading as an unrelated duplicate WITHIN that inlined
	// list. There is no inlined message list left to disambiguate: the
	// decision spine never lists individual NewEvents, and the underlying
	// fact this test locks in — Event.Revises being computed correctly —
	// still reaches journey-<id>.json's structure field as EventRef.Revises
	// (P4), which is what a consumer needing this relationship now reads.
	// The assertions above are the ones that matter for this test's name.
}
