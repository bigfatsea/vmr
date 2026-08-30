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

// --- P1b.1: Tool Result Misinterpretation (E3) ------------------------------

func detectLLMToolResultMisinterpretation(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	pairs := extractSuspiciousToolPairs(j)
	if len(pairs) == 0 {
		return nil
	}
	pack := ToolMisinterpretationEvidencePack{SuspiciousPairs: pairs}
	res, err := Interpret(ctx, opts, pack, lang)
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
			for _, p := range pairs {
				if p.StepSeq == item.StepSeq {
					tool = p.ToolName
					break
				}
			}
			fText := tx.ToolResultMisinterpretation(tool, item.Explanation)
			action := item.SuggestedAction
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

func detectLLMSemanticOscillation(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	steps := journeySteps(j)
	cands := detectOscillationCandidates(steps)
	if len(cands) == 0 {
		return nil
	}
	pack := SemanticOscillationEvidencePack{Candidates: cands}
	res, err := Interpret(ctx, opts, pack, lang)
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
			for _, c := range cands {
				for _, call := range c.Calls {
					if call.StepSeq == item.StepSeq {
						tool = c.ToolName
						break
					}
				}
			}
			fText := tx.SemanticOscillation(tool, item.Explanation)
			action := item.SuggestedBreakout
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

func detectLLMGoalDrift(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
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
	pack := GoalDriftEvidencePack{
		RootUserIntent: intent,
		Checkpoints:    checkpts,
	}
	res, err := Interpret(ctx, opts, pack, lang)
	if err != nil {
		return nil
	}
	var item goalDriftResult
	if err := parseJSONFromLLM(res.Text, &item); err != nil {
		return nil
	}
	if item.DriftDetected && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" && item.DriftStepSeq > 0 {
		tx := i18n.StoryFindings(lang)
		fText := tx.GoalDrift(item.DriftStepSeq, item.DriftExplanation)
		action := item.SuggestedAction
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

func detectLLMConstraintDropped(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	steps := journeySteps(j)
	var excerpts []CompactionExcerpt
	for _, s := range steps {
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
	pack := CompactionConstraintEvidencePack{Excerpts: excerpts}
	res, err := Interpret(ctx, opts, pack, lang)
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
			fText := tx.LLMConstraintDropped(item.EvidenceAnchor)
			evidence := item.Explanation
			action := item.SuggestedAction
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

func detectLLMPlanMisalignment(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
	steps := journeySteps(j)
	if len(steps) < 2 {
		return nil
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
		return nil
	}
	toolIdx := buildToolIndex(j, lang)
	pack := PlanAuditEvidencePack{
		PlanItems:    planItems,
		ActionsTaken: toolIdx,
	}
	res, err := Interpret(ctx, opts, pack, lang)
	if err != nil {
		return nil
	}
	var item planAuditResult
	if err := parseJSONFromLLM(res.Text, &item); err != nil {
		return nil
	}
	if item.HasMisalignment && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" {
		tx := i18n.StoryFindings(lang)
		fText := tx.PlanExecutionMisalignment(len(item.UnfulfilledItems), len(planItems))
		evidence := item.Explanation
		if evidence == "" {
			evidence = fText.Evidence
		}
		action := item.SuggestedAction
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

func detectLLMUnverifiedCompletionClaim(ctx context.Context, j *Journey, opts LLMOptions, lang i18n.Lang) []Finding {
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
	pack := CompletionClaimEvidencePack{
		FinalOutcome:          truncOutcome,
		FinalReasoning:        truncReasoning,
		VerificationCommands:  verifCmds,
		UnresolvedErrorEvents: errEvents,
	}
	res, err := Interpret(ctx, opts, pack, lang)
	if err != nil {
		return nil
	}
	var item completionClaimResult
	if err := parseJSONFromLLM(res.Text, &item); err != nil {
		return nil
	}
	if strings.ToUpper(item.ClaimStatus) == "CLAIM_WITHOUT_VERIFICATION" && strings.ToUpper(item.Confidence) == string(ConfidenceHigh) && item.EvidenceAnchor != "" {
		tx := i18n.StoryFindings(lang)
		fText := tx.UnverifiedCompletionClaim(item.MissingVerification)
		action := item.SuggestedAction
		if action == "" {
			action = fText.Action
		}
		return []Finding{{
			Code:           FindingUnverifiedCompletionClaim,
			StepSeq:        lastStep.Seq,
			Source:         SourceLLMInferred,
			Confidence:     ConfidenceHigh,
			EvidenceAnchor: item.EvidenceAnchor,
			Finding:        fText.Finding,
			Evidence:       item.MissingVerification,
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
// verbatim in pool. An LLM detector that fabricates an anchor (a plausible
// quote that was never in the transcript) would otherwise be promoted to a
// HIGH-confidence [AI推测] Finding and fed back as established fact into the
// next single-Journey interpretation pass — the whole "only anchor-backed
// claims are HIGH" safety contract rests on this check, which until B3 lived
// only in _eval/calibrate_p1b.go and was never enforced at runtime.
func anchoredInTranscript(f Finding, pool string) bool {
	return f.EvidenceAnchor != "" && strings.Contains(pool, f.EvidenceAnchor)
}

// ComputeLLMFindings runs all Phase 1b LLM semantic detectors against j.
// Fail-open: if LLM call fails or returns non-conforming output, errors are ignored
// and only valid high-confidence findings are returned. Every surviving
// finding's EvidenceAnchor is verified against the real transcript (B3) —
// one that doesn't appear verbatim is dropped, however confident the model
// claimed to be.
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
		pool, err := searchableTranscript(j)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: journey %s: anchor verification pool incomplete (%v) — unverifiable findings dropped\n", j.ID, err)
		}
		for _, f := range raw {
			if anchoredInTranscript(f, pool) {
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
