// Ver 2026-09-22 02:20, by Sonnet 5

// Pairs with internal/report/viewmodel_efficiency.go (Efficiency & Waste)
// and internal/report/metrics.go's buildFindings. The six Finding* closures
// here are called twice, both times through buildFindings(rep, lang): once
// with EN by buildFindingsForJSON (populating Report.Efficiency and
// macro/summary.json's language-invariant baseline), and again with the
// report's actual render language by viewmodel_efficiency.go's
// vmEfficiencySection for the Markdown output — it deliberately never reads
// the already-computed rep.Efficiency, precisely so Markdown keeps
// following -lang after language-neutralization froze the JSON path to English. Code
// (report.FindingCode) never appears here — it's the caller's stable
// identifier and never varies by language; neither does Params, which
// carries the raw values a script needs to rebuild the sentence in another
// language without calling back into this package.
package i18n

import (
	"fmt"
	"strings"
)

// FindingText is one auto-discovered finding's localized parts.
type FindingText struct {
	Title, Value, Implicated, Action string
}

// EfficiencyText is viewmodel_efficiency.go's text plus metrics.go's finding
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

// efficiencyRow holds report_efficiency.go's literal templates, one row
// per Lang (Table's own doc comment). Two findings have real conditional
// composition (CacheMissFinding on dominantModel, ProviderQuotaExhaustionFinding
// on len(models)) — the condition is identical in both languages, so that
// logic is written once in Efficiency below and only the branch templates
// live here.
type efficiencyRow struct {
	title               string
	tableHeaders        [5]string
	toolWasteTitle      string
	toolWasteHeaders    [6]string
	windowNote          string
	detailSummaryFmt    string
	calledToolsTitleFmt string
	calledToolLineFmt   string
	neverCalledTitleFmt string
	neverCalledLineFmt  string

	toolSchemaWasteTitle         string
	toolSchemaWasteImplicatedFmt string
	toolSchemaWasteActionFmt     string

	cacheMissTitle                 string
	cacheMissValueFmt              string
	cacheMissImplicatedPlain       string
	cacheMissImplicatedDominantFmt string
	cacheMissAction                string

	cronRedundancyTitle    string
	cronRedundancyValueFmt string
	cronRedundancyAction   string

	outputTruncationTitle      string
	outputTruncationImplicated string
	outputTruncationAction     string

	slowRequestsTitle      string
	slowRequestsValueFmt   string
	slowRequestsImplicated string
	slowRequestsAction     string

	contextGrowthTitle    string
	contextGrowthValueFmt string
	contextGrowthAction   string

	quotaExhaustionTitle    string
	quotaExhaustionValueFmt string
	quotaExhaustionAction   string
}

