<!-- Ver 2026-08-11, by Sonnet 5 -->

# vmr — Known Issues（已知问题清单）

> **定位**：本文档是关于代码/架构问题的**唯一权威、持续跟踪的当前状态清单**，取代并合并原来的两份文档——
> `OUTSTANDING_ISSUES_opus-5.md`（遗留问题清单，2026-08-04 基线）与
> `vmr_architecture_review_independent_sonnet-5.md`（独立架构深度审查，2026-08-10/11 基线）。两份原始文档
> 已移入 `docs/archived/`，仅作历史存档，**不再更新**；本文档才是当前状态的准绳。
>
> **本文档同时包含两类问题**，这是与旧 `OUTSTANDING_ISSUES` 相比最主要的结构变化：
> 1. **待解决问题**——我们知道、还没处理、值不值得处理需要排期判断的问题；
> 2. **已知但确定不修复的问题**——我们看到了、评估过、明确选择"暂不处理"或"永久不处理"的问题，连同选择
>    背后的理由一起记录下来。记录的目的不是"以后要改"，而是**证明这些问题不是被忽略，而是被看见后主动
>    放弃**，避免未来的审查反复重新发明、重新论证同一批已有结论的问题。
>
> **基线**：本文档在 commit `82a4ad1`（"Aggregate quirk-repair marker frequency by endpoint in vmr
> report §3"）上逐条对照当前代码库实测重写，不是对两份旧文档的机械拼接——每一条都重新核对了代码现状，
> 旧文档里过时、已修复、或表述有误的条目已经更正或删除，不保留错误记录。核对当日
> `go vet ./...` 全绿，关键改动点（`ErrContextLimit`/`response.go` 预算/`NewTokenWeights`/
> `chatmsg.NewUserWindow`/`firstDeadOverride`/`EndpointRow.NormCounts`）均以 `grep`/行数实测复核，
> 非转述旧文档的自我陈述。
>
> **本次复核发现的一处不一致，特此更正**：`vmr_architecture_review_independent_sonnet-5.md` §11.2 曾
> 自称"已处理"`docs/future-strategy/` 目录归档（声称把三份文档 `mv` 进新建的 `docs/future-strategy/archived/`）。
> 实测该目录**不存在**，且声称移动的三个文件名（`vmr_strategy_and_competitive_analysis_v1.0_custom-2-agent.md`
> 等）与当前目录里实际的文件名都对不上。这个"已处理"是一条失实记录（该操作实际从未发生，或发生后又被
> 撤销，无法判断具体原因），本文档已将其改列为**仍未处理**，见 §1.3。这是本文档在"double-check 是否有
> 疏漏和错误"环节里发现的唯一一处实质性事实错误。
>
> **分档标准**（沿用旧 `OUTSTANDING_ISSUES` 的既有约定，实测证明有效，不改）：
> - **第一梯队** = 倾向立刻处理。要么高价值高 ROI，要么价值一般但改动极简、无回归风险。
> - **待定** = 长远看有价值，但成本高或有风险，改与不改都合理；给出具体触发条件。
> - **其他** = 一句话点出，不展开。
>
> 严重度：`[S]` 严重 / `[M]` 中等 / `[L]` 轻微。

---

## 0. 结论先行

- 没有会导致数据丢失、凭证泄漏或服务不可用的缺陷，可以继续放心用于生产。**这个判断经过了两轮独立复核**
  （2026-08-04 的遗留问题清单核对、2026-08-10/11 的独立架构审查），两轮结论一致。
- 架构层面**没有发现"设计错了"的问题**——独立架构审查逐包通读全部 24 个 internal 包 + cmd/vmr 后的结论是：
  真正值得讨论的问题全部集中在**范围（这个特性值多少工程投入）与优先级（先做哪个）**,不是正确性或设计
  模式维度。这个判断本次复核维持不变。
- 当前**第一梯队为空**。上一轮审查发现的三项高 ROI 代码级问题（`ErrContextLimit` 分类盲区、
  `response.go` 缺行数预算、TokenPlan 定价引擎复杂度超配）均已处理完毕，见 §3。
- 本次合并新增/更正的条目：§1.3 更正了一处失实的"已处理"记录（`future-strategy/` 归档实际未发生）；
  §1.2 新增"分析半区体量持续增长"作为独立待定项（原架构审查的 P0-C，需要产品层面裁定，不是代码 bug）；
  §1.3 新增"额度燃尽看板未交付"（原架构审查缺口清单 #5）；§2 吸收了架构审查里全部"核实后判定不成立/
  不建议动"的条目，与旧 `OUTSTANDING_ISSUES` §3.2 合并去重。

---

## 1. 待解决问题

### 1.1 第一梯队（倾向立刻处理）

当前为空。原有的高 ROI 项（MiniMax thinking 泄漏观测、truncated 请求端点归属、compaction 链接漏链日志、
`RewriteModel`/`RewriteStream` fuzz 测试、`ErrContextLimit` 分类盲区、`response.go` 行数预算、TokenPlan
定价引擎复杂度收窄、`newUserWindow`/`TokenWeights` 跨包重复、quirk 修复聚合展示等）均已处理完毕，见 §3。

---

### 1.2 待定（有价值，但成本或风险不低）

#### 1.2.1 [M] `vmr report` 仍对同一批输入做两趟完整扫描

`report.Build(paths, …)` 先跑一趟 `AnalyzeSessions`（会话/任务分组，已按文件并行），再跑一趟聚合循环
（详单导出已并入这一趟的 `onRecord` 钩子）。剩下的两趟仍各自完整读一遍全部输入文件（含 `.zst` 解压）。
GB 级审计日志下，批处理时间里有相当一部分是重复的 I/O + 解压。

**方案**：合并成单趟，让会话分析器与聚合器共享同一次 `ForEachLine` 扫描。**为什么待定**：
`AnalyzeSessions` 需要"全部记录都到齐"才能做 LCP 选父与 compaction 链接（本质是全局 join），聚合器天然
流式，真做单趟要把会话分析器改造成"边喂边攒、结尾统一 finalize"的形状，改动量 >200 行且触碰 report
最微妙的启发式部分。**触发条件**：真的遇到"报告跑太慢"的实际投诉时再做，届时先测两趟各自的耗时占比。

#### 1.2.2 [M] `report` 全内存聚合，记录量上限偏紧

