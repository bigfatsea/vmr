- **建议把 `evidence/*.md`（system prompt / tools 哈希去重）写进 UserGuide 产物清单**，避免新用户依赖时找不到。

### C.4 待处理事项汇总（按重要程度与建议优先级排序）

> 本节汇总前文（C.1 逐批发现 / C.2 最终总结 / C.3 补充意见）中**确需进一步处理**的事项，剔除纯优点、确认 OK、已撤回结论，去重并合并同类后按优先级排列。每条给出：问题 → 依据（来源）→ 建议动作。

#### P0 · 阻断可读性（建议立即做，纯呈现层改动、收益最大）

**P0-1 宏观报表 §6 会话长尾 + `vmr-requests-<tag>.md` 巨大，无折叠/过滤**
- 问题：`vmr-report.md` 的 §6 占报告全文约 45%（~1120 行），数百行微会话把大会话挤在顶部；`vmr-requests-lobster.md` 单文件 11,704 行，占 suite 输出近半。二者在 full corpus 下都失去信息密度。
- 依据：B1-1、B2-7、C.2 布局 P0。
- 建议：① 在 `vmr-requests-<tag>.md` 每个 session 标题行下补一行会话级摘要（token 总量 + outcome，如 `1.90M / 27.55M / 132.9K · ok (1 error)`），随后**删除 `vmr-report.md` 的 §6 整表**，改为一行指向 `vmr-requests.md` 的链接（直接减报表 45% 体积）；② 长尾（<N 轮/无实质动作）会话折叠进 `<details>`，或默认只显示 ≥N 轮 / 有实质 action 的会话，并提供"只看 task 类"开关。

**P0-2 `vmr-requests.md` 末尾 15,281 行全量平铺表（`# All Requests (chronological)`）**
- 问题：该平铺表占 `vmr-requests.md` 99% 篇幅（2.8MB / 15,359 行），与各 `vmr-requests-<tag>.md` 明细 100% 数据重复。
- 依据：C.3。
- 建议：直接移除平铺表，让 `vmr-requests.md` 回归几十行的纯导航索引；全量时序留给 `vmr-requests.json`（脚本），错误时序留给 `vmr-requests-failed.md`。

**P0-3 各 section 长尾普遍失去信息密度**
- 问题：不止 §6，全文在长尾处（§2 冷端点、§5 低流量端点等）都有同一倾向。
- 依据：C.2 布局 P1。
- 建议：每 section 默认"Top-N + 折叠其余"，需显式展开。

#### P1 · 核心体验（建议尽快做）

**P1-1 对比/单任务 md 内联整份文档/网页正文**
- 问题：`write` 工具参数直接内联整份 Markdown 总结、整段网页调研，单任务 md 膨胀到 4,966 行；compare md 内联 760 行系统提示正文（占全文 65%）。
- 依据：B4-11、B7-16、C.2 布局 P1。
- 建议：大块正文（写入的文件、完整网页、系统提示）折叠进 `<details>` 或指向 `details/*.md` / `evidence/*.md`；两侧 system prompt 相同时（本例如 0 changes）合并为一个折叠块，标注"以下为两侧共享上下文"。

**P1-2 HTML 看板在巨大 journey 下结构时间线平铺，未按 Task 分组**
- 问题：CRITICAL 版 518 步时 `srow` 层层平铺，无按 Task 的视觉分组/缩进，垂直滚动长、看不清 Task 边界。
- 依据：B5/B6-14、C.2 布局 P1。
- 建议：按 Task 分块 + 可折叠的分层时间线（每个 Task 一个 `<details>`，Step 在 Task 内缩进）。

**P1-3 compare HTML 的 Opening instruction 两侧完整各贴一遍**
- 问题：B 侧指令以 `<details>` 完整展开，与 A 侧几乎只差时间戳，重复度较高。
- 依据：B7-17。
- 建议：两侧相似度 >90% 时合并为"共通指令"，仅列出差异参数。

#### P2 · 统计/噪声误导（建议中期做）

**P2-1 语料分布均值被少数超长任务严重拉偏**
- 问题：Agent-Side Execution `Median 8.2s / Mean 11626s`；Net Working `Median 129.8s / Mean 11994s`，被 Max 36 天的极少数 journey 稀释。对非统计背景读者极度误导。
- 依据：B9-19、C.2 布局 P2。
- 建议：分布表加 P99，或加一句"均值受少数超长任务影响"脚注。

**P2-2 `unused_tool_result` 占 finding 69% 且多为探索噪声**
- 问题：规则对"探索性任务正常中间输出不再引用"的固有噪声占比过高，且该 journey 一次就报 27 条，真正高价值的 finding（unverified_completion_claim / plan_execution_misalignment）被淹没。
- 依据：B4-12、B9-20、C.2 布局 P2。
- 建议：在 finding 命中率表对 `unused_tool_result` 加解释性脚注；按置信度/Code 分组折叠，默认展开高置信度（LLM inferred HIGH），把低置信度的 unused_tool_result 收进折叠。

**P2-3 缺"跨时间段趋势对比"视图**
- 问题：报告是单时间窗快照；`-corpus` 是 cross-journey 统计、`-compare` 是双任务对比，但无"同一指标跨时段（7月 vs 8月）"对比。用户想验证"实施了 X 后改进多少"只能跑两次再自己 diff。
- 依据：C.2 缺口 5、布局 P2。
- 建议：新增 `-range` / `-compare-period`，或在报表内提供日期分桶趋势。

#### P3 · 打磨/术语/元数据（建议随时做）

**P3-1 zh 术语两处不统一**
- 问题：`§6.7"Compaction 还原"` 未翻译 Compaction；`task/zh journey 的 "System Prompt"` 未翻译。
- 依据：B12-23、B12-24。
- 建议：统一译为"压缩"或"上下文压缩"、"系统提示词"。

**P3-2 索引 `sNN` 序号与"N sessions"并存易误读**
- 问题：`s432` 是文档内位置序号，`422 sessions` 是会话总数，口径不同并存易让新读者以为 432>422 是矛盾。
- 依据：B2-8、C.2 布局 P3。
- 建议：索引头加一句"sNN 是本文档内位置序号，非会话总数"。

**P3-3 cost 表缺少空档日**
- 问题：2026-07-25/26/27、08-04/11/15/16 等日期未出现在"Estimated Cost by Date"，读者会疑惑"这天去哪了"。
- 依据：B1-6。
- 建议：补 0 值行，或加一句"未列出的日期无该日成本记录"。

**P3-4 `agent` 同名跨 protocol 在同表并列**
- 问题：§1/§4/§5 中 `agent` 同时出现在 anthropic-messages 与 openai-completions 两行，读者易困惑"为什么出现两行"。
- 依据：C.2 错误/逻辑。
- 建议：在 model 名后加协议后缀，或注明"同名不同 protocol 分列"。

**P3-5 tool-waste.html 无回链到 vmr-report.md**
- 问题：独立 HTML 单屏卡与 §7 数据同源，但无回链，无法理解"87% 是什么"的上下文。
- 依据：B11-22。
- 建议：加一行指向 `vmr-report.md` §7 的链接。

#### 结构性缺口（需新功能，列为 backlog）

**G-1 无"任务是否达成目标"信号（最高价值缺口）**
- 问题：`-journey`/`-compare`/`-corpus` 只呈现"过程"，不能回答"这事办成了没有"。读者只能靠 deliverable（file-write）或耗时代理猜测。
- 依据：C.2 缺口 1。
- 建议：探索从审计记录可推断的成功/失败代理（如最终回复含完成确认、deliverable 落盘、plan 全部 check 完成），或引入用户显式标记。

**G-2 无"客户视角成本 + 月度趋势"分摊**
- 问题：§2 按客户给总估算，但无"某客户本月 $ 趋势 / 环比"二维视图。
- 依据：C.2 缺口 3。
- 建议：新增按 client × 时间桶的成本视图。

**G-3 无"同一用户意图跨会话延续"聚合入口**
- 问题：会话按 client 分组，但"同一主题（如 DNS 调研）跨 3 个 session"无自动关联提示，需读者凭 title 人工拼。
- 依据：C.2 缺口 2、B1-3（obster/lobster 拼错即此类噪声）。
- 建议：增加相似 title / 内容哈希聚类提示，并提示疑似重复或拼错的 client key tag。

**G-4 无导出 CSV/Excel 的方便入口**
- 问题：`vmr-report.json` / `vmr-requests.json` 嵌套 schema 对 pandas 用户需先展平，用户更想要扁平 CSV（如 `monthly_cost_by_client.csv`）。
- 依据：C.2 缺口 4。
- 建议：新增 `-format csv` 导出，或提供展平版 jsonl。

**G-5 调度型 workload 与真实交互混在同一个宏观报表**
- 问题：§0 top-line 是全量加总（含 heartbeat/dream_diary），想只看真实交互需自己看 §5。
- 依据：C.2 缺口 7、B3-9、B3-10。
- 建议：增加"排除调度型/只看真实交互"的视角开关，或在 §0 同时给出排除后的数字。

**G-6 生成过程默认应"坐标引用 + 按需懒生成"，而非默认 `-details`**
- 问题：`-details` 在 full corpus 下物化 1.7GB 且大部分详情不会被打开，拖慢运行（11 分钟 vs 3 分钟/语言）。
- 依据：C.3。
- 建议：确认默认（非 `-details`）即懒生成是正确的使用方式，文档强化引导。

