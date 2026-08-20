// Ver 2026-08-20 21:20, by Sonnet 5

# vmr 日志分析体系重构 — 开发计划第二期（P7 起）

## 0. 本文定位

`story_report_dev_plan_opus-5.md`（下称"第一期"）规划的 P0–P6 已全部完成并落地（commit `b098ca9`
至 `2c45a58`）。两轮独立复盘（一份详尽版，逐项实测量化；一份独立复核，未发现额外问题，二者结论
不冲突，只是覆盖面不同——原始过程记录已归档，不在版本控制范围内，结论已完整吸收进本文与
`KNOWN_ISSUES_sonnet-5.md`）对照当前代码库核实了六个阶段的实际达成度：**架构目标本身已经实现**
（3×2 矩阵四个错格补齐、坐标层贯通、共享证据层生效、导航矩阵六条边全部存在），但留下三类未竟
事项，也是本文的范围：

1. **P6 自身的"最后一公里"**：验收走查跑的是 `vmr analyze`（最全路径），用户日常会跑的是
   `vmr report`（默认路径），默认路径上的几处缺陷因此被系统性跳过——最典型的是请求索引的详单
   链接在默认模式下 100% 失效（`KNOWN_ISSUES §1.31`），以及 `vmr analyze` 强制 `-render-all`
   在大语料下把 P3.3 已经关闭的"默认全量物化"重新打开（`§1.32`/`§1.30`）。
2. **一项被搁置未推进的方案**：JSON 输出的语言策略统一（`§1.19`）——`journey-<id>.json` 已经跟随
   `-lang`，`compare-*.json`/`vmr-report.json` 仍固定英文，同一个项目里两条规则并存。方案已有
   专门文档打底（`json_lang_policy_plan_sonnet-5.md`），只是从未落地。
3. **一项被跳过确认步骤的架构决策**：P6.5 的命令行收敛，原计划是"收敛为一个分析动词"，实际交付
   的是"新增第三个动词 `vmr analyze`"（`report`/`story` 原样保留），且这一步本该在落地前询问
   用户确认、实际执行时跳过了。**本文写就前已就此单独征询用户，决策见 §1。**

`docs/KNOWN_ISSUES_sonnet-5.md` 现状（含 §1.30–1.34 与更新后的 §4 ROI 表）已由 Opus-5 复盘就地
核实并回写，本文不重复那次核实工作，只承接其结论。CLI 重构参考
`cli_architecture_redesign_gemini-3.7-flash.md`、JSON 语言策略参考
`json_lang_policy_plan_sonnet-5.md` 两份文档，本文各自章节按需采纳其中已定型的部分，取舍写明
理由——**不是照抄对象**。

**沿用第一期的全部约定**，不在本文重复：里程碑级、不涉及源码细节；每阶段开工前基于该阶段起点的
真实仓库状态重新写 ActionPlan，不沿用本文对后续阶段的任何预判；§2.2 的通用完成定义（测试通过、
真实日志重跑核对、基线快照更新、变更日志与用户指南同步更新、本阶段取舍登记进 `KNOWN_ISSUES`、
阶段收尾边界复核）原样适用于 P7–P9，不再重复列出；§6 的调整机制同样原样适用。**阶段编号从 P7
续起**，衔接第一期的 P0–P6——这是同一条主线的延续，不是另一次独立重构。

**本文对以下关键事实做过独立核实**（不是转述评审报告的转述）：`internal/report/requests.go` 的
`detailLink` 无条件渲染 `details/*.md` 链接、而默认 `-details=false` 不生成该文件；
`cmd/vmr/cmd_analyze.go` 对 story 侧无条件追加 `-render-all`；`internal/report/metrics.go` 的
`buildFindingsForJSON` 硬编码 `i18n.EN`，`report.Build`/`story.Compare` 均不接收 `lang` 参数；
`internal/i18n.MetricLabel(lang, code)` 已存在且已用于 Markdown 渲染，只是未接入 `Compare()` 的
JSON 构造路径；`JourneyIndexRow.Category` 的 `CategoryTask` 是空字符串零值 + `omitempty`；
`internal/report/session.go` 的 `analyzeFile`/`collect()` 确认仍未接入 `.parse-cache/`。

---

## 1. 关键决策：CLI 终态方向（已拍板）

