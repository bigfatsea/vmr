<!-- Ver 2026-08-31, by pi -->

# vmr — Known Issues（已知问题与架构取舍清单）

> **定位**：vmr 已知问题、待评估演进项与刻意架构取舍的**唯一权威、持续维护的当前状态清单**。
> 发现新问题先在这里查一遍，再决定它是不是新的。
>
> **维护原则**
> 1. **只记当前系统里还找得到的东西**：要么是待办（§2），要么是「看着像 bug、其实是有意为之」的取舍（§1）。**已修复的问题不进这里**——它已经不存在了，代码本身就是证明，留一条「已解决」记录毫无价值。那段历史在 `CHANGELOG.md` 和 git history 里。
> 2. **三类分区**：§1 确定不修（连同 First Principles 决策逻辑，避免被反复重新提出）｜§2 待定问题（有优化空间，等真实负载/触发条件；分组、组内按用户价值 × ROI 排序）｜§3 优先级总览（替代原 ROI 表，只给跨组排期结论）。
> 3. **每条都要能对源码核实**。核实不了的，说明已过期，删掉。
> 4. **散文可压缩、可重组**；§1/§2 的编号只是文内导航，不承诺跨文件稳定。源码注释不靠编号回指本文档——每条注释自带完整理由，本文档只在「这个取舍值得被独立追踪」时补一条 §1。

---



## 0. 当前状态

- **稳定性与安全性**：无凭证泄漏、并发竞态或服务阻断级别的缺陷；单机生产环境可稳定运行。`copyFlush` 异常路径下的 `respnorm` 查询方法全部互斥锁同步，`-race` 全绿并经端到端流式断开集成测试守护。
- **自动化基线**：`internal/archtest` 强制导入单向边界、文件/函数行数预算、文档引用完整性，全绿。`go test ./...` 全绿（`internal/...` 与 `cmd/vmr` 均含 `-race`）。
- **§2 分布**：高危 0；中危 4（`2.2`/`2.17`/`2.18`/`2.85`），其余均为低危。
- **2026-09-04 已闭环**（定价 / 计费 / 配额专题 review 落地）：`firstDeadOverride` 收窄为「只有显式规则终结匹配」（合法化「通配折扣在前 + 专属显式在后」，F1）；`Resolve`/`Resolver.RateFor` 对悬空折扣与全空费率返回「无费率」而非冒充 `$0.00`（F7，同时惠及 `vmr report` §2/§2.5 与 `vmr story`）；`parseRateRow` 拒绝四分量全空的费率行（N1）；`TokenCountersSides` 精确/降级折算口径下沉 `internal/quota`（纯标量入参），router/replay/report 共用一份，消灭跨隔离包手写复刻（F2）；cost 计费与其估算并进 `ChargeCost` 单锁原子写、`/status` 走 `Snapshot` 单锁读（F4）；`PeriodBounds` 一次 `findK` 取周期起止（F9）；`ScoreForLimits` 空 Limit 集返回中性 `1.0`（备注B）；配额耗尽 Finding / §2.5 子表格带 per-model 作用域（F8 / N6）；报表 skip 统计移入 `Report2`、进 JSON 契约、去包级全局（F3 / N7）。详见 `CHANGELOG` `[Unreleased]`。新增待定：§2.89 / §2.90 / §2.91。
- **2026-09-04 已闭环**：`ScoreForLimits` 闸归并二值化（原 §2.88，N3 裁决采纳「闸 = 带安全余量的厂商限流本地代理」语义：活着的闸不参与评分，烧断的闸归零沉底到窗口重置；旧 `min(1, raw)` 硬封顶把带闸账号的桶抢跑加分压死在 ≤1.0 的病灶随之消除）。详见 `CHANGELOG` `[Unreleased]`。
- **2026-09-03 已闭环**：一批三方 review 核实后的小修（错误分类补 `error.code`、config 加载期禁 provider 名冒号 / 校验 strategy、keyless 自建上游按地址分级、探针仅 2xx 扣配额、anthropic 损坏图片仍计入 `HasImage`、`.parse-cache` mtime 消歧、md/html 行为指标统一、LLM detector 并发限时、`metric:cost` estimated 口径分侧等），详见 `CHANGELOG` `[Unreleased]`。删除的 §2 条目：`2.65`/`2.71`/`2.72`/`2.76`/`2.78`/`2.81`/`2.82`/`2.83`/`2.84`。
- **2026-09（早于本轮）已闭环**：`standard_price_curated.yaml` 别名指向空 key 致 `LoadStandard` 失败（原 §2.87，commit `2e5d9cd` 恢复 `rates:` 段）。
- **2026-08 已闭环**：`vmr analyze -corpus` 全量语料约 43GB → 约 2.4GB（`story.Step` 不再持有 `audit.Record` + 字节预算分批），见 §2.2。

---



## 1. 刻意取舍，不是缺陷

> 以下基于项目核心哲学（KISS / YAGNI / 单二进制 / 零代码侵入）做出，已论证过，不需要重新论证。
> **推翻其中任何一条是允许的，但必须先知道自己在推翻它，并给出新的理由。**

### 1.0 永久不做（架构红线）

语义缓存（对确定性编程/Agent 任务是正确性隐患）｜MCP 网关与工具执行拦截（不在标准 LLM API 线路上）｜Web UI / 内嵌 DB / RBAC / 分布式 / 跨实例 quota｜协议互译 / bypass 模式｜`.so` 运行时插件（坚持编译期 blank-import 注册）｜让价目表进实时路由热路径｜通用 HTTP provider（映射 DSL）｜更多 LLM 检测器 / 对比维度 / corpus 维度（分析半区标 v1-complete，新增维度从默认冲动改为需理由的例外）。

### 1.1 运行时与并发

- **`health.Registry` 全局互斥锁不分片**：单机场景锁持有只是纳秒级 map 读写，分片增复杂度无吞吐收益。
- **`health.Registry.Available` 无生产调用方，但不是死代码**：它是唯一无副作用的路由资格查询（`Acquire` 会占用 half-open 探针名额），`health` 与 `router` 的测试断言端点状态都靠它——用 `Acquire` 去查会改变被断言的状态。「无生产调用方」不等于「可删」。
- **`fails` 的语义是「当前退避曲线下的连续失败深度」，跨曲线切换重置为 1**：`ErrAuth`/`ErrEndpoint` 走 10min 起的 long 曲线，其余走 2s 起的 transient 曲线——两条曲线基数差 300 倍，共用一个深度计数器会让一串 5xx（transient 才退到 32s）后的一次 401 直接顶到 1h 封顶。**适用于**：判断"这个端点在当前故障模式下连续失败了几次"。**不适用于**：当作"这个端点历史上一共失败了多少次"的累计量——它不是，也从不是。`internal/server/active_probe_test.go` 因此不能再用 `fails >= 2` 当"走的是 ReportFailure 不是 ReportNeutral"的代理判据，只有 `last_error` 能区分两者。
- **`ReleaseProbe` 与 `ReportNeutral` 行为相同但保留为两个方法**：前者是"名额先还、健康结论稍后再报"（`forwardSuccess` 在流真正跑完前用它），后者是"这次结果对健康没有信息量，到此为止"。合一会让 `forwardSuccess` 的调用点读起来像已经下了终局结论，而它恰恰还没有。
- **探针成功只做衰减（`fails--`），真实流量成功才清零**：探针是 `max_tokens=300` 的小请求，对限流/上下文受压端点的成功率系统性高于真实的 20 万 token 请求——用最容易通过的信号解除对最容易失败流量的保护，正是 429→2s 冷却→探针成功→满额流量→429 的循环成因。**已知均衡态**：探针成功与真实失败交替时深度在两档间振荡（如 2↔3），不是单调加深；钉死的保证是"没有真实成功就永不归零、永不回到最浅档"。
- **退避冷却带 ±10% 抖动，且抖动也作用于已封顶的值**：封顶端点整点齐射正是抖动要防的场景，因此结果可超名义 cap 至多 10%。**例外**：`Retry-After` 路径不抖——那是上游指定的节奏，不是我们的估计。
- **后台探针按 requests 口径计 1，对 token/cost 限额计 0**：探针消耗真实上游额度，`metric: requests` 的账号侧一定计数，本地账本不计就是系统性欠记。token/cost 侧不解析探针 usage（响应体有 `probeBodyCap` 封顶），计 0 是诚实下界而非精确值。
- **`log_dir` 在 Unix 上被 `flock` 独占，第二个指向同目录的实例拒绝启动**：两个进程对同一 JSONL 做 housekeeping 会把两股 zstd 流交错写进同一归档，`rename` 之后**不可恢复**；同根还有双进程 O_APPEND 行交错与 quota 双写覆盖。锁文件 `.vmr-audit.lock`（0600）成为 `log_dir` 的常驻文件，不参与压缩与保留。**不适用于 Windows**：那里没有 flock，`acquireDirLock` 是 no-op——唯一临时文件名仍保证归档不被交错写坏，但双进程的其余后果在 Windows 上依然可能发生。用 pidfile 替代会因崩溃残留把启动永久卡死，比问题本身更糟。
- **`HealthKey` 取 SHA-256 前 4 字节**：单实例端点规模下碰撞概率可忽略。
- **健康状态机的退避冷却参数硬编码**：坚持「零调参」，不暴露难以科学校准的旋钮。
- **`copyFlush` 的 goroutine + channel 流水线**：避免在底层连接层设全局 Deadline 破坏 TLS/Header 超时语义。
- **客户端取消时不停止计费**：上游已生成的 token 厂商照收，路由侧照收才与账单对齐；改成不计费会让 `vmr report` 系统性低估消耗。取消的**传播**（中止上游连接）已通过 `BuildRequest(r.Context(), …)` 自动完成；取消的**检测/归类**（`router` 标 attempt、`server` 标审计 `Outcome` 为 `canceled`，消除误计为成功）需 `copyFlush` select 一次 `ctx.Done()`。
- **`respnorm.Read` 等待更多字节时返回 `(0, nil)`**：唯一消费方 `copyFlush` 显式处理；改成内部阻塞循环会让 idle 看门狗失去以读取为粒度的心跳。
- **`respnorm` 的 usage sniffing 不外移为 `router` 侧装饰器**：装饰器要在转发热路径每 chunk 多付一次接口调用；当前实现搭 `ingest` 已有的 per-chunk 循环，零额外开销。理由在 `internal/respnorm` 包注释末尾。
- **`respnorm` 的观测标记 `crlf_framing_suspected` / `thinking_process_pattern_detected` 不删**：字节未改动，只往审计 `norm` 串加一个标记，看似无消费者——实则被 `internal/reqdetail` 详单页逐条叙述，`thinking_process_pattern_detected` 另进 `internal/report` 的 `diagnosticNormMarker` → `EndpointRow.NormCounts`，作为「剥离规则是否失效」的跨请求频率预警。这是在用的低成本预警，不是死代码。
- **`GET /health` 为存活探针而非就绪探针，永不因上游不可用返回非 200**：与上游健康绑定会让容器编排在所有供应商不可用时触发无休止重启，放大雪崩。需要就绪度的调用方消费 `/status` 的模型健康块。
- **`/status` 的 `traffic.requests` 含未鉴权/被拒请求**：口径是「进程见过多少 HTTP 请求」，不是「成功路由了多少」。精确语义在审计日志。
- **`traffic.by_status` 按请求（非 attempt）计数，且不保证 `ok+canceled+error == total`**：failover 中间 attempt 不计入，少数早断路径不记或记入 error。展示语义，非路由语义。
- **`/status` 的 `traffic.by_status` 在流式中途截断时记 `error`，与审计顶层 `outcome` 记 `ok` 口径不同（刻意不对齐）**：前者答「客户端是否拿到完整响应」，后者答「HTTP 交换是否在传输层正常完成」，各自口径内部自洽（`Telemetry.RecordOutcome` doc comment、`forwardSuccess` 调用点各自写了自洽说明）。截断的客户端信号：TRUNCATED 时 `forwardSuccess` 在把可安全交付的已收字节 flush 给客户端后 `panic(http.ErrAbortHandler)`，此账本口径不受影响。
- **buffered / undecided 模式的 SSE 在上游中途断流时一律不 flush 已缓冲尾部**：`respnorm.Read` 错误分支只把**可安全交付**的字节交出去——非 SSE 响应 flush 部分 JSON（直连也是这个结果），SSE 仅 `modePassthrough` flush 尾部；`modeUndecided` / `modeBuffered` 一律不 flush，避免把未闭合的 `<think>` 泄漏给客户端（审计记 `truncated_withheld`）。随后 `forwardSuccess` 于全部记账之后 `panic(http.ErrAbortHandler)`，客户端 SDK 看到断掉的传输而非格式良好的空 200。软屏蔽 opt-in 路径（`checkSoftBlock` 的 `io.ReadAll`）曾复发同一「吞掉断流错误、假成功」失效，已由 `readCloser` + `errReader` 包装引回同一 `TRUNCATED` 分支。这是「杜绝静默假成功」的硬要求，不是可优化的保守行为。
- **`respnorm` 缓冲区超 `bufferedCap`（当前 8 MiB）即放弃规范化，转 opaque 原样透传**：`modeBuffered` / `modeUndecided` 缓冲越过上限时置 `s.opaque = true`、审计 `norm` 串加 `overflow_raw_passthrough`，把已缓冲字节原样 flush、后续字节直通。**已知代价**：这条路径不再走重写循环（`emitBlock`），因此**虚拟模型名改写（sanctioned deviation 之一）在本条响应上不发生**——响应体留的是上游真实模型名。刻意取舍：失控流（缺 `[DONE]`、无限增长的 `<think>`）继续攒内存、或对超大 body 强行 splice，都比「这一条响应模型名没改写」更糟；opaque 透传正是直连的等价行为。`respnorm` 的 `TestStream_Overflow*` 反向锁死这条降级不再经重写路径。触发面：单条响应体量到 MiB 级且带 `model` 字段——正常聊天/Agent 流量下不出现。
- **厂商专属协议约束拒绝归 `core.ErrQuirk`，不复用 `ErrContextLimit`**：DeepSeek 思考模式要求回传 `reasoning_content`、Google 要求回传 `thought_signature` 这类「换个端点就好」的拒绝，`DefaultClassify` 归入专门的 `ErrQuirk`（切换 + 零冷却）。复用 `ErrContextLimit` 能得到相同的 failover 行为，但审计标签会说谎——这不是上下文超限。OAuth 标准错误码同理独立归 `ErrAuth`。此前三类都落进兜底 `ErrClient`（永不 failover）而中断重试。全量端点级 quirk 模块方向见 §2.48。
- **`/status` 端点项刻意不加端点级累计计数器（requests / ok / failed / tokens）**：`consecutive_failures` 出现在 `/status` 因为它是**当前健康状态**读数（liveness 视图）。端点级累计账是**分析半区**职责——`internal/report` 的 `EndpointRow`（Attempts/OK/Forwarded/Failed/Availability/ErrorClasses/tokens/cost，含 by-date 与 cross-date）已完整产出，数据源是可持久化、可按时间切片的审计日志。给 `/status` 塞一份进程内、重启即失的实时副本：① 与分析半区双账本（正是「一个分析数字复现一个路由数字必须差分测试锁定」要防的负担）；② 做全（4–8 个计数器 × N 端点）等于给 `router.Telemetry` 加一张按端点的动态 map，破坏它「全固定原子、热路径零 map 零锁」的设计。
- **审计响应方向上游原始 Body 不单独落盘（改动由 `norm`/`raw_pre_strip`/`observed_model` 精确记录）**：为大幅节约磁盘占用（避免在长文本与 Agent 多轮交互中为每请求存储两份几乎完全相同的全量响应体），`audit.Attempt` 响应方向默认不存储上游原始 Body（`Body` 为空），而由 `Client.Response.Body` 统一记录交付给客户端的响应体。上游与客户端之间的所有改动（虚拟模型名改写、MiniMax `<think>` 标签剥离、思考过程剥离等）均由 `Attempt` 的 `Norm`（改写步骤列表）、`RawPreStrip`（发生剥离前的原始未改写片段）以及 `ObservedModel`（上游实际返回的模型名）精确记录，足以完整复现与审计上游真实响应。
- **`system.disk.free_space` 在 Windows 上是桩（恒 0）**：`syscall.Statfs` 无 Windows 等价物，而 Windows 不是目标部署平台。
- **`/log` 慢订阅者以「丢行 + 标记」处理，永不让日志热路径阻塞**：每订阅者一条有界 channel（64 行），满则丢行插 `... dropped N lines ...` 标记；`log.html` 不做自动重连（只手动重试按钮），避免重启风暴下的重连洪水。
- **启动 banner 与 panic 直写 stderr，tee 不捕获**：banner 只出现一次，panic 时进程将死，两者都不值得为 `/log` 引入第二条写入路径。

