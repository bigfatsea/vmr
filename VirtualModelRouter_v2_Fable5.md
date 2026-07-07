<!-- Ver 2026-07-07 (V2.2 增量：Anthropic 协议入口 + 并发限制), by Fable 5 -->

# Virtual Model Router — 设计方案 V2

> 本文档是对 V1（`VirtualModelRouter.md`）的架构评审与全面修订。
> 定位、问题定义、完成标准均继承 V1，不再重复论证；本文重点是：
> 修正 V1 中的矛盾点、把模糊概念落实为可实现的机制、并给出分阶段路线。

---

## 0. V1 评审结论（变更摘要）

V1 的定位和边界判断是对的：本地单二进制、配置驱动、只做 Virtual Model Routing、不向平台演化。这些全部保留。但有六个问题需要修正：

**① "排除 Plugin System" 与 "Provider 可扩展" 自相矛盾。**
V1 一边排除 Plugin System，一边要求未来接入 Anthropic、Gemini 等。需要区分两个概念：

* **运行时插件系统**（动态加载 .so / 脚本 / 外部进程）——继续排除，它是复杂度黑洞；
* **编译期 Adapter 注册机制**（Go `database/sql` 驱动模式）——这恰恰是本项目的核心机制。

新增一个 Provider = 新增一个实现固定接口的包 + 一行 blank import，不改动任何其他代码。这满足"插件式按需扩展、写法统一"，又不引入运行时插件的任何成本。

**② "复用官方 SDK" 与 "只依赖标准库" 自相矛盾。**
V1 要求 Adapter "优先复用官方 SDK"，又要求"整体依赖标准库"。V2 明确：**不引入任何 Provider SDK**。理由：MVP 只做 OpenAI Compatible 透传，根本不需要解析完整协议；SDK 会带来二进制膨胀和版本纠缠；Adapter 需要的只是"改 URL、改 Header、改 model 字段、转换错误"，自己写不超过两百行。

**③ "Canonical Request/Response" 是过度设计陷阱。**
自己发明一套中间表示（IR）意味着永远追着所有 Provider 的新字段跑，这是 LiteLLM 复杂度失控的根源之一。V2 明确：**Canonical 格式 = OpenAI Chat Completions 格式本身**。它是事实标准，绝大多数 Provider 原生兼容；Adapter 的职责从"双向翻译到自研 IR"简化为"把非 OpenAI 协议翻译成 OpenAI 协议"。OpenAI Compatible 的 Provider 走零转换透传路径。

**④ Health 机制在 V1 中未定义，且"定期恢复探测"方向不对。**
对 LLM API 做主动探测是要花真金白银的（每次探测都是一次计费请求）。V2 采用**被动健康 + 冷却 + 半开恢复**：失败驱动冷却，冷却到期后放行一个真实请求充当探针，成功即恢复。零额外成本，机制简单。

**⑤ Strategy 应统一为"过滤 + 多键排序"模型，而不是策略枚举。**
V1 把 Priority / Round Robin / Weighted / Sticky 列为并列策略，每加一种策略就多一个分支。V2 重新设计：**调度 = 健康过滤 → 按配置的维度序列做稳定多键排序 → 顺序尝试**。Priority、Weight、RoundRobin、Latency、Cost 都只是"排序维度"，可以任意组合（这正是"多维度多组合优先级调度"的需求本身）。新增策略 = 新增一个排序维度实现，路由主流程永远不变。

**⑥ V1 缺失多个决定成败的工程细节。**
包括：流式请求的 failover 时机（首字节发出后不可切换）、非流式请求体缓冲（failover 重放的前提）、错误分类（哪些错误该切换、哪些该直接返回）、Router 自身鉴权、`/v1/models` 端点（很多客户端启动时必查）、超时定义。V2 全部落实。

## 0.5 动工前终审修订（V2.1）

动工前对 V2 做了最后一轮审核，并用真实 Provider（MiniMax / DeepSeek / OpenRouter）实测了错误行为。修订八处，均已合入正文：

**(a) 请求体缓冲统一到所有请求。** 流式请求在首字节发出前同样允许 failover，而 failover 的前提是请求体可重放。因此不区分流式/非流式，所有请求体在入口处缓冲（默认上限 8MB，超限返回 413）。原 3.1 "仅非流式缓冲"的表述是漏洞。

