# D1 路由核心域深度审查报告

**审查范围**：
- `internal/router/`（failover 循环、候选构建、流式转发、并发闸、路由头、探针分派、软屏蔽等）
- `internal/health/`（健康状态机、退避算法、半开探针单飞）
- `internal/respnorm/`（响应流归一化状态机、MiniMax quirk 修复、用量嗅探）
- `internal/sticky/`（会话亲和注册表、容量上限与淘汰）
- `internal/server/`（HTTP 入口、鉴权、RequestFacts 提取、审计录制、健康/状态/日志端点、慢连接防护）
- `internal/strategy/`（Dimension 排序与 Condition 淘汰）
- `internal/core/`（CanonicalRequest、Endpoint、ErrorClass 等共享实体）
- 依据设计文档：`docs/VirtualModelRouter_Design_v4_Core.md`、`docs/KNOWN_ISSUES.md`

---

## 一、审查任务详细结论

### 任务 1: failover 循环正确性（`internal/router/`）

#### 1.1 核心循环流转与候选耗尽 / `max_attempts` 语义
- **源码锚点**：`internal/router/router.go:64-162` (`ServeWithSnap`), `internal/router/candidates.go:37-104` (`buildCandidates`), `internal/router/routehdr.go:88-100` (`noCandidatesMessage`)
- **审查分析**：
  1. **初始化与前置校验**：`ServeWithSnap` 在入口判断 `snap == nil` 并安全返回 503（`router.go:68-76`）；校验虚拟模型在对应协议下是否存在（`router.go:78-90`），若同名模型存在于其他协议入口则给出精确的 404 重定向提示（`otherProtocolFor`）；执行全端点 keyless 检测（`rejectIfAllKeyless`，`router.go:92-94, 150-164`），避免空 key 导致全员 401 上游冷却。
  2. **候选构建管线**：`buildCandidates` 严格遵循「健康过滤 → 硬性条件淘汰 → 上下文长度估算过滤（带非空兜底 fallback）→ Pinned 路由过滤 → 多键稳定排序 `strategy.Sort` → 额度感知重排 `reorderByQuota` → 会话粘性置顶 `moveToFront`」的固定管线（`candidates.go:37-104`）。
  3. **循环推进与 `max_attempts` 截断**：循环迭代 `cs.endpoints`，每次迭代前检查 `snap.Cfg.MaxAttempts > 0 && attempts >= snap.Cfg.MaxAttempts`，达到上限立即 break（`router.go:108-110`）。
  4. **半开单飞守卫**：进入尝试前调用 `rt.Health.Acquire(ep.HealthKey(), time.Now())`（`router.go:118-120`），与 `buildCandidates` 中的 `Classify` 双重加锁守卫，防止并发请求击穿半开端点。
  5. **失败结果保真回传**：全部候选失败后，若存在实际的上游 HTTP 错误响应（`last != nil`），通过 `copyRespHeaders`（过滤 hop-by-hop 头）原样将上游的状态码、响应头（含 `Retry-After`、`x-ratelimit-*` 等）和响应体回传给客户端，并在最外层注入 `X-VMR-Attempts` 和 `X-VMR-Failover`（`router.go:150-155`）；若无任何上游 HTTP 响应（如全员网络建连失败或全员冷却），回退 503 `vmr_no_candidates` 并输出精确原因（`router.go:157-158`）。
- **判断**：【设计取舍 / 实现正确】逻辑闭环且完全符合设计规范。
- **影响范围**：全局请求调度与故障转移。

#### 1.2 错误分类驱动的冷却与首字节前 Failover 约束
- **源码锚点**：`internal/router/router.go:307-368` (`handleErrorResponse`), `internal/router/router.go:370-476` (`forwardSuccess`), `internal/router/softblock.go:28-66` (`checkSoftBlock`)
- **审查分析**：
  1. **首字节前 failover 约束**：上游响应状态码 `>= 400` 时，在 `handleErrorResponse` 中读取并分类错误。只有未向客户端写入首字节前允许继续 failover；一旦进入 `forwardSuccess`（`w.WriteHeader(resp.StatusCode)` 执行后），响应即向客户端提交，后续发生断流只能标记 `TRUNCATED` 并 panic `http.ErrAbortHandler`，绝不回退重试（`router.go:370-476`）。
  2. **非惩罚类错误（零冷却切换）**：
     - `ErrContent`（内容合规拦截）、`ErrContextLimit`（模型上下文窗口超限）、`ErrQuirk`（端点专属协议格式约束拒绝）均被判定为「当前请求特定属性，非端点故障」，调用 `Health.ReportNeutral(key)` 释放可能占用的探针槽位，不加深退避，日志记录 `(no cooldown)` 并继续 failover（`router.go:343-348`）。
     - `ErrClient`（客户端传参错误）：判定所有端点均无法处理，调用 `Health.ReportNeutral(key)`，就地向客户端原样写回上游 4xx 响应，返回 `done=true, success=false` 终止 failover（`router.go:350-362`）。
  3. **故障类错误（加深退避冷却）**：
     - `ErrAuth` / `ErrEndpoint` / `ErrRateLimit` / `ErrTransient` 均进入 `Health.ReportFailure(key, class, parseRetryAfter(resp.Header), time.Now())` 计算冷却时间（`router.go:364-367`）。
  4. **软屏蔽（Soft-Block）Failover**：
     - 在 `softblock.go` 中，仅对非 SSE、非压缩且配置了 `SoftBlockFailover: true` 的 2xx 响应进行 peek（≤64KB）。命中合规屏蔽标记且助手有效回复为空/短拒绝文本时，归入 `ErrContent` 进行零冷却 failover（`softblock.go:58-65`）。
     - 若 peek 过程中上游断流或超时，通过 `readCloser{io.MultiReader(bytes.NewReader(peek), errReader{readErr}), resp.Body}` 包装还原 Body，确保读错误原样暴露给 `forwardSuccess` 进入 `TRUNCATED` 路径，杜绝静默假成功（`softblock.go:38-46`）。
