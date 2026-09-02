<!-- Ver 2026-08-22 16:00, by Sonnet 5 -->

# Virtual Model Router (vmr) — 设计方案 · Part 2：报表与叙事（Analytics）

本文档描述 vmr 审计日志的两个离线消费方：聚合报表 `vmr report`（`internal/report`）与 Agent 任务叙事重建 `vmr story`（`internal/story` + 支撑层 `internal/ctxgraph`/`internal/taskseg`/`internal/chatmsg`）。读完即可维护与二次开发这一侧。使用文档见 `README.md`/`README.zh.md`、`docs/UserGuide.md`/`UserGuide.zh.md`。

**这是 v4 版设计文档的 Part 2**：路由核心（虚拟模型、协议透传、Adapter、调度与健康、审计日志格式本身等）见姊妹文档 `docs/VirtualModelRouter_Design_v4_Core.md`（Part 1）。（v4 另有两篇专题：额度感知路由 `docs/VirtualModelRouter_Design_v4_Quota.md`，战略与竞品 `docs/VirtualModelRouter_Design_v4_Strategy.md`。前者与本文档有一处实质接口：`vmr report` 的 §2.5 额度对照表是它那份 `vmr-quota.json` 的离线读者，读取契约与口径一致性纪律以那篇为准。）两份主文档只通过审计日志的 JSONL 格式耦合——本文档描述的一切都是**离线、只读**地消费 Part 1"记录结构"一节定义的 `audit.Record`，不影响、也不参与任何实时路由决策；`internal/report`/`internal/story`/`internal/ctxgraph`/`internal/taskseg`/`internal/chatmsg` 均不出现在 `internal/router`/`internal/server` 的依赖图里，这条边界由 `internal/archtest` 的可执行检查强制。

---

## 0. 定位

vmr 的审计日志（Part 1 §9）记录的不是"日志"，是同一份对话状态在时间轴上的完整快照序列——Agent 每一轮都会重发累积的完整历史。这个事实决定了两个产物的关系：

- **`vmr report`**：横向聚合。跨全部请求统计成本、延迟、错误率、缓存效率、会话/任务分布——回答"这段时间整体花了多少、哪里在浪费"。
- **`vmr story`**：纵向还原。把一条 Agent 任务的完整执行过程重建成可读的叙事流——回答"这一个任务具体是怎么做的、哪一步开始跑偏"。

两者服务四类具体场景，落在事实层（原始记录）/剖面层（规则派生指标）/解读层（可选的 LLM 注解，见 §3.5c/§3.7）三层里不同的层：单 Agent 调优（"这次改了 prompt，工具调用是变多还是变少了"——剖面层，`vmr story` 的九项指标 + 模型使用/切换 + §3.7 对比）、跨框架/跨模型横向比较（"Claude Code 和 OpenClaw 跑同一类任务谁更省"——同样是剖面层，跨 Journey 对比）、事故复盘（"这次为什么突然开始乱猜文件路径"——事实层，`vmr story` 的完整叙事 + compaction 信息损失摘要）、上下文工程（"这个 Agent 的上下文预算都花在哪了"——`vmr story` 的上下文构成演化曲线 + `vmr report` 的 §1 token 经济）。四类场景没有一类**必须**依赖 LLM——这也是为什么剖面层先于解读层实现，解读层至今仍是可选、可整体降级的第三层，不生产任何规则层给不出的数字。

两者也是同一份数据的**两个连续变焦倍率**，不是两个割裂的产品：`vmr report` 是宏观（全量聚合，回答"钱花在哪、哪条链路在失败"），`vmr story` 是中观（单任务叙事，回答"这一步为什么跑偏"），逐请求详单（§2.5）是两者共同的微观下钻终点（回答"这一次上游到底收发了什么字节"）——读者的提问天然会跨倍率移动（宏观层看到某个 client 突增就想问是哪个任务，中观层看到某一步失败就想知道上游具体返回了什么），`vmr analyze` 的 `-journey`/`-compare`/`-corpus` 变焦选择器与索引→详单的导航链接正是为了让这种移动零成本，而不是逼读者每换一次倍率就重新定位。

两者共享同一个底层事实来源——`internal/ctxgraph` 把审计记录建模成内容寻址的 manifest（消息哈希向量）序列 + 编辑分类 + lineage 图（见 §3），`vmr report` 的会话分组、`vmr story` 的任务叙事都是这张图上的查询，不是两套独立算法。这不是巧合：Agent 场景里"一个会话有几个任务""这次压缩丢了什么""这一步是不是新指令"这些问题，本质都是对同一份 manifest 序列做差分，用两套启发式各自回答一遍只会导致两者迟早不一致。

**为什么是离线而不是实时**：审计日志本身已经是内容寻址存储（全量原始 body 已在盘上、不可变、按时间有序），"归档"是冗余的——只需要一个索引，不需要数据库、版本链、保留策略。`internal/ctxgraph.Scan` 就是这个索引的构建过程；索引本身（每条 manifest 的消息哈希向量 + 其源坐标 `Path`/`Line`/`Req`，正文不驻留内存、按需回捞）对全量语料（7112 条记录、809K 条消息实例的实测规模）只有几十 MB，构建耗时以秒计，完全不需要常驻状态或后台任务。

---

## 1. 审计日志：唯一输入契约

两个产物都从 Part 1 §9.2 定义的 `audit.Record` JSONL 起步，不做任何假设之外的解析。这里只重述对本文档后续内容必要的字段（完整格式定义、六条约定见 Part 1 §9.2）：

- `client.request.body` / `client.response.body`：客户端 ↔ vmr 的原始请求/响应体（`vmr story` 的 manifest、`vmr report` 的会话分组都从这里取消息列表）。
- `attempts[]`：vmr ↔ 上游每次 failover 尝试，含 `endpoint`/`protocol`/`provider`/`model`/`error_class`/`norm` 等结构化字段。
- `ts`/`dur_ms`/`ttft_ms`：请求到达时刻、总耗时、首字延迟。
- `client_key_tag`：调用方标签（`vmr report` 按它分组导出，`vmr story` 的 Journey id 用它做客户端前缀）。

两个产物都支持混合读取明文 `.jsonl` 与历史压缩产生的 `.jsonl.zst`（`audit.OpenLogFile` 透明处理），且都能在未显式指定输入文件时回退到 `<config.yaml 的 log_dir>/vmr-audit-*`（`cmd/vmr/auditpaths.go` 的 `resolveInputPaths`，`vmr report`/`vmr story` 共用）。

**时区**：`ts` 序列化成 RFC3339（Go `time.Time` 默认 `MarshalJSON`），带写入那一刻进程自身的本地偏移量（即 `time.Now()`），不转 GMT——一份 `audit.Record` JSONL 打开就是当地墙钟时间，无需心算时区，偏移量本身又消除了任何解析歧义，两头都占。审计文件按天轮转（`vmr-audit-YYYY-MM-DD.jsonl`）用的是同一个本地时钟，轮转边界即当地日历日的午夜。

`vmr report`/`vmr story` 的展示层与聚合层反过来：任何把一个已持久化的 `time.Time` 格式化给人看、用它算聚合分桶 key（`vmr-report.md` 的按日期/按小时统计、`vmr-requests.md`/`vmr story` 的每一处时间戳），或者嵌进文件名（per-request `details/*.md`、报告/日志输出路径）的地方，一律先 `.In(fmtutil.DisplayZone)`（= `time.Local`，即运行 `vmr report`/`vmr story` 这台机器的系统默认时区）再格式化——不信任记录自带的原始偏移量，也不硬编码某个固定时区。这样同一批数据不管在哪台机器上生成报告，`vmr-requests.md`/`vmr-report.md`/`vmr story` 三者对同一条记录显示的时间永远彼此一致，且等于"当下看报告"这台机器的本地时间，而不是"当初写日志"那台机器的时区。

`internal/story/journey.go` 的 `deriveID` 是唯一不走这条规则的地方：它直接用 manifest 自带的 `time.Time`（未经 `.In()`/`.UTC()` 转换，即写入那一刻服务器自身的本地偏移量）格式化。这个 id 同时是 `journey-<id>.md`/`compare-*.md` 的文件名，要同时满足①同一份审计数据不管在哪台机器上跑都算出同一个 id 字符串（`-journey`/`-compare` 精确匹配）②文件名里的时间是人一眼能看懂的本地墙钟时间。因为这个偏移量是数据本身的属性、不随读取者变化，两个诉求同时满足——走 `DisplayZone` 会引入①想避免的不稳定性，强制 `UTC` 会导致②的可读性问题。

---

## 2. `vmr report`：聚合报表

```
vmr report [-c config.yaml] [-o dir] [-details] <file|glob>...
    # 输出 vmr-report.json + vmr-report.md + vmr-requests.json + vmr-requests.md（+ 按 client_key_tag 的 sibling）
    # + {out}/details/ 逐请求详单（-details 才渲染，默认关闭——见 §2.5；只要定价数据解析出结果就渲染 §2 成本估算——
    # 内置标准表始终生效，-c 指定的 config.yaml 若能读到会在其上叠加账号覆盖，见 §2.1）
```

全部实现在 `internal/report` 一个包里：`aggregate.go`（`buildInternal` 及其 `aggState`/`scanFiles`/`finishBuckets`/`sortBuckets` 三段式——单遍扫描输入文件、收尾、排序，`Build`/`BuildCached` 两个对外入口本身在 `build_cached.go`）、`ingest.go`（各 Row 类型的累加半区：`TrafficStats.Ingest`/`Finish` 是 `Row`/`HourRow`/`ClientRow`/`WorkloadRow`/`SessionRow` 共享的 token/时延/量级核心，`EndpointRow` 因 attempt/request 两种粒度不共用它，单独留着 `IngestAttempt`/`IngestRequest`）、`recextract.go`（`buildRec2` 等单条记录抽取）、`session.go`（会话/任务分组）、`rows.go`（`Report2`、`TrafficStats` 及各 Row 类型 = `vmr-report.json` 的公开 schema）、`metrics.go`（派生指标 + `buildFindings`）、`render_doc.go`（章节运行顺序 + 共享的 `mdTable`）+ 一个章节一个文件的 `section_*.go`（**新增章节 = 新增文件，不是把某个文件改大**，`internal/archtest` 的行数预算逼着遵守这条）、`detail.go`/`render.go`（逐请求详单）、`requests.go`（`vmr-requests.md` 索引）、`cost.go`（`costFor` 成本公式 + 端点标签拆分）、`pricing.go`（定价摘要类型——实际的三层解析引擎在 `internal/pricing`，见其包注释的"两个消费者"说明）。与审计记录格式强耦合：改动 `audit.Record` 结构必须同步改这个包及其测试。

**输出语言**：`vmr-report.md`/`vmr-requests*.md`/`details/*.md` 的文案（英文/中文，默认英文）来自渲染函数新增的 `lang i18n.Lang` 参数；`vmr-report.json` 的叙述性字段（`Finding.Finding` 等）跟随同一个 `lang`——`Build` 本身仍不接收 `lang` 参数，`cmd_report.go` 在写 JSON 之前调用 `report.LocalizeEfficiency(rep, lang)` 按实际语言覆写。完整机制（`internal/i18n` 架构、`report.yaml`/`-lang`、JSON/Markdown 语言契约）见 §4。

### 2.1 两遍读取：`AnalyzeSessions` + `Build`

`Build`（`aggregate.go`）对同一批输入文件读两遍：

1. **`AnalyzeSessions`（`session.go`）**：把每条记录关联到 `internal/ctxgraph` 的 manifest（见 §3），按 `ctxgraph.Lineage` 分组出会话（`SessionInfo`）与任务（`TaskInfo`），同时提取会话分组之外的报表特征——工具签名、角色字符/token 统计、compaction 标记、chat_id、NoReply 检测等（这些是报表领域的关切，`ctxgraph` 不需要知道）。`AnalyzeSessions` 内部并发跑两条独立的文件扫描通道：一条是它自己对每条记录的 `collect()`（提取上述报表特征），另一条是 `ctxgraph.Scan`+`ctxgraph.StitchGraph`（构建分组用的 Lineage/Stitch 图）——两者读同一批文件、互不依赖，用 goroutine 并发而非顺序执行，避免"审计文件读两遍"变成两倍墙钟时间。
2. **`Build` 自身的第二遍扫描**：流式重新扫一遍同一批文件，按 `path:line` 用 `AnalyzeSessions` 的结果把每条记录接到分组坐标、usage、工具调用等，同时重算尚未提取的原始量（工具声明字节数、serving endpoint、错误类）。这一遍同时把每条记录的 detail 渲染也带上（见 §2.5），所以整个 `vmr report` 只需要对源文件读两遍，不是三遍。

`AnalyzeSessions` 失败（唯一的失败面是文件级 I/O：`OpenLogFile` 打开失败 / `ForEachLine` 扫描中途出错——单行 JSON 解析失败只是跳过计数，不会导致整体失败）即整个 `Build` 返回错误，`vmr-report.json`/`.md` 都不写出，不做分文件容错——现实中触发场景几乎只有一种：`vmr start` 常驻进程的 housekeeping 轮转扫描与 `vmr report` 并发读同一份日志时的竞态窗口，属于"重跑就好"的窄场景，不值得为它引入按文件粒度容错的复杂度。

