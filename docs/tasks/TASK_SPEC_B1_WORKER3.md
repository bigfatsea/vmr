# 任务说明书：Batch 1 - Worker 3 (Stitch Gap Pre-filter, Story User Intent, Tokenutil Coefficients Doc)

## 一、协作原则与红线约束（铁律）
1. 工作区限制：仅在当前指定的 Worktree 目录下操作。
2. 文件修改白名单（极度关键）：
   - ✅ 允许修改：
     - `internal/ctxgraph/stitch.go`
     - `internal/ctxgraph/stitch_test.go`
     - `internal/story/llm_single.go`
     - `internal/story/llm_single_test.go` (若无则新建)
     - `internal/tokenutil/tokenutil.go` (包级注释)
   - ❌ 严禁修改：白名单以外的任何文件！严禁修改 `CHANGELOG.md`、`KNOWN_ISSUES.md` 或 `docs/` 下的任何设计文档。待登记事项写入不提交的 `NOTES_FOR_LEAD.md`。
3. 代码风格与架构门禁：遵循 Go 标准库设计原则，不引入外部依赖，保持 archtest 全部通过。
4. Git 规范：提交信息简明（如 `fix(ctxgraph): pre-filter gap shadowing; refactor(story): use InitialInstruction; doc(tokenutil): annotate regression coefficients`），无 trailer。
5. 忽略目录：`_tmp/`、`archived/`、`logs/`、`reports/` 以及临时编译产物目录视为不存在，严禁读取或修改。

## 二、具体修复任务清单 (Action Plan)

### 任务 1: P-D4-3 `ctxgraph.stitch` 候选过滤前置于打分
- 背景与根因：`internal/ctxgraph/stitch.go:344-399` 候选排序按 `score → gap → idx` 三级，但其中 `overGap`（>72h）参与计算却不参与选择，赢家先按分数选出再在后续被降级，遮蔽了分数稍低但在窗内的合法前驱。
- 目标修改：
  1. 在 `stitch.go:344` 起的选择循环中，对超过 `stitchSameKeyMaxGap` 阈值的 same-bucket 候选不参与最佳评分，直接通过 `continue` 过滤（仅保留对打分有意义的合法候选），但在过滤后无候选时保留降级逻辑的诊断输出。
  2. 调整现有的 `bestOverGap` 处理路径（`:385-399` 的 if 块），确保不再被错误触发。
  3. 在 `internal/ctxgraph/stitch_test.go` 中补一条单测：构造两个候选，A 超 gap 但分数高，B 分数稍低但在 gap 内，期望 Stitch 选中 B（而非 A 被遮蔽后降级）。

### 任务 2: P-D6-8 `story.extractRootUserIntent` 复用 `j.InitialInstruction`
- 背景与根因：`internal/story/llm_single.go:63` 当前通过遍历 `j.Tasks[*].Steps[*].NewEvents` 抓取第一条 `role=user` 的消息，绕过了 `taskseg.Profile` 的 agent-dialect 过滤。OpenClaw 类客户端会将系统脚手架伪装为 `user` 角色，导致首句被脚手架污染。
- 目标修改：
  1. `extractRootUserIntent` 改为直接返回 `j.InitialInstruction`（已在 `internal/story/journey.go:446` 中由 `firstRealInstruction` 经过 dialect 过滤设置）。
  2. 保留 `truncateText` 截断逻辑（如长度超过 2000 字符）。
  3. 在 `internal/story/llm_single_test.go` 中补充测试：用含脚手架 user 事件的 Journey，验证 `extractRootUserIntent` 提取的是真正的初始意图而非脚手架。

### 任务 3: P-7-10 `tokenutil` 回归系数文档化
- 背景与根因：`internal/tokenutil/tokenutil.go` 顶部文档声称系数从线性回归得到，但缺少来源（语料、tokenizer、时间点）说明与再校准路径。属于约束不严的“裸数字”。
- 目标修改：
  1. 在 `tokenutil.go` 的 Package 注释中明确补充：这些系数的来源、回归所用 tokenizer/语料类型，以及“若需重新校准，建议通过 `tools/` 下的脚本（实现可后续）利用审计日志中 `usageOK=true` 的样本反算当前误差分布”。
  2. 若来源已不可考，如实写明；表达清晰即可。

## 三、测试与验收步骤
1. 运行相关单测：
   ```bash
   go test -v -race ./internal/ctxgraph/... ./internal/story/... ./internal/tokenutil/...
   ```
2. 运行架构门禁测试：
   ```bash
   go test -v ./internal/archtest/...
   ```
3. 检查变更状态：
   ```bash
   git status -s
   ```
4. 执行 Commit：
   ```bash
   git add internal/ctxgraph/stitch.go internal/ctxgraph/stitch_test.go internal/story/llm_single.go internal/story/llm_single_test.go internal/tokenutil/tokenutil.go
   git commit -m "fix(ctxgraph): pre-filter gap shadowing; refactor(story): use InitialInstruction; doc(tokenutil): annotate regression coefficients"
   ```
