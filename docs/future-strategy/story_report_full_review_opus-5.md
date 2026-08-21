// Ver 2026-08-21 15:10, by Opus 5

# Story/Report 架构重构两轮十阶段 — 全面复查记录

## 0. 任务 debrief 与 Action Plan

### 0.1 任务要求的重述

用户要求的是**一次针对 `story_report_architecture_opus-5.md` 落地完整性的独立复查**，而不是又一轮设计。
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

### 0.2 复查范围与证据基线

| 项 | 值 |
| --- | --- |
| 代码基线 | `a4a1e00`（P10 收尾）+ 工作区 4 个未提交文件 |
| Commit 范围 | `b098ca9`(P0) → `a4a1e00`(P10)，共 11 个阶段提交 |
| 规划文档 | `story_report_architecture_opus-5.md`（方案权威）、`story_report_dev_plan_opus-5.md`（P0–P6）、`story_report_dev_plan_2_sonnet-5.md`（P7–P10） |
| 真实语料 | `logs/vmr-audit-2026-07-28.jsonl.zst`（322 条记录、25 条 lineage、6 个候选 Journey）；全量语料数字引自 P9 执行记录（34 文件 / 11274 条 / 压缩 645MB） |
| 验证二进制 | 工作区代码 `go build -o <scratchpad>/vmrbin ./cmd/vmr` |
| 基线健康度 | `go build ./...` / `go test ./...` / `go test ./internal/archtest/...` / `gofmt -l` / `go vet ./...` 全绿 |

### 0.3 Action Plan：待复查功能点 × 文件清单

复查以**阶段任务**为功能点单位（这是三份规划文档共用的粒度），每项列出需实读的文件。
"结论"列在 §1 逐项填写。

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
真实语料上可验（out1 的 openclaw journey 脊柱有工具结果行）。

两处缺口：

1. **归一化落在 `internal/story`，不在 `chatmsg`。** 架构文档 §5.7 建议 2 的原话是"`chatmsg` 的配对
   逻辑加归一化回退"。实际实现放在 story 侧的 `toolResultsFor`。后果是 `internal/report` 半区
   （以及任何未来的第三个消费方）拿不到归一化配对，而 `CLAUDE.md` 的不变量写的是"`ctxgraph`/
   `chatmsg` 是消息哈希与消息解析的单一真源……私有再实现会与之静默分歧，这是一整类 bug"。
   今天没有第二个消费方，所以不是活的 bug；但它是一处**已经发生的真源外移**，该登记。
2. **`chatmsg.CheckToolPairing` 的 F9 doc comment 与 §5 实测直接矛盾**（"100% pairing rate is an
   invariant of the data"），而 §5 已用五个真实日志文件证明 OpenClaw 家族客户端上精确匹配是 0%。
   → **本次已修**（见 §4 第 7 项）。

### R2 脊柱覆盖补完 — ✅

`renderDecisionSpine`（`render_spine_step.go:145`）对每个 Step 二选一渲染
（`renderSpineStep` / `renderSpineBriefStep`），无 `anyCalls` 提前返回；`renderFinalDeliverable`
在末尾复用 compare 的 `deliverableStats`；中途指令行由 `s.Instruction` 驱动，两条渲染路径都覆盖
（`renderSpineStep:255` 与 `renderSpineBriefStep`）。真实语料 out1 六条 journey 逐条肉眼核对，
无整段消失的 Step。

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

真实语料实测：`vmr-requests.json` 322 行全部带 `req`，与 `vmr-stories.json` 的 manifest 坐标口径
一致（P7.4 已把 `journeys[].files` 也改成 `CanonicalPath`）。

### R5 详单减法 + 下沉 + 确定性命名 — ✅

`internal/reqdetail` 是一个真正的叶子包（`import_boundaries_test.go` 新增的 `vmr/internal/reqdetail`
条目禁止它反向 import `report`/`story`/`router`/`server`/`config`）。`Render(rec, path, line, m, prev,
prof, lang, linkEvidence)` 的入参全是共享层类型，无 `report.ReqInfo` 残留。文件名
`FileName(ts, virtual, real, outcome, req)` 用记录自带时区偏移 + `ReqHash8`，无批次计数器、无 `-N`
后缀、无 `fmtutil.DisplayZone`；`FileNameForManifest` 与 `FileNameForRecord` 汇入同一个格式化器。

`internal/report/detail.go` 从 1047 行缩到 282 行（`archtest` 豁免值工作区已从 1150 调到 350）。

### R6 删 `details/*.json` + 坐标读取原语 — ✅

