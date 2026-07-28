# 一夜、三个供应商、零任务丢失——VMR 在凌晨的 8 个小时

> **版本**：v2（故事叙事 / 一夜纪实）
> **对标 v1**：`article-03-cn-failover.md`
> **差异**：以时间线叙事的方式还原一个真实夜晚 VMR 在底下的所有自动切换。不讲技术原理，让叙事本身传达"为什么需要这个"。
> **目标平台**：linux.do、V2EX
> **字数**：约 2200 字

---

以下时间线来自 2026-07-26 晚上到次日早晨的 VMR 审计记录。没有改编，只是把机器日志翻译成了人能读的故事。

**配置**：
```
coding:
  1. minimax/MiniMax-M3       (便宜，context 8K)
  2. deepseek/deepseek-v4-pro  (中价位，context 32K)
  3. openrouter/z-ai/glm-5.2   (备用)
```

**Agent**：OpenClaw，跑一个持续的重构任务——把一个 Go 项目从老版本依赖迁移到新版本。

---

## 23:14 —— 任务启动

Agent 开始迁移 `internal/report/` 包。一切正常，MiniMax M3 在高效运转。延迟 1.6-1.8s，每轮大约消耗 4000 token。

审计记录上，`attempts` 数组里每一条都只有 1 个元素。没有 failover 发生。Sticky Model 机制让所有请求钉在 MiniMax 上——prompt cache 持续命中。

---

## 01:37 —— 第一次切换：DeepSeek 顶上

```
"status": 503
"error_class": "transient"
```

MiniMax 突然返回了一个 503。VMR 立即标记 MiniMax 为冷却状态，请求切换到 DeepSeek。

**关键**：这次切换花了不到 1 秒。Agent 不知道发生了什么——它只是在等回复，然后等来了 DeepSeek 的回复。Sticky Model 自动把后续请求钉到了 DeepSeek 上。

DeepSeek 接过任务，继续跑。响应延迟从 1.7s 升到了 2.9s——DeepSeek 比 MiniMax 慢一点，但没人在意，因为没人在看。

---

## 02:52 —— MiniMax 悄悄恢复了

1 小时 15 分钟后，MiniMax 的冷却计时器归零。VMR 的主动探测模式（active probe）发了一个 echo-nonce 请求：

```
{"messages":[{"role":"user","content":"ping"}]}
```

MiniMax 回复正常。端点恢复健康。

但当前请求仍然留在 DeepSeek 上——Sticky Model 不会在会话中间切换回去（切换意味着丢掉 DeepSeek 的 prompt cache，让下次请求重传全部 context，反而更贵）。MiniMax 是健康的，会话亲和选择了继续留在 DeepSeek。

---

## 04:03 —— DeepSeek 也出问题了

```
"status": 429
"error_class": "rate_limit"
```

DeepSeek 返回了 429（Rate Limit），带 `Retry-After: 30`。VMR 服从 Retry-After，把 DeepSeek 标记为冷却 30 秒，切换到 OpenRouter / GLM-5.2。

GLM-5.2 接上。延迟 2.3s——和 DeepSeek 差不多。

---

## 04:05 —— DeepSeek 冷却还没到，VMR 没有等

30 秒还没到，但 Agent 又发了一条新请求。VMR 没有尝试 DeepSeek（仍在冷却中），直接走了 GLM-5.2。

如果 VMR 的冷却策略是"等冷却到期后再试同一个端点"，这条请求就会在 DeepSeek 上再失败一次——浪费一次尝试机会，Agent 端的延迟加 30 秒。

---

## 04:34 —— DeepSeek 冷却到期，主动探测通过

VMR 发了第二个 echo-nonce 到 DeepSeek。DeepSeek 回应正常。端点恢复健康。下一个请求可以走 DeepSeek。

---

## 05:18 —— MiniMax 再次出问题，但这次不一样

```
"status": 200
"response": {"choices": [{"message": {"content": "", "output_sensitive": true}}]}
"norm": ["soft_block_detected"]
```

MiniMax 返回了 HTTP 200，但 content 是空的，带了软屏蔽标记。

当前 VMR 对软屏蔽是**观测模式**——在审计里打了 `soft_block_detected`，但没有自动 failover（这是 P1 规划中的功能）。Agent 收到了那个空回复，基于它继续往下跑了一段。

这是这个晚上**唯一一次 Agent 受到了实际影响**。前面的 503 和 429 都被自动处理了——Agent 完全无感。只有软屏蔽，因为还没有自动 failover，Agent 拿到了一轮空回复。

---

## 05:22 —— Agent 从空回复中恢复

Agent 的下一轮请求，模型看到对话历史里有一条空的 assistant 消息，忽略了它，继续正常推理。没有发生"事故一"级别的严重偏航（见 `article-02-cn-softblock-v2.md`），但这是运气——上次类似的情况导致了一个 2 小时任务的最终输出产生幻觉。

---

## 06:47 —— 任务完成

Agent 完成了重构任务。提交了代码。

---

## 复盘：这个晚上实际发生了什么

从 Agent 的视角：它发了一个长任务，早上起来任务完成了。一切正常。

从 VMR 的审计记录看：

| 时间 | 事件 | Agent 是否感知 |
|---|---|---|
| 01:37 | MiniMax 503 → 切 DeepSeek | 否 |
| 02:52 | MiniMax 恢复（探测通过） | 否 |
| 04:03 | DeepSeek 429 → 切 GLM-5.2 | 否 |
| 04:34 | DeepSeek 恢复（探测通过） | 否 |
| 05:18 | MiniMax 软屏蔽 → 观测打标 | **是**——拿到空回复 |
| 05:22 | Agent 从空回复自然恢复 | 是 |

**5 次事件，1 次 Agent 感知到了。** 而且那 1 次是因为软屏蔽的自动 failover 还没有实现——一旦 P1 落地，连那一次都会被自动处理。

---

## 如果没有 VMR

如果没有路由器，Agent 直连 MiniMax：
- 01:37 的 503 → 任务中断，早上起来手动重跑
- 三个小时的 context 和进度丢失
- 整个重构任务需要重新做

如果有一个简单的重试型代理（只有"失败就重试同一个 URL"逻辑）：
- 01:37 的 503 → 不断重试 MiniMax（它还在挂）→ 超时 → 任务中断
- 04:03 的 429 → 不断重试 DeepSeek（被限流了）→ 积压 30 秒 → 可能也超时

如果是翻译型网关：
- 错误分类不区分 `ErrContent` 和 `ErrClient`
- 05:18 的软屏蔽可能根本检测不到
- 没有 Sticky Model，每次切换都丢 prompt cache，成本悄悄上升

---

**项目地址**：[github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)

```bash
brew install bigfatsea/tap/vmr
```

MIT 开源。个人工具。