// Ver 2026-08-21, by Sonnet 5

# P14+P15 · 索引渲染范围一致化 + CLI 入口收敛收尾 — ActionPlan

> 依据：`story_report_full_review_opus-5.md` 第 6 章「后续开发计划（P11–P15）」§6.5 对 P14/P15 的
> 定义（目标、范围、任务表、阶段验收），以及该文 §2.4/§2.9/§2.10、Package A6/C/E、
> `KNOWN_ISSUES_sonnet-5.md` §1.38/§1.42/§1.43 的具体依据。P13 已完成（P14 的排期约束已满足，见
> DevPlan §6.4 2026-08-21 订正）。
>
> **合批决策（2026-08-21）**：P14 与 P15 之间没有硬依赖，且都只改 `cmd/vmr` 这一薄层，合并为一个
> 执行批次，理由与记录见 DevPlan §6.4 的订正段落。批内顺序：**先 P14 后 P15**——P14.1 改
> `cmd_analyze.go` 的候选过滤判据，P15.1 给同一个文件加两个新模式，两者错开改能避免同一文件的
> 两次改动互相踩脚（DevPlan §6.4 新增的第四条"硬依赖"）。
>
> **用户拍板记录（2026-08-21，AskUserQuestion 三问）**：
> 1. P14.1 采用推荐方案——统一判据，只把 heartbeat 归为噪声，cron/subagent 与 task 同等对待
>    （默认渲染候选数从约 238 条增至约 370 条）。
> 2. P14.2 采用"报告头部一行注记"——在披露检测器覆盖率的位置加一行说明，不新增独立章节。
> 3. P15.1 采用"两个独立布尔开关"——`-macro-only`、`-list-only`，与现有 `-corpus`/`-render-all`/
>    `-details` 等离散布尔风格一致，不引入新的 `-scope` 枚举参数。
>
> 本计划不沿用上述文档对任务范围的字面判定——按 DevPlan §6.1 的约定，**开工前基于当前真实仓库状态
> 重新核实**。下面 §0 记录了这次核实的结果与发现。

---

## 0. 开工前重新核实：与原计划的出入

逐项核实了 DevPlan §6.5 对 P14/P15 的判定所依赖的源码位置：`internal/story/storyindex.go`、
`internal/story/candidates.go`、`internal/story/findings_toolresult.go`、`internal/story/corpus.go`、
`internal/story/render_corpus.go`、`internal/chatmsg/toolresults.go`、`cmd/vmr/cmd_analyze.go`、
`cmd/vmr/cmd_story.go`、`cmd/vmr/cmd_story_setup.go`、`cmd/vmr/cmd_report.go`，以及
`KNOWN_ISSUES_sonnet-5.md §1.38/§1.42/§1.43`、P13 ActionPlan 结语。**核实结果：DevPlan 对
P14/P15 的判定与当前代码基本一致，任务表本身不需要改字**；以下是核实中确认/细化，以及新发现的执行
细节。

1. **P14 的前置条件确认已满足**——`taskOnlyCandidates`（`cmd_analyze.go:36`）与
   `storyindex.go:251` 的折叠判据（`CategoryHeartbeat || CategorySubagent`）与复查时描述完全一致，
   没有代码漂移。P13 ActionPlan 结语已确认"默认渲染量从 238 条抬到约 370 条不再是给已知体积问题
   加码"，`KNOWN_ISSUES §1.42` 的排期约束已登记为"已满足"。

2. **新发现：i18n 文案本身已经与代码行为不一致，且方向和本次要改的一样。**
   `internal/i18n/story_index.go` 的 `NoiseFoldSummary`（EN："Scheduled/heartbeat/subagent
   journeys"；ZH："定时/心跳/子代理任务"）与其 doc comment（"the collapsed heartbeat/cron/subagent
   block"）都**声称 cron 也被折叠**，但 `storyindex.go:251` 的实际判据从未包含
   `CategoryCron`——这是复查阶段没有发现的第三处矛盾（前两处是"索引显示只折叠
   heartbeat+subagent"与"套件渲染只渲染 task"的相反判定）。P14.1 把折叠范围收窄到只剩
   heartbeat 后，这处文案自然改为准确（只提 heartbeat），不需要额外处理成本。

3. **P14.1 的实现落点比 DevPlan 原文更精确**：DevPlan 只说"两条分类线合并成一条判据"，未点名
   在哪里放这条判据。核实后，`JourneyCategory` 常量与 `classifyJourney` 都在 `internal/story`
   包（`storyindex.go`/`candidates.go`），而 `cmd_analyze.go` 的 `taskOnlyCandidates` 只是读
   `su.freshRows[i].Category`（已经是 `story.JourneyCategory` 类型）。**判据应作为
   `internal/story` 的导出函数**（如 `IsNoiseCategory(cat JourneyCategory) bool`），
   `storyindex.go` 与 `cmd_analyze.go` 各自调用同一个函数，而不是各自维护一份枚举值列表——这样
   "只有 heartbeat 是噪声"这条规则只有一处定义，未来再调整只改一处。

4. **P14.1 需要同步改名 `taskOnlyCandidates`**：函数改成"排除噪声"而非"只留 task"后，原名字
   自身就是误导性文档——按 `CLAUDE.md` 的注释纪律处理，改名为 `renderableCandidates`（唯一调用点
   `cmd_analyze.go:236`，无其他引用，改名是纯本地操作，`grep` 已确认）。

5. **P14.2 的落点核实：`internal/story/corpus.go` 的 `FindingRate` map 对零命中的
   FindingCode 是"整个 key 都不出现"，不是"值为 0"**（`corpus.go:230-247`，`hitByCode` 只在
   `ComputeFindings` 实际返回该 Code 时才建 key）——这正是 §2.10 说的"读者无法区分'查过没问题'和
   '测不出来'"的根本原因，比"显示 0%"更彻底地不可见。而 `MetricDist`（`error_recovery_count`
   等）**不受此影响**——`metricSpecs` 无条件遍历全部 journey（`corpus.go:220-228`），
   `error_recovery_count` 的分布会照常出现，只是值全是 0——这是另一种误导（"显式的 0"而非
   "缺席"），但和 Finding 那一半是**同一个根因**（`chatmsg.ToolResult.IsError` 只在 Anthropic
   协议下有意义），值得在同一行注记里一并说明，不需要为 Metric 单独再起一段。

