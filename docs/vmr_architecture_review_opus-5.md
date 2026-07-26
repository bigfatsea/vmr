<!-- Ver 2026-07-26 16:30, by Opus 5 -->

# vmr 架构深度 Review 与演进方案

---

## 0. 任务 debrief

**任务理解**：对 vmr 做一次彻底的架构 review，本轮不写代码，只出分析与方案。

**三个评估角度**（用户明确指定）：

1. **简洁性** —— 整个架构是否干净简洁；
2. **性能** —— 是否做到了极致性能；
3. **正交性/可分离性** —— 相对独立的模块是否已经分离出来，以提升可维护性与可扩展性。

**关键约束**：抛开既有实现的路径依赖，假设从零设计会怎么做，再回头对比现状；不被"现在就是这么写的"绑架。同时承认现实：最终目标是最优解，但落地要分 2–3 步，第一步优先**低风险、高 ROI、简单易行**的部分。

**产出结构**（按用户要求）：任务 debrief + 项目概述 → 逐模块原始 review 记录 → 拔高到系统级的综合重估与目标架构（局部给 option）→ 分阶段迁移计划。

**我自行假定、未单独确认的前提**：vmr 是本地单二进制、个人/小团队工具，设计文档 §11 明确写过 "对一个 breaking change 成本极低的本地工具，删掉换简单是划算的"。因此本方案允许包含破坏性变更，但会在迁移计划里逐条标注风险等级。

**一句话结论**：**这是一个质量明显高于平均水平的代码库**——协议模型的设计（让非法状态无法表达）、错误分类、health 状态机、字节级 splice、"已识别但不改"决策表，这些都是成熟工程判断的产物。它当前最大的架构问题不是"哪里写错了"，而是**一个批处理数据分析工具（`internal/report`，占源码 45%、占二进制自有代码 55%）与一个延迟敏感的常驻路由进程被熔接在同一个模块边界内**。除此之外，运行时热路径上存在一类系统性的、几乎零风险即可消除的浪费：**不可变派生值被反复重算**。

---

## 1. 项目概况（事实基线）

以下数据均为本轮实测，非估算。

### 1.1 代码规模

| 包 | 源码行 | 测试行 | 备注 |
|---|---:|---:|---|
| `internal/report` | **6,392** | 2,240 | 审计日志聚合分析 |
| `internal/router` | 1,700 | 1,807 | failover 循环 + 响应归一化 |
| `cmd/vmr` | 870 | 746 | CLI，8 个子命令 |
| `internal/adapter`(含 2 子包) | 872 | 815 | 协议适配 |
| `internal/audit` | 663 | 462 | JSONL 落盘 + 压缩保留 |
| `internal/imgprep` | 630 | 723 | 图片降采样 |
| `internal/server` | 615 | **3,960** | HTTP 入口（测试是全栈 E2E） |
| `internal/config` | 570 | 1,046 | |
| `internal/diagnose` | 558 | 717 | |
| `internal/replay` | 452 | 694 | |
| `internal/core` | 290 | 163 | |
| `internal/strategy` | 195 | 52 | |
| `internal/health` | 174 | 169 | |
| `internal/sticky` | 99 | 63 | |
| `internal/probe` | 72 | 62 | |
| `internal/rundir` | 60 | 43 | |
| **合计** | **~14,212** | **~13,762** | 测试:源码 ≈ 1:1 |

- `internal/report` 单包占源码 **45.0%**。
- 离线工具三件套（`report` + `replay` + `diagnose`）= 7,402 行 = **52.1%**。

### 1.2 二进制自有代码构成（`go tool nm -size` 实测）

完整二进制 12.36 MB，其中 vmr 自有代码符号 335,438 字节：

| 包 | 字节 | 占自有代码 |
|---|---:|---:|
| `report` | 183,672 | **54.8%** |
| `router` | 42,534 | 12.7% |
| `diagnose` | 20,936 | 6.2% |
| `adapter` | 16,712 | 5.0% |
| `server` | 14,352 | 4.3% |
| `replay` | 12,400 | 3.7% |
| `imgprep` | 11,632 | 3.5% |
| `audit` | 9,384 | 2.8% |
| `config` | 8,248 | 2.5% |
| 其余 6 包 | 15,568 | 4.6% |

**离线工具占二进制自有代码的 64.7%；真正的运行时路由核心只占 35.3%。**

单个最大函数是 `internal/report.Build.func8`（9,744 字节机器码）——一个匿名闭包，比 `router.tryOne` + `router.Serve` 加起来还大。

### 1.3 包依赖图（实测，无环）

```
core      -> （无）                      ← 叶子
probe     -> （无）                      ← 叶子
rundir    -> （无）                      ← 叶子
sticky    -> （无）                      ← 叶子
adapter   -> core
audit     -> core
health    -> core
strategy  -> core
imgprep   -> core
config    -> adapter rundir sticky       ← ⚠ 见 3.2
report    -> audit core                  ← ⚠ 只依赖这两个，完全孤岛
router    -> adapter audit config core health probe sticky strategy
server    -> adapter audit core health imgprep router
diagnose  -> adapter config core probe router
replay    -> adapter audit config core router server   ← ⚠ 见 3.13
```

依赖图整体健康、无环、方向正确。三处值得注意的边已标注。

**最重要的一条事实**：`report` 只依赖 `{audit, core}`，与 `router`/`server`/`config` **零耦合**。它在结构上已经是一座孤岛——这意味着把它分离出去的成本接近于零，而收益（55% 的自有代码移出路由进程的关注范围）很大。

### 1.4 当前状态

- `go build ./... && go vet ./... && go test ./...` 全绿。
- 性能实测（设计文档 §12，2026-07-24）：11 个场景 × 3 档负载共 4,100 请求，100% 成功；11 个场景中 9 个 p50 在 0–4ms；唯一真实成本是图片降采样。
- `.gitignore` 完备，敏感产物（含完整对话正文的 details/、audit jsonl）均已排除，无误提交。

---

## 2. 评审判据

### 2.1 三个角度的操作化定义

| 角度 | 我实际在找什么 |
|---|---|
| **简洁性** | 同一件事有几处表达？一个概念要理解几个机制？删掉某段代码系统会不会更好？ |
| **性能** | 热路径上有无**重复的、结果恒定的**计算；有无不必要的内存拷贝与分配；有无把 O(1) 写成 O(n) |
| **正交性** | 两个模块之间的耦合面是"一个明确的数据契约"还是"互相知道对方的内部"？改 A 会不会被迫改 B？ |

### 2.2 第一性原理基线：从零设计，vmr 是什么

剥掉所有实现，vmr 的本质是**一个带故障转移的字节转发器**，加上**它自己产生的一份运行记录**：

```
请求字节 ──► [选一个上游] ──► 转发（改最少的字节）──► 响应字节
                  │
                  └──► 一条结构化记录（发生了什么）
```

这里有且只有三件事：

1. **选择**（selection）：健康 + 能力 + 亲和 + 排序 → 一个有序候选列表。纯函数，输入是 (路由表, 健康状态, 请求特征)。
2. **转发**（forwarding）：字节进、字节出，尽可能少改。延迟敏感。
3. **记录**（recording）：把 1 和 2 发生的事写成一条不可变事实。

**关键判断：分析（analysis）不在这三件事里。** 分析消费的是"记录"这个产物，它与转发器的关系，和 `jq`、DuckDB、一个 Python 脚本与转发器的关系**在种类上完全相同**——都是审计日志的下游消费者。审计日志的 JSONL schema 才是那个边界。

这个判断是本文后续所有结论的起点。

---

## 3. 逐模块 Review 记录

> 记录格式：现状 → 发现（按 简洁性 / 性能 / 正交性 归类）→ 判断。
> 结论强度标记：**[强]** 有明确证据、建议做；**[中]** 有道理、需权衡；**[弱]** 记录备查、暂不建议动。

### 3.1 `internal/core`（290 行）

**现状**：包含 `CanonicalRequest`、`Endpoint`、`ErrorClass`、`RequestFacts` 四个领域类型 + `WriteJSON`/`WriteError`（HTTP 响应） + `FmtBytes`/`FmtTokens`/`FmtSeconds`（展示格式化） + `SortedKeys`/`MarshalNoEscape`（通用工具） + `EstimateTextTokens`（token 估算）。

**发现（正交性）[强]**：`core` 已经变成"两个以上包共用的东西就放这"的**公共抽屉**（junk drawer）。里面混了四类关注点：领域类型、HTTP 传输、展示格式化、通用工具函数。

最能说明问题的是 `FmtBytes`/`FmtTokens`/`FmtSeconds`：它们**同时被实时路由日志和离线报表渲染器使用**。这意味着 `report`（离线分析）与 `router`（运行时）之间除了审计 schema 之外，还多了一条"共用展示格式化函数"的隐性耦合——而这正是 §1.3 里 `report -> core` 那条边的真实内容。要让 report 干净分离，这条边必须先处理掉。

