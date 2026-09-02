<!-- Ver 2026-09-02, by sub-agent (S3) -->

# 领域审查报告：D3 报表分析域 (Report & Analytics)

> **审查范围**：
> - `internal/report/`（聚合报表核心）
> - `internal/reqdetail/`（逐请求详单渲染与事实提取）
> - `internal/i18n/report_*.go`、`internal/i18n/reqdetail_detail.go`（双语渲染文案）
> - `cmd/vmr/cmd_report.go`、`cmd/vmr/reportconfig.go`（CLI 组合根及报表配置）
> - 共享依赖核查：`internal/ctxgraph/`、`internal/chatmsg/`、`internal/taskseg/`、`internal/pricing/`
>
> **代码基线**：`main@4e0d962` (2026-09-02)

---

## 0. 领域概览

报表分析域（D3）是 VMR（Virtual Model Router）分析半区的**宏观聚合引擎**与**微观详单下钻中心**。其核心定位是离线、只读地消费 `audit.Record` JSONL 审计日志，产出 `vmr-report.{json,md}`、`vmr-requests.{json,md}` 以及 `{out}/details/*.md` 详单。

在架构分层上，D3 严格遵循「两半区、单向契约」原则：
1. **零运行时侵入**：`internal/report` 与 `internal/reqdetail` 绝不依赖 `router`/`server`/`config`，完全通过 `audit.Record` 契约解耦（`archtest` 严格阻断反向依赖）。
2. **SSOT（单源真相）**：会话与任务拓扑完全建立在 `ctxgraph` 的 Manifest / Lineage / Stitch 图与 `taskseg` 的 Profile 切分之上，消除了历史上私有哈希向量与 LCP 搜索的重复逻辑。
3. **两层变焦统一**：宏观聚合（`report`）与中观叙事（`story`）共享微观详单叶子（`reqdetail`），通过确定性坐标哈希和指纹校验机制，保障跨命令调用的 Byte-Identical 输出。

---

## 1. 任务审查结论与源码级证据

### 任务 1: 会话与任务分组（`internal/report/session.go`, `internal/ctxgraph/`）

#### 1.1 并发扫描设计（两条 Goroutine 流水线）
- **实现位置**：`internal/report/session.go:142-202` (`AnalyzeSessionsCached`)
- **设计审查**：
  - `AnalyzeSessionsCached` 采用了双路并发机制：
    1. **拓扑图构建通道**：启动独立 goroutine 执行 `ctxgraph.ScanCached(paths, prior)` 及 `ctxgraph.StitchGraph(g)`（`session.go:154-162`），并发构建内容寻址的 Lineage 图与缝合边；
    2. **报表特征提取通道**：利用有界 Worker Pool（`sem := make(chan struct{}, analysisWorkerCount(len(paths)))`，`session.go:164-175`）并发执行 `analyzeFile(path, prof)`，提取 ToolsDeclared、RoleChars/Tokens、ChatID、Compaction 标记等报表域专属特征；
  - **安全性与确定性**：
    - `results` 切片按文件索引 `i` 预分配，Worker 并发写入互不干扰，完全无锁；
    - 双通道在 `wg.Wait()` 与 `scanWG.Wait()` 处汇合，任意一方出错均能捕获并按路径顺序返回第一处错误（`session.go:178-185`）；
    - 汇合后显式执行 `sort.SliceStable(a.Recs, ...)` 按照记录到达时间 `TS` 稳定排序（`session.go:193`），彻底消除了并发文件读取完成顺序的不确定性。
- **结论**：并发模型清晰高效，在离线重算场景下将大语料解析的 CPU 吞吐利用到极致，且严格保障了后续时序处理的确定性。

#### 1.2 SSOT（单源真相）与哈希共享
- **实现位置**：`internal/report/session.go:343-380` (`group`)，`session.go:402-440` (`attach`)
- **审查核实**：
  - 过去报表内部曾维护私有的 `keys` 向量与 `lcp()` 窗口搜索；重构后，`session.go` 完全废弃了私有哈希计算，`ReqInfo.manifest` 直接通过 `(Path, Line)` 坐标对齐绑定 `ctxgraph.Manifest`（`session.go:356`）；
  - 会话切分完全由 `ctxgraph.Lineage` 主导（`session.go:368-377`），每个 `SessionInfo` 即一个 `Lineage`，`SessionInfo.ID` 统一采用 `Lineage.LineageID()`（`session.go:379`，即 `"l-<hash8>"`），与 `story` 域的 Journey Lineage ID 保持完全一致；
  - 增量识别与历史比对完全调用 `ctxgraph.Classify(p.manifest, r.manifest)`（`session.go:412`），从中提取 `e.LCP` 计算 `DeltaStart` 与 `ReplacedTail`。