- **判断**：【实现正确】严格遵守「首字节后不可 failover」和「请求级错误不惩罚端点健康」的架构铁律。
- **影响范围**：各上游故障/异常拦截场景下的切换行为与健康状态更新。

#### 1.3 `X-VMR-*` 响应头语义
- **源码锚点**：`internal/router/router.go:98, 122, 146, 395-396`, `internal/router/routehdr.go:44-86`
- **审查分析**：
  1. `X-VMR-Route-Reason`：在循环前写入 `w.Header()`，渲染路由决策全貌（如 `pick=order|quota|sticky eligible=N/M cooldown=X conditions=Y ctx_fallback=1`）（`router.go:98`）。
  2. `X-VMR-Failover`：利用 `trail.apply(w.Header())` 在每次尝试**发起前**预先写至 Header Map（`router.go:122`），确保最终成功的尝试在 `forwardSuccess` 内部调用 `w.WriteHeader` 时能够完整携带此前所有失败尝试的轨迹（如 `provider/model:429, provider2/model2:err`）。
  3. `X-VMR-Endpoint` 与 `X-VMR-Attempts`：在响应提交处准确注入命中的端点名与累计尝试次数（`router.go:395-396`）。
- **判断**：【实现正确】Header 注入时机精确，不存在成功后丢失 failover 轨迹的问题。
- **影响范围**：客户端响应调试头信息。

#### 1.4 并发安全、上下文取消与资源泄漏风险
- **源码锚点**：`internal/router/snapshot.go:167-203` (`Install`), `internal/router/transport.go:61-125` (`copyFlush`), `internal/router/router.go:315-321` (`handleErrorResponse`)
- **审查分析**：
  1. **快照并发安全**：`Router.snap` 为 `atomic.Pointer[Snapshot]`。`Install` 内部使用 `installMu` 互斥保护新快照客户端构建、信号量更新、原子交换 `Swap` 以及 `Quota/Health` 注册表修剪（`snapshot.go:167-203`）。在途请求在请求入口（`server.chatHandler`）一次性加载当前 Snapshot 指针并穿透至 `ServeWithSnap`，彻底杜绝单请求跨快照视图撕裂。
  2. **连接池清理**：旧快照被替换后，`Install` 显式调用 `old.clientSet` 中所有客户端的 `CloseIdleConnections()`（`snapshot.go:196-200`），避免空闲连接泄漏。
  3. **上下文取消传播**：
     - 建连与传输期：`ad.BuildRequest` 传入 `r.Context()`。若客户端取消，`snap.clientFor(ep).Do(req)` 返回错误，`tryOne` 识别 `r.Context().Err() != nil`，调用 `ReportNeutral` 释放探针并安全退出，不惩罚端点（`router.go:276-285`）。
     - 流式转发期：`copyFlush` 在 select 中监听 `<-ctx.Done()`，退出时 defer `close(done)` 通知后台读取协程退出（`transport.go:72, 90-91`）。
  4. **超时与 Body 泄漏防护**：
     - 错误响应体读取：配置 `time.AfterFunc(StreamIdle, func() { resp.Body.Close() })` 看门狗并限制 `io.LimitReader(resp.Body, 128KB+1)`，读取完成后停止看门狗并显式关闭 `resp.Body`（`router.go:315-318`）。
     - 成功响应体流式：`defer body.Close()` 确保网络连接回收；`copyFlush` 内部针对每个数据块重置 `idle` 定时器，杜绝上游流挂死（`transport.go:88-124`）。
