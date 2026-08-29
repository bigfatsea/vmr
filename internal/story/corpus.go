// Ver 2026-08-05, by Sonnet 5

// Corpus-level statistics — "一批 Journey 里找出反复出现的行为
// 倾向" (the story design specification's
// corpus-level statistics section), built directly on data this package
// already computes per-Journey (Metrics, Finding) — no new collection, no
// LLM, pure descriptive statistics. Three deliberate limits, all straight
// from the design doc's own discipline (its relevance/no-labels
// disciplines, restated here because this is the one file that could
// otherwise drift into overclaiming):
//
// 1. Correlations are Spearman rank correlation (not Pearson) reported as
// an effect size (rho) only — no p-values, no significance claims. The
// corpus sizes this runs on (tens to low hundreds of Journeys) can't
// support a real significance test, and reporting one anyway would
// manufacture false confidence.
// 2. There is no "did the task succeed" label anywhere in this file.
// VMR's zero-embedded-instrumentation premise means it structurally
// cannot know whether a task was actually accomplished — only rule-
// derived proxies (duration, Finding hit rate) are compared, and the
// output says so.
// 3. No multiple-comparison correction. At this corpus scale that kind of
// rigor is pure form over substance — the mitigation is reporting
// effect size only and requiring a minimum sample size before a
// comparison is shown at all, not a Bonferroni correction nobody
// would trust the inputs to anyway.
package story

import (
	"math"
	"sort"

	"vmr/internal/i18n"
)

// Distribution summarizes one metric's values across a corpus — mean/
// median/min/max/p90, deliberately nothing fancier (no skewness, no
// confidence interval) since the corpus sizes this runs on don't support
// more than eyeballing the shape.
type Distribution struct {
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	P90    float64 `json:"p90"`
}

func computeDistribution(values []float64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	return Distribution{
		Count:  len(sorted),
		Mean:   sum / float64(len(sorted)),
		Median: percentile(sorted, 0.5),
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		P90:    percentile(sorted, 0.9),
	}
}

// percentile expects sorted ascending; linear interpolation between the
// two nearest ranks — good enough for a triage-level summary, not a
// statistics-library-grade implementation.
func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 1 {
		return sorted[0]
	}
	pos := p * float64(n-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}

// CorrelationRow is one metric pair's Spearman rank correlation across the
// corpus — see this file's package comment for why rho (effect size) only,
// never a p-value.
type CorrelationRow struct {
	MetricA MetricCode `json:"metric_a"`
	MetricB MetricCode `json:"metric_b"`
	Rho     float64    `json:"rho"`
	N       int        `json:"n"`
}

// corpusMinCorrelationN/corpusMinCorrelationRho: below N, a correlation
// coefficient is noise dressed up as a number; below |rho|, it's not worth
// a reader's attention even if computed. Both are triage bars, not
// statistical significance claims.
const (
	corpusMinCorrelationN   = 5
	corpusMinCorrelationRho = 0.3
)

func spearman(a, b []float64) (rho float64, n int) {
	n = len(a)
	if n != len(b) || n < corpusMinCorrelationN {
		return 0, n
	}
	ra, rb := rankValues(a), rankValues(b)
	return pearson(ra, rb), n
}

// rankValues assigns each value its average rank (ties share the mean of
// the ranks they'd occupy) — the standard Spearman tie-handling rule.
func rankValues(vals []float64) []float64 {
	n := len(vals)
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool { return vals[idx[i]] < vals[idx[j]] })
	ranks := make([]float64, n)
	i := 0
	for i < n {
		j := i
		for j+1 < n && vals[idx[j+1]] == vals[idx[i]] {
			j++
		}
		avgRank := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			ranks[idx[k]] = avgRank
		}
		i = j + 1
	}
	return ranks
}

func pearson(a, b []float64) float64 {
	n := float64(len(a))
	var sumA, sumB, sumAB, sumA2, sumB2 float64
	for i := range a {
		sumA += a[i]
		sumB += b[i]
		sumAB += a[i] * b[i]
		sumA2 += a[i] * a[i]
		sumB2 += b[i] * b[i]
	}
	num := n*sumAB - sumA*sumB
	den := math.Sqrt((n*sumA2 - sumA*sumA) * (n*sumB2 - sumB*sumB))
	if den == 0 {
		return 0
	}
	return num / den
}

// GroupComparison is the corpus layer's Finding-grouped validation: journeys that hit
// FindingCode at least once vs journeys that didn't, compared on
// NetWorkingMS's median (the closest single "cost" proxy this package
// has — see this file's package comment on why there's no success/failure
// label to compare against instead). Notable reuses compare.go's own
// notableFloor/notableRelThreshold bar, the same "worth a second look, not
// a proven cause" semantics MetricDiff.Notable already carries.
type GroupComparison struct {
	Code        FindingCode `json:"code"`
	HitCount    int         `json:"hit_count"`
	NoHitCount  int         `json:"no_hit_count"`
	HitMedian   float64     `json:"hit_median_net_working_ms"`
	NoHitMedian float64     `json:"no_hit_median_net_working_ms"`
	DeltaRel    float64     `json:"delta_rel"`
	Notable     bool        `json:"notable"`
}

// corpusMinGroupSize: below this on EITHER side, a median comparison is
// one or two data points pretending to be a distribution — skipped
// entirely rather than shown with a misleadingly precise-looking number.
const corpusMinGroupSize = 3

