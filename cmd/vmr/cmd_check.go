// Ver 2026-08-02, by Sonnet 5
package main

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/router"
)

// checkKeyWidth is the fixed column every "vmr check" key label (indent +
// key + colon) pads out to, so every section's value column lines up.
const checkKeyWidth = 30

// checkLine renders one "<indent><key>:<pad>value" row.
func checkLine(indent int, key, val string) string {
	return padLabel(strings.Repeat(" ", indent)+key+":", checkKeyWidth) + val
}

// padLabel right-pads label with spaces out to width (at least one space if
// label is already that long or longer), so whatever follows lines up in a
// column regardless of how long label itself is.
func padLabel(label string, width int) string {
	if len(label) < width {
		return label + strings.Repeat(" ", width-len(label))
	}
	return label + " "
}

// endpointKeyWidth is the fixed column an endpoint's "N. provider/model:"
// label (indent included) pads out to, so extra_capabilities=/
// max_context_tokens=/key=EMPTY values line up the same way checkLine's
// key column does elsewhere.
const endpointKeyWidth = 40

// warn appends the ⚠️ marker used throughout `vmr check` to flag a value
// that also shows up in the trailing Failed summary.
func warn(s string) string { return s + " ⚠️" }

// maskAPIKey renders an upstream credential as a short, unambiguous
// fingerprint — enough to confirm "this is the key I think is configured"
// without ever printing a working secret to a terminal or captured log.
// Never called with an empty key (callers render "missing" for that case).
func maskAPIKey(key string) string {
	if len(key) <= 6 {
		return strings.Repeat("*", len(key))
	}
	return key[:2] + "***" + key[len(key)-4:]
}

// orUnlimited renders a "0/absent = unlimited" int config field.
func orUnlimited(n int) string {
	if n <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", n)
}

// orNone renders an optional string field.
func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// cmdCheck validates the config, runs Config.Check's consistency scan, and
// prints the effective routing table (no network I/O). With a trailing
// "log" or "cache" argument it instead prints just that one resolved
// directory (config.LogDir / config.ImageCacheDir) and returns — the
// scripting-friendly form vmr.sh uses to locate its own server log without
// keeping a second copy of the resolution logic. This absorbs the former
// standalone `vmr dirs` subcommand; the directory values were already part
// of check's normal summary output below.
func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		if fs.NArg() > 1 {
			return fmt.Errorf("usage: vmr check [-c config.yaml] [log|cache]")
		}
		cfg, err := config.Load(*path)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		switch fs.Arg(0) {
		case "log":
			fmt.Println(cfg.LogDir)
		case "cache":
			fmt.Println(cfg.ImageCacheDir)
		default:
			return fmt.Errorf("usage: vmr check [-c config.yaml] [log|cache]")
		}
		return nil
	}

	cfg, err := config.Load(*path)
	if err != nil {
		// A structural/hard-validate failure means there's no defaulted
		// Config to render sections from at all — report it exactly like
		// every other section-ending failure so the two failure modes read
		// the same to a script grepping for "=== Failed ===", even though
		// this one can't be accompanied by a full report.
		fmt.Println("=== Failed ===")
		fmt.Printf("- %v\n", err)
		return err
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		fmt.Println("=== Failed ===")
		fmt.Printf("- %v\n", err)
		return err
	}

	issues := cfg.Check()
	printGlobalSettings(cfg, issues)
	printProviders(cfg)
	printModels(cfg, snap, issues)

	fmt.Println()
	if len(issues) == 0 {
		fmt.Println("=== OK ===")
		fmt.Println("config and routing are valid — to test real connectivity: vmr diagnose")
		return nil
	}
	fmt.Println("=== Failed ===")
	for _, is := range issues {
		fmt.Printf("- %s\n", is.Message)
	}
	return fmt.Errorf("%d config issue(s) found", len(issues))
}

// hasIssue reports whether issues contains one matching this exact
// provider/model/endpoint/field scope — used to decide whether a rendered
// line gets the ⚠️ marker.
func hasIssue(issues []config.Issue, provider, model, endpoint, field string) bool {
	for _, is := range issues {
		if is.Provider == provider && is.Model == model && is.Endpoint == endpoint && is.Field == field {
			return true
		}
	}
	return false
}

