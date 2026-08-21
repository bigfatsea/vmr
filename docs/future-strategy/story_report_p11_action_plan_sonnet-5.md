// Ver 2026-08-21, by Sonnet 5

# P11 · 清理与守卫先行 — ActionPlan

> 依据：`story_report_full_review_opus-5.md` 第 6 章「后续开发计划（P11–P15）」§6.5 对 P11 的定义
> （目标、范围、任务表、阶段验收），以及该文 §2.6 / Package D / `KNOWN_ISSUES_sonnet-5.md` §1.39
> 的具体依据。
>
> 本计划不沿用上述文档对任务范围的字面判定——按 DevPlan §6.1 的约定，**每个阶段开工前基于该阶段
> 起点的真实仓库状态重新分析**。下面 §0 记录了这次重新分析推翻/修正了什么、为什么。

---

## 0. 开工前重新核实：与原计划的出入

DevPlan 把 P11.2 定成"删除六个非缓存版/单条版死函数"，`KNOWN_ISSUES §1.39` 也照抄了这个判定。
开工前用 `go run golang.org/x/tools/cmd/deadcode@latest ./cmd/vmr/...`（`main()` 可达性分析，
比 grep 更权威）+ 逐个读调用点复核，结果是：**这个判定对其中 4 个函数是错的**。

`deadcode` 报告"unreachable from main()"和"没有生产调用方"是同一件事的两种说法，但**它不等于
"没有任何理由存在"**——`internal/story`/`internal/report` 的测试套件里，有一类函数专门只服务于
测试：要么是"批量版/缓存版的独立参照实现"（缓存正确性无法用缓存版本自己证明自己正确，必须有一个
不经缓存路径的独立实现做差分），要么是"单条构造入口，是几十个测试文件搭建 fixture 的公共起点"。
删掉它们不是删掉冗余，是**删掉测试基础设施**，且删除成本远高于收益（要么改写几十个测试调用点，
要么让缓存正确性从此失去独立参照）。这与 DevPlan 自己保留 `chatmsg.CheckToolPairing` 的理由
（"F9 不变量的可执行断言，属于有意的测试基础设施"）是同一类判断，只是这次没有被应用到位。

逐项复核结果：

| 函数 | DevPlan 判定 | 复核证据 | 结论 |
| --- | --- | --- | --- |
| `report.Build` | 删 | 唯一"无缓存"路径，是 `TestBuildCached_ColdMatchesBuild`/`TestBuildCached_WarmMatchesBuild`（缓存正确性差分基准）与 `TestBuildOnRecordMatchesWriteDetails`（单/双遍等价性证明）的参照实现，共 4 个测试点 | **保留**，判定修正 |
| `report.WriteDetails` | 删 | 同上；且是"旧两遍路径 vs 新 onRecord 单遍路径"字节级等价性回归的另一半（`TestBuildOnRecordMatchesWriteDetails` 等 4 处） | **保留**，判定修正 |
| `report.AnalyzeSessions` | 删 | `TestAnalyzeSessionsCached_ColdCacheMatchesAnalyzeSessions` 的参照实现；另有 `session_conformance_test.go`（跨包一致性）、`detail_test.go`、`session_test.go` 等 7+ 处直接调用 | **保留**，判定修正 |
| `ctxgraph.Scan` | 删 | 全仓 **15 处**测试文件（`cache_test.go`/`scan_test.go`/`stitch_test.go`/`reallog_lineageid_test.go`）用它搭建 fixture；且 `_eval/calibrate_p1b.go`（Phase 1b LLM 判别器校准工具，`KNOWN_ISSUES §1.18` 仍在排期、DevPlan §6.6 表已确认"维持独立排期"）是**真实生产调用方**——只是 `_eval/` 目录名以下划线开头，被 `go build ./...`/`go list ./...` 自动排除，不出现在 `deadcode ./cmd/vmr/...` 的可达性分析里，不代表它不是活代码 | **保留**，判定修正 |
| `story.Build` | 删 | 全仓 **50+ 处**测试文件用它做单 lineage → Journey 的标准构造入口（`compare_test.go`/`journey_test.go`/`findings_toolresult_test.go`/`llm_findings_test.go`/`structure_test.go` 等几乎每个 `internal/story` 测试文件）；`_eval/calibrate_p1b.go` 同样直接调用 | **保留**，判定修正 |
| `story.PreviewTitle`（单数） | 删 | 唯一调用方是它自己的 `preview_test.go`；批量版 `PreviewTitles`（复数）才是 `cmd/vmr/cmd_story_setup.go:83` 的生产路径，两者不是"同一个函数的两个版本互相测试"关系 | **确认删除** |

**结论**：P11.2 缩小为只删 `story.PreviewTitle`（连同其专属测试）。其余 5 个函数在 `KNOWN_ISSUES §1.39`
里的记录需要连带订正（见 §5 收尾）。

