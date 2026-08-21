// Ver 2026-08-21 16:30, by Opus 5

# Story/Report 架构重构两轮十阶段 — 全面复查记录与后续开发计划

## 0. 任务 debrief 与 Action Plan

### 0.1 任务要求的重述

本文是**一次针对 `story_report_architecture_opus-5.md` 落地完整性的独立复查**，不是又一轮设计。
拆开看是六件事：

1. 以**当前代码库的真实状态**（不是各阶段 ActionPlan 的自述）核对架构文档规划的重构、模块调整、
   功能范围是否完整正确落地；结合 commit 历史找疏漏与错误。
2. 先建 Action Plan：列出全部待复查功能点、Commit 范围、相关文件清单，然后**逐项**核实。
   以模块功能点为单位；一个功能涉及几份文件，每份都要读，读完确认这些文件确实实现了该功能。
3. 复查过程中同时找**冗余待清理**的内容（无用代码、冗余注释、可简化可合并的文档）与潜在优化空间。
4. 复查完成后回顾整个过程，把重要问题**从架构层面**集中分析，按严重程度分成若干**相关且独立可
   处理可验证**的 Package。
5. 错别字、引用错误、小修改**本次直接改掉**，并在文档中记录问题与处理结果，作为"已解决"。
6. **不被既有决策束缚**：架构文档本身可能有问题，要直言；`KNOWN_ISSUES` 里的既有裁决是"省去重复
   论证的起点，不是禁止重新论证的封条"——前提变了就要重新提。

复查完成后，另有四份独立审阅报告被逐项核实——三份针对架构落地
（gpt-5.6-terra / Gemini 3.7 Flash / Sonnet 5），一份针对本轮改动本身
（`story_report_uncommitted_review_gemini-3.7-flash.md`）。其中经实测确认成立的发现已有机并入
本文正文与 Package；核实过程与不采纳理由记录在 `story_report_review_triage_opus-5.md`，
本文只保留结论。第 6 章是基于全部结论编制的后续开发计划。

### 0.2 复查范围与证据基线

| 项 | 值 |
| --- | --- |
| 代码基线 | `a4a1e00`（P10 收尾）+ 工作区未提交改动 |
| Commit 范围 | `b098ca9`(P0) → `a4a1e00`(P10)，共 11 个阶段提交 |
| 规划文档 | `story_report_architecture_opus-5.md`（方案权威）、`story_report_dev_plan_opus-5.md`（P0–P6）、`story_report_dev_plan_2_sonnet-5.md`（P7–P10） |
| 验证二进制 | 工作区代码 `go build -o <scratchpad>/vmrbin ./cmd/vmr` |
| 基线健康度 | `go build ./...` / `go test ./...` / `go test ./internal/archtest/...` / `gofmt -l` / `go vet ./...` 全绿 |

**证据纪律**：本文每一条判定都落到"实读源码（给出文件与行号）"或"实测（给出可复现命令与数字）"
两类证据之一。凡是无法落到证据上的，正文明确写"未验证"。**不采信任何一份文档（包括各阶段自己的
执行记录）的自述作为结论依据。**

**本次复查的全部实测**：

| # | 实测 | 语料 | 结果 |
| --- | --- | --- | --- |
| M1 | 默认 `vmr analyze` 产物体积 | 07-28（322 条） | **164MB**，其中 `details/` 160MB / 306 份、`evidence/` 512KB、`stories/` 2.6MB |
| M2 | 默认 `vmr report` 产物体积 | 同上 | 1.5MB（索引量级） |
| M3 | 详单体积构成 | 306 份 / 152MB | Raw SSE 块 **62.4MB（41.1%）**；① 段 82.9MB，其中首个 `🆕` 之前 **79.2MB（该段 95.5%）**、本轮新增消息约 3.7MB；① 段之外的其余内容约 6.7MB |
| M4 | journey 报告体积 | 6 条 | 107KB / 132KB / 171KB / 192KB / 384KB / **609KB** |
| M5 | 导航边存在性扫描 | out1 全部 `*.md` | 419 条导航链接，真实死链 **0**（72 条"失效"逐条核对后全部是围栏内的对话正文） |
| M6 | `sessions[].id` ↔ `journeys[].lineages` join | 25 session / 6 journey | 9/9 命中 |
| M7 | 三种 JSON 的 `-lang zh` 一致性 | journey / compare / report | 全部中文 |
| M8 | **同目录先 EN 后 ZH 渲染同一条 journey** | 22 步 journey | 详单文件名相同、**内容不同**（EN 残留）；与全新 ZH 目录同名文件 `diff` → DIFFERENT |
| M9 | **删除 `evidence/` 后重跑** | 同上 | evidence **未重建**（0 文件）；journey 头 1 条 + 22 份详单 44 条 = **45 条链接永久死亡** |
| M10 | **全量语料坐标与文件名唯一性** | **11,274 条** | distinct `req` 11,274；distinct `hash8` **11,274（0 碰撞）**；distinct 文件名 **11,274（0 碰撞）**；共享前缀 10 个 / 覆盖 20 条 |
| M11 | **`evidence/` 命名空间** | 四日抽样 2,939 条 | sysprompt **60** + tools **11** = 71 |
| M12 | **全量语料候选分类与请求量** | 477 候选 | task 238 / 9,274 req；cron 112 / 1,032 req（**44 条 ≥10 req，max 30**）；heartbeat 107 / 229 req（**0 条 ≥10，max 7**）；subagent 20 / 563 req（**16 条 ≥10，max 91**） |
| M13 | **索引可见性 vs 渲染** | 07-15 默认 analyze | 首屏可见 22 行（12 task + 10 cron），**仅 8 行有 journey 链接**；10 个 cron 行全部可见且不可点 |
| M14 | **协议分布** | 11,274 条 | openai **99.48%** / anthropic **0.52%** / openairesponses **0.00%** |
| M15 | **全量语料热缓存耗时** | 34 文件 | `report -details=false` **18.4s**；`story`（列表）**3.4s** |
| M16 | **生产源码的阶段号与规划文档引用** | `internal/` + `cmd/` | 71 处阶段号 / 27 文件；23 处 `story_report_*.md` 引用，当前 **0 死引用** |

全量语料数字（渲染耗时 413s、峰值 4.59GB、产出 3.5GB / 8343 份详单）引自 P9 执行记录，
本次未重跑该项。

### 0.3 Action Plan：待复查功能点 × 文件清单

复查以**阶段任务**为功能点单位（这是三份规划文档共用的粒度），每项列出需实读的文件。

| # | 功能点 | 阶段 | 需实读的文件 |
| --- | --- | --- | --- |
| R1 | 工具调用结果三级配对（精确→归一化→位置） | P1.1 | `story/findings_toolresult.go`、`story/render_spine_step.go`、`chatmsg/pairing.go`、`chatmsg/toolresults.go`、`story/invariants_test.go` |
| R2 | 脊柱覆盖补完（指令行/汇报行/交付物） | P1.2 | `story/render_spine_step.go`、`story/render_spine.go`、`story/journey.go` |
| R3 | Compare 初始指令 + LLM 标题层级兜底 | P1.3/P1.4 | `story/compare.go`、`story/render_compare.go`、`story/llm.go` |
| R4 | 请求级坐标 `req` 定义、发布、断言 | P2.1 | `ctxgraph/reqcoord.go`、`ctxgraph/manifest.go`、`ctxgraph/scan.go`、`ctxgraph/cache.go`、`report/rows.go`、`report/recextract.go` |
| R5 | 详单减法 + 下沉 `reqdetail` + 确定性命名 | P2.2/P2.3 | `reqdetail/detail.go`、`reqdetail/render.go`、`reqdetail/facts.go`、`reqdetail/diff.go`、`reqdetail/ensure.go`、`report/detail.go`、`archtest/import_boundaries_test.go` |
| R6 | 删 `details/*.json` + 坐标读取原语 | P3.1/P3.2 | `report/detail.go`、`cmd/vmr/cmd_replay.go`、`audit`（`LineAt`） |
| R7 | 详单按需且幂等 | P3.3 | `cmd/vmr/cmd_report.go`、`reqdetail/ensure.go`、`story/ensure_details.go` |
| R8 | 共享证据条目 `evidence/` | P3.4 | `reqdetail/evidence.go`、`story/render_md_sysprompt.go` |
| R9 | 缓存与索引分家、分片、载荷扩大、schema 版本戳 | P3.5–P3.7 | `ctxgraph/cache.go`、`report/factscache.go`、`story/storyindex.go`、`report/requests.go` |
| R10 | `journey-<id>.json` 的 `structure` + 内联/引用边界 + 常驻守卫 | P4.1–P4.3 | `story/structure.go`、`story/structure_test.go`、`story/llm.go`、`story/llm_packs_test.go` |
| R11 | 删 fact-layer、脊柱挂详单链接、系统提示词改引用 | P5.1–P5.3 | `story/render_md.go`、`story/render_spine_step.go`、`story/render_md_sysprompt.go` |
| R12 | Lineage 内容寻址 ID | P6.1 | `ctxgraph/lineage.go`、`report/session.go`、`story/storyindex.go` |
| R13 | 六条导航边 | P6.2/P7.1 | `report/render_doc.go`、`report/requests.go`、`cmd/vmr/cmd_report_stories_link.go`、`story/render_md.go`、`reqdetail/detail.go` |
| R14 | 索引类别列与噪声折叠 | P6.3 | `story/storyindex.go`、`story/candidates.go` |
| R15 | 自指流量口径统一 | P6.4 | `cmd/vmr/selftraffic.go`、`report/selftraffic.go`、`report/aggregate.go` |
| R16 | `taskseg` 方言过滤补漏 + 指令构建期算好 | P7.2 | `taskseg`、`story/journey.go`、`story/render_spine_step.go` |
| R17 | `resolveString` 的 `flagPassed` 变体 | P7.3 | `cmd/vmr/reportconfig.go`、`cmd/vmr/cmd_analyze.go` |
| R18 | 机读契约两处小缺口（`category`、`files` 口径） | P7.4 | `story/storyindex.go` |
| R19 | JSON 语言策略统一 | P8.1–P8.4 | `story/compare.go`、`report/metrics.go`、`report/recextract.go`、`cmd/vmr/cmd_report.go`、`i18n` |
| R20 | `vmr analyze` 单一入口 + 三级变焦 + 默认渲染范围 | P9.1/P9.2 | `cmd/vmr/cmd_analyze.go`、`cmd/vmr/cmd_story_setup.go`、`cmd/vmr/cmd_story.go` |
| R21 | `report`/`story` 降级为过渡别名 | P9.3/P9.5 | `cmd/vmr/cmd_report.go`、`cmd/vmr/cmd_story.go`、`cmd/vmr/main.go` |
| R22 | 文档与门面同步 | P9.4/P10 | `README*.md`、`docs/UserGuide*.md`、`CHANGELOG.md`、`docs/VirtualModelRouter_Design_v4_Analytics.md`、`docs/future-strategy/*` |
| R23 | 跨切面：架构文档自身的目标达成度 | §7.4/§7.6/§7.9 | 真实语料产物实测 |
| R24 | 跨切面：死代码、冗余注释、可简化项 | — | `deadcode` 全仓扫描 + 人工核对 |

---

## 1. 逐项核实记录

约定：✅ = 完整落实；⚠️ = 落实但有缺口；❌ = 未落实或与验收标准不符。

### R1 三级配对 — ⚠️

读了 `story/findings_toolresult.go`（归一化 + 两趟匹配，第 50–73 行）、`story/render_spine_step.go`
（`positionalToolResults`，第 288 行起，带 `SpinePositionalMatch` 标注）、`chatmsg/pairing.go`、
`chatmsg/toolresults.go`。三级分层与"只有第三级是推断、只用于渲染"的纪律都实现了，配对正确性在
真实语料上可验（openclaw journey 脊柱有工具结果行）。

两处缺口：

