<!-- Ver 2026-07-07 15:00, by Fable 5 -->

# Virtual Model Router (vmr) — 设计方案

本文档描述 vmr 的当前完整设计：定位、架构、机制与关键决策。读完即可维护与二次开发本项目。使用文档见 `README.md`，进度记录见 `DEV_PLAN.md`。

---

## 1. 定位

本地运行、单二进制、配置驱动的 LLM 路由器。客户端只连稳定的 Virtual Model 名（如 `coding` / `cheap` / `claude`），Provider、账号、Key、优先级、故障切换全部由 vmr 隐藏。Unix 风格工具：零数据库、零 Web UI、零运行时插件。

**边界**（永久有效的"不做"清单）：Dashboard、用户管理、计费、Prompt 管理、Workflow、运行时插件系统、MCP 框架、企业级 AI Gateway、**跨协议转换**（见 §3）。每个新需求先过一道门：它增加的是能力，还是复杂度？

---

## 2. 核心概念

| 概念 | 职责 |
| --- | --- |
| **Virtual Model** | 对外暴露的模型名，代表能力而非厂商；对应一组 Endpoint，绑定一种协议 |
| **Provider** | 一个可复用的上游定义：type（用哪个 Adapter）+ base_url + api_key |
| **Endpoint** | 最小调度单位：Provider × 实际模型名 × 调度属性；同厂不同 Key / 不同协议面即不同 Provider→不同 Endpoint |
| **Adapter** | 协议插件：构造上游请求、转换响应、归类错误；声明自己的协议 |
| **Strategy** | 候选排序器：健康过滤后按维度序列做稳定多键排序 |

Health 与并发闸不是独立概念，而是运行时状态（§6、§7）。

---

## 3. 协议模型：多入口，永不翻译

vmr 同时暴露两个聊天入口，**不做任何跨协议转换**：

```
POST /v1/chat/completions   OpenAI 协议   → 只路由到 OpenAI 兼容端点
POST /v1/messages           Anthropic 协议 → 只路由到 Anthropic 兼容端点
```

**决策逻辑**：双向流式翻译（Anthropic 的 `message_start`/`content_block_delta` 事件流 vs OpenAI 的 chunk 流，tool-use / thinking 块的语义映射）是 LiteLLM 复杂度失控的根源之一；而主流厂商（MiniMax、DeepSeek、OpenRouter）已原生提供两种兼容面，翻译不创造价值。协议内透传保证零损耗、对上游新字段前向兼容。

落实机制：

* 协议是 Adapter 的属性（`Protocol() string`）。Virtual Model 的协议由其全部 endpoints 的 Adapter 推断，混协议在配置加载期报错——不设显式配置字段，消灭"声明与实际不符"这类错误而非校验它。
* 模型存在但协议不符 → 404，message 指明正确入口。
* 恰好两种协议的请求体都是顶层 `model` + `stream` 字段，路由解析层（`CanonicalRequest`）天然协议无关。
* vmr 自产的错误体为两种客户端都能解析的合并形态：`{"type":"error","error":{"type","message"}}`（OpenAI SDK 读 `error.message`，Anthropic SDK 认 `type:"error"` 信封）。`GET /v1/models` 同理（`object:"list"` + `has_more` + `type:"model"` 并存）。
* 新增协议入口（如 gemini）= 新 Adapter + 新路由行，同样透传。

已接入的厂商协议面（均实测）：

| Provider 配置 | base_url | 协议 |
| --- | --- | --- |
| minimax | `https://api.minimaxi.com/v1` | openai |
| minimax_a | `https://api.minimaxi.com/anthropic/v1` | anthropic |
| deepseek | `https://api.deepseek.com/v1` | openai |
| deepseek_a | `https://api.deepseek.com/anthropic/v1` | anthropic |
| openrouter | `https://openrouter.ai/api/v1` | openai |
| openrouter_a | `https://openrouter.ai/api/v1`（同一 base，Adapter 拼 `/messages`） | anthropic |

