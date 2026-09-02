// Ver 2026-08-16 23:15, by gemini-3.7-flash

// Phase 1b LLM semantic detectors for single Journey analysis.
//
// Sits on top of the deterministic rule layer (findings.go / findings_toolresult.go).
// Strict boundary contracts:
// 1. Candidates are filtered by deterministic rules first; LLM is only called on bounded slices.
// 2. Output is structured JSON with discrete confidence (HIGH/MEDIUM/LOW) and textual EvidenceAnchor.
// 3. Only HIGH confidence findings with non-empty EvidenceAnchor are promoted to Finding (SourceLLMInferred).
// 4. Always fail-open: network or model errors never fail the analysis.
package story

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
)

// --- P1b.1: Tool Result Misinterpretation (E3) ------------------------------

// buildToolMisinterpretationPack assembles the detector's evidence pack from
// j's suspicious tool pairs — nil when the candidate filter finds none (the
// same builders the detectors themselves call, so -llm-dry-run's estimate
// enumerates exactly the calls a real run would make).
func buildToolMisinterpretationPack(j *Journey) *ToolMisinterpretationEvidencePack {
	pairs := extractSuspiciousToolPairs(j)
	if len(pairs) == 0 {
		return nil
	}
	return &ToolMisinterpretationEvidencePack{SuspiciousPairs: pairs}
}

func detectLLMToolResultMisinterpretation(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	pack := buildToolMisinterpretationPack(j)
	if pack == nil {
		return nil
	}
	res, err := Interpret(ctx, opts, *pack, lang)
	if err != nil {
		return nil
	}
	var items []toolMisinterpretationItem
	if err := parseJSONFromLLM(res.Text, &items); err != nil {
		return nil
	}
	tx := i18n.StoryFindings(lang)
	var findings []Finding
	for _, item := range items {
		if item.IsMisinterpreted && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" {
			tool := ""
			for _, p := range pack.SuspiciousPairs {
				if p.StepSeq == item.StepSeq {
					tool = p.ToolName
					break
				}
			}
			fText := tx.ToolResultMisinterpretation(tool, sanitizeMDStruct(item.Explanation))
			action := sanitizeMDStruct(item.SuggestedAction)
			if action == "" {
				action = fText.Action
			}
			findings = append(findings, Finding{
				Code:           FindingToolResultMisinterpretation,
				StepSeq:        item.StepSeq,
				Source:         SourceLLMInferred,
				Confidence:     ConfidenceHigh,
				EvidenceAnchor: item.EvidenceAnchor,
				Finding:        fText.Finding,
				Evidence:       fText.Evidence,
				Action:         action,
			})
		}
	}
	return findings
}

// --- P1b.2: Semantic Oscillation (E4) ---------------------------------------

func detectOscillationCandidates(steps []*Step) []OscillationCandidate {
	const windowSize = 6
	if len(steps) < 3 {
		return nil
	}
	var out []OscillationCandidate
	emitted := map[string]bool{} // deduplicate per tool name across overlapping windows
	for i := 0; i+3 <= len(steps); i++ {
		end := i + windowSize
		if end > len(steps) {
			end = len(steps)
		}
		window := steps[i:end]
		toolCounts := map[string][]ToolCallSnippet{}
		toolKeys := map[string]map[string]bool{}

		for _, s := range window {
			for _, tc := range s.ToolCalls {
				k := toolCallKey(tc)
				if toolCounts[tc.Name] == nil {
					toolCounts[tc.Name] = nil
					toolKeys[tc.Name] = map[string]bool{}
				}
				brief, _ := truncateText(tc.Args, 200)
				toolCounts[tc.Name] = append(toolCounts[tc.Name], ToolCallSnippet{
					StepSeq:   s.Seq,
					ToolName:  tc.Name,
					ArgsBrief: brief,
				})
				toolKeys[tc.Name][k] = true
			}
		}

		toolNames := make([]string, 0, len(toolCounts))
		for tn := range toolCounts {
			toolNames = append(toolNames, tn)
		}
		sort.Strings(toolNames)

		for _, toolName := range toolNames {
			calls := toolCounts[toolName]
			if emitted[toolName] {
				continue // already captured this tool from an earlier window
			}
			if len(calls) >= 3 && len(toolKeys[toolName]) > 1 {
				out = append(out, OscillationCandidate{
					ToolName: toolName,
					Calls:    calls,
				})
				emitted[toolName] = true
			}
		}
		if len(out) >= 5 {
			break
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Calls[0].StepSeq != out[j].Calls[0].StepSeq {
			return out[i].Calls[0].StepSeq < out[j].Calls[0].StepSeq
		}
		return out[i].ToolName < out[j].ToolName
	})

	return out
}

