# VMR 报告重新设计

> 对 `vmr report` 的 markdown 输出（结构 / 索引 / 维度）进行第一性原理重新设计。
> 基于 `logs/vmr-audit-2026-07-24.jsonl`（182 条记录）生成的报告。
> 这是一份**设计文档**，不是生成的输出。未修改任何现有文件。

---

## 第 0 部分 - 摘要（TL;DR）

当前报告是一个**统一宽表倾倒器**：每个章节都复用同一套 9–11 列模板，派生信号（成本、缓存浪费、工具声明浪费、慢请求数量）都被埋在括号补充说明里，而总览只是不可读的单行。本次重新设计围绕**八个运维问题**重组报告，用**七个指标族**（每张表只挑选相关族）替代一刀切的列集合，并新增一个**效率/浪费**章节，量化那些本不必花费的 token、字节和金钱。

单一最高价值的改动：将**缓存效率**、**工具 schema 浪费**和**计费 token 类别**提升为一等命名指标，而不是 `Tokens In/CacheHit/Out` 单元格里的括号补充说明。

---

## 第 1 部分 - 现状（摘录）

### 1.1 输出索引（`vmr report` 如今生成的文件）

| 文件 | 粒度 | 作用 |
|---|---|---|
| `vmr-report.md` | 汇总 | 人类可读摘要（本次重新设计的对象） |
| `vmr-report.json` | 汇总 | 机器可读孪生文件（format 9） |
| `vmr-requests-index.md` | 单请求 | 下钻：Chat User -> Session -> Task -> Turn |
| `vmr-requests-index-<tag>.md` | 单请求 | 同上，按 `client_key_tag` 过滤（如 `-lobster`、`-pimini`） |
| `vmr-requests.jsonl` | 单请求 | 原始审计记录 |
| `details/<ts>_<model>_<outcome>.{md,json}` | 单请求 | 每请求一对文件（完整 req/resp 捕获） |
| `loadtest-report.md` | 汇总 | 独立，仅来自 `loadtest/runner` |

### 1.2 `vmr-report.md` 当前的章节目录

```
# VMR 用量报告
  头部（数据源、时间范围、记录数）
  1. Overview              （单一宽行）
     - 请求消息字符及占比    （按角色字符）
     - finish_reason 数量及占比
     - thinking tokens 数量及占比
  2. 按模型                 （按虚拟模型 × 协议）
  3. 端点可用度              （按端点）
  4. 上游错误分布            （按端点的错误类别）
  5. 按日趋势                （按日期，仅当跨天时）
  6. 每小时活跃度            （按本地小时，跨天合并）
  7. 工作负载                （interactive vs heartbeat vs dream_diary）
  8. Agent 会话             （按会话，定时一次性任务合并）
  9. 工具使用                （按请求形态：声明/调用/从未调用）
```

### 1.3 当前维度（列模板，几乎处处复用）

```
请求/回退/截断 | 成功率 | Tokens In/CacheHit(占比%)/Out | 图片/压缩
| 平均Tokens In/Out | 字节 In/Out | 平均消息数 | p50/p95 首字延迟
| p50/p95 请求耗时 | 平均吞吐(tok/s)
```

各视图额外列：
- **按模型**：+ 模型 | 协议
- **端点**：+ 端点 | 尝试 | 成功 | 可用度（去掉 成功率/avg/messages）
- **工作负载**：+ Tool 调用（调用数 + %含调用的请求）
- **会话**：+ 标题 | 任务 | 时段（去掉 成功率、平均 token、字节、吞吐）
- **工具**：+ 形态 | 声明数 | 声明字节 | 调用排名 | 从未调用列表

单请求详情列（索引）：
```
轮 | 时间 | Message | finish | 耗时 | 首字延迟 | Tokens In/CacheHit/Out | 图片/压缩 | 文件
```

### 1.4 当前设计做得好的地方（保留这些）

- **真实的每桶百分位。** 每个桶保留各自的原始 `dur_ms`/`ttft_ms` 并直接计算 p50/p95 —— 不做百分位取最大值的上卷。（format 9 不变式。本次重新设计保留这一点。）
- **预聚合的 `*_all` / `hours_of_day` 兄弟桶**，各自带真实百分位，因此跨天合并不是从已释放的按日期桶伪造出来的。
- **合并的定时一次性任务。** Heartbeat/diary 单记录会话折叠为每类一行 —— 保持会话表可读。
- **按客户端 key 的索引兄弟文件。** `vmr-requests-index-<tag>.md` 已存在。
- **详情文件捕获**（完整 req/resp，带可折叠 `<details>` 块）非常出色，不在本次重新设计范围内。

---

