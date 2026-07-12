<!-- Ver 2026-07-12 15:30, by Fable 5 -->

# vmr 全面 Review：主路径损耗 + 改进/重构建议（2026-07-12）

本报告是对 vmr 全部源码（约 16K 行含测试）的一次通读 review，聚焦三件事：**请求代理主路径上的性能损耗盘点**、**本次直接修掉的问题**、**按 ROI 分梯队的改进建议**。设计文档见 `docs/VirtualModelRouter_v2_Fable5.md`。

---

## 1. 结论（TLDR）

**主路径损耗很小，vmr 已经配得上"轻量、低损耗"的定位。** 典型 Agent 请求（~200KB body、流式响应）经过 vmr 的附加 CPU 时延约 **1–3ms**，对比上游数百 ms 到几十秒的生成时长，占比 <1%，TTFT 影响可忽略。最大的单项成本不是"代理"而是**审计**（默认开启的全量记录），且它是特性而非损耗——关掉 `-audit=false` 后主路径几乎只剩必要的缓冲和一次 model 字段改写。

本次发现并修复 **2 个真 bug + 2 个性能疏漏**（§3），其中代理环境变量被静默忽略是实际影响可用性的问题。剩余优化空间集中在"请求体被反复扫描/拷贝"这一类，单项收益都在 ms 级以下，列为第一/第二梯队建议（§4）。**没有发现值得立刻动手的大型重构**——架构本身（透传 + 快照 + 无 IR）就是性能上限最高的形态。

---

## 2. 主路径损耗盘点（逐阶段）

以"audit 开启、无图片、单次成功尝试、流式响应"这个最常见路径为基准。

### 2.1 请求侧（到达 → 发往上游）

| # | 步骤 | 代码位置 | 成本 | 评价 |
| --- | --- | --- | --- | --- |
| 1 | `io.ReadAll` 全量缓冲请求体 | `server.go` chatHandler | 1 次读入 + 1 份内存 | **必要**：failover 重放的前提，不可省 |
| 2 | `audit.EncodeBody`：`json.Valid` 全扫 + 全拷贝 | `server.go` → `audit.go` | 1 扫 + 1 拷 | 审计成本；audit off 时为零 |
| 3 | probe `json.Unmarshal`（model/stream 两字段） | `server.go` | 1 次全扫 | 必要；Go 的 json 必须扫完整个 body |
| 4 | `HasImageMarker` 子串扫描 | `imgprep.go` | 1 次 memchr 级扫描 | 便宜，设计合理 |
| 5 | `RewriteModel`：`json.Unmarshal` 到 `map[string]RawMessage` + `MarshalNoEscape` 全量重新序列化 | `classify.go`，**每次 attempt 各一遍** | 1 扫 + 1–2 拷 / attempt | **主路径上最大的可优化点**（§4 一梯队 b） |
| 6 | audit attempt 出站 body：`GetBody()` + `io.ReadAll` 再拷一份 + `EncodeBody` 再扫再拷 | `router.go` tryOne | 2 扫 + 2 拷 | 纯冗余拷贝，rewrite 结果本来就在手里（§4 一梯队 c） |
| 7 | header 黑名单过滤 + 复制 | `server.go` / adapter | 每请求几十次 map 查 | 纳秒级，忽略 |

合计：**约 6–8 次全 body 扫描、4–6 次全 body 拷贝**。200KB body 下总计 <1ms；8MB 上限 body 下约 20–40ms + GC 压力。内存峰值 ≈ 请求体 × 4–5 份（audit on）。

### 2.2 响应侧（上游 2xx → 客户端）

| # | 步骤 | 代码位置 | 成本 | 评价 |
| --- | --- | --- | --- | --- |
| 1 | `respStream`：SSE 事件级归一化，每事件 1 次 regex Match（`"model":"`）+ 命中才 ReplaceAll | `response.go` | 每 chunk µs 级 | v3 设计正确，passthrough 缺省真流式 |
| 2 | `copyFlush`：每响应 1 个 reader goroutine + 每 chunk 1 次 `append` 拷贝过 channel | `router.go` | 每 chunk 1 alloc + 1 拷 | watchdog 换来的代价，可用 buffer 复用削减（三梯队） |
| 3 | `recorder.buf`：全量响应缓冲进内存 | `recorder.go` | 响应体 1 份内存，无上限 | 审计契约（不截断）的直接后果；audit off 时为零 |
| 4 | 请求结束 `json.Marshal(rec)`：把 client body + attempt body 全部 compact/escape 一遍再落盘 | `audit.go` | 全部 body 再扫 1–2 遍 | 发生在响应写完之后，不影响客户端时延，只吃 CPU/GC |

流式路径客户端可感知的额外时延 = respStream 判定（首个载荷事件，µs 级）+ 每 chunk 的两次内存拷贝——**TTFT 与逐 token 时延基本无损**。

### 2.3 量化结论

