// Ver 2026-07-30 23:00, by Sonnet 5

# Agent 任务叙事报告（`vmr story`）—— 第一性原理推导、价值论证与渐进实现方案

> **v1.3**（继承 v1.2/v1.1/v2.0 的全部设计推导；v1.0→v2.0 修订清单见附录 B）。
> 本次修订：**4a LLM 解读层已实施，但范围是 4d（双 Journey 对比）场景的一个切片，不是完整的单 Journey 逐 Step 解读**——用户明确要求丰富 compare 报告后落地，实施前先评审了另一份 coding agent 给出的建议方案（`docs/Step4a_LLM增强对比报告_实施计划.md`，内联批注了逐项吸收/拒绝理由），吸收了其中"规则层能做的事更多"的判断，拒绝了它把"system prompt 文件名/工具清单识别"和"迭代阶段划分"做成正则/启发式规则层的建议（改为交给 LLM 解读，理由见该文档批注与附录 G 补记）。详见附录 G 的落地记录与 `docs/Step4a_compare_LLM解读层_实施计划与执行记录_2026-07-29_sonnet-5.md`。
> 前序修订：**D1（断头 journey 自证）已实施**——采用文件名 `-partial` 后缀而非原计划的 ID `t-` 前缀（见 §11、附录 F.1，理由是后缀不改变 `deriveID` 本身、改动面更小）；**第 3 步（"能还债"）已完成**，附录 E 的重新评估计划基本按原样落地，唯一实质性偏差是 compaction 缝合的信号来源（附录 F.2 有诚实记录）；附录 D/E 的状态总览更新为"已完成"；**第 4 步计划基于第 3 步的实际发现做了微调**（附录 G）。v2.0 原文（`Agent任务叙事报告_设计与价值论证_2026-07-28_opus-5.md`）保留完整的逐条评审过程记录，不在本文重复。
> 关联：`CCR_特性借鉴分析报告_opus-5.md` §2.4（Context Archive）、§5 N-4（Compaction 感知与告警）
> 证据源：`reports/`（7112 条记录 / 15 天 / 752 会话 / 809K 条消息实例）+ `internal/report/{session,render,aggregate,metrics,rows}.go` + 设计文档 §9
> 方法：所有结论都在真实审计数据上跑过验证脚本，附录 A 给出可复现的实测数字

---

## 0. Debrief

需求是**一个新的产物类别**，不是 `vmr-report.md` 的又一节：把一次 Agent 任务的完整执行过程还原成可读的叙事流，每一步既有压缩后的解读，又能展开原始证据。

三层动机，重要性递增：

1. 看懂单个 Agent 怎么干活——上下文怎么组织、工具怎么用、哪一步开始跑偏；
2. 横向对比不同 Agent 系统（OpenClaw / Lobster 内嵌版 / Hermes / Pi）在同一任务上的行为差异；
3. 把 `docs/openclaw_dual_instance_analysis_2026-07-28_v1.0_deepseek-v4-pro.md` 那种手工分析产品化。

三个技术障碍：任务串联的稳定性、compaction 还原、subagent（本期暂缓，设计留位置）。

已拍板前提：设计文档先行；报告以 Journey 为文件、内部按 task 分章；LLM 复用 `config.yaml` 已有 endpoint 直连上游；通用内核 + agent 特化插件。

---

## 1. 结论先行

**做。但 v1.0 的方案结构是错的，本版给出修正后的最优解和渐进落地路径。**

一句话的第一性原理结论：

> **VMR 手里的不是"日志"，而是同一份对话状态在时间轴上的完整快照序列。所有关于 Agent 行为的问题，本质都是对这个序列做差分。因此正确的底层抽象不是"请求记录 + 启发式分组"，而是内容寻址的消息池 + 每请求一份清单（manifest）+ 清单之间的编辑关系图——这是 git 的模型。会话、任务、compaction、上下文生命周期、subagent，全部退化成这张图上的查询，而不是各自独立的启发式规则。**

而现有实现里，这张图的核心数据结构**已经存在**——`ReqInfo.keys`（每条非 system 消息的内容 hash 向量）——但它被当成 LCP 选父的中间变量，用完即弃。**技术债的确切位置就在这里：最有价值的数据结构被降级成了一个私有字段。**

因此方案是：把 manifest 提升为一等公民（`internal/ctxgraph`），`story` 与 `report` 都成为它的视图；分三阶段渐进偿还，每阶段独立可交付，并用一致性测试防止分叉。

**Effort 约 20.5 人天**（v1.0 估的 14.5 偏低，见 §8 的诚实重估），其中前 11.5 天构成完整闭环、不含任何 LLM 成本，剩下 9 天是四个互不依赖的可选模块。**第一步只要 3.5 天**就能出可读产物——实测 95.86% 的上下文编辑是纯追加（附录 A.7），所以"切分"能独立交付，"缝合"才是贵的那部分。可执行的分步计划见附录 C；第 1、2、3 步的实际完成情况分见附录 D、F（前 11.5 天已全部交付）。

---

## 2. 第一性原理：这个问题的本质是什么

### 2.1 假设只有原始日志，从零开始该怎么做

抛开 VMR 现有代码，只给定"一堆按时间排序的完整请求体"，问：要回答"这个 Agent 是怎么解决这个问题的"，最小充分表示是什么？

推导链：

1. **观测的原子事实**是：`(时刻 t, 完整对话状态 S(t), 上游响应 R(t))`。VMR 的审计记录**恰好就是这个三元组**，一条不多一条不少。
2. **对话状态 S 是一个有序的消息列表**。消息是不可变的值——同样的内容就是同一条消息，无论它出现在第几次请求的第几个位置。因此消息应当**内容寻址**。
3. 于是 `S(t)` 退化为一个**哈希向量**（manifest）：`M(t) = [h₀, h₁, …, hₙ]`。整个语料就是一个 manifest 的时间序列。
4. Agent 每轮重发完整历史，所以相邻 manifest 高度相似。**Agent 对自己历史做的每一个动作，都表现为 `M(t) → M(t+1)` 的一次编辑。**
5. 因此：**理解 Agent = 对 manifest 序列做差分并给每次编辑分类。**

这就是 git：blob（消息）+ tree/manifest（对话状态）+ commit 图（编辑关系）。这个类比不是修辞，它直接给出了实现。

### 2.2 编辑类型学：五种编辑，覆盖全部现象

给定同一 lineage 上相邻的 `M(t)` 与 `M(t+1)`，用 LCP + 哈希集合运算即可分类（全部 O(n)）：

| 编辑 | 判据 | 语义 |
| --- | --- | --- |
| `append` | `M(t)` 是 `M(t+1)` 的前缀 | 正常推进：新的 assistant / tool_result 追加 |
| `replace_tail` | 有公共前缀 p，尾部发散，长度相近 | 临时尾替换、重试、ephemeral message |
| `splice` | `M(t+1) = M(t)[:k] + [新 blob] + M(t)[j:]` | **原地替换**：中段被改写（见 F11 的 S2 型 compaction） |
| `contract` | `\|M(t+1)\| ≪ \|M(t)\|` 且内容基本是 `M(t)` 的子集 | 截断 / 重置 / 外挂式 compaction 后的重建 |
| `fork` | 重叠率低于阈值 | 新 lineage 起点 |

**关键推论：compaction 不该靠识别模板来检测，而该靠检测"非追加型编辑"来检测。** 模板只是事后分类的补充证据。这一条让整套机制天然框架无关——不认识 OpenClaw 的模板也照样能发现"这里历史被动过手术了"。v1.0 把这个因果关系搞反了（见附录 B 修订 2）。

### 2.3 所有概念都是这张图上的查询

| 概念 | 在图上的定义 |
| --- | --- |
| Lineage（连续片段） | 由 `append` / `replace_tail` 边连成的链 |
| Journey（逻辑任务） | 跨 `splice` / `contract` / `fork` 缝合后的 lineage 路径 |
| Task | Journey 上"新增真实用户指令"事件切出的区间 |
| Event | 一个 blob 在该 Journey 上的首次出现 |
| 上下文生命周期 | 一个 blob 出现在哪些 manifest 里 → 首见 step、末见 step、消失原因 |
| Compaction | 一次 `splice` 或 `contract` 编辑 + （若有）对应的 summarization 请求 |
| 信息损失 | 被编辑掉的 blob 集合 vs 新 blob 的 token 差 + 实体覆盖率 |
| Subagent | 另一个 lineage，其根 manifest 与主 lineage 共享 system blob，且其输出 blob 出现在主 lineage 后续某个 tool_result blob 里 |

**七个原本各自需要一套启发式的问题，坍缩成同一张图上的七个查询。** 这是判断这个抽象是否正确的标准，也是它相对 v1.0（每个问题一套规则）的根本优势。

### 2.4 为什么 VMR 不需要数据库，而 CCR 需要

CCR 的 Context Archive 用了 SQLite + `generation` 版本链 + `parent_archive_id` + 保留策略，`context-archive.ts` 840 行 + `store.ts` + `protocol.ts` 798 行。

差别不在工程能力，在**时机**：

- **CCR 在写入时归档** → 必须有一个独立的存储层，于是需要数据库、版本链、保留策略、权限管理；
- **VMR 在读取时重建** → 审计日志本身就是内容寻址存储（全量原始 body 已经在盘上，且不可变、按时间有序），只需要一个**索引**。

所以对 VMR 来说，"归档"是冗余的——它早就无损保存了全部内容，只是没建索引。这直接兑现了 CCR 报告里 N-4 的判断（"VMR 不需要也不应该去修复 compaction，但它可以成为唯一能告诉你 compaction 发生了什么的工具"），而且比 N-4 设想的观测版更进一步：不只是告警，是**完整还原**。

**实测可行性**（附录 A.5）：7112 条记录 / 809K 条消息实例。索引只存 hash + 位置，不存内容：

- manifest 总计 809K × 16B ≈ **13MB**
- blob 位置索引（去重后约 10–20 万条）× 48B ≈ **10MB**

内容按需从审计文件回捞。**zstd 不可随机寻址**，所以回捞必须是"按文件批量顺序扫描"——这恰好就是 `Build` 现在的两遍读取模式，不需要新机制。

### 2.5 技术债的确切位置

`session.go:403` 已经在算 manifest：

```go
b, _ := json.Marshal(raw)
r.keys = append(r.keys, fmt.Sprintf("%x", md5.Sum(b)))
```

这个 `keys` 字段是**非导出的、用完即弃的中间变量**，唯一用途是 `attach()` 里的 `lcp()` 选父。于是：

- 想知道"这条消息什么时候消失的" → 没有索引，做不到；
- 想知道"这次 compaction 吞掉了什么" → 只能拿 200 字节文本 needle 去猜（于是有了那 8 条告警）；
- 想知道"session 中途是不是被重置过" → `context_growth = last/first tokens_in` 得到 ×179.5 这种脏值（§7 自己都标了"中途 compaction"）；
- 想做 story → 只能重头再写一套。

**同一个数据结构，被四个功能各自绕过。** 这就是要还的债。

---

## 3. 应用场景 review：它到底用在哪

### 3.1 四个真实场景

| 场景 | 提问 | 需要哪一层 |
| --- | --- | --- |
| **S-A 单 Agent 调优** | 我的 prompt/工具集改了之后，Agent 的行为变了吗？ | 剖面层（指标对比）为主 |
| **S-B 跨框架对比** | Lobster 版比 Standalone 版好在哪？ | 剖面层为主 + 解读层点睛 |
| **S-C 事故复盘** | 这次任务为什么跑偏/为什么花了 3 倍时间？ | 事实层（事件流）+ 解读层 |
| **S-D 上下文工程** | 我的上下文里有多少是浪费的？哪些内容进来之后从没被用到？ | 生命周期视图（事实层） |

S-D 是 v1.0 完全没提的场景，但它可能是**最实用**的一个：上下文生命周期直接回答"哪条 tool_result 进来 5 万 token，之后 40 轮再没被引用，最后被压缩吞掉"——这是可执行的优化建议，不是观察。

### 3.2 从那份 deepseek 报告反推：证据其实来自规则层

这是本轮 review 最重要的自我修正。逐条拆 `openclaw_dual_instance_analysis` 的证据构成：

| 该报告的论据 | 来源 |
| --- | --- |
| 58 轮 vs 22 轮、16 分 vs 7.5 分 | 规则可得 |
| 124 msgs vs 57 msgs、末轮 token 按角色分布 | 规则可得（`roleTokens` 已实现） |
| 缓存命中率逐轮曲线 18%→97% vs 82%→99% | 规则可得（已在 §1 实现） |
| system prompt 20.5K vs 17.1K、工具 67 vs 46 | 规则可得（`ToolShapes` 已实现） |
| 工具调用分布、迭代阶段划分 | 规则可得 |
| "MEMORY.md 是单一最大根因" | **人做的推断**，不是 LLM 做的 |

**结论：那份报告 90% 的证据是规则可得的表格，LLM 在其中承担的是文字组织，而不是发现。** 而它最有价值的那个结论（MEMORY.md 是根因）是人在看到表格差异后推断出来的。

由此推翻 v1.0 的一个结构性判断：**LLM 解读层不是核心价值，规则派生的"行为剖面"才是。** 而剖面层是免费的、确定性的、可复现的、跨框架可比的。它必须进 Phase A，不能像 v1.0 那样根本没出现。

同时这也解释了为什么 P4（双 Journey 对比）可以大幅提前、大幅降本——两份剖面做差就是对比报告的骨架。

### 3.3 因此产物是三层，不是两层

```
① 事实层（规则，零成本，确定性）
   事件流 + 上下文生命周期 + 时间三分解 + compaction 还原与信息损失
② 剖面层（规则派生指标，零成本，跨框架可比）        ← v1.0 遗漏
   工具分布 / 重复动作率 / 错误恢复次数 / 计划-执行比 / 上下文构成演化 / 净工作时长
③ 解读层（LLM，可选，可降级）
   每 Step 的意图-动作-结果注解 + 全局总述
```

