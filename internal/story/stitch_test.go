// Ver 2026-07-29 23:00, by Sonnet 5

// End-to-end coverage for the stitching acceptance criterion:
// "s231 的两段被缝成一个 journey，边类型 compaction，置信度
// 高，证据可打印" — the real s231 pattern (pure appends, then a Contract
// that keeps the same opening user message) rendered all the way through
// Scan -> StitchGraph -> ListCandidates -> ChainFrom -> BuildChain ->
// RenderMarkdown, not just the structural ctxgraph-level checks
// stitch_test.go (internal/ctxgraph) and session_conformance_test.go
// (internal/report) already cover independently.
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

func s231StyleFixture(t *testing.T) string {
	t.Helper()
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "深入调研这个内存涨价这一波")

	var recs []audit.Record
	msgsList := []any{sys, u1}
	for i := 0; i < 5; i++ {
		recs = append(recs, mkRec(at(i), "", append([]any{}, msgsList...), sseText("ok")))
		msgsList = append(msgsList, msg("assistant", "step reply"))
	}
	// Contract: history collapses, but the exact opening instruction
	// survives verbatim — the real s231 shape.
	recs = append(recs, mkRec(at(30), "", []any{msg("system", "sys v2"), u1, msg("assistant", "post-break reply")}, sseText("continuing")))

	return writeJSONL(t, recs)
}

// TestStitchedJourney_EndToEnd is the full pipeline check for the stitching
// acceptance criterion.
func TestStitchedJourney_EndToEnd(t *testing.T) {
	path := s231StyleFixture(t)
	g, err := ctxgraph.Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2", len(g.Lineages))
	}
	ctxgraph.StitchGraph(g)

	first, second := g.Lineages[0], g.Lineages[1]
	if second.Stitch == nil || second.Stitch.Outcome != ctxgraph.Stitched {
		t.Fatalf("second lineage should be Stitched, got %+v", second.Stitch)
	}
	if second.Stitch.Edge.Kind != ctxgraph.StitchCompaction {
		t.Fatalf("stitch kind = %v, want StitchCompaction", second.Stitch.Edge.Kind)
	}

	// ListCandidates: only the chain TAIL (second) should be offered — the
	// predecessor (first) is absorbed into that chain, not its own
	// candidate.
	cands := ListCandidates(g)
	if len(cands) != 1 {
		t.Fatalf("got %d candidates, want 1 (predecessor absorbed into the stitched chain)", len(cands))
	}
	if cands[0].Idx != second.Idx {
		t.Fatalf("the one candidate should be the chain tail (lineage %d), got lineage %d", second.Idx, cands[0].Idx)
	}

	byIdx := ctxgraph.LineageIndex(g)
	chain := ctxgraph.ChainFrom(cands[0], byIdx)
	if len(chain) != 2 || chain[0].Idx != first.Idx || chain[1].Idx != second.Idx {
		t.Fatalf("chain = %v, want [%d, %d] (oldest first)", chain, first.Idx, second.Idx)
	}

	j, err := BuildChain(chain, profile.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("BuildChain: %v", err)
	}

	// The Journey must be ONE continuous narrative covering all 6 steps
	// (5 pre-break + 1 post-break), not two separate documents.
	steps := 0
	for _, task := range j.Tasks {
		steps += len(task.Steps)
	}
	if steps != 6 {
		t.Fatalf("total steps = %d, want 6 (5 pre-break + 1 post-break)", steps)
	}

	// The stitch-boundary step (the 6th, last) carries nothing genuinely new
	// — its only content is the shared opening instruction (already shown
	// by the predecessor) plus "post-break reply" from the ASSISTANT side,
	// not a real user instruction. HumanInitiated must be false there:
	// metrics.go's F10 gap classification depends on this to count the long
	// pre-compaction gap as agent-side execution, not human idle.
	last := j.Tasks[len(j.Tasks)-1].Steps[len(j.Tasks[len(j.Tasks)-1].Steps)-1]
	if last.HumanInitiated {
		t.Error("the stitch-boundary step should not be HumanInitiated (no genuinely new instruction there)")
	}

	// Journey.Break must be nil — the chain's OWN head (first) opened its
	// bucket cleanly, so after stitching there's no remaining unresolved
	// break at the top of this Journey.
	if j.Break != nil {
		t.Errorf("Journey.Break should be nil after successful stitching, got %+v", j.Break)
	}

	// ID must span the full chain: client from `first` (chain[0]), end
	// time from `second` (the tail) — not just the tail lineage alone.
	wantEnd := second.Manifests[len(second.Manifests)-1].TS.UTC().Format(idTimeLayout)
	if !strings.Contains(j.ID, wantEnd) {
		t.Errorf("Journey.ID = %q, want it to contain the tail's end time %q", j.ID, wantEnd)
	}
	wantStart := first.Manifests[0].TS.UTC().Format(idTimeLayout)
	if !strings.Contains(j.ID, wantStart) {
		t.Errorf("Journey.ID = %q, want it to contain the chain head's start time %q", j.ID, wantStart)
	}

	md := RenderMarkdown(j, ComputeMetrics(j), ComputeFindings(j, i18n.EN), i18n.EN)
	for _, want := range []string{
		"🧵 **Stitched from an earlier fragment**",
		"compaction",
		"深入调研这个内存涨价这一波",    // the opening instruction, shared by both lineages
		"post-break reply", // content unique to the post-break lineage
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered Markdown missing %q\n--- full output ---\n%s", want, md)
		}
	}

	// The shared opening instruction naturally appears more than once
	// within Step 1 alone (journey title, task title, the event's <summary>
	// preview, and its fenced body) — that's normal rendering, not
	// duplication. What must NOT happen is the stitch-boundary task (t02
	// onward) showing it YET AGAIN: global seen-hash dedup must suppress it
	// there (its manifest reappears in `second`'s opening, which scans the
	// whole manifest since deltaStart=0 at a stitch boundary), and the task
	// title must fall back to the stitch marker instead of re-quoting an
	// instruction that wasn't actually asked again (see
	// newInstructionTitleAtStitch).
	stitchSection := md[strings.Index(md, "## t02"):]
	if strings.Contains(stitchSection, "深入调研这个内存涨价这一波") {
		t.Errorf("the stitch-boundary task re-shows the shared opening instruction — dedup should have suppressed it:\n%s", stitchSection)
	}
	if !strings.Contains(stitchSection, "stitched from an earlier fragment") {
		t.Errorf("the stitch-boundary task title should fall back to the stitch marker (no genuinely new instruction there):\n%s", stitchSection)
	}
}
