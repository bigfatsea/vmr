// Ver 2026-08-30, by Sonnet 5

package main

import "vmr/internal/ctxgraph"

// renderBatchBudgetBytes bounds one story.BuildAll batch's decompressed
// working set: BuildAll does ONE combined ctxgraph.FetchRecords across its
// input, decoding ~this many bytes of audit JSON, and that transient
// dominates peak memory (the built Journeys themselves are ~1% of it —
// Step stopped holding its Record). Chunking bounds the peak to one
// batch's worth regardless of total candidate count; each batch's records
// are released before the next fetch.
//
// The budget is on Manifest.Bytes (decompressed line length), not
// candidate count: a single real "task" Journey can be hundreds of MB of
// resent history while twenty heartbeat candidates together are under a
// megabyte, so a fixed count gave a wildly variable peak. 160 MiB (decode
// amplification ~1.4x plus transient parse garbage) keeps a batch's
// working set a few hundred MB, measured to hold full-corpus -corpus and
// -render-all peak RSS under ~2 GB. A candidate bigger than the whole
// budget still forms its own batch — a Journey is never split.
const renderBatchBudgetBytes = 160 << 20

// batchByBytes splits chains into consecutive [start,end) index ranges
// whose summed Manifest.Bytes stays under budget (order preserved; a
// single over-budget chain forms its own range).
func batchByBytes(chains [][]*ctxgraph.Lineage, budget int) [][2]int {
	var out [][2]int
	start, curBytes := 0, 0
	for i, chain := range chains {
		n := chainBytes(chain)
		if i > start && curBytes+n > budget {
			out = append(out, [2]int{start, i})
			start, curBytes = i, 0
		}
		curBytes += n
	}
	if start < len(chains) {
		out = append(out, [2]int{start, len(chains)})
	}
	return out
}

// chainBytes sums a candidate's decompressed size across every manifest.
// Manifest.Bytes is set by every fresh scan (scan.go), and the one source
// of a 0 — a pre-v3 parse cache — is invalidated by CacheSchemaVersion 3,
// so a real candidate always has weight here. batchByBytes therefore
// doesn't guard the all-zero input (which would collapse into one
// unbounded batch): producing it needs hand-built manifests, i.e. a test.
func chainBytes(chain []*ctxgraph.Lineage) int {
	total := 0
	for _, l := range chain {
		for _, m := range l.Manifests {
			total += m.Bytes
		}
	}
	return total
}
