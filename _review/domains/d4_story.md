<!-- Ver 2026-09-02, by S4 (D4+D5 Review Agent) -->

# Domain Review 报告：D4 分析·叙事域 & D5 共享解析域 (S4)

> **审查范围**：
> - **D5 共享解析域**：`internal/ctxgraph/`（内容寻址、编辑分类、lineage 图、缝合与缓存）、`internal/chatmsg/`（多协议消息/SSE/用量/配对/token 估算）、`internal/taskseg/`（Agent 方言 Profile、任务切分算法）。
> - **D4 分析·叙事域**：`internal/story/`（叙事构建、决策脊柱、指标计算、Findings 检测、严苛度评级、PONR、LLM 解读层、Compare 对比、Corpus 语料统计）、`internal/i18n/story_*.go`、`cmd/vmr/cmd_story*.go`、`cmd/vmr/taskprofile.go`。
> - **参考设计与权威清单**：`docs/VirtualModelRouter_Design_v4_Analytics.md`、`docs/KNOWN_ISSUES.md`。

---

## 0. 领域审查总览与核心结论

1. **架构契约与单向依赖严格成立**：
   - `ctxgraph`、`chatmsg`、`taskseg`、`story` 均未引入对 `router`、`server`、`config` 的反向依赖（`archtest` 可执行守卫背书）。
   - 分析半区与路由半区仅通过 `audit.Record` JSONL 契约解耦，无内存共享或运行时双向状态渗透。
2. **SSOT 唯一权威性高度收敛**：
   - **消息哈希**：`ctxgraph.BuildManifest`（`hash.go:72` / `manifest.go:192`）是消息内容寻址的唯一源头。
   - **任务/会话切分**：`taskseg` 作为 `report` 和 `story` 共享的唯一实现彻底成立（`cmd/vmr/taskprofile.go:16` 统一解析）。
   - **降级 Token 估算**：`chatmsg.EstimateDegradedTokens`（`tokenest.go:44`）统一了无 upstream usage 时的估算基数，消除了两半区此前各自估算导致的金额分歧。
3. **确定性与防御性设计完备**：
   - `ctxgraph.StitchGraph` 实现了严格的覆盖率比例 + 绝对下限（`stitchMinAbsOverlap=3`）+ 双时间窗（同桶 72h 降级 / 跨桶 6h 淘汰）+ 三级排序确定性决平（`stitch.go:240`），杜绝了 Go map 遍历随机性导致的非幂等问题。
   - `story.pickDriver` 强制排除 `SourceLLMInferred`（`severity.go:76`），彻底杜绝转录本作者通过 Prompt 注入操纵看板顶级判词的安全风险。
   - LLM 判别器具备强运行期硬护栏：逐字 `EvidenceAnchor` 核验（`llm_findings.go:415`）、非合法 step clamp 丢弃、输出 Markdown 结构字符深度转义（`sanitizeMDStruct`）、全链路失败 fail-open 优雅降级。

---

## 一、分任务审查结论与源码证据

### 任务 1: 内容寻址与血缘（`internal/ctxgraph/`）

#### 1.1 消息内容哈希与 SSOT 权威性
- **消息哈希实现**：`hash.go:66-77`（`hashJSON` / `hashMsgJSON`）。对消息结构序列化（`encoding/json` 保证 key 排序）后计算 MD5 摘要（`[16]byte`）。
- **Anthropic `cache_control` 剥离机制**：`hash.go:79-114`（`containsCacheControl` / `stripCacheControl`）。深拷贝并丢弃任意深度的 `cache_control` 断点元数据，防止客户端轮次移动断点引发伪编辑（Append 误判为 Contract/Splice）。
- **消息哈希高速缓存**：`manifest.go:207-268`（`hashMsgJSONCached` / `fastRawDigest`）。采用 16 分片 RWMutex + 128-bit 非内存分配指纹算法，针对 Map 遍历无序性做了加法与 XOR 组合处理，避免长会话 O(N²) 序列化重算开销。
- **System Prompt 哈希权威性**：`manifest.go:189-194`（`m.SysHash = md5.Sum([]byte(LeadingSystemText(msgs, m.LeadSys)))`）。前导 system 块直取原始字节摘要，`report/session.go` 与 `story/journey.go` 均直接复用该哈希及 `LeadingSystemText`（`manifest.go:198`），无重复私有实现。

