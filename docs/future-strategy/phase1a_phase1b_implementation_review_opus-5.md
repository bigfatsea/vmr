<!-- Ver 2026-08-17, by Claude Opus 5 (review-only; no code was modified in this pass) -->

# VMR Agent 运行时分析 Phase 1a / Phase 1b 落地实现复核报告

> **审阅范围**：Phase 1a 六个提交（`ec57bae` → `4c58184`：ec57bae / 4989284 / 8159726 / e50057b / 3ee11eb / 4c58184）与 Phase 1b 一个提交（`c61c2f6`）。`c61c2f6` 之后的 `b8e8d47`（蓝菱形前缀）与 `34d2840`（compare 自动补渲染）不在本轮声明的范围内，未深入复核。
> **对照基准**：`docs/future-strategy/agent_runtime_implementation_roadmap_gemini-3.7-flash.md` 第 9 章、`phase1a_implementation_plan_gemini-3.7-flash.md`、`phase1b_implementation_plan_gemini-3.7-flash.md`（含两份计划内嵌的核实修订与复核记录）。
> **方法**：逐提交 `git show` 通读全部改动；对可疑行为写临时探针测试实证（跑完即删）；复跑 `go build`/`go vet`/`gofmt -l`/`go test -count=1`（chatmsg/story/report/i18n/archtest 全绿）；用 `logs/vmr-audit-2026-07-25/26/27` 真实语料复跑 `story -corpus` 两轮（逐字节比对）与 `vmr report`，核对计划文档声称的数字。
> **结论先行**：两阶段实现总体质量高、与计划符合度好，Phase 1a 已知的 4 处缺陷修复与 1 处设计缺口补齐均确认真实落地。本轮新发现 **2 处较重要问题**（journey JSON 不含 LLM Finding、E4 候选序不确定）、**1 处确定违反仓库惯例**（CHANGELOG 未更新）、以及若干中低severity 的疏漏与可改进点，详见下文与第 5 节汇总表。

---

## 1. 环境与基线验证（复核前提）

以下全部为本次实测，作为后续判断的证据基线：

