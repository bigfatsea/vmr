// Ver 2026-07-25, by Sonnet 5

// Markdown rendering of Report2, organized around nine numbered sections
// (§0-§8: summary, token economics, cost estimate, reliability, latency,
// workload, sessions, efficiency/waste, request-index pointer — the §2 cost
// section is new since the original "eight operator questions" design in
// REPORT_REDESIGN_V2.zh.md §5, added when per-endpoint/per-client cost
// estimates got their own section instead of a subsection of §1). Every
// table stays ≤7 columns; percentiles carry their basis n with a
// ⚠️low-n flag when n<20; ratio metrics whose denominator falls below 90% of
// total requests get a footnote; hourly series render as mermaid
// xychart-beta charts.

package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Markdown renders the full vmr-report.md document.
func Markdown(rep *Report2) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	o := rep.Overall

	// ---- header ----
	w("# VMR 用量报告\n\n")
	w("数据源: %s · format %d · %d 条记录（%d 坏行）· %s – %s\n",
		strings.Join(rep.Meta.Inputs, ", "), rep.Meta.Format, rep.Meta.Records, rep.Meta.ParseErrors,
		cut(rep.Meta.From, 19), cut(rep.Meta.To, 19))
	w("详单见 [vmr-requests.md](./vmr-requests.md) · 同名 .json")
	if len(rep.ByClient) > 0 {
		for _, c := range rep.ByClient {
			w(" · [-%s](./vmr-requests-%s.md)", c.ClientKey, sanitize(c.ClientKey))
		}
	}
	w("\n\n")

	renderSummary(w, rep, o)
	renderCostTokens(w, rep, o)
	renderCostEstimate(w, rep)
	renderReliability(w, rep, o)
	renderLatency(w, rep, o)
	renderWorkload(w, rep, o)
	renderSessions(w, rep)
	renderEfficiency(w, rep, o)
	renderRequestIndexLink(w, rep)
	renderAppendix(w, rep)
	return b.String()
}

// ---- §0 摘要 ----
func renderSummary(w func(string, ...any), rep *Report2, o Row) {
	w("## §0 摘要\n\n")
	w("| 请求 | 成功率 | 计费输入(fresh)⭐ | 缓存效率⭐ | p95 耗时 |\n|---|---|---|---|---|\n")
	p95n := o.RequestsWithDur
	w("| %d（fallback %d / trunc %d） | %s | %s | %s | %s |\n\n",
		o.Requests, o.Fallbacks, o.Truncated,
		pctStr2(o.OK, o.Requests),
		fmtTokens(o.TokensInFresh),
		cacheEffCell(o.CacheEfficiency, o.TokensKnown, o.Requests),
		durCell(o.DurMSP95, p95n))
	w("**亮点 (auto):**\n")
	for _, h := range highlights(rep) {
		w("- %s\n", h)
	}
	w("\n")
}

// highlights generates ≤3 auto highlights from the finished buckets.
func highlights(rep *Report2) []string {
	var out []string
	// 1. workload with low cache-eff
	for _, wl := range rep.Workloads {
		if wl.TokensKnown > 0 && wl.CacheEfficiency < 0.30 {
			out = append(out, fmt.Sprintf("⚠️ **%s 工作负载缓存效率 %s** - %s fresh tokens（占该负载输入大头）",
				wl.Class, pctStr(wl.CacheEfficiency), fmtTokens(wl.TokensInFresh)))
			break
		}
	}
	// 2. tool shape with low utilization
	for _, t := range rep.Tools {
		if t.Requests > 0 && t.DeclareUtilization < 0.20 && t.SchemaBytesShipped > 0 {
			out = append(out, fmt.Sprintf("⚠️ **工具声明 %s** - 跨 %d 请求发送 %s schema，利用率 %s（%d 个从未调用）",
				t.Shape, t.Requests, fmtBytesGB(t.SchemaBytesShipped),
				pctStr(t.DeclareUtilization), len(t.NeverCalled)))
			break
		}
	}
	// 3. worst endpoint error rate
	var worst *EndpointRow
	for i := range rep.EndpointsAll {
		e := &rep.EndpointsAll[i]
		if worst == nil || e.ErrorRate > worst.ErrorRate {
			worst = e
		}
	}
	if worst != nil && worst.Attempts >= 4 && worst.ErrorRate > 5 {
		top := topErrorClass(worst)
		out = append(out, fmt.Sprintf("⚠️ **端点 %s 错误率 %s/100**（最差）%s",
			worst.Endpoint, strconv.FormatFloat(float64(worst.ErrorRate), 'f', 1, 64), top))
	}
	if len(out) == 0 {
		out = append(out, "（无明显异常：缓存效率、工具利用率、端点错误率均在正常区间）")
	}
	return out
}

