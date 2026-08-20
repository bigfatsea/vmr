// Ver 2026-08-20 11:30, by pi

# vmr 日志分析体系重构 - P4 ActionPlan 第二轮独立评审（事实核查 + 设计/策略复核）

## 0. 定位与方法

本文是对 `docs/future-strategy/story_report_p4_action_plan_sonnet-5.md`（下称 **P4 计划**）的独立评审。
与既有的 `story_report_p4_action_plan_review_gemini-3.7-flash.md`（下称 **Gemini 评审**）并列且互补：
重叠的发现各自独立复核后确认，不重复展开；本文的重点放在 Gemini 评审**没有覆盖**的事实错误、
设计边界问题与流程疏漏上，并对 Gemini 评审中一处需要修正的修法给出交叉意见（§5）。

评审手段：

1. **逐条核对计划引用的代码位置**（约 30 项：函数签名、行号、行数预算、flag 语义、既有先例），
   对照 P4 起点（HEAD `a24789f`）的真实源码。
2. **实证运行**：用当前工作区构建的二进制对 `logs/vmr-audit-2026-07-28.jsonl.zst` 实跑
   `-journey 'j-openclaw-20260728T000544*'`，核对计划 §2.3 步骤 4 的全部预期数字，并对产物做逐字段
   体积分析。
3. **对照三份上位文档**（架构文档 §7.4(b)/§7.6/§8、DevPlan P4 任务表与通用完成定义、
   KNOWN_ISSUES §1.20）检查计划与它们的一致性。

一个需要先说明的现状：**P4 正在被并行执行**。评审期间工作区先后出现 `structure.go`、
`structure_test.go`、`metrics.go`/`llm.go`/`cmd_story.go` 的改动（时间戳 2026-08-20 10:28 起）。
本文以**计划文本**为评审对象、以执行现状为佐证；凡"执行已纠正"的地方都会注明，因为"计划漏了、
执行时靠计划自带的验证步骤撞出来"本身就是一个计划质量的度量。

---

## 1. 事实核查总览

### 1.1 核实为准确的关键前提（抽样）

P4 计划 §0 "已读代码确认的关键前提"的核对结果**大体可信**，代表性核实项：

| 计划的断言 | 核查结果 |
| --- | --- |
| `JourneySummary` 只有 ID/Title/From/To/Partial/Metrics/Findings/LLMFindings（`metrics.go:399-408`） | ✅（HEAD 上结构体约在 393-404 行，行号有 5 行级偏差，无实质影响） |
| 行数预算 `journey.go 697/850`、`metrics.go 424/470`、默认 700（`defaultFileLineLimit`） | ✅ 逐项精确吻合（`git show HEAD:internal/story/metrics.go \| wc -l` = 424） |
| `j.Events` = 各 Step 的 `NewEvents` 按 Step 顺序拼接（`appendNewEvents` 同一循环写两份） | ✅ `journey.go:445-458` 原文确认 |
| `toolResultsFor`（`findings_toolresult.go:50`）exact + 归一化两级、CallID 重写回原始 `tc.ID` | ✅ 原文确认 |
| `truncateText`（`compare.go:699`）/ `initialInstructionExcerptChars = 2000`（`compare.go:270`） | ✅ |
| `buildTestJourney(t, n, injectFinding)` 写真实 JSONL + `Build(...)`（`corpus_test.go:88`） | ✅ |
| `EvidencePack`/`SingleJourneyEvidencePack` 直接吃 `*Journey`、不经过 `JourneySummary`；`journeyRef` 只投影 4 个标量字段 | ✅（`llm.go`/`compare.go:175-181`） |
| `audit.LineAt`、`ctxgraph.ParseReqCoord`、`EnsureSysPromptEvidence`/`EnsureToolsEvidence` 已由 P2/P3 交付 | ✅ |
| `-llm-dry-run` 需要 `-llm-addr`；P1 §4.3 记录的 69,936 字符基线存在；`llm.go` 当前 476 行 | ✅ |
| 真实语料预期：22 步、22 步带 req、33 次工具调用、33 配对 | ✅ **实证完全复现**（§1.2） |

结论：计划的数据质量在同系列文档里属于高的，预期数字可以直接复现。

### 1.2 实证复现（真实语料，2026-07-28 openclaw 样例）

```
steps: 22        with req: 22        tool_calls: 33        matched: 33
```

