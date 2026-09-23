<!-- Ver 2026-09-23 02:43, by Claude Opus 5.5 -->

# Virtual Model Router (vmr) — 设计方案 · Part 1：路由核心

本文档描述 vmr 路由核心的设计方案：定位、架构、机制与关键决策。读完即可理解并二次开发路由主线。使用文档见 `README.md`（英文）/ `README.zh.md`（中文）。

**这是 v4 版设计文档的 Part 1**：
* 审计日志离线消费方（`vmr analyze`）独立成篇，见 `docs/VirtualModelRouter_Design_v4_Analytics.md`（Part 2）；
* Token-Plan 额度感知路由独立成篇，见 `docs/VirtualModelRouter_Design_v4_Quota.md`；
* 实时请求统计与控制台独立成篇，见 `docs/VirtualModelRouter_Design_v4_LiveStats.md`；
* 战略定位与竞品分析见 `docs/VirtualModelRouter_Design_v4_Strategy.md`。

---

## 1. 定位与设计边界

vmr 是本地运行、单二进制、配置驱动的 LLM 智能路由器。客户端仅面向稳定的 Virtual Model（如 `coding` / `agent` / `claude`），Provider 拓扑、密钥轮换、优先级与故障切换全部由 vmr 屏蔽。Unix 风格：零数据库、零外部依赖、零运行时插件。

**永久不做的边界清单**：用户管理、计费收单、Prompt 编排、工作流引擎、运行时动态插件（.so/脚本）、**跨协议翻译转换**。

---

## 2. 核心概念

| 概念 | 职责 |
| --- | --- |
| **Virtual Model** | 对外暴露的虚拟模型名，代表能力而非特定厂商；对应一组按协议隔离的 Endpoint-Group。 |
| **Provider** | 可复用的上游账号抽象：包含名称、凭据、代理设置以及按协议分键的 `base_url` 映射。 |
| **Endpoint** | 最小调度单位：Provider × 实际模型名 × 调度属性；由 Endpoint-Group 展开而成。 |
| **Adapter** | 协议适配器：负责出站请求构造、模型字段重写、响应协议探测与错误分类。 |
| **Strategy** | 调度策略体系：由条件过滤（准入剔除）与稳定多键排序维度（候选重排）组成。 |

---

## 3. 协议模型：多入口，永不翻译

vmr 同时暴露三个原生协议入口，**严格禁止跨协议翻译转换**：

```
POST /v1/chat/completions   OpenAI Chat Completions 协议 ──► 只路由到该协议上游端点
POST /v1/messages           Anthropic Messages 协议      ──► 只路由到该协议上游端点
POST /v1/responses          OpenAI Responses 协议        ──► 只路由到该协议上游端点
```

### 决策逻辑
跨协议双向流式翻译（如 Anthropic 的 typed SSE 事件与 OpenAI chunk 互转，或 Thinking/Tool-Use 块映射）极易破坏语义保真度并随上游迭代迅速腐化。主流厂商均提供原生兼容端点，同协议内透传保证零语义损耗与前向兼容性。新协议采用“新 Adapter + 新路由行”独立接入。

### 核心机制
* 协议是 Adapter 的内在属性，也是模型配置的第一级分类键。端点必须在所属协议下声明有效的 `base_url`。
* 路由入口不匹配时直接返回 404，并在响应中提示正确的协议入口。
* vmr 生成的错误信封采用合并格式：`{"type":"error","error":{"type":"...","message":"..."}}`，兼容各类 SDK 的解析约定。

### 3.1 Responses 协议接入要点与陷阱防御（`internal/adapter/openairesponses`）

作为 OpenAI 官方力推的新一代协议（`POST /v1/responses`），vmr 原生接入且不做跨协议互译：
* **结构差异**：请求体顶层为 `input` 与可选 `instructions`（替代 `messages`），响应为 `output[]`（typed Item 数组），流式传输采用类型化 SSE 事件（`response.output_text.delta`/`response.completed` 等），天然无 `[DONE]` 哨兵。
* **协议专属实现**：
  * `jsonscan.RewriteInputRoles`：`role_map` 作用于顶层 `input` 数组（处理混杂带/不带 `role` 的 Item）；
  * `adapter.SessionFingerprint`：`openai-responses` 分支提取顶层 `instructions` 与 `input` 前导 system/developer 消息组合生成联合哈希，保障加密 reasoning item 会话粘性；
  * `imgprep.rewriteResponsesImage`：处理扁平 `image_url` 字符串（非嵌套对象）及 `file_id` 块兜底；
  * **单飞恢复探测防死锁机制**：半开端点后台恢复探测（`probe.ResponsesRequest`）必须按 `ep.AdapterType` 发送携带有效 `input` 字段的请求。若误用 Chat Completions 消息体会因缺少 `input` 遭上游 400 拒绝，**导致半开端点在冷却一次后因探测失败陷入永久死锁**。
* **响应归一化短路**：Responses 协议原生将 reasoning 隔离为独立 typed Item，不存在思考混入 content 的怪癖，流式状态机直接短路至纯透传（`modePassthrough`）。

---

## 4. 系统架构与请求流程