> **说明**：`evidence/*.md` 未列入 UserGuide 产物清单、以及 §6.7 compaction 表加直达 journey Step 锚点链接，属于文档/导航增强，见 C.3；`vmr-stories.json`/`vmr-report.json` 作为脚本入口是被低估的资产，属正向提示，不计入待处理问题。

---

## C.5 复核结论与处置（Sonnet 5 · 2026-08-31）

> 本节对 C.4 全部 24 条（P0-1…P3-5、G-1…G-6）逐条复核：**问题是否成立 → 根因是否正确 → 方案是否合理、有无更优解 → 值不值得改（ROI）**。判据一律以 `sample_reports/` 下的真实产物（macro / corpus / task / compare / incident / suite，均为当前分支实跑结果）和 `internal/{report,story,i18n,reqdetail}` 源码为准，不采信 C.1–C.3 的转述（那批中间稿已不存在）。
>
> C.4 里引用的批次编号（B1-1 等）已无法回溯，但 C.4 每条自带「问题 + 方案」，可独立核实。
>
> **处置原则**：问题成立、根因清楚、方案无争议、ROI 明显为正的 → 直接改（留 working tree，未 commit，等人工 review）。其余（有争议、根因存疑、方案需再设计、或 ROI 不划算）→ 保留，附分析与 Action Plan。

### C.5.0 总览

| 编号 | 问题成立？ | 根因评估 | 方案评估 | ROI | 处置 |
|---|---|---|---|---|---|
| P0-1 §6 长尾臃肿 | ✅ 成立（§6 = 报表 45%） | ⚠️ 描述不准（大会话已在顶部，问题是尾部无信息量的行太多） | ⚠️「删整表」牵连 compaction chain，属产品决策 | 中 | **✅ 已实施轻量方案**（长尾折叠，见 C.5.5） |
| P0-2 全量平铺表 | ✅ 成立（占 vmr-requests.md 99.5%） | ✅ 正确（与分组明细同源） | ⚠️ 与「唯一跨组时序视图」的既定设计冲突 | 中高 | **✅ 已实施（方案 C，直接删）**，见 C.5.5 |
| P0-3 各 section 长尾 | ✅ 部分成立 | ✅ 正确 | ⚠️ 需逐 section 定 N，工作量分散 | 中 | **保留**（P0-1 已单独处理 §6） |
| P1-1 md 内联大正文 | ⚠️ 部分成立（已有 3000 rune cap + `<details>` + details/ 外链） | ⚠️ 根因夸大（非「整份」） | ⚠️ 残余问题真实但需再设计 | 中 | **保留** |
| P1-2 HTML 大 journey 未分组 | ⚠️ 部分成立（已有 `<div class="task">` 分组，但 Step 无缩进、与 Task 视觉齐平） | ⚠️ 「无分组」不准，「无缩进」成立 | 加缩进 = 纯 CSS，低成本 | 中 | **✅ 已实施缩进**（rail + 16px 缩进，见 C.5.5） |
| P1-3 compare 两侧指令各贴一遍 | ✅ 成立 | ✅ 正确 | ⚠️ 需相似度阈值，与 P1-1 同源 | 中 | **保留** |
| P2-1 语料均值被拉偏 | ✅ 成立（Mean 11626s vs Median 8.2s） | ⚠️ 只说对一半（更深层是单间隙无上限归因） | ✅ 脚注方案合理且 ROI 高 | 高 | **✅ 已修复**（脚注） |
| P2-2 unused_tool_result 淹没 | ⚠️ 前提有误（69% 是「命中率」非「占比」） | ❌ 部分错误 | ❌ 「按置信度分组」与数据模型不符 | 低 | **保留**，附事实更正 |
| P2-3 缺跨时段趋势 | ✅ 成立（确实没有） | ✅ 正确 | 新功能，需独立设计 | 中 | **保留**（backlog） |
| P3-1 zh 术语不统一 | ❌ 前提偏弱：MD 侧其实自洽（`§6.7 Compaction 还原` 同 `§6.5 Sticky 有效性`，是「留英文特性名 + 中文描述」的既定惯例） | ⚠️ 「统一全译」不成比例且牵连多份文档 | 低 | 低 | **✅ 方案 2 处理完毕**：不改代码，登记为 KNOWN_ISSUES §2.5 刻意非-fix，见 C.5.5 |
| P3-2 sNN vs N sessions | ⚠️ 勉强成立 | ✅ 正确 | 一句话脚注，价值也小 | 低 | **保留** |
| P3-3 cost 表缺日期 | ✅ 成立 | ❌ **根因错误**（不是空档日，是当日无可定价记录） | ❌ 「补 0 值行」会误导 | 中 | **✅ 已修复**（诚实脚注，非补 0） |
| P3-4 agent 跨 protocol 并列 | ❌ 基本不成立（表内已有 Protocol 列区分） | — | 「加协议后缀」与 Protocol 列重复 | 极低 | **保留**，仅记录 |
| P3-5 tool-waste.html 无回链 | ⚠️ 成立但与「独立可分享卡片」设计意图冲突 | ✅ 正确 | 低 | 低 | **保留** |
| G-1 无「任务达成」信号 | ✅ 成立（设计已知：VMR 零埋点） | ✅ 正确 | 最高价值但最难，需独立设计 | 高/难 | **保留**（backlog） |
| G-2 无客户×月度成本 | ✅ 成立 | ✅ 正确 | 新视图 | 中 | **保留**（backlog） |
| G-3 无跨会话意图聚合 | ✅ 成立 | ✅ 正确 | 需聚类，易误报 | 中 | **保留**（backlog） |
| G-4 无 CSV 导出 | ✅ 成立 | ✅ 正确 | `-format csv` 或 flatten | 中 | **保留**（backlog） |
| G-5 调度型与真实交互混合 | ✅ 成立 | ✅ 正确（§0 是全量加总） | 「排除调度型」视角开关 | 中 | **保留**（backlog） |
| G-6 默认应懒生成 | ❌ 已经是（KNOWN_ISSUES §1.2 段后有守卫测试锁定） | ❌ 事实错误 | 无需改代码 | — | **保留**，仅补文档指引 |

---

### C.5.1 已直接修复

#### ✅ P2-1 — 语料指标分布：均值脚注

**问题复核（成立）**：`sample_reports/corpus/en/stories/vmr-story-corpus.md` 实测：

| Metric | Mean | Median | P90 | Max |
|---|---|---|---|---|
| Agent-Side Execution Time | 11626.3s | 8.2s | 232.7s | 3125755.6s（≈36.2 天） |
| Net Working Time | 11994.6s | 129.8s | 1299.1s | 3125811.5s |

均值比 P90 还高一个量级，比中位数高约 1400 倍。对非统计背景读者，「Agent 侧执行 Mean 3.2 小时」是严重误读。

**根因评估（只说对一半）**：C.4 说「被 Max 36 天的极少数 journey 稀释」——方向对，但不完整。更深层：`computeTimeSplit`（`internal/story/metrics.go`）把「上一 Step 响应落地」到「下一 Step 请求到达」之间的墙钟间隙，只要下一 Step 非 `HumanInitiated` 就整段计入 `AgentExecMS`，**单个间隙没有任何上限**。一条跨周的 lineage（stitch 拼接的、或长期存活的 session）里，某个「N 天后又发了一个非人类发起的请求」的间隙会被整段当成「Agent 在忙」。这不是纯统计尾部效应，是归因规则本身在超长间隙上失真。

**方案评估（脚注合理、ROI 高）**：C.4 给的「加 P99 或加脚注」中，脚注是对的选择：
- 加 P99 = 改 `Distribution` 结构 + `computeDistribution` + JSON schema + golden，且 P99 在这类语料上仍可能远低于 Mean（P90 已 232s，Mean 11626s），信息增量有限。
- 脚注（1 个 i18n 字段 + 1 行渲染）成本极低，且与该报告已有的两处同类免责脚注（Anthropic 覆盖率、效应量不报 p 值）风格一致。
- 真正的根因修复（对单间隙做上限，例如「>1h 的间隙不计入 Agent 执行，归入 idle/unknown」）需要改指标语义 + 更新 `docs/VirtualModelRouter_Design_v4_Analytics.md` 的时间拆分定义 + 差分测试，属于独立议题，不在本次范围。脚注是当下 ROI 最优。

**改动摘要**：
- `internal/i18n/story_corpus.go`：`CorpusText` 新增 `MetricDistFootnote string`，EN/ZH 各一句（「时间类指标的均值对少数超长 Journey 极其敏感……甚至高于 P90。判断常见情况请优先看中位数与 P90」）。
- `internal/story/render_corpus.go`：指标分布表渲染后加 `w("%s", t.MetricDistFootnote)`，用 `metricRows > 0` 守卫（指标表为空时不渲染脚注——比"恒出现"更合理，空表下一句"均值受影响"没有意义）。
- `internal/story/corpus_test.go`：`TestRenderCorpusMarkdown` 的"populated corpus"子测试加断言，确认脚注出现。