与计划 §2.3 步骤 4 的四项期望**逐项一致**。产物体积的逐字段分析见 §3.4（M4）。

### 1.3 事实错误清单（计划中确实错的）

| # | 计划原文 | 实际 | 严重度 |
| --- | --- | --- | --- |
| E1 | §0：`positionalToolResults`（**同文件** `:213`）-- 暗示在 `findings_toolresult.go` | 定义在 `render_spine_step.go:213`。行号对、文件错 | 低（误导实现者找错文件，一次 grep 可解） |
| E2 | §2.3/§2.4："与 **P2 §7** 记录的 6,428 字节基线对比" | 6,428 字节记录在**架构文档 §2.2**；P2 ActionPlan 里没有这个数字（P2 明确写"P2 不改 `journey-<id>.json`"） | 低（出处引错，数字本身真实） |
| E3 | §0："`Step.Manifest` 由构造保证非 nil，**全仓无 nil 判空先例**" | 执行落地时 `buildStepStructure`/`buildToolIndex` 都加了 `if s.Manifest != nil` 防御（测试夹具 `journeyOf` 直接手工构造 Step，nil 并非不可能）。断言过于绝对，虽然对生产路径成立 | 低（实现的防御性选择更好，但计划把它写成了"事实"而非"判断"） |

---

## 2. 第一梯队（严重问题 + 高 ROI 改进）

> 按"严重程度 × ROI"合并排序。T1/T2 与 Gemini 评审重叠（本文独立复核后确认，并各补一块
> Gemini 漏掉的内容）；T3/T4 为本文新发现。

### T1 【执行级遗漏】改动清单不含 `journey-<id>.json` 的唯一写入点（Gemini §1.3 确认 + 补充）

**问题**：计划 §2.3 步骤 2 只改 `metrics.go` 的 `JourneySummary` + `Summarize`。但
`journey-<id>.json` 的生产写入点是 `cmd/vmr/cmd_story.go` 的 `writeJourneyFile`（约 :736），它
**手工构造 `JourneySummary` 字面量、从不调用 `Summarize`**。`Summarize` 在生产代码里的唯一调用点
是 compare 路径（`cmd_story.go:457`）--而那条路径恰好**不需要** Structure。

**后果**：严格照计划执行，journey JSON 里会出现 `"structure": {"tasks": null}` --字段存在、内容为空，
比字段缺失更具迷惑性（schema 上"已交付"，数据上零交付）。计划自带的 §2.3 步骤 4 验证脚本会抓住它，
执行期间也确实是这样被发现并补上的（工作区 `cmd_story.go` 的 diff 就是那一行
`Structure: story.BuildStructure(j)`）--但"计划精确到行数预算，却漏掉交付物的唯一序列化点"说明
§0 "已读代码确认"的覆盖面有盲区：读了 `internal/story` 的全部相关文件，没读 `cmd/vmr` 的写入路径。

**补充建议**（Gemini 评审未提）：

1. 给 `writeJourneyFile` 补一个**产物级测试**（当前该函数完全没有测试）：断言写出的 JSON 中
   `structure.tasks` 非空、steps 数与 Journey 一致。本次是靠人工验证脚本兜住的；下一个改
   `writeJourneyFile` 的人未必再跑一遍。
2. 顺手登记：compare 路径的 `Summarize` 现在会为两个 Journey 各白算一份 `BuildStructure`
   （结果只被 `journeyRef` 的 4 字段投影消费）。浪费是毫秒级的，但值得在 `Summarize` 的注释里
   写一句，避免后人误以为 compare 依赖 structure。

### T2 【设计缺口】"无损重建 fact-layer"的验收承诺与字段设计不自洽（Gemini §1.2/§1.4 确认 + 补 Edge）

**问题**：DevPlan P4.1 的验收是"可由该产物**无损重建当前人读事实层的等价内容**"，P4 计划 §2.4
原样引用了这句话。但事实层（`render_md.go` 的 `renderStep`）每步还渲染四类内容，`StepStructure`
均未承载：

| fact-layer 渲染的内容 | StepStructure 现状 | 能否从 structure + 审计日志重建 |
| --- | --- | --- |
| 步头行：`DurMS`/`TTFTMS`/`Usage(Fresh/CacheRead/Out)`/`Endpoint` | ❌ 缺 | 可以（都在记录里），但需逐步回源 I/O |
| `Edge`（编辑分类行，`render_md.go:108-110`） | ❌ 缺（**计划与 Gemini 评审均未提及**） | 仅靠重新实现 `ctxgraph.Classify` |
| `StitchEdge`（缝合类型/得分/置信度，:111-113） | ❌ 缺 | **不能**--需重跑缝合图分析 |
| `Compaction`（token 前后/实体吞噬存活摘要，:117-119） | ❌ 缺（计划显式推迟到 P5 边界复核） | **不能**--需跨记录实体比对 |

