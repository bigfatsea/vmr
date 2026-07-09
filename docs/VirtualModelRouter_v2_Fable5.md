<!-- Ver 2026-07-09 00:40, by Sonnet 5 -->

# Virtual Model Router (vmr) — 设计方案

本文档描述 vmr 的当前完整设计：定位、架构、机制与关键决策。读完即可维护与二次开发本项目。使用文档见 `README.md`（英文）/ `README.zh.md`（中文）。

---

## 1. 定位

本地运行、单二进制、配置驱动的 LLM 路由器。客户端只连稳定的 Virtual Model 名（如 `coding` / `cheap` / `claude`），Provider、账号、Key、优先级、故障切换全部由 vmr 隐藏。Unix 风格工具：零数据库、零 Web UI、零运行时插件。

**边界**（永久有效的"不做"清单）：Dashboard、用户管理、计费、Prompt 管理、Workflow、运行时插件系统、MCP 框架、企业级 AI Gateway、**跨协议转换**（见 §3）。每个新需求先过一道门：它增加的是能力，还是复杂度？

---

## 2. 核心概念

| 概念 | 职责 |
| --- | --- |
| **Virtual Model** | 对外暴露的模型名，代表能力而非厂商；对应一组 Endpoint，绑定一种协议 |
| **Provider** | 一个可复用的上游定义：base_url + api_key；归属哪个协议由它在配置里的位置决定（`providers.<protocol>.<name>`），不再是自带的字段 |
| **Endpoint** | 最小调度单位：Provider × 实际模型名 × 调度属性；同厂不同 Key / 不同协议面即不同 Provider→不同 Endpoint |
| **Adapter** | 协议插件：构造上游请求、转换响应、归类错误；声明自己的协议 |
| **Strategy** | 候选排序器：健康过滤后按维度序列做稳定多键排序 |

Health 与并发闸不是独立概念，而是运行时状态（§6、§8）。

---

## 3. 协议模型：多入口，永不翻译

vmr 同时暴露两个聊天入口，**不做任何跨协议转换**：

```
POST /v1/chat/completions   OpenAI 协议   → 只路由到 OpenAI 兼容端点
POST /v1/messages           Anthropic 协议 → 只路由到 Anthropic 兼容端点
```

**决策逻辑**：双向流式翻译（Anthropic 的 `message_start`/`content_block_delta` 事件流 vs OpenAI 的 chunk 流，tool-use / thinking 块的语义映射）是 LiteLLM 复杂度失控的根源之一；而主流厂商（MiniMax、DeepSeek、OpenRouter）已原生提供两种兼容面，翻译不创造价值。协议内透传保证零损耗、对上游新字段前向兼容。

落实机制：

* 协议是 Adapter 的属性（`Protocol() string`），也是配置里 `providers`/`models` 的外层 key。一个 Virtual Model 的 endpoints 只能引用同一协议分组下的 provider——跨协议混用没有语法能表达它，不是"配置了会被校验拒绝"，而是"配置这个东西本身写不出来"（§10）。
* 模型存在但协议不符 → 404，message 指明正确入口。
* 恰好两种协议的请求体都是顶层 `model` + `stream` 字段，路由解析层（`CanonicalRequest`）天然协议无关。
* vmr 自产的错误体为两种客户端都能解析的合并形态：`{"type":"error","error":{"type","message"}}`（OpenAI SDK 读 `error.message`，Anthropic SDK 认 `type:"error"` 信封）。`GET /v1/models` 同理（`object:"list"` + `has_more` + `type:"model"` 并存）。
* 新增协议入口（如 gemini）= 新 Adapter + 新路由行，同样透传。

已接入的厂商协议面（均实测）。同一账号的两个协议面现在共用同一个 provider 名（分属 `providers.openai`/`providers.anthropic`），不再需要 `_a` 后缀区分：

| Provider 名 | base_url（openai 面 / anthropic 面） |
| --- | --- |
| minimax | `https://api.minimaxi.com/v1` / `https://api.minimaxi.com/anthropic/v1` |
| deepseek | `https://api.deepseek.com/v1` / `https://api.deepseek.com/anthropic/v1` |
| openrouter | `https://openrouter.ai/api/v1`（同一 base，anthropic 面由 Adapter 拼 `/messages`） |

---

## 4. 系统架构

### 4.1 请求流程

```
Client ── POST /v1/chat/completions | /v1/messages
  │
Server     审计记录开始 → 鉴权(可选) → 缓冲请求体(≤8MB,413) → 并发闸
  │        → 图片降采样(可选) → 解析 model/stream，其余字节不动
  ▼
Router     查 Virtual Model → 校验协议 → 健康过滤 → 稳定多键排序 → 候选序列
  │
  ▼ failover 循环（每个可用候选各试一次，直到成功或候选耗尽；max_attempts 可选设上限）
Adapter    BuildRequest：改 URL / 注入 Key / 改写 model 字段（其余透传）
  │
  ▼
Upstream   ├─ 2xx → 响应归一化（见 §5）→ 转发 → 上报健康成功 → 审计落盘
           ├─ 4xx/5xx → ClassifyError → ErrClient 直接返回；其余记冷却、试下一个
           └─ 网络错误 → 短冷却，试下一个
```

硬规则：

* **请求体一律入口缓冲**（流式也是）：failover 重放的前提。
* **流式只在首字节发出前允许 failover**；实现上该约束自然成立——仅上游 2xx 后才开始向客户端写，此前的一切失败都发生在写出之前。首字节后的上游错误只能断流并记日志。
* **失败语义**：有真实上游尝试 → 原样返回最后一次上游错误（status+headers+body，`Retry-After` 等原样到达客户端，保留客户端可解析的厂商错误结构）；无候选可试 → 503。凡进入 failover 循环的响应带 `X-VMR-Attempts`（成功另带 `X-VMR-Endpoint`）；路由之前被拒的请求（401/404/413/坏 JSON）不带。
* **请求侧 Header 透传**：黑名单之外的客户端 header 全部透传（含 `anthropic-version`/`anthropic-beta` 协议头，§5.4），Content-Type 与凭证由 Adapter 统一设置。客户端 `Authorization`/`x-api-key` 绝不到上游；不透传 `Accept-Encoding`（Go Transport 透明 gzip）。
* **响应侧 Header 透传**：与请求侧对称——上游响应头默认全部透传（`x-ratelimit-*`、request id、`Date`、`Retry-After`、`Content-Encoding`…），只剥 hop-by-hop（Connection/Keep-Alive/TE/Trailer/Transfer-Encoding/Upgrade/Proxy-*）与 `Content-Length`（归一化可能改变长度，Go 重新成帧）。客户端看到的头与直连一致，仅多出 `X-VMR-*`。

### 4.2 模块划分

```
cmd/vmr/main.go            CLI（stdlib flag）：start / check / status / report；Adapter 的 blank import 注册点
internal/core              CanonicalRequest、ErrorClass、Endpoint（无依赖的共享类型）
internal/config            YAML 加载、${ENV} 展开、校验、热加载 watch
internal/adapter           Adapter 接口 + 注册表 + 共享错误分类表/model 改写
internal/adapter/openai    OpenAI 协议透传 Adapter
internal/adapter/anthropic Anthropic 协议透传 Adapter
internal/health            被动健康状态机（冷却、退避、半开探针）
internal/strategy          Dimension 接口 + priority 维度 + 稳定多键排序
internal/router            快照构建 + failover 循环 + 流转发 + 并发闸（核心）
                            ├─ response.go  响应归一化器（详见 §5.5）
internal/server            HTTP 入口、鉴权、Header 黑名单、审计录制、四个端点
internal/audit             审计日志（JSONL 落盘）
                            ├─ housekeep.go  历史文件压缩（zstd）+ 按保留期清理（详见 §9.5）
internal/report            审计日志聚合统计（vmr report，透明读取明文/.zst）
internal/imgprep           请求内联图片降采样（§7）
```

依赖 `gopkg.in/yaml.v3`、`fsnotify`、`golang.org/x/image`（图片降采样的 WEBP/BMP 解码与缩放，Go 官方扩展库）、`github.com/klauspost/compress/zstd`（审计历史文件压缩，纯 Go 无 cgo，§9.5），其余标准库——不用 Web/CLI 框架、不用任何 Provider SDK（透传路由只需"改 URL、注 Key、改 model 字段"，SDK 只带来二进制膨胀与版本纠缠）。

### 4.3 端点一览

| 端点 | 说明 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI 协议入口 |
| `POST /v1/messages` | Anthropic 协议入口 |
| `GET /v1/models` | 全部 Virtual Model，合并格式，带 `vmr_protocol` 字段 |
| `GET /admin/status` | 端点健康 + 并发指标 JSON；仅接受 loopback 来源 |