## 第 2 部分 - 批评（为何重新设计）

| # | 问题 | 2026-07-24 报告中的证据 |
|---|---|---|
| 1 | **统一的宽表。** 每张表无论是否相关都复用 10 列模板。 | 每小时表携带 图片/压缩 + 平均消息数，尽管没有哪个小时有图片，且消息数在那里是噪声。 |
| 2 | **没有成本维度。** 一个面向付费 LLM API 的路由器从不提及金钱或计费 token 类别。 | 显示了 18.9M 输入 token，但*计费*拆分（fresh 3.04M vs cached 15.9M）只是括号补充说明。 |
| 3 | **缓存效率被埋没。** Agent 流量的头号成本杠杆是一个单元格里的 `(84.0%)` 括号。 | Heartbeat 工作负载的缓存命中率是 **8.3%** —— 整份报告中最尖锐的浪费信号 —— 却一眼看不到。 |
| 4 | **没有派生效率指标。** 数据收集了，但从未作为命名信号呈现。 | 工具形态 `tools:67` 在 53 个请求中发送了 **5.94 MB** 的 schema 字节，却只用了 67 个工具中的 4 个（6.0% 利用率）。这个数字在报告中无处可寻。 |
| 5 | **百分位缺少基数。** p50/p95 显示时没有 `n=`。 | `agent/anthropic` 行显示 p95 25.6s 来自 **n=1** —— 与稳健的 p95 无法区分。 |
| 6 | **总览是不可读的单行。** 10 个单元格塞满密集数字。 | `182/⚠️ 2/- | 91.8% | 18.9M / 15.9M(84.0%) / 81.8K | - | 113.4K / 490 | 77.4M / 19.7M | 83.3 | 2.8s / 15.1s | 4.4s / 27.9s | 50.2` |
| 7 | **扁平的延迟模型。** 显示了 TTFT 和耗时，但它们的关系（流式时间 = dur − ttft）才是洞察所在。 | p95 TTFT 15.1s vs p95 dur 27.9s -> 约 12.8s 流式，但读者必须手动相减。 |
| 8 | **没有负载形态可视化。** 每小时视图是 17 行 × 10 列。 | 01:00 的峰值（100 请求）和 13:00 的延迟峰值（p95 44.3s）才是故事所在；一条 sparkline 就能一行讲清。 |
| 9 | **错误没有速率/时间。** `client × 1, rate_limit × 4` —— 没有每 100 请求的速率，也没有时间。 | `dashscope:glm-5.2` 有 12.5 错误/100 次尝试 —— 最差的端点 —— 但读起来和一个 3.7/100 的端点一样。 |
| 10 | **会话携带无用的延迟列。** | s03 每会话的 p50/p95 TTFT 是噪声；真正重要的是"128 轮、4 个任务、12 小时挂钟、16.3M token"。 |
| 11 | **工具章节是一个 63 行的从未调用列表。** | 可执行的洞察（"67 个工具中 63 个从未调用 = 5.5MB 浪费"）被埋在编号倾倒之下。 |
| 12 | **角色字符脱节。** tool 角色 = 77.1% 上下文 —— 单一最大的上下文优化杠杆 —— 却是一个扁平的要点。 | 从未跨工作负载比较，也未与"这就是你的上下文为何巨大"挂钩。 |
| 13 | **摘要里没有"按客户端"。** | `client_key_tag` 存在（pimini 129 / lobster 53）并驱动索引兄弟文件，但摘要报告从不按它拆分。 |
| 14 | **没有与上期的差值。** | 一份周期性报告最有价值的补充是"什么变了"；当前结构没有为它预留位置。 |

---

## 第 3 部分 - 第一性原理

### 3.1 这份报告是做什么用的？

`vmr` 是一个本地、单二进制的 LLM 路由器，面向个人/小团队的 agent 流量。报告读者是该部署的**运维者**。他们想按大致以下优先级回答：

1. **我花得太多吗？** 哪个模型/端点/会话在烧 token？缓存生效吗？
2. **它可靠吗？** 哪些端点不稳定？故障转移恢复了吗？
3. **它够快吗？** 慢点在哪？有多少请求慢到难以忍受？
4. **工作负载模式是什么？** 交互式还是定时？高峰小时？按客户端？
5. **我能优化什么？** 浪费的 token、过度声明的工具、冗余的定时 prompt、被截断的输出。
6. **发生了哪些对话？** 下钻找到某一个具体请求。

### 3.2 五条设计原则

