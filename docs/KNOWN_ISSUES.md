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
- **自动化基线**：`go test ./...` 与 `go test -race ./...` 全绿；`internal/archtest` 强制导入单向边界、文件/函数行数预算、文档引用完整性。
- **§2 分布**：高危 0、中危 3（`2.2`/`2.17`/`2.18`）、低危 27，合计 30。无 `[中低]` 条目。
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
- **厂商专属协议约束拒绝归 `core.ErrQuirk`，不复用 `ErrContextLimit`**：DeepSeek 思考模式要求回传 `reasoning_content`、Google 要求回传 `thought_signature` 这类「换个端点就好」的拒绝，`DefaultClassify` 归入专门的 `ErrQuirk`（切换 + 零冷却）。复用 `ErrContextLimit` 能得到相同的 failover 行为，但审计标签会说谎——这不是上下文超限。OAuth 标准错误码同理独立归 `ErrAuth`。此前三类都落进兜底 `ErrClient`（永不 failover）而中断重试。全量端点级 quirk 模块方向见 §2.48。
- **`/status` 端点项刻意不加端点级累计计数器（requests / ok / failed / tokens）**：`consecutive_failures` 出现在 `/status` 因为它是**当前健康状态**读数（liveness 视图）。端点级累计账是**分析半区**职责——`internal/report` 的 `EndpointRow`（Attempts/OK/Forwarded/Failed/Availability/ErrorClasses/tokens/cost，含 by-date 与 cross-date）已完整产出，数据源是可持久化、可按时间切片的审计日志。给 `/status` 塞一份进程内、重启即失的实时副本：① 与分析半区双账本（正是「一个分析数字复现一个路由数字必须差分测试锁定」要防的负担）；② 做全（4–8 个计数器 × N 端点）等于给 `router.Telemetry` 加一张按端点的动态 map，破坏它「全固定原子、热路径零 map 零锁」的设计。
- **`system.disk.free_space` 在 Windows 上是桩（恒 0）**：`syscall.Statfs` 无 Windows 等价物，而 Windows 不是目标部署平台。
- **`/log` 慢订阅者以「丢行 + 标记」处理，永不让日志热路径阻塞**：每订阅者一条有界 channel（64 行），满则丢行插 `... dropped N lines ...` 标记；`log.html` 不做自动重连（只手动重试按钮），避免重启风暴下的重连洪水。
- **启动 banner 与 panic 直写 stderr，tee 不捕获**：banner 只出现一次，panic 时进程将死，两者都不值得为 `/log` 引入第二条写入路径。

### 1.2 配置与协议