鉴权（可选 `api_key`）同时接受 `Authorization: Bearer` 与 `x-api-key`，作用于 `/v1/*`。

---

## 5. Adapter 机制（扩展性核心）

接口三个方法；注册用 `database/sql` 驱动模式（编译期注册，非运行时插件）。响应体不经过 Adapter——协议内透传是硬原则，仅有的响应处理（§5.5 归一化）在 router 层，曾经预留的 `TransformBody` 恒等方法从未有过第二种实现，已删（预留即负债）：

```go
type Adapter interface {
    Protocol() string          // "openai" | "anthropic"：该 Adapter 服务的入口协议
    BuildRequest(ctx, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, error)
    ClassifyError(status int, body []byte) core.ErrorClass
}
// 新增 Provider 协议 = internal/adapter/<name>/ 一个包 + main.go 一行 blank import
```

`CanonicalRequest{Model, Stream, Raw, Header}`：只解析路由所需字段，`Raw` 保留原始字节（前向兼容）；`Header` 是黑名单过滤后的客户端 header（凭证已剥除，含 `anthropic-version` 等协议头，§5.4）。model 改写：`map[string]json.RawMessage` 局部替换 `model` 键，未知字段字节原样（不保证键序，无语义影响）。

### 错误分类（决定 failover 质量的关键）

```go
ErrClient     请求本身有问题 → 直接返回客户端，不切换
ErrAuth       401 / 403（非内容类）→ 长冷却（10min 起），切换
ErrRateLimit  429 → 尊重 Retry-After（秒/HTTP-date），切换
ErrEndpoint   端点持续不可用（额度耗尽/402、模型不存在/404 或 400+嗅探）→ 长冷却，切换
ErrTransient  5xx/408/529/超时/网络 → 短冷却（2s 指数退避；带 Retry-After 则从其值），切换
ErrContent    内容合规拦截 → 切换，但不惩罚端点健康（零冷却）
```

`ErrContent` 是"按请求"而非"按端点"的错误：各厂对内容的敏感度不同，换端点常能成功，所以必须继续 failover；但被拦的端点本身完全健康，绝不能因此进冷却（否则一条敏感请求就会把健康端点打下线）。若该端点恰处半开探针中，以 `health.ReportNeutral` 只释放探针、不加深退避。全部候选都被拦时，客户端原样收到最后一次内容错误。

分类表两 Adapter 共享（`adapter.DefaultClassify`），差异点各自覆盖（如 anthropic 的 529）。**必须做 body 嗅探**，因为实测/官方文档显示各家习惯不一：

* MiniMax 未知模型返回 400（非 404）；内容违规错误码 1026/1027；
* DeepSeek Anthropic 口对错模型名的措辞是 "The supported API model names are …"；内容风险走 400 + "risk" 类消息（其官方错误码表 400/401/402/422/429/500/503 中无内容专码）；
* OpenRouter：402 余额不足；**403 = moderation flag / guardrail 拦截**（body 带 "flagged"、`metadata.reasons`）；429 与 503 都可能带 Retry-After；
* 有厂商额度耗尽也发 429（body 见 insufficient/quota/balance/credit）。

嗅探词表：模型类 = `model` × {unknown, not found, does not exist, invalid model, supported}；内容类 = {content_filter, content_policy, moderation, flagged, guardrail, inappropriate, exists risk, data_inspection, (1026), (1027), sensitive, 敏感, 违规, 合规}（中英并收）+ 状态码 451。取舍：误判的代价只是一次无害切换，漏判的代价是永不 failover（400 内容错被当 ErrClient）或误罚健康端点（403 被当 ErrAuth）——宁可宽。

**已知边界**：个别厂商（如 MiniMax）会在 HTTP 200 响应内嵌合规标记（`input_sensitive`/`output_sensitive` 等字段）并可能返回空/替换内容。响应归一化器现在会嗅探这两个标记并记入审计 `norm`（`soft_block_detected`，§5.5），但**仅观测、不干预**：字节原样到达客户端，不触发 failover、不影响端点健康——这是 `docs/SensitiveWordFilter_Analysis_Fable5.md` §3.6 建议的"第一阶段"（先把频率变成可量化的数字，再决定要不要做请求预处理插件，§12.1）。把这类响应变成主动拦截或自动 failover 仍是未实现的未来方向。

### 5.4 请求侧 Header 透传策略

从「严格白名单」改为「默认透传 + 小型黑名单」。白名单实现最初只透传 `Content-Type` 和 Anthropic 协议头，但实测发现会丢掉客户端的合法元数据：User-Agent、OpenAI JS SDK 的 `X-Stainless-*` 7 个、OpenTelemetry 的 `Traceparent` 全部丢失；MiniMax 看到的是 Go 默认 UA，丢失了「这是 OpenAI 兼容客户端」这个信号，可能走不同的服务路径。

LLM SDK 发出的 header 集合是已知且固定的——里面**没有**危险 header（不会发 `Cookie` / `X-Forwarded-For` / `Proxy-Authorization`）。所以默认透传是安全的。需要显式 blocklist 的是真正会出问题的少数几项：

| Header | 原因 |
| --- | --- |
| `Authorization` / `x-api-key` | 客户端发的是「给 VMR 的凭证」，VMR 注自己的 key 上游，绝不能让客户端的 key 漏到上游 |
| `Cookie` / `Proxy-Authorization` | 浏览器/代理会话状态，与 LLM API 无关 |
| `X-Forwarded-*` / `X-Real-Ip` | IP 欺骗向量，上游可能据此做访问控制 |
| `Host` / `Content-Length` / `Transfer-Encoding` / `Connection` | Go Transport 自动管理，传过去会冲突 |
| `Accept-Encoding` | 手工设置会关闭 Go Transport 的透明 gzip：上游若返回压缩体，响应归一化的 regex 会在 gzip 字节上跑，且客户端只收到 Content-Type（拿不到 Content-Encoding）——必须让 Transport 自己协商并解压，各层始终处理明文 |

**与「必须由 Adapter 覆盖」的几项不冲突**：`Authorization` 在 blocklist 里**也是**由 Adapter 用 `Header.Set` 覆盖，blocklist 是第二道防线（如果上游意外处理了一个客户端的 Authorization，VMR 至少不会主动转发）。这种「belt and suspenders」是必要的——Header.Set 覆盖只对 Adapter 构造的请求有效，对 VMR 自己生成的请求（如 `/admin/status`）不适用。

### 5.5 响应侧归一化（`internal/router/response.go`）

上游的响应进入 VMR 后、转发到客户端之前，经过一个**归一化层**。响应体不经过 Adapter（§5 已说明），协议内透传、**Adapter 之间不做转换**是 §3 的设计原则。但光透传会让上游的「指纹」原样到达客户端，**部分客户端 SDK 会因此失灵**：

| 问题 | 表现 | 原因 |
| --- | --- | --- |
| 响应 `model` 是上游名（如 `"MiniMax-M3"`）而客户端发的是虚拟名（如 `"agent"`） | OpenAI JS SDK 按 `model` 做 prompt cache 关联 + per-model hook，**静默丢消息** | SDK 假设 `response.model === request.model` |
| 上游发 `<think>...</think>` 标签在 content 里 | 思考被持久化进 assistant message，下一轮请求的 prompt 含上轮思考 → **模型陷入自我指涉的反馈循环** | MiniMax M3 在 thinking 模式下把推理放在 content 里 |
| 上游发 `<think>...</think>` 后无 trailing newline trim | 助手消息以两个空行起头，每轮累积 | MiniMax 的固定格式 |
| 上游不发 `data: [DONE]` | 客户端 SDK 的 stream 终止逻辑靠 EOF 而非 `[DONE]`，正常路径没问题，**边界条件下触发 `APIUserAbortError`** | MiniMax 直接关 TCP |
| MiniMax thinking=medium 下以纯文本 `Thinking Process:` + 编号小节 1-5 + `Final Polish` 草稿输出思考 | 思考+草稿直接展示给用户（`Reasoning: off` 是 UI 开关，**不影响模型行为**） | 模型在 thinking=medium 下不写 `<think>` 标签 |

**指导原则：直连等价**。客户端经 VMR 收到的字节应与直连上游一致，仅有的偏离是（a）`model` 字段改回虚拟名（虚拟模型抽象的根基），（b）两个 MiniMax-M3 专属修复——且只在**确认命中其确切形态**时才触发，失手时的行为=直连行为，永不更差。

**双传输模式**（按响应逐个决定）：