三处内存驻留叠加：`AnalyzeSessions` 常驻全部记录的 `ReqInfo`；每条 record 的原始 `dur/ttft/stream/
inTok/outTok` push 进 6 个 bucket 各自的切片（真百分位需要保留原始值）；`RequestRows` 每条记录一个
struct。千万级记录约 640MB。

**方案 A（推荐，保精确）**：利用"输入按时间有序"做流式分桶，跨过日界即 finalize 释放原始切片，内存从
`O(总记录)` 降到 `O(单日记录)`。**方案 B（不推荐）**：t-digest/固定容量蓄水池换 `O(1)` 内存，但真百分位
是这份报表有意的产品不变式（判断慢请求分布用），不该为内存让步。**为什么待定**：方案 A 依赖"跨日之后
不再有该日记录"，多文件混合输入 + 乱序 glob 会破坏这个前提，需要先做输入排序保证。**触发条件**：真的撞到
内存墙时再做。

#### 1.2.3 [M] OpenClaw / Claude Code 客户端方言散落在 `report` 的 6 个文件里

`internal/report` 硬编码了对具体 agent 客户端的深度知识，实测分布在 6 个源文件（不含测试）：
`session.go`（信封 JSON 块、时间前缀、`chat_id` 正则、no-reply 检测，33 处，占绝大多数）、`aggregate.go`
（heartbeat/dream_diary 定时任务类别）、`metrics.go`（对应的低 cache-efficiency 告警规则）、`requests.go`
（`cronFileTag` 的 `"hartbeat"` 刻意拼错文件名）、`detail.go`（`compacted_session` 标签处理）、
`section_sessions.go`（对应渲染分支——**这是本次复核的更正**：旧 `OUTSTANDING_ISSUES` 记的是
`aggregate_render.go`，该文件早已被拆分重构为 `render_doc.go` + 一节一文件的 `section_*.go`，OpenClaw
渲染分支现在的落点是 `section_sessions.go`，旧记录的文件名已失效）。外加 Claude Code 的
`metadata.user_id`。

这些启发式本身写得克制（失配无害），提供的价值也真实（agent 会话分组是 `vmr report` 相对通用日志分析
工具的核心差异化）。问题不在于它们存在，而在于**位置**——应该是审计日志边界下游的一个可替换"客户端
方言插件"，而不是散落在通用聚合逻辑里。

**方案**：收拢成 `internal/report/dialect/` 显式子包，暴露一个窄接口（给一条记录，返回会话 ID/任务
类别/要剥掉的信封前缀），OpenClaw 作为第一个实现。**为什么待定**：现在只有一个方言实现，抽象一个只有
一个实现的接口是投机性泛化。**触发条件**：接入第二个 agent 客户端时做。

#### 1.2.4 [M] 客户端流中途断开与完整成功，在审计里完全不可区分

2xx 响应头已提交给客户端之后，客户端主动断开（Ctrl-C、agent 超时放弃）时，attempt 记录的 `error`
字段留空、状态 2xx——与真正跑完的成功请求在所有字段上完全一样（`grep -rn
"client_disconnected\|ClientDisconnected" internal/` 无命中，本次复核重新确认）。这类请求在报表里被计成
成功，但 token 用量、耗时分布与真实完成的请求有系统性差异，是分析"为什么 p95 变差了"时完全看不见的
噪音源。

**方案**：`audit.Attempt`（或 `Record`）加一个 `client_disconnected` 布尔位，在 `copyFlush` 写回客户端
返回 `ErrClosedPipe`/context canceled 时置位；`report` 侧增加对应统计与筛选。**为什么待定**：这是改
审计记录 schema——`internal/report` 编译期耦合 `audit.Record` 的形状，改动必须同步 `report` 及其测试，
还要考虑历史审计文件（旧记录没这个字段，消费端必须把"缺失"和"false"都当"未知"处理）。收益真实但不
紧急，成本是一次完整跨包变更 + Format 号升级。

#### 1.2.5 [L] 6 个 bucket 类型仍各自重复声明同一批度量字段（体量已逼近文件行数预算）

`internal/report/aggregate.go` 里 `Row`/`HourRow`/`EndpointRow`/`ClientRow`/`WorkloadRow`/`SessionRow`
六个结构体各自重复声明同一批度量字段（token 家族 ×6、耗时百分位家族 ×6、TTFT 家族 ×5、原始切片 ×6）。
共有派生计算（真百分位、fresh tokens、cache efficiency）已收进共享的 `finishMeasures`，但**结构体声明
本身没动**——新增一个分组维度仍需改 4 个地方。

**实测更新（本次复核）**：`aggregate.go` 当前 **970 行**（`wc -l`），距 `internal/archtest` 的 1000 行
预算只剩 30 行——比原记录的"999/1000"略有反复（P0-A 简化定价引擎减了一些行，quirk 聚合展示又加了
`NormCounts` 相关逻辑），但仍然是全仓库距撞线最近的文件之一。

**方案**：泛型 `Bucket[K comparable]{ Key K; Measures }` + `Measures.Add/Finish`，估算 1,200–1,500 行
→ ~300 行。**为什么待定（有硬约束）**：同名字段（如 `CacheEfficiency`）在不同 Row 类型上的 `omitempty`
标签并不一致，统一成共享嵌入结构体会悄悄改变 `vmr-report.json` 的输出，真要做必须接受一次 Format 号
升级（10→11）并用真实生产日志逐字节比对验证。**触发条件不变**：`aggregate.go` 撞线的那次改动，就是
做这条重构的时候——`archtest` 的注释已经预先反对"撞线就调大预算数字"这条退路，不要用它当挡箭牌继续拖。

#### 1.2.6 [L] ingress 侧请求体仍被独立扫描约 7 遍

一个典型的无图无文档 agent 请求，发往上游前 body 被完整/近似完整遍历：`TopLevelProbe`（字节级顶层
扫描，已经把 tools 判断并进来，是最刺眼的一处，已解决）→ `imgprep.HasImageMarker` → `facts.go` 的
`EstimateTextTokens`（全字节循环）→ 4 个 `documentMarkers` 扫描（全部未命中时=4 次完整扫描）→
`SessionFingerprint`（结构化扫描 + 2× md5）→ `RewriteModel`（顶层扫描 + splice，每次 attempt 一遍）。

**方案**：融合成单遍结构化提取器，一次扫描产出图片块、文档 payload、token 估算。**为什么待定（有明确
风险来源）**：`HasImage` 喂的是**无 fallback 的硬性淘汰条件**——判错直接导致 503，历史上出过一次真实
事故（图片误判）。融合时必须逐字保持 `imgprep` 那套结构化遍历的判定语义，只让其他信号搭车，不属于"简单
易行"那一档。当前实测 p50 是 1–2ms、主要成本在上游往返，**不存在性能危机来正当化这个风险**。