**验收**：`go test ./internal/story/ ./internal/i18n/` 通过（已跑）。人工 review 点：脚注措辞是否 over-claim；是否该同时在 `-compare` 的净工作时长处也提（设计文档要求「墙钟总时长紧邻净工作时长且注明不是效率指标」，compare 已做，corpus 之前没有）。

**收尾**：无需改 CHANGELOG（分析产物层微调，非用户可感知的行为变更）；`sample_reports/` 为 gitignore 的本地快照，重跑即刷新，不阻塞。

---

#### ✅ P3-3 — 按日成本表缺失日期：诚实脚注（不是补 0）

**问题复核（成立）**：`sample_reports/macro/en/vmr-report.md` §2「Estimated Cost by Date」缺 2026-07-25/26/27、08-04/11/15/16/18/24/25/28。

**根因评估（C.4 判断错误）**：C.4 说这些是「空档日……无该日成本记录」，建议「补 0 值行」。**这是错的**。同一份报告的 §5「Daily Activity」mermaid 图（`section_workload.go`，与 §2 同样迭代 `rep.ByDate`）里，07-25 = 59 请求、07-26 = 69、07-27 = 127、08-24 = 518、08-25 = 877——**这些日期有大量流量**。真实根因：`section_cost.go` 的按日循环有 `if d.CostEstimate != nil` 过滤，而 `rep.ByDate` 每条 entry 必有 ≥1 条记录（`aggregate.go` 按记录建桶），`CostEstimate == nil` 意味着**当日没有任何一条记录解析出单价**——即当天流量全部走了定价表里没有的上游端点（如 `cliproxy`、`bai` 等，它们出现在 §4 延迟表但不在 §2 成本端点表）。

所以「补 0 值行」会把「成本未知」谎报成「成本为 0」，是主动误导。

**方案评估（改为诚实脚注）**：既定设计是「只渲染有数据的行」（`section_cost_test.go` 的 `TestRenderCostEstimate_NoDateData_TableAbsent` 明确锁死），与相邻的 by-model/endpoint/client 表一致，不应为按日单独破例改成「渲染所有行 + 显示 —」。正确做法：当存在「有流量但未解析出单价」的日期时，在表下加一句说明。

**改动摘要**：
- `internal/i18n/report_cost.go`：`CostText` 新增 `ByDatePartialNote string`，EN/ZH 各一句（「仅列出存在可定价记录的日期；其余日期有请求流量，但其上游端点未解析出适用单价，当日成本未知（并非 0）」）。
- `internal/report/section_cost.go`：按日循环里，遇到 `d.CostEstimate == nil` 置 `unpricedDate = true`；表后若为 true 则渲染 `t.ByDatePartialNote`。
- `internal/report/section_cost_test.go`：新增 `TestRenderCostEstimate_ByDatePartialNote`——混合价/无价日期时脚注出现，全部有价（含解析出的 0）时脚注不出现。

**验收**：`go test ./internal/report/ ./internal/i18n/` 通过（已跑）。人工 review 点：措辞是否够准（「上游端点未解析出适用单价」是否比「无可定价记录」更好懂）；是否顺带值得在 §2.5 Provider Spend 处也提一句「哪些 provider 未定价」（本次未做，见下方 backlog 建议）。

**收尾**：同上，不动 CHANGELOG。

---

### C.5.2 保留待人工 review（逐条分析 + Action Plan）

#### P0-1 — 宏观报表 §6 会话长尾

**复核（成立，措辞需纠正）**：`sample_reports/macro/en/vmr-report.md` 共 2488 行，§6「Sessions & Tasks」占 862–1983 行 = **1122 行 / 45.1%**，与 C.4 数字吻合。但 C.4 「数百行微会话把大会话挤在顶部」措辞不准：`aggregate.go:579` 明确按 `Requests`（轮数）降序排，大会话本就在顶部（s432=216 轮在第一行）。真实问题是**尾部几百行 17–25 轮的 cron 会话没有信息量，却把整个 §6 撑到报表的 45%**。

**根因**：`section_sessions.go` 的 `renderSessions` 对每个 client 的全部 interactive 会话无脑平铺，无 Top-N、无折叠。

**方案评估**：C.4 方案 ① 「给 `vmr-requests-<tag>.md` 每个 session 标题补一行摘要，然后**删掉 §6 整表**，换成一行链接」——牵连两点：
1. `renderSessions` 末尾会调 `renderCompactionChains`（≥3 节点的 compaction 链 mermaid 图），删 §6 得给它另找位置（注意 §6.7 是另一回事，是 compaction 还原表，不是链图）。
2. §6 是宏观报表里唯一「一屏看完所有会话规模 + outcome」的地方，删掉等于让读者必须跳到 `vmr-requests-<tag>.md`（每个 client 一个文件）才能看会话列表。「co-equal halves，分析半区不是附庸」的项目定位下，这是个产品决策，不是无争议清理。

C.4 方案 ② 「长尾折叠进 `<details>` / 默认只显示 ≥N 轮 / 加『只看 task 类』开关」方向更稳，但要定 N、要加渲染分支。

**建议（轻量方案，ROI 更优）**：
- 保留 §6，但**每个 client 的会话表按 `Requests` 降序后，前 K 行正常渲染，其余收进一个 `<details><summary>+ 其余 M 个会话（均 ≤N 轮）</summary>`**。K 取 15–20，N 取阈值（如 ≤ 12 轮或 1 task）。
- Compaction chain 图不动。
- 不删表、不加 CLI flag。

**Action Plan（若决定做）**：
1. **改什么**：`internal/report/section_sessions.go` 的 `renderSessions`（每 client 循环体）；`internal/i18n/report_sessions.go` 的 `SessionsText` 加 `LongTailSummary func(n, turnCap int) string`。
2. **怎么改**：client 的 `rows` 已按全局 `rep.Sessions` 顺序（即 Requests 降序）。取 `head = rows[:min(K,len)]` 正常 `renderSessionRow`；`tail` 里 `Requests <= N` 的部分包进 `<details>`，表头重复一次；`tail` 里仍 > N 的（异常：低排名但高轮数，基本不会有）跟 head 一起正常渲染。常量 `sessionsHeadRows = 20`、`sessionsLongTailTurnCap = 12` 放文件内，附注释说明校准依据。
3. **怎么测**：`section_sessions_test.go`（新建，当前无）——① 会话数 ≤ K 时不出现 `<details>`；② 会话数 > K 且尾部有 ≤N 轮会话时，`<details>` 出现且 summary 里的计数正确；③ compaction chain 图仍在 `<details>` 之后渲染（防止折叠逻辑吃掉链图）。
4. **怎么验收**：重跑 `sample_reports` macro，目测 §6 行数应从 ~1122 降到 ~300–400；§6 占比从 45% 降到 ~20%；大会话仍全部可见；cron 长尾进折叠。
5. **文档同步**：`docs/VirtualModelRouter_Design_v4_Analytics.md` 的 §6 描述加一句「长尾折叠」；`docs/KNOWN_ISSUES_sonnet-5.md` 若 P0-3 一并处理则合并登记「各 section Top-N + 折叠」为一条已闭环。`docs/UserGuide.md` + `.zh` 同步（若其中描述了 §6 的完整性）。
6. **收尾**：`CHANGELOG.md` `[Unreleased]` → `Changed`：「`vmr analyze` 宏观报表 §6 会话长尾折叠，报表体积显著下降」。`go test ./internal/archtest/...`（`renderSessions` 会变长，注意 per-function 行预算，超了就拆 `renderClientSessions` 子函数）。

---

#### P0-2 — `vmr-requests.md` 末尾全量平铺表

**复核（成立）**：`sample_reports/macro/en/vmr-requests.md` 共 15359 行，导航索引占 1–74 行，`# All Requests (chronological)` 平铺表占 75–15359 = **15285 行 / 99.5%**。由 `requests.go` 的 `writeAllRequestsFooter` 生成，逐条渲染每个 `RequestRow`。

**根因**：`writeAllRequestsFooter` 无条数上限，全量渲染。

**方案评估**：C.4 「直接移除，全量时序留给 `vmr-requests.json`，错误时序留给 `vmr-requests-failed.md`」——技术上可行（`WriteRequestsJSON` 确实写全量行；`vmr-requests-failed.md` 确实是错误时序）。但：
- 平铺表和各 `vmr-requests-<tag>.md` **不是 100% 冗余**：平铺表多了跨组的全局时序 + `VM/API`（protocol/model）列，少了 `Turn#`/`msgs`/`ttft` 列和 session/task 层级。是「同一批 request 行的不同切法」。
- `writeAllRequestsFooter` 的注释明说这是「唯一一处跨组时序视图该在的地方」——**这是有意的设计决策**。
- 但客观讲：15K 行的 markdown 表格没人真会滚，人类可读价值接近 0；脚本场景 JSON 更合适。所以 C.4 的判断大概率是对的，争议度不高。

**建议（有界折中，而非直接删）**：
- 方案 A（最小）：平铺表**只保留最近 N 行**（如 N=500，按时间倒序取最新），末尾加「完整时序见 `vmr-requests.json`」。
- 方案 B：平铺表整体收进 `<details>`（默认折叠），并在 summary 注明行数 + 指向 JSON。
- 方案 C（=C.4 原方案）：直接删，`vmr-requests.md` 回归几十行索引。
- 倾向 **A 或 C**：A 保留「快速瞄一眼最近发生了什么」的human价值且体积可控；C 最干净但丢失该视图。B 只是把 2.8MB 藏起来，git/grep/加载成本还在，价值最低。