- **协议枚举 2026-08 重命名为 `openai-completions` / `anthropic-messages`（`openai-responses` 不变），与 Pi Agent 等对齐**：全链路一步到位用新名，路由侧零兼容负担。**唯一兼容咽喉点**是 `audit.Record.UnmarshalJSON`：读到旧名经 `core.CanonicalProtocol` / `core.NormalizeEndpointLabel` 归一化，只服务分析侧读历史日志；`vmr replay` 不做兼容。`ctxgraph.CacheSchemaVersion` 已 1→2 使旧事实缓存失效。**这是「版本必须匹配、不做兼容」原则的唯一刻意例外**——历史审计文件是不可变的既存事实。config 仍带旧名时加载错误直接点名要改成什么（`internal/config/provider.go` 的 `unknownProtocolHint`），但 parser 不接受旧名（strict YAML）。**TODO(2026-10)**：过渡期约一个月后拆除 `core.CanonicalProtocol` / `core.NormalizeEndpointLabel`（`internal/core/protocol.go`，常量保留）、`Record.UnmarshalJSON` 及其为此新增的 `internal/core` import、两个 legacy-name 归一化测试；`ctxgraph.CacheSchemaVersion` 保持 2 不回退。
- **CLI 与 Server 版本必须匹配，不一致直接报错不做兼容**：单二进制、可随时重启，`vmr status` 与 `vmr start` 理应同版本——不一致说明升级没走完，报错正是暴露它。`json.RawMessage` 式兼容层只覆盖一个滚动升级窗口却永久留在代码里，违反 KISS。曾为「旧 server 缺失新 key」保留的 `serving *bool` 兜底已作为死代码删除（`instance.config` 由 string 改 object 后即不可达）——版本必须匹配的原则不再留任何字段级例外。
- **`/status` 的 `instance.base_urls` 回显请求自身地址而非 `listen` 配置**：host 取自 HTTP Host 头、scheme 取自是否 TLS——调用方用什么地址访问 `/status` 就广告什么地址，这正是客户端该填的值。纯展示、不参与鉴权或路由，Host 可伪造无安全影响；刻意不做 `X-Forwarded-Host` 解析。
- **环境变量未定义时静默展开为空串，不支持 `${VAR:-default}`**：保持配置解析简单明确，默认值在 YAML 里显式写出。
- **`internal/config` 的三层费率解析不后置到 `router.BuildSnapshot`**：`config` import `pricing`、在 `validate()` 跑完解析，看似「配置层反向侵入用例层」，但这是 Quota 设计文档决策表明文选定的方案——「只让 report 一侧解析、`metric: cost` 另开一条运行时校验路径」是同一行里已否决的备选（两份实现容易漂移）。后置还会摧毁「`metric: cost` 费率不齐 = **加载期**错误」这条硬要求。
- **多协议适配器（`adapter/{openai,anthropic,openairesponses}`）保持独立子包**：三协议底层已有真实分叉（Anthropic 529 特判、Responses 顶层 `input` 数组与 `RewriteInputRoles`、`x-api-key` vs `Authorization`）；独立子包支持编译期 `init()` 注册与独立单测，新增协议零侵入。合并成参数化结构体只是把多态改写为字符串 `if` 分支。
- **不引入端点级通用运行时 quirks 插件系统**：坚持编译期确定性，只对已证实的厂商行为差异做受控修复。
- **不合并 `Dimension`（排序）与 `Condition`（淘汰）**：淘汰依赖请求事实，排序只比较端点属性，职责分离保证接口纯粹。
- **ProviderGroup 的多 Key（`api_keys:`）已实现，运行时均衡与分级 Failover 仍不做**：早先设想的运行时 KeyPool（请求期在池内随机选 Key）会违反 `core.Endpoint` 「构造后不可变、`HealthKey()` 只算一次」这条贯穿 health/sticky/quota 的不变式。实际落地是「配置期展开成多个独立 `core.Endpoint`」：`Provider.APIKeys`（`{label: key}`）在 `config.Parse` 里展开成 `<name>-<label>` 命名的独立 `Provider` 并就地重写引用，下游全部按 `Provider.Name` 字符串解析、零改动。当初设想的三处工作，前两处被这个展开形状架构性绕开（均衡：谁排第一不可预先指定，只能读 `vmr check` 的实际展开结果，没配 quota 时排第一的吃全部流量；配额聚合：每把 key 独立 Provider 名、独立 quota 池，对齐难题不存在了），第三处（分级 Failover：402 跳 Key / 5xx 跳 Provider）维持原判，留到看到真实需求。

### 1.3 校验与防御性编程