### 4.1 请求生命周期

```
Client ──► POST /v1/chat/completions | /v1/messages | /v1/responses
  │
Server     入口审计打标 ──► 鉴权校验 ──► 缓冲请求体(≤8MB, 超限413) ──► 获取并发闸
  │        ──► 图片识别与降采样 ──► 解析 model/stream ──► 提取 RequestFacts
  ▼
Router     寻址 Virtual Model ──► 校验协议 ──► 健康过滤 ──► 条件准入过滤
  │        ──► 稳定多键排序 (Priority/Weight) ──► 额度感知重排 ──► 会话亲和置顶 (Sticky)
  ▼
Failover   循环遍历可用候选集 (直至成功或候选用尽)
  │
Adapter    BuildRequest：改写 URL、注入 Key、就地改写顶层 model 字段（其余字节透传）
  ▼
Upstream   ├─ 2xx 成功 ──► 响应流归一化 ──► 客户端转发 ──► 报告健康成功 ──► 刷新粘性指针 ──► 审计落盘
           ├─ 可重试错误 ──► 错误分类器判定 ──► 标记端点冷却 ──► 触发下一次 Attempt
           └─ 客户端错误 ──► 原样透传回客户端，不重试，不惩罚端点
```

### 4.2 架构硬性规则
1. **请求体全额缓冲**：流式与非流式请求均在入口完整缓冲，作为 Failover 重放的前置保证。
2. **首字节后禁止重试**：Failover 切换仅在上游返回 4xx/5xx 可重试错误或发生网络故障、且尚未向客户端写出首字节前触发。一旦收到 2xx 成功响应并开始向客户端转发首字节，通道立即锁定为流式交付，后续网络或上游故障只能断流并记入审计，绝不在半途中断重发；亦不对 HTTP 2xx 软拦截（如部分厂商在 200 中内嵌 `input_sensitive:true`）做重发切换，坚守字节保真透传。
3. **失败语义与追踪头**：遍历失败原样返回最后一次上游错误（保留上游厂商结构）；无可用候选时返回 503。每次出站响应附加 `X-VMR-Attempts`、`X-VMR-Route-Reason`（记录选择原因：`order` / `quota` / `sticky`）及 `X-VMR-Failover` 轨迹。
4. **角色转换（`role_map`）**：部分上游不认识特定角色（如拒收 `developer`）。Provider 级配置 `role_map` 允许在发出请求前就地替换角色名，其余字节原样保留。

### 4.3 模块划分

| 核心包 | 职责与设计边界 |
| --- | --- |
| `internal/core` | 无依赖共享契约类型：`CanonicalRequest`、`RequestFacts`、`ErrorClass`、`Endpoint` 等。 |
| `internal/config` | 配置加载、环境变量展开、严格 YAML 校验、热重载监听与定价/额度模型规范化。 |
| `internal/adapter` | `Adapter` 接口、协议注册中心、共享错误分类与会话指纹提取。 |
| `internal/adapter/*` | 各协议适配器实现（`openai`、`anthropic`、`openairesponses`）。 |
| `internal/router` | 核心路由循环：快照管理、Failover 调度、并发闸、In-flight 跟踪与额度重排编排。 |
| `internal/server` | HTTP 服务入口、鉴权、请求特征提取、基础监控与控制台端点。 |
| `internal/health` | 被动失败驱动的健康状态机：指数退避、Retry-After 遵循与后台单飞探测名额控制。 |
| `internal/sticky` | 会话亲和注册表：维护基于 Prompt Cache 的会话连续性与内存淘汰。 |
| `internal/respnorm` | 响应流归一化：流式状态机、按协议已知位置的模型名改写、厂商特定思考块剥离与用量嗅探。 |
| `internal/audit` | 双层原始字节审计记录落盘（JSONL）与按日归档压缩（zstd）。 |
| `internal/imgprep` | 内联图片特征识别、解码缩放与本地磁盘缓存。 |
| `internal/guard` | Agent Guard 双向安全护栏：锚定规则检测核心、出向干预与入向隐写净化挂载点（详见 Agent Guard 一节）。 |
| `internal/quota` | 额度感知路由记账、周期时间数学与 Headroom 配速打分。 |
| `internal/livestats` | 瞬态小时 WAL、轻量 Rollup 账本与遥测聚合（零内部依赖）。 |

### 4.4 核心 HTTP 端点
* **协议入口**：`POST /v1/chat/completions`、`POST /v1/messages`、`POST /v1/responses`。
* **模型发现**：`GET /v1/models`（聚合全部虚拟模型，输出协议兼容列表）。
* **基础健康检查**：`GET /health`（免鉴权，仅应答进程存活与运行时间，不泄露拓扑信息）。
* **运维与状态**：`GET /status`（受鉴权保护，暴露进程元数据、配置时效、并发及配额快照）。
* **实时遥测与日志**：`GET /stats`（指标聚合与活跃请求）、`GET /log`（实时控制台日志流）。
* **统一控制台**：`GET /status.html`（系统全景监控）、`GET /models.html`（模型与配额拓扑）、`GET /log.html`（日志终端）、`GET /help.html`（Agent 配置向导）。
* **静态分析报告托管**：`GET /reports/*`（可选，只读托管本地生成的分析报告，受鉴权保护）。

