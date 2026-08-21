// Ver 2026-07-28 22:40, by Sonnet 5

package ctxgraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/audit"
)

// writeJSONL writes recs to a fresh temp file and returns its path. The
// basename is derived from t.TempDir()'s own unique suffix (not a fixed
// "audit.jsonl") — real audit files always carry a date and never collide
// on basename (see reqcoord.go's CheckPathCollisions), and a fixed name
// would make two calls within the same test collide on purpose, which
// defeats any test that scans multiple files at once.
func writeJSONL(t *testing.T, recs []audit.Record) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "audit-"+filepath.Base(dir)+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range recs {
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func chatBody(msgs ...map[string]any) map[string]any {
	arr := make([]any, len(msgs))
	for i, m := range msgs {
		arr[i] = m
	}
	return map[string]any{"messages": arr}
}

func sysMsg(text string) map[string]any  { return map[string]any{"role": "system", "content": text} }
func userMsg(text string) map[string]any { return map[string]any{"role": "user", "content": text} }
func assistantMsg(text string) map[string]any {
	return map[string]any{"role": "assistant", "content": text}
}

// TestScan_AppendRunThenContractSplitsLineage reproduces the real corpus
// case (session s231, turn 20->21): a session
// that grows turn by turn via pure appends, then a request whose message
// list collapses to a handful of messages that still opens with the exact
// same user instruction (the survives-a-compaction shape) — this must split
// into two lineages, not stay merged under one anchor.
func TestScan_AppendRunThenContractSplitsLineage(t *testing.T) {
	t.Parallel()
	zone := time.UTC
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 15, m, 0, 0, zone) }

	sys := sysMsg("You are a personal assistant.")
	u1 := userMsg("深入调研这个内存涨价这一波")

	var recs []audit.Record
	// Turns 1..5: append-only growth (mirrors turn 1-20 in the real s231).
	msgs := []map[string]any{sys, u1}
	for i := 0; i < 5; i++ {
		recs = append(recs, mkAuditRec(at(i), chatBody(msgs...)))
		msgs = append(msgs, assistantMsg("step reply"))
	}
	// The break: system prompt AND most history changed, but the same
	// opening user instruction survives verbatim (real observed shape).
	brokenSys := sysMsg("You are a personal assistant. [updated tool policy]")
	recs = append(recs, mkAuditRec(at(30), chatBody(brokenSys, u1, assistantMsg("post-break reply"))))
	// One more turn continuing the new (post-break) lineage via append.
	recs = append(recs, mkAuditRec(at(31), chatBody(brokenSys, u1, assistantMsg("post-break reply"), userMsg("follow up"))))

	path := writeJSONL(t, recs)
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2 (append run + post-contract continuation)", len(g.Lineages))
	}
	first, second := g.Lineages[0], g.Lineages[1]
	if len(first.Manifests) != 5 {
		t.Errorf("first lineage has %d manifests, want 5", len(first.Manifests))
	}
	for i, e := range first.Edges {
		if e.Kind != Append {
			t.Errorf("first lineage edge %d = %v, want Append", i, e.Kind)
		}
	}
	if second.BrokeFrom == nil {
		t.Fatal("second lineage should have BrokeFrom set")
	}
	if second.BrokeFrom.Edit.Kind != Contract {
		t.Errorf("break edit kind = %v, want Contract", second.BrokeFrom.Edit.Kind)
	}
	if len(second.Manifests) != 2 {
		t.Errorf("second lineage has %d manifests, want 2", len(second.Manifests))
	}
	if len(second.Edges) != 1 || second.Edges[0].Kind != Append {
		t.Errorf("second lineage's internal edge should be Append, got %+v", second.Edges)
	}
	// Both lineages share the same SessKey (same anchor: same opening
	// message survived) — this is exactly the "anchor glues two lineages
	// together" case the split logic exists to correct.
	if first.SessKey != second.SessKey {
		t.Errorf("expected same SessKey bucket, got %q vs %q", first.SessKey, second.SessKey)
	}
	// Both lineages' root manifests open with the exact same user
	// instruction (u1) — the real-world shape RootHash is specifically
	// designed to still tell apart (see RootHash's doc comment): first's
	// root manifest is [sys, u1] (1 key), second's is [brokenSys, u1,
	// reply] (2 keys + a different system hash) — different root
	// manifests as a whole, so RootHash must differ even though Keys[0]
	// alone would not have.
	if first.RootHash() == second.RootHash() {
		t.Error("RootHash should differ: root manifests differ even though both open with the same instruction")
	}
	if first.Manifests[0].Keys[0] != second.Manifests[0].Keys[0] {
		t.Fatal("test setup invariant broken: both roots should share the same opening message hash")
	}
	// LineageID is a thin "l-" + RootHash prefix wrapper — first/second
	// already proved RootHash differs for these two lineages above, so
	// this is really asserting the wrapper doesn't accidentally collapse
	// that distinction (e.g. by truncating to a length short enough to
	// collide) and that the format is what callers (report's SessionInfo,
	// story's JourneyIndexRow) will depend on.
	if first.LineageID() == second.LineageID() {
		t.Error("LineageID should differ: it wraps RootHash, which already differs for these two lineages")
	}
	if got, want := first.LineageID()[:2], "l-"; got != want {
		t.Errorf("LineageID prefix = %q, want %q", got, want)
	}
	if got, want := len(first.LineageID()), len("l-")+lineageIDCodeLen; got != want {
		t.Errorf("LineageID length = %d, want %d", got, want)
	}
}

