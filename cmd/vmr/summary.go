// Ver 2026-07-26, by Sonnet 5

// Config-summary rendering shared by cmd_start.go (startup/reload banner)
// and cmd_check.go (`vmr check` output): configFlag, logConfigSummary, and
// the provider-proxy-line helpers both draw on.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/router"
)

func configFlag(args []string, cmd string) (string, error) {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	path := fs.String("c", "config.yaml", "path to config file")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	return *path, nil
}

// logConfigSummary prints what the running instance is actually configured
// to do: limits, timeouts, and every virtual model's endpoints in their
// effective try order with key state. Printed at startup and after each
// successful hot reload, so the console always reflects the live config.
//
// Each of the three sections (global/provider/model) is built as one
// multi-line string and emitted through a single logger.Printf call.
// log.Logger writes its formatted message in exactly one Write, and
// stampWriter stamps once per Write — so the timestamp lands only on the
// section header line, while the indented detail lines stay unstamped and
// readable as a block.
func logConfigSummary(logger *log.Logger, cfg *config.Config, snap *router.Snapshot) {
	orNoLimit := func(v int, unit string) string {
		if v <= 0 {
			return "unlimited"
		}
		return fmt.Sprintf("%d%s", v, unit)
	}
	auth := "off"
	if len(cfg.APIKeys) > 0 {
		auth = "on"
	}
	imgScale := "off"
	if cfg.ImageDownscaleMaxPx > 0 {
		imgScale = fmt.Sprintf("%dpx", cfg.ImageDownscaleMaxPx)
	}
	retention := "forever"
	if cfg.AuditRetentionDays > 0 {
		retention = fmt.Sprintf("%dd", cfg.AuditRetentionDays)
	}

	const globalKeyWidth = 17 // len("max_request_body"), the widest field name below
	field := func(indent int, key string, val any) string {
		return fmt.Sprintf("\n%s%-*s = %v", strings.Repeat(" ", indent), globalKeyWidth, key, val)
	}

	var global strings.Builder
	global.WriteString("global config:")
	global.WriteString(field(4, "listen", cfg.Listen))
	global.WriteString(field(4, "auth", auth))
	global.WriteString(field(4, "max_attempts", orNoLimit(cfg.MaxAttempts, "")))
	global.WriteString(field(4, "max_request_body", fmt.Sprintf("%dMB", cfg.MaxRequestBodyMB)))
	global.WriteString(field(4, "max_concurrency", orNoLimit(cfg.MaxConcurrency, "")))
	global.WriteString(field(4, "image_downscale", imgScale))
	global.WriteString(field(4, "image_cache_ttl", fmt.Sprintf("%dd", cfg.ImageCacheTTLDays)))
	global.WriteString(field(4, "audit_retention", retention))
	global.WriteString(field(4, "probe_mode", cfg.ProbeMode))
	global.WriteString(field(4, "probe_timeout", cfg.ProbeTimeout.D()))
	global.WriteString("\n    timeouts")
	global.WriteString(field(8, "connect", cfg.Timeouts.Connect.D()))
	global.WriteString(field(8, "response_header", cfg.Timeouts.ResponseHeader.D()))
	global.WriteString(field(8, "stream_idle", cfg.Timeouts.StreamIdle.D()))
	global.WriteString("\n    dirs")
	global.WriteString(field(8, "log", cfg.LogDir))
	global.WriteString(field(8, "image_cache", cfg.ImageCacheDir))
	logger.Printf("%s", global.String())

	if entries := providerProxyEntries(cfg); len(entries) > 0 {
		nameWidth := 0
		for _, e := range entries {
			if len(e.Name) > nameWidth {
				nameWidth = len(e.Name)
			}
		}
		var provider strings.Builder
		provider.WriteString("provider config:")
		for _, e := range entries {
			marker := ""
			if e.IsProxied {
				marker = " (proxy)"
			}
			fmt.Fprintf(&provider, "\n    %-*s base_url=%s%s", nameWidth, e.Name, e.BaseURL, marker)
		}
		logger.Printf("%s", provider.String())
	}

	var model strings.Builder
	model.WriteString("model config:")
	for _, protocol := range core.SortedKeys(cfg.Models) {
		for _, name := range core.SortedKeys(cfg.Models[protocol]) {
			route := snap.Models[protocol][name]
			imgOverride := ""
			if route.ImageDownscaleMaxPx != nil {
				imgOverride = fmt.Sprintf(" (image_downscale=%dpx)", *route.ImageDownscaleMaxPx)
			}
			fmt.Fprintf(&model, "\n    %s/%s%s", protocol, name, imgOverride)
			for i, ep := range route.EffectiveOrder() {
				fmt.Fprintf(&model, "\n        %d.%s/%s, max_context_tokens=%s, capabilities=%s",
					i+1, ep.Provider, ep.Model, fmtMaxContextTokens(ep.MaxContextTokens), fmtCapabilities(ep.Capabilities))
			}
		}
	}
	logger.Printf("%s", model.String())
}