**(b) ErrorClass 扩容：`ErrQuota` 改为 `ErrEndpoint`。** 实测发现 MiniMax 对"未知模型"返回 **400**（OpenAI 惯例是 404），OpenRouter 余额不足返回 **402**。若 400 一律判为 ErrClient 不切换，配错模型名的端点将永远无法 failover。因此：额度耗尽、模型不存在（404 或 400+body 嗅探）、402 等"该端点持续不可用但换端点可解"的错误统一归入 `ErrEndpoint`（长冷却 + 切换）；Adapter 的 ClassifyError 对 400 做关键词嗅探（"unknown model" / "model_not_found" 等）。

**(c) 全部候选失败的返回语义。** 有真实上游尝试 → 原样返回最后一次上游错误（原 status + 原 body），让客户端看到真实原因；无候选可试（全部冷却或列表为空）→ 返回 503 + OpenAI 风格错误 JSON。所有响应带 `X-VMR-Attempts` 头便于排查。

**(d) Header 白名单策略。** 客户端请求头只透传 `Content-Type` 与 `Accept`；`Authorization` 绝不透传（由 Adapter 注入 Provider Key）；不透传 `Accept-Encoding`（让 Go Transport 做透明 gzip）。响应侧透传 `Content-Type` 及状态码。

**(e) model 字段改写机制。** 请求体解析为 `map[string]json.RawMessage`，仅替换 `model` 键后重编码：未知字段原样保留（对上游新参数前向兼容），代价是不保证 JSON 键序（无语义影响）。

**(f) 健康状态跨热重载保留。** 健康注册表以 `provider/model/key指纹` 为稳定键、独立于配置快照存活。否则每次改配置都会把冷却中的端点放出来重打一轮 429。

**(g) `/v1/models` 响应格式。** OpenAI list 格式：`{"object":"list","data":[{"id":"coding","object":"model","owned_by":"vmr"}]}`。

**(h) 默认 Provider 组合更新为 MiniMax / OpenRouter / DeepSeek**（三家均已实测可用，MiniMax 与 DeepSeek 同时提供 OpenAI 兼容接口，走透传路径）。见第 4 节配置示例。

---

## 0.6 增量设计 V2.2：Anthropic 协议入口 + 并发限制（MVP 完成后新增）

### 0.6.1 多协议入口，永不翻译

原 Phase 2 计划写一个"翻译型" anthropic Adapter（`/v1/messages` ↔ Chat Completions 双向转换）。**这个计划作废**：双向流式翻译（Anthropic 的 `message_start` / `content_block_delta` 事件流 vs OpenAI 的 chunk 流、tool-use / thinking 块的语义映射）正是 0-③ 警告过的复杂度黑洞；且 MiniMax、DeepSeek 等厂商已原生提供 Anthropic 兼容端点，翻译不再创造任何价值。替代原则：

> **VMR 不做协议转换。** 调用方用 OpenAI 协议（`POST /v1/chat/completions`），只在 OpenAI 兼容端点间路由；用 Anthropic 协议（`POST /v1/messages`），只在 Anthropic 兼容端点间路由。每个协议族内部都是零转换透传。

落实机制：

* **Protocol 是 Adapter 的属性**：接口新增 `Protocol() string`（`openai` Adapter → `"openai"`，`anthropic` Adapter → `"anthropic"`）。
* **Virtual Model 的协议自动推断**：由其全部 endpoints 的 Adapter 推断；混协议的模型在配置校验期直接报错。不新增配置字段——消灭"声明与实际不符"这类错误，而不是校验它。
* **入口按协议路由**：`/v1/chat/completions` 只接受 OpenAI 协议模型，`/v1/messages` 只接受 Anthropic 协议模型；模型存在但协议不符 → 404 并在 message 里说明该用哪个入口。
* `CanonicalRequest` 结构不变（两种协议的请求体都是顶层 `model` + `stream` 字段），新增 `Header`：服务端白名单透传的协议头（目前只有 `anthropic-version`、`anthropic-beta`）经由它传给 Adapter。
* **anthropic Adapter 也是透传型**：拼 `{base_url}/messages`、注入 `x-api-key`（Anthropic 惯例，实测 MiniMax 强制要求）、缺省补 `anthropic-version: 2023-06-01`、改写 model 字段。错误分类与 openai 共用一张默认分类表，另加 529（overloaded）→ ErrTransient。
* Router 自身鉴权同时接受 `Authorization: Bearer` 与 `x-api-key`（Anthropic SDK 只会发后者）。
* `/v1/models` 返回两种客户端都能解析的合并形态（同时带 OpenAI 的 `object:"list"` 与 Anthropic 的 `has_more`、`type:"model"` 字段），列出全部协议的模型。

