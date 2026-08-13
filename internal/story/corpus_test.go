// Ver 2026-08-05, by Sonnet 5

package story

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/story/profile"
)

func TestComputeDistribution(t *testing.T) {
	d := computeDistribution([]float64{1, 2, 3, 4, 5})
	if d.Count != 5 {
		t.Errorf("Count = %d, want 5", d.Count)
	}
	if d.Mean != 3 {
		t.Errorf("Mean = %v, want 3", d.Mean)
	}
	if d.Median != 3 {
		t.Errorf("Median = %v, want 3", d.Median)
	}
	if d.Min != 1 || d.Max != 5 {
		t.Errorf("Min/Max = %v/%v, want 1/5", d.Min, d.Max)
	}
}

func TestComputeDistribution_Empty(t *testing.T) {
	d := computeDistribution(nil)
	if d.Count != 0 {
		t.Errorf("Count = %d, want 0", d.Count)
	}
}

func TestSpearman(t *testing.T) {
	t.Run("perfectly correlated: rho near 1", func(t *testing.T) {
		a := []float64{1, 2, 3, 4, 5, 6}
		b := []float64{10, 20, 30, 40, 50, 60}
		rho, n := spearman(a, b)
		if n != 6 {
			t.Fatalf("n = %d, want 6", n)
		}
		if math.Abs(rho-1) > 1e-9 {
			t.Errorf("rho = %v, want ~1", rho)
		}
	})

	t.Run("perfectly anti-correlated: rho near -1", func(t *testing.T) {
		a := []float64{1, 2, 3, 4, 5, 6}
		b := []float64{60, 50, 40, 30, 20, 10}
		rho, _ := spearman(a, b)
		if math.Abs(rho+1) > 1e-9 {
			t.Errorf("rho = %v, want ~-1", rho)
		}
	})

	t.Run("below minimum N: reports n but no attempt to compute a meaningful rho", func(t *testing.T) {
		a := []float64{1, 2}
		b := []float64{1, 2}
		rho, n := spearman(a, b)
		if n != 2 {
			t.Errorf("n = %d, want 2", n)
		}
		if rho != 0 {
			t.Errorf("rho = %v, want 0 (below corpusMinCorrelationN)", rho)
		}
	})

	t.Run("ties handled via average rank", func(t *testing.T) {
		a := []float64{1, 1, 2, 2, 3}
		b := []float64{1, 1, 2, 2, 3}
		rho, _ := spearman(a, b)
		if math.Abs(rho-1) > 1e-9 {
			t.Errorf("rho = %v, want ~1 for identical tied sequences", rho)
		}
	})
}

// buildTestJourney constructs a real *Journey (with usable Manifest/Rec
// data, unlike the lightweight journeyOf fixture) with n Steps, optionally
// injecting an error+verbatim-retry pattern to guarantee a Finding hit,
// so corpus tests have a controllable mix of Metrics/Findings.
func buildTestJourney(t *testing.T, n int, injectFinding bool) *Journey {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "do the task")
	var recs []audit.Record
	msgs := []any{sys, u1}
	for i := 0; i < n; i++ {
		// Distinct arguments per step (the step index baked in) —
		// otherwise n>=exactRepeatThreshold steps would accidentally
		// trigger exact_repeat_tool_call on their own, contaminating
		// tests that want to control exactly when that Finding fires.
		args := `{"i":` + string(rune('0'+i)) + `}`
		recs = append(recs, mkRec(at(i), "", append([]any{}, msgs...), sseToolCalls([]any{
			map[string]any{"id": "c" + string(rune('a'+i)), "function": map[string]any{"name": "step_tool", "arguments": args}},
		})))
		msgs = append(msgs, msg("assistant", "did step"))
	}
	if injectFinding {
		// Three identical repeats of the same tool call trigger
		// exact_repeat_tool_call (exactRepeatThreshold == 3).
		for i := 0; i < 3; i++ {
			recs = append(recs, mkRec(at(n+i), "", append([]any{}, msgs...), sseToolCalls([]any{
				map[string]any{"id": "r" + string(rune('a'+i)), "function": map[string]any{"name": "repeat_tool", "arguments": `{"x":1}`}},
			})))
			msgs = append(msgs, msg("assistant", "repeated"))
		}
	}
	path := writeJSONL(t, recs)
	j, err := Build(onlyLineage(t, path), profile.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return j
}

