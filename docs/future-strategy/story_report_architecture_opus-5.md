// Ver 2026-08-19 15:40, by Opus 5

# vmr 日志分析工具体系 — 需求再理解与第一性原理重构方案

本文是对 `vmr report` / `vmr story` 这两条命令的一次**整体性**重新审视，而不是对上一轮 7 条
UX 诉求的续写。它包含三部分产出：

1. 对用户原始诉求的重新理解与澄清（抛开既有文档的既定框架）；
2. 对 `docs/future-strategy/story_report_ux_review_sonnet-5.md`（下称"上一轮报告"）的逐条复核，
   包括其中**两处判断错误**和**一处根因误判**；
3. **重点章节**：以第一性原理重排整套日志分析工具体系（第 7 章）。

所有判断都标注了证据来源（`file:line`、实测数字、或外部参考链接）。凡是我没有实际验证、
只能给出推断的，正文里会明确写"未验证"。

---

## 0. 摘要（结论先行）

**对"是否只需要一个 story"这个核心问题的回答：不是。但你的直觉指向的那个真问题是对的，
只是位置偏了一格。**

- `report` 和 `story` 不能合并，因为它们回答的是**结构上不可互相推导**的两类问题，且
  `story` 在设计上就看不见一整类流量（见 §3 的删除检验）。
- 但 `detail report` **确实不该属于 `report`**——它既是 `report` 的下钻终点，也是 `story` 的
  下钻终点。正确的位置不是"收编进 story"，而是**提升为两者共享的证据层**。你的直觉对了一半。
- 真正的架构缺陷不是"两条命令没合并"，而是：**同一批 lineage 上跑着两套互不认识的坐标系**
  （`report` 的 run-scoped `s01/t01/turn N` vs `story` 的内容寻址 `j-<client>-<start>-<end>-<hash>`），
  两侧内部都用 `path:line` 当主键，但**两侧都没有把它发布出去**。修掉这个缺口，"脊柱链接
  detail""report 链接 journey"全部变成一次 JSON join，不需要动任何 import 边界。
- 一个必须先修的分层错配：**人读的 Markdown 里塞了 290KB 事件流，机读的 JSON 里事件流是 0 字节**
  （实测：`journey-j-openclaw-*.md` 290898 B，其 `.json` 6428 B 且结构上不含消息正文）。
  你答复的"人读为主、LLM 读为辅"恰好要求把这两者对调。**先补机读层，再删人读层的 fact-layer**
  ——顺序反了就是真的丢数据。

**对上一轮报告的复核结论**：6 条"已直接处理"里 5 条正确、1 条方案可以更稳；2 条"留待确认"里
**第 8 条（fact-layer / detail 链接）的"不可行"论证是错的**，有三条独立证据推翻它。

**一个独立的新发现（本轮最有操作价值的单点）**：上一轮报告与 `KNOWN_ISSUES §1.21` 把工具调用
结果配对失败的根因判定为"`opencode` 这个中间网关改写了 tool_call id"。**这个归因不成立。**
实测证据显示是 **agent 客户端在回写历史时去掉了下划线**，与上游 provider 无关；而且
**一行归一化（比较前 strip `_`）就能把 07-28 openclaw/lobster 的配对率从 0% 恢复到 100%**，
且仍然是精确匹配、不引入任何位置推断——也就是说，`§1.21` 里"不修的理由"（不愿引入推断规则）
在这个修法下不成立。详见 §5。

---

## 1. 前置诊断（本轮工作方式的第一步）

### 1.1 隐含假设

| # | 假设 | 核查结论 |
| --- | --- | --- |
| A1 | report 与 story 是同一脉络的两个层级 | **只成立一半**。原子单位不同且非包含关系：report 的原子是 `audit.Record`（含全部 failover attempt 与失败），story 的原子是 `Step`（一次成功且能挂上 lineage 的往返）。`internal/story/candidates.go` 的 `ListCandidates` 明确排除整链 < 2 个 manifest 的 lineage——即**全部单发请求**（heartbeat/dream_diary 这类定时脚手架的结构特征）。存在一整类流量 story 在设计上看不见 |
| A2 | detail report 是"辅助 story 的一种方式" | **方向对了一半**。`internal/report/detail.go` 的渲染结构（`renderClientRequest`/`renderAttempts`/`renderRawPreStrip`/`diffHeaderTable`/`renderBodyDiff`）里约一半内容是 story 永远不需要也无法解释的路由半区排障资料。detail 是两者**共同**的下钻终点，不是任一方的附属 |
| A3 | "story/report 必须严格隔离"是产品原则 | **是代码原则，不是产品原则**。`internal/archtest/import_boundaries_test.go:64-67` 禁止的是 Go 包级 import；它从不禁止两个**产出物**互相链接，也不禁止 `cmd/vmr` 组合根同时驱动两者（CLAUDE.md 明确写了组合根是唯一允许同时看到两半的地方）。上一轮报告把这条读成了"产出物不能耦合"，由此得出过强结论 |
| A4 | story 想链接 detail，必须先改 report 的文件命名机制 | **不成立**，三条独立证据见 §4.8 |
| A5 | 跑 journey 报告时"顺带"生成 detail 是低成本的 | **成本相差两个数量级**。`vmr report` 的 details 是 `Build` 第二遍扫描里随 `onRecord` **全量**落盘（每请求 1 md + 1 json）。本机 `reports/details/` 现有 118 个文件 = 59 个请求；全语料 11374 条记录 → 22748 个文件 |

### 1.2 信息缺口

1. **产物的第一读者**——已由你答复填补（见 §1.4）。
2. **运行节奏与扫描范围**：两条命令各跑一次全量扫描、各持久化一份**同一个类型**的
   `ctxgraph.FileCache`（`vmr-requests.json` 的 `files` 段 vs `vmr-stories.json` 的 `files` 段）。
   是否值得合并取决于运行频率——本文按"report 周期性全量、story 针对性单点"这一常见形态给建议，
   如果你的实际用法不同，§7.4 的结论需要相应调整。
3. **有无下游程序消费这些 JSON**：`KNOWN_ISSUES §1.15` 的指导原则明确写着"新指标优先用外部脚本
   消费稳定的 JSON 契约验证"。若该原则正在被实践，JSON 就是硬契约，§7.3 的字段新增必须走
   `omitempty` 向后兼容路线（本文按这条保守假设写）。
4. **是否要对外分享**：设计文档 §8 已把"HTML 单文件 + 脱敏"列为可选扩展，并注明"没有脱敏能力
   之前不应假定这个产物适合对外分享"。本文不假设需要它，但 §7 的分层设计为它留了自然位置。

