<!-- Ver 2026-08-06 21:49, by Gemini 3.6 Flash -->

# VMR 多 Token-Plan 调度与零埋点 Agent 飞行记录仪战略规划与竞品分析

## 1. 概述与核心定位

VMR (Virtual Model Router) 是一款专为 AI Agent 研发设计的单二进制、零代码侵入（Zero-Instrumentation）、字节忠实（Byte-Faithful）的智能路由与飞行记录仪。

在 AI Agent 落地和多模型采购日趋复杂的背景下，VMR 确立了如下核心定位与市场切入策略：

* **目标用户画像**：**100 人以内的中小型 AI 研发团队、AI 创业项目与重度 Agent 架构师**。此类团队采购了多家厂商的 Token-Plan / Coding-Plan 以降低成本，急需在不修改代码的前提下实现多订阅聚合、防锁死、防 Prefix-Cache 失效，同时具备轻量级、无 DB 依赖的 Agent 执行诊断能力。
* **双核一体两面架构**：
  1. **In-Flight Passthrough Router**：极轻量代理层，支持 OpenAI / Anthropic 协议双向原样透传。支持基于 Session Affinity 的 Prefix-Cache 保护、错误类型感知的 Failover、以及基于本地消耗累计的 Token-Plan 额度平衡调度。当关闭 Audit Log 时，可提供极致的代理性能（p95 延迟小于 10ms，已通过 `loadtest/` 压测验证，见 README）。
  2. **Post-Flight Agent Forensics**：基于两层忠实 Raw Byte Audit Log，提供零埋点的 Agent 执行故事还原（`vmr story`）、跨 Run 行为差异对比（`vmr story -compare`）与一键二进制重放（`vmr replay`）。
  3. **模块间松耦合**：路由与诊断分析模块在逻辑和运行态完全独立，仅以标准 JSONL/zstd 格式的 Audit Log 作为唯一数据契约。

---

## 2. 核心痛点与需求分析

### 2.1 多 Token-Plan 采购的悖论与难点

为了降低大模型使用成本，团队倾向于采购智谱 GLM Coding Plan、Moonshot Kimi 等厂商按月打包的 Coding-Plan（额度内单价远低于按量 API），同时仍用阿里、字节、DeepSeek 等厂商的低价按量 API 覆盖打包套餐没有覆盖到的模型——这两类采购方式通常并存于同一个团队的账号列表里，而不是互相替代。这带来了五个核心问题：

1. **厂商绑定与额度不均**：Coding-Plan 通常只能调用该厂商自家模型；购买多家套餐容易出现部分套餐跑爆溢出到高价按量 API、部分套餐吃不满浪费的局面。
2. **Prefix-Cache 跨账号隔离陷阱**：在长上下文 Agent 会话中，不同账号间的 Prompt Cache 完全隔离。常规网关的无状态轮询调度会导致 Agent 的多轮对话频繁落到不同账号上，缓存命中率归零，成本与延迟暴涨。
3. **上游缺乏标准配额 API**：各大厂商控制台对于 Token-Plan 剩余额度、自然日刷新限制等缺乏稳定、公开的标准 API 接口。
4. **计费模型不统一，"额度"本身有两种形态**：部分 Coding-Plan 是严格的月度/日度 Token 总量桶（如「100M tokens/月」），可以用剩余量做加权调度；但相当一部分主流套餐（典型如 Claude Code 的 Pro/Max 订阅）并非按 Token 总量计费，而是按滚动时间窗口内的用量/会话数限流（例如"每 5 小时一个窗口"），且厂商官方从不精确公开窗口内的剩余额度。这两种形态需要两种不同的调度机制——前者是"额度加权分流"问题，后者本质是"限流感知的退避与切流"问题——不能用同一套 `total_tokens`/`reset_period` 模型硬套，详见 4.1。
5. **合规与账号共享边界**：不少 Coding-Plan（尤其是绑定 CLI 客户端 OAuth 会话、而非标准 API Key 计费的套餐）在服务条款中限定"单一用户/单一客户端"使用。能否合规地把它接入网关做多人共享，取决于厂商条款、以及该套餐是否提供可重定向 `base_url` 的标准 API Key 出口——不能默认所有 Coding-Plan 都允许，需要逐厂商确认后再接入。

### 2.2 传统 Agent 观测工具的痛点

