// Ver 2026-07-26, by Sonnet 5

// Snapshot construction and installation: turning a validated config.Config
// into the immutable, atomically-swappable routing table Serve reads. Split
// out of router.go — pure move, no behavior change.
package router

import (
	"fmt"
	"net/http"

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
	// which always wins over the global one (§7 image downscale).
	ImageDownscaleMaxPx *int

	// Sticky mirrors config.ModelConfig.Sticky, resolved at BuildSnapshot
	// time: nil (field absent in config) defaults to true, so Sticky Model
	// affinity applies unless a virtual model explicitly opts out. See
	// docs/VirtualModelRouter_System_Design_v3.md §6.5.
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
// Models is keyed protocol -> name, mirroring config.Config.Models.
type Snapshot struct {
	Cfg    *config.Config
	Models map[string]map[string]*ModelRoute

	// clients maps "<protocol>/<provider>" to the http.Client serving that
	// provider. Built in Install (travels with the snapshot to avoid races);
	// providers with the same effective proxy resolution (§config.ProxySpecFor)
	// share one client, so connection pooling stays per proxy group —
	// typically one or two clients per snapshot. clientSet is the distinct
	// set, kept for closing idle connections when the snapshot is replaced.
	clients   map[string]*http.Client
	clientSet []*http.Client
}

// clientFor returns the http.Client that carries this endpoint's provider.
// Coverage is guaranteed by construction: BuildSnapshot resolves endpoints
// from the same Cfg.Providers map Install builds clients from.
func (s *Snapshot) clientFor(ep *core.Endpoint) *http.Client {
	return s.clients[ep.AdapterType+"/"+ep.Provider]
}

// BuildSnapshot resolves provider references into concrete endpoints. Because
// an endpoint's provider is looked up within its own model's protocol group
// (cfg.Providers[protocol]), every endpoint of a model is guaranteed to share
// one adapter/protocol by construction — there is no "mixed protocol" case
// left to detect.
func BuildSnapshot(cfg *config.Config) (*Snapshot, error) {
	snap := &Snapshot{Cfg: cfg, Models: map[string]map[string]*ModelRoute{}}
	for protocol, models := range cfg.Models {
		ad, ok := adapter.Get(protocol)
		if !ok { // defensive; config.validate already checked this
			return nil, fmt.Errorf("protocol %q: unknown adapter type (available: %v)", protocol, adapter.Names())
		}
		byName := make(map[string]*ModelRoute, len(models))
		for name, m := range models {
			dims, err := strategy.Build(m.Strategy)
			if err != nil {
				return nil, fmt.Errorf("model %q: %w", name, err)
			}
			route := &ModelRoute{Dims: dims, ImageDownscaleMaxPx: m.ImageDownscaleMaxPx, Sticky: m.Sticky == nil || *m.Sticky}
			for _, ec := range m.Endpoints {
				p, ok := cfg.Providers[protocol][ec.Provider]
				if !ok { // defensive; config.validate already checked this
					return nil, fmt.Errorf("model %q: unknown provider %q in the %s protocol group", name, ec.Provider, protocol)
				}
				stickyTTL := cfg.StickyTTL.D()
				if ec.StickyTTL != nil {
					stickyTTL = ec.StickyTTL.D()
				}
				ep := &core.Endpoint{
					Provider:         ec.Provider,
					AdapterType:      protocol,
					BaseURL:          p.BaseURL,
					FullURL:          ad.ResolveURL(p.BaseURL),
					APIKey:           p.APIKey,
					Model:            ec.Model,
					Priority:         ec.Priority,
					RoleMap:          p.RoleMap,
					Capabilities:     ec.Capabilities,
					MaxContextTokens: ec.MaxContextTokens,
					StickyTTL:        stickyTTL,
				}
				// Precompute HealthKey()/Name() once, here, before ep is
				// ever reachable from a concurrently-read Snapshot (see
				// core.Endpoint.Freeze's doc comment) — every later call on
				// the request hot path becomes a plain field read instead
				// of re-hashing APIKey with SHA-256.
				ep.Freeze()
				route.Endpoints = append(route.Endpoints, ep)
			}
			byName[name] = route
		}
		snap.Models[protocol] = byName
	}
	return snap, nil
}

// Install atomically swaps in a new snapshot; in-flight requests keep the old one.
// One http.Client is built per distinct proxy resolution (direct, or a config
// proxy URL) and shared by every provider that resolves the same way — the
// per-provider proxy switch never costs a per-request check.
func (rt *Router) Install(s *Snapshot) {
	byResolution := map[string]*http.Client{}
	s.clients = map[string]*http.Client{}
	for protocol, byName := range s.Cfg.Providers {
		for name, p := range byName {
			mode, proxyURL := s.Cfg.ProxySpecFor(p)
			key := mode + "|" + proxyURL
			c, ok := byResolution[key]
			if !ok {
				c = NewUpstreamClient(s.Cfg, p)
				byResolution[key] = c
				s.clientSet = append(s.clientSet, c)
			}
			s.clients[protocol+"/"+name] = c
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
