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

import "vmr/internal/i18n"

// SingleJourneyEvidencePack is the single-Journey LLM layer's entire input:
// j's already-computed Metrics and Findings (findings.go's rule-derived "candidate, not verdict" list —
// this is exactly why Finding's shape was designed to "be ready" for this,
// see findings.go's own doc comment) plus the same per-Step tool index
// llm.go's EvidencePack already uses for -compare, so the model can narrate
// working style/phases the same way it does there.
type SingleJourneyEvidencePack struct {
	Journey    JourneyRef       `json:"journey"`
	Metrics    Metrics          `json:"metrics"`
	Findings   []Finding        `json:"findings"`
	TaskTitles []string         `json:"task_titles"`
	ToolIndex  []ToolIndexEntry `json:"tool_index"`
}

// BuildSingleJourneyEvidencePack assembles j/m/findings into the bounded
// evidence pack the LLM prompt embeds — m/findings are passed in rather
// than recomputed so a caller that already has them (cmd_story.go's
// writeJourneyFile always does) doesn't pay for ComputeMetrics/
// ComputeFindings a second time.
func BuildSingleJourneyEvidencePack(j *Journey, m Metrics, findings []Finding, lang i18n.Lang) SingleJourneyEvidencePack {
	return SingleJourneyEvidencePack{
		Journey:    JourneyRef{ID: j.ID, Title: j.Title, From: j.From, To: j.To},
		Metrics:    m,
		Findings:   findings,
		TaskTitles: journeyTaskTitles(j),
		ToolIndex:  buildToolIndex(j, lang),
	}
}

func (p SingleJourneyEvidencePack) EstimateChars() int { return packChars(p) }

func (SingleJourneyEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version: "single-journey-llm-v1", System: t.SingleJourneySystemPrompt,
		UserPrefix: t.SingleJourneyUserPromptPrefix, UserSuffix: t.SingleJourneyUserPromptSuffix,
	}
}