其中 StitchEdge/Compaction 是**图层级分析结论，物理上不存在于任何单条审计记录**，`Req` 坐标
救不了它们。这正是 DevPlan 硬依赖"P5 依赖 P4--先删后补会留下一个真丢结构的版本"所要防的事：
P5.1 删除 fact-layer 时，其验收（"内容无丢失--以 P4 的无损重建检验为对照"）建立在一个比字面承诺
**弱**的检验上，缝合边界与压缩分析的展示将静默消失--而压缩信息损失分析恰是 Analytics 设计文档
里 Compaction 叙事的核心卖点。

**修复成本与 ROI**：这些都是结构化小字段（枚举、数值、短实体列表），不是对话正文，加进
`StepStructure` 完全不违反内联/引用边界；Gemini 评审已给出 `StitchRef`/`CompactionRef` 的形状，
本文只补一句：**`Edge.Kind` 同样该进**（一行字符串，`omitempty`），以及 usage 四元组的存在让
"单步成本剖面"类下游查询免去 22 次回源解压。若最终决定不加，必须把"无损重建"的验收语义在
DevPlan/KNOWN_ISSUES 里显式收窄并说明理由--那是一条比加字段更贵的论证。

### T3 【设计边界偏离，未登记】`ToolCallRef.Result` 内联超出了架构文档划定的内联边界（本文新发现）

**架构文档的边界原文**（§7.4(b) 与 §8 决策表两处一致）：

> 保留在 JSON 里的短文本是例外且有理由：Step 的 `RespText`/`Reasoning` 摘要、tool_call 的
> **参数**--它们是"决策"本身而不是"上下文"。
> （§8：属于本轮决策的（RespText/Reasoning/tool_call 参数）可内联）

DevPlan P4.2 的任务表述同样只写了"属于本轮决策的短文本可内联；**属于对话历史的正文一律引用**"。

**工具结果是对话历史**，而且是字面意义上的：`tool` 角色消息会被客户端逐字回写进下一轮请求体，
因此它已经作为 `EventRef` 出现在**下一步的 `NewEvents`** 里（实证：样例 journey 的 57 条 new_event
中 `tool` 角色恰好 33 条，与 33 次工具调用一一对应）。P4 计划的字段表把 `ToolResult.Text` 加进
内联集合，是**对上位文档边界的一次静默扩展**--计划没有引用任何支撑它的裁决，也没有登记。

**实证分量**（样例 journey，JSON 共 112,840 字节）：

| 内联字段 | 字符数 | 占内联文本比 |
| --- | --- | --- |
| `resp_text` | 910 | 1.6% |
| `reasoning` | 10,140 | 17.7% |
| `args` | 21,348 | 37.3% |
| **`result`** | **24,796** | **43.4%** |

结果文本是最大的内联分量，且它此刻有**三条**存在路径：`ToolCallRef.Result` 内联、下一步
`NewEvents` 的哈希引用、审计日志原文--按项目自己的 blob/tree 原则，这是 tree 里塞了一份 blob。

**两个都成立的处置，选一个并登记**：

- **(a) 登记为显式偏离**：论据是真实的--结果是 Finding 检测器与读者最关心的证据，内联让消费者
  免回源；有 `structureExcerptChars` 封顶。若走这条路，更新架构文档 §7.4(b)/§8 那两处边界原文，
  并在 KNOWN_ISSUES 登记"结果内联是登记过的偏离"，防止后人拿着原文来"修"它。
- **(b) 收敛回引用**：`ToolCallRef` 保留 `Matched`/`ResultError`/`ResultTruncated`，正文改走
  下一步 `NewEvents` 的既有哈希引用（配对关系本身仍由 `ToolCallRef.ID` 给出）。文件体积立减
  ~40%，且与 `args` 的处理形成清晰对照（参数是决策→内联；结果是历史→引用）。

不建议的是现状：既不登记也不收敛。这条边界是 P4.2 常驻检查要守的那条线，计划自己在守线的同时
把线挪了半格。

