// Ver 2026-08-07, by Opus 5

// Package pricing is Quota-Aware Routing's per-1M-token pricing resolution
// engine (P2.2 — see docs/TokenPlan_Quota_Routing_Design_opus-5.md's §4.2①
// for the three-layer design and docs/TokenPlan_Quota_P2_DevPlan_opus-5.md's
// §3.3/§6 for what this batch actually delivers). A leaf package: only
// depends on core + stdlib + gopkg.in/yaml.v3, same layer as internal/quota
// (see that package's own doc comment for the precedent this follows).
//
// Two consumers share this package's resolution logic (see the design doc's
// §4.2⑤): internal/config, which resolves pricing into core.PricingSpec at
// config-validate time so it can ride along in router.Snapshot for the
// metric: cost charging path (internal/router/quota.go); and
// cmd/vmr/cmd_report.go, which resolves the same tables for vmr report's
// offline $ estimates. Neither internal/report nor internal/router imports
// this package directly for report's case — cmd is the composition root
// that reads config.yaml, resolves pricing, and hands report a plain value
// (see internal/report/pricing.go's own doc comment for why that boundary
// exists).
package pricing

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rate is a per-1,000,000-token four-component price snapshot, in whatever
// currency its Table (or the resolving account's pricing.currency) uses. A
// nil field means "unknown" (absent from the source data), NOT "free" — an
// explicit *float64 pointing at 0.0 is how "free" is spelled. This
// distinction is the whole reason the design doc's §4.2① calls out
// "missing is more dangerous than 0": a metric: cost account whose
// cache_read price is silently treated as 0 looks cheaper than it really
// is, gets more traffic, and overspends — see Complete/MissingComponents,
// which config.validate() uses to fail loudly instead.
type Rate struct {
	InFresh    *float64
	CacheRead  *float64
	CacheWrite *float64
	Out        *float64
}

// Complete reports whether every one of Rate's four components is set
// (explicitly, even if to 0.0) — the gate config.validate() applies to any
// provider+model a metric: cost Limit will actually charge.
func (r Rate) Complete() bool {
	return r.InFresh != nil && r.CacheRead != nil && r.CacheWrite != nil && r.Out != nil
}

// MissingComponents names r's unset fields, in a fixed order, for error
// messages — config.validate() uses this to say exactly which component is
// missing rather than just "incomplete".
func (r Rate) MissingComponents() []string {
	var missing []string
	for _, f := range []struct {
		name string
		val  *float64
	}{{"in_fresh", r.InFresh}, {"cache_read", r.CacheRead}, {"cache_write", r.CacheWrite}, {"out", r.Out}} {
		if f.val == nil {
			missing = append(missing, f.name)
		}
	}
	return missing
}

// Scale multiplies every SET component by f, leaving unset (nil) components
// unset — the discount-form override's operation (design doc §4.2④): "the
// discount multiplies the rate the lower layer resolved", and a lower layer
// that never had a cache_write price to begin with doesn't gain one just
// because a discount rule applied.
func (r Rate) Scale(f float64) Rate {
	scale := func(v *float64) *float64 {
		if v == nil {
			return nil
		}
		s := *v * f
		return &s
	}
	return Rate{InFresh: scale(r.InFresh), CacheRead: scale(r.CacheRead), CacheWrite: scale(r.CacheWrite), Out: scale(r.Out)}
}

// Cost prices fresh/cacheRead/cacheWrite/out (raw token counts) through r and
// sums them — the base(cost) formula from
// docs/TokenPlan_Quota_Routing_Design_opus-5.md's §3, shared by
// internal/router/quota.go's charge-time computation and
// internal/report/cost.go's per-record estimate so the two never drift
// (they were independently hand-written to the same formula until P2.2's
// post-delivery review consolidated them here). A nil component (see this
// type's own doc comment: unknown, not free) contributes 0 rather than
// panicking — a defensive floor, not a documented degrade path.
func (r Rate) Cost(fresh, cacheRead, cacheWrite, out int64) float64 {
	priced := func(tokens int64, perMillion *float64) float64 {
		if perMillion == nil {
			return 0
		}
		return float64(tokens) / 1_000_000 * *perMillion
	}
	return priced(fresh, r.InFresh) + priced(cacheRead, r.CacheRead) +
		priced(cacheWrite, r.CacheWrite) + priced(out, r.Out)
}

