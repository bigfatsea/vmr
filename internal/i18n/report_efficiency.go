// Ver 2026-08-01, by Sonnet 5

// Pairs with internal/report/section_efficiency.go (§7 Efficiency & Waste)
// and internal/report/metrics.go's buildFindings. See
// docs/VirtualModelRouter_Design_v4_Analytics.md's "JSON 契约" subsection: the
// six Finding* closures here are called with EN by Build (populating
// Report2.Efficiency's language-agnostic default), again with the report's
// real language by cmd_report.go's LocalizeEfficiency (overwriting it
// before vmr-report.json is written), and again independently by
// renderEfficiency for the Markdown output — three calls, same selection
// logic, only the text differs. Code (report.FindingCode) never appears
// here — it's the caller's stable identifier and never varies by language.
package i18n

import (
	"strconv"
	"strings"
)

// FindingText is one auto-discovered finding's localized parts.
type FindingText struct {
	Title, Value, Implicated, Action string
}

// EfficiencyText is section_efficiency.go's text plus metrics.go's finding
// generators, in one language.
type EfficiencyText struct {
	Title            string
	TableHeaders     [5]string // finding, metric, value, implicated, action
	ToolWasteTitle   string
	ToolWasteHeaders [6]string // shape, requests, declared, used, utilization, wasted bytes
	WindowNote       string
	DetailSummary    func(shape string, requests, declared, distinctCalled int) string
	CalledToolsTitle func(n int) string
	CalledToolLine   func(idx int, name string, n int) string
	NeverCalledTitle func(n int) string
	NeverCalledLine  func(idx int, name string) string

	ToolSchemaWasteFinding  func(shape string, requests int, wasteBytes, utilPct string) FindingText
	CacheMissFinding        func(freshTokens, sharePct string, dominantModel, dominantTokens string) FindingText
	CronRedundancyFinding   func(freshTokens, cacheEffPct, class string) FindingText
	OutputTruncationFinding func(trunc, total int) FindingText
	SlowRequestsFinding     func(sharePct string, thresholdSec int) FindingText
	ContextGrowthFinding    func(growthX, sessionID, sessionTitle string) FindingText
	// ProviderQuotaExhaustionFinding  fires from the router's own
	// real-time counter (report.ProviderQuotaRow.Live), never from this
	// report's recomputed window value — see that field's doc comment for
	// why an estimate must never be the basis of an alert. models is the
	// row's model scope (per-model Limit) or empty (shared) — a per-model
	// exhaustion must not read as a whole-account one.
	ProviderQuotaExhaustionFinding func(provider string, models []string, usedPct string, metric, every string) FindingText
}

