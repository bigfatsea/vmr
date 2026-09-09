package livestats

import (
	"math"
	"slices"
	"time"
)

type ringKey struct {
	provider, model string
	stream          bool
}

// ring is a fixed-capacity circular buffer of raw per-request tuples
// (design §3.4): percentiles and TPS are computed at read time, so a
// formula fix never requires data migration.
type ring struct {
	buf  [ringCap]ringEntry
	next int
	n    int
}

type ringEntry struct {
	ts        time.Time
	durMS     int64
	ttftMS    int64
	tokensOut int64
}

func (r *ring) add(e ringEntry) {
	r.buf[r.next] = e
	r.next = (r.next + 1) % ringCap
	if r.n < ringCap {
		r.n++
	}
}

// last returns the most recent k entries, oldest first; k beyond the fill
// level returns everything there is.
func (r *ring) last(k int) []ringEntry {
	if k > r.n {
		k = r.n
	}
	out := make([]ringEntry, k)
	start := (r.next - k + ringCap) % ringCap
	for i := range out {
		out[i] = r.buf[(start+i)%ringCap]
	}
	return out
}

// quantilesFor computes nearest-rank p50/p90 over a sorted copy of the
// entries, at read time (§3.4). stream selects the TPS denominator (§8).
func quantilesFor(entries []ringEntry, stream bool) *Quantiles {
	ttfts := make([]int64, len(entries))
	tps := make([]float64, 0, len(entries))
	for i, e := range entries {
		ttfts[i] = e.ttftMS
		if v := tpsOf(e, stream); v > 0 {
			tps = append(tps, v)
		}
	}
	slices.Sort(ttfts)
	slices.Sort(tps)
	return &Quantiles{
		TTFTP50: nearestRankInt(ttfts, 0.5),
		TTFTP90: nearestRankInt(ttfts, 0.9),
		TPSP50:  nearestRankFloat(tps, 0.5),
		TPSP90:  nearestRankFloat(tps, 0.9),
	}
}

// tpsOf applies the stream-keyed TPS denominator (§8). Samples with no
// generated tokens or no measurable span yield 0 and drop out of the TPS
// percentile population.
func tpsOf(e ringEntry, stream bool) float64 {
	if e.tokensOut <= 0 {
		return 0
	}
	durMS := e.durMS
	if stream {
		durMS -= e.ttftMS
	}
	if durMS <= 0 {
		return 0
	}
	return float64(e.tokensOut) / (float64(durMS) / 1000)
}

// nearestRank{Int,Float} are the nearest-rank percentile over a pre-sorted
// slice: ceil(p·n), clamped to [1, n].
func nearestRankInt(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p * float64(len(sorted))))
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

func nearestRankFloat(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p * float64(len(sorted))))
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}
