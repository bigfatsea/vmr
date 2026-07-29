<!-- Ver 2026-07-30 12:00, by Sonnet 5 -->
<!-- keywords: LLM 路由器, LLM 网关, AI agent 网关, agent-first, OpenAI 兼容代理, Anthropic API 代理, 故障切换, 模型路由, 负载均衡, 本地部署, 单二进制, MiniMax, DeepSeek, OpenRouter, Claude Code, LiteLLM 替代 -->

# vmr — Virtual Model Router

**为 AI agent 而生的透明 LLM 路由器。** 一个模型名，背后所有供应商，自动故障切换——字节级透传，你的 agent 发出去的请求，到供应商那边还是原来的样子。

[English](README.md) | 简体中文

Agent 是无人值守跑的：凌晨三点某家供应商限流、欠费、宕机时，没人盯着终端。`vmr` 挡在你的 agent 运行时和 LLM 供应商之间——这种事发生时，agent 自动切换到下一家继续跑，你是事后从日志里知道的，不是从一个跑死的会话里，第二天早上再向自己解释发生了什么。

把 Claude Code、OpenClaw，或任何 OpenAI/Anthropic SDK 指向一个稳定的虚拟模型名（`coding` / `claude` / `agent`）。vmr 把供应商、账号、API Key、优先级、故障切换全部藏在它背后——而且跟那些"先翻译成自己的内部格式"的 SDK 型网关不同，vmr 从不会先改写你的请求。**→ 想对比的话看这篇：[为什么选 vmr 而不是 LiteLLM](docs/Why_vmr_over_LiteLLM.zh.md)**

## 为什么用 vmr

- **一个名字，所有供应商** —— 上游（MiniMax、DeepSeek、OpenRouter 及任何协议兼容厂商）的增删与排序只是改一个 YAML，数秒热生效，客户端永远不用动。`base_url` 路径重叠自动消除——写不写 `/v1`，vmr 都能拼出正确的 URL。
- **字节级透传** —— 没有中间表示，永不做协议翻译。请求与响应和直连供应商字节一致（含响应头），仅有的例外是虚拟名↔真实名的 model 字段改写和两个有触发守卫、有实证依据的厂商怪癖修复。上游 3xx 重定向原样透传（绝不静默跟随）。未知 API 参数原样通过——上游新功能上线当天即可用，vmr 代码零改动。
- **真正会切换的故障切换** —— 健康追踪按错误类别区分冷却（限流 ≠ 坏 Key ≠ 内容拦截 ≠ 网关自己报的转发失败），指数退避，尊重 `Retry-After`。恢复检测默认用一个跟真实流量彻底解耦的后台探测——一次又慢又大的请求不会拖累其他正在等待恢复端点的并发调用方（仍可通过 `probe_mode: passive` 切回单飞探针防惊群的旧行为）。内容合规拒绝会切换供应商，但**不惩罚**健康端点。
- **条件感知路由 + 会话粘性** —— 端点声明自己实际支持什么（`capabilities: [image, tools]`、`max_context_tokens`），请求需要而某个端点没声明的能力会被自动跳过；估算过于保守时有内建的降级兜底，绝不会因为一次猜测就拒掉一条本该成功的请求。多轮对话会尽量留在上游 prompt cache 已经预热的那个端点上，让"更聪明的路由"不会因为中途换供应商反而悄悄推高成本。
- **真流式** —— SSE 事件到达即转发。归一化器只在检测到厂商"思考内联"病理形态时才缓冲，且 `</think>` 闭合后立即恢复实时流。
- **双协议一体** —— 原生 `POST /v1/chat/completions`（OpenAI）与 `POST /v1/messages`（Anthropic）两个入口，各自严格在本协议族内路由。不做有损的跨协议翻译——这是特性，不是缺口。
- **飞行记录仪式审计日志，为 agent 场景而设计** —— 每个请求一行 JSONL，双层完整记录（调用方↔vmr、vmr↔上游）、每次 failover 尝试、实际生效的归一化清单。`vmr report` 把日志变成用量/延迟/可用度统计——而且专门读得懂 Agent 流量：请求自动分组为会话 → 任务 → 轮次，每份详单用 🆕 前缀标记本轮新增，工具使用报告直接告诉你哪些声明的工具从未被调用。`vmr story` 更进一步：把单次 Agent 任务的完整执行过程还原成一份可读的叙事（进了什么上下文、模型拿它做了什么、哪一次历史压缩丢了什么信息），外加一套规则派生的行为剖面，可以拿两次运行直接做差对比。过期的日志文件自动压缩为 `.zst`（实测缩小 20~75 倍），也可以设置自动过期。
- **视觉 token 减负（可选）** —— 入口处压缩超大内联图片附件；默认关闭，fail-open；降采样结果按内容哈希落盘缓存，避免重复处理。
- **Unix 风格工具** —— 单二进制、零数据库、零 Web UI、零运行时插件。坏配置拒绝启动（热加载同样拒绝）。只有 4 个直接依赖，完整清单见 `go.mod`。
- **性能是量出来的，不是猜的** —— 压测至 150 req/s：11 个测试场景中有 9 个路由/透传开销 p95 在 10ms 以内，唯一有实质成本的是可选的图片降采样。细节见 [`loadtest/`](loadtest/)。

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
./vmr.sh ps             # 列出本机全部 vmr 实例：端口 + 配置文件 + uptime
./vmr.sh <check|diagnose|report|…>   # 任意 vmr 子命令，原样转发给二进制

