# Verification Report — P 系列修复验收（main@554d8af）

> 验收人：只读 verification agent · 验收日期：2026-09-02 · 工作区：`/Volumes/SSD2T/code/vmr`
> 验收范围：P-01 / P-06 / P-07 / P-09 四项修复
> 依据：`PROJECT_REVIEW_REPORT_agent_20260902.md` §5.2；`docs/KNOWN_ISSUES.md`；`CHANGELOG.md`；四个修复 commit（`dadf63b` / `ed0ce67` / `36fc415` / `2a341bd`）以及后续文档 commit（`fff5053` / `554d8af`）

---

## 一、逐项验收表

### P-01 · `jsonscan.RewriteModel` 非法 JSON 转义修复

| 维度 | 结论 | 依据 |
| --- | --- | --- |
| 代码核验 | ✅ 通过 | `internal/jsonscan/rewrite.go:31-44`：`RewriteModel` 改用 `MarshalNoEscape(model)`；`strconv` import 已删除（`grep strconv internal/jsonscan/rewrite.go` 无命中）。`MarshalNoEscape` 自身在 `internal/jsonscan/jsonscan.go:33-41` 关闭 HTML 转义并用标准 `json.Encoder`，行为与同文件 `rewriteRolesInTopLevelArray` 已用的路径一致。失败模式下，Err 正确向上传播。 |
| 测试核验 | ✅ 通过（强） | 新增 `TestRewriteModel_ProducesValidJSON`（10 个控制字节 + 10 个 roundtrip 子用例，全部 `json.Unmarshal` 全文档通过）+ `TestRewriteModel_NoGoLiteralEscapes`（负向断言：输出不得含 `\x`）。负向断言精准钉住了 P-01 的实际症状（`out={"model":"\xba"}` 的 `\xNN` 序列），不是空转。 |
| 回归核验 | ✅ 通过 | 全部 `go test -race -count=1 ./...` 绿（含 `internal/adapter`、`internal/replay`、`internal/server` 三个直接/间接调用 `RewriteModel` 的包）。`FuzzRewriteModel` 全部 17 个 seed 通过，`FuzzSessionFingerprint` 17/17 通过。签名 `([]byte, error)` 不变，`adapter.BuildUpstreamRequest` 调用方零改动。 |
| **总判** | **通过** | 修复精确：换工具不改语义，删除一个误用的 import；测试既覆盖正向（合法 JSON）又覆盖负向（无 Go-literal 转义）。无副作用。 |

### P-06 · `report.finishSession` ContextGrowth 首轮无 usage 回退估算

| 维度 | 结论 | 依据 |
| --- | --- | --- |
| 代码核验 | ✅ 通过 | `internal/report/metrics.go:210-213` 与新增的 `contextGrowthIn` helper（`metrics.go:225-242`）。三段式：UsageOK → 用 `Usage.In`；否则 `manifest.EstIn`；否则 0。`manifest.EstIn` 由 `ctxgraph.Manifest` 在 `manifest.go:163` 调用 `chatmsg.EstimateDegradedTokens` 写入——与 `report` 半边 `degraded-cost` 路径的同一函数、同一口径，**满足 AGENTS.md「同一函数，两个 caller，no drift」原则**。 |
| 测试核验 | ✅ 通过 | 两个新测试：(1) `TestContextGrowthInFallback` 单测三向分支（usage 胜出 / 回退 manifest / 都无归 0）；(2) `TestContextGrowthFallsBackToEstimateEndToEnd` 跑完整 `Build` 链路——首轮响应**无 usage 字段**、第二轮 9000 实测 prompt_tokens，断言 `ContextGrowth > 1`（即不再是恒 0）。后者是该 bug 的「真症状级」端到端钉子。 |
| 回归核验 | ✅ 通过 | `internal/report/...` 全套绿；`go test -race -count=1 ./...` 整体绿。原有 `TestContextGrowthDoesNotCrossContractBreak` 仍通过——说明 `finishSession` 的另一条不变量（不能跨 contract break 比 growth）未被新逻辑破坏。 |
| **总判** | **通过** | 修复点极小、helper 单测 + 端到端双层覆盖。EstIn 与成本路径共享同一 `EstimateDegradedTokens`，口径一致性由源代码共同导入（而非注释）保证。 |