### 4.5 统一控制台契约
四个控制台页面共享一套内嵌运行时与一组数据契约：

* **共享运行时**：`console.css`/`console.js` 单一来源（`go:embed`），每页在首次服务时经注入标记内联一次，无标记的页面原样返回。公共面：`mountConsole`（页头、导航栏、页脚、鉴权弹窗；`refresh` 三态 `countdown`/`stream`/`static`）、`VMRAuth`（401 → 密钥弹窗 → 重试一次的统一流程，`localStorage` 单键跨页共享）、共享数字格式化（两位小数、整体去尾零、千分位——页面禁止各自手搓）与共享弹窗机制（Esc、点击外部、焦点管理）。
* **刷新纪律**：Overview 整页统一 5 分钟时钟（点击即刷、页签隐藏时可见地暂停）；Live Requests/Recent Failures/并发区附加自适应轮询（活跃 ~1s、空闲 15s）；Log 页由流状态驱动；Help 静态。
* **`/status` `alerts[]` 纪律**：只放**可操作状态**，滚动统计量绝不进——config 校验问题各一条（`kind: config`）；端点仅在 cooldown 期间在列（`consecutive_failures > 0` 但未冷却的降级态由拓扑表 Health 列承载，否则无流量时残留失败计数不消零，会把徽章永久钉在非零，违反告警收敛纪律）；quota `used_frac ≥ 1` 为 error、`≥ 0.9` 为 warning，阈值刻意写死不作配置项。排序：error 优先，再按 `kind`+`ref` 稳定排序；同一账号多限额触发合并为一条。
* **`/status` 端点行契约**：`provider`/`key_label`/`model` 取自 `core.Endpoint`；`from_fallback` 恒出现（含 `false` 零值，供前端切分）；`headroom` 从路由半区既有的配额导出读取（禁止第二套公式重推，与配额差分测试纪律一致），未配额端点省略该字段。
* **流量计数只有一个来源**：`/status` 的 `traffic` 块只保留 sticky 注册表规模；请求数、结果分布与 token 用量一律由 livestats 账本（`/stats`）提供，重启后可恢复。进程内不再维护第二本计数器。

---

## 5. 协议适配与响应归一化

### 5.1 适配器模型与错误分类体系
```go
type Adapter interface {
    Protocol() string
    BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, []byte, error)
    ClassifyError(status int, body []byte) core.ErrorClass
}
```

错误分类体系（`core.ErrorClass`）直接决定 Failover 决策与端点健康度奖惩：

| 错误分类 | 语义与触发条件 | 调度与健康处理 |
| --- | --- | --- |
| **`ErrClient`** | 客户端入参缺陷（格式非法、缺少必填参数等） | 直接向客户端返回错误，不重试，不处罚端点。 |
| **`ErrAuth`** | 凭据失效（401 / 403 明确认证失败、OAuth Token 过期） | 触发长冷却（10min 起，封顶 1h），切换下一端点。 |
| **`ErrRateLimit`** | 上游超频限流（429） | 优先遵循 `Retry-After`（封顶 1h），切换下一端点。 |
| **`ErrEndpoint`** | 端点持续不可用（402 余额耗尽、404 模型不存在、网关转发失败） | 触发长冷却（10min 起，封顶 1h），切换下一端点。 |
| **`ErrTransient`** | 临时网络故障（5xx、408、超时、连接重置） | 触发短冷却（5s 起指数退避，封顶 5min），切换下一端点。 |
| **`ErrContent`** | 内容安全与合规拦截 | 零冷却切换（非端点故障，不处罚健康度）。 |
| **`ErrContextLimit`** | 历史上下文超出端点物理窗口 | 零冷却切换（端点本身健康，尝试更大窗口候选）。 |
| **`ErrQuirk`** | 厂商专属协议私有约束拒绝（如缺少思考上下文回传） | 零冷却切换（模型偏好冲突，不处罚端点健康度）。 |

另有 `ErrBuild` / `ErrNetwork` / `ErrCanceled` / `ErrTruncated` 专用于审计记录，标识在 HTTP 响应到达前或连接非正常中断的状态。

### 5.2 请求头透传策略
采用**默认透传 + 敏感黑名单剥离**机制：
* **必须剔除的 Header**：客户端凭证（`Authorization` / `x-api-key`，由 Adapter 注入上游密钥覆盖）、浏览器会话状态（`Cookie` / `Proxy-Authorization`）、伪造链路头（`X-Forwarded-*` / `X-Real-Ip`）以及 Go 传输层自管头（`Host` / `Content-Length` / `Transfer-Encoding`）。
* **保留客户端元数据**：`User-Agent`、`X-Stainless-*`、OpenTelemetry `Traceparent` 等均安全透传，保留上游所需的客户端特征。