#### 1.2 编辑分类器（`edit.go`）
- **分类算法**：`edit.go:90-112`（`Classify`）。
  - `Append`：`LCP == len(prev.Keys)` 或尾部微小差异（`tailSlack=2`）。
  - `Contract`：`len(cur.Keys) < len(prev.Keys) * 0.6`（分裂 lineage）。
  - `Fork`：`Coverage < 0.5`（分裂 lineage）。
  - `Splice`：非收缩且 `commonSuffixLen >= 2`（`spliceMinTailMatch`，保留 lineage 并打上 `Event.Revises` 标记）。
  - `ReplaceTail`：尾部分歧且无公共后缀重现（保留 lineage）。
- **结构化纯粹性**：纯集合与前缀计算，无 Agent 模板与标记依赖，符合第一性原理。

#### 1.3 Lineage 与跨 Lineage 缝合（`lineage.go`, `stitch.go`）
- **Lineage 身份**：`lineage.go:103-120`（`Lineage.LineageID()`）。采用 `\"l-\" + md5(root_sys_hash + root_keys + root_ts_nanos)[:8]`，结合了内容寻址与纳秒时间戳，解决模板化定时心跳请求内容碰撞问题。
- **全图缝合逻辑**：`stitch.go:144-269`（`StitchGraph` / `resolveStitch`）。
  - 倒排索引：`buildBlobLineageIndex`（`stitch.go:173`）构建 `map[Hash][]int`，在遍历时即时去重，大幅压降内存。
  - 集合交判定：`resolveStitch`（`stitch.go:210`）对 `b0.Keys` 去重后统计与前驱的共享消息数（`distinct`），杜绝重复消息膨胀分子分母。
  - 严苛门禁：
    - 比例门槛：`stitchCompactionScore = 0.5`（Contract）、`stitchHeadPruneScore = 0.15`（Fork）。
    - 绝对下限：`stitchMinAbsOverlap = 3`（`stitch.go:88`），不满足时降级为 `AmbiguousMatch`。
    - 跨桶时间窗：`stitchCrossBucketMaxGap = 6h`（`stitch.go:114`），超窗直接淘汰。
    - 同桶时间窗：`stitchSameKeyMaxGap = 72h`（`stitch.go:101`），超窗降级为 `AmbiguousMatch`（保留边供人工复核，不自动缝合）。
  - 确定性三级决平：`score > bestScore` -> `gap < bestGap` -> `idx < bestIdx`（`stitch.go:240-244`），彻底消除 Go map 迭代随机性引起的非幂等问题。

#### 1.4 解析缓存机制（`cache.go`）
- **版本门禁**：`cache.go:39`（`CacheSchemaVersion = 6`）。明确约束版本不匹配即全量重新解析。
- **分片存储与原子写入**：`cache.go:204-263`（`SaveCacheDir` / `writeCacheShardAtomic`）。采用 `<hash>.json` 分片，CreateTemp + Rename 保证写入原子性。
- **路径重绑定**：`cache.go:157-164`。命中缓存后自动将 `Manifest.Path` 重绑定为当前运行路径，`Manifest.Req` 保持规范化坐标。

---

### 任务 2: 消息解析（`internal/chatmsg/`）

#### 2.1 多协议消息与 SSE 还原（`messages.go`, `sse.go`）
- **协议覆盖**：
  - OpenAI Chat Completions：`messages` 数组、`choices[0].delta`、`tool_calls`。
  - Anthropic Messages：顶层 `system`（合成 message #0）、`message_start`/`content_block_*`、`tool_use`/`tool_result` 内容块。
  - OpenAI Responses：顶层 `instructions`（合成 message #0）、`input` 数组/裸字符串、`function_call`/`function_call_output`/`reasoning` Item（`messages.go:166-189` `responsesItemMessage`）。
- **Responses SSE 策略**：`sse.go:102-125`。明确只解析 `response.completed` 终端自包含事件，复用 `responsesFinalMessage`，避免猜测未定型的 delta 字段造成静默错误。

