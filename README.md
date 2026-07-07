<!-- Ver 2026-07-07 15:10, by Fable 5 -->

# vmr — Virtual Model Router

本地运行、单二进制、配置驱动的 LLM 路由器。客户端只连稳定的 Virtual Model 名（如 `coding` / `claude`），Provider、Key、优先级、故障切换全部由 vmr 隐藏。零数据库、零 Web UI，依赖仅 `yaml.v3` + `fsnotify`。

```
OpenAI 客户端    ──(/v1/chat/completions)──┐        ┌─> MiniMax / DeepSeek / OpenRouter (OpenAI 兼容口)
                                           ├─ vmr ──┤
Anthropic 客户端 ──(/v1/messages)──────────┘        └─> MiniMax / DeepSeek / OpenRouter (Anthropic 兼容口)
                                                        失败/冷却自动切换到下一优先级
```

调用方用哪种协议，就在该协议的兼容端点间路由，vmr 不做跨协议转换。OpenRouter 两种协议面都支持，任一协议的调用方都能经 vmr 使用它。架构与决策详见 [设计文档](VirtualModelRouter_v2_Fable5.md)。

## 快速开始

```bash
go build -o vmr ./cmd/vmr

cp config.example.yaml config.yaml   # api_key 支持 ${ENV} 展开
./vmr check -c config.yaml           # 校验配置并打印路由表
./vmr start -c config.yaml
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

`providers` 定义"我有什么"（type 即协议：`openai` / `anthropic`），`models` 定义"对外叫什么、按什么顺序用"：

```yaml
listen: 127.0.0.1:8800
# api_key: sk-vmr-xxx          # 可选：保护 vmr（Bearer 或 x-api-key）
# max_concurrency: 8           # 全局并发上限，超限请求挂起等待（缺省不限）

providers:
  openrouter:
    type: openai
    base_url: https://openrouter.ai/api/v1
    api_key: ${OPENROUTER_API_KEY}
  openrouter_a:                # 同一服务的 Anthropic 协议面是另一个 provider
    type: anthropic
    base_url: https://openrouter.ai/api/v1
    api_key: ${OPENROUTER_API_KEY}

models:
  coding:                      # 协议由 endpoints 自动推断，混协议报错
    endpoints:
      - {provider: openrouter, model: z-ai/glm-5.2, priority: 1}
  claude:                      # anthropic 协议 → 走 /v1/messages
    endpoints:
      - {provider: openrouter_a, model: minimax/minimax-m3, priority: 1}
```

全部字段与校验规则见设计文档 §9。修改配置数秒内热生效；坏配置被拒绝、不影响运行实例。

## 端点与 CLI

| 端点/命令 | 作用 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI 协议入口（流式 + 非流式） |
| `POST /v1/messages` | Anthropic 协议入口（流式 + 非流式） |
| `GET /v1/models` | Virtual Model 列表（两种客户端均可解析） |
| `GET /admin/status` | 端点健康 + 并发指标（仅 loopback） |
| `vmr check -c config.yaml` | 校验配置、打印路由表与 Key 状态 |
| `vmr status -c config.yaml` | 渲染运行实例的健康与并发占用 |

响应带 `X-VMR-Endpoint`（实际命中端点）与 `X-VMR-Attempts`（尝试次数）。

## 故障切换

上游失败即按优先级顺序逐个尝试下一端点，直到成功或所有可用端点耗尽（`max_attempts` 可选设上限）。失败驱动的被动健康：网络/5xx 短冷却指数退避，401/额度耗尽/模型不存在长冷却，429 尊重 `Retry-After`；冷却到期放行单个探针请求验证恢复。400 类客户端错误不切换；全部候选失败时原样返回最后一次上游错误。流式只在首字节前切换。机制详见设计文档 §5–6。

## 审计日志

默认开启，每个聊天请求记一行 JSONL：调用方与上游两层的完整 request/response（凭证掩码、单 body 上限 1MiB），供事后统计脚本使用。

```bash
./vmr start -c config.yaml                # 写入 $VMR_LOG_DIR（未设置则系统临时目录）
./vmr start -c config.yaml -audit=false   # 关闭
ls "$TMPDIR"/vmr-audit-*.jsonl            # 每天一个文件
jq '.model, .outcome, .dur_ms' vmr-audit-2026-07-07.jsonl   # body 为合法 JSON 时可直接 jq 查询
```

记录格式契约见设计文档 §8。

## 开发

```bash
go test -race ./...
```

新增 Provider：OpenAI/Anthropic 兼容的厂商直接配 `type: openai|anthropic`，零代码。新协议 = `internal/adapter/<name>/` 实现四方法接口 + `cmd/vmr/main.go` 一行 blank import（设计文档 §5）。