- **判断**：【实现正确 / 无泄漏风险】资源生命周期管理严密，并发与取消处理健壮。
- **影响范围**：长连接资源占用、高并发稳定性。

---

### 任务 2: 健康状态机（`internal/health/`）

#### 2.1 状态机模型、冷却、退避与抖动
- **源码锚点**：`internal/health/health.go:23-28` (`state`), `internal/health/health.go:146-198` (`ReportFailure`, `retryAfterCooldown`, `backoff`)
- **审查分析**：
  1. **退避曲线设计**：
     - `ErrAuth` / `ErrEndpoint` 走长周期退避：基数 `longBase = 10m`，上限 `longCap = 1h`。
     - `ErrTransient` / `ErrRateLimit` 走短周期退避：基数 `transientBase = 2s`，上限 `transientCap = 5m`。
  2. **`fails` 计数语义与跨曲线重置**：
     - 当连续失败在长短曲线之间发生模式切换时，`s.fails` 重置为 1（`health.go:154-159`）。这避免了前期若干次 5xx 短暂重试将后续出现的 401 认证错误直接放大至 1 小时封顶退避。
  3. **抖动（Jitter）算法**：
     - `backoff` 计算公式为 `d + time.Duration(float64(d)*(0.2*rand.Float64()-0.1))`，即 `±10%` 随机抖动（`health.go:196`）。
     - 抖动作用于已达 ceiling 的数值，防止大规模端点在整点发生同步惊群重试。
  4. **`Retry-After` 异常处理**：
     - `retryAfterCooldown` 优先采用上游指定的 `Retry-After`，但强制执行 `min(retryAfter, longCap)`（`health.go:179-181`），有效防御上游恶意/异常超大 Retry-After 导致端点永久封锁。且该路径不添加随机抖动，严格遵从上游节奏。
- **判断**：【实现正确】与 KNOWN_ISSUES §1.1 完全一致。
- **影响范围**：端点故障恢复节奏与重试退避稳定性。

#### 2.2 半开单飞探测、探针释放与衰减恢复
- **源码锚点**：`internal/health/health.go:57-144` (`Classify`, `Acquire`, `ReportSuccess`, `ReportProbeSuccess`, `ReportNeutral`, `ReleaseProbe`), `internal/router/probe.go:26-118` (`runProbe`)
- **审查分析**：
  1. **后台解耦探测**：在 `candidates.go:41-47` 中，通过 `Health.Classify` 进行原子探测判定。若端点处于半开状态且未在探测中，立即占用单飞名额并返回 `needsProbe=true`，触发后台协程 `go rt.runProbe(ep, snap)`，而真实请求直接将该端点视为不可用，不参与任何阻塞等待。
  2. **探针成功只衰减（`fails--`）**：后台探针为 300 token 小请求，通过 `ReportProbeSuccess` 仅将 `fails` 减 1 并解除冷却（`health.go:137-144`），保持半开状态继续由探针验证，直至连续探测衰减到 0 或真实业务请求成功调用 `ReportSuccess` 彻底清零（`health.go:118-124`）。有效防止大流量长上下文下的频繁 429 振荡。
  3. **探针名额释放与全结局兜底**：
     - `ReportNeutral` 与 `ReleaseProbe` 均安全将 `s.probing` 置为 false（`health.go:88-112`）。
     - `tryOne` 内部设置 `defer func() { if !healthReported { rt.Health.ReportNeutral(key) } }()` 兜底，防止任何未捕获 panic 导致探针名额永久泄漏（`router.go:230-234`）。
     - `runProbe` 内部同样设置 `defer recover()` 并调用 `ReportNeutral(key)`（`probe.go:30-35`）。
     - `forwardSuccess` 在开始流式传输前显式调用 `ReleaseProbe(key)` 归还单飞名额，并将健康结论推迟至流式结束后由 `reportStreamOutcome` 决议（`router.go:380, 439-444`）。
- **判断**：【实现正确】单飞槽位流转严密，无死锁和永久锁死风险。
- **影响范围**：故障端点恢复探测流程。

#### 2.3 并发与剪枝正确性
- **源码锚点**：`internal/health/health.go:31, 203-214` (`Prune`)
- **审查分析**：
  - `Registry` 全局所有导出方法（`Available`, `Acquire`, `ReportSuccess`, `ReportProbeSuccess`, `ReportFailure`, `ReportNeutral`, `ReleaseProbe`, `Classify`, `Status`, `Prune`）均全程持有 `r.mu.Lock()`，无竞态条件。
  - `Prune` 方法在热重载时由 `Router.Install` 触发，安全遍历并清理已从配置中移除的端点状态（`snapshot.go:193-195`）。
- **判断**：【实现正确】并发模型简单可靠。
- **影响范围**：多协程并发健康状态更新。

---

### 任务 3: 流式归一化（`internal/respnorm/`）

