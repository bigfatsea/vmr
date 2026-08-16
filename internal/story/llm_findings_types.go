// Ver 2026-08-16 23:10, by gemini-3.7-flash

// Evidence pack types and LLM result payload definitions for Phase 1b semantic detectors.
package story

import "vmr/internal/i18n"

// --- P1b.1: Tool Result Misinterpretation (E3) Types ------------------------

type ToolMisinterpretationEvidencePack struct {
	SuspiciousPairs []SuspiciousToolPair `json:"suspicious_pairs"`
}

func (p ToolMisinterpretationEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version:    "tool-misinterpretation-v1",
		System:     t.ToolMisinterpretationSystemPrompt,
		UserPrefix: t.ToolMisinterpretationUserPrefix,
		UserSuffix: t.ToolMisinterpretationUserSuffix,
	}
}

type toolMisinterpretationItem struct {
	StepSeq          int    `json:"step_seq"`
	IsMisinterpreted bool   `json:"is_misinterpreted"`
	Confidence       string `json:"confidence"`
	EvidenceAnchor   string `json:"evidence_anchor"`
	Explanation      string `json:"explanation"`
	SuggestedAction  string `json:"suggested_action"`
}

// --- P1b.2: Semantic Oscillation (E4) Types ---------------------------------

type ToolCallSnippet struct {
	StepSeq   int    `json:"step_seq"`
	ToolName  string `json:"tool_name"`
	ArgsBrief string `json:"args_brief"`
}

type OscillationCandidate struct {
	ToolName string            `json:"tool_name"`
	Calls    []ToolCallSnippet `json:"calls"`
}

type SemanticOscillationEvidencePack struct {
	Candidates []OscillationCandidate `json:"candidates"`
}

func (p SemanticOscillationEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version:    "semantic-oscillation-v1",
		System:     t.SemanticOscillationSystemPrompt,
		UserPrefix: t.SemanticOscillationUserPrefix,
		UserSuffix: t.SemanticOscillationUserSuffix,
	}
}

type semanticOscillationItem struct {
	StepSeq           int    `json:"step_seq"`
	IsOscillating     bool   `json:"is_oscillating"`
	Confidence        string `json:"confidence"`
	EvidenceAnchor    string `json:"evidence_anchor"`
	Explanation       string `json:"explanation"`
	SuggestedBreakout string `json:"suggested_breakout"`
}

// --- P1b.3: Long-term Goal Drift (E5) Types ---------------------------------

type StepCheckpoint struct {
	StepSeq   int      `json:"step_seq"`
	Reasoning string   `json:"reasoning_brief"`
	ToolNames []string `json:"tools,omitempty"`
}

type GoalDriftEvidencePack struct {
	RootUserIntent string           `json:"root_user_intent"`
	Checkpoints    []StepCheckpoint `json:"checkpoints"`
}

func (p GoalDriftEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version:    "goal-drift-v1",
		System:     t.GoalDriftSystemPrompt,
		UserPrefix: t.GoalDriftUserPrefix,
		UserSuffix: t.GoalDriftUserSuffix,
	}
}

type goalDriftResult struct {
	DriftDetected    bool   `json:"drift_detected"`
	DriftStepSeq     int    `json:"drift_step_seq"`
	Confidence       string `json:"confidence"`
	EvidenceAnchor   string `json:"evidence_anchor"`
	DriftExplanation string `json:"drift_explanation"`
	SuggestedAction  string `json:"suggested_action"`
}

// --- P1b.4: Compaction Constraint Dropped (E7) Types ------------------------

type CompactionExcerpt struct {
	StepSeq            int    `json:"step_seq"`
	PredecessorExcerpt string `json:"predecessor_excerpt"`
}

type CompactionConstraintEvidencePack struct {
	Excerpts []CompactionExcerpt `json:"excerpts"`
}

func (p CompactionConstraintEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version:    "compaction-constraint-v1",
		System:     t.CompactionConstraintSystemPrompt,
		UserPrefix: t.CompactionConstraintUserPrefix,
		UserSuffix: t.CompactionConstraintUserSuffix,
	}
}

type compactionConstraintItem struct {
	StepSeq         int    `json:"step_seq"`
	ConstraintLost  bool   `json:"constraint_lost"`
	Confidence      string `json:"confidence"`
	EvidenceAnchor  string `json:"evidence_anchor"`
	Explanation     string `json:"explanation"`
	SuggestedAction string `json:"suggested_action"`
}

// --- P1b.5: Plan Execution Misalignment (PCPC) Types ------------------------

type PlanItemAudit struct {
	Seq  int    `json:"seq"`
	Text string `json:"text"`
}

type PlanAuditEvidencePack struct {
	PlanItems    []PlanItemAudit  `json:"plan_items"`
	ActionsTaken []ToolIndexEntry `json:"actions_taken"`
}

func (p PlanAuditEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version:    "plan-audit-v1",
		System:     t.PlanAuditSystemPrompt,
		UserPrefix: t.PlanAuditUserPrefix,
		UserSuffix: t.PlanAuditUserSuffix,
	}
}

type planAuditResult struct {
	HasMisalignment  bool `json:"has_misalignment"`
	UnfulfilledItems []struct {
		Seq    int    `json:"seq"`
		Text   string `json:"text"`
		Status string `json:"status"`
	} `json:"unfulfilled_items"`
	Confidence      string `json:"confidence"`
	EvidenceAnchor  string `json:"evidence_anchor"`
	Explanation     string `json:"explanation"`
	SuggestedAction string `json:"suggested_action"`
}

// --- P1b.6: Unverified Completion Claim (E2) Types --------------------------

type CompletionClaimEvidencePack struct {
	FinalOutcome          string   `json:"final_outcome"`
	FinalReasoning        string   `json:"final_reasoning,omitempty"`
	VerificationCommands  []string `json:"verification_commands_observed"`
	UnresolvedErrorEvents []string `json:"unresolved_error_events,omitempty"`
}

func (p CompletionClaimEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version:    "completion-claim-v1",
		System:     t.CompletionClaimSystemPrompt,
		UserPrefix: t.CompletionClaimUserPrefix,
		UserSuffix: t.CompletionClaimUserSuffix,
	}
}

type completionClaimResult struct {
	ClaimStatus         string `json:"claim_status"`
	Confidence          string `json:"confidence"`
	EvidenceAnchor      string `json:"evidence_anchor"`
	MissingVerification string `json:"missing_verification"`
	SuggestedAction     string `json:"suggested_action"`
}
