// Ver 2026-08-20 17:10, by Sonnet 5

// Self-traffic exclusion (P6.4): `vmr story -llm-addr`'s interpretation
// calls route back through this same VMR instance and land in the audit
// log like any other request. Their cost/tokens are the analysis tool's
// own overhead, not the workload being analyzed — the architecture doc's
// §9 risk #1 calls this out by name: it pollutes both `vmr report`'s cost
// report and `vmr story -corpus`'s aggregate stats.
//
// The identification rule is defined exactly once, here, and consumed by
// both cmd_report.go (internal/report's excludeClientTags) and
// cmd_story.go (filtering ListCandidates' output) — never reimplemented
// per command, the same discipline this project already applies to
// session/task segmentation.
package main

import (
	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

// selfTrafficExcludeTags builds the exclusion set: audit.KeyTag(llmKey) —
// the SAME transform internal/server's authenticateWithSnap() applies to every
// configured api_keys entry, so this reproduces exactly the client_key_tag
// a self-analysis call carries in the audit log — plus any explicitly
// configured report.yaml self_traffic_client_tags (for the edge case where
// -llm-addr traffic was generated under a different/rotated credential).
// llmKey == "" contributes nothing (most `vmr report`/`vmr analyze
// -macro-only` runs never resolve one at all — cmd_report.go's -llm-key
// flag, added P15.3, only ever identifies PAST self-analysis traffic to
// exclude; `vmr report` never makes a new LLM call itself).
// Returns nil (not an empty map) when there is nothing to exclude, so
// callers can pass it straight through as "exclude nothing" without a
// separate nil check.
func selfTrafficExcludeTags(llmKey string, extra []string) map[string]bool {
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

// filterSelfTrafficCandidates drops any candidate Lineage whose root
// manifest's ClientKeyTag is a self-traffic tag (P6.4) — filtered here in
// cmd/vmr, not inside story.ListCandidates: self-traffic identification is
// a deployment-time configuration fact, not a structural signal, so it
// doesn't belong in internal/story's own "no new guessing" judgment.
func filterSelfTrafficCandidates(cands []*ctxgraph.Lineage, llmKey string, extra []string) []*ctxgraph.Lineage {
	excludeTags := selfTrafficExcludeTags(llmKey, extra)
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