- **结论**：彻底实现 SSOT，无任何私有消息哈希向量重算，杜绝了两半区在对话重置判定上的逻辑漂移。

#### 1.3 任务边界与任务切分
- **实现位置**：`internal/report/session.go:417-422`，`internal/taskseg/segment.go:121-124` (`IsNewTask`)
- **审查核实**：
  - 任务边界判定下沉至 `internal/taskseg.IsNewTask`：
    `traceChanged || (!prevNoReply && hasNewInstr)`
  - 准确识别客户端 `Traceparent` 变更（强任务边界）与真实用户指令注入（`taskseg.HasNewInstruction`）；
  - 对父记录为 `NoReply`（模型主动跳过或 OpenClaw `NO_REPLY`）的重试请求，正确维持在同一任务内（`session.go:418-422`），避免将 Agent 内部重试机制割裂为两个独立任务。
- **结论**：任务分块逻辑与 Agent 行为模式高度契合，且在 `report` 和 `story` 之间实现 100% 共享。

#### 1.4 Compaction 链接的两条互补信号设计
- **实现位置**：
  - 结构哈希缝合：`internal/report/session.go:387-400` (`linkStitchedLineages`)
  - 文本子串关联：`internal/report/session.go:487-526` (`linkCompactions`)
- **互补性分析**：
  - **`linkStitchedLineages`（结构性精确信号）**：基于 `ctxgraph.StitchGraph` 计算的 Blob 重叠（`StitchCompaction` / `StitchHeadPrune`），处理 Contract/Fork 后的断裂。适用于原地压缩后保留了后续消息哈希的场景。
  - **`linkCompactions`（文本语义补充信号）**：针对独立发出的摘要 LLM 请求（Body 匹配 `"summarization"` 或无工具声明+`max_completion_tokens`）。此类请求自身输入为格式化渲染后的转录文本，输出为浓缩摘要，与前后会话在消息结构上**0 哈希重叠**（`ctxgraph` 无法通过倒排索引匹配）。通过 200 字节的前后针刺（`needle(c.respText)` 与 `needle(first.firstText)`）进行子串匹配。
  - **优先级契约**：`linkStitchedLineages` 先跑；`linkCompactions` 仅在 `successor.ContinuedFrom == ""` 时填补（`session.go:395`, `session.go:511`），绝对不覆盖高置信度的结构匹配结果。
- **结论**：双信号互补设计合理，逻辑严密，既守住了精确哈希的底线，又通过文本锚点覆盖了端到端摘要调用的盲区。

#### 1.5 内存释放策略（`releaseTextBuffers`）
- **实现位置**：`internal/report/session.go:838-863`
- **审查核实**：
  - `collect()` 阶段捕获的 `firstText`（上限 512KB）和 `respText`（上限 256KB）在大语料下常驻会导致严重的堆膨胀；
  - `releaseTextBuffers` 精确计算保留白名单：仅保留每个 Session 的首条记录（供 `sessionTitle` 提取）和 Compaction 记录（供 `recextract.buildCompactions` 提取实体），其余所有中间请求的 `firstText`/`respText` 立即置空（`session.go:860`）。
- **结论**：主动内存回收机制针对性强，有效遏制了万级请求全内存聚合下的 RSS 峰值。

---

### 任务 2: 聚合算法（`internal/report/aggregate.go`, `ingest.go`, `metrics.go`）

