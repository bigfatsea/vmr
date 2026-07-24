// Ver 2026-07-25, report2

// Derived-metric helpers, true per-bucket percentiles, and the small
// per-record extraction helpers shared by Build. Every finish* computes the
// bucket's derived fields (fresh tokens, cache_efficiency, slow_requests,
// true stream_ms percentiles) from raw sums/slices the accumulation pass
// already populated - no new I/O, no cross-bucket approximation.

package report2

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/report"
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

// percentiles returns nearest-rank p50 and p95 from a raw slice. Identical
// semantics to the legacy report.percentiles so cross-validation holds.
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

// finishRow computes true percentiles + derived fields for a full Row.
func finishRow(r *Row) {
	r.DurMSP50, r.DurMSP95 = percentiles(r.durs)
	r.TTFTMSP50, r.TTFTMSP95 = percentiles(r.ttfts)
	r.StreamMSP50, r.StreamMSP95 = percentiles(r.streamMS)
	r.TokensInFresh = freshTokens(r.TokensIn, r.TokensInCached, r.TokensInCacheWrite)
	if r.Requests > 0 {
		r.SuccessRate = round2(float64(r.OK) / float64(r.Requests))
	}
	if r.TokensIn > 0 {
		r.CacheHitRate = cacheHitRate(r.TokensInCached, r.TokensIn)
	}
	if r.TokensKnown > 0 {
		r.CacheEfficiency = cacheEff(r.TokensInCached, r.TokensInFresh)
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
	r.durs, r.ttfts, r.streamMS = nil, nil, nil
}

func finishHour(h *HourRow) {
	h.DurMSP50, h.DurMSP95 = percentiles(h.durs)
	h.TTFTMSP50, h.TTFTMSP95 = percentiles(h.ttfts)
	_, h.StreamMSP95 = percentiles(h.streamMS)
	h.TokensInFresh = freshTokens(h.TokensIn, h.TokensInCached, h.TokensInCacheWrite)
	if h.TokensKnown > 0 {
		h.CacheEfficiency = cacheEff(h.TokensInCached, h.TokensInFresh)
	}
	h.durs, h.ttfts, h.streamMS = nil, nil, nil
}

func finishEndpoint(e *EndpointRow) {
	e.DurMSP50, e.DurMSP95 = percentiles(e.durs)
	e.TTFTMSP50, e.TTFTMSP95 = percentiles(e.ttfts)
	_, e.StreamMSP95 = percentiles(e.streamMS)
	e.TokensInFresh = freshTokens(e.TokensIn, e.TokensInCached, e.TokensInCacheWrite)
	if e.Attempts > 0 {
		e.Availability = round2(float64(e.OK) / float64(e.Attempts))
		e.ErrorRate = round2(float64(e.Failed) / float64(e.Attempts) * 100)
	}
	if e.TokensKnown > 0 {
		e.CacheEfficiency = cacheEff(e.TokensInCached, e.TokensInFresh)
	}
	if e.DurMSSum > 0 {
		e.TokOutPerSec = round2(float64(e.TokensOut) / (float64(e.DurMSSum) / 1000))
	}
	if len(e.ErrorClasses) == 0 {
		e.ErrorClasses = nil
	}
	e.durs, e.ttfts, e.streamMS = nil, nil, nil
}

func finishClient(c *ClientRow) {
	c.DurMSP50, c.DurMSP95 = percentiles(c.durs)
	c.TokensInFresh = freshTokens(c.TokensIn, c.TokensInCached, c.TokensInCacheWrite)
	if c.Requests > 0 {
		c.SuccessRate = round2(float64(c.OK) / float64(c.Requests))
	}
	if c.TokensKnown > 0 {
		c.CacheEfficiency = cacheEff(c.TokensInCached, c.TokensInFresh)
	}
	c.durs, c.ttfts, c.streamMS = nil, nil, nil
}

func finishWorkload(w *WorkloadRow) {
	w.DurMSP50, w.DurMSP95 = percentiles(w.durs)
	w.TokensInFresh = freshTokens(w.TokensIn, w.TokensInCached, w.TokensInCacheWrite)
	if w.TokensKnown > 0 {
		w.CacheEfficiency = cacheEff(w.TokensInCached, w.TokensInFresh)
	}
	if w.Requests > 0 {
		w.ToolCallRate = round2(float64(w.RequestsWithToolCalls) / float64(w.Requests))
	}
	w.durs, w.streamMS = nil, nil
}

func finishSession(s *SessionRow, info *report.SessionInfo) {
	s.DurMSP95 = p95Only(s.durs)
	s.TTFTMSP95 = p95Only(s.ttfts)
	s.TokensInFresh = freshTokens(s.TokensIn, s.TokensInCached, s.TokensInCacheWrite)
	if s.TokensKnown > 0 {
		s.CacheEfficiency = cacheEff(s.TokensInCached, s.TokensInFresh)
	}
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

func p95Only(xs []int64) int64 {
	_, p95 := percentiles(xs)
	return p95
}

// buildFindings assembles the §7 efficiency/waste table from the finished
// buckets. One row per actionable finding, each naming the implicated entity
// and a suggested action.
func buildFindings(rep *Report2) []Finding {
	var out []Finding
	add := func(finding, metric, value, implicated, action string) {
		out = append(out, Finding{finding, metric, value, implicated, action})
	}

	// tool schema waste (top shape)
	for _, t := range rep.Tools {
		if t.DeclareUtilization < 0.20 && t.SchemaBytesShipped > 0 {
			add("工具 schema 浪费", "schema_bytes_shipped",
				fmtBytes(t.SchemaBytesShipped),
				t.Shape+"/"+strconv.Itoa(t.Requests)+" 请求",
				"裁剪未用工具；利用率 "+strconv.FormatFloat(float64(t.DeclareUtilization)*100, 'f', 1, 64)+"%")
			break // one finding per report (the worst shape)
		}
	}

	// cache miss input (global)
	if rep.Overall.TokensKnown > 0 {
		fresh := rep.Overall.TokensInFresh
		share := float64(fresh) / float64(rep.Overall.TokensIn) * 100
		// find the by-model split for the implicated note
		implicated := "全局"
		for _, m := range rep.ByModel {
			if m.TokensInFresh > 0 && m.TokensInFresh >= fresh/2 {
				implicated = "全局，" + m.Model + " 占 " + fmtTokens(m.TokensInFresh)
				break
			}
		}
		add("缓存未命中输入", "cache_miss_tokens",
			fmtTokens(fresh)+" ("+strconv.FormatFloat(share, 'f', 1, 64)+"%)",
			implicated, "检查 prompt 前缀稳定性 / 开启 provider 缓存")
	}

	// scheduled-task redundancy (heartbeat/dream_diary low cache-eff)
	for _, w := range rep.Workloads {
		if (w.Class == "heartbeat" || w.Class == "dream_diary") && w.TokensKnown > 0 && w.CacheEfficiency < 0.30 {
			add("定时任务冗余", "fresh + cache_eff",
				fmtTokens(w.TokensInFresh)+" fresh, 缓存效率 "+pctStr(w.CacheEfficiency),
				w.Class, "拉长间隔 / 换便宜模型 / 缓存前缀")
			break
		}
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

	// context growth (worst session)
	var worst *SessionRow
	for i := range rep.Sessions {
		s := &rep.Sessions[i]
		if s.ContextGrowth > 0 && (worst == nil || s.ContextGrowth > worst.ContextGrowth) {
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

// bodyBytes sizes a recorded body: JSON bodies by re-serialization, string
// bodies (SSE) by length. Mirrors the legacy report.bodyBytes.
func bodyBytes(body any) int64 {
	switch b := body.(type) {
	case nil:
		return 0
	case string:
		return int64(len(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0
		}
		return int64(len(raw))
	}
}

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

// attemptErrClass returns an attempt's typed error class, falling back to
// parsing the free-text Error field for logs written before ErrorClass
// existed. Replicates the legacy report.attemptErrorClass (unexported).
func attemptErrClass(a audit.Attempt) string {
	if a.ErrorClass != "" {
		return a.ErrorClass
	}
	if a.Error == "" {
		return ""
	}
	if i := strings.IndexByte(a.Error, ':'); i > 0 {
		return a.Error[:i]
	}
	return a.Error
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

func fmtBytes(n int64) string {
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