**会话分组算法**（`session.go` 的 `group()`）：一个 `ctxgraph.Lineage` 对应一个 `SessionInfo`——Lineage 已经在结构上把 Contract/Fork 类型的历史重置切成了独立片段（见 §3.2），`group()` 不再自己判断"这是不是同一个会话"，只是消费这个既有分类。`SessionInfo.ID`（P6.1）直接就是这条 Lineage 的内容寻址身份（`Lineage.LineageID()`，`"l-" + RootHash 前 8 位十六进制`，与 `story` 的 Journey id 同一套口径）——run-scoped 的位置序号 `s01`/`s02`……降级为 `SessionInfo.DisplayAlias`，只供人读表格里快速对照，不再承担身份职责；`story` 的 `JourneyIndexRow.Lineages` 携带同一批 id，两侧的会话行/任务因此可以直接按集合成员关系判定归属，不需要各自算一个哈希再对表。每条记录的"这一轮相对上一轮改了什么"（`DeltaStart`/`ReplacedTail`/`SysChanged`）来自 `ctxgraph.Classify(前一条记录的 manifest, 这一条的 manifest)`，不再有报表包自己的哈希向量/LCP 实现——历史上这是两套并行实现（`ReqInfo.keys` + 私有 `lcp()` vs `ctxgraph.Manifest.Keys` + `Classify`），现在统一成一套。任务边界判定（是否开新任务）曾经是报表领域自己的规则、`story` 另有一份独立实现，架构审查 B3 批把两者收敛进了下文 `internal/taskseg` 一节描述的共享算法：`taskseg.IsNewTask`——新 trace id、或 delta 里出现一条不在父级历史里出现过的真实用户指令，就开一个新任务；父级回复是 NoReply（空回复或 OpenClaw 的 `NO_REPLY` 标记）时不开新任务，视为对同一指令的重试。`report`/`story` 都只调用这一份实现，不再各自维护。

**跨会话链接，两条并列信号**：
- `linkStitchedLineages`：任何 Lineage 从其所在的 SessKey 桶断裂（Contract/Fork）又被 `ctxgraph.StitchGraph` 缝合回某个更早 Lineage 时，直接把 `SessionInfo.ContinuedFrom` 设成前驱会话的 ID——这是纯结构信号，覆盖同一 SessKey 桶内的断裂重连（典型：一次原地改写式的历史压缩，开场白锚点原样保留）。
- `linkCompactions`：报表自己识别的、独立发出的 compaction 摘要调用（system prompt 含"summarization"，或"无工具声明 + `max_completion_tokens` + 无 trace"的形状），用 200 字节文本子串匹配把它与压缩前/压缩后两个会话关联（`c.Summarizes`/`c.ContinuesTo`），并在 `ContinuedFrom` 仍为空时补上跨会话链接。

两条信号**故意不是同一套机制的两种实现，而是覆盖不同场景的互补信号**：`ctxgraph` 的缝合基于精确消息哈希匹配，一次真正的历史重写（摘要调用的输入是渲染过的对话文本，不是逐字消息）往往与前后会话没有任何逐字消息重合，哈希倒排索引在这种情况下没有信号可用；文本子串匹配能覆盖这个盲区，代价是精度较低（200 字节的子串巧合命中理论上可能，实践中未观测到）。

### 2.2 数据形状：`Report2`（= `vmr-report.json` 的 schema，`meta.format` = 10）

按维度分桶，每桶各自从原始值算自己的百分位（百分位不可加——跨桶拿已经算好的 p95 再汇总只能退化成错误近似）：`Overall`（单桶）、`ByModel`（model×protocol）、`ByDate`、`Hours`/`HoursOfDay`、`Endpoints`/`EndpointsAll`、`ByClient`、`Workloads`（工作负载类）、`Sessions`、`Compactions`（见 §2.4）、`Tools`（声明工具集形态）、`Efficiency`（§2.6 自动发现表）、`Sticky`（Sticky Model 有效性）、`Providers`（§2.5 账户消耗与额度参照，从 `EndpointsAll` 事后上卷，见其下方一段）、`ClientEndpoints`（§5.5 按客户端的上游归属，`(client_key_tag, endpoint)` 二元组的 token 消耗，流式累计——见其下方一段）、`Pricing`（可选）。

派生指标（每个 finish 阶段就地写回，原始切片随即释放）：`tokens_in_fresh = tokens_in − tokens_in_cached − tokens_in_cache_write`；`cache_efficiency = cached / (cached + fresh)`；`cache_hit_rate = cached / tokens_in`；`slow_requests`（`dur_ms` 超过 30s 阈值）；`context_growth`（会话内末轮/首轮 `tokens_in` 之比——现在这个比值永远在同一个 Lineage 范围内计算，不会跨越一次隐藏的历史重置，见 §3.2 对这条历史缺陷的说明）。比值类指标的分母低于该桶总请求数 90% 时，Markdown 侧标注 `¹` 低置信度脚注。

### 2.3 成本估算（可选，P2.2 起接入 `internal/pricing`）

定价来自 `internal/pricing` 的两层模型（随二进制内置的标准价目表 + `-c` 指定的 config.yaml 里 `providers[].pricing`/全局 `pricing:` 的账号覆盖，与 `metric: cost` 额度共用同一套解析——见 `docs/VirtualModelRouter_Design_v4_Quota.md`"简化：砍什么、留什么"一节里"定价分三层"的论证、`internal/pricing` 的包注释）；找不到 config.yaml 时优雅降级为只用标准表列表价。`cmd/vmr/cmd_report.go` 的 `buildPricing` 是唯一的组合根：读 config、构造 `pricing.Resolver`（按 provider 记忆化，`RateFor(provider, model, ts)` 免去每条记录重新走四步 canonical key 解析）连同一份供渲染用的 `report.Pricing` 摘要（币种、标准表生成日期、supplement 路径、override 条数），一并传给 `Build`/`BuildCached`——`internal/report` 自身从不 import `internal/config`（`report ↛ config` 边界不变）。每条记录按其 `endpoint` 拆出 provider/model 查价（`internal/report/cost.go` 的 `splitEndpointProviderModel`），命中则把成本累加进 `Overall`/`ByModel`/`EndpointsAll`/`ByClient` 各自的 `cost_estimate` 指针字段（`nil` = 未解析出定价，前端据此判断是否渲染）——`costFor`（同文件）四个分量全部计入，包括 `cache_read`（P2.2 订正：P1 时代的样例 `pricing.yaml` 四个 provider 恰好都把缓存读定价为 0，旧公式因此省略了它，是巧合不是通例，见该函数注释）。`report.Pricing` 的 `Disclaimer()` 渲染成免责声明，避免旧报告的 `$` 列被误当成实际账单。

**§2 的口径是"按量计费等价成本"**（pay-as-you-go equivalent，见 Quota 设计文档"定价分三层"一节的 ⑧）：这些流量若按定价体系里可解析到的公开价（渠道自定价或第一方列表价）逐 Token 计费要花多少。不是实付金额——包月账号边际成本是 0，经转售商/代理的实际单价只有用户知道；要实付价就配 `providers[].pricing.overrides` 或 `pricing.supplement`。标题、免责声明、`§0` 摘要列都按这个口径措辞。

**四张表都带合计行，并把"合计里没有什么"说出来**（`costTotalOf`/`renderCostBy*`）。一个静默省略未定价行的合计，和 `§2.5` 的 `WindowUnpricedPct` 要防的是同一件事：一个精确的、系统性偏低的、读起来完全正常的数字。三类旁注：**未定价**（N/M 行的端点解析不出费率，成本未知而非 0）｜**降级估算占比**（上游没返回 usage、按字节数推算 Token 后计价的那部分金额，`EndpointRow.CostEstimateEst` 汇总）｜**费率缺分量**（`EndpointRow.CostRateIncomplete`：`pricing.Rate.Cost` 把 nil 分量按 0 计价——一个防御性下限，不是文档化的降级路径——所以这些端点的金额是系统性偏低的下界；厂商不公布 `cache_read` 而账号缓存命中率 90% 时，九成输入 Token 是没计价的）。**未定价的剔除规则（从未成功送达的行不计入未定价分母——那不是定价缺口）仅适用于 endpoint 表**：该表有 attempt 粒度的 `Forwarded` 信号，一个从未送达的端点单独成行会直接制造"为什么没有它的价格"的错觉；model 和 client 表聚合层级不同、没有 attempt 粒度的送达信号，如实列出所有有流量的行（即使其请求全失败）保留了完整性，两个口径的差异是刻意的。`§0` 摘要的成本列在完全没解析出定价时显示"未定价"而**不是 0**——`0` 读起来是"这些流量免费"，正是 `internal/pricing` 整个包在防的 unknown/zero 混淆。展示币种可以和账号实际计费的币种不同——`-currency`（或 `report.yaml` 的 `currency`/`exchange_rate`）在 `buildPricing` 里对 `pricing.Resolver` 套一层 `WithDisplayFactor`，只重新标定最终显示的数字，从不改动实际解析/计费逻辑；解析不出对应汇率时降级为显示原始计费币种并打印一行警告，不会中断报告生成，同样是"定价问题只丢 `$` 列精度，绝不丢整份报告"的既定哲学。**成本基数在两个半区之间是同一个**：`internal/report` 与 `internal/story` 都通过 `pricing.Rate.Cost` 计价（同一个**公式**），但公式的**基数**——哪些记录进入这个和——曾经各选各的：report 把上游没返回 usage 的记录按字节数估算计价并计入总额（`§2` 脚注一直写着），story 直接跳过该 step，于是同一批记录两个产物给出不同金额，而两边都不说自己漏了什么。现已统一：降级估算下沉为一份实现（`chatmsg.BodyRaw`/`EstimateRequestBodyTokens`/`EstimateResponseBodyTokens`，放在 `chatmsg` 而不是 `reqdetail`，因为 `reqdetail` import `ctxgraph`、方向上到不了 manifest 构建），`ctxgraph.Manifest` 带上 `EstIn`/`EstOut`（parse cache v3→v4），`story.ComputeJourneyCost` 按同一基数计价并用 `EstimatedSteps` 披露降级占比；端点归属也一并对齐（`outcome == "error"` 的记录两边都不计——report 侧本就要求"某次 attempt 拿到过 2xx"）。这条等价关系由 `cmd/vmr/cost_basis_parity_test.go` 差分测试钉住，理由与 `quota_parity_test.go` 相同，也同样只能放在能同时看见两个产物的组合根。

**已知缺口**：溯源目前只做到聚合级——`report.Pricing` 摘要只给"本次用了哪些定价来源"的总数，单行 `$` 数字看不出它具体走的是标准表/补充表/账号覆盖哪一层；真要做需要在 `pricing.Resolve` 的返回值里带上来源标记并一路穿到 `report` 的行结构里，不是小改动，暂缓到额度看板（`section_quota.go`）那批一并考虑，详见 `docs/VirtualModelRouter_Design_v4_Quota.md`"现状与后续计划"一节。

### 2.4 §6.7 Compaction 还原（CCR N-4 的落地）

`buildCompactions`（`aggregate.go`）把 `AnalyzeSessions` 识别出的每次独立 compaction 调用渲染成一行：调用时间、`Summarizes`/`ContinuesTo`（链接到的会话 ID）、`tokens_in → tokens_out`（压缩调用自己的输入/输出 token，不是任一侧会话自己的 token 数）、保留比（`tokens_out/tokens_in`，越低压缩越狠；≥100% 说明这次调用其实没有压缩任何东西，值得怀疑是不是 compaction 检测的误判）、吞掉的实体样例。实体识别复用 `internal/chatmsg.ExtractEntities`（一个粗糙的文件路径/URL 正则扫描，`vmr story` 的 compaction 信息损失摘要——见 §3.6——用的是同一个函数，两边共享一份实现而不是分别维护）：压缩调用输入里出现过、但输出摘要里完全没提到的实体记为"吞掉"，否则记为"存活"。**不修复，只揭示**——不判断丢失是否重要，只把可核查的事实摆出来。

### 2.5 逐请求详单与索引

每条审计记录可以渲染成一个 Markdown 文件到 `{out}/details/`（`vmr report` 全部产物 0600/目录 0700，与审计文件同权限——详单承载完整对话正文）；曾经同名的 `.json` 副本（`json.MarshalIndent(&rec, ...)` 的逐字复制）已经删除——原始记录本来就能通过坐标从源审计日志直接取回（`internal/audit.LineAt`，CLI 入口是 `vmr replay -req COORD -print`），物化一份同构副本只多花磁盘不多加信息（见 P3 阶段的证据层瘦身）。

**详单渲染机制**：
1. **懒物化**：默认套件下索引表格与决策脊柱基于纯函数生成指向详单的相对链接（`internal/reqdetail.FileName`），**不预先物化全量详单文件**；只在用户单点钻取（`-journey`）、`-details` 或 `-render-all` 时才写盘（默认套件写盘体积因此从数百 MB 降至数 MB）。
2. **模板版本感知重绘**（`renderTemplateVersion`）：详单渲染结构 / 转义规则 / 样式更新时递增版本号，`EnsureRendered` 除了核对指纹与前驱还比对模板版本，自动失效并重绘陈旧文件。
3. **单轮差分**：详单页只展示本轮增量消息（Messages）与模型响应（LLM Response），历史轮次给指向前驱的坐标超链接；系统提示词/工具声明证据按内容哈希（`sysHash`）全局去重（`EnsureSysPromptEvidence`）。
4. **转义**：详单渲染与决策脊柱对全部原始文本输入点执行 `escapeHTML`/`escapeCell`，防未闭合 `<!--` 吞噬正文、`|` 撕裂表格（覆盖点由 `reqdetail.EscapeHTML`/`EscapeCell` 与 `render_spine_test.go` / `TestMarkdownEscapesUserDerivedTitles` 等回归测试锁定）。

`vmr-requests.md` 是一份纯索引，按 Chat User（`client_key_tag`）分组，真正的 Session→Task→Turn 展开只存在于每个分组自己的文件（`vmr-requests-<tag>.md`）里；单发定时脚手架（heartbeat/dream_diary）归到独立的 `vmr-requests-cron-<class>.md`，不出现在任何 Chat User 分组下。

