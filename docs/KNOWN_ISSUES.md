<!-- Ver 2026-09-11 -->

# vmr — Known Issues（已知问题与架构取舍清单）

> **定位**：vmr 已知问题、待评估演进项与刻意架构取舍的**唯一权威、持续维护的当前状态清单**。
> 发现新问题先在这里查一遍，再决定它是不是新的。
> **不在这里**：还没做的**新功能**（新视图 / 导出 / 信号）属产品路线，见 `ROADMAP`——与技术债性质不同，不并列排期。
>
> **维护原则**
> 1. **只记当前系统里还找得到的东西**：要么是待办（§2），要么是「看着像 bug、其实是有意为之」的取舍（§1）。**已修复的问题不进这里**——它已经不存在了，代码本身就是证明。那段历史在 `CHANGELOG.md` 和 git history 里。
> 2. **三类分区**：§1 确定不修（连同决策逻辑，避免被反复重新提出）｜§2 待定问题（分组、组内按用户价值 × ROI 排序）｜§3 跨组排期结论。
> 3. **每条都要能对源码核实**。核实不了的，说明已过期，删掉。
> 4. **散文可压缩、可重组**；§1/§2 的编号只是稳定身份标识，供历史引用对号，不承诺连续、也不代表顺序。源码注释不靠编号回指本文档——每条注释自带完整理由，本文档只在「这个取舍值得被独立追踪」时补一条 §1。
> 5. **不写演进史**。一条取舍只陈述当前结论与当前理由；被推翻的旧措辞直接替换，不留「原先如何、后来如何」的分层。

---

## 0. 当前状态

- **稳定性与安全性**：无凭证泄漏、并发竞态或服务阻断级别的缺陷；单机生产环境可稳定运行。`copyFlush` 异常路径下的 `respnorm` 查询方法全部互斥锁同步，`-race` 全绿并经端到端流式断开集成测试守护。
- **自动化基线**：`internal/archtest` 强制导入单向边界、文件/函数行数预算、文档引用完整性，全绿。`go test ./...` 全绿（`internal/...` 与 `cmd/vmr` 均含 `-race`）。
- **§2 分布**：高危 0；中危路由/配额半区 `2.2` / `2.17`、LLM 校准 `2.18` + 1 项发版前必做（`2.97`）；其余均为低危。

---

## 1. 刻意取舍，不是缺陷

> 以下基于项目核心哲学（KISS / YAGNI / 单二进制 / 零代码侵入）做出，已论证过，不需要重新论证。
> **推翻其中任何一条是允许的，但必须先知道自己在推翻它，并给出新的理由。**

### 1.0 永久不做（架构红线）

语义缓存（对确定性编程/Agent 任务是正确性隐患）｜MCP 网关与工具执行拦截（不在标准 LLM API 线路上）｜Web UI / 内嵌 DB / RBAC / 分布式 / 跨实例 quota｜协议互译 / bypass 模式｜`.so` 运行时插件（坚持编译期 blank-import 注册）｜让价目表进实时路由热路径｜通用 HTTP provider（映射 DSL）｜更多 LLM 检测器 / 对比维度 / benchmark 维度（分析半区标 v1-complete，新增维度从默认冲动改为需理由的例外）。

### 1.1 运行时与并发

- **`health.Registry` 全局互斥锁不分片**：单机场景锁持有只是纳秒级 map 读写，分片增复杂度无吞吐收益。
- **`health.Registry.Available` 无生产调用方，但不是死代码**：它是唯一无副作用的路由资格查询（`Acquire` 会占用 half-open 探针名额），`health` 与 `router` 的测试断言端点状态都靠它——用 `Acquire` 去查会改变被断言的状态。「无生产调用方」不等于「可删」。
- **`fails` 的语义是「当前退避曲线下的连续失败深度」，跨曲线切换重置为 1**：`ErrAuth`/`ErrEndpoint` 走 10min 起的 long 曲线，其余走 5s 起的 transient 曲线——两条曲线基数差 120 倍，共用一个深度计数器会让一串 5xx（transient 才退到 80s）后的一次 401 直接顶到 1h 封顶。**不适用于**：当作「这个端点历史上一共失败了多少次」的累计量。`internal/server/active_probe_test.go` 因此不能用 `fails >= 2` 当「走的是 ReportFailure 不是 ReportNeutral」的代理判据，只有 `last_error` 能区分两者。
- **`ReleaseProbe` 与 `ReportNeutral` 行为相同但保留为两个方法**：前者是「名额先还、健康结论稍后再报」（`forwardSuccess` 在流真正跑完前用它），后者是「这次结果对健康没有信息量，到此为止」。合一会让 `forwardSuccess` 的调用点读起来像已经下了终局结论，而它恰恰还没有。
- **探针成功只做衰减（`fails--`），真实流量成功才清零**：探针是 `max_tokens=300` 的小请求，对限流/上下文受压端点的成功率系统性高于真实的 20 万 token 请求——用最容易通过的信号解除对最容易失败流量的保护，正是 429→5s 冷却→探针成功→满额流量→429 的循环成因。由 `TestFlappingEndpointKeepsBackoff` 钉死的保证是「探针成功与真实失败交替时，深度永不回落到最浅档」；`fails>0` 期间对真实流量恒 `available=false`（last-resort 释放是唯一例外，见 §2.85）。**已知残留**：连续探针成功可把 `fails` 衰减到 0 并把端点放回常规池原优先级，对「慢而未死」的灰区上游构成池级振荡循环（transient 首档 5s 只降频）；根除方案登记在 §2.99，待触发。
- **退避冷却带 ±10% 抖动，且抖动也作用于已封顶的值**：封顶端点整点齐射正是抖动要防的场景，因此结果可超名义 cap 至多 10%。**例外**：`Retry-After` 路径不抖——那是上游指定的节奏，不是我们的估计。
- **后台探针按 requests 口径计 1，对 token 限额计 0**：探针消耗真实上游额度，`metric: requests` 的账号侧一定计数，本地账本不计就是系统性欠记。token 侧不解析探针 usage（响应体有 `probeBodyCap` 封顶），计 0 是诚实下界而非精确值。
- **`log_dir` 在 Unix 上被 `flock` 独占，第二个指向同目录的实例拒绝启动**：两个进程对同一 JSONL 做 housekeeping 会把两股 zstd 流交错写进同一归档，`rename` 之后**不可恢复**；同根还有双进程 O_APPEND 行交错与 quota 双写覆盖。锁文件 `.vmr-audit.lock`（0600）成为 `log_dir` 的常驻文件，不参与压缩与保留。**不适用于 Windows**：那里没有 flock，`acquireDirLock` 是 no-op——唯一临时文件名仍保证归档不被交错写坏，但双进程的其余后果依然可能发生。用 pidfile 替代会因崩溃残留把启动永久卡死，比问题本身更糟。`internal/livestats` 自带一把**独立**的同机制 flock（`.vmr-stats.lock`），因为它不寄生 audit：`-audit=false` 时 audit 锁根本不存在，而"不留正文仍要监控"是一等场景——第二个实例的 livestats 拿不到锁就降级为纯内存（不写 slim/rollup），不污染首个实例的归档。
- **`HealthKey` 取 SHA-256 前 4 字节**：单实例端点规模下碰撞概率可忽略。
- **健康状态机的退避冷却参数硬编码**：坚持「零调参」，不暴露难以科学校准的旋钮。
- **`copyFlush` 的 goroutine + channel 流水线**：避免在底层连接层设全局 Deadline 破坏 TLS/Header 超时语义。
- **客户端取消时不停止计费**：上游已生成的 token 厂商照收，路由侧照收才与账单对齐；改成不计费会让 `vmr analyze` 系统性低估消耗。取消的**传播**（中止上游连接）已通过 `BuildRequest(r.Context(), …)` 自动完成；取消的**检测/归类**（`router` 标 attempt、`server` 标审计 `Outcome` 为 `canceled`）需 `copyFlush` select 一次 `ctx.Done()`。
- **`respnorm.Read` 等待更多字节时返回 `(0, nil)`**：唯一消费方 `copyFlush` 显式处理；改成内部阻塞循环会让 idle 看门狗失去以读取为粒度的心跳。
- **`respnorm` 的 usage sniffing 不外移为 `router` 侧装饰器**：装饰器要在转发热路径每 chunk 多付一次接口调用；当前实现搭 `ingest` 已有的 per-chunk 循环，零额外开销。理由在 `internal/respnorm` 包注释末尾。
- **`respnorm` 的观测标记 `crlf_framing_suspected` / `thinking_process_pattern_detected` 不删**：字节未改动，只往审计 `norm` 串加一个标记，看似无消费者——实则被 `internal/reqdetail` 详单页逐条叙述，`thinking_process_pattern_detected` 另进 `internal/report` 的 `diagnosticNormMarker` → `EndpointRow.NormCounts`，作为「剥离规则是否失效」的跨请求频率预警。
- **`GET /health` 为存活探针而非就绪探针，永不因上游不可用返回非 200**：与上游健康绑定会让容器编排在所有供应商不可用时触发无休止重启，放大雪崩。需要就绪度的调用方消费 `/status` 的模型健康块。
- **`/status` 的 `traffic.requests` 含未鉴权/被拒请求**：口径是「进程见过多少 HTTP 请求」，不是「成功路由了多少」。精确语义在审计日志。
- **`traffic.by_status` 按请求（非 attempt）计数，且不保证 `ok+canceled+error == total`**：failover 中间 attempt 不计入，少数早断路径不记或记入 error。展示语义，非路由语义。
- **`/status` 的 `traffic.by_status` 在流式中途截断时记 `error`，与审计顶层 `outcome` 记 `ok` 口径不同（刻意不对齐）**：前者答「客户端是否拿到完整响应」，后者答「HTTP 交换是否在传输层正常完成」，各自口径内部自洽（`Telemetry.RecordOutcome` doc comment 与 `forwardSuccess` 调用点各自写了自洽说明）。
- **buffered / undecided 模式的 SSE 在上游中途断流时一律不 flush 已缓冲尾部**：`respnorm.Read` 错误分支只把**可安全交付**的字节交出去——非 SSE 响应 flush 部分 JSON（直连也是这个结果），SSE 仅 `modePassthrough` flush 尾部；`modeUndecided` / `modeBuffered` 一律不 flush，避免把未闭合的 `<think>` 泄漏给客户端（审计记 `truncated_withheld`）。随后 `forwardSuccess` 于全部记账之后 `panic(http.ErrAbortHandler)`，客户端 SDK 看到断掉的传输而非格式良好的空 200。这是「杜绝静默假成功」的硬要求，不是可优化的保守行为。
- **不对 HTTP 2xx 软拦截（如 MiniMax `input_sensitive`）做运行时 Failover，坚守字节保真透传**：个别厂商在触发内容合规拦截时返回 200 OK 且内嵌 `"input_sensitive":true`，同时给空回复或套话。不做 2xx 拦截与 failover 的三个理由：① **流式不可逆**——绝大多数 Agent 与生产请求走 SSE，首包与 200 状态行早已交付客户端，中途无法撤销再切候选；② **契约与计费违背**——上游已记 200 并扣配额，私自丢弃重试造成二次计费与延迟翻倍；③ **KISS/YAGNI**——其他主流厂商合规均返回标准 4xx，天然由 `handleErrorResponse` 归为 `ErrContent` 自动切换，为单一厂商的非流式边缘行为暴露配置项和运行时预读缓冲，给全体用户加认知负担。**当前方案**：`respnorm` 仅在审计层打 `soft_block_detected` 标记供排查，响应字节原样透传。事前关键词过滤是降低此类触发的唯一正规路线。
- **`respnorm` 缓冲区超 `bufferedCap`（8 MiB）即放弃规范化，转 opaque 原样透传**：`modeBuffered` / `modeUndecided` 缓冲越限时置 `s.opaque = true`、审计 `norm` 串加 `overflow_raw_passthrough`，把已缓冲字节原样 flush、后续字节直通。**已知代价**：这条路径不再走重写循环（`emitBlock`），因此**虚拟模型名改写在本条响应上不发生**——响应体留的是上游真实模型名。刻意取舍：失控流（缺 `[DONE]`、无限增长的 `<think>`）继续攒内存、或对超大 body 强行 splice，都比「这一条响应模型名没改写」更糟；opaque 透传正是直连的等价行为。`TestRespStream_UndecidedOverflowDegradesToOpaque` 反向锁死这条降级不再经重写路径。触发面：单条响应体量到 MiB 级且带 `model` 字段——正常聊天/Agent 流量下不出现。
- **厂商专属协议约束拒绝归 `core.ErrQuirk`，不复用 `ErrContextLimit`**：DeepSeek 思考模式要求回传 `reasoning_content`、Google 要求回传 `thought_signature` 这类「换个端点就好」的拒绝，`DefaultClassify` 归入专门的 `ErrQuirk`（切换 + 零冷却）。复用 `ErrContextLimit` 能得到相同 failover 行为，但审计标签会说谎——这不是上下文超限。OAuth 标准错误码同理独立归 `ErrAuth`。全量端点级 quirk 模块方向见 §2.48。
- **`/status` 端点项刻意不加端点级累计计数器（requests / ok / failed / tokens）**：`consecutive_failures` 出现在 `/status` 因为它是**当前健康状态**读数（liveness 视图）。端点级累计账是**分析半区**职责——`internal/report` 的 `EndpointRow` 已完整产出，数据源可持久化、可按时间切片。给 `/status` 塞一份进程内、重启即失的实时副本：① 与分析半区双账本（正是「一个分析数字复现一个路由数字必须差分测试锁定」要防的负担）；② 做全等于给 `router.Telemetry` 加一张按端点的动态 map，破坏它「全固定原子、热路径零 map 零锁」的设计。
- **审计响应方向不单独落盘上游原始 Body**：避免为每请求存两份几乎相同的全量响应体。`audit.Attempt` 响应方向 `Body` 为空，交付给客户端的响应体由 `Client.Response.Body` 统一记录；上游与客户端之间的全部改动由 `Attempt` 的 `Norm`（改写步骤列表）、`RawPreStrip`（剥离前的原始片段）与 `ObservedModel`（上游实际返回的模型名）精确记录，足以复现与审计上游真实响应。
- **`system.disk.free_space` 在 Windows 上是桩（恒 0）**：`syscall.Statfs` 无 Windows 等价物，而 Windows 不是目标部署平台。
- **`/log` 慢订阅者以「丢行 + 标记」处理，永不让日志热路径阻塞**：每订阅者一条有界 channel（`subBuffer` 行），满则丢行插 `... dropped N lines ...` 标记；`log.html` 不做自动重连（只手动重试按钮），避免重启风暴下的重连洪水。
- **启动 banner 与 panic 直写 stderr，tee 不捕获**：banner 只出现一次，panic 时进程将死，两者都不值得为 `/log` 引入第二条写入路径。

