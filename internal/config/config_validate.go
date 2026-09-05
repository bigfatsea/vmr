// Ver 2026-08-30, by Sonnet 5

package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/strategy"
)

// validateBasic performs structural and top-level scalar validation: listen
// address, sticky TTL against internal memory-eviction backstop, API keys,
// redaction header names, proxies, and non-empty provider/model definitions.
func (c *Config) validateBasic() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", c.Listen, err)
	}
	if c.MaxAttempts < 0 {
		return fmt.Errorf("max_attempts must be >= 0 (got %d)", c.MaxAttempts)
	}
	if c.MaxConcurrency < 0 {
		return fmt.Errorf("max_concurrency must be >= 0 (got %d)", c.MaxConcurrency)
	}
	if c.ImageDownscaleMaxPx < 0 {
		return fmt.Errorf("image_downscale must be >= 0 (got %d; 0 = disabled)", c.ImageDownscaleMaxPx)
	}
	if c.TTL.Sticky.D() > core.StickyBackstopTTL {
		return fmt.Errorf("ttl.sticky %s exceeds the internal memory-eviction backstop (%s): a sticky entry idle longer than the backstop is dropped regardless of this setting, so stickiness would silently stop working before %s elapses — keep ttl.sticky at or under %s",
			c.TTL.Sticky.D(), core.StickyBackstopTTL, c.TTL.Sticky.D(), core.StickyBackstopTTL)
	}
	for i, k := range c.APIKeys {
		if len(k) < minAPIKeyLen {
			return fmt.Errorf("api_keys[%d]: too short (min %d characters) — its tail becomes a report label (see audit.KeyTag), so short keys would expose the whole key", i, minAPIKeyLen)
		}
	}
	for i, h := range c.ExtraRedactHeaders {
		if strings.TrimSpace(h) == "" {
			return fmt.Errorf("extra_redact_headers[%d]: empty header name", i)
		}
	}
	for _, proxy := range [...]struct{ name, val string }{
		{"http_proxy", c.HTTPProxy},
		{"https_proxy", c.HTTPSProxy},
	} {
		if proxy.val == "" {
			continue
		}
		u, err := url.Parse(proxy.val)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("invalid %s %q (want e.g. http://127.0.0.1:7890)", proxy.name, proxy.val)
		}
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("no providers defined")
	}
	if len(c.Models) == 0 {
		return fmt.Errorf("no models defined")
	}
	return nil
}

// validateIdentSegment rejects a name that would serve as the provider
// segment of the protocol:provider:model audit label (see core.EndpointLabel):
// a ':' turns SplitEndpointLabel's three-field split into a mis-parse (the
// provider tail gets folded into the model segment, corrupting every analysis-
// half grouping/detail/file name that keyed off it), and
// a '/' collides with the path-ish separators used downstream. Shared by
// provider names (validateProviders) and api_keys labels
// (expandProviderAPIKeys), since both are concatenated into that segment.
func validateIdentSegment(name string) error {
	if strings.ContainsAny(name, ":/") {
		return fmt.Errorf("name %q must not contain ':' or '/' (breaks the protocol:provider:model audit label — see core.EndpointLabel)", name)
	}
	return nil
}

// validateProviders checks provider declarations: non-empty distinct names,
// valid adapter protocols, URL credentials, proxy settings, and quota limits.
func (c *Config) validateProviders(quotaNow time.Time) error {
	seenProvider := map[string]bool{}
	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("providers[%d]: missing name", i)
		}
		if err := validateIdentSegment(p.Name); err != nil {
			return fmt.Errorf("providers[%d]: %w", i, err)
		}
		if seenProvider[p.Name] {
			return fmt.Errorf("providers[%d]: duplicate provider name %q", i, p.Name)
		}
		seenProvider[p.Name] = true
		if len(p.BaseURL) == 0 {
			return fmt.Errorf("provider %q: base_url: at least one protocol required", p.Name)
		}
		for protocol, raw := range p.BaseURL {
			if _, ok := adapter.Get(protocol); !ok {
				return fmt.Errorf("provider %q: base_url.%s: unknown adapter type (available: %v)%s", p.Name, protocol, adapter.Names(), unknownProtocolHint(protocol))
			}
			u, err := url.Parse(raw)
			if err != nil || u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("provider %q: invalid base_url.%s %q", p.Name, protocol, raw)
			}
			if err := checkBaseURLCredentials(p.Name, protocol, raw); err != nil {
				return err
			}
			if p.Proxy {
				if mode, _ := c.ProxySpecFor(p, protocol); mode != ProxyURL {
					return fmt.Errorf("provider %q: proxy: true but no matching proxy is configured for %s base_urls (set https_proxy/http_proxy; ${VAR} expansion works)", p.Name, u.Scheme)
				}
			}
		}
		if p.StickyTTL != nil {
			if p.StickyTTL.D() <= 0 {
				return fmt.Errorf("provider %q: sticky_ttl must be positive", p.Name)
			}
			if p.StickyTTL.D() > core.StickyBackstopTTL {
				return fmt.Errorf("provider %q: sticky_ttl %s exceeds the internal memory-eviction backstop (%s): a sticky entry idle longer than the backstop is dropped regardless of this setting, so stickiness would silently stop working before %s elapses — keep sticky_ttl at or under %s",
					p.Name, p.StickyTTL.D(), core.StickyBackstopTTL, p.StickyTTL.D(), core.StickyBackstopTTL)
			}
		}
		for k, v := range p.RoleMap {
			if strings.TrimSpace(k) == "" {
				return fmt.Errorf("provider %q: role_map: empty role name (from)", p.Name)
			}
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("provider %q: role_map: %q maps to an empty role name", p.Name, k)
			}
			if k == v {
				return fmt.Errorf("provider %q: role_map: %q maps to itself (a no-op rewrite)", p.Name, k)
			}
		}
		if len(p.RoleMap) == 0 && p.RoleMap != nil {
			p.RoleMap = nil
			c.Providers[i].RoleMap = nil
		}
		if err := validateQuota(p.Name, p.Quota, quotaNow); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateModelDefaults() error {
	for modelKey, entry := range c.ModelDefaults {
		if strings.TrimSpace(modelKey) == "" {
			return fmt.Errorf("model_defaults: empty model name")
		}
		if entry.MaxContextTokens < 0 {
			return fmt.Errorf("model_defaults[%q]: max_context_tokens must be >= 0", modelKey)
		}
		if entry.Providers != nil && len(entry.Providers) == 0 {
			return fmt.Errorf("model_defaults[%q]: providers must not be empty when specified", modelKey)
		}
		for j, pn := range entry.Providers {
			if strings.TrimSpace(pn) == "" {
				return fmt.Errorf("model_defaults[%q]: providers[%d]: empty", modelKey, j)
			}
			if _, ok := c.ProviderByName(pn); !ok {
				return fmt.Errorf("model_defaults[%q]: unknown provider %q", modelKey, pn)
			}
		}
	}
	return nil
}

