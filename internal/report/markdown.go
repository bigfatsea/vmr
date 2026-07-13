// Ver 2026-07-13 04:00, by Sonnet 5
package report

import (
	"fmt"
	"sort"
	"strings"
)

// Markdown renders the report's most-consulted views. Every table iterates
// its own pre-aggregated bucket directly — no cross-bucket roll-up, no
// weighted-expansion approximation, no percentile derived by taking the max
// of other percentiles (percentiles aren't additive; only a true per-bucket
// computation from raw values is correct). Tier 1 (overview, per-model
// summary, endpoint availability) and tier 2 (daily trend, error classes)
// come from the pre-aggregated buckets; finer cuts live in the JSON.
func Markdown(rep *Report) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	overall := rep.Overall

	w("# VMR 用量报告\n\n")
	w("详单见 [vmr-requests-index.md](./vmr-requests-index.md) · 数据源: %s · 同名 .json（format %d）\n\n",
		strings.Join(rep.Meta.Inputs, ", "), rep.Meta.Format)
	w("时段 %s — %s · 生成于 %s · 解析 %d 条记录（%d 条坏行跳过）\n\n",
		cut(rep.Meta.From, 10), cut(rep.Meta.To, 10), cut(rep.Meta.GeneratedAt, 10), rep.Meta.Records, rep.Meta.ParseErrors)

	// ---- Tier 1: overview (single row from rep.Overall) ----
	w("| Req/Fall/Trunc | 成功率 | Tokens In/CacheHit/Out | 图片/压缩 | 平均Tokens In/Out | 字节 In / Out | 平均消息数 | p50/p95 首字延迟 | p50/p95 请求耗时 | 平均吞吐 (tok/s) |\n")
	w("|---|---|---|---|---|---|---|---|---|---|\n")
	w("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n\n",
		reqFallTrunc(overall.Requests, overall.Fallbacks, overall.Truncated),
		pct(overall.OK, overall.Requests),
		tokensTriple(overall.TokensIn, overall.TokensInCached, overall.TokensOut),
		imagesCell(overall.Images, overall.ImagesCompressed),
		avgTokensInOut(overall),
		bytesInOut(overall.BytesIn, overall.BytesOut),
		avgMessages(overall),
		p50p95(overall.TTFTMSP50, overall.TTFTMSP95),
		p50p95(overall.DurMSP50, overall.DurMSP95),
		tokPerSecCell(overall))

	if line := roleStatLine(overall.RoleChars, true, false); line != "" {
		w("**请求消息字符及占比**\n\n按角色，汇总 %d 条请求，%s 字符\n\n",
			overall.MessagesKnown, fmtN(sumRoleChars(overall.RoleChars)))
		for _, part := range strings.Split(line, " · ") {
			w("- %s\n", part)
		}
		w("\n")
	}
	if line := finishLine(overall.FinishReasons, overall.Requests); line != "" {
		w("**finish_reason 数量及占比**\n\n")
		for _, part := range strings.Split(line, " · ") {
			w("- %s\n", part)
		}
		w("\n")
	}
	if overall.TokensReasoning > 0 {
		w("**thinking tokens 数量及占比**\n\n%s（占输出 %s；仅统计上游回报该字段的记录）\n\n",
			fmtN(overall.TokensReasoning), pct(int(overall.TokensReasoning), int(overall.TokensOut)))
	}

	// ---- Tier 1: per-model summary (iterates rep.ByModel directly) ----
	if len(rep.ByModel) > 0 {
		w("## 按模型\n\n")
		w("| 模型 | 协议 | Req/Fall/Trunc | 成功率 | Tokens In/CacheHit/Out | 图片/压缩 | 平均Tokens In/Out | 字节 In / Out | 平均消息数 | p50/p95 首字延迟 | p50/p95 请求耗时 | 平均吞吐 (tok/s) |\n")
		w("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, m := range rep.ByModel {
			w("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				m.Model, m.Protocol,
				reqFallTrunc(m.Requests, m.Fallbacks, m.Truncated),
				pct(m.OK, m.Requests),
				tokensTriple(m.TokensIn, m.TokensInCached, m.TokensOut),
				imagesCell(m.Images, m.ImagesCompressed),
				avgTokensInOut(m),
				bytesInOut(m.BytesIn, m.BytesOut),
				avgMessages(m),
				p50p95(m.TTFTMSP50, m.TTFTMSP95),
				p50p95(m.DurMSP50, m.DurMSP95),
				tokPerSecCell(m))
		}
		w("\n")
	}

	// ---- Tier 1: endpoint availability (rep.Endpoints is already pre-aggregated per day) ----
	if len(rep.EndpointsAll) > 0 {
		// rep.EndpointsAll is already grained "endpoint, all dates merged"
		// with its own raw dur_ms/ttft_ms values (Format 9) — no rollup
		// needed, unlike a naive merge of the per-date rep.Endpoints rows
		// (whose raw values are freed once finishEndpoint computes their
		// own per-date percentiles, leaving nothing true to re-derive from).
		w("## 端点可用度\n\n")
		w("| 端点 | 尝试 | 成功 | 可用度 | Tokens In/CacheHit/Out | 图片/压缩 | 平均Tokens In/Out | 字节 In / Out | p50/p95 首字延迟 | p50/p95 请求耗时 | 平均吞吐 (tok/s) |\n")
		w("|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, e := range rep.EndpointsAll {
			w("| %s | %d | %d | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				e.Endpoint, e.Attempts, e.OK, pct(e.OK, e.Attempts),
				tokensTriple(e.TokensIn, e.TokensInCached, e.TokensOut),
				imagesCell(e.Images, e.ImagesCompressed),
				avgTokensInOutEndpoint(e),
				bytesInOut(e.BytesIn, e.BytesOut),
				p50p95(e.TTFTMSP50, e.TTFTMSP95),
				p50p95(e.DurMSP50, e.DurMSP95),
				tokPerSecCellEndpoint(e))
		}
		w("\n")
		anyErrs := false
		for _, e := range rep.EndpointsAll {
			if len(e.ErrorClasses) > 0 {
				anyErrs = true
				break
			}
		}
		if anyErrs {
			w("## 上游错误分布\n\n")
			for _, e := range rep.EndpointsAll {
				if len(e.ErrorClasses) == 0 {
					continue
				}
				w("**%s**\n\n", e.Endpoint)
				for _, k := range sortedKeys(e.ErrorClasses) {
					w("- %s × %d\n", k, e.ErrorClasses[k])
				}
				w("\n")
			}
		}
	}

	// ---- Tier 2: daily trend (iterates rep.ByDate directly) ----
	if len(rep.ByDate) > 1 {
		w("## 按日趋势\n\n")
		w("| 日期 | Req/Fall/Trunc | 成功率 | Tokens In/CacheHit/Out | 图片/压缩 | 平均Tokens In/Out | 字节 In / Out | 平均消息数 | p50/p95 首字延迟 | p50/p95 请求耗时 | 平均吞吐 (tok/s) |\n")
		w("|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, d := range rep.ByDate {
			w("| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				d.Date,
				reqFallTrunc(d.Requests, d.Fallbacks, d.Truncated),
				pct(d.OK, d.Requests),
				tokensTriple(d.TokensIn, d.TokensInCached, d.TokensOut),
				imagesCell(d.Images, d.ImagesCompressed),
				avgTokensInOut(d),
				bytesInOut(d.BytesIn, d.BytesOut),
				avgMessages(d),
				p50p95(d.TTFTMSP50, d.TTFTMSP95),
				p50p95(d.DurMSP50, d.DurMSP95),
				tokPerSecCell(d))
		}
		w("\n")
	}

	// ---- Tier 2: hourly activity profile (rep.HoursOfDay is grained local
	// hour only, all dates merged, with its own true p50/p95 — Format 9;
	// NOT derived from rep.Hours, whose per-date raw values are already
	// freed by the time this table renders) ----
	if len(rep.HoursOfDay) > 0 {
		w("## 每小时活跃度（本地时区，跨日汇总）\n\n")
		w("| 时段 | Req/Fall/Trunc | Tokens In/CacheHit/Out | 图片/压缩 | 平均Tokens In/Out | 字节 In / Out | 平均消息数 | p50/p95 首字延迟 | p50/p95 请求耗时 | 平均吞吐 (tok/s) |\n")
		w("|---|---|---|---|---|---|---|---|---|---|\n")
		for _, h := range rep.HoursOfDay {
			if h.Requests == 0 {
				continue
			}
			w("| %02d:00 | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				h.Hour,
				reqFallTrunc(h.Requests, h.Fallbacks, h.Truncated),
				tokensTriple(h.TokensIn, h.TokensInCached, h.TokensOut),
				imagesCell(h.Images, h.ImagesCompressed),
				avgTokensInOutHour(h),
				bytesInOut(h.BytesIn, h.BytesOut),
				avgMessagesHour(h),
				p50p95(h.TTFTMSP50, h.TTFTMSP95),
				p50p95(h.DurMSP50, h.DurMSP95),
				tokPerSecStr(h.TokOutPerSec))
		}
		w("\n")
	}

	// ---- Tier 1: workload split (rep.Workloads already has true p50/p95) ----
	if len(rep.Workloads) > 0 {
		w("## 工作负载（交互工作 vs 定时机械任务）\n\n")
		w("| 类型 | 请求 | Tokens In/CacheHit/Out | 图片/压缩 | 平均Tokens In/Out | 字节 In / Out | 平均消息数 | p50/p95 首字延迟 | p50/p95 请求耗时 | 平均吞吐 (tok/s) | Tool 调用 |\n")
		w("|---|---|---|---|---|---|---|---|---|---|---|\n")
		for _, wl := range rep.Workloads {
			w("| %s | %d | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				wl.Class, wl.Requests,
				tokensTriple(wl.TokensIn, wl.TokensInCached, wl.TokensOut),
				imagesCell(wl.Images, wl.ImagesCompressed),
				avgTokensInOutWorkload(wl),
				bytesInOut(wl.BytesIn, wl.BytesOut),
				avgMessagesWorkload(wl),
				p50p95(wl.TTFTMSP50, wl.TTFTMSP95),
				p50p95(wl.DurMSP50, wl.DurMSP95),
				tokPerSecCellWorkload(wl),
				toolCallsCell(wl.ToolCalls, wl.RequestsWithToolCalls, wl.Requests))
		}
		w("\n定时任务（heartbeat/dream_diary）每次触发都重发完整 system prompt——Token In 显著偏高时,考虑拉长触发间隔或换更便宜的模型。\n\n")
	}

	// ---- Tier 1: agent sessions (collapsed for scheduled one-shot sessions) ----
	if len(rep.Sessions) > 0 {
		w("## Agent 会话\n\n")
		w("| 会话 | 标题 | Req/Fall/Trunc | 任务 | 时段 | Tokens In/CacheHit/Out | 图片/压缩 | 平均Tokens In/Out | 平均消息数 | p50/p95 首字延迟 | p50/p95 请求耗时 | 平均吞吐 (tok/s) |\n")
		w("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
		// collapsedAgg tracks a merged SessionRow plus the raw per-session
		// dur/ttft values behind it: each collapsed candidate has exactly
		// one request (that's why it collapses), so its own DurMSSum/
		// TTFTMSSum (when RequestsWithDur/TTFTKnown is 1) IS that request's
		// raw value — collecting these across the class gives a true
		// p50/p95 for the merged row, not just a total/count average.
		type collapsedAgg struct {
			row      SessionRow
			dursDur  []int64
			dursTTFT []int64
		}
		collapsed := map[string]*collapsedAgg{}
		var collapsedOrder []string
		for i := range rep.Sessions {
			s := rep.Sessions[i]
			// One-request scheduled sessions (each heartbeat/diary fire gets
			// a fresh conversation) would flood the table; fold them into
			// one row per class.
			if s.Requests == 1 && s.Class != "interactive" {
				agg := collapsed[s.Class]
				if agg == nil {
					agg = &collapsedAgg{row: SessionRow{ID: "（合并）", Class: s.Class, From: s.From}}
					collapsed[s.Class] = agg
					collapsedOrder = append(collapsedOrder, s.Class)
				}
				mergeIntoCollapsed(&agg.row, &s)
				if s.RequestsWithDur > 0 {
					agg.dursDur = append(agg.dursDur, s.DurMSSum)
				}
				if s.TTFTKnown > 0 {
					agg.dursTTFT = append(agg.dursTTFT, s.TTFTMSSum)
				}
				continue
			}
			title := s.Title
			idCell := s.ID
			if s.Link != "" {
				idCell = fmt.Sprintf("[%s](./%s)", s.ID, s.Link)
			}
			if s.ContinuedFrom != "" {
				idCell = fmt.Sprintf("%s ← %s", idCell, s.ContinuedFrom)
			}
			w("| %s | %s | %s | %d | %s–%s | %s | %s | %s | %s | %s | %s | %s |\n",
				idCell, escapeCell(truncCell(title, 40)),
				reqFallTrunc(s.Requests, s.Fallbacks, s.Truncated), s.Tasks,
				clock(s.From), clock(s.To),
				tokensTriple(s.TokensIn, s.TokensInCached, s.TokensOut),
				imagesCell(s.Images, s.ImagesCompressed),
				avgTokensInOutSession(&s),
				avgMessagesSession(&s),
				p50p95(s.TTFTMSP50, s.TTFTMSP95),
				p50p95(s.DurMSP50, s.DurMSP95),
				tokPerSecStr(s.TokOutPerSec))
		}
		for _, cls := range collapsedOrder {
			ca := collapsed[cls]
			s := ca.row
			n := s.Requests
			durP50, durP95 := percentiles(ca.dursDur)
			ttftP50, ttftP95 := percentiles(ca.dursTTFT)
			tokPerSec := "-"
			if s.DurMSSum > 0 {
				tokPerSec = tokPerSecStr(round2(float64(s.TokensOut) / (float64(s.DurMSSum) / 1000)))
			}
			w("| %s | %s | %s | %d | %s–%s | %s | %s | %s | %s | %s | %s | %s |\n",
				s.ID, escapeCell(fmt.Sprintf("%s 单发会话 ×%d", cls, n)),
				reqFallTrunc(s.Requests, s.Fallbacks, s.Truncated), s.Tasks,
				clock(s.From), clock(s.To),
				tokensTriple(s.TokensIn, s.TokensInCached, s.TokensOut),
				imagesCell(s.Images, s.ImagesCompressed),
				avgTokensInOutSession(&s),
				avgMessagesSession(&s),
				p50p95(ttftP50, ttftP95),
				p50p95(durP50, durP95),
				tokPerSec)
		}
		w("\n")
	}

	// ---- Tier 1: tool declaration vs. per-turn use (numbered list) ----
	if len(rep.Tools) > 0 {
		w("## 工具使用（按请求形态；只计每请求当轮的调用，历史重复不计）\n\n")
		for _, t := range rep.Tools {
			w("### %s · %d 请求 · 声明 %d 个（%s/请求） · 实际调用 %d 个\n\n",
				t.Shape, t.Requests, len(t.Declared), fmtBytes(t.DeclaredBytes), len(t.Calls))
			if len(t.Calls) > 0 {
				w("**调用过的工具（%d 个，按调用次数降序）：**\n\n",
					len(t.Calls))
				type kv struct {
					name string
					n    int
				}
				ranked := make([]kv, 0, len(t.Calls))
				for name, n := range t.Calls {
					ranked = append(ranked, kv{name, n})
				}
				sort.Slice(ranked, func(i, j int) bool {
					if ranked[i].n != ranked[j].n {
						return ranked[i].n > ranked[j].n
					}
					return ranked[i].name < ranked[j].name
				})
				for i, r := range ranked {
					w("%d. %s (%d 次)\n", i+1, r.name, r.n)
				}
				w("\n")
			}
			if len(t.NeverCalled) > 0 {
				w("**声明但本窗口内从未调用（%d 个，按字母序）：**\n\n", len(t.NeverCalled))
				for i, name := range t.NeverCalled {
					w("%d. %s\n", i+1, name)
				}
				w("\n")
			}
		}
		w("统计窗口 = 本报告的输入日志范围；低频工具（如 cron 触发类）可能不在窗口内,裁剪决策建议基于 ≥1 周日志。\n\n")
	}

	return b.String()
}

