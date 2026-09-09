// Ver 2026-07-30, by Sonnet 5

// Snapshot construction and installation: turning a validated config.Config
// into the immutable, atomically-swappable routing table Serve reads. Split
// out of router.go — pure move, no behavior change.
package router

import (
	"fmt"
	"net/http"
	"time"

	"vmr/internal/adapter"
	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/strategy"
)

// ModelRoute is the runtime routing table entry for one virtual model. It
// carries no protocol field: a route only ever exists inside
// Snapshot.Models[protocol], so the protocol is positional, not stored data —
// there is no "protocol" value here that could disagree with where the route
// lives.
type ModelRoute struct {
	Dims      []strategy.Dimension
	Endpoints []*core.Endpoint

	// ImageDownscaleMaxPx mirrors config.ModelConfig.ImageDownscaleMaxPx: nil
	// = this model has no override and inherits the global image_downscale;
	// non-nil (including a pointer to 0) = this model's explicit setting,
	// which always wins over the global one.
	ImageDownscaleMaxPx *int

	// Sticky mirrors config.ModelConfig.Sticky, resolved at BuildSnapshot
	// time: nil (field absent in config) defaults to true, so Sticky Model
	// affinity applies unless a virtual model explicitly opts out. See
	// docs/VirtualModelRouter_Design_v4_Core.md's Sticky Model section.
	Sticky bool
}

// EffectiveOrder returns route's endpoints in the order they would actually
// be tried — health ignored, a static preview only — by copying and running
// the same strategy.Sort every real request goes through (see Serve).
// Shared by every command that previews routing (vmr start's startup log,
// vmr check, vmr diagnose) so they can't silently disagree about try-order
// for the same config: each held its own copy of "append then sort" before
// this was factored out.
func (r *ModelRoute) EffectiveOrder() []*core.Endpoint {
	ordered := append([]*core.Endpoint(nil), r.Endpoints...)
	strategy.Sort(ordered, r.Dims)
	return ordered
}

// EffectiveImageDownscaleMaxPx resolves the image-downscale cap that
// actually applies to this model: its own override if set (even 0, which
// force-disables downscaling for this model regardless of the global
// setting), else globalMaxPx. Safe to call on a nil receiver (an unknown
// model whose route lookup failed) — callers don't need a separate nil
// check before falling back to the global setting.
func (r *ModelRoute) EffectiveImageDownscaleMaxPx(globalMaxPx int) int {
	if r != nil && r.ImageDownscaleMaxPx != nil {
		return *r.ImageDownscaleMaxPx
	}
	return globalMaxPx
}

// Snapshot is an immutable view of the config; hot reload swaps the whole
// thing atomically, so in-flight requests keep the version they started with.
// Models is keyed protocol -> name: BuildSnapshot splits each
// config.VirtualModel's endpoint groups by their own declared protocol, so
// this shape is derived, not a direct mirror of config.Config.Models (which
// is keyed by virtual-model name alone; the protocol lives as the map key of
// VirtualModel.Endpoints — see config.VirtualModel).
type Snapshot struct {
	Cfg    *config.Config
	Models map[string]map[string]*ModelRoute

	// clients maps "<protocol>/<provider>" to the http.Client serving that
	// provider. Built in Install (travels with the snapshot to avoid races);
	// providers with the same effective proxy resolution (see config.ProxySpecFor)
	// share one client, so connection pooling stays per proxy group —
	// typically one or two clients per snapshot. clientSet is the distinct
	// set, kept for closing idle connections when the snapshot is replaced.
	clients   map[string]*http.Client
	clientSet []*http.Client
}

// clientFor returns the http.Client that carries this endpoint's provider.
// Coverage is guaranteed by construction: BuildSnapshot resolves endpoints
// from the same Cfg.Providers list Install builds clients from.
func (s *Snapshot) clientFor(ep *core.Endpoint) *http.Client {
	return s.clients[ep.AdapterType+"/"+ep.Provider]
}

