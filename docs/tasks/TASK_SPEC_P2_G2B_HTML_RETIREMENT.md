# 任务说明书：Group 2B - 废弃自包含 HTML 并接线骨架页（Phase 2 波次 B）

> 基于 docs/future-strategy/analyze_architecture_redesign_opus-5.md §6.4（D6/D15）、§5.4（骨架页幂等刷新）、§5.6（Go 侧 fixture 消费）。
> **前置：Group 1D、2A、2C 均已合并 main。** 本组是 Phase 2 的收尾整合组，与前三者均有文件交集，必须串行。

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与删除：
     - 删除：`internal/journey/render_html.go`、`render_html_assets.go`、`render_html_dashboard.go`、`render_compare_html.go`、`toolwaste_html.go` 及同名 `_test.go`、`internal/journey/assets/`、`internal/report/toolwaste_html.go` 及其 `_test.go`
     - 修改：`cmd/vmr/cmd_analyze.go`、`cmd/vmr/cmd_story.go`、`cmd/vmr/cmd_report.go`、`cmd/vmr/*_test.go`
     - 新建：`internal/fmtutil/fmtfixture_test.go`（消费 2A 组 fixture）
   - ❌ 严禁修改：白名单以外的任何文件！特别地：**不得修改 `internal/dashboard/**`**（若发现其 API 或 fixture 结构有问题，记录到 `NOTES_FOR_LEAD.md` 交回主控）；**不得修改 `internal/report` 的其他文件**；**不得修改 `internal/archtest/**`**（删文件后行数预算表中的对应条目过期：`render_corpus.go` 等仍在的条目不动；仅当 archtest 因删除的文件条目报错时才允许从 `file_sizes_test.go` 移除对应行，并在 NOTES_FOR_LEAD.md 说明）。
3. Git 规范：短命令式提交信息，严禁任何 trailer。
4. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占；待登记项（Breaking Change：`-html`/`-redact` 删除、自包含 HTML/`tool-waste.html` 废弃）写入不提交的 `NOTES_FOR_LEAD.md`。
5. 并发抗干扰：严禁 `git add .`，仅精准暂存白名单文件；删除文件用 `git rm`。

## 二、具体研发任务清单 (Action Plan)

### 任务 1: 删除三处自包含 HTML 渲染器（D6/D15）
- `internal/journey`：删 `render_html*.go`、`render_compare_html.go`、`assets/`；清理 `pointofnoreturn.go`、`compare.go` 等处的死引用（`RenderHTML`/`RenderComparisonHTML` 调用点与注释）。
- `internal/report`：删 `toolwaste_html.go`（`RenderToolWasteHTML`；`section_efficiency.go` 的 Markdown 渲染部分**保留**，那不是 HTML 渲染器）。
- `cmd/vmr`：删 `-html`、`-redact` 两个 flag 及全部 `htmlOn`/`redactOn` 参数管道（`renderJourney`、`compareJourneys` 签名收敛）；删 `writeToolWasteCard` 及 `tool-waste.html` 的无条件写出。

### 任务 2: 接线骨架页幂等刷新（§5.4 实现纪律）
- 每次 `vmr analyze` 调用（全量套件、`-journey`、`-compare` 等一切模式）在输出根幂等刷新骨架页：调用 `dashboard.WriteSkeletons(outDir)`，失败不阻断分析主流程但要有 stderr 告警。
- 接线点选在 cmd 层公共出口（避免每条分支各调一遍）；-compare 等模式的 outDir 语义与全量一致。

### 任务 3: Go 侧 fixture 消费测试（§5.6）
- `internal/fmtutil/fmtfixture_test.go`：读 `../dashboard/testdata/fmt_cases.json`（相对路径），对每个 case 调用对应 fmtutil 函数断言 `want`。fixture 格式契约见 `docs/tasks/TASK_SPEC_P2_G2A_DASHBOARD.md`：`{"cases":[{"fn":"FmtTokens","input":<number>,"want":"..."}]}`。**发现两侧既有显示行为不一致时**：不擅自改 fmtutil 或 dashboard 的实现——把差异逐条记录 `NOTES_FOR_LEAD.md`，测试先对齐样本中无争议的条目。
- 该测试与 dashboard 包的 Node 测试消费同一份 fixture，防 Go/JS 显示漂移。

### 任务 4: 测试同步
- 更新/删除引用 `-html`、`-redact`、`tool-waste.html`、自包含 `.html` 产物的 cmd 测试；新增"analyze 后输出根存在骨架页"的断言。

## 三、测试与验收步骤
1. 全局编译：`go build ./...`
2. 局部单测：`go test -race ./internal/journey/... ./internal/report/... ./internal/fmtutil/... ./cmd/vmr/...`
3. 架构门禁：`go test ./internal/archtest/...`
4. 手工冒烟：临时目录跑一次 `vmr analyze`（可造最小审计日志），确认输出根有六个骨架页 + 无 `tool-waste.html`；`python3 -m http.server` 打开 macro-dashboard 能加载切片（结论记 NOTES_FOR_LEAD.md）
5. `git status -s` 确认无越界
6. Commit：`git commit -m "feat(analyze): replace self-contained html renderers with skeleton pages, drop -html/-redact"`