var efficiencyRows = Table[efficiencyRow]{
	EN: {
		title:               "§7 Efficiency & Waste ⭐",
		tableHeaders:        [5]string{"Finding", "Metric", "Value", "Implicated", "Action"},
		toolWasteTitle:      "**Tool Shape Waste Top-5** (sorted by wasted bytes descending; full detail in macro/context-efficiency.json -> tools[])",
		toolWasteHeaders:    [6]string{"Shape", "Requests", "Declared", "Used", "Utilization", "Wasted Bytes"},
		windowNote:          "> Stats window = this report's input log range; low-frequency tools (e.g. cron-triggered ones) may fall outside it — base trimming decisions on ≥1 week of logs.\n\n",
		detailSummaryFmt:    "%s · %d requests · %d declared · %d actually called",
		calledToolsTitleFmt: "**Tools called (%d, by call count descending):**",
		calledToolLineFmt:   "%d. %s (%d×)",
		neverCalledTitleFmt: "**Declared but never called (%d, alphabetical):**",
		neverCalledLineFmt:  "%d. %s",

		toolSchemaWasteTitle:         "Tool schema waste",
		toolSchemaWasteImplicatedFmt: "%s/%d requests",
		toolSchemaWasteActionFmt:     "Trim unused tool declarations; utilization %s%%",

		cacheMissTitle:                 "Cache-missed input",
		cacheMissValueFmt:              "%s (%s%%)",
		cacheMissImplicatedPlain:       "Global",
		cacheMissImplicatedDominantFmt: "Global, %s accounts for %s",
		cacheMissAction:                "Check prompt-prefix stability / enable provider caching",

		cronRedundancyTitle:    "Scheduled-task redundancy",
		cronRedundancyValueFmt: "%s fresh, cache efficiency %s",
		cronRedundancyAction:   "Lengthen the interval / switch to a cheaper model / cache the prefix",

		outputTruncationTitle:      "Output truncation",
		outputTruncationImplicated: "stream interrupted",
		outputTruncationAction:     "Investigate upstream timeouts / raise stream_idle",

		slowRequestsTitle:      "Slow requests",
		slowRequestsValueFmt:   "~%s%% > %ds",
		slowRequestsImplicated: "see §4 stream_ms attribution",
		slowRequestsAction:     "see §4",

		contextGrowthTitle:    "Context growth",
		contextGrowthValueFmt: "×%s",
		contextGrowthAction:   "compact mid-session",

		quotaExhaustionTitle:    "Quota nearing exhaustion",
		quotaExhaustionValueFmt: "%s%% (%s · %s)",
		quotaExhaustionAction:   "Review this account's or model's routing weight or quota configuration",
	},
	ZH: {
		title:               "§7 效率与浪费 ⭐",
		tableHeaders:        [5]string{"发现", "指标", "值", "涉及", "建议"},
		toolWasteTitle:      "**工具形态浪费 Top-5**（按浪费字节降序；完整明细见 macro/context-efficiency.json -> tools[]）",
		toolWasteHeaders:    [6]string{"形态", "请求", "声明", "已用", "利用率", "浪费字节"},
		windowNote:          "> 统计窗口 = 本报告的输入日志范围；低频工具（如 cron 触发类）可能不在窗口内，裁剪决策建议基于 ≥1 周日志。\n\n",
		detailSummaryFmt:    "%s · %d 请求 · 声明 %d 个 · 实际调用 %d 个",
		calledToolsTitleFmt: "**调用过的工具（%d 个，按调用次数降序）：**",
		calledToolLineFmt:   "%d. %s (%d 次)",
		neverCalledTitleFmt: "**声明但从未调用（%d 个，按字母序）：**",
		neverCalledLineFmt:  "%d. %s",

		toolSchemaWasteTitle:         "工具 schema 浪费",
		toolSchemaWasteImplicatedFmt: "%s/%d 请求",
		toolSchemaWasteActionFmt:     "裁剪未用工具；利用率 %s%%",

		cacheMissTitle:                 "缓存未命中输入",
		cacheMissValueFmt:              "%s (%s%%)",
		cacheMissImplicatedPlain:       "全局",
		cacheMissImplicatedDominantFmt: "全局，%s 占 %s",
		cacheMissAction:                "检查 prompt 前缀稳定性 / 开启 provider 缓存",

		cronRedundancyTitle:    "定时任务冗余",
		cronRedundancyValueFmt: "%s fresh, 缓存效率 %s",
		cronRedundancyAction:   "拉长间隔 / 换便宜模型 / 缓存前缀",

		outputTruncationTitle:      "输出截断",
		outputTruncationImplicated: "stream 中断",
		outputTruncationAction:     "排查上游超时 / 提高 stream_idle",

		slowRequestsTitle:      "慢请求",
		slowRequestsValueFmt:   "~%s%% > %ds",
		slowRequestsImplicated: "见 §4 stream_ms 归因",
		slowRequestsAction:     "见 §4",

		contextGrowthTitle:    "上下文膨胀",
		contextGrowthValueFmt: "×%s",
		contextGrowthAction:   "中途 compaction",

		quotaExhaustionTitle:    "额度即将耗尽",
		quotaExhaustionValueFmt: "%s%%（%s · %s）",
		quotaExhaustionAction:   "检查该账户或模型的路由权重或额度配置",
	},
}

