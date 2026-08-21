<!-- Ver 2026-08-21 11:45, by Gemini 3.7 Flash -->

# vmr 日志分析体系重构 — P9 执行计划事实核查与架构评审报告

<statement>
**重要声明：针对本文档所描述问题开展核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**
</statement>

本文是对 [`docs/future-strategy/story_report_p9_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p9_action_plan_sonnet-5.md)（下称 **P9 ActionPlan**）的完整事实核查、设计合理性研判、执行风险评估，以及**对当前工作区 uncommitted 修改的端到端落地核查报告**。

对照基准：
1. 架构方案 [`docs/future-strategy/story_report_architecture_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md)（尤其是 §7.9 命令行目标模型与 §7.2 的 3×2 矩阵）
2. 第二期开发计划 [`docs/future-strategy/story_report_dev_plan_2_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_dev_plan_2_sonnet-5.md)（P9 阶段目标与验收标准）
3. 已知问题与架构取舍清单 [`docs/KNOWN_ISSUES_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md)（§1.30/§1.32/§1.33/§1.34 等相关条目）
4. 真实仓库源码基线与当前未提交代码（15 个 modified 文件 + 1 个 untracked 新增源文件）

---

## 0. 评审总结与核心裁决

**总体判定**：当前工作区中所有尚未 commit 的代码与文档修改，**完全符合 P9 ActionPlan 所描绘的范围、设计约束与验收标准**。改动精准收敛在 `cmd/vmr` 组合根与文档层，`git diff internal/report internal/story` 严格为空。针对初期评审中指出的全部 7 项问题（第一梯队 4 项、第二梯队 3 项），实现代码均给出了周密且高质量的处置；同时，在真实语料验证过程中就地发现并完美解决了 **1 处重大内存缺陷（追加分批渲染 `renderBatchSize = 20` 根治 35.5GB SIGKILL）** 与 **1 处参数联动缺陷（`resolveLLMOpts` 延迟校验修复纯 `-llm-key` 过滤）**。

全套自动化测试（包含新编写的 9 组端到端测试）以及架构守卫（`internal/archtest`）全部通过，全量 34 文件语料（11274 条记录）默认 `vmr analyze` 从基线 SIGKILL 转为正常退出（峰值内存 4.59GB），达到了直接提交（Ready to Commit）的标准。

---

## 1. 第一梯队：严重缺陷、设计隐患与高 ROI 改进项（详细核查）

### 1.1 【事实遗漏与架构守卫】`func_sizes_test.go` 豁免清理与行数预算控制