func topErrorClass(e *EndpointRow) string {
	if len(e.ErrorClasses) == 0 {
		return ""
	}
	var cls string
	var n int
	for c, m := range e.ErrorClasses {
		if m > n {
			cls, n = c, m
		}
	}
	return "，主因 " + cls + " ×" + strconv.Itoa(n)
}

// ---- §1 成本与 Token 经济 ----
func renderCostTokens(w func(string, ...any), rep *Report2, o Row) {
	w("## §1 成本与 Token 经济\n\n")
	// token class breakdown
	w("**Token 类别分解**（basis: %d 条带 usage 的记录）\n\n", o.TokensKnown)
	w("| 类别 | 数量 | 占比 |\n|---|---|---|\n")
	w("| 输入-缓存命中 | %s | %s of in |\n", fmtTokens(o.TokensInCached), pctStr(o.CacheHitRate))
	freshShare := 0.0
	if o.TokensInCached+o.TokensInFresh > 0 {
		freshShare = float64(o.TokensInFresh) / float64(o.TokensInCached+o.TokensInFresh)
	}
	w("| 输入-fresh ⭐ | %s | %s of (fresh+cached) |\n", fmtTokens(o.TokensInFresh), pctFloat(freshShare))
	cw := ""
	if o.TokensInCacheWrite > 0 {
		cw = "（Anthropic 缓存创建，溢价计费）"
	}
	w("| 输入-cache_write | %s | %s |\n", fmtTokens(o.TokensInCacheWrite), orDash(cw))
	w("| 输出 | %s | - |\n", fmtTokens(o.TokensOut))
	if o.TokensReasoning > 0 {
		w("| └ 其中 reasoning | %s | %s of out |\n", fmtTokens(o.TokensReasoning), pctStr(o.ReasoningShare))
	}
	w("\n> 计费口径：fresh + cache_write(×溢价) + out。缓存命中按各厂免费/极低价计。\n")
	if rep.Pricing == nil {
		w("> 未配置定价 -> 不显示 $ 估算；配置后见 §2 成本估算。\n")
	} else {
		w("> %s 详见 §2 成本估算。\n", rep.Pricing.Disclaimer())
	}
	w("\n")

	// by-model cache efficiency (7 cols)
	w("**按模型缓存效率** ⭐\n\n")
	hdr := "| 模型 | 协议 | 请求 | 缓存效率⭐ | fresh | cached | out |\n|---|---|---|---|---|---|---|\n"
	w(hdr)
	for _, m := range rep.ByModel {
		w("| %s | %s | %d | %s | %s | %s | %s |\n",
			m.Model, m.Protocol, m.Requests,
			cacheEffCell(m.CacheEfficiency, m.TokensKnown, m.Requests),
			fmtTokens(m.TokensInFresh), fmtTokens(m.TokensInCached), fmtTokens(m.TokensOut))
	}
	w("\n")

	// role chars (D-family)
	if len(o.RoleChars) > 0 {
		w("**请求消息字符及占比**\n\n| 角色 | 字符 | 占比 |\n|---|---|---|\n")
		total := sumRoleChars(o.RoleChars)
		for _, role := range sortedRoles(o.RoleChars) {
			c := o.RoleChars[role]
			share := 0.0
			if total > 0 {
				share = float64(c) / float64(total)
			}
			w("| %s | %s | %s |\n", role, fmtTokens(c), pctStr(share))
		}
		w("\n> takeaway: tool 结果占比最大时，上下文优化的首要杠杆是压缩 tool 返回，而非 system prompt。\n\n")
	}
}