**发现（简洁性）[弱]**：`ErrorClass.String()` 的 `default` 分支返回 `"transient"` 而非 `"unknown"`，注释说明了理由（避免下游 bucket key 无界增长），判断合理，不动。

**判断**：
- 拆分 `core` → `core`（纯领域类型，零行为）+ `internal/fmtutil`（展示格式化）。`WriteJSON`/`WriteError` 归属可讨论（见 3.9）。
- 这是**低风险、机械性**的改动，且是 report 分离的前置条件。

---

### 3.2 `internal/config`（570 行）

**现状**：YAML 严格解析（`KnownFields`）、`${ENV}` 展开、校验、三态代理解析、热加载 watch。校验规则密集且到位。

**做得好的**：
- **YAML 严格解析**：`max_concurency` 这类 typo 直接拒绝加载。这是配置驱动工具最容易出真实事故的地方，处理正确。
- **代理三层解析 + 无环境变量回退**：`http(s)_proxy`（地址）与 `proxy`（开关）分离，是一次正确的重新设计。"流量去哪必须在 config.yaml 里读得出来"是个好原则。
- **`proxy: true` 但没配代理地址 = 加载期校验错误**：把运行时问题变成静态可判问题，是典型的好设计。

**发现（正交性）[中]**：`config -> sticky` 这条依赖很别扭。原因是校验 `sticky_ttl` 不得超过 `sticky.BackstopTTL`（24h）。结果是**配置 schema 校验器依赖了一个运行时状态包，只为读一个常量**。

层次上应该反过来：`BackstopTTL` 是一个策略常量，属于配置/领域层；`internal/sticky`（一个纯 KV + mtime 存储）应该消费它，而不是定义它。当前方向让 `config` 这个本应接近叶子的包被拉高了。

**发现（简洁性）[弱]**：`CountNested` 已导出并被三处共用——但设计文档 §14 仍把它列为"三份独立拷贝、不值得统一"。**文档与代码已漂移**（详见 3.15）。

**判断**：把 `BackstopTTL` 移到 `core` 或 `config`，删掉 `config -> sticky` 边。10 分钟的改动。

---

### 3.3 `internal/adapter`（872 行，含 2 个协议子包）

**现状**：3 方法接口（`Protocol`/`ResolveURL`/`BuildRequest`/`ClassifyError`——实为 4）、`database/sql` 式编译期注册、共享错误分类表、字节级 `RewriteModel`/`RewriteRoles`、`SessionFingerprint`、`HasNonEmptyTopLevelArray`。

**做得好的 [强，勿动]**：
- **接口极小**。新增协议 = 一个包 + 一行 blank import。这是本项目扩展性的核心，设计正确。
- **`BuildRequest` 同时返回出站 body 字节**：避免 `GetBody()+ReadAll` 再拷一份。所有权契约（"交出去就不得改"）清晰且五个调用点都天然成立。是深思熟虑的性能设计。
- **字节级 splice 改写 model**：200KB body 实测 99µs / 5 次分配，比全量 unmarshal 快一个数量级，且逐字节保留客户端原文。设计与实现都对。
- **错误分类含 body 嗅探**：实测驱动（MiniMax 用 400 当 404、OpenRouter 403 = moderation），并且取舍方向正确（宁可误判一次无害切换，不可漏判导致永不 failover）。`upstreamHint` 那处刻意收窄的例外，论证也是对的。

**发现（性能）[强]**：`adapter.Get(ep.AdapterType)` 在 `tryOne` 里**每次尝试都调用一次**，内部是 `RWMutex.RLock` + map 查找。但 registry 只在 `init()` 写入，运行期完全不可变。这是"热路径上为一个不可能发生的并发场景加锁"。归入 §4.1 的统一根因。

**判断**：接口和实现质量高。唯一问题是"每请求重复解析不可变值"，见 §4.1 统一处理。

---

### 3.4 `internal/health`（174 行）

**现状**：冷却 + 指数退避 + 半开单飞名额。`Available`/`Acquire`/`ReportSuccess`/`ReportFailure`/`ReportNeutral`/`Status`。

**做得好的 [强，勿动]**：
- **174 行做完了整个健康状态机**，职责边界干净，不知道自己被主动/被动哪种模式调用。这是本项目最漂亮的一个包。
- **"每个 acquired probe 必须以 Success/Failure/Neutral 之一结束"** 这条不变式被显式写进注释，并被两套镜像回归测试锁定（被动 `server_probe_test.go` / 主动 `server_active_probe_test.go`）。这是对的做法：不变式 + 测试锁定，而不是靠人记住。
- `Retry-After` 封顶 1h 的理由（上游可控输入，畸形值不该锁死到重启）正确。

**发现（性能）[弱，不建议动]**：整个 registry 用一把全局 `sync.Mutex`。理论上 N 端点高并发下所有健康查询串行化。但实测量级是 150 req/s × ~4 端点 ≈ 600 次加锁/秒，锁内操作是纳秒级 map 访问——**这不是问题，分片或原子化是纯粹的过度设计**。明确记录为"不建议优化"。

**判断**：保持原样。

---

### 3.5 `internal/strategy`（195 行）

**现状**：`Dimension`（排序，看不到请求）+ `Condition`（准入，看得到请求）两个平行接口 + `WithinContext`（独立函数，不注册进 Condition）。

**做得好的 [强]**：`Dimension` / `Condition` 的分离是**正确的第一性原理判断**。排序是"都能处理时先试谁"，准入是"能不能处理"，这是两种不同的问题；给 `Dimension.Compare` 硬塞一个 request 参数会强迫每个排序维度都感知请求。设计文档 §11 对此的论证是对的。

**发现（简洁性）[中]**：现在"选端点"这件事由**三种机制**表达：`Dimension`（接口）、`Condition`（接口）、`WithinContext`（自由函数 + 调用方特判）。第三种存在的唯一原因是它需要"全体拒绝时不能真拒绝"的降级语义。

从零设计的话，更统一的模型是单一 `Filter` 管线，每个 filter 声明自己是 `hard`（可以清空候选集）还是 `soft`（清空则回退）：

```go
type Filter interface {
    Name() string
    Hard() bool
    Eligible(ep *core.Endpoint, facts core.RequestFacts) bool
}
```

这样 `WithinContext` 就是一个普通的 soft filter，`Serve` 里只有一个循环，三种机制变两种。

**但是**——设计文档 §11 已经论证过："为一个目前只有一个成员的特例改动整个接口的语义不划算，`router.Serve` 里两行代码就能表达清楚"。**这个论证仍然成立**。soft filter 目前有且只有一个成员，抽象一个只有一个实现的接口是投机性泛化，违反用户自己 CLAUDE.md 里的 YAGNI 原则。

**判断**：**记录备查，本轮不建议动**。触发条件：当出现第二个需要 soft 语义的 filter 时（例如未来的 price/latency 软偏好），再做这个统一——那时它就不是投机而是有据。

**发现（性能）[中]**：`Eligible()` 对**每个端点、每个请求**都做一次 `condMu.RLock()`。`conditions` 只在 `init()` 追加，运行期不可变。同 §4.1 根因。修法：`atomic.Pointer[[]Condition]` 或直接冻结。

---

### 3.6 `internal/sticky`（99 行）

**现状**：`Peek`/`Set` 的 KV + mtime，24h 兜底清理。不知道任何端点/TTL 细节，有效性判定留给调用方。

**判断 [强，勿动]**：职责切得非常干净——"内存淘汰"与"粘性有效性"是两件事、用两个数字，这个区分是对的。`BackstopTTL` 的归属问题见 3.2，与本包设计无关。

---

### 3.7 `internal/router/router.go`（948 行）

**现状**：`BuildSnapshot`、`Install`、并发闸、`Serve`（候选构建 + failover 循环）、`tryOne`（单次尝试全流程）、`copyFlush`、以及约 80 行日志格式化辅助函数。

#### 发现 A（简洁性）[强]：项目自己的警戒线已经被突破且无人察觉

设计文档 §11 明确写着：

> router 主流程（`router.go`，含并发闸与流转发）**约 550 行**，响应归一化器（`response.go`）另约 550 行；**若主流程显著变大，说明抽象错了**

**实测：`router.go` = 948 行，超出自设阈值 72%。** `response.go` = 657 行，超出 19%。

这条自设的 tripwire 已经触发，但因为没有任何自动化检查，它只是文档里的一句话。这本身是一个值得修的**流程问题**：项目有明确的架构不变式，却没有把它变成可执行的检查。

`router.go` 增长到哪里去了，拆解如下：

| 内容 | 约行数 | 是否属于"failover 主流程" |
|---|---:|---|
| `Serve`（候选构建 + failover 循环） | 155 | 是 |
| `tryOne`（单次尝试） | 190 | 是（但混了多种关注点，见 C） |
| `BuildSnapshot` / `Install` / `clientFor` | 130 | 快照构建，可独立 |
| 并发闸（limiter/AcquireSlot/Concurrency） | 60 | 可独立 |
| `copyFlush` | 55 | 传输层，可独立 |
| **日志格式化**（`tagCol`/`clientTag`/`epLabel`/`fmtDur`/`capField`/`attemptPrefix`） | **~80** | **否** |
| header 黑名单 + copyRespHeaders | 30 | 可独立 |
| 各类小工具（`findByHealthKey`/`moveToFront`/`rejectionSummary`/`parseRetryAfter`/`IngressPath`…） | 100 | 混杂 |