// ---- helpers (shared across tables) ----

func pct(n, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
}

func fmtN(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// tokensTriple renders the 3-tuple (In / CacheHit(share%) / Out). Cache-write
// tokens are tracked in the JSON export but not shown here — always zero in
// practice for this deployment's traffic.
func tokensTriple(in, hit, out int64) string {
	if in == 0 && out == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s(%s) / %s",
		fmtN(in), fmtN(hit), pct(int(hit), int(in)), fmtN(out))
}

// bytesInOut renders bytes as a 2-tuple (In / Out).
func bytesInOut(in, out int64) string {
	if in == 0 && out == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s", fmtN(in), fmtN(out))
}

// p50p95 renders latency as p50 / p95 (or "-/-" when both are 0).
func p50p95(p50, p95 int64) string {
	if p50 == 0 && p95 == 0 {
		return "-/-"
	}
	return fmt.Sprintf("%s / %s", ms(p50), ms(p95))
}

// avgTokensInOut renders per-request average tokens In/Out (denominator is
// tokens_known — only records with extractable usage). "-" when none.
func avgTokensInOut(r Row) string {
	if r.TokensKnown == 0 {
		return "-"
	}
	n := int64(r.TokensKnown)
	return fmt.Sprintf("%s / %s", fmtN(r.TokensIn/n), fmtN(r.TokensOut/n))
}