// TestMetricValue_ModelSwitchCount_Registered (batch 4) locks in all three
// corpus.go registration points for MetricModelSwitchCount: it must be in
// corpusMetricCodes (so it gets a distribution slot at all), have a
// corpusMetricKinds entry (so the Markdown table knows how to render it),
// and metricValue must extract len(Metrics.ModelSwitches), not always 0.
func TestMetricValue_ModelSwitchCount_Registered(t *testing.T) {
	found := false
	for _, c := range corpusMetricCodes {
		if c == MetricModelSwitchCount {
			found = true
		}
	}
	if !found {
		t.Fatal("MetricModelSwitchCount missing from corpusMetricCodes")
	}
	if kind, ok := corpusMetricKinds[MetricModelSwitchCount]; !ok || kind != KindCount {
		t.Fatalf("corpusMetricKinds[MetricModelSwitchCount] = %v (ok=%v), want KindCount", kind, ok)
	}
	m := Metrics{ModelSwitches: []ModelSwitch{{StepSeq: 1}, {StepSeq: 2}, {StepSeq: 3}}}
	if got := metricValue(m, MetricModelSwitchCount); got != 3 {
		t.Errorf("metricValue(MetricModelSwitchCount) = %v, want 3", got)
	}
}

func TestComputeCorpusStats(t *testing.T) {
	t.Run("empty corpus", func(t *testing.T) {
		stats := ComputeCorpusStats(nil)
		if stats.JourneyCount != 0 {
			t.Errorf("JourneyCount = %d, want 0", stats.JourneyCount)
		}
	})

	t.Run("metric distributions and finding rates populated", func(t *testing.T) {
		var journeys []*Journey
		for i := 0; i < 4; i++ {
			journeys = append(journeys, buildTestJourney(t, 2+i, i%2 == 0)) // 2 hit, 2 don't
		}
		stats := ComputeCorpusStats(journeys)
		if stats.JourneyCount != 4 {
			t.Fatalf("JourneyCount = %d, want 4", stats.JourneyCount)
		}
		d, ok := stats.MetricDist[MetricToolCallCount]
		if !ok || d.Count != 4 {
			t.Fatalf("MetricDist[tool_call_count] = %+v, want Count=4", d)
		}
		rate, ok := stats.FindingRate[FindingExactRepeatToolCall]
		if !ok || rate != 0.5 {
			t.Errorf("FindingRate[exact_repeat_tool_call] = %v (ok=%v), want 0.5", rate, ok)
		}
	})

	t.Run("group comparison skipped when sample too small", func(t *testing.T) {
		var journeys []*Journey
		journeys = append(journeys, buildTestJourney(t, 2, true)) // only 1 hit
		journeys = append(journeys, buildTestJourney(t, 2, false))
		journeys = append(journeys, buildTestJourney(t, 3, false))
		stats := ComputeCorpusStats(journeys)
		found := false
		for _, c := range stats.SkippedGroupComparisons {
			if c == FindingExactRepeatToolCall {
				found = true
			}
		}
		if !found {
			t.Errorf("expected exact_repeat_tool_call in SkippedGroupComparisons (only 1 hit, below corpusMinGroupSize): %v", stats.SkippedGroupComparisons)
		}
		for _, g := range stats.GroupComparisons {
			if g.Code == FindingExactRepeatToolCall {
				t.Errorf("exact_repeat_tool_call should not appear in GroupComparisons with only 1 hit: %+v", g)
			}
		}
	})

	// buildTestJourney(t, n, false) produces a Journey whose ModelMS is
	// exactly 100*n (n Steps, mkRec's fixed DurMS=100) and whose
	// ToolCallCount is exactly n (one tool call per Step) — an exact
	// monotonic relationship, deliberately used here so the expected
	// Spearman rho is a known constant (1.0) rather than something this
	// test would have to approximate.
	t.Run("correlations: populated, one specific pair asserted, sorted by |rho| descending", func(t *testing.T) {
		var journeys []*Journey
		for n := 3; n <= 8; n++ { // 6 journeys, N clears corpusMinCorrelationN(5)
			journeys = append(journeys, buildTestJourney(t, n, false))
		}
		stats := ComputeCorpusStats(journeys)
		if len(stats.Correlations) == 0 {
			t.Fatal("expected a non-empty Correlations slice")
		}

		var found *CorrelationRow
		for i := range stats.Correlations {
			c := &stats.Correlations[i]
			if c.MetricA == MetricModelMS && c.MetricB == MetricToolCallCount {
				found = c
			}
		}
		if found == nil {
			t.Fatalf("expected a (model_ms, tool_call_count) correlation row, got: %+v", stats.Correlations)
		}
		if found.N != 6 {
			t.Errorf("N = %d, want 6 (one sample per journey)", found.N)
		}
		if math.Abs(found.Rho-1.0) > 1e-9 {
			t.Errorf("Rho = %v, want ~1.0 (ModelMS=100*n and ToolCallCount=n are perfectly monotonic)", found.Rho)
		}

		for i := 1; i < len(stats.Correlations); i++ {
			if math.Abs(stats.Correlations[i-1].Rho) < math.Abs(stats.Correlations[i].Rho) {
				t.Errorf("Correlations not sorted by |rho| descending at index %d: %+v", i, stats.Correlations)
			}
		}
	})

	t.Run("correlations: excluded entirely when corpus size is below corpusMinCorrelationN", func(t *testing.T) {
		var journeys []*Journey
		for n := 3; n <= 6; n++ { // 4 journeys, below corpusMinCorrelationN(5)
			journeys = append(journeys, buildTestJourney(t, n, false))
		}
		stats := ComputeCorpusStats(journeys)
		if len(stats.Correlations) != 0 {
			t.Errorf("Correlations = %+v, want none (N=4 < corpusMinCorrelationN=5 for every pair)", stats.Correlations)
		}
	})

	// Three hit=true journeys (n=4, so 4+3=7 Steps: buildTestJourney's
	// injected exact-repeat pattern adds 3 more Steps) vs three hit=false
	// journeys (n=4, 4 Steps) give a real, genuinely different NetWorkingMS
	// median between the two groups — verified by hand: hit journeys land
	// at NetWorkingMS=360100 (ModelMS=700 + AgentExecMS=6*59900), no-hit
	// journeys at NetWorkingMS=180100 (ModelMS=400 + AgentExecMS=3*59900),
	// both exactly identical across their group's 3 journeys since
	// buildTestJourney(t, 4, hit) is otherwise deterministic.
	t.Run("group comparisons: hit vs no-hit medians, DeltaRel and Notable computed correctly", func(t *testing.T) {
		var journeys []*Journey
		for i := 0; i < 3; i++ {
			journeys = append(journeys, buildTestJourney(t, 4, true))
		}
		for i := 0; i < 3; i++ {
			journeys = append(journeys, buildTestJourney(t, 4, false))
		}
		stats := ComputeCorpusStats(journeys)

		var found *GroupComparison
		for i := range stats.GroupComparisons {
			if stats.GroupComparisons[i].Code == FindingExactRepeatToolCall {
				found = &stats.GroupComparisons[i]
			}
		}
		if found == nil {
			t.Fatalf("expected a GroupComparison for exact_repeat_tool_call, got: %+v (skipped: %v)", stats.GroupComparisons, stats.SkippedGroupComparisons)
		}
		if found.HitCount != 3 || found.NoHitCount != 3 {
			t.Errorf("HitCount/NoHitCount = %d/%d, want 3/3", found.HitCount, found.NoHitCount)
		}
		if found.HitMedian != 360100 {
			t.Errorf("HitMedian = %v, want 360100", found.HitMedian)
		}
		if found.NoHitMedian != 180100 {
			t.Errorf("NoHitMedian = %v, want 180100", found.NoHitMedian)
		}
		wantDeltaRel := (360100.0 - 180100.0) / 360100.0
		if math.Abs(found.DeltaRel-wantDeltaRel) > 1e-9 {
			t.Errorf("DeltaRel = %v, want %v", found.DeltaRel, wantDeltaRel)
		}
		// notableFloor[KindMillis]=2000ms and notableRelThreshold=0.30
		// (compare.go): |360100-180100|=180000 >= 2000 and
		// |DeltaRel|~=0.4999 >= 0.30, so this must be flagged Notable.
		if !found.Notable {
			t.Error("Notable = false, want true (both notableFloor[KindMillis] and notableRelThreshold are cleared)")
		}
	})
}