### 5.3 响应流归一化（`internal/respnorm`）
遵循**直连等价**原则：客户端经 vmr 收发的数据与直连上游保持字节级一致。仅有的法定偏离：
1. **模型名称重写**：将响应体中的上游物理模型名改写回客户端请求的虚拟模型名（防止客户端 SDK 丢弃消息）。只改写各协议的已知模型字段位置：非流式响应的顶层 `model`；流式响应中 Chat Completions chunk 的顶层 `model`、Anthropic `message_start` 的 `message.model`、Responses 事件的 `response.model`。嵌套在内容、工具参数或厂商扩展里的同名字段是客户端可见内容，不改写；
2. **MiniMax 思考块剥离**：识别并剔除混杂在内容中的 `<think>...</think>` 标签或特定思考引导文本，防止多轮会话产生自我指涉循环；
3. **SSE 哨兵补齐**：对 Chat Completions 协议缺失 `data: [DONE]` 终止哨兵的上游，在流结束时补齐。

**双传输模式**：
* **透传模式（Passthrough）**：缺省模式。首个载荷事件确认无思考块污染后，立即定型为纯实时逐事件透传，保障首字延迟（TTFT）。
* **缓冲模式（Buffered）**：非流式响应、小尺寸流或检测到思考块起头的流进入内存缓冲（上限 8MB），完成正则清理闭合后再平滑恢复输出。
* **异常断流与部分交付保护（Non-EOF Failure）**：若上游流在首包发送后中途连接重置或超时失败：
  * **安全字节交付（`truncated_flush`）**：对非 SSE 响应或已定型为 Passthrough 的流，先向客户端 flush 已收到的有效字节，随后由 router 触发 `panic(http.ErrAbortHandler)` 中止连接，确保客户端感知网络断开而非误判为空成功的 200 OK；
  * **思考块扣住不交付（`truncated_withheld`）**：若流仍处于缓冲判定阶段（思考块待剥离），已收字节坚决不交付给客户端，杜绝向客户端泄露未闭合的 `<think>` 标签进而导致下轮上下文陷入自我指涉反馈循环。
  * 审计记录顶层 `outcome` 仍按传输层记录 `ok`，但 Attempt 记 `truncated: <原因>`。

---

## 6. 调度与健康状态机

### 6.1 调度管线
候选端点选取依次执行过滤与多级重排：
$$\text{全部端点} \xrightarrow{\text{健康过滤}} \text{可用集} \xrightarrow{\text{条件准入过滤}} \text{合格集} \xrightarrow{\text{多键排序}} \text{梯队序列} \xrightarrow{\text{额度重排}} \text{配速序列} \xrightarrow{\text{会话亲和置顶}} \text{最终候选序列}$$

### 6.2 被动健康状态机与后台单飞探测
* **退避与冷却**：失败分类决定端点冷却时长（短冷却 5s~5min 指数退避，长冷却 10min~1h，遵守 Retry-After 但封顶 1h）。
* **后台异步单飞探测**：端点冷却到期进入半开状态后，**不放行真实请求充当探针**。系统抢占单飞名额，由后台协程发送轻量 Nonce 回显探针验证连通性：
  * **成功恢复**：2xx 且回显校验通过，端点恢复可用；
  * **中性释放**：探测触发客户端错误、内容拦截或规格限制，释放名额但不加深退避；
  * **探测失败**：探测超时或报错，端点重入退避冷却。
  * **槽位强归还保证**：任意路径退出均确保释放探测单飞槽，杜绝端点假死。
* **独立性**：健康状态跨配置热重载保留，进程重启则重置。

### 6.3 条件准入路由（Condition-based Routing）
准入判断解决“端点能否物理承载该请求”的硬淘汰问题：
* **请求特征提取（`RequestFacts`）**：在入口缓冲后一次性探测多模态图片、工具声明（Tools）及预估 Token 长度。
* **物理模型能力默认表（`model_defaults`）**：集中声明各真实模型的上下文窗口与能力（`capabilities: [text, tools, image, ...]`, `max_context_tokens`）。
* **单一权威来源与宽松降级**：能力与上下文窗口只在 `model_defaults` 声明（精确真实模型名 > `”*”` 通配兜底），虚拟模型层没有覆盖旋钮——同一个真实模型无论挂在哪个虚拟模型名下，声明的能力都一致，不存在”虚拟模型说的”和”真实模型能做的”互相打架的可能。对于上下文长度超限判定，采取”宁高估、不低估”策略，若预估淘汰导致可用候选全空，则回退至硬性条件集合，交由真实上游 400 兜底。

### 6.4 会话亲和路由（Sticky Model）
针对多轮 Agent 场景，优先将同一对话锁定在最近一次成功服务该会话的端点上，保护上游 Prompt Cache。
* **指纹算法**：分别计算 System Prompt 与首条非系统消息的内容哈希作为联合亲和键，代价恒定且不随会话增长膨胀。
* **有效性解耦**：亲和有效性遵循 Provider 级配置的 TTL；内存注册表采用 24 小时兜底淘汰防止内存泄露。
* **亲和置顶**：仅作用于已通过健康与准入过滤的候选，绝不复活故障端点。
* **并发饱和避峰保护**：当粘性端点所在 Provider 并发满载且排队超时逃逸到备用端点成功时，不改写粘性指针（临时避峰借道），保护后续轮次继续回流原端点。