// BuildSnapshot resolves provider references into concrete endpoints. A
// virtual model's Endpoints are already bucketed by protocol key; each key
// with at least one group becomes its own *ModelRoute — the same virtual
// model name can be reachable from both ingress protocols at once (see
// config.VirtualModel's doc comment), each independently, sharing the
// model-level Dims/Sticky/ImageDownscaleMaxPx but never each other's
// endpoints. Each EndpointGroup's Models list expands into that many
// independent *core.Endpoint values, in list order.
func BuildSnapshot(cfg *config.Config) (*Snapshot, error) {
	snap := &Snapshot{Cfg: cfg, Models: map[string]map[string]*ModelRoute{}}
	// Single filtering point for Provider.Disabled — every downstream
	// consumer (buildEndpoints' (provider,model) expansion, fallback
	// injection, BuildQuotaSpecsDisabled) keys off this map. Don't scatter
	// p.Disabled checks at each consumer: hot-reload sequences would then
	// need every consumer updated in lockstep or routes would silently
	// drift (BuildQuotaSpecs would skip a disabled provider but
	// buildEndpoints would still emit its endpoints, etc.). One map, one
	// filter. Everything downstream follows for free: /status lists only
	// endpoints a snapshot can actually route to; hot reload needs no new
	// mechanism (snapshot swaps atomically; sticky re-checks candidates
	// per request, so pinned sessions just fail over past a disabled
	// provider); quota registry Prune drops the stranded bucket on install.
	enabled := enabledProviders(cfg.Providers)
	quotaSpecs := BuildQuotaSpecsDisabled(cfg.Providers, enabled)
	for name, m := range cfg.Models {
		dims, err := strategy.Build(m.Strategy)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", name, err)
		}
		sticky := m.Sticky == nil || *m.Sticky
		fallbackOK := m.Fallback == nil || *m.Fallback
		routes := map[string]*ModelRoute{} // protocol -> this model's route for that protocol
		for protocol, groups := range m.Endpoints {
			for _, eg := range groups {
				eps, err := buildEndpoints(cfg, quotaSpecs, enabled, m, eg, protocol, cfg.TTL.Sticky.D(), false)
				if err != nil {
					return nil, fmt.Errorf("model %q: %w", name, err)
				}
				route, ok := routes[protocol]
				if !ok {
					route = &ModelRoute{Dims: dims, ImageDownscaleMaxPx: m.ImageDownscaleMaxPx, Sticky: sticky}
					routes[protocol] = route
				}
				route.Endpoints = append(route.Endpoints, eps...)
			}
		}
		// Only attaches to protocols this model already routes — a fallback
		// augments, it never opens a new ingress. Direct per-protocol lookup
		// into FallbackEndpoints (no scan): the config is already keyed the
		// way this loop consumes it.
		if fallbackOK {
			for protocol, groups := range cfg.FallbackEndpoints {
				route, ok := routes[protocol]
				if !ok {
					continue
				}
				for i, fb := range groups {
					eps, err := buildEndpoints(cfg, quotaSpecs, enabled, m, fb, protocol, cfg.TTL.Sticky.D(), true)
					if err != nil {
						return nil, fmt.Errorf("model %q: fallback_endpoints.%s[#%d]: %w", name, protocol, i+1, err)
					}
					route.Endpoints = append(route.Endpoints, eps...)
				}
			}
		}
		for protocol, route := range routes {
			byName, ok := snap.Models[protocol]
			if !ok {
				byName = map[string]*ModelRoute{}
				snap.Models[protocol] = byName
			}
			byName[name] = route
		}
	}
	return snap, nil
}

// buildEndpoints expands one EndpointGroup (fromFallback marks whether it's
// a FallbackEndpoints entry; protocol is the map key the group lives under)
// into its *core.Endpoint values — outer loop over Models, inner loop over
// eg.Providers. Providers absent from `enabled` (Provider.Disabled = true)
// are silently skipped — that's the stated intent of the switch ("carries no
// traffic until re-enabled"), and the check warning is what makes it
// visible. Each returned Endpoint is already Freeze()'d.
func buildEndpoints(cfg *config.Config, quotaSpecs map[string]*core.QuotaSpec, enabled map[string]bool, m config.VirtualModel, eg config.EndpointGroup, protocol string, globalStickyTTL time.Duration, fromFallback bool) ([]*core.Endpoint, error) {
	ad, ok := adapter.Get(protocol)
	if !ok { // defensive; config.validate already checked this
		return nil, fmt.Errorf("unknown adapter type %q (available: %v)", protocol, adapter.Names())
	}
	var eps []*core.Endpoint
	for _, upstreamModel := range eg.Models {
		for _, providerName := range eg.Providers {
			if !enabled[providerName] {
				continue // Provider.Disabled — see enabledProviders below
			}
			p, ok := cfg.ProviderByName(providerName)
			if !ok { // defensive; config.validate already checked this
				return nil, fmt.Errorf("unknown provider %q", providerName)
			}
			// Sticky validity is a per-provider property: the endpoint
			// inherits its provider's sticky_ttl, falling back to the
			// global ttl.sticky default.
			stickyTTL := globalStickyTTL
			if p.StickyTTL != nil {
				stickyTTL = p.StickyTTL.D()
			}
			baseURL, ok := p.BaseURL[protocol]
			if !ok { // defensive; config.validate already checked this
				return nil, fmt.Errorf("provider %q has no base_url for protocol %q", providerName, protocol)
			}
			effCapabilities := resolveModelCapabilities(m, cfg.ModelDefaults, providerName, upstreamModel)
			effMaxContextTokens := resolveModelMaxContextTokens(m, cfg.ModelDefaults, providerName, upstreamModel)
			ep := &core.Endpoint{
				Provider:         providerName,
				AdapterType:      protocol,
				BaseURL:          baseURL,
				FullURL:          ad.ResolveURL(baseURL),
				APIKey:           p.APIKey,
				KeyLabel:         p.KeyLabel,
				Model:            upstreamModel,
				Priority:         eg.Priority,
				RoleMap:          p.RoleMap,
				Capabilities:     effCapabilities,
				MaxContextTokens: effMaxContextTokens,
				FromFallback:     fromFallback,
				StickyTTL:        stickyTTL,
				Quota:            quotaSpecs[providerName],
			}
			// Precompute HealthKey()/Name() once, here, before ep is
			// ever reachable from a concurrently-read Snapshot (see
			// core.Endpoint.Freeze's doc comment) — every later call on
			// the request hot path becomes a plain field read instead
			// of re-hashing APIKey with SHA-256.
			ep.Freeze()
			eps = append(eps, ep)
		}
	}
	return eps, nil
}