* **passthrough（SSE 缺省）**：逐 SSE 事件实时转发——真流式，除 model 改写外字节一致。首个承载有效载荷的事件（非空 `content`/`text`/`reasoning_content`/`thinking` 值、`tool_calls`、`partial_json`）一旦证明响应**不是** MiniMax 思考形态，立即定型为透传；此前仅暂扣 role marker/ping 等零载荷小事件。
* **buffered**：整体缓冲，EOF 时单遍 regex 归一化。用于：非 SSE 响应（单 JSON，客户端本来就等完整体）；首个载荷事件含 `<think>` 或 content 以 `Thinking Process:` 开头的 SSE（思考期间客户端本就无正文可看，缓冲无体验损失）；未及判定即 EOF 的小流。**`<think>` 触发的缓冲在 closer 到达后恢复流式**（剥掉 think 块、已缓冲部分一次吐出、其余实时转发）——客户端只等了思考阶段；`Thinking Process:` 形态的结束标记在响应末尾才可识别，无法恢复，只能全缓冲。缓冲上限 32MB，超限放弃归一化降级为原样透传（=直连行为）。
* **opaque**：上游响应带 `Content-Encoding`（Go Transport 未透明解压的压缩形态）→ 原样转发零变换——对压缩字节跑 regex 只会损坏它。

**变换清单**（每项触发条件独立，实际生效的集合记入审计 `attempts[].norm`）：

| 变换 | 触发条件 | norm 标记 |
| --- | --- | --- |
| `"model"` 改写回虚拟名 | 事件/响应体中出现顶层 `"model":"…"`（JSON 转义的 `\"` 不会误命中 content 内文本） | `model_rewrite` |
| 剥 `<think>...</think>` 块 + closer 后的 `\n` 填充（转义与真实换行都收） | 缓冲模式且 regex 命中 | `think_strip` |
| 剥 "Thinking Process:" 结构化思考 | **守卫：首个 `"content":"` 值以 `Thinking Process:` 开头**（前导转义空白可跳过）+ 存在 `Looks good. Pro(ceed)` 自认可标记；按 `\n\n` 切 data: line，弃中间思考行、末行从标记后截取；marker 即首行时原地截取不复制 | `thinking_process_strip` |
| 追加 `data: [DONE]\n\n` | **仅 openai 协议 + SSE + 上游未发**（MiniMax 直接关流；上游已发绝不重复；Anthropic 协议无此哨兵，永不追加） | `done_appended` |
| 软拦截标记嗅探（**不改字节，仅记录**） | 响应体（buffered 整体或 passthrough 单个事件块）内出现 `"input_sensitive":true` 或 `"output_sensitive":true` | `soft_block_detected` |

`isSSE` 由上游响应的 `Content-Type` 判定（含 `text/event-stream`；缺失时回退到客户端的 `stream` 字段），而非盲信请求参数——上游若忽略 `stream` 返回 JSON，原样透传。

**已知边界：quirk 修复靠全局嗅探而非按端点声明**。think-strip / Thinking Process strip 对所有 provider 的响应做形态检测，而不是只对声明了该 quirk 的 endpoint 启用。理论误伤面：某个模型合法地以 `<think>` 或 `Thinking Process:` 开头输出正文（比如复述用户给的模板），会被误剥。选择嗅探的理由：误伤需要「响应开头恰好命中触发形态」这个低概率前提，而 endpoint 级 `quirks:` 配置是一个新概念 + 新配置面 + 用户须理解各厂内部行为才能填对——目前的守卫（首个载荷事件的前缀判定）已把误伤面压到足够小，为它引入配置维度不划算。若未来实际发生误伤，升级路径是加 endpoint 级开关，嗅探逻辑可整体复用。

**历史教训**：v1 的「200 字节 carry + 字节级状态机」换了 4 个版本都有 corner case（carry 装不下 3000+ 字节 think 块、IN_THINK 时 input 字节丢失、flushTail 重入吐残留）；v2 的「全量缓冲 + 单遍 regex」正确但假流式——TTFB=完整生成时长，逼近 OpenAI SDK 的 `X-Stainless-Timeout: 120` 预算（实测 97K prompt 的请求生成 59s）。v3（现行）以**完整 SSE 事件**为处理单位：事件内 JSON 完整，model 改写无跨界问题；跨事件的 think 块只在确认命中后进入缓冲，缓冲的正确性 = v2 的单遍 regex。状态机的复杂度病灶在「字节级切分」，不在「模式切换」。

---

## 6. 调度与健康

### 6.1 调度 = 过滤 + 稳定多键排序

```go
type Dimension interface {
    Name() string
    Compare(a, b *core.Endpoint) int   // <0: a 优先；0: 平手交给下一维度
}
候选序列 = 稳定多键排序( 健康过滤(endpoints), 配置的维度列表 )
```

Priority、Weight、RoundRobin、Latency、Cost 都只是排序维度，任意组合（`strategy: [priority, weight]`）；新增策略 = 新增维度实现，路由主流程永不改。当前实现 `priority`（数字小优先，平手保持配置文件顺序）；其余在路线图。

### 6.2 被动健康：冷却 + 半开单飞探针

对 LLM API 主动探测每次都是计费请求，故全部被动：

* 失败按类别计冷却：Transient 2s 起指数退避（×2 封顶 5min）；Auth/Endpoint 10min 起（封顶 1h）；RateLimit 与 Transient 优先 `Retry-After`（429/503 都可能携带）。内容合规（ErrContent）零冷却（§5）。
* 冷却中被健康过滤剔除；到期进入半开：**只放行一个真实请求当探针**（避免惊群），成功清零、失败退避加深、中性结果只释放探针（`ReportNeutral`）。**探针槽必须在每种结局下都归还**——中性结局共三类：内容拦截（ErrContent）、客户端中途断连、ErrClient（坏请求原样返回）。后两类曾漏掉释放：半开探针恰好撞上一条被取消/被上游判 400 的请求时，`probing` 永久为 true，端点被锁死到进程重启（回归测试 `server_probe_test.go`）。
* 客户端主动断连不计入端点失败（与上游健康无关，防状态污染）；若断连的请求正持有半开探针，探针槽照常释放。
* **配额窗口 vs 余额耗尽不做区分**：两者都归 ErrEndpoint（10min 起指数退避封顶 1h）。曾考虑对"N 小时窗口配额"设更长冷却（如 5h 后再试），否决——厂商错误信号无法可靠区分两种耗尽，且现行封顶 1h 意味着最坏情况每小时只花一次失败探针请求，充值/窗口刷新后一小时内自动回归；专设长冷却省下的探针成本可忽略，代价却是恢复迟钝。
* 健康注册表以 `provider/model/key指纹` 为稳定键、独立于配置快照存活——**热重载不清零冷却**（否则每次改配置都会把 429 中的端点放出来重打）。重启即重置，不持久化。

### 6.3 热加载

fsnotify（监听目录，兼容编辑器原子替换，300ms 防抖）+ SIGHUP 兜底。新配置完整校验，失败保留旧配置并打日志——绝不带病上线。路由表（含 http.Client）随快照原子指针交换，运行中请求持有旧快照直至完成；换入时关闭旧连接池的 idle 连接（in-flight 连接不受影响），重载成功后打印配置摘要。

---

## 7. 请求图片自动降采样

Agent 场景里请求经常带截图/照片附件，但视觉理解通常不需要原始分辨率——图越大，vision token 消耗越高。vmr 提供一个可选开关，在入口把超限的内联图片等比缩小、统一转码，上游收到的是缩小后的版本。

**范围**：只处理请求里的内联 base64 图片（OpenAI `image_url` 的 data URI／Anthropic `source.type=base64`）；不处理 response，也不 fetch 远程图片 URL——两者都超出"改写本地已有字节"的边界。与 §12.1 规划的敏感词过滤插件共享同一接入点（body 解析后、`router.Serve` 之前），但不是同一套机制：图片降采样是具体、确定的处理，不经过预留的插件注册表。

**开关，全局 + 逐虚拟模型覆盖**：全局配置项 `image_downscale`（int，长边像素上限；0/缺省=关闭）——开关即参数，不设独立的 enabled 字段。每个 virtual model 也可以在 `models.<protocol>.<name>.image_downscale` 单独设置，**模型自身的值优先于全局值**；不写则继承全局。`config.ModelConfig.ImageDownscaleMaxPx` 是 `*int` 而非 `int`：nil 代表"未设置，继承全局"，非 nil（含指向 0 的指针）代表"模型显式设置"，0 在模型层面是明确的"强制关闭"——即使全局开着，这个模型也不降采样。用 `int` 存不下这个区分（0 到底是"没写"还是"写了 0"），这是选指针类型的唯一原因。负数（全局或模型级）在 `applyDefaults` 里一律钳制为 0，与既有惯例一致。

运行时对应关系：`router.ModelRoute` 携带同名的 `ImageDownscaleMaxPx *int` 字段（`BuildSnapshot` 从 `ModelConfig` 透传），并提供 `EffectiveImageDownscaleMaxPx(globalMaxPx int) int` 方法解出对某个模型实际生效的上限——nil 接收者安全（未知模型直接回退全局，调用方不用先判空）。`server.chatHandler` 因此需要在做降采样之前先解出 `probe.Model`：JSON 探测解析（`model`/`stream` 两个字段）被挪到了并发闸获取与图片降采样**之前**（原先在两者之后）——探测本身够便宜，不需要等并发闸，这个顺序调整也让"坏 JSON / 缺 model"这两类 400 提前返回，不再白占一个并发槽位。