// avgTokensInOutWorkload is the same but typed against WorkloadRow fields.
func avgTokensInOutWorkload(w WorkloadRow) string {
	if w.TokensKnown == 0 {
		return "-"
	}
	n := int64(w.TokensKnown)
	return fmt.Sprintf("%s / %s", fmtN(w.TokensIn/n), fmtN(w.TokensOut/n))
}

// avgTokensInOutSession reads from SessionRow.
func avgTokensInOutSession(s *SessionRow) string {
	if s.TokensKnown == 0 {
		return "-"
	}
	n := int64(s.TokensKnown)
	return fmt.Sprintf("%s / %s", fmtN(s.TokensIn/n), fmtN(s.TokensOut/n))
}

// avgTokensInOutEndpoint reads from EndpointRow.
func avgTokensInOutEndpoint(e EndpointRow) string {
	if e.TokensKnown == 0 {
		return "-"
	}
	n := int64(e.TokensKnown)
	return fmt.Sprintf("%s / %s", fmtN(e.TokensIn/n), fmtN(e.TokensOut/n))
}

// avgTokensInOutHour reads from HourRow.
func avgTokensInOutHour(h HourRow) string {
	if h.TokensKnown == 0 {
		return "-"
	}
	n := int64(h.TokensKnown)
	return fmt.Sprintf("%s / %s", fmtN(h.TokensIn/n), fmtN(h.TokensOut/n))
}