**层间约束**：② 只能由 ① 派生，③ 只能引用 ①②，且不得生成任何数字。缺 ③ 时报告完整可用，只是没有散文。

---

## 4. 实测核查

以下每条都在真实数据上验证过，脚本见附录 A。F1–F8 来自 v1.0（F6 有重要补充），F9–F12 是本轮新增。

### F1 — 只读末轮会丢掉 26%–99% 的内容

6 个真实会话，"全部请求去重合并"与"末轮消息集合"之差：

| 会话 | 请求数 | 去重消息 | 末轮消息 | 丢失字符占比 |
| --- | ---: | ---: | ---: | ---: |
| s231（OpenClaw，24 轮） | 24 | 86 | 8 | **81.4%** |
| s290（Pi，跨 5 天） | 444 | 1034 | 5 | **99.2%** |
| s187 | 254 | 562 | 394 | **49.3%** |
| s407 | 342 | 743 | 682 | **26.1%** |
| s634 | 251 | 483 | 222 | **80.8%** |
| s217 | 366 | 1303 | 449 | **81.7%** |

"没有压缩时末轮就是完整上下文"这个假设在真实数据上是错的。

### F2 — Compaction 缝合用消息 hash，不用文本 needle

现状：`linkCompactions()` 拿 session 首条消息前 200 字节，去 substring 匹配 compaction 请求里那段**渲染过的** transcript。失败根因：OpenClaw 首条消息带 `[Thu … GMT+8] [message_id: om_x1…]` 前缀 + envelope JSON，transcript 里渲染成 `[User]: 就你那个skill…`，永远对不上；`stripBracketPrefix` 只剥第一个方括号。

实测（`20260716-155251.355`，60 条消息，#1 为 summary，取 `[2:]` 的 58 条 hash 与当日更早 581 条请求求交）：

```
match 42/58  20260716-152238.775   <- s231，唯一强峰
match 40/58  20260716-152233.327
match 38/58  20260716-152229.810
match 36/58  20260716-152226.699   <- 同会话更早轮次，单调衰减
```

排序干净、峰值明确、零文本启发式，与现有文本 needle 得到的 `s238 ← s231` 结论一致。**第 2 步已实现（`ctxgraph.StitchGraph`），第 3 步会替换掉 `report` 里那套旧的文本 needle（见附录 E）。**

### F3 — Compaction 请求本身不是可靠历史来源，只是锚点

`20260716-155115.506`：body 仅 17.5KB / 6227 token，输入是一段被大幅截断的渲染 transcript（工具结果基本砍光），而它压缩的历史实际有几十 K token。原始历史必须回到 pre-compaction 的最后一轮请求取。

### F4 — "successor 找不到"有时是对的

那条 compaction 的输出（`## Goal\nExtract an **open, unconstrained task brief**…`）在全部 7112 条记录里**只出现在它自己那一条**——结果被丢弃了。缝合逻辑必须区分**已缝合 / 确认无后继 / 匹配失败**三态，不能把后两者混成一条日志。**第 2 步已实现为 `StitchOutcome` 的三态（`Stitched`/`NoPredecessorFound`/`AmbiguousMatch`）。**

### F5 — 模板标记必须锚定位置和角色

实测反例：`20260716-031913.090` 命中 `The conversation history before this point was compacted`，但它在**第 120 条 tool 消息**里——Pi agent 正在读 VMR 自己的 `session.go` 源码。规则必须收紧为"首条非 system 消息 + role=user + 位于文本开头"。

### F6 — session 粒度 ≠ 任务，且 anchor 会把两段 lineage 粘在一起

752 个 session 里 `s290` = 444 请求 / 32 任务 / 跨 5 天，末轮只剩 5 条消息。

**本轮新增的更严重发现**：`s231` 内部有一次隐藏的硬断裂。逐轮 manifest 演化（附录 A.3）：

```
turn 1-20:  msgs 3 → 79，LCP 完美跟随，cover 0.60 → 0.97   （纯 append）
turn 21:    msgs 79 → 4，LCP=0，cover=0.25                  <== 断裂
turn 22-24: 重新增长
```

也就是说 `s231` 是**两段 lineage 被同一个 anchor 粘在一起**（第一条非 system 消息——原始用户指令——被 compaction 保留了下来，所以 `SessKey` 不变）。

**所以缝合不只要合并，还要会切分。** v1.0 只设计了合并（journey 缝合），漏了切分。这是 anchor 分组的固有缺陷：它对"历史被换掉但开头没换"完全失明。manifest 编辑类型学天然覆盖这一点——那是一次 `contract` 编辑。**第 1 步已实现切分，第 2 步已实现缝合（s231 精确复现，见附录 D）。**

### F7 — 架构硬约束

`archtest` 禁止 `internal/report` 依赖 `router` / `server` / `config`；`render_doc.go` 400 行、`aggregate.go` 1000 行有预算，"新增一节 = 新增一个文件"。本功能不能进 `report` 包。

### F8 — 非 OpenClaw 的 compaction 形态尚未采样

`compaction` 工作负载只有 9 条，形态全是 OpenClaw 的外挂式。Pi 的重置机制不同（F6/F11）；Anthropic 入口仅 59 条请求，Claude Code 的 `/compact` 无法验证。

### F9（新）— 因果边由协议给出，不需要推断

实测 `20260716-152238.775`（79 条消息）：**57 个 `tool_calls` ↔ 57 个 `tool_call_id`，零孤儿。**

这意味着一个 Step 内部的因果结构（哪个 tool_call 产生了哪个 tool_result）是**协议给定的精确事实**，不是启发式。Anthropic 侧同理（`tool_use` / `tool_use_id`，`render.go:156-165` 已经在解析）。

推论：叙事流里"Agent 做了什么 → 得到了什么"这条最核心的边是**零误差**的。需要启发式的只有跨 Step 的 lineage 关系。这大幅收窄了不确定性的作用域。**第 2 步已把这条断言落地为自动化不变量测试，全语料 406534/406534 配对，零孤儿。**

### F10（新）— 墙钟时间可三分解，且这是 VMR 独有的度量

`ts(N+1) − (ts(N) + dur_ms(N))` 就是 Agent 侧的间隙：工具执行 + 客户端处理 + 人类思考。实测：

| 会话 | 轮次 | 墙钟 | 模型时间 | Agent 侧间隙 | 间隙 p50 / p95 / max |
| --- | ---: | ---: | ---: | ---: | --- |
| s231 | 24 | 2069.9s | 381.0s (18.4%) | 88.0s (4.3%) | 4.2s / 10.2s / 10.4s |
| s187 | 254 | 56665.8s | 5090.4s (9.0%) | 783.4s (1.4%) | 0.2s / 4.0s / 296.0s |
| s634 | 251 | 74720.5s | 4031.2s (5.4%) | 822.7s (1.1%) | 0.2s / 7.1s / 348.8s |
| s407 | 342 | 15365.6s | 3074.9s (20.0%) | 4172.8s (27.2%) | 0.6s / 30.0s / 586.0s |

两个结论：

1. **"墙钟时长"作为效率指标几乎没有意义**——s187 有 90% 的墙钟是人不在场的空等。那份 deepseek 报告的"16 分钟 vs 7.5 分钟"其实混淆了三种时间。
2. **正确的指标是"净工作时长" = 模型时间 + Agent 侧执行时间**，而区分"Agent 侧执行"与"人类空闲"不该用魔法阈值，而该用**间隙之后发生了什么**来分类：紧跟着一条新的真实用户指令 → 人类空闲；紧跟着纯工具循环延续 → Agent 侧执行。这个判据是规则性的，且 `deltaHasNewInstruction()` 已经实现了它需要的判断。

这是一个**免费、新颖、跨框架可比**的指标，v1.0 完全遗漏。**第 2 步已实现（`ComputeMetrics`），s231 复测结果 381.0s/18.4% vs 实测 380.983s/18.40%——几乎逐位吻合，见附录 D。**

### F11（新）— Compaction 至少有三种形态，v1.0 只建模了一种

| 型 | 机制 | VMR 可见性 | manifest 编辑 |
| --- | --- | --- | --- |
| **S1 外挂式** | 独立的 summarization 请求，之后新 lineage 带 `The conversation history…` 标记 | 完全可见（请求 + 标记 + 尾部） | `contract` + `fork` |
| **S2 原地替换** | 同一 lineage，中段被改写的消息取代，开头保留 | 只在 manifest 差分里可见 | `splice` |
| **S3 静默截断** | 无 LLM 调用，消息直接消失 | 只在 manifest 差分里可见 | `contract` |

S2 的实证（附录 A.4，`s231` turn 20 → 21）：

```
turn 20 (79 msgs): #0 system h=0b0a6cd7ee28 (36579 字符)
                   #1 user   h=6eb2c49e49ea  ← 原始指令
                   #2 assist h=3d6042de2c0e  (120 字符)
turn 21 ( 4 msgs): #0 system h=8c78dc78bcb0 (34060 字符)   ← system prompt 也变了
                   #1 user   h=6eb2c49e49ea  ← 同一条，逐字未变
                   #2 assist h=ed195ccf9337  (2153 字符)   ← 被改写，吸收了摘要
```

中间 76 条工具调用/结果被一条改写后的 assistant 消息取代。

**这引出一个 v1.0 遗漏的建模需求：`revision` 关系。** 在全局 seen-hash 去重下，`ed195ccf9337` 会被当作一个**全新事件**，而 `3d6042de2c0e` 留在流里——渲染出来就像 Agent 把同一件事说了两遍。必须显式标注"B 是 A 在同位置的修订版"（判据：同一 manifest 位置 + 编辑前后共享该位置之前的前缀）。位置信息只用于建立 `revision` 关系，不用于事件流排序。

**同时注意 system prompt 也变了**（36579 → 34060 字符）。system 变更是一个独立的、有分析价值的事件类型（换模型、换工具集、平台注入变化），必须进事件流。**第 2 步已实现（`Event.Revises` + `Step.SysChanged`）。**

**实施后的诚实澄清**：真正落地时发现 `s231` 的 S2 实证本身是走 `contract` 分支（大幅缩减），并不落在字面意义的"公共前缀之后尾部原样重现"（`Splice` 判据）里；`Splice` 判据本身在第 2 步的语料上命中 0 次。这不改变 F11 三形态分类的正确性，只是说明 S2 更常见的表现形式是"`Contract` + 新开头逐字保留旧锚点"，`revision` 关系的检测因此没有绑定在 `Splice` 上，而是独立按 F11 的语义直接实现——细节见附录 D.4。

### F12（新）— 语料规模与索引可行性

7112 条记录、809K 条消息实例、平均 113 条/请求；审计源文件 zstd 后 124MB（明文 GB 级）。索引化后 manifest ≈ 13MB、blob 位置索引 ≈ 10MB，全内存无压力。内容按需回捞，因 zstd 不可寻址，必须按文件批量顺序扫描——与现有两遍读取模式一致。

---

## 5. 值不值得做

### 5.1 支持的理由

1. **位置独有。** 跨框架（四套 Agent 系统走同一个入口）+ 全量原始 body + **快照时间序列**。第三条是真护城河：Agent 自己不记录它对历史做过的手术，因为对它而言那只是一次内存赋值。
2. **实测证明捷径走不通。** F1（末轮丢 26–99%）、F6（anchor 粘连）、F11（三种 compaction 形态）都说明：任何"取末轮 / 按 session 分组 / 认模板"的简化方案都会给出错误结论。而正确做法的数据已经在盘上了。
3. **免费的那部分价值最大。** §3.2 证明了对比分析的证据 90% 来自规则层。剖面层零成本、确定性、可复现。
4. **顺手偿还既有技术债。** Phase B 让 `report` 自身受益：修掉 8 条 compaction linking 告警、修掉 `context_growth` 的 ×179.5 脏值、补上 N-4 章节。这不是为 story 做的牺牲，是 report 自己欠的。

### 5.2 反对的理由与缓解

| 风险 | 说明 | 缓解 |
| --- | --- | --- |
| **受众窄** | 直接用户是本团队 + 少数评测者 | 承认它是定位型而非增长型功能；Phase A 零 LLM 成本，先证明自己有用 |
| **LLM 引入不确定性** | 不可复现、有成本、有幻觉 | 三层强制分层（§3.3）；默认不开；注解按 `(step_hash, model, prompt_version)` 落盘缓存，重跑稳定 |
| **启发式债务** | 无 ground truth 可验证缝合对错 | 内核只用协议通用信号；F9 已证明 Step 内因果是零误差的，不确定性只作用在跨 Step 的 lineage 关系上；每条边带证据 + 置信度，宁断勿错 |
| **范围扩张** | VMR 迄今最大的一次扩张 | 独立包 + 独立命令 + archtest 边界；随时可整体砍掉而不影响路由 |
| **隐私** | 产物含完整对话正文 | 沿用 `details/` 的 0600/0700；且见 §5.4 的诚实澄清 |
| **重构风险** | Phase B 动 `report` 的核心分组 | 一致性测试先行（§7.2），Phase B 是机械迁移而非重写 |

### 5.3 与既有原则的冲突复核

设计文档 §9 写死"只记录不分析"。处理方式就是 §3.3 的三层分层：

- **事实层 / 剖面层**：纯规则，与现有 report 同性质，**不构成原则突破**；
- **解读层**：LLM 只产出自然语言，必须携带引用的 step id，渲染为可折叠的独立样式块，旁边永远有原始证据入口；不配置分析端点时整层消失，报告仍完整可用（与 `pricing.yaml` 缺失时 §2 优雅降级同一套路）。

**LLM 说的话不是证据，只是索引。** 在这个约束下黑匣子定位不破。

### 5.4 关于"传播价值"的诚实澄清（v1.0 的一处夸大）

v1.0 说"一张 Agent 执行瀑布图是天然的内容素材"。这话本身没错，但 v1.0 同时把脱敏列为"延后到有需求再做"——**这两条是矛盾的**：任何真实 journey 都含完整对话正文、文件路径、内部项目名。

