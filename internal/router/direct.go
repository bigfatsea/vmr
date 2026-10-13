// Ver 2026-10-13 08:12, by Claude Opus 5.5

// Direct routing: with config's direct_routing on, a client may name a
// real upstream model as "provider:model" instead of a virtual model.
//
// Like pinned routing (pin.go) it is a lens over endpoints the config
// already declares, never an opening: the addressable set is exactly the
// (protocol, provider, model) triples some virtual model or
// fallback_endpoints expands to, so no new HealthKey, quota bucket or
// upstream ever comes into existence. A direct route is otherwise an
// ordinary ModelRoute and goes through the same pipeline — health,
// conditions, pin, sort, quota reorder, sticky — minus fallback injection:
// the client named one model, and landing it on another would defeat the
// point.
//
// Direct routes live in their own Snapshot.Direct table, not in Models, so
// every consumer that walks Models (/status topology, alerts, diagnose,
// quota rows, vmr check's model list) keeps listing only virtual models.
// Snapshot.Route is the one lookup that sees both.
package router

import (
	"fmt"
	"sort"
	"strings"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
)

// Route resolves name under protocol: a virtual model first, then a direct
// "provider:model" route. Config validation keeps the two namespaces
// disjoint while direct routing is on, so the order is only a tiebreak in
// principle. Nil-safe.
func (s *Snapshot) Route(protocol, name string) (*ModelRoute, bool) {
	if s == nil {
		return nil, false
	}
	if r, ok := s.Models[protocol][name]; ok {
		return r, true
	}
	r, ok := s.Direct[protocol][name]
	return r, ok
}

// buildDirectRoutes derives the direct table from the already-built virtual
// model routes, so it inherits their Provider.Disabled filtering and
// model_defaults resolution for free. Each endpoint registers under its
// group name ("<written name>:model", every api_keys key at once) and, for
// an expanded api_keys entry, also under its own name (that key only).
//
// Endpoints are copies with Priority 0 and FromFallback false — a direct
// route has a single tier, and the same triple can appear in several groups
// with different priorities. HealthKey/Name are unchanged by those two
// fields, so health state, sticky pointers, quota and provider concurrency
// stay shared with the original endpoints.
func buildDirectRoutes(cfg *config.Config, models map[string]map[string]*ModelRoute) map[string]map[string]*ModelRoute {
	groupOf := make(map[string]string, len(cfg.Providers))
	for _, p := range cfg.Providers {
		groupOf[p.Name] = p.GroupName()
	}
	out := map[string]map[string]*ModelRoute{}
	seen := map[string]bool{} // protocol + name + HealthKey
	for _, protocol := range fmtutil.SortedKeys(models) {
		byName := map[string]*ModelRoute{}
		for _, vm := range fmtutil.SortedKeys(models[protocol]) {
			for _, ep := range models[protocol][vm].Endpoints {
				names := []string{core.ProviderModel(groupOf[ep.Provider], ep.Model)}
				if groupOf[ep.Provider] != ep.Provider {
					names = append(names, core.ProviderModel(ep.Provider, ep.Model))
				}
				for _, name := range names {
					k := protocol + "\x00" + name + "\x00" + ep.HealthKey()
					if seen[k] {
						continue
					}
					seen[k] = true
					route := byName[name]
					if route == nil {
						route = &ModelRoute{Sticky: true}
						byName[name] = route
					}
					cp := *ep
					cp.Priority, cp.FromFallback = 0, false
					route.Endpoints = append(route.Endpoints, &cp)
				}
			}
		}
		for _, route := range byName {
			sort.SliceStable(route.Endpoints, func(i, j int) bool {
				return route.Endpoints[i].Provider < route.Endpoints[j].Provider
			})
			route.GuardAllTrusted = allEndpointsTrusted(route.Endpoints, cfg.Guard)
		}
		if len(byName) > 0 {
			out[protocol] = byName
		}
	}
	return out
}

// DirectNames returns every direct-routing name across protocols, sorted
// and deduplicated — for /v1/models and vmr check.
func (s *Snapshot) DirectNames() []string {
	if s == nil {
		return nil
	}
	set := map[string]bool{}
	for _, byName := range s.Direct {
		for name := range byName {
			set[name] = true
		}
	}
	return fmtutil.SortedKeys(set)
}

// DirectNode is one direct-routing name in DirectTree. An api_keys group name
// ("openrouter_free:m", Keys > 1) owns the per-key names its keys expand to
// ("openrouter_free-abfs:m", ...) as Members, so a listing can show that they
// are the same model reached two ways: through the group, or pinned to one key.
type DirectNode struct {
	Name      string
	Provider  string // the part before the first ':'
	Protocols []string
	Keys      int // endpoints behind the name when more than one (a group name)
	Members   []*DirectNode
}

// DirectTree lists the direct names reachable under protocol ("" = every
// protocol, with Protocols recording where each is reachable), sorted by name,
// with per-key names nested under their group name. A per-key name whose group
// name is absent from the set stays a root. Nil-safe.
func (s *Snapshot) DirectTree(protocol string) []*DirectNode {
	if s == nil {
		return nil
	}
	nodes := map[string]*DirectNode{}
	for _, proto := range fmtutil.SortedKeys(s.Direct) {
		if protocol != "" && proto != protocol {
			continue
		}
		for name, route := range s.Direct[proto] {
			n := nodes[name]
			if n == nil {
				prov, _, _ := strings.Cut(name, ":")
				n = &DirectNode{Name: name, Provider: prov}
				nodes[name] = n
			}
			n.Protocols = append(n.Protocols, proto)
			if k := len(route.Endpoints); k > 1 && k > n.Keys {
				n.Keys = k
			}
		}
	}
	var roots []*DirectNode
	for _, name := range fmtutil.SortedKeys(nodes) {
		n := nodes[name]
		if parent := s.directGroupName(n.Name); parent != "" {
			if g := nodes[parent]; g != nil {
				g.Members = append(g.Members, n)
				continue
			}
		}
		roots = append(roots, n)
	}
	return roots
}

// directGroupName is the group-level direct name an expanded api_keys name
// belongs to ("" for a name that is not a per-key expansion).
func (s *Snapshot) directGroupName(name string) string {
	if s.Cfg == nil {
		return ""
	}
	prov, model, _ := strings.Cut(name, ":")
	p, ok := s.Cfg.ProviderByName(prov)
	if !ok || p.GroupName() == p.Name {
		return ""
	}
	return core.ProviderModel(p.GroupName(), model)
}

// unknownModelMessage is Serve's 404 text for a name no route answers. A
// "provider:model"-shaped name gets a reason instead of only the model
// list: direct routing is off, or that pair isn't declared under this
// protocol.
func unknownModelMessage(s *Snapshot, protocol, name string) string {
	list := strings.Join(modelNames(s, protocol), ", ")
	if prov, _, ok := strings.Cut(name, ":"); ok && s.Cfg != nil && s.Cfg.IsProviderName(prov) {
		if !s.Cfg.DirectRouting {
			return fmt.Sprintf("model %q not found: provider:model names need direct_routing: true in the config; models on this endpoint: %s", name, list)
		}
		return fmt.Sprintf("model %q not found: direct routing only reaches provider:model pairs some virtual model or fallback_endpoints declares for the %s protocol; models on this endpoint: %s", name, protocol, list)
	}
	return fmt.Sprintf("model %q not found; models on this endpoint: %s", name, list)
}