#### 3.1 状态机流转、切分与 `(0, nil)` 语义
- **源码锚点**：`internal/respnorm/respnorm.go:167-425` (`newStream`, `Read`, `ingest`, `decide`, `emitBlock`)
- **审查分析**：
  1. **模式初始判定**：非 SSE 模式初始化为 `modeBuffered`；Responses 协议直接初始化为 `modePassthrough`；OpenAI/Anthropic SSE 初始化为 `modeUndecided`，暂扣首包事件（`respnorm.go:232-255`）。
  2. **决策与流式恢复**：
     - 在 `decide` 中扫描 `\n\n` 分隔的完整事件，首个有效载荷事件若不含 MiniMax 思考特征立即转为 `modePassthrough` 实时吐出（`respnorm.go:437-474`）。
     - 若触发 `<think>` 思考模式进入 `modeBuffered`，在检测到 `</think>` 完整闭合事件后，自动执行 `stripFirstThink` 剥离思考块并切换至 `modePassthrough` 恢复实时流式（`respnorm.go:396-419`）。
  3. **`Read` 方法的 `(0, nil)` 语义**：
     - 当处于 `modeUndecided` 暂扣等待更多数据或接收到不完整事件时，`Read` 消耗了底层字节但暂无可交付数据，直接返回 `(0, nil)`（`respnorm.go:351`）。该行为专为消费方 `copyFlush` 定制（`transport.go:61-125`），允许调用方的 idle 看门狗在读取间隙持续运转。
  4. **缓冲区防溢出保护**：
     - `modeBuffered` 与 `modeUndecided` 均设有 `bufferedCap = 8MB` 内存上限（`respnorm.go:177, 420-428, 430-436`）。一旦超限立即降级为 `opaque` 裸透传，防止上游恶意长流耗尽内存。
- **判断**：【实现正确 / 契约明确】状态机流转与边界防护清晰。
- **影响范围**：流式转发吞吐、首字延迟与内存占用。

#### 3.2 断流处理、`truncated_withheld` 与 Panic 兜底
- **源码锚点**：`internal/respnorm/respnorm.go:323-338, 359-382` (`flushRawOnError`), `internal/router/router.go:463-474`, `internal/router/transport.go:65-74`
- **审查分析**：
  1. **中途断流与部分数据交付**：
     - 上游发生非 EOF 异常中断时，`Read` 触发 `flushRawOnError`（`respnorm.go:323-338`）。
     - 非 SSE 响应交付已缓冲的 partial JSON；`modePassthrough` 交付已接收的尾部数据；而仍处于 `modeUndecided` 或 `modeBuffered` 的 SSE 流一律**扣留不发**，记录 `truncated_withheld`（`respnorm.go:368-370`），防止向客户端泄漏未闭合的 `<think>` 标签导致下一轮对话进入自我指涉死循环。
  2. **连接硬中断**：
     - `forwardSuccess` 检测到 `status == "TRUNCATED"` 后，在完成配额扣减与审计记录后，显式执行 `panic(http.ErrAbortHandler)`（`router.go:463-474`）。net/http 捕获该 panic 后会直接掐断 TCP 连接而不发送终止 chunk，确保客户端 SDK 明确感知网络中断而非误判为正常空回复。
  3. **Panic 兜底恢复**：
     - `transport.go` 中的 `copyFlush` 在工作协程中加入了 `defer recover()`，将 `respnorm` 内部可能出现的 panic 转换为 `upstream stream panic` 错误（`transport.go:65-74`），顺畅引导至 `TRUNCATED` 流程，杜绝主进程崩溃。
- **判断**：【实现正确】断流一致性处理与防御机制完备。
- **影响范围**：网络不稳定与异常断流场景下的客户端行为一致性。

#### 3.3 虚拟模型改写、`[DONE]` 补全与 Quirk 修复
- **源码锚点**：`internal/respnorm/respnorm.go:200, 235-238, 477-512, 532-555`, `internal/respnorm/minimax.go:10-184`
- **审查分析**：
  1. **模型名称改写**：
     - `newStream` 针对 `clientModel` 包含 `$` 字符的情况提前转义为 `$$`（`respnorm.go:235-238`），避免 `regexp.ReplaceAll` 模板解析错误。
     - 在 `emitBlock` 和 `finalizeBuffered` 中通过 `modelFieldPattern` 正则全局将上游模型名替换为虚拟模型名（`respnorm.go:477-488, 517-521`），并在覆盖前通过 `noteUpstreamModel` 记录真实上游自报模型名。
  2. **`[DONE]` 哨兵补齐**：
     - `appendDone` 严格限定仅当 `isSSE && protocol == "openai-completions" && !sawDone` 时才追加 `data: [DONE]\n\n`（`respnorm.go:532-544`）。Anthropic 与 Responses 协议绝不追加。
     - 维护 `emittedTail` 跟踪跨数据块的尾部换行符状态（`respnorm.go:490-502`），防止因数据分片导致多注入空行。
  3. **MiniMax Quirk 修复**：
     - `stripFirstThink` 与 `stripThinkingProcess` 均具备严格的前置守卫（`thinkShapeGuard` 与 `thinkingProcessPrefix` 检查），正文中间引用 `<think>` 标签的内容不会被误伤（`minimax.go:37-77, 128-175`）。