func Efficiency(lang Lang) EfficiencyText {
	r := efficiencyRows.Row(lang)
	return EfficiencyText{
		Title:            r.title,
		TableHeaders:     r.tableHeaders,
		ToolWasteTitle:   r.toolWasteTitle,
		ToolWasteHeaders: r.toolWasteHeaders,
		WindowNote:       r.windowNote,
		DetailSummary: func(shape string, requests, declared, distinctCalled int) string {
			return fmt.Sprintf(r.detailSummaryFmt, shape, requests, declared, distinctCalled)
		},
		CalledToolsTitle: func(n int) string { return fmt.Sprintf(r.calledToolsTitleFmt, n) },
		CalledToolLine: func(idx int, name string, n int) string {
			return fmt.Sprintf(r.calledToolLineFmt, idx, name, n)
		},
		NeverCalledTitle: func(n int) string { return fmt.Sprintf(r.neverCalledTitleFmt, n) },
		NeverCalledLine:  func(idx int, name string) string { return fmt.Sprintf(r.neverCalledLineFmt, idx, name) },

		ToolSchemaWasteFinding: func(shape string, requests int, wasteBytes, utilPct string) FindingText {
			return FindingText{
				Title: r.toolSchemaWasteTitle, Value: wasteBytes,
				Implicated: fmt.Sprintf(r.toolSchemaWasteImplicatedFmt, shape, requests),
				Action:     fmt.Sprintf(r.toolSchemaWasteActionFmt, utilPct),
			}
		},
		CacheMissFinding: func(freshTokens, sharePct string, dominantModel, dominantTokens string) FindingText {
			implicated := r.cacheMissImplicatedPlain
			if dominantModel != "" {
				implicated = fmt.Sprintf(r.cacheMissImplicatedDominantFmt, dominantModel, dominantTokens)
			}
			return FindingText{
				Title: r.cacheMissTitle, Value: fmt.Sprintf(r.cacheMissValueFmt, freshTokens, sharePct),
				Implicated: implicated, Action: r.cacheMissAction,
			}
		},
		CronRedundancyFinding: func(freshTokens, cacheEffPct, class string) FindingText {
			return FindingText{
				Title: r.cronRedundancyTitle, Value: fmt.Sprintf(r.cronRedundancyValueFmt, freshTokens, cacheEffPct),
				Implicated: class, Action: r.cronRedundancyAction,
			}
		},
		OutputTruncationFinding: func(trunc, total int) FindingText {
			return FindingText{
				Title: r.outputTruncationTitle, Value: fmt.Sprintf("%d/%d", trunc, total),
				Implicated: r.outputTruncationImplicated, Action: r.outputTruncationAction,
			}
		},
		SlowRequestsFinding: func(sharePct string, thresholdSec int) FindingText {
			return FindingText{
				Title: r.slowRequestsTitle, Value: fmt.Sprintf(r.slowRequestsValueFmt, sharePct, thresholdSec),
				Implicated: r.slowRequestsImplicated, Action: r.slowRequestsAction,
			}
		},
		ContextGrowthFinding: func(growthX, sessionID, sessionTitle string) FindingText {
			return FindingText{
				Title: r.contextGrowthTitle, Value: fmt.Sprintf(r.contextGrowthValueFmt, growthX),
				Implicated: sessionID + " " + sessionTitle, Action: r.contextGrowthAction,
			}
		},
		ProviderQuotaExhaustionFinding: func(provider string, models []string, usedPct string, metric, every string) FindingText {
			implicated := provider
			if len(models) > 0 {
				implicated = fmt.Sprintf("%s (%s)", provider, strings.Join(models, ", "))
			}
			return FindingText{
				Title: r.quotaExhaustionTitle, Value: fmt.Sprintf(r.quotaExhaustionValueFmt, usedPct, metric, every),
				Implicated: implicated, Action: r.quotaExhaustionAction,
			}
		},
	}
}