- **`/status` 的网络可达性与身份认证解耦，且复用聊天入口的同一把 `api_keys`**：网络范围由 `listen` 决定，认证由 `api_keys` 决定——未配 `api_keys` 时任何能连到端口的人都能读 `/status`（2026-08-23 的显式决策，替代旧的 loopback-only）。这把 key 同时是管理凭证：持有客户端 key 者能看到全部端点名、provider 身份、quota 消耗与配置路径。对单人/小团队代理这是正确的简化。`config.Check()` 对「非 loopback 且无 api_keys」给 warning。
- **`vmr status -addr` 回退读取本地 config 的 `api_keys[0]` 并发送到目标地址**：`-addr` 显式指向别的实例时，把本地 key 当 Bearer 发过去。设计意图是让 `./vmr.sh ps` 对本机多实例免手工传 key；只发 key、不进 URL 或日志。目标地址是使用者自己敲的，不是网络层漏洞。
- **看板（`/status.html` / `/log.html`）把 API key 存 `localStorage`，静态外壳免鉴权直出**：外壳不含数据，数据请求走 `s.auth()`；key 只在浏览器本地持久化，不进 URL、不进服务端日志。所有配置派生字符串内插进 `innerHTML` 前均 `esc()` HTML 转义。`/log` 输出 `text/plain` 而非 SSE/JSONL（源头已是格式化文本）；无查询参数（回放窗口固定 512 行缓冲）。
- **`/help.html` / `/help.zh.html` 的 Agent 配置片段在浏览器就地装配，不做服务端模板渲染**：`/help` 按架构必须公开免鉴权，服务端渲染会逼它强制鉴权、或让服务端拿不到用户 Key。API Key 复用 `localStorage['vmr_status_key']`。服务端下发的 HTML 保留写死默认值（`coding` / `claude`、200k context、`high` effort），保证无 JS / 未鉴权时也自洽。四点取舍：max-output 预算按 context 分档经验估计（VMR 无模型级元数据）；片段一律 vision-on（空 capabilities = 不受约束）；四个列表型生成器只枚举 `openai-completions` 模型；无浏览器 JS 测试基建，`TestHelpPage_SnippetFillEngine` 只做构建期字符串守卫。
- **`nil` 校验只加在跨包公共入口且一律 fail-fast，绝不静默兜底**：已加的是 `report.AnalyzeSessionsCached` 与 `story.BuildChain`/`BuildAll`/`PreviewTitle`/`PreviewTitles` 五个入口——判据是「跨包公共 API + 后接并发扇出或递归组装」。包内被这些入口保护的函数不重复校验。
- **`fmtutil.DisplayZone` 保持裸 `var`，不封装线程安全访问器**：生产代码零写入点——全仓写入全在 `_test.go` 且相关测试无 `t.Parallel()`，`-race` 全绿。「让测试能确定性覆盖」本就是它存在的理由之一。
- **尤其不做「`prof == nil` 就回退到 `Generic`」这类静默兜底**：`OpenClawAware` 与 `Generic` 给出不同的任务标题与边界，静默换一个 Profile 会产出一份错误但看起来正常的分析结果，比 panic 难查。
- **`.parse-cache/` 不做分片孤儿回收 GC**（原 1.27）：`ctxgraph.SaveCacheDir` 只增量写入当前存在的分片，不主动删旧 hash 孤儿分片。缓存是完全可再生的派生产物，`vmr report`/`vmr story` 均可从空缓存目录冷启动。触发条件：`.parse-cache/` 体积超过同批压缩审计日志总体积（当前实测 51MB vs 177MB），或升级后异常磁盘占用；在那之前「整目录删除重建」比任何 GC 更简单可靠。
- **默认分析套件不物化 `details/`，`report` 的「文件」列判据是文件存在性而非 `-details` flag**：`writeJourneyFile` / `renderJourneys` / `renderAllJourneys` 带 `materializeDetails` 入参——只有单条下钻、`-compare`、`-render-all` 传 `true`；默认套件的脊柱「→ detail」与 sysprompt 指针渲染成行内 `文件:行` 坐标（`Manifest.Req` 的纯函数），不写盘、不留 404 链接。`report.detailCell` 因此不能只看本次的 `-details`：`vmr analyze` 先跑 story 半区（可能已批量物化）再跑 report 半区，纯 flag 判据会谎报「没写详单」或反之——改查 `r.DetailFile` 是否真实存在（一次 `os.ReadDir` 建 set）。常驻守卫测试盯着「默认套件 `details/` 为 0、指针是坐标非链接」，人为改回无条件物化当场失败。这条纪律反复退化过四次，这次靠测试锁死。

### 1.4 包边界与依赖

