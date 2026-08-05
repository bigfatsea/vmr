// Ver 2026-08-05, by Sonnet 5

// Pairs with internal/story/storyindex.go (vmr-stories.md).
package i18n

import "strconv"

// StoryIndexText is storyindex.go's text, in one language.
type StoryIndexText struct {
	Title            string
	TableHeader      string
	NotRendered      string
	Footer           func(n int) string
	NoCandidatesNote string
}

func StoryIndexT(lang Lang) StoryIndexText {
	if lang == ZH {
		return StoryIndexText{
			Title:       "VMR Story 索引",
			TableHeader: "| ID | Client | 时间范围 | 任务 | 轮数 | 标题 | 已渲染 |\n|---|---|---|---|---|---|---|\n",
			NotRendered: "—",
			Footer: func(n int) string {
				return "\n共 " + strconv.Itoa(n) + " 个候选 journey。用 `-journey <id前缀>` 渲染其中一个，或 `-render-all` 全部渲染。\n"
			},
			NoCandidatesNote: "没有候选 journey。\n",
		}
	}
	return StoryIndexText{
		Title:       "VMR Story Index",
		TableHeader: "| ID | Client | Time Range | Tasks | Steps | Title | Rendered |\n|---|---|---|---|---|---|---|\n",
		NotRendered: "—",
		Footer: func(n int) string {
			return "\n" + strconv.Itoa(n) + " candidate journey(s). Use `-journey <id-prefix>` to render one, or `-render-all` for all of them.\n"
		},
		NoCandidatesNote: "No candidate journeys.\n",
	}
}
