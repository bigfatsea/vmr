<!-- Ver 2026-08-03, by Sonnet 5 -->

# vmr — 遗留问题清单

> **基线**：本清单在 commit `a9eee76`（"Architecture review + two-phase refactor"）上逐条实测核对，非文献综述。核对当日 `go build ./... && go vet ./... && go test ./...` 全绿。
>
> **自包含**：所有事实、行号、代码片段均在本文档内重写，不依赖任何其他审计/review 文档。
>
> **分档标准**：
> - **第一梯队** = 倾向立刻处理。要么高价值高 ROI，要么价值一般但改动极简、无回归风险。给问题 + 方案 + 工作量。
> - **待定** = 长远看有价值，但成本高或有风险，改与不改都合理。给问题 + 方案，不给排期。
> - **其他** = 一句话点出，不展开。
>
> 严重度：`[S]` 严重 / `[M]` 中等 / `[L]` 轻微。

---

## 0. 结论先行

- 没有会导致数据丢失、凭证泄漏或服务不可用的缺陷。可以继续放心用于生产。
- 原第一梯队 5 项已全部处理完毕（见 §3.1）；处理过程中 fuzz 测试额外发现并修复了一个真实 nil map panic（`RewriteModel`/`RewriteStream` 对 JSON `null` 输入）。
- 当前第一梯队为空。待定 11 项，其他 26 项。
- 2026-08 全面评审（`docs/VMR_全面评审报告_opus-5.md`，基线 `c2c6df7`）的 P0/P1/P2/P3 已逐项处理：3 项 P0 全部修复，7 项 P1 里 5 项修复、2 项评估后判断本轮成本过高暂不做，16 项 P2 里 14 项修复、2 项复核后判定是误判未改，P3-3（存量编号引用的分级处置）已全项目执行完毕。本次并入的 §2.9-2.11 三个新待定项，以及对 §2.5/§3.2/§3.3 既有条目的补充，都来自那 2 项未采纳的 P1（MiniMax response_fix 开关、探针纳入审计）和 3 项 P0 里未完全采纳的子建议（`/admin/status` 暴露 Check 结果、`recorderBodyCap` 可配置化）——已修复的部分不重复记录在本文档，详情见评审报告本身。

---

## 1. 第一梯队（倾向立刻处理）

当前为空 —— 原有的 5 项（MiniMax thinking 泄漏观测、truncated 请求端点归属、compaction 链接漏链日志、RewriteModel/RewriteStream fuzz 测试、五处一行级修补）已全部处理完毕，见 §3.1。

---

## 2. 待定（有价值，但成本或风险不低）

### 2.1 [M] `vmr report` 仍对同一批输入做两趟完整扫描

**现状**：`report.Build(paths, …)` 内部先跑一趟 `AnalyzeSessions`（会话/任务分组的启发式分析，`internal/report/session.go:194`，已做到按文件并行），再跑第二趟自己的聚合循环。逐请求详单（`details/*.md`）的导出**已经**并进第二趟（`DetailWriter.Submit` 作为 `onRecord` 钩子在聚合过程中被调用），所以现在是两趟而不是三趟——这一步是最近的改进。

**问题**：剩下的两趟仍然各自完整读一遍全部输入文件，包含 `.zst` 解压。GB 级审计日志下，批处理时间中有相当一部分是重复的 I/O + 解压。

**方案**：合并成单趟，让会话分析器与聚合器共享同一次 `ForEachLine` 扫描。

**为什么待定**：两个分析器的内部状态形状不同——`AnalyzeSessions` 需要"全部记录都到齐"才能做 LCP 选父与 compaction 链接（本质上是全局 join），而聚合器是天然流式的。真做单趟，得把会话分析器改造成"边喂边攒、结尾统一 finalize"的形状，改动量 >200 行，且会碰到 report 里最微妙的那部分启发式。**建议只在真的遇到"报告跑太慢"的实际投诉时再做**，届时先测一下两趟里各自的耗时占比再决定。

---

### 2.2 [M] `report` 全内存聚合，记录量上限偏紧

**现状**：三处内存驻留叠加：

