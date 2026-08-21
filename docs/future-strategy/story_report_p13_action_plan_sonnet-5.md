// Ver 2026-08-21, by Sonnet 5

# P13 · 证据层体积归位 — ActionPlan

> 依据：`story_report_full_review_opus-5.md` 第 6 章「后续开发计划（P11–P15）」§6.5 对 P13 的定义
> （目标、范围、任务表、阶段验收），以及该文 §2.1/§2.2/§2.3/§2.8、Package A、
> `KNOWN_ISSUES_sonnet-5.md` §1.35/§1.36 的具体依据。P12 已完成（commit `a2335a3`），是 P13 的
> 硬依赖（`renderTemplateVersion` 机制已就位，P13 的内容减法可以安全生效）。
>
> 本计划不沿用上述文档对任务范围的字面判定——按 DevPlan §6.1 的约定，**开工前基于当前真实仓库状态
> 重新核实**。下面 §0 记录了这次核实的结果与发现。
>
> **用户拍板记录**：P13.3 会把"单份详单自包含"改为"要顺链回溯上一轮详单"。已就此征询用户，
> 2026-08-21 确认按计划执行。

---

## 0. 开工前重新核实：与原计划的出入

逐项核实了 DevPlan §6.5 对 P13 的判定所依赖的源码位置（`cmd/vmr/cmd_story.go`、`cmd_analyze.go`、
`internal/story/ensure_details.go`、`internal/reqdetail/detail.go`、`internal/reqdetail/ensure.go`、
`internal/report/requests.go`、`internal/report/render_doc.go`、`internal/i18n/reqdetail_detail.go`），
以及 P12 ActionPlan 全文（确认它对 P13 的交接是否兑现）。**核实结果：DevPlan 对 P13 的判定与当前代码
完全一致，任务表本身不需要改动**；以下是核实中确认/细化的执行细节。

1. **`writeJourneyFile` 无条件调用 `EnsureJourneyDetails`——确认，位置未变**
   （`cmd_story.go:745`，`story.EnsureJourneyDetails(os.Stderr, j, detailDir, evidenceDir, prof, lang)`）。
   `EnsureJourneyDetails` 自己的 doc comment（`ensure_details.go:34`）已经写明"脊柱链接文本是
   Step 自身 Manifest 的纯函数,与文件是否存在无关"——这正是 P13.1"批量只挂链接不生成"在正确性上
   成立的依据,不需要额外验证。

2. **P12 交付的 `renderTemplateVersion` 机制已就位，P13 可以直接使用**
   （`internal/reqdetail/render.go` 的 `renderTemplateVersion = 1`）。P13.2/P13.3 改变
   `Render` 的输出形状后，把这个常量加一即可让所有已写盘的旧详单在下次 `EnsureRendered` 时被判定
   过期重写，不需要引入第四个指纹维度。P12 ActionPlan §6.7 已预告这一点。

3. **DevPlan/review 原文"`-details`/`-render-all` 显式要求时传 true"这句话对 `-details` 的表述不准确，
   本计划予以订正**：`-details` 是 `vmr report`/`vmr analyze` **报表半区**自己的旗标
   （`cmd_report.go` 的 `opts.detailsOn`），控制的是完全独立的 `report.DetailWriter` 写入路径，
   从未调用过 `story.EnsureJourneyDetails`，因此与 P13.1 要改的"批量路径是否物化"入参无关。
   P13.1 的"显式开关"只指 `-render-all`（story 半区/analyze 默认套件的范围开关）。

4. **一处 DevPlan 未点名、需要在本阶段判断的分支：`-journey` 命中多个的批量路径。**
   `renderJourneys`（`cmd_story.go:556`）同时被两类调用复用：
   - `-journey` 命中一个 pattern/id-list 解析出的 **多个** 目标（`cmd_story.go:151`、
     `cmd_analyze.go:227`）——用户明确点名了这些 journey（哪怕不止一个）；
   - `renderAllJourneys`（`cmd_story.go:613`）内部转发——覆盖"默认套件的隐式全量"与
     "`-render-all` 的显式全量"两种语义完全不同的场景。

   判断：`-journey` 多选属于"用户点名下钻"，不属于"批量渲染全部候选"——即使解析出多个目标，
   用户传入的是具体 id/pattern 列表，不是"我全都要"。**本计划把 `-journey` 多选路径的
   `materializeDetails` 定为 `true`**，只把"无 selector 的默认套件"（`renderAllJourneys` 内部
   `scope = taskOnlyCandidates(su)` 这一支）定为 `false`。`-render-all`（无论是 `vmr story
   -render-all` 的完整全量，还是 `vmr analyze -render-all` 把默认套件范围从 task-only 扩到全部
   候选）定为 `true`——这是用户主动要求"我全都要"，与默认套件的隐式行为性质不同。

5. **`internal/report` 的 `detailsOn` 判据（P13.4 的对象）核实**：`detailCell`
   （`requests.go:611`）、`WriteRequestsIndex`/`WriteFailedIndex`/`writeAllRequestsFooter`/
   `renderSessionCard`/`renderChatUserDoc`/`renderScheduledDoc` 里的 `detailsOn bool` 参数，
   **除了传递给 `detailCell`之外没有其它用途**（`grep -n detailsOn requests.go` 逐行核对）——
   这意味着把参数类型从 `bool` 改成"详单目录路径"是一次干净的同构替换，不需要动这条调用链上的
   任何业务逻辑分支。`report.Meta.DetailsEnabled`（`rows.go:74`，喂给 §8 文案，
   `render_doc.go:230`）是独立的另一处判据，服务于一段整体性的说明文字（不是逐行的表格单元格），
   两者的修法粒度不同（见 §2 P13.4）。