#### 2.1 `buildInternal` 三段式生命周期
- **实现位置**：`internal/report/aggregate.go:141-171`
- **流程拆解**：
  1. **`scanFiles`（单遍扫描/重放）**：流式遍历输入文件，结合 `sess.Lookup` 将每条审计记录映射到 `rec2`，调用各 Row 的 `Ingest` 进行数据分发（`aggregate.go:173-228`）；支持 `factscache.go` 缓存直读（`ingestCachedFile`），大幅加速非 `-details` 运行；
  2. **`finishBuckets`（收尾计算与后置构建）**：就地计算各分桶的百分位数、派生比率（`tokens_in_fresh`、`cache_efficiency`、`context_growth`），并装配 `Tools`、`Providers`、`ProviderQuotas`、`Compactions`、`Sticky`、`Efficiency` 等后置表格（`aggregate.go:264-297`）；
  3. **`sortBuckets`（确定性排序）**：对所有生成切片执行严格的多级比较排序（`aggregate.go:303-360`）。
- **结论**：三段式设计职责单一、流程清晰，彻底解耦了原始累加与终态指标计算。

#### 2.2 两遍扫描设计（Two-Pass Design）
- **实现位置**：`internal/report/build_cached.go:30-49`，`aggregate.go:141-168`
- **机制评估**：
  - **Pass 1 (`AnalyzeSessionsCached`)**：先全局构建会话谱系、任务边界及 Compaction 链，输出 `SessionAnalysis`；
  - **Pass 2 (`scanFiles` / `ingestRecord`)**：第二遍扫描时，每条记录携带已有的 `SessionInfo`/`TaskInfo` 坐标，一次性完成全局、按模型、按日期、按端点、按客户端、按工作负载的指标累加及详单输出。
- **结论**：两遍扫描是正确处理全局拓扑依赖（如 Compaction 上下文链接、Session 归属）所必需的架构设计；配合 `factscache.go` 使得 Pass 2 在热缓存下仅耗时百毫秒级。

#### 2.3 桶的百分位不可加性与原始样本生命周期
- **实现位置**：`internal/report/metrics.go:47-68` (`percentiles`, `finishMeasures`), `rows.go:160-184`
- **数学与工程审查**：
  - **硬性约束**：百分位数（P50/P95）在数学上不可相加（`P95(A ∪ B) ≠ max(P95(A), P95(B))`）。VMR 严禁在跨天或汇总时进行“百分位的百分位”二次平均；
  - **实现机制**：每个分桶（`Overall`、`ByModel`、`Endpoints` 等）在 Ingest 期间均持有原始样本切片（`durs`、`ttfts`、`streamMS`、`inToks`、`outToks`）；
  - **流式耗时真实采样**：针对流式请求，明确记录 `streamMS = durMS - ttftMS`，独立计算真实 P50/P95，避免了 `P95(dur) - P95(ttft)` 在统计上的失真（`rows.go:160-165`）；
  - **内存即时释放**：在 `finish*` 函数（`metrics.go:70-168`）完成 Nearest-Rank 百分位提取后，**立即将原始样本切片置为 `nil`**（例如 `s.durs = nil`, `e.durs, e.ttfts... = nil`），严防内存泄漏。
- **结论**：统计口径严谨无误，内存生命周期管理严格。

#### 2.4 派生指标的正确性
- **实现位置**：`internal/report/metrics.go:21-45`, `metrics.go:175-188`
- **指标核查**：
  - **`freshTokens`**：`Usage.Fresh()` = `max(0, In - CacheRead - CacheWrite)`，严格遵循 Token 守恒；
  - **`cache_efficiency`**：`CacheRead / (CacheRead + Fresh)`。当总有效输入为 0 时返回 0（避免 `0/0` 导致 NaN 或虚假的 100% 效率）；
  - **`cache_hit_rate`**：`CacheRead / In`，衡量直接命中率；
  - **`context_growth`**（`metrics.go:175-188`）：计算会话尾轮与首轮 `tokens_in` 之比（`float64(last) / float64(first)`）。**关键守卫**：由于分组已严格按 `ctxgraph.Lineage` 切分，`info.Recs` 绝不会跨越 Contract/Fork 导致的隐式历史重置，保证了上下文膨胀倍率反映的是同一连续对话流。

---

### 任务 3: 成本估算（`internal/report/cost.go`, `pricing.go`, `internal/pricing/`）