- **判断**：【实现正确】边界处理极其精细。
- **影响范围**：响应内容保真度与 SDK 兼容性。

#### 3.4 用量嗅探（Usage Sniffing）与并发安全
- **源码锚点**：`internal/respnorm/usagesniff.go:21-93`, `internal/respnorm/respnorm.go:216-227`
- **审查分析**：
  1. **低开销 Gate**：`noteUsage` 采用 `bytes.Contains(b, []byte("\"usage\""))` 作为低开销过滤网，绝大多数常规文本 chunk 零额外开销跳过（`usagesniff.go:35-37`）。
  2. **分侧记录（UsageSides）**：
     - `usageBlockSides` 特别针对 Anthropic Messages 协议的 `message_start` 事件进行鉴别：该事件包含真实输入计数和占位符 `output_tokens≈1`。此时仅将 `inSeen` 标记为 true，`outSeen` 保持 false（`usagesniff.go:68-75`），防止流截断时将占位符输出当作精确值计费。
  3. **并发安全**：
     - `NormalizerStream` 上的所有导出查询方法（`Applied`, `RawPreStrip`, `ObservedModel`, `Usage`, `UsageSides`, `OutTokens`）均统一由 `s.mu sync.Mutex` 保护（`respnorm.go:257-270`, `usagesniff.go:77-93`）。
     - `copyFlush` 提前退出时（如 client disconnect），读取协程的尾随读取与 `forwardSuccess` 主协程的状态读取完全互斥，杜绝 Data Race。
- **判断**：【实现正确 / 线程安全】完美闭环了计费嗅探与并发读写安全。
- **影响范围**：Token 计量准确性与 `-race` 测试通过率。

---

### 任务 4: 并发闸与粘性（`internal/router/limiter.go`、`internal/sticky/`）

#### 4.1 全局并发闸（`internal/router/limiter.go`）
- **源码锚点**：`internal/router/limiter.go:10-56`
- **审查分析**：
  1. **信号量与原子计数**：
     - `limiter` 基于带缓冲 channel `chan struct{}` 实现 FIFO 信号量。
     - `AcquireSlot` 精确维护原子变量 `rt.inFlight` 和 `rt.waiting`（`limiter.go:33-50`）。
     - 获取槽位后返回释放闭包 `func() { rt.inFlight.Add(-1); <-l.sem }`，未获取到或取消时 `rt.waiting.Add(-1)`，计数无泄漏。
  2. **热重载容量变更取舍**：
     - `installLimiter` 在容量变化时原子存储新 `limiter`（`limiter.go:17-30`）。持有旧信号量槽位的请求在释放时归还给旧 channel，存在短暂可接受的瞬时超额窗口，符合单机路由器的实用主义设计。
  3. **获取时机**：
     - 位于 `server.chatHandler` 中请求体完全缓冲**之后**获取（`server.go:222-227`），慢客户端上传不占并发槽，仅保护 CPU 密集型（图片缩放）与上游网络交互阶段。
- **判断**：【设计取舍 / 实现正确】计数精确，死锁与泄漏防护到位。
- **影响范围**：服务过载保护。

#### 4.2 会话粘性亲和（`internal/sticky/`）
- **源码锚点**：`internal/sticky/sticky.go:14-118`
- **审查分析**：
  1. **注册表并发安全**：`Registry` 内部操作全部通过 `r.mu sync.Mutex` 加锁（`sticky.go:56-118`）。
  2. **容量硬上限与淘汰机制**：
     - `MaxEntries = 10000`，`sweepInterval = 1h`，`BackstopTTL = 24h`。
     - 在 `Set` 方法中，当容量满或距离上次清理超过 1 小时时，执行惰性 TTL 清理（`sticky.go:75-84`）。
     - 清理后若仍达容量上限，执行 O(N) 遍历淘汰最久未使用的条目（`oldestKey`）（`sticky.go:87-102`），确保内存占用严格受限。
  3. **TTL 判定与重新置顶逻辑**：
     - `Peek` 仅返回记录值与 `lastUsed`，不自行执行 TTL 淘汰（`sticky.go:56-66`）。
     - 真正的 TTL 校验发生在 `candidates.go:94-97`，比对具体端点配置的 `ep.StickyTTL`。有效则调用 `moveToFront` 将其提升至候选序列首位。
     - 请求成功后，`router.go:124-130` 立即调用 `rt.Sticky.Set(cs.stickyKey, ep.HealthKey())` 刷新粘性指针。
