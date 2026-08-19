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
// is keyed by virtual-model name alone — see config.VirtualModel).
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
// virtual model's config.EndpointGroups are grouped by their own declared
// Protocol into one *ModelRoute per protocol actually present — the same
// virtual model name can be reachable from both ingress protocols at once
// (see config.VirtualModel's doc comment), each independently, sharing the
// model-level Dims/Sticky/ImageDownscaleMaxPx but never each other's
// endpoints. Each EndpointGroup's Models list expands into that many
// independent *core.Endpoint values, in list order.
func BuildSnapshot(cfg *config.Config) (*Snapshot, error) {
	snap := &Snapshot{Cfg: cfg, Models: map[string]map[string]*ModelRoute{}}
	quotaSpecs := BuildQuotaSpecs(cfg.Providers)
	for name, m := range cfg.Models {
		dims, err := strategy.Build(m.Strategy)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", name, err)
		}
		sticky := m.Sticky == nil || *m.Sticky
		fallbackOK := m.Fallback == nil || *m.Fallback
		routes := map[string]*ModelRoute{} // protocol -> this model's route for that protocol
		for _, eg := range m.Endpoints {
			eps, err := buildEndpoints(cfg, quotaSpecs, m, eg, cfg.StickyTTL.D(), false)
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", name, err)
			}
			route, ok := routes[eg.Protocol]
			if !ok {
				route = &ModelRoute{Dims: dims, ImageDownscaleMaxPx: m.ImageDownscaleMaxPx, Sticky: sticky}
				routes[eg.Protocol] = route
			}
			route.Endpoints = append(route.Endpoints, eps...)
		}
		// Only attaches to protocols this model already routes — a fallback
		// augments, it never opens a new ingress.
		if fallbackOK {
			for i, fb := range cfg.FallbackEndpoints {
				route, ok := routes[fb.Protocol]
				if !ok {
					continue
				}
				eps, err := buildEndpoints(cfg, quotaSpecs, m, fb, cfg.StickyTTL.D(), true)
				if err != nil {
					return nil, fmt.Errorf("model %q: fallback_endpoints[%d]: %w", name, i, err)
				}
				route.Endpoints = append(route.Endpoints, eps...)
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
// a FallbackEndpoints entry) into its *core.Endpoint values — outer loop
// over Models, inner loop over eg.Providers. Each returned Endpoint is
// already Freeze()'d.
func buildEndpoints(cfg *config.Config, quotaSpecs map[string]*core.QuotaSpec, m config.VirtualModel, eg config.EndpointGroup, globalStickyTTL time.Duration, fromFallback bool) ([]*core.Endpoint, error) {
	ad, ok := adapter.Get(eg.Protocol)
	if !ok { // defensive; config.validate already checked this
		return nil, fmt.Errorf("unknown adapter type %q (available: %v)", eg.Protocol, adapter.Names())
	}
	stickyTTL := globalStickyTTL
	if eg.StickyTTL != nil {
		stickyTTL = eg.StickyTTL.D()
	}
	// Capabilities: union of the virtual model's base list and this
	// endpoint's own (additive) declaration. MaxContextTokens: this
	// endpoint's own override if set, else the model's base — a
	// scalar can't be unioned, so it's override-or-inherit instead.
	// See config.VirtualModel/config.EndpointGroup's doc comments.
	effCapabilities := mergeCapabilities(m.Capabilities, eg.Capabilities)
	effMaxContextTokens := m.MaxContextTokens
	if eg.MaxContextTokens > 0 {
		effMaxContextTokens = eg.MaxContextTokens
	}
	var eps []*core.Endpoint
	for _, upstreamModel := range eg.Models {
		for _, providerName := range eg.Providers {
			p, ok := cfg.ProviderByName(providerName)
			if !ok { // defensive; config.validate already checked this
				return nil, fmt.Errorf("unknown provider %q", providerName)
			}
			baseURL, ok := p.BaseURL[eg.Protocol]
			if !ok { // defensive; config.validate already checked this
				return nil, fmt.Errorf("provider %q has no base_url for protocol %q", providerName, eg.Protocol)
			}
			ep := &core.Endpoint{
				Provider:            providerName,
				AdapterType:         eg.Protocol,
				BaseURL:             baseURL,
				FullURL:             ad.ResolveURL(baseURL),
				APIKey:              p.APIKey,
				Model:               upstreamModel,
				Priority:            eg.Priority,
				RoleMap:             eg.RoleMap,
				Capabilities:        effCapabilities,
				ExtraCapabilities:   eg.Capabilities,
				MaxContextTokens:    effMaxContextTokens,
				OwnMaxContextTokens: eg.MaxContextTokens,
				FromFallback:        fromFallback,
				StickyTTL:           stickyTTL,
				Quota:               quotaSpecs[providerName],
				PricingRate:         cfg.ResolvedPricing[providerName+"\x00"+upstreamModel],
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
		out[p.Name] = &core.QuotaSpec{
			Limits:           limits,
			TokenWeights:     p.Quota.ResolvedTokenWeights,
			ModelMultipliers: p.Quota.ModelMultipliers,
		}
	}
	return out
}

// mergeCapabilities unions a virtual model's base capabilities with one
// endpoint's own (additive) declaration, base entries first, deduplicated —
// nil when both are empty so an endpoint declaring neither stays
// unconstrained exactly as before these fields existed (see
// core.Endpoint.Capabilities).
func mergeCapabilities(base, extra []string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(base)+len(extra))
	merged := make([]string, 0, len(base)+len(extra))
	for _, lists := range [][]string{base, extra} {
		for _, c := range lists {
			if !seen[c] {
				seen[c] = true
				merged = append(merged, c)
			}
		}
	}
	return merged
}

// Install atomically swaps in a new snapshot; in-flight requests keep the old one.
// One http.Client is built per distinct proxy resolution (direct, or a config
// proxy URL) and shared by every provider that resolves the same way — the
// per-provider proxy switch never costs a per-request check.
func (rt *Router) Install(s *Snapshot) {
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
	if old := rt.snap.Swap(s); old != nil {
		// Release the previous pools' idle connections now instead of
		// waiting for GC. In-flight requests still holding the old
		// snapshot are unaffected — their connections are active.
		for _, c := range old.clientSet {
			c.CloseIdleConnections()
		}
	}
}

func (rt *Router) Snapshot() *Snapshot { return rt.snap.Load() }