- **`imgprep.ImageInfo` → `audit.ImageInfo` 的字段拷贝**：换 `imgprep` 不依赖 `audit`，保住公共工具包零依赖边界。
- **`chatmsg.ReassembleSSE` 与 `respnorm` 的 SSE 状态机保持分离**：前者面向离线完整语义提取，后者面向在线字节级保真转发，关注点不同。
- **`internal/report/cost.go` 的端点标签切分不并入 `core.SplitEndpointLabel`**：后者兼容 `:` 与 `/`，前者只认 `:`。放宽 `$` 成本估算那个调用点会改变旧格式日志的历史报表金额——一次需单独评审的行为变更，不是「统一实现」的顺带产物。
- **`core.StickyBackstopTTL` 不迁回 `internal/sticky`**：迁回制造一条 `config` → `sticky` 的新依赖边，仅用于读一个常量；不做这个校验则 `sticky_ttl` 超过 backstop 的配置会「看起来被接受、实际静默失效」。
- **`adapter` 的协议字段字面量（`"model"`/`"stream"`/`"messages"`/`"input"`）不从 `jsonscan` 导出复用**：它们是不可变字节常量而非共享状态；「知道这些字段名的含义」正是把 `SessionFingerprint`/`TopLevelProbe` 留在 `adapter` 的领域知识，也是「需要具体字段名的函数不属于 `jsonscan`」这条规则的由来。
- **不把分析半区拆成独立二进制**：坚持「单二进制单文件分发」。
- **不引入 DuckDB / cgo 做数据聚合**：保持纯 Go、跨平台零 C 依赖。
- **`i18n` 的 26 个微文件不合并**：与 `internal/report/section_*.go` 的「一节一文件」硬规则一一配对（`archtest` 强制），合并击穿 700 行全局预算，且改一节文案从打开小文件变成在大文件里找。
- **`i18n` 的 `type XxxText` + `if lang == ZH` 样板不改写成 `map[Lang]T` + 泛型 `pick`**：改写只消掉每文件 2 行分支，占体量的 struct 定义与两份字段赋值一行都省不掉，还新引入泛型 helper 与「key 缺失怎么办」。收益为负。
- **`internal/probe` / `rundir` / `buildinfo` 不登记进 `zeroInternalDepPackages`**：那张表的语义是「**承诺**永远零依赖」，不是「当前碰巧零依赖的都登记」。`probe` 独立成包是为避免 `diagnose`→`router` import cycle，未来 import `core` 完全合理。（对照：`internal/tokenutil` 承诺永不依赖内部包，已如实登记。）
- **`internal/core/core.go` 不按领域拆成 `endpoint.go`/`quota.go`/`pricing.go`**：同包拆文件不改变任何编译依赖，是代码导航整理不是架构重构。真正解决「core 会不会长成上帝包」的是准入规则，已写在包注释里并对存量逐条复核过。
- **`imgprep` 的 `map[string]json.RawMessage` 不与 `jsonscan` 的字节扫描统一**：图片降采样要重算尺寸并重编码，是深度结构化重写，字节 splice 做不到。这是三个 sanctioned deviation 里最大的一个。
- **不向 Clean Architecture 四层同心圆靠拢做整体重构**：要把横跨环边界的包「归位」就得为满足图示而拆包插接口，代价是新的包边界与一层不解决任何真实问题的间接性。项目已有更强且**可执行**的架构模型（两半区 + `archtest`）。反证：`internal/config` import `internal/adapter`（校验期需知道协议注册表）按 CA 是「外环依赖内环」的合法边——CA 本就不是这个项目合适的透镜。
- **不对 OpenAI 工具返回做 `error:` 关键字模糊嗅探**：实测全量生产语料 495,672 条 OpenAI 工具调用结果，结构化 JSON 错误字段 0 条（0.00%），全部是自由文本 stdout/stderr。子串模糊嗅探会引入海量代码输出/测试用例的假阳性。只对协议原生结构化错误标记（如 Anthropic `is_error`）做确定性统计。
- **`go.mod` 保持裸模块名 `vmr`**：改名要动全项目 import 路径，无实质收益。
- **模型/端点展示面的一致性靠统一口径 + 契约测试，不靠共享结构体**：运行时视图以 `/status` 的 `models` 数组为唯一权威（`vmr status` CLI 与 `status.html` 直接消费同一 JSON）；人类可读模型标签 `"<name> [<protocol>]"` 只在 `core.ModelLabel` 一处定义。刻意不统一的三处：`/v1/models`（协议面 schema）、`vmr check` 的分层 config 视图（看配置缺口）与 `/status` 的聚合运行时视图（并集/最大值）、`vmr diagnose` 的扁平 Result 数组。`/status` JSON 形状由 `internal/server/admin_status_test.go` 契约测试锁定。