**检测分层，越靠前越便宜**：

1. **无图请求**（预期占比 95%+）：对已缓冲的请求体做一次 `bytes.Contains` 子串扫描（找 `` "image `` 标记），不解析 JSON——这是唯一的常态开销。
2. **命中标记的请求**：反序列化为 `map[string]json.RawMessage`（与 §5 `adapter.RewriteModel` 同一模式），只识别 `messages[].content[]` 里 `image_url`/`image` 类型块，未知字段字节不动。
3. **单张图片是否需要处理**：`image.DecodeConfig` 读文件头拿到真实像素宽高（不解码像素数据），长边 ≤ 上限则跳过。用真实尺寸判断而非按字节数估算——同样便宜（只读文件头），但没有"压缩率不同导致误判"的问题：512px 的照片可能是 30KB 或 300KB，字节数阈值在两者间必然选错一个。

**处理**：解码 → 等比缩放（`golang.org/x/image/draw`，`BiLinear`）→ 透明通道摊平到白底 → 统一编码为 JPEG（quality 85）。格式支持 JPEG/PNG/GIF（标准库）+ WEBP/BMP（`golang.org/x/image`，Go 官方扩展库而非第三方野包），格式以文件头嗅探为准，不信任声明的 mime type。

**安全边界**：

* 动图（GIF 多帧）直接跳过——缩放会把动画塌缩成单帧，语义改变；`golang.org/x/image/webp` 本身不支持动图解析，遇到动图 WEBP 通常直接解码失败，副作用上也是跳过。
* 解压炸弹防护：`DecodeConfig` 拿到的声明像素总数超过 64MP 的图片拒绝解码，原样透传（一张几十字节的畸形 PNG 足以声明出几亿像素）。
* **fail-open**：解析/解码/编码路径上任何失败（含 panic，`recover` 兜底）都回退到原始字节——这一步的 bug 绝不能让本可成功的请求失败。

**对现有机制的影响**：改动点仍在 `server.chatHandler` 里，`rec.Client.Request.Body` 记录**之后**——审计的"客户端层"仍记录原始请求；"上游尝试层"（`attempts[].request`）自然记录降采样后的实际出站内容，语义与既有的 model 字段改写一致，无需改动审计结构。并发闸（§8）天然限制了图片处理的并发数，不引入新的无界并发。

### 7.1 降采样结果磁盘缓存

**动机**：同一张源图片在同一个目标像素上限下，降采样结果是确定的——没有理由重复计算。更重要的是字节一致性：上游（尤其是 Anthropic）的 prompt cache 按精确字节/token 匹配，如果同一张图片每次请求都重新走一遍 JPEG 编码，编码器不保证逐字节输出相同，任何细微差异都会让上游缓存静默失效，而这类失效在日志里几乎不可见——只会看到 `tokens_in_cached` 莫名其妙地低。有缓存的话，同一张图第二次发出的就是完全相同的字节，上游缓存才谈得上稳定命中。

**Key**：`sha256(原始图片字节)` + 目标 `maxPx`。maxPx 必须入 key——同一张图对不同虚拟模型可能用不同的降采样目标（§7 的逐模型覆盖），两者是两份不同的结果，不能共享一个缓存条目。文件名 `<hex>-<maxPx>.jpg`，值就是降采样后的 JPEG 字节（输出格式固定为 JPEG，见上文）。

**目录**：`imgprep.CacheDir()` 解析 `$VMR_IMG_CACHE_DIR`，未设置则退回 `os.TempDir()/vmr-imgcache`——固定加一层子目录（不同于 `audit.Dir()` 直接用根目录），因为缓存是大量按内容哈希命名的小文件，不该和临时目录里其它东西混在一起。

**查找时机**：只在"确认需要处理"（`longSide > maxPx` 且未触发解压炸弹防护）之后才计算哈希、查缓存——绝大多数图片根本不需要降采样，在这条路径之外查缓存只会给最常见的场景白加一次哈希开销。命中则直接返回缓存字节，跳过解码/缩放/编码整段；未命中则走原有全量处理，处理完成后再写入缓存。

**失效策略：按最近命中时间（mtime）的 TTL，不设容量上限**。命中时 `os.Chtimes` 把 mtime 刷新到当前时间——语义是"最近使用"而非"创建时间"，避免一个长会话里反复引用的截图，仅仅因为会话跑得久就被 TTL 判定过期。容量上限被有意省略：类比 §9.5 审计日志的取舍（当时也只做了按天数的 retention，没做体积上限），图片缓存的体积由"不同源图片数 × 不同 maxPx 数 × TTL 窗口"共同界定，实践中量级有限（典型部署里 maxPx 的取值种类是个位数——一个全局值加少数几个模型覆盖）；真出现磁盘占用问题，再按 §12 的路线图补一个容量上限，不为一个尚未出现的问题预先加复杂度。

配置项 `image_cache_ttl_days`（int，缺省/非正数 → 7 天）。和 `audit_retention_days` 的"0=永久保留"刻意不同：审计日志有取证/成本核算价值，删除是数据丢失风险，需要用户显式选择清理；图片缓存纯粹是性能优化，没有对应的"数据丢失"属性，主动清理是更安全的默认值，不需要用户显式开启。

**触发时机**：不额外起 ticker/goroutine。仿照 §9.5 审计 housekeeping 的"事件触发 + 节流"思路，每次命中"确认需要处理"分支时调用一次节流检查——按缓存目录各自记录"今天是否已经扫过"，每个目录每天最多触发一次异步清理（单次 `os.ReadDir` + 按文件 mtime 判断，顺带清理因进程崩溃遗留的 `.tmp-*` 半写文件）。

**写入**：`os.CreateTemp` + `os.Rename`，与 §9.5 审计压缩落盘同一套 crash-safety 模式；失败一律静默忽略（fail-open——缓存只是优化，写盘失败不该让一个已经处理成功的请求失败）。

**已知限制：service 模式下 `VMR_IMG_CACHE_DIR` 不会被自动继承**。§10 描述的 `write_env_file` 只抓两类变量：config.yaml 里出现的 `${VAR}` 引用、以及固定的几个 proxy 变量；`VMR_LOG_DIR` 是唯一被显式强制写进 plist/systemd unit 的例外。`VMR_IMG_CACHE_DIR` 两类都不占，所以 launchd/systemd 托管下的 vmr 永远看不到当前 shell 里设的这个变量，缓存固定落在系统临时目录，除非手动把它加进生成的 `~/.config/vmr/env`。不算 bug（temp dir 本来就是安全的兜底默认值，缓存丢了顶多重新计算），但没在文档里提前说明容易让人诧异——已在 README 补充说明。

---

## 8. 并发闸

可选全局上限 `max_concurrency`（缺省 0 = 不限）：

* 两个聊天入口共用一个信号量；超限请求**在内存中挂起等待**（channel 阻塞，近似 FIFO），不排队列、不设等待超时（客户端自有超时，服务端再加一层是重复机制）。
* **闸在请求体缓冲完成之后获取**：慢客户端的上传阶段不占槽，闸覆盖的是 CPU（图片降采样）与上游往返阶段——否则几个慢 POST 就能占满全局并发饿死正常请求。
* 等待期间客户端断开 → 立即出队。
* 只闸聊天入口；`/v1/models`、`/admin/status` 不受限。
* 热重载仅当容量变化才换信号量；换闸瞬间新旧持有者叠加、短暂超额（秒级边界行为，可接受）。
* `/admin/status` 暴露 `limit` / `in_flight` / `waiting`。

每 Endpoint 的 rpm/并发精细限流是另一个问题（服务于主动避免 429），在路线图中。

---

## 9. 审计日志

**目标**：原始、完整、可追溯地记录每一个聊天请求的两层往返（调用方↔vmr、vmr↔上游），只记录不分析；请求数、Token 用量等统计由**外部脚本**事后读取 JSONL 完成——本节格式即该脚本的输入契约。

### 9.1 运行行为

| 项 | 行为 |
| --- | --- |
| 开关 | 默认开启；`vmr start -audit=false` 关闭 |
| 目录 | `$VMR_LOG_DIR`，未设置则系统临时目录；启动日志打印实际路径 |
| 文件 | 每天一个：`vmr-audit-YYYY-MM-DD.jsonl`（本地时区，写入时轮转），权限 0600 |
| 时机 | 请求完成后追加一行（含流式全程），不影响 TTFB |
| 失败 | 写盘失败仅打 stderr 日志，绝不影响请求服务 |
| 覆盖 | 两个聊天入口的所有请求，含被 vmr 拒绝的（401/413/坏 JSON/未知模型/协议不符）；`/v1/models`、`/admin/status` 不记 |

