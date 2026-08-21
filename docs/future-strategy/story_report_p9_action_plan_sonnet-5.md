// Ver 2026-08-21 00:00, by Sonnet 5

# vmr 日志分析体系重构 — P9 ActionPlan（CLI 命令层真正收敛）

## 0. 定位

本文是 `docs/future-strategy/story_report_dev_plan_2_sonnet-5.md`（下称"第二期 DevPlan"）**P9
阶段**的执行级细化，基于本仓库 P9 起点（P0–P8 全部已完成并落地——P0–P6 commit `b098ca9` 至
`2c45a58`、P7 commit `2884d51`、P8 commit `699e73b`）的真实代码状态编写。架构依据见
`docs/future-strategy/story_report_architecture_opus-5.md` §7.9（命令层目标模型）、§7.2（3×2
矩阵，`vmr analyze` 是三级变焦共同的入口）；具体问题登记见 `docs/KNOWN_ISSUES_sonnet-5.md`
§1.30/§1.32/§1.33/§1.34。

**DevPlan review 结论**（本次 review 的产出，已回填进第二期 DevPlan 正文，两处小修）：P9 的任务
范围/边界/验收标准逐条对照当前代码核实后**全部成立**，五项任务（P9.1–P9.5）没有发现需要推翻或
收窄的落差。review 过程中发现两处引用错误——DevPlan §4"P9 详述"的"范围"段与 P9.4 任务行都把
`vmr analyze` 的命令行描述段落引作 `VirtualModelRouter_Design_v4_Analytics.md` §7.9，但该文档
没有 §7.9（其目录只到 §8），真正的位置是 §4.4"配置与命令行"——§7.9 是**另一份文档**
（`story_report_architecture_opus-5.md`）里讨论 CLI 收敛设计的章节号，两份文档章节号被张冠李戴。
已在 DevPlan 正文就地改正为 §4.4，不影响 P9 的范围/任务描述本身。除此之外 DevPlan 原文准确，
本文不再重复这次核实过程。

**P9 范围边界**（与 DevPlan 一致）：`cmd/vmr` 的子命令分派与三套现有 `flag.NewFlagSet`
（`cmd_analyze.go`/`cmd_report.go`/`cmd_story.go`）；`README.md`/`.zh`、`docs/UserGuide.md`/`.zh`、
`docs/VirtualModelRouter_Design_v4_Analytics.md` §4.4、`docs/VirtualModelRouter_Design_v4_Strategy.md`
的对应措辞、`CHANGELOG.md`。**不改**：`internal/report`/`internal/story` 的包边界与两包内部任何
聚合/渲染/Finding 判据（P9 是纯 `cmd/vmr` 组合根层面的重排——两包依旧互不 import，`cmd/vmr` 依旧是
唯一同时看到两半区的组合根）；`internal/reqdetail`/`internal/ctxgraph` 不动。

**已读代码确认的关键前提**（P9.1–P9.5 都要用到或与之相关，先在这里一次性钉住，不在各任务里重复
核实）：

1. **今天的 `vmr analyze` 不是"一个入口"，是"给另两个入口的调用重新拼一遍参数"**
   （`cmd/vmr/cmd_analyze.go`）：`cmdAnalyze` 自己解析一套 8 个 flag（`-c`/`-o`/`-details`/
   `-include-partial`/`-include-self-traffic`/`-lang`/`-currency`/`-report-config`），然后把它们
   重新拼回 `[]string`，依次调用 `cmdStory(storyArgs)`（`storyArgs` 强制附加 `-render-all`）与
   `cmdReport(reportArgs)`。它**没有** `-journey`/`-compare`/`-corpus`——三级变焦今天只能从
   `vmr story` 进，`vmr analyze` 只有"默认套件"这一种输出形态。
2. **调用顺序是 story 先、report 后，且这是对的，只是三处文档写反了**（`cmd_analyze.go:98-103`
   的实现 + 其上注释）：`report.Markdown` 在渲染时如果 `stories/vmr-stories.md` 已存在就会链接
   过去（`loadStoriesLink`，P6.2a），story 先跑能让这条边在**首次**调用就命中。P7 执行记录
   （`story_report_p7_action_plan_sonnet-5.md` §10.1 第 3 点）已经核实过这一点、且明确"不属于 P7
   范围，本次未改动"——P9 是它排定的落点。真实核实结果：`docs/UserGuide.md:549`、
   `docs/UserGuide.zh.md:547`、`docs/VirtualModelRouter_Design_v4_Analytics.md:497`、
   `CHANGELOG.md:21` 四处都写成"先 report 后 story"，与实现相反。
3. **story 侧的"每种变焦模式"渲染函数已经是独立、可复用的顶层函数，不需要重新实现**
   （`cmd/vmr/cmd_story.go`）：`renderJourney`（单个）、`renderJourneys`（给定候选集合批量渲染，
   `renderAllJourneys` 只是 `renderJourneys(cands, ...)` 的一层薄封装，`cmd_story.go:620-623`）、
   `compareJourneys`、`corpusStats`、`listJourneys`、`resolveLLMOptions`（`-llm-*` 校验，
   `cmd_story.go:39-53`）全部是独立函数，`cmdStory` 本身只做"解析 flag → 解出配置 → 扫描建图 →
   按哪个 flag 非空分派到上述某个函数"。P9.1 的"纯 CLI 层路由，不重新实现渲染/聚合逻辑"这条约束，
   落到实现上就是**复用这批既有函数**，不是照着它们的行为重写一遍。
4. **story 侧"扫描建图 + 候选集合 + 索引行"这一段今天是内联在 `cmdStory` 里的，没有被提炼成函数
   ——这是本次唯一需要新写的"胶水"**（`cmd_story.go:102-160`）：`storiesDir`/`indexPath`/
   `priorCache` → `ctxgraph.ScanCached` → `ctxgraph.StitchGraph`/`LineageIndex` →
   `resolveTaskProfile()` → `story.ListCandidates` → （若 `!includeSelfTraffic`）
   `filterSelfTrafficCandidates` → 逐候选算 `chains`/`titles`（`story.PreviewTitles`，一次批量
   IO）→ 逐候选 `story.BuildJourneyIndexRow`（**这一步顺带算出了 `Category`**，
   `internal/story/storyindex.go` 的 `BuildJourneyIndexRow` 内部调用 `classifyJourney`）→
   `story.MergeJourneyIndexRows` 拼出 `idx`。P9.2 要的"候选分类"不需要新写任何分类逻辑——
   `freshRows[i].Category`（`story.JourneyCategory`，导出类型，`CategoryTask`/`CategoryCron`/
   `CategoryHeartbeat`/`CategorySubagent` 四个导出常量）在这一步就已经算好，P9.2 只是"用已经
   算好的这个字段过滤一次 `cands`"。
5. **report 侧同理：`cmdReport` 的 flag 解析（`cmd_report.go:249-259`）与"解出配置"
   （260-297）之后，从 297 行到函数末尾（399 行）是一段没有分支的线性管线**——
   `report.BuildCached` → 写 `vmr-report.json`/`.md` → 详单收尾 → 写 requests 索引/失败索引，
   全程只消费已经解出的值（`outDir`/`detailsOn`/`lang`/`displayCCY`/`rc.ExchangeRate`/
   `excludeClientTags`/`cfg`/`pricingSrc`/`pricingInfo`），没有再读一次 `*flag.FlagSet`。这段可以
   原样提出去做成一个接收"已解出参数"的函数，不需要拆解内部逻辑。
