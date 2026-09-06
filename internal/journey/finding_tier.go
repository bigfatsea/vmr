// Ver 2026-09-15, by pi

// Finding display-trust tiers: the shared ordering source for both render
// paths' findings grouping (render_spine.go's renderFindingsSection and
// viewmodel_spine.go's buildVMFindings). Split out of severity.go when that
// file's only remaining live consumers were these two maps, and out of
// render_spine.go when the viewmodel layer joined them as a second consumer —
// one tier definition, two renderers, no drift.
package journey

// findingTrustTier ranks a FindingCode for display grouping (问题 15):
// critical codes (real failure modes) read first, low-confidence codes
// (unverified_entity_reference) last, everything else in between. Tier ties
// within the same rank break by earliest StepSeq — deterministic, independent
// of findings-slice order. The two maps below moved here from the retired
// severity.go (whose only remaining consumers were this tier ranking): they
// are the shared source of truth for which codes are trustworthy.
func findingTrustTier(c FindingCode) int {
	if criticalFindings[c] {
		return 0
	}
	if lowConfidenceFindings[c] {
		return 2
	}
	return 1
}

// criticalFindings are the codes that mean the agent was actively burning
// resources or had lost the task (loops, goal drift, dropped constraints) —
// the "actively going wrong", not merely "worth checking", failure modes.
var criticalFindings = map[FindingCode]bool{
	FindingExactRepeatToolCall:    true, // tool-call loop
	FindingUnadaptedRetry:         true, // retry loop, same broken args
	FindingNarrationWithoutAction: true, // talks in circles, never acts
	FindingSemanticOscillation:    true, // flip-flops between positions
	FindingGoalDrift:              true, // working on something else now
	FindingConstraintTextDropped:  true, // a compaction dropped its constraints
}

// lowConfidenceFindings are detector codes too noisy to headline a verdict on
// their own. unverified_entity_reference fires on ~34% of the real corpus and
// its own detector doc comment hedges it as "a suspicious signal anchored on a
// tool falsification, not a confirmed hallucination" (it flags Go stdlib types
// and the project's own live endpoints as "证伪").
var lowConfidenceFindings = map[FindingCode]bool{
	FindingUnverifiedEntityReference: true,
}
