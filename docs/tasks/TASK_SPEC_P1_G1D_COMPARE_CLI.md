// Ver 2026-09-06 20:28, by gemini-3.8-flash

# 任务说明书：Group 1D - 对比索引派生与 CLI 面收敛

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：
     - `cmd/vmr/cmd_analyze.go`
     - `cmd/vmr/cmd_story.go`
     - `cmd/vmr/cmd_report.go`
     - `cmd/vmr/cmd_story_batch.go`
     - `cmd/vmr/cmd_story_setup.go`
     - `cmd/vmr/cmd_report_stories_link.go`
     - `cmd/vmr/compares_index.go` (新建)
     - `cmd/vmr/main.go`
     - `cmd/vmr/*_test.go`
   - ❌ 严禁修改：白名单以外的任何文件！严禁修改 `internal/`！
3. 代码风格与架构门禁：
   - CLI 保持薄层编排（解析参数、调用 internal 包），复杂数据逻辑下沉已有 internal 包。
   - 严禁违背 `archtest` 行数预算。
4. Git 规范：提交信息遵循短命令式，如 `feat(cli): derives compares index, retire report/story aliases, rename flags to -benchmark/-journey-only`，严禁任何 trailer。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占，Worker 严禁修改；待登记项写入不提交的 `NOTES_FOR_LEAD.md`。
6. 并发抗干扰：严禁 `git add .`，仅 `git add <file>` 精准暂存白名单文件。
7. 交接现状（重要）：本 worktree 存在前一 Agent 中断留下的**未提交半成品**（`compares_index.go`、`compares_index_test.go` 为新增，另有 4 个 cmd 文件的零散改动，**当前不能编译**）。可拣可弃：有用的拿走，编译不过的部分直接改对或重写，以最终编译通过 + 验收全绿为准，不承诺保留任何半成品代码。
8. 语义变更预警：主干上 `cmd/vmr` 现有 5 个失败测试（`TestCmdAnalyze_ProducesFullSuiteInOneOutputRoot`、`TestCmdAnalyze_DefaultSuiteExcludesHeartbeat`、`TestCmdAnalyze_DefaultSuiteRendersCronAndSubagent`、`TestCmdAnalyze_DefaultSuiteJourneyHasNoDeadDetailLinks`、`TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists`——后者还会 panic）。它们断言的是旧拓扑（`vmr-requests.md`、`stories/`、`journey-*.md`），是已合并的 Group 1A/1B/1C 改变产物拓扑后未同步的欠账，**归入本组任务 0 修复**。若排查发现根因在 `internal/`（白名单外），修复方案写入不提交的 `NOTES_FOR_LEAD.md` 交回主控，不得擅改 internal。

---

## 二、具体研发任务清单 (Action Plan)

### 任务 0: 修复 main 遗留的 5 个失败 cmd 测试（拓扑欠账）
- 背景：已合并的 Group 1A/1B/1C 改变了产物拓扑（删除人读请求索引、`stories/`→`journeys/`、`journey-*.md`→`j-*.md`），cmd 层测试未同步，main 上 `go test ./cmd/vmr/...` 为红。
- 目标修改：把上述 5 个测试的断言同步到新拓扑（`journeys/index.*`、`j-*.md`、`requests/*`）；`TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists` 的 panic（`cmd_analyze_test.go:447`）须查明是真回归还是断言过期，真回归则记录到 `NOTES_FOR_LEAD.md`。
- 验收：`go test -race ./cmd/vmr/...` 全绿。

### 任务 1: 对比索引扫描派生（D21 / §3.8）
- 背景：现状 `compares/` 子树跨调用累积，无目录列表时用户无法发现已跑过的对比；对比只能离线计算，索引是唯一的发现路径。
- 目标修改：
  1. 在 `cmd/vmr/compares_index.go` 中实现 `RebuildComparesIndex(comparesDir string) error`。
  2. 逻辑：扫描 `compares/compare-*.json`，解析并提取对比双方的 Journey ID、标题、时段，派生生成 `compares/index.json` 与 `compares/index.md`。
  3. 纪律：每次 `vmr analyze` 调用（无论是否跑对比）均触发重建；空目录时生成空状态引导；整份 `compares/` 子树严格不进 manifest 指纹（避免造成假失效）。

### 任务 2: CLI 面彻底收敛与别名清理（§8.1 / §2.1 / §2.2）
- 背景：消除历史双轨与学术命名黑话，CLI 入口收敛为 `vmr analyze`。
- 目标修改：
  1. 删除 `vmr report` 与 `vmr story` 子命令别名（一步到位，无废弃过渡期 D2/§8.1）。
  2. Flags 更名：
     - `-corpus` 更名为 `-benchmark`（不留 `-corpus` 别名）。
     - `-story-only` 更名为 `-journey-only`（不留 `-story-only` 别名）。
  3. 目录与产物输出路径切换到新拓扑（§4）：
     - 故事目录全面切为 `journeys/`，索引为 `journeys/index.{json,md}`。
     - 基准测试为 `journeys/benchmarks.{json,md}`。
     - 证据与详单切为 `requests/details/` 与 `requests/evidence/`。
  4. 提交顺序与原子性（§3.4）：在 `cmd_analyze.go` 完成各部分生成后，最后调用 `manifest` 提交。

### 任务 3: 测试同步与验证
- 更新 `cmd/vmr/*_test.go` 中对旧子命令 `report`/`story` 以及旧 flags 的调用。
- 验证 `compares/index` 扫描派生自愈能力。

---

## 三、测试与验收步骤
1. 全局编译：`go build ./...`
2. 局部单元测试：`go test -v -race ./cmd/vmr/...`（必须全绿，含任务 0 的 5 个修复）
2. 架构门禁测试：`go test -v ./internal/archtest/...`
3. 检查变更范围：`git status -s`（确认无越界文件）
4. 执行 Commit：`git add ... && git commit -m "feat(cli): derives compares index, retire report/story aliases, rename flags to -benchmark/-journey-only"`
