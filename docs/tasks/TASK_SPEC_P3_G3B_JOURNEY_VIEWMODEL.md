# 任务说明书：Group 3B - Journey 侧 ViewModel 与脊柱重建（Phase 3，与 3A 并行）

> 基于 docs/future-strategy/analyze_architecture_redesign_opus-5.md §5.0（D11）、§3.6（D18 已落地）、§9（LosslessReconstruction 改写）、§8.3。
> **前置：Group 2B 已合并。** 与 Group 3A（internal/report / internal/i18n/report_* / archtest i18n）文件集正交，可并行。

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改与新建：`internal/journey/**`（viewmodel 新文件、render_md/render_spine 最小适配、`*_test.go`）、`internal/i18n/story_*.go`
   - ❌ 严禁修改：白名单以外的任何文件！特别地：**不得修改 `internal/report/**`、`internal/i18n/report_*`（3A 并行作业中）、`cmd/`（3C）、`internal/archtest/**`（新 viewmodel 文件控制在默认 700 行预算内，超了拆文件，不动预算表）**。
3. 架构门禁：文案全进 VM（D4）、无模板引擎（D3）、VM 不落盘（D12）、VM 内聚本包不与 report 共享。
4. Git 规范：短命令式提交信息，严禁任何 trailer。
5. 共享文件禁改：CHANGELOG.md / KNOWN_ISSUES.md / 设计文档主控独占；待登记项写 `NOTES_FOR_LEAD.md`。
6. 并发抗干扰：严禁 `git add .`，精准暂存。

## 二、具体研发任务清单 (Action Plan)

### 任务 1: Journey ViewModel 构建器
- `render_spine`（决策脊柱）与 `render_md` 的渲染职责迁移为 VM 构建器：输入是**自包含的 `JourneySummary`**（1C 已落地 tree + bodies blob 表 + 三级 match），不是内存 `*Journey`。
- 全部业务格式化（参数智能截断、多阶标签、badge 判定）与 i18n 查表进构建器；`internal/i18n/story_*.go` 配对保持。

### 任务 2: 固定序列化器
- 与 report 侧同构的固定序列化器（各自内聚，不共享代码——两半区表格语义不同，见 §11.1 第 1 条）。若实现中发现可安全复用的纯函数，记录 `NOTES_FOR_LEAD.md` 由主控裁决是否下沉，不得自行建跨包依赖。

### 任务 3: 测试改写（§9）
- `LosslessReconstruction` 改写为真自包含断言：**只给 `journeys/details/j-<id>.json`，`.md` 能逐字节渲染出来**——审计日志不在输入里（D18 后的结构性保证）。
- golden 下沉到 VM 结构比对；新旧路径（旧吃 `*Journey`，新吃 `JourneySummary`）字节等价过渡测试。**旧渲染路径本组保留不删**（3C 删）。
- 补 §9 两条守卫（1C 落地后若已有则确认覆盖即可）：`bodies` 无孤儿无悬引用；三级 `match` 与脊柱实际渲染的配对一致。

### 任务 4: 2B 移交的死代码清理（其 NOTES_FOR_LEAD 第 5 条）
- 退役完全失去消费者的 `internal/i18n/story_html.go`、`story_compare_html.go`（及各自 `_test.go`）——自包含 HTML 渲染器已删，它们现在是无人引用的导出 API。
- 同理评估并删除 `journey.ComputePointOfNoReturn`、`journey.JourneySeverity`（最后一个生产调用方 render_html.go 已删，仍导出且有测试）：有测试锁行为但零生产调用方的导出 API 按 YAGNI 退役；若认为某个另有价值，记录 `NOTES_FOR_LEAD.md` 说明理由后保留。
- `compares_index.go` 的 `CompareItem.HTML` 字段不在本组白名单，不动。

## 三、测试与验收步骤
1. 全局编译：`go build ./...`
2. 局部单测：`go test -race ./internal/journey/... ./internal/i18n/...`
3. 架构门禁：`go test ./internal/archtest/...`
4. `git status -s` 确认无越界
5. Commit：`git commit -m "feat(journey): viewmodel layer over self-contained journey summary, lossless reconstruction from json only"`
