// Ver 2026-08-07, by Opus 5

// Package report includes Pricing summary metadata used when rendering report cost estimates.
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
// nil-safe (mirrors an earlier sidecar's Disclaimer contract).
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