// BuildQuotaSpecs converts each provider's config.QuotaConfig into a
// core.QuotaSpec once, keyed by provider name, so every core.Endpoint
// expanded from that provider (however many virtual models/protocols
// reference it) shares the SAME pointer — quota is an account property, not
// a per-endpoint one (see core.Endpoint.Quota's doc comment). Providers
// with no quota: configured are simply absent from the map, so a lookup
// miss below naturally yields nil (unmetered). Exported: internal/replay
// needs the same provider-name -> *core.QuotaSpec resolution BuildSnapshot
// does, for a hand-built core.Endpoint that never goes through BuildSnapshot
// itself (see replay.go's chargeReplay).
func BuildQuotaSpecs(providers []config.Provider) map[string]*core.QuotaSpec {
	out := map[string]*core.QuotaSpec{}
	for _, p := range providers {
		if p.Quota == nil {
			continue
		}
		limits := make([]core.Limit, len(p.Quota.Limits))
		for i, lc := range p.Quota.Limits {
			limits[i] = lc.Resolved
		}
		out[p.Name] = &core.QuotaSpec{Limits: limits}
	}
	return out
}

// BuildQuotaSpecsDisabled is BuildQuotaSpecs plus the Provider.Disabled
// filter — the BuildSnapshot-side companion, since BuildSnapshot must NOT
// leave a stranded counter bucket for an account carrying no traffic.
// Internal/replay is unchanged: it deliberately builds a core.Endpoint for
// a specific (provider,model) pair and reuses BuildQuotaSpecs' shape.
func BuildQuotaSpecsDisabled(providers []config.Provider, enabled map[string]bool) map[string]*core.QuotaSpec {
	out := map[string]*core.QuotaSpec{}
	for _, p := range providers {
		if !enabled[p.Name] {
			continue
		}
		if p.Quota == nil {
			continue
		}
		limits := make([]core.Limit, len(p.Quota.Limits))
		for i, lc := range p.Quota.Limits {
			limits[i] = lc.Resolved
		}
		out[p.Name] = &core.QuotaSpec{Limits: limits}
	}
	return out
}

// enabledProviders returns the set of provider names that should be live
// in the routing half — the inverse of Provider.Disabled. Single source
// of truth used by BuildSnapshot (route expansion, fallback injection)
// and BuildQuotaSpecsDisabled, so reload sequences can never drift (one
// consumer filtering and another not). All providers pass when none
// declares it (the common case today).
func enabledProviders(providers []config.Provider) map[string]bool {
	out := make(map[string]bool, len(providers))
	for _, p := range providers {
		if !p.Disabled {
			out[p.Name] = true
		}
	}
	return out
}

// resolveModelCapabilities resolves capabilities for a (provider, model) under
// virtual model m by consulting:
// 1. Virtual model explicit override
// 2. model_defaults[model] exact match (if provider matches and non-empty)
// 3. model_defaults["*"] wildcard fallback (if provider matches and non-empty)
// 4. nil (unconstrained)
func resolveModelCapabilities(m config.VirtualModel, defaults map[string]config.ModelDefaultEntry, provider, model string) []string {
	if len(m.Capabilities) > 0 {
		return m.Capabilities
	}
	if entry, ok := defaults[model]; ok && providerMatches(entry.Providers, provider) && len(entry.Capabilities) > 0 {
		return entry.Capabilities
	}
	if wildcard, ok := defaults["*"]; ok && providerMatches(wildcard.Providers, provider) && len(wildcard.Capabilities) > 0 {
		return wildcard.Capabilities
	}
	return nil
}