func printGlobalSettings(cfg *config.Config, issues []config.Issue) {
	fmt.Println("=== Global Settings ===")
	fmt.Println(checkLine(0, "listen", cfg.Listen))
	auth := "off"
	if len(cfg.APIKeys) > 0 {
		auth = fmt.Sprintf("on (%d key(s))", len(cfg.APIKeys))
	}
	fmt.Println(checkLine(0, "auth", auth))
	fmt.Println(checkLine(0, "max_attempts", orUnlimited(cfg.MaxAttempts)))
	fmt.Println(checkLine(0, "max_request_body", fmt.Sprintf("%dMB", cfg.MaxRequestBodyMB)))
	fmt.Println(checkLine(0, "max_concurrency", orUnlimited(cfg.MaxConcurrency)))
	imgScale := "off"
	if cfg.ImageDownscaleMaxPx > 0 {
		imgScale = fmt.Sprintf("%dpx", cfg.ImageDownscaleMaxPx)
	}
	fmt.Println(checkLine(0, "image_downscale", imgScale))
	fmt.Println(checkLine(0, "image_cache_ttl", fmt.Sprintf("%dd", cfg.ImageCacheTTLDays)))
	retention := "forever"
	if cfg.AuditRetentionDays > 0 {
		retention = fmt.Sprintf("%dd", cfg.AuditRetentionDays)
	}
	fmt.Println(checkLine(0, "audit_retention", retention))
	fmt.Println(checkLine(0, "sticky_ttl", cfg.StickyTTL.D().String()))
	probeTimeout := cfg.ProbeTimeout.D().String()
	if hasIssue(issues, "", "", "", "probe_timeout") {
		probeTimeout = warn(probeTimeout)
	}
	fmt.Println(checkLine(0, "probe_timeout", probeTimeout))
	fmt.Println(checkLine(0, "http_proxy", orNone(cfg.HTTPProxy)))
	fmt.Println(checkLine(0, "https_proxy", orNone(cfg.HTTPSProxy)))
	// Quota period boundaries (and every other human-facing timestamp) render
	// through fmtutil.DisplayZone, which is just time.Local — a container
	// with no TZ set is silently UTC, which can differ from an operator's
	// mental model by several hours with no other visible symptom (see the
	// design doc's Timezone section). Printing the resolved value here is
	// the cheapest possible guard against that.
	// time.Local's own Name() is literally the string "Local" — useless for
	// telling "TZ correctly set to Asia/Shanghai" apart from "TZ unset,
	// silently UTC", so the actual offset is appended (both read the same
	// "Local" either way, but the offset gives it away).
	fmt.Println(checkLine(0, "timezone", fmt.Sprintf("%s (UTC%s)", fmtutil.DisplayZone.String(), time.Now().In(fmtutil.DisplayZone).Format("-07:00"))))
	if line, ok := pricingTableLine(cfg); ok {
		fmt.Println(checkLine(0, "pricing_table", line))
	}
	fmt.Println("dirs:")
	fmt.Println(checkLine(2, "log", cfg.LogDir))
	fmt.Println(checkLine(2, "image_cache", cfg.ImageCacheDir))
	fmt.Println("timeouts:")
	fmt.Println(checkLine(2, "connect", cfg.Timeouts.Connect.D().String()))
	responseHeader := cfg.Timeouts.ResponseHeader.D().String()
	if hasIssue(issues, "", "", "", "probe_timeout") {
		responseHeader = warn(responseHeader) // the probe_timeout/response_header contradiction is about both values together
	}
	fmt.Println(checkLine(2, "response_header", responseHeader))
	fmt.Println(checkLine(2, "stream_idle", cfg.Timeouts.StreamIdle.D().String()))
}