**Action Plan（以方案 A 为例）**：
1. **改什么**：`internal/report/requests.go` 的 `writeAllRequestsFooter`；`internal/i18n/report_requests.go` 加 `AllRequestsCapNote func(shown, total int) string`。
2. **怎么改**：`all` 排序后若 `len(all) > allRequestsMax`（常量，500），倒序取尾部 N 条再正序渲染，表后加 `AllRequestsCapNote`。`allRequestsMax` 附注释。
3. **怎么测**：`requests_test.go`（或现有 index 测试）——① `< N` 条时无 note、全量渲染；② `> N` 条时只渲染 N 行 + note，且渲染的是最新的 N 条（检查首行时间戳）。
4. **怎么验收**：重跑 macro，`vmr-requests.md` 从 15359 行降到 ~600 行；`vmr-requests.json` 行数不变（全量）；`vmr-requests-failed.md` 不受影响。
5. **文档同步**：`docs/VirtualModelRouter_Design_v4_Analytics.md` 里 `vmr-requests.md` 的描述改为「索引 + 最近 N 条时序，全量在 JSON」；`docs/UserGuide.md` + `.zh`。
6. **收尾**：CHANGELOG `[Unreleased]` → `Changed`。archtest 行预算。

---

#### P0-3 — 各 section 长尾普遍失去信息密度

**复核（部分成立）**：C.4 点名 §2 冷端点、§5 低流量端点。实测：
- §2「Estimated Cost by Endpoint」23 行，尾部多条 `0.0000 CNY`（`google-abfs:gemini-3.5-flash` 等 2 token 的行）。
- §5「By Endpoint」更长。
- §3 Reliability（212–447，235 行）、§5.5 Per-Client Upstream Attribution（694–861，167 行）也偏长。
成立，但程度不如 §6。

**根因**：多数 section 渲染器直接迭代聚合结果全集，无 Top-N。

**方案评估**：C.4 「每 section 默认 Top-N + 折叠其余」——方向对，但每个 section 的「N 取多少、按什么排、折叠粒度」都不同，是一批分散的小改动，需要逐个判断。其中有的 section 已经做了（§7 tool-waste 是 Top-5/Top-8，`toolWasteTopN`；corpus 相关性是 Top-15，`corpusCorrelationsShown`）——说明项目已有这个 pattern，只是没铺开。

**建议**：不作为一个大任务，而是**跟 P0-1 一起，先只处理 §6（收益最大）**，其余 section 逐个评估。优先级：§5「By Endpoint」>§2「By Endpoint」>§3 > 其余。判据：某 section > 报表 10% 且尾部 > 1/3 行的估算/请求数低于噪声阈值，才值得折叠。

**Action Plan（若逐个做，以 §5 By Endpoint 为例）**：
1. **改什么**：`internal/report/section_workload.go` 的 endpoint 表渲染；对应 i18n。
2. **怎么改**：按 Requests 降序，前 K（如 15）正常渲染，其余进 `<details>`，summary 注明「+M 个端点，合计 X 请求」。复用已有的 `<details>` pattern。
3. **测/验收/文档/收尾**：同 P0-1 模式，逐 section 各自的 `section_*_test.go` 加折叠断言。
4. **登记**：全部铺开后在 `docs/KNOWN_ISSUES_sonnet-5.md` 记一条「宏观报表所有列表型 section 统一 Top-N + 折叠」为设计约定。

---

#### P1-1 — 对比/单任务 md 内联整份文档/网页正文

**复核（部分成立，根因夸大）**：
- 单任务：`sample_reports/task/en/stories/journey-…-cf31407d.md` = 4966 行，与 C.4 吻合。
- incident：`sample_reports/incident/critical_en/stories/journey-…-b0ebb0e4.md` = **35295 行 / 1.9MB**，378 步，同一份 ACM 规格文档被 `write` 重写 26 次。
- compare：`compare-…-72fdd80c-vs-…-8a5cd1de.md` = 1120 行，两侧 system prompt 节选各约 392 行，合计 ~70%。

但 C.4 「直接内联整份 Markdown 总结」不准确：`render_spine_args.go` 有 `spineFullCap = 3000`（rune），每个 `write` payload 在脊柱里最多 3000 rune，且已包在 `<details>` 里折叠，完整内容已外链到 `details/*.md`（`reqdetail`）。HTML dashboard 更是**完全不内联** tool args（所以 incident 的 `.html` 只有 760KB vs `.md` 1.9MB）。

**残余真问题**：
1. 3000-rune 节选 × 数百步，累计仍能到几 MB（incident 的 ACM 文档 26 次重写 = 26 段近似内容的节选）。
2. compare 的 system prompt 节选上限是 20000 字符（设计文档定的，为覆盖「加载了哪些上下文文件」这类落在 1.5 万字符处的声明），两侧各贴一份，且本例两侧 `Changes` 均为 0、内容几乎逐字相同。

**方案评估**：C.4 「大块正文折叠进 `<details>`（已做）/ 指向 details/（已做）/ 两侧相同时合并为一个折叠块」——前两项已实现，**真正的增量是「两侧 system prompt / opening instruction 相同时合并」**（也是 P1-3）。这块 ROI 中等：
- 精确相等的合并是无争议的（`sp.A.Excerpt == sp.B.Excerpt`），但节选是截断的，「节选相等」≠「原文相等」，label 措辞要小心（「两侧节选一致」而非「两侧 system prompt 相同」）。
- 相似度 > 90% 的合并需要定义相似度算法 + 阈值，有争议。
- 对单任务/incident 的「同一文档多次重写」去重，需要跨 Step 识别「这是上一版的修订」，是 `ctxgraph` edit classification 的活，工作量大。

**建议**：
- **只做 compare 的精确相等合并**（P1-1 与 P1-3 合并处理，见下）。
- 单任务/incident 的累计体积问题**不改**：`.md` 自包含是有意设计，读者要精简版可以看 `.html`（已经不内联）或 `details/`；真要压，成本远大于收益。在 UserGuide 里补一句「超大 journey 建议看 `.html` 或 `-journey` 单看」。

**Action Plan（compare 精确合并，与 P1-3 共用）**：见 P1-3。

---

#### P1-2 — HTML 看板大 journey 下结构时间线未按 Task 分组

**复核（基本不成立）**：C.4 说「`srow` 层层平铺，无按 Task 的视觉分组/缩进」。看源码 `internal/story/render_html_dashboard.go` 的 `htmlStructure`：**每个 task 已经包在 `<div class="task" id="task-N"><h3>Task N · 标题</h3> … </div>`** 里，step rows 在 div 内；左侧 rail（`htmlRail`）也已把 step 锚点嵌在 task 下。CSS（`render_html_assets.go`）`.task { margin-bottom: 18px }` + `.task > h3` 有视觉分隔。所以「无 Task 分组」是事实错误。

**残余（弱）**：378/518 步时即使有 task 分组，也没有**按 task 折叠**——一屏还是要滚几百个 srow。C.4 的「每个 Task 一个 `<details>`」是这个意思。

**方案评估**：Task 级 `<details>` 有一定价值，但：
- 看板的核心价值就是「一眼扫完整条时间线 + ⚠️ 标记的分布」，默认折叠会破坏这个概览。
- 折叠状态怎么定？默认全折叠 → 概览没了；默认全展开 → 没变化；按「含 finding 的 task 展开、其余折叠」→ 是个合理折中，但要写逻辑。
- 生成 300+ 步 incident 看板的频率有多高？incident sample 是刻意造的极端。

**建议**：**不改**，仅记录。若将来 incident 场景变高频，再考虑「含 finding 的 task 默认展开、纯 append 的长 task 默认折叠」，且必须保留顶部一个「全展开/全折叠」开关。

**Action Plan（若将来做）**：
1. `htmlStructure` 每个 task div 包一层 `<details open>`（含 flagged step 时 `open`，否则不 `open`）；`<summary>` = 现在的 `<h3>` 内容 + step 数 + finding 数。
2. `render_html_assets.go` 的 IntersectionObserver（`document.querySelectorAll('section.block, .task[id], .srow[id]')`）要处理折叠态下 `.srow[id]` 不可见时 rail 高亮的降级。
3. 加一个顶部 toggle 按钮（纯 JS，`details` 批量 open/close）。
4. 测试：`render_html_test.go` 断言 flagged task 带 `open`、纯 append task 不带。
5. 文档：无（HTML 看板细节不在设计文档里逐一描述）。

---

#### P1-3 — compare 的 Opening instruction / System Prompt 两侧各贴一遍

**复核（成立）**：`compare-…72fdd80c-vs-…8a5cd1de.md`：
- `## Initial Instruction`：A 的节选 13–68 行、B 的节选 70–125 行，两侧结构逐条对应（同为 "Source Fetcher (COMBINED…)" / "CRITICAL: Output structured JSON first" / "Pipeline …"），仅时间戳等极少差异。
- `## System Prompt Size & Stability`：A/B 各一个 `<details>` 节选（205–597、599–991），两侧 `tokens` 同为 10.4K、`Changes` 同为 0，正文几乎逐字相同。

