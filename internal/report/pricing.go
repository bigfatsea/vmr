// Ver 2026-07-26, by Sonnet 5

// Optional pricing sidecar (V2 §4). A local hand-maintained YAML file mapping
// each provider+model to per-1M-token unit prices. When absent (the
// default), no $ appears anywhere in the report - the report degrades
// gracefully to token-class accounting, with a one-line "configure pricing to
// see $ estimates" note. When present, cost_estimate fields populate on the
// buckets and a disclaimer renders in the appendix.
//
// Pricing is strictly local: never transmitted, never fetched from a live API
// (that would violate the single-binary, no-external-service stance). The
// updated_at date is rendered as a disclaimer - "cost estimates are based on
// the prices current as of <date>, not the prices in effect when the
// historical requests actually ran" - so an old report's $ column can't be
// mistaken for a real bill. The raw file bytes are also embedded verbatim
// (collapsed) in §2 of the rendered report, freezing the exact price
// snapshot a report's $ figures came from - pricing.yaml can keep evolving
// without making an already-generated report's numbers ambiguous.
//
// Rates key on provider+model only, not protocol: the same upstream
// account/model costs the same whether reached over vmr's openai or
// anthropic ingress surface, so there's no reason to duplicate a rate per
// protocol the way endpoint labels ("protocol:provider:model") do elsewhere
// in this package.
//
// Price fields accept a bare number (uses the file's own top-level
// currency) or a string with a leading currency code ("USD1.2", "jpy 1.2" -
// case-insensitive, space optional), converted to the file's currency via
// exchange_rate at load time. exchange_rate entries pair currencies at an
// equivalent amount ({USD: 1, CNY: 7} means 1 USD = 7 CNY) and are walked as
// a graph (BFS), so a chain like USD->EUR->CNY resolves even without a
// direct USD->CNY entry. A price naming a currency that isn't reachable
// this way - not the base currency, not connected through any
// exchange_rate entry - is a load-time error, not a silent zero.
package report

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"vmr/internal/i18n"
)

// Pricing is the optional sidecar config. Nil/absent => no $ anywhere.
type Pricing struct {
	Currency  string        `json:"currency,omitempty"`
	UpdatedAt string        `json:"updated_at,omitempty"`
	Rates     []PricingRate `json:"rates"`

	// Raw is the exact file bytes as read from disk - embedded verbatim
	// (collapsed) in §2 so a report's cost figures stay traceable to the
	// pricing snapshot that produced them, even after pricing.yaml changes.
	// Empty for a Pricing built by hand (tests) rather than via LoadPricing.
	Raw []byte `json:"-"`

	// byKey indexes Rates by rateKey(provider, model); each slice keeps
	// file order, since RateFor's window matching is first-match-wins.
	byKey map[string][]PricingRate `json:"-"`
}

// PricingRate is one provider+model's unit prices (per 1M tokens), already
// converted to Pricing.Currency. Protocol-agnostic - see the package doc
// comment for why.
//
// DateFrom/DateTo ("2026-07-01", yyyy-MM-dd) and HourFrom/HourTo ("22:00",
// HH:MM - a from>to pair wraps past midnight, e.g. 22:00..06:00) are each
// independently optional; a rate with none set matches any time. A
// provider+model can have several PricingRate entries - e.g. an off-peak
// discount plus a full-price fallback - and RateFor returns the first (in
// pricing.yaml's own rates: order) whose window contains the request
// timestamp, so a narrower/promotional window must be listed before the
// catch-all entry it overrides.
type PricingRate struct {
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	InFreshPer1M    float64 `json:"in_fresh_per_1m"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m"`
	CacheWritePer1M float64 `json:"cache_write_per_1m"`
	OutPer1M        float64 `json:"out_per_1m"`
	DateFrom        string  `json:"date_from,omitempty"`
	DateTo          string  `json:"date_to,omitempty"`
	HourFrom        string  `json:"hour_from,omitempty"`
	HourTo          string  `json:"hour_to,omitempty"`
}

// matches reports whether ts falls inside r's optional date/hour window.
func (r PricingRate) matches(ts time.Time) bool {
	if r.DateFrom != "" && ts.Format("2006-01-02") < r.DateFrom {
		return false
	}
	if r.DateTo != "" && ts.Format("2006-01-02") > r.DateTo {
		return false
	}
	switch {
	case r.HourFrom != "" && r.HourTo != "":
		h := ts.Format("15:04")
		if r.HourFrom <= r.HourTo {
			if h < r.HourFrom || h > r.HourTo {
				return false
			}
		} else if h > r.HourTo && h < r.HourFrom { // wraps midnight
			return false
		}
	case r.HourFrom != "":
		if ts.Format("15:04") < r.HourFrom {
			return false
		}
	case r.HourTo != "":
		if ts.Format("15:04") > r.HourTo {
			return false
		}
	}
	return true
}