### 1.2 配置与协议

- **协议枚举命名为 `openai-completions` / `anthropic-messages` / `openai-responses`，路由侧零兼容负担**：**唯一兼容咽喉点**是 `audit.Record.UnmarshalJSON`——读到旧名经 `audit.CanonicalProtocol` / `audit.NormalizeEndpointLabel` 归一化，只服务分析侧读历史日志；`vmr replay` 不做兼容；config 带旧名是加载错误（strict YAML），但错误信息直接点名要改成什么（`internal/config/provider.go` 的 `unknownProtocolHint`）。**这是「版本必须匹配、不做兼容」原则的唯一刻意例外**——历史审计文件是不可变的既存事实。**拆除条件是事实，不是日期**：审计日志默认永不删除（`retention_days: 0`），旧协议名不会随时间自然消失，按日期排期必然误伤。前提改为：对当前全部审计语料 grep 旧协议名零命中，且用户确认没有离线归档需要再解析；满足后拆 `internal/audit/legacy_protocol.go`、`Record.UnmarshalJSON` 及其为此新增的 `internal/core` import 与两个 legacy-name 归一化测试。**不适用于**：把审计日志投递到外部归档、或从别的机器拷入历史日志的场景——那里旧名可能随时重新出现，兼容层应当永久保留。
- **CLI 与 Server 版本必须匹配，不一致直接报错不做兼容**：单二进制、可随时重启，`vmr status` 与 `vmr start` 理应同版本——不一致说明升级没走完，报错正是暴露它。`json.RawMessage` 式兼容层只覆盖一个滚动升级窗口却永久留在代码里，违反 KISS。此原则不留任何字段级例外。
- **`/status` 的 `instance.base_urls` 回显请求自身地址而非 `listen` 配置**：host 取自 HTTP Host 头、scheme 取自是否 TLS——调用方用什么地址访问 `/status` 就广告什么地址，这正是客户端该填的值。纯展示、不参与鉴权或路由，Host 可伪造无安全影响；刻意不做 `X-Forwarded-Host` 解析。
- **`base_url` 内嵌凭据在加载期报错，而不是在审计侧脱敏**：`base_url` 是自由字符串，`https://u:p@host` 或 `?api_key=...` 会原样进 `Attempt.URL` 落盘——审计脱敏只覆盖 header，这是脱敏模型的唯一旁路。在源头消灭比运行期脱敏正确：脱敏是永远追不全的黑名单。**适用于**：固定的凭据键名清单（`api_key`/`token`/`secret`/`password` 等）与 userinfo 段。**不适用于**：自定义网关用非常规键名承载凭据的情形——刻意不做「值看起来像 key」的启发式判断，那会误杀 `api-version` 这类合法参数。错误信息只回显键名，绝不回显值。
- **价目表的数值防线建在 `pricing.ParseTable`，不下沉到 `internal/config`**：`ParseTable` 是标准/curated 表的唯一解析入口，config.yaml 的 `providers[].pricing.rates` 侧另有自己的 `positiveFinite`/`nonNegativeFinite`——两层各自的入口各自把关。NaN/±Inf/负费率一律加载期硬错误；定价与配额已彻底解耦，一条脏费率的影响面只污染离线 `vmr analyze` 的 $ 估算，触达不到 `quota.Counters`（该结构自身也不含任何价格分量）。
- **`internal/config` 的二层费率解析不后置到 `router.BuildSnapshot`**：`config` import `pricing`、在 `validate()` 跑完解析，看似「配置层反向侵入用例层」，但只让 `cmd/vmr` 一侧另行解析、config 侧完全不校验会导致两份实现各自推断、容易漂移，是已否决的备选（Quota 设计文档决策表明文选定）。后置到 `BuildSnapshot` 还会摧毁「费率行四分量全给或全不给、`aliases` 目标必须存在」这些加载期校验——它们的价值就在于**加载期**能立刻报错，而不是等 `vmr analyze`/`vmr check` 跑一次才发现打错的字。
- **org 前缀请求名的费率解析兜底是**递归**重跑裸名，且残余误匹配风险刻意接受**：带 org 前缀的上游名（openrouter 的 `meta-llama/...`、together 的 `google/gemma-...`）四步全落空后，`resolveCanonicalKey` 用 `pricing.ModelBasename` 掐成裸名**递归重跑全部四步**（含 `<provider>/<basename>` 步）——只重跑裸名/后缀步会让「同名不同写法在同一 provider 上解析到不同价」的不对称换个位置重现。不做的：按厂商维护 org 前缀注册表（太精确所以太脆）、全局归一化请求名（会失配账号层 `pricing.aliases`/`rates` 的原始名 key）。残余：网关自造 id 掐掉前缀后恰与另一模型裸名同名时会命中那家的价——与 substring 匹配同型的极小概率误匹配，可用 `pricing.aliases` 先钉（优先级更高）。
- **`report.yaml` 解析失败是硬错误退出，文件不存在才是静默 no-op**：严格解析（`KnownFields`）配上软降级是最坏组合——一个键名笔误会静默关掉**全部** report.yaml 设置，包括自流量排除（分析工具自己的开销于是混进被分析的工作负载）。文件不存在是合法的「未配置」；显式 `-report-config` 指向的文件不存在则报错，那是用户自己给的指针。报表头另有一行写明本次实际应用的配置文件路径（`Meta.ReportConfigPath`）——「没找到 report.yaml」和「本来就没有」在产物上必须可区分。
- **环境变量未定义时静默展开为空串，不支持 `${VAR:-default}`**：保持配置解析简单明确，默认值在 YAML 里显式写出。
- **配置形态的四个表达力边界**（ttl / endpoints / disabled / model_defaults 简化落地时敲定，无兼容层）：
  - **`ttl` 时间换算用固定长度近似**（d=24h、w=7d、mo=30d、y=365d，与 quota `every: 1mo` 同约定），额外接受裸整数=天；TTL→天数换算**向上取整**，避免亚天值被截断为 0 恰好落进 audit/imgprep 的「0 = 不删/不淘汰」旧语义。
  - **同一模型名在 `model_defaults` 里只能有一条声明**：`Config.ModelDefaults` 是 `map[string]ModelDefaultEntry`，重复 key 由 YAML 自身拒绝；exact key 与 `"*"` 通配同时匹配是回退链（exact 优先）不是合并，因此不存在取 max / 后写覆盖的问题。今天能表达的是「一条 entry，可选整体限定到某个 provider 子集」，**不是**「同一模型对不同 provider 子集各开一条不同取值」。后者只能靠虚拟模型层 `override` 绕（把需要不同上限的 (provider, model) 对拆到单独虚拟模型上）；若两个 provider 就是要挂在同一个虚拟模型的同一份 `endpoints:` 里，这条路也走不通。是否改成 `map[string][]ModelDefaultEntry` 待真实需求出现再评估。
  - **`BuildQuotaSpecs` 双形态**：`BuildSnapshot` 路径走 `BuildQuotaSpecsDisabled`（跳过 disabled provider，不留无主计数器）；`BuildQuotaSpecs` 保持原行为供 `replay.chargeReplay` 使用（replay 定向单个 (provider, model)，解析其 quota spec 与在线路由状态无关，是正确语义而非兼容残留）。
  - **全 disabled 的 endpoint-group 保留空 route**：`(protocol, virtual model)` 路由仍在但零 endpoint，走常规 no-candidates 失败路径而非 unknown-model（测试钉住）。**disabled 引用告警逐引用点发**：一个 disabled provider 被 N 处引用产生 N 条 warning，每条点名具体位置；嫌吵再聚合为 per-provider 一条。
- **多协议适配器（`adapter/{openai,anthropic,openairesponses}`）保持独立子包**：三协议底层已有真实分叉（Anthropic 529 特判、Responses 顶层 `input` 数组与 `RewriteInputRoles`、`x-api-key` vs `Authorization`）；独立子包支持编译期 `init()` 注册与独立单测，新增协议零侵入。合并成参数化结构体只是把多态改写为字符串 `if` 分支。
- **不引入端点级通用运行时 quirks 插件系统**：坚持编译期确定性，只对已证实的厂商行为差异做受控修复。
- **`TopLevelProbe` 的契约是「探测」不是「校验」，不检查尾随字节**：它回答「这团字节是不是某个协议的对话请求」（结构探测，供 `RequestFacts` 与 sticky 指纹用），不承诺「字节流在探针返回的结构之后没有尾随垃圾」。加尾随检查会背离字节保真透传：透传层本就把原样字节直送上游，多余的校验只会把合法流量拦下来。
- **不合并 `Dimension`（排序）与 `Condition`（淘汰）**：淘汰依赖请求事实，排序只比较端点属性，职责分离保证接口纯粹。
- **ProviderGroup 的多 Key（`api_keys:`）已实现，运行时均衡与分级 Failover 仍不做**：运行时 KeyPool（请求期在池内随机选 Key）会违反 `core.Endpoint`「构造后不可变、`HealthKey()` 只算一次」这条贯穿 health/sticky/quota 的不变式。实际落地是「配置期展开成多个独立 `core.Endpoint`」：`Provider.APIKeys`（`{label: key}`）在 `config.Parse` 里展开成 `<name>-<label>` 命名的独立 `Provider` 并就地重写引用，下游全部按 `Provider.Name` 字符串解析、零改动。这个形状架构性地绕开了均衡（谁排第一不可预先指定，只能读 `vmr check` 的实际展开结果；没配 quota 时排第一的吃全部流量）与配额聚合（每把 key 独立 Provider 名、独立 quota 池）两个难题；分级 Failover（402 跳 Key / 5xx 跳 Provider）维持原判，留到看到真实需求。

### 1.3 校验与防御性编程

- **`/status` 的网络可达性与身份认证解耦，且复用聊天入口的同一把 `api_keys`**：网络范围由 `listen` 决定，认证由 `api_keys` 决定——未配 `api_keys` 时任何能连到端口的人都能读 `/status`。这把 key 同时是管理凭证：持有客户端 key 者能看到全部端点名、provider 身份、quota 消耗与配置路径。对单人/小团队代理这是正确的简化。`config.Check()` 对「非 loopback 且无 api_keys」给 warning。
- **`vmr status -addr` 回退读取本地 config 的 `api_keys[0]` 并发送到目标地址**：设计意图是让本机多实例免手工传 key；只发 key、不进 URL 或日志。目标地址是使用者自己敲的，不是网络层漏洞。
- **看板（`/status.html` / `/log.html`）把 API key 存 `localStorage`，静态外壳免鉴权直出**：外壳不含数据，数据请求走 `s.auth()`；key 只在浏览器本地持久化，不进 URL、不进服务端日志。所有配置派生字符串内插进 `innerHTML` 前均 `esc()` HTML 转义。`/log` 输出 `text/plain` 而非 SSE/JSONL（源头已是格式化文本）；无查询参数（回放窗口固定 512 行缓冲）。
- **`/help.html` / `/help.zh.html` 的 Agent 配置片段在浏览器就地装配，模型名 / effort / context 默认值不做服务端模板渲染**：`/help` 按架构必须公开免鉴权，服务端渲染这些值会逼它强制鉴权、或让服务端拿不到用户 Key。API Key 复用 `localStorage['vmr_status_key']`；服务端下发的 HTML 对这部分保留写死默认值，保证无 JS / 未鉴权时也自洽。**唯一例外**是 `{{BASE_URL_OPENAI}}` / `{{BASE_URL_ANTHROPIC}}` 两个占位符：`renderHelp` 按请求 Host 就地填成本实例地址（访客自己的地址、HTML 转义、不需要用户 Key），并有一段 JS 在代理重写 Host 时用 `location.origin` 复填。四点取舍：max-output 预算按 context 分档经验估计（VMR 无模型级元数据）；片段一律 vision-on（空 capabilities = 不受约束）；四个列表型生成器只枚举 `openai-completions` 模型；无浏览器 JS 测试基建，`TestHelpPage_SnippetFillEngine` 只做构建期字符串守卫。
- **`nil` 校验只加在跨包公共入口且一律 fail-fast，绝不静默兜底**：已加的是 `report.AnalyzeSessionsCached` 与 `journey.BuildChain`/`BuildAll`/`PreviewTitles` 四个入口——判据是「跨包公共 API + 后接并发扇出或递归组装」。包内被这些入口保护的函数不重复校验。
- **尤其不做「`prof == nil` 就回退到 `Generic`」这类静默兜底**：`OpenClawAware` 与 `Generic` 给出不同的任务标题与边界，静默换一个 Profile 会产出一份错误但看起来正常的分析结果，比 panic 难查。
- **持续性故障的日志按「错误文本相同」去重，不做事件级审计**：quota flush 失败（磁盘满、权限变更）与时钟回退都是持续性的，10 秒一次刷屏会淹没日志。flush 侧按错误文本去重（首次 + 每 10 次，附连续失败计数），时钟回退侧每进程最多一条 WARN。**已知代价**：两种错误交替出现时 flush 侧不去重（每 tick 一条——但交替本身就是有效信号）；时钟「回退→恢复→再回退」的第二次不再 WARN。**边界**：需要回退事件级审计的话，这里要换成带去抖窗口的计数器。
- **`vmr-quota.json` 的结构损坏整文件拒绝，绝不部分采纳**：静默丢掉一个 provider 的账本比报错更危险。版本戳不匹配、nil account map、null bucket 三者任一即视为损坏，由调用方 WARN + 从零开始——与既有的语法损坏路径同构。`version` 是真正的门而非「写而不校验」：有版本戳却不校验比没有更危险，下一个人会以为「有版本号所以安全」。
- **配额周期的惰性重置方向敏感**：只有周期真正前进（`ps > PeriodStart`）才重置计数。NTP 向后校正、VM 快照回滚、容器 TZ 变更都会让周期起点向后跳，而「不等即重置」会抹掉整个计费周期且随下次 Flush 落盘、不可恢复。反方向保留计数并 WARN。
- **原子写只做文件级 Sync，不做目录 fsync**：全仓的 CreateTemp+Rename 站点（quota 账本、audit 压缩、ctxgraph 解析缓存、reqdetail 证据、journey LLM 缓存）都不 fsync 父目录——掉电时 rename 的目录项可能未持久化，最近一次落盘可能丢失或回退。丢失代价分别是「统计计数回退到上次 flush」（quota，文件级 Sync 已做）与「缓存 miss 重算」（其余站点，靠读取侧的哈希/schema 校验把半写内容兜成 miss），全部落在各自 best-effort 契约内；而目录 fsync 每次落盘多一次系统调用，换来的只是把丢失窗口从「最近一个 flush 间隔」缩到零。若未来某站点升级为「不许丢」的契约（如计费级账本），在该站点单独补，而不是全仓统一加。
- **`fmtutil.DisplayZone` 保持裸 `var`，不封装线程安全访问器**：生产代码零写入点——全仓写入全在 `_test.go` 且相关测试无 `t.Parallel()`，`-race` 全绿。「让测试能确定性覆盖」本就是它存在的理由之一。
- **`.cache/parse/` 的分片文件名 = 内容哈希 = `FileCache` 的 map key，三者对齐，且不做孤儿回收 GC**：文件名即内容哈希使同名冲突天然不可能，反查代价取消（运维侧想按路径定位分片时 grep 分片内嵌的 `CanonicalPath` 即可），`LoadCacheDir` 仍是 best-effort 全扫描。`ctxgraph.SaveCacheDir` 只增量写入当前存在的分片，不删旧哈希孤儿——孤儿只是多占磁盘、永不扰乱命中，而缓存是完全可再生的派生产物，`vmr analyze` 可从空目录冷启动，「整目录删除重建」比任何 GC 更简单可靠。**结果取舍**：两个不同路径、内容相同的审计文件（备份副本与原件同批扫描）共享同一 entry，共享 Manifests 的 `Path` 绑定为 last-writer 的路径拼写——功能正确，但不保证指向最早扫到的路径。**触发条件**：`.cache/parse/` 体积超过同批压缩审计日志总体积，或升级后异常磁盘占用。
- **默认分析套件不物化 `details/`，`report` 的「文件」列判据是文件存在性而非 `-details` flag**：`writeJourneyFile` / `renderJourneys` / `renderAllJourneys` 带 `materializeDetails` 入参——只有单条下钻、`-compare`、`-render-all` 传 `true`；默认套件的脊柱「→ detail」与 sysprompt 指针渲染成行内「文件:行」坐标（`Manifest.Req` 的纯函数），不写盘、不留 404 链接。`report.detailCell` 因此不能只看本次的 `-details`：`vmr analyze` 先跑 journey 半区（可能已批量物化）再跑 report 半区，纯 flag 判据会谎报「没写详单」或反之——改查 `r.DetailFile` 是否真实存在（一次 `os.ReadDir` 建 set）。常驻守卫测试盯着「默认套件 `details/` 为 0、指针是坐标非链接」，人为改回无条件物化当场失败。这条纪律反复退化过多次，靠测试锁死。