`vmr-requests.json` 是 `vmr-requests.md` 背后的数据层（`requests` 字段是 `RequestRow` 列表）。解析缓存不嵌在这个文件里——拆到 `{outDir}/.parse-cache/<filehash>.json` 分片目录（`ctxgraph.LoadCacheDir`/`SaveCacheDir`），与 `vmr-stories.json`（§3.4）共用同一个目录。缓存条目装两半：`ctxgraph.ScanCached` 命中时跳过 `BuildManifest` 的 JSON 解析 + 逐消息哈希；`Build` 自己的第二遍扫描（产出 `ReqInfo`/`RequestRow`）也查同一份缓存的 `facts` 字段（`internal/report/factscache.go`）——两趟都命中时一次重跑不再重新打开、解码该文件（实测 34 文件/177MB 压缩语料，热缓存耗时从 ~72s 降到 ~16s）。`BuildCached`（`cmd_report.go` 用，接受并返回 `*ctxgraph.FileCache`）与 `Build`（无缓存瘦封装，保留给既有调用方）两个入口并存。

### 2.6 Markdown 渲染：九个编号章节

`renderSummary`/`renderCostTokens`/`renderCostEstimate`/`renderProviders`/`renderReliability`/`renderLatency`/`renderWorkload`/`renderClientEndpoint`/`renderSessions`/`renderStickyEffect`/`renderEndpointValue`/`renderCompactions`/`renderEfficiency`/`renderRequestIndexLink`/`renderAppendix`（`render_doc.go` 的 `Markdown()` 固定运行顺序）：`§0` 摘要 + 自动亮点、`§1` 成本与 Token 经济、`§2` 成本估算、`§2.5` 账户（Provider）消耗与额度（`provider.go` 的 `buildProviders` 从已完成的 `EndpointsAll` 事后上卷——不新增流式累计，与 `buildTools`/`buildCompactions` 同构；`ProviderRow.Quota` 只读 `config.yaml` 已解析好的 `LimitConfig.Resolved`，不碰 `internal/router`，但**只写入 `vmr-report.json`，主表 Markdown 不再渲染这一列**——额度参照唯一的渲染出口是其下的子表，见 A.2-2：两处各用一个 formatter 渲染同一个 `Amount` 曾经产生过显示不一致，改为单一出口后不再可能发生），其下的**"额度与消耗对照"子表**（`providerquota.go` 的 `buildProviderQuotaRows`，见前期额度设计方案批 2）只列配了 `quota:` 的账户，把"本报表窗口消耗"（从 `EndpointsAll` 重算，经 `internal/quota` 的 `BaseAmount`/`ApplyModelMultiplier` 加权）与"本周期已用"（`<log_dir>/vmr-quota.json` 的实时计数器，经 `cmd_report.go` 的 `buildProviderQuotas` 按 `PeriodStart` 匹配后写入，不匹配则显示 `-`）并排给出——两者窗口不同，渲染层强制不做减法、各自标注来源。

**重算列的基数按 metric 各不相同，这一点决定了它与路由半区记账的偏差**：`requests` 口径用 `EndpointRow.Forwarded`，而 `Forwarded` 本身读自 `audit.Attempt.Forwarded`（路由半区 `forwardSuccess` 唯一置位点）——不重推 `(Response存在 && Status<400)`。区别在于：softblock（2xx + `ErrorClass=content`，`router.checkSoftBlock`）不经过 `forwardSuccess`、配额零扣，但重推会错计为 Forwarded；截断流（`SetTruncated` 走成 2xx）则仍进配额、仍进 Forwarded。`Forwarded` 以路由半区为权威，分析侧读字段不反推——这是「记录事实、不反推」的明确边界，由 `audit.Attempt.IsForwarded()` 单点预测词守口（兼容旧格式记录 `Response存在 && Status<400 && ErrorClass==""` 的回退）。`Forwarded` 也刻意宽于 `OK`（一个 2xx 但传输中断的 attempt 会被 `SetTruncated` 写上 `Error` 从而掉出 `OK`，但路由半区**已经为它记了账**）。因此这一列在 `requests` 口径下是**恒等复现而非估算**。`tokens` 口径做不到：报表只能累计 usage 解析成功的请求，而路由半区在 usage 嗅探失败时按字节数估算记了账——该账户所有请求都如此时该列渲染 `-` 而非 `0`（与 `cost` 口径"有流量但无定价"同一条纪律：缺数据不伪装成零）。`cmd/vmr/quota_parity_test.go` 是这条等价关系的差分测试——同一批合成记录分别喂给 `router.ChargeResponse` 与完整的 `vmr report` 管线，断言两者相等；**它跨越了 `report`↛`router` 的导入边界，所以只能放在组合根 `cmd/vmr`，不能放进 `internal/report`**。`§3` 可靠性、`§4` 延迟与吞吐、`§5` 负载分布、`§5.5` 按客户端的上游归属（`clientendpoint.go` 的 `clientEndpointCollector`——按 `(client_key_tag, endpoint)` 流式累计 token，因为没有任何既有桶按这个 key 分组；渲染上按 client 分组、组内按 token 降序，不是 client×endpoint 矩阵，见前期客户端成本分析 §3.2）、`§6` 会话与任务（含 `§6.5` Sticky 有效性、`§6.6` 端点性价比、`§6.7` Compaction 还原）、`§7` 效率与浪费、`§8` 请求详单入口，外加一段附录（数据源、百分位方法、`⭐` 含义）。

`§7` 的自动发现表（`buildFindings`）扫描已完成聚合的各桶，挑出跨过阈值的浪费项（工具 schema 浪费、缓存未命中、定时任务冗余、输出截断率、慢请求占比、上下文膨胀），每条附一句可执行建议。每条 `Finding` 除展示文案外还带一个不随语言变化的稳定标识 `Code`（`FindingCode`，如 `tool_schema_waste`）——测试与任何程序化消费者都应该只依赖 `Code`，不依赖展示文案本身，见 §4.2。

`§7` 的 `provider_quota_exhaustion`（批 3，`findings_quota.go`）只认这份实时计数器，从不用本报表自己的重算值顶替；缺实时数据不命中（一个估算值不该成为可执行警报的依据）。判据是两个条件同时满足：`Live.Pct >= 90`（绝对量已经吃紧）**且** `Live.Pct > PeriodElapsedPct`（消耗速度快于周期流逝）——后一项**才是**与路由半区 `quota.Headroom < 1` 同源的相对项（`Headroom < 1 ⟺ 已用% > 周期已过%`，两者数学等价，见 `internal/quota/score.go` 的 `Headroom`），前一项是报表自己加的绝对下限，两者合起来才是完整判据，不能只说"与 `Headroom<1` 同源"。这个双条件是刻意设计：只看绝对阈值会让 `every: 5h` 这类短周期账户在每个周期末尾稳定越过 90% 而持续误报——一个会对正确配置持续报警的检测器，只会训练用户忽略整个 `§7`（三梯队否决"Client 单点倾斜 Finding"的同一条理由）。

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
- **源坐标与按需回捞**（`records.go`）：每条 `Manifest` 自带 `Path`/`Line`（以及规范化后的 `Req` 坐标），正文不驻留内存；需要原始 `audit.Record` 时由 `FetchRecords` 按文件批量回捞（zstd 不可随机寻址，每个文件只开一次、顺序扫描），或由其流式孪生 `ForEachRecord` 逐条喂给回调、用完即弃（详情页渲染、LLM anchor 校验等「读一次」场景）。不另维护 `map[Hash]→位置` 的独立哈希索引——`Manifest` 自带的坐标已覆盖同一职责。**回捞结果不得被长寿命结构持有**：`story.Step` 只保存 `buildFrom` 从记录里提取好的事实（token 构成、attempt 的 provider/model、本步 delta 的 tool result、system 块字符数），不保存 `*audit.Record` 本身——否则 O(N²) 的原始字节（每轮重发全历史）会被钉在对象图里，把「按需回捞」变成「回捞后缓存」。`Manifest` 另带 `Bytes`（解压 JSON 行长），供 `cmd/vmr` 按字节预算分批构建时把每批的回捞工作集卡在数百 MB。
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
- **缝合**(`stitch.go`):`StitchGraph` 为每个带 `BrokeFrom` 的 Lineage,用消息哈希倒排索引在全图范围内找最佳匹配前驱(不限于同一个桶--一次真正的历史重写后 `SessKey` 完全可能变化)。三态结果(`StitchOutcome`):`Stitched`(找到足够证据的前驱,`StitchKind` 为 `StitchCompaction` 或 `StitchHeadPrune` 两者之一,区分依据是覆盖率 `stitchCompactionScore=0.5`/`stitchHeadPruneScore=0.15` 两档阈值)、`NoPredecessorFound`(穷尽搜索零重叠,是一个合法结论而非搜索失败)、`AmbiguousMatch`(只有 `SessKey` 相同 + 时间邻近但零内容重叠的"疑似同源"信号,标记但**不自动缝合**--`stitchSameChatWindow=24h`)。跨桶匹配受 `stitchCrossBucketMaxGap=6h` 约束；同桶候选也受 `stitchSameKeyMaxGap=72h` 宽松上界约束——旧规则曾豁免同桶候选（“用户可能几小时后回来追问同一个话题，Agent 只要保留了上下文就仍是同一个任务”），这个理由对人类成立，但堆积在同一锚点 SessKey 下的流量主要是定时/心跳任务：模板化开头互相无关、可跨数百小时，正是当初催生跨桶闸的同一种失败模式，只是发生在跨桶闸永远盖不到的桶内。超窗候选不参与赢家竞争（淘汰优先于排序，与 `strategy` 包 `Condition`/`Dimension` 分离同型——避免「高分超窗者先赢再降级」遮蔽窗内合法前驱），仅当过滤后无任何窗内候选时，最强超窗者作为降级 `AmbiguousMatch` 边保留供人复核——人类真实的“离开几天后续接”保持断裂标记可见可复核，而不是消失在 `NoPredecessorFound` 里。比例阈值之外还有绝对下限 `stitchMinAbsOverlap=3`（共享的**去重**消息数）：压缩重建的首个请求天然很短（system + summary + 首条指令），单条共享消息就能清掉任何比例阈值，而那条消息往往正是 SessKey 错点本身——共享恰恰因为同源，而非因为发生过压缩，作为证据零信息量（与 `edit.go` 的 `spliceMinTailMatch=2` 同一论证族）。`overlap` 的计数是**集合交**：每个去重后哈希至多贡献一次，重复键不得把“共享一条”虚报成“共享多条”。**宁可断开,不要错连**:这是全系统唯一的可信度来源,置信度不够就显式渲染断裂标记,绝不静默缝合。**幂等可重跑**是与之并列的另一条硬约束--同一输入必须逐字节产出相同结果,包括在多个候选前驱评分相同的时候:`overlap` 用 map 实现,Go 每次运行都会随机化其遍历顺序,因此挑选最佳前驱不能依赖遍历到的第一个候选,而是显式三级排序--覆盖率高者胜出,覆盖率打平则时间间隔更短者胜出,再打平则 Lineage 索引更小者胜出(纯粹为了给出一个确定的全序,无业务含义)。真实语料上这个平局场景并不罕见(多个"疑似同源"候选覆盖率完全相同),漏掉这条兜底会让同一份日志跑两次得到不同的 Journey id--这个问题不是靠单元测试发现的(合成测试数据太小,凑不出真实平局),而是把 `StitchGraph` 对同一语料连跑 5 次、逐 Lineage 比对 `PredIdx` 发现的。`ChainFrom` 沿 `Stitch.Edge.PredIdx` 反向走出一条 Lineage 的完整缝合链(chain),是 `vmr story`/`vmr report` 后续构建 Journey/会话的实际消费单位。

### 3.2 与 `vmr report` 共享

`internal/report/session.go` 的会话分组直接消费 `internal/ctxgraph.Lineage`/`Classify`（§2.1），不再有报表包私有的哈希向量 + LCP 搜索。一个 `SessionInfo` 严格对应一个 Lineage——`context_growth`（末轮/首轮 token 比）因此不可能再跨越一次隐藏的 Contract 型历史重置算出脏值（这曾是私有实现只按 `SessKey` 分桶、从不分裂导致的真实缺陷）。`internal/archtest` 允许 `internal/report` 单向依赖 `internal/ctxgraph`。

注意 `session_conformance_test.go` 的证据权重：迁移前它交叉验证两套独立实现是否得出一致结论；迁移后 `session.go` 直接读 `ctxgraph.Manifest`，同一份测试只验证"同一份数据被一致地读取"，保证强度弱于"两套独立实现互相印证"——之后扩展分组逻辑（尤其 §8 的 Subagent 树）时不能把它当成前者。

### 3.3 `internal/chatmsg`：消息解析共享层

从 `internal/report` 下沉出来的纯函数集合，被 `ctxgraph`/`story`/`report` 三方共同依赖，是三者共享而不重复实现的最低公共点：`Messages`/`RenderContent`（三种协议的消息列表归一化解析）、`ReassembleSSE`/`FinalMessage`（响应体重组，JSON 或 SSE 两种形态）、`ExtractUsage`（token 用量提取）、`ToolCallList`/`CheckToolPairing`（工具调用列表解析 + F9 不变量断言）、`ExtractEntities`（文件路径/URL 的粗糙正则扫描，`vmr story` 的 compaction 信息损失与 `vmr report` 的 §6.7 章节共享同一份实现）。

`openai-responses` 的会话结构也由这层归一化，三方不需单独适配：`Messages` 把顶层 `instructions` 当 anthropic `system` 同类、顶层 `input`（数组或裸字符串）当 `messages` 同类，`input` 数组里无 `role` 的 Item（`function_call`/`function_call_output`/`reasoning`）映射到最接近的角色（assistant/tool/assistant）。`RawArray` 返回 `messages`/`input` 中实际存在的那个数组，供 `ctxgraph.BuildManifest` 等按位置回取原始 JSON 编码，替代五处硬编码的 `body["messages"].([]any)`。响应侧 `FinalMessage`/`ReassembleSSE` 通过共享的 `responsesFinalMessage` 解析 Responses 的 `output[]`（非流式）与 `response.completed` 事件里嵌套的 `response.output[]`（流式）。**只信任 `response.completed` 终态事件，不逐个解析 delta**——Responses 的分片事件字段名还没有真实流量验证过，猜错字段名会静默拼错内容，比"暂时没有数据"更差。

