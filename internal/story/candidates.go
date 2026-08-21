// Ver 2026-07-29 22:30, by Sonnet 5

package story

import (
	"errors"
	"strings"

	"vmr/internal/ctxgraph"
)

var errEmptyLineage = errors.New("story: lineage has no manifests")

// errNilProfile guards every exported entry point that fans out into
// concurrent goroutines (BuildAll) or worker-free recursion (BuildChain) —
// a nil taskseg.Profile reaching prof.RealUserText inside one of those
// would panic with no recover() in the call chain, which for a goroutine
// means the whole process dies mid-flight instead of returning a clean
// error the caller can report.
var errNilProfile = errors.New("story: prof is nil")

// classifyJourney tags a candidate Journey by structural signals in its
// already-derived title alone (P6.3) — it does not re-scan message
// content or consult turn count. Turn count was in this task's original
// plan as a second signal, but real-corpus verification (run against the full
// local logs/ corpus, 477 candidate Journeys) found title markers alone fully
// separate cron/heartbeat/subagent from real tasks with zero ambiguity — and
// a short-but-genuine interaction (a real corpus example: "hi back", 2 turns)
// would be misclassified as noise by a turn-count heuristic with no supporting
// evidence it helps. Dropping the unused signal follows the same "don't
// introduce a guess without evidence" rule the category design itself is
// built on.
//
// The three literal markers below are verbatim substrings observed in
// real title output, not the architecture doc's paraphrased examples
// (which turned out inexact — see this package's real-corpus survey):
//   - cron:      title HAS PREFIX "[cron:<job-id> ...]" — OpenClaw's
//     scheduler always opens the title with this, never buried mid-text.
//   - heartbeat: title CONTAINS "[OpenClaw heartbeat poll]" — appears
//     right after OpenClaw's leading timestamp bracket, so a prefix check
//     alone would miss it.
//   - subagent:  title CONTAINS "[Subagent Context]" — same shape as
//     heartbeat, always preceded by the timestamp bracket.
//
// All three were disjoint across the full real corpus (0 titles matched
// more than one), but the checks below still run in a fixed priority
// order (cron, then heartbeat, then subagent) in case a future client
// ever combines markers — silently picking one deterministic answer beats
// depending on map/branch ordering.
func classifyJourney(title string) JourneyCategory {
	switch {
	case strings.HasPrefix(title, "[cron:"):
		return CategoryCron
	case strings.Contains(title, "[OpenClaw heartbeat poll]"):
		return CategoryHeartbeat
	case strings.Contains(title, "[Subagent Context]"):
		return CategorySubagent
	default:
		return CategoryTask
	}
}

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
