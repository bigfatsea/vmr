// Ver 2026-07-29 22:00, by Sonnet 5

// Stitching (see design doc's stitching-policy section): for every lineage that
// broke away from its bucket (BrokeFrom != nil), find its best predecessor
// across the WHOLE graph using a blob inverted index (hash -> which
// lineages ever carried it) — not just within the original SessKey bucket,
// since a compaction can legitimately land under a different SessKey (a
// changed metadata.user_id, a different anchor after history was rebuilt).
//
// Package-boundary note: the design doc's four stitch kinds
// (splice/compaction/head_prune/same_chat) distinguish "splice" from
// "compaction" using an OPTIONAL marker/summarization-request signal —
// ctxgraph has no template-matching capability by design (see package doc:
// "no template matching, no agent-specific knowledge") and never will, so
// those two collapse into one StitchCompaction kind here, decided purely by
// blob-overlap score. Splitting a StitchCompaction edge further using an
// agent's own compaction marker (e.g. OpenClaw's "The conversation history
// before this point was compacted") is exactly what internal/taskseg
// exists for — a presentation-layer refinement on top of this structural
// edge, not something this package should reach for.
package ctxgraph

import "time"

// StitchKind classifies a stitching edge's structural evidence.
type StitchKind int

const (
	stitchNoneKind StitchKind = iota
	// StitchCompaction: the broken lineage's opening manifest has strong
	// blob overlap with a temporally-preceding lineage's accumulated
	// content, and the break itself was a Contract (history shrank) — the
	// structural signature of a compaction/rebuild that mostly preserved
	// content, just compressed it.
	StitchCompaction
	// StitchHeadPrune: weaker overlap (above the "don't stitch" floor) or a
	// Fork-origin break with real content continuity — history was pruned
	// from the front, but the work plausibly continues.
	StitchHeadPrune
	// StitchSameChat: only a SessKey match within a bucket that already
	// forked, or some overlap below the stitching floor — flagged as
	// "疑似同源" but never auto-stitched ("same_chat 默认不缝合").
	StitchSameChat
)

func (k StitchKind) String() string {
	switch k {
	case StitchCompaction:
		return "compaction"
	case StitchHeadPrune:
		return "head_prune"
	case StitchSameChat:
		return "same_chat"
	default:
		return "none"
	}
}

// StitchOutcome is a lineage's stitching resolution — exactly one of three
// states (the design rule: "缝合逻辑必须区分已缝合/确认无后继/匹配失败三态",
// read here from the searching lineage's own perspective: did IT find a
// predecessor, confirm it has none, or come up ambiguous).
type StitchOutcome int

const (
	// NoBreak: this lineage didn't BrokeFrom anything — stitching doesn't
	// apply (a bucket's first lineage).
	NoBreak StitchOutcome = iota
	// Stitched: a predecessor was found with sufficient evidence
	// (StitchCompaction or StitchHeadPrune).
	Stitched
	// NoPredecessorFound: exhaustive search found zero blob overlap with
	// ANY earlier lineage, and no same_chat signal either — a legitimate,
	// evidenced fresh start (sometimes "not found" is the correct
	// answer, not a search failure), not the same as AmbiguousMatch.
	NoPredecessorFound
	// AmbiguousMatch: some signal exists (same_chat SessKey/time proximity,
	// or overlap below the stitching floor) but it's below the bar to act
	// on — "疑似同源", explicitly not stitched ("宁可断开，不要错连").
	AmbiguousMatch
)

func (o StitchOutcome) String() string {
	switch o {
	case Stitched:
		return "stitched"
	case NoPredecessorFound:
		return "no_predecessor_found"
	case AmbiguousMatch:
		return "ambiguous_match"
	default:
		return "no_break"
	}
}

