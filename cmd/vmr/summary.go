// Ver 2026-08-02, by Sonnet 5

// Config-summary rendering shared by cmd_start.go (startup/reload banner)
// and cmd_check.go (`vmr check` output): logConfigSummary and the
// provider-proxy-entry helpers both draw on.
package main

import (
	"bytes"
	"log"
	"net/url"
	"sort"

	"vmr/internal/config"
	"vmr/internal/fmtutil"
	"vmr/internal/router"
)

// logConfigSummary prints what the running instance is actually configured
// to do, by calling the exact same section renderers `vmr check` uses
// (printGlobalSettings/printProviders/printModels) — one shared source of
// format for both, so the startup/reload log can never drift out of sync
// with `vmr check`'s output the way the two used to. issues is the caller's
// already-computed cfg.Check() result, shared with logConfigCheckIssues'
// WARN lines, so a listen/probe_timeout warning gets the same ⚠️ marker
// here as it does in `vmr check`.
//
// Each section is rendered into a buffer and emitted through one
// logger.Printf call: log.Logger writes its formatted message in exactly
// one Write, and stampWriter stamps once per Write — so the timestamp lands
// on the section's first line while the rest reads as an unstamped block.
// printGlobalSettings/printProviders/printModels never print a leading
// blank line themselves — cmdCheck inserts the separator between calls when
// it prints all three to one stdout stream, so nothing here has to peel a
// blank line back off.
func logConfigSummary(logger *log.Logger, cfg *config.Config, snap *router.Snapshot, issues []config.Issue) {
	var buf bytes.Buffer

	printGlobalSettings(&buf, cfg, issues)
	logger.Printf("%s", buf.String())

	buf.Reset()
	printProviders(&buf, cfg)
	logger.Printf("%s", buf.String())

	buf.Reset()
	printModels(&buf, cfg, snap, issues)
	logger.Printf("%s", buf.String())
}

// providerProxyEntry is one provider's resolved proxy setting, keyed by its
// "protocol/name" for display — the human-readable description (direct /
// direct (proxy: false) / redacted proxy URL) cmd_check.go's printProviders
// renders per-provider via providerProxyLine.
type providerProxyEntry struct {
	Name  string
	Proxy string
}

// redactProxyURL masks a proxy URL's embedded credentials (userinfo) for
// display — url.Redacted() replaces the password with "xxxxx", leaving the
// scheme/host/port visible. Falls back to the raw string on a parse
// failure rather than hiding a config value a human is actively debugging.
func redactProxyURL(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		return u.Redacted()
	}
	return raw
}

// providerProxyEntries resolves the proxy each provider will actually use (a
// config proxy, or direct) — the answer to "why did this provider('s
// traffic) go through the proxy" without tcpdump. Credentials inside proxy
// URLs are masked via redactProxyURL. Proxy environment variables play no
// part: proxies are explicit config, and "proxy: true with nothing
// configured" is a validation error long before this renders.
func providerProxyEntries(cfg *config.Config) []providerProxyEntry {
	var entries []providerProxyEntry
	providers := append([]config.Provider(nil), cfg.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	for _, p := range providers {
		for _, protocol := range fmtutil.SortedKeys(p.BaseURL) {
			desc := "direct"
			if mode, proxyURL := cfg.ProxySpecFor(p, protocol); mode == config.ProxyURL {
				desc = redactProxyURL(proxyURL)
			}
			entries = append(entries, providerProxyEntry{Name: protocol + "/" + p.Name, Proxy: desc})
		}
	}
	return entries
}