#### 2.2 用量解析（`usage.go`）
- **协议感知解析**：`usage.go:65-180`（`ExtractUsageWithProtocol` / `usageFromObj`）。
  - Anthropic：`In = input_tokens + cacheRead + cacheWrite`（加法模型）。
  - OpenAI Completions：`In = max(prompt_tokens, input_tokens)`（包含模型）。
  - OpenAI Responses：`In = input_tokens`（包含模型）。
- **非负 Fresh 保证**：`usage.go:38-44`（`Usage.Fresh()`），计算 `In - CacheRead - CacheWrite` 时下限截断为 0，防止脏数据导致负数。

#### 2.3 降级 Token 估算的不对称性设计（`tokenest.go`）
- **实现位置**：`tokenest.go:44-54`（`EstimateDegradedTokens`）。
- **不对称性机制**：
  - **请求侧**：`EstimateRequestBodyTokens`（`usage.go:264`），回退到原始 JSON 字节估算，严格对齐路由半区 `Facts.EstimatedTokens` 扣额基数。
  - **响应侧**：`EstimateResponseBodyTokens`（`usage.go:273`），仅从提取的有效文本估算；对于截断 SSE、二进制 opaque 等无法解析文本的场景严格返回 0。杜绝了把传输信封当生成文本计算导致的 71 倍虚高（Q04 缺陷修复基准）。

#### 2.4 工具调用配对与结果解码（`pairing.go`, `toolresults.go`）
- **F9 因果配对验证**：`pairing.go:33-87`（`CheckToolPairing`）。严格核验请求体内 `tool_call` 与 `tool_result` ID 的 1:1 双向映射，覆盖三大协议。
- **跨步结果提取**：`toolresults.go:29-65`（`ToolResultList`）与 `story/findings_toolresult.go:17-48`（`toolResultsFor`）。从第 `i+1` 步请求中反查第 `i` 步的工具应答，支持两阶段 ID 归一化（精确匹配 -> 下划线剥离 `NormalizeToolCallID`）。

---

### 任务 3: 任务切分（`internal/taskseg/`）

#### 3.1 双 Profile 设计（`openclaw.go`, `generic.go`）
- **`Profile` 接口定义**：`taskseg.go:9-21`，统一定义 `RealUserText`、`NoReply`、`ChatID`。
- **`OpenClawAware`**：
  - 运输脚手架过滤：精准识别 `OpenClaw runtime context`、`Attached image(s)`、`The conversation history before this point was compacted` 等前缀（`openclaw.go:28-36`）。
  - 元数据信封剥离：`stripOpenClawEnvelope`（`openclaw_brackets.go:15`）去除 `(untrusted metadata)` 并清洗时间戳/消息 ID 括号。
  - 纯工具结果排除：当用户消息的所有 content parts 均为 `tool_result` 时判定为非指令（`openclaw.go:50-65`）。
  - `NoReply` 判别：`stop`/`end_turn` 且内容为空或以 `NO_REPLY` 结尾（`openclaw.go:74-82`）。
- **`Generic`**：通用兜底，非空即真实指令，空回复即 NoReply。

#### 3.2 共享任务切分算法（`segment.go`）
- **单次索引与内存防膨胀**：`segment.go:48-59`（`IndexRealUsers`）。每条记录只扫描构建一次 `RealUsers map[int]string`，且在存入前即执行 `Preview` 截断，避免 Go 子字符串切片引用导致整段大消息常驻内存。
- **新指令判定**：`segment.go:87-97`（`HasNewInstruction`）。必须同时满足 `idx >= deltaStart`、处于尾部窗口（`idx >= total - chatmsg.NewUserWindow`，即 8 条内）、且内容哈希不在父级历史 `prevKeys` 集合中（防止历史局部裁剪导致旧消息位移被误判为新指令）。
- **新任务切分**：`segment.go:144-146`（`IsNewTask(traceChanged, prevNoReply, hasNewInstr)`）。
- **SSOT 唯一性核实**：`report/session.go`（479/686/693 行）与 `story/journey.go`（384/385/510 行）均直接调用 `taskseg`，无任何私有切分副本。

---

### 任务 4: 叙事重建（`internal/story/` 核心与渲染）