### 3.4 `internal/story`：Journey 视图

```
vmr story [-c config.yaml] [-o dir] [-journey <selector> | -render-all | -compare <id1,id2> | -corpus]
          [-include-partial] [-show-ungrouped] [-lang en|zh] [-report-config report.yaml] [file|glob]...

# <selector>：逗号分隔的多个 token，每个 token 是 id/id 前缀，或匹配完整 id 的
# shell 风格通配符（*、?、[...]，path.Match 语义）——解出恰好一个 Journey 时走
# 单 Journey 渲染路径，解出多个时走 -render-all 同一条批处理路径：
vmr story -journey j-a,j-b | -journey 'j-openclaw-*' | -journey 'j-a-*,j-b-*' ...

# -llm-addr：单 Journey（5.9，即 -journey 解出恰好一个匹配时）与对比（含 6c 的
# 分叉点解读）都支持，-render-all/-corpus 以及解出不止一个匹配的 -journey 不支持
# （会对每个 Journey 各打一次 LLM 调用，费用不可控）：
vmr story -journey <id前缀或恰好命中一个的通配符>|-compare id1,id2 [-llm-addr host:port -llm-model name [-llm-key KEY] [-llm-dry-run]] ...

# -corpus：语料级统计（§3.9），不接 -journey/-render-all/-compare：
vmr story -corpus [-o dir] [file|glob]...
```

**输出语言**：与 `vmr report` 同一套机制（见 §4），`Build`/`BuildChain`/`BuildAll` 多一个 `lang i18n.Lang` 参数，`Compare` 同样接收 `lang`（§4.3）——`journey-<id>.json`/`compare-*.json` 里的叙述字段（`Finding`/`MetricDiff.Label` 等）都跟随它。`journey.go` 自己的两个标题占位符（无真实用户指令时的兜底："(tool loop continuation)"/"(untitled)"）同样跟随 `lang`：`Journey.Title`/`Task.Title` 本来就是用户原话的逐字引用、从不保证是英文，`Journey` 另有内容寻址的 `ID` 承担 identity 职责，`Title` 从未被当成 identity 用过，所以这里跟随语言不会重演"展示文案被当成 identity"的问题。

- **无参数**：列出全部候选 Journey（id、任务数、轮数、时间范围、标题预览）。`-journey` 接受逗号分隔的多个 id/id 前缀/`path.Match` 风格通配符，合并去重后渲染全部匹配——只匹配到一个走单 Journey 渲染路径，匹配到多个则复用 `-render-all` 的批处理路径（`renderJourneys`，`cmd_story.go`）。任一 token 一个都没匹配上，整个命令报错（不静默丢弃），避免拼错的 token 被悄悄漏掉。
- **`-render-all`**：批量渲染全部非断头候选，共享一次性的批量文件读取（不是逐候选各扫一遍源文件）。
- **`-compare <id1,id2>`**：两个 Journey 的行为剖面对比（逗号分隔的两个 id 或 id 前缀），见"双 Journey 对比"节；`-llm-addr` 给了才追加可选的 LLM 解读小节（含 6c 的分叉点解读，当 6a/6b 定位到分叉点时）。
- **`-corpus`**：语料级统计（§3.9），跨全部非断头候选，产出 `vmr-story-corpus.md`/`.json`；不接 `-llm-addr`。
- **`-include-partial`**：默认跳过"断头"候选——头部 manifest 看起来像是从更早的、未加载进本次输入范围的历史续接而来（启发式：非冷启动形态的消息数 + 位于最早输入文件的开头若干行）；显式传入才渲染。断头 Journey 的文件名带 `-partial` 后缀（`journey-<id>-partial.md`/`.json`）——它的 ID 本身依赖"最早可见的 manifest"，加载了更多历史文件后 ID 会变化，后缀是这个不稳定性的自我声明，不需要打开正文找警示语才知道。
- **`-show-ungrouped`**：打印无法归组的记录（既无 `metadata.user_id` 也无非 system 消息可锚定）的源位置，用于排查。

**`vmr-stories.json`/`.md`——候选列表落盘，解析缓存另在别处**：无论带不带任何选择性 flag（无参数列表、`-journey`、`-render-all`、`-compare`、`-corpus`），每次运行都会在 `{out}/stories/` 下写一份 `vmr-stories.json`（纯数据，`journeys` 段，每行还带 `lineages`——该 Journey 链上每条 Lineage 的内容寻址 id，供 `report` 侧按集合成员关系 join，见上文会话分组一节；以及 `category`，见下）+ `vmr-stories.md`（纯人读索引表，字段与终端候选列表一致：id、client、时间范围、任务数、轮数、标题、若已渲染则给出 `journey-<id>.md` 的链接）——此前"无参数"模式只打印到终端，跑完就丢，找不到历史候选列表，这一版把它落盘。**候选分类与噪声折叠**（P6.3 + P14 统一判据 `story.IsNoiseCategory`）：按标题里的内容标记把候选分成 `task`/`cron`/`heartbeat`/`subagent` 四类（`[cron:...]` 前缀、`[OpenClaw heartbeat poll]`/`[Subagent Context]` 子串——字面量拼法已用真实语料核实）。经 P14 统一，**仅 `heartbeat`（高频无实质动作的心跳轮询）属于噪声并默认折叠进 `<details>` 块**；`cron`（定时长任务）与 `subagent`（子代理多步任务）均承载实质工作流，与 `task` 一同在主表平权展开并纳入默认套件的预渲染范围；`vmr-stories.json` 照常全量输出，不做取舍。

解析缓存：`{outDir}/.parse-cache/<filehash>.json`（一个输入文件一个分片，与 `vmr-requests.json` §2.5 共用同一目录、同一套 `ctxgraph.FileCache`/`ScanCached` 机制）。分片装 `ctxgraph.BuildManifest` 的输出（消息哈希向量 + 少量标量元数据，**不含消息正文**——正文永远按 `Path`/`Line` 回原始审计文件取）+ schema 版本戳。每次运行对每个输入文件重算内容哈希（`ctxgraph.HashFile`，sha256）：哈希与版本都命中就跳过 `BuildManifest` 的 JSON 解析 + 逐消息哈希（扫描过程里真正贵的部分）。一份文件从 `.jsonl` 压成 `.jsonl.zst` 后字节和路径都变，天然被当成"没缓存过"重解析一次（一次性代价）。

**关键设计取舍:只缓存"文件 → Manifest 解析结果",绝不缩小参与图重建的文件集合**--`ScanCached` 拿到每个文件的 Manifest(缓存的或新解析的)后,永远把**全部**文件的 Manifest 合并成完整集合再整体跑分桶/拆 lineage/`StitchGraph`。这是刻意的正确性边界：`StitchGraph` 的缝合搜索对全部 Lineage 开放（同桶候选受 `stitchSameKeyMaxGap=72h`、跨桶受 `stitchCrossBucketMaxGap=6h` 约束——超窗候选预过滤出局、不再参与赢家竞争，最强超窗者仅作降级诊断兜底，候选本身始终参与图重建），一个全新文件里的记录完全可能续接到一个“看起来早就定型”的旧 Journey 后面、把它的 ID 往后推;只按"这个文件是否被某个已知 Journey 引用过"决定是否纳入图重建,一个全新文件永远查不到引用,续接就被静默漏掉。因为 Manifest 很轻(不含正文),"整体重建图"的开销正比于请求条数而非原始字节数,跳过昂贵的解析步骤之后已经便宜到不需要更激进的增量式图更新。

**数据模型**（`journey.go`）：

```
Journey  一条缝合链（Chain []*ctxgraph.Lineage）渲染成的连续叙事
  ├─ Task   一次用户指令引发的一段连续工作
  │   └─ Step   一次请求/响应轮次
  └─ Event   一条消息在整个 Journey 里的首次出现（全局去重、按首次出现排序）
```

`Build`/`BuildChain`/`BuildAll` 从一个 Lineage（或缝合链）构建 Journey：批量取回链上每个 manifest 对应的完整 `audit.Record`（`ctxgraph.FetchRecords`，按源文件分组，一次扫描服务多个候选），提取每个 Step 需要的事实后即把记录丢弃（Step 不持有它——见「源坐标与按需回捞」）；`BuildAllWithRecords` 是给随后要渲染详情页的调用方用的变体，额外返回那批已解压的记录，省掉二次解压。逐 manifest 判定任务边界（新 trace id，或 delta 里出现真实新指令）、系统提示词是否变化（`SysChanged`）、是否处于缝合边界（`StitchEdge` 非空——此时 `DeltaStart` 恒为 0，靠全局去重而非计算出的 delta 抑制已展示过的内容，因为 `Classify` 的结构化 LCP 在缝合边界两侧没有意义）。**缝合边界与任务边界是两件事**：一次任务中途为回收上下文的压缩（缝合边界但没有新指令）不开新 Task，只在该 Step 内联渲染 compaction 标记；只有缝合边界处 `newInstructionTitleAtStitch` 找到一条不在 `seen` 里的真实新指令，才与普通 delta 一样开新 Task（2026-08 修正——此前每个缝合边界都被无条件当成新 Task，虚增 `len(j.Tasks)` 与 per-Task 检测器分母）。事件流按"新 blob 首次出现"逐 Step 累积，一条消息在整个 Journey 生命周期里只渲染一次。

**Revision 关系**（`Event.Revises`）：一次 `Splice` 编辑的分岔点是一条消息被原地改写，不是巧合的新消息——不标注的话，全局去重会把改写后的版本渲染成一个全新的、无关的 Event，读起来像是"同一件事说了两遍"。

**`internal/taskseg`：Agent 特化知识 + 会话/任务切分算法（`report`/`story` 共用）**：`ctxgraph` 全程不做模板匹配（§3.1），但"这条 user 消息是不是真实指令，还是路由信封/纯工具结果/心跳时间戳""这次回复是不是 Agent 框架约定的'本轮故意不回复'""chat_id 这类会话标识怎么从消息里抠出来"这三件事本质上依赖具体框架的约定，硬塞进 `ctxgraph` 会破坏它的框架无关性。`taskseg.Profile` 接口（`RealUserText`/`NoReply`/`ChatID`）就是这条边界——`story`（`journey.go` 的任务边界判定、标题提取、缝合边界处的"是否有新指令"判断）与 `report`（`session.go` 的会话/任务边界判定、chat_id 提取、NoReply 检测）都通过这个接口调用，从不各自硬编码某个框架的约定。目前只有两个实现：`OpenClawAware`（认 OpenClaw 已验证过的具体标记）与 `Generic`（模板无关的兜底）——刻意不做基于检测的 profile 注册表/自动选择：第二个真实 profile（另一款 Agent 框架）出现、且真实语料显示需要专属规则时才值得抽象，现在建一个只有两个成员的注册表是猜测未来需求。两个命令目前都用同一个默认（`OpenClawAware`），选择逻辑收在 `cmd/vmr` 的一个共用解析函数（`resolveTaskProfile`）里，不是两处各自判断。

`report`/`session.go` 与 `story`/`journey.go` 曾各自维护一份"同一个概念"的切分算法（`responseSummary`/`taskTitle`/`preview`/是否有新指令/挑最新指令这五对同名函数 + 任务边界规则本身）；已收敛进 `taskseg/segment.go`（裁决以 `story` 侧无状态纯函数为权威）：`RealUsers`/`IndexRealUsers`（一次请求只建一次"哪些消息是真实用户指令"的索引）、`HasNewInstruction`（按内容哈希集合判定而非位置，避免历史裁剪把旧消息挤进窗口误判为"新"）、`LastInstruction`/`FirstInstruction`、`IsNewTask`（`traceChanged || (!prevNoReply && hasNewInstr)` 的唯一实现）、`TaskTitle`、`ResponseSummary`、`Preview`。缝合边界特有的 `newInstructionTitleAtStitch` 无 `report` 对应概念，留在 `story` 内。`taskseg` 因此依赖 `internal/ctxgraph` 的 `Hash`/`Manifest` 类型（不成环）。

**Markdown 渲染**（`render_md.go`）：单文件自包含，事件默认折叠进 `<details>`，Step 拆成 **Messages**（本轮新进入上下文的内容）与 **LLM Response**（推理块、回复文本、每个 tool_call 的完整参数）两段。Compaction 边界渲染信息损失摘要（§3.6）。产物 0600/目录 0700——正文含完整对话内容。每个 Step 的"→ detail"指针与 system-prompt 证据指针：单 `-journey` / `-compare` / `-render-all` 路径渲染成指向 `details/*.md` / `evidence/*.md` 的链接；默认批量套件（不物化）渲染成行内 `文件:行` 坐标（`Manifest.Req`），不产生死链接。概览卡在定价可解析时（单 `-journey`/`-compare` 才穿 `*pricing.Resolver`，默认批量套件不穿）多一行成本估算——与 `journey-<id>.json` 的 `cost`、HTML 看板 damage 行同源（`ComputeJourneyCost`），并标注"按标价估算，非实际账单"。

