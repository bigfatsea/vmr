<!-- Ver 2026-07-30 12:00, by Sonnet 5 -->

# Virtual Model Router (vmr) — 设计方案 · Part 2：报表与叙事（Analytics）

本文档描述 vmr 审计日志的两个离线消费方：聚合报表 `vmr report`（`internal/report`）与 Agent 任务叙事重建 `vmr story`（`internal/story` + 支撑层 `internal/ctxgraph`/`internal/chatmsg`）。读完即可维护与二次开发这一侧。使用文档见 `README.md`/`README.zh.md`、`docs/UserGuide.md`/`UserGuide.zh.md`。

**这是 v4 版设计文档的 Part 2（共两部分）**：路由核心（虚拟模型、协议透传、Adapter、调度与健康、审计日志格式本身等）见姊妹文档 `docs/VirtualModelRouter_Design_v4_Core.md`（Part 1）。两份文档只通过审计日志的 JSONL 格式耦合——本文档描述的一切都是**离线、只读**地消费 Part 1 §9.2 定义的 `audit.Record`，不影响、也不参与任何实时路由决策；`internal/report`/`internal/story`/`internal/ctxgraph`/`internal/chatmsg` 均不出现在 `internal/router`/`internal/server` 的依赖图里，这条边界由 `internal/archtest` 的可执行检查强制。

---

## 0. 定位

vmr 的审计日志（Part 1 §9）记录的不是"日志"，是同一份对话状态在时间轴上的完整快照序列——Agent 每一轮都会重发累积的完整历史。这个事实决定了两个产物的关系：

- **`vmr report`**：横向聚合。跨全部请求统计成本、延迟、错误率、缓存效率、会话/任务分布——回答"这段时间整体花了多少、哪里在浪费"。
- **`vmr story`**：纵向还原。把一条 Agent 任务的完整执行过程重建成可读的叙事流——回答"这一个任务具体是怎么做的、哪一步开始跑偏"。

两者服务四类具体场景，落在事实层（原始记录）/剖面层（规则派生指标）/解读层（§7 尚未实现的 LLM 注解）三层里不同的层：单 Agent 调优（"这次改了 prompt，工具调用是变多还是变少了"——剖面层，`vmr story` 的九项指标 + §3.7 对比）、跨框架/跨模型横向比较（"Claude Code 和 OpenClaw 跑同一类任务谁更省"——同样是剖面层，跨 Journey 对比）、事故复盘（"这次为什么突然开始乱猜文件路径"——事实层，`vmr story` 的完整叙事 + compaction 信息损失摘要）、上下文工程（"这个 Agent 的上下文预算都花在哪了"——`vmr story` 的上下文构成演化曲线 + `vmr report` 的 §1 token 经济）。四类场景没有一类需要 LLM 参与——这也是为什么剖面层先于解读层实现（§4）。

两者共享同一个底层事实来源——`internal/ctxgraph` 把审计记录建模成内容寻址的 manifest（消息哈希向量）序列 + 编辑分类 + lineage 图（见 §3），`vmr report` 的会话分组、`vmr story` 的任务叙事都是这张图上的查询，不是两套独立算法。这不是巧合：Agent 场景里"一个会话有几个任务""这次压缩丢了什么""这一步是不是新指令"这些问题，本质都是对同一份 manifest 序列做差分，用两套启发式各自回答一遍只会导致两者迟早不一致。

**为什么是离线而不是实时**：审计日志本身已经是内容寻址存储（全量原始 body 已在盘上、不可变、按时间有序），"归档"是冗余的——只需要一个索引，不需要数据库、版本链、保留策略。`internal/ctxgraph.Scan` 就是这个索引的构建过程；索引本身（manifest 哈希向量 + blob 位置表）对全量语料（7112 条记录、809K 条消息实例的实测规模）只有几十 MB，构建耗时以秒计，完全不需要常驻状态或后台任务。

---

## 1. 审计日志：唯一输入契约

两个产物都从 Part 1 §9.2 定义的 `audit.Record` JSONL 起步，不做任何假设之外的解析。这里只重述对本文档后续内容必要的字段（完整格式定义、六条约定见 Part 1 §9.2）：

- `client.request.body` / `client.response.body`：客户端 ↔ vmr 的原始请求/响应体（`vmr story` 的 manifest、`vmr report` 的会话分组都从这里取消息列表）。
- `attempts[]`：vmr ↔ 上游每次 failover 尝试，含 `endpoint`/`protocol`/`provider`/`model`/`error_class`/`norm` 等结构化字段。
- `ts`/`dur_ms`/`ttft_ms`：请求到达时刻、总耗时、首字延迟。
- `client_key_tag`：调用方标签（`vmr report` 按它分组导出，`vmr story` 的 Journey id 用它做客户端前缀）。

两个产物都支持混合读取明文 `.jsonl` 与历史压缩产生的 `.jsonl.zst`（`audit.OpenLogFile` 透明处理），且都能在未显式指定输入文件时回退到 `<config.yaml 的 log_dir>/vmr-audit-*`（`cmd/vmr/auditpaths.go` 的 `resolveInputPaths`，`vmr report`/`vmr story` 共用）。

---

## 2. `vmr report`：聚合报表

```
vmr report [-o dir] [-details=false] [-pricing pricing.yaml] <file|glob>...
    # 输出 vmr-report.json + vmr-report.md + vmr-requests.jsonl + vmr-requests.md（+ 按 client_key_tag 的 sibling）
    # + {out}/details/ 逐请求详单（-details=false 关闭；加载了定价配置才会渲染 §2 成本估算）
```