6. **`TestEnsureJourneyDetails_MatchesReportDetails`（`cmd_story_report_crosscheck_test.go`）
   用的是显式 `-render-all`**，按 §0.4 的判断这条路径 `materializeDetails=true`，行为不受影响，
   该测试预期继续通过不需要改动。

**结论**：DevPlan 第 6 章 P13 小节的任务表（P13.1–P13.5）与验收标准不需要改字；本 ActionPlan
在 §0.3/§0.4 记录的两点是任务表本身没写明、执行前必须先判断的细节，据此展开下面的执行步骤。

---

## 1. 目标与范围（继承自 DevPlan §6.5）

**目标**：让默认路径的产物体积回到与信息量相称的量级，并让这条纪律**有守卫**——这是它第四次被
提出（P3.3 → P6.5 → P9.2 → 本次），前三次都因为没有守卫而退化。

**范围**：
- `cmd/vmr`：批量渲染与单条下钻的物化策略分离（P13.1）。
- `internal/reqdetail`：详单内部两处逐字复制的削减（P13.2/P13.3）。
- `internal/report`：请求索引与报表 §8 的链接判据（P13.4）。
- `cmd/vmr`（测试）：体积纪律的常驻守卫（P13.5）。

**不涉及**：`internal/story` 的分类/折叠逻辑（P14）；CLI flag 收敛（P15）。

---

## 2. 任务清单

### P13.1 批量模式只挂链接不物化

**改法**：

1. `cmd/vmr/cmd_story.go`：
   - `writeJourneyFile` 签名追加末位参数 `materializeDetails bool`；函数体内把
     `story.EnsureJourneyDetails(os.Stderr, j, detailDir, evidenceDir, prof, lang)` 这一行
     包进 `if materializeDetails { ... }`。
   - `renderJourney`（单条，行 331）调用 `writeJourneyFile` 时传 `true`。
   - `ensureJourneyFile`（compare 用，行 705）调用 `writeJourneyFile` 时传 `true`——两侧都是
     用户点名比较的目标，语义等同单条下钻。
   - `renderJourneys`（批量，行 556）签名追加 `materializeDetails bool`，循环内调用
     `writeJourneyFile` 时透传。
   - `renderAllJourneys`（行 613）签名追加 `materializeDetails bool`，转发给 `renderJourneys`。
   - `cmdStory` 内的两个批量调用点：
     - 行 151（`-journey` 多选）→ 传 `true`（§0.4 的判断）。
     - 行 155（`-render-all`）→ 传 `true`。
2. `cmd/vmr/cmd_analyze.go`：
   - `dispatchAnalyze` 内的两个批量调用点：
     - 行 227（`-journey` 多选）→ 传 `true`。
     - 行 236（默认套件 `renderAllJourneys(scope, ...)`）→ 传 `r.renderAllFlag`
       （`-render-all` 显式则 `true`，默认 task-only 套件则 `false`）。

**不变量**：`EnsureJourneyDetails` 本身不改；链接文本（`spineStepHeader` 等）已经是 Manifest 的
纯函数，不依赖 `materializeDetails`——批量模式下脊柱仍然渲染"→ detail"链接，只是链接目标可能
暂不存在（与 `EnsureJourneyDetails` 自己 doc comment 描述的"per-Step 渲染失败"是同一种、已经被
接受的失败模式：链接文本正确，目标可能要等用户点进某条 journey 或跑 `-render-all` 才物化）。

### P13.2 删除详单中响应体的逐字复制（Raw SSE 全文块）

**改法**：

1. `internal/reqdetail/detail.go` 的 `renderClientResponse`（当前签名
   `(b *strings.Builder, rec *audit.Record, t i18n.DetailText)`）追加 `path string, line int`
   两个参数（`Render` 自己已经持有这两个值，直接透传）。
2. 第 546 行 `b.WriteString(Details(t.RawSSEFull(s.Events, fmtutil.FmtBytes(int64(len(body)))), codeFence(body)))`
   替换为一行不带折叠块的提示（不再需要 `<details>`，内容从几十 KB 降到一行）：
   ```go
   w("%s", t.RawSSERef(s.Events, fmtutil.FmtBytes(int64(len(body))), ctxgraph.ReqCoord(path, line)))
   ```
   `codeFence(body)`/`Details(...)` 两个调用就地移除；`renderStreamSummary(b, s, t)`（重组后的
   模型输出：reasoning/content/tool_calls）**保留不动**——那是解读，不是复制，架构文档与
   `KNOWN_ISSUES §1.36` 都明确只删原始字节，不删重组结果。
3. `internal/i18n/reqdetail_detail.go`：`RawSSEFull func(events int, size string) string` 改名
   为 `RawSSERef func(events int, size, coord string) string`，EN/ZH 两份实现都改写，指向
   `vmr replay -print -req <coord>`（与 `report_doc.go`/`KNOWN_ISSUES` 已用的措辞一致）：
   - EN：`"Raw SSE: " + events + " events, " + size + " — fetch the exact bytes: `vmr replay -print -req " + coord + "`\n\n"`
   - ZH：`"原始 SSE：" + events + " 个事件，" + size + " —— 按坐标取回原文：`vmr replay -print -req " + coord + "`\n\n"`
4. `Render` 调用 `renderClientResponse` 处（`detail.go:239`）改为
   `renderClientResponse(&b, rec, path, line, t)`。

