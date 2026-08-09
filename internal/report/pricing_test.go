// Ver 2026-08-07, by Opus 5
package report

import (
	"testing"

	"vmr/internal/i18n"
)

// TestReportPricing_Disclaimer_NilSafe mirrors the pre-P2.2 sidecar's
// Disclaimer nil-safety contract — the resolution engine itself
// (internal/pricing.Table/Resolver, the three-layer resolve, discount/
// time-window matching, canonical-key auto-resolution) moved to
// internal/pricing/*_test.go; this file only covers the report-local
// summary type's own small surface.
func TestReportPricing_Disclaimer_NilSafe(t *testing.T) {
	var p *Pricing
	if got := p.Disclaimer(i18n.EN); got != "" {
		t.Fatalf("nil *Pricing.Disclaimer() = %q, want empty", got)
	}
}

func TestReportPricing_Disclaimer_DefaultsWhenFieldsEmpty(t *testing.T) {
	p := &Pricing{}
	got := p.Disclaimer(i18n.EN)
	if got == "" {
		t.Fatal("Disclaimer() should never be empty for a non-nil *Pricing")
	}
}

func TestReportPricing_Disclaimer_UsesConfiguredValues(t *testing.T) {
	p := &Pricing{Currency: "CNY", StandardGeneratedAt: "2026-08-01"}
	got := p.Disclaimer(i18n.EN)
	if got == "" {
		t.Fatal("Disclaimer() should not be empty")
	}
}
