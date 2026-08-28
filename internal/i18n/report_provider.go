// Ver 2026-08-12 23:40, by Opus 5

// Pairs with internal/report/section_provider.go (§2.5 Provider Spend & Quota).
package i18n

import "strconv"

// ProviderText is section_provider.go's text, in one language.
type ProviderText struct {
	Title   string
	Intro   string
	Headers []string // provider, models, requests, success rate, fresh/cached/out, cache eff, dur mean, error rate, top error class — +cost appended conditionally
	CostHdr func(cur string) string
}

func Provider(lang Lang) ProviderText {
	if lang == ZH {
		return ProviderText{
			Title:   "§2.5 账户（Provider）消耗与额度",
			Intro:   "按上游账户（config.yaml 的 providers[].name）上卷的跨模型汇总——回答\"这个账户整体消耗多少、可靠性如何\"，而不是逐个模型手动相加。\n\n",
			Headers: []string{"账户", "模型数", "请求", "成功率", "fresh/cached/out", "缓存效率", "均值耗时", "错误率", "主要错误类"},
			CostHdr: func(cur string) string { return "$ 估算" + cur },
		}
	}
	return ProviderText{
		Title:   "§2.5 Provider Spend & Quota",
		Intro:   "Cross-model roll-up by upstream account (config.yaml's providers[].name) — answers \"how much did this account consume overall, and how reliable was it\" without manually summing its endpoint rows.\n\n",
		Headers: []string{"Provider", "Models", "Requests", "Success Rate", "fresh/cached/out", "Cache Eff.", "Dur. Mean", "Error Rate", "Top Error"},
		CostHdr: func(cur string) string { return "$ Estimate" + cur },
	}
}

// ProviderQuotaText is section_provider.go's renderProviderQuotaTable text
// ("额度与消耗对照" sub-table), in one language.
type ProviderQuotaText struct {
	Title               string
	Intro               string
	Headers             []string // provider, metric, window-consumed, live-used, amount, used%, elapsed%, period range
	WindowFootnote      string
	StalePeriodFootnote string
	// ConfigChangedFootnote explains the "-‡" rendered in the live-
	// used/used% cells when Live is nil specifically because this
	// account's quota: metric/every changed since vmr-quota.json was last
	// written under its old key — distinct from the generic stale-period
	// "-" (StalePeriodFootnote), which would wrongly suggest the process
	// itself wasn't running. Only rendered when at least one row actually
	// has the marker.
	ConfigChangedFootnote string
	// OverQuotaFootnote explains the ⭐ marker appended to "已用%" when
	// Live.Pct >= 100 — Pct is deliberately never clamped
	// (LiveQuota's own doc comment), so without a visual flag an
	// over-quota account's 138.9% reads no differently than a healthy
	// 68.0% at a glance.
	OverQuotaFootnote string
	// SourcePathLine names the <log_dir>/vmr-quota.json path the live
	// column's numbers actually came from, so a reader can judge whether
	// it's plausibly the same instance that produced the input audit logs.
	SourcePathLine func(path string) string
	// CrossInstanceWarning renders only when NONE of this report's
	// input audit logs resolve under the live counter's own log_dir — the
	// single-machine variant of "these two numbers might be from different
	// instances" that can happen today (analyzing a colleague's copied-over
	// logs against this machine's own healthy counter).
	CrossInstanceWarning string
	// NoOverlapFootnote explains the † marker appended to "本报表窗口
	// 消耗" when this report's audit-log window and the account's billing
	// period share no time at all (e.g. analyzing three-month-old archived
	// logs against today's period) — only rendered when at least one row
	// actually has the marker.
	NoOverlapFootnote string
	// UnpricedFootnote explains the ◇ marker: part of the account's traffic
	// could not be priced at all and is absent from the figure (see
	// ProviderQuotaRow.WindowUnpricedPct). Distinct from "X% est.", which
	// marks traffic that IS in the figure. Rendered only when a row has it.
	UnpricedFootnote string
	// IncludeUsageFootnote explains why a token/cost account can show a near-
	// total estimated share: on streaming openai-completions the response
	// carries no usage block unless the client sent
	// stream_options.include_usage:true, and vmr never injects request
	// fields. Rendered only when at least one such row is ≥95% estimated.
	IncludeUsageFootnote string
	// FormatEstimatedShare annotates an already-formatted consumption number
	// with its degraded-estimate share, so consumption that came from a
	// byte-count estimate never renders identically to consumption backed by
	// authoritative usage. Used by BOTH consumption columns — "本周期已用"
	// (the router's live counter) and "本报表窗口消耗" (this run's own
	// recomputation) — deliberately: the two carry the same kind of
	// uncertainty and must not describe it in two different phrasings, or a
	// reader would take them for two different things.
	//
	// Takes usedStr rather than a float on purpose: number formatting stays
	// in internal/report's numStr, the same formatter the neighbouring 上限
	// cell goes through — i18n is a zero-dep leaf package, so a float
	// parameter would force a second copy of that rule here and let the two
	// drift.
	FormatEstimatedShare func(usedStr string, estimatedPct float64) string
}