要么放弃传播这条价值主张，要么把脱敏提升为它的前置条件。建议后者：在 Phase D（HTML）里同时做 `--redact` 模式（保留结构与指标，正文替换为长度占位符 + 类型标签）。在此之前，传播价值记为 **0**，不计入 ROI。

### 5.5 成本实测估算

按 `pricing.yaml` 现价（最低 USD0.14/1M in ≈ ¥1/1M；MiniMax ¥1.20/1M in、¥8/1M out；汇总用 deepseek-v4-pro ≈ USD0.435/1M in）：

| 场景 | 去重内容量 | 解读输入（含 1.5× 滑窗） | 估算成本 |
| --- | ---: | ---: | ---: |
| 典型单任务（s231，24 轮） | ~80K token | ~120K | **¥0.15–0.25** |
| 中型（s187，254 请求） | ~550K token | ~800K | **¥1.0–1.3** |
| 极端（s290，444 请求 / 5 天） | ~1M token | ~1.5M | **¥2.0–2.5** |

不构成障碍，前提是两条硬约束：**按需触发**（绝不在 `vmr report` 里全量跑——752 个会话是几百块且 99% 没人看）、**落盘缓存**。再加一条操作约束：跑前 dry-run 打印预估 token 与金额并要求确认。

注意：Phase A/B 交付的事实层 + 剖面层**成本为零**。LLM 只在 Phase C 才出现，而彼时是否还需要它，应该用 Phase A/B 的实际使用体验来决定。

---

## 6. 方案设计（第一性原理版）

### 6.1 概念模型

```
ctxgraph（内容寻址层）
  Blob      一条消息的内容 hash → (path, line, msgIndex) 位置索引，内容不驻留
  Manifest  一次请求的 hash 向量 + system blob + 元数据（ts, dur, endpoint, usage…）
  Edit      相邻 manifest 的差分分类（append / replace_tail / splice / contract / fork）
  Lineage   由 append / replace_tail 连成的链
  Graph     lineage 之间由 splice / contract / fork 边连接，每条边带 kind + evidence + confidence

story（视图层）
  Journey ── Task ── Step ── Event
                              └ revision 关系（F11）
```

Event 类型：`user_message` / `assistant_text` / `thinking` / `tool_call` / `tool_result` / `compaction_summary` / `system_change`。
每个 Event 带生命周期：首见 step、末见 step、消失原因（`truncated` / `pruned` / `compacted` / `reset` / `alive`）。

### 6.2 Journey 构建：切分 + 合并

**先切分**（F6 的教训）：在同一 SessKey 内，遇到 `contract` / `fork` 编辑就断开成独立 lineage。不能假设 anchor 相同就是同一段。

**再合并**：对每个 lineage 的首 manifest `B₀`，用 blob 倒排索引（hash → lineage）找覆盖率最高的前驱 `A`，`score = |H(B₀) ∩ H(A)| / |H(B₀)|`。边分四类：

| kind | 触发条件 | 置信度 |
| --- | --- | --- |
| `splice` | 同 lineage 内的中段替换（F11 的 S2） | 高（结构判定） |
| `compaction` | `contract` 编辑 + 尾部 hash 强覆盖 +（可选）标记/summarization 请求 | 高 |
| `head_prune` | 头部剪枝，后续 hash 大量重叠 | 中 |
| `same_chat` | 仅 chat_id 相同 + 时间邻近，无内容重叠 | 低——**默认不缝合**，只标注"疑似同源" |

**原则：宁可断开，不要错连。** 低置信度的边渲染成显式的「⟂ 此处可能断裂（未找到足够证据）」。这是整个功能可信度的唯一来源。

> **实施记（第 2 步）**：`ctxgraph` 层面无法区分"splice"与"compaction"（区分依据是可选的模板/摘要请求信号，属于 profile 层知识），落地时把这两类收缩成一个 `StitchCompaction`；如果要进一步细分，应在 `story/profile` 层做。四类边的语义仍然成立，只是 `ctxgraph` 这一层只暴露三类边（`StitchCompaction`/`StitchHeadPrune`/`StitchSameChat`）。详见附录 D。

### 6.3 事件流与生命周期

全局 seen-hash 去重，按（请求时序，消息位置）首次出现排序。同时维护每个 blob 的出现区间，journey 结束时回填消失原因。位置信息**只**用于建立 `revision` 关系，不参与排序。

### 6.4 Compaction 还原与信息损失（= CCR N-4 的落地）

compaction 渲染为特殊 Step，三块内容：

1. **被吞掉的原始 events 全量**（默认折叠，从 blob 索引回捞）——这是相对 Agent 自身日志的核心优势；
2. **summary 原文**（S1 有独立请求，S2 是被改写的消息）；
3. **信息损失度量**：压缩前后 token 对比 + 规则粗筛的实体覆盖率（被吞段里出现过的文件路径 / URL，有多少在 summary 中彻底消失——`chatmsg.ExtractEntities` 的正则只匹配路径/URL 形状的 token，不识别工具名，见 `internal/story/journey.go`）。

纯规则、零 LLM 成本、可复现。**不修复，只揭示**——CCR 花 1600+ 行修一个它自己也修不好的问题，VMR 只需如实呈现"这次压缩把 42K token 压成 1.9K，其中这 7 个文件路径在摘要里消失了"。

### 6.5 行为剖面（剖面层，v1.0 遗漏）

全部规则派生、跨框架可比：

| 指标 | 定义 | 回答什么 |
| --- | --- | --- |
| 净工作时长 | 模型时间 + Agent 侧执行时间（F10 的间隙分类） | 真实效率，剔除人类空闲 |
| 模型/工具时间比 | 同上两项之比 | 瓶颈在推理还是在执行 |
| 工具调用分布 | 按工具名的次数与 token 占比 | 工作方式画像 |
| 重复动作率 | 相同 tool_call 参数 hash 的重复次数 | 是否在原地打转 |
| 错误恢复次数 | `is_error` / 非零退出的 tool_result 后的重试 | 韧性 |
| 计划-执行比 | 无 tool_call 的纯文本轮次 / 总轮次 | 思考与行动的配比 |
| 上下文构成演化 | 每轮 system/user/assistant/tool token 占比曲线 | 上下文预算流向 |
| 上下文有效率 | 被后续引用过的 blob token / 总 blob token | 上下文浪费程度（S-D 场景） |
| compaction 次数与损失 | §6.4 | 信息断层 |

两份剖面做差 = 对比报告的骨架（Phase F 因此大幅降本）。**第 2 步已实现全部九项（`story.ComputeMetrics`），随 `journey-<id>.json` 落盘，详见附录 D。**

### 6.6 渲染

- **Phase A Markdown**：`{out}/stories/journey-<id>.md`，0600/0700，原始内容 `<details>` 折叠，与现有 detail 页风格一致。行为剖面另落 `journey-<id>.json`（§6.5），不重复渲染进 Markdown。
- **Phase D HTML**：单文件自包含（内联 CSS/JS，零外部依赖）。左侧时间轴 + 右侧 Step 卡片瀑布；卡片正面是骨干描述，点开是原始 body。同时实现 `--redact`（§5.4）。

---

## 7. 回到 VMR 现实：落地路径与技术债偿还

### 7.1 第一性原理最优解与现状的差距

| 维度 | 最优解 | VMR 现状（v1.1 更新） | 差距性质 |
| --- | --- | --- | --- |
| manifest | 一等公民，带索引 | ✅ `internal/ctxgraph.Manifest`（第 1 步已交付） | 已解决 |
| blob 位置索引 | 有 | ✅ `internal/ctxgraph.BlobIndex`（第 1 步） | 已解决 |
| 编辑分类 | 五类显式建模 | ✅ 五类全部落地（`Append`/`ReplaceTail`/`Splice`/`Contract`/`Fork`，第 1+2 步） | 已解决 |
| lineage 切分 | 按编辑类型切 | ✅ `ctxgraph.Scan`（第 1 步） | 已解决 |
| lineage 缝合 | hash 集合匹配 | ✅ `ctxgraph.StitchGraph`（第 2 步） | 已解决 |
| 剖面层九项指标 | 规则派生 | ✅ `story.ComputeMetrics`（第 2 步） | 已解决 |
| `report` 自身分组/告警/`context_growth` | 消费同一套 `ctxgraph` | ✅ `report/session.go` 已消费 `ctxgraph.Lineage`/`Classify`，`keys`/`lcp()`/`sysKey`/`anchor` 已删除（compaction 文本 needle 部分保留，见附录 F.2） | 第 3 步已完成，见附录 F |

**结论不变：差距不大，而且大部分是"现有实现本来就该修的缺陷"，不是"为 story 额外付的代价"。** 第 1、2、3 步已经把这张表清零——`ctxgraph`/`story`/`report` 三边全部消费同一套 lineage 事实来源，见附录 D、F。

### 7.2 渐进计划的结构（可执行版见附录 C）

> 本节讲**为什么这么分阶段**；**开工用的任务清单、验收标准和不做清单在附录 C**；**第 1/2 步实际完成情况见附录 D，第 3 步的重新评估计划见附录 E、实施记录见附录 F**。
> 阶段映射：本节的 Phase A 在附录 C 里按"能否缝合"拆成了第 1 步（切分，3.5 天）与第 2 步（缝合 + 剖面，5 天）——因为实测发现 95.86% 的边是纯 append（附录 A.7），切分能独立交付而缝合不能。Phase B = 第 3 步；Phase C/D/E/F = 第 4 步的四个可选模块。

**Phase A — 建立 `internal/ctxgraph`，`story` 跑起来（`report` 不动）** —— ✅ 已完成（第 1+2 步，附录 D）

- 新包实现 blob 索引 / manifest / 编辑分类 / lineage 切分与缝合；
- `story` 在其上做事实层 + 剖面层 + Markdown 渲染；
- `report` 完全不改，零回归风险。

**防分叉机制（关键）**：Phase A 必须同时交付一个**一致性测试**——用 `ctxgraph` 重建的 session 分组，与 `report.AnalyzeSessions` 在同一语料上的输出做对比，差异必须**逐条被显式声明**（要么"应当一致"，要么"这是 ctxgraph 修正了 report 的已知缺陷，见 F6/F2"）。✅ 已交付（`internal/report/session_conformance_test.go`）。

没有这个测试，Phase A 就会变成一个永久分叉，Phase B 永远不会发生。有了它，Phase B 变成机械迁移。

**Phase B — `report` 迁移到 `ctxgraph`，偿还技术债** —— ✅ 已完成（第 3 步，附录 F）

- `session.go` 的分组改为消费 `ctxgraph` 的 lineage，删掉私有 `keys`；
- 顺带修掉：compaction linking 告警、`context_growth` 的 ×179.5 脏值、F6 的 lineage 粘连；
- 新增 `report` 的 compaction 章节（= CCR N-4），作为 `section_compaction.go`（符合"新增一节 = 新增一个文件"的预算约束）；
- `archtest` 增加 `ctxgraph` 的边界规则（已在第 1 步交付，Phase B 无需再加）。

**Phase C 及之后** — LLM 解读层 / HTML / subagent / 对比报告。

**顺序建议**：`A → B → C`。理由是趁 ctxgraph 设计还新鲜时把 `report` 迁过去，且 Phase B 有独立的用户价值（修 report 自身的缺陷）。

**明确的终局状态**：一套 lineage 模型，两个渲染器（运维报告 + 叙事报告）。这是终点，不是妥协；上面三个阶段是逼近它的路径。

### 7.3 包与命令边界

```
internal/chatmsg   ← 消息/SSE/usage 解析（第 1 步已从 report/render.go 下沉）
internal/ctxgraph  ← blob 索引 + manifest + 编辑分类 + lineage 图 + 缝合（第 1+2 步已交付）
                     依赖 {audit, core, chatmsg}
internal/story     ← journey 视图 + 事实层 + 剖面层 + 渲染（第 1+2 步已交付；LLM 客户端未做，第 4 步）
                     依赖 {ctxgraph, chatmsg, core, config}
internal/report    ← ✅ 第 3 步已交付：分组消费 ctxgraph.Lineage/Classify，
                     依赖 {ctxgraph, chatmsg, audit, core}（compaction 文本 needle
                     链接作为 ctxgraph.Stitch 的并列补充信号保留，见附录 F.2）
cmd/vmr/cmd_story.go
```

`archtest` 规则（已生效）：`ctxgraph` 不得依赖 `router` / `server` / `config` / `report` / `story`；`story` 不得依赖 `router` / `server` / `report`；`report` 依赖 `ctxgraph` 现在是**合法的、单向的**（第 3 步落地），`archtest` 的旧注释（"report 只依赖 audit/core"）已随之更新。

**这里推翻了 v1.0 的一个结构性判断。** v1.0 说"`story` 不依赖 `report`"——方向对（不该让 report 为 story 开导出口子），但**解耦的层选错了**：正确答案不是两边各写一套，而是**两边共同依赖一个更低的层**。v1.0 那个方案会造成 lineage 逻辑的永久二重实现，正是要避免的东西。

CLI（实际落地，与原计划的差异见附录 D）：`vmr story [-c config.yaml] [-journey <id前缀> | -render-all] [-include-partial] [-show-ungrouped] [-o dir] [file|glob]...`。`vmr.sh` 的 `passthrough` 自动转发，脚本无需改动。

---

## 8. 分期与 effort（诚实重估）

> 本节是**总量与口径**；实际排期以**附录 C.2 的四步计划**为准（同一批工作、同一个总量 20.5 天，只是把 Phase A 按可交付性拆成了两步）。