// entry is one canonical-model-id row inside a Table.
type entry struct {
	key  string // lowercased canonical key, e.g. "anthropic/claude-3-5-sonnet-20241022"
	rate Rate
}

// Table is a canonical-key -> Rate index: the shape both the embedded
// standard table and a user's supplement file share (see embed.go's
// LoadStandard and config.PricingConfig.Supplement). Keys are matched
// case-insensitively — canonical ids are conventionally lowercase, but a
// hand-written supplement file shouldn't have to get case exactly right.
type Table struct {
	Currency    string // always "USD" for the embedded standard/supplement tables — see fileTable's doc comment for why this package doesn't do general multi-currency tables the way the old report.Pricing sidecar did
	GeneratedAt string
	entries     map[string]entry // lowercased key -> entry
	order       []string         // insertion order, for Step ④'s deterministic "first ambiguous match wins... no, doesn't win" scan
}

// NewTable creates an empty Table — used by tests and as Merge's base case.
func NewTable(currency string) *Table {
	return &Table{Currency: currency, entries: map[string]entry{}}
}

// put inserts or overwrites key's Rate — internal, used by ParseTable/Merge.
func (t *Table) put(key string, r Rate) {
	lk := strings.ToLower(key)
	if _, exists := t.entries[lk]; !exists {
		t.order = append(t.order, lk)
	}
	t.entries[lk] = entry{key: lk, rate: r}
}

// Lookup returns key's Rate, case-insensitively. ok=false means this exact
// canonical key has no row — Resolve (resolve.go) is what tries the
// multi-step fallback resolution on top of this.
func (t *Table) Lookup(key string) (Rate, bool) {
	if t == nil {
		return Rate{}, false
	}
	e, ok := t.entries[strings.ToLower(key)]
	return e.rate, ok
}

// LookupUniqueSuffix implements the design doc's §9.1 step ④: scan every
// row for one whose canonical key ends in "/"+model (case-insensitive) —
// e.g. a bare upstream model name "claude-3-5-sonnet-20241022" matching the
// standard table's "anthropic/claude-3-5-sonnet-20241022" row. ok=false
// covers both "no row ends that way" and "more than one does" — the design
// doc is explicit that an ambiguous match must never be guessed at
// ("有歧义不猜——猜错一个费率比没有费率危险得多").
func (t *Table) LookupUniqueSuffix(model string) (Rate, bool) {
	if t == nil {
		return Rate{}, false
	}
	suffix := "/" + strings.ToLower(model)
	var found Rate
	matches := 0
	for _, k := range t.order {
		if strings.HasSuffix(k, suffix) {
			matches++
			if matches > 1 {
				return Rate{}, false
			}
			found = t.entries[k].rate
		}
	}
	if matches != 1 {
		return Rate{}, false
	}
	return found, true
}

// Merge returns a new Table containing every row of base, overlaid by every
// row of overlay — a whole-row replacement per canonical key ("按 key 合并，
// 补充表在冲突时胜出", design doc §4.2①), not a per-component merge: a
// supplement row that only sets in_fresh does NOT inherit base's
// cache_read, it simply replaces the whole row (the same "explicit beats
// partial" reasoning as everywhere else in this package — a supplement
// author who wanted to keep base's other three components would copy them
// forward explicitly, same as any override).
func Merge(base, overlay *Table) *Table {
	currency := "USD"
	if base != nil {
		currency = base.Currency
	}
	out := NewTable(currency)
	if base != nil {
		// GeneratedAt travels with base, not overlay: base is the
		// standard/refreshed table (the "is this stale" signal callers like
		// vmr report's §2 appendix render — see internal/report/pricing.go's
		// Pricing.Disclaimer); overlay is typically a hand-maintained
		// supplement/curated table with no meaningful generation date of
		// its own.
		out.GeneratedAt = base.GeneratedAt
		for _, k := range base.order {
			out.put(k, base.entries[k].rate)
		}
	}
	if overlay != nil {
		for _, k := range overlay.order {
			out.put(k, overlay.entries[k].rate)
		}
	}
	return out
}