1. **归一化落在 `internal/story`，不在 `chatmsg`。** 架构文档 §5.7 建议 2 的原话是"`chatmsg` 的配对
   逻辑加归一化回退"。实际实现放在 story 侧的 `toolResultsFor`。后果是 `internal/report` 半区
   （以及任何未来的第三个消费方）拿不到归一化配对，而 `CLAUDE.md` 的不变量写的是"`ctxgraph`/
   `chatmsg` 是消息哈希与消息解析的单一真源……私有再实现会与之静默分歧，这是一整类 bug"。
   今天没有第二个消费方，所以不是活的 bug；但它是一处**已经发生的真源外移**，该登记。
2. **`chatmsg.CheckToolPairing` 的 F9 doc comment 与 §5 实测直接矛盾**（"100% pairing rate is an
   invariant of the data"），而 §5 已用五个真实日志文件证明 OpenClaw 家族客户端上精确匹配是 0%。
   → **本次已修**（见 §4 第 7 项）。

另有一条与配对分层相邻、但性质不同的发现：依赖 `ToolResult.IsError` 的 Finding 检测器只在
Anthropic 协议上能触发，而实测协议分布是 openai 99.48%——见 §2.10。

### R2 脊柱覆盖补完 — ✅

`renderDecisionSpine`（`render_spine_step.go:145`）对每个 Step 二选一渲染
（`renderSpineStep` / `renderSpineBriefStep`），无 `anyCalls` 提前返回；`renderFinalDeliverable`
在末尾复用 compare 的 `deliverableStats`；中途指令行由 `s.Instruction` 驱动，两条渲染路径都覆盖。
真实语料六条 journey 逐条肉眼核对，无整段消失的 Step。

顺带发现一处防御性代码写反：`spineStepHeader` 在第 194 行无条件解引用 `s.Manifest.TS`，第 199 行
才 `if s.Manifest != nil`——nil 检查永远救不了它。→ **本次已修**。

### R3 Compare 初始指令 + LLM 标题兜底 — ✅

