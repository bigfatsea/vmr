// Ver 2026-08-20 13:45, by Gemini 3.7 Flash

# vmr 日志分析体系重构 — P4 ActionPlan 审阅与落地执行核查报告

**重要声明：本文档以客观代码事实为唯一核心依据，严格对齐上位架构文档 `docs/future-strategy/story_report_architecture_opus-5.md`（§7.4b、§7.6a、§8）、阶段规划文档 `docs/future-strategy/story_report_dev_plan_opus-5.md`（P4/P5 阶段定义与硬依赖），以及当前工作区未提交代码与实际执行测试结果。**

---

## 0. 审阅与核查定位

本文档对 `docs/future-strategy/story_report_p4_action_plan_sonnet-5.md`（以下简称 **P4 ActionPlan**）及其在当前工作区中的落地代码实现、测试覆盖与产物进行了全量事实核查。核查重点覆盖此前两份独立审阅报告（Gemini 评审报告初版及 `docs/future-strategy/story_report_p4_action_plan_review_pi.md`，以下简称 **Pi 评审**）所提出的全部问题，并补充本次深入代码走查发现的新问题与技术细节。

### 总体核查结论

1. **范围与执行结果高度符合**：工作区未提交改动严格约束在 P4 目标范围内，`journey-<id>.json` 已成功补齐 Task/Step/Event/ToolCall 机读层结构（`structure` 字段），真实语料（22 步 / 33 次工具调用）实测输出 92.9KB，与 ActionPlan 记录的 92KB 基线吻合。
2. **两轮独立评审意见均得到高质量处置**：前期发现的严重设计盲区（图层级事实丢失、生产写入点遗漏、伪无损重建、ToolCall 结果越界内联等）均已在源码中完成修复与常驻测试锁定。
3. **全仓质量守卫全绿**：`go test ./...`（含 `-race`）、`go test ./internal/archtest/...`、`go vet ./...`、`gofmt -l .` 全部通过，真实语料与边界测试验证闭环。
4. **发现 1 处微小模式差异（PascalCase 子对象）与 1 处防御性建议**：在 `usage` 字段 JSON 命名风格及 `BuildStructure` 入参防御上记录事实，供后续阶段参考。

---

## 1. 已知问题处置核查总览（逐项源码对照）

下表对两份审阅报告提出的共 16 项问题在当前代码中的实际处置情况进行事实核对：