#### 1.2.7 [L] 审计落盘的文件 write syscall 仍在全局锁内

`audit.Logger.Write` 已经优化过一轮——JSON 编码用 `sync.Pool` 复用缓冲区、在锁外完成，只有最终字节
写入持锁，消除了"多 MB JSON 编码被并发请求串行化"这个真实瓶颈。`f.Write` 这个 syscall 本身仍在全局
mutex 内，是全仓库唯一一处"全局锁包住 syscall"。

**方案**：审计写入走带缓冲 channel + 单写协程，把 syscall 移出请求路径。**为什么待定**：要处理背压
（channel 满了是丢记录还是阻塞请求？）与关停语义（`Close` 要不要等队列排空？），复杂度明显上升，实测
未暴露问题。**触发条件**：审计落盘成为实测瓶颈时。

#### 1.2.8 [L] 探针请求完全绕过审计，`vmr report` 看不到探针成本

`internal/router/probe.go` 的 `runProbe` 直接发请求、读响应、报健康，全程不经过 `audit.Record`（本次
复核重新确认：`Router` 结构体不持有 `audit.Logger`，`audit.Record.Client` 也没有为无真实客户端请求的
探针场景准备任何 provenance 字段）。半开端点频繁抖动的场景下，这部分成本（token/延迟）持续不可见。

**问题的真实深度**（不只是"补一行日志"）：(1) `Router` 要接一个可选 `Audit *audit.Logger` 字段并在
`cmd_start.go` 里接好；(2) `audit.Record.Client`（`Exchange` 类型）不是指针、`Request Message` 也没
`omitempty`，写一条探针记录要么塞假的空 `Client.Request`，要么放宽 schema；(3) 必须先决定 `vmr report`
的可靠性/延迟/成交量统计要不要排除探针记录——已有的同类先例 `ReplayOf` 目前在 `report` 侧**完全没有
特殊处理**，照抄这个先例大概率不是探针记录真正需要的行为。

**方案**：`Router` 加 `Audit *audit.Logger`（nil=未接入，与 `server.Server.audit` 的"nil=disabled"
惯例一致）；`audit.Record` 加类似 `ReplayOf` 的 provenance 字段（如 `Probe bool`）；`runProbe` 写一条
不含完整 body 的精简记录；`report` 侧显式决定探针记录在哪些章节参与/排除统计。**为什么待定**：贯穿
`router→audit→report` 三层，核心难点不是代码量，是"探针记录在 report 里该怎么呈现"这个产品判断——没
做这个判断之前仓促定 schema 大概率要返工。

#### 1.2.9 [L] `/admin/status` 未暴露 `config.Check()` 的操作性告警

**已落地部分**（本次复核确认属实）：`cmd_start.go` 已在启动和每次热重载（fsnotify/SIGHUP/服务自动
重启）时调用 `cfg.Check()` 并把结果打成日志 `WARN config check: ...`（`logConfigCheckIssues`，
`cmd_start.go:106,179`）。此前该检查只在人工运行 `vmr check`/`vmr diagnose` 时才跑，热重载路径完全
缺席。

**未做的部分**（本次复核确认仍未做，`internal/server/admin.go` 无 `config_check_issues` 相关字段）：
这些 `WARN` 目前只进日志文件，`/admin/status` 里看不到。`/admin/status` 已有 `config_stale`（配置文件是
否比运行中的新）这个信号，`Check()` 的结果是同一类"配置合法但操作性可疑"的信号，逻辑上应该并列。

**方案**：`instanceBlock`（`admin.go`）加一个 `config_check_issues` 字段，与 `config_stale` 并列渲染
最近一次 `Check()` 的结果。**为什么待定**：日志里的 `WARN` 已解决"完全没有信号"这个最紧迫的问题，
`/admin/status` 只是让信号在不翻日志时也能看到，价值是锦上添花而非从无到有，牵连 `ReloadState`/
`admin.go` 及其测试，留作后续一起做更划算。

#### 1.2.10 [M] 分析半区体量持续增长，与"单人可维护"目标的张力（产品/战略层面，需用户裁定）

**证据**（本次复核重新实测，数字比架构审查当时略高）：`report`（6828 行）+ `story`（5456 行）+
`ctxgraph`（1408 行）+ `chatmsg`（1026 行）+ `i18n`（2471 行）= **17,189 行，占生产代码约 54.3%**
（生产代码总计约 31,664 行）——已经超过路由半区（约 14,475 行，45.7%），且这个占比仍在缓慢上升
（架构审查当时测的是 53.5%）。两个"体量已开始产生维护摩擦"的具体信号：§1.2.5 的 `aggregate.go`
逼近 archtest 预算；`newUserWindow` 常量曾在 `report`/`story` 两包间物理重复（已修复，见 §3，但是
这类"同一语义被独立抄两份"的风险在体量持续增长时会更容易复现）。

**这不是代码质量问题**——`chatmsg`/`ctxgraph` 被独立评为全项目架构最优雅的部分之一，`report`/`story`
的整体架构（聚合 vs 渲染分离、一节一文件、archtest 强制的行数预算）经抽样核实没有发现问题。**是一个
范围问题**：这一层是全项目里"功能面还在被外部战略文档持续推动扩张"的唯一部分（agent 运行时 47 维
分析、Web UI 等未来方向），"架构支撑得住扩张"不等于"应该无限往里加"。

**建议**（需用户裁定，不代为下结论）：新的、探索性的分析维度（需要不断试错阈值的指标）优先作为消费
`vmr-report.json`/`journey-*.json` 稳定 JSON 契约的**外部脚本**验证，证明真实语料 ROI 之后再决定要不要
收进二进制；已经完成真实语料校准、证明了价值的现有能力（九项行为指标、Finding 检测器、compare/corpus）
不需要现在改动。如果用户认同这个准入门槛，最终应该沉淀成 `internal/archtest` 里一条可执行规则（比如
按目录统计导出函数/类型数量、或给 Finding 检测器数量设上限），而不是停留在文档共识——`archtest` 目前
只覆盖"行数"和"import 边界"两个维度，"功能面膨胀"是它现有机制的盲区。

#### 1.2.11 [L] 图片降采样缓存无物理磁盘容量上限