**deadcode 扫描还发现一个 DevPlan 未列出的死函数**：`cmd/vmr/summary.go` 的 `providerProxyLines`
(unreachable)。核实：`cmd_check.go:253` 的 `printProviders`（`vmr check` 真正在用的渲染路径）调用的是
`providerProxyEntries` + 单数的 `providerProxyLine`（`cmd_check.go:464`），不是复数的
`providerProxyLines`——后者是被取代后没人回头删的第二套渲染。它有 3 个专属测试
（`TestProviderProxyLines_Direct/_ProxyFalse/_ProxyURL`），测的其实是共享逻辑
`providerProxyEntries`/`redactProxyURL`（凭据脱敏、direct/proxy 判定）——这部分测试覆盖有效且
不该丢，只是不该通过一个死函数间接测。**并入本次清理，连带改写这 3 个测试直接调用
`providerProxyEntries`。**

`ctxgraph/blobindex.go` 的判定核实无误但措辞需要精确一句：DevPlan 说
"`Lookup`/`Len`/`FetchAll` 生产与测试双零引用"不完全准确——`scan_test.go` 里确实有
`TestScan_BlobIndexFetchAllRecoversOriginalContent` 直接调用这三个方法，**但那个测试唯一的作用
就是测试 `BlobIndex` 自己**，没有任何其他测试或生产代码依赖它恢复的内容。这是一个自我闭环的废弃
子系统，连它的测试一起删，与"删除一个从未被下游使用过的子系统"这个判断并不矛盾。

---

## 1. 目标与范围（继承自 DevPlan §6.5，未改动）

把 P2/P3 两次大改留下的废弃代码清空，并把守卫的覆盖面扩到本轮复查发现的两个缺口，让后续
P12–P15 都在一个更小、更受保护的代码面上工作。

**范围**：`internal/ctxgraph`、`internal/report`、`internal/story`、`internal/chatmsg`、
`internal/reqdetail`、`internal/health`、`cmd/vmr` 的死代码删除；`internal/archtest` 的守卫扩展；
`docs/future-strategy/` 的文档状态标注。

**边界（本阶段最重要的约束）**：**不改变任何产物的字节内容**。同一批真实日志在本阶段前后跑出的
全部产物必须逐字节相同——这是本阶段唯一的功能性验收。

**明确排除**：`internal/story` 三处 `<summary>` 未转义、别名分派分叉、体积纪律等——都是 P12/P13/P15
的范围，本阶段不动。

---

## 2. 任务清单

### P11.1 删除废弃子系统 `ctxgraph/blobindex.go`

**文件**：
- 删除 `internal/ctxgraph/blobindex.go`（全文件，125 行：`BlobRef`/`BlobIndex`/`newBlobIndex`/
  `firstSeen`/`Lookup`/`Len`/`FetchAll`/`wantEntry`/`fetchFromFile`）
- 删除 `internal/ctxgraph/scan_test.go` 的 `TestScan_BlobIndexFetchAllRecoversOriginalContent`
  （第 252–290 行）——它是 `BlobIndex` 自己的专属测试，没有其他消费方
- `internal/ctxgraph/scan.go`：
  - `Graph` 结构体删除 `Index *BlobIndex` 字段（第 29 行）及其上方注释里"plus a blob index for
    recovering original message content on demand"半句
  - `buildGraph`：删除 `g := &Graph{Index: newBlobIndex(), NoBody: noBody}` 里的 `Index:` 初始化
    （改回 `&Graph{NoBody: noBody}`），删除内层 `for i, h := range m.Keys { g.Index.firstSeen(...) }`
    循环（第 91–93 行）——**`m.Keys`/`m.MsgIdx` 字段本身不能删**，`edit.go`/`stitch.go`/`lineage.go`/
    `report/session.go`/`story/journey.go`/`story/structure.go`/`story/candidates.go` 都在用，
    只是不再喂给 `BlobIndex`
- `internal/ctxgraph/manifest.go`：第 26、67–68、96 行三处提到 `BlobIndex.FetchAll` 的注释改写或删除
  （不能留"这个字段是为 BlobIndex 准备的"这种指向已删除类型的注释）
- `internal/ctxgraph/cache.go:180`：同上，注释里的 `BlobIndex.FetchAll` 引用改写
- `internal/ctxgraph/records.go:23,33`：注释原文"use this instead of BlobIndex.FetchAll"——`records.go`
  的 `FetchRecords` 是 `BlobIndex.FetchAll` 真正的替代品（P3 已完成的迁移），这两处改写为不再提及
  已删除的类型，只保留"为什么整批读、不逐条读"的技术理由本身
- `internal/ctxgraph/reqcoord_test.go:47`：错误消息文案里的 `BlobIndex.FetchAll` 提及一并改写

**验收**：`go build ./...`/`go test ./internal/ctxgraph/... -race`/
`go run golang.org/x/tools/cmd/deadcode@latest ./cmd/vmr/...` 不再出现 `BlobIndex`/`blobindex.go`
任何符号；对同一批真实日志跑 `ctxgraph.ScanCached`（`report`/`story` 走的生产路径）产物逐字节不变
（该路径本就不读 `Graph.Index`，删除前后行为不可能有差异，此处验收是形式确认而非真发现风险）。

---