全部实现在 `internal/report` 一个包里：`aggregate.go`（聚合入口 `Build`）、`session.go`（会话/任务分组）、`rows.go`（`Report2` 及各 Row 类型 = `vmr-report.json` 的公开 schema）、`metrics.go`（派生指标）、`render_doc.go`（章节运行顺序 + 共享的 `mdTable`）+ 一个章节一个文件的 `section_*.go`（**新增章节 = 新增文件，不是把某个文件改大**，`internal/archtest` 的行数预算逼着遵守这条）、`detail.go`/`render.go`（逐请求详单）、`requests.go`（`vmr-requests.md` 索引）、`pricing.go`（定价 sidecar）。与审计记录格式强耦合：改动 `audit.Record` 结构必须同步改这个包及其测试。

### 2.1 两遍读取：`AnalyzeSessions` + `Build`

`Build`（`aggregate.go`）对同一批输入文件读两遍：

1. **`AnalyzeSessions`（`session.go`）**：把每条记录关联到 `internal/ctxgraph` 的 manifest（见 §3），按 `ctxgraph.Lineage` 分组出会话（`SessionInfo`）与任务（`TaskInfo`），同时提取会话分组之外的报表特征——工具签名、角色字符/token 统计、compaction 标记、chat_id、NoReply 检测等（这些是报表领域的关切，`ctxgraph` 不需要知道）。`AnalyzeSessions` 内部并发跑两条独立的文件扫描通道：一条是它自己对每条记录的 `collect()`（提取上述报表特征），另一条是 `ctxgraph.Scan`+`ctxgraph.StitchGraph`（构建分组用的 Lineage/Stitch 图）——两者读同一批文件、互不依赖，用 goroutine 并发而非顺序执行，避免"审计文件读两遍"变成两倍墙钟时间。
2. **`Build` 自身的第二遍扫描**：流式重新扫一遍同一批文件，按 `path:line` 用 `AnalyzeSessions` 的结果把每条记录接到分组坐标、usage、工具调用等，同时重算尚未提取的原始量（工具声明字节数、serving endpoint、错误类）。这一遍同时把每条记录的 detail 渲染也带上（见 §2.5），所以整个 `vmr report` 只需要对源文件读两遍，不是三遍。

`AnalyzeSessions` 失败（唯一的失败面是文件级 I/O：`OpenLogFile` 打开失败 / `ForEachLine` 扫描中途出错——单行 JSON 解析失败只是跳过计数，不会导致整体失败）即整个 `Build` 返回错误，`vmr-report.json`/`.md` 都不写出，不做分文件容错——现实中触发场景几乎只有一种：`vmr start` 常驻进程的 housekeeping 轮转扫描与 `vmr report` 并发读同一份日志时的竞态窗口，属于"重跑就好"的窄场景，不值得为它引入按文件粒度容错的复杂度。

**会话分组算法**（`session.go` 的 `group()`）：一个 `ctxgraph.Lineage` 对应一个 `SessionInfo`——Lineage 已经在结构上把 Contract/Fork 类型的历史重置切成了独立片段（见 §3.2），`group()` 不再自己判断"这是不是同一个会话"，只是消费这个既有分类。每条记录的"这一轮相对上一轮改了什么"（`DeltaStart`/`ReplacedTail`/`SysChanged`）来自 `ctxgraph.Classify(前一条记录的 manifest, 这一条的 manifest)`，不再有报表包自己的哈希向量/LCP 实现——历史上这是两套并行实现（`ReqInfo.keys` + 私有 `lcp()` vs `ctxgraph.Manifest.Keys` + `Classify`），现在统一成一套。任务边界判定（是否开新任务）是报表领域自己的规则，不受影响：新 trace id、或 delta 里出现一条不在父级历史里出现过的真实用户指令，就开一个新任务；父级回复是 NoReply（空回复或 OpenClaw 的 `NO_REPLY` 标记）时不开新任务，视为对同一指令的重试。

**跨会话链接，两条并列信号**：
- `linkStitchedLineages`：任何 Lineage 从其所在的 SessKey 桶断裂（Contract/Fork）又被 `ctxgraph.StitchGraph` 缝合回某个更早 Lineage 时，直接把 `SessionInfo.ContinuedFrom` 设成前驱会话的 ID——这是纯结构信号，覆盖同一 SessKey 桶内的断裂重连（典型：一次原地改写式的历史压缩，开场白锚点原样保留）。
- `linkCompactions`：报表自己识别的、独立发出的 compaction 摘要调用（system prompt 含"summarization"，或"无工具声明 + `max_completion_tokens` + 无 trace"的形状），用 200 字节文本子串匹配把它与压缩前/压缩后两个会话关联（`c.Summarizes`/`c.ContinuesTo`），并在 `ContinuedFrom` 仍为空时补上跨会话链接。

两条信号**故意不是同一套机制的两种实现，而是覆盖不同场景的互补信号**：`ctxgraph` 的缝合基于精确消息哈希匹配，一次真正的历史重写（摘要调用的输入是渲染过的对话文本，不是逐字消息）往往与前后会话没有任何逐字消息重合，哈希倒排索引在这种情况下没有信号可用；文本子串匹配能覆盖这个盲区，代价是精度较低（200 字节的子串巧合命中理论上可能，实践中未观测到）。

### 2.2 数据形状：`Report2`（= `vmr-report.json` 的 schema，`meta.format` = 10）

按维度分桶，每桶各自从原始值算自己的百分位（百分位不可加——跨桶拿已经算好的 p95 再汇总只能退化成错误近似）：`Overall`（单桶）、`ByModel`（model×protocol）、`ByDate`、`Hours`/`HoursOfDay`、`Endpoints`/`EndpointsAll`、`ByClient`、`Workloads`（工作负载类）、`Sessions`、`Compactions`（见 §2.4）、`Tools`（声明工具集形态）、`Efficiency`（§2.6 自动发现表）、`Sticky`（Sticky Model 有效性）、`Pricing`（可选）。

