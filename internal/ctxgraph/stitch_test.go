// Ver 2026-07-29 21:30, by Sonnet 5

package ctxgraph

import (
	"testing"
	"time"

	"vmr/internal/audit"
)

// TestStitchGraph_CompactionCase reuses the exact real-corpus shape
// TestScan_AppendRunThenContractSplitsLineage already reproduces (design
// doc F6/A.3: s231 turn 20->21): a 5-turn append run, then a Contract whose
// opening keeps the same anchor. The two lineages share a SessKey (so Scan
// already splits them), and the successor's opening overlaps 100% with the
// predecessor's accumulated content — StitchGraph must reconnect them as
// StitchCompaction.
func TestStitchGraph_CompactionCase(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := sysMsg("You are a personal assistant.")
	u1 := userMsg("深入调研这个内存涨价这一波")

	var recs []audit.Record
	msgs := []map[string]any{sys, u1}
	for i := 0; i < 5; i++ {
		recs = append(recs, mkAuditRec(at(i), chatBody(msgs...)))
		msgs = append(msgs, assistantMsg("step reply"))
	}
	brokenSys := sysMsg("You are a personal assistant. [updated tool policy]")
	recs = append(recs, mkAuditRec(at(30), chatBody(brokenSys, u1, assistantMsg("post-break reply"))))

	path := writeJSONL(t, recs)
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2", len(g.Lineages))
	}
	StitchGraph(g)

	first, second := g.Lineages[0], g.Lineages[1]
	if first.Stitch == nil || first.Stitch.Outcome != NoBreak {
		t.Errorf("first lineage (bucket opener) should be NoBreak, got %+v", first.Stitch)
	}
	if second.Stitch == nil {
		t.Fatal("second lineage's Stitch should be set")
	}
	if second.Stitch.Outcome != Stitched {
		t.Fatalf("outcome = %v, want Stitched", second.Stitch.Outcome)
	}
	if second.Stitch.Edge.Kind != StitchCompaction {
		t.Errorf("kind = %v, want StitchCompaction", second.Stitch.Edge.Kind)
	}
	if second.Stitch.Edge.PredIdx != first.Idx {
		t.Errorf("predecessor = lineage %d, want %d (the append run)", second.Stitch.Edge.PredIdx, first.Idx)
	}
	if second.Stitch.Edge.Score < stitchCompactionScore {
		t.Errorf("score = %.2f, want >= %.2f", second.Stitch.Edge.Score, stitchCompactionScore)
	}
}

// TestStitchGraph_NoPredecessorFoundForGenuinelyNewLineage covers F4's
// "sometimes not-found is the correct answer": a Fork-origin break whose
// opening shares nothing at all with any earlier lineage, and no
// metadata.user_id to fall back on for a same_chat signal either.
func TestStitchGraph_NoPredecessorFoundForGenuinelyNewLineage(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := sysMsg("sys")
	// Bucket A: an anchor that has nothing to do with bucket B.
	recA := mkAuditRec(at(0), chatBody(sys, userMsg("anchor A opening"), assistantMsg("A reply")))
	// Bucket B: a totally unrelated anchor, later in time, sharing zero
	// content and no metadata.user_id — a genuine independent conversation.
	recB1 := mkAuditRec(at(10), chatBody(sys, userMsg("anchor B opening"), assistantMsg("B reply 1")))
	recB2 := mkAuditRec(at(11), chatBody(sys, userMsg("anchor B opening"), assistantMsg("B reply 1"), userMsg("B follow-up")))

	path := writeJSONL(t, []audit.Record{recA, recB1, recB2})
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2 (two independent SessKey buckets)", len(g.Lineages))
	}
	StitchGraph(g)

	for _, l := range g.Lineages {
		if l.BrokeFrom != nil {
			t.Fatalf("neither lineage should have BrokeFrom set (both are independent bucket openers) — test setup is wrong")
		}
		if l.Stitch.Outcome != NoBreak {
			t.Errorf("lineage %d: want NoBreak (bucket opener), got %v", l.Idx, l.Stitch.Outcome)
		}
	}
}

