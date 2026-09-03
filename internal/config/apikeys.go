// Ver 2026-08-22, by Sonnet 5

package config

import "fmt"

// expandProviderAPIKeys desugars every Provider.APIKeys into that many
// independent Provider entries — named "<name>-<label>", sharing
// BaseURL/Proxy/Quota/Pricing by copy — and rewrites every reference to the
// original name (models[].endpoints[].providers, fallback_endpoints[].
// providers) into the expanded name list.
//
// Runs in Parse right after YAML decode, before applyDefaults/validate —
// everything downstream (quota, pricing, health, sticky, audit, vmr check/
// diagnose) resolves providers by name from Config.Providers and has no
// idea an entry was hand-written vs. expanded. In particular
// BuildQuotaSpecs keys by provider name, so each expanded entry — carrying
// its own Name but the same *QuotaConfig pointer as its siblings — gets its
// own independent core.QuotaSpec/counter bucket for free; there is no
// shared-ledger case to reconcile. See docs/KNOWN_ISSUES.md's
// ProviderGroup entry for why this config-time-expansion shape (vs. a
// runtime key pool) is the one that doesn't fight core.Endpoint's
// construct-once/HealthKey-once invariant.
//
// Expansion order follows Go's map iteration over Provider.APIKeys, which
// is randomized per process — deliberately not pinned to declaration order.
// Priority ties (the common no-quota case) resolve to file order, so this
// means "which key goes first" can differ across restarts. That's an
// accepted simplification, not an oversight: pinning it would mean parsing
// api_keys as an ordered structure instead of a plain map, for a guarantee
// nothing downstream actually needs — quota-configured keys are scored by
// headroom regardless of order, and an unlucky first pick just fails over
// to the next candidate like any other unhealthy endpoint. The resolved
// order is never a mystery either way: `vmr check`/the startup log print
// the actual expanded provider list and effective try-order every time.
func (c *Config) expandProviderAPIKeys() error {
	rename := map[string][]string{} // original provider name -> expanded names
	expanded := make([]Provider, 0, len(c.Providers))
	for _, p := range c.Providers {
		if len(p.APIKeys) == 0 {
			expanded = append(expanded, p)
			continue
		}
		if p.APIKey != "" {
			return fmt.Errorf("provider %q: set either api_key or api_keys, not both", p.Name)
		}
		names := make([]string, 0, len(p.APIKeys))
		for label, key := range p.APIKeys {
			if label == "" {
				return fmt.Errorf("provider %q: api_keys: label must not be empty", p.Name)
			}
			if err := validateIdentSegment(label); err != nil {
				return fmt.Errorf("provider %q: api_keys: label %w", p.Name, err)
			}
			child := p
			child.Name = p.Name + "-" + label
			child.APIKey = key
			child.APIKeys = nil
			expanded = append(expanded, child)
			names = append(names, child.Name)
		}
		rename[p.Name] = names
	}
	c.Providers = expanded

	for name, m := range c.Models {
		changed := false
		for i, eg := range m.Endpoints {
			if newProviders, ok := rewriteProviderRefs(eg.Providers, rename); ok {
				m.Endpoints[i].Providers = newProviders
				changed = true
			}
		}
		if changed {
			c.Models[name] = m
		}
	}
	for i, fb := range c.FallbackEndpoints {
		if newProviders, ok := rewriteProviderRefs(fb.Providers, rename); ok {
			c.FallbackEndpoints[i].Providers = newProviders
		}
	}
	return nil
}

// rewriteProviderRefs replaces any name in refs that was expanded (per
// rename) with its expanded name list, preserving the position and order of
// unexpanded names. ok reports whether anything actually changed, so
// callers can skip a needless map write.
func rewriteProviderRefs(refs []string, rename map[string][]string) ([]string, bool) {
	changed := false
	for _, r := range refs {
		if _, ok := rename[r]; ok {
			changed = true
			break
		}
	}
	if !changed {
		return refs, false
	}
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		if names, ok := rename[r]; ok {
			out = append(out, names...)
		} else {
			out = append(out, r)
		}
	}
	return out, true
}