### 1.4 包边界与依赖

- **`internal/server` 对 `chatmsg` 的传递依赖是既有豁免**：`archtest` 对 server 的禁 import 清单列 report/journey/ctxgraph/taskseg/reqdetail，**不列 chatmsg**——server 依赖 router，而 router（quota 计量、usage 解析）与 respnorm 合法消费 chatmsg，`go list -deps` 意义上的传递依赖必然成立。两半区契约守的是「server 不得直接消费分析半区的包」，不是依赖闭包纯洁性；把 chatmsg 从 router 剥离是另一个量级的改动且无消费者受益。
- **`archtest` 的包边界守卫是单向的，与规则本身同构**：「分析半区不 import 路由半区」是单向禁令，`import_boundaries_test` 只需要守这一半；「audit JSONL 记录是唯一耦合」那半句是**数据流事实**，不是另一条可机检的 import 规则。不要因为「只守了一半」提案加反向守卫——反向（路由 import 分析）本来就是合法的依赖方向。
- **`imgprep.ImageInfo` → `audit.ImageInfo` 的字段拷贝**：换 `imgprep` 不依赖 `audit`，保住公共工具包零依赖边界。
- **`chatmsg.ReassembleSSE` 与 `respnorm` 的 SSE 状态机保持分离**：前者面向离线完整语义提取，后者面向在线字节级保真转发，关注点不同。
- **`ctxgraph.Manifest.MsgIdx` 是承载性导出数据，不是死字段**：`ctxgraph` 不导出任何哈希函数，`MsgIdx` 是包外把 `Keys[i]` 对回 `chatmsg.Messages` 元素的**唯一通道**。两类消费方：`cmd/vmr` 的 `vmr diff` 用它把 manifest 哈希位置映射回真实消息角色（`tailLine` / `msgRoleAt`），是一等生产消费者；`internal/journey/structure_test.go` 用它验证「内容寻址坐标确实解析到所声称的内容」这条不变量。删掉它等于同时砍掉 `vmr diff` 与该不变量的包外验证，还要让全部用户白付一次全语料重解析。
- **`jsonscan` 与 `adapter` 的边界：按「引擎 vs 语义」切，不按「是否出现协议字段名」切**——「字节级扫描与 splice 改写引擎」整体归 `jsonscan`（含 `RewriteModel`/`RewriteRoles`/`RewriteInputRoles` 这类带协议字段字面量的改写函数，fuzz 覆盖也在此包）；「协议路由语义、适配器构造、错误分类」归 `adapter` 及以上。`adapter` 侧的协议字段字面量（`"model"`/`"stream"`/`"messages"`/`"input"`）也因此不从 `jsonscan` 导出复用：它们是不可变字节常量而非共享状态，「知道这些字段名的含义」正是把 `SessionFingerprint`/`TopLevelProbe` 留在 `adapter` 的领域知识。**不要再提案按字段名归属移动这批函数**。
- **`core` 准入规则是「禁令 + 显式豁免清单」，豁免项不是待清理项**：`Endpoint.HealthKey`/`Name`/`Freeze` 保留在 `core`——它们是「双半区无主、纯计算于 Endpoint 自身字段」的值对象方法（`HealthKey` 是 health/sticky/quota 共用的端点身份，`Freeze` 只是把两个纯函数 memoize 供快照构建），外移到任何单侧都会制造反向依赖或循环。`core.StickyBackstopTTL` 同理不迁回 `internal/sticky`：迁回制造一条 `config` → `sticky` 的新依赖边，仅用于读一个常量；不做这个校验则 `sticky_ttl` 超过 backstop 的配置会「看起来被接受、实际静默失效」。新增符号仍需逐个过审，但**不要再逐个提案外移这批豁免符号**。
- **`internal/core/core.go` 不按领域拆成 `endpoint.go`/`quota.go`/`pricing.go`**：同包拆文件不改变任何编译依赖，是代码导航整理不是架构重构。真正解决「core 会不会长成上帝包」的是准入规则，已写在包注释里并对存量逐条复核过。
- **`internal/probe` 不登记进 `zeroInternalDepPackages`**：那张表的语义是「**承诺**永远零依赖」，不是「当前碰巧零依赖的都登记」。`probe` 独立成包是为避免 `diagnose`→`router` import cycle，未来 import `core` 完全合理。（`rundir` / `buildinfo` / `sysinfo` 与 `tokenutil` 均作为基础叶子包登记守卫。）
- **`internal/digest` 是全系统唯一的 Digest 构造**：D8 的「一个 cache-digest 构造」由结构而非差分测试保证——纯 stdlib 叶子包、零内部依赖，`internal/report` 是唯一调用方。线格式（uvarint 长度前缀 + sha256 链）由手算向量测试钉死。
- **`internal/report/cost.go` 的端点标签切分不并入 `core.SplitEndpointLabel`**：后者兼容 `:` 与 `/`，前者只认 `:`。放宽 `$` 成本估算那个调用点会改变旧格式日志的历史报表金额——一次需单独评审的行为变更，不是「统一实现」的顺带产物。
- **降级 token 估算的 fallback 刻意不对称：请求侧回退原始字节、响应侧一律 0**：统一规则是「用对内容最忠实的可用表示估算内容 token；剩余字节量到的若不是内容本身（SSE 信封、压缩/损坏的 opaque 字节），宁可为 0——量错一个量比没有估算更糟」，且每一侧都必须镜像路由半区实际扣减的基。两侧信息状态不同，同一规则推导出的分支就不同：请求侧的原始字节是「内容 + 脚手架」，且路由侧输入扣减（`Facts.EstimatedTokens`）本来就是 raw 基——回退 0 会让报表与实扣劈叉；响应侧的原始字节在截断/opaque 场景量的是传输不是生成（实测可达 71 倍虚高），回退 raw 等于把它复活。规则全权落在 `EstimateDegradedTokens` 的 doc comment（`internal/chatmsg/tokenest.go`）；不对称行为由 `TestEstimateDegradedBasis_FallbackAsymmetry` 钉死，对齐情形（两侧可提取文本、两侧 opaque）由 quota parity 测试钉死。**不要「统一」两侧的 fallback**——任何统一方向都已论证过是复现已修过的 bug。
- **用量折算（精确 vs 降级）的跨包入口刻意只有一条，没有单标志合并形式**：`quota.TokenCountersSides`（纯标量入参）是唯一权威实现，`router.TokenCountersSides` 是 `chatmsg.Usage`→`quota.TokenUsage` 的唯一翻译层（`report` 直接调 `quota` 侧，`archtest` 禁它 import `router`）。合并后的「some usage was seen」单比特信号无法区分完整账本与部分账本，把 partial 当 exact 记账正是 sides 拆分要消灭的 bug 类。**不要以「API 对称」或「给未来消费者留入口」名义复活单标志包装**——只能给单比特的调用方本就该逐侧决策。
- **金额展示统一收敛在 `fmtutil` 一处**：`FmtCurrency`（恒两位小数 + 货币符号，`$124.36`/`¥34.20`）是所有「账面金额」的唯一格式；`FmtCurrencyPrecise`（四位小数）只用于单价/微额列（报表「端点性价比」章的成本/1M out 与成本/成功请求）。跨语言 fixture（dashboard testdata/fmt_cases.json）钉住 FmtCurrency/FmtCost/FmtCurrencyPrecise 两侧逐字节一致——两侧都按 Go strconv 的 half-to-even 口径对二进制精确平局舍入（`0.125` → `$0.12`），JS 侧不是裸 `toFixed`（那是 half-away-from-zero，平局值上会与 Go 差一分），而是 common.js 的 `goFixed` 全程 BigInt 精确复刻，fixture 内含平局 case 防回退。**不要在渲染层手写 `FormatFloat`/`toFixed`/`Sprintf("%.4f")` 渲染金额**；表格列的货币也可在表头标注，此时单元格内的符号属冗余但无害，不算漂移。非货币数字（token 数、统计量、配额余量）的 `toFixed(2)` 与此无关，不收编。
- **LLM 文本的 Markdown 结构转义做在 Finding 构造时，不做在渲染侧**：Finding 文本同时进 Markdown 产物与机读 JSON，而 Markdown 的**结构**破坏——反引号、竖线、行首结构标记（ATX 标题、`-`/`*`/`+` 列表项、有序列表 `1.`、块引用 `>`、主题分隔线 `---`）——在 `i18n` 模板层修不了：模板把文本插进结构位置，转义必须发生在插进去之前。**已知代价**：finding 文本此后永久带反斜杠，非 Markdown 消费者（如 JSON 导出）会看到转义痕迹；行首的 `>`、数字+点+空格（如 `>= 5`、`2026. `）也会被转义，渲染结果不变但 JSON 侧可见。
- **`imgprep` 的 `map[string]json.RawMessage` 不与 `jsonscan` 的字节扫描统一**：图片降采样要重算尺寸并重编码，是深度结构化重写，字节 splice 做不到。这是三个 sanctioned deviation 里最大的一个。
- **`imgprep.HasImageMarker` 的宽松预检维持「宁误报不漏报」，不收窄**：宽松的 `bytes.Contains` 预检会让正文里 `"image_path"` 之类的代码文本误触整套降采样反序列化——但误报只多付一次 JSON 解析成本，解析后结构化 dispatch 找不到真实图片块即原样放行，无正确性后果（`TestDownscaleTextMentioningMarkerIsNotAnImage` 钉死「误报≠误判」）。收窄需枚举全部已知图片引用形态 token，新增形态（如 Anthropic `tool_result` 子块 `type:"image"`）会重引漏报；漏报是硬路由 `HasImage` Condition 的正确性 bug，误报只是性能小税。
- **不对 OpenAI 工具返回做 `error:` 关键字模糊嗅探**：实测全量生产语料近 50 万条 OpenAI 工具调用结果，结构化 JSON 错误字段 0 条，全部是自由文本 stdout/stderr。子串模糊嗅探会引入海量代码输出/测试用例的假阳性。只对协议原生结构化错误标记（如 Anthropic `is_error`）做确定性统计。
- **模型/端点展示面的一致性靠统一口径 + 契约测试，不靠共享结构体**：运行时视图以 `/status` 的 `models` 数组为唯一权威（`vmr status` CLI 与 `status.html` 直接消费同一 JSON）；人类可读模型标签 `"<name> [<protocol>]"` 只在 `fmtutil.ModelLabel` 一处定义。刻意不统一的三处：`/v1/models`（协议面 schema）、`vmr check` 的分层 config 视图（看配置缺口）与 `/status` 的聚合运行时视图（并集/最大值）、`vmr diagnose` 的扁平 Result 数组。`/status` JSON 形状由 `internal/server/admin_status_test.go` 契约测试锁定。
- **`i18n` 的一批微文件不合并**：与 `internal/report/viewmodel_*.go` 的「一节一文件」硬规则一一配对（`archtest` 强制），合并击穿全局行预算，且改一节文案从打开小文件变成在大文件里找。
- **`i18n` 的 `type XxxText` + `if lang == ZH` 样板不改写成 `map[Lang]T` + 泛型 `pick`**：改写只消掉每文件 2 行分支，占体量的 struct 定义与两份字段赋值一行都省不掉，还新引入泛型 helper 与「key 缺失怎么办」。收益为负。
- **不把分析半区拆成独立二进制**：坚持「单二进制单文件分发」。
- **不引入 DuckDB / cgo 做数据聚合**：保持纯 Go、跨平台零 C 依赖。
- **`go.mod` 保持裸模块名 `vmr`**：改名要动全项目 import 路径，无实质收益。