### 1.2 配置与协议

- **协议枚举 2026-08 重命名为 `openai-completions` / `anthropic-messages`（`openai-responses` 不变），与 Pi Agent 等对齐**：全链路一步到位用新名，路由侧零兼容负担。**唯一兼容咽喉点**是 `audit.Record.UnmarshalJSON`：读到旧名经 `audit.CanonicalProtocol` / `audit.NormalizeEndpointLabel` 归一化，只服务分析侧读历史日志；`vmr replay` 不做兼容。重命名当次一并 bump 了 `ctxgraph.CacheSchemaVersion` 使旧事实缓存失效（该常量此后又因别的形状变更继续前进，只增不回退）。**这是「版本必须匹配、不做兼容」原则的唯一刻意例外**——历史审计文件是不可变的既存事实。config 仍带旧名时加载错误直接点名要改成什么（`internal/config/provider.go` 的 `unknownProtocolHint`），但 parser 不接受旧名（strict YAML）。**拆除条件是事实，不是日期**：审计日志的默认保留策略是永不删除（`retention_days: 0`），所以旧协议名的记录**不会随时间自然老化消失**——按日期排期拆除必然误伤。拆除前提改为：对当前全部审计语料 grep 旧协议名（`openai` / `anthropic` 的裸旧拼写）**零命中**，且用户已确认没有离线归档需要再解析。满足后拆除 `audit.CanonicalProtocol` / `audit.NormalizeEndpointLabel`（`internal/audit/legacy_protocol.go`，常量保留在 `internal/core`）、`Record.UnmarshalJSON` 及其为此新增的 `internal/core` import、两个 legacy-name 归一化测试。**适用于**：只跑本机、语料可完整枚举的单用户部署。**不适用于**：把审计日志投递到外部归档、或从别的机器拷入历史日志的场景——那里旧名可能在任何时候重新出现，兼容层应当永久保留。
- **CLI 与 Server 版本必须匹配，不一致直接报错不做兼容**：单二进制、可随时重启，`vmr status` 与 `vmr start` 理应同版本——不一致说明升级没走完，报错正是暴露它。`json.RawMessage` 式兼容层只覆盖一个滚动升级窗口却永久留在代码里，违反 KISS。曾为「旧 server 缺失新 key」保留的 `serving *bool` 兜底已作为死代码删除（`instance.config` 由 string 改 object 后即不可达）——版本必须匹配的原则不再留任何字段级例外。
- **`/status` 的 `instance.base_urls` 回显请求自身地址而非 `listen` 配置**：host 取自 HTTP Host 头、scheme 取自是否 TLS——调用方用什么地址访问 `/status` 就广告什么地址，这正是客户端该填的值。纯展示、不参与鉴权或路由，Host 可伪造无安全影响；刻意不做 `X-Forwarded-Host` 解析。
- **`base_url` 内嵌凭据在加载期报错，而不是在审计侧脱敏**：`base_url` 是自由字符串，`https://u:p@host` 或 `?api_key=...` 会原样进 `Attempt.URL` 落盘——审计脱敏只覆盖 header，这是脱敏模型的唯一旁路。在源头消灭比运行期脱敏正确：脱敏是永远追不全的黑名单。**适用于**：固定的凭据键名清单（`api_key`/`token`/`secret`/`password` 等）与 userinfo 段。**不适用于**：自定义网关用非常规键名承载凭据的情形——刻意不做"值看起来像 key"的启发式判断，那会误杀 `api-version` 这类合法参数。错误信息只回显键名，绝不回显值。
- **价目表的数值防线建在 `pricing.parseTable`，不下沉到 `internal/config`**：`parseTable` 是 supplement/standard/curated 三类手写文件的唯一解析入口，config.yaml 的 overrides 侧另有自己的 `positiveFinite`/`nonNegativeFinite`——两层各自的入口各自把关，config 不重复校验 pricing 的解析结果。NaN/±Inf/负费率一律加载期硬错误：它们会让 `Counters.Cost` 中毒，进而让 `UsedFrac`/`Headroom`/`ScoreForLimits` 全部失效、评分永久停在最大余量，把该账号变成流量磁铁。
- **`report.yaml` 解析失败是硬错误退出，文件不存在才是静默 no-op**：严格解析（`KnownFields`）配上软降级是最坏组合——一个键名笔误会静默关掉**全部** report.yaml 设置，包括自流量排除（分析工具自己的开销于是混进被分析的工作负载）。文件不存在是合法的"未配置"（多数运行本就没有 report.yaml）；显式 `-report-config` 指向的文件不存在则报错，那是用户自己给的指针。报表头另有一行写明本次实际应用的配置文件路径（`Meta.ReportConfigPath`）——"没找到 report.yaml"和"本来就没有"在产物上必须可区分。
- **环境变量未定义时静默展开为空串，不支持 `${VAR:-default}`**：保持配置解析简单明确，默认值在 YAML 里显式写出。
- **`internal/config` 的三层费率解析不后置到 `router.BuildSnapshot`**：`config` import `pricing`、在 `validate()` 跑完解析，看似「配置层反向侵入用例层」，但这是 Quota 设计文档决策表明文选定的方案——「只让 report 一侧解析、`metric: cost` 另开一条运行时校验路径」是同一行里已否决的备选（两份实现容易漂移）。后置还会摧毁「`metric: cost` 费率不齐 = **加载期**错误」这条硬要求。
- **org 前缀请求名的费率解析兜底是**递归**重跑裸名，且残余误匹配风险刻意接受**：带 org 前缀的上游名（openrouter 的 `meta-llama/...`、together 的 `google/gemma-...`）四步全落空后，`resolveCanonicalKey` 用 `pricing.ModelBasename` 掐成裸名**递归重跑全部四步**（含 `<provider>/<basename>` 步）——只重跑裸名/后缀步会让「同名不同写法在同一 provider 上解析到不同价」的命名形态不对称换个位置重现。不做的：按厂商维护 org 前缀注册表（太精确所以太脆）、全局归一化请求名（会失配账号层 `pricing.map`/`overrides` 的原始名 key）。残余：网关自造 id 掐掉前缀后恰与另一模型裸名同名时会命中那家的价——与第 ④ 步 substring 匹配同型的极小概率误匹配，可用 `map`/别名先钉（优先级更高）。
- **多协议适配器（`adapter/{openai,anthropic,openairesponses}`）保持独立子包**：三协议底层已有真实分叉（Anthropic 529 特判、Responses 顶层 `input` 数组与 `RewriteInputRoles`、`x-api-key` vs `Authorization`）；独立子包支持编译期 `init()` 注册与独立单测，新增协议零侵入。合并成参数化结构体只是把多态改写为字符串 `if` 分支。
- **不引入端点级通用运行时 quirks 插件系统**：坚持编译期确定性，只对已证实的厂商行为差异做受控修复。
- **`TopLevelProbe` 的契约是「探测」不是「校验」，不检查尾随字节**：它回答「这团字节是不是某个协议的对话请求」（结构探测，供 `RequestFacts` 与 sticky 指纹用），不承诺「字节流在探针返回的结构之后没有尾随垃圾」。
  加尾随检查会背离字节保真透传：透传层本就把原样字节直送上游，多余的校验只会把合法流量拦下来。
  **适用于**：判断「能不能按协议语义解析出会话形状」。**不适用于**：把它当请求合法性校验器（那不是它的职责）。
