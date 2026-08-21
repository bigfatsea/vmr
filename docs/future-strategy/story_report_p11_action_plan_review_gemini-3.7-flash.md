// Ver 2026-08-21 16:55, by gemini-3.7-flash

# P11 执行计划独立审阅与事实核查报告

> **重要声明：针对本文档所描述问题开展核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**

---

## 0. 概述与核心结论

本文档针对 [`story_report_full_review_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_full_review_opus-5.md) 第 6 章（DevPlan P11–P15）以及 [`story_report_p11_action_plan_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p11_action_plan_sonnet-5.md)（P11 执行计划）进行独立的事实核查、设计评估与策略研判。

### 0.1 核心审阅结论

1. **核心方向与关键刹车有效**：
   - Sonnet-5 的 ActionPlan 准确识别并阻止了 Opus-5 全面复查中对测试基础设施（如 [`ctxgraph.Scan`](file:///Users/stanford/code/vmr/internal/ctxgraph/scan.go#L37)、[`story.Build`](file:///Users/stanford/code/vmr/internal/story/journey.go#L184)、[`report.Build`](file:///Users/stanford/code/vmr/internal/report/build_cached.go#L18) 等）的盲目删除，避免了破坏全仓数十处测试 fixture 和 [`_eval/calibrate_p1b.go`](file:///Users/stanford/code/vmr/_eval/calibrate_p1b.go) 的风险。
   - 准确保留了 [`health.Registry.Available`](file:///Users/stanford/code/vmr/internal/health/health.go#L51) 作为唯一无副作用的只读状态检查接口。
   - 成功将文档引用守卫扩展至源码注释（P11.4），并为历史战略文档补充了 CLI 过期标注（P11.5）。

2. **存在显著的事实性错误与幻觉分析**：
   - **虚构删除目标与伪行号**：ActionPlan 声称删除 [`internal/story/preview.go`](file:///Users/stanford/code/vmr/internal/story/preview.go) 第 24–46 行的 `PreviewTitle`（单数）及 `preview_test.go` 第 40/66 行的专属测试。**事实核查表明：代码库中根本不存在单数版 `PreviewTitle`**，`preview_test.go` 的对应行实际是在测试复数版 [`PreviewTitles`](file:///Users/stanford/code/vmr/internal/story/preview.go#L31) 与 [`titleFromRecord`](file:///Users/stanford/code/vmr/internal/story/preview.go#L52)。若机械执行该删除，将直接损毁正常测试。
   - **概念混淆**：ActionPlan 将 [`report.Build`](file:///Users/stanford/code/vmr/internal/report/build_cached.go#L18) 与 [`report.AnalyzeSessions`](file:///Users/stanford/code/vmr/internal/report/session.go#L191) 论证为"独立的无缓存差分参照实现"。**事实核查表明：两者均只是传递 `nil` 缓存参数给内部实现的 3 行便利包装函数（Convenience Wrappers）**，而非独立实现。保留它们的真实价值在于降低 25+ 测试用例的调用噪音，而非作为差分算法基准。

3. **高 ROI 设计缺陷与治理盲区**：
   - **P11.4 守卫实现脆弱**：[`internal/archtest/doc_refs_test.go`](file:///Users/stanford/code/vmr/internal/archtest/doc_refs_test.go) 采用全文件纯文本正则匹配扫描 `.go` 文件，未通过 AST 提取注释，导致代码字符串、字面量与注释混杂，存在假阳性与假阴性风险。
   - **`_eval` 验证脱幅**：全仓 CI 和阶段验收均忽略以 `_` 开头的目录，导致 [`_eval/calibrate_p1b.go`](file:///Users/stanford/code/vmr/_eval/calibrate_p1b.go) 处于无编译/语法守卫状态（且其内部包含未被发现的历史断链引用）。
   - **保留函数未设边界**：[`report.WriteDetails`](file:///Users/stanford/code/vmr/internal/report/detail.go#L233) 拥有 50+ 行两遍扫描独立逻辑，若不标记为 Deprecated/Test-only，将在 P12（指纹改造）和 P13（详单 93% 瘦身）演进中诱发严重的差分测试断裂与双重维护负担。

---

## 1. 关键信息事实核查 (Fact-Checking)

依据真实代码库状态，对 ActionPlan 中的关键主张与事实描述进行逐项核实：

### 1.1 事实核查表

| 核查对象 | ActionPlan 主张 | 真实代码库事实 (Ground Truth) | 核查判定 | 详细影响与根因分析 |
| --- | --- | --- | :---: | --- |
| `story.PreviewTitle`（单数） | 声称存在于 `internal/story/preview.go:24-46`，且 `preview_test.go:40, 66` 为其专属测试，定为"确认删除" | `internal/story/preview.go` 全长 70 行，**仅存在 `ID`、`PreviewTitles`（复数）与 `titleFromRecord`**，无单数版；`preview_test.go:40, 66` 测的是复数版 `PreviewTitles` 与 `titleFromRecord` | **严重事实错误** | ActionPlan 依赖历史印象或幻觉生成了虚假行号与删除指令。若盲目按行删除将损毁复数版 `PreviewTitles` 的测试用例。 |
| `cmd/vmr/summary.go` 小函数 | 声称 `configFlag`（21–28行）、`providerProxyLines`（105–113行）为待删死函数，且需重写 `main_test.go:649-712` | `summary.go` 全长 92 行，上述两函数在基线中已被重构/清理；`main_test.go` 中用例已命名为 `TestProviderProxyEntries_*` 并直测共享函数 | **事实漂移/行号虚构** | 该清理在更早提交中已实质落地或行号已严重偏移，ActionPlan 描述脱离代码现状。 |
| `report.Build` | "唯一'无缓存'路径，是缓存正确性差分基准与单/双遍等价性证明的参照实现，共 4 个测试点，保留" | `Build` 仅是 3 行代码：`buildInternal(..., nil, nil, nil)`。在全仓 **25+ 处测试用例**（`aggregate_test.go` 等）中被广泛作为默认无配置调用入口 | **保留正确，但理由与性质描述错误** | 不是独立参照实现，而是参数简化的 Convenience Wrapper。保留价值在于避免重写数十处测试调用，而非差分参照。 |
| `report.AnalyzeSessions` | "TestAnalyzeSessionsCached_ColdCacheMatchesAnalyzeSessions 的参照实现；7+ 处测试调用，保留" | 仅是 3 行代码：`AnalyzeSessionsCached(paths, nil, taskseg.OpenClawAware)`。全仓 **14+ 处测试** 直接调用 | **保留正确，但性质为便利封装** | 同上，是避免在测试 fixture 中频繁传递 Profile 与 nil cache 的便利入口。 |
| `report.WriteDetails` | "与 Build 相同，旧两遍路径 vs 新 onRecord 单遍路径字节级等价性回归的另一半，共 4 处测试调用，保留" | 确为 50+ 行独立的两遍扫描实现，目前由 `detail_test.go:300` 的 `TestBuildOnRecordMatchesWriteDetails` 用于证明等价性 | **保留正确，但潜藏演化风险** | 生产已全面切换至 `BuildCached` 的 `onRecord` 流式写入，保留此函数需警惕 P12/P13 详单重构时的兼容陷阱。 |
| `ctxgraph.Scan` & `story.Build` | "全仓 15 / 50+ 处测试用例搭建 fixture 的基础入口，且 _eval/calibrate_p1b.go 为真实生产调用方，保留" | 证实 `_eval/calibrate_p1b.go:75, 83` 确实直接调用两者，且两个函数是单 lineage / 单图构建的基本原语，数十个测试严重依赖 | **完全属实，保留决策正确** | 成功避免了为了消除局部 deadcode 警告而将大规模测试 fixture 强行重构为批量接口的巨大浪费。 |
| `health.Registry.Available` | "Acquire 之外唯一无副作用的可用性查询方法，6 处测试依赖它断言状态，从删除清单移出" | `internal/health/health.go:51` 的 `Available` 仅持 `RLock`，而 `Acquire` 会触发半开探测名额递增（副作用）；测试依赖只读断言 | **完全属实，保留决策正确** | 准确辨析了读操作与写操作的副作用边界。 |
| `_eval/` 目录特性 | "_eval/ 目录以 _ 开头，被 go build/list/deadcode 自动排除，但它是活代码" | Go 工具链规范对 `.` 或 `_` 开头的目录天然排除于 `./...` 模式之外 | **完全属实** | 揭示了 `deadcode ./cmd/vmr/...` 报告的盲区。 |

---

## 2. 设计点与策略疏漏按严重程度 + ROI 分级评估

依据 **问题严重程度（Severity）** 与 **长期改进投资回报率（ROI）**，将发现的设计疏漏与优化项划分为三个梯队。**严重程度高或 ROI 极高的项目均列入第一梯队并作深度剖析**。

```
┌────────────────────────────────────────────────────────────────────────┐
│ 第一梯队 (Tier 1): 严重程度高 / ROI 极高 (深度分析 + 给出落地方案)       │
│  - 2.1 P11.4 守卫机制设计缺陷: 正则扫全文件导致假阳性/假阴性脆弱性        │
│  - 2.2 保留函数未设维护边界: report.WriteDetails 对 P12/P13 的潜在阻塞   │
│  - 2.3 验收闭环存在重大盲区: 缺少对 _eval/ 脚本的常驻编译守卫            │
├────────────────────────────────────────────────────────────────────────┤
│ 第二梯队 (Tier 2): 严重程度中等 / ROI 中等                             │
│  - 2.4 ActionPlan 事实核查纪律漂移与虚构行号修正                       │
│  - 2.5 战略文档状态标注的生命周期与归档策略缺失                         │
├────────────────────────────────────────────────────────────────────────┤
│ 第三梯队 (Tier 3): 严重程度低 / 长期收益与代码卫生改进                  │
│  - 2.6 health.Registry.Available 注释边界补全                         │
│  - 2.7 实际代码变更与 ActionPlan 文件清单的微小差异对齐                │
└────────────────────────────────────────────────────────────────────────┘
```

---

### 第一梯队 (Tier 1: 严重程度高 / ROI 极高)

#### 2.1 P11.4 守卫机制设计缺陷：纯文本正则扫描 `.go` 全文，存在假阳性与假阴性风险

- **问题严重程度**：高
- **长期改进 ROI**：极高（改动仅需约 20 行标准库代码，彻底杜绝后续 CI 假报警与漏检）

##### 深度分析与问题根因
在 P11.4 的实现 [`internal/archtest/doc_refs_test.go`](file:///Users/stanford/code/vmr/internal/archtest/doc_refs_test.go#L301) 中，新增的 `TestArchitecture_DocReferences_SourceComments` 通过 `os.ReadFile` 直接读取 Go 源文件字节流，并将其全文送入 `checkDocRefs` 进行正则匹配：
```go
reInternalRef = regexp.MustCompile(`(?:vmr/)?internal/([a-zA-Z0-9_]+(?:/[a-zA-Z0-9_\-.*]+)*)`)
reDocPath     = regexp.MustCompile(`\bdocs/[a-zA-Z0-9_\-./*]+\.md\b`)
```

这种"把 Go 源代码当纯 Markdown 文本扫"的做法存在三大缺陷：
1. **假阳性（False Positives）风险**：
   - 如果 Go 代码中的**字符串字面量**（如日志格式化字符串、HTTP URL 路径、测试 mock 数据、错误提示信息）包含形如 `"internal/foo"` 或 `"docs/bar.md"` 的子串，会被当成文档引用强行校验。如果该路径不是实际存在的包或文件，测试将当场变红。
   - 尽管 Sonnet-5 在实现时将 `docHasSymbols` 与 `docHasMarkdownLinks` 针对 `.go` 文件关闭，但这只是治标不治本的补丁。
2. **假阴性（False Negatives）与误匹配**：
   - 正则 `reInternalRef` 强制要求匹配前缀 `(?:vmr/)?internal/`。如果开发者在注释中书写相对路径（例如 `// see ../reqdetail/render.go` 或 `// see render.go`），守卫完全漏检。
   - 对 Go 注释中常见的行内代码标记、第三方库引用（如 `github.com/gin-gonic/gin/internal/bytesconv`）若不慎截断匹配，可能引发误判。
3. **破坏关注点分离**：
   - Go 拥有极其强大且轻量的官方解析工具库 `go/parser` 和 `go/token`（`loadDocWorld` 已经引入并使用了它们）。放弃现成的 AST 注释提取，退化为全文正则匹配，属于典型的方法论降级。

##### 优化解决方案
使用 `go/parser.ParseFile(fset, path, nil, parser.ParseComments)` 仅提取源文件中的 `ast.File.Comments`，只对其注释文本执行路径校验：

```go
// 建议优化实现方案：
func extractGoComments(path string) (string, error) {
    fset := token.NewFileSet()
    node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
    if err != nil {
        return "", err
    }
    var b strings.Builder
    for _, cg := range node.Comments {
        b.WriteString(cg.Text())
        b.WriteByte('\n')
    }
    return b.String(), nil
}
```
**收益**：
- 隔离所有代码逻辑、字符串常量与 import 声明，只审查纯粹的注释文本。
- 彻底消除字符串字面量引发 CI 偶发阻塞的风险。

---

#### 2.2 保留的 `report.WriteDetails` 缺乏明确废弃/测试专用边界，将在 P12/P13 诱发维护陷阱

- **问题严重程度**：高
- **长期改进 ROI**：高（提前理清测试基准边界，防止后续核心体积削减阶段返工）

##### 深度分析与问题根因
在 P11.2 的裁决中，[`report.WriteDetails`](file:///Users/stanford/code/vmr/internal/report/detail.go#L233) 被完整保留在 `internal/report` 的生产源文件中，理由是：
> "旧两遍路径 vs 新 onRecord 单遍路径字节级等价性回归的另一半（`TestBuildOnRecordMatchesWriteDetails` 等 4 处）"。

**这在当下是合理的回归基准，但在后续阶段演进中存在严重设计隐患**：
1. **与后续 P12 / P13 产生直接冲突**：
   - **P12（渲染正确性）** 将在详单内写入渲染指纹（Fingerprint）并在 `EnsureRendered` 中重构跳过谓词。
   - **P13（体积纪律归位）** 将执行两项颠覆性减法：(a) 削减 Raw SSE 41% 体积；(b) 削减历史消息 52% 体积，并改造 `onRecord` 流式写入行为。
2. **双重维护负担**：
   - `WriteDetails` 内部是完全独立的物理读取循环（`audit.OpenLogFile` -> `audit.ForEachLine` -> `sess.Lookup` -> `dw.submit`）。
   - 如果后续开发者修改了详单渲染契约（例如传递增量 context 或新指纹），而没有同步修改这个 50+ 行的非生产函数，`TestBuildOnRecordMatchesWriteDetails` 将持续暴红，或者迫使开发者为这个"早已不被生产调用的旧实现"反复适配新特性。

##### 优化解决方案
1. **语义降级与边界固化**：
   在 `internal/report/detail.go` 中明确为 `WriteDetails` 添加标准 Go 注释：
   ```go
   // Deprecated: WriteDetails is retained solely as a differential verification baseline
   // in detail_test.go (TestBuildOnRecordMatchesWriteDetails) to ensure single-pass
   // onRecord streaming matches the legacy two-pass output. Production callers must use
   // BuildCached with onRecord.
   ```
2. **终态规划**：
   在 P13（体积改造阶段）完成单遍流式测试固化后，将 `WriteDetails` 及其专用测试移入 `detail_test.go` 作为测试内部私有辅助函数，或者在单双遍等价性验证使命完成后予以终结，防止其伪装成生产 API 长期滞留。

---

#### 2.3 P11 验收闭环存在重大盲区：完全遗漏了 `_eval` 脚本的编译与语法守卫

- **问题严重程度**：高
- **长期改进 ROI**：极高（零成本增加 1 行测试/脚本，封堵 CI 盲区）

##### 深度分析与问题根因
1. **工具链天然盲区**：
   ActionPlan §0 指出，`_eval/calibrate_p1b.go` 是 `ctxgraph.Scan` 和 `story.Build` 的真实生产调用方，之所以 `deadcode` 报告死代码，是因为 `_eval/` 目录前缀带下划线，被 `go build ./...`、`go test ./...` 和 `deadcode` 自动排除。
2. **验收标准脱节**：
   ActionPlan §4（阶段验收）和 DevPlan §6.3 列出的所有编译检查指令：
   `go build ./...` / `go test ./... -race` / `go vet ./...`
   **全部无法触达 `_eval` 目录**！
3. **已经发生的真实缺陷**：
   经本次实测审查，[`_eval/calibrate_p1b.go`](file:///Users/stanford/code/vmr/_eval/calibrate_p1b.go#L10) 第 10 行正文中赫然写着：
   `// See docs/future-strategy/phase1b_implementation_plan_gemini-3.7-flash.md §7.2`
   而经全局文件核查，**`docs/future-strategy/phase1b_implementation_plan_gemini-3.7-flash.md` 根本不存在于代码库中**！
   正因为 P11.4 的 `doc_refs_test.go` 明确排除了 `_eval`，导致这个断链引用完全绕过了守卫。

##### 优化解决方案
1. **在 `archtest` 中纳入 `_eval` 编译断言**：
   在 `internal/archtest` 中增加一个常驻测试用例，确保 `_eval` 目录下的所有 Go 工具在历次重构中始终保持可编译：
   ```go
   func TestArchitecture_EvalToolsCompile(t *testing.T) {
       cmd := exec.Command("go", "build", "-o", os.DevNull, "./_eval/calibrate_p1b.go")
       cmd.Dir = repoRootDir(t)
       if out, err := cmd.CombinedOutput(); err != nil {
           t.Fatalf("_eval/calibrate_p1b.go failed to compile: %v\n%s", err, out)
       }
   }
   ```
2. **修正 `_eval/calibrate_p1b.go` 的断链注释**：
   将第 10 行无效的文档引用修改为当前现存的权威架构文档（如 `story_report_architecture_opus-5.md`）。

---

### 第二梯队 (Tier 2: 严重程度中等 / ROI 中等)

#### 2.4 ActionPlan 事实核查纪律漂移与虚构行号修正

- **问题严重程度**：中
- **长期改进 ROI**：中（维护开发记录与 `KNOWN_ISSUES` 的严肃性与一致性）

##### 分析与说明
ActionPlan §0 记录的 `story.PreviewTitle` 删除主张以及多处函数行号（如 `summary.go` 虚构 105–113 行、`usage.go` 虚构 168–200 行）与真实代码库严重不符。
- **根因**：撰写 ActionPlan 时未能做到"每次开工基于真实最新仓库状态 grep/view"，而是依赖历史未提交草稿或模型训练记忆中的幻觉行号编造了详细步骤。
- **改进方案**：
  1. 在本次提交的 ActionPlan 总结与 [`docs/KNOWN_ISSUES_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md) §1.39 订正中，客观说明 `PreviewTitle` 在 P11 开工前已被清理的真实情况，删除对不存在代码的删除指令。
  2. 确立准则：任何 ActionPlan 声明删除具体函数时，必须以当前真实 commit 的行号或 AST 符号为准。

---

#### 2.5 战略文档状态标注的生命周期与归档策略缺失

- **问题严重程度**：中
- **长期改进 ROI**：中

##### 分析与说明
P11.5 在 [`vmr_future_strategy_v2_sonnet-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/vmr_future_strategy_v2_sonnet-5.md) 和 [`agent_runtime_implementation_roadmap_gemini-3.7-flash.md`](file:///Users/stanford/code/vmr/docs/future-strategy/agent_runtime_implementation_roadmap_gemini-3.7-flash.md) 头部添加了"CLI 示例已过期"的引用块标注。
- **不足之处**：
  这种方式属于被动打补丁。随着 P15（CLI 完全收敛）完成，文档中出现 `vmr report`/`vmr story` 与实际主入口 `vmr analyze` 的认知割裂会越来越大。
- **改进建议**：
  明确 `docs/future-strategy/` 下各文档的分类与生命周期：
  - **现行权威方案**（Active Authority）：仅保留 [`story_report_architecture_opus-5.md`](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md) 等极少数核心方案。
  - **历史评审/阶段总结**（Point-in-Time Reviews）：如各阶段 Review 报告，打上时间戳与归档标记，不再对其内部历史命令强求同步。
  - **候选路线图**（Future Proposals）：如 Agent Runtime 路线图，明确标注其前置条件与依赖版本。

---

### 第三梯队 (Tier 3: 严重程度低 / 长期收益与代码卫生改进)

#### 2.6 `health.Registry.Available` 的注释边界补全
- **问题严重程度**：低
- **说明**：
  `health.Registry.Available` 从待删列表中移出是完全正确的。但建议在其 Go doc comment 中进一步明确写明：
  `// Available returns whether the given target endpoint is currently usable without side effects (does not increment probe attempts or alter circuit breaker states). Intended for health probes, routing inspections, and non-mutating test assertions.`
  防止后续维护者因看到生产分派路径（`Acquire`）而再次将其误列入 deadcode 清理计划。

#### 2.7 实际代码变更与 ActionPlan 文件清单的微小差异对齐
- **问题严重程度**：低
- **说明**：
  实际 git staged diff 中包含了对 [`internal/chatmsg/messages.go`](file:///Users/stanford/code/vmr/internal/chatmsg/messages.go) 和 [`internal/i18n/lang.go`](file:///Users/stanford/code/vmr/internal/i18n/lang.go) 的微调，但在 ActionPlan §2 的任务清单中未显式罗列。建议在执行记录（Section 6）中补充这两个文件的调整说明，保持文档与提交的一致性。

---

## 3. 对后续 P12–P15 演进的关键设计建议与风险预警

P11 的核心定位是"清理与守卫先行"，为后续 P12–P15 打好地基。基于本次事实核查与架构审阅，对后续各阶段提出以下硬核建议：

```
┌──────────────────────────────────────────────────────────────────────────┐
│                   后续阶段演进关键路径与架构建议                            │
├──────────────────────────────────────────────────────────────────────────┤
│ P12 (渲染正确性) ──► 必须先落地页面指纹 (Fingerprint)，彻底替换 Stat 谓词     │
│       │                                                                  │
│       ▼                                                                  │
│ P13 (体积归位)   ──► 削减 93% 冗余 (Raw SSE 坐标化 + 增量历史折叠)            │
│       │              注意同步适配 WriteDetails 差分测试                      │
│       ▼                                                                  │
│ P14 (索引统一)   ──► 合并 cron/subagent 判定，首屏全面可点                     │
│       │                                                                  │
│       ▼                                                                  │
│ P15 (CLI收敛)    ──► 补齐 analyze 缺失模式，将 report/story 薄化为纯转发      │
└──────────────────────────────────────────────────────────────────────────┘
```

### 3.1 P12（渲染表示正确性）的核心关注点
- **跳过谓词从"假设"到"校验"**：
  必须在详单文件内注入格式指纹（如 `<!-- vmr-detail-fingerprint: lang=zh,evidence=true,ver=2 -->`）。`EnsureRendered` 必须先校验文件头部的指纹，若语言或模板版本不匹配立即触发原子重写。
- **B4 是 P13 的绝对硬前置**：
  如果 P12.1 的指纹校验未就绪，P13 对 `reqdetail.Render` 做出的任何内容裁剪（删除 Raw SSE、折叠历史消息），在已存在 `details/` 的本地测试环境中都将被旧文件命中跳过而**静默不生效**！

### 3.2 P13（证据层体积归位）的核心关注点
- **单条下钻 vs 批量渲染的物化分离**：
  默认 `vmr analyze` 在批量渲染所有 task 候选时，必须做到"只生成链接、不物化 300+ 份详单"。
- **守卫必须常驻化**：
  必须如 DevPlan 所要求，编写常驻自动化测试断言：在默认分析运行后，`details/` 目录下的生成文件数必须为 0（或严格等于显式指定的下钻数），彻底防止体积纪律第四次被绕过。

---

## 4. 总结与行动项建议

| 优先级 | 行动项 (Action Item) | 涉及文件 | 预期收益 |
| :---: | --- | --- | --- |
| **P0** | **升级 P11.4 注释扫描机制**：由全文件正则改为 `go/parser` 提取 AST 注释后校验 | `internal/archtest/doc_refs_test.go` | 消除 Go 字符串字面量假阳性，提升守卫精准度 |
| **P0** | **建立 `_eval` 编译守卫**：在 `archtest` 中补充对 `_eval/calibrate_p1b.go` 的编译测试 | `internal/archtest/doc_refs_test.go` | 封堵以 `_` 开头目录脱离 CI 守卫的盲区 |
| **P1** | **修复 `_eval` 内部断链**：清理 `calibrate_p1b.go:10` 指向不存在文档的过期引用 | `_eval/calibrate_p1b.go` | 消除代码库内残留的死引用 |
| **P1** | **订正 ActionPlan 与 KNOWN_ISSUES 记录**：如实记录 `PreviewTitle` 及小函数的真实基线状态，澄清保留函数的封装性质 | `docs/KNOWN_ISSUES_sonnet-5.md`, `story_report_p11_action_plan_sonnet-5.md` | 纠正事实错误，维持团队知识库客观性 |
| **P2** | **标注 `report.WriteDetails` 为测试专用**：补齐 Deprecated 注释，防范 P12/P13 重构时的维护陷阱 | `internal/report/detail.go` | 明确架构边界，消除维护误区 |


---

## 5. 当前 Uncommitted 修改与 ActionPlan 符合度核查及问题处置状态

### 5.1 工作区未提交修改全量核对表

对当前工作区的所有未提交变更（含 22 个暂存文件、4 个未暂存文件以及 3 个未跟踪文件）与 `story_report_p11_action_plan_sonnet-5.md` 设定的范围和实际执行结果进行逐项对照核查：

| 文件类别 / 路径 | 修改内容与性质 | 对应 ActionPlan 任务 | 符合度核查结果 |
| --- | --- | --- | :---: |
| `deleted: internal/ctxgraph/blobindex.go` | 删除废弃索引子系统（全文件 125 行） | P11.1 | **完全符合** |
| `modified: internal/ctxgraph/scan.go` | 移除 `Graph.Index` 字段及 `buildGraph` 填充循环 | P11.1 | **完全符合** |
| `modified: internal/ctxgraph/scan_test.go` | 移除 `TestScan_BlobIndexFetchAllRecoversOriginalContent` | P11.1 | **完全符合** |
| `modified: internal/ctxgraph/cache.go`, `manifest.go`, `records.go`, `reqcoord_test.go` | 清理指向 `BlobIndex.FetchAll` 的注释，更新为 `FetchRecords` | P11.1 | **完全符合** |
| `modified: internal/story/preview.go`, `preview_test.go` | 确认清理单数版 `PreviewTitle` 历史遗留，保留并强化复数版 `PreviewTitles` 测试 | P11.2 | **完全符合** |
| `modified: cmd/vmr/summary.go`, `main_test.go` | 移除死函数 `configFlag` 与 `providerProxyLines`，测试改为直测 `providerProxyEntries` | P11.2 | **完全符合** |
| `modified: internal/chatmsg/usage.go`, `usage_test.go` | 移除死函数 `ExtractFinish` 及其测试 | P11.2 | **完全符合** |
| `modified: internal/reqdetail/facts.go` | 移除单条包装死函数 `ErrorClass`，保留 `AttemptErrorClass` | P11.2 | **完全符合** |
| `modified: internal/reqdetail/evidence.go`, `evidence_test.go` | 移除死函数 `contentHash8`，测试对齐为 `SysPromptEvidenceFileName` | P11.2 | **完全符合** |
| `modified: internal/chatmsg/messages.go`, `internal/i18n/lang.go` | 修正已搬迁/废弃文件的注释引用 | P11.2 / P11.4 | **完全符合** |
| `modified: docs/future-strategy/vmr_future_strategy_v2_sonnet-5.md`, `agent_runtime_implementation_roadmap_gemini-3.7-flash.md` | 增加头部 CLI 过期标注（说明 `vmr report`/`vmr story` 已由 `vmr analyze` 替代） | P11.5 | **完全符合** |
| `modified: docs/KNOWN_ISSUES_sonnet-5.md` | 订正 §1.39、ROI 排期表及 §3.35 闭环条目 | §5 (收尾) | **完全符合** |
| `modified: internal/archtest/doc_refs_test.go` | 扩展注释路径引用守卫，并升级为 `goFileComments`（AST 解析纯注释） | P11.4 & Review 2.1 | **完全符合（超额优化）** |
| `modified: internal/health/health.go` | 明确 `Available` 的只读无副作用与测试断言语义 | P11.3 & Review 2.6 | **完全符合（超额优化）** |
| `modified: internal/report/detail.go` | 为 `WriteDetails` 标注明确的 Deprecated / 差分测试基准语义 | Review 2.2 | **完全符合（超额优化）** |
| `modified: _eval/calibrate_p1b.go` | 清理内部对不存在文档的断链注释 | Review 2.3 | **完全符合（超额优化）** |
| `untracked: internal/archtest/eval_build_test.go` | 新增 `TestArchitecture_EvalToolsCompile` 保证 `_eval/` 目录可编译 | Review 2.3 | **完全符合（超额优化）** |

---

### 5.2 Review 报告中所提关键问题的处置状态核验

核对本审阅报告（Section 2 / Section 4）中列出的所有问题在当前工作区代码中的处置结果：

1. **【问题 2.1 处置核实】`internal/archtest/doc_refs_test.go` 正则缺陷改造**：
   - **源码核验**（[`internal/archtest/doc_refs_test.go:282-305`](file:///Users/stanford/code/vmr/internal/archtest/doc_refs_test.go#L282)）：新增 `goFileComments(path)`，使用 `parser.ParseFile(fset, path, nil, parser.ParseComments)` 遍历 `ast.File.Comments` 提取纯注释文本，彻底隔离了代码字符串字面量、标识符与 import 语句。
   - **判定**：**已彻底闭环解决**。

2. **【问题 2.2 处置核实】`report.WriteDetails` 维护边界明确**：
   - **源码核验**（[`internal/report/detail.go:225-235`](file:///Users/stanford/code/vmr/internal/report/detail.go#L225)）：正式添加了 `// Deprecated: WriteDetails is retained only as the two-pass differential baseline TestBuildOnRecordMatchesWriteDetails (detail_test.go) diffs Build's single-pass onRecord hook against, byte-for-byte...`。
   - **判定**：**已彻底闭环解决**。

3. **【问题 2.3 处置核实】`_eval` 脚本编译守卫与断链清理**：
   - **源码核验**：
     - [`internal/archtest/eval_build_test.go`](file:///Users/stanford/code/vmr/internal/archtest/eval_build_test.go) 落地了常驻自动化测试 `TestArchitecture_EvalToolsCompile`。
     - [`_eval/calibrate_p1b.go:10`](file:///Users/stanford/code/vmr/_eval/calibrate_p1b.go#L10) 移除了失效的 `phase1b_implementation_plan_gemini-3.7-flash.md` 引用。
   - **判定**：**已彻底闭环解决**。

4. **【问题 2.6 处置核实】`health.Registry.Available` 只读语义与测试依据补齐**：
   - **源码核验**（[`internal/health/health.go:47-58`](file:///Users/stanford/code/vmr/internal/health/health.go#L47)）：详细补充了无副作用查询、不抢占半开探测槽位的说明，并交叉引用了 `KNOWN_ISSUES §3.35`。
   - **判定**：**已彻底闭环解决**。

---

### 5.3 最终结论与提交准备度

1. **范围完整性**：未提交修改严格覆盖了 ActionPlan P11.1–P11.5 的全部范围，且额外闭环了审阅中发现的所有高 ROI 隐患；
2. **测试与质量**：全仓 `go test ./...`（含 `-race` 竞态分析与 `internal/archtest` 架构守卫）100% 通过；`go vet ./...` 0 警告；
3. **零退化保证**：不改变任何默认日志分析与报表产物的字节内容。

**综合裁决：当前工作区所有未提交修改均已达标且问题均得到理想处置，无遗留缺陷，完全具备提交（Ready to Commit）条件。**
