// Ver 2026-08-02, by Sonnet 5
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/pricing"
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

// endpointKeyWidth is the fixed column an endpoint's "- p=N. provider/model:"
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

// orNoneList renders an optional string-list field.
func orNoneList(ss []string) string {
	if len(ss) == 0 {
		return "(none)"
	}
	return strings.Join(ss, ",")
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
	printGlobalSettings(os.Stdout, cfg, issues)
	fmt.Println()
	printProviders(os.Stdout, cfg)
	if len(cfg.FallbackEndpoints) > 0 {
		fmt.Println()
		printFallbackEndpoints(os.Stdout, cfg)
	}
	fmt.Println()
	printModels(os.Stdout, cfg, snap, issues)

	var errs, warns []config.Issue
	for _, is := range issues {
		if is.Severity == config.SeverityWarning {
			warns = append(warns, is)
		} else {
			errs = append(errs, is)
		}
	}

	fmt.Println()
	if len(warns) > 0 {
		fmt.Println("=== Warnings ===")
		for _, is := range warns {
			fmt.Printf("- %s\n", is.Message)
		}
	}
	if len(errs) == 0 {
		fmt.Println("=== OK ===")
		fmt.Println("config and routing are valid — to test real connectivity: vmr diagnose")
		return nil
	}
	fmt.Println("=== Failed ===")
	for _, is := range errs {
		fmt.Printf("- %s\n", is.Message)
	}
	return fmt.Errorf("%d config issue(s) found", len(errs))
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

func printGlobalSettings(w io.Writer, cfg *config.Config, issues []config.Issue) {
	fmt.Fprintln(w, "=== Global Settings ===")
	listen := cfg.Listen
	if hasIssue(issues, "", "", "", "listen") {
		listen = warn(listen)
	}
	fmt.Fprintln(w, checkLine(0, "listen", listen))
	auth := "off"
	if len(cfg.APIKeys) > 0 {
		auth = fmt.Sprintf("on (%d key(s))", len(cfg.APIKeys))
	}
	fmt.Fprintln(w, checkLine(0, "auth", auth))
	fmt.Fprintln(w, checkLine(0, "max_attempts", orUnlimited(cfg.MaxAttempts)))
	fmt.Fprintln(w, checkLine(0, "max_request_body", fmt.Sprintf("%dMB", cfg.MaxRequestBodyMB)))
	fmt.Fprintln(w, checkLine(0, "max_concurrency", orUnlimited(cfg.MaxConcurrency)))
	imgScale := "off"
	if cfg.ImageDownscaleMaxPx > 0 {
		imgScale = fmt.Sprintf("%dpx", cfg.ImageDownscaleMaxPx)
	}
	fmt.Fprintln(w, checkLine(0, "image_downscale", imgScale))
	fmt.Fprintln(w, checkLine(0, "image_cache_ttl", fmt.Sprintf("%dd", cfg.ImageCacheTTLDays)))
	retention := "forever"
	if cfg.AuditRetentionDays > 0 {
		retention = fmt.Sprintf("%dd", cfg.AuditRetentionDays)
	}
	fmt.Fprintln(w, checkLine(0, "audit_retention", retention))
	fmt.Fprintln(w, checkLine(0, "extra_redact_headers", orNoneList(cfg.ExtraRedactHeaders)))
	fmt.Fprintln(w, checkLine(0, "sticky_ttl", cfg.StickyTTL.D().String()))
	probeTimeout := cfg.ProbeTimeout.D().String()
	if hasIssue(issues, "", "", "", "probe_timeout") {
		probeTimeout = warn(probeTimeout)
	}
	fmt.Fprintln(w, checkLine(0, "probe_timeout", probeTimeout))
	// redactProxyURL: cfg.HTTPProxy/HTTPSProxy can carry embedded userinfo
	// credentials (http://user:pass@host:port), same as a provider's own
	// proxy: URL below — printing either raw to a terminal or a log file
	// leaks a credential a human didn't ask to see.
	fmt.Fprintln(w, checkLine(0, "http_proxy", orNone(redactProxyURL(cfg.HTTPProxy))))
	fmt.Fprintln(w, checkLine(0, "https_proxy", orNone(redactProxyURL(cfg.HTTPSProxy))))
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
	fmt.Fprintln(w, checkLine(0, "timezone", fmt.Sprintf("%s (UTC%s)", fmtutil.DisplayZone.String(), time.Now().In(fmtutil.DisplayZone).Format("-07:00"))))
	if line, ok := pricingTableLine(cfg); ok {
		fmt.Fprintln(w, checkLine(0, "pricing_table", line))
	}
	fmt.Fprintln(w, "dirs:")
	fmt.Fprintln(w, checkLine(2, "log", cfg.LogDir))
	fmt.Fprintln(w, checkLine(2, "image_cache", cfg.ImageCacheDir))
	fmt.Fprintln(w, "timeouts:")
	fmt.Fprintln(w, checkLine(2, "connect", cfg.Timeouts.Connect.D().String()))
	responseHeader := cfg.Timeouts.ResponseHeader.D().String()
	if hasIssue(issues, "", "", "", "probe_timeout") {
		responseHeader = warn(responseHeader) // the probe_timeout/response_header contradiction is about both values together
	}
	fmt.Fprintln(w, checkLine(2, "response_header", responseHeader))
	fmt.Fprintln(w, checkLine(2, "stream_idle", cfg.Timeouts.StreamIdle.D().String()))
}

func printProviders(w io.Writer, cfg *config.Config) {
	fmt.Fprintln(w, "=== Providers ===")
	providers := append([]config.Provider(nil), cfg.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	proxyDesc := providerProxyEntries(cfg)
	descFor := map[string]providerProxyEntry{}
	for _, e := range proxyDesc {
		descFor[e.Name] = e
	}
	for _, p := range providers {
		fmt.Fprintln(w, p.Name+":")
		if p.APIKey == "" {
			fmt.Fprintln(w, checkLine(2, "api_key", warn("missing")))
		} else {
			fmt.Fprintln(w, checkLine(2, "api_key", maskAPIKey(p.APIKey)))
		}
		protocols := fmtutil.SortedKeys(p.BaseURL)
		for _, protocol := range protocols {
			fmt.Fprintln(w, checkLine(2, fmt.Sprintf("base_url(%s)", protocol), p.BaseURL[protocol]))
		}
		fmt.Fprintln(w, checkLine(2, "proxy", providerProxyLine(p, protocols, descFor)))
		printProviderQuota(w, cfg, p)
		printProviderPricing(w, cfg, p)
	}
}

// printFallbackEndpoints previews the raw Config.FallbackEndpoints entries
// (their per-model expansion shows up as `fallback` tags under === Models
// === below).
func printFallbackEndpoints(w io.Writer, cfg *config.Config) {
	fmt.Fprintln(w, "=== Fallback Endpoints ===")
	for i, fb := range cfg.FallbackEndpoints {
		fmt.Fprintf(w, "%d:\n", i+1)
		fmt.Fprintln(w, checkLine(2, "protocol", fb.Protocol))
		fmt.Fprintln(w, checkLine(2, "providers", strings.Join(fb.Providers, ",")))
		fmt.Fprintln(w, checkLine(2, "models", strings.Join(fb.Models, ",")))
		fmt.Fprintln(w, checkLine(2, "priority", fmt.Sprintf("%d", fb.Priority)))
	}
}

// pricingStaleAfter is how old the built-in standard price table may get
// before `vmr check` says so out loud. Two months, not the six it used to
// be: the 2026-08-31 refresh showed a 20-day-old snapshot already missing
// list prices for four models this repo's own traffic was running on
// (gemini-3.7-flash, deepseek-v4-flash-vision-exp, kimi-k3, and a renamed
// glm row). A threshold that only fires after the table has been wrong for
// half a year is not a guardrail. Refresh is one command with no arguments
// (`go run ./tools/gen_standard_pricing -generated-at <today>` fetches
// upstream itself), so the reminder is cheap to act on.
const pricingStaleAfter = 60 * 24 * time.Hour

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
	line := "built-in standard table (generation date unknown)"
	if table.GeneratedAt != "" {
		line = "built-in standard table generated " + table.GeneratedAt
		gen, perr := time.ParseInLocation("2006-01-02", table.GeneratedAt, fmtutil.DisplayZone)
		if perr == nil {
			if age := time.Since(gen); age > pricingStaleAfter {
				line += fmt.Sprintf(" — %d days old, list prices may have moved (regenerate: go run ./tools/gen_standard_pricing -generated-at $(date +%%F))", int(age.Hours()/24))
			}
		}
	}
	// cfg.Pricing's currency/exchange_rate/supplement are otherwise
	// invisible in this output, yet directly determine what unit a
	// metric: cost quota's amount= (below) is denominated in.
	if cfg.Pricing != nil {
		currency := cfg.Pricing.Currency
		switch {
		case currency == "":
			line += "; currency=USD"
		case cfg.Pricing.ExchangeRate[currency] != 0:
			line += fmt.Sprintf("; currency=%s (1 USD = %g %s)", currency, cfg.Pricing.ExchangeRate[currency], currency)
		default:
			line += "; currency=" + currency
		}
		if cfg.Pricing.Supplement != "" {
			line += "; supplement=" + cfg.Pricing.Supplement
		}
		if cfg.Pricing.Standard != "" {
			line += "; standard=" + cfg.Pricing.Standard
		}
	}
	// Aliases silently redirect a bare model name to another vendor's row
	// (see internal/pricing.Table.aliases). That is exactly the kind of
	// resolution an operator should be able to see is in effect before
	// trusting a $ column, so their count is stated even though the
	// individual mappings are not.
	if n := len(table.Aliases()); n > 0 {
		line += fmt.Sprintf("; %d model alias(es)", n)
	}
	return line, true
}

// printProviderPricing renders p's resolved metric: cost pricing , if
// any — one line per upstream model this provider actually resolved a
// price for (see config.Config.ResolvedPricing), so an operator can see
// exactly what rate a cost account will be charged at without cross-
// referencing the standard table by hand. Absent entirely for a provider
// with no resolved pricing, same as every other optional section here.
func printProviderPricing(w io.Writer, cfg *config.Config, p config.Provider) {
	var models []string
	prefix := p.Name + "\x00"
	for key := range cfg.ResolvedPricing {
		if strings.HasPrefix(key, prefix) {
			models = append(models, strings.TrimPrefix(key, prefix))
		}
	}
	if len(models) > 0 {
		sort.Strings(models)
		fmt.Fprintln(w, "  pricing:")
		for _, model := range models {
			spec := cfg.ResolvedPricing[prefix+model]
			// EffectiveRate, not spec.Base: an account with an override (e.g.
			// discount:) is charged at the resolved rate, not the standard
			// table's list price — printing Base here would show an operator a
			// number that has nothing to do with what metric: cost will actually
			// charge.
			r := pricing.EffectiveRate(spec)
			fmt.Fprintln(w, checkLine(4, model, fmt.Sprintf("in_fresh=%s cache_read=%s cache_write=%s out=%s %s/1M (%d override rule(s))",
				ratePart(r.InFresh), ratePart(r.CacheRead), ratePart(r.CacheWrite), ratePart(r.Out), spec.Currency, len(spec.Overrides))))
		}
		return
	}
	if p.Pricing == nil {
		return
	}
	// A declared providers[].pricing block with no ResolvedPricing entry
	// means no virtual model's endpoint currently routes any model to this
	// provider (so resolvePricing had nothing to resolve against) — show
	// the raw declaration instead of silently dropping it, since it's
	// explicit config a human wrote and may expect to see confirmed.
	fmt.Fprintln(w, "  pricing: (declared; not resolved — no routed endpoint references this provider)")
	for _, local := range fmtutil.SortedKeys(p.Pricing.Map) {
		fmt.Fprintln(w, checkLine(4, "map", local+" -> "+p.Pricing.Map[local]))
	}
	for i, oc := range p.Pricing.Overrides {
		val := fmt.Sprintf("in_fresh=%s cache_read=%s cache_write=%s out=%s",
			ratePart(oc.InFresh), ratePart(oc.CacheRead), ratePart(oc.CacheWrite), ratePart(oc.Out))
		if oc.Discount != nil {
			val = fmt.Sprintf("discount=%g", *oc.Discount)
		}
		if oc.Currency != "" {
			val += " currency=" + oc.Currency
		}
		fmt.Fprintln(w, checkLine(4, fmt.Sprintf("overrides[%d] model=%s", i, oc.Model), val))
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
	// anything downstream computes from (router.ChargeResponse/componentCost
	// read the pricing.Rate directly, never this formatted string).
	return strconv.FormatFloat(*v, 'g', 6, 64)
}

// printProviderQuota renders p's quota: block, if any — purely static
// (config-derived), never reads Registry state (that's /status's and
// `vmr status`'s job, see server/admin.go). Absent entirely for a provider
// with no quota: configured, same as every other optional section here.
func printProviderQuota(w io.Writer, cfg *config.Config, p config.Provider) {
	if p.Quota == nil || len(p.Quota.Limits) == 0 {
		return
	}
	fmt.Fprintln(w, "  quota:")
	for _, lc := range p.Quota.Limits {
		l := lc.Resolved
		since := l.Since.In(fmtutil.DisplayZone).Format("2006-01-02 15:04")
		amount := fmt.Sprintf("%g", l.Amount)
		// A cost-metric amount is denominated in cfg.Pricing.Currency
		// (resolvePricing requires it to be set for any metric: cost
		// provider) — bare "amount=698" is otherwise ambiguous.
		if l.Metric == core.MetricCost && cfg.Pricing != nil && cfg.Pricing.Currency != "" {
			amount += " " + cfg.Pricing.Currency
		}
		detail := fmt.Sprintf("every=%s since=%s amount=%s", l.EveryText, since, amount)
		if len(l.Models) > 0 {
			detail += " models=" + strings.Join(l.Models, ",")
		}
		fmt.Fprintln(w, checkLine(4, string(l.Metric), detail))
		// token_weights is always resolved (defaults to all 1.0), but only
		// printed when this Limit actually configured it non-default — an
		// all-1.0 line on every Limit would be noise on the common case
		// (plain token/request counting).
		if tw := l.TokenWeights; tw != core.NewTokenWeights() {
			fmt.Fprintln(w, checkLine(6, "token_weights", fmt.Sprintf("in_fresh=%g cache_read=%g cache_write=%g out=%g", tw.InFresh, tw.CacheRead, tw.CacheWrite, tw.Out)))
		}
		if len(l.ModelMultipliers) > 0 {
			parts := make([]string, 0, len(l.ModelMultipliers))
			for _, model := range fmtutil.SortedKeys(l.ModelMultipliers) {
				parts = append(parts, fmt.Sprintf("%s=%g", model, l.ModelMultipliers[model]))
			}
			fmt.Fprintln(w, checkLine(6, "model_multipliers", strings.Join(parts, " ")))
		}
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

func printModels(w io.Writer, cfg *config.Config, snap *router.Snapshot, issues []config.Issue) {
	fmt.Fprintln(w, "=== Models ===")
	for i, name := range fmtutil.SortedKeys(cfg.Models) {
		if i > 0 {
			fmt.Fprintln(w)
		}
		m := cfg.Models[name]
		fmt.Fprintln(w, name+":")
		caps := "(unconstrained)"
		if len(m.Capabilities) > 0 {
			caps = strings.Join(m.Capabilities, ",")
		}
		fmt.Fprintln(w, checkLine(2, "capabilities", caps))
		tokens := "(unconstrained)"
		if m.MaxContextTokens > 0 {
			tokens = fmt.Sprintf("%d", m.MaxContextTokens)
		}
		fmt.Fprintln(w, checkLine(2, "max_context_tokens", tokens))
		fmt.Fprintln(w, checkLine(2, "strategy", strings.Join(m.Strategy, ",")))
		sticky := m.Sticky == nil || *m.Sticky
		fmt.Fprintln(w, checkLine(2, "sticky", fmt.Sprintf("%v", sticky)))
		if sticky {
			fmt.Fprintln(w, checkLine(2, "sticky_ttl", cfg.StickyTTL.D().String()))
		}
		if m.ImageDownscaleMaxPx != nil {
			fmt.Fprintln(w, checkLine(2, "image_downscale", fmt.Sprintf("%dpx", *m.ImageDownscaleMaxPx)))
		}
		if len(cfg.FallbackEndpoints) > 0 {
			fallbackOK := m.Fallback == nil || *m.Fallback
			fmt.Fprintln(w, checkLine(2, "fallback", fmt.Sprintf("%v", fallbackOK)))
		}
		for _, protocol := range fmtutil.SortedKeys(snap.Models) {
			route, ok := snap.Models[protocol][name]
			if !ok {
				continue
			}
			fmt.Fprintf(w, "  %s:\n", protocol)
			for _, ep := range route.EffectiveOrder() {
				key := ep.Provider + "/" + ep.Model
				var parts []string
				if len(ep.ExtraCapabilities) > 0 {
					parts = append(parts, "extra_capabilities="+strings.Join(ep.ExtraCapabilities, ","))
				}
				if ep.OwnMaxContextTokens > 0 {
					parts = append(parts, fmt.Sprintf("max_context_tokens=%d", ep.OwnMaxContextTokens))
				}
				if len(ep.RoleMap) > 0 {
					rm := make([]string, 0, len(ep.RoleMap))
					for _, from := range fmtutil.SortedKeys(ep.RoleMap) {
						rm = append(rm, from+"->"+ep.RoleMap[from])
					}
					parts = append(parts, "role_map="+strings.Join(rm, ","))
				}
				if sticky && ep.StickyTTL != cfg.StickyTTL.D() {
					parts = append(parts, "sticky_ttl="+ep.StickyTTL.String())
				}
				if ep.APIKey == "" {
					parts = append(parts, warn("key=EMPTY"))
				}
				if ep.FromFallback {
					parts = append(parts, "fallback")
				}
				label := fmt.Sprintf("    - p=%d. %s:", ep.Priority, key)
				line := label
				if len(parts) > 0 {
					line = padLabel(label, endpointKeyWidth) + strings.Join(parts, "; ")
				}
				endpointKey := protocol + "/" + ep.Provider + "/" + ep.Model
				if hasIssue(issues, "", name, endpointKey, "endpoint") && !strings.Contains(line, "⚠️") {
					line = warn(line)
				}
				fmt.Fprintln(w, line)
			}
		}
	}
}
