// Ver 2026-08-16 23:10, by gemini-3.7-flash

// Evidence pack types and LLM result payload definitions for Phase 1b semantic
// detectors — plus the two things those payloads flow through: parsing a
// detector reply out of raw LLM text, and the -llm-dry-run pack enumeration.
package story

import (
	"encoding/json"
	"fmt"
	"strings"

	"vmr/internal/i18n"
)

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

func parseJSONFromLLM[T any](text string, out *T) error {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 2 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		trimmed = strings.TrimSpace(strings.Join(lines, "\n"))
	}

	startObj := strings.Index(trimmed, "{")
	startArr := strings.Index(trimmed, "[")
	start := -1
	if startObj >= 0 && startArr >= 0 {
		if startObj < startArr {
			start = startObj
		} else {
			start = startArr
		}
	} else if startObj >= 0 {
		start = startObj
	} else if startArr >= 0 {
		start = startArr
	}

	endObj := strings.LastIndex(trimmed, "}")
	endArr := strings.LastIndex(trimmed, "]")
	end := -1
	if endObj >= 0 && endArr >= 0 {
		if endObj > endArr {
			end = endObj
		} else {
			end = endArr
		}
	} else if endObj >= 0 {
		end = endObj
	} else if endArr >= 0 {
		end = endArr
	}

	if start >= 0 && end > start {
		trimmed = trimmed[start : end+1]
	}

	return json.Unmarshal([]byte(trimmed), out)
}

// --- Detector pack enumeration (shared with -llm-dry-run) --------------------

// llmDetectorPack pairs one detector's would-be evidence pack with the
// stable name -llm-dry-run's per-call breakdown displays for it.
type llmDetectorPack struct {
	name string
	pack evidencePackKind
}

// buildLLMDetectorPacks assembles every detector's evidence pack whose
// candidate filter fires — the same builders the detectors themselves use,
// so a dry run enumerates exactly the calls a real run would make; the
// filters are pure local computation and building a pack sends nothing.
func buildLLMDetectorPacks(j *Journey, lang i18n.Lang) []llmDetectorPack {
	var out []llmDetectorPack
	if p := buildToolMisinterpretationPack(j); p != nil {
		out = append(out, llmDetectorPack{"tool_result_misinterpretation", *p})
	}
	if p := buildOscillationPack(j); p != nil {
		out = append(out, llmDetectorPack{"semantic_oscillation", *p})
	}
	if p := buildGoalDriftPack(j); p != nil {
		out = append(out, llmDetectorPack{"goal_drift", *p})
	}
	if p := buildConstraintPack(j); p != nil {
		out = append(out, llmDetectorPack{"constraint_dropped", *p})
	}
	if p, _ := buildPlanAuditPack(j, lang); p != nil {
		out = append(out, llmDetectorPack{"plan_misalignment", *p})
	}
	if p := buildCompletionClaimPack(j); p != nil {
		out = append(out, llmDetectorPack{"completion_claim", *p})
	}
	return out
}

// --- -llm-dry-run pack enumeration ------------------------------------------

// LLMDryRunEstimate is one LLM call a single-journey run would make, as
// reported by -llm-dry-run: the pack's stable name and its serialized size.
type LLMDryRunEstimate struct {
	Name  string
	Chars int
}

// EstimateLLMDryRun enumerates every LLM call a single-journey run would
// make: one per detector whose candidate filter fires (building a pack sends
// nothing) plus the journey interpretation itself, which is always sent.
// len(result) is therefore the maximum number of calls a real run would
// issue — the number "should I even run this" has to be judged against, not
// just the interpretation pack's size.
func EstimateLLMDryRun(j *Journey, m Metrics, findings []Finding, lang i18n.Lang) []LLMDryRunEstimate {
	var out []LLMDryRunEstimate
	for _, dp := range buildLLMDetectorPacks(j, lang) {
		out = append(out, LLMDryRunEstimate{Name: dp.name, Chars: packChars(dp.pack)})
	}
	pack := BuildSingleJourneyEvidencePack(j, m, findings, lang)
	return append(out, LLMDryRunEstimate{Name: "journey_interpretation", Chars: packChars(pack)})
}

// FormatLLMDryRun renders the estimate list as -llm-dry-run's stdout block:
// one line per call, then a total line. The total is the sum of the itemized
// sizes and the call count the itemized count, so the output cannot
// understate the spend it exists to preview.
func FormatLLMDryRun(est []LLMDryRunEstimate) string {
	var b strings.Builder
	total := 0
	for _, e := range est {
		fmt.Fprintf(&b, "%s: %d chars (~%d tokens estimated)\n", e.Name, e.Chars, e.Chars/4)
		total += e.Chars
	}
	fmt.Fprintf(&b, "%d pack(s), %d chars (~%d tokens estimated) total — up to %d LLM call(s) — dry run, no request sent\n",
		len(est), total, total/4, len(est))
	return b.String()
}