### P11.2 删除真正的死函数（范围已按 §0 收窄）

- `internal/story/preview.go`：删除 `PreviewTitle`（单数版本，第 24–46 行左右）；保留 `PreviewTitles`
  （复数，生产路径）
- `internal/story/preview_test.go`：删除 `TestPreviewTitle` 相关的两个用例（第 40、66 行一带，
  专属测试 `PreviewTitle` 单数版本的部分）——**先确认 `preview_test.go` 里是否还有测试
  `PreviewTitles`（复数）的用例要保留**，只删单数版本相关的
- `cmd/vmr/summary.go`：删除 `configFlag`（第 21–28 行）——它的 doc comment（package doc 第 4 行）
  声称"`cmd_start.go`/`cmd_check.go` 都在用"，但全仓零调用点，这句话本身也要改
- `cmd/vmr/summary.go`：删除 `providerProxyLines`（第 105–113 行）
- `cmd/vmr/main_test.go`：`TestProviderProxyLines_Direct`/`_ProxyFalse`/`_ProxyURL`（第 649–712 行
  一带）三个测试改为直接调用 `providerProxyEntries(cfg)` 取 `[0].Proxy` 断言，而不是经
  `providerProxyLines` 间接断言字符串——保留原有的三种场景�covered（direct / proxy:false /
  proxy:true 凭据脱敏），只是断言对象换成仍存活的共享函数；测试名同步改为
  `TestProviderProxyEntries_*`
- `internal/chatmsg/usage.go`：删除 `ExtractFinish`（第 168–约 200 行,含其内部 SSE 逐行解析逻辑）
- `internal/chatmsg/usage_test.go`：删除 `TestExtractFinish`（第 117–133 行）
- `internal/reqdetail/facts.go`：删除 `ErrorClass`（第 35–49 行，记录级包装）；**保留**
  `AttemptErrorClass`（第 51 行起，`detail.go:125` 生产在用、`helpers_test.go` 测试在用）
- `internal/reqdetail/evidence.go`：删除 `contentHash8`（第 30–39 行）
- `internal/reqdetail/evidence_test.go:103`：`contentHash8("distinctive system prompt text")`
  改为内联等价计算（`md5.Sum` + `hex.EncodeToString(...[:4])`，与生产路径
  `EnsureSysPromptEvidence` 内部实际做的事一致，而不是调用一个即将删除的辅助函数）

**明确保留（连带修正 `KNOWN_ISSUES §1.39` 的错误分类）**：`report.Build`、`report.WriteDetails`、
`report.AnalyzeSessions`、`ctxgraph.Scan`、`story.Build`——理由见 §0 表格。这五个函数在源码里已有
的 doc comment 大多已经写明"是参照实现"这层意图（例如 `build_cached.go:16`
"callers that need it use BuildCached directly"），不需要新增注释；如果巡查发现某处 doc comment
仍暗示"这是遗留代码、该删"，顺手订正。

**验收**：`deadcode` 复扫这 5 项消失，5 项保留项目不受影响；`go test ./internal/story/...
./internal/report/... ./internal/reqdetail/... ./internal/chatmsg/... ./cmd/vmr/... -race` 全绿；
对同一批真实日志的 `vmr report`/`vmr story`/`vmr analyze` 产物逐字节不变。

---

### P11.3 已并入 P11.2

DevPlan 原把"六个非缓存版函数"（P11.2）与"五个零引用小函数"（P11.3）分成两个任务表。复核后
真正要删的函数来自两张表的并集且已大幅缩水（`story.PreviewTitle` 来自原 P11.2、
`configFlag`/`providerProxyLines`/`ExtractFinish`/`ErrorClass`/`contentHash8` 来自原 P11.3 加复核中
新发现的 `providerProxyLines`），拆两个任务已无意义，合并执行、合并验收。

**`health.Registry.Available` 从删除清单移出**：它是 `Acquire`（有副作用，会占用半开探测名额）之外
**唯一**无副作用的可用性查询方法；`internal/health/health_test.go`（3 处）与
`internal/router/router_serve_test.go`（3 处）都靠它在不触发副作用的前提下断言"这个 endpoint 现在
会不会被选中"。删掉它，这 6 处断言没有替代路径可用（`Acquire` 本身会改变状态，用来做断言会污染
后续测试步骤）。**保留，判定修正**，理由记入 `KNOWN_ISSUES`。

---

### P11.4 守卫扩展到源码注释的文件路径引用

**现状**：`internal/archtest/doc_refs_test.go` 的 `TestArchitecture_DocReferences` 只扫描
`CLAUDE.md`/`README.md`/`README.zh.md`/`docs/*.md`（顶层），完全不看任何 `.go` 源文件的注释。
`checkDocRefs` 本身的 `internal/<pkg>/<file>.go` 路径校验逻辑已经存在（`docHasInternalPaths` 门控），
只是从未对 `.go` 源文件打开过。

**改动**（`internal/archtest/doc_refs_test.go`）：

