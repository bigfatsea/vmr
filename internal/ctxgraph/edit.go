// Ver 2026-07-28 22:30, by Sonnet 5

package ctxgraph

// EditKind classifies the transition between two consecutive manifests in
// the same SessKey bucket. Deliberately structural — no template/marker
// matching — so it works the same for any agent client (design doc F11:
// at least three real compaction shapes exist, two of which have no marker
// to match at all).
//
// Step 1 (this package's first cut) distinguishes four kinds. A real
// "mid-conversation splice" (design doc F11's S2: a message rewritten in
// place, absorbing a summary) and an ordinary trailing-message replacement
// (ephemeral tail edits, image pruning) both land in ReplaceTail for now —
// telling them apart needs a second structural check (does the ORIGINAL
// tail resurface elsewhere in the new manifest?) that doesn't change
// whether the lineage splits, only how the split is explained. That's
// deferred to Phase 2 (see the design doc's Appendix C.4 T2.1) — splitting
// it out is a labeling refinement, not a correctness fix: ReplaceTail never
// splits a lineage, exactly like Splice never will either.
type EditKind int

const (
	// Append: cur's manifest starts with all of prev's messages (LCP ==
	// len(prev)) plus something new at the end. The overwhelmingly common
	// case — 95.86% of edges in the calibration corpus (Appendix A.7).
	Append EditKind = iota
	// ReplaceTail: a common prefix holds, but the tail diverges without
	// prev shrinking drastically or losing most of its content — normal
	// turn-to-turn editing (retries, ephemeral message replacement) or an
	// unclassified in-place splice (see the Step-1/Step-2 split above).
	// Does not split the lineage.
	ReplaceTail
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
// records, 168 multi-turn sessions) — see docs/
// Agent任务叙事报告_设计与价值论证_2026-07-28_opus-5.md 附录 A.7 for the
// resulting edit-kind distribution these values produce. Tune here when a
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
// mirrors the classifier used to produce the corpus distribution in
// Appendix A.7 — do not reorder without rechecking that appendix.
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

// coverage is |cur ∩ prev| / len(cur) — how much of cur's content already
// existed somewhere in prev (not just its LCP-matched prefix), the same
// set-based check design doc F6/session.go's deltaHasNewInstruction relies
// on to avoid position-based false positives after a mid-history edit.
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