`internal/imgprep/cache.go` 的 `sweepCacheDir` 只按 `image_cache_ttl_days`（缺省 7 天）做基于 mtime
的定期清理，没有总字节数上限。风险有限——缓存的是降采样后的 JPEG（受 `image_downscale_max_px` 约束，
单文件通常几百 KB 量级），要在 TTL 窗口内撑爆磁盘需要海量互不相同的高分辨率图片持续涌入，当前无实际
故障证据。

**方案**：加 `image_cache_max_mb` 配置项 + 按 mtime 的 LRU 淘汰（sweep 时若总字节数超限，从最旧条目
删起直到阈值下）。**为什么待定**：需要新配置项、双语文档同步（`config.example.yaml`/`.zh.yaml`）、
淘汰逻辑与测试，改动面相对于当前风险偏低。**触发条件**：真的观测到磁盘压力（而非假设）时再做。

---

### 1.3 其他（一句话，不展开）

- **`docs/future-strategy/` 目录归档未完成，本次复核更正一处失实记录**：该目录当前堆积 8 份文档
  （`agent_runtime_analysis_v1.0_custom-2-agent.md`、`VMR_综合评审与发展建议_report_v2.md`、
  `vmr_competitiveness_future_strategy_independent_review_agent.md`、`vmr_future_strategy_v2_sonnet-5.md`、
  `vmr_story_deepdive_devplan_sonnet-5.md`、`vmr_story_journey_deepdive_sonnet-5.md`、
  `vmr_strategy_review_opus-5.md`、`vmr_strategy_synthesis_gemini-3.6-flash.md`），内容高度重叠
  （至少 4-5 份都在做"战略分析/竞争力评估"这同一件事），`docs/future-strategy/archived/` 目录**不存在**。
  旧架构审查文档曾记录"已处理"（声称把三份文档 mv 进 archived/），但该记录与当前实际状态不符，文件名
  也对不上现有文件——本文档将其改列为**仍未处理**。建议区分"仍在指导当前方向的结论性文档"与"一次性
  分析素材"，把已被后续合成文档吸收的原始素材移进 `archived/` 子目录，只在顶层保留 1-2 份当前生效的
  权威结论文档。优先级低，不影响代码正确性，纯粹是文档卫生。
- **`vmr report` 的额度燃尽看板（`section_quota.go`）未交付**：对"最大化套餐效益"这个核心目标有直接
  价值（每个套餐的燃尽曲线、该优先烧哪个），但排在"让 $ 列更准"（`metric: cost`，已在 §3 简化）之后
  交付，目前尚未开工。
- `config.go` 629/750 行，逼近 archtest 预算但还有余量——`validate()` 本身已超过 200 行，混合 provider
  校验、model/endpoint 校验、`providerModels` 集合收集。建议下次改动时主动拆成 `validateProviders`/
  `validateModels` 两个私有函数，不需要等撞线，现在拆的成本远低于撞线后临时拆。
- `PricingOverrideConfig.Currency`（覆盖行级独立币种）是多币种功能面里复杂度收益比最低的一个角落——
  可以用"用户自己换算好再填"完全替代，零代码成本。**这是一个会改变用户可见配置 schema 的功能移除**，
  与 §3 已经处理的"时间窗"是两个独立功能面，不应该顺带一起砍，留给用户单独裁定要不要在未来某次一起
  简化掉。
- `go.mod` module 名仍是裸 `vmr`，未改成完整仓库路径——机械替换会触碰几乎每个 `.go` 文件的 import
  路径，diff 体量大，建议作为一次独立、单一目的的 commit 处理，不要和其他改动混在一起。
- `IngressPath` 写死 openai/anthropic/openai-responses 三协议 case（`internal/router/router.go`），加
  第四个协议时需记得同步改（可挪进 `Adapter` 接口）。
- `audit.retentionDays`/`extraRedactHeaders` 是包级 atomic 全局变量，`SetRetentionDays`→
  `RetentionDays` 的往返不变式没有专门测试锁定（同仓库的 `imgprep` 对同类问题用的是显式参数，是一处
  一致性瑕疵，非 bug）。
- `report/detail.go` 的 `sanitizeName` 不去重连续 `-`（`[^A-Za-z0-9._-]+` 的 `+` 量词已合并大部分场景，
  但合法 `-` 与替换产生的 `-` 相邻时仍会留下 `--`）。
- `adapter/classify.go` 的 `contentHint` 词表含裸 `"sensitive"`，会命中 `"case-sensitive"` 等无关措辞
  （已知取舍：误判仅多一次无害 failover）。
- 顶层字段扫描器对畸形 JSON"宽进"，依赖上游 `json.Unmarshal` 先行校验——当前所有入口都满足这个隐式
  前提，但没有注释写明。
- `go.mod` 无 `toolchain` 指令（声明 go1.25.1，本机实测更高版本）。
- `.gitignore` 全局忽略 `*.jsonl`/`*.jsonl.zst`，但已有 `!/examples/*.jsonl` 白名单例外，当前也没有
  任何测试需要在 `examples/` 之外提交 `.jsonl` fixture——核实后是个假问题，没有可动的手。
- `vmr.sh`（609 行，dev/service 双模式）+ `vmr-loadtest.sh`（76 行）无脚本行为测试，关键路径靠人工
  验证——CI 的 `shellcheck` job 只是静态检查，不是行为测试，plist/unit 渲染是否真的能被 launchd/
  systemd 接受这类问题它照样测不出来。
- `loadtest/runner/main.go` 的 `defer mock.Process.Kill()` 用 SIGKILL，会在日志里留下"有 START 无
  STOP"的假崩溃痕迹（正常退出路径已用 `os.Interrupt`）。
- `loadtest/gentargets/main.go` 的 `f.Write(line)` 与 `defer all.Close()` 错误被忽略，磁盘满时
  `targets.json` 可能静默截断。
- P3-3 分级处置里明确保留不动的 ~23 处引用（`docs/VirtualModelRouter_Design_v4_{Core,Analytics}.md`
  形式的编号引用，已带文件名）：CLAUDE.md 收紧后的原则是"只引用文档名字和章节名称，不用编号"，这些
  从严格意义上仍不完全合规，但号码旁边已经有文件名兜底，紧迫性明显低于已清掉的裸编号和死引用。