---

## 4. 系统架构

### 4.1 请求流程

```
Client ── POST /v1/chat/completions | /v1/messages
  │
Server     审计记录开始 → 鉴权(可选) → 并发闸 → 缓冲请求体(≤8MB,413)
  │        → 解析 model/stream，其余字节不动
  ▼
Router     查 Virtual Model → 校验协议 → 健康过滤 → 稳定多键排序 → 候选序列
  │
  ▼ failover 循环（每个可用候选各试一次，直到成功或候选耗尽；max_attempts 可选设上限）
Adapter    BuildRequest：改 URL / 注入 Key / 改写 model 字段（其余透传）
  │
  ▼
Upstream   ├─ 2xx → 转发（流式逐块+Flush）→ 上报健康成功 → 审计落盘
           ├─ 4xx/5xx → ClassifyError → ErrClient 直接返回；其余记冷却、试下一个
           └─ 网络错误 → 短冷却，试下一个
```

硬规则：

* **请求体一律入口缓冲**（流式也是）：failover 重放的前提。
* **流式只在首字节发出前允许 failover**；实现上该约束自然成立——仅上游 2xx 后才开始向客户端写，此前的一切失败都发生在写出之前。首字节后的上游错误只能断流并记日志。
* **失败语义**：有真实上游尝试 → 原样返回最后一次上游错误（status+body，保留客户端可解析的厂商错误结构）；无候选可试 → 503。所有响应带 `X-VMR-Endpoint` / `X-VMR-Attempts`。
* **Header 白名单**：向上游只透传 `Content-Type` 及协议头（`anthropic-version`、`anthropic-beta`）；客户端 `Authorization`/`x-api-key` 绝不透传，凭证由 Adapter 注入；不透传 `Accept-Encoding`（Go Transport 透明 gzip）。

### 4.2 模块划分

```
cmd/vmr/main.go            CLI（stdlib flag）：start / check / status；Adapter 的 blank import 注册点
internal/core              CanonicalRequest、ErrorClass、Endpoint（无依赖的共享类型）
internal/config            YAML 加载、${ENV} 展开、校验、热加载 watch
internal/adapter           Adapter 接口 + 注册表 + 共享错误分类表/model 改写
internal/adapter/openai    OpenAI 协议透传 Adapter
internal/adapter/anthropic Anthropic 协议透传 Adapter
internal/health            被动健康状态机（冷却、退避、半开探针）
internal/strategy          Dimension 接口 + priority 维度 + 稳定多键排序
internal/router            快照构建 + failover 循环 + 流转发 + 并发闸（核心）
internal/server            HTTP 入口、鉴权、审计录制、四个端点
internal/audit             审计日志（JSONL 落盘）
```

约 1950 行（不含测试约 1300 行）。依赖仅 `gopkg.in/yaml.v3` 与 `fsnotify`，其余标准库——不用 Web/CLI 框架、不用任何 Provider SDK（透传路由只需"改 URL、注 Key、改 model 字段"，SDK 只带来二进制膨胀与版本纠缠）。

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

接口四个方法；注册用 `database/sql` 驱动模式（编译期注册，非运行时插件）：

```go
type Adapter interface {
    Protocol() string          // "openai" | "anthropic"：该 Adapter 服务的入口协议
    BuildRequest(ctx, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, error)
    TransformBody(body io.ReadCloser, stream bool) io.ReadCloser   // 透传型恒等返回
    ClassifyError(status int, body []byte) core.ErrorClass
}
// 新增 Provider 协议 = internal/adapter/<name>/ 一个包 + main.go 一行 blank import
```

`CanonicalRequest{Model, Stream, Raw, Header}`：只解析路由所需字段，`Raw` 保留原始字节（前向兼容）；`Header` 是服务端白名单后的协议头子集。model 改写：`map[string]json.RawMessage` 局部替换 `model` 键，未知字段字节原样（不保证键序，无语义影响）。