### 1.3 典型错误（本类问题最容易踩的坑）

1. **把"内容重复"等价成"该合并"。** 重复的是数据源，不是职责。正确检验是逐项问"删掉它之后，
   哪个具体问题回答不了"（§3 就是这个检验）。
2. **拿 CLI 命令数量当设计单位。** 命令是入口，不是架构。"统一数据层/一次扫描"与"合并成一个
   命令"是两件独立的事。
3. **把 `<details>` 折叠当体积问题的解药。** 折叠只减少视觉高度，不减少字节。290KB 的 Markdown
   在编辑器与 GitHub 里照样卡。fact-layer 的问题不是"它展开着"，是"它在这份文件里"。
4. **让人读产物依赖另一条命令的运行顺序。** 一份会 404 的链接比没有链接更糟。任何链接方案必须
   自带存在性证明。
5. **搬动已校准的判据。** 九个 Finding 检测器、`ctxgraph` 的 `contractLenRatio`/`forkCoverage`
   都在 7112 条记录上校准过（设计文档 §6）；而 `stitch` 的两个阈值**明确尚未校准**（§7 已登记）。
   重构可以动组装方式，不该动这些。
6. **拿本次两条样例当全语料代表。** 它们的客户端（openclaw/lobster）恰好属于会改写 tool_call id
   的那一族——§5 证明这是**客户端**特征而非链路特征，是一个已知偏样本。

### 1.4 关键追问与你的答复

> **问**：这套分析产物的第一读者与主要消费方式是什么？
> **答**：**人读为主、LLM 读为辅，两者都要。**

这条答复的直接推论（本文后续全部据此展开）：

- 产物必须**分层**，而不是在同一份文件里堆两个区块。
- 人读层的目标函数是**纵向空间与跳转效率**；机读层的目标函数是**完整性与可寻址**。
  今天这两个目标被压在同一份 Markdown 里互相打架。
- 折叠（`<details>`）是人读层的手段，对 LLM 读毫无意义（它看到的是展开后的全文，
  折叠标记只是噪声）。**给 LLM 的应该是 JSON 或按需切片，不是折叠过的 Markdown。**

---

## 2. 事实基线：两条命令今天究竟产出什么

### 2.1 产物清单

**`vmr report`**（`cmd/vmr/cmd_report.go`，实现全在 `internal/report`）

| 产物 | 内容 | 读者 |
| --- | --- | --- |
| `vmr-report.md` / `.json` | §0 摘要 · §1 成本与 Token · §2 成本估算 · §2.5 账户消耗与额度 · §3 可靠性 · §4 延迟吞吐 · §5 负载分布 · §5.5 按客户端的上游归属 · §6 会话与任务（含 §6.5 Sticky · §6.6 端点性价比 · §6.7 Compaction 还原）· §7 效率与浪费 · §8 详单入口 · 附录 | 人 / 脚本 |
| `vmr-requests.md` / `.json` | 请求索引：按 Chat User 分组，`RequestRow` 列表 + `files` 解析缓存 | 人 / 脚本 |
| `vmr-requests-<tag>.md` | 该 client 的 Session→Task→Turn 展开 | 人 |
| `vmr-requests-cron-<class>.md` | 单发定时脚手架（heartbeat/dream_diary）独立成文件 | 人 |
| `vmr-requests-failed.jsonl` / `.md` | 失败请求过滤导出 | 人 / 脚本 |
| `details/*.md` + `*.json` | 逐请求全量详单：Client→VMR、VMR→上游每次 attempt（header diff / body diff / 剥离前原文）、VMR→Client | 人 / 排障 |

**`vmr story`**（`cmd/vmr/cmd_story.go`，实现全在 `internal/story`）

