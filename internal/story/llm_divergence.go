// Ver 2026-08-05, by Sonnet 5

// The divergence-point LLM interpretation layer — the -compare counterpart
// to llm.go's overall EvidencePack — see llm.go's evidencePackKind doc
// comment for how the three pack shapes coexist through the same
// Interpret/cacheKey call chain. The evidence pack is deliberately scoped
// to a small window around the divergence point (not the two full
// Journeys): docs/future-strategy/vmr_story_journey_deepdive_sonnet-5.md's
// own EvidencePack discipline is "a restricted evidence package, must
// declare what it can't see" — the same discipline already applies to
// -compare's system-prompt/deliverable excerpts, reused here for a
// different evidence source (Step briefs, not raw text).
package story

import (
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

// divergenceContextWindow bounds how many Steps before/after the
// divergence point are included on each side — enough to see the
// immediate lead-up and aftermath without ballooning into "just send the
// whole Journey", which would defeat the point of a scoped evidence pack.
const divergenceContextWindow = 2

// DivergenceStepBrief is one Step's compact summary for the divergence
// evidence pack — same shape/purpose as llm.go's ToolIndexEntry (a
// condensed, rule-generated line, not raw content), kept as its own type
// rather than reusing ToolIndexEntry since a reader of the JSON pack
// shouldn't have to guess why a "tool index" field shows up inside a
// divergence-focused pack.
type DivergenceStepBrief struct {
	Seq   int      `json:"seq"`
	Tools []string `json:"tools,omitempty"`
	Brief string   `json:"brief"`
}

// DivergenceEvidencePack is the divergence-point LLM layer's entire input:
// the already-computed DivergencePoint fact (compare.go's divergence-point
// detection — which Step, light/heavy severity, which tools) plus a bounded
// window of DivergenceStepBrief entries before/at/after it on each side.
type DivergenceEvidencePack struct {
	Divergence DivergencePoint `json:"divergence"`

	BeforeA []DivergenceStepBrief `json:"before_a,omitempty"`
	AtA     *DivergenceStepBrief  `json:"at_a,omitempty"`
	AfterA  []DivergenceStepBrief `json:"after_a,omitempty"`

	BeforeB []DivergenceStepBrief `json:"before_b,omitempty"`
	AtB     *DivergenceStepBrief  `json:"at_b,omitempty"`
	AfterB  []DivergenceStepBrief `json:"after_b,omitempty"`
}

// BuildDivergenceEvidencePack assembles jA/jB/div into the bounded
// evidence pack the LLM prompt embeds. div must come from
// computeDivergence(jA, jB) — a DivergencePoint computed against any other
// pair of Journeys would index into the wrong aligned sequence. When
// div.Found is false, the returned pack carries only the (empty)
// DivergencePoint — callers should treat that the same way -compare's own
// dry-run treats "nothing to interpret" and skip calling Interpret at all.
func BuildDivergenceEvidencePack(jA, jB *Journey, div DivergencePoint, lang i18n.Lang) DivergenceEvidencePack {
	pack := DivergenceEvidencePack{Divergence: div}
	if !div.Found {
		return pack
	}
	a, b := flattenWithTask(jA), flattenWithTask(jB)
	if div.Index >= len(a) || div.Index >= len(b) {
		return pack // defensive: div wasn't computed from this exact (jA, jB) pair
	}
	pack.BeforeA, pack.AtA, pack.AfterA = divergenceStepBriefs(a, div.Index, lang)
	pack.BeforeB, pack.AtB, pack.AfterB = divergenceStepBriefs(b, div.Index, lang)
	return pack
}

func divergenceStepBriefs(steps []alignedStep, center int, lang i18n.Lang) (before []DivergenceStepBrief, at *DivergenceStepBrief, after []DivergenceStepBrief) {
	mk := func(as alignedStep) DivergenceStepBrief {
		s := as.step
		var tools []string
		for _, tc := range s.ToolCalls {
			tools = append(tools, tc.Name)
		}
		brief := taskseg.Preview(s.RespText)
		if brief == "" {
			brief = i18n.LLM(lang).NoTextReply
		}
		return DivergenceStepBrief{Seq: s.Seq, Tools: tools, Brief: brief}
	}

	start := center - divergenceContextWindow
	if start < 0 {
		start = 0
	}
	for i := start; i < center; i++ {
		before = append(before, mk(steps[i]))
	}
	atBrief := mk(steps[center])
	at = &atBrief
	end := center + divergenceContextWindow + 1
	if end > len(steps) {
		end = len(steps)
	}
	for i := center + 1; i < end; i++ {
		after = append(after, mk(steps[i]))
	}
	return
}

func (p DivergenceEvidencePack) EstimateChars() int { return packChars(p) }

func (DivergenceEvidencePack) promptSpec(lang i18n.Lang) promptSpec {
	t := i18n.LLM(lang)
	return promptSpec{
		Version: "divergence-llm-v1", System: t.DivergenceSystemPrompt,
		UserPrefix: t.DivergenceUserPromptPrefix, UserSuffix: t.DivergenceUserPromptSuffix,
	}
}