`writeOneDetail`（`report/detail.go:84`）只写 `.md`；`vmr replay -req COORD -print` 已交付并写进
UserGuide；`audit.LineAt` 是底层原语。真实语料 out1 的 `details/` 下 0 个 `.json`。

一处过期注释：`DetailWriter` 的 doc comment 仍写"writes one .md + one .json"，与同文件上方
`writeOneDetail` 的说明自相矛盾。→ **本次已修**。

### R7 详单按需且幂等 — ❌（对 `vmr report` 成立，对 `vmr analyze` 不成立）

`vmr report` 侧成立：`-details` 默认 false，默认运行只写索引（out3 实测 1.5MB）。
`reqdetail.EnsureRendered` 的幂等性由"文件在就跳过"+ `writeFileAtomic`（临时文件 + 改名）保证，
符合架构文档 §7.6c 第 3 条。

**但 `vmr analyze` 默认路径把这条纪律整体抵消了**——这是本次复查最重的一条，详见 §2.1。

### R8 共享证据条目 — ✅

`reqdetail/evidence.go` 的 `EnsureSysPromptEvidence`/`EnsureToolsEvidence` 内容寻址、幂等、
经 `ensureEvidenceFile` 原子写。真实语料：322 条记录 → `evidence/` 8 个文件（4 个 sysprompt +
4 个 tools），512KB。`story/render_md_sysprompt.go` 用每个 era 起始 Step 的 `Manifest.SysHash`
定位文件名（P4 交接说明里点名的那个坑没踩）。

架构文档 §7.6b 表格里"report 侧引用者"两格已在 P7.5 标为"设计预留，未实现"，与代码一致。

### R9 缓存与索引分家 — ✅

`ctxgraph.CacheSchemaVersion = 1` 参与命中判据（`cache.go:175`）；`LoadCacheDir`/`SaveCacheDir`
按 `CachedFile.Hash` 分片、紧凑编码、临时文件 + 改名；两条命令共用 `{outDir}/.parse-cache/`。
`report/factscache.go` 把每条记录的事实提取结果一并纳入缓存并共用同一个版本戳。索引侧
`vmr-stories.json`/`vmr-requests.json` 保留 `MarshalIndent`（人会看，架构文档明确要求）。

真实语料：out1 的 `vmr-stories.md` 2.4KB、`.parse-cache/` 单分片，索引已回到"可随手 cat"的量级。

`ctxgraph.FileCache` 的 key 规范化为 `CanonicalPath`，架构文档 §7.3a 提到的"绝对路径/相对路径各存
一份、255 条 manifest 重复"随之消失。

### R10 中观机读层 — ✅

`story/structure.go` 覆盖 Task/Step/Event/ToolCall 全结构，含 P4 执行期才确认必须纳入的
`EditRef`/`StitchRef`/`CompactionRef`（fact-layer 展示、但单条审计记录物理上算不出来的图级事实）。
内联/引用边界正确：`NewEvents` 只有 `Hash`/`Role`/`FirstStepSeq`，`ToolCallRef` 不带结果正文。

**P4.2 要求的"常驻自动化检查"真的存在**：`TestBuildStructure_VolumeBoundedByStepsNotProseLength`
（`structure_test.go:322`）用同步数、正文长度差两个数量级的两条 Journey 断言序列化体积差被
`structureExcerptChars × 字段数` 界住，并额外断言巨型 payload 从不整段内联。这是这次复查里少数
"纪律真的有守卫、不是文档里一句话"的例子。

真实语料：openclaw 22 步样例的 `structure` 65.8KB。

### R11 人读层瘦身 — ⚠️（结构达成、体积未达成）

fact-layer 渲染函数已删；脊柱每步挂 `../details/<name>.md` 链接（`spineStepHeader`，纯由 Manifest
算出）；系统提示词头部改为 `evidence/` 链接；五类图级分析事实搬进 `spineTransitionLines`。

体积：架构文档 §7.4 的目标是"298KB → < 50KB"。真实语料 out1 六条 journey 实测
**107KB / 132KB / 171KB / 192KB / 384KB / 609KB**。§7.4 已有一条"落地现状注记（P6 复盘）"承认
86KB–506KB 未达标，但那条注记停在 P6；P9 之后单条最大值涨到 609KB，注记的数字已经过期。
根因不变（脊柱每步的工具参数与结果各按 3000 字符封顶全文内联，体积随步数×调用数增长）。

### R12 Lineage 内容寻址 ID — ✅