两份复盘唯一一致认定"需要用户拍板"的事项。**决策：真正收敛为一个入口。**

- `vmr analyze` 吸收 `vmr report`/`vmr story` 的全部能力，用互斥的**变焦选择器**
  （`-journey`/`-compare`/`-corpus`）切换输出范围；不带选择器时产出默认的宏观套件。
- `vmr report`/`vmr story` **保留为过渡期别名**：仍可执行、产出不变，但打印一行 stderr 提示指出
  等价的 `vmr analyze` 写法。移除时机留给未来一次独立决定，本文不预判。
- **不新增 `vmr cat`**。CLI 参考文档把它设计为"接管被删除的 `details/*.json`"的读取原语，但这个
  职能已经在第一期 P3 以 `vmr replay -req <coord> -print` 的形式交付、测试、写进用户指南——
  `-req` 定位坐标、`-print` 输出原始 JSON 到 stdout 且不需要 `-provider`，与参考文档里 `vmr cat`
  的构想做的是同一件事。新开一个语义重复的顶层命令，只会让"统一的记录选择器"这个第一期已交付的
  成果出现两个同义拼法，制造这次收敛本来要消除的那类分裂。
- **不采纳参考文档提议的 `--long-flag`/短选项风格**（如 `-j`/`-c`）。vmr 全仓统一用 Go 标准库
  `flag` 包的单横线长写法，`cmd/vmr` 现有十余个子命令没有一处用短选项别名。收敛动词与 flag
  拼写风格是两个独立问题，没有理由借这次机会一并引入新风格——只会让新旧命令的帮助文本不一致，
  不带来实质收益。

这条决策约束 §4 的 P9。

---

## 2. 总体思路

四条主线，对应四个阶段，**顺序不是任意的**：

1. **P7 · 正确性归位**——先把复盘发现的、与 CLI 结构无关的缺陷修掉：默认路径下的死链接、一处
   真实存在的方言过滤漏洞、一处容易误触发付费 LLM 调用的 flag 语义陷阱、几处机读契约小缺口、
   几处过期文案。这些改动全部落在 `internal/report`/`internal/story`/`internal/i18n`——不会被
   P9 的 CLI 重写触碰，先做不会被推翻。
2. **P8 · JSON 语言策略统一**——按参考文档已经论证定型的方向（叙述文本跟随 `-lang`，
   `Code`/`EvidenceAnchor` 保持语言无关）补齐 `report.Build`/`story.Compare` 两处缺口。这会改
   这两个函数的签名。排在 P9 之前，是为了让 P9 收敛出的新入口直接对着已经稳定的签名写调用，
   不必先写一遍再因签名变化改一遍。
3. **P9 · CLI 收敛落地**——建立在前两个阶段之上：正确性已经归位、语言参数已经贯通，P9 成为一次
   相对纯粹的"入口重排"。`vmr analyze` 默认渲染范围的裁决（`§1.32`/`§1.30` 的修法，复用 P6.3
   已算好的 `category` 分类）随之一并做——它今天寄居在即将被整体重写的 `cmd_analyze.go` 里，
   不值得单独立项去修一个马上要被替换的文件。
4. **P10 · 历史文档与规划遗留清理**——P0–P9 十个阶段累积了 22 份 `docs/future-strategy/` 规划/
   评审文档（含本文），其中一部分内容已经被后续文档吸收、甚至源文件已被删除，但引用它们的文字
   还留在权威文档里，指向的是空文件；另一部分（两份 CLI/JSON 语言策略参考文档）现在已经被 P8/P9
   实际采纳，需要一个"已吸收"的指针，避免被当成还悬空的独立提案重新讨论一遍。这一步排在最后，
   是因为它要清理的正是 P7–P9 落地过程中产生的新状态（例如 P8/P9 完成后两份参考文档才算真正被
   吸收），提前做要么清理不完整、要么等 P7–P9 变更落地后又要再清一遍。

**P9 依赖 P7 与 P8**；**P10 依赖 P7、P8、P9 全部完成**（理由见上）。P7 与 P8 之间没有硬依赖，但
仍按顺序做，避免两个阶段交叉修改 `internal/i18n` 下同一批文件（沿用第一期 §2.1"不设并行分支"
的约束）。

---

## 3. 阶段总览