func TestRenderCorpusMarkdown(t *testing.T) {
	t.Run("empty corpus renders without panicking", func(t *testing.T) {
		md := RenderCorpusMarkdown(CorpusStats{}, i18n.EN)
		if md == "" {
			t.Error("expected non-empty output even for an empty corpus")
		}
	})

	t.Run("populated corpus renders all sections", func(t *testing.T) {
		var journeys []*Journey
		for i := 0; i < 6; i++ {
			journeys = append(journeys, buildTestJourney(t, 3+i, i%2 == 0))
		}
		stats := ComputeCorpusStats(journeys)
		md := RenderCorpusMarkdown(stats, i18n.EN)
		for _, want := range []string{"# Journey Corpus Report", "## Metric Distributions", "## Finding Hit Rates", "## Metric Correlations", "## Finding-Grouped Comparison"} {
			if !strings.Contains(md, want) {
				t.Errorf("rendered corpus report missing %q:\n%s", want, md)
			}
		}
	})

	t.Run("distribution table renders human units, not raw numbers (regression)", func(t *testing.T) {
		stats := CorpusStats{
			JourneyCount: 1,
			MetricDist: map[MetricCode]Distribution{
				MetricModelMS:             {Count: 1, Mean: 2500, Median: 2500, Min: 2500, Max: 2500, P90: 2500},
				MetricDuplicateActionRate: {Count: 1, Mean: 0.5, Median: 0.5, Min: 0.5, Max: 0.5, P90: 0.5},
			},
			GroupComparisons: []GroupComparison{
				{Code: "exact_repeat_tool_call", HitCount: 2, NoHitCount: 3, HitMedian: 4000, NoHitMedian: 2000, DeltaRel: 1.0, Notable: true},
			},
		}
		md := RenderCorpusMarkdown(stats, i18n.EN)
		if strings.Contains(md, "2500.00") {
			t.Errorf("KindMillis metric rendered as a raw float instead of via fmtutil.FmtSeconds:\n%s", md)
		}
		if strings.Contains(md, "0.50 | 0.50 | 0.50 | 0.50 | 0.50") {
			t.Errorf("KindRatio metric rendered as a raw fraction instead of a percentage:\n%s", md)
		}
		if strings.Contains(md, "4000ms") || strings.Contains(md, "2000ms") {
			t.Errorf("GroupComparison median rendered via the old ad-hoc ms formatter instead of formatMetric(KindMillis, ...):\n%s", md)
		}
	})

	t.Run("correlation table truncates to corpusCorrelationsShown, footnote reports the rest", func(t *testing.T) {
		const total = corpusCorrelationsShown + 5
		rows := make([]CorrelationRow, total)
		for i := 0; i < total; i++ {
			rows[i] = CorrelationRow{
				MetricA: MetricCode(fmt.Sprintf("syn_%02d_a", i)),
				MetricB: MetricCode(fmt.Sprintf("syn_%02d_b", i)),
				Rho:     1.0 - float64(i)*0.01, // strictly descending, matches the sort contract
				N:       10,
			}
		}
		stats := CorpusStats{JourneyCount: 1, Correlations: rows}
		md := RenderCorpusMarkdown(stats, i18n.EN)

		dataRows := 0
		for _, line := range strings.Split(md, "\n") {
			if strings.HasPrefix(line, "| syn_") {
				dataRows++
			}
		}
		if dataRows != corpusCorrelationsShown {
			t.Errorf("rendered %d correlation data rows, want exactly %d (corpusCorrelationsShown):\n%s", dataRows, corpusCorrelationsShown, md)
		}
		if !strings.Contains(md, "syn_00_a") || !strings.Contains(md, fmt.Sprintf("syn_%02d_a", corpusCorrelationsShown-1)) {
			t.Errorf("expected the top corpusCorrelationsShown rows (highest |rho|) in the table:\n%s", md)
		}
		if strings.Contains(md, fmt.Sprintf("syn_%02d_a", corpusCorrelationsShown)) {
			t.Errorf("row %d should have been truncated out of the table:\n%s", corpusCorrelationsShown, md)
		}
		wantFootnote := i18n.Corpus(i18n.EN).CorrelationMore(total - corpusCorrelationsShown)
		if !strings.Contains(md, wantFootnote) {
			t.Errorf("expected CorrelationMore(%d) footnote %q in:\n%s", total-corpusCorrelationsShown, wantFootnote, md)
		}
	})

	t.Run("nonzero journey count with empty findings/correlations/group comparisons hits the no-data branches", func(t *testing.T) {
		stats := CorpusStats{JourneyCount: 3} // no MetricDist/FindingRate/Correlations/GroupComparisons at all
		md := RenderCorpusMarkdown(stats, i18n.EN)
		et := i18n.Corpus(i18n.EN)
		for _, want := range []string{et.NoFindings, et.NoCorrelations, et.NoGroupComparisons} {
			if !strings.Contains(md, want) {
				t.Errorf("expected no-data text %q in rendered report:\n%s", want, md)
			}
		}
		// These branches must not be confused with the JourneyCount==0
		// early-return path — the report body (section titles) still
		// renders.
		if !strings.Contains(md, et.MetricDistTitle) {
			t.Errorf("expected metric distribution section title even with an empty MetricDist:\n%s", md)
		}
	})
}