# service 模式 —— 交给操作系统 init 系统监督：崩溃自动重启 + 登录自启。
# macOS → launchd user agent；Linux → systemd 用户单元。install 会渲染并注册
# 服务描述文件，同时从当前 shell 抓取 config.yaml 引用的全部 ${VAR}
# 生成 ~/.config/vmr/env（0600）——init 系统的环境是干净的，否则 key 全为空。
./vmr.sh service install     # 注册并启动（幂等，改路径/配置后重跑即更新）
./vmr.sh service status      # 另有 start / stop / restart / logs
./vmr.sh service uninstall   # 停止并注销
# Linux：若需注销登录后服务仍运行，执行一次 loginctl enable-linger $USER
```

两种模式同一时间只用一种——`service install`/`start` 会自动停掉 dev 模式进程。macOS 下 service 模式的服务日志在 `~/Library/Logs/vmr.log`（TCC 不允许 launchd 对外置卷做文件操作）；审计日志照常跟随 config 的 `log_dir`。

把客户端的 Base URL 指向 vmr 即可：

```bash
# OpenAI 协议客户端（OPENAI_BASE_URL=http://127.0.0.1:8800/v1）
# 如果 config.yaml 里设置了 vmr 自己的 api_keys，加上 -H "Authorization: Bearer <key>"
curl http://127.0.0.1:8800/v1/chat/completions -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic 协议客户端（如 Claude Code：ANTHROPIC_BASE_URL=http://127.0.0.1:8800）
# Anthropic 客户端发送的是 x-api-key 而非 Authorization——vmr 两种都认
curl http://127.0.0.1:8800/v1/messages -H "Content-Type: application/json" \
  -H "x-api-key: sk-vmr-local-xxx" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'

# 列出虚拟模型（认证规则同上，用的是 vmr 自己的 api_keys）
curl http://127.0.0.1:8800/v1/models -H "Authorization: Bearer sk-vmr-local-xxx"

# 各 endpoint 的健康状态 + 并发情况（仅限本机回环访问；即使配了 api_keys 也不需要）
curl http://127.0.0.1:8800/admin/status
```

第一次跑起来需要知道的就这些。再往后的东西——完整配置参考、协议/归一化细节、审计日志与报表格式、图片降采样、完整 CLI——都在 **[用户指南](docs/UserGuide.zh.md)** 里。

## 延伸阅读

- **[用户指南](docs/UserGuide.zh.md)** —— 完整配置参考、透传/归一化行为、故障切换与健康细节、审计日志与 `vmr report`、图片降采样、完整 CLI 参考。
- **[为什么选 vmr 而不是 LiteLLM](docs/Why_vmr_over_LiteLLM.zh.md)** —— 字节级透传跟翻译型网关比到底差在哪，以及什么时候你其实该选翻译型网关。
- **设计文档**（分两部分）—— 完整架构与每条设计决策，附带取舍理由：[Part 1 —— 路由核心](docs/VirtualModelRouter_Design_v4_Core.md)、[Part 2 —— `vmr report`/`vmr story`](docs/VirtualModelRouter_Design_v4_Analytics.md)。

## 开发

```bash
go test -race ./...
```

新增 Provider：OpenAI/Anthropic 兼容的厂商只是一条配置，零代码。新协议 = `internal/adapter/<name>/` 实现 `Adapter` 四方法接口 + `cmd/vmr/main.go` 一行 blank import。

## 开源协议

[MIT](LICENSE)
