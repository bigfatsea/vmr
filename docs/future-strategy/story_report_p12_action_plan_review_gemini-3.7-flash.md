<!-- Ver 2026-08-21 19:20, by gemini-3.7-flash -->

**重要声明：针对本文档所描述问题开展核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**

---

# P12 执行计划与工作区改动全面核查与审阅报告（更新版）

## 0. 核查背景与工作区改动总览

本报告对当前工作区未提交修改（涵盖 `internal/reqdetail`、`internal/story`、`docs/KNOWN_ISSUES_sonnet-5.md`、`docs/future-strategy/story_report_full_review_opus-5.md` 以及 `docs/future-strategy/story_report_p12_action_plan_sonnet-5.md`）进行源码级核实，严格比对前期审阅意见的处置情况及执行结果的一致性。

### 0.1 工作区变更文件与职责核查表

| 文件路径 | 实际改动内容 | 对应 Action Plan / 评审项 | 状态 |
| --- | --- | --- | --- |
| [`internal/reqdetail/render.go`](file:///Users/stanford/code/vmr/internal/reqdetail/render.go) | 增加 `renderTemplateVersion = 1` 与 `renderFingerprint`；导出 `EscapeHTML` 与 `EscapeCell` | P12.1, P12.4, F-02 | ✅ 正常 |
| [`internal/reqdetail/ensure.go`](file:///Users/stanford/code/vmr/internal/reqdetail/ensure.go) | `EnsureRendered` 调整执行顺序（无条件前置 evidence 补建，再做指纹跳过比对）；增加 `readRenderFingerprint` | P12.1, F-01 (M9修复) | ✅ 正常 |
| [`internal/reqdetail/detail.go`](file:///Users/stanford/code/vmr/internal/reqdetail/detail.go) | `Render` 首行写入 `renderFingerprint`；调用 `EscapeHTML` | P12.1, P12.4 | ✅ 正常 |
| [`internal/reqdetail/diff.go`](file:///Users/stanford/code/vmr/internal/reqdetail/diff.go) | 更新包内 `EscapeHTML` 调用点 | P12.4 | ✅ 正常 |
| [`internal/reqdetail/evidence.go`](file:///Users/stanford/code/vmr/internal/reqdetail/evidence.go) | 更新包内 `EscapeHTML` 调用点 | P12.4 | ✅ 正常 |
| [`internal/reqdetail/ensure_test.go`](file:///Users/stanford/code/vmr/internal/reqdetail/ensure_test.go) | 补充指纹重写、语言切换重写、Evidence切换重写、Evidence删除重建（M9）、模板版本过期重写、无残留临时文件等 6 组回归测试 | P12.1, F-01, F-03 | ✅ 正常 |
| [`internal/story/render_md.go`](file:///Users/stanford/code/vmr/internal/story/render_md.go) | 引入 `reqdetail`；定义本地薄包装 `escapeHTML` / `escapeCell`；`j.Title` 增加转义 | P12.4, F-02 | ✅ 正常 |
| [`internal/story/render_md_test.go`](file:///Users/stanford/code/vmr/internal/story/render_md_test.go) | 增加 `TestRenderMarkdown_EscapesJourneyTitle` | F-02 | ✅ 正常 |
| [`internal/story/render_spine_args.go`](file:///Users/stanford/code/vmr/internal/story/render_spine_args.go) | `toolCallLine`（`allShort` 分支）与 `payloadBlock`（inline/折叠）全部接入转义 | P12.2, P12.3 | ✅ 正常 |
| [`internal/story/render_spine_args_test.go`](file:///Users/stanford/code/vmr/internal/story/render_spine_args_test.go) | 增加 `allShort`、inline、folded、`<script>`+`&` 特殊字符测试 | P12.2, P12.3, F-05 | ✅ 正常 |
| [`internal/story/render_spine_step.go`](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go) | `foldWhyLine`、`toolResultLine`、`renderDecisionSpine`（Task标题）、`renderSpineStep`（指令）、`renderSpineBriefStep`（指令/RespText/Reasoning）全部接入转义 | P12.2, P12.3 | ✅ 正常 |
| [`internal/story/render_spine_test.go`](file:///Users/stanford/code/vmr/internal/story/render_spine_test.go) | 增加 Task 标题、指令行、report 行、why 行、foldWhyLine、toolResultLine 的转义断言 | P12.2, P12.3 | ✅ 正常 |
| [`internal/story/storyindex.go`](file:///Users/stanford/code/vmr/internal/story/storyindex.go) | 索引表格标题列采用 `escapeCell(escapeHTML(r.Title))` 双重转义 | F-02 | ✅ 正常 |
| [`internal/story/storyindex_test.go`](file:///Users/stanford/code/vmr/internal/story/storyindex_test.go) | 增加 `TestRenderStoryIndexMarkdown_EscapesTitle`（断言管道符 `\|` 不产生额外列） | F-02 | ✅ 正常 |
| [`internal/story/render_compare.go`](file:///Users/stanford/code/vmr/internal/story/render_compare.go) | `cmp.A.Title`、`cmp.B.Title` 及分歧点 `d.TaskTitle` 增加转义 | F-02 | ✅ 正常 |
| [`internal/story/compare_test.go`](file:///Users/stanford/code/vmr/internal/story/compare_test.go) | 增加 `TestRenderComparisonMarkdown_EscapesTitles` | F-02 | ✅ 正常 |
| [`docs/KNOWN_ISSUES_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md) | `§1.41`/`§1.37` 移入 `§3`（第 36/37 项已闭环）；`§0` 与 `§4` 统计与 ROI 表同步更新 | F-06 | ✅ 正常 |
| [`docs/future-strategy/story_report_full_review_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_full_review_opus-5.md) | 第 6 章 P12 标记已完成 | F-06 | ✅ 正常 |
| [`docs/future-strategy/story_report_p12_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p12_action_plan_sonnet-5.md) | 补充完整执行记录（§6.1–§6.7）与外部审阅核查处置记录（§6.8） | 全文一致性 | ✅ 正常 |

---

## 1. 重点评审问题（F-01 ~ F-06）处置情况逐项实证核查

### F-01 【高危】`evidence/` 删除后重建场景未真正修复（实测 M9 漏洞）
* **前期问题诊断**：首版 `EnsureRendered` 将 Evidence 补建调用放在指纹比对之后，当详单指纹已匹配但 `evidence/` 被删时，函数在首个 `return` 退出，导致 45 条证据链接永久失效。
* **实际处置核查**（见 [`internal/reqdetail/ensure.go:65-79`](file:///Users/stanford/code/vmr/internal/reqdetail/ensure.go#L65-L79)）：
  ```go
  if linkEvidence {
      if _, err := EnsureSysPromptEvidence(evidenceDir, rec); err != nil {
          return "", err
      }
      if _, err := EnsureToolsEvidence(evidenceDir, rec); err != nil {
          return "", err
      }
  }

  got, err := readRenderFingerprint(target)
  if err != nil {
      return "", err
  }
  if got == renderFingerprint(lang, linkEvidence) {
      return filename, nil
  }
  ```
  在进行指纹判定前，无条件确保关联 Evidence 文件存在（内部基于 `os.Stat` 幂等，开销极小）。
* **自动化测试核查**：[`internal/reqdetail/ensure_test.go`](file:///Users/stanford/code/vmr/internal/reqdetail/ensure_test.go#L190-L225) 补充了 `TestEnsureRendered_RebuildsDeletedEvidence`，专门验证在 `linkEvidence=true` 保持不变的情况下删除 `evidence/` 目录再重跑，断言 Evidence 目录与文件被正确恢复。
* **结论**：**已彻底修复并闭环**。

---

### F-02 【高危】转义范围划界自相矛盾，且漏掉 Markdown 表格破坏高危注入点
* **前期问题诊断**：
  1. `render_md.go` 因下沉 `escapeHTML` 已在修改集内，"未计划修改"理由不成立；
  2. `storyindex.go` 索引表格单元格直接拼接 `r.Title`，遇到包含 `|` 的用户输入（如 `ps aux | grep vmr`）会导致 `vmr-stories.md` 主导航表格整行撕裂。
* **实际处置核查**：
  1. [`internal/reqdetail/render.go:78`](file:///Users/stanford/code/vmr/internal/reqdetail/render.go#L78) 导出 `EscapeCell`（转义 `|` 为 `\|` 并将换行替换为空格）；
  2. [`internal/story/storyindex.go:293`](file:///Users/stanford/code/vmr/internal/story/storyindex.go#L293) 采用 `escapeCell(escapeHTML(r.Title))` 进行双重转义保护；
  3. [`internal/story/render_md.go:36`](file:///Users/stanford/code/vmr/internal/story/render_md.go#L36) 对 `j.Title` 调用 `escapeHTML`；
  4. [`internal/story/render_compare.go`](file:///Users/stanford/code/vmr/internal/story/render_compare.go) 对 `cmp.A.Title`、`cmp.B.Title`、`d.TaskTitle` 全部接入 `escapeHTML`；
  5. 补充了 3 个针对性单元测试：`TestRenderStoryIndexMarkdown_EscapesTitle`、`TestRenderMarkdown_EscapesJourneyTitle`、`TestRenderComparisonMarkdown_EscapesTitles`。
* **结论**：**已彻底修复并闭环**，转义注入点从最初规划的 5 处扩充至 12 处且全部测试守卫。

---

### F-03 【高 ROI】模板指纹版本号（renderTemplateVersion）缺少失效测试与 P13 契约
* **前期问题诊断**：缺少 `renderTemplateVersion` 版本递增导致旧文件失效的测试，且未与 P13 建立明确跨阶段契约。
* **实际处置核查**：
  1. [`internal/reqdetail/ensure_test.go:102`](file:///Users/stanford/code/vmr/internal/reqdetail/ensure_test.go#L102) 增加了 `TestEnsureRendered_RewritesOnStaleTemplateVersion`，构造包含旧版本指纹（`v0`）的文件并断言触发自动重写；
  2. Action Plan 文档 §6.7 与 §6.8 明确确立契约：P13 实施详单内容减法（削减 Raw SSE 与历史上下文）时，只需将 `renderTemplateVersion` 递增至 `2` 即可使所有已存在详单自动更新。
* **结论**：**已彻底解决并闭环**。

---

### F-04 【设计评估】`EscapeHTML` 下沉至 `reqdetail` 的架构职责评估
* **核查结论**：`import_boundaries_test.go` 保持全绿，`story -> reqdetail` 依赖方向合法，消除了复制多份导致代码漂移的隐患，实现精简且符合 KISS 原则，确认维持现有设计。

---

### F-05 【测试完备性】特殊字符集完备性与截断顺序核查
* **实际处置核查**：
  1. [`internal/story/render_spine_args_test.go`](file:///Users/stanford/code/vmr/internal/story/render_spine_args_test.go) 增加了包含 `<script>` 与 `&` 组合的子测试；
  2. 源码走查确认：全部 12 处转义调用点均严格遵循 `escapeHTML(oneLineTruncate(...))` 的顺序（先截断/拉平，再转义），从结构上杜绝了在实体字符中间截断（如 `&amp;` 截成 `&am`）的可能。
* **结论**：**已彻底解决并闭环**。

---

### F-06 【文档与规范】`KNOWN_ISSUES` 与 DevPlan 的状态同步
* **实际处置核查**：
  1. `KNOWN_ISSUES_sonnet-5.md` 将 `§1.37` 与 `§1.41` 正式移入 `§3`（已闭环列表第 36/37 项），记录了完整的外部审阅处置历程与扩充后的 12 处转义范围；
  2. `§0` 当前状态分布重算：高危项 3→2（`1.35`/`1.36` 保持开放），中危项 7→6，§1 总数 26→24；
  3. `§4` ROI 评估总表与分档结论同步更新；
  4. `docs/future-strategy/story_report_full_review_opus-5.md` 第 6 章标记 P12 为已完成。
* **结论**：**文档与代码状态完全一致，无遗留死引用**。

---

## 2. 代码库与测试套件验证结果

在包含所有工作区改动的状态下执行完整静态检查与回归测试：

```bash
go build ./...                      # Exit 0
go test ./... -race                 # 全绿（含全仓并发竞争检测）
go test ./internal/archtest/...     # 全绿（架构边界、行数预算、函数预算、文档引用守卫）
gofmt -l .                          # 格式化完全合规（0 输出）
go vet ./...                        # 静态检查全绿
```

各文件行数严格保持在 `archtest` 预算范围内：
- `internal/story/render_spine_args.go`: 196 行（硬预算 200 行，合规）
- `internal/story/render_spine_step.go`: 395 行（默认预算 700 行，合规）
- `internal/story/render_md.go`: 174 行（默认预算 700 行，合规）
- `internal/story/storyindex.go`: 301 行（默认预算 700 行，合规）
- `internal/story/render_compare.go`: 284 行（豁免预算 850 行，合规）
- `internal/reqdetail/ensure.go`: 144 行（默认预算 700 行，合规）
- `internal/reqdetail/render.go`: 233 行（默认预算 700 行，合规）

---

## 3. 终审结论

当前工作区的全部修改与 [`story_report_p12_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p12_action_plan_sonnet-5.md) 描述的实际执行结果**逐字、逐文件完全一致**。前期独立审阅发现的 6 项问题（尤其是 M9 证据重建漏洞与索引表格管道符撕裂高危点）均已得到高质量的根本性修复，并配齐了常驻回归测试。

**P12 阶段已具备完整提交（Commit）条件。**