实测备注（2026-07-07）：MiniMax / DeepSeek 的 Anthropic 兼容口分别为 `https://api.minimaxi.com/anthropic/v1` 与 `https://api.deepseek.com/anthropic/v1`（Adapter 追加 `/messages`）；DeepSeek 对未知模型返回 400 且措辞为 "The supported API model names are …"，错误嗅探词表需覆盖（"supported" + "model"）。

### 0.6.2 全局并发限制（挂起等待，不排队）

新增可选配置 `max_concurrency`（缺省 0 = 不限制）。语义：

* 两个聊天入口共用一个全局信号量；超限请求**在内存中挂起等待**（Go channel 阻塞，唤醒近似 FIFO），前面的请求完成即放行。不引入队列结构、不设等待上限。
* 等待中的请求若客户端断开（context 取消），立即出队，不占坑。
* 只闸聊天入口；`/v1/models`、`/admin/status` 不受限。
* 热重载时仅当容量变化才更换信号量（换闸瞬间新旧持有者叠加、短暂超额，属可接受的边界行为，日志可见）。
* `/admin/status` 暴露 `in_flight` / `limit` 便于观察。

每 Endpoint 的 rpm/并发精细限流仍留在 Phase 2，与本全局闸互不冲突。

---

## 1. 定位（继承 V1，一句话）

本地运行、单二进制、配置驱动的 LLM 路由器：客户端只连稳定的 Virtual Model（如 `coding` / `smart` / `cheap`），Provider、账号、Key、策略、故障切换全部由 Router 隐藏。Unix 风格工具，零数据库、零 Web UI、零外部依赖。

---

## 2. 核心抽象（五个，只多一个 Adapter）

| 概念 | 职责 | 说明 |
| --- | --- | --- |
| **Virtual Model** | 对外暴露的唯一模型名 | 代表能力而非厂商；对应一组 Endpoint |
| **Provider** | 一个可复用的上游定义 | type（用哪个 Adapter）+ base_url + api_key |
| **Endpoint** | 最小调度单位 | Provider × 实际模型名 × 调度属性（priority/weight…）；同 Provider 不同 Key 也是不同 Endpoint |
| **Adapter** | 协议翻译插件 | 把 OpenAI 格式请求翻译成 Provider 协议、把响应翻译回来、把错误归类 |
| **Strategy** | 候选排序器 | 健康过滤后，按配置的维度序列对 Endpoint 排序 |

Health 不再作为独立概念，而是 Endpoint 自带的运行时状态（见 3.5）。

---

## 3. 架构设计

### 3.1 请求流程

```
Client ── POST /v1/chat/completions (model: "coding")
  │
  ▼
Server        鉴权(可选) → 解析 model/stream 字段，其余字节原样保留
  │
  ▼
Router        查 Virtual Model → 健康过滤 → 多键排序 → 得到候选序列
  │
  ▼ 逐个尝试（failover 循环）
Adapter       BuildRequest：改 URL / Header / model 字段（透传路径零拷贝）
  │
  ▼
Upstream      发送；流式则边收边转发
  │
  ├─ 成功 → TransformBody → 回写客户端 → 上报健康成功
  └─ 失败 → ClassifyError → 更新健康状态 → 试下一个候选
```

两条硬规则：

* **请求体一律在入口处缓冲**（默认上限 8MB，超限 413），failover 时原样重放；每个候选只试一次，总尝试数默认 3。流式请求同样缓冲——首字节前的 failover 也需要重放请求体。
* **流式**：只有在**首个数据块转发给客户端之前**才允许 failover；首字节已发出后发生错误只能断流并记录。这是流式代理的物理约束，必须写进设计而不是留给实现者踩坑。实现上该约束自然成立：只有上游返回 2xx 才开始向客户端转发，非 2xx / 网络错误都发生在写出之前。

失败语义（0.5-c）：有真实上游尝试 → 原样返回最后一次上游错误；无候选可试 → 503 + OpenAI 风格错误 JSON；响应带 `X-VMR-Attempts`。

### 3.2 模块划分