1. `docHasInternalPaths`：加一个 `.go` 后缀分支，与其余判据并列——

   ```go
   func docHasInternalPaths(docRel string) bool {
       return strings.HasSuffix(docRel, ".go") ||
           docRel == "CLAUDE.md" ||
           strings.HasPrefix(docRel, "docs/VirtualModelRouter_Design_v4_") ||
           docRel == "docs/KNOWN_ISSUES_sonnet-5.md"
   }
   ```

   **`docHasSymbols` 不跟着变**：它目前的实现是
   `docHasInternalPaths(docRel) || strings.HasPrefix(docRel, "README") || ...`——如果不改，
   `.go` 文件会顺带打开符号校验（`` `pkg.Symbol` `` 反引号语法在 Go 注释里到处都是，误报面远超
   本任务范围）。改法：把 `docHasSymbols` 现有的四个条件展开成不经过 `docHasInternalPaths` 的
   独立判据，行为对已有四类文档（CLAUDE.md/design docs/KNOWN_ISSUES/README*/UserGuide）不变，
   但不再自动继承 `.go` 分支。

2. 新增 `TestArchitecture_DocReferences_SourceComments`：复用已有的 `loadDocWorld`，
   `filepath.WalkDir` 遍历 `internal/` 与 `cmd/`（不含 `_eval/`——那是独立小工具，不受这套约束；
   若后续要收编，另开任务），跳过 `_test.go`，对每个 `.go` 文件的全文调用
   `checkDocRefs(w, relPath, content)`。relPath 用相对仓库根的路径（如
   `internal/report/detail.go`），使 `reDocPath`（`docs/....md` 存在性）与
   `internal/<pkg>/<file>.go` 路径校验同时生效——这正好覆盖 §2.6 发现的"源码注释引用
   `docs/future-strategy/*.md`"和"源码注释引用已搬迁的 `internal/xxx.go`"两类失效引用。

3. `TestArchitecture_DocReferences_Negative` 补两个用例，证明新分支真的会跳闸：

   ```go
   {"missing source file in a .go comment", "internal/report/detail.go", "see internal/report/nosuchfile.go"},
   {"missing future-strategy doc in a .go comment", "internal/report/detail.go", "see docs/future-strategy/nosuch_xyz.md"},
   ```

   以及一条"合法引用应保持沉默"的 `ok` 用例（例如指向本次仍然存在的
   `docs/future-strategy/story_report_architecture_opus-5.md`），证明新分支不是无差别报警。

**先跑一遍再确认没有存量违规**：在写新测试前，先用一次性脚本（`grep` + 手工核对，不进仓库）扫一遍
`internal/`+`cmd/` 现存的 `docs/future-strategy/*.md` 与 `internal/*.go` 路径引用，确认全部可解析
（开工前已核实：11 处 `docs/future-strategy/` 引用全部指向现存的两份文档，`internal/*.go` 类路径
引用同批复核未发现新的死引用——P2/P3/P10 遗留的三处 `internal/report/render.go` 死引用已在
`story_report_full_review_opus-5.md` 复查时当场修过，见该文 §4 第 2/3/4 项）。**这一步的目的是
确保新测试落地时不会因为存量问题而先天变红**——若发现存量违规，先在本任务内订正引用，再让守卫
生效，不要为了让测试通过而放宽正则。

**验收**：`go test ./internal/archtest/...` 全绿；人为在某个 `.go` 文件注释里写一条指向不存在文件
的引用，跑该测试当场失败；撤回后恢复绿。

---

### P11.5 `docs/future-strategy/` 文档状态归位

**复核结论**：DevPlan 引用的"自称现行、基线停在整轮重构之前"的问题文档就是审阅报告本身点名的
`vmr_future_strategy_v2_sonnet-5.md`。开工前重新核实了 `docs/future-strategy/` 全部 6 份文档的现状：

| 文档 | 状态标注 | 结论 |
| --- | --- | --- |
| `story_report_architecture_opus-5.md` | 无需标注——仍是 P0–P10 及本 P11–P15 序列的权威"为什么"文档，源码里 11 处引用都指向它 | 不动 |
| `story_report_full_review_opus-5.md` | 本文档自身 | 不动 |
| `json_lang_policy_plan_sonnet-5.md` | 已有"现状（2026-08-21）：本文档 §2/§3/§5 定型的方向已在 P8 阶段基本全盘采纳落地"标注 | 不动 |
| `cli_architecture_redesign_gemini-3.7-flash.md` | 已有"现状（2026-08-21）：本文档的核心方向已在 P9 阶段采纳落地……"标注 | 不动 |
| **`vmr_future_strategy_v2_sonnet-5.md`** | 首行自称"**性质：自包含的现行战略文档**"，"基线：commit `4ef2665`（2026-07-27）"——早于 P0（`b098ca9`）；全文 0 次 `vmr analyze`、6 次 `vmr report` | **需要标注** |
| `agent_runtime_implementation_roadmap_gemini-3.7-flash.md` | 无"已采纳/现行"类自我定性，但写于 2026-08-16（P9/P10 之前），全文 9 处 `vmr report`/`vmr story` 示例、0 处 `vmr analyze`——本次复核期间独立发现，DevPlan 未列出 | **顺带标注**（成本低、发现于同一次巡查，不需要单独立项） |