1. `AnalyzeSessions` 第一趟把**所有记录**的 `ReqInfo` 常驻内存（第二趟按 `path:line` 做 join 需要）；
2. 每条 record 的原始 `dur / ttft / stream / inTok / outTok` 被 push 进 6 个 bucket 各自的切片（真百分位需要保留原始值，不能只存 count/sum）；
3. `RequestRows` 每条记录一个 struct。

千万级记录约 640MB。对日均 1–2GB 审计文件的 agent 场景，这个上限不算遥远。

**方案 A（推荐，保精确）**：利用"输入按时间有序"这个性质做流式分桶——跨过日界即 finalize 当日 bucket 并释放其原始切片。内存从 `O(总记录)` 降到 `O(单日记录)`，真百分位不受影响。

**方案 B（不推荐）**：t-digest / 固定容量蓄水池，`O(1)` 内存换 ±1% 的 p50/p95 精度。**明确不建议**：真百分位是这份报表有意识的产品不变式（报表用它判断慢请求分布，近似值会让"p95 突然变化"这类信号失真），不该为内存让步。

**为什么待定**：方案 A 依赖"跨日之后不再有该日记录"这个前提，而多文件混合输入 + 乱序 glob 会破坏它，需要先做输入排序保证。工作量中等、风险中等，**建议在真的撞到内存墙时再做**。

---

### 2.3 [M] OpenClaw / Claude Code 客户端方言散落在 `report` 的 6 个文件里

**现状**：`internal/report` 硬编码了对具体 agent 客户端的深度知识，实测分布在 6 个源文件、共 33 处（不含测试）：

| 文件 | 内容 |
|---|---|
| `session.go` | OpenClaw 的信封 JSON 块（`Conversation info (untrusted metadata)` / `Sender (untrusted metadata)`）、`[Day Mon DD HH:MM TZ]` 时间前缀、`OpenClaw runtime context` 前缀、`chat_id` 正则、no-reply 模式检测 |
| `aggregate.go` | `heartbeat` / `dream_diary` 两个定时任务类别 |
| `metrics.go` | 针对 heartbeat/dream_diary 的低 cache-efficiency 告警规则 |
| `requests.go` | `cronFileTag`：`"heartbeat"` 的输出文件名被固定拼作 `"hartbeat"`（运营方指定的拼写，代码里有注释说明） |
| `detail.go` | `compacted_session` 标签处理 |
| `aggregate_render.go` | 对应的渲染分支 |

外加 Claude Code 的 `metadata.user_id`。

**问题**：一个协议无关、厂商无关的通用路由器，其二进制里编译进了某个特定 agent 应用的消息格式解析规则、定时任务名称，甚至一个刻意拼错的文件名常量。

需要公平地说：**这些启发式本身写得很克制**（失配无害——标不出就不标），提供的价值也是真实的（agent 会话分组正是 `vmr report` 相对通用日志分析工具的核心差异化）。问题不在于它们存在，而在于**它们的位置**：它们应该是审计日志边界下游的一个可替换"客户端方言插件"，而不是散落在通用聚合逻辑里。

**方案**：收拢成 `internal/report/dialect/` 显式子包，对外暴露一个窄接口（大意为"给我一条记录，告诉我它的会话 ID、任务类别、要剥掉的信封前缀"），OpenClaw 作为其第一个实现。通用聚合代码只认接口。

**为什么待定**：现在只有一个方言实现，抽象一个只有一个实现的接口是投机性泛化。**触发条件明确：接入第二个 agent 客户端时做。** 在那之前，收益只是"看起来更整齐"。

---

### 2.4 [M] 客户端流中途断开与完整成功，在审计里完全不可区分

**现状**：2xx 响应头已提交给客户端之后，客户端主动断开（用户按 Ctrl-C、agent 超时放弃）时，vmr 的 attempt 记录里 `error` 字段留空、状态 2xx —— 与一次真正跑完的成功请求**在所有字段上完全一样**。实测 `grep -rn "client_disconnected\|ClientDisconnected" internal/` 无任何命中。

**问题**：这类请求在报表里被计成成功，但它们的 token 用量、耗时分布与真实完成的请求有系统性差异（尤其 agent 场景下用户中断很常见）。分析"为什么 p95 变差了"时，这是一类完全看不见的噪音源。

**方案**：`audit.Attempt`（或 `Record`）加一个 `client_disconnected` 布尔位，在 `copyFlush` 写回客户端返回 `ErrClosedPipe` / context canceled 时置位；`report` 侧增加对应的统计与筛选。