| 阶段 | 主题 | 核心交付 | 依赖 | ROI（依据 `KNOWN_ISSUES §4`） |
| --- | --- | --- | --- | --- |
| **P7**✅ | 正确性归位 | 默认路径死链接清零；方言过滤漏洞修复；flag 语义陷阱修复；机读契约与文案缺口归位 | P0–P6 | 高（`§1.31` 是该表第一次出现的"价值高+成本低+还在等"异常项）——**已完成，2026-08-20，见 `story_report_p7_action_plan_sonnet-5.md` §10 执行记录** |
| **P8** | JSON 语言策略统一 | `journey`/`compare`/`report` 三种 JSON 产物的叙述字段在同一次 `-lang` 下语言一致 | 与 P7 无硬依赖，紧随其后 | 中（架构文档已明确"独立议题，可并行，不卡主线"） |
| **P9** | CLI 命令层真正收敛 | `vmr analyze` 成为唯一分析入口（含三级变焦选择器与默认渲染范围裁决）；`report`/`story` 降级为打印提示的过渡别名 | P7, P8 | 中（架构完整性 + 消除"文档说收敛、代码是三个动词"的自相矛盾，且一并解决 `§1.30` 的 SIGKILL） |
| **P10** | 历史文档与规划遗留清理 | 修复权威文档里指向已删除文件的死引用；给已被吸收的参考文档补"已采纳"指针；`KNOWN_ISSUES §4` ROI 表补全并把 P7–P9 闭环项移入已闭环 | P7, P8, P9 | 低成本、纯文档，防止死引用与过期结论持续误导下一个读者 |

---

## 4. 阶段详述

### P7 · 正确性归位 ✅ 已完成（2026-08-20）

执行细节、真实语料验证结果、与本节设计的两处实现期落差见
`story_report_p7_action_plan_sonnet-5.md` §10"执行记录"，本节原文保留不动（作为阶段开工前的
设计依据），不在此重复。

**目标**：让 `vmr report`（用户日常默认路径）在"什么都不做、只是默认运行一下"时不再产出坏结果
或误导性文案；顺带清掉几处已经诊断清楚、成本低、不需要架构决策的正确性缺口。

**范围**：`internal/report`、`internal/story`、`internal/i18n`、`cmd/vmr/reportconfig.go`。
不改 `cmd_report.go`/`cmd_story.go`/`cmd_analyze.go` 的 flag 定义与子命令分派——那是 P9 的范围，
这里改了也会被整体替换。

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P7.1** 请求索引默认渲染坐标而非死链接 | `detailLink` 无条件渲染链接，默认 `-details=false` 不生成目标文件。`RequestRow.Req` 坐标已在第一期发布，只是没进 Markdown 表；默认模式换成渲染 `req` 坐标，`-details`/详单已生成时保持链接（架构文档 §7.5 原本写明、但未实现的规则） | 单日样本 `vmr-requests.md`/各 client 分组 sibling/`vmr-requests-failed.md` 默认路径下死链接从 322/322 降到 0；`-details` 模式链接如常 100% 可达 |
| **P7.2** `taskseg` 方言过滤补漏 | `openClawAware.RealUserText` 的识别集合没覆盖 `[message_id: …]` 一类脚手架标记，真实语料已观测到它顺着"已过滤"的标题路径泄漏（不是"某处没复用过滤器"，是过滤器本身有缺口）。同批把过滤后的指令文本在 `journey.go` 的 `buildStep` 构建期算好存入 `Step` 字段，脊柱"💬 指令"行改读该字段而非现取，顺带闭环 `§1.21` 登记的"未复用"问题 | 含该标记的样例任务标题/脊柱指令行不再泄漏；补一条基于真实反例的回归测试 |
| **P7.3** `resolveString` 补 `flagPassed` 变体 | 照抄同文件已有的 `resolveBool(explicit, flagVal, rcVal)` 模式，解决 `-llm-addr ''` 无法覆盖 `report.yaml` 默认地址、验证命令容易意外触发真实付费 LLM 调用的问题 | 显式传 `-llm-addr ''` 时不再回退到配置默认值；未传时行为不变 |
| **P7.4** 机读契约两处小缺口 | `JourneyIndexRow.Category` 的 `CategoryTask` 用空字符串零值 + `omitempty`，最常见的一类永不出现在 JSON 里，改为显式 `"task"`；`vmr-stories.json` 的 `journeys[].files` 用原始输入路径而非第一期定的坐标口径（去压缩后缀 basename），改为一致 | `vmr-stories.json` 里全部四类候选都显式带 `category` 字段；`files` 与 `req` 坐标拼法一致 |
| **P7.5** 过期文案批量修正 | `internal/i18n/report_requests.go` 仍承诺已删除的 `.json` 副本；`internal/story/render_spine_args.go` 的 `spineFullCap` 注释引用已删除的 `renderLLMResponse`；`internal/i18n/story_spine.go` 的工具结果截断提示指向本 Step 而实际正文在下一 Step；架构文档 §7.6(b) 给 `evidence/` 列的两个 report 侧引用者从未实现，标注为"设计预留"而非待办 | 四处文案/注释与当前实现一致 |