真实语料实测：`vmr-report.json` 的 25 个 `sessions[].id` 全部是 `l-<hash8>` 形态、`alias` 保留
`s01` 位置序号；`vmr-stories.json` 的 `journeys[].lineages` 与之 join，9/9 命中（其余 16 条 lineage
不构成候选 Journey，符合 `ListCandidates` 的结构性排除）。

### R13 导航边 — ⚠️

六条边在 out1 上逐条走查：

| 边 | 状态 |
| --- | --- |
| `vmr-report.md` → `stories/vmr-stories.md` | ✅ 存在，且带生成时间与覆盖窗口（§7.5 要求的"让链接自己说明来源"） |
| `vmr-requests-<tag>.md` 会话行 → journey | ✅ |
| `journey-*.md` → `vmr-stories.md` / `vmr-report.md` | ✅ |
| `details/*.md` → `vmr-requests.md` | ✅ |
| 脊柱 Step → `details/*.md` | ✅ 22/22 可达 |
| `vmr-report.md` §8 → 详单 | ⚠️ 见下 |

写脚本对 out1 的 `*.md` + `stories/*.md` 做了本地链接存在性扫描：419 条导航链接、72 条"失效"，
**逐条核对后全部是误报**——它们是脊柱内联的工具参数/结果**正文**里的 Markdown 链接（对话内容，
落在代码围栏内，渲染时不成为链接）。真正的导航边 0 失效。

⚠️ 的那一条是一个**新的、P7.1 与 P9.2 交互产生的口径错误**：`vmr analyze` 默认路径下 story 半区
已经物化了 306 份详单，但 report 半区的 `detailsOn` 仍是 false，于是
(a) §8 渲染的文案是 "This run did not write `details/*.md`"——事实上写了；
(b) `vmr-requests.md` 的"文件"列渲染 `req` 坐标而非链接——306 份已存在的详单没有被链接。
不产生死链接（这是 P7.1 的本意），但产生了一句假话和一次可用性损失。详见 §2.3。

### R14 索引类别与噪声折叠 — ✅

真实语料：本样本 6/6 全部 `task`（`vmr-stories.md` 无折叠块，符合预期）；P9 执行记录在全量语料上
的数字是 473 个候选 / 234 个 task。P7.4 已把 `CategoryTask` 从空字符串零值改为显式 `"task"`，
out1 的 `vmr-stories.json` 六行全部显式带 `category`。

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

真实语料实测（`-lang zh`）：
- `journey-*.json` 的 20 条 finding 全部中文；
- `compare-*.json` 的 `rows[].label` 全部中文（"模型时间"/"Agent 侧执行时间"…）；
- `vmr-report.json` 的 4 条 `efficiency[]` 全部中文。

P8.2 走的是路径 (b)：`Build` 保持语言无关，`cmd_report.go:337` 调用点 `report.LocalizeEfficiency`
重算。核对了这一行落在 `runReport` 内部（第 271 行起），因此 `vmr analyze` 与 `vmr report` 两条
入口都覆盖——这是一处容易漏的隐性契约（`recextract.go:59` 的 doc comment 明确写"跳过它会静默拿到
英文默认值，不报错"），当前唯一调用点正确，但契约本身靠注释维持，没有守卫。属于可接受的小风险，
登记备查。

### R20 `vmr analyze` 单一入口 — ⚠️

三级变焦选择器、互斥校验、默认套件模式都实现了，`internal/report`/`internal/story` 的 diff 为零
（"纯 CLI 层路由"约束守住了）。`taskOnlyCandidates` 复用 P6.3 已算好的 `category`，不新增分类逻辑。

**但 P9.2 的验收标准"单日样本产物体积从 164MB 级降到与'仅任务类候选'相称的量级"没有达成**——
实测仍是 164MB / 306 份详单，与 P6 复盘的数字**逐字相同**。详见 §2.1。

### R21 别名降级 — ⚠️

两个别名都打印 stderr 迁移提示且产物不变。但 P9.1 的验收标准原文是"**收敛三套独立 flag 集合为
一套**"，实际交付是**新增第四套（analyze 的并集）+ 原样保留 report/story 两套**。
`cmd_story.go:126–157` 的分派逻辑与 `cmd_analyze.go:196–257` 的 `dispatchAnalyze` 是两份手写实现，
互斥规则已经有差异。P9 ActionPlan §4.2 对这个收窄有明确论证（"它们已经是薄封装，不需要为降级
再做一次结构调整"），论证成立于"调用共享执行函数"这一层，但没有覆盖 flag 定义与分派逻辑本身。
详见 §2.4。

`§1.34` 的 `-llm-key` 不对称：P9.5 声称"收敛后自然消失"，但只在 `vmr analyze` 上消失——
`cmd_report.go` 至今没有 `-llm-key` flag，仍只读 `rc.LLMKey`。别名路径上不对称原样存在。