现有的 Agent 观测平台（如 LangSmith、Braintrust 等）依赖在业务代码中引入 SDK、装饰器或 OpenTelemetry 埋点。这带来了额外的开发负担、潜在的流式传输破坏以及协议层信息的丢失。

---

## 3. 全方位竞品深度对比

为了明确 VMR 的独特性与技术壁垒，将 VMR 与市面上五大类典型解决方案进行全方位对比：

### 3.1 竞品类别分类
1. **开源转发网关**：One-API、New-API、One-Hub 等。
2. **企业级 AI 网关**：LiteLLM、Bifrost 等。
3. **商业云网关/聚合服务**：Cloudflare AI Gateway、Vercel AI Gateway、七牛 AI 网关、OpenRouter 等。
4. **Agent 可观测与调试工具**：LangSmith、Braintrust、Arize Phoenix 等。
5. **订阅额度聚合/中继工具**：GitHub 上常见的、面向单一厂商（尤其是 Claude Code）的多账号 Coding-Plan 中继/负载均衡小工具——这类工具的存在本身就说明"多账号池化防 Cache 失效"是个已被验证的真实需求，只是目前的实现都局限在单一厂商内。

### 3.2 深度对比矩阵

| 维度 | 开源转发网关 (One-API 等) | 企业级网关 (LiteLLM) | 商业云网关 (OpenRouter / Cloudflare) | SDK 观测平台 (LangSmith / Phoenix) | 订阅额度聚合工具 (单厂商中继) | **VMR (本方案)** |
|---|---|---|---|---|---|---|
| **架构与部署开销** | 轻量，支持 SQLite/MySQL | 基础代理模式可单机运行；启用虚拟 Key / 预算持久化 / 多实例限流等企业特性时才需要 PostgreSQL（+ 可选 Redis） | 托管 SaaS 服务，业务流量与内容对第三方平台可见，企业合规/数据主权需额外评估 | 托管 SaaS / 重型 Self-Hosted 部署 | 轻量脚本/小型代理，通常单机部署，无 DB | **单二进制 (Single-binary)，零 DB，无外部依赖** |
| **接入侵入性** | 替换 `base_url` (无侵入) | 替换 `base_url` (无侵入) | 替换 `base_url` (无侵入) | **强侵入** (需在代码中导入 SDK / 装饰器 / OTel) | 替换 `base_url` 或 CLI 配置 (无侵入) | **替换 `base_url` (零代码侵入，Zero-Instrumentation)** |
| **协议处理方式** | 强行转为 OpenAI 格式 | 强行转为 OpenAI 格式 | 统一中间格式转换 | 仅捕获 SDK 层 Payload | 通常仅覆盖单一厂商原生协议，不做多协议透传 | **字节忠实透传 (Byte-Faithful Passthrough)，保留厂商特有字段** |
| **Prefix-Cache 保护** | ❌ 无状态/随机轮询，极易摧毁 Cache | ⚠️ 部分支持基础 Session Affinity | ❌ 无状态路由，不支持自备账号 Cache 优化 | N/A (仅观测，不参与路由) | ✅ 局限于单一厂商内的多账号级 Sticky（这是它们存在的初衷） | **✅ 原生 Session-Sticky (System Prompt + First Msg 哈希绑定)** |
| **Token-Plan 水位调度** | ❌ 仅感知下游虚拟扣费，不感知上游额度 | ❌ 无针对 Token-Plan 配额平滑调度的逻辑 | ❌ 不支持带入用户自备 Token-Plan 额度池 | N/A (仅观测) | ✅ 部分支持同厂商多账号轮转，但无法跨厂商统一调度 | **✅ 本地用量累计 (Response Usage) + 平滑加权水位调度** |
| **执行诊断与对比** | ❌ 仅有基础日志 | ⚠️ 简单日志与 Cost 统计 | ❌ 仅有 HTTP 调用日志 | ✅ 强大的链路 Trace，但无 1-Click Replay | ❌ 基本没有，多为纯转发脚本 | **✅ 步骤级 Story 还原 (`vmr story`) + 跨 Run 差异对比 (`compare`) + 1-Click Replay** |

