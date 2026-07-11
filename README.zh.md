<!-- Ver 2026-07-12 03:10, by Fable 5 -->
<!-- keywords: LLM 路由器, LLM 网关, OpenAI 兼容代理, Anthropic API 代理, 故障切换, 模型路由, 负载均衡, 本地部署, 单二进制, MiniMax, DeepSeek, OpenRouter, Claude Code, LiteLLM 替代 -->

# vmr — Virtual Model Router

**一个模型名，背后所有供应商。自动故障切换，字节级透传。**

[English](README.md) | 简体中文

`vmr` 是本地运行、单二进制的 LLM 路由器。客户端只连一个稳定的虚拟模型名（`coding` / `claude` / `agent`）——供应商、账号、API Key、优先级、故障切换全部藏在它背后。凌晨三点某家限流、欠费或宕机时，你的 agent 继续跑，你从日志里知道这件事，而不是从一个死掉的会话里。

## 为什么用 vmr

- **一个名字，所有供应商** —— Claude Code、OpenClaw 或任何 OpenAI/Anthropic SDK 只需指向 vmr 一次；上游（MiniMax、DeepSeek、OpenRouter 及任何协议兼容厂商）的增删与排序只是改一个 YAML，数秒热生效，客户端永远不用动。
- **真正会切换的故障切换** —— 被动健康追踪，按错误类别区分冷却（限流 ≠ 坏 Key ≠ 内容拦截），指数退避，尊重 `Retry-After`，恢复用单飞探针防惊群。内容合规拒绝会切换供应商，但**不惩罚**健康端点。
- **字节级透传** —— 没有中间表示，永不做协议翻译。请求与响应和直连供应商字节一致（含响应头），仅有的例外是虚拟名↔真实名的 model 字段改写和几个有触发守卫、有实证依据的厂商怪癖修复。未知 API 参数原样通过——上游新功能上线当天即可用。
- **真流式** —— SSE 事件到达即转发。归一化器只在检测到厂商"思考内联"病理形态时才缓冲，且 `</think>` 闭合后立即恢复实时流。
- **双协议一体** —— 原生 `POST /v1/chat/completions`（OpenAI）与 `POST /v1/messages`（Anthropic）两个入口，各自严格在本协议族内路由。不做有损的跨协议翻译——这是特性，不是缺口。
- **飞行记录仪式审计日志** —— 每个请求一行 JSONL，双层完整记录（调用方↔vmr、vmr↔上游）、每次 failover 尝试、错误类别、实际生效的归一化清单。`vmr report` 把日志变成用量/延迟/可用度统计（含缓存命中细分），而且读得懂 Agent 流量：请求自动分组为 会话 → 任务 → 轮次，每份详单只展开当轮增量，按请求形态的工具使用报告直接告诉你哪些声明的工具从未被调用。过期的日志文件自动压缩为 `.zst`（实测缩小 20~75 倍，`vmr report` 透明读取），也可用 `audit_retention_days` 设置自动过期。
- **视觉 token 减负（可选）** —— 入口处压缩超大内联图片附件；全局开关 + 逐虚拟模型覆盖，默认关闭，fail-open；降采样结果按内容哈希落盘缓存，避免重复处理并保住上游 prompt cache。
- **Unix 风格工具** —— 单二进制、零数据库、零 Web UI、零运行时插件。坏配置拒绝启动（热加载同样拒绝）。依赖只有 `yaml.v3`、`fsnotify`、`golang.org/x/image`、`klauspost/compress`（zstd，审计日志压缩），就这些。

```
OpenAI 客户端    ──(/v1/chat/completions)──┐         ┌─> MiniMax / DeepSeek / OpenRouter (OpenAI 面)
                                           ├── vmr ──┤
Anthropic 客户端 ──(/v1/messages)──────────┘         └─> MiniMax / DeepSeek / OpenRouter (Anthropic 面)
                                                         失败/冷却 → 按序切换下一端点
```

## 快速开始

