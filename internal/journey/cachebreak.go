// Ver 2026-09-16, by pi

package journey

import (
	"vmr/internal/ctxgraph"
)

// CacheBreakKind classifies why a turn experienced a prompt cache miss or hit ratio drop.
type CacheBreakKind string

const (
	CacheBreakNone           CacheBreakKind = ""
	CacheBreakSystem         CacheBreakKind = "system"
	CacheBreakTools          CacheBreakKind = "tools"
	CacheBreakProviderSwitch CacheBreakKind = "provider_switch"
	CacheBreakHistoryStitch  CacheBreakKind = "history:stitch"
	CacheBreakHistory        CacheBreakKind = "history:" // prefix: history:replace_tail, history:splice, etc.
	CacheBreakUnexplained    CacheBreakKind = "unexplained"
)

// Threshold constants for detecting a sudden unexplained cache drop. All three
// must hold — a break is genuine only when a well-established cache collapses to
// near nothing, which is what a full-prefix re-encode looks like.
//
// CacheDropPrevMin (0.50): the prior turn must have had real cache reuse (>= 50%)
// — otherwise there is nothing to "break".
//
// CacheDropDiffThreshold (0.25): the drop must be steeper than incremental append
// dilution. A big tool result adding fresh input tokens routinely dilutes the
// ratio a little; 25 points between consecutive turns cannot be that.
//
// CacheDropAbsFloor (0.15): the *current* ratio must be near-zero. A genuine
// break re-encodes the whole prefix, so the ratio collapses; 0.98 -> 0.70 is
// still a healthy cache and is normal token dilution, not a break. Without this
// floor the attribution fires on ~half of ordinary multi-turn appends.
const (
	CacheDropPrevMin       = 0.50
	CacheDropDiffThreshold = 0.25
	CacheDropAbsFloor      = 0.15
)

// CacheRatio computes the prompt-cache hit ratio (CacheRead / In) from manifest usage.
// Returns (0, false) if In is uncomputable, non-positive, or UsageInOK is false.
func CacheRatio(m *ctxgraph.Manifest) (float64, bool) {
	if m == nil || !m.UsageInOK || m.Usage.In <= 0 {
		return 0, false
	}
	return float64(m.Usage.CacheRead) / float64(m.Usage.In), true
}

// ComputeCacheBreak determines the primary cause of prompt cache invalidation
// between consecutive turns. Returns CacheBreakNone for first turns, uncomputable
// transitions, or normal high-reuse appends.
func ComputeCacheBreak(prev, cur *ctxgraph.Manifest, edge *ctxgraph.Edit, stitch *ctxgraph.StitchEdge) CacheBreakKind {
	if prev == nil || cur == nil {
		return CacheBreakNone
	}

	// 1. Stitch boundary: structural break across lineages
	if edge == nil && stitch != nil {
		return CacheBreakHistoryStitch
	}

	// 2. System prompt changed (added, removed, or text changed)
	if cur.HasSys != prev.HasSys || (cur.HasSys && prev.HasSys && cur.SysHash != prev.SysHash) {
		return CacheBreakSystem
	}

	// 3. Tool declarations churned: a toolset appearing, disappearing, or changing
	// shape all break the cache prefix (tools sit ahead of the messages).
	if cur.HasTools != prev.HasTools || (prev.HasTools && cur.HasTools && prev.ToolsHash != cur.ToolsHash) {
		return CacheBreakTools
	}

	// 4. Provider switch: request served by a different physical endpoint
	if prev.ServedEndpoint != "" && cur.ServedEndpoint != "" && prev.ServedEndpoint != cur.ServedEndpoint {
		return CacheBreakProviderSwitch
	}

	// 5. History divergence: tail replaced, spliced, contracted, or forked
	if edge != nil && edge.Kind != ctxgraph.Append {
		return CacheBreakHistory + CacheBreakKind(edge.Kind.String())
	}

	// 6. Expected full reuse (Append + same sys + same tools + same endpoint),
	// but an established cache collapsed to near-zero without explanation.
	if prevRatio, ok1 := CacheRatio(prev); ok1 {
		if curRatio, ok2 := CacheRatio(cur); ok2 {
			if prevRatio >= CacheDropPrevMin &&
				curRatio < prevRatio-CacheDropDiffThreshold &&
				curRatio < CacheDropAbsFloor {
				return CacheBreakUnexplained
			}
		}
	}

	// 7. Otherwise none
	return CacheBreakNone
}

// ShouldDisplayCacheBreak reports whether a cache break kind is notable enough
// to render in the decision spine and UI badges. Normal turn-to-turn appends
// and routine tail edits / splices are suppressed to avoid visual noise.
func ShouldDisplayCacheBreak(kind string) bool {
	switch CacheBreakKind(kind) {
	case CacheBreakUnexplained,
		CacheBreakProviderSwitch,
		CacheBreakSystem,
		CacheBreakTools,
		CacheBreakHistoryStitch,
		CacheBreakHistory + "contract",
		CacheBreakHistory + "fork":
		return true
	default:
		return false
	}
}