`render_compare.go` 的 `renderSysPrompt` / `renderInitialInstruction` 无条件渲染两侧，无「A≈B 则合并」。

**根因**：`renderExcerpt` 被调两次（A、B），没有前置的相等/相似判断。

**方案评估**：
- **精确相等合并**（`f.A.Text == f.B.Text` / `sp.A.Excerpt == sp.B.Excerpt`）：无争议，实现简单。风险：节选是前缀截断，「节选相等」只能说「前 N 字符相同」，label 要写「两侧此节选一致（截断前缀，不代表完整文本逐字相同）」。
- 相似度阈值合并（C.4 的 > 90%）：需引入相似度度量（编辑距离/token Jaccard），阈值主观，**不做**。

**建议**：只做精确相等合并。对 System Prompt 额外利用已有信号——`SysPromptFact` 里若两侧 `Tokens` 相等且 `Changes` 均为 0 且节选逐字相等，几乎可确定两侧 system prompt 一致，可以更自信地标注。

**Action Plan**：
1. **改什么**：`internal/story/render_compare.go` 的 `renderSysPrompt`、`renderInitialInstruction`；`internal/i18n/story_compare.go` 的 `CompareText` 加 `SharedExcerptLabel string`（EN/ZH，「两侧共享（节选一致）」）与 `SharedContextNote string`。
2. **怎么改**：
   - `renderInitialInstruction`：若 `f.A.Found && f.B.Found && f.A.Text == f.B.Text`，只渲一个 `renderExcerpt(w, t.SharedExcerptLabel, f.A.Text, f.A.Truncated, t)`，前面加一行 `SharedContextNote`；否则维持现状两侧各渲。
   - `renderSysPrompt`：表格（A/B 两行 tokens+Changes）保持不变（这是对比信息，要留）；只对下面的节选做合并——`sp.A.Excerpt == sp.B.Excerpt` 时渲一个。
3. **怎么测**：`render_compare_test.go`——① 两侧文本相等 → 输出只有一个 `<details>`、含 `SharedExcerptLabel`、不含 "A's …" / "B's …"；② 两侧不等 → 维持两个 `<details>`；③ 一侧 Found 一侧不 → 维持现状。
4. **怎么验收**：重跑 `sample_reports/compare/{en,zh}`，该 compare md 从 1120 行降到 ~700 行；两侧不同的 compare 样本（找一个 A/B system prompt 真不同的）输出不变。
5. **文档同步**：`docs/VirtualModelRouter_Design_v4_Analytics.md` 的 `ComparisonExtras` 描述里，System Prompt / Initial Instruction 节选处加一句「两侧节选逐字一致时合并为一块共享上下文」。`.zh` 同步（该设计文档是中文，无 `.zh` 兄弟）。
6. **收尾**：CHANGELOG `[Unreleased]` → `Changed`：「`vmr analyze -compare` 两侧 system prompt / 初始指令逐字一致时合并展示」。archtest 行预算（两个函数都会加分支，注意 per-function 预算）。redact 模式（`sample_reports/compare/redact_*`）也要跑一遍确认不炸。

---

#### P2-2 — `unused_tool_result` 占 finding 69% 且多为探索噪声

**复核（前提有误）**：
1. C.4 说「`unused_tool_result` 占 finding 69%」——**误读**。`sample_reports/corpus/en/stories/vmr-story-corpus.md` 的「Finding Hit Rates」表头是 `Hit Rate (≥1 occurrence)`，`unused_tool_result 69%` 是**「69% 的 journey 至少命中一次」**，不是「占所有 finding 的 69%」。两个完全不同的量。
2. C.4 说「多为探索噪声」——`findings_toolresult.go` 的 `detectUnusedToolResult` 注释明确记录：早期版本按 entity 触发、每 journey ~40 条，**已重新校准**为「只在整个 tool result 的所有 entity 后续全不引用时才触发」。而且同一份 corpus 报告的「Finding-Grouped Comparison」显示 `unused_tool_result` 命中组净工作时长中位数 253.3s vs 未命中组 36.6s（+86%）——它**与显著更长的 journey 强相关**，说「多为噪声」证据不足（虽然相关不等于因果）。
3. C.4 说「该 journey 一次报 27 条」——`sample_reports/task/en/stories/journey-…cf31407d.md` 的 Findings 段确实有 ~26 条编号项，其中约 15 条是 `unused_tool_result`。这条属实。

**真问题（弱化版）**：在**单条大型探索 journey** 里，findings 按 StepSeq 平铺（`ComputeFindings` sort 是 `StepSeq` then `Code`），十几条 `unused_tool_result` 会把高价值 finding（`plan_execution_misalignment` 等）夹在中间。但注意：该 journey 的 **LLM 解读层已经做了分优先级**（`#### 优先 3：带错误语义的未引用工具结果` / `#### 优先 5：其余 unused_tool_result`）——分组已经存在，只是在 LLM 层不在规则层。

**方案评估**：C.4 「按置信度/Code 分组折叠，默认展开高置信度（LLM inferred HIGH）」——**与数据模型不符**：
- `Finding.Confidence`（`findings.go:41-43` HIGH/MEDIUM/LOW）**只对 LLM-inferred finding 填充**，规则 finding（含全部 `unused_tool_result`）没有 confidence 字段。「按置信度分组」对规则 finding 无从谈起。
- 按 Code 分组会打散 findings 与决策脊柱 `⚠️` 标记的一一对应（`render_spine.go` 注释：「读者扫 ⚠️ 标记时期望在那个 Step 找到解释」）。

**建议**：
- **不按 C.4 方案做**。
- 低成本可做的：给 corpus 报告的「Finding Hit Rates」表加一句脚注，说明 (a) 这是「命中率」不是「占比」；(b) `unused_tool_result` 在探索型任务里天然偏高（中间产物用完不再引用是正常行为），高命中率本身不代表问题严重。——这条 ROI 尚可，但要小心不要变成「教读者忽略这个 finding」。
- 若真要在单 journey 里降噪：把**同一 Code 连续出现 ≥3 次**的 finding 折叠成「Code ×N（Step a, b, c…）+ 展开」——这个不依赖 confidence、不打散 Step 顺序（按第一次出现的 Step 定位），是更贴合数据模型的做法。但仍是中等工作量。

**Action Plan（若做「连续同 Code 折叠」）**：
1. **改什么**：`internal/story/render_spine.go` 的 `renderFindingsSection`；i18n `SpineText` 加 `FindingCodeRunSummary func(code string, n int, seqs string) string`。
2. **怎么改**：遍历已排序的 findings，把相邻同 `Code` 的 run（长度 ≥ `findingRunFoldThreshold`，取 3）合并：先渲一个 summary 行（Code ×N · Steps …），再把每条包进 `<details>`。run 长度 < 阈值的维持逐条平铺。
3. **怎么测**：`render_spine_test.go` / `llm_findings_test.go` 里加——① 2 条同 Code 不折叠；② 4 条同 Code → summary + 4 个 `<details>`；③ 混合 Code 交替出现不折叠；④ 折叠后每条的 evidence/action 仍可见。
4. **验收**：重跑 task sample，该 journey Findings 段从 ~26 条平铺变成「unused_tool_result ×15」折叠 + 其余逐条；决策脊柱 ⚠️ 锚点仍能跳。
5. **文档**：`docs/VirtualModelRouter_Design_v4_Analytics.md` Findings 段加一句「同类 finding 连续 ≥N 条折叠」。
6. **收尾**：CHANGELOG。archtest。

---

#### P2-3 — 缺「跨时间段趋势对比」视图

**复核（成立）**：确认无此功能。`-journey` 单条、`-compare` 双条、`-corpus` 跨 journey 统计，都不是「同一指标 7 月 vs 8 月」。宏观报表是单时间窗快照。

**根因**：从未设计。

**方案评估**：C.4 「新增 `-range` / `-compare-period`，或报表内日期分桶趋势」——是个合理的新功能方向，但：
- 宏观报表**已有**日期分桶（§5 Daily Activity 的 mermaid 图、§2 按日成本）——「趋势」的原料在，缺的是「两个窗口并排对比 + 环比%」。
- 「实施 X 后改进多少」这个诉求，更适合 `-compare-period`：给两个日期范围，输出两组聚合 + delta。
- 工作量不小（新 CLI flag + 双窗口聚合 + 新渲染 + i18n + 测试），需要独立设计。

**建议**：backlog。若做，优先 `vmr analyze -compare-period A..B vs C..D`，复用现有 `report` 聚合跑两遍 + 一个 diff 渲染层（类似 `story` 的 compare）。不要往宏观报表里塞「趋势」——会让本就臃肿的报表更长。

**Action Plan（若立项）**：需先写设计草案（放 `docs/` 讨论），这里只给骨架：
1. `cmd/vmr/cmd_analyze.go` 加 `-compare-period` flag，值形如 `2026-07-01..2026-07-31,2026-08-01..2026-08-31`。
2. `internal/report` 加 `AnalyzeSessionsRange(records, from, to)`（或在现有入口加时间过滤），跑两次得两个 `Report2`。
3. 新 `internal/report/section_period_compare.go` + i18n，渲染核心指标（请求数/成本/cache eff/p95/success rate/tool waste）的 A→B 对比表 + 环比%。
4. 差分测试：两个相同窗口 delta 必须全 0。
5. 文档：设计文档新增小节；UserGuide + `.zh`；CHANGELOG `Added`。

