// Ver 2026-07-28 23:15, by Sonnet 5

package story

import (
	"errors"

	"vmr/internal/ctxgraph"
)

var errEmptyLineage = errors.New("story: lineage has no manifests")

// ListCandidates returns the lineages worth offering as a Journey,
// chronologically. A lineage with fewer than two manifests is excluded
// without any content-based tag detection: a single-request lineage is
// exactly what a scheduled single-shot call (OpenClaw's heartbeat/
// dream_diary and similar) looks like structurally, and there is no task
// narrative to tell for one request anyway (design doc §11 D4).
func ListCandidates(g *ctxgraph.Graph) []*ctxgraph.Lineage {
	var out []*ctxgraph.Lineage
	for _, l := range g.Lineages {
		if len(l.Manifests) < 2 {
			continue
		}
		out = append(out, l)
	}
	sortByRootThenTime(out)
	return out
}

// coldStartKeyBudget/partialHeadLineBudget calibrate IsPartialHead — see
// its doc comment. Deliberately cheap constants, not configuration: this
// heuristic trades precision for zero extra cost (Manifest-level only, no
// record refetch just to decide what to skip).
const (
	coldStartKeyBudget    = 2
	partialHeadLineBudget = 50
)

// IsPartialHead reports whether l's root manifest looks like a lineage
// whose true beginning sits in a file the caller didn't load, rather than
// genuinely being the start of a conversation (design doc §11 D1). A fresh
// conversation's first turn is short — leading system plus one real
// instruction, maybe a brief ack, so few non-system keys. A root manifest
// with substantially more content than that, sitting within the first few
// lines of the earliest scanned file, is more likely a continuation whose
// actual opening request lives outside the loaded range.
//
// firstPath is the caller's own notion of "earliest file" (its sorted
// input path list's [0]) — Graph doesn't track this itself, since it's a
// property of what the CALLER chose to load, not of the data.
func IsPartialHead(l *ctxgraph.Lineage, firstPath string) bool {
	root := l.Manifests[0]
	if len(root.Keys) <= coldStartKeyBudget {
		return false
	}
	return root.Path == firstPath && root.Line <= partialHeadLineBudget
}