// ---- §2 成本估算 ----
func renderCostEstimate(w func(string, ...any), rep *Report2) {
	w("## §2 成本估算\n\n")
	if rep.Pricing == nil {
		w("未配置定价（`-pricing pricing.yaml`），本章节不显示 $ 估算。\n\n")
		return
	}
	w("> %s\n\n", rep.Pricing.Disclaimer())
	cur := rep.Pricing.Currency

	hasModel := false
	for _, m := range rep.ByModel {
		if m.CostEstimate != nil {
			hasModel = true
			break
		}
	}
	if hasModel {
		w("**按模型估算成本**（%s）\n\n| 模型 | 协议 | fresh | out | 估算成本 |\n|---|---|---|---|---|\n", cur)
		for _, m := range rep.ByModel {
			if m.CostEstimate != nil {
				w("| %s | %s | %s | %s | %.4f %s |\n",
					m.Model, m.Protocol, fmtTokens(m.TokensInFresh), fmtTokens(m.TokensOut), *m.CostEstimate, cur)
			}
		}
		w("\n")
	}

	hasEndpoint := false
	for _, e := range rep.EndpointsAll {
		if e.CostEstimate != nil {
			hasEndpoint = true
			break
		}
	}
	if hasEndpoint {
		w("**按端点估算成本**（%s，跨日合并）\n\n| 端点 | fresh | out | 估算成本 |\n|---|---|---|---|\n", cur)
		for _, e := range rep.EndpointsAll {
			if e.CostEstimate != nil {
				w("| %s | %s | %s | %.4f %s |\n",
					e.Endpoint, fmtTokens(e.TokensInFresh), fmtTokens(e.TokensOut), *e.CostEstimate, cur)
			}
		}
		w("\n")
	}

	hasClient := false
	for _, c := range rep.ByClient {
		if c.CostEstimate != nil {
			hasClient = true
			break
		}
	}
	if hasClient {
		w("**按客户端估算成本**（%s）\n\n| client_key | fresh | out | 估算成本 |\n|---|---|---|---|\n", cur)
		for _, c := range rep.ByClient {
			if c.CostEstimate != nil {
				w("| %s | %s | %s | %.4f %s |\n",
					c.ClientKey, fmtTokens(c.TokensInFresh), fmtTokens(c.TokensOut), *c.CostEstimate, cur)
			}
		}
		w("\n")
	}

	if !hasModel && !hasEndpoint && !hasClient {
		w("配置了定价，但没有请求命中已配置的端点，暂无成本数据。\n\n")
	}
}