**阶段验收**：用真实日志重跑默认 `vmr report`/`vmr story`，P7.1–P7.5 均可肉眼或脚本核实；
`go test ./...`、`go test ./internal/archtest/...` 全绿。

---

### P8 · JSON 语言策略统一

**状态**：方向已经论证定型，见 `json_lang_policy_plan_sonnet-5.md` §2——本节只把该文档
§2/§3/§5 的结论收敛进阶段任务表，不重新论证方向；ActionPlan 编写时应通读原文档。

**目标**：`journey-<id>.json`、`compare-<a>-<b>.json`、`vmr-report.json` 三种产物的叙述性文本
（`Finding`/`Evidence`/`Action`/`MetricDiff.Label`）在同一次 `-lang` 下语言一致；
`FindingCode`/`MetricCode`/`EvidenceAnchor` 保持语言无关，作为程序化消费方唯一应依赖的稳定锚点。

**范围**：`internal/report`（`Build`/`BuildCached`、`buildFindingsForJSON`）、`internal/story`
（`compare.go` 的 `Compare()`）、两处"恒英文"回归测试、涉及的设计文档章节。不改
`FindingCode`/`MetricCode` 取值本身，不改 `EvidenceAnchor` 来源，不碰 LLM system prompt 的语言
联动（那部分已经正确）。

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P8.1** `story.Compare()` 接入语言 | 新增 `lang` 参数；`Rows[].MetricDiff.Label` 从硬编码英文改为调用已存在的 `i18n.MetricLabel(lang, code)`（该查表函数今天已经服务 Markdown 渲染，只是没接进 JSON 构造路径）；`cmd_story.go` 及测试补实参 | 同一次 `-lang zh` 下 `compare-*.json` 的 `Label` 与对应 Markdown 用词一致 |
| **P8.2** `report` 侧语言接入路径裁决 | 两条候选路径（方案文档 §3.1 已列出）：(a) 给 `Build`/`BuildCached` 加 `lang` 参数；(b) 保持 `Build` 语言无关，在 `cmd_report.go` 调用点用已经拿到手的 `lang` 现场重算 `rep.Efficiency`。**动手前先确认** `rep.Efficiency` 在 `Build()` 内部有没有被依赖其英文文本本身（而非 `Code`）的下游逻辑——没有则优先 (b)，改动面更小，不必给两个已经很长的函数再加参数 | 选定路径有一句话记录（为什么选它、不选另一条）；`vmr-report.json` 的 `efficiency[]` 在 `-lang zh` 下为中文；`buildFindingsForJSON` 按选定方向删除或改造 |
| **P8.3** 文档回填 | `docs/VirtualModelRouter_Design_v4_Analytics.md` §4.3 整节重写（不是打补丁）；`KNOWN_ISSUES §1.19` 移入已闭环；`CHANGELOG.md` 记一条 `Changed`（`-lang zh` 下 JSON 叙述字段从英文变为中文，是行为变化）；`docs/UserGuide.md`/`.zh` 现有一句"`vmr-report.json`/`journey-*.json`/`compare-*.json` 不受 `-lang` 影响，永远英文"——这句话在 P8 之前就已经不准确（`journey-*.json` 早就跟随语言了），P8 落地后需要整句改写，不是小修 | 三处文档互相一致，无"本节只对部分产物成立"这类自相矛盾的注记；`UserGuide.md`/`.zh` 不再有与实现不符的语言声明 |
| **P8.4** 测试反转与新增一致性检验 | `TestE2E_ReportLangFlagZh`/`TestE2E_StoryCompareLangZhKeepsJSONLabelsEnglish` 两个"恒英文"断言反转（保留测试改名而非删除）；新增一个端到端测试，同一次 `-lang zh` 运行下同时核对三种 JSON 输出的叙述文本语言一致 | 新测试通过；不能只看三个包各自单元测试分别通过就判定完成——方案文档明确指出"各自通过、整体没人核对"正是当前不一致状态的成因 |