### 1.5 产出与工程惯例

- **用 Go 结构化代码而非 `text/template` 渲染 Markdown**：复杂条件列、对齐与动态脚注在 Go 里更容易保持类型安全和可读性。
- **不自建 Markdown→HTML 的渲染层**：Markdown 产物的人读入口就是 Markdown 阅读器与看板骨架页（后者直接消费 JSON 切片，不渲染 .md）；再要 web 化展示时，用现成渲染器做转换层，而不是在数据层养一个只覆盖子集的解析器。**推论**：`journey-compare.html` 复刻 compare Markdown 的章节结构时，`llm_interpretation.text` 这类携带 markdown 表格/标题的正文按 `white-space: pre-wrap` 原样铺开，不做结构化渲染。
- **看板骨架页的 chrome 是英文单版，数据侧才本地化**（`WriteSkeletons(dir)` 只吃目录不吃语言，同一份 HTML 同时写进 `reports/` 与 `reports-en/`）：导航、tab、表头、banner、章节标题、`formatDelta` 的 `new` 之类**渲染器计算出的**文本恒英文；`rows[].label`、findings 叙述、LLM 正文、excerpt 等**由 JSON 携带语言**的部分跟随 `-lang`。于是 `reports/`（中文数据）下看板是「英文 chrome + 中文数据」。**不要把它当 bug 报**；要 chrome 也双语，正解是 `common.js` 持 `UI_TEXT[lang]` 字典 + `WriteSkeletons` 按产物语言注入 `window.__LANG`、对带 `data-i18n` 属性的静态文本节点做替换，约 1 人天，已登记为 §2.95 待办。
- **`-render-only` 在 L3 缓存命中时信任磁盘上已有 `.md` 产物**：L3 缓存以 ViewModel 指纹 + 渲染器版本 + 语言为判据；当 L3 命中且磁盘目标 `.md` 已存在时直接跳过渲染与写盘。手工改过 `.md` 需要强制重绘时用 `-no-cache`。
- **配了 quota 的部署里，`vmr-quota.json` 变化会使 L2 缓存失效**：报表「账户（Provider）消耗与额度」章与 `macro/finance.json` 的 `provider_quotas` 直接读 `<log_dir>/vmr-quota.json`（路由半区每次计费请求都会重写它），其内容因此并入 L2 输入哈希集——否则同批日志隔时重跑会在 L2 命中路径上给出过期的额度进度与已用量。副作用：路由半区正在活跃承接流量时，对同一批历史日志反复 `vmr analyze` 会频繁 L2 miss（配额计数器确实在动，这一章本就该重算）；这是刻意的保守失效。不含 quota 限制的配置完全不受影响（`vmr-quota.json` 不进指纹）。`period_elapsed_pct` 这类纯 wall-clock 派生值仍是「as of 上次全量运行」——要 render-time 现算需把它移出切片、渲染侧按 `manifest.generated_at` 重算，属独立的低优改进。
- **分析产物的整套版本戳收在输出根 `manifest.json` 的 `format`**：整份产物一个版本单位，单文件不再各自长版本戳（`.cache/parse/` 分片仍自带 `CacheSchemaVersion`，那是内部缓存不是产物）。切片 schema 收敛为加性优先，删改字段必须 bump `format` 并在 CHANGELOG 标注 Breaking——骨架页可被用户复制定制，用户副本就是事实上的 schema 消费者。
- **分析产物 ZH 术语的 loanword / 全译两套约定并存，刻意不统一**：Markdown/报表侧保留英文特性名 + 中文描述词（`Sticky 有效性`、`Compaction 还原`、`账户（Provider）消耗与额度`，journey 叙事正文里 `system prompt` 也一贯是外来词）；看板侧全译（`系统提示词` / `上下文压缩`）。两套各自内部自洽。全量统一要改十余处 i18n 字符串 + 发给 LLM 的 prompt 正文 + `UserGuide.zh.md` 与 Analytics 设计文档里的既有章节名，收益纯观感、还牵出「Compaction 该不该译」之争（类比 `prompt cache` 通常不译）。**触发条件**：同一 section 内出现自相矛盾的形态（如标题译、紧邻正文不译），才值得局部收敛。新增 i18n 字符串时跟随同 section 已有正文的形态。
- **索引折叠与默认渲染范围只把 `heartbeat` 归为噪声，不含 cron / subagent**（`journey.IsNoiseCategory`）：真实语料实测——heartbeat 每候选最多个位数请求，而 cron 与 subagent 都有双位数请求的候选，含全语料最长的一条 journey（subagent）。索引显示分割与 CLI 默认渲染范围共用这一个判据，避免二者对同类候选给出不同答案。cron 因此也会进重复任务聚类——同一 cron 多次运行聚在一起有诊断价值（如首次失败重跑）；锚点标题剥掉 `[cron:<uuid> ]` 装饰再展示。
- **重复任务聚类（`clusters.go`）：相似度阈值硬编码、不进 report.yaml 与 L2 指纹，候选列表看板不按簇分组**：3 人团队 + 单一使用场景下，一个永不会调的阈值做成配置项、以及在 `index.md` 之外再做一套看板分组，都是过度设计（Agent 会话可视化 deepdive 曾列这两项，独立验收时按第一性原理裁掉）。阈值 `0.45` 写在代码里带注释；`journeys/index.json` 已带 `clusters` 字段，将来真要看板分组时前端自取即可。聚类键用任务标题（taskseg 多由开场指令派生）而非初始指令全文，是可接受的近似。**触发条件**：出现反复 A/B 阈值的需求，或看板成为聚类的主要入口。
- **stitch 缝合同时要求比例阈值与绝对下限（共享去重键 ≥ `stitchMinAbsOverlap`）**：断裂后的开头 manifest 天然很短（system + 摘要 + 第一条指令），一条共享消息就能把比例顶过任何阈值——而那条消息往往正是 SessKey 本身的构成成分，它共享是**因为**这是同一个会话的锚，不是因为发生了 compaction（证据循环）。比例防长会话、绝对值防短会话，两道闸正交。不满足下限**降级为 `AmbiguousMatch` 而非淘汰**，候选仍可供人工查看。论证谱系与 `edit.go` 的 `spliceMinTailMatch` 相同。
- **同 SessKey 候选有 72h 宽松时间上界（`stitchSameKeyMaxGap`），超窗候选预过滤出局，最强者仅作诊断兜底**：「用户可以走开几天再回来接同一个 anchor」对人类成立、对机器相反——同一 anchor SessKey 下堆积最多的是定时/心跳任务：开头模板相同、彼此无关、可跨数百小时，正是促成 `stitchCrossBucketMaxGap` 的那批假匹配，只是发生在桶内所以那道闸从没管过。规则是**淘汰优先于排序**（与 `strategy` 包 `Condition`/`Dimension` 分离同型）：超窗候选不参与赢家竞争，避免「高分超窗者先赢再降级」遮蔽窗内合法前驱；仅当过滤后无任何窗内候选时，最强超窗者作为降级 `AmbiguousMatch` 边保留供人查看。
- **消息内容哈希剥离 Anthropic 的 `cache_control` 标记**：`cache_control` 是缓存控制元数据，不是对话内容；客户端逐轮移动缓存断点会改变哈希，把一次纯 Append 误判成内容编辑，整条 lineage 谱系失真。**证据状态要如实说**：机制已从代码确认（`hashJSON` 对原始消息对象全字段哈希，标记确实进哈希输入），但本机语料太小，**按协议拆 Append 比例无法产生统计意义，未能从语料实证**。剥离本身严格更正确，故仍实施。**已知副作用**：消息内容载荷里键名恰为 `cache_control` 的（如工具结果回显）也会被剥离——只影响哈希与 lineage 判定，不影响存储内容与渲染。
- **详情页在 `report` 与 `journey` 之间字节一致，靠的是两侧传入同一个 `(record, manifest, prev)` 三元组 + 指纹携带 `m`/`prev` 身份**：只做其中一半都不够。曾经的失效形态是：`report` 侧在 `group()` 里对 compaction 记录先 `continue` 再赋值 manifest，于是它的 manifest 恒为 nil，而 `journey` 侧传的是真实 manifest；渲染指纹只含 lang/evidence，于是同名文件**先写者赢**——用户拿到哪个版本取决于渲染顺序。补上 manifest 消除差异源，指纹折入身份防同类复发。
- **LLM 解读层生成结构化 Finding 的准入与置信度契约**：LLM 判别器产出的 Finding 必须强制标记 `Source: "llm_inferred"`、离散置信度（`HIGH/MEDIUM/LOW`）与原文 `EvidenceAnchor`。仅 `HIGH` + 直接证据锚点的项以 Finding（⚠️）呈现并标 `[AI推测]`；`MEDIUM`/`LOW` 降级为参考提示。**锚点运行期强制校验**：`ComputeLLMFindings` 收完全部 detector 输出后逐条 `strings.Contains(真实 transcript, EvidenceAnchor)` 校验，非逐字子串即丢弃——但这是**防幻觉**检查，**不是防注入**：注入方就是转录本的作者，他能同时植入被引用的锚点和结论，`Contains` 必然命中。注入面靠另外两道闸收口：LLM 来源的 Finding **一律不参与 `pickDriver`**（仪表盘顶部判词只可能来自规则产出），且 `StepSeq` 不在本 Journey 真实步号范围内的 Finding **直接丢弃而不是 clamp**（clamp 会把攻击者选的序号映射到一个合法步）。问法严格约束在有证据支撑的事实性问题上（拒绝开放式主观质量打分），守住「揭示事实与过程异常而非冒充裁判」的边界。
- **`archtest` 的文档守卫只覆盖 `CLAUDE.md`、设计文档、本文件与用户指南，不扩展到 review 报告类文档**：后者会正当地讨论已删除的文件与「建议新增的 XXX 函数」。真正的风险（一份陈旧 review 被当施工依据）**用定位而非机制解决**：权威的当前状态清单只有本文件。
- **`archtest` 不加圈复杂度检查**：一次只加一个守卫。函数长度预算落地不久，确认不够用之前不引入第二个。
- **文件与函数行数预算线是提醒式绊线，非架构缺陷**：`internal/archtest` 的文件/函数预算（默认 700 / 120 + 豁免表）是轻量提醒机制，连 Warning 都算不上。未触线前无需焦虑、不需在常规 review 里逐个排查；一旦触线，按职责拆分重构，或逻辑内聚时临时按 +15~20% 调高豁免。
- **可维护性的核心在整体架构与设计复杂度，而非代码行数**：单人可维护性取决于是否守住 First Principles / KISS / YAGNI、是否消除了不必要的过度设计与复杂分支，而非机械度量行数或两半区体量比。
- **聚合浮点字段在冷/热缓存两次运行间的 1 ULP 级差异不追查、不消除**：浮点加法不满足结合律的教科书现象，不是可以「修好」的缺陷。唯一该做的事（差分/一致性测试用容差而非逐字节相等）**已经是现状**（`report/e2e_test.go` 用 `1e-6`、`quota_parity_test.go` 用 `1e-9*want`）。唯一需重新当作 bug 的情形：差异远超浮点精度量级，或开始出现在 `cost_estimate` 之外的字段上。
- **`buildinfo` 只输出 VCS commit 哈希，不人工编造语义化版本**：如实反映构建来源。
- **官方用量 API 不预先抽象 `Source` 接口**：YAGNI，等真正接入第一个厂商私有用量接口时再设计。
- **不维护外部贡献者 `CONTRIBUTING.md`**：与小团队运作方式不匹配。
- **`client_key_tag` 与 `key_label` 命名与推导刻意不统一**：前者代表调用方（`audit.KeyTag` 尾 8 位窗口 + 连字符截断，有 16 字符下限与 `-alice` 命名约定的历史包袱，文件名与既有报表已定型）；后者代表实际分发到的上游账号（沿用 config 的 `api_keys` label 叫法，未配置 label 时取 key 尾 6 位）。两者是相互独立的两个维度，在所有统计报表（slim、rollup、`/stats`、`/stats.html`）中并列记录，刻意不统一推导或合为一个字段。
- **in-flight 请求注册表（`router/inflight.go`）归属路由运行态，永不落盘、不结算进完成时账本**：排队、逐 attempt 发出、流式逐块盖章等事件发生在 router 内部，早于任何 audit record 产生；完成时钩子（`server.done`）对每个请求恰好记账一次，in-flight 仅补充"请求进行中"的内存观测空窗，条目在请求结束时整条删除，两条路径互不写对方的数据。
- **`last_byte_at` 与 `est_out` 逐块盖章、刻意不节流**：`last_byte_at` 的语义是"上游最后一块数据真实到达的时刻"，用于卡死检测；若按时间或字节数攒批，节流后的盖章时间反而滞后于真实末块到达时间，让已卡死的流显得"更新鲜"，方向恰好做反；成本上每块只有一次 `OutTokens()`（per-stream 互斥锁内读计数器，本来每块就进过一次）+ 几次 per-entry 原子写，零内存分配，千块/秒极端流下也远低于 JSON/SSE 协议开销，无需攒批。
- **body 上传与 probe 阶段对 in-flight 注册表不可见**：注册点设在 `TopLevelProbe` + `authenticate` 之后、`AcquireSlot` 之前，因为未获得协议与虚拟模型名前的请求无法归属维度，且慢速客户端 body 上传并不占用并发槽，不属于并发门排队观测的对象。
- **livestats rollup 文件只追加、永不删；内存只是它近 7 天的滑动窗口**：小时聚合行小（年万行级），文件当全量归档一直叠——唯一消费者是 `/stats`，`vmr analyze` 读的是 audit log。内存 `a.rollup` 启动按窗口过滤加载、运行中每次日切（`rollHourLocked` 里 `evictOldRollup`）逐出掉队的一天，所以读时 fold 是**不随部署年限增长的常数**（否则读缓存也兜不住一个越来越贵的 fold），`by_*` 累计口径因此是"滚动近 7 天"而非"自启动以来"。窗口按**日历日**对齐——`rollupRetentionDays`（7）个整天 + 当天，`now` 是 0:00 时正好 7 天、中午时 7 天半。文件的启动解析成本仍是 O(全历史)——`vmr start` 打一行"恢复行数 + 耗时"日志盯着，真到几百 ms 再上按天分文件（`vmr-stats-rollup-YYYYMMDD.jsonl`），现在不做。取 7 天不取 30：实测每 `(hour,dims)` 行 ~450B，7 天在小团队规模是 1–3 MB、内存里正好是控制台 `?range=` 最宽档能显示的量；lazy 从文件加载被否掉——为省几 MB 把读路径搞成带文件 I/O 的，比刚优化掉的 fold 还慢。理由与取舍见 LiveStats 设计文档的内存态一节与决策表。
- **Overview 头部告警 pill 的端点告警只在 cooldown 期间在列**（`server/alerts.go`）：cooldown = 该端点此刻被排除在路由外，是「需要动手的状态」；`consecutive_failures>0` 但未冷却的降级态由拓扑表 Health 列（带因果悬停）承载——无流量时残留失败计数不消零，进告警会把徽章永久钉在非零，违反告警收敛纪律（已登记于 console-unification 实施契约 §5）。
- **Log 页 level 芯片是前端启发式分类，后端 `/log` 流不带结构化 level**：`/log` 是与 stderr 逐字节一致的纯文本流，给日志行加结构化字段牵动 stderr 格式与全部日志消费者；芯片只影响终端着色与过滤（`classifyLevel`），纯属展示层。
- **`/stats.overall` 合并窗口块目前无内置消费者，作为 JSON 契约保留**：它是为控制台首屏那一段"全局 TTFT p50"加的（分位数不可跨 ring 合并，只能服务端在读时对样本并集算）；该 vitals段在 console polish 轮据用户反馈移除，`overall` 随之空转。删掉它是纯粹的契约收缩且要改测试，收益为零；留着无害（已测、可外部消费、首屏日后补延迟信号会重新用上）。无 ring 样本时为 `null`。见 console-unification 设计文档「未纳入本轮」与 LiveStats 设计文档 `/stats` 契约段。
- **零 attempt 失败（全端点冷却等）的错误类别由 `sampleFromRecord` 合成为 `no_candidate`，audit.Record 不扩充顶层字段**：一批失败导致所有候选端点进入 cooldown 后的请求是 0 attempt 的即时快速失败，路由半区未向上游发起任何 attempt，因而 `audit.Record.Attempts` 为空。`server.sampleFromRecord` 在 `len(Attempts)==0` 且 `Outcome=="error"` 时为 `livestats.Sample` 合成 `error_class="no_candidate"` 并由客户端侧 HTTP 响应码（503 等）兜底 `status`，使控制台 Recent Failures 能够准确区分并过滤最常见的级联冷却失败，而无需为了展示层需求扩充审计日志顶层 schema。