至少 **~245 行（26%）** 与"failover 主流程"无关：日志格式化、快照构建、并发闸。仅仅按文件拆分（同包内 `snapshot.go` / `limiter.go` / `logfmt.go` / `transport.go`），就能让 `router.go` 回到 ~600 行并重新贴近它自己声明的形状。**这是零行为风险的纯文件移动。**

#### 发现 B（正交性）[强]：路由核心依赖审计层

`Serve` 的签名是：

```go
func (rt *Router) Serve(w, r, creq *core.CanonicalRequest, protocol string, rec *audit.Record)
```

`router` 因此 `import "vmr/internal/audit"`。后果在 `tryOne` 里非常直观——**15 处 `if att != nil { ... }` 与路由逻辑逐行交织**：

```go
if att != nil { att.Error = "build: " + err.Error(); att.ErrorClass = core.ErrBuild.String() }
...
if att != nil { att.URL = req.URL.String(); att.Request = audit.NewMessage(...) }
...
if att != nil { /* 8 行构造 Response */ }
```

从第一性原理看，"转发"与"记录"是两件正交的事。转发器应该**发出事实**，由某个收集器决定要不要记、记到哪。当前是转发器**自己填写审计对象的字段**。

诚实的反面论证：Go 里做 event-emission（channel 或 interface 回调）会引入分配和间接调用，对每请求路径不是零成本；而且现在这个"可变 Record 顺着调用链传"的模式**性能上是最优的**（零分配、直接写字段）。所以这不是一个"明显该改"的问题。

**判断 [中]**：这是**真实的正交性缺陷，但修复的收益主要是可读性，代价是热路径开销和一次不小的重构**。建议的中间方案：定义一个 `router` 自己拥有的窄接口（`type attemptSink interface { ... }`）由 `audit` 实现，或至少把 15 处 nil 检查收敛成一个 `att.set(...)` 之类的空安全 helper（nil receiver 安全的方法），把噪音从 15 处降到 15 行单行调用。**不建议为此上事件总线。**

#### 发现 C（简洁性）[中]：`tryOne` 190 行混合了 6 种关注点

依次是：adapter 解析 → 请求构建 → 审计填充 → HTTP 执行 → 错误分类与健康上报 → 响应头转发 → 归一化器装配 → 日志。

可提取的自然接缝：`handleErrorResponse`（>=400 那 60 行）和 `forwardSuccess`（2xx 那 50 行）。拆完 `tryOne` 约 80 行，且两个子函数各自的职责单一、可独立测试。低风险。

#### 发现 D（性能）[强]：`copyFlush` 每个流式请求 1 goroutine + 1 channel + 每 chunk 1 次分配

```go
data = append([]byte(nil), buf[:n]...)   // 每个 chunk 一次堆分配
select { case ch <- chunk{data, err}: ... }
```

这个 goroutine + channel 结构存在的**唯一目的**是给阻塞的 `Read` 加一个 idle 超时。代价是：每个流式响应一个 goroutine、一个 channel、一个 timer，以及**每个 SSE chunk 一次堆分配**（因为切片要安全地跨 channel 传递）。

**从零设计的正确做法**：idle 超时属于**传输层**，不属于拷贝循环。在 `NewUpstreamClient` 的 `DialContext` 里包一层 `net.Conn`，每次成功读取后 `SetReadDeadline(now + idle)`。这样：

- `copyFlush` 退化成一个普通的 `for { n, err := body.Read(buf); w.Write(buf[:n]); flusher.Flush() }` 循环；
- goroutine、channel、timer、每 chunk 分配**全部消失**；
- idle 超时语义反而更准确（覆盖 TCP 层静默，而不只是 `Read` 调用的返回间隔）。

#### 发现 E（性能）[中]：流式路径上有 5 次数据拷贝

追踪一个 SSE 字节从上游到客户端：

1. `s.src.Read(s.scratch)` — 上游 → scratch（32KB）
2. `s.pending = append(s.pending, b...)` — scratch → pending
3. `s.out = append(s.out, block...)` — pending → out
4. `copy(p, s.out)` — out → copyFlush 的 buf
5. `append([]byte(nil), buf[:n]...)` — buf → chunk（**分配**）
6. `w.Write(c.data)`

**5 次拷贝 + 至少 2 次分配**。理论下界是 2 次（上游 → 缓冲 → socket）。修掉 D 能直接干掉第 5 步及其分配；`respStream` 内部的 2↔3 在 passthrough 模式下也有合并空间（passthrough 时 `pending` 与 `out` 可以是同一块缓冲的两个游标）。

**诚实的量级评估**：这些都不是当前的瓶颈——实测 p50 是 1–2ms，主要成本在上游往返。这些改动的价值是"消除确定的浪费"和"让流式路径的成本可预测"，**不是"系统很慢需要救"**。不应该以性能危机的名义推销它们。

---

### 3.8 `internal/router/response.go`（657 行）

**现状**：三态传输模式状态机（undecided / buffered / passthrough）+ model 字段改写 + `<think>` 剥离 + "Thinking Process:" 剥离 + `[DONE]` 补齐 + 软拦截/CRLF 观测标记。

**做得好的 [强]**：
- **以完整 SSE 事件为处理单位**这个决定是对的，论证也漂亮："复杂度病灶从来不在什么时候切换模式，而在用什么粒度切分字节"。按字节切分才会有 carry 装不下、状态半途丢失、重入吐残留这类 corner case。
- **两个 quirk 的触发守卫是对称的**（首个非空 content/text 值必须以标记开头），并有回归测试锁定"正文中段引用 `<think>` 不被误删"。这是从一次真实数据损坏中学到的正确教训。
- **失手即退化为直连行为，永不更差**——这个 fail-safe 方向是对的。

#### 发现（正交性 + 简洁性）[中]：约 80% 的复杂度服务于单一厂商的两个 bug

这个文件里的内容可以清晰地分成两类：

| 类别 | 内容 | 是否所有 provider 都需要 |
|---|---|---|
| **通用、必需** | model 字段改回虚拟名、`[DONE]` 策略（协议级）、SSE 事件切分 | 是 |
| **单厂商 quirk 修复** | `<think>` 剥离、`Thinking Process:` 剥离 | 否，仅 MiniMax-M3 |
| **纯观测** | `soft_block_detected`、`crlf_framing_suspected` | 否 |

**如果移除 MiniMax 的两个修复**，`respStream` 就不再需要：三态模式机、`decide()`、`classifyEvent()`、`thinkShapeGuard()`、`bufferedCap`、`rawPreStrip`、buffered→passthrough 的恢复逻辑、`stripThinkingProcess()` 那 70 行。剩下的是：切事件、正则替换 model、发出——**大约 120 行，且没有任何模式状态**。

也就是说：**一个通用路由器的响应层里，有 ~500 行以某个具体厂商的名字命名的逻辑，且对所有 provider 的所有响应全局生效（靠嗅探而非声明）。**

**从零设计**会把它做成**端点级声明式的 quirk 链**：

```yaml
endpoints:
  - provider: minimax
    model: MiniMax-M3
    quirks: [strip_think, strip_thinking_process]
```

好处：默认路径退化成极简的流式透传；只有声明了 quirk 的端点才实例化那台状态机；对其他 provider 的误伤面从"理论上存在"变成"结构上不可能"。

**必须诚实地记录反面论证**：设计文档 §5.5 **已经明确考虑并否决过**这个方案，理由是"endpoint 级 `quirks:` 配置是一个新概念 + 新配置面 + 用户须理解各厂内部行为才能填对，为剩余误伤面引入配置维度不划算"。加上对称守卫上线后，误伤面已经收窄到"回复恰好以触发标记开头"这一种。

**我的判断 [中]**：这个否决在**用户体验**维度上是有道理的，但它把成本记在了错误的账上——真正的代价不是"误伤概率"，而是**这 500 行永久性地成为了路由核心的一部分，且会随着接入新厂商而继续增长**（今天是 MiniMax 两条，明天新厂商再来两条，状态机就要再长一截）。这是一个**会累积的架构债**，而不是一次性的取舍。

建议的折中（保留两者优点）：**保持自动嗅探作为默认行为不变**（用户零配置、不破坏现有部署），但在**代码结构**上把 quirk 修复抽成一个独立的、可组合的 `responsefix` 层，`respStream` 只保留"切事件 + model 改写 + DONE 策略"。即：**先做代码层面的分离，不动配置面和用户可见行为**。这样既拿到了架构收益，又不承担 §5.5 担心的配置负担。将来若真要做 endpoint 级声明，接缝已经在了。

---

### 3.9 `internal/server`（615 行源码 / 3,960 行测试）

**现状**：HTTP 入口、鉴权、header 黑名单、`chatHandler` 线性管线、`/v1/models`、`/admin/status`。

