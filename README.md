<!-- Ver 2026-07-07 17:45, by Fable 5 -->

# vmr — Virtual Model Router

本地运行、单二进制、配置驱动的 LLM 路由器。客户端只连稳定的 Virtual Model 名（如 `coding` / `claude`），Provider、Key、优先级、故障切换全部由 vmr 隐藏。零数据库、零 Web UI，依赖仅 `yaml.v3` + `fsnotify` + `golang.org/x/image`（图片降采样用）。

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

`providers`/`models` 都按协议分两层：外层 key 是协议（`openai` / `anthropic`），内层才是名字。一个 model 的 endpoints 只能引用同协议分组下的 provider——想让同一账号同时有 OpenAI 和 Anthropic 两个面，就在两个协议分组下各写一份同名 provider，不需要后缀区分：

```yaml
listen: 127.0.0.1:8800
# api_key: sk-vmr-xxx          # 可选：保护 vmr（Bearer 或 x-api-key）
# max_concurrency: 8           # 全局并发上限，超限请求挂起等待（缺省不限）
# image_downscale: 512         # 请求内联图片长边像素上限，缺省不限（关闭）

providers:
  openai:
    openrouter:
      base_url: https://openrouter.ai/api/v1
      api_key: ${OPENROUTER_API_KEY}
  anthropic:
    openrouter:                # 同一服务的 Anthropic 面，同名不冲突（两层 map 天然隔离）
      base_url: https://openrouter.ai/api/v1
      api_key: ${OPENROUTER_API_KEY}

models:
  openai:
    coding:
      endpoints:
        - {provider: openrouter, model: z-ai/glm-5.2}   # 不写 priority：endpoints 列表顺序就是尝试顺序
  anthropic:
    claude:                    # anthropic 协议 → 走 /v1/messages
      endpoints:
        - {provider: openrouter, model: minimax/minimax-m3}
```

全部字段与校验规则见设计文档 §10。修改配置数秒内热生效；坏配置被拒绝、不影响运行实例。

## 端点与 CLI

| 端点/命令 | 作用 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI 协议入口（流式 + 非流式） |
| `POST /v1/messages` | Anthropic 协议入口（流式 + 非流式） |
| `GET /v1/models` | Virtual Model 列表（两种客户端均可解析） |
| `GET /admin/status` | 端点健康 + 并发指标（仅 loopback） |
| `vmr check -c config.yaml` | 校验配置、打印路由表与 Key 状态 |
| `vmr status -c config.yaml` | 渲染运行实例的健康与并发占用 |
| `vmr report [-o dir] <glob>` | 审计日志 → 统计报表（JSON + Markdown） |

响应带 `X-VMR-Endpoint`（实际命中端点）与 `X-VMR-Attempts`（尝试次数）。

## 故障切换

上游失败即按优先级顺序逐个尝试下一端点，直到成功或所有可用端点耗尽（`max_attempts` 可选设上限）。失败驱动的被动健康：网络/5xx 短冷却指数退避，401/额度耗尽/模型不存在长冷却，429/503 尊重 `Retry-After`；冷却到期放行单个探针请求验证恢复。两类特殊处理：400 类客户端错误不切换、直接返回；**内容合规拦截**（各厂审核标准不一，如 OpenRouter 的 403 moderation、DeepSeek 的内容风险 400）会继续切换下一端点但**不惩罚**被拦端点——它只是拒绝这一条请求，并没有坏。全部候选失败时原样返回最后一次上游错误。流式只在首字节前切换。机制详见设计文档 §5–6。

## 响应归一化

VMR 透传上游响应时做 5 步归一化，确保客户端拿到的内容与「直连上游」时**字节级一致**：

| 步 | 做什么 | 原因 |
| --- | --- | --- |
| 1 | 把响应每个 chunk 的 `"model":"<upstream>"` 改回 `"model":"<client_virtual_model>"` | OpenAI/Anthropic JS SDK 按 `response.model === request.model` 做 prompt cache 关联 + per-model hook，**不一致会静默丢消息** |
| 2 | 剥离 content 里的 `<think>...</think>` 块 | MiniMax M3 thinking 模式下把推理放在 content 里，不剥会被持久化进 assistant message，下轮 prompt 含上轮思考 → **模型陷入反馈循环** |
| 3 | trim `</think>\n\n` 末尾的两个换行 | 避免助手消息每轮以两个空行起头 |
| 4 | 剥离 MiniMax thinking=medium 下的纯文本「Thinking Process:」结构化思考 | OpenClaw 的 `Reasoning: off` 是 UI 开关不影响模型行为；不剥用户会看到「思考过程 + 草稿迭代」 |
| 5 | 流式响应末尾追加 `data: [DONE]\n\n` | MiniMax 偶尔不发 [DONE]；客户端 SDK 靠 [DONE] 标记收尾以避免 idle abort |

完整逻辑见设计文档 §5.5。**所有 5 步只对上游 2xx 响应生效**，4xx/5xx 走原样透传保留厂商错误结构。

## 请求图片自动降采样

可选功能，默认关闭。开启后，请求里超过设定长边像素的内联图片附件（截图/照片）会被等比缩小、统一转 JPEG 再转发上游——降低 vision token 消耗，只影响请求，不碰响应，也不 fetch 远程图片 URL：

```yaml
image_downscale: 512   # 长边像素上限；0 或缺省 = 关闭
```

不带图片的请求只多付出一次字符串扫描的开销；动图与解析失败一律原样透传（fail-open）。机制详见设计文档 §7。

## 审计日志

默认开启，每个聊天请求记一行 JSONL：调用方与上游两层的完整 request/response（凭证掩码、单 body 上限 1MiB），供事后统计脚本使用。

```bash
./vmr start -c config.yaml                # 写入 $VMR_LOG_DIR（未设置则系统临时目录）
./vmr start -c config.yaml -audit=false   # 关闭
ls "$TMPDIR"/vmr-audit-*.jsonl            # 每天一个文件
jq '.model, .outcome, .dur_ms' vmr-audit-2026-07-07.jsonl   # body 为合法 JSON 时可直接 jq 查询
```

记录格式契约见设计文档 §9。

## 统计分析（vmr report）

把审计日志聚合成统计报表，tokens 与字节双指标（无 usage 的记录用字节兜底）：

```bash
./vmr report "$TMPDIR"/vmr-audit-*.jsonl        # 通配符匹配多个日志文件
./vmr report -o out/ logs/vmr-audit-2026-07.jsonl
```

输出两个文件：

* `vmr-report.json` — 细粒度数据表：按 日期×模型 的用量/延迟/吞吐/成功率/fallback（`rows`），按 日期×端点 的可用度与错误分布（`endpoints`）。图表、Dashboard 等二次开发以它为数据源。
* `vmr-report.md` — 人读版：总览、按模型汇总、端点可用度、按日趋势、错误分布。

字段定义与格式契约见设计文档 §9.4（与审计日志格式联动更新）。

## 开发

```bash
go test -race ./...
```

新增 Provider：OpenAI/Anthropic 兼容的厂商直接在对应协议分组下加一条配置，零代码。新协议 = `internal/adapter/<name>/` 实现四方法接口 + `cmd/vmr/main.go` 一行 blank import，之后 `providers.<name>`/`models.<name>` 自动可用（设计文档 §5）。
