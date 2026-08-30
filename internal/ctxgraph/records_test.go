// Ver 2026-07-28 22:55, by Sonnet 5

package ctxgraph

import (
	"strings"
	"sync"
	"testing"
	"time"

	"vmr/internal/audit"
)

func TestBuildManifest_TraceID(t *testing.T) {
	t.Parallel()
	body := map[string]any{"messages": []any{userMsg("hi")}}
	rec := mkAuditRec(time.Now(), body)
	rec.Client.Request.Headers = map[string][]string{
		"Traceparent": {"00-a08ed774f864abadece75eb1bfa1a373-ef5fd26272c5b12f-01"},
	}
	m, ok := BuildManifest(&rec, "f", 1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if m.TraceID != "a08ed774f864abadece75eb1bfa1a373" {
		t.Errorf("TraceID = %q, want a08ed774f864abadece75eb1bfa1a373", m.TraceID)
	}
}

func TestBuildManifest_NoTraceparent(t *testing.T) {
	t.Parallel()
	body := map[string]any{"messages": []any{userMsg("hi")}}
	rec := mkAuditRec(time.Now(), body)
	m, _ := BuildManifest(&rec, "f", 1)
	if m.TraceID != "" {
		t.Errorf("TraceID = %q, want empty", m.TraceID)
	}
}

func TestFetchRecords_ResolvesRequestedLines(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	r1 := mkAuditRec(at, chatBody(sysMsg("sys"), userMsg("one")))
	r2 := mkAuditRec(at.Add(time.Second), chatBody(sysMsg("sys"), userMsg("one"), assistantMsg("two")))
	r3 := mkAuditRec(at.Add(2*time.Second), chatBody(sysMsg("sys"), userMsg("three")))
	path := writeJSONL(t, []audit.Record{r1, r2, r3})

	// Ask for lines 1 and 3 only — line 2 must not appear in the result.
	got, err := FetchRecords([]Loc{{Path: path, Line: 1}, {Path: path, Line: 3}})
	if err != nil {
		t.Fatalf("FetchRecords: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d records, want 2", len(got))
	}
	rec1, ok := got[Loc{Path: path, Line: 1}]
	if !ok {
		t.Fatal("line 1 not found")
	}
	body1, _ := rec1.Client.Request.Body.(map[string]any)
	msgs1 := body1["messages"].([]any)
	if len(msgs1) != 2 {
		t.Errorf("line 1 messages = %d, want 2", len(msgs1))
	}
	if _, ok := got[Loc{Path: path, Line: 2}]; ok {
		t.Error("line 2 should not be in the result (not requested)")
	}
	if _, ok := got[Loc{Path: path, Line: 3}]; !ok {
		t.Error("line 3 should be in the result")
	}
}

func TestFetchRecords_MissingFile(t *testing.T) {
	t.Parallel()
	if _, err := FetchRecords([]Loc{{Path: "/nonexistent", Line: 1}}); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFetchRecords_EmptyLocs(t *testing.T) {
	t.Parallel()
	got, err := FetchRecords(nil)
	if err != nil {
		t.Fatalf("FetchRecords(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %d", len(got))
	}
}

func TestForEachRecord_MatchesFetchRecords(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	r1 := mkAuditRec(at, chatBody(sysMsg("sys"), userMsg("one")))
	r2 := mkAuditRec(at.Add(time.Second), chatBody(sysMsg("sys"), userMsg("one"), assistantMsg("two")))
	r3 := mkAuditRec(at.Add(2*time.Second), chatBody(sysMsg("sys"), userMsg("three")))
	path := writeJSONL(t, []audit.Record{r1, r2, r3})

	locs := []Loc{{Path: path, Line: 1}, {Path: path, Line: 3}}
	want, err := FetchRecords(locs)
	if err != nil {
		t.Fatalf("FetchRecords: %v", err)
	}

	got := map[Loc]*audit.Record{}
	var mu sync.Mutex
	if err := ForEachRecord(locs, func(loc Loc, rec *audit.Record) {
		mu.Lock()
		got[loc] = rec
		mu.Unlock()
	}); err != nil {
		t.Fatalf("ForEachRecord: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("ForEachRecord yielded %d records, FetchRecords %d", len(got), len(want))
	}
	for loc, w := range want {
		g, ok := got[loc]
		if !ok {
			t.Errorf("%v missing from ForEachRecord", loc)
			continue
		}
		wb, _ := w.Client.Request.Body.(map[string]any)
		gb, _ := g.Client.Request.Body.(map[string]any)
		if len(wb["messages"].([]any)) != len(gb["messages"].([]any)) {
			t.Errorf("%v message count differs", loc)
		}
	}
	if _, ok := got[Loc{Path: path, Line: 2}]; ok {
		t.Error("line 2 should not be yielded (not requested)")
	}
}

func TestForEachRecord_MissingFile(t *testing.T) {
	t.Parallel()
	err := ForEachRecord([]Loc{{Path: "/nonexistent", Line: 1}}, func(Loc, *audit.Record) {})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestForEachRecord_EmptyLocs(t *testing.T) {
	t.Parallel()
	n := 0
	if err := ForEachRecord(nil, func(Loc, *audit.Record) { n++ }); err != nil {
		t.Fatalf("ForEachRecord(nil): %v", err)
	}
	if n != 0 {
		t.Errorf("callback fired %d times on empty input", n)
	}
}

func TestScanFile_PopulatesManifestBytes(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	recs := []audit.Record{
		mkAuditRec(at, chatBody(sysMsg("sys"), userMsg("short"))),
		mkAuditRec(at.Add(time.Second), chatBody(sysMsg("sys"), userMsg(strings.Repeat("padding ", 500)))),
	}
	path := writeJSONL(t, recs)
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var seen int
	for _, l := range g.Lineages {
		for _, m := range l.Manifests {
			seen++
			if m.Bytes <= 0 {
				t.Errorf("manifest line %d: Bytes = %d, want > 0", m.Line, m.Bytes)
			}
		}
	}
	for _, m := range g.Ungrouped {
		seen++
		if m.Bytes <= 0 {
			t.Errorf("ungrouped manifest line %d: Bytes = %d, want > 0", m.Line, m.Bytes)
		}
	}
	if seen == 0 {
		t.Fatal("no manifests produced")
	}
}