**做得好的 [强]**：
- **615 行做完整个 HTTP 面**，`chatHandler` 是一条可以从上读到下的线性管线，非常清楚。
- **测试:源码 = 6.4:1，且是全栈 E2E**（`server_failover_test.go`、`server_condition_routing_test.go`、`server_active_probe_test.go`、`server_openclaw_scenario_test.go`…）。这些测试打在 HTTP 边界上，**不耦合内部结构**。

  **这一点极其重要，值得单独强调**：它意味着本文提出的绝大多数重构（拆文件、提取函数、预解析端点、融合扫描器）都有一张行为级安全网兜底。**这是本次演进方案最大的风险缓释因素**，也是我敢于建议动 router/server 的底气。
- **鉴权设计**：`api_keys` 单一鉴权面，命中的 key 自身尾部即 `client_key_tag`——用密钥自身派生标签，省掉了"再配一个名字"的配置维度，聪明。

**发现（性能）[强]**：见 §4.2——ingress 处的 `json.Unmarshal(body, &probe)` 为了取两个顶层字段而全量 tokenize 整个 body。

**发现（简洁性）[弱]**：`chatHandler` 里 20 行把 `imgprep.ImageInfo` 逐字段抄进 `audit.ImageInfo`。两个结构体字段完全同名同义。可以让 `imgprep` 直接返回 `audit.ImageInfo`（会引入 `imgprep -> audit` 依赖，不好），或者接受这 20 行是"包边界的合理代价"。**倾向于保持现状**——这 20 行换来的是 `imgprep` 不依赖 `audit`，值得。记录备查。

---

### 3.10 `internal/audit`（663 行）

**现状**：`Record`/`Attempt`/`Message` schema、`Logger`（互斥追加 + 按日轮转）、凭证掩码、`KeyTag`、housekeeping（zstd 压缩 + 保留）。

**做得好的 [强]**：
- **schema 设计好**：双层结构（client / attempts）、成功尝试不存 body（透传恒等）、`norm` 列表完整解释字节差异。这是一个**深思熟虑的数据契约**。
- **zstd 整文件压缩**的论证正确（跨行冗余是体积主因，gzip 32KB 窗口看不见，实测 20–75×）。
- **crash-safe 落盘**（tmp + rename + 重启续跑）。
- `Close` 不等 housekeeping 的理由清楚且正确。

**发现（性能）[中]**：`Write` 的两个问题：

```go
line, err := json.Marshal(rec)      // ① 全量 marshal 到新分配的 []byte
l.mu.Lock()
...
_, err = l.f.Write(append(line, '\n'))   // ② append 可能触发整体 realloc+copy
```

① 对 agent 流量意味着**每请求分配一份 (请求体 + 响应体 + 所有 attempt body) 大小的内存**——典型 200KB–1MB，峰值可能 2–3MB。这是纯 GC 压力。
② `json.Marshal` 返回的切片 cap 常常等于 len，`append` 一个字节就再复制一遍整个 buffer。

同时，**整个文件写 syscall 在全局 mutex 内**——所有请求的审计落盘串行化。实测未暴露问题（load test 没有大 body 场景），但这是唯一一处"全局锁包住 syscall"的地方。

**判断**：
- 低风险改法：`Logger` 持有一个可复用的 `bytes.Buffer`，在锁内 `buf.Reset(); json.NewEncoder(buf).Encode(rec); f.Write(buf.Bytes())`。`Encoder.Encode` 自带换行，`append` 那步也一并消失。**几行代码，消除每请求的 MB 级分配。**
- 更彻底（可选）：审计写入走带缓冲 channel + 单写协程，把 syscall 移出请求路径。但要处理背压与关停语义，复杂度上升，**留在长期演进方向里按需触发**。

**发现（正交性）[弱]**：`retentionDays` 是**包级 `atomic.Int64` 全局变量**，通过 `SetRetentionDays` 设置。设计文档 §11 里已经论证过 `imgprep` 应该用显式参数而非包级状态（并且照做了），但 `audit` 这里保留了全局单例。**同一个仓库里对同一问题有两种相反的处理**。不影响正确性，但是一处一致性瑕疵。记录备查。

---

### 3.11 `internal/imgprep`(630) / `internal/probe`(72) / `internal/rundir`(60)

**做得好的 [强]**：
- **`imgprep` 的分层检测**（子串扫描 → JSON 解析 → `DecodeConfig` 读文件头）成本递增顺序正确，95% 无图请求只付一次 `bytes.Contains`。
- **GIF 一律跳过**的安全论证是本仓库最好的一段风险分析：`image/gif.DecodeAll` 对帧数和累计内存都无上限，为了"缩放少数单帧 GIF"而必须先付出无界解码代价，不划算——**直接把整条路径上的 `DecodeAll` 调用连根拔掉**。这是正确的"重新设计问题以消除边界情况"（用户 CLAUDE.md 里的 Taste 原则）。
- **解压炸弹防护**（声明像素 > 64MP 拒绝解码）、**fail-open + panic recover 且不静默**，都对。
- **降采样缓存 key 含 maxPx** 的理由正确。
- `probe` / `rundir` 是干净的小叶子包，`probe` 被 `diagnose` 与 `router` 共用且二者互不依赖，避免循环 import——这个设计是对的。

**判断**：这三个包**不需要改动**。

---

### 3.12 `internal/report`（6,392 行）——本次 review 的重点

按子模块拆开看。

#### 3.12.1 结构现状

| 文件 | 行数 | 职责 |
|---|---:|---|
| `aggregate.go` | 1,270 | 9 个 bucket map + `Build` 主循环 + 6 个 bucket 类型定义 |
| `detail.go` | 1,073 | 逐请求 Markdown 详单渲染 |
| `aggregate_render.go` | 972 | §0–§8 九章 Markdown + Mermaid |
| `session.go` | 879 | 会话/任务分组启发式（LCP、compaction、workload 标签） |
| `render.go` | 681 | 详单渲染辅助 |
| `requests.go` | 581 | 请求索引 + 分组 sibling |
| `pricing.go` | 363 | 多币种、时间窗定价 |
| `metrics.go` | 332 | 派生指标 + 6 个 `finishX` |
| `usage.go` | 178 | 四种 usage 形态提取 |
| `export.go` | 63 | |

**渲染层合计（`detail.go` + `aggregate_render.go` + `render.go` + `requests.go`）= 3,307 行，占本包 52%。** 全部是 `fmt.Fprintf` 手写 Markdown。

#### 3.12.2 发现（简洁性）[强]：6 个 bucket 类型是同一个"度量集"的 6 次复制

`Row` / `HourRow` / `EndpointRow` / `ClientRow` / `WorkloadRow` / `SessionRow` 各自重复声明了同一批度量字段：

| 度量族 | 重复次数 |
|---|---:|
| `TokensIn/InCached/InCacheWrite/InFresh/Out/Known` + `CacheEfficiency` | 6× |
| `RequestsWithDur` / `DurMSP50` / `DurMSP95` / `DurMSMax` / `SlowRequests` | 6× |
| `TTFTKnown` / `TTFTMSP50` / `TTFTMSP95` | 5× |
| `StreamKnown` / `StreamMSP95` | 4× |
| 原始切片 `durs, ttfts, streamMS []int64` | 6× |
| `Requests` / `OK` / `Errors` / `SuccessRate` | 5× |
| `InTokP50/P95` / `OutTokP50/P95` | 2× |

对应地，`metrics.go` 里有 6 个 `finishX` 函数，每个都在重复同一套派生计算：

```go
X.DurMSP50, X.DurMSP95 = percentiles(X.durs)
X.TokensInFresh = freshTokens(X.TokensIn, X.TokensInCached, X.TokensInCacheWrite)
if X.TokensKnown > 0 { X.CacheEfficiency = cacheEff(...) }
X.durs, X.ttfts = nil, nil
```

**这是一个手写的 OLAP cube**：本质上只有「分组键 × 度量集」两个维度，却被展开成 6 份结构体 + 6 份 add + 6 份 finish + 6 份渲染。新增一个分组维度（比如"按 provider"）需要改 4 个地方。

**从零设计**：

```go
type Measures struct {
    Volume  VolumeStats
    Tokens  TokenStats
    Latency LatencyStats   // 内部持有 durs/ttfts/streamMS
    Wire    WireStats
    Cost    *float64
}
func (m *Measures) Add(rc *rec2)
func (m *Measures) Finish()

type Bucket[K comparable] struct { Key K; Measures }
```

**估算：~1,200–1,500 行 → ~300 行。** 这是全仓库最大的单点简化机会。

代价：`vmr-report.json` 的字段布局会从 30 个平铺字段变成嵌套结构 → `Format` 需要从 10 升到 11。考虑到这是本地工具且 format 号机制本就为此存在，**这个代价可以接受，且嵌套后的 JSON 可读性更好**。

#### 3.12.3 发现（简洁性）[中]：3,307 行手写 Markdown 字符串拼接