func avgMessages(r Row) string {
	if r.MessagesKnown == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(r.Messages)/float64(r.MessagesKnown))
}

func avgMessagesWorkload(w WorkloadRow) string {
	if w.MessagesKnown == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(w.Messages)/float64(w.MessagesKnown))
}

func avgMessagesSession(s *SessionRow) string {
	if s.MessagesKnown == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(s.Messages)/float64(s.MessagesKnown))
}

func avgMessagesHour(h HourRow) string {
	if h.MessagesKnown == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(h.Messages)/float64(h.MessagesKnown))
}

// tokPerSecCell renders the throughput for a Row (Row has TokOutPerSec).
func tokPerSecCell(r Row) string {
	return tokPerSecStr(r.TokOutPerSec)
}

func tokPerSecCellWorkload(w WorkloadRow) string {
	return tokPerSecStr(w.TokOutPerSec)
}

func tokPerSecCellEndpoint(e EndpointRow) string {
	return tokPerSecStr(e.TokOutPerSec)
}

func tokPerSecStr(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", v)
}

func ms(v int64) string {
	if v > 1000 {
		return fmt.Sprintf("%.1fs", float64(v)/1000)
	}
	return fmt.Sprintf("%dms", v)
}

// warnCount renders a should-be-zero counter: "-" when zero, flagged when not.
func warnCount(n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("⚠️ %d", n)
}