// ---- §3 可靠性 ----
func renderReliability(w func(string, ...any), rep *Report2, o Row) {
	w("## §3 可靠性\n\n")
	w("**结果分布**\n\n| ok | error | canceled | truncated | fallback(恢复/失败)⭐ |\n|---|---|---|---|---|\n")
	w("| %d | %d | %d | %d | %d (%d/%d) |\n\n", o.OK, o.Errors, o.Canceled, o.Truncated, o.Fallbacks, o.FallbackRecovered, o.FallbackFailed)

	// endpoint health (6 cols) - use EndpointsAll for cross-date view
	if len(rep.EndpointsAll) > 0 {
		w("**端点健康**（跨日合并）\n\n| 端点 | 尝试 | 成功 | 可用度 | 错误率/100⭐ | 首要错误 |\n|---|---|---|---|---|---|\n")
		for _, e := range rep.EndpointsAll {
			marker := ""
			if e.ErrorRate > 10 {
				marker = " ⚠️"
			}
			w("| %s | %d | %d | %s | %s%s | %s |\n", e.Endpoint, e.Attempts, e.OK,
				pctStr(e.Availability),
				strconv.FormatFloat(float64(e.ErrorRate), 'f', 1, 64), marker,
				topErrorClassShort(e))
		}
		w("\n")
	}

	// error class × endpoint (only non-zero), top entries
	nonzero := false
	for _, e := range rep.EndpointsAll {
		if len(e.ErrorClasses) > 0 {
			nonzero = true
			break
		}
	}
	if nonzero {
		w("**错误类别 × 端点**（仅非零）\n\n| 端点 | 类别 | 计数 | 速率/100 |\n|---|---|---|---|\n")
		for _, e := range rep.EndpointsAll {
			for _, cls := range sortedKeysInt(e.ErrorClasses) {
				n := e.ErrorClasses[cls]
				rate := 0.0
				if e.Attempts > 0 {
					rate = float64(n) / float64(e.Attempts) * 100
				}
				w("| %s | %s | %d | %s |\n", e.Endpoint, cls, n, strconv.FormatFloat(rate, 'f', 1, 64))
			}
		}
		w("\n")
	}

	// error timeline sparkline (per hour error counts from HoursOfDay)
	if len(rep.HoursOfDay) > 0 {
		errs := make([]int64, 24)
		for _, h := range rep.HoursOfDay {
			if h.Hour >= 0 && h.Hour < 24 {
				errs[h.Hour] += int64(h.Errors)
			}
		}
		w("**错误时间线**（错误数 / 小时）\n\n%s", mermaidHourBar("错误数 / 小时", "错误数", errs))
		// callout the peak hour
		peakH, peakN := 0, int64(0)
		for i, n := range errs {
			if n > peakN {
				peakH, peakN = i, n
			}
		}
		if peakN > 0 {
			w("> 错误集中在 %02d:00（共 %d 条）。\n\n", peakH, peakN)
		}
	}
}

// ---- §4 延迟与吞吐 ----
func renderLatency(w func(string, ...any), rep *Report2, o Row) {
	w("## §4 延迟与吞吐\n\n")
	w("| 模型 | 协议 | ttft p50/p95 (n) | dur p50/p95/max (n) | stream p95 (n)⭐ | slow>%ds⭐ | tok/s |\n|---|---|---|---|---|---|---|\n",
		SlowThresholdMS/1000)
	for _, m := range rep.ByModel {
		w("| %s | %s | %s | %s | %s | %d | %s |\n",
			m.Model, m.Protocol,
			ppCell(m.TTFTMSP50, m.TTFTMSP95, 0, m.TTFTKnown),
			ppCell(m.DurMSP50, m.DurMSP95, m.DurMSMax, m.RequestsWithDur),
			streamCell(m.StreamMSP95, m.StreamKnown),
			m.SlowRequests,
			tokPerSec(m.TokOutPerSec))
	}
	w("\n> 全局 p95 dur %s，max %s。stream_ms = dur − ttft（每请求独立切片算真百分位，非两百分位相减）。\n",
		fmtDurMS(o.DurMSP95), fmtDurMS(o.DurMSMax))
	w("> 若 coding 的慢主要来自长流式输出（stream p95 大），而非首字延迟，即为流式归因。\n\n")
}