// buildOscillationPack assembles the oscillation detector's evidence pack
// from j's candidate windows — nil when none fire.
func buildOscillationPack(j *Journey) *SemanticOscillationEvidencePack {
	cands := detectOscillationCandidates(journeySteps(j))
	if len(cands) == 0 {
		return nil
	}
	return &SemanticOscillationEvidencePack{Candidates: cands}
}

func detectLLMSemanticOscillation(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	pack := buildOscillationPack(j)
	if pack == nil {
		return nil
	}
	res, err := Interpret(ctx, opts, *pack, lang)
	if err != nil {
		return nil
	}
	var items []semanticOscillationItem
	if err := parseJSONFromLLM(res.Text, &items); err != nil {
		return nil
	}
	tx := i18n.StoryFindings(lang)
	var findings []Finding
	for _, item := range items {
		if item.IsOscillating && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" {
			tool := ""
			for _, c := range pack.Candidates {
				for _, call := range c.Calls {
					if call.StepSeq == item.StepSeq {
						tool = c.ToolName
						break
					}
				}
			}
			fText := tx.SemanticOscillation(tool, sanitizeMDStruct(item.Explanation))
			action := sanitizeMDStruct(item.SuggestedBreakout)
			if action == "" {
				action = fText.Action
			}
			findings = append(findings, Finding{
				Code:           FindingSemanticOscillation,
				StepSeq:        item.StepSeq,
				Source:         SourceLLMInferred,
				Confidence:     ConfidenceHigh,
				EvidenceAnchor: item.EvidenceAnchor,
				Finding:        fText.Finding,
				Evidence:       fText.Evidence,
				Action:         action,
			})
		}
	}
	return findings
}

// --- P1b.3: Long-term Goal Drift (E5) ----------------------------------------

// buildGoalDriftPack assembles the drift detector's evidence pack from j's
// checkpoints — nil when j has no stated root intent, is too short to audit,
// or would present fewer than two checkpoints.
func buildGoalDriftPack(j *Journey) *GoalDriftEvidencePack {
	intent := extractRootUserIntent(j)
	if intent == "" {
		return nil
	}
	steps := journeySteps(j)
	if len(steps) < 6 {
		return nil
	}
	var checkpts []StepCheckpoint
	for i, s := range steps {
		if i == 0 || i == len(steps)-1 || s.HumanInitiated || (i%3 == 0) {
			reasoning := s.Reasoning
			if reasoning == "" {
				reasoning = s.RespText
			}
			brief, _ := truncateText(reasoning, 200)
			var tools []string
			for _, tc := range s.ToolCalls {
				tools = append(tools, tc.Name)
			}
			checkpts = append(checkpts, StepCheckpoint{
				StepSeq:   s.Seq,
				Reasoning: brief,
				ToolNames: tools,
			})
		}
	}
	if len(checkpts) < 2 {
		return nil
	}
	return &GoalDriftEvidencePack{
		RootUserIntent: intent,
		Checkpoints:    checkpts,
	}
}