### T4 【检验强度】"无损重建检验"的三处削弱（Gemini §1.1 只发现了其中一处，且其修法不完整）

计划 §2.3 步骤 3 把 `TestBuildStructure_LosslessReconstruction` 定位为"P5.1 的验收会直接引用"的
对照物，但它的设计与"仅凭 JSON + 审计日志重建"的承诺之间有三处落差：

1. **测试偷用了内存态**：切片用的是 `s.DeltaStart`（未发布的内存字段），不是从 JSON 可得的信息。
   Gemini 评审 §1.1 发现了这一点。**但其修法（把 `DeltaStart` 发布进 JSON、测试改用
   `ss.DeltaStart`）不完整**，见下一条。
2. **"从 DeltaStart 切片、逐一对应"在去重存在时根本不成立**：`appendNewEvents` 的语义是
   `NewEvents = msgs[DeltaStart:] 减去全局已见哈希`。缝合边界处 `DeltaStart == 0`、整个 manifest
   全量扫描、前驱已展示的内容全部被去重压掉（`journey.go` 的 StitchEdge 注释原文写明了这一点）；
   纯追加链内出现逐字重复消息时同理（`exact_repeat_tool_call` 这个 Finding 的存在证明重复真实发生）。
   因此对任何**有缝合边界的真实 journey**，`msgs[DeltaStart:]` 与 `NewEvents` 是"过滤子序列"关系，
   严格的一一对应断言会挂--Gemini 的修法在合成夹具上能过，拿到真实缝合 journey 上就失效。
   **更强的外部重建路径是哈希匹配**：对每条 `EventRef.Hash`，在 `Req` 取回的记录解码消息里找
   md5 匹配的那条。这一路径 (i) 不依赖 `DeltaStart`（要不要发布它变成独立的导航便利问题，而非
   正确性问题），(ii) 顺带验证了哈希契约本身（见 M1），(iii) 天然兼容去重。
3. **覆盖面只有"最干净"的形态**：现在的无损测试只跑无去重、无缝合、无 compaction 的三步合成
   夹具；真实语料验证只数计数（22/33/33）。恰恰是缝合边界、逐字重复这些**重建最难的形态**没有
   被检验。建议至少加一个含缝合边界（或注入重复消息）的夹具，用哈希匹配做断言。

**ROI 说明**：P5.1 删 fact-layer 的"内容无丢失"完全押在这个测试上。现在花半天把它做实，比 P5
之后发现缝合 journey 重建不出来再回头补便宜得多。计划 §5 边界复核里那句"若测试设计比本文预想的
更弱……需要如实标注"说明作者预感到了这个风险--预感到就该在计划里直接把测试设计写对。

---

## 3. 第二梯队（中等严重度）

### M1 【契约不完整】`EventRef.Hash` 的推导规则没有随键一起发布

结构层发布了一个引用键，却没发布**键怎么算**。实际推导是：消息原文（解码后的原始 JSON 值）经
Go `json.Marshal`（map 键排序、`<`/`>`/`&` HTML 转义）后取 md5（`ctxgraph/hash.go:66` 的
`hashJSON`，未导出）。两个后果：

- 跨语言消费者（Python 脚本、未来 Web 服务）想按哈希取正文时，`json.dumps(sort_keys=True)` 与
  Go 的序列化**不逐字节相同**（分隔符、HTML 转义），自己摸会摸错；
- 它也不在 `EventRef` 的文档注释里，只存在于 `ctxgraph` 源码。

建议：在 `structure.go` 的 `EventRef`（或包文档）里写死推导口径一句话；这也正是 T4 建议的
哈希匹配测试能顺带钉住的东西。`DeltaStart` 若发布（Gemini 建议），其语义（对
`chatmsg.Messages` 的 0-based 下标、缝合边界恒 0）也须同样写死。

### M2 【流程疏漏】收尾清单缺了 UserGuide 与 Analytics 设计文档的同步

计划 §5 列了 CHANGELOG、KNOWN_ISSUES、架构文档备注，但 DevPlan 通用完成定义第 4 条要求的
"用户指南及其中文兄弟"与"既有设计文档对应章节"都没出现，而这两处都**逐字段描述了
`journey-<id>.json` 的内容**：

- `docs/UserGuide.md`（story 一节）："journey-<id>.json … nine rule-derived, zero-LLM-cost
  numbers … plus model usage & switches" -- P4 之后这句话对了一半、漏了一半；