- **不合并 `Dimension`（排序）与 `Condition`（淘汰）**：淘汰依赖请求事实，排序只比较端点属性，职责分离保证接口纯粹。
- **ProviderGroup 的多 Key（`api_keys:`）已实现，运行时均衡与分级 Failover 仍不做**：早先设想的运行时 KeyPool（请求期在池内随机选 Key）会违反 `core.Endpoint` 「构造后不可变、`HealthKey()` 只算一次」这条贯穿 health/sticky/quota 的不变式。实际落地是「配置期展开成多个独立 `core.Endpoint`」：`Provider.APIKeys`（`{label: key}`）在 `config.Parse` 里展开成 `<name>-<label>` 命名的独立 `Provider` 并就地重写引用，下游全部按 `Provider.Name` 字符串解析、零改动。当初设想的三处工作，前两处被这个展开形状架构性绕开（均衡：谁排第一不可预先指定，只能读 `vmr check` 的实际展开结果，没配 quota 时排第一的吃全部流量；配额聚合：每把 key 独立 Provider 名、独立 quota 池，对齐难题不存在了），第三处（分级 Failover：402 跳 Key / 5xx 跳 Provider）维持原判，留到看到真实需求。

### 1.3 校验与防御性编程

- **`/status` 的网络可达性与身份认证解耦，且复用聊天入口的同一把 `api_keys`**：网络范围由 `listen` 决定，认证由 `api_keys` 决定——未配 `api_keys` 时任何能连到端口的人都能读 `/status`（2026-08-23 的显式决策，替代旧的 loopback-only）。这把 key 同时是管理凭证：持有客户端 key 者能看到全部端点名、provider 身份、quota 消耗与配置路径。对单人/小团队代理这是正确的简化。`config.Check()` 对「非 loopback 且无 api_keys」给 warning。
- **`vmr status -addr` 回退读取本地 config 的 `api_keys[0]` 并发送到目标地址**：`-addr` 显式指向别的实例时，把本地 key 当 Bearer 发过去。设计意图是让 `./vmr.sh ps` 对本机多实例免手工传 key；只发 key、不进 URL 或日志。目标地址是使用者自己敲的，不是网络层漏洞。
- **看板（`/status.html` / `/log.html`）把 API key 存 `localStorage`，静态外壳免鉴权直出**：外壳不含数据，数据请求走 `s.auth()`；key 只在浏览器本地持久化，不进 URL、不进服务端日志。所有配置派生字符串内插进 `innerHTML` 前均 `esc()` HTML 转义。`/log` 输出 `text/plain` 而非 SSE/JSONL（源头已是格式化文本）；无查询参数（回放窗口固定 512 行缓冲）。
- **`/help.html` / `/help.zh.html` 的 Agent 配置片段在浏览器就地装配，不做服务端模板渲染**：`/help` 按架构必须公开免鉴权，服务端渲染会逼它强制鉴权、或让服务端拿不到用户 Key。API Key 复用 `localStorage['vmr_status_key']`。服务端下发的 HTML 保留写死默认值（`coding` / `claude`、200k context、`high` effort），保证无 JS / 未鉴权时也自洽。四点取舍：max-output 预算按 context 分档经验估计（VMR 无模型级元数据）；片段一律 vision-on（空 capabilities = 不受约束）；四个列表型生成器只枚举 `openai-completions` 模型；无浏览器 JS 测试基建，`TestHelpPage_SnippetFillEngine` 只做构建期字符串守卫。
- **`nil` 校验只加在跨包公共入口且一律 fail-fast，绝不静默兜底**：已加的是 `report.AnalyzeSessionsCached` 与 `story.BuildChain`/`BuildAll`/`PreviewTitle`/`PreviewTitles` 五个入口——判据是「跨包公共 API + 后接并发扇出或递归组装」。包内被这些入口保护的函数不重复校验。
- **持续性故障的日志按"错误文本相同"去重，不做事件级审计**：quota flush 失败（磁盘满、权限变更）与时钟回退都是持续性的，10 秒一次刷屏会淹没日志。flush 侧按错误文本去重（首次 + 每 10 次，附连续失败计数），时钟回退侧每进程最多一条 WARN。**已知代价**：两种错误交替出现时 flush 侧不去重（每 tick 一条——但交替本身就是有效信号）；时钟"回退→恢复→再回退"的第二次不再 WARN。**边界**：需要回退事件级审计的话，这里要换成带去抖窗口的计数器。
- **`vmr-quota.json` 的结构损坏整文件拒绝，绝不部分采纳**：静默丢掉一个 provider 的账本比报错更危险。版本戳不匹配、nil account map、null bucket 三者任一即视为损坏，由调用方 WARN + 从零开始——与既有的语法损坏路径同构。`version` 字段从"写而不校验"改为真正的门：有版本戳却不校验比没有更危险，下一个人会以为"有版本号所以安全"。
- **配额周期的惰性重置方向敏感**：只有周期真正前进（`ps > PeriodStart`）才重置计数。NTP 向后校正、VM 快照回滚、容器 TZ 变更都会让周期起点向后跳，而“不等即重置”会抹掉整个计费周期且随下次 Flush 落盘、不可恢复。反方向保留计数并 WARN。
- **原子写只做文件级 Sync，不做目录 fsync**：全仓的 CreateTemp+Rename 站点（quota 账本、audit 压缩、ctxgraph `.parse-cache`、reqdetail 证据、story LLM 缓存）都不 fsync 父目录——掉电时 rename 的目录项可能未持久化，最近一次落盘可能丢失或回退。刻意取舍：丢失代价分别是“统计计数回退到上次 flush”（quota，文件级 Sync 已做）与“缓存 miss 重算”（其余站点，多数连文件级 Sync 都没做，靠读取侧的哈希/schema 校验把半写内容兜成 miss），全部落在各自 best-effort 契约内；而目录 fsync 每次落盘多一次系统调用，换来的只是把丢失窗口从“最近一个 flush 间隔”缩到零。若未来某站点升级为“不许丢”的契约（如计费级账本），在该站点单独补目录 fsync，而不是全仓统一加。
- **`fmtutil.DisplayZone` 保持裸 `var`，不封装线程安全访问器**：生产代码零写入点——全仓写入全在 `_test.go` 且相关测试无 `t.Parallel()`，`-race` 全绿。「让测试能确定性覆盖」本就是它存在的理由之一。
- **尤其不做「`prof == nil` 就回退到 `Generic`」这类静默兜底**：`OpenClawAware` 与 `Generic` 给出不同的任务标题与边界，静默换一个 Profile 会产出一份错误但看起来正常的分析结果，比 panic 难查。
- **`.parse-cache/` 不做分片孤儿回收 GC**（原 1.27）：`ctxgraph.SaveCacheDir` 只增量写入当前存在的分片，不主动删旧 hash 孤儿分片。缓存是完全可再生的派生产物，`vmr report`/`vmr story` 均可从空缓存目录冷启动。触发条件：`.parse-cache/` 体积超过同批压缩审计日志总体积（当前实测 51MB vs 177MB），或升级后异常磁盘占用；在那之前「整目录删除重建」比任何 GC 更简单可靠。**2026-09-03 修正**：孤儿分片不只是磁盘膨胀——`LoadCacheDir` 按 `CanonicalPath` 为 key、按文件名（=hash）字典序遍历、后写覆盖，一个字典序排在当前分片之后的过期分片会覆盖 map 里的新分片，`scanCachedFile` 的 `Hash` 校验失败 → 整文件全量重解析（对已轮转未压缩的昨日文件约 50% 概率）。已由 `LoadCacheDir` 按分片文件 mtime 消歧缓解（append-only 日志下 newest = current）；**彻底解法**是 `FileCache.Files` 改按 `Hash` 为 key，涉及 `scanCachedFile` / `report/factscache.go` 的 `loadCachedFacts`/`storeCachedFacts` / `cache.go` 的 merge 多处，留待后续。
- **`.parse-cache/` 分片文件名是内容哈希、cache key 是内嵌 `CanonicalPath`，两者刻意不对齐**：文件名=内容哈希使同名冲突天然不可能（两份不同内容各得各的分片），代价是「从审计文件路径反查分片」必须遍历读内嵌字段——`LoadCacheDir` 本就是 best-effort 全扫描，这个代价在契约内。运维侧想按路径定位分片时 grep `CanonicalPath` 即可，不要改成路径哈希命名（会重新引入内容冲突的命名空间问题）。
- **默认分析套件不物化 `details/`，`report` 的「文件」列判据是文件存在性而非 `-details` flag**：`writeJourneyFile` / `renderJourneys` / `renderAllJourneys` 带 `materializeDetails` 入参--只有单条下钻、`-compare`、`-render-all` 传 `true`;默认套件的脊柱「→ detail」与 sysprompt 指针渲染成行内 `文件:行` 坐标(`Manifest.Req` 的纯函数),不写盘、不留 404 链接。`report.detailCell` 因此不能只看本次的 `-details`:`vmr analyze` 先跑 story 半区(可能已批量物化)再跑 report 半区,纯 flag 判据会谎报「没写详单」或反之--改查 `r.DetailFile` 是否真实存在(一次 `os.ReadDir` 建 set)。常驻守卫测试盯着「默认套件 `details/` 为 0、指针是坐标非链接」,人为改回无条件物化当场失败。这条纪律反复退化过四次,这次靠测试锁死。

### 1.4 包边界与依赖