| 阶段 | 内容 | 人天 | 独立可交付 | 状态 |
| --- | --- | ---: | --- | --- |
| **A** | `chatmsg` 下沉 0.5 + `ctxgraph`（blob 索引/manifest/编辑分类/lineage 切分与缝合）2.5 + `story` 事实层 1.5 + 剖面层 1 + Markdown 渲染 1 + CLI 0.5 + **一致性测试 1** + 拆 `splice/tail` 混合桶 0.5 | **8.5** | 是（附录 C 里拆成第 1、2 步） | ✅ 已完成 |
| **B** | `report` 迁移到 `ctxgraph` + 修 compaction linking / context_growth / lineage 粘连 + `section_compaction.go` + archtest | **3** | 是：`vmr report` 自身缺陷修复 + N-4 章节 | ✅ 已完成（附录 F） |
| **C** | LLM 解读层：prompt、结构化输出、落盘缓存、dry-run 预算、失败降级 | **2.5** | 是 | 🟡 部分完成——4d（compare）场景的切片已实施（自由格式 Markdown，非结构化输出；见附录 G 补记），单 Journey 逐 Step 解读仍未开始 |
| **D** | 单文件自包含 HTML 瀑布流 + `--redact` | **2.5** | 是 | 未开始 |
| **E** | Subagent 树（含先期采样验证 §10.1 的三条信号） | **2.5** | 是 | 未开始 |
| **F** | 双 Journey 对比报告（剖面做差 + 差异归因） | **1.5** | 是 | ✅ 已完成（含本次新增的规则层扩展：端点/缓存/system prompt/末轮上下文/终止方式/最终交付物，见附录 G 补记） |
| | **合计** | **20.5** | | 12.5–13/20.5 已完成（Phase C 只完成了 compare 切片，未计满分） |

**与 v1.0（14.5）的差异说明**：+6 天。其中 +3 是 `ctxgraph`、一致性测试与编辑分类细化（买的是"不用推倒重来"），+3 是 Phase B（买的是 report 自身缺陷修复 + 债务出清），+0.5 是剖面层与 `--redact`；−2 是 Phase F 因剖面层前置而降本。

**v1.0 的 14.5 天是低估**，因为它把 lineage 逻辑当成 story 私有的一次性工作，没算 report 迟早要重做同一件事的成本。

---

## 9. 风险登记

| ID | 风险 | 概率 | 影响 | 缓解 | 现状（v1.1） |
| --- | --- | --- | --- | --- | --- |
| R1 | 缝合误判，把两个不相关任务缝成一个 journey | 中 | 高 | 保守阈值 + 边带置信度 + 显式渲染断裂点；F9 已证明 Step 内因果零误差，不确定性只在跨 Step 边 | ✅ 已缓解：F9 断言已自动化（0 孤儿/406534 对），缝合真实语料验证 62/68 成功缝合、6 正确识别为疑似同源、0 跨桶时间窗违规 |
| R2 | **lineage 误切分**（把一个任务切成两段） | 中 | 中 | 与 R1 对称的失败模式，v1.0 未识别。切分阈值同样保守，切分点必须渲染出来并给出证据 | 阈值仍是初始取值，未做更大语料的重新校准（见附录 D.4） |
| R3 | LLM 幻觉污染结论 | 中 | 高 | 三层分层；数字只由规则产生；注解带 step 引用 | 未触及（第 4 步范围） |
| R4 | 成本失控 | 低 | 中 | dry-run 预估 + 显式确认 + 落盘缓存 + 绝不在 report 里全量跑 | 未触及（第 4 步范围） |
| R5 | 启发式债务膨胀 | 中 | 中 | 内核只用协议通用信号；agent 特化收敛到 profile 层 | 保持成立 |
| R6 | 非 OpenClaw 的 compaction 形态未知（F8） | 高 | 中 | 编辑分类是结构性的，不依赖模板；未识别的编辑标为低置信度而非静默合并 | 保持成立，真实语料上验证过 hermes/pi/pimini 等非 OpenClaw 语料的缝合结果合理 |
| R7 | 隐私 / 传播的矛盾 | — | 中 | §5.4：脱敏是传播价值的前置条件，Phase D 交付前传播价值记 0 | 未触及（第 4 步范围） |
| R8 | **Phase A 永久分叉，Phase B 不发生** | 中 | 高 | 一致性测试（§7.2）是唯一保险，必须与 Phase A 同批交付，不可延后 | ✅ 风险已消失：Phase B（第 3 步）已完成，`report` 现在直接消费 `ctxgraph`，不再有"两套独立实现"可分叉——一致性测试本身的性质也因此变化（见附录 F.7） |
| R9 | Phase B 动 `report` 核心分组导致回归 | 中 | 中 | 一致性测试先行，迁移变机械操作；`session_test.go` 534 行现有覆盖作为安全网 | ✅ 已验证：534 行 `session_test.go` 全部无需修改、原样通过（附录 F.4）——"迁移是机械操作"这一判断被真实回归结果证实，而非仅仅是计划阶段的预期 |
| R10 | 范围蔓延反噬 router 主线 | 中 | 中 | 独立包 + 独立命令 + archtest 边界；随时可整体砍掉 | 保持成立，`archtest` 边界测试持续全绿 |

---

## 10. 盲区与已知限制

诚实列出 VMR 这个位置**看不到**的东西，避免报告读者把"没记录"误读成"没发生"：

| 盲区 | 影响 | 能否缓解 |
| --- | --- | --- |
| 工具执行本身 | 只能看到耗时和返回文本，看不到副作用（写了什么文件、跑了什么命令的真实效果） | 不能。报告须显式声明"工具结果是 Agent 自述" |
| 不经 VMR 的调用 | Agent 若对某些任务直连其他 provider（如本地小模型做 compaction），这段完全不可见 | 部分：manifest 会出现无法解释的 `splice`/`contract`，应标为"外部编辑，来源不可见" |
| Agent 的内部状态 | 计划、记忆检索、循环检测等不体现在 messages 里的逻辑 | 不能。只能从行为反推 |
| 客户端重试与本地缓存 | 客户端自行重试可能不产生新请求 | 部分：`X-Stainless-Retry-Count` 头可见 |
| 被 VMR 拒绝的请求 | 有记录（`outcome=error`），但无上游往返 | 已覆盖，须计入事件流（Agent 的重试行为本身有信息量） |
| 跨 Agent 的人类协调 | 用户把 A 的输出粘给 B | 部分：内容 hash 相同即可发现，属于 Phase F 的额外能力 |

其中第二条是唯一需要在设计上留口子的：**编辑分类必须有"来源不可解释"这个显式类别**，不能强行归到某种已知 compaction。

### 10.1 暂缓项：Subagent

不做，但 Event 模型预留 `parent_step_id`，渲染层预留分支。识别信号（三条合取，均可验证）：

1. system blob 与主 journey 同源（共享 system hash 或工具集高度重叠）；
2. 时间窗**完整嵌套**在主 journey 某次 Task/agent 类 `tool_call` 与其对应 `tool_result` 之间——F9 已证明这对配对是精确的，所以这个窗口是**准确的区间**，不是估计；
3. 其最终输出 blob 出现在主 journey 后续那条 `tool_result` blob 里。

第 3 条是决定性的，与 F2 的 hash 匹配同一思路。Phase E 开工前先写采样脚本验证三条信号在现有语料上的命中率，验证不过就继续推迟。

---

## 11. 决策记录

v2.0 遗留的五个未决问题已于 2026-07-28 全部拍板，决策与理由见 **附录 C.1**，落地安排见附录 C 的分步计划。摘要：

| # | 问题 | 决策 | 落地状态 |
| --- | --- | --- | --- |
| D1 | Journey 断头（头不在输入集合里） | 默认跳过，`--include-partial` 才处理；缓存 key 用 step hash 而非 journey id，因此 ID 不稳定不再是问题 | ✅ **已实施（v1.2）**：自证方式从原计划的 ID `t-` 前缀改为**文件名 `-partial` 后缀**（`journey-<id>-partial.md`/`.json`）——不改动 `deriveID`/`RootHash` 本身，只在 `cmd_story.go` 写文件那一步追加后缀，改动面更小、且不影响 ID 的内容寻址语义；`story.Journey.Partial` 字段（此前已声明但从未被设置）现在由调用方在渲染前显式赋值。断头 journey 文件名现在自带"这个 ID 不稳定"的信号，不需要打开正文找警示语 |
| D2 | `contract` / `fork` 阈值 | 代码常量，不进 `config.yaml`；用全语料实测值定初值（附录 A.7） | ✅ 已落地，第 2 步新增的缝合阈值（`stitchCompactionScore`/`stitchHeadPruneScore`/`stitchCrossBucketMaxGap`）延续同一原则 |
| D3 | 同一 chat 内连续任务是否合并 | **Journey 边界 = manifest 编辑边界，与时间无关**——Agent 是否保留上下文，就是它对"是否同一任务"的表态。不需要时间阈值 | ✅ 已落地，第 2 步的跨 SessKey 缝合时间窗（`stitchCrossBucketMaxGap`）只限制**跨桶**匹配，同桶内部依然与时间无关，与 D3 一致 |
| D4 | 间隙分类边界 / 无人值守 | `heartbeat` / `dream_diary` 等 scheduled 类不进 story；间隙分类规则自洽，无人值守场景自动归 Agent 侧 | ✅ 已落地（`Step.HumanInitiated` + `ComputeMetrics` 的 F10 时间三分解） |
| D5 | 非 OpenClaw profile 优先级 | 先按 OpenClaw 实现，架构留 profile 接口；之后跑 Pi / Hermes 看结果再决定是否写专用 profile | ✅ 按计划：仍只有 OpenClaw + 通用兜底两个 profile，Pi/Hermes/Lobster 等真实语料验证过通用兜底不出错，暂未发现需要专用 profile 的证据 |

---

## 附录 A：实测数据与复现方式

全部脚本在 `reports/`（`vmr report logs/vmr*` 的产物）上直接运行。

### A.1 末轮上下文的信息丢失（§2.3 / F1）

按 `vmr-requests.jsonl` 的 `session` + `detail_file` 取该会话全部 `details/*.json`，每条消息做 `md5(canonical json)`，比较"全部请求去重消息集合"与"末轮消息集合"：

```
s231  reqs= 24 union=  86 last=   8 dropped=  78  chars 165K/203K -> 81.4%
s290  reqs=444 union=1034 last=   5 dropped=1029  chars 2562K/2582K -> 99.2%
s187  reqs=254 union= 562 last= 394 dropped= 168  chars 697K/1414K -> 49.3%
s407  reqs=342 union= 743 last= 682 dropped=  61  chars 138K/531K -> 26.1%
s634  reqs=251 union= 483 last= 222 dropped= 261  chars 1126K/1393K -> 80.8%
s217  reqs=366 union=1303 last= 449 dropped= 854  chars 1844K/2258K -> 81.7%
```

### A.2 Compaction hash 缝合（F2）

`20260716-155251.355`（post-compaction，60 消息，#1 为 summary），取 `[2:]` 的 58 条 hash，与当日更早 581 条请求求交：

```
match 42/58  20260716-152238.775   <- s231，唯一强峰
match 40/58  20260716-152233.327
match 38/58  20260716-152229.810
match 36/58  20260716-152226.699
match 35/58  20260716-152145.349
match 31/58  20260716-152130.915
```

### A.3 lineage 内部的隐藏断裂（F6）

`s231` 逐轮 manifest 演化（`msgs` 含 system；`cover` = 与上一轮的 hash 集合覆盖率）：

```
15:17:29 turn= 1 msgs=  3 lcp=  0 cover=0.00
15:18:06 turn= 2 msgs=  5 lcp=  2 cover=0.60
   …（turn 3-19 单调 append，cover 0.71 → 0.97）…
15:22:38 turn=20 msgs= 79 lcp= 76 cover=0.97
15:51:22 turn=21 msgs=  4 lcp=  0 cover=0.25 <== BREAK
15:51:27 turn=22 msgs=  6 lcp=  4 cover=0.67
15:51:30 turn=23 msgs=  6 lcp=  6 cover=1.00
15:51:53 turn=24 msgs=  8 lcp=  6 cover=0.75
```

### A.4 S2 型「原地替换」compaction（F11）

`s231` turn 20 → turn 21 的头部对照：

```
turn 20 (79 msgs): #0 system    h=0b0a6cd7ee28  36579 字符
                   #1 user      h=6eb2c49e49ea    442 字符  [Thu 2026-07-16 15:17 …] 深入调研这个内存涨价…
                   #2 assistant h=3d6042de2c0e    120 字符
turn 21 ( 4 msgs): #0 system    h=8c78dc78bcb0  34060 字符  ← system prompt 变了
                   #1 user      h=6eb2c49e49ea    442 字符  ← 逐字未变
                   #2 assistant h=ed195ccf9337   2153 字符  ← 被改写，吸收摘要
```

中间 76 条工具调用/结果被这一条改写后的 assistant 消息取代。

### A.5 因果配对与规模（F9 / F12）

```
20260716-152238.775: messages 79  tool_calls 57  tool_results 57
                     ids matched 57 / orphan calls 0 / orphan results 0
语料: 7112 records, 808998 message instances, avg 113/request
审计源: 15 文件, zstd 后 124MB
```

### A.6 时间三分解（F10）

见 §4 F10 表格。间隙定义 `gap = ts(N+1) − (ts(N) + dur_ms(N))`，统计时剔除 `gap > 600s` 的样本（那些是人类空闲，需按 F10 的规则单独分类）。

### A.7 编辑类型的全语料分布（决定第 1 步范围的关键数字）

对全部 752 个 session 中请求数 ≥3 的 168 个，按时序遍历相邻 manifest 并分类（判据即 §2.2 的类型学，阈值取 `contract: len < 0.6×prev`、`fork: cover < 0.5`）：

```
edge_append            6045   95.86%
edge_splice_or_tail     202    3.20%
edge_contract            27    0.43%
edge_fork                32    0.51%

多轮 session          168
含至少一次断裂         27  (16.1%)
断裂最多的会话        s290(444 请求, 10 次)、s248(46, 8)、s70(9, 6)、s217(366, 3)
请求数 ≤2 被跳过      584   （单发定时脚手架，按 D4 本就不进 story）
```

三条结论：

1. **95.86% 的相邻边是纯 append**，83.9% 的多轮任务全程无断裂 → 第 1 步只处理单 lineage 就能覆盖绝大多数真实任务；
2. 真正的断裂只有 59 条边（`contract` 27 + `fork` 32），且**高度集中在少数长会话**里 → 缝合是高价值但低频的工作，适合放到第 2 步；
3. `splice_or_tail` 的 202 条是**混合桶**（真正的原地替换 + 正常的 ephemeral tail 替换），第 2 步需要把它拆开——这是第 2 步唯一需要新判据的地方。