### 3.3 总结 VMR 的差异化壁垒
1. **相较于普通网关**：VMR 懂 Agent 的 Prefix-Cache 价值，引入了 Session-Sticky 锁定，并补充了基于本地 Response 消耗累计的 Token-Plan 水位平滑调度。
2. **相较于重型网关**：VMR 保持单二进制极简运维，无数据库负担，遵循 Byte-Faithful Passthrough 原则。
3. **相较于代码埋点观测工具**：VMR 在网络代理层实现"零埋点"抓取忠实的底层 Byte，并提供专属的 `vmr story` 与 `vmr replay`。
4. **相较于订阅额度聚合中继工具**：这类工具通常只解决单一厂商内的多账号轮转，VMR 是跨厂商、跨协议的统一池化，并原生带有 `vmr story`/`vmr report` 的执行取证与成本归因能力，而这类工具通常止步于纯转发。

---

## 4. 技术实现策略与调度算法

针对上游无标准 API 的现状，VMR 采用"本地 Response 消耗累计 + 双层会话黏性路由"策略。本节仅覆盖 2.1 第 4 点中"月度/日度 Token 总量桶"这一类套餐；"滚动时间窗口限流"类套餐（如 Claude Code Max/Pro）不适用下述模型，留作独立机制评估（见 5. 路线图）。

### 4.1 本地 Response Token 累计策略