**改动**：给这两份文档各加一段与 `json_lang_policy_plan_sonnet-5.md`/`cli_architecture_redesign_*.md`
同款式的现状标注（引用块，不改写正文——两者都是几万字的业务/路线图文档，正文内容的时效性核查不在
P11 范围内，P11 只解决"文档自称现行但 CLI 表述已经过期"这一个具体误导点）：

- `vmr_future_strategy_v2_sonnet-5.md`：在现有"性质"引用块下方追加一行，
  说明 CLI 命令示例（`vmr report`/`vmr story`）已被 P9（`vmr analyze` 单一入口收敛）取代，
  读者遇到这两个命令时应理解为 `vmr analyze` 的等价历史写法；不改"自包含的现行战略文档"这句定性
  本身（那是关于业务战略部分的定性，与 CLI 命令层无关，本次不越权评估战略内容是否仍然现行）
- `agent_runtime_implementation_roadmap_gemini-3.7-flash.md`：文档开头补一句同类型的现状标注

**验收**：两份文档头部都能让读者在 10 秒内看到"CLI 示例基线早于当前 CLI 收敛"这条信息；
P11.4 的守卫对这两处新增文字不产生新的失败（标注文字本身不引入新的路径/符号引用）。

---

## 3. 执行顺序

1. P11.1（blobindex 删除）——独立，最先做，风险最低
2. P11.2（含合并后的 P11.3）——依赖 P11.1 已把 `ctxgraph` 一侧的引用关系理清，但两者其实互不相关，
   顺序可对调；实际按文件包分组执行：`ctxgraph` → `story` → `report` → `reqdetail` → `chatmsg` →
   `cmd/vmr`，每包改完跑一次该包测试再进下一个
3. P11.4（守卫扩展）——放最后，理由不变：它要检验的是"P11.1/P11.2 有没有在源码注释里留下新的死
   引用"，必须在实际删除动作完成之后再加guard，否则测不出真实效果
4. P11.5（文档标注）——与 1–3 无依赖，穿插执行

## 4. 阶段验收（对齐 DevPlan §6.3 通用完成定义 + 本期新增两条）

- [x] `go build ./...` 绿
- [x] `go test ./... -race` 绿
- [x] `go test ./internal/archtest/...` 绿（含新增的 `TestArchitecture_DocReferences_SourceComments`
      与两条新负例）
- [x] `gofmt -l .` 无输出
- [x] `go vet ./...` 绿
- [x] `go run golang.org/x/tools/cmd/deadcode@latest ./cmd/vmr/...` 不再报告本计划列出的死函数；
      `report.Build`/`report.WriteDetails`/`report.AnalyzeSessions`/`ctxgraph.Scan`/`story.Build`/
      `health.Registry.Available` 仍会出现在报告里（预期内——它们本来就只被测试/`_eval` 调用，
      `deadcode` 分析的是 `main()` 可达性，不代表应该删）
- [x] **默认路径实测**（DevPlan 新增完成定义第 7 条）：对同一批真实日志分别在改动前后跑一次
      `vmr analyze`（默认参数）与 `vmr report -details`，`diff -r` 两次输出目录，**必须逐字节相同**
      ——本阶段唯一允许的差异是新增的 `KNOWN_ISSUES`/DevPlan/ActionPlan 文档本身，不涉及任何
      `logs/` 派生产物
- [x] `KNOWN_ISSUES_sonnet-5.md` §1.39 按 §0 的复核结论订正（见下）
- [x] 本 ActionPlan 文档补执行记录与总结

## 5. 收尾：`KNOWN_ISSUES` 订正

`§1.39` 当前把 `report.Build`/`report.WriteDetails`/`report.AnalyzeSessions`/`ctxgraph.Scan`/
`story.Build` 五项和 `health.Registry.Available` 一起列为"待删死函数"，与本计划 §0 的复核结论矛盾。
执行完成后需要：

1. 订正 `§1.39` 正文，把这六项从"待删"移除，改写"现状"小节说明它们是测试基础设施（缓存正确性的
   独立参照实现 / 大规模测试 fixture 构造入口 / 唯一无副作用的状态查询方法），并注明"2026-08-21
   `story_report_full_review_opus-5.md` 的判定在 P11 开工前重新核实后订正"
2. `§1.39` 的"登记来源"行补一条 P11 ActionPlan 的自我修正记录
3. `§4` 的 ROI 表 `1.39` 一行如涉及具体函数点名，同步更新
4. 新增一条极小的登记：`providerProxyLines` 死函数（DevPlan 未列出，`deadcode` 复核时发现），
   写明它是被 `printProviders`/`providerProxyLine`（单数）取代后的遗留，已删

---

## 6. 执行记录

### 6.1 P11.1 — 删除 `ctxgraph/blobindex.go`