派生指标（每个 finish 阶段就地写回，原始切片随即释放）：`tokens_in_fresh = tokens_in − tokens_in_cached − tokens_in_cache_write`；`cache_efficiency = cached / (cached + fresh)`；`cache_hit_rate = cached / tokens_in`；`slow_requests`（`dur_ms` 超过 30s 阈值）；`context_growth`（会话内末轮/首轮 `tokens_in` 之比——现在这个比值永远在同一个 Lineage 范围内计算，不会跨越一次隐藏的历史重置，见 §3.2 对这条历史缺陷的说明）。比值类指标的分母低于该桶总请求数 90% 时，Markdown 侧标注 `¹` 低置信度脚注。

### 2.3 成本估算（可选）

`-pricing pricing.yaml` 显式指定一份本地维护的定价 sidecar，不传时自动尝试加载当前目录下的 `./pricing.yaml`（不存在则安静跳过，全程不出现 `$`/成本数字）。按 provider+model（不含协议）配置四个费率字段，支持货币前缀字符串与按 `exchange_rate` 换算；同一 provider+model 可配多条按时间窗口生效的规则。每条记录按其 `endpoint` 拆出 provider/model 查价，命中则把成本累加进 `Overall`/`ByModel`/`EndpointsAll`/`ByClient` 各自的 `cost_estimate` 指针字段（`nil` = 未配置定价，前端据此判断是否渲染）。定价 sidecar 的 `updated_at` 会渲染成免责声明，避免旧报告的 `$` 列被误当成实际账单。

### 2.4 §6.7 Compaction 还原（CCR N-4 的落地）

`buildCompactions`（`aggregate.go`）把 `AnalyzeSessions` 识别出的每次独立 compaction 调用渲染成一行：调用时间、`Summarizes`/`ContinuesTo`（链接到的会话 ID）、`tokens_in → tokens_out`（压缩调用自己的输入/输出 token，不是任一侧会话自己的 token 数）、保留比（`tokens_out/tokens_in`，越低压缩越狠；≥100% 说明这次调用其实没有压缩任何东西，值得怀疑是不是 compaction 检测的误判）、吞掉的实体样例。实体识别复用 `internal/chatmsg.ExtractEntities`（一个粗糙的文件路径/URL 正则扫描，`vmr story` 的 compaction 信息损失摘要——见 §3.6——用的是同一个函数，两边共享一份实现而不是分别维护）：压缩调用输入里出现过、但输出摘要里完全没提到的实体记为"吞掉"，否则记为"存活"。**不修复，只揭示**——不判断丢失是否重要，只把可核查的事实摆出来。

### 2.5 逐请求详单与索引

每条审计记录导出一个 Markdown 文件 + 同名 JSON 文件到 `{out}/details/`（`vmr report` 全部产物 0600/目录 0700，与审计文件同权限——详单承载完整对话正文）。渲染+写盘跑在有界 worker 池上，与 `Build` 的聚合循环共享同一趟文件扫描（`onRecord` 回调），不再单独扫一遍。详单头部展示虚拟模型/端点/结果/耗时/token 明细，正文按请求物理路径分三段（Client→VMR、VMR→上游每次 attempt、VMR→Client），Messages 区默认折叠。

`vmr-requests.md` 是一份纯索引，按 Chat User（`client_key_tag`）分组，真正的 Session→Task→Turn 展开只存在于每个分组自己的文件（`vmr-requests-<tag>.md`）里；单发定时脚手架（heartbeat/dream_diary）归到独立的 `vmr-requests-cron-<class>.md`，不出现在任何 Chat User 分组下。

### 2.6 Markdown 渲染：九个编号章节

`renderSummary`/`renderCostTokens`/`renderCostEstimate`/`renderReliability`/`renderLatency`/`renderWorkload`/`renderSessions`/`renderStickyEffect`/`renderEndpointValue`/`renderCompactions`/`renderEfficiency`/`renderRequestIndexLink`/`renderAppendix`（`render_doc.go` 的 `Markdown()` 固定运行顺序）：`§0` 摘要 + 自动亮点、`§1` 成本与 Token 经济、`§2` 成本估算、`§3` 可靠性、`§4` 延迟与吞吐、`§5` 负载分布、`§6` 会话与任务（含 `§6.5` Sticky 有效性、`§6.6` 端点性价比、`§6.7` Compaction 还原）、`§7` 效率与浪费、`§8` 请求详单入口，外加一段附录（数据源、百分位方法、`⭐` 含义）。`§7` 的自动发现表（`buildFindings`）扫描已完成聚合的各桶，挑出跨过阈值的浪费项（工具 schema 浪费、缓存未命中、定时任务冗余、输出截断率、慢请求占比、上下文膨胀），每条附一句可执行建议。

**`§6.5` Sticky 有效性的度量方法**：Sticky Model 存在只有一个理由——保持上游 prompt cache 处于热状态，这个章节就是验证它是否真的起作用的证据。**按结果（是否落在上一请求的同一端点）度量，不按机制**：一次触发了 sticky 指针但落到冷端点的请求，仍然算作"切换"；切换的具体原因（TTL 过期、健康冷却、条件路由淘汰、该模型未开 sticky）事后无法互相区分，因此这个章节**不区分切换原因**，只对比"延续上一端点"与"切换了端点"两组各自的缓存命中效率。结果按虚拟模型（sticky 配置的实际粒度）拆开；任一组样本量低于 20 时表格仍渲染但不给结论。

**`§6.6` 端点性价比的度量方法**：不是"这个端点花了多少钱"（`§2` 已经回答），而是"单位产出的代价、以及失败让你多等了多久"：成本/1M 输出 token、成本/成功请求、失败尝试数、失败尝试累计的墙钟时间。**只记时间，不折算成钱**——失败尝试拿不到 usage，多数厂商也不为失败请求计费，强行给一个失败尝试标金额是编造数字，不是估算。

---

## 3. `vmr story`：Agent 任务叙事重建

### 3.0 第一性原理

Agent 每一轮请求都重发累积的完整对话历史。把这个事实推到底：审计记录不是"日志"，是同一份对话状态在时间轴上的完整快照序列。消息是不可变的值——同样的内容就是同一条消息，无论出现在第几次请求的第几个位置，因此应当内容寻址：每条消息取内容哈希，一次请求的历史就退化成一个哈希向量（manifest）。Agent 对自己历史做的每一个动作（追加、重试、压缩、重置、分岔）都表现为相邻 manifest 之间的一次"编辑"。

