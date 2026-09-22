// Ver 2026-09-22 02:10, by Sonnet 5

// One Journey's upstream model usage/switches text — consumed by
// internal/journey/viewmodel_build.go's vmModelUsage.
package i18n

import "fmt"

// ModelUsageText is the model-usage block's text, in one language.
type ModelUsageText struct {
	Title           string
	UsageHeader     string // header+separator row: model, steps, in, cached, out
	NoSwitches      string
	SwitchTitle     string
	SwitchLine      func(seq int, from, to string) string
	OnFailoverNote  string
	CacheImpactNote func(prevRatio, curRatio string) string
}

// modelUsageRow holds journey_modelusage.go's literal templates, one row
// per Lang (Table's own doc comment).
type modelUsageRow struct {
	title              string
	usageHeader        string
	noSwitches         string
	switchTitle        string
	switchLineFmt      string
	onFailoverNote     string
	cacheImpactNoteFmt string
}

var modelUsageRows = Table[modelUsageRow]{
	EN: {
		title:              "### Model Usage",
		usageHeader:        "| Model (provider) | Steps | in | cached | out |\n|---|---|---|---|---|\n",
		noSwitches:         "No upstream model switch occurred.\n\n",
		switchTitle:        "**Switches**\n\n",
		switchLineFmt:      "- Step %d: %s → %s",
		onFailoverNote:     " (this switch occurred on a Step that also triggered a failover)",
		cacheImpactNoteFmt: " [cache hit rate %s → %s]",
	},
	ZH: {
		title:              "### 模型使用",
		usageHeader:        "| 模型（provider） | Step 数 | in | cached | out |\n|---|---|---|---|---|\n",
		noSwitches:         "全程未切换上游模型。\n\n",
		switchTitle:        "**切换记录**\n\n",
		switchLineFmt:      "- 第 %d 步：%s → %s",
		onFailoverNote:     "（这次切换发生在一个触发过 failover 的 Step 上）",
		cacheImpactNoteFmt: " [缓存命中率 %s → %s]",
	},
}

func ModelUsage(lang Lang) ModelUsageText {
	r := modelUsageRows.Row(lang)
	return ModelUsageText{
		Title:       r.title,
		UsageHeader: r.usageHeader,
		NoSwitches:  r.noSwitches,
		SwitchTitle: r.switchTitle,
		SwitchLine: func(seq int, from, to string) string {
			return fmt.Sprintf(r.switchLineFmt, seq, from, to)
		},
		OnFailoverNote: r.onFailoverNote,
		CacheImpactNote: func(prevRatio, curRatio string) string {
			return fmt.Sprintf(r.cacheImpactNoteFmt, prevRatio, curRatio)
		},
	}
}