### P-07 · `vmr report` §0 summary 新增 interactive workload 占比

| 维度 | 结论 | 依据 |
| --- | --- | --- |
| 代码核验 | ✅ 通过 | `internal/report/render_doc.go:155-165`（`renderSummary` 内嵌）与 `summaryInteractiveShare` helper（`render_doc.go:190-205`）；`internal/i18n/report_doc.go:33-39`（字段声明）+ 102/182 行（EN/ZH 实现）。EN/ZH 文本均为完整句、含具体数字与百分比。`-1` 哨兵（empty Workloads）+ 显式 `o.Requests > 0` 守卫共同保证 0/0、N/0 等病态格式不会出现。 |
| 测试核验 | ✅ 通过（带一个小缺口） | `TestSummaryInteractiveShare` 锁 helper（40、-1、-1 三向）；`TestSummaryRendersInteractiveNote` 走 `renderSummary` 验证 EN 输出含 `interactive workload accounts for 80.0%` 与 `(40/50)`，且空 Workloads 场景**不**出 note（含 ZH 反向断言）。**缺口**：未对 ZH 模板做正向"含数字"断言——只断言了"空 Workloads 不出"。i18n 字符串本身的字面值由 EN 断言隐式支撑，但严格说 ZH 字符串拼写错误不会红。这是「应有但非必须」覆盖。 |
| 回归核验 | ✅ 通过 | `internal/report` 全套绿；整仓绿。`renderSummary` 调用点未变（`vmr-report.md` 仍由同一条流水线写出）。新增的一行紧跟表格、`SummaryStarNote` 之前，与既有 `Highlights` 段不冲突。 |
| **总判** | **有条件通过** | 功能与文档同步到位；测试覆盖核心属性。唯一可记录项是 ZH 模板缺乏正向数字断言，**不构成验收否决**（EN 断言 + 字段声明类型签名 + helper 单元覆盖已经守住了行为面）。 |

### P-09 · `story.detectLLMGoalDrift` 拒绝 DriftStepSeq=1 锚点

| 维度 | 结论 | 依据 |
| --- | --- | --- |
| 代码核验 | ✅ 通过 | `internal/story/llm_findings.go:272`：`item.DriftStepSeq > 0` → `item.DriftStepSeq > 1`。附 13 行注释（`lines 264-274`）说明 `extractRootUserIntent` 返回首条 user 消息，因此 Step 1 在构造上就是根意图，`DriftStepSeq:1` 是范畴错误；同时 `buildGoalDriftPack` 的首 checkpoint 也是 Step 1，护栏同时锁住「首 checkpoint ≠ 漂移锚点」。 |
| 测试核验 | ✅ 通过 | 新增 `TestP1b3_GoalDrift_AnchoredAtStep1IsRejected`（`llm_findings_test.go:255-307`）：构造 9 步 journey，LLM mock 返回 `drift_step_seq: 1` + HIGH 置信度，断言 `len(findings) == 0`。**负向断言精准**——直接钉「静默拒绝」这条契约，没有假装通过。原 `TestP1b3_GoalDrift`（`drift_step_seq: 4`、合法路径）仍 PASS，说明合法漂移检测未受牵连。 |
| 回归核验 | ✅ 通过 | `internal/story` 全套绿；整仓绿。`buildGoalDriftPack` 未改；`extractRootUserIntent` 未改；`Finding` 数据结构未改。 |
| **总判** | **通过** | 一字之差（`> 0` → `> 1`）+ 充分注释 + 强负向测试。零副作用。 |

---

## 二、遗留问题清单（按严重性排序）

### L-1 [中·文档未更新] `PROJECT_REVIEW_REPORT_agent_20260902.md` §5.2 / §5.3 / §5.4 关于 P-01/P-06/P-07/P-09 的描述已过时

