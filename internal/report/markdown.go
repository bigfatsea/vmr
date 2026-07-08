// Ver 2026-07-08 15:30, by Sonnet 5
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
	w("| 请求 | 成功率 | Fallback | Tokens in/out | 输入缓存命中 | 缓存写入 | 字节 in/out | 平均吞吐 |\n|---|---|---|---|---|---|---|---|\n")
	w("| %d | %s | %d 次 | %s / %s | %s | %s | %s / %s | %.1f tok/s |\n\n",
		t.requests, pct(t.ok, t.requests), t.fallbacks,
		fmtN(t.tokensIn), fmtN(t.tokensOut), cacheStr(t.tokensInCached, t.tokensIn), cacheAbs(t.tokensInCacheWrite),
		fmtN(t.bytesIn), fmtN(t.bytesOut), t.tokPerSec())

	// ---- Tier 1: per-model summary (rows rolled up over dates) ----
	w("## 按模型\n\n")
	w("| 模型 | 协议 | 请求 | 成功率 | Fallback | Tokens in/out | 输入缓存命中 | 缓存写入 | 字节 out | p50/p95 延迟 | tok/s |\n")
	w("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, m := range rollupModels(rep.Rows) {
		w("| %s | %s | %d | %s | %d | %s / %s | %s | %s | %s | %s / %s | %.1f |\n",
			m.Model, m.Protocol, m.requests, pct(m.ok, m.requests), m.fallbacks,
			fmtN(m.tokensIn), fmtN(m.tokensOut), cacheStr(m.tokensInCached, m.tokensIn), cacheAbs(m.tokensInCacheWrite), fmtN(m.bytesOut),
			ms(m.p50), ms(m.p95), m.tokPerSec())
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
		w("## 按日趋势\n\n| 日期 | 请求 | 成功率 | Tokens in/out | 输入缓存命中 | 字节 out | p95 延迟 |\n|---|---|---|---|---|---|---|\n")
		for _, d := range days {
			w("| %s | %d | %s | %s / %s | %s | %s | %s |\n",
				d.Date, d.requests, pct(d.ok, d.requests), fmtN(d.tokensIn), fmtN(d.tokensOut), cacheStr(d.tokensInCached, d.tokensIn), fmtN(d.bytesOut), ms(d.p95))
		}
		w("\n")
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
	tokensIn, tokensOut                int64
	tokensInCached, tokensInCacheWrite int64
	bytesIn, bytesOut                  int64
	tokDurWeightMS                     int64
	Model, Protocol, Date              string
	durs                               []int64
	p50, p95                           int64
}

func (t *total) add(r Row) {
	t.requests += r.Requests
	t.ok += r.OK
	t.fallbacks += r.Fallbacks
	t.tokensIn += r.TokensIn
	t.tokensOut += r.TokensOut
	t.tokensInCached += r.TokensInCached
	t.tokensInCacheWrite += r.TokensInCacheWrite
	t.bytesIn += r.BytesIn
	t.bytesOut += r.BytesOut
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

func (t *total) finish() {
	t.p50, _ = percentiles(t.durs)
	t.durs = nil
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
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// cacheStr renders a cache-read share as "35.2% (420k)"; "-" when there's no
// tokens_in to compute a share against.
func cacheStr(cached, tokensIn int64) string {
	if tokensIn == 0 {
		return "-"
	}
	return fmt.Sprintf("%s (%s)", pct(int(cached), int(tokensIn)), fmtN(cached))
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
	if v >= 10_000 {
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