- **`imgprep.ImageInfo` → `audit.ImageInfo` 的字段拷贝**：换 `imgprep` 不依赖 `audit`，保住公共工具包零依赖边界。
- **`chatmsg.ReassembleSSE` 与 `respnorm` 的 SSE 状态机保持分离**：前者面向离线完整语义提取，后者面向在线字节级保真转发，关注点不同。
- **`ctxgraph.Manifest.MsgIdx` 没有生产消费者，但不是死数据**：`ctxgraph` 不导出任何哈希函数，`MsgIdx` 是包外把 `Keys[i]` 对回 `chatmsg.Messages` 元素的**唯一通道**——`internal/story/structure_test.go` 靠它验证"内容寻址坐标确实解析到所声称的内容"这条不变量。删掉它等于让该不变量无法从包外验证，还要让全部用户白付一次全语料重解析。与 `health.Registry.Available` 同类：「无生产调用方」不等于「可删」。
- **LLM 文本的 Markdown 结构转义做在 Finding 构造时，不做在渲染侧**：同一份文本要进 Markdown 与 HTML 两种产物，而 HTML 侧的转义早已由 `story/mdlite.go` 在渲染时全量完成（`<script>` 从来进不去）。真正没人管的是 Markdown **结构**破坏——反引号、竖线、行首结构标记（ATX 标题、`-`/`*`/`+` 列表项、有序列表 `1.`、块引用 `>`（含无空格形态）、主题分隔线 `---`）——而它在 `i18n` 模板层修不了：模板把文本插进结构位置，转义必须发生在插进去之前。**已知代价**：finding 文本此后永久带反斜杠，非 Markdown 消费者（如 JSON 导出）会看到转义痕迹；行首的 `>`、数字+点+空格（如 `>= 5`、`2026. `）也会被转义，渲染结果不变但 JSON 侧可见。
- **`internal/report/cost.go` 的端点标签切分不并入 `core.SplitEndpointLabel`**：后者兼容 `:` 与 `/`，前者只认 `:`。放宽 `$` 成本估算那个调用点会改变旧格式日志的历史报表金额——一次需单独评审的行为变更，不是「统一实现」的顺带产物。
- **`core.StickyBackstopTTL` 不迁回 `internal/sticky`**：迁回制造一条 `config` → `sticky` 的新依赖边，仅用于读一个常量；不做这个校验则 `sticky_ttl` 超过 backstop 的配置会「看起来被接受、实际静默失效」。
- **降级 token 估算的 fallback 刻意不对称：请求侧回退原始字节、响应侧一律 0**：降级估算的统一规则是「用对内容最忠实的可用表示估算内容 token；剩余字节量到的若不是内容本身（SSE 信封、压缩/损坏的 opaque 字节），宁可为 0——量错一个量比没有估算更糟」，且每一侧都必须镜像路由半区实际扣减的基。两侧信息状态不同，同一规则推导出的分支就不同：请求侧的原始字节是「内容 + 脚手架」，且路由侧输入扣减（`Facts.EstimatedTokens`）本来就是 raw 基——回退 0 会让报表与实扣劈叉；响应侧的原始字节在截断/opaque 场景量的是传输不是生成（Q04 的 71 倍虚高），回退 raw 等于把它复活。规则全权落在 `EstimateDegradedTokens` 的 doc comment（`internal/chatmsg/tokenest.go`）；不对称行为由 `TestEstimateDegradedBasis_FallbackAsymmetry` 钉死，对齐情形（两侧可提取文本、两侧 opaque）由 quota parity 测试钉死。**不要「统一」两侧的 fallback**——任何统一方向都已论证过是复现已修过的 bug。
- **`adapter` 的协议字段字面量（`"model"`/`"stream"`/`"messages"`/`"input"`）不从 `jsonscan` 导出复用**：它们是不可变字节常量而非共享状态；「知道这些字段名的含义」正是把 `SessionFingerprint`/`TopLevelProbe` 留在 `adapter` 的领域知识。（`jsonscan` 自身的准入措辞 2026-09 已重写，见下条。）
- **`jsonscan` 的 `RewriteModel`/`RewriteRoles`/`RewriteInputRoles` 留在 `jsonscan`，不迁 `adapter`**（2026-09 Q17 收敛）：原评审指出「同批协议字面量在 `jsonscan` 包文档与 `adapter` 得出两个相反归属结论」，最终以重写 `jsonscan` 包文档的边界规则消除，而非移动代码——「字节级扫描与 splice 改写引擎」整体归 `jsonscan`（含带协议字段字面量的改写函数，fuzz 覆盖在此包），「协议路由语义、适配器构造、错误分类」归 `adapter` 及以上。旧表述「需要具体字段名的函数不属于 `jsonscan`」已废止，**不要再提案移动这批改写函数或恢复旧措辞**。
- **`core` 准入规则的例外清单是显式豁免，不是待清理项**（2026-09 Q18 收敛）：`Endpoint.HealthKey`/`Name`/`Freeze` 保留在 `core`——它们是「双半区无主、纯计算于 Endpoint 自身字段」的值对象方法（`HealthKey` 是 health/sticky/quota 共用的端点身份，`Freeze` 只是把两个纯函数 memoize 供快照构建），外移到任何单侧都会制造反向依赖或循环。已落地的清理：`SortedKeys` 下沉 `fmtutil`；`ModelLabel` 也下沉 `fmtutil`（2026-09 复核：其签名不含任何 core 类型，是纯展示格式化，两个调用方本就 import `fmtutil`，无依赖两难，不构成例外）；`StickyBackstopTTL` 以「canonical 在 core」如实标注（见上文）。准入规则从「绝对禁令」变为「禁令 + 显式豁免清单」，新增符号仍需逐个过审。**不要再逐个提案外移这批豁免符号**。
- **`archtest` 的包边界守卫是单向的，与规则本身同构**：CLAUDE.md 的不变量「分析半区不 import 路由半区」是单向禁令，`import_boundaries_test` 只需要守这一半；「audit JSONL 记录是唯一耦合」那半句是**数据流事实**，不是另一条可机检的 import 规则，不存在对应护栏也不需要有。不要因为「只守了一半」提案加反向守卫——反向（路由 import 分析）本来就是合法的依赖方向。
- **不把分析半区拆成独立二进制**：坚持「单二进制单文件分发」。
- **不引入 DuckDB / cgo 做数据聚合**：保持纯 Go、跨平台零 C 依赖。
- **`i18n` 的 26 个微文件不合并**：与 `internal/report/section_*.go` 的「一节一文件」硬规则一一配对（`archtest` 强制），合并击穿 700 行全局预算，且改一节文案从打开小文件变成在大文件里找。
- **`i18n` 的 `type XxxText` + `if lang == ZH` 样板不改写成 `map[Lang]T` + 泛型 `pick`**：改写只消掉每文件 2 行分支，占体量的 struct 定义与两份字段赋值一行都省不掉，还新引入泛型 helper 与「key 缺失怎么办」。收益为负。
- **`internal/probe` 不登记进 `zeroInternalDepPackages`**：那张表的语义是「**承诺**永远零依赖」，不是「当前碰巧零依赖的都登记」。`probe` 独立成包是为避免 `diagnose`→`router` import cycle，未来 import `core` 完全合理。（`rundir` / `buildinfo` / `sysinfo` 与 `tokenutil` 均作为基础叶子包登记守卫。）
- **`internal/core/core.go` 不按领域拆成 `endpoint.go`/`quota.go`/`pricing.go`**：同包拆文件不改变任何编译依赖，是代码导航整理不是架构重构。真正解决「core 会不会长成上帝包」的是准入规则，已写在包注释里并对存量逐条复核过。
- **`imgprep` 的 `map[string]json.RawMessage` 不与 `jsonscan` 的字节扫描统一**：图片降采样要重算尺寸并重编码，是深度结构化重写，字节 splice 做不到。这是三个 sanctioned deviation 里最大的一个。
- **不向 Clean Architecture 四层同心圆靠拢做整体重构**：要把横跨环边界的包「归位」就得为满足图示而拆包插接口，代价是新的包边界与一层不解决任何真实问题的间接性。项目已有更强且**可执行**的架构模型（两半区 + `archtest`）。反证：`internal/config` import `internal/adapter`（校验期需知道协议注册表）按 CA 是「外环依赖内环」的合法边——CA 本就不是这个项目合适的透镜。
- **不对 OpenAI 工具返回做 `error:` 关键字模糊嗅探**：实测全量生产语料 495,672 条 OpenAI 工具调用结果，结构化 JSON 错误字段 0 条（0.00%），全部是自由文本 stdout/stderr。子串模糊嗅探会引入海量代码输出/测试用例的假阳性。只对协议原生结构化错误标记（如 Anthropic `is_error`）做确定性统计。
- **`go.mod` 保持裸模块名 `vmr`**：改名要动全项目 import 路径，无实质收益。
- **模型/端点展示面的一致性靠统一口径 + 契约测试，不靠共享结构体**：运行时视图以 `/status` 的 `models` 数组为唯一权威（`vmr status` CLI 与 `status.html` 直接消费同一 JSON）；人类可读模型标签 `"<name> [<protocol>]"` 只在 `fmtutil.ModelLabel` 一处定义。刻意不统一的三处：`/v1/models`（协议面 schema）、`vmr check` 的分层 config 视图（看配置缺口）与 `/status` 的聚合运行时视图（并集/最大值）、`vmr diagnose` 的扁平 Result 数组。`/status` JSON 形状由 `internal/server/admin_status_test.go` 契约测试锁定。

- **用量折算（精确 vs 降级）的跨包入口刻意只有一条，没有单标志合并形式**：`quota.TokenCountersSides`（纯标量入参）是唯一权威实现，`router.TokenCountersSides` 是 `chatmsg.Usage`→`quota.TokenUsage` 的唯一翻译层（`report` 直接调 `quota` 侧，`archtest` 禁它 import `router`）。2026-09-04 删除 router 侧的单标志退化形式（router.TokenCounters，已不存在——零生产调用方、仅自身测试保活）——合并的“some usage was seen”信号无法区分完整账本与部分账本，把 partial 当 exact 记账正是 sides 拆分要消灭的 bug 类（`replay` 的 `chargeReplay` 曾把截断流的 ~1 占位 out 计成 exact，commit `ba6b0b3`）。**不要以“API 对称”或“给未来消费者留入口”名义复活单标志包装**——只能给单比特的调用方本就该逐侧决策。

### 1.5 产出与工程惯例