---

## 2. 待定与待解决问题（分组；组内按用户价值 × ROI 排序）

> 标题方括号里是**严重程度**（现在有多糟），不是优先级；它与组内顺序（现在做有多划算）是两个正交轴。
> 要排期看组内顺序与 §3，要判断「现在有多糟」看方括号。编号只是稳定身份标识，不连续、不代表顺序；
> 「决定不做 / 暂不做 / 非活跃」类条目排在组尾、各自带触发条件。

### A. 分析半区 · 大语料规模（内存与耗时）

#### 2.2 [中] `vmr analyze` 全内存聚合的记录量上限

- **现状**：`AnalyzeSessions` 常驻全部记录关键信息 + 原始耗时/延迟/Token 样本切片（算真实百分位）。实测万级记录即 GB 级 RSS（`report` 单跑约 1.4GB / 1.1 万条；`analyze` 组合路径约 3.75GB / 1.5 万条）。
- **journey 半边曾是更大的来源，已消除**：`Step` 不再持有 `audit.Record`，`-benchmark`/`-render-all` 按 `Manifest.Bytes` 字节预算分批构建，全部 Journey 常驻只剩约 300MB，与语料量解耦。
- **剩下的**：report 半边的 `AnalyzeSessions` 样本切片仍是全内存。
- **可能方案**：按审计日志的时间局部性分自然日分桶，跨日即时释放原始切片。
- **为什么仍待定**：这个量级目前仍跑得完（16GB 机器有余量），且分桶释放依赖「记录时间严格单调递增」这个隐蔽正确性前提，不成立就是静默算错而非报错——押上方案前必须先把它证实或证伪。
- **触发线是「该动手了」不是「已经坏了」**：单次 `report`/`analyze` 宏观半边语料 > 约 3 万条、或该半边峰值 RSS > 4GB 是不可越过的上限；实际立项要往回留约两成提前量（约 2.5 万条 / 3.3GB 起就排期），让「证前提 → 实现 → 补一套对等的 cold/warm 一致性测试」在撞墙前落地——撞墙时正是最缺内存、最需要跑分析的那一刻。
- **相关未做项（warm-path，登记待触发）**：语料不变、只渲染单个 journey 时，`setupJourneyRun` 仍无条件全量 `ScanCached` + `buildGraph` + `StitchGraph`。窄路径需给 `journeys/index.json` 的 `JourneyIndexRow` 补 `stitch_edges`（每条 lineage 的前驱边持久化，按内容寻址 `LineageID` 重放，避开 tie-break 不确定性）+ 一条陈旧性闸。触发条件同上。

#### 2.1 [低，已部分闭环] 会话分析那一趟（`collect()`）仍未缓存

- **现状**：`report.Build`/`BuildCached` 跑三趟扫描——① `ctxgraph.ScanCached`（manifest，已缓存）② `collect()`（会话/任务分组用的每记录特征）**未缓存，每次全量重跑** ③ `aggState.scanFiles`（指标聚合，已接 `factscache.go` 缓存）。③ 接缓存后真实语料实测热耗时 5.2×，但 ② 未缓存使热耗时离个位数秒仍有差距。
- **为什么不顺手做**：`collect()` 产出（`ReqInfo`）直接喂 `group()`/`ctxgraph.StitchGraph` 做会话/任务边界判定，正确性敏感度高于纯指标聚合（算错是把不相关对话缝到同一 Journey）。
- **触发条件**：投入前先补一套对等的 cold/warm 一致性测试（参照 `TestBuildCached_WarmMatchesBuild`）。

#### 2.55 [低，登记待触发] `journey.BuildAll` 仍先把一批的全部记录物化成 map

- **现状**：`BuildAll` 内部 `FetchRecords` 把一批（字节预算 ~160MiB 原始）的记录一次性收进 `map[Loc]*audit.Record` 再 `buildFrom`，这个 map 是每批的瞬时峰值来源（约几百 MB）。
- **可能方案**：全流式——`FetchRecords` 的 map 也不要，逐条喂给 builder。要求把 `buildFrom` 从「拿到全部记录后遍历」改成「按到达顺序 feed」，并处理并发扫文件的乱序（Event 的 `FirstStepSeq` 需按 seq 事后归并）。
- **触发条件**：语料再涨约 5 倍，或需要在 8GB 以下机器上跑全量 `-benchmark`/`-render-all`。

#### 2.56 [低，登记待触发] 一次 `vmr analyze` 至少把全量语料解压三遍

- **现状**：`.cache/parse` 只覆盖 `ctxgraph` 的 manifest 扫描。`PreviewTitles`（全部候选根记录）、每批 `BuildAll` 的 `FetchRecords`、report 半边的 `analyzeFile`（§2.1）各自独立全量解压一遍——全量语料上是 `-render-all` 耗时的主要来源。产物级 L2 缓存落地后，输入未变时这套重复解压整体被跳过（`-no-cache` 可退回全量对照）；本条针对的仍是冷启动/输入变化后的那一次全量计算。
- **相关**：`FetchRecords` 的接口形状（返回全量 map）天然逼调用方驻留全部；`PreviewTitles` 是纯提取（读一条、取一句标题、丢弃），可顺手切 `ctxgraph.ForEachRecord`，消掉一个数百 MB 的瞬时峰值。
- **可能方案（治本）**：让 `Manifest` 携带每步 delta 正文，取消 `FetchRecords` 这第二遍解压。但要把叙事提取逻辑从 `journey` 挪进 `ctxgraph`，破坏后者「不驻留正文」的契约，解析缓存体积涨两个数量级，且该 cache 是 report 半区共享的。跨包契约 + 双半边影响。
- **触发条件**：内存不再是瓶颈后，时间成为首要痛点时单独立项。

#### 2.96 [低，登记待办] 单请求详情/证据链过度延迟物化，批量回溯场景下引发 I/O 抖动

- **现状**：`requests/details/` 与 `requests/evidence/` 严格懒物化。静态 HTTP 服务器走查或批量下载多个单请求分析时，后台需即时解压审计日志定位单行，引发磁盘随机寻道与 CPU 尖峰。
- **根因**：审计日志按块压缩存放（`.jsonl.zst`），随机定位一个请求需解压整个压缩块；连续请求多个详情时解压重复执行，无块级复用。
- **可能方案**：保持「全量分析不产生海量微小 Markdown」的前提下，在 `requests/index.json` 增加每请求在压缩日志中的字节偏移与块 ID（schema 加性变更），并在按需物化模块引入容量有限的最近解压块 LRU 缓存（如 10 个 block）。
- **触发条件**：作为按需物化性能专项排期处理；在那之前单请求详情的数百毫秒级响应在单人本地场景可接受。

#### 2.69 [低，登记待触发] `searchableTranscript` 大语料下 O(N²) 全量物化

- **现状**：`internal/journey/llm_findings.go` 的 `searchableTranscript` 为每次锚点校验把 Journey 的转录本整体拼接成字符串。校验次数 × 转录本长度是乘积关系，大语料下是分析半区唯一的复杂度悬崖。
- **可能方案**：校验改在已分片文本上逐段 `Contains`（锚点语义不变），或对超长 Journey 截断校验域并明示。
- **触发条件**：`vmr analyze -llm` 在真实大语料上出现可感知的耗时占比（当前无实测瓶颈）。

#### 2.50 [低，潜在] 详单文件名去重位 `md5(basename:line)[:4]`（32 bit）

- **现状**：`internal/ctxgraph/reqcoord.go` 的 `ReqHash8` 给详单文件名算 4 字节 hash 去重后缀。单源文件近 1 万条记录时按生日界碰撞概率约 1%；真正撞成同一文件名还需同毫秒 + 同模型 + 同 outcome，现实可忽略。
- **恶化曲线**：与 §2.2 的语料上限同步线性恶化。真出现时把去重位提到 hash12/16 或改用递增序号，都是局部改动。

#### 2.3 [低，决定不做] `chatmsg` 离线解析路径的 `map[string]any` 分配

- **现状**：`internal/chatmsg` 的 `map[string]any` 全在离线消息/SSE/usage 解析路径，转发热路径实测零命中。
- **决定不做**：真实语料内存分析直接测了这一层——`audit.Record` 反序列化后的 live heap 相对原始 JSON 字节只放大 **1.40x**（审计记录绝大部分是长文本对话正文，`string` 只有 16 字节 header，结构开销被文本稀释）。把 `Body` 从 `any` 改成 `json.RawMessage` 延迟解析最多省 29%，不改变量级，却要改动 `journey`/`report`/`reqdetail`/`chatmsg` 里几十处类型断言——投入产出比最差。journey 半边的内存问题另有真因（§2.2），已单独解决。
- **触发条件**：真实 profile 显示某个离线聚合路径的时间/内存确由 `map[string]any` 分配主导（当前证据相反）。

### B. 分析半区 · 指标与口径正确性

#### 2.100 [中，登记待做] `internal/report` 未消费 `Attempt.tokens` 盖章值，与 `/stats` 的 token 双路径

- **现状**：LiveStats 前置改动把每个 forwarded attempt 的 raw 四分量 token 盖到
  `audit.Attempt.tokens`（`internal/router/quota.go:tokenStamp`，与 quota 扣费同源），目的是
  "routing half 能盖戳就不让下游反推"。`internal/livestats` 的 `/stats` 已消费它；但
  `internal/report`（`session.go` 的 `chatmsg.ExtractUsageSides(resp.Body, …)`）**仍从响应体
  反解析** token。字段是加性的，report 不改也编译。
- **双路径分叉风险**：同一条 audit record，`/stats` 的 `by_provider_model` token 走
  `Attempt.tokens`（quota 的 exact/degraded fold），`vmr analyze` 的按端点 token 走 body
  反解析——两条路径对同一请求可能不一致：① degraded 场景（流在 usage 块前截断）两侧的估算
  口径不同；② `Attempt.tokens.in` 是 fresh（净 cache），`chatmsg.Usage.In` 是 gross。运维
  对不上账。
- **可能方案**：`report` 改为优先读 `Attempt.tokens`（缺失时 fallback 到
  `ExtractUsageSides`，仿 `Attempt.IsForwarded` 的 stamped-优先-heuristic-兜底），并补
  livestats-token vs report-token 的差分测试。
- **为什么还没做**：跨 viewmodel 层、golden fixture 会变，有独立的测试成本；不阻塞现状
  （两条路径各自有测试、各自能跑）。
- **触发条件**：有分析半区改动窗口时一起做；或运维实际报出 `/stats` 与 `vmr analyze` 的
  token 对不上。

#### 2.57 [低] `computeTimeSplit` 单间隙时间归因无上限，污染 benchmark 均值