#### 3.1 四分量精确计价模型
- **实现位置**：`internal/report/cost.go:9-16` (`costFor`), `internal/pricing/pricing.go:108-117` (`Rate.Cost`)
- **审查核实**：
  - 计价公式全面覆盖大模型计费四个维度：
    $$\text{Cost} = \frac{\text{Fresh} \times P_{\text{fresh}} + \text{CacheRead} \times P_{\text{read}} + \text{CacheWrite} \times P_{\text{write}} + \text{Out} \times P_{\text{out}}}{1,000,000}$$
  - `pricing.Rate.Cost` 对任一未定义分量（`nil`）视为 0（防御性下界），并在聚合报告中输出 `CostRateIncomplete` 警告。

#### 3.2 端点标签分割与防漂移
- **实现位置**：`internal/report/cost.go:28-36`, `internal/pricing/resolver.go:118-134`, `internal/report/provider.go:99-135`
- **审查核实**：
  - `RateForEndpoint` 与 `splitEndpointProviderModel` 统一采用严格的冒号分割 `strings.SplitN(endpoint, ":", 3)`，第 3 段完整保留模型名（即便模型名包含 `:` 或 `/`，如 `z-ai/glm-5.2` 亦不受损）；
  - 严格冒号分割是刻意架构取舍（见 `KNOWN_ISSUES §1.4`）：拒绝自动兼容历史老日志的 `/` 分割，防止在历史报表上产生未经评审的金额漂移；而展示层的 `splitEndpointProviderModelAny`（`provider.go:126`）则保留了容错能力，边界分明。

#### 3.3 §2 口径与三类旁注
- **实现位置**：`internal/report/section_cost.go:15-132`
- **口径审查**：
  - **§2 核心口径**：明确定义为「按量计费等价成本」（Pay-As-You-Go Equivalent），按公开刊例价/账号覆盖价计量，非真实转售/包月实付账单（`section_cost.go:20`, `report_cost.go:18-24`）；
  - **三类旁注完整性**：
    1. **未定价（Unpriced）**：`costTotalOf` 统计未命中费率的行数，报表明确标注「合计不含 N/M 个模型/端点」，未定价行成本显示为 `-` 而非欺骗性的 `0`；
    2. **降级估算（Degraded Estimate）**：针对未返回 Usage、靠字节估算的记录，累加 `CostEstimateEst` 并在表底披露金额与占比（`section_cost.go:117-123`）；
    3. **费率缺分量（Incomplete Rate）**：通过 `CostRateIncomplete` 标记并提示「N 个端点的单价缺分量，金额为保守下界」（`section_cost.go:124-128`）。

#### 3.4 降级估算的 Fallback 不对称性设计
- **实现位置**：`internal/chatmsg/tokenest.go:11-47` (`EstimateDegradedTokens`)
- **原则核实**：
  - **请求侧（Request）**：回退至 Raw 字节估算（`EstimateRequestBodyTokens`）。因为请求体 JSON 即为实际发送的内容与脚手架，且路由半区配额扣减即以此为基准，回退 0 会导致报表与配额扣减劈叉；
  - **响应侧（Response）**：回退为 0，**严禁回退至 Raw 字节**。因为在流式截断或乱码响应中，Raw 字节衡量的是传输层（SSE 信封/分块编码）而非生成内容，回退 Raw 字节曾导致虚高 71 倍的严重缺陷（Q04）；
  - 该行为由 `chatmsg/tokenest_test.go` (`TestEstimateDegradedBasis_FallbackAsymmetry`) 严格守护，且与 `cmd/vmr/cost_basis_parity_test.go` 保持端到端差分对齐。

---

### 任务 4: 逐请求详单（`internal/reqdetail/`）

#### 4.1 纯函数设计与事实提取
- **实现位置**：`internal/reqdetail/facts.go:1-150`，`detail.go:78-112` (`extractSessionFeatures`)
- **审查核实**：
  - `facts.go` 中的每记录事实抽取（`RoleChars`、`RoleTokens`、`AttemptErrorClass`、`CountImages`、`ToolsSig`）均为纯函数输入 `audit.Record`，无任何跨记录状态；
  - 详单渲染（`Render`）只依赖入参 `(rec, path, line, m, prev, prof, lang, linkEvidence)`，不依赖报表内部的 `ReqInfo` 或全局累加器。