### 6.5 额度感知重排（Quota Pacing）
针对按周期计费的套餐账号，通过 Headroom 余量比进行配速调度。紧随策略多键排序之后，仅在同一优先级梯队内部调整顺序，不越级、不淘汰。完整机制见 `docs/VirtualModelRouter_Design_v4_Quota.md`。

---

## 7. 图片自动降采样与磁盘缓存

针对 Agent 常见的长截屏场景，提供入口级内联 base64 图片自动缩放，降低 Vision Token 消耗。

* **参数层级**：全局配置长边像素上限（`image_downscale`），虚拟模型层支持显式覆盖或置 `0` 强制关闭。
* **分层廉价检测**：无图请求做快速子串扫描直接跳过；含图请求反序列化提取图片块，通过仅读文件头获取尺寸（`DecodeConfig`），未超限直接跳过。
* **安全边界**：
  * GIF 动图一律跳过缩放（规避解压炸弹与动画坍缩风险）；
  * 声明尺寸超过 16MP 拒绝解码，原样透传；
  * 解码与缩放全链路 Fail-open：任何异常均降级为原图透传，不阻断请求。
* **磁盘缓存**：按 `sha256(原始图片字节) + 目标像素上限` 缓存压缩结果，保证多次重发时精确字节一致，避免打断上游 Prompt Cache。缓存通过双闸（TTL + 容量上限）在访问时惰性清理。

---

## 8. 并发闸（Concurrency Limiter）

系统提供两层正交的并发控制体系：全局入向门控与 Provider 级会话感知并发控制。

### 8.1 全局并发闸（Global Limiter）
可选的全局并发上限（`max_concurrency`）：
* 获取时机位于请求体缓冲完成之后，慢客户端上传不占并发槽位，闸保护的是 CPU 计算与上游网络往返。
* 超限请求在内存中挂起等待，客户端主动断开时立即出队注销。

### 8.2 Provider 级并发控制与会话感知分层调度（Provider Concurrency）
针对上游供应商对账号级并发的硬性限制，在 `providers[]` 层级支持独立配置最大在途请求数（`concurrency`）与排队等待时长（`concurrency_queue`，默认 10s，上限 30s）：
* **租约信号量契约**：仅在真正向物理上游发起网络调用前获取（Acquire），通过 `defer` 保证无论请求正常完成、SSE 流式中断、客户端取消还是 Panic，槽位百分之百归还，杜绝泄漏。
* **会话感知分层调度**：
  * **全新会话（未命中 Sticky）**：上游无缓存资产，目标 Provider 并发打满时**快速跳过（Fast-Skip）**，零等待尝试下一个可用候选 Provider，盘活备用算力；
  * **粘性会话（命中 Sticky）**：上游拥有宝贵的 Prompt Cache，目标 Provider 满载时进入内存微排队（等待至 `concurrency_queue`），吸收并发短峰保全缓存；排队中若客户端主动断开连接，立即中止调度循环，杜绝向备选端点做无效探测，且不把断开误归为超时；
  * **临时逃逸不污染指针**：Sticky 请求排队超时逃逸至备用 Provider 成功返回时，**不改写** Sticky 内存指针，后续请求依然优先回流原端点续用缓存。
* **全候选满载诊断**：当某模型的所有可用候选端点均因并发打满而被跳过时，返回明确的 503 `vmr_no_candidates` 诊断信息（`all candidate endpoints for model <name> are busy (provider concurrency limit reached)`），避免误导为条件过滤拒绝。
* **热重载平滑复用**：配置热重载时，参数未变更的 Provider 保持活体信号量实例，在途请求平滑排水。
* **可观测性**：响应头 `X-VMR-Route-Reason` 记录排队耗时（`conc_waited=...`），`X-VMR-Failover` 记录并发饱和跳过（`busy` / `busy_timeout`），`/stats` 实时暴露 `providers_concurrency` 水位。

---

## 9. Agent Guard（双向安全护栏）

面向 Agent 流量的双向安全层，**默认关闭**：配置中省略整个 `guard:` 键（或不接线）时，请求路径保持零开销、字节保真透传。立项边界与 `respnorm` 的用量嗅探同类——只审查标准 LLM API 线路上的内容（如 `tool_use.input`/`tool_calls.arguments`），不代理 MCP 协议、不介入客户端的工具执行路径。

### 9.1 检测核心（`internal/guard`）

* **锚定规则**：凭据规则按五重锚定准入（左边界 + 字面前缀 + 定长 + 字符集 + 熵）。五锚齐备为 Tier 1（可作在线依据）；缺锚者为 Tier 2（仅供离线人工复核，永不在线改写/阻断）——`sk-` 泛前缀是开放式形状匹配，永远停留 Tier 2；五锚齐备的厂商规则（`sk-ant-api03-`/`AKIA`/`ghp_` 等）已是 Tier 1。
* **扫描机制**：锚定正则规则 + JSON 字符串值遍历（`Engine.Scan`）+ Aho-Corasick 字面预筛；`InspectToolCall` 提供工具调用风险与凭据回显的**离线**判定（在线闸门已移除，见 9.3）。
* **指纹**：`Hit.FP` 为确定性无盐哈希（无 Salt 持久化机制，KNOWN_ISSUES K-G19）；`GuardRecord.Ver` 标记规则集版本——版本升级使新旧 `FP` 不可比，跨版本聚合"同一凭据重复出现"会漏配对，是指纹固有属性而非 bug。