### R22 文档同步 — ✅

`grep -c 'vmr analyze'`：README.md 2 / README.zh.md 2 / UserGuide.md 14 / UserGuide.zh.md 14 /
CHANGELOG.md 3。`CHANGELOG.md` 的 `[Unreleased]` 覆盖了 P1–P9 全部用户可见变更。
P10.1/P10.2 的死引用修复与"已采纳"指针都落地了（两份参考文档开头各有一行现状指针）。

对 `docs/future-strategy/*.md` + `CLAUDE.md` + `README*` + `KNOWN_ISSUES` 跑了一次 Markdown 路径
存在性扫描，逐条核对后**无真死引用**——所有指向已归档文件的引用都带"（已归档，不在版本控制范围
内）"措辞，这是 P10.1 有意选择的处理方式，比删掉引用更保留可追溯性。

### R23 架构文档自身目标达成度 — 见 §2.5

### R24 死代码与冗余 — 见 §2.6

---

## 2. 集中分析：从架构层面看发现

### 2.1 【最重】证据层的体积纪律从未在推荐入口上成立，且被三次验收系统性绕过

**事实。** 用工作区二进制跑单日真实语料（322 条记录）的默认 `vmr analyze`：

```
164MB  总产出
160MB  details/（306 份详单）
512KB  evidence/
2.6MB  stories/
```

这与 P6 复盘记录的"单日 322 条记录实测产出 164MB（其中 `details/` 160MB，306 份详单）"**逐字相同**。
P9.2 的修法（默认只渲染 `category == task` 候选）在这份样本上过滤掉了 **0 个**候选——6 个候选
全部是 `task`。P9 自己的全量语料执行记录也印证了这一点：task 类只占候选数量的 49%，却占**83.5%
的请求量**。

**这条纪律被绕过了三次，每次都是"验收标准漂移"而不是"实现走样"：**

| 轮次 | 架构文档的纪律 | 实际验收对象 |
| --- | --- | --- |
| P3.3 | "默认运行的产物集合回到索引量级" | 只验 `vmr report`——当时 `analyze` 还不存在 |
| P6.5 | 同上 | 验的是"一次调用得到导航闭合的套件"，体积没进验收表；`analyze` 强制 `-render-all` 抵消了 P3.3，P6 复盘事后发现 |
| P9.2 | "单日样本体积从 164MB 级降到与'仅任务类候选'相称的量级" | 验的是"不再复现 SIGKILL"。全量语料 3.5GB / 8343 份详单被记为成功 |

`KNOWN_ISSUES §3` 把 `1.32`（`analyze` 强制 `-render-all` 抵消默认按需）列为已闭环。**这个判定
应当撤回**：形态从"强制 `-render-all`"换成了"默认渲染全部 task 候选、每条 journey 每步物化详单"，
纪律本身仍未成立。P9 只解决了它的一个下游症状（SIGKILL）。

**根因不是 P9.2 诊断的那个。** P9.2 假设根因是"渲染了不该渲染的候选类别"。真实根因在
`cmd/vmr/cmd_story.go:745`：`writeJourneyFile` **无条件**调用 `story.EnsureJourneyDetails`，
不区分"用户点名要看这一条 journey"和"批量渲染全部候选"。

架构文档 §7.5 其实已经写明了这条规则的边界，只是没人在批量模式上执行它：

> 这条规则的成本与"被渲染的链接条数"成正比。任务报告的脊柱一次几十步，可以永远挂链接；
> 而请求索引列的是**全部请求**，若每行都挂链接就等于全量物化详单——那正是 §7.6(c) 要消除的东西。

"渲染即生成"是为**单 journey 下钻**设计的（几十步、用户主动要求）。默认套件模式一次渲染 234 条
journey / 9259 步，把它变成了全量物化。链接文件名可算、生成幂等——这两条性质恰恰说明**批量模式
可以只挂链接不生成**，用户真点进去的那一条再按需补。

**修法**：`writeJourneyFile` 增加一个"是否物化详单"的入参；单 journey 路径（`renderJourney`、
`ensureJourneyFile`）传 true，批量路径（`renderJourneys`/`renderAllJourneys`）传 false，
`-details`/`-render-all` 显式要求时传 true。改动面在 `cmd/vmr` 一层，`internal/*` 零 diff。

### 2.2 【最重】详单内部 93% 是复制——同一把刀该砍第三次