52% 的 report 包在做 `fmt.Fprintf` 拼 Markdown 表格。这通常是"抽象选错了"的信号——Go 标准库有 `text/template`。

**但要诚实**：模板化 Markdown 表格未必更易读（对齐、条件列、脚注标记 `⭐`/`¹`/`⚠️low-n` 这类逻辑在模板里会很难看），而且当前代码是可测试的。**这不是一个有把握的净胜项**。

更有把握的中间改进：抽出一个 `table` builder（列定义 + 行数据 + 渲染），把重复的表头/分隔符/对齐/空值 `-` 处理收敛掉。九章报告里的表格结构高度相似，这层抽象是有实据的，不是投机。**估算能收掉 400–600 行。**

#### 3.12.4 发现（正交性）[强]：通用路由器里嵌入了特定第三方客户端的领域知识

`report` 硬编码了对 **OpenClaw** 这一个具体客户端应用的深度知识：

```
session.go:461  openClawEnvelopeRe —— "Conversation info (untrusted metadata)" / "Sender (untrusted metadata)" JSON 块
session.go:470  stripOpenClawEnvelope()
session.go:474  leadingBracketRe —— OpenClaw 的 "[Day Mon DD HH:MM TZ]" 时间前缀
session.go:492  strings.HasPrefix(head, "OpenClaw runtime context")
session.go:281  chatIDRe —— OpenClaw 的 chat_id
session.go:334  OpenClaw 的 no-reply 模式检测
aggregate.go:1209  "heartbeat" / "dream_diary" 定时任务类别
metrics.go:229     heartbeat/dream_diary 的低 cache-eff 告警规则
requests.go:65     cronFileTag: "heartbeat" → 文件名固定拼作 "hartbeat"
detail.go:535      OpenClaw 的 compacted_session 标签处理
```

外加 Claude Code 的 `metadata.user_id`。

**这是本仓库最清晰的一处正交性违反**：一个协议无关、厂商无关的通用路由器，其二进制里编译进了某个特定 agent 应用的消息格式解析规则、定时任务名称、甚至一个拼写错误的文件名硬编码（`heartbeat` → `hartbeat`）。

需要说明的是，**这些启发式本身写得很克制**——设计文档明确说了"失配无害——标不出就不标"，而且它们提供的价值是真实的（agent 会话分组正是 `vmr report` 相对通用日志分析工具的核心差异化）。**问题不在于它们存在，而在于它们的位置**：它们应该在审计日志边界的**下游**，作为一个可替换的"客户端方言插件"，而不是路由器二进制的一部分。

#### 3.12.5 发现（性能）[中]：全内存聚合的内存模型

- `AnalyzeSessions` 第一遍把**所有记录**的 `ReqInfo` 常驻内存（供第二遍 `path:line` join）。
- 每条 record 的原始 `dur/ttft/stream/inTok/outTok` 被 push 进 ~6 个 bucket 的切片。
- `RequestRows` 每条记录一个 struct。

设计文档自己承认"千万级约 640MB，偏紧"。对于日均 1–2GB 审计文件的 agent 场景，这个上限并不遥远。

**做得好的一点 [强]**：`attach()` 里的 max-LCP 选父用 `parentWindow` 限制了候选窗口，是 `O(n·window·m)` 而不是朴素的 `O(n²·m)`。这是一次正确的优化（对应最近那次 "cut report runtime ~70%"）。

**改进方向（给出，但标注为需权衡）**：
- **选项 A（保精确）**：利用输入按时间有序的性质，做流式分桶——跨过日界即 finalize 并释放该日 bucket 的原始切片。保持真百分位，内存从 O(总记录) 降到 O(单日记录)。
- **选项 B（换近似）**：t-digest / 固定容量蓄水池，O(1) 内存拿到 ±1% 的 p50/p95。但设计文档把"真百分位"列为 Format 10 的核心不变式，这是**有意识的产品选择**，不应轻易推翻。

**建议 A，明确不建议 B**——除非将来真的撞到内存墙。

#### 3.12.6 发现（简洁性）[弱]：`Build.func8` = 9,744 字节机器码

单个匿名闭包的编译产物比 `tryOne` + `Serve` 加起来还大。这是 3.12.2 那个根因（bucket 类型爆炸 → 巨型 add 闭包）的直接体征。修了 3.12.2，这个自然消失。

---

### 3.13 `internal/diagnose`(558) / `internal/replay`(452)

**做得好的 [强]**：
- **二者都不新造路由/协议逻辑**，复用 `adapter.BuildRequest` 和 `router.NewUpstreamClient`，保证"诊断/回放看到的"与"真实流量会发生的"字节级一致。这个约束被写进包注释并被遵守，是对的。
- `diagnose` **代理感知**（走代理的 provider 跳过直连 DNS/TLS 检查）——避免把"只能通过代理访问"的健康 provider 系统性误报为故障。这个洞察很实际。
- `replay` 在 `FilterClientHeaders` 之外**再按 `IsCredentialHeader` 剔除一遍**——因为审计记录里存的是打码占位符，两张表故意不同源。这个细节抓得很准。
- `replay -ts` / `-detail` 两种定位方式取代纯 `-line`，理由（行号在真实排障工作流里拿不到）正确。

**发现（正交性）[中]**：`replay -> server` 这条依赖，唯一目的是拿 `FilterClientHeaders`。结果是一个 CLI 调试工具把整个 HTTP server 包拖进依赖树。

修法：把 header 黑名单 + 过滤函数下沉到一个极小的包（或 `core`）。`server` 和 `replay` 都依赖它，`replay -> server` 这条边消失。**~15 分钟的机械改动。**

---

### 3.14 `cmd/vmr`（870 行）

**现状**：8 个子命令 + 配置摘要打印 + banner。

**发现（简洁性）[中]**：`main.go` 里约 180 行是展示逻辑——`logConfigSummary`(89) + `fmtMaxContextTokens` + `fmtCapabilities` + `providerProxyEntries` + `providerProxyLines`。`cmdStart` 本身 140 行。

八个子命令共处一个文件，且每个都自己做 flag 解析 + 配置加载 + 错误包装。拆成 `main.go`（分发）+ 每命令一文件，是纯机械操作。

**发现（简洁性）[弱]**：`cmdCheck` / `cmdStart` / `diagnose` 三处各自实现"打印路由表"的格式化（排序逻辑已经统一到 `EffectiveOrder()`）。设计文档 §14 已论证"剩下的纯格式化差异不值得再抽象"——**同意，不动**。

---

### 3.15 文档与代码漂移 [强]

设计文档 v3 是一份高质量文档，但本轮发现至少两处它与代码已经脱节，而且**恰好都在最不该脱节的地方**：

| # | 文档陈述 | 代码实际 | 危害 |
|---|---|---|---|
| 1 | §14 决策表："`countNested` 在 `config`（未导出）、`cmd/vmr`、`internal/diagnose` **各有一份**…导出它只为省 7 行…不值" | `config.CountNested` **已导出**，三处全部调用它。文档论证的那个现状**已经不存在** | §14 的全部价值在于"下次有人盯着它犹豫时不必重新论证"。一条与现实不符的条目会让读者对整张表失去信任 |
| 2 | §11 决策表："router 主流程（`router.go`）**约 550 行**…**若主流程显著变大，说明抽象错了**" | `router.go` = **948 行**（+72%） | 这是项目**自设的架构 tripwire**，已经触发但无人察觉，因为它只是文档里的一句话，没有任何自动检查 |

第 2 条尤其值得重视：**项目有明确的架构不变式，却没有把它变成可执行的检查。** 这本身就是一个可以修的流程缺口——一个 20 行的测试就能把它变成 CI 可见的红灯。

---

## 4. 横向根因归纳

把 §3 的散点收敛成 5 个根因。这一步很重要：**修根因比修症状划算得多**。

### 4.1 根因一：Endpoint 是「数据结构」而非「已解析的运行时对象」[强 · 最高 ROI]

`core.Endpoint` 在 `BuildSnapshot` 里构造一次，之后**永不修改**。但热路径上反复从它**重新推导**出恒定不变的值：

| 派生值 | 推导成本 | 每请求调用次数（N 个端点） |
|---|---|---|
| `ep.HealthKey()` | **sha256(APIKey)** + hex 编码 + 3 次字符串拼接 | **2N + 3** |
| `snap.clientFor(ep)` | 字符串拼接 + map 查找 | 每次 attempt |
| `adapter.Get(ep.AdapterType)` | **RWMutex.RLock** + map 查找 | 每次 attempt |
| `ep.Name()` | 2 次字符串拼接 | 每次成功响应 |
| `strategy.Eligible()` | **RWMutex.RLock** | 每端点每请求 |

`HealthKey()` 的调用点（`router.go:335` 健康过滤循环、`:411` Acquire、`:465` findByHealthKey、`:557` tryOne、`:421` Sticky.Set、`probe.go:33`、`server.go:371`）意味着一个 4 端点的路由，单次成功请求要算 **~11 次 SHA-256 + 11 次 hex + 33 次字符串拼接**，全部产出完全相同的字符串。

