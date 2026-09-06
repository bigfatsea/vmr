// Ver 2026-08-07, by Opus 5

// Package pricing is Quota-Aware Routing's per-1M-token pricing resolution
// engine (see docs/VirtualModelRouter_Design_v4_Quota.md's pricing sections
// for the two-layer design: providers[].pricing (account-local contract) over
// the embedded standard table (official baseline)). A leaf package: only
// depends on core + stdlib + gopkg.in/yaml.v3, same layer as internal/quota (see that package's own
// doc comment for the precedent this follows).
//
// Two consumers share this package's resolution logic: internal/config,
// which validates every provider's pricing: block at config-validate time
// (structural checks only — no runtime charging depends on this anymore,
// see core.PricingSpec's doc comment); and cmd/vmr/cmd_report.go, which
// resolves the same tables for vmr report's offline $ estimates. Neither
// internal/report nor internal/router imports this package directly for
// report's case — cmd is the composition root that reads config.yaml,
// resolves pricing, and hands report a plain value (see
// internal/report/pricing.go's own doc comment for why that boundary
// exists).
package pricing

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rate is a per-1,000,000-token four-component USD price snapshot — every
// Rate this package produces is USD-denominated (see Table's doc comment).
// A nil field means "unknown" (absent from the source data), NOT "free" —
// an explicit *float64 pointing at 0.0 is how "free" is spelled. This
// distinction matters for Complete/MissingComponents, which vmr report's
// incomplete-rate labeling (internal/report/cost.go) uses to avoid silently
// treating an unknown component as free.
type Rate struct {
	InFresh    *float64
	CacheRead  *float64
	CacheWrite *float64
	Out        *float64
}

// Complete reports whether every one of Rate's four components is set
// (explicitly, even if to 0.0) and is a finite non-negative number —
// internal/report/cost.go uses this to label a $ estimate as incomplete
// rather than silently under-price a nil component as 0.
func (r Rate) Complete() bool {
	if r.InFresh == nil || r.CacheRead == nil || r.CacheWrite == nil || r.Out == nil {
		return false
	}
	for _, v := range []*float64{r.InFresh, r.CacheRead, r.CacheWrite, r.Out} {
		if math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 {
			return false
		}
	}
	return true
}

// IsEmpty reports whether all four components are nil — "no pricing at
// all", the complement of Complete (all four set and finite); anything
// between is partial pricing. The gate Resolver.RateFor applies so an
// all-nil rate can't flow downstream as a priced $0.00.
func (r Rate) IsEmpty() bool {
	return r.InFresh == nil && r.CacheRead == nil && r.CacheWrite == nil && r.Out == nil
}