func detectLLMGoalDrift(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	pack := buildGoalDriftPack(j)
	if pack == nil {
		return nil
	}
	res, err := Interpret(ctx, opts, *pack, lang)
	if err != nil {
		return nil
	}
	var item goalDriftResult
	if err := parseJSONFromLLM(res.Text, &item); err != nil {
		return nil
	}
	// Step 1 IS the root intent by construction (extractRootUserIntent
	// returns the first user message's text) — any "drift anchored at
	// step 1" verdict is a contradiction in terms, and the observed
	// failure mode (LLM occasionally returns DriftStepSeq:1, see review
	// P-09 / KNOWN_ISSUES §2.53) is exactly that category mistake. The
	// evidence pack's first checkpoint is Step 1 by buildGoalDriftPack's
	// own loop, so this guard also keeps "first checkpoint" and "drift
	// anchor" on different steps and prevents the same step from being
	// cited as both the root and the departure from it.
	if item.DriftDetected && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" && item.DriftStepSeq > 1 {
		tx := i18n.StoryFindings(lang)
		fText := tx.GoalDrift(item.DriftStepSeq, sanitizeMDStruct(item.DriftExplanation))
		action := sanitizeMDStruct(item.SuggestedAction)
		if action == "" {
			action = fText.Action
		}
		var related []int
		if len(j.Tasks) > 0 && len(j.Tasks[0].Steps) > 0 {
			related = []int{j.Tasks[0].Steps[0].Seq}
		}
		return []Finding{{
			Code:           FindingGoalDrift,
			StepSeq:        item.DriftStepSeq,
			RelatedSeq:     related,
			Source:         SourceLLMInferred,
			Confidence:     ConfidenceHigh,
			EvidenceAnchor: item.EvidenceAnchor,
			Finding:        fText.Finding,
			Evidence:       fText.Evidence,
			Action:         action,
		}}
	}
	return nil
}

// --- P1b.4: Compaction Constraint Dropped (E7) ------------------------------

// buildConstraintPack assembles the compaction-constraint detector's
// evidence pack from j's compaction excerpts — nil when there are none.
func buildConstraintPack(j *Journey) *CompactionConstraintEvidencePack {
	var excerpts []CompactionExcerpt
	for _, s := range journeySteps(j) {
		if s.Compaction != nil && s.Compaction.PredecessorTextExcerpt != "" {
			excerpts = append(excerpts, CompactionExcerpt{
				StepSeq:            s.Seq,
				PredecessorExcerpt: s.Compaction.PredecessorTextExcerpt,
			})
		}
	}
	if len(excerpts) == 0 {
		return nil
	}
	return &CompactionConstraintEvidencePack{Excerpts: excerpts}
}

func detectLLMConstraintDropped(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	pack := buildConstraintPack(j)
	if pack == nil {
		return nil
	}
	res, err := Interpret(ctx, opts, *pack, lang)
	if err != nil {
		return nil
	}
	var items []compactionConstraintItem
	if err := parseJSONFromLLM(res.Text, &items); err != nil {
		return nil
	}
	tx := i18n.StoryFindings(lang)
	var findings []Finding
	for _, item := range items {
		if item.ConstraintLost && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" {
			fText := tx.LLMConstraintDropped(sanitizeMDStruct(item.EvidenceAnchor))
			evidence := sanitizeMDStruct(item.Explanation)
			action := sanitizeMDStruct(item.SuggestedAction)
			if action == "" {
				action = fText.Action
			}
			findings = append(findings, Finding{
				Code:           FindingConstraintTextDropped,
				StepSeq:        item.StepSeq,
				Source:         SourceLLMInferred,
				Confidence:     ConfidenceHigh,
				EvidenceAnchor: item.EvidenceAnchor,
				Finding:        fText.Finding,
				Evidence:       evidence,
				Action:         action,
			})
		}
	}
	return findings
}

// --- P1b.5: Plan Execution Misalignment (PCPC) ------------------------------

