<!-- Ver 2026-08-06 10:30, by Gemini 3.6 Flash -->
<!-- keywords: LLM 路由器, LLM 网关, AI agent 网关, agent-first, OpenAI 兼容代理, Anthropic API 代理, 故障切换, 模型路由, 负载均衡, 本地部署, 单二进制, MiniMax, DeepSeek, OpenRouter, Claude Code, LiteLLM 替代, 黑匣子, 审计重放, 行为剖面 -->

# vmr — 无侵入透明路由器与 AI Agent 全生命周期黑匣子

**vmr** 是一个单二进制的、给无人值守 Agent 用的透明路由器与黑匣子。一个稳定的虚拟模型名字（`coding`、`claude`、`agent`）把供应商、Key、故障切换规则全部藏在身后——把任意 OpenAI/Anthropic 兼容客户端的 `base_url` 指向 vmr 即可，**无需任何 SDK 修改或代码埋点**。

正是这份字节级透传——从不做协议翻译——让这份记录真正可信：vmr 记下来的，从来不是它自己先改写过的东西。每一条请求都会落成一条 `details/` 审计记录、一段 Agent 执行叙事（`vmr story`）、一份跨运行行为剖面对比（`vmr story -compare`），或一次精确的 1-Click 重放（`vmr replay`）。凌晨三点发生的一次故障切换、一次悄无声息的内容拦截，事后你是从日志里看到的，而不是面对一个已经死掉的会话，第二天早上自己都解释不清发生了什么。

[English](README.md) | 简体中文