### 9.2 出向干预（Outbound）

挂载于 `chatHandler`（图片降采样之前），配置 `guard:` 后扫描每个请求：

* `mode: audit_only`——命中记入审计（唯一纯由离线证据校准过的模式）；`mode: block`——仅 Tier 1 命中在进入路由前以 HTTP 400 拒绝（Tier 2 永不阻断，K-G5）。
* `guard.trusted_providers`：虚拟模型的全部候选端点（含 fallback）均为可信 provider 时整体豁免，Snapshot 构建时预计算。
* 早期的 replace/伪名还原模式已整体移除（K-G1）：网关侧改写凭据的收益远低于其复杂度与误还原风险。

### 9.3 入向干预（Inbound）

挂载于响应归一化下游，**唯一**的在线入向动作是 Unicode 隐写净化：

* 重帧 SSE 事件（非流式体直接净化），剥离不可见/隐写字符：A 档（Tags 区、C0 控制）与 B 档（Bidi 覆盖/隔离、零宽空格、BOM、软连字符）无条件删除；C 档（ZWNJ/ZWJ/LRM/RLM、变体选择符）**只计数标记绝不删除**（K-G7）——它们是波斯语/印地语/阿拉伯语排版与全部 ZWJ emoji 序列的必需字符。
* 双线格式解析（字面 UTF-8 与 JSON `\uXXXX`/代理对转义），ASCII 快路径零分配。
* 从不阻断、不改变 HTTP 状态码；内部错误或压缩（不透明）响应一律 fail-open；异常上游无 SSE 空行分隔符时由内部持有上限兑底而非无界增长。唯一配置旋钮：`guard.inbound.sanitize_invisible_runes`（默认开）。
* 在线的 Tool Call 双级闸门、三协议熔断帧与非流式阻断已整体移除（K-G15/ADR-15）：客户端自己的审批门与沙箱掌握严格更多的上下文，网关在信息量更少的位置重做同一判断只会得到更差的判断——检测层 `InspectToolCall` 保留为离线取证函数。

### 9.4 审计盖章与数据源双路

接线后每个请求在审计记录顶层盖章 `guard` 字段（检测命中、档位、指纹、规则集版本）；`vmr analyze` 优先读该盖章（权威路径），对缺少盖章的记录（未接线时期的历史数据）按需对 `Client.Request`/`Client.Response.Body` 现场补扫（Fallback Path）——补扫无论护栏是否配置都会运行，只有连请求体都为空的记录才真正跳过。

### 9.5 离线消费面

* `vmr analyze`：`macro/guard.json` 可选切片（见 Part 2 的宏切片说明）；
* `vmr diagnose -guard`：5 探针矩阵主动审计上游中转安全（工具调用篡改、静默上下文截断、思考剥离、用量膨胀、隐写字符注入）；
* `tools/guard_corpus_scan`：对本仓真实审计语料的校准复现工具，检测全部委托 `internal/guard`，不携带第二份私有实现。

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 默认关闭 | 省略 `guard:` 键即零开销透传 | 安全层必须是显式 opt-in；不配置不进请求路径 |
| 放量上限 audit_only 起步 | block 仅对 Tier 1、仅入路由前 | 在线改写/阻断从未在生产流量上验证过判定精度，宁缺勿滥 |
| C 档字符只计数 | 绝不删除变体选择符类 | 无差别删除会当场破坏正常语言排版与 ZWJ emoji |
| 无盐确定性指纹 | 不配置、不持久化 Salt | replace 模式移除后伪名还原不复存在，盐失去存在理由 |

---

## 10. 审计日志规范（Audit Log）

审计日志（`audit.Record`）为系统对外发布的唯一持久化事实源，按日轮转并采用 JSONL 存储。它是 Routing Half 与 Analytics Half 之间**唯一的物理契约**。

### 10.1 核心 Record 结构与分层模型
```jsonc
{
  "ts": "2026-07-07T12:15:20.123+08:00",
  "dur_ms": 864, "ttft_ms": 120,
  "model": "coding", "protocol": "anthropic-messages", "stream": false,
  "outcome": "ok",                        // ok | error | canceled
  "client": { "addr": "...", "request": {...}, "response": {...} },
  "images": [ { "format": "jpeg", "bytes": 812000, "width": 3024, "height": 4032, "downscaled": true } ],
  "facts": { "has_image": false, "has_tools": true, "estimated_tokens": 1280 },
  "guard": { "ver": 3, "hits": [...] }, // Agent Guard 盖章（guard: 接线时才有，见 Agent Guard 一节）
  "attempts": [                           // 每次 failover 尝试一条
    {
      "endpoint": "anthropic-messages:minimax:MiniMax-M3", // 展示标签（:分隔）
      "protocol": "anthropic-messages", "provider": "minimax", "model": "MiniMax-M3", // 结构化三段
      "dur_ms": 543, "request": {...}, "response": {...},
      "forwarded": true,                  // 路由半区在 forwardSuccess 唯一置位
      "error": "...", "error_class": "...", "norm": ["model_rewrite"]
    }
  ]
}
```