- **问题回溯**：初期评审指出，提炼出 `runReport` 和 `setupStoryRun` 后，`cmdReport`（降至 50 行）和 `cmdStory`（降至 45 行）已远低于 120 行限制，若不清理其 155/145 的旧豁免将留下过度余量；同时新函数 `runReport` 若超 120 行将导致测试失败。
- **实际处置核查**（源码位置：[`internal/archtest/func_sizes_test.go:54-66`](file:///Users/stanford/code/vmr/internal/archtest/func_sizes_test.go#L54-L66)）：
  ```go
  // cmdReport/cmdStory/cmdAnalyze themselves stay below the default limit
  // once P9.1 (CLI convergence) pulled their linear pipelines out into
  // runReport/setupStoryRun/dispatchAnalyze below...
  "cmd/vmr/cmd_report.go:runReport": 121,
  ```
  1. 旧的 `cmdReport: 155` 与 `cmdStory: 145` 豁免条目被彻底删除，消除了陈旧过度余量。
  2. `runReport` 实测 121 行，按实际行数精确登记 `"cmd/vmr/cmd_report.go:runReport": 121`。
  3. `cmdAnalyze` 在重构中被拆分为 `cmdAnalyze`（flag 解析与 resolve，106 行）与 `dispatchAnalyze`（setup 与分派路由，75 行），两者均在 120 行默认限制内，无需新增额外豁免。
- **核查结论**：**完美处置（100% 符合架构纪律）**。

---

### 1.2 【事实核查偏差】`CategoryCron` 索引展示状态的事实描述纠正

- **问题回溯**：初期评审指出，源码中 [`RenderStoryIndexMarkdown`](file:///Users/stanford/code/vmr/internal/story/storyindex.go#L250-L258) 仅对 `CategoryHeartbeat` 与 `CategorySubagent` 进行折叠，`CategoryCron` 与 `CategoryTask` 一样在主表中展开。ActionPlan 与 DevPlan 中“cron 折叠展示不变”的描述与代码相悖。
- **实际处置核查**：
  - ActionPlan §3.2 正文、§10.1 第 5 点已全面修正为：“`cron` 在 `vmr-stories.md` 中与 `task` 一样保持主表展开，但默认套件不物化其 `journey-*.md`（显示为未生成）；`heartbeat`/`subagent` 保持折叠展示且不物化”。
  - [`docs/UserGuide.md:547`](file:///Users/stanford/code/vmr/docs/UserGuide.md#L547)、[`docs/UserGuide.zh.md:547`](file:///Users/stanford/code/vmr/docs/UserGuide.zh.md#L547) 以及 [`docs/VirtualModelRouter_Design_v4_Analytics.md:522`](file:///Users/stanford/code/vmr/docs/VirtualModelRouter_Design_v4_Analytics.md#L522) 中均同步更新了这一精准事实描述。
- **核查结论**：**已彻底纠正，消除所有文档与实现之间的认知分歧**。

---

### 1.3 【用户认知与策略高 ROI】`vmr report` / `vmr story` 别名提示语的精准优化

- **问题回溯**：初期评审指出，`vmr analyze` 默认是套件模式（包含任务叙事物化），并无 `-report-only` 开关；若直接称“此为 vmr analyze 别名”，会误导用户以为两者行为完全相同。
- **实际处置核查**：
  - [`cmd/vmr/cmd_report.go:437`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_report.go#L437)：
    ```go
    fmt.Fprintln(os.Stderr, "vmr report: deprecated alias — `vmr analyze` now produces the full navigable suite (macro report + task journeys) from a single call; if you only want the macro report, `vmr report` remains fully supported. See `vmr analyze -h`.")
    ```
  - [`cmd/vmr/cmd_story.go:174`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story.go#L174)：
    ```go
    fmt.Fprintln(os.Stderr, "vmr story: deprecated alias — `vmr analyze` now produces the full navigable suite by default, or use -journey/-compare/-corpus for individual views; `vmr story` remains supported with its existing flags. See `vmr analyze -h`.")
    ```
- **核查结论**：**文案表述客观准确，既明确了新入口推荐，又诚实交代了功能差异与兼容性承诺**。

---

### 1.4 【质量守卫高 ROI】P9 专属自动化测试集落地与验证

- **问题回溯**：初期评审要求在 [`cmd/vmr/cmd_analyze_test.go`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze_test.go) 中补齐变焦选择器互斥、默认 Task 过滤、别名提示与变焦直通 4 类专属测试。
- **实际处置核查**：
  `cmd_analyze_test.go` 从 129 行扩充至 444 行，完整新增了 9 组端到端单元测试：
  1. `TestCmdAnalyze_SelectorsAreMutuallyExclusive`（验证 `-journey` 与 `-corpus` 同传报错）
  2. `TestCmdAnalyze_RenderAllRejectsSelector`（验证选择器与 `-render-all` 同传报错）
  3. `TestCmdAnalyze_DefaultSuiteScopeIsTaskOnly`（验证默认套件仅物化 task 候选，`-render-all` 物化全部 task + heartbeat 候选）
  4. `TestCmdAnalyze_JourneySelectorRunsStoryHalfOnly`（验证 `-journey` 只跑 story 半区，不产出 `vmr-report.md`）
  5. `TestCmdAnalyze_CorpusSelectorRunsStoryHalfOnly`（验证 `-corpus` 仅产出语料统计，不跑报表半区）
  6. `TestCmdAnalyze_CompareSelectorRunsStoryHalfOnly`（验证 `-compare` 仅产出成对对比，不跑报表半区）
  7. `TestCmdAnalyze_LLMAddrRejectedInDefaultSuite`（验证批量/默认套件模式下拒绝 `-llm-addr`）
  8. `TestCmdAnalyze_LLMKeyExcludesSelfTrafficFromBothHalves`（验证纯 `-llm-key` 在无 `report.yaml` 时对两半区同时生效）
  9. `TestCmdReportCmdStory_PrintDeprecationHint`（验证两别名命令均输出规范的 stderr 迁移提示且执行成功）
- **核查结论**：**测试覆盖全面严密，`go test -race ./cmd/vmr/...` 耗时 26.4s 全绿通过**。

---

## 2. 第二梯队：中低风险与实现细节项核查

| 序号 | 评审建议项 | 实际源码位置 | 实际执行结果核查 | 判定 |
| :--- | :--- | :--- | :--- | :--- |
| **2.1** | `cmdAnalyze` 必须使用 `flagPassed(fs, ...)` 绑定自身 `fs` | [`cmd/vmr/cmd_analyze.go:125, 141, 156, 172`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze.go#L125) | `flagPassed(fs, "llm-addr")`、`flagPassed(fs, "include-partial")`、`flagPassed(fs, "details")` 均严谨绑定了 `fs`，完整保留了 `report.yaml` 配置级联能力。 | **完全达标** |
| **2.2** | `cmd_story.go` 提炼若超预算应拆至 `cmd_story_setup.go` | [`cmd/vmr/cmd_story_setup.go`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story_setup.go) | 实际提炼时 `cmd_story.go` 触达 855/850 行。开发团队严格按预案将 `storySetup` 与 `setupStoryRun` 拆分至新文件 `cmd_story_setup.go`（99 行），`cmd_story.go` 回落至 774 行，未调大 850 预算。 | **完全达标** |
| **2.3** | `README.md` / `README.zh.md` 补全 `vmr analyze` 示例 | [`README.md:104`](file:///Users/stanford/code/vmr/README.md#L104)、[`README.zh.md:104`](file:///Users/stanford/code/vmr/README.zh.md#L104) | 在“2. Run / 2. 运行”段落新增 `./vmr analyze -c config.yaml` 示例，并在对比表与延伸阅读中同步更新命令拼写（`grep -c 'vmr analyze'` 由 0 变为 2）。 | **完全达标** |

---

## 3. 实现期新发现的两处关键技术突破与缺陷修复

在对当前 uncommitted 代码与 `P9 ActionPlan §10` 执行记录的比对核查中，确认了开发团队在实现期就地捕获并妥善解决的 2 处关键技术问题：

### 3.1 【重大突破】默认范围收窄不足以根治 SIGKILL，追加分批渲染机制（`renderBatchSize = 20`）

- **真实问题**：
  原设计假设“默认仅渲染 `category == task`”即可彻底解决大语料 SIGKILL。但在 34 个文件（11274 条记录）的真实全量语料实测中发现：`task` 类候选虽只占数量的 49%（234/473），却占了 **83.5% 的请求量（9259/11086）**。
  由于 `story.BuildAll` 内部对整批候选做一次性 `ctxgraph.FetchRecords`，一次性将 9259 条请求体载入内存，导致峰值内存飙升至 **35.5GB**，进程仍被系统杀死。
- **修法核查**（源码位置：[`cmd/vmr/cmd_story.go:547-603`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_story.go#L547-L603)）：
  在 `cmd_story.go` 中引入 `const renderBatchSize = 20`，并在 `renderJourneys` 中以 20 个候选为单位分批调用 `story.BuildAll` 并落盘。每批完成后，上一批的 `Journey` 对象与请求记录即可被 Go GC 回收。
- **验证数据**：
  全量 34 文件语料默认 `vmr analyze` 峰值内存从 **35.5GB（SIGKILL）降至 4.59GB（RSS 4.26GB）**，413 秒正常退出（exit 0），成功物化 234 个 task journey 与 8343 个 detail 页面（3.44GB）。
  且该修法**完全封闭在 `cmd/vmr` 内部，未修改 `internal/story` 或 `internal/report` 任何一行代码**。

---

### 3.2 【缺陷修复】`resolveLLMOptions` 饥饿校验导致批量模式纯 `-llm-key` 报错修复

- **真实问题**：
  在 P9.5 统一 flag 集合中，若在 `cmdAnalyze` 顶层无条件执行 `resolveLLMOptions`，当用户仅为了自指流量排除而传入 `-llm-key`（未传 `-llm-addr`）运行默认套件或 `-corpus` 时，`resolveLLMOptions` 会因缺少 `-llm-addr` 而报错阻断执行。
- **修法核查**（源码位置：[`cmd/vmr/cmd_analyze.go:160-167, 204, 219`](file:///Users/stanford/code/vmr/cmd/vmr/cmd_analyze.go#L160-L167)）：
  将 LLM 选项的有效性校验重构为 `analyzeRun.resolveLLMOpts` 延迟闭包，仅在真正消费 LLM 的 `-journey`（单匹配）与 `-compare` 分支中按需触发校验。
- **验证**：新增 `TestCmdAnalyze_LLMKeyExcludesSelfTrafficFromBothHalves` 测试完全通过。

---

## 4. 文档与权威元数据同步核查

对工作区中所有修改的文档进行逐项交叉比对：

1. **执行顺序四处订正**（全量核查通过）：
   - [`docs/UserGuide.md:547`](file:///Users/stanford/code/vmr/docs/UserGuide.md#L547) & [`docs/UserGuide.zh.md:547`](file:///Users/stanford/code/vmr/docs/UserGuide.zh.md#L547)
   - [`docs/VirtualModelRouter_Design_v4_Analytics.md:539`](file:///Users/stanford/code/vmr/docs/VirtualModelRouter_Design_v4_Analytics.md#L539)（明文强调“顺序不是任意的：story 先跑、report 后跑”）
   - [`CHANGELOG.md:21`](file:///Users/stanford/code/vmr/CHANGELOG.md#L21)
2. **`KNOWN_ISSUES_sonnet-5.md` 清单闭环与 ROI 表重算**（全量核查通过）：
   - `§1.30`（大语料 SIGKILL）、`§1.32`（analyze 强制 render-all）、`§1.33`（文档执行顺序颠倒）、`§1.34`（自指流量输入不对称）已全部移入 `§3` 已闭环（新增第 31–34 项，含批处理修法详述）。
   - `§4` ROI 表重算后，待处理高 ROI 条目数正式降为 **0 条**。
3. **`CHANGELOG.md` 规范记录**（全量核查通过）：
   - `[Unreleased]` 下准确记录了 `vmr analyze` 单一入口收敛、`report`/`story` 别名弃用说明以及分批物化内存优化。
4. **`story_report_dev_plan_2_sonnet-5.md` 状态更新**（全量核查通过）：
   - P9 行已标记为 `✅ 已完成（2026-08-21）`，并附有详尽的实测落差说明与 ActionPlan §10 链接。

---

## 5. 验收标准最终核对表（全项达成）

- [x] `vmr analyze` 统一解析所有 flag 并实现变焦选择器互斥路由（`-journey`、`-compare`、`-corpus` 互斥，选择器与 `-render-all` 互斥）。
- [x] 默认 `vmr analyze` 仅为 `category == task` 候选物化，配合 `renderBatchSize = 20` 分批机制，全量语料彻底消除 SIGKILL（峰值 4.59GB，exit 0）。
- [x] `vmr report` 与 `vmr story` 成功降级为别名，输出精准的迁移提示，且执行结果与收敛前保持一致。
- [x] `internal/archtest/func_sizes_test.go` 清理陈旧的 `cmdReport`/`cmdStory` 豁免，并为 `runReport` 准确登记 121 行。
- [x] `docs/UserGuide.md`/`.zh`、`VirtualModelRouter_Design_v4_Analytics.md`、`CHANGELOG.md` 中 4 处执行顺序全部订正为“story 先、report 后”。
- [x] `README.md` 与 `README.zh.md` 补齐 `vmr analyze` 示例与相关描述。
- [x] `KNOWN_ISSUES_sonnet-5.md` 中 `§1.30`、`§1.32`、`§1.33`、`§1.34` 成功移入 `§3` 已闭环，`§4` ROI 表同步重算。
- [x] `cmd/vmr/cmd_analyze_test.go` 9 组新增测试用例全部通过；`go test ./...`、`go test -race ./cmd/vmr/...`、`go test ./internal/archtest/...`、`gofmt -l .`、`go vet ./...` 全绿。
- [x] `git diff internal/report internal/story` 严格为空，解耦边界完整无损。

---

## 6. 最终结论

当前工作区中的 uncommitted 修改**无明显疏漏、无事实错误、无退化风险**，对既往所有评审发现均给出了严谨的闭环处置，并在实现期完成了超越预期的内存健壮性加固。可以安全地将当前修改提交入库。