[ 快速开始 ](#快速开始) | [ 用户指南 ](docs/UserGuide.zh.md)

```
[ Agent 应用 / SDK ] ──(零代码埋点接入)──> [ vmr 透明路由器 ] ──(字节透传)──> [ LLM 上游供应商 ]
                                                  │
                                            (字节级真实审计)
                                                  │
                       ┌──────────────────────────┼──────────────────────────┐
                       ▼                          ▼                          ▼
             [ 1-Click 故障重发 ]        [ vmr report / details ]    [ vmr story / compare ]
```

## 现场视角

### 1. 运行时 Failover 现场 (`details/*.md`)
真实来自内置示例 [`examples/sample-audit.jsonl`](examples/sample-audit.jsonl) —— 自己跑一遍 `./vmr report -o /tmp/out examples/sample-audit.jsonl` 对比即可。主端点悄悄内容拦截了请求，vmr 把同一条 payload 换到备用端点重试，客户端从头到尾只看到一个正常的 200 OK：

```
### Attempt 1/2 · openai-completions:coder-primary:coder-large · ❌ HTTP 403
{"error": {"code": "content_flagged", "message": "This request was flagged by our safety guardrail and blocked.", "type": "guardrail_blocked"}}

### Attempt 2/2 · openai-completions:coder-backup:coder-large-mini · ✅ HTTP 200（耗时 2.5s）
```

### 2. Agent 任务执行叙事与信息丢失 (`vmr story`)
一次真实的多工具 Agent 运行，还原成任务、Step 与上下文压缩截断边界后长这样：

```
Task 1: Search codebase and outline implementation
  Step 1: 用户指令 -> 🆕 检查了 3 个文件 -> 模型回复
  --- ⚠️ 上下文压缩截断边界: 18.5K tokens -> 4.2K tokens ---
  丢弃的实体: [internal/core/router.go, https://docs.example.com/api]
```

### 3. 分叉点检测与 LLM 因果分析 (`vmr story -compare`)
对比同一任务的两次运行（例如 OpenClaw vs Lobster、或 DeepSeek vs Claude），精确定位从哪一步开始选了不同的路径：

```
⚡ 步级分叉点检测 at Step 1 (DivergenceHeavy)
- Journey A: Step 1 调用了 [memory_search, read]
- Journey B: Step 1 调用了 [web_fetch]

## LLM 解读（模型：agent · 分叉点）
| 候选根因 | 直接证据 | 置信度 | 改进建议 |
|---|---|---|---|
| 初始策略分叉 | Journey A 先加载本地上下文；Journey B 先抓取实时网页 | 高 | 在 System Prompt 中统一初始工具的选择优先级 |
```

## 双核能力

### 柱石 A：运行时透明路由与高可用
- **零代码埋点接入**：只需修改 `base_url`，无需修改项目代码或 Tracing SDK。原生支持 OpenAI (`/v1/chat/completions`)、Anthropic (`/v1/messages`) 和 OpenAI Responses (`/v1/responses`) 三大协议入口。
- **错误类感知 Failover**：智能区分限流、死 Key 与内容拦截；后台独立恢复探针，绝不用真实请求当探针，不拖累并发调用。
- **Session-Sticky Prompt Cache 保护**：多轮对话自动钉在已预热的端点上，防止故障切换打断供应商 Prompt Cache 造成费用静默飙升。
- **字节级透传**：零中介格式翻译、零参数改写，上游新特性上线当天可用；包含 MiniMax `<think>` 剥离与软屏蔽空响应等隐式防御。
- **实测过，不是拍脑袋**：压测到 150 req/s，非图片场景 p95 稳定在 10ms 以内；唯一真实开销是可选的图片降采样。见 [`loadtest/`](loadtest/)。

### 柱石 B：运行后审计、叙事与重放
- **两层真实字节记录**：无伪造记录客户端↔VMR、VMR↔上游双层原始字节。
- **1-Click 故障重试 (`vmr replay`)**：基于历史日志字节，1-Click 无损重发快速复现线上故障。
- **统一分析入口 (`vmr analyze`)**：一条命令、一个输出目录——默认一次调用产出完整可导航套件（聚合报表 + 任务 journey），或用 `-journey`/`-compare`/`-corpus` 只变焦进某一个视图。
- **聚合统计报告 (`vmr report`)**：自动归组为会话 → 任务 → 轮次，标注增量 (`🆕`)，揭示声明了却从未被调用的 Tool Schema 浪费。
- **Agent 任务叙事 (`vmr story`)**：把单个任务的完整执行过程还原成逐 Step 的故事——进了什么上下文、模型拿它做了什么、哪一次压缩事件悄悄丢了信息。
- **行为剖面与分叉点对比 (`vmr story -compare`)**：自动对比 9 项行为指标，定位步级分叉点 (Divergence Point)，可选挂载 `-llm-addr` 生成归因因果链。

## 快速开始

### 1. 安装

```bash
# macOS
brew install bigfatsea/tap/vmr
```

或从 [最新 Release](https://github.com/bigfatsea/vmr/releases/latest) 下载对应平台的预编译二进制（darwin/linux，amd64/arm64）——不需要装 Go 工具链。

<details>
<summary>也可以从源码构建</summary>

```bash
go build -o vmr ./cmd/vmr
```
</details>

### 2. 运行

```bash
cp config.example.yaml config.yaml   # api_key 支持 ${ENV} 展开
./vmr check -c config.yaml           # 校验配置并打印路由表
./vmr start -c config.yaml           # 前台运行

# 或后台 dev 模式：
./vmr.sh start          # 另有 stop / restart / status / logs / ps

# 或 OS 服务模式 (launchd / systemd)：
./vmr.sh service install     # 注册并启动
```

### 3. 接入（零代码修改）

将客户端的 Base URL 指向 vmr：

```bash
# OpenAI 协议
OPENAI_BASE_URL=http://127.0.0.1:8800/v1

# Anthropic 协议（如 Claude Code）
ANTHROPIC_BASE_URL=http://127.0.0.1:8800
```

<details>
<summary>Curl 与 API 测试示例</summary>

```bash
# OpenAI Chat Completions
curl http://127.0.0.1:8800/v1/chat/completions -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}]}'

# Anthropic Messages
curl http://127.0.0.1:8800/v1/messages -H "Content-Type: application/json" \
  -H "x-api-key: sk-vmr-local-xxx" \
  -d '{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}'

# OpenAI Responses
curl http://127.0.0.1:8800/v1/responses -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-vmr-local-xxx" \
  -d '{"model":"coding","input":"hi"}'

# 探针与健康状态 (JSON 与 Web 可视化看板)
curl http://127.0.0.1:8800/status
# 或在浏览器中打开 http://127.0.0.1:8800/status.html 访问可视化看板
# 实时控制台日志（浏览器版 tail -f）：http://127.0.0.1:8800/log.html
```
</details>

### 4. 分析

```bash
./vmr analyze -c config.yaml   # 一次调用、一个输出目录：聚合报表 + 每个任务 journey，互相链接
```

`-journey <id>`/`-compare id1,id2`/`-corpus` 可以只变焦进单个任务叙事、一次成对行为对比，或语料级统计，而不是默认的完整套件。

更多细节见 **[用户指南](docs/UserGuide.zh.md)**。

## 为什么选 vmr 而不是翻译型网关

| 维度 | 翻译型网关 (LiteLLM / Bifrost) | vmr (透明路由器 + 黑匣子) |
|---|---|---|
| **架构哲学** | 将所有 API 翻译统一为 OpenAI 格式 | 字节级透传（原生多入口直通） |
| **部署成本** | 需配置数据库、Web UI 与依赖 | 单二进制、零数据库、零代码埋点 |
| **审计追溯** | 元数据 / 摘要化 JSON | 双层原始字节记录 + 1-Click `vmr replay` 重放 |
| **Agent 归因** | 扁平的 HTTP 请求日志 | 任务/Step 叙事还原 (`vmr story`) 与分叉点对比 |

## 延伸阅读

- **[用户指南](docs/UserGuide.zh.md)** —— 完整配置参考、透传与归一化细节、Failover 与健康状态、审计日志与 `vmr report`、完整 CLI 参考。
- **设计文档** —— [Part 1: 路由核心](docs/VirtualModelRouter_Design_v4_Core.md)、[Part 2: 分析与 Story](docs/VirtualModelRouter_Design_v4_Analytics.md)，外加两篇专题：[额度感知路由](docs/VirtualModelRouter_Design_v4_Quota.md)、[战略定位与竞品分析](docs/VirtualModelRouter_Design_v4_Strategy.md)。

## 开发

```bash
go test -race ./...
```

新增 Provider：OpenAI/Anthropic 兼容厂商只是一条配置，零代码。新协议 = `internal/adapter/<name>/` 实现 `Adapter` 接口 + `cmd/vmr/main.go` 一行 blank import。

## 开源协议

[MIT](LICENSE)
