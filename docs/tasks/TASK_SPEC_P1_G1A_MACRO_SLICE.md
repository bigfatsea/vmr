// Ver 2026-09-06 20:12, by gemini-3.8-flash

# 任务说明书：Group 1A - Macro 领域切片化与 Manifest 核心

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：
     - `internal/report/aggregate.go`
     - `internal/report/export.go`
     - `internal/report/rows.go`
     - `internal/report/metrics.go`
     - `internal/report/manifest.go` (新建)
     - `internal/report/slices.go` (新建)
     - `internal/report/*_test.go`
   - ❌ 严禁修改：白名单以外的任何文件！严禁修改 `internal/journey/`、`internal/reqdetail/`、`cmd/`！
3. 代码风格与架构门禁：
   - 严禁 import `router`, `server`, `config`（archtest 强制）。
   - 保持行数与函数预算，大函数按职责拆分到白名单内文件。
4. Git 规范：提交信息遵循短命令式，如 `feat(report): deconstruct Report2 into domain slices and manifest`，严禁任何 trailer（如 `Co-Authored-By`）。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占，Worker 严禁修改；待登记项写入不提交的 `NOTES_FOR_LEAD.md`。
6. 并发抗干扰：同目录下可能有其他 Worker 运行，严禁 `git add .`，仅 `git add <file>` 精准暂存白名单文件。

---

## 二、具体研发任务清单 (Action Plan)

### 任务 1: 领域切片数据结构与导出实现
- 背景：参考 `docs/future-strategy/analyze_architecture_redesign_opus-5.md` §3.2 与 §0.3 D2。彻底解构单体大 JSON，按消费者关注点拆为 5 个切片：
  - `macro/summary.json`: `Overall`, `Efficiency`, `Highlights []string` (下沉渲染期现算的 highlights，带语言)
  - `macro/finance.json`: `ByModel`, `ByClient`, `Providers`, `ProviderQuotas`, `ProviderQuotaSkipped*`, `CostCoverage`
  - `macro/reliability.json`: `Endpoints`, `EndpointsAll`, `Sticky`
  - `macro/workloads.json`: `ByDate`, `Hours`, `HoursOfDay`, `Workloads`, `ClientEndpoints`
  - `macro/context-efficiency.json`: `Sessions`, `Compactions`, `Tools`
- 目标修改：
  1. 在 `internal/report/slices.go` 中定义五大切片结构体。切片间禁止交叉引用数值（D1）。
  2. 补齐事实（§3.3）：
     - 置信度字段：Row 上增加 `TokensCoveragePct float64`, `DurLowN bool`，不再由渲染层临场打标签。
     - 成本覆盖披露：结构化 `CostCoverage { UnpricedCount int, IncompleteRateCount int, DegradedEstimatePct float64 }`。
     - 脚注与免责：`Footnotes map[string]string`, `Disclaimers []string` 结构化落盘。
     - 时间双字段：时间点落 `ts` (epoch 毫秒) + `ts_display` (`fmtutil.DisplayZone` 格式化字符串)。
  3. 提供 `WriteMacroSlices(dir string, r *Report2, lang i18n.Lang) error` 方法写入 `macro/*.json`（权限 0600，原子写入）。

### 任务 2: Manifest 核心建模与原子写入顺序 (D20 / §3.4)
- 背景：Manifest 是快照准入的唯一权威。读取方以 `manifest.json` 为准入凭证，必须最后写入。
- 目标修改：
  1. 在 `internal/report/manifest.go` 中定义 `Manifest`：
     - `Format`: 11（直接升级，无过渡期 D2）
     - `GeneratedAt`: 时间戳（包含 epoch ms 与 display 串）
     - `TimeRange`: `[2]string`
     - `Lang`: 语言代码
     - `Timezone`: 显示时区名
     - `Inputs`: 审计输入文件清单及其 sha256
     - `Slices`: map 包含 5 个 macro 切片、`requests/index.json`、`journeys/index.json`、`journeys/benchmarks.json` 的相对路径与 sha256
  2. 实现 `WriteManifest(dir string, m *Manifest) error`：
     - 必须使用 `os.CreateTemp` + `os.Rename` 原子写。
     - 纪律保证：在所有切片与索引写完后最后写入。

### 任务 3: 单元测试与切片等价性断言
- 在 `internal/report/slices_test.go` 中：
  1. 验证五大切片字段并集与原有数据源无信息丢失。
  2. 验证双时间字段 `ts` 与 `ts_display` 均有效。
  3. 验证 Manifest 计算与原子写入，sha256 校验正确。

---

## 三、测试与验收步骤
1. 运行单测：`go test -v -race ./internal/report/...`
2. 架构门禁：`go test -v ./internal/archtest/...`
3. 范围检查：`git status -s`（严格无白名单外改动）
4. 执行 Commit：`git add internal/report/... && git commit -m "feat(report): deconstruct Report2 into domain slices and manifest"`