- **P1 - 报告回答问题，而非展示数据。** 每个章节映射到一个运维问题。如果不映射，就删掉或移到 JSON。
- **P2 - 相关性优先于统一性。** 每个视图只显示对其问题重要的列。统一的巨型模板损害可读性。
- **P3 - 以派生信号为先。** 成本、缓存效率、上下文膨胀、慢请求数、浪费都是*派生的* —— 且比原始求和更可执行。把它们作为命名指标计算并呈现。
- **P4 - 渐进式披露。** 总览（10 秒扫视）-> 聚焦章节 -> 下钻（索引/详情）。每层增加细节。
- **P5 - 量化浪费，而不仅是用量。** 对成本敏感的路由器来说最有价值的指标是"花掉但本不必花的 token/字节/金钱"：未缓存的新鲜输入、过度声明的工具 schema、被截断的输出、冗余的定时任务 system prompt。

### 3.3 八个运维问题 -> 报告章节

| Q | 问题 | 章节 |
|---|---|---|
| Q0 | 发生了什么，用 5 个数字概括？ | §0 摘要 (Executive Summary) |
| Q1 | 实际/将花费多少？ | §1 成本与 Token 经济 |
| Q2 | 它可靠吗？ | §2 可靠性 |
| Q3 | 它快吗？ | §3 延迟与吞吐 |
| Q4 | 负载来自哪里？ | §4 负载分布 |
| Q5 | 发生了哪些对话？ | §5 会话与任务 |
| Q6 | 什么浪费/可优化？ | §6 效率与浪费 |
| Q7 | 如何找到某一个请求？ | §7 请求详单（索引，仅链接）+ details/ |

---

## 第 4 部分 - 新维度目录

用**七个指标族**替代单一扁平列模板。每张表只挑选它需要的族。每个指标都标注其**分母/基数**，让读者知道一个数字是在什么之上度量的。

### A 族 - 体量与结果
| 指标 | 定义 | 基数 |
|---|---|---|
| `requests` | 总请求数 | 全部记录 |
| `ok` / `errors` / `canceled` | 结果计数 | 全部记录 |
| `fallbacks` | 需要 >1 次上游尝试的请求 | 全部记录 |
| `truncated` | 2xx 但流式中途断开 | ok 记录 |
| `success_rate` | ok / requests | 全部记录 |
| `effective_success` | (ok + 恢复的 fallback) / requests | 全部记录 *（用户感知的可靠性）* |

### B 族 - Token 经济（成本导向）★ 新框架
| 指标 | 定义 | 基数 |
|---|---|---|
| `tokens_in` | 总输入（含缓存） | 带 usage 的记录 |
| `tokens_in_fresh` | in − cached − cache_write **★** | 带 usage 的记录 |
| `tokens_in_cached` | 缓存命中部分 | 带 usage 的记录 |
| `tokens_in_cache_write` | Anthropic 缓存创建（溢价计费） | 带 usage 的记录 |
| `cache_hit_rate` | cached / in | 带 usage 的记录 |
| `cache_efficiency` | cached / (cached + fresh) **★** | 带 usage 的记录 *（杠杆，而非标题）* |
| `tokens_out` | 总输出 | 带 usage 的记录 |
| `tokens_reasoning` | thinking-token 子集（out 的一部分） | 报告了它的记录 |
| `reasoning_share` | reasoning / out | 报告了它的记录 |
| `billable_tokens` | fresh·p_in + cache_write·p_cw + out·p_out **★** | 若配置了定价 |
| `avg_tokens_in` / `avg_tokens_out` | 每请求均值 | tokens_known |

### C 族 - 延迟与速度
| 指标 | 定义 | 基数 |
|---|---|---|
| `ttft_p50` / `ttft_p95` | 首字延迟百分位 | ttft_known *（始终标注 n）* |
| `dur_p50` / `dur_p95` / `dur_max` | 请求耗时百分位 + 最大值 | requests_with_dur |
| `stream_time_p95` | dur_p95 − ttft_p95 **★** | 派生 *（模型流式了多久）* |
| `slow_requests` | count(dur > 阈值) **★** | requests_with_dur *（阈值默认 30s）* |
| `throughput` | tok_out / s | 带 usage+dur 的记录 |
| `bytes_per_sec` | bytes_out / s | 带 dur 的记录 |

### D 族 - 线路与载荷
| 指标 | 定义 | 基数 |
|---|---|---|
| `bytes_in` / `bytes_out` | 线路字节 | 全部记录 |
| `avg_messages` | 每请求消息数 | messages_known |
| `role_chars` | 每角色字符（system/user/assistant/tool/developer）+ 占比 | messages_known |
| `images` / `images_compressed` | 内联图片计数 | 全部记录 |