阈值取值即上表所用值，直接作为代码常量初值（D2）。

**第 2 步复测（全部 15 文件，`ctxgraph.Scan` 全量边）**：`append 96.25%`、`replace_tail 2.68%`、`splice 0%`、`contract 0.43%`、`fork 0.65%`，total edges=6351，lineages=829。`Splice` 命中数为 0——诚实记录，见附录 D.4。

### A.8 语料构成

| 维度 | 分布 |
| --- | --- |
| 协议 | openai 7053 / anthropic 59 |
| 调用方 | lobster 1860、pimini 1310、hermes 764、openclaw 663、pi 462、workbudy 61、其余 17 |
| 工作负载 | interactive 6555、heartbeat 324、dream_diary 224、compaction 9 |
| 会话 | 752（最大 444 请求 / 32 任务 / 跨 5 天） |

---

## 附录 B：v1.0 → v2.0 修订清单

本轮 review 在源码与数据上重新推导后，推翻了 v1.0 的三处结构性判断，补了两处遗漏，并修正了一处夸大。

| # | v1.0 的说法 | v2.0 的修正 | 依据 |
| --- | --- | --- | --- |
| 1 | LLM 解读层是核心价值，剖面指标未提及 | **规则派生的行为剖面才是核心价值**，LLM 是可选的第三层 | §3.2 拆解 deepseek 报告，其 90% 证据来自规则可得的表格 |
| 2 | compaction 靠识别模板检测 | **靠检测非追加型 manifest 编辑检测**，模板只是补充证据 | F11：至少三种形态，其中两种没有任何模板可认 |
| 3 | `story` 不依赖 `report`，各写一套 | **两者共同依赖更低的 `ctxgraph` 层**；v1.0 会造成 lineage 逻辑永久二重实现 | §2.5 / §7.1：manifest 逻辑已存在于 `report`，只是被降级成私有字段 |
| 4 | （无） | 新增 **F10 时间三分解**：墙钟时长作为效率指标几乎无意义，净工作时长才是 | 实测 s187 有 90% 墙钟是人不在场的空等 |
| 5 | （无） | 新增 **F6 补充 / R2**：缝合不只要合并，还要会**切分**；anchor 会把两段 lineage 粘在一起 | 实测 `s231` turn 20→21 的硬断裂 |
| 6 | （无） | 新增 **F11 `revision` 关系**：原地改写的消息若不标注，叙事会读成"同一件事说了两遍" | 实测 `s231` #2 消息 hash 变更 |
| 7 | 传播价值成立，脱敏"延后到有需求再做" | **两者矛盾**；脱敏是传播价值的前置条件，Phase D 之前传播价值记 0 | §5.4 |
| 8 | 合计 14.5 人天 | **20.5 人天**；v1.0 低估，因为它没算 `report` 迟早要重做同一件事的成本 | §8 |
| 9 | 分期 P0–P4 | 重组为 A–F，新增 Phase B（技术债偿还）与 §7.2 的**一致性测试防分叉机制** | §7.2 / R8 |

---

## 附录 C：分步实施计划

### C.1 已确认的决策与理由

**D1 — 断头 Journey：默认跳过，且这个问题比看上去小。**

原始担忧是"journey 的头不在输入集合里会导致 ID 不稳定，缓存和外链失效"。重新推导后发现担忧的前半段成立、后半段不成立：

- ID 确实会变（内容寻址的 ID 依赖于"最早可见的 manifest"）；
- 但**缓存不受影响**——LLM 注解的缓存 key 是 `(step_hash, model, prompt_version)`，`step_hash` 由消息内容决定，与 journey 边界无关。加载更多历史文件后，同一个 Step 的注解照样命中缓存。
- 剩下的只有"外链失效"（有人存了 `journey-xxx.md` 的路径），这个代价可以接受。

因此方案（成本≈0）：

1. **默认跳过断头 journey**，日志里给一句 `skipped N head-truncated journey(s), use --include-partial to render them`；
2. `--include-partial` 时才渲染，ID 加 `t-` 前缀（如 `t-a91f3c…`）——**前缀本身就是"这个 ID 不保证稳定"的自证**，不需要额外文档（**⚠️ 截至 v1.1 未实现，见 §11 D1**）；
3. 断头判据：lineage 的首 manifest 不是"冷启动形态"（消息数 ≤3 且无 tool_result），且它在输入文件集合的**第一个文件的前若干行内**（说明历史被输入范围截断，而非真的从这里开始）。

**D2 — 阈值是代码常量。** 放 `internal/ctxgraph/edit.go` 顶部，附一句注释说明取值来自哪次实测。初值取附录 A.7 实测所用值：

```go
// Thresholds are calibrated against the 2026-07-14..28 corpus (7112 records,
// 168 multi-turn sessions) — see docs/…_opus-5.md 附录 A.7 for the resulting
// edit-kind distribution. Tune here when more corpora disagree; deliberately
// NOT a config knob (users cannot calibrate what they cannot measure).
const (
    contractLenRatio = 0.6 // |cur| < 0.6*|prev| → contract
    forkCoverage     = 0.5 // |cur ∩ prev| / |cur| < 0.5 → fork
    tailSlack        = 2   // LCP within this of |prev| still counts as append
)
```

**D3 — Journey 边界 = manifest 编辑边界，与时间无关。** 这条同时解决了"用户 2 小时后回来追问"的场景，而且不需要任何时间阈值：

- 用户走开 2 小时后回来追问、Agent 带着完整历史继续 → manifest 是 `append` → **同一 journey，新 task**；
- 用户开一个无关的新任务、Agent 清空或重建上下文 → `contract` / `fork` → **新 journey**。

**Agent 是否保留上下文，就是它自己对"这是不是同一个任务"的表态。** 我们不需要判断语义，只需要读取它的表态。这比任何时间阈值都准确，而且零参数。

残留边界情况：Agent 在同一 chat 里不清空上下文就接了个完全无关的新任务 → 会并成一个 journey。此时 task 分章已经表达了边界，报告里额外给一个提示（新指令与前文内容重叠度极低）。可接受，不特殊处理。

**D4 — scheduled 类不进 story；间隙分类规则自洽。**

- `heartbeat` / `dream_diary` 等单发脚手架整体排除（复用 `report` 已有的 workload class 判据）。附录 A.7 里那 584 个"请求数 ≤2 被跳过"的 session 基本就是这一类，本来也进不来。
- 间隙分类规则：**间隙之后紧跟一条新的真实用户指令 → 人类空闲；否则 → Agent 侧执行。** 夜间无人值守长任务不会出现新用户指令，因此自动归为 Agent 侧执行——规则自洽，不需要特判。
- 间隙后既有新用户指令又有工具循环：按"该请求的 delta 里是否含新真实用户指令"判定，`deltaHasNewInstruction()` 的现成语义直接可用，不引入新判据。

**D5 — OpenClaw 优先，架构留 profile 接口。** 第 1 步就要把 agent 特化收敛到一个接口后面，但只实现 OpenClaw + 一个通用兜底：

```go
// internal/story/profile/profile.go
type Profile interface {
    Name() string
    Detect(m *ctxgraph.Manifest) bool          // 是否由本 profile 处理
    IsRealUser(msg chatmsg.Message, raw any) bool  // 剥离路由头/脚手架
    CompactionMarker(msg chatmsg.Message) bool     // 首条非 system 消息的严格判定（F5）
    NoReply(respText string) bool
}
```

内核（`ctxgraph`）完全不认识 profile——它只做 manifest 差分。profile 只影响 `story` 的 task 切分与标记识别。这样跑 Pi / Hermes 时，最坏情况是走通用兜底、task 标题差一点，**事件流和编辑分类照样正确**。

### C.2 总体节奏

| 步 | 主题 | 一句话目标 | 人天 | 交付后能做什么 | 状态 |
| --- | --- | --- | ---: | --- | --- |
| **1** | **能看** | 单 lineage 的事实层叙事报告，切分但不缝合 | **3.5** | 对 84% 的多轮任务出完整报告，16% 出分段报告 | ✅ 已完成 |
| **2** | **能信 + 能比** | 跨 compaction 缝合 + 行为剖面 + 一致性测试 | **5** | 100% 覆盖；两份剖面做差即可对比两套 Agent | ✅ 已完成 |
| **3** | **能还债** | `report` 迁移到 `ctxgraph`，修三个已知缺陷 | **3** | 一套 lineage 模型两个渲染器；report 自身变准 | ✅ 已完成，实施记录见附录 F |
| **4** | **可选扩展** | 四个互不依赖的模块，按当时需要挑 | **9** | LLM 解读 / HTML+脱敏 / subagent / 对比报告 | ⬜ 未开始，计划见 §C.6（附录 G 有基于第 3 步实况的微调） |

前三步共 **11.5 人天，已全部交付**。第 4 步的四个模块彼此无依赖，可以任意挑、任意顺序、随时停。第 1、2 步的详细交付物、真实语料验证、走查发现的问题见**附录 D**（原 v2.0 附录 D–I 的压缩合并版）；第 3 步见**附录 F**。

### C.3 第 1 步 —— 能看（3.5 人天，✅ 已完成）

**目标**：单 lineage 事实层叙事报告，切分不缝合，零 LLM 成本，不动 `report`。**为什么是 low hanging fruit**：附录 A.7 证明 95.86% 的相邻边是纯 append，缝合贵、切分免费，所以第 1 步只切不缝，100% 的任务都拿得到可用产物，只是 16% 会分成几篇。

任务清单（全部已完成，详见附录 D）：`chatmsg` 下沉、`ctxgraph` 内核（manifest/blob 索引/编辑分类/lineage 切分）、`story` 事实层 + Markdown 渲染、CLI（`vmr story`）。

**验收标准逐条结果**：s231→s238 断裂精确复现；全语料扫描不 panic 且远低于 2× `vmr report` 耗时预算；`tool_call`/`tool_result` 100% 配对（第 2 步前置任务补齐为自动化断言）；golden test 幂等；`archtest` 全绿；`vmr report` 输出 `diff -r` 无差异。

**明确不做（全部保留至今）**：不缝合 · 无剖面层 · 无 LLM · 无 HTML · 不动 `report` · 不处理 subagent · 断头 journey 默认跳过 · 不拆 `splice_or_tail` 混合桶。

一个真实渲染样例（`journey-j-pimini-20260724T125852-20260724T141928-aca17c1f.md`，S-C 事故复盘场景）：

````
# Journey j-pimini-20260724T125852-20260724T141928-aca17c1f

> read both @REPORT_REDESIGN.zh.md and @REPORT_REDESIGN_V2.zh.md 你看一看V2当中指出的问题是不是有…

> 3 任务 · 105 轮 · 2026-07-24 12:58:52 → 14:19:28

## t02 · read both @REPORT_REDESIGN.zh.md and @REPORT_REDESIGN_V2.zh.md 你看一看V2当中指出的问题是不是有…

### Step 4 · 21:32:26 · 2.9s (ttft 2.0s) · 135/25.2K/86 · openai:volcengine:glm-5.2

> 编辑: append（最长相同前缀 6 条消息，内容重合率 75%）

**Messages**

<details><summary>▸ assistant · Translated and saved to `…REPORT_REDESIGN.zh.md`. The tra…</summary>
```
（原文……）
```
</details>

**LLM Response**

<details><summary>🤔 reasoning · 170 字符</summary>
```
The user wants me to read both files and evaluate…
```
</details>

<details><summary>finish: tool_calls (read, read)</summary>

🔧 **tool_call** `read` [id=call_6cedd1881aad4a8d80063471]
```json
{
  "path": "/Volumes/SSD2T/code/vmr/REPORT_REDESIGN.zh.md"
}
```
</details>
````

（`Messages` = 本轮新进入上下文的内容；`LLM Response` = 本轮模型自己产出的内容，含 reasoning 与每个 tool_call 的完整参数——这个拆分是第 1 步交付后根据真实阅读反馈补的，见附录 D.3。）

### C.4 第 2 步 —— 能信 + 能比（5 人天，✅ 已完成）

**目标**：断裂能缝上且缝得可信；两份 journey 的剖面做差就能回答"这两套 Agent 差在哪"。

任务清单（全部已完成，详见附录 D）：拆分 `splice_or_tail` 混合桶（新增 `Splice` EditKind）、缝合（`internal/ctxgraph/stitch.go`）、Compaction 三形态与信息损失、行为剖面九项指标（`internal/story/metrics.go`）、一致性测试（`internal/report/session_conformance_test.go`）。

**验收标准逐条结果**：s231 精确缝合并复现 F10 手算的净工作时长（几乎逐位吻合）；一致性测试全绿，`knownImprovement` 白名单每条都有 F 编号；旧版 `compaction linking` 告警与新缝合结果的对账**部分符合**（旧机制本身待第 3 步替换，细节见附录 D.4/E）。

### C.5 第 3 步 —— 能还债（3 人天，✅ 已完成，重新评估见附录 E，实施记录见附录 F）

**目标**：`report` 迁移到 `ctxgraph`，一套 lineage 模型两个渲染器。原任务清单（T3.1 迁移 session 分组、T3.2 修 context_growth、T3.3 新增 compaction 章节）在附录 E 重新核对后**范围基本不变**，仅有一处新增的小澄清点（`extractEntities` 的复用位置）。**验收**：一致性测试仍全绿；`vmr report` 的会话数与 journey 数可解释对应；`archtest` 全绿。

**实施后的结果**：三条验收标准全部达成，详见附录 F。唯一超出 E.2 预判范围的决策点是 compaction 缝合的信号来源（附录 F.2）——`ctxgraph.StitchGraph` 只覆盖同 `SessKey` 桶内的断裂，跨桶、内容零重合的标准 compaction 调用仍需要文本 needle 兜底，因此 `linkCompactions` 被保留而非删除，`group()` 新增的 `linkStitchedLineages` 是**并列的第二条信号**，不是替换。

### C.6 第 4 步 —— 可选扩展（9 人天，四个模块互不依赖）