**根本修法**：让 `BuildSnapshot` 产出**完全解析好**的 Endpoint：

```go
type Endpoint struct {
    // ...现有配置字段...

    // 快照期解析一次，运行期只读
    healthKey string
    name      string
    adapter   adapter.Adapter
    client    *http.Client
}
```

热路径上的推导工作**归零**。同时 `strategy` 的 conditions 与 `adapter` 的 registry 在 `init()` 后冻结，用 `atomic.Pointer` 或直接冻结切片替代 RWMutex。

**这是全仓库 ROI 最高的一处改动**：改动局部、无行为变化、有 4,000 行 E2E 测试兜底、彻底消除一整类浪费。

### 4.2 根因二：同一份请求体被独立扫描约 10 遍 [强]

统计 `chatHandler` 里一个**典型的无图无文档 agent 请求**（最常见情况）在发往上游前，body 被完整或近似完整遍历的次数：

| # | 位置 | 操作 |
|---|---|---|
| 1 | `server.go:217` | `json.Unmarshal(body, &probe)` —— **全量 JSON tokenize，只为取 `model` 和 `stream` 两个顶层字段** |
| 2 | `imgprep.Downscale` | `HasImageMarker` — `bytes.Contains` |
| 3 | `facts.go:68` | `HasNonEmptyTopLevelArray(body,"tools")` — 结构化扫描 |
| 4 | `facts.go:69` | `EstimateTextTokens(body)` — 全字节循环 |
| 5–8 | `facts.go:99` | `documentMarkers` 4 个 `bytes.Contains`（**全部未命中时 = 4 次完整扫描**） |
| 9 | `router.go:384` | `SessionFingerprint` — 结构化扫描 + 2× md5 |
| 10 | `BuildRequest` | `RewriteModel` — 顶层扫描 + splice（每次 attempt） |

**约 10 遍。** 对一个 500KB 的 agent 请求就是 ~5MB 内存流量。

其中第 1 条最刺眼：**这个代码库自己实现了一个高效的顶层字段字节扫描器**（`adapter.topLevelValues`，设计文档还专门夸过它比 unmarshal 快一个数量级），却在**每个请求的第一步**用全量 `json.Unmarshal` 去取两个顶层字段。

有意思的是，代码里已经有了正确的思想——针对图片，注释明确写了"**一次探测，三处复用**"（`imageCount` 同时喂给 audit、`HasImage`、token 估算）。**这个原则是对的，只是没有推广到其余信号上。**

**根本修法**：一个**融合的单遍结构化提取器**，一次扫描产出 `model` / `stream` / `tools` 存在性 / 图片块 / 文档 payload / token 估算。10 遍 → 约 2 遍（1 次提取 + 1 次 splice）。

需要注意的风险：`HasImage` 喂的是**无 fallback 的硬性淘汰条件**（误判会导致 503），历史上出过一次真实事故。因此融合时必须**保持 imgprep 那套结构化遍历的判定语义不变**，只是把其他信号搭车提取。**这需要仔细做，不属于"简单易行"那一档**——见第 6 节"结构性简化"2.2 的范围取舍。

### 4.3 根因三：分析工具与路由器共处同一模块边界 [强 · 最高结构性收益]

- `report` = 源码 45% / 二进制自有代码 55%；加上 replay/diagnose = 52% / 65%。
- `report` 只依赖 `{audit, core}`，与路由核心**零耦合**——它在结构上已经是孤岛。
- `report` 是**变更最频繁**的部分（最近 5 次提交有 3 次是 report）。
- `report` 是**唯一**包含启发式与第三方客户端方言（OpenClaw）的部分。
- `report` 是**唯一**受"零依赖、纯 Go、单二进制"约束拖累的部分——它想用 DuckDB/Parquet 就必须引入 cgo，与产品定位冲突，于是只能手写 6,400 行。

这四条加起来说明：**它们是两个产品**。它们唯一真实的契约是 `audit.Record` 的 JSONL schema。

### 4.4 根因四：厂商特定 quirk 长在通用路径上 [中]

`response.go` 中 ~500/657 行服务于 MiniMax-M3 的两个 bug，且全局嗅探生效。这是**会随接入厂商增长而累积**的债，不是一次性取舍（详见 3.8）。

### 4.5 根因五：架构不变式只存在于文档，没有可执行检查 [强 · 成本极低]

- `router.go` 550 行的自设上限已被突破 72%，无人察觉。
- §14 决策表里有条目描述的现状已经不存在。
- `report -> core` 这条"分析层依赖运行时格式化函数"的边，没有任何机制阻止它继续加深。

**修法极便宜**：几个 Go 测试即可。

```go
func TestArchitecture_ImportBoundaries(t *testing.T)  // report 不得依赖 router/server/config
func TestArchitecture_CoreFileSizes(t *testing.T)     // router.go 超过 N 行则失败
```

不变式一旦可执行，就不会再悄悄漂移。

---

## 5. 从第一性原理重看：目标架构

### 5.1 vmr 其实是三个产品

| # | 产品 | 特征 | 当前位置 |
|---|---|---|---|
| **A. 路由器** | 常驻进程；延迟敏感；正确性关键；必须永不崩溃；变更应当**罕见** | 确定性逻辑，零启发式 | `router`/`server`/`adapter`/`health`/`strategy`/`sticky`/`config`/`imgprep` ≈ 5,100 行 |
| **B. 审计日志** | 一份**不可变的事实记录** + 其 schema | 是 A 与 C 之间**唯一**应有的契约 | `internal/audit` ≈ 660 行 |
| **C. 分析工具** | 批处理 CLI；无延迟要求；启发式密集；变更**频繁**；包含客户端方言 | 消费 B 的产物 | `report`/`replay`/`diagnose` ≈ 7,400 行 |

C 与 A 的关系，在**种类上**等同于 `jq`、DuckDB、一个 Python 脚本与 A 的关系。它现在之所以在同一个二进制里，是历史演进的结果，不是设计的结论。

### 5.2 目标分层

```
┌─────────────────────────────────────────────────────┐
│  A. 路由运行时（唯一的常驻进程）                        │
│                                                     │
│   server ──► router ──► adapter ──► upstream        │
│                │                                     │
│         selection: health + condition + sticky + sort│
│                │                                     │
│                └──► emit: AuditRecord                │
└──────────────────────────┬──────────────────────────┘
                           │
                  ╔════════▼════════╗
                  ║  B. 审计 schema  ║  ← 唯一契约（JSONL）
                  ║  (audit.Record)  ║
                  ╚════════┬════════╝
                           │
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
   C1. vmr report     C2. replay/diagnose   C3. jq / DuckDB
   （含客户端方言插件）    （调试工具）          （用户自己的工具）
```

三条设计规则：

1. **A 不得依赖 C**（今天已成立）。
2. **C 只能依赖 B**（今天不成立：`report -> core` 拿了格式化函数）。
3. **客户端方言（OpenClaw/Claude Code 特有的解析）只能存在于 C 内部一个显式命名的子模块**，不得散落。

### 5.3 关键决策与 options

#### 决策 1：`report` 要不要拆成独立二进制？

| 选项 | 做法 | 优点 | 缺点 |
|---|---|---|---|
| **O1** | 拆成 `cmd/vmr-report` 独立二进制 | 路由器二进制瘦身 65%；边界物理隔离 | **破坏"单二进制"这一明确的产品定位**；发布/分发多一份 |
| **O2** | 同一二进制，但 `report` 独立成仓库内的子 module | 结构上强制边界 | 不减二进制；Go multi-module 在单仓库里有工具链摩擦 |
| **O3** | 保持现状，用**导入图测试**锁定边界 | 成本近乎为零；无任何破坏 | 不减二进制；靠测试而非结构约束 |
| **O4** | build tag：`go build -tags lean` 产出精简路由器 | 默认单二进制不变；需要时可瘦身 | 多一个构建维度；tag 组合要测 |

**建议：O3（立即） → 视情况 O4（可选）。明确不建议 O1。**

理由要说清楚：**"二进制从 12MB 降到 ~7MB" 不是一个真实的收益**——磁盘廉价，常驻进程的 RSS 也不会因为未执行的代码而显著增加。为了这个数字牺牲"单二进制"这一核心产品定位，是被"分离"这个词本身诱导的过度设计。

真正的收益是 **(a) 认知与维护上的分离、(b) 防止耦合继续加深、(c) 构建/测试反馈变快、(d) 让 OpenClaw 方言的位置变得显眼**。这四项 **O3 花几乎零成本就能拿到 (a)(b)(d)**。

#### 决策 2：`report` 的聚合层要不要改用 SQL 引擎？

诱惑很大：`report` 本质是一个手写的 OLAP cube，DuckDB 能用 `read_json_auto` + `GROUP BY` + `quantile_cont` 在几十行 SQL 里做完 3.12.2 那 1,200 行的活，而且快得多。

**但这条路当前走不通**，理由必须讲明：`go-duckdb` 需要 cgo，与 vmr "纯 Go、无 cgo、单二进制自包含"的定位直接冲突（这个定位在设计文档 §4.2 里是明确的，连选 zstd 库都特意挑了纯 Go 实现）。