架构文档 §7.6c 只砍了 `details/*.json` 这一份副本。对 `.md` **内部**的重复从未审视过。
实测 306 份详单、152MB（`du` 的 160MB 含块对齐）：

| 组成 | 字节 | 占比 | 性质 |
| --- | --- | --- | --- |
| `Raw SSE, full` 折叠块 | 62.4MB | **41.1%** | `rec.Client.Response.Body` 的**逐字复制**（`reqdetail/detail.go:546`） |
| ① Client → VMR Request 段 | 82.9MB | 54.6% | 其中 **79.2MB（该段的 95.5%）**在第一个 `🆕` 标记之前——即**上一轮详单已经渲染过一遍**的历史消息 |
| 其余（②attempts、③重组输出、头部） | ~6.7MB | 4.4% | 本轮真正新增的内容 |

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
与 §2.1 的修法正交，可分别交付、分别验证。

### 2.3 【中】默认套件的两个半区不知道对方物化了什么

`vmr analyze` 默认路径：story 半区物化了 306 份详单，report 半区的 `detailsOn` 仍是 `false`。后果：

- `vmr-report.md` §8 渲染 "This run did not write `details/*.md` (generated on demand by default)"
  ——**这句话是假的**，本次运行写了 306 份。
- `vmr-requests.md` 的"文件"列渲染 `req` 坐标而非链接——306 份已存在的详单没有被链接到。

P7.1 把这一列的判据定为 `detailsOn`（report 半区自己的 flag），而架构文档 §7.5 定的判据是
**目标产物是否存在**（"两类边两种策略"里的第二类，`stat` 一次）。用 flag 近似"文件存不存在"，
在两个半区各自决定物化范围之后就不再成立。

修法有两条，取决于 §2.1 怎么改：若采纳 §2.1（批量模式不物化），则默认路径下确实一份详单都没有，
现状文案与坐标列都变成正确的，这条自动消失；若不采纳，则 `detailCell` 与 §8 文案改为按目标存在性
判断。**建议合并到 §2.1 一起做**，不单独立项。

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

这不是要求立刻推倒重来。`vmr report`/`vmr story` 是明确的过渡别名，"何时真正移除是一次独立的、
面向未来使用数据的决定"（DevPlan2 P9 结语）。但**在移除之前，别名应当是薄的**：解析 args →
翻译成 analyze 的等价 args → 调 `cmdAnalyze`。这样分叉在结构上不可能发生，也顺带让
`-llm-key` 不对称（`§1.34`）真正消失。

### 2.5 架构文档自身：三条被实践修正的结论

用户要求"如果原始设计方案本身存在问题，也要直言不讳"。三条：

1. **§7.4 的"< 50KB"是一个从未成立的估算。** 它按"脊柱 42.8KB 基本不变"推算，没有把"每步都要
   展示工具参数与结果"算进去。§7.4 已有一条 P6 复盘注记承认这一点，但注记里的数字（86KB–506KB）
   在 P9 之后已经过期（实测上界 609KB）。**建议**：把这个数字从"目标"改写成"当前实测区间 + 唯一
   剩余的减法是把工具结果也降级为引用"，不要再让下一个读者把它当未达成的待办。
2. **§7.6c 的四条处置只覆盖了"份数"，没覆盖"每份的量纲"。** §2.2 已论证。这不是实现走样，是
   设计期的盲区——文档把 `details/*.json` 当成唯一的逐字复制，但 `.md` 里的 Raw SSE 块是同一性质
   的另一半，历史消息重复是第三半。**建议**：§7.6c 补第 5 条。
3. **§7.5"两类边两种策略"里的第二类判据被 P7.1 换成了 flag。** §2.3 已论证。

另有两条**已有注记、无需再动**的：§7.9 的"一次扫描、一份缓存、一次建图"至今是顺序调用两遍
（有明确注记）；§7.9 的"收敛为一个动词"在 P9 已经落地，注记该更新（P9 之后它不再是"开放决策"）。

### 2.6 死代码与冗余

`deadcode` 全仓扫描 + 人工核对生产路径引用（排除只被本包测试引用的情况）：