### E 族 - 工作负载形态
| 指标 | 定义 | 基数 |
|---|---|---|
| `workload_class` | interactive / heartbeat / dream_diary / … | 分类 |
| `client_key` | `client_key_tag`（如 pimini / lobster）**★ 摘要中新增** | 全部记录 |
| `requests_per_task` | requests / tasks **★** | 会话级 *（工具调用扇出）* |
| `tool_calls` | 总工具调用 | 带调用的请求 |
| `tool_call_rate` | 带工具调用的请求 / 请求 | 请求 |
| `context_growth` | tokens_in(最后一轮) / tokens_in(第一轮) **★** | 会话级 |

### F 族 - 效率/浪费（派生，"那又怎样"）★ 新族
| 指标 | 定义 | 为何重要 |
|---|---|---|
| `cache_miss_tokens` | = tokens_in_fresh | 本*可以*缓存但未缓存的输入 token |
| `cache_efficiency` | cached / (cached + fresh) | 84%+ 健康；某工作负载 <30% 意味着 prompt 不可缓存 |
| `tool_schema_bytes_shipped` | declared_bytes × requests **★** | 作为工具 schema 发送的字节，求和 |
| `tool_declare_utilization` | distinct_called / declared **★** | 6% 意味着 94% 的 schema 字节是死重 |
| `tool_schema_waste_bytes` | schema_bytes × (1 − utilization) **★** | 死重部分，若有定价则货币化 |
| `scheduled_prompt_redundancy` | system_chars × scheduled_fires **★** | 每次 cron 触发都重发的字节；通过拉长间隔或换模型降低 |
| `output_truncation_rate` | finish=length / requests | 撞到 token 上限的输出 = 浪费，必须重试 |
| `slow_request_share` | slow_requests / requests_with_dur | 难以忍受地慢的流量占比 |

### G 族 - 端点健康
| 指标 | 定义 | 基数 |
|---|---|---|
| `endpoint` | provider:real-model | - |
| `attempts` / `ok` / `failed` | 尝试计数 | 尝试 |
| `availability` | ok / attempts | 尝试 |
| `error_rate` | failed / attempts × 100 **★** | 尝试 *（每 100，跨端点可比）* |
| `error_classes` | 类别 -> 计数 + 类别 -> 速率/100 **★** | 尝试 |
| `failover_recovered` / `failover_failed` | 恢复成功 vs 最终失败的 fallback **★** | fallback 记录 |

> **★** = 新增或从括号补充说明提升为一等指标。

---

## 第 5 部分 - 新报告结构

### 5.1 新目录

```
# VMR 用量报告
  ## §0 摘要 (Executive Summary)           - 5 个数字 + ≤3 条自动亮点
  ## §1 成本与 Token 经济 (Cost & Tokens)    - B 族（+ D 族角色字符）
  ## §2 可靠性 (Reliability)                - A 族 + G 族
  ## §3 延迟与吞吐 (Latency & Throughput)   - C 族
  ## §4 负载分布 (Workload Distribution)    - E 族（模型 / 类 / 小时 / 客户端）
  ## §5 会话与任务 (Sessions & Tasks)        - 下钻，无延迟列
  ## §6 效率与浪费 (Efficiency & Waste)     - F 族  ← 可执行章节
  ## §7 请求详单 (Request Index)            - 链接到 vmr-requests-index.md
  ## 附录 数据源与方法论 (Appendix)          - 输入、格式、百分位方法、n 基准
```

### 5.2 各章节规格

**§0 摘要** - 一张紧凑的 2 行卡片，不是宽表。五个标题数字加最多三条机器生成的亮点（异常/浪费）。仅 A+B+C 族。

列（纵向，不是横向）：
```
requests: 182   success: 91.8%   billable-in(fresh): 3.04M   cache-eff: 84.0%   p95 dur: 27.9s
```
亮点（跨过阈值时自动生成）：
- `⚠ heartbeat 工作负载 cache-eff 8.3% - 18 次机械轮询烧掉 0.59M fresh tokens`
- `⚠ 工具形态 tools:67 - 发送 5.94MB schema 字节，6.0% 利用率（63 个工具从未调用）`
- `⚠ 端点 dashscope:glm-5.2 错误率 12.5/100（最差）`

**§1 成本与 Token 经济** - B 族。两张小表：
- **Token 类别拆分**（一行，纵向）：fresh / cached / cache-write / out / reasoning，带占比。
- **按模型缓存效率**（窄）：`model | protocol | req | cache-eff | fresh | cached | out | reasoning_share`。
- **角色字符分布**（D 族）：一张紧凑表 `role | chars | share`，加一行要点。
- *（可选，若配置了定价）* **成本估算**表：`class | qty | unit-price | est. cost`。