- **用 Go 结构化代码而非 `text/template` 渲染 Markdown**：复杂条件列、对齐与动态脚注在 Go 里更容易保持类型安全和可读性。
- **不维护外部贡献者 `CONTRIBUTING.md`**：与小团队运作方式不匹配。
- **分析产物 ZH 术语的 loanword / 全译两套约定并存，刻意不统一**：Markdown/报表侧保留英文特性名 + 中文描述词（`§6.5 Sticky 有效性`、`§6.7 Compaction 还原`、`§2.5 账户（Provider）消耗与额度`，journey 叙事正文里 `system prompt` 也一贯是外来词）；HTML 看板侧全译（`系统提示词` / `上下文压缩`）。两套各自内部自洽。全量统一要改约 15 处 i18n 字符串 + 发给 LLM 的 prompt 正文 + `UserGuide.zh.md` / Analytics 设计文档里的既有章节名，收益纯观感、还牵出「Compaction 该不该译」之争（类比 `prompt cache` 通常不译）。**触发条件**：同一 section 内出现自相矛盾的形态（如标题译、紧邻正文不译），才值得局部收敛。新增 i18n 字符串时跟随同 section 已有正文的形态。
- **`internal/story/mdlite.go` 只覆盖 `-compare -html` 的 LLM 解读段实际会用到的 Markdown 子集**（ATX 标题、段落、无序列表、GFM 竖线表格、`**粗体**`、`` `行内代码` ``——全部先转义）：`-compare` 的 LLM 提示词明确要求「结论句 + 候选根因表 + 三个三级小节」，围绕这个形状裁剪。有序列表与围栏代码块落进段落分支（已转义、无注入、不丢字符）。不引 CommonMark 解析器。已知瑕疵见 §2.51。
- **索引折叠与默认渲染范围只把 `heartbeat` 归为噪声，不含 cron / subagent**（`story.IsNoiseCategory`）：真实语料实测——heartbeat 每候选最多 7 请求（107 个候选无一到 10），而 cron 与 subagent 都有双位数请求的候选，含全语料最长的一条 journey（subagent，91 请求）。索引显示分割与 CLI 默认渲染范围共用这一个判据，避免二者对同类候选给出不同答案。
- **stitch 缝合同时要求比例阈值与绝对下限（共享去重键 ≥3）**：断裂后的开头 manifest 天然很短（system + 摘要 + 第一条指令），一条共享消息就能把比例顶过任何阈值——而那条消息往往正是 SessKey 本身的构成成分，它共享是**因为**这是同一个会话的锚，不是因为发生了 compaction（证据循环）。比例防长会话、绝对值防短会话，两道闸正交。不满足下限**降级为 `AmbiguousMatch` 而非淘汰**，候选仍可供人工查看。论证谱系与 `edit.go` 的 `spliceMinTailMatch = 2` 相同。
- **同 SessKey 候选有 72h 宽松时间上界（`stitchSameKeyMaxGap`），超窗候选预过滤出局，最强者仅作诊断兜底**：旧规则豁免同桶候选的理由是“用户可以走开几天再回来接同一个 anchor”——**对人类成立，对机器相反**。同一 anchor SessKey 下堆积最多的是定时/心跳任务：开头模板相同、彼此无关、可跨数百小时，正是当初促成 `stitchCrossBucketMaxGap` 的那批假匹配，只是发生在桶内所以那道闸从没管过。2026-09 收敛为**淘汰优先于排序**（与 `strategy` 包 `Condition`/`Dimension` 分离同型）：超窗候选不参与赢家竞争，避免「高分超窗者先赢再降级」遮蔽窗内合法前驱；仅当过滤后无任何窗内候选时，最强超窗者作为降级 `AmbiguousMatch` 边保留供人查看——真的走开三天回来接着聊的人不会消失进 `NoPredecessorFound`。
- **消息内容哈希剥离 Anthropic 的 `cache_control` 标记**：`cache_control` 是缓存控制元数据，不是对话内容；客户端逐轮移动缓存断点会改变哈希，把一次纯 Append 误判成内容编辑，整条 lineage 谱系失真。**证据状态要如实说**：机制已从代码确认（`hashJSON` 对原始消息对象全字段哈希，标记确实进哈希输入），但本机语料太小（36KB / 1 条 anthropic 记录 / 0 条 `cache_control` 命中），**按协议拆 Append 比例无法产生统计意义，未能从语料实证**。剥离本身严格更正确，故仍实施。**已知副作用**：消息内容载荷里键名恰为 `cache_control` 的（如工具结果回显）也会被剥离——只影响哈希与 lineage 判定，不影响存储内容与渲染。
- **详情页在 `report` 与 `story` 之间字节一致，靠的是两侧传入同一个 `(record, manifest, prev)` 三元组 + 指纹携带 `m`/`prev` 身份**：只做其中一半都不够。`report` 侧曾在 `group()` 里对 compaction 记录先 `continue` 再赋值 manifest，于是它的 manifest 恒为 nil，而 `story` 侧传的是真实 manifest；渲染指纹只含 lang/evidence，于是同名文件**先写者赢**——用户拿到哪个版本取决于先跑 `vmr report` 还是 `vmr story`。补上 manifest 消除差异源，指纹折入身份防同类复发。
- **`archtest` 的文档守卫不扩展到 review 报告类文档**：守卫只覆盖 `CLAUDE.md`、设计文档、本文件与用户指南。review 报告会正当地讨论已删除的文件与「建议新增的 XXX 函数」。真正的风险（一份陈旧 review 被当施工依据）**用定位而非机制解决**：权威的当前状态清单只有本文件。
- **`archtest` 不加圈复杂度检查**：一次只加一个守卫。函数长度预算落地不久，确认不够用之前不引入第二个。
- **`buildinfo` 只输出 VCS commit 哈希，不人工编造语义化版本**：如实反映构建来源。
- **官方用量 API 不预先抽象 `Source` 接口**：YAGNI，等真正接入第一个厂商私有用量接口时再设计。
- **聚合浮点字段在冷/热缓存两次运行间的 1 ULP 级差异不追查、不消除**（原 1.24）：浮点加法不满足结合律的教科书现象，不是可以「修好」的缺陷。唯一该做的事（差分/一致性测试用容差而非逐字节相等）**已经是现状**（`report/e2e_test.go` 用 `1e-6`、`quota_parity_test.go` 用 `1e-9*want`）。唯一需重新当作 bug 的情形：差异远超浮点精度量级，或开始出现在 `cost_estimate` 之外的字段上。
- **文件与函数行数预算线是提醒式绊线，非架构缺陷**（原 1.5）：`internal/archtest` 的文件/函数预算（默认 700 / 120 + 豁免表）是轻量提醒机制，连 Warning 都算不上。未触线前无需焦虑、不需在常规 review 里逐个排查；一旦触线，按职责拆分重构（如 `detail.go` → `internal/reqdetail`、`config.go` 拆出 `apikeys.go`），或逻辑内聚时临时按 +15~20% 调高豁免。
- **可维护性的核心在整体架构与设计复杂度，而非代码行数**（原 1.15）：单人可维护性取决于是否守住 First Principles / KISS / YAGNI、是否消除了不必要的过度设计与复杂分支，而非机械度量行数或两半区体量比。探索性新分析指标优先用外部脚本消费稳定的 `vmr-report.json` / `journey-*.json` 契约验证，确认真实价值后再评估主库实现。
- **LLM 解读层生成结构化 Finding 的准入与置信度契约**：LLM 判别器产出的 Finding 必须强制标记 `Source: "llm_inferred"`、离散置信度（`HIGH/MEDIUM/LOW`）与原文 `EvidenceAnchor`。仅 `HIGH` + 直接证据锚点的项以 Finding（⚠️）呈现并标 `[AI推测]`；`MEDIUM`/`LOW` 降级为参考提示。**锚点运行期强制校验**：`ComputeLLMFindings` 收完全部 detector 输出后逐条 `strings.Contains(真实 transcript, EvidenceAnchor)` 校验，非逐字子串即丢弃——但这是**防幻觉**检查，**不是防注入**：注入方就是转录本的作者，他能同时植入被引用的锚点和结论，`Contains` 必然命中。注入面靠另外两道闸收口：LLM 来源的 Finding **一律不参与 `pickDriver`**（仪表盘顶部判词只可能来自规则产出），且 `StepSeq` 不在本 Journey 真实步号范围内的 Finding **直接丢弃而不是 clamp**（clamp 会把攻击者选的序号映射到一个合法步）。问法严格约束在有证据支撑的事实性问题上（拒绝开放式主观质量打分），守住「揭示事实与过程异常而非冒充裁判」的边界。


## 2. 待定与待解决问题（分组；组内按用户价值 × ROI 排序）

> 标题方括号里是**严重程度**（现在有多糟），不是优先级；它与组内顺序（现在做有多划算）是两个正交轴，
> 不该也不会一致。要排期看组内顺序与 §3 优先级总览，要判断「现在有多糟」看方括号。
>
> **条目编号（2.x）只是稳定身份标识**（由旧编号 1.x 平移而来，供历史引用对号），不代表顺序；
> 组内从上到下才是有效优先级，「决定不做 / 暂不做 / 非活跃」类条目排在组尾、各自带触发条件。

### A. 分析半区 · 大语料规模（内存与耗时）

#### 2.2 [中] `vmr report` / `vmr analyze` 全内存聚合的记录量上限

- **现状**：`AnalyzeSessions` 常驻全部记录关键信息 + 原始耗时/延迟/Token 样本切片（算真实百分位）。实测万级记录即 GB 级 RSS（`report` 单跑约 1.4GB / 1.1 万条；`analyze` 组合路径约 3.75GB / 1.5 万条）——原文「千万级约数百 MB」是量级判断错误，已作废。
- **story 半边曾是更大的来源，已消除**：`story.Step` 曾持有完整 `audit.Record`，让每 request 重发全历史的 O(N²) 原始字节钉在对象图里——`vmr analyze -corpus` 因此在全量语料上峰值约 43GB（16GB 机器 swap thrashing 假死）。2026-08 改动：`Step` 只保留 `buildFrom` 预提取的事实，不再持有记录；`-corpus`/`-render-all` 改为按 `Manifest.Bytes` 字节预算分批构建。实测（43 文件 / 约 12.4GB 解压）`-corpus` 峰值 RSS 从约 43GB 降到约 2.4GB、`-render-all` 从 4.1GB 到 1.9GB（详见 `CHANGELOG` [Unreleased]）。全部 Journey 常驻只剩约 300MB，与语料量解耦。
- **剩下的**：report 半边的 `AnalyzeSessions` 样本切片仍是全内存（未触及本次改动）。
- **可能方案**：按审计日志的时间局部性分自然日分桶，跨日即时释放原始切片。
- **为什么仍待定**：report 半边这个量级目前仍跑得完（约 1.6GB / 1.5 万条，16GB 机器有余量），且分桶释放依赖「记录时间严格单调递增」这个隐蔽正确性前提，不成立就是静默算错而非报错。**触发条件：单次 `report`/`analyze` 宏观半边语料 > 约 3 万条，或该半边峰值 RSS > 4GB**。
- **一处已做的收窄**：`ctxgraph/stitch.go` blob 倒排索引 `map[Hash]map[int]bool` → `map[Hash][]int`（去掉数百万单元素小 map 头开销），只降常数、不改分桶前提。
- **相关未做项（warm-path，登记待触发）**：语料不变、只渲染单个 journey 时，`setupStoryRun` 仍无条件全量 `ScanCached` + `buildGraph` + `StitchGraph`。窄路径需给 `vmr-stories.json` 的 `JourneyIndexRow` 补 `stitch_edges`（每条 lineage 的前驱边持久化，按内容寻址 `LineageID` 重放，避开 tie-break 不确定性）+ 一条陈旧性闸 warm path。触发条件同上（语料 > 约 3 万条，或大语料上反复 `-journey` 调查）。


#### 2.1 [低，已部分闭环] `vmr report` 多文件输入：会话分析那一趟（`collect()`）仍未缓存（含原 1.23）

- **现状**：`report.Build`/`BuildCached` 跑三趟扫描——① `ctxgraph.ScanCached`（manifest，已缓存）② `collect()`（会话/任务分组用的每记录特征）**未缓存，每次全量重跑** ③ `aggState.scanFiles`（指标聚合，P3.6 已接 `factscache.go` 缓存）。③ 接缓存后真实语料实测热耗时 5.2×，但 ② 未缓存使热耗时离个位数秒仍有差距。
- **为什么不顺手做**：`collect()` 产出（`ReqInfo`）直接喂 `group()`/`ctxgraph.StitchGraph` 做会话/任务边界判定，正确性敏感度高于纯指标聚合（算错是把不相关对话缝到同一 Journey）。
- **触发条件**：投入前先补一套对等的 cold/warm 一致性测试（参照 `TestBuildCached_WarmMatchesBuild`）。


#### 2.55 [低，登记待触发] `story` 的 `BuildAll` 仍先把一批的全部记录物化成 map

- **现状**：2026-08 改动让 `Step` 不再持有 `audit.Record`，全部 Journey 常驻降到约 300MB；但 `BuildAll` 内部仍 `FetchRecords` 把一批（字节预算 ~160MiB 原始）的记录一次性收进 `map[Loc]*audit.Record` 再 `buildFrom`，这个 map 是每批的瞬时峰值来源（约几百 MB）。
- **可能方案**：全流式——`FetchRecords` 的 map 也不要，逐条喂给 builder。要求把 `buildFrom` 从「拿到全部记录后遍历」改成「按到达顺序 feed」，并处理并发扫文件的乱序（Event 的 `FirstStepSeq` 需按 seq 事后归并）。改动量比本次大一个量级。
- **触发条件**：语料再涨约 5 倍，或需要在 8GB 以下机器上跑全量 `-corpus`/`-render-all`。


#### 2.56 [低，登记待触发] 一次 `vmr analyze` 至少把全量语料解压三遍

- **现状**：`.parse-cache` 只覆盖 `ctxgraph` 的 manifest 扫描。`PreviewTitles`（全部候选根记录）、每批 `BuildAll` 的 `FetchRecords`、report 半边的 `analyzeFile`（§2.1）各自独立全量解压一遍——全量语料上是 `-render-all` 约 500s 耗时的主要来源。
- **相关**：`FetchRecords` 的接口形状（返回全量 map）天然逼调用方驻留全部；`PreviewTitles` 是纯提取（读一条、取一句标题、丢弃），可顺手切 `ctxgraph.ForEachRecord`，消掉一个约 600MB 的瞬时峰值。
- **可能方案（治本）**：让 `Manifest` 携带每步 delta 正文，取消 `FetchRecords` 这第二遍解压。但要把叙事提取逻辑从 `story` 挪进 `ctxgraph`，破坏后者「不驻留正文」的契约，`.parse-cache` 从几 MB 涨到约 160MB，且该 cache 是 report 半边共享的。跨包契约 + 双半边影响。
- **触发条件**：内存不再是瓶颈后，时间成为首要痛点时单独立项。