**为什么待定**：这是**改审计记录 schema**。`internal/report` 在编译期耦合 `audit.Record` 的形状，schema 变更必须同步改 `report` 及其测试，并且要考虑历史审计文件（旧记录没有这个字段，消费端必须把"缺失"和"false"都当成"未知"处理，不能当成"没断开"）。收益真实但不紧急，成本是一个完整的跨包变更 + 一次 Format 号升级。

---

### 2.5 [L] 6 个 bucket 类型仍各自重复声明同一批度量字段

**现状**：`internal/report/aggregate.go` 里 `Row`（第 92 行）/ `HourRow`（157）/ `EndpointRow`（194）/ `ClientRow`（244）/ `WorkloadRow`（278）/ `SessionRow`（304）六个结构体，各自重复声明同一批度量字段：token 家族重复 6 次、耗时百分位家族 6 次、TTFT 家族 5 次、原始切片 `durs/ttfts/streamMS` 6 次。

**已做的部分**：`metrics.go` 里的共有派生计算（真百分位、fresh tokens、cache efficiency）已经收进一个共享的 `finishMeasures`，6 个 `finishX` 函数各自只保留独有字段。这消除了计算逻辑的重复。

**未做的部分**：**结构体声明本身没动**。因此新增一个分组维度（比如"按 provider"）仍需要改 4 个地方；`Build` 里那个负责往 6 个 bucket 分发的匿名闭包编译出 **9,744 字节机器码**（实测 `go tool nm -size`，比 `router.Serve` 的 5,616 字节还大），是全仓库最大的单个函数。

**方案**：泛型 `Bucket[K comparable]{ Key K; Measures }` + `Measures.Add/Finish`，估算 1,200–1,500 行 → ~300 行。

**为什么待定（有硬约束）**：同名字段（如 `CacheEfficiency`）在不同 Row 类型上的 `omitempty` 标签**并不一致**。统一成共享嵌入结构体会在零值场景悄悄改变 `vmr-report.json` 的输出。真要做，必须接受一次 Format 号升级（10 → 11），并用真实生产日志逐字节比对验证。这是本清单里**代码量收益最大**的一项，但也是唯一一项会改变对外数据契约的。

**2026-08 复核补充**：`internal/archtest` 已经给 `aggregate.go` 定过 1000 行的行数预算（防它重新长成从前 1053 行的 `aggregate_render.go`），实测已用到 999 行——下一次改动大概率直接撞线。撞线时的直觉反应是把预算数字改大，但 `archtest` 自己的注释已经预先反对这么做（"split it, don't just raise this number"）。这正是本条方案该登场的时候：先把 `Row`/`HourRow`/`EndpointRow`/`ClientRow`/`WorkloadRow`/`SessionRow` 六个结构体收敛成泛型 `Bucket[K]`，`aggregate.go` 的行数会随之大幅下降，新预算届时自然水到渠成——而不是反过来，先在文件已经臃肿的状态下讨论该给多大的预算。这让"何时做"多了一个具体触发信号：`aggregate.go` 撞线的那次改动，就是做这条重构的时候，而不是继续往后拖。

---

### 2.6 [L] ingress 侧请求体仍被独立扫描约 7 遍

**现状**：一个典型的无图无文档 agent 请求，在发往上游前 body 被完整或近似完整遍历的次数：

| # | 位置 | 操作 |
|---|---|---|
| 1 | `server.go` `chatHandler` | `adapter.TopLevelProbe` —— 一次字节级顶层扫描，同时取出 `model` / `stream` / `tools` 是否非空 |
| 2 | `imgprep.HasImageMarker` | `bytes.Contains` |
| 3 | `facts.go` | `EstimateTextTokens(body)` —— 全字节循环 |
| 4–7 | `facts.go` | `documentMarkers` 的 4 个 `bytes.Contains`（全部未命中时 = 4 次完整扫描） |
| 8 | `router` | `SessionFingerprint` —— 结构化扫描 + 2× md5 |
| 9 | `BuildRequest` | `RewriteModel` —— 顶层扫描 + splice（每次 attempt 一遍） |