```
cmd/vmr/main.go            入口 + CLI（stdlib flag，不用 cobra）
internal/config            YAML 加载、校验、${ENV} 展开、热加载
internal/server            HTTP 入口、鉴权、/v1/chat/completions、/v1/models、/admin/status
internal/router            候选生成 + failover 循环（全项目最核心、也最短的代码）
internal/strategy          Dimension 接口 + priority 等维度实现
internal/health            被动健康状态机（冷却、半开）
internal/adapter           Adapter 接口 + 注册表
internal/adapter/openai    MVP 唯一内置 Adapter（透传型）
internal/adapter/anthropic Phase 2
```

预期 MVP 代码量 ≤ 2500 行（不含测试）。router 包本身应当只有一两百行——如果它变大了，说明抽象错了。

### 3.3 Adapter 插件机制（本项目的扩展性核心）

接口刻意压到三个方法。参考 one-api 的 adaptor 切分方式（GetRequestURL / ConvertRequest / DoResponse），但去掉它与计费、DB 耦合的部分：

```go
// CanonicalRequest：以 OpenAI Chat Completions 为规范格式。
// 只解析路由需要的字段，其余字节不动 —— 保证对上游新参数天然前向兼容。
type CanonicalRequest struct {
    Model  string          // 已解析
    Stream bool            // 已解析
    Raw    json.RawMessage // 原始请求体
}

type ErrorClass int
const (
    ErrClient    ErrorClass = iota // 请求本身有问题：直接返回客户端，不切换
    ErrAuth                        // 401/403：该 Endpoint 长冷却，切换
    ErrRateLimit                   // 429：按 Retry-After 冷却，切换
    ErrEndpoint                    // 端点持续不可用（额度耗尽/402、模型不存在/404、400+嗅探）：长冷却，切换
    ErrTransient                   // 5xx/408/超时/网络：短冷却，切换
)

type Adapter interface {
    // 规范请求 → Provider 的 HTTP 请求（URL、Header、body 改写）
    BuildRequest(ctx context.Context, ep *Endpoint, req *CanonicalRequest) (*http.Request, error)

    // Provider 响应体 → OpenAI 格式响应体（流式与非流式）。
    // OpenAI 兼容 Provider 直接原样返回，零转换。
    TransformBody(body io.ReadCloser, stream bool) io.ReadCloser

    // Provider 错误 → 统一错误类别，驱动 failover 与健康状态
    ClassifyError(status int, body []byte) ErrorClass
}
```

注册采用 `database/sql` 驱动模式：

```go
// internal/adapter/registry.go
func Register(name string, a Adapter)
func Get(name string) (Adapter, bool)

// internal/adapter/openai/openai.go
func init() { adapter.Register("openai", &OpenAI{}) }

// cmd/vmr/main.go —— 接入新 Provider 唯一需要动的"其他地方"：一行 import
import (
    _ "vmr/internal/adapter/openai"
    _ "vmr/internal/adapter/anthropic" // Phase 2 加这一行
)
```

**新增一个 Provider 的完整工作量 = 一个包（实现三个方法）+ 一行 import。** 配置里 `type: anthropic` 即可引用。写法完全统一，互不影响。

错误分类（ClassifyError）是最容易被忽略但决定 failover 质量的部分：400 类错误通常换哪个 Endpoint 都会失败，必须直接返回客户端；401 是这个 Key 的问题，应该切换并长冷却。把这个判断放进 Adapter 是因为各家错误格式不同——实测：MiniMax 对未知模型返回 400（需嗅探 body 中的 "unknown model" 归入 ErrEndpoint），OpenRouter 余额不足返回 402，有的厂商额度耗尽也返回 429（需看 body 区分 ErrRateLimit 与 ErrEndpoint）。

### 3.4 调度模型：过滤 + 多键排序

```go
// 一个维度 = 一个可比较的排序键。有状态的维度（round_robin）自己管理状态。
type Dimension interface {
    Name() string
    Compare(a, b *Endpoint) int // <0: a 优先; 0: 平手交给下一维度
}
```

```
候选序列 = 稳定多键排序( 健康过滤(endpoints), 配置的维度列表 )
```

配置形如 `strategy: [priority, weight]`——先按 priority 分层，同层内按 weight 加权随机。failover 就是顺着这个序列走，不存在第二套逻辑。

* MVP 只实现 `priority`（数字越小越优先，平手按配置文件顺序）。
* Phase 2 加 `weight`（加权随机）、`round_robin`、`latency`（滑动窗口均值）、`cost`。
* Sticky 未来作为一个"按会话哈希置顶"的维度实现，同样不动主流程。