这正是 git 的模型：blob（消息）+ tree（manifest）+ commit 图（编辑关系）。会话、任务、compaction、上下文生命周期，全部退化成这张图上的查询，而不是各自独立的启发式规则——这是 `vmr story`（以及 §2 `vmr report` 的会话分组）背后唯一的架构原则，也是把两者的公共部分下沉成 `internal/ctxgraph` 独立包的理由。

**实测验证的关键前提**（真实 7112 条记录/809K 条消息实例的语料）：
- **只读末轮消息会丢失 26%–99% 的内容**——"没有压缩时末轮就是完整上下文"这个假设在真实数据上是错的，必须走完整的事件流重建。
- **95.86% 的相邻 manifest 编辑是纯追加**——真正的历史断裂（Contract/Fork）只占约 1%，但高度集中在最长、最值得读的会话里。
- **一次请求内部，`tool_call`/`tool_result` 的因果配对是协议给定的精确事实，不是启发式**——全语料 406534/406534 配对，零孤儿（`internal/chatmsg.CheckToolPairing`，F9 不变量）。这意味着"Agent 做了什么→得到了什么"这条最核心的边是零误差的，不确定性只作用在跨 Step 的 lineage 关系上，大幅收窄了这个系统需要"猜"的范围。

### 3.1 `internal/ctxgraph`：内容寻址层

不含任何 Agent 特化知识——纯粹基于消息哈希与结构比较，不做模板匹配。依赖 `{audit, core, chatmsg}`，被 `internal/report`/`internal/story` 共同依赖，自身不依赖两者（`internal/archtest` 强制）。

- **`Manifest`**（`manifest.go`）：一次请求的内容寻址快照——每条非前导 system 消息的哈希（`Keys []Hash`）、前导 system 块整体哈希（`SysHash`/`HasSys`/`LeadSys`）、`SessKey`（`metadata.user_id` 优先，否则 `"anchor:" + Keys[0]`）、`TS`/`Model`/`Usage` 等请求元数据。`Hash` 是消息规范化 JSON 编码的 md5（`encoding/json` 排序 map key，跨请求同一条消息哈希一致）。
- **`BlobIndex`**（`blobindex.go`）：消息哈希 → 首次出现位置（`(Path, Line, MsgIdx)`），内容本身不驻留内存——索引全量语料只需要几十 MB，原文按需从审计源文件回捞（zstd 不可随机寻址，因此回捞按文件批量顺序扫描，与 `Scan`/`Build` 本身的两遍读取模式一致）。
- **编辑分类**（`edit.go`）：相邻 manifest 之间的转换归为五类之一，判据全部基于最长公共前缀（LCP）与集合覆盖率，O(n) 纯结构比较：
  | 编辑 | 判据 | 语义 |
  | --- | --- | --- |
  | `Append` | LCP = 前一条的全部长度 | 正常推进 |
  | `ReplaceTail` | 公共前缀成立，尾部分岔，长度未剧烈收缩，且前一条尾部未在当前尾部重现 | 临时尾替换（重试、图片裁剪等） |
  | `Splice` | 与 `ReplaceTail` 同形，但前一条尾部至少 2 条消息原样重现在当前尾部末尾 | 原地改写：中段被替换（历史压缩的一种真实形态） |
  | `Contract` | 当前长度 < 前一条长度 × 0.6 | 历史被截断/重建，**分裂 lineage** |
  | `Fork` | 当前内容与前一条的重叠率 < 0.5 | 同一 SessKey 下的另一次对话，**分裂 lineage** |

  阈值（`contractLenRatio=0.6`/`forkCoverage=0.5`/`tailSlack=2`/`spliceMinTailMatch=2`）是代码常量，基于真实语料的编辑类型分布校准，不进 `config.yaml`（用户无法校准自己无法测量的东西）。
- **`Lineage`**（`lineage.go`）：同一 `SessKey` 桶内、由非分裂型编辑（`Append`/`ReplaceTail`/`Splice`）连成的最长链。`splitBucket` 按时间顺序遍历一个桶的 manifest，遇到 `Contract`/`Fork` 就切出新 Lineage，新 Lineage 的 `BrokeFrom` 记录触发切分的那条编辑证据。`RootHash()` 对首个 manifest 的系统哈希 + 全部消息哈希做内容寻址，是 Journey id 的身份来源（不是 `Keys[0]` 单独哈希——一次 Contract 编辑经常保留完全相同的开场白，只哈希首条消息会让两个真正不同的 lineage 被误判成同一个）。
- **缝合**（`stitch.go`）：`StitchGraph` 为每个带 `BrokeFrom` 的 Lineage，用消息哈希倒排索引在全图范围内找最佳匹配前驱（不限于同一个桶——一次真正的历史重写后 `SessKey` 完全可能变化）。三态结果（`StitchOutcome`）：`Stitched`（找到足够证据的前驱，`StitchKind` 为 `StitchCompaction` 或 `StitchHeadPrune` 两者之一，区分依据是覆盖率 `stitchCompactionScore=0.5`/`stitchHeadPruneScore=0.15` 两档阈值）、`NoPredecessorFound`（穷尽搜索零重叠，是一个合法结论而非搜索失败）、`AmbiguousMatch`（只有 `SessKey` 相同 + 时间邻近但零内容重叠的"疑似同源"信号，标记但**不自动缝合**——`stitchSameChatWindow=24h`）。跨桶匹配额外受 `stitchCrossBucketMaxGap=6h` 约束（同桶匹配不受时间窗限制——用户可能几小时后回来追问同一个话题，Agent 只要保留了上下文就仍是同一个任务，这个判断不需要时间阈值）。**宁可断开，不要错连**：这是全系统唯一的可信度来源，置信度不够就显式渲染断裂标记，绝不静默缝合。**幂等可重跑**是与之并列的另一条硬约束——同一输入必须逐字节产出相同结果，包括在多个候选前驱评分相同的时候：`overlap` 用 map 实现，Go 每次运行都会随机化其遍历顺序，因此挑选最佳前驱不能依赖遍历到的第一个候选，而是显式三级排序——覆盖率高者胜出，覆盖率打平则时间间隔更短者胜出，再打平则 Lineage 索引更小者胜出（纯粹为了给出一个确定的全序，无业务含义）。真实语料上这个平局场景并不罕见（多个"疑似同源"候选覆盖率完全相同），漏掉这条兜底会让同一份日志跑两次得到不同的 Journey id——这个问题不是靠单元测试发现的（合成测试数据太小，凑不出真实平局），而是把 `StitchGraph` 对同一语料连跑 5 次、逐 Lineage 比对 `PredIdx` 发现的。`ChainFrom` 沿 `Stitch.Edge.PredIdx` 反向走出一条 Lineage 的完整缝合链（chain），是 `vmr story`/`vmr report` 后续构建 Journey/会话的实际消费单位。

