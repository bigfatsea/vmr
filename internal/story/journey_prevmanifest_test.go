// Ver 2026-08-20, by Sonnet 5

// Regression coverage for Step.PrevManifest's stitch-boundary rule:
// it must be nil at a Lineage's first Step, INCLUDING a stitch boundary —
// not the predecessor lineage's tail Manifest, even though buildFrom
// computes and uses that value locally for sysChanged/CompactionInfo. This
// is the one place a wrong choice would silently break reqdetail's
// cross-command byte-identical guarantee (internal/report's session
// grouping is strictly per-Lineage too, so a stitch boundary's ReqInfo.Parent
// is always nil on that side).
package story

import (
	"testing"

	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func TestStep_PrevManifest_NilAtStitchBoundary(t *testing.T) {
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

	byIdx := ctxgraph.LineageIndex(g)
	chain := ctxgraph.ChainFrom(ListCandidates(g)[0], byIdx)
	j, err := BuildChain(chain, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("BuildChain: %v", err)
	}

	steps := journeySteps(j)
	if len(steps) != 6 {
		t.Fatalf("total steps = %d, want 6", len(steps))
	}

	// steps[0]: the whole Journey's first step — no predecessor at all.
	if steps[0].PrevManifest != nil {
		t.Errorf("steps[0].PrevManifest = %+v, want nil (Journey's first step)", steps[0].PrevManifest)
	}
	// steps[1..4]: ordinary continuations within the first Lineage — each
	// must point at its own immediate physical predecessor.
	for i := 1; i < 5; i++ {
		want := first.Manifests[i-1]
		if steps[i].PrevManifest != want {
			t.Errorf("steps[%d].PrevManifest = %p, want %p (first.Manifests[%d])", i, steps[i].PrevManifest, want, i-1)
		}
	}
	// steps[5]: the stitch boundary (second Lineage's only Step) — must be
	// nil, NOT first.Manifests[len-1] (the predecessor lineage's tail),
	// even though that value is exactly what buildFrom computes locally
	// for sysChanged/CompactionInfo at this same Step.
	last := steps[5]
	if last.StitchEdge == nil {
		t.Fatalf("steps[5] should be the stitch boundary (StitchEdge != nil)")
	}
	if last.PrevManifest != nil {
		t.Errorf("steps[5] (stitch boundary).PrevManifest = %+v, want nil — internal/report's ReqInfo.Parent "+
			"is nil for any Lineage's first record (session.go's group() is strictly per-Lineage), so "+
			"reqdetail.EnsureRendered must see the same nil here or the two commands' detail pages diverge", last.PrevManifest)
	}
	// Sanity: the stitch boundary Step's own Compaction (which legitimately
	// needs the cross-lineage predecessor) must still be present — this
	// test is about PrevManifest specifically, not about breaking
	// Compaction/SysChanged, which have their own already-passing coverage
	// in stitch_test.go.
	if last.Compaction == nil {
		t.Fatalf("steps[5].Compaction should still be non-nil — PrevManifest's nil-ness must not affect it")
	}
}
