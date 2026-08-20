// Ver 2026-08-20 00:00, by Sonnet 5

package reqdetail

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func minimalRec(ts time.Time) *audit.Record {
	return &audit.Record{TS: ts, Model: "agent", Outcome: "ok",
		Client: audit.Exchange{
			Request: audit.Message{Body: map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}},
		},
		Attempts: []audit.Attempt{{Endpoint: "openai:minimax:MiniMax-M3", Model: "MiniMax-M3"}}}
}

func TestEnsureRendered_WritesOnce(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := minimalRec(ts)

	name1, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, "")
	if err != nil {
		t.Fatal(err)
	}
	fi1, err := os.Stat(filepath.Join(dir, name1))
	if err != nil {
		t.Fatal(err)
	}

	name2, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, "")
	if err != nil {
		t.Fatal(err)
	}
	if name2 != name1 {
		t.Fatalf("filename changed across calls: %q vs %q", name1, name2)
	}
	fi2, err := os.Stat(filepath.Join(dir, name2))
	if err != nil {
		t.Fatal(err)
	}
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Errorf("mtime changed across calls (%v -> %v): second call re-rendered instead of skipping", fi1.ModTime(), fi2.ModTime())
	}
}

func TestEnsureRendered_NeverOverwritesAPreexistingFile(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := minimalRec(ts)

	name := FileNameForRecord(rec, "audit.jsonl", 1)
	sentinel := []byte("not what Render would have produced")
	if err := os.WriteFile(filepath.Join(dir, name), sentinel, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != name {
		t.Fatalf("filename = %q, want %q", got, name)
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(sentinel) {
		t.Errorf("EnsureRendered overwrote a pre-existing same-named file — the idempotency contract is a skip, not a refresh")
	}
}

func TestEnsureRendered_NoLeftoverTempFiles(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2026, 7, 9, 0, 31, 6, 0, time.UTC)
	rec := minimalRec(ts)

	if _, err := EnsureRendered(dir, rec, "audit.jsonl", 1, nil, nil, taskseg.OpenClawAware, i18n.EN, ""); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want exactly 1 (the .md, no leftover .tmp): %v", len(entries), entries)
	}
}