**阶段验收**：`go test ./... -race` 全绿；真实语料对 `-lang zh`/`-lang en` 各跑一遍，人工核对
三种 JSON 输出语言真正统一，不只信单元测试。

---

### P9 · CLI 命令层真正收敛

**目标**：落地 §1 的决策——`vmr analyze` 成为唯一分析入口，`vmr report`/`vmr story` 降级为打印
迁移提示的过渡别名；`vmr analyze` 默认渲染范围随之一次性做对，不再依赖"寄居在即将重写的文件
里"这一权宜状态。

**范围**：`cmd/vmr` 的子命令分派与三套现有 `flag.NewFlagSet`；`README.md`/`.zh`、
`docs/UserGuide.md`/`.zh`、`docs/VirtualModelRouter_Design_v4_Analytics.md` §7.9 对应表述、
`CHANGELOG.md`。不改变 `internal/report`/`internal/story` 的包边界——两包仍互不 import，
`cmd/vmr` 依旧是唯一同时看到两半区的组合根。

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P9.1** `vmr analyze` 单一入口 + 三级变焦选择器 | 收敛三套独立 flag 集合为一套；`-journey`/`-compare`/`-corpus` 互斥，选中其一时行为等价于今天 `vmr story` 的对应模式（纯 CLI 层路由，不重新实现渲染/聚合逻辑）；不带选择器时是默认套件模式 | 每种现有 `vmr story` 调用方式，在新入口下用等价参数得到逐字节相同的产物；`internal/report`/`internal/story` 的 diff 为零 |
| **P9.2** 默认渲染范围改为 `category == task` | 复用 P6.3 已算好的候选分类，默认套件模式只对 `task` 类候选做完整物化；`cron`/`heartbeat`/`subagent` 仍进索引（P6.3 的折叠展示不变）但不强制材料化；保留一个显式全量物化开关（沿用现有 `-render-all` 语义） | 全量语料默认路径不再复现 SIGKILL（`§1.30` 闭环）；单日样本产物体积从 164MB 级降到与"仅任务类候选"相称的量级 |
| **P9.3** `report`/`story` 降级为过渡别名 | 内部直接映射到 `analyze` 的等价调用，不重复实现分析逻辑；每次调用打印一行 stderr 迁移提示（含等价 `vmr analyze` 写法） | 两个子命令仍能跑完并产出与之前一致的结果，且打印提示 |
| **P9.4** 文档与门面同步 | `docs/UserGuide.md`/`.zh` 改为以 `analyze` 为主入口；`README.md`/`.zh` 补上 `analyze` 示例（当前完全没有，`grep -c` = 0）；`VirtualModelRouter_Design_v4_Analytics.md` §7.9 表述从"新增第三个动词"改为"收敛为一个动词"；`docs/VirtualModelRouter_Design_v4_Strategy.md` §"额度看板"一节点名 `vmr story`/`vmr story -compare` 要求"保持独立高效"，命令拼写变化后措辞需要跟着更新（架构文档 §10.3 已点名这处，不是本次新发现）；`CHANGELOG.md` 记一条 `Changed`（破坏性变更，`report`/`story` 仍可用但已弃用）；同时修正四处写反的 `vmr analyze` 执行顺序说明（`UserGuide.md`/`.zh`、`VirtualModelRouter_Design_v4_Analytics.md`、`CHANGELOG.md`，应为 story 先、report 后） | 文档与实现一致；`grep -c 'vmr analyze' README.md` > 0 |
| **P9.5** 自指流量输入不对称随收敛自然消失 | `KNOWN_ISSUES §1.34` 记录的"`cmd_story.go` 有 `-llm-key` flag 可覆盖、`cmd_report.go` 没有对应 flag、只用 `rc.LLMKey`"这处不对称，在单一 flag 集合下不再是两份独立实现——`-llm-key` 对所有模式统一生效。P9.1 收敛 flag 集合时这条会自然成立，本任务只是确认它成立并在验收里显式核对，不需要额外代码路径 | `vmr analyze -llm-key X`（不论是否带变焦选择器）与自指流量排除口径一致，不再区分"跑的是报表还是叙事"；`KNOWN_ISSUES §1.34` 该条移入已闭环 |

