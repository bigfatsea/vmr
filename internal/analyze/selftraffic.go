// Ver 2026-09-22 19:15, by Sonnet 5

// Self-traffic exclusion: `vmr analyze -llm-addr`'s interpretation
// calls route back through this same VMR instance and land in the audit
// log like any other request. Their cost/tokens are the analysis tool's
// own overhead, not the workload being analyzed — the architecture doc's
// risk register calls this out by name: it pollutes both `vmr analyze`'s cost
// report and `vmr analyze -benchmark`'s aggregate stats.
//
// The identification rule is defined exactly once, here, and consumed by
// both the macro report half (internal/report's excludeClientTags) and
// the journey half (filtering ListCandidates' output) — never reimplemented
// per command, the same discipline this project already applies to
// session/task segmentation.
package analyze

import (
	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

// SelfTrafficExcludeTags builds the exclusion set: audit.KeyTag(llmKey) —
// the SAME transform internal/server's authenticateWithSnap() applies to every
// configured api_keys entry, so this reproduces exactly the client_key_tag
// a self-analysis call carries in the audit log — plus any explicitly
// configured report.yaml self_traffic_client_tags (for the edge case where
// -llm-addr traffic was generated under a different/rotated credential).
// llmKey == "" contributes nothing (most `vmr analyze
// -macro-only` runs never resolve one at all — the -llm-key flag only ever
// identifies PAST self-analysis traffic to exclude; `vmr analyze` never
// makes a new LLM call itself).
// Returns nil (not an empty map) when there is nothing to exclude, so
// callers can pass it straight through as "exclude nothing" without a
// separate nil check.
func SelfTrafficExcludeTags(llmKey string, extra []string) map[string]bool {
	if llmKey == "" && len(extra) == 0 {
		return nil
	}
	tags := map[string]bool{}
	if llmKey != "" {
		tags[audit.KeyTag(llmKey)] = true
	}
	for _, tag := range extra {
		if tag != "" {
			tags[tag] = true
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// LLMSelfTag is the exclusion tag one effective llm_key contributes to the
// self-traffic exclusion set (audit.KeyTag), empty when there is no key —
// the single-string form SelfTrafficExcludeTags folds into its map. It is
// what the L2 cache's analysis-params fingerprint has to see: adding or
// changing -llm-key/report.yaml llm_key changes which records the run
// excludes, so it must invalidate the product cache.
func LLMSelfTag(llmKey string) string {
	if llmKey == "" {
		return ""
	}
	return audit.KeyTag(llmKey)
}

// filterSelfTrafficCandidates drops any candidate Lineage whose root
// manifest's ClientKeyTag is a self-traffic tag — filtered here in
// analyze, not inside journey.ListCandidates: self-traffic identification is
// a deployment-time configuration fact, not a structural signal, so it
// doesn't belong in internal/journey's own "no new guessing" judgment.
func filterSelfTrafficCandidates(cands []*ctxgraph.Lineage, llmKey string, extra []string) []*ctxgraph.Lineage {
	excludeTags := SelfTrafficExcludeTags(llmKey, extra)
	if len(excludeTags) == 0 {
		return cands
	}
	kept := cands[:0]
	for _, l := range cands {
		if len(l.Manifests) > 0 && excludeTags[l.Manifests[0].ClientKeyTag] {
			continue
		}
		kept = append(kept, l)
	}
	return kept
}
