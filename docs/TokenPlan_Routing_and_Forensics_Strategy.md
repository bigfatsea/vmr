<!-- Ver 2026-08-06 21:49, by Gemini 3.6 Flash -->

# VMR 多 Token-Plan 调度与零埋点 Agent 飞行记录仪战略规划与竞品分析

## 1. 概述与核心定位

VMR (Virtual Model Router) 是一款专为 AI Agent 研发设计的单二进制、零代码侵入（Zero-Instrumentation）、字节忠实（Byte-Faithful）的智能路由与飞行记录仪。

在 AI Agent 落地和多模型采购日趋复杂的背景下，VMR 确立了如下核心定位与市场切入策略：

* **目标用户画像**：**100 人以内的中小型 AI 研发团队、AI 创业项目与重度 Agent 架构师**。此类团队采购了多家厂商的 Token-Plan / Coding-Plan 以降低成本，急需在不修改代码的前提下实现多订阅聚合、防锁死、防 Prefix-Cache 失效，同时具备轻量级、无 DB 依赖的 Agent 执行诊断能力。
* **双核一体两面架构**：
  1. **In-Flight Passthrough Router**：极轻量代理层，支持 OpenAI / Anthropic 协议双向原样透传。支持基于 Session Affinity 的 Prefix-Cache 保护、错误类型感知的 Failover、以及基于本地消耗累计的 Token-Plan 额度平衡调度。当关闭 Audit Log 时，可提供极致的代理性能（p95 延迟小与 10ms）。
  2. **Post-Flight Agent Forensics**：基于两层忠实 Raw Byte Audit Log，提供零埋点的 Agent 执行故事还原（`vmr story`）、跨 Run 行为差异对比（`vmr story -compare`）与一键二进制重放（`vmr replay`）。
  3. **模块间松耦合**：路由与诊断分析模块在逻辑和运行态完全独立，仅以标准 JSONL/zstd 格式的 Audit Log 作为唯一数据契约。

---

## 2. 核心痛点与需求分析

### 2.1 多 Token-Plan 采购的悖论与难点
为了降低大模型使用成本，团队倾向于采购阿里、字节、MiniMax、DeepSeek 等厂商的 Token-Plan / Coding-Plan（按月打包订阅，单价远低于按量 API）。但这带来了三个核心问题：
1. **厂商绑定与额度不均**：购买单一厂商套餐容易被能力锁死；购买多家套餐容易出现部分套餐跑爆溢出到高价按量 API、部分套餐吃不满浪费的局面。
2. **Prefix-Cache 跨账号隔离陷阱**：在长上下文 Agent 会话中，不同账号间的 Prompt Cache 完全隔离。常规网关的无状态轮询调度会导致 Agent 的多轮对话频繁落到不同账号上，缓存命中率归零，成本与延迟暴涨。
3. **上游缺乏标准配额 API**：各大厂商控制台对于 Token-Plan 剩余额度、自然日刷新限制等缺乏稳定、公开的标准 API 接口。

### 2.2 传统 Agent 观测工具的痛点
现有的 Agent 观测平台（如 LangSmith、Braintrust 等）依赖在业务代码中引入 SDK、装饰器或 OpenTelemetry 埋点。这带来了额外的开发负担、潜在的流式传输破坏以及协议层信息的丢失。

---

## 3. 全方位竞品深度对比

为了明确 VMR 的独特性与技术壁垒，将 VMR 与市面上四大类典型解决方案进行全方位对比：

### 3.1 竞品类别分类
1. **开源转发网关**：One-API、New-API、One-Hub 等。
2. **企业级 AI 网关**：LiteLLM、Bifrost 等。
3. **商业云网关/聚合服务**：Cloudflare AI Gateway、Vercel AI Gateway、七牛 AI 网关、OpenRouter 等。
4. **Agent 可观测与调试工具**：LangSmith、Braintrust、Arize Phoenix 等。

### 3.2 深度对比矩阵