```bash
go build -o vmr ./cmd/vmr

cp config.example.yaml config.yaml   # api_key 支持 ${ENV} 展开
./vmr check -c config.yaml           # 校验配置并打印路由表
./vmr start -c config.yaml           # 前台运行；启动时打印完整配置摘要

# dev 模式 —— 快速后台运行，你自己监督（先校验再拉起，日志落 logs/）：
./vmr.sh start          # 另有 stop / restart / status / logs

# service 模式 —— 交给操作系统 init 系统监督：崩溃自动重启 + 登录自启。
# macOS → launchd user agent；Linux → systemd 用户单元。install 会渲染并注册
# 服务描述文件，同时从当前 shell 抓取 config.yaml 引用的全部 ${VAR} 与代理变量
# 生成 ~/.config/vmr/env（0600）——init 系统的环境是干净的，否则 key 全为空。
./vmr.sh service install     # 注册并启动（幂等，改路径/配置后重跑即更新）
./vmr.sh service status      # 另有 start / stop / restart / logs
./vmr.sh service uninstall   # 停止并注销
# Linux：若需注销登录后服务仍运行，执行一次 loginctl enable-linger $USER
```

两种模式同一时间只用一种——`service install`/`start` 会自动停掉 dev 模式进程。macOS 下 service 模式的服务日志在 `~/Library/Logs/vmr.log`（TCC 不允许 launchd 对外置卷做文件操作）；审计日志照常跟随 `VMR_LOG_DIR`。

把客户端的 Base URL 指向 vmr 即可：

```bash
# OpenAI 协议客户端（OPENAI_BASE_URL=http://127.0.0.1:8800/v1）
# 如果 config.yaml 里设置了 vmr 自己的 api_key，加上 -H "Authorization: Bearer <api_key>"
curl http://127.0.0.1:8800/v1/chat/completions -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic 协议客户端（如 Claude Code：ANTHROPIC_BASE_URL=http://127.0.0.1:8800）
# Anthropic 客户端发送的是 x-api-key 而非 Authorization——vmr 两种都认
curl http://127.0.0.1:8800/v1/messages -H "Content-Type: application/json" \
  -H "x-api-key: sk-vmr-local-xxx" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'

# 列出虚拟模型（认证规则同上，用的是 vmr 自己的 api_key）
curl http://127.0.0.1:8800/v1/models -H "Authorization: Bearer sk-vmr-local-xxx"

# 各 endpoint 的健康状态 + 并发情况（仅限本机回环访问；即使配了 api_key 也不需要）
curl http://127.0.0.1:8800/admin/status
```

## 配置

`providers`/`models` 都按协议分两层：外层 key 是协议（`openai` / `anthropic`），内层才是名字。一个 model 的 endpoints 只能引用同协议分组下的 provider——跨协议混用没有语法能表达它，而不是靠校验去抓。同一账号的两个协议面可复用同一个短名（`openrouter`），不需要后缀区分：

```yaml
listen: 127.0.0.1:8800
# api_key: sk-vmr-xxx          # 可选：保护 vmr（Bearer 或 x-api-key）
# max_attempts: 0              # 每请求上游尝试数上限（缺省 0 = 试遍全部候选）
# max_body_mb: 8               # 请求体缓冲上限；同时决定审计 body 记录上限
# max_concurrency: 8           # 全局并发上限，超限请求挂起等待（缺省不限）
# image_downscale: 512         # 请求内联图片长边像素上限，缺省关闭（可被虚拟模型自身设置覆盖，见下文）
# image_cache_ttl_days: 7      # 降采样结果缓存的失效期（缺省 7 天）
# audit_retention_days: 30     # 超过此天数的审计文件自动删除（缺省永久保留）
# timeouts:
#   connect: 10s               # 连接上游
#   response_header: 120s      # 上游首字节
#   stream_idle: 120s          # 上游 body 静默看门狗（流式/非流式/错误体都覆盖）

providers:
  openai:
    openrouter:
      base_url: https://openrouter.ai/api/v1
      api_key: ${OPENROUTER_API_KEY}
  anthropic:
    openrouter:                # 同一账号的 Anthropic 面，同名不冲突（两层 map 天然隔离）
      base_url: https://openrouter.ai/api/v1
      api_key: ${OPENROUTER_API_KEY}

models:
  openai:
    coding:
      endpoints:
        - {provider: openrouter, model: z-ai/glm-5.2}   # 不写 priority：列表顺序就是尝试顺序
  anthropic:
    claude:                    # anthropic 协议 → 走 /v1/messages
      endpoints:
        - {provider: openrouter, model: minimax/minimax-m3}
```

