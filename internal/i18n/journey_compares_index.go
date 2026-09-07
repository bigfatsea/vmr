// Ver 2026-09-07, by pi

// Pairs with cmd/vmr/compares_index.go (compares/index.{json,md}, D21).
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

func ComparesIndex(lang Lang) ComparesIndexText {
	if lang == ZH {
		return ComparesIndexText{
			Title:      "# Journey 对照索引\n\n",
			EmptyState: "本目录下暂无对照。\n\n运行一次双任务对照：\n```bash\nvmr analyze -compare <id1>,<id2>\n```\n",
			Total: func(n int) string {
				return fmt.Sprintf("对照总数：%d\n\n", n)
			},
			TableHeader: "| A 侧（基线） | B 侧（候选） | 报告 |\n| --- | --- | --- |\n",
			Steps: func(n int) string {
				return fmt.Sprintf("（%d 步）", n)
			},
			PartialMark: " ⚠️ 部分",
			TimeRange: func(from, to string) string {
				return fmt.Sprintf("<br>%s ~ %s", from, to)
			},
		}
	}
	return ComparesIndexText{
		Title:      "# Journey Comparisons\n\n",
		EmptyState: "No comparisons found in this directory.\n\nTo run a pairwise journey comparison:\n```bash\nvmr analyze -compare <id1>,<id2>\n```\n",
		Total: func(n int) string {
			return fmt.Sprintf("Total comparisons: %d\n\n", n)
		},
		TableHeader: "| Side A (Baseline) | Side B (Candidate) | Report |\n| --- | --- | --- |\n",
		Steps: func(n int) string {
			return fmt.Sprintf(" (%d steps)", n)
		},
		PartialMark: " ⚠️ partial",
		TimeRange: func(from, to string) string {
			return fmt.Sprintf("<br>%s ~ %s", from, to)
		},
	}
}