**已做的部分**：原来的第 1 步是 `json.Unmarshal(body, &probe)` —— 为了两个顶层字段做一次完整的反射式解码。现在换成了字节级的 `TopLevelProbe`，并顺手把独立的 tools 扫描并了进来。这是最刺眼的一处，已经解决。

**未做的部分**：`HasImageMarker` / `EstimateTextTokens` / 4 个 documentMarkers 仍是各自独立的整体遍历。对一个 500KB 的 agent 请求，剩余部分仍有约 3MB 的内存流量。

**方案**：融合成单遍结构化提取器，一次扫描产出图片块、文档 payload、token 估算。

**为什么待定（有明确的风险来源）**：`HasImage` 喂的是**无 fallback 的硬性淘汰条件**——判错会直接导致 503，历史上出过一次真实事故（图片误判）。融合时必须逐字保持 `imgprep` 那套结构化遍历的判定语义，只让其他信号搭车。这不属于"简单易行"那一档，且当前实测 p50 是 1–2ms、主要成本在上游往返，**不存在性能危机来正当化这个风险**。

---

### 2.7 [L] `audit.Attempt.RawPreStrip` 字段类型仍是 `any`

**位置**：`internal/audit/audit.go:160`，消费端在 `internal/report/detail.go:707-715` 要做类型断言（先试 `string`，否则走 `jsonIndent`）。

**问题**：不利于 schema 化，也让"这个字段到底可能是什么类型"只能靠读消费端代码反推。

**方案**：改成 `json.RawMessage`。

**为什么待定**：需要同步检查所有读取该字段的消费方（目前是 `detail.go` 的渲染逻辑，但要确认没有遗漏），且会影响已有审计文件的向后兼容读取路径。改动不大但要仔细，收益纯属整洁性。

---

### 2.8 [L] 审计落盘的文件 write syscall 仍在全局锁内

**现状**：`audit.Logger.Write` 已经优化过一轮——用 `sync.Pool` 复用编码缓冲区，**JSON 编码在锁外完成**，只有最终的字节写入持锁。这消除了"多 MB 的 JSON 编码被并发请求串行化"这个真实瓶颈。

**未做的部分**：`f.Write` 这个 syscall 本身仍在全局 mutex 内，仍是全仓库唯一一处"全局锁包住 syscall"。

**方案**：审计写入走带缓冲 channel + 单写协程，把 syscall 移出请求路径。

**为什么待定**：要处理背压（channel 满了是丢记录还是阻塞请求？）与关停语义（`Close` 要不要等队列排空？丢一条审计记录 vs 让关停挂住，哪个更糟？），复杂度明显上升。实测未暴露问题。**触发条件：审计落盘成为实测瓶颈时。**

---

### 2.9 [L] 探针请求完全绕过审计，`vmr report` 看不到探针成本

**现状**：`internal/router/probe.go` 的 `runProbe` 直接发请求、读响应、报健康，全程不经过 `audit.Record`。`audit` 包自己的定位是"每请求一行"，但探针请求——本质上也是一次真实的上游调用，会消耗 token、产生延迟——完全不在这一行里。半开端点频繁抖动的场景下，这部分成本持续不可见。

**问题**：不只是"补一行日志"这么简单，2026-08 复核实测发现两处比想象中更深的耦合：
1. `Router` 结构体目前完全不持有 `audit.Logger`——审计日志由 `server` 持有，逐请求把 `*audit.Record` 传进 `Serve`；`runProbe` 是 `router` 包内部的后台 goroutine，要写审计得先给 `Router` 加一个可选的 `Audit *audit.Logger` 字段并在 `cmd_start.go` 里接好。
2. `audit.Record.Client`（`Exchange` 类型）不是指针、其 `Request Message` 也没有 `omitempty`——探针没有真实客户端请求，写一条探针记录要么塞一个假的空 `Client.Request`，要么放宽 schema。`internal/report` 在编译期耦合 `audit.Record` 的形状，改 schema 必须同步改 `report` 及其测试，还必须先决定 `vmr report` 的可靠性/延迟/成交量统计要不要把探针记录排除在外——不排除的话，一个端点的"错误率"统计会被后台探针污染。项目里已有的同类先例 `ReplayOf`（标记一条记录来自 `vmr replay` 而非真实流量）目前在 `report` 侧**完全没有特殊处理**，照抄这个先例大概率不是探针记录真正需要的行为。

