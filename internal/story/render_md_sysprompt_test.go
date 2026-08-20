// Ver 2026-08-20, by Sonnet 5

package story

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func sysAt(m int) time.Time { return time.Date(2026, 7, 20, 9, m, 0, 0, time.UTC) }

// TestSystemPromptEras_UnchangedThroughout is the baseline: one system
// prompt used for every Step produces exactly one era spanning the whole
// Journey.
func TestSystemPromptEras_UnchangedThroughout(t *testing.T) {
	sys := msg("system", "same prompt throughout")
	u1 := msg("user", "do it")
	recs := []audit.Record{
		mkRec(sysAt(0), "", []any{sys, u1}, sseText("ok")),
		mkRec(sysAt(1), "", []any{sys, u1, msg("assistant", "ok"), msg("user", "more")}, sseText("ok2")),
	}
	path := writeJSONL(t, recs)
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	eras := systemPromptEras(j)
	if len(eras) != 1 {
		t.Fatalf("got %d eras, want 1: %+v", len(eras), eras)
	}
	if !eras[0].HasSys {
		t.Errorf("era should have HasSys=true")
	}
}

// TestSystemPromptEras_MidLineageChange is the regression this rewrite
// exists for: a system-prompt change on an ORDINARY continuation Step
// (i>0 within the same Lineage, not the Journey's first Step and not a
// stitch boundary) must still be detected as a new era. The prior
// NewEvents-scanning implementation could not see this — deltaStart for an
// i>0 Step is always >= Manifest.LeadSys, so appendNewEvents structurally
// never scans the leading system message's index, no matter how different
// its text is from the previous Step's.
func TestSystemPromptEras_MidLineageChange(t *testing.T) {
	sysV1 := msg("system", "prompt version 1")
	sysV2 := msg("system", "prompt version 2 — totally different text")
	u1 := msg("user", "do it")
	a1 := msg("assistant", "ok")
	u2 := msg("user", "more")
	recs := []audit.Record{
		mkRec(sysAt(0), "", []any{sysV1, u1}, sseText("ok")),
		// Step 2: same conversation history (u1, a1) plus one new user
		// message — a plain Append from Classify's point of view (system
		// messages never participate in Keys/LCP, see manifest.go) — but
		// the LEADING system block itself changed.
		mkRec(sysAt(1), "", []any{sysV2, u1, a1, u2}, sseText("ok2")),
	}
	path := writeJSONL(t, recs)
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	steps := journeySteps(j)
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	if !steps[1].SysChanged {
		t.Fatalf("steps[1].SysChanged should be true (fixture changed the system prompt text)")
	}
	eras := systemPromptEras(j)
	if len(eras) != 2 {
		t.Fatalf("got %d eras, want 2 (the mid-Lineage system prompt change went undetected): %+v", len(eras), eras)
	}
	if eras[0].FromSeq != 1 || eras[0].ToSeq != 1 {
		t.Errorf("era[0] range = %d-%d, want 1-1", eras[0].FromSeq, eras[0].ToSeq)
	}
	if eras[1].FromSeq != 2 || eras[1].ToSeq != 2 {
		t.Errorf("era[1] range = %d-%d, want 2-2", eras[1].FromSeq, eras[1].ToSeq)
	}
	if eras[0].SysHash == eras[1].SysHash {
		t.Errorf("the two eras should have different SysHash values")
	}
}

// TestSystemPromptEras_StitchBoundaryChange confirms the state-machine
// rewrite didn't regress the one case the old NewEvents-based
// implementation COULD already detect: a system prompt change riding in on
// a stitch boundary (deltaStart stays 0 there, so the old scan did reach
// index 0 in that specific case).
func TestSystemPromptEras_StitchBoundaryChange(t *testing.T) {
	sysV1 := msg("system", "prompt version 1")
	sysV2 := msg("system", "prompt version 2 after compaction")
	u1 := msg("user", "深入调研这个内存涨价这一波")
	var recs []audit.Record
	msgsList := []any{sysV1, u1}
	for i := 0; i < 5; i++ {
		recs = append(recs, mkRec(sysAt(i), "", append([]any{}, msgsList...), sseText("ok")))
		msgsList = append(msgsList, msg("assistant", "step reply"))
	}
	// Contract with a NEW system prompt at the stitch boundary.
	recs = append(recs, mkRec(sysAt(30), "", []any{sysV2, u1, msg("assistant", "post-break reply")}, sseText("continuing")))

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
	chain := ctxgraph.ChainFrom(ListCandidates(g)[0], byIdx)
	j, err := BuildChain(chain, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("BuildChain: %v", err)
	}
	eras := systemPromptEras(j)
	if len(eras) != 2 {
		t.Fatalf("got %d eras, want 2: %+v", len(eras), eras)
	}
	if eras[1].FromSeq != 6 {
		t.Errorf("era[1].FromSeq = %d, want 6 (the stitch-boundary step)", eras[1].FromSeq)
	}
}

// TestRenderSystemPromptHeader_LinksNotInline locks P5.3's core behavior:
// the header renders a link, not the full system prompt text — and the
// linked filename matches reqdetail.SysPromptEvidenceFileName for the same
// hash, so the two never disagree about where the evidence blob lives.
func TestRenderSystemPromptHeader_LinksNotInline(t *testing.T) {
	sys := msg("system", "UNIQUE_SENTINEL_do_not_inline_this_text")
	u1 := msg("user", "hi")
	recs := []audit.Record{mkRec(sysAt(0), "", []any{sys, u1}, sseText("ok"))}
	path := writeJSONL(t, recs)
	j, err := Build(onlyLineage(t, path), taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var b strings.Builder
	w := func(format string, args ...any) { b.WriteString(fmt.Sprintf(format, args...)) }
	renderSystemPromptHeader(w, j, i18n.Story(i18n.EN))
	out := b.String()
	if strings.Contains(out, "UNIQUE_SENTINEL_do_not_inline_this_text") {
		t.Errorf("header should link to the evidence blob, not inline the system prompt text: %s", out)
	}
	if !strings.Contains(out, "../evidence/sysprompt-") {
		t.Errorf("header should contain a link to ../evidence/sysprompt-<hash>.md, got: %s", out)
	}
}