| 审阅来源与序号 | 核心问题简述 | 严重级别 | 实际处置状态 | 源码依据与验证结果 |
| :--- | :--- | :--- | :--- | :--- |
| **Gemini §1.1 / Pi T4** | `StepStructure` 遗漏 `DeltaStart` 与伪无损重建 | 🔴 High | ✅ **超预期解决** | 在 [`structure.go:178`](file:///Users/stanford/code/vmr/internal/story/structure.go#L178) 增加 `DeltaStart`（作为导航便利字段）；采纳 Pi 建议，在 [`structure_test.go:166-312`](file:///Users/stanford/code/vmr/internal/story/structure_test.go#L166-L312) 重写为真正的**哈希匹配**无损重建测试，并构造重复消息去重夹具验证 |
| **Gemini §1.2 / Pi T2** | 排除 `StitchEdge`、`Compaction`、`Edit` 导致图分析事实永久灭失 | 🔴 High | ✅ **彻底解决** | 在 [`structure.go:106-149`](file:///Users/stanford/code/vmr/internal/story/structure.go#L106-L149) 补充定义 `EditRef`、`StitchRef`、`CompactionRef`；并在 [`structure_test.go:55-115`](file:///Users/stanford/code/vmr/internal/story/structure_test.go#L55-L115) 锁定图事实内联 |
| **Gemini §1.3 / Pi T1** | `cmd_story.go`（`writeJourneyFile`）遗漏 Structure 字段 | 🟠 Med-High | ✅ **彻底解决** | 在 [`metrics.go:446`](file:///Users/stanford/code/vmr/internal/story/metrics.go#L446) 抽象出单一构造函数 `NewJourneySummary`；在 [`cmd_story.go:736`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story.go#L736) 接入；并在 [`cmd_story_test.go:154-164`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story_test.go#L154-L164) 增加产物级非空断言 |
| **Pi T3 (独有发现)** | `ToolCallRef.Result` 内联违反架构文档边界 | 🔴 High | ✅ **彻底解决** | 采纳收敛为引用的方案 (b)，在 [`structure.go:97-104`](file:///Users/stanford/code/vmr/internal/story/structure.go#L97-L104) 移除 `Result`/`ResultTruncated`，仅保留 `Matched`/`ResultError`；在 [`structure_test.go:117-164`](file:///Users/stanford/code/vmr/internal/story/structure_test.go#L117-L164) 用专有测试锁定结果正文落在下一步 `NewEvents` 中 |
| **Gemini §1.4 / Pi T2** | 补充内联单步耗时与用量（`Usage`/`DurMS`/`TTFTMS`/`Endpoint`） | 🟡 Med (High ROI) | ✅ **彻底解决** | 在 [`structure.go:180-184`](file:///Users/stanford/code/vmr/internal/story/structure.go#L180-L184) 内联上述字段，并在 [`structure_test.go:104-114`](file:///Users/stanford/code/vmr/internal/story/structure_test.go#L104-L114) 断言校验 |
| **Gemini §2.1 / Pi L3** | `structureExcerptChars` 常量解耦与实测分布留档 | 🟢 Low | ✅ **合理处置** | 保持与 `initialInstructionExcerptChars` 一致（2000 字符），但在 [`structure.go:39-44`](file:///Users/stanford/code/vmr/internal/story/structure.go#L39-L44) 详细记录了真实语料实测分布（6/33 args 截断，0/22 resp 截断） |
| **Gemini §2.2 / Pi L4** | `EventRef.FirstStepSeq` 冗余字段反范式理由说明 | 🟢 Low | ✅ **合理处置** | 在 [`structure.go:65-75`](file:///Users/stanford/code/vmr/internal/story/structure.go#L65-L75) 注释中明确阐述该字段专为展平全局事件流免回溯设计 |
| **Gemini §2.3 / Pi §5** | 体积守卫测试断言门限收紧 | 🟢 Low | ✅ **合理处置** | 在移除 Result 字段后，在 [`structure_test.go:344`](file:///Users/stanford/code/vmr/internal/story/structure_test.go#L344) 将门限收紧为 `4 * structureExcerptChars * 2`（覆盖 JSON 转义开销） |
| **Gemini §2.4 / Pi §5** | `byID` 多工具调用重名防御 | 🟢 Low | ✅ **合理处置** | 在 [`structure.go:88-91`](file:///Users/stanford/code/vmr/internal/story/structure.go#L88-L91) 注释中明确指出此处强依赖 `findings_toolresult.go` 的规范性保证 |
| **Pi M1** | `EventRef.Hash` 计算契约明确化 | 🟡 Med | ✅ **彻底解决** | 在 [`structure.go:50-56`](file:///Users/stanford/code/vmr/internal/story/structure.go#L50-L56) 明确写明哈希口径为原始 JSON 规范化后 md5，指引消费者使用 `ctxgraph.BuildManifest` 匹配 |
| **Pi M2** | 文档同步缺漏（UserGuide、Analytics 设计文档） | 🟡 Med | ✅ **彻底解决** | 已在 [`docs/UserGuide.md:485`](file:///Users/stanford/code/vmr/docs/UserGuide.md#L485)、[`docs/UserGuide.zh.md:478`](file:///Users/stanford/code/vmr/docs/UserGuide.zh.md#L478)、[`docs/VirtualModelRouter_Design_v4_Analytics.md:257`](file:///Users/stanford/code/vmr/docs/VirtualModelRouter_Design_v4_Analytics.md#L257)、[`CHANGELOG.md:21`](file:///Users/stanford/code/vmr/CHANGELOG.md#L21) 同步补齐 `structure` 描述 |
| **Pi M3** | `structure` 字段缺乏 schema 版本戳 | 🟡 Med | 📋 **登记待办** | 在 [`docs/KNOWN_ISSUES_sonnet-5.md:263`](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md#L263) 登记为 `§1.29`，因非跨运行复用缓存，排期在 P5/P6 统一处理 |
| **Pi M4** | 产物体积验收判据实测锚点 | 🟡 Med | ✅ **已记录** | 在 ActionPlan §8.3 完整记录各阶段演进数字（最终版 92,160 字节）并在结构体注释中说明 |
| **Pi M5.1** | `-llm-addr ''` 无法覆盖配置导致实跑真实 LLM | 🟡 Med | 📋 **登记待办** | 在 [`docs/KNOWN_ISSUES_sonnet-5.md:246`](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md#L246) 登记为 `§1.28`，作为全局通用 flag 优先级问题在后续阶段统一治理 |
| **Pi M5.2** | P1 证据包 69,936 基线未绑定 Journey 对 | 🟢 Low | ✅ **已核实** | P4.3 在 [`llm_packs_test.go:198-251`](file:///Users/stanford/code/vmr/internal/story/llm_packs_test.go#L198-L251) 中直接采用参数膨胀 4 个数量级的直接对比测试，消除了对历史基线 Journey 对的脆弱依赖 |
| **Pi E1/E2/E3** | ActionPlan 中三处行文/出处错误 | 🟢 Low | ✅ **已修正** | 在 ActionPlan §0 原文已纠正文件位置与引用出处 |

---

## 2. 核心改进项深度技术核实

### 2.1 无损重建检验的哈希匹配闭环（Gemini §1.1 + Pi T4 处置核实）

* **核查事实**：
  在 [`internal/story/structure_test.go:166-312`](file:///Users/stanford/code/vmr/internal/story/structure_test.go#L166-L312) 的 `TestBuildStructure_LosslessReconstruction` 中，测试执行流程如下：
  1. 通过合成包含 3 条记录的语料，其中 Step 3 故意重发与 Step 1 完全相同的用户消息 `u1`。
  2. 验证 Step 3 的 `NewEvents` 长度确实被全局去重为 1（仅包含新增的 `a2`，而不是 `msgs[DeltaStart:]` 切出的 2 条）。
  3. 通过 `audit.LineAt(path, line)` 仅凭 `ss.Req` 坐标从磁盘读取原始记录，使用 `ctxgraph.BuildManifest(&rec, path, line)` 重新生成 `Keys` 与 `MsgIdx`。
  4. 构建 `byHash[ctxgraph.Hash]chatmsg.Message` 映射表，对 `ss.NewEvents` 中的每个 `EventRef.Hash` 进行严格内容查找与角色断言。
* **结论**：完全脱离了任何内存态 `*Step` 对象，真实证明了仅凭 `structure` JSON + 审计日志即可无损恢复事实层内容。

### 2.2 图层级分析事实完整内联（Gemini §1.2 + Pi T2 处置核实）

* **核查事实**：
  在 [`internal/story/structure.go`](file:///Users/stanford/code/vmr/internal/story/structure.go) 中：
  ```go
  // structure.go:186-189
  Edit       *EditRef       `json:"edit,omitempty"`
  StitchEdge *StitchRef     `json:"stitch_edge,omitempty"`
  SysChanged bool           `json:"sys_changed,omitempty"`
  Compaction *CompactionRef `json:"compaction,omitempty"`
  ```
  在真实语料实测（`j-openclaw-20260728T000544-20260728T001259-8b175da9`）输出中：
  - Step 1：`edit` 为 null（首步无前驱）。
  - Step 2（缝合边界）：
    - `stitch_edge`: `{"kind": "head_prune", "score": 0.3333333333333333, "confidence": 0.5}`
    - `compaction`: `{"tokens_before": 28958, "tokens_after": 29676, "survived_entities": [...]}`
  - 其余 20 步：均携带精准的 `edit` 分类（如 `append`、`replace-tail` 等）。
* **结论**：彻底消除了 P5.1 移除 Markdown 事实层后图分析事实永久灭失的架构隐患。

### 2.3 决策文本与对话历史的内联/引用边界（Pi T3 处置核实）

* **核查事实**：
  - `ToolCallRef` 仅保留 `id`, `name`, `args`, `args_truncated`, `matched`, `result_error`，彻底剔除了 `result` 与 `result_truncated` 文本。
  - 在真实语料生成的 92.9KB JSON 中，`result` 字段出现次数为 0。
  - 工具返回结果通过下一步的 `NewEvents`（`role: "tool"`）以哈希引用方式自然承载。
* **结论**：严格捍卫了架构文档 §7.4(b) "本轮决策内联、对话历史引用" 的 Blob/Tree 分离原则。

---

## 3. 本次核查新发现与潜在改进项

在对全部改动文件与生成产物进行深度代码走查与实证运行后，发现以下 2 处可进一步关注的细节：

### 3.1 【模式差异 / Schema 风格微小不一致】`Usage` 字段序列化为 PascalCase 子对象

* **现象描述**：
  在 [`internal/story/structure.go:183`](file:///Users/stanford/code/vmr/internal/story/structure.go#L183) 中，定义为：
  ```go
  Usage   chatmsg.Usage `json:"usage"`
  UsageOK bool          `json:"usage_ok,omitempty"`
  ```
  而在 [`internal/chatmsg/usage.go:21`](file:///Users/stanford/code/vmr/internal/chatmsg/usage.go#L21) 中，`Usage` 结构体的字段无 json tag：
  ```go
  type Usage struct {
      In, Out               int64
      CacheRead, CacheWrite int64
      Reasoning             int64
  }
  ```
  这导致输出的 `journey-<id>.json` 中，整体为严格的 `snake_case`（如 `dur_ms`, `ttft_ms`, `stitch_edge`, `delta_start`），但 `usage` 内部的字段为 `PascalCase`：
  ```json
  "usage": {
    "In": 28958,
    "Out": 416,
    "CacheRead": 5248,
    "CacheWrite": 0,
    "Reasoning": 249
  }
  ```
* **分析与影响**：
  - `ctxgraph.Manifest`（`.parse-cache/` 缓存）同样使用了 `Usage chatmsg.Usage`，且在内部流转中正常工作。
  - 对于面向外部机器消费者的 `journey-<id>.json` 产物，这种风格混合虽不影响解析正确性，但存在细微的契约风格不一致。
* **处置建议**：
  无需在 P4 紧急修改（避免破坏已通过的测试与与 Manifest 的一致性）。建议在 P5/P6 统一梳理 `journey-<id>.json` 的公开 Schema 与版本戳（`KNOWN_ISSUES §1.29`）时一并规范化。

---

### 3.2 【防御性编程】`BuildStructure` 入参 `j *Journey` 缺乏 nil 判空

* **现象描述**：
  在 [`internal/story/structure.go:224`](file:///Users/stanford/code/vmr/internal/story/structure.go#L224) 中：
  ```go
  func BuildStructure(j *Journey) JourneyStructure {
      steps := journeySteps(j) // 若 j == nil，此处 j.Tasks 将触发 panic
      ...
  }
  ```
  对比 [`internal/story/metrics.go:121`](file:///Users/stanford/code/vmr/internal/story/metrics.go#L121) 的 `ComputeMetrics`，其头部包含 `if j == nil { return Metrics{} }` 防御。
* **分析与影响**：
  生产路径上 `NewJourneySummary` 传入的 `j` 由上游保证非 nil；但若单元测试或外部调用传入 nil，会直接 panic。
* **处置建议**：
  可在后续代码清理时在 `BuildStructure` 开头补充一行 `if j == nil { return JourneyStructure{Tasks: []TaskStructure{}} }`，符合全仓一致的防御规范。

---

## 4. 阶段验收结论

对照 `docs/future-strategy/story_report_dev_plan_opus-5.md` P4 阶段的验收标准：

1. **无损重建检验**：✅ `TestBuildStructure_LosslessReconstruction` 经哈希匹配闭环重构，已完全验证。
2. **体积随步数有界检验**：✅ `TestBuildStructure_VolumeBoundedByStepsNotProseLength` 常驻测试与真实语料 92.9KB 实测双重锁定。
3. **证据包隔离性**：✅ `TestBuildEvidencePack_SizeBoundedRegardlessOfStructureRichness` 与单 Journey 版本均已建立防御测试。
4. **全套自动化测试**：✅ `go test ./...`、`go test ./internal/archtest/...`、`go vet ./...`、`gofmt -l .` 全绿。
5. **配套文档与设计同步**：✅ `CHANGELOG.md`、`KNOWN_ISSUES_sonnet-5.md`、`UserGuide`（中英文）、`Analytics.md`、`architecture`、`dev_plan` 均已同步。

**结论：当前工作区中未 commit 的全部修改完全符合 P4 Action Plan 及其执行记录的描述，已知设计盲区均已妥善处置，代码质量扎实，无阻断性缺陷，可以安全进入后续人工 Review 与 Commit 阶段。**