按计划执行，无偏差：删除文件本体（125 行）+ 专属测试
`TestScan_BlobIndexFetchAllRecoversOriginalContent`；`Graph.Index` 字段与 `buildGraph` 里的填充
循环一并删除；`manifest.go`/`cache.go`/`records.go`/`reqcoord_test.go` 五处指向 `BlobIndex` 的
注释改写为指向真正的替代品 `ctxgraph.FetchRecords`（P3 已完成的迁移，只是注释一直没跟上）。
`m.Keys`/`m.MsgIdx` 字段本身按计划保留（`edit.go`/`stitch.go`/`lineage.go`/`report/session.go`/
`story/journey.go`/`story/structure.go`/`story/candidates.go` 仍在用）。

### 6.2 P11.2 — 删除真正的死函数（范围已按 §0 收窄）

按 §0 的复核结论执行：只删 `story.PreviewTitle`（单数）+ `configFlag` + `providerProxyLines`
（deadcode 复扫时发现的计划外第 6 个死函数）+ `chatmsg.ExtractFinish` + `reqdetail.ErrorClass`
（记录级包装，`AttemptErrorClass` 保留）+ `reqdetail.contentHash8`。`report.Build`/
`report.WriteDetails`/`report.AnalyzeSessions`/`ctxgraph.Scan`/`story.Build`/
`health.Registry.Available` 六项按 §0 的判定全部保留，未删。

`providerProxyLines` 的 3 个专属测试（`TestProviderProxyLines_Direct/_ProxyFalse/_ProxyURL`）
改写为 `TestProviderProxyEntries_Direct/_ProxyFalse/_ProxyURL`，直接测试两者共享的
`providerProxyEntries`/`redactProxyURL`，覆盖（direct 判定、`proxy: false` 判定、代理 URL 凭据
脱敏）原样保留，不因删除死函数而丢失。`reqdetail.contentHash8` 删除后，
`evidence_test.go:103` 的期望文件名改为直接调用生产路径实际使用的 `SysPromptEvidenceFileName`，
比原来"再实现一次同样的 md5 哈希"更贴近生产代码，不是单纯的等价替换。

`story.PreviewTitle` 删除后 `preview_test.go` 的三个相关测试调整：
`TestPreviewTitle_NilProfileErrors` 删除（`TestPreviewTitles_NilProfileErrors` 保留，覆盖
批量版的同一条防御）；`TestPreviewTitleAndPreviewTitlesAgree` 改写为
`TestPreviewTitles_ReturnsRealOpeningInstruction`，只保留对批量版行为的验证（"两者一致"这条
断言随单数版本一起消失是必然的，不是覆盖损失）。

### 6.3 P11.4 — 守卫扩展到源码注释的文件路径引用

按计划新增 `TestArchitecture_DocReferences_SourceComments`，遍历 `internal/`+`cmd/` 全部非测试
`.go` 文件，复用既有 `checkDocRefs`。首次跑出比计划预想更多的存量问题，按计划"先订正引用，
再让守卫生效"的原则逐项处理，而不是放宽正则：

1. **两处真实的历史死引用**（`internal/chatmsg/messages.go`、`internal/ctxgraph/cache.go`
   各一处，都是"提到一个已删除的 report 侧旧实现"）——按本仓库处理同类问题的既有惯例
   （`story_report_full_review_opus-5.md` §4 第 2/3/4 项的修法）改写为不含可校验死路径的表述，
   保留"为什么"本身。
2. **三处假阳性**（`internal/i18n/lang.go`）——包名列表未加分隔符被路径正则贪婪匹配成一条
   嵌套路径（如"internal/config/router/server/report/story"被读成单一路径）。修法是给列表补上
   正确的分隔符（逐个 `internal/xxx` 独立成词），而不是改写规则去兼容错误的写法。
3. **一类范围设计缺陷，在实现阶段才暴露**：`checkDocRefs` 原样复用到 `.go` 源文件后，
   `reMarkdownLink`（`[text](x.md)` 语法）在 `internal/i18n/*.go`/`internal/report/render_doc.go`
   上产生了系统性假阳性——这些文件里的 `[vmr-requests.md](./vmr-requests.md)` 是**渲染进生成产物
   的字符串字面量**，不是仓库内的文档交叉引用。这是 P11 ActionPlan 编写阶段没有预见到的一类
   区别（doc-level 检查的假设"markdown 链接语法总是同仓库交叉引用"对 .md 文档成立，对包含
   模板字符串的 .go 源文件不成立）。修法：新增 `docHasMarkdownLinks` 判据，`.go` 源文件不跑
   `reMarkdownLink` 检查，只跑 `reDocPath`（`docs/....md` 裸路径存在性）与
   `internal/<pkg>/<file>.go` 路径检查——这两类在源码注释里确实是真实的交叉引用声明，
   值得守；markdown 链接语法在源码里不是。补了一条负例锁定这条边界。

### 6.4 P11.5 — 文档状态归位

按计划执行：`vmr_future_strategy_v2_sonnet-5.md`（复查原文点名的目标）与
`agent_runtime_implementation_roadmap_gemini-3.7-flash.md`（本次复核独立发现的同类问题，成本低、
顺带处理）各补一段"CLI 表述已过期"标注，不改写正文其余内容。