在配置文件 [`UserGuide.md`](file:///Users/stanford/code/vmr/docs/UserGuide.md#config-layout) 中允许为每个 Provider 配置预估额度与重置周期。`budget` 挂在 Provider 一级而非 EndpointGroup 一级，因为额度是账号级资源——同一账号的 Coding-Plan 常常被多个虚拟模型下的不同 EndpointGroup 复用，只有挂在 Provider 上才不会被重复计费或分裂成互不相干的多份计数：

```yaml
providers:
  - name: ali-coding-plan-1
    base_url: {openai: https://dashscope.aliyuncs.com/compatible-mode/v1}
    api_key: ${ALI_KEY_1}
    budget:
      total_tokens: 100000000     # 100M tokens 初始套餐配额
      reset_period: monthly       # monthly / daily / fixed
      reset_day: 1                # 每月 1 号自动重置本地累计器（VMR 本地时钟，非厂商账单周期的精确对齐，见下）
```

* **运行态统计**：每次请求成功返回后，VMR 复用 `internal/chatmsg.ExtractUsage` 已有的 `Usage{In, Out, CacheRead, CacheWrite, Reasoning}` 结构提取实际消耗，在内存中更新该 Provider 的 `consumed_tokens`。不同厂商对 Coding-Plan 额度的实际扣减权重并不总是"In+Out 原样相加"——例如 Cache Write 通常按溢价计入额度、Cache Read 常按折扣或免费计入——因此扣减公式需要按 Provider 可配置权重，而不是套用统一的等权求和，否则本地估算会系统性偏离厂商的真实剩余额度。
* **有效权重计算**：
  $$\text{Effective Weight} = \max\left(0, \frac{\text{total\_tokens} - \text{consumed\_tokens}}{\text{total\_tokens}}\right)$$
* **已知限制，非本阶段解决**：
  * **单一数据源假设**：本地计数只有在该 Provider 账号的全部流量都经过这一个 VMR 实例时才准确；账号被绕过 VMR 直接使用（例如同一把 Key 被另一个客户端直连），会导致本地计数与厂商真实剩余额度静默偏离。这是选择"本地统计"这条路径（而非等待厂商开放配额 API）必须接受的代价，不是实现缺陷。
  * **`reset_day` 是本地近似，不是厂商账单周期的精确复刻**：厂商实际重置时刻的时区、小时精度未必对外公开，本地重置只保证"大致对齐"，不保证跨自然日边界的分秒级一致。
  * **不覆盖并发/会话数上限**：不少 Coding-Plan 除总量桶外还单独限制并发会话数或 RPM，Effective Weight 看不到这类限制——账号显示"额度充足"仍可能因触发并发上限被上游拒绝。这类瞬时拒绝由现有的错误分类 + 健康状态机（冷却/半开）兜底处理，不需要 Effective Weight 提前预测，但需要明确这不是同一个问题。

### 4.2 结合 Prefix-Cache 保护的双层路由算法

为了防止用量平衡算法破坏长上下文缓存，路由决策按如下两层执行：

1. **第一层：Session Affinity 强锁定（Sticky Pin）**
   * 对于已建立的 Agent 多轮会话（通过 System Prompt + 首条 User Message 的 Hash 提取指纹），只要原 Endpoint 处于健康状态（未触发 4xx/5xx/429/Block 且满足 Capabilities 要求），**强制路由至原 Endpoint**。
   * 该优先级高于任何水位相位调度，确保 Prefix-Cache 命中率达到 100%。
   * **Sticky 命中的账号 Effective Weight 降为 0（套餐用尽）时的显式策略**：只要该端点仍处于健康态，继续沿用第一层的强锁定，任由本轮会话按量计费溢出，而不是为了省钱强行切换账号——切换账号打断 Prefix-Cache 后的重算成本，在长上下文场景下通常比短暂溢出计费更贵，这与"Cache 命中率优先"的既定取舍一致，实现时必须显式保留，不能被水位调度覆盖。

2. **第二层：新会话加权分流（Quota-Aware Balanced Allocation）**
   * 当接收到全新的 Agent 会话请求时，计算候选 Endpoint/Provider 的 `Effective Weight`。
   * 采用平滑加权轮询（Smooth Weighted Round-Robin）算法分配新 Session。消耗较少、剩余额度比例较大的 Token-Plan 将获得更高的新 Session 接入概率。
   * 这一层是在既有 `strategy.Dimension`（如 `priority`）排序打平之后，同一优先级梯队内部的加权细分，而不是取代 `priority`。典型用法：把预算充足的几个 Coding-Plan 账号配成同一优先级，做加权轮询；再用更低的 `priority` 挂一个按量计费的兜底 EndpointGroup。这样当所有 Coding-Plan 的 Effective Weight 同时打平至 0 时，现有 failover 逻辑天然会往下一优先级尝试，不需要为"套餐全部用尽后去哪"另外设计溢出逻辑。

### 4.3 内部多用户可见性：复用已有 ClientKeyTag，而非另起一套

"企业同时采购多家厂商 Token-Plan，对内给员工/业务线共享调度"这类细分需求里，"共享调度"（4.1/4.2 的 Provider 级加权分流）和"按人可见性/归因"是两件事，本方案第一阶段只解决前者。路由层面无法做到、也不必做到按员工/业务线的实时限流——这需要在请求路径上引入下游身份维度的配额状态，比 Provider 级水位调度重得多，应作为独立范围评估，不与本阶段混淆。

但"按人可见性"第一阶段就能低成本交付：VMR 的 `server` 层已经把每个下游 API Key 映射为 `ClientKeyTag`（`Cfg.APIKeys` → `audit.KeyTag`），逐请求写进审计日志，`vmr report`/`vmr story` 已经能按这个 tag 分组统计。也就是说"每个员工/业务线消耗了多少、逼近哪个套餐的额度上限"这类事后可见性，直接复用现有数据契约即可交付（见 5. 路线图阶段二），不需要为此新增一套采集机制。

---

## 5. 路线图与后续实施规划

项目的实施分为两个明确阶段，保持一体两面的解耦架构：

### 阶段一：Router 侧 Token-Plan 水位感知路由与算法增强
1. 在配置解析器中增加 `budget` 配置项支持（`total_tokens`、`reset_period` 等），仅覆盖 4.1 定义的"月度/日度 Token 总量桶"型套餐。
2. 在路由引擎中接入 `chatmsg.ExtractUsage` 的本地统计器（支持按天/月时间窗口轮转，按 Provider 可配置的 Cache Read/Write 权重折算）。
3. 扩展现有的 `strategy` 路由逻辑，在 Sticky 命中之外，实现基于 `Effective Weight` 的新会话分配策略——作为 `priority` 排序打平后的同层加权，而非替代（见 4.2）。
4. "滚动时间窗口限流"型套餐（如 Claude Code Max/Pro）明确不纳入本阶段的 Token 桶模型，留作独立的限流感知机制单独评估。

### 阶段二：Analytics 侧 Agent Story 与 Audit 丰富化
1. 保持现有 [`UserGuide.md`](file:///Users/stanford/code/vmr/docs/UserGuide.md#agent-task-narratives-vmr-story) 中的 `vmr story` 与 `vmr story -compare` 分析指令独立高效。
2. 在 `vmr report` 的用量报告中追加基于 Token-Plan 本地预算的消耗进度与预测看板。
3. 复用现有 `ClientKeyTag`，在 `report`/`story` 中新增按员工/业务线的套餐消耗占比与成本归因视图（见 4.3），把"多人共享调度"的可见性需求用既有数据契约交付，不新增采集机制。