### 1.5 产出与工程惯例

- **用 Go 结构化代码而非 `text/template` 渲染 Markdown**：复杂条件列、对齐与动态脚注在 Go 里更容易保持类型安全和可读性。
- **不维护外部贡献者 `CONTRIBUTING.md`**：与小团队运作方式不匹配。
- **分析产物 ZH 术语的 loanword / 全译两套约定并存，刻意不统一**：Markdown/报表侧保留英文特性名 + 中文描述词（`§6.5 Sticky 有效性`、`§6.7 Compaction 还原`、`§2.5 账户（Provider）消耗与额度`，journey 叙事正文里 `system prompt` 也一贯是外来词）；HTML 看板侧全译（`系统提示词` / `上下文压缩`）。两套各自内部自洽。全量统一要改约 15 处 i18n 字符串 + 发给 LLM 的 prompt 正文 + `UserGuide.zh.md` / Analytics 设计文档里的既有章节名，收益纯观感、还牵出「Compaction 该不该译」之争（类比 `prompt cache` 通常不译）。**触发条件**：同一 section 内出现自相矛盾的形态（如标题译、紧邻正文不译），才值得局部收敛。新增 i18n 字符串时跟随同 section 已有正文的形态。
- **`internal/story/mdlite.go` 只覆盖 `-compare -html` 的 LLM 解读段实际会用到的 Markdown 子集**（ATX 标题、段落、无序列表、GFM 竖线表格、`**粗体**`、`` `行内代码` ``——全部先转义）：`-compare` 的 LLM 提示词明确要求「结论句 + 候选根因表 + 三个三级小节」，围绕这个形状裁剪。有序列表与围栏代码块落进段落分支（已转义、无注入、不丢字符）。不引 CommonMark 解析器。已知瑕疵见 §2.51。
- **索引折叠与默认渲染范围只把 `heartbeat` 归为噪声，不含 cron / subagent**（`story.IsNoiseCategory`）：真实语料实测——heartbeat 每候选最多 7 请求（107 个候选无一到 10），而 cron 与 subagent 都有双位数请求的候选，含全语料最长的一条 journey（subagent，91 请求）。索引显示分割与 CLI 默认渲染范围共用这一个判据，避免二者对同类候选给出不同答案。
- **`archtest` 的文档守卫不扩展到 review 报告类文档**：守卫只覆盖 `CLAUDE.md`、设计文档、本文件与用户指南。review 报告会正当地讨论已删除的文件与「建议新增的 XXX 函数」。真正的风险（一份陈旧 review 被当施工依据）**用定位而非机制解决**：权威的当前状态清单只有本文件。
- **`archtest` 不加圈复杂度检查**：一次只加一个守卫。函数长度预算落地不久，确认不够用之前不引入第二个。
- **`buildinfo` 只输出 VCS commit 哈希，不人工编造语义化版本**：如实反映构建来源。
- **官方用量 API 不预先抽象 `Source` 接口**：YAGNI，等真正接入第一个厂商私有用量接口时再设计。
- **聚合浮点字段在冷/热缓存两次运行间的 1 ULP 级差异不追查、不消除**（原 1.24）：浮点加法不满足结合律的教科书现象，不是可以「修好」的缺陷。唯一该做的事（差分/一致性测试用容差而非逐字节相等）**已经是现状**（`report/e2e_test.go` 用 `1e-6`、`quota_parity_test.go` 用 `1e-9*want`）。唯一需重新当作 bug 的情形：差异远超浮点精度量级，或开始出现在 `cost_estimate` 之外的字段上。
- **文件与函数行数预算线是提醒式绊线，非架构缺陷**（原 1.5）：`internal/archtest` 的文件/函数预算（默认 700 / 120 + 豁免表）是轻量提醒机制，连 Warning 都算不上。未触线前无需焦虑、不需在常规 review 里逐个排查；一旦触线，按职责拆分重构（如 `detail.go` → `internal/reqdetail`、`config.go` 拆出 `apikeys.go`），或逻辑内聚时临时按 +15~20% 调高豁免。
- **可维护性的核心在整体架构与设计复杂度，而非代码行数**（原 1.15）：单人可维护性取决于是否守住 First Principles / KISS / YAGNI、是否消除了不必要的过度设计与复杂分支，而非机械度量行数或两半区体量比。探索性新分析指标优先用外部脚本消费稳定的 `vmr-report.json` / `journey-*.json` 契约验证，确认真实价值后再评估主库实现。
- **LLM 解读层生成结构化 Finding 的准入与置信度契约**：LLM 判别器产出的 Finding 必须强制标记 `Source: "llm_inferred"`、离散置信度（`HIGH/MEDIUM/LOW`）与原文 `EvidenceAnchor`。仅 `HIGH` + 直接证据锚点的项以 Finding（⚠️）呈现并标 `[AI推测]`；`MEDIUM`/`LOW` 降级为参考提示。**锚点运行期强制校验**：`ComputeLLMFindings` 收完全部 detector 输出后逐条 `strings.Contains(真实 transcript, EvidenceAnchor)` 校验，非逐字子串即丢弃。问法严格约束在有证据支撑的事实性问题上（拒绝开放式主观质量打分），守住「揭示事实与过程异常而非冒充裁判」的边界。


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