**阶段验收**：一次端到端走查——`vmr analyze` 默认调用、`-journey`、`-compare`、`-corpus` 四种
形态各跑一遍，产物与收敛前逐项比对；`vmr report`/`vmr story` 仍可独立跑通并给出迁移提示；
全量语料一次完整运行不再复现 SIGKILL。

**对后续的影响**：命令行层面的结构性工作至此收尾。`report`/`story` 两个别名何时真正移除，是
一次独立的、面向未来使用数据的决定，不在本文预判。

---

### P10 · 历史文档与规划遗留清理

**目标**：让 P0–P9 十个阶段留下的规划/评审文档堆积不再干扰下一个读者——已被吸收的内容有明确
指针，已被删除的文件不再被权威文档引用，`KNOWN_ISSUES` 的 ROI 表覆盖它自己登记的全部条目。
**这不是一次代码变更**，全部任务都是文档编辑。

**范围**：`docs/future-strategy/*.md`、`docs/KNOWN_ISSUES_sonnet-5.md`。不涉及
`docs/VirtualModelRouter_Design_v4_*.md` 等核心设计文档（那些的同步已经分别是 P7–P9 各自任务表
里的一部分，不在本阶段重复）。

**已核实的具体缺口**（不是猜测，逐条读源文件确认过）：

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P10.1** 修复架构文档里指向 `docs/future-strategy/` 之外文件的引用 | `story_report_architecture_opus-5.md` 四处引用的文件已不在 `docs/future-strategy/` 下（**已核实：不是被删除，是被移到了仓库根 `archived/` 目录——该目录被 `.gitignore` 第 52 行显式排除，是本项目既有的"本地存档、不进 git"惯例，`story_report_comprehensive_redesign_gemini-3.7-flash.md`/`story_report_suite_reorganization_glm-4.7.md`/`story_report_peer_review_opus-5.md`/`story_report_ux_review_sonnet-5.md` 均已在那里找到**）：§0.1 引用 `story_report_peer_review_opus-5.md`（"逐条评审...记录在"）；§1.2 引用 `story_report_suite_reorganization_glm-4.7.md` §0.2；§10.3 引用前两者之一并断言 `story_report_ux_review_sonnet-5.md`"仍然有效"。四处都需要改写为不依赖文件路径的措辞（例如"结论已吸收进本文，原始过程记录已归档，不在版本控制范围内"），不是简单删掉引用——删掉会丢失"这个结论曾经过独立评审"这条可追溯性；也不是简单把路径改成 `archived/...`——那个目录对任何新 clone 这个仓库的人都不存在，指向它和指向一个真正不存在的文件对读者是同一种体验 | 四处引用不再暗示目标文件在版本控制范围内可达；`internal/archtest` 的文档引用检查覆盖的是 `CLAUDE.md`/`README*`/`docs/` 顶层，不含 `docs/future-strategy/`（已确认，见该检查器 `docs_refs_test.go` 的显式排除注释），所以这条只能靠人工核对，不能指望测试兜底 |
| **P10.2** 给已吸收的参考文档补"已采纳"指针 | `cli_architecture_redesign_gemini-3.7-flash.md`、`json_lang_policy_plan_sonnet-5.md` 的核心结论已经分别进了本文 P9（部分采纳，两点未采纳且写明理由）与 P8（基本全盘采纳）。两份文档本身仍有实施期需要的细节（尤其 `json_lang_policy_plan_sonnet-5.md` §3 的模块级大纲），不删除，但在文档开头加一行"核心方向已采纳，见 `story_report_dev_plan_2_sonnet-5.md` P8/P9；本文档保留作实施期细节参考"，避免被当成还悬空的独立提案重新论证一遍 | 两份文档开头各有一行现状指针；不删除任何内容 |
| **P10.3** `KNOWN_ISSUES §4` ROI 表补全并收口 P7–P9 闭环项 | 该表现有的自述（"本表建成时覆盖的是当时的 §1 全部条目；此后新增的 `1.18`/`1.19`/`1.21`/`1.22`/`1.23`/`1.28`/`1.29`/`1.30` 一直没有补进来"）本身已经是一条待办；P7–P9 落地后又会新增一批需要移入 §3 已闭环的条目（`1.19`/`1.21`/`1.30`/`1.31`/`1.32`/`1.34` 等，具体以各阶段实际验收结果为准，不在此预判哪些条目最终真正闭环） | ROI 表覆盖 §1 当时全部条目；P7–P9 验收确认闭环的条目已移入 §3，不再留在 §1/§4 |