| 模块 | 人天 | 触发条件 | 状态 |
| --- | ---: | --- | --- |
| **4a LLM 解读层** | 2.5 | 用了第 1–3 步之后仍觉得读起来累。含 prompt 设计、结构化输出、`(step_hash, model, prompt_version)` 落盘缓存、dry-run 预算与确认、端点缺失时整层降级 | 🟡 部分完成——2026-07-30 落地了 4d（compare）场景的切片，不是完整的单 Journey 逐 Step 解读；结构化输出未做（先上自由格式 Markdown），其余（prompt/缓存/dry-run/降级）均已实施。落地记录见附录 G 补记 |
| **4b HTML + 脱敏** | 2.5 | 需要对外分享时。单文件自包含（内联 CSS/JS，零外部依赖）+ `--redact`。**在 4b 交付前，传播价值记 0**（§5.4） | 未开始 |
| **4c Subagent** | 2.5 | 先跑采样脚本验证 §10.1 三条信号的命中率，不达标就继续推迟 | 未开始 |
| **4d 双 Journey 对比** | 1.5 | 第 2 步的剖面 JSON 已落地——两份剖面做差 + 差异归因表。**成本可能低于原估**：第 2 步交付的 `JourneySummary`/`Metrics` 结构已经是"两份剖面"的现成输入 | ✅ 已完成，2026-07-30 追加了 `ComparisonExtras` 规则层扩展（端点/缓存/system prompt/末轮上下文/终止方式/最终交付物）——严格说超出了这里最初"剖面做差"的最小范围，但都是零 LLM 成本的规则事实，落地记录见附录 G 补记 |

建议顺序：先 4d（最便宜且直接命中原始动机，且第 2 步已经把它的前置依赖做完），再按需 4a / 4b / 4c。**实际执行确认了这个顺序是对的**：4d 做完之后，4a 只需要在 4d 的产物上加一层可选的 LLM 解读，两者天然衔接，没有出现"先做小的、后做大的时发现返工"的情况。

### C.7 贯穿所有步骤的不变量

开工前贴在显眼处，每次 PR 自查：

1. **三层不得混淆**：事实层（规则）→ 剖面层（规则派生）→ 解读层（LLM）。**LLM 不得生成任何数字**，注解必须携带引用的 step id。
2. **宁可断开，不要错连。** 低置信度的边渲染成显式断裂标记，不静默合并。切分同理（R2）。
3. **不变量断言**：`tool_call` ↔ `tool_result` 配对率 100%（F9）。这是唯一零误差的事实，用测试守住。✅ 已落地为 `internal/story/invariants_test.go`。
4. **阈值是代码常量**，附实测出处注释，不进 `config.yaml`（D2）。
5. **产物 0600 / 目录 0700**，与 `details/` 同级——它们携带完整对话正文。
6. **archtest 边界**：`ctxgraph` 不依赖 `router`/`server`/`config`/`report`/`story`；`story` 不依赖 `router`/`server`/`report`；`report` 从第 3 步起可依赖 `ctxgraph`。
7. **幂等可重跑**：同一输入产出逐字节相同（LLM 注解靠缓存保证）。**第 2 步曾一度违反这条**（`ctxgraph.StitchGraph` 的同分平局打破依赖 map 遍历顺序）；已修复并补了回归测试，见附录 D.4。
8. **"来源不可解释"是一等公民**（§10）：无法归类的编辑标为该类别，不强行塞进某种已知 compaction。
9. **内容不驻留内存**：只索引 `(path, line, idx)`，按需按文件顺序回捞；zstd 不可寻址是硬约束。

---

## 附录 D：第 1、2 步实施状态总览

第 1 步（2026-07-28 实现）与第 2 步（2026-07-29/30 实现）均已完成并通过代码走查 + 真实语料验证。本附录压缩合并了 v2.0 原附录 D–I 的全部实施记录，只保留对后续开发者有用的结论；完整的逐条评审过程见 git 历史。

### D.1 交付物

| 步 | 新增/改动 |
| --- | --- |
| 第 1 步 | `internal/chatmsg`（消息/SSE/usage 解析，从 `report` 下沉）；`internal/ctxgraph`（`Manifest`/`Edit`/`Lineage`/`Scan`/`BlobIndex`/`FetchRecords`）；`internal/story` + `internal/story/profile`（`Journey`/`Task`/`Step`/`Event`/`Build`/`RenderMarkdown`）；`cmd/vmr/cmd_story.go`；`internal/archtest` 新增 `ctxgraph`/`story` 依赖边界规则 |
| 第 2 步 | `internal/chatmsg/pairing.go`（`CheckToolPairing`，F9 断言）；`internal/ctxgraph/stitch.go`（`StitchGraph`/`ChainFrom`/`LineageIndex`）；`edit.go` 新增 `Splice` EditKind；`story.Journey` 从"1 lineage"泛化为"1 缝合链"（`Chain []*ctxgraph.Lineage`）；`Step.StitchEdge`/`SysChanged`/`Compaction`/`HumanInitiated`；`Event.Revises`；`internal/story/metrics.go`（`ComputeMetrics`，九项行为剖面 + `journey-<id>.json` 落盘）；`internal/report/session_conformance_test.go`（一致性测试） |

全部新增/改动文件均有随附单测；全量测试（含 `-race`、`archtest`）持续保持全绿。

### D.2 关键设计决策（自主决策，均按"debrief 后无阻塞问题即自行实施"执行）

- **Journey ID（缝合后）**：取整条缝合链最早 lineage 的 client + code，覆盖到最晚 lineage 的结束时间——`deriveID` 从"单 lineage"泛化为"lineage 链"。
- **`Splice` 判据**：`ReplaceTail` 形状的边里，prev 尾部至少 2 条消息原样重现在 cur 尾部末尾则重分类为 `Splice`；不分裂 lineage（与 `ReplaceTail` 相同）。
- **缝合判据**：blob 倒排索引找每个断裂 lineage 首 manifest 的最佳覆盖前驱；设计文档原定的四类边（splice/compaction/head_prune/same_chat）在 `ctxgraph` 层收缩为三类（`splice` 与 `compaction` 的区分依据在这一层拿不到，见 §6.2 实施记）；`same_chat` 默认不缝合，只标"疑似同源"；三态处理对应 F4。
- **跨 SessKey 时间窗**（`stitchCrossBucketMaxGap`，6 小时）：真实语料验证中发现的必要修正——见 D.4。

### D.3 真实语料验证结论（全部 15 文件 / 7112 条记录）

- **F9 不变量**：`tool_call`/`tool_result` 配对 406534/406534，零孤儿。
- **一致性测试**（T2.5）：752 个 session 中 718 个与单个 lineage 1:1 对应，34 个被 ctxgraph 正确切成多段（F6 模式），**0 个反向违规**（无 lineage 横跨多个 report session）。
- **编辑类型分布**：`append 96.25%`、`replace_tail 2.68%`、`splice 0%`、`contract 0.43%`、`fork 0.65%`，829 lineages。
- **缝合**：68 个断裂中 62 个成功缝合（24 compaction + 38 head_prune）、6 个正确识别为"疑似同源"未缝合、0 个跨桶时间窗违规。
- **行为剖面**（T2.4）：`vmr story -render-all` 全量渲染 226 个候选 journey，0 panic，全部 `.md`+`.json` 通过解析校验；s231 复测的净工作时长（模型时间 380.983s/18.40%、Agent 侧间隙 88.276s/4.26%）与设计文档 A.6/F10 的手算值（381.0s/18.4%、88.0s/4.3%）几乎逐位吻合。

### D.4 走查中发现并修复的问题（含本轮复核新发现）

| # | 问题 | 发现方式 | 修复 |
| --- | --- | --- | --- |
| 1 | `internal/story/render_md.go` 用预估 token 格式化函数（`EST` 后缀）渲染实际用量 | golden test | 新增包内私有 `fmtTokens`，不带 `EST` 后缀 |
| 2 | 缝合边界任务标题把"前驱段已出现过的共享开场白"误认成新指令，标题重复 | 端到端测试 `TestStitchedJourney_EndToEnd` | 新增 `newInstructionTitleAtStitch`，跳过全局 `seen` 里已出现的候选 |
| 3 | 跨 SessKey 缝合匹配无时间窗约束，产生 190+ 小时的假阳性（定时任务模板文本巧合高分） | 真实语料验证（"太顺利"的 68/68 全部缝合、0 无匹配，反常） | 新增 `stitchCrossBucketMaxGap`（6 小时，仅限跨桶匹配） |
| 4 | **`resolveStitch`/`findSameChatCandidate` 同分/同 gap 候选的选择依赖 Go map 遍历顺序，非确定性**——同一语料重复跑 `StitchGraph`，部分 lineage 的缝合前驱在不同次运行间随机变化，进而导致 `vmr story` 候选列表的成员集合、Journey ID 都可能一次多几个一次少几个 | 本轮复核（连续多次运行 `StitchGraph` 并对比结果的一致性） | 加入"分数相同 → 时间距离更近者胜出 → 仍相同则 Idx 更小者胜出"的确定性平局打破；新增回归测试 `TestStitchGraph_TiedScoreCandidatesPickDeterministicWinner`；修复后独立跑两次全量渲染，216 个文件、文件名和内容 `diff -rq` 零差异 |
| 5 | 三处测试覆盖缺口：`HumanInitiated` 在缝合边界的取值（有/无新指令两种情况）此前无直接测试；`journey-<id>.json` 的写出此前只检查文件存在，不检查内容有效 | 本轮复核 | 补齐 `TestHumanInitiated_StitchBoundaryWithGenuinelyNewInstruction`、`TestStitchedJourney_EndToEnd` 里补一条断言、`TestCmdStory_ListAndRender` 改为反序列化校验 JSON |

**S2/`Splice` 的诚实澄清**：F11 附录 A.4 的真实 S2 案例（s231 turn20→21）走的是 `Contract` 分支（大幅缩减），并不落在 `Splice` 的字面判据（"公共前缀之后有新 blob，且原尾部一段在新 manifest 里原样重现"）里——`Splice` 判据忠实实现了设计公式，只是这个模式在当前语料的"未大幅缩减"边里目前没有真实样本命中（全语料 `Splice` 命中数为 0）。`revision` 关系的检测因此没有绑定在 `EditKind==Splice` 上，而是在 `Splice` 边上独立触发（`Event.Revises`），保留了机制本身，只是暂无真实触发案例。

### D.5 已知限制 / 遗留事项（截至 v1.2）

- ~~D1 的 `t-` 前缀未实现~~ **已修复（v1.2）**：见 §11 D1 与附录 F.1（改用文件名 `-partial` 后缀，而非 ID 前缀）。
- **缝合阈值未做更大语料的重新校准**：`stitchCompactionScore`(0.5)/`stitchHeadPruneScore`(0.15) 是初始取值，真实案例里出现过 s231 的缝合分数（0.33）落在 `head_prune` 而非直觉上更贴切的 `compaction`——不是 bug（0.33 如实反映了内容重合度不算高），但提示阈值值得在有更大语料时复核。
- **`ErrorRecoveryCount` 只识别 Anthropic 的 `is_error` 字段**：OpenAI 协议无对应标准标记，纯 OpenAI 语料下这项指标会偏低估——已在代码注释里如实记录。
- **`internal/chatmsg/pairing_test.go` 缺一个 Anthropic 版本的"孤儿 result"测试**：现有覆盖了 OpenAI 孤儿 result + Anthropic 孤儿 call，两种协议的孤儿检测走同一段共享代码，风险低，可选补充。
- ~~第 3 步尚未开工~~ **已完成（v1.2）**：`report` 自身的 compaction linking 告警、`context_growth` 脏值、lineage 粘连均已修复，见附录 F。

---

## 附录 E：第 3 步范围重新评估与规划（2026-07-30）

### E.1 现状核对：原计划的假设是否还成立

原第 3 步计划（附录 C.5）写于第 2 步开工之前，基于的假设是"`ctxgraph` 的缝合/剖面能力尚不存在，第 3 步要在做迁移的同时补齐这些能力"。第 2 步实际完成后，重新核对 `internal/report/session.go`（878 行，未改动）与 `internal/ctxgraph`/`internal/story` 的当前状态：

| 原计划子任务 | 原假设 | 现状核对结果 |
| --- | --- | --- |
| T3.1 迁移 session 分组 | `session.go` 有私有 `keys []string` 字段（第 52 行）+ `lcp()`（第 765 行）+ `group()`（第 582 行）+ `linkCompactions()`（第 779 行，200 字节文本 needle） | **原样存在，未受第 2 步影响**。`ctxgraph.Lineage`/`ctxgraph.StitchGraph` 已经是现成的、已测试的替代数据源——T3.1 现在是纯粹的"换数据源"，不需要再设计任何新算法 |
| T3.2 修 `context_growth` | `metrics.go` 的 `context_growth = last_in/first_in`（按时序） | **原样存在**，×179.5 脏值的根因（跨 compaction 计算比值）未变，方案（"按 lineage 分段计算，取最长段"）不需要调整 |
| T3.3 新增 `section_compaction.go` | 需要"本期发生 N 次 compaction、每次前后 token、信息损失率"——第 3 步写这些计算 | **信息损失的计算逻辑已经在第 2 步写出来了**（`internal/story/journey.go` 的 `extractEntities`/`buildCompactionInfo`），但是**私有**且绑定在 `story.CompactionInfo`（Journey/Step 粒度）上，`report` 不能 `import "vmr/internal/story"`（archtest 边界，`report` 只能依赖 `ctxgraph`）——需要一个新决策，见 E.2 |

**结论：原计划的范围假设基本仍然成立，只有 T3.3 出现一个第 2 步开工前不可能预见的新决策点**（因为当时 `extractEntities` 还不存在）。

### E.2 范围调整：`extractEntities` 的复用位置

T3.3 需要的"文件路径/URL 实体粗筛"逻辑，第 2 步已经在 `internal/story/journey.go` 写出来一份（`entityRe` 正则 + `extractEntities` 函数，~25 行，零外部依赖，只依赖 `regexp`）。`report` 做自己的 compaction 章节时需要等价的能力，两个选择：

