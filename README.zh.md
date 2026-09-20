<!-- Ver 2026-09-12 18:00, by pi -->
<!-- keywords: LLM 路由器, LLM 网关, AI agent 网关, agent-first, OpenAI 兼容代理, Anthropic API 代理, 故障切换, 模型路由, 负载均衡, 本地部署, 单二进制, MiniMax, DeepSeek, OpenRouter, Claude Code, LiteLLM 替代, 黑匣子, 审计重放, 行为剖面 -->

# <img src="docs/vmr-logo.svg" alt="vmr" width="24" height="28" align="absmiddle" style="vertical-align: middle; margin-right: 4px;" /> vmr — 无侵入透明路由器与 AI Agent 全生命周期黑匣子

**vmr** 是一个单二进制的、给无人值守 Agent 用的透明路由器与黑匣子。一个稳定的虚拟模型名字（`coding`、`claude`、`agent`）把供应商、Key、故障切换规则全部藏在身后——把任意 OpenAI/Anthropic 兼容客户端的 `base_url` 指向 vmr 即可，**无需任何 SDK 修改或代码埋点**。

正是这份字节级透传——从不做协议翻译——让这份记录真正可信：vmr 记下来的，从来不是它自己先改写过的东西。每一条请求都会落成一条 `requests/details/` 审计记录、一段 Agent 执行叙事（`vmr analyze -journey`）、一份跨运行行为剖面对比（`vmr analyze -compare id1,id2`）、一次结构差异比对（`vmr diff`），或一次精确的 1-Click 重放（`vmr replay`）。凌晨三点发生的一次故障切换、一次悄无声息的内容拦截，事后你是从日志里看到的，而不是面对一个已经死掉的会话，第二天早上自己都解释不清发生了什么。

[English](README.md) | 简体中文