- **现状**：`internal/journey/metrics.go` 的 `computeTimeSplit` 对每对相邻 Step，把「上一步响应落地 → 下一步请求到达」的整段 wall-clock 间隙按「下一步是否 `HumanInitiated`」二分为 human idle 或 `AgentExecMS`，间隙不设上限。跨天/跨周的 lineage 上，一段几十天的空档会整段计入「Agent 执行时间」——`-benchmark` 的 `Agent-Side Execution` 因此出现 `Median 8s / Mean 数小时` 乃至数十天量级的均值。
- **当前缓解**：`-benchmark` 指标分布表已加脚注「time 类指标的 Mean 被少数长命 journey 严重拉偏，看 Median/P90」。只是免责，没动根因。
- **可能方案**：对单间隙设上限（如 > 1h 归 idle/unknown 而非 agent 执行）。需改指标语义 + 更新 Analytics 设计文档的时间拆分定义 + 差分测试。
- **触发条件**：脚注被证明不够（读者仍据 Mean 下结论），或要把 `NetWorkingMS` / `ModelToToolRatio` 当硬指标用。

#### 2.58 [低] 定价覆盖与溯源的四个已知边界

报表成本章四张表的四个已知口径边界，同源、同一批做才划算——都需要把解析结果的元信息从 `pricing.Resolve` 一路穿到 report 的行结构。

- **(a) 定价表覆盖不到的模型，其成本永远不进任何合计**：合计只含解析出费率的行，表下注明「合计不含 N/M 天（个模型/端点/客户端）」；未定价行仍渲染、成本列写 `-`（不是 0，也不是整行消失）。这是数据缺口不是呈现缺口——三张表都查不到的模型，其流量成本就是未知。**缓解**：厂商优先级消歧 + curated 别名把标准表覆盖面拉满；带 org/路径前缀的聚合商模型名经 `pricing.ModelBasename` 兜底与裸名同解析（见 §1.2）；剩余缺口由用户在对应 provider 的 `pricing.rates`/`pricing.aliases` 自补，或贡献进 `standard_price_curated.yaml`。`vmr check` 在表龄超 60 天时提示刷新。**框架只保证查得到就用得上、查不到就说不知道**，无代码方案。
- **(b) 费率缺分量时按 0 计价，只在汇总层披露，不逐行标注**：`pricing.Rate.Cost` 把 nil 分量按 0 计价（防御性下限），`EndpointRow.CostRateIncomplete` + `IncompleteRateNote` 汇总提示「有 N 个端点的单价缺分量」，但具体哪几行、缺哪一项、少算多少，行上看不出。**触发条件**：主力模型的厂商长期不公布缓存价，而账号缓存命中率又高。
- **(c) 费率溯源只到聚合级，单行看不出走的是哪一层**：`report.Pricing` 摘要只给「本次用了哪些定价来源」的总数；单行 `$` 看不出它走的是标准表、账号覆盖，还是**厂商优先级替代**（一个转售 provider 用了第一方刊例价）。**缓解**：该章免责声明写明整章是「按量计费等价成本、按第一方刊例价」；`vmr check` 的 `pricing_table` 行显示别名条数。**触发条件**：读者需要逐行判断某个金额可不可信。
- **(d) 按客户端表的合计略低于其它三张表**：按日期/模型/端点三张覆盖全部记录，按客户端那张只覆盖解析出 `client_key` 的记录（auth 关闭或没匹配上任何 key 时为空），这些记录压根不成行。实测差额在 0.002% 量级；四个合计都是各自表内行的诚实求和，没有哪个假装是「全局总额」。**可能方案**：加一行 `(no client_key)`（更好，但要动 `ByClient` 的分桶键语义），或表下注明。**触发条件**：读者拿按客户端合计和总额对账，发现对不上。

#### 2.64 [低] 「上下文有效利用率」在语料级呈现双峰退化

- **现状**：`internal/journey/metrics.go` 计算的 Context Utilization 在实际语料中高度双峰退化：约两成样本值为 0（无工具调用或单轮任务），约三成为 1.0（全工具结果均被后续轮次引用），中间值稀疏。导致「均值 70% / 中位数 95%」缺乏统计区分度，看板的「100%」亦难提供有效洞察。
- **当前缓解**：`-benchmark` 统计时需结合分布形状（P10/P50/P90 及两端样本数）共同解读；暂不重定义指标语义以维护 v1-complete 稳定性。
- **可能方案**：细化有效引用粒度（如按实体引用率加权或按 token 深度衰减）或按任务类别（含/不含工具调用）分桶展示。
- **触发条件**：后续版本计划重构行为指标语义时统一评估。

#### 2.67 [低] Anthropic 侧的 usage 侧别判定对不吐 `message_start` 类型标记的兼容网关 fail-open

- **现状**：`respnorm` 按 SSE 事件分别记录 in/out 两侧的 usage 是否见过（截断流不再把占位 `output_tokens≈1` 当精确值计费）。侧别判定依赖事件里的 `"type":"message_start"` 标记——含该标记的事件只记 in 侧。
- **边界**：不吐这个标记的 anthropic 兼容网关退回通用判定（占位 Out=1 记为 out 侧见过），即该形态下回到修复前的行为。刻意 fail-open：宁可退回旧行为，也不因为识别不出网关方言就把整条流判成无 usage。

#### 2.68 [低，登记待触发] crosscheck 夹具没有 body-sniffed 的 compaction 记录

- **现状**：`cmd/vmr/cmd_journey_report_crosscheck_test.go` 的夹具里没有 summarization（compaction）请求，因此「report 与 journey 对同一条 compaction 记录渲染逐字节相同的 detail 页」在该端到端测试里没有直擦形态的覆盖——实际由 `internal/report/session_compaction_manifest_test.go` 加指纹机制（`renderFingerprint` 折入 m/prev 身份）间接保证。
- **触发条件**：语料出现真实的 compaction 记录后，往 crosscheck 夹具补一条 body-sniffed summarization 记录。在那之前不构成已知失真——两条直接测试已钉住机制本身。

#### 2.70 [低，登记待触发] `buildRec2` 的 (path, line) join 依赖审计日志追加不变性

- **现状**：`internal/report/recextract.go` 把解析缓存的 `recordFacts` 与会话分析的 `ReqInfo` 按 (path, line) 配对。审计日志是追加型的（压缩轮转生成新 path、哈希变化自然 miss），常规运维下两侧永不错位；但手工编辑/拼接历史日志会让同一 (path, line) 指向不同记录，配错完全静默。
- **可能方案**：join 前对 `rf.TS` 与 `ri.TS` 做阈值校验（如差 > 1s 记 warning 并跳过 join）。
- **触发条件**：出现「对已归档日志做手工编辑」的运维形态，或用户报告无法解释的指标错乱。在那之前这是加固项，不是缺陷。

### C. 分析半区 · LLM 解读层校准

#### 2.18 [中] 六个 LLM 语义判别器尚未完成完整黄金样本校准

- **现状**：`internal/journey/llm_findings.go` 六个判别器已实现、单测覆盖、且用 `_eval/calibrate_p1b.go` 对真实生产日志跑过真实模型验证（6 个真实 Journey 上机械核验 Evidence Anchor 有效率 100%，人工抽查合理）。但不是正式合入门禁——那需 30~50 个 Journey、每模块 ≥6 正/负例的系统性黄金样本集 + 人工标注 Ground Truth 算真实 Precision/Recall。
- **为什么待定**：黄金样本挑选与人工标注是需实际投入时间的判断性工作，无法自动化；当前抽样规模下无需立即处理的误报模式，不构成阻塞。`_eval/calibrate_p1b.go` 已是可直接复用的校准工具，扩大 `-input`/`-limit` 即可推进——**成本在人力时间，不在代码**。

### D. 分析半区 · 展示与产出契约

#### 2.59 [低] `vmr analyze -compare` 两侧 system prompt / 初始指令逐字一致时未合并

- **现状**：`internal/journey/render_compare.go` 的 `renderSysPrompt` / `renderInitialInstruction` 无条件各渲 A、B 两份节选。两侧同源（`Changes` 均为 0、节选逐字相同）时，同一段 system prompt 正文在 compare 产物里贴两遍，实测占单份 compare 全文约 65%。
- **可能方案**：只做精确相等合并（`sp.A.Excerpt == sp.B.Excerpt` / `f.A.Text == f.B.Text`）——渲一份，标注「两侧此节选一致（截断前缀，不代表完整文本逐字相同）」，A/B 的 tokens+Changes 对比行保留。相似度阈值合并不做（阈值主观）。
- **触发条件**：界限清楚、随时可做；改动会给两个函数各加一个分支，注意 `archtest` per-function 行预算。

#### 2.6 [低] 报表账户消耗表的标记符号已达四个

- **现状**：`⭐` 超额度 / `‡` 配置变更 / `†` 无时间交集 / `◇` 部分流量未计价，各配一条按需渲染脚注。信息都必要，但四个符号叠一张表可能已到「标记多到没人看脚注」的临界。
- **为什么待定**：主观展示密度判断，四个标记都按需渲染，健康报表一个都不出现。真实报表读起来觉得吵了再动（`◇` 是最可能降级为纯 JSON 字段的候选）。

#### 2.93 [低] 跨运行累积产物的语言混排（render-only 重渲染时）

- **现状**：`compares/*.json`、`journeys/details/j-<id>.json` 是跨调用累积的产物，各自携带生成时的语言；`-render-only`（及全量运行的 renderAllFromDisk）统一以 manifest.lang 重渲染 Markdown，且保留旧文件里的 `## LLM ` 段（旧语言）。同一输出目录换过语言并累积过产物时，重渲染结果可能中英混排。
- **为什么待定**：「渲染继承 JSON 语言」的规则里，累积产物的「JSON 语言」不是一个值；逐文件采用各自 JSON 的 lang 字段需要 compare JSON 增加语言字段（schema 加性变更），且触发条件苛刻（同目录换语言 + 有跨语言累积）。
- **触发条件**：出现真实的双语交替使用场景，或用户报告混排造成误读；在那之前「换语言请全量重跑并清理输出目录」是够用的指引。

#### 2.94 [低，登记待办] 看板 `wireHashReload` 用 `location.reload()` 解决路由刷新

- **现状**：`journey-viewer.html` / `journey-compare.html` 内用户在同页切换 `#data=` 链接（如候选列表点开某个任务）时，`common.js` 的 `wireHashReload` 监听 `hashchange` 后直接 `location.reload()` 整页重载——功能可用，但销毁了滚动位置、筛选器状态与展开状态。
- **根因**：骨架页早期是一次性 IIFE 绑定生命周期，没有组件化「数据拉取 → DOM 局部清空与重绘」的函数。
- **可能方案**：把各页数据加载与渲染封装为显式的无状态渲染函数（如 `renderJourneyViewer(data)`），`hashchange` 时仅局部 fetch + 内存内替换 DOM 节点。需重构两到三个页面的生命周期。
- **触发条件**：作为看板体验专项排期处理；在那之前 reload 行为可接受。

#### 2.95 [低，登记待办] 看板 chrome 的中英双语化

- **现状**：`-lang zh` 产出的 Markdown 全中文，但同目录下看板骨架页的导航、表头、按钮、图例固定英文。裁决理由见 §1.5「看板骨架页的 chrome 是英文单版」——**这不是 bug**，本条只是把改造登记为排期待办。
- **可能方案**：`common.js` 内置轻量中英词典（数十词条），骨架页写入时按产物语言注入 `window.__LANG`，`initDashboard()` 阶段对带 `data-i18n` 属性的静态文本节点做替换；无需多套 HTML。约 1 人天。

#### 2.73 [低-中，暂不做] LLM 自由文本的 `<`/`>` 未净化即进 `.md` 产物

- **现状**：`sanitizeMDStruct`（`internal/journey/llm.go`）只处理 Markdown **结构**破坏（反引号/竖线/行首标记），不处理 `<`/`>`。LLM 判别器输出的类 HTML 片段会原样进入 `.md` 文件。
- **为什么暂不做**：`.md` 产物没有 HTML 渲染面（看板骨架页消费的是 JSON 切片，不渲染 Markdown），Markdown 阅读器对裸 `<...>` 的降级仅是显示瑕疵。
- **触发条件**：产物开始被 web 化渲染，或出现把 `.md` 直接转 HTML 的新消费方——届时在转换层做 HTML 转义，而不是提前在数据层碰文本。

#### 2.7 [低] 报表成本表结构化透传 `CostEstimateEst`

- **现状**：Markdown 口径提示脚注已闭环。进一步的结构化透传要给 `Row`/`ClientRow` 补 `CostEstimateEst`、改 `rows.go`/`accumulateCost`/渲染层三处，并再次改 macro 切片的形状。
- **为什么待定**：无明确外部程序消费需求前遵循 YAGNI。

#### 2.22 [低，决定不做] `chatmsg.ToolResultList`/`ToolCallList` 未覆盖 Responses API 的 `function_call` 形状

- **现状**：`chatmsg.Messages` 已能把 `function_call_output` 渲染成人读文本，但结构化提取层只覆盖 OpenAI Chat Completions 与 Anthropic 两种形状。纯 Responses API 流量下脊柱不展示工具结果、三个 Finding 检测器无证据、`j-<id>.json` 的 `tool_calls` 会静默报告「这一步没有工具调用」。
- **决定不做**：真实语料按 `protocol` 统计 `openai-responses` **0 条 / 0.0%**——一次都没触发过。**触发条件（量化）**：任意一次 `vmr analyze` 的 `requests/index.json` 出现 `protocol == "openai-responses"` 的记录，即重新排期。

### E. 路由半区 · 配额与请求路径

#### 2.17 [中] `imgprep` 解码闸门按「防炸弹」设定，其内存上界与单请求内存预算差一个数量级

- **现状**：`processImage` 在 `image.Decode` 前用 `image.DecodeConfig` 只读头取宽高，声明尺寸 > `maxDecodePixels`（代码值 16MP）直接放弃降采样原样透传。**闸门存在且工作正常**，目的是拦解压炸弹。问题在阈值量纲：16MP 按 RGBA 约 64MB/次解码，而 UserGuide「单请求内存预算」核算的是 ~32MB/请求——两个数字各自都对，回答的不是同一个问题（「多大算恶意」vs「一个请求该占多少」）。图片逐张解码逐张释放，多图不累加。
- **可能方案**：为内存预算再设一道更低的、可配置的闸门。
- **为什么待定**：够到闸门需刻意构造，正常截图/照片低一到两个数量级，无实测显示真实负载下造成过内存问题；且方案自带「用账单换内存」的取舍，不能替用户默认决定。零风险的一半（UserGuide/.zh + `config.example` 注释写明这段峰值由像素数决定、逐张释放）已落地。

