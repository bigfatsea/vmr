// Ver 2026-07-25, by Sonnet 5

// Derived-metric helpers, true per-bucket percentiles, and the small
// per-record extraction helpers shared by Build. Every finish* computes the
// bucket's derived fields (fresh tokens, cache_efficiency, slow_requests,
// true stream_ms percentiles) from raw sums/slices the accumulation pass
// already populated - no new I/O, no cross-bucket approximation.

package report

import (
	"encoding/json"
	"sort"
	"strconv"
)

// freshTokens returns in - cached - cacheWrite, floored at 0.
func freshTokens(in, cached, cacheWrite int64) int64 {
	f := in - cached - cacheWrite
	if f < 0 {
		f = 0
	}
	return f
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

// measuresInput is the raw per-bucket accumulation every one of the 6 Row
// types shares: latency samples plus the token-cache breakdown. All 6
// finishX functions below start by feeding their own fields through
// finishMeasures instead of repeating the same percentiles/freshTokens/
// cacheEff calls — that shared computation is as far as the unification
// goes: the 6 Row *struct declarations* are deliberately left untouched
// (their "same-name" fields carry inconsistent omitempty tags across
// types — e.g. Row.CacheEfficiency is omitempty, ClientRow.CacheEfficiency
// is not — so a shared embedded struct would change zero-value JSON output
// for some types).
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

// finishRow computes true percentiles + derived fields for a full Row.
func finishRow(r *Row) {
	m := finishMeasures(measuresInput{durs: r.durs, ttfts: r.ttfts, streamMS: r.streamMS,
		tokensIn: r.TokensIn, tokensInCached: r.TokensInCached, tokensInCacheWrite: r.TokensInCacheWrite, tokensKnown: r.TokensKnown})
	r.DurMSP50, r.DurMSP95 = m.durMSP50, m.durMSP95
	r.TTFTMSP50, r.TTFTMSP95 = m.ttftMSP50, m.ttftMSP95
	r.StreamMSP50, r.StreamMSP95 = m.streamMSP50, m.streamMSP95
	r.TokensInFresh = m.tokensInFresh
	r.CacheEfficiency = m.cacheEfficiency
	if r.Requests > 0 {
		r.SuccessRate = round2(float64(r.OK) / float64(r.Requests))
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
	r.durs, r.ttfts, r.streamMS = nil, nil, nil
}

func finishHour(h *HourRow) {
	m := finishMeasures(measuresInput{durs: h.durs, ttfts: h.ttfts, streamMS: h.streamMS,
		tokensIn: h.TokensIn, tokensInCached: h.TokensInCached, tokensInCacheWrite: h.TokensInCacheWrite, tokensKnown: h.TokensKnown})
	h.DurMSP50, h.DurMSP95 = m.durMSP50, m.durMSP95
	h.TTFTMSP50, h.TTFTMSP95 = m.ttftMSP50, m.ttftMSP95
	h.StreamMSP95 = m.streamMSP95
	h.TokensInFresh = m.tokensInFresh
	h.CacheEfficiency = m.cacheEfficiency
	h.durs, h.ttfts, h.streamMS = nil, nil, nil
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
		e.SuccessRate = round2(float64(e.RequestsOK) / float64(e.Requests))
	}
	if e.DurMSSum > 0 {
		e.TokOutPerSec = round2(float64(e.TokensOut) / (float64(e.DurMSSum) / 1000))
	}
	if len(e.ErrorClasses) == 0 {
		e.ErrorClasses = nil
	}
	e.durs, e.ttfts, e.streamMS, e.inToks, e.outToks = nil, nil, nil, nil, nil
}

func finishClient(c *ClientRow) {
	m := finishMeasures(measuresInput{durs: c.durs, ttfts: c.ttfts, streamMS: c.streamMS,
		tokensIn: c.TokensIn, tokensInCached: c.TokensInCached, tokensInCacheWrite: c.TokensInCacheWrite, tokensKnown: c.TokensKnown})
	c.DurMSP50, c.DurMSP95 = m.durMSP50, m.durMSP95
	c.TokensInFresh = m.tokensInFresh
	c.CacheEfficiency = m.cacheEfficiency
	c.InTokP50, c.InTokP95 = percentiles(c.inToks)
	c.OutTokP50, c.OutTokP95 = percentiles(c.outToks)
	if c.Requests > 0 {
		c.SuccessRate = round2(float64(c.OK) / float64(c.Requests))
	}
	c.durs, c.ttfts, c.streamMS, c.inToks, c.outToks = nil, nil, nil, nil, nil
}

func finishWorkload(w *WorkloadRow) {
	m := finishMeasures(measuresInput{durs: w.durs, streamMS: w.streamMS,
		tokensIn: w.TokensIn, tokensInCached: w.TokensInCached, tokensInCacheWrite: w.TokensInCacheWrite, tokensKnown: w.TokensKnown})
	w.DurMSP50, w.DurMSP95 = m.durMSP50, m.durMSP95
	w.TokensInFresh = m.tokensInFresh
	w.CacheEfficiency = m.cacheEfficiency
	if w.Requests > 0 {
		w.ToolCallRate = round2(float64(w.RequestsWithToolCalls) / float64(w.Requests))
	}
	w.durs, w.streamMS = nil, nil
}

func finishSession(s *SessionRow, info *SessionInfo) {
	m := finishMeasures(measuresInput{durs: s.durs, ttfts: s.ttfts,
		tokensIn: s.TokensIn, tokensInCached: s.TokensInCached, tokensInCacheWrite: s.TokensInCacheWrite, tokensKnown: s.TokensKnown})
	s.DurMSP95 = m.durMSP95
	s.TTFTMSP95 = m.ttftMSP95
	s.TokensInFresh = m.tokensInFresh
	s.CacheEfficiency = m.cacheEfficiency
	// context_growth: last-turn tokens_in / first-turn tokens_in (ts order).
	if info != nil && len(info.Recs) >= 2 {
		first := info.Recs[0].Usage.In
		last := info.Recs[len(info.Recs)-1].Usage.In
		if first > 0 {
			s.ContextGrowth = round2(float64(last) / float64(first))
		}
	}
	// compaction chain: assembled in the renderer from all sessions' links.
	if len(s.RoleChars) == 0 {
		s.RoleChars = nil
	}
	s.durs, s.ttfts = nil, nil
}

// buildFindings assembles the §7 efficiency/waste table from the finished
// buckets. One row per actionable finding, each naming the implicated entity
// and a suggested action.
//
// buildFindings runs BEFORE Build's own "---- sort all slices ----" pass
// (rep.Tools/rep.ByModel/rep.Workloads are still in whatever order their
// source map happened to iterate in when this function reads them — see
// aggregate.go's call site). Every "pick the worst one" search below must
// therefore find its answer by explicit comparison over the WHOLE bucket,
// with its own tie-break on the bucket's identity field — exactly the
// non-determinism class TestBuildIsDeterministic's doc comment describes,
// just one level removed (a *finding* picked from map-order data, not a
// *row* rendered in map-order). "First match, then break" over an
// as-yet-unsorted slice silently picks a different winner on every run
// whenever two rows tie on the filter condition — caught by
// TestBuildFindingsIsDeterministic comparing rep.Efficiency (not just
// rep.Workloads/Tools/ByModel themselves, which the sort later in Build
// does make deterministic) across repeated Build() calls.
func buildFindings(rep *Report2) []Finding {
	var out []Finding
	add := func(finding, metric, value, implicated, action string) {
		out = append(out, Finding{finding, metric, value, implicated, action})
	}

	// tool schema waste: the worst (highest SchemaWasteBytes, tie-broken by
	// Shape) among shapes under 20% declare-utilization — mirrors the
	// criteria rep.Tools' own later sort uses, so "the worst shape" means
	// the same thing here as it does in §7's own table.
	var worstTool *ToolShapeRow
	for i := range rep.Tools {
		t := &rep.Tools[i]
		if t.DeclareUtilization >= 0.20 || t.SchemaBytesShipped == 0 {
			continue
		}
		if worstTool == nil || t.SchemaWasteBytes > worstTool.SchemaWasteBytes ||
			(t.SchemaWasteBytes == worstTool.SchemaWasteBytes && t.Shape < worstTool.Shape) {
			worstTool = t
		}
	}
	if worstTool != nil {
		add("工具 schema 浪费", "schema_bytes_shipped",
			fmtBytesGB(worstTool.SchemaBytesShipped),
			worstTool.Shape+"/"+strconv.Itoa(worstTool.Requests)+" 请求",
			"裁剪未用工具；利用率 "+strconv.FormatFloat(float64(worstTool.DeclareUtilization)*100, 'f', 1, 64)+"%")
	}

	// cache miss input (global)
	if rep.Overall.TokensKnown > 0 {
		fresh := rep.Overall.TokensInFresh
		share := float64(fresh) / float64(rep.Overall.TokensIn) * 100
		// find the by-model split for the implicated note: the model with
		// the MOST fresh tokens (tie-broken by name), used only if it
		// actually accounts for at least half the global total — at most
		// one model can exceed half by construction, so finding the max
		// explicitly (instead of "first one seen that exceeds half")
		// removes the same ambient-order dependency as the two findings
		// above, even though a real tie here would need two models each
		// at exactly 50%.
		implicated := "全局"
		var domModel *Row
		for i := range rep.ByModel {
			m := &rep.ByModel[i]
			if domModel == nil || m.TokensInFresh > domModel.TokensInFresh ||
				(m.TokensInFresh == domModel.TokensInFresh && m.Model < domModel.Model) {
				domModel = m
			}
		}
		if domModel != nil && domModel.TokensInFresh > 0 && domModel.TokensInFresh >= fresh/2 {
			implicated = "全局，" + domModel.Model + " 占 " + fmtTokens(domModel.TokensInFresh)
		}
		add("缓存未命中输入", "cache_miss_tokens",
			fmtTokens(fresh)+" ("+strconv.FormatFloat(share, 'f', 1, 64)+"%)",
			implicated, "检查 prompt 前缀稳定性 / 开启 provider 缓存")
	}

	// scheduled-task redundancy (heartbeat/dream_diary low cache-eff): the
	// worst offender (highest TokensInFresh, tie-broken by Class) among the
	// two scheduled classes with cache_efficiency below 0.30 — this is the
	// exact case that was empirically observed flipping between "heartbeat"
	// and "dream_diary" from one otherwise-identical run to the next before
	// this fix, since both classes routinely sit at the same rounded ~1%
	// cache efficiency in real corpora.
	var worstWL *WorkloadRow
	for i := range rep.Workloads {
		w := &rep.Workloads[i]
		if (w.Class != "heartbeat" && w.Class != "dream_diary") || w.TokensKnown == 0 || w.CacheEfficiency >= 0.30 {
			continue
		}
		if worstWL == nil || w.TokensInFresh > worstWL.TokensInFresh ||
			(w.TokensInFresh == worstWL.TokensInFresh && w.Class < worstWL.Class) {
			worstWL = w
		}
	}
	if worstWL != nil {
		add("定时任务冗余", "fresh + cache_eff",
			fmtTokens(worstWL.TokensInFresh)+" fresh, 缓存效率 "+pctStr(worstWL.CacheEfficiency),
			worstWL.Class, "拉长间隔 / 换便宜模型 / 缓存前缀")
	}

	// output truncation
	trunc := rep.Overall.Truncated
	// finish=length is a stronger truncation signal; approximate from overall if available
	if trunc > 0 || rep.Overall.Requests > 0 {
		// count finish=length from sessions/tools? Truncated field covers stream breaks;
		// finish=length is separate. We report truncated stream breaks here.
		if trunc > 0 {
			add("输出截断", "truncated",
				strconv.Itoa(trunc)+"/"+strconv.Itoa(rep.Overall.Requests),
				"stream 中断", "排查上游超时 / 提高 stream_idle")
		}
	}

	// slow requests
	if rep.Overall.RequestsWithDur > 0 {
		slow := rep.Overall.SlowRequests
		if slow > 0 {
			share := float64(slow) / float64(rep.Overall.RequestsWithDur) * 100
			add("慢请求", "slow_request_share",
				"~"+strconv.FormatFloat(share, 'f', 0, 64)+"% > "+strconv.Itoa(SlowThresholdMS/1000)+"s",
				"见 §4 stream_ms 归因", "见 §4")
		}
	}

	// context growth (worst session) — tie-broken by ID for the same
	// reason as the three findings above: on an exact ContextGrowth tie
	// (plausible since it's rounded to 1 decimal for display), a bare ">"
	// comparison keeps whichever session was encountered first in
	// rep.Sessions' as-yet-unsorted order, which is not deterministic.
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
	if worst != nil && worst.ContextGrowth >= 5 {
		add("上下文膨胀", "context_growth",
			"×"+strconv.FormatFloat(float64(worst.ContextGrowth), 'f', 1, 64),
			worst.ID+" "+worst.Title, "中途 compaction")
	}

	return out
}

// ---- per-record extraction helpers (recompute fields ReqInfo keeps
//      unexported: bytes, tool-decl bytes, endpoint, error class) ----
//
// bodyBytes (sizing a recorded body) is shared with render.go — same
// package now, one definition.

// toolDeclInfo returns (count, serializedBytes) of the request's "tools"
// array. Mirrors what ReqInfo.declBytes captures (unexported).
func toolDeclInfo(body any) (count int, bytes int64) {
	m, ok := body.(map[string]any)
	if !ok {
		return 0, 0
	}
	tools, ok := m["tools"].([]any)
	if !ok || len(tools) == 0 {
		return 0, 0
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return len(tools), 0
	}
	return len(tools), int64(len(raw))
}

// ---- formatting helpers (shared by render + findings) ----

func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 2, 64) + "B"
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 2, 64) + "M"
	case n >= 1_000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

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

func pctStr(f float64) string {
	return strconv.FormatFloat(float64(f)*100, 'f', 1, 64) + "%"
}