- **判断**：【实现正确】淘汰与亲和生命周期管理清晰。
- **影响范围**：Prompt Cache 命中率与内存占用。

---

### 任务 5: 服务器层（`internal/server/`）

#### 5.1 身份认证、端点隔离与 Slowloris 防护
- **源码锚点**：`internal/server/server.go:58-75` (`Handler`), `internal/server/server.go:84-142` (`health`, `authenticateWithSnap`, `auth`), `internal/server/server.go:196-209` (`chatHandler` body read)
- **审查分析**：
  1. **认证与鉴权**：
     - `authenticateWithSnap` 支持 `Authorization: Bearer <key>` 与 `x-api-key`，大小写不敏感剔除 `Bearer ` 前缀（`server.go:133-138`）。
     - 未配置 `api_keys` 时认证开放，但仍从客户端 Header 提取自声明标签供审计；配置 `api_keys` 时使用 `subtle.ConstantTimeCompare` 防时序攻击（`server.go:125-129`）。
     - `GET /health` 为存活探针（Liveness），显式置于 `auth` 之外，返回 uptime 与 no-store（`server.go:84-98`），不泄漏任何业务数据。
     - `GET /status` 与 `GET /log` 均受 `s.auth` 严格保护；其对应的静态单页 HTML（`/status.html`, `/log.html`, `/help.html`）免鉴权直出外壳，数据由前端二次拉取并触发鉴权（`status_page.go:15-20`, `admin_log.go:30-36`, `help.go:38-44`）。
  2. **Slowloris 防护与 Body 上限**：
     - 使用 `http.ResponseController(w).SetReadDeadline(time.Now().Add(getBodyReadTimeout()))`（默认 60s）为入站请求体读取设置硬超时（`server.go:197-200`）。
     - 使用 `http.MaxBytesReader` 限制请求体大小（默认 8MB，超限返回 413 `StatusRequestEntityTooLarge`）（`server.go:198, 203-207`）。
  3. **Header 过滤**：
     - `FilterClientHeaders` 剔除 `authorization`、`x-api-key`、`cookie`、`x-forwarded-*`、`host`、`accept-encoding`、`x-vmr-*` 等敏感/冲突 Header（`clientheaders.go:9-30`），保证安全隔离与上游透明 gzip 正确工作。
- **判断**：【实现正确】安全防护与鉴权边界严密。
- **影响范围**：服务入口安全性与抗慢速攻击能力。

#### 5.2 `RequestFacts` 提取与审计记录
- **源码锚点**：`internal/server/facts.go:57-147`, `internal/server/recorder.go:19-71`, `internal/server/server.go:237-251`
- **审查分析**：
  1. **`RequestFacts` 提取效率与准确性**：
     - `computeRequestFacts` 不重复扫描 JSON：`hasTools` 直接取自 `adapter.TopLevelProbe`，`imageCount` 取自 `imgprep.Downscale`（`facts.go:42-56`）。
     - 附件 Payload 排除机制：`attachmentSpans` 扫描并标记 base64 图片/文件数据范围，`estimateTextTokens` 仅对非附件文本区间统计字符特征，防止 500KB 内联图片被误当做 100K 文本 token 导致配额计量与条件路由严重失真（`facts.go:60-147`）。
  2. **审计流式录制器**：
     - `recorder` 包装 `http.ResponseWriter`，实现 `Flush` 透传保证流式延迟不受影响；记录 `ttftMS`；使用 `recorderBodyCap = 16MB` 限制录制内存，超限时安全截断审计副本而不影响向客户端交付的实际响应（`recorder.go:19-71`）。
     - 审计记录在 `defer done()` 中统一异步落盘，落盘失败仅记录日志，绝不中断客户端响应（`server.go:275-285`）。
- **判断**：【实现正确】计算高效且对主请求链路零侵入。
- **影响范围**：条件路由准确性与审计完整度。

---

### 任务 6: 策略层（`internal/strategy/`）

#### 6.1 `Dimension`（排序）与 `Condition`（淘汰）的职责分离
- **源码锚点**：`internal/strategy/strategy.go:19-122`, `internal/strategy/conditions.go:11-30`
- **审查分析**：
  1. **职责严格解耦**：
     - `Dimension` 接口（`Compare(a, b *core.Endpoint) int`）专职端点两两排序，完全不感知请求内容（`strategy.go:27-30`）。
     - `Condition` 接口（`Eligible(ep *core.Endpoint, facts core.RequestFacts) bool`）专职请求级硬性准入判定，纯粹一票否决，不改变候选相对次序（`strategy.go:73-76`）。
  2. **多键稳定排序**：
     - `strategy.Sort` 基于 `sort.SliceStable` 依次应用配置的维度链（`strategy.go:49-57`），在所有维度持平时严格保持配置文件中的相对顺序。
  3. **并发安全注册**：
     - `Dimension` 在 Snapshot 构建时由 `strategy.Build` 实例化；`Condition` 在全局 `atomic.Pointer[[]Condition]` 中以 COW 机制维护，读取无锁（`strategy.go:78-95`）。