6. **P14.2 的数据来源核实**：`ctxgraph.Manifest.Protocol`（`manifest.go:36`，来自
   `rec.Protocol`）在每个 `Step.Manifest` 上都有——`ComputeCorpusStats(journeys []*Journey)`
   可以直接遍历 `j.Steps[i].Manifest.Protocol` 算协议分布，不需要重新解析任何东西，满足 §5.6
   "零推断"的纪律。

7. **P14.2 的呈现范围收窄，理由记录在此**：review 原文"在 Findings 章节标注"字面上可以理解成
   每条 journey 报告都加一行，但协议占比是"本批语料"的统计量，单条 journey 自己的协议分布对读者
   没有意义（可能就是 1-2 个协议样本）。**本计划把披露收窄到 `-corpus` 报告（`render_corpus.go`
   的 `RenderCorpusMarkdown`）一处**——这是唯一已经在做"本批数据"聚合统计（Finding hit rate、
   Metric 分布）的位置，与用户选定的"报告头部一行注记"完全吻合，且改动面最小（1-2 个文件）。
   不改 `render_md.go`（单条 journey 报告）。

8. **P15.1 的两个新模式与 `dispatchAnalyze` 现有结构的接口点核实**：
   - `-list-only` 对应 `cmdStory` 的"无 selector"分支（`cmd_story.go:161`
     `return listJourneys(su.idx, su.g, outDir, includePartial, lang)`）——`listJourneys`
     本身已经是独立函数，接口签名 `(idx, g, outDir, includePartial, lang)` 全部来自
     `setupStoryRun` 的返回值与 `analyzeRun` 已有字段，**可以原样复用，不需要改签名**。
   - `-macro-only` 对应 `cmdReport` 的整个行为——但 `cmdReport` **从不调用 `setupStoryRun`**
     （`cmd_report.go` 全文 `grep setupStoryRun` 零命中）。这意味着 `-macro-only` 必须在
     `dispatchAnalyze` 里**跳过 `setupStoryRun` 这一步**，而不是跑了 setup 再跳过渲染——否则
     `-macro-only` 会比真正的 `vmr report` 多做一次全量扫描/拼接，且可能在 `{outDir}/stories/`
     或 `.parse-cache` 留下 `vmr report` 从来不留的副作用，违反"产物逐字节相同"的验收标准。
     `dispatchAnalyze` 需要在调用 `setupStoryRun` **之前**先分支掉 `-macro-only`。

9. **P15.2 别名薄化的设计选择，与 DevPlan 原文字面表述不同，记录理由**：DevPlan/`KNOWN_ISSUES
   §1.38` 的原话是"解析 args → 翻译为 analyze 等价 args → 调 `cmdAnalyze`"，字面上暗示把已解析的
   值重新序列化成 `[]string` 再喂给 `cmdAnalyze` 走一次 `flag.Parse`。核实后**本计划改用更直接的
   方式**：`cmdReport`/`cmdStory` 保留各自的 `flag.FlagSet`（沿用今天已验证过的 flag 定义与
   `resolveXxx` 语义，不重新发明），解析完之后直接构造一个 `*analyzeRun` 并调用
   `dispatchAnalyze(r)`——跳过"值 → 字符串 → 再解析回值"这一趟无意义的序列化/反序列化，也避免
   一个真实风险：`-currency`/`-report-config` 这类字符串值如果原样拼进 `[]string` 由
   `dispatchAnalyze` 内部再转发，遇到路径包含空格等边界情况需要额外做 shell 风格转义，纯属为了
   "看起来在传 flag 字符串"而引入的新故障面。**效果与"翻译为等价 args"完全一致**（两个别名的
   产出仍然逐字节不变，分派逻辑仍然只有 `dispatchAnalyze` 一份），只是"翻译"发生在 Go 结构体
   层，不是字符串层——这是 P9.1 已经用过的同一个 `analyzeRun` 结构体，不是新发明。DevPlan 6.7
   允许"范围缩减、顺序调整"，这属于同一类"实现路径选择"，不改变任务的验收标准（产物逐字节相同、
   分派逻辑全仓只剩一份）。

10. **P15.3（`-llm-key` 不对称）核实**：`cmd_report.go` 当前完全没有 `-llm-key` flag，
    `excludeClientTags = selfTrafficExcludeTags(rc.LLMKey, rc.SelfTrafficClientTags)`
    （`cmd_report.go:445`）只读 `report.yaml`。按 §9 的合并设计，给 `cmdReport` 加
    `-llm-key` flag 并塞进 `analyzeRun.llmKey`，这条不对称随 P15.2 的重构一起消失，不需要
    单独再起一个任务——**并入 P15.2 一并交付**，DevPlan 里作为独立小节列出是文档粒度，不代表
    要单独提交。

11. **P15.4 措辞修正核实**：`cmd_report.go:456`、`cmd_story.go:119` 的 stderr 提示与
    `cmd_report.go:449-455` 的解释性注释都需要跟着 P15.1（`-macro-only`/`-list-only` 出现）
    重写——注释里"vmr analyze 没有纯报表模式"这条前提在 P15.1 之后不再成立，提示文案与注释一起改，
    不是两次改动。

**结论**：DevPlan 第 6 章 P14/P15 小节的任务表与验收标准不需要改字；本 ActionPlan 在 §0.2–§0.11
记录的是任务表本身没写明、执行前必须先判断的实现细节（判据落点、呈现范围、别名薄化的具体机制），
据此展开下面的执行步骤。

---

## 1. 目标与范围（继承自 DevPlan §6.5，P14+P15 合并）

**P14 目标**：消除"索引把一类候选推到首屏、套件却不给它任何可点的东西"这个自相矛盾；同时让读者
能分辨"检查过没问题"与"检测器结构性沉默"。

**P15 目标**：让"别名"这个词名副其实——`vmr report`/`vmr story` 的每一种既有用法都能由
`vmr analyze` 表达，别名退化为纯转发，两份手写分派合并为一份。

**范围**：
- `internal/story`：候选类别的显示折叠判据统一为一个导出函数（P14.1）；语料协议分布 + Finding/
  Metric 覆盖率披露，收窄到 `-corpus` 报告（P14.2）。