- `docs/VirtualModelRouter_Design_v4_Analytics.md`（§3.5 附近）："落盘这九项指标 + 模型使用/切换
  + Journey 身份 + Findings" -- 同样需要补 structure 一句。

不同步的代价是文档描述的产物形状与真实产物形状分叉，且分叉恰好发生在"契约"文档上。
（本条同样适用于在写的 CHANGELOG 措辞：structure 是产物形状变更，归 Changed 而非纯 Added 更
诚实。）

### M3 【一致性】`structure` 没有 schema 版本戳

P3.7 刚给解析缓存补了 `CacheSchemaVersion`，理由是"改了提取逻辑旧缓存会被静默复用"。同样的教训
对 journey JSON 成立：文件是幂等重写的，但**只在被重新渲染时**重写--磁盘上躺着的旧
`journey-<id>.json`（P4 之前生成的）与新的逐字段同名却不同形（没有 structure / 旧哈希口径），
消费者无从分辨。一个 `structure_schema int` 字段（或复用未来的 JSON 语言策略字段一并考虑）成本
接近零，与 P3.7 的先例对齐。优先级不高，但**改在 P4 落地时最便宜**，P5/P6 之后再补就要考虑
"已有文件怎么办"。

### M4 【验收判据模糊】"体积保持在结构量级"没有数字，实测落在灰色地带

实测：样例 journey 的 JSON 从 6,428 字节涨到 **112,840 字节**（17.6×，~5.1KB/步），其中内联决策
文本 57,194 字符（构成见 T3 表；截断触发率：args 6/33、result 6/33、resp 0/22）。这**不违反**
"随步数而非正文长度增长"（有封顶），但"不应跳变到 KB×步数的量级"这句话按字面读，5KB/步就在
KB×步数量级上。问题不在结果，在判据不可判定：一个常驻体积检查守的是不等式，不等式的右边得是
公式。建议把验收写成封闭形式，例如：

```
bytes(structure) ≤ Σ_step (structureExcerptChars × 内联字段数(step)) + 固定开销
```

并在 KNOWN_ISSUES 或结构体注释里记下实测锚点（22 步/33 调用 → 112,840B）。这样后续任何人调
`structureExcerptChars` 或加内联字段，体积检查的边界跟着公式走，不用重新吵架。

### M5 【验证脚本的可复现性】两个操作层缺陷

1. **`report.yaml` 的 `llm_addr` 默认值会让验证命令真打 LLM 调用**。实证：按计划 §2.3 步骤 4 的
   命令原样执行，输出里出现 `calling http://192.168.0.22:8800/v1/ (model=cheap): evidence pack
   22551 chars` -- 本机的 report.yaml 配了 `llm_addr`，`-llm-addr` 的 flag default 会吃它。副作用：
   产生真实费用、往审计日志里回流自指流量（正是架构文档 §9.1 要在 P6.4 清理的那类污染）、往被测
   产物里写入 `llm_findings`（污染体积度量）。验证命令应显式传 `-llm-addr ''` 或在无 config 的
   目录下跑，计划应当注明。P1/P2/P3 的 ActionPlan 同样有这个问题，本次一并指出。
2. **P1 的 69,936 字符证据包基线没有绑定 journey 对**：P1 文档写的是"`<id1>,<id2>` 换成列表里
   挑的两条"，69,936 属于当时挑的那一对，但那一对是谁没有记录。P4 计划 §3.3 拿它当对比基线，
   换一对就不可比。要么在收尾时把当时用的 id 对补记进 P1 文档，要么 P4 自己重测一对并记录。

---

## 4. 第三梯队（低严重度 / 卫生项）

- **L1** 计划"全仓无 nil 判空先例"的绝对化表述与实现现实不符（见 E3）。落地的防御性判空是对的，
  需要改的只是计划里把判断写成事实的措辞习惯。
- **L2** compare 路径白算两份 `BuildStructure`（见 T1 补充建议 2）。
- **L3** 计划 §4.1 要求"落地时用真实语料跑一次 Args/Result 长度分布再定 2000 这个数"，但分布
  数据没有留档（结构体注释只写了"如果太激进再分叉"的预案）。本文实测：args/result 各 6/33 触发
  截断、resp 0/22 -- 2000 对该语料不激进，这个结论应当写进注释或本评审存档，否则下次还会有人
  重新做这个测量。