- **方案 A（复制）**：在 `internal/report` 里再写一份等价的 `extractEntities`，~25 行代价，零耦合风险，但两处逻辑以后可能独立漂移（这条规则以后调整时容易漏改一处）。
- **方案 B（下沉）**：把 `extractEntities`（及其正则常量）从 `internal/story/journey.go` 挪到 `internal/chatmsg`——两个包都已经依赖 `chatmsg`，这是唯一不违反 archtest 边界、又能避免逻辑漂移的共享点。`story` 侧改成调用 `chatmsg.ExtractEntities`，行为不变，只是搬家。

**建议方案 B**：这是一次性、低风险的搬迁（纯函数，无状态，已有测试可以原样带过去），换来的是"以后只有一处实体粗筛规则"，符合 D5/R5"启发式收敛，不要膨胀"的既有原则。计入 T3.3 的成本增量约 0.1–0.2 人天（搬文件 + 改两处 import + 跑现有测试），不改变 T3.3 的总体量级（仍是 1 天）。

### E.3 可行性评估

**高。** 原计划里唯一的不确定性——"迁移会不会引入回归"——的两个前置条件都已在第 2 步满足：

1. **一致性测试已交付并全绿**（T2.5，附录 D.3）：752 会话全量比对，`knownImprovement` 白名单每条都有 F 编号支撑。这正是设计文档 §7.2 反复强调的"没有它，Phase B 会变成永久分叉；有了它，Phase B 是机械迁移"的那个前提。
2. **缝合逻辑本身已经过真实语料验证且确定性已修复**（附录 D.4 问题 4）：T3.1 要读取的 `ctxgraph.StitchEdge` 不再有"同一语料跑两次结果不一样"的风险——这条如果没在第 2 步复核阶段发现并修掉，会直接传导成 `report` 新 compaction 章节的输出不稳定，是那种一旦交付到 Phase B 才会暴露、返工成本高得多的问题。

唯一新增的设计决策（E.2 的 `extractEntities` 归属）范围小、风险低、有明确推荐方案，不构成可行性障碍。

`session.go`/`session_test.go`（534 行现有覆盖）没有因为第 2 步的任何改动而变化，原计划的回归安全网原样有效。

### E.4 必要性评估 / ROI

**必要，且比原计划写作时更清楚为什么必要**：

1. **修复的是 `report`（团队日常在用的工具）里三个已经确认、仍在发生的真实缺陷**，与 `story` 今后是否继续投入无关——即使团队决定第 4 步四个可选模块都不做，第 3 步依然独立值得做：
   - compaction linking 告警：本轮复核实测（当前语料）打出 12 条告警（5 个不同时间点），其中至少 1 个（s231→s238）已确认 ctxgraph 能正确缝合，其余因为新旧两套系统的会话切分逻辑本身不同、逐点核对性价比低——**这恰恰是 T3.1 要解决的问题本身**：迁移之后，这条旧告警连同它背后的 200 字节文本 needle 一起被删除，不再需要逐点对账；
   - `context_growth` 的 ×179.5 脏值：未变，仍在影响 `vmr report` 里"上下文膨胀"这条发现的可信度；
   - F6 的 lineage 粘连：已通过一致性测试精确量化（34/752 个 session 被粘连），`report` 目前仍按粘连后的粗粒度分组。
2. **为 Phase 4d（双 Journey 对比）铺路的价值不受影响**——4d 消费的是 `story.Metrics`，不依赖第 3 步；但第 3 步做完后 `report` 自己的 compaction 章节（CCR N-4 的落地）能反过来给"要不要花 4a 的钱上 LLM 解读层"提供更多不上 LLM 就能看到的证据，间接降低 4a 的必要性。
3. **成本没有变化**（仍是 3 人天，T3.3 增加的 0.1–0.2 天在四舍五入误差内），**风险因为第 2 步的验证工作而降低**（E.3）——ROI 相对原计划只有更好，没有变差。

### E.5 结论与建议顺序

**第 3 步值得做，范围基本不需要调整**，仅有 E.2 一处新增的小决策点（建议方案 B：下沉 `extractEntities` 到 `chatmsg`）。建议执行顺序维持原计划 T3.1 → T3.2 → T3.3：

1. T3.1（1.5 天）：`session.go` 分组改为消费 `ctxgraph.Lineage`；`linkCompactions()` 改为直接读 `Lineage.Stitch`（`StitchEdge.Kind`/`Score`/`Confidence`），删除 `keys`/`lcp()`/文本 needle；`session_test.go` 按一致性测试的白名单更新。

   > **实施后的偏差（见附录 F.2）**：`keys`/`lcp()` 确实删除了（分组不再自己算哈希，改读 `ctxgraph.Manifest`）；但 `linkCompactions()` 的文本 needle **没有删除**——`ctxgraph.StitchGraph` 只解析同 `SessKey` 桶内的断裂（`BrokeFrom != nil`），标准 compaction 调用的输入/输出往往与前后会话零字面重合（渲染过的摘要文本，不是逐字消息），跨桶且零重合时 `Stitch` 机制天生无信号可用。方案改为**新增、并列**的 `linkStitchedLineages`（读 `Stitch` 数据，覆盖同桶断裂 —— 即 F6 类型）而不是替换 `linkCompactions`（继续覆盖跨桶、走文本证据的标准 compaction 调用）。两者职责不重叠，`linkCompactions` 只在 `ContinuedFrom` 仍为空时才写入，不会互相覆盖。
2. T3.2（0.5 天）：`context_growth` 改为按 lineage 分段计算、取最长段。

   > **实施后的简化（见附录 F.3）**：T3.1 落地后一个 `SessionInfo` 已经严格等于一个 `ctxgraph.Lineage`（Contract/Fork 边界处必然拆分），"分段取最长段" 因此没有独立代码可写——每个 session 本来就是唯一一段。只补了一条回归测试（`TestContextGrowthDoesNotCrossContractBreak`）和一句代码注释，验证这条推论。
3. T3.3（1–1.2 天，含 E.2 的下沉）：新增 `internal/report/section_compaction.go`（= CCR N-4）；`extractEntities` 下沉到 `internal/chatmsg`，`story` 侧改用共享版本。

实施记录（走查发现、真实决策、测试结果）见**附录 F**。

**验收标准维持原计划**：一致性测试仍全绿；`vmr report` 对全语料输出的会话数与 journey 数可解释地对应；`go test ./internal/archtest/...` 全绿；额外建议加一条——`extractEntities` 下沉后 `internal/story` 的既有测试（`compaction_test.go`/`metrics_test.go` 等涉及实体抽取的用例）不改断言、原样通过，证明搬迁没有改变行为。

---

## 附录 F：第 3 步实施记录（2026-07-29，v1.2）

第 3 步（T3.1/T3.2/T3.3）已按附录 E 的评估开工并完成。本附录记录实际交付物、与 E 的计划文本之间的诚实偏差、真实测试结果，风格同附录 D（第 1/2 步）。

### F.1 交付物

| 子任务 | 新增/改动 |
| --- | --- |
| D1（顺带修复） | `internal/story/journey.go`（`Journey.Partial` 字段实际被赋值）；`cmd/vmr/cmd_story.go`（`renderJourney`/`renderAllJourneys` 计算并回填 `Partial`；`writeJourneyFile` 按 `Partial` 追加 `-partial` 文件名后缀）；新增 `TestCmdStory_PartialHeadFilenameSuffix` |
| T3.1 | `internal/report/session.go`：`AnalyzeSessions` 新增一个与 `collect()` 并发运行的 `ctxgraph.Scan`+`StitchGraph` 通道；`ReqInfo` 删除 `keys`/`sysKey`/`anchor`/`leadSys`，新增 `manifest *ctxgraph.Manifest`；`group()` 改为按 `ctxgraph.Lineage` 分组（一个 Lineage 一个 `SessionInfo`），新增 `linkStitchedLineages`；`attach()` 改用 `ctxgraph.Classify` 取代自算 LCP；`collect()` 删除哈希计算，`leadSys` 降级为函数内局部变量；`linkCompactions()` 保留但新增"不覆盖已有 `ContinuedFrom`"的判断；**删除** `internal/report/usage.go`（先删掉随之失去唯一调用者的 `nested()`，复核确认其邻居 `num()` 也已无调用者后一并清理，见附录 F.5）；`internal/archtest/import_boundaries_test.go` 更新过时注释（`report` 现在合法依赖 `ctxgraph`）；**删除** `internal/report/ctxgraph_parity_test.go`（迁移后其存在理由——校验两套独立哈希实现是否漂移——不再成立）；`session_conformance_test.go` 的 `TestConformance_F6AnchorGluedLineageSplitIsKnownImprovement` 改写为 `TestConformance_F6AnchorGluedLineageSplitMatchesCtxgraph`（"已知改进"变成"默认行为"），其私有 `recLoc` 类型上移到 `session.go` 生产代码避免重复声明 |
| T3.2 | `internal/report/metrics.go` 补充注释说明因果关系；`internal/report/aggregate_test.go` 新增 `TestContextGrowthDoesNotCrossContractBreak` |
| T3.3 | 新增 `internal/chatmsg/entities.go`（`ExtractEntities`/`MaxEntities`，原 `internal/story/journey.go` 的 `entityRe`/`extractEntities`/`maxCompactionEntities` 原样搬来，`story` 侧改为一行委托，行为不变）+ `entities_test.go`；`internal/report/rows.go` 新增 `CompactionRow` + `Report2.Compactions`；`internal/report/aggregate.go` 新增 `buildCompactions`；新增 `internal/report/section_compaction.go`（§6.7，符合"新增一节 = 新增一个文件"预算）；`render_doc.go` 在 §6.6 端点性价比之后、§7 效率之前插入该节；`aggregate_test.go` 新增 `TestBuildCompactionsEntitySplitAndTokens` |

全部新增/改动文件均有随附单测；全量测试（含 `-race`、`archtest`）保持全绿。

### F.2 关键决策：linkCompactions 为什么没删

附录 E 的原计划设想 `linkCompactions()` 的 200 字节文本 needle 可以整体替换为读 `ctxgraph.Lineage.Stitch`。实施时用 `internal/report/session_test.go` 的 `fixture()`（linkCompactions 的规范测试场景：一次独立的 compaction 摘要调用，前后两个会话锚点完全不同）反向验证，发现：

- `ctxgraph.StitchGraph` **只解析 `BrokeFrom != nil` 的 lineage**（即同一个 `SessKey` 桶内部发生的断裂）；
- 标准的"独立 compaction 调用"（design doc F2/F3 的 S1 型）在 `fixture()` 这类零字面重合场景下，前后两个会话本来就是**不同的 `SessKey` 桶**（不同锚点），且 compaction 调用自己的输入/输出是"渲染过的摘要文本"，与前后会话没有任何逐字消息重合——`ctxgraph` 的哈希倒排索引在这种情况下没有任何信号可用，`resolveStitch` 也不会被触发（它只服务于同桶断裂）。

F2 附录 A.2 里那个"42/58 命中"的真实成功案例之所以能用哈希匹配，**匹配的是后续仍逐字复用的工具调用/结果消息，不是摘要文本本身**——这是两种不同的证据来源，`fixture()` 测的是后者不存在时的情形。

**结论**：两种链接信号覆盖的是不同的现象，不能互相替代：

| 信号 | 覆盖场景 | 依据 |
| --- | --- | --- |
| `linkStitchedLineages`（新增，读 `Stitch`） | 同 `SessKey` 桶内的断裂——典型是 F6 的"锚点存活"型（in-place rewrite / S2/S3），即使没有独立的 compaction 调用也能连上 | 结构性哈希重合，高置信度 |
| `linkCompactions`（保留，文本 needle） | 跨桶、且 compaction 调用输入/输出与前后会话零字面重合的标准调用（S1 型） | 200 字节子串匹配，低置信度但是当前唯一还有信号的手段 |

因此最终实现是**两条并列信号**：`group()` 内部先跑 `linkStitchedLineages`（设置部分 `SessionInfo.ContinuedFrom`），`linkCompactions()` 随后只在 `ContinuedFrom` 仍为空时才写入（一行判断防止互相覆盖）。这不是"没做完"，而是"E 计划写作时对 `ctxgraph.StitchGraph` 的覆盖范围有一处过于乐观的假设，实测后予以纠正"——同样是 §11 D2/D3 一贯坚持的"诚实记录，不是打补丁掩盖"的风格。

### F.3 关于 context_growth 的诚实澄清

附录 E 认定 T3.2 的方案（"按 lineage 分段计算，取最长段"）不需要因为 T3.1 而调整。实施后发现这句话本身**部分不再需要**：T3.1 让 `group()` 按 `ctxgraph.Lineage` 分组之后，一个 `SessionInfo` 就是严格意义上的一个 Lineage——Contract/Fork 边界处必然产生新的 `SessionInfo`，同一个 session 内部不可能再出现跨越"历史重置"的记录。"分段取最长段"这个操作因此没有独立代码可写，因为分段已经在 `group()` 里免费发生了。

用 `TestContextGrowthDoesNotCrossContractBreak` 验证：一个正常增长到 8000 token 又被压缩重置到 150 token 再增长到 900 token 的场景，压缩前 session 的 `ContextGrowth` = 80（100→8000，诚实反映其自身范围内的增长），压缩后 session 的 `ContextGrowth` = 6（150→900，同样诚实），互不污染——如果依然用旧的"整会话 last/first"算法，这两段会被粘成一个 session，比值会是 900/100=9，一个既不是 80 也不是 6 的、跨越了一次隐藏重置因而没有实际意义的数字。

### F.4 真实测试结果