func ProviderQuota(lang Lang) ProviderQuotaText {
	if lang == ZH {
		return ProviderQuotaText{
			Title: "额度与消耗对照",
			Intro: "只列配了 `quota:` 的账户，把两个不同时间窗口的消耗数字并排给出——" +
				"不做减法、不算覆盖率，各自标注来源。\n\n",
			Headers: []string{"账户", "metric", "本报表窗口消耗¹", "本周期已用²", "上限", "已用%", "周期已过%", "周期区间"},
			WindowFootnote: "> ¹ 本报表窗口消耗：从本次输入的审计日志重算得到，是**重算值**，不是路由半区当时记账的重放。" +
				"各口径的精度不同：**requests 口径无出入**——按 `倍率 × 已转发尝试数` 逐字复现路由半区的记账公式" +
				"（路由每转发一次上游成功响应记一次账，失败尝试本就不记，倍率精确相乘、不取整）；**tokens 口径**：" +
				"上游未返回精确 usage 的请求，本列与路由半区一样按字节数估算计入（不再计 0），估算占比见括号内的\"X% 估算\"标注——" +
				"两侧公式相同，唯一残留出入是路由半区数的是**上游原始字节**、本列只能数**转发给客户端的字节**，" +
				"当响应正规化改写过内容（模型名改写、`<think>` 剥离等）时两者会差出这段字节；" +
				"**cost 口径**已知一处出入——本报表的定价解析结果可能与记账当时生效的价格不同。三种口径共同的出入源：" +
				"config 里的权重/倍率在本窗口期内被改过。\n",
			StalePeriodFootnote: "> ² 本周期已用：来自 `<log_dir>/vmr-quota.json` 的实时计数器，是路由半区的权威记账——" +
				"与上一列的统计窗口不同，两者不可相减、不可求比值。计数器仍停留在更早周期时显示 `-`。括号内的\"X% 估算\"标注" +
				"这段消耗里有多少来自降级估算（上游未返回精确 usage 时的字节数粗估），不是精确记账。\n",
			ConfigChangedFootnote: "> ‡ 该账户的 `quota:` metric/every 曾被改过——盘上还留着旧配置写下的计数器，" +
				"新配置对应的计数器还没有任何记账；进程本身是健康的，不是停过，只是配置换了一把新钥匙。" +
				"（旧计数器不会被自动清理，所以这个标记只说明「改过」，不说明改于何时——一次很久以前的修改同样会让它出现。）\n",
			OverQuotaFootnote: "> ⭐ 已用% ≥ 100% 时的标记：该账户本周期已超出配置的额度上限。\n",
			SourcePathLine: func(path string) string {
				return "实时计数器来自 `" + path + "`。\n"
			},
			CrossInstanceWarning: "> 本次输入的审计日志全部不在这份实时计数器所在的 `log_dir` 下——" +
				"实时列可能来自另一台机器/另一个 vmr 实例，与左侧重算列不属于同一账户的同一份记账。\n",
			NoOverlapFootnote: "> † 本报表窗口消耗与右侧的周期区间没有任何时间交集——例如用几个月前的存档日志对照今天的计费周期，" +
				"两个数字分属完全不相干的两段时间，比\"窗口不对齐\"更极端，读到这个标记时不要把两者当作同一段时间的两种口径。\n",
			UnpricedFootnote: "> ◇ 该账户本窗口有一部分请求**完全没有计入**左侧金额——本次报表没能为它们的端点解析出费率，" +
				"所以显示的金额是偏低的。与括号里的\"估算\"不同：\"估算\"的流量在金额里，只是价格来自降级的字节估算；" +
				"◇ 的流量根本不在金额里。常见成因是审计日志比 config.yaml 更旧（模型改名或已从 models: 移除），" +
				"因为 `metric: cost` 账户当前配置里的模型在加载期就被强制要求可完整定价。" +
				"缺失比例见 `vmr-report.json` 的 `window_unpriced_pct`。\n",
			IncludeUsageFootnote: "> 某个 tokens/cost 账户的\"估算\"占比接近 100%：多半是流式 `openai-completions` 调用方没发 " +
				"`stream_options.include_usage:true`——此时上游响应里没有 usage 块，而 vmr 不会替客户端注入请求字段（字节透传）。" +
				"让客户端带上该选项，或改用 `anthropic-messages`/`openai-responses`（两者总是回传 usage）。\n",
			FormatEstimatedShare: func(usedStr string, estimatedPct float64) string {
				if estimatedPct > 0 {
					return usedStr + "（" + pctHundredStr(estimatedPct) + " 估算）"
				}
				return usedStr
			},
		}
	}
	return ProviderQuotaText{
		Title: "Quota vs. Consumption",
		Intro: "Every account that declares a `quota:`, with two independently-windowed consumption figures " +
			"placed side by side — never subtracted or ratioed, each labeled with its own source.\n\n",
		Headers: []string{"Provider", "Metric", "Window Consumed¹", "Used This Period²", "Amount", "Used%", "Elapsed%", "Period"},
		WindowFootnote: "> ¹ Window Consumed: recomputed from this run's audit-log input — a RECOMPUTED figure, not a replay " +
			"of the router's actual charge history. Accuracy differs per metric. **requests: no drift** — it reproduces the " +
			"router's own `multiplier × forwarded-attempt count` formula literally (the router charges once per forwarded " +
			"upstream success, failed attempts were never charged in the first place, and the multiplier is applied by exact " +
			"multiplication with no rounding). **tokens** — requests whose upstream returned no exact usage are counted here " +
			"with the same byte-count estimate the router charged (no longer counted as 0); the estimated share is shown as " +
			"\"X% est.\" in parentheses. Both sides run the same formula; the one residual drift is that the router counts " +
			"UPSTREAM bytes while this column can only count the bytes forwarded to the client, so the two differ by whatever " +
			"response normalization rewrote (model-name rewrite, `<think>` stripping, ...). **cost: one known drift source** — " +
			"this report's own pricing resolution may differ from the price in effect at charge time. Common to all three: " +
			"config weights/multipliers changed mid-window.\n",
		StalePeriodFootnote: "> ² Used This Period: the router's own real-time counter from `<log_dir>/vmr-quota.json` — the authoritative " +
			"account, in a different window than the column to its left. Never subtract or ratio the two. Shows `-` when the stored " +
			"counter is still on an earlier period. The parenthesized \"X% est.\" marks how much of that consumption came from a " +
			"degraded estimate (a byte-count fallback used when upstream didn't return exact usage), not authoritative metering.\n",
		ConfigChangedFootnote: "> ‡ This account's `quota:` metric/every was changed at some point — the on-disk counter is still " +
			"keyed under the OLD config, and the new key has no charges yet. The process itself is healthy and running; only the " +
			"config changed underneath it. (Superseded keys are never cleaned up, so this marker says the config *was* changed, " +
			"not *when* — a long-ago edit raises it just the same.)\n",
		OverQuotaFootnote: "> ⭐ marks Used% >= 100%: this account is over its configured quota for the current period.\n",
		SourcePathLine: func(path string) string {
			return "The real-time counter is read from `" + path + "`.\n"
		},
		CrossInstanceWarning: "> None of this report's input audit logs resolve under that counter's `log_dir` — the real-time column " +
			"may be from a different machine/vmr instance than the one that produced the logs, not necessarily the same account's " +
			"same books as the recomputed column to its left.\n",
		NoOverlapFootnote: "> † Window Consumed shares NO time at all with the period range to its right — e.g. analyzing months-old " +
			"archived logs against today's billing period. More extreme than the routine \"windows don't align\" case: the two " +
			"numbers belong to two entirely unrelated stretches of time, not two views of the same one.\n",
		UnpricedFootnote: "> ◇ Some of this account's requests in this window are **not in the figure at all** — this report " +
			"resolved no rate for their endpoint, so the amount shown is systematically low. Different from the parenthesized " +
			"\"est.\": that traffic IS in the amount, just priced from a degraded byte-count estimate; ◇ traffic is missing " +
			"outright. The usual cause is an audit log older than config.yaml (a model since renamed or dropped from `models:`), " +
			"since a `metric: cost` account's currently-configured models are all required to price completely at load time. " +
			"The missing share is `window_unpriced_pct` in `vmr-report.json`.\n",
		IncludeUsageFootnote: "> A tokens/cost account showing a near-100% estimated share is usually a streaming `openai-completions` " +
			"caller that didn't send `stream_options.include_usage:true` — without it the upstream response carries no usage block, " +
			"and vmr never injects request fields (byte-faithful). Have the client send that option, or use `anthropic-messages` / " +
			"`openai-responses` (both always report usage).\n",
		FormatEstimatedShare: func(usedStr string, estimatedPct float64) string {
			if estimatedPct > 0 {
				return usedStr + " (" + pctHundredStr(estimatedPct) + " est.)"
			}
			return usedStr
		},
	}
}

// pctHundredStr formats an already-percentage value (0-100 scale) to one
// decimal place with a trailing "%" — mirrors internal/report's pctHundred.
func pctHundredStr(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64) + "%"
}