### 错误分类（决定 failover 质量的关键）

```go
ErrClient     请求本身有问题 → 直接返回客户端，不切换
ErrAuth       401/403 → 长冷却（10min 起），切换
ErrRateLimit  429 → 尊重 Retry-After（秒/HTTP-date），切换
ErrEndpoint   端点持续不可用（额度耗尽/402、模型不存在/404 或 400+嗅探）→ 长冷却，切换
ErrTransient  5xx/408/529/超时/网络 → 短冷却（2s 指数退避），切换
```

分类表两 Adapter 共享（`adapter.DefaultClassify`），差异点各自覆盖（如 anthropic 的 529）。**必须做 body 嗅探**，因为实测各家错误习惯不一：MiniMax 未知模型返回 400（非 404）；DeepSeek Anthropic 口的措辞是 "The supported API model names are …"；OpenRouter 余额不足返回 402；有厂商额度耗尽也发 429。嗅探词表：`model` × {unknown, not found, not_found, does not exist, invalid model, supported}；429 body × {insufficient, quota, balance, credit} → ErrEndpoint。误判的代价只是一次无害切换，漏判的代价是永不 failover——宁可宽。

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

* 失败按类别计冷却：Transient 2s 起指数退避（×2 封顶 5min）；Auth/Endpoint 10min 起（封顶 1h）；RateLimit 优先 `Retry-After`。
* 冷却中被健康过滤剔除；到期进入半开：**只放行一个真实请求当探针**（避免惊群），成功清零、失败退避加深。
* 客户端主动断连不计入端点失败（与上游健康无关，防状态污染）。
* 健康注册表以 `provider/model/key指纹` 为稳定键、独立于配置快照存活——**热重载不清零冷却**（否则每次改配置都会把 429 中的端点放出来重打）。重启即重置，不持久化。

### 6.3 热加载

fsnotify（监听目录，兼容编辑器原子替换，300ms 防抖）+ SIGHUP 兜底。新配置完整校验，失败保留旧配置并打日志——绝不带病上线。路由表（含 http.Client）随快照原子指针交换，运行中请求持有旧快照直至完成。

---

## 7. 并发闸

可选全局上限 `max_concurrency`（缺省 0 = 不限）：

* 两个聊天入口共用一个信号量；超限请求**在内存中挂起等待**（channel 阻塞，近似 FIFO），不排队列、不设等待超时（客户端自有超时，服务端再加一层是重复机制）。
* 等待期间客户端断开 → 立即出队。
* 只闸聊天入口；`/v1/models`、`/admin/status` 不受限。
* 热重载仅当容量变化才换信号量；换闸瞬间新旧持有者叠加、短暂超额（秒级边界行为，可接受）。
* `/admin/status` 暴露 `limit` / `in_flight` / `waiting`。

每 Endpoint 的 rpm/并发精细限流是另一个问题（服务于主动避免 429），在路线图中。

---

## 8. 审计日志

**目标**：原始、完整、可追溯地记录每一个聊天请求的两层往返（调用方↔vmr、vmr↔上游），只记录不分析；请求数、Token 用量等统计由**外部脚本**事后读取 JSONL 完成——本节格式即该脚本的输入契约。

### 8.1 运行行为

| 项 | 行为 |
| --- | --- |
| 开关 | 默认开启；`vmr start -audit=false` 关闭 |
| 目录 | `$VMR_LOG_DIR`，未设置则系统临时目录；启动日志打印实际路径 |
| 文件 | 每天一个：`vmr-audit-YYYY-MM-DD.jsonl`（本地时区，写入时轮转），权限 0600 |
| 时机 | 请求完成后追加一行（含流式全程），不影响 TTFB |
| 失败 | 写盘失败仅打 stderr 日志，绝不影响请求服务 |
| 覆盖 | 两个聊天入口的所有请求，含被 vmr 拒绝的（401/413/坏 JSON/未知模型/协议不符）；`/v1/models`、`/admin/status` 不记 |

