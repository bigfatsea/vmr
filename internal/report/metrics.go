// Ver 2026-09-22 18:05, by coding

// Derived-metric helpers, true per-bucket percentiles, and the small
// per-record extraction helpers shared by Build. Every finish* computes the
// bucket's derived fields (fresh tokens, cache_efficiency, slow_requests,
// true stream_ms percentiles) from raw sums/slices the accumulation pass
// already populated - no new I/O, no cross-bucket approximation.

package report

import (
	"sort"
	"strconv"

	"vmr/internal/chatmsg"
	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// freshTokens returns in - cached - cacheWrite, floored at 0 — a thin
// wrapper over chatmsg.Usage.Fresh() for the one caller here (finishRow,
// below) that only has the three fields unpacked onto a Measures
// accumulator, not a chatmsg.Usage value to call the method on directly.
// Callers that already hold a chatmsg.Usage (recextract.go, cost.go) call
// .Fresh() on it directly instead of going through this wrapper.
func freshTokens(in, cached, cacheWrite int64) int64 {
	return chatmsg.Usage{In: in, CacheRead: cached, CacheWrite: cacheWrite}.Fresh()
}

// cacheEff is cached / (cached + fresh) - the cost lever. 0 when no fresh
// input existed at all (pure cache reads) is reported as 0, not 100, to stay
// consistent with "how much of the billable input did the cache absorb" - a
// workload with zero fresh input has nothing to optimize.
func cacheEff(cached, fresh int64) float64 {
	denom := cached + fresh
	if denom <= 0 {
		return 0
	}
	return round2(float64(cached) / float64(denom))
}

// cacheHitRate is cached / in (the headline ratio). 0 when in == 0.
func cacheHitRate(cached, in int64) float64 {
	if in <= 0 {
		return 0
	}
	return round2(float64(cached) / float64(in))
}

// percentiles returns nearest-rank p50 and p95 from a raw slice.
func percentiles(xs []int64) (p50, p95 int64) {
	if len(xs) == 0 {
		return 0, 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	rank := func(p float64) int64 {
		i := int(p*float64(len(s))+0.5) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i]
	}
	return rank(0.50), rank(0.95)
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// measuresInput is the raw accumulation finishMeasures turns into
// percentiles/freshTokens/cacheEff. Only two callers left: TrafficStats.Finish
// (below), for the dur-percentile + token-derived fields the 5 embedding Row
// types share (see rows.go), and finishEndpoint, the one bucket that still
// needs all five fields at once (EndpointRow deliberately doesn't embed
// TrafficStats — see that type's doc comment). finishRow/finishHour/
// finishSession call percentiles(ttfts)/percentiles(streamMS) directly
// instead of routing through here — building a measuresInput just to read
// two of its six fields back out was worse than calling the primitive.
type measuresInput struct {
	durs, ttfts, streamMS                        []int64
	tokensIn, tokensInCached, tokensInCacheWrite int64
	tokensKnown                                  int
}

type measuresResult struct {
	durMSP50, durMSP95       int64
	ttftMSP50, ttftMSP95     int64
	streamMSP50, streamMSP95 int64
	tokensInFresh            int64
	cacheEfficiency          float64
}

func finishMeasures(in measuresInput) measuresResult {
	var out measuresResult
	out.durMSP50, out.durMSP95 = percentiles(in.durs)
	out.ttftMSP50, out.ttftMSP95 = percentiles(in.ttfts)
	out.streamMSP50, out.streamMSP95 = percentiles(in.streamMS)
	out.tokensInFresh = freshTokens(in.tokensIn, in.tokensInCached, in.tokensInCacheWrite)
	if in.tokensKnown > 0 {
		out.cacheEfficiency = cacheEff(in.tokensInCached, out.tokensInFresh)
	}
	return out
}

// Finish computes the shared core's true percentiles + derived fields
// (DurMSP50/95, TokensInFresh, CacheEfficiency) from the raw samples
// Ingest accumulated — see TrafficStats' own doc comment (rows.go) for why
// TTFT/stream stay outside it, computed by each embedding row's own Finish
// wrapper below instead.
func (s *TrafficStats) Finish() {
	m := finishMeasures(measuresInput{durs: s.durs,
		tokensIn: s.TokensIn, tokensInCached: s.TokensInCached, tokensInCacheWrite: s.TokensInCacheWrite, tokensKnown: s.TokensKnown})
	s.DurMSP50, s.DurMSP95 = m.durMSP50, m.durMSP95
	s.TokensInFresh = m.tokensInFresh
	s.CacheEfficiency = m.cacheEfficiency
	s.durs = nil
}

// finishRow computes true percentiles + derived fields for a full Row.
func finishRow(r *Row) {
	r.TrafficStats.Finish()
	r.TTFTMSP50, r.TTFTMSP95 = percentiles(r.ttfts)
	r.StreamMSP50, r.StreamMSP95 = percentiles(r.streamMS)
	if r.Requests > 0 {
		r.SuccessRate = float64(r.OK) / float64(r.Requests)
		r.TokensCoveragePct = round2(float64(r.TokensKnown) / float64(r.Requests) * 100)
	}
	if r.RequestsWithDur > 0 && r.RequestsWithDur < 20 {
		r.DurLowN = true
	}
	if r.TokensIn > 0 {
		r.CacheHitRate = cacheHitRate(r.TokensInCached, r.TokensIn)
	}
	if r.TokensOut > 0 && r.TokensReasoning > 0 {
		r.ReasoningShare = round2(float64(r.TokensReasoning) / float64(r.TokensOut))
	}
	if r.tokDurMS > 0 {
		r.TokOutPerSec = round2(float64(r.TokensOut) / (float64(r.tokDurMS) / 1000))
	}
	if len(r.RoleChars) == 0 {
		r.RoleChars = nil
	}
	if len(r.RoleTokens) == 0 {
		r.RoleTokens = nil
	}
	r.ttfts, r.streamMS = nil, nil
}

func finishHour(h *HourRow) {
	h.TrafficStats.Finish()
	h.TTFTMSP50, h.TTFTMSP95 = percentiles(h.ttfts)
	_, h.StreamMSP95 = percentiles(h.streamMS)
	h.ttfts, h.streamMS = nil, nil
}

func finishEndpoint(e *EndpointRow) {
	m := finishMeasures(measuresInput{durs: e.durs, ttfts: e.ttfts, streamMS: e.streamMS,
		tokensIn: e.TokensIn, tokensInCached: e.TokensInCached, tokensInCacheWrite: e.TokensInCacheWrite, tokensKnown: e.TokensKnown})
	e.DurMSP50, e.DurMSP95 = m.durMSP50, m.durMSP95
	e.TTFTMSP50, e.TTFTMSP95 = m.ttftMSP50, m.ttftMSP95
	e.StreamMSP95 = m.streamMSP95
	e.TokensInFresh = m.tokensInFresh
	e.CacheEfficiency = m.cacheEfficiency
	e.InTokP50, e.InTokP95 = percentiles(e.inToks)
	e.OutTokP50, e.OutTokP95 = percentiles(e.outToks)
	if e.Attempts > 0 {
		e.Availability = round2(float64(e.OK) / float64(e.Attempts))
		e.ErrorRate = round2(float64(e.Failed) / float64(e.Attempts) * 100)
	}
	if e.Requests > 0 {
		e.SuccessRate = float64(e.RequestsOK) / float64(e.Requests)
		e.TokensCoveragePct = round2(float64(e.TokensKnown) / float64(e.Requests) * 100)
	}
	if e.RequestsWithDur > 0 && e.RequestsWithDur < 20 {
		e.DurLowN = true
	}
	if e.tokDurMS > 0 {
		e.TokOutPerSec = round2(float64(e.TokensOut) / (float64(e.tokDurMS) / 1000))
	}
	if len(e.ErrorClasses) == 0 {
		e.ErrorClasses = nil
	}
	e.durs, e.ttfts, e.streamMS, e.inToks, e.outToks = nil, nil, nil, nil, nil
}

func finishClient(c *ClientRow) {
	c.TrafficStats.Finish()
	c.InTokP50, c.InTokP95 = percentiles(c.inToks)
	c.OutTokP50, c.OutTokP95 = percentiles(c.outToks)
	if c.Requests > 0 {
		c.SuccessRate = float64(c.OK) / float64(c.Requests)
		c.TokensCoveragePct = round2(float64(c.TokensKnown) / float64(c.Requests) * 100)
	}
	if c.RequestsWithDur > 0 && c.RequestsWithDur < 20 {
		c.DurLowN = true
	}
	c.inToks, c.outToks = nil, nil
}

func finishWorkload(w *WorkloadRow) {
	w.TrafficStats.Finish()
	if w.Requests > 0 {
		w.ToolCallRate = round2(float64(w.RequestsWithToolCalls) / float64(w.Requests))
	}
}

func finishSession(s *SessionRow, info *SessionInfo) {
	s.TrafficStats.Finish()
	_, s.TTFTMSP95 = percentiles(s.ttfts)
	// context_growth: last-turn tokens_in / first-turn tokens_in (ts order).
	// Safe to compare across the whole session since group() now splits a
	// session at every Contract/Fork edit (one SessionInfo per
	// ctxgraph.Lineage), so info.Recs can no longer straddle a hidden
	// history reset the way it used to (the dirty ContextGrowth value case)
	// — see TestContextGrowthDoesNotCrossContractBreak.
	if info != nil && len(info.Recs) >= 2 {
		first := contextGrowthIn(info.Recs[0])
		last := contextGrowthIn(info.Recs[len(info.Recs)-1])
		if first > 0 {
			s.ContextGrowth = round2(float64(last) / float64(first))
		}
	}
	// compaction chain: assembled in the renderer from all sessions' links.
	if len(s.RoleChars) == 0 {
		s.RoleChars = nil
	}
	s.ttfts = nil
}

// contextGrowthIn returns a session-turn's tokens_in for the ContextGrowth
// ratio: the upstream-reported usage when it exists, else the degraded
// byte-count estimate (manifest.EstIn, computed by ctxgraph on the same
// EstimateDegradedTokens basis report's own cost path uses). Without the
// fallback, a session whose opening turn's upstream reported no usage
// (opaque response, no usage field, or a stream truncated before any
// usage-bearing block) would keep ContextGrowth at 0 forever — the ratio
// would read "no growth" when it really means "no first baseline" (review
// P-06). Mixing one sniffed and one estimated figure across the ratio is
// the same tradeoff the degraded-cost path already makes: the estimate is
// the closest available proxy for the true input count, and a calibrated
// ratio is strictly more informative than a silent 0.
func contextGrowthIn(r *ReqInfo) int64 {
	// In-side gate: the ratio measures the input baseline, so only the In
	// side of the usage ledger counts as known (see
	// chatmsg.ExtractUsageSides for the side rule).
	if r.UsageInOK {
		return r.Usage.In
	}
	if r.manifest != nil {
		return r.manifest.EstIn
	}
	return 0
}

// freshestModel returns the ByModel row with the most fresh (cache-missed)
// input tokens, for the cache-miss finding's "mostly this model" note.
//
// The max is found explicitly rather than by taking the first row that
// exceeds half the total, for the same reason the two findings above it do:
// buildFindings runs before aggregate.go sorts rep.ByModel, so the slice is
// still in Go map-iteration order and "first one seen" is not reproducible
// across runs.
//
// The tie-break is (fresh desc, model asc, protocol asc), and protocol is not
// decorative: ByModel is keyed by model+protocol (aggregate.go's mk), so one
// model name can occupy two rows. Name alone is therefore not a total order
// over these rows and a tie would fall back to map order. No caller reads a
// field that could differ under such a tie today — a tie needs equal names
// and equal counts, and only those two are read — but that is what makes the
// arbitrary pick safe, and it is not a property to leave resting on the
// caller happening not to grow a third field read.
func freshestModel(rows []Row) *Row {
	var best *Row
	for i := range rows {
		m := &rows[i]
		if best == nil || m.TokensInFresh > best.TokensInFresh ||
			(m.TokensInFresh == best.TokensInFresh &&
				(m.Model < best.Model ||
					(m.Model == best.Model && m.Protocol < best.Protocol))) {
			best = m
		}
	}
	return best
}

// buildFindings assembles the §7 efficiency/waste table from the finished
// buckets. One row per actionable finding, each naming the implicated entity
// and a suggested action.
//
// buildFindings dispatches to one independent finder per Code (below) and
// collects whichever ones fired — split into named helpers, rather than one
// long function, once every finder gained its own Params block.
//
// Every finder runs BEFORE Build's own "---- sort all slices ----" pass
// (rep.Tools/rep.ByModel/rep.Workloads are still in whatever order their
// source map happened to iterate in when a finder reads them — see
// aggregate.go's call site). Every "pick the worst one" search therefore
// must find its answer by explicit comparison over the WHOLE bucket, with
// its own tie-break on the bucket's identity field — exactly the
// non-determinism class TestBuildIsDeterministic's doc comment describes,
// just one level removed (a *finding* picked from map-order data, not a
// *row* rendered in map-order). "First match, then break" over an
// as-yet-unsorted slice silently picks a different winner on every run
// whenever two rows tie on the filter condition — caught by
// TestBuildFindingsIsDeterministic comparing rep.Efficiency (not just
// rep.Workloads/Tools/ByModel themselves, which the sort later in Build
// does make deterministic) across repeated Build() calls.
func buildFindings(rep *Report2, lang i18n.Lang) []Finding {
	var out []Finding
	for _, f := range []*Finding{
		findToolSchemaWaste(rep, lang),
		findCacheMiss(rep, lang),
		findCronRedundancy(rep, lang),
		findOutputTruncation(rep, lang),
		findSlowRequests(rep, lang),
		findContextGrowth(rep, lang),
		quotaExhaustionFinding(rep, lang), // findings_quota.go
	} {
		if f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// findToolSchemaWaste picks the worst (highest SchemaWasteBytes, tie-broken
// by Shape) among shapes under 20% declare-utilization — mirrors the
// criteria rep.Tools' own later sort uses, so "the worst shape" means the
// same thing here as it does in §7's own table.
func findToolSchemaWaste(rep *Report2, lang i18n.Lang) *Finding {
	var worst *ToolShapeRow
	for i := range rep.Tools {
		t := &rep.Tools[i]
		if t.DeclareUtilization >= 0.20 || t.SchemaBytesShipped == 0 {
			continue
		}
		if worst == nil || t.SchemaWasteBytes > worst.SchemaWasteBytes ||
			(t.SchemaWasteBytes == worst.SchemaWasteBytes && t.Shape < worst.Shape) {
			worst = t
		}
	}
	if worst == nil {
		return nil
	}
	ft := i18n.Efficiency(lang).ToolSchemaWasteFinding(
		worst.Shape, worst.Requests, fmtBytesGB(worst.SchemaBytesShipped),
		strconv.FormatFloat(float64(worst.DeclareUtilization)*100, 'f', 1, 64))
	return &Finding{
		Code: FindingToolSchemaWaste, Finding: ft.Title, Metric: "schema_bytes_shipped",
		Value: ft.Value, Implicated: ft.Implicated, Action: ft.Action,
		Params: map[string]string{
			"shape":                worst.Shape,
			"requests":             strconv.Itoa(worst.Requests),
			"schema_bytes_shipped": strconv.FormatInt(worst.SchemaBytesShipped, 10),
			"declare_utilization":  strconv.FormatFloat(worst.DeclareUtilization, 'f', -1, 64),
		},
	}
}

// findCacheMiss reports the global fresh-input share, naming the dominant
// model behind it when one model alone accounts for at least half of it.
func findCacheMiss(rep *Report2, lang i18n.Lang) *Finding {
	if rep.Overall.TokensKnown <= 0 {
		return nil
	}
	fresh := rep.Overall.TokensInFresh
	share := float64(fresh) / float64(rep.Overall.TokensIn) * 100
	dominantModel, dominantTokens := "", ""
	var dominantModelFresh int64
	if m := freshestModel(rep.ByModel); m != nil && m.TokensInFresh > 0 && m.TokensInFresh >= fresh/2 {
		dominantModel, dominantTokens = m.Model, fmtutil.FmtTokens(m.TokensInFresh)
		dominantModelFresh = m.TokensInFresh
	}
	params := map[string]string{
		"fresh_tokens": strconv.FormatInt(fresh, 10),
		"share_pct":    strconv.FormatFloat(share, 'f', -1, 64),
	}
	if dominantModel != "" {
		params["dominant_model"] = dominantModel
		params["dominant_tokens"] = strconv.FormatInt(dominantModelFresh, 10)
	}
	ft := i18n.Efficiency(lang).CacheMissFinding(
		fmtutil.FmtTokens(fresh), strconv.FormatFloat(share, 'f', 1, 64), dominantModel, dominantTokens)
	return &Finding{
		Code: FindingCacheMiss, Finding: ft.Title, Metric: "cache_miss_tokens",
		Value: ft.Value, Implicated: ft.Implicated, Action: ft.Action, Params: params,
	}
}

// findCronRedundancy picks the worst offender (highest TokensInFresh,
// tie-broken by Class) among the two scheduled classes (heartbeat/
// dream_diary) with cache_efficiency below 0.30 — this is the exact case
// that was empirically observed flipping between "heartbeat" and
// "dream_diary" from one otherwise-identical run to the next before this
// fix, since both classes routinely sit at the same rounded ~1% cache
// efficiency in real corpora.
func findCronRedundancy(rep *Report2, lang i18n.Lang) *Finding {
	var worst *WorkloadRow
	for i := range rep.Workloads {
		w := &rep.Workloads[i]
		if (w.Class != "heartbeat" && w.Class != "dream_diary") || w.TokensKnown == 0 || w.CacheEfficiency >= 0.30 {
			continue
		}
		if worst == nil || w.TokensInFresh > worst.TokensInFresh ||
			(w.TokensInFresh == worst.TokensInFresh && w.Class < worst.Class) {
			worst = w
		}
	}
	if worst == nil {
		return nil
	}
	ft := i18n.Efficiency(lang).CronRedundancyFinding(
		fmtutil.FmtTokens(worst.TokensInFresh), pctStr(worst.CacheEfficiency), worst.Class)
	return &Finding{
		Code: FindingCronRedundancy, Finding: ft.Title, Metric: "fresh + cache_eff",
		Value: ft.Value, Implicated: ft.Implicated, Action: ft.Action,
		Params: map[string]string{
			"fresh_tokens": strconv.FormatInt(worst.TokensInFresh, 10),
			"cache_eff":    strconv.FormatFloat(worst.CacheEfficiency, 'f', -1, 64),
			"class":        worst.Class,
		},
	}
}

// findOutputTruncation fires when any stream broke off truncated this window.
func findOutputTruncation(rep *Report2, lang i18n.Lang) *Finding {
	trunc := rep.Overall.Truncated
	if trunc <= 0 {
		return nil
	}
	ft := i18n.Efficiency(lang).OutputTruncationFinding(trunc, rep.Overall.Requests)
	return &Finding{
		Code: FindingOutputTruncation, Finding: ft.Title, Metric: "truncated",
		Value: ft.Value, Implicated: ft.Implicated, Action: ft.Action,
		Params: map[string]string{
			"truncated": strconv.Itoa(trunc),
			"requests":  strconv.Itoa(rep.Overall.Requests),
		},
	}
}

// findSlowRequests fires when any request crossed SlowThresholdMS.
func findSlowRequests(rep *Report2, lang i18n.Lang) *Finding {
	if rep.Overall.RequestsWithDur <= 0 {
		return nil
	}
	slow := rep.Overall.SlowRequests
	if slow <= 0 {
		return nil
	}
	share := float64(slow) / float64(rep.Overall.RequestsWithDur) * 100
	ft := i18n.Efficiency(lang).SlowRequestsFinding(strconv.FormatFloat(share, 'f', 0, 64), SlowThresholdMS/1000)
	return &Finding{
		Code: FindingSlowRequests, Finding: ft.Title, Metric: "slow_request_share",
		Value: ft.Value, Implicated: ft.Implicated, Action: ft.Action,
		Params: map[string]string{
			"share_pct":         strconv.FormatFloat(share, 'f', -1, 64),
			"slow":              strconv.Itoa(slow),
			"requests_with_dur": strconv.Itoa(rep.Overall.RequestsWithDur),
			"threshold_s":       strconv.Itoa(SlowThresholdMS / 1000),
		},
	}
}

// findContextGrowth picks the worst session (highest ContextGrowth,
// tie-broken by ID for determinism over rep.Sessions' as-yet-unsorted
// order — see buildFindings' own doc history), reporting only when growth
// reaches 5x.
func findContextGrowth(rep *Report2, lang i18n.Lang) *Finding {
	var worst *SessionRow
	for i := range rep.Sessions {
		s := &rep.Sessions[i]
		if s.ContextGrowth <= 0 {
			continue
		}
		if worst == nil || s.ContextGrowth > worst.ContextGrowth ||
			(s.ContextGrowth == worst.ContextGrowth && s.ID < worst.ID) {
			worst = s
		}
	}
	if worst == nil || worst.ContextGrowth < 5 {
		return nil
	}
	ft := i18n.Efficiency(lang).ContextGrowthFinding(
		strconv.FormatFloat(float64(worst.ContextGrowth), 'f', 1, 64), worst.ID, worst.Title)
	return &Finding{
		Code: FindingContextGrowth, Finding: ft.Title, Metric: "context_growth",
		Value: ft.Value, Implicated: ft.Implicated, Action: ft.Action,
		Params: map[string]string{
			// session_title is the RAW value, unescaped — Params holds
			// original values, never pre-escaped text, and so does the
			// narrative built above now that the Markdown table escapes
			// its Implicated cell at projection time (viewmodel_efficiency.go).
			"context_growth": strconv.FormatFloat(float64(worst.ContextGrowth), 'f', -1, 64),
			"session_id":     worst.ID,
			"session_title":  worst.Title,
		},
	}
}

// buildFindingsForJSON is buildFindings fixed to English — the only call
// Build itself makes (aggregate.go), so this is Report2.Efficiency's
// language-agnostic default: a deterministic baseline Build computes
// without needing a lang parameter. Nothing overwrites rep.Efficiency
// afterward (R1: the persisted JSON stays language-invariant regardless of
// -lang — see BuildSummarySlice, which reads it as-is). Kept as its own
// named function (not an inline i18n.EN literal at the call site) so
// aggregate.go's own call site never needs to import internal/i18n itself
// — see that file's line-count budget note.
//
// viewmodel_efficiency.go's own Markdown renderer deliberately does NOT read
// rep.Efficiency — it computes its own independent buildFindings(rep, lang)
// call with the report's actual display language, so Markdown rendering
// never depends on this English baseline at all.
func buildFindingsForJSON(rep *Report2) []Finding {
	return buildFindings(rep, i18n.EN)
}

// ---- formatting helpers (shared by render + findings) ----

func fmtBytesGB(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 2, 64) + " GB"
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 2, 64) + " MB"
	case n >= 1_000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + " KB"
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
}

// pctStr is report's local 1-decimal alias for fmtutil.FmtPercent — same
// thin-wrapper convention as render.go's fmtBytes, so the ~15 call sites
// across this package don't all need the "fmtutil." qualifier.
func pctStr(f float64) string {
	return fmtutil.FmtPercent(f, 1)
}