### 3.2 与 `vmr report` 共享：技术债已还清

历史上 `internal/report/session.go` 有自己的一套私有实现——每条记录自算消息哈希向量（`ReqInfo.keys`）、自己的最长公共前缀窗口搜索（`lcp()`）选父、只按 `SessKey` 分桶从不分裂——这与 `ctxgraph` 事实上是同一个概念的两份独立实现，而且报表这一份有一个真实缺陷：只按 `SessKey` 分桶意味着一次 Contract 型历史重置（开场白锚点保留，其余内容坍缩）不会被识别为两个不同的会话，会被静默粘成一个——这既让"下文生命周期"这类问题无法回答，也让 `context_growth`（末轮/首轮 token 比）这个指标在跨越一次隐藏重置时算出脏值（比如短短几轮却报出几百倍"膨胀"）。

现状：`internal/report/session.go` 的会话分组直接消费 `internal/ctxgraph.Lineage`/`Classify`（§2.1），私有的哈希向量与 LCP 搜索已删除。一个 `SessionInfo` 严格对应一个 Lineage，`context_growth` 因此不可能再跨越一次隐藏重置——这不是额外写的分段逻辑，是分组单位本身变了之后的自然结果。`internal/archtest` 的导入边界规则相应更新：`internal/report` 现在合法地、单向地依赖 `internal/ctxgraph`。

`internal/report/session_conformance_test.go` 在这次迁移前后的证据权重发生了变化，值得记在这里：迁移前它交叉验证的是**两套独立实现**（报表私有的 `keys`/`lcp()` vs `ctxgraph.Manifest`/`Classify`）是否得出一致结论，能捕获两者行为分叉；迁移后 `session.go` 直接读 `ctxgraph.Manifest`，同一份测试验证的是"同一份数据被一致地读取"，不再是两个独立算法的交叉核验——仍然有用（能捕获接线错误、字段搬运时的疏漏），但保证强度比迁移前弱。之后再扩展分组逻辑（尤其是 §7 可选扩展里的 Subagent 树）时，这份测试给出的信心不能等同于"两套独立实现互相印证"。

### 3.3 `internal/chatmsg`：消息解析共享层

从 `internal/report` 下沉出来的纯函数集合，被 `ctxgraph`/`story`/`report` 三方共同依赖，是三者共享而不重复实现的最低公共点：`Messages`/`RenderContent`（两种协议的消息列表归一化解析）、`ReassembleSSE`/`FinalMessage`（响应体重组，JSON 或 SSE 两种形态）、`ExtractUsage`（token 用量提取）、`ToolCallList`/`CheckToolPairing`（工具调用列表解析 + F9 不变量断言）、`ExtractEntities`（文件路径/URL 的粗糙正则扫描，`vmr story` 的 compaction 信息损失与 `vmr report` 的 §6.7 章节共享同一份实现）。

### 3.4 `internal/story`：Journey 视图

```
vmr story [-c config.yaml] [-o dir] [-journey <id前缀> | -render-all | -compare-a <id> -compare-b <id>]
          [-include-partial] [-show-ungrouped] [file|glob]...
```

- **无参数**：列出全部候选 Journey（id、任务数、轮数、时间范围、标题预览），`-journey <id前缀>` 渲染其中一个。
- **`-render-all`**：批量渲染全部非断头候选，共享一次性的批量文件读取（不是逐候选各扫一遍源文件）。
- **`-compare-a <id> -compare-b <id>`**：两个 Journey 的行为剖面对比，见 §3.7。
- **`-include-partial`**：默认跳过"断头"候选——头部 manifest 看起来像是从更早的、未加载进本次输入范围的历史续接而来（启发式：非冷启动形态的消息数 + 位于最早输入文件的开头若干行）；显式传入才渲染。断头 Journey 的文件名带 `-partial` 后缀（`journey-<id>-partial.md`/`.json`）——它的 ID 本身依赖"最早可见的 manifest"，加载了更多历史文件后 ID 会变化，后缀是这个不稳定性的自我声明，不需要打开正文找警示语才知道。
- **`-show-ungrouped`**：打印无法归组的记录（既无 `metadata.user_id` 也无非 system 消息可锚定）的源位置，用于排查。

**数据模型**（`journey.go`）：

```
Journey  一条缝合链（Chain []*ctxgraph.Lineage）渲染成的连续叙事
  ├─ Task   一次用户指令引发的一段连续工作
  │   └─ Step   一次请求/响应轮次
  └─ Event   一条消息在整个 Journey 里的首次出现（全局去重、按首次出现排序）
```