- **描述**：主报告写于 2026-09-02 之前，四处合入后没有回头改：
  - §5.2 路由半区 D5「P-01 [D5 中危·先修]」、分析·报表域 D3「P-06 / P-07」、分析·叙事域 D4「P-09」四处仍以「**问题 / 根因 / 建议 / ROI**」形式列出，措辞是「建议改为 / 建议加 / 建议在 …」，仿佛尚未修。
  - §5.2 行 117「`ContextGrowth` 首轮 Usage==0 时未回退 `EstInFresh`」与行 117「`goal_drift` 锚点可能定位 Step 1（§2.53，建议加 `DriftStepSeq > 首步` 后验护栏）」属同源 D 级汇总，也仍按「待做」写。
  - §5.3 gantt 图四行（`P-01/P-06/P-07/P-09` 2026-09, 1w）仍标为「未来工作」。
  - §5.4 审查结论（`404-405`）仍写「修复 P-01 并补充回归测试；将 … P-06/P-07/P-09 … 登记进 KNOWN_ISSUES / 按常规节奏落地」。
- **源码/文档锚点**：
  - `PROJECT_REVIEW_REPORT_agent_20260902.md:299-330`（P-01 / P-06 / P-07 / P-09 卡片）
  - 同文件 `:117`、`338`、`350-352`（汇总与具体条目）
  - `:375-378`（gantt）
  - `:391`、`404-405`（结论段）
- **影响**：
  1. 项目治理层（任何不读代码、只看报告的人）会被错误告知这四项还在 backlog；这正是 AGENTS.md「文档不能比代码落后」原则的反面教材。
  2. 行 117 把 P-06/P-09 挂在 §2.53 / §2.63 上，KNOWN_ISSUES 这两个条目已经按规范删除，主报告里留下的 § 编号成了悬空引用。
  3. 验收入库后未更新报告本身，对下一轮 reviewer 不友好。
- **建议**：由报告原作者（非本次只读验收 agent）按事实直接替换这四张卡片的「**建议**」段为「**已修** · commit `dadf63b` / `ed0ce67` / `36fc415` / `2a341bd`；验证见 `_review/VERIFICATION_REPORT_agent_20260902.md`」；gantt 把这四行挪到「已完成」段；§5.4 删「修复 P-01」那一条后续行动。

### L-2 [低·文档内部不一致] `docs/KNOWN_ISSUES.md:413` 优先级表范围声明已不再准确

- **描述**：第 413 行的元注释：

  > 另立一张表维护第二份口径，正是这份文档最反对的重复——而且原表只评了 22/30 条（§2.53–§2.56、§2.60–§2.63 从没进表）。

  §2.53（goal_drift）与 §2.63（§0 interactive）已于 `554d8af`「已修复即删除」原则删去，因此「§2.53–§2.56, §2.60–§2.63」这个范围里还**仍然存在**的只有 §2.55 / §2.56 / §2.60 / §2.61 / §2.62。文字陈述的事实已经过时。
- **源码/文档锚点**：`docs/KNOWN_ISSUES.md:413`
- **影响**：极低——元注释、不是活跃条目；不影响读者对现存条目的查找。但属于 KNOWN_ISSUES 自身范围内的一致性瑕疵，规则上「已删即不要留下指向已删条目的语句」应一起清理。
- **建议**：把「§2.53–§2.56、§2.60–§2.63」改为「§2.55–§2.56、§2.60–§2.62」，与「按 ID 删除、不留空号」保持口径一致。

### L-3 [低·覆盖可补] P-07 `TestSummaryRendersInteractiveNote` 缺 ZH 模板正向断言

- **描述**：测试只断言 EN 输出的字面值（"interactive workload accounts for 80.0%" / "(40/50)"），对 ZH 只断言"空 Workloads 不出 note"。i18n `SummaryInteractiveNote` 字段在 `i18n/report_doc.go:99-104`（ZH）已实现，**字面拼错不会被测试红**。
- **源码/文档锚点**：`internal/report/render_doc_test.go:41-65`
- **影响**：若有人误改 ZH 字符串（"工作负载占" 拼成"工作负载铺"），EN 断言仍绿，CI 不会阻断——i18n 双语一致性只能靠人眼/偶发 diff review 兜底。
- **建议**（不阻塞验收）：在 `TestSummaryRendersInteractiveNote` 加一段 ZH 模板正向断言，例如 `strings.Contains(out, "工作负载占")` + `strings.Contains(out, "（40/50）")`。结构上与 EN 段完全对称。

