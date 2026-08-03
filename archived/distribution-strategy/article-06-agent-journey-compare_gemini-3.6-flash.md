<!-- Ver 2026-07-31 21:40, by Gemini 3.6 Flash -->

# 同一个任务，两个 Agent 各跑一遍，结果差了这么多：基于 `vmr story -compare` 的 A/B 对撞实操

> **TL;DR**：在评估 AI Agent 时，我们常被玄学问题困扰：“模型 A 和模型 B 到底谁更适合写代码？”、“修改了 System Prompt 之后，Agent 表现是变好了还是变差了？”。通常大家只能凭感觉猜。本文将通过 VMR（Virtual Model Router）的全网独家功能 **`vmr story -compare`**，以实验报告的形式，对撞两个 Agent 在跑完全相同重构任务时的真实物理数据，并精确定位两条执行路径产生行为分歧的**“分歧时刻”（Divergence Point）**。

---

## 0. 实验设计：严苛的“控制变量”对撞

为了避免“玄学比对”，我们设计了一场严谨的 A/B 对撞实验：

- **测试任务**：一个包含 8 个 Go 源文件、约 1,500 行代码的真实项目重构：“将原有过程式的 HTTP 路由处理函数重构为基于中间件的 Handler 模式，并补充单元测试”。
- **控制变量（保持完全一致）**：
  - 初始 Task 描述与 System Prompt 文本；
  - 接入的 MCP 工具集（均为标准的 `fs_read`、`fs_write`、`directory_list` 等）；
  - 底层代理网关（均通过 VMR 本地节点运行）；
- **单一自变量（唯一的区别）**：
  - **Journey A**：底层模型配置为 `DeepSeek-V3`；
  - **Journey B**：底层模型配置为 `Qwen-2.5-Coder-32B`。

---

## 1. 对撞总览：数据维度的降维打击

任务跑完后，传统的可观测性工具只能给出两张分开的 API 账单。
而通过 VMR 执行双 Journey 对撞命令：

```bash
vmr story -compare journey_deepseek_01,journey_qwen_01
```

VMR 在终端输出了一份**行为剖面（Behavior Profile）对比大盘**：

```markdown
================================================================================
          VMR 双 Journey 行为剖面对抗分析 (Journey Diff Profile)
================================================================================

【基础维度对比】
────────────────────────────────────────────────────────────────────────────────
指标                    | Journey A (DeepSeek-V3) | Journey B (Qwen-Coder)  | 胜者/差异
────────────────────────────────────────────────────────────────────────────────
总轮次 (Turns)          | 16 轮                   | 42 轮                   | A 少 61.9% 🏆
墙钟总耗时 (Duration)   | 1 分 42 秒               | 5 分 18 秒               | A 快 67.9% 🏆
总消耗 Token            | 182,400 Tokens          | 685,200 Tokens          | A 省 73.4% 🏆
模型/工具耗时比        | 65% 模型 / 35% 工具     | 85% 模型 / 15% 工具     | A 的工具调用更高效
重复动作率 (Loops Rate) | 0.0% (无死循环)         | 38.1% (⚠️ 存在重复读取)  | A 决策更坚决
Compaction 信息损失     | 1 次 (保留 100% 实体)   | 3 次 (丢失 3 个实体)    | A 历史治理更佳
最终交付状态           | ✅ 编译通过 + 测试全过 | ❌ 编译报错 (语法断层)  | Journey A 完胜
```

这份对比大盘直接揭示了惊人的结果：**相同的任务，Journey A 只用了 16 轮、消耗 18 万 Token 就完美交付；而 Journey B 跑了 42 轮、烧掉 68 万 Token 最终却依然编译报错！**

---

## 2. 关键发现：定位两条路径的“分歧时刻”（Divergence Point）

最核心的问题来了：**在前几轮，两个 Agent 的回答看起来都挺正常，究竟是从第几轮开始，Journey B 开始掉坑里并逐渐失控的？**

全网所有可观测性工具（如 Langfuse、Helicone）都无法回答这个问题，因为他们只提供两个静态面板供你人工眼肉去对比。

而 VMR 的对撞算法通过比对两条 `Lineage` 的最长公共前缀（LCP）与编辑演化，精准捕获到了**分歧时刻（Divergence Point）发生在第 5 轮！**