// buildPlanAuditPack assembles the plan-misalignment detector's evidence
// pack — the plan baseline found in j's text plus the tool index to audit
// against — and the seq of the Step where the baseline plan was announced
// (the Finding's StepSeq). Pack is nil when no plan with enough items is
// announced anywhere.
func buildPlanAuditPack(j *Journey, lang i18n.Lang) (*PlanAuditEvidencePack, int) {
	steps := journeySteps(j)
	if len(steps) < 2 {
		return nil, 0
	}
	var planItems []PlanItemAudit
	var planStepSeq int
	var baselineWords map[string]bool
	for _, s := range steps {
		raw := s.RespText
		if s.Reasoning != "" {
			raw = s.Reasoning + "\n" + raw
		}
		items := ExtractActionablePlan(raw)
		if len(items) < 2 {
			continue
		}
		var planText strings.Builder
		for _, it := range items {
			planText.WriteString(it.Text)
			planText.WriteByte(' ')
		}
		words := wordSet(planText.String())
		// The first plan found becomes the baseline. A later plan only
		// replaces it (Plan v2 / dynamic re-planning) when it diverges
		// enough (Jaccard < 0.4) from the current baseline — otherwise
		// it's the same plan restated, and auditing fulfillment against
		// the original baseline (not a stale earlier one, and not every
		// incidental restatement) is what keeps a legitimate mid-task
		// re-plan from being flagged against an abandoned item list.
		if planItems != nil && jaccardSim(baselineWords, words) >= 0.4 {
			continue
		}
		planStepSeq = s.Seq
		baselineWords = words
		planItems = planItems[:0]
		for idx, it := range items {
			planItems = append(planItems, PlanItemAudit{
				Seq:  idx + 1,
				Text: it.Text,
			})
		}
	}
	if len(planItems) == 0 {
		return nil, 0
	}
	return &PlanAuditEvidencePack{
		PlanItems:    planItems,
		ActionsTaken: buildToolIndex(j, lang),
	}, planStepSeq
}

func detectLLMPlanMisalignment(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	pack, planStepSeq := buildPlanAuditPack(j, lang)
	if pack == nil {
		return nil
	}
	res, err := Interpret(ctx, opts, *pack, lang)
	if err != nil {
		return nil
	}
	var item planAuditResult
	if err := parseJSONFromLLM(res.Text, &item); err != nil {
		return nil
	}
	if item.HasMisalignment && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" {
		tx := i18n.StoryFindings(lang)
		fText := tx.PlanExecutionMisalignment(len(item.UnfulfilledItems), len(pack.PlanItems))
		evidence := sanitizeMDStruct(item.Explanation)
		if evidence == "" {
			evidence = fText.Evidence
		}
		action := sanitizeMDStruct(item.SuggestedAction)
		if action == "" {
			action = fText.Action
		}
		return []Finding{{
			Code:           FindingPlanExecutionMisalignment,
			StepSeq:        planStepSeq,
			Source:         SourceLLMInferred,
			Confidence:     ConfidenceHigh,
			EvidenceAnchor: item.EvidenceAnchor,
			Finding:        fText.Finding,
			Evidence:       evidence,
			Action:         action,
		}}
	}
	return nil
}

// --- P1b.6: Unverified Completion Claim (E2) --------------------------------