- 全量 `go build ./...` + `go vet ./...` + `go test ./...` 全绿（含 `cmd/vmr`、`internal/report`、`internal/ctxgraph`、`internal/story`、`internal/archtest`）。
- `go test ./internal/report/... ./internal/ctxgraph/... ./internal/archtest/... ./internal/chatmsg/... ./internal/story/... -race -count=1` 全绿——`AnalyzeSessions` 新增的"`ctxgraph.Scan` 与 collect() 并发跑"这条并发路径本身也在 race 覆盖范围内。
- `session_conformance_test.go` 的 F6 场景（原"已知改进"白名单唯一条目）现在是**默认行为**：`TestConformance_F6AnchorGluedLineageSplitMatchesCtxgraph` 验证 report 现在也把 anchor 存活型断裂拆成两个 session，且第二个 session 的 `ContinuedFrom` 正确指回第一个（`linkStitchedLineages` 新增能力，此前 report 连"拆开"都做不到，更谈不上"拆开之后还能连回去"）。
- `TestAnalyzeSessionsGrouping`/`TestLinkCompactionsLogsMiss`/`TestNoReplyMergesRetryIntoSameTask` 等第 1/2 步就已存在的 `session_test.go` 全部**无需修改**、原样通过——这是"迁移是机械操作而非重写"（附录 E.3 的可行性判断）在实测中的直接证据。

### F.5 未在计划内、顺手清理的技术债

- `internal/report/usage.go` 的 `nested()` 因为 T3.1 删除了它唯一的调用者（`collect()` 里的 `metadata.user_id` 读取）而失去存在理由，一并删除。它的邻居 `num()` 复核后确认在改动前就已经没有任何调用者（全仓库搜索确认——`internal/chatmsg` 有自己的同名私有函数，两者是不同包的独立副本，不是同一个符号）；`num()` 删除后整个 `usage.go` 就只剩一句过时的包级注释，因此在本轮审查清理中把 `num()` 与整个文件一起删除，而不是留着一个空壳文件。
- `internal/archtest/import_boundaries_test.go` 的 "internal/report 只依赖 audit/core" 这句过时注释一并更新——它在第 3 步之前是对的，之后就是错的，放着不管会误导下一个读这份代码的人。

### F.6 性能取舍：AnalyzeSessions 现在扫描每个审计文件两遍

T3.1 让 `AnalyzeSessions` 内部同时跑 `ctxgraph.Scan`（供 `group()` 消费）和它自己原有的 `collect()` 并发文件读取通道——两者都独立解析同一批审计文件。这在设计文档 Appendix E 的可行性评估里没有被提及（E.3 只讨论了回归风险，没讨论吞吐）。

处理方式：两个通道用 goroutine 并发跑而非先后顺序执行，把"读两遍文件"的代价从"墙钟时间翻倍"降到"CPU 占用翻倍、墙钟时间大致不变"（`vmr report` 是偶发运行的离线批处理命令，不是常驻热路径，短暂的 CPU 超订阅可以接受）。这是一个已知、已记录、当前语料规模下无感的权衡，不是遗留 bug——但如果未来 `vmr report` 需要处理明显更大规模的语料，或者被搬进某个延迟敏感的场景，这里是第一个该回头看的地方。

### F.7 一致性测试的性质变化（给第 4 步及后续维护者的提示）

`session_conformance_test.go`（原 T2.5，R8 的唯一缓解手段）在第 3 步之前比较的是**两套独立实现**（report 自己的分组 vs ctxgraph 的分组），任何差异都必须落在"应当一致"或"已知改进"两个白名单之一，这是它能防止"Phase A 与 report 永久分叉"的原因。

第 3 步之后，`group()` 直接消费 `ctxgraph.Scan` 的输出——这份测试现在比较的更接近"同一份数据被两种方式读出来是否一致"，而不是两套独立算法的交叉验证。它依然有价值（防止 `group()` 未来的改动悄悄读错 `ctxgraph` 的输出、以及端到端验证 F6 修复 + `linkStitchedLineages`），但不再是"两个独立事实来源互相校验"意义上的强保证。这一点在规划第 4 步（尤其是 4d 双 Journey 对比，如果它未来也要消费 `report` 的分组结果）时值得记住：`report` 和 `ctxgraph`/`story` 现在共享同一个分组事实来源，不是两条独立证据链。

---

## 附录 G：第 4 步计划复核（基于第 3 步实况的微调，2026-07-29）

第 3 步完成后回看 §C.6 的四个可选模块，结论：**优先级排序不变（4d → 4a/4b/4c 按需），但三处细节值得更新。**

**4d 双 Journey 对比，进一步降本**：第 3 步交付的 `internal/report/section_compaction.go`（§6.7）本身就是"用规则语言描述一次 compaction 的前后差异"的一个小型实例——`buildCompactions` 里"tokens_in→tokens_out + 实体存活/吞没"这套比较逻辑，和 4d 需要的"两份 `story.Metrics` 做差"是同一类工作（拿两份结构化数据比较，产出人可读的差异表）。4d 的成本估计（1.5 天）不需要下调，但可以确认：4d 不需要等 4a（LLM 解读层）先做，纯规则的对比就已经有直接可用的实现范式可以照抄。

**4a LLM 解读层，必要性进一步降低**：§6.4 分析已经指出 report 自己的新 compaction 章节能给"要不要上 4a"提供更多免费证据。第 3 步实际交付后这个论点更具体了——现在 `vmr report` 单独就能回答"这次压缩把多少 token 变成多少 token、哪些文件路径/URL 消失了"，这恰好是 deepseek 报告里最耗人工的那类观察之一。建议：4a 开工前先看几期真实 `vmr report`/`vmr story` 的实际阅读体验，再决定值不值得上 LLM——这条建议在 v1.1 就有，第 3 步的交付让"不上也够用"的可能性又高了一些，不是结论性的，只是概率上更值得先观望。

**新增一条第 3 步暴露出的、值得在第 4 步开工前排查的技术问题**：附录 F.2 记录了 `ctxgraph.StitchGraph` 无法覆盖跨 `SessKey` 桶、零字面重合的标准 compaction 场景。如果 4d（双 Journey 对比）或未来的 subagent（4c）需要可靠地把"压缩前"和"压缩后"两个 Journey 串成一条时间线，会撞上和 `linkCompactions` 完全相同的局限——`ctxgraph` 目前没有能力单靠内容重合桥接这类断裂。这不阻塞 4d/4c 开工（多数真实场景要么同桶断裂、要么 4d/4c 本就只关心单个 Journey 内部的指标对比，不强依赖跨 Journey 缝合），但如果开工后发现"两个本该连起来的 Journey 连不上"，第一反应应该是"这是已知限制，不是新 bug"，去查附录 F.2 而不是重新调试 `StitchGraph`。

---

## 附录 G 补记：4a/4d 实际落地记录（2026-07-30）

用户明确要求丰富 4d 的对比报告，并给出了两份 Journey（`j-openclaw-20260727T160544-20260727T161259-8b175da9` 22 轮、`j-lobster-20260727T160549-20260727T162156-d6b04665` 58 轮）作为具体案例，指定以 `docs/openclaw_dual_instance_analysis_2026-07-28_v1.0_deepseek-v4-pro.md` 那份人工分析报告的深度为参照（但明确要求不照抄它的框架和结论，跳出来以第一性原理判断该对比哪些维度）。

### G.1 决策过程

1. 先制定初版计划（`docs/Step4a_compare_LLM解读层_实施计划与执行记录_2026-07-29_sonnet-5.md` v1），核实了 deepseek 报告里的证据链在当前代码上是否真的可行——确认 `ctxgraph.Manifest` 已经带 `Endpoint`/`Usage{In,Out,CacheRead,CacheWrite}`，模型端点核查和缓存命中率曲线两节可以零 LLM 成本直接派生。
2. 与用户确认两处简化：LLM 端点只支持手动指定一个已部署的 VMR 实例（不做 config.yaml 直连上游）；LLM 输出先做自由格式 Markdown（不做结构化 JSON）。
3. 另一份 coding agent 独立给出了一份实施建议（`docs/Step4a_LLM增强对比报告_实施计划.md`），逐节核对源码后原文内联批注：骨架判断（两层分离、LLM 走 VMR 端点、缓存/降级/dry-run）与本方案独立吻合，可以互相印证；但它把"system prompt 里识别文件名/工具清单"和"迭代阶段划分"设计成正则/启发式规则层，评审判定为方向性错误——这两件事本质需要语义理解，规则做只会是脆弱、错了也不会显式暴露的伪精确，且违反"内核不认框架、框架特有识别归 profile 层"的既有原则；改为把有边界的原文节选和逐轮工具索引交给 LLM 自己读、自己标注"这是我的理解"。它的报告结构（对齐 deepseek 报告的 11 章）也被判定为"照抄章节数，不是按独立维度组织"，最终合并方案精简为 6 个正文小节。评审还发现它的缓存 key 缺了 `model`（换端点会误命中旧缓存的具体 bug）、以及两处"待验证"的技术前提其实已经有现成答案（system prompt 全文已经在 `Journey.Events` 里、cache 字段已经在 `Manifest.Usage` 里，都不需要重新解析原始 body）。
4. 复核过程中按第一性原理补了一项两份方案都遗漏的维度：**最终交付物对比**——如果任务产出是通过一次"参数形状像文件写入"的工具调用落盘的，规则层可以直接把它的内容摆出来，这是 VMR 唯一能直接看到"结果差异"而非"过程差异"的证据，也是 deepseek 报告里人工比较两份产出报告篇幅/结构那部分工作的自动化版本。
5. 按最终版 `docs/Step4a_compare_LLM解读层_实施计划与执行记录_2026-07-29_sonnet-5.md` 实施代码，全部测试通过（含 `archtest`）后，用两个真实 Journey + 一个本地 mock OpenAI 兼容端点跑了一次完整的端到端验证。

### G.2 实际交付物

- `internal/story/compare.go`：`Comparison` 新增 `Extras *ComparisonExtras`（`ComputeComparisonExtras`），六类规则事实：端点核查、缓存曲线、system prompt 规模/稳定性（含节选）、末轮上下文构成（免费，直接取 `Metrics.ContextCurve` 最后一项）、总耗时+终止方式、最终交付物节选。
- `internal/story/render_compare.go`：渲染上述小节，`Extras` 为 nil 时整段跳过（向后兼容，不影响任何既有 `Compare(JourneySummary, JourneySummary)` 调用方）。
- `internal/story/llm.go`（新文件）：`EvidencePack` 组装、prompt 模板（数字不可编造 / 文本节选可以是模型自己的阅读理解但要declare / 必须声明 VMR 看不到什么）、纯 `net/http` 的 OpenAI chat-completions 客户端、磁盘缓存（key 含 model）、`RenderLLMSection`。
- `cmd/vmr/cmd_story.go`：新增 `-llm-addr`/`-llm-model`/`-llm-key`/`-llm-dry-run`，`-llm-addr` 是唯一开关；失败只警告、不影响 `.md`/`.json` 正常产出。
- 测试：`internal/story/compare_test.go`（`ComputeComparisonExtras`/渲染新增小节）、`internal/story/llm_test.go`（`httptest.Server` 覆盖端点解析/缓存/失败降级/dry-run）、`cmd/vmr/cmd_story_test.go`（CLI flag 校验 + 真实 mock 端点端到端）。`go build`/`go vet`/`go test ./...`（含 `-race`、含 `archtest`）全部通过。

### G.3 端到端验证结果（两个真实 Journey + 本地 mock LLM 端点）

规则层复现的数字与 deepseek 报告手工核对出的数字一致：22 轮 vs 58 轮、缓存命中率 18%→97%（Standalone/A）vs 82%→99-100%（Lobster/B，R54 88% 的异常也复现了）、system prompt 17.1K vs 20.5K tokens、双侧端点均为 `openai:opencode:deepseek-v4-pro`（相同）。最终交付物对比正确识别出两侧 `write` 调用的真实报告内容（A 在第 21 轮、B 在第 56 轮）。LLM 解读小节按预期渲染，带完整的"不是事实层"免责声明。

### G.4 保留、遗漏与待定事项（诚实清单）

- **范围明确收窄**：本次只做了 4a 的 4d（compare）场景切片，不是设计文档 4a 原定范围的单 Journey 逐 Step 解读——那部分仍未开始，如果以后要做，`internal/story/llm.go` 的证据包/客户端/缓存代码大部分可以直接复用，只是证据包的内容需要换成单 Journey 的 Step 序列。
- **LLM 输出仍是自由格式 Markdown，未做结构化 JSON + 程序化校验**——按计划，这是有意的第一版简化，不是遗漏；如果实际使用中发现模型经常编造数字或格式不稳定，下一步应该收紧成强制 JSON schema。
- **只支持指向一个手动部署好的 VMR 实例，不支持直连 config.yaml 里的上游**——同样是按用户确认的简化，技术上直连 config.yaml 完全可行（`internal/replay` 已经证明），只是当前场景没有必要做两条路径。
- **只支持 OpenAI 协议**，不支持 Anthropic——如果未来需要，`internal/story/llm.go` 的客户端代码需要单独适配 Anthropic 的 `/v1/messages` 形状，目前完全没做。
- **不支持健康检查/failover/重试**——一次失败就是失败，直接降级为"没有 LLM 解读小节"，符合"简单优先"的既定原则，但意味着一次瞬时网络抖动就会让当次运行拿不到解读（重跑一次通常就好，缓存也不会因为一次失败而写入脏数据）。
- **最终交付物识别是启发式的（参数形状像文件写入），不保证覆盖所有 Agent 框架的落盘方式**——如果某个框架的写文件工具用了完全不同的参数命名（既不是 path/file_path 类，也不是 content/text 类），会如实报告"未识别到可比较的最终交付物"，不会强行凑一个错误的匹配；这是"宁可断开，不要错连"原则在这个新功能上的应用，不是 bug。
- **未处理 subagent 场景**——如果某个 Journey 涉及 subagent 分支（4c 尚未开工），当前的证据包组装只看主 lineage 的 Step，不会包含 subagent 的执行细节。
- **未做 -redact 脱敏**——证据包和渲染出的最终交付物/system prompt 节选仍然是明文，`compare-*.md`/`.json`/缓存文件继承 `stories/` 现有的 0600/0700 权限，但脱敏（4b 范围）仍未实施，对外分享前需要人工审查内容。