6. **`archtest` 的函数行数豁免表里，`cmdReport`/`cmdStory` 都是"记录在当前大小，没有余量"**
   （`internal/archtest/func_sizes_test.go:57-58`：`cmdReport` 155、`cmdStory` 145，注释原文
   "Recorded at ~current size, NOT rounded up: the point is that these cannot grow further
   without a deliberate edit here"）。P9.1 的提炼会**缩小**这两个函数（把线性管线挪进新函数），
   所以不会撞到这两条豁免；但会新增至少一个函数（`cmdAnalyze` 自己的 flag 解析 + 分派逻辑，
   拼上 18 个 flag 的并集），大概率超过 `defaultFuncLineLimit`（120 行）——按同一张表里
   `cmdReport`/`cmdStream` 的先例（"Top-level command/entry-point bodies: flag parsing, wiring,
   and a linear happy path... splitting them tends to produce helpers with one caller and no
   independent meaning"），新增一条 `"cmd/vmr/cmd_analyze.go:cmdAnalyze": N` 豁免是符合这张表
   自己的设计意图的，不是绕过纪律。文件级预算：`cmd/vmr/cmd_story.go` 今天 780/850（`file_sizes_
   test.go:77`）、`cmd/vmr/cmd_report.go` 400/500（`:79`）、`cmd/vmr/cmd_analyze.go` 未登记
   （适用全局默认 700 行）——三个文件都有余量，但 `cmd_story.go` 只剩 70 行，需要在实现时留意
   （§7 有开放确认点）。
7. **`self_traffic` 排除规则的输入不对称，根因就在 `cmd_report.go` 没有 `-llm-key` flag**
   （`cmd/vmr/selftraffic.go:28-29` 的 doc comment 原文明写"report has no -llm-key flag of its
   own, see cmd_report.go"）：`cmd_story.go:87` 是 `llmKey := resolveString(*llmKeyFlag, rc.LLMKey,
   "")`（flag 可覆盖），`cmd_report.go:276` 直接用 `rc.LLMKey`（无 flag）。P9.1 统一 flag 集合后，
   `vmr analyze` 天然拥有 `-llm-key`；只要 `cmdAnalyze` 对 story 半区与 report 半区**用同一个已解出
   的 `llmKey` 值**（而不是像今天的桥接方式那样，report 侧永远读不到这个 flag），P9.5 就随 P9.1
   自动成立，不需要给 `vmr report` 别名本身新增 flag（那会违反 P9.3"别名产出与之前一致"的约束）。
8. **`report.yaml`（`reportConfig`，`cmd/vmr/reportconfig.go:35-64`）没有、也不需要新增任何字段**
   来支撑 P9.2 的默认渲染范围决策——"默认只渲染 `task` 类候选"是 `vmr analyze` 这一个命令自己的
   默认值决策，不是一个用户会想在 `report.yaml` 里跨命令配置的持久化偏好（`vmr story -render-all`
   的语义完全不变，见任务二）。
9. **`vmr replay -req <coord> -print` 已经是"统一的记录选择器"读取原语**（P6.5 交付），P9 不需要
   新增任何读取入口；`README.md`/`.zh` 与 `UserGuide.md`/`.zh` 补 `vmr analyze` 示例时，若示例里
   提到"如何看某条记录的原文"，直接引用它，不要在 P9 范围内讨论是否需要 `vmr cat`（DevPlan §1 已
   拍板不做）。

---

## 1. 执行前置检查

```bash
cd /Volumes/SSD2T/code/vmr
git status --short                     # 确认工作区干净（除本文件与 DevPlan 的两处引用修正外）
go build -o /private/tmp/claude-501/-Volumes-SSD2T-code-vmr/*/scratchpad/vmrbin ./cmd/vmr
go test ./... 2>&1 | tail -30           # 建立改动前的基线：全绿
go test ./internal/archtest/...         # 记录改动前的行数豁免基线，方便对比改动后的增量
ls logs/*.jsonl.zst 2>/dev/null | wc -l # 确认本机有 P0-P8 用过的同一批真实样本日志（34 个文件）可用
```

**任务顺序有真实依赖，不是文档顺序**：P9.1（统一入口 + 提炼共享函数）是地基，P9.2（默认渲染范围）
只是往 P9.1 提炼出的调用点插一次过滤，P9.3（别名化）复用 P9.1 提炼出的同一批函数——这三者是**同一次
重构的三个可验收切面**，建议按 P9.1 → P9.2 → P9.3 顺序实现但**一次性提交**（中间状态下
`cmd_analyze.go` 会同时具备"新流程"和"旧的参数拼接桥接"两套逻辑，属于设计上不该长期存在的过渡态，
不宜作为独立提交点）。P9.4（文档）依赖 P9.1–P9.3 的最终行为定型（文档要写实现之后的样子，不是
实现之前的猜测）。P9.5 是确认性任务，验收即完成，不需要独立代码改动。每个大步骤做完后跑一次
`go build ./... && go test ./cmd/vmr/... ./internal/archtest/...`，不要攒到最后一起查错。

---

## 2. 任务一：`vmr analyze` 统一入口 + 三级变焦选择器（P9.1）

### 2.1 现状（已读代码确认，见 §0 第 1/3/4/5/6 点）

`cmd_analyze.go` 是一个 105 行的参数转发层：解析自己的 8 个 flag，重新拼成两组 `[]string`，
分别调用 `cmdStory`/`cmdReport`。它做不到"选中一个变焦倍率就只渲染那一个视图"，因为它对 story 侧
永远追加 `-render-all`、且从不透传 `-journey`/`-compare`/`-corpus`（这三个 flag 甚至没有在
`cmd_analyze.go` 里定义）。`cmdStory`/`cmdReport` 各自的"解析 flag → 解出配置 → 执行" 三段式今天
耦合在一个函数体内，没有对外暴露"给定已解出的配置，执行一次"这个原语。

### 2.2 目标设计

**(a) 两个新的共享执行函数，落在各自现有文件里（不新建文件——两者都只有一个调用方增加，不构成
"提炼成叶子包"的理由；且它们仍然只被 `cmd/vmr` 内部消费，不产生新的包边界）：**

```go
// cmd_report.go — 替换 cmdReport 249 行之后的线性管线部分
type reportRunOpts struct {
	configPath        string
	outDir            string
	detailsOn         bool
	lang              i18n.Lang
	displayCCY        string
	exchangeRate      map[string]float64
	excludeClientTags map[string]bool
}

// runReport executes vmr report's full pipeline against already-resolved
// options — the piece cmdReport's flag/report.yaml resolution feeds into,
// factored out so cmdAnalyze can drive the same pipeline with its own
// resolved values instead of re-serializing them into a []string and
// re-parsing (P9.1).
func runReport(paths []string, tw timestampWriter, opts reportRunOpts) error {
	// body = today's cmd_report.go:279-399, parametrized on opts fields
	// instead of *flag reads; identical logic, identical output.
}

func cmdReport(args []string) error {
	// flag definitions + fs.Parse + resolveInputPaths: unchanged
	// resolve rc/lang/outDir/detailsOn/excludeClientTags/displayCCY: unchanged
	return runReport(paths, tw, reportRunOpts{ /* ...resolved values... */ })
}
```

```go
// cmd_story.go — 提炼"扫描建图 + 候选集合 + 索引行"这一段
type storySetup struct {
	g         *ctxgraph.Graph
	byIdx     map[int]*ctxgraph.Lineage
	cands     []*ctxgraph.Lineage       // ListCandidates 的结果，已按 self-traffic 过滤
	chains    [][]*ctxgraph.Lineage     // cands[i] 对应的完整链
	freshRows []story.JourneyIndexRow   // 含已算好的 Category（P9.2 直接消费）
	idx       *story.StoryIndex
	firstPath string
	prof      taskseg.Profile
}