- **判断**：【设计取舍 / 实现正确】架构设计优美，无职责混淆。
- **影响范围**：候选排序与过滤逻辑。

#### 6.2 `WithinContext` 估算与降级规则
- **源码锚点**：`internal/strategy/strategy.go:119-122`, `internal/router/candidates.go:56-68`
- **审查分析**：
  1. **非 Condition 独立设计**：`WithinContext` 明确作为独立函数实现，故意不注册进 `Condition` 接口（`strategy.go:115-122`）。
  2. **软性降级 Fallback**：在 `candidates.go:56-68` 中，硬性条件过滤得到 `hardFiltered`；随后对 `hardFiltered` 进行 `WithinContext` 过滤得到 `candidates`。若估算导致 `len(candidates) == 0 && len(hardFiltered) > 0`，则自动回退为 `candidates = hardFiltered` 并标记 `reason.ctxFallback = true`。这保证了因粗估过大导致所有端点看似放不下时，依然能由真实上游请求去尝试并由 failover 兜底，绝不凭猜想直接拒绝服务。
- **判断**：【实现正确】完美落实设计文档关于软硬条件不对称处理的决策。
- **影响范围**：长文本请求的端点准入与兜底。

---

## 二、KNOWN_ISSUES 声明一致性验证

对 `docs/KNOWN_ISSUES.md` 中与 D1 路由核心域相关的全部架构声明进行逐项源码比对，核验结果如下：

| KNOWN_ISSUES 声明项 | 声明内容摘要 | 源码核验位置 | 现状一致性 | 审查结论 |
| :--- | :--- | :--- | :---: | :--- |
| **§1.1 全局互斥锁不分片** | `health.Registry` 采用单个全局 `sync.Mutex` 不分片 | `internal/health/health.go:31` | **一致** | 纳秒级 map 读写，无分片复杂度 |
| **§1.1 `Available` 无生产调用方** | 唯一无副作用状态查询，仅供测试断言端点状态 | `internal/health/health.go:48` | **一致** | 保留合理，非死代码 |
| **§1.1 `fails` 计数语义** | 退避曲线切换时 `fails` 重置为 1，非累计失败总数 | `internal/health/health.go:154-159` | **一致** | 避免跨曲线（2s vs 10m）深度污染 |
| **§1.1 `ReleaseProbe` vs `ReportNeutral`** | 行为相同但语义区分（释放名额待决议 vs 终局无信息） | `internal/health/health.go:88-112` | **一致** | 清晰表达调用处语义意图 |
| **§1.1 探针成功只衰减** | `ReportProbeSuccess` 执行 `fails--`，真实请求才清零 | `internal/health/health.go:120, 139` | **一致** | 防止小探针解除对大流量的保护 |
| **§1.1 冷却抖动与 Retry-After** | 冷却带 ±10% 抖动且作用于封顶值；`Retry-After` 不抖 | `internal/health/health.go:174-197` | **一致** | 防整点惊群，且遵从上游节奏 |
| **§1.1 探针额度计量口径** | 后台探针 requests 计 1，token/cost 计 0 | `internal/router/probe.go:82-83` | **一致** | 诚实下界计量 |
| **§1.1 `HealthKey` SHA-256 截断** | 取 APIKey SHA-256 前 4 字节（8 字符 hex） | `internal/core/core.go:207` | **一致** | 避免重复哈希计算，碰撞概率极低 |
| **§1.1 客户端取消不停止计费** | 客户端主动取消照常扣减已产生 token | `internal/router/router.go:446` | **一致** | 与上游真实账单对齐 |
| **§1.1 `respnorm.Read` 返回 `(0, nil)`** | 等待更多数据时返回 `(0, nil)`，由 `copyFlush` 专有消费 | `internal/respnorm/respnorm.go:351` | **一致** | 保持 idle 看门狗心跳粒度 |
| **§1.1 Usage Sniffing 不外移** | 响应归一化器就地嗅探 token usage，不加装饰器层 | `internal/respnorm/usagesniff.go:35-50` | **一致** | 转发热路径零额外开销 |
| **§1.1 观测标记不删** | `crlf_framing_suspected` / `thinking_process_pattern_detected` | `internal/respnorm/respnorm.go:431, 565` | **一致** | 详单与报表跨请求预警在用 |
| **§1.1 `GET /health` 为存活探针** | 仅测进程存活性，永不因上游不可用返回非 200 | `internal/server/server.go:84-98` | **一致** | 防容器编排雪崩杀进程 |
| **§1.1 状态口径与审计刻意不对齐** | 截断流在状态机记 error，在审计顶层 outcome 记 ok | `internal/router/router.go:453` | **一致** | 传输层 vs 业务完整性双口径自洽 |
| **§1.1 中途断流不 flush 未决 SSE** | buffered/undecided SSE 断流时不吐出未闭合 `<think>` | `internal/respnorm/respnorm.go:368-370` | **一致** | 杜绝反馈循环与假成功 |
| **§1.1 厂商协议拒绝归 `core.ErrQuirk`** | 格式约束拒绝归 `ErrQuirk` 零冷却切换 | `internal/core/core.go:76-85` | **一致** | 语义独立，不污染上下文超限标签 |

