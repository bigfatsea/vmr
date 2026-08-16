// Ver 2026-08-16 23:20, by gemini-3.7-flash

// Rendering helpers for LLM-inferred findings with confidence and anchor tags.
package story

import "vmr/internal/i18n"

// formatFindingHeader renders the header line for a finding, adding an [AI推测] tag
// and confidence badge when Source == SourceLLMInferred. When all carries another
// Finding with the same Code but a different Source (a rule detector and an LLM
// detector both independently firing on the same Code — see P1b.5's
// FindingPlanExecutionMisalignment, produced by both detectPlanExecutionMisalignment
// and detectLLMPlanMisalignment), a rule-sourced entry is also tagged (normally left
// bare) so the two independent hits read as clearly distinct verdicts rather than a
// duplicate.
func formatFindingHeader(i int, f Finding, all []Finding, t i18n.SpineText, lang i18n.Lang) string {
	codeStr := string(f.Code)
	switch {
	case f.Source == SourceLLMInferred:
		if lang == i18n.ZH {
			codeStr += " [AI推测 · 置信度: " + string(f.Confidence) + "]"
		} else {
			codeStr += " [AI Inferred · " + string(f.Confidence) + "]"
		}
	case hasMixedSourceHit(all, f.Code):
		if lang == i18n.ZH {
			codeStr += " [规则检测]"
		} else {
			codeStr += " [Rule-detected]"
		}
	}
	return t.FindingHeader(i+1, codeStr, f.StepSeq)
}

// hasMixedSourceHit reports whether findings contains both a SourceLLMInferred
// and a non-LLM entry for code — the case formatFindingHeader needs to
// disambiguate. Findings lists are small (per-Journey, bounded by how many
// detectors can fire), so the O(n) scan per call is not worth memoizing.
func hasMixedSourceHit(findings []Finding, code FindingCode) bool {
	sawLLM, sawOther := false, false
	for _, f := range findings {
		if f.Code != code {
			continue
		}
		if f.Source == SourceLLMInferred {
			sawLLM = true
		} else {
			sawOther = true
		}
	}
	return sawLLM && sawOther
}

// formatFindingEvidence formats the evidence block, including evidence_anchor if present.
func formatFindingEvidence(f Finding, t i18n.SpineText, lang i18n.Lang) string {
	ev := f.Evidence
	if f.EvidenceAnchor != "" {
		anchorLabel := "原文证据锚点："
		if lang != i18n.ZH {
			anchorLabel = "Evidence Anchor: "
		}
		if ev != "" {
			ev += "\n   " + anchorLabel + f.EvidenceAnchor
		} else {
			ev = anchorLabel + f.EvidenceAnchor
		}
	}
	if ev == "" {
		return ""
	}
	return t.FindingEvidence(ev)
}