### 9.2 记录结构（JSONL，每行一个 Record）

```jsonc
{
  "ts": "2026-07-07T12:15:20.123+08:00",  // 请求到达时刻（RFC3339 毫秒）
  "dur_ms": 864,                          // 总耗时（含并发闸等待与流式全程）
  "model": "claude-failtest",             // Virtual Model；解析前被拒则为 ""
  "protocol": "anthropic",                // 入口协议：openai | anthropic
  "stream": false,
  "outcome": "ok",                        // ok（客户端拿到 2xx）| error | canceled（未写出任何响应）
  "client": {                             // 第一层：调用方 ↔ vmr
    "addr": "127.0.0.1:54321",
    "request":  { "method": "POST", "path": "/v1/messages", "headers": {...}, "body": {...} },
    "response": { "status": 200, "headers": {...}, "body": {...} }   // 未写出时缺省
  },
  "attempts": [                           // 第二层：vmr ↔ 上游，每次 failover 尝试一条，按序
    {
      "endpoint": "anthropic/minimax_badkey/MiniMax-M3",   // protocol/provider/实际模型
      "url": "https://api.minimaxi.com/anthropic/v1/messages",
      "dur_ms": 543,
      "request":  { "headers": {...}, "body": {...} },   // 出站请求（model 已改写）
      "response": { "status": 401, "headers": {...}, "body": {...} },
      "error": "auth"                     // 失败原因：错误类别 | "network: …" | "build: …" | "canceled by client"
    },
    {
      "endpoint": "anthropic/deepseek/deepseek-v4-flash",
      "url": "https://api.deepseek.com/anthropic/v1/messages",
      "dur_ms": 320,
      "request":  { "headers": {...}, "body": {...} },
      "response": { "status": 200, "headers": {...} },   // 见下：成功尝试不存 body
      "norm": ["model_rewrite", "done_appended"]         // 实际生效的响应归一化步骤（§5.5）
    }
  ]
}
```

四条约定（统计脚本必须知道）：

1. **成功尝试的响应 body 不存**：透传恒等，它与 `client.response.body` 字节相同，只在 client 层存一份；两者的字节差异**完整由 `norm` 列表解释**（`model_rewrite`/`think_strip`/`thinking_process_strip`/`done_appended`/`buffered`/`resumed_stream`/`opaque`/`overflow_raw_passthrough`）——**唯一例外是 `soft_block_detected`**（§5.5）：它是纯观测标记，不对应任何字节改动，出现时 upstream body 与 client body 仍然完全相同。失败尝试的错误 body（≤64KB）存在 attempt 内。成功尝试后流中断时 `error` 为 `"truncated: <原因>"`（客户端已收到 2xx，outcome 仍为 ok——status 与 error 并存即"当时 200 但中途断了"）。
2. **body 编码**：合法 JSON 原样嵌入（可直接用 jq 查询，如 `.client.response.body.usage`）；非 JSON（如 SSE 流文本）为字符串。单个 body 的记录上限**联动 `max_body_mb`**（缺省 8MiB，热重载同步）——VMR 接受的请求绝不会在自己的审计里被截断，响应享有同等额度；超限截断并标记 `body_truncated: true`。流式响应的 usage 通常在末尾 SSE 事件里，脚本需从字符串 body 中解析。
3. **凭证掩码**：`Authorization` / `X-Api-Key` / `Api-Key` / `X-Auth-Token` 的值只保留末 4 字符（`"Bearer ***abcd"`），其余 header 原样。这是对"完整 header"要求的唯一偏离——审计文件常驻磁盘，明文密钥外泄风险大于取证价值。
4. **`attempts[].error` 的形态**：错误类别裸词（`auth`/`rate_limit`/…）或带详情的 `"network: …"` / `"build: …"` / `"truncated: …"` / `"canceled by client"`。`vmr report` 聚合时按首个 `:` 前缀归桶，保证错误分布表的基数有界。

### 9.3 实现要点

`internal/audit`：Record 类型 + Logger（互斥追加、按日期轮转，轮转时异步触发 §9.5 的压缩/保留扫描）。server 层用包装 `ResponseWriter` 的录制器捕获 client 层响应（保留 `Flusher`，流式时延零影响，body 记录上限联动 `max_body_mb`）；router 层在 failover 循环中逐次填充 attempts。Record 经 `Serve` 参数显式传递（nil = 关闭，零开销）。

### 9.4 统计分析工具 `vmr report`

读取 §9.2 的 JSONL，输出聚合统计。**与审计格式强耦合：改 §9.2 必须同步改 `internal/report` 及其测试**（复用 `audit.Record` 类型，编译期即绑定）。

```
vmr report [-o dir] <file|glob>...     # 输出 vmr-report.json + vmr-report.md（输入可混合明文 .jsonl 与 §9.5 产生的 .jsonl.zst）
```

* **输入**：一个或多个审计 JSONL 路径/通配符；坏行跳过并计数（`meta.parse_errors`）。全内存聚合，几十 MB 日志无压力。
* **JSON 输出**（`meta.format` 版本号随结构变更递增）：
  * `rows[]` — 粒度 **日期×协议×Virtual Model**（同名模型可同时存在于两个协议组，是两个不同的模型，不可合并）：请求数、ok/error/canceled、流式数、attempts、fallbacks（>1 次尝试的请求数）、tokens in/out 与 tokens_known（可提取 usage 的记录数）、tokens in 的缓存细分（`tokens_in_cached`/`tokens_in_cache_write`，见下）、bytes in/out、时延 sum/p50/p95/max、吞吐（tok/s、bytes/s）。
  * `endpoints[]` — 粒度 **日期×上游端点**：尝试/成功/失败、可用度、错误类别分布、时延 p50/p95。
  * 该粒度可向上卷（仅按模型/仅按日期），不可向下切；更细的问题回原始日志。二次开发（图表/Dashboard/HTML）以此 JSON 为数据源。
* **双指标原则**：tokens 与 bytes 并行统计。usage 提取覆盖四种形态——OpenAI/Anthropic 的 JSON 与 SSE 流（Anthropic 取 `message_start` 的 input + `message_delta` 累计 output，OpenAI 取末尾 usage chunk，字段取最大值以兼容累计流）；无 usage 的记录（上游不回报、请求失败）落在 bytes 与 tokens_known 缺口里，bytes 是它们唯一的用量参考。
* **tokens_in 缓存细分**（`internal/report/usage.go`）：`tokens_in` 是全部输入 token（含缓存），另拆出两个子集——`tokens_in_cached` 是缓存命中（Anthropic `cache_read_input_tokens` / OpenAI `usage.prompt_tokens_details.cached_tokens` / DeepSeek `prompt_cache_hit_tokens`），`tokens_in_cache_write` 是仅 Anthropic 有的缓存新写入（`cache_creation_input_tokens`，按溢价计费，不算命中）。两者都已含在 `tokens_in` 里；新鲜（未命中）部分 = `tokens_in - tokens_in_cached - tokens_in_cache_write`，消费方按需自行相减，JSON 不重复存储比例。**两家上游对"总输入"的定义不同，提取时已做归一**：Anthropic 的 `input_tokens` 不含缓存两项，是分开计数的三个字段相加；OpenAI/DeepSeek 的 `prompt_tokens` 本身已含缓存命中，`cached_tokens`/`prompt_cache_hit_tokens` 只是从中圈出的子集，不可再相加。判据是 usage 对象里是否存在 `input_tokens` 键（Anthropic 独有字段名）。审计日志本身不需要改动——完整原始 body 早已落盘，这些字段只是之前没被读取。
* **Markdown 输出**：从 JSON 再聚合的人读版，收录 T1（总览、按模型、端点可用度）与 T2（按日趋势、上游错误分布）；T3 细分（日期×模型交叉、协议/流式切分）只在 JSON 里。跨组百分位为近似值（以组 p50 按请求数加权重算）。总览/按模型/按日趋势三张表新增"输入缓存命中"（命中占比 + 绝对量）与"缓存写入"（仅总览/按模型，Anthropic 专属，多数行为 `-`）两列。

### 9.5 历史文件压缩与保留（`internal/audit/housekeep.go`）

Agent 场景每轮请求都重发完整对话历史，单日审计文件可达 1~2GB，且体积主因是**跨行**冗余（相邻甚至相隔很远的记录之间大段重复），不是单行内部冗余——分析见 `docs/AuditLogCompression_Analysis_Sonnet5.md`（含本机真实日志的压缩比实测）。据此只压缩**已经轮转完毕、不再写入**的历史文件，且用能看到跨行重复的算法：

