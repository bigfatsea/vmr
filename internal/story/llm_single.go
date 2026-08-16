// Ver 2026-08-05, by Sonnet 5

// The single-Journey LLM interpretation layer — the -journey counterpart to
// llm.go's -compare EvidencePack — see llm.go's evidencePackKind doc
// comment for how the two coexist through the same Interpret/cacheKey
// call chain without either needing a special case. Deliberately not a
// shared "evidence pack builder" abstraction with EvidencePack/
// DivergenceEvidencePack — the dev plan review's own call (three
// different-shaped packs don't share enough to be worth forcing into one
// interface beyond promptSpec).
package story

import (
	"strings"

	"vmr/internal/i18n"
)

// SuspiciousToolPair captures a tool call result and the subsequent model reasoning
// for downstream semantic evaluation (e.g. tool result misinterpretation).
type SuspiciousToolPair struct {
	StepSeq        int    `json:"step_seq"`
	ToolName       string `json:"tool_name"`
	ToolResultText string `json:"tool_result_text"`
	NextReasoning  string `json:"next_reasoning"`
}

// SingleJourneyEvidencePack is the single-Journey LLM layer's entire input:
// j's already-computed Metrics and Findings (findings.go's rule-derived "candidate, not verdict" list —
// this is exactly why Finding's shape was designed to "be ready" for this,
// see findings.go's own doc comment) plus the same per-Step tool index
// llm.go's EvidencePack already uses for -compare, so the model can narrate
// working style/phases the same way it does there.
type SingleJourneyEvidencePack struct {
	Journey         JourneyRef           `json:"journey"`
	Metrics         Metrics              `json:"metrics"`
	Findings        []Finding            `json:"findings"`
	TaskTitles      []string             `json:"task_titles"`
	ToolIndex       []ToolIndexEntry     `json:"tool_index"`
	UserIntent      string               `json:"user_intent,omitempty"`
	FinalOutcome    string               `json:"final_outcome,omitempty"`
	SuspiciousPairs []SuspiciousToolPair `json:"suspicious_pairs,omitempty"`
}

// BuildSingleJourneyEvidencePack assembles j/m/findings into the bounded
// evidence pack the LLM prompt embeds — m/findings are passed in rather
// than recomputed so a caller that already has them (cmd_story.go's
// writeJourneyFile always does) doesn't pay for ComputeMetrics/
// ComputeFindings a second time.
func BuildSingleJourneyEvidencePack(j *Journey, m Metrics, findings []Finding, lang i18n.Lang) SingleJourneyEvidencePack {
	return SingleJourneyEvidencePack{
		Journey:         JourneyRef{ID: j.ID, Title: j.Title, From: j.From, To: j.To},
		Metrics:         m,
		Findings:        findings,
		TaskTitles:      journeyTaskTitles(j),
		ToolIndex:       buildToolIndex(j, lang),
		UserIntent:      extractRootUserIntent(j),
		FinalOutcome:    extractFinalOutcome(j),
		SuspiciousPairs: extractSuspiciousToolPairs(j),
	}
}

func extractRootUserIntent(j *Journey) string {
	for _, t := range j.Tasks {
		for _, s := range t.Steps {
			for _, ev := range s.NewEvents {
				if ev.Msg.Role == "user" && ev.Msg.Text != "" {
					t, _ := truncateText(ev.Msg.Text, 2000)
					return t
				}
			}
		}
	}
	return ""
}

func extractFinalOutcome(j *Journey) string {
	steps := journeySteps(j)
	if len(steps) == 0 {
		return ""
	}
	last := steps[len(steps)-1]
	if last.RespText != "" {
		t, _ := truncateText(last.RespText, 2000)
		return t
	}
	return ""
}

func extractSuspiciousToolPairs(j *Journey) []SuspiciousToolPair {
	steps := journeySteps(j)
	if len(steps) < 2 {
		return nil
	}
	var pairs []SuspiciousToolPair
	for i := 0; i < len(steps)-1; i++ {
		s := steps[i]
		if len(s.ToolCalls) == 0 {
			continue
		}
		results := toolResultsFor(steps, i)
		if len(results) == 0 {
			continue
		}
		nextStep := steps[i+1]
		nextReasoning := nextStep.Reasoning
		if nextReasoning == "" {
			nextReasoning = nextStep.RespText
		}
		if nextReasoning == "" {
			continue
		}

		for _, r := range results {
			lowerText := strings.ToLower(r.Text)
			isCandidate := r.IsError ||
				strings.Contains(lowerText, "error") ||
				strings.Contains(lowerText, "fail") ||
				strings.Contains(lowerText, "not found") ||
				strings.Contains(lowerText, "denied") ||
				strings.Contains(lowerText, "exception") ||
				strings.Contains(lowerText, "fatal") ||
				strings.Contains(lowerText, "invalid")

			if !isCandidate {
				continue
			}

			toolName := ""
			for _, tc := range s.ToolCalls {
				if tc.ID == r.CallID {
					toolName = tc.Name
					break
				}
			}
			if toolName == "" && len(s.ToolCalls) > 0 {
				toolName = s.ToolCalls[0].Name
			}

			truncResult, _ := truncateText(r.Text, 500)
			truncReasoning, _ := truncateText(nextReasoning, 800)

			pairs = append(pairs, SuspiciousToolPair{
				StepSeq:        s.Seq,
				ToolName:       toolName,
				ToolResultText: truncResult,
				NextReasoning:  truncReasoning,
			})
			if len(pairs) >= 10 {
				return pairs
			}
		}
	}
	return pairs
}

func (p SingleJourneyEvidencePack) EstimateChars() int { return packChars(p) }

func (SingleJourneyEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version: "single-journey-llm-v1", System: t.SingleJourneySystemPrompt,
		UserPrefix: t.SingleJourneyUserPromptPrefix, UserSuffix: t.SingleJourneyUserPromptSuffix,
	}
}