---

## 三、领域级问题与观察项汇总

按严重级别划分如下：

### 1. 高危问题（High）
- **无**。D1 域在并发控制、内存边界、资源泄漏、认证鉴权与状态机流转方面无致命缺陷。

### 2. 中危问题（Medium）
- **无**。

### 3. 低危问题（Low）
- **无**。未发现影响功能正确性或稳定性的代码缺陷。

### 4. 架构与设计观察项（Observations / Tradeoffs）

1. **【观察项 1】`limiter` 热重载信号量替换的瞬时超额窗口（设计取舍）**
   - **位置**：`internal/router/limiter.go:17-30`
   - **分析**：当热重载修改 `max_concurrency` 时，正在使用旧信号量的请求在完成时释放回旧 channel，而新请求直接从新 channel 获取槽位。在极端高并发重载瞬间，在途请求数可能会短暂超过新的容量上限。
   - **评定**：【符合预期 / 设计取舍】代码注释与设计文档中明确记录此项为本地工具的可接受边界行为，无需额外加重量级排空锁。

2. **【观察项 2】`respnorm.Read` 非标准 `io.Reader` 契约（设计取舍）**
   - **位置**：`internal/respnorm/respnorm.go:272-352`
   - **分析**：`Read` 在暂扣未决数据时返回 `(0, nil)`，虽违反标准库 `io.Reader` 不鼓励返回 0 字节 nil error 的建议，但其唯一消费者是 `transport.go` 中的 `copyFlush`，该设计使传输层的 stream idle 定时器能获得细粒度调度心跳。
   - **评定**：【符合预期 / 架构约定】KNOWN_ISSUES §1.1 已明确锁定此契约，且内部测试与 fuzz 测试均充分覆盖。

3. **【观察项 3】`copyFlush` 异常路径下的多协程元数据查询安全（并发加固亮点）**
   - **位置**：`internal/router/transport.go:61-125`, `internal/respnorm/respnorm.go:257-270`, `internal/respnorm/usagesniff.go:77-93`
   - **分析**：当客户端发生写断开时，`copyFlush` 提前返回，此时读取协程可能仍处于阻塞读取或刚刚返回的边缘。`NormalizerStream` 将 `Applied`, `RawPreStrip`, `ObservedModel`, `Usage`, `UsageSides`, `OutTokens` 全部收敛在 `s.mu` 互斥锁下，消除了潜在的 Data Race。
   - **评定**：【健壮性优异】经 `-race` 测试和异常断开集成测试严格守护。

4. **【观察项 4】`SoftBlockFailover` 的 Peek 还原与断流传递机制（设计亮点）**
   - **位置**：`internal/router/softblock.go:38-46`
   - **分析**：针对 MiniMax 等 2xx 软屏蔽的 opt-in 检测，若 peek 期间遇到断流，利用 `readCloser` + `errReader` 构造复合 Reader，将读取错误精确传递给后续的 `forwardSuccess`，严密杜绝了因前置探测吞掉网络异常导致的「伪成功响应」。
   - **评定**：【健壮性优异】逻辑极为严谨。

---

## 四、审查总结

本轮对 **D1 路由核心域** 的全量生产代码审查表明：
1. **架构契约严格落地**：三协议无互译透传、首字节前 failover 约束、零冷却请求级错误切换、上下文估算软降级兜底等核心原则在代码中执行彻底。
2. **状态机与并发模型扎实**：健康退避（长短曲线隔离、抖动）、半开探针单飞与衰减恢复、粘性亲和与容量硬淘汰、快照原子发布等机制逻辑严密，无资源泄漏或死锁隐患。
3. **文档一致性极高**：KNOWN_ISSUES §1.1 所声明的 16 项关键架构裁决在源码中均可逐行精准对齐验证。
4. **代码质量与鲁棒性优秀**：异常路径（断流、panic、超时、取消）均有完备的恢复与兜底机制。
