// Ver 2026-09-02 00:00, by pi-agent

package report

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

// TestGroup_CompactionRecordGetsManifestAndPrevManifest pins R72(b): the
// group() function must set r.manifest and r.prevManifest on compaction
// records too (not just session-attached records), and a.Compactions must
// still contain them — the report-only body-sniffed treatment is
// preserved alongside the lineage-level manifest correlation. The fixture
// is one bucket (shared metadata.user_id) holding a single lineage whose
// manifest sequence is [r1, compaction, r2] via pure Appends, so the
// compaction's lineage-direct predecessor is r1's manifest and r2's is the
// compaction's — exactly the (m, prev) internal/story's Step/PrevManifest
// pair would carry for the same records.
func TestGroup_CompactionRecordGetsManifestAndPrevManifest(t *testing.T) {
	zone := time.FixedZone("CST", 8*3600)
	at := func(min, sec int) time.Time { return time.Date(2026, 7, 9, 10, min, sec, 0, zone) }
	sys := msg("system", "You are a personal assistant.")
	u1 := msg("user", "first instruction")
	a1 := msg("assistant", "ok")
	feed := msg("user", "The conversation history before this point was compacted.")

	// r1: ordinary opening turn.
	r1 := mkRec(at(0, 0), "trace1", []any{sys, u1, a1}, nil, sseText("done"))

	// compaction call in the same bucket/lineage: same opening messages
	// (Append), summarization system prompt, no tools, max_completion_tokens.
	comp := mkRec(at(2, 0), "", []any{
		msg("system", "You are a context summarization assistant. Summarize."),
		u1, a1, feed,
	}, nil, sseText("summary output"))
	comp.Client.Request.Body.(map[string]any)["max_completion_tokens"] = 16000
	comp.Client.Request.Body.(map[string]any)["metadata"] = map[string]any{"user_id": "session_comp"}

	// r2: continuation after compaction, same bucket, Appends onto comp's
	// messages.
	r2 := mkRec(at(3, 0), "trace2", []any{sys, u1, a1, feed, msg("user", "continue")}, nil, sseText("done"))

	// Give all three the same metadata.user_id so they share one SessKey
	// bucket (r1's mkRec has no metadata yet).
	meta := map[string]any{"user_id": "session_comp"}
	r1.Client.Request.Body.(map[string]any)["metadata"] = meta
	r2.Client.Request.Body.(map[string]any)["metadata"] = meta

	path := writeJSONL(t, []audit.Record{r1, comp, r2})
	a, err := AnalyzeSessions([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	if len(a.Compactions) != 1 {
		t.Fatalf("a.Compactions = %d, want 1 (the compaction record)", len(a.Compactions))
	}
	c := a.Compactions[0]
	if c.manifest == nil {
		t.Error("compaction record's .manifest is nil — group() must set it to kill R72's divergence")
	}
	if c.prevManifest == nil || c.prevManifest.Line != 1 {
		t.Errorf("compaction record's .prevManifest = %+v, want the lineage-direct predecessor (r1 at line 1)", c.prevManifest)
	}

	if len(a.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1 (one lineage, compaction excluded from grouping)", len(a.Sessions))
	}
	s := a.Sessions[0]
	if len(s.Recs) != 2 {
		t.Fatalf("session records = %d, want 2 (r1 and r2; compaction stays out of s.Recs)", len(s.Recs))
	}
	// r2 follows the compaction record in the lineage, so its prev is the
	// compaction's manifest — the attach() Parent chain would have skipped
	// the compaction entirely; prevManifest must not.
	last := s.Recs[len(s.Recs)-1]
	if last.prevManifest == nil || last.prevManifest.Line != 2 {
		t.Errorf("r2's .prevManifest = %+v, want the compaction record's manifest (line 2)", last.prevManifest)
	}
}

// TestDetailJob_ManifestsForPrefersLineagePrev pins detail.go's
// manifestsFor: the lineage-derived prevManifest wins over the Parent
// chain (which skips compaction records), so both commands hand reqdetail
// the same (m, prev) pair.
func TestDetailJob_ManifestsForPrefersLineagePrev(t *testing.T) {
	parentM := &ctxgraph.Manifest{Req: "a.jsonl:1"}
	lineagePrev := &ctxgraph.Manifest{Req: "a.jsonl:2"}
	own := &ctxgraph.Manifest{Req: "a.jsonl:3"}
	info := &ReqInfo{
		manifest:     own,
		prevManifest: lineagePrev,
		Parent:       &ReqInfo{manifest: parentM},
	}
	m, prev := (detailJob{info: info}).manifestsFor()
	if m != own || prev != lineagePrev {
		t.Errorf("manifestsFor = (%p, %p), want (%p, %p)", m, prev, own, lineagePrev)
	}
	// Nil prevManifest (lineage head) stays nil regardless of Parent.
	info2 := &ReqInfo{manifest: own, Parent: &ReqInfo{manifest: parentM}}
	if _, prev2 := (detailJob{info: info2}).manifestsFor(); prev2 != nil {
		t.Errorf("nil prevManifest should give nil prev even with Parent, got %p", prev2)
	}
	if _, prev3 := (detailJob{info: &ReqInfo{manifest: own}}).manifestsFor(); prev3 != nil {
		t.Errorf("nil prevManifest and nil Parent should give nil prev, got %p", prev3)
	}
}
