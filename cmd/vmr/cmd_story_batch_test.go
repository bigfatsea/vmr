// Ver 2026-08-30, by Sonnet 5

package main

import (
	"testing"

	"vmr/internal/ctxgraph"
)

func chainOfBytes(total int) []*ctxgraph.Lineage {
	return []*ctxgraph.Lineage{{Manifests: []*ctxgraph.Manifest{{Bytes: total}}}}
}

func TestBatchByBytes(t *testing.T) {
	t.Run("packs consecutive chains until adding the next would exceed the budget", func(t *testing.T) {
		chains := [][]*ctxgraph.Lineage{
			chainOfBytes(30), chainOfBytes(30), chainOfBytes(30), // 90 <= 100, all in the first range
			chainOfBytes(40), chainOfBytes(40), // 130 > 100 at the 4th -> new range; then 80
		}
		got := batchByBytes(chains, 100)
		want := [][2]int{{0, 3}, {3, 5}}
		if !equalRanges(got, want) {
			t.Errorf("batchByBytes = %v, want %v", got, want)
		}
	})

	t.Run("a single over-budget chain forms its own range, never split", func(t *testing.T) {
		chains := [][]*ctxgraph.Lineage{
			chainOfBytes(10), chainOfBytes(500), chainOfBytes(10),
		}
		got := batchByBytes(chains, 100)
		want := [][2]int{{0, 1}, {1, 2}, {2, 3}}
		if !equalRanges(got, want) {
			t.Errorf("batchByBytes = %v, want %v", got, want)
		}
	})

	t.Run("everything fits in one range", func(t *testing.T) {
		chains := [][]*ctxgraph.Lineage{chainOfBytes(10), chainOfBytes(10)}
		got := batchByBytes(chains, 100)
		if !equalRanges(got, [][2]int{{0, 2}}) {
			t.Errorf("batchByBytes = %v, want [[0 2]]", got)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if got := batchByBytes(nil, 100); len(got) != 0 {
			t.Errorf("batchByBytes(nil) = %v, want empty", got)
		}
	})

	t.Run("ranges cover every chain exactly once, in order", func(t *testing.T) {
		var chains [][]*ctxgraph.Lineage
		for i := 0; i < 25; i++ {
			chains = append(chains, chainOfBytes(17))
		}
		got := batchByBytes(chains, 100)
		prevEnd := 0
		for _, r := range got {
			if r[0] != prevEnd {
				t.Fatalf("gap or overlap: range %v after end %d", r, prevEnd)
			}
			prevEnd = r[1]
		}
		if prevEnd != len(chains) {
			t.Errorf("ranges stop at %d, want %d", prevEnd, len(chains))
		}
	})
}

func equalRanges(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestChainBytes_SumsEveryManifest(t *testing.T) {
	chain := []*ctxgraph.Lineage{
		{Manifests: []*ctxgraph.Manifest{{Bytes: 10}, {Bytes: 20}}},
		{Manifests: []*ctxgraph.Manifest{{Bytes: 5}}},
	}
	if got := chainBytes(chain); got != 35 {
		t.Errorf("chainBytes = %d, want 35", got)
	}
}