#### 4.1 叙事数据结构（`journey.go`）
- **三层层次模型**：`Journey -> Task -> Step` + 全局首次出现去重事件流 `Event[]`。
- **Step 内存轻量化**：`Step`（`journey.go:54-120`）仅持有 `Manifest` 及 `fillStepFacts`（`journey_stepfacts.go:14`）抽取的结构化事实（`ContextPoint`、`Attempts`、`NewToolResults`、`SysChars`），完全脱钩 `*audit.Record`。全部 Journey 常驻内存压降至约 300MB。
- **缝合边界与任务边界正交**：`journey.go:460-475`。缝合边界（Compaction）默认保留在当前 Task 内，仅在 `newInstructionTitleAtStitch`（`journey.go:743`）发现全新真实指令时才开启新 Task，避免 Task 数量虚高。

#### 4.2 决策脊柱渲染（`render_spine*.go`）
- **概览卡**：`render_spine.go:81-125`（`renderOverviewCard`）。输出 3-5 个关键转折节点、结构化标签、失败步数统计、定价成本。
- **决策脊柱**：`render_spine_step.go:124-156`（`renderDecisionSpine`）。
  - Step 角色标注：`render_spine.go:132-152`（`stepRoleTag`），优先级为 🧹压缩 > ⚠️错误 > 🔄重试 > 🔧执行 > 📋规划 > 💬汇报 > 👀观察。
  - 模型意图展示：`render_spine_step.go:46-56`（`spineWhyLine`），优先展示 `RespText`，无回复但有推理时展示 `> 🤔 `（带 `<details>` 折叠）。
  - 参数与结果渲染：`render_spine_args.go:44-93`（`toolCallLine`）与 `render_spine_step.go:78-93`（`toolResultLine`）。结构化展示参数，超长内容折叠至多 3000 字符，配对结果展示 ↩️ 或 ❌。
  - 详单链接模式切换：根据 `linkDetails` 渲染为 `details/*.md` 链接或行内 `文件:行` 坐标（`Manifest.Req`），杜绝 404 死链。

#### 4.3 行为指标与模型使用（`metrics.go`, `modelusage.go`）
- **九项行为指标**（`metrics.go:21-75`, `ComputeMetrics`）：
  1. `ModelMS` / `AgentExecMS` / `HumanIdleMS` / `NetWorkingMS`（基于 `HumanInitiated` 的 F10 三路时间分解）。
  2. `ModelToToolRatio`。
  3. `ToolCallCount` / `ToolCallDist`。
  4. `DuplicateActionRate`（`toolCallRepeats` 底层统一判据）。
  5. `OutputRepetitionRate`（4-gram 文本冗余度）。
  6. `ErrorRecoveryCount`（收到 `is_error` 后的工具调用恢复计数）。
  7. `PlanExecRatio`（无工具纯文本 Step 比例）。
  8. `ContextCurve`（按 Step 拆解 System/User/Assistant/Tool Token 堆叠曲线）。
  9. `ContextUtilization`（上下文实体后续被引用 Token 比例）。
- **模型使用与切换**：`modelusage.go:21-65`（`computeModelUsage`）。严格从 `Step.Attempts` 提取物理 `(Provider, Model)`，避免取虚拟模型名导致切换数据失真；切换点记录 `OnFailoverStep` 标记。

#### 4.4 Findings 检测体系与严苛度评级（`findings*.go`, `severity.go`, `pointofnoreturn.go`）
- **规则层 9 大检测器**（`findings.go`, `findings_toolresult.go`）：
  1. `exact_repeat_tool_call`（重复调用 ≥ 3 次）。
  2. `narration_without_action`（连续 ≥ 3 步纯文本且 Jaccard 相似度 ≥ 0.5）。
  3. `error_then_unverified_success`（报错后未调用验证类工具即结束任务）。
  4. `reasoning_action_mismatch`（推理最后一句实体未在动作中出现）。
  5. `plan_execution_misalignment`（任务首步编号计划与后续执行实体脱节）。
  6. `error_retry_unadapted`（报错后 5 步内参数未改动重试）。
  7. `unused_tool_result`（工具返回的所有实体后续均未被引用）。
  8. `unverified_entity_reference`（命中证伪标记的实体后续仍被引用）。
  9. `constraint_text_dropped_at_compaction`（压缩边界吞噬实体）。