// validateModels validates all virtual model definitions and registers
// configured (provider, model) pairs into providerModels for pricing resolution.
func (c *Config) validateModels(providerModels map[string]map[string]bool) error {
	for _, name := range fmtutil.SortedKeys(c.Models) {
		m := c.Models[name]
		if len(m.Endpoints) == 0 {
			return fmt.Errorf("model %q: no endpoints", name)
		}
		// Reject an unknown strategy dimension at load time, not later at
		// snapshot-build (strategy.Build is the single source of truth for
		// what's registered), symmetric with pricing rate resolution being
		// strict at load. A typo'd dimension name otherwise parses cleanly
		// and only fails once `vmr start` builds the routing table — a load
		// error here catches it in `vmr check`'s no-network validate path too.
		if _, err := strategy.Build(m.Strategy); err != nil {
			return fmt.Errorf("model %q: %w", name, err)
		}
		if m.MaxContextTokens < 0 {
			return fmt.Errorf("model %q: max_context_tokens must be >= 0", name)
		}
		if m.ImageDownscaleMaxPx != nil && *m.ImageDownscaleMaxPx < 0 {
			return fmt.Errorf("model %q: image_downscale must be >= 0 (got %d; 0 = force-disabled for this model)", name, *m.ImageDownscaleMaxPx)
		}
		for _, protocol := range fmtutil.SortedKeys(m.Endpoints) {
			groups := m.Endpoints[protocol]
			// Protocol lives at the map key, so one check per bucket covers
			// every group under it — the key can't drift from the entries.
			if _, ok := adapter.Get(protocol); !ok {
				return fmt.Errorf("model %q: endpoints: unknown protocol %q (available: %v)%s", name, protocol, adapter.Names(), unknownProtocolHint(protocol))
			}
			for i, eg := range groups {
				ctx := fmt.Sprintf("model %q endpoints.%s[#%d]", name, protocol, i+1)
				if err := c.validateEndpointGroup(ctx, protocol, eg, providerModels); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// validateFallbackEndpoints validates fallback endpoint definitions and ensures
// priority is explicitly set and positive.
func (c *Config) validateFallbackEndpoints(providerModels map[string]map[string]bool) error {
	for _, protocol := range fmtutil.SortedKeys(c.FallbackEndpoints) {
		groups := c.FallbackEndpoints[protocol]
		if _, ok := adapter.Get(protocol); !ok {
			return fmt.Errorf("fallback_endpoints: unknown protocol %q (available: %v)%s", protocol, adapter.Names(), unknownProtocolHint(protocol))
		}
		for i, fb := range groups {
			if fb.Priority <= 0 {
				return fmt.Errorf("fallback_endpoints.%s[#%d]: priority must be set and > 0 (an unset priority defaults to 0, which could silently outrank a model's own endpoints)", protocol, i+1)
			}
			ctx := fmt.Sprintf("fallback_endpoints.%s[#%d]", protocol, i+1)
			if err := c.validateEndpointGroup(ctx, protocol, fb, providerModels); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateEndpointGroup validates one EndpointGroup under protocol (ctx
// names its context for error messages) and records its (provider, model)
// pairs into providerModels. Shared by both models.<name>.endpoints and
// FallbackEndpoints buckets so the two can't drift on what "valid" means.
func (c *Config) validateEndpointGroup(ctx, protocol string, eg EndpointGroup, providerModels map[string]map[string]bool) error {
	if len(eg.Providers) == 0 {
		return fmt.Errorf("%s: providers: at least one required", ctx)
	}
	for _, pn := range eg.Providers {
		p, ok := c.ProviderByName(pn)
		if !ok {
			return fmt.Errorf("%s: unknown provider %q", ctx, pn)
		}
		if _, ok := p.BaseURL[protocol]; !ok {
			return fmt.Errorf("%s: provider %q has no base_url for protocol %q", ctx, pn, protocol)
		}
	}
	if len(eg.Models) == 0 {
		return fmt.Errorf("%s: models: at least one required", ctx)
	}
	for j, mn := range eg.Models {
		if mn == "" {
			return fmt.Errorf("%s: models[%d]: empty", ctx, j)
		}
		for _, pn := range eg.Providers {
			if providerModels[pn] == nil {
				providerModels[pn] = map[string]bool{}
			}
			providerModels[pn][mn] = true
		}
	}
	return nil
}
