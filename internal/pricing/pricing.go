// Ver 2026-08-07, by Opus 5

// Package pricing is Quota-Aware Routing's per-1M-token pricing resolution
// engine (see docs/VirtualModelRouter_Design_v4_Quota.md's
// "定价分三层" section for the three-layer design and its "现状与后续计划"
// section for what's actually shipped). A leaf package: only
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
	"math"
	"sort"
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
// (explicitly, even if to 0.0) and is a finite non-negative number — the
// gate config.validate() applies to any provider+model a metric: cost Limit
// will actually charge. A nil, NaN, Inf, or negative component makes a rate
// unusable, matching the parse-time numeric gate in parseTable (R42).
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

// MissingComponents names r's unset fields, in a fixed order, for error
// messages — config.validate() uses this to say exactly which component is
// missing rather than just "incomplete".
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
// sums them — the base(cost) formula from
// docs/VirtualModelRouter_Design_v4_Quota.md's §3, shared by
// internal/router/quota.go's charge-time computation and
// internal/report/cost.go's per-record estimate so the two never drift
// (they were independently hand-written to the same formula until
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
func NewTable(currency string) *Table {
	return &Table{Currency: currency, entries: map[string]entry{}, aliases: map[string]string{}}
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
// load time (embed.go's LoadStandard, config's buildPricingContext) so a
// typo is a startup error, not a rate that silently falls through to the
// suffix scan and lands on some other vendor's number.
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
	e, ok := t.entries[strings.ToLower(strings.TrimSpace(key))]
	return e.rate, ok
}

// LookupRateOrAlias resolves key's Rate directly, or via t's aliases if key is
// a bare alias name. Case-insensitive. Used by pricing.map resolution and
// config validation so a mapping target can be either an exact canonical key
// ("anthropic/claude-3-5-sonnet") or a standard bare alias ("claude-3-5-sonnet").
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
// of them with no per-model configuration at all (51 of 78 collisions in
// the 2026-08-31 snapshot, plus 6 more pinned outright by curated aliases;
// the remaining 21 are reseller-only models, which genuinely have no
// canonical list price and stay unresolved).
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

// IsAggregatorVendor reports whether vendor (a canonical-key prefix) resells
// other vendors' models rather than originating its own — see
// aggregatorVendors' own doc comment for the reasoning and the measured
// basis. No production caller today: the shared criterion is
// Table.Ambiguities, which tools/gen_standard_pricing consumes and whose
// implementation consults aggregatorVendors directly — this func only
// re-exposes that set. Deleting it also requires retiring its citation in
// docs/VirtualModelRouter_Design_v4_Quota.md, so it stays until that edit
// and this one can land together.
func IsAggregatorVendor(vendor string) bool { return aggregatorVendors[vendor] }

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
// list price, which is what an offline $ estimate means (see the design
// doc's "套餐账号的 $ 含义"). ok=false when the highest occupied rank still
// holds more than one candidate: two aggregators disagreeing about someone
// else's model have no canonical answer between them, and the design doc is
// explicit that an ambiguous match must never be guessed at ("有歧义不猜——
// 猜错一个费率比没有费率危险得多"). A single match of any rank still wins,
// exactly as before — this only ever widens what resolves, never changes
// what a previously-unambiguous name resolved to.
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
		for _, k := range sortedAliasKeys(base.aliases) {
			out.putAlias(k, base.aliases[k])
		}
	}
	if overlay != nil {
		for _, k := range overlay.order {
			out.put(k, overlay.entries[k].rate)
		}
		// Aliases overlay per-name the same way rates overlay per-key: a
		// user supplement can retarget (or, by pointing it at its own row,
		// effectively replace) an alias the curated table shipped.
		for _, k := range sortedAliasKeys(overlay.aliases) {
			out.putAlias(k, overlay.aliases[k])
		}
	}
	return out
}