**方案**：给 `Router` 加 `Audit *audit.Logger`（nil = 未接入，与 `server.Server.audit` 的"nil = disabled"惯例一致）；`audit.Record` 加一个类似 `ReplayOf` 的 provenance 字段（如 `Probe bool`）；`runProbe` 写一条不含完整 body 的精简记录；`report` 侧显式决定探针记录在哪些章节参与统计、哪些章节排除。

**为什么待定**：这是一条贯穿 `router`→`audit`→`report` 三层的改动，且核心难点不是代码量，而是"探针记录在 report 里该怎么呈现"这个产品判断——这个判断没做之前，仓促定 schema 大概率做完一次还要再改一次。

---

### 2.10 [M] 单请求最坏内存约 104MB，`recorderBodyCap` 目前不可配置

**现状**：三个独立定义、各自局部合理的缓冲上限叠加：`config.MaxRequestBodyMB`（默认 8MB，入站请求体）、`router.bufferedCap`（32MB，响应归一化缓冲）、`server.recorderBodyCap`（64MB，审计响应副本，`bytes.Buffer` 增长期还有约 2x 峰值）。三者之和约 104MB/请求，而 `max_concurrency` 默认无限（0）。

**已做的部分（2026-08 复核落地）**：`UserGuide.md`/`.zh.md` 已加"Per-request memory budget"/"单请求内存预算"说明三者乘积及"共享实例请设置 `max_concurrency`"的建议；`config.example.yaml` 的 `max_concurrency` 注释补了估算。

**未做的部分**：`recorderBodyCap` 仍是硬编码常量，不能调低也不能配置。64MB 的审计响应副本对绝大多数请求都是过量的（真实响应很少超过几百 KB），但对"极少数确实需要完整大响应审计"的场景，调低默认值又会让审计记录变得不完整。

**方案**：把 `recorderBodyCap` 降一档默认值（如 16MB），或做成 `config` 里的可选项（如 `audit_response_cap_mb`），默认值维持现状以免破坏现有部署对审计完整性的预期。

**为什么待定**：降低默认值是一个真实的行为变更——可能让原本能完整入档的大响应审计记录被截断，需要单独评估影响面，不是文档层面能替用户一次性决定的事。

---

### 2.11 [L] `/admin/status` 未暴露 `config.Check()` 的操作性告警

**现状（2026-08 复核落地部分）**：`cmd_start.go` 现已在启动和每次热重载（fsnotify/SIGHUP/服务自动重启）时调用 `cfg.Check()`，把每条 `Issue` 打成日志里的一行 `WARN config check: ...`——此前该检查只在人工运行 `vmr check`/`vmr diagnose` 时才跑，热重载路径完全缺席（例如 `api_key` 拼错的 `${ENV_VAR}` 会被静默接受，把全部流量打成 401 且日志无任何提示）。

**未做的部分**：这些 `WARN` 目前只进日志文件，`/admin/status` 里看不到。`/admin/status` 已经有 `config_stale`（配置文件是否比运行中的新）这个信号，`Check()` 的结果是同一类"配置合法但操作性可疑"的信号，逻辑上应该并列。

**方案**：`instanceBlock`（`admin.go`）里加一个 `config_check_issues` 字段，与 `config_stale` 并列渲染最近一次 `Check()` 的结果。

**为什么待定**：日志里的 `WARN` 已经解决了"完全没有信号"这个最紧迫的问题；`/admin/status` 只是让这个信号在不翻日志的情况下也能看到，价值是锦上添花而非从无到有，牵连 `ReloadState`/`admin.go` 及其测试，留作后续一起做更划算。

---

## 3. 其他（一句话）

### 3.1 已修复 / 已解决