全部字段与校验规则见设计文档 §10。修改配置数秒内热生效；坏配置被拒绝、不影响运行实例。

## 透传与归一化

**原则：直连等价**。客户端经 vmr 收到的内容——字节、头部、传输节奏——与直连供应商一致。仅有的偏离：

- `model` 字段——请求侧改成上游真实名，响应侧改回虚拟名（SDK 假设 `response.model === request.model`）；
- 两个 **MiniMax-M3 专属修复**，各自只在确认命中其确切形态时触发：剥 content 里的内联 `<think>…</think>` 推理（不剥会持久化进历史，把模型锁进反馈循环），以及剥 `thinking=medium` 下的纯文本「Thinking Process:」思考段；
- `data: [DONE]` 哨兵——**仅** OpenAI 协议流式且上游未发时补；绝不重复，绝不注入 Anthropic 流。

流式是真的：事件到达即转发；只有检测到思考形态才缓冲，`</think>` 闭合后立即恢复实时流。带 `Content-Encoding` 的压缩体零变换直通。响应头与请求头同一策略——除 hop-by-hop 外全部透传；错误响应连状态、头（含 `Retry-After`）、体原样返回。每个请求实际生效的归一化记录在审计日志 `attempts[].norm`，上游与客户端之间的任何字节差异都有逐请求的解释。

## 故障切换与健康

上游失败即按端点列表顺序逐个尝试，直到成功或全部耗尽（`max_attempts` 可选设上限）。健康完全由失败驱动——不做花钱的主动探测：

- 网络/5xx：短冷却指数退避；401/额度耗尽/模型不存在：长冷却；429/503：尊重 `Retry-After`；
- 冷却到期只放行**一个探针请求**验证恢复（防惊群）；
- 400 类客户端错误直接返回——不做无意义的重试；
- **内容合规拦截**切换下一家，但**不惩罚**被拦端点——它只是拒绝了这一条请求，并没有坏。

全部候选失败时原样返回最后一次上游错误。流式只在首字节前切换。

## 审计日志与用量报表

默认开启：每个请求一行 JSONL，双层记录（调用方↔vmr 与每次 vmr↔上游尝试）、凭证掩码、错误类别、生效的归一化清单。body 记录上限联动 `max_body_mb`——vmr 接受的请求绝不会在自己的审计里被截断。

```bash
./vmr start -c config.yaml                 # 写入 $VMR_LOG_DIR，未设置则用 `vmr dirs log` 的解析结果
./vmr start -c config.yaml -audit=false    # 关闭
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl

./vmr report "$(./vmr dirs log)/vmr-audit-*.jsonl*"   # → vmr-report.json + vmr-report.md + vmr-requests.jsonl（明文/.zst 混着传也行）
```

`vmr report` 同时统计 tokens 与字节（上游不回报 usage 时以字节兜底）：按 日期×协议×模型 的行、按端点的可用度与错误分布、延迟与 TTFT 分位、吞吐，以及按模型的健康信号——finish_reason 分布（`length` = 输出被 token 上限截断）和"ok 但截断"计数（2xx 背后流中途断掉的请求）。Token 统计还拆出缓存读取、（Anthropic）缓存写入与 reasoning tokens。另有每小时活跃度（`hours[]`）和工作负载切分（`workloads[]`：交互工作 vs heartbeat/日记 cron 这类定时脚手架），一眼看清请求和账单到底来自哪里。JSON 是二次开发（图表/Dashboard）的数据源。