// TestStitchGraph_SameChatAmbiguousMatch covers the same_chat kind: two
// lineages share a stable metadata.user_id SessKey (so they're in the same
// bucket and Scan considers them connected by anchor), but their content
// shares nothing at all — the Fork edge that splits them leaves zero blob
// overlap for content-based scoring, so StitchGraph must fall back to the
// SessKey+time-proximity signal and flag it AmbiguousMatch (same_chat),
// never auto-stitch it.
func TestStitchGraph_SameChatAmbiguousMatch(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := sysMsg("sys")
	meta := func(uid string) map[string]any { return map[string]any{"user_id": uid} }

	body1 := chatBody(sys, userMsg("first sub-task entirely"), assistantMsg("reply 1"))
	body1["metadata"] = meta("session_shared")
	rec1 := mkAuditRec(at(0), body1)

	body2 := chatBody(sys, userMsg("completely different second sub-task"), assistantMsg("reply 2"))
	body2["metadata"] = meta("session_shared")
	rec2 := mkAuditRec(at(5), body2) // within stitchSameChatWindow of rec1

	path := writeJSONL(t, []audit.Record{rec1, rec2})
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2 (Fork split within the shared metadata SessKey bucket)", len(g.Lineages))
	}
	second := g.Lineages[1]
	if second.BrokeFrom == nil || second.BrokeFrom.Edit.Kind != Fork {
		t.Fatalf("second lineage should have broken via Fork, got %+v", second.BrokeFrom)
	}
	StitchGraph(g)

	if second.Stitch.Outcome != AmbiguousMatch {
		t.Fatalf("outcome = %v, want AmbiguousMatch", second.Stitch.Outcome)
	}
	if second.Stitch.Edge.Kind != StitchSameChat {
		t.Errorf("kind = %v, want StitchSameChat", second.Stitch.Edge.Kind)
	}
	if second.Stitch.Edge.Score != 0 {
		t.Errorf("same_chat match should carry Score=0 (no content overlap), got %.2f", second.Stitch.Edge.Score)
	}
}

// TestStitchGraph_HeadPruneCase covers the middle band: overlap exists and
// clears stitchHeadPruneScore but not stitchCompactionScore — a Fork-origin
// break with real (but partial) content continuity.
func TestStitchGraph_HeadPruneCase(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := sysMsg("sys")
	meta := func(uid string) map[string]any { return map[string]any{"user_id": uid} }

	shared := userMsg("shared context message that survives")
	body1 := chatBody(sys, userMsg("predecessor opening"), assistantMsg("predecessor reply"), shared)
	body1["metadata"] = meta("session_hp")
	rec1 := mkAuditRec(at(0), body1)

	// Successor's opening: 1 of 6 keys (the shared message) overlaps with
	// the predecessor -> score = 1/6 ≈ 0.17, clears stitchHeadPruneScore
	// (0.15) but not stitchCompactionScore (0.5).
	body2 := chatBody(sys, shared,
		userMsg("brand new sub-task A"), assistantMsg("ack A"),
		userMsg("brand new sub-task B"), assistantMsg("ack B"))
	body2["metadata"] = meta("session_hp")
	rec2 := mkAuditRec(at(5), body2)

	path := writeJSONL(t, []audit.Record{rec1, rec2})
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2", len(g.Lineages))
	}
	second := g.Lineages[1]
	if second.BrokeFrom == nil {
		t.Fatalf("second lineage should have broken away")
	}
	StitchGraph(g)

	if second.Stitch.Outcome != Stitched {
		t.Fatalf("outcome = %v, want Stitched", second.Stitch.Outcome)
	}
	if second.Stitch.Edge.Kind != StitchHeadPrune {
		t.Errorf("kind = %v, want StitchHeadPrune (score=%.3f)", second.Stitch.Edge.Kind, second.Stitch.Edge.Score)
	}
	if second.Stitch.Edge.Score < stitchHeadPruneScore || second.Stitch.Edge.Score >= stitchCompactionScore {
		t.Errorf("score = %.3f, want in [%.2f, %.2f)", second.Stitch.Edge.Score, stitchHeadPruneScore, stitchCompactionScore)
	}
}

