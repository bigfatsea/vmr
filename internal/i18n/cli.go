// Ver 2026-09-22 02:05, by Sonnet 5

// Pairs with cmd/vmr/cmd_journey.go's stdout listing output (cmd_report.go's
// own progress lines are already English in both languages and don't need
// this — lower priority than the Markdown/JSON producing paths).
package i18n

import "fmt"

// CLIText is cmd_journey.go's candidate-listing text, in one language.
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

// cliRow holds cli.go's literal templates, one row per Lang (Table's own
// doc comment). Adding a language means adding a row here, never touching
// the closures in CLI below.
type cliRow struct {
	headTruncatedMark     string
	stitchedMarkFmt       string
	listLineFmt           string
	skippedPartialNoteFmt string
	renderHint            string
	ungroupedHeaderFmt    string
	ungroupedMoreFmt      string
	renderedNoteFmt       string
	allRenderedSkippedFmt string
	allRenderedNoteFmt    string
}

var cliRows = Table[cliRow]{
	EN: {
		headTruncatedMark:     " [head-truncated]",
		stitchedMarkFmt:       " [stitched×%d]",
		listLineFmt:           "  %s%-6s %3d turns  %s → %s  %s\n",
		skippedPartialNoteFmt: "\n%d head-truncated journey(s) skipped (pass -include-partial to show; see design doc's head-truncated-journey section)\n",
		renderHint:            "\nUse -journey <id-prefix> to render one of these\n",
		ungroupedHeaderFmt:    "  first %d ungrouped record(s):\n",
		ungroupedMoreFmt:      "    ... %d more\n",
		renderedNoteFmt:       "%s (%d tasks, %d turns)\n",
		allRenderedSkippedFmt: "\n%d head-truncated journey(s) skipped (pass -include-partial to render; see design doc's head-truncated-journey section)\n",
		allRenderedNoteFmt:    "\n%d journey(s) rendered to %s\n",
	},
	ZH: {
		headTruncatedMark:     " [断头]",
		stitchedMarkFmt:       " [缝合×%d]",
		listLineFmt:           "  %s%-6s %3d 轮  %s → %s  %s\n",
		skippedPartialNoteFmt: "\n%d 个断头 journey 已跳过（-include-partial 显示；见设计文档「断头 journey」小节）\n",
		renderHint:            "\n用 -journey <id前缀> 渲染其中一个\n",
		ungroupedHeaderFmt:    "  前 %d 条未归组记录:\n",
		ungroupedMoreFmt:      "    ... 还有 %d 条\n",
		renderedNoteFmt:       "%s (%d 任务, %d 轮)\n",
		allRenderedSkippedFmt: "\n%d 个断头 journey 已跳过（-include-partial 渲染；见设计文档「断头 journey」小节）\n",
		allRenderedNoteFmt:    "\n%d 个 journey 已渲染到 %s\n",
	},
}

func CLI(lang Lang) CLIText {
	r := cliRows.Row(lang)
	return CLIText{
		HeadTruncatedMark: r.headTruncatedMark,
		StitchedMark:      func(n int) string { return fmt.Sprintf(r.stitchedMarkFmt, n) },
		ListLine: func(id, mark string, steps int, from, to, title string) string {
			return fmt.Sprintf(r.listLineFmt, id, mark, steps, from, to, title)
		},
		SkippedPartialNote: func(n int) string { return fmt.Sprintf(r.skippedPartialNoteFmt, n) },
		RenderHint:         r.renderHint,
		UngroupedHeader:    func(n int) string { return fmt.Sprintf(r.ungroupedHeaderFmt, n) },
		UngroupedMore:      func(n int) string { return fmt.Sprintf(r.ungroupedMoreFmt, n) },
		RenderedNote: func(outPath string, tasks, turns int) string {
			return fmt.Sprintf(r.renderedNoteFmt, outPath, tasks, turns)
		},
		AllRenderedSkipped: func(n int) string { return fmt.Sprintf(r.allRenderedSkippedFmt, n) },
		AllRenderedNote:    func(n int, dir string) string { return fmt.Sprintf(r.allRenderedNoteFmt, n, dir) },
	}
}