// resolveModelMaxContextTokens resolves context window ceiling for a (provider, model)
// under virtual model m by consulting:
// 1. Virtual model explicit override (> 0)
// 2. model_defaults[model] exact match (if provider matches and > 0)
// 3. model_defaults["*"] wildcard fallback (if provider matches and > 0)
// 4. 0 (unconstrained)
func resolveModelMaxContextTokens(m config.VirtualModel, defaults map[string]config.ModelDefaultEntry, provider, model string) int64 {
	if m.MaxContextTokens > 0 {
		return m.MaxContextTokens
	}
	if entry, ok := defaults[model]; ok && providerMatches(entry.Providers, provider) && entry.MaxContextTokens > 0 {
		return entry.MaxContextTokens
	}
	if wildcard, ok := defaults["*"]; ok && providerMatches(wildcard.Providers, provider) && wildcard.MaxContextTokens > 0 {
		return wildcard.MaxContextTokens
	}
	return 0
}

func providerMatches(allowed []string, provider string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, p := range allowed {
		if p == provider {
			return true
		}
	}
	return false
}

// Install atomically swaps in a new snapshot; in-flight requests keep the old one.
// One http.Client is built per distinct proxy resolution (direct, or a config
// proxy URL) and shared by every provider that resolves the same way — the
// per-provider proxy switch never costs a per-request check.
//
// Atomicity is self-contained: installMu serializes the installLimiter +
// snapshot swap + quota prune sequence so a concurrent Install (a second hot
// reload racing the first, or any future admin caller) can never interleave
// the three steps — a caller must not need its own external lock to call
// Install safely.
func (rt *Router) Install(s *Snapshot) {
	rt.installMu.Lock()
	defer rt.installMu.Unlock()
	byResolution := map[string]*http.Client{}
	s.clients = map[string]*http.Client{}
	for _, p := range s.Cfg.Providers {
		for protocol := range p.BaseURL {
			mode, proxyURL := s.Cfg.ProxySpecFor(p, protocol)
			key := mode + "|" + proxyURL
			c, ok := byResolution[key]
			if !ok {
				c = NewUpstreamClient(s.Cfg, p, protocol)
				byResolution[key] = c
				s.clientSet = append(s.clientSet, c)
			}
			s.clients[protocol+"/"+p.Name] = c
		}
	}
	rt.installLimiter(s.Cfg.MaxConcurrency)
	old := rt.snap.Swap(s)
	// Prune AFTER the swap, not before: once the new snapshot is live every
	// new Charge keys buckets by the new config, so Prune removes exactly
	// the stale keys. Pruning first left a window where an in-flight request
	// on the old snapshot could rebuild a bucket that was just pruned; a
	// straggler that still does so after the swap is cleaned by the next
	// reload's Prune (B7).
	if rt.Quota != nil {
		rt.Quota.Prune(s.ProviderLimits())
	}
	if rt.Health != nil {
		rt.Health.Prune(s.HealthKeys())
	}
	if old != nil {
		// Release the previous pools' idle connections now instead of
		// waiting for GC. In-flight requests still holding the old
		// snapshot are unaffected — their connections are active.
		for _, c := range old.clientSet {
			c.CloseIdleConnections()
		}
	}
}

// ProviderLimits returns a map of provider name -> configured Limits from this snapshot.
// HealthKeys is the set of endpoint health keys this snapshot can route to —
// the "keep" set for health.Registry.Prune, so an endpoint dropped from the
// config stops carrying its failure state (and its /status row) across a hot
// reload. Same reason ProviderLimits exists for the quota registry.
func (s *Snapshot) HealthKeys() map[string]bool {
	if s == nil {
		return nil
	}
	keep := map[string]bool{}
	for _, byName := range s.Models {
		for _, route := range byName {
			for _, ep := range route.Endpoints {
				keep[ep.HealthKey()] = true
			}
		}
	}
	return keep
}

func (s *Snapshot) ProviderLimits() map[string][]core.Limit {
	if s == nil || s.Cfg == nil {
		return nil
	}
	out := make(map[string][]core.Limit, len(s.Cfg.Providers))
	for _, p := range s.Cfg.Providers {
		if p.Quota != nil && len(p.Quota.Limits) > 0 {
			limits := make([]core.Limit, len(p.Quota.Limits))
			for i, lc := range p.Quota.Limits {
				limits[i] = lc.Resolved
			}
			out[p.Name] = limits
		}
	}
	return out
}

func (rt *Router) Snapshot() *Snapshot { return rt.snap.Load() }