- `cmd/vmr`：`cmd_analyze.go` 的候选过滤判据改用统一判据，新增 `-macro-only`/`-list-only`
  两个模式（P14.1、P15.1）；`cmd_report.go`/`cmd_story.go` 的分派逻辑清空，改为构造 `analyzeRun`
  转发给 `dispatchAnalyze`（P15.2/P15.3）；两处迁移提示与相邻注释改写（P15.4）。
- `internal/i18n`：`story_index.go` 的 `NoiseFoldSummary` 措辞订正（P14.1 的必然结果）；
  `story_corpus.go`（或其归属文件）新增覆盖率披露文案（P14.2）。
- 文档：`CHANGELOG.md`、`docs/UserGuide.md`+`.zh`（新增两个 flag 的说明）、
  `docs/KNOWN_ISSUES_sonnet-5.md`（§1.38/§1.42/§1.43 状态收尾）。

**不涉及**：分类器本身（`classifyJourney` 的 `[cron:]`/`[heartbeat]`/`[Subagent Context]`
前缀判据不变，§2.9 已论证不引入新猜测）；`internal/report`/`internal/story` 的渲染/聚合逻辑
（P15 明确排除）；OpenAI 工具返回内容嗅探（§2.10 已用 495,672 条实测否决）。

---

## 2. 任务清单

### P14.1 两条分类线合并为一条判据

**改动**：

1. `internal/story/storyindex.go`（`JourneyCategory` 常量块附近）新增：
   ```go
   // IsNoiseCategory reports whether cat should be folded out of the
   // primary index view and excluded from the default suite's render
   // scope — the single judgment both RenderStoryIndexMarkdown's display
   // split and cmd/vmr's default-suite render scope must share (P14.1;
   // before this they answered differently for CategoryCron and
   // CategorySubagent — KNOWN_ISSUES §1.42). Real-corpus measurement
   // (story_report_full_review_opus-5.md §2.9, M12) is the only evidence
   // this classification is built on: heartbeat topped out at 7 requests
   // per candidate across 107 candidates (0 ever reached 10); cron and
   // subagent both had double-digit-request candidates, including the
   // single largest journey in the corpus (subagent, 91 requests) — so
   // only heartbeat qualifies as noise.
   func IsNoiseCategory(cat JourneyCategory) bool {
       return cat == CategoryHeartbeat
   }
   ```
2. `RenderStoryIndexMarkdown`（`storyindex.go:251`）：
   `if r.Category == CategoryHeartbeat || r.Category == CategorySubagent {` →
   `if IsNoiseCategory(r.Category) {`。函数上方的 doc comment（"heartbeat/subagent are
   structural noise...over half a typical candidate list"）同步改为反映新判据与新实测比例
   （heartbeat 单独占比，引用 M12 数字）。
3. `cmd/vmr/cmd_analyze.go`：
   - `taskOnlyCandidates` 改名为 `renderableCandidates`，判据从
     `su.freshRows[i].Category == story.CategoryTask` 改为
     `!story.IsNoiseCategory(su.freshRows[i].Category)`；doc comment 同步改写（不再是
     "filters down to CategoryTask rows"，而是"excludes noise-category rows per
     P14.1"）。
   - `dispatchAnalyze` 默认分支里 `scope = taskOnlyCandidates(su)` 改为
     `scope = renderableCandidates(su)`；其上方引用 P9.2/"category=task scope"的注释一并
     改写为准确描述（P14.1 之后不再是"只渲染 task"，而是"渲染除心跳外的全部候选"）。
   - `-render-all` flag 的 help 文案（`"default: task-only — cron/heartbeat/subagent
     candidates still appear..."`）改为准确描述新默认范围（"default: excludes only
     heartbeat candidates"）。
4. `internal/i18n/story_index.go`：`NoiseFoldSummary` 的 EN/ZH 文案（"Scheduled/heartbeat/
   subagent" → "Heartbeat"；"定时/心跳/子代理" → "心跳"），doc comment 同步改（不再提 cron/
   subagent）。

**验收**：默认 `vmr analyze` 的索引首屏（`vmr-stories.md`）里 cron/subagent 候选都在主表、都有
可点的 `Rendered` 链接；只有 heartbeat 候选进折叠块。真实语料实测默认渲染候选数与请求数相对 P13
基线的增量（预期候选数从约 238 条增至约 370 条，量级见 §0.1）。

---

### P14.2 检测器覆盖率披露（收窄到 `-corpus` 报告）

**改动**：

1. `internal/story/corpus.go`：
   - `CorpusStats` 新增字段，如 `ProtocolShare map[string]float64`（协议 → 占比，分母是
     全部非 partial candidate journey 的 Step 总数，不是请求数——与 Metrics/Finding 的统计
     单位一致，避免引入第二套分母）。
   - `ComputeCorpusStats` 遍历 `journeys[i].Steps[j].Manifest.Protocol` 累计计数，算出
     `ProtocolShare`。
   - 新增一个包级常量/列表，登记"依赖 Anthropic-only 字段"的 FindingCode 与 MetricCode——
     目前只有 `FindingUnadaptedRetry`（Finding）与 `MetricErrorRecoveryCount`（Metric），
     `chatmsg.ToolResult.IsError` 的唯二读者（`grep -n "\.IsError" internal/story/*.go`
     已核实，`llm_single.go` 的 `isCandidate` 是 LLM 证据包筛选启发式，不是读者可见的检测器/
     指标，不纳入披露范围）。
2. `internal/story/render_corpus.go`：`RenderCorpusMarkdown` 在 Finding Rate 表格（`t.
   FindingRateTitle`）之前插入一行注记——**只在 `ProtocolShare["anthropic"]` 为 0（或低于一个
   小阈值，如 1%）时才输出**，避免在一个本来就有 Anthropic 流量的语料上写一句无意义的免责声明。
   注记内容：本批语料的协议占比（如"openai 99.5% / anthropic 0.5%"）+ 哪些 Finding/Metric
   在 openai-only 数据上结构性无法触发及原因（一句话，指向 `chatmsg.ToolResult.IsError` 字段
   注释里已有的解释，不重复论证）。