### B. 分析半区 · 指标与口径正确性

#### 2.57 [低] `computeTimeSplit` 单间隙时间归因无上限，污染 corpus 均值

- **现状**：`internal/story/metrics.go` 的 `computeTimeSplit` 对每对相邻 Step，把「上一步响应落地 → 下一步请求到达」的整段 wall-clock 间隙按「下一步是否 `HumanInitiated`」二分为 human idle 或 `AgentExecMS`，间隙不设上限。跨天/跨周的 lineage 上，一段几十天的空档会整段计入「Agent 执行时间」——`vmr analyze -corpus` 的 `Agent-Side Execution` 因此出现 `Median 8s / Mean 数小时` 乃至 36 天量级的均值。
- **当前缓解**：`-corpus` 指标分布表已加脚注「time 类指标的 Mean 被少数长命 journey 严重拉偏，看 Median/P90」（2026-08-31）。只是免责，没动根因。
- **可能方案**：对单间隙设上限（如 > 1h 归 idle/unknown 而非 agent 执行）。需改指标语义 + 更新 Analytics 设计文档的时间拆分定义 + 差分测试。
- **触发条件**：脚注被证明不够（读者仍据 Mean 下结论），或要把 `NetWorkingMS` / `ModelToToolRatio` 当硬指标用。


#### 2.58 [低] 宏观报表按日成本表的覆盖度隐性依赖当日端点是否在定价表内

- **现状**：`internal/report/section_cost.go` 的按日成本表只列「当日至少有一条记录能解析出费率」的日期；高流量端点（如 `cliproxy`、`bai`）未定价时，整块日期从表里消失，读者看到的是「这些天没有成本」而非「这些天成本未知」。
- **当前缓解**：`internal/i18n/report_cost.go` 的 `ByDatePartialNote` 脚注（「未列出的日期有流量但上游端点无适用费率，成本未知，非零」），当且仅当确有未定价日期时渲染（2026-08-31）。
- **可能方案**：补齐高流量 provider 定价（`internal/pricing` supplement 或 config override），或在报表 §2.5（账户消耗与额度）显式列出「未定价 provider 及其请求占比」。
- **触发条件**：主力上游长期不在定价表内，按日成本表因此长期残缺。


