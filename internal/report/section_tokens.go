// Ver 2026-07-28 19:20, by Opus 5

// §1 成本与 Token 经济: the token-class breakdown (cached / fresh /
// cache_write / out), per-model cache efficiency, and the role-level
// character and estimated-token split.
package report

import (
	"strconv"
)

// ---- §1 成本与 Token 经济 ----
func renderCostTokens(w func(string, ...any), rep *Report2, o Row) {
	w("## §1 成本与 Token 经济\n\n")
	// token class breakdown
	w("**Token 类别分解**（basis: %d 条带 usage 的记录）\n\n", o.TokensKnown)
	tokTbl := newTable(w, "类别", "数量", "占比")
	tokTbl.row("输入-缓存命中", fmtTokens(o.TokensInCached), pctStr(o.CacheHitRate)+" of in")
	freshShare := 0.0
	if o.TokensInCached+o.TokensInFresh > 0 {
		freshShare = float64(o.TokensInFresh) / float64(o.TokensInCached+o.TokensInFresh)
	}
	tokTbl.row("输入-fresh ⭐", fmtTokens(o.TokensInFresh), pctFloat(freshShare)+" of (fresh+cached)")
	cw := ""
	if o.TokensInCacheWrite > 0 {
		cw = "（Anthropic 缓存创建，溢价计费）"
	}
	tokTbl.row("输入-cache_write", fmtTokens(o.TokensInCacheWrite), orDash(cw))
	tokTbl.row("输出", fmtTokens(o.TokensOut), "-")
	if o.TokensReasoning > 0 {
		tokTbl.row("└ 其中 reasoning", fmtTokens(o.TokensReasoning), pctStr(o.ReasoningShare)+" of out")
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
	modelTbl := newTable(w, "模型", "协议", "请求", "缓存效率⭐", "fresh", "cached", "out")
	for _, m := range rep.ByModel {
		modelTbl.row(m.Model, m.Protocol, strconv.Itoa(m.Requests),
			cacheEffCell(m.CacheEfficiency, m.TokensKnown, m.Requests),
			fmtTokens(m.TokensInFresh), fmtTokens(m.TokensInCached), fmtTokens(m.TokensOut))
	}
	w("\n")

	// role chars + estimated tokens (D-family)
	if len(o.RoleChars) > 0 {
		w("**请求消息字符、预估Token及占比**\n\n")
		roleTbl := newTable(w, "角色", "字符", "预估Token⭐", "占比⭐")
		totalTok := sumRoleChars(o.RoleTokens)
		for _, role := range sortedRoles(o.RoleChars) {
			c := o.RoleChars[role]
			t := o.RoleTokens[role]
			share := 0.0
			if totalTok > 0 {
				share = float64(t) / float64(totalTok)
			}
			roleTbl.row(role, fmtTokens(c), fmtTokens(t), pctStr(share))
		}
		w("\n> 预估Token⭐：上游 usage 不按角色拆分，无法拿到真实值，这里用粗估口径（ASCII ~4B/token，多字节 UTF-8 ~2B/token，同 §1 计费口径）；占比按预估Token 计算。\n")
		w("> takeaway: tool 结果占比最大时，上下文优化的首要杠杆是压缩 tool 返回，而非 system prompt。\n\n")
	}
}