// ---- §5 负载分布 ----
func renderWorkload(w func(string, ...any), rep *Report2, o Row) {
	w("## §5 负载分布\n\n")
	// by virtual model (6)
	w("**按虚拟模型**\n\n| 模型 | 协议 | 请求 | 成功率 | fresh/cached/out | p95 dur |\n|---|---|---|---|---|---|\n")
	for _, m := range rep.ByModel {
		w("| %s | %s | %d | %s | %s / %s / %s | %s |\n",
			m.Model, m.Protocol, m.Requests, pctStr2(m.OK, m.Requests),
			fmtTokens(m.TokensInFresh), fmtTokens(m.TokensInCached), fmtTokens(m.TokensOut),
			fmtDurMS(m.DurMSP95))
	}
	w("\n")
	// by workload class (6)
	w("**按工作负载类**\n\n| 类 | 请求 | fresh | 缓存效率⭐ | tool_call_rate | p95 dur |\n|---|---|---|---|---|---|\n")
	for _, wl := range rep.Workloads {
		flag := ""
		if wl.TokensKnown > 0 && wl.CacheEfficiency < 0.30 {
			flag = " ⚠️"
		}
		w("| %s | %d | %s | %s%s | %s | %s |\n",
			wl.Class, wl.Requests, fmtTokens(wl.TokensInFresh),
			cacheEffCell(wl.CacheEfficiency, wl.TokensKnown, wl.Requests), flag,
			pctStr(wl.ToolCallRate), fmtDurMS(wl.DurMSP95))
	}
	w("\n")
	// by hour: dual sparkline + narrow table
	if len(rep.HoursOfDay) > 0 {
		vol := make([]int64, 24)
		p95 := make([]int64, 24)
		for _, h := range rep.HoursOfDay {
			if h.Hour >= 0 && h.Hour < 24 {
				vol[h.Hour] = int64(h.Requests)
				p95[h.Hour] = h.DurMSP95
			}
		}
		w("**每小时活跃度**\n\n%s%s",
			mermaidHourBar("请求量 / 小时", "请求", vol),
			mermaidHourLine("p95 耗时 / 小时（秒）", "p95 dur (s)", p95))
		// narrow table only for non-trivial hours
		w("| 小时 | 请求 | p95 dur | slow |\n|---|---|---|---|\n")
		for _, h := range rep.HoursOfDay {
			if h.Requests == 0 {
				continue
			}
			w("| %02d | %d | %s | %d |\n", h.Hour, h.Requests, fmtDurMS(h.DurMSP95), h.SlowRequests)
		}
		w("\n")
	}
	// by client (6)
	if len(rep.ByClient) > 0 {
		w("**按客户端** ⭐\n\n| client_key | 请求 | 成功率 | fresh | 缓存效率 | p95 dur |\n|---|---|---|---|---|---|\n")
		for _, c := range rep.ByClient {
			w("| %s | %d | %s | %s | %s | %s |\n",
				c.ClientKey, c.Requests, pctStr2(c.OK, c.Requests),
				fmtTokens(c.TokensInFresh),
				cacheEffCell(c.CacheEfficiency, c.TokensKnown, c.Requests),
				fmtDurMS(c.DurMSP95))
		}
		w("\n")
	}
}

// ---- §6 会话与任务 ----
func renderSessions(w func(string, ...any), rep *Report2) {
	w("## §6 会话与任务\n\n")
	if len(rep.Sessions) == 0 {
		w("（无会话）\n\n")
		return
	}
	w("| 会话 | 标题 | 类 | 轮 | 任务 | fresh/cached/out | 结果 |\n|---|---|---|---|---|---|---|\n")
	// Collapse single-turn scheduled sessions (heartbeat/dream_diary/compaction
	// one-shots) into one row per class, like the legacy report - keeps the
	// table readable. Multi-turn scheduled sessions (cron jobs with several
	// turns) and interactive sessions stay individual.
	type collapsed struct {
		class              string
		count, requests    int
		fresh, cached, out int64
	}
	byClass := map[string]*collapsed{}
	var order []string
	for _, s := range rep.Sessions {
		if s.Class != "interactive" && s.Requests == 1 {
			c := byClass[s.Class]
			if c == nil {
				c = &collapsed{class: s.Class}
				byClass[s.Class] = c
				order = append(order, s.Class)
			}
			c.count++
			c.requests += s.Requests
			c.fresh += s.TokensInFresh
			c.cached += s.TokensInCached
			c.out += s.TokensOut
			continue
		}
		renderSessionRow(w, s)
	}
	for _, cls := range order {
		c := byClass[cls]
		w("| （合并） | %s ×%d | %s | %d | %d | %s / %s / %s | ok |\n",
			cls, c.count, cls, c.requests, c.count,
			fmtTokens(c.fresh), fmtTokens(c.cached), fmtTokens(c.out))
	}
	w("\n")
	// compaction chains: mermaid for chains ≥3 nodes
	renderCompactionChains(w, rep)
}

