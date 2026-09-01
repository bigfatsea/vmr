// Ver 2026-08-12 23:40, by Opus 5

// Pairs with internal/story/render_modelusage.go — one Journey's upstream
// model usage/switches.
package i18n

import "strconv"

// ModelUsageText is render_modelusage.go's text, in one language.
type ModelUsageText struct {
	Title           string
	UsageHeader     string // header+separator row: model, steps, in, cached, out
	NoSwitches      string
	SwitchTitle     string
	SwitchLine      func(seq int, from, to string) string
	OnFailoverNote  string
	CacheImpactNote func(prevRatio, curRatio string) string
}

func ModelUsage(lang Lang) ModelUsageText {
	if lang == ZH {
		return ModelUsageText{
			Title:       "### 模型使用",
			UsageHeader: "| 模型（provider） | Step 数 | in | cached | out |\n|---|---|---|---|---|\n",
			NoSwitches:  "全程未切换上游模型。\n\n",
			SwitchTitle: "**切换记录**\n\n",
			SwitchLine: func(seq int, from, to string) string {
				return "- 第 " + strconv.Itoa(seq) + " 步：" + from + " → " + to
			},
			OnFailoverNote: "（这次切换发生在一个触发过 failover 的 Step 上）",
			CacheImpactNote: func(prevRatio, curRatio string) string {
				return " [缓存命中率 " + prevRatio + " → " + curRatio + "]"
			},
		}
	}
	return ModelUsageText{
		Title:       "### Model Usage",
		UsageHeader: "| Model (provider) | Steps | in | cached | out |\n|---|---|---|---|---|\n",
		NoSwitches:  "No upstream model switch occurred.\n\n",
		SwitchTitle: "**Switches**\n\n",
		SwitchLine: func(seq int, from, to string) string {
			return "- Step " + strconv.Itoa(seq) + ": " + from + " → " + to
		},
		OnFailoverNote: " (this switch occurred on a Step that also triggered a failover)",
		CacheImpactNote: func(prevRatio, curRatio string) string {
			return " [cache hit rate " + prevRatio + " → " + curRatio + "]"
		},
	}
}
