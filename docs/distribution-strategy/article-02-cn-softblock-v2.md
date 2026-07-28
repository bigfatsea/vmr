# 三个真实的软屏蔽事故——以及为什么你的网关没有告诉你

> **版本**：v2（案例集 / 调查报道式）
> **对标 v1**：`article-02-cn-softblock.md`
> **差异**：不讲技术原理，直接给出三个从审计记录还原的真实事故案例，用证据驱动说服。适合"给我看事实，别给我讲道理"的读者。
> **目标平台**：linux.do
> **字数**：约 2600 字

---

以下三个事故都来自 VMR 审计记录的真实还原。供应商名字保留，因为这是生产环境中真实发生的行为，不是推测。

---

## 事故一：凌晨 2:47，MiniMax 把整个 Agent 会话送进了幻觉

**时间**：2026-07-23 02:47 CST
**虚拟模型**：`coding`
**命中端点**：`openai:minimax:MiniMax-M3`
**HTTP 状态码**：200

**审计记录关键字段**：

```json
{
  "ts": "2026-07-23T02:47:31+08:00",
  "model": "coding",
  "outcome": "ok",
  "attempts": [{
    "endpoint": "openai:minimax:MiniMax-M3",
    "status": 200,
    "norm": ["soft_block_detected"],
    "response": {
      "body": {
        "choices": [{"message": {"content": "", "output_sensitive": true}}]
      }
    }
  }]
}
```

上游返回了 HTTP 200，content 为空，带 `"output_sensitive": true` 标记。

**Agent 做了什么**：

Agent 拿到了 `content: ""` 的回复。它的下一轮请求里，`messages` 数组的最后一条是：

```json
{"role": "assistant", "content": ""}
```

模型看到上一条 assistant 消息是空的，但被要求"基于以上对话继续"。于是它自己编了一段推理：认为上一个任务步骤被用户跳过了，然后基于这个推断，跳过了整个中间步骤，直接生成了一份"最终结果"——而这份结果是基于幻觉的。

**后果**：一个跑了 2 小时 40 分钟的重构任务，最后 40 分钟的输出基于幻觉。第二天早上用户（我）不得不回滚并重跑。

**损失类型**：静默偏航。没有报错、没有告警、Agent 和网关都没有感知到异常。

**如果当时有 VMR 的自动 failover（P1 规划中）**：非流式路径下，VMR 检测到 `soft_block_detected` 后会直接切换到下一个候选端点重试，Agent 根本不会拿到那个空回复。这个事故就不会发生。

---

## 事故二：下午 3:12，DeepSeek 的 400 不是"你的请求有问题"

**时间**：2026-07-25 15:12 CST
**虚拟模型**：`coding`
**命中端点**：`openai:deepseek:deepseek-v4-pro`

**审计记录**：

```json
{
  "attempts": [{
    "endpoint": "openai:deepseek:deepseek-v4-pro",
    "status": 400,
    "error_class": "client",
    "error": "...content management policy..."
  }]
}
```

**关键细节**：DeepSeek 返回了 400 状态码，但错误 body 里的措辞与内容审核有关（`content management policy`）。VMR 的 `DefaultClassify` 检测到了 `content` + `policy` 关键词，将错误分类为 `ErrContent`——这意味着：换端点，但不惩罚 DeepSeek 的健康状态。

如果没有这个分类逻辑，一个简单的"400 = 请求有问题"的分类会把这条请求直接返回给客户端，整个 Agent 会话中断。而实际上，换到 OpenRouter 上的 GLM-5.2 后，同一个请求正常返回了。

**损失类型**：误分类导致的非必要中断。如果没有 VMR 的内容策略识别，一个本可以自动恢复的请求会中断整个 Agent 任务。

---

## 事故三：全天断断续续的 thinking 泄漏，污染了三个 Agent 长会话

**时间**：2026-07-24 全天
**供应商**：MiniMax M3
**触发条件**：thinking 模式为 `medium` 时

**审计记录特征**（多条，这里摘一条代表）：

```json
{
  "norm": ["think_strip"],
  "raw_pre_strip": " thinking\n我需要先分析用户的请求...\n（878 字节 thinking 内容）\n response\n这是最终回复..."
}
```

**原始流式事件序列（简化）**：

```
data: {"choices":[{"delta":{"content":" thinking"}}]}
data: {"choices":[{"delta":{"content":"我需要分析用户请求中的三个关键要素..."}}]}
data: {"choices":[{"delta":{"content":"1. **文件路径解析**——需要确认..."}}]}
data: {"choices":[{"delta":{"content":"2. **权限检查**——当前用户可能没有..."}}]}
data: {"choices":[{"delta":{"content":" response"}}]}
data: {"choices":[{"delta":{"content":"根据您的请求，我将为您..."}}]}
```

**VMR 做了什么**：

`norm: ["think_strip"]` 表明 VMR 检测到了 ` thinking... response` 标签包裹的 thinking 块，并且在转发给客户端之前剥掉了。Agent 收到的 SSE 流直接从 `"根据您的请求，我将为您..."` 开始。

`raw_pre_strip` 字段保留了被剥掉的内容，你可以事后查看——确认被剥掉的是什么。

**如果没有 think_strip 会怎样**：

thinking 内容——包括模型对"当前用户可能没有权限"的推测——会作为正常的 assistant 回复进入对话历史。下一轮 Agent 会基于"模型说我没权限"这个误判继续推理。三个长会话都出现了不同程度的推理偏航。

**损失类型**：上下文污染。不是某一条请求失败，而是整个对话历史被混入了不可靠的推理草稿。

---

## 这三个事故的共同点

1. **都是"成功"的响应**——HTTP 200 或 4xx 但不是 5xx。大多数监控系统只看 5xx 比率，这三个全部漏掉。

2. **翻译型网关看不到**——
   - 事故一：`output_sensitive` 是 MiniMax 的私有字段，翻译型网关在归一化时会丢掉
   - 事故二：区分 `ErrContent` 和 `ErrClient` 需要读 error body 的文本内容，翻译型网关只看状态码
   - 事故三：thinking 泄漏需要理解"响应的第一个 content 值是不是以 ` thinking` 开头"——这是字节级的内容感知，翻译型网关做的是结构转换

3. **Agent 比人脆弱得多**——这三件事如果发生在人手动用 ChatGPT 时，人看一眼就知道出问题了。Agent 不会。

---

## VMR 目前做到的，和还没做到的

**已做到**：
- 事故一：观测打标（`soft_block_detected`），事后可查
- 事故二：`ErrContent` vs `ErrClient` 的正确区分 + 不惩罚端点的 failover
- 事故三：thinking 剥离 + `raw_pre_strip` 留底可查

**还没做到（规划中）**：
- 事故一：软屏蔽自动 failover（P1——非流式路径）
- 事故二：context 超限的独立识别和 failover（P1）
- 事故三：守卫失效时的主动告警（`thinking_process_pattern_detected` 已打标，但还没有通知机制）

---

**项目地址**：[github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)

```bash
brew install bigfatsea/tap/vmr
```

MIT 开源。个人工具。