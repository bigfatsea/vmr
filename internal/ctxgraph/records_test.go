// Ver 2026-07-28 22:55, by Sonnet 5

package ctxgraph

import (
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