- **CPU**：audit on 时 vmr 每请求约 1–3ms（200KB 级），dominated by JSON 扫描/拷贝；audit off 时 <0.5ms。
- **内存**：峰值 ≈ 请求体×4–5 + 响应体×2（audit on）。`max_concurrency=8` 且 8MB 上限下最坏 ~350MB，实际典型负载几十 MB。
- **时延**：TTFT 附加损耗 µs~低 ms 级；并发闸等待与上游 RTT 才是真实变量。

**判断：不存在需要"救火"的性能问题。** 下面的建议是把"已经很低的损耗"进一步压向零，以及防患于未然。

---

## 3. 本次已修复（直接改掉的错误与疏漏）

1. **【bug】上游 Transport 忽略代理环境变量**（`internal/router/router.go` Install）。手工构造的 `http.Transport` 不含 `Proxy` 字段（与 `http.DefaultTransport` 不同），`HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY` 被静默忽略——而 vmr.sh 特意把代理变量注入 service 环境，两者自相矛盾；直连不了上游的部署（大陆访问境外 API 的典型场景）会表现为全部 endpoint 网络错误。已加 `Proxy: http.ProxyFromEnvironment`，同时补上缺失的 `TLSHandshakeTimeout: 10s`（原来为零 = 无界,一个 TLS 握手挂死的上游会把该次尝试卡到客户端放弃——dial timeout 和 ResponseHeaderTimeout 都覆盖不到这一段）。设计文档 §10 环境变量段已同步。
   **后续演进（同日第三轮，本条已被取代）**：代理机制随后改为**纯显式配置**——config 级 `http_proxy`/`https_proxy` + provider 级 `proxy: false` 开关，`ProxyFromEnvironment` 整体移除（隐式旋钮决定流量走向正是本条 bug 的根源形态），vmr.sh 的代理变量自动抓取一并删除。现行方案与取舍见设计文档 §10"上游代理"段与 §11 决策表；`TLSHandshakeTimeout` 的修复保留。

2. **【bug】failover 循环中 nil 覆盖已保存的上游错误**（`internal/router/router.go` Serve）。`last = uerr` 无条件赋值,而 build/network 失败返回 `uerr=nil`：若尝试序列是「429（带 Retry-After）→ 网络错误」,客户端收到的不是 429 而是误导性的 503 "all cooling down or none configured"。已改为只在 `uerr != nil` 时更新,与设计文档"原样返回最后一次上游错误"的本意一致（最后一次**有响应体可返回的**错误）；同时当 attempts>0 但全部失败于无响应路径时,503 的 message 改为如实描述（"failed before an upstream response"）,不再谎报"cooling down or none configured"。

3. **【perf】`respStream.Read` 每次调用在栈上声明 32KB scratch 数组**（`internal/router/response.go`）。Go 每次调用都要对它 memclr,长流式响应每次 Read 白付 32KB 清零。已改为 struct 字段、每响应懒分配一次。

4. **【perf】audit 关闭且降采样关闭时 imgprep 仍全量运行**（`internal/server/server.go`）。images 元数据唯一的消费方是审计记录,`rec==nil && maxPx<=0` 时扫描/解析纯属浪费。已加跳过条件。

全部测试通过（`go test ./...` 14 包全绿）。

---

## 4. 改进建议(按 ROI 分梯队)

### 第一梯队：建议执行（高 ROI）——**已全部完成（2026-07-12 同日第二轮）**

**a. 【已完成】`RewriteModel` 改为字节级 splice,消除每 attempt 的全量 parse+reserialize。**
原状:每次上游尝试都把整个请求体 `json.Unmarshal` 成 `map[string]json.RawMessage` 再 `MarshalNoEscape` 重新序列化——主路径上最大的单项 CPU 成本,failover 时每个 attempt 重复一遍。实现（`internal/adapter/classify.go`）:单趟免分配扫描定位**顶层** `model` 键的值区间（字符串跳跃用 `bytes.IndexByte`,memchr 速度;嵌套 `model` 键、content 里转义提及的 `"model"` 均不受影响）,三段拼接替换;虚拟名与上游名相同时零拷贝返回原 slice;扫描器搞不定的形态回退旧 map 重建路径（含"缺键则补"语义）。**实测(Apple M4,200KB agent 形态 body):99µs vs 1333µs——快 13.5 倍,分配 213KB/5 次 vs 433KB/26 次,吞吐 2.07GB/s**;附带收益是请求体键序/空白不再被改写,"直连等价"从近似变成字面事实。新增 6 个单测(字节保留、嵌套键不动、转义文本不动、零拷贝、缺键回退)+ 2 个基准。

**b. 【已完成】audit attempt 的出站 body 复用 rewrite 结果,消除 `GetBody`+`ReadAll` 双拷贝。**
`Adapter.BuildRequest` 签名改为 `(*http.Request, []byte, error)`,出站 body 直接返回,router 审计路径不再 `GetBody()+io.ReadAll` 重读一份;同时 `audit.EncodeBody` 去掉防御性克隆,改为文档化的所有权契约(五个调用点——client 请求缓冲、recorder 响应缓冲、attempt 出站 body、上游错误 body、pre-strip 快照——全部是终态字节,已逐一核实)。每个被审计请求净省 2–3 次全 body 拷贝(其中一次是**完整响应体**)。