// fileTable is a standard/curated/supplement pricing table's on-disk YAML
// shape. Every in-memory Table this package produces is USD (LiteLLM's
// native currency, and the canonical-key space this package interoperates
// with is LiteLLM's) — a source file (or one row of it) may declare a
// different currency, converted to USD once at parse time via
// FactorBetween/ParseTableWithRates, rather than carrying a currency tag
// through Resolve/EffectiveRate the way the old, retired report.Pricing sidecar's
// moneyValue did. This is still deliberately simpler than that sidecar's
// general multi-currency graph (arbitrary CCY->CCY chains): every currency
// here goes through one USD pivot hop, since every real-world rate has a
// USD cross-rate and nothing in this package's own data (LiteLLM's
// standard table) is denominated any other way.
type fileTable struct {
	Currency    string     `yaml:"currency"`
	GeneratedAt string     `yaml:"generated_at"`
	Rates       []fileRate `yaml:"rates"`
	// Aliases is this file's bare-model-name -> canonical-key map (see
	// Table.aliases). A reference, not a price: it never carries numbers,
	// so a regenerated standard table moves every alias's rate with it and
	// nothing here goes stale on its own.
	Aliases map[string]string `yaml:"aliases"`
	// ExchangeRate is this file's OWN "1 USD = X <code>" map (same shape as
	// config.yaml's pricing.exchange_rate), consulted BEFORE the rates
	// argument parseTable was called with — a supplement/standard-override
	// file that declares its own rate here stays fully self-contained and
	// portable: its rows' USD-equivalent prices never drift just because
	// the consuming config.yaml's accounting-currency rate later changes
	// for an unrelated reason, and the file can be copied to a different
	// deployment (different pricing.currency, maybe no matching
	// pricing.exchange_rate entry at all) and still resolve correctly. A
	// currency this file doesn't declare a rate for still falls back to
	// the caller-supplied rates (typically config.yaml's
	// pricing.exchange_rate) — this field is a per-file override, not a
	// replacement for that shared table.
	ExchangeRate map[string]float64 `yaml:"exchange_rate"`
}