* **触发时机**：复用 `Logger.Write` 已有的"日期变化即轮转"判断（无新增定时器/轮询）——检测到 `date != l.date` 时，除了切到新文件，额外对目录做一次 housekeeping 扫描；`New()` 也在启动时扫一次，补上进程重启期间错过的轮转。两处都异步执行（独立 goroutine，`atomic.Bool` 防止扫描重叠），绝不阻塞审计写入或请求服务。
* **压缩**：zstd（`github.com/klauspost/compress/zstd`，纯 Go、无 cgo；库默认压缩级别，未手工调参）。选它是因为 stdlib 的 `compress/gzip` 只有 32KB 滑动窗口，看不到相隔几十万到百万字节的跨行重复，实测压缩比被死死摁在 ~3.3×；zstd 默认窗口是 MB 级别，天然覆盖这种重复模式，实测压缩比 20~75×（同一份日志换算不同压缩粒度的完整数据见分析报告 §2.3）。写入临时文件（`.zst.tmp`）→ 校验 → `rename` 落地 → 确认落地后才删除原文件，中途崩溃不会丢数据也不会留半截 `.zst`；重启后遇到"明文+`.zst` 同时存在"（rename 后、删除原文件前崩溃）视为续跑，直接补删原文件，不重新压缩。
* **保留**：配置项 `audit_retention_days`（缺省 **0 = 永久保留，不清理**）。默认关闭是刻意的产品判断——审计日志是 `vmr report` 成本核算的唯一数据源，静默按天数删除对没读文档的用户是数据丢失风险，需要显式设置天数才启用。
* **零全盘扫描**：审计文件名自带日期（`vmr-audit-YYYY-MM-DD.jsonl[.zst]`），压缩/保留判定只需一次 `os.ReadDir`（目录内条目数 = 保留的天数，不是磁盘总量）+ 文件名正则取日期比较，不解析文件内容、不 `stat` 全盘。同一次目录扫描里，一个文件如果"既该压缩又已过保留期"，本轮就直接压缩后立即删除，不用等到下一天的扫描才清理。
* **`vmr report` 的配套**：§9.4 的 `Build` 按扩展名分支，`.zst` 输入透明解压后再喂给同一套 JSONL 解析——历史压缩文件与当天明文文件可以混在同一次 glob 里（`vmr report 'vmr-audit-*.jsonl*'`），调用方不需要关心哪个是哪个。

---

## 10. 配置参考

```yaml
listen: 127.0.0.1:8800        # 缺省 127.0.0.1:8800
api_key: sk-vmr-xxx           # 可选：vmr 自身鉴权（Bearer 或 x-api-key）
max_attempts: 0               # 上游尝试数上限；缺省 0 = 不限，试遍所有可用候选（正数用于约束尾延迟）
max_body_mb: 8                # 请求体缓冲上限（缺省 8，超限 413）
max_concurrency: 8            # 全局并发上限（缺省 0 = 不限）
image_downscale: 0            # 请求内联图片长边像素上限（§7）；缺省 0 = 关闭；模型自身的 image_downscale（下方）优先于这个全局值
image_cache_ttl_days: 7       # 降采样结果缓存的失效期（§7.1）；缺省/非正数 = 7 天
audit_retention_days: 0       # 审计文件保留天数（§9.5）；缺省 0 = 永久保留，不清理；历史文件压缩为 .zst 与此项无关，无条件在轮转时发生
timeouts:
  connect: 10s                # 连接上游（缺省 10s）
  response_header: 120s       # 上游首字节（缺省 120s）
  stream_idle: 120s           # 上游 body 静默看门狗（缺省 120s）：SSE 流、非流式响应体、错误响应体的读取全部受此约束——响应头之后的一切上游读取都有超时兜底，任何上游停滞都不能把请求永久卡住

providers:                       # "我有什么"——按协议分组，两层 map
  <protocol>:                    # openai | anthropic | 未来任何已注册的 adapter 名
    <name>:
      base_url: https://...      # openai 型拼 /chat/completions；anthropic 型拼 /messages
      api_key: ${ENV_VAR}        # 支持 ${VAR} 展开；未设置的变量展开为空串

models:                          # "对外叫什么、按什么顺序用"——同样按协议分组
  <protocol>:
    <virtual-model-name>:
      strategy: [priority]       # 缺省 [priority]
      image_downscale: 512       # 可选；覆盖全局 image_downscale，只对这一个虚拟模型生效（§7）；写 0 表示对这个模型强制关闭，即使全局开着
      endpoints:
        - provider: <name>       # 必须引用同协议分组下已定义的 provider
          model: <上游真实模型名>
          priority: 1            # 可选；缺省 0，同优先级按文件顺序（稳定排序）——多数场景不必写这个字段，直接按想要的顺序排列 endpoints 即可
```

**两层 map 而非扁平 map + 显式字段**：provider 不再有 `type:` 字段，协议就是它在配置里所处的位置（`providers.<protocol>.<name>`）；一个 model 的 endpoints 只能引用同一 `<protocol>` 分组下的 provider，跨协议引用没有语法能表达它。带来两个直接好处：同一账号的两个协议面可以复用同一个 provider 短名（`openrouter` 在 `providers.openai` 和 `providers.anthropic` 下各存一份），不必再造 `_a` 后缀；同一个 virtual model 名也可以在两个协议分组下各存一份，两个入口各自独立可达（§3）。副作用：`Endpoint` 的 `HealthKey()`/`Name()`（进而 `X-VMR-Endpoint` 响应头、审计日志 `attempts[].endpoint`）改为三段式 `<protocol>/<provider>/<model>`——如果两个协议面复用同一 provider 名、同一 API Key，两段式的 `provider/model` 键会把它们的健康状态错认成同一个端点。

**Priority 是可选的逃生舱，不是必填项**：`strategy.Sort` 用稳定排序，同优先级（含全员缺省的 0）保留配置文件顺序。日常写法是完全不写 `priority`，靠 endpoints 的列表顺序表达优先级；只有需要表达"这几个是同一档位、组内再按 weight/latency 等维度决胜"这类分层语义时才需要显式数字。`vmr check` 按实际生效顺序打印 `1. 2. 3.`（跑一遍 `strategy.Sort`），而不是回显原始 priority 数字，所以不管你写没写这个字段，看到的都是真实的尝试顺序。

校验规则：listen 可解析、providers/models 非空、provider 引用存在（在同协议分组内查找）、协议 key 已注册为 adapter、base_url 合法、endpoint.model 非空；`image_downscale`（全局与模型级）、`audit_retention_days` 负数均在加载期钳制为 0（拒绝配置不如静默纠正——这不是能表达"错误意图"的字段）；`image_cache_ttl_days` 非正数钳制为默认值 7，而不是 0（图片缓存没有 `audit_retention_days` 那种"0=永久保留"的产品含义，见 §7.1）。模型级 `image_downscale` 在解析层是 `*int`：省略该字段与显式写 `0` 在校验后仍然是两种不同的状态（前者继承全局，后者强制关闭），这是唯一一个"缺省值"和"显式 0"语义不同的字段。CLI：`vmr start -c <cfg> [-audit=false]`、`vmr check -c <cfg>`（校验+按生效顺序打印路由表，含每个模型的 image_downscale 覆盖标记）、`vmr status [-c <cfg>]`（渲染健康与并发）、`vmr report [-o dir] <glob>...`（§9.4）。环境变量：`VMR_LOG_DIR`（审计目录）、`VMR_IMG_CACHE_DIR`（图片降采样缓存目录，§7.1，缺省为系统临时目录下的 `vmr-imgcache` 子目录）、配置内 `${VAR}` 展开引用的任意变量。

**启动摘要**：`vmr start` 在启动与每次热重载成功后向 stderr 打印生效配置——listen/鉴权开关/各上限/超时、每个 virtual model 的端点生效顺序与 key 状态（同 `vmr check` 的口径），控制台即可核对运行实例的真实配置。

**vmr.sh（唯一脚本入口，双模式）**：dev 模式（`start/stop/restart/status/logs`）nohup 后台、人肉监督，无 PID 文件、按二进制绝对路径 `pgrep -f` 匹配，start 前先 `vmr check` 拒绝坏配置；service 模式（`service install/uninstall/start/stop/restart/status/logs`）把监督交给 init 系统——macOS 渲染 launchd user agent（`~/Library/LaunchAgents/com.vmr.plist`，KeepAlive+RunAtLoad，stop 走 `bootout` 避免 KeepAlive 复活被杀进程），Linux 渲染 systemd 用户单元（`Restart=always`；系统级部署把 unit 拷到 `/etc/systemd/system` 去掉 `--user` 即可）。模板内嵌于脚本（heredoc 注入绝对路径），不设独立模板目录。**环境是 service 模式的第一大坑**：init 系统不继承 shell 的 export，`install` 自动从当前 shell 抓取 config 引用的 `${VAR}` 与代理变量生成 `~/.config/vmr/env`（0600，存在则不覆盖），launchd 经 `set -a; . env` 加载（不 `set -a` 则 source 的变量不会进入 exec 后的进程环境），systemd 经 `EnvironmentFile=`。macOS 第二坑：TCC 禁止 launchd/sh 对外置卷做文件操作（spawn 报 EX_CONFIG / Operation not permitted），但 vmr 进程自身写卷不受限——故 plist 的 WorkingDirectory 指 `$HOME`、服务日志落 `~/Library/Logs/vmr.log`（macOS 惯例），审计照常写 `VMR_LOG_DIR`。两模式互斥：service install/start 自动停 dev 进程。均经 macOS 实机全周期验证（install→E2E→kill -9 自愈→stop→start→uninstall）。

