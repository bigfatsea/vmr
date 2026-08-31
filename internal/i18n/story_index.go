// Ver 2026-08-05, by Sonnet 5

// Pairs with internal/story/storyindex.go (vmr-stories.md).
package i18n

import "strconv"

// StoryIndexText is storyindex.go's text, in one language.
type StoryIndexText struct {
	Title            string
	TableHeader      string
	NotRendered      string
	UnresolvedClient string
	Footer           func(n int) string
	NoCandidatesNote string
	// NoiseFoldSummary is the <summary> line for the collapsed heartbeat
	// block (P6.3, narrowed to heartbeat-only by P14.1's IsNoiseCategory —
	// cron/subagent moved into the main table) — n is how many rows it holds.
	NoiseFoldSummary func(n int) string
}

func StoryIndexT(lang Lang) StoryIndexText {
	if lang == ZH {
		return StoryIndexText{
			Title:            "VMR Story 索引",
			TableHeader:      "| ID | Client | 时间范围 | 任务 | 轮数 | 标题 | 已渲染 |\n|---|---|---|---|---|---|---|\n",
			NotRendered:      "—",
			UnresolvedClient: "(unresolved)",
			Footer: func(n int) string {
				return "\n> ⚠ = 断头 Journey（开头截断，需 `-include-partial` 渲染）。\n\n共 " + strconv.Itoa(n) + " 个候选 journey。用 `-journey <id前缀>` 渲染其中一个，或 `-render-all` 全部渲染。\n"
			},
			NoCandidatesNote: "没有候选 journey。\n",
			NoiseFoldSummary: func(n int) string {
				return "心跳任务（" + strconv.Itoa(n) + " 个，默认折叠）"
			},
		}
	}
	return StoryIndexText{
		Title:            "VMR Story Index",
		TableHeader:      "| ID | Client | Time Range | Tasks | Steps | Title | Rendered |\n|---|---|---|---|---|---|---|\n",
		NotRendered:      "—",
		UnresolvedClient: "(unresolved)",
		Footer: func(n int) string {
			return "\n> ⚠ = head-truncated journey (pass `-include-partial` to render).\n\n" + strconv.Itoa(n) + " candidate journey(s). Use `-journey <id-prefix>` to render one, or `-render-all` for all of them.\n"
		},
		NoCandidatesNote: "No candidate journeys.\n",
		NoiseFoldSummary: func(n int) string {
			return "Heartbeat journeys (" + strconv.Itoa(n) + ", collapsed by default)"
		},
	}
}