3. `internal/i18n/story_corpus.go`（或其实际归属文件，执行时以 `grep -n
   FindingRateTitle internal/i18n/*.go` 定位）新增 EN/ZH 两条文案。

**验收**：一份 openai-only 语料（真实语料 M14 已实测 99.48%）的 `-corpus` 报告里出现协议占比与
覆盖率注记；一份协议混合正常的语料（or 合成 fixture）不出现该行（阈值判断生效，不引入噪声）。
不改变 `FindingRate`/`MetricDist` 的既有计算逻辑与既有测试的数值预期。

---

### P15.1 `vmr analyze` 补齐两个模式

**改动**（`cmd/vmr/cmd_analyze.go`）：

1. `analyzeRun` 结构体新增两个字段：`macroOnly bool`、`listOnly bool`。
2. `cmdAnalyze` 新增两个 flag：
   - `-macro-only`："default suite only: run just the macro report half (equivalent to
     `vmr report`) — no candidate scan, no journey rendering, no stories/ output"
   - `-list-only`："default suite only: list candidate journeys without rendering any of
     them (equivalent to bare `vmr story`) — writes stories/vmr-stories.{md,json} but no
     journey-*.md"
   两者与 `-journey`/`-compare`/`-corpus`/`-render-all` 互斥（并入现有 `selectorCount`
   互斥校验，或新增一组同级校验——执行时以现有 `hasSelector`/`renderAllFlag` 校验模式为准，
   保持同一种错误提示风格）；两者彼此也互斥。
   `-details`/`-currency` 等 report-half-only flag 与 `-list-only` 同时给出时，按现有项目
   风格（见 `-render-all` 与 selector 互斥的先例）**显式拒绝而非静默忽略**——避免用户以为
   flag 生效了。
3. `dispatchAnalyze` 改动：
   - 在调用 `setupStoryRun` **之前**先处理 `r.macroOnly`（见 §0.8 的理由）：直接构造
     `reportRunOpts` 调 `runReport`，返回。
   - `setupStoryRun` 之后的 `switch` 新增一个 `case r.listOnly:` 分支，调用
     `listJourneys(su.idx, su.g, r.outDir, r.includePartial, r.lang)`。

**验收**：`vmr analyze -macro-only` 与 `vmr report`（同参数、同输入）产物逐字节相同（不写
`stories/`、不留 story 侧 `.parse-cache` 副作用）；`vmr analyze -list-only` 与 `vmr story`
（无 selector、同参数）产物逐字节相同。

---

### P15.2/P15.3 别名退化为纯转发（含 `-llm-key` 对称）

**改动**：

1. `cmd/vmr/cmd_report.go`：`cmdReport` 保留自己的 `flag.FlagSet`（`-c`/`-o`/`-details`/
   `-lang`/`-currency`/`-report-config`/`-include-self-traffic`），**新增 `-llm-key`
   flag**（文案对齐 `cmd_story.go` 已有的同名 flag）。解析完毕后，不再直接调 `runReport`，
   改为构造 `&analyzeRun{macroOnly: true, ...}`（把已解析的 `outDir`/`lang`/`detailsOn`/
   `displayCCY`/`exchangeRate`/`llmKey`/`includeSelfTraffic`/`selfTrafficTags` 等字段填入）
   并调用 `dispatchAnalyze(r)`。
2. `cmd/vmr/cmd_story.go`：`cmdStory` 同样保留自己的 `flag.FlagSet`（flag 定义不变，包括
   `-llm-addr`/`-llm-model`/`-llm-key`/`-llm-cache-dir`/`-llm-dry-run`）。解析完毕后删除
   现有的 `switch`/分支体（`compareJourneys`/`corpusStats`/`resolveJourneySelector`+
   `renderJourney`/`renderJourneys`/`renderAllJourneys`/`listJourneys` 那一段，约 40 行），
   改为构造 `&analyzeRun{corpusFlag: *corpus, compareArg: *compare, journeyArg:
   *journeyArg, renderAllFlag: *renderAll, listOnly: !hasSelector && !*renderAll, ...}`
   并调用 `dispatchAnalyze(r)`。**`resolveLLMOpts` 从"无条件调用"改为
   `analyzeRun.resolveLLMOpts` 的闭包形式**（与 `cmdAnalyze` 一致）——这顺带修掉
   `KNOWN_ISSUES §1.38` 点名的"P9 只在 analyze 侧修过的缺陷在 story 侧原样留着"，不需要
   再单独处理。
3. 两个别名各自的 mutual-exclusion 校验（`-corpus` 与其他互斥、`-llm-addr` 批量拒绝等）如果
   与 `dispatchAnalyze` 内部已有的校验重复，**删除别名侧的重复校验，只保留一份**（在
   `dispatchAnalyze`/`cmdAnalyze` 里）——这是"结构上不可能分叉"的字面要求：重复校验即使内容
   相同，也是分叉的温床。

**验收**：`cmd_story.go`/`cmd_report.go` 里不再出现独立的分派 `switch`；
`TestEnsureJourneyDetails_MatchesReportDetails` 等既有交叉校验测试继续通过；新增/扩展测试覆盖
"`vmr report -llm-key X` 与 `vmr analyze -macro-only -llm-key X` 自指流量排除口径一致"。

---

### P15.4 措辞与文档归位

**改动**：

1. `cmd_report.go:449-456`：重写解释性注释与 stderr 提示——P15.1 之后 `vmr analyze` 已经有
   纯报表模式（`-macro-only`），"Deliberately does NOT say an alias for vmr analyze"这条
   前提不再成立。新提示应准确表达"`vmr report` 现在就是 `vmr analyze -macro-only` 的转发"，
   不再使用"deprecated alias"与"remains fully supported"这两个互相矛盾的措辞。