**§2 可靠性** - A 族 + G 族。
- **结果分布**：`ok | error | canceled | truncated | fallback(恢复/失败)`。
- **端点健康**：`endpoint | attempts | ok | availability | error_rate/100 | 首要错误类别`。窄。
- **按类别 × 端点的错误**（仅非零）：`endpoint | class | count | rate/100`。
- **错误时间线**：每小时错误数（sparkline + 计数），让读者看到*何时*。

**§3 延迟与吞吐** - C 族。一张表，窄，**每个百分位都标注 n**：
`model | protocol | req | ttft p50/p95 (n) | dur p50/p95/max (n) | stream_p95 | slow>30s | tok/s`。
下方吞吐说明。

**§4 负载分布** - E 族。四个紧凑子视图，各 ≤6 列：
- **按虚拟模型**：`model | protocol | req | success | fresh/cached/out | p95 dur`。
- **按工作负载类**：`class | req | fresh | cache-eff | tool_call_rate | p95 dur`。
- **按小时**：**体量 sparkline** + **p95-dur sparkline** 并排，然后仅对非平凡小时显示窄表。去掉 图片/字节/消息。
- **按客户端 key** ★：`client | req | success | fresh | cache-eff | p95 dur`。

**§5 会话与任务** - 下钻。**完全去掉延迟列。**
`session | title | class | turns | tasks | req/task | wall-clock | fresh/cached/out | outcome`。
定时一次性任务保持合并。下方一行冗余提示。

**§6 效率与浪费** - F 族。可执行章节。每个发现一行：
`finding | metric | value | implicated | suggested action`。
发现项：缓存浪费、工具 schema 浪费、定时 prompt 冗余、输出截断、上下文增长。

**§7 请求详单** - 一行：链接到 `vmr-requests-index.md`（+ 各 tag 兄弟文件）。

**附录** - 数据源、格式版本、记录/解析错误数、时间范围、百分位方法说明（"各桶从原始值直接计算；跨天合并使用预聚合的 `*_all`/`hours_of_day` 兄弟桶"），以及 `n` 基准图例。

### 5.3 排版/可读性规则

- **表格 ≤ 7 列。** 拆分而非加宽。
- **百分位始终带 `(n=…)`。** 当 `n < 20` 时，标记为 `⚠ low-n`。
- **Sparkline**（▁▂▃▄▅▆▇█）用于时间序列体量/延迟形态 —— markdown 友好，一行传达形态，无需图片。
- **浪费/异常提示**用 `⚠` 并点名涉及实体 + 一个建议动作。
- **与上期差值**列预留（无上期时为空）—— 为周期性报告预留而不改结构。
- **货币**仅在配置了定价时显示；否则显示 token 类别并附注"配置定价以查看 $ 估算。"

---

## 第 6 部分 - 渲染样例（使用真实 2026-07-24 数据）

这是同一输入下重新设计的 `vmr-report.md` 会呈现的样子。所有数字从 `vmr-report.json` 计算；`★` 标记派生/提升指标。

