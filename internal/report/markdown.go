// Ver 2026-07-12 03:10, by Fable 5
package report

import (
	"fmt"
	"sort"
	"strings"
)

// Markdown renders the report's most-consulted views. Tier 1 (overview,
// per-model summary, endpoint availability) and tier 2 (daily trend, error
// classes, fallback detail) are included; finer cuts live in the JSON.
func Markdown(rep *Report) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	// ---- Tier 1: overview ----
	var t total
	for _, r := range rep.Rows {
		t.add(r)
	}
	w("# VMR 用量报告\n\n")
	w("时段 %s — %s · 生成于 %s · 解析 %d 条记录（%d 条坏行跳过）\n\n",
		cut(rep.Meta.From, 10), cut(rep.Meta.To, 10), cut(rep.Meta.GeneratedAt, 10), rep.Meta.Records, rep.Meta.ParseErrors)
	w("| 请求 | 成功率 | Fallback | 截断 | Tokens In/CacheHit/Out | 请求均值 In/CacheHit/Out | 缓存写入 | 平均消息数 | 平均首字延迟 | 字节 in/out | 平均吞吐 |\n|---|---|---|---|---|---|---|---|---|---|---|\n")
	w("| %d | %s | %d 次 | %s | %s | %s | %s | %s | %s | %s / %s | %.1f tok/s |\n\n",
		t.requests, pct(t.ok, t.requests), t.fallbacks, warnCount(t.truncated),
		tokTriple(t.tokensIn, t.tokensInCached, t.tokensOut), t.avgTriple(), cacheAbs(t.tokensInCacheWrite),
		t.avgMessages(), t.avgTTFT(),
		fmtN(t.bytesIn), fmtN(t.bytesOut), t.tokPerSec())
	if line := roleStatLine(t.roleChars, false); line != "" {
		w("请求消息字符占比（按角色，汇总 %d 条请求）：%s\n\n", t.messagesKnown, line)
	}
	if line := finishLine(t.finishReasons); line != "" {
		w("finish_reason 分布：%s\n\n", line)
	}
	if t.tokensReasoning > 0 {
		w("输出中的推理（thinking）tokens：%s（占输出 %s；仅统计上游回报该字段的记录）\n\n",
			fmtN(t.tokensReasoning), pct(int(t.tokensReasoning), int(t.tokensOut)))
	}

	// ---- Tier 1: per-model summary (rows rolled up over dates) ----
	w("## 按模型\n\n")
	w("| 模型 | 协议 | 请求 | 成功率 | Fallback | 截断 | Tokens In/CacheHit/Out | 缓存写入 | 字节 out | p50/p95 延迟 | TTFT p50 | tok/s |\n")
	w("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, m := range rollupModels(rep.Rows) {
		w("| %s | %s | %d | %s | %d | %s | %s | %s | %s | %s / %s | %s | %.1f |\n",
			m.Model, m.Protocol, m.requests, pct(m.ok, m.requests), m.fallbacks, warnCount(m.truncated),
			tokTriple(m.tokensIn, m.tokensInCached, m.tokensOut), cacheAbs(m.tokensInCacheWrite), fmtN(m.bytesOut),
			ms(m.p50), ms(m.p95), m.ttftP50Cell(), m.tokPerSec())
	}
	w("\n")

	// ---- Tier 1: endpoint availability ----
	w("## 端点可用度\n\n| 端点 | 尝试 | 成功 | 可用度 | 错误分布 | p50 延迟 |\n|---|---|---|---|---|---|\n")
	for _, e := range rollupEndpoints(rep.Endpoints) {
		w("| %s | %d | %d | %s | %s | %s |\n",
			e.Endpoint, e.attempts, e.ok, pct(e.ok, e.attempts), errStr(e.errors), ms(e.p50))
	}
	w("\n")

	// ---- Tier 2: daily trend ----
	if days := rollupDates(rep.Rows); len(days) > 1 {
		w("## 按日趋势\n\n| 日期 | 请求 | 成功率 | Tokens In/CacheHit/Out | 字节 out | p95 延迟 |\n|---|---|---|---|---|---|\n")
		for _, d := range days {
			w("| %s | %d | %s | %s | %s | %s |\n",
				d.Date, d.requests, pct(d.ok, d.requests), tokTriple(d.tokensIn, d.tokensInCached, d.tokensOut), fmtN(d.bytesOut), ms(d.p95))
		}
		w("\n")
	}

	// ---- Tier 2: hourly activity profile (hour of day, all dates merged) ----
	if len(rep.Hours) > 0 {
		type hourAgg struct {
			requests, errors    int
			tokensIn, tokensOut int64
		}
		var byHour [24]hourAgg
		maxReq := 0
		for _, h := range rep.Hours {
			a := &byHour[h.Hour]
			a.requests += h.Requests
			a.errors += h.Errors
			a.tokensIn += h.TokensIn
			a.tokensOut += h.TokensOut
			if a.requests > maxReq {
				maxReq = a.requests
			}
		}
		w("## 每小时活跃度（本地时区，跨日汇总）\n\n| 时段 | 请求 | | 错误 | Tokens In/Out |\n|---|---|---|---|---|\n")
		for hh, a := range byHour {
			if a.requests == 0 {
				continue
			}
			barLen := a.requests * 20 / maxReq
			if barLen == 0 {
				barLen = 1
			}
			w("| %02d:00 | %d | %s | %s | %s / %s |\n",
				hh, a.requests, strings.Repeat("█", barLen), warnCount(a.errors),
				fmtN(a.tokensIn), fmtN(a.tokensOut))
		}
		w("\n")
	}

	// ---- Tier 1: workload split (real work vs. scheduled scaffolding) ----
	if len(rep.Workloads) > 0 {
		w("## 工作负载（交互工作 vs 定时机械任务）\n\n")
		w("| 类型 | 请求 | Tokens In/CacheHit/Out | In 占比 | 累计耗时 | Tool 调用 |\n|---|---|---|---|---|---|\n")
		var totalIn int64
		for _, wl := range rep.Workloads {
			totalIn += wl.TokensIn
		}
		for _, wl := range rep.Workloads {
			w("| %s | %d | %s | %s | %s | %d |\n",
				wl.Class, wl.Requests, tokTriple(wl.TokensIn, wl.TokensInCached, wl.TokensOut),
				pct(int(wl.TokensIn), int(totalIn)), ms(wl.DurMSSum), wl.ToolCalls)
		}
		w("\n定时任务（heartbeat/dream_diary）每次触发都重发完整 system prompt——若其 In 占比过高,考虑拉长触发间隔或换更便宜的模型。\n\n")
	}

	// ---- Tier 1: agent sessions (scheduled one-shot sessions collapsed) ----
	if len(rep.Sessions) > 0 {
		w("## Agent 会话\n\n| 会话 | 标题 | 请求 | 任务 | 时段 | Tokens In/Out | 累计耗时 | 截断 | 续接自 |\n|---|---|---|---|---|---|---|---|---|\n")
		collapsed := map[string]*SessionRow{}
		var collapsedOrder []string
		for i := range rep.Sessions {
			s := rep.Sessions[i]
			// One-request scheduled sessions (each heartbeat/diary fire gets
			// a fresh conversation) would flood the table; fold them into
			// one row per class.
			if s.Requests == 1 && s.Class != "interactive" {
				agg := collapsed[s.Class]
				if agg == nil {
					agg = &SessionRow{ID: "（合并）", Class: s.Class, From: s.From}
					collapsed[s.Class] = agg
					collapsedOrder = append(collapsedOrder, s.Class)
				}
				agg.Requests += s.Requests
				agg.Tasks += s.Tasks
				agg.TokensIn += s.TokensIn
				agg.TokensOut += s.TokensOut
				agg.DurMSSum += s.DurMSSum
				agg.Truncated += s.Truncated
				agg.To = s.To
				agg.Title = fmt.Sprintf("%s ×N 单发会话", s.Class)
				continue
			}
			trunc := "-"
			if s.Truncated > 0 {
				trunc = fmt.Sprintf("⚠️ %d", s.Truncated)
			}
			cont := "-"
			if s.ContinuedFrom != "" {
				cont = s.ContinuedFrom
			}
			w("| %s | %s | %d | %d | %s–%s | %s / %s | %s | %s | %s |\n",
				s.ID, escapeCell(truncCell(s.Title, 40)), s.Requests, s.Tasks,
				clock(s.From), clock(s.To), fmtN(s.TokensIn), fmtN(s.TokensOut),
				ms(s.DurMSSum), trunc, cont)
		}
		for _, cls := range collapsedOrder {
			s := collapsed[cls]
			n := s.Requests
			w("| %s | %s | %d | %d | %s–%s | %s / %s | %s | %s | - |\n",
				s.ID, escapeCell(fmt.Sprintf("%s 单发会话 ×%d", cls, n)), s.Requests, s.Tasks,
				clock(s.From), clock(s.To), fmtN(s.TokensIn), fmtN(s.TokensOut),
				ms(s.DurMSSum), warnCount(s.Truncated))
		}
		w("\n")
	}

	// ---- Tier 1: tool declaration vs. per-turn use ----
	if len(rep.Tools) > 0 {
		w("## 工具使用（按请求形态；只计每请求当轮的调用，历史重复不计）\n\n")
		for _, t := range rep.Tools {
			w("### %s · %d 请求 · 声明 %d 个（%s/请求） · 实际调用 %d 个\n\n",
				t.Shape, t.Requests, len(t.Declared), fmtBytes(t.DeclaredBytes), len(t.Calls))
			if len(t.Calls) > 0 {
				w("| 工具 | 调用次数 |\n|---|---|\n")
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
				for _, r := range ranked {
					w("| %s | %d |\n", r.name, r.n)
				}
				w("\n")
			}
			if len(t.NeverCalled) > 0 {
				w("**声明但本窗口内从未调用（%d 个）**：%s\n\n", len(t.NeverCalled),
					escapeCell(strings.Join(t.NeverCalled, ", ")))
			}
		}
		w("统计窗口 = 本报告的输入日志范围；低频工具（如 cron 触发类）可能不在窗口内,裁剪决策建议基于 ≥1 周日志。\n\n")
	}

	// ---- Tier 2: error classes across all endpoints ----
	errs := map[string]int{}
	for _, e := range rep.Endpoints {
		for k, v := range e.ErrorClasses {
			errs[k] += v
		}
	}
	if len(errs) > 0 {
		w("## 上游错误分布\n\n| 类别 | 次数 |\n|---|---|\n")
		for _, k := range sortedKeys(errs) {
			w("| %s | %d |\n", k, errs[k])
		}
		w("\n")
	}
	w("---\n*数据源: %s · 细粒度数据见同名 .json（format %d）*\n",
		strings.Join(rep.Meta.Inputs, ", "), rep.Meta.Format)
	return b.String()
}