// fmtMaxContextTokens renders an endpoint's declared context ceiling for the
// "model config:" block — "<empty>" for the unconstrained zero value (see
// core.Endpoint.MaxContextTokens), "<N>k" for the common round-thousands
// case (128000 -> "128k"), the raw integer otherwise (an odd, non-round
// value is rare enough not to deserve special-casing).
func fmtMaxContextTokens(n int64) string {
	if n <= 0 {
		return "<empty>"
	}
	if n%1000 == 0 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// fmtCapabilities renders an endpoint's declared capabilities for the
// "model config:" block — "<empty>" when unset (unconstrained: this
// endpoint is assumed to support everything, see core.Endpoint.HasCapability),
// else the declared list joined with "/".
func fmtCapabilities(caps []string) string {
	if len(caps) == 0 {
		return "<empty>"
	}
	return strings.Join(caps, "/")
}

// providerProxyEntry is one provider's resolved proxy setting, keyed by its
// "protocol/name" for display. BaseURL/IsProxied feed logConfigSummary's
// "provider config:" block (base_url=<url>, "(proxy)" marker only); Proxy is
// the older human-readable description (direct / direct (proxy: false) /
// redacted proxy URL) providerProxyLines still renders for `vmr check`.
type providerProxyEntry struct {
	Name      string
	BaseURL   string
	Proxy     string
	IsProxied bool
}

// providerProxyEntries resolves the proxy each provider will actually use (a
// config proxy, or direct) — the answer to "why did this provider('s
// traffic) go through the proxy" without tcpdump. Credentials inside proxy
// URLs are masked (url.Redacted). Proxy environment variables play no part:
// proxies are explicit config, and "proxy: true with nothing configured" is
// a validation error long before this renders.
func providerProxyEntries(cfg *config.Config) []providerProxyEntry {
	redact := func(raw string) string {
		if u, err := url.Parse(raw); err == nil {
			return u.Redacted()
		}
		return raw
	}
	var entries []providerProxyEntry
	for _, protocol := range core.SortedKeys(cfg.Providers) {
		for _, name := range core.SortedKeys(cfg.Providers[protocol]) {
			p := cfg.Providers[protocol][name]
			desc := "direct"
			if p.Proxy != nil && !*p.Proxy {
				desc = "direct (proxy: false)"
			}
			isProxied := false
			if mode, proxyURL := cfg.ProxySpecFor(p); mode == config.ProxyURL {
				desc = redact(proxyURL)
				isProxied = true
			}
			entries = append(entries, providerProxyEntry{
				Name: protocol + "/" + name, BaseURL: p.BaseURL, Proxy: desc, IsProxied: isProxied,
			})
		}
	}
	return entries
}

// providerProxyLines renders providerProxyEntries as flat "provider a/b
// proxy=c" lines, kept for cmdCheck's one-line-per-provider "vmr check" output.
func providerProxyLines(cfg *config.Config) []string {
	entries := providerProxyEntries(cfg)
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = fmt.Sprintf("provider %s proxy=%s", e.Name, e.Proxy)
	}
	return lines
}