```markdown
# VMR 用量报告

数据源: logs/vmr-audit-2026-07-24.jsonl · format 9 · 182 条记录（0 坏行）· 2026-07-24 00:17:58 – 16:53:33
详单见 [vmr-requests-index.md](./vmr-requests-index.md) · 同名 .json · per-client: [-lobster](./vmr-requests-index-lobster.md) [-pimini](./vmr-requests-index-pimini.md)

## §0 摘要

| 请求 | 成功率 | 计费输入(fresh)★ | 缓存效率★ | p95 耗时 |
|---|---|---|---|---|
| 182（fallback 2 / trunc 0） | 91.8% | 3.04M | 84.0% | 27.9s (n=182) |

**亮点 (auto):**
- ⚠ **heartbeat 工作负载缓存效率 8.3%** - 18 次机械轮询烧掉 0.59M fresh tokens（占该负载输入 91%）
- ⚠ **工具声明 tools:67** - 跨 53 请求发送 5.94MB schema，仅用 4/67 工具（利用率 6.0%，63 个从未调用）
- ⚠ **端点 dashscope:glm-5.2 错误率 12.5/100**（最差），主因 auth ×1

## §1 成本与 Token 经济

**Token 类别分解**（basis: 167 条带 usage 的记录）

| 类别 | 数量 | 占输入/输出 |
|---|---|---|
| 输入-缓存命中 | 15.90M | 84.0% of in |
| 输入-fresh ★ | 3.04M | 16.0% of (fresh+cached) |
| 输入-cache_write | 0 | - |
| 输出 | 81.8K | - |
| └ 其中 reasoning | 21.7K | 26.5% of out |

> 计费口径：fresh + cache_write(×溢价) + out。缓存命中按各厂免费/极低价计。
> 未配置定价 -> 不显示 $ 估算；配置后此表追加「估算成本」列。

**按模型缓存效率** ★

| 模型 | 协议 | 请求 | 缓存效率★ | fresh | cached | out | reasoning 占比 |
|---|---|---|---|---|---|---|---|
| coding | openai | 128 | 88.8% | 1.83M | 14.49M | 48.3K | - |
| agent | openai | 53 | 57.2% | 1.07M | 1.40M | 32.4K | - |
| agent | anthropic | 1 | 0.0% ⚠low-n | 156K | 0 | 1.1K | - |

**请求消息字符及占比**

| 角色 | 字符 | 占比 |
|---|---|---|
| tool | 45.23M | 77.1% |
| assistant | 9.36M | 16.0% |
| developer | 2.41M | 4.1% |
| system | 1.40M | 2.4% |
| user | 0.30M | 0.5% |

> takeaway: tool 结果占上下文 77%--上下文优化的首要杠杆是压缩 tool 返回，而非 system prompt。

## §2 可靠性

**结果分布**

| ok | error | canceled | truncated | fallback(恢复/失败)★ |
|---|---|---|---|---|
| 167 | 15 | 0 | 0 | 2 (2/0) |

**端点健康**

| 端点 | 尝试 | 成功 | 可用度 | 错误率/100★ | 首要错误 |
|---|---|---|---|---|---|
| openai:volcengine:doubao-seed-2.0-lite | 134 | 129 | 96.3% | 3.7 | rate_limit ×4 |
| openai:volcengine:glm-5.2 | 31 | 30 | 96.8% | 3.2 | rate_limit ×1 |
| openai:dashscope:glm-5.2 | 8 | 7 | 87.5% | 12.5 ⚠ | auth ×1 |
| anthropic:volcengine:doubao-seed-2.0-lite | 1 | 1 | 100% | 0 | - |

**错误时间线**（错误数 / 小时）

```
00▁ 01▁ 02▁ 03▁ 04▁ 05▁ 06▁ 07▁ 08▁ 09▁ 10▁ 11▁ 12▁ 13▄ 14▁ 15▁ 16▁
```
> 错误集中在 13:00（rate_limit ×4 on coding session s03）。

## §3 延迟与吞吐

| 模型 | 协议 | n | ttft p50/p95 | dur p50/p95/max | stream_p95★ | slow>30s★ | tok/s |
|---|---|---|---|---|---|---|---|
| coding | openai | 128 | 2.6s / 18.4s | 3.8s / 32.5s / 94.5s | 14.1s | 9 | 44.1 |
| agent | openai | 53 | 2.8s / 9.6s | 6.0s / 16.0s | 6.4s | 0 | 63.6 |
| agent | anthropic | 1 ⚠low-n | 16.3s / 16.3s | 25.6s / 25.6s | 9.3s | 0 | 44.9 |

> 全局 p95 dur 27.9s，max 94.5s--约 5% 请求 >27.9s。stream_time 揭示：coding 的慢主要来自
> 长流式输出（p95 stream 14.1s），而非首字延迟。

## §4 负载分布

**按虚拟模型**

| 模型 | 协议 | 请求 | 成功率 | fresh/cached/out | p95 dur |
|---|---|---|---|---|---|
| coding | openai | 128 | 88.3% | 1.83M / 14.49M / 48.3K | 32.5s |
| agent | openai | 53 | 100% | 1.07M / 1.40M / 32.4K | 16.0s |
| agent | anthropic | 1 | 100% | 156K / 0 / 1.1K | 25.6s |

**按工作负载类**

| 类 | 请求 | fresh | 缓存效率★ | tool_call_rate | p95 dur |
|---|---|---|---|---|---|
| interactive | 146 | 2.27M | 87.2% | 84.2% | 41.7s |
| heartbeat | 18 | 0.59M | 8.3% ⚠ | 5.6% | 3.9s |
| dream_diary | 18 | 0.18M | 71.8% | 0% | 14.1s |

**每小时活跃度**

```
volume:  00▁ 01█ 02▁ 03▂ 04▁ 05▁ 06▁ 07▁ 08▁ 09▁ 10▁ 11▁ 12▁ 13▂ 14▁ 15▁ 16▁
p95 dur: 00▁ 01▁ 02▁ 03▁ 04▁ 05▁ 06▁ 07▁ 08▂ 09▁ 10▁ 11▁ 12▁ 13▄ 14█ 15▁ 16▁
```
> 两簇负载：01:00（100 请求，coding 代码审查）与 13:00–14:00（24+10 请求，p95 飙到 44–91s）。

**按客户端** ★

| client_key | 请求 | 成功率 | fresh | 缓存效率 | p95 dur |
|---|---|---|---|---|---|
| pimini | 129 | 88.3% | 1.83M | 88.8% | 32.5s |
| lobster | 53 | 100% | 1.07M | 57.2% | 16.0s |

## §5 会话与任务

| 会话 | 标题 | 类 | 轮 | 任务 | 轮/任务★ | 挂钟 | fresh/cached/out | 结果 |
|---|---|---|---|---|---|---|---|---|
| s01 | audio-album-creator… | interactive | 1 | 1 | 1.0 | 00:17 | 156K/0/1.1K | ok |
| s03 | review the entire project… | interactive | 128 | 4 | 32.0 | 01:34–13:59 | 1.83M/14.49M/48.3K | ok (2 fallback 恢复) |
| s29 | cron: Daily News Brief | scheduled | 8 | 1 | 8.0 | 08:00–08:01 | 93K/435K/5.7K | ok |
| s36 | cron: Daily Finance Brief | scheduled | 9 | 1 | 9.0 | 14:00–14:01 | 189K/466K/12.2K | ok |
| s39 | OpenClaw heartbeat poll | scheduled | 2 | 1 | 2.0 | 16:53 | 35K/37K/176 | ok |
| （合并） | heartbeat ×16 | scheduled | 16 | 16 | 1.0 | 00:53–15:53 | 0.59M/16K/1.2K | ok |
| （合并） | dream_diary ×18 | scheduled | 18 | 18 | 1.0 | 03:00–03:01 | 0.18M/451K/13.2K | ok |

> s03 的 32 轮/任务 = 典型 agent 工具调用扇出。定时任务每次重发完整 system prompt--见 §6。

## §6 效率与浪费 ★

| 发现 | 指标 | 值 | 涉及 | 建议 |
|---|---|---|---|---|
| 工具 schema 浪费 | schema_bytes_shipped ★ | 5.94 MB | tools:67 / 53 请求 | 裁剪未用工具；6.0% 利用率 |
| 工具 schema 死重 | waste_bytes ★ | ≈5.5 MB | 63 个从未调用的工具 | 按场景拆分工具集 |
| 缓存未命中输入 | cache_miss_tokens ★ | 3.04 M (16%) | 全局，coding 占 1.83M | 检查 prompt 前缀稳定性 |
| 定时任务冗余 | scheduled_prompt_redundancy ★ | 34 次 × 完整 system prompt | heartbeat(8.3% 命中) + dream_diary | 拉长间隔 / 换便宜模型 / 缓存前缀 |
| 输出截断 | output_truncation_rate ★ | 4 / 182 (2.2%) | finish=length | 提高 max_tokens 或拆任务 |
| 慢请求 | slow_request_share ★ | ~5% >27.9s, 1 个 >90s | coding 13:00–14:00 | 见 §3 stream_time 归因 |
| 上下文膨胀 | context_growth ★ | s03: 5.4K -> 164K (×30) | coding 代码审查会话 | 中途 compaction |

## §7 请求详单

每条记录（Chat User -> Session -> Task -> Turn）见 [vmr-requests-index.md](./vmr-requests-index.md)。
per-client: [-lobster](./vmr-requests-index-lobster.md) · [-pimini](./vmr-requests-index-pimini.md)
单请求全量捕获（req/resp/SSE）见 `details/*.md`。

## 附录 数据源与方法论

- 输入: logs/vmr-audit-2026-07-24.jsonl · format 9 · 182 记录 / 0 坏行
- 时段: 2026-07-24 00:17:58 – 16:53:33 (本地时区)
- 百分位: 各桶从原始 dur_ms/ttft_ms 直接计算（非跨桶 max 近似）；
  跨日合并使用预聚合的 endpoints_all / hours_of_day 兄弟桶，各自保留真实 p50/p95。
- n 基准: 每个百分位标注 n（= ttft_known / requests_with_dur）；n<20 标 ⚠low-n。
- 计费口径: fresh + cache_write(溢价) + out；缓存命中按各厂免费/极低价。未配置定价时不显示 $。
```

---

## 第 7 部分 - 索引（下钻）重新设计

当前 `vmr-requests-index.md` 精神上是对的（Chat User -> Session -> Task -> Turn）。保留层级。收紧两点：

### 7.1 每轮表 - 去噪，补一个缺失的信号

当前：
```
轮 | 时间 | Message | finish | 耗时 | 首字延迟 | Tokens In/CacheHit/Out | 图片/压缩 | 文件
```

重新设计：
```
轮 | 时间 | msgs | finish | dur | ttft | fresh/cached/out | cache-eff★ | 文件
```

- `msgs` 保留（`in+out` 简写，如 `0+2`）。
- `cache-eff` ★ - 每轮缓存命中率。在多轮会话中这是最有用的单请求信号：它显示缓存*何时*开始生效（如果前缀稳定，第 2 轮+ 应接近 95%+）。当前单元格 `5395 / 1024(19.0%) / 149` 迫使读者自己算 19%；把它提升为独立窄列。
- `图片/压缩` 从默认每轮表去掉（仅当该轮确有图片时移到脚注/`*` 标记 —— 这里 182 轮中 0 轮有图片）。
- 错误行保留 `❌error` 标记，但内联加上**错误类别**：`24ms ❌rate_limit` 而非裸的 `24ms ❌error`。（当前类别只在详情文件里；在索引中呈现可省一次点击。）

### 7.2 "全部请求（时间序）"页脚表

当前：
```
时间 | 会话/任务 | VM/API | 耗时 | 首字延迟 | Tokens In/CacheHit/Out | 图片/压缩 | 文件
```
重新设计 - 同精神，提升 cache-eff，去掉图片（罕见），加结果标记：
```
时间 | 会话/任务 | VM/API | outcome★ | dur | fresh/cached/out | cache-eff★ | 文件
```

### 7.3 各 tag 兄弟文件

结构不变（已共享 `renderIndex`）。唯一新增：每个 `vmr-requests-index-<tag>.md` 顶部一行**按 tag 摘要头**（请求数、成功率、fresh token、cache-eff），这样客户端专属文件自包含，无需打开主报告。

---

## 第 8 部分 - JSON 孪生与迁移说明

### 8.1 JSON（`vmr-report.json`）应镜像新结构

向相关行添加这些派生字段（在 `Build`/`finish*` 期间计算都很廉价，因为原始求和已存在）：

- 每个聚合行：`tokens_in_fresh`、`cache_efficiency`、`slow_requests`、`stream_time_p95`（从已保留的原始 `dur`/`ttft` 切片派生）。
- `ToolShapeRow`：`schema_bytes_shipped`（= `declared_bytes × requests`）、`declare_utilization`（= `len(calls)/len(declared)`）、`schema_waste_bytes`。
- `WorkloadRow`：已有大部分；加 `cache_efficiency`。
- 新增顶层 `efficiency` 章节镜像 §6 的发现表，使 JSON 可对同样的浪费信号做程序化查询。
- 新增顶层 `by_client` 桶（粒度 = `client_key_tag`）—— 数据已存在于审计记录（`client_key_tag`）中，只是今天未聚合进摘要桶。

### 8.2 已有的（无需新采集）

- A、B、C、D、E、G 族全部：原始字段存在于 `Row`/`EndpointRow`/`HourRow`/`WorkloadRow`/`SessionRow`。
- `client_key_tag`：存在于审计 JSONL，用于索引兄弟文件，只是未被汇总。
- 原始 `dur_ms`/`ttft_ms` 切片：每桶保留至 `finish*` —— 足以计算 `slow_requests`、`stream_time` 百分位、真实跨天百分位。

### 8.3 真正新增的计算

- `tokens_in_fresh` / `cache_efficiency`：一次减法 + 一次比值。轻松。
- `slow_requests`：在现有百分位遍历中加一次 `count(dur > 阈值)`。
- `stream_time_p95`：百分位前需每请求 `dur − ttft` —— 在工作状态中与 `durs`/`ttfts` 一并收集。
- `context_growth`：会话级，需首轮/末轮 `tokens_in` —— `SessionRow` 已通过会话分析按记录访问；存 min/max `tokens_in`。
- `billable_tokens` / 成本：需可选定价配置（`config.yaml` 新增 `pricing:` 块或 sidecar 文件）。受配置存在性门控；缺失 -> §1 仅显示 token 类别并附注。
- §0 自动亮点：对已完成桶做一个小规则遍历（任意工作负载 cache-eff < 30%、工具利用率 < 20%、端点 error_rate > 10%、slow_share > 5% 等）。纯呈现，无新数据。

### 8.4 范围纪律（不要加什么）

依 vmr 设计哲学（"它增加的是能力还是复杂度？"）：
- 不加图表/图片 —— sparkline 是 unicode，随处可渲染。
- 不加数据库、不加 web UI —— 保持 markdown + JSON 一对。
- 报告中不做跨协议翻译 —— OpenAI/Anthropic 行保持分开（如今天），只是列更好。
- 定价是**可选的**且**仅本地**（配置文件，永不外传）—— 缺失时报告优雅降级为 token 类别核算。