#### 2.50 [低，潜在] 详单文件名去重位 `md5(basename:line)[:4]`（32 bit）

- **现状**：`internal/ctxgraph/reqcoord.go` 的 `ReqHash8` 给详单文件名算 4 字节 hash 去重后缀。单源文件近 1 万条记录时按生日界碰撞概率约 1%；真正撞成同一文件名还需同毫秒 + 同模型 + 同 outcome，现实可忽略。
- **恶化曲线**：与 §2.2 的语料上限同步线性恶化。真出现时把去重位提到 hash12/16 或改用递增序号，都是局部改动。


#### 2.3 [低，决定不做] `chatmsg` 离线解析路径的 `map[string]any` 分配

- **现状**：`internal/chatmsg` 43 处 `map[string]any`，全在离线消息/SSE/usage 解析路径。转发热路径实测零命中。
- **决定不做**：2026-08 内存分析在真实语料上直接测了这一层——`audit.Record` 反序列化后的 live heap 相对原始 JSON 字节只放大 **1.40x**（审计记录绝大部分是长文本对话正文，`string` 只有 16 字节 header，结构开销被文本稀释）。把 `Body` 从 `any` 改成 `json.RawMessage` 延迟解析最多省 29%，不改变量级，却要改动 `story`/`report`/`reqdetail`/`chatmsg` 里几十处 `.(map[string]any)` 断言——投入产出比最差。story 半边的内存问题另有真因（见 §2.2），已单独解决。
- **触发条件**：真实 profile 显示某个离线聚合路径的时间/内存确由 `map[string]any` 分配主导（当前证据相反）。

#### 2.69 [低，登记待触发] `searchableTranscript` 大语料下 O(N²) 全量物化

- **现状**：`internal/story/llm_findings.go` 的 `searchableTranscript` 为每次锚点校验把 Journey 的转录本整体拼接成字符串。校验次数 × 转录本长度是乘积关系，大语料下是分析半区唯一的复杂度悬崖。
- **可能方案**：校验改在已分片文本上逐段 `Contains`（锚点语义不变），或对超长 Journey 截断校验域并明示。
- **触发条件**：`vmr analyze -llm` 在真实大语料上出现可感知的耗时占比（当前无实测瓶颈）。


### B. 分析半区 · 指标与口径正确性

#### 2.57 [低] `computeTimeSplit` 单间隙时间归因无上限，污染 corpus 均值

- **现状**：`internal/story/metrics.go` 的 `computeTimeSplit` 对每对相邻 Step，把「上一步响应落地 → 下一步请求到达」的整段 wall-clock 间隙按「下一步是否 `HumanInitiated`」二分为 human idle 或 `AgentExecMS`，间隙不设上限。跨天/跨周的 lineage 上，一段几十天的空档会整段计入「Agent 执行时间」——`vmr analyze -corpus` 的 `Agent-Side Execution` 因此出现 `Median 8s / Mean 数小时` 乃至 36 天量级的均值。
- **当前缓解**：`-corpus` 指标分布表已加脚注「time 类指标的 Mean 被少数长命 journey 严重拉偏，看 Median/P90」（2026-08-31）。只是免责，没动根因。
- **可能方案**：对单间隙设上限（如 > 1h 归 idle/unknown 而非 agent 执行）。需改指标语义 + 更新 Analytics 设计文档的时间拆分定义 + 差分测试。
- **触发条件**：脚注被证明不够（读者仍据 Mean 下结论），或要把 `NetWorkingMS` / `ModelToToolRatio` 当硬指标用。


#### 2.58 [低] 定价表覆盖不到的模型，其成本永远不进任何合计

- **现状**：`§2` 四张表都带合计行，合计只含解析出费率的行，并在表下注明「合计不含 N/M 天（个模型/端点/客户端）」。未定价行本身仍渲染、成本列写 `-`（不是 0，也不是整行消失）。剩下的是数据缺口本身，不是呈现缺口：一个三张表都查不到的模型，其流量的成本就是未知。
- **当前缓解**：厂商优先级消歧 + curated 别名把标准表的可用覆盖面拉满（2026-08-31 快照：709 个裸名里 78 个撞车，6 个由 curated 别名钉死、51 个自动解开）；带 org/路径前缀的聚合商模型名经 `pricing.ModelBasename` 兜底与裸名同解析（2026-09 交付，见 §1.2 对应条目）；剩余缺口由用户在 `pricing.supplement` 自补，不补则如实显示未定价。`vmr check` 在表龄超 60 天时提示刷新。
- **可能方案**：无代码方案——这是数据边界。框架只保证查得到就用得上、查不到就说不知道。
- **触发条件**：常用模型长期不在任何一层表内，且用户不愿自补。

#### 2.58a [低] 费率缺分量时按 0 计价，只在 §2 汇总层披露，不逐行标注

- **现状**：`pricing.Rate.Cost` 把 nil 分量按 0 计价（防御性下限）。`§2` 会汇总提示「有 N 个端点的单价缺分量」，但具体是哪几行、缺哪一项、少算了多少，行上看不出来。
- **当前缓解**：`EndpointRow.CostRateIncomplete` + `§2` 的 `IncompleteRateNote`；`metric: cost` 那条路径不受影响——它有 `pricing.Complete` 的加载期硬门。
- **可能方案**：与 2.58b（逐行溯源）同一批做——两者都需要把解析结果的元信息从 `pricing.Resolve` 一路穿到 report 的行结构。
- **触发条件**：主力模型的厂商长期不公布缓存价，而账号缓存命中率又高。

#### 2.58c [低] §2 按客户端表的合计略低于其它三张表，因为没有 `client_key` 的请求不成行

- **现状**：`§2` 四张表各自对自己的行求和。按日期/模型/端点三张覆盖全部记录，按客户端那张只覆盖解析出 `client_key` 的记录（auth 关闭或没匹配上任何 key 时为空），这些记录压根不成行，于是也进不了那张表的合计。实测差额在 0.002% 量级。
- **当前缓解**：无。四个合计都是各自表内行的诚实求和，没有哪个假装是"全局总额"。
- **可能方案**：给按客户端表加一行 `(no client_key)`，或在表下注明"另有 N 个请求无 client_key、未计入"。前者更好，但要动 `ByClient` 的分桶键语义。
- **触发条件**：读者拿按客户端合计和 §0 的总额对账，发现对不上。

#### 2.58b [低] §2 的费率溯源只到聚合级，单行看不出走的是哪一层

- **现状**：`report.Pricing` 摘要只给「本次用了哪些定价来源」的总数（标准表生成日期 / supplement 路径 / override 条数 / 别名条数）。单行 `$` 看不出它走的是标准表、补充表、账号覆盖，还是**厂商优先级替代**（一个转售 provider 用了第一方的刊例价）。厂商优先级上线后这条更值钱了——替代发生得更频繁。
- **当前缓解**：`§2` 免责声明写明整章是「按量计费等价成本、按第一方刊例价」；`vmr check` 的 `pricing_table` 行显示别名条数。
- **可能方案**：`pricing.Resolve` 的返回值带上来源标记，穿到 `report` 的行结构，`§2` 加一列。不是小改动，等额度看板那批一并做。
- **触发条件**：读者需要逐行判断某个金额可不可信。


#### 2.64 [低] 「上下文有效利用率」在语料级呈现双峰退化

- **现状**：`internal/story/metrics.go` 计算的「上下文有效利用率」（Context Utilization）在实际语料（111 个样本）中高度双峰退化：约 21% 样本值为 0（无工具调用或单轮任务），约 32% 样本值为 1.0（全工具结果均被后续轮次不同程度引用），中间值稀疏。导致语料统计中的「均值 70% / 中位数 95%」缺乏统计区分度，HTML 看板的「100%」亦难以提供有效洞察。
- **当前缓解**：`vmr analyze -corpus` 统计时需结合分布形状（P10/P50/P90 及两端样本数）共同解读；暂不重定义指标语义以维护 v1-complete 稳定性。
- **可能方案**：细化有效引用粒度（如按实体引用率加权或按 token 深度衰减）或按任务类别（含/不含工具调用）分桶展示。
- **触发条件**：后续版本计划重构行为指标语义时统一评估。


#### 2.66 [已解决] 分析半区的 usage 解析协议参数全链路贯通

- **现状**：`internal/report`（`session.go`）、`internal/ctxgraph`（`manifest.go`）、`internal/reqdetail`（`detail.go`）与 `internal/replay`（`replay.go`）均已全部贯通 `ExtractUsageWithProtocol` / `MergeUsageWithProtocol`，显式传递 `protocol` 参数，消除了字段存在性猜测导致的 In/Cache 统计口径失真。
- **验证**：已在 `manifest_test.go`（`TestBuildManifest_UsageProtocolAware`）与 `helpers_test.go`（`TestExtractUsage`）中对齐了 Anthropic 与 OpenAI Responses 协议的差异化用例。

#### 2.67 [低] Anthropic 侧的 usage 侧别判定对不吐 `message_start` 类型标记的兼容网关 fail-open

- **现状**：`respnorm` 现在按 SSE 事件分别记录 in/out 两侧的 usage 是否见过（截断流不再把占位 `output_tokens≈1` 当精确值计费）。侧别判定依赖事件里的 `"type":"message_start"` 标记——含该标记的事件只记 in 侧。
- **边界**：不吐这个标记的 anthropic 兼容网关退回通用判定（占位 Out=1 记为 out 侧见过），即该形态下回到修复前的行为。刻意 fail-open：宁可退回旧行为，也不因为识别不出网关方言就把整条流判成无 usage。

#### 2.68 [低，登记待触发] crosscheck 夹具没有 body-sniffed 的 compaction 记录，字节一致性靠指纹机制间接保证

- **现状**：`cmd/vmr/cmd_story_report_crosscheck_test.go` 的夹具里没有 summarization（compaction）请求，因此“report 与 story 对同一条 compaction 记录渲染逐字节相同的 detail 页”（R72）在该端到端测试里没有直擦形态的覆盖——实际由 report 侧单元测试（`session_compaction_manifest_test.go`）加指纹机制（`renderFingerprint` 折入 m/prev 身份）间接保证。
- **触发条件**：语料出现真实的 compaction 记录后，往 crosscheck 夹具补一条 body-sniffed summarization 记录，让字节一致性有端到端直证。在那之前不构成已知失真——两条直接测试已钉住机制本身。

#### 2.70 [低，登记待触发] `buildRec2` 的 (path, line) join 依赖审计日志追加不变性，无时间戳交叉校验

- **现状**：`internal/report/recextract.go` 把 parse-cache 的 `recordFacts` 与会话分析的 `ReqInfo` 按 (path, line) 配对。审计日志是追加型的（压缩轮转生成新 path、哈希变化自然 miss），常规运维下两侧永不错位；但手工编辑/拼接历史日志会让同一 (path, line) 指向不同记录，配错完全静默。
- **可能方案**：join 前对 `rf.TS` 与 `ri.TS` 做阈值校验（如差 > 1s 记 warning 并跳过 join）。
- **触发条件**：出现「对已归档日志做手工编辑」的运维形态，或用户报告无法解释的指标错乱。在那之前这是加固项，不是缺陷。


### C. 分析半区 · LLM 解读层校准

#### 2.18 [中] Phase 1b 六个 LLM 语义判别器尚未完成完整黄金样本校准

