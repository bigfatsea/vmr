// Ver 2026-08-20 18:30, by Sonnet 5

package ctxgraph

import (
	"os"
	"path/filepath"
	"testing"
)

// realLogsGlob mirrors internal/report/e2e_test.go's realLogPath
// convention (relative path, skip-if-absent) — this repo's real corpus
// lives outside the module and isn't present on every clone/CI runner.
var realLogsGlob = "../../logs/*.jsonl.zst"

// TestRealCorpus_LineageIDHasNoCollisions is a permanent regression guard
// for a bug caught in LineageID's first implementation: computing it from
// RootHash alone collided for real, structurally distinct Lineages
// (confirmed on this exact corpus: 4 collision groups among 1638 Lineages,
// all recurring cron/heartbeat jobs sharing a byte-identical opening message
// template).
// report's session-grouping maps are keyed by this string (SessionInfo.ID)
// — a collision there silently merges two unrelated sessions' tokens/
// requests into one, which no synthetic fixture is likely to reproduce as
// convincingly as this real corpus already has once.
func TestRealCorpus_LineageIDHasNoCollisions(t *testing.T) {
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