| 验证项 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` | 通过，无输出 |
| `gofmt -l`（story/chatmsg/i18n/cmd/vmr/_eval 全部触碰文件） | 空 |
| `go test -count=1`（chatmsg / story / report / i18n / archtest） | 全部 ok |
| 关键文件行数 vs 预算 | 全部在预算内（llm_findings.go 523/700、findings.go 518/580、journey.go 697/850、render_spine.go 379/380、entities.go 160/700、cmd_story.go 742/850 等） |
| `vmr story -corpus`（07-25/26/27 真实语料）连跑两轮 | 输出逐字节相同；58 lineage、11 journey，与 phase1a 文档 §4.3 一致 |
| corpus 报告中 `output_repetition_rate` | 进入指标分布（均值 9%、P90 15%）与相关性矩阵（与工具调用次数 rho=0.77），与 phase1a 文档 §4.3 声称一致 |
| `vmr report`（07-25 语料） | 正常完成；`vmr-report.json` 中非空 `SwallowedEntities` 列表为 **0 个**（见 P1a.1 发现 1b） |

---

## 2. Phase 1a 逐任务复核

### 2.1 P1a.1 实体提取重构（`ec57bae`，`internal/chatmsg/entities.go` + 测试）

**符合项**：多模式分类扫描（URL／显式路径／带扩展名文件／目录／CLI 白名单／驼峰／蛇形）、尾部标点剥离、空间子跨度重叠过滤、原序去重与 `MaxEntities=30` 上限均按计划落地；核实修订提出的 `dirPathRe` 两段式约束（`(?:\/[\w.\-]+)+\/`）确认存在——`and/or`、`yes/no`、`km/h`、`1/2`、`他/她` 实测全部不提取；CLI 白名单按核实修订收紧为已知子命令（`go test`、`git commit` 等），`go read`/`go ahead` 实测不命中；负例测试按 phase1a §5.1 修复 #1 的方式改写为检查截断形式（`and/`、`yes/`），防回归有效。

**发现（2a-1，低）**：`report` 半区回归验收实际上没有被真正锻炼到。计划验收标准明确要求"用真实语料跑一遍 Compaction 吞噬检测，确认 `SwallowedEntities` 判定没有漂移"，但实测 `vmr-report.json`（07-25 语料）里非空的 swallowed 实体列表为 0——该语料根本没有 Compaction 事件，"人工抽查 Compaction 章节"是空转。P1a.1 是全计划里唯一要求跨半区回归的改动，这次回归实际上没验证到它的目标路径。**建议**：换一份含 Compaction 的语料（或构造 fixture）重跑一次；重点看新引入的驼峰/蛇形实体（见下条）在 `SwallowedEntities` 里的噪音量。

**发现（2a-2，低）**：实测噪音实体存在但可预期：`"See section 3.5 and Fig 2.1 in the U.S.A report"` 提取出 `U.S.A`（fileExtRe 类）；`"I'll GitHub the code and macOS style iPhone"` 提取出 `GitHub macOS iPhone`（camelIdentRe 类）。这类词条在 story 侧要同时出现在推理与动作两侧才影响判定，噪音会被 `entityReferenced` 的双向子串匹配放大一点（计划核实修订第 3 条预警过），但真实语料回归（§4.3 的 58 lineage 渲染）未见异常。属于已知代价，建议在 `entities.go` 的包注释里点名"专有名词（GitHub/macOS/U.S.A）会被驼峰/扩展名规则吸收"这一已知噪音面，避免下一个读者当成 bug 报。

**发现（2a-3，低）**：`trimPunctuation` 会把 `./...` 整个修剪掉（尾随 `.` 全剥后长度 <4），顺带消灭了 `go test ./...` 里的 `./...` 噪音——这是好事，但也意味着任何以 `.` 结尾的合法路径会被破坏。极边缘，记录即可。

**发现（2a-4，文档）**：phase1a 文档 §4.1 声称 P1a.1 的改动文件包含 `internal/story/findings.go`（"`~/` 与 `./` 容忍匹配"）。实际上该改动（`cleanEntityForMatch`）落在 P1a.7 的提交 `4c58184` 里，`ec57bae` 只碰了 entities.go 与其测试。执行摘要把改动归错了提交，属文档归因错误，不影响代码正确性。

### 2.2 P1a.2 参数 JSON 规范化（`4989284`，新建 `toolcall_normalize.go`）

**符合项**：`canonicalizeToolArgs` 独立纯函数；`json.Decoder + UseNumber()` 修复（phase1a §5.1 修复 #2）确认在位，且有 2^53 精度回归测试；接入点只有 `metrics.go` 的 `toolCallKey` 一处（核实修订纠正过的"不改 findings.go"被遵守）；空串、非法 JSON 回退、嵌套对象键排序均有测试。findings 侧经 `groupToolCallsByKey` 自动受益，无循环依赖。

**发现（2b-1，低）**：两处语义未注释的等价合并——(a) 非 JSON 回退 `strings.Join(strings.Fields(...), " ")` 会把**引号内**的多空格也折叠（`bash -c "echo   a"` ≡ `bash -c "echo a"`），轻微过度合并；(b) JSON 重复键 `{"a":1,"a":2}` 规范化为 `{"a":2}`。两者作为"重复调用检测的 key"都算合理取舍，但建议加一行注释写明这是刻意的，否则后人可能当成 bug 修。

### 2.3 P1a.3 Shell 验证意图候选 + OpenAI 错误调查（`8159726`，新建 `verification_intent.go` + KNOWN_ISSUES）

**符合项**：CLI 验证命令白名单（17 个模式）而非"关键字 + 任意小写词"（遵守核实修订的收紧要求）；shell 工具名集合 8 个；JSON 参数键提取（`command`/`cmd`/`script`/`input`/`CommandLine`/`args` 数组）；正反例测试齐全；KNOWN_ISSUES 写入 OpenAI 调查结论条目（维持"不做自由文本嗅探"）。Phase 1a 内无调用方（纯候选层，为 P1b.6 备料）符合计划。

**发现（2c-1，中低）**："495,672 条 / 0.00%"这个调查数字永久不可复核——调查脚本按收尾纪律被删除，仓库无任何可重跑产物。phase1a §5.3 已自我披露为"信任但未复核"，但对比 Phase 1b 的校准脚本 `_eval/calibrate_p1b.go` 被完整保留，两次对"探索性调查产物要不要留痕"的处理不一致。**建议**：把这条经验写进 KNOWN_ISSUES 1.15 类治理条目或 CLAUDE.md 惯例——调查型脚本的产物（脚本或至少扫描日志）应保留在 `_eval/` 下，结论数字必须可重跑。

**发现（2c-2，低）**：`extractCommandText` 的键白名单里有 `input` 这类泛型键，命中后内容未必是命令（候选层误召回无害，但 `Pattern` 匹配是在整段文本上做的，`{"input":"see go test docs"}` 这类内容会命中 `go test` 模式）。候选层 + LLM 终审的设计消化得了，记录即可。

### 2.4 P1a.4 / P1a.5 Context Rot 与工具序列挖掘（`e50057b`，新建 `corpus_contextrot.go`/`corpus_sequence.go` + 接线）

**符合项**：渲染下沉纪律严格执行——`render_corpus.go` 仅 +3 行（两节各一行调用 + 空行），渲染主体在两个新文件内；`i18n/story_corpus.go` 中英文标题/表头/脚注齐全；序列排序 tiebreak（phase1a §5.1 修复 #3）在位，实测 `-corpus` 两轮输出逐字节相同；分桶边界一致（32000 落入 "32k-64k"）；n-gram 跨 Task 隔离正确；`isErrorStep` 复用 `isErrorMarker` 由两节共享。

**发现（2d-1，中低，重复计算）**：`computeContextRot(journeys)` 内部对每个 Journey 重新调用 `ComputeFindings(j, i18n.EN)`，而 `ComputeCorpusStats` 在其上方约 20 行处**刚刚**算过 `findingsPerJourney[i] = ComputeFindings(j, i18n.EN)`。纯离线场景下只是重复计算，但两处调用点意味着未来规则层改动时这两个口径有分叉风险。**建议**：给 `computeContextRot` 加一个 `findingsPerJourney [][]Finding` 参数复用现成结果。

**发现（2d-2，低）**：`contextRotRanges` 的 `max` 注释写 "inclusive upper bound"，实际判断是 `tokens < r.max`（半开区间）。行为没错，注释错。

**发现（2d-3，低）**：`s.Manifest == nil` 的 Step 以 0 token 计入 "0-32k" 桶，会轻微污染第一个桶的密度/错误率。建议跳过或单独注释。

**发现（2d-4，低）**：计划核实修订第 2 条要求 ErrorRate 字段"必须在文档/字段注释里注明 OpenAI 语料系统性偏低估的已知偏差"。实现只写了 `// error_step_count / step_count`，i18n 脚注说"基于协议原生错误标记统计"——交代了口径来源但没交代偏差。KNOWN_ISSUES 的 OpenAI 条目全局覆盖了这件事，算部分满足，但离计划原话有距离。