- `internal/router/transport.go` 的 `copyFlush` 在 idle 超时/写错误两条返回路径上不等读 goroutine 真正
  退出就返回，`forwardSuccess` 随后读 `rbody.Applied()`/`RawPreStrip()`/`ObservedModel()` 存在既有数据
  竞争（触发条件窄：上游中途卡住超过 `stream_idle` 超时，或客户端写入失败；最坏后果是审计记录里
  `norm`/`upstream_model` 字段读写竞争，不会导致客户端收到错误响应）。判定"下次因其他原因触碰这两个
  文件时顺手修"，不单独立项、不单独测试验证。
- CI（`.github/workflows/ci.yml`）覆盖 `go vet`/`go build`/`go test -race`（ubuntu+macOS 矩阵）+
  `gofmt -l` + `shellcheck`。`staticcheck` 评估后未采纳——见 §2.3。
- 缺 `CHANGELOG.md`/`CONTRIBUTING.md`（≤3 人内部项目，靠 commit message 追踪，见 §2.3 的产品判断）。
- `admin.go` 判断 loopback 用 `net.ParseIP(host).IsLoopback()`，对带 zone 的 IPv6 loopback（`::1%lo0`）
  没有专门测试——`net.ParseIP` 对带 zone 的地址返回 `nil`，`nil.IsLoopback()` 不会 panic（`net.IP` 是
  `[]byte`，方法体只检查切片长度）、返回 false，fail-closed 方向无害，只是极端场景下 admin 端点会被
  误拒绝的可用性小瑕疵。
- YAML 解析错误信息不含行号（`yaml.v3` 库本身的限制，非 vmr 代码可控）。
- `vmr.sh` 的 `write_env_file` 用 `printf '%s=%s\n'` 写值，值含换行或特殊字符时 launchd/systemd 的
  EnvironmentFile 解析可能出错（API key 现实中不含空格，实际触发概率低）。
- **标准定价表四分量覆盖率偏低**：`cache_read` 仅约 23%、`cache_write` 仅约 8%，国产第一方厂商普遍
  缺失，是 `metric: cost` 可用性的门槛之一。这是持续的数据维护工作（靠社区/手工补
  `standard_price_curated.yaml` 推进）而非一次性代码任务，不排期；§3 已把 `metric: cost` 的功能面
  收窄到静态按模型覆盖，紧迫性随之降低。
- **HTML 单文件渲染 + 脱敏模式未实现**：`vmr report`/`vmr story` 的 Markdown 产物含完整对话正文、
  文件路径、内部项目名，是分享给团队外部这个场景的硬门槛。设计文档已列为"可选扩展"（单文件自包含
  HTML 内联 CSS/JS + 脱敏模式，保留结构/指标、正文替换为长度占位符+类型标签）——架构上已支持
  （`vmr-report.json`/`journey-*.json` 是稳定的机器契约），只缺实现，不涉及架构改动。
- **`/metrics` Prometheus 端点未实现**：对"单人维护多个实例时用标准可观测性工具串联看板"有真实价值，
  落地成本低（`/admin/status` 已暴露大部分需要的数据，改成 Prometheus 文本格式是格式转换，不是新的
  数据采集），排在路线图里但尚未开工。
- **系统提示词版本化时间线未实现**：`story.Step.SysChanged`/`report` 的同名信号已能检测"这一轮系统
  提示词是否变化"（布尔量），但没有"一个 Journey 里出现过哪些不同版本、分别何时切换"的时间线视图。
  基础漂移检测已存在，缺的是更完整的呈现，优先级低。
- **`response.go` 的错误分类/quirk 触发点未来可能需要从"函数内 if 链"演化成可插拔检测器列表**：
  `DefaultClassify`（`adapter/classify.go`）与 `response.go` 的 `classifyEvent`/`decide` 目前都是
  单体函数，靠代码注释固定判断优先级。当前规则量级下 if/switch 链仍是最简单的形态，不需要现在改——
  但如果未来继续为新厂商在这些函数里加分支（尤其 `response.go`，新增一种响应端异常检测要同时改
  `classifyEvent`/`decide`/`emitBlock`/`finalizeBuffered` 四个函数），会逐渐失去"新协议/新 quirk =
  新文件"这种隔离性。已被认知到的未来风险，非当前问题。

---

## 2. 已知问题，确定不修复

> 这些问题**不是没看到，是看到了、评估过、主动选择不处理**。每条都给出理由，理由站不住脚时应该重新
> 打开讨论，而不是被这份清单本身当挡箭牌。

### 2.1 架构/设计层面的刻意取舍

- **`health.Registry` 的全局互斥锁不分片**——实测约 600 次加锁/秒、锁内是纳秒级 map 访问，分片是纯
  过度设计。
- **`HealthKey` 的 sha256 前 4 字节截断**——碰撞概率 2⁻³² 量级，可忽略。
- **健康冷却参数硬编码**（`transientBase=2s`/`transientCap=5m`/`longBase=10m`/`longCap=1h`）——"零调参"
  是既有设计选择：这类参数很难在没有真实生产数据支撑的情况下让用户自己校准出更好的值，暴露成配置项
  只会增加配置面复杂度，收益存疑。
- **`ErrorClass.String()` 的 `default` 返回 `"transient"` 而非 `"unknown"`**——刻意为之，防止报表的
  error_classes bucket key 无界增长；`default` 分支实际不可达。
- **`${VAR}` 未定义时静默展开为空串，且不支持 shell 风格的 `${VAR:-default}` 默认值语法**——
  `internal/config/config.go` 的 `expandEnv` 正则只认 `\$\{[A-Za-z_][A-Za-z0-9_]*\}`，`:-` 这类语法
  完全不匹配、会原样残留在展开结果里不被替换。都是刻意取舍：未定义展开为空串已文档化
  （`docs/VirtualModelRouter_Design_v4_Core.md`），`vmr diagnose` 的 api_key 检查是现有缓解；需要
  默认值时直接在 YAML 里写字面值即可，成本接近零，与项目"环境变量只做显式单一用途、不留隐式旋钮"的
  一贯哲学一致，没有加 shell 风格默认值解析的实际诉求，按 YAGNI 不做。
- **`report/render.go` 的 `reassembleSSE`（语义重组）与 `router/response.go` 的 SSE 状态机是两套独立
  实现**——一个字节级保真增量转发、一个整体语义提取，关注点不同，合并成本高于收益。