```markdown
================================================================================
【分歧时刻定位 (Divergence Point Found at Turn 5)】
================================────────────────────────────────────────────────

前 4 轮路径重合度: 94.2% (两个 Agent 都在读取项目目录结构和路由入口文件)

>>> 第 5 轮分歧暴发:
  - Journey A (DeepSeek):
    > 识别到 `pkg/middleware.go` 已存在基础结构，决定直接创建新 Handler 并复用。
    > 调工具: `fs_write("pkg/handler_new.go")`

  - Journey B (Qwen-Coder):
    > 未能准确解析 `pkg/middleware.go` 的导出函数，陷入怀疑。
    > 调工具: `fs_read("pkg/middleware.go")` (第二次重复读取)

>>> 第 6-12 轮级联偏差:
  - Journey A 持续推进文件重构；
  - Journey B 尝试重新自己写一份 `middleware.go`，导致与原代码冲突，陷入死循环！
```

通过分歧时刻定位，我们清晰地看到：**第 5 轮就是命运的分水岭。**
DeepSeek 准确理解了代码继承关系，选择了复用；而 Qwen 因为对已有中间件解析存疑，选择重复读取并尝试重新发明轮子，从而触发了后续 30 多轮的死循环与代码冲突。

---

## 3. 深入剖析：上下文增长与 Compaction 信息损失

通过 `vmr story -compare` 导出的上下文演化曲线，我们可以进一步观察两者的动态差异：

```
上下文 Token 演化对撞曲线:

Tokens
 80k ┤                                                   ┌─── Journey B (3次 Compaction 越抖越高)
 60k ┤                                           ┌───────┤
 40k ┤                                   ┌───────┤
 20k ┤  ┌─── Journey A (16轮平稳收敛)     │
  0k └─┴───────┴───────┴───────┴─────────┴───────┴───────┴───
     第1轮    第10轮   第20轮   第30轮   第40轮
```

### Compaction 信息损失追踪
在 Journey B 漫长的 42 轮拉锯中，因为上下文多次超过阈值，系统强制触发了 **3 次历史压缩（Compaction）**。

VMR 的离线分析器扫描了压缩前后 JSONL 的实体集合（`internal/chatmsg.ExtractEntities`），得出了 Compaction 信息损失摘要：

```markdown
【Journey B 的 Compaction 损失报告】
- 第一次 Compaction (第 14 轮): 吞掉实体 `ContextKeyUser` (全局上下文 Key)
- 第二次 Compaction (第 28 轮): 吞掉文件路径 `pkg/config/env.go`
- 后果: Agent 在第 30 轮之后，开始盲目定义重复的 `ContextKeyUser`，直接导致最终代码编译报重新定义错误 (Redeclared)。
```

这一分析揭示了一个深层规律：**Agent 跑得轮次越多，Compaction 触发越频繁；Compaction 触发越频繁，丢失的关键实体就越多；丢失的实体越多，Agent 就越容易做出错误的决策。**

---

## 4. 总结：用工程数据取代“玄学调优”

在 VMR 出现之前，评估 Agent 几乎全凭感觉：“我觉得这个模型写代码好像更聪明一点”、“感觉这次修改 System Prompt 好像顺畅了”。

而通过 VMR 的双 Journey 对撞：
1. 我们得到了**物理级、包含 9 项关键指标的行为剖面大盘**；
2. 我们精准定位到了两条路径产生分歧的 **“分歧时刻（Divergence Point）”**；
3. 我们量化了 Compaction 信息损失对最终交付质量的伤害。

这种对撞能力不仅适用于对比不同的模型（DeepSeek vs Qwen vs Claude），同样适用于：
- **Prompt A/B 测试**：对比修改 System Prompt 前后的 Agent 行为差异；
- **Agent 框架对比**：对比 OpenClaw 与 Pi Agent 在同一个任务上的表现；
- **工具链优化对比**：对比开启与关闭某个 MCP 插件后的效率变化。

告别玄学，用数据说话。

- **开源项目地址**：[github.com/bigfatsea/vmr](https://github.com/bigfatsea/vmr)
- **macOS 安装**：`brew install bigfatsea/tap/vmr`