| 项 | 行数 | 判定 |
| --- | --- | --- |
| `ctxgraph/blobindex.go` 整个文件 | 125 | **完全废弃**。`Lookup`/`Len`/`FetchAll` 生产与测试双零引用；`records.go:33` 的注释明写"use this instead of BlobIndex.FetchAll"。但 `buildGraph`（`scan.go:79/92`）仍在每次扫描时构造并逐哈希填充它（单日语料 22135 次 `firstSeen`，852 个唯一键）。成本量级很小（全量约 2.6MB map），**问题不在性能，在于一个 125 行的废弃子系统还挂在 `Graph` 的公开字段上** |
| `report.Build`（`build_cached.go:18`） | 28 | 生产零引用，`BuildCached` 是唯一路径 |
| `report.WriteDetails`（`detail.go:233`） | ~50 | 生产零引用。它曾是架构文档 §4.8 论证"此路不通不成立"的三条证据之一，P2/P3 走了更简单的路之后没人回头删它 |
| `report.AnalyzeSessions`（`session.go:191`） | 3 | 生产零引用，`AnalyzeSessionsCached` 是唯一路径 |
| `ctxgraph.Scan`（`scan.go:39`） | ~28 | 生产零引用，`ScanCached` 是唯一路径 |
| `story.Build` / `story.PreviewTitle` | 各 ~20 | 生产零引用，批量版（`BuildAll`/`PreviewTitles`）是唯一路径 |
| `chatmsg.CheckToolPairing` + `PairingReport` | 97 | 只被 `story/invariants_test.go` 引用。**这一条建议保留**——它是 F9 不变量的可执行断言，属于有意的测试基础设施，但应在 doc comment 里说清楚（本次已改） |
| `chatmsg.ExtractFinish`、`cmd/vmr.configFlag`、`health.Registry.Available`、`reqdetail.ErrorClass`、`reqdetail.contentHash8` | 各 <15 | 零引用小函数 |

前六项是同一个模式：**一个函数的缓存版/批量版成为唯一生产路径之后，非缓存版/单条版留在原地，
各自还带着测试。** 这是 P2/P3 两次大改留下的、成体系的一批。合计约 250 行生产代码 + 相应测试。

冗余注释（本次已修的四处见 §4）：源码注释里有三处引用 `internal/report/render.go`，该文件在 P2 已
移为 `internal/reqdetail/render.go`。`archtest` 的 `doc_refs_test.go` 只守 `CLAUDE.md`/`README*`/
`docs/` 顶层，**不守源码注释里的文件路径引用**——而 `CLAUDE.md` 要求注释只写非显然的 why，这类
交叉引用正是 why 的载体，指错地方就是误导。这是一个可以廉价补上的守卫缺口。

### 2.7 【中】人读产物的内容转义在两个包之间已经分叉

`internal/reqdetail` 与 `internal/story` 都用 `<details><summary>用户内容</summary>` 这个模式。
`reqdetail` 转义（`render.go:41` 的 `escapeHTML`，在 `renderMessageSection`/`ToolCallArgsChars`
等处调用）；`internal/story` **三处都不转义**：

| 位置 | 注入点 |
| --- | --- |
| `render_spine_args.go:194` `payloadBlock` | `<summary>` 放工具参数原文前 160 字符 |
| `render_spine_step.go:111` `toolResultLine` | `<summary>` 放工具结果原文前 160 字符 |
| `render_spine_step.go:86` `foldWhyLine` | `<summary>` 放 `RespText`/`Reasoning` 原文 |

真实语料上已经观察到后果：`out1/stories/journey-j-pimini-…-754b71e2.md` 第 42 行的 summary 是
`<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> <!-- keywords: …`——工具结果的开头是一段 HTML 注释，
渲染时 summary 的这部分被浏览器**静默吞掉**。读者不会知道自己少看了东西。同类风险还包括内容里
出现 `</summary>` / `</details>` 直接破坏折叠块结构。

两处 inline 分支（`payloadBlock` ≤120 字符单行、`foldWhyLine` 短文本）同样把原文裸写进 Markdown
正文，未转义也未 fence——内容里的 `*`/`_`/`[](…)` 会被当作 Markdown 语法。

值得注意的是 `story/render_md.go` 的 `codeFence` doc comment 花了七行论证"故意重复而不共享，
因为两份拷贝不会以读者能察觉的方式漂移"。这个论证对 `codeFence` **本身**成立（两份实现确实一致），
但它掩盖了一件事：**整个折叠块渲染模式（fence + summary + escape）在两个包各写了一遍，而它已经
漂移了**——漂的不是 `codeFence`，是它旁边那个没被一起复制的 `escapeHTML`。

---

## 3. 问题打包

按严重程度排序。每个 Package 内部相关、Package 之间独立可分别交付与验证。

### Package A — 证据层体积纪律归位（高）