**发现（2d-5，低，文案与实现不符）**：`computeToolSequences` 里 `if ps.count >= 1` 是恒真的死过滤；i18n 的 `NoToolSeq` 文案说"未提取到**满足频次阈值**的工具调用序列"/"No tool call sequences met the **frequency threshold**"——代码里根本没有频次阈值（只有 Top-10 截断）。文案误导，二者应统一（要么加最小频次阈值，要么改文案）。

**发现（2d-6，低）**：序列的"尾步错误率"把 Step 级错误标记摊到该 Step 的**所有**工具调用上（一个 Step 3 个调用共享同一错误标志），是粗糙代理但结构体注释未说明。

### 2.5 P1a.6 输出重复率指标（`3ee11eb`，新建 `verbosity.go` + metrics/compare/i18n 接线）

**符合项**：核实修订的方案 A 完整落地——`Metrics.OutputRepetitionRate` 字段、`ComputeMetrics` 接线、`MetricOutputRepetitionRate` 常量 + `metricSpecs` 条目（自动获得分布/相关性/`-compare` diff/渲染）、`i18n/story_compare.go` 中英文标签；`compare_test` 的 13→14 行断言同步更新。真实语料实测数字与文档声称一致（9%/15%/rho=0.77）。中英混合分词（CJK 单字 token、ASCII 词 token、统一小写）处理得当。