`compare.go:283–329` 的 `InitialInstructionStats`（2000 字符上限、`Truncated` 标记、方言感知提取），
进 `compare-*.json` 的 `extras.initial_instruction`；`render_compare.go:31` 渲染折叠块。
`llm.go:433–460` 的标题降级跳过 ``` 与 ~~~ 两种围栏。三点定案全部落地。

### R4 请求级坐标 — ✅

`ctxgraph/reqcoord.go` 是完整的一套：`CanonicalPath`（去 `.zst`、取 basename）、`ReqCoord`、
`ParseReqCoord`、`ReqHash8`（md5 前 4 字节，沿用 `toolsSig` 口径）、`CheckPathCollisions`（架构文档
§7.3a 要求的"响亮地失败"断言，`Scan`/`ScanCached` 各自入口调用）。坐标在构造时存进
`Manifest.Req`（`manifest.go:31`），下游读字段而非重算。

实测 M10（全量 11,274 条）：`req` 全部唯一，`hash8` 全部唯一（**0 次碰撞**），详单文件名全部唯一。
坐标契约在真实规模上成立。

### R5 详单减法 + 下沉 + 确定性命名 — ✅

`internal/reqdetail` 是一个真正的叶子包（`import_boundaries_test.go` 新增的 `vmr/internal/reqdetail`
条目禁止它反向 import `report`/`story`/`router`/`server`/`config`）。`Render(rec, path, line, m, prev,
prof, lang, linkEvidence)` 的入参全是共享层类型，无 `report.ReqInfo` 残留。文件名
`FileName(ts, virtual, real, outcome, req)` 用记录自带时区偏移 + `ReqHash8`，无批次计数器、无 `-N`
后缀、无 `fmtutil.DisplayZone`；`FileNameForManifest` 与 `FileNameForRecord` 汇入同一个格式化器。

`internal/report/detail.go` 从 1047 行缩到 282 行（`archtest` 豁免值工作区已从 1150 调到 350）。

**一处应当订正的表述**：`FileName` 的 doc comment 写"hash8 是唯一性的保证，前缀
（`ts_virtual_real_outcome`）纯属装饰"。实测 M10 显示前缀自身就已近乎唯一（11,264 / 11,274，
只有 10 个前缀被两条记录共享）——设计意图上这句话成立，但用它评估碰撞风险会误导：
事实上装饰性前缀承担了绝大部分区分度。这一点在 §2.8 评估"文件名可信度"时是关键。

### R6 删 `details/*.json` + 坐标读取原语 — ✅

`writeOneDetail`（`report/detail.go:84`）只写 `.md`；`vmr replay -req COORD -print` 已交付并写进
UserGuide；`audit.LineAt` 是底层原语。真实语料 `details/` 下 0 个 `.json`。

一处过期注释：`DetailWriter` 的 doc comment 仍写"writes one .md + one .json"，与同文件上方
`writeOneDetail` 的说明自相矛盾。→ **本次已修**。

需要补一句量级上的准确表述：删掉的 `.json` 是架构文档 §7.6c 测算的约 12GB 中的
**2/3（`.json` 40MB : `.md` 20MB）**，即约 8GB；剩下的 `.md` 路径在默认 `vmr analyze` 上仍写出
3.44GB（见 §2.1/§2.2）。把这一步描述为"消除了 12GB 级的写盘放大"会让读者以为体积问题已经解决。

### R7 详单按需且幂等 — ❌（对 `vmr report` 成立；对 `vmr analyze` 不成立；幂等性本身也只在一个维度上成立）

`vmr report` 侧成立：`-details` 默认 false，默认运行只写索引（实测 M2 = 1.5MB）。

**两处不成立**：

- **`vmr analyze` 默认路径把"按需"整体抵消了**——单日 164MB（M1），详见 §2.1。
- **"幂等"这个词今天只在"同语言、同 evidence 模式"这个前提下成立。** `EnsureRendered` 的跳过谓词是
  `os.Stat(target) == nil`，而 `Render` 还依赖 `lang` 与 `linkEvidence`，两者都不进文件名。
  实测 M8/M9 证明这不是理论风险，详见 §2.8。

### R8 共享证据条目 — ⚠️

`reqdetail/evidence.go` 的 `EnsureSysPromptEvidence`/`EnsureToolsEvidence` 内容寻址、幂等、
经 `ensureEvidenceFile` 原子写。真实语料：322 条记录 → `evidence/` 8 个文件，512KB。
`story/render_md_sysprompt.go` 用每个 era 起始 Step 的 `Manifest.SysHash` 定位文件名
（P4 交接说明里点名的那个坑没踩）。架构文档 §7.6b 表格里"report 侧引用者"两格已在 P7.5 标为
"设计预留，未实现"，与代码一致。

⚠️ 的原因是**证据目录与详单目录之间没有任何一致性保证**：evidence 的生成位于 `EnsureRendered` 的
"文件不存在"分支内，所以详单一旦存在，evidence 就永远不会被重建。实测 M9：删掉 `evidence/` 后重跑，
45 条链接永久失效。详见 §2.8。

另有一处应当更新的量级判断：架构文档 §7.6c 用"共享条目去重后的基数是个位数到几十"来论证不需要
引用计数与 GC。实测 M11（四日抽样 2,939 条）已是 **60 个 sysprompt + 11 个 tools**。
**结论仍然成立**（71 个文件 / 512KB 不值得引入生命周期管理），但支撑它的数字应换成实测值。

### R9 缓存与索引分家 — ✅

`ctxgraph.CacheSchemaVersion = 1` 参与命中判据（`cache.go:175`）；`LoadCacheDir`/`SaveCacheDir`
按 `CachedFile.Hash` 分片、紧凑编码、临时文件 + 改名；两条命令共用 `{outDir}/.parse-cache/`。
`report/factscache.go` 把每条记录的事实提取结果一并纳入缓存并共用同一个版本戳。索引侧
`vmr-stories.json`/`vmr-requests.json` 保留 `MarshalIndent`（人会看，架构文档明确要求）。

实测：`vmr-stories.md` 2.4KB、`vmr-stories.json` 4,055 B，索引已回到"可随手 cat"的量级。
`ctxgraph.FileCache` 的 key 规范化为 `CanonicalPath`，§7.3a 提到的"绝对/相对路径各存一份、
255 条 manifest 重复"随之消失。

热缓存收益按 §7.10 的目标（个位数秒）未达成：实测 M15 全量语料 `report` 热缓存 **18.4s**、
`story` **3.4s**——与 `KNOWN_ISSUES §1.1/§1.23` 登记的诊断一致（`session.go` 的
`collect()`/`analyzeFile` 仍未接入缓存），维持原判。

### R10 中观机读层 — ✅

`story/structure.go` 覆盖 Task/Step/Event/ToolCall 全结构，含 P4 执行期才确认必须纳入的
`EditRef`/`StitchRef`/`CompactionRef`（fact-layer 展示、但单条审计记录物理上算不出来的图级事实）。
内联/引用边界正确：`NewEvents` 只有 `Hash`/`Role`/`FirstStepSeq`，`ToolCallRef` 不带结果正文。

**P4.2 要求的"常驻自动化检查"真的存在**：`TestBuildStructure_VolumeBoundedByStepsNotProseLength`
（`structure_test.go:322`）用同步数、正文长度差两个数量级的两条 Journey 断言序列化体积差被
`structureExcerptChars × 字段数` 界住，并额外断言巨型 payload 从不整段内联。**这是全仓唯一一条把
设计纪律本身写成常驻测试的任务，也是唯一一条至今没有退化的纪律**——这个对照在 §2.11 与第 6 章
的通用完成定义里都会再用到。

真实语料：openclaw 22 步样例的 `structure` 65.8KB。

### R11 人读层瘦身 — ⚠️（结构达成、体积未达成）

fact-layer 渲染函数已删；脊柱每步挂 `../details/<name>.md` 链接（`spineStepHeader`，纯由 Manifest
算出）；系统提示词头部改为 `evidence/` 链接；五类图级分析事实搬进 `spineTransitionLines`。

体积：架构文档 §7.4 的目标是"298KB → < 50KB"。实测 M4 六条 journey
**107KB / 132KB / 171KB / 192KB / 384KB / 609KB**。§7.4 原有的 P6 复盘注记（86KB–506KB）在 P9 之后
已过期。根因见 §2.5 第 1 条——那不是样本错配，是同一条样本上 2 倍以上的估算偏差。

### R12 Lineage 内容寻址 ID — ✅

实测 M6：`vmr-report.json` 的 25 个 `sessions[].id` 全部是 `l-<hash8>` 形态、`alias` 保留 `s01`
位置序号；`vmr-stories.json` 的 `journeys[].lineages` 与之 join，9/9 命中（其余 16 条 lineage
不构成候选 Journey，符合 `ListCandidates` 的结构性排除）。

### R13 导航边 — ⚠️

六条边逐条走查：

| 边 | 状态 |
| --- | --- |
| `vmr-report.md` → `stories/vmr-stories.md` | ✅ 存在，且带生成时间与覆盖窗口（§7.5 要求的"让链接自己说明来源"） |
| `vmr-requests-<tag>.md` 会话行 → journey | ✅ |
| `journey-*.md` → `vmr-stories.md` / `vmr-report.md` | ✅ |
| `details/*.md` → `vmr-requests.md` | ✅ |
| 脊柱 Step → `details/*.md` | ✅ 22/22 可达 |
| journey 头 / 详单 → `evidence/*.md` | ⚠️ 可达，但**没有任何机制保证它保持可达**（M9：删 evidence 后 45 条永久失效，见 §2.8） |
| `vmr-report.md` §8 → 详单 | ⚠️ 见下 |

实测 M5：419 条导航链接，逐条核对后真实死链 **0**（脚本报的 72 条全部是脊柱内联的工具参数/结果
**正文**里的 Markdown 链接，落在代码围栏内，渲染时不成为链接）。

⚠️ 的那一条是**P7.1 与 P9.2 交互产生的口径错误**：`vmr analyze` 默认路径下 story 半区已经物化了
306 份详单，但 report 半区的 `detailsOn` 仍是 false，于是
(a) §8 渲染 "This run did not write `details/*.md`"——事实上写了；
(b) `vmr-requests.md` 的"文件"列渲染 `req` 坐标而非链接——306 份已存在的详单没有被链接。
不产生死链接（这是 P7.1 的本意），但产生了一句假话和一次可用性损失。详见 §2.3。

### R14 索引类别与噪声折叠 — ⚠️

分类器（`candidates.go:52` 的 `classifyJourney`）与折叠渲染（`storyindex.go:252`）都已落地；
P7.4 已把 `CategoryTask` 从空字符串零值改为显式 `"task"`，`vmr-stories.json` 每行都显式带 `category`。

⚠️ 的原因是一处此前无人发现的**策略互相拆台**：索引的**显示**策略只折叠 heartbeat + subagent
（cron 与 task 并列显示在首屏），而默认套件的**渲染**策略只渲染 task。实测 M13：单日 22 行可见、
仅 8 行可点，10 个 cron 行全部以一等公民身份显示且全部点不进去。详见 §2.9。

### R15 自指流量口径 — ✅

识别规则只定义在 `cmd/vmr/selftraffic.go`，`report`/`story` 两侧共用同一次计算结果；
`vmr-report.json` 的 `meta.self_traffic_excluded` 存在（`rows.go:80`，`omitempty`，本样本无自指
流量故不出现）。

### R16 `taskseg` 方言过滤补漏 — ✅

`RealUserText` 识别集合已覆盖 `[message_id: …]`；过滤后的指令文本在 `journey.go` 的 `buildStep`
构建期存入 `Step.Instruction`，脊柱读字段而非现取（`§1.21` 因此闭环）。有基于真实反例的回归测试。

### R17 `resolveString` 的 `flagPassed` 变体 — ✅

`resolveStringExplicit(flagPassed(fs, "llm-addr"), ...)` 在 `cmd_analyze.go:125` 与
`cmd_story.go:90` 都在用，`-llm-addr ''` 可覆盖 `report.yaml` 默认值。

### R18 机读契约小缺口 — ✅

见 R14、R4。

### R19 JSON 语言策略统一 — ✅

实测 M7（`-lang zh`）：`journey-*.json` 的 20 条 finding、`compare-*.json` 的 `rows[].label`、
`vmr-report.json` 的 4 条 `efficiency[]` 全部中文。

P8.2 走的是路径 (b)：`Build` 保持语言无关，`cmd_report.go:337` 调用点 `report.LocalizeEfficiency`
重算。核对了这一行落在 `runReport` 内部，因此 `vmr analyze` 与 `vmr report` 两条入口都覆盖——
这是一处容易漏的隐性契约（`recextract.go:59` 的 doc comment 明确写"跳过它会静默拿到英文默认值，
不报错"），当前唯一调用点正确，但契约本身靠注释维持，没有守卫。属于可接受的小风险，登记备查。

**但语言这条线在产物层面并没有真正统一**：同一次 `-lang zh` 运行下，journey 报告是中文而它链接的
详单可能是英文（M8）。JSON 契约统一了，人读产物没有。见 §2.8。

### R20 `vmr analyze` 单一入口 — ⚠️

三级变焦选择器、互斥校验、默认套件模式都实现了，`internal/report`/`internal/story` 的 diff 为零
（"纯 CLI 层路由"约束守住了）。`taskOnlyCandidates` 复用 P6.3 已算好的 `category`，不新增分类逻辑。

**但 P9.2 的验收标准"单日样本产物体积从 164MB 级降到与'仅任务类候选'相称的量级"没有达成**——
实测 M1 仍是 164MB / 306 份详单，与 P6 复盘的数字**逐字相同**。详见 §2.1。

### R21 别名降级 — ⚠️

两个别名都打印 stderr 迁移提示且产物不变。但 P9.1 的验收标准原文是"**收敛三套独立 flag 集合为
一套**"，实际交付是**新增第四套（analyze 的并集）+ 原样保留 report/story 两套**。
`cmd_story.go:126–157` 的分派逻辑与 `cmd_analyze.go:196–257` 的 `dispatchAnalyze` 是两份手写实现，
互斥规则已经有差异。详见 §2.4。

`§1.34` 的 `-llm-key` 不对称：P9.5 声称"收敛后自然消失"，但只在 `vmr analyze` 上消失——
`cmd_report.go` 至今没有 `-llm-key` flag，仍只读 `rc.LLMKey`。别名路径上不对称原样存在。

### R22 文档与门面同步 — ⚠️

`grep -c 'vmr analyze'`：README.md 2 / README.zh.md 2 / UserGuide.md 14 / UserGuide.zh.md 14 /
CHANGELOG.md 3。`CHANGELOG.md` 的 `[Unreleased]` 覆盖了 P1–P9 全部用户可见变更。
P10.1/P10.2 的死引用修复与"已采纳"指针都落地了。对 `docs/future-strategy/*.md` + `CLAUDE.md` +
`README*` + `KNOWN_ISSUES` 跑了一次 Markdown 路径存在性扫描，**无真死引用**——所有指向已归档文件的
引用都带"（已归档，不在版本控制范围内）"措辞。

⚠️ 的原因是这次扫描**只检查了"引用的文件是否存在"，没有检查"自称是当前状态的文档，其内容是否仍是
当前状态"**——这是两类不同的检查，后者本轮才补上。实测：
`docs/future-strategy/vmr_future_strategy_v2_sonnet-5.md` 第 4 行自称"**自包含的现行战略文档**"，
第 6 行写"基线：commit `4ef2665`（2026-07-27）"——**早于 P0（`b098ca9`）**，即整轮重构开始之前；
全文 `vmr analyze` 出现 **0 次**，`vmr report` 出现 6 次。

P10 的验收标准原文是"每份文档要么是仍然权威的当前方案，要么是明确标注了去向的历史记录"。
这份文档两者都不是。同类风险还包括生产源码里 23 处指向 `docs/future-strategy/` 的引用——
见 §2.6。

### R23 架构文档自身目标达成度 — 见 §2.5

### R24 死代码与冗余 — 见 §2.6

---

## 2. 集中分析：从架构层面看发现

### 2.1 【最重】证据层的体积纪律从未在推荐入口上成立，且被三次验收系统性绕过

**事实。** 实测 M1，默认 `vmr analyze` 跑单日真实语料（322 条记录）：

```
164MB  总产出
160MB  details/（306 份详单）
512KB  evidence/
2.6MB  stories/
```

这与 P6 复盘记录的"单日 322 条记录实测产出 164MB（其中 `details/` 160MB，306 份详单）"**逐字相同**。
P9.2 的修法（默认只渲染 `category == task` 候选）在这份样本上过滤掉了 **0 个**候选——6 个候选
全部是 `task`。实测 M12 在全量语料上给出同样的结论：task 类占候选数量的 50%（238/477），
却占 **83.6% 的请求量**（9,274/11,098）。

**这条纪律被绕过了三次，每次都是"验收标准漂移"而不是"实现走样"：**

| 轮次 | 架构文档的纪律 | 实际验收对象 |
| --- | --- | --- |
| P3.3 | "默认运行的产物集合回到索引量级" | 只验 `vmr report`——当时 `analyze` 还不存在 |
| P6.5 | 同上 | 验的是"一次调用得到导航闭合的套件"，体积没进验收表；`analyze` 强制 `-render-all` 抵消了 P3.3，P6 复盘事后发现 |
| P9.2 | "单日样本体积从 164MB 级降到与'仅任务类候选'相称的量级" | 验的是"不再复现 SIGKILL"。全量语料 3.5GB / 8343 份详单被记为成功 |

`KNOWN_ISSUES §3` 曾把 `1.32`（`analyze` 强制 `-render-all` 抵消默认按需）列为已闭环。**该判定已在
本次复查中撤回**：形态从"强制 `-render-all`"换成了"默认渲染全部 task 候选、每条 journey 每步物化
详单"，纪律本身仍未成立。P9 只解决了它的一个下游症状（SIGKILL）。

**根因不是 P9.2 诊断的那个。** P9.2 假设根因是"渲染了不该渲染的候选类别"。真实根因在
`cmd/vmr/cmd_story.go:745`：`writeJourneyFile` **无条件**调用 `story.EnsureJourneyDetails`，
不区分"用户点名要看这一条 journey"和"批量渲染全部候选"。

架构文档 §7.5 其实已经写明了这条规则的边界，只是没人在批量模式上执行它：

> 这条规则的成本与"被渲染的链接条数"成正比。任务报告的脊柱一次几十步，可以永远挂链接；
> 而请求索引列的是**全部请求**，若每行都挂链接就等于全量物化详单——那正是 §7.6(c) 要消除的东西。

"渲染即生成"是为**单 journey 下钻**设计的（几十步、用户主动要求）。默认套件模式一次渲染 238 条
journey / 9,274 步，把它变成了全量物化。链接文件名可算、生成幂等——这两条性质恰恰说明**批量模式
可以只挂链接不生成**，用户真点进去的那一条再按需补。

**修法**：`writeJourneyFile` 增加一个"是否物化详单"的入参；单 journey 路径（`renderJourney`、
`ensureJourneyFile`）传 true，批量路径（`renderJourneys`/`renderAllJourneys`）传 false，
`-details`/`-render-all` 显式要求时传 true。改动面在 `cmd/vmr` 一层，`internal/*` 零 diff。

### 2.2 【最重】详单内部 93% 是复制——同一把刀该砍第三次

架构文档 §7.6c 只砍了 `details/*.json` 这一份副本。对 `.md` **内部**的重复从未审视过。
实测 M3（306 份详单、152MB；`du` 的 160MB 含块对齐）：

| 组成 | 字节 | 占比 | 性质 |
| --- | --- | --- | --- |
| `Raw SSE, full` 折叠块 | 62.4MB | **41.1%** | `rec.Client.Response.Body` 的**逐字复制**（`reqdetail/detail.go:546`） |
| ① Client → VMR Request 段 | 82.9MB | 54.6% | 其中 **79.2MB（该段的 95.5%、总量的 52.1%）**在第一个 `🆕` 标记之前——即**上一轮详单已经渲染过一遍**的历史消息；剩下的 ~3.7MB（2.4%）才是本轮真正新增的消息 |
| 其余（②attempts、③重组输出、头部） | ~6.7MB | 4.4% | — |

**约 93% 的详单体积是复制。** 这与 §7.6c 删 `.json` 的论证**逐字同构**：

> 它是 `json.MarshalIndent(audit.Record)` 的逐字复制……有了 `req` 坐标，"取这条记录的原文"是
> 一次定位，不是一次物化。

两条减法，两个机制都已经在仓库里现成：

1. **Raw SSE 全文块 → 坐标。** 它是 `Client.Response.Body` 一字不差的复制，而
   `vmr replay -req COORD -print`（P3.2 已交付）正是取回它的原语。把
   `Details(t.RawSSEFull(...), codeFence(body))` 换成一行带坐标的提示即可。重组后的模型输出
   （reasoning/content/tool_calls）**保留**——那是解读，不是复制。**立减 41%。**
2. **历史消息 → 上一轮详单链接。** `renderSessionHeader`（`reqdetail/detail.go:285`）**已经**在
   渲染 `PrevTurnLink(prev.TS, FileNameForManifest(prev))`。那条链接本来就是为这件事准备的，
   只是没被用来做减法。把 `for i, msg := range msgs` 的历史段落改为一行"#1–#57 见上一轮详单
   <link>"，只逐条渲染 `deltaStart` 之后的消息。**再减约 52%。**

   一处诚实的取舍：这会让"单份详单自包含"变成"要顺链回溯"。但 lineage 首条记录（`prev == nil`）
   仍然全文渲染，链条有起点；而 `Manifest.SysHash` 走 evidence 引用的先例已经证明这种取舍在这个
   证据层里是可接受的。

预期：单日 152MB → 约 11MB；全量语料 3.44GB → 约 250MB。**这才是 §7.6c "回落到索引量级"的兑现。**

**这三处复制与 §2.8 的缓存谓词缺陷共享同一个根**：详单被当成了"名字确定即内容确定"的纯产物。
这个前提让"跳过已存在的文件"看起来安全，也让"每份详单必须自包含"看起来必要——**两个推论都不成立，
而且它们必须一起修**：只要跳过谓词还只看文件名，任何对 `Render` 输出的改动（包括本节的两条减法）
在已有 `details/` 目录上都**永远不会生效**。这是 §2.8 成为本节硬前置的原因，不是排期偏好。

### 2.3 【中】默认套件的两个半区不知道对方物化了什么

`vmr analyze` 默认路径：story 半区物化了 306 份详单，report 半区的 `detailsOn` 仍是 `false`。后果：

- `vmr-report.md` §8 渲染 "This run did not write `details/*.md` (generated on demand by default)"
  ——**这句话是假的**，本次运行写了 306 份。
- `vmr-requests.md` 的"文件"列渲染 `req` 坐标而非链接——306 份已存在的详单没有被链接到。

P7.1 把这一列的判据定为 `detailsOn`（report 半区自己的 flag），而架构文档 §7.5 定的判据是
**目标产物是否存在**（"两类边两种策略"里的第二类，`stat` 一次）。用 flag 近似"文件存不存在"，
在两个半区各自决定物化范围之后就不再成立。

修法取决于 §2.1 怎么改：若采纳 §2.1（批量模式不物化），则默认路径下确实一份详单都没有，
现状文案与坐标列都变成正确的，这条自动消失；若不采纳，则 `detailCell` 与 §8 文案改为按目标存在性
判断。**合并到 §2.1 一起做**，不单独立项。

### 2.4 【中】CLI 收敛停在半程，且已经产生第二份手写分派

`main.go` 今天分派三个动词，其中两个是"打印提示后走自己完整的一套 flag 解析 + 分派逻辑"的别名。
`cmd_story.go:126–157` 与 `cmd_analyze.go:196–257` 是同一件事的两份手写实现，互斥规则已经不一致：

| 规则 | `cmdStory` | `dispatchAnalyze` |
| --- | --- | --- |
| `-corpus` 与其他互斥 | 显式检查 | 由 `selectorCount > 1` 覆盖 |
| `-render-all` 与选择器互斥 | 无（`-render-all` 与 `-journey` 可共存，后者优先） | 显式拒绝 |
| `-llm-addr` 批量拒绝 | `-render-all` / `-corpus` | `-corpus` / 默认套件 |
| `resolveLLMOptions` 调用 | 无条件（P9 执行记录承认这是缺陷） | 按需（已修） |

最后一行是关键证据：**P9 已经在 analyze 侧修掉了一个缺陷，而 story 侧的同一个缺陷原样留着。**
两份实现分叉不是理论风险，它已经发生了。

**"别名"这个词今天在代码里就是不准确的，而且代码自己已经承认了。** `cmd_analyze.go:74-96` 的 flag
集合里**没有**任何"只跑宏观半区"或"只列候选"的开关，`dispatchAnalyze` 的 `default` 分支无条件
先跑 story 半区再跑 report 半区。因此：

- `vmr report <args>` → **无等价的 `vmr analyze` 写法**（analyze 一定会渲染 journey）；
- `vmr story`（不带 selector）→ 只列候选不渲染，analyze 无对应模式。

而 `cmd_report.go:437` 的迁移提示原文是：

> `vmr report: deprecated alias — … if you only want the macro report, vmr report remains fully supported.`

**同一句话里既称自己是 deprecated alias，又说 remains fully supported。** 这不是文档漂移，
是代码与自己的措辞打架。

要让"别名"名副其实，`vmr analyze` 需要补上这两个缺失的模式；补上之后别名才能真正薄化成
"解析 args → 翻译为 analyze 等价 args → 调 `cmdAnalyze`"，分叉在结构上不再可能，`-llm-key`
不对称（`§1.34`）也随之真正消失。

> 有一种相反的意见值得记下并明确否决：**承认三条稳定入口、去掉 deprecated 叙述**。
> 它的论据是"实现不完整所以 alias 名不副实"——但这论证的是**应该补完**，不是**应该反悔**。
> `story_report_dev_plan_2_sonnet-5.md` §1 记载的是用户已拍板"真正收敛为一个入口"，
> 以"实现没做完"为由推翻一次产品决策，方向是反的。

### 2.5 架构文档自身：四条被实践修正的结论

用户要求"如果原始设计方案本身存在问题，也要直言不讳"。四条：

1. **§7.4 的"< 50KB"是一个从未成立的估算——而且不是样本错配。**
   §7.4 原文是 `预期体积：298KB → **< 50KB**（脊柱现为 42.8KB，加链接与三条补齐）`，其中
   298,732 与 42,809 两个数字来自 §2.2 的表格，表头写明样本是 **22 轮、33 次工具调用的
   openclaw journey**。**即：`<50KB` 恰恰是对这条 journey 做出的预测。** 实测 M4 同一条 journey
   今天是 **107KB**——**同一样本上 2 倍以上的偏差**。所以它不能用"复杂任务当然比简单任务大"来解释，
   它就是一次系数估错（按"脊柱基本不变"推算，没有把"每步都要展示工具参数与结果"算进去）。
   **处置**：把这个数字从"目标"改写成"当前实测区间 + 唯一剩余的减法是把工具结果也降级为引用"。
   **已在本次完成。**
2. **§7.6c 的四条处置只覆盖了"份数"，没覆盖"每份的量纲"。** §2.2 已论证。这不是实现走样，是
   设计期的盲区——文档把 `details/*.json` 当成唯一的逐字复制，但 `.md` 里的 Raw SSE 块是同一性质
   的另一半，历史消息重复是第三半。**已在本次补入 §7.6c 第 5 条。**
3. **§7.5"两类边两种策略"里的第二类判据被 P7.1 换成了 flag。** §2.3 已论证。**已在本次补注。**
4. **§7.6c 用来论证"不做 GC"的量级判断已被实测超过。** 原文"共享条目去重后的基数是个位数到几十"
   vs 实测 M11 四日抽样 60 个 sysprompt + 11 个 tools。**结论不变**（71 个文件 / 512KB 不值得引入
   生命周期管理），但支撑它的数字应换成实测值。

另有两条**已有注记、无需再动**的：§7.9 的"一次扫描、一份缓存、一次建图"至今是顺序调用两遍
（有明确注记）；§7.9 的"收敛为一个动词"在 P9 已经落地，注记已在本次结案。

### 2.6 死代码、冗余注释与守卫的覆盖缺口

`deadcode` 全仓扫描 + 人工核对生产路径引用（排除只被本包测试引用的情况）：

| 项 | 行数 | 判定 |
| --- | --- | --- |
| `ctxgraph/blobindex.go` 整个文件 | 125 | **完全废弃**。`Lookup`/`Len`/`FetchAll` 生产与测试双零引用；`records.go:33` 的注释明写"use this instead of BlobIndex.FetchAll"。但 `buildGraph`（`scan.go:79/92`）仍在每次扫描时构造并逐哈希填充它（单日语料 22,135 次 `firstSeen`，852 个唯一键）。成本量级很小（全量约 2.6MB map），**问题不在性能，在于一个 125 行的废弃子系统还挂在 `Graph` 的公开字段上** |
| `report.Build`（`build_cached.go:18`） | 28 | 生产零引用，`BuildCached` 是唯一路径 |
| `report.WriteDetails`（`detail.go:233`） | ~50 | 生产零引用。它曾是架构文档 §4.8 论证"此路不通不成立"的三条证据之一，P2/P3 走了更简单的路之后没人回头删它 |
| `report.AnalyzeSessions`（`session.go:191`） | 3 | 生产零引用，`AnalyzeSessionsCached` 是唯一路径 |
| `ctxgraph.Scan`（`scan.go:39`） | ~28 | 生产零引用，`ScanCached` 是唯一路径 |
| `story.Build` / `story.PreviewTitle` | 各 ~20 | 生产零引用，批量版（`BuildAll`/`PreviewTitles`）是唯一路径 |
| `chatmsg.CheckToolPairing` + `PairingReport` | 97 | 只被 `story/invariants_test.go` 引用。**这一条建议保留**——它是 F9 不变量的可执行断言，属于有意的测试基础设施，已在本次于 doc comment 里写清楚 |
| `chatmsg.ExtractFinish`、`cmd/vmr.configFlag`、`health.Registry.Available`、`reqdetail.ErrorClass`、`reqdetail.contentHash8` | 各 <15 | 零引用小函数 |

前六项是同一个模式：**一个函数的缓存版/批量版成为唯一生产路径之后，非缓存版/单条版留在原地，
各自还带着测试。** 这是 P2/P3 两次大改留下的、成体系的一批。合计约 250 行生产代码 + 相应测试。

**注释债与守卫的覆盖缺口**（实测 M16）：生产 `.go` 文件（排除 `_test.go`）中有 **71 处**形如
`P1.1`/`P9.2` 的阶段号，分布在 **27 个文件**；**23 处**指向 `story_report_*.md` 的路径引用，
涉及 8 份文档，**当前全部可解析（0 死引用）**。

这批引用今天没坏，但它们处在一个特殊的位置：

- `archtest` 的 `doc_refs_test.go` **明确不守 `docs/future-strategy/`**（有文档化理由——历史评审
  报告正当地引用已删除的文件）；
- 而 P10 描述的终态**允许** ActionPlan 被归档出版本控制（已有四份被移到 `.gitignore` 排除的
  `archived/`）；
- 于是这 23 处是**指向一个被允许消失的目录的、无守卫的交叉引用**。

本次复查里已经发生过三处同类失效：源码注释引用 `internal/report/render.go`，而该文件在 P2 已移为
`internal/reqdetail/render.go`（见 §4 第 2/3/4 项）。**同一个失效路径写在 `KNOWN_ISSUES` 里会被
守卫当场拦下，写在源码注释里躺了两个阶段没人发现**——这个对照见 §4.1。

阶段号本身不必一刀切删除（部分是"这个反直觉写法是哪次论证的产物"的唯一线索，删掉等于删可追溯性，
这正是 P10.1 拒绝"简单删掉引用"的同一理由）。**要收敛的是路径引用：要么进守卫，要么改为不依赖
路径的措辞。**

### 2.7 【中】人读产物的内容转义在两个包之间已经分叉

`internal/reqdetail` 与 `internal/story` 都用 `<details><summary>用户内容</summary>` 这个模式。
`reqdetail` 转义（`render.go:41` 的 `escapeHTML`，在 `renderMessageSection`/`ToolCallArgsChars`
等处调用）；`internal/story` **三处都不转义**：

| 位置 | 注入点 |
| --- | --- |
| `render_spine_args.go:194` `payloadBlock` | `<summary>` 放工具参数原文前 160 字符 |
| `render_spine_step.go:111` `toolResultLine` | `<summary>` 放工具结果原文前 160 字符 |
| `render_spine_step.go:86` `foldWhyLine` | `<summary>` 放 `RespText`/`Reasoning` 原文 |

真实语料上已经观察到后果：`journey-j-pimini-…-754b71e2.md` 第 42 行的 summary 是
`<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> <!-- keywords: …`——工具结果的开头是一段 HTML 注释，
渲染时 summary 的这部分被浏览器**静默吞掉**。读者不会知道自己少看了东西。同类风险还包括内容里
出现 `</summary>` / `</details>` 直接破坏折叠块结构。

两处 inline 分支（`payloadBlock` ≤120 字符单行、`foldWhyLine` 短文本）同样把原文裸写进 Markdown
正文，未转义也未 fence——内容里的 `*`/`_`/`[](…)` 会被当作 Markdown 语法。

值得注意的是 `story/render_md.go` 的 `codeFence` doc comment 花了七行论证"故意重复而不共享，
因为两份拷贝不会以读者能察觉的方式漂移"。这个论证对 `codeFence` **本身**成立（两份实现确实一致），
但它掩盖了一件事：**整个折叠块渲染模式（fence + summary + escape）在两个包各写了一遍，而它已经
漂移了**——漂的不是 `codeFence`，是它旁边那个没被一起复制的 `escapeHTML`。

### 2.8 【最重】详单的"跳过已存在文件"谓词假设"同名即同内容"，实测在两个维度上不成立

**这是本次复查唯一一条会产出错误内容（而不只是多余内容）的缺陷。**

`reqdetail.EnsureRendered`（`ensure.go:41-65`）的跳过条件是 `os.Stat(target) == nil`。
它的 doc comment 在自己的论证中间写下了自己的反证：

> A pre-existing file with this exact name is guaranteed byte-identical to what Render would produce
> now — the name is a pure function of (rec.TS, rec.Model, RealModel(rec), rec.Outcome, req) and
> **Render is a pure function of (rec, m, prev, prof, lang, linkEvidence)** — so an existence check
> is a correct and sufficient skip condition, not an approximation

同一句话同时说明了：文件名不含 `lang`/`linkEvidence`，而 `Render` 依赖它们。结论因此不成立。

**实测 M8（语言维度）**：同一输出目录先 `-lang en` 后 `-lang zh` 渲染同一条 journey——
journey 报告变成中文（"决策脊柱"），而它链接的 **22 份详单全部仍是英文**
（`## ① Client → VMR Request`）。与全新中文目录生成的同名文件 `diff` → **DIFFERENT**
（新目录里是 `## ① Client → VMR 请求`）。

**实测 M9（evidence 模式维度）**：删掉 `evidence/` 后重跑同一条 journey——evidence 目录
**未重建**（0 文件），因为 evidence 的生成位于 `EnsureRendered` 的"文件不存在"分支内，
而 22 份详单都命中了跳过。结果：journey 报告头部 1 条 + 22 份详单各 2 条 = **45 条链接永久失效，
且任何次数的重跑都不会修复**。

**第三个维度（哈希碰撞）在实测上是安全的，但机制相同**：M10 在全量 11,274 条上测得 hash8
**0 次碰撞**、文件名 **0 次碰撞**。而且详单文件名的区分度并不只来自 hash8——前缀
`ts_virtual_real_outcome` 自身已近乎唯一（11,264/11,274，仅 10 个前缀被两条记录共享），
所以真实的文件名碰撞需要"hash8 碰撞 ∧ 前缀相同"，条件概率约 `10 × 2⁻³² ≈ 2×10⁻⁹`。
证据层（`evidence/<kind>-<h8>.md`）那里 h8 确实是唯一判据，但实测命名空间只有 71 项（M11），
先验碰撞概率约 `6×10⁻⁷`。**结论：不需要换更长的哈希**——换 SHA-256 要动四处命名口径、
架构文档写死的哈希惯例并让全部既有产物失效，为一个 `10⁻⁹` 量级的风险付这个代价不成比例。
**但碰撞一旦发生，后果与前两个维度完全相同：静默返回错内容。**

**根因**：谓词 `文件存在 ⇒ 内容正确` 本身是错的。它把"记录身份稳定"（确实成立）当成了
"渲染表示稳定"（不成立）。三个维度是同一个缺陷的三种触发方式，应当一次修好，不是三个独立问题。

**修法方向**（具体实现留给 ActionPlan）：把跳过谓词从**假设**改成**校验**——在详单页面里写一行
机器可读的渲染指纹（语言 + evidence 模式 + 模板版本），命中 `Stat` 后读该行比对，不匹配就重写。
这保留了"文件名只凭 Manifest 就能算出、无需 I/O"这条 P2 建立的核心性质，代价是每次跳过多一次
几十字节的读——相对于它今天可能返回错误内容，这是必要成本。

两条明确否决的备选，写下来以免被当成遗漏重提：

- **把 `lang` 编进文件名**：会让同一条记录在两种语言下有两个地址，直接破坏 P2 的"一条记录一个稳定
  地址"，并让脊柱链接与 `RequestRow.detail_file` 都要跟着分叉。
- **每份详单配一个 sidecar 元数据文件**：产物文件数翻倍，而 P3.1 刚为了消除同构副本删掉了
  `details/*.json`。

**这一条是 §2.2 的硬前置**：§2.2 的两条减法会改变 `Render` 的输出而不改变文件名，在跳过谓词修好
之前，任何已经跑过 analyze 的输出目录都会**永久复用旧详单**，新减法一次都不会生效
（唯一的绕过方式是手动 `rm -rf details/`，那不是可交付的方案）。

### 2.9 【中】索引的"显示"策略与套件的"渲染"策略对 `cron` 的判定相反

项目里有两条独立的"重要 vs 噪声"分类线，它们**对同一个类别给出了相反的答案**：

| 策略 | 位置 | 对 `cron` 的处理 |
| --- | --- | --- |
| P6.3 索引**显示** | `storyindex.go:252` — 只折叠 `heartbeat` 与 `subagent` | **不折叠**，与 task 并列显示在首屏表格 |
| P9.2 默认**渲染** | `cmd_analyze.go:40` `taskOnlyCandidates` — 只渲染 `CategoryTask` | **不渲染**，没有 journey 报告 |

**实测 M13**（单日 07-15 默认 `vmr analyze`）：候选 12 task + 10 cron + 1 heartbeat；
`vmr-stories.md` 首屏可见 **22 行**，其中**只有 8 行有 journey 链接**——
**10 个 cron 行全部以一等公民身份显示在首屏，且全部点不进去。**

索引把 cron 推到读者眼前，套件却不给它任何可点的东西。而 §7.7 设立类别列的**全部目的**就是
"让索引成为可用的下钻落地页"。两条策略各自都有道理，合起来正好互相拆台。

**实测 M12 让"哪些才是噪声"这个问题有了数据答案**：

| 类别 | 候选数 | 请求数 | 单条最大请求数 | ≥10 请求的候选数 |
| --- | ---: | ---: | ---: | ---: |
| task | 238 | 9,274 | — | — |
| **cron** | 112 | 1,032 | **30** | **44** |
| heartbeat | 107 | 229 | 7 | **0** |
| **subagent** | 20 | 563 | **91** | **16** |

三类"噪声"里**只有 heartbeat 的噪声定性被数据支持**（最大 7 个请求、没有一条 ≥10）。
cron 有 44 条 ≥10 请求；subagent 更极端——20 条里 16 条 ≥10，**最大一条 91 个请求，
是全语料单条最大的 journey**，比绝大多数 task 都长，却同时被折叠**又**不被渲染。

**修法方向**：让两条线合并成一条判据（显示即可渲染），并由数据定档——只把 heartbeat 归为噪声，
`cron`/`subagent` 与 task 同等对待。

**但这会把默认渲染量从 238 条抬到 370 条**，在当前"批量模式无条件物化每步详单"的实现下会显著放大
产物体积。**所以这一条必须排在 §2.1/§2.2 的体积修复之后**，否则是在给一个已知的体积问题加码。

一个明确否决的备选：**在分类器上叠加"cron 且 chain>3 且 ToolCalls≥10 就升级为 task"的复合阈值。**
§7.7 立类别列时的原话是"判据用已有的结构信号，**不引入新的猜测**"，而 `ToolCalls >= 10` 是一个
没有校准依据的新猜测；更重要的是它解决错了问题——`[cron:…]` 前缀是客户端自己打的，分类是准确的，
真问题是同一个准确的分类被两条策略各答了一次且答案相反。

### 2.10 【中低】三个 Finding 检测器在 99.48% 的语料上结构性沉默，而这一点没有向读者披露

`chatmsg.ToolResult.IsError` 的字段注释原文：
"Anthropic's explicit is_error field; **always false for OpenAI-shaped results**"
（`toolresults.go:19`）。`detectUnadaptedRetry` 与 `ErrorRecoveryCount` 等依赖它。

**实测 M14（全量 11,274 条）**：openai **99.48%** / anthropic **0.52%** / openairesponses 0.00%。
即这些检测器在**99.48% 的语料上结构性地无法触发**。

**取舍本身是正确的，且已经登记过。** `KNOWN_ISSUES §2.4` 有一条逐字针对此事的裁决：

> **不对 OpenAI 工具返回做 `error:` 关键字模糊嗅探**：经对真实生产语料库全量 **495,672 条** OpenAI
> 工具调用结果实测扫描，确认其全部为自由文本 stdout/stderr（包含 `{ "error": ... }` 等结构化 JSON
> 错误字段为 **0 条，占比 0.00%**）……

任何"加内容嗅探"的提案都被这条实测挡住，不必重新论证。**缺的不是能力，是披露**：
今天一份 Findings 输出里"没有 `error_retry_unadapted`"对读者意味着两种完全不同的事——
"检查过了，没问题"和"这个检测器对你这批数据结构性沉默"——而产物里没有任何地方区分它们。

这是一条零推断、低成本的诚实性改进：在 Findings 章节标注哪些检测器对本批数据不适用及原因。
它与 §5.6 的"推断与事实的分界线"纪律完全兼容——披露覆盖率不引入任何新判据。

### 2.11 复查方法论：四轮验证共享同一个盲点

本次复查之后，另有三份独立审阅报告（gpt-5.6-terra / Gemini 3.7 Flash / Sonnet 5）对同一批
commit 做了各自的复核。逐项核实的过程与结论记录在 `story_report_review_triage_opus-5.md`。
其中一条横向观察值得写进本文，因为它解释了前面几乎所有"最重"条目为什么能存活到今天：

**三份独立报告，没有一份测量过默认 `vmr analyze` 的产物体积。**

- 一份明确断言"内存峰值完全可控"，依据是一个虚构的候选数量（"通常仅 2~10 条真实任务"
  vs 实测 M12 的 238 条）；
- 一份准确描述了 `EnsureJourneyDetails` 的"幂等补齐"机制，但没有把这个机制乘以 238 条 journey 的规模；
- 一份完全未涉及。

三份都以"逐文件读代码 + 跑测试套件"为方法，而**产物体积不是任何一个测试断言的对象，也不是任何一个
函数读起来会显眼的性质**——它只在"把机制乘以真实规模、然后真的跑一遍去量"时才出现。

这与 §2.1 记录的"三次验收漂移"是同一个机制的第四次显现：**验收与复查的对象是"代码做了什么"，
而不是"跑一遍产出了什么"。** P4.2 是全仓唯一一条把设计纪律本身写成常驻测试的任务（R10），
也是唯一一条至今没有退化的纪律——这不是巧合。

**推论直接写进第 6 章的通用完成定义**：每个阶段的验收都必须包含一次"在默认路径上跑真实语料并
测量产出"的步骤，且凡是可以表达成断言的纪律，都要落成常驻测试而不是验收清单上的一行字。

---

## 3. 问题打包

按严重程度排序。每个 Package 内部相关、Package 之间独立可分别交付与验证。
"需拍板"列标出动手前需要用户决策的条目——这一列的存在是为了避免 P6.5 那次"ActionPlan 写了
建议落地前确认、实际执行时跳过"的重演。

### Package A — 证据层体积纪律归位（高）

| 项 | 内容 | 验收 | 需拍板 |
| --- | --- | --- | :---: |
| A1 | `writeJourneyFile` 区分单条下钻与批量渲染，批量模式只挂链接不物化详单（§2.1） | 默认 `vmr analyze` 的 `details/` 为空或仅含显式请求的记录；`-journey <单条>` 与 `-details`/`-render-all` 行为不变 | — |
| A2 | 删除详单里的 `Raw SSE, full` 全文块，改为带 `req` 坐标的取用提示（§2.2 第 1 条） | 同一批详单体积降约 41%；重组后的模型输出、reasoning、tool_calls 一字不改 | — |
| A3 | 详单 ① 段只渲染 `deltaStart` 之后的消息，历史段落折叠为一行指向 `PrevTurnLink`（§2.2 第 2 条）；`prev == nil` 时仍全文渲染 | 再降约 52%；`vmr report -details` 与 `vmr analyze -journey` 两条路径生成的详单仍逐字节相同（P2 核心不变量） | ⚠️ 改变"单份详单自包含"的性质 |
| A4 | §8 文案与 `detailCell` 的判据从 `detailsOn` 改为目标存在性（§2.3）——若 A1 落地则复核是否自动消失 | `vmr-report.md` §8 的陈述与实际产出一致 | — |
| A5 | 撤回 `KNOWN_ISSUES §1.32` 的"已闭环"判定 | 条目状态与实测一致 | — ✅ 已完成 |
| A6 | 索引显示与默认渲染两条分类线合并；只把 heartbeat 归为噪声（§2.9） | 索引首屏每一行都可点；默认渲染范围与折叠范围由同一判据推出 | ⚠️ 改变默认产出范围与体积 |

**硬依赖**：A2/A3 依赖 **Package B 的 B4**（跳过谓词改为校验）——见 §2.8 末段：
在谓词修好之前，改变 `Render` 输出而不改变文件名，在已有输出目录上永远不会生效。
A4 依赖 A1 的结论。A6 依赖 A1–A3（否则是给已知的体积问题加码）。

**这个 Package 必须补一条常驻守卫**，否则它会第四次被绕过：一条断言"默认套件模式产出的
`details/` 文件数为 0（或不超过显式请求数）"的端到端测试，与 P4.2 的体积守卫同级。

### Package B — 人读产物的渲染正确性（高）

**这个 Package 的共同点是"产出了错误内容"，而不是"产出了多余内容"。**

| 项 | 内容 | 验收 | 需拍板 |
| --- | --- | --- | :---: |
| B1 | `internal/story` 的三处 `<summary>` 注入点转义 `< > &`（§2.7） | 含 HTML 注释/标签的工具结果，summary 完整可见 | — |
| B2 | 两处 inline 分支的裸文本转义或改走 fence（§2.7） | 含 Markdown 元字符的短参数按字面显示 | — |
| B3 | `escapeHTML` 的归属定一次：要么两个包各留一份并在注释里互相点名（同 `codeFence` 的既有处理），要么下沉 | 两个包对同一模式的处理一致，且这条一致性有注释或测试记录 | — |
| B4 | **详单跳过谓词从"假设"改为"校验"**：页面内写入渲染指纹（语言 + evidence 模式 + 模板版本），命中 `Stat` 后比对，不匹配即重写（§2.8） | 同目录先 EN 后 ZH，全部详单变为 ZH；删除 `evidence/` 后重跑，证据文件与链接全部恢复；`FileNameForManifest` 仍无需 I/O 即可算出链接 | — |

回归测试用真实反例钉住：B1–B3 用 `journey-j-pimini-…-754b71e2` 的那条 `read` 结果；
B4 用"先 EN 后 ZH"与"删 evidence 后重跑"两个端到端场景。

**B4 是 Package A 的 A2/A3 的硬前置**，因此 Package B 应整体排在 Package A 的减法之前。

### Package C — CLI 入口收敛收尾（中）

| 项 | 内容 | 验收 | 需拍板 |
| --- | --- | --- | :---: |
| C1 | `vmr analyze` 补上缺失的两个模式（只跑宏观半区 / 只列候选），使别名的产出真正可由 analyze 表达（§2.4） | 每种现有 `vmr report`/`vmr story` 调用都有等价的 analyze 写法，产物逐字节相同 | ⚠️ 新增 flag 的拼写与语义 |
| C2 | 别名薄化：`cmdReport`/`cmdStory` 改为"解析 args → 翻译为 analyze 等价 args → 调 `cmdAnalyze`" | 两个别名对任意既有调用方式产物逐字节不变；`cmd_story.go` 的分派分支删除 | — |
| C3 | `§1.34` 的 `-llm-key` 不对称随 C2 真正消失 | `vmr report -llm-key X` 与 `vmr analyze -llm-key X` 的自指流量排除口径一致 | — |
| C4 | 修正 `cmd_report.go:437` 自相矛盾的迁移提示（同一句既称 deprecated alias 又称 remains fully supported） | 提示文案与代码实际能力一致 | — |
| C5 | DevPlan2 P9.1 的验收标准"收敛三套 flag 集合为一套"如实回填当前状态 | 文档与实现一致 | — |

C1 是 C2 的前置：在 analyze 补齐两个模式之前，薄化会改变别名的产出。
C4 可以先做，它不依赖任何其他项。

### Package D — 清理与守卫（中低）

| 项 | 内容 | 验收 | 需拍板 |
| --- | --- | --- | :---: |
| D1 | 删除 `ctxgraph/blobindex.go` 及 `Graph.Index` 字段与 `buildGraph` 的填充循环 | 全量测试绿；扫描产物逐字节不变 | — |
| D2 | 删除六个"非缓存版/单条版"死函数及其测试：`report.Build`、`report.WriteDetails`、`report.AnalyzeSessions`、`ctxgraph.Scan`、`story.Build`、`story.PreviewTitle` | 同上；`deadcode` 复扫这六项消失 | — |
| D3 | 删除零引用小函数：`chatmsg.ExtractFinish`、`cmd/vmr.configFlag`、`health.Registry.Available`、`reqdetail.ErrorClass`、`reqdetail.contentHash8` | 同上 | — |
| D4 | `archtest` 的文档引用守卫扩展到源码注释里的 `internal/<pkg>/<file>.go` 路径（§2.6） | 三处 `internal/report/render.go` 式的过期引用若再出现会被测试挡下 | — |
| D5 | `docs/future-strategy/` 里自称"当前"的文档补状态标注；生产源码指向该目录的 23 处引用要么进守卫、要么改为不依赖路径的措辞（§2.6、R22） | 不存在"自称现行、内容停在 P0 之前"的文档；源码里的规划文档引用有守卫或不依赖路径 | — |

D1–D3 合计约 250 行生产代码 + 测试。**保留 `chatmsg.CheckToolPairing`**——它是 F9 的可执行断言。
D4/D5 是防止这一类问题复发的那一半。

**D4 应尽早做**：它零风险、成本极低，且能挡住后续每个阶段引入的新过期引用。

一个明确否决的备选：**给所有 `docs/future-strategy/` 文档加路径存在性检查脚本。**
与 `doc_refs_test.go` 排除该目录的既有理由冲突（历史评审正当地引用已删除文件，全目录扫描会对大量
合法引用报警）。守卫的对象应该是"自称当前"的少数文档，不是整个目录。

### Package E — 分析结论的诚实性（中低）

| 项 | 内容 | 验收 | 需拍板 |
| --- | --- | --- | :---: |
| E1 | 在 Findings 输出中标注哪些检测器对本批数据结构性不适用及原因（§2.10） | 一份 openai-only 语料的报告里，读者能分辨"检查过没问题"与"检测器无法触发" | ⚠️ 呈现形态 |

明确不做：**给 OpenAI 工具返回加内容嗅探。** `KNOWN_ISSUES §2.4` 已用 495,672 条实测登记
（目标 JSON 形状出现 0 次），且它会把推断引入 Findings 的证据基础，违反 §5.6 的分层纪律。

### Package F — 文档回填（低成本，纯文档）✅ 本次已完成

| 项 | 内容 | 状态 |
| --- | --- | --- |
| F1 | 架构文档 §7.4 的"< 50KB"作废，改为当前实测区间 + 唯一剩余减法（§2.5 第 1 条） | ✅ |
| F2 | 架构文档 §7.6c 补第 5 条「同一把刀砍进详单内部」（§2.5 第 2 条） | ✅ |
| F3 | 架构文档 §7.5 补注 P7.1 用 `detailsOn` 近似存在性判断的后果（§2.5 第 3 条） | ✅ |
| F4 | 架构文档 §7.9 的"开放决策"结案——P9 已落地 | ✅ |
| F5 | `KNOWN_ISSUES` 登记新条目、撤回 `§1.32` 的闭环判定、ROI 表与分布重算 | ✅ 见 §4.2 |

---

## 4. 本次直接处理的问题（已解决）

以下属于"错别字/引用错误/小修改"，按任务要求在复查中直接改掉。全部改动通过
`go build ./...`、`go test ./...`、`go test ./internal/archtest/...`、`gofmt -l`、`go vet ./...`。

| # | 问题 | 文件 | 处理 |
| --- | --- | --- | --- |
| 1 | `DetailWriter` doc comment 说"writes one .md + one .json"，与同文件 `writeOneDetail` 的 P3.1 说明自相矛盾 | `internal/report/detail.go:93` | 改为"one .md"，并点名 P3.1 |
| 2 | 注释引用已不存在的 `internal/report/render.go`（P2 移为 `internal/reqdetail/render.go`）；且其中提到的 `fmtBytes` 私有函数本身也已被 `fmtutil.FmtBytes` 取代 | `internal/fmtutil/fmtutil.go:52` | 去掉死引用，改述为"FmtBytes 已建立的同一模式" |
| 3 | 同上（迁移来源叙述） | `internal/chatmsg/messages.go:10` | 改述为"那个文件此后又移到了 internal/reqdetail/render.go" |
| 4 | 同上；且同一段 doc comment 内部自相矛盾——第 122 行说"plus an optional lang tag"，第 133 行说"No longer takes a language tag"（P5.1 只追加没删除） | `internal/story/render_md.go:117` | 修正引用目标，两处矛盾合并为一句 |
| 5 | `spineStepHeader` 在第 194 行无条件解引用 `s.Manifest.TS`，第 199 行才做 `s.Manifest != nil` 检查——防御性检查永远救不了它 | `internal/story/render_spine_step.go:192` | nil 检查提到时间戳之前，两处统一；补注说明生产路径保证非 nil、守卫是给测试 fixture 的 |
| 6 | `-report-config` 的 help 文案仍写"vmr report/vmr story sidecar config yaml"，P9 之后主入口是 `vmr analyze` | `cmd/vmr/cmd_report.go`、`cmd/vmr/cmd_story.go` | 改为"vmr analyze's sidecar config yaml (shared with this alias)" |
| 7 | `chatmsg.PairingReport` 的 F9 doc comment 断言"100% pairing rate is an invariant of the data"，而架构文档 §5 已用五个真实日志文件证明 OpenClaw 家族客户端上精确匹配率是 0% | `internal/chatmsg/pairing.go:13` | 补写清楚：不变量成立于"记录下来的 id"，这个 checker 是严格形式（供 F9 合成 fixture 回归测试用），配对真实客户端流量的调用方要用两趟形式；并点名 `story.toolResultsFor` |
| 8 | `i18n.DetailText.NoValue` 字段声明并赋值两次（EN/ZH），全仓零引用 | `internal/i18n/reqdetail_detail.go` | 删除字段与两处赋值 |
| 9 | `CLAUDE.md` 的 `i18n` 行只提 `report`/`story`，未提 P2 新增的 `i18n/reqdetail_detail.go` | `CLAUDE.md` | 补齐三个 `i18n/*` 与其对应包的邻接关系 |
| 10 | **`KNOWN_ISSUES` 的 `## 2` 章节标题完全缺失**——全文有 12 处引用 `§2`/`§2.x`，`§4` 的引言还专门写"§2 是已经论证过的刻意取舍"，但该章节只有引言和 `### 2.1`，没有二级标题 | `docs/KNOWN_ISSUES_sonnet-5.md` | 补上 `## 2. 刻意取舍，不是缺陷` |

（第 6 项涉及两个文件，故编号 10 项、表述 9 类。）

### 4.1 一次意外的守卫现场演示

登记 §2.6 的死代码条目时，条目正文里写了一句"三处引用已不存在的 `internal/report/render.go`"——
`archtest` 的 `TestArchitecture_DocReferences` 立刻对**这句描述本身**报错，因为
`KNOWN_ISSUES_sonnet-5.md` 在它的守卫范围内。

这件事有两层意思，都值得记下来：

1. 守卫是有效的——它在同一次会话里挡住了一处新引入的死引用，用的正好是被复查对象的那个缺陷。
2. 它也划出了自己的边界：**守卫覆盖文档，不覆盖源码注释**。同样是 `internal/report/render.go`
   这个失效路径，写在 `KNOWN_ISSUES` 里会被立刻挡下，写在 `fmtutil.go`/`messages.go`/
   `render_md.go` 的注释里躺了两个阶段没人发现（§4 第 2/3/4 项修的就是它们）。
   Package D 的 D4 因此不是"顺手加的"——它是这次复查里唯一一个**同一个缺陷在守卫内外各出现一次、
   结果完全不同**的对照实验。

### 4.2 登记进 `KNOWN_ISSUES` 的新条目

按 `CLAUDE.md` 的"在源码注释里论证过但没登记的取舍，等于没论证"，本次复查的全部发现已登记：

| 条目 | 级别 | 内容 | 对应 Package |
| --- | --- | --- | --- |
| `§1.35` | 高 | 证据层"默认按需"在 `vmr analyze` 上从未成立 | A |
| `§1.36` | 高 | 详单内部约 93% 是重复拷贝 | A |
| `§1.37` | 中 | `internal/story` 三处 `<summary>` 未转义 | B |
| `§1.38` | 中 | 别名保留完整分派逻辑、已分叉 | C |
| `§1.39` | 中低 | P2/P3 留下的成体系死代码（约 250 行）+ 守卫覆盖缺口 | D |
| `§1.40` | 低 | 归一化配对落在 `story` 而非 `chatmsg`（真源外移） | — |
| `§1.41` | 高 | 详单跳过谓词假设"同名即同内容"，实测在语言/evidence 两维度不成立 | B |
| `§1.42` | 中 | 索引显示策略与默认渲染策略对 `cron` 判定相反 | A |
| `§1.43` | 中低 | 三个 Finding 检测器在 99.48% 语料上结构性沉默且未披露 | E |

同时：
- **撤回了 `§1.32` 的"已闭环"判定**（§3 第 31 项就地订正 + `§0` 分布重算）——它闭环的是 SIGKILL
  这一个症状，不是纪律本身。
- `§4` ROI 表补入相应行；`§4.2` 的"高 ROI 0 条"改写，并把四次绕过的机制作为教训写进去。
- 架构文档四处回填已落地（见 Package F）。

---

## 5. 结论

**架构目标本身已经实现。** 3×2 矩阵的四个错格全部补齐、坐标层贯通两个半区（全量 11,274 条实测
坐标与文件名零碰撞）、共享证据层生效、导航矩阵六条边在真实语料上全通、机读层的内联/引用边界有
常驻守卫、JSON 语言策略统一、CLI 收敛到单一入口。十个阶段的执行记录基本诚实——多处主动登记了与
设计的落差（P5 的体积、P6.5 的收窄、P9.2 的根因推翻），这在一个自我评估的序列里不常见。

**但有两类问题穿透了全部四轮验证。**

**第一类是体积纪律**：架构文档 §7.6c 的"证据层默认按需、体积与信息量相称"在 P3 对 `vmr report`
成立过一次，随后被 P6.5 的 `analyze` 抵消、被 P9.2 的错误根因诊断放过。今天推荐入口的默认行为是：
单日 322 条记录写 164MB，全量语料写 3.5GB——**而其中约 93% 是同一批字节的第二份、第三份拷贝**。
这与项目自己反复引用的第一性原理（blob 只存一份，tree 只持引用）正面冲突，且两条修法所需的机制
（`req` 坐标 + 读取原语、`PrevTurnLink`）**都已经在仓库里，只是没有被用来做减法**。

**第二类是渲染表示的正确性**：详单的"跳过已存在文件"谓词假设"同名即同内容"，而实测证明它在语言
与 evidence 模式两个维度上都不成立——同一次 `-lang zh` 运行会产出中文报告 + 英文详单；删掉
`evidence/` 后重跑，45 条链接永久失效且再也修不回来。这一类比第一类更严重：它产出的不是多余内容，
是**错误内容**。它同时是第一类修法的硬前置——在谓词修好之前，任何对详单内容的减法在已有输出目录上
都不会生效。

**四轮验证共享同一个盲点，这比任何一条具体缺陷都值钱**：P3.3、P6.5、P9.2 三次阶段验收，加上三份
独立审阅报告，没有一次把"跑一遍默认命令然后量一下产出"当成验收动作。验收对象始终是"代码做了什么"，
而不是"那条纪律现在还成立吗"。P4.2 是全仓唯一一条把设计纪律本身写成常驻自动检查的任务，
也是唯一一条至今没有退化的纪律——第 6 章的通用完成定义因此把"默认路径实测"与"纪律落成常驻测试"
写成了每个阶段的硬性要求。

**次要但真实的一批**：索引的显示策略与渲染策略对 `cron` 判定相反（首屏 10 行可见、0 行可点）、
脊柱的 `<summary>` 未转义（已在真实语料上吞掉内容）、CLI 别名的分派逻辑已出现分叉（P9 在一侧修掉
的缺陷在另一侧原样留着）、三个 Finding 检测器在 99.48% 的语料上沉默而未披露、P2/P3 留下的一批
成体系死代码（约 250 行）。

**架构文档的四处回填已在本次完成**，其中"< 50KB"这个数字直接作废而不是继续挂着——实测证明它与
预测针对的是同一条 journey，偏差 2 倍以上，是一次系数估错，把它留在文档里只会让下一个读者以为
还有一项未完成的工作。

---

## 6. 后续开发计划（P11–P15）

### 6.1 本章定位

沿用两期 DevPlan 的三层文档结构，本章占据中间那一层：

| 文档 | 回答什么 | 细节粒度 |
| --- | --- | --- |
| `story_report_architecture_opus-5.md` | 为什么这样设计、事实依据是什么 | 方案级 |
| 本文第 1–5 章 | 落地情况如何、问题在哪、根因是什么 | 复查级，含实测数据 |
| **本章（DevPlan）** | 分几个阶段做、每个阶段的边界与验收 | **里程碑级，不涉及源码细节** |
| 各阶段 ActionPlan | 这一阶段具体动哪些地方、怎么改 | 执行级，**每个阶段开工前单独编写** |

**为什么 ActionPlan 不现在写**：沿用第一期 DevPlan §0 的理由——每个阶段验收后都会改变下一阶段的
前提。P9 的执行记录就是活例子：它按计划做完"范围收窄"后才发现那不足以解决 SIGKILL，必须追加批处理。
现在写下的源码级细节，到那时多半已经失效。

**约定**：每个阶段开工前，基于该阶段起点的**真实仓库状态**重新做一次分析，产出该阶段的 ActionPlan；
不沿用本章对后续阶段的任何预判。

### 6.2 总体思路

第一期（P0–P6）建立了坐标层、共享证据层与导航矩阵；第二期（P7–P10）补正确性、统一 JSON 语言策略、
收敛 CLI 入口。**两期都在"建立机制"，本期（P11–P15）的主题是"让机制在默认路径上真的成立"。**

三条主线：

1. **先修"错的"，再修"多的"。** 产出错误内容（渲染表示不一致）优先于产出多余内容（体积），
   而且前者在技术上就是后者的前置——跳过谓词不修，任何内容减法都不会生效。
2. **让两条互相拆台的策略合并成一条。** 索引显示与默认渲染对同一批候选给出相反答案；
   合并的前提是体积先降下来，否则扩大渲染范围是加码。
3. **把纪律写进测试，把清理写进守卫。** 本轮复查最贵的教训不是任何一条缺陷，而是"四轮验证共享同一个
   盲点"（§2.11）。凡是能表达成断言的，一律落成常驻测试。

**不做的事**（已在正文论证）：不换更长的哈希（§2.8）、不给 OpenAI 工具返回加内容嗅探（§2.10）、
不在分类器上叠加未校准阈值（§2.9）、不为 `docs/future-strategy/` 做全目录路径扫描（Package D）、
不反悔"收敛为一个入口"的方向（§2.4）。

### 6.3 阶段划分原则与通用完成定义

#### 划分原则

沿用第一期 DevPlan §2.1 的四条（按模块切不按工作量切、每阶段自身可验收、阶段之间只允许单向依赖、
前置阶段优先交付可见价值），不重复。本期补充一条：

- **凡是本轮复查中"被绕过过"的纪律，其修复阶段必须同时交付守卫。** 修复本身只解决这一次，
  守卫才解决下一次。

#### 通用完成定义

第一期 DevPlan §2.2 的六条（全量测试与架构边界检查通过、真实日志重跑肉眼核对、基线快照更新、
用户可见变化写入变更日志与用户指南、本阶段取舍登记进 `KNOWN_ISSUES`、阶段收尾做边界复核）
**原样适用**，不重复列出。本期**新增两条**，直接来自 §2.11 的方法论发现：

7. **默认路径实测**：验收必须包含一次"在**默认命令、默认参数**下跑真实语料并测量产出"的步骤，
   记录产物体积、文件数与关键链接的可达性。不得只验最全的那条路径（`-render-all`/`-details`），
   也不得只跑单元测试——P3.3/P6.5/P9.2 三次漂移与三份独立审阅报告的共同盲点正在于此。
8. **纪律落成断言**：本阶段声称恢复或建立的每一条纪律，凡是可以表达成断言的，都要落成常驻测试，
   而不是验收清单上的一行字。参照物是 `TestBuildStructure_VolumeBoundedByStepsNotProseLength`
   ——全仓唯一一条这样做的纪律，也是唯一一条没有退化的。

### 6.4 阶段总览

| 阶段 | 主题 | 核心交付 | 依赖 | 对应 Package |
| --- | --- | --- | --- | --- |
| **P11** | 清理与守卫先行 | 废弃代码清空、守卫覆盖源码注释与"自称当前"的文档 | — | D |
| **P12** | 渲染表示的正确性 | 详单内容与请求它的那次运行一致；人读产物不再静默吞内容 | P11（守卫先立） | B |
| **P13** | 证据层体积归位 | 默认路径产物回到索引量级，且这条纪律有常驻守卫 | **P12（硬依赖）** | A（A1–A5） |
| **P14** | 索引与渲染范围一致化 | 索引首屏每一行都可点；检测器覆盖率对读者可见 | P13 | A6、E |
| **P15** | CLI 入口收敛收尾 | 别名名副其实且结构上不可能分叉 | P11（其余独立） | C |

**三条硬依赖**（绕过会造成真实的正确性或返工损失）：

- **P13 依赖 P12** —— 详单的跳过谓词只看文件名。P13 的两条减法改变 `Render` 输出而不改变文件名，
  在谓词修好之前，任何已经跑过 `analyze` 的输出目录都会永久复用旧详单，减法一次都不会生效。
  唯一的绕过方式是让用户手动 `rm -rf details/`——那不是可交付的方案。
- **P14 依赖 P13** —— P14 把默认渲染范围从 238 条候选扩到约 370 条。在体积问题修好之前做，
  是给一个已知问题加码。
- **P15 的 C2 依赖 C1** —— 在 `vmr analyze` 补上"只跑宏观半区/只列候选"两个模式之前，
  把别名薄化成 analyze 的转发会改变别名的产出，违反"别名产出不变"这条 P9.3 就定下的约束。

**P11 排在最前**的理由不是它最重要，而是它**零风险、成本最低、且其守卫保护后面每一个阶段**：
D4/D5 建立之后，P12–P15 引入的任何新过期引用都会当场被挡下。同时它先清掉约 250 行废弃代码，
让后续阶段的读者面对的是一个更小的代码面。

**P15 与 P12–P14 之间没有硬依赖**，可以按资源情况提前或延后；但按第一期"不设并行分支"的约束，
它改的 `cmd/vmr` 文件与 P13 的 A1 有重叠，不能与 P13 同时进行。

### 6.5 阶段详述

---

#### P11 · 清理与守卫先行

**目标**：把 P2/P3 两次大改留下的废弃代码清空，并把守卫的覆盖面扩到本轮复查发现的两个缺口，
让后续每个阶段都在一个更小、更受保护的代码面上工作。

**范围**：`internal/ctxgraph`、`internal/report`、`internal/story`、`internal/chatmsg`、
`internal/reqdetail`、`internal/health`、`cmd/vmr` 的死代码删除；`internal/archtest` 的守卫扩展；
`docs/future-strategy/` 的文档状态标注。**不改变任何产物的字节内容**——这是本阶段最重要的边界。

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P11.1** 删除废弃子系统 | `ctxgraph/blobindex.go` 整个文件及其在 `Graph` 上的公开字段与扫描期填充循环 | 全量测试绿；对同一批日志的扫描产物逐字节不变 |
| **P11.2** 删除被取代的非缓存版/单条版 API | 六个生产零引用的导出函数及其专属测试 | `deadcode` 复扫这六项消失；保留 `chatmsg.CheckToolPairing`（F9 断言基础设施，已在注释写明） |
| **P11.3** 删除零引用小函数 | 五个 <15 行的零引用函数 | 同上 |
| **P11.4** 守卫扩展到源码注释的文件路径引用 | 让文档引用守卫也检查生产源码注释里的 `internal/<pkg>/<file>.go` 路径 | 人为引入一处失效路径，测试当场失败 |
| **P11.5** 规划文档引用与"自称当前"文档的状态归位 | 源码里指向 `docs/future-strategy/` 的路径引用要么纳入守卫、要么改为不依赖路径的措辞；`docs/future-strategy/` 里自称"现行"的文档补真实状态标注 | 不存在"自称现行、基线停在整轮重构之前"的文档 |

**阶段验收**：全量测试与架构守卫绿；同一批真实日志在本阶段前后跑出的全部产物**逐字节相同**
（这是本阶段唯一的功能性验收——它不该改变任何输出）；新守卫的失败路径经人为反例验证。

**为什么单独成阶段**：它是唯一一个"只减不增、且不碰产物"的阶段，风险最低，适合作为本期的起点；
而它交付的守卫是后面四个阶段的安全网。

---

#### P12 · 渲染表示的正确性

**目标**：让一份详单的内容与"请求它的那次运行"一致，而不是与"第一次生成它的那次运行"一致；
同时让人读产物不再静默吞掉内容。

**范围**：`internal/reqdetail` 的跳过谓词与页面指纹；`internal/story` 的三处折叠块渲染与两处
inline 分支；两个包之间转义辅助函数的归属。不涉及体积、不涉及产物集合（那是 P13）。

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P12.1** 跳过谓词从"假设"改为"校验" | 详单页面内写入机器可读的渲染指纹（至少：语言、evidence 链接模式、模板版本）；命中已存在文件后比对指纹，不匹配即原子重写 | 同一目录先 `-lang en` 后 `-lang zh`，全部详单变为中文；删除 `evidence/` 后重跑，证据文件与全部链接恢复；`FileNameForManifest` 仍可在无 I/O 的前提下算出链接目标 |
| **P12.2** 折叠块 summary 转义 | `internal/story` 三处把用户内容放进 `<summary>` 的注入点统一转义 | 含 HTML 注释/标签的工具结果，summary 完整可见 |
| **P12.3** inline 分支的裸文本处理 | 两处短文本直接进 Markdown 正文的分支，转义或改走围栏 | 含 Markdown 元字符的短参数按字面显示 |
| **P12.4** 转义辅助的归属定案 | 与 `codeFence` 的既有处理保持一致：两个包各留一份并互相点名，或下沉共享——二选一并记录理由 | 两个包对同一模式的处理一致，且一致性有注释或测试记录 |

**阶段验收**：两个端到端场景（先 EN 后 ZH、删 evidence 后重跑）落成常驻测试；转义用真实反例
（本文 §2.7 引用的那条 `read` 结果）钉住；默认路径实测确认产物内容与运行参数一致。

**对后续的影响**：P12.1 是 P13 全部内容减法的硬前置。P12 完成后，`details/` 目录从"一旦写下就
永远不变"变成"跟随渲染逻辑演进"，这是 P13 能够真正交付的前提。

---

#### P13 · 证据层体积归位

**目标**：让默认路径的产物体积回到与信息量相称的量级，并让这条纪律**有守卫**——这是它第四次被
提出，前三次都因为没有守卫而退化。

**范围**：批量渲染与单条下钻的物化策略分离（`cmd/vmr` 一层）；详单内部两处逐字复制的削减
（`internal/reqdetail`）；请求索引与报表 §8 的链接判据。

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P13.1** 批量模式只挂链接不物化 | 区分"用户点名下钻一条"与"批量渲染全部候选"；后者渲染链接但不生成详单，显式开关仍可全量物化 | 默认 `vmr analyze` 的 `details/` 为空或仅含显式请求的记录；单条 `-journey` 与显式全量开关行为不变 |
| **P13.2** 删除详单中响应体的逐字复制 | 原始 SSE 全文块改为一行带坐标的取用提示；重组后的模型输出、推理、工具调用一字不改 | 同一批详单体积降约 41%；被删内容仍可由已交付的读取原语按坐标取回 |
| **P13.3** 详单只渲染本轮增量 | 历史消息段落折叠为一行指向上一轮详单的链接；lineage 首条（无前驱）仍全文渲染 | 再降约 52%；两条生成路径对同一条记录产出的详单仍逐字节相同（P2 核心不变量） |
| **P13.4** 链接判据从 flag 改为事实 | 报表 §8 文案与请求索引的详单列，判据从"本次是否开了详单开关"改为"目标是否存在" | 报表对产物的陈述与实际产出一致 |
| **P13.5** 体积纪律的常驻守卫 | 断言默认套件模式产出的详单数为 0（或不超过显式请求数） | 人为让批量路径物化详单，测试当场失败 |

**阶段验收**：单日与全量两个规模上各做一次默认路径实测，给出与本阶段前的对比数字；
P13.5 的守卫经人为反例验证；`vmr report -details` 与单条 `-journey` 两条路径的详单逐字节一致性
回归测试仍绿。

**需拍板**：P13.3 改变"单份详单自包含"这一性质（变成需要顺链回溯）。本文 §2.2 已论证这个取舍
可接受（有 `SysHash` 走 evidence 引用的先例、且链条有起点），但它改变的是用户阅读单份详单的体验，
**开工前应确认**。

**对后续的影响**：体积降下来之后，P14 扩大默认渲染范围才不是加码。

---

#### P14 · 索引与渲染范围一致化

**目标**：消除"索引把一类候选推到首屏、套件却不给它任何可点的东西"这个自相矛盾；
同时让读者能分辨"检查过没问题"与"检测器结构性沉默"。

**范围**：候选类别的显示折叠与默认渲染两条策略的合并；Findings 输出的覆盖率披露。
不改分类器本身（`[cron:]`/`[heartbeat]`/`[Subagent Context]` 的前缀判据是准确的，不引入新猜测）。

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P14.1** 两条分类线合并为一条判据 | 显示折叠与默认渲染共用同一个"重要 vs 噪声"判据；按实测数据只把 heartbeat 归为噪声 | 索引首屏的每一行都有可达的报告链接；折叠范围与不渲染范围由同一处推出 |
| **P14.2** 检测器覆盖率披露 | 在 Findings 输出中标注哪些检测器对本批数据结构性不适用及原因 | 一份纯 OpenAI 语料的报告里，读者能分辨"检查过没问题"与"无法触发"；不引入任何新的推断判据 |

**阶段验收**：真实语料上默认路径实测，索引首屏零个不可点的行；产物体积相对 P13 的增量在预期范围内
并记录数字。

**需拍板**：P14.1 会把默认渲染范围从约 238 条候选扩到约 370 条，改变默认产出的范围与体积；
P14.2 的呈现形态（单独一节 / 每条 Finding 旁注 / 报告头部一行）需要选定。**两项都应在开工前确认。**

---

#### P15 · CLI 入口收敛收尾

**目标**：让"别名"这个词名副其实——`vmr report`/`vmr story` 的每一种既有用法都能由
`vmr analyze` 表达，别名退化为纯转发，两份手写分派合并为一份。

**范围**：`cmd/vmr` 的 flag 集合与子命令分派；相应的用户指南、README 与变更日志。
不改 `internal/report`/`internal/story` 的包边界与任何渲染/聚合逻辑。

| 任务 | 说明 | 验收标准 |
| --- | --- | --- |
| **P15.1** 补齐 `analyze` 缺失的两个模式 | "只跑宏观半区"与"只列候选不渲染"——这是别名今天无法被 analyze 表达的全部原因 | 每一种现有 `vmr report`/`vmr story` 调用，都有等价的 `vmr analyze` 写法且产物逐字节相同 |
| **P15.2** 别名退化为纯转发 | 两个别名改为"解析自己的 args → 翻译成 analyze 的等价 args → 转发"，删除各自的分派分支 | 两个别名对任意既有调用方式产物逐字节不变；分派逻辑全仓只剩一份 |
| **P15.3** 自指流量口径不对称随之消失 | `-llm-key` 在单一 flag 集合下对所有模式统一生效 | 两条入口的自指流量排除口径一致 |
| **P15.4** 措辞与文档归位 | 修正同一句话里既称 deprecated alias 又称 remains fully supported 的迁移提示；回填 P9.1 验收标准与当前状态的差异 | 提示文案与代码实际能力一致；文档不再声称一件代码做不到的事 |

**阶段验收**：四种变焦形态与两个别名的全部调用方式各跑一遍，产物与收敛前逐项比对；
`internal/report`/`internal/story` 的 diff 为零。

**需拍板**：P15.1 新增 flag 的拼写与语义（例如是一个 `-scope` 取值，还是两个独立开关）。
这会进入用户的肌肉记忆，**应在开工前定稿，不要分两轮改两次命令行**——这是 P9 自己登记过的教训。

---

### 6.6 不进入本期序列的项

| 项 | 处置 | 理由 |
| --- | --- | --- |
| `session.go` 的 `collect()` 接入解析缓存 | **维持后续独立排期**（`KNOWN_ISSUES §1.1`/`§1.23`） | 本轮实测热缓存 18.4s，结论不变：收益已证但正确性风险高于聚合缓存（算错会把不相关对话缝到同一个 Session），需先补 cold/warm 一致性测试。与本期五个阶段无依赖 |
| 决策脊柱的工具结果降级为纯引用 | **维持暂缓** | 报告体积 107KB–609KB 的主因，但纪律本身成立（随步数×调用数增长，不随对话长度）。是可读性与体积的权衡，留给报告体积真正成为痛点时再决定 |
| 工具 id 归一化下沉到 `chatmsg` | **等触发条件**（`§1.40`） | 今天只有一个消费方，不是活的 bug。触发条件：出现第二个需要按 id 配对工具结果的消费方 |
| 详单/证据文件名换更长的哈希 | **不做** | 全量 11,274 条实测零碰撞；详单条件风险约 `10⁻⁹`，证据层约 `10⁻⁷`。其唯一真实后果（碰撞后静默返回错内容）由 P12.1 的"校验而非假设"顺带覆盖 |
| OpenAI 工具返回的错误内容嗅探 | **不做** | `KNOWN_ISSUES §2.4` 已用 495,672 条实测登记：目标 JSON 形状出现 0 次；且会把推断引入 Findings 证据基础 |
| `.parse-cache/`/`evidence/` 的孤儿回收 | **维持不做**（`§1.27`） | 实测四日抽样 71 个证据文件 / 512KB，不值得引入生命周期管理。架构文档 §7.6c 支撑该结论的量级数字已更新为实测值 |
| 全内存聚合按日分桶 | **等触发条件**（`§1.2`） | 重估条件仍是"单次分析超约 3 万条记录，或峰值 RSS 超 4GB" |
| Responses API 工具调用形状覆盖 | **不做** | 本轮实测复核触发条件：`openairesponses` 占比 0.00% |
| Phase 1b 六个 LLM 判别器的黄金样本校准 | **维持独立排期**（`§1.18`） | 成本在人工标注投入，不在代码；与本期无依赖 |

### 6.7 调整机制

沿用第一期 DevPlan §6：**允许**范围缩减、阶段顺序调整（不违反 §6.4 的三条硬依赖）、相邻阶段任务
合并；**不允许**跨阶段并行推进同一层产物、不允许沿用未经重新分析的 ActionPlan 开工。

每阶段收尾做边界复核，回答三个问题：本阶段是否产生了本文未预见的事实？是否改变了后续阶段的前提？
是否暴露了某个原以为需要的任务其实不必要？**主动删任务比补任务更值钱。**

本期补充一条复核问题，来自 §2.11 的教训：

4. **本阶段声称恢复的纪律，有没有一条会在下次改动时自己举手的测试？** 如果答案是"靠验收时人工核对"，
   那么这条纪律已经具备了退化的全部条件——前三次就是这么退化的。