- **`router` 包单文件过大**：`router.go` 曾长到 948 行（自设警戒线约 550），现已按职责拆成 `router.go`(532) / `snapshot.go`(178) / `limiter.go`(66) / `transport.go`(115) / `logfmt.go`(112)。**已解决**。
- **`response.go` 里厂商 quirk 与通用路径纠缠**：MiniMax 专属知识（`<think>` 剥离、Thinking-Process 剥离、soft-block marker、泄漏观测）已抽到 `responsefix.go`，`response.go` 只留通用状态机。**已解决**。
- **架构不变式只存在于文档**：`internal/archtest` 已把两条不变式变成可执行测试——`report` 的 import 边界、`router.go` 的 700 行上限。**已解决**。
- **`config -> sticky` 依赖**（配置校验器为读一个常量而依赖运行时包）：`core.StickyBackstopTTL` 现为唯一权威值，`go list` 实测该边已消失。**已解决**。
- **`replay -> server` 依赖**（CLI 调试工具为一个函数拖进整个 HTTP server 包）：`core.FilterClientHeaders` 现为共同依赖，`go list` 实测该边已消失。**已解决**。
- **`core` 是公共抽屉**：展示格式化 `FmtBytes`/`FmtTokens`/`FmtSeconds` 已拆到 `internal/fmtutil`。**已解决**。
- **热路径重复推导不可变值**：`Endpoint.Freeze()` 在 `BuildSnapshot` 里预计算 `healthKey`/`name`，消除了每请求约 11 次 SHA-256。**已解决**。
- **注册表读路径加锁**：`adapter.registry` / `strategy.conditions` 改为 `atomic.Pointer` copy-on-write，读路径无锁（写路径的 mutex 不可省，已有 `-race` 测试锁定）。**已解决**。
- **审计写入把 JSON 编码串行化**：编码已移出锁，改用 `sync.Pool` 缓冲。**已解决**（剩余部分见 2.8）。
- **`cmd/vmr` 单文件 870 行混杂 8 个子命令**：已按子命令拆分，`main.go` 只剩 65 行的 dispatch/usage/adapter 注册。**已解决**。
- **ingress 用 `json.Unmarshal` 全量解码只为取两个字段**：已换成 `adapter.TopLevelProbe`。**已解决**（剩余部分见 2.6）。
- **`ErrorClass.String()` 缺显式 case**：10 个声明值现已全部有显式 `case`，测试锁定了 4 个审计专用值的字符串。**已解决**（`default` 返回 `"transient"` 而非 `"unknown"` 是刻意选择，见 3.2）。
- **详单文件链接用裸 HTML `<a href>`**：现已改为 Markdown 语法 `[Ⓜ️ Markdown](details/…)`。**已解决**。
- **设计文档与代码漂移两处**（`CountNested` 已导出但文档仍说"三份拷贝"、`router.go` 550 行上限已被突破 72%）：文档已更新，且行数上限已由 `archtest` 强制。**已解决**。
- **`vmr report` 需要第三趟扫描导出详单**：详单渲染已并入聚合趟（`onRecord` 钩子 + 独立 worker pool）。**已解决**（剩余两趟见 2.1）。
- **`priority.Compare` 整数溢出、3xx 重定向未处理**：已修复。**已解决**。
- **MiniMax thinking 剥离失效无观测**：`stripThinkingProcess` 的守卫未命中但内容仍具编号推理小节形状（≥3 处、内容 >1KB）时，打 `thinking_process_pattern_detected` 观测标记（`response.go`/`responsefix.go`），覆盖 passthrough 与 buffered 两条路径，不改字节。**已解决**。
- **truncated 请求端点归属为空**：`endpointInfo` 在没有严格成功的 attempt 时回退到"最后一次拿到 2xx 响应头的 attempt"，流被截断请求的 tokens/耗时/成本正确计入服务过它的端点。**已解决**。
- **compaction 链接漏链静默**：`linkCompactions` 未匹配到 successor/predecessor 时补一条 debug 日志，区分"确实无关联"与"探针漏了"。**已解决**。
- **`RewriteModel`/`RewriteStream` 缺 fuzz 测试**：已加 `FuzzRewriteModel`/`FuzzRewriteStream`；fuzz 过程中发现并修复一个真实 bug——两者对 JSON 字面量 `null` 输入会 panic（`assignment to entry in nil map`），现改为返回 error。**已解决**。
- **五处一行级瑕疵**（`purgeOne` ENOENT 噪音日志、`ms()` 1000ms 边界、`Redact` 浅拷贝、`WriteRequestsJSONL` 吞 Close 错误、`vmr.sh` `ExecStart` 缺引号）：全部修复，均有对应测试。**已解决**。

### 3.2 未解决，但明确不建议动

