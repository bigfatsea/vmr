// Ver 2026-09-22 02:10, by Sonnet 5

// Pairs with internal/journey/journeyindex.go (journeys/index.md).
package i18n

import "fmt"

// JourneyIndexText is journeyindex.go's text, in one language.
type JourneyIndexText struct {
	Title            string
	TableHeader      string
	NotRendered      string
	UnresolvedClient string
	Footer           func(n int) string
	NoCandidatesNote string
	// ListOnlyNote precedes the table when this run rendered no Journey
	// (-list-only / bare `vmr analyze`) — the Tasks/Rendered columns are
	// blank for every row and Steps shows the request count.
	ListOnlyNote        string
	SelfTrafficActive   func(excluded int) string
	SelfTrafficInactive string
	// NoiseFoldSummary is the <summary> line for the collapsed heartbeat
	// block (narrowed to heartbeat-only —
	// cron/subagent moved into the main table) — n is how many rows it holds.
	NoiseFoldSummary func(n int) string

	ClustersTitle     func(n int) string
	ClusterHeader     func(idx int, anchor string, size int) string
	ClusterCompareCmd func(idA, idB string) string
	// ClusterMemberLine renders one run in a cluster: id, dominant model,
	// net working time, cost (already formatted or an "unpriced" string), and
	// two badge slots (⭐ cheapest / ⚡ fastest, each "" when not this row).
	ClusterMemberLine func(id, model, wall, cost, badges string) string
	ClusterUnpriced   string
	ClusterCheapest   string // ⭐ badge
	ClusterFastest    string // ⚡ badge
}

// journeyIndexRow holds journey_index.go's literal templates, one row per
// Lang (Table's own doc comment). clusterMemberWallLabel/clusterMemberCostLabel
// are the only two pieces of ClusterMemberLine's conditional string-building
// that vary by language — the " · "+model and " "+badges segments don't
// translate, so ClusterMemberLine below builds those directly.
type journeyIndexRow struct {
	title                  string
	tableHeader            string
	notRendered            string
	unresolvedClient       string
	footerFmt              string
	noCandidatesNote       string
	listOnlyNote           string
	selfTrafficActiveFmt   string
	selfTrafficInactive    string
	noiseFoldSummaryFmt    string
	clustersTitleFmt       string
	clusterHeaderFmt       string
	clusterCompareCmdFmt   string
	clusterMemberWallLabel string
	clusterMemberCostLabel string
	clusterUnpriced        string
	clusterCheapest        string
	clusterFastest         string
}