**HTML 单页看板 + 脱敏**（`-journey <id> -html` / `-compare a,b -html`，2026-08）：另写一份单文件自包含 `.html`（内联 CSS + 一小段 `IntersectionObserver` 滚动高亮 JS，零外部请求、theme-aware、0600）——**不是 Markdown 的转写，是一份为分享重新设计的看板**。Journey 看板一屏滚动分四块：判定条（id/时间/结局）· 结构时间轴（Task→Step 骨架，每步一行带模型/工具 chip/failover 徽章/转换标记/Finding 旗标，逐行链回 `details/*.md`）· 指标 grid + 一条内联 SVG sparkline · Findings（规则层 + LLM 层）。Compare 看板三块：两侧头 + 双侧初始指令 · 分岔点大字标出 + 逐指标 A/B 差异表 + endpoint/cache/sysprompt/duration/deliverable/cost 事实（deliverable 两侧都没有则整行跳过；cost 只有一侧解析出定价时加一行"另一侧定价未收录"脚注，空白不等于免费）· LLM 解读段落（`res.Text` 经 `internal/story/mdlite.go` 极简 md→html 渲染）。数据源是 Markdown 渲染器走的同一个 `*story.Journey`/`Metrics`/`[]Finding`，不重解析。`-redact`（需 `-html`）把正文替换为 `‹text: N chars›` 占位、去掉逐步详单链接、Findings 只留代码 + Step 锚、compaction 实体名降级为计数，compare 下另整块去掉 LLM 段；结构/指标/角色/token 数/工具名/时间/compaction 标记均保留。渲染器 `render_html.go` + `render_html_dashboard.go` + `render_html_assets.go` + `render_compare_html.go`，chrome `i18n/story_html.go` + `story_compare_html.go`。仅 `-journey` 单命中 / `-compare` 时出，默认套件不出 HTML。

### 3.5 行为剖面：九项规则派生指标 + 模型使用/切换（`metrics.go`）

零 LLM 成本、确定性、跨框架可比——`ComputeMetrics` 纯粹是对已构建 Journey 的一次遍历，不重新拉取任何数据：

| 指标 | 定义 |
| --- | --- |
| 模型时间 / Agent 侧执行时间 / 人类空闲时间 / 净工作时长 | 每个 Step 间隙按"下一个 Step 是否 `HumanInitiated`"分类——人类空闲还是 Agent 自己在忙碌（工具执行、规划），不用魔法阈值，直接读 Step 是否携带真实新指令 |
| 模型/工具时间比 | 模型时间 / Agent 侧执行时间——瓶颈在推理还是在执行 |
| 工具调用分布 | 按工具名的次数与 Args token 占比 |
| 重复动作率 | 相同 `(工具名, 参数)` 对在同一 Journey 里重复出现的比例——是否在原地打转 |
| 错误恢复次数 | 收到 `is_error` 标记的 tool_result 后仍发起工具调用的 Step 数（仅 anthropic-messages 协议有该标记，openai-completions 协议无对应标准字段，纯 openai-completions 语料下这项指标会偏低估——已知限制，不是 bug） |
| 计划/执行比 | 无工具调用的纯文本 Step 占比 |
| 上下文构成演化曲线 | 每个 Step 自己完整请求体的 token 数按角色（system/user/assistant/tool）拆分，逐 Step 记一个点——上下文预算的构成如何随任务推进变化，是曲线不是单值 |
| 上下文有效利用率 | 非 system Event 里，其提取到的实体（文件路径/URL）后续被更晚的 Event 再次提到的 token 占比——低值意味着大量进入上下文的内容从未被再次引用 |
| Compaction 次数与信息损失 | 见 §3.6，汇总到 Journey 级别 |

`journey-<id>.json`（`Summarize`，与 `.md` 同时写出）落盘这九项指标 + 模型使用/切换 + Journey 身份 + §3.5a 的 Findings，是 §3.7 对比模块的直接输入。另落盘一个 `structure` 字段（`structure.go` 的 `BuildStructure`）——完整的 Task/Step/Event/ToolCall 骨架，每个 Step 带 `req` 坐标、单步性能/成本、与决策脊柱相同的图层级事实（`Edit`/`StitchEdge`/compaction 前后 token 与实体吞噬列表——这三者 `req` 坐标救不回来，必须内联）、该 Step 自己的回复/推理/工具调用参数（有界截断）、工具调用是否配对上结果（`matched`/`result_error`）。**工具结果正文与对话历史正文（`NewEvents`）不内联**——只给内容哈希与角色引用，取用路径是该 Step 的 `req` 坐标；这份机读结构是 P5 删除人读 fact-layer 时验证"信息不丢失"的等价依据（`TestBuildStructure_LosslessReconstruction`）。

**模型使用与切换**（`modelusage.go`）：一个 Journey 用过哪些上游 `(provider, model)` 及各自 Step 数/token + 相邻 Step 间上游变化的切换点。**取值必须来自端点，不能取 `Manifest.Model`**（那是虚拟模型名，一个 Journey 内全程不变，照它实现会得到一张永远"未换过模型"的空表）——从 `Step.Attempts`（`buildFrom` 从记录 attempt 列表里预提取的 `(Provider, Model)`）取，为空（很旧的日志）才回退到切分 `Manifest.Endpoint`。Step 数按"任意一次 attempt 命中即 +1"计（被 failover 掉的端点在表里可见，但不计 token）；切换点带纯观察性标记 `OnFailoverStep`（= `len(Attempts) > 1`），不断言切换原因。这两个字段是列表型数据，**不进** §3.7 的标量 diff/Spearman 机制。

### 3.5a Findings：规则派生的 Step 级"疑似问题"清单（`findings.go`）

`internal/report` 的 `buildFindings`（§6.7 之前那批§7 效率发现）是**聚合级**发现——扫全部请求算出一行统计。`story.ComputeFindings(j *Journey, lang i18n.Lang) []Finding` 是同一套"稳定 `Code` + 展示文案分离 + 一句话建议"模式的**Step 级**版本——每条 `Finding` 定位到 Journey 里具体的一个 `StepSeq`（+ 可选的 `RelatedSeq`），不是一个聚合数字。两者故意不共享类型：`internal/archtest` 的 import 边界禁止 `story` 依赖 `report`（反之亦然），语义粒度也本来就不同。

**措辞纪律**：每条 Finding 都是"候选/嫌疑清单，不是判决"——文案统一是"检测到疑似 X，建议人工复核"，不是"Agent 在这里出错了"。这不是谦虚，是被学术证据逼出来的克制：Who&When（ICML 2025）/TRAIL（Patronus AI）两个独立数据集上，公开最好的自动根因定位方法 step 级准确率也只有 11%–14.2%——`vmr story` 不承诺、也不该承诺比这更高的确定性（详见前期 Journey 深挖分析 §3 的规则化边界表）。

规则层 Phase 1 落地五个检测器，Phase 2（`findings_toolresult.go`）在此之上新增四个——后四个全部建立在 I1（`chatmsg.ToolResultList`，见下）之上，能精确定位"哪个 `tool_call` 的哪个 `tool_result`"，不再依赖 `Event.Msg.Text` 里拍扁的文本猜测。这九条全部零 LLM 成本、纯规则/字符串匹配，每条都对应一个真实事故/issue 或学术失败分类法（MAST，Cemri et al. 2025）里的一个具名类别。表末四行是 Phase 1b 的 LLM 语义检测器，框架另述（见表后）：

| `FindingCode` | 判据 | 依据 |
| --- | --- | --- |
| `exact_repeat_tool_call` | 同一 `(工具名, 参数)` 对（`toolCallKey`，§3.5b 的 `toolCallRepeats` 同一份底层实现）重复出现 ≥ `exactRepeatThreshold`（3）次 | MAST 最高发失败模式（Step repetition，15.7%）；真实案例：`anthropics/claude-code#19699`（同一命令连续报错 7 次以上才被人工打断）、`#15909`（重试 300+ 次、耗时 4.6 小时） |
| `narration_without_action` | 连续 ≥ `narrationMinRun`（3）个无 `tool_call` 的 Step，相邻 `RespText` 词集合 Jaccard 相似度 ≥ `narrationJaccardThreshold`（0.5） | `anthropics/claude-code#27281`（反复说"让我现在组装文档"却不触发工具调用，直到耗尽上下文窗口） |
| `error_then_unverified_success` | 一次 `is_error` 标记后，同 Task 内直到最后一个 Step（携带 `Finish`）都没出现"看起来像验证"的调用（`verificationLikeToolRe`，一个局部启发式，不是正式的读写分类器） | Replit/Google Gemini CLI/Amazon 三起独立报道的真实生产事故，共同点是"面对含糊错误响应自行脑补了乐观结论" |
| `reasoning_action_mismatch` | 推理文本**最后一句**（`lastSentence`，非全文）提取到的实体（`chatmsg.ExtractEntities`）里，有实体在本轮 `tool_call` 参数里找不到子串意义上的匹配（`entityReferenced`，双向 `Contains`，不要求完全相等） | MAST 第二高发模式（Reasoning-action mismatch，13.2%） |
| `plan_execution_misalignment` | Task 首个 Step 的推理/回复里若有编号列表（`lastNumberedList`，只取**最后一段连续编号**，`minPlanItems`=2 到 `maxPlanItems`=8 项），逐项检查后续 Step（含首个 Step 自己的 `tool_call`）是否有实体/关键词重合 | 自我限定为字符串/实体匹配，不做语义理解——识别不出编号格式、列表过长（更像文档而非可执行计划）时直接跳过，不强行分析 |
| `error_retry_unadapted`（Phase 2） | 一次 `tool_call` 收到 `is_error` 结果后，`retryLookaheadSteps`（5）步内第一次同工具调用的参数与出错那次逐字相同 | 5.3 的 Step 级精细化版本——`ErrorRecoveryCount`（九项指标）只问"出错后有没有再调用"，这个问"再调用是不是原地打转" |
| `unused_tool_result`（Phase 2） | 一次 `tool_result` 提取到的实体，**全部**在此后没有任何 Step 再引用（不是"部分未引用"——见下方校准记录） | Step 级定位版的 `ContextUtilization`（九项指标） |
| `unverified_entity_reference`（Phase 2） | 一次 `tool_result` 命中证伪信号（`falsificationRe`：ENOENT/404/not found 等），其实体在此后仍被引用 | 对应 Breunig 命名的 context poisoning 失败模式；措辞明确"仅基于字面证伪标记，不代表确认幻觉" |
| `constraint_text_dropped_at_compaction`（Phase 2） | 缝合边界 Step 的 `Compaction.SwallowedEntities` 非空——直接复用 §3.6 已验证过的信息损失机制，不新增解析路径 | 对应 Governance Decay 论文命名、但论文本身未提出检测器的失败模式；措辞明确"未经验证的假设级检测" |
| `tool_result_misinterpretation`（Phase 1b / LLM） | 模型回复内容与工具真实执行结果存在事实违背（如工具报错但模型宣称成功） | 依赖 `-llm-addr` 解读层语义推断（`llm_findings.go`） |
| `semantic_oscillation`（Phase 1b / LLM） | 模型在多个方案/结论之间来回摇摆反弹，无法收敛 | 对应决策振荡失效模式 |
| `goal_drift`（Phase 1b / LLM） | 当前执行偏离了任务初始目标或约束条件 | 对应目标漂移失效模式 |
| `unverified_completion_claim`（Phase 1b / LLM） | 模型宣称已完成任务但缺乏任何工具执行或文件交付证据 | 对应乐观脑补/虚假交付失效模式 |

**Phase 1b（LLM 语义检测器，`llm_findings.go`，可选）**：`ComputeLLMFindings` 共六个检测器——上表四行加两个复用规则层 `FindingCode` 的（`plan_execution_misalignment` / `constraint_text_dropped_at_compaction`，同码不同触发路径）。仅在 `-llm-addr` 解读层开启时运行；只有 HIGH 置信度、且 `EvidenceAnchor` 在真实 transcript 里逐字命中的才提升为 `Finding`（`SourceLLMInferred`），其余一律丢弃。这六个尚未完成规则层那种黄金样本校准，见 `KNOWN_ISSUES` 的 Phase 1b 语义检测器校准条目。

**I1：`chatmsg.ToolResultList`**（Phase 2 基础设施）：`CheckToolPairing`（F9 因果配对不变量）只证明每个 `tool_call`/`tool_use` 都有匹配的结果，不返回结果内容本身；`ToolResultList(rawMsgs []any) []ToolResult`（`CallID`/`Text`/`IsError`）是它的内容版，复用同一套双协议扫描逻辑。`story.toolResultsFor(steps, i)` 从**下一个** Step 的请求体里查 `steps[i]` 自己发起的 `tool_call` 的应答——协议本身的轮次结构保证这一点成立：模型在自己发起的 `tool_call` 被应答之前拿不到新的一轮。

**校准（真实语料，两轮，提交前跑而非理论设计后直接上线）**：四处假阳性/精度问题已发现并修复，固化为 `findings_test.go`/`findings_toolresult_test.go` 的具名回归测试——`reasoning_action_mismatch` 改为只扫推理**最后一句** + 双向子串匹配（原对整段做实体差集，假阳性约 90%）；`plan_execution_misalignment` 改为扫描含首个 Step 自己的 `tool_call`、且 `lastNumberedList` 只取**最后一段连续编号**（避免两段独立编号列表被拼成一个虚高的"计划"）、编号列表 > `maxPlanItems`（8）项直接跳过（多为长篇文档非可执行计划）；`unused_tool_result` 改为只在**整个结果的所有实体都没被再引用**时触发（原按单实体判定，目录列表类结果一次列几十个文件、Agent 只跟进几个是正常 triage）。

### 3.5b 决策脊柱与呈现层（`render_spine.go`）

用户排查一条 trace 时真正想问的是"哪里开始不对"，不是"Agent 做了什么"——`Journey → Task → Step` 的数据模型天然能构建这个"决策脊柱"，纯粹是渲染层重新分层，不新采集数据：