- **`internal/adapter/openai` 与 `anthropic` 各有约 4 行的 header 拷贝循环**——凭证 header 名字与格式
  不同，抽公共函数收益不明显；三个协议适配器 `BuildRequest` 结构高度重复（约 30 行骨架三份几乎相同），
  同理不建议抽象——凭证 header 名（`Authorization: Bearer` vs `x-api-key`）、`anthropic-version` 透传
  这类协议差异本身就要求"看得到全貌"，硬抽参数化骨架函数不会减少认知负担，反而让"这个协议到底发了
  什么请求"从"读一个 70 行文件"变成"读一个 70 行文件 + 跳转到公共函数"。
- **`imgprep.ImageInfo` → `audit.ImageInfo` 的 20 行字段抄写**——换来 `imgprep` 不依赖 `audit`，是包
  边界的合理代价。
- **`copyFlush` 的 goroutine + channel + 每 chunk 一次堆分配**——唯一替代路径是在 `DialContext` 里包
  `net.Conn` 做 `SetReadDeadline`，但 deadline 会覆盖整条连接生命周期（含 TLS 握手、响应头阶段），与
  现有 `TLSHandshakeTimeout`/`ResponseHeaderTimeout` 语义重叠，且只能靠真实 TCP 往返验证。
- **把 `Dimension`/`Condition`/`WithinContext` 合并成统一的 `Filter` 接口**——soft 语义目前只有一个
  成员，抽象一个只有一个实现的接口违反 YAGNI；触发条件是出现第二个 soft filter。`Dimension`（排序）
  与 `Condition`（淘汰）刻意分成两个不同形状的接口而不是合并，是对的决定：`Condition.Eligible` 需要
  看到请求内容（`RequestFacts`），`Dimension.Compare` 只比较两个端点、看不到请求，合并会造成接口污染。
- **端点级 `quirks: [...]` 声明式配置**——会引入新配置概念且用户须理解各厂内部行为才填得对；触发条件
  是出现第三个厂商 quirk，或发生真实误伤。更窄的替代方案（只给 `responsefix.go` 里已存在的、真正会
  改字节的 MiniMax 修复加一个 provider 级 on/off 开关，如 `response_fix: [minimax_think,
  minimax_thinking_process]`）评估后判断实现成本比看起来高（要贯穿 `config`→`core.Endpoint`→`router`
  三层），比通用 quirks 系统更值得做，但量级仍属独立任务，未实现。
- **把 `report` 拆成独立二进制**——二进制 12MB→7MB 不是真实收益，却要牺牲"单二进制"这一核心定位
  （`report` 实测占自有代码符号 55%，182KB/335KB）。
- **引入 DuckDB/cgo 做聚合**——与"纯 Go、无 cgo、自包含"定位直接冲突。
- **用 `text/template` 重写 Markdown 渲染**——条件列、对齐、`⭐`/`¹`/`⚠️low-n` 脚注在模板里更难读；
  已用 `mdTable` helper 收掉重复的表头/分隔符拼接，是更有据的中间方案。
- **为 `router→audit` 上事件总线**——当前的可变 Record 传递性能最优；15 处 `if att != nil` 的噪音已由
  nil-safe 的 `SetXxx` 方法收敛。
- **统一 `cmdCheck`/`cmdStart`/`diagnose` 三处的路由表打印格式**——排序逻辑已统一到 `EffectiveOrder()`，
  剩下的纯格式化差异不值得再抽象。
- **`archtest` 的文件行数预算机制不覆盖"功能面"**（Finding 检测器数量、报表章节数量）——这类"这一层
  的复杂度还在可控范围内吗"的问题，行数预算只能间接反映。已知机制盲区，不是缺陷；如果 §1.2.10 的
  准入门槛要落地，需要一种新的度量方式（比如按目录统计导出函数/类型数量），而不是继续加行数预算。
- **`core.WriteJSON`/`WriteError` 是行为函数而非类型，混在"零依赖共享类型包"里**——与 `core` 自身定位
  略有偏差，但只有两个函数，拆一个新包换来的"分类洁癖"收益小于多一层 import 的成本。只在未来继续往
  `core` 加类似"两处都要用的行为"时才值得重新评估。
- **`core.go`（580 行）一个文件装七个不同领域概念**（路由类型、错误分类、额度类型、定价类型、token
  估算、泛型工具函数）——目前体量还可控，是全项目"概念密度"最高的单个文件，但都是小型、稳定的数据
  结构，拆分本身的收益（文件更小）小于成本（多一层 import，路由核心概念反而更难一眼看全），不建议
  现在拆。**建议今后新增的运行态类型优先考虑独立叶子包**（`quota`/`pricing` 已经示范了"零依赖叶子包
  + 只有 core 依赖它"的更优模式），不要再往 `core.go` 里加新领域。
- **`adapter/classify.go` 的 `DefaultClassify` 是单体函数，规则扫描顺序靠注释固定，没有用查表/优先级
  列表表达"这是一个有优先级的规则链"**——目前规则不多（5 条，含 `ErrContextLimit`），可读性尚可，
  保持现状不动，不需要现在重构成查表结构。**触发条件**：真的加到第 8、9 条规则时再考虑要不要用
  数据驱动的规则表替代硬编码分支链。
- **`config.go` 的 `validate()` 在 provider/model 校验循环里顺带收集 `providerModels` 集合**（供
  `resolvePricing` 使用）——"验证循环里夹带数据收集"，职责略混，但代价很小，属于纯粹的代码组织
  洁癖，不值得单独立项；如果 §1.3 提到的 `validateProviders`/`validateModels` 拆分将来真的做，可以
  顺手一起理顺。

### 2.2 配置/schema 层面的刻意取舍

- **`audit.Attempt.RawPreStrip` 字段类型是 `any`，看起来应该收窄成 `json.RawMessage`**——**核实后判定
  方案本身不成立，不是"待定"而是"不该做"**：唯一构造点 `EncodeBody`（`internal/audit/audit.go`）按
  内容是否为合法 JSON 在 `json.RawMessage` 与 `string` 之间二选一返回（非 JSON 的原始 SSE 文本必须走
  `string` 分支），`RawPreStrip` 因此本来就必须是 `any`（`json.RawMessage | string` 的联合），收窄成
  纯 `json.RawMessage` 会在非 JSON 场景丢数据或直接 panic——与 `Message.Body` 是完全相同的模式（同一个
  `EncodeBody` 产出）。
- **README 的 `/admin/status` 示例"无需 api_key"**——与实现一致，单机单用户场景可接受，已文档化。
- **`AllPathsComplete` 不主动提示"一条无条件 override 排在一条有时间窗的 override 前面导致后者不可达"**
  ——这条建议随 §3 的时间窗功能面移除已经**用更强的形式解决**：时间窗砍掉后，这类死代码判定从"需要
  遍历时间轴的可达性分析"降级成一次线性 dedup，成本几乎为零，直接做成了 `internal/config` 里
  `firstDeadOverride` 的加载期硬错误（拒绝而非警告）。