#### 2.52 [低] 虚拟模型级预算硬闸未做

- **现状**：quota 的 gate/bucket 是「配速」——从不拒绝请求，只在同优先级梯队内重排端点。硬闸要的是**硬急停**：进死循环的 agent 触顶后请求被**明确拒绝**（可解析错误，绝不静默降级到便宜模型），每日零点 + 进程重启重置、不引入持久化。两者目标不同——配速降低「跑爆某套餐」概率，硬闸给「一夜烧光」设确定性上限。
- **为什么待定**：用户 hold。真要做需一个独立的内存态机制（仿 `health.Registry`，请求入口查一次），不是拧 quota 旋钮能得到的。

#### 2.85 [低] 半开恢复的深度退避解除策略：多候选同时半开的场景未覆盖

- **已落地的部分**：`buildCandidates`（经 `healthFilter`，`internal/router/candidates.go`）带 last-resort——候选全空（`healthOK` 为空）且存在至少一个半开（cooldown 已过期、仅 `fails>0`）端点时，释放其中退避最浅的一个（`ReportNeutral`，终态释放 `Classify` 刚占的 single-flight 名额，不动 `fails`/cooldown）作为本轮真实候选，走和普通端点完全相同的 `tryOne`/`Acquire`/`ReportSuccess` 路径——真实成功直接清零 `fails`，不再需要多轮探针衰减。同一轮里其余半开端点不受影响，仍正常派后台探针。与 `ctxFallback`（「估计值不该清空非空候选集，交给一次真实尝试去判断」）是同一条设计原则在健康过滤上的延伸，`X-VMR-Route-Reason` 新增 `health_fallback=1` 标记可观测。
- **边界（有意的取舍，非缺陷）**：若被释放的端点其实仍未恢复，这次真实请求要等到 `response_header` 超时（默认 120s，可配）才失败，而不是秒回 503——因为这条路径复用的是真实流量的 upstream client，不是 `timeouts.probe` 那条专门收窄过的探针路径。只在「反正所有候选都会 503」的极端场景触发，不影响任何本来能成功的请求；`internal/server/active_probe_test.go` 的 `TestActiveProbe_HalfOpenEndpointServedAsLastResort` 钉死这个边界。
- **残留场景（决定不做）**：多候选同时半开时 last-resort 只释放退避最浅的一个；若它不巧仍未恢复，本轮吃满 `response_header` 超时才失败，不会转去试另一个可能已恢复的半开端点。曾提案「探针成功 1 次即放该端点回常规路由（保留 `fails` 深度，后续真实请求失败则在原深度继续退避、成功则清零）」让这些端点提前备好。不做的三个理由：① 直接违背 §1.1 已钉死的「探针成功只做衰减（`fails--`），真实流量成功才清零」——探针是 `max_tokens=300` 的小请求，用它发常规路由资格正是 429→冷却→探针成功→满额流量→429 循环的成因；② 要横跨多个请求累积信任、自带真实 flap 风险，需要独立设计草案 + 回摆回归测试；③ 只在「反正本轮所有候选都会失败」的极端场景才有差别，不拖慢任何本可成功的请求。**触发条件（触发即重估）**：真实生产报告显示这个二阶场景确实把本可成功的请求拖垮了（多候选半开 + 释放的那个未恢复 + 等满超时），而非仅理论存在。

#### 2.86 [低，需先设计] `respnorm` 初始 `modeUndecided` 扣留保活帧，慢/排队上游下客户端可能读超时

- **现状**：SSE 流初始处于 `modeUndecided`（为侦测 MiniMax 的 inline-think 形态），首个「payload-bearing」事件（`content`/`text` 非 `<think>` 开头，或 `tool_calls`/`partial_json`，或专用 `reasoning_content`/`thinking` 字段）到达前，所有事件——含 `message_start`、role marker、`event: ping` 保活帧——全部囤在 `s.pending`。`bufferedCap` 几十秒也到不了（ping ~30B/个）。
- **影响面**：常见情况首个 content delta 在 role 后几十 ms 到达、扣留可忽略；Anthropic extended thinking 的 `thinking_delta` 立即放行。**窄场景**：上游排队 / 慢推理模型在 `message_start` 后长时间只发 ping、无任何 content/thinking/tool delta，超过客户端读超时。
- **可能方案**（触及核心四态机，需评估与 buffered/opaque/截断三分支的交互 + 完整回归）：在 `decide()` / `ingest` 的 undecided/buffered 路径识别保活帧（`event: ping`、SSE 注释行 `:`）并立即旁路 `s.out`，不参与 decide（ping 无 content、无 model 字段，早发对 SSE 语义无害，与 MiniMax buffered 不共存）。
- **触发条件**：真实用户报告「用某慢上游时流式响应假死后被客户端超时切断」。

#### 2.99 [低，登记待评估] 灰区振荡：探针放行把「慢而未死」端点送回常规池首选位，新流量反复撞满超时

- **现状与根因**：恢复探针（`max_tokens=300` 回显请求）对「慢/过载但未死」上游的通过率系统性高于真实大请求，连续探针成功把 `fails` 衰减到 0 后端点回到常规池的**配置原优先级**。对灰区上游构成池级振荡：新流量（无 sticky 指针者按配置序路由）撞满 `response_header` 超时 → failover 到别的候选成功且 sticky 随之迁移（同会话不再回撞）→ A 冷却到期探针过 → 回池原位 → 下一个新会话又先撞 A。探针撞墙最多烧 `timeouts.probe`（15s），真实流量撞墙烧满 `response_header`（默认 120s）——代价在后者。
- **已落地的缓解（治标）**：transient 首档 2s→5s（2026-08，`internal/health/health.go`），振荡频率压到 1/2.5，不改变循环结构。
- **候选方案（治本，改动接近参数级）**：探针只衰减到 `fails==1`（`ReportProbeSuccess` 改 `if s.fails > 1 { s.fails-- }`），`Classify` 对 `fails==1` 且冷却已过的端点直接放行真实请求——该请求即终审：成功清零回池，失败 `fails=2` 进更深冷却。效果：灰区端点永不回到常规池首选位，终审失败逐轮加深冷却，单位时间烧掉的用户超时严格更少；同时让 §1.1 的「没有真实成功就永不归零」从交替语境保证升级为字面保证（当前纯探针序列可衰减到 0，`ReportProbeSuccess` 包注释与之一致）。Envoy outlier detection 的「弹出到期由真实流量终审」是同一形态。
- **代价与需要改的不变式**：①「真实流量永不接触半开端点」缩窄为「永不接触 `fails>1` 的半开」，`internal/server/active_probe_test.go`、`internal/health/health_test.go` 一批断言与 `ReportProbeSuccess` 包注释要同步改写；②终审请求吃满 `response_header` 超时从 last-resort 专属形态变为深度 1 放行的常规行为，需接受或为终审尝试设收窄的 header 预算（独立决策点）；③单端点配置下深度 1 放行即 last-resort，行为不变。与 §2.85 曾否决的「探针成功 1 次即放行」提案的区别：放行请求本身定义为终审（失败加深、成功清零），且只在深度 1 触发——探针仍承担 1→N 的全部验证工作，不重复 §2.85 反对的「弱信号换满额路由资格」。
- **已否决的替代**：保真探针（按最近失败请求体量放大探针）——探针按 token 计费，每次恢复烧真实成本，`timeouts.probe` 预算也要跟着涨。间隔 soak（K 次成功且相邻间隔 ≥10s）只降概率不根除，留作方案 A 落地后仍有残留时的补充。按请求规模软降级（超时类失败不冷却、大请求降权小请求照常）是语义上更精确的长期模型，与 `ROADMAP` 的 Latency 维度同源，不在本条范围。
- **触发条件（触发即实施）**：先用首档 5s 观察——日志/审计显示同一端点在短间隔内规律性复现「失败→探针通过→再失败」（如同一端点数分钟内多次 `cooldown=` 记录、`X-VMR-Failover` 反复出现同一对端点），再决定是否按候选方案实施。

#### 2.48 [低] 错误分类词表的长期形态：端点级 quirk 统一模块未做

- **现状**：vendor 知识散在 `DefaultClassify` 的全局词表里（`contentHint`/`contextLimitHint`/`upstreamHint`/`vendorQuirkHint`/`authHint`）。已知厂商专属误判已由 `ErrQuirk` 类 + 词条修复覆盖（见 §1.1），词表之间尚未互相干扰。
- **可能方案（升级时直接可用）**：每 vendor 一个编译期注册的 quirk profile，按 **model glob** 匹配（不按 provider 名——用户自起名，改名即静默失效），字段含 marker 表 / 建议分类 / sticky 策略；`DefaultClassify` 保留为兜底。**附带**：quirk 命中时对 sticky 会话降级（清粘性/降权），消除中毒会话每轮 ~1–2s 的重复失败往返。
- **触发条件**：全局词表增长到出现互相干扰/误命中，或 sticky 重复往返在真实负载中可观测地拖慢中毒会话。

#### 2.14 [低] 滑动时间窗（Rolling Window）限流模型

- **现状**：`internal/quota/period.go` 是日历对齐的惰性周期重置，短 tumbling 窗（如 `every: 5h`）按周期近似。真正的滑动窗需要平滑计数器（Ring）。
- **性质**：功能演进，不是缺陷——当前近似对目标场景（月度/日度 token plan）够用；滚动窗类套餐（Claude Code Pro/Max）的瞬时拒绝由健康状态机的冷却/退避兜底。**除非实测到某厂商套餐的密集 429 冲击，否则不做**——不在 README / Strategy 里当卖点讲。

#### 2.10 [低] 审计落盘的 `write` syscall 在全局锁内

- **现状**：`audit.Logger.Write` 的 JSON 编码已用 `sync.Pool` 移到锁外，但写文件的系统调用仍在全局互斥锁内。
- **可能方案**：带缓冲通道 + 单独写协程。
- **为什么待定**：异步队列要处理背压（丢弃 vs 阻塞）与优雅关停等待；当前直接写入未构成瓶颈。

#### 2.9 [低] 探针请求绕过审计日志

- **现状**：`internal/router/probe.go` 的健康探活请求不写 `audit.Record`，`vmr analyze` 看不到探活消耗。
- **为什么待定**：探活消耗极低；且需先明确探针流量在报表中的呈现口径，避免污染业务 SLO 统计。

#### 2.74 [低] `attachmentSpans` 对大 body 重复扫描

- **现状**：`internal/server/facts.go` 的 `attachmentSpans` 每次调用线性扫描全 body；同一请求的 facts 提取路径上存在多次扫描的形态，大 body（多图/长文）下重复开销。
- **为什么待定**：这是性能项而非安全项——本地单用户运行，客户端即操作员；无超时风险敞口。
- **触发条件**：profile 显示 facts 提取在真实负载耗时中占比可感知。

#### 2.75 [低] 配置 hot-reload 在高频写入下可乱序

- **现状**：`internal/config/watch.go` 的 reload 管线在高频连续写入时，事件到达顺序不保证与写入顺序一致，存在短暂加载到「新产物与旧校验交错」的混合态窗口。
- **当前缓解**：触发面窄（需要亚秒级连续改写 config.yaml），且混合态每次都会重新走完整校验，不是「未校验状态上线」。
- **可能方案**：reload 合并与去抖（debounce）+ 序号丢弃过期事件。
- **触发条件**：出现外部自动化高频改写 config.yaml 的运维形态。

#### 2.77 [低，加固项] 评分层无 NaN 纵深防御

- **现状**：`quota` 评分路径的输入由加载期与写入期校验挡住（NaN/±Inf 进不来），评分本身无二次防御。当前不可达。
- **可能方案**：评分入口加 `math.IsNaN`/`IsInf` 兜底归零。
- **触发条件**：出现绕过既有校验层直接构造 Counters 的新调用方（如未来的导入/迁移工具）。

#### 2.91 [低，决定不做] `Registry.rollbackWarned` 是进程级一次性 latch

- **现状**：`internal/quota/quota.go` `resetIfStaleLocked` 的 `rollbackWarned` 一旦置位便无重置机会，此后宿主机真实 NTP 阶跃/快照回滚/时区误配都不再有 WARN。
- **裁决（不修）**：latch 只控制 WARN 是否打印，对计量行为**零影响**——回退期间计数照常累进且方向**保守**（used 偏高 → headroom 偏低 → provider 被轻微降权），并随下一次周期前移或进程重启**自愈**；残留代价仅是第二次真实回退不再有日志。cost 计费单锁原子化后，误触发的主要来源（周期切换瞬间晚到的估算写携旧 `periodStart`）已消除，真实回退本身罕见。为一条日志引入时间窗/limitKey 状态不值得。若日后真实回退频繁出现且确需逐次告警，再按窗口化去重改。

#### 2.49 [低，非活跃——仅 32-bit] `imgprep` 解压炸弹守卫的像素乘积在 32-bit `int` 平台可溢出

- **现状**：`processImage` 用 `cfg.Width*cfg.Height > maxDecodePixels` 挡炸弹（两值是 `int`）；32-bit 平台两值接近 `int32` 上限时乘积回绕成小值绕过守卫。
- **为什么非活跃**：Go `image/png` 把 IHDR 宽高钳在 `int32`，64-bit（唯一 CI/目标平台）乘积 ≤ (2³¹)² < `int64` 上限，不可能溢出。
- **修法（触发时）**：`int64(cfg.Width) * int64(cfg.Height)`，一行。**触发条件**：32-bit 成为受支持的构建/部署目标。

### F. 工程工具与运维入口

#### 2.97 [中，发版前处理] `CHANGELOG.md [Unreleased]` 需一次归整才能发版