// MissingComponents names r's unset fields, in a fixed order, for error
// messages.
func (r Rate) MissingComponents() []string {
	var missing []string
	for _, f := range []struct {
		name string
		val  *float64
	}{{"in_fresh", r.InFresh}, {"cache_read", r.CacheRead}, {"cache_write", r.CacheWrite}, {"out", r.Out}} {
		if f.val == nil || math.IsNaN(*f.val) || math.IsInf(*f.val, 0) || *f.val < 0 {
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
// sums them — a thin delegate to core.Rate.Cost, where the base(cost)
// formula from docs/VirtualModelRouter_Design_v4_Quota.md's §3 actually
// lives (one formula, both halves — see that method's doc comment for the
// drift and parity reasoning). Kept as a method here so the analytics
// half's call sites read naturally and no caller has to spell the
// conversion out by hand.
func (r Rate) Cost(fresh, cacheRead, cacheWrite, out int64) float64 {
	c := r.toCore()
	return c.Cost(fresh, cacheRead, cacheWrite, out)
}

// entry is one canonical-model-id row inside a Table.
type entry struct {
	key  string // lowercased canonical key, e.g. "anthropic/claude-3-5-sonnet-20241022"
	rate Rate
}

// Table is a canonical-key -> Rate index — the embedded standard/curated
// tables' in-memory shape (see embed.go's LoadStandard). Always USD: every
// row is normalized to USD at parse time (see parseTable), so no downstream
// consumer (Merge, Resolve, the discount-chain recursion in resolve.go)
// ever has to think about currency. Keys are matched case-insensitively —
// canonical ids are conventionally lowercase, but a hand-written curated row
// shouldn't have to get case exactly right.
type Table struct {
	GeneratedAt string
	entries     map[string]entry // lowercased key -> entry
	order       []string         // insertion order, for Step ④'s deterministic "first ambiguous match wins... no, doesn't win" scan
	// aliases maps a bare (vendor-prefix-free) model name to the canonical
	// key that names its price — a REFERENCE, never a copied price, so a
	// regenerated standard table moves every alias with it. Two jobs it
	// alone can do (see resolveCanonicalKey's step ③): naming which
	// vendor's row a locally-used bare model name means when several
	// vendors carry it and vendor precedence can't decide (a first-party
	// vendor reselling another first party, e.g. dashscope/deepseek-v4-flash
	// vs deepseek/deepseek-v4-flash), and pointing a proxy's own invented
	// model name at the first-party model it actually serves
	// (gemini-3.7-flash-high -> gemini/gemini-3.7-flash). One hop only,
	// by construction — an alias whose target is itself an alias key is
	// rejected by ValidateAliases, so there is no chain to cycle.
	aliases map[string]string // lowercased alias -> lowercased canonical key
}

// NewTable creates an empty Table — used by tests and as Merge's base case.
func NewTable() *Table {
	return &Table{entries: map[string]entry{}, aliases: map[string]string{}}
}

// putAlias inserts or overwrites one bare-name -> canonical-key alias.
func (t *Table) putAlias(from, to string) {
	if t.aliases == nil {
		t.aliases = map[string]string{}
	}
	t.aliases[strings.ToLower(strings.TrimSpace(from))] = strings.ToLower(strings.TrimSpace(to))
}

// LookupAlias resolves one alias hop: the canonical key name names, if any.
// Never chains — see Table.aliases' doc comment.
func (t *Table) LookupAlias(name string) (string, bool) {
	if t == nil {
		return "", false
	}
	ck, ok := t.aliases[strings.ToLower(strings.TrimSpace(name))]
	return ck, ok
}

// Aliases returns a copy of t's alias map (lowercased both sides) — for
// `vmr check`'s pricing line and tests; the live map stays unexported so
// nothing can mutate a loaded table.
func (t *Table) Aliases() map[string]string {
	if t == nil || len(t.aliases) == 0 {
		return nil
	}
	out := make(map[string]string, len(t.aliases))
	for k, v := range t.aliases {
		out[k] = v
	}
	return out
}

// ValidateAliases reports the first alias that can never resolve: one whose
// target has no priced row, or whose target is itself an alias key (a chain
// — banned outright rather than followed, since a chain is the only way to
// build a cycle and a one-hop rule has no case it can't express). Called at
// load time (embed.go's LoadStandard) so a typo is a startup error, not a
// rate that silently falls through to the suffix scan and lands on some
// other vendor's number.
func (t *Table) ValidateAliases() error {
	if t == nil {
		return nil
	}
	for _, from := range sortedAliasKeys(t.aliases) {
		to := t.aliases[from]
		if _, isAlias := t.aliases[to]; isAlias {
			return fmt.Errorf("pricing alias %q -> %q: the target is itself an alias — aliases resolve in exactly one hop, point this one straight at the priced canonical key", from, to)
		}
		if _, ok := t.entries[to]; !ok {
			return fmt.Errorf("pricing alias %q -> %q: no such key in the merged price table — fix the canonical key, or add the row it names", from, to)
		}
	}
	return nil
}

func sortedAliasKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// put inserts or overwrites key's Rate — internal, used by parseTable/Merge.
// Key is TrimSpace'd before storage so it matches Lookup's contract: a
// hand-written curated row "  gpt-4o  " and a hand-written lookup "gpt-4o"
// both reach the same entry rather than silently falling through to the
// suffix scan with a key the scan can't disambiguate.
func (t *Table) put(key string, r Rate) {
	lk := strings.ToLower(strings.TrimSpace(key))
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
	e, ok := t.entries[strings.ToLower(strings.TrimSpace(key))]
	return e.rate, ok
}

// LookupRateOrAlias resolves key's Rate directly, or via t's aliases if key is
// a bare alias name. Case-insensitive. Used by providers[].pricing.aliases
// resolution and config validation so a mapping target can be either an
// exact canonical key ("anthropic/claude-3-5-sonnet") or a standard bare
// alias ("claude-3-5-sonnet").
func (t *Table) LookupRateOrAlias(key string) (Rate, bool) {
	if t == nil {
		return Rate{}, false
	}
	if r, ok := t.Lookup(key); ok {
		return r, true
	}
	if target, ok := t.LookupAlias(key); ok {
		return t.Lookup(target)
	}
	return Rate{}, false
}

// aggregatorVendors are canonical-key vendor prefixes that RESELL other
// vendors' models rather than originate them. Measured, not assumed: every
// bare model name carried by more than one vendor in the standard table is
// a first-party-vs-reseller collision, never two first parties disagreeing
// about their own model — so "the first-party row is the list price, a
// reseller's is that reseller's markup" resolves the overwhelming majority
// of them with no per-model configuration at all.
//
// Deliberately the SHORT list (resellers), not the long one (first
// parties): a vendor this package has never heard of is far more likely to
// be a new first party than a new aggregator, and ranking it first-party
// costs nothing unless it collides. A platform that is first-party for its
// own line but a reseller for others (dashscope for Qwen vs DeepSeek,
// volcengine for Doubao vs DeepSeek) can't be captured by a per-VENDOR
// rank at all — that split is per (vendor, model), which is exactly what
// the curated alias table exists to express (see Table.aliases).
var aggregatorVendors = map[string]bool{
	"openrouter": true, "fireworks_ai": true, "together_ai": true,
	"groq": true, "perplexity": true,
}

// vendorOf returns the canonical key's vendor prefix ("gemini" for
// "gemini/gemini-3.7-flash"), or "" for a bare key with no prefix.
func vendorOf(key string) string {
	if i := strings.Index(key, "/"); i >= 0 {
		return key[:i]
	}
	return ""
}

// LookupPreferredSuffix implements the design doc's step ④: scan every row
// for one whose canonical key ends in "/"+model (case-insensitive) — e.g. a
// bare upstream model name "claude-3-5-sonnet-20241022" matching the
// standard table's "anthropic/claude-3-5-sonnet-20241022" row.
//
// Several rows matching is the common case (a model sold by its maker and
// resold by three aggregators), and it used to mean "no rate at all". The
// tie is broken by vendor precedence, never by an arbitrary pick: a single
// non-aggregator (first-party) match wins outright — its price IS the model's
// list price, which is what an offline $ estimate means. ok=false when the
// highest occupied rank still holds more than one candidate: two
// aggregators disagreeing about someone else's model have no canonical
// answer between them, and an ambiguous match must never be guessed at.
func (t *Table) LookupPreferredSuffix(model string) (Rate, bool) {
	if t == nil {
		return Rate{}, false
	}
	suffix := "/" + strings.ToLower(strings.TrimSpace(model))
	var all, firstParty []string
	for _, k := range t.order {
		if strings.HasSuffix(k, suffix) {
			all = append(all, k)
			if !aggregatorVendors[vendorOf(k)] {
				firstParty = append(firstParty, k)
			}
		}
	}
	switch {
	case len(firstParty) == 1:
		return t.entries[firstParty[0]].rate, true
	case len(firstParty) == 0 && len(all) == 1:
		return t.entries[all[0]].rate, true
	}
	return Rate{}, false
}

// Merge returns a new Table containing every row of base, overlaid by every
// row of overlay — a whole-row replacement per canonical key (curated wins
// over generated on a conflict), not a per-component merge: an overlay row
// that only sets in_fresh does NOT inherit base's cache_read, it simply
// replaces the whole row (the same "explicit beats partial" reasoning as
// everywhere else in this package).
func Merge(base, overlay *Table) *Table {
	out := NewTable()
	if base != nil {
		// GeneratedAt travels with base (the generated table, whose
		// freshness is the signal callers like vmr report's §2 appendix
		// render — see internal/report/pricing.go's Pricing.Disclaimer);
		// overlay is the hand-maintained curated table, with no meaningful
		// generation date of its own.
		out.GeneratedAt = base.GeneratedAt
		for _, k := range base.order {
			out.put(k, base.entries[k].rate)
		}
		for _, k := range sortedAliasKeys(base.aliases) {
			out.putAlias(k, base.aliases[k])
		}
	}
	if overlay != nil {
		for _, k := range overlay.order {
			out.put(k, overlay.entries[k].rate)
		}
		// Aliases overlay per-name the same way rates overlay per-key: the
		// curated table can retarget (or, by pointing it at its own row,
		// effectively replace) an alias the generated table shipped.
		for _, k := range sortedAliasKeys(overlay.aliases) {
			out.putAlias(k, overlay.aliases[k])
		}
	}
	return out
}

// fileTable is the embedded standard/curated pricing table's on-disk YAML
// shape (standard_price_generated.yaml, standard_price_curated.yaml — see
// embed.go). Every in-memory Table this package produces is USD (LiteLLM's
// native currency, and the canonical-key space this package interoperates
// with is LiteLLM's).
//
// ExchangeRate lets a hand-maintained curated row be entered straight from
// a vendor's native-currency official price list (via that row's own
// RateRow.Currency), self-contained within this file — no external
// exchange-rate source exists anymore (no external supplement/standard file,
// no per-deployment fallback rates). A currency a row names without a matching
// entry here is a load-time error, not a silent skip.
type fileTable struct {
	Currency     string             `yaml:"currency"`
	GeneratedAt  string             `yaml:"generated_at"`
	Rates        []RateRow          `yaml:"rates"`
	Aliases      map[string]string  `yaml:"aliases"`
	ExchangeRate map[string]float64 `yaml:"exchange_rate"`
}

// RateRow is one row of the embedded standard/curated pricing table.
// Pointer fields: an absent YAML key decodes to nil (unknown),
// present-with-0.0 decodes to a non-nil pointer at 0.0 (explicitly free) —
// this is the exact mechanism Rate's "missing vs zero" distinction is built
// on.
//
// Currency optionally overrides the file's own default currency for this
// one row — e.g. a domestic vendor's row entered straight from its official
// CNY price list inside an otherwise-USD table. Empty means "inherit the
// table's currency:" (itself USD if that's also empty), converted via the
// file's own exchange_rate: block (fileTable.ExchangeRate).
type RateRow struct {
	Key        string   `yaml:"key"`
	Currency   string   `yaml:"currency"`
	InFresh    *float64 `yaml:"in_fresh"`
	CacheRead  *float64 `yaml:"cache_read"`
	CacheWrite *float64 `yaml:"cache_write"`
	Out        *float64 `yaml:"out"`
}

// FactorBetween returns the multiplier that converts an amount denominated
// in fromCCY into toCCY, via a USD pivot: rates maps a currency code to "1
// USD = X <that code>" (USD itself is always implicit 1.0 and never needs
// an entry). ok=false when a needed non-USD currency has no entry, or its
// entry isn't a finite positive number — deliberately no indirect
// CCY->CCY chaining beyond the one USD hop: every currency this package
// deals with (a curated row's native currency, a provider's
// pricing.currency, vmr report's display currency) has a well-known USD
// cross-rate, so a general multi-hop graph would be unneeded complexity.
// Exported: internal/config uses this directly to convert a provider's
// pricing.rates components into USD at validate time.
func FactorBetween(fromCCY, toCCY string, rates map[string]float64) (float64, bool) {
	from, ok := rateVsUSD(fromCCY, rates)
	if !ok {
		return 0, false
	}
	to, ok := rateVsUSD(toCCY, rates)
	if !ok {
		return 0, false
	}
	return to / from, true
}

// rateVsUSD looks up ccy's "1 USD = X ccy" rate — 1.0 for USD (or empty)
// without needing a map entry, otherwise rates[ccy] if present and a finite
// positive number.
func rateVsUSD(ccy string, rates map[string]float64) (float64, bool) {
	ccy = strings.ToUpper(strings.TrimSpace(ccy))
	if ccy == "" || ccy == "USD" {
		return 1, true
	}
	v, ok := rates[ccy]
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0, false
	}
	return v, true
}

// ParseTable parses one standard/curated-shaped YAML document (the embedded
// tables' own shape — see fileTable's doc comment). The sole entry point
// into this parser: no external supplement/standard file exists anymore
// (see fileTable's doc comment), so there is no second variant that accepts
// a caller-supplied fallback exchange-rate map — a row's own currency:
// converts only through this same file's own exchange_rate: block.
func ParseTable(data []byte) (*Table, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		// An empty file (standard_price_curated.yaml starts this way — see
		// embed.go's doc comment) is a valid, empty table, not an error.
		return NewTable(), nil
	}
	var ft fileTable
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // strict, same fail-fast contract as config.yaml itself (see CLAUDE.md)
	if err := dec.Decode(&ft); err != nil {
		if errors.Is(err, io.EOF) {
			// A document that's entirely comments (standard_price_curated.yaml
			// starts this way — see embed.go) decodes to no document at
			// all, not a zero-value one; that's still a valid empty table.
			return NewTable(), nil
		}
		return nil, fmt.Errorf("parse pricing table: %w", err)
	}
	defaultCCY := strings.ToUpper(strings.TrimSpace(ft.Currency))
	if defaultCCY == "" {
		defaultCCY = "USD"
	}
	t, err := newTableFromRows(ft.Rates, ft.Aliases, defaultCCY, ft.ExchangeRate)
	if err != nil {
		return nil, fmt.Errorf("parse pricing table: %w", err)
	}
	t.GeneratedAt = ft.GeneratedAt
	return t, nil
}

// newTableFromRows constructs a Table from in-memory RateRow entries and
// optional aliases, normalized to USD via rates (typically the file's own
// exchange_rate: block — see fileTable.ExchangeRate). defaultCCY sets the
// fallback currency for rows that omit Currency ("" or "USD" means USD).
func newTableFromRows(rows []RateRow, aliases map[string]string, defaultCCY string, rates map[string]float64) (*Table, error) {
	defaultCCY = strings.ToUpper(strings.TrimSpace(defaultCCY))
	if defaultCCY == "" {
		defaultCCY = "USD"
	}
	if defaultCCY != "USD" {
		if _, ok := FactorBetween(defaultCCY, "USD", rates); !ok {
			return nil, fmt.Errorf("currency %q has no matching exchange_rate entry to convert into USD (write exchange_rate: {%s: <rate>}, \"1 USD = <rate> %s\")", defaultCCY, defaultCCY, defaultCCY)
		}
	}
	t := NewTable()
	for from, to := range aliases {
		if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
			return nil, fmt.Errorf("aliases: both sides must be non-empty (got %q -> %q)", from, to)
		}
		t.putAlias(from, to)
	}
	seen := map[string]bool{}
	for i, r := range rows {
		lk, rate, err := parseRateRow(r, i, defaultCCY, rates)
		if err != nil {
			return nil, err
		}
		if seen[lk] {
			return nil, fmt.Errorf("rates[%d]: duplicate key %q", i, r.Key)
		}
		seen[lk] = true
		t.put(lk, rate)
	}
	return t, nil
}