- **现状**：`internal/story/llm_findings.go` 六个判别器已实现、单测覆盖、且用 `_eval/calibrate_p1b.go` 对真实生产日志跑过真实模型验证（6 个真实 Journey 上机械核验 Evidence Anchor 有效率 100%，人工抽查合理）。但不是正式合入门禁——那需 30~50 个 Journey、每模块 ≥6 正/负例的系统性黄金样本集 + 人工标注 Ground Truth 算真实 Precision/Recall。
- **为什么待定**：黄金样本挑选与人工标注是需实际投入时间的判断性工作，无法自动化；当前抽样规模下无需立即处理的误报模式，不构成阻塞。`_eval/calibrate_p1b.go` 已是可直接复用的校准工具，扩大 `-input`/`-limit` 即可推进——**成本在人力时间，不在代码**。


### D. 分析半区 · 展示与产出契约

#### 2.59 [低] `vmr analyze -compare` 两侧 system prompt / 初始指令逐字一致时未合并

- **现状**：`internal/story/render_compare.go` 的 `renderSysPrompt` / `renderInitialInstruction` 无条件各渲 A、B 两份节选（`renderExcerpt` 调两次）。两侧同源（`Changes` 均为 0、节选逐字相同）时，同一段 system prompt 正文在 compare md 里贴两遍，实测占单份 compare 全文约 65%。
- **可能方案**：只做精确相等合并（`sp.A.Excerpt == sp.B.Excerpt` / `f.A.Text == f.B.Text`）——渲一份，标注「两侧此节选一致（截断前缀，不代表完整文本逐字相同）」，A/B 的 tokens+Changes 对比行保留。相似度阈值合并不做（阈值主观）。
- **触发条件**：界限清楚、随时可做；改动会给两个函数各加一个分支，注意 `archtest` per-function 行预算。


#### 2.51 [低] `internal/story/mdlite.go` 行内代码里的 `**` 会在 `<code>` 内注入 `<strong>`

