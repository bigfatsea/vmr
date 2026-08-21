// Ver 2026-08-19 22:30, by Opus 5

# vmr 日志分析工具体系 — 需求再理解与第一性原理重构方案

本文是对 `vmr report` / `vmr story` 这两条命令的一次**整体性**重新审视，而不是对某一轮 UX 诉求的
续写。它包含三部分产出：

1. 对用户诉求的重新理解与澄清（抛开既有文档的既定框架）；
2. 对已有分析记录的复核——包括其中的判断错误与根因误判；
3. **重点章节**：以第一性原理重排整套日志分析工具体系（第 7 章）。

所有判断都标注证据来源（`file:line`、实测数字、或外部参考链接）。凡是我没有实际验证、只能给出
推断的，正文里会明确写"未验证"。逐条评审三份前序文档的过程记录在
`story_report_peer_review_opus-5.md`（已归档，不在版本控制范围内），本文只保留结论。

---

## 0. 摘要（结论先行）

**对"是否只需要一个 story"的回答：不是。** 实测：一个 16 天窗口的 2889 条审计记录里，
Journey 只覆盖 2672 条（**92.5%**），其余 7.5% 结构上不属于任何 Journey；且路由半区最关心的对象
（provider 账户周期、endpoint 健康度、failover attempt）在 Journey 坐标系里根本没有位置。
两条命令回答的是**不可互相推导**的两类问题。

**但你的直觉指向的真问题是对的，只是位置偏了一格。** `detail report` 确实不该属于 `report`——
它既是 `report` 的下钻终点，也是 `story` 的下钻终点，正确的位置是**两者共享的证据层**。

**真正的架构缺陷**不是"两条命令没合并"，而是一张本该完整的 3×2 表格里有四个空格是错的：

| | 宏观（全量聚合） | 中观（单任务叙事） | 微观（单请求法证） |
| --- | --- | --- | --- |
| **人读** | `vmr-report.md` ✅ | journey 决策脊柱 ⚠️ 缺一步 | `details/*.md` ✅ |
| **机读** | `vmr-report.json` ✅ | `journey-*.json` ❌ **事件流 0 字节** | `details/*.json` ⚠️ 审计记录的逐字复制 |

- 中观×人读格里塞了 **204,532 字节的 fact-layer**（占 journey 报告 68%），而中观×机读格的事件流是
  **0 字节**——最贵的载体装了最适合机读的内容，最适合机读的载体是空的。
- 中观×人读格自己还有一个洞：决策脊柱**只渲染有 tool_call 的 Step**（`render_spine_step.go` 的
  `acting` 过滤），实测样例 journey 覆盖 21/22 步，**缺的正是最后一步——没有工具调用、只有最终
  交付物正文的那一步**。所以"删掉 fact-layer"必须先补脊柱，否则删掉的是整条任务里信息价值最高的
  那一段。
- 三个格子之间**没有共同地址**：`report` 用 run-scoped 的 `s01/t01`，`story` 用内容寻址的
  `j-<client>-<start>-<end>-<hash>`，两侧内部都以 `path:line` 为主键却都没发布出去；
  `vmr-report.md` 对 `stories/` 的引用命中数是 **0**。

**修复的关键不在于把两条命令并成一条，而在于三件事**：一套跨半区的稳定坐标（§7.3）、一张双向导航矩阵（§7.5）、
一个共享证据层（§7.6）。

其中第三件的做法是**先做减法，而不是搬家**。今天的 `details/*.md` 里混着 `s01`/`t01` 这类只在
report 聚合阶段才存在的位置坐标——但微观层根本不需要知道自己在树上的位置，那是父视图的职责。
把这些减掉之后，detail 退化成 `f(一条记录, 同 lineage 的前驱)` 的纯函数：文件名可以只从
`ctxgraph.Manifest` 算出（不必读正文），两个半区各自生成的结果逐字节相同，批次顺序与本机时区
都不再进入产物。**这一刀下去，"按需生成 detail"从一个需要跨半区协调的难题变成一次幂等写盘。**

**同一把刀还要再挥两次。** 项目自己的第一性原理是 git 模型——消息是 blob、manifest 是 tree、
**blob 只存一份，tree 只持引用**（设计文档"第一性原理"一节）。但派生产物层今天到处在违反它：

- `details/*.json` 是 `json.MarshalIndent(audit.Record)`（`detail.go:75`）——**审计记录的逐字
  复制再加美化缩进**。实测 59 条记录的 detail 产物合计 **60MB**，而这 59 条记录的源日志压缩后
  只有 **334KB**（解压 27MB）。全语料 11374 条按此比例约 **12GB**，而全部审计日志是 645MB。
  而 `-details` 的默认值是 `true`。
- 同一份 ~20.5K token 的 system prompt 在 179 份 journey 报告里各存一份全文。
- 解析缓存也用 `MarshalIndent` 美化（`storyindex.go:81`、`requests.go:58`），一份机器只读、
  可随时重建的缓存被存成 88MB 的带缩进 JSON。

**减法之后的微观机读层不需要被"生成"——它已经存在**：审计日志本身就是那一格，按 `req` 坐标寻址
即可。这不是省事，这是消除一个从设计上就注定会与源数据不一致的副本。

**命令行也要跟着收敛**：今天要得到一个导航可用的套件，用户必须记得跑两条命令、并且给它们同一个
输出目录——套件闭合成了用户的责任。目标是**一个分析动词（默认产出完整套件）+ 一个按坐标读取记录
的原语 + 一种统一的记录选择器**（§7.9）。这不改变任何包边界：CLI 结构与包边界是正交的。
方案同时为"将来把这套工具变成一个只读 Web 服务"留了空间——本轮不实现任何相关功能，只守住三条
零成本的纪律（§7.12）。

**一个独立的高价值发现**：工具调用结果配对失败的根因不是"`opencode` 网关改写 ID"，而是
**agent 客户端在回写历史时去掉了下划线**；一行归一化即可把配对率从 0% 恢复到 100%，且仍是精确
匹配、不引入任何推断。详见 §5。

---

## 1. 前置诊断

### 1.1 隐含假设

| # | 假设 | 核查结论 |
| --- | --- | --- |
| A1 | report 与 story 是同一脉络的两个层级 | **只成立一半**。原子单位不同且非包含关系：report 的原子是 `audit.Record`（含全部 failover attempt 与失败），story 的原子是 `Step`（一次成功且能挂上 lineage 的往返）。`internal/story/candidates.go` 的 `ListCandidates` 排除整链 < 2 个 manifest 的 lineage——即全部单发请求。实测 7.5% 的记录不属于任何 Journey |
| A2 | detail report 是"辅助 story 的一种方式" | **方向对了一半**。`internal/report/detail.go` 的渲染结构（`renderClientRequest`/`renderAttempts`/`renderRawPreStrip`/`diffHeaderTable`/`renderBodyDiff`）里约一半是 story 永远不需要也无法解释的路由半区排障资料。detail 是两者**共同**的下钻终点 |
| A3 | "story/report 必须严格隔离"是产品原则 | **是代码原则，不是产品原则**。`internal/archtest/import_boundaries_test.go` 的 `forbiddenImports` 禁止的是 Go 包级 import；它不禁止两个**产出物**互相链接，不禁止 `cmd/vmr` 组合根同时驱动两者（CLAUDE.md 明确写了组合根是唯一允许同时看到两半的地方），也不禁止两者共同 import 一个新的共享叶子包 |
| A4 | story 想链接 detail，必须先改 report 的文件命名机制 | **不成立**，三条独立证据见 §4.8 |
| A5 | 跑 journey 报告时"顺带"生成 detail 是低成本的 | **成本相差两个数量级**。`vmr report` 的 details 是 `Build` 第二遍扫描里随 `onRecord` **全量**落盘（每请求 1 md + 1 json）。全语料 11374 条记录 → 22748 个文件 |
| A6 | 决策脊柱是 fact-layer 的完整摘要 | **不成立**。脊柱只渲染 `len(s.ToolCalls) > 0` 的 Step，且整条 Journey 无工具调用时提前返回不渲染。这是"删 fact-layer"方案里最容易踩空的一块地板 |

### 1.2 已裁决的前提

以下四条曾是本方案的关键信息缺口，现已由用户裁决填补（裁决原文记录于
`story_report_suite_reorganization_glm-4.7.md` §0.2（已归档，不在版本控制范围内）；本文按其字面含义采纳，**若记录有出入请指正，
因为 §7 的几处取舍直接依赖它们**）：

| 前提 | 裁决 | 它约束了什么 |
| --- | --- | --- |
| **首要服务对象** | 首先是自己；VMR 同时是开源项目，服务类似的个体开发者与小团队 | 产物必须自解释、零外部依赖；不需要多租户/权限 |
| **大盘与复盘的关系** | **两者独立并重**，但需要**从大盘一键下钻到微观 Task 链路的统一套件** | 直接否定"合并成一个 story"；把重点从"合并"转向"导航" |
| **detail 的定位** | 用户分析过程的**辅助阅读素材**——既辅助 Journey 分析，也辅助整体用量分析，还用于特殊任务出错时找原始记录排查 | 确认 detail 是**共享**证据层，不是任一方的附属 |
| **兼容性** | 没有稳定外部客户、没有历史包袱、JSON 无外部脚本消费，可基于第一性原理从底层重构 | 允许改文件命名、改 JSON 结构、改会话 ID 语义，无需向后兼容层 |

**第五条是我自己补的读者维度**（本轮对话中确认）：**人读为主、LLM 读为辅，两者都要**。
它的直接推论——产物必须按"变焦倍率 × 读者"两维分层，而不是在同一份文件里堆两个区块——
是第 7 章整个骨架的来源。折叠（`<details>`）是人读层的手段，对 LLM 是纯噪声（它看到的是展开后的
全文）；给 LLM 的应该是可切片的 JSON，不是折叠过的 Markdown。

### 1.3 典型错误（本类问题最容易踩的坑）

1. **拿"现状有什么"当"应当有什么"。** 这是本轮最容易犯、也确实反复犯了的错：看到 `renderDetail`
   依赖 `report.ReqInfo`，就去论证"怎么把这份依赖安顿好"，而不是先问"这些东西本来就该在 detail
   里吗"；看到机读层是空的，就想把人读层的正文搬过去，而不是先问"正文该有几份"。
   **一个被现状绑架的前提会派生出一串为它服务的加固设计**——本方案里 S1（detail 依赖）派生出
   "会话 ID 内容寻址是正确性前提"，S3（搬事件流）派生出"接受 `details/*.json` 这份副本"，
   都是这样长出来的。识别方法：每当写下"因为 X 现在是这样，所以要 Y"，把它翻成
   "X 本来就该这样吗"再问一遍。
   **同一个错误还有一个更隐蔽的变体：拿既有裁决当不可挑战的前提。** 已知问题清单、项目根说明、
   设计文档里的每一条结论，都是在**当时的架构、当时的约束**下做出的；架构一变，前提可能已经不
   成立。§7.10 就是被这个变体绊倒后重推的——引用一条裁决而不复核它的前提，和引用现状一样，
   都是用别人的结论替代自己的判断。**登记过的决定是省去重复论证的起点，不是禁止重新论证的封条。**
2. **把"内容重复"等价成"该合并"。** 重复的是数据源，不是职责。正确检验是逐项问"删掉它之后，
   哪个具体问题回答不了"（§3 就是这个检验）。
3. **拿 CLI 命令数量当设计单位。** 命令是入口，不是架构。"统一数据层/一次扫描"与"合并成一个
   命令"是两件独立的事。
4. **把 `<details>` 折叠当体积问题的解药。** 折叠只减少视觉高度，不减少字节。290KB 的 Markdown
   在编辑器与 GitHub 里照样卡。fact-layer 的问题不是"它展开着"，是"它在这份文件里"。
5. **在删掉一个视图之前不核查它的替代物覆盖了多少。** A6 就是这么被漏掉的：连续两轮讨论
   fact-layer 的去留，都没人去数脊柱到底覆盖了几步。
6. **让人读产物依赖另一条命令的运行顺序。** 一份会 404 的链接比没有链接更糟。任何链接方案必须
   自带存在性证明。
7. **搬动已校准的判据。** 九个 Finding 检测器、`ctxgraph` 的 `contractLenRatio`/`forkCoverage`
   都在 7112 条记录上校准过（设计文档 §6）；而 `stitch` 的两个阈值**明确尚未校准**（§7 已登记）。
   重构可以动组装方式，不该动这些。
8. **拿单一样例当全语料代表。** 本轮的两条样例 journey 的客户端（openclaw/lobster）恰好属于会改写
   tool_call id 的那一族——§5 证明这是**客户端**特征而非链路特征，是一个已知偏样本。

---

## 2. 事实基线：两条命令今天究竟产出什么

### 2.1 产物清单

**`vmr report`**（`cmd/vmr/cmd_report.go`，实现全在 `internal/report`）

| 产物 | 内容 | 读者 |
| --- | --- | --- |
| `vmr-report.md` / `.json` | §0 摘要 · §1 成本与 Token · §2 成本估算 · §2.5 账户消耗与额度 · §3 可靠性 · §4 延迟吞吐 · §5 负载分布 · §5.5 按客户端的上游归属 · §6 会话与任务（含 §6.5 Sticky · §6.6 端点性价比 · §6.7 Compaction 还原）· §7 效率与浪费 · §8 详单入口 · 附录 | 人 / 脚本 |
| `vmr-requests.md` / `.json` | 请求索引：按 Chat User 分组，`RequestRow` 列表 + `files` 解析缓存 | 人 / 脚本 |
| `vmr-requests-<tag>.md` | 该 client 的 Session→Task→Turn 展开，每轮链接到 `details/*.md` | 人 |
| `vmr-requests-cron-<class>.md` | 单发定时脚手架（heartbeat/dream_diary）独立成文件 | 人 |
| `vmr-requests-failed.jsonl` / `.md` | 失败请求过滤导出 | 人 / 脚本 |
| `details/*.md` + `*.json` | 逐请求全量详单：Client→VMR、VMR→上游每次 attempt（header diff / body diff / 剥离前原文）、VMR→Client | 人 / 排障 |

**`vmr story`**（`cmd/vmr/cmd_story.go`，实现全在 `internal/story`）