### 2.3 产品/流程层面的刻意取舍

- **CI 未接入 `staticcheck`**——评估后未采纳：本机无网络环境无法预先验证，25,000+ 行生产代码首次接入
  大概率冒出大量未经筛选的既有发现，没有时间预算逐条判断真假阳性，贸然接入可能让 CI 从下一次 push 就
  直接变红，风险大于收益。
- **不维护 `CHANGELOG.md`/`CONTRIBUTING.md`**——≤3 人内部项目，靠 commit message 追踪足够，维护变更日志
  的边际收益低。
- **`buildinfo` 刻意不编造 semver，只报告 VCS commit 短哈希**——没有能递增语义化版本号的发布流程，编造
  一个没人维护的版本号比诚实报告 commit SHA 更容易误导人。**这个判断的前提是"当前阶段"**：如果未来
  `vmr` 的使用者从"看得懂 commit SHA 的开发者"扩展到更广受众（比如通过 Homebrew tap 触达的普通用户），
  "这个版本改了什么"会变得更重要，届时可以引入 tag 驱动的语义化版本号——不需要现在做，值得随受众变化
  重新评估。
- **官方用量 API 校准（根治本地额度计数漂移的手段）未做，且刻意不预先抽象 `Source` 接口**——设计文档
  已确认存在私有用量查询接口，但选择"等写第一个真实适配器时再抽象"，避免投机性抽象。这是合理的克制，
  不是遗漏，本地计数漂移（绕过 vmr 的流量、单位换算偏差、时段倍率）在此之前会持续存在，属于已知限制。

---

## 3. 已解决

> 精简记录，只列"做了什么、为什么算解决"，不复述完整改动细节（细节见对应 commit 与代码注释）。

- **`ErrContextLimit` 分类盲区**：新增 `core.ErrContextLimit`（`ReportNeutral` 语义，零冷却，继续
  failover），`internal/adapter/classify.go` 新增 `contextLimitHint`（上下文超限词表）与
  `maxOutputHint`（输出长度参数超限的窄嗅探，必须排在前者之前，否则会被误吞），区分"会话历史超限应
  failover"与"请求自身 `max_tokens` 超限换端点也没用"两种措辞形态。此前"maximum context length
  exceeded"类措辞会被误判成 `ErrClient`，直接返回客户端而不触发 failover，是当时全项目唯一实质性
  削弱 failover 卖点的分类盲区。测试：`classify_test.go` 的 `TestDefaultClassify_ContextLimit`、
  `router_serve_test.go` 的 `TestServe_ContextLimitFailsOverWithoutCooldown`。
- **TokenPlan `metric: cost` 定价引擎复杂度收窄**：`internal/pricing`（880 行）+
  `internal/config/pricing.go`（403 行）曾支撑一个完整账单系统级别的费率解析引擎，而 router 侧真正
  消费它的代码只有约 25 行——比例失衡。裁定方案：保留 `metric: cost` 与三层费率解析（账号覆盖→补充
  表∪标准表→无费率），砍掉 `PricingOverrideConfig` 的 `date_from`/`date_to`/`hour_from`/`hour_to`
  分时/限时促销功能面，只保留静态按模型 `discount`/显式费率覆盖。`resolveChain` 不再需要 `eligible`
  时间谓词参数，`AllPathsComplete` 从"对每条 Override 单独跑一次可达性分析"降级成一次线性 walk。
  意外收获：时间窗去掉后，"同一 model 出现两条 override"从"用时间窗合法区分促销/常态价"变成必然的
  死配置，顺手加了 `firstDeadOverride` 加载期硬校验。影响面：`core.PricingOverride`/`PricingSpec`、
  `router/quota.go` 的 `chargeCost`、`pricing/resolver.go` 的 `RateFor`（去掉不再需要的 `ts` 参数）、
  两对配置示例文件、`UserGuide.md`/`.zh.md`、`TokenPlan_Quota_Routing_Design_opus-5.md`、
  `VirtualModelRouter_Design_v4_Core.md`、`CLAUDE.md` 模块地图，约 15 个测试用例。
- **`response.go` 缺行数预算**：`internal/archtest/file_sizes_test.go` 的 `fileLineLimits` 加了
  `"internal/router/response.go": 850`（按既有条目 ~15% 余量惯例，从注册时的 736 行取整）。此前
  `response.go` 是全项目认知密度最高的文件之一（三种传输模式状态机 + SSE 事件切分 + MiniMax 双形态
  思考泄漏修复触发判断 + 额度 usage 嗅探），却是"行数预算"这套护栏机制唯一的覆盖盲区。
- **`newUserWindow` 常量跨包重复**：`internal/report/session.go`、`internal/story/journey.go` 各自
  独立声明 `const newUserWindow = 8`（同一语义、同一数值的物理重复，没有测试锁定两者必须一致），
  下沉为 `chatmsg.NewUserWindow`（选 `chatmsg` 而非 `ctxgraph`——常量语义是"消息列表位置窗口"，属于
  消息级词汇表），两个消费方改为引用。
- **`core.TokenWeights` 零值陷阱**：新增 `core.NewTokenWeights()`（返回全 1.0），`config/quota.go`
  与 `cmd/vmr/cmd_check.go` 的 `printProviderQuota`（复核时发现的第二处独立同款字面量，原先"唯一
  生产构造点"的判断实际已经不成立）都改用它，`core.DefaultTokenWeight` 不再被外部直接拼字面量引用。
- **quirk 修复聚合展示**：`soft_block_detected`/`thinking_process_pattern_detected`/`think_strip`/
  `thinking_process_strip` 等标记此前已进审计 `norm` 字段，但没有消费方把它们聚合成可读的章节/指标
  （核对仓库现存 9101 份 `reports/details/*.md`，命中率约 9%-19%，不是罕见边缘情况）。落地：
  `report/aggregate.go` 的 `addAttempt` 按端点聚合 `EndpointRow.NormCounts`（只统计四个真正的 quirk
  标记，`model_rewrite`/`opaque` 等结构性标记显式过滤掉，否则会被 ~100% 命中率的噪音淹没），
  `section_reliability.go` 新增"Quirk Fix × Endpoint"表。顺手删除了 `report/session.go` 里写而不读
  的孤儿字段 `ReqInfo.norm`（按"最后一条非空 Norm"取值，在 failover 场景下会丢掉更早 attempt 的
  记录，不适合做这个聚合）。`vmr story` 暂未接线。
