// Ver 2026-07-29 21:00, by Sonnet 5

package ctxgraph

// EditKind classifies the transition between two consecutive manifests in
// the same SessKey bucket. Deliberately structural — no template/marker
// matching — so it works the same for any agent client (at least three
// real compaction shapes exist, two of which have no marker to match at
// all).
//
// Step 1 shipped four kinds, with a real "mid-conversation splice" (a
// message rewritten in place, absorbing a summary) and an ordinary
// trailing-message replacement (ephemeral tail edits, image pruning) both
// landing in ReplaceTail — deliberately deferred ("明确不做": telling them
// apart doesn't change whether the lineage splits, only how the split is
// explained). Step 2 tells them apart: Splice is now its own kind, split out
// of ReplaceTail by a second structural check (does a suffix of prev's tail
// resurface, unchanged, as a suffix of cur's tail? — evidence the tail
// wasn't discarded but spliced around). Splice still never splits a
// lineage, exactly like ReplaceTail — this is a labeling refinement, not a
// correctness fix.
type EditKind int

const (
	// Append: cur's manifest starts with all of prev's messages (LCP ==
	// len(prev)) plus something new at the end. The overwhelmingly common
	// case — 95.86% of edges in the calibration corpus.
	Append EditKind = iota
	// ReplaceTail: a common prefix holds, but the tail diverges without
	// prev shrinking drastically or losing most of its content, AND none of
	// prev's tail resurfaces in cur's tail (see Splice) — ordinary
	// turn-to-turn editing: retries, ephemeral message replacement (image
	// pruning, a resent request with a corrected last message). Does not
	// split the lineage.
	ReplaceTail
	// Splice: same shape as ReplaceTail (common prefix holds, tail
	// diverges, no drastic shrink), but at least spliceMinTailMatch of
	// prev's tail messages reappear verbatim, in order, at the end of cur's
	// tail — new content was inserted mid-conversation and the original
	// tail preserved further along, not discarded. Does NOT split the
	// lineage ("splitting it out is a labeling refinement, not a
	// correctness fix").
	Splice
	// Contract: cur is much smaller than prev (below contractLenRatio) —
	// history was truncated or rebuilt. Splits the lineage.
	Contract
	// Fork: cur shares little content with prev (below forkCoverage) even
	// though it's in the same SessKey bucket (same anchor or metadata
	// session id) — a genuinely new conversation reusing the same
	// fingerprint. Splits the lineage.
	Fork
)

func (k EditKind) String() string {
	switch k {
	case Append:
		return "append"
	case ReplaceTail:
		return "replace_tail"
	case Splice:
		return "splice"
	case Contract:
		return "contract"
	case Fork:
		return "fork"
	default:
		return "unknown"
	}
}

// Splits reports whether this edit kind starts a new Lineage.
func (k EditKind) Splits() bool {
	return k == Contract || k == Fork
}

// Threshold constants, calibrated against the 2026-07-14..28 corpus (7112
// records, 168 multi-turn sessions) — see docs/VirtualModelRouter_Design_v4_Analytics.md
// §5 for the resulting edit-kind distribution these values produce. Tune here when a
// wider or different corpus disagrees; deliberately NOT a config knob —
// users cannot calibrate what they have no way to measure, and getting
// this wrong just means the report explains itself worse, it doesn't route
// traffic differently.
const (
	// contractLenRatio: cur is a Contract if it has fewer than this
	// fraction of prev's messages.
	contractLenRatio = 0.6
	// forkCoverage: cur is a Fork if less than this fraction of its own
	// messages were already present somewhere in prev.
	forkCoverage = 0.5
	// tailSlack: an LCP within this many messages of len(prev) still
	// counts as a plain Append (guards against off-by-one/duplicate-final-
	// message noise being classified as ReplaceTail for no useful reason).
	tailSlack = 2
	// spliceMinTailMatch: within a ReplaceTail-shaped edit, reclassify as
	// Splice when at least this many of prev's tail messages reappear
	// verbatim, in order, at the end of cur's tail. 2 (not 1) guards
	// against a single short, generic reply ("好的"/"OK") matching by pure
	// coincidence rather than genuine evidence the tail was preserved —
	// tune against a wider corpus if this misses or over-fires real splices.
	spliceMinTailMatch = 2
)

// Edit is the classified transition from prev to cur.
type Edit struct {
	Kind EditKind
	// LCP is the longest common prefix length over the two manifests' Keys.
	LCP int
	// Coverage is |cur.Keys ∩ prev.Keys| / len(cur.Keys) (1.0 if cur.Keys
	// is empty — vacuously fully covered).
	Coverage float64
}

// Classify determines the edit between two manifests known to be adjacent
// in time within the same SessKey bucket. The order of checks matters and
// mirrors the classifier used to produce the calibration corpus's edit-kind
// distribution — do not reorder without rechecking that distribution.
func Classify(prev, cur *Manifest) Edit {
	l := lcpLen(prev.Keys, cur.Keys)
	cov := coverage(cur.Keys, prev.Keys)
	e := Edit{LCP: l, Coverage: cov}

	switch {
	case l == len(prev.Keys):
		e.Kind = Append
	case float64(len(cur.Keys)) < float64(len(prev.Keys))*contractLenRatio:
		e.Kind = Contract
	case cov < forkCoverage:
		e.Kind = Fork
	case l < len(prev.Keys)-tailSlack:
		e.Kind = ReplaceTail
		if commonSuffixLen(prev.Keys[l:], cur.Keys[l:]) >= spliceMinTailMatch {
			e.Kind = Splice
		}
	default:
		e.Kind = Append
	}
	return e
}

// lcpLen is the longest common prefix length of two hash vectors.
func lcpLen(a, b []Hash) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// commonSuffixLen is lcpLen's mirror: the longest common suffix length of
// two hash vectors, counted from the end — used by Classify to tell Splice
// (old tail resurfaces further along) apart from ReplaceTail (it doesn't).
func commonSuffixLen(a, b []Hash) int {
	n := 0
	for n < len(a) && n < len(b) && a[len(a)-1-n] == b[len(b)-1-n] {
		n++
	}
	return n
}

// coverage is |cur ∩ prev| / len(cur) — how much of cur's content already
// existed somewhere in prev (not just its LCP-matched prefix), the same
// set-based check taskseg.HasNewInstruction relies on to avoid
// position-based false positives after a mid-history edit.
func coverage(cur, prev []Hash) float64 {
	if len(cur) == 0 {
		return 1.0
	}
	prevSet := make(map[Hash]bool, len(prev))
	for _, h := range prev {
		prevSet[h] = true
	}
	n := 0
	for _, h := range cur {
		if prevSet[h] {
			n++
		}
	}
	return float64(n) / float64(len(cur))
}