- **概览卡**（`renderOverviewCard`）：3–5 个关键节点（起始时间、首个错误、首个非 `Append`/缝合转折点、结束时间）+ 基于 `Metrics` 阈值的结构标签 + 定价可解析时一行成本估算（`cost *CostFact` 由调用方穿入，`nil`/未解析则不渲染）。
- **决策脊柱**（`renderDecisionSpine`）：按 Task 分组，一个 Step 一个块（同一 Step 的多个 `tool_call` 聚在一起）。每个块——**Step 标题行**（复用 `stepRoleTag`；命中 §3.5a Finding 加 ⚠️；一条"→ detail"指针）：P5 删除人读层的逐轮 fact-layer 后，决策脊柱是唯一的人读 per-Step 内容层，`Edit`/`StitchEdge`/`SysChanged`/`Compaction`/`NoReply` 五类跨记录事实（详单渲染器物理上无法重建）搬进了这里。**"为什么"一行**（`spineWhyLine`）：`RespText`（几乎总是一句简短的决策性的话，原样展示）优先于 `Reasoning`（标 🤔 前缀、截得更短，完整版在该 Step 的 LLM Response 折叠区）；两者都没有就不渲染。**每个 `tool_call`**（`toolCallLine`，`render_spine_args.go`）：不按 JSON 原文截断前 N 字符（那样只看到 `{"command": "` 信封语法），改为按解出的值形状通用挑选、展示**完整**内容——字段全是短标量就 `key=value` 紧凑列；有一个字段扛负载（"渲染后最长的顶层字段"）就展示那一个字段的完整值，单行 ≤ `spineInlineLen`（120）内联、否则起代码块展示到 `spineFullCap`（3000）为止，撞上限时附一句提示指向详情链接。不针对具体工具名建表，解不出 JSON 对象时退化为原文本身。纯问答 Journey / 无 `tool_call` 的 Task 跳过这一节。
- **Step 角色标注**（`stepRoleTag`）：7 类标签取优先级最高的——🧹压缩 > ⚠️错误 > 🔄重试 > 🔧执行 > 📋规划 > 💬汇报 > 👀观察。
- **工具调用时序图**（`renderToolTimeline`，Journey 末尾）：每个工具名一行、每个 Step 一列的 ASCII 图（`●` 正常/`🔄` 疑似重复/`❌` 含错误标记），用于发现线性阅读容易漏掉的密集重试。
- **Findings 清单**（`renderFindingsSection`）：§3.5a 每条 Finding 的完整文案。

**共享基础设施**：`toolCallRepeats(steps) []ToolCallOccurrence`（`metrics.go`）是"这次调用是不是在重复更早的调用"的唯一判据来源——Step 角色标注的 🔄、决策脊柱标题行、时序图、`exact_repeat_tool_call` Finding、`Metrics.DuplicateActionRate` 全部基于它，避免判据不一致（某 Step 标了 🔄 但 Findings 里找不到对应条目会像产品 bug）。**渲染层与 JSON 输出共享同一份 `ComputeFindings` 计算**，其**选择逻辑**（挑哪些 `Code`/`StepSeq`）必须与 `lang` 无关（只有展示文案随 `lang` 变），`TestComputeFindingsIsDeterministic` 锁定——否则中文 Markdown 和英文 JSON 可能标出不同的 Finding 集合。

### 3.5c 单 Journey LLM 解读层（`llm_single.go`）

解出恰好一个匹配的 `-journey` 也能接 `-llm-addr`（`-render-all`/`-corpus`/多命中 `-journey` 不支持——会对每个 Journey 各打一次 LLM 调用，费用不可控），复用 §3.7 描述的同一套 `Interpret`/`cacheKey`/`buildUserPrompt` 链路。`SingleJourneyEvidencePack` = Journey 的 `Metrics` + `[]Finding` + 逐轮工具索引；system prompt 的任务是对已有 Finding 做优先级排序/串联解读，禁止"自己发现清单之外的新问题"（除非明确标注是模型自己的阅读判断而非规则核实的 Finding）。

### 3.6 Compaction 三形态与信息损失（CCR N-4 的落地）

真实语料观测到至少三种历史压缩形态，`ctxgraph` 的编辑分类天然覆盖全部三种（不依赖任何模板/标记，模板只是事后补充证据）：

| 形态 | 机制 | manifest 编辑 |
| --- | --- | --- |
| 外挂式 | 独立的摘要请求，之后新历史带明确标记 | `Contract` + `Fork` |
| 原地替换 | 同一 lineage 内，中段被一条改写后的消息取代 | `Splice`（`revision` 关系随之触发，见 §3.4） |
| 静默截断 | 无独立 LLM 调用，消息直接消失 | `Contract` |

每个缝合边界 Step 渲染一段信息损失摘要（`buildCompactionInfo`）：压缩前/后 token 数（取自记录的 `Usage`）、被吞掉的实体（在前驱最后一次请求里出现过、这一步没再提到）与存活的实体（两边都出现）——纯规则、零 LLM 成本，**不修复，只揭示**：不判断丢失是否重要，只把可核查的事实摆出来，供人复核。

### 3.7 双 Journey 对比（`internal/story/compare.go`）

两份已算好的行为剖面（`JourneySummary`）逐项做差，`Compare(a, b JourneySummary, lang)` 纯粹是对 §3.5 九项指标的再加工，零 LLM、不额外采集数据。按模型的 token 拆分是列表型数据，不进这套只吃标量的 diff 机制；`len(Metrics.ModelSwitches)`（切换次数）作为第 13 项标量登记（`MetricModelSwitchCount`）——它是**路由环境**变量，读作"这两个 Journey 的路由环境是否不同"，不能读成"Agent 行为不同"。每行差值配一个 `Notable` 布尔（相对变化 ≥ 30% 且绝对差值超过按指标类型定的噪声下限），不生成自由文本。每行带稳定标识 `Metric`（`MetricCode`）与展示用 `Label` 分离（§4.2）。渲染成 `compare-<a>-vs-<b>.md`（含"成本估算"小节，与同名 JSON 的 `extras.cost` 同源）+ 同名 JSON（断头 Journey 受 `-include-partial` 门控、文件名带 `-partial` 后缀）；`-html` 另写一份对照看板，见 §3.4 的 HTML 看板段。`-compare` 两侧 id 与 `-journey` 一样走 `journeyPatternMatches`（shell glob 或无 glob 字符时按 id 前缀），各取首个命中的候选。

**`Comparison.Extras *ComparisonExtras`**（`ComputeComparisonExtras`，零 LLM 成本、从 `ctxgraph.Manifest`/`Step` 已有字段派生）：模型与端点核查（双侧 distinct `Manifest.Endpoint` 集合是否相同）｜Prompt 缓存命中率（逐轮 `CacheRead/In` 曲线 + 首轮/稳态/最值）｜System Prompt 规模与稳定性（最后一次 `SysChanged` 所在 Step 的 token 数、变更次数，以及一段有边界的原文节选，默认 20000 字符——按真实样本倒推的覆盖量级，"加载了哪些项目上下文文件"这类关键声明常落在第 1.5 万字符处）｜末轮上下文构成（= `Metrics.ContextCurve` 末项）｜总耗时 + 终止方式（墙钟总时长必须紧邻"净工作时长"展示并注明"不是效率指标"；双侧最后一个 Step 的 `Finish` 是最接近"是否被 loop detection 打断"的代理信号）｜**最终交付物对比**（若产出是通过一次"参数形状像文件写入"的工具调用落盘的，逆序取最后一次匹配、附内容节选，默认 6000 字符——VMR 唯一能直接看到"两边交付物本身差多少"而非"过程指标差多少"的证据；两侧都没有则整节跳过，不渲染"A 无 / B 无"）｜**成本估算**（`CostPair`，双侧各一个 `ComputeJourneyCost`，需 `*pricing.Resolver`——两侧都无定价时渲染"无可解析定价"，恰一侧有时另一侧显示 `—` 并加脚注避免被读成免费）｜`Sources []string`（本次对比实际读取的源审计文件路径，渲染成末尾"证据溯源"小节）。

**LLM 解读层**（`internal/story/llm.go`，可选、可整体降级）：`-llm-addr host:port -llm-model name [-llm-key KEY] [-llm-dry-run]`。端点解析只支持一种最简单的模式——手动指向一个已在跑的 VMR 实例（不做直连上游/健康检查/failover），只认 openai-completions 协议。证据包（`EvidencePack`）只含规则事实 + 两段有边界的原文节选（system prompt、最终交付物）+ 逐轮"工具名+brief"索引，不塞完整 transcript。system prompt 强约束：数字只能引用给定证据；文本节选的解读必须标注是模型自己的阅读理解；"节选里没提到"不能被当成"确实不存在"断言；必须专门声明 VMR 看不到什么。落盘缓存（`{outDir}/stories/.llm-cache/`，key 含 `model` 与 `promptVersion` 防误命中）+ `-llm-dry-run`（只估算证据包大小）+ 任何失败只在 stderr 打警告、不影响 `.md`/`.json`。

**分叉点检测（`computeDivergence`）**：两个 Journey 按位置对齐（`flattenWithTask` 展平成跨 Task 的 Step 序列，**不是**按 `Step.Seq`——两个独立 Journey 的 `Seq` 编号互不可比），逐位比较一个粗糙的结构签名（`toolSignature`：是否有 `tool_call` + 排序去重后的工具名集合），第一个不同的位置即候选分叉点。严重度：工具名集合不同（或一方有 tool_call 另一方没有）→ `DivergenceHeavy`；集合相同但参数不同 → `DivergenceLight`。产出是纯结构事实（`DivergencePoint`），渲染时紧跟免责声明"分叉点定位 ≠ 根因判定"——"为什么这个分叉更差"永远是解读层的可选推测。

**分叉点 LLM 解读层（`llm_divergence.go`）**：`-compare` given `-llm-addr` 且定位到分叉点时，额外发起第二次、单独缓存的 LLM 调用。证据包（`DivergenceEvidencePack`）只含分叉点本身 + 双侧各 2 步的简要信息。system prompt：分叉点本身是已确认的结构事实不需重新论证；"为什么分道扬镳"必须标注为推测；**绝不判断哪一方更好**（VMR 没有任务是否达成目标的信号）。两段解读落进同一份 `.md`，标题带 `scope` 后缀（整体对比 / 分叉点）以区分。

`SingleJourneyEvidencePack` / `DivergenceEvidencePack` / `EvidencePack` 三个 pack 类型共享同一套 `Interpret`/`cacheKey`/`buildUserPrompt` 调用链路（`Interpret[T evidencePackKind]` 泛型化后 `-compare` 原有调用点一行没改）。

### 3.8 盲区（诚实声明，避免"没记录"被误读成"没发生"）

| 盲区 | 说明 |
| --- | --- |
| 工具执行本身的副作用 | 只能看到耗时和返回文本，看不到真实写了什么文件、跑了什么命令——工具结果是 Agent 自述，不是独立验证 |
| 不经 vmr 的调用 | Agent 若对某些任务直连其他 provider，这段完全不可见；manifest 会出现无法解释的编辑，标为"来源不可见"而不是强行归类 |
| Agent 的内部状态 | 计划、记忆检索、循环检测等不体现在消息列表里的逻辑，只能从行为反推 |
| 跨 SessKey 桶、零字面重合的历史重写 | `ctxgraph.StitchGraph` 的哈希倒排索引在这种情况下没有信号可用（§3.1）——`vmr report` 的独立文本匹配（§2.1）能覆盖一部分，但不是全部；`vmr story` 目前没有对应的兜底信号，这类 Journey 会渲染成一个确认无前驱的断头 |

### 3.9 语料级统计（7.1/7.2，`corpus.go`）

`vmr story -corpus [-o dir]` 把"两两对比"扩展到"一批"——不是比较 A 和 B，是从几十到几百个 Journey 里找反复出现的行为倾向。批量构建走和 `-render-all` 同一条按字节预算分批的路径（`cmd/vmr` 的 `batchByBytes`：每批累计 `Manifest.Bytes` ≤ ~160 MiB，一批的回捞工作集用完即释放，再取下一批）；每个构建好的 Journey 只剩约 1% 的叙事结构，几百个累积起来仍是数百 MB，全部交给 `ComputeCorpusStats`。产出 `vmr-story-corpus.md`/`.json`，落在同一个 `{out}/stories/` 目录。三条纪律直接从设计文档继承，不是本节新提出：

- **相关性只报效应量**：`ComputeCorpusStats` 对九项指标 + 模型切换次数共 13 个数值字段（按模型的 token 拆分仍不含在内——同样是列表型，进不了这套只吃标量的机制）两两算 Spearman 秩相关（非 Pearson——不假设线性/正态），只报 `rho`，不报 p 值/显著性——当前语料规模（几十到一两百个 Journey）撑不住严格显著性检验，报 p 值只会制造虚假确定性。Markdown 只显示按 `|rho|` 降序的前 15 条（同 §7 "工具形态浪费 Top-5" 的惯例），完整列表在 JSON；真实语料上曾出现 48 条过阈值的相关性，不截断的话读起来是噪声不是信号，且相当一部分是同类时间指标之间的机械关系（如"净工作时长 = 模型时间 + Agent 侧执行时间"），本身就不是新信息。
- **无成功/失败标签**：VMR 零埋点前提意味着结构性拿不到任务是否真正达成目标的信号——`GroupComparison`（7.2，按 `Finding.Code` 分组比较净工作时长中位数）比较的是"耗时"这一个代理指标，不是效果；每处输出都明确写"不是确定性结论"。
- **样本量门槛，不是静默阈值**：`corpusMinCorrelationN`（5）、`corpusMinGroupSize`（3，双侧）——低于门槛的 Finding 分组对比不是不显示就算了，`SkippedGroupComparisons` 字段显式列出被跳过的 Code，Markdown 也渲染成一句"因样本不足跳过"，不是悄悄消失。

真实语料验证（137 个候选 Journey）：`exact_repeat_tool_call`/`plan_execution_misalignment` 命中组的净工作时长中位数比未命中组高 34%/38%，双双越过 `notableRelThreshold`（30%，复用 `compare.go` 已有阈值）——这是这批语料上第一个"某类 Finding 确实伴随更长耗时"的数据支撑，而不是纯粹的直觉；同时也如实验证了 §2.5 的顾虑本身成立：直接列出全部相关性会是一张 48 行的表，Top-15 截断是必要的可读性处理，不是可选项。