[ 快速开始 ](#快速开始) | [ 用户指南 ](docs/UserGuide.zh.md)

```
[ Agent 应用 / SDK ] ──(零代码埋点接入)──> [ vmr 透明路由器 ] ──(字节透传)──> [ LLM 上游供应商 ]
                                                  │
                                            (字节级真实审计)
                                                  │
                       ┌──────────────────────────┼──────────────────────────┐
                       ▼                          ▼                          ▼
             [ 1-Click 故障重发 ]        [ vmr analyze / details ]  [ journey / compare ]
```

## 现场视角

### 1. 运行时 Failover 现场 (`requests/details/r-*.md`)
真实来自内置示例 [`examples/sample-audit.jsonl`](examples/sample-audit.jsonl) —— 自己跑一遍 `./vmr analyze -details -o /tmp/out examples/sample-audit.jsonl` 对比即可。主端点悄悄内容拦截了请求，vmr 把同一条 payload 换到备用端点重试，客户端从头到尾只看到一个正常的 200 OK：

```
### Attempt 1/2 · openai-completions:coder-primary:coder-large · ❌ HTTP 403
{"error": {"code": "content_flagged", "message": "This request was flagged by our safety guardrail and blocked.", "type": "guardrail_blocked"}}

### Attempt 2/2 · openai-completions:coder-backup:coder-large-mini · ✅ HTTP 200（耗时 2.5s）
```

### 2. Agent 任务执行叙事与信息丢失 (`vmr analyze -journey <id>`)
一次真实的多工具 Agent 运行，还原成任务、Step 与上下文压缩截断边界后长这样：

```
Task 1: Search codebase and outline implementation
  Step 1: 用户指令 -> 🆕 检查了 3 个文件 -> 模型回复
  --- ⚠️ 上下文压缩截断边界: 18.5K tokens -> 4.2K tokens ---
  丢弃的实体: [internal/core/router.go, https://docs.example.com/api]
```

### 3. 分叉点检测与 LLM 因果分析 (`vmr analyze -compare id1,id2`)
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
- **字节级透传**：零中介格式翻译、零参数改写，上游新特性上线当天可用；包含 MiniMax `<think>` 剥离与软屏蔽审计追踪（`soft_block_detected`）。
- **实测过，不是拍脑袋**：覆盖 17 种场景（包含持续慢流并发、配额水位调度、会话粘性以及按 plain / stream / image 独立成本标定），非图片场景 p95 路由开销稳定在 10ms 以内。见 [`loadtest/`](loadtest/)。
- **实时可观测性**：零内部依赖的轻量级实时指标账本（`internal/livestats`，小时 WAL + 本地 Rollup 归档），TTFT 与产出型 Token 生成速率百分位（p50/p10），以及统一内置控制台（`/status.html`, `/models.html`, `/log.html`, `/help.html`）。

### 柱石 B：运行后审计、叙事与重放
- **两层真实字节记录**：无伪造记录客户端↔VMR、VMR↔上游双层原始字节，落盘不进行 HTML 转义。
- **1-Click 故障重放 (`vmr replay`) 与结构对比 (`vmr diff`)**：基于历史日志字节无损重发复现线上故障，或对任意两条请求坐标进行 Header / Prompt / Tools / 消息 LCP 结构差异分析。
- **领域切片分析套件 (`vmr analyze`)**：基于领域切片（`macro/*.json`）与校验签章 `manifest.json`。一键生成完整 Markdown 报告与静态看板骨架页，支持快速从磁盘重绘（`-render-only`）与强制旁路缓存（`-no-cache`）。
- **聚合统计报告**：自动归组为会话 → 任务 → 轮次，标注上下文增量 (`🆕`)，揭示声明了却从未被调用的 Tool Schema 浪费，建模按量等价成本。
- **Agent 任务叙事 (`vmr analyze -journey <id>`)**：还原逐 Step 的任务执行故事，支持提示词缓存击穿结构归因（Cache Break Attribution）、触碰工件（Touched Artifacts）追踪与压缩信息丢失检测。
- **行为剖面与分叉点对比 (`vmr analyze -compare id1,id2`)**：自动对比核心行为指标，对重复任务进行聚类分析，定位步级分叉点 (Divergence Point)，可选挂载 `-llm-addr` 生成归因因果链。

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
cp config.minimal.yaml config.yaml   # ~10 行即可起步；完整注解参考见 config.example.yaml
export DEEPSEEK_API_KEY=sk-...        # minimal 配置引用的 ${ENV}
./vmr check -c config.yaml           # 校验配置并打印路由表
./vmr start -c config.yaml           # 前台运行

# 或后台 dev 模式：
./vmr.sh start          # 另有 stop / restart / redeploy / status / logs / ps

# 或 OS 服务模式 (launchd / systemd)：
./vmr.sh service install     # 注册并启动
```

### 3. 验证

```bash
./vmr diagnose -c config.yaml   # 一屏红绿灯：配置检查、DNS + TLS、每个端点一次真实 echo 请求、路由预览
```

### 4. 接入（零代码修改）

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

# 探针与实时遥测 (JSON 与 Web 可视化控制台)
curl http://127.0.0.1:8800/status
curl http://127.0.0.1:8800/stats
# 统一内置控制台（概览、模型拓扑、实时日志与配置向导）：
# - 概览大屏 (Overview): http://127.0.0.1:8800/status.html
# - 模型与配额 (Models): http://127.0.0.1:8800/models.html
# - 实时终端日志 (Log): http://127.0.0.1:8800/log.html
# - Agent 配置向导 (Help): http://127.0.0.1:8800/help.html
```
</details>

### 5. 分析

```bash
./vmr analyze -c config.yaml   # 一次调用、一个输出目录：聚合报表 + 每个任务 journey，互相链接
```

`-journey <id>`/`-compare id1,id2`/`-benchmark` 可以只变焦进单个任务叙事、一次成对行为对比，或跨每个候选 journey 的基准统计，而不是默认的完整套件。

更多细节见 **[用户指南](docs/UserGuide.zh.md)**。

## 为什么选 vmr 而不是翻译型网关

| 维度 | 翻译型网关 (LiteLLM / Bifrost) | vmr (透明路由器 + 黑匣子) |
|---|---|---|
| **架构哲学** | 将所有 API 翻译统一为 OpenAI 格式 | 字节级透传（原生多入口直通） |
| **部署成本** | 需配置数据库、Web UI 与依赖 | 单二进制、零数据库、零代码埋点 |
| **审计追溯** | 元数据 / 摘要化 JSON | 双层原始字节记录 + 1-Click `vmr replay` 重放 |
| **Agent 归因** | 扁平的 HTTP 请求日志 | 任务/Step 叙事还原 (`vmr analyze -journey`) 与分叉点对比 |

## 延伸阅读

- **[用户指南](docs/UserGuide.zh.md)** —— 完整配置参考、透传与归一化细节、Failover 与健康状态、审计日志与 `vmr analyze`、完整 CLI 参考。
- **设计文档** —— [Part 1: 路由核心](docs/VirtualModelRouter_Design_v4_Core.md)、[Part 2: 分析与 Journey](docs/VirtualModelRouter_Design_v4_Analytics.md)，外加三篇专题：[额度感知路由](docs/VirtualModelRouter_Design_v4_Quota.md)、[实时指标与控制台](docs/VirtualModelRouter_Design_v4_LiveStats.md)、[战略定位与竞品分析](docs/VirtualModelRouter_Design_v4_Strategy.md)。

## 开发

```bash
go test -race ./...
```

新增 Provider：OpenAI/Anthropic 兼容厂商只是一条配置，零代码。新协议 = `internal/adapter/<name>/` 实现 `Adapter` 接口 + `cmd/vmr/main.go` 一行 blank import。

## 开源协议

[MIT](LICENSE)