- **`loadtest` 地址常量三处人工同步**：新增 `loadtest/addr` 包，`runner`/`gentargets` 两个 Go 源改为
  引用同一常量，降为"一处 Go 常量 + 一处有注释指引的 YAML 人工同步点"（`config.yaml` 本身没法 import
  Go 常量，这一处物理上消不掉）。
- **`router` 包单文件过大**：`router.go` 曾长到 948 行（自设警戒线约 550），现已按职责拆成
  `router.go`(586)/`snapshot.go`/`limiter.go`/`transport.go`/`logfmt.go`。
- **`response.go` 里厂商 quirk 与通用路径纠缠**：MiniMax 专属知识（`<think>` 剥离、Thinking-Process
  剥离、soft-block marker、泄漏观测）已抽到 `responsefix.go`，`response.go` 只留通用状态机。
- **架构不变式只存在于文档**：`internal/archtest` 已把"import 边界"与"核心文件行数上限"变成可执行
  测试。
- **`config→sticky`/`replay→server` 的不必要依赖**：`core.StickyBackstopTTL`/`core.FilterClientHeaders`
  已成为共同依赖，`go list` 实测两条边均已消失。
- **`core` 是公共抽屉**：展示格式化 `FmtBytes`/`FmtTokens`/`FmtSeconds` 已拆到 `internal/fmtutil`。
- **热路径重复推导不可变值**：`Endpoint.Freeze()` 在 `BuildSnapshot` 里预计算 `healthKey`/`name`，
  消除每请求约 11 次 SHA-256。
- **注册表读路径加锁**：`adapter.registry`/`strategy.conditions` 改为 `atomic.Pointer` copy-on-write，
  读路径无锁（写路径 mutex 不可省，有 `-race` 测试锁定）。
- **审计写入把 JSON 编码串行化**：编码已移出锁，改用 `sync.Pool` 缓冲（syscall 本身仍在锁内，见
  §1.2.7 待定项）。
- **`cmd/vmr` 单文件 870 行混杂 8 个子命令**：已按子命令拆分，`main.go` 只剩 71 行的 dispatch/usage/
  adapter 注册。
- **ingress 用 `json.Unmarshal` 全量解码只为取两个字段**：已换成 `adapter.TopLevelProbe`（剩余重复
  扫描见 §1.2.6）。
- **`ErrorClass.String()` 缺显式 case**：10 个声明值现已全部有显式 case。
- **详单文件链接用裸 HTML `<a href>`**：已改为 Markdown 语法。
- **设计文档与代码漂移两处**（`CountNested` 已导出但文档仍说"三份拷贝"、`router.go` 550 行上限已被
  突破 72%）：文档已更新，且行数上限已由 `archtest` 强制。
- **`vmr report` 需要第三趟扫描导出详单**：详单渲染已并入聚合趟（剩余两趟见 §1.2.1）。
- **`priority.Compare` 整数溢出、3xx 重定向未处理**：已修复。
- **MiniMax thinking 剥离失效无观测**：守卫未命中但内容仍具编号推理小节形状时打观测标记，不改字节。
- **truncated 请求端点归属为空**：回退到"最后一次拿到 2xx 响应头的 attempt"。
- **compaction 链接漏链静默**：未匹配到 successor/predecessor 时补一条 debug 日志。
- **`RewriteModel`/`RewriteStream` 缺 fuzz 测试**：已加 fuzz 测试，过程中发现并修复一个真实 nil map
  panic（对 JSON 字面量 `null` 输入）。
- **五处一行级瑕疵**（`purgeOne` ENOENT 噪音日志、`ms()` 1000ms 边界、`Redact` 浅拷贝、
  `WriteRequestsJSONL` 吞 Close 错误、`vmr.sh` `ExecStart` 缺引号）：全部修复，均有对应测试。
- **单请求最坏内存约 104MB 偏大**：`router.bufferedCap`（32MB→8MB）、`server.recorderBodyCap`
  （64MB→16MB）已降档，`config.MaxRequestBodyMB`（8MB）不变，三者按统一推导（1M-token 上下文窗口
  约 3-4MB 字节量 + ~2 倍余量）重新取值，单请求最坏驻留降到约 32MB。
- **`vmr replay` 消耗真实上游额度但不计费**（2026-08-11）：`chargeQuota` 的 metric 分发 +
  `model_multipliers` 缩放 + `cost` 定价尾段抽成导出函数 `router.ChargeResponse`，`router` 的流式路径
  与 `internal/replay` 的一次性路径共用同一实现。`replay.Run` 加载 `<log_dir>/vmr-quota.json`（与
  `vmr start` 同一份状态文件）→ 成功响应（状态码 `< 400`）后计费 → 返回前 flush 一次；usage 用
  `chatmsg.MergeUsageBytes` 从已完整缓冲的响应体里取（而非 `respStream` 的增量嗅探），拿不到时降级
  为对请求/响应体分别跑 `core.EstimateTextTokens`。`-dry-run` 与未配置 `quota:` 的 provider 均不触碰
  状态文件。未覆盖：与另一个正在写同一状态文件的进程（如运行中的 `vmr start`）并发时没有跨进程锁，
  这是 `TokenPlan_Quota_Routing_Design_opus-5.md`"多实例共享计数：不做"这条既有取舍的一个具体表现，
  不是新问题。

---

## 4. 使用约定

本文档是**唯一**需要跟踪的当前状态。往后每次核实或处理，**直接在本文档的对应条目上更新或删除**，不再
另起新报告；只把仍然成立的新问题并进来。§3（已解决）按项目惯例允许累积式追加（`CLAUDE.md` 明确把
`OUTSTANDING_ISSUES`/`KNOWN_ISSUES` 这类文档列为"文档是当前状态、不是 changelog"这条规则的例外），
但 §1/§2 必须保持"当前仍然成立"——一旦某条待定项被处理或某条不修复的判断被推翻，立刻从原位置删除或
挪到 §3，不要留着两处同时存在造成矛盾。

在给出任何"已修复"结论之前，**用代码实测核对（grep/行数/测试是否存在），不要只转述上一版文档的自我
陈述**——本文档 §1.3 记录的 `future-strategy/` 归档失实案例就是"自我陈述与代码现状脱节"的一次真实
教训。
