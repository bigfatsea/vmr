// Ver 2026-08-12 23:40, by Opus 5

// Model usage & switches within a single Journey — which upstream
// models/endpoints this task actually hit, and where it moved between them.
// Deliberately reads upstream identity from the endpoint (Step.Attempts'
// structured fields, falling back to splitting Manifest.Endpoint), NOT
// Manifest.Model — that field is audit.Record.Model, the VIRTUAL model name
// (e.g. "coding"/"agent"), which a client requests once and never changes
// within a Journey. Reading it here would produce a table that always
// claims "no model switch ever happened" — see
// the cost analysis design's
// §5.5 ① for the full account of this pitfall.
package story

import (
	"sort"

	"vmr/internal/core"
)

// ModelUsageStat is one (provider, model) pair's footprint within a Journey.
type ModelUsageStat struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	// Steps counts every Step that touched this (provider, model) pair
	// through ANY of its upstream attempts — not just the Step's final
	// resolved one: a Step whose first attempt failed
	// over to a different endpoint used to make the failed-over-FROM
	// endpoint invisible in this table entirely (only the last attempt was
	// ever read); it now counts here too, even though it contributed no
	// tokens (see the TokensIn/TokensInCached/TokensOut fields below,
	// attributed only to the Step's final resolved pair and gated per side
	// on Manifest.UsageInOK/UsageOutOK). Read this as "how many Steps
	// touched this upstream", not "how many Steps succeeded on it" — a
	// Step can appear under more than one ModelUsageStat when it failed
	// over mid-Step.
	Steps int `json:"steps"`

	TokensIn       int64 `json:"tokens_in"`
	TokensInCached int64 `json:"tokens_in_cached"`
	TokensOut      int64 `json:"tokens_out"`
}

// ModelSwitch is one point where consecutive Steps resolved to a different
// upstream (provider, model) pair.
type ModelSwitch struct {
	StepSeq int    `json:"step_seq"`
	From    string `json:"from"` // "provider:model"
	To      string `json:"to"`
	// OnFailoverStep is an observational marker, not a causal claim (see
	// this file's package doc comment and the design doc's §5.3): whether
	// the Step where the switch was observed itself needed more than one
	// upstream attempt. failover/TTL-expiry/routing-policy/sticky-off are
	// indistinguishable after the fact — this only says the two co-occurred.
	OnFailoverStep bool `json:"on_failover_step"`

	// Cache telemetry around the switch point (问题 43 ②).
	HasCacheData   bool    `json:"has_cache_data,omitempty"`
	PrevCacheRatio float64 `json:"prev_cache_ratio,omitempty"`
	CurCacheRatio  float64 `json:"cur_cache_ratio,omitempty"`
}