- **严苛度与驱动归因安全机制**：
  - 分级：`SeverityClean` / `SeverityWarning` / `SeverityCritical`（`severity.go:13-17`）。
  - 核心安全防线：`severity.go:73-95`（`pickDriver`）。**强制过滤 `SourceLLMInferred`**，防止被分析的转录本作者通过 Prompt 注入操纵看板顶部判词；低置信度检测项（`unverified_entity_reference`）降级处理。
- **不可逆转折点（PONR）**：`pointofnoreturn.go:54-94`（`ComputePointOfNoReturn`）。从 Compaction 约束丢失、无适应重试闭环、Contract 历史重组中取最早发生的 StepSeq。

---

### 任务 5: LLM 解读层（`internal/story/llm*.go`）

#### 5.1 架构与可选降级设计（`llm.go`）
- **单向只读契约**：LLM 仅消费规则层已经算好的事实（`EvidencePack` / `SingleJourneyEvidencePack` / `DivergenceEvidencePack`），Prompt 严禁模型编造新数字。
- **极简独立传输**：`llm.go:179-245`（`Interpret`）。基于标准 `net/http` POST 直连目标 VMR 虚拟模型，超时 120s，零 SDK 依赖，不反向依赖 router/adapter。
- **磁盘缓存**：`llm.go:164-177`（`cacheKey`）。在 `.llm-cache/` 中按 SHA-256(evidence_json + model + prompt_version) 缓存，支持 `-llm-dry-run` 预估。
- **优雅降级**：任何网络超时、模型拒绝或 JSON 解析异常均静默记录并在控制台打印警告，完全不阻断 Markdown / HTML 报告的正常生成与输出。

#### 5.2 语义判别器与安全护栏（`llm_findings.go`）
- **6 大 LLM 判别器**：覆盖工具误读、决策振荡、目标漂移、压缩约束丢失、计划脱节、虚假交付。
- **硬核防幻觉核验**：`llm_findings.go:415-420`。收集 LLM 输出后，必须满足 `strings.Contains(transcript, item.EvidenceAnchor)` 逐字匹配，且 `StepSeq` 处于当前 Journey 合法步数范围内（越界直接丢弃而非 clamp）。
- **Markdown 结构转义**：`llm_findings.go:440-475`（`sanitizeMDStruct`）。对 LLM 生成文本转义反引号、竖线、标题、列表项、引用符等结构符号，防止破坏外层表格或文档布局。

---

### 任务 6: Compare 与 Corpus 分析（`internal/story/compare*.go`, `corpus*.go`）

#### 6.1 双 Journey 对比（`compare.go`, `compare_metrics.go`）
- **指标做差**：`compare.go:68-75`（`Compare`）。对比 14 项行为指标，相对变化 ≥ 30% 且超过绝对噪声下限时标记 `Notable`。
- **全景事实对比（Extras）**：`compare.go:110-230`（`ComputeComparisonExtras`）。包含端点差异、Cache 命中曲线、SysPrompt 演化与节选、最终上下文构成、总耗时与终止状态、最终交付物对比（`DeliverableFact`，提取参数像写文件的末次工具调用）、估算 spend（`CostPair`）。
- **结构化分叉点定位**：`compare.go:300-350`（`computeDivergence`）。对齐展平步骤，按工具集合与参数判定首个分歧点（`DivergenceHeavy` / `DivergenceLight`），配合 `llm_divergence.go` 进行可选语义归因。

#### 6.2 语料级宏观统计（`corpus.go`）
- **无假设统计口径**：
  - 分布：`Distribution`（Mean / Median / Min / Max / P90，`corpus.go:21-47`）。
  - 相关性：Spearman 秩相关仅报效应量 `rho`，不报 p 值（`corpus.go:88-125`），并在展示层自动过滤 `mechanicalCorrelationPairs`（如 NetWorkingMS 与 ModelMS 等定义保证相关的指标对，`corpus.go:73-86`）。
  - 分组对比门槛：`GroupComparison`（`corpus.go:140-195`）强制 `corpusMinCorrelationN=5` 与 `corpusMinGroupSize=3`，样本不足时显式记录于 `SkippedGroupComparisons`。