- `health.Registry` 的全局互斥锁分片化 —— 实测约 600 次加锁/秒、锁内是纳秒级 map 访问，纯过度设计。
- `HealthKey` 的 sha256 前 4 字节截断 —— 碰撞概率 2⁻³² 量级，可忽略。
- 健康冷却参数硬编码（`transientBase=2s` / `transientCap=5m` / `longBase=10m` / `longCap=1h`）—— "零调参"是既有设计选择。
- `ErrorClass.String()` 的 `default` 返回 `"transient"` 而非 `"unknown"` —— 刻意为之，防止报表的 error_classes bucket key 无界增长；`default` 分支实际不可达。
- `${VAR}` 未定义时静默展开为空串 —— 已文档化的预期行为，`vmr diagnose` 的 api_key 检查是现有缓解。
- `report/render.go` 的 `reassembleSSE`（语义重组）与 `router/response.go` 的 SSE 状态机是两套独立实现 —— 一个字节级保真增量转发、一个整体语义提取，关注点不同，合并成本高于收益。
- `internal/adapter/openai` 与 `anthropic` 各有约 4 行的 header 拷贝循环 —— 凭证 header 名字与格式不同，抽公共函数收益不明显。
- `imgprep.ImageInfo` → `audit.ImageInfo` 的 20 行字段抄写 —— 换来 `imgprep` 不依赖 `audit`，是包边界的合理代价。
- `copyFlush` 的 goroutine + channel + 每 chunk 一次堆分配 —— 唯一替代路径是在 `DialContext` 里包 `net.Conn` 做 `SetReadDeadline`，但 deadline 会覆盖整条连接生命周期（含 TLS 握手、响应头阶段），与现有 `TLSHandshakeTimeout`/`ResponseHeaderTimeout` 语义重叠，且只能靠真实 TCP 往返验证。
- 把 `Dimension` / `Condition` / `WithinContext` 合并成统一的 `Filter` 接口 —— soft 语义目前只有一个成员，抽象一个只有一个实现的接口违反 YAGNI；触发条件是出现第二个 soft filter。
- 端点级 `quirks: [...]` 声明式配置 —— 会引入新配置概念且用户须理解各厂内部行为才填得对；触发条件是出现第三个厂商 quirk，或发生真实误伤。**2026-08 复核细化了一版更窄的替代方案**：不是通用 `quirks` 系统，只给 `responsefix.go` 里已经存在的、真正会改字节的 MiniMax 修复（`<think>`/Thinking-Process 剥离）加一个 provider 级 on/off 开关，如 `response_fix: [minimax_think, minimax_thinking_process]`，默认全开保持兼容——这是全项目唯一一处"vmr 替用户做了一个用户无法否决的内容决定"，比通用 quirks 配置更聚焦、价值更明确。但实现成本比看起来高：`newRespStream`（`response.go`）目前完全不感知 provider/endpoint 身份，要做成可配置需要贯穿 `config`（新 schema + 校验）→`core.Endpoint`（新字段 + `Freeze()`考量）→`router`（`BuildSnapshot` 计算生效值、`newRespStream` 构造签名改动）三层，外加对应测试。判断：比通用 `quirks` 系统更值得做，但量级仍属于独立任务而非顺手为之，2026-08 这轮未实现。
- 把 `report` 拆成独立二进制 —— 二进制 12MB → 7MB 不是真实收益，却要牺牲"单二进制"这一核心定位（`report` 实测占自有代码符号 55%，182KB/335KB）。
- 引入 DuckDB/cgo 做聚合 —— 与"纯 Go、无 cgo、自包含"定位直接冲突。
- 用 `text/template` 重写 Markdown 渲染 —— 条件列、对齐、`⭐`/`¹`/`⚠️low-n` 脚注在模板里更难读；已用 `mdTable` helper 收掉重复的表头/分隔符拼接，这是更有据的中间方案。
- 为 `router → audit` 上事件总线 —— 当前的可变 Record 传递性能最优；15 处 `if att != nil` 的噪音已由 nil-safe 的 `SetXxx` 方法收敛。
- 统一 `cmdCheck` / `cmdStart` / `diagnose` 三处的路由表打印格式 —— 排序逻辑已统一到 `EffectiveOrder()`，剩下的纯格式化差异不值得再抽象。
- README 的 `admin/status` 示例"无需 api_key" —— 与实现一致，单机单用户场景可接受，已文档化。