2. `cmd_story.go:112-119`：同上，改为准确表达"`vmr story`（无 selector）= `vmr analyze
   -list-only`；`-journey`/`-compare`/`-corpus`/`-render-all` 原样转发"。
3. `CHANGELOG.md` `[Unreleased]`：`Added` 补 `-macro-only`/`-list-only`；`Changed` 补
   "cron/subagent 候选默认参与套件渲染"、"vmr report/vmr story 别名内部改为纯转发"。
4. `docs/UserGuide.md`+`.zh`：补充两个新 flag 的说明（若文档列出了 `vmr analyze` 的完整
   flag 表，按现有格式插入一行；若是叙述性说明变焦模式，补一句"macro-only/list-only 对应
   report/story 别名的行为"）。
5. `docs/KNOWN_ISSUES_sonnet-5.md`：`§1.38` 标记为已修复（分派合一）；`§1.42` 标记为已修复
   （统一判据落地）；`§1.43` 标记为已修复（披露落地，收窄范围需在条目里如实写明"只在
   `-corpus` 报告"，不要写成"全部报告"）。`§1.34`（若仍单独存在）随 `-llm-key` 对称一并标记
   已修复或归并进 `§1.38`。

**验收**：`grep -c "deprecated alias"` 两个文件降为 0（或改为准确措辞后不再包含自相矛盾的
表述）；`.zh` 文档与英文版本同批更新，键值结构一致。

---

## 3. 测试

按 DevPlan §6.3 通用完成定义第 8 条（"纪律落成断言"）与 §2.11 的教训（"默认路径实测"），以下每条
都要落成常驻测试，不是验收清单上的一行字：

- **P14.1**：
  - `internal/story/storyindex_test.go` 新增：cron/subagent 行进主表、只有 heartbeat 行进
    折叠块（构造三种 Category 各一行的 fixture，断言折叠块只含 heartbeat 那一行）。
  - `cmd/vmr/cmd_analyze_test.go` 新增（对照现有 `TestCmdAnalyze_DefaultSuiteScopeIsTaskOnly`
    的写法）：cron/subagent 标题的候选在默认套件下也被渲染（`journeyFileNames` 命中），只有
    heartbeat 候选缺席。
- **P14.2**：
  - `internal/story/corpus_test.go`（或新文件）：全 openai 协议的 journeys 输入，
    `ComputeCorpusStats` 的 `ProtocolShare["anthropic"]` 为 0；混合协议输入时占比正确。
  - `internal/story/render_corpus_test.go`：全 openai 输入触发披露行，混合协议输入不触发。
- **P15.1**：
  - `cmd/vmr/cmd_analyze_test.go` 新增：`-macro-only` 产物与直接调 `cmdReport`（同参数）逐字节
    相同；`-list-only` 产物与直接调 `cmdStory`（无 selector，同参数）逐字节相同；互斥校验的
    错误路径（`-macro-only` 与 `-journey` 同时给出应报错）。
- **P15.2/P15.3**：
  - 扩展或新增交叉校验测试（参考 `cmd_story_report_crosscheck_test.go` 的现有模式）：
    `vmr report <args>` 与 `vmr analyze -macro-only <等价 args>` 产物逐字节相同；
    `vmr story <args>`（覆盖无 selector / `-journey` / `-compare` / `-corpus` /
    `-render-all` 五种形态）与对应的 `vmr analyze` 调用产物逐字节相同；
    `-llm-key` 在 `vmr report`/`vmr analyze -macro-only` 两条路径下自指流量排除结果一致。
- **默认路径实测**（DevPlan 通用完成定义第 7 条）：单日语料（`logs/vmr-audit-2026-07-15.jsonl.zst`
  或同规模文件）与全量语料（`logs/` 全部 35 个文件）各跑一次默认 `vmr analyze`，记录
  P14 前后的候选数/渲染数/产物体积对比，以及总耗时（覆盖"扩大渲染范围是否显著拖慢默认路径"这个
  P14.1 决策拍板时提到的风险）。

全部改动通过 `go build ./...`、`go test ./... -race`、`go test ./internal/archtest/...`、
`gofmt -l`、`go vet ./...`。

---

## 4. 阶段验收（对齐 DevPlan §6.3 通用完成定义 + 本期新增两条）

1. 全量测试与架构守卫绿。
2. 真实语料默认路径实测（单日 + 全量），肉眼核对 `vmr-stories.md` 首屏每一行是否可点、`-corpus`
   报告的披露行是否按协议占比正确出现/不出现。
3. 用户可见变化写入 `CHANGELOG.md` 与 `docs/UserGuide.md`+`.zh`。
4. 本阶段取舍登记进 `KNOWN_ISSUES_sonnet-5.md`（§1.38/§1.42/§1.43 状态收尾，§0.2 发现的
   i18n 文案矛盾一并记录为"顺带修复"，不需要单独开条目）。
5. 阶段收尾做边界复核：本阶段是否产生了本文未预见的事实？是否改变了后续阶段的前提？（本期是
   P11–P15 的最后一个阶段，没有 P16，只需回答第一问）
6. **默认路径实测**（新增第 7 条）：已并入上面第 2 条。
7. **纪律落成断言**（新增第 8 条）：P14.1 的"统一判据"、P14.2 的"披露阈值"、P15.1/P15.2 的
   "别名产出不变"全部有常驻测试锁定（见 §3），不依赖人工核对。

---

## 5. 收尾：`KNOWN_ISSUES` 与 DevPlan 更新计划

- `KNOWN_ISSUES_sonnet-5.md`：`§1.38`/`§1.42`/`§1.43` 三条标记已修复，写明修复方式与限定范围
  （P14.2 的披露只在 `-corpus` 报告，不是全部报告——如实登记，不要过度宣称）。
- `story_report_full_review_opus-5.md` §6.4：P14/P15 两行的"状态"列改为
  "✅ 已完成（详见 `story_report_p14_p15_action_plan_sonnet-5.md`）"，与 P11–P13 的既有记法一致。
- 本文件末尾补「6. 执行记录」「7. 总结」两节，记录实际执行范围与开工前判断的出入（若有）。

---

## 6. 执行记录

全部改动通过 `go build ./...`、`go vet ./...`、`gofmt -l .`（无输出）、`go test ./... -race`、
`go test ./internal/archtest/...`，并在真实语料（`logs/` 下 07-15 单日 394 条记录、全量 35 文件
11,374 条记录）上跑过默认路径与关键 flag 组合，逐项见 §6.7。

### 6.1 P14.1 — 统一噪声判据

按计划落地：`internal/story/storyindex.go` 新增 `IsNoiseCategory`（只判 `CategoryHeartbeat`），
`RenderStoryIndexMarkdown` 的折叠判据与 `cmd/vmr/cmd_analyze.go` 的候选过滤（`taskOnlyCandidates`
改名 `renderableCandidates`）改为共用它。§0.2 记录的"i18n 文案已经与代码行为脱节"顺带订正
（`NoiseFoldSummary` 不再提 cron/subagent）。新增测试：
`TestRenderStoryIndexMarkdown_OnlyHeartbeatFolded`、
`TestCmdAnalyze_DefaultSuiteRendersCronAndSubagent`；`TestCmdAnalyze_DefaultSuiteScopeIsTaskOnly`
改名为 `TestCmdAnalyze_DefaultSuiteExcludesHeartbeat`（原名字面意思已不准确）。

### 6.2 P14.2 — 检测器覆盖率披露

首版实现按计划落地（`internal/story/corpus_coverage.go` 新增，`anthropicOnlyCoverage` 登记
`FindingUnadaptedRetry`/`MetricErrorRecoveryCount`，披露收窄到 `-corpus` 报告，1% 断崖阈值）。
**一次并行的独立审阅（gemini-3.7-flash，2026-08-21 22:11，
`story_report_p14_p15_review_gemini-3.7-flash.md`）当场发现两处需要修正的实质问题，均已核实并
修复**，处置记录见 §6.8 第 1/4 条。改动后的最终形态：

- `anthropicOnlyCoverage` 扩为 `Findings`（`FindingUnadaptedRetry`、新增
  `FindingUnverifiedSuccess`）+ `Metrics`（`MetricErrorRecoveryCount`）+ `CorpusSections`（Context
  Rot / Tool Sequence 两个错误率栏目，`-corpus` 独有）+ `JourneySections`（决策脊柱的 ❌ 徽标、
  `structure.json` 的 `ResultError`，单条 journey 独有）——覆盖清单从 2 项扩到 7 项，且第一次
  区分了"corpus 视图独有"与"journey 视图独有"两类自由文本项。
- 披露从 `-corpus` 单点扩展到默认路径本身：`internal/story/render_spine.go` 的
  `renderFindingsSection` 新增 `journeyAnthropicCoverageNote`（该 journey 全部 Step 都非 Anthropic
  协议时触发），`internal/i18n/story_spine.go` 新增对应文案。黄金测试
  （`testdata/golden.md`/`golden_zh.md`）同步更新，diff 只新增一处折叠块，无其它字节差异。
- 1% 断崖阈值整体删除，改为"非 100% Anthropic 即披露"（corpus 层面）/"该 journey 一条 Anthropic
  Step 都没有即披露"（journey 层面）——不再有任何业务可调的阈值常量，只留一个处理浮点误差的
  epsilon。`TestRenderCorpusMarkdown` 的对应子测试重写，新增"98.8% Anthropic 仍触发"
  与"100% Anthropic 不触发"两个边界用例锁定这一行为。

真实语料验证：07-15 单日默认 `vmr analyze` 渲染的 18 份 journey 报告全部正确携带披露注记
（中文措辞，因该目录的 `report.yaml` 配置了 `language: zh`）；`-corpus` 报告同样正确披露且列出
全部 5 项受限信号。

### 6.3 P15.1 — 补齐 analyze 缺失的模式

首版按计划交付 `-macro-only`/`-list-only`（`analyzeRun` 新增字段、`validateAnalyzeModeFlags` 抽出
互斥校验、`dispatchAnalyze` 在 `setupStoryRun` 之前分支掉 `-macro-only`、`-list-only` 并入
`switch`）。**同一次并行独立审阅发现第三个缺口**（处置见 §6.8 第 3 条）：为保住
`vmr story -render-all` 从不触碰宏观报表半区这一 P9 以来的既有行为，首版用了一个只有别名转发器
能设置的 `analyzeRun` 内部字段（`skipMacroReport`），`vmr analyze` 自己的公开 flag 无法达到同一
效果——与"`vmr analyze` 是唯一入口、能力应完全覆盖别名"的架构前提直接冲突。征询用户后（见文档
开头"用户拍板记录"）改为公开的第三个 flag `-story-only`：与 `-macro-only`/`-list-only` 不同，它
刻意**不**与 `-render-all` 互斥而是可以组合（`validateAnalyzeModeFlags` 里单独一条判据）——
`-story-only` 单独使用等价于"默认套件的非噪声范围、只跑 story 半区"，`-story-only -render-all`
是 `vmr story -render-all` 的精确公开等价写法。`cmdAnalyze` 因此三个模式互斥校验略微复杂化，抽出
`validateAnalyzeModeFlags` 独立函数以维持 `archtest` 的函数行数预算。

新增/调整测试：`TestCmdAnalyze_StoryOnly`（单独使用、与 `-render-all` 组合两个子用例）、
`TestCmdStory_RenderAllAlone_NeverWritesReportHalf`（回归测试，锁定 `vmr story -render-all` 不写
report 半区文件，且 story 半区产物与 `vmr analyze -render-all` 逐字节相同）、
`TestCmdAnalyze_RenderAllBare_StillRunsReportHalf`（对照测试，锁定 `vmr analyze -render-all` 自身
的行为不受影响）、`TestCmdAnalyze_MacroOnlyListOnly_MutualExclusion` 扩充 4 个 `-story-only` 互斥
用例。

### 6.4 P15.2/P15.3 — 别名薄化 + `-llm-key` 对称

按计划落地，采用 §0.9 记录的"结构体层翻译"而非"字符串层翻译"：`cmdReport`/`cmdStory` 保留各自
`flag.FlagSet`，解析后构造 `*analyzeRun` 直接调用 `dispatchAnalyze`。`cmdReport` 新增 `-llm-key`
flag（`§1.34` 的不对称随之消失）；`cmdStory` 的 `resolveLLMOptions` 从无条件调用改为
`resolveLLMOpts` 闭包（按需，与 `cmdAnalyze` 同构），`KNOWN_ISSUES §1.38` 点名的"P9 只在 analyze
侧修过的缺陷在 story 侧原样留着"随之自动消失，不需要单独处理。

**执行期间发现并当场修复的一个真实回归**（不是外部审阅发现，是本计划自己的开工中验证发现）：
`cmdStory` 完成薄化后跑既有测试全绿，但手工核对 `vmr story -render-all` 的产物目录时发现多出
`vmr-report.*`/`vmr-requests*` 文件——薄化前它从不写这些文件。根因正是 §6.3 记录的
"`vmr analyze -render-all`（无 selector）与 `vmr story -render-all` 共享 `dispatchAnalyze` 同一个
默认套件分支，前者原本就该跑报表半区"，通过引入 `-story-only`（当时还是内部字段）解决，随后又
因外部审阅指出内部字段本身是架构问题而升级为公开 flag（见 §6.3）。这是本计划里"发现 → 用内部字段
止血 → 外部审阅指出止血方案本身不对 → 升级为公开能力"的完整链条，记录下来避免以后重复走一遍同样
的弯路。

新增测试：`TestCmdAnalyze_MacroOnlyMatchesReport`、`TestCmdAnalyze_ListOnlyMatchesStory`、
`TestCmdStory_MatchesAnalyzeEquivalent`（bare/corpus/journey 三种形态的逐字节对照）、
`TestCmdReport_LLMKeyMatchesAnalyzeMacroOnly`（`-llm-key` 自指流量排除口径在 `vmr report` 与
`vmr analyze -macro-only` 两条路径下一致）。

### 6.5 P15.4 — 措辞与文档归位

`cmd_report.go`/`cmd_story.go` 的 stderr 迁移提示与相邻注释重写，不再使用"deprecated alias"与
"remains fully supported"这两个互相矛盾的措辞——新提示准确表达"是 `vmr analyze <等价 flag>` 的别名，
产物逐字节相同"。`CHANGELOG.md`/`docs/UserGuide.md`+`.zh` 同批更新（`-macro-only`/`-list-only`/
`-story-only` 三个新 flag、cron/subagent 默认渲染、检测器覆盖率披露），详见 §6.7 的具体改动点。
`docs/KNOWN_ISSUES_sonnet-5.md` 的 §1.38/§1.42/§1.43 移入 §3（新增第 39–41 项），ROI 表与分布
统计同步重算——见 `docs/KNOWN_ISSUES_sonnet-5.md` 自身的改动，不在此重复。

### 6.6 测试

§3 规划的全部测试项均已落地（见 §6.1–6.4 各小节列出的具体测试名），另有 §6.8 外部审阅驱动新增的
测试。测试总数：`internal/story` 新增/修改 6 个测试函数（含 3 个子测试组），`cmd/vmr` 新增/修改
9 个测试函数。`gofmt -w` 后全部改动通过 `go build`/`go vet`/`go test -race`/`archtest`。

### 6.7 默认路径实测

- **单日语料**（07-15，394 条记录）：默认 `vmr analyze` 从 P13 基线的 47MB/253 份详单（P13 结语
  记录的数字）到本次 P14 之后的 3.2MB/0 份详单、18/19 个候选可渲染（此前 12 task + 10 cron，
  cron 全部不可点；现在 18 个渲染成功、1 个 heartbeat 折叠，4 个断头 journey 按设计跳过）；
  `-corpus`/单条 journey 报告均正确携带协议覆盖率披露注记。
- **全量语料**（35 文件，11,374 条记录）：默认 `vmr analyze` 耗时约 354s（此前 P9.2 时代的
  SIGKILL 前身耗时 413s，量级相当，且本次是在候选数从约 238 条增至约 370 条的前提下，未见明显
  劣化——P13 的"批量不物化详单"纪律吸收了这部分增量），输出目录 128MB（`stories/` 69MB，无
  `details/` 目录）。相比 P13 结语记录的全量语料数字，体积增长完全来自新增渲染的约 130 条 journey
  报告本身（spine 内容，非详单），符合预期，不是体积纪律退化。
- `-macro-only`/`-list-only`/`-story-only` 三个新模式均在真实语料上跑通并与对应别名逐字节比对
  一致（`generated_at` 时间戳除外）。

### 6.8 外部独立审阅（gemini-3.7-flash）核查与处置

一次并行审阅（`story_report_p14_p15_review_gemini-3.7-flash.md`，2026-08-21 22:11，基线
commit `37ed96b` + 本次 P14/P15 工作区改动）核查了本 ActionPlan 与其执行结果，提出 6 项问题。
逐项核实结果：

1. **【已核实并修复】P14.2 遗漏 3 个受限信号**（`FindingUnverifiedSuccess`、决策脊柱 ❌ 徽标、
   `structure.json` 的 `ResultError`，以及 `-corpus` 独有的 Context Rot/Tool Sequence 两个
   错误率栏目）：审阅指出的根因（`isErrorMarker` 文本标记路径比直接读 `ToolResult.IsError` 更
   广）经 `grep -n isErrorMarker internal/story/*.go` 核实完全成立。**采纳，已扩充
   `anthropicOnlyCoverage`**（见 §6.2）。审阅建议一并纳入的 `llm_findings.go` 证据包构造
   **未采纳**——那是 LLM 判断的弱化证据（有其它信号仍可能触发），不是规则层面的结构性沉默，
   与本条目"性质不同"这一判断经复核成立，本次不作为受限项列入。
2. **【已核实并修复】披露收窄至 `-corpus` 导致默认套件 100% 绕过**：审阅指出的执行路径（默认
   套件从不调用 `corpusStats`）经读 `dispatchAnalyze` 源码确认完全成立——这是本计划 §0.7 自己
   的收窄决定留下的真实盲区，不是审阅的过度解读。**采纳，已实现**：单条 journey 报告新增
   `journeyAnthropicCoverageNote`（见 §6.2），采用审阅建议的方案 1（`renderFindingsSection` 内
   披露），未采纳审阅建议的方案 2（`vmr-stories.md` 表尾统一提示）——单条 journey 报告是读者
   实际会看到疑似问题的地方，索引表尾的提示对应不上具体触发信号，价值不如前者，且会让索引这个
   本来就在做减法的页面（P14.1 刚统一噪声判据）再添一段不针对具体行的说明文字。
3. **【已核实并处置，方案与审阅推荐一致】`skipMacroReport` 内部字段导致别名反超主入口**：审阅
   指出的架构冲突（`vmr analyze` 自身无法达到 `vmr story -render-all` 的效果）经读代码确认
   成立。审阅给出两个方案（暴露为公开 flag / 收敛行为改为始终双半区），**征询用户后采纳方案
   A（暴露为公开 flag，即审阅的推荐方案）**——见 §6.3 的 `-story-only`。
4. **【已核实并修复】`anthropicCoverageThreshold` 断崖缺陷**：1.2% Anthropic 语料会让披露整体
   噤声这一逻辑经手工验证成立（`protocolShare["anthropic"] >= 0.01` 在 1.2% 处为真，直接跳过
   披露）。**采纳，已修复**：改为"非 100% 即披露"（见 §6.2），比审阅建议的"只要存在非 Anthropic
   协议就展示协议构成比例，另加受限项脚注"更彻底——不是从"1% 阈值"改成"另一个阈值"，而是删除
   阈值本身，直接对应"任何非 100% 都意味着部分数据结构性测不出来"这个精确的真命题。
5. **【核实后维持原计划，不单独处理】P14.1 候选扩展后的常驻性能断言**：审阅承认这是"优化建议"
   而非缺陷，且承认 P13 的详单不物化纪律已经吸收了候选数增长带来的磁盘压力。本次 §6.7 已经做了
   一次单日 + 全量两个规模的默认路径实测并记录数字（耗时、体积），满足 DevPlan §6.3 通用完成
   定义第 7 条（"默认路径实测"）的要求；固化为常驻基准测试（例如给全量语料跑一个耗时上限的
   回归断言）是一次独立的测试基础设施投入，与本次候选范围扩展没有直接因果关系，留给
   `KNOWN_ISSUES §1.2`（全内存聚合的记录量上限，已登记"单次分析超约 3 万条记录，或峰值 RSS
   超 4GB"的重估触发条件）统一考虑，不在本次 P14/P15 范围内单独立项。
6. **【已核实，本次执行中已同步完成】CHANGELOG/UserGuide 历史描述滞后**：审阅指出的具体位置
   （`CHANGELOG.md` 仍写"cron/heartbeat/subagent candidates still appear in the index but
   aren't pre-rendered"）在本次 §6.5 的文档同步工作中已经订正，此外还发现并订正了 UserGuide.md/
   `.zh` 中 3 处同类表述（`vmr analyze` 主表格行、两条命令行示例注释）——审阅指出的是这类问题
   存在，但没有穷举全部位置，本次开工前用 `grep -n "category=task\|cron.*heartbeat.*subagent"`
   做了一次全仓扫描，比审阅点名的范围更完整。

**方法论小结**：这是继 P12/P13 之后第三次由计划外的独立审阅在 ActionPlan 落地后发现执行方自己
没覆盖到的真实缺陷——第 1/2/4 条都是"计划本身的判断有系统性盲区"（受限信号清单不完整、披露路径
选错了默认场景、阈值设计本身反直觉地制造了断崖），第 3 条是"修复一个问题时引入了另一个架构问题"。
四条全部在同一次会话内核实、修复、补测试，没有留到下一阶段——这也是 DevPlan §6.3 通用完成定义
第 8 条（"纪律落成断言"）实际执行的样子：不是"信"审阅的判断，是每一条都独立复现（`grep`/读代码/
构造最小反例）后才动手改。

---

## 7. 总结

**P14+P15 合批目标已达成，且比首版实现更彻底**——两次"当场核实后修复"（内部审阅式的开工前复核 +
一次真正独立的外部审阅）共同把这份 ActionPlan 从"看起来完整"改成"经得起复现检验"：

- **P14.1** 把索引显示与套件渲染两条互相拆台的噪声判据合一为 `story.IsNoiseCategory`，真实语料
  验证默认套件首屏从"22 行可见、8 行可点"变为"18/19 行全部可点、1 行折叠"。
- **P14.2** 的检测器覆盖率披露从"两个 Finding/Metric、只在 `-corpus` 报告、1% 断崖阈值"扩展为
  "7 项受限信号（含 journey 独有的脊柱徽标与 structure.json 字段）、默认路径的每份 journey 报告
  都披露、无阈值断崖"——这个扩展完全来自一次并行独立审阅在同一天内发现的两处真实盲区，不是本计划
  自己预见到的。
- **P15.1–P15.3** 把 `vmr report`/`vmr story` 从"保留独立分派逻辑的过渡别名"变成"结构上不可能
  分叉的纯转发"，`vmr analyze` 补齐三个此前无法表达的模式（`-macro-only`/`-list-only`/
  `-story-only`），最后一个是执行过程中自己发现问题、又被独立审阅指出止血方案本身有架构缺陷、
  最终征询用户后升级为公开能力的完整闭环——记录在 §6.3/§6.4，是这份 ActionPlan 里唯一一处经历了
  "内部止血 → 外部指出止血方案不对 → 用户拍板→ 公开化"完整链条的改动。

**四条经复核成立的独立审阅发现，没有一条是本计划开工前的复核能发现的**——它们分别需要：全仓
`isErrorMarker` 用法扫描（比 `.IsError` 字段搜索更深一层）、对"默认套件从不碰 `-corpus`"这一
执行路径事实的确认、对"`vmr analyze` 自身能力是否覆盖别名"这条架构不变量的显式检查、对
"断崖阈值在中间取值时的行为"这类边界条件的专门验证。这与 P11–P13 的经验一致：**"计划内的开工前
复核"与"计划外的事后独立核查"是两种互补、缺一不可的纠错机制**——这是第三次印证。

**唯一一处需要向用户明确披露的行为变化**：`vmr story -render-all` 首次向公开 flag 空间开放了
`-story-only`；直接使用 `vmr analyze` 的用户此前也无法达到"只渲染全部候选、不跑宏观报表"这个
效果，现在可以。这不是产物变化，是能力补齐，但改变了 `vmr analyze` 的 flag 表面（三个新增
mutually-exclusive 模式），已在 CHANGELOG/UserGuide 中如实记录。

`KNOWN_ISSUES_sonnet-5.md` 的 §1 分布从 P13 之后的"高危 0、中危 6（含中低 1）、低危 15"收敛到
"高危 0、中危 3、低危 15"，不再有 `[中低]` 条目。P11–P15 五阶段全部完成。