var journeyIndexRows = Table[journeyIndexRow]{
	EN: {
		title:                  "VMR Journey Index",
		tableHeader:            "| ID | Client | Time Range | Tasks | Steps | Title | Rendered |\n|---|---|---|---|---|---|---|\n",
		notRendered:            "—",
		unresolvedClient:       "(unresolved)",
		footerFmt:              "\n> ⚠ = head-truncated journey (pass `-include-partial` to render).\n\n%d candidate journey(s). Use `-journey <id-prefix>` to render one, or `-render-all` for all of them.\n",
		noCandidatesNote:       "No candidate journeys.\n",
		listOnlyNote:           "> This run rendered no journeys (`-list-only` / bare `vmr analyze`): the Tasks and Rendered columns are blank for every row and Steps shows the request count. Render with `-render-all` or `-journey <id-prefix>` to populate them.\n\n",
		selfTrafficActiveFmt:   "> Self-traffic exclusion: active (%d candidate(s) removed).\n\n",
		selfTrafficInactive:    "> Self-traffic exclusion: not active (no `llm_key` / `self_traffic_client_tags` configured, or disabled via `-include-self-traffic`).\n\n",
		noiseFoldSummaryFmt:    "Heartbeat journeys (%d, collapsed by default)",
		clustersTitleFmt:       "## Task Clusters (%d groups)\n\n> Clustered by task-title similarity to surface multiple runs of the same goal — and which run was cheapest / fastest.\n\n",
		clusterHeaderFmt:       "### Cluster %d · %s (%d runs)\n\n",
		clusterCompareCmdFmt:   "Suggested compare command: `vmr analyze -compare %s,%s`\n\n",
		clusterMemberWallLabel: " · net ",
		clusterMemberCostLabel: " · cost ",
		clusterUnpriced:        "unpriced",
		clusterCheapest:        "⭐cheapest",
		clusterFastest:         "⚡fastest",
	},
	ZH: {
		title:                  "VMR Journey 索引",
		tableHeader:            "| ID | Client | 时间范围 | 任务 | 轮数 | 标题 | 已渲染 |\n|---|---|---|---|---|---|---|\n",
		notRendered:            "—",
		unresolvedClient:       "(unresolved)",
		footerFmt:              "\n> ⚠ = 断头 Journey（开头截断，需 `-include-partial` 渲染）。\n\n共 %d 个候选 journey。用 `-journey <id前缀>` 渲染其中一个，或 `-render-all` 全部渲染。\n",
		noCandidatesNote:       "没有候选 journey。\n",
		listOnlyNote:           "> 本次运行未渲染任何 Journey（`-list-only` / 直接 `vmr analyze`）：`任务`、`已渲染` 两列留空，`轮数` 显示请求数。用 `-render-all` 或 `-journey <id前缀>` 渲染后这些列才会填充。\n\n",
		selfTrafficActiveFmt:   "> 自指流量排除：已启用（排除 %d 条候选）。\n\n",
		selfTrafficInactive:    "> 自指流量排除：未启用（未配置 `llm_key` / `self_traffic_client_tags`，或已通过 `-include-self-traffic` 关闭）。\n\n",
		noiseFoldSummaryFmt:    "心跳任务（%d 个，默认折叠）",
		clustersTitleFmt:       "## 重复任务对照分组（%d 组）\n\n> 基于任务标题的相似度聚类，可快速定位同一任务的多次尝试，以及哪次最省 / 最快。\n\n",
		clusterHeaderFmt:       "### 分组 %d · %s (%d 次执行)\n\n",
		clusterCompareCmdFmt:   "建议对比命令：`vmr analyze -compare %s,%s`\n\n",
		clusterMemberWallLabel: " · 净工作 ",
		clusterMemberCostLabel: " · 成本 ",
		clusterUnpriced:        "未定价",
		clusterCheapest:        "⭐最省",
		clusterFastest:         "⚡最快",
	},
}

func JourneyIndexT(lang Lang) JourneyIndexText {
	r := journeyIndexRows.Row(lang)
	return JourneyIndexText{
		Title:            r.title,
		TableHeader:      r.tableHeader,
		NotRendered:      r.notRendered,
		UnresolvedClient: r.unresolvedClient,
		Footer:           func(n int) string { return fmt.Sprintf(r.footerFmt, n) },
		NoCandidatesNote: r.noCandidatesNote,
		ListOnlyNote:     r.listOnlyNote,
		SelfTrafficActive: func(excluded int) string {
			return fmt.Sprintf(r.selfTrafficActiveFmt, excluded)
		},
		SelfTrafficInactive: r.selfTrafficInactive,
		NoiseFoldSummary:    func(n int) string { return fmt.Sprintf(r.noiseFoldSummaryFmt, n) },

		ClustersTitle: func(n int) string { return fmt.Sprintf(r.clustersTitleFmt, n) },
		ClusterHeader: func(idx int, anchor string, size int) string {
			return fmt.Sprintf(r.clusterHeaderFmt, idx, anchor, size)
		},
		ClusterCompareCmd: func(idA, idB string) string {
			return fmt.Sprintf(r.clusterCompareCmdFmt, idA, idB)
		},
		ClusterMemberLine: func(id, model, wall, cost, badges string) string {
			line := "- `" + id + "`"
			if model != "" {
				line += " · " + model
			}
			if wall != "" {
				line += r.clusterMemberWallLabel + wall
			}
			line += r.clusterMemberCostLabel + cost
			if badges != "" {
				line += " " + badges
			}
			return line + "\n"
		},
		ClusterUnpriced: r.clusterUnpriced,
		ClusterCheapest: r.clusterCheapest,
		ClusterFastest:  r.clusterFastest,
	}
}