#### 2.63 [低] 宏观报表 §0 top-line 把调度型 workload 计入总量

- **现状**：§0 顶部的总请求数是全量加总，含 `heartbeat` / `dream_diary` / compaction 等调度型流量。想看「排除调度型后的真实交互规模」得自己去 §5「By Workload Class」找 `interactive` 行（`rep.Workloads` / `WorkloadRow.Class`，数据已具备）。
- **可能方案**：不加 CLI flag，在 §0 Summary 表下补一行「其中 interactive：N 请求 / 成功率 / p95」。
- **触发条件**：随时可做；调度型流量占比越高越值得。


#### 2.64 [低] 「上下文有效利用率」在语料级呈现双峰退化

- **现状**：`internal/story/metrics.go` 计算的「上下文有效利用率」（Context Utilization）在实际语料（111 个样本）中高度双峰退化：约 21% 样本值为 0（无工具调用或单轮任务），约 32% 样本值为 1.0（全工具结果均被后续轮次不同程度引用），中间值稀疏。导致语料统计中的「均值 70% / 中位数 95%」缺乏统计区分度，HTML 看板的「100%」亦难以提供有效洞察。
- **当前缓解**：`vmr analyze -corpus` 统计时需结合分布形状（P10/P50/P90 及两端样本数）共同解读；暂不重定义指标语义以维护 v1-complete 稳定性。
- **可能方案**：细化有效引用粒度（如按实体引用率加权或按 token 深度衰减）或按任务类别（含/不含工具调用）分桶展示。
- **触发条件**：后续版本计划重构行为指标语义时统一评估。


### C. 分析半区 · LLM 解读层校准

#### 2.18 [中] Phase 1b 六个 LLM 语义判别器尚未完成完整黄金样本校准

- **现状**：`internal/story/llm_findings.go` 六个判别器已实现、单测覆盖、且用 `_eval/calibrate_p1b.go` 对真实生产日志跑过真实模型验证（6 个真实 Journey 上机械核验 Evidence Anchor 有效率 100%，人工抽查合理）。但不是正式合入门禁——那需 30~50 个 Journey、每模块 ≥6 正/负例的系统性黄金样本集 + 人工标注 Ground Truth 算真实 Precision/Recall。
- **为什么待定**：黄金样本挑选与人工标注是需实际投入时间的判断性工作，无法自动化；当前抽样规模下无需立即处理的误报模式，不构成阻塞。`_eval/calibrate_p1b.go` 已是可直接复用的校准工具，扩大 `-input`/`-limit` 即可推进——**成本在人力时间，不在代码**。


#### 2.53 [低] LLM `goal_drift` 可能把漂移锚点定位到 Step 1

- **现状**：`detectLLMGoalDrift`（`internal/story/llm_findings.go`）把每个 Journey 的第一步也放进喂给 LLM 的 checkpoint 列表；第一步是原目标陈述处，语义上不可能「从自身漂移」，但 LLM 偶尔会把 `drift_step_seq` 落在这里，产出一条读起来不通的 finding。`StepSeq` 取自 LLM 返回值，中间没有归一化代码——这不是坐标转换 bug，是证据包结构 + 提示词的问题。
- **可能方案**：checkpoint 列表排除 `i == 0`；接受条件加 `drift_step_seq > 首步 Seq` 的护栏（fail-open，与本文件其它 detector 一致）。~20 分钟含 mock 测试。
- **为什么待定**：仅 `-llm-addr` 时触发，无任何 GTM/规则层工件依赖 LLM 解读层；属 §2.18 未完成黄金样本校准的一个具体表现，随那批一并处理。2026-08-30 增长打磨批次评估后明确剔除出第一梯队。


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


#### 2.17 [中] `imgprep` 解码闸门按「防炸弹」设定，其内存上界与单请求内存预算差一个数量级

