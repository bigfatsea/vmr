// Ver 2026-07-26, by Sonnet 5
package main

import (
	"fmt"

	"vmr/internal/config"
	"vmr/internal/core"
	"vmr/internal/router"
)

func cmdCheck(args []string) error {
	path, err := configFlag(args, "check")
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("OK  listen=%s  providers=%d  models=%d  image_downscale=%dpx  image_cache_ttl=%dd  probe_mode=%s  probe_timeout=%s\n",
		cfg.Listen, config.CountNested(cfg.Providers), config.CountNested(cfg.Models), cfg.ImageDownscaleMaxPx, cfg.ImageCacheTTLDays, cfg.ProbeMode, cfg.ProbeTimeout.D())
	fmt.Printf("  dirs log=%s image_cache=%s\n", cfg.LogDir, cfg.ImageCacheDir)
	for _, line := range providerProxyLines(cfg) {
		fmt.Println("  " + line)
	}
	for _, protocol := range core.SortedKeys(cfg.Models) {
		for _, name := range core.SortedKeys(cfg.Models[protocol]) {
			m := cfg.Models[protocol][name]
			imgOverride := ""
			if m.ImageDownscaleMaxPx != nil {
				imgOverride = fmt.Sprintf("  image_downscale=%dpx", *m.ImageDownscaleMaxPx)
			}
			route := snap.Models[protocol][name]
			fmt.Printf("  %s [%s] (strategy=%v sticky=%v)%s\n", name, protocol, m.Strategy, route.Sticky, imgOverride)
			// Print endpoints in the order they'd actually be tried (health
			// ignored — this is a static preview), not raw config priority
			// numbers: with priority omitted (the common case) that order is
			// exactly config-file order, which is the whole point.
			ordered := route.EffectiveOrder()
			for i, ep := range ordered {
				key := cfg.Providers[protocol][ep.Provider].APIKey
				keyState := "key:set"
				if key == "" {
					keyState = "key:EMPTY"
				}
				// Condition-routing/Sticky Model declarations, printed only
				// when they actually constrain something — an endpoint with
				// none of these set behaves exactly as before they existed,
				// and the check output should look exactly as before too
				// (see docs/VirtualModelRouter_System_Design_v3.md §6.4:
				// absent = unconstrained/inherit, never a new limit).
				extra := ""
				if len(ep.Capabilities) > 0 {
					extra += fmt.Sprintf("  capabilities=%v", ep.Capabilities)
				}
				if ep.MaxContextTokens > 0 {
					extra += fmt.Sprintf("  max_context_tokens=%d", ep.MaxContextTokens)
				}
				if route.Sticky {
					extra += fmt.Sprintf("  sticky_ttl=%s", ep.StickyTTL)
				}
				fmt.Printf("    %d. %s/%s  [%s]%s\n", i+1, ep.Provider, ep.Model, keyState, extra)
			}
		}
	}
	return nil
}