---

#### P3-1 — zh 术语两处不统一

**复核（局部成立，但不是简单 bug）**：
- `internal/i18n/report_compaction.go:16` ZH：`"§6.7 Compaction 还原 ⭐"`——"Compaction" 混在中文标题里。
- `internal/i18n/story_render.go`（ZH 块）：`SysPromptHeaderTitle: "## System Prompt\n\n"`——`sample_reports/task/zh/…` 和 `internal/story/testdata/golden_zh.md:9` 确实是一个突兀的英文 H2（周围是「概览」「决策脊柱」）。

但**这不是孤立的两处**，而是两套并行的约定：
- **MD/报表侧**：ZH 输出里 "system prompt" / "compaction" **一贯当作外来术语不译**——`story_render.go` 的 `"> ⚙️ **system prompt 变更**"`、`"全程共出现 N 版不同的 system prompt"`、`story_llm.go` 的 `"以下是在 Compaction 中被丢弃"`、`story_findings.go` 的 `"疑似 compaction 丢失了约束文本"` 等 ~10 处都是这样。
- **HTML 侧**：`story_html.go` 译成「系统提示词」「上下文压缩」，`story_compare_html.go` 译成「系统提示词」。

所以 MD 侧的 `## System Prompt` 标题与它正下方的正文「全程共出现 N 版不同的 system prompt」是**自洽的**（都用外来词）。只把标题译成中文，反而制造标题/正文不一致。

**方案评估**：C.4 「统一全译为『上下文压缩』『系统提示词』」——要动 `story_render`、`story_compare`、`story_llm`、`story_findings`、`reqdetail_detail`、`report_compaction`、`report_tokens` ~15 处字符串，外加 `llmCompactionConstraintSystemPromptZH` 等**发给 LLM 的 prompt 正文**里也有「上下文压缩（Compaction）」，还要更新 `golden_zh.md`。而且 "compaction" 是不是该译本身可争论（类似 "prompt cache" 通常不译）。`~/.claude/CLAUDE.md` 也倾向「技术术语保留英文」。改动面大、收益纯观感、有术语之争——**ROI 不划算**。

**建议**（三选一，交人工定，倾向 2）：
1. 全译（C.4 方案）：改 ~15 处 + LLM prompt + golden。不推荐。
2. **只统一 MD 侧标题向「外来词」靠拢**：`report_compaction.go:16` → `"§6.7 Compaction 还原 ⭐"` 保持不动（已经是外来词形态），或退一步把「还原」也去掉争议——实际上现状「Compaction 还原」的问题只是中英混排不好看。最小改法：ZH 标题保留 "Compaction" 但整体协调，如 `"§6.7 Compaction 重建 ⭐"`（与 EN "Reconstruction" 对齐）。`story_render.go` 的 `## System Prompt` **不动**（与正文一致）。基本等于「接受现状 + 微调 compaction 标题」。
3. 接受 MD/HTML 两套约定并存，文档里记一句「MD 侧技术术语（system prompt / compaction / prompt cache）默认不译，HTML 侧译」，以后新增字符串照此办。

**Action Plan（若选 3，成本最低）**：
1. 不改代码。
2. 在 `docs/VirtualModelRouter_Design_v4_Analytics.md` 或一个 i18n 约定注释（`internal/i18n/lang.go` 包注释）里写明该约定。
3. `internal/i18n/report_compaction.go:16` 顺手把 "还原" 与 EN "Reconstruction" 对齐为「重建」，纯措辞。
4. 无测试影响（golden_zh 不变）。

---

#### P3-2 — 索引 `sNN` 序号与「N sessions」并存易误读

**复核（勉强成立）**：`vmr-requests.md` 头部 `## Chat User: lobster · 422 sessions …`，而 §6 和 `vmr-requests-lobster.md` 里散落 `s432 (l-…)` 之类别名。`renderSessionRow` 注释说 `s%02d` 是「本报告内 at-a-glance 参照」的别名，`l-<hash8>` 才是真 id。理论上有读者会把 `s432` 和 `422` 当矛盾，但 `sNN` 明显是散落的行标签、不是计数，实际混淆概率不高。

**方案评估**：C.4 「索引头加一句『sNN 是本文档内位置序号，非会话总数』」——一句话脚注，零风险。但价值也很小。

**建议**：可做可不做。若做，加在 `ChatUserLegend`（`vmr-requests-<tag>.md` 的图例行）而非索引头——因为 `sNN` 出现在明细文件里，不在 `vmr-requests.md` 索引里。

**Action Plan（若做）**：
1. **改什么**：`internal/i18n/report_requests.go` 的 `ChatUserLegend`（EN/ZH）。
2. **怎么改**：末尾加「sNN 为本文档内会话顺序别名（按轮数降序），非会话计数。」/ 英文对应。
3. **测**：`requests_test.go` 若有 legend 断言则更新；否则加一条 `strings.Contains`。
4. **验收**：重跑，`vmr-requests-<tag>.md` 图例行出现该句。
5. **文档/收尾**：无 CHANGELOG（纯说明文字）。`.zh` 无关（i18n 内已双语）。

---

#### P3-3 — 见 C.5.1（已修复）

#### P3-4 — `agent` 同名跨 protocol 在同表并列

**复核（基本不成立）**：`sample_reports/macro/en/vmr-report.md`：
- §1「Cache Efficiency by Model」：`| agent | openai-completions | …` 与 `| agent | anthropic-messages | …` 两行，**中间有独立的 Protocol 列**。
- §4 Latency、§5「By Virtual Model」、§2「Estimated Cost by Model」同样都有 Protocol 列。

读者要区分只需看 Protocol 列。C.4 「在 model 名后加协议后缀」会与 Protocol 列**信息重复**。

**方案评估**：C.4 两个方案里，「加后缀」冗余；「注明同名不同 protocol 分列」是个脚注，价值极低（Protocol 列已经在说这件事）。

**建议**：**不改**。若人工 review 认为确有困惑，最多在 §5「By Virtual Model」表下加半句脚注「同名 virtual model 按 protocol 分行（vmr 三条 ingress 协议互不翻译）」。

**Action Plan（若坚持加脚注）**：
1. `internal/i18n/report_workload.go` 加 `ByVirtualModelNote string`（EN/ZH）。
2. `section_workload.go` 的 By Virtual Model 表后渲染。
3. 测试加断言；验收重跑；无 CHANGELOG。

---

#### P3-5 — tool-waste.html 无回链到 vmr-report.md

**复核（成立，但与设计意图相悖）**：`internal/report/toolwaste_html.go` 包注释：「A shareable single-screen artifact」「Self-contained (inline CSS, zero external requests)」「Carries no conversation content」。它有 `Footnote` 和 `GeneratedNote` 两个脚注位，但不指向 `vmr-report.md`。

**方案评估**：C.4 「加一行指向 `vmr-report.md` §7 的链接」——
- 优点：在 reports/ 目录里看的读者能跳到上下文（§7 有完整的 tool-shape-waste 表 + 方法论）。
- 缺点：这张卡片的定位就是「单独发出去分享」的，分享出去后相对链接 `vmr-report.md` 会 404（虽然降级成纯文本 "vmr-report.md §7" 还算有信息）。与「zero external requests / self-contained」的设计基调相悖。

**建议**：**倾向不改**。真要做，只在 `GeneratedNote`（已经是「本页由 vmr analyze 生成」性质的 footer）里追加「完整分析见同目录 vmr-report.md 的 §7」纯文本，不做成 `<a>`（避免分享后死链的观感）。

**Action Plan（若做纯文本提示）**：
1. **改什么**：`internal/i18n/report_toolwaste.go` 的 `GeneratedNote`（EN/ZH），或新增 `ContextHint string`。
2. **怎么改**：`toolwaste_html.go` 的 footer 区，`GeneratedNote` 后再加一个 `<div class="gennote">` 放 `ContextHint`（"Full breakdown: §7 of vmr-report.md in the same folder" / 中文对应）。不用 `<a>`。
3. **测**：`toolwaste_html_test.go` 加 `strings.Contains` 断言。
4. **验收**：重跑 `sample_reports`，`tool-waste.html` footer 出现该句；单独打开 html 不报错。
5. **文档/收尾**：无 CHANGELOG（措辞级）。注意 `toolwaste_html.go` 已接近 archtest 行预算，加代码前先看。

---

### C.5.3 结构性缺口（G-1…G-6）：均为新功能，统一列 backlog

这 6 条都不是「修 bug」，是「加能力」，每条都需要独立的设计讨论（有的还挺大）。这里只给结论 + 若立项的骨架，不展开完整 Action Plan。

