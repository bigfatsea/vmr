// Ver 2026-07-29 22:30, by Sonnet 5

package story

import (
	"errors"

	"vmr/internal/ctxgraph"
)

var errEmptyLineage = errors.New("story: lineage has no manifests")

// ListCandidates returns the lineages worth offering as a Journey,
// chronologically. Each returned lineage is a chain TAIL — call
// ctxgraph.ChainFrom(l, byIdx) to get its full stitched chain before
// rendering; a lineage with a stitched successor
// is excluded here because its content is already reachable through that
// successor's own chain (rendering it again as its own candidate would
// duplicate it). A lineage with fewer than two manifests is excluded
// without any content-based tag detection: a single-request lineage is
// exactly what a scheduled single-shot call (OpenClaw's heartbeat/
// dream_diary and similar) looks like structurally, and there is no task
// narrative to tell for one request anyway.
func ListCandidates(g *ctxgraph.Graph) []*ctxgraph.Lineage {
	hasSuccessor := ctxgraph.StitchedSuccessorSet(g)
	byIdx := ctxgraph.LineageIndex(g)
	var out []*ctxgraph.Lineage
	for _, l := range g.Lineages {
		if hasSuccessor[l.Idx] {
			continue // not a chain tail; reachable via its successor's chain
		}
		// The >=2-manifests bar applies to the FULL chain, not just this
		// tail lineage alone — a stitched successor can legitimately be a
		// single post-compaction request on its own, but the chain it
		// completes usually has plenty of narrative.
		total := 0
		for _, cl := range ctxgraph.ChainFrom(l, byIdx) {
			total += len(cl.Manifests)
		}
		if total < 2 {
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

// IsPartialHead reports whether chain[0]'s root manifest — the Journey's
// actual visible beginning after stitching, not necessarily the candidate
// lineage passed to ListCandidates — looks like it sits in a file the
// caller didn't load, rather than genuinely being the start of a
// conversation. A fresh conversation's first turn is
// short — leading system plus one real instruction, maybe a brief ack, so
// few non-system keys. A root manifest with substantially more content
// than that, sitting within the first few lines of the earliest scanned
// file, is more likely a continuation whose actual opening request lives
// outside the loaded range.
//
// firstPath is the caller's own notion of "earliest file" (its sorted
// input path list's [0]) — Graph doesn't track this itself, since it's a
// property of what the CALLER chose to load, not of the data.
func IsPartialHead(chain []*ctxgraph.Lineage, firstPath string) bool {
	root := chain[0].Manifests[0]
	if len(root.Keys) <= coldStartKeyBudget {
		return false
	}
	return root.Path == firstPath && root.Line <= partialHeadLineBudget
}