// TestStitchGraph_CrossBucketMatchRejectedBeyondMaxGap covers the fix for a
// real finding on the 2026-07-14..28 corpus: several successors matched a
// high-scoring but 190+-hour-old predecessor under a DIFFERENT SessKey —
// shared scheduled-task boilerplate, not a real relationship. A
// cross-bucket candidate beyond stitchCrossBucketMaxGap must be rejected
// even with a very high score, falling through to same_chat/no-match
// instead of a false StitchHeadPrune.
func TestStitchGraph_CrossBucketMatchRejectedBeyondMaxGap(t *testing.T) {
	sys := sysMsg("sys")
	shared := userMsg("recurring scheduled-task boilerplate that repeats verbatim every day")

	// Predecessor: a totally different SessKey (different anchor), far in
	// the past — well beyond stitchCrossBucketMaxGap.
	predBody := chatBody(sys, shared, assistantMsg("old reply"))
	predRec := mkAuditRec(time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC), predBody)

	// A same-bucket opener for the successor's own SessKey, so the break is
	// structural (Fork), far enough in time from the cross-bucket
	// candidate but recent relative to the successor.
	openerBody := chatBody(sys, userMsg("today's distinct opening"), assistantMsg("today's reply"))
	openerRec := mkAuditRec(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC), openerBody)

	// Successor: high overlap with the week-old predecessor (the shared
	// boilerplate message), but forked from today's own opener.
	succBody := chatBody(sys, shared, userMsg("brand new content A"), userMsg("brand new content B"))
	succRec := mkAuditRec(time.Date(2026, 7, 20, 8, 5, 0, 0, time.UTC), succBody)

	path := writeJSONL(t, []audit.Record{predRec, openerRec, succRec})
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 3 {
		t.Fatalf("got %d lineages, want 3 (unrelated predecessor + today's opener + today's Fork successor)", len(g.Lineages))
	}
	byIdx := map[int]*Lineage{}
	for _, l := range g.Lineages {
		byIdx[l.Idx] = l
	}
	StitchGraph(g)

	var succ *Lineage
	for _, l := range g.Lineages {
		if l.BrokeFrom != nil {
			succ = l
		}
	}
	if succ == nil {
		t.Fatal("no lineage broke away — test setup didn't produce a Fork as expected")
	}
	if succ.Stitch.Outcome == Stitched {
		predLineage := byIdx[succ.Stitch.Edge.PredIdx]
		if predLineage.SessKey != succ.SessKey {
			gap := succ.Manifests[0].TS.Sub(predLineage.Manifests[len(predLineage.Manifests)-1].TS)
			t.Fatalf("stitched to cross-bucket predecessor %.1fh away (max allowed %v) — the week-old boilerplate match should have been rejected",
				gap.Hours(), stitchCrossBucketMaxGap)
		}
	}
}

// TestChainFrom_CompactionCase covers the chain-resolution helpers using
// the same compaction fixture TestStitchGraph_CompactionCase uses.
func TestChainFrom_CompactionCase(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, time.UTC) }
	sys := sysMsg("sys")
	u1 := userMsg("深入调研这个内存涨价这一波")
	var recs []audit.Record
	msgs := []map[string]any{sys, u1}
	for i := 0; i < 5; i++ {
		recs = append(recs, mkAuditRec(at(i), chatBody(msgs...)))
		msgs = append(msgs, assistantMsg("step reply"))
	}
	recs = append(recs, mkAuditRec(at(30), chatBody(sysMsg("sys v2"), u1, assistantMsg("post-break reply"))))

	path := writeJSONL(t, recs)
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	StitchGraph(g)
	byIdx := LineageIndex(g)

	first, second := g.Lineages[0], g.Lineages[1]

	chain := ChainFrom(second, byIdx)
	if len(chain) != 2 {
		t.Fatalf("ChainFrom(second) length = %d, want 2", len(chain))
	}
	if chain[0].Idx != first.Idx || chain[1].Idx != second.Idx {
		t.Errorf("chain order = [%d, %d], want [%d, %d] (oldest first)", chain[0].Idx, chain[1].Idx, first.Idx, second.Idx)
	}

	soloChain := ChainFrom(first, byIdx)
	if len(soloChain) != 1 || soloChain[0].Idx != first.Idx {
		t.Errorf("ChainFrom(first) should be the 1-element chain [first], got %v", soloChain)
	}

	successors := StitchedSuccessorSet(g)
	if !successors[first.Idx] {
		t.Error("first lineage should be in StitchedSuccessorSet (second stitches onto it)")
	}
	if successors[second.Idx] {
		t.Error("second lineage should NOT be in StitchedSuccessorSet (nothing stitches onto it)")
	}
}