// reqFallTrunc merges request/fallback/truncated counts into one cell
// ("Req/Fall/Trunc"): fallback and truncated follow warnCount's "flag only
// when non-zero" philosophy so a healthy row reads as "12/-/-".
func reqFallTrunc(req, fallbacks, truncated int) string {
	return fmt.Sprintf("%d/%s/%s", req, warnCount(fallbacks), warnCount(truncated))
}

// imagesCell renders inline request image counts as "total/compressed";
// "-" when the row saw no images at all.
func imagesCell(total, compressed int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", total, compressed)
}

// toolCallsCell renders total tool calls plus the share of requests that
// made at least one call (not the share of all tool calls this row
// accounts for — that would read ~100% whenever only one row has any).
func toolCallsCell(calls, requestsWithCalls, requests int) string {
	return fmt.Sprintf("%d (%s)", calls, pct(requestsWithCalls, requests))
}

// finishLine renders a finish_reason distribution with absolute + percentage.
// Percentages are computed over all records (not just those with a finish
// marker), so the reader can tell at a glance what share of traffic each
// reason accounts for.
func finishLine(m map[string]int, totalReq int) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		label := k
		switch k {
		case "":
			label = "（无）"
		case "length", "max_tokens":
			label = "⚠️ " + k
		}
		parts = append(parts, fmt.Sprintf("%s×%d(%s)", label, m[k], pct(m[k], totalReq)))
	}
	return strings.Join(parts, " · ")
}