- **现状**：`[Unreleased]` 下有重复的 `### Added` / `### Changed` / `### Fixed` 段（多轮 feature 分支各自 append 未合并），且 analyze 架构重构**之前**就在 `[Unreleased]` 里的条目通篇引用被本次同版删除的东西（退役的子命令、旧包名、旧产物文件名、`-corpus` flag）——发版体裁会同时说「删掉了 X」和「在 X 里修了 bug」。`release.yml` 逐字提取该段作 GitHub Release body。
- **为什么现在不动**：合并同类段是机械活但需逐条核对不漏；把「在已删命令里修 bug」类条目改写为现名 / 并进 breaking 条目需逐条判断十几个特性「重构后去哪了」，有编辑判断成分；且 CHANGELOG 是 trail，不做 patch-on-patch。
- **发版前必做**：并段成每类型一段（Keep a Changelog 顺序）；`[Unreleased]` 内容重读为「vN vs vN-1 的净变更」，退役术语条目改写或折叠。
- **触发条件**：准备打第一个含 analyze 重构的 tag。

#### 2.54 [低，暂不做] `/help.html` 页内发测试请求

- **现状**：`/help.html`（`internal/server` 里的双语内嵌页）已有一键复制各 Agent 接入片段、鉴权弹窗、`fetch('/status')` 健康展示。「页内直接发一次测试请求看命中节点/延迟」在技术上可行——`X-VMR-Endpoint`/`X-VMR-Attempts`/`X-VMR-Route-Reason` 响应头已具备。
- **为什么暂不做**：真实工作量约 1–1.5 人日（双语内嵌 HTML 锁步、真实计费请求、streaming、错误面、虚拟模型选择器、内嵌 JS 无 Go 测试）。这是 onboarding 漏斗功能，被 `vmr init`/`vmr connect` 完全压制——真做漏斗应先做那两个。Strategy 文档里本就列在第三梯队。

#### 2.80 [低] `sysinfo` 把系统调用失败折叠成 0，违反「missing is not zero」

- **现状**：`internal/sysinfo` 的 `DirTotalSize`/`DiskFreeBytes` 在目录不可读或调用失败时返回 0——与「磁盘真的满/目录真的为空」在返回值上不可区分，状态看板可能把「读不到」显示成「用量为零」。
- **为什么待定**：消费方是本地状态展示，错误折叠的误导面小；真正「missing is not zero」的纪律挂在会进报表与配额决策的数字上。
- **可能方案**：返回 `(value, ok)` 并让消费方显式展示 unknown。
- **触发条件**：状态看板数字开始参与任何自动决策（而不仅是人看）。

#### 2.101 [低，待观察] Overview 告警 pill 的 quota 阈值上线后看噪音再调

- **现状**：quota 告警阈值写死在 `server/alerts.go`（used ≥ 100% → error；≥ 90% → warning），是实施轮主控拍板值（contracts §2.1 / D4）。
- **为什么待定**：阈值本身没有权威来源（quota 评分曲线只有 headroom=1 一个语义分界），90% 是否过吵取决于真实用量曲线；先跑真实流量再定，不预调。
- **触发条件**：告警 pill 长期非零但无实际可操作事项（噪音），或濒临耗尽从未提前告警（漏报）。

#### 2.102 [低，待决] recent_errors 与 Log 页之间无进程内请求号关联

- **现状**：console-unification 设计 §8.4-d 提出 Recent Failures 条目保留进程内请求号、与 Log 页每行同号互跳；但 audit.Record 与 `/log` 行目前都不携带任何请求号，实施轮未为它加字段（加号牵动 audit 格式与日志消费者，越界）。
- **可能方案**：给 Record 加进程级 seq 并在 logfmt 前缀携带（成本：日志格式变更 + audit schema 演进 + report 兼容）；或接受无关联（Log 页有子串过滤可按时间窗人工对齐）。
- **为什么待定**：关联的实际排障收益未经验证，而格式变更成本确定。

#### 2.104 [低] 拓扑表 Headroom 列只反映 bucket limit，不反映更紧的 gate

- **现状**：`endpointHeadroom`（`server/alerts.go`）在账户无 limit 触顶时返回 **bucket limit** 的 headroom（经 `quota.BucketIndex` 选定），不取账户所有 applicable limit 里最小的那个。于是一个被近饱和的短周期 gate 限流的账户，端点行 Headroom 仍显示绿色的 bucket 值——比它实际的路由有效余量乐观。差分测试钉住的是"端点 headroom == 对应 QuotaStatus 行的 headroom（同源）"，没钉住"选的是哪一行"。
- **为什么可接受**：与 Quota Budgets 表一致（那张表也逐 limit 各显各的 headroom）；gate 的紧迫感设计上由 Quota 表的 Progress 红条承担（console-unification §8.5）。端点表没有 Progress 列是这条的短板。
- **可能方案**：改取 applicable limit 里 headroom 最小者；或端点表补一个迷你 gate 指示。
- **触发条件**：运维因端点 Headroom 显示健康而没预判到 gate 限流。

#### 2.105 [低] `/status` 告警 `ref`（`provider:key_label:model`）在同名 provider 跨协议组复用时不唯一

- **现状**：同一个 provider 名可以同时出现在 `providers.openai` 与 `providers.anthropic` 下（`core.Endpoint.HealthKey` 的文档明确支持）。这两个是不同端点、各有独立健康状态，但告警 `ref` 只有 `provider:key_label:model` 三段，会撞在一起；`endpointAlerts` 按 `ref+message` 去重、`statusAlerts` 末尾 `sort.SliceStable` 遇到 `(severity,kind,ref)` 全等时保留 map 迭代序 → 同 ref 的多条告警排序在两次请求间可能不稳定。拓扑表不受影响（它按虚拟模型分组，协议在组首行可见）。
- **可能方案**：`ref` 加协议前缀（`anthropic-messages:provider:key_label:model`），或去重键并入 `HealthKey`。
- **为什么待定**：需要这种少见配置 + 影响仅是告警列表排序抖动，非功能错误。

#### 2.98 [低，待触发] `archtest` 的 `funcLineExemptions` 以「文件:函数名」为键，同文件重名方法共用一条

- **现状**：同文件重名方法（如 `report/ingest.go` 的多个 `Ingest`）共用一条豁免。今天全部远低于默认限额，无影响；一旦为其一登记豁免，其余会一并放宽。
- **可能方案**：键改「文件:接收者类型.函数名」（`ast.FuncDecl.Recv` 已有类型信息）。
- **为什么待定**：需真的出现一个必须豁免的重名方法才有意义。

### H. 2026-09-11 全系统 Review 新增（来源：`PROJECT_REVIEW_REPORT_agent_2026-09-11.md` 阶段二/三，源码已核实）

> 本条组由全系统 Review 的 6 路 subagent 发现、主控源码交叉核实后登记。凡与既有 §2 条目重复的（如 §2.86 respnorm 保活帧、§2.57 computeTimeSplit、§2.69 searchableTranscript、§2.100 token 双路径、§2.77 NaN 防御、§2.49 imgprep 溢出）不再重复登记，仅复核一致性后维持原编号。
>
> **2026-09-11 复核后续**（详见 `PROJECT_REVIEW_REPORT_agent_2026-09-11.md` 附录 A）：11 项已修并从本清单移除（编号不再复用）——顺手批 `2.106`（/log 写超时）、`2.108`（zstd 限并发）、`2.112`（buildGraph tie-breaker）、`2.113`（明细索引原子落盘）、`2.117`（tailPrev 死字段）、`2.118`（replay 前导空白）、`2.123`（rt.ctx 同步）；批次 1 `2.107`（/reports 热重载联动）、`2.110`（tailSlack 修正）、`2.114`（LLM 锚点长度门槛）、`2.116`（linkCompactions needle 门槛）。`2.111` 复核结论改为**明确不补**（见其条目）。

#### 2.111 [低，潜在路径，决定不补] `taskseg.Generic` 缺失 Anthropic tool_result 过滤

- **现状**：`internal/taskseg/generic.go` 的 `RealUserText` 对非空文本直接返回 true；Anthropic 协议将 tool_result 置于 user 角色消息，通用 Profile 下工具轮次会被误切为新任务。
- **裁决（2026-09-11 复核，不补）**：**现有组装根（`resolveTaskProfile`/`report.Build`）一律硬编码 `OpenClawAware`，`Generic` 生产完全不可达**——给一条零执行可能的路径加防御代码 + 测试正是 YAGNI 反对的过度设计。且真到启用 Detect-based profile 调度那一步，必然要系统性重审 `Generic` 的全部启发式（不止 tool_result 一处），届时一并处理更合理。§1.3「尤其不做 `prof == nil` 就回退到 `Generic` 这类静默兜底」的裁决同源。
- **触发条件**：启用 Detect-based profile 调度（届时重审 `Generic` 全部启发式）。

#### 2.120 [低] `core.PricingSpec/Rate/PricingOverride` 职责漂移

- **现状**：实时路由已完全剔除定价，但定价契约仍滞留 `core` 叶子包（与 `internal/pricing.Rate` 双重定义并存），违反 core 包文档的“最小充分集”准入声明。
- **可能方案**：下沉到 `internal/pricing` 包，消除类型冗余。
- **ROI**：Return=core 准入纯度；Investment=跨包移动——架构演进期做，不单独立项。

#### 2.121 [低] `cmd_diff.go` 450+ 行领域逻辑滞留 CLI 组装根

- **现状**：diff 计算/渲染（`computeDiff`, `diffReport` 等）全部在 `cmd/vmr/cmd_diff.go`，并借用 `cmd_replay.go` 的私有函数 `loadAuditRecord`，违反“CLI 薄组装根”原则。
- **可能方案**：下沉到一个新的 `auditdiff` 叶子包，或并入 `internal/reqdetail`。
- **ROI**：Return=组装根纯净度 + 可测试性；Investment=移动重构。

#### 2.122 [低] `sticky` 满容量驱逐为持锁 O(N) 线性扫描

- **现状**：`internal/sticky/sticky.go` 满容量时在 `mu` 下 for-range 找最老条目（maxEntries=10000 时每次 O(10000)）。
- **可能方案**：map + doubly-linked list 实现 O(1) LRU。
- **触发条件**：并发活跃会话接近 maxEntries 时（当前 μs 级非瓶颈）。

#### 2.124 [低] `expandEnv` 在 YAML flow-style 集合语法下存在逗号注入残存边界

- **现状**：`internal/config/config.go` 注释已承认：`api_keys: [${VAR}]` 若环境变量含逗号可能分割出额外元素；生产与推荐配置一律使用 block 语法，当前拦截已满足安全边界。
- **可能方案**：YAML Node 树反序列化后再做标量展开。
- **ROI**：低——可暂不处理，登记防回归。


#### 2.125 [低，登记待触发] `dataFieldMarkers` 只匹配紧凑 JSON，pretty-printed 请求体的附件 payload 会被整段当文本估算

- **现状**：`internal/server/facts.go` 的四个 data-field marker（`"data":"` / `"file_data":"` / `"url":"data:` / `"image_url":"data:`）都假设冒号后无空格的紧凑序列化。pretty-printed 请求体（`"data": "<base64>"`）一个 marker 都匹配不上 → span 不建立 → 整段 base64 payload 落进 `estimateTextTokens` 按文本计权（400KB 图 ≈ 85K 幻影文本 token，比 §2.109 修掉的双重计费更差）。§2.109 修复的分型嗅探本身不受影响——紧凑 body 里 media_type 的值引号始终紧贴 `image/`。
- **为什么待定**：主流 SDK 一律发送紧凑 JSON（带内联附件还做 pretty-print 的形态至今为零）；marker 改为空白容忍匹配要动热路径扫描循环的字节匹配结构，复杂度不小。Span 建立失败的后果也只落在 degraded 扣费估算与 `WithinContext` 软重排，方向保守。
- **触发条件**：`vmr analyze` 的 `requests/index.json` 出现"请求体含缩进/换行的附件 payload"的记录（即真实流量中出现 pretty-printed 附件请求），或 profile 显示 facts 提取对真实负载失真。

---

## 3. 跨组排期结论

- **全局结论**：待办里没有「价值高、成本低、却一直没做」的异常。值得优先投入的集中在三类：大语料规模（§2.2 看触发、§2.1 已证 5.2×）、LLM 解读层校准（§2.18，成本在人工标注）、路由配额（§2.52，用户 hold）。分析半区的产品路线（新视图 / 导出 / 达成信号）已移入 `ROADMAP`，不在此清单排期。
- **2026-09-11 全系统 Review 剩余项（复核后）**：11 项已修（顺手 7 + 批次 1 的 4，见 H 组顶部说明与 `PROJECT_REVIEW_REPORT_agent_2026-09-11.md` 附录 A）。剩余排期——批次 2 已全部落地（§2.109 / §2.115 / §2.119 移除）；**批次 3（需设计 / 待触发）**：§2.86、§2.100、§2.57；**批次 4（架构演进期 / 待触发）**：§2.120 / §2.121 / §2.122 / §2.124。§2.111 复核后改为明确不补；§2.125 为修复 §2.109 时新发现的登记待触发项。
- **多数条目不是「不值得做」，是「收益未经测量」**：§2.2 / §2.3 / §2.7 / §2.10 / §2.17 的共同点是收益尚未实测——而先做优化再测量正是这个项目一贯拒绝的顺序；触发条件到了先测再说。
- **发版前必做**：§2.97（CHANGELOG `[Unreleased]` 归整）——唯一一条不等触发、按日程必须处理的。
- **立即可做**（界限清楚、随时可做）：§2.59 compare 同源节选合并。
- **触发即做**（成本主要等触发，触发条件写在条目里）：§2.2（上限 3 万条 / RSS 4GB，留两成提前量即约 2.5 万条 / 3.3GB 起排期）、§2.55（语料再涨约 5 倍）、§2.56（时间成首要痛点）、§2.48（词表互相干扰 / sticky 往返可观测）、§2.57（脚注不够用）、§2.58（主力上游长期无价）、§2.18（黄金样本窗口）、§2.99（灰区振荡在 5s 首档下仍规律复现，触发条件写在条目里）。
- **需要先设计**（价值高、易做错，禁止仓促）：§2.86 保活帧旁路。
- **明确不做**（各有量化触发条件，触发即重估）：§2.3 / §2.22 / §2.49 / §2.50 / §2.91。