`vmr report` 还能读懂 Agent 工作负载——全部离线、纯规则、不调用 LLM（方法与实证见 `docs/AgentSessionGrouping_Analysis_Fable5.md`）：

- **会话 → 任务 → 轮次分组**。每轮重发同一段渐增对话的请求以首条非 system 消息做指纹（Claude Code 的 `metadata.user_id` 存在时优先），按最长公共前缀成链——多个 Agent 会话即使在时间上互相穿插也能干净分开。任务边界来自 Traceparent trace-id 变化与增量中的新用户指令，两个信号互为交叉验证。Compaction 调用被识别并双向链接，会话与其压缩后的续接体串成同一条线程。
- **`vmr-requests.jsonl`** —— 每请求一行特征（会话/任务/轮次、trace 与 chat id、请求形态、`heartbeat` 等标签、当轮 tool 调用、finish_reason、"ok 但截断"标志、含 reasoning 的 token 细分、增量大小、最新指令），jq / DuckDB / pandas 直接可用。
- **工具使用报告** —— 按请求形态列出：声明的工具 vs **当轮实际调用**的工具（从响应中提取,历史重发绝不重复计数），外加"声明但从未调用"清单及其每请求字节成本——为从 Agent 配置里裁掉没用的工具提供直接依据。

`vmr report` 还会把每条记录导出为一个人类可读的 Markdown 详单，落在 `{out}/details/` 下（附 `INDEX.md` 索引，含会话分组视图与全量时间序表两种入口），用于深挖单个请求：头部一行定位（会话 · 任务 · 轮次 · 上一轮链接 · 本轮 tool 调用），**「本轮增量」区**只展示这一轮新增的内容——最新指令高亮、被替换的临时尾部消息以删除线列出——之后才是完整消息列表与工具 schema（长内容默认折叠、图片以尺寸占位）、每次上游尝试的 headers 与 body 字段全量对照（变化项以 emoji 标记：🟢 新增 / 🔴 删除 / 🔶 变化，未变化的照常列出不突出）、客户端响应部分把 SSE 流重组成模型实际输出并保留原始事件全文。文件名以零填充时间戳开头，按名字排序即按时间排序。加 `-details=false` 可关闭。

Agent 场景下每一轮都会把完整对话历史重新发一遍，单日日志动辄几个 GB——而且这种冗余主要出现在**行与行之间**，不是单行内部。每天的日志文件一旦不再是"今天"就自动轮转压缩：用 zstd 压缩整个文件（而不是逐行压缩）才能吃到跨行的重复内容，实测压缩比 20~75 倍——这是逐条记录单独压缩根本达不到的量级，因为单条记录看不到上一轮几乎重复的请求体。`vmr report` 对 `.jsonl` 和 `.jsonl.zst` 一视同仁，通配符同时覆盖两者即可。设置 `audit_retention_days` 还能让过期文件自动删除（缺省永久保留，不设置不会删任何东西）；压缩和清理都只看文件名里的日期，不需要扫描或逐个 `stat` 整个日志目录。背后的实测数据和方案取舍见 `docs/AuditLogCompression_Analysis_Sonnet5.md`。

## 请求图片自动降采样

可选，默认关闭。开启后，超过设定长边的内联 base64 图片附件会被等比缩小、转 JPEG 再发上游——为截图密集的 agent 工作流削减 vision token 成本。只处理请求，不碰响应，不抓远程 URL；动图与解码失败一律原样透传（fail-open）。

```yaml
image_downscale: 512   # 全局长边像素上限；0 或缺省 = 关闭
image_cache_ttl_days: 7   # 降采样结果缓存的失效期（缺省 7 天，见下）

models:
  openai:
    coding:
      image_downscale: 1024   # 覆盖全局值，只对这一个虚拟模型生效
      endpoints: [...]
    cheap:
      image_downscale: 0      # 显式关闭：即使全局开启，这个模型也不降采样
      endpoints: [...]
```