// buildCompletionClaimPack assembles the unverified-completion-claim
// detector's evidence pack from the final step's outcome plus the
// verification/error trail — nil when there is no final response text to
// audit.
func buildCompletionClaimPack(j *Journey) *CompletionClaimEvidencePack {
	steps := journeySteps(j)
	if len(steps) == 0 {
		return nil
	}
	lastStep := steps[len(steps)-1]
	outcome := lastStep.RespText
	if outcome == "" {
		return nil
	}
	var verifCmds []string
	var errEvents []string
	for _, s := range steps {
		for _, tc := range s.ToolCalls {
			if cand, ok := ExtractShellVerificationCandidate(tc); ok {
				verifCmds = append(verifCmds, tc.Name+": "+cand.Command)
			}
		}
		for _, ev := range s.NewEvents {
			if strings.Contains(ev.Msg.Text, isErrorMarker) {
				brief, _ := truncateText(ev.Msg.Text, 100)
				errEvents = append(errEvents, fmt.Sprintf("Step %d: %s", s.Seq, brief))
			}
		}
	}
	truncOutcome, _ := truncateText(outcome, 2000)
	truncReasoning, _ := truncateText(lastStep.Reasoning, 1000)
	return &CompletionClaimEvidencePack{
		FinalOutcome:          truncOutcome,
		FinalReasoning:        truncReasoning,
		VerificationCommands:  verifCmds,
		UnresolvedErrorEvents: errEvents,
	}
}

func detectLLMUnverifiedCompletionClaim(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	pack := buildCompletionClaimPack(j)
	if pack == nil {
		return nil
	}
	steps := journeySteps(j)
	lastStepSeq := steps[len(steps)-1].Seq
	res, err := Interpret(ctx, opts, *pack, lang)
	if err != nil {
		return nil
	}
	var item completionClaimResult
	if err := parseJSONFromLLM(res.Text, &item); err != nil {
		return nil
	}
	if strings.ToUpper(item.ClaimStatus) == "CLAIM_WITHOUT_VERIFICATION" && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" {
		tx := i18n.StoryFindings(lang)
		missing := sanitizeMDStruct(item.MissingVerification)
		fText := tx.UnverifiedCompletionClaim(missing)
		action := sanitizeMDStruct(item.SuggestedAction)
		if action == "" {
			action = fText.Action
		}
		return []Finding{{
			Code:           FindingUnverifiedCompletionClaim,
			StepSeq:        lastStepSeq,
			Source:         SourceLLMInferred,
			Confidence:     ConfidenceHigh,
			EvidenceAnchor: item.EvidenceAnchor,
			Finding:        fText.Finding,
			Evidence:       missing,
			Action:         action,
		}}
	}
	return nil
}

// --- Main LLM Findings Entry Point ------------------------------------------

// searchableTranscript concatenates a Journey's already-reconstructed text
// (RespText, Reasoning, ToolCall args, every NewEvents message) plus each
// Step's marshaled raw audit.Record into one blob — the "original text" an
// LLM EvidenceAnchor must be a literal substring of. Mirrors
// _eval/calibrate_p1b.go's transcriptPool: a streamed response's text
// arrives as many small SSE delta fragments, so a faithfully-quoted phrase
// survives contiguously only in the reassembled fields, while tool_result
// text (delivered whole in the following request body) and raw tool-call
// arguments are only visible in the marshaled record.
//
// The Journey no longer holds those records (Step.Rec is gone), so they are
// streamed back in with ctxgraph.ForEachRecord — one pass over this
// Journey's source files, discarded as marshaled. This is a single -journey
// -llm-addr path, so the extra read costs nothing. A read error yields
// what could be assembled plus the error; the caller treats a partial pool
// as fail-open (an anchor that can't be verified is dropped, not trusted).
func searchableTranscript(j *Journey) (string, error) {
	var b strings.Builder
	var locs []ctxgraph.Loc
	for _, t := range j.Tasks {
		for _, s := range t.Steps {
			b.WriteString(s.RespText)
			b.WriteByte('\n')
			b.WriteString(s.Reasoning)
			b.WriteByte('\n')
			for _, tc := range s.ToolCalls {
				b.WriteString(tc.Args)
				b.WriteByte('\n')
			}
			for _, ev := range s.NewEvents {
				b.WriteString(ev.Msg.Text)
				b.WriteByte('\n')
			}
			if s.Manifest != nil {
				locs = append(locs, ctxgraph.Loc{Path: s.Manifest.Path, Line: s.Manifest.Line})
			}
		}
	}
	var mu sync.Mutex
	err := ctxgraph.ForEachRecord(locs, func(_ ctxgraph.Loc, rec *audit.Record) {
		data, merr := json.Marshal(rec)
		if merr != nil {
			return
		}
		mu.Lock()
		b.Write(data)
		b.WriteByte('\n')
		mu.Unlock()
	})
	return b.String(), err
}