#### 4.2 物理路径 Diff 与三段式呈现
- **实现位置**：`internal/reqdetail/detail.go:140-300`
- **审查核实**：
  - 详单完全按物理流向组织：
    ① **客户端请求**（`renderClientRequest`）：请求头、参数、工具集（证据链接或折叠）、消息列表、角色 Token 占比；
    ② **上游尝试**（`renderAttempts`）：逐个 Attempt 呈现，包含 Header Diff（`diffHeaderTable`）、Body Diff（`renderBodyDiff`）、响应 Norm 变换记录（`writeNorms`）及被剥离前原始思考（`RawPreStrip`）；
    ③ **客户端响应**（`renderClientResponse`）：状态码、响应 Header 与成功 Attempt 的 Diff、SSE 重组后的 Stream Summary（思考折叠、内容呈现、工具调用参数折叠）。

#### 4.3 坐标哈希命名与无状态去重
- **实现位置**：`internal/reqdetail/detail.go:47-88` (`FileName`), `internal/ctxgraph/reqcoord.go:1-40`
- **审查核实**：
  - 详单文件名规范：`{TS}_{VirtualModel}_{RealModel}_{Outcome}_{ReqHash8}.md`；
  - `ReqHash8` 为 `md5(CanonicalPath(path) + ":" + line)[:4]`（8 字符十六进制）。文件名纯由记录固有坐标与属性决定，完全去除了历史上的并发批次序号计数器，实现 100% 确定性。

#### 4.4 跨记录上下文（`prev`）与 O(N²) 历史折叠
- **实现位置**：`internal/reqdetail/detail.go:210-270`
- **审查核实**：
  - 详单借助 `prev`（Lineage 中的前驱 Manifest）调用 `ctxgraph.Classify(prev, m)` 计算 `deltaStart`；
  - 在 `i < deltaStart` 范围内的历史消息直接折叠为指向前驱详单的链接（`HistoryFoldedNote`，`detail.go:249-254`），仅展开 `i >= deltaStart` 的新消息（标 `🆕`）；
  - 此设计彻底终结了长对话中每轮完整展开历史导致的 $O(N^2)$ 文件体积爆炸。

#### 4.5 双半区 Byte-Identical 一致性与指纹防御
- **实现位置**：`internal/reqdetail/render.go:46-65` (`renderFingerprint`), `ensure.go:15-60` (`EnsureRendered`)
- **审查核实**：
  - `EnsureRendered` 在文件头部注入首行注释指纹：
    `<!-- reqdetail:v2 lang=... evidence=... m=... prev=... -->`
  - 详单内容依赖 `(m, prev)`，但文件名不含两者坐标；通过 `readRenderFingerprint` 读取首行，若指纹不符（如语言切换、证据模式变更、或前驱上下文不同），则强制重新渲染；
  - `report` 域（`detail.go:manifestsFor`）与 `story` 域向 `EnsureRendered` 传递相同的三元组，确保两个子命令生成的详单逐字节一致。

---

### 任务 5: 各 Section 文件与架构门禁（`internal/report/section_*.go`）

#### 5.1 「一节一文件」规则与 Archtest 行数预算
- **审查核实**：
  - 全部章节严格拆分在独立文件中，无单文件膨胀现象：
    - `section_tokens.go` (§1) — 79 行
    - `section_cost.go` (§2) — 216 行
    - `section_provider.go` (§2.5) — 229 行
    - `section_efficiency.go` (§2.6) — 126 行
    - `section_reliability.go` (§3) — 249 行
    - `section_latency.go` (§4) — 87 行
    - `section_workload.go` (§5) — 116 行
    - `section_client_endpoint.go` (§5.5) — 44 行
    - `section_sessions.go` (§6) — 241 行
    - `section_sticky.go` (§6.5) — 81 行
    - `section_endpoint_value.go` (§6.6) — 128 行
    - `section_compaction.go` (§6.7) — 75 行
  - 所有文件均远低于 `archtest` 700 行默认上限；
  - 每个 `section_*.go` 均在 `internal/i18n/report_*.go` 中有严格对应的双语文案结构体，文案改动内聚在对应文件中。

#### 5.2 渲染抽象 `mdTable` 与表格安全防线
- **实现位置**：`internal/report/render_doc.go:18-68`
- **审查核实**：
  - `mdTable` 统一了 Markdown 表格的表头与对齐线输出，强制列数匹配校验（`len(cells) != t.cols` 则直接 panic 暴露编程错误，`render_doc.go:48`）；
  - **单元格安全转义**：
    - `EscapeCell`（`reqdetail/render.go:73-77`）替换 `|` 为 `\|`，替换 `\n` 为空格，防止破坏 GFM 结构；
    - 针对可能包含未闭合 HTML/注释的内容，调用 `EscapeHTML`（`reqdetail/render.go:27-33`），彻底防止诸如 `<!--` 吞噬后续整个 Markdown 文档的破坏性渲染 Bug。