| 产物 | 内容 | 读者 |
| --- | --- | --- |
| `stories/vmr-stories.json` / `.md` | 候选 Journey 索引 + `ctxgraph.FileCache` 解析缓存 | 人 / 缓存 |
| `stories/journey-<id>.md` | 头部元信息 · System Prompt（折叠）· 概览卡 · 模型使用 · **决策脊柱** · **`## t0X`/`### Step N` fact-layer** · 工具调用时序图 · 疑似问题 · （可选）LLM 解读 | 人 |
| `stories/journey-<id>.json` | `JourneySummary`：ID/Title/From/To/Partial/**Metrics**/**Findings**/**LLMFindings** —— **不含任何消息正文** | 脚本 |
| `stories/compare-<a>-<b>.md` / `.json` | 行为剖面对比 · 工具对比 · 分叉点 · 端点/缓存/SysPrompt/交付物核查 · 证据溯源 · （可选）两段 LLM 解读 | 人 / 脚本 |
| `stories/vmr-story-corpus.md` / `.json` | 语料级统计（指标分布、Finding 命中率、相关性） | 人 / 脚本 |

### 2.2 实测基线

> **P1（commit `30c5159`）、P2（见 `docs/future-strategy/story_report_p2_action_plan_sonnet-5.md`）、
> P3（见 `docs/future-strategy/story_report_p3_action_plan_sonnet-5.md`）、P4（见
> `docs/future-strategy/story_report_p4_action_plan_sonnet-5.md`）与 P5（见
> `docs/future-strategy/story_report_p5_action_plan_sonnet-5.md`）均已完成**：本节下方的数字
> （工具结果 0 条、脊柱 21/22 步、detail 文件名批次碰撞率/本机时区依赖、`details/*.json` 逐字复制、
> §7.10 的 83.8s/71.8s/1.17× 缓存收益、`journey-<id>.json` = 6,428 字节且"事件流在机读层占 0 字节"、
> 下方"单条 Journey 报告的构成"表格里 fact-layer 占 68% 体积等）是这五个阶段之前的历史基线，不是
> 当前状态——保留原文只是为了保留"问题曾经确实存在"的证据链，读者若要了解当前行为，请以五份
> ActionPlan 的执行记录为准，不要把这里的数字当成现状。P3 批 D 实测的当前数字：同一份 34 文件/
> 177MB 压缩语料，冷启动 ~83.8s 不变，热缓存从 71.8s 降到 ~16.2s（收益 1.17×→~5.2×）——尚未达到
> `vmr story` 的个位数秒量级，差距诊断见 `docs/KNOWN_ISSUES_sonnet-5.md` §1.1/§1.23（`session.go`
> 的 `collect()`/`analyzeFile` 是报表三趟扫描里唯一仍未接入缓存的一遍）。P4 已给
> `journey-<id>.json` 补上完整的 Task/Step/Event/ToolCall 结构（`structure` 字段，
> `internal/story/structure.go`）：事件流在机读层不再是 0 字节，真实语料（22 步/33 次工具调用的
> 样例 Journey）实测 92KB，且已用真实语料证明"无损重建 + 体积随步数而非对话长度增长"两条同时成立
> （`TestBuildStructure_LosslessReconstruction`/`TestBuildStructure_VolumeBoundedByStepsNotProseLength`）。
> P5 已删除 fact-layer、给决策脊柱每步补上详单链接、系统提示词改为引用共享证据条目：同一样例
> Journey 的人读报告从 298,732 字节（fact-layer 占 68%）降到约 107KB；决策脊柱的工具结果配对（P1
> 已修复）与 22/22 步覆盖不变，但 Edit/StitchEdge/SysChanged/Compaction/NoReply 五类跨记录分析
> 事实原样搬进了脊柱本身（详单渲染器物理上无法重建它们）；`vmr story` 材料化的详单与 `vmr report
> -details` 对同一条记录（含缝合边界，`prev` 均为 `nil`）生成的详单已用真实语料与自动化测试
> （`TestEnsureJourneyDetails_MatchesReportDetails`）验证逐字节相同。
>
> **P6（见 `docs/future-strategy/story_report_p6_action_plan_sonnet-5.md`）也已完成**：会话身份
> 改为内容寻址（`l-<hash8>`，与 Journey id 同一套 `RootHash` 前缀口径）；导航矩阵六条边补齐
> （`vmr-report.md`→`stories/vmr-stories.md`、会话行→journey、journey→返回入口、详单→返回索引、
> report §8 从纯文本提示改为按需读取原语的示例坐标），真实语料端到端走查确认无死链接；
> `vmr-stories.md` 按标题内容标记分类，噪声类（心跳/子代理，真实语料占比过半）默认折叠；
> `vmr story -llm-addr` 自身产生的分析流量默认从两侧统计中排除；新增 `vmr analyze`（report+story
> 一次调用产出同一输出目录下的完整套件）与 `vmr replay -req` 免位置参数。

以下数字全部用**当前工作区代码**构建的二进制实测（含尚未提交的 6 项 UX 改动），或对本机
`reports/` 产物直接统计。

**单条 Journey 报告的构成**（`-journey 'j-openclaw-20260728T000544*'`，22 轮、33 次工具调用）：

| 区段 | 行数 | 字节 | 占比 |
| --- | --- | --- | --- |
| 头部（含 System Prompt 折叠块） | 711 | 50,132 | 17% |
| **决策脊柱** | 1,100 | 42,809 | **14%** |
| **fact-layer（`## t01`/`## t02`）** | 2,408 | 204,532 | **68%** |
| 合计 | 4,243 | 298,732 | |

**与之对应的机读层**：`journey-<id>.json` = **6,428 字节**，`JourneySummary` 结构体
（`metrics.go:399-407`）**不含任何消息正文字段**。事件流在人读层占 204KB，在机读层占 0 字节。

**决策脊柱的覆盖**：21 / 22 步。唯一缺失的是 **Step 22**——这条 Journey 的最后一步，
没有工具调用、只有最终报告正文。脊柱内工具结果行（`↩️`/`❌`）**0 条**（33 次调用全部静默降级，
根因见 §5）。

**Journey 对全量流量的覆盖**（`vmr story` 跑 07-25..08-09 共 16 个日志文件）：

| 指标 | 数值 |
| --- | --- |
| 审计记录总数 | 2,889 |
| 候选 Journey 数 | 220 |
| Journey 覆盖的记录数 | 2,672（**92.49%**） |
| 其中 ≤2 轮的 heartbeat 类 | **120 条 journey（55%）**，只覆盖 240 条记录（9%） |
| 客户端分布 | lobster 179 / pimini 15 / hermes 11 / openclaw 8 / **vmrstory 4** / aiscript 2 |
| 运行耗时 | 4.52s（冷缓存，含缓存写入）——**这只是 16 文件窗口的数字，不能代表全量语料**，全量数字见下表 |

**其他实测**：

| 观测 | 数字 | 依据 |
| --- | --- | --- |
| `vmr-stories.json` 体积（长期累积后） | **88MB**，其中 `journeys` 只有 3 行，其余全是 `ctxgraph.FileCache` | `ls -la` + `json.load` |
| 缓存的重复条目 | 同一文件同时以绝对路径与相对路径存在两份，共 255 条 manifest 重复 | 按 `(basename, line)` 去重后 11629 → 11374 |
| detail 文件名批次去重实际触发率 | **10 个键 / 11374 条记录 = 0.088%** | 按 `(ts_ms, virtual, endpoint, outcome)` 近似 `detailFileName` 的键在全语料上计数 |
| **detail 文件名依赖本机时区** | `detail.go:358` 用 `rec.TS.In(fmtutil.DisplayZone)`；`fmtutil/timezone_test.go:13` 锁死 `DisplayZone == time.Local` | 读源码 |
| 物理读放大 | `vmr report` **3 遍**（`ctxgraph/scan.go:129` + `report/session.go:302` + `report/aggregate.go:198`，前两遍并发）；`vmr story` **2 遍**（`ctxgraph/scan.go:129` + `ctxgraph/records.go:82`） | 读源码 |
| **语料真实规模** | 34 个审计文件、11374 条记录，**压缩 177MB / 解压 9002MB** | `zstdcat \| wc -c` 逐文件累加 |
| **全量运行耗时与缓存收益** | `vmr story`（列表）冷 16.8s / 热 **3.4s**（**5.0×**）；`vmr report -details=false` 冷 83.8s / 热 **71.8s**（**1.17×**）。差距全部来自缓存覆盖率：story 唯一一遍昂贵解析被缓存，report 三遍里只缓存一遍 | 用当前构建的二进制实测 |
| **解析缓存没有 schema 版本戳** | 缓存条目只记文件内容哈希（`ctxgraph/cache.go` 的 `CachedFile{Hash, Manifests, NoBody}`），命中判据里没有任何版本信息——改了提取逻辑，旧缓存会被静默复用 | 读源码 |
| 自指流量 | `client_key_tag = vmrstory` 共 **21 条记录 / 4 条 journey**——`vmr story -llm-addr` 的解读调用经 VMR 回流进审计日志 | `zstdcat \| grep -o` 全窗口统计 |
| 半区体量 | 分析半区 21,145 行 / 路由半区 12,552 行（非测试 `.go`） | `find \| cat \| wc -l` |
| System Prompt 重复 | openclaw 报告头部 50KB（17%）；一条 2 轮 heartbeat journey 全文 20,559 字节中约 68% 是 system prompt；实测窗口里 **179 条 lobster journey 共享同一份 ~20.5K token 的 system prompt** | 渲染产物统计 |
| **证据层的体积放大** | 59 条记录 → `details/` 合计 **60MB**（`.json` 40MB + `.md` 20MB，平均 709KB + 353KB / 条）；同这 59 条记录的源日志压缩后 **334KB**、解压 27MB。按比例全语料约 **12GB**（全部审计日志 645MB）。`-details` 默认 `true` | `du -ch` + `zstdcat \| wc -c` |
| **`details/*.json` 是审计记录的逐字复制** | `detail.go:75` 是 `json.MarshalIndent(j.rec, "", "  ")`——原样输出 `audit.Record` 再加缩进，所以派生的 709KB 比源记录的 461KB 还大 54% | 读源码 + 逐字段比对产物结构 |
| 解析缓存被美化输出 | `storyindex.go:81` / `requests.go:58` 都用 `json.MarshalIndent`——一份机器只读、可随时重建的缓存存成带缩进的 88MB JSON | 读源码 |
| 既有的内容寻址先例 | `session.go:512` 的 `toolsSig` = `fmt.Sprintf("tools:%d/%x", len(names), sum[:4])`——工具声明集合早就是按内容哈希引用的 | 读源码 |

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

两侧消费的是**同一个 lineage 划分、同一个 `taskseg.IsNewTask` 任务边界规则**（设计文档 §3.4 记录了
这次收敛：收敛前后跑真实语料，输出逐字节一致）。差别只有两处：

1. **组装粒度**：`report` 的 `SessionInfo` = 一条 Lineage（跨 compaction 靠 `ContinuedFrom` 弱链接）；
   `story` 的 `Journey` = 一条**缝合链**（`ChainFrom`）。story 多做了一步缝合。
2. **身份**：`report` 用 run-scoped 序号（`session.go:580-582`：`fmt.Sprintf("s%02d", i+1)` /
   `t%02d`）；`story` 用内容寻址（`journey.go:566-575`：`j-<client>-<start>-<end>-<roothash[:8]>`）。

**结论：同一根骨架上挂了两套互不认识的坐标系。** 这才是"两个报告看起来在做同一件事却无法互相
跳转"的机制性原因——不是重复渲染，是**没有共同的地址**。

今天的链接现状印证了这一点：

| 边 | 状态 |
| --- | --- |
| `vmr-requests-<tag>.md` → `details/*.md` | ✅ 存在（实测 19 处链接） |
| `compare-*.md` → `journey-*.md` | ✅ 存在，且缺失时自动补生成（`ensureJourneyFile`） |
| `vmr-stories.md` → `journey-*.md` | ✅ 存在（渲染过才出现） |
| `vmr-report.md` → `vmr-requests.md` | ✅ 存在（§8） |
| **`vmr-report.md` → `stories/*`** | ❌ **完全不存在**（对 `stories`/`journey` 的命中数为 0） |
| **journey 脊柱 Step → `details/*.md`** | ❌ 不存在 |
| `vmr-report.md` → `details/` | ⚠️ 只有一句纯文本提示，不是链接 |

---

## 3. 删除检验：什么能合并、什么不能

不问"是否重复"，问"删掉它之后，哪个具体问题回答不了"。

| 待删对象 | 删掉后回答不了的问题 | 判定 |
| --- | --- | --- |
| `vmr report` 聚合层（§1/§2/§2.5/§5/§5.5/§7） | 这段时间花了多少钱、哪个账号快烧爆、缓存有没有生效、谁在吃额度 | **不可删**。story 没有跨任务聚合能力，也不看成本 |
| `vmr report` 可靠性层（§3/§4/§6.5/§6.6） | 哪条链路在失败、failover 走了几次、sticky 有没有起作用、哪个端点性价比差 | **不可删**。`Step` 只保留成功往返，`attempt` 层在 story 里根本不存在 |
| `vmr-requests-cron-*.md` 与失败导出 | 定时脚手架跑了多少次、那些全失败的请求发生了什么 | **不可删**。实测 7.5% 的记录（217 条）不属于任何 Journey；`ListCandidates` 在结构上排除全部单发请求 |
| `details/*` | 这次 502 到底发生了什么、vmr 改了哪个字节 | **不可删**，且它同时是两侧的下钻终点 |
| `vmr story` journey 叙事 | agent 是怎么想的、哪一步走偏、上下文怎么演化 | **不可删**。report 的 Session→Task→Turn 只有元数据行，没有因果 |
| `vmr story -compare` | 两套 agent 框架在同一任务上的行为差异、分叉点在哪 | **不可删** |
| `vmr story -corpus` | 哪些行为指标与结果相关（跨 Journey） | 可延后，但与 report §7 的口径不同（一个是"浪费"，一个是"行为↔结果相关性"），**不可互相替代** |
| **journey 报告里的 `## t0X` fact-layer** | ——在**两个前置条件都满足后**没有：机读层补上完整的事件流**结构**（正文按 `req` 引用证据层），且脊柱补完到覆盖无工具调用的 Step | **唯一可删的那一块，但它有前提** |

**这张表就是对"是不是只需要一个 story"的正面回答**：八项里七项不可删，唯一可删的那一项恰好就是
你自己指出的那一项。**可删的是一个渲染区块，不是一项能力。**
（这张表回答的是"哪些**能力**不可删"；命令行该有几个动词是另一个问题，见 §7.9——
早先版本正是把这两件事混在一起，从而得出了一个站不住的"不合并命令"的结论。）

需要特别记下的一条修正：早先版本的这张表把 fact-layer 写成"无条件可删"，那是错的——脊柱只覆盖
有工具调用的 Step（A6），实测样例里缺的正是最终交付物那一步。**前置条件不是形式主义，它是"删掉
的到底是冗余还是正文"的分界线。**

另外，"运维流量 vs 任务流量"并不对应"report vs story"：实测窗口里 `Daily News Brief`（16 轮）、
`Daily Finance Brief`（21 轮）这类 cron 任务链**会**形成正常的 Journey。被 story 排除的是
**结构上的单发请求**（整链 < 2 个 manifest），不是"定时任务"这个类别。

---

## 4. 对既有分析记录的复核

前序的 UX 分析记录（`story_report_ux_review_sonnet-5.md`，已归档，不在版本控制范围内）有 6 项"已直接处理"、2 项"留待确认"。
逐条复核结论如下——这不是评审仪式，其中两条的纠正直接决定了第 7 章的形状。

### 4.1 证据溯源改为按 Journey 精确定位 —— ✅ 认可

诊断与修法都对。`SourceFiles`（`storyindex.go`）从 `idx.Journeys[].Files` 取并集，数据本来就已算好
（`BuildJourneyIndexRow` 遍历 chain 上全部 manifest 的 `.Path`）。这是典型的"数据已算好但没接上"。

**一个附带的观察**：`SourceFiles` 依赖 `idx.Journeys` 里有对应行，这由 `MergeJourneyIndexRows`
保证（fresh 永远是本次运行的完整权威集合）——但这也意味着索引的 `journeys` 段是 run-scoped 的，
与同一文件里永久累积的 `files` 段语义不一致（§7.8 处理）。

### 4.2 LLM 解读标题层级降一级 —— ✅ 认可结论，⚠️ 方案可以更稳

事实澄清是对的：那些 `## 候选根因` 是 prompt 里要求 LLM 输出的，不是 Go 拼的
（`internal/i18n/story_llm.go`）。选"改 prompt 不改渲染"也对——两次 LLM 调用的证据包内容完全不同，
不该合并。

**但文档的**结构**正确性不该外包给 LLM 的指令遵从度**。更稳的做法：在 `RenderLLMSection` 里对
返回文本做一次确定性的标题降级（仅在 LLM 段落内，行首 `## ` → `### `），**必须跳过围栏内的 `#`**
（LLM 返回文本里出现代码块是常态）。prompt 侧指令保留作第一道，渲染兜底作第二道。优先级：低。

### 4.3 多行/超长工具调用参数默认折叠 —— ✅ 认可

"预览用整段拉平后的前 160 字符，而不是严格第一行"这个偏离原始表述的选择，理由（heredoc 的第一行
`python3 << 'PYEOF'` 没有信息量）成立。

### 4.4 脊柱 Step 原始消息折叠而非截断 —— ✅ 认可

`payloadBlock` 已经证明了"折叠而非截断"这个模式，"完整"与"脊柱不膨胀"不冲突。

### 4.5 System Prompt 移到文档头部 —— ✅ 认可，但产生了一个新问题

`systemPromptEras` 复用 `NewEvents` 的消息级去重来分段（而不是猜），是正确做法；只影响 Markdown
渲染、不动 JSON 契约的判断也核对无误。这条改动同时是 §7 分层方案的第一块拼图——它把一段"每个读者
只需要看一次的背景资料"从时间轴里抽出来放到了固定位置。

**但它把资料从"每步重复"变成了"每份报告重复"**：实测窗口里 179 条 lobster journey 各自内嵌一份
完整的、几乎逐字节相同的 ~20.5K token system prompt；一条 2 轮 heartbeat journey 有 68% 的字节是
它。这正是 §7.6 要处理的：system prompt 是共享证据，不是每份报告的私产。

### 4.6 脊柱补充工具调用结果 —— ✅ 实现认可，❌ 根因判定错误

实现（按 `ToolCall.ID` 精确配对、配不上就干净地不渲染）正确且防御性好。用当前构建重新渲染样例
journey 复现了它的失效：33 次工具调用，脊柱内工具结果行 **0 条**。

**但它给出的根因是错的**，且这个错误被写进 `KNOWN_ISSUES §1.21` 成为"不修的理由"，并被后续两份
方案原样继承。完整证据见 §5。

### 4.7 Compare 开篇完整初始 User Message —— ✅ 该做，三个"待拍板"现已定案

1. **要不要设上限**：设。沿用 `renderExcerpt` 既有的"有界 + 折叠 + 截断时标注"惯例。反例
   （几千字符 JSON 粘贴）真实存在，无界展示会毁掉 compare 报告的可读性。
2. **放在哪**：保留现有短摘要（`SideBlock` 的 `Title`，负责"扫一眼是什么任务"），**下面新增一个
   折叠块**。概览与全文各司其职。
3. **要不要进 `compare-*.json`**：**进**。`BuildEvidencePack`（`llm.go:140-148`）今天给 LLM 的
   任务描述只有 `journeyTaskTitles`（80 rune 截断）。**分叉点分析的质量直接取决于 LLM 是否理解
   原始指令**，这是整个证据包里单位 token 价值最高的一段。以 2000 字符为上限，对一个本来就几千
   字符的证据包是可接受的边际成本。

### 4.8 fact-layer 删除 + 脊柱链接 detail —— ❌ "不可行"论证被推翻，但前提比原设想多

原论证链条：`story ↛ report` 的 import 边界 → 无法自动生成 detail → 即使只拼路径，
`detailFileName` 依赖跨批次去重计数器 → `story` 侧无法确定性推算文件名 → **必须先改 `report` 侧
命名机制才能解决**。

**三条独立证据推翻这个结论：**

1. **不需要"推算"，因为映射已经被发布了。** `internal/report/rows.go:499` 的 `RequestRow` 带
   `DetailFile string \`json:"detail_file,omitempty"\`` 字段，随 `vmr-requests.json` 落盘：
   ```json
   {"ts":"2026-07-25T00:53:29+08:00","session":"s01","task":"t01","turn":1, ...,
    "detail_file":"20260725-005329.261_agent_doubao-seed-2.0-lite_ok.md"}
   ```
   `story` 侧读一个 JSON 即可得到权威映射，既不 import `report` 包，也不复制它的命名规则。
2. **`report.WriteDetails` 本来就是为这个场景导出的。** `detail.go:219` 的函数注释原文：
   *"a standalone alternative to Build's onRecord hook … for callers that want detail export
   without running the full aggregation pass."* 它连同 `AnalyzeSessions` 都是导出的，在 `cmd/vmr`
   组合根调用不触碰任何 import 边界。
3. **"自动生成缺失的兄弟产物并链接过去"在 story 内部已是既成模式。** `compareJourneys` 调用
   `ensureJourneyFile(jA/jB)`，`JourneyRef.ReportFile` 由 `JourneyReportFile(s.ID, s.Partial)`
   确定性推算并渲染成链接。跨半区做同一件事，唯一的新增要求是"在组合根做"。

> 这三条只是用来证明"此路不通"不成立。**最终方案走的比它们都更简单的一条**：§7.6(a) 让 detail
> 退化成纯函数之后，story 既不需要读 `vmr-requests.json` 的映射，也不需要经组合根去跑
> `report.AnalyzeSessions`——它自己就能算出文件名并渲染。上面三条因此是"退路"，不是"路线"。

**原论证里唯一站得住的部分，量化后是边际的**：`detailFileName` 的批次去重计数器实测触发率
**0.088%**（10 键 / 11374 条），本机 59 个 detail 文件中 0 例。

**但复核过程中发现了两个原论证和后续方案都没有的、更硬的前提**：

- **文件名还依赖本机时区**（`detail.go:358` 的 `rec.TS.In(fmtutil.DisplayZone)`，而
  `DisplayZone` 默认就是 `time.Local`）。任何"确定性命名"方案必须同时消除计数器和时区两个
  不确定性来源，只解决前者是不够的。
- **今天的 detail 正文里混进了只在 report 聚合阶段才存在的坐标。** `renderDetail` 渲染
  `info.SessionID`/`info.TaskID`/`info.SessSeq`（`detail.go:493-494`）和
  `info.Parent.DetailFile`（上一轮链接，`detail.go:496`）。`SessionID` 是 `s%02d` **位置序号**——
  同一条 lineage 在"跑 1 个文件"与"跑 16 个文件"两种批次里会拿到不同编号；`Parent` 落在子集之外时
  上一轮链接直接断掉。所以按 Journey 子集生成的 detail，**正文**也会与全量 report 生成的不一致。

  **但这不是一个要靠"给会话 ID 也做内容寻址"去兜住的问题**——那是拿现状当约束。第一性原理的答案
  是把这些坐标**从 detail 里拿掉**：微观层是一片叶子，叶子不需要知道自己在树上的位置，那是父视图
  的职责（§7.6）。减法做完，子集与全量生成逐字节相同，问题从根上消失。

**还有一个前置条件与 import 边界无关，纯粹是内容层面的**：脊柱不覆盖无工具调用的 Step（A6）。
删 fact-layer 之前必须先补脊柱，否则删掉的是最终交付物。

---

## 5. 工具调用结果配对失败的真实根因，与一行修复

### 5.1 既有结论

`KNOWN_ISSUES §1.21` 记载：openclaw / lobster 两条 Journey 的 tool_call 配对成功率 0%；根因判定为
"`opencode`（VMR 与 deepseek-v4-pro 之间的代理/网关）在转发时改写了 ID"，并据此判定"不该现在修"，
因为唯一的修法（位置兜底）会"在协议保证之外引入一条推断规则"。

### 5.2 复核方法

直接对原始审计日志做统计：正则抓响应体 SSE 里的 `"id":"call…"`，与请求体
`messages[].tool_calls[].id` / `messages[].tool_call_id` 做集合比较，按 `client_key_tag` 与
`endpoint` 两种口径分组。样本：`2026-07-16 / 07-19 / 07-28 / 08-14 / 08-16` 五个日志文件，
合计 4159 条记录。

### 5.3 结果：改写在客户端，不在上游

**按 `endpoint` 分组（08-14）**：

| endpoint | 请求侧 id 带 `_` | 响应侧 id 带 `_` |
| --- | --- | --- |
| `openai:deepseek:deepseek-v4-flash` | 0.0% (n=138) | 100.0% (n=12) |
| `openai:volcengine:deepseek-v4-flash` | 0.0% (n=290) | 100.0% (n=268) |
| `openai:volcengine2:deepseek-v4-flash` | 0.0% (n=148) | 100.0% (n=5) |
| `openai:volcengine:doubao-seed-2.1-turbo` | 0.0% (n=271) | 100.0% (n=56) |
| `openai:cliproxy:gemini-3.7-flash-high` | 0.0% (n=117) | — |

五个不同的 provider，**没有一个经过 `opencode`**，却全部呈现同一种改写。

**按 `client_key_tag` 分组（07-28，同一天、同一批上游）**：

| client | 请求侧带 `_` | 响应侧带 `_` | id 集合交集 | 去下划线归一化后交集 |
| --- | --- | --- | --- | --- |
| `hermes` | 100.0% (n=91) | 100.0% | **100.0%** | 100.0% |
| `pimini` | 100.0% (n=260) | 100.0% | **100.0%** | 100.0% |
| `lobster` | **0.0%** (n=64) | 100.0% | **0.0%** | **100.0%** |
| `openclaw` | **0.0%** (n=33) | 100.0% | **0.0%** | **100.0%** |

**结论（高置信）**：改写发生在**客户端**。同一天、同一批端点上，`hermes`/`pimini` 的 id 两侧完全
一致；`lobster`/`openclaw`（同属 OpenClaw 家族）把响应里的 `call_00_xxx` 回写成 `call00xxx`。
顺带说明：07-28 当天 `openai:opencode:deepseek-v4-pro` 链路整体的 id 集合交集是 **59.6%**、
请求侧 76.5% 的 id 仍带下划线，与"这条链路会剥离下划线"直接矛盾。

（另有第三种形态：`cliproxy:gemini` 链路上出现 `exec1786691703864731…` / `process1786697731217703…`
形状的 id，即"工具名 + epoch 微秒"——客户端**自造** id，这类无论如何都不可能按 id 配对。）

### 5.4 修复：归一化后仍是精确匹配

改写是**确定性的字符级变换**，所以配对不需要引入位置推断——只需在比较前对两侧做同一个归一化
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
`lobster(64) + openclaw(33) = 97`，即归一化合并的全部是真配对，**零误合并**。

### 5.5 位置兜底：实测安全，但归一化之后它只剩边角场景

对自造 id 的客户端，归一化也救不回来，只能按位置配对。这一档的安全性同样用数据验证过——
对每个 `assistant(tool_calls)` → 随后连续 `role=tool` 消息组，比较（归一化后的）id 序列是否逐位相同：

| 文件 | 组数 | 顺序完全一致 | 数量一致但顺序不同 | 数量不一致 |
| --- | --- | --- | --- | --- |
| 07-16 | 113,315 | 113,315 | 0 | 0 |
| 08-14 | 32,053 | 32,053 | 0 | 0 |
| 07-28 | 7,599 | 7,599 | 0 | 0 |

**152,967 组，100% 同序，零数量不匹配。** 范围声明：这验证的是"客户端在同一请求内保持
tool_calls 与 tool 消息同序"；"响应流顺序 → 客户端回写顺序"这一段未单独验证。

### 5.6 分层纪律：推断与事实的分界线画在哪

三级降级：**精确 ID → 归一化 ID → 位置配对**。分界线**不在归一化那一格**：

- 归一化之后仍然是**精确的一一匹配**，只是键做了规范化，没有引入任何顺序假设——因此
  **前两级都可以作为 Findings 检测器的证据基础**。
- 真正的推断只有第三级。**位置配对限制在渲染层，且必须标注"按位置推断，ID 未匹配"**；
  Findings 层不接受它作为证据。

这条纪律的意义：`§1.21` 里"不愿引入推断规则"这条不修理由，在归一化修法下**根本不成立**——
它拦住的是第三级，不该连第二级一起拦。

### 5.7 建议

1. 把 `KNOWN_ISSUES §1.21` 的根因改写为"**agent 客户端在回写工具调用历史时改写了 id**
   （OpenClaw 家族去下划线；部分链路自造 `name+timestamp` 形态 id），与上游 provider 无关"，
   并附按 client 分组的对照数据。
2. `chatmsg` 的配对逻辑加归一化回退（先精确、失败再归一化）；渲染层再加位置兜底并标注。
3. **这条修完之后，脊柱展示工具结果才真正在主力语料上产生可见效果**——目前它对两个主要 client
   全程静默降级。

---

## 6. 原始 UX 诉求的最终评估

| # | 诉求 | 评估 |
| --- | --- | --- |
| 1 | compare 开篇挂 journey 链接 | 已生效（`JourneyRef.ReportFile`）。**保持** |
| 2 | compare 开篇展示完整初始 User Message | **该做**，按 §4.7 的三点定案。真正的价值在给 LLM 解读用，人读侧短摘要已经够 |
| 3 | 证据溯源只列相关文件 | **已做且正确** |
| 4 | 两段 LLM 解读收进大章节 | **已做**，建议补渲染层兜底（§4.2） |
| 5 | 脊柱 Step 消息完整、不截断 | **已做**（折叠而非截断，正确） |
| 6 | System Prompt 移到头部折叠 | **已做**，但引出"每份报告重复一份"的新问题（§7.6） |
| 7 | 脊柱展示工具调用结果 | **已做但对主力语料全程静默失效**（实测 0/33），需先修 §5 |
| 8 | 多行工具参数默认折叠 + 首行预览 | **已做** |
| 9 | 去掉 `## t0X` fact-layer、脊柱挂 detail 链接 | **判断正确，且是唯一真正动架构的一项。但有三个前置条件**：机读层补上事件流结构（正文按引用）、脊柱补完到覆盖无工具调用的 Step、detail 做完减法并改为确定性命名。原"不可行"论证不成立（§4.8） |

> 原诉求里"如果拆开不好拆，就用一大块对应整组工具调用"——按 id 精确拆分并不难
> （`toolResultsFor` 本来就按 id 配对），精细方案是对的；§5 修完后它才真正跑得起来。整组呈现
> 保留为第三级兜底的展示形态。

---

## 7. 【重点】第一性原理重构：日志分析工具体系

### 7.1 从原料出发：三级变焦，一个共享的证据终点

**原料**：append-only 的 JSONL，一条记录 = 一次客户端请求的完整物理事实（客户端 ↔ vmr 层 +
vmr ↔ 上游每次 attempt 层）。**其余一切都是 view。**

读者对它的提问，按"处在什么情境下问"穷举，只有三个**变焦倍率**：

| 倍率 | 问题 | 主轴 | 今天由谁回答 |
| --- | --- | --- | --- |
| **宏观** | 钱花在哪、额度还剩多少、哪条链路在失败、sticky 有没有用、谁在消耗 | 时间 / 链路 | `vmr report` §0–§7 |
| **中观** | 这次任务是怎么被完成的、哪一步走偏、两个 agent 差在哪 | 因果 | `vmr story` |
| **微观** | 这一次请求上游到底收到、返回了什么字节、为什么 failover | 单条记录 | `details/*` |

"变焦"这个模型比"三类问题"更准确的地方在于：**它们是连续的，不是三个割裂的产品**。读者不会
在宏观停下——看到某个 client 突增就要问是哪个任务，看到某一步失败就要问上游到底返回了什么。
一个只能定格在某一倍率的工具，等于强迫读者每次换倍率就换一套坐标重新找位置。

**关键判断：微观层不是第三个"命令"，它是前两个倍率共同的证据终点。**
`vmr-requests-failed.md` 的每一行、§7 效率发现的每一条、§3 的每个失败率，最终都要落到某条具体
记录上；journey 的每一步同样如此。

> 你说"detail report 不过是辅助 story 解决问题的其中一个方式方法"。**方向对了一半**：detail 确实
> 是 story 的下钻终点，但它同样是宏观层的下钻终点。正确的结论不是"把 detail 收编进 story"，
> 而是**把它从 `report` 的附属提升为与两者平级的共享证据层**。它今天挂在 `reports/details/` 下
> 纯粹是历史原因——report 先做。

### 7.2 目标模型：3×2 矩阵

变焦倍率是横轴，读者是纵轴。两条轴正交，合起来是这套体系的完整形状：

| | **宏观**（全量聚合） | **中观**（单任务叙事） | **微观**（单请求法证） |
| --- | --- | --- | --- |
| **人读**<br>目标：纵向空间与跳转 | `vmr-report.md`<br>`vmr-requests*.md` | `journey-<id>.md`（脊柱 + 链接）<br>`compare-*.md` | `details/<req>.md`（按需渲染） |
| **机读**（脚本 / LLM）<br>目标：完整性与可寻址 | `vmr-report.json`<br>`vmr-requests.json` | `journey-<id>.json`（结构 + `req` 引用） | **审计日志本身**（按 `req` 坐标寻址） |

最右下角那一格值得单独说明：**它不需要被"生成"，它已经存在**。`details/*.json` 今天是
`json.MarshalIndent(audit.Record)` 的逐字复制（`detail.go:75`），也就是把源数据解压、加缩进、
拆成一个个文件重存一遍——59 条记录换来 40MB，而源记录本身压缩后是 334KB。一份注定与源数据
同构、又注定可能与它不一致的副本，没有存在理由。有了稳定的 `req` 坐标，"取这条记录的原文"是
一次定位操作，不是一次物化操作。

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
        │        ★ 坐标层（缺失 —— 本方案的关键新增，§7.3）      │
        │   请求级 req：<audit-basename>:<line>（两侧都发布）     │
        │   会话级：lineage 内容寻址 ID（s01 退为显示别名）        │
        │   证据级：文件名 = hash(req)（去批次、去时区、去 -N，    │
        │           只凭 Manifest 即可算出，无需读正文）           │
        └──────────────────────────┬──────────────────────────┘
                                   │
     ┌──────────────┬──────────────┴───────────────┬──────────────────┐
     ▼              ▼                              ▼                  ▼
   宏观           中观                        共享证据层           语料级统计
 vmr report     vmr story              details/*.md（人读，按需）   story -corpus
                                       evidence/*.md（共享 blob）
                                       审计日志本身（机读，按 req）
        └──────────── 导航矩阵（§7.5，双向）────────────┘
```

**今天矩阵里的五处错**，按修复优先级：

1. **中观×机读是空的**（事件流 0 字节），而中观×人读塞了 204KB 事件流 —— 载体与内容完全对调。
2. **中观×人读自己有洞**：脊柱不覆盖无工具调用的 Step，实测缺的是最终交付物。
3. **格与格之间没有地址**：宏观→中观的链接完全不存在，中观→微观的链接不存在。
4. **共享内容被复制而不是被引用**（三处，全都违反项目自己的 blob/tree 原则）：
   system prompt 在 179 份 journey 报告里各存一份全文；`details/*.json` 逐字复制审计记录；
   解析缓存被美化输出成 88MB。
5. **微观层被无条件全量物化**：`-details` 默认 `true`，一次全语料 report 会写出约 12GB 派生文件。

第 4、5 两条是本轮回头重审时才浮出来的——前几版方案（包括本方案的早期版本）都默认接受了
"派生产物就该把内容抄一份"这个前提，而项目的第一性原理恰恰相反。

### 7.3 坐标层：三件套

**问题**：两侧内部的真正主键都是 `path:line`（`report` 的 `SessionAnalysis.byKey`——
`session.go:526-533` 的 `recLoc`；`ctxgraph.Manifest.Path/Line`），但两侧的公开 JSON 都没有发布它。

#### (a) 请求级坐标 `req`

```jsonc
// vmr-requests.json 的 requests[] 新增
{ "ts": "...", "req": "vmr-audit-2026-07-25.jsonl:317", "detail_file": "...", ... }

// journey-<id>.json 的每个 Step 携带同一个键
{ "seq": 7, "req": "vmr-audit-2026-07-25.jsonl:317", ... }
```

为什么用 `basename:line`：

| 候选 | 优点 | 缺点 | 判定 |
| --- | --- | --- | --- |
| 绝对 `path:line` | 已是内部主键，零成本 | housekeeping 压缩把 `.jsonl` 变成 `.jsonl.zst`，路径变；跨机器路径不同 | 否 |
| **`basename:line`（规范化掉 `.zst`）** | 稳定（轮转只改扩展名、不重排行）、零成本、人可读、可直接 `sed -n` 定位 | 跨目录同名文件理论冲突（审计文件名带日期，实践中不冲突；落地时加一条断言） | **推荐** |
| 记录内容 hash | 完全稳定 | 需多算一次哈希；不可读；无法直接定位回原文件 | 备选 |

**坐标契约三条，必须写死在文档里而不是留给读源码推断**——它是主键，歧义的代价由所有下游承担：

- **`line` 的基准**：审计文件解压后按 `\n` 切分的 **1-based 逻辑行**（与解析层今天的读取路径一致）。
  不是压缩文件内的字节偏移，也不是解压后的字节偏移。
- **`basename` 的规范化**：去掉压缩扩展名，使一份日志在压缩前后得到同一个坐标。
- **同名冲突**：审计文件名带日期，跨目录同名在实践中不发生。落地时加断言**响亮地失败**，
  而不是悄悄降级——单机工具撞上这种情况，明确报错并提示用绝对路径消歧，好过引入一条更复杂的
  命名规则去兜一个不存在的场景。

**坐标同时是文档内锚点。** 由坐标派生的稳定锚点挂在脊柱的每一步与详单的每次尝试上，
使跨产物跳转从"跳到文件开头"变成"跳到那一步"。一份几十 KB 的报告里，这两者的差别是实质性的，
而成本接近于零。

顺带修掉一个实测缺陷：`ctxgraph.FileCache` 以路径字符串为 key，导致同一文件的绝对路径与相对路径
在缓存里各存一份（实测 255 条 manifest 重复）。规范化成 basename 同时解决它。

#### (b) 会话级身份：一条 Lineage 一个内容寻址 ID

`report` 的 `SessionInfo.ID = s%02d` 是 run-scoped 位置序号，`story` 的 Journey ID 是内容寻址。
把 report 那侧也换成内容寻址不是给它"加一个字段"，而是**认领一个已经存在的事实**：

> `report` 的一个 Session **就是**一条 `ctxgraph.Lineage`（`session.go` 的 `group()` 注释原文：
> "one `SessionInfo` per Lineage"）；`story` 的一个 Journey **就是**若干条 Lineage 缝合成的链
> （`ChainFrom`）。

所以正确的做法是给**Lineage 本身**一个内容寻址 ID（`l-<hash8>`，基于 `RootHash` 与首条记录到达时间戳组合哈希——实测发现纯 RootHash 在模板化定时任务/心跳上会发生真实碰撞，折入纳秒时间戳后实现零碰撞且稳定），然后：

- report 的 `SessionInfo.ID` = 该 lineage 的 ID；`s01` 退化为一份报告内部的显示别名（想留就留，
  它不再承担 identity 职责——这正是设计文档"identity 与展示文案分离"那条纪律）；
- story 的 `JourneyIndexRow` 增加 `lineages: [l-…, l-…]`（缝合链的成员）。

于是"report 的会话行 → journey"**不需要查表，它是结构性的**：journey 的成员列表里有没有这条
lineage，就是答案。这比"两边各自算一个哈希再想办法对上"少一层间接。

这条的定级：**导航能力，不是正确性前提**。早先版本把它写成正确性前提，理由是"否则子集生成的
detail 内容不一致"——那个理由在 §7.6 做完减法之后不再成立（detail 里根本不再出现会话坐标）。
把它降级是一次诚实的修正，也顺带解开了迁移路径上一条不必要的依赖。

#### (c) detail 确定性文件名

```
{ts，用记录自带的时区偏移}_{virtual}_{real}_{outcome}_{hash8}.md
```

- **`hash8` = `req` 坐标本身的哈希前 8 hex**，即 `hash(basename + ":" + line)`。
  也就是说文件名与 (a) 的坐标**是同一个身份的两种编码**：前缀 `ts_virtual_real_outcome` 纯粹是
  给人扫读和按时间排序用的装饰，唯一性由 `hash8` 承担。
  **哈希口径沿用项目既有惯例**：md5 取前 4 字节渲染成 8 位十六进制（与工具集指纹的写法一致）。
  写死它是为了不被实现两遍——但**不需要**把全链路的哈希统一成一种：内容身份用 md5、文件完整性
  校验用 sha256，两者服务于不同目的，而跨模块关联靠的是**同一个键**，不是同一个哈希函数
  （任务 ID 用 lineage 根哈希、详单名用坐标哈希，这两者从不需要互相比较）。
- **不能**用 `Manifest.SysHash + Keys[0]` 的组合——那两个值在一条纯追加的 Journey 内**每一轮都
  相同**，用作后缀会把偶发碰撞变成整条 Journey 的必然碰撞。
- 也**不必**用请求体哈希：请求体哈希在"重试重发完全相同 body"时仍会撞，于是又得请回 `-N` 后缀，
  而 `-N` 恰恰把批次顺序依赖带了回来。坐标是主键，天然唯一，**`-N` 后缀可以彻底删除**。
- `ts` 用**记录自带的时区偏移**格式化，不走 `fmtutil.DisplayZone`（默认 `time.Local`）。
  先例是 `deriveID`（`journey.go:571-572` 直接 `root.TS.Format(idTimeLayout)`），也是设计文档
  "时区：一处显示权威"那条纪律里 story 的既有例外。

**这个取法带来一个关键性质：文件名可以只从 `ctxgraph.Manifest` 算出，不需要读记录正文。**
Manifest 里 path/line/ts/model/endpoint/outcome 全都有，而 Manifest 本来就在解析缓存里。于是：

- 任何持有索引的组件（journey 脊柱、requests 索引）**随时可以算出链接目标**，不必先把文件生成出来；
- 生成变成幂等的"文件在就跳过"，谁生成的、什么时候生成的、跑的是全量还是子集，都不影响结果；
- §7.5 里"存在才挂链接"那条防御性设计对 detail 这条边**可以取消**——链接永远可算、永远可补。

**这三件套的杠杆效应**：都不改任何 import 边界（两侧都已依赖 `ctxgraph`），但让下面这些立刻成为
一次 JSON join —— journey 脊柱的每个 Step → `details/<file>.md`；report 的 session 行 →
`journey-<id>.md`；外部脚本把 `vmr-report.json` 的一条 Finding 对上某条 Journey 的某个 Step。
这是"两半区、一个契约"原则的自然延伸：**契约从"audit 记录"扩展到"派生索引"**，两个包依然互不
import，只是各自发布了对方能读的数据。

**行业对照**：Langfuse（session → trace → observation）、LangSmith（thread → trace → run）、
OpenTelemetry（metrics / traces / logs 三种信号 + exemplar 把指标数据点关联回 trace 上下文）
三者的共同点不是"只有一个视图"，而是**每一层只有一个稳定 ID，所有视图都用它互相跳转**；
它们的仪表盘与 trace 视图从来是同一批对象的不同视图。OTel 并不**要求**三种信号合并，它把力气
花在定义它们之间的关联键上——这正是本设计的立场。vmr 的差异化（零埋点、字节忠实、单二进制、
文件产物）不需要放弃这条共性，它需要的只是把已经存在于内存里的主键发布出去。

### 7.4 中观层的两次修补：脊柱补完 + 事件流归位

**(a) 脊柱补完**（删 fact-layer 的硬前提）

`renderDecisionSpine` 今天只收 `len(s.ToolCalls) > 0` 的 Step，且 `if !anyCalls { return }`。三条补齐：

| 补什么 | 形态 | 为什么 |
| --- | --- | --- |
| **用户在任务中途追加的指令** | 该 Step 块首一行 `💬 指令 · <原话>`，复用 `foldWhyLine` 折叠惯例 | 今天完全不在脊柱里；它是因果链上最重要的外部输入 |
| **无 tool_call 的 Step** | 单行进脊柱（`💬 汇报 · RespText 预览`），不再整段消失 | 实测样例 22 步缺 1 步，缺的就是这一类 |
| **最终交付物** | Journey 末尾一节，复用 compare 已验证的 deliverable 检测（参数形状像文件写入的最后一次调用 + 内容节选） | 实测缺失的 Step 22 正是交付物那一步——整条 Journey 信息价值最高的一段 |

顺带把"整条 Journey 无工具调用就不渲染脊柱"改为渲染单行块——纯问答 Journey 也应该有脊柱。

**(b) 事件流归位：补结构，不搬正文**

`journey-<id>.json` 补上今天完全缺失的那一层：Task / Step / Event / ToolCall / ToolResult 的
**完整结构**——顺序、全局去重后的首次出现位置、工具调用与结果的配对关系、每个 Step 的
`req` 坐标与每条消息的 `ctxgraph` 内容哈希。

**但不把消息正文抄进去。** 这是本方案里最容易走偏的一步：把 204KB 的事件流从 Markdown"搬"到
JSON，看起来是归位，实际上只是换个文件重复一遍——同一批正文在 `details/` 和审计日志里已经各有
一份。项目自己的第一性原理（blob 只存一份，tree 只持引用）在这里给出的答案很明确：
**journey JSON 是 tree，正文是 blob，tree 只该持引用。**

正文的取用路径因此是：`journey-<id>.json` 的某个 Step → `req` 坐标 → 审计日志原记录
（机读）或 `details/<req>.md`（人读）。对 LLM 消费者，`-llm-addr` 的证据包本来就在 Go 侧构造，
按需内联它真正要看的那几段即可——这比让它读一份把所有正文都塞进去的巨型 JSON 更省 token，
也更可控。

保留在 JSON 里的**短文本**是例外且有理由：Step 的 `RespText`/`Reasoning` 摘要、tool_call 的
参数——它们是"决策"本身而不是"上下文"，体量小、且是脊柱与 Findings 的直接证据，内联比引用更实用。
边界就一条：**属于对话历史的内容走引用，属于本轮决策的内容可内联。**

**这条边界必须有可执行守卫，不能只是文档里的一句规矩。** 一条没有自动检查的纪律，会被后续迭代
一次加一个字段地侵蚀掉，最终退回"把事实层搬进 JSON"——那正是这一步要消除的形态。守卫的形式与
`archtest` 同源：断言该产物的体积随**步数**增长而不随**对话正文长度**增长。这不是一次性的验收项，
是常驻检查。

这一步**必须先于**删 fact-layer：今天 fact-layer 是这层结构在派生产物里的唯一载体，先删后补 =
中间存在一个真正丢结构的版本。

补完之后，人读层的 journey 报告结构收敛为：

```
头部元信息 + System Prompt（链接，见 7.6）
概览卡 / 模型使用
决策脊柱          ← 唯一的主体，每步：why + 工具调用(折叠) + 工具结果(折叠) + → detail 链接
最终交付物
Findings
（可选）LLM 解读
```

预期体积：298KB → **< 50KB**（脊柱现为 42.8KB，加链接与三条补齐）。

> **"< 50KB"这个数字作废（2026-08-21 全面复查订正）**。它是设计期的估算偏差，不是一项未完成的
> 待办——按"脊柱 42.8KB 基本不变"推算，没有把"每步都要展示工具参数与结果"算进去。留着它只会让
> 下一个读者以为还有工作没做。
>
> **当前实测区间（2026-08-21，单日真实语料 6 条 journey）**：107KB / 132KB / 171KB / 192KB /
> 384KB / **609KB**。（P6 复盘当时记的是 86KB–506KB，P9 之后上界抬高了。）
>
> **结构目标达成**：fact-layer 已删、脊柱挂链接、系统提示词走证据条目。体积的构成也确实符合
> §7.4(b) 的纪律——75%–86% 的字节是脊柱里折叠的工具调用参数与工具结果，每项 `spineFullCap = 3000`
> 字符封顶，所以体积随**步数×调用数**增长而不随对话正文长度增长。**纪律成立，只是估算错了系数。**
>
> 唯一剩余的减法是把工具结果也降级为引用（P4 已经在机读层做了这个判断：`ToolCallRef` 不带结果
> 正文，因为结果本来就作为下一 Step 的 `NewEvents` 存在）——人读层是否跟进是一个独立的可读性
> vs 体积权衡，留给报告体积真正成为痛点时再决定（`story_report_dev_plan_2_sonnet-5.md` §5 已登记）。

**fact-layer 的处置：删渲染，不留开关。** 有人主张保留一个默认关闭的 `full_transcript` 开关作为
逃生门，理由是"删掉渲染代码省不了多少维护成本（golden test 已锁住），删掉选项则损失一种阅读模式"。
不采纳，三条理由：golden 基线本身就是维护成本，一个默认关闭的渲染分支意味着两条渲染路径与两套
基线；用户原话是"完全不必要再重新把它输出一遍"；fact-layer 的独有价值（全局去重后的事件流）
**正确载体是机读层的 JSON**，用 Markdown 开关承载它是用最贵的载体装最适合机读的内容。
如果日后真的出现"我要一份人读的完整转录"的需求，它的正确形态是一个**独立产物**
（`journey-<id>-transcript.md`，按需生成），不是主报告上的一个 flag。

### 7.5 导航矩阵

变焦要连续，链接就必须闭环。目标状态（✅ 已存在 / ➕ 新建）：

| 起点 | 终点 | 状态 | 依赖 |
| --- | --- | --- | --- |
| `vmr-report.md` | `vmr-requests.md` / per-client sibling | ✅ | — |
| `vmr-report.md` | `stories/vmr-stories.md` | ➕ **最大的一块新建**（今天命中数为 0） | 存在性判断 |
| `vmr-report.md` §8 | `details/` | ⚠️ 今天只有纯文本提示，改为链接 | — |
| `vmr-requests-<tag>.md` | `details/*.md` | ✅（实测 19 处） | — |
| `vmr-requests-<tag>.md` 会话行 | `journey-<id>.md` | ➕ 行级 | 会话级内容寻址 ID（7.3b） |
| `vmr-stories.md` | `journey-*.md` | ✅（渲染过才出现） | — |
| **journey 脊柱每个 Step** | `details/<req>.md` | ➕ | 7.3a + 7.3c |
| journey 报告头 | `vmr-stories.md` / `vmr-report.md` | ➕ 返回链接 | 存在性判断 |
| journey 头部 System Prompt | `evidence/sysprompt-<h8>.md` | ➕ | 7.6b |
| `details/<req>.md` 头部 | `vmr-requests.md`（索引） | ➕ 自我定位 + 返回 | 7.3a |
| `compare-*.md` 开篇 | `journey-*.md` | ✅ 含自动补生成 | — |
| `compare-*.md` 分叉点 | 双侧 `details/<req>.md` | ➕（低优先） | 7.3a + 7.3c |

**两类边，两种策略**——这个区分很重要，早先版本用一条"只链接实际存在的文件"把它们混在一起，
是把防御性设计套在了不需要防御的地方：

- **指向内容寻址产物的边**（脊柱 → detail、journey 头 → sysprompt blob）：目标文件名可由 Manifest
  直接算出（§7.3c），生成幂等。规则是**"渲染即生成"——谁渲染链接，谁在同一时刻负责生成目标**，
  因此永远渲染链接，不需要 `stat`，不可能 404。
  **推论要说清楚**：这条规则的成本与"被渲染的链接条数"成正比。任务报告的脊柱一次几十步，可以
  永远挂链接；而请求索引列的是**全部请求**，若每行都挂链接就等于全量物化详单——那正是 §7.6(c)
  要消除的东西。所以**请求索引在默认（不生成详单）模式下渲染坐标而非链接**，显式开启全量生成时
  才渲染为链接。两种呈现都不会产生失效链接。
  > **落地现状注记（P6 复盘，2026-08-20）**：**这一条没有实现**。`internal/report` 的
  > `detailLink` 无条件渲染 `details/<file>.md` 链接（其 doc comment 明确接受"文件要等到某次
  > `-details` 运行才存在"），于是默认的 `vmr report` 产出里 `vmr-requests.md` 及其分组 sibling、
  > `vmr-requests-failed.md` 的详单链接**全部失效**（单日样本实测 322/322）。这同时使 DevPlan
  > P6.2 的验收标准"不存在失效链接"在默认路径上不成立。修法就是本段原文——默认模式渲染 `req`
  > 坐标（今天 `RequestRow.Req` 已经有值，只是没有出现在 Markdown 表的任何一列里）。
  >
  > **补注（2026-08-21 全面复查）**：P7.1 按上面这段修好了，但**判据换成了 `detailsOn` 这个
  > flag，而不是本段原文的"目标产物是否存在"**。用 flag 近似存在性，在两个半区各自决定物化范围
  > 之后就不再成立：`vmr analyze` 默认路径下 story 半区已经物化了 306 份详单，而 report 半区的
  > `detailsOn` 仍是 false，于是 §8 渲染出 "This run did not write `details/*.md`"（假话），
  > 请求索引也把 306 份已存在的详单渲染成坐标而非链接（可链未链）。不产生死链接，但产生了一句
  > 假陈述和一次可用性损失。见 `KNOWN_ISSUES §1.35`——若那一条按"批量模式不物化"修，
  > 默认路径下确实一份详单都没有，这处口径错误会自动消失。
- **指向另一条命令聚合产物的边**（`vmr-report.md` → `stories/vmr-stories.md`）：目标是另一条命令
  的分析结论，本命令没有资格也没有必要去算它。**渲染时 `stat` 一次，存在才链接**，
  并**一并带出目标产物自述的生成时间与覆盖窗口**——跨命令的边指向的是另一次运行的产物，
  两次运行的输入范围可能不同，只校验存在性会产生"链接有效但窗口对不上"的误导。让链接自己说明
  来源，比引入版本协商便宜得多。

**一条必须显式写明的前提**：跨产物导航假定两条命令写到**同一个输出根**。这在默认路径下天然成立
（两者的输出目录解析逐字相同，且读同一个配置键），只有显式给两条命令传不同的输出目录才会破。
作为导航的地基，它不该是一条隐性契约。

**行级 join 发生在 `cmd/vmr` 组合根**：`cmd_report.go` 读到 `stories/vmr-stories.json` 存在时，
把一份轻量的 `{lineage ID → journey 文件名}` 映射（§7.3b 让这一步成为集合判断而非哈希对表）
作为普通数据结构传给渲染层。**两个 internal 包仍然互不知道对方存在。**

一条明确的反向裁决：**不让 `vmr report` 去生成 `vmr-stories.*`**。那会让 report 承担 story 的候选
Journey 计算（`ListCandidates` + `PreviewTitles` + 缝合链解析），扩大而不是缩小耦合面。导航靠
存在性判断即可。

### 7.6 共享证据层

**(a) detail：先做减法，然后它自然是一片叶子**

有一种主张是把 detail 渲染直接提取成 `report`/`story` 之下的共享叶子包，理由是"detail 本来就是
单条 `audit.Record` 的纯函数渲染，无 report 侧聚合状态"。**这个理由不成立，但结论成立**——
区别在于：**不是它本来就纯，而是我们要先把它改纯。** 照那个理由直接搬，搬过去的会是一个仍然
拖着 `report.ReqInfo` 的包。

先把 `renderDetail(rec *audit.Record, info *ReqInfo, lang)` 用到的东西按性质分清楚：

| 内容 | 性质 | 处置 |
| --- | --- | --- |
| `SessionID` / `TaskID` / `TaskSeq` / `SessSeq`（`detail.go:493-494`） | **视图层的位置命名**，run-scoped | **砍掉**。叶子不需要知道自己在树上的位置 |
| `Compaction` / `Summarizes` / `ContinuesTo`（`detail.go:484-490`） | report 的 §6.7 专属分析结论（跨记录文本匹配） | **砍掉**。那是宏观层的结论，不是这条记录的事实 |
| `Parent.DetailFile`（上一轮链接，`detail.go:496`） | 关系 | **保留，改由 `ctxgraph` 同 lineage 的前驱给出** |
| `DeltaStart`（🆕 增量高亮，`detail.go:593-605`） | 关系 | **保留，同上**——它来自 `ctxgraph.Classify`，两个半区算出来必然相同 |
| `RoleTokens`（`detail.go:578-590`） | **per-record 纯计算** | 保留，直接算。代码里本来就有纯函数回退路径 `roleTok = roleTokens(req.Body)`，`ReqInfo` 那条只是复用优化 |
| `ToolCalls` / `TraceID` / `ChatID` / `ToolsSig` / `Truncated` / `NoReply`（`detail.go:500-525`） | **per-record 提取**（`collect()`） | 保留，直接算（`NoReply` 需要 `taskseg.Profile`，本身就是叶子） |
| `Usage` / `UsageOK`（`detail.go:400`） | per-record | 保留，直接算 |
| `DetailFile`（自己的文件名） | 批次计数器 | **砍掉**，改为 §7.3(c) 的坐标哈希 |

> 一处自我更正：本方案的早期版本把 `RoleTokens` 与 `collect()` 那批字段一并列为"跨记录依赖"，
> 从而把跨记录面说得比实际大。`detail.go:578-590` 的注释写得很清楚——那只是复用已算好的结果，
> 并且已有纯函数回退。真正的跨记录面只有上表前四行，而且其中两行是该砍的、两行来自 `ctxgraph`。

减法做完，签名变成：

```go
// 两个入参都是共享层类型；不再有 report 的影子
func Render(rec *audit.Record, prev *ctxgraph.Manifest, lang i18n.Lang) (md string)
```

`prev` 定义为**同一条 `ctxgraph.Lineage` 内的前一个 Manifest**（不是 story 缝合链上的前驱）——
这样两个半区对"上一轮是谁"给出的答案严格一致；缝合边界的处理留在中观层，那本来就是中观层的语义。

于是它成为一个真正的叶子包，依赖面 `{audit, core, chatmsg, ctxgraph, taskseg, i18n, fmtutil}`
全是既有叶子，`report` 与 `story` 都 import 它，**archtest 无需改动一行**（禁止的只有两个消费方
互相引用）。顺带缓解 `KNOWN_ISSUES §1.5`（`detail.go` 1047/1150 逼近行数预算）。

**这一刀砍掉的连锁复杂度，比这一刀本身值钱**：

1. 子集生成 == 全量生成，**逐字节相同**。跨半区协调问题从根上消失。
2. 生成变成幂等写盘（文件在就跳过），不再有"哪个批次生成的""要不要重生成"。
3. §7.3(b) 的会话内容寻址从"正确性前提"降级为"导航能力"，迁移路径上少一条硬依赖。
4. 不再需要 `cmd/vmr` 去跑一遍 `report.AnalyzeSessions` 才能生成 detail——story 只要有 Manifest
   和记录就能自己渲染。组合根从"必经之路"退回"可选的编排位置"。

**丢了什么，诚实说**：从 detail 文件里点不到"我属于 s01 的第 7 轮"了。这不是损失，是归位——
链接过来的那一行（requests 索引行、journey 脊柱行）本来就渲染着这个上下文，父视图知道树的形状，
叶子不必重复一遍。叶子自己保留的是一个**稳定的自我地址**（`req` 坐标）和一条回索引的链接，
这比一个 run-scoped 的 `s01` 有用得多。

**(b) System Prompt：内容寻址成共享 artifact**

把 system prompt 挪到 journey 报告头部是对的，但它现在**每份报告存一份全文**：实测 179 条 lobster
journey 各内嵌同一份 ~20.5K token 的 prompt；一条 2 轮 heartbeat journey 有 68% 的字节是它。
`-render-all` 会产出数 MB 的重复。

**处置**：落成 `evidence/sysprompt-<h8>.md`（`h8` = 该 system 块的 `ctxgraph` 内容哈希，
天然去重、天然幂等），journey 报告头部只渲染"生效的 Step 区间 + 长度 + 链接"。

**放在 `evidence/` 而不是 `stories/`，是有意的**：它不是 story 的产物，它是一段被两个视图共同
引用的原始内容——report 的 §6 会话卡片、requests 索引同样可以链它。早先版本把它写成
`stories/sysprompt-*.md`，是因为"是 story 渲染它的"，那又是一次拿现状定归属。

再往前一步看：这不是一个 system-prompt 专属的机制，而是**证据层的通用命名规则**——
内容寻址的 blob，谁引用谁链接。项目里早就有这个先例：`session.go:512` 的
`toolsSig = fmt.Sprintf("tools:%d/%x", len(names), sum[:4])`——**工具声明集合早就是按内容哈希
引用的**，只是没有把 blob 本身落盘。所以 `evidence/` 下自然容纳三类条目，用同一条命名规则：

| 条目 | 地址 | 引用者 |
| --- | --- | --- |
| 单请求详单 | `details/<ts>_<virtual>_<real>_<outcome>_<h8(req)>.md` | requests 索引行、journey 脊柱 Step |
| system prompt | `evidence/sysprompt-<h8>.md` | journey 报告头、会话卡片（**设计预留，未实现**——P7 复核确认 `internal/report` 从不渲染指向 `evidence/*.md` 的链接，会话卡片本来就不展示提示词全文，大概率永远不需要做） |
| 工具声明集合 | `evidence/tools-<h8>.md`（`toolsSig` 已在算这个哈希） | detail 头部、report §5 工具形态章节（**设计预留，未实现**，同上一格） |

一句话：**同一份内容被 N 个视图引用时，它应该有一个地址，而不是有 N 份拷贝。** 这正是项目
自己在 `ctxgraph` 那一层已经贯彻的原则，只是从没延伸到派生产物层。

**(c) 证据层的体积纪律：默认按需，不默认全量**

实测：59 条记录的 `details/` 产物合计 60MB（`.json` 40MB + `.md` 20MB），而这 59 条记录的源日志
压缩后 334KB。按比例，一次全语料 `vmr report` 会写出约 **12GB**——而 `-details` 的默认值是 `true`。
这不是"存储便宜就无所谓"，它意味着一个常规命令会在用户的磁盘上写出比源数据大一个数量级的
派生副本，且其中大部分永远不会被打开。

四条处置，按收益排序：

1. **删掉 `details/*.json`，同时补上按坐标取记录的原语——两件事必须同批交付。**
   前半：它是 `json.MarshalIndent(audit.Record)` 的逐字复制（`detail.go:75`），因为美化缩进甚至
   比源记录还大 54%（709KB vs 461KB）；有了 `req` 坐标，"取这条记录的原文"是一次定位，不需要
   预先物化一份。
   **后半是前半的前提，不是可选项**：删掉副本就等于删掉了微观机读层唯一可寻址的载体，
   必须同时提供一个把坐标解析成记录原文、输出到标准输出的读取原语来接管它。
   **重放不能替代它**——重放是"重新发起这次请求"，读取是"取出这条记录"，两者回答的不是同一个
   问题；只补重放而不补读取，就是只拆不建。补齐之后，消费成本从"自己写解压加定位"降到"一条命令"。
   （顺带：重放/诊断入口也接受坐标，于是不必先跑任何分析命令就能对着审计日志工作。
   唯一的反向考虑是保留期删源文件时副本能当归档——但那应该是一次显式的导出动作，
   不是每次跑报表的默认副作用；也不保留"可选生成开关"，那会把"哪一份是权威"的问题永久留在体系里。）
2. **`-details` 默认改为按需**：`vmr report` 默认只写索引，detail 在被真正请求时生成——
   `vmr story -journey` 为它涉及的记录生成（渲染即生成，见 §7.5），或显式开启全量生成。
   幂等写盘让这件事没有任何协调成本。
   > **落地现状注记（P6 复盘，2026-08-20）**：这一条对 `vmr report` 成立（单日样本 322 条记录，
   > 默认产出 1.5MB），但 **P6.5 的 `vmr analyze` 把它抵消了**——`analyze` 强制给 story 侧加
   > `-render-all`，每条 journey 的每一步都触发 `EnsureJourneyDetails`，同一批日志实测产出
   > **164MB（其中 `details/` 160MB，306 份详单，无需 `-details`）**。也就是说"默认按需"这条
   > 纪律在新的推荐入口上退回成了"默认全量"，只是路径从 `-details=true` 换成了 `-render-all`。
   > `vmr analyze -details` 这个 flag 因此也是误导性的：不传它照样会写出 95% 的详单。
   > 这同时是 `KNOWN_ISSUES §1.30`（大语料 `-render-all` 被 SIGKILL）的直接成因，两者应一并处理。
3. **幂等产物用临时文件加改名写入。** 幂等性建立在"文件存在就跳过"之上，那么一次被中断的写入会
   留下一个永远不会被修复的半截文件，并被后续每一次运行当作已完成而跳过——这是静默错误，不是性能
   问题。项目里已有现成做法（额度状态的持久化就是临时文件 + 改名），沿用即可。
   **证据目录本身不做引用计数与垃圾回收**：它是完全可再生的派生物，随时可整目录删除重建；
   共享条目去重后的基数是个位数到几十（一个客户端一份系统提示词），为它引入生命周期管理是过度设计。
   写下这条是为了避免下一轮把它当成遗漏重提。
4. **解析缓存不再美化输出**（`storyindex.go:81` / `requests.go:58` 的 `json.MarshalIndent`），
   并按文件拆成 `.parse-cache/<filehash>.json`——一份机器只读、可随时重建的缓存不需要给人看的
   缩进，也不该每次运行都读改写一个 88MB 的单体文件（拆开之后，新增一个日志文件就只写一个新条目）。

5. **（2026-08-21 全面复查补入）同一把刀还要砍进每一份详单的内部。** 上面四条处置全都是关于
   **份数**的（删掉一份副本、默认不生成、缓存不美化），没有一条处置**每份的量纲**。这是本节的
   设计盲区：它把 `details/*.json` 当成了唯一的逐字复制，而 `.md` 里还有两处同性质的复制。
   实测（306 份详单 / 152MB）：

   | 组成 | 字节 | 占比 | 性质 |
   | --- | --- | --- | --- |
   | `Raw SSE, full` 折叠块 | 62.4MB | **41.1%** | `rec.Client.Response.Body` 的逐字复制 |
   | ① 段第一个 `🆕` 标记之前的历史消息 | 79.2MB | **52.1%** | 上一轮详单已经渲染过一遍的同一批消息 |
   | ① 段本轮真正新增的消息 | ~3.7MB | 2.4% | — |
   | 其余（②attempts、③重组输出、头部） | ~6.7MB | 4.4% | — |

   **约 93% 的详单体积是复制**，论证与本节第 1 条删 `.json` 的论证逐字同构。两条减法所需的机制
   都已经在仓库里，只是从没被用来做减法：
   - Raw SSE 块 → 一行带 `req` 坐标的取用提示（`vmr replay -req COORD -print`，P3.2 已交付）。
     重组后的模型输出/reasoning/tool_calls **保留**——那是解读，不是复制。
   - ① 段的历史消息 → `renderSessionHeader` **已经在渲染**的 `PrevTurnLink`。只逐条渲染
     `deltaStart` 之后的消息；`prev == nil`（lineage 首条、缝合边界）仍全文渲染，链条有起点。

   代价诚实说：修法 2 让"单份详单自包含"变成"要顺链回溯"。`Manifest.SysHash` 走 evidence 引用
   的先例（本节 §7.6b）已经证明这种取舍在证据层里可接受。必须守住的不变量是 P2 那条：
   两条生成路径对同一条记录产出的详单仍须逐字节相同。
   登记为 `KNOWN_ISSUES §1.36`。

### 7.7 索引的可用性：先解决噪声，"一键下钻"才成立

导航矩阵把 `vmr-stories.md` 变成了宏观→中观的落地页，那它就必须可用。实测窗口的 220 条候选
Journey 里：

- **120 条（55%）是 ≤2 轮的 heartbeat poll**，合计只覆盖 240 / 2672 = **9%** 的请求；
- 真正的任务型 Journey（含 `Daily News Brief` 16 轮、`Daily Finance Brief` 21 轮这类 cron 任务链）
  是少数派。

一个一半以上是噪声的列表，作为下钻落地页是不可用的。

**处置**：索引行增加**类别列**，判据用已有的结构信号，不引入新的猜测——轮数，加上 `taskseg`
已经能识别的标题前缀（`[OpenClaw heartbeat poll]` / `cron:` / `[Subagent Context]`）。
类别取 `task` / `cron` / `heartbeat` / `subagent`，`vmr-stories.md` 默认折叠噪声类，
JSON 侧照常全量输出（机读层不做取舍）。

### 7.8 索引与缓存分家

`vmr-stories.json` 今天是一个语义自相矛盾的文件：`journeys` 段是 run-scoped 的
（`MergeJourneyIndexRows` 丢弃"本次输入文件无法证明"的旧行），`files` 段是永久累积的——
结果是**一个 88MB 的"索引"，其中索引本身只有 3 行**。

| 文件 | 语义 | 形态 |
| --- | --- | --- |
| `reports/stories/vmr-stories.json` | 纯索引（`journeys[]`，run-scoped，含 §7.7 的类别列与 §7.3b 的 lineage 成员） | KB 级，缩进保留（人也会看），可随手 `cat` |
| `reports/.parse-cache/<filehash>.json` | 单文件的解析结果条目：manifest **加上每条记录的事实提取结果**，**`vmr report` 与 `vmr story` 共用同一个目录** | 紧凑编码、机器专用、可整目录删除重建 |

三点都不需要任何边界改动——两条命令用的本来就是同一个 `ctxgraph.FileCache` 类型和同一个
`ScanCached`（设计文档 §3.4 已明确）：

- **共用**：今天是同一份数据在 `vmr-requests.json` 和 `vmr-stories.json` 里各存一份。
- **拆分到单文件条目**：缓存天然按文件内容哈希分片，拆开之后新增一个日志文件只写一个新条目，
  不必读改写一个 88MB 的单体；淘汰旧条目也变成删文件。
- **不再美化输出**：`storyindex.go:81` 与 `requests.go:58` 今天都用 `json.MarshalIndent` —— 给一份
  机器只读、随时可重建的缓存加缩进，是把"索引"和"缓存"混成一个文件之后顺带继承下来的习惯。
  索引保留缩进（人会看），缓存用紧凑编码。
- **载荷扩大到每条记录的全部事实**（不只是 manifest）：这是 §7.10 的落点——缓存只装了一半，
  正是 report 全量运行热缓存只快 14% 的原因。扩大之后，两条命令在稳态下都只需要读新增/变化的文件。
- **补一个 schema 版本戳**：今天的命中判据只有文件内容哈希，**改了提取逻辑旧缓存会被静默复用**。
  这在只缓存 manifest 时已是潜在缺陷；载荷扩大后风险面同步扩大，必须同批补上。
  版本戳变化即整体失效重建——缓存是纯派生物，重建的代价是可接受的，静默用错才不可接受。

顺带修掉一个实测缺陷：缓存以路径字符串为 key，同一文件的绝对路径与相对路径各存一份
（实测 255 条 manifest 重复）。key 规范化成 §7.3(a) 的 basename 同时解决它。

### 7.9 命令层：一个分析动词，一个读取原语

**这一节也推翻了本方案早先的结论**，理由与 §7.10 同源：早先版本给出四条"不合并"的理由，
其中两条是不成立的推理，一条被新架构削弱，只有一条是真的软偏好。逐条复盘：

| 早先的理由 | 复盘 |
| --- | --- |
| "删除检验证明两组提问都不可删，合并只会把两组输出塞进一个更长的文档" | **不成立的推理**。删除检验回答的是"哪些**能力**不可删"，而合并**命令**不等于合并**文档**——同一条命令照样可以产出两份独立文档。这里把"一条命令"和"一份文档"混为一谈了 |
| "flag 空间是笛卡尔积" | **只对"扁平合并"这个稻草人成立**。用选择器或子命令分模式，flag 空间是并集而非乘积，各模式保留各自的默认值（包括"断头任务默认跳过"这类差异） |
| "扫描代价与运行频率天然不同，合并会让低频昂贵的一方拖累高频的一方" | **被 §7.10 削弱**。缓存载荷补齐后两者的稳态成本都是个位数秒；而且**一次进程内做完两件事反而更便宜**——今天分两次跑要各自加载一次缓存、各自重建一次图 |
| "两个动词对应两种心智" | 唯一站得住的一条，但它是软偏好。**而且它与本方案的中心比喻相冲突**：§7.1 刚论证过"变焦是连续的，不是三个割裂的产品"，转头又用"两种心智"论证要把它切成两条命令 |

**真正的问题不是"要不要合并"，而是一个每天都会碰到的papercut**：今天要得到一个**导航可用**的
套件，用户必须**记得跑两条命令、并且给它们同一个输出目录**——否则 §7.5 的跨产物链接一半是空的。
把"套件闭合"变成用户的责任，正是"统一套件"这个目标要消除的东西。

**目标形态**（形状是架构决策，具体拼写留给实施）：

| 层次 | 形态 | 说明 |
| --- | --- | --- |
| **一个分析动词** | 默认产出**可导航的完整套件**（宏观报表 + 请求索引 + 任务索引），一次扫描、一份缓存、一次建图 | 套件闭合由工具保证，不由用户记忆保证 |
| **变焦由选择器指定** | 单任务叙事 / 双任务对比 / 语料统计，都是这个动词上的选择器 | 直接映射 §7.2 的矩阵：一个入口，三个倍率 |
| **一个读取原语** | 按坐标取出一条记录的原文 | 补上 §7.6(c) 删掉逐请求副本后空出的那一格 |
| **统一的记录选择器** | 重放 / 读取共用同一个坐标形式 | 今天同一件事有三种拼法（行号、时间戳、详单文件路径），坐标把它们收敛成一种。**P6 落地时的一处订正**：`diagnose` 不参与——它探测的是实时连通性，从不选择一条历史记录，复用的是 `Adapter.BuildRequest`/`router.NewUpstreamClient` 这两个执行原语（`CLAUDE.md` 模块表已写明），不是一个记录选择器；本表原文把它归进来是不准确的 |

> **落地现状注记（P6 复盘，2026-08-20）**：P6.5 落地的是这张表的**一半**，且方向做了收窄，
> 三点如实记录：
> 1. **不是"收敛"，是"新增"**。`vmr analyze` 作为第三个动词加入，`vmr report`/`vmr story`
>    原样保留——命令数从 2 变成 3。P6 ActionPlan 论证过这个收窄（保留旧入口成本极低、删除不可逆），
>    但它同时写明"这是一个需要在实现前拍板的决策，建议落地前询问确认"，实际执行时没有走这一步。
>    ~~**这条至今仍是一个开放决策**~~ **已在 P9 结案（2026-08-21 复查确认）**：用户拍板"真正收敛
>    为一个入口"（`story_report_dev_plan_2_sonnet-5.md` §1），P9.1 落地 `vmr analyze` 为唯一分析
>    入口、P9.3 把 `report`/`story` 降级为打印迁移提示的过渡别名。**本表这一行现在成立。**
>    唯一的遗留是 P9.1 验收标准里的"收敛三套 flag 集合为一套"——实际是新增第四套（`analyze` 的
>    并集）+ 保留原两套，两个别名各自保留了完整的分派逻辑并已经出现分叉，登记为
>    `KNOWN_ISSUES §1.38`。
> 2. **"变焦由选择器指定"没有实现**。`vmr analyze` 没有 `-journey`/`-compare`/`-corpus`，
>    三个倍率仍然只能从 `vmr story` 进——"一个入口，三个倍率"目前只对"默认套件"这一格成立。
> 3. **"一次扫描、一份缓存、一次建图"没有实现**。`cmd_analyze.go` 是顺序调用
>    `cmdStory` 再 `cmdReport`（顺序是承重的，见其代码注释：story 先跑才能让"报表 → 任务索引"
>    这条边在**首次**调用就命中），两遍扫描，第二遍靠 P3 的分片缓存命中。这是被明确论证过的
>    取舍（收益有限、改动两个包的核心装配路径），不是遗漏——但本表"一次扫描…一次建图"的措辞
>    与它不符，以本注记为准。

**两个包仍然互不知道对方存在**——CLI 结构与包边界是正交的两件事，`cmd/vmr` 本来就是唯一允许
同时看到两个半区的组合根。合并命令不改变任何 import 边界。

**代价与取舍**：这是一次命令行表面的破坏性变更（"无历史包袱"的裁决适用，但仍要写进变更日志与
用户指南）。收益是套件闭合从"用户责任"变成"工具保证"、以及一次进程内共享缓存与图重建。
**这条排在缓存载荷补齐之后**——在那之前默认产出整个套件要付 84 秒，之后是个位数秒。

### 7.10 重复解析：把缓存扩到全部事实，而不是把遍数合并成一遍

**这一节推翻了本方案早先的结论。** 早先版本以"既有裁决已经定过、且实测无瓶颈"为由维持现状——
两条论据现在都不成立：前者是拿既定结论当不可挑战的前提（而那条裁决是在**旧架构**下做的），
后者的"实测"只跑了一个 16 文件的窗口，不足以代表全量语料。

**重新实测（全量 34 个审计文件、11374 条记录、压缩 177MB / 解压 9002MB）**：

| 命令 | 冷缓存 | 热缓存 | 缓存收益 |
| --- | --- | --- | --- |
| `vmr story`（候选列表） | 16.8s | **3.4s** | **5.0×** |
| `vmr report -details=false` | **83.8s** | **71.8s** | **1.17×** |

（`-details` 的默认值是 `true`，那条路径还要再叠加约 12GB 的渲染与写盘，见 §7.6c。）

**触发条件其实早就满足了。** 既有裁决写的是"真实 GB 级语料上确认 I/O 成为显著瓶颈才动"——
语料**已经是 9GB 解压级**，全量报表一次 72–84 秒，而且缓存对它几乎无效。没人重新量过，
包括本方案的早先版本，全都在引用那句裁决。

**诊断也要修正得更准确：瓶颈不是 I/O，是 JSON 解析。** `vmr story` 热缓存 3.4s 就跑完同样 9GB
的输入，证明"解压 + 跳过不需要的行"很便宜；贵的是**逐记录把完整请求体解成对象**。
两条命令的差距完全来自缓存覆盖率——story 唯一那一遍昂贵解析被缓存了，report 三遍里只缓存了一遍
（另外两遍每次全量重跑）。

**旧论据错在哪。** 既有裁决说"会话与任务分析依赖全量倒排索引，无法在单趟流式输入中就地确定单条
记录归属"——这句话是对的，**但它回答的不是"要不要把字节读三遍"这个问题**。分组依赖全量图，
只约束**阶段顺序**（先建图、再产出依赖分组的输出），不约束**读几遍字节**。

真正强制第三遍存在的，是另一个原因：详单渲染需要完整记录体，而记录体太大不能全量驻留内存
（平均 461KB × 11374 ≈ 5GB），所以只能"建完图之后再流式重扫一遍、边扫边渲染"。

**而新架构恰好把这个原因消掉了**：详单渲染现在是 `f(记录, 前驱 manifest)` 的纯函数（§7.6a），
不再依赖分组；`-details` 默认按需（§7.6c），默认路径根本不渲染详单。旧的约束条件不复存在，
在它之上做出的裁决自然也不该继续沿用。

**第一性原理推导**：

- 审计日志不可变、按天轮转 —— 昨天以前的文件**永远不会再变**；
- 每条记录的事实（用量、工具签名、角色 token、错误类、compaction 标记…）是**该记录的纯函数**；
- 分组是 manifest 集合的纯函数，**不需要字节**；
- 聚合是（事实、分组）的纯函数，**同样不需要字节**。

⇒ **稳态运行只需要读新增/变化的那一个文件。** 成本应当正比于"新增数据量"，而不是"历史总量"——
这才是一个增量索引该有的性质。今天做不到，仅仅因为缓存只装了 manifest 这一半。

**结论：改变裁决，但改变的方向不是"合并遍数"。**

1. **把每条记录的事实提取结果一并纳入共享解析缓存**，与 §7.8 的缓存重构同批完成——那次改动本来
   就要动缓存的存储形态，扩大载荷是边际成本（每条记录几百字节，相对已有的哈希向量可忽略）。
   稳态下 report 与 story 都退化为"只读今天这一个文件"。
2. **缓存必须补一个 schema 版本戳。** 今天的缓存条目只记文件内容哈希，命中判据里没有任何版本
   信息——**改了提取逻辑，旧缓存会被静默复用**。这在只缓存 manifest 时已经是潜在缺陷；把载荷
   扩大到全部事实之后，风险面同步扩大，必须一并补上。

**仍然不做"单遍流式扫描引擎"**——但拒绝它的理由要换掉：不是"既有裁决说不做"，而是
**重复解析的解药是缓存覆盖率，不是遍数合并**。合并遍数只能把冷启动的 84s 降到约 30s，
而扩大缓存能把稳态直接降到个位数秒；前者要重构报表的核心管线，后者是一次载荷扩展。
首次分析一份历史语料付一次 84s 是一次性成本，可以接受。

> **落地现状注记（P3 验收）**：在 P3 落地时发现 `report.Build` 实际存在三趟扫描（`ctxgraph.ScanCached`、`session.go` 的 `analyzeFile`/`collect` 会话切分、`ingest.go` 的端点/记录聚合）。P3.6 为 `ingest.go` 接入了 `RecordFacts` 缓存，全量语料热缓存耗时由 71.8s 降至 16.2s（收益 5.2×）；剩余差距来自未缓存的 `session.go` 遍，已登记为 `KNOWN_ISSUES §1.1/§1.23` 供后续排期。

### 7.11 迁移路径

> **落地状态说明**：本节初始设计的 8 个批次已在 `story_report_dev_plan_opus-5.md` 中映射为 **P0～P6 六个执行阶段**，并已于 commit `b098ca9`（P0）至 `2c45a58`（P6）全部完成落地与真实语料验证。下方表格与验收标准保留为设计期依赖论证与历史参照。

每批独立可上线，顺序有硬依赖。

| 批 | 内容 | 依赖 | 风险 |
| --- | --- | --- | --- |
| **0** | **提交工作区里那 6 项已验证的 UX 改动** | 无 | 无。文档描述的是工作区状态而非仓库状态，任何人 checkout 当前 HEAD 都得不到文档描述的行为——后续每一批都建立在这个不一致上 |
| **1** | `chatmsg` 配对加归一化回退；渲染层加位置兜底并标注（§5） | 无 | 低，收益立竿见影（0/33 → 33/33） |
| **2** | 脊柱补完三条（指令行 / 汇报行 / 交付物节）（§7.4a） | 无 | 低-中，golden 基线更新 |
| **3** | `req` 请求级坐标进两侧 JSON；`FileCache` key 规范化为 basename（§7.3a） | 无 | 低（纯新增 `omitempty` 字段） |
| **4** | **detail 做减法 → 纯叶子包**：砍掉会话坐标与 compaction 链接、`RoleTokens`/`collect()` 那批改为自算、`prev` 改由 ctxgraph 给；文件名改为坐标哈希（去批次 + 去时区 + 去 `-N`）（§7.6a、§7.3c） | 3 | 中（核心架构步；旧 detail 链接全部失效，无兼容包袱、重跑即得，写进 CHANGELOG 的 Changed） |
| **5** | 证据层瘦身：删 `details/*.json`、`-details` 默认改按需、`vmr replay -req <坐标>`、解析缓存拆分为单文件条目且紧凑编码（§7.6c、§7.8） | 4 | 中（`vmr replay -detail` 的用法变化，需同步 UserGuide 及其 `.zh` 兄弟） |
| **6** | `journey-<id>.json` 补**结构 + `req` 引用**（不搬正文）（§7.4b） | 3、4 | 低（新增字段） |
| **7** | 删 fact-layer；脊柱每步挂 detail 链接（**永远挂，缺了当场补生成**）；`vmr story -journey` 按需生成 detail（§7.4b、§7.6a） | **2 + 4 + 6** | 中（人读产物形态变化） |
| **8** | Lineage 内容寻址 ID（§7.3b）；导航矩阵 Tier 1 → Tier 2（§7.5）；索引类别列（§7.7）；system prompt / tools 声明落成 `evidence/` blob（§7.6b）；**CLI 收敛为一个分析动词 + 一个读取原语 + 统一记录选择器（§7.9）** | 3、4、缓存载荷补齐 | 中（会话 ID 语义与命令行表面同时变化，都要一次定稿） |

**四条硬依赖，都不是形式主义**：

- 批 7 依赖批 2 —— 不补脊柱就删 fact-layer，删掉的是最终交付物那一步。
- 批 7 依赖批 6 —— 今天 fact-layer 是事件流结构在派生产物里的唯一载体，先删后补 = 真丢结构。
- 批 6、7 依赖批 4 —— 引用要能解析、链接要能算出来，都以确定性命名为前提。
- 批 5 依赖批 4 —— 删 `details/*.json` 的前提是 `req` 坐标已经能顶替它的寻址职能。

**一条贯穿全部批次的演进原则：能力不降级——旧载体的下线与新访问路径的上线必须同批交付。**
批 5 之所以把"删副本"和"按坐标读取/重放"绑在同一批，就是这条原则的直接应用；任何试图把它们
拆到两批的排期，都会制造一个中间版本，在那个版本里微观机读层既没有副本也没有取用方式。

**批 8 不再是批 7 的前置**。早先版本把"会话内容寻址 ID"排在删 fact-layer 之前，理由是子集生成的
detail 正文会不一致——批 4 的减法让那个理由消失了，这条依赖随之解开，批 8 可以独立排期。

**验收标准**（每批都要有，且都跑 `go test ./...` + `archtest` + 真实日志重跑肉眼核对）：

- 批 1：07-28 的 openclaw/lobster 两条 Journey，脊柱工具结果配对率从 0% 升到 >90%，
  位置兜底命中的条目带来源标注。
- 批 2：样例 journey 的脊柱 Step 覆盖率 22/22；末尾出现交付物节；纯问答 Journey 也有脊柱。
- 批 3：`vmr report` 与 `vmr story` 分别跑同一批日志，用 `req` 字段 join 两份 JSON，命中率 100%；
  `basename:line` 在全部历史日志上无冲突（加断言）。
- 批 4：**同一条记录，分别经 `vmr report` 全量路径与 `vmr story` 单 Journey 路径生成的 detail，
  文件名与正文逐字节相同**；把 `TZ` 换一个值重跑，文件名不变；`internal/report` 与
  `internal/story` 都只 import 新叶子包，`archtest` 全绿。
- 批 5：全语料一次报表的派生产物体积从约 12GB 降到索引量级；按坐标取记录与按坐标重放，两者都在
  **没跑过任何分析命令**的干净环境下可用。
- 批 6：`journey-<id>.json` 的结构 + 引用，经一个解析脚本可无损重建出今天 fact-layer 的等价内容。
- 批 7：`journey-j-openclaw-*.md` < 50KB；每个脊柱 Step 的 detail 链接 `test -f` 全部为真。
- 批 8：从 `vmr-report.md` 出发，纯靠链接可以走到任意 journey、任意 detail，并从 journey 走回大盘；
  `vmr-stories.json` < 100KB；连跑两条命令只产生一份解析缓存目录。

---

### 7.12 面向未来：如果这套工具将来要变成一个 Web 服务

**本轮不实现任何与之相关的功能**，这一节的目的只有两个：把这个方向记在案，以及确认现在的设计
不会挡路、并守住几条零成本的纪律，免得将来要推倒重来。

可以预期的形态是：一个只读的本地/内网服务，对着同一批审计日志，提供大盘、任务列表、任务叙事、
单请求详情四类页面——也就是把 §7.2 那张 3×2 矩阵从"文件树 + 相对链接"换成"路由 + 响应"。

**当前设计天然对齐的部分**（这些不是巧合，是同一批第一性原理推出来的）：

| 本方案的决策 | 在服务形态下的对应物 |
| --- | --- |
| 请求级坐标 `basename:line`（§7.3a） | 天然就是一个资源路径 |
| 内容寻址的身份（lineage ID、详单名、evidence blob） | 稳定 URL 与可缓存的实体标签 |
| 人读层 / 机读层分离（§7.2） | UI 与 API 的分离——机读层就是 API 的返回体 |
| 证据层按需生成且幂等（§7.6） | 服务端惰性渲染 + 结果缓存，语义完全一致 |
| 解析缓存按文件分片、带版本戳（§7.8、§7.10） | 增量索引：只有当天的文件会变，正是服务需要的更新粒度 |
| 只读、离线消费审计日志，不碰路由运行时 | 一个只读服务天然是安全的，不需要为它设计隔离 |

**需要现在就守住的三条纪律**（成本都接近于零，只是不要走反方向）：

1. **地址由身份派生，文件路径只是身份的一种渲染。** 只要"给定身份 → 它的地址"是一处可替换的
   映射，把文件树换成路由就是替换这一层；反之，如果相对路径被硬编码在各个渲染点，将来就要逐处
   重写。§7.3(c) 已经把命名收敛到一处，保持住。
2. **人读层展示的每一项事实，机读层都必须能提供。** 这条已经有守卫了——§7.4 的"无损重建检验"
   正是它。守住它，将来的 Web 视图就是**同一份契约上的第二个渲染器**，而不是一次分叉实现；
   走反了，某些事实只存在于 Markdown 的渲染逻辑里，服务端就得把它重新实现一遍。
3. **脱敏是任何网络暴露的硬前置，不是可选扩展。** 产物默认 0600 是因为它们承载完整对话正文；
   这个理由在网络场景下只会更强，不会更弱。任何"把 reports 目录挂到 HTTP 上"的捷径都要先过
   这一关。这条写在这里，是为了让它在被需要之前就已经是一条明确的前置条件。

**明确不做的预备工作**：不引入 HTTP 层、不引入数据库、不为"将来可能的服务"提前定义抽象接口。
上面三条纪律之所以值得现在守，正是因为它们**不需要任何提前投入**——它们本来就是好设计的一部分，
只是顺带把将来的路留出来了。真到要做服务的那天，再基于当时的现实重新分析。

---

## 8. 决策与取舍

| 决策 | 备选 | 取舍逻辑 |
| --- | --- | --- |
| **收敛为一个分析动词（默认产出完整套件）+ 一个读取原语 + 统一的记录选择器** | 保留 `report`/`story` 两条命令 | 今天要得到一个导航可用的套件，用户必须记得跑两条命令并给同一个输出目录——套件闭合成了用户责任。早先"不合并"的四条理由里，两条是把"命令"与"文档"混为一谈的推理，一条被缓存载荷补齐削弱（一次进程内做完反而更便宜），只剩一条软偏好，而它还与 §7.1"变焦是连续的"这个中心比喻相冲突。CLI 结构与包边界正交，合并不改变任何 import 边界 |
| detail 提升为两者共享的证据层 | 保持为 report 的附属 / 收编进 story | 三个变焦倍率的下钻终点是同一个东西；今天挂在 report 下是历史顺序，不是逻辑归属 |
| **detail 先做减法（砍掉会话坐标与 compaction 链接），再下沉为共享叶子包** | 原样下沉 / 走 `cmd/vmr` 组合根编排 | 原样下沉搬过去的是一个仍拖着 `report.ReqInfo` 的包；组合根编排则是拿现状当约束。减法之后 `Render(rec, prevManifest, lang)` 是真正的纯函数，两半区生成的结果逐字节相同，跨半区协调问题从根上消失 |
| **叶子不记录自己在树上的位置** | detail 保留 `s01/t01/turn N` 上下文 | 父视图（requests 索引行、journey 脊柱行）本来就渲染着这个上下文；叶子重复一遍换来的是 run-scoped 污染。叶子保留的是稳定自我地址（`req`）+ 回索引链接 |
| 新增 `basename:line` 请求坐标，并让 detail 文件名 = 该坐标的哈希 | 让 story 直接 import report / 用请求体哈希 / 用 `Manifest.SysHash + Keys[0]` | 第一个违反 archtest；请求体哈希在重发同一 body 时仍会撞，于是要请回 `-N` 后缀、把批次顺序依赖带回来；`SysHash+Keys[0]` 在纯追加 Journey 内每轮相同，会把偶发碰撞变成必然碰撞。坐标是主键，天然唯一，`-N` 可彻底删除，且文件名只从 Manifest 就能算出 |
| detail 文件名去掉本机时区依赖 | 保持 `fmtutil.DisplayZone` | `DisplayZone` 默认 `time.Local`，同一条记录在不同机器上生成不同文件名；`deriveID` 已有"文件名时间用数据自身属性"的先例 |
| **journey JSON 补结构与引用，不搬正文** | 把 204KB 事件流从 Markdown 抄进 JSON | 抄过去只是换个文件重复一遍——同一批正文在审计日志里已有一份。项目自己的第一性原理是 blob 只存一份、tree 只持引用；journey JSON 是 tree。边界：属于对话历史的走引用，属于本轮决策的（RespText/Reasoning/tool_call 参数）可内联 |
| **删掉 `details/*.json`，微观机读层就用审计日志本身** | 保留逐请求 JSON 副本 | 它是 `json.MarshalIndent(audit.Record)` 的逐字复制（`detail.go:75`），因缩进比源记录还大 54%。有了 `req` 坐标，取原文是一次定位而非一次物化。**注意重放本来就不依赖它**——`vmr replay` 今天已经支持「审计文件 + `-line N`」，那就是坐标的另一种拼法，`-detail` 只是免去数行号的便利选择器。所以删副本影响的不是重放能力，而是「取出这条记录」这个原语，那要单独补 |
| **`-details` 默认改为按需生成** | 保持默认 `true` 全量物化 / 保留一个可选的副本开关 | 实测 59 条记录 → 60MB，全语料约 12GB（源日志 645MB）。文件名可算 + 生成幂等，让"按需"没有任何协调成本。保留可选副本会把"哪一份是权威"的问题永久留在体系里——那正是删掉它的首要理由 |
| 共享条目不做引用计数与垃圾回收 | 为证据层引入生命周期管理 | 去重后基数是个位数到几十（一个客户端一份系统提示词），且整个证据目录完全可再生、随时可删除重建。为几十个文件引入 GC 是过度设计 |
| **不下沉 Finding 检测器到共享层** | 把两侧的判定规则合并共享 | 两侧的 Finding 集合**零重叠**（中观 13 个全是 Agent 行为判定，宏观 7 个全是资源浪费判定），不存在"同一事实两种判定"。真正共享的分析（会话/任务切分、消息解析、内容寻址）早已收敛；跨半区复现同一个数字另有更强的纪律——差分测试钉死，而不是靠共享代码假定一致 |
| 面向未来的服务形态：只守纪律，不做预备工作 | 现在就抽出服务层接口 / 完全不考虑 | 三条纪律（地址由身份派生、人读层不得独占事实、脱敏是网络暴露的硬前置）本来就是好设计的一部分，零额外成本；而提前定义"将来可能需要"的抽象，是在没有真实需求时猜形状。见 §7.12 |
| 不统一全链路哈希函数 | 把 md5/sha256 统一成一种 | 跨模块关联靠**同一个键**，不是同一个哈希函数；内容身份与文件完整性校验服务于不同目的。需要写死的是"每个新地址用哪个口径"，不是"全部改成一种" |
| 解析缓存拆成单文件条目、紧凑编码 | 单体 JSON + `MarshalIndent` | 一份机器只读、随时可重建的缓存不需要给人看的缩进；拆开后新增日志文件只写一个新条目，不必读改写 88MB 单体 |
| **缓存载荷扩大到每条记录的全部事实，而不是合并扫描遍数** | 维持只缓存 manifest / 重构成单遍流式引擎 | 实测全量语料（9GB 解压）：report 热缓存只快 1.17×，story 快 5.0×，差距全在缓存覆盖率。扩大载荷让稳态成本正比于新增数据量而非历史总量（个位数秒）；合并遍数只能把冷启动 84s 降到约 30s，却要重构报表核心管线。**冷启动是一次性成本，稳态才是每天都付的** |
| **缓存补 schema 版本戳** | 只按文件内容哈希判定命中 | 改了提取逻辑旧缓存会被静默复用——载荷扩大后这个风险面同步扩大。缓存是纯派生物，整体失效重建可接受，静默用错不可接受 |
| **删 fact-layer 前先补脊柱完整性** | 直接删 | 脊柱只渲染有 tool_call 的 Step，实测样例缺的正是最终交付物那一步 |
| **删 fact-layer 渲染，不保留 `full_transcript` 开关** | 改默认值 + 留开关 | golden 基线本身就是维护成本，默认关闭的分支是 YAGNI 债；fact-layer 的独有价值（全局去重事件流）的正确载体是 JSON。真需要人读全文时，正确形态是独立产物而非主报告的 flag |
| 工具配对：精确 + 归一化可作 Finding 证据，只有位置配对限渲染层并标注 | 三级全部限渲染层 / 全部接受 | 归一化后仍是精确一一匹配，不含顺序假设；只有位置配对是推断。实测归一化 0%→100% 零误合并，位置同序 152,967 组 100% |
| 索引与解析缓存分家、两命令共用缓存 | 保持现状 | 现状是同一文件里 run-scoped 与永久累积两种语义并存，产出 88MB 的"索引"，且同一份数据存了两份 |
| 索引增加类别列并默认折叠噪声 | 保持全量平铺 | 55% 的候选是 ≤2 轮 heartbeat、只覆盖 9% 请求；作为下钻落地页不可用。判据全部来自已有结构信号，不引入新猜测 |
| System Prompt / 工具声明落成 `evidence/` 下的内容寻址 blob | 每份 journey 内嵌全文 / 放在 `stories/` 下 | 179 份报告各存一份同样的 ~20.5K token 全文；同一份内容被 N 个视图引用时应该有地址而不是 N 份拷贝。放 `stories/` 是拿"谁渲染它"定归属——它是两个视图共同引用的原始内容。`toolsSig` 早就在按内容哈希引用工具集，只是没把 blob 落盘 |
| **两类链接两种策略**：指向内容寻址产物的边永远渲染并按需补生成；指向另一条命令聚合产物的边才 `stat` | 一律"存在才链接" | 前者的文件名可算、生成幂等，不可能 404，加 `stat` 是把防御性设计套在不需要防御的地方；后者是另一条命令的分析结论，本命令没资格去算 |
| report 不生成 stories 索引 | 让 report 也生成 `vmr-stories.*` | 那会让 report 承担 story 的候选计算（`ListCandidates` + 缝合链解析），扩大耦合面；存在性判断零耦合、零协调状态 |
| Lineage 内容寻址 ID：report 的 Session 与 story 的 Journey 成员共用它 | 两边各算一个哈希再对表 / 保持 `s01` | report 的一个 Session 本来就**是**一条 Lineage、Journey 本来就**是**若干条 Lineage 的链；给 Lineage 本身一个 ID，join 变成集合判断而非查表。`s01` 退化为报告内的显示别名，不再承担 identity 职责 |
| LLM 解读标题层级由渲染层兜底 | 只靠 prompt 指令 | 文档结构是结构，不该外包给 LLM 的指令遵从度；prompt 保留为第一道，渲染兜底为第二道（需跳过围栏内的 `#`） |
| compare 首条 User Message 进 JSON（喂 LLM） | 只在 Markdown 渲染层取数 | 分叉点分析质量直接取决于 LLM 是否理解原始指令；今天它只拿到 80 rune 截断的标题 |

### 明确不做

- **不引入数据库或服务端**。Strategy 文档的定位是单二进制、无数据库、零埋点；本方案全部产物
  仍是文件，"像一个 store 那样行为"靠的是稳定地址，不是存储引擎。
- **不为将来可能的 Web 服务预先抽象接口**（§7.12）——只守住三条零成本的纪律，不引入 HTTP 层、
  不引入数据库、不提前定义"将来可能需要"的抽象。
- **不让 `story` import `report`**（两者共同 import 新的 detail 叶子包，不互相引用；仍需要
  跨半区编排时才落到 `cmd/vmr` 组合根）。
- **不为 detail 做全量预生成**。减法之后全语料是 11374 个 `.md`（`.json` 副本已删），但仍然只在
  被真正请求时生成——幂等写盘让"按需"零成本。
- **不把派生产物当归档层**。`details/` 不承担"审计日志被 retention 删除后仍能追溯"的职责；
  真需要归档是一次显式导出动作，不是每次跑报表的默认副作用。
- **不做单遍流式扫描引擎**。重复解析是真的、代价也是真的（全量 report 一次 72–84 秒），但解药是
  **缓存覆盖率**而不是**遍数合并**：扩大缓存载荷把稳态降到个位数秒，合并遍数只能把一次性的冷启动
  从 84s 降到约 30s，代价却是重构报表核心管线。见 §7.10——那一节同时推翻了本方案早先"维持既有
  裁决"的处理方式。
- **不把 JSON 语言策略绑进本方案的依赖链**。它是独立议题，已有专门方案文档
  （`json_lang_policy_plan_sonnet-5.md`）；与本方案唯一的交集是"反正要动 journey JSON"，
  可以并行做，但不该让任一方受阻时卡住另一方。
- **不动已校准的判据**（九个 Finding 检测器、`contractLenRatio`/`forkCoverage`）。
  未校准的 `stitch` 两个阈值另有 `KNOWN_ISSUES` 条目，不在本方案范围。
- **不做 HTML/脱敏渲染**。设计文档 §8 已列为可选扩展；3×2 矩阵为它留了位置（人读那一行换一个
  渲染器即可），但不提前实现。

---

## 9. 风险与开放问题

1. **自指流量污染统计。** ✅ **已在 P6.4 解决**——`vmr story -llm-addr` 的解读调用经 VMR 路由后
   回流进审计日志：实测 `client_key_tag = vmrstory` 共 21 条记录 / 4 条 journey。它不只污染
   corpus 统计——也污染 `vmr report`：那些 token 与成本是**分析行为的开销，不是被分析工作负载的
   开销**，混进一份以"这段时间花了多少钱"为首要问题的报表里是口径错误。识别规则最终**只定义
   一处**（`cmd/vmr/selftraffic.go`，基于 `report.yaml` 的 `llm_key` 派生 `audit.KeyTag`，与
   `api_keys` 认证走同一个变换），`report`/`story` 两侧共用同一次计算结果，**默认排除**、
   `-include-self-traffic` 可显式包含。真实语料验证：08-05 那份日志的 16 条自指记录被正确排除，
   `vmr-report.json` 的 `meta.self_traffic_excluded` 如实反映排除数量。
2. **detail 命名与形态切换的过渡。** 批 4 生效后旧 `reports/` 里的 detail 链接全部失效；
   批 5 删掉 `details/*.json` 会让 `vmr replay -detail <file>.json` 这个**选择器**失去目标——
   但重放能力本身不受影响：`vmr replay` 今天已经支持「审计文件 + `-line N`」，那就是坐标的另一种
   拼法。批 5 要做的是把三种拼法（行号 / 时间戳 / 详单路径）收敛成统一的坐标形式。
   无外部消费者，重跑即可，但要写进 `CHANGELOG.md` 的 Changed，并同步
   `docs/UserGuide.md` 及其 `.zh` 兄弟里的 replay 示例。
3. **位置兜底的残余风险。** 本语料 152,967 组 100% 同序，但"响应流顺序 → 客户端回写顺序"未单独
   验证。标注"按位置推断"已让风险对读者可见；若真实语料观测到错配，回退为整组呈现（不声称对应关系）。
4. **会话 ID 语义变更一次定稿。** ✅ **已在 P6.1 解决**——`s%02d` → 内容寻址（`l-<hash8>`，
   `ctxgraph.Lineage.LineageID()`，与 `story` 的 Journey id 同一套 `RootHash` 前 8 位十六进制
   口径）改变了 `vmr-report.json`'s `sessions[].id` 的既有字段语义（登记为 `CHANGELOG.md` 的
   Breaking 变更）。位置编号保留为 `SessionRow.alias`（`s01`/`s02`……）供人读场景对照，不再承担
   身份职责，一次定稿，未见需要回退的理由。
5. **`basename:line` 的唯一性假设。** 审计文件名带日期，理论上跨目录同名才会冲突。落地时加断言，
   不靠假设。
6. **命令行收敛是一次破坏性变更。** "无历史包袱"的裁决适用，但它改变的是**肌肉记忆**——
   变更日志、用户指南及其中文兄弟都要同步，且应在一次变更里定稿，不要分两轮改两次命令行。
   它还有一条排期约束：**必须排在缓存载荷补齐之后**，否则"默认产出完整套件"要付掉全量扫描的代价。
7. **行业参照的效力边界。** Langfuse / LangSmith / OTel 的三层模型是**同构印证**，不是权威指令；
   真正的裁决依据仍是 §3 的删除检验与 §2.2 的覆盖率实测。引用它们是为了说明这个结构在行业里被
   独立收敛过多次。

---

## 10. 附录

### 10.1 验证方法

1. **源码**：通读 `internal/story/`（`candidates.go`/`journey.go`/`storyindex.go`/`compare.go`/
   `metrics.go`/`findings.go`/`render_md.go`/`render_spine*.go`/`llm.go`）、`internal/report/`
   （`detail.go`/`session.go`/`rows.go`/`requests.go`/`aggregate.go`）、`internal/ctxgraph/`
   （`scan.go`/`records.go`）、`cmd/vmr/cmd_story.go`/`cmd_report.go`、`internal/fmtutil/timezone*`、
   `internal/archtest/import_boundaries_test.go`，逐条对照真实产物找到生成它的确切位置。
2. **产物实测**：对本机 `reports/` 产物做体积/结构统计；用当前工作区代码构建二进制
   （`go build -o <scratchpad>/vmrbin ./cmd/vmr`）重新渲染样例 journey 与一条 heartbeat journey，
   按标题行切分统计三个区段的行数与字节、脊柱 Step 覆盖、工具结果行数。
   证据层体积用 `du -ch reports/details/*.{md,json}` 与
   `zstdcat <源日志> | wc -c` 对照（59 条记录：派生 60MB vs 源 27MB 解压 / 334KB 压缩）；
   `details/*.json` 的"逐字复制"用 `json.load` 逐字段比对其结构与 `audit.Record` 一致，
   并在 `detail.go:75` 找到 `json.MarshalIndent(j.rec, "", "  ")` 作为直接证据。
3. **覆盖率实测**：`vmrbin story -o <tmp> logs/vmr-audit-2026-07-2[56789]* logs/…-07-3* logs/…-08-0*`
   → 220 journeys / `sum(requests)`=2672；`zstdcat | wc -l`=2889 → 92.49%。
3a. **性能实测**（§7.10）：对全部 34 个审计文件分别跑 `vmr story`（列表）与
   `vmr report -details=false`，各测冷缓存与热缓存两次。语料规模用 `zstdcat | wc -c` 逐文件累加
   （压缩 177MB / 解压 9002MB）。**一处方法论教训**：首次测量把输出管到 `head`，SIGPIPE 在缓存
   落盘前杀掉了进程，导致"热缓存"其实还是冷的、看起来缓存零收益——改为重定向到文件后复测才拿到
   真实数字。凡是"某项优化看起来完全无效"的观测，先怀疑测量本身。
4. **全语料统计**：用 `vmr-stories.json` 的 11374 条 manifest（按 `(basename, line)` 去重后）
   统计 detail 文件名碰撞率——这是**近似**（键用 `(ts_ms, virtual, endpoint, outcome)`，真实实现用
   `realModel(rec)` 与 `outcome+errClass`），量级可靠，绝对数字有小幅误差。
5. **原始日志核查**（§5）：`zstdcat` 五个日志文件（07-16 / 07-19 / 07-28 / 08-14 / 08-16，
   合计 4159 条记录）管道进 Python，正则抓响应体 SSE 的 `"id":"call…"` 与请求体的
   `tool_calls[].id`/`tool_call_id`，按 `client_key_tag` 与 `endpoint` 两种口径做集合比较与形态
   统计；归一化误合并检查用"归一化前后唯一 id 数之差"对照"受影响 client 的 id 计数之和"；
   顺序安全性用"assistant(tool_calls) → 随后连续 role=tool 消息组的 id 序列逐位比较"，共 152,967 组。
6. **未做的验证**（诚实声明）：
   - 没有跑 `vmr report`（12s 量级的耗时数据引自他人实测，未复核）。
   - §5 的按 endpoint 分组统计有已知口径误差：一条记录的历史 id 被归到该记录**最后一次 attempt**
     的 endpoint 上，而这些 id 实际由更早的轮次产生。按 `client_key_tag` 分组的那张表不受此影响，
     §5.3 的结论以它为准。
   - 位置兜底验证的是"客户端在同一请求内保持同序"，"响应流顺序 → 客户端回写顺序"未单独验证。
   - 没有验证 `basename:line` 在全部历史日志上无冲突（见 §9.5）。

### 10.2 外部参考

- [Langfuse — Data Model（session / trace / observation 三层，session 为可选顶层）](https://langfuse.com/docs/observability/data-model)
- [Langfuse — Sessions（session 是共享 `session_id` 的 trace 的虚拟聚合）](https://langfuse.com/docs/observability/features/sessions)
- [Langfuse — What does a good trace look like?（"一次对话一个 session、一轮一个 trace"）](https://langfuse.com/docs/observability/best-practices)
- [LangChain — Debugging Deep Agents with LangSmith（thread = trace 集合，trace = run 树，从仪表盘下钻到单个 run）](https://www.langchain.com/blog/debugging-deep-agents-with-langsmith)
- [LangSmith — Query traces using the SDK（runs/query 是查询 span 数据的入口）](https://docs.langchain.com/langsmith/export-traces)
- [Uptrace — OpenTelemetry for AI Systems: LLM and Agent Observability (2026)（`gen_ai.conversation.id` 把 span 归入所属会话；Orchestration/LLM/Tool/Memory 四类 span 嵌套）](https://uptrace.dev/blog/opentelemetry-ai-systems)

对照结论：这三套系统的共同点不是"只有一个视图"，而是**每层只有一个稳定 ID、所有视图共用它互相
跳转**，且仪表盘与 trace 视图是同一批对象的不同视图。它们提供的是关联**机制**（如 OTel 的
exemplar 把指标数据点关联回 trace 上下文），不是"必须一键跳转"的合规要求——把机制说成要求会让
论据在被引用时失真。

### 10.3 本文与既有文档的关系

- 本文是重构方案的当前权威，取代 `story_report_comprehensive_redesign_gemini-3.7-flash.md` 与
  `story_report_suite_reorganization_glm-4.7.md`（两份均已归档，不在版本控制范围内）：两者的可取
  部分（三级变焦、确定性命名、导航矩阵、脊柱完整性、配对分层、Phase 0）已吸收进本文正文，被否决
  部分及其理由记录在逐条评审文档里。
- `story_report_ux_review_sonnet-5.md`（已归档，不在版本控制范围内）是第一轮 6 项 UX 改动的过程
  记录。
- 逐条评审记录：`story_report_peer_review_opus-5.md`（已归档，不在版本控制范围内）。
- **本方案已全部落地**（P0–P9，见 `story_report_dev_plan_opus-5.md`/`story_report_dev_plan_2_sonnet-5.md`
  与各阶段 ActionPlan 的执行记录）。本节原先在此列出的"若本方案被采纳，需要相应更新的既有条目"
  清单是落地前的待办快照，写就时尚不知道每一条最终会按原方案落地还是改道——例如清单曾预判
  `KNOWN_ISSUES §1.22` 会"分批实施"，但该条目最终实际处置是"决定不做"。该清单已随各阶段
  ActionPlan 与 `KNOWN_ISSUES §3` 的执行记录逐条兑现或改道，不再需要在这里重复维护；
  `KNOWN_ISSUES_sonnet-5.md` 是当前状态的唯一权威来源，本文不再是它的待办输入。
- 阶段划分与验收边界见 `docs/future-strategy/story_report_dev_plan_opus-5.md`；
  每个阶段开工前另行编写该阶段的 ActionPlan，本文不承担执行级细节。
- 本文**没有修改任何代码或其他文档**。