// StitchEdge is one lineage's resolved (or candidate) connection to its
// predecessor.
type StitchEdge struct {
	Kind    StitchKind
	PredIdx int // the predecessor Lineage.Idx
	// Score is |distinct(B0.Keys) ∩ predecessor's accumulated Keys| /
	// |distinct(B0.Keys)| — the same coverage-ratio shape edit.go's
	// Coverage uses (set semantics: a hash repeated in B0.Keys counts
	// once, on both sides of the fraction), just measured against a
	// candidate predecessor lineage instead of the immediately preceding
	// manifest. 0 for a pure same_chat match (no content overlap at all,
	// only SessKey + time proximity).
	Score float64
	// Confidence is informational, derived from Kind — not used for any
	// further decision (design doc's per-kind stitch-confidence column).
	Confidence float64
}

// StitchResolution is the outcome StitchGraph attaches to Lineage.Stitch.
type StitchResolution struct {
	Outcome StitchOutcome
	Edge    *StitchEdge // non-nil only when Outcome is Stitched or AmbiguousMatch
}

// Thresholds — same calibration philosophy as edit.go's (code constants,
// not config; see edit.go's own comment for why). Initial values, not yet
// corpus-recalibrated the way contractLenRatio/forkCoverage were (design
// a corpus re-run recorded the resulting distribution these produce on
// the 2026-07-14..28 corpus — tune here if a wider corpus disagrees).
const (
	// stitchCompactionScore: a Contract-origin break stitches as
	// StitchCompaction when the successor's opening manifest shares at
	// least this fraction of its OWN distinct keys with a candidate
	// predecessor's accumulated content — the denominator is the
	// successor's opening (B0) key count, never the predecessor's.
	stitchCompactionScore = 0.5
	// stitchMinAbsOverlap is the absolute floor on shared distinct keys,
	// required ALONGSIDE the ratio thresholds: a tiny opening manifest
	// (a compaction rebuild's first request is naturally short — system +
	// summary + first instruction) clears any ratio on a single shared
	// message, and that shared message is often the very anchor the
	// SessKey itself was built from, so as evidence it carries no
	// information (the evidence loop: it shares BECAUSE it's the same
	// conversation's anchor, not because a compaction happened). Same
	// argument family as edit.go's spliceMinTailMatch = 2. Failing the
	// floor downgrades to AmbiguousMatch (the candidate stays visible for
	// human review); it never eliminates the candidate.
	stitchMinAbsOverlap = 3
	// stitchHeadPruneScore: the floor for stitching at all (Fork-origin
	// breaks, or a Contract that didn't clear stitchCompactionScore) —
	// below this, a real overlap is downgraded to AmbiguousMatch rather
	// than acted on.
	stitchHeadPruneScore = 0.15
	// stitchSameChatWindow: how close in time two lineages under the same
	// SessKey must be for a zero-overlap same_chat signal to be worth
	// flagging at all (still never auto-stitched).
	stitchSameChatWindow = 24 * time.Hour
	// stitchSameKeyMaxGap is the same-bucket counterpart of
	// stitchCrossBucketMaxGap. The old rule exempted same-SessKey
	// candidates from any gap limit because "a user can walk away for
	// days and resume the same anchor" — true for humans, but the traffic
	// that piles up under one anchor SessKey is mostly recurring
	// scheduled/heartbeat jobs: identical template openings, unrelated to
	// each other, spanning hundreds of hours. That is the same failure
	// mode that motivated stitchCrossBucketMaxGap (the 190+-hour
	// scheduled-task boilerplate matches), just inside the bucket where
	// the cross-bucket gate never applied. Candidates beyond this gap may
	// still WIN the search (they carry the strongest content evidence),
	// but a match they'd otherwise stitch is forced down to
	// AmbiguousMatch — downgraded, not eliminated: the genuinely-human
	// "walked away for days" case keeps its edge visible for review
	// instead of disappearing into NoPredecessorFound.
	stitchSameKeyMaxGap = 72 * time.Hour
	// stitchCrossBucketMaxGap bounds content-score matches ACROSS different
	// SessKeys only (same-SessKey candidates are bounded more leniently by
	// stitchSameKeyMaxGap). Cross-bucket is a
	// much bigger claim (two seemingly-different conversations are actually
	// one), and real corpus evidence shows a genuine
	// compaction link landing within tens of minutes, not days. Added after
	// an unbounded first pass on the 2026-07-14..28 corpus produced several
	// cross-bucket "matches" 190+ hours apart, all against recurring
	// scheduled-task boilerplate — high score from shared template text,
	// zero real relationship. "宁可断开，不要错连" applies here more
	// than anywhere else in this file.
	stitchCrossBucketMaxGap = 6 * time.Hour
)

