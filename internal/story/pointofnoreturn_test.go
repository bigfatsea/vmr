// Ver 2026-08-29, by Sonnet 5

package story

import (
	"testing"

	"vmr/internal/ctxgraph"
)

func TestComputePointOfNoReturn(t *testing.T) {
	compactionStep := func(seq int) *Step {
		return &Step{Seq: seq, Compaction: &CompactionInfo{
			TokensBefore: 64000, TokensAfter: 8000,
			SwallowedEntities: []string{"internal/auth/policy.go", "docs/spec.md"},
		}}
	}
	contractStep := func(seq int) *Step {
		return &Step{Seq: seq, Edge: &ctxgraph.Edit{Kind: ctxgraph.Contract}}
	}
	plainStep := func(seq int) *Step { return &Step{Seq: seq} }

	t.Run("nil when nothing marks a turn", func(t *testing.T) {
		j := journeyOf(plainStep(1), plainStep(2))
		if p := ComputePointOfNoReturn(j, nil); p != nil {
			t.Fatalf("want nil, got %+v", p)
		}
	})

	t.Run("compaction that dropped constraints", func(t *testing.T) {
		j := journeyOf(plainStep(1), compactionStep(2), plainStep(3))
		p := ComputePointOfNoReturn(j, nil)
		if p == nil || p.Kind != PONRCompaction || p.StepSeq != 2 {
			t.Fatalf("want compaction@2, got %+v", p)
		}
		if p.TokensBefore != 64000 || len(p.EntitiesDropped) != 2 {
			t.Fatalf("compaction detail not carried: %+v", p)
		}
	})

	t.Run("hard history contraction", func(t *testing.T) {
		j := journeyOf(plainStep(1), plainStep(2), contractStep(3))
		p := ComputePointOfNoReturn(j, nil)
		if p == nil || p.Kind != PONRContract || p.StepSeq != 3 {
			t.Fatalf("want contract@3, got %+v", p)
		}
	})

	t.Run("LLM constraint-drop finding with no swallowed-entities signal", func(t *testing.T) {
		// The entity-diff rule found nothing (SwallowedEntities empty), but
		// the LLM excerpt reader emitted the finding — the turn must still be
		// located, enriched with the step's compaction sizes.
		j := journeyOf(plainStep(1), &Step{Seq: 2, Compaction: &CompactionInfo{
			TokensBefore: 155000, TokensAfter: 3800}}, plainStep(3))
		fs := []Finding{{Code: FindingConstraintTextDropped, StepSeq: 2}}
		p := ComputePointOfNoReturn(j, fs)
		if p == nil || p.Kind != PONRCompaction || p.StepSeq != 2 {
			t.Fatalf("want compaction@2, got %+v", p)
		}
		if p.TokensBefore != 155000 || len(p.EntitiesDropped) != 0 {
			t.Fatalf("compaction sizes not carried / entities wrongly populated: %+v", p)
		}
	})

	t.Run("retry-loop finding", func(t *testing.T) {
		j := journeyOf(plainStep(1), plainStep(2), plainStep(3))
		fs := []Finding{{Code: FindingUnadaptedRetry, StepSeq: 3}}
		p := ComputePointOfNoReturn(j, fs)
		if p == nil || p.Kind != PONRRetry || p.StepSeq != 3 {
			t.Fatalf("want retry@3, got %+v", p)
		}
	})

	t.Run("earliest signal wins across kinds", func(t *testing.T) {
		j := journeyOf(plainStep(1), compactionStep(6), contractStep(7))
		fs := []Finding{{Code: FindingUnverifiedSuccess, StepSeq: 2}}
		p := ComputePointOfNoReturn(j, fs)
		if p == nil || p.Kind != PONRRetry || p.StepSeq != 2 {
			t.Fatalf("want retry@2 (earliest), got %+v", p)
		}
	})
}