// computeModelUsage walks steps in Seq order and returns the per-(provider,
// model) usage stats (TokensIn-descending, "provider:model" tie-break — see
// this file's sort below) plus every point where the STEP-LEVEL resolved
// upstream changed from the previous resolvable Step (switch detection
// itself is unchanged by the all-attempts fix below — detecting a switch
// that happens WITHIN a Step is a separate, larger change, not done here). A Step whose upstream can't be resolved at all (no structured
// attempt fields AND no usable Endpoint — only possible on a request that
// failed before any attempt) contributes to neither: there is nothing to
// attribute tokens to, and it can't anchor a switch comparison either side.
func computeModelUsage(steps []*Step) ([]ModelUsageStat, []ModelSwitch) {
	stats := map[string]*ModelUsageStat{}
	var switches []ModelSwitch
	prevKey, havePrev := "", false
	var prevStep *Step

	for _, s := range steps {
		provider, model := stepUpstream(s)
		if provider == "" && model == "" {
			continue
		}
		key := provider + ":" + model

		// Count every DISTINCT (provider, model) any
		// attempt against this Step touched, not just the final resolved
		// one — otherwise an endpoint the Step failed over AWAY FROM is
		// invisible in this table even though the request genuinely
		// routed there first.
		seen := map[string]bool{}
		for _, a := range s.Attempts {
			if a.Provider == "" && a.Model == "" {
				continue
			}
			ak := a.Provider + ":" + a.Model
			if seen[ak] {
				continue
			}
			seen[ak] = true
			ast := stats[ak]
			if ast == nil {
				ast = &ModelUsageStat{Provider: a.Provider, Model: a.Model}
				stats[ak] = ast
			}
			ast.Steps++
		}
		// The Step's resolved key itself might not be one of the Attempts
		// entries above (pre-structured-fields audit logs, resolved via
		// stepUpstream's Manifest.Endpoint fallback instead) — ensure both
		// the entry AND this Step's count exist, guarded by `seen` (per-
		// Step, reset every iteration) rather than "was this entry ever
		// created before": with the same key recurring across Steps (the
		// overwhelmingly common case — one endpoint, many Steps), gating on
		// existence alone would only ever count the FIRST Step that created
		// it and silently drop every Step after.
		st := stats[key]
		if st == nil {
			st = &ModelUsageStat{Provider: provider, Model: model}
			stats[key] = st
		}
		if !seen[key] {
			st.Steps++
		}
		// Token attribution stays exactly as before: ONLY the Step's final
		// resolved pair, never a failed-over-away-from attempt — a failed
		// attempt has no usage to attribute. Per side: In counts when the
		// In side was reported, Out when the Out side was (see
		// chatmsg.ExtractUsageSides).
		if s.Manifest != nil {
			if s.Manifest.UsageInOK {
				st.TokensIn += s.Manifest.Usage.In
				st.TokensInCached += s.Manifest.Usage.CacheRead
			}
			if s.Manifest.UsageOutOK {
				st.TokensOut += s.Manifest.Usage.Out
			}
		}

		if havePrev && key != prevKey {
			sw := ModelSwitch{
				StepSeq:        s.Seq,
				From:           prevKey,
				To:             key,
				OnFailoverStep: len(s.Attempts) > 1,
			}
			// Cache ratios are In-side quantities (cacheRead/In).
			if prevStep != nil && prevStep.Manifest != nil && prevStep.Manifest.UsageInOK && prevStep.Manifest.Usage.In > 0 &&
				s.Manifest != nil && s.Manifest.UsageInOK && s.Manifest.Usage.In > 0 {
				sw.HasCacheData = true
				sw.PrevCacheRatio = float64(prevStep.Manifest.Usage.CacheRead) / float64(prevStep.Manifest.Usage.In)
				sw.CurCacheRatio = float64(s.Manifest.Usage.CacheRead) / float64(s.Manifest.Usage.In)
			}
			switches = append(switches, sw)
		}
		prevKey, havePrev, prevStep = key, true, s
	}

	out := make([]ModelUsageStat, 0, len(stats))
	for _, st := range stats {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TokensIn != out[j].TokensIn {
			return out[i].TokensIn > out[j].TokensIn
		}
		return out[i].Provider+":"+out[i].Model < out[j].Provider+":"+out[j].Model
	})
	return out, switches
}

// stepUpstream resolves one Step's real upstream (provider, model),
// preferring the last attempt's structured fields and falling back to
// splitting Manifest.Endpoint for logs written before those fields existed
// — mirrors internal/report/detail.go's attemptUpstream, duplicated rather
// than shared: internal/story can't import internal/report
// (internal/archtest's import-boundary rule), and the dev plan's take is
// that a 12-line duplicate here is cheaper than sinking this into
// internal/audit before a second real consumer needs it (see
// the cost analysis design's
// §5.5 ②).
func stepUpstream(s *Step) (provider, model string) {
	if len(s.Attempts) > 0 {
		a := s.Attempts[len(s.Attempts)-1]
		if a.Provider != "" || a.Model != "" {
			return a.Provider, a.Model
		}
	}
	if s.Manifest == nil {
		return "", ""
	}
	return splitEndpointLabel(s.Manifest.Endpoint)
}

// splitEndpointLabel splits an endpoint label in either the current
// "protocol:provider:model" form or the "/"-joined form older audit logs
// used, returning ("", "") for anything else (including the "-" sentinel
// ctxgraph.Manifest.Endpoint uses when a request never reached any
// attempt). A thin wrapper over core.SplitEndpointLabel that discards
// protocol — byte-identical logic to
// what this function inlined before, so this is a pure dedup, not a
// behavior change.
func splitEndpointLabel(endpoint string) (provider, model string) {
	_, provider, model, ok := core.SplitEndpointLabel(endpoint)
	if !ok {
		return "", ""
	}
	return provider, model
}
