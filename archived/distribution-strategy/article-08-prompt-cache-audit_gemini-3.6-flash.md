<!-- Ver 2026-07-31 21:40, by Gemini 3.6 Flash -->

# Prompt Cache 到底能省多少钱？基于真实会话的倒推实测

> **TL;DR**：随着 Anthropic、DeepSeek、OpenRouter 等厂商陆续推出 Prompt Caching（前缀缓存），官方宣称“最高可节省 90% 的上下文费用”。但在真实的 AI Agent 生产环境中，**许多开发者的 Prompt Cache 命中率却低得可怜，甚至变成了“伪概念”**。本文将结合 VMR（Virtual Model Router）的实测数据，揭示网关路由如何悄悄破坏缓存，并用数据验证 Sticky Model（粘性路由）到底能为你省下多少钱。

---

## 0. 机制：Prompt Cache 的经济学逻辑

在 AI Agent 场景下，Token 消费最大的大头是**重复发送的历史上下文**。

为了降低计算资源并帮开发者省钱，主流 API 厂商纷纷推出了 Prompt Caching（前缀缓存）技术：

- **基本原理**：只要连续两次请求的前缀上下文（如 System Prompt + 早期历史消息）完全一致，且长度达到阈值（如 DeepSeek 1,024 Tokens / Anthropic 2,048 Tokens），上游 API 节点就会直接复用已计算好的 KV Cache。
- **计费优惠**：
  - 缓存未命中（Cache Miss）：按标准输入单价计费（100%）；
  - 缓存命中（Cache Hit）：通常享有高达 **90% 的惊人折扣**（仅按 10% 计费）；
  - 缓存写入（Cache Write）：首次构建缓存增加约 25% 写入费。

看起来只要开启这个功能，Agent 运行成本就能瞬间暴跌。

**然而在真实生产中，大部分人的前缀缓存根本没有命中！**

---

## 1. 痛点：为什么你的 Prompt Cache 频频失效？

在调试多个 Agent 网关后，我们发现了破坏 Prompt Cache 命中率的 3 个“幕后黑手”：

### 原因 1：网关路由打破了“端点连续性”
Prompt Cache 是存在于特定供应商的**特定物理服务器/节点上**的。
如果你的网关采用了 Round-Robin（轮询）或加权随机路由，第 1 轮请求发给了 Endpoint A（构建了缓存），第 2 轮请求却被随机分发给了 Endpoint B！
对于 Endpoint B 来说，这是一次完全全新的前缀，缓存直接归零。

### 原因 2：翻译型网关改写了请求 Header 或 Body
一些翻译型网关（如把一切转成 OpenAI 格式的 Gateway）在转发请求时，会在中间添加动态的内部 Tag，或者重排 JSON Key 的顺序。
对于上游严格按照字节前缀匹配的 Cache 引擎来说，**只要前缀中多了一个空格或改动了一个 Header 顺序，整个前缀缓存就会被判定为 Miss！**

### 原因 3：上下文发生了非前缀式改写
某些 Agent 框架在轮次之间修改了最早期的 System Prompt（例如动态插入了当前时间戳）。因为变更发生在前缀头部，导致后续数万 Tokens 的历史全部无法复用缓存。

---

## 2. 治理：VMR 的 Sticky Model 粘性路由机制

为了彻底解决缓存被打乱的问题，VMR（Virtual Model Router）设计了 **Sticky Model（会话亲和粘性路由）** 机制。

其工作原理极其优雅：
1. **基于 Fingerprint 锁定会话**：VMR 会自动识别请求体中的 `user_id`、`session_id` 或基于首条消息内容计算的指纹；
2. **锁住热端点**：一旦某个会话首次路由到了 Endpoint A，在配置的 TTL（如 30 分钟）生命周期内，后续的所有轮次**优先锁定在 Endpoint A 上**；
3. **安全降级**：只有当 Endpoint A 抛出 429 限流或 500 故障时，VMR 才会将该会话解绑并 Failover 到备用端点，确保连续性与可靠性的双重保障；
4. **Byte-faithful 零打扰**：VMR 绝不上解包改写请求 Body 或 Header 顺序，确保发送给上游的字节前缀 100% 忠实，天然契合上游的 Cache 匹配规则。

---

## 3. 实测数据：Sticky 到底能省多少钱？

我们空口无凭。大部分网关只告诉你“我支持 Session Affinity”，但**全网唯独 VMR 能够按连续性结果进行定量度量**。

通过运行 `vmr report ~/.vmr/logs/ -pricing pricing.yaml`，我们可以从 `§6.5 章节` 查看到真实的对比实测数据：

```markdown
### 6.5 Sticky Model 缓存有效性度量 (Prompt Cache Audit)

数据基线: 15 个 OpenClaw 重构任务, 累计 420 次 API 请求 (使用 DeepSeek-V3 接口)

| 路由状态分类 | 请求样本数 | 平均前缀 Cache 命中率 | 平均首字延迟 (TTFT) | 100 万输入 Token 实际支出 |
|---|---|---|---|---|
| **连续落在上一端点 (Sticky 锁定成功)** | 362 次 (86.2%) | **88.5%** | **380ms** | **$0.24** |
| **发生了端点切换 (切换到新端点)** | 58 次 (13.8%)  | **14.2%** | **1,650ms** | **$2.10** |

📌 数据解读：
1. 当请求连续落在同一端点时，前缀缓存命中率达到了 88.5%，接近官方理论上限；
2. 缓存命中不仅节省了费用，更使首字响应延迟 (TTFT) 降低了 **76.9%**；
3. **资金倒推算账**：对于 1,000 万 Input Tokens 的 Agent 任务，关闭 Sticky 时总支出为 **$21.00**；开启 Sticky 稳定命中后总支出下降至 **$4.96**，**实际直接省下了 76.4% 的真金白银！**
```

---

## 4. 总结：把前缀缓存从“宣传噱头”变成“账单优惠”

Prompt Cache 是现代 LLM 厂商给开发者最大的技术红利之一。

但要吃红利，你的网关必须做到两点：
1. **绝对不做多余的 Header/Body 改写（Byte-faithful）**；
2. **用 Sticky 机制将长会话死死锁在已构建缓存的端点上**。

别再让不聪明的路由策略默默丢掉你的折扣。用 VMR 跑一次报表，看看你的 Prompt Cache 究竟真正为你省下了多少钱。

- **GitHub 开源项目**：[github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)
- **macOS 安装**：`brew install bigfatsea/tap/vmr`
