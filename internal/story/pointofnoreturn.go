// Ver 2026-08-29, by Sonnet 5

// Point of no return: the earliest Step where the Journey took damage it
// never recovered from — a headline for the incident report, derived
// entirely from signals the Journey already carries (a constraint-losing
// compaction, an unadapted retry loop, a hard history contraction). No new
// detection: this only picks the earliest of three things findings.go /
// journey.go already found, and stays silent when none fired (a clean
// Journey has no point of no return, and the report says so).
//
// The constraint-losing compaction signal is keyed off the
// FindingConstraintTextDropped finding, not the raw
// Step.Compaction.SwallowedEntities field alone — that finding has two
// detectors (the entity-diff rule AND the LLM excerpt reader), and only the
// rule one populates SwallowedEntities. Scanning the field alone would let
// an LLM-confirmed constraint loss set the verdict to CRITICAL while this
// still reported "stayed on its rails".
package story

import "vmr/internal/ctxgraph"

// PointOfNoReturnKind names which signal marked the turn.
type PointOfNoReturnKind string

const (
	// PONRCompaction: a compaction dropped constraints — a
	// FindingConstraintTextDropped finding (entity-diff rule or LLM excerpt
	// reader), enriched with Step.Compaction token sizes when the finding's
	// step still carries them.
	PONRCompaction PointOfNoReturnKind = "compaction_constraint_loss"
	// PONRRetry: the step that closed an unadapted retry loop or an
	// unverified-success pattern (findings.go's error_retry_unadapted /
	// error_then_unverified_success).
	PONRRetry PointOfNoReturnKind = "unadapted_retry"
	// PONRContract: a hard history contraction (ctxgraph.Contract) — the
	// context was rebuilt much smaller, whatever was in the discarded tail
	// is gone.
	PONRContract PointOfNoReturnKind = "history_contract"
)

// PointOfNoReturn locates the turn, or nil when the Journey never took
// unrecoverable damage by any of the three signals.
type PointOfNoReturn struct {
	StepSeq int
	Kind    PointOfNoReturnKind
	// TokensBefore/After and EntitiesDropped are populated only for
	// PONRCompaction (straight off Step.Compaction).
	TokensBefore    int64
	TokensAfter     int64
	EntitiesDropped []string
}

// ponrRetryCodes are the findings whose StepSeq marks a "kept doing the same
// broken thing" turn — the retry-loop half of the point-of-no-return signal.
var ponrRetryCodes = map[FindingCode]bool{
	FindingUnadaptedRetry:    true,
	FindingUnverifiedSuccess: true,
}

// ComputePointOfNoReturn locates the turn from j and its already-computed
// findings (RenderHTML passes the same slice it renders). Language-
// independent — it reads Finding.Code and Finding.StepSeq only, never the
// localized text.
func ComputePointOfNoReturn(j *Journey, findings []Finding) *PointOfNoReturn {
	steps := journeySteps(j)

	var best *PointOfNoReturn
	consider := func(p *PointOfNoReturn) {
		if p == nil {
			return
		}
		if best == nil || p.StepSeq < best.StepSeq {
			best = p
		}
	}

	// Rule-signal fallback: a SwallowedEntities compaction always also
	// produces a FindingConstraintTextDropped in ComputeFindings, so the
	// findings loop below covers this too — but keep it so the function still
	// locates the turn when called with a nil findings slice.
	for _, s := range steps {
		if c := s.Compaction; c != nil && len(c.SwallowedEntities) > 0 {
			consider(&PointOfNoReturn{
				StepSeq: s.Seq, Kind: PONRCompaction,
				TokensBefore: c.TokensBefore, TokensAfter: c.TokensAfter,
				EntitiesDropped: c.SwallowedEntities,
			})
			break // earliest such step; steps are Seq-ordered
		}
	}
	for _, s := range steps {
		if s.Edge != nil && s.Edge.Kind == ctxgraph.Contract {
			consider(&PointOfNoReturn{StepSeq: s.Seq, Kind: PONRContract})
			break
		}
	}
	for _, f := range findings {
		switch {
		case ponrRetryCodes[f.Code]:
			consider(&PointOfNoReturn{StepSeq: f.StepSeq, Kind: PONRRetry})
		case f.Code == FindingConstraintTextDropped:
			p := &PointOfNoReturn{StepSeq: f.StepSeq, Kind: PONRCompaction}
			for _, s := range steps {
				if s.Seq == f.StepSeq && s.Compaction != nil {
					p.TokensBefore, p.TokensAfter = s.Compaction.TokensBefore, s.Compaction.TokensAfter
					p.EntitiesDropped = s.Compaction.SwallowedEntities
					break
				}
			}
			consider(p)
		}
	}
	return best
}