- **Context Rot 与时序演化**：
  - `corpus_contextrot.go:21-65`：按 4 档 Token 深度分桶，分析错误率、重试率随上下文膨胀的衰减趋势。
  - `corpus_sequence.go:18-60`：统计高频工具调用的 Bi-gram 与 Tri-gram 组合序列。
  - `corpus_coverage.go:18-50`：统计工具声明集与实际调用集的覆盖率。

---

## 二、领域级问题汇总（按严重性）

| 编号 | 严重性 | 涉及文件与位置 | 问题简述 | 影响与现状评估 |
| --- | --- | --- | --- | --- |
| **D4-1** | **[中]** | `internal/story/llm_findings.go:1-400` | Phase 1b 6 个 LLM 语义判别器尚未完成系统性黄金样本标注校准（对应 KNOWN_ISSUES §2.18） | 单测与真实日志抽查有效，但缺乏 30~50 个 Journey 的系统性 Precision/Recall 数据。仅在 `-llm-addr` 显式开启时运行，不影响规则层基线。 |
| **D4-2** | **[低]** | `internal/story/metrics.go:154-165` | `computeTimeSplit` 单间隙时间归因无上限，污染 corpus 均值（对应 KNOWN_ISSUES §2.57） | 跨天/长休眠会话的巨大 gap 会计入 `AgentExecMS` 或 `HumanIdleMS`，拉偏 Mean 指标。目前已在 Markdown 增加脚注引导看 Median/P90。 |
| **D4-3** | **[低]** | `internal/story/metrics.go:275-310` | 「上下文有效利用率」（Context Utilization）呈现双峰退化（对应 KNOWN_ISSUES §2.64） | 约 21% 样本为 0（无工具），32% 样本为 1.0，中间值稀疏，中位数 95% 缺乏统计区分度。属于指标语义精细化演进项。 |
| **D4-4** | **[低]** | `internal/story/llm_findings.go:230-245` | LLM `goal_drift` 可能将漂移锚点错误定位至 Step 1（对应 KNOWN_ISSUES §2.53） | `buildGoalDriftPack` 包含 `i == 0`，LLM 偶发返回 `DriftStepSeq: 1`。需增加 `DriftStepSeq > 首步` 的后验护栏。 |
| **D4-5** | **[低]** | `internal/story/render_compare.go:320-330` | `renderInitialInstruction` 两侧初始指令逐字相同时未合并（对应 KNOWN_ISSUES §2.59 剩余部分） | `renderSysPrompt` 已合并完全相同节选，但 `renderInitialInstruction` 仍无条件渲染 A、B 两份相同文本。 |
| **D4-6** | **[低]** | `internal/story/mdlite.go:145-155` | `mdInline` 在已包裹 `<code>` 内若包含 `**` 会注入 `<strong>`（对应 KNOWN_ISSUES §2.51） | 状态机分步处理导致。经 `html.EscapeString` 前置转义，无 XSS 风险，仅极罕见展示瑕疵。 |

---

## 三、KNOWN_ISSUES §2 及分析半区声明验证结果