// fileTable is a standard/curated/supplement pricing table's on-disk YAML
// shape. Deliberately simpler than the old report.Pricing sidecar's
// moneyValue (a bare number or a currency-prefixed string per field): every
// table this package loads is USD (LiteLLM's native currency, and the
// canonical-key space this package interoperates with is LiteLLM's), and
// the ONE currency conversion vmr needs — USD standard-table prices into
// whatever currency an account's metric: cost Limit is denominated in — is
// a single global factor (config.PricingConfig.ExchangeRate), applied once
// at resolve time, not per price field. See
// docs/TokenPlan_Quota_P2_DevPlan_opus-5.md §6 for why this is a deliberate
// simplification versus the old sidecar's more general (and, since P2.2
// retires that sidecar, no longer used) per-field multi-currency graph.
type fileTable struct {
	Currency    string     `yaml:"currency"`
	GeneratedAt string     `yaml:"generated_at"`
	Rates       []fileRate `yaml:"rates"`
}

// fileRate is one fileTable row. Pointer fields: an absent YAML key decodes
// to nil (unknown), present-with-0.0 decodes to a non-nil pointer at 0.0
// (explicitly free) — this is the exact mechanism Rate's "missing vs zero"
// distinction is built on; no custom UnmarshalYAML is needed to get it
// (see config.LimitConfig's own doc comment for the same "plain fields
// beat a custom decoder" precedent this package follows).
type fileRate struct {
	Key        string   `yaml:"key"`
	InFresh    *float64 `yaml:"in_fresh"`
	CacheRead  *float64 `yaml:"cache_read"`
	CacheWrite *float64 `yaml:"cache_write"`
	Out        *float64 `yaml:"out"`
}

// ParseTable parses one standard/curated/supplement-shaped YAML document.
// Every table this package reads is asserted (not merely assumed) to be
// USD — see fileTable's doc comment; a table declaring anything else is a
// load-time error rather than a silent misinterpretation.
func ParseTable(data []byte) (*Table, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		// An empty file (standard_price_curated.yaml starts this way — see
		// embed.go's doc comment) is a valid, empty table, not an error.
		return NewTable("USD"), nil
	}
	var ft fileTable
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // strict, same fail-fast contract as config.yaml itself (see CLAUDE.md)
	if err := dec.Decode(&ft); err != nil {
		if errors.Is(err, io.EOF) {
			// A document that's entirely comments (standard_price_curated.yaml
			// starts this way — see embed.go) decodes to no document at
			// all, not a zero-value one; that's still a valid empty table.
			return NewTable("USD"), nil
		}
		return nil, fmt.Errorf("parse pricing table: %w", err)
	}
	cur := strings.ToUpper(strings.TrimSpace(ft.Currency))
	if cur == "" {
		cur = "USD"
	}
	if cur != "USD" {
		return nil, fmt.Errorf("parse pricing table: currency %q not supported (standard/curated/supplement tables must be USD — see pricing.fileTable's doc comment)", cur)
	}
	t := NewTable(cur)
	t.GeneratedAt = ft.GeneratedAt
	seen := map[string]bool{}
	for i, r := range ft.Rates {
		if r.Key == "" {
			return nil, fmt.Errorf("parse pricing table: rates[%d]: key is required", i)
		}
		lk := strings.ToLower(r.Key)
		if seen[lk] {
			return nil, fmt.Errorf("parse pricing table: rates[%d]: duplicate key %q", i, r.Key)
		}
		seen[lk] = true
		t.put(r.Key, Rate{InFresh: r.InFresh, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Out: r.Out})
	}
	return t, nil
}
