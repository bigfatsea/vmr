// Ver 2026-08-07, by Opus 5

// Pricing (P2.2) is a display-only SUMMARY of the pricing sources used to
// compute this report's $ estimates — not the resolution engine itself
// (that's internal/pricing.Resolver, threaded through Build/BuildCached's
// pricingSrc parameter in build_cached.go/aggregate.go). The two are
// separate on purpose: this package must never import internal/config (see
// CLAUDE.md's "report ↛ config" invariant, archtest-enforced), so cmd/vmr's
// composition root (cmd_report.go) is the only place that ever reads
// config.yaml, resolves the standard/curated/supplement tables, and builds
// BOTH a *pricing.Resolver (for aggregate.go to actually price records)
// and this summary (for rendering — currency label, disclaimer text, and
// the "what pricing sources fed this report" line in §2's appendix).
//
// This replaces the pre-P2.2 sidecar (a single hand-maintained pricing.yaml
// file, its raw bytes frozen verbatim into every report for traceability —
// see git history for that implementation). P2.2's pricing model is
// inherently multi-layered (embedded standard table + optional supplement +
// per-provider config.yaml overrides), so there is no longer one file's
// worth of bytes to freeze; this summary is the replacement traceability
// mechanism.
package report

import (
	"vmr/internal/i18n"
)

// Pricing summarizes what fed a report's $ estimates. Nil = no pricing
// data resolved anything at all (same "no $ column" degrade as before).
type Pricing struct {
	Currency string `json:"currency,omitempty"`
	// StandardGeneratedAt is the embedded standard table's generation date
	// (internal/pricing.Table.GeneratedAt) — the "is this stale" signal
	// the design doc's §4.2③ guardrail requires be visible somewhere.
	StandardGeneratedAt string `json:"standard_generated_at,omitempty"`
	// Supplement is the user supplement table's path, if config.yaml's
	// pricing.supplement configured one; empty otherwise.
	Supplement string `json:"supplement,omitempty"`
	// ProviderOverrides is the total providers[].pricing.overrides rule
	// count across every provider config.yaml declared pricing for — a
	// quick "was any account-specific override in effect" signal.
	ProviderOverrides int `json:"provider_overrides,omitempty"`
}

// Disclaimer renders the cost-estimate disclaimer for the appendix/footer.
// nil-safe (mirrors the pre-P2.2 sidecar's Disclaimer contract).
func (p *Pricing) Disclaimer(lang i18n.Lang) string {
	if p == nil {
		return ""
	}
	asOf := p.StandardGeneratedAt
	if asOf == "" {
		asOf = "(unknown date)"
	}
	cur := p.Currency
	if cur == "" {
		cur = "USD"
	}
	return i18n.Cost(lang).Disclaimer(asOf, cur)
}