func renderSessionRow(w func(string, ...any), s SessionRow) {
	outcome := "ok"
	if s.Errors > 0 {
		outcome = fmt.Sprintf("ok (%d error)", s.Errors)
	}
	if s.Fallbacks > 0 {
		outcome += fmt.Sprintf(" · %d fallback", s.Fallbacks)
	}
	title := s.Title
	if len(title) > 28 {
		title = title[:28] + "…"
	}
	w("| %s | %s | %s | %d | %d | %s / %s / %s | %s |\n",
		s.ID, title, s.Class, s.Requests, s.Tasks,
		fmtTokens(s.TokensInFresh), fmtTokens(s.TokensInCached), fmtTokens(s.TokensOut),
		outcome)
}

// renderCompactionChains builds head->current chains from SessionRow.ContinuedFrom
// and renders a mermaid flowchart for any chain with ≥3 nodes (≥2 compaction
// hops). Shorter chains are noted inline as text. (V2 A3 / M5)
func renderCompactionChains(w func(string, ...any), rep *Report2) {
	byID := map[string]*SessionRow{}
	for i := range rep.Sessions {
		byID[rep.Sessions[i].ID] = &rep.Sessions[i]
	}
	// child -> parent (ContinuedFrom). A session is a "tip" if nobody continues from it.
	pointedTo := map[string]bool{}
	for _, s := range rep.Sessions {
		if s.ContinuedFrom != "" {
			pointedTo[s.ContinuedFrom] = true
		}
	}
	seen := map[string]bool{}
	for _, s := range rep.Sessions {
		if pointedTo[s.ID] {
			continue // not a tip
		}
		// walk back to head via ContinuedFrom links (string-only, no pointer)
		chain := []string{s.ID}
		parent := s.ContinuedFrom
		for parent != "" && byID[parent] != nil && !seen[parent] {
			chain = append(chain, parent)
			seen[parent] = true
			parent = byID[parent].ContinuedFrom
		}
		// reverse: head -> tip
		for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
			chain[i], chain[j] = chain[j], chain[i]
		}
		if len(chain) >= 3 {
			w("```mermaid\nflowchart LR\n")
			for i := 0; i < len(chain)-1; i++ {
				w("    %s[\"%s\"] -->|compacted| %s[\"%s\"]\n", chain[i], chain[i], chain[i+1], chain[i+1])
			}
			w("```\n\n")
		} else if len(chain) == 2 {
			// text arrow, inline note
			w("> %s ← %s（单次 compaction）\n\n", chain[1], chain[0])
		}
	}
}

// ---- §7 效率与浪费 ----
func renderEfficiency(w func(string, ...any), rep *Report2, o Row) {
	w("## §7 效率与浪费 ⭐\n\n")
	if len(rep.Efficiency) > 0 {
		w("| 发现 | 指标 | 值 | 涉及 | 建议 |\n|---|---|---|---|---|\n")
		for _, f := range rep.Efficiency {
			w("| %s | %s | %s | %s | %s |\n", f.Finding, f.Metric, f.Value, f.Implicated, f.Action)
		}
		w("\n")
	}
	// tool waste Top-5: compact table + per-shape used/never-called detail
	if len(rep.Tools) > 0 {
		w("**工具形态浪费 Top-5**（按浪费字节降序；完整明细见 vmr-report.json -> tools[]）\n\n")
		w("| 形态 | 请求 | 声明 | 已用 | 利用率 | 浪费字节 |\n|---|---|---|---|---|---|\n")
		top := rep.Tools
		if len(top) > 5 {
			top = top[:5]
		}
		for _, t := range top {
			w("| %s | %d | %d | %d | %s | %s |\n",
				t.Shape, t.Requests, len(t.Declared), t.DistinctCalled,
				pctStr(t.DeclareUtilization), fmtBytesGB(t.SchemaWasteBytes))
		}
		w("\n")
		for _, t := range top {
			renderToolShapeDetail(w, t)
		}
		w("> 统计窗口 = 本报告的输入日志范围；低频工具（如 cron 触发类）可能不在窗口内，裁剪决策建议基于 ≥1 周日志。\n\n")
	}
}

