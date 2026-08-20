// Ver 2026-07-29 21:30, by Sonnet 5

package ctxgraph

import (
	"crypto/md5"
	"encoding/binary"
)

// Lineage is a maximal run of manifests connected by non-splitting edits
// (Append/ReplaceTail/Splice) within one SessKey bucket. It is NOT itself a
// "Journey" in the design doc's sense — a Lineage is deliberately just the
// structural, zero-inference unit; stitching lineages back together across
// a Contract/Fork break (this package's stitch.go) is a
// separate, later pass over the whole Graph, and internal/story.Journey is
// what actually chains stitched lineages into one narrative.
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
	// fresh conversation": the corpus's input window itself can truncate a
	// lineage's true head (default: skip such journeys, see
	// internal/story).
	BrokeFrom *BreakInfo

	// Stitch is nil until StitchGraph (stitch.go) runs — a separate pass
	// over the whole Graph, after Scan has built every lineage, since
	// finding a lineage's best predecessor needs the full blob inverted
	// index across ALL lineages, not just its own bucket. Only meaningful
	// when BrokeFrom != nil; StitchGraph leaves it nil for a bucket's first
	// lineage (nothing to stitch).
	Stitch *StitchResolution
}

// BreakInfo is the structural evidence for why a lineage started mid-bucket
// instead of continuing the previous one. Phase 2 turns this into a
// labeled, evidenced stitching edge (compaction /
// head_prune / same_chat) and a confidence score; Step 1 only needs enough
// to render "⚠ context was rebuilt/replaced here" without over-claiming why.
type BreakInfo struct {
	Edit     Edit
	PrevTail *Manifest // last manifest of the lineage this one broke from
}

// RootHash identifies this lineage by hashing its ENTIRE first manifest
// (system hash + every message key, in order) — content-addressed, so the
// same lineage gets the same id across runs regardless of which other
// files were also loaded. internal/story's Journey id
// leads with client tag + start/end timestamps for sortability and only
// uses a short prefix of this hash as a trailing disambiguator — RootHash
// itself stays the full-strength identity check.
//
// Deliberately not just Keys[0] (the opening message alone): a Contract
// edit very often preserves the exact opening user instruction verbatim
// (the real s231 case does exactly this: the same first user message
// survives a mid-conversation history rebuild), so two
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

// lineageIDCodeLen mirrors internal/story's idCodeLen (deriveID) — same
// 8-hex-char prefix convention, kept as an independent constant here since
// ctxgraph is a leaf package and must not depend on story for it.
const lineageIDCodeLen = 8

// LineageID is l's stable identity: "l-" plus an 8-hex-char prefix of a
// hash over the root manifest's content AND its own arrival timestamp. A
// Lineage IS the structural unit report's SessionInfo and story's Journey
// each already treat as "one session" (report: one SessionInfo per
// Lineage; story: a Journey is a chain of these) — this gives that unit
// the single identity both halves should reference instead of each
// inventing their own (report's run-scoped s%02d, story's own per-Journey
// hash).
//
// Deliberately NOT just RootHash's own prefix (an earlier version of this
// function was): RootHash is a pure content fingerprint by design — two
// structurally distinct Lineages with byte-identical opening content
// (same system prompt, same first user message) hash the same. That is
// not hypothetical: a real corpus scan (1638 Lineages) found 4 such
// collisions, all recurring cron/heartbeat jobs firing a fixed message
// template. internal/story's own deriveID already defends against exactly
// this by folding in client+start+end alongside a RootHash-derived code
// (see its own doc comment); a caller here that used RootHash bare would
// have been relying on a guarantee RootHash never claimed to make. Folding
// in the root manifest's own arrival timestamp (nanosecond precision — two
// independent Lineages sharing both identical opening content AND the
// exact same nanosecond is not a realistic collision to defend against)
// closes the gap while keeping the id itself content-addressed and stable
// across independent re-scans of the same data — RootHash's identity
// semantics for content comparison (Classify/edit-detection) are
// untouched, only this id's own hash input changed.
func (l *Lineage) LineageID() string {
	if len(l.Manifests) == 0 {
		var zero Hash
		return "l-" + zero.String()[:lineageIDCodeLen]
	}
	root := l.Manifests[0]
	h := md5.New()
	if root.HasSys {
		h.Write(root.SysHash[:])
	}
	for _, k := range root.Keys {
		h.Write(k[:])
	}
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(root.TS.UnixNano()))
	h.Write(tsBuf[:])
	var out Hash
	copy(out[:], h.Sum(nil))
	return "l-" + out.String()[:lineageIDCodeLen]
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
