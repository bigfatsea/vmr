// Ver 2026-09-15, by pi

// The persisted half of the LLM interpretation layer: the
// LLMInterpretation record (what -llm-addr's outcome becomes inside
// j-<id>.json's llm_interpretation / compare-*.json's
// llm_interpretation+llm_divergence, design doc §3.6) and its one
// renderer. Split out of llm.go when that file grew past its archtest
// budget: llm.go owns the calling/caching machinery, this file owns the
// record both renderers consume — the .md's LLM section is a pure function
// of the record, which is what lets -render-only reproduce it from the
// JSON alone (the removed bypass-splice scraped it from the previous .md).
package journey

import (
	"strings"

	"vmr/internal/i18n"
)

// Scope tokens for LLMInterpretation.Scope — canonical identifiers, mapped
// to localized labels only at render time (scopeTitleLabel), so the JSON
// never carries locale text. "" is a document's single LLM section (a
// single-journey report); LLMScopeOverall/LLMScopeDivergence distinguish
// compare-*.md's two possible sections.
const (
	LLMScopeOverall    = "overall"
	LLMScopeDivergence = "divergence"
)

// Status values for LLMInterpretation.Status.
const (
	LLMStatusOK     = "ok"
	LLMStatusFailed = "failed"
)

// LLMInterpretation is one Interpret call's persisted outcome —
// JourneySummary's / Comparison's llm_interpretation (and the divergence
// call's llm_divergence) field. Design doc §3.6: the interpretation lands in
// the journey JSON (模型、耗时、状态与正文) so the .md's LLM section is a pure
// function of the JSON — that is what lets -render-only reproduce it without
// scraping the old .md (the removed bypass-splice hack). Text carries the
// model's raw reply; downgradeHeadingLevels stays a render-layer decision.
// A failed call is recorded too (Status "failed" + Error) — the .md renders
// nothing for it, exactly as the full run left the section out — so the JSON
// is honest about "we tried and it failed" instead of silently absent.
type LLMInterpretation struct {
	Model string `json:"model"`
	// Scope: "" (single-journey report), LLMScopeOverall or
	// LLMScopeDivergence. Localized at render time.
	Scope      string `json:"scope,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Cached     bool   `json:"cached,omitempty"`
	Text       string `json:"text,omitempty"`
}

// NewLLMInterpretation records one Interpret call's outcome for persistence.
// err != nil yields a "failed" record (Text empty, Error set); success
// yields "ok" with the model's raw reply. elapsed is the caller's observed
// wall-clock span of the call.
func NewLLMInterpretation(opts LLMOptions, res InterpretResult, err error, scope string) *LLMInterpretation {
	rec := &LLMInterpretation{
		Model:      opts.Model,
		Scope:      scope,
		DurationMS: res.Duration.Milliseconds(),
		Cached:     res.Cached,
		Text:       res.Text,
	}
	if err != nil {
		rec.Status = LLMStatusFailed
		rec.Error = err.Error()
		rec.Text = ""
		rec.Cached = false
		return rec
	}
	rec.Status = LLMStatusOK
	return rec
}

// scopeTitleLabel maps a record's canonical scope token to the localized
// scope label SectionTitle appends — the same labels the old direct
// RenderLLMSection call sites passed in. An unknown token renders as-is
// (a forward-compat reader of a newer JSON still gets a distinguishable
// heading rather than an empty scope slot).
func scopeTitleLabel(lang i18n.Lang, scope string) string {
	t := i18n.LLM(lang)
	switch scope {
	case LLMScopeOverall:
		return t.ScopeOverall
	case LLMScopeDivergence:
		return t.ScopeDivergence
	}
	return scope
}

// RenderLLMSection renders one persisted interpretation record with the
// "this is interpretation, not fact" banner — always rendered as its own
// clearly separated section, never blended into the fact-layer sections
// above it. A nil or failed record renders nothing: the full run also left
// the .md without a section in those cases, so this is what keeps
// -render-only byte-identical (D11). scope on the record distinguishes
// compare-*.md's two possible sections (see LLMInterpretation.Scope).
func RenderLLMSection(rec *LLMInterpretation, lang i18n.Lang) string {
	if rec == nil || rec.Status != LLMStatusOK {
		return ""
	}
	t := i18n.LLM(lang)
	var b strings.Builder
	b.WriteString(t.SectionTitle(rec.Model, scopeTitleLabel(lang, rec.Scope)))
	b.WriteString(t.SectionDisclaimer(rec.Model))
	if rec.Cached {
		b.WriteString(t.CachedNote)
	}
	b.WriteString("\n")
	b.WriteString(downgradeHeadingLevels(rec.Text))
	b.WriteString("\n")
	return b.String()
}