// CorpusStats is the corpus layer's entire output — vmr-story-corpus.json's shape.
type CorpusStats struct {
	JourneyCount     int                         `json:"journey_count"`
	MetricDist       map[MetricCode]Distribution `json:"metric_distributions"`
	FindingRate      map[FindingCode]float64     `json:"finding_rates"` // fraction of journeys hitting >=1 Finding of this Code
	Correlations     []CorrelationRow            `json:"correlations,omitempty"`
	GroupComparisons []GroupComparison           `json:"group_comparisons,omitempty"`
	// SkippedGroupComparisons names FindingCodes that had at least one hit
	// but not enough journeys on one side (< corpusMinGroupSize) to compare
	// — named explicitly rather than silently absent, so "not shown" reads
	// as "not enough data" and not "nothing found".
	SkippedGroupComparisons []FindingCode         `json:"skipped_group_comparisons,omitempty"`
	ContextRot              []ContextRotBucket    `json:"context_rot,omitempty"`
	ToolSequences           []ToolSequencePattern `json:"tool_sequences,omitempty"`
	// ProtocolShare is each ctxgraph.Manifest.Protocol value's fraction of
	// this corpus's Steps, feeding the detector-coverage disclosure.
	// Populated unconditionally, at zero inference cost (a straight
	// tally of a field every Step already carries) — anthropicCoverageNote
	// decides whether it's worth printing.
	ProtocolShare map[string]float64 `json:"protocol_share,omitempty"`
}

// ComputeCorpusStats is the corpus layer's entire computation: per-metric
// distributions, per-Finding-Code hit rates, pairwise Spearman
// correlations among the fourteen behavior-profile metrics, and
// Finding-grouped NetWorkingMS comparisons. All of it is pure, in-memory,
// zero-LLM aggregation over already-computed Metrics/Findings — journeys
// themselves are never re-parsed here, matching Findings' own "rules
// first" discipline.
func ComputeCorpusStats(journeys []*Journey) CorpusStats {
	stats := CorpusStats{
		JourneyCount: len(journeys),
		MetricDist:   map[MetricCode]Distribution{},
		FindingRate:  map[FindingCode]float64{},
	}
	if len(journeys) == 0 {
		return stats
	}

	metrics := make([]Metrics, len(journeys))
	findingsPerJourney := make([][]Finding, len(journeys))
	for i, j := range journeys {
		metrics[i] = ComputeMetrics(j)
		findingsPerJourney[i] = ComputeFindings(j, i18n.EN)
	}
	stats.ProtocolShare = protocolShare(journeys)

	values := make(map[MetricCode][]float64, len(metricSpecs))
	for _, spec := range metricSpecs {
		vs := make([]float64, len(metrics))
		for i, m := range metrics {
			vs[i] = spec.Value(m)
		}
		values[spec.Code] = vs
		stats.MetricDist[spec.Code] = computeDistribution(vs)
	}

	hitByCode := map[FindingCode]map[int]bool{}
	for i, fs := range findingsPerJourney {
		seen := map[FindingCode]bool{}
		for _, f := range fs {
			seen[f.Code] = true
		}
		for code := range seen {
			if hitByCode[code] == nil {
				hitByCode[code] = map[int]bool{}
			}
			hitByCode[code][i] = true
		}
	}
	var codes []FindingCode
	for code, hits := range hitByCode {
		codes = append(codes, code)
		stats.FindingRate[code] = float64(len(hits)) / float64(len(journeys))
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })

	for ai, specA := range metricSpecs {
		for _, specB := range metricSpecs[ai+1:] {
			a, b := specA.Code, specB.Code
			rho, n := spearman(values[a], values[b])
			if n < corpusMinCorrelationN || math.Abs(rho) < corpusMinCorrelationRho {
				continue
			}
			stats.Correlations = append(stats.Correlations, CorrelationRow{MetricA: a, MetricB: b, Rho: rho, N: n})
		}
	}
	sort.Slice(stats.Correlations, func(i, j int) bool {
		return math.Abs(stats.Correlations[i].Rho) > math.Abs(stats.Correlations[j].Rho)
	})

	netWorking := values[MetricNetWorkingMS]
	for _, code := range codes {
		hits := hitByCode[code]
		var hitVals, noHitVals []float64
		for i, v := range netWorking {
			if hits[i] {
				hitVals = append(hitVals, v)
			} else {
				noHitVals = append(noHitVals, v)
			}
		}
		if len(hitVals) < corpusMinGroupSize || len(noHitVals) < corpusMinGroupSize {
			stats.SkippedGroupComparisons = append(stats.SkippedGroupComparisons, code)
			continue
		}
		hitMed := computeDistribution(hitVals).Median
		noHitMed := computeDistribution(noHitVals).Median
		denom := math.Max(math.Abs(hitMed), math.Abs(noHitMed))
		var deltaRel float64
		if denom != 0 {
			deltaRel = (hitMed - noHitMed) / denom
		}
		notable := math.Abs(hitMed-noHitMed) >= notableFloor[KindMillis] && math.Abs(deltaRel) >= notableRelThreshold
		stats.GroupComparisons = append(stats.GroupComparisons, GroupComparison{
			Code: code, HitCount: len(hitVals), NoHitCount: len(noHitVals),
			HitMedian: hitMed, NoHitMedian: noHitMed, DeltaRel: deltaRel, Notable: notable,
		})
	}
	stats.ContextRot = computeContextRot(journeys, findingsPerJourney)
	stats.ToolSequences = computeToolSequences(journeys)
	return stats
}