---

## 4. 输出语言（多语言支持）

`vmr report`/`vmr story` 支持英文（默认）与简体中文两种输出语言：当前目录放一份 `report.yaml`（`language: zh`）自动切换，或 `-lang en|zh` 覆盖（优先级更高）。`vmr story -compare` 的 LLM 解读层跟随同一个语言设置。不支持除中/英外的第三种语言（真正需要时再加，见 §4.7），不引入 i18n 框架（两种语言的静态文本，标准库足够），不本地化文件名/路径与 JSON 里的结构化字段。

### 4.1 `internal/i18n`：按来源文件组织的类型化文本

新增的零依赖叶子包，只装类型化的双语文本，不装渲染逻辑、不装业务判断——`internal/report`/`internal/story` 都合法依赖它，它不依赖两者，也不依赖 `internal/config`（语言这类"报表/叙事产物的展示偏好"不属于路由部署配置，`report`/`story` 本来就经常在没有 `config.yaml` 的场景下运行——见 §4.4）。

文本**不放在一个大文件里，也不放在消费它的 `report`/`story` 包里**，而是按"它服务哪个源文件"一一对应拆成多个小文件，延续 `internal/report` 里 `section_*.go` 已经验证过的组织原则：`internal/i18n/report_workload.go` 对应 `section_workload.go`，`story_render.go` 对应 `render_md.go`，以此类推。每个文件导出一个"取当前语言这一份文本"的函数，返回一个只在这个文件里定义的 struct，不做成横跨全部章节的巨型 `Catalog`：

```go
// internal/i18n/report_workload.go
type WorkloadText struct {
    Title, ByModel, ByWorkload, Model, Protocol, Requests string
}

func Workload(lang Lang) WorkloadText {
    if lang == ZH {
        return WorkloadText{Title: "负载分布", ByModel: "按虚拟模型", ...}
    }
    return WorkloadText{Title: "Workload Distribution", ByModel: "By Virtual Model", ...}
}
```

**字段拼错直接编译失败**（`t.Mdoel` 是编译错误，不是运行时空字符串）——这是选 struct 而不是 `map[string]string` 查表的唯一理由。跨章节复用的词（"模型"/"请求"这类在多个章节都出现的表头）不设全局公共词表文件——放在它们第一次出现的那个章节对应的文件里，其它章节重复声明一遍自己的字段。一个"公共词表"文件会制造一个和任何章节都没有一一对应关系的杂项容器；重复几个词条换来的是"改哪个章节的文案，只用打开哪个章节对应的一个文件"。

对于**完整句子**（自动发现的结论、Journey 断裂提示、对比结论），struct 里放的不是格式串字段，而是**函数值字段**，中英文各自的语序完全自由：

```go
type EfficiencyText struct {
    ToolSchemaWasteFinding func(shape string, requests int, wasteGB, utilPct string) FindingText
}
```

这样 Go 编译器/`go vet` 的 printf 检查在编译期就独立核实每个语言分支自己的 `Sprintf` 调用，不需要额外写"两种语言占位符数量必须一致"的反射测试——不存在"两边共用同一个模板、其中一边漏填"的风险类别，因为压根没有共用模板。这与"一个格式串 + 两语言共享同一套 `%s` 占位符顺序"的方案相对：后者要求译者倒着数第几个占位符对应哪个参数，是一类肉眼难查的错误源。

语言值像 `report.Build` 现有的 `pricing *Pricing` 参数一样，作为普通参数逐层显式传入，不用包级单例/`atomic.Pointer` 存一份"当前语言"给所有函数隐式读取——这与项目里唯一的包级可变状态先例（`adapter.registry`/`strategy.conditions`）解决的是本质不同的问题（"编译期注册的一组实现，运行时只读" vs "这一次调用要用哪个语言"），不应该套用同一个模式。代价是给约 15 个 `report`/`story` 的导出函数各加一个 `lang i18n.Lang` 参数——这些函数已经在传 `rep *Report2`/`o Row` 这类参数，多一个 token 不构成心智负担的质变。

### 4.2 identity 与展示文案分离

`Finding` / `MetricDiff` 各带一个不随语言变化的稳定编码字段——`Finding.Code`（`FindingCode`，如 `tool_schema_waste`）、`MetricDiff.Metric`（`MetricCode`，如 `model_ms`）。测试与任何程序化消费者只依赖编码；`Finding.Finding` / `MetricDiff.Label` 是纯展示文案（曾经这两个字符串兼职 identity 并写进 JSON，语言一旦可配置测试就会随机失败）。

### 4.3 JSON 契约：叙述字段跟随 `-lang`，`Code`/`EvidenceAnchor` 是稳定机器锚点

`journey-<id>.json`、`compare-*.json`、`vmr-report.json` 三种产物的叙述性字段
（`Finding.Finding`/`Implicated`/`Action`、`MetricDiff.Label`）**统一跟随 `-lang`**，
与同一次调用产出的 `.md` 用词一致——不再存在"JSON 版本固定英文、只有 Markdown 跟随语言"的
特例。程序化消费方唯一应该依赖的稳定锚点是 `FindingCode`/`MetricCode` 这类枚举标识符（如
`tool_schema_waste`/`model_ms`，§4.2 已经把它们从展示文案里分离出来）和 `EvidenceAnchor` 这类
原文逐字摘录——它们从不参与本地化，`Code` 已经是"写一个脚本时不用先判断这次报告用了哪种语言"
这句话的全部依据，叙述句子再额外锁死英文是重复保险，不是唯一防线。

两个类型达成这条规则的**具体机制不同**：
- **`MetricDiff.Label`**（14 个固定标签，无嵌入数据）：`Compare(a, b JourneySummary, lang i18n.Lang)` 直接接收 `lang`，循环体调 `i18n.MetricLabel(lang, string(spec.Code))`（纯静态查表）算出 `Label` 写进 `Comparison.Rows`；`RenderComparisonMarkdown` 直接读 `cmp.Rows[].Label` 不重复查表。
- **`Finding.Finding`/`Implicated`/`Action`**（拼了插值数据的完整句子）：`buildFindings(rep, lang)` 保留 `lang` 参数；`report.Build`/`BuildCached` 自身**不接收** `lang`（刻意保持语言无关），内部先算一份英文默认写进 `rep.Efficiency`，`cmd_report.go` 在写 JSON 前用 `report.LocalizeEfficiency(rep, lang)` 覆写。`section_efficiency.go` 的 Markdown 渲染路径**不读**被覆写的值，保留独立的 `buildFindings(rep, lang)` 调用——否则会引入一条隐藏的调用顺序契约（`Markdown()` 必须在 `LocalizeEfficiency()` 之后），未来新增调用点不遵守就会静默产出语言不一致的输出。给 `Build`/`BuildCached`（已有 6/9 个参数）加 `lang` 本可避免这个顺序依赖，但会牵动数十处测试，改动面大两个数量级，选后者——多付的成本只是一次内存内的纯函数重复调用。

`story.Interpret` 的 system prompt 本身指示模型用 en/中文回答（§4.5），不受这条规则约束——它产出的是模型自由文本，不是模板拼句。

### 4.4 配置与命令行

`vmr report`/`vmr story` 的受众是分析这批审计日志的人，往往不是部署路由进程的人：把日志从生产环境拷到自己笔记本上跑 `vmr report`，或者在 CI 的一次性容器里批量生成报告——这些场景下手头常常没有、也不该有 provider API key。这类设置因此**不进 `config.yaml`**，落在一份专属、轻量的 `report.yaml` 里，跟 `config.yaml` 同构：`report.yaml` 本身 `.gitignore`（可以放真实的 `llm_key`），提交进仓库的是模板 `report.example.yaml`（完整字段见该文件）：

```yaml
# report.yaml — vmr report/vmr story 专属配置，与 config.yaml 完全独立
language: zh          # en (默认) | zh
output: reports        # -o 的默认值
details: false           # vmr report 专属，-details 的默认值（默认按需生成，见 §2.5）
include_partial: false   # vmr story 专属，-include-partial 的默认值
llm_addr: ""              # vmr story 专属，-llm-addr 的默认值
llm_model: ""              # vmr story 专属，-llm-model 的默认值
llm_key: ""                  # vmr story 专属，-llm-key 的默认值；明文或 ${ENV} 都可以
llm_cache_dir: ""             # vmr story 专属，-llm-cache-dir 的默认值；两处都不设 = 永不缓存
self_traffic_client_tags: []   # 两条命令共用，P6.4——额外排除的自指流量 client_key_tag，
                                # 通常不需要填，llm_key 派生的那一个就够用（见下方说明）
```

每个字段都只是命令行同名 flag 的兜底默认值：flag 显式传了就赢，没传才看这份文件，文件里也没有就落到 flag 自己的内建默认（`resolveString`/`resolveBool`/`flagPassed`，`cmd/vmr/reportconfig.go`）——`resolveLanguage` 当初定下的 `-lang` > `report.yaml` > 内建默认这个优先级，后来给其余每个字段原样套用，不是各写一套。两个 bool 字段（`details`/`include_partial`）在 struct 里是指针，不是普通 bool——一个 flag 的零值（`false`）本身就是合法的显式选择，没法靠"是不是零值"判断用户到底传没传，只能用 `flag.FlagSet.Visit` 拿"这个 flag 到底出现在命令行没有"这个事实，report.yaml 侧同理用指针的 nil/非 nil 表达"这个字段到底写没写"。

`llm_key` 是这份文件里唯一可能敏感的字段——`report.yaml` 已经 `.gitignore`，明文写 token 跟 `config.yaml` 里写 provider API key 是同一个安全模型，不需要额外保护。仍然支持 `${ENV_VAR}` 展开（`expandReportEnv`，对整份文件的原始文本做替换，跟 `internal/config` 的 `expandEnv` 同一套 `${NAME}` 语法，各自独立实现，不共享代码——`report.yaml` 刻意不依赖 `internal/config`，见本文件包注释），纯粹是给想复用某个已有环境变量（比如跟 `config.yaml` 的 provider key 共用一个）而不想在两份文件里各写一份明文的人一个可选项，不是强制要求。

`llm_cache_dir` 是唯一没有内建默认值的字段——早期实现里它硬编码成 `{output}/stories/.llm-cache`，只要开了 `-llm-addr` 就自动落盘缓存；现在改成两处（flag、`report.yaml`）都不设就完全不缓存，缓存目录必须是用户显式点名的地方，不再有隐式路径。

**自指流量排除**（P6.4）：`vmr analyze -llm-addr` 的解读调用经 VMR 自身路由回流进审计日志，混进去的
token/成本是分析行为本身的开销，不是被分析工作负载的一部分。识别规则只算一次，放在 `cmd/vmr`
组合根（`selftraffic.go`）：`audit.KeyTag(llm_key)`——跟 `api_keys` 认证给每个 key 打标签用的
是同一个取尾变换，所以自然算出自指流量在审计日志里会留下的那个 `client_key_tag`，不需要用户
另外配置。`self_traffic_client_tags` 只在"自指流量用了另一个、已经轮换掉的历史凭证"这种边缘场景
才需要填，多数部署留空即可。每种模式（默认套件、`-journey`、`-compare`、`-corpus`）都默认排除，
`-include-self-traffic` 关闭；`vmr-report.json` 的 `meta.self_traffic_excluded` 如实记录排除了
多少条。`vmr report`/`vmr story` 两个过渡别名各自保留自己原来的排除口径不变（P9.5 之前，
`cmd_report.go` 没有 `-llm-key` flag，只读 `report.yaml` 的 `llm_key`；这处输入不对称只在统一
入口 `vmr analyze` 下自然消失，见下）。

**`vmr analyze`**（P9 + P14/P15 CLI 与模式收敛）：单一分析入口，一套 flag 集合是 `vmr report`/`vmr story` 曾经各自拥有的 flag 的并集。
- **变焦与子集模式（互斥选择器）**：
  - `-journey`/`-compare`/`-corpus`：单任务叙事、成对对比、语料统计——选中其一时**只跑 story 半区对应视图，不跑宏观报表**；
  - `-macro-only`（P15）：**仅运行宏观聚合报表**（等价于以前的 `vmr report`）；
  - `-list-only`（P15）：**仅生成候选索引与列表**，不执行任何 Journey 渲染；
  - `-story-only`（P15）：**仅运行叙事套件**，不生成宏观报表。
- **默认全套件模式**（无上述互斥选择器）：先跑 story 半区、再跑 report 半区，共用同一个 `-o` 目录，产出完整互链的套件。
- **渲染范围与物化控制开关**：
  - 默认预渲染范围（P14）：默认只预渲染**非噪声候选**（`!story.IsNoiseCategory`，即 `task`/`cron`/`subagent` 均纳入预渲染，仅 `heartbeat` 跳过预渲染并折叠）；
  - `-render-all`：将渲染范围放宽到物化全部候选（含 `heartbeat`）；
  - `-details`：显式为所有被渲染的 Step 物化全量 `details/*.md` 文件（默认按需懒加载生成，见 §2.5）。

`vmr report`/`vmr story` 降级为过渡别名：仍是独立的 `flag.NewFlagSet`、独立的默认值、产出与
收敛前逐字节相同，调用时向 stderr 打印一行迁移提示，不强制任何人切换。三者在 `cmd/vmr` 内部
共享同一套执行函数（`runReport`/`setupStoryRun` 及既有的 `renderJourney`/`renderAllJourneys`/
`compareJourneys`/`corpusStats`），`cmdAnalyze` 本身只做 flag 解析与按选择器路由，不重新实现任何
渲染或聚合逻辑——`internal/report`/`internal/story` 不因这次收敛发生任何改动，两个 internal 包
依旧互不 import，`cmd/vmr` 依旧是唯一同时看到两半区的组合根。

