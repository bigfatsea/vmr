# 任务说明书：Verification Agent — P 系列修复验收

## 一、协作原则与红线约束
1. 工作区限制：仅在 `/Volumes/SSD2T/code/vmr` 目录下操作。
2. 文件白名单：
   - ✅ 允许写入：`_review/VERIFICATION_REPORT_agent_20260902.md`（你的唯一输出文件）
   - ✅ 允许读取：全部源代码、`PROJECT_REVIEW_REPORT_agent_20260902.md`、`docs/KNOWN_ISSUES.md`、`CHANGELOG.md`、git 历史
   - ❌ 严禁修改任何源代码、设计文档、KNOWN_ISSUES、CHANGELOG、PROJECT_REVIEW_REPORT。
   - ❌ 严禁读取 `_tmp/`、`archived/`、`_eval/`。
3. 这是一个**只读验收任务**：你只核实并写出验收报告，不修改任何文件（除了你唯一的输出文件）。

## 二、任务背景

上一轮全量 Review（见 `PROJECT_REVIEW_REPORT_agent_20260902.md` §5.2）发现了 4 个待修问题（P-01/P-06/P-07/P-09），并已在 `main@554d8af` 上完成修复合入。你的任务是**独立核实这些修复的正确性、测试完备性与文档同步完整性**，并检查修复是否引入了新的遗留问题。

被验收的 4 项修复：
- **P-01**（`dadf63b`）：`internal/jsonscan/rewrite.go` 的 `RewriteModel` 从 `strconv.AppendQuote` 改为 `MarshalNoEscape`，修复非 UTF-8/控制字符转义成非法 JSON 的问题。
- **P-06**（`ed0ce67`）：`internal/report/metrics.go` 的 `finishSession` 新增 `contextGrowthIn` helper，首轮无 usage 时回退 manifest.EstIn，修复 ContextGrowth 恒 0 问题。
- **P-07**（`36fc415`）：`internal/report/render_doc.go` §0 Summary 新增 interactive 占比行（`summaryInteractiveShare` + i18n `SummaryInteractiveNote`）。
- **P-09**（`2a341bd`）：`internal/story/llm_findings.go` 的 goal_drift 检测接受条件从 `DriftStepSeq > 0` 改为 `> 1`，修复漂移锚点落在 Step 1 的问题。

## 三、验收步骤（逐项）

对每项修复，独立执行以下核实并记录到输出文件：

1. **代码核验**：读当前 `main@554d8af` 的实际源码，确认修复确实生效、逻辑正确、无副作用。
2. **测试核验**：找到对应测试，独立运行验证：
   ```bash
   cd /Volumes/SSD2T/code/vmr
   go test -race ./internal/jsonscan/ -run='TestRewriteModel' -v        # P-01
   go test -race ./internal/story/ -run='TestP1b3_GoalDrift' -v          # P-09
   go test -race ./internal/report/ -run='TestContextGrowth' -v          # P-06
   go test -race ./internal/report/ -run='TestSummary' -v                # P-07
   ```
   注意：不要只看测试名，要**通读测试代码**，判断测试是否真正钉住了缺陷（而不是空转/绕过）。
3. **回归核验**：判断修复是否影响相邻功能（如 P-01 影响所有调用 RewriteModel 的 adapter/replay 路径；P-06 影响 ContextGrowth 所有消费方）。
4. **文档同步核验**：
   - CHANGELOG 是否如实记录了 4 项改动（`CHANGELOG.md` [Unreleased] 段）；
   - KNOWN_ISSUES 是否按「已修复即删除」原则正确移除了 §2.53（goal_drift）与 §2.63（§0 interactive），且无残留悬空引用；
   - `PROJECT_REVIEW_REPORT_agent_20260902.md` §5.2/§5.3 中这 4 项的描述是否已过时（仍写「待修/待做」）——如果有，记录为一个遗留问题（你**不要改主报告**，只记录）。

## 四、输出要求

在 `_review/VERIFICATION_REPORT_agent_20260902.md` 中写：

1. **逐项验收表**：每项修复一行，含：代码核验结论（✅/⚠️/❌ + 依据源码位置）、测试核验结论、回归风险、验收总判（通过/有条件通过/不通过）。
2. **遗留问题清单**（按严重性排序）：
   - 修复过程中发现的新问题（代码层面）；
   - 主报告/文档与代码现状不一致之处（如 PROJECT_REVIEW_REPORT 仍标记 P 项待做、KNOWN_ISSUES 计数、编号残留等）；
   - 修复未覆盖的相邻边界。
   每条含：问题描述、源码/文档锚点、影响、建议。
3. **验收总结**：4 项修复是否全部合格，整体是否可交付。

## 五、验收标准
- 修复生效且无副作用 → ✅
- 测试真正钉住缺陷（含负向断言）→ ✅
- 文档同步到位（CHANGELOG 有、KNOWN_ISSUES 无残留）→ ✅
- 发现问题 → 如实记录，不自行修复。