**模型级覆盖**：每个 virtual model 都可以设置自己的 `image_downscale`，优先级高于全局值；不写则继承全局设置。`image_downscale: 0` 在模型层面是一个明确的"关闭"指令，即使全局开着也照样关——因为"没写"和"写了 0"含义不同（前者继承，后者强制关闭）。

**降采样结果缓存**：同一张原始图片、同一个目标像素上限，第一次处理后会把结果（JPEG 字节）按内容哈希缓存到磁盘（`$VMR_IMG_CACHE_DIR` 有设置就用它，否则用 `vmr dirs cache` 的解析结果）。后续请求命中同一张图片时直接复用缓存字节，不再重新解码/缩放/编码。这带来两个好处：省 CPU（agent 场景每轮都会把完整对话历史连同图片重发一遍），以及避免破坏上游的 prompt cache——上游的缓存是按精确字节/token 匹配的，同一张图片如果每次都重新编码，输出字节可能有细微差异，足以让上游缓存失效；用缓存后的完全相同字节，上游缓存才能命中。缓存条目按"最近一次被命中"的时间做 TTL 淘汰（`image_cache_ttl_days`，缺省 7 天；命中会刷新计时，长对话里反复引用的图片不会被提前清掉），淘汰扫描搭在缓存目录访问上触发，不额外起定时器。

**审计目录和缓存目录到底落在哪**：两者默认规则统一——`$VMR_LOG_DIR`/`$VMR_IMG_CACHE_DIR` 有设置就原样使用，否则落在系统临时目录下的 `vmr_logs`/`vmr_image_cache` 子目录，再否则（只有系统连临时目录都给不出来这种极端情况）落在二进制所在目录的 `./logs`/`./image_cache`。想知道实际解析出来的路径，直接跑 `vmr dirs log` / `vmr dirs cache`，不用真的启动服务。`vmr.sh` 用的是同一套规则——它自己也是调用 `vmr dirs` 去问，而不是在 bash 里另写一份猜测逻辑——dev 模式和 `service install` 因此不会对"数据到底存在哪"这件事产生分歧。

## 端点与 CLI

| 端点/命令 | 作用 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI 协议入口（流式 + 非流式） |
| `POST /v1/messages` | Anthropic 协议入口（流式 + 非流式） |
| `GET /v1/models` | Virtual Model 列表（两种 SDK 均可解析） |
| `GET /admin/status` | 端点健康 + 并发指标（仅 loopback） |
| `vmr check -c config.yaml` | 校验配置、打印路由表与 Key 状态 |
| `vmr status -c config.yaml` | 渲染运行实例的健康与并发占用 |
| `vmr report [-o dir] <glob>` | 审计日志（明文或 `.zst`）→ 用量统计 + 会话/工具分析 + 逐请求特征（`vmr-requests.jsonl`）+ 详单（`-details=false` 关闭） |
| `vmr dirs log\|cache` | 打印默认审计/缓存目录的解析结果（`vmr.sh` 内部就是问这个） |
| `./vmr.sh start\|stop\|…` | dev 模式生命周期（自己监督） |
| `./vmr.sh service install\|uninstall\|start\|…` | init 系统服务（launchd/systemd：崩溃重启、登录自启） |

经路由的响应带 `X-VMR-Endpoint`（实际命中端点）与 `X-VMR-Attempts`（尝试次数）。

## 开发

```bash
go test -race ./...
```

新增 Provider：OpenAI/Anthropic 兼容的厂商只是一条配置，零代码。新协议 = `internal/adapter/<name>/` 实现三方法接口 + `cmd/vmr/main.go` 一行 blank import。

架构与全部设计决策（含每条背后的事故账本）：[设计文档](docs/VirtualModelRouter_v2_Fable5.md)。

## 开源协议

[MIT](LICENSE)