// ---- roll-ups (JSON keeps date×model; markdown re-aggregates) ----

type total struct {
	requests, ok, fallbacks            int
	truncated                          int
	tokensIn, tokensOut                int64
	tokensInCached, tokensInCacheWrite int64
	tokensReasoning                    int64
	tokensKnown                        int
	bytesIn, bytesOut                  int64
	tokDurWeightMS                     int64
	messages                           int64
	messagesKnown                      int
	roleChars                          map[string]int64
	finishReasons                      map[string]int
	ttftSumMS                          int64
	ttftKnown                          int
	ttfts                              []int64
	ttftP50                            int64
	Model, Protocol, Date              string
	durs                               []int64
	p50, p95                           int64
}

func (t *total) add(r Row) {
	t.requests += r.Requests
	t.ok += r.OK
	t.fallbacks += r.Fallbacks
	t.truncated += r.Truncated
	t.tokensIn += r.TokensIn
	t.tokensOut += r.TokensOut
	t.tokensInCached += r.TokensInCached
	t.tokensInCacheWrite += r.TokensInCacheWrite
	t.tokensReasoning += r.TokensReasoning
	t.tokensKnown += r.TokensKnown
	t.bytesIn += r.BytesIn
	t.bytesOut += r.BytesOut
	t.messages += r.Messages
	t.messagesKnown += r.MessagesKnown
	for role, c := range r.RoleChars {
		if t.roleChars == nil {
			t.roleChars = map[string]int64{}
		}
		t.roleChars[role] += c
	}
	for fr, n := range r.FinishReasons {
		if t.finishReasons == nil {
			t.finishReasons = map[string]int{}
		}
		t.finishReasons[fr] += n
	}
	t.ttftSumMS += r.TTFTMSSum
	t.ttftKnown += r.TTFTKnown
	// same weighted-median approximation as durs below
	for i := 0; i < r.TTFTKnown; i++ {
		t.ttfts = append(t.ttfts, r.TTFTMSP50)
	}
	if r.TokOutPerSec > 0 {
		t.tokDurWeightMS += int64(float64(r.TokensOut) / r.TokOutPerSec * 1000)
	}
	// approximate percentiles across groups via weighted medians: reuse p50/p95
	// samples weighted by request count
	for i := 0; i < r.Requests; i++ {
		t.durs = append(t.durs, r.DurMSP50)
	}
	if r.DurMSP95 > t.p95 {
		t.p95 = r.DurMSP95
	}
}