| 维度 | 开源转发网关 (One-API 等) | 企业级网关 (LiteLLM) | 商业云网关 (OpenRouter / Cloudflare) | SDK 观测平台 (LangSmith / Phoenix) | **VMR (本方案)** |
|---|---|---|---|---|---|
| **架构与部署开销** | 轻量，支持 SQLite/MySQL | 偏重，依赖 PostgreSQL + Redis + Web UI | 托管 SaaS 服务，存在密钥外泄隐患 | 托管 SaaS / 重型 Self-Hosted 部署 | **单二进制 (Single-binary)，零 DB，无外部依赖** |
| **接入侵入性** | 替换 `base_url` (无侵入) | 替换 `base_url` (无侵入) | 替换 `base_url` (无侵入) | **强侵入** (需在代码中导入 SDK / 装饰器 / OTel) | **替换 `base_url` (零代码侵入，Zero-Instrumentation)** |
| **协议处理方式** | 强行转为 OpenAI 格式 | 强行转为 OpenAI 格式 | 统一中间格式转换 | 仅捕获 SDK 层 Payload | **字节忠实透传 (Byte-Faithful Passthrough)，保留厂商特有字段** |
| **Prefix-Cache 保护** | ❌ 无状态/随机轮询，极易摧毁 Cache | ⚠️ 部分支持基础 Session Affinity | ❌ 无状态路由，不支持自备账号 Cache 优化 | N/A (仅观测，不参与路由) | **✅ 原生 Session-Sticky (System Prompt + First Msg 哈希绑定)** |
| **Token-Plan 水位调度** | ❌ 仅感知下游虚拟扣费，不感知上游额度 | ❌ 无针对 Token-Plan 配额平滑调度的逻辑 | ❌ 不支持带入用户自备 Token-Plan 额度池 | N/A (仅观测) | **✅ 本地用量累计 (Response Usage) + 平滑加权水位调度** |
| **执行诊断与对比** | ❌ 仅有基础日志 | ⚠️ 简单日志与 Cost 统计 | ❌ 仅有 HTTP 调用日志 | ✅ 强大的链路 Trace，但无 1-Click Replay | **✅ 步骤级 Story 还原 (`vmr story`) + 跨 Run 差异对比 (`compare`) + 1-Click Replay** |

### 3.3 总结 VMR 的差异化壁垒
1. **相较于普通网关**：VMR 懂 Agent 的 Prefix-Cache 价值，引入了 Session-Sticky 锁定，并补充了基于本地 Response 消耗累计的 Token-Plan 水位平滑调度。
2. **相较于重型网关**：VMR 保持单二进制极简运维，无数据库负担，遵循 Byte-Faithful Passthrough 原则。
3. **相较于代码埋点观测工具**：VMR 在网络代理层实现“零埋点”抓取忠实的底层 Byte，并提供专属的 `vmr story` 与 `vmr replay`。

---

## 4. 技术实现策略与调度算法

针对上游无标准 API 的现状，VMR 采用“本地 Response 消耗累计 + 双层会话黏性路由”策略。

### 4.1 本地 Response Token 累计策略
在配置文件 [`UserGuide.md`](file:///Users/stanford/code/vmr/docs/UserGuide.md#config-layout) 中允许为每个 Provider / Token-Plan 配置预估额度与重置周期：

```yaml
providers:
  - name: ali-coding-plan-1
    base_url: {openai: https://dashscope.aliyuncs.com/compatible-mode/v1}
    api_key: ${ALI_KEY_1}
    budget:
      total_tokens: 100000000     # 100M tokens 初始套餐配额
      reset_period: monthly       # monthly / daily / fixed
      reset_day: 1                # 每月 1 号自动重置本地累计器
```

* **运行态统计**：每次请求成功返回后，VMR 从响应的 `usage` 结构（或流式末尾节点）提取实际消耗的 token 数，并在内存中更新该 Provider 的 `consumed_tokens`。
* **有效权重计算**：
  $$\text{Effective Weight} = \max\left(0, \frac{\text{total\_tokens} - \text{consumed\_tokens}}{\text{total\_tokens}}\right)$$

### 4.2 结合 Prefix-Cache 保护的双层路由算法

为了防止用量平衡算法破坏长上下文缓存，路由决策按如下两层执行：

1. **第一层：Session Affinity 强锁定（Sticky Pin）**
   * 对于已建立的 Agent 多轮会话（通过 System Prompt + 首条 User Message 的 Hash 提取指纹），只要原 Endpoint 处于健康状态（未触发 4xx/5xx/429/Block 且满足 Capabilities 要求），**强制路由至原 Endpoint**。
   * 该优先级高于任何水位相位调度，确保 Prefix-Cache 命中率达到 100%。

2. **第二层：新会话加权分流（Quota-Aware Balanced Allocation）**
   * 当接收到全新的 Agent 会话请求时，计算候选 Endpoint/Provider 的 `Effective Weight`。
   * 采用平滑加权轮询（Smooth Weighted Round-Robin）算法分配新 Session。消耗较少、剩余额度比例较大的 Token-Plan 将获得更高的新 Session 接入概率。

---

## 5. 路线图与后续实施规划

项目的实施分为两个明确阶段，保持一体两面的解耦架构：

### 阶段一：Router 侧 Token-Plan 水位感知路由与算法增强
1. 在配置解析器中增加 `budget` 配置项支持（`total_tokens`、`reset_period` 等）。
2. 在路由引擎中接入 Response `usage` 本地统计器（支持按天/月时间窗口轮转）。
3. 扩展现有的 `strategy` 路由逻辑，在 Sticky 命中之外，实现基于 `Effective Weight` 的新会话分配策略。

### 阶段二：Analytics 侧 Agent Story 与 Audit 丰富化
1. 保持现有 [`UserGuide.md`](file:///Users/stanford/code/vmr/docs/UserGuide.md#agent-task-narratives-vmr-story) 中的 `vmr story` 与 `vmr story -compare` 分析指令独立高效。
2. 在 `vmr report` 的用量报告中追加基于 Token-Plan 本地预算的消耗进度与预测看板。
