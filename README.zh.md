<!-- Ver 2026-07-08 13:10, by Fable 5 -->
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
- **飞行记录仪式审计日志** —— 每个请求一行 JSONL，双层完整记录（调用方↔vmr、vmr↔上游）、每次 failover 尝试、错误类别、实际生效的归一化清单。`vmr report` 把日志变成用量/延迟/可用度统计。
- **视觉 token 减负（可选）** —— 入口处压缩超大内联图片附件；一个配置项，默认关闭，fail-open。
- **Unix 风格工具** —— 单二进制、零数据库、零 Web UI、零运行时插件。坏配置拒绝启动（热加载同样拒绝）。依赖只有 `yaml.v3`、`fsnotify`、`golang.org/x/image`，就这些。

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

# 或用 vmr.sh 以 daemon 方式管理（先校验再拉起，日志/审计缺省落 logs/）：
./vmr.sh start
./vmr.sh status
./vmr.sh logs
./vmr.sh stop
```

把客户端的 Base URL 指向 vmr 即可：

```bash
# OpenAI 协议客户端（OPENAI_BASE_URL=http://127.0.0.1:8800/v1）
curl http://127.0.0.1:8800/v1/chat/completions -H "Content-Type: application/json" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic 协议客户端（如 Claude Code：ANTHROPIC_BASE_URL=http://127.0.0.1:8800）
curl http://127.0.0.1:8800/v1/messages -H "Content-Type: application/json" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'
```

## 配置

`providers`/`models` 都按协议分两层：外层 key 是协议（`openai` / `anthropic`），内层才是名字。一个 model 的 endpoints 只能引用同协议分组下的 provider——跨协议混用没有语法能表达它，而不是靠校验去抓。同一账号的两个协议面可复用同一个短名（`openrouter`），不需要后缀区分：

```yaml
listen: 127.0.0.1:8800
# api_key: sk-vmr-xxx          # 可选：保护 vmr（Bearer 或 x-api-key）
# max_concurrency: 8           # 全局并发上限，超限请求挂起等待（缺省不限）
# image_downscale: 512         # 请求内联图片长边像素上限，缺省关闭

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
./vmr start -c config.yaml                 # 写入 $VMR_LOG_DIR（未设置则系统临时目录）
./vmr start -c config.yaml -audit=false    # 关闭
jq '.model, .outcome, .attempts[0].norm' vmr-audit-2026-07-08.jsonl

./vmr report logs/vmr-audit-*.jsonl        # → vmr-report.json + vmr-report.md
```

`vmr report` 同时统计 tokens 与字节（上游不回报 usage 时以字节兜底）：按 日期×协议×模型 的行、按端点的可用度与错误分布、延迟分位、吞吐。JSON 是二次开发（图表/Dashboard）的数据源。

## 请求图片自动降采样

可选，默认关闭。开启后，超过设定长边的内联 base64 图片附件会被等比缩小、转 JPEG 再发上游——为截图密集的 agent 工作流削减 vision token 成本。只处理请求，不碰响应，不抓远程 URL；动图与解码失败一律原样透传（fail-open）。

```yaml
image_downscale: 512   # 长边像素上限；0 或缺省 = 关闭
```

## 端点与 CLI

| 端点/命令 | 作用 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI 协议入口（流式 + 非流式） |
| `POST /v1/messages` | Anthropic 协议入口（流式 + 非流式） |
| `GET /v1/models` | Virtual Model 列表（两种 SDK 均可解析） |
| `GET /admin/status` | 端点健康 + 并发指标（仅 loopback） |
| `vmr check -c config.yaml` | 校验配置、打印路由表与 Key 状态 |
| `vmr status -c config.yaml` | 渲染运行实例的健康与并发占用 |
| `vmr report [-o dir] <glob>` | 审计日志 → 用量统计（JSON + Markdown） |
| `./vmr.sh start\|stop\|restart\|status\|logs` | daemon 生命周期 |

经路由的响应带 `X-VMR-Endpoint`（实际命中端点）与 `X-VMR-Attempts`（尝试次数）。

## 开发

```bash
go test -race ./...
```

新增 Provider：OpenAI/Anthropic 兼容的厂商只是一条配置，零代码。新协议 = `internal/adapter/<name>/` 实现三方法接口 + `cmd/vmr/main.go` 一行 blank import。

架构与全部设计决策（含每条背后的事故账本）：[设计文档](VirtualModelRouter_v2_Fable5.md)。

## 开源协议

[MIT](LICENSE)
