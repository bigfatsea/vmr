// Ver 2026-09-06 20:10, by gemini-3.8-flash

# vmr analyze 架构重构 Master Action Plan 与执行跟踪账本

本文档为 `vmr analyze` 架构重构（基于 [analyze_architecture_redesign_opus-5.md](file:///Users/stanford/code/vmr/docs/future-strategy/analyze_architecture_redesign_opus-5.md)）的权威执行与进度跟踪基准。所有 Sub-Agent 派发、执行状态、Worktree 编排与验收结果均实时在此登记销项。

---

## 进度总览与阶段门禁 (Phase Gate Status)

| 阶段 | 核心目标 | 状态 | 负责人 |
|---|---|---|---|
| **Phase 0** | 基线绿灯与 `internal/story` -> `internal/journey` 包名重命名 | **DONE (已合并)** | Lead Orchestrator |
| **Phase 1** | 数据层闭环：五大切片、Manifest 核心、Journey 自包含、命名与拓扑归一 | **DONE (已合并，main 全绿)** | Multi-Agent (Lead + Workers) |
| **Phase 2** | HTML 看板：静态骨架页 + 内联 SVG 图表 + /reports/ 安全挂载 | PENDING | Multi-Agent (Lead + Workers) |
| **Phase 3** | 渲染重构：ViewModel 内存层 + 固定 Markdown 序列化器 + -render-only | PENDING | Multi-Agent (Lead + Workers) |
| **Phase 4** | 产物级缓存：L2 产物缓存 + L3 表现层缓存 + 冷热一致性守卫 | PENDING | Multi-Agent (Lead + Workers) |

---

## Phase 0: 前置基线准备 (DONE)

- [x] **Task 0.1**: 修复主干 doc_refs 测试历史失效文档引用与 quota store 并发写入竞争 (Commit: `edd4096`)
- [x] **Task 0.2**: `internal/story` -> `internal/journey` 全局重命名，全仓测试 100% 绿灯 (Commit: `050ad25`)

---

## Phase 1: 数据层闭环任务跟踪账本 (Execution Ledger)

| 任务组 | 分支名称 | 进程号 (PID) | 状态 | 修改白名单 | 独立验收命令 |
|---|---|---|---|---|---|
| **Group 1A** (Macro 切片) | `feat/p1-g1a-macro-slice` | 52752 | **DONE (已合并)** | `internal/report/aggregate.go`<br>`internal/report/export.go`<br>`internal/report/rows.go`<br>`internal/report/manifest.go`<br>`internal/report/metrics.go`<br>`internal/report/*_test.go` | `go test -v -race ./internal/report/...` |
| **Group 1C** (Journey 自包含) | `feat/p1-g1c-journey-selfcontain` | 52810 | **DONE (已合并)** | `internal/journey/structure.go`<br>`internal/journey/journey.go`<br>`internal/journey/corpus.go`<br>`internal/journey/candidates.go`<br>`internal/journey/storyindex.go`<br>`internal/journey/render_md.go`<br>`internal/journey/*_test.go` | `go test -v -race ./internal/journey/...` |
| **Group 1B** (请求拓扑与明细) | `feat/p1-g1b-requests` | 57016 | **DONE (已合并)** | `internal/reqdetail/*`<br>`internal/report/requests.go`<br>`internal/report/requests_failed.go`<br>`internal/report/detail.go`<br>`internal/report/*_test.go` | `go test -v -race ./internal/reqdetail/...`<br>`go test -v -race ./internal/report/...` |
| **Group 1D** (对比索引与 CLI) | `feat/p1-g1d-compares-cli` | 61207→主控收尾 | **DONE (已合并)** | | `cmd/vmr/cmd_analyze.go`<br>`cmd/vmr/cmd_story.go`<br>`cmd/vmr/cmd_report.go`<br>`cmd/vmr/cmd_story_batch.go`<br>`cmd/vmr/cmd_story_setup.go`<br>`cmd/vmr/cmd_report_stories_link.go`<br>`cmd/vmr/compares_index.go` (新建)<br>`cmd/vmr/main.go`<br>`cmd/vmr/*_test.go` | `go build ./...`<br>`go test -v -race ./cmd/vmr/...` |
| **Group 2A** (看板骨架资产包) | `feat/p2-g2a-dashboard` | 61591 | **DONE (已合并)** | `internal/dashboard/**` (全新建) | `go build ./...`<br>`go test -v -race ./internal/dashboard/...` |
| **Group 2C** (/reports/ 托管与配置) | `feat/p2-g2c-server-hosting` | 61590 | **DONE (已合并)** | `internal/server/reports*.go` (新建)<br>`internal/server/server.go`<br>`internal/config/{config,config_validate}.go` + tests<br>`config.example*.yaml`<br>`internal/archtest/import_boundaries_test.go` | `go test -v -race ./internal/server/... ./internal/config/...` |

---

## 任务组详细设计与交付物定义

### Group 1A: Macro 切片化与 Manifest 核心 (`internal/report/`)
- **目标与要点**：
  1. 解构 `Report2` 为 5 个独立领域切片：`macro/summary.json`, `macro/finance.json`, `macro/reliability.json`, `macro/workloads.json`, `macro/context-efficiency.json`。
  2. 补齐 §3.3 事实：`CostCoverage`（未定价、降级估算端点、不全费率汇总）、`footnotes`、`disclaimers`、`highlights` 亮点句下沉至 JSON（带语言）、双时间字段 `ts` (epoch ms) + `ts_display` (`fmtutil.DisplayZone`)。
  3. 实现 `manifest.json`：原子写最后提交，format=11，包含 5 切片指纹与元数据，作为准入检查唯一凭证（D20）。
- **验收标准**：切片并集无损覆盖原 `Report2` 字段，通过 `go test -v -race ./internal/report/...`。

### Group 1C: Journey 切片自包含化与命名归一 (`internal/journey/`)
- **目标与要点**：
  1. 实施 D18：`JourneySummary` 补齐 `structure` (tree) + 同文件 `bodies` blob 表（按内容哈希去重）。
  2. 工具配对升级为三级 `match`（`exact`, `normalized`, `positional`）。
  3. 截断口径在数据层统一（3000 字符，RespText/Reasoning 不截断）。
  4. 消除落盘文件名冗余前缀与后缀：`journey-` -> `j-<id>.{json,md}`，取消 `-partial` 文件名后缀（D19）。
  5. 实施 D20：全量运行清扫 `journeys/details/` orphan 文件，清扫严格局限于 `journeys/details/`，严禁碰触 `compares/` 与 `requests/`。
  6. `corpus` 概念与文件归一更名为 `journeys/benchmarks.{json,md}`。
- **验收标准**：blob 表无孤儿/无悬引用，三级 match 一致性测试通过，orphan 清扫测试通过，`go test -v -race ./internal/journey/...` 全绿。

### Group 1B: 请求明细与证据拓扑归位 (`internal/reqdetail/` & `internal/report/`)
- **目标与要点**：
  1. 详单文件名统一加 `r-` 前缀（`reqdetail.FileName` 单点改动）。
  2. 系统提示词证据下沉至 `requests/evidence/sysprompt-<h8>.md`。
  3. 产出机读流 `requests/failed.jsonl` 与保留排障入口 `requests/failed.md`。
  4. 彻底删除 `vmr-requests.md` 及全部客户端/定时类人读请求索引（D7）。
  5. `requests/index.json` 补齐 `SessionAnalysis` 会话/任务标题映射投影与 journey 交叉链接。
- **验收标准**：生成拓扑符合 §4 规范，`go test -v -race ./internal/reqdetail/...`。

### Group 1D: 对比索引扫描派生与 CLI 收敛 (`cmd/vmr/`)
- **目标与要点**：
  1. 实施 D21：扫 `compares/*.json` 现算 `compares/index.{json,md}`，每次 analyze 运行重建，子树不进 manifest 指纹。
  2. 收敛 CLI：删除 `vmr report` 与 `vmr story` 别名，移除 `-corpus`（更名 `-benchmark`），移除 `-story-only`（更名 `-journey-only`）。
  3. 协调串接 Group 1A/1B/1C 输出路径，确保 `manifest.json` 最后写。
- **验收标准**：`cmd/vmr` 单元测试与集成测试通过，`go test -v -race ./cmd/vmr/...`。

---

## 阶段后续规划 (Phase 2 - Phase 4)
- **Phase 2 波次划分**（按文件交集重排）：波次 A = 2A（新建 internal/dashboard，不碰 archtest——预算已由主控预登记）∥ 2C（server/config/archtest 边界）；波次 B = 2B（删旧自包含 HTML 渲染器 + 接线 WriteSkeletons + fmtutil 侧 fixture 消费 + 删 -html/-redact），必须在 2A/2C 合并后串行（与两者均有交集）。2A 的 `testdata/fmt_cases.json` 是 2B 的 Go 侧消费契约，字段结构不得擅改。
- **Phase 3 (ViewModel 与 Markdown 序列化器)**: 3A (Report ViewModel) ∥ 3B (Journey ViewModel) 可并行；3C (-render-only 整合与单轨渲染接线) 在 3A/3B 合并后串行。
- **Phase 4 (产物级缓存)**: 单组串行（Digest + L2/L3 + -no-cache + 冷热一致性），在 Phase 3 之后。