**发现（2e-1，低）**：n-gram 窗口跨 Step 边界、跨 RespText/Reasoning 拼接点（无分隔符直接串 token），边界 gram 是稀释性噪音。无注释说明。极小。

### 2.6 P1a.7 计划格式解析扩展（`4c58184`，新建 `plan_parse.go` + findings/render_spine 瘦身）

**符合项**：实测六类输入全部正确解析——`(1)`/`1)`（第一分支经回溯覆盖，phase1a §5.3 第 4 条说的"第二分支是死代码"经本人独立验证成立）、`步骤一：`（中文数字映射）、数字列表与 Checklist 混排时取结尾更靠后的一段、Checklist 位置临近性拆段（§5.2 用户拍板方案）及两条回归测试在位；`findings.go` 从 547 降到约 490（现为 518，因 P1b 又加了字段），`minPlanItems/maxPlanItems` 语义保留；`detectPlanExecutionMisalignment` 只消费 `PlanItem.Text`，`Kind/Index` 留给 P1b——与计划一致。

**发现（2f-1，低，随行语义变更未记录）**：`render_spine.go` 的 `StepTagPlan` 判定从"任意单行命中 `numberedListRe` 即为 Plan"变成"`ExtractActionablePlan(...) >= minPlanItems`（连续 ≥2 条）"。只含孤立一行 `1. xxx` 的 Step 现在会跌落为普通文本标签。变更是合理方向（"计划"本应多条），但这是一个**行为变更**，计划文档与执行摘要均未提及，golden.md 测试恰好覆盖不到单行场景。建议在 plan_parse.go 或 render_spine.go 加一行注释声明这个语义收紧是有意的。

**发现（2f-2，微）**：死代码 `numberedRe` 第二分支仍在（文档明确说留待下次顺手清理，尊重该决定，此处仅登记）。

---

## 3. Phase 1b 复核（`c61c2f6`）

### 3.1 架构契约与数据模型

**符合项**（逐条对照 phase1b 计划 §1 与路线图 9.3 架构决策）：

- `Finding` 增 `Source`/`Confidence`/`EvidenceAnchor` 三字段，全部 `omitempty`，JSON 向后兼容；
- `FindingSource`/`FindingConfidence` 类型 + 四个新 `FindingCode` 常量；
- `ComputeFindings` 确认保持纯规则、零 LLM（源码通读 + 包注释未动）；六个判别器全部走独立入口 `ComputeLLMFindings`，fail-open 由"不可达地址返回 nil 无 error"的测试锁定；
- HIGH + 非空锚点才晋升 Finding 的门禁在六个判别器里一致执行（`strings.ToUpper(item.Confidence) == "HIGH" && EvidenceAnchor != ""`）；
- prompt 版本化（每个 pack 自己的 `Version`，如 `tool-misinterpretation-v1`）、缓存走 `llm.go` 的 `cacheKey`（pack JSON 全量入哈希）、`-llm-cache-dir` 生效；
- `render_spine.go` 379/380 一行未超（`formatFindingHeader` 扩参净增 0 行），渲染辅助在 `render_inferred.go`；
- 规则版/LLM 版共用 `FindingPlanExecutionMisalignment` 的歧义按 §7.4.2 拍板方案处理（`hasMixedSourceHit` 混源时给规则版补 `[规则检测]` 标签，单源不加），两条回归测试在位；
- P1b.5 动态重规划基线（§7.1 修复）确认实现：`jaccardSim < 0.4` 才重置基线，回归测试覆盖"计划变更后只审计新计划"；
- `_eval/calibrate_p1b.go` 是真校准：调生产入口 `ComputeLLMFindings`、真实 VMR 实例、机械锚点核验（对重组后的 `RespText`/`Reasoning`/`ToolCalls[].Args`/`NewEvents` 做字面子串匹配）、明确拒绝编造 Precision/Recall——§7.4.1 描述的"锚点校验器自身 bug"（对原始 audit JSON 匹配导致 0/9）的修复方向正确且注释诚实；
- KNOWN_ISSUES 两条决策记录（1.18 未完成校准的开放条目 + §2 的 LLM Finding 契约"确定不修"类记录）均已写入，路线图 9.6 的两条决策要求落实；
- 六个判别器的中英文 System Prompt 逐条通读：锚点"只放一段逐字摘录、禁止拼接、禁止自造标签"的规则在 ZH/EN 两侧措辞一致（§7.4.1 修复的 Prompt 矛盾已消除），置信度三档判据与计划 §1.1 契约吻合。