// LineageIndex maps Lineage.Idx to *Lineage for ChainFrom lookups. Build
// once per Graph and reuse — a fresh map per lookup would be wasteful when
// resolving many chains (e.g. internal/story.ListCandidates over every
// candidate).
func LineageIndex(g *Graph) map[int]*Lineage {
	idx := make(map[int]*Lineage, len(g.Lineages))
	for _, l := range g.Lineages {
		idx[l.Idx] = l
	}
	return idx
}

// ChainFrom returns l's full stitched chain, oldest first — walking
// backward through Stitch.Edge.PredIdx while Outcome == Stitched. l itself
// is always the last element. A lineage with no stitched predecessor (the
// common case) returns the single-element chain []*Lineage{l} — Step 1's
// "one lineage, one Journey" behavior is the degenerate case of this, not a
// separate code path (internal/story.Build wraps a lone Lineage in a
// 1-element chain for exactly this reason).
func ChainFrom(l *Lineage, byIdx map[int]*Lineage) []*Lineage {
	chain := []*Lineage{l}
	cur := l
	for cur.Stitch != nil && cur.Stitch.Outcome == Stitched {
		pred := byIdx[cur.Stitch.Edge.PredIdx]
		if pred == nil {
			break
		}
		chain = append(chain, pred)
		cur = pred
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// StitchedSuccessorSet returns the set of lineage indices that have at
// least one OTHER lineage stitched onto them. internal/story.ListCandidates
// uses this to only offer chain TAILS as candidates — a lineage in this set
// is not its own candidate; its content is still reachable via whichever
// successor(s) claim it through ChainFrom.
func StitchedSuccessorSet(g *Graph) map[int]bool {
	out := map[int]bool{}
	for _, l := range g.Lineages {
		if l.Stitch != nil && l.Stitch.Outcome == Stitched {
			out[l.Stitch.Edge.PredIdx] = true
		}
	}
	return out
}

// StitchGraph resolves every lineage in g that broke away from its bucket
// to its best predecessor (or a documented non-match), writing the result
// into each Lineage's own Stitch field. Call once after Scan.
func StitchGraph(g *Graph) {
	blobLineages := buildBlobLineageIndex(g)
	sessBuckets := buildSessKeyIndex(g)
	byIdx := make(map[int]*Lineage, len(g.Lineages))
	for _, l := range g.Lineages {
		byIdx[l.Idx] = l
	}
	for _, l := range g.Lineages {
		if l.BrokeFrom == nil {
			l.Stitch = &StitchResolution{Outcome: NoBreak}
			continue
		}
		res := resolveStitch(l, byIdx, blobLineages, sessBuckets)
		l.Stitch = &res
	}
}

// buildSessKeyIndex buckets lineages by SessKey — the same one-index-per-
// graph pattern as buildBlobLineageIndex, so findSameChatCandidate scans
// one bucket instead of the whole graph (O(B×L) → O(bucket size)). Bucket
// order follows g.Lineages', which is deterministic; the candidate choice
// itself doesn't rely on it (explicit gap/Idx tie-break below).
func buildSessKeyIndex(g *Graph) map[string][]*Lineage {
	idx := make(map[string][]*Lineage)
	for _, l := range g.Lineages {
		idx[l.SessKey] = append(idx[l.SessKey], l)
	}
	return idx
}

// buildBlobLineageIndex maps every message hash seen anywhere in the graph
// to the lineages that ever carried it — the "blob 倒排索引" the design doc
// calls for, built once and reused across every lineage's search.
//
// The posting list is a []int, not a map[int]bool: on a real corpus this
// index holds millions of (hash, lineage) pairs, and one tiny map per hash
// (header + bucket array, ~100+ bytes even for a single entry) was a
// measured chunk of `vmr analyze`'s peak RSS. Dedup without a set: the outer
// loop finishes a whole lineage before moving to the next, so every append
// for a given l.Idx is contiguous — skipping when it already equals the
// slice tail is enough. Consumers only range over the list; order doesn't
// matter (resolveStitch builds its own overlap map).
func buildBlobLineageIndex(g *Graph) map[Hash][]int {
	idx := make(map[Hash][]int)
	for _, l := range g.Lineages {
		for _, m := range l.Manifests {
			for _, h := range m.Keys {
				if b := idx[h]; len(b) == 0 || b[len(b)-1] != l.Idx {
					idx[h] = append(b, l.Idx)
				}
			}
		}
	}
	return idx
}

// resolveStitch finds l's best-scoring temporally-preceding candidate via
// the blob index, then classifies the match.
func resolveStitch(l *Lineage, byIdx map[int]*Lineage, blobLineages map[Hash][]int, sessBuckets map[string][]*Lineage) StitchResolution {
	b0 := l.Manifests[0]

	// No early return for len(b0.Keys) == 0 (a broken-away lineage whose
	// opening manifest carries no content-hash keys, e.g. system-prompt-only)
	// — that used to short-circuit straight to NoPredecessorFound, skipping
	// findSameChatCandidate below even though that fallback needs no overlap
	// at all (same SessKey + time proximity only). An empty b0.Keys just
	// means the loop below never populates `overlap`, so bestIdx naturally
	// stays -1 and control falls through to the findSameChatCandidate call —
	// the same path a real "zero overlap found" case already takes.
	// Set-intersection semantics, not multiset: b0.Keys is the message
	// SEQUENCE, so a content hash that repeats in it (recurring heartbeat
	// prompts, identical tool results) must count once on both sides of
	// the score fraction — otherwise a duplicated hash inflates the
	// numerator (and the old multiset denominator together with it), and
	// when the predecessor itself repeats that message too, overlap could
	// even exceed len(b0.Keys) and push score past 1.0. blobLineages'
	// posting lists are already lineage-deduped by buildBlobLineageIndex
	// (documented there), so each (h, idx) pair contributes at most once.
	seen := make(map[Hash]bool, len(b0.Keys))
	overlap := map[int]int{}
	for _, h := range b0.Keys {
		if seen[h] {
			continue
		}
		seen[h] = true
		for _, idx := range blobLineages[h] {
			if idx != l.Idx {
				overlap[idx]++
			}
		}
	}
	distinct := len(seen)

	// bestIdx/bestScore/bestGap pick the winner deterministically despite
	// `overlap` being a map (Go randomizes map iteration order every run):
	// higher score wins outright; an exact score tie (real on this corpus —
	// several same_chat/compaction candidates share identical coverage)
	// falls back to the smaller time gap, then to the smaller Idx as a
	// final total-ordering tie-break. Without this, two runs over the same
	// input could pick different predecessors among equally-scored
	// candidates — silently violating "idempotent, same input -> same
	// output" (an explicit invariant) and, downstream,
	// story's content-addressed Journey ids. Caught by running StitchGraph
	// 5x over the real corpus and diffing PredIdx per lineage — not by any
	// unit test (a synthetic fixture is too small to ever produce a real
	// tie by chance).
	bestIdx, bestScore, bestN := -1, -1.0, 0
	var bestGap time.Duration
	bestOverGap := false
	for idx, n := range overlap {
		pred := byIdx[idx]
		if pred == nil || len(pred.Manifests) == 0 {
			continue
		}
		predEnd := pred.Manifests[len(pred.Manifests)-1].TS
		if !predEnd.Before(b0.TS) {
			continue // a predecessor must temporally precede the break
		}
		if pred.SessKey != l.SessKey && b0.TS.Sub(predEnd) > stitchCrossBucketMaxGap {
			continue // see stitchCrossBucketMaxGap's doc comment
		}
		score := float64(n) / float64(distinct)
		gap := b0.TS.Sub(predEnd)
		overGap := pred.SessKey == l.SessKey && gap > stitchSameKeyMaxGap
		switch {
		case bestIdx < 0, score > bestScore:
			bestScore, bestGap, bestIdx, bestN, bestOverGap = score, gap, idx, n, overGap
		case score == bestScore && (gap < bestGap || (gap == bestGap && idx < bestIdx)):
			bestGap, bestIdx, bestN, bestOverGap = gap, idx, n, overGap
		}
	}

	if bestIdx < 0 {
		if edge := findSameChatCandidate(l, sessBuckets); edge != nil {
			return StitchResolution{Outcome: AmbiguousMatch, Edge: edge}
		}
		return StitchResolution{Outcome: NoPredecessorFound}
	}

	res := StitchResolution{Outcome: AmbiguousMatch}
	switch {
	case l.BrokeFrom.Edit.Kind == Contract && bestScore >= stitchCompactionScore && bestN >= stitchMinAbsOverlap:
		res = StitchResolution{Outcome: Stitched, Edge: &StitchEdge{
			Kind: StitchCompaction, PredIdx: bestIdx, Score: bestScore, Confidence: 0.8,
		}}
	case bestScore >= stitchHeadPruneScore && bestN >= stitchMinAbsOverlap:
		res = StitchResolution{Outcome: Stitched, Edge: &StitchEdge{
			Kind: StitchHeadPrune, PredIdx: bestIdx, Score: bestScore, Confidence: 0.5,
		}}
	default:
		res = StitchResolution{Outcome: AmbiguousMatch, Edge: &StitchEdge{
			Kind: StitchSameChat, PredIdx: bestIdx, Score: bestScore, Confidence: 0.2,
		}}
	}
	// A same-bucket winner beyond stitchSameKeyMaxGap never produces a
	// Stitched outcome — see the constant's doc comment. The edge (its
	// content kind and score intact) is kept at same_chat's informational
	// confidence so a human can still follow the trail.
	if res.Outcome == Stitched && bestOverGap {
		res.Outcome = AmbiguousMatch
		res.Edge.Confidence = 0.2
	}
	return res
}

// findSameChatCandidate looks for the closest-in-time, same-SessKey,
// temporally-preceding lineage when content-overlap search found nothing
// at all — the last resort before declaring NoPredecessorFound. sessBuckets
// (buildSessKeyIndex) scopes the scan to l's own bucket.
func findSameChatCandidate(l *Lineage, sessBuckets map[string][]*Lineage) *StitchEdge {
	bestIdx := -1
	var bestGap time.Duration
	b0TS := l.Manifests[0].TS
	for _, pred := range sessBuckets[l.SessKey] {
		if pred.Idx == l.Idx || len(pred.Manifests) == 0 {
			continue
		}
		predEnd := pred.Manifests[len(pred.Manifests)-1].TS
		if !predEnd.Before(b0TS) {
			continue
		}
		gap := b0TS.Sub(predEnd)
		if gap > stitchSameChatWindow {
			continue
		}
		// Same determinism concern as resolveStitch's overlap loop above:
		// an exact gap tie needs an explicit smaller-Idx tie-break, not
		// "whichever this iteration order visits first".
		if bestIdx == -1 || gap < bestGap || (gap == bestGap && pred.Idx < bestIdx) {
			bestIdx, bestGap = pred.Idx, gap
		}
	}
	if bestIdx == -1 {
		return nil
	}
	return &StitchEdge{Kind: StitchSameChat, PredIdx: bestIdx, Score: 0, Confidence: 0.1}
}