- **现状**：`mdInline` 先 `mdWrap` 处理 `` ` ``、再处理 `**`；若 `-compare -html` 的 LLM 解读段在行内代码里输出 `**`（如 `` `glob/**` ``），第二遍会在已生成的 `<code>` 内注入 `<strong>`。纯展示层轻微瑕疵——`html.EscapeString` 最前置，无 XSS。
- **为什么待定**：真观察到 LLM 频繁触发再微调解析状态机；`mdlite` 只覆盖解读段实际用到的 Markdown 子集这一取舍见 §1.5。


#### 2.6 [低] §2.5 表格的标记符号已达四个

- **现状**：`⭐` 超额度 / `‡` 配置变更 / `†` 无时间交集 / `◇` 部分流量未计价，各配一条按需渲染脚注。信息都必要，但四个符号叠一张表可能已到「标记多到没人看脚注」的临界。
- **为什么待定**：主观展示密度判断，四个标记都按需渲染，健康报表一个都不出现。真实报表读起来觉得吵了再动（`◇` 是最可能降级为纯 JSON 字段的候选）。


#### 2.7 [低] `vmr report` §2 成本表结构化透传 `CostEstimateEst`（方案 ②）

- **现状**：方案 ①（Markdown 口径提示脚注）已闭环。方案 ② 要给 `Row`/`ClientRow` 补 `CostEstimateEst`、改 `rows.go`/`accumulateCost`/渲染层三处，并再次改 `vmr-report.json` 形状。
- **为什么待定**：无明确外部程序消费需求前遵循 YAGNI。


#### 2.22 [低，决定不做] `chatmsg.ToolResultList`/`ToolCallList` 未覆盖 OpenAI Responses API 的 `function_call`/`function_call_output` 形状

- **现状**：`chatmsg.Messages` 已能把 `function_call_output` 渲染成人读文本，但结构化提取层只覆盖 OpenAI Chat Completions 与 Anthropic 两种形状。纯 Responses API 流量下脊柱不展示工具结果、三个 Finding 检测器无证据、`journey-<id>.json` 的 `tool_calls` 会静默报告「这一步没有工具调用」（机读契约降级读者看不出来）。
- **决定不做**：真实语料按 `protocol` 统计 `openai-responses` **0 条 / 0.0%**——一次都没触发过。**触发条件（量化）**：任意一次 `vmr report` 的 `vmr-requests.json` 出现 `protocol == "openai-responses"` 的记录，即重新排期。


#### 2.29 [低，暂不做] `journey-<id>.json` 的 `structure` 字段没有 schema 版本戳

- **现状**：`.parse-cache/` 有 `CacheSchemaVersion`，`journey-<id>.json` 无等价机制——P4 前后生成的旧/新文件字段名相同、形状不同，消费者无法仅凭文件本身分辨。
- **为什么暂不做**：YAGNI + 已裁决「JSON 无外部脚本消费」——`journey-<id>.json` 至今唯一已知程序化消费方是 `_eval/calibrate_p1b.go`（只读 `EvidenceAnchor`）。没有消费者，就没有人需要探测版本。
- **触发条件**：出现第一个 `_eval/` 之外的程序化消费方。加 `schema_version int` 成本接近零，但改在下次新增字段时最便宜。

#### 2.73 [低-中，暂不做] LLM 自由文本的 `<`/`>` 未净化即进 `.md` 产物

- **现状**：`sanitizeMDStruct`（`story/llm.go`）只处理 Markdown **结构**破坏（反引号/竖线/行首标记），不处理 `<`/`>`。LLM 判别器输出的类 HTML 片段会原样进入 `.md` 文件。
- **为什么暂不做**：产物是本地文件，不是 web 渲染面（HTML 侧转义已由 `mdlite` 全量覆盖，`<script>` 进不去）；Markdown 阅读器对裸 `<...>` 的降级仅是显示瑕疵。
- **触发条件**：产物开始被 web 化渲染，或出现把 `.md` 直接转 HTML 的新消费方——届时在转换层做 HTML 转义，而不是提前在数据层碰文本。


### E. 分析半区 · 新能力（视图 / 导出 / 信号）

#### 2.61 [低，结构性缺口] 无「任务是否达成目标」信号

- **现状**：VMR 零埋点前提结构性地拿不到「任务是否真正达成目标」，`GroupComparison` 只能拿「耗时」当代理（Analytics 设计文档已述为已知限制）。
- **可能的弱代理**：最后一条 assistant 回复是否含完成确认措辞、是否有 deliverable 落盘（`DeliverableFact` 已在 compare 里算）、plan 的 checkbox 是否全 check（`plan_parse.go` 已解析）。拼起来可给「疑似完成 / 疑似未完成 / 无法判断」三态弱信号。
- **为什么待定**：价值最高、也最容易做错——一个不准的「成功率」比没有更糟。要做需专门的设计任务先论证各代理信号的假阳/假阴代价，不能仓促上。


#### 2.60 [低，需独立设计] 缺跨时间窗对比分析

- **现状**：`-journey` 单条、`-compare` 双条、`-corpus` 跨 journey 统计，宏观报表是单时间窗快照——没有「同一指标 7 月 vs 8 月」或「某改动实施后改进多少」的视图。§5 有按日活动、§2 有按日成本，原料在，缺的是双窗口并排 + 环比。
- **可能方案**：`vmr analyze -compare-period A..B vs C..D`，复用现有 `report` 聚合跑两遍 + 一个 diff 渲染层（形态类似 `story` 的 compare）。「客户 × 月成本矩阵」是同一维度的子集。不要往宏观报表里塞趋势——会让本就长的报表更长。
- **触发条件**：需要量化某次路由/配置改动的效果，或客户成本要按月分摊。立项前先写设计草案。


#### 2.13 [低] 额度燃尽看板未交付

- **现状**：`vmr report` 已有额度与消耗对照子表，更进一步的长期燃尽曲线与预测看板未实现。属产品路线，不与技术债并列排期。


#### 2.62 [低，YAGNI 待触发] 无 CSV / 扁平表导出

- **现状**：`vmr-report.json` / `vmr-requests.json` 是嵌套 schema，pandas / Excel 用户要先 flatten（`RequestRow` 本身已接近扁平）。
- **可能方案**：`vmr analyze -format csv` 导出几张固定扁平表（`requests.csv` / `cost_by_client.csv` / `cost_by_date.csv` / `sessions.csv`），不做「万能 CSV」。
- **触发条件**：出现真实的表格工具消费需求——与 §2.29 同口径，无消费者就没人需要。


### F. 路由半区 · 配额与请求路径

#### 2.52 [低] 虚拟模型级预算硬闸（E3）未做

- **现状**：quota 的 gate/bucket 是「配速」——从不拒绝请求，只在同优先级梯队内重排端点。E3 想要的是**硬急停**：进死循环的 agent 触顶后请求被**明确拒绝**（可解析错误，绝不静默降级到便宜模型），每日零点 + 进程重启重置、不引入持久化。两者目标不同——配速降低「跑爆某套餐」概率，硬闸给「一夜烧光」设确定性上限。
- **为什么待定**：用户 hold。真要做需一个独立的内存态机制（仿 `health.Registry`，请求入口查一次），不是拧 quota 旋钮能得到的。


#### 2.85 [中，需先设计] 半开恢复的深度退避解除策略：低流量/单候选部署恢复尾延迟可达分钟级

- **现状**（2026-09-03 核实）：`health.Classify` 对 `s.fails > 0` 的端点恒返回 `available=false`——只发后台探针、绝不给实流量；`ReportProbeSuccess` 每次成功仅 `s.fails--`。一个退避到 `fails=5` 的端点即使上游已恢复，也需 **5 次独立的后台探针先后成功**（每次由一个恰好排到它的真实请求触发）才 `fails` 归零、实流量回归。`buildCandidates` 的 `ctxFallback` 只回退到健康过滤之后的 `hardFiltered`，**没有「候选全空时放行退避最浅端点」的 last-resort**。
- **影响面**：高流量 + 多候选下缺口窗口是数个请求间隔（秒级）、由 failover 兜住，几乎无感。**低流量 / 单候选 / 全候选同时退避**的部署下，恢复尾延迟可达分钟级（探针频率 ≈ 请求到达率），期间客户端收 503。
- **注释已订正**：`health.go` 的 `ReportProbeSuccess` doc 曾写「…or a real request completes and ReportSuccess zeroes the count」——`fails>0` 期间实流量到不了该端点，此分支不可达；已改为如实描述（2026-09-03）。原 §1.1「探针成功与真实失败交替时深度在两档间振荡（2↔3）」同样不准确：能到达 `fails>0` 端点的只有后台探针。
- **可能方案**（需设计草案，有 flap 风险）：(a) 探针成功 1 次即允许该端点参与常规路由（保留 `fails` 深度），后续真实请求失败则在原深度继续退避、成功则清零——把「探针是弱证据」体现在「只给一次尝试机会」而非「压着不放行」；(b) `buildCandidates` 增一条「候选全空且存在仅半开（非硬冷却）端点时，放行退避最浅的一个」的 last-resort。
- **触发条件**：真实用户在低流量 / 单候选部署报告「上游已恢复但 vmr 仍 503 数分钟」。


#### 2.86 [低，需先设计] `respnorm` 初始 `modeUndecided` 扣留保活帧，慢/排队上游 + inline-content 协议下客户端可能读超时

- **现状**（2026-09-03 核实）：SSE 流初始处于 `modeUndecided`（为侦测 MiniMax 的 inline-think 形态），首个「payload-bearing」事件（`content`/`text` 非 `<think>` 开头，或 `tool_calls`/`partial_json`，或专用 `reasoning_content`/`thinking` 字段）到达前，所有事件——含 `message_start`、role marker、`event: ping` 保活帧——全部囤在 `s.pending`。`bufferedCap`（8MiB）几十秒也到不了（ping ~30B/个）。
- **影响面**：常见情况首个 content delta 在 role 后几十 ms 到达、扣留可忽略；Anthropic extended thinking 的 `thinking_delta` 立即放行。**窄场景**：上游排队 / 慢推理模型在 `message_start` 后长时间只发 ping、无任何 content/thinking/tool delta，超过客户端读超时。
- **可能方案**（触及核心四态机，需评估与 buffered/opaque/截断三分支的交互 + 完整回归）：在 `decide()` / `ingest` 的 undecided/buffered 路径识别保活帧（`event: ping`、SSE 注释行 `:`）并立即旁路 `s.out`，不参与 decide（ping 无 content、无 model 字段，早发对 SSE 语义无害，与 MiniMax buffered 不共存）。
- **触发条件**：真实用户报告「用某慢上游时流式响应假死后被客户端超时切断」。


#### 2.89 [低，登记待触发] `core.Endpoint.PricingRate` 持 `*PricingSpec`，热路径每笔 cost 请求重跑一次链式解析

- **现状**（2026-09-04 review 发现）：`core.Endpoint` 挂 `*core.PricingSpec` 而非折叠好的 `Rate`，`router.ChargeResponse` 的 cost 分支每笔请求 `pricing.EffectiveRate(ep.PricingRate)` 重跑一次 `resolveChain`（≤3 元素切片的递归下钻）。P0-A 移除时间维后 `EffectiveRate(spec)` 对给定 spec 是纯静态确定性函数，`BuildSnapshot` 时端点已细化到唯一 upstream model，完全可在加载期折叠为一个已解析 `Rate` 挂到端点上、热路径变字段直读。
- **性质**：`resolveChain` 是纳秒级、相对一次上游 HTTP 可忽略——不是性能问题，是「让价目表进实时路由热路径」这条架构红线（§1.0）的一处轻微越界。（曾叠加 `core.go` 注释「加载完成后也不能折叠为静态 Rate」的误导，2026-09-04 随 review 收口订正——「不能折叠」只约束 Resolve 期逐规则折叠，P0-A 后 spec 全静态、`EffectiveRate(spec)` 是纯函数，整链本可在 `BuildSnapshot` 预折叠为单个 `Rate` 挂 `Endpoint`；本条只剩折叠本身。）
- **可能方案**：`BuildSnapshot` 折叠预解析 `Rate` 挂 `Endpoint`；`core.PricingSpec` 的 `Base + Overrides` 收进 `internal/pricing`（离线 `Resolver` 的 memo 同样只需缓存 `Rate`）。
- **触发条件**：`core` / `router` 有其它改动顺路做，或 profiling 真的把它标出来（不太可能）。

#### 2.90 [低] 周期长度相同的多条 Limit，`BucketIndex` 的桶/闸角色取决于 YAML 书写顺序

- **现状**（2026-09-04 review 发现）：`internal/quota/score.go` `BucketIndex` 用 `nominalUnitHours` 严格大于挑最长周期。一个 provider 同时配了共享池 `every: 1mo` 和某模型专属池 `every: 1mo`（合法共存）时，两者名义时长相等，排在配置数组前面的被选为桶、后面的被选为闸——仅仅上下颠倒两行就会互换角色，缺确定性仲裁。
- **可能方案**：周期时长并列时加次级裁决——共享池优先为桶（`!PerModel` 优于 `PerModel`），同类则 `Amount` 大者为桶。
- **触发条件**：真实配置出现等长复合 Limit 且用户报告角色不符预期。当前可靠规范书写规避。

#### 2.91 [低，F4 修复后已缓解] `Registry.rollbackWarned` 是进程级一次性 latch，误触发后真实时钟回退永久静默

- **现状**（2026-09-04 review 发现）：`internal/quota/quota.go` `resetIfStaleLocked` 的 `rollbackWarned` 一旦置位便无重置机会。F4（`ChargeCost` 单锁原子）落地前，周期切换瞬间晚到的估算写可能携旧 `periodStart` 误触发一次时钟回退分支，此后宿主机真实 NTP 阶跃/快照回滚/时区误配都不再有 WARN。
- **现状缓解**：F4 已消除那条跨锁裂缝，spurious trip 的主要来源没了。
- **可能方案**：按 `limitKey` 或时间窗去重抑制，而非进程生命周期一次性 latch。
- **触发条件**：F4 之后仍观察到本该有的时钟回退 WARN 缺失。


#### 2.17 [中] `imgprep` 解码闸门按「防炸弹」设定，其内存上界与单请求内存预算差一个数量级

- **现状**：`processImage` 在 `image.Decode` 前用 `image.DecodeConfig` 只读头取宽高，声明尺寸 > `maxDecodePixels`（**代码值 16MP**，`internal/imgprep/imgprep.go`）直接放弃降采样原样透传。**闸门存在且工作正常**，目的是拦解压炸弹。问题在阈值量纲：16MP 按 RGBA 约 64MB/次解码，而 UserGuide「单请求内存预算」核算的是 ~32MB/请求——两个数字各自都对，回答的不是同一个问题（「多大算恶意」vs「一个请求该占多少」）；16MP 下差距是 2×（早先 64MP 时是 8×，论证不变、倍数收窄）。图片逐张解码逐张释放，多图不累加。
- **可能方案**：为内存预算再设一道更低的、可配置的闸门。
- **为什么待定**：够到 64MP 需刻意构造，正常截图/照片低一到两个数量级，无实测显示真实负载下造成过内存问题；且方案自带「用账单换内存」的取舍，不能替用户默认决定。零风险的一半（UserGuide/.zh + `config.example` 注释写明这段峰值由像素数决定、逐张释放）已落地。


#### 2.48 [低] 错误分类词表的长期形态：端点级 quirk 统一模块（含 sticky 降级优化）未做

- **现状**：vendor 知识散在 `DefaultClassify` 的全局词表里（`contentHint`/`contextLimitHint`/`upstreamHint`/`vendorQuirkHint`/`authHint`，约 30 词）。已知厂商专属误判已由 `ErrQuirk` 类 + 词条修复覆盖（见 §1.1），词表之间尚未互相干扰。
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

- **现状**：`internal/router/probe.go` 的健康探活请求不写 `audit.Record`，`vmr report` 看不到探活消耗。
- **为什么待定**：探活消耗极低；且需先明确探针流量在报表中的呈现口径，避免污染业务 SLO 统计。


#### 2.49 [低，非活跃——仅 32-bit] `imgprep` 解压炸弹守卫的像素乘积在 32-bit `int` 平台可溢出

- **现状**：`processImage` 用 `cfg.Width*cfg.Height > maxDecodePixels` 挡炸弹（`cfg.Width`/`Height` 是 `int`）；32-bit 平台两值接近 `int32` 上限时乘积回绕成小值绕过守卫。
- **为什么非活跃**：Go `image/png` 把 IHDR 宽高钳在 `int32`，64-bit（唯一 CI/目标平台）乘积 ≤ (2³¹)² < `int64` 上限，不可能溢出。
- **修法（触发时）**：`int64(cfg.Width) * int64(cfg.Height)`，一行。**触发条件**：32-bit 成为受支持的构建/部署目标。

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


### G. 工程工具与运维入口

#### 2.54 [低，暂不做] `/help.html` 页内发测试请求

- **现状**：`/help.html`（`internal/server` 里的 `help.html` + `help.zh.html` 双语内嵌页，各 ~1550 行）已有一键复制各 Agent 接入片段、鉴权弹窗、`fetch('/status')` 健康展示。增长战略文档提过「页内直接发一次测试请求看命中节点/延迟」——`X-VMR-Endpoint`/`X-VMR-Attempts`/`X-VMR-Route-Reason` 响应头已具备，技术上可行。
- **为什么暂不做**：真实工作量约 1–1.5 人日（双语内嵌 HTML 锁步、真实计费请求、streaming、错误面、虚拟模型选择器、内嵌 JS 无 Go 测试）。这是 onboarding 漏斗功能，被 `vmr init`/`vmr connect`（战略文档第一梯队）完全压制——真做漏斗应先做那两个。战略文档里本就列在第三梯队。2026-08-30 增长打磨批次评估确认延后。


#### 2.8 [低] `archtest` 函数长度豁免的键无法区分同文件重名方法

- **现状**：`funcLineExemptions` 以 `文件:函数名` 为键，同文件重名方法共用一条（如 `report/ingest.go` 6 个 `Ingest`）。今天全部远低于默认限额，无影响；一旦为其一登记豁免，其余会一并放宽。
- **可能方案**：键改 `文件:接收者类型.函数名`（`ast.FuncDecl.Recv` 已有类型信息）。
- **为什么待定**：需真的出现一个必须豁免的重名方法才有意义。

#### 2.79 [低] 弃用别名 `vmr story` 的 flag 校验宽于 `vmr analyze`

- **现状**：迁移期别名 `vmr report`/`vmr story` 与主入口 `vmr analyze` 的 flag 集合存在漂移，个别 flag 别名仍接受而主入口已收紧——方向反了：别名应窄于或等于主入口，否则用户按别名写死的脚本在迁移后会突然不可用。
- **可能方案**：别名入口复用主入口的同一套 flag 解析，差异只在弃用提示。
- **拆除触发条件**（对照 `legacy_protocol.go` 的兼容咽喉有明写前提，别名层也应有）：`vmr report` / `vmr story` 这两个别名整体拆除的前提是「CHANGELOG 宣告弃用后满一个次要版本，且当前已无脚本/文档引用」。在那之前，flag 解析收敛到 `vmr analyze` 同一套（差异只在弃用提示）随下一次碰 CLI flag 层时做。

#### 2.80 [低] `sysinfo` 把系统调用失败折叠成 0，违反「missing is not zero」

- **现状**：`internal/sysinfo` 的 `DirTotalSize`/`DiskFreeBytes` 在目录不可读或调用失败时返回 0——与「磁盘真的满/目录真的为空」在返回值上不可区分，状态看板可能把「读不到」显示成「用量为零」。
- **为什么待定**：消费方是本地状态展示，错误折叠的误导面小；真正「missing is not zero」的纪律挂在会进报表与配额决策的数字上。
- **可能方案**：返回 `(value, ok)` 并让消费方显式展示 unknown。
- **触发条件**：状态看板数字开始参与任何自动决策（而不仅是人看）。


---


## 3. 优先级总览（替代原 ROI 评估总表）

> 原「ROI 评估总表」已废止：逐条的成本 / 风险 / 价值本就写在 §2 各条目自己的「可能方案 / 为什么待定 / 触发条件」里，
> 另立一张表维护第二份口径，正是这份文档最反对的重复——而且原表只评了 22/30 条（§2.54–§2.56、§2.60–§2.62 从没进表）。
> §2 的组内顺序就是按用户价值 × ROI 排好的，本节省略结论与跨组排期。

- **全局结论**：待办里没有「价值高、成本低、却一直没做」的异常。值得优先投入的集中在四类：大语料规模（§2.2 看触发、§2.1 已证 5.2×）、LLM 解读层校准（§2.18，成本在人工标注）、路由配额（§2.52，用户 hold）、产品路线（§2.13）。
- **多数条目不是「不值得做」，是「收益未经测量」**：§2.2 / §2.3 / §2.7 / §2.10 / §2.17 的共同点是收益尚未实测——而先做优化再测量正是这个项目一贯拒绝的顺序；触发条件到了先测再说。
- **跨组怎么排**：
  - 立即可做（界限清楚、随时可做）：§2.59 compare 同源节选合并。
  - 触发即做（成本主要等触发，触发条件写在条目里）：§2.2（语料 > 约 3 万条 / RSS > 4GB）、§2.55（语料再涨约 5 倍）、§2.56（时间成首要痛点）、§2.51（观察到 LLM 频繁在行内代码里输出 `**`）、§2.48（词表互相干扰 / sticky 往返可观测）、§2.57（脚注不够用）、§2.58（主力上游长期无价）、§2.18（黄金样本窗口）。
  - 需要先设计（价值高、易做错，禁止仓促）：§2.61 任务达成信号、§2.60 跨时间窗对比。
  - 明确不做（各有量化触发条件，触发即重估）：§2.22 / §2.29 / §2.49 / §2.50 / §2.3 / §2.62。