**发现（3-1，中高）：journey-<id>.json 永远不含 LLM Finding，.md 与 .json 出现事实分叉。**
`writeJourneyFile` 的 JSON 部分序列化的是 `story.Summarize(j)`，而 `Summarize` 内部自己重算 `ComputeFindings(j, i18n.EN)`——只有规则 Finding。`renderJourney` 里合并进 `findings` 的 LLM Finding 只进了 Markdown。后果有三层：

1. **打破既有不变量**：`findings.go` 的 `ComputeFindings` 注释原话是"journey-<id>.json (always EN) and the rendered Markdown (target lang) must never disagree on WHICH Steps got flagged"。带 `-llm-addr` 渲染时，.md 有 `llm_inferred` Finding 而 .json 没有——恰好是这条注释禁止的分叉（虽然分叉的成因从"语言"变成了"来源"，对下游消费者观感相同）。
2. **违背计划明文**：phase1b 计划 §1.2 写的是"journey-<id>.json 可以选择性地包含 LLM Finding（通过 -llm-addr flag 控制）"，未实现。
3. **架空治理前提**：KNOWN_ISSUES §1.15 与路线图 9.3 的治理模式是"外部脚本消费 `vmr-report.json`/`journey-*.json` 这两个稳定数据契约"——LLM Finding 在 JSON 契约里不可见，外部脚本路线实际无法消费它们（本轮 `_eval` 是靠直接 import `story` 包绕过的，下次外部 Python 脚本就没这条路了）。

**处置建议**（需拍板，二选一）：(a) `JourneySummary` 增设独立的 `LLMFindings []Finding` 字段（或 `writeJourneyFile` 增参），保持与规则 Findings 分离、随 `-llm-addr` 出现——契约完整且不污染规则层数据；(b) 明确决策"LLM Finding 只进 .md 不进 .json"，同时修订 phase1b 计划 §1.2 与 `ComputeFindings` 注释里的不变量表述，并在 UserGuide 写明。当前"既没实现也没声明"的状态是最差的一种。

**发现（3-2，中）：E4 候选序不确定（map 遍历），缓存与可复现性双重受损。**
`detectOscillationCandidates` 的 `for toolName, calls := range toolCounts` 依赖 Go map 随机遍历序。实证：同一 Journey 跑 200 次，得到 2 种候选排列（`toolA,toolB,toolC` 与 `toolB,toolA,toolC`）。候选序直接进入证据包 JSON → 进 `cacheKey` 哈希 → 进 prompt。后果：(a) 同一 Journey 每次运行缓存键都不同，`-llm-cache-dir` 对 E4 基本永远 miss，白付重复调用；(b) 相同输入产生不同 prompt，LLM 判定不可复现。**这与 Phase 1a 在 `corpus_sequence.go` 修过的问题是同一类**（phase1a §5.1 修复 #3，当时实测三次运行顺序翻转），修复模式（排序 tiebreak）没有带到新代码里。修复成本一行：按首个 StepSeq（或工具名）对 `out` 排序。

**发现（3-3，中低）：合并后的 Findings 没有全局重排。**
`cmd_story.go` 的 `renderJourney` 里 `findings = append(findings, llmFindings...)`——规则块（StepSeq 有序）后接 LLM 块（内部有序），整体不是 StepSeq 序，渲染出的 Findings 列表呈"两段式"。确定性没问题，但与 `ComputeFindings` 自己的排序约定不一致。补一次同比较器的 `sort.SliceStable` 即可。

