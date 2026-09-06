# 任务说明书：Group 3A - Report 侧 ViewModel 与固定 Markdown 序列化器

> 基于 docs/future-strategy/analyze_architecture_redesign_opus-5.md §5（D3/D4）、§9（golden 下沉）、§8.3（新旧并存一个周期）。
> **前置：Group 2B 已合并**（自包含 HTML 已删）。与 Group 3B（journey 侧）文件集正交，可并行。

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：
     - `internal/report/viewmodel*.go` (新建)、`internal/report/section_*.go`（只在接入 VM 构建器需要时最小改动）、`internal/report/*_test.go`
     - `internal/i18n/report_*.go`
     - `internal/archtest/i18n_test.go`（**本组独占**：配对对象从 `section_*.go` 换为 `viewmodel_*.go`）
   - ❌ 严禁修改：白名单以外的任何文件！特别地：**不得修改 `internal/journey/**`**（3B 并行作业中）、**不得修改 `cmd/`**（接线与旧渲染路径删除由 3C 串行完成）、**不得修改 `internal/archtest` 其他文件**。
3. 架构门禁：ViewModel 全部文案（含章节标题）进 VM（D4），渲染器只表达结构与顺序；不引模板引擎（D3）；VM 只驻内存不落盘（D12）；VM 内聚在 internal/report，**不建跨包共享的通用胖 VM**（两半区不共享）。
4. Git 规范：短命令式提交信息，严禁任何 trailer。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档由主控独占；待登记项写 `NOTES_FOR_LEAD.md`。
6. 并发抗干扰：同仓可能有 3B 并行作业（internal/journey / internal/i18n/story_*），严禁触碰；严禁 `git add .`。

## 二、具体研发任务清单 (Action Plan)

### 任务 1: ViewModel 类型与构建器（§5.2）
- 在 `internal/report` 定义 `TableVM` / `SectionVM` / `MacroReportVM`（含 `FootnoteVM`），字段以设计文档 §5.2 为基准，按现行切片聚合结构取材。
- 每个 `section_*.go` 的渲染职责迁移为 **VM 构建器**（`viewmodel_<section>.go`）：业务格式化、置信度标记（`tokens_coverage_pct`/`dur_low_n` 样式判定）、i18n 查表、排版决策全部在构建器完成，产出平坦的字符串/表格结构。
- 判据（§10 收益判断）：一个字段若在 VM 里只是原样透传且无格式化/本地化，它就不该存在。
- `internal/i18n/report_*.go` 与 VM 构建器保持一一配对（纪律保留，配对对象更换，archtest 随之更新——本组独占改 `i18n_test.go`）。

### 任务 2: 固定序列化器（§5.3）
- 约百行 Go：标题行 → 时间戳/范围 → highlights → 各 Section（`##`、Intro、表格）→ Disclaimers/Footnotes，全文无一处字面文案。
- 表格渲染沿用现有 `newTable`/列宽逻辑的输出形态，保证与旧路径产物可比。

### 任务 3: golden 测试下沉与字节等价（§9 / §8.3）
- 新增"VM 结构 golden"测试：比对 VM 构建结果（结构化）而非最终字符串，diff 可读。
- 新增"新旧路径字节等价"过渡测试：同一聚合输入分别走旧 `render_doc.go` 路径与新 VM 路径，断言 Markdown 字节一致（浮点按既有 `1e-6` 惯例不容差——这是字符串比对，必须全等）。**旧渲染路径本组保留不删**（3C 删）。
- 序列化器留少量端到端 smoke。

## 三、测试与验收步骤
1. 全局编译：`go build ./...`
2. 局部单测：`go test -race ./internal/report/... ./internal/i18n/...`
3. 架构门禁：`go test ./internal/archtest/...`
4. `git status -s` 确认无越界（尤其无 internal/journey、无 cmd 文件）
5. Commit：`git commit -m "feat(report): viewmodel layer with fixed markdown serializer, golden sunk to vm structure"`