### 8.2 记录结构（JSONL，每行一个 Record）

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
      "endpoint": "minimax_a_badkey/MiniMax-M3",   // provider/实际模型
      "url": "https://api.minimaxi.com/anthropic/v1/messages",
      "dur_ms": 543,
      "request":  { "headers": {...}, "body": {...} },   // 出站请求（model 已改写）
      "response": { "status": 401, "headers": {...}, "body": {...} },
      "error": "auth"                     // 失败原因：错误类别 | "network: …" | "build: …" | "canceled by client"
    },
    {
      "endpoint": "deepseek_a/deepseek-v4-flash",
      "url": "https://api.deepseek.com/anthropic/v1/messages",
      "dur_ms": 320,
      "request":  { "headers": {...}, "body": {...} },
      "response": { "status": 200, "headers": {...} }    // 见下：成功尝试不存 body
    }
  ]
}
```

三条约定（统计脚本必须知道）：

1. **成功尝试的响应 body 不存**：透传恒等，它与 `client.response.body` 字节相同，只在 client 层存一份。失败尝试的错误 body（≤64KB）存在 attempt 内。
2. **body 编码**：合法 JSON 原样嵌入（可直接用 jq 查询，如 `.client.response.body.usage`）；非 JSON（如 SSE 流文本）为字符串。单个 body 超过 1MiB 截断并标记 `body_truncated: true`。流式响应的 usage 通常在末尾 SSE 事件里，脚本需从字符串 body 中解析。
3. **凭证掩码**：`Authorization` / `X-Api-Key` / `Api-Key` / `X-Auth-Token` 的值只保留末 4 字符（`"Bearer ***abcd"`），其余 header 原样。这是对"完整 header"要求的唯一偏离——审计文件常驻磁盘，明文密钥外泄风险大于取证价值。

### 8.3 实现要点

`internal/audit`：Record 类型 + Logger（互斥追加、按日期轮转）。server 层用包装 `ResponseWriter` 的录制器捕获 client 层响应（保留 `Flusher`，流式时延零影响，body 记录上限即 1MiB）；router 层在 failover 循环中逐次填充 attempts。Record 经 `Serve` 参数显式传递（nil = 关闭，零开销）。

---

## 9. 配置参考

```yaml
listen: 127.0.0.1:8800        # 缺省 127.0.0.1:8800
api_key: sk-vmr-xxx           # 可选：vmr 自身鉴权（Bearer 或 x-api-key）
max_attempts: 0               # 上游尝试数上限；缺省 0 = 不限，试遍所有可用候选（正数用于约束尾延迟）
max_body_mb: 8                # 请求体缓冲上限（缺省 8，超限 413）
max_concurrency: 8            # 全局并发上限（缺省 0 = 不限）
timeouts:
  connect: 10s                # 连接上游（缺省 10s）
  response_header: 120s       # 上游首字节（缺省 120s）
  stream_idle: 120s           # 流静默看门狗（缺省 120s）

providers:                    # "我有什么"
  <name>:
    type: openai | anthropic  # Adapter（即协议）
    base_url: https://...     # openai 型拼 /chat/completions；anthropic 型拼 /messages
    api_key: ${ENV_VAR}       # 支持 ${VAR} 展开；未设置的变量展开为空串

models:                       # "对外叫什么、按什么顺序用"
  <virtual-model-name>:
    strategy: [priority]      # 缺省 [priority]
    endpoints:
      - provider: <name>      # 必须引用已定义 provider
        model: <上游真实模型名>
        priority: 1           # 数字小优先；缺省 0；平手按文件顺序