**发现（3-4，低）：MEDIUM/LOW 被整段丢弃，与书面契约不符。**
计划 §1.1 的流程图与路线图 9.3 决策原文都是"MEDIUM/LOW 降级为 LLM 解读段落内的**参考提示**，不计入 Finding 统计"；实现是直接丢弃（六个判别器一致）。现行做法更保守（更安全），但与已写进 KNOWN_ISSUES 决策记录的措辞不符。要么在渲染层把 MEDIUM/LOW 以"辅助解读"形式呈现（不计统计），要么修订决策记录措辞为"低于 HIGH 一律丢弃"。

**发现（3-5，低）：fail-open 完全静默。**
六个判别器的所有错误（网络、非 2xx、JSON 解析失败）一律吞掉、无任何 stderr 输出；而同一函数里紧随其后的整体解读调用失败时会打 "warning: LLM interpretation failed..."。用户配错 `-llm-addr` 时看到的是"报告正常生成、没有 AI 推测 Finding、只有一条解读失败的警告"——无法区分"检测过且无发现"与"检测根本没跑成"。建议 `ComputeLLMFindings` 聚合输出一条 stderr 汇总（如 "5/6 detectors skipped: last error: ..."）。

**发现（3-6，低）：i18n 惯例被同一提交内的两种做法分裂。**
`internal/i18n/story_findings.go` 为四个新 Finding 类型补了规范的中英文闭包（ToolResultMisinterpretation/SemanticOscillation/GoalDrift/UnverifiedCompletionClaim），但同一提交里：P1b.4 的 `detectLLMConstraintDropped` 用内联 `if lang != i18n.ZH {...}` 手拼 finding/action 文案；`render_inferred.go` 硬编码 `[AI推测 · 置信度: ...]`、`[规则检测]`、`原文证据锚点：`。CLAUDE.md 模块表对 `i18n` 的定义是"EN/ZH text for **every** report/story output string"。应统一收进 i18n（`ConstraintDropped` 闭包 + SpineText 的锚点/推测标签）。

**发现（3-7，低）：LLM 返回的 `step_seq` 未校验。**
P1b.1/P1b.2/P1b.4 直接采信 LLM 返回的 `step_seq` 构造 Finding；模型幻觉出一个不存在的步号时，Finding 指向不存在的 Step（工具名查找静默回退 `""`）。锚点已有机械校验路径（校准脚本），但步号没有。建议：非候选集合内的 `step_seq` 直接丢弃该条。

**观察（不计缺陷）**：
- E5 检查点采样为 `i%3==0` + 首/末/HumanInitiated，计划写 K=5——更密的采样，无碍，但属未声明的偏差；
- `-llm-dry-run` 只估算整体解读的证据包，不含六个判别器的调用（真实运行最多 7 次调用），估算系统性偏低——既然 dry-run 的定位就是"要不要跑"的成本预估，建议把判别器调用的条数/估算也打出来；
- `extractRootUserIntent` 里 `t, _ := truncateText(...)` 遮蔽外层循环变量 `t *Task`（因立即 return 无害，纯可读性）；`extractFinalOutcome` 与 P1b.6 内部逻辑重复；
- `SuspiciousPairs` 无条件进入每个单 Journey 证据包（最多 10 对 × 约 1.3KB），即使用户只想要整体解读也会付这部分 prompt 成本——设计使然，知情即可；
- 串行 7 次调用、单次 120s 超时的最坏耗时问题已由用户拍板维持现状（§7.4.3），不再列。

**过程性评价（治理顺序）**：路线图 9.3 与 KNOWN_ISSUES §1.15 的治理规则是"先外部脚本验证、校准达标后再合入"。`c61c2f6` 把六个判别器直接合进了 `internal/story` 生产路径，校准只做了 6 个真实 Journey 的抽查（KNOWN_ISSUES 1.18 已如实登记差距）。缓释因素是实在的：`-llm-addr` 纯 opt-in、默认输出零变化、`Source`/`[AI推测]` 标注使 LLM 判定在呈现层不可冒充规则事实、`ComputeFindings` 未被触碰、稳定 JSON 契约反而因发现 3-1 而未被污染。**可以接受，但应认识到这是对既定顺序的一次倒置**——若后续按 3-1(a) 方案把 LLM Finding 写进 JSON 契约，"先校准后入契约"的门槛就必须重新从严执行（那才是 §1.15 真正要守的边界）。