刻意没有做"一次扫描、一份缓存、一次建图"的深度合并：P3 之后两条命令共用同一个内容哈希分片的
`.parse-cache/`，`analyze` 内部先跑 story 再跑 report 时，report 那一趟扫描已经是热缓存命中，
不是重新解析——真正的单遍扫描收益因此有限，而实现成本（把 `AnalyzeSessionsCached` 的扫描与它的
图构建拆开，让 report/story 都能接受一个已经建好的 `*ctxgraph.Graph`）不成比例，予以搁置。

**顺序不是任意的**：story 半区必须先跑、report 半区后跑——`report.Markdown` 只在渲染时
`stories/vmr-stories.md` 已存在才会挂链接（`loadStoriesLink`，P6.2a），story 先跑能让这条边
在**第一次** `vmr analyze` 调用就命中，而不是要等到第二次运行。

默认路径是当前目录下的 `report.yaml`（不存在就安静跳过，回退默认值，不报错），也可以用 `-report-config path` 显式指定。schema 与解析（`cmd/vmr/reportconfig.go`）不经过 `internal/config`——字段不多，不需要那套面向路由配置的复杂校验，但同样用 `yaml.Decoder.KnownFields(true)` 严格解码：拼错字段名是加载错误，不是静默的无操作。

文件本身缺失/损坏时的降级行为：`report.yaml` 不存在时安静跳过；存在但解析失败，或某个字段值非法（如 `language` 不是 `en`/`zh`）时降级为该字段的内建默认并打印一行 warning——但**只有在这份路径是用户用 `-report-config` 显式指定时**才对"文件不存在"本身也打印 warning（区别于自动探测 `./report.yaml`：那种情况下文件不存在是正常状态，不该出声；显式指定的路径缺失，多半是拼写错误，值得提醒）。`-lang` 本身给了错值是唯一的例外，是硬错误而非降级（用户主动输入的，不是 best-effort 场景）。

### 4.5 LLM 解读层的语言联动

关键约束：中文报告配中文解读，英文报告配英文解读。`internal/story/llm.go` 的 system prompt 因此拆成 `i18n.LLM(lang).SystemPrompt`，两个完整常量各自完整叙述规则，不做"共享骨架 + 局部替换"——这段文本是给 LLM 读的完整指令集，硬拆共享/差异部分只会让人更难核对两个语言版本的规则是否真的等价。`promptVersion` 缓存 key（`compare-llm-v2`）追加语言维度（`compare-llm-v2-en`/`compare-llm-v2-zh`）——两种语言各自独立缓存，语言切换自然触发重新调用而不会误命中另一语言的缓存结果。

### 4.7 扩展性

新增第三种语言：`internal/i18n/lang.go` 加一个常量、`Parse` 认识新的字符串值；每个 `internal/i18n/*.go` 文件里对应的文本函数加一个 `case` 分支——不改任何一行 `report`/`story`/`cmd/vmr` 的代码，因为它们只认 `i18n.Lang` 类型和从 `i18n.Workload(lang)` 等拿到的 struct，不关心内部有几种语言分支。新增一条文案：在对应文件里给相应 struct 加一个字段（或函数字段）、两个语言分支各填一个值。这条路径没有被架构性堵死，但没有为它预先做任何多余准备（没有语言注册表、没有插件化翻译加载器）——第二个真实需求出现之前，"支持任意多语言"是一个假设的需求，与 `internal/taskseg` 只有两个 profile 实现、刻意不做自动检测注册表是同一个原则。

---

## 5. 关键决策与取舍

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
| `vmr report`/`vmr story` 内部对同一批文件各跑一次独立扫描（`AnalyzeSessions` 内的 `ctxgraph.Scan` 通道 + 报表自己的 `collect()`/`analyzeFile` 通道），用 goroutine 并发而非合并成一趟 | 合并成单一遍历，两边共享同一份解析结果 | 合并需要把两套本来独立演进的特征提取（`ctxgraph` 的哈希/lineage vs 报表的工具签名/角色统计等）耦合进同一个循环体，代价是架构复杂度；并发跑两条独立通道用 goroutine 就能把"审计文件读两遍"的墙钟代价从翻倍压到大致不变。语料规模涨到需要正视这件事之后，先做的是更小的一步：`ctxgraph.Scan` 那条通道加了文件级哈希缓存（`ScanCached`/`vmr-requests.json`，见 §2.5），文件内容没变就跳过它的解析，`collect()`/`analyzeFile` 通道仍未缓存、仍全量重跑——合并成单一遍历、让 report 直接消费 `ctxgraph.Manifest` 仍是更大的一步，尚未做 |
| 报表的独立 compaction 文本匹配（`linkCompactions`）与 `ctxgraph` 的结构化缝合（`linkStitchedLineages`）并存，不用后者取代前者 | 统一成一套机制 | 两者覆盖不同场景：结构化缝合基于精确哈希匹配，对"零字面重合的历史重写"没有信号；文本匹配能覆盖这个盲区，代价是精度较低。合并会让报表在最需要它的场景（标准的独立摘要调用）里失去唯一还有效的信号 |
| `internal/chatmsg` 承接三方（`ctxgraph`/`story`/`report`）共享的消息解析/实体抽取，不各自维护一份 | 各包各自实现 | 曾经真实发生过：`extractEntities` 一度是 `story` 包的私有函数，`report` 需要同样的能力时面临"复制一份"或"下沉"的选择——下沉到两者都已依赖的 `chatmsg`，换来的是以后只有一处规则要维护，不增加任何一方的依赖面 |
| 语言配置走独立 `report.yaml`，不进 `config.yaml`（§4.4） | 复用 `config.yaml`，加一个 `language` 字段 | `report`/`story` 本来就不依赖 `internal/config`，且这两个命令经常在没有 `config.yaml`（无 provider 密钥）的场景下运行；语言是纯展示偏好，不该绑定到一份含敏感凭证、面向路由部署的配置文件上 |
| 叙述字段（`Finding.Finding`/`MetricDiff.Label`）跟随 `-lang`，与 Markdown 一致（§4.3） | JSON 里固定英文，只有 Markdown 本地化 | `Code`/`MetricCode`/`EvidenceAnchor` 已经是程序化消费方唯一应该依赖的稳定锚点（§4.2）；叙述句子再额外锁死英文是重复保险，不是唯一防线。这个项目里唯一真实存在的 JSON 消费脚本（`_eval/calibrate_p1b.go`）只匹配 `EvidenceAnchor`，从不依赖叙述文本本身；≤3 人、聚焦中国大陆场景的团队，`report.yaml` 的 `language: zh` 基本等于"全程只想看中文"，JSON 里混一半英文对这个使用模式没有实际价值，只增加认知负担 |
| 动态拼句用函数值字段（`func(args...) FindingText`），不用位置化占位符模板（§4.1） | 一个格式串 + 两语言共享同一套 `%s` 占位符顺序 | 中英文语序天然不同；共享模板要求译者数第几个占位符对应哪个参数，是一类肉眼难查的错误源，`go vet` 的 printf 检查覆盖不到"两边模板参数对不上"这类错误。函数字段让每个语言分支各自是独立类型检查过的 `Sprintf` 调用 |
| 详单渲染按需懒物化（§2.5，P13） | 默认套件批量全量写盘 `details/*.md` | 批量模式预生成数百个 Journey 的详单会产生数百 MB 磁盘写入与数十秒延迟；纯函数链接生成允许按需懒加载，只在用户单点钻取（`-journey`）或显式 `-details` 时物化，兼顾可读性与执行效率 |
| 详单单轮差分与坐标回溯（§2.5，P13） | 每一轮详单内联完整累积历史消息 | 累积内联导致历史轮次呈 $O(n^2)$ 冗余膨胀；单轮增量展示 + 前驱坐标超链接消除了跨 Step 冗余，且通过坐标保持了完整的可追溯性 |
| 详单模板版本感知（`renderTemplateVersion`，§2.5，P12） | 静态文件命中即跳过 | 渲染模板、转义与样式升级后，若只依赖数据指纹会导致磁盘旧文件呈现陈旧视图；引入版本号让陈旧文件在下次执行时自动重绘 |
| 候选噪声分类收敛：仅 Heartbeat 折叠（§3.4，P14） | 同时折叠 Heartbeat、Cron、Subagent | 实测显示只有高频空转的心跳轮询（Heartbeat，通常 <10 轮）属于噪声；Cron 和 Subagent 承载实质工作流，应与 Task 平权展开并纳入默认套件预渲染范围 |
| CLI 模式收敛：`vmr analyze` 单一入口 + 离散正交模式开关（§4.4，P15） | 引入层级枚举或保持多命令分散 | 保持 Unix 扁平命令行风格，通过 `-macro-only`、`-list-only`、`-story-only` 等离散布尔开关表达子集视图，学习成本最低且与现有旗标习惯一致 |

## 6. 实测结论（真实语料，7112 条记录 / 809K 条消息实例 / 752 会话）

- **只读末轮消息丢失 26%–99% 的内容**（6 个真实会话实测）——任何"取末轮/按 session 分组/认模板"的简化方案都会给出错误结论。
- **95.86% 的相邻编辑是纯追加，真正的断裂只占约 1%**，但集中在最长、最值得精读的会话里（例：一个 444 请求的会话内部断裂 10 次）——这是"切分成本低、缝合成本高"这条架构判断的实测依据，也是为什么大部分任务不需要缝合就能得到完整叙事。
- **一次真实压缩案例**（79 条消息、57 对 tool_call/tool_result 被一次改写压成 4 条消息）逐位复现：净工作时长手算 381.0s/18.4%，代码复算 380.983s/18.40%，几乎逐位吻合。
- **一次隐藏断裂案例**：某会话内部 20 轮纯追加（cover 0.60→0.97）之后突然 79 条消息骤降到 4 条（cover=0.25）——同一开场白锚点原样保留，是"anchor 存活型"Contract 编辑的真实样本，也是 §3.2 那条历史缺陷的实证来源。
- **一致性验证**：752 个会话中 718 个与单个 lineage 一一对应，34 个被 `ctxgraph` 正确切成多段（对应上面的隐藏断裂类型）；lineage 缝合在同一语料上 68 个断裂中 62 个成功缝合、6 个正确识别为"疑似同源"未缝合、0 个跨桶时间窗违规。
- **F9 因果配对不变量**：全语料 406534 个 `tool_call`/`tool_result` 配对，零孤儿。
- **Finding 检测器（137 个候选 Journey 校准）**：九条规则层检测器提交前均在真实语料上跑过校准（§3.5a），修复四处真实假阳性/精度问题；语料级统计验证出 `exact_repeat_tool_call`/`plan_execution_misalignment` 命中组的净工作时长中位数比未命中组高 34%/38%。

## 7. 已知限制、暂不处理的事项

已识别、但判定"动它的收益低于扰动成本，或数据不足以支撑动它"的项。每项都不是 bug，列在这里是为了下次有人重新发现它时不必从头论证一遍。

| 项 | 现状 | 不动的理由 |
| --- | --- | --- |
| `stitch.go` 的 `stitchCompactionScore`/`stitchHeadPruneScore` 两档阈值 | 初始值，只在 2026-07-14..28 这批语料上验证过产生的分布是否合理 | `edit.go` 的 `contractLenRatio`/`forkCoverage` 已经过真实语料校准，这两个缝合阈值还没有；语料规模变大、或出现风格差异很大的新 Agent 框架时，应该重新跑一遍分布检查，而不是假定当前值继续成立 |
| `Metrics.ErrorRecoveryCount` 在纯 openai-completions 语料上偏低估 | 只识别 anthropic-messages 协议的 `is_error` 内容块标记，openai-completions 协议没有对应的标准字段 | 强行在 openai-completions 响应体里猜"这是不是一次错误结果"就是 §3 反复强调的"宁可粗糙也不猜语义"的反例；等 openai-completions 一侧出现可靠的结构信号再补，不用启发式凑数 |
| `report` 的独立 compaction 文本匹配（`linkCompactions`）用 200 字节子串比对，理论上存在误配对的可能 | 实测语料上未观测到一例误配对 | 提高比对长度/加校验会增加误报"没匹配上"的风险（真实摘要文本本身会被压缩改写）；现阶段的精度换取的是覆盖率，等真的观测到一例误配对再收紧 |
| `vmr story` 对"跨 SessKey 桶、零字面重合的历史重写"没有兜底信号（§3.8） | `vmr report` 的独立文本匹配能覆盖一部分同类场景，`vmr story` 没有对应机制 | 两个产物的输入相同，但 `vmr story` 目前没有移植 `linkCompactions` 那一类文本匹配信号；这类 Journey 会渲染成一个诚实的断头而不是错误缝合，符合"宁可断开"的原则，不算 bug，只是覆盖率上限还没到 |

## 8. 可选扩展（尚未实现）

以下模块设计上互不依赖，可以任意挑选、任意顺序实现，不阻塞彼此：

- ~~LLM 解读层~~：已实现（§3.5c 单 Journey、§3.7 `-compare` 整体解读 + 分叉点解读）。唯一还剩的部分：语料级统计（§3.9）暂无对应的 LLM 叙述层——"把统计上有支撑的模式翻译成人话"这一步还没做。
- ~~HTML 单文件看板 + 脱敏~~：已实现（`-journey` / `-compare` 各一份单页看板），详见 §3.4 渲染段与 §3.7 的 compare 看板段。
- **Subagent 树**：Event 模型已预留 `parent_step_id` 挂载点，渲染层预留分支，但识别信号（system blob 与主 Journey 同源、时间窗完整嵌套在某次工具调用之间、其输出 blob 出现在主 Journey 后续 tool_result 里）尚未验证过命中率。开工前先跑采样脚本验证这三条信号在现有语料上是否成立——不预先假定这套判据有效。

---
