// Ver 2026-08-22, by Sonnet 5

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// expandProviderAPIKeys desugars every Provider.APIKeys into that many
// independent Provider entries — named "<name>-<label>", sharing
// BaseURL/Proxy/Quota/Pricing by copy — and rewrites every reference to the
// original name (models[].endpoints' and fallback_endpoints' providers
// lists) into the expanded name list.
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
			p.KeyLabel = keyTailLabel(p.APIKey)
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
			child.KeyLabel = label
			expanded = append(expanded, child)
			names = append(names, child.Name)
		}
		rename[p.Name] = names
	}
	c.Providers = expanded

	for name, m := range c.Models {
		changed := false
		for protocol, groups := range m.Endpoints {
			bucketChanged := false
			for i, eg := range groups {
				if newProviders, ok := rewriteProviderRefs(eg.Providers, rename); ok {
					groups[i].Providers = newProviders
					bucketChanged = true
				}
			}
			if bucketChanged {
				m.Endpoints[protocol] = groups
				changed = true
			}
		}
		if changed {
			c.Models[name] = m
		}
	}
	for protocol, groups := range c.FallbackEndpoints {
		bucketChanged := false
		for i, fb := range groups {
			if newProviders, ok := rewriteProviderRefs(fb.Providers, rename); ok {
				groups[i].Providers = newProviders
				bucketChanged = true
			}
		}
		if bucketChanged {
			c.FallbackEndpoints[protocol] = groups
		}
	}
	for model, entry := range c.ModelDefaults {
		if newProviders, ok := rewriteProviderRefs(entry.Providers, rename); ok {
			entry.Providers = newProviders
			c.ModelDefaults[model] = entry
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

// keyTailLabel derives a Provider.KeyLabel for a provider written the plain
// api_key way (no api_keys labels): the key's last 6 characters. 6 (vs the
// client-side audit.KeyTag's 8-char window) is deliberate — a different
// concept with a different derivation; see the LiveStats design doc's
// decision table, do not unify them.
func keyTailLabel(key string) string {
	if len(key) <= 6 {
		// A key this short has no safe 6-character tail to reveal — the
		// "tail" would be the whole key, and this label lands verbatim in
		// logs, audit records, and the console. Hash it instead: still
		// distinct per key (so two different short keys don't collide into
		// one label), never reversible back to the plaintext.
		sum := sha256.Sum256([]byte(key))
		return "#" + hex.EncodeToString(sum[:2])
	}
	return key[len(key)-6:]
}
