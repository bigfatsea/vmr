// Ver 2026-08-20 18:30, by Sonnet 5

package ctxgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/audit"
)

// realLogsGlob mirrors internal/report/e2e_test.go's realLogPath
// convention (relative path, skip-if-absent) — this repo's real corpus
// lives outside the module and isn't present on every clone/CI runner.
var realLogsGlob = "../../logs/*.jsonl.zst"

// TestSyntheticCorpus_LineageIDHasNoCollisions is the hermetic, in-memory
// regression guard for the bug caught in LineageID's first implementation:
// recurring cron/heartbeat jobs sharing a byte-identical opening message
// template must NOT collide on LineageID even when their opening content
// is identical. This runs hermetically in milliseconds and executes on all
// clones and CI runners without requiring external audit logs.
func TestSyntheticCorpus_LineageIDHasNoCollisions(t *testing.T) {
	t.Parallel()
	at := func(min int) time.Time { return time.Date(2026, 8, 20, 10, min, 0, 0, time.UTC) }

	sys := sysMsg("You are a recurring heartbeat monitor.")
	trigger := userMsg("CRON_HEARTBEAT_CHECK")

	// 4 distinct cron sessions executed at different times (e.g. every 15 minutes).
	// Each session has the exact same opening template (sys + trigger) and its
	// own metadata session id, producing structurally distinct Lineages that share
	// identical opening messages.
	var recs []audit.Record
	for i := 0; i < 4; i++ {
		startTime := at(i * 15)
		meta := map[string]any{"user_id": fmt.Sprintf("session_cron_run_%d", i)}
		bodyTurn1 := map[string]any{
			"metadata": meta,
			"messages": []any{sys, trigger},
		}
		bodyTurn2 := map[string]any{
			"metadata": meta,
			"messages": []any{sys, trigger, assistantMsg("all systems operational")},
		}
		recs = append(recs, mkAuditRec(startTime, bodyTurn1))
		recs = append(recs, mkAuditRec(startTime.Add(2*time.Second), bodyTurn2))
	}

	path := writeJSONL(t, recs)
	g, err := Scan([]string{path})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(g.Lineages) != 4 {
		t.Fatalf("expected 4 distinct lineages, got %d", len(g.Lineages))
	}

	seen := map[string]int{} // LineageID -> Idx
	for _, l := range g.Lineages {
		id := l.LineageID()
		if prior, ok := seen[id]; ok {
			t.Errorf("LineageID collision: %q claimed by both lineage %d and lineage %d", id, prior, l.Idx)
			continue
		}
		seen[id] = l.Idx
	}
	if len(seen) != len(g.Lineages) {
		t.Errorf("distinct LineageIDs = %d, want %d (one per Lineage)", len(seen), len(g.Lineages))
	}
}

// TestRealCorpus_LineageIDHasNoCollisions is an opt-in full-corpus regression
// guard executed against real production audit logs when explicitly requested.
// It is skipped by default to keep unit test suites fast and hermetic; enable
// with RUN_REAL_CORPUS_E2E=1 when validating against local log collections.
func TestRealCorpus_LineageIDHasNoCollisions(t *testing.T) {
	if os.Getenv("RUN_REAL_CORPUS_E2E") != "1" {
		t.Skip("skipping real audit corpus scan by default; set RUN_REAL_CORPUS_E2E=1 to run against local logs/")
	}
	paths, err := filepath.Glob(realLogsGlob)
	if err != nil || len(paths) == 0 {
		t.Skip("real audit corpus not present; skipping on this clone")
	}
	if os.Getenv("SKIP_SLOW_E2E") == "1" {
		t.Skip("SKIP_SLOW_E2E set")
	}
	g, err := Scan(paths)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{} // LineageID -> the Idx that first claimed it
	for _, l := range g.Lineages {
		id := l.LineageID()
		if prior, ok := seen[id]; ok {
			t.Errorf("LineageID collision: %q claimed by both lineage %d and lineage %d", id, prior, l.Idx)
			continue
		}
		seen[id] = l.Idx
	}
	if len(seen) != len(g.Lineages) {
		t.Errorf("distinct LineageIDs = %d, want %d (one per Lineage)", len(seen), len(g.Lineages))
	}
}