func (t *total) tokPerSec() float64 {
	if t.tokDurWeightMS == 0 {
		return 0
	}
	return float64(t.tokensOut) / (float64(t.tokDurWeightMS) / 1000)
}

// avgTriple renders per-request average tokens In/CacheHit/Out over the
// records with extractable usage; "-" when there are none.
func (t *total) avgTriple() string {
	if t.tokensKnown == 0 {
		return "-"
	}
	n := int64(t.tokensKnown)
	return tokTriple(t.tokensIn/n, t.tokensInCached/n, t.tokensOut/n)
}

// avgMessages renders the mean message count per request with a parseable
// chat body; "-" when none parsed (e.g. all-rejected traffic).
func (t *total) avgMessages() string {
	if t.messagesKnown == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(t.messages)/float64(t.messagesKnown))
}

// avgTTFT renders the mean first-token latency over records that carry
// ttft_ms; "-" when none do (logs predating the field).
func (t *total) avgTTFT() string {
	if t.ttftKnown == 0 {
		return "-"
	}
	return ms(t.ttftSumMS / int64(t.ttftKnown))
}

func (t *total) finish() {
	t.p50, _ = percentiles(t.durs)
	t.ttftP50, _ = percentiles(t.ttfts)
	t.durs, t.ttfts = nil, nil
}