这个统一模型是 V2 对"支持多维度多组合优先级调度"的直接回答：组合能力来自排序键的叠加，而不是策略类的枚举。

### 3.5 健康机制（被动 + 冷却 + 半开）

每个 Endpoint 维护两个字段：`consecutiveFailures int`、`cooldownUntil time.Time`。

* 失败 → 按错误类别计冷却：`ErrTransient` 基础 2s，指数退避（×2，封顶 5min）；`ErrRateLimit` 优先尊重 `Retry-After`（支持秒数与 HTTP-date 两种格式）；`ErrAuth`/`ErrEndpoint` 直接 10min 起。
* 冷却期内被健康过滤器剔除。
* 冷却到期 → 半开：**单飞探针**——只放行一个真实请求充当探针（其余请求继续跳过该端点），成功清零计数，失败重新冷却并加深退避。
* 成功 → 计数清零。

健康注册表以 `provider/model/key指纹` 为稳定键，独立于配置快照存活，热重载不清零（0.5-f）。不做主动探测（每次探测都花钱）；不持久化到磁盘（重启即重置，符合无状态原则）。

### 3.6 其他工程决策

* **Router 自身鉴权**：配置可选 `api_key`，客户端以标准 `Authorization: Bearer` 传入，作用于 `/v1/*`。默认监听 `127.0.0.1` 时可不设。
* **Header 白名单**：向上游只透传 `Content-Type`、`Accept`；客户端 `Authorization` 绝不透传，由 Adapter 注入 Provider Key；不透传 `Accept-Encoding`，让 Go Transport 做透明 gzip。
* **`/v1/models`**：返回 Virtual Model 列表，OpenAI list 格式（`{"object":"list","data":[{"id":...,"object":"model","owned_by":"vmr"}]}`）。成本极低，但很多客户端（各类 ChatUI、IDE 插件）启动时必查，MVP 必须有。
* **超时**：连接超时 10s；首字节（响应头）超时默认 120s（LLM 排队可能很慢，需可配）；流式 idle 超时 120s；不设整请求硬上限（长流式由客户端控制）。以上均为单次尝试粒度。
* **热加载**：fsnotify 监听 + SIGHUP 兜底；新配置先完整校验，失败则保留旧配置并打日志——绝不带病上线。运行中的请求继续用旧路由表（原子指针交换）。
* **日志**：stderr，每请求一行：virtual model、命中的 endpoint、尝试次数、时延、状态。无持久化。
* **可观测**：`GET /admin/status` 返回各 Endpoint 健康状态 JSON（仅接受 loopback 来源连接），CLI `vmr status` 读它渲染。

---

## 4. 配置示例（完整可用形态）

```yaml
listen: 127.0.0.1:8800
# api_key: sk-vmr-local-xxx        # 可选：保护 Router 自身

providers:
  minimax:
    type: openai                    # 使用哪个 Adapter（MiniMax 提供 OpenAI 兼容接口，透传即可）
    base_url: https://api.minimaxi.com/v1
    api_key: ${MINIMAX_PLAN_KEY}    # 支持环境变量展开
  openrouter:
    type: openai
    base_url: https://openrouter.ai/api/v1
    api_key: ${OPENROUTER_API_KEY}
  deepseek:
    type: openai
    base_url: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}

models:
  coding:
    strategy: [priority]            # MVP 唯一维度；Phase 2 可写 [priority, weight]
    endpoints:
      - provider: minimax
        model: MiniMax-M2
        priority: 1
      - provider: openrouter
        model: anthropic/claude-sonnet-4.5
        priority: 2
      - provider: deepseek
        model: deepseek-chat
        priority: 3

  cheap:
    endpoints:                      # strategy 缺省即 [priority]
      - provider: deepseek
        model: deepseek-chat
        priority: 1
      - provider: openrouter
        model: deepseek/deepseek-chat-v3.1
        priority: 2
```

配置即产品。这份 YAML 就是用户对系统的全部认知模型：providers 定义"我有什么"，models 定义"对外叫什么、按什么顺序用"。

---

## 5. 对成熟项目的借鉴

**one-api / new-api（Go）**——借它的 **Adapter 接口切分**。它验证了"每个 Provider 一个包、实现统一 adaptor 接口（构造 URL、转换请求、转换响应）"在 Go 生态里可以支撑数十个 Provider 而保持整洁。不借它的整体：它绑定 SQL 数据库、Web 控制台、用户/令牌/计费体系，是多租户中转站，与本项目"单机开发工具"定位相反。