// rateKey normalizes a provider+model pair for byKey lookups - case
// doesn't distinguish two otherwise-identical rules.
func rateKey(provider, model string) string {
	return strings.ToLower(provider) + "\x00" + strings.ToLower(model)
}

// RateFor returns the price rule matching provider+model at ts (nil-safe:
// a nil *Pricing always misses). provider/model are matched
// case-insensitively; see PricingRate for how ts picks among several rules
// for the same provider+model.
func (p *Pricing) RateFor(provider, model string, ts time.Time) (PricingRate, bool) {
	if p == nil {
		return PricingRate{}, false
	}
	for _, r := range p.byKey[rateKey(provider, model)] {
		if r.matches(ts) {
			return r, true
		}
	}
	return PricingRate{}, false
}

// Disclaimer renders the cost-estimate disclaimer for the appendix / footer.
func (p *Pricing) Disclaimer(lang i18n.Lang) string {
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
	return i18n.Cost(lang).Disclaimer(asOf, cur)
}

// ---- on-disk shape ----

// pricingFile is pricing.yaml's top-level shape.
type pricingFile struct {
	Currency     string               `yaml:"currency"`
	ExchangeRate []map[string]float64 `yaml:"exchange_rate,omitempty"`
	UpdatedAt    string               `yaml:"updated_at"`
	Rates        []pricingFileRate    `yaml:"rates"`
}

// pricingFileRate is one rates[] entry's on-disk shape.
type pricingFileRate struct {
	Provider        string     `yaml:"provider"`
	Model           string     `yaml:"model"`
	InFreshPer1M    moneyValue `yaml:"in_fresh_per_1m"`
	CacheReadPer1M  moneyValue `yaml:"cache_read_per_1m"`
	CacheWritePer1M moneyValue `yaml:"cache_write_per_1m"`
	OutPer1M        moneyValue `yaml:"out_per_1m"`
	DateRange       []string   `yaml:"date_range,omitempty"`
	HourRange       []string   `yaml:"hour_range,omitempty"`
}

// moneyValue is one price field's on-disk shape: a bare number (the file's
// own currency) or a currency-prefixed string. See UnmarshalYAML.
type moneyValue struct {
	amount   float64
	currency string // "" = the file's own currency
}

// moneyRe splits an optional 2-5 letter currency code from the trailing
// number - "USD1.2" -> ("USD", "1.2"), "jpy 1.2" -> ("jpy", "1.2"),
// "1.2" -> ("", "1.2").
var moneyRe = regexp.MustCompile(`^\s*([A-Za-z]{2,5})?\s*(-?[0-9]+(?:\.[0-9]+)?)\s*$`)

// UnmarshalYAML accepts a bare number or a currency-prefixed string; either
// way the scalar's raw text decodes cleanly into a Go string first (yaml.v3
// decodes any scalar node into a string target using its literal text,
// regardless of the node's resolved tag), then moneyRe splits the optional
// currency prefix from the numeric value.
func (m *moneyValue) UnmarshalYAML(value *yaml.Node) error {
	var raw string
	if err := value.Decode(&raw); err != nil {
		return err
	}
	match := moneyRe.FindStringSubmatch(raw)
	if match == nil {
		return fmt.Errorf("invalid price %q: want a number, optionally prefixed with a currency code (e.g. \"1.2\" or \"USD1.2\")", raw)
	}
	amount, err := strconv.ParseFloat(match[2], 64)
	if err != nil {
		return fmt.Errorf("invalid price %q: %w", raw, err)
	}
	m.amount = amount
	m.currency = strings.ToUpper(match[1])
	return nil
}

// resolveCurrencyFactors builds a "1 unit = factor units of base" table for
// every currency reachable from base through exchange_rate. Each entry
// pairs two or more currencies at an equivalent amount
// ({USD: 1, CNY: 7} means 1 USD = 7 CNY); every pairwise combination inside
// one entry becomes a graph edge, then BFS from base finds the factor for
// every reachable currency - including ones only connected through a chain
// of entries, not a direct pair with base.
func resolveCurrencyFactors(base string, pairs []map[string]float64) map[string]float64 {
	type edge struct {
		to     string
		factor float64 // 1 unit of the source currency = factor units of `to`
	}
	adj := map[string][]edge{}
	for _, pair := range pairs {
		type kv struct {
			cur string
			amt float64
		}
		items := make([]kv, 0, len(pair))
		for k, v := range pair {
			items = append(items, kv{strings.ToUpper(strings.TrimSpace(k)), v})
		}
		for i := range items {
			for j := range items {
				if i == j || items[i].amt == 0 {
					continue
				}
				adj[items[i].cur] = append(adj[items[i].cur], edge{items[j].cur, items[j].amt / items[i].amt})
			}
		}
	}
	factors := map[string]float64{base: 1}
	queue := []string{base}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range adj[cur] {
			if _, ok := factors[e.to]; !ok {
				factors[e.to] = factors[cur] / e.factor
				queue = append(queue, e.to)
			}
		}
	}
	return factors
}