### 6.5 收尾 — `KNOWN_ISSUES` 订正

`§1.39` 按 §0 的复核结论整条重写后，触发了刚扩展的源码注释守卫本身（§0 表格里提到的"新分类判定"
文本中直接点名了已删除的符号/文件路径，被 `docHasSymbols`/`docHasInternalPaths` 当场拦下——这是
本次 P11 执行期间第二次真实撞见"守卫在自己身上生效"的现场，与
`story_report_full_review_opus-5.md` §4.1 记录的那次是同一类事件）。按该文档已确立的处理方式：
不写全已删除符号的可校验形态，只描述事实本身。最终把 `§1.39` 从"待定问题"整条移到"已闭环"
（§3 第 35 项）——因为 P11 执行完成后它已经是事实上闭环的条目，不该再挂在待办清单里；
相应地同步了 §0 的分布统计（27 条 → 26 条，中危 8 项→7 项）、§4 ROI 表（删掉 `1.39` 一行）与
两处引用它的列表式段落。

### 6.6 验收结果

| 检查项 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `gofmt -l .`（不含 `_eval/`） | 无输出 |
| `go test ./... -race` | 全绿 |
| `go test ./internal/archtest/...` | 全绿，含新增的 `TestArchitecture_DocReferences_SourceComments` 与 4 条新负例 |
| `deadcode ./cmd/vmr/...` 复扫 | 计划列出的死函数全部消失；`report.Build`/`report.WriteDetails`/`report.AnalyzeSessions`/`ctxgraph.Scan`/`story.Build`/`health.Registry.Available` 仍在报告中——符合预期，它们本就只被测试/`_eval` 调用 |
| 默认路径实测（同一批真实日志，改动前后各跑一次 `vmr report -details` 与 `vmr analyze`） | `diff -r` 逐文件核对，全部产物字节相同，唯一差异是 `vmr-report.json` 里的 `generated_at` 运行时间戳 |

### 6.7 总结

P11 最终交付的范围比 DevPlan 原计划小，但比原计划更准确：**真正删除的死代码只有 1 个废弃子系统
+ 6 个零引用小函数**（DevPlan 原计划的 11 项候选里有 5 项被证明不是死代码，5/11 的错误率足以
说明"deadcode 报告不可达"不能不经复核就等同于"可以删除"——不可达可以是真的没用，也可以是
"只服务测试基础设施"，两者对 `main()` 可达性分析呈现完全相同的信号，只有读调用点才能分辨）。
这个订正过程本身验证了 DevPlan 反复强调的方法论：**每个阶段开工前必须基于真实仓库状态重新核实，
不能沿用上一份文档的判定**——本次是 P11 自己的开工前核实推翻了上一份文档（复查报告）的判定，
而不是等到执行完才发现。

`archtest` 守卫扩展到源码注释后，暴露的假阳性（3 处包名列表解析错误、系统性的 markdown-链接
误判）比真实死引用（2 处）还多——这是往生产代码扩展一条为文档设计的检查规则时的正常代价，
不是设计失败：说明这条正则规则本身对"散文体文档"这个原始适用场景是精确的，扩大适用范围到
"包含模板字符串的源代码"时才暴露出未被验证过的边界，值得在规则本身补一条判据（`docHasMarkdownLinks`）
而不是靠人工在测试失败时逐次豁免。

`KNOWN_ISSUES` 的自我修正是本次执行里最能体现"证据说话、不被既有判定束缚"这条要求的一步：
一份两天前才写完的复查报告的具体判定（"六个死函数"），在下一个阶段真正开工前的核实中被证明
五分之六是错的；而这次核实本身，又在收尾阶段被本次新建的守卫当场验证了措辞的准确性
——工具链在这个仓库里已经具备了对"回顾性文字本身是否准确"进行机器校验的能力，这比人工复查
更可靠。

### 6.8 外部独立审阅（gemini-3.7-flash）的核查与处置

`story_report_p11_action_plan_review_gemini-3.7-flash.md` 对本 ActionPlan 做了一次独立审阅。
逐条核实（`git diff HEAD` 对照真实基线，而不是只读当前代码）后，处置如下。

**审阅报告里两条"严重事实错误"指控本身是错的**——审阅者读的是**执行后**的代码状态，把"§2
任务清单描述的是执行前状态、§6 执行记录确认已完成删除"误读成了"ActionPlan 虚构了从未存在的代码"：

- 指控"`story.PreviewTitle`（单数）根本不存在，`preview_test.go:40,66` 测的是复数版"——
  `git diff HEAD -- internal/story/preview.go` 显示单数版确实存在于基线（第 19–35 行区间，
  与 ActionPlan 描述的 24–46 行有小幅出入但主体属实），本次执行已将其删除；
  `preview_test.go` 当时的第 40/66 行确实是单数版的专属测试，已随之删除并改写。
- 指控"`configFlag`/`providerProxyLines` 在基线中已被清理，`main_test.go` 已经是
  `TestProviderProxyEntries_*` 命名"——`git diff HEAD -- cmd/vmr/summary.go` 与
  `cmd/vmr/main_test.go` 同样证明两个函数与三个 `TestProviderProxyLines_*` 测试确实存在于基线，
  本次执行删除/改写。