| 选项 | 说明 |
|---|---|
| **P1** | 保持手写 Go，但按 4.3 做泛型化重构（1,200 → 300 行）。**保守、确定、无依赖代价** |
| **P2** | `vmr report` 只输出一份规整的 `vmr-requests.jsonl` / Parquet，把聚合与渲染交给用户的 DuckDB/Python。**最 Unix，但砍掉了 vmr report 现有的核心产品价值** |
| **P3** | 若将来 `report` 真的独立成 C3 那一层的独立工具（甚至换语言），它就不再受 vmr 的 cgo 约束，届时 SQL 路线自然可行 |

**建议：P1（本轮）。把 P3 作为长期可能性记录，不作为当前目标。** P2 会丢掉 agent 会话分组这个真实差异化能力，不建议。

#### 决策 3：MiniMax quirk 如何处理？

| 选项 | 说明 |
|---|---|
| **Q1** | 端点级 `quirks: [...]` 声明式配置 | 架构最干净，但设计文档 §5.5 已明确否决（新增配置概念、用户填不对） |
| **Q2** | **代码层面分离，用户可见行为完全不变**：`respStream` 只保留「切事件 + model 改写 + DONE 策略」，quirk 修复抽成独立可组合的 `responsefix` 层，仍由自动嗅探触发 | 拿到架构收益，不承担配置负担；将来要做 Q1 时接缝已在 |
| **Q3** | 不动 | 债继续累积 |

**建议：Q2。** 它绕开了 §5.5 否决的真正理由（配置面复杂度），同时解决了真正的问题（500 行厂商逻辑与通用路径纠缠）。

---

## 6. 重构记录

本节记录第 4 节根因分析落地后的架构现状——按当初的两轮划分保留编号，方便与代码里的注释互相对照，但每一项都写的是**现在是什么样、为什么这样设计**，不是"计划 vs 实际"的对比。

### 第一轮：消除确定的浪费，立起护栏

| # | 现状 | 角度 | 为什么这样做 |
|---|---|---|---|
| 1.1 | `core.Endpoint` 的 `healthKey`/`name` 在 `router.BuildSnapshot` 里通过 `Freeze()` 预计算一次，存成字段；请求热路径上的每次调用都是纯字段读取 | 性能 | `HealthKey()` 原来每次调用都重新 `sha256.Sum256(APIKey)`——不可变的派生值理应只算一次。未走 `BuildSnapshot`（例如测试里直接构造 `&core.Endpoint{}`）时退化为按需计算，不强制要求调用 `Freeze` |
| 1.2 | `strategy.conditions`、`adapter.registry` 用 `atomic.Pointer` 做 copy-on-write，读路径无锁；写路径（`RegisterCondition`/`Register`，只在各文件 `init()` 里调用一次）用独立 mutex 序列化 | 性能 | 这两个注册表实际上"写一次、读千万次"，RWMutex 的读锁开销是纯浪费。写路径的 mutex 不是可选项——早期实现漏掉它，在并发写入下会静默丢更新（`TestGetConcurrentWithRegister`/`TestEligibleConcurrentWithRegisterCondition` 在 `-race` 下锁定这条不变式） |
| 1.3 | `audit.Logger.Write` 用 `sync.Pool` 复用编码缓冲区，JSON 编码在锁外完成，只有最终字节写入文件时持锁 | 性能 | 审计记录可以到几 MB（长 agent 会话），编码是 CPU 密集操作；放在锁内会把并发请求的 JSON 编码串行化，比省下的那点分配更亏。`sync.Pool` 拿到摊销分配收益的同时不引入这个回退 |
| 1.4 | `internal/archtest` 用可执行测试固化两条不变式：`report` 包的 import 边界（不依赖 `router`/`server`/`config`）、`router.go` 的行数上限（700 行） | 正交/流程 | 之前这类约束只写在文档注释里，`router.go` 曾在无人察觉的情况下从约 550 行长到 948 行才被发现。写成测试就不会再悄悄漂移 |
| 1.5 | `internal/router` 按职责拆成 `router.go`（Serve/tryOne 主流程）、`snapshot.go`（Snapshot 构建/安装）、`limiter.go`（并发闸）、`transport.go`（upstream client + copyFlush）、`logfmt.go`（日志行格式化）、`response.go`+`responsefix.go`（响应归一化，见 2.3） | 简洁 | `router.go` 一度膨胀到 948 行；按职责切开后核心 failover 循环重新变得可读，每个文件单一职责 |
| 1.6 | `internal/fmtutil` 独立出 `FmtBytes`/`FmtTokens`/`FmtSeconds`；`core.StickyBackstopTTL` 是唯一权威值（`sticky.BackstopTTL` 是它的别名）；`core.FilterClientHeaders` 取代原来 `server` 包内的同名函数 | 正交 | 三处显示格式化函数不是路由域类型，不该拖着 `report` 一起依赖 `core.Endpoint` 这些类型；`config` 校验 `sticky_ttl` 时不该为了一个常量导入整个 `sticky` 包；`replay` 重建请求头时也不该为了一个函数导入整个 HTTP server 包 |
| 1.7 | `cmd/vmr` 按子命令拆成 `cmd_check.go`/`cmd_start.go`/`cmd_status.go`/`cmd_report.go`/`cmd_dirs.go`/`cmd_diagnose.go`/`cmd_replay.go` + `summary.go`（配置摘要渲染，start/check 共用），`main.go` 只剩 dispatch/usage/adapter 注册 | 简洁 | 原 870 行单文件混杂 7 个子命令的实现，找一个子命令的逻辑要在整个文件里翻 |

全部由现有的 E2E 测试（`internal/server`/`internal/router` 打在 HTTP 边界上的用例）兜底，用户可见行为无变化。

### 第二轮：结构性简化

| # | 现状 | 角度 | 为什么这样做 |
|---|---|---|---|
| 2.1 | `internal/report/metrics.go` 用一个共享的 `finishMeasures` 函数计算 6 个 Row 类型（`Row`/`HourRow`/`EndpointRow`/`ClientRow`/`WorkloadRow`/`SessionRow`）共有的部分（真百分位、`fresh tokens`、`cache_efficiency`）；6 个 `finishX` 函数各自只保留自己独有的字段计算（`SuccessRate`/`Availability`/`ToolCallRate`/`ContextGrowth` 等），struct 声明和 JSON 输出结构不变 | 简洁 | 6 个 Row 类型字段高度重叠，但同名字段（如 `CacheEfficiency`）在不同类型上的 `omitempty` 标签并不一致——真正统一成一个共享嵌入结构体会在零值场景悄悄改变 JSON 输出。只统一计算逻辑、不碰字段声明，是唯一能保证输出逐字节不变的做法；已用 11 天生产日志验证重构前后 `vmr-report.md`/`.json`（除时间戳）完全相同 |
| 2.2 | `internal/server/server.go` 的 `chatHandler` 用 `adapter.TopLevelProbe` 一次结构化扫描同时取出 model/stream/tools-是否非空三个顶层字段，取代原来的 `json.Unmarshal` 反射解码加一次独立的 tools 顶层扫描 | 性能 | `json.Unmarshal` 为了两个字段做了一次完整的反射式结构遍历；`TopLevelProbe` 复用 `adapter` 包已有的字节级顶层扫描器（`topLevelValues`），语义上与 `encoding/json` 对 null/类型不匹配的处理完全一致（有测试直接对照验证）。`imgprep.HasImageMarker`/`EstimateTextTokens`/`estimateDocumentTokens` 三个扫描没有并进来——`HasImage` 喂的是无 fallback 的硬性淘汰条件，历史上出过图片误判导致 503 的真实事故，融合的收益不足以承担这个风险 |
| 2.3 | `internal/router/response.go` 只保留响应归一化的通用状态机（事件切分、`model` 字段改写、`[DONE]` 策略、缓冲/直通决策）；MiniMax 特定的 quirk 知识（`<think>` 剥离、"Thinking Process:" 剥离、soft-block 检测的具体 marker）单独放在 `responsefix.go` | 正交/简洁 | 通用传输机制和厂商专属知识长在同一个文件里，会让后续每个新 quirk 都往同一处堆。缓冲/直通的状态机本身是通用机制（任何未来的 quirk 都可能需要缓冲），留在 `response.go`；只有"什么样的内容算 quirk"这部分知识移出去 |
| 2.4 | `router.tryOne` 只做编排（拿 adapter、`BuildRequest`、发请求、分派），>=400 响应的处理在 `handleErrorResponse`，2xx 转发在 `forwardSuccess`；`audit.Attempt` 有一组 nil-safe 的 `SetXxx` 方法（`SetBuildError`/`SetRequest`/`SetErrorResponse`/`SetSuccessResponse` 等），调用方不需要每次都判 `att != nil` | 简洁 | 原来 `tryOne` 一个函数扛下整个请求-响应生命周期外加 15 处 `if att != nil` 判断，读起来要在心里维护好几层状态。拆开之后每个函数只对应一种结局 |
| 2.5 | `transport.go` 的 `copyFlush` 仍然是 goroutine + channel 的实现，没有改用 `net.Conn.SetReadDeadline` | 性能 | `http.Response.Body` 不暴露 per-body 的 `SetReadDeadline`；唯一能拿到 deadline 的路径是在 `DialContext` 里包一层自定义 `net.Conn`，每次 `Read` 前重置 deadline——但这样 deadline 会覆盖整条连接生命周期（含 TLS 握手、响应头阶段），与现有的 `TLSHandshakeTimeout`/`ResponseHeaderTimeout` 语义重叠，且只能靠真实 TCP 往返测试才能验证。收益（少一个 goroutine/channel）不足以承担这个语义变化的风险和验证成本 |
| 2.6 | `internal/report/aggregate_render.go` 用 `mdTable` helper（`newTable`+`row`）统一约 20 处 markdown 表格的表头/分隔符/行拼接 | 简洁 | 每处手写 `w("| h1 | h2 |\n|---|---|\n")` 加逐行 `Fprintf` 是纯重复的样板；`mdTable` 只收掉这层拼接，不做成通用模板引擎——每张表的列格式化逻辑仍然不同，硬套模板反而更难读。**用真实生产日志逐字节比对重构前后输出时，抓到两个原来就存在、和这次重构无关的 bug**：`topErrorClass`/`topErrorClassShort` 对 `ErrorClasses` map 直接 `range` 取最大值，同计数并列时结果随 Go 的随机 map 遍历顺序而变——和之前修过的 `sort.Slice` 缺 tie-break 是同一类问题，只是当时的修复没扫到这两个函数；现在统一用 `sortedKeysInt` 排序遍历，并列取字母序靠前者，已补两个回归测试锁定 |