**LiteLLM（Python）**——借它的 **Router 语义**：cooldown（失败冷却）、fallbacks（跨模型降级链）、尊重 Retry-After、按失败类型区分处理。这套语义经过海量生产验证，V2 的健康机制就是它的极简重述。不借它的实现：Python 运行时 + 重量级依赖违背单二进制目标，且它为兼容上百 Provider 自建的参数翻译层正是我们靠"OpenAI 格式即 Canonical"绕开的复杂度。

**结论：没有可以整体复用的现成项目**（凡是功能覆盖的都带着 DB/UI/计费走向平台化），从零实现是正确选择，且成本可控（≤2500 行）。第三方依赖只取两个微型库：`gopkg.in/yaml.v3`（配置解析）和 `fsnotify`（热加载），其余全部标准库——不用 Web 框架（`net/http` 足够）、不用 CLI 框架（`flag` 足够）、不用任何 Provider SDK（见 0-②）。

---

## 6. 分阶段实现

### Phase 1 — MVP（目标：一周内可用，≤2500 行）

范围：

* 仅 `POST /v1/chat/completions`（流式 + 非流式）与 `GET /v1/models`，其余 404
* 仅 `openai` 一个 Adapter（透传型：改 URL / Key / model 字段，body 零转换）
* 仅 `priority` 一个排序维度 + failover 循环
* 被动健康：冷却、指数退避、半开恢复、Retry-After
* 配置：YAML + `${ENV}` 展开 + 校验 + 热加载
* CLI：`vmr start -c config.yaml`、`vmr check -c config.yaml`（校验配置）、`vmr status`
* `/admin/status` 健康状态端点（仅本机）

验收标准（全部满足才算 MVP 完成）：

1. 单二进制 + 一份 YAML，一条命令启动；任意 OpenAI 客户端把 Base URL 指过来即可用 `coding`。
2. 把第一优先 Endpoint 的 Key 改错或断网，请求自动落到第二优先，客户端无感知。
3. 上游返回 429 后该 Endpoint 进入冷却（日志可见），冷却到期自动恢复。
4. 流式响应逐块转发，本机代理开销对 TTFB 的影响 < 5ms。
5. 修改配置文件数秒内生效；写坏配置不影响运行中的实例。

### Phase 2 — 重点扩张（MVP 跑通后）

按需排序，每一项都不改动 router 主流程：

* **Adapter**：~~`anthropic` 翻译型~~（已按 V2.2 原则作废并以透传型实现，见 0.6.1）；后续按需加 `gemini` 等新**协议入口**（同样透传，不翻译）。
* **排序维度**：`weight`（加权随机）、`round_robin`、`latency`（滑动窗口实测时延）、`cost`（按配置单价）。
* **Endpoint 限流**：每 Endpoint 可选 rpm / 并发上限（内存令牌桶），主动避免打出 429。
* **直连语法**：`model: "openrouter:gpt-5"` 绕过 Virtual Model 直达指定 Provider，便于调试。
* **模型改写**：Endpoint 级 alias/参数覆盖（如强制 `temperature`、注入 OpenRouter 的 `provider` 路由参数）。
* **可观测增强**：`/metrics`（Prometheus 文本格式，无依赖手写）、`vmr test <virtual-model>`（对每个候选发一次最小请求并报告）。
* **发布**：goreleaser + Homebrew tap。

### Phase 3 — 远期设想（只列方向，不展开）

* 能力/标签路由：按上下文长度、vision、tool-use 等能力自动过滤候选。
* 本地用量与成本统计（可选嵌入 SQLite，仍是单二进制）。
* 更多端点：embeddings、images、audio 的同构路由。
* 会话粘性（sticky dimension）与语义缓存。
* 小团队共享部署形态（多 Key 分发、简单 ACL）——是否做取决于届时是否偏离"开发工具"定位。

---

## 7. 明确不做（继承 V1，永久有效）

Dashboard、数据库（Phase 3 的可选统计除外）、用户管理、计费、Prompt 管理、Workflow、运行时插件系统、MCP 框架、企业级 AI Gateway、**跨协议转换**（OpenAI ↔ Anthropic ↔ Gemini 的任何方向翻译——协议内透传是本项目的边界，见 0.6.1）。每个新需求先过一道门：**它增加的是能力，还是复杂度？**