func printProviders(cfg *config.Config) {
	fmt.Println()
	fmt.Println("=== Providers ===")
	providers := append([]config.Provider(nil), cfg.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	proxyDesc := providerProxyEntries(cfg)
	descFor := map[string]providerProxyEntry{}
	for _, e := range proxyDesc {
		descFor[e.Name] = e
	}
	for _, p := range providers {
		fmt.Println(p.Name + ":")
		if p.APIKey == "" {
			fmt.Println(checkLine(2, "api_key", warn("missing")))
		} else {
			fmt.Println(checkLine(2, "api_key", maskAPIKey(p.APIKey)))
		}
		protocols := core.SortedKeys(p.BaseURL)
		for _, protocol := range protocols {
			fmt.Println(checkLine(2, fmt.Sprintf("base_url(%s)", protocol), p.BaseURL[protocol]))
		}
		fmt.Println(checkLine(2, "proxy", providerProxyLine(p, protocols, descFor)))
		printProviderQuota(p)
		printProviderPricing(cfg, p)
	}
}

// pricingStaleAfter is how old the built-in standard price table may get
// before `vmr check` says so out loud. The design doc's pricing guardrails
// put "过期比缺失更危险" first: a stale-but-authoritative-looking table
// silently produces wrong $ figures, where a missing one at least produces
// none. Half a year is deliberately loose — list prices move, but not so
// fast that a monthly nag would be anything but noise, and this is a
// reminder (plain text, no ⚠️), never a validation failure: an out-of-date
// reference price must not be able to stop a router from starting.
const pricingStaleAfter = 180 * 24 * time.Hour

// pricingTableLine describes the standard price table backing this config's
// $ figures — its generation date, and whether that date is old enough to
// deserve a refresh. ok=false when nothing in this config touches pricing at
// all (no global pricing: block, no providers[].pricing, no metric: cost
// Limit), so the common quota-free config gains no new line.
func pricingTableLine(cfg *config.Config) (string, bool) {
	if len(cfg.ProviderPricingPolicies) == 0 && cfg.Pricing == nil {
		return "", false
	}
	table, err := cfg.PricingTable()
	if err != nil || table == nil {
		return "", false
	}
	if table.GeneratedAt == "" {
		return "built-in standard table (generation date unknown)", true
	}
	line := "built-in standard table generated " + table.GeneratedAt
	gen, perr := time.ParseInLocation("2006-01-02", table.GeneratedAt, fmtutil.DisplayZone)
	if perr == nil {
		if age := time.Since(gen); age > pricingStaleAfter {
			line += fmt.Sprintf(" — %d days old, list prices may have moved (regenerate: go run ./tools/gen_standard_pricing)", int(age.Hours()/24))
		}
	}
	return line, true
}

// printProviderPricing renders p's resolved metric: cost pricing (P2.2), if
// any — one line per upstream model this provider actually resolved a
// price for (see config.Config.ResolvedPricing), so an operator can see
// exactly what rate a cost account will be charged at without cross-
// referencing the standard table by hand. Absent entirely for a provider
// with no resolved pricing, same as every other optional section here.
func printProviderPricing(cfg *config.Config, p config.Provider) {
	if len(cfg.ResolvedPricing) == 0 {
		return
	}
	var models []string
	prefix := p.Name + "\x00"
	for key := range cfg.ResolvedPricing {
		if strings.HasPrefix(key, prefix) {
			models = append(models, strings.TrimPrefix(key, prefix))
		}
	}
	if len(models) == 0 {
		return
	}
	sort.Strings(models)
	fmt.Println("  pricing:")
	for _, model := range models {
		spec := cfg.ResolvedPricing[prefix+model]
		r := spec.Base
		fmt.Println(checkLine(4, model, fmt.Sprintf("in_fresh=%s cache_read=%s cache_write=%s out=%s %s/1M (%d override rule(s))",
			ratePart(r.InFresh), ratePart(r.CacheRead), ratePart(r.CacheWrite), ratePart(r.Out), spec.Currency, len(spec.Overrides))))
	}
}

// ratePart renders one Rate component for display — "?" makes a missing
// (nil) component visually distinct from an explicit 0, the same
// distinction internal/pricing.Rate exists to preserve everywhere else.
func ratePart(v *float64) string {
	if v == nil {
		return "?"
	}
	// %.6g rather than %g: a rate that went through an exchange-rate
	// multiplication (e.g. 3.0 USD x 7.1) routinely lands on a float64 like
	// 21.299999999999997 — cosmetic noise for a display line, not a value
	// anything downstream computes from (chargeCost/componentCost read the
	// pricing.Rate directly, never this formatted string).
	return strconv.FormatFloat(*v, 'g', 6, 64)
}

// printProviderQuota renders p's quota: block, if any — purely static
// (config-derived), never reads Registry state (that's /admin/status's and
// `vmr status`'s job, see server/admin.go). Absent entirely for a provider
// with no quota: configured, same as every other optional section here.
func printProviderQuota(p config.Provider) {
	if p.Quota == nil || len(p.Quota.Limits) == 0 {
		return
	}
	fmt.Println("  quota:")
	for _, lc := range p.Quota.Limits {
		l := lc.Resolved
		since := l.Since.In(fmtutil.DisplayZone).Format("2006-01-02 15:04")
		fmt.Println(checkLine(4, string(l.Metric), fmt.Sprintf("every=%s since=%s amount=%g", l.EveryText, since, l.Amount)))
	}
	// token_weights is always resolved (defaults to all 1.0), but only
	// printed when the account actually configured it non-default — an
	// all-1.0 line on every quota-configured provider would be noise on the
	// common case (P1-style plain token/request counting).
	w := p.Quota.ResolvedTokenWeights
	if w != core.NewTokenWeights() {
		fmt.Println(checkLine(4, "token_weights", fmt.Sprintf("in_fresh=%g cache_read=%g cache_write=%g out=%g", w.InFresh, w.CacheRead, w.CacheWrite, w.Out)))
	}
	if len(p.Quota.ModelMultipliers) > 0 {
		parts := make([]string, 0, len(p.Quota.ModelMultipliers))
		for _, model := range core.SortedKeys(p.Quota.ModelMultipliers) {
			parts = append(parts, fmt.Sprintf("%s=%g", model, p.Quota.ModelMultipliers[model]))
		}
		fmt.Println(checkLine(4, "model_multipliers", strings.Join(parts, " ")))
	}
}

// providerProxyLine collapses a provider's per-protocol proxy resolution
// into the single line `vmr check` renders: Provider.Proxy is one switch
// per provider, not per protocol, so the common case — every declared
// base_url resolving the same way — needs only one value. The rare case
// where http/https base_urls resolve differently (say only https_proxy is
// configured) falls back to listing each protocol's own resolution instead
// of hiding the discrepancy behind a single misleading value.
func providerProxyLine(p config.Provider, protocols []string, descFor map[string]providerProxyEntry) string {
	descs := map[string]bool{}
	for _, protocol := range protocols {
		descs[descFor[protocol+"/"+p.Name].Proxy] = true
	}
	out := descFor[protocols[0]+"/"+p.Name].Proxy
	if len(descs) > 1 {
		parts := make([]string, len(protocols))
		for i, protocol := range protocols {
			parts[i] = fmt.Sprintf("%s=%s", protocol, descFor[protocol+"/"+p.Name].Proxy)
		}
		out = strings.Join(parts, ", ")
	}
	return out
}

func printModels(cfg *config.Config, snap *router.Snapshot, issues []config.Issue) {
	fmt.Println()
	fmt.Println("=== Models ===")
	for i, name := range core.SortedKeys(cfg.Models) {
		if i > 0 {
			fmt.Println()
		}
		m := cfg.Models[name]
		fmt.Println(name + ":")
		caps := "(unconstrained)"
		if len(m.Capabilities) > 0 {
			caps = strings.Join(m.Capabilities, ",")
		}
		fmt.Println(checkLine(2, "capabilities", caps))
		tokens := "(unconstrained)"
		if m.MaxContextTokens > 0 {
			tokens = fmt.Sprintf("%d", m.MaxContextTokens)
		}
		fmt.Println(checkLine(2, "max_context_tokens", tokens))
		sticky := m.Sticky == nil || *m.Sticky
		fmt.Println(checkLine(2, "sticky", fmt.Sprintf("%v", sticky)))
		if sticky {
			fmt.Println(checkLine(2, "sticky_ttl", cfg.StickyTTL.D().String()))
		}
		if m.ImageDownscaleMaxPx != nil {
			fmt.Println(checkLine(2, "image_downscale", fmt.Sprintf("%dpx", *m.ImageDownscaleMaxPx)))
		}
		for _, protocol := range core.SortedKeys(snap.Models) {
			route, ok := snap.Models[protocol][name]
			if !ok {
				continue
			}
			fmt.Printf("  %s:\n", protocol)
			for epIdx, ep := range route.EffectiveOrder() {
				key := ep.Provider + "/" + ep.Model
				var parts []string
				if len(ep.ExtraCapabilities) > 0 {
					parts = append(parts, "extra_capabilities="+strings.Join(ep.ExtraCapabilities, ","))
				}
				if ep.OwnMaxContextTokens > 0 {
					parts = append(parts, fmt.Sprintf("max_context_tokens=%d", ep.OwnMaxContextTokens))
				}
				if ep.APIKey == "" {
					parts = append(parts, warn("key=EMPTY"))
				}
				label := fmt.Sprintf("    %d. %s:", epIdx+1, key)
				line := label
				if len(parts) > 0 {
					line = padLabel(label, endpointKeyWidth) + strings.Join(parts, "; ")
				}
				endpointKey := protocol + "/" + ep.Provider + "/" + ep.Model
				if hasIssue(issues, "", name, endpointKey, "endpoint") && !strings.Contains(line, "⚠️") {
					line = warn(line)
				}
				fmt.Println(line)
			}
		}
	}
}