// TestStitchGraph_TiedScoreCandidatesPickDeterministicWinner is a
// regression test for a real bug found by running StitchGraph repeatedly
// over the 2026-07-14..28 corpus and diffing PredIdx per lineage: several
// lineages picked a DIFFERENT (but equally-scored) predecessor on different
// runs, because resolveStitch's `overlap` is a map and Go randomizes map
// iteration order — a bare `score > bestScore` has no defined winner on an
// exact tie. Two independent, unrelated buckets ("closer"/"farther") each
// mention the same two shared messages the successor's opening does,
// giving them IDENTICAL content-overlap scores (0.5, both clearing the
// bucket's own natural opener's much weaker anchor-only overlap of 0.25);
// only their distance in time to the break differs. The fix adds an
// explicit gap-then-Idx tie-break — this test locks in that "closer"
// always wins, across many repeated Scan+StitchGraph calls on the exact
// same input.
func TestStitchGraph_TiedScoreCandidatesPickDeterministicWinner(t *testing.T) {
	sys := sysMsg("sys")
	shared1 := userMsg("SHARED_ONE unique content")
	shared2 := userMsg("SHARED_TWO unique content")

	// Defines the successor's own bucket anchor — a much weaker (0.25)
	// competing candidate the tie-break must NOT prefer just for being
	// nearby in time.
	openerRec := mkAuditRec(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC),
		chatBody(sys, userMsg("today's opening anchor"), assistantMsg("today's reply")))
	closerRec := mkAuditRec(time.Date(2026, 7, 20, 7, 50, 0, 0, time.UTC),
		chatBody(sys, userMsg("closer bucket anchor"), shared1, shared2))
	fartherRec := mkAuditRec(time.Date(2026, 7, 20, 7, 0, 0, 0, time.UTC),
		chatBody(sys, userMsg("farther bucket anchor"), shared1, shared2))
	// Same bucket as openerRec (shares its first message) but forks away
	// (low coverage) — BrokeFrom gets set, triggering resolveStitch.
	succRec := mkAuditRec(time.Date(2026, 7, 20, 8, 5, 0, 0, time.UTC),
		chatBody(sys, userMsg("today's opening anchor"), shared1, shared2, userMsg("brand new content A")))

	path := writeJSONL(t, []audit.Record{openerRec, closerRec, fartherRec, succRec})

	wantPred := -1
	for i := 0; i < 8; i++ {
		g, err := Scan([]string{path})
		if err != nil {
			t.Fatalf("run %d: Scan: %v", i, err)
		}
		if len(g.Lineages) != 4 {
			t.Fatalf("run %d: got %d lineages, want 4", i, len(g.Lineages))
		}
		StitchGraph(g)

		var closer, succ *Lineage
		for _, l := range g.Lineages {
			if len(l.Manifests) > 0 && l.Manifests[0].TS.Equal(closerRec.TS) {
				closer = l
			}
			if l.BrokeFrom != nil {
				succ = l
			}
		}
		if closer == nil || succ == nil {
			t.Fatalf("run %d: test setup didn't produce the expected lineages", i)
		}
		if succ.Stitch.Outcome != Stitched {
			t.Fatalf("run %d: outcome = %v, want Stitched", i, succ.Stitch.Outcome)
		}
		if i == 0 {
			wantPred = succ.Stitch.Edge.PredIdx
			if wantPred != closer.Idx {
				t.Fatalf("run %d: picked predecessor %d, want the closer-in-time candidate %d (tie-break rule)", i, wantPred, closer.Idx)
			}
			continue
		}
		if succ.Stitch.Edge.PredIdx != wantPred {
			t.Fatalf("run %d: predecessor changed from %d to %d across repeated Scan+StitchGraph calls on the SAME input — non-deterministic tie-break", i, wantPred, succ.Stitch.Edge.PredIdx)
		}
	}
}

func TestStitchKind_String(t *testing.T) {
	cases := map[StitchKind]string{StitchCompaction: "compaction", StitchHeadPrune: "head_prune", StitchSameChat: "same_chat"}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", k, got, want)
		}
	}
}

func TestStitchOutcome_String(t *testing.T) {
	cases := map[StitchOutcome]string{
		NoBreak: "no_break", Stitched: "stitched",
		NoPredecessorFound: "no_predecessor_found", AmbiguousMatch: "ambiguous_match",
	}
	for o, want := range cases {
		if got := o.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", o, got, want)
		}
	}
}

// TestResolveStitch_EmptyOpeningKeysStillTriesSameChat is a regression test:
// resolveStitch used to return NoPredecessorFound immediately whenever the
// broken-away lineage's opening manifest had zero content-hash Keys (e.g. a
// system-prompt-only request), skipping findSameChatCandidate entirely even
// though that fallback needs no key overlap at all — only a shared SessKey
// and time proximity. A same-SessKey predecessor ending well within
// stitchSameChatWindow of l's start must still surface as AmbiguousMatch/
// StitchSameChat, not get silently downgraded to "no predecessor found".
func TestResolveStitch_EmptyOpeningKeysStillTriesSameChat(t *testing.T) {
	predEnd := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	pred := &Lineage{
		Idx: 0, SessKey: "s1",
		Manifests: []*Manifest{{TS: predEnd, Keys: []Hash{{1}}}},
	}
	l := &Lineage{
		Idx: 1, SessKey: "s1",
		BrokeFrom: &BreakInfo{Edit: Edit{Kind: Fork}},
		// Keys is nil/empty — an opening manifest with no non-system
		// message content at all (e.g. system-only), the exact case the old
		// early return mishandled.
		Manifests: []*Manifest{{TS: predEnd.Add(5 * time.Minute)}},
	}
	byIdx := map[int]*Lineage{0: pred, 1: l}

	res := resolveStitch(l, byIdx, map[Hash]map[int]bool{})

	if res.Outcome != AmbiguousMatch {
		t.Fatalf("Outcome = %v, want AmbiguousMatch (empty Keys must still fall through to findSameChatCandidate)", res.Outcome)
	}
	if res.Edge == nil || res.Edge.Kind != StitchSameChat || res.Edge.PredIdx != 0 {
		t.Errorf("Edge = %+v, want {Kind: StitchSameChat, PredIdx: 0}", res.Edge)
	}
}