### 长期演进方向（按需触发，非必须）

| # | 动作 | 触发条件 |
|---|---|---|
| 3.1 | **OpenClaw 方言收拢**成 `report/dialect/` 显式子模块 | 接入第二个 agent 客户端时 |
| 3.2 | **report 流式分桶**（跨日 finalize 释放） | 真的撞到内存墙时 |
| 3.3 | **`lean` build tag** 或独立 report 二进制 | 有明确的分发/体积诉求时 |
| 3.4 | **`Filter` 统一接口**（合并 Dimension/Condition/WithinContext） | 出现第二个 soft filter 时 |
| 3.5 | **端点级 `quirks:` 声明** | 出现第三个厂商 quirk，或发生真实误伤时 |
| 3.6 | **审计写入异步化**（channel + 单写协程） | 审计落盘成为实测瓶颈时 |

**每一项都写明了触发条件——在条件满足前做，就是投机性泛化。**

---

## 7. 明确不建议做的事

一份诚实的 review 必须说清楚哪些"看起来该优化"的东西**不该动**。以下每一项我都实际评估过，结论是**收益低于扰动成本**：

| 项 | 为什么不动 |
|---|---|
| `health.Registry` 的全局互斥锁分片化 | 实测量级约 600 次加锁/秒，锁内是纳秒级 map 访问。纯粹的过度设计 |
| report 百分位改 t-digest/蓄水池 | "真百分位"是 Format 10 有意识的产品不变式，不是疏忽。除非撞到内存墙，否则不该用精度换内存 |
| 用 `text/template` 重写 Markdown 渲染 | 条件列、对齐、`⭐`/`¹`/`⚠️low-n` 脚注在模板里更难读。没把握是净胜项。表格 builder（2.6）是更有据的中间方案 |
| 合并 `Dimension`/`Condition`/`WithinContext` | soft 语义目前只有一个成员。抽象一个只有一个实现的接口违反 YAGNI。设计文档 §11 的论证仍然成立 |
| 统一 `cmdCheck`/`cmdStart`/`diagnose` 的路由表打印格式 | 排序逻辑已统一到 `EffectiveOrder()`；剩下的纯格式化差异抽象成本高于收益。§14 判断正确 |
| 消除 `imgprep.ImageInfo` → `audit.ImageInfo` 的 20 行字段抄写 | 这 20 行换来 `imgprep` 不依赖 `audit`。包边界的合理代价 |
| 把 `report` 拆成独立二进制 | "12MB → 7MB" 不是真实收益，却要牺牲"单二进制"这一核心定位。O3 用近乎零成本拿到大部分实际好处 |
| 引入 DuckDB/cgo 做聚合 | 与"纯 Go、无 cgo、自包含"定位直接冲突。这个定位是对的，不该为聚合代码量让步 |
| 为 `router → audit` 上事件总线/消息队列 | 当前的可变 Record 传递在性能上是最优的。收益是可读性，代价是热路径开销 + 大重构。用 nil-safe helper 收敛噪音即可（2.4） |
| 每 endpoint rpm 限流、`/metrics`、weight/latency 维度 | 这些在路线图上，是**新功能**，不是架构改进。本次 review 范围外 |

---

## 8. 附录：本轮实测事实清单

所有结论所依据的、本轮实际验证过的事实（非推测）：

1. 源码 14,212 行 / 测试 13,762 行；`internal/report` 占源码 45.0%。
2. `go tool nm -size`：`report` 占 vmr 自有代码符号 54.8%；`report+diagnose+replay` 占 64.7%。
3. `internal/report.Build.func8` 单个闭包 9,744 字节机器码，大于 `tryOne`+`Serve` 之和。
4. `go list` 实测依赖图无环；`report` 仅依赖 `{audit, core}`。
5. `config -> sticky`、`replay -> server` 两条边确实存在，且各自只为一个符号。
6. `HealthKey()` 非测试调用点 7 处，其中 5 处在每请求路径上；实现含 `sha256.Sum256`。
7. `router.go` 948 行 / `response.go` 657 行，对照设计文档 §11 自设的"约 550 行"。
8. `config.CountNested` 已导出且被 3 处共用，与设计文档 §14 的描述不符。
9. `report` 中 OpenClaw/Claude Code 特定逻辑分布在 `session.go`(9 处)、`aggregate.go`、`metrics.go`、`requests.go`、`detail.go`。
10. 6 个 bucket 类型 + 6 个 `finishX`，度量字段重复 4–6 次。
11. ingress 路径对 body 的完整/近似完整遍历约 10 次（含 `json.Unmarshal` 全量 tokenize）。
12. `internal/server` 测试 3,960 行，为 HTTP 边界上的全栈 E2E（failover/条件路由/主动探测/imgprep/headers/audit/内容策略/并发/OpenClaw 场景）。
13. `attach()` 的 max-LCP 选父有 `parentWindow` 上界，非 O(n²)。
14. `go build ./... && go vet ./... && go test ./...` 全绿。
15. `.gitignore` 覆盖完整，无敏感产物被跟踪。

---

## 9. 一页纸总结

**这个代码库的工程质量高于平均水平。** 协议模型（让非法配置无法表达）、错误分类的实测驱动、health 状态机的边界、字节级 splice、GIF 解压炸弹的处理方式、"已识别但不改"决策表这个实践本身——都是成熟判断的产物。重构没有重写它，只做了三件事：

1. **把不可变的派生值算一次**（根因 4.1 + 4.2）。`Endpoint.Freeze()` 让 `HealthKey()` 从每请求一次 SHA-256 变成字段读取；`adapter`/`strategy` 的注册表从 RWMutex 改成 copy-on-write 的 `atomic.Pointer`；`adapter.TopLevelProbe` 把 model/stream/tools 的提取收进一次结构化扫描。这些改动无用户可见行为变化，全部由现有 E2E 测试兜底。

2. **给 `report` 一条可执行的边界，而不是拆二进制**（根因 4.3）。`internal/archtest` 用 import 边界测试固化"`report` 只依赖 `{audit, core}`，不依赖 `router`/`server`/`config`"这条不变式；`report` 内部 6 套 bucket 的公共计算逻辑收进 `finishMeasures`，但 struct 声明和 JSON 输出结构原样保留——真正的 `Bucket[K]` 泛型容器会改变输出的 `omitempty` 行为，收益不足以承担这个风险。

3. **让架构不变式可执行**（根因 4.5）。`router.go` 的行数上限（700 行）和 `report` 的 import 边界都写成了 `internal/archtest` 里的可执行测试，不再只是文档里的一句话。

第 4 项，`response.go` 里厂商特定 quirk 与通用路径纠缠（根因 4.4），拆成了 `response.go`（通用状态机）+ `responsefix.go`（MiniMax quirk 知识）——只做代码层分离，用户可见行为不变，也没有触碰设计文档 §5.5 当初否决端点级配置的理由。

**最大的风险缓释因素**：`internal/server` 那几千行打在 HTTP 边界上的 E2E 测试，加上用真实生产日志对 `report` 输出做逐字节比对——后者在这轮重构里实际抓到两个真实 bug（表格渲染的 `%` 格式化损坏、`topErrorClass` 的 map 遍历非确定性），都是测试覆盖不到、只有对真实数据跑一遍才会暴露的那类问题。
