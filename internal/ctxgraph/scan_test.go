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

func TestScan_BlobIndexFetchAllRecoversOriginalContent(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	body := chatBody(sysMsg("sys"), userMsg("hello there"), assistantMsg("hi back"))
	rec := mkAuditRec(at, body)
	path := writeJSONL(t, []audit.Record{rec})
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(g.Lineages) != 1 || len(g.Lineages[0].Manifests) != 1 {
		t.Fatalf("unexpected graph shape: %d lineages", len(g.Lineages))
	}
	m := g.Lineages[0].Manifests[0]
	if len(m.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(m.Keys))
	}
	fetched, err := g.Index.FetchAll(m.Keys)
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if fetched[m.Keys[0]].Text != "hello there" {
		t.Errorf("fetched[0].Text = %q, want %q", fetched[m.Keys[0]].Text, "hello there")
	}
	if fetched[m.Keys[1]].Text != "hi back" {
		t.Errorf("fetched[1].Text = %q, want %q", fetched[m.Keys[1]].Text, "hi back")
	}

	// Lookup/Len are BlobIndex's other public accessors (FetchAll reads
	// idx.refs directly, so neither is exercised by the assertions above).
	if got := g.Index.Len(); got != 2 {
		t.Errorf("Index.Len() = %d, want 2", got)
	}
	ref, ok := g.Index.Lookup(m.Keys[0])
	if !ok || ref.Path != path || ref.Line != 1 || ref.Idx != m.MsgIdx[0] {
		t.Errorf("Lookup(m.Keys[0]) = %+v, %v, want {%s 1 %d}, true", ref, ok, path, m.MsgIdx[0])
	}
	if _, ok := g.Index.Lookup(Hash{}); ok {
		t.Error("Lookup of a hash that was never indexed should report ok=false")
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