`Build`/`BuildChain`/`BuildAll` 从一个 Lineage（或缝合链）构建 Journey：批量取回链上每个 manifest 对应的完整 `audit.Record`（`ctxgraph.FetchRecords`，按源文件分组，一次扫描服务多个候选），逐 manifest 判定任务边界（新 trace id，或 delta 里出现真实新指令）、系统提示词是否变化（`SysChanged`）、是否处于缝合边界（`StitchEdge` 非空——此时 `DeltaStart` 恒为 0，靠全局去重而非计算出的 delta 抑制已展示过的内容，因为 `Classify` 的结构化 LCP 在缝合边界两侧没有意义）。事件流按"新 blob 首次出现"逐 Step 累积，一条消息在整个 Journey 生命周期里只渲染一次。

**Revision 关系**（`Event.Revises`）：一次 `Splice` 编辑的分岔点是一条消息被原地改写，不是巧合的新消息——不标注的话，全局去重会把改写后的版本渲染成一个全新的、无关的 Event，读起来像是"同一件事说了两遍"。

**`internal/story/profile`：唯一的 Agent 特化知识**：`ctxgraph` 全程不做模板匹配（§3.1），但"这条 user 消息是不是真实指令，还是路由信封/纯工具结果/心跳时间戳"以及"这次回复是不是 Agent 框架约定的'本轮故意不回复'"这两件事本质上依赖具体框架的约定，硬塞进 `ctxgraph` 会破坏它的框架无关性。`profile.Profile` 接口（`IsRealUser`/`RealUserText`/`NoReply`）就是这条边界——`journey.go` 的任务边界判定、标题提取、缝合边界处的"是否有新指令"判断全部通过这个接口调用，从不直接硬编码某个框架的约定。目前只有两个实现：`OpenClawAware`（认 OpenClaw 已验证过的具体标记）与 `Generic`（模板无关的兜底）——刻意不做基于检测的 profile 注册表/自动选择：第二个真实 profile（另一款 Agent 框架）出现、且真实语料显示需要专属规则时才值得抽象，现在建一个只有两个成员的注册表是猜测未来需求。

**渲染**（`render_md.go`）：单文件自包含 Markdown，事件默认折叠进 `<details>`，Step 拆成 **Messages**（本轮新进入上下文的内容）与 **LLM Response**（模型自己产出的内容：推理块、回复文本、每个 tool_call 的完整参数）两段。Compaction 边界渲染信息损失摘要（§3.6）。产物 0600/目录 0700，与 `vmr report` 的 `details/` 同等敏感度——正文含完整对话内容。

### 3.5 行为剖面：九项规则派生指标（`metrics.go`）

零 LLM 成本、确定性、跨框架可比——`ComputeMetrics` 纯粹是对已构建 Journey 的一次遍历，不重新拉取任何数据：

| 指标 | 定义 |
| --- | --- |
| 模型时间 / Agent 侧执行时间 / 人类空闲时间 / 净工作时长 | 每个 Step 间隙按"下一个 Step 是否 `HumanInitiated`"分类——人类空闲还是 Agent 自己在忙碌（工具执行、规划），不用魔法阈值，直接读 Step 是否携带真实新指令 |
| 模型/工具时间比 | 模型时间 / Agent 侧执行时间——瓶颈在推理还是在执行 |
| 工具调用分布 | 按工具名的次数与 Args token 占比 |
| 重复动作率 | 相同 `(工具名, 参数)` 对在同一 Journey 里重复出现的比例——是否在原地打转 |
| 错误恢复次数 | 收到 `is_error` 标记的 tool_result 后仍发起工具调用的 Step 数（仅 Anthropic 协议有该标记，OpenAI 协议无对应标准字段，纯 OpenAI 语料下这项指标会偏低估——已知限制，不是 bug） |
| 计划/执行比 | 无工具调用的纯文本 Step 占比 |
| 上下文构成演化曲线 | 每个 Step 自己完整请求体的 token 数按角色（system/user/assistant/tool）拆分，逐 Step 记一个点——上下文预算的构成如何随任务推进变化，是曲线不是单值 |
| 上下文有效利用率 | 非 system Event 里，其提取到的实体（文件路径/URL）后续被更晚的 Event 再次提到的 token 占比——低值意味着大量进入上下文的内容从未被再次引用 |
| Compaction 次数与信息损失 | 见 §3.6，汇总到 Journey 级别 |

`journey-<id>.json`（`Summarize`，与 `.md` 同时写出）落盘这九项指标 + Journey 身份（id/标题/时间范围），是 §3.7 对比模块的直接输入，不需要重新解析 Markdown。

### 3.6 Compaction 三形态与信息损失（CCR N-4 的落地）

真实语料观测到至少三种历史压缩形态，`ctxgraph` 的编辑分类天然覆盖全部三种（不依赖任何模板/标记，模板只是事后补充证据）：

| 形态 | 机制 | manifest 编辑 |
| --- | --- | --- |
| 外挂式 | 独立的摘要请求，之后新历史带明确标记 | `Contract` + `Fork` |
| 原地替换 | 同一 lineage 内，中段被一条改写后的消息取代 | `Splice`（`revision` 关系随之触发，见 §3.4） |
| 静默截断 | 无独立 LLM 调用，消息直接消失 | `Contract` |

每个缝合边界 Step 渲染一段信息损失摘要（`buildCompactionInfo`）：压缩前/后 token 数（取自记录的 `Usage`）、被吞掉的实体（在前驱最后一次请求里出现过、这一步没再提到）与存活的实体（两边都出现）——纯规则、零 LLM 成本，**不修复，只揭示**：不判断丢失是否重要，只把可核查的事实摆出来，供人复核。

### 3.7 双 Journey 对比（`internal/story/compare.go`）

