// Ver 2026-08-05, by Sonnet 5

// Pairs with internal/journey/journeyindex.go (journeys/index.md).
package i18n

import "strconv"

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
	// block (P6.3, narrowed to heartbeat-only by P14.1's IsNoiseCategory —
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

func JourneyIndexT(lang Lang) JourneyIndexText {
	if lang == ZH {
		return JourneyIndexText{
			Title:            "VMR Journey 索引",
			TableHeader:      "| ID | Client | 时间范围 | 任务 | 轮数 | 标题 | 已渲染 |\n|---|---|---|---|---|---|---|\n",
			NotRendered:      "—",
			UnresolvedClient: "(unresolved)",
			Footer: func(n int) string {
				return "\n> ⚠ = 断头 Journey（开头截断，需 `-include-partial` 渲染）。\n\n共 " + strconv.Itoa(n) + " 个候选 journey。用 `-journey <id前缀>` 渲染其中一个，或 `-render-all` 全部渲染。\n"
			},
			NoCandidatesNote: "没有候选 journey。\n",
			ListOnlyNote:     "> 本次运行未渲染任何 Journey（`-list-only` / 直接 `vmr analyze`）：`任务`、`已渲染` 两列留空，`轮数` 显示请求数。用 `-render-all` 或 `-journey <id前缀>` 渲染后这些列才会填充。\n\n",
			SelfTrafficActive: func(excluded int) string {
				return "> 自指流量排除：已启用（排除 " + strconv.Itoa(excluded) + " 条候选）。\n\n"
			},
			SelfTrafficInactive: "> 自指流量排除：未启用（未配置 `llm_key` / `self_traffic_client_tags`，或已通过 `-include-self-traffic` 关闭）。\n\n",
			NoiseFoldSummary: func(n int) string {
				return "心跳任务（" + strconv.Itoa(n) + " 个，默认折叠）"
			},
			ClustersTitle: func(n int) string {
				return "## 重复任务对照分组（" + strconv.Itoa(n) + " 组）\n\n> 基于任务标题的相似度聚类，可快速定位同一任务的多次尝试，以及哪次最省 / 最快。\n\n"
			},
			ClusterHeader: func(idx int, anchor string, size int) string {
				return "### 分组 " + strconv.Itoa(idx) + " · " + anchor + " (" + strconv.Itoa(size) + " 次执行)\n\n"
			},
			ClusterCompareCmd: func(idA, idB string) string {
				return "建议对比命令：`vmr analyze -compare " + idA + "," + idB + "`\n\n"
			},
			ClusterMemberLine: func(id, model, wall, cost, badges string) string {
				line := "- `" + id + "`"
				if model != "" {
					line += " · " + model
				}
				if wall != "" {
					line += " · 净工作 " + wall
				}
				line += " · 成本 " + cost
				if badges != "" {
					line += " " + badges
				}
				return line + "\n"
			},
			ClusterUnpriced: "未定价",
			ClusterCheapest: "⭐最省",
			ClusterFastest:  "⚡最快",
		}
	}
	return JourneyIndexText{
		Title:            "VMR Journey Index",
		TableHeader:      "| ID | Client | Time Range | Tasks | Steps | Title | Rendered |\n|---|---|---|---|---|---|---|\n",
		NotRendered:      "—",
		UnresolvedClient: "(unresolved)",
		Footer: func(n int) string {
			return "\n> ⚠ = head-truncated journey (pass `-include-partial` to render).\n\n" + strconv.Itoa(n) + " candidate journey(s). Use `-journey <id-prefix>` to render one, or `-render-all` for all of them.\n"
		},
		NoCandidatesNote: "No candidate journeys.\n",
		ListOnlyNote:     "> This run rendered no journeys (`-list-only` / bare `vmr analyze`): the Tasks and Rendered columns are blank for every row and Steps shows the request count. Render with `-render-all` or `-journey <id-prefix>` to populate them.\n\n",
		SelfTrafficActive: func(excluded int) string {
			return "> Self-traffic exclusion: active (" + strconv.Itoa(excluded) + " candidate(s) removed).\n\n"
		},
		SelfTrafficInactive: "> Self-traffic exclusion: not active (no `llm_key` / `self_traffic_client_tags` configured, or disabled via `-include-self-traffic`).\n\n",
		NoiseFoldSummary: func(n int) string {
			return "Heartbeat journeys (" + strconv.Itoa(n) + ", collapsed by default)"
		},
		ClustersTitle: func(n int) string {
			return "## Task Clusters (" + strconv.Itoa(n) + " groups)\n\n> Clustered by task-title similarity to surface multiple runs of the same goal — and which run was cheapest / fastest.\n\n"
		},
		ClusterHeader: func(idx int, anchor string, size int) string {
			return "### Cluster " + strconv.Itoa(idx) + " · " + anchor + " (" + strconv.Itoa(size) + " runs)\n\n"
		},
		ClusterCompareCmd: func(idA, idB string) string {
			return "Suggested compare command: `vmr analyze -compare " + idA + "," + idB + "`\n\n"
		},
		ClusterMemberLine: func(id, model, wall, cost, badges string) string {
			line := "- `" + id + "`"
			if model != "" {
				line += " · " + model
			}
			if wall != "" {
				line += " · net " + wall
			}
			line += " · cost " + cost
			if badges != "" {
				line += " " + badges
			}
			return line + "\n"
		},
		ClusterUnpriced: "unpriced",
		ClusterCheapest: "⭐cheapest",
		ClusterFastest:  "⚡fastest",
	}
}