// TestLineageID_ContentAddressed proves the id is a pure function of the
// root manifest's content (SysHash/Keys), independent of everything else
// on the Lineage (Idx, SessKey, later manifests) — the property report's
// SessionInfo.ID and story's JourneyIndexRow.Lineages both need: the same
// underlying conversation must resolve to the same id across independent
// scans/subsets, not just within one run's Idx assignment order.
func TestLineageID_ContentAddressed(t *testing.T) {
	t.Parallel()
	ts0 := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	mkLineage := func(sysHash Hash, hasSys bool, ts time.Time, keys ...Hash) *Lineage {
		return &Lineage{
			Idx:       99, // deliberately not 0/1 — id must not depend on scan order
			SessKey:   "irrelevant-bucket",
			Manifests: []*Manifest{{SysHash: sysHash, HasSys: hasSys, TS: ts, Keys: keys}},
		}
	}
	h1 := Hash{1, 2, 3}
	h2 := Hash{9, 9, 9}
	a := mkLineage(h1, true, ts0, h2)
	b := mkLineage(h1, true, ts0, h2) // same root content AND same ts, different Lineage value
	c := mkLineage(h2, true, ts0, h1) // different root content

	if a.LineageID() != b.LineageID() {
		t.Errorf("same root content and ts should yield the same LineageID: %q vs %q", a.LineageID(), b.LineageID())
	}
	if a.LineageID() == c.LineageID() {
		t.Error("different root content should yield different LineageIDs")
	}

	// The confirmed real-corpus failure mode this fix closes: two
	// structurally distinct Lineages (a recurring cron/heartbeat job)
	// with BYTE-IDENTICAL opening content (same sys hash, same first
	// message keys) but different arrival times must NOT collide — a
	// real 1638-Lineage corpus scan found 4 such collisions when this id
	// was computed from RootHash alone (see this method's own doc
	// comment). report's session-grouping maps are keyed by this string;
	// a collision there silently merges two unrelated sessions.
	sameContentLaterTS := mkLineage(h1, true, ts0.Add(5*time.Minute), h2)
	if a.LineageID() == sameContentLaterTS.LineageID() {
		t.Error("identical opening content at a different arrival time must still yield a different LineageID (this is the real collision this method exists to prevent)")
	}

	// Degenerate case: a root manifest with neither HasSys nor Keys
	// (RootHash's own doc comment calls this out as a case RootHash
	// itself collapses to the zero Hash) must still produce a
	// well-formed, non-panicking, deterministic id — not treated as an
	// error, and no longer colliding with every other empty-root Lineage
	// regardless of when it occurred, since ts still feeds the hash.
	empty1 := mkLineage(Hash{}, false, ts0)
	empty2 := mkLineage(Hash{}, false, ts0)
	emptyLaterTS := mkLineage(Hash{}, false, ts0.Add(time.Hour))
	if empty1.LineageID() != empty2.LineageID() {
		t.Errorf("two empty-root Lineages at the same ts should still agree: %q vs %q", empty1.LineageID(), empty2.LineageID())
	}
	if empty1.LineageID() == emptyLaterTS.LineageID() {
		t.Error("two empty-root Lineages at different ts must not collide")
	}
	if got, want := len(empty1.LineageID()), len("l-")+lineageIDCodeLen; got != want {
		t.Errorf("empty-root LineageID length = %d, want %d", got, want)
	}
}

