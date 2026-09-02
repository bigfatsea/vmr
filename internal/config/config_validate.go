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
)

// validateBasic performs structural and top-level scalar validation: listen
// address, sticky TTL against internal memory-eviction backstop, API keys,
// redaction header names, proxies, and non-empty provider/model definitions.
func (c *Config) validateBasic() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("invalid listen address %q: %w", c.Listen, err)
	}
	if c.StickyTTL.D() > core.StickyBackstopTTL {
		return fmt.Errorf("sticky_ttl %s exceeds the internal memory-eviction backstop (%s): a sticky entry idle longer than the backstop is dropped regardless of this setting, so stickiness would silently stop working before %s elapses — keep sticky_ttl at or under %s",
			c.StickyTTL.D(), core.StickyBackstopTTL, c.StickyTTL.D(), core.StickyBackstopTTL)
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

// validateProviders checks provider declarations: non-empty distinct names,
// valid adapter protocols, URL credentials, proxy settings, and quota limits.
func (c *Config) validateProviders(quotaNow time.Time) error {
	seenProvider := map[string]bool{}
	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("providers[%d]: missing name", i)
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
		if err := validateQuota(p.Name, p.Quota, quotaNow); err != nil {
			return err
		}
	}
	return nil
}

// validateModels validates all virtual model definitions and registers
// configured (provider, model) pairs into providerModels for pricing resolution.
func (c *Config) validateModels(providerModels map[string]map[string]bool) error {
	for name, m := range c.Models {
		if len(m.Endpoints) == 0 {
			return fmt.Errorf("model %q: no endpoints", name)
		}
		if m.MaxContextTokens < 0 {
			return fmt.Errorf("model %q: max_context_tokens must be >= 0", name)
		}
		for i, eg := range m.Endpoints {
			if err := c.validateEndpointGroup(fmt.Sprintf("model %q endpoint group #%d", name, i+1), eg, providerModels); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateFallbackEndpoints validates fallback endpoint definitions and ensures
// priority is explicitly set and positive.
func (c *Config) validateFallbackEndpoints(providerModels map[string]map[string]bool) error {
	for i, fb := range c.FallbackEndpoints {
		if fb.Priority <= 0 {
			return fmt.Errorf("fallback_endpoints[%d]: priority must be set and > 0 (an unset priority defaults to 0, which could silently outrank a model's own endpoints)", i)
		}
		if err := c.validateEndpointGroup(fmt.Sprintf("fallback_endpoints[%d]", i), fb, providerModels); err != nil {
			return err
		}
	}
	return nil
}

// validateEndpointGroup validates one EndpointGroup (ctx names its context
// for error messages) and records its (provider, model) pairs into
// providerModels. Shared by both models.<name>.endpoints[] and
// FallbackEndpoints so the two can't drift on what "valid" means.
func (c *Config) validateEndpointGroup(ctx string, eg EndpointGroup, providerModels map[string]map[string]bool) error {
	if _, ok := adapter.Get(eg.Protocol); !ok {
		return fmt.Errorf("%s: unknown protocol %q (available: %v)%s", ctx, eg.Protocol, adapter.Names(), unknownProtocolHint(eg.Protocol))
	}
	if len(eg.Providers) == 0 {
		return fmt.Errorf("%s: providers: at least one required", ctx)
	}
	for _, pn := range eg.Providers {
		p, ok := c.ProviderByName(pn)
		if !ok {
			return fmt.Errorf("%s: unknown provider %q", ctx, pn)
		}
		if _, ok := p.BaseURL[eg.Protocol]; !ok {
			return fmt.Errorf("%s: provider %q has no base_url for protocol %q", ctx, pn, eg.Protocol)
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
	if eg.MaxContextTokens < 0 {
		return fmt.Errorf("%s: max_context_tokens must be >= 0", ctx)
	}
	if eg.StickyTTL != nil {
		if eg.StickyTTL.D() <= 0 {
			return fmt.Errorf("%s: sticky_ttl must be positive", ctx)
		}
		if eg.StickyTTL.D() > core.StickyBackstopTTL {
			return fmt.Errorf("%s: sticky_ttl %s exceeds the internal memory-eviction backstop (%s): a sticky entry idle longer than the backstop is dropped regardless of this setting, so stickiness would silently stop working before %s elapses — keep sticky_ttl at or under %s",
				ctx, eg.StickyTTL.D(), core.StickyBackstopTTL, eg.StickyTTL.D(), core.StickyBackstopTTL)
		}
	}
	return nil
}