- **现状**：`processImage` 在 `image.Decode` 前用 `image.DecodeConfig` 只读头取宽高，声明尺寸 > `maxDecodePixels`（64MP）直接放弃降采样原样透传。**闸门存在且工作正常**，目的是拦解压炸弹。问题在阈值量纲：64MP 按 RGBA 约 256MB/次解码，而 UserGuide「单请求内存预算」核算的是 ~32MB/请求——两个数字各自都对，回答的不是同一个问题（「多大算恶意」vs「一个请求该占多少」）。图片逐张解码逐张释放，多图不累加。
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


### G. 工程工具与运维入口

#### 2.54 [低，暂不做] `/help.html` 页内发测试请求

- **现状**：`/help.html`（`internal/server` 里的 `help.html` + `help.zh.html` 双语内嵌页，各 ~1550 行）已有一键复制各 Agent 接入片段、鉴权弹窗、`fetch('/status')` 健康展示。增长战略文档提过「页内直接发一次测试请求看命中节点/延迟」——`X-VMR-Endpoint`/`X-VMR-Attempts`/`X-VMR-Route-Reason` 响应头已具备，技术上可行。
- **为什么暂不做**：真实工作量约 1–1.5 人日（双语内嵌 HTML 锁步、真实计费请求、streaming、错误面、虚拟模型选择器、内嵌 JS 无 Go 测试）。这是 onboarding 漏斗功能，被 `vmr init`/`vmr connect`（战略文档第一梯队）完全压制——真做漏斗应先做那两个。战略文档里本就列在第三梯队。2026-08-30 增长打磨批次评估确认延后。


#### 2.8 [低] `archtest` 函数长度豁免的键无法区分同文件重名方法

- **现状**：`funcLineExemptions` 以 `文件:函数名` 为键，同文件重名方法共用一条（如 `report/ingest.go` 6 个 `Ingest`）。今天全部远低于默认限额，无影响；一旦为其一登记豁免，其余会一并放宽。
- **可能方案**：键改 `文件:接收者类型.函数名`（`ast.FuncDecl.Recv` 已有类型信息）。
- **为什么待定**：需真的出现一个必须豁免的重名方法才有意义。


---


## 3. 优先级总览（替代原 ROI 评估总表）

> 原「ROI 评估总表」已废止：逐条的成本 / 风险 / 价值本就写在 §2 各条目自己的「可能方案 / 为什么待定 / 触发条件」里，
> 另立一张表维护第二份口径，正是这份文档最反对的重复——而且原表只评了 22/30 条（§2.53–§2.56、§2.60–§2.63 从没进表）。
> §2 的组内顺序就是按用户价值 × ROI 排好的，本节省略结论与跨组排期。

- **全局结论**：待办里没有「价值高、成本低、却一直没做」的异常。值得优先投入的集中在四类：大语料规模（§2.2 看触发、§2.1 已证 5.2×）、LLM 解读层校准（§2.18，成本在人工标注）、路由配额（§2.52，用户 hold）、产品路线（§2.13）。
- **多数条目不是「不值得做」，是「收益未经测量」**：§2.2 / §2.3 / §2.7 / §2.10 / §2.17 的共同点是收益尚未实测——而先做优化再测量正是这个项目一贯拒绝的顺序；触发条件到了先测再说。
- **跨组怎么排**：
  - 立即可做（界限清楚、随时可做）：§2.59 compare 同源节选合并、§2.63 §0 补 interactive 行。
  - 触发即做（成本主要等触发，触发条件写在条目里）：§2.2（语料 > 约 3 万条 / RSS > 4GB）、§2.55（语料再涨约 5 倍）、§2.56（时间成首要痛点）、§2.51（观察到 LLM 频繁在行内代码里输出 `**`）、§2.48（词表互相干扰 / sticky 往返可观测）、§2.57（脚注不够用）、§2.58（主力上游长期无价）、§2.18（黄金样本窗口）。
  - 需要先设计（价值高、易做错，禁止仓促）：§2.61 任务达成信号、§2.60 跨时间窗对比。
  - 明确不做（各有量化触发条件，触发即重估）：§2.22 / §2.29 / §2.49 / §2.50 / §2.3 / §2.62。