| KNOWN_ISSUES 条目 / 声明 | 源码位置 / 机制验证 | 验证结论 |
| --- | --- | --- |
| **§2.18 Phase 1b 判别器未完成完整黄金样本校准** | `internal/story/llm_findings.go`, `_eval/calibrate_p1b.go` | **已验证**。判别器逻辑完备且单测覆盖，但大规模黄金测试集标注受限于人力投入，状态声明属实。 |
| **§2.53 LLM `goal_drift` 定位 Step 1** | `internal/story/llm_findings.go:232` (`if i == 0 ...`) | **已验证**。Step 1 确实作为 checkpoint 送入 prompt，缺乏 `DriftStepSeq > 1` 护栏，状态声明属实。 |
| **§2.57 `computeTimeSplit` 单间隙无上限** | `internal/story/metrics.go:157` (`gap := s.Manifest.TS.Sub(prevEnd).Milliseconds()`) | **已验证**。未设 1h 上限，长休眠会话整段计入 AgentExecMS/HumanIdleMS，状态声明属实。 |
| **§2.64 上下文有效利用率双峰退化** | `internal/story/metrics.go:275` (`contextUtilization`) | **已验证**。全实体粗粒度匹配导致无工具任务恒 0、全引用任务恒 1.0，状态声明属实。 |
| **§2.65 anthropic `is_error` 信号未在生产语料触发** | `internal/story/findings_toolresult.go:50-100` | **已验证**。检测器依赖 `ToolResult.IsError`，语料为 0% Anthropic 时命中恒为 0，单测有覆盖，声明属实。 |
| **§2.68 crosscheck 夹具无 compaction 记录** | `cmd/vmr/cmd_story_report_crosscheck_test.go:47` | **已验证**。夹具使用两段式 Contract 模拟，无真实 body-sniffed summarization 记录，字节一致性由指纹机制保证，声明属实。 |
| **§2.59 compare 相同节选未合并** | `internal/story/render_compare.go:294, 320` | **部分闭环/部分属实**。`renderSysPrompt` 已合并相同文本；`renderInitialInstruction` 仍无条件双份渲染。 |
| **§2.51 `mdlite.go` 代码块内 `**` 注入** | `internal/story/mdlite.go:146-150` | **已验证**。`mdInline` 先调 `mdWrap(\"`\")` 再调 `mdWrap(\"**\")`，确会产生标签嵌套，无安全问题，声明属实。 |
| **§2.22 ToolResultList 未覆盖 Responses function_call** | `internal/chatmsg/toolresults.go:59` | **纠正/已部分覆盖**。`ToolResultList` 已支持 `function_call_output`；`ToolCallList` 仍针对 completions 数组。语料为 0 条，声明属实。 |
| **§2.29 `journey-<id>.json` structure 无版本戳** | `internal/story/structure.go:15-30` | **已验证**。`structure` 字段未定义 `schema_version`，符合目前仅由 `_eval` 消费的 YAGNI 裁决。 |
| **§2.61 无任务达成目标硬信号** | `internal/story/corpus.go:13-16` | **已验证**。结构性零埋点前提下，仅拿 NetWorkingMS 作为弱代理，且在报告正文诚实披露限制，声明属实。 |
| **§2.55 `BuildAll` 一批物化 records map** | `internal/story/journey.go:225` (`FetchRecords`) | **已验证**。按 ~160MB 字节预算分批加载 map，批次间释放，常驻内存与语料规模解耦，声明属实。 |
| **§2.56 `analyze` 解压三遍语料** | `cmd/vmr/cmd_story.go`, `cmd/vmr/cmd_report.go` | **已验证**。ScanCached、PreviewTitles、FetchRecords 各自解压，内存优化后时间成为潜在优化点，声明属实。 |
| **§2.3 `chatmsg` 离线 `map[string]any` 分配** | `internal/chatmsg/messages.go`, `sse.go` | **已验证**。全在离线路径，实测 heap 放大仅 1.40x，维持“决定不做”判决合理。 |
| **§1.4 降级估算 fallback 刻意不对称** | `internal/chatmsg/tokenest.go:44` | **已验证**。请求侧 raw 字节 vs 响应侧 0 字节，经 `TestEstimateDegradedBasis_FallbackAsymmetry` 锁定。 |
| **§1.4 详情页字节一致性** | `cmd/vmr/cmd_story_report_crosscheck_test.go:56` | **已验证**。两端传入相同 `(record, manifest, prev)` 三元组，指纹机制折入身份，测试全绿。 |
| **§1.5 索引折叠仅把 heartbeat 归为噪声** | `internal/story/candidates.go` (`IsNoiseCategory`) | **已验证**。仅 `heartbeat` 折叠进 `<details>`，`cron` 与 `subagent` 平权展开并纳入渲染。 |
| **§1.3 默认分析套件不物化 `details/`** | `internal/story/render_spine_step.go:138, 165` | **已验证**。默认输出行内 `file:line` 坐标，`-details` / 单 `-journey` / `-render-all` 显式开启才写盘。 |

---

## 四、审查终结总结

D4（分析·叙事域）与 D5（共享解析域）代码实现严谨、测试覆盖完备、架构边界清晰。第一性原理（内容寻址、不可变快照差分、零侵入只读分析）在全部模块中得到了不折不扣的贯彻。全仓测试与 `-race` 竞态检测全绿，未发现安全漏洞或破坏架构红线的隐患。