// fileRate is one fileTable row. Pointer fields: an absent YAML key decodes
// to nil (unknown), present-with-0.0 decodes to a non-nil pointer at 0.0
// (explicitly free) — this is the exact mechanism Rate's "missing vs zero"
// distinction is built on; no custom UnmarshalYAML is needed to get it
// (see config.LimitConfig's own doc comment for the same "plain fields
// beat a custom decoder" precedent this package follows).
//
// Currency optionally overrides the table's own top-level currency for this
// one row — e.g. a domestic vendor's row entered straight from its official
// CNY price list inside an otherwise-USD supplement file. Empty means
// "inherit the table's currency:" (itself USD if that's also empty). Only
// ParseTableWithRates actually honors a non-USD value; plain ParseTable
// (the embedded standard/curated tables' loader) rejects it exactly as
// before — see that function's doc comment.
type fileRate struct {
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
// deals with (the standard table's own USD, a supplement row's native
// currency, an account override's currency, vmr report's display currency)
// has a well-known USD cross-rate, so a general multi-hop graph (the old,
// retired report.Pricing sidecar's approach — see this package's doc
// comment) would be unneeded complexity, not a missing capability.
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

// ParseTable parses one standard/curated/supplement-shaped YAML document.
// Every table this package reads via this entry point is asserted (not
// merely assumed) to be USD — see fileTable's doc comment; a table
// declaring anything else is a load-time error rather than a silent
// misinterpretation. Used for the embedded standard/curated tables
// (LoadStandard), which are authored by us and never need conversion — see
// ParseTableWithRates for the user-supplied-file entry point that does.
func ParseTable(data []byte) (*Table, error) {
	return parseTable(data, nil)
}

// ParseTableWithRates is ParseTable plus support for a table (top-level
// currency:) or individual rows (fileRate.Currency) denominated in a
// non-USD currency, auto-converted to USD at parse time via FactorBetween
// — internal/config's buildPricingContext uses this for
// pricing.supplement/pricing.standard, so a user can author a rate straight
// from a vendor's native-currency official price without hand-converting to
// USD first. rates is the FALLBACK conversion source (typically config.yaml's
// pricing.exchange_rate) — the file's own fileTable.ExchangeRate block, if
// it declares one, wins on a matching currency code (see that field's doc
// comment for why: a self-contained file shouldn't have its resolved prices
// silently drift when some unrelated deployment's rate later changes). nil
// rates with no file-level exchange_rate: either behaves exactly like
// ParseTable (any non-USD currency is a load-time error).
func ParseTableWithRates(data []byte, rates map[string]float64) (*Table, error) {
	return parseTable(data, rates)
}

func parseTable(data []byte, rates map[string]float64) (*Table, error) {
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
	// The file's own exchange_rate: block (if any) wins over the
	// caller-supplied rates on a matching key — see fileTable.ExchangeRate's
	// doc comment for why a self-declared rate must take priority (a
	// self-contained, portable file) rather than the shared table always
	// winning (which would defeat the point of declaring one locally at
	// all).
	effectiveRates := rates
	if len(ft.ExchangeRate) > 0 {
		effectiveRates = make(map[string]float64, len(rates)+len(ft.ExchangeRate))
		for k, v := range rates {
			effectiveRates[k] = v
		}
		for k, v := range ft.ExchangeRate {
			effectiveRates[k] = v
		}
	}
	defaultCCY := strings.ToUpper(strings.TrimSpace(ft.Currency))
	if defaultCCY == "" {
		defaultCCY = "USD"
	}
	// Validated unconditionally, even for a table with zero rows (or every
	// row naming its own currency) — declaring a table-level currency: is a
	// commitment that it CAN convert, not just that something happens to
	// need it right now.
	if defaultCCY != "USD" {
		if _, ok := FactorBetween(defaultCCY, "USD", effectiveRates); !ok {
			return nil, fmt.Errorf("parse pricing table: currency %q has no matching pricing.exchange_rate entry to convert into USD (write exchange_rate: {%s: <rate>}, \"1 USD = <rate> %s\", either in this file itself or in config.yaml's pricing.exchange_rate)", defaultCCY, defaultCCY, defaultCCY)
		}
	}
	// The resulting Table is ALWAYS USD, regardless of what the source file
	// declared — every row below is normalized to USD before t.put, so
	// every downstream consumer (Merge, Resolve, the discount-chain
	// recursion in resolve.go) keeps operating on pure-USD Rate values
	// exactly as before this function existed.
	t := NewTable("USD")
	t.GeneratedAt = ft.GeneratedAt
	for from, to := range ft.Aliases {
		if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
			return nil, fmt.Errorf("parse pricing table: aliases: both sides must be non-empty (got %q -> %q)", from, to)
		}
		t.putAlias(from, to)
	}
	seen := map[string]bool{}
	for i, r := range ft.Rates {
		if r.Key == "" {
			return nil, fmt.Errorf("parse pricing table: rates[%d]: key is required", i)
		}
		// Canonical keys are exactly "vendor/basename" (or a bare name). A
		// deeper key — openrouter's forced "meta-llama/llama-3.3-70b-instruct",
		// fireworks' "accounts/fireworks/models/..." — looks self-consistent
		// but is invisible to bare-name and suffix resolution, which strip
		// org prefixes via ModelBasename (see resolveCanonicalKey's fallback):
		// it would silently split one physical model into two namespaces that
		// can never see each other. Reject at load time so a hand-written
		// supplement names the two-segment key instead.
		if strings.Count(r.Key, "/") > 1 {
			return nil, fmt.Errorf("parse pricing table: rates[%d]: key %q must be \"vendor/basename\" or a bare name (at most one \"/\") — org/path prefixes are not model identity and are stripped from every table key (see pricing.ModelBasename)", i, r.Key)
		}
		lk := strings.ToLower(r.Key)
		if seen[lk] {
			return nil, fmt.Errorf("parse pricing table: rates[%d]: duplicate key %q", i, r.Key)
		}
		seen[lk] = true
		rowCCY := strings.ToUpper(strings.TrimSpace(r.Currency))
		if rowCCY == "" {
			rowCCY = defaultCCY
		}
		rate := Rate{InFresh: r.InFresh, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite, Out: r.Out}
		if rowCCY != "USD" {
			factor, ok := FactorBetween(rowCCY, "USD", effectiveRates)
			if !ok {
				return nil, fmt.Errorf("parse pricing table: rates[%d]: currency %q has no matching pricing.exchange_rate entry to convert into USD (write exchange_rate: {%s: <rate>}, \"1 USD = <rate> %s\", either in this file itself or in config.yaml's pricing.exchange_rate)", i, rowCCY, rowCCY, rowCCY)
			}
			rate = rate.Scale(factor)
		}
		// Reject NaN, Inf, and negative rates — a hand-written supplement
		// file can have a typo (e.g. "-5.0" or ".nan") that silently poisons
		// every downstream consumer (Counters.Cost, ScoreForLimits, Flush).
		// Each component is checked individually so the error message names
		// the exact field the user needs to fix.
		for _, comp := range []struct {
			name string
			val  *float64
		}{{"in_fresh", rate.InFresh}, {"cache_read", rate.CacheRead}, {"cache_write", rate.CacheWrite}, {"out", rate.Out}} {
			if comp.val == nil {
				continue
			}
			if math.IsNaN(*comp.val) {
				return nil, fmt.Errorf("parse pricing table: rates[%d]: key %q: %s is NaN", i, r.Key, comp.name)
			}
			if math.IsInf(*comp.val, 0) {
				return nil, fmt.Errorf("parse pricing table: rates[%d]: key %q: %s is Inf", i, r.Key, comp.name)
			}
			if *comp.val < 0 {
				return nil, fmt.Errorf("parse pricing table: rates[%d]: key %q: %s is negative (%v)", i, r.Key, comp.name, *comp.val)
			}
		}
		t.put(r.Key, rate)
	}
	return t, nil
}