### 10.2 六大核心契约约定（跨半区唯一契约）
1. **成功响应体单存去重**：上游成功 Attempt 不重复存储 Body（透传恒等，与 `client.response.body` 字节完全一致），两者的字节差异完整由 `norm` 列表解释（`model_rewrite`, `think_strip`, `done_appended` 等）；纯观测标记（如 `soft_block_detected`, `crlf_framing_suspected`, `truncated_withheld`）仅记录不改变转发字节。失败 Attempt 的错误体设 128KB 截断保护。
2. **容量与截断口径**：入站请求体不设记录截断上限（只要 vmr 接受即完整记录）；出站响应体审计副本设 16MiB 上限（`server.recorderBodyCap`，超出追加截断标记，但客户端转发链路始终完整无损）。
3. **严格凭据脱敏**：敏感认证头（`Authorization`、`x-api-key`、`Cookie` 等）在审计中仅保留末 4 字符掩码；`audit.IsCredentialHeader` 与路由转发黑名单解耦，`vmr replay` 重建请求时依此彻底剔除脱敏占位符。
4. **错误分类与转发权威标（`forwarded`）**：`error_class` 包含 7 种 HTTP 错误与 4 种非 HTTP 故障（`build`/`network`/`canceled`/`truncated`）；**`attempts[].forwarded` 由路由半区在 `forwardSuccess` 唯一置位**（软拦截 2xx 不置位，流式截断仍置位）。分析侧直接通过 `audit.Attempt.IsForwarded()` 消费该权威事实，严禁自行根据状态码反推。
5. **图片元数据采集**：仅在请求侧对内联图片通过 `DecodeConfig` 提取宽高与格式，与模型是否开启降采样解耦；响应侧与远程图片不采集。
6. **特征原样落盘（`facts`）**：`core.RequestFacts` 在入口一次性计算后原样存入 `rec.Facts`，离线分析与重放直接读取，严禁事后重新解析反推。

---

## 11. 诊断与重放工具

* **静态检查（`vmr check`）**：无网络 I/O 的静态配置扫描与生效路由表预览。
* **网络诊断（`vmr diagnose`）**：连通性探针，按阶段测试代理可达性、DNS、TLS 及最小模型回显。走代理的 Provider 自动跳过直连检测。
* **二进制重放（`vmr replay`）**：从审计记录重建真实请求并重发，复用生产环境完全一致的构建管线与计费逻辑。
  * **精确定位记录**：支持 `-req basename:line`（跨命令标准化坐标）、`-ts <时间戳>`（毫秒对齐匹配）与 `-line N` 三种互斥定位方式；`-print` 支持直接打印原始记录；
  * **安全重建头**：重放重建 Header 时，同时经 `FilterClientHeaders` 与 `audit.IsCredentialHeader` 双重过滤，防止脱敏打码串当作真实凭据误发给上游；
  * **回放审计（`--record`）**：将重发结果追加至指定文件，完全遵循生产流量相同的审计结构与耗时定义。
* **在线冒烟（`vmr smoke`）**：使用 `X-VMR-Provider` / `X-VMR-Target-Model` 请求头将请求受控钉至指定后端。
  * **只收窄不开口**：仅在虚拟模型已配置的合法端点中收窄，绝不凭空穿透未声明端点；
  * **协议安全**：控制头在转发给上游前强制剥离（`FilterClientHeaders`），绝不泄露给外部 Provider。

---

## 12. 核心架构决策汇总