// ttftP50Cell renders the rolled-up TTFT median; "-" when no record carried
// ttft_ms (logs predating the field, or nothing was ever written back).
func (t *total) ttftP50Cell() string {
	if t.ttftKnown == 0 {
		return "-"
	}
	return ms(t.ttftP50)
}

// warnCount renders a should-be-zero counter: "-" when zero, flagged when not.
func warnCount(n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("⚠️ %d", n)
}

// finishLine renders a finish_reason distribution compactly, flagging
// "length" (output hit the token cap) and "" (no finish marker — broken or
// rejected responses).
func finishLine(m map[string]int) string {
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
		parts = append(parts, fmt.Sprintf("%s×%d", label, m[k]))
	}
	return strings.Join(parts, " · ")
}

func rollupModels(rows []Row) []*total {
	m := map[string]*total{}
	for _, r := range rows {
		// Model + protocol: the same name in both protocol groups is two
		// distinct models (mirrors the report row grain).
		key := r.Model + "\x00" + r.Protocol
		g, ok := m[key]
		if !ok {
			g = &total{Model: r.Model, Protocol: r.Protocol}
			m[key] = g
		}
		g.add(r)
	}
	out := values(m)
	for _, g := range out {
		g.finish()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].requests > out[j].requests })
	return out
}

func rollupDates(rows []Row) []*total {
	m := map[string]*total{}
	for _, r := range rows {
		g, ok := m[r.Date]
		if !ok {
			g = &total{Date: r.Date}
			m[r.Date] = g
		}
		g.add(r)
	}
	out := values(m)
	for _, g := range out {
		g.finish()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

type epTotal struct {
	Endpoint     string
	attempts, ok int
	errors       map[string]int
	durs         []int64
	p50          int64
}

func rollupEndpoints(eps []EndpointRow) []*epTotal {
	m := map[string]*epTotal{}
	for _, e := range eps {
		g, ok := m[e.Endpoint]
		if !ok {
			g = &epTotal{Endpoint: e.Endpoint, errors: map[string]int{}}
			m[e.Endpoint] = g
		}
		g.attempts += e.Attempts
		g.ok += e.OK
		for k, v := range e.ErrorClasses {
			g.errors[k] += v
		}
		for i := 0; i < e.Attempts; i++ {
			g.durs = append(g.durs, e.DurMSP50)
		}
	}
	out := values(m)
	for _, g := range out {
		g.p50, _ = percentiles(g.durs)
		g.durs = nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].attempts > out[j].attempts })
	return out
}

// ---- formatting helpers ----

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

// tokTriple renders tokens as one "In / CacheHit(share%) / Out" cell, e.g.
// "41.0M / 38.8M(94.7%) / 86.9K"; "-" when there are no tokens at all.
func tokTriple(in, cached, out int64) string {
	if in == 0 && out == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s(%s) / %s", fmtN(in), fmtN(cached), pct(int(cached), int(in)), fmtN(out))
}

// cacheAbs renders an absolute cache-write token count; "-" when zero
// (the common case for non-Anthropic-only traffic).
func cacheAbs(n int64) string {
	if n == 0 {
		return "-"
	}
	return fmtN(n)
}

func ms(v int64) string {
	if v > 1000 {
		return fmt.Sprintf("%.1fs", float64(v)/1000)
	}
	return fmt.Sprintf("%dms", v)
}

func errStr(errs map[string]int) string {
	if len(errs) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(errs))
	for _, k := range sortedKeys(errs) {
		parts = append(parts, fmt.Sprintf("%s×%d", k, errs[k]))
	}
	return strings.Join(parts, " ")
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func values[V any](m map[string]V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
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
