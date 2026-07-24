// Ver 2026-07-25, report2

// Optional pricing sidecar (V2 §4). A local hand-maintained YAML file mapping
// each endpoint (provider:real-model) to per-1M-token unit prices. When absent
// (the default), no $ appears anywhere in the report - the report degrades
// gracefully to token-class accounting, with a one-line "configure pricing to
// see $ estimates" note. When present, cost_estimate fields populate on the
// buckets and a disclaimer renders in the appendix.
//
// Pricing is strictly local: never transmitted, never fetched from a live API
// (that would violate the single-binary, no-external-service stance). The
// updated_at date is rendered as a disclaimer - "cost estimates are based on
// the prices current as of <date>, not the prices in effect when the
// historical requests actually ran" - so an old report's $ column can't be
// mistaken for a real bill.

package report2

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// pricingFile is the on-disk shape of pricing.yaml (lowercase yaml field tags).
type pricingFile struct {
	Currency  string        `yaml:"currency"`
	UpdatedAt string        `yaml:"updated_at"`
	Rates     []PricingRate `yaml:"rates"`
}

// LoadPricing reads the optional pricing sidecar. Returns (nil, nil) when the
// file does not exist (pricing is opt-in). Returns an error only for a file
// that exists but fails to parse.
func LoadPricing(path string) (*Pricing, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pricing %s: %w", path, err)
	}
	var pf pricingFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parse pricing %s: %w", path, err)
	}
	p := &Pricing{
		Currency:   pf.Currency,
		UpdatedAt:  pf.UpdatedAt,
		Rates:      pf.Rates,
		byEndpoint: map[string]PricingRate{},
	}
	for _, r := range pf.Rates {
		// Normalize: keep the endpoint label exactly as the audit record's
		// attempt.Endpoint (provider/real-model). The user's pricing.yaml is
		// expected to use the same labels `vmr check` / the report show.
		p.byEndpoint[r.Endpoint] = r
	}
	if p.Currency == "" {
		p.Currency = "CNY"
	}
	return p, nil
}

// Disclaimer renders the cost-estimate disclaimer for the appendix / footer.
func (p *Pricing) Disclaimer() string {
	if p == nil {
		return ""
	}
	asOf := p.UpdatedAt
	if asOf == "" {
		asOf = "(unknown date)"
	}
	cur := p.Currency
	if cur == "" {
		cur = "CNY"
	}
	return fmt.Sprintf("成本估算基于 %s 的价格配置（货币 %s），不代表报告所涵盖历史请求实际发生时的价格。", asOf, cur)
}
