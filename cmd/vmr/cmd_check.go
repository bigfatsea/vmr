// Ver 2026-07-30, by Sonnet 5
package main

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"vmr/internal/config"
	"vmr/internal/core"
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
	printProviders(cfg, issues)
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
	fmt.Println(checkLine(0, "probe_mode", cfg.ProbeMode))
	probeTimeout := cfg.ProbeTimeout.D().String()
	if hasIssue(issues, "", "", "", "probe_timeout") {
		probeTimeout = warn(probeTimeout)
	}
	fmt.Println(checkLine(0, "probe_timeout", probeTimeout))
	// proxy grouped with http_proxy/https_proxy: it's the global default
	// switch for whichever of those two URLs actually applies, so the three
	// read as one unit rather than proxy sitting apart from the URLs it
	// defaults for.
	fmt.Println(checkLine(0, "proxy", fmt.Sprintf("%v", cfg.Proxy)))
	fmt.Println(checkLine(0, "http_proxy", orNone(cfg.HTTPProxy)))
	fmt.Println(checkLine(0, "https_proxy", orNone(cfg.HTTPSProxy)))
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

func printProviders(cfg *config.Config, issues []config.Issue) {
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
		fmt.Println(checkLine(2, "proxy", providerProxyLine(p, protocols, descFor, issues)))
	}
}

// providerProxyLine collapses a provider's per-protocol proxy resolution
// into the single line `vmr check` renders: Provider.Proxy is one switch
// per provider, not per protocol, so the common case — every declared
// base_url resolving the same way — needs only one value. The rare case
// where http/https base_urls resolve differently (say only https_proxy is
// configured) falls back to listing each protocol's own resolution instead
// of hiding the discrepancy behind a single misleading value.
func providerProxyLine(p config.Provider, protocols []string, descFor map[string]providerProxyEntry, issues []config.Issue) string {
	descs := map[string]bool{}
	anyIssue := false
	for _, protocol := range protocols {
		descs[descFor[protocol+"/"+p.Name].Proxy] = true
		if hasIssue(issues, p.Name, "", protocol, "proxy") {
			anyIssue = true
		}
	}
	out := descFor[protocols[0]+"/"+p.Name].Proxy
	if len(descs) > 1 {
		parts := make([]string, len(protocols))
		for i, protocol := range protocols {
			parts[i] = fmt.Sprintf("%s=%s", protocol, descFor[protocol+"/"+p.Name].Proxy)
		}
		out = strings.Join(parts, ", ")
	}
	if anyIssue {
		out = warn(out)
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
