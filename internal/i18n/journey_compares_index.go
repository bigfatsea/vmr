// Ver 2026-09-23 03:25, by Claude Opus 5.5

// Pairs with internal/analyze/compares_index.go (compares/index.{json,md}).
package i18n

import "fmt"

// ComparesIndexText is the compares/index.md renderer's text, in one language.
type ComparesIndexText struct {
	Title       string
	EmptyState  string
	Total       func(n int) string
	TableHeader string
	Steps       func(n int) string
	PartialMark string
	TimeRange   func(from, to string) string
}

// comparesIndexRow holds journey_compares_index.go's literal templates, one
// row per Lang (Table's own doc comment).
type comparesIndexRow struct {
	title        string
	emptyState   string
	totalFmt     string
	tableHeader  string
	stepsFmt     string
	partialMark  string
	timeRangeFmt string
}

var comparesIndexRows = Table[comparesIndexRow]{
	EN: {
		title:        "# Journey Comparisons\n\n",
		emptyState:   "No comparisons found in this directory.\n\nTo run a pairwise journey comparison:\n```bash\nvmr analyze -compare <id1>,<id2>\n```\n",
		totalFmt:     "Total comparisons: %d\n\n",
		tableHeader:  "| Side A (Baseline) | Side B (Candidate) | Report |\n| --- | --- | --- |\n",
		stepsFmt:     " (%d steps)",
		partialMark:  " ⚠️ partial",
		timeRangeFmt: "<br>%s ~ %s",
	},
	ZH: {
		title:        "# Journey 对照索引\n\n",
		emptyState:   "本目录下暂无对照。\n\n运行一次双任务对照：\n```bash\nvmr analyze -compare <id1>,<id2>\n```\n",
		totalFmt:     "对照总数：%d\n\n",
		tableHeader:  "| A 侧（基线） | B 侧（候选） | 报告 |\n| --- | --- | --- |\n",
		stepsFmt:     "（%d 步）",
		partialMark:  " ⚠️ 部分",
		timeRangeFmt: "<br>%s ~ %s",
	},
}

func ComparesIndex(lang Lang) ComparesIndexText {
	r := comparesIndexRows.Row(lang)
	return ComparesIndexText{
		Title:       r.title,
		EmptyState:  r.emptyState,
		Total:       func(n int) string { return fmt.Sprintf(r.totalFmt, n) },
		TableHeader: r.tableHeader,
		Steps:       func(n int) string { return fmt.Sprintf(r.stepsFmt, n) },
		PartialMark: r.partialMark,
		TimeRange:   func(from, to string) string { return fmt.Sprintf(r.timeRangeFmt, from, to) },
	}
}
