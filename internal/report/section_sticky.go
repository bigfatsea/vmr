// Ver 2026-07-28 20:20, by Opus 5

// §6.5 Sticky 有效性: the cache-efficiency gap between requests that stayed
// on their session's previous endpoint and those that switched. See
// StickyEffect (rows.go) for the measurement's definition and its limits.
package report

import (
	"fmt"
	"strconv"
)

func renderStickyEffect(w func(string, ...any), rep *Report2) {
	eff := rep.Sticky
	if eff == nil {
		return
	}
	w("## §6.5 Sticky 有效性 ⭐\n\n")
	w("同一会话内，落回**上一条请求所用端点**的请求 vs **换了端点**的请求，两组缓存效率对比。\n")
	w("Sticky Model（设计文档 §6.5）存在的唯一理由是让上游 prompt cache 保温——这一节是它到底有没有兑现的证据。\n\n")

	tbl := newTable(w, "组", "请求", "带 usage", "缓存效率⭐", "cached", "fresh")
	stickyRow(tbl, "落回同一端点", eff.Continued)
	stickyRow(tbl, "换了端点", eff.Switched)
	w("\n")

	// The headline: one number, stated plainly, or an explicit "not enough
	// data" — never a percentage computed from a handful of requests.
	switch {
	case eff.Continued.TokensKnown < stickyMinBasis || eff.Switched.TokensKnown < stickyMinBasis:
		w("> 样本不足（任一组带 usage 的记录 < %d 条），本期不下结论。\n", stickyMinBasis)
	case eff.Continued.CacheEfficiency > eff.Switched.CacheEfficiency:
		w("> **Sticky 在起作用**：落回同一端点的缓存效率 %s，换端点后 %s，相差 %s。\n",
			pctStr(eff.Continued.CacheEfficiency), pctStr(eff.Switched.CacheEfficiency),
			pctFloat(eff.Continued.CacheEfficiency-eff.Switched.CacheEfficiency))
	case eff.Continued.CacheEfficiency < eff.Switched.CacheEfficiency:
		w("> ⚠️ **反常**：换端点的缓存效率反而更高（%s vs %s）。样本偏斜（例如换端点的多是短会话开头）是最常见的解释，\n",
			pctStr(eff.Switched.CacheEfficiency), pctStr(eff.Continued.CacheEfficiency))
		w("> 但如果持续如此，值得检查这些虚拟模型的 sticky_ttl 是否过长、把会话钉在了缓存已经失效的端点上。\n")
	default:
		w("> 两组缓存效率相同——这批数据里 sticky 没有产生可观测的差异。\n")
	}
	w(">\n")
	w("> 口径：会话首条请求（%d 条）没有前一条可比，不计入任何一组%s。\n",
		eff.First, orDash2(eff.Ungrouped == 0, "", fmt.Sprintf("；未能归入会话的 %d 条同样不计入", eff.Ungrouped)))
	w("> **不解释切换原因**：sticky_ttl 到期、端点冷却、条件路由淘汰了 sticky 首选、该模型压根没开 sticky——事后无法区分，本节只陈述发生了什么。\n\n")

	if len(eff.ByModel) > 0 {
		w("**按虚拟模型**（sticky 是按虚拟模型配置的，这一层才是可操作的粒度）\n\n")
		mt := newTable(w, "模型", "协议", "落回同端点", "缓存效率⭐", "换了端点", "缓存效率", "差值")
		for _, m := range eff.ByModel {
			delta := "-"
			if m.Continued.TokensKnown >= stickyMinBasis && m.Switched.TokensKnown >= stickyMinBasis {
				delta = pctFloat(m.Continued.CacheEfficiency - m.Switched.CacheEfficiency)
			}
			mt.row(m.Model, m.Protocol,
				strconv.Itoa(m.Continued.Requests), cacheEffCell(m.Continued.CacheEfficiency, m.Continued.TokensKnown, m.Continued.Requests),
				strconv.Itoa(m.Switched.Requests), cacheEffCell(m.Switched.CacheEfficiency, m.Switched.TokensKnown, m.Switched.Requests),
				delta)
		}
		w("\n> 差值为空 = 该模型任一组样本不足，不足以比较。\n\n")
	}
}

// stickyMinBasis is the smallest per-group usage-bearing sample this
// section will draw a conclusion from. Below it the numbers still render
// (with the existing ⚠️low-n cell treatment) but the verdict line refuses
// to call it — a cache-efficiency gap computed from three requests is
// noise, and stating it as a finding is worse than saying nothing.
const stickyMinBasis = 20

func stickyRow(tbl *mdTable, label string, g StickyGroup) {
	tbl.row(label, strconv.Itoa(g.Requests), strconv.Itoa(g.TokensKnown),
		cacheEffCell(g.CacheEfficiency, g.TokensKnown, g.Requests),
		fmtTokens(g.TokensInCached), fmtTokens(g.TokensInFresh))
}