- **L4** `EventRef.FirstStepSeq` 在 `NewEvents` 嵌套语境下恒等于所在 Step 的 `Seq`（
  `appendNewEvents` 构造保证），是冗余字段。Gemini 评审 §2.2 已指出；保留的辩护理由（消费者
  展平全局事件流时不必回溯父节点）成立，那就把这个理由写进 `EventRef` 的注释，把"冗余"变成
  "为展平场景设计的反范式"。
- **L5** 验收清单在 §2.4、§3.4、§6 三处重复维护，收尾勾选时容易漏项（纯流程卫生）。

---

## 5. 对 Gemini 评审的交叉意见

| Gemini 评审条目 | 本文意见 |
| --- | --- |
| §1.1 DeltaStart 遗漏（High） | **确认发现成立，修法不完整**：只发布 `DeltaStart` 并把测试改成 `msgs[ss.DeltaStart:]`，在有缝合边界或逐字重复的 journey 上会因去重过滤而失败（T4 第 2 点）。正确形状是哈希匹配断言（免 DeltaStart、顺带验证哈希契约、天然兼容去重）；`DeltaStart` 本身可作为导航便利字段另行发布。 |
| §1.2 StitchEdge/Compaction 遗漏（High） | **确认**，并补充 `Edge`（编辑分类）同缺、以及"P5.1 验收依赖链"的具体传导路径（T2）。 |
| §1.3 writeJourneyFile 遗漏（Med-High） | **确认**，补充"产物级测试缺失"与"Summarize 接入点选错"两点（T1）。 |
| §1.4 内联 usage/dur/ttft/endpoint（High ROI） | **确认**，但注意它应并入 T2 的"无损重建"缺口来论证（这些字段 fact-layer 今天就渲染，缺失首先是验收缺口，其次才是免 I/O 优化）。 |
| §2.1 structureExcerptChars 解耦 | 部分同意：解耦与否是口味，**必须做的是把分布实测留档**（L3）。计划"一个上限不是两个"的原则也有其道理，分叉要有数据理由。 |
| §2.3 体积守卫门限过宽 | **不建议照单采纳**：`4 × structureExcerptChars × 4` 恰是"每步 4 个封顶字段全量变化"的理论上界，不是随意放宽。收紧到 1.5× 之前，得先确认夹具确实只有一个字段在变，否则合法改动会误报。 |
| §2.4 byID 覆盖风险 | 同意加注释即可，无需代码改动（`toolResultsFor` 的 CallID 重写已含规范性保证）。 |

---

## 6. 建议动作清单（合并去重，按落地顺序）

**P4 收尾前应做**（成本小、且只有现在做最便宜）：

1. `StepStructure` 补 `StitchRef`/`CompactionRef`/`Edge.Kind`/`Endpoint`/`DurMS`/`TTFTMS`/
   `Usage`（T2）；同步更新计划 §2.1 的字段表与"无损重建"论证。
2. 对 `ToolCallRef.Result` 的内联做出**显式裁决**（登记偏离 or 收敛为引用，T3），更新架构文档
   §7.4(b)/§8 的边界原文与 KNOWN_ISSUES。
3. 重写无损重建测试为哈希匹配断言，并加一个含缝合边界/重复消息的夹具（T4）。
4. `writeJourneyFile` 补产物级测试（T1）。
5. 收尾清单补 UserGuide.md/.zh 与 Analytics 设计文档两处同步（M2）；验证命令注明
   `-llm-addr ''` 隔离（M5）。
6. 在 `structure.go` 注释里写死：哈希推导口径（M1）、`FirstStepSeq` 的反范式理由（L4）、
   截断分布实测锚点（L3）、体积公式上界（M4）。

**可随 P5/P6 再做**：`structure` schema 版本戳（M3，但若要加最好也在 P4 定稿时一并）、
`DeltaStart` 作为导航字段发布（T4 附带）、P1 基线补记 journey 对（M5.2）。

**总体结论**：P4 计划的方向、边界意识与数据质量都过关--预期数字逐项复现，前提核查大体准确，
对 P4.3 的诚实重定界（"钉测试而非修 bug"）尤其值得肯定。它的两个真问题都在"承诺与设计的对齐"上：
一个是**交付物序列化点漏进改动清单**（T1，执行已纠），一个是**"无损重建"的承诺比字段设计和测试
设计都大**（T2/T3/T4）。前者已付出过一次发现成本，后者是 P5.1 的地基--趁 P4 还没收尾，把地基
浇够。