// sumRoleChars totals a role→count map (for the overview header).
func sumRoleChars(m map[string]int64) int64 {
	var s int64
	for _, v := range m {
		s += v
	}
	return s
}

// mergeIntoCollapsed accumulates a single-request scheduled session into
// the per-class aggregate row. It can't just do field sums — the "never
// called"-style counters don't apply to collapsed rows, so we leave them
// at zero rather than producing misleading totals.
func mergeIntoCollapsed(agg, s *SessionRow) {
	agg.Requests += s.Requests
	agg.Tasks += s.Tasks
	agg.TokensIn += s.TokensIn
	agg.TokensInCached += s.TokensInCached
	agg.TokensInCacheWrite += s.TokensInCacheWrite
	agg.TokensOut += s.TokensOut
	agg.BytesIn += s.BytesIn
	agg.BytesOut += s.BytesOut
	agg.DurMSSum += s.DurMSSum
	if s.DurMSMax > agg.DurMSMax {
		agg.DurMSMax = s.DurMSMax
	}
	agg.Attempts += s.Attempts
	agg.Fallbacks += s.Fallbacks
	agg.OK += s.OK
	agg.Errors += s.Errors
	agg.Canceled += s.Canceled
	agg.Truncated += s.Truncated
	agg.Images += s.Images
	agg.ImagesCompressed += s.ImagesCompressed
	agg.TTFTMSSum += s.TTFTMSSum
	agg.TTFTKnown += s.TTFTKnown
	agg.Messages += s.Messages
	agg.MessagesKnown += s.MessagesKnown
	agg.TokensKnown += s.TokensKnown
	agg.To = s.To
	agg.Title = fmt.Sprintf("%s ×N 单发会话", s.Class)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// clock trims an RFC3339 timestamp to "MM-DD HH:MM" for compact table cells.
func clock(s string) string {
	if len(s) >= 16 {
		return strings.Replace(s[5:16], "T", " ", 1)
	}
	return s
}