两份已经算好的行为剖面（`JourneySummary`）逐项做差，是这个功能里最省钱、也最直接命中"横向对比不同 Agent 框架"这个原始动机的模块——不需要额外的数据采集，`Compare(a, b JourneySummary) Comparison` 纯粹是对 §3.5 九项指标的再加工。同样是纯规则、零 LLM：每一行差值配一个"相对变化是否越过阈值"的布尔标记（`Notable`，同时要求相对变化 ≥ 30% 且绝对差值超过一个按指标类型定的噪声下限——避免"0 次调用 vs 1 次调用"这种理论上无穷大的相对变化被无意义地标红），不生成任何自由文本解读。工具调用分布额外做一次并集对比（各自调用过的工具、次数）。渲染成 `compare-<idA>-vs-<idB>.md` + 同名 JSON，与单 Journey 的 `.md`+`.json` 同一套惯例；任一方是断头 Journey 时同样受 `-include-partial` 门控，文件名同样带 `-partial` 后缀。

### 3.8 盲区（诚实声明，避免"没记录"被误读成"没发生"）

| 盲区 | 说明 |
| --- | --- |
| 工具执行本身的副作用 | 只能看到耗时和返回文本，看不到真实写了什么文件、跑了什么命令——工具结果是 Agent 自述，不是独立验证 |
| 不经 vmr 的调用 | Agent 若对某些任务直连其他 provider，这段完全不可见；manifest 会出现无法解释的编辑，标为"来源不可见"而不是强行归类 |
| Agent 的内部状态 | 计划、记忆检索、循环检测等不体现在消息列表里的逻辑，只能从行为反推 |
| 跨 SessKey 桶、零字面重合的历史重写 | `ctxgraph.StitchGraph` 的哈希倒排索引在这种情况下没有信号可用（§3.1）——`vmr report` 的独立文本匹配（§2.1）能覆盖一部分，但不是全部；`vmr story` 目前没有对应的兜底信号，这类 Journey 会渲染成一个确认无前驱的断头 |

---

## 4. 关键决策与取舍

| 决策 | 备选 | 取舍逻辑 |
| --- | --- | --- |
| 内容寻址的 manifest + 编辑分类 + lineage 图（`internal/ctxgraph`），`report`/`story` 共同消费 | 各自一套独立的启发式分组 | 会话、任务、compaction、上下文生命周期本质都是对同一份 manifest 序列做差分；两套独立实现迟早分叉，且已经真实分叉过一次（`report` 的私有 `keys`/`lcp()` 长期不知道 Contract 型历史重置，见 §3.2） |
| compaction 检测靠结构性编辑分类（Contract/Fork/Splice），模板只是补充证据 | 靠识别已知框架的固定文本标记 | 真实语料至少有三种压缩形态，其中两种没有任何模板可认；结构性判据天然框架无关 |
| lineage 断裂"宁可断开，不要错连"——低置信度只标注疑似同源，不自动缝合 | 尽量缝合，容忍误连 | 误连（把两个不相关任务缝成一个）比误断（把一个任务切成两段分别展示）代价更高——前者会让读者基于错误的因果关系做判断；断裂在渲染层是显式可见的，误连不是 |
| `Splice` 与 `ReplaceTail` 分开建模，即便真实语料上前者命中数为零 | 只建模 `ReplaceTail`，不单独拆 `Splice` | 判据本身忠实实现了设计公式，这个模式在当前语料的非剧烈收缩型编辑里没有真实样本，不代表未来不会出现；分开建模的运行时成本是几行代码，不是新增复杂度的理由 |
| 一个 `SessionInfo`/一个 Journey 的 Lineage 边界 = manifest 编辑边界，与时间无关 | 按时间窗口切分任务 | Agent 是否保留上下文，就是它自己对"这是不是同一个任务"的表态；用户几小时后回来追问同一件事，只要 Agent 保留了历史就仍是同一个任务，不需要任何时间阈值 |
| 断头 Journey 默认跳过，`-include-partial` 才渲染，文件名带 `-partial` 后缀 | 强行当作全新对话渲染 / 完全拒绝渲染 | 断头意味着真正的开头在本次加载范围之外，ID 因此依赖"最早可见的 manifest"、不是稳定的内容寻址；后缀是这个不稳定性最低成本的自我声明——不改动 ID 本身的计算方式，只在文件名层面提醒读者 |
| 行为剖面（九项规则派生指标）先于 LLM 解读层实现 | LLM 解读层优先 | 横向对比两套 Agent 框架的证据里，九成来自规则可得的表格（工具分布、耗时构成、缓存效率曲线），LLM 在这类分析里承担的是文字组织而非发现；规则层零成本、确定性、可复现，理应先做 |
| 双 Journey 对比（4d）用规则化的相对变化阈值，不生成自由文本解读 | 让 LLM 生成对比叙述 | 4d 和其余八项指标同属"剖面层"（规则派生），LLM 解读层是独立的、可选的第三层，两者不能混——数字必须只由规则产生，混入 LLM 生成的数字会破坏"报告里的每个数字都可复现"这个约束 |
| `vmr report`/`vmr story` 内部对同一批文件各跑一次独立扫描（`AnalyzeSessions` 内的 `ctxgraph.Scan` 通道 + 报表自己的 `collect()` 通道），用 goroutine 并发而非合并成一趟 | 合并成单一遍历，两边共享同一份解析结果 | 合并需要把两套本来独立演进的特征提取（`ctxgraph` 的哈希/lineage vs 报表的工具签名/角色统计等）耦合进同一个循环体，代价是架构复杂度；并发跑两条独立通道用 goroutine 就能把"审计文件读两遍"的墙钟代价从翻倍压到大致不变，是当前语料规模（7112 条记录）下更划算的取舍。若未来语料规模显著增长到 CPU 而非墙钟成为瓶颈，这里是第一个该回头看的地方 |
| 报表的独立 compaction 文本匹配（`linkCompactions`）与 `ctxgraph` 的结构化缝合（`linkStitchedLineages`）并存，不用后者取代前者 | 统一成一套机制 | 两者覆盖不同场景：结构化缝合基于精确哈希匹配，对"零字面重合的历史重写"没有信号；文本匹配能覆盖这个盲区，代价是精度较低。合并会让报表在最需要它的场景（标准的独立摘要调用）里失去唯一还有效的信号 |
| `internal/chatmsg` 承接三方（`ctxgraph`/`story`/`report`）共享的消息解析/实体抽取，不各自维护一份 | 各包各自实现 | 曾经真实发生过：`extractEntities` 一度是 `story` 包的私有函数，`report` 需要同样的能力时面临"复制一份"或"下沉"的选择——下沉到两者都已依赖的 `chatmsg`，换来的是以后只有一处规则要维护，不增加任何一方的依赖面 |