**c. （上一轮已完成)代理支持、TLS 握手超时、last-error 覆盖、scratch 复用、imgprep 跳过——见 §3。**

### 第二梯队：高价值但成本偏高,放入发展规划

**a. 审计大响应的内存峰值治理。**
`recorder` 的 `bytes.Buffer` 无上限 + 结束时 `json.Marshal(rec)` 再造一份,意味着一个 500MB 的异常流式响应会瞬时吃掉 ~1GB 内存。当前单机单用户 + 并发闸下不是现实问题,但如果 vmr 走向多客户端共享部署,这是第一个会炸的点。方案方向:audit body 超过阈值时旁路落盘（record 里存文件引用）,或流式写 JSONL（需要放弃"一行一条"或做两段式写入）。改动面大（审计契约、report 解析、detail 导出全要动）,故不建议现在做——但**建议在 README/设计文档给出"共享部署前必读"的显式边界**,成本一句话。

**b. SSE `\r\n\r\n`（CRLF 分隔）兼容。**
`response.go` 的事件分隔符写死 `\n\n`;一个用 CRLF 行尾的合法 SSE 上游会导致 decide() 永远找不到完整事件 → 整个响应退化为全量缓冲假流式（正确性无损,但 TTFB=完整生成时长,32MB 上限后再降级 opaque）。目前接入的四家上游都用 `\n\n`,所以是**潜伏兼容性债**而非 bug。修复本身不难（分隔符匹配同时认两种）,但要把 emitComplete/decide/strip 全链路的边界测试重做一遍,建议在接入新 provider 报出此形态时一并处理,或路线图排队。

**c. 路线图既有项的优先级背书。**
`weight`/`latency` 排序维度、endpoint 级限流（主动避 429）、`/metrics`——架构缝都已留好（Dimension 接口、tryOne 单点）,属于"要做的时候成本已经被现在的设计压到最低"。无需提前。

### 第三梯队：可改可不改（一句话罗列）

- `Endpoint.HealthKey()` 每次调用重算 sha256,可在 BuildSnapshot 预计算成字段——每请求省 ~2µs。
- `copyFlush` 每 chunk 一次 `append([]byte(nil), ...)` 分配,可用双缓冲乒乓消除——省一点 GC churn。
- `chatHandler` 里 `checkAuth` 与后续逻辑各 `Snapshot()` 一次、Serve 里再 Load 一次,可传参复用——纳秒级。
- 未知模型（注定 404）的请求仍会先过 imgprep 降采样——只浪费在错误请求上。
- `writeError`/`countNested` 的双份实现、三个测试 mock 上游——设计文档 §13 已论证过不动的理由,维持。
- module 名 `vmr` 改完整仓库路径——路线图发布项已含,发布前一并做。
- `report` 包 4000+ 行、与审计格式编译期强耦合——它是离线工具,不在任何运行时路径上;耦合是文档明示的契约（§9.4）,拆包只会增加同步成本。

**为什么这些放第三梯队**:每一项的收益都是 µs 级、或只影响错误路径/离线工具,而 vmr 的主路径已有完整的回归测试覆盖——为不可感知的收益去扰动已验证的代码,ROI 为负;它们的正确姿势是"顺路修"（下次因为别的原因碰到那个文件时捎带),而不是专项执行。

---

## 5. 技术债总体评估

**债务水平:低,且已知债都有书面记录。** 几个结构性判断:

1. **架构无重构必要。** "协议内透传、无 IR、快照原子交换、编译期注册"这套骨架就是这类工具损耗的理论下限形态;router 主流程 ~600 行的自我约束(§11)仍然成立。任何"框架化"改造只会往 LiteLLM 的方向退化。
2. **最大的真实债在 `response.go` 的 quirk 嗅探**——MiniMax 专属修复靠全局形态检测而非按端点声明。文档 §5.5 已论证过取舍并给出升级路径(endpoint 级 `quirks:` 开关);当第二家厂商出现同类 quirk、或发生一次真实误伤时,就是兑现那个升级路径的时机。在那之前它是可控债。
3. **审计子系统是"重"的唯一来源**(每请求 4–6 份 body 拷贝、全响应缓冲、report 包体量),但这是产品选择(完整取证 + 成本核算)而非事故;它的所有成本都随 `-audit=false` 归零,边界干净。
4. **测试质量好**:probe 锁死、错误分类回退、真实生产日志回归出的 bug 都有对应测试,这是本次敢于直接改 router 热路径的底气。

一句话:**vmr 当前不需要重构;第一梯队两项优化已于同日完成(splice 实测快 13.5 倍),剩下的只是在共享部署成为现实之前记得第二梯队 a 的内存边界。**