// anchoredInTranscript reports whether f cites an EvidenceAnchor that appears
// verbatim in pool. It is an anti-hallucination check and nothing more: it
// proves the model quoted the transcript instead of inventing a plausible
// quote. It is NOT an injection defense and must not be relied on as one —
// the transcript's own author can plant the quoted text, so a verifying
// anchor proves provenance, not trustworthiness. Injection containment
// lives in ComputeLLMFindings' StepSeq bounds check and pickDriver's
// SourceLLMInferred exclusion, never here.
//
// The pool also deliberately mixes authorship classes — model output, tool
// results, tool-call arguments and (through the marshaled records it
// appends) raw user input — because a streamed response's text survives
// contiguously only in the reassembled fields while tool_result text is
// visible only in the marshaled record. An anchor can therefore be
// satisfied by user-authored text; narrowing the pool to
// model-output/tool-result spans would require role-aware re-parsing of
// those records and is not done — a documented limitation, not a guarantee.
func anchoredInTranscript(f Finding, pool string) bool {
	return f.EvidenceAnchor != "" && strings.Contains(pool, f.EvidenceAnchor)
}

// ComputeLLMFindings runs all Phase 1b LLM semantic detectors against j.
// Fail-open: if LLM call fails or returns non-conforming output, errors are ignored
// and only valid high-confidence findings are returned. Every surviving
// finding's EvidenceAnchor is verified against the real transcript (B3) —
// one that doesn't appear verbatim is dropped, however confident the model
// claimed to be. A finding whose StepSeq is not one of the Journey's real
// step numbers is dropped too (never clamped — clamping would map an
// attacker-chosen sequence onto a legitimate step), and every LLM-authored
// text component is passed through sanitizeMDStruct before the Finding is
// handed to any renderer.
func ComputeLLMFindings(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) ([]Finding, error) {
	if !opts.Enabled() {
		return nil, nil
	}
	var raw []Finding
	raw = append(raw, detectLLMToolResultMisinterpretation(ctx, j, opts, lang)...)
	raw = append(raw, detectLLMSemanticOscillation(ctx, j, opts, lang)...)
	raw = append(raw, detectLLMGoalDrift(ctx, j, opts, lang)...)
	raw = append(raw, detectLLMConstraintDropped(ctx, j, opts, lang)...)
	raw = append(raw, detectLLMPlanMisalignment(ctx, j, opts, lang)...)
	raw = append(raw, detectLLMUnverifiedCompletionClaim(ctx, j, opts, lang)...)

	var out []Finding
	if len(raw) > 0 {
		validStep := map[int]bool{}
		for _, s := range journeySteps(j) {
			validStep[s.Seq] = true
		}
		pool, err := searchableTranscript(j)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: journey %s: anchor verification pool incomplete (%v) — unverifiable findings dropped\n", j.ID, err)
		}
		for _, f := range raw {
			if !validStep[f.StepSeq] {
				continue
			}
			if anchoredInTranscript(f, pool) {
				// only after verification: sanitizing first would break
				// the anchor's verbatim-transcript match
				f.EvidenceAnchor = sanitizeMDStruct(f.EvidenceAnchor)
				out = append(out, f)
			}
		}
	}

	sort.SliceStable(out, func(a, b int) bool {
		if out[a].StepSeq != out[b].StepSeq {
			return out[a].StepSeq < out[b].StepSeq
		}
		return out[a].Code < out[b].Code
	})
	return out, nil
}