### L-4 [观察·无修复必要] P-09 修复合入后，§2.53 的设计意图被注释完整吸收

- **描述**：`internal/story/llm_findings.go:264-274` 的 13 行注释实际上把 §2.53 的"为什么"重新叙述了一遍（`extractRootUserIntent` 首条 user 消息、`buildGoalDriftPack` 首 checkpoint）。
- **依据**：注释承担了原 KNOWN_ISSUES 条目的解释职能——这是合理的，因为该条目已删除、且代码自身应当说明"为什么 > 1"。不再单独记录到 KNOWN_ISSUES。
- **影响**：无。属于 KNOWN_ISSUES「已修即删 + 留代码注释」的标准做法。
- **建议**：无需动作；仅在「reviewer 容易误以为漏写 KNOWN_ISSUES」时引用本条说明。

### L-5 [观察·非缺陷] 修复未覆盖的相邻边界（仅记录，不视为遗留问题）

- **P-01 相邻边界**：`RewriteStream` / `RewriteRoles` / `RewriteInputRoles` 早已使用 `MarshalNoEscape`（`rewrite.go:71/121/241`），P-01 的修复只是把 `RewriteModel` 拉回一致——不存在未覆盖的同类风险。
- **P-06 相邻边界**：helper 只在 `finishSession` 一处使用；`info.Recs[len-1]` 的 last 端若同样无 usage，回退到 EstIn，**意味着当会话末轮也缺 usage 时，ContextGrowth 可能使用「首估 / 末估」两个估计值的比**。测试覆盖了"首估"路径，但没显式覆盖"末估"路径——这是一个潜在的口径混合点。是否需要测试取决于是否有真实语料触发；若末轮缺 usage 通常意味着会话被中途截断（少见），验收不否决，但值得在后续观察中跟踪。
- **P-07 相邻边界**：`summaryInteractiveShare` 假设「每个记录落入恰好一个 workload 桶」——`workloadClassOf` 默认 `"interactive"` 已是这个性质；与 §2.13（额度燃尽看板）无耦合，验收不否决。
- **P-09 相邻边界**：`> 1` 的护栏假设 `Step.Seq` 从 1 开始且单调——这是 `taskseg` 与 `journey` 构造的不变量，测试未单独钉这条假设。验收不否决。

---

## 三、验收总结

| 项 | 状态 |
| --- | --- |
| P-01（`dadf63b`）| ✅ 通过 |
| P-06（`ed0ce67`）| ✅ 通过 |
| P-07（`36fc415`）| ✅ 有条件通过（功能完整，仅 ZH 模板测试覆盖不充分） |
| P-09（`2a341bd`）| ✅ 通过 |
| **CHANGELOG 同步** | ✅ 四项均已落 `[Unreleased]`（P-07 在 Added；P-01 / P-06 / P-09 在 Fixed），描述与代码事实一致 |
| **KNOWN_ISSUES 同步** | ✅ §2.53 / §2.63 已按「已修即删」移除；目标条目不再悬空（仅 §413 元注释的范围声明过时，见 L-2） |
| **整仓回归** | ✅ `go test -race -count=1 ./...` 全部绿（含 archtest） |

**整体可交付**：四项修复在功能、测试、回归三个层面均合格。**唯一阻塞级**的 遗留问题是 L-1（`PROJECT_REVIEW_REPORT_agent_20260902.md` 自身与代码现实不一致）——按任务说明书「不修改主报告」的要求仅作记录；建议主报告原作者按 L-1 的处置方案在下一轮评审前更新。L-2 / L-3 为低危文档/测试清扫项，可在任意时刻顺手处理；L-4 / L-5 是观察项，无需动作。

**遗留问题数：5**（L-1 中危 × 1，L-2 / L-3 低危 × 2，L-4 / L-5 观察项 × 2）。