// renderToolShapeDetail lists, for one declared-tool-set shape, which tools
// were actually called (call count, descending) and which were declared but
// never invoked (alphabetical) — the data behind the summary table's
// "利用率" number, collapsed into <details> so a 60+-tool schema doesn't
// blow out the document while still keeping full detail one click away.
func renderToolShapeDetail(w func(string, ...any), t ToolShapeRow) {
	w("<details><summary>%s · %d 请求 · 声明 %d 个 · 实际调用 %d 个</summary>\n\n",
		t.Shape, t.Requests, len(t.Declared), t.DistinctCalled)
	if len(t.Calls) > 0 {
		type callCount struct {
			name string
			n    int
		}
		calls := make([]callCount, 0, len(t.Calls))
		for name, n := range t.Calls {
			calls = append(calls, callCount{name, n})
		}
		sort.Slice(calls, func(i, j int) bool {
			if calls[i].n != calls[j].n {
				return calls[i].n > calls[j].n
			}
			return calls[i].name < calls[j].name
		})
		w("**调用过的工具（%d 个，按调用次数降序）：**\n\n", len(calls))
		for i, c := range calls {
			w("%d. %s (%d 次)\n", i+1, c.name, c.n)
		}
		w("\n")
	}
	if len(t.NeverCalled) > 0 {
		names := append([]string(nil), t.NeverCalled...)
		sort.Strings(names)
		w("**声明但从未调用（%d 个，按字母序）：**\n\n", len(names))
		for i, n := range names {
			w("%d. %s\n", i+1, n)
		}
		w("\n")
	}
	w("</details>\n\n")
}

// ---- §8 请求详单 ----
func renderRequestIndexLink(w func(string, ...any), rep *Report2) {
	w("## §8 请求详单\n\n")
	w("每条记录（Chat User -> Session -> Task -> Turn）见 [vmr-requests.md](./vmr-requests.md)。\n")
	if len(rep.ByClient) > 0 {
		w("per-client: ")
		for i, c := range rep.ByClient {
			if i > 0 {
				w(" · ")
			}
			w("[-%s](./vmr-requests-%s.md)", c.ClientKey, sanitize(c.ClientKey))
		}
		w("\n")
	}
	w("单请求全量捕获（req/resp/SSE）见 `details/*.md`。\n\n")
}

// ---- 附录 ----
func renderAppendix(w func(string, ...any), rep *Report2) {
	w("## 附录 数据源与方法论\n\n")
	w("- 输入: %s · format %d · %d 记录 / %d 坏行\n", strings.Join(rep.Meta.Inputs, ", "), rep.Meta.Format, rep.Meta.Records, rep.Meta.ParseErrors)
	w("- 时段: %s – %s (本地时区)\n", cut(rep.Meta.From, 19), cut(rep.Meta.To, 19))
	w("- 百分位: %s\n", rep.Meta.PercentileMethod)
	w("- n 基准: 每个百分位标注 n（= ttft_known / requests_with_dur / stream_known）；n<20 标 ⚠️low-n。\n")
	w("- 比值低置信度: cache_efficiency 等比值指标的分母 / 总请求数 < 90%% 时标注脚注 ¹。\n")
	w("- ⭐ 标记: 该列为衍生/预估指标（非上游直接返回值），解读时请结合样本量与口径说明。\n")
	w("- 计费口径: fresh + cache_write(溢价) + out；缓存命中按各厂免费/极低价。%s\n", orDash2(rep.Pricing == nil, "未配置定价时不显示 $。", ""))
	w("- 慢请求阈值: %ds\n", rep.Meta.SlowThreshold/1000)
}

// ---- cell/format helpers ----

func cut(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func fmtDurMS(v int64) string {
	if v <= 0 {
		return "-"
	}
	if v < 1000 {
		return strconv.FormatInt(v, 10) + "ms"
	}
	return strconv.FormatFloat(float64(v)/1000, 'f', 1, 64) + "s"
}

func pctFloat(f float64) string {
	return strconv.FormatFloat(f*100, 'f', 1, 64) + "%"
}

func pctStr2(num, den int) string {
	if den <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(num)/float64(den)*100, 'f', 1, 64) + "%"
}