## 5. 实测结论（真实语料，7112 条记录 / 809K 条消息实例 / 752 会话）

- **只读末轮消息丢失 26%–99% 的内容**（6 个真实会话实测）——任何"取末轮/按 session 分组/认模板"的简化方案都会给出错误结论。
- **95.86% 的相邻编辑是纯追加，真正的断裂只占约 1%**，但集中在最长、最值得精读的会话里（例：一个 444 请求的会话内部断裂 10 次）——这是"切分成本低、缝合成本高"这条架构判断的实测依据，也是为什么大部分任务不需要缝合就能得到完整叙事。
- **一次真实压缩案例**（79 条消息、57 对 tool_call/tool_result 被一次改写压成 4 条消息）逐位复现：净工作时长手算 381.0s/18.4%，代码复算 380.983s/18.40%，几乎逐位吻合。
- **一次隐藏断裂案例**：某会话内部 20 轮纯追加（cover 0.60→0.97）之后突然 79 条消息骤降到 4 条（cover=0.25）——同一开场白锚点原样保留，是"anchor 存活型"Contract 编辑的真实样本，也是 §3.2 那条历史缺陷的实证来源。
- **一致性验证**：752 个会话中 718 个与单个 lineage 一一对应，34 个被 `ctxgraph` 正确切成多段（对应上面的隐藏断裂类型）；lineage 缝合在同一语料上 68 个断裂中 62 个成功缝合、6 个正确识别为"疑似同源"未缝合、0 个跨桶时间窗违规。
- **F9 因果配对不变量**：全语料 406534 个 `tool_call`/`tool_result` 配对，零孤儿。

## 6. 已知限制、暂不处理的事项

已识别、但判定"动它的收益低于扰动成本，或数据不足以支撑动它"的项。每项都不是 bug，列在这里是为了下次有人重新发现它时不必从头论证一遍。

| 项 | 现状 | 不动的理由 |
| --- | --- | --- |
| `stitch.go` 的 `stitchCompactionScore`/`stitchHeadPruneScore` 两档阈值 | 初始值，只在 2026-07-14..28 这批语料上验证过产生的分布是否合理 | `edit.go` 的 `contractLenRatio`/`forkCoverage` 已经过真实语料校准，这两个缝合阈值还没有；语料规模变大、或出现风格差异很大的新 Agent 框架时，应该重新跑一遍分布检查，而不是假定当前值继续成立 |
| `Metrics.ErrorRecoveryCount` 在纯 OpenAI 语料上偏低估 | 只识别 Anthropic 协议的 `is_error` 内容块标记，OpenAI 协议没有对应的标准字段 | 强行在 OpenAI 响应体里猜"这是不是一次错误结果"就是 §3 反复强调的"宁可粗糙也不猜语义"的反例；等 OpenAI 一侧出现可靠的结构信号再补，不用启发式凑数 |
| `report` 的独立 compaction 文本匹配（`linkCompactions`）用 200 字节子串比对，理论上存在误配对的可能 | 实测语料上未观测到一例误配对 | 提高比对长度/加校验会增加误报"没匹配上"的风险（真实摘要文本本身会被压缩改写）；现阶段的精度换取的是覆盖率，等真的观测到一例误配对再收紧 |
| `vmr story` 对"跨 SessKey 桶、零字面重合的历史重写"没有兜底信号（§3.8） | `vmr report` 的独立文本匹配能覆盖一部分同类场景，`vmr story` 没有对应机制 | 两个产物的输入相同，但 `vmr story` 目前没有移植 `linkCompactions` 那一类文本匹配信号；这类 Journey 会渲染成一个诚实的断头而不是错误缝合，符合"宁可断开"的原则，不算 bug，只是覆盖率上限还没到 |

## 7. 可选扩展（尚未实现）

以下模块设计上互不依赖，可以任意挑选、任意顺序实现，不阻塞彼此：

- **LLM 解读层**：在规则派生的事实层/剖面层之上，加一层可选的自然语言注解（每个 Step 的意图-动作-结果、Journey 全局总述）。硬约束：三层不得混淆（事实层→剖面层→LLM 解读层），LLM 不得生成任何数字，注解必须携带引用的 Step id 供核实；按 `(step_hash, model, prompt_version)` 落盘缓存，重跑稳定；不配置分析端点时整层消失，报告仍完整可用。跑前 dry-run 打印预估 token 与金额并要求确认，绝不在 `vmr report`/`vmr story` 的默认路径里自动触发。§5 的实测已经说明：多数横向对比的证据规则层就能给出，这层的必要性应该用"规则层交付后的实际阅读体验"来决定，而不是预先假定需要它。
- **HTML 单文件渲染 + 脱敏**：单文件自包含（内联 CSS/JS，零外部依赖），左侧时间轴 + 右侧 Step 卡片瀑布。脱敏模式（保留结构与指标，正文替换为长度占位符 + 类型标签）是分享给团队外部这个使用场景的前置条件——Markdown 产物含完整对话正文、文件路径、内部项目名，没有脱敏能力之前不应该假定这个产物适合对外分享。
- **Subagent 树**：Event 模型已预留 `parent_step_id` 挂载点，渲染层预留分支，但识别信号（system blob 与主 Journey 同源、时间窗完整嵌套在某次工具调用/结果之间、其输出 blob 出现在主 Journey 后续的 tool_result 里）尚未验证过命中率。开工前应先跑一次采样脚本验证这三条信号在现有语料上是否成立，验证不过继续推迟——不预先假定这套判据有效。

---