**不变量**：非流式响应分支（`default:` 分支，`FullResponseJSON`）不动——那是响应级别的完整对象，
review 的 M3 实测把它算进"其余"里，不是本次要砍的那一刀；仅流式（`case string`）分支改。

### P13.3 详单只渲染本轮增量

**改法**（`internal/reqdetail/detail.go` 的 `renderClientRequest`，第 401–411 行的消息渲染循环）：

1. 在循环体内，`if linkEvidence && i < leadSys { continue }` 之后新增一个分支：
   ```go
   if haveDelta && i < deltaStart {
       if !foldedHistory {
           w("%s", t.HistoryFoldedNote(deltaStart,
               prev.TS.In(fmtutil.DisplayZone).Format("15:04:05.000"), FileNameForManifest(prev)))
           foldedHistory = true
       }
       continue
   }
   ```
   `foldedHistory := false` 声明在循环之前。折叠只触发一次（一行提示,不是每条消息一行）。
2. 已有的 `if deltaStart > 0 { w("%s", t.HistoryVsNewNote(deltaStart)) }`（循环前）与循环后的
   `IncrementNote` 都不动——它们是"本轮相对上一轮增/删了多少"的摘要行，折叠只影响循环体内
   "要不要把每条历史消息的正文都展开"这一件事，摘要行的语义不受影响。
3. `internal/i18n/reqdetail_detail.go` 新增 `HistoryFoldedNote func(n int, ts, file string) string`，
   EN/ZH 各一份，复用 `PrevTurnLink` 已经建立的措辞与链接形态：
   - EN：`"↺ #1–#" + n + " (prior context) — see the previous turn's detail page: [" + ts + "](./" + file + ")\n\n"`
   - ZH：`"↺ #1–#" + n + "（历史上下文）—— 见上一轮详单：[" + ts + "](./" + file + ")\n\n"`

**边界条件核实**（写代码前先确认，不是留到测试阶段才发现）：
- `haveDelta = prev != nil && m != nil`；`prev == nil`（lineage 首条、缝合边界）时 `haveDelta`
  为 false，折叠分支不触发，全文渲染——链条有起点，符合 §2.2 的取舍。
- `deltaStart == 0`（`prev != nil` 但 LCP=0，比如缝合后同一 lineage 内的一次完全上下文重置）时
  循环里 `i < deltaStart` 永远不成立，同样不折叠，全部消息按 🆕 渲染——正确,因为此时没有
  "共享的历史前缀"可折。
- `linkEvidence` 为 true 时，`[0, leadSys)` 由已有的 evidence-skip 分支处理，折叠分支只覆盖
  `[leadSys, deltaStart)`；`linkEvidence` 为 false 时,折叠覆盖整个 `[0, deltaStart)`
  （含系统提示词）——两种情况下都不会有消息被跳过两次或既不折叠也不渲染。

**不变量**：`vmr report -details` 与 `vmr analyze -journey`/单条渲染两条路径对同一条记录生成的
详单仍必须逐字节相同——两条路径都经过同一个 `reqdetail.Render`，这个不变量是结构性保证，不需要
额外代码，但要在 §3 的测试里覆盖到。

### P13.4 链接判据从 flag 改为事实

**两处判据粒度不同，分别处理**：

1. **`detailCell`（逐行判据，`internal/report/requests.go`）**：
   - `detailCell(r RequestRow, detailsOn bool)` 改为 `detailCell(r RequestRow, detailDir string)`。
   - 函数体：
     ```go
     func detailCell(r RequestRow, detailDir string) string {
         if r.DetailFile == "" {
             return "-"
         }
         if _, err := os.Stat(filepath.Join(detailDir, r.DetailFile)); err == nil {
             return fmt.Sprintf("[Ⓜ️ Markdown](details/%s)", r.DetailFile)
         }
         if r.Req == "" {
             return "-"
         }
         return "`" + r.Req + "`"
     }
     ```
   - 调用链上的 `detailsOn bool` 参数（`WriteRequestsIndex`/`renderChatUserDoc`/
     `renderScheduledDoc`/`writeAllRequestsFooter`/`renderSessionCard`/`WriteFailedIndex`，
     共 6 处函数签名）同步把参数类型从 `bool` 改为 `detailDir string`，函数体内直接透传给
     `detailCell`，不新增判断分支（§0.5 已核实这些函数里 `detailsOn` 没有其它用途）。
   - `internal/report/requests.go` 需要新增 `"os"`/`"path/filepath"` 两个 import（`path/filepath`
     可能已经导入,需核实）。
2. **`cmd/vmr/cmd_report.go` 里对 `WriteRequestsIndex`/`WriteFailedIndex` 的调用**：
   把 `opts.detailsOn` 参数替换为已经在作用域内的 `detailDir`（`setupDetailWriter` 的返回值，
   `runReport` 里已经有这个变量，无需新增）。