| 产物 | 内容 | 读者 |
| --- | --- | --- |
| `stories/vmr-stories.json` / `.md` | 候选 Journey 索引 + `ctxgraph.FileCache` 解析缓存 | 人 / 缓存 |
| `stories/journey-<id>.md` | 头部元信息 · System Prompt（本轮新增，折叠）· 概览卡 · 模型使用 · **决策脊柱** · **`## t0X`/`### Step N` fact-layer** · 工具调用时序图 · 疑似问题 · （可选）LLM 解读 | 人 |
| `stories/journey-<id>.json` | `JourneySummary`：ID/Title/From/To/Partial/**Metrics**/**Findings**/**LLMFindings** —— **不含任何消息正文** | 脚本 |
| `stories/compare-<a>-<b>.md` / `.json` | 行为剖面对比 · 工具对比 · 分叉点 · 端点/缓存/SysPrompt/交付物核查 · 证据溯源 · （可选）两段 LLM 解读 | 人 / 脚本 |
| `stories/vmr-story-corpus.md` / `.json` | 语料级统计（指标分布、Finding 命中率、相关性） | 人 / 脚本 |

### 2.2 实测数字（本机 `reports/`，2026-08-19）

| 观测 | 数字 | 依据 |
| --- | --- | --- |
| journey 报告体积 | openclaw 290898 B / 4066 行；lobster 568394 B / 8512 行 | `ls -la reports/stories/` |
| **脊柱层 vs fact-layer 占比** | 头部+脊柱 35473 B（**12%**），fact-layer 及其后 255425 B（**88%**） | 以 `## t01` 所在行（948）切分 openclaw 报告统计 |
| **机读层的事件流** | **0 字节** | `JourneySummary` 结构体（`metrics.go:399-407`）无消息正文字段；`journey-*.json` 实测 6428 B / 10922 B |
| `vmr-stories.json` 体积 | **88 003 599 B（88MB）** | `ls -la` |
| 其中索引行 | `journeys`：**3 行** | `json.load` 后计数 |
| 其中缓存 | `files`：38 个文件条目、11629 条 manifest（含逐消息哈希向量） | 同上 |
| 缓存的重复条目 | 同一文件同时以绝对路径与相对路径存在两份（`logs/vmr-audit-2026-07-25.jsonl.zst` 与 `/Users/stanford/code/vmr/logs/...`），共 255 条 manifest 重复 | 按 `(basename, line)` 去重后 11629 → 11374 |
| detail 文件名批次去重实际触发率 | **10 个键 / 11374 条记录 = 0.088%**，受影响记录 20 条 | 按 `(ts_ms, virtual, endpoint, outcome)` 近似 `detailFileName` 的键在全语料上计数（近似：真实实现用 `realModel(rec)` 与 `outcome+errClass`） |
| 本机 details 目录实际碰撞 | **0 / 59** | `ls reports/details/ \| grep -c -- '-[0-9]\.md$'` |

### 2.3 它们其实已经是同一个骨架

这是本轮核查最有价值的结构性事实：

```
audit.Record  ──►  ctxgraph.Manifest ──► Lineage ──► StitchGraph
                                            │
              taskseg.IsNewTask ────────────┤
                                            │
        ┌───────────────────────────────────┴───────────────────────────┐
        ▼                                                               ▼
report: SessionInfo(s01) → TaskInfo(t01) → Turn → RequestRow      story: Journey(j-…) → Task(t01) → Step
        └─ detail 文件名 = ts_virtual_real_outcome[.md]                  └─ journey-<id>.md
```

两侧消费的是**同一个 lineage 划分、同一个 `taskseg.IsNewTask` 任务边界规则**（设计文档
§3.4 的 taskseg 一节记录了这次收敛：收敛前后跑真实语料，输出逐字节一致）。差别只有两处：

1. **组装粒度**：`report` 的 `SessionInfo` = 一条 Lineage（跨 compaction 靠 `ContinuedFrom` 弱链接）；
   `story` 的 `Journey` = 一条**缝合链**（`ChainFrom`）。story 多做了一步缝合。
2. **身份**：`report` 用 run-scoped 序号（`session.go:580-582`：`fmt.Sprintf("s%02d", i+1)` /
   `t%02d`）；`story` 用内容寻址（`journey.go:566-575`：`j-<client>-<start>-<end>-<roothash[:8]>`）。

**结论：同一根骨架上挂了两套互不认识的坐标系。** 这才是"两个报告看起来在做同一件事却无法互相
跳转"的机制性原因——不是重复渲染，是**没有共同的地址**。

---

## 3. 删除检验：什么能合并、什么不能

不问"是否重复"，问"删掉它之后，哪个具体问题回答不了"。

| 待删对象 | 删掉后回答不了的问题 | 判定 |
| --- | --- | --- |
| `vmr report` 聚合层（§1/§2/§2.5/§5/§5.5/§7） | 这个月花了多少钱、哪个账号快烧爆、缓存有没有生效、哪个 client 在吃额度 | **不可删**。story 完全没有跨任务聚合能力，也不看成本 |
| `vmr report` 可靠性层（§3/§4/§6.5/§6.6） | 哪条链路在失败、failover 走了几次、sticky 有没有起作用、哪个端点性价比差 | **不可删**。`Step` 只保留成功往返，`attempt` 层在 story 里根本不存在 |
| `vmr-requests-cron-*.md` | 定时脚手架跑了多少次、值不值 | **不可删**。`ListCandidates` 在结构上排除全部单发请求（`candidates.go` 的 `total < 2` 分支） |
| `details/*` | 这次 502 到底发生了什么、vmr 改了哪个字节 | **不可删**，且它同时是两侧的下钻终点 |
| `vmr story` journey 叙事 | agent 是怎么想的、哪一步走偏、上下文怎么演化 | **不可删**。report 的 Session→Task→Turn 只有元数据行，没有因果 |
| `vmr story -compare` | 两套 agent 框架在同一任务上的行为差异、分叉点在哪 | **不可删** |
| `vmr story -corpus` | 哪些行为指标与结果相关（跨 Journey） | 可延后，但与 report §7 的口径不同（一个是"浪费"，一个是"行为↔结果相关性"），**不可互相替代** |
| **journey 报告里的 `## t0X` fact-layer** | ——**没有**。它承载的全部内容，在"机读层有完整事件流 + 证据层有 detail"之后可完整替代 | **唯一真正可删的那一块** |

**这张表就是对"是不是只需要一个 story"的正面回答**：八项里七项不可删，唯一可删的那一项恰好就是
你自己指出的那一项（原诉求 4）。你的判断是对的，只是它的适用范围比"合并两条命令"小得多——
**可删的是一个渲染区块，不是一条命令。**

---

## 4. 对上一轮报告（sonnet-5）的逐条复核

### 4.1 第 1 条 · 证据溯源改为按 Journey 精确定位 —— ✅ 认可

诊断与修法都对。`SourceFiles`（`storyindex.go`）从 `idx.Journeys[].Files` 取并集，数据本来就已算好
（`BuildJourneyIndexRow` 遍历 chain 上全部 manifest 的 `.Path`）。这是典型的"数据已算好但没接上"。

**补充一条它没写的前提**：`SourceFiles` 依赖 `idx.Journeys` 里有对应行。这一点由
`MergeJourneyIndexRows` 保证（fresh 永远是本次运行的完整权威集合），所以安全——但这也意味着
**索引的 `journeys` 段是 run-scoped 的**，与同一文件里永久累积的 `files` 段语义不一致（见 §7.5）。

### 4.2 第 2 条 · LLM 解读标题层级降一级 —— ✅ 认可结论，⚠️ 方案可以更稳

事实澄清是对的：那些 `## 候选根因` 是 prompt 里要求 LLM 输出的，不是 Go 拼的
（`internal/i18n/story_llm.go`）。选"改 prompt 不改渲染"也对——两次 LLM 调用的证据包内容完全不同，
不该合并。

**但方案本身有一个结构性弱点它自己也承认了**：文档的**标题层级是结构，不是内容**，
把结构正确性外包给 LLM 的指令遵从度是脆弱的。更稳的做法：在 `RenderLLMSection` 里对返回文本做
一次确定性的标题降级（仅在 LLM 段落内，把行首 `## ` 改写为 `### `），代价是十几行代码，
**必须处理围栏内的 `#` 不能被误改**（LLM 返回文本里出现 ```` ``` ```` 代码块是常态）。
prompt 侧的指令可以保留作为第一道；渲染侧兜底是第二道。两道并存才是"结构由代码保证"。

优先级：低。当前做法在实测上有效，只是没有保证。

### 4.3 第 3 条 · 多行/超长工具调用参数默认折叠 —— ✅ 认可，含它的实现选择

"预览用整段拉平后的前 160 字符，而不是严格第一行"这个偏离你原话的选择，理由（heredoc 的第一行
`python3 << 'PYEOF'` 没有信息量）是成立的，我认同。真实报告里 Step 4-6 的 exec 命令确实是这个形态。

### 4.4 第 4 条 · 脊柱 Step 原始消息折叠而非截断 —— ✅ 认可

它对自己第一轮判断的推翻是对的：`payloadBlock` 已经证明了"折叠而非截断"这个模式，"完整"与
"脊柱不膨胀"不冲突，不需要先决定 fact-layer 去留。

### 4.5 第 5 条 · System Prompt 移到文档头部 —— ✅ 认可

`systemPromptEras` 复用 `NewEvents` 的消息级去重来分段（而不是猜），是正确做法。
只影响 Markdown 渲染、不动 JSON 契约的判断也核对无误（`JourneySummary` 确实不含逐消息内容）。

**补充**：这条改动同时也是 §7 分层方案的**第一块拼图**——它把一段"每个读者只需要看一次的
背景资料"从时间轴里抽出来，放到了文档的固定位置。fact-layer 的处置应该沿用同一个思路。

### 4.6 第 6 条 · 脊柱补充工具调用结果 —— ✅ 实现认可，❌ 根因判定错误

实现（按 `ToolCall.ID` 精确配对、配不上就干净地不渲染）是正确且防御性好的。

**但它给出的根因是错的**，且这个错误被写进了 `KNOWN_ISSUES §1.21` 成为"不修的理由"。
完整证据见 §5——这是本轮最有操作价值的单点发现。

### 4.7 第 7 条 · Compare 开篇完整初始 User Message —— ✅ 方向认可，但三个"待拍板"我给明确意见

它列的三点取舍是真实的，但都可以现在就定：

1. **要不要设上限**：设。沿用 `renderExcerpt` 既有的"有界 + 折叠 + 截断时标注"惯例。
   反例（几千字符 JSON 粘贴）是真实存在的，无界展示会毁掉 compare 报告的可读性。
2. **放在哪**：保留现有短摘要（`SideBlock` 的 `Title`，负责"扫一眼是什么任务"），
   **下面新增一个折叠块**。这与 §7 的分层原则一致：概览与全文各司其职。
3. **要不要进 `compare-*.json`**：**进**。它的顾虑是"进了就会被喂给 LLM，增加成本"——但
   `BuildEvidencePack`（`llm.go:140-148`）今天给 LLM 的任务描述只有
   `journeyTaskTitles`（`taskseg.Preview` 的 80 rune 截断）。**分叉点分析的质量直接取决于
   LLM 是否理解原始指令**，这是整个证据包里单位 token 价值最高的一段。以 2000 字符为上限，
   对一个本来就几千字符的证据包是可接受的边际成本。

结论：这条**可以做，做法基本按它的推荐**，加上以上三点定案。

### 4.8 第 8 条 · fact-layer 删除 + 脊柱链接 detail —— ❌ "不可行"论证被推翻

它的论证链条是：`story ↛ report` 的 import 边界 → 无法自动生成 detail → 即使只拼路径，
`detailFileName` 依赖跨批次去重计数器 `used map[string]int` → `story` 侧无法确定性推算文件名
→ **必须先改 `report` 侧命名机制（换成内容 hash）才能解决**。

**三条独立证据推翻这个结论：**

1. **不需要"推算"，因为映射已经被发布了。** `internal/report/rows.go:499` 的 `RequestRow` 带
   `DetailFile string \`json:"detail_file,omitempty"\`` 字段，随 `vmr-requests.json` 落盘。
   本机实测第一条记录：
   ```json
   {"ts":"2026-07-25T00:53:29+08:00","session":"s01","task":"t01","turn":1, ...,
    "detail_file":"20260725-005329.261_agent_doubao-seed-2.0-lite_ok.md"}
   ```
   `story` 侧读一个 JSON 文件即可得到"请求 → detail 文件"的权威映射，**既不 import `report`
   包，也不复制它的命名规则**。这条路径上一轮报告完全没有考虑——它把"不能 import 代码"
   直接推成了"不能获得信息"。

2. **`report.WriteDetails` 本来就是为这个场景导出的。** `internal/report/detail.go:219` 的
   函数注释原文：*"This is a standalone alternative to Build's onRecord hook — a second,
   independent read of the same audit files, **for callers that want detail export without
   running the full aggregation pass**."* 它连同 `AnalyzeSessions` 都是导出的。在 `cmd/vmr`
   这个**唯一允许同时看到两半的组合根**里调用它们，对某个 Journey 的文件子集生成 detail，
   不触碰任何 import 边界。

3. **"自动生成缺失的兄弟产物并链接过去"在 story 内部已经是既成模式。** `cmd_story.go` 的
   `compareJourneys` 调用 `ensureJourneyFile(jA, ...)` / `ensureJourneyFile(jB, ...)`，
   `JourneyRef.ReportFile` 由 `JourneyReportFile(s.ID, s.Partial)` 确定性推算并渲染成链接
   （`compare.go:174-183`）。跨半区做同一件事，唯一的新增要求是"在组合根做"，而这正是
   CLAUDE.md 给组合根的定义。

**它那条顾虑里唯一站得住的部分，我把它量化了**：`detailFileName` 的批次去重计数器
（`detail.go:354-368`）确实会让同一毫秒 + 同虚拟模型 + 同真实模型 + 同结果的记录带上 `-2` 后缀，
**且这个后缀取决于本次批次里谁先出现**。全语料实测：**10 个碰撞键 / 11374 条记录 = 0.088%，
受影响 20 条**；本机 `details/` 59 个文件中 0 例。所以：

- **走证据 1（读 `vmr-requests.json`）的路径完全不受影响**——那份映射本身就是全量口径下算出来的。
- 只有走"按 Journey 子集独立生成 detail"的路径（证据 2）才可能与全量口径不一致，影响面 0.088%。
- "必须先把命名换成内容 hash"是一个**可选的加固**，不是**前置条件**。上一轮报告把它写成了前置条件。

**但我要给这条加一个上一轮报告和你都没提的前置条件**（见 §7.6）：
**先把完整事件流补进 `journey-<id>.json`，再删 fact-layer。** 否则删除是真的丢数据——
今天 fact-layer 是消息正文在派生产物里的**唯一**载体（机读 JSON 里是 0 字节）。

---

## 5. 新发现：工具调用结果配对失败的真实根因，与一行修复

### 5.1 现有结论

`KNOWN_ISSUES §1.21` 与上一轮报告 §6 记载：openclaw / lobster 两条 Journey 的 tool_call 配对
成功率 0%；根因判定为——

> "`opencode`（这条链路里 VMR 与 deepseek-v4-pro 之间的代理/网关）在转发时改写了 ID"

并据此判定"不该现在修"，因为唯一的修法（位置兜底）会"在协议保证之外引入一条推断规则"。

### 5.2 复核方法

直接对原始审计日志做统计（脚本在 `scratchpad/`，逻辑：正则抓响应体 SSE 里的
`"id":"call…"`，与请求体 `messages[].tool_calls[].id` / `messages[].tool_call_id` 做集合比较，
按 `client_key_tag` 与 `endpoint` 两种口径分组）。样本：`2026-07-16 / 07-19 / 07-28 / 08-14 / 08-16`
五个日志文件，合计 4159 条记录。

### 5.3 结果

**按 `endpoint` 分组（08-14）**：

| endpoint | 请求侧 id 带 `_` | 响应侧 id 带 `_` |
| --- | --- | --- |
| `openai:deepseek:deepseek-v4-flash` | 0.0% (n=138) | 100.0% (n=12) |
| `openai:volcengine:deepseek-v4-flash` | 0.0% (n=290) | 100.0% (n=268) |
| `openai:volcengine2:deepseek-v4-flash` | 0.0% (n=148) | 100.0% (n=5) |
| `openai:volcengine:doubao-seed-2.1-turbo` | 0.0% (n=271) | 100.0% (n=56) |
| `openai:cliproxy:gemini-3.7-flash-high` | 0.0% (n=117) | — |

**五个不同的 provider，没有一个经过 `opencode`，却全部呈现同一种改写。**

**按 `client_key_tag` 分组（07-28，同一天、同一批上游）**：

| client | 请求侧带 `_` | 响应侧带 `_` | id 集合交集 | 去下划线归一化后交集 |
| --- | --- | --- | --- | --- |
| `hermes` | 100.0% (n=91) | 100.0% | **100.0%** | 100.0% |
| `pimini` | 100.0% (n=260) | 100.0% | **100.0%** | 100.0% |
| `lobster` | **0.0%** (n=64) | 100.0% | **0.0%** | **100.0%** |
| `openclaw` | **0.0%** (n=33) | 100.0% | **0.0%** | **100.0%** |

**结论（高置信）**：改写发生在**客户端**，不在上游。同一天、同一批端点上，`hermes`/`pimini`
的 id 两侧完全一致；`lobster`/`openclaw`（同属 OpenClaw 家族）把响应里的 `call_00_xxx` 回写成
`call00xxx`。`KNOWN_ISSUES §1.21` 把它归因给 `opencode` 网关是**归因错误**——顺带说明，
07-28 当天 `openai:opencode:deepseek-v4-pro` 这条链路整体的 id 集合交集是 **59.6%**、
请求侧 76.5% 的 id 仍带下划线，与"这条链路会剥离下划线"的说法直接矛盾。

（另有第三种形态：`cliproxy:gemini` 链路上出现 `exec1786691703864731…` /
`process1786697731217703…` 形状的 id，即"工具名 + epoch 微秒"——客户端**自造** id，
这类无论如何都不可能按 id 配对。）

### 5.4 一行修复：归一化后仍是精确匹配

既然改写是**确定性的字符级变换**，配对不需要引入位置推断——只需在比较前对两侧做同一个归一化
（`strings.ReplaceAll(id, "_", "")`）。实测：

| 文件 | client | 原始匹配率 | 去下划线后匹配率 |
| --- | --- | --- | --- |
| 07-28 | openclaw | 0.0% | **100.0%** |
| 07-28 | lobster | 0.0% | **100.0%** |
| 07-16 | openclaw | 0.0% | **100.0%** |
| 07-16 | lobster | 0.0% | **87.9%** |
| 08-14 | lobster | 0.0% | **74.4%** |
| 08-16 | lobster | 0.0% | **62.0%** |
| 07-28 | hermes / pimini | 100.0% | 100.0%（无变化） |

（08-14/08-16 未达 100% 是集合级跨文件比较的自然残差：最后一轮的调用其结果落在下一个文件、
或调用本身来自前一个文件。逐 Step 配对不受此影响。）

**误配风险已核查**：07-28 全文件 548 个唯一 id，归一化后 451 个，减少 97 个——恰好等于
`lobster(64) + openclaw(33) = 97`，即**归一化合并的全部是真配对，零误合并**。

### 5.5 建议

1. 把 `KNOWN_ISSUES §1.21` 的根因改写为"**agent 客户端在回写工具调用历史时改写了 id
   （OpenClaw 家族去下划线；部分链路自造 `name+timestamp` 形态 id）**，与上游 provider 无关"，
   并附上按 client 分组的对照数据。
2. 修法：`chatmsg` 的配对逻辑加**归一化回退**——先精确匹配，失败再用归一化键匹配。
   这**仍然是精确匹配**（只是键做了规范化），不引入任何顺序/位置推断，因此
   `§1.21` 里"不愿引入推断规则"这条不修理由在这个修法下不成立。
3. 位置兜底作为第三档（应对自造 id 的客户端）可以保留为待定，配对结果标注来源。
4. **这条修完之后，第 6 条（脊柱展示工具结果）才真正在你的主力语料上产生可见效果**——
   目前它对你自己的两个主要 client 全程静默降级。

---

## 6. 原始 7 条诉求的重新评估

| # | 原诉求 | 我的评估 |
| --- | --- | --- |
| 1 | compare 开篇挂 journey 链接 | 已生效（`JourneyRef.ReportFile`）。**保持** |
| 2 | compare 开篇展示完整初始 User Message | **该做**，按 §4.7 的三点定案。但注意它的真正价值是给 **LLM 解读**用，人读侧短摘要已经够 |
| 3 | 证据溯源只列相关文件 | **已做且正确**（§4.1） |
| 4 | 两段 LLM 解读收进大章节 | **已做**，建议补渲染层兜底（§4.2） |
| 5 | 脊柱 Step 消息完整、不截断 | **已做**（折叠而非截断，正确） |
| 6 | System Prompt 移到头部折叠 | **已做**（§4.5） |
| 7 | 脊柱展示工具调用结果 | **已做但对你的主力语料全程静默失效**，需先修 §5 |
| 8 | 多行工具参数默认折叠 + 首行预览 | **已做**（§4.3） |
| 9 | 去掉 `## t0X` fact-layer、脊柱挂 detail 链接 | **判断正确，且是 8 项里唯一真正动架构的一项**。但**执行顺序必须是"先补机读层，再删人读层"**，见 §7.6。上一轮报告判定的"不可行"不成立（§4.8） |

> 你在原诉求里说"如果这个拆开不好拆，就用一大块对应整组工具调用"——实际上按 id 精确拆分并不难
> （`toolResultsFor` 本来就按 id 配对），上一轮报告选了精细方案是对的。§5 修完后它才真正跑得起来。

---

## 7. 【重点】第一性原理重构：日志分析工具体系

### 7.1 从原料出发：只有一份事实，三类提问，一个共同的证据终点

**原料**：append-only 的 JSONL，一条记录 = 一次客户端请求的完整物理事实（客户端 ↔ vmr 层 +
vmr ↔ 上游每次 attempt 层）。**其余一切都是 view。**

**读者会问的问题，穷举后只有三类**（分类依据是"读者处在什么情境下问"，不是数据维度）：

| 类 | 问题 | 主轴 | 今天由谁回答 |
| --- | --- | --- | --- |
| **账** | 钱花在哪、额度还剩多少、缓存省了多少、谁在消耗 | 时间 | `vmr report` §1/§2/§2.5/§5/§5.5/§7 |
| **健** | 哪条链路在失败、延迟多少、failover/sticky 是否生效、端点值不值 | 链路 | `vmr report` §3/§4/§6.5/§6.6 |
| **事** | 这次任务是怎么被完成的、哪一步走偏、两个 agent 差在哪 | 因果 | `vmr story` |

**"一次具体请求发生了什么"不是第四类提问——它是三类提问共同的下钻终点。**

这就是本章最关键的一句判断，也是对你原始设想的修正：

> 你说"detail report 不过是辅助 story 解决问题的其中一个方式方法"。
> **方向对了一半：detail 确实是 story 的下钻终点，但它同样是"账"和"健"的下钻终点**
> （`vmr-requests-failed.md` 的每一行、§7 效率发现的每一条、§3 的每个失败率，最终都要落到某条
> 具体记录上）。所以正确的结论不是"把 detail 收编进 story"，而是**把 detail 从 `report` 的
> 附属提升为与两者平级的证据层**。它今天挂在 `reports/details/` 下纯粹是历史原因——report 先做。

### 7.2 目标模型

```
                        audit JSONL（唯一事实，只读）
                                   │
        ┌──────────────────────────┴──────────────────────────┐
        │            解析层（已存在、已共享、不动）              │
        │   ctxgraph（内容寻址/lineage/缝合）                   │
        │   chatmsg（消息/SSE/usage/工具配对）                  │
        │   taskseg（agent 方言 + 会话/任务切分）                │
        └──────────────────────────┬──────────────────────────┘
                                   │
        ┌──────────────────────────┴──────────────────────────┐
        │        ★ 坐标层（缺失 —— 本方案的关键新增）           │
        │   req 地址：<audit-basename>:<line>（两侧都发布）      │
        │   journey/session 互认：各自索引发布对方能 join 的键    │
        └──────────────────────────┬──────────────────────────┘
                                   │
     ┌──────────────┬──────────────┴───────────────┬──────────────────┐
     ▼              ▼                              ▼                  ▼
  账 + 健        事（叙事）                     证据层            语料级统计
 vmr report     vmr story                    details/*         story -corpus
 （聚合）       （单任务）              （两者共享的叶子）        （跨任务）
```

### 7.3 关键新增：请求级稳定坐标

**问题**：两侧内部的真正主键都是 `path:line`（`report` 的
`SessionAnalysis.byKey`（`session.go:526-533` 的 `recLoc`）、`ctxgraph.Manifest.Path/Line`），
但**两侧的公开 JSON 都没有发布它**——`RequestRow` 只有 `ts`，`JourneySummary` 连 Step 都没有。

**方案**：把它提升为公开字段。

```jsonc
// vmr-requests.json 的 requests[] 新增一个字段
{ "ts": "...", "req": "vmr-audit-2026-07-25.jsonl:317", "detail_file": "...", ... }

// journey-<id>.json 的 events/steps（见 7.4）每个 Step 携带同一个键
{ "seq": 7, "req": "vmr-audit-2026-07-25.jsonl:317", ... }
```

**为什么用 `basename:line` 而不是绝对路径或内容 hash**：

| 候选 | 优点 | 缺点 | 判定 |
| --- | --- | --- | --- |
| 绝对 `path:line` | 已是内部主键，零成本 | housekeeping 压缩会把 `.jsonl` 变成 `.jsonl.zst`，路径变；且不同机器路径不同 | 否 |
| **`basename:line`（去掉 `.zst`）** | 稳定（轮转只改扩展名、不重排行）、零成本、人可读、可直接 `sed -n` 定位 | 跨目录同名文件理论上冲突（实践中审计文件名带日期，不冲突） | **推荐** |
| 记录内容 hash | 完全稳定 | 需多算一次哈希；不可读；无法直接定位回原文件 | 备选 |

顺带修掉一个实测缺陷：`ctxgraph.FileCache` 以**路径字符串**为 key，导致同一文件的绝对路径与
相对路径在缓存里存了两份（实测 255 条 manifest 重复）。规范化成 `basename` 同时解决这个。

**这一步的收益是杠杆式的**——它不改任何 import 边界（两侧都已依赖 `ctxgraph`）、不改文件命名、
不改渲染，但让下面这些立刻成为一次 JSON join：

- story 脊柱的每个 Step → `details/<file>.md`（读 `vmr-requests.json` 的 `{req → detail_file}`）
- report 的 Session 卡片 → `journey-<id>.md`（读 `vmr-stories.json` 的 `{journey → files}`）
- 外部脚本把 `vmr-report.json` 的一条 Finding 直接对上某条 Journey 的某个 Step

这正是"两半区、一个契约"原则的自然延伸：**契约从"audit 记录"扩展到"派生索引"**，
两个包依然互不 import，只是各自发布了对方能读的数据。

**行业对照**：LangSmith（thread → trace → run）、Langfuse（session → trace → observation）、
OpenTelemetry GenAI（`gen_ai.conversation.id` 串联 span 树）三者的共同点不是"只有一个视图"，
而是**每一层只有一个稳定 ID，所有视图（仪表盘/会话视图/trace 视图）都用它互相跳转**。
vmr 今天缺的正是这一条；而它们的仪表盘与 trace 视图从来都是**同一批对象的不同视图**、
不是两份互不相干的产物——这与本方案 §7.2 的形状一致。

### 7.4 三层产物（对应"人读为主、LLM 读为辅"）

| 层 | 读者 | 载体 | 内容 | 体积目标 |
| --- | --- | --- | --- | --- |
| **叙事层** | 人 | `journey-<id>.md` / `vmr-report.md` / `compare-*.md` | 概览 + **决策脊柱** + Findings + LLM 解读，每一步挂链接 | journey **< 50KB**（今天 290–568KB） |
| **数据层** | 脚本 / LLM | `journey-<id>.json` / `vmr-report.json` / `vmr-requests.json` | **完整事件流**（Task/Step/Event/ToolCall/ToolResult）+ Metrics + Findings + `req` 坐标 | 不限 |
| **证据层** | 人 + LLM，按需 | `details/<req>.md` + `.json` | 单请求物理全貌（含 attempt/header/body diff） | 按需生成 |

**今天的错配是这个方案要修的第一件事**：

```
现状：  人读 md ── 290KB 事件流 ─┐          机读 json ── 0 字节事件流
                                 └── 反了 ──┘
目标：  人读 md ── 脊柱 + 链接（~40KB）      机读 json ── 完整事件流
```

**为什么"给 LLM 的应该是 JSON 而不是折叠 Markdown"**：`<details>`/`<summary>` 对 LLM 是纯噪声
（它看到的是展开后的全文），而 JSON 的结构让"只取第 7 步"这类切片成为可能。今天
`BuildSingleJourneyEvidencePack` 之所以要在 Go 里重新组织一遍证据，正是因为没有一份可切片的
机读全量——补上数据层之后，证据包的构造会变简单而不是变复杂。

### 7.5 索引与缓存分家

`vmr-stories.json` 今天是一个语义自相矛盾的文件：

- `journeys` 段是 **run-scoped** 的（`MergeJourneyIndexRows` 明确丢弃"本次输入文件无法证明"的旧行）；
- `files` 段是 **永久累积** 的（实测 38 个文件条目、11629 条 manifest）；
- 结果：**88MB 的"索引"，其中索引本身只有 3 行**。

建议拆成两个文件：

| 文件 | 语义 | 体积 |
| --- | --- | --- |
| `reports/stories/vmr-stories.json` | 纯索引（`journeys[]`，run-scoped） | KB 级，可随手 `cat` |
| `reports/.parse-cache.json` | `ctxgraph.FileCache`，**`vmr report` 与 `vmr story` 共用一份** | MB 级，可随时删除重建 |

共用缓存不需要任何边界改动——两条命令用的本来就是同一个 `ctxgraph.FileCache` 类型和同一个
`ScanCached`（设计文档 §3.4 已明确写了这一点）。今天是同一份数据存了两份。

### 7.6 迁移路径（分批，每批独立可上线，顺序有依赖）

| 批 | 内容 | 依赖 | 风险 |
| --- | --- | --- | --- |
| **1** | `req` 坐标字段进 `RequestRow` 与 story 的 Step JSON；`FileCache` key 规范化为 basename | 无 | 低（纯新增 `omitempty` 字段） |
| **2** | `journey-<id>.json` 补完整事件流（Task/Step/Event/ToolCall/ToolResult） | 批 1 | 低（新增字段，JSON 体积上升——这是设计意图） |
| **3** | `chatmsg` 工具配对加归一化回退（§5） | 无 | 低，收益立竿见影 |
| **4** | 删 `## t0X` fact-layer；脊柱每步挂 detail 链接（**存在才挂**：读 `vmr-requests.json` 判断） | **批 1 + 批 2** | 中（人读产物形态变化，golden 测试要更新） |
| **5** | 索引与缓存分家、两命令共用缓存 | 批 1 | 中（产物路径变化，需同步 UserGuide/README 及其 `.zh` 兄弟） |
| **6**（可选） | `vmr story -journey ... -with-evidence`：在 `cmd/vmr` 组合根按 Journey 的文件子集调 `report.AnalyzeSessions` + `report.WriteDetails` | 批 1 | 中（子集口径与全量口径在 0.088% 的记录上文件名可能不一致，见 §4.8） |

**批 4 依赖批 2 是硬依赖**：今天 fact-layer 是消息正文在派生产物里的唯一载体。
先删后补 = 中间存在一个真正丢数据的版本。这一点你的原始诉求和上一轮报告都没有提到。

**验收标准**（每批都要有）：

- 批 1：`vmr report` 与 `vmr story` 分别跑同一批日志，用 `req` 字段能把两份 JSON join 上，
  join 命中率 100%。
- 批 2：`journey-<id>.json` 的事件流经反向渲染能重建出今天 fact-layer 的等价内容。
- 批 3：07-28 的 openclaw/lobster 两条 Journey，脊柱工具结果配对率从 0% 升到 >90%。
- 批 4：`journey-j-openclaw-*.md` 体积 < 50KB，且每个脊柱 Step 的 detail 链接
  `test -f` 全部为真（或干净地不渲染）。
- 批 5：`vmr-stories.json` < 100KB；连跑 `vmr report` + `vmr story` 只产生一份解析缓存。

### 7.7 命令层：为什么不合并成一个 `vmr analyze`

三条理由，按分量排序：

1. **删除检验（§3）证明两组提问都不可删**，合并只会把两组不同的输出塞进一个更长的文档。
2. **扫描代价与运行频率天然不同**：`report` 是周期性全量（两遍扫描 + 全量 detail 落盘），
   `story` 是针对性单点。合并会强迫低频昂贵的一方拖累高频的一方——这与 §7.5 共用**缓存**
   （降低成本）方向相反。
3. **两个动词对应两种心智**：`report`（"最近怎么样"）与 `story`（"这一次发生了什么"）。
   合并成一个动词后，用户仍然要靠 flag 区分，只是换了个地方选择。

**要合并的是数据层与坐标层，不是命令。** 这是本章与你原始设想最主要的分歧点，
也是我认为经得起推敲的那一条。

---

## 8. 决策与取舍表

| 决策 | 备选 | 取舍逻辑 |
| --- | --- | --- |
| 保留 `vmr report` / `vmr story` 两个命令 | 合并成一个 `vmr analyze` | 删除检验里八项功能七项不可删；两者扫描代价与运行频率不同；合并只换了选择的位置，没减少选择 |
| detail 提升为两者共享的证据层 | 保持为 report 的附属 / 收编进 story | 三类提问的下钻终点是同一个东西；今天挂在 report 下是历史顺序，不是逻辑归属 |
| 新增 `basename:line` 请求坐标并两侧发布 | 让 story 直接 import report / 让 report 改成内容寻址文件名 | 前者违反 archtest 边界；后者是可选加固而非前置条件（碰撞率 0.088%）。发布坐标零边界成本，且同时解决双向链接 |
| 事件流从人读 Markdown 移到机读 JSON | 保持现状 / 只折叠不移动 | 折叠不减字节；你的答复要求分层。今天是"人读的有 290KB 事件流、机读的有 0 字节"的错配 |
| 删 fact-layer 前先补 JSON 事件流 | 直接删 | fact-layer 今天是消息正文在派生产物里的唯一载体，先删后补存在真正丢数据的中间版本 |
| 工具配对用归一化回退（strip `_`），不用位置推断 | 位置兜底 / 不修 | 归一化后仍是精确匹配，不引入推断规则，因此 `§1.21` 的不修理由不成立；实测 0%→100% 且零误合并 |
| 索引与解析缓存分家、两命令共用缓存 | 保持现状 | 现状是同一文件里 run-scoped 与永久累积两种语义并存，产出 88MB 的"索引"，且同一份数据存了两份 |
| LLM 解读标题层级由渲染层兜底 | 只靠 prompt 指令 | 文档结构是结构，不该外包给 LLM 的指令遵从度；prompt 保留为第一道，渲染兜底为第二道 |
| compare 首条 User Message 进 JSON（喂 LLM） | 只在 Markdown 渲染层取数 | 分叉点分析质量直接取决于 LLM 是否理解原始指令；今天它只拿到 80 rune 截断的标题。这是证据包里单位 token 价值最高的一段 |

### 明确不做

- **不引入数据库或服务端**。Strategy 文档的定位是单二进制、无数据库、零埋点；本方案全部产物
  仍是文件，"像一个 store 那样行为"靠的是稳定地址，不是存储引擎。
- **不合并两个命令**（理由见 §7.7）。
- **不让 `story` import `report`**（跨半区的事一律在 `cmd/vmr` 组合根做）。
- **不为 detail 做全量预生成**（全语料 22748 个文件）；只做"存在才链接" + 可选的按需生成。
- **不动已校准的检测器阈值**（九个 Finding 检测器、`contractLenRatio`/`forkCoverage`）。
  未校准的 `stitch` 两个阈值另有 `KNOWN_ISSUES` 条目，不在本方案范围。
- **不做 HTML/脱敏渲染**。设计文档 §8 已把它列为可选扩展；本方案的分层为它留了位置
  （叙事层换一个渲染器即可），但不提前实现。

---

## 9. 附录

### 9.1 验证方法

1. **源码**：通读 `internal/story/`（`candidates.go`/`journey.go`/`storyindex.go`/`compare.go`/
   `metrics.go`/`findings.go`/`render_md.go`/`render_spine*.go`/`llm.go`）、`internal/report/`
   （`detail.go`/`session.go`/`rows.go`/`requests.go`）、`cmd/vmr/cmd_story.go`/`cmd_report.go`、
   `internal/archtest/import_boundaries_test.go`，逐条对照真实产物找到生成它的确切位置。
2. **产物实测**：对本机 `reports/` 下的真实产物做体积/结构统计（`wc -c`、按标题行切分、
   `python3 -c "import json"` 解析 `vmr-stories.json` / `vmr-requests.json`）。
3. **全语料统计**：用 `vmr-stories.json` 的 11374 条 manifest（按 `(basename, line)` 去重后）
   统计 detail 文件名碰撞率——这是一次**近似**（键用 `(ts_ms, virtual, endpoint, outcome)`，
   真实实现用 `realModel(rec)` 与 `outcome+errClass`），结论的量级可靠，绝对数字有 ±小幅误差。
4. **原始日志核查**（§5）：`zstdcat` 五个日志文件（07-16 / 07-19 / 07-28 / 08-14 / 08-16，
   合计 4159 条记录）管道进 Python，正则抓响应体 SSE 的 `"id":"call…"` 与请求体的
   `tool_calls[].id`/`tool_call_id`，按 `client_key_tag` 与 `endpoint` 两种口径做集合比较与
   形态统计。归一化误合并检查用"归一化前后唯一 id 数之差"对照"两个受影响 client 的 id 计数之和"。
5. **未做的验证**（诚实声明）：
   - 没有重新构建二进制、没有重跑 `vmr story` / `vmr report`（本轮不改代码，也避免再产生真实
     LLM API 调用与费用）。
   - §5 的按 endpoint 分组统计有一个已知的口径误差：一条记录的历史 id 被归到**该记录最后一次
     attempt 的 endpoint** 上，而这些 id 实际由更早的轮次产生（可能落在别的 endpoint）。
     按 `client_key_tag` 分组的那张表不受此影响，§5.3 的结论以它为准。
   - 没有验证 `basename:line` 在你全部历史日志上无冲突（审计文件名带日期，理论上安全，
     但批 1 落地时应加一个断言）。

### 9.2 外部参考

- [Langfuse — Data Model（session / trace / observation 三层，session 为可选顶层）](https://langfuse.com/docs/observability/data-model)
- [Langfuse — Sessions（session 是共享 `session_id` 的 trace 的虚拟聚合）](https://langfuse.com/docs/observability/features/sessions)
- [Langfuse — What does a good trace look like?（"一次对话一个 session、一轮一个 trace"）](https://langfuse.com/docs/observability/best-practices)
- [LangChain — Debugging Deep Agents with LangSmith（thread = trace 集合，trace = run 树，从仪表盘下钻到单个 run）](https://www.langchain.com/blog/debugging-deep-agents-with-langsmith)
- [LangSmith — Query traces using the SDK（runs/query 是查询 span 数据的入口）](https://docs.langchain.com/langsmith/export-traces)
- [Uptrace — OpenTelemetry for AI Systems: LLM and Agent Observability (2026)（`gen_ai.conversation.id` 把 span 归入所属会话；Orchestration/LLM/Tool/Memory 四类 span 嵌套）](https://uptrace.dev/blog/opentelemetry-ai-systems)

对照结论：这三套系统的共同点不是"只有一个视图"，而是**每层只有一个稳定 ID、所有视图共用它互相
跳转**，且仪表盘与 trace 视图是同一批对象的不同视图。vmr 的差异化（零埋点、字节忠实、单二进制、
文件产物）不需要放弃这条共性——它需要的只是把已经存在于内存里的主键**发布出去**。

### 9.3 本文与既有文档的关系

- 本文**不替代** `story_report_ux_review_sonnet-5.md`——那份是上一轮 7 条 UX 诉求的执行记录，
  其中 6 项改动已在工作区（未提交）。本文复核它、纠正其中 2 处判断和 1 处根因，并把问题
  提升到体系层面。
- 若本文的方案被采纳，需要相应更新的既有条目：
  - `KNOWN_ISSUES §1.21`（根因改写 + 修法改为归一化回退）
  - `KNOWN_ISSUES §1.22`（"不可行"论证撤销，改为"分批实施，见本文 §7.6"）
  - `KNOWN_ISSUES §1.20`（三个待拍板点已在本文 §4.7 定案）
  - `docs/VirtualModelRouter_Design_v4_Analytics.md` §2.5 / §3.4（产物分层与坐标层）
- 本文**没有修改任何代码或其他文档**。