#### G-1 — 无「任务是否达成目标」信号
- **成立**。这是设计层面的已知限制：`docs/VirtualModelRouter_Design_v4_Analytics.md` 明说「VMR 零埋点前提意味着结构性拿不到任务是否真正达成目标的信号」，所以 `GroupComparison` 只能拿「耗时」当代理。
- **价值最高、最难**。可探索的弱代理：最后一条 assistant 回复是否含完成确认措辞、是否有 deliverable 落盘（`DeliverableFact` 已经在 compare 里算了）、plan 的 checkbox 是否全 check（`plan_parse.go` 已解析 plan）。这些拼起来能给一个「疑似完成 / 疑似未完成 / 无法判断」的三态弱信号。
- **建议**：作为一个专门的设计任务立项，先写 `docs/` 草案论证「哪些代理信号可靠、假阳性/假阴性代价」。不要仓促上，一个不准的「成功率」比没有更糟。

#### G-2 — 无「客户视角成本 + 月度趋势」
- **成立**。§2 有按客户总估算、§5 有按日活动，但没有 client × 时间桶的二维成本。
- **建议**：与 P2-3（`-compare-period`）一起设计——本质都是「加时间维度」。若单独做，在 §2 或 §5.5 加一个「按客户 × 月」的成本矩阵（受 P3-3 的「未定价日期」问题影响，要带同样的免责脚注）。
- 骨架：`aggregate.go` 的 `byClient` 聚合下再挂一层 `byMonth`；新 i18n + 渲染；差分测试（单月时矩阵退化为 §2 的按客户表）。

#### G-3 — 无「同一用户意图跨会话延续」聚合
- **成立**。会话按 client 分组，同主题跨 session 无自动关联。C.4 提到的 `obster`（`sample_reports` §2/§5 里确实有 `obster` 这个 1 请求的 client_key，是 `lobster` 的拼写错误）属于「同一用户不同 tag」的噪声。
- **建议**：两个子问题分开——
  1. **疑似拼错的 client tag**：低成本、值得做。对 `rep.ByClient` 的 key 两两算编辑距离，距离 1–2 且一方请求数 <<< 另一方时，在 §5.5 或 §0 Highlights 提示「`obster` (1 req) 疑似 `lobster` 的拼写变体」。
  2. **跨会话主题聚类**：需要 title/内容相似度聚类，易误报，成本高。backlog。
- 子问题 1 的 Action Plan：`internal/report` 加 `suspectClientKeyTypos(byClient)` → 返回 `[]{suspect, likely, suspectReqs}`；在 §0 Highlights 或新的一行渲染；i18n；测试（`obster`/`lobster`、`piminni`/`pimini` 等）。

#### G-4 — 无 CSV/Excel 导出
- **成立**。`vmr-report.json` / `vmr-requests.json` 是嵌套 schema，pandas 用户要先 flatten。
- **建议**：值得做，成本可控。`vmr analyze -format csv` 导出几张扁平表：`requests.csv`（= `RequestRow` 直接摊平，已经基本是扁平的）、`cost_by_client.csv`、`cost_by_date.csv`、`sessions.csv`。不做「万能 CSV」，就固定几张分析师最常要的。
- 骨架：`internal/report/export.go` 已有导出骨架，加 `WriteCSVBundle(rep, dir)`；`cmd_analyze.go` 加 `-format` flag（默认 `md,json`，可加 `csv`）；每张表一个 `encoding/csv` writer；测试断言表头 + 行数 + 一行抽样值；UserGuide + `.zh` + CHANGELOG `Added`。

#### G-5 — 调度型 workload 与真实交互混在宏观报表
- **成立**。§0 top-line（`15280 requests`）是全量加总，含 heartbeat（1076）/dream_diary（450）/compaction（27）。想只看真实交互得自己去 §5「By Workload Class」的 `interactive` 行（13727）。
- **建议**：不加 CLI flag（又一个开关），而是**在 §0 Summary 表下加一行「其中 interactive: N 请求 / …」**，让读者一眼看到「排除调度型后的规模」。低成本、直接命中诉求。
- Action Plan：`section_workload.go` / §0 渲染处（`render_doc.go`），§0 表后加一行 `interactive-only` 复述关键指标（请求数、成功率、p95）；数据 `rep` 里已有（`ByWorkloadClass` 或类似）；i18n；测试；无需 flag。

#### G-6 — 生成过程默认应「坐标引用 + 懒生成」而非默认 `-details`
- **不成立 / 已经是这样**。`docs/KNOWN_ISSUES_sonnet-5.md` 已有专门条目：「**默认分析套件不物化 `details/`，`report` 的『文件』列判据是文件存在性而非 `-details` flag**」，且「常驻守卫测试盯着『默认套件 `details/` 为 0、指针是坐标非链接』，人为改回无条件物化当场失败」「这条纪律反复退化过四次，这次靠测试锁死」。C.4 的担心（「默认 `-details` 物化 1.7GB」）与现状不符——**默认就是懒生成**，`-details` 是显式 opt-in。
- **建议**：无需改代码。仅在 `docs/UserGuide.md`（+ `.zh`）里，若「产物清单」处没写清楚「`details/*.md` 默认不生成，脊柱用 `文件:行` 坐标，需要时 `vmr replay -req COORD -print` 或跑 `-details`」，补一句。同时（对应 C.3 那条悬空 bullet）把 `evidence/*.md`（system prompt / tools 哈希去重的证据 blob）也列进产物清单。
- Action Plan：纯文档。改 `docs/UserGuide.md` + `docs/UserGuide.zh.md` 的产物清单小节；无代码、无测试；无 CHANGELOG。

---

### C.5.4 收尾说明

- **已改动文件**（均在 working tree，未 commit）：
  - `internal/i18n/story_corpus.go`、`internal/story/render_corpus.go`、`internal/story/corpus_test.go`（P2-1）
  - `internal/i18n/report_cost.go`、`internal/report/section_cost.go`、`internal/report/section_cost_test.go`（P3-3）
- **验证**：`go build ./...`、`go vet ./internal/...`、`go test ./...`、`go test ./internal/archtest/...` 全绿；`gofmt -l internal/` 干净。
- **未动**：`CHANGELOG.md`（两处均为分析产物的免责/澄清脚注，非用户可感知的行为变更，是否入 changelog 由 review 定）；`sample_reports/`（gitignore 的本地快照，重跑刷新）；所有设计文档（本次两处小改不涉及设计层描述）。
- **给人工 review 的优先级建议**：先看 C.5.1 两处已改（确认措辞与判断）→ 再看 P0-1 / P0-2（收益最大、需产品决策）→ P1-3（compare 合并，界限清楚）→ 其余按 ROI 表。G-1…G-5 需要各自立项，不适合夹在本轮里做。
- **若 C.5.1 两处修复被接受，建议同时在 `docs/KNOWN_ISSUES_sonnet-5.md` 登记两条更深层的未修问题**（本轮只加了脚注，没动根因）：
  1. **`computeTimeSplit` 的单间隙归因无上限**：任意长的「上一 Step 响应 → 下一 Step 非人类发起请求」间隙都整段计入 `AgentExecMS`，跨周 lineage 上可产生 36 天量级的「Agent 执行时间」，污染 corpus 均值。可能修法：对单间隙设上限（如 >1h 归 idle/unknown 而非 agent 执行），但需改指标语义 + 更新设计文档时间拆分定义 + 差分测试。
  2. **按日成本表的覆盖度隐性依赖当日上游端点是否在定价表内**：`cliproxy`（1510 请求）、`bai`（1348 请求）等高流量端点未定价，导致整块日期从 §2 按日成本表消失。除本轮脚注外，真正的缓解是补齐这些 provider 的定价（`internal/pricing` supplement 或 config override），或在 §2.5 显式列出「未定价 provider 及其占比」。

---

### C.5.5 第二批已实施（Sonnet 5 · 2026-08-31，用户点名 P0-1 / P0-2 / P1-2 / P3-1）

> 均在 working tree，未 commit。`go build ./...` / `go test ./...`（35 包）/ `go vet` / `gofmt -l internal/` / `archtest` 全绿。

#### ✅ P0-1 — §6 会话长尾折叠（轻量方案）

**做法**：不删 §6、不加 CLI flag、不动 compaction chain 图。每个 client 的会话表按轮数降序后，`splitSessionLongTail` 把「排在 `sessionsHeadRows`(20) 之后、且轮数 ≤ `sessionsLongTailTurnCap`(12) 的短会话」折进一个 `<details>`；任何 > 12 轮的会话即使排位靠后也留在 head（fold 永远不吞实质会话）。小 client（≤20 会话）完全不受影响。

**改动**：
- `internal/i18n/report_sessions.go`：`SessionsText` 加 `LongTailOpen func(n, turnCap int) string` + `LongTailClose string`（EN/ZH）。
- `internal/report/section_sessions.go`：`renderSessions` 每 client 循环体拆 head/tail 渲染；新增常量 `sessionsHeadRows` / `sessionsLongTailTurnCap` 和纯函数 `splitSessionLongTail`。
- `internal/report/section_sessions_test.go`（新建）：短尾折叠 / 大会话不折叠 / 小 client 全内联，外加 `TestRenderSessions_CompactionChainSurvivesFold`——3 节点 compaction 链的 mermaid 必须仍渲染在最后一个 `</details>` 之后（锁死 fold 不吞 `renderCompactionChains`）。

**预期效果**：full corpus 下 §6 从 ~1122 行降到 ~300–400 行、占比从 45% 降到 ~20%；所有实质会话仍可见。

**review 关注点**：K=20 / N=12 阈值是否合适（可调）；`<details>` 里放 GFM 表格在你常用的 Markdown 渲染器里是否正常折叠/展开。