func TestScan_PureAppendStaysOneLineage(t *testing.T) {
	t.Parallel()
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 10, m, 0, 0, time.UTC) }
	sys := sysMsg("sys")
	msgs := []map[string]any{sys, userMsg("do the task")}
	var recs []audit.Record
	for i := 0; i < 8; i++ {
		recs = append(recs, mkAuditRec(at(i), chatBody(msgs...)))
		msgs = append(msgs, assistantMsg("reply"), userMsg("more"))
	}
	path := writeJSONL(t, recs)
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 1 {
		t.Fatalf("got %d lineages, want 1 (pure append growth)", len(g.Lineages))
	}
	if len(g.Lineages[0].Manifests) != 8 {
		t.Errorf("got %d manifests, want 8", len(g.Lineages[0].Manifests))
	}
}

func TestScan_DifferentSessKeysAreIndependentBuckets(t *testing.T) {
	t.Parallel()
	at := func(m int) time.Time { return time.Date(2026, 7, 16, 10, m, 0, 0, time.UTC) }
	recA := mkAuditRec(at(0), chatBody(sysMsg("sys"), userMsg("task A")))
	recB := mkAuditRec(at(1), chatBody(sysMsg("sys"), userMsg("task B (unrelated)")))
	path := writeJSONL(t, []audit.Record{recA, recB})
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 2 {
		t.Fatalf("got %d lineages, want 2 (different anchors)", len(g.Lineages))
	}
	if g.Lineages[0].SessKey == g.Lineages[1].SessKey {
		t.Error("different opening instructions should anchor to different SessKeys")
	}
}

func TestScan_NoBodyRecordsCounted(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	rejected := audit.Record{TS: at, Model: "", Outcome: "error",
		Client: audit.Exchange{Request: audit.Message{Body: nil}}}
	good := mkAuditRec(at.Add(time.Second), chatBody(sysMsg("sys"), userMsg("hi")))
	path := writeJSONL(t, []audit.Record{rejected, good})
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if g.NoBody != 1 {
		t.Errorf("NoBody = %d, want 1", g.NoBody)
	}
	if len(g.Lineages) != 1 {
		t.Errorf("got %d lineages, want 1 (only the parseable record)", len(g.Lineages))
	}
}

func TestScan_EmptyPaths(t *testing.T) {
	t.Parallel()
	g, err := Scan(nil)
	if err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if len(g.Lineages) != 0 {
		t.Errorf("expected no lineages for empty input")
	}
}

func TestScan_MissingFileReturnsError(t *testing.T) {
	t.Parallel()
	if _, err := Scan([]string{"/nonexistent/path.jsonl"}); err == nil {
		t.Error("expected error for missing file")
	}
}
