<!-- Ver 2026-07-07, by Fable 5 -->

# vmr — Virtual Model Router

本地运行、单二进制、配置驱动的 LLM 路由器。客户端只连稳定的 Virtual Model（如 `coding` / `cheap` / `claude`），Provider、Key、优先级、故障切换全部由 vmr 隐藏。零数据库、零 Web UI，依赖仅 `yaml.v3` + `fsnotify`。

```
OpenAI 客户端 ──(/v1/chat/completions)──> vmr ──> MiniMax / OpenRouter / DeepSeek (OpenAI 兼容口)
Anthropic 客户端 ──(/v1/messages)───────> vmr ──> MiniMax / DeepSeek (Anthropic 兼容口)
                                            └── 失败/冷却自动切换到下一优先级
```

**双协议入口，永不翻译**：OpenAI 协议调用只在 OpenAI 兼容端点间路由，Anthropic 协议调用只在 Anthropic 兼容端点间路由。每个 Virtual Model 归属一种协议（由其 endpoints 自动推断，混协议在配置校验期报错）。

## 快速开始

```bash
go build -o vmr ./cmd/vmr

cp config.example.yaml config.yaml   # 按需修改；api_key 支持 ${ENV} 展开
./vmr check -c config.yaml           # 校验配置并打印路由表
./vmr start -c config.yaml           # 启动（默认 127.0.0.1:8800）
```

任何 OpenAI 或 Anthropic 客户端把 Base URL 指向 `http://127.0.0.1:8800` 即可：

```bash
# OpenAI 协议
curl http://127.0.0.1:8800/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic 协议（如 Claude Code：ANTHROPIC_BASE_URL=http://127.0.0.1:8800）
curl http://127.0.0.1:8800/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'
```

## 配置

```yaml
listen: 127.0.0.1:8800
# api_key: sk-vmr-local-xxx    # 可选：保护 vmr 自身（Bearer 或 x-api-key 均可）
# max_attempts: 3              # 每请求最大尝试端点数
# max_body_mb: 8               # 请求体缓冲上限（failover 重放所需）
# max_concurrency: 8           # 全局并发上限；超限请求在内存中挂起等待（缺省不限）
# timeouts:
#   connect: 10s               # 连接上游
#   response_header: 120s      # 上游首字节
#   stream_idle: 120s          # 流静默看门狗

providers:                     # 我有什么
  deepseek:
    type: openai               # 内置 Adapter：openai / anthropic（均为透传型）
    base_url: https://api.deepseek.com/v1
    api_key: ${DEEPSEEK_API_KEY}
  deepseek_a:                  # 同一厂商的 Anthropic 兼容口是另一个 provider
    type: anthropic
    base_url: https://api.deepseek.com/anthropic/v1
    api_key: ${DEEPSEEK_API_KEY}

models:                        # 对外叫什么、按什么顺序用
  cheap:                       # openai 协议模型（由 endpoints 推断）
    strategy: [priority]       # 缺省即 [priority]；数字小优先，平手按文件顺序
    endpoints:
      - {provider: deepseek, model: deepseek-v4-flash, priority: 1}
  claude:                      # anthropic 协议模型，走 /v1/messages
    endpoints:
      - {provider: deepseek_a, model: deepseek-v4-pro, priority: 1}
```

修改配置文件数秒内自动生效（fsnotify + SIGHUP 兜底）；写坏的配置会被拒绝，运行中的实例不受影响。

## 端点与 CLI

| 端点/命令 | 作用 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI 协议入口（流式 + 非流式） |
| `POST /v1/messages` | Anthropic 协议入口（流式 + 非流式） |
| `GET /v1/models` | Virtual Model 列表（OpenAI/Anthropic 客户端均可解析的合并格式） |
| `GET /admin/status` | 各端点健康状态 + 并发指标 JSON（仅 loopback） |
| `vmr check -c config.yaml` | 校验配置、打印路由表与 Key 状态 |
| `vmr status -c config.yaml` | 渲染运行实例的端点健康（冷却/半开/ok）与并发占用 |

响应带 `X-VMR-Endpoint`（实际命中的端点）与 `X-VMR-Attempts`（尝试次数），便于排查。

## 故障切换与健康机制

* 失败驱动冷却（无主动探测，不烧钱）：网络/5xx 基础 2s 指数退避封顶 5min；401/403、额度耗尽、模型不存在 10min 起；429 尊重 `Retry-After`。
* 冷却到期进入半开：只放行一个真实请求当探针，成功即恢复，失败退避加深。
* 400 类客户端错误不切换，原样返回；全部候选失败时返回最后一次上游错误原文。
* 流式请求只在首字节发出前切换；之后只能断流（代理的物理约束）。
* 健康状态跨热重载保留，重启清零（无持久化）。
* 可选全局并发闸（`max_concurrency`）：超限请求挂起等待（近似 FIFO），客户端断开即出队；`/admin/status` 可见 in_flight/waiting。

## 开发

```bash
go test ./...          # 单元 + 集成测试（httptest 模拟上游）
go test -race ./...
```

新增一个 Provider 协议 = 在 `internal/adapter/<name>/` 实现 `Protocol / BuildRequest / TransformBody / ClassifyError` 四个方法 + 在 `cmd/vmr/main.go` 加一行 blank import。OpenAI 兼容的厂商直接用 `type: openai`，Anthropic 兼容的厂商（MiniMax、DeepSeek 的 `/anthropic` 口等）直接用 `type: anthropic`，均无需写代码。vmr 不做跨协议转换。

设计文档：`VirtualModelRouter_v2_Fable5.md`；开发计划与验收记录：`DEV_PLAN.md`。
