// Ver 2026-07-29 15:00, by Sonnet 5

package ctxgraph

import "crypto/md5"

// Lineage is a maximal run of manifests connected by non-splitting edits
// (Append/ReplaceTail) within one SessKey bucket. It is NOT yet a "Journey"
// in the design doc's sense — stitching lineages back together across a
// Contract/Fork break (design doc §6.2's compaction/head_prune/same_chat
// edge classification) is Phase 2 work; a Lineage here is deliberately just
// the structural, zero-inference unit.
type Lineage struct {
	// Idx is this package's own bookkeeping order (assignment order within
	// Scan), not a stable cross-run identifier — internal/story derives its
	// user-facing, content-addressed Journey id from RootHash().
	Idx       int
	SessKey   string
	Manifests []*Manifest
	// Edges[i] is Classify(Manifests[i], Manifests[i+1]); len(Edges) ==
	// len(Manifests)-1. Every edge here is non-splitting by construction —
	// a splitting edge (Contract/Fork) is where the PREVIOUS lineage ends
	// and this one (or a sibling) begins, recorded in BrokeFrom instead.
	Edges []Edit

	// BrokeFrom is non-nil when this lineage did not open the SessKey
	// bucket — i.e., it started because the previous manifest in the same
	// bucket triggered a Contract/Fork edit. nil for a bucket's first
	// lineage (nothing preceded it) — that is NOT the same as "definitely a
	// fresh conversation": design doc F8 notes the corpus's input window
	// itself can truncate a lineage's true head (D1's decision: default to
	// skipping such journeys, see internal/story).
	BrokeFrom *BreakInfo
}

// BreakInfo is the structural evidence for why a lineage started mid-bucket
// instead of continuing the previous one. Phase 2 (design doc Appendix C.4)
// turns this into a labeled, evidenced stitching edge (compaction /
// head_prune / same_chat) and a confidence score; Step 1 only needs enough
// to render "⚠ context was rebuilt/replaced here" without over-claiming why.
type BreakInfo struct {
	Edit     Edit
	PrevTail *Manifest // last manifest of the lineage this one broke from
}

// RootHash identifies this lineage by hashing its ENTIRE first manifest
// (system hash + every message key, in order) — content-addressed, so the
// same lineage gets the same id across runs regardless of which other
// files were also loaded (design doc §11 D1). internal/story's Journey id
// leads with client tag + start/end timestamps for sortability and only
// uses a short prefix of this hash as a trailing disambiguator — RootHash
// itself stays the full-strength identity check.
//
// Deliberately not just Keys[0] (the opening message alone): a Contract
// edit very often preserves the exact opening user instruction verbatim
// (design doc F6 — the real s231 case does exactly this: the same first
// user message survives a mid-conversation history rebuild), so two
// genuinely different lineages under the same SessKey bucket can share
// Keys[0] while differing in everything else about their root manifest.
// Hashing the whole key vector instead means they only collide when their
// entire first request was identical — which is the correct notion of
// "the same thing" for a content-addressed id.
//
// Zero value (all-zero Hash) when the root manifest has neither a system
// block nor any non-system messages (a degenerate, essentially-empty
// request) — callers should treat that as "unidentifiable" rather than a
// real id.
func (l *Lineage) RootHash() Hash {
	if len(l.Manifests) == 0 {
		return Hash{}
	}
	root := l.Manifests[0]
	if !root.HasSys && len(root.Keys) == 0 {
		return Hash{}
	}
	h := md5.New()
	if root.HasSys {
		h.Write(root.SysHash[:])
	}
	for _, k := range root.Keys {
		h.Write(k[:])
	}
	var out Hash
	copy(out[:], h.Sum(nil))
	return out
}

// splitBucket takes one SessKey bucket's manifests, already in timestamp
// order, and partitions them into lineages at every splitting edit
// (Contract/Fork). ms must be non-empty.
func splitBucket(sessKey string, ms []*Manifest) []*Lineage {
	var out []*Lineage
	cur := &Lineage{SessKey: sessKey, Manifests: []*Manifest{ms[0]}}
	out = append(out, cur)
	for i := 1; i < len(ms); i++ {
		prev, next := ms[i-1], ms[i]
		e := Classify(prev, next)
		if e.Kind.Splits() {
			cur = &Lineage{
				SessKey:   sessKey,
				Manifests: []*Manifest{next},
				BrokeFrom: &BreakInfo{Edit: e, PrevTail: prev},
			}
			out = append(out, cur)
			continue
		}
		cur.Edges = append(cur.Edges, e)
		cur.Manifests = append(cur.Manifests, next)
	}
	return out
}