- 附带的"§2.7 文件清单遗漏 `messages.go`/`i18n/lang.go`"同理不成立——这两处改动本就记在
  §6.3 执行记录里（P11.4 存量问题订正的一部分），审阅者只读了 §2 的原始任务清单，没有对照 §6。

**三条 Tier-1 发现是真实且有价值的，已采纳并修正**：

1. **P11.4 的正则全文扫描确实会把 Go 字符串字面量误当文档引用**——`internal/i18n/report_doc.go`
   等文件的 `docHasMarkdownLinks` 特例正是这类假阳性的产物，属于"发现一起、патch 一起"的被动
   模式。改用 `go/parser`（`parser.ParseComments`）只提取 `ast.File.Comments`
   （`goFileComments`，`doc_refs_test.go`）后，`TestArchitecture_DocReferences_SourceComments`
   现在只看注释文本，字符串字面量结构性地不在扫描范围内——这是消除整类假阳性，不是再打一个补丁。
   `docHasMarkdownLinks` 保留：注释本身仍可能合法引用一个不存在的生成产物文件名，这层判据依然
   有意义，只是不再需要靠它兜底字符串字面量误判。
2. **`_eval/` 是验收盲区，且这个盲区已经藏了一条真实死引用**——`_eval/calibrate_p1b.go` 第
   10–12 行引用的 `docs/future-strategy/phase1b_implementation_plan_gemini-3.7-flash.md`
   早在 `b46500a`（远早于本轮 P0–P11）就已被删除，P11.4 的守卫因显式排除 `_eval/` 而看不到它。
   已修：删除该死引用（改写为不含可校验路径的表述，同本次其他历史死引用的处理方式）；新增
   `TestArchitecture_EvalToolsCompile`（`internal/archtest/eval_build_test.go`）对
   `_eval/calibrate_p1b.go` 做 `go build -o /dev/null` 编译断言——这条测试的价值不止是"曾经
   漏检一次"，而是 P11 保留 `ctxgraph.Scan`/`story.Build` 的理由本身依赖"`_eval` 是活代码"这个
   前提，没有编译守卫，这个前提会随时间静默失效而无人知道。
3. **`report.Build`/`report.AnalyzeSessions` 的定性需要修正，但不影响保留决策**——
   审阅指出两者只是 `buildInternal(..., nil, nil, nil)` /
   `AnalyzeSessionsCached(paths, nil, taskseg.OpenClawAware)` 的 3 行薄封装，不是"独立参照
   实现"，读源码后确认属实：它们与各自的 Cached 版本共享同一套底层算法（`buildInternal`/
   `AnalyzeSessionsCached`），区别只是入参的 cache 是否为 nil。但审阅把 `report.WriteDetails`
   也归入同一类批评——那是错的：`WriteDetails` 是真正独立的 ~50 行两遍扫描实现（自己的
   `audit.OpenLogFile`/`audit.ForEachLine` 循环，不调用 `buildInternal`），这一点在
   `detail.go` 的既有 doc comment 里本就写明。修法按两者的真实性质分别处理：`§1.39`
   的表述已改为"缓存正确性/单双遍等价性差分测试的独立参照实现"这个笼统说法拆分不再准确，
   但由于 `§3` 第 35 项本身已经是"已闭环"的历史记录（记录判定过程，不是当前值守文档），不再
   回改措辞制造第二次漂移；本条修正记在这里，供下次读到 `§3` 第 35 项的人对照。`WriteDetails`
   补了标准 Go `// Deprecated:` 段落（`detail.go`），明确它与 `Build`/`AnalyzeSessions`
   不同——后两者是新测试仍应continue使用的默认构造入口，前者是被冻结、只服务一个差分测试的
   历史实现，不应被当作可复用 API。

**一条 Tier-2/3 建议采纳，一条不采纳**：

- 采纳：`health.Registry.Available` 补充了更明确的 doc comment，说明它是唯一无副作用的可用性
  查询方法、指向 `KNOWN_ISSUES §3` 第 35 项，防止未来被误列入死代码清单（`internal/health/health.go`）。
- 不采纳：审阅建议给 `docs/future-strategy/` 建立正式的文档生命周期分类体系（现行/历史评审/候选
  路线图三类）。这是一个合理方向，但属于 P11 范围之外的独立文档治理决策，不是本阶段"清理与
  守卫"的任务边界内该顺手做的事——已有的按需标注（P11.5）足够解决本阶段实际发现的问题，
  正式分类体系留给需要它的时候再单独提。

**处置后的验收**：`go build`/`go vet`/`gofmt`/`go test ./... -race`/`go test
./internal/archtest/...`（含新增的 `TestArchitecture_EvalToolsCompile`）全部重新跑过，全绿；
`deadcode` 复扫结果不变（符号集合相同，仅行号因新增 doc comment 而偏移）；同一批真实日志的
默认路径实测重新做过一次，产物逐字节相同。