**阶段验收**：`docs/future-strategy/` 里没有指向不存在文件的死引用（人工通读一遍全部 22 份文档的
交叉引用，逐条核实目标文件存在）；`KNOWN_ISSUES §4` 的覆盖范围自述行不再需要存在（因为它描述的
缺口已经补上）。

**对后续的影响**：本阶段完成后，`docs/future-strategy/` 的状态是"每份文档要么是仍然权威的当前
方案（本文与架构文档），要么是明确标注了去向的历史记录（阶段执行记录、已采纳的参考文档），要么
已被删除"——不存在第四种"读者读到一半发现它其实已经过时但没人告诉他"的文档。

---

## 5. 不进入阶段序列的项

| 项 | 处置 | 理由 |
| --- | --- | --- |
| `session.go` 的 `collect()`/`analyzeFile` 接入解析缓存（`KNOWN_ISSUES §1.1`/`§1.23`） | **后续优化，独立排期** | 收益已经被同类改动（`ingest.go` 接缓存，第一期 P3.6）证明为真实（5.2×），但正确性风险高于聚合缓存——算错不是数字偏差，是把不相关对话缝到同一个 Session/Journey。需要先补一套 cold/warm 一致性测试再动手，与 P7–P9 无依赖，可在任意时间点单独立项 |
| 新增 `vmr cat` 命令 | 不做（§1 已拍板） | 职能已被 `vmr replay -req <coord> -print` 完整覆盖，新开顶层命令只是换名字 |
| `chatmsg` 覆盖 OpenAI Responses API 工具调用形状 | 不做 | 真实语料 11253 条记录中占比 0%（`§1.22` 已量化）。触发条件：`vmr-requests.json` 出现该 protocol |
| `journey-<id>.json` 补 schema 版本戳 | 不做（YAGNI） | 唯一已知程序化消费方只读 `EvidenceAnchor`，不受形状变化影响 |
| 决策脊柱的工具调用结果是否也降级为纯引用 | 暂缓 | P5 体积目标未达成（298KB → 实测 86KB–506KB，非 <50KB）的主因，但纪律本身成立（随步数×调用数增长，不随对话正文长度增长）。是否贯彻到工具结果，是可读性与体积的权衡，留给报告体积真正成为痛点时再决定 |
| 大语料深度性能优化（流式批处理/惰性材料化） | 视 P9.2 效果决定 | P9.2 把默认渲染范围收窄后，SIGKILL 的触发条件大概率已消失（根因是"渲染了不该渲染的候选"，不是"渲染得不够快"）。P9 验收后若用户显式要求全量渲染仍然资源耗尽，才需要独立立项 |
| 额度燃尽看板 | 不在本 DevPlan 范围 | 产品功能而非技术债，按产品路线单独排期（`§1.13`） |
| 分析半区体量持续增长（现约 23,300 行 vs 路由半区约 5,300 行） | 不是一次性任务 | 持续性约束（`§1.15`）：新的探索性分析指标应先用外部脚本消费既有稳定契约验证价值，再谈合入主干 |
| 浮点 1 ULP 差异、`§2` 已登记的其余刻意取舍 | 维持原判 | 均已有独立论证，重新打分等于重新论证，不在本文重复 |

---

## 6. 调整机制

沿用第一期 §6 的原文原则（不重复）：允许范围缩减、阶段顺序调整（不违反 P9 对 P7/P8 的依赖、P10
对 P7/P8/P9 的依赖）、相邻阶段任务合并；不允许跨阶段并行推进同一层产物、不允许沿用未经重新分析
的 ActionPlan 开工。

每阶段收尾同样做边界复核，多问一句"现状本来就该这样吗"。