// LoadPricing reads the optional pricing sidecar. Returns (nil, nil) when the
// file does not exist (pricing is opt-in). Returns an error for a file that
// exists but fails to parse, has a malformed date_range/hour_range, or
// prices something in a currency exchange_rate never defines.
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
	currency := strings.ToUpper(strings.TrimSpace(pf.Currency))
	if currency == "" {
		currency = "CNY"
	}
	factors := resolveCurrencyFactors(currency, pf.ExchangeRate)

	convert := func(label string, m moneyValue) (float64, error) {
		if m.currency == "" {
			return m.amount, nil
		}
		f, ok := factors[m.currency]
		if !ok {
			return 0, fmt.Errorf("parse pricing %s: %s: currency %q is not defined in exchange_rate and is not the base currency %q",
				path, label, m.currency, currency)
		}
		return m.amount * f, nil
	}

	p := &Pricing{
		Currency:  currency,
		UpdatedAt: pf.UpdatedAt,
		Raw:       data,
		byKey:     map[string][]PricingRate{},
	}
	for i, r := range pf.Rates {
		label := fmt.Sprintf("rates[%d] (%s/%s)", i, r.Provider, r.Model)
		inFresh, err := convert(label+".in_fresh_per_1m", r.InFreshPer1M)
		if err != nil {
			return nil, err
		}
		cacheRead, err := convert(label+".cache_read_per_1m", r.CacheReadPer1M)
		if err != nil {
			return nil, err
		}
		cacheWrite, err := convert(label+".cache_write_per_1m", r.CacheWritePer1M)
		if err != nil {
			return nil, err
		}
		out, err := convert(label+".out_per_1m", r.OutPer1M)
		if err != nil {
			return nil, err
		}
		rate := PricingRate{
			Provider: r.Provider, Model: r.Model,
			InFreshPer1M: inFresh, CacheReadPer1M: cacheRead, CacheWritePer1M: cacheWrite, OutPer1M: out,
		}
		if len(r.DateRange) > 0 {
			if len(r.DateRange) != 2 {
				return nil, fmt.Errorf("parse pricing %s: %s: date_range needs exactly 2 entries [start, end]", path, label)
			}
			rate.DateFrom, rate.DateTo = strings.TrimSpace(r.DateRange[0]), strings.TrimSpace(r.DateRange[1])
			if rate.DateFrom != "" {
				if _, err := time.Parse("2006-01-02", rate.DateFrom); err != nil {
					return nil, fmt.Errorf("parse pricing %s: %s: invalid date_range start %q (want yyyy-MM-dd): %w", path, label, rate.DateFrom, err)
				}
			}
			if rate.DateTo != "" {
				if _, err := time.Parse("2006-01-02", rate.DateTo); err != nil {
					return nil, fmt.Errorf("parse pricing %s: %s: invalid date_range end %q (want yyyy-MM-dd): %w", path, label, rate.DateTo, err)
				}
			}
		}
		if len(r.HourRange) > 0 {
			if len(r.HourRange) != 2 {
				return nil, fmt.Errorf("parse pricing %s: %s: hour_range needs exactly 2 entries [start, end]", path, label)
			}
			rate.HourFrom, rate.HourTo = strings.TrimSpace(r.HourRange[0]), strings.TrimSpace(r.HourRange[1])
			if rate.HourFrom != "" {
				if _, err := time.Parse("15:04", rate.HourFrom); err != nil {
					return nil, fmt.Errorf("parse pricing %s: %s: invalid hour_range start %q (want HH:MM): %w", path, label, rate.HourFrom, err)
				}
			}
			if rate.HourTo != "" {
				if _, err := time.Parse("15:04", rate.HourTo); err != nil {
					return nil, fmt.Errorf("parse pricing %s: %s: invalid hour_range end %q (want HH:MM): %w", path, label, rate.HourTo, err)
				}
			}
		}
		p.Rates = append(p.Rates, rate)
		key := rateKey(r.Provider, r.Model)
		p.byKey[key] = append(p.byKey[key], rate)
	}
	return p, nil
}