**收尾待办（review 通过后）**：`CHANGELOG.md` `[Unreleased] → Changed`；`docs/UserGuide.md` + `.zh` 的 §6 描述加一句「长尾折叠」（当前两文件对 §6 的描述未提完整性，改动小）。

#### ✅ P0-2 — 删除 `vmr-requests.md` 末尾全量平铺表（方案 C）

**做法**：`WriteRequestsIndex` 不再调用 `writeAllRequestsFooter`，删掉尾部的 `---` 分隔线。`vmr-requests.md` 回归纯索引（每组一个 `## 标题` + 摘要 blockquote + 指向 sibling 的链接，几十行）。全量时序数据在 `vmr-requests.json`（`WriteRequestsJSON` 写全部行，不变）；错误时序在 `vmr-requests-failed.md`；逐组明细在各 `vmr-requests-<tag>.md` / `-unresolved.md`。

**改动**：
- `internal/report/requests.go`：删 `writeAllRequestsFooter` 函数 + 调用；更新 `WriteRequestsIndex` 文档注释、`partitionGroups` 注释（去掉已失效的 `Markdown()` 提法）、`buildDetailFileSet` 注释（去掉 "all-requests footer"）。
- `internal/i18n/report_requests.go`：删 `AllRequestsTitle` / `AllRequestsTableHeader` 字段 + EN/ZH 两处字面量；`FailedIndexIntro`（EN/ZH）措辞从「在 vmr-requests.md 及其分组 sibling 里照常出现」改为「在各分组明细文件里照常出现」（平铺表没了，失败行不再出现在 `vmr-requests.md` 本体）。
- `internal/report/aggregate_test.go`：
  - `TestWriteRequestsIndexGrouping`：DisplayZone 转换断言从主索引 `s` 移到 `alice` sibling（主索引已无逐请求时间戳）。
  - `TestWriteFailedIndex`：「主视图未被搬走」断言从读 `vmr-requests.md` 改为读 `vmr-requests-unresolved.md`，断言 `❌transient` / `❌canceled` / `⚠️trunc` 仍在逐组明细里（证明 `WriteFailedIndex` 是加法不是搬移）。

**预期效果**：`vmr-requests.md` 从 ~15,359 行 / 2.8MB 降到 ~几十行；`vmr-requests.json` 不变。

**review 关注点**：确认你不依赖「在一个 md 文件里跨 client 按时间滚一遍所有请求」这个视图（设计注释原话称它是「唯一跨组时序视图该在的地方」——本次判断是 15K 行 md 无人真的滚，脚本用 JSON 更合适）。若确实要保留人类可读的跨组时序，退回 C.5.2 P0-2 的方案 A（只留最近 N 行 + 指向 JSON）。

**收尾**（已随 commit 落地，见 C.5.6）：`CHANGELOG.md`；`docs/UserGuide.md` + `.zh`（两文件确实各有一句描述末尾的「全部请求（时间序）」全量表，已改为指向 JSON）。`docs/VirtualModelRouter_Design_v4_Analytics.md` 原文已写 `vmr-requests.md` 是「纯索引」、从未提平铺表，**无需改**。

#### ✅ P1-2 — HTML 看板 Task/Step 缩进（不折叠）

**复核修正**：C.4 说「无按 Task 的视觉分组」不准——`htmlStructure` 早已把每个 task 包在 `<div class="task"><h3>…</h3>…</div>` 里。真实缺陷是 **Step (`.srow`) 与 Task 标题左边缘齐平，没有缩进**，长 journey 下扫起来分不清层级。

**做法**：纯 CSS（`internal/story/render_html_assets.go` 的 `htmlStyle`）。给 `.task` 加 `padding-left: 16px` + `border-left: 2px solid var(--rule)`（左侧 rail），`.task > h3` 加 `margin-left: -18px` 把标题拉到 rail 外缘。效果：Task 标题坐在 rail 上，其下所有 Step 统一缩进 16px，长时间线里一眼能看出 Task 边界。redact 版共用同一 DOM/CSS，一并生效；compare HTML 不含 `.task`，不受影响。

**改动**：`internal/story/render_html_assets.go` 3 行 CSS。无新测试（现有 `render_html_test.go` 断言 `.task[id]` scroll-spy 选择器不受影响；story 全套测试通过）。

**review 关注点**：在你的浏览器里目测一眼 incident 版（378 步）——rail + 缩进是否清晰、`.srow` 自身的 3px 左边框与 `.task` 的 2px rail 并置是否显得杂乱（我判断不会，二者在不同 x 位置）。若要更强分隔可再加 Task 折叠（C.5.2 P1-2 的 Action Plan），但那会牺牲「一屏扫完时间线」的概览价值。

**收尾待办**：无（HTML 看板 CSS 细节不在设计文档里逐条描述，无 CHANGELOG 必要——可选加一条 `Changed`）。

#### ✅ P3-1 — zh 术语（方案 2：接受现状 + 登记）

**复核修正**：P3-1 的「两处不统一」前提偏弱。核对 i18n 源码：报表侧 ZH 章节名是一套**既定惯例**——「保留英文特性名 + 中文描述词」：`§6.5 Sticky 有效性`、`§2.5 账户（Provider）消耗与额度`，`§6.7 Compaction 还原` 完全遵循同一模式，不是 bug。journey 叙事里 `## System Prompt` 与其正文（`> ⚙️ system prompt 变更`、`全程共出现 N 版不同的 system prompt`）也一致用外来词。真正的「不统一」只在 MD（留英文特性名）vs HTML（全译 `系统提示词`/`上下文压缩`）之间，两套各自自洽。

**做法**：**不改代码**。全量统一要动约 15 处 i18n 字符串 + 发给 LLM 的 prompt 正文 + `UserGuide.zh.md` / Analytics 设计文档里的既有章节名（`§6.7 Compaction 还原` 在这两份文档里都是权威章节名），收益纯观感、还牵出「Compaction 该不该译」之争，不成比例。改为在 `docs/KNOWN_ISSUES_sonnet-5.md` §2.5 登记为刻意非-fix（含触发条件：同一 section 内自相矛盾才局部收敛；新增字符串跟随同 section 已有形态），避免被反复重新提出。

**改动**：`docs/KNOWN_ISSUES_sonnet-5.md` §2.5 加一条。无代码、无测试。

---

### C.5.6 汇总：本文件相关的全部改动

> 用户复核后指示：处理反馈 → 全面复核 → 无问题即 commit + 合并 main + 删分支。以下已随 commit 落地。

**代码 / i18n**：
- P2-1：`internal/i18n/story_corpus.go`、`internal/story/render_corpus.go`、`internal/story/corpus_test.go`
- P3-3：`internal/i18n/report_cost.go`、`internal/report/section_cost.go`、`internal/report/section_cost_test.go`
- P0-1：`internal/i18n/report_sessions.go`、`internal/report/section_sessions.go`、`internal/report/section_sessions_test.go`（新建，含 `TestRenderSessions_CompactionChainSurvivesFold`——回应复核反馈 #2）
- P0-2：`internal/report/requests.go`、`internal/i18n/report_requests.go`、`internal/report/aggregate_test.go`
- P1-2：`internal/story/render_html_assets.go`

**文档**：
- `CHANGELOG.md` `[Unreleased] → Changed`：P0-1、P0-2 各一条；P1-2 一条；P2-1/P3-3 合并一条。
- `docs/UserGuide.md` + `docs/UserGuide.zh.md`（`.zh` 同步硬规则）：§6 长尾折叠一句；`vmr-requests.md` 产物描述去掉「全部请求（时间序）」全量表、改为指向 `vmr-requests.json` / `vmr-requests-failed.md`。
- `docs/VirtualModelRouter_Design_v4_Analytics.md`：**无需改**——该文档 §「按请求下钻」原文已写 `vmr-requests.md` 是「一份纯索引」，从未描述过全量平铺表。
- `docs/KNOWN_ISSUES_sonnet-5.md`：§2.5 加 P3-1 的刻意非-fix 条目。
- `docs/ANALYZE_REPORTS_GENERATION_PLAN.md`：本 C.5 全节（分析记录，随 commit 保留）。

**验证**：`go build ./...` ✅｜`go test ./...`（35 包全过，含 `-race` 复跑 report/story/archtest）✅｜`go vet ./...` ✅｜`gofmt -l internal/ cmd/` 干净 ✅｜`go test ./internal/archtest/...`（文件/函数行预算 + 文档引用完整性）✅

**仍未做（按计划留给后续，非本轮范围）**：
1. `sample_reports/` 重跑刷新（gitignore 的本地快照，不阻塞、不进版本库）。
2. C.5.2 里判为「保留待人工 review」的其余条目（P0-3 / P1-1 / P1-3 / P2-2 / P2-3 / P3-2 / P3-4 / P3-5 / G-1…G-6）——各自的 Action Plan 已在 C.5.2/C.5.3，需要时按那里执行。
3. C.5.4 末尾的两条更深层未修问题（`computeTimeSplit` 单间隙无上限、按日成本表覆盖度依赖 provider 定价）——本轮 P2-1/P3-3 只加了诚实脚注，没动根因；若要根治按 C.5.4 登记进 KNOWN_ISSUES 并排期。