---

## 11. 关键决策与取舍

| 决策 | 备选 | 取舍逻辑 |
| --- | --- | --- |
| Canonical 格式 = 协议原生格式，透传不翻译 | 自研中间表示（IR） | IR 意味着永远追各家新字段，是 LiteLLM 复杂度失控的根源；透传对新参数天然前向兼容 |
| 不用 Provider SDK | 官方 SDK | 路由只需改 URL/Key/model 字段；SDK 带来二进制膨胀与版本纠缠 |
| 编译期 Adapter 注册（blank import） | 运行时插件（.so/脚本/外部进程） | 满足"插件式扩展、写法统一"，不引入运行时插件的任何成本 |
| 调度 = 过滤+多键排序 | 策略类枚举（Priority/RR/Weighted 各一套） | 组合能力来自排序键叠加；新策略不改主流程 |
| 被动健康 + 半开单飞探针 | 定期主动探测 | 探测 LLM API 每次都花钱；单飞探针把恢复试错成本压到一个请求并防惊群 |
| 错误分类含 body 嗅探 | 严格按 HTTP status 映射 | 实测各家 status 习惯不一（400 当 404 用等）；漏判 = 永不 failover，误判只是一次无害切换 |
| providers/models 按协议分两层 map，协议即配置位置 | 扁平 map + provider.type / model.protocol 显式字段 | 显式字段要么冗余（与 endpoint 实际协议重复）要么矛盾（写错/漏改）；把协议变成 map 的外层 key 之后，"一个 model 混用两种协议" 连语法都写不出来，不是运行时校验能不能查到，是从设计上不给它写的机会 |
| 全败透传最后上游错误 | 合成统一 502 | 保留客户端 SDK 可解析的厂商错误结构；聚合信息在日志里 |
| 健康状态跨热重载保留 | 重载清零 | 清零会把冷却中的端点放出来重打；carry-over 仅十几行 |
| 并发闸：全局、无等待上限 | 每端点限流 / 排队超时 | 全局闸覆盖"保护本机与总用量"诉求且实现极简；客户端自有超时 |
| failover 默认穷尽全部候选 | 固定尝试上限（旧默认 3） | 配了兜底端点就该兜到底，固定上限会让后位端点永远轮不到；尾延迟由可选 max_attempts 与各超时约束 |
| 内容合规错误：切换但零惩罚（ErrContent） | 当普通 4xx 处理 | 该错误按请求不按端点：不切换会中断长程任务，惩罚健康会让一条敏感请求打掉整个端点；靠 403/451 + 中英文词表嗅探识别 |
| 配额窗口与余额耗尽同罪同罚 | 按窗口设 5h 级长冷却 | 厂商信号无法可靠区分两者；封顶 1h 的探针成本 ≤1 失败请求/小时/端点，恢复及时性优先 |
| 审计双层结构、成功 body 去重 | 每层完整存两份 | 透传恒等，重复存储只膨胀文件；失败 body 各自保留因为各不相同 |
| 审计凭证掩码（留末 4 位） | 完整记录 header | 密钥落盘外泄风险 > 取证价值；末 4 位足以区分 Key |
| 无中心 IR、router 只做循环 | —— | router 主流程（`router.go`，含并发闸与流转发）约 550 行，响应归一化器（`response.go`）另约 550 行；若主流程显著变大，说明抽象错了——归一化器的增长对应的是实证 quirk 清单，另算 |
| 图片降采样跳过判定用真实像素尺寸（`DecodeConfig`） | 按字节数估算换算像素 | 压缩率随内容剧烈波动，字节数与像素尺寸不是稳定映射；读文件头一样便宜且没有换算误差 |
| 图片降采样直接实现为函数调用 | 复用/预建 §12.1 的通用请求预处理插件框架 | 插件框架的词库形态、按 Provider 差异化等问题还未定型，图片降采样是具体确定的处理，不该为一个未来设计买单 |
| 动图/超限声明尺寸一律 fail-open 跳过 | 尝试部分处理或报错 | 动图缩放会破坏语义，畸形声明尺寸可能是解压炸弹；跳过的代价只是错过一次可选优化，处理的代价可能是内存暴涨或输出错误 |
| 模型级 `image_downscale` 用 `*int` 区分"未设置"与"显式 0"（§7） | 用 `int`，0 统一当"关闭/继承" | 需求明确要求"模型自己没设置就跟随全局"和"模型显式设为 0"是两种不同状态；`int` 存不下这个区分，只有指针能让"没写"在解析后仍可判定 |
| 降采样缓存 key 含 `maxPx`（§7.1） | 只按源图片哈希建 key | 同一张源图对不同虚拟模型可能配了不同的降采样目标；只按图片哈希会让后写入的结果覆盖或误命中前一个模型的缓存，返回错误尺寸的图片 |
| 降采样缓存只做按 mtime 的 TTL，不设容量上限（§7.1） | TTL + 容量双重限制 | 类比 §9.5 审计 retention 的取舍：先上最简单、可预测的单一机制；图片缓存的体积由源图片种类 × maxPx 种类 × TTL 窗口共同界定，实践中量级有限，真出现磁盘问题再加容量上限，不为未发生的问题预先加复杂度 |
| 降采样缓存目录/失效期通过显式参数传入 `imgprep.Downscale`，不用包级可变状态 | 仿照 `audit` 包用 Set* 全局单例 | `Downscale` 每请求调用一次，调用方（`server.chatHandler`）本来就持有解析好的配置快照；显式传参没有额外成本，还让测试能用 `t.TempDir()` 互相隔离，不用担心跨测试的全局状态污染。唯一必要的包级状态是"缓存目录今天是否已经扫过"的节流簿记，与配置无关，纯粹是防抖动 |
| Endpoint 键（HealthKey/Name）加协议前缀（`protocol/provider/model`） | 保持两段式 `provider/model` | provider 名允许跨协议复用之后，同名同 Key 同上游模型串会在两段式键下撞车，把两个真实不同的端点误判成同一个健康状态实体；三段式从根上消除这个碰撞面，代价是 `X-VMR-Endpoint`/审计 `attempts[].endpoint` 的格式多一段，两处都是人读字符串，没有内部逻辑解析它 |
| Endpoint priority 字段保留但可选，鼓励省略、靠列表顺序 | 删掉 priority，强制纯列表顺序 | 稳定排序下全员缺省 priority=0 就是列表顺序，日常写法已经不需要这个字段；但删掉它会丢失"这几个是同一档位，组内再按 weight/latency 决胜"这类分层表达能力，为未来的排序维度组合（§12.2）保留逃生舱 |
| 请求侧 Header 默认透传 + 小型黑名单（§5.4） | 严格白名单（最初实现） | LLM SDK 发的 header 集合已知且无危险（不会发 Cookie / X-Forwarded-For），全杀掉反而丢 User-Agent / X-Stainless-* / Traceparent 这些上游做 cache 路由决策需要的元数据。blocklist 只剥真正会出问题的几项（凭证、IP 欺骗、Go Transport 管理的几个），其余透传。OpenClaw 验证生效后反过来证明：原白名单太严苛是因为没区分「协议实现内部白名单」与「代理透传黑名单」的职责 |
| 响应侧归一化：双模式——事件级透传缺省，确认命中思考形态才缓冲（§5.5） | 全量缓冲单遍 regex（v2）/ 字节级状态机（v1） | v1 换 4 版都有 corner case（病灶在字节级切分）；v2 正确但假流式，TTFB=完整生成时长，逼近 OpenAI SDK 120s 超时预算，且对不需要修复的 provider（DeepSeek/OpenRouter）也失去流式。v3 以完整 SSE 事件为单位：事件内 JSON 完整、regex 无跨界；think 缓冲在 closer 后恢复流式；失手时=直连行为，永不更差 |
| 响应头默认透传 + hop-by-hop 黑名单（§4.1） | 只转发 Content-Type（旧实现） | 与请求侧同一逻辑：白名单丢 `Retry-After`（客户端 SDK 自身退避失效）、`x-ratelimit-*`、request id（找厂商排障的唯一凭据）。错误路径同样透传，全败时最后一次上游错误连头带体原样返回 |
| [DONE] 仅 openai 协议且上游缺失时补 | 无条件追加（旧实现） | 旧实现对已发 [DONE] 的上游（DeepSeek/OpenRouter）产生双哨兵、对 Anthropic 流注入协议外内容（stainless SDK 恰好容忍属于运气）。条件化后与直连字节一致 |
| 归一化痕迹记入审计 `attempts[].norm` | 不记录 | 成功尝试不存 body 的约定建立在"透传恒等"上，归一化打破了恒等；norm 列表让"上游发的和客户端收的差在哪"在日志里自解释，debug 不用猜 |
| Rewrite `"model"` 字段必须做（§5.5） | 上游 model 名原样转发 | OpenAI JS SDK 假设 `response.model === request.model`，不一致会按 model 做 prompt cache 关联时**静默丢消息**。这是「代理」和「路由」概念被破坏的根——「我发了 agent 收的也必须是 agent」是虚拟模型抽象的根基 |
| Strip `<think>` 标签必须做（§5.5） | 原样转发 | MiniMax M3 thinking 模式下把推理放在 content 里。如果不剥，思考被持久化进 assistant message，下一轮 prompt 含上轮思考 → 模型陷入自我指涉的反馈循环。audit log 中 line 4-27 的 24 轮 tool-use 循环就是反馈循环的典型表现：模型反复 read() 不存在的文件，prompt_tokens 16K → 43K，line 3 直接撞 483K tokens 返 finish_reason=length |
| Strip "Thinking Process:" 启发式只对 thinking=medium 触发（§5.5） | 总是触发 | OpenClaw 的 `Reasoning: off` 是 UI 开关，**不影响模型行为**——模型在 thinking=medium 下不写 `<think>` 标签，直接以纯文本 "Thinking Process:" + 编号小节 1-5 + Final Polish 草稿输出思考。**触发守卫：首个 `"content":"` 值以 "Thinking Process:" 字面量开头**——避免误杀正常回复里包含 "Looks good. Pro" 短语的场景（2026-07-08 审计实测：无守卫时此类回复的前置 chunk 被静默丢弃、非流式 body 被复制成两个 JSON 对象）。启发式看的是 SSE `\n\n` 分隔的 data: line（JSON-escaped 内容里没有真实 `\n\n`），丢弃含 thinking 的中间 line，保留首条（role marker）和末条（含 "Pro" 标记），从 `Pro` / `Proceed` 之后开始截取最终回复；marker 即首行时原地截取不复制，重组时保留末尾空元素以维持 `[DONE]` 前的 SSE 分隔 |
| 审计历史文件压缩用 zstd（整文件、轮转时触发），不做单条记录压缩（§9.5） | 逐条记录 base64/zip 编码 | 本机真实日志实测：单条记录粒度的压缩（无论 gzip 还是 zip+base64）天花板只有 ~3.3×，因为 Agent 场景的冗余主要在跨记录（同一会话每轮重发历史），压缩窗口锁在一条记录内根本看不见；整文件 zstd（默认窗口已是 MB 级）实测 20~75×。逐条压缩还会打破 §9.2"合法 JSON 原样嵌入、可直接 jq 查询"的契约，且落在写路径上；整文件压缩挂在轮转边界，只碰不再写入的历史文件，当天文件保持明文可查询 |
| 压缩/保留复用 Logger 已有的按日轮转边界触发，不设独立 ticker/cron（§9.5） | 周期性 timer 扫描 / 依赖外部 logrotate | 审计文件名自带日期，一次 `os.ReadDir` 即可判定压缩与保留对象，不需要周期性触发就能保证"至多晚一天生效"；新增 ticker 是额外的 goroutine 生命周期管理，外部 logrotate 依赖破坏 vmr"单二进制自包含"的定位（§1） |
| `audit_retention_days` 缺省 0（永久保留） | 缺省一个"合理"天数（如 30） | 审计日志是 `vmr report` 成本核算的唯一数据源，非用户主动设置就被静默删除的风险 > 磁盘空间收益；压缩（§9.5 无条件发生）已经解决了大头的磁盘占用问题，保留期清理是可选的第二层 |