func Efficiency(lang Lang) EfficiencyText {
	if lang == ZH {
		return EfficiencyText{
			Title:            "§7 效率与浪费 ⭐",
			TableHeaders:     [5]string{"发现", "指标", "值", "涉及", "建议"},
			ToolWasteTitle:   "**工具形态浪费 Top-5**（按浪费字节降序；完整明细见 vmr-report.json -> tools[]）",
			ToolWasteHeaders: [6]string{"形态", "请求", "声明", "已用", "利用率", "浪费字节"},
			WindowNote:       "> 统计窗口 = 本报告的输入日志范围；低频工具（如 cron 触发类）可能不在窗口内，裁剪决策建议基于 ≥1 周日志。\n\n",
			DetailSummary: func(shape string, requests, declared, distinctCalled int) string {
				return shape + " · " + strconv.Itoa(requests) + " 请求 · 声明 " + strconv.Itoa(declared) + " 个 · 实际调用 " + strconv.Itoa(distinctCalled) + " 个"
			},
			CalledToolsTitle: func(n int) string {
				return "**调用过的工具（" + strconv.Itoa(n) + " 个，按调用次数降序）：**"
			},
			CalledToolLine: func(idx int, name string, n int) string {
				return strconv.Itoa(idx) + ". " + name + " (" + strconv.Itoa(n) + " 次)"
			},
			NeverCalledTitle: func(n int) string {
				return "**声明但从未调用（" + strconv.Itoa(n) + " 个，按字母序）：**"
			},
			NeverCalledLine: func(idx int, name string) string { return strconv.Itoa(idx) + ". " + name },

			ToolSchemaWasteFinding: func(shape string, requests int, wasteBytes, utilPct string) FindingText {
				return FindingText{
					Title: "工具 schema 浪费", Value: wasteBytes,
					Implicated: shape + "/" + strconv.Itoa(requests) + " 请求",
					Action:     "裁剪未用工具；利用率 " + utilPct + "%",
				}
			},
			CacheMissFinding: func(freshTokens, sharePct string, dominantModel, dominantTokens string) FindingText {
				implicated := "全局"
				if dominantModel != "" {
					implicated = "全局，" + dominantModel + " 占 " + dominantTokens
				}
				return FindingText{
					Title: "缓存未命中输入", Value: freshTokens + " (" + sharePct + "%)",
					Implicated: implicated, Action: "检查 prompt 前缀稳定性 / 开启 provider 缓存",
				}
			},
			CronRedundancyFinding: func(freshTokens, cacheEffPct, class string) FindingText {
				return FindingText{
					Title: "定时任务冗余", Value: freshTokens + " fresh, 缓存效率 " + cacheEffPct,
					Implicated: class, Action: "拉长间隔 / 换便宜模型 / 缓存前缀",
				}
			},
			OutputTruncationFinding: func(trunc, total int) FindingText {
				return FindingText{
					Title: "输出截断", Value: strconv.Itoa(trunc) + "/" + strconv.Itoa(total),
					Implicated: "stream 中断", Action: "排查上游超时 / 提高 stream_idle",
				}
			},
			SlowRequestsFinding: func(sharePct string, thresholdSec int) FindingText {
				return FindingText{
					Title: "慢请求", Value: "~" + sharePct + "% > " + strconv.Itoa(thresholdSec) + "s",
					Implicated: "见 §4 stream_ms 归因", Action: "见 §4",
				}
			},
			ContextGrowthFinding: func(growthX, sessionID, sessionTitle string) FindingText {
				return FindingText{
					Title: "上下文膨胀", Value: "×" + growthX,
					Implicated: sessionID + " " + sessionTitle, Action: "中途 compaction",
				}
			},
			ProviderQuotaExhaustionFinding: func(provider string, models []string, usedPct string, metric, every string) FindingText {
				implicated := provider
				if len(models) > 0 {
					implicated = provider + " (" + strings.Join(models, ", ") + ")"
				}
				return FindingText{
					Title: "额度即将耗尽", Value: usedPct + "%（" + metric + " · " + every + "）",
					Implicated: implicated, Action: "检查该账户或模型的路由权重或额度配置",
				}
			},
		}
	}
	return EfficiencyText{
		Title:            "§7 Efficiency & Waste ⭐",
		TableHeaders:     [5]string{"Finding", "Metric", "Value", "Implicated", "Action"},
		ToolWasteTitle:   "**Tool Shape Waste Top-5** (sorted by wasted bytes descending; full detail in vmr-report.json -> tools[])",
		ToolWasteHeaders: [6]string{"Shape", "Requests", "Declared", "Used", "Utilization", "Wasted Bytes"},
		WindowNote:       "> Stats window = this report's input log range; low-frequency tools (e.g. cron-triggered ones) may fall outside it — base trimming decisions on ≥1 week of logs.\n\n",
		DetailSummary: func(shape string, requests, declared, distinctCalled int) string {
			return shape + " · " + strconv.Itoa(requests) + " requests · " + strconv.Itoa(declared) + " declared · " + strconv.Itoa(distinctCalled) + " actually called"
		},
		CalledToolsTitle: func(n int) string { return "**Tools called (" + strconv.Itoa(n) + ", by call count descending):**" },
		CalledToolLine: func(idx int, name string, n int) string {
			return strconv.Itoa(idx) + ". " + name + " (" + strconv.Itoa(n) + "×)"
		},
		NeverCalledTitle: func(n int) string { return "**Declared but never called (" + strconv.Itoa(n) + ", alphabetical):**" },
		NeverCalledLine:  func(idx int, name string) string { return strconv.Itoa(idx) + ". " + name },

		ToolSchemaWasteFinding: func(shape string, requests int, wasteBytes, utilPct string) FindingText {
			return FindingText{
				Title: "Tool schema waste", Value: wasteBytes,
				Implicated: shape + "/" + strconv.Itoa(requests) + " requests",
				Action:     "Trim unused tool declarations; utilization " + utilPct + "%",
			}
		},
		CacheMissFinding: func(freshTokens, sharePct string, dominantModel, dominantTokens string) FindingText {
			implicated := "Global"
			if dominantModel != "" {
				implicated = "Global, " + dominantModel + " accounts for " + dominantTokens
			}
			return FindingText{
				Title: "Cache-missed input", Value: freshTokens + " (" + sharePct + "%)",
				Implicated: implicated, Action: "Check prompt-prefix stability / enable provider caching",
			}
		},
		CronRedundancyFinding: func(freshTokens, cacheEffPct, class string) FindingText {
			return FindingText{
				Title: "Scheduled-task redundancy", Value: freshTokens + " fresh, cache efficiency " + cacheEffPct,
				Implicated: class, Action: "Lengthen the interval / switch to a cheaper model / cache the prefix",
			}
		},
		OutputTruncationFinding: func(trunc, total int) FindingText {
			return FindingText{
				Title: "Output truncation", Value: strconv.Itoa(trunc) + "/" + strconv.Itoa(total),
				Implicated: "stream interrupted", Action: "Investigate upstream timeouts / raise stream_idle",
			}
		},
		SlowRequestsFinding: func(sharePct string, thresholdSec int) FindingText {
			return FindingText{
				Title: "Slow requests", Value: "~" + sharePct + "% > " + strconv.Itoa(thresholdSec) + "s",
				Implicated: "see §4 stream_ms attribution", Action: "see §4",
			}
		},
		ContextGrowthFinding: func(growthX, sessionID, sessionTitle string) FindingText {
			return FindingText{
				Title: "Context growth", Value: "×" + growthX,
				Implicated: sessionID + " " + sessionTitle, Action: "compact mid-session",
			}
		},
		ProviderQuotaExhaustionFinding: func(provider string, models []string, usedPct string, metric, every string) FindingText {
			implicated := provider
			if len(models) > 0 {
				implicated = provider + " (" + strings.Join(models, ", ") + ")"
			}
			return FindingText{
				Title: "Quota nearing exhaustion", Value: usedPct + "% (" + metric + " · " + every + ")",
				Implicated: implicated, Action: "Review this account's or model's routing weight or quota configuration",
			}
		},
	}
}
