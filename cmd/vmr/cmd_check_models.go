// Ver 2026-10-13 17:30, by Sonnet 5.5
package main

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/fmtutil"
	"vmr/internal/router"
)

// printModelCatalog is `vmr check models`: a compact, read-only catalog of
// what the config actually routes to — every provider with the models some
// virtual model or fallback_endpoints references, then every virtual model's
// try-order per protocol. Both views read the same Snapshot the router
// serves from, so they cannot drift from real routing. A provider's model
// list is only what the config references: vmr keeps no per-provider model
// catalog and does no network I/O here.
func printModelCatalog(w io.Writer, cfg *config.Config, snap *router.Snapshot) {
	printProviderCatalog(w, cfg, snap)
	fmt.Fprintln(w)
	printVirtualCatalog(w, snap)
}

// catalogModel is one upstream model under a provider group.
type catalogModel struct {
	protocols map[string]bool
	refs      map[string]bool // "virtual@priority", or "fallback@priority"
}

func printProviderCatalog(w io.Writer, cfg *config.Config, snap *router.Snapshot) {
	groupOf := make(map[string]string, len(cfg.Providers))
	keys := map[string][]string{}
	disabled := map[string]bool{}
	var groups []string
	for _, p := range cfg.Providers {
		g := p.GroupName()
		groupOf[p.Name] = g
		if _, seen := keys[g]; !seen {
			keys[g] = nil
			groups = append(groups, g)
		}
		if p.Name != g {
			keys[g] = append(keys[g], strings.TrimPrefix(p.Name, g+"-"))
		}
		disabled[g] = p.Disabled
	}
	models := map[string]map[string]*catalogModel{}
	for _, protocol := range fmtutil.SortedKeys(snap.Models) {
		for _, vm := range fmtutil.SortedKeys(snap.Models[protocol]) {
			for _, ep := range snap.Models[protocol][vm].Endpoints {
				g := groupOf[ep.Provider]
				if models[g] == nil {
					models[g] = map[string]*catalogModel{}
				}
				m := models[g][ep.Model]
				if m == nil {
					m = &catalogModel{protocols: map[string]bool{}, refs: map[string]bool{}}
					models[g][ep.Model] = m
				}
				m.protocols[protocol] = true
				ref := vm
				if ep.FromFallback {
					ref = "fallback" // injected into every virtual model; one ref says it
				}
				m.refs[ref+"@"+strconv.Itoa(ep.Priority)] = true
			}
		}
	}

	fmt.Fprintln(w, "=== Providers → Models ===")
	slices.Sort(groups)
	for _, g := range groups {
		head := g
		if len(keys[g]) > 0 {
			slices.Sort(keys[g])
			head += " (" + strings.Join(keys[g], ", ") + ")"
		}
		switch {
		case disabled[g]:
			head += "  (disabled)"
		case len(models[g]) == 0:
			head += "  (no models referenced)"
		}
		fmt.Fprintln(w, head)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, name := range fmtutil.SortedKeys(models[g]) {
			m := models[g][name]
			line := "  " + name + "\t" + strings.Join(fmtutil.SortedKeys(m.protocols), ",") + "\t" + joinRefs(m.refs)
			if directReachable(snap, g, name) {
				line += "  [direct]"
			}
			fmt.Fprintln(tw, line)
		}
		tw.Flush()
	}
}

// directReachable reports whether direct routing exposes group:model under
// any protocol (always false while direct_routing is off).
func directReachable(snap *router.Snapshot, group, model string) bool {
	for _, byName := range snap.Direct {
		if _, ok := byName[core.ProviderModel(group, model)]; ok {
			return true
		}
	}
	return false
}

// joinRefs renders "virtual@priority" refs ordered by priority, then name.
func joinRefs(refs map[string]bool) string {
	list := fmtutil.SortedKeys(refs)
	prio := func(ref string) int {
		n, _ := strconv.Atoi(ref[strings.LastIndexByte(ref, '@')+1:])
		return n
	}
	slices.SortStableFunc(list, func(a, b string) int { return prio(a) - prio(b) })
	return strings.Join(list, " ")
}

func printVirtualCatalog(w io.Writer, snap *router.Snapshot) {
	fmt.Fprintln(w, "=== Virtual Models → Upstreams ===")
	names := map[string]bool{}
	for _, byName := range snap.Models {
		for name := range byName {
			names[name] = true
		}
	}
	for i, name := range fmtutil.SortedKeys(names) {
		if i > 0 {
			fmt.Fprintln(w)
		}
		for _, protocol := range fmtutil.SortedKeys(snap.Models) {
			route, ok := snap.Models[protocol][name]
			if !ok {
				continue
			}
			fmt.Fprintf(w, "%s / %s\n", name, protocol)
			printTiers(w, route)
		}
	}
}

// printTiers prints one line per priority tier of route's try-order, the
// tier's endpoints in the order the router will try them.
func printTiers(w io.Writer, route *router.ModelRoute) {
	var items []string
	flush := func(prio int) {
		if len(items) > 0 {
			fmt.Fprintf(w, "  p=%-3d %s\n", prio, strings.Join(items, ", "))
			items = nil
		}
	}
	cur := 0
	for i, ep := range route.EffectiveOrder() {
		if i > 0 && ep.Priority != cur {
			flush(cur)
		}
		cur = ep.Priority
		label := core.ProviderModel(ep.Provider, ep.Model)
		if ep.FromFallback {
			label += " (fallback)"
		}
		items = append(items, label)
	}
	flush(cur)
}