---

## 2. 领域级问题汇总（按严重性）

经过全量代码审查，D3 报表分析域未发现服务阻断、内存泄漏或数据破坏级别的高危缺陷，系统整体健壮、口径严谨。发现的潜在中/低风险项汇总如下：

| 编号 | 严重性 | 问题分类 | 描述与影响 | 源码锚点 |
| :--- | :---: | :--- | :--- | :--- |
| **D3-01** | **[中]** | 大语料内存 | `AnalyzeSessionsCached` 在全内存持有全量 `ReqInfo` 切片。当语料达到数万条以上时，虽然已释放文本缓冲，但结构体基础开销与原始耗时样本切片仍会导致 2GB+ 的瞬时内存占用。 | `internal/report/session.go:142`, `aggregate.go:18` |
| **D3-02** | **[低]** | 缓存覆盖度 | 会话拓扑的第一遍扫描 `analyzeFile`（`collect()`）尚未接入磁盘持久化缓存（每次均需解压与 JSON 反序列化）。虽有并发 Worker 缓解，但热耗时仍受 I/O 限制。 | `internal/report/session.go:164-175` |
| **D3-03** | **[低]** | 指标边界 | `finishSession` 的 `ContextGrowth` 计算依赖 `info.Recs[0].Usage.In`。若首轮请求未嗅探到 Usage（`Usage.In == 0`），则 `ContextGrowth` 会保持为 0，未尝试使用 `EstInFresh` 进行回退计算。 | `internal/report/metrics.go:183-186` |
| **D3-04** | **[低]** | 统计展示 | `§2` 成本表的按 Client 汇总合计行略低于全局总额（若存在无 `client_key_tag` 的请求，这些请求不构成行亦不入 ByClient 合计），虽在各表内部自洽，但跨表对比可能引起轻微困惑。 | `internal/report/section_cost.go:137-160` |

---

## 3. 对 KNOWN_ISSUES §2 A/B/D 组中相关项的评价

对 `KNOWN_ISSUES.md` 中涉及报表与分析半区（A组/B组/D组）的条目，结合源码现状进行逐项核实与评估：

### 3.1 A 组（大语料规模与内存/耗时）

- **`§2.2 [中] vmr report / vmr analyze 全内存聚合的记录量上限`**
  - **现状核实**：`report` 半区在 `AnalyzeSessionsCached` 中常驻全量 `ReqInfo`，在 `aggState` 中持有原始 `durs`/`ttfts` 切片以计算真实百分位。2026-08 已实施 `releaseTextBuffers`（清理中间记录的 `firstText`/`respText`），实测万级记录 RSS 在 1.4~1.6GB，运行正常。
  - **评价**：当前「待定，触发条件设为语料 > 3 万条或 RSS > 4GB」的策略非常稳健。按日分桶释放原始切片的方案虽然可行，但引入了时间单调性的隐蔽前提，在达到触发条件前不宜盲目重构。

- **`§2.1 [低] vmr report 多文件输入：会话分析那一趟（collect()）仍未缓存`**
  - **现状核实**：`scanFiles` 已通过 `factscache.go` 实现指标层缓存（加速 5.2×），但 Pass 1 的 `collect()` 每次仍重新解析文件。
  - **评价**：`collect()` 直接决定会话切分与 Stitch 边界，正确性敏感度极高。维持当前「必须先补齐 Cold/Warm 差分测试再行动」的准入红线是正确的。

- **`§2.50 [低] 详单文件名去重位 md5(basename:line)[:4]（32 bit）`**
  - **现状核实**：`ReqHash8` 截取 4 字节哈希。
  - **评价**：文件名同时组合了 `{ts}_{virtual}_{real}_{outcome}`，发生完全同名冲突的实际概率微乎其微。维持低危、无需提前过度设计。