| 项 | 内容 | 验收 |
| --- | --- | --- |
| A1 | `writeJourneyFile` 区分单条下钻与批量渲染，批量模式只挂链接不物化详单（§2.1） | 单日样本默认 `vmr analyze` 的 `details/` 为空或仅含显式请求的记录；`-journey <单条>` 与 `-details`/`-render-all` 行为不变 |
| A2 | 删除详单里的 `Raw SSE, full` 全文块，改为带 `req` 坐标的取用提示（§2.2 第 1 条） | 同一批详单体积降约 41%；重组后的模型输出、reasoning、tool_calls 一字不改 |
| A3 | 详单的 ① 段只渲染 `deltaStart` 之后的消息，历史段落折叠为一行指向 `PrevTurnLink`（§2.2 第 2 条）；`prev == nil` 时仍全文渲染 | 再降约 52%；`vmr report -details` 与 `vmr analyze -journey` 两条路径生成的详单仍逐字节相同（P2 的核心不变量不能破） |
| A4 | §8 文案与 `detailCell` 的判据从 `detailsOn` 改为目标存在性（§2.3）——若 A1 落地则复核是否自动消失 | `vmr-report.md` §8 的陈述与实际产出一致 |
| A5 | 撤回 `KNOWN_ISSUES §3` 对 `1.32` 的"已闭环"判定，改登记为"症状已解决（SIGKILL），纪律未成立" | 该条目状态与实测一致 |

依赖：A2/A3 互相独立；A4 依赖 A1 的结论。**A1 与 A2 单独任一项就能把单日 164MB 降到可接受量级**，
可分批上线。

**这个 Package 必须补一条常驻守卫**，否则它会第四次被绕过：一条断言"默认套件模式产出的
`details/` 文件数为 0（或不超过显式请求数）"的端到端测试，与 P4.2 的体积守卫同级。三次验收漂移
的共同点是"没有自动检查、每次靠人记得去量"。

### Package B — 人读产物的内容转义（中高）

| 项 | 内容 | 验收 |
| --- | --- | --- |
| B1 | `internal/story` 的三处 `<summary>` 注入点转义 `< > &` | 含 HTML 注释/标签的工具结果，summary 完整可见 |
| B2 | 两处 inline 分支的裸文本转义或改走 fence | 含 Markdown 元字符的短参数按字面显示 |
| B3 | 把 `escapeHTML` 的归属定一次：要么两个包各留一份并在注释里互相点名（同 `codeFence` 的既有处理），要么下沉 | 两个包对同一模式的处理一致，且这条一致性有注释或测试记录 |

回归测试用真实反例（`journey-j-pimini-…-754b71e2` 的那条 `read` 结果）钉住。

### Package C — CLI 别名薄化（中）

| 项 | 内容 | 验收 |
| --- | --- | --- |
| C1 | `cmdReport`/`cmdStory` 改为"解析 args → 翻译为 analyze 等价 args → 调 `cmdAnalyze`" | 两个别名对任意既有调用方式产物逐字节不变；`cmd_story.go` 的分派分支删除 |
| C2 | `§1.34` 的 `-llm-key` 不对称随 C1 真正消失 | `vmr report -llm-key X` 与 `vmr analyze -llm-key X` 的自指流量排除口径一致 |
| C3 | DevPlan2 P9.1 的验收标准"收敛三套 flag 集合为一套"如实回填当前状态 | 文档与实现一致 |

C1 有一处真实风险需要在 ActionPlan 里先判：`vmr story` 无选择器时是"只列表"，`vmr analyze` 无
选择器时是"渲染默认套件"。翻译层需要一个内部的"只列表"模式，或让 `cmdStory` 保留这一个分支。
不要为了形式上的薄化改变别名的产出。

### Package D — 死代码清理（中低）

| 项 | 内容 | 验收 |
| --- | --- | --- |
| D1 | 删除 `ctxgraph/blobindex.go` 及 `Graph.Index` 字段与 `buildGraph` 的填充循环 | 全量测试绿；扫描产物逐字节不变 |
| D2 | 删除六个"非缓存版/单条版"死函数及其测试：`report.Build`、`report.WriteDetails`、`report.AnalyzeSessions`、`ctxgraph.Scan`、`story.Build`、`story.PreviewTitle` | 同上；`deadcode` 复扫这六项消失 |
| D3 | 删除零引用小函数：`chatmsg.ExtractFinish`、`cmd/vmr.configFlag`、`health.Registry.Available`、`reqdetail.ErrorClass`、`reqdetail.contentHash8` | 同上 |
| D4 | `archtest` 的文档引用守卫扩展到源码注释里的 `internal/<pkg>/<file>.go` 路径（§2.6） | 三处 `internal/report/render.go` 式的过期引用若再出现会被测试挡下 |