---

## 12. 路线图

### 12.1 请求预处理插件（敏感词过滤，已规划未实现）

目标：请求发往上游前做关键词过滤/替换（外部词库），降低触发厂商内容合规拦截（§5 ErrContent）的概率；对 2xx 内嵌的"软拦截"（如 MiniMax `input_sensitive`）也是唯一的**事前**防线——响应侧现在能**事后观测**到这类拦截（`soft_block_detected`，§5.5），但不能阻止它发生，也不自动 failover。

**本轮明确不预留接口**。理由：插件的词库形态、替换策略、是否需要按 Provider 差异化都未定，先挖的接口大概率与真实插件对不上；预留即负债。待插件设计定型后与其一起实现。§7 的图片降采样已经证明这个接入点可用，但走的是直接函数调用而非插件注册表——两者不是同一套机制。

届时的架构改动点（缝已经存在，改动是局部的）：

* **接入位置**：`server.chatHandler` 中 body 解析之后、`router.Serve` 之前——此处已持有完整缓冲的 `CanonicalRequest.Raw`，改写后 failover 重放自然用的是改写后内容。若需按 Provider 差异化处理，则下沉到 `router.tryOne` 的 `BuildRequest` 之前（每次尝试各改一次）。
* **注册方式**：沿用编译期注册（`filter.Register` + blank import），与 Adapter/Dimension 同构；配置里按 virtual model 或全局启用。
* **需要一并决策的问题**：审计日志记原文还是改写后（或两层都记）；改写是否影响 `stream` 等路由字段（不应）；词库热加载是否走同一 fsnotify 通道；过滤失败（词库损坏）时 fail-open 还是 fail-close。

### 12.2 其他方向

* **排序维度**：`weight`（加权随机）、`round_robin`、`latency`（滑动窗口实测）、`cost`（按配置单价）。
* **Endpoint 级限流**：每端点 rpm/并发（内存令牌桶），主动避免 429。
* **直连语法**：`model: "openrouter:gpt-5"` 绕过 Virtual Model 直达指定 Provider（调试用）。
* **模型改写**：Endpoint 级参数覆盖（强制 temperature、注入 OpenRouter `provider` 路由参数等）。
* **报表增强**：在 `vmr report` 之上做成本核算（按配置单价）、图表/HTML Dashboard（以 report JSON 为数据源二次开发）。
* **可观测**：`/metrics`（Prometheus 文本格式，手写无依赖）、`vmr test <model>`（对每候选发最小请求）。
* **更多协议入口**：gemini；embeddings / images 的同构路由。
* **发布**：goreleaser + Homebrew tap；届时 module 名改为完整仓库路径。
* 远期：能力/标签路由（按 context 长度、vision、tool-use 过滤）、会话粘性维度、本地用量统计（可选嵌 SQLite）。

---

## 13. 已识别、暂不落地的清理项

清理审查（2026-07-08 第四轮）中识别、但判定"动它的收益低于扰动成本"的项。每项都不是 bug，改与不改行为一致；列在这里是为了下次有人盯着它们犹豫时不必重新论证。

| 项 | 现状 | 不动的理由 |
| --- | --- | --- |
| `writeError` 在 `router` 与 `server` 两包各有一份相同实现 | 8 行 × 2，注释互相引用 | 消除重复需在 `core` 增加 HTTP 写出的导出函数，引入的耦合大于省下的 8 行；出现第三份时再统一 |
| `countNested` 在 `config`（未导出）与 `cmd/vmr` 各有一份 | 7 行泛型函数 × 2 | 导出它只为省 7 行，扩大 config 的 API 面；不值 |
| `cmdCheck` 与 `logConfigSummary` 各自实现"按生效顺序打印路由表" | 输出目标（stdout / logger）与格式刻意不同 | 统一需要 writer+格式抽象，比 20 行重复更复杂 |
| `respStream.Read` 会返回 `(0, nil)`（等待更多字节时） | io.Reader 文档不鼓励该形态 | 唯一消费方是 `copyFlush`（显式处理）；改成阻塞式内部循环会让 idle 看门狗失去以读取为粒度的心跳 |
| 健康注册表中被配置删除的端点条目跨热重载残留 | 每条目几十字节，重启清零 | 有界（≤ 历史配置的端点总数），加清理逻辑需要 diff 新旧快照，复杂度不成比例 |
| 测试里存在三个各自为政的 mock 上游（`upstream`/`probeUpstream`/`stallingUpstream`） | 各 30~50 行，职责不同（脚本化状态 / 探针时序 / 停滞） | 测试代码合并会互相牵连；等真实收敛需求出现再说 |