- **`§2.3 [低，决定不做] chatmsg 离线解析路径的 map[string]any 分配`**
  - **现状核实**：实测 Heap 仅放大 1.40x，长文本字符串占主导。
  - **评价**：裁决完全正确。「决定不做」避免了破坏 40+ 处类型断言的高昂维护成本。

- **`§2.56 [低] 一次 vmr analyze 至少把全量语料解压三遍`**
  - **现状核实**：`ScanCached`、`PreviewTitles`、`BuildAll` 与 `report.analyzeFile` 各自解压。
  - **评价**：CPU 耗时主要发生在首次冷启动，后续受缓存保护。在内存瓶颈闭环后，可作为次级优化项。

---

### 3.2 B 组（指标与口径正确性）

- **`§2.58 [低] 定价表覆盖不到的模型，其成本永远不进任何合计`**
  - **现状核实**：`costTotalOf`（`section_cost.go:183`）严格遵守「未定价显示 `-`，并在表底注明缺失 N/M 行」，合计行只对成功定价的行求和。
  - **评价**：体现了「宁可承认未知，不可造假为 0」的严谨哲学，完全符合设计预期。

- **`§2.58a [低] 费率缺分量时按 0 计价，只在 §2 汇总层披露，不逐行标注`**
  - **现状核实**：`IncompleteRateNote` 正确汇总了缺分量端点数；`Rate.Cost` 按 0 计价作为保守下界。
  - **评价**：对于报表宏观分析已经足够透明，未来若引入额度看板可一并下沉到行级溯源。

- **`§2.58c [低] §2 按客户端表的合计略低于其它三张表`**
  - **现状核实**：无 `client_key_tag` 的请求不成行，因此未计入 ByClient 合计。
  - **评价**：差额仅在万分之二量级，且表内求和自洽。保持现状即可。

- **`§2.63 [低] 宏观报表 §0 top-line 把调度型 workload 计入总量`**
  - **现状核实**：§0 总请求数包含 `heartbeat` / `dream_diary` / `compaction`，具体分类需在 §5 展开。
  - **评价**：建议在后续小版本中，在 §0 Summary 表底增加一行注明「其中 interactive 占比 N%」，ROI 极高且随时可做。

- **`§2.66 [已解决] 分析半区的 usage 解析协议参数全链路贯通`**
  - **现状核实**：`ExtractUsageWithProtocol` 已在 `session.go`、`manifest.go`、`detail.go` 中全量替换 `ExtractUsage`。
  - **评价**：经源码核验已彻底闭环，消除了协议推测导致的 CacheRead/In 误判。

- **`§2.68 [低] crosscheck 夹具没有 body-sniffed 的 compaction 记录`**
  - **现状核实**：Compaction 详单的一致性当前由 `session_compaction_manifest_test.go` 和指纹机制间接保障。
  - **评价**：机制已闭环，待未来有真实语料时补充端到端直擦夹具即可。

---

### 3.3 D 组（展示与产出契约）

- **`§2.6 [低] §2.5 表格的标记符号已达四个`**
  - **现状核实**：`⭐` / `‡` / `†` / `◇` 四种标记按需出现，有对应脚注。
  - **评价**：正常报表中各标记稀疏出现，信息密度尚在可读范围内。

- **`§2.7 [低] vmr report §2 成本表结构化透传 CostEstimateEst（方案 ②）`**
  - **现状核实**：Markdown 口径已闭环，JSON 字段扩充遵循 YAGNI 暂缓。
  - **评价**：符合项目对外部消费者按需驱动的演进原则。

---

## 4. 总结与建议

D3 报表分析域在本次审查中展现出极高的工程质量与架构纪律：
1. **架构边界清晰**：严格执行单向依赖，与路由核心彻底解耦；
2. **算法严谨可信**：百分位真实采样、上下文膨胀率基于 Lineage 隔离、四分量计价与降级估算不对称性均有严格的数学与测试锁定；
3. **性能与内存受控**：通过双通道并发、事实缓存与文本缓冲主动释放，保障了万级日志处理的稳定性。

**后续演进建议**：
1. **§0 顶层体验微调（低成本）**：采纳 `KNOWN_ISSUES §2.63`，在 §0 增加一行 interactive 请求占比提示；
2. **ContextGrowth 降级增强**：当首轮未嗅探到 Usage 时，尝试回退至 `EstInFresh` 计算近似倍率，提高该指标覆盖度。