D1–D3 合计约 250 行生产代码 + 测试。**保留 `chatmsg.CheckToolPairing`**——它是 F9 的可执行断言。
D4 是防止这一类问题复发的那一半，建议与 D1–D3 同批。

### Package E — 文档回填（低成本，纯文档）✅ 本次已完成

| 项 | 内容 | 状态 |
| --- | --- | --- |
| E1 | 架构文档 §7.4 的"< 50KB"改写为当前实测区间 + 唯一剩余减法（§2.5 第 1 条） | ✅ |
| E2 | 架构文档 §7.6c 补一条"每份详单内部的两处逐字复制"（§2.5 第 2 条），并说明它与删 `.json` 是同一把刀 | ✅ |
| E3 | 架构文档 §7.5 的"两类边两种策略"补注：P7.1 用 `detailsOn` 近似了存在性判断，及其后果（§2.3） | ✅ |
| E4 | 架构文档 §7.9 的落地注记更新——"收敛为一个动词"在 P9 已落地，不再是开放决策 | ✅ |
| E5 | `KNOWN_ISSUES` 登记 `§1.35`–`§1.40` 六条新条目、撤回 `§1.32` 的闭环判定、ROI 表与分布重算 | ✅ 见 §4.2 |

E5 里 `§1.5`（行数现状）的更新在本轮开始前已存在于工作区，随本轮一并提交。

---

## 4. 本次直接处理的问题（已解决）

以下八项属于"错别字/引用错误/小修改"，按任务要求在本次复查中直接改掉。全部改动通过
`go build ./...`、`go test ./...`、`go test ./internal/archtest/...`、`gofmt -l`。

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
   `render_md.go` 的注释里躺了两个阶段没人发现（本次 §4 第 2/3/4 项修的就是它们）。
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

同时：
- **撤回了 `§1.32` 的"已闭环"判定**（§3 第 31 项就地订正 + `§0` 分布重算）——它闭环的是 SIGKILL
  这一个症状，不是纪律本身。
- `§4` ROI 表补入五行；`§4.2` 的"高 ROI 0 条"改写为 2 条，并把三次绕过的机制作为教训写进去。
- 架构文档三处回填已落地：§7.4 的"< 50KB"作废并给出当前实测区间（E1）、§7.6c 补第 5 条
  「同一把刀砍进详单内部」（E2）、§7.5 补注 P7.1 用 `detailsOn` 近似存在性判断的后果（E3）、
  §7.9 的"开放决策"结案（E4）。**Package E 因此已在本次完成**，不留到后续阶段。

---

## 5. 结论

**架构目标本身已经实现。** 3×2 矩阵的四个错格全部补齐、坐标层贯通两个半区、共享证据层生效、
导航矩阵六条边在真实语料上全通、机读层的内联/引用边界有常驻守卫、JSON 语言策略统一、CLI 收敛到
单一入口。十个阶段的执行记录基本诚实——多处主动登记了与设计的落差（P5 的体积、P6.5 的收窄、
P9.2 的根因推翻），这在一个自我评估的序列里不常见。

**但有一条纪律从头到尾没有真正成立**：架构文档 §7.6c 的"证据层默认按需、体积与信息量相称"。
它在 P3 对 `vmr report` 成立过一次，随后被 P6.5 的 `analyze` 抵消、被 P9.2 的错误根因诊断放过。
今天推荐入口的默认行为是：单日 322 条记录写 164MB，全量语料写 3.5GB——**而其中约 93% 是同一批
字节的第二份、第三份拷贝**。这与项目自己反复引用的第一性原理（blob 只存一份，tree 只持引用）
正面冲突，且两条修法所需的机制（`req` 坐标 + 读取原语、`PrevTurnLink`）**都已经在仓库里，只是
没有被用来做减法**。

三次绕过的共同机制值得单独记下来，因为它比这个具体缺陷更值钱：**每一次的验收对象都是"这次改动
做了什么"，而不是"那条纪律现在还成立吗"。** P4.2 是唯一一个把纪律本身写成常驻自动检查的任务，
也是唯一一条至今没有退化的纪律。Package A 因此附带一条守卫要求——不是为了这一次修好，是为了
它不会有第四次。

**次要但真实的一批**：脊柱的 `<summary>` 未转义（已在真实语料上吞掉内容）、CLI 别名的分派逻辑已
出现分叉（P9 在一侧修掉的缺陷在另一侧原样留着）、P2/P3 留下的一批成体系死代码（约 250 行）。

**架构文档需要三处回填**（§2.5），其中"< 50KB"这个数字建议直接改写而不是继续挂着——它是设计期的
估算偏差，把它留在文档里只会让下一个读者以为还有一项未完成的工作。
