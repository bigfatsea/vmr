// Ver 2026-08-01, by Sonnet 5

// Pairs with cmd/vmr/cmd_story.go's stdout listing output (cmd_report.go's
// own progress lines are already English in both languages and don't need
// this — lower priority than the Markdown/JSON producing paths).
package i18n

import "fmt"

// CLIText is cmd_story.go's candidate-listing text, in one language.
type CLIText struct {
	HeadTruncatedMark  string
	StitchedMark       func(n int) string
	ListLine           func(id, mark string, steps int, from, to, title string) string
	SkippedPartialNote func(n int) string
	RenderHint         string
	UngroupedHeader    func(n int) string
	UngroupedMore      func(n int) string
	RenderedNote       func(outPath string, tasks, turns int) string
	AllRenderedSkipped func(n int) string
	AllRenderedNote    func(n int, dir string) string
}

func CLI(lang Lang) CLIText {
	if lang == ZH {
		return CLIText{
			HeadTruncatedMark: " [断头]",
			StitchedMark:      func(n int) string { return fmt.Sprintf(" [缝合×%d]", n) },
			ListLine: func(id, mark string, steps int, from, to, title string) string {
				return fmt.Sprintf("  %s%-6s %3d 轮  %s → %s  %s\n", id, mark, steps, from, to, title)
			},
			SkippedPartialNote: func(n int) string {
				return fmt.Sprintf("\n%d 个断头 journey 已跳过（-include-partial 显示；见设计文档「断头 journey」小节）\n", n)
			},
			RenderHint:      "\n用 -journey <id前缀> 渲染其中一个\n",
			UngroupedHeader: func(n int) string { return fmt.Sprintf("  前 %d 条未归组记录:\n", n) },
			UngroupedMore:   func(n int) string { return fmt.Sprintf("    ... 还有 %d 条\n", n) },
			RenderedNote: func(outPath string, tasks, turns int) string {
				return fmt.Sprintf("%s (%d 任务, %d 轮)\n", outPath, tasks, turns)
			},
			AllRenderedSkipped: func(n int) string {
				return fmt.Sprintf("\n%d 个断头 journey 已跳过（-include-partial 渲染；见设计文档「断头 journey」小节）\n", n)
			},
			AllRenderedNote: func(n int, dir string) string {
				return fmt.Sprintf("\n%d 个 journey 已渲染到 %s\n", n, dir)
			},
		}
	}
	return CLIText{
		HeadTruncatedMark: " [head-truncated]",
		StitchedMark:      func(n int) string { return fmt.Sprintf(" [stitched×%d]", n) },
		ListLine: func(id, mark string, steps int, from, to, title string) string {
			return fmt.Sprintf("  %s%-6s %3d turns  %s → %s  %s\n", id, mark, steps, from, to, title)
		},
		SkippedPartialNote: func(n int) string {
			return fmt.Sprintf("\n%d head-truncated journey(s) skipped (pass -include-partial to show; see design doc's head-truncated-journey section)\n", n)
		},
		RenderHint:      "\nUse -journey <id-prefix> to render one of these\n",
		UngroupedHeader: func(n int) string { return fmt.Sprintf("  first %d ungrouped record(s):\n", n) },
		UngroupedMore:   func(n int) string { return fmt.Sprintf("    ... %d more\n", n) },
		RenderedNote: func(outPath string, tasks, turns int) string {
			return fmt.Sprintf("%s (%d tasks, %d turns)\n", outPath, tasks, turns)
		},
		AllRenderedSkipped: func(n int) string {
			return fmt.Sprintf("\n%d head-truncated journey(s) skipped (pass -include-partial to render; see design doc's head-truncated-journey section)\n", n)
		},
		AllRenderedNote: func(n int, dir string) string {
			return fmt.Sprintf("\n%d journey(s) rendered to %s\n", n, dir)
		},
	}
}