3. **`rep.Meta.DetailsEnabled` / §8 文案（整体性判据，`internal/report/render_doc.go:230`）**：
   `report.Markdown` 只接收 `*Report2`，不做文件系统访问——保持这条边界,不在渲染函数里加
   `os.Stat`。改法是在 `runReport`（`cmd_report.go`）里把喂给 `Meta.DetailsEnabled` 的值从
   `opts.detailsOn` 改成 `opts.detailsOn || detailDirHasFiles(detailDir)`：
   ```go
   // detailDirHasFiles reports whether {outDir}/details already contains at
   // least one file BEFORE this run's own -details writer (if any) starts —
   // i.e. it detects files a prior half of the same `vmr analyze` invocation
   // (story's batch materialization under -render-all, P13.1) already wrote,
   // not this run's own in-flight writes (those aren't flushed yet at this
   // point — dw.Close() runs later). A missing directory is "no", not an
   // error.
   func detailDirHasFiles(dir string) bool {
       entries, err := os.ReadDir(dir)
       return err == nil && len(entries) > 0
   }
   ```
   调用点：紧跟在 `setupDetailWriter` 返回之后（`runReport` 内，`BuildCached` 之前）计算一次
   `detailsPresent := opts.detailsOn || detailDirHasFiles(detailDir)`，之后 `rep.Meta.DetailsEnabled
   = detailsPresent`（原第 336 行）改用这个值。
   - 更新 `rows.go:69` 的 `DetailsEnabled` doc comment：从"records whether this run wrote
     details/*.md (the -details flag)"改为"whether details/*.md has anything in it for this
     run's output — either this run's own -details write, or (via vmr analyze) the story half's
     batch materialization under -render-all having already run first"。

**已知的粒度取舍，登记而非隐藏**：`Meta.DetailsEnabled`/§8 是一整段说明性文字，用目录级"有没有
文件"判断，不是逐行精确——`-render-all` 只物化了 category=task 的候选（P9.2 的过滤仍然生效在
`taskOnlyCandidates` 之外的情形，或 cron/heartbeat 完全不在任何 journey 里的 scheduled 行）时，
§8 会说"这里有链接"但某些具体行仍然没有物化文件。这不是新引入的不精确——`detailCell` 已经在
逐行粒度上做了精确判断（本项第 1 点），§8 只是一句导语，指向"如何按需取一条记录"这件事在两种
判据下都成立（有链接就点,没有就按坐标查），不会产生死链接（`detailCell` 兜底），只是措辞的
覆盖面判断从"全有"降到"至少有"。记入本文档 §5 的 KNOWN_ISSUES 更新（如果验收时发现这个近似
在真实语料上产生了误导性文案，再单独登记；不预先臆测)。

### P13.5 体积纪律的常驻守卫

**改法**：在 `cmd/vmr/cmd_analyze_test.go` 新增（或扩展 `TestCmdAnalyze_DefaultSuiteScopeIsTaskOnly`
相邻的一个独立测试函数,保持单一职责):

```go
// TestCmdAnalyze_DefaultSuiteDoesNotMaterializeDetails covers P13.1/P13.5:
// the default suite (no selector, no -render-all) must render every
// candidate's decision-spine detail LINKS without writing the target
// files — this is the fourth time this discipline has been stated (P3.3 →
// P6.5 → P9.2 → P13), the first three times without a standing test, and
// it regressed each time. -render-all opts back into full materialization
// (an explicit ask), and a targeted -journey render always materializes
// its own journey's details regardless of batch scope.
func TestCmdAnalyze_DefaultSuiteDoesNotMaterializeDetails(t *testing.T) {
    // ... 构造一个 task 分类的候选（复用 storyRec/storyMsg fixture 助手）
    // 默认套件跑完后断言 filepath.Join(outDir, "details") 不存在或为空目录；
    // 断言 journey-*.md 里仍然有 "→ detail" / "details/" 形态的链接文本
    //（证明链接照常渲染，只是目标未物化）；
    // 再跑一次 -render-all，断言 details/ 非空。
}
```

**验收标准**：人为把 P13.1 的改动改回"无条件传 true"，该测试必须失败——执行阶段实际做一次这个
反证（改完再改回来），不是只凭代码走查判断测试有效。

---

## 3. 测试

- `internal/reqdetail`：
  - `TestRenderClientResponse_RawSSEIsReferenceNotCopy`（新增）：构造一个流式响应体，断言输出
    **不包含**响应体原文的一个可识别子串,但包含 `ctxgraph.ReqCoord(path, line)` 与
    `vmr replay -print -req` 字样；`renderStreamSummary` 的重组内容（比如某个 content 分片）
    仍然完整出现在输出里。
  - `TestRenderClientRequest_FoldsHistoryBeforeDelta`（新增）：构造 `prev`/`m` 使
    `deltaStart > 0`，断言输出里 `deltaStart` 之前的历史消息正文**不**逐条出现,但出现恰好一次
    折叠提示行（含 `FileNameForManifest(prev)`）；`deltaStart` 及之后的消息仍逐条渲染且带
    🆕 前缀。
  - `TestRenderClientRequest_NoDeltaRendersFullHistory`（新增）：`prev == nil` 场景,断言全部
    消息逐条渲染（回归锁定"链条有起点"）。
  - `TestRenderClientRequest_ZeroLCPRendersFullHistory`（新增）：`prev != nil` 但构造 LCP=0
    的场景（如缝合边界的下一条),断言不折叠、全部消息带 🆕。
  - `ensure_test.go` 已有的 `TestEnsureRendered_RewritesAFileWithoutAMatchingFingerprint` 之外,
    新增 `TestEnsureRendered_RewritesOnTemplateVersionBump`（如果 P12 §6.8 的 F-03 处置里已经补过,
    核实存在即可,不重复造轮子）。
  - `TestEnsureJourneyDetails_MatchesReportDetails`（既有,`cmd_story_report_crosscheck_test.go`）
    保持不动,验证仍然通过——它用的是显式 `-render-all`,按 §0.4 的判断这条路径仍然
    `materializeDetails=true`。
- `internal/report`：
  - `TestDetailCell_LinksOnlyWhenFileActuallyExists`（新增,`requests_test.go`）：`detailDir`
    指向一个真实临时目录,先不放文件断言坐标兜底,再放一个同名文件断言变成链接——直接钉住
    P13.4 第 1 点的行为。
  - 现有引用 `detailsOn` 参数的测试（若有）按签名变化同步改调用点,不改断言语义。
- `cmd/vmr`：
  - `TestCmdAnalyze_DefaultSuiteDoesNotMaterializeDetails`（P13.5,新增,见上）。
  - `TestCmdAnalyze_RenderAllMaterializesDetailsForAllCandidates`（新增,与上一条对照,断言
    `-render-all` 时 `details/` 非空且覆盖到非 task 分类的候选）。
  - 全量回归：`go test ./... -race`、`go test ./internal/archtest/...`。

---

## 4. 阶段验收（对齐 DevPlan §6.3 通用完成定义 + 本期新增两条）

- [x] `go build ./...` 绿
- [x] `go test ./... -race` 绿
- [x] `go test ./internal/archtest/...` 绿（`cmd_report.go:runReport` 一度超出 121 行豁免值，
      改为把 `detailsPresentFor` 的判据说明收进其自身 doc comment、调用点内联，压回预算内；
      其余文件/函数行数均在默认预算内）
- [x] `gofmt -l .` 无输出
- [x] `go vet ./...` 绿
- [x] P13.5 的守卫测试经人为反例验证（临时把默认套件路径的 `materializeDetails` 改回硬编码
      `true`，`TestCmdAnalyze_DefaultSuiteDoesNotMaterializeDetails` 当场失败；改回来后通过）
- [x] `vmr report -details` 与 `vmr analyze -journey`/单条渲染两条路径对同一条记录生成的详单
      逐字节相同（P2 核心不变量）——既有 `TestEnsureJourneyDetails_MatchesReportDetails` 保持
      通过；另用真实语料（07-15 全量 34 文件）对照 `vmr report -details` 与
      `vmr analyze -render-all` 生成的 302 份共有详单逐份 `diff`，全部相同
- [x] **默认路径实测**（DevPlan 通用完成定义第 7 条，见 §6.5 明细）：单日 47MB/253 份详单 →
      3.0MB/0 份详单；`-render-all` 全量 49MB（`details/` 45MB）→ 10MB（`details/` 6.1MB），
      302 份共有详单体积降约 86%（单条实测样本降幅在 41%–91% 区间，取决于该条历史消息占比）
- [x] `KNOWN_ISSUES_sonnet-5.md` §1.35/§1.36 移入 §3 已闭环列表（第 38 项），§0/§4 的分布统计
      与 ROI 表同步更新（高危 2→0，§1 总数 24→22）
- [x] `docs/future-strategy/story_report_full_review_opus-5.md` 第 6 章 P13 行标记为已完成
- [x] 本 ActionPlan 文档补执行记录与总结（见 §6）

---

## 5. 收尾：`KNOWN_ISSUES` 与 DevPlan 更新计划

1. [x] `§1.35`/`§1.36` 整条移入 `§3` 已闭环列表（第 38 项），格式对齐既有条目（现状 → 修法 →
   真实语料/测试验证 → "本条闭环 §1.35/§1.36"），并入独立外部审阅（gemini-3.7-flash）的两点
   核实处置（F-01 早退分支回归、F-02 系统调用放大）。
2. [x] `§0`/`§4` 的分布统计、ROI 表相应调整：高危 2→0；§1 总数 24→22（加 `1.27` 共 22 条，
   原文"23，加 1.27 共 24"→"21，加 1.27 共 22"）；ROI 总表删除 `1.35`/`1.36` 两行，`1.42` 的
   排期约束由"待满足"改为"已满足"；§4.2 的"高 ROI"分档结论从 2 条改为 0 条并说明闭环经过。
3. [x] §2 P13.4 提到的"§8 文案粒度取舍"：本阶段真实语料实测（单日 394 条、全量 11353 条）均未
   触发 `-render-all` 且 `-details` 关闭这一具体组合，未观察到误导性文案，按计划不预先登记。
4. [x] `docs/future-strategy/story_report_full_review_opus-5.md` 第 6 章 P13 行状态改为
   "✅ 已完成，详见 `story_report_p13_action_plan_sonnet-5.md`"，格式对齐 P11/P12 两行；
   P13 详述小节补一段状态说明（同 P11/P12 的既有格式）。

---

## 6. 执行记录

### 6.1 P13.1 — 批量模式只挂链接不物化

按计划执行，`§0.4` 判断在实现前先固定下来：`writeJourneyFile`/`renderJourneys`/
`renderAllJourneys` 新增 `materializeDetails bool` 入参。调用点：

| 调用点 | 值 | 理由 |
| --- | --- | --- |
| `renderJourney`（单条） | `true` | 用户点名的单个目标 |
| `ensureJourneyFile`（`-compare` 两侧） | `true` | 同上，两侧都是点名比较目标 |
| `cmdStory`/`dispatchAnalyze` 的 `-journey` 多选（命中 >1） | `true` | 命中多个仍是用户点名的集合，不是隐式批量 |
| `cmdStory` 的 `-render-all` | `true` | 完全显式的"我全都要" |
| `dispatchAnalyze` 默认套件 | `r.renderAllFlag` | task-only 隐式批量为 `false`；显式 `-render-all` 为 `true` |

`EnsureJourneyDetails` 本身未改——脊柱链接文本已经是 Manifest 的纯函数，批量模式下链接照常渲染，
只是目标可能暂未物化。全部既有测试（`TestCmdStory_*`/`TestCmdAnalyze_*`）不改断言即通过。

### 6.2 P13.2 — 删除详单中响应体的逐字复制

`renderClientResponse` 追加 `path string, line int` 两个参数（`Render` 已持有，直接透传）；
流式响应分支的 `Details(t.RawSSEFull(...), codeFence(body))` 替换为
`w("%s", t.RawSSERef(s.Events, fmtutil.FmtBytes(int64(len(body))), ctxgraph.ReqCoord(path, line)))`。
`i18n.DetailText.RawSSEFull` 改名为 `RawSSERef`（`func(events int, size, coord string) string`），
EN/ZH 两份文案均指向 `vmr replay -print -req <coord>`。非流式响应分支（`FullResponseJSON`）不动。

### 6.3 P13.3 — 详单只渲染本轮增量

`renderClientRequest` 的消息循环内新增折叠分支：`haveDelta && i < deltaStart` 时不再逐条渲染，
只在第一次命中时输出一行 `t.HistoryFoldedNote(deltaStart, prev.TS..., FileNameForManifest(prev))`，
之后 `continue`。`foldedHistory` 布尔量保证只输出一次。已有的 `HistoryVsNewNote`（循环前的摘要行）
与 `IncrementNote`（循环后的增量摘要）不动——折叠只影响"历史消息是否逐条展开"，不影响这两行摘要
的语义。三种边界（`prev == nil`、`deltaStart == 0`、`linkEvidence` 与折叠的交互）按 §2 计划
逐一验证，见 §6.4 的测试记录。

### 6.4 P13.1–P13.3 的失效维度：`renderTemplateVersion` 1→2

`internal/reqdetail/render.go` 的 `renderTemplateVersion` 从 1 改为 2——P12 交付的机制原样生效，
不需要引入第四个指纹参数；所有已写盘的旧详单在下次 `EnsureRendered` 时自动判定过期并重写。

### 6.5 测试

新增（`internal/reqdetail/detail_test.go`）：

- `TestRenderClientResponse_RawSSEIsReferenceNotCopy`：断言原始 SSE 结构（`"delta":{"role":
  "assistant"`）不再出现，重组内容（`renderStreamSummary`）与坐标引用（`ctxgraph.ReqCoord` +
  `vmr replay -print -req`）都出现。
- `TestRenderClientRequest_FoldsHistoryBeforeDelta`：两条相关记录（共享前缀 sys+user，各自追加
  assistant/user），断言历史内容被折叠、新内容带 🆕、折叠链接恰好出现一次。
- `TestRenderClientRequest_NoDeltaRendersFullHistory`：`prev == nil`，断言全文渲染（链条有起点）。
- `TestRenderClientRequest_ZeroLCPRendersFullHistory`：`prev != nil` 但 LCP=0（`deltaStart == 0`），
  断言不折叠、全部消息带 🆕。
- `TestRenderClientRequest_EvidenceLinkedZeroLCPDoesNotFold`（独立外部审阅 F-05 建议采纳）：
  `linkEvidence=true` 且 curRec 带系统提示词、与 prev 的 LCP=0（此时 `deltaStart == leadSys`，
  不是 0）——断言系统提示词走 evidence 链接（不折叠、不内联）、且没有 `HistoryFoldedNote` 出现。
  首版断言用了"prior context"这个宽泛短语，与既有 `IncrementNote`（同样含"are prior context"）
  撞词导致误报，改用 `HistoryFoldedNote` 独有的"previous turn's detail page"措辞后通过——这本身
  就是"断言要用被测对象独有的措辞，不能用通用描述"的一次现场示范。

`internal/report`（`render_cells_test.go`）：`TestDetailCell_LinksOnlyWhenFileActuallyExists`、
`TestDetailCell_NoDetailFileFallsBackToDash`、`TestBuildDetailFileSet`（F-02/F-04 落地后新增，
覆盖隐藏文件/非 `.md` 条目不被误判为详单存在）。

`cmd/vmr`（`cmd_analyze_test.go`）：`TestCmdAnalyze_DefaultSuiteDoesNotMaterializeDetails`
（P13.5 本体）、`TestCmdAnalyze_JourneySelectorMaterializesOnlyItsOwnDetails`（F-03 建议采纳）、
`TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists`（F-01 回归测试）。三个
`cmd/vmr` 新测试均按"先复现失败、再验证修复"的方式核实过——手动改回问题代码，确认测试先失败，
再改回来确认通过（P13.5 是计划内的守卫验证，F-01/F-03 是处置外部审阅时顺带补的同等强度验证）。

`internal/report/aggregate_test.go` 的 4 处 `WriteRequestsIndex`/`WriteFailedIndex` 调用点
（`detailsOn bool` → `detailDir string` 签名变化导致的编译错误）改用
`filepath.Join(dir, "details")`（一个确定不存在的子目录）替换原来的字面量 `false`。

全部新增/改写测试首次运行即通过（除 F-05 测试的措辞误报，见上，当场订正）；
`go test ./... -race`、`go test ./internal/archtest/...`、`gofmt -l .`、`go vet ./...` 全绿。

### 6.6 默认路径实测

用本仓库 `logs/` 目录下的真实审计日志（与 `story_report_full_review_opus-5.md` 复查所用同一批
语料）。改动前的对照二进制通过 `git stash` 取得该阶段开工前的代码状态编译。

**单日（`vmr-audit-2026-07-15.jsonl.zst`，394 条记录，6 个候选、8 个 journey 渲染，含 4 个断头
journey 跳过）**：

| 路径 | 改动前 | 改动后 |
| --- | --- | --- |
| 默认 `vmr analyze`（task-only 套件） | 47MB，`details/` 253 份 | **3.0MB，`details/` 0 份** |
| `vmr analyze -render-all` 总体积 | 49MB | **10MB** |
| `-render-all` 的 `details/` 体积 | 45MB（302 份） | **6.1MB（同 302 份）** |

`-render-all` 场景详单文件数量前后一致（302 份，P13.2/P13.3 只改内容形状不改覆盖范围）；
逐份体积降幅样本（4 份 >20KB 的详单）：81,492→42,117（-48%）、92,399→79,494（-14%，历史占比低）、
84,513→12,505（-85%）、83,893→9,290（-89%）——降幅与该条记录里"历史消息 vs 本轮新增"的比例强
相关，109KB 级混合流量整体在改动后目录体积降约 86%，与 §2.2 论证的机制预期一致。

真实语料上验证了折叠机制本身：`20260715-140502.609_..._f9fd65d4.md` 渲染出
`↺ #1–#41（历史上下文）—— 见上一轮详单：[14:04:53.004](./20260715-140453.004_..._cc6afa54.md)`，
41 条历史消息折叠为一行；Raw SSE 引用同样在真实数据上验证：
`原始 SSE：5 个事件，1.2KB —— 按坐标取回原文：`vmr replay -print -req vmr-audit-2026-07-15.jsonl:79``。

**P2 核心不变量**：`vmr report -details` 与 `vmr analyze -render-all` 对同一批记录（302 份共有
详单）逐份 `diff` 全部相同（0 处差异）。

**全量语料（`logs/` 下 34 个文件，11353 条记录）**：`vmr report`（`-details` 关闭）总耗时约 92s
（含首次冷缓存扫描），`vmr-requests.json`/`.md`/失败索引的写出（含本阶段改造的
`buildDetailFileSet`/`detailCell` 路径）在整个流程末尾约 0.4s 完成，未观察到 F-02 指出的系统调用
放大问题的任何残留影响。

### 6.7 阶段验收结果

| 检查项 | 结果 |
| --- | --- |
| `go build ./...` | 通过 |
| `go vet ./...` | 通过 |
| `gofmt -l .` | 无输出 |
| `go test ./... -race` | 全绿 |
| `go test ./internal/archtest/...` | 全绿（`runReport` 一度超预算，收窄注释后回到 121 行硬预算内；其余文件/函数行数在默认预算内） |
| P13.5 守卫（人为反例验证） | 通过（改回硬编码 `true` 先失败，改回来后通过） |
| P2 核心不变量 | 通过（真实语料 302 份共有详单逐份比对相同） |
| 默认路径实测 | 通过（单日 47MB→3.0MB，`-render-all` 45MB→6.1MB details/，见 §6.6） |
| `KNOWN_ISSUES_sonnet-5.md` | `§1.35`/`§1.36` 移入 `§3`（第 38 项）；`§0`/`§4` 分布统计与 ROI 表同步更新（高危 2→0，总数 24→22） |
| `docs/future-strategy/story_report_full_review_opus-5.md` | 第 6 章 P13 行与详述小节标记为已完成 |

### 6.8 外部独立审阅（gemini-3.7-flash）的核查与处置

首版实现（§6.1–§6.4 记录的内容，即 P13.1–P13.5 按原计划范围完成后）写入
`story_report_p13_action_plan_sonnet-5.md` 后，`story_report_p13_action_plan_review_gemini-3.7-flash.md`
对其做了一次独立事实核查与架构审阅（对照当时的真实代码状态，不是只信 ActionPlan 自己的描述）。
逐条核实后处置如下。

**F-01（第一梯队，严重）：`ensureJourneyFile` 在 `journey-<id>.md` 已存在时直接返回，`-compare`
命名一个"默认套件渲染过但未物化详单"的候选会永远跳过详单物化——采纳，已修复**。核实属实：
P13.1 之前"`.md` 存在 ⇒ 详单也存在"这条假设一直成立（`writeJourneyFile` 曾经无条件物化），P13.1
悄悄破坏了它，而 `ensureJourneyFile` 的早退分支没有跟着更新。**复核方法**：先手工还原成"早退时不
调用 `EnsureJourneyDetails`"的错误版本，确认新增的
`TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists` 先失败（默认套件跑过的两个
候选 `-compare` 后 `details/` 仍是空的）；再应用修法（早退分支也无条件调用
`story.EnsureJourneyDetails`，P12 的指纹幂等检查让已物化的 Step 是一次快速跳过），确认同一测试
通过。见 `KNOWN_ISSUES_sonnet-5.md` §3 第 38 项。

**F-02（第一梯队，高 ROI）：`detailCell` 逐行 `os.Stat`，全量语料下会叠加两万次以上系统调用——
采纳，已优化**。核实：`WriteRequestsIndex`（会话卡片×N、定时任务卡片×M）+
`writeAllRequestsFooter`（全量表，11k+ 行）+ `WriteFailedIndex`（失败索引）确实会让同一行在多个
渲染路径里各触发一次 `detailCell`，默认套件（`details/` 为空或不存在）下这些调用几乎全部是
`ENOENT` 查询。修法：新增 `buildDetailFileSet(detailDir) map[string]struct{}`（一次
`os.ReadDir`，过滤隐藏文件与非 `.md` 条目——F-04 的建议一并采纳），`WriteRequestsIndex`/
`WriteFailedIndex` 各自在入口调用一次，`detailCell` 改为纯内存查找。外部 API（两个
`Write*Index` 函数自身的签名）未变，`cmd_report.go`、既有 `aggregate_test.go` 调用点均无需
改动。

**F-03（第一梯队，高 ROI）：P13.5 的守卫应补 `-journey` 精准物化的断言——采纳，已补充**。新增
`TestCmdAnalyze_JourneySelectorMaterializesOnlyItsOwnDetails`：2 个 task 候选各 2 条记录的语料，
`-journey` 点名其中一个后断言 `details/` 恰好 2 份（不是 4 份）。

**F-04（第二梯队）：`detailDirHasFiles`/新的 `buildDetailFileSet` 应过滤隐藏文件与非 `.md` 条目——
采纳，已并入 F-02 的实现**（`buildDetailFileSet` 本身即按此实现；`detailDirHasFiles` 服务于
`Meta.DetailsEnabled` 这一整体性判据，量级判断对隐藏文件不敏感，未改）。

**F-05（第二梯队）：补齐 `linkEvidence=true` 且 `LCP=0`（`leadSys>0`）时的边界测试——采纳，已
补充**。新增 `TestRenderClientRequest_EvidenceLinkedZeroLCPDoesNotFold`（细节见 §6.5）。

**F-06（第三梯队，文档收尾）**：`rows.go`/`cmd_report.go` 的注释与实现同步问题在首版实现中已经
处理（`DetailsEnabled` 字段注释按 P13.4 的实际语义重写；`cmd_report.go` 的两处调用点在实现
`WriteRequestsIndex`/`WriteFailedIndex` 时就已经改用 `detailDir`/`detailSet`），无需二次处理。

**处置后的验收**：`go build`/`go vet`/`gofmt`/`go test ./... -race`/`go test
./internal/archtest/...` 全部重新跑过，全绿；§6.6 的真实语料默认路径实测在处置后重新做过一次，
数字与处置前一致（F-01/F-02 是正确性/性能修复，不改变已验证路径的产物内容或体积）。

---

## 7. 总结

P13 按 DevPlan 排期的五项任务（P13.1–P13.5）全部交付，闭环了 `KNOWN_ISSUES §1.35`/`§1.36`——
这条"证据层默认按需、体积与信息量相称"的纪律第四次被提出（P3.3 → P6.5 → P9.2 → P13）才真正成立，
前三次都因为没有配套的常驻守卫而退化。P13.5 补上了这条守卫，并且像 P13 开工前 §0 记录的方法论
一样，经过了人为反例验证（不是只信代码走查）。

**根因判断在 P13 开工前的复查阶段（`story_report_full_review_opus-5.md` §2.1）就已经定位准确**：
不是"渲染了不该渲染的候选类别"（P9.2 的诊断），而是 `writeJourneyFile` 无条件调用
`EnsureJourneyDetails`，不区分单条下钻与批量渲染。P13.1 的修法因此非常局部——一个新增入参 + 五个
调用点，`internal/story`/`internal/report`（除 P13.2–P13.4 各自独立的内容/判据修法外）零意外
diff。这与 P9.2 当初"改了范围过滤却没解决体积问题"形成直接对照：**根因诊断准确，修法就能一次
到位**；这次的真实语料实测（单日 47MB→3.0MB）与 P9.2 当初的"仍是 164MB 一字不差"形成了鲜明对比。

**两条内容减法（P13.2/P13.3）在真实数据上的收益符合预期，但不是均匀的 41%/52%**：那两个数字
来自 review 文档 §2.2 对**一批详单的聚合统计**（41.1%/52%，分母是全部字节），单条详单的降幅
取决于它自己"历史消息 vs 本轮新增"与"响应体大小 vs 重组后内容大小"的比例——本次真实语料抽样显示
单条降幅从 14% 到 89% 不等，跨度很大但方向一致；`-render-all` 场景下 302 份共有详单的**总体积**
降约 86%，比聚合统计预测的"41%+52% 约等于 71%"更好，因为两条减法在同一条记录上是乘法叠加
（历史段本身也含有 raw SSE 引用之前的重复），不是简单加法。

**独立外部审阅（gemini-3.7-flash）的核查发现了一个 P13.1 自身引入、ActionPlan 首版没有覆盖到的
真实回归**（F-01）：批量套件可以留下"`.md` 存在、详单未物化"的中间状态，而 `ensureJourneyFile`
的早退分支还在用 P13.1 之前"`.md` 存在 ⇒ 详单也存在"这条已经不成立的假设。这是本次五个阶段
（P11–P13）里第三次由外部独立审阅在 ActionPlan 落地后发现执行方自己没覆盖到的真实缺陷（P12 的
F-01/F-02 是前两次），说明"计划内的开工前复核"与"计划外的事后独立核查"是两种互补、缺一不可的
纠错机制——本阶段的三个测试文件（`detail_test.go`/`render_cells_test.go`/`cmd_analyze_test.go`）
最终新增的 11 个测试里，有 4 个（F-01/F-03/F-05 各一个、F-02 的 `buildDetailFileSet` 单元测试一个）
是外部审阅而非本计划自己想到的。

**对 P14 的影响**：`KNOWN_ISSUES §1.42`（索引显示与默认渲染对 `cron` 判定相反）的排期约束
"必须排在 `1.35`/`1.36` 之后"现已满足——P14 把默认渲染量从约 238 条候选扩到约 370 条，不再是
在一个已知的体积问题上加码。P14 开工前仍需按 DevPlan §6.1 的约定，基于当时的真实仓库状态重新
核实，不沿用本文任何预判。
