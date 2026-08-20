// Ver 2026-08-20 17:25, by Sonnet 5

package main

import (
	"testing"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

func TestSelfTrafficExcludeTags(t *testing.T) {
	t.Run("empty llmKey and no extra tags excludes nothing", func(t *testing.T) {
		if got := selfTrafficExcludeTags("", nil); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
	t.Run("llmKey derives its audit.KeyTag", func(t *testing.T) {
		key := "sk-analysis-vmrstory"
		got := selfTrafficExcludeTags(key, nil)
		want := audit.KeyTag(key)
		if !got[want] {
			t.Errorf("selfTrafficExcludeTags(%q, nil) = %v, want a set containing %q (same transform internal/server's authenticate() applies)", key, got, want)
		}
	})
	t.Run("extra tags are unioned in, empty strings ignored", func(t *testing.T) {
		got := selfTrafficExcludeTags("", []string{"manual-tag", ""})
		if !got["manual-tag"] || len(got) != 1 {
			t.Errorf("got %v, want exactly {manual-tag: true}", got)
		}
	})
}

func TestFilterSelfTrafficCandidates(t *testing.T) {
	mk := func(tag string) *ctxgraph.Lineage {
		return &ctxgraph.Lineage{Manifests: []*ctxgraph.Manifest{{ClientKeyTag: tag}}}
	}
	workload, selfAnalysis := mk("workload"), mk("vmrstory")
	cands := []*ctxgraph.Lineage{workload, selfAnalysis}

	got := filterSelfTrafficCandidates(cands, "", []string{"vmrstory"})
	if len(got) != 1 || got[0] != workload {
		t.Errorf("filterSelfTrafficCandidates dropped the wrong set: got %d candidates, want [workload]", len(got))
	}

	// No exclusion configured at all (llmKey == "" and no extra tags) —
	// the common case — must return every candidate unchanged.
	all := filterSelfTrafficCandidates(cands, "", nil)
	if len(all) != 2 {
		t.Errorf("no exclusion configured: got %d candidates, want 2 (both kept)", len(all))
	}
}