---

## 4. 跨领域与仓库惯例问题（两阶段共有）

**发现（4-1，确定违规）：`CHANGELOG.md` 的 `[Unreleased]` 没有任何 Phase 1a/1b 条目。**
七个提交带来了实打实的用户可见变化：第 14 项行为指标、corpus 两个新章节、实体提取口径变化、四个新 Finding 类型、LLM 判别器管线。CLAUDE.md 惯例明确要求"user/developer-visible changes 落地时同步写 `[Unreleased]`"。最后一次 CHANGELOG 更新停在 Phase 1a 之前的 `c7f8ca2`。**这是本轮复核中唯一一处无可争辩的流程违反，应尽快补齐**（补写不影响已提交代码）。

**发现（4-2，中）：`docs/UserGuide.md` / `.zh` 未同步更新。**
- UserGuide 仍写 "each of the **thirteen** behavior-profile numbers"——`metricSpecs` 现在是 14 项；
- `vmr story -llm-addr` 一节仍描述旧行为（"never inventing a new issue outside that list"），未提六个判别器、`[AI推测]` Finding、以及"每次运行最多 7 次 LLM 调用"的成本变化；
- corpus 章节未提两个新小节。
CLAUDE.md 要求 UserGuide 与 `.zh` 在用户可见变化的同一提交内更新，且两份计划文档自己的收尾清单也没列这一项——计划本身的疏漏。

**发现（4-3，文档）**：phase1b §6.1 以"KNOWN_ISSUES §2.5"这类章节号做跨文档引用（§7.3 已自我注意到了，未改）；phase1a §4 自评行数与实测漂移（§5.3 已披露）。均已知，登记不展开。

---

## 5. 问题与建议汇总表

| # | 阶段/位置 | 严重度 | 问题 | 建议 |
|---|---|---|---|---|
| 3-1 | P1b / `writeJourneyFile`+`Summarize` | **中高** | journey JSON 不含 LLM Finding，.md/.json 分叉，违背计划 §1.2 与既有不变量，架空"外部脚本消费 JSON 契约"治理前提 | 拍板：`JourneySummary` 加独立 `LLMFindings` 字段，或明文决策 .md-only 并修订计划/注释/UserGuide |
| 3-2 | P1b / `detectOscillationCandidates` | **中** | map 遍历导致候选序不确定（实测 200 次 2 种排列）→ E4 缓存永不命中、判定不可复现；与 P1a.5 修过的同类问题 | 对候选按首 StepSeq/工具名排序（一行） |
| 4-1 | 全局 / `CHANGELOG.md` | **中**（确定违规） | 七个提交零 CHANGELOG 条目 | 按 Added/Changed 补写 `[Unreleased]` |
| 4-2 | 全局 / `UserGuide.md(.zh)` | **中** | "thirteen" 计数过期、`-llm-addr` 行为描述过期、新章节未提 | 同步更新中英文两份 |
| 2a-1 | P1a.1 | 中低 | report 半区回归空转（所用语料无 Compaction，`SwallowedEntities` 全空） | 用含 Compaction 的语料/fixture 真正跑一次 |
| 2d-1 | P1a.4 | 中低 | `computeContextRot` 重复计算 `ComputeFindings`（`ComputeCorpusStats` 已算过） | 传入 `findingsPerJourney` 复用 |
| 3-3 | P1b / `cmd_story.go` | 中低 | 合并 LLM Finding 后未全局重排，Findings 列表两段式 | 合并后补一次 `sort.SliceStable` |
| 2c-1 | P1a.3 | 中低 | 调查数字（495,672/0.00%）不可复现，脚本已删；与 `_eval` 保留策略不一致 | 治理层面统一"调查脚本留痕"惯例 |
| 3-4 | P1b | 低 | MEDIUM/LOW 整段丢弃 vs 书面契约"降级为参考提示" | 实现降级呈现或修订决策记录措辞 |
| 3-5 | P1b | 低 | 判别器错误全静默，与整体解读的告警不对称 | 聚合一条 stderr 汇总警告 |
| 3-6 | P1b | 低 | P1b.4 与 render_inferred 内联中英文案，绕开 i18n 惯例 | 收进 `i18n` 包 |
| 3-7 | P1b | 低 | LLM 返回 `step_seq` 未校验，幻觉步号产生悬空 Finding | 校验在候选集合内，否则丢弃 |
| 2d-5 | P1a.5 | 低 | `count >= 1` 死过滤 + i18n"频次阈值"文案无对应实现 | 统一（加阈值或改文案） |
| 2f-1 | P1a.7 | 低 | `StepTagPlan` 语义收紧（单行列表不再标 Plan）未在任何文档声明 | 加注释声明有意收紧 |
| 2a-2 | P1a.1 | 低 | 专有名词噪音实体（GitHub/macOS/U.S.A）实测存在 | 包注释登记已知噪音面 |
| 2d-2/2d-3/2d-4 | P1a.4 | 低 | 区间注释错（inclusive vs 半开）、无 Manifest Step 入 0 桶、ErrorRate 偏差说明不完整 | 改注释/跳过无 Manifest Step/补偏差脚注 |
| 2b-1 | P1a.2 | 低 | 引号内空白折叠、重复 JSON 键合并未注释 | 加一行"why"注释 |
| 2e-1 | P1a.6 | 低 | n-gram 跨拼接点无分隔 | 注释登记 |
| 2a-4 / 4-3 | 文档 | 微 | 执行摘要归因/行数/章节号引用类瑕疵 | 已被两份文档的复核章节部分自披露，随下次触碰顺手修 |