// setupStoryRun runs the scan/stitch/candidate/index-row pipeline shared by
// every vmr story mode — today inlined in cmdStory (102-160行) — so
// cmdAnalyze can reuse it without going through cmdStory's own flag set
// (P9.1). llmKey/selfTrafficTags/includeSelfTraffic drive the same
// self-traffic exclusion cmdStory already applies (P9.5's shared-resolution
// point).
func setupStoryRun(paths []string, outDir string, includeSelfTraffic bool, llmKey string, selfTrafficTags []string, showUngrouped bool, lang i18n.Lang) (*storySetup, error) {
	// body = today's cmd_story.go:102-160, parametrized
}

func cmdStory(args []string) error {
	// flag definitions + fs.Parse + validation（互斥性校验不变）: unchanged
	// resolve rc/lang/outDir/includePartial/llmOpts: unchanged
	su, err := setupStoryRun(paths, outDir, *includeSelfTraffic, llmKey, rc.SelfTrafficClientTags, *showUngrouped, lang)
	if err != nil { return err }
	// 162-193 行的分派逻辑不变，只是改读 su.cands/su.byIdx/su.idx/... 而不是局部变量
}
```

**(b) `cmd_analyze.go` 重写为真正的统一入口**：一套 `flag.NewFlagSet` 收敛 `cmd_report.go` 与
`cmd_story.go` 两套 flag 的并集（`-c`/`-o`/`-lang`/`-report-config`/`-include-self-traffic` 是
共享的，只定义一次）：

| 来自 | flag |
| --- | --- |
| 共享 | `-c` `-o` `-lang` `-report-config` `-include-self-traffic` |
| report 独有 | `-details` `-currency` |
| story 独有 | `-journey` `-compare` `-corpus` `-render-all` `-include-partial` `-show-ungrouped` `-llm-addr` `-llm-model` `-llm-key` `-llm-cache-dir` `-llm-dry-run` |

分派逻辑（`-journey`/`-compare`/`-corpus` 互斥，复用 `cmdStory` 今天已有的互斥性校验代码，原样
搬过来而不是重新写一遍判断）：

```go
switch {
case *corpus:
    // 只跑 story 半区的 corpus 模式（复用 corpusStats），report 半区不跑
case *compare != "":
    // 只跑 story 半区的 compare 模式（复用 compareJourneys）
case *journeyArg != "":
    // 只跑 story 半区的 journey 模式（复用 renderJourney/renderJourneys，按匹配数量分支——与 cmdStory 今天的逻辑一致）
default:
    // 默认套件模式：story 半区（renderAllJourneys，范围见 P9.2）在前，report 半区在后
    // ——顺序沿用 §0 第 2 点的既有理由，不需要重新论证
}
```

`-render-all` 在带了任一选择器（`-journey`/`-compare`/`-corpus`）时**报错拒绝**（与今天
`-corpus` 拒绝 `-journey`/`-render-all`/`-compare` 同一种"提前报错、不要静默忽略"的风格）——
它在统一入口下的唯一角色是默认套件模式的一个范围开关（P9.2），不再是一种变焦选择方式。

**验证标准的精确含义**（DevPlan 原文"internal/report/internal/story 的 diff 为零"）：`git diff
internal/report internal/story` 在 P9.1–P9.3 全部完成后必须为空——这不是一句口号，是这次重构
唯一的硬约束：所有改动只能发生在 `cmd/vmr` 内。

### 2.3 具体步骤

1. 在 `cmd_report.go` 新增 `reportRunOpts` 与 `runReport`，把今天 `cmdReport` 249 行之后
   （`fs.Parse` 完成、`paths` 解出之后）到函数末尾的执行体原样搬进 `runReport`，只做"从局部变量
   改读 `opts` 字段"这一种改动，不改变任何一行业务逻辑、任何一次函数调用的参数顺序。`cmdReport`
   收窄为"解析 flag → 解出配置 → 调用 `runReport`"。
2. 在 `cmd_story.go` 新增 `storySetup` 与 `setupStoryRun`，把今天 `cmdStory` 102-160 行的执行体
   原样搬进去，同样只做"改读参数/返回结构体字段"这一种改动。`cmdStory` 收窄为"解析 flag → 解出
   配置 → 调用 `setupStoryRun` → 按今天 162-193 行同样的分派逻辑调用 `renderJourney`/
   `renderJourneys`/`renderAllJourneys`/`compareJourneys`/`corpusStats`/`listJourneys`"。
3. 跑一次 `go build ./... && go test ./cmd/vmr/...`——这一步结束时 `cmdReport`/`cmdStory` 的
   **外部行为必须与改动前逐字节相同**（它们的 flag 集合、默认值、输出完全没变，只是内部多了一层
   函数调用）。用本机真实日志对 `vmr report`/`vmr story -render-all`/`vmr story -journey <id>`
   分别跑一遍，`diff` 改动前后的 `reports/` 输出目录，必须为空。这一步是后续步骤的安全网：如果
   这里 diff 不为空，说明提炼过程本身引入了偏差，必须先修好再继续，不要带着已知偏差往下叠加改动。
4. 重写 `cmd_analyze.go`：定义 §2.2(b) 的并集 flag 集合，解析后解出共享配置（`rc`/`lang`/
   `outDir`/`excludeClientTags` 等——**只解一次**，report 半区与 story 半区共用，这是相对今天
   "两次独立加载 report.yaml"的一个自然改进，不需要额外设计），按互斥选择器分派。
5. 迁移互斥性校验：把 `cmdStory` 今天 96-101 行的"`-llm-addr` 与批量模式互斥"、"`-corpus` 与其余
   选择器互斥"这两条校验逻辑复制到 `cmd_analyze.go`（不是共享函数——`cmd_analyze.go` 的选择器
   语义比 `cmd_story.go` 窄，`cmd_story.go` 里"`-journey` 与 `-render-all` 同时给出时 `-journey`
   静默优先"这条今天存在的宽松行为不需要延续到统一入口，统一入口应该对"选择器 + `-render-all`"
   这种组合直接报错，见 §2.2(b) 末尾）。
6. `go build ./... && go test ./cmd/vmr/... ./internal/archtest/...`。若 `cmdAnalyze` 超过
   `defaultFuncLineLimit`（120 行），在 `internal/archtest/func_sizes_test.go` 的
   `funcLineExemptions` 里新增 `"cmd/vmr/cmd_analyze.go:cmdAnalyze": N`（N 取实际行数，不要
   预留余量——同一张表里其余条目的注释已经解释过为什么"不要 rounded up"）。若 `cmd_story.go`
   超过 850 行文件预算，在 `file_sizes_test.go` 里相应调整该文件的登记值（同样按实测值 +
   合理缓冲，参考 P8 §5 的"+15~20%"惯例）。

### 2.4 验收标准（对照 DevPlan P9.1）

- 每种现有 `vmr story` 调用方式（`-journey id`、`-journey 'glob*'`、`-journey a,b`、
  `-render-all`、`-compare a,b`、`-corpus`、`-llm-addr` 组合、无 flag 列表模式），在
  `vmr analyze` 用等价参数下得到逐字节相同的产物（`diff -r` 两次运行各自的输出目录，忽略
  `vmr-report.md`/`.json` 里必然变化的生成时间戳字段）。
- `git diff internal/report internal/story`（相对本任务开工前）为空。
- `go test ./...`、`go test ./internal/archtest/...`、`gofmt -l .`、`go vet ./...` 全绿。

---

## 3. 任务二：`vmr analyze` 默认渲染范围改为 `category == task`（P9.2）

### 3.1 现状（已读代码确认，见 §0 第 1/4 点；数据见 `KNOWN_ISSUES §1.30`/`§1.32`）

`renderAllJourneys(cands, ...)`（`cmd_story.go:620-623`）不做任何范围过滤，直接把
`ListCandidates` 的全部结果转交给 `renderJourneys`。今天 `cmd_analyze.go` 无条件传
`-render-all` 给 story 半区，等于"默认套件模式 = 全量物化"。真实语料（34 文件、477 候选）实测：
238 个 `task`、112 个 `cron`、107 个 `heartbeat`、20 个 `subagent`（`§1.30` 引用的
P6.3 数字）；单日样本 `vmr analyze` 产出 164MB（其中 `details/` 160MB/306 份），全量语料外推
约 5.7GB 且被 SIGKILL。

### 3.2 目标设计

`setupStoryRun` 返回的 `storySetup.freshRows[i].Category` 与 `storySetup.cands[i]` 按下标一一
对应（`freshRows`/`cands`/`chains` 三者今天在 `cmdStory` 里本来就是同下标平行数组，提炼进
`storySetup` 后这个不变式原样保留，不需要额外维护一份映射）。默认套件模式下，`cmd_analyze.go`
在调用 `renderAllJourneys` 之前插一次过滤：

```go
func taskOnlyCandidates(su *storySetup) []*ctxgraph.Lineage {
	var out []*ctxgraph.Lineage
	for i, l := range su.cands {
		if su.freshRows[i].Category == story.CategoryTask {
			out = append(out, l)
		}
	}
	return out
}
```

`-render-all` 显式传入时跳过这次过滤，直接用 `su.cands`（全部候选，与今天 `vmr story
-render-all` 的语义完全一致——这也是为什么 `-render-all` 的行为不能挪进 `internal/story` 包内部：
它是"要不要过滤"这个 CLI 层默认值决策的开关，不是一个新的聚合规则）。**`cron`/`heartbeat`/
`subagent` 类候选依然进 `vmr-stories.json`/`.md` 索引**（`BuildJourneyIndexRow` 对全部候选一视
同仁地计算索引行，过滤只发生在"要不要渲染 `journey-<id>.md`+详单"这一步）——**但索引里的折叠规则
本身不因这次改动而变**（独立复核核实过一处需要订正的表述：`internal/story/storyindex.go` 的
`RenderStoryIndexMarkdown` 只折叠 `CategoryHeartbeat`/`CategorySubagent` 两类，`CategoryCron`
与 `CategoryTask` 同样留在主表且默认展开；早先草稿"cron/heartbeat/subagent 折叠展示不变"的措辞
把 cron 也算进了折叠类，不准确）：`task`/`cron` 都在主表里，`task` 的"报告"列渲染成可点击链接，
`cron`（默认套件下未物化）渲染成 `writeStoryIndexRow` 的"未生成"占位（`r.Rendered == ""` 时的
既有行为，本次不改）；`heartbeat`/`subagent` 依旧折叠在 `<details>` 里、同样未物化。链接指向一个
尚未物化的 journey 文件时，读者按需单独跑 `vmr analyze -journey <该id>` 即可补渲染（幂等写盘，
架构文档 §7.3(c) 已论证过这条链接策略）。

### 3.3 具体步骤

1. 在 `cmd_analyze.go` 加 `taskOnlyCandidates(su *storySetup) []*ctxgraph.Lineage`（或作为
   `storySetup` 的一个方法，二选一，按实现时哪种读起来更顺手判断，不是需要预先拍板的架构决策）。
2. 默认套件模式的调用点从 `renderAllJourneys(su.cands, ...)` 改为：
   ```go
   scope := su.cands
   if !*renderAllFlag {
       scope = taskOnlyCandidates(su)
   }
   if err := renderAllJourneys(scope, su.byIdx, su.firstPath, su.prof, includePartial, outDir, lang, su.idx); err != nil {
       return err
   }
   ```
3. `vmr story`（P9.3 落地后的别名）内部调用同一个 `setupStoryRun`/`renderAllJourneys` 路径时，
   **不做这次过滤**——`-render-all` 在 `vmr story` 自己的 flag 集合里永远是"全部候选"，过滤只是
   `vmr analyze` 这一个调用点的默认值选择，不要把它下沉进 `renderAllJourneys` 或 `setupStoryRun`
   本身（那会连带改变 `vmr story -render-all` 的行为，违反 P9.3 的"别名产出与之前一致"）。
4. 真实语料验证：跑一次全量 34 文件 `vmr analyze`（默认，无 `-render-all`），确认不再 SIGKILL、
   记录实际耗时/内存峰值/`reports/` 目录体积；跑一次 `vmr analyze -render-all` 作对照，确认它
   与"跑改动前的 `vmr analyze`"（若还能承受，用之前 SIGKILL 前记录的单日样本数字对照即可，不必
   重跑一次全量的旧版本）在候选覆盖上一致。

### 3.4 验收标准（对照 DevPlan P9.2）

- 全量语料（34 文件）默认 `vmr analyze` 不再被 SIGKILL，正常退出。
- 单日样本（`vmr-audit-2026-07-28.jsonl.zst`，6 个候选、全部是 `task` 类——P7 执行记录已确认
  这批样本没有 cron/heartbeat/subagent 候选）产物体积与改动前一致（这批样本本来就不触发过滤，
  是一个"改动不应该影响无 cron/heartbeat 候选场景"的回归检验，不是范围验证——范围验证需要全量
  语料，因为单日样本没有非 task 候选）。
- `vmr-stories.md`/`.json` 中 `cron`/`heartbeat`/`subagent` 类候选行数不变（索引不受渲染范围
  收窄影响），但对应的 `journey-<id>.md` 文件在默认套件模式下不存在（`test -f` 为假）；
  `vmr analyze -render-all` 或 `vmr analyze -journey <该id>` 补渲染后 `test -f` 为真。

---

## 4. 任务三：`vmr report`/`vmr story` 降级为过渡别名（P9.3）

### 4.1 现状（已读代码确认，见 §0 第 1/2 点）

`cmdReport`/`cmdStory` 是两个独立的顶层命令，各自完整实现，互不感知对方存在，也不感知
`cmdAnalyze`（今天反而是 `cmdAnalyze` 反向调用它们）。`main.go` 的 `switch os.Args[1]` 三个分支
各自独立分派。

### 4.2 目标设计

P9.1 完成后，`cmdReport`/`cmdStory` 本身就已经是"解析自己的 flag → 调用共享执行函数
（`runReport`/`setupStoryRun` + 既有分派函数）"的结构——它们**已经**是薄封装，不需要为了"降级为
别名"再做一次结构调整。P9.3 要做的只有一件事：**各自的 flag 解析完成、即将调用共享执行函数之前，
打印一行 stderr 迁移提示**。不阻塞、不影响 `stdout`/返回值/退出码：

```go
func cmdReport(args []string) error {
	// ...flag 解析、配置解出，与 P9.1 之后的现状完全一致...
	fmt.Fprintln(os.Stderr, "vmr report: this is now an alias for `vmr analyze` (report half only); the same flags work unchanged under `vmr analyze` — see `vmr analyze -h`. This alias remains supported, no action required.")
	return runReport(paths, tw, opts)
}
```

`vmr story` 同理，提示文案指出"用 `-journey`/`-compare`/`-corpus`/`-render-all` 选变焦"。
**不逐参数生成"等价的 vmr analyze 命令行"**——两条命令的 flag 集合本来就是 `vmr analyze` 并集的
子集，逐参数拼接等价命令行的唯一好处是"用户可以直接复制粘贴"，但维护成本（要跟着两条命令未来
任何 flag 变化同步更新拼接逻辑）不成比例；一句话说明"flag 不变、换个命令名"已经足够，DevPlan
"含等价 vmr analyze 写法"这条验收标准按"提示文本里出现 `vmr analyze` 字样并说明如何达到同样效果"
理解，不要求逐字符精确的可执行命令行。

### 4.3 具体步骤

1. 在 `runReport`/`setupStoryRun` 调用点之前各加一行 `fmt.Fprintln(os.Stderr, ...)`，中英文各一份
   文案（跟随 `lang` 参数，走 `internal/i18n` 还是内联字符串是一个实现细节——**倾向内联**：这是
   一次性的过渡提示，不是长期维护的产品文案，且 `lang` 在这个时间点已经解出，用
   `i18n.CLI(lang)` 已有的 struct 加两个新字段也可以，两种做法都能过验收，选哪个不影响正确性）。
2. `go test ./cmd/vmr/...`——检查现有测试是否有断言 `cmdReport`/`cmdStory` 的 stderr 输出为空/
   特定内容（若有，需要更新断言以容纳新增的一行；`main_test.go`/`cmd_story_test.go`/
   `reportconfig_test.go` 是最可能命中的文件，执行时先 `grep -n "Stderr\|stderr" cmd/vmr/*_test.go`
   摸底，不要假设没有）。
3. `main.go` 的 usage 文本与三行子命令摘要（第 66-74 行）**不需要改结构**（三个子命令依然三选一
   分派），只需要在 `report`/`story` 两行末尾补一句"（deprecated alias for `vmr analyze`，见下）"
   或类似措辞，与 P9.4 的文档同步一并做，不单独算一个步骤。

### 4.4 验收标准（对照 DevPlan P9.3）

- `vmr report <args>`/`vmr story <args>` 对任意既有调用方式，产物与 P9.1 完成、P9.3 之前的状态
  逐字节相同（因为 P9.3 唯一的改动是多打印一行 stderr，不touch 任何返回值路径）。
- 两条命令的 stderr 输出各自新增恰好一行提示，包含字面量 `vmr analyze`；`stdout`/退出码/写盘
  产物不受影响。
- `go test ./cmd/vmr/...` 全绿（含更新后的 stderr 相关断言，如有）。

---

## 5. 任务四：文档与门面同步（P9.4）

### 5.1 现状（已读代码/文档确认，本文 §0 第 2 点 + 本次 review 过程核实）

四处"先 report 后 story"执行顺序描述与实现相反：`docs/UserGuide.md:549`、
`docs/UserGuide.zh.md:547`、`docs/VirtualModelRouter_Design_v4_Analytics.md:497-503`
（§4.4 段落，非 DevPlan 原文误引的 §7.9——见 §0 的"DevPlan review 结论"）、`CHANGELOG.md:21`。
`README.md`/`README.zh.md` 里 `vmr analyze` 出现次数为 0（`grep -c` 已核实）。
`docs/VirtualModelRouter_Design_v4_Strategy.md:115` 承诺"保持现有 ... `vmr story` 与
`vmr story -compare` 分析指令独立高效"，命令拼写变化后这句话仍然成立（能力不变、只是入口变了），
但措辞需要跟进说明"`vmr story` 现在是 `vmr analyze` 的别名"这一层新事实
（`story_report_architecture_opus-5.md` §10.3 已点名这处）。

### 5.2 具体步骤

1. **`docs/VirtualModelRouter_Design_v4_Analytics.md` §4.4**：改写 497-503 行（`vmr analyze`
   段落）。核心变化：从"依次跑 `vmr report`、`vmr story -render-all`，是 `cmd/vmr` 组合根里的
   第三个入口"改写为"`vmr analyze` 是统一分析入口，`-journey`/`-compare`/`-corpus` 选变焦，
   不带选择器时是默认套件（story 半区先、report 半区后；默认只渲染 `task` 类候选，`-render-all`
   全量物化）；`vmr report`/`vmr story` 降级为打印一行迁移提示的过渡别名"。同时更新
   485-505 行区间涉及的字段说明表（若有 flag 层面的表格需要同步并入 `-journey`/`-compare`/
   `-corpus`/`-render-all`）。
2. **`docs/UserGuide.md`/`docs/UserGuide.zh.md`**：改写 548-549 / 546-547 行的命令参考表——
   `vmr analyze` 一行需要补全新的 flag 集合（含三个选择器）与"story 先、report 后"的正确顺序；
   `vmr report`/`vmr story` 两行加一句"deprecated alias"说明。搜索这两份文档里其余提到
   "先 report 后 story"或类似顺序描述的地方（不止命令参考表这一处，例如"运行进度"一节如果也
   描述了执行顺序，需要一并核对——执行 `grep -n "先.*report\|report.*先" docs/UserGuide.md
   docs/UserGuide.zh.md` 摸底，不要只改命令参考表这一处就假设完成）。
3. **`README.md`/`README.zh.md`**：补 `vmr analyze` 的命令示例（当前完全没有）。示例应体现
   "默认套件"与至少一个变焦选择器（如 `-journey`）两种用法，不需要穷举全部 flag——README 是
   门面，不是命令参考。
4. **`docs/VirtualModelRouter_Design_v4_Strategy.md:115`**：措辞从"保持现有 `vmr story` 与
   `vmr story -compare` 分析指令独立高效"调整为反映"`vmr story` 现在是 `vmr analyze` 的别名，
   能力与产出不变"这一层——不改变这句话的承诺本身（能力确实还是独立高效），只更新命令拼写的
   现状描述。
5. **`CHANGELOG.md`**：`[Unreleased]/### Changed` 新增一条，说明这是破坏性变更：
   `vmr analyze` 现在是唯一推荐入口，支持 `-journey`/`-compare`/`-corpus` 三级变焦选择器，
   默认渲染范围收窄为 `task` 类候选（`-render-all` 保留全量物化）；`vmr report`/`vmr story`
   仍可用，但已弃用，会打印迁移提示。同时修正第 21 行"先 report 后 story"的执行顺序描述——
   这不是新增条目，是订正一条既有 Added 条目里的事实错误，就地改写该行，不要在 Changed 区再
   记一条"修正了 Added 区的错误"。
6. **`selftraffic.go` 的 doc comment 顺带更新**（`cmd_report.go` 的行为在这次重构后没有变化，
   但注释里"report has no -llm-key flag of its own"这句话在 `vmr analyze` 的 report 半区语境下
   不再是完整事实——P9.5 会让 `vmr analyze -llm-key X` 的 report 半区也能读到非空 `llmKey`）：
   把这句话改为"`cmd_report.go`/`vmr report` 别名本身没有 `-llm-key` flag；`vmr analyze` 统一
   flag 集合下 report 半区与 story 半区共用同一个已解出的 `llmKey`（P9.5）"。这一步严格来说
   属于代码注释而非"文档"，但与 P9.4 的"措辞同步"性质相同，一并在这里执行，不单独立一个任务。

### 5.3 验收标准（对照 DevPlan P9.4）

- `grep -c 'vmr analyze' README.md` > 0，`README.zh.md` 同理。
- 四处执行顺序描述全部改为"story 先、report 后"，`grep -rn "先.*report.*后.*story\|report.*后.*
  story"` 类模式在这四份文档里不再命中与实现矛盾的表述。
- `docs/VirtualModelRouter_Design_v4_Analytics.md` 不再出现 §7.9 这个错误引用（本身该文档就没有
  §7.9 这个章节号，此项在 DevPlan review 阶段已经修正，这里是确认文档本身内容准确，不是确认
  引用准确）。
- `CHANGELOG.md` `[Unreleased]/### Changed` 有且仅有一条描述本次 CLI 收敛的新条目；第 21 行的
  执行顺序错误已订正。

---

## 6. 任务五：确认自指流量输入不对称随 P9.1 自然消失（P9.5）

### 6.1 现状（已读代码确认，见 §0 第 7 点）

`KNOWN_ISSUES §1.34` 记录：`cmd_story.go` 的 `llmKey` 可被 `-llm-key` flag 覆盖，
`cmd_report.go` 的 `llmKey` 只能来自 `report.yaml` 的 `rc.LLMKey`，二者在 `vmr story -llm-key X`
与 `vmr report`（同一目录、同一份 `report.yaml`）下会算出不同的自指流量排除集合。

### 6.2 目标设计与具体步骤

这一项**不需要独立的代码改动**——P9.1 的 `cmd_analyze.go` 统一 flag 集合天然包含 `-llm-key`
（来自 story 侧的 flag 并集，见 §2.2(b) 表格），且 P9.1 步骤 4"解出共享配置，只解一次"意味着
`llmKey := resolveString(*llmKeyFlag, rc.LLMKey, "")` 在 `cmd_analyze.go` 里只算一次，同时喂给
`setupStoryRun`（story 半区的 `filterSelfTrafficCandidates`）与 `reportRunOpts.excludeClientTags`
（经 `selfTrafficExcludeTags(llmKey, rc.SelfTrafficClientTags)`，report 半区）。**本任务的全部
工作是在 P9.1 完成后做一次显式验证，并更新 `KNOWN_ISSUES`**——不要在 P9.1 之外再实现一遍。

1. 用一份配置了 `llm_key`（正常值）、`report.yaml` 之外再用一个不同凭证生成过自指流量的样本
   （若本机没有现成样本，用 `-llm-key <与 report.yaml 不同的值>` 显式覆盖来构造这个场景），跑
   `vmr analyze -llm-key <值> -journey <id>` 与 `vmr analyze -llm-key <值>`（默认套件，同时驱动
   report/story 两半区），核对两侧排除的 `client_key_tag` 集合一致（`vmr-report.json` 的
   `meta.self_traffic_excluded` 计数与 `vmr-stories.json` 候选列表排除掉的条目要对得上）。
2. `docs/KNOWN_ISSUES_sonnet-5.md` `§1.34` 的这一条移入 `§3` 已闭环（沿用该文件已有的登记格式，
   参照 P7/P8 已经移入 `§3` 的条目写法：一句话描述根因+修法+真实语料验证结果，"曾登记为 §1.34"）；
   `§0`/`§4` 的分布统计与 ROI 表覆盖范围声明按惯例同步重算（`§4` 里 `1.32` 这次一并移入 `§3`
   ——`1.32` 的修法就是 P9.2，验收标准相同，不需要为它单独重复一次核实过程）。

### 6.3 验收标准（对照 DevPlan P9.5）

- `vmr analyze -llm-key X`（带或不带变焦选择器）的自指流量排除口径一致，用真实/构造样本验证过，
  不是仅凭代码走查判断。
- `KNOWN_ISSUES §1.34` 该条、`§1.30`/`§1.32`（P9.2 的验收已经满足，一并移入）移入 `§3`；
  `§4` ROI 表与覆盖范围声明同步更新。

---

## 7. 需要在实现时确认、不预先假设的几个点

以下几点是本文设计阶段能预判到"存在选择"、但不值得在开工前拍板的实现细节——按项目一贯的
"以第一性原理和实事求是的态度就地判断"处理，事后如实记录落差即可，不要因为这几点悬而未决就
推迟开工：

1. **`cmd_story.go` 提炼 `setupStoryRun` 后是否会超过 850 行文件预算**：目前只剩 70 行余量，
   新增的 `storySetup` struct + `setupStoryRun` 函数体（原地搬迁，理论上净增量很小，主要是
   struct 定义本身的十几行）大概率不会撞线，但如果撞线，按 `archtest` 一贯做法——先看是否有
   自然的拆分点（例如把 `storySetup`/`setupStoryRun` 单独挪到新文件 `cmd_story_setup.go`，
   仍在 `cmd/vmr` 包内，不产生新的包边界），而不是简单调高预算数字。
2. **`cmdAnalyze` 的具体行数与是否需要拆成多个函数**：18 个 flag 的定义 + 互斥性校验 + 分派
   逻辑，实测行数落地后再决定是"一个函数 + 一条 `funcLineExemptions` 登记"还是"拆成
   `cmdAnalyze`（flag 解析）+ `dispatchAnalyze`（分派）两个函数，后者留在默认 120 行预算内"。
   两种做法都合规，选择依据是哪种读起来更像`cmdReport`/`cmdStory` 已有的风格（它们是"一个函数、
   一条豁免"），倾向于跟随既有风格保持一致性，除非实测行数悬殊过大。
3. **迁移提示文案是否走 `internal/i18n`**：§4.3 步骤 1 已给出倾向（内联，不新增 i18n 条目），
   但如果实现时发现 `i18n.CLI(lang)` 已有的某个字段可以直接复用（而不是新增字段），优先复用。
4. **`vmr replay -req` 相关的 README/UserGuide 示例是否需要在 P9.4 里顺带补充**：DevPlan 没有
   把这个列进 P9.4 的范围，本文 §0 第 9 点也说明了不需要，但如果 P9.4 步骤 3（README 示例）写
   到"如何查看某条记录原文"时自然带出 `vmr replay -print -req`，顺手写上即可，不必刻意回避，
   也不必刻意寻找机会插入。

---

## 8. 收尾（P9.1–P9.5 共用）

1. `go build ./... && go test ./... -race ./cmd/vmr/... && go test ./internal/archtest/...`
   全绿；`gofmt -l .`、`go vet ./...` 无输出。
2. 用本机真实日志做一次端到端走查（DevPlan 原文的阶段验收标准）：`vmr analyze` 默认调用、
   `-journey`、`-compare`、`-corpus` 四种形态各跑一遍，产物与 P9 开工前的等价旧调用逐项比对；
   `vmr report`/`vmr story` 仍可独立跑通并给出迁移提示；全量 34 文件语料一次完整运行不再复现
   SIGKILL（记录实际耗时与 `reports/` 目录体积，作为本次改动的量化证据，参照 P6/P7/P8 的既有
   记录风格）。
3. `CHANGELOG.md`/`KNOWN_ISSUES_sonnet-5.md` 按 §5/§6 的步骤更新；`docs/future-strategy/
   story_report_dev_plan_2_sonnet-5.md` 的 P9 行状态标注为 ✅ 已完成，注明完成日期与本文件路径
   （沿用 P7/P8 行已有的写法："已完成，日期，见 `story_report_p9_action_plan_sonnet-5.md` §N
   执行记录"），并在本文件追加一节"执行记录"记录实现期发现的落差（如有）——不要预先在本文里
   空占位这一节,按 P7/P8 的惯例,执行完毕后再追加。
4. 所有改动先留给人工 review，不在本次会话内自动提交（沿用 P7/P8 的收尾方式）。

---

## 9. 验收清单（对照第二期 DevPlan P9 的验收标准逐项勾）

- [x] `vmr analyze` 默认调用、`-journey`、`-compare`、`-corpus` 四种形态与收敛前的等价旧调用
      产物逐项比对一致（P9.1）
- [x] `git diff internal/report internal/story` 为空（P9.1）
- [x] 全量语料默认 `vmr analyze` 不再 SIGKILL；`-render-all` 保留全量物化能力（P9.2）——**实测发现
      仅靠范围收窄不够，追加了批处理修法，见 §10**
- [x] `vmr report`/`vmr story` 仍可独立跑通、产物与之前一致，且新增迁移提示（P9.3）
- [x] 四处执行顺序文档描述订正为"story 先、report 后"；README 补 `vmr analyze` 示例；Strategy
      文档措辞同步；CHANGELOG 记录本次变更（P9.4）
- [x] `vmr analyze -llm-key X` 在带/不带选择器下自指流量排除口径一致，`KNOWN_ISSUES §1.30`/
      `§1.32`/`§1.34` 移入已闭环（P9.5）——**实测发现一处连带缺陷并修复，见 §10**
- [x] `go test ./...`、`go test ./internal/archtest/...`、`gofmt -l .`、`go vet ./...` 全绿

---

## 10. 执行记录（2026-08-21）

**范围**：本文 P9.1–P9.5 全部任务已按本文设计执行完毕，`go build`/`go test ./...`/
`go test ./internal/archtest/...`/`gofmt -l .`/`go vet ./...` 全绿；用本机真实全量日志语料
（`logs/*.jsonl.zst`，34 个文件、11274 条记录、压缩 645MB）跑通默认 `vmr analyze`，从 SIGKILL
（改动前的历史基线）到正常退出。所有改动尚未提交，留待人工 review。

### 10.1 与本文设计的落差（均为实现期发现，按"以第一性原理和实事求是的态度就地判断"处理，不预先假设）

1. **P9.2 最大的一处落差：仅做 `category == task` 范围收窄不足以解决 SIGKILL，必须再加一次批处理修法**。
   本文 §3（任务二）与 `KNOWN_ISSUES §1.30`/`§1.32` 原文都把"默认只渲染 task 类候选"当作 SIGKILL
   的完整修法。真实全量语料实测推翻了这个假设：`task` 类候选虽只占候选总数的 49%（234/473），却占了
   **83.5% 的请求量**（9259/11086）——真正长的任务对话本来就分在 `task` 类，按类别过滤候选**数量**
   并不能按比例削减批次的实际**数据量**。用改动前的代码单独验证（只做范围收窄、未加批处理）：全量语料
   默认 `vmr analyze` 峰值内存（`/usr/bin/time -l` 的 `peak memory footprint`）约 **35.5GB**，进程仍被
   系统杀死，与改动前"强制 `-render-all`"的失败模式相同，只是延迟了触发点。
   根因定位到 `internal/story/journey.go` 的 `BuildAll`：它对整批候选做**一次性** `ctxgraph.FetchRecords`
   （P1/P2 时代刻意的 I/O 优化——共享一次文件扫描，而不是每个候选各读一遍源文件），在候选批次较大时
   变成无界内存——`cmd_story.go` 的 `renderJourneys` 拿到 234 个候选后一次性传给 `BuildAll`，相当于
   把 9259 条请求的完整记录体同时解析进内存。
   **修法**（不在本文原设计范围内，是执行期新增）：`renderJourneys` 改为按 `renderBatchSize`（20，
   `cmd_story.go` 新增常量）分批调用 `story.BuildAll`——每批构建完立即写盘，下一批开始前上一批的
   `Journey`/记录体可以被 GC。这个函数是 `renderAllJourneys`/`-journey` 多匹配批渲染共用的唯一实现，
   改一处即覆盖两条路径，且**不改变 `internal/story`/`internal/report` 任何一行**（`git diff` 验证
   仍为空）。
   **真实语料验证结果**：同一份 34 文件语料，默认 `vmr analyze`（234 个 task 候选、9259 条请求）
   从 SIGKILL（约 35.5GB 峰值）→ **正常退出**（exit 0），峰值内存 **4.59GB**（`maximum resident
   set size` 4.26GB），总耗时 413s（~6.9 分钟），输出目录 3.5GB（`stories/` 58MB + `details/`
   8343 个文件、约 3.44GB）。用最终代码（含 §10.2 的 `resolveLLMOpts` 重构）重跑一遍同一命令，
   确认正常退出、无回归。
   **这条修法已经补进 §3（任务二）正文与 `KNOWN_ISSUES §3` 第 31 项**，不是遗漏，是执行期发现后
   就地纠正——本文 §7 开放确认点原本预判的是"文件/函数行数预算"这类实现细节风险，没有预判到
   "范围收窄本身可能不足以解决问题"这一条，这是本次执行最大的一次假设推翻。
2. **P9.5 的一处连带缺陷：`resolveLLMOptions` 的无条件调用会让纯 `-llm-key` 用法在批量模式下报错**。
   本文 §6（任务五）原设计认为"`llmKey` 只解析一次、同时喂给两半区"就完成了 P9.5。实现期发现：
   `cmdAnalyze` 若像最初写法那样无条件调用 `resolveLLMOptions(llmAddr, llmModel, llmKey, dryRun)`
   校验"能否发起一次可用的 LLM 调用"，会让单独设置 `-llm-key`（不带 `-llm-addr`——这正是"只用于
   排除自指流量、这次运行不发起 LLM 调用"的合理用法，也是最初诊断 §1.34 时设想的典型场景）在
   `-corpus`/默认套件模式下直接报错退出——即便这两种模式从不消费 `llmOpts` 本身。这是一个 P9 之前
   就存在于 `cmd_story.go` 里的潜在缺陷（`resolveLLMOptions` 一直是无条件调用），只是收敛到单一
   入口、且默认套件结构性禁止 `-llm-addr` 之后，才让这条路径完全走不通、变成一个真正会被撞到的缺陷。
   **修法**：`resolveLLMOptions` 改为只在 `-journey`/`-compare` 分支里按需调用（`dispatchAnalyze`
   的 `resolveLLMOpts` 闭包），`llmKey` 本身的解析与自指流量排除逻辑不再依赖它。新增测试
   `TestCmdAnalyze_LLMKeyExcludesSelfTrafficFromBothHalves` 用一个不存在 `report.yaml`、纯靠
   `-llm-key` CLI flag 的场景钉住这条路径。
3. **`cmdAnalyze` 两次触达行数预算，按 archtest 自己的规则处理，不是绕过**：第一次（P9.1 落地后）
   143 行，按既有先例（`cmdReport`/`cmdStory` 曾经的豁免条目）登记豁免；第二次（P9.5 的
   `resolveLLMOpts` 重构后）涨到 165 行，超过刚登记的豁免值——`internal/archtest` 的报错信息原文
   写明"shorten it, don't raise the number"，于是没有再抬高豁免值，而是把 `cmdAnalyze` 拆成
   "flag 解析+resolve"（`cmdAnalyze`）与"setupStoryRun+按选择器分派"（新增 `dispatchAnalyze`，
   接收一个 `analyzeRun` 结构体）两个函数——拆完两者分别是 106/75 行，都在默认 120 行预算内，
   `cmdReport`/`cmdStory` 曾经的豁免条目也随之从表里删掉（它们早已缩到 120 行以下，继续挂着
   155/145 的豁免值属于"陈旧的过度豁免"，一并清理，这是独立复核 §1.1 指出的问题）。
4. **`cmd_story.go` 提炼 `setupStoryRun` 后确实触达文件行数预算**：本文 §7 开放确认点 1 预判到了
   这个可能性。实测提炼后文件到 855/850 行，超预算 5 行。按预判的处置方式：新建 `cmd_story_setup.go`
   承载 `storySetup`/`setupStoryRun`，不调高 850 这个数字——拆分后 `cmd_story.go` 773 行、
   `cmd_story_setup.go` 98 行，两者都在各自预算内。
5. **一份独立复核**（`story_report_p9_action_plan_review_gemini-3.7-flash.md`，在本文执行过程中
   出现）核实后采纳了其中四项有价值的发现，均已落地：
   - `func_sizes_test.go` 的 `cmdReport`/`cmdStory` 豁免清理与 `runReport` 登记——落地时已经这样做
     （见上第 3 点，独立复核确认了这个处理方向）。
   - `vmr-stories.md` 里 `CategoryCron` 的折叠状态描述有误——本文 §3.2 原文把 `cron` 也算进了
     "折叠展示不变"，但 `internal/story/storyindex.go` 的 `RenderStoryIndexMarkdown` 只折叠
     `CategoryHeartbeat`/`CategorySubagent` 两类，`CategoryCron` 与 `CategoryTask` 一样留在主表
     （默认展开）。已在 §3.2 正文订正。
   - `vmr report`/`vmr story` 的迁移提示文案容易让用户误以为 `vmr analyze` 有对应的"仅报表"/
     "仅列表"单选开关——已重写两条提示语，明确说明 `vmr analyze` 默认套件与各自旧命令行为的
     实际差异（`vmr report` 一节措辞见 `cmd_report.go`，`vmr story` 一节见 `cmd_story.go`）。
   - 补充 P9 专属自动化测试——已在 `cmd_analyze_test.go` 新增 9 个测试（互斥性校验、默认范围
     task-only 过滤 + `-render-all` 反例、三个选择器各自只跑 story 半区、`-llm-addr` 批量模式
     拒绝、别名迁移提示、P9.5 自指流量双半区一致性），覆盖独立复核列出的全部四类场景。
   独立复核同时指出的 `cmd_story.go` 提炼后可能超预算（当时实测 781/850，预判"若超标应拆至
   `cmd_story_setup.go`，不调高上限"）——落地时果然超标（855/850），按其给出的方案原样处理
   （见上第 4 点）。

### 10.2 真实语料验证要点

- **单日样本**（`logs/vmr-audit-2026-07-28.jsonl.zst`，322 条记录）：`vmr analyze` 默认套件
  渲染 6 个 journey（该样本全部候选都是 `task` 类，P7 时代已核实），产物与 P9 之前逐字节一致，
  作为"改动不影响无噪声候选场景"的回归检验。
- **全量语料**（34 个文件、11274 条记录）：默认 `vmr analyze` 首次因批处理修法验证成功（详见
  §10.1 第 1 点的数字），第二次用最终代码（含 §10.1 第 2/3 点的重构）重跑确认无回归、正常退出。
- **P9.1 新增测试**（`cmd_analyze_test.go`）：`-journey`/`-compare`/`-corpus` 三种选择器各自
  只运行 story 半区（`vmr-report.md` 不存在）；两两选择器同传、选择器与 `-render-all` 同传均
  报错；`vmr report`/`vmr story` 别名跑通、stderr 含迁移提示且提及 `vmr analyze`。
- **P9.5 新增测试**：`-llm-key`（不带 `-llm-addr`、不存在 `report.yaml`）在默认套件下正常运行，
  `vmr-stories.json`（1 候选）与 `vmr-report.json` 的 `meta.self_traffic_excluded`（2 条记录）
  一致排除同一批自指流量记录。

### 10.3 文档同步

`CHANGELOG.md` `[Unreleased]`：`### Added` 的 `vmr analyze` 条目就地改写为收敛后的最终行为描述
（analyze 本身尚未发布过任何 tag，不需要额外记一条"变更"）；`### Changed` 新增两条——`vmr report`/
`vmr story` 降级为过渡别名（破坏性变更），以及批处理修法本身。`docs/KNOWN_ISSUES_sonnet-5.md`：
`§1.30`/`§1.32`/`§1.33`/`§1.34` 移入 `§3`（新增第 31–34 项，含批处理修法与自指流量连带缺陷的
完整记录）；`§0`/`§4` 的分布统计、ROI 总表与"分档结论"同步重算（`§4` 不再有任何高 ROI 待办条目）。
`docs/UserGuide.md`/`.zh`、`docs/VirtualModelRouter_Design_v4_Analytics.md`、`CHANGELOG.md` 四处
执行顺序描述订正为"story 先、report 后"；`README.md`/`.zh` 补 `vmr analyze` 示例与能力条目；
`docs/VirtualModelRouter_Design_v4_Strategy.md` 措辞同步；`cmd/vmr/main.go` 的顶部命令摘要与
`usage()` 文本同步调整为以 `vmr analyze` 为主、`report`/`story` 标注为过渡别名。

### 10.4 未做的事（按 P9 边界声明，本文未预判需要而未做）

未改变 `internal/report`/`internal/story` 的包边界或任何一行代码（`git diff` 验证为空，全程
守住）；未引入新的顶层命令（`vmr cat` 等，DevPlan §1 已拍板不做）；未给 `renderBatchSize`
新增命令行开关——这是一个内部实现细节，不是需要用户调节的语义旋钮，20 是一个保守常量，不是
针对某个内存预算算出来的精确值（本文 §10.1 第 1 点已说明原因）；`docs/future-strategy/
story_report_dev_plan_2_sonnet-5.md` 的 P10（历史文档清理）未启动——按其自身"每阶段开工前基于
该阶段起点的真实状态重新写 ActionPlan"的约定，留给 P10 自己的 ActionPlan 处理。