// parseRateRow validates one rates[] row's key and converts/validates its
// Rate against the table's default currency and the effective exchange-rate
// map, returning the normalized (lowercased, whitespace-trimmed) key and the
// USD-denominated rate. TrimSpace on the key so "x" and " x" are the same key
// (both name one model, see put's doc comment) rather than two rows only one
// lookup form can ever reach; the duplicate check against the already-seen
// set stays with the caller, which owns that set.
func parseRateRow(r RateRow, i int, defaultCCY string, rates map[string]float64) (string, Rate, error) {
	if strings.TrimSpace(r.Key) == "" {
		return "", Rate{}, fmt.Errorf("rates[%d]: key is required", i)
	}
	// Canonical keys are exactly "vendor/basename" (or a bare name). A
	// deeper key — openrouter's forced "meta-llama/llama-3.3-70b-instruct",
	// fireworks' "accounts/fireworks/models/..." — looks self-consistent
	// but is invisible to bare-name and suffix resolution, which strip
	// org prefixes via ModelBasename (see resolveCanonicalKey's fallback):
	// it would silently split one physical model into two namespaces that
	// can never see each other. Reject at load time so a hand-written row
	// names the two-segment key instead.
	if strings.Count(r.Key, "/") > 1 {
		return "", Rate{}, fmt.Errorf("rates[%d]: key %q must be \"vendor/basename\" or a bare name (at most one \"/\") — org/path prefixes are not model identity and are stripped from every table key (see pricing.ModelBasename)", i, r.Key)
	}
	lk := strings.ToLower(strings.TrimSpace(r.Key))
	rowCCY := strings.ToUpper(strings.TrimSpace(r.Currency))
	if rowCCY == "" {
		rowCCY = defaultCCY
	}
	rate := Rate{InFresh: r.InFresh, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Out: r.Out}
	if rowCCY != "USD" {
		factor, ok := FactorBetween(rowCCY, "USD", rates)
		if !ok {
			return "", Rate{}, fmt.Errorf("rates[%d]: currency %q has no matching exchange_rate entry to convert into USD (write exchange_rate: {%s: <rate>}, \"1 USD = <rate> %s\" in this file's own exchange_rate: block)", i, rowCCY, rowCCY, rowCCY)
		}
		rate = rate.Scale(factor)
	}
	// A row naming a key but carrying no rate component would make every
	// lookup of that key a tableHit with an all-nil (unpriced) Rate —
	// bypassing the "no rate at all" contract upstream callers rely on.
	if rate.IsEmpty() {
		return "", Rate{}, fmt.Errorf("rates[%d]: key %q: at least one of in_fresh/cache_read/cache_write/out must be set", i, r.Key)
	}
	// Reject NaN, Inf, and negative rates — a hand-written row can have a
	// typo (e.g. "-5.0" or ".nan") that silently poisons every downstream
	// consumer.
	for _, comp := range []struct {
		name string
		val  *float64
	}{{"in_fresh", rate.InFresh}, {"cache_read", rate.CacheRead}, {"cache_write", rate.CacheWrite}, {"out", rate.Out}} {
		if comp.val == nil {
			continue
		}
		if math.IsNaN(*comp.val) {
			return "", Rate{}, fmt.Errorf("rates[%d]: key %q: %s is NaN", i, r.Key, comp.name)
		}
		if math.IsInf(*comp.val, 0) {
			return "", Rate{}, fmt.Errorf("rates[%d]: key %q: %s is Inf", i, r.Key, comp.name)
		}
		if *comp.val < 0 {
			return "", Rate{}, fmt.Errorf("rates[%d]: key %q: %s is negative (%v)", i, r.Key, comp.name, *comp.val)
		}
	}
	return lk, rate, nil
}