**明确核实无问题、不建议再怀疑的点**（避免下一轮重复劳动）：P1a.2 的 UseNumber 精度修复与接入点唯一性；P1a.7 的六类格式解析与跨格式"取最后一段"语义（实测通过）；P1a.5 的确定性 tiebreak（实测 `-corpus` 两轮逐字节相同）；P1b 的 fail-open、`ComputeFindings` 纯度、prompt 版本化/缓存接线、混源标签方案、P1b.5 动态重规划基线修复、`_eval/calibrate_p1b.go` 的真实性、KNOWN_ISSUES 两条决策记录；archtest/gofmt/test 全量绿。

---

## 6. 总结

**Phase 1a**：七个任务与两份计划（含全部核实修订）高度吻合，此前复核修复的 4 缺陷 + 1 设计缺口确认真实在位且有回归测试锁住；真实语料复跑的全部数字（11 journey、58 lineage、rho=0.77、重复率 9%/15%）与文档自报一致。剩余问题集中在"验收动作走了但没打到靶心"（2a-1 的 report 回归空转）与若干注释/文案级瑕疵，无正确性缺陷。

**Phase 1b**：架构契约（来源标记、离散置信度、锚点门禁、fail-open、纯规则层不动、渲染区分）落地完整且测试覆盖扎实；此前复核发现的自证循环校准已被替换为真校准工具并留下诚实的边界声明。本轮新发现的问题里，**3-1（JSON 契约缺 LLM Finding）是唯一影响数据契约层面的缺口**，需要一次明确拍板；3-2（E4 不确定序）是 Phase 1a 已修过的同类问题的复发，修复成本极低；其余为呈现/惯例层面的打磨。过程上，"先合入、后补完整校准"与治理规则的既定顺序发生了倒置，但缓释措施（opt-in、标注、规则层隔离、KNOWN_ISSUES 1.18 自我登记）使其可控——前提是 3-1 的修复方向选择 (a) 时，把"校准达标才能进 JSON 契约"重新立为硬门槛。

**最优先的三件事**：① 拍板并处理 3-1；② 修复 3-2（一行排序）与 4-1（补 CHANGELOG）；③ 用含 Compaction 的语料补一次真正的 P1a.1 跨半区回归。