// cacheEffCell renders a cache_efficiency value with a low-confidence footnote
// marker when its basis (TokensKnown) is < 90% of total requests.
func cacheEffCell(eff float64, basis, total int) string {
	s := pctStr(eff)
	if total > 0 && basis > 0 && float64(basis)/float64(total) < 0.90 {
		s += "¹"
	}
	return s
}

// ppCell renders "p50 / p95" or "p50 / p95 / max" with (n=…) and ⚠️low-n.
func ppCell(p50, p95, max int64, n int) string {
	if n == 0 {
		return "- (n=0)"
	}
	flag := ""
	if n < 20 {
		flag = " ⚠️low-n"
	}
	if max > 0 {
		return fmt.Sprintf("%s / %s / %s (n=%d%s)", fmtDurMS(p50), fmtDurMS(p95), fmtDurMS(max), n, flag)
	}
	return fmt.Sprintf("%s / %s (n=%d%s)", fmtDurMS(p50), fmtDurMS(p95), n, flag)
}

func streamCell(p95 int64, n int) string {
	if n == 0 {
		return "- (n=0)"
	}
	flag := ""
	if n < 20 {
		flag = " ⚠️low-n"
	}
	return fmt.Sprintf("%s (n=%d%s)", fmtDurMS(p95), n, flag)
}

func durCell(p95 int64, n int) string {
	if n == 0 {
		return "-"
	}
	flag := ""
	if n < 20 {
		flag = " ⚠️low-n"
	}
	return fmt.Sprintf("%s (n=%d%s)", fmtDurMS(p95), n, flag)
}

func tokPerSec(f float64) string {
	if f <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(f), 'f', 1, 64)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orDash2(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func sumRoleChars(m map[string]int64) int64 {
	var t int64
	for _, v := range m {
		t += v
	}
	return t
}

func sortedRoles(m map[string]int64) []string {
	// order: tool, assistant, developer, system, user, then others
	order := []string{"tool", "assistant", "developer", "system", "user"}
	out := []string{}
	seen := map[string]bool{}
	for _, r := range order {
		if _, ok := m[r]; ok {
			out = append(out, r)
			seen[r] = true
		}
	}
	for r := range m {
		if !seen[r] {
			out = append(out, r)
		}
	}
	return out
}

func sortedKeysInt(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func topErrorClassShort(e EndpointRow) string {
	if len(e.ErrorClasses) == 0 {
		return "-"
	}
	var cls string
	var n int
	for c, m := range e.ErrorClasses {
		if m > n {
			cls, n = c, m
		}
	}
	return cls + " ×" + strconv.Itoa(n)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---- hourly mermaid charts ----

// hourAxisLabels renders the fixed 24-hour x-axis category list ("00".."23"),
// shared by both hourly chart shapes below.
func hourAxisLabels() string {
	labels := make([]string, 24)
	for i := 0; i < 24; i++ {
		labels[i] = fmt.Sprintf("%q", fmt.Sprintf("%02d", i))
	}
	return strings.Join(labels, ", ")
}

// mermaidHourBar renders 24 hourly integer buckets (requests, error counts)
// as a mermaid xychart-beta bar chart.
func mermaidHourBar(title, yLabel string, vals []int64) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.FormatInt(v, 10)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "```mermaid\nxychart-beta\n    title %q\n    x-axis [%s]\n    y-axis %q\n    bar [%s]\n```\n\n",
		title, hourAxisLabels(), yLabel, strings.Join(parts, ", "))
	return b.String()
}

// mermaidHourLine renders 24 hourly ms-duration buckets (e.g. p95 dur) as a
// mermaid xychart-beta line chart, scaled to seconds with one decimal.
func mermaidHourLine(title, yLabel string, valsMS []int64) string {
	parts := make([]string, len(valsMS))
	for i, v := range valsMS {
		parts[i] = strconv.FormatFloat(float64(v)/1000, 'f', 1, 64)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "```mermaid\nxychart-beta\n    title %q\n    x-axis [%s]\n    y-axis %q\n    line [%s]\n```\n\n",
		title, hourAxisLabels(), yLabel, strings.Join(parts, ", "))
	return b.String()
}