| 决策点 | 采纳方案 | 放弃方案与核心权衡 |
| --- | --- | --- |
| **协议转换** | 原生透传，永不翻译 | 自研 IR 中间层会陷入永远追踪各厂商新增特性的无底洞，透传具备最佳天然前向兼容性。 |
| **依赖管理** | 纯标准库 + 极简扩展，不用厂商 SDK | SDK 带来巨大依赖膨胀、版本锁死与隐式行为，代理层仅需操作 URL/Header/Body 字节。 |
| **插件模型** | 编译期 blank import 注册 | 避免动态库（.so）或脚本运行时带来的稳定性与性能隐患。 |
| **Failover 边界** | 仅首字节发出前可重试，全败透传最后错误 | 避免重发导致上游产生重复副作用；透传最后错误保留客户端 SDK 能够解析的错误结构。 |
| **2xx 软拦截处理** | 不做运行时 Failover，字节原样透传，审计标记 | 流式首包已交付不可撤销；上游已计费扣量，二次重试造成翻倍延迟；通过事前敏感词过滤防范。 |
| **健康状态** | 后台单飞异步探测，不让业务请求当探针 | 杜绝业务大请求充当探针导致耗时绑定与流量雪崩；探针成功只衰减退避深度，真实流量成功才清零。 |
| **错误分类与嗅探** | 结合 HTTP 状态码与 Body 关键短语智能分类 | 实测厂商状态码习惯不一（如 400 当 404）；漏判导致无法 failover，误判仅是一次无害切换。 |
| **协议私有约束分类** | 归入独立枚举 `ErrQuirk`（切换且零冷却） | 区分于 `ErrContextLimit`；若归入 `ErrEndpoint` 会触发 10min 长冷却误伤健康端点。 |
| **内容合规与窗口超限** | `ErrContent` / `ErrContextLimit` 零冷却切换 | 该类错误属于请求内容或模型静态参数属性，非端点故障，切换下一端点不处罚健康度。 |
| **代理路由模型** | Provider 级显式布尔开关，无全局/环境变量隐式回退 | 流量走向必须在 config.yaml 静态可查，隐式环境变量是排障最难发现的陷阱；直连 DNS/TLS 检查对代理端点自动跳过。 |
| **准入与排序分离** | 淘汰（`strategy.Eligible`，看请求事实）与排序（`strategy.Sort`，只看端点 `Priority`）是两个独立函数 | `Sort` 的签名不接收请求，排序不可能感知请求。只有一个排序键且无配置入口，所以不设接口与注册表；出现第二个排序维度时再引入。 |
| **窗口超限降级** | `WithinContext` 单独实现，不放进硬条件表 | 唯一需要“全体超限时不真拒绝、留给上游 400 兜底”的条件，硬编码两行避免破坏接口纯洁性。 |
| **会话亲和（Sticky）** | 默认开启，基于 System Prompt + 首条消息联合哈希 | 保护 Prompt Cache 降低延迟与成本；TTL 挂在 Provider 账号级（基础设施属性），免跨模型重复配置；设 24h 校验上限防内存淘汰失效。 |
| **Model 字段重写** | 免分配字节 Splice（`jsonscan.RewriteModel`） | 全量 unmarshal+marshal 消耗热路径 CPU 并重排键序；Splice 单趟扫描除 model 值外逐字节保留。 |
| **思考块剥离守卫** | 首个非空 content 必须以 `<think>` 开头才认定思考形态 | 避免正文中合法引用 `<think>` 标签的代码或文本被静默删除；Thinking Process 剥离同样仅限 thinking=medium 且匹配前缀。 |
| **图片降采样开关** | 模型级配置采用 `*int` 指针 | 区分“未显式配置（跟随全局）”与“显式置 0（强制对该模型关闭降采样）”，裸 int 无法表达。 |
| **图片缓存防污染** | 缓存 Key 为 `sha256(源图) + maxPx` | 不同虚拟模型对同一张图片可能有不同的缩放规格，只哈希图片会导致跨模型缓存覆盖错乱。 |
| **重放凭据过滤** | `vmr replay` 同时经 `FilterClientHeaders` 与 `audit.IsCredentialHeader` | 审计记录中的 Header 已经过掩码脱敏，若直接重放会把 `****` 掩码字符串当成真实 Key 发出。 |
| **审计截断与权限** | 错误体 128KB 截断记录，文件 0600 / 目录 0700 | 内存与磁盘有界，转发字节始终完整；审计日志与分析派生产物均包含对话明文，严格权限隔离。 |

---

## 13. 已识别、暂不落地的清理项

判定“动它的收益低于扰动成本”的项。每项都不是 Bug，改与不改行为一致；在此沉淀避免后续重复论证（分析半区清单见 `docs/KNOWN_ISSUES.md`）。

| 项 | 现状 | 不动与保留现状的理由 |
| --- | --- | --- |
| `cmdCheck`/`cmdStart` 与 `vmr diagnose` 生效路由表打印分别实现 | 排序已统一为 `EffectiveOrder()`，打印格式仍分别实现 | 输出目标（stdout / logger）与信息维度不同（diagnose 需标注连通性结果）；统一打印格式需要额外抽象，比各自数行格式化代码更复杂。 |
| `respnorm.Read` 等待更多字节时返回 `(0, nil)` | `io.Reader` 文档不鼓励该形态 | 唯一消费方是 `copyFlush`；改成阻塞式内部循环会让 idle 看门狗失去以读取为粒度的心跳监测。 |
| 测试中存在三个各自为政的 mock 上游 | `upstream` / `probeUpstream` / `stallingUpstream` 各几十行 | 职责明确分离（脚本化状态 / 探针时序 / 停滞挂起）；测试代码强行合并会互相牵连。 |
| `runProbe` 半开端点后台探测是 fire-and-forget 协程 | 不挂在服务退出 drain 链路之下 | 最坏情况仅丢失一次探测结果（下次启动从零状态开始，非数据损坏）；接上退出信号需传递上下文，复杂度不成比例。 |
| `audit.Logger.Close` 不等待后台 housekeeping 压缩收尾 | `hkWG.Wait()` 仅供测试使用 | 压缩写入采用原子 rename，具备天然 crash-safe；不碰当日正在写的文件，避免关机阻塞在数 GB 的 zstd 压缩上。 |
| `vmr analyze` 不主动过滤 `vmr replay --record` 的产出 | 回放记录（带 `replay_of` 字段）在 glob 命中时计入统计 | 属于用户主动行为（`--record` 默认不写且为独立文件）；混入本身有时是预期行为（验证回放 Token 用量）。 |