```

校验规则：listen 可解析、providers/models 非空、provider 引用存在、adapter type 已注册、base_url 合法、endpoint.model 非空、同一 model 的 endpoints 协议一致。CLI：`vmr start -c <cfg> [-audit=false]`、`vmr check -c <cfg>`（校验+打印路由表）、`vmr status [-c <cfg>]`（渲染健康与并发）。

---

## 10. 关键决策与取舍

| 决策 | 备选 | 取舍逻辑 |
| --- | --- | --- |
| Canonical 格式 = 协议原生格式，透传不翻译 | 自研中间表示（IR） | IR 意味着永远追各家新字段，是 LiteLLM 复杂度失控的根源；透传对新参数天然前向兼容 |
| 不用 Provider SDK | 官方 SDK | 路由只需改 URL/Key/model 字段；SDK 带来二进制膨胀与版本纠缠 |
| 编译期 Adapter 注册（blank import） | 运行时插件（.so/脚本/外部进程） | 满足"插件式扩展、写法统一"，不引入运行时插件的任何成本 |
| 调度 = 过滤+多键排序 | 策略类枚举（Priority/RR/Weighted 各一套） | 组合能力来自排序键叠加；新策略不改主流程 |
| 被动健康 + 半开单飞探针 | 定期主动探测 | 探测 LLM API 每次都花钱；单飞探针把恢复试错成本压到一个请求并防惊群 |
| 错误分类含 body 嗅探 | 严格按 HTTP status 映射 | 实测各家 status 习惯不一（400 当 404 用等）；漏判 = 永不 failover，误判只是一次无害切换 |
| 协议归属自动推断 | models 显式 protocol 字段 | 显式字段要么冗余要么矛盾；推断消灭这类错误本身 |
| 全败透传最后上游错误 | 合成统一 502 | 保留客户端 SDK 可解析的厂商错误结构；聚合信息在日志里 |
| 健康状态跨热重载保留 | 重载清零 | 清零会把冷却中的端点放出来重打；carry-over 仅十几行 |
| 并发闸：全局、无等待上限 | 每端点限流 / 排队超时 | 全局闸覆盖"保护本机与总用量"诉求且实现极简；客户端自有超时 |
| failover 默认穷尽全部候选 | 固定尝试上限（旧默认 3） | 配了兜底端点就该兜到底，固定上限会让后位端点永远轮不到；尾延迟由可选 max_attempts 与各超时约束 |
| 审计双层结构、成功 body 去重 | 每层完整存两份 | 透传恒等，重复存储只膨胀文件；失败 body 各自保留因为各不相同 |
| 审计凭证掩码（留末 4 位） | 完整记录 header | 密钥落盘外泄风险 > 取证价值；末 4 位足以区分 Key |
| 无中心 IR、router 只做循环 | —— | router 包（含并发闸与流转发）约 500 行；若显著变大，说明抽象错了 |

---

## 11. 路线图

* **排序维度**：`weight`（加权随机）、`round_robin`、`latency`（滑动窗口实测）、`cost`（按配置单价）。
* **Endpoint 级限流**：每端点 rpm/并发（内存令牌桶），主动避免 429。
* **直连语法**：`model: "openrouter:gpt-5"` 绕过 Virtual Model 直达指定 Provider（调试用）。
* **模型改写**：Endpoint 级参数覆盖（强制 temperature、注入 OpenRouter `provider` 路由参数等）。
* **审计统计脚本**：读取 §8.2 JSONL 出日/模型/端点维度的请求数、Token、成本报表（独立脚本，不进 vmr 二进制）。
* **可观测**：`/metrics`（Prometheus 文本格式，手写无依赖）、`vmr test <model>`（对每候选发最小请求）。
* **更多协议入口**：gemini；embeddings / images 的同构路由。
* **发布**：goreleaser + Homebrew tap；届时 module 名改为完整仓库路径。
* 远期：能力/标签路由（按 context 长度、vision、tool-use 过滤）、会话粘性维度、本地用量统计（可选嵌 SQLite）。
