// Ver 2026-08-20 23:10, by Gemini 3.7 Flash

# vmr 日志分析体系重构 — P8 执行计划事实核查与架构评审报告

<statement>
**重要声明：针对本文档所描述问题开展核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**
</statement>

本文是对 [`docs/future-strategy/story_report_p8_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p8_action_plan_sonnet-5.md)（下称 **P8 ActionPlan**）的完整事实核查、设计合理性研判与执行风险评估报告。

对照基准：
1. 架构方案 [`docs/future-strategy/story_report_architecture_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md)
2. 第二期开发计划 [`docs/future-strategy/story_report_dev_plan_2_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_dev_plan_2_sonnet-5.md)（P8 阶段目标与验收标准）
3. 语言策略方案 [`docs/future-strategy/json_lang_policy_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/json_lang_policy_plan_sonnet-5.md)（已确定的语言跟随原则与模块大纲）
4. 真实仓库源码基线（commit [`2884d51`](file:///Users/stanford/code/vmr) 及当前工作区状态）

---

## 0. 评审总结与核心裁决

**总体判定**：P8 ActionPlan 的核心目标（实现 JSON 产物叙述文本跟随 `-lang`，同时保持 `FindingCode`/`MetricCode`/`EvidenceAnchor` 语言无关与机器锚点不变）完全符合语言策略文档与第二期 DevPlan 的架构方向。但执行计划正文中存在 **1 处硬性编译阻塞遗漏**、**1 处状态突变导致的架构脆弱性隐患**、**3 处核心契约源码注释遗漏更新** 以及 **1 处文件行数预算事实混淆**。

下表按**严重程度 + ROI** 分级汇总本次评审发现的所有问题：

| 梯队 | 序号 | 类别 | 问题摘要 | 影响与处置价值 | 处理建议 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **第一梯队**<br>(严重缺陷 / 高 ROI 改进) | **1.1** | 事实遗漏 / 编译阻塞 | 遗漏 [`internal/story/llm_packs_test.go:205`](file:///Users/stanford/code/vmr/internal/story/llm_packs_test.go#L205) 的 `Compare` 调用点 | **高**（按计划执行将直接导致 `go test ./internal/story/...` 编译失败） | 在 §2.3 步骤 6 中明确补全该调用点并传参 `i18n.EN` |
| | **1.2** | 架构设计 / 状态突变 | `report` 侧 Path (b) 采用原地覆写 `rep.Efficiency`，存在调用时序耦合与重复计算 | **高**（任何未来非 `cmd_report.go` 调用点若漏调 `LocalizeEfficiency` 将静默产出英文 JSON） | 保持 `Build` 签名不变以节约测试迁移成本，但将 `LocalizeEfficiency` 封装进序列化路径或在入口做强制约束 |
| | **1.3** | 契约注释 / 高 ROI | 遗漏更新 [`rows.go:463`](file:///Users/stanford/code/vmr/internal/report/rows.go#L463)、[`section_efficiency.go:16`](file:///Users/stanford/code/vmr/internal/report/section_efficiency.go#L16)、[`report_efficiency.go:5`](file:///Users/stanford/code/vmr/internal/i18n/report_efficiency.go#L5) 中陈旧的“JSON 恒英文”契约注释 | **高**（消除核心定义与实现相悖的暗知识，防止未来维护被误导） | 将这 3 处源码注释纳入 P8.3 文档回填任务清单 |
| | **1.4** | 测试防护 / 高 ROI | 端到端多语言一致性测试（P8.4）需补充 `report.yaml` 配置级联与命令行覆盖场景 | **高**（防止 `report.yaml` 中的 `language: zh` 在某些 JSON 路径下未正确生效） | 在 `TestE2E_LangZh_AllThreeJSONOutputsAgree` 中增加配置文件级联验证 |
| **第二梯队**<br>(中低风险 / 事实偏差) | **2.1** | 事实偏差 | 混淆 [`internal/report/metrics.go`](file:///Users/stanford/code/vmr/internal/report/metrics.go) 与 [`internal/story/metrics.go`](file:///Users/stanford/code/vmr/internal/story/metrics.go) 的行数预算豁免 | **低**（`report/metrics.go` 实际未在豁免表，受 700 行全局限制，当前 444 行余量充足，不影响编译） | 修正 ActionPlan §3.4 步骤 3 的行数事实描述 |
| | **2.2** | 规则定量 | `compare.go` 拆分后行数豁免需精准重新登记 | **低**（拆分后约 715 行，略微超过 700 默认限制，需在 `file_sizes_test.go` 登记合适预算） | 在 `file_sizes_test.go` 中将 850 调整为 760 行（约 6% 余量） |
| **已确认无误**<br>(合理设计 / 事实属实) | **3.1** | 架构优化 | `compare_metrics.go` 独立拆分纯度高，无多余 import 依赖 | — | 予以认可，保持方案 |
| | **3.2** | 契约清理 | `metricSpec.Label` 字段全仓无读取，删除死字段 | — | 予以认可，保持方案 |
| | **3.3** | 性能与代码清理 | `render_compare.go` 消除重复查表，直接复用 `r.Label` | — | 予以认可，保持方案 |
| | **3.4** | 文案修正 | `story_compare.go` 指标计数 12 改为 14 属实 | — | 予以认可，保持方案 |
| | **3.5** | 范围排除 | `vmr-story-corpus.json` 与 `vmr-requests.json` 经核查本身语言无关，无需改动 | — | 予以认可，保持范围排除 |

---

## 1. 第一梯队：高严重度缺陷与高 ROI 改进项（详细分析）

### 1.1 【硬性编译阻塞】遗漏 `internal/story/llm_packs_test.go:205` 的 `Compare` 调用点

- **事实依据**：
  在 [`internal/story/llm_packs_test.go:203-207`](file:///Users/stanford/code/vmr/internal/story/llm_packs_test.go#L203-L207) 中存在以下代码：
  ```go
  packFor := func(j *Journey) EvidencePack {
      s := Summarize(j, i18n.EN) // computes Structure — the thing under test must not leak through
      cmp := Compare(s, s)
      return BuildEvidencePack(j, j, cmp, i18n.EN)
  }
  ```
  而在 P8 ActionPlan §2.3 步骤 6 中，列出的测试调用点仅包含：
  - `internal/story/compare_test.go`（8 处）
  - `internal/story/llm_test.go`（2 处）
  完全遗漏了 `llm_packs_test.go:205`。
- **严重性与影响**：
  若执行人员严格遵循 ActionPlan 逐项修改，运行 `go test ./internal/story/...` 时将立刻遇到编译错误：`not enough arguments in call to story.Compare`。
- **处置方案**：
  在 P8 ActionPlan §2.3 步骤 6 中增加该条目：
  - `internal/story/llm_packs_test.go:205`：`cmp := Compare(s, s)` 改为 `cmp := Compare(s, s, i18n.EN)`。
  - 同时在步骤 7 强调以全包编译检查 `go test ./internal/story/...` 为最终收敛依据。

---

### 1.2 【架构设计与状态突变隐患】`report` 侧 Path (b) 的原地状态突变（In-Place Mutation）隐患与封装加固

- **事实与现状分析**：
  P8 ActionPlan §3.2 在 (a)（给 `Build`/`BuildCached` 加 `lang` 参数）与 (b)（保持 `Build` 语言无关，调用点原地重算 `rep.Efficiency`）之间选择了 (b)。
  
  **选择 (b) 的正面价值**：`internal/report` 包内部测试数十处直接调用 `Build(...)`，不改动 `Build` 签名避免了大量机械测试代码变动，符合实用主义和 YAGNI 原则。
  
  **潜在设计隐患与认知成本**：
  1. **状态突变与时序耦合（Temporal Coupling）**：`Report2` 结构体由 `Build()` 构造后，其 `Efficiency` 默认填入英文。调用方必须显式记得调用 `report.LocalizeEfficiency(rep, lang)` 才能将其变为目标语言。
  2. **双轨计算（Dual Paths）**：
     - 在 `cmd_report.go` 中：`Build` 算了一遍 EN，`LocalizeEfficiency` 算了一遍 `lang`，然后写出 `vmr-report.json`。
     - 在 `Markdown()` 渲染路径中：[`section_efficiency.go:27`](file:///Users/stanford/code/vmr/internal/report/section_efficiency.go#L27) 的 `renderEfficiency` 又独立调用了 `buildFindings(rep, lang)` 算了一遍 `lang`。
     即同一次运行中，`buildFindings` 被调用了 3 次（1 次 EN + 2 次目标语言）。
  3. **调用泄漏风险**：如果未来在其他入口（例如脚本、测试辅助函数、或者 P9 的 `cmd_analyze.go` 重构中）直接调用了 `report.WriteJSON(rep, path)` 而遗漏了 `report.LocalizeEfficiency`，将静默导致 `vmr-report.json` 保持英文，而同目录下的 `vmr-report.md` 变成中文，重新引入语言不一致的暗坑。
- **高 ROI 改进建议**：
  - **建议方案**：保持 `Build` 签名不变，但在 `internal/report` 导出安全的序列化入口，或将 `LocalizeEfficiency` 约束为序列化前置逻辑。例如：
    将 `report.WriteJSON(rep *Report2, path string)` 升级为 `report.WriteJSON(rep *Report2, lang i18n.Lang, path string)`，或者在 `report.WriteJSON` 内部若发现需要本地化则就地执行，杜绝未本地化直接落盘的可能。
  - **最低成本防线**：若保持 `LocalizeEfficiency(rep, lang)` + `WriteJSON(rep, path)` 分步调用不变，必须在 `WriteJSON` 的文档注释以及 `cmd_analyze.go`、`cmd_report.go` 的装配点显式注明该强约束，并在 `i18n_e2e_test.go` 中通过真实命令行测试锁定该行为。

---

### 1.3 【契约文档一致性 - 高 ROI】多处核心 Go 源码 doc comment 遗漏更新

- **事实依据**：
  P8 ActionPlan §4.2 详尽列出了设计文档、用户指南、`KNOWN_ISSUES`、`CHANGELOG` 的修改计划，但未检索并覆盖以下 **3 处定义核心数据契约的关键源码注释**：
  1. [`internal/report/rows.go:463-468`](file:///Users/stanford/code/vmr/internal/report/rows.go#L463-L468)（`Finding` 结构体核心定义）：
     ```go
     // Finding/Value/Implicated/Action are narrative text. They are always
     // English in this persisted struct, regardless of report language —
     // buildFindings is always called with i18n.EN to populate Report2.
     // A localized copy for Markdown rendering is produced separately...
     ```
     （此处直接宣称 `Report2` 中永远固定为英文，P8 落地后该断言直接失效）。
  2. [`internal/report/section_efficiency.go:16-23`](file:///Users/stanford/code/vmr/internal/report/section_efficiency.go#L16-L23)：
     ```go
     // This section does NOT render rep.Efficiency directly — that field is
     // always English (report.Build populates it via buildFindingsForJSON, fixed
     // to i18n.EN, so vmr-report.json's efficiency[] never varies by language...
     ```
  3. [`internal/i18n/report_efficiency.go:5-10`](file:///Users/stanford/code/vmr/internal/i18n/report_efficiency.go#L5-L10)：
     ```go
     // See docs/VirtualModelRouter_Design_v4_Analytics.md's "JSON 契约" subsection: the
     // six Finding* closures here are called twice by report.buildFindings — once
     // with EN (feeding the always-English Report2.Efficiency JSON field) and...
     ```
- **处置价值**：
  Go 源码注释是开发者理解数据结构的首要窗口。核心契约字段上的过时注释会制造严重的认知分裂。清理成本极低（几分钟的注释修改），长期消除误导的 ROI 极高。
- **处置方案**：
  在 P8 ActionPlan §4.2 步骤中显式增列上述 3 处文件的注释改写任务。

---

### 1.4 【质量守卫高 ROI】端到端跨产物语言一致性校验（P8.4）边界用例强化

- **分析与评估**：
  P8 ActionPlan §5.2 规划了新增端到端一致性测试 `TestE2E_LangZh_AllThreeJSONOutputsAgree`，对三种产物（`vmr-report.json`、`journey-<id>.json`、`compare-*.json`）在 `-lang zh` 运行时同时断言中文。这一规划切中了策略文档指出的“各自测试通过但全局无人核对”的痛点。
- **高 ROI 优化点**：
  实际用户场景中，语言设置不仅通过 `-lang zh` 传入，还会通过 `report.yaml` 的 `language: zh` 配置生效（在无显式 `-lang` flag 时自动级联）。
- **处置方案**：
  在新增的 E2E 测试中，除了显式 `-lang zh` 外，增加一组验证：
  - 构造 `report.yaml`（`language: zh`），无 `-lang` 参数运行命令，断言三种 JSON 输出同样全量为中文。
  - 构造 `report.yaml`（`language: zh`）+ 显式 `-lang en`，断言命令行 flag 胜出，三种 JSON 输出均为英文。
  确保语言解析链条（`resolveLanguage`）在所有 JSON 落盘路径上没有遗漏。

---

## 2. 第二梯队：事实偏差与中低风险规范项

### 2.1 事实核查错误：`internal/report/metrics.go` 行数预算与豁免名单混淆

- **事实核对**：
  - P8 ActionPlan §3.4 步骤 3 称：“`wc -l internal/report/metrics.go`——当前 453/470 行豁免，新增函数约 15 行，预计到 468/470，仍在预算内，但余量只剩 2 行...”。
  - 查阅 [`internal/archtest/file_sizes_test.go:60-70`](file:///Users/stanford/code/vmr/internal/archtest/file_sizes_test.go#L60-L70)：
    - 登记项为 `"internal/story/metrics.go": 470`（当前实际 453 行）。
    - [`internal/report/metrics.go`](file:///Users/stanford/code/vmr/internal/report/metrics.go) 当前实际行数为 **444 行**，且**并未在 `fileLineExemptions` 登记**，适用的是全局默认限制 `defaultFileLineLimit = 700` 行。
- **影响评估**：
  444 + 15 = 459 行，远低于 700 行的全局限制。该事实错误不会导致构建中断或测试报警，但属于 ActionPlan 编写过程中的包名混淆笔误。
- **处置**：修正 ActionPlan 中关于行数预算的描述，避免给执行人员制造虚假的行数焦虑。

---

### 2.2 `internal/story/compare.go` 拆分后行数豁免登记与余量核算

- **事实核对**：
  - [`internal/story/compare.go`](file:///Users/stanford/code/vmr/internal/story/compare.go) 原始行数为 847 行（豁免预算为 850 行）。
  - 将 metric-diff 相关逻辑（132 行）移至新文件 [`internal/story/compare_metrics.go`](file:///Users/stanford/code/vmr/internal/story/compare_metrics.go) 后，`compare.go` 实际剩余约 714-718 行。
  - 新建的 `compare_metrics.go` 约 138 行，远低于 700 行限制。
- **影响评估**：
  由于 718 行依然超过 700 行的全局默认阈值，因此必须更新 `internal/archtest/file_sizes_test.go` 中的豁免条目。
- **处置**：
  在 `file_sizes_test.go` 中，将 `"internal/story/compare.go": 850` 下调为 **760 行**（保留 ~6% 缓冲），既严格控制代码膨胀，又符合项目的行数预算管理纪律。

---

### 2.3 `CHANGELOG.md` 变更记录规范

- **核查与建议**：
  ActionPlan 将 P8 的改动登记在 `[Unreleased]/### Changed` 分区，记录为“`-lang zh` 下 `vmr-report.json`/`compare-*.json` 的叙述字段从英文变为中文”。
  经核查，由于 `FindingCode`/`MetricCode` 等枚举标识符保持英文不变，机器消费方无需破坏性变更；但在人类阅读和日志比对层确实发生变化，分类为 `Changed` 符合 Keep a Changelog 规范。

---

## 3. 事实核查通过且经论证合理的关键设计点

以下 ActionPlan 中的设计点与实现考量经深度核实完全正确，应予以保留：

1. **`compare_metrics.go` 文件拆分的设计纯度**：
   拆分抽离的 132 行代码仅包含基础标量类型与比较规则，不引用任何重型外部包（无 `chatmsg`、`core`、`taskseg` 依赖），拆分干净且彻底解决了 `compare.go` 逼近行数红线的问题。
2. **`metricSpec.Label` 字段的果断删除**：
   全仓 grep 确认除了 `Compare()` 内部使用外，没有任何业务代码读取 `spec.Label`（`corpus.go` 等均仅读取 `.Code`/`.Kind`/`.Value`）。删除该字段并统一调用 `i18n.MetricLabel`，从根本上消除了“结构体字面量”与“i18n 查表”两份标签漂移的风险。
3. **`render_compare.go` 消除重复查表**：
   `RenderComparisonMarkdown` 改为直接消费 `cmp.Rows[].Label`，不再进行二次 `i18n.MetricLabel` 查表，保持了 Markdown 渲染与 JSON 产物源自同一份计算结果的严谨性。
4. **排除无关产物（`vmr-story-corpus.json` / `vmr-requests.json`）的合理性**：
   经核查，`vmr-story-corpus.json`（指标分布、相关性矩阵）与 `vmr-requests.json`（请求索引）中的字段均为数字、日期、坐标或英文枚举 Code，不存在本地化模板文本，不纳入 P8 改动范围的决策完全正确。

---

## 4. 执行计划（ActionPlan）具体修订操作指引

在正式执行 P8 之前，建议对 [`story_report_p8_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p8_action_plan_sonnet-5.md) 补充以下修正：

### 补丁 1：§2.3 步骤 6 补充遗漏的测试调用点
```markdown
- `internal/story/llm_packs_test.go`：第 205 行，`cmp := Compare(s, s)` 改为 `cmp := Compare(s, s, i18n.EN)`。
```

### 补丁 2：§3.4 步骤 3 修正行数事实描述
```markdown
3. `wc -l internal/report/metrics.go`——当前 444 行（未在豁免表，适用 700 行全局限制），
   新增 LocalizeEfficiency 约 15 行，预计到 459/700，余量充沛。
```

### 补丁 3：§4.2 补充 3 处核心源码注释清理清单
```markdown
8. `internal/report/rows.go:463-468`：改写 `Finding` 结构体注释，删除 "always English in this persisted struct" 等陈旧表述。
9. `internal/report/section_efficiency.go:16-23`：更新顶部关于 `rep.Efficiency` 语言行为的注释。
10. `internal/i18n/report_efficiency.go:5-10`：更新顶部包注释关于双重调用与 JSON 语言绑定的说明。
```

### 补丁 4：§5.2 步骤 2 强化端到端测试用例
```markdown
- 增加一组针对 `report.yaml`（`language: zh`，无命令行 flag）的用例，确保三种 JSON 产物自动级联生效为中文。
```

---

## 5. 验收标准最终核对表（供 P8 落地对照）

- [ ] `internal/story/compare_metrics.go` 成功新建且行数在 700 行以内；`compare.go` 行数降至 760 行以内并更新 `file_sizes_test.go`。
- [ ] `internal/story/llm_packs_test.go:205` 及 `compare_test.go`、`llm_test.go` 全量调用点补齐 `lang` 实参，编译全绿。
- [ ] `internal/report/rows.go`、`section_efficiency.go`、`report_efficiency.go` 中的过时“JSON 恒英文”注释全部清理。
- [ ] `vmr report -lang zh` 产出的 `vmr-report.json` 中 `efficiency[].finding` 等叙述字段输出为中文。
- [ ] `vmr story -compare ... -lang zh` 产出的 `compare-*.json` 中 `rows[].label` 输出为中文。
- [ ] `TestE2E_LangZh_AllThreeJSONOutputsAgree` 新增测试通过，同时覆盖 `-lang` 参数与 `report.yaml` 级联路径。
- [ ] `go test ./... -race` 全绿；`go test ./internal/archtest/...` 全绿。
- [ ] 真实日志抽检核对：`zh` 目录下三种 JSON 叙述字段均为中文且与对应 `.md` 一致；`en` 目录下均为英文。