### 3.3 未解决，可做可不做（不展开）

- `IngressPath` 写死 openai/anthropic 两协议，加第三个协议时需记得同步改（可挪进 `Adapter` 接口）。
- `audit.retentionDays` 是包级 atomic 全局变量，`SetRetentionDays` → `RetentionDays` 的往返不变式没有专门测试锁定（同仓库的 `imgprep` 对同类问题用的是显式参数，是一处一致性瑕疵）。
- `adminStatus` 对带 zone 的 IPv6 loopback（`::1%lo0`）的判断没有针对性测试（`net.ParseIP` 对带 zone 的地址返回 nil → fail-closed，方向无害）。
- YAML 解析错误信息不含行号（`yaml.v3` 库本身的限制）。
- `report/detail.go:306` 的 `sanitizeName` 不去重连续 `-`（`[^A-Za-z0-9._-]+` 的 `+` 量词已合并大部分场景，但合法 `-` 与替换产生的 `-` 相邻时仍会留下 `--`）。
- `adapter/classify.go:106` 的 `contentHint` 词表含裸 `"sensitive"`，会命中 `"case-sensitive"` 等无关措辞（已知取舍：误判仅多一次无害 failover）。
- 顶层字段扫描器对畸形 JSON"宽进"，依赖上游 `json.Unmarshal` 先行校验——当前所有入口都满足这个隐式前提，但没有注释写明。
- `go.mod` 无 `toolchain` 指令（声明 go1.25.1，本机实测更高版本）。
- `.gitignore` 全局忽略 `*.jsonl` / `*.jsonl.zst`，未来想提交测试 fixture 需要加白名单。
- `vmr.sh` 的 `write_env_file` 用 `printf '%s=%s\n'` 写值，值含换行或特殊字符时 launchd/systemd 的 EnvironmentFile 解析可能出错（API key 现实中不含空格）。
- `loadtest/` 下 `runner` / `config.yaml` / `gentargets` 三处地址常量需人工同步，无自动化保障。
- `loadtest/runner/main.go:116,138` 的 `defer mock.Process.Kill()` 用 SIGKILL，会在日志里留下"有 START 无 STOP"的假崩溃痕迹（第 180 行的正常退出路径已用 `os.Interrupt`）。
- `loadtest/gentargets/main.go:234` 的 `f.Write(line)` 与 `defer all.Close()` 错误被忽略，磁盘满时 `targets.json` 可能静默截断。
- P3-3 分级处置里明确保留不动的 ~23 处引用（`docs/VirtualModelRouter_Design_v4_{Core,Analytics}.md §x.y` 形式，已带文件名）：CLAUDE.md 收紧后的原则是"只引用文档名字和章节名称，不用编号"，这些从严格意义上仍不完全合规，但报告原建议就是留到最后一档——号码旁边已经有文件名兜底，出问题时至少还能定位到文档，紧迫性明显低于本轮已清掉的裸编号和死引用。
- CI（`.github/workflows/ci.yml`）覆盖 `go vet`/`go build`/`go test -race`；2026-08 复核加了 macOS 矩阵、`gofmt -l`、`shellcheck` 三个 job。`staticcheck` 评估后未采纳——本机无网络环境无法预先验证，且 25,574 行生产代码首次接入大概率冒出大量未经筛选的既有发现，没有时间预算逐条判断真假阳性，贸然接入可能让 CI 从下一次 push 就直接变红，风险大于收益。
- 缺 `CHANGELOG.md` / `CONTRIBUTING.md`（≤3 人内部项目，靠 commit message 追踪）。
- `vmr.sh`（609 行，dev/service 双模式）+ `vmr-loadtest.sh`（76 行）无脚本测试，关键路径靠人工验证——2026-08 复核加的 `shellcheck` CI job（见上一条）只是静态检查，不是行为测试，plist/unit 渲染是否真的能被 launchd/systemd 接受这类问题它照样测不出来。

---

## 4. 使用约定

本文档是**唯一**需要跟踪的当前状态。历次审计反复出现"产出不进队列、下一轮重复确认同一批问题"的循环，处理方式是：往后每次核实或处理，**直接在本文档的对应条目上更新或删除**，不再另起新报告；只把仍然成立的新问题并进来。
