// Ver 2026-08-20 15:00, by Sonnet 5

# vmr 日志分析体系重构 — P5 ActionPlan（人读层瘦身）

## 0. 定位

本文是 `docs/future-strategy/story_report_dev_plan_opus-5.md` 里 **P5 阶段**的执行级细化，基于
本仓库 P5 起点（P4 已完成，commit `eb45238`）的真实代码状态编写。架构依据见
`docs/future-strategy/story_report_architecture_opus-5.md` 的 §7.6(a)/§7.6(b)（detail/证据层的
共用形态）、§7.4(b)（内联/引用边界，P4 已落地的同一条边界现在应用到人读层）。

**DevPlan review 结论**（本次 review 的产出，已回填进 DevPlan 正文，见其 P4 一节的"对后续的影响"）：
P4 的 ActionPlan 曾在其 §4 第 2 点把"`CompactionInfo` 在人读侧是否已有其它落点"这个问题明确留给
"写 P5 ActionPlan 前确认"——本文 §2.1 就是这次确认，结论是：**没有**，且不止 `Compaction`，
`Edit`/`StitchEdge`/`SysChanged`/`NoReply` 五类信号今天全部只存在于即将被删除的 fact-layer 里，
处置方式见 §2.2。DevPlan P5.1/P5.2/P5.3 的任务边界与验收标准逐条核实后**不需要改动**——
需要调整的只是**执行顺序**，见下段。

**执行顺序与 DevPlan 文本顺序不同，原因见此**：DevPlan 按 P5.1→P5.2→P5.3 编号，但 P5.1（删除
fact-layer）的安全前提是"删除之后，fact-layer 承载的每一类信息都还能通过别的路径抵达"——这个前提
恰恰是 P5.2（详单链接）和 P5.3（系统提示词引用化）建立的。先删后补会经历一个真丢信息的中间态
（DevPlan 用在 P4→P5 依赖上的同一句话："先删后补会留下一个真丢结构的版本"，同样适用于 P5 内部
三个子任务之间）。因此本文**先做 P5.2、再做 P5.3、最后做 P5.1**——这与 P2/P4 ActionPlan 因同样
理由重排/合并任务顺序是同一类调整，不是范围变化。

**P5 范围边界**（与 DevPlan 一致）：只动 `journey-<id>.md` 的渲染（`internal/story/render_md.go`/
`render_md_sysprompt.go`/`render_spine.go`/`render_spine_step.go`）与其生成时机
（`cmd/vmr/cmd_story.go` 的 `writeJourneyFile`/`ensureJourneyFile`/`renderJourney`/`renderJourneys`）。
**不动**：`journey-<id>.json`（P4 已定型，`structure` 字段不变）、`render_compare.go`/
`vmr-story-corpus.md`（P4 的范围说明已明确"不动人读产物"专指单 Journey 报告；Compare/Corpus
报告不复用 `render_md.go` 的 fact-layer 渲染函数，已用 `grep` 核实零调用，本次不涉及）。

**已读代码确认的关键前提**（P5.2/P5.3/P5.1 都要用到，先在这里一次性钉住，不在各任务里重复核实）：

1. **`reqdetail.FileNameForManifest` 的 doc comment 已经明确预留了本任务的用途**：
   "the shape a 'previous turn' link, or **a future spine Step's '→ detail' link (see the
   architecture doc's P5.2)**, has on hand instead of the full record"（`internal/reqdetail/
   detail.go:88-96`）——P5.2 直接调用它计算链接文件名，不需要新设计命名规则，也不需要 `*audit.Record`
   就能算出链接文本（渲染 Markdown 时用）。
2. **`Step` 没有存"同一 Lineage 内的上一条 Manifest"，且这个值在缝合边界必须是 `nil`，不是前一条
   Lineage 的尾记录**：`journey.go` 构建时 `prevManifest` 只是一个局部变量（约 349-393 行），从未
   落到 `Step` 字段上，这部分判断没错；但**执行前修正**：`atStitchBoundary` 分支里的 `prevManifest
   = predLineage.Manifests[len-1]` 只是为了算 `sysChanged`/`buildCompactionInfo`，**不能**当作
   `reqdetail.EnsureRendered` 的 `prev` 参数使用——已用源码核实，`internal/report/session.go` 的
   `group()`（526 行注释"one SessionInfo per Lineage"）与 `attach()`（633-645 行，`parent :=
   s.Recs[len(s.Recs)-1]`）证明 `report` 侧的会话分组严格以单个 `ctxgraph.Lineage` 为界，一个
   Lineage 的第一条记录（不论是否被缝合到前一条 Lineage）`r.Parent` 恒为 `nil`
   （`linkStitchedLineages` 只设置 `SessionInfo.ContinuedFrom` 这个展示层链接，不改变任何单条记录
   的 `Parent`）——即 `vmr report -details` 为任何一个 Lineage 首条记录生成的详单，`prev` 恒为
   `nil`，没有 `PrevTurnLink`，`DeltaStart=0`（整条 manifest 都算新增）。如果 `story` 在缝合边界
   把 `predLineage.Manifests[len-1]`（一个物理上不相关的、来自另一条 Lineage 的 Manifest）当作
   `prev` 传给 `EnsureRendered`，会：(a) 触发 `ctxgraph.Classify(prev, m)` 在两个结构上无关的
   manifest 之间计算一个没有意义的 LCP/编辑分类（`journey.go` 自己在这一分支的注释已经明确写道
   "Classify's structural LCP has no meaning across a stitch boundary"）；(b) 渲染出一个
   `report -details` 从未为同一条记录生成过的 `PrevTurnLink`。两个半区对同一条记录产出内容不同的
   详单，直接击穿 P2 的"逐字节相同"不变量，且 `EnsureRendered` 的存在性检查只认文件名不认内容，
   谁先跑谁的版本被永久固化。**正确做法**：`Step.PrevManifest` 只在 `case i > 0`（同一 Lineage
   内部）赋值为 `l.Manifests[i-1]`；`atStitchBoundary` 分支保持 `Step.PrevManifest` 为 `nil`——
   与 `report` 侧 `ReqInfo.Parent` 在同一位置的语义完全一致（都是"这条记录是不是它所在 Lineage 的
   第一条"）。缝合边界原有的局部变量 `prevManifest`（用于 `sysChanged`/`buildCompactionInfo`）不受
   影响，继续按原逻辑计算，只是**不再**把它写进 `Step.PrevManifest` 这个新字段。
3. **`story`/`report`/`vmr replay` 三者共享同一个 `outDir`**：`stories/`、`details/`、`evidence/`
   都是 `outDir` 的直接子目录（`cmd_story.go` 的 `storiesDir := filepath.Join(outDir, "stories")`；
   `internal/report/detail.go` 的 `evidenceDir: filepath.Join(filepath.Dir(dir), "evidence")`，
   `dir` 是 `{outDir}/details`，故 `evidenceDir = {outDir}/evidence`）——`stories/journey-X.md` 到
   `details/Y.md`、`evidence/Z.md` 的相对路径都是 `../details/Y.md`、`../evidence/Z.md`，与
   `details/` 内部到 `evidence/` 的相对深度**相同**。这就是 `KNOWN_ISSUES §1.26`"证据条目跨目录
   深度的相对链接规则，只有 `details/` 一处写清楚了"这条低优先级提醒在 P5 这里的具体答案：**不需要
   一条新规则**，直接复用 `../` 前缀即可，P5 收尾时把这条 KNOWN_ISSUES 改写为"已在 P5 验证适用"。
4. **系统提示词证据文件名必须用 `Manifest.SysHash`，不能用 `systemPromptEras` 自己拼出的文本重新
   算哈希**：`render_md_sysprompt.go` 的 `systemPromptEras` 用 `strings.Join(parts, "\n\n---\n\n")`
   拼接同一 Step 里可能存在的多条 system 消息（如 Responses 协议的 `system`+`instructions` 分离
   字段），这个分隔符是 `story` 自己发明的展示格式；而 `internal/reqdetail/evidence.go` 的
   `EnsureSysPromptEvidence`→`leadingSystem`→`ctxgraph.LeadingSystemText` 是**无分隔符**的原始
   拼接（`b.WriteString(msg.Text)`，逐条直接相连），且是 `Manifest.SysHash` 本身的定义
   （`ctxgraph/manifest.go:137` `m.SysHash = md5.Sum([]byte(LeadingSystemText(msgs, m.LeadSys)))`）。
   两种拼接结果字面不同 ⇒ 两种哈希不同。**正确做法**：每个 era 的证据文件名必须由该 era 起始 Step
   的 `s.Manifest.SysHash`（或直接调用 `EnsureSysPromptEvidence(evidenceDir, s.Rec)` 并使用它
   返回的文件名）决定，不能自己重新算——`ctxgraph.LeadingSystemText` 的 doc comment 原文正是为了
   防止"any consumer materializing 'the text behind a given SysHash' ... derives it from this
   one function instead of re-implementing the concatenation and silently drifting"（
   `internal/ctxgraph/manifest.go:154-163`），P5.3 如果需要文本本身（用于展示字符数摘要），也应该
   调用这同一个函数，不要沿用 `systemPromptEras` 现有的 `"\n\n---\n\n"` 拼接。
5. **`systemPromptEras` 靠扫描 `NewEvents` 分组，对 `i > 0` 的 Step 完全检测不到系统提示词变更，
   这是一个比"哈希来源不一致"更严重的现存 bug，P5.3 顺手一并修正**：`journey.go` 的构建循环里，
   只有两种情况 `deltaStart == 0`——整个 Journey 的第一个 Step（`ci==0 && i==0`，初始值从未被
   任何分支改写），以及缝合边界（`atStitchBoundary` 分支显式保留 `deltaStart` 为 0）。除此之外，
   `case i > 0`（同一 Lineage 内的正常延续）里 `deltaStart = m.LeadSys + e.LCP ≥ m.LeadSys`——
   `appendNewEvents`（`journey.go:445-458`）从 `deltaStart` 开始扫描，**结构上永远不会**把索引
   `0..LeadSys-1`（即 leading system 消息本身）纳入 `NewEvents`。也就是说，一次发生在 Lineage
   内部（非缝合边界）的系统提示词变更——`s.SysChanged` 完全可以在这种 Step 上为 `true`（换模型/
   换工具集/平台注入变化，`journey.go:392-393` 的判断本来就不区分是否在 Lineage 内部）——新的
   系统提示词文本永远不会出现在这个 Step 的 `NewEvents` 里，`systemPromptEras` 靠"扫 `NewEvents`
   找 `role=="system"` 的消息"这条现有规则**必然漏检**这类变更，只能在"Journey 第一步"和"缝合
   边界且新文本此前从未出现过"这两种场景下正确工作。这是当前代码里已经存在的一个真实缺陷（不是
   P5 引入的），但 P5.3 恰好要重写这个函数，应该借这个机会一次性改对，而不是把这条隐藏的坏规则
   原样保留、只换成员访问哈希来源。**正确做法**：`systemPromptEras` 改为直接基于每个 Step 的
   `Manifest.HasSys`/`Manifest.SysHash` 做状态机分组——`HasSys != 上一个 era 的 HasSys` 或
   `HasSys && SysHash != 上一个 era 的 SysHash` 时开一个新 era，完全不读 `NewEvents`，天然对齐
   `SysChanged` 本身的判定依据（`journey.go:392-393` 用的正是同一对字段），不会再漏检任何一种
   变更场景，也顺带让 §2.1(d) `spineTransitionLines` 里 `SysChangedLine` 的渲染条件与
   `systemPromptEras` 的分组条件成为同一份判断依据的两处独立表达，不会互相矛盾。见 §3.2 的具体
   设计。
6. **五类"图级分析事实"在人读侧目前只存在于即将删除的 fact-layer 里，且无法通过 P5.2 的详单链接
   触达**——这是本文对 P4 §4 第 2 点遗留问题的最终结论，详见 §2.1。`renderStep`（`render_md.go:97`）
   里除了消息正文和 LLM 回复之外，还渲染了 `s.Edge`（Edit 分类+LCP/Coverage）、`s.StitchEdge`
   （缝合证据）、`s.SysChanged`（system prompt 变更标记）、`s.Compaction`（token 增减+
   swallowed/survived 实体）、`s.NoReply`（本轮未实际回复）。这五者都是**跨记录的图分析结论**
   （`Edit`/`Compaction` 需要比较相邻两条记录，`StitchEdge` 需要整条语料的缝合图搜索）——`reqdetail`
   的详单渲染器只吃单条 `*audit.Record`（外加一个 `prev *ctxgraph.Manifest`，只够算 `DeltaStart`，
   不够重建完整 `Edit`/`Compaction`），**物理上不可能**通过"点开这一步的详单"看到它们。P4 已经把
   它们发布进 `journey-<id>.json` 的 `structure` 字段（`EditRef`/`StitchRef`/`CompactionRef`），
   但那是机读产物，不是人读报告里的一个可点击入口。**结论**：这五类信号不能随 fact-layer 一起
   删除，必须先搬进决策脊柱本身（人读层唯一还留着的地方），见 §2.2。搬迁是纯粹的"调用点位移"——
   `renderCompactionInfo` 函数体、`i18n.StoryText` 的 `EditLine`/`StitchLine`/`SysChangedLine`/
   `NoReplyLine`/`CompactionSummary` 全部原样复用，不重新设计格式或新增 i18n 文案。
7. **fact-layer 的另外两部分（消息正文、LLM 回复内容）确认可以安全删除，不需要替代**：
   - `renderEvent` 展示的 `NewEvents` 原始消息正文——P5.2 落地后，同一条记录的**完整**请求体
     （含它引入的全部历史消息）通过该 Step 的详单链接一键可达（`reqdetail.Render`→
     `renderClientRequest` 本来就逐条渲染请求里的每条消息），信息没有丢失，只是从"报告里默认展开
     的折叠块"变成"链接另一个文件"，这正是 DevPlan P5.1 验收标准"信息可达性由 P3/P4 建立的引用与
     链接保证"的字面含义。
   - `renderLLMResponse` 展示的 `Reasoning`/`RespText`/`ToolCalls`——P1.2 已经把决策脊柱扩展到
     **每一个** Step（不只是有工具调用的），`spineWhyLine`（RespText/Reasoning 摘要，超限折叠不
     截断）+ `toolCallLine`/`toolResultLine`（工具调用与配对结果，同样折叠不截断）已经完整覆盖
     fact-layer 这部分展示的内容，且脊柱版本的折叠阈值（`spineWhyRespCap`/`spineWhyReasoningCap`）
     本来就是"折叠，不是截断"，没有信息损失。这部分是真正的重复，删除不需要任何替代。
8. **`internal/story` 已被列入 `internal/reqdetail` 的既定消费方**（`CLAUDE.md` 模块表：
   "`internal/reqdetail`……Shared by `report` and `story` so a page is byte-identical regardless
   of which command renders it"），`archtest` 的 `import_boundaries_test.go` 里 `story` 的禁止
   导入列表（`router`/`server`/`report`）不含 `reqdetail`——`story` 直接 `import "vmr/internal/
   reqdetail"` 不会触发边界测试，不需要额外登记豁免。
9. **行数预算现状**（P5.1 是净删除，无风险；P5.2/P5.3 是净增加，逐一确认余量）：
   `render_md.go` 350 预算/296 现状（P5.1 会让它显著缩小，不是风险点）；`render_spine.go` 380/278
   （102 行余量）；`render_spine_step.go` 未单独登记，走默认 700 行上限，现状 329（371 行余量）；
   `journey.go` 850/697（153 行余量，加一个 `Step.PrevManifest` 字段只占 1 行）；`cmd_story.go`
   850/754（**只剩 96 行余量**——P5.2 需要在多个调用点（`writeJourneyFile`/`ensureJourneyFile`/
   `renderJourney`/`renderJourneys`）穿线 `detailDir`/`evidenceDir`/`prof`，如果签名改动本身还不够，
   把"确保本 Journey 全部 Step 详单已生成"这一段循环逻辑放进 `internal/story`（如新文件
   `internal/story/ensure_details.go`，走 `story` 包自己的 700 行默认预算，而不是挤占
   `cmd_story.go` 本就紧张的余量）——这是本文对"落在哪个文件"的明确建议，不是留给实现时随意判断
   的开放项。

---

## 1. 执行前置检查

```bash
git status --short                     # 确认工作区干净，P4 那批改动已在 HEAD（eb45238）
go build -o /private/tmp/claude-501/-Volumes-SSD2T-code-vmr/6f39e657-dc7d-4f31-9815-44ccaf4bc8f5/scratchpad/vmrbin ./cmd/vmr
go test ./... 2>&1 | tail -30           # 建立改动前的基线：全绿
ls logs/vmr-audit-2026-07-28.jsonl.zst  # P1-P4 用过的同一批样本，本机已存在
```

按 §2（P5.2）→ §3（P5.3）→ §4（P5.1）顺序执行——原因见 §0。每做完一个子步骤跑一次
`go test ./internal/story/... ./internal/reqdetail/... ./internal/archtest/...`，不要攒到最后
一起查错，这三个包在 P5 里都有改动。

---

## 2. 任务一：脊柱挂详单链接 + 补齐图级分析事实的人读落点（P5.2，先行执行）

### 2.1 目标设计

**(a) `Step` 加 `PrevManifest` 字段，且只在同一 Lineage 内部赋值**（`internal/story/journey.go`）：
把构建循环里已有的 `prevManifest` 局部变量（约 349-393 行）写进新建的 `Step.PrevManifest
*ctxgraph.Manifest` 字段——但**只在 `case i > 0` 分支**（`prevManifest = l.Manifests[i-1]`）写入，
`atStitchBoundary` 分支**保持 `Step.PrevManifest` 为 `nil`**（§0 第 2 点已用源码核实：缝合边界的
`prevManifest = predLineage.Manifests[len-1]` 只用于该分支自己的 `sysChanged`/
`buildCompactionInfo` 计算，与 `reqdetail.EnsureRendered` 需要的"同一 Lineage 内的物理前驱"是
两个不同的东西——传给 `EnsureRendered` 会导致 `story`/`report` 对同一条记录生成不同内容的详单，
击穿 P2 的逐字节一致不变量）。写法上，`case i > 0` 分支末尾加一行 `step.PrevManifest =
prevManifest`（此时 `prevManifest` 已经是 `l.Manifests[i-1]`）；`atStitchBoundary` 分支不加
任何赋值，`buildStep` 的返回值里 `PrevManifest` 保持零值 `nil`。

**(b) 脊柱每个 Step 挂详单链接**：在 `spineStepHeader`（`render_spine_step.go:168`）或紧随其后的
位置，加一行链接：

```go
// 伪代码，实际落点/函数名以实现时为准，语义不变：
filename := reqdetail.FileNameForManifest(s.Manifest)
w("%s", st.SpineDetailLink("../details/" + filename))
```

`FileNameForManifest` 是纯函数（不需要 `prev`，不需要 I/O），可以在**任何**渲染路径下算出与最终
落盘文件名逐字节相同的链接文本——这正是 P2 建立、P4 doc comment 明确点名复用的性质。链接**永远
渲染**（DevPlan 原文"名称可算"），不依赖文件是否已经在磁盘上——即使 §2.1(c) 的按需生成因为某种
原因失败，链接文本本身仍然正确，只是点开会 404，这比"没有链接"更接近事实。

**(c) 渲染时按需生成详单**（DevPlan 原文"渲染时目标缺失即按需补生成"）：新增一个 story 包内的
编排函数（建议放新文件 `internal/story/ensure_details.go`，理由见 §0 第 9 点），签名形如：

```go
// EnsureJourneyDetails materializes every Step's detail page (and, when it
// has one, its system-prompt evidence blob — see EnsureSysPromptEvidence)
// under detailDir/evidenceDir, skipping any that already exist. Idempotent
// and safe to call before every render (EnsureRendered's own existence
// check makes repeat calls cheap) — this is what lets a spine's "→ detail"
// link resolve without requiring the caller to have run `vmr report
// -details` first.
//
// A per-Step failure is reported to warn, not returned — `vmr story` is a
// read-only offline analysis tool (see the architecture doc's positioning),
// and a single record that fails to render (a rare malformed body, a
// transient disk error) should not cost the reader the other 99% of an
// otherwise-complete Journey narrative. The link text itself is already
// correct either way (§2.1b computes it from the Manifest alone, not from
// whether the file exists) — a failed Step just leaves that one link
// pointing at a file that doesn't exist yet, which is a strictly better
// failure mode than losing the whole report.
func EnsureJourneyDetails(w io.Writer, j *Journey, detailDir, evidenceDir string, prof taskseg.Profile, lang i18n.Lang) {
    for _, s := range journeySteps(j) {
        if s.Rec == nil || s.Manifest == nil {
            continue // defensive — production path guarantees non-nil, test fixtures may not
        }
        if _, err := reqdetail.EnsureRendered(detailDir, s.Rec, s.Manifest.Path, s.Manifest.Line,
            s.Manifest, s.PrevManifest, prof, lang, evidenceDir); err != nil {
            fmt.Fprintf(w, "warning: journey %s step %d: detail export failed: %v\n", j.ID, s.Seq, err)
        }
    }
}
```

**错误处理策略：优雅降级，不中断主流程**（吸收并采纳并发评审的判断，推翻本文初稿"单步失败即整个
命令报错"的默认选择）——第一性原理：`vmr story` 是离线只读分析工具，不是在线交易网关；链接文本
本身是纯函数，与文件是否成功落盘无关（§2.1b），一个 Step 的详单写入失败（极端情况：磁盘满、极少数
历史记录编码异常）不应该让用户连其余 99% 已经能正常渲染的 Journey 叙事都拿不到。`w io.Writer`
（生产路径传 `os.Stderr`）让警告可见但不阻断，测试路径可以传一个 `bytes.Buffer` 断言警告文本。

调用点：`cmd/vmr/cmd_story.go` 的 `writeJourneyFile`（单 Journey 渲染的唯一落盘点）与
`ensureJourneyFile`（Compare 侧确保双方 Journey 文件存在时也顺带确保其详单——由于 Compare 报告
本身不接 P5.2 的链接，这里的"顺带"只是让被比较的两个 Journey 各自的 `journey-<id>.md` 拥有可用
链接，不代表 Compare 报告本身的范围变化）在调用 `story.RenderMarkdown` **之前**先调用
`story.EnsureJourneyDetails(j, detailDir, evidenceDir, prof, lang)`。`detailDir`/`evidenceDir`
按 §0 第 3 点的既定布局在 `outDir` 下拼出（`filepath.Join(outDir, "details")`/
`filepath.Join(outDir, "evidence")`），不需要新的配置项。

**默认无开关，无条件生成**：`vmr report -details` 默认 `false` 是因为一次性物化整个语料所有记录
代价高；`vmr story -journey ...`/`-compare` 一次只渲染一个（或两个）Journey，materialize 的
是该 Journey 自己的 Step 集合（真实语料 22-126 步/条），量级与"渲染一份报告"本身同一数量级，不需要
用户先跑一条不相关的命令才能让链接生效——这正是 DevPlan 验收标准"无需先运行另一条命令"的字面要求。
**`-render-all`/`-corpus` 例外**：这两条路径会在一次调用里遍历全部候选 Journey，材料化成本与
`vmr report -details` 同一数量级，落地时用真实语料实测一次全量 `-render-all` 的耗时增量，如果
明显不可接受（架构文档 §7.6(a) 用过的判断基准），再决定是否需要一个显式开关跳过（不预先假设
需要，用数字说话）。

**(d) 补齐图级分析事实的脊柱指示行**（§0 第 6 点的落地）：新增一个共享函数（建议放
`render_spine_step.go`，与 `spineStepHeader` 相邻）：

```go
// spineTransitionLines renders the cross-record analysis facts fact-layer
// used to show (renderStep, now deleted by P5.1) — Edit/StitchEdge/
// SysChanged/Compaction — reusing the exact same i18n.StoryText functions
// and CompactionInfo renderer render_md.go already has: these are graph-
// level facts a per-step detail link (§2.1b) can never reach (reqdetail
// renders one record + one prev Manifest, never a full Edit/StitchGraph/
// Compaction computation), so the spine — the one human-readable layer
// that survives P5.1 — is their only remaining home. Called from both
// renderSpineStep and renderSpineBriefStep, right after the header line,
// so the placement is independent of whether the Step happens to have
// tool calls.
func spineTransitionLines(w func(string, ...any), s *Step, storyT i18n.StoryText) {
    if s.Edge != nil && s.Edge.Kind != ctxgraph.Append {
        w("%s", storyT.EditLine(s.Edge.Kind.String(), editStatsHint(*s.Edge, storyT)))
    }
    if s.StitchEdge != nil {
        w("%s", storyT.StitchLine(s.StitchEdge.Kind.String(), pctStr(s.StitchEdge.Score), pctStr(s.StitchEdge.Confidence)))
    }
    if s.SysChanged {
        w("%s", storyT.SysChangedLine)
    }
    if s.Compaction != nil {
        renderCompactionInfo(w, s.Compaction, storyT)
    }
}
```

**`Append` 静默，不渲染**：同一 Lineage 内 `i>0` 的 Step，`s.Edge.Kind` 结构上只可能是
`Append`/`ReplaceTail`/`Splice`（`Contract`/`Fork` 会切分 Lineage，永远不出现在 `l.Edges` 里，
见 `lineage.go` 的 `Edges` 字段注释），而 `Append`（"cur 以 prev 的全部消息开头"）正是绝大多数
正常轮次的默认状态——真实语料里这类 Step 占压倒性多数。fact-layer 原样为每个非 nil `s.Edge`
渲染一行"> 编辑: Append（...）"，把这条噪声原样搬进脊柱会严重稀释脊柱"3 秒扫读"的信噪比，且与
脊柱自身"宁可粗糙也不猜语义、但也不堆砌无信息量的行"这条一贯原则相悖。`Append` 是"什么都没有
结构性地发生"，机读层 `structure` 字段已经完整记录每一步的 `EditRef`（P4），省略常规 `Append`
不构成信息丢失，只省略"正常"这个默认状态本身。

`s.NoReply` 保持 fact-layer 原有的位置语义（描述"这一轮结束时发生了什么"，不是"进入这一步时发生了
什么"）——在 `renderSpineStep`/`renderSpineBriefStep` 各自内容渲染**之后**补一行
`if s.NoReply { w("%s", storyT.NoReplyLine) }`，不要并进 `spineTransitionLines`。

**命名冲突提醒（实现时容易踩的坑，不是需要决策的开放项）**：`render_spine_step.go`/
`render_spine.go` 现有函数签名里 `t` 这个参数名指的是 `i18n.SpineText`（与 `render_md.go`/
`render_md_sysprompt.go` 里 `t` 指 `i18n.StoryText`、`st` 才是 `SpineText` 正好相反）。
`renderDecisionSpine`/`renderSpineStep`/`renderSpineBriefStep` 需要新增一个
`i18n.StoryText` 参数（`renderDecisionSpine` 内部本来就有 `lang`，加一行
`storyT := i18n.Story(lang)` 即可取得，不需要改调用方签名），命名上**不要**复用 `t`（已被
`SpineText` 占用），本文示例用 `storyT` 避免歧义，实现时可另择更短的名字，但务必在改动前确认
当前作用域里 `t` 到底指哪个类型，避免把 `StoryText` 的方法调用错误地写在 `SpineText` 变量上
（编译器会直接报错，不会是运行时才发现的问题，但排查会浪费时间）。

**i18n 新增**：`i18n.SpineText` 加一个字段，如 `SpineDetailLink func(relPath string) string`
（EN/ZH 都写，`internal/i18n/story_spine.go`），格式参考现有 `SpineFinalDeliverableExcerptLabel`
一类"純文字标签"风格，例如 ZH `"→ [详情](%s)"`、EN `"→ [detail](%s)"`（具体措辞落地时定，语义是
"这是通往这条记录完整渲染页面的链接"）。

### 2.2 具体步骤

1. `internal/story/journey.go`：`Step` 结构体加 `PrevManifest *ctxgraph.Manifest` 字段；**只在
   `case i > 0` 分支**（`prevManifest = l.Manifests[i-1]` 那一行之后）加
   `step.PrevManifest = prevManifest`；`atStitchBoundary` 分支不加任何赋值——`buildStep` 需要
   一个新参数把这个值传进去，或者在 `buildStep` 返回后由调用方直接赋值，取决于 `buildStep` 现有
   签名改起来是否顺手，两种方式语义等价。加一个直接单测：构造一条跨越缝合边界的合成 Chain
   （`stitch_test.go` 已有类似夹具可参考/复用），断言缝合边界那个 Step 的 `PrevManifest == nil`，
   同一 Lineage 内后续 Step 的 `PrevManifest` 等于其在 `Lineage.Manifests` 里的物理前驱——这是
   本任务最容易踩错、后果又最隐蔽的一步，必须有测试锁定，不能只靠代码审查。
2. `internal/story/ensure_details.go`（新文件）：`EnsureJourneyDetails`（§2.1c，`io.Writer` +
   优雅降级签名），加直接单测——正常场景（构造一个用 `buildTestJourney` 风格夹具生成的真实
   Journey，指向临时目录，断言 `journeySteps(j)` 每个 Step 对应的详单文件确实落盘，且用
   `reqdetail.FileNameForManifest` 独立算出的文件名与实际落盘文件名一致；重复调用一次断言不重复
   写）与失败场景（构造一个会导致 `EnsureRendered` 报错的输入，如 `detailDir` 指向一个不可写路径，
   断言警告文本写进传入的 `io.Writer`、函数正常返回、其余 Step 仍然继续处理，不因一个 Step 失败
   而提前退出循环）。
3. `internal/story/render_spine_step.go`：加 `spineTransitionLines`（§2.1d）；`renderSpineStep`/
   `renderSpineBriefStep` 分别在 `spineStepHeader(...)` 调用之后插入
   `spineTransitionLines(w, s, storyT)`，在各自内容渲染完之后插入 NoReply 行；`renderDecisionSpine`
   （`render_spine_step.go:128`）加 `storyT := i18n.Story(lang)`，传给两个子函数。
4. `internal/story/render_spine_step.go`（或 `spineStepHeader` 所在同一处）：加详单链接渲染
   （§2.1b），使用 `reqdetail.FileNameForManifest(s.Manifest)`；`s.Manifest == nil` 时跳过链接
   （防御性判空，理由同 P4 §0：生产路径保证非 nil，但测试夹具可能不保证）。
5. `internal/i18n/story_spine.go`：加 `SpineDetailLink` 字段（EN/ZH）。
6. `cmd/vmr/cmd_story.go`：`writeJourneyFile`/`ensureJourneyFile` 调用 `story.RenderMarkdown` 之前
   先调用 `story.EnsureJourneyDetails(os.Stderr, j, detailDir, evidenceDir, prof, lang)`——
   `detailDir`/`evidenceDir` 在这两个函数的调用方（`renderJourney`/`compareJourneys`/
   `renderJourneys`/`renderAllJourneys`）里用 `outDir` 拼出，作为新参数穿线下去（这几个函数已经
   持有 `outDir`，加两个 `filepath.Join` 调用即可，不需要新的配置读取）。**注意**：
   `writeJourneyFile`/`ensureJourneyFile` 当前不接收 `prof`——调用方 `renderJourney`/
   `compareJourneys` 都已经持有 `prof`（函数签名里已有），补上这一个参数穿线即可，不需要新的
   解析逻辑。
7. `go test ./internal/story/... ./internal/reqdetail/... ./internal/archtest/...`：确认新增测试
   通过；`render_spine_test.go`/`golden_test.go` 里断言 Markdown 全文的测试大概率需要
   `UPDATE_GOLDEN=1` 重跑（脊柱新增了链接行与 Edit/StitchEdge/SysChanged/Compaction/NoReply 行），
   逐行核对 diff 只对应本任务的改动。
8. `cmd/vmr/cmd_story_test.go`：至少一个产物级测试验证 `journey-<id>.md` 里的详单链接指向的文件
   确实存在于 `{outDir}/details/` 下（复用 P4 §8.2 的教训——`writeJourneyFile` 这条生产写入路径
   曾经漏过一次真实的接线，产物级测试是唯一能在真实调用形状下发现这类遗漏的检验）。
9. 真实语料验证：
   ```bash
   ./vmrbin story -o /tmp/p5verify -journey 'j-openclaw-20260728T000544*' logs/vmr-audit-2026-07-28.jsonl.zst
   grep -c '详情\|detail' /tmp/p5verify/stories/journey-j-openclaw-*.md   # 期望 22（每个 Step 一条链接）
   # 逐条验证链接目标文件确实存在（用 python3 或 shell 都可以）：
   python3 -c "
   import re, os, glob
   md = open(glob.glob('/tmp/p5verify/stories/journey-j-openclaw-*.md')[0]).read()
   links = re.findall(r'\.\./details/(\S+\.md)', md)
   print('links:', len(links))
   missing = [l for l in links if not os.path.exists(f'/tmp/p5verify/details/{l}')]
   print('missing:', missing)
   "
   grep -c '缝合\|Compaction\|stitch' /tmp/p5verify/stories/journey-j-openclaw-*.md  # 期望 ≥1（P4 执行记录确认这条样例真的经过一次缝合）
   ```
   （用 P4 执行记录 §8.1 表格里确认过的同一条样例——"1 步有 `stitch_edge`、1 步有 `compaction`"，
   这里应该能在人读 `.md` 里同样看到这一步的缝合详情，验证从"只在 JSON 里可查"变成"在报告本身
   可读"这件事真的发生了。）

### 2.3 验收标准（对照 DevPlan P5.2）

- [x] 每一步的详单链接均可达（链接目标文件确实存在于 `details/` 下）——无需先运行 `vmr report`。
- [x] 链接文件名与 `vmr report -details` 对同一条记录生成的文件名逐字节相同——新增一个跨命令回归
      测试（建议 `TestEnsureJourneyDetails_MatchesReportDetails`，放 `internal/story/
      ensure_details_test.go`）：用同一段构造的语料，分别通过 `EnsureJourneyDetails`（`story` 侧）
      与 `report.WriteDetails`/`reqdetail.EnsureRendered`（`report` 侧的既有调用路径）各自生成
      详单目录，断言两边同名文件正文 **100% 逐字节相同**——覆盖普通 Step（`prev` 非 nil）与缝合
      边界 Step（`prev` 应为 nil）两种场景，后者正是 §0 第 2 点修正的那个 bug 的直接回归锁定。
- [x] Edit/StitchEdge/SysChanged/Compaction/NoReply 五类信号在删除 fact-layer 之后仍然可以在
      `journey-<id>.md`（脊柱本身）里直接看到，不需要打开 `journey-<id>.json`。

---

## 3. 任务二：系统提示词改为引用（P5.3）

### 3.1 现状（已读代码确认，见 §0 第 4/5 点）

`render_md_sysprompt.go` 的 `renderSystemPromptHeader`/`systemPromptEras` 今天为每个 system
prompt "era"（一段连续、system 提示词不变的 Step 区间）渲染一个折叠块，**内联全文**
（`codeFence(e.Text)`）。这个函数有**两个**独立问题，P5.3 必须一并解决，不能只修其中一个：

1. **哈希来源不一致**（§0 第 4 点）：`e.Text` 由 `systemPromptEras` 自己用 `"\n\n---\n\n"` 拼接，
   与 `EnsureSysPromptEvidence` 内部用 `ctxgraph.LeadingSystemText`（无分隔符拼接）算出的
   `Manifest.SysHash` 字面不同——今天只用于展示所以不构成风险，但改成链接后必须换成
   `Manifest.SysHash` 本身，不能沿用这份展示专用的拼接去算哈希。
2. **分组逻辑对 `i>0` 的系统提示词变更完全失明**（§0 第 5 点，比第 1 点更严重、且是当前代码里
   已经存在的真实缺陷）：`systemPromptEras` 靠扫描每个 Step 的 `NewEvents` 找 `role=="system"`
   消息来判定"这里开始了一个新 era"，但 `journey.go` 的构建逻辑决定了**只有** Journey 第一步
   （`deltaStart` 从未被赋值，保持初始值 0）和缝合边界（`atStitchBoundary` 分支显式保留
   `deltaStart=0`）这两种情况，leading system 消息才可能落进 `NewEvents`——同一 Lineage 内部
   （`i>0`）的每一步，`deltaStart = m.LeadSys + e.LCP ≥ m.LeadSys`，`appendNewEvents` 的扫描范围
   结构上永远排除索引 `< LeadSys` 的消息。也就是说，一次发生在 Lineage 内部的系统提示词变更（
   `s.SysChanged == true` 但不在缝合边界、也不是第一步——例如同一条对话中途切换模型或工具集）
   今天的 `systemPromptEras` **检测不到**，报告头部的"系统提示词"折叠块会漏掉这次变更，读者会
   以为全程只用了一种系统提示词。

### 3.2 目标设计

**`systemPromptEras` 改为基于 `Manifest.HasSys`/`Manifest.SysHash` 的状态机分组，完全不读
`NewEvents`**（解决第 2 点，同时天然解决第 1 点——状态机本身就是按 `SysHash` 分组，不存在另拼一份
文本再算哈希的步骤）：

```go
// systemPromptEra is one contiguous run of Steps sharing the same system
// prompt content (by SysHash — not by scanning NewEvents, which only ever
// carries leading-system content at a Journey's first Step or a stitch
// boundary; see this file's package doc for why the previous NewEvents-
// based grouping missed every genuine mid-Lineage change).
type systemPromptEra struct {
    HasSys         bool
    SysHash        ctxgraph.Hash
    FromSeq, ToSeq int
    Owner          *Step // the era's first Step — carries the Manifest/Rec EnsureSysPromptEvidence needs
}

func systemPromptEras(j *Journey) []systemPromptEra {
    steps := journeySteps(j)
    var eras []systemPromptEra
    for _, s := range steps {
        if s.Manifest == nil {
            continue
        }
        m := s.Manifest
        last := len(eras) - 1
        if last < 0 || m.HasSys != eras[last].HasSys || (m.HasSys && m.SysHash != eras[last].SysHash) {
            eras = append(eras, systemPromptEra{HasSys: m.HasSys, SysHash: m.SysHash, FromSeq: s.Seq, ToSeq: s.Seq, Owner: s})
            continue
        }
        eras[last].ToSeq = s.Seq
    }
    return eras
}
```

分组条件（`HasSys` 变化，或 `HasSys` 都为 true 但 `SysHash` 不同）与 `journey.go:392-393` 计算
`sysChanged` 用的是同一对字段——这不是巧合，是"同一件事只应该有一种判定依据"的直接体现：
`spineTransitionLines`（P5.2 §2.1d）渲染 `SysChangedLine` 的条件与这里开新 era 的条件现在永远
一致，不会出现"脊柱说变了、系统提示词头部说没变"这种自相矛盾。`HasSys == false` 的 era（纯无
system prompt 交互）不渲染证据链接（见下）。

**报告头部只保留生效区间与摘要，正文引用共享证据条目**：`renderSystemPromptHeader` 对每个
`HasSys == true` 的 era，用 `era.Owner.Manifest.SysHash` 算出链接文件名，渲染
"`../evidence/<filename>`" 链接，替换掉 `codeFence(e.Text)` 的整段全文内联；`FromSeq`/`ToSeq`
生效区间保留（今天已经在渲染）。`HasSys == false` 的 era 不渲染证据链接（没有系统提示词，没有
可链接的证据文件），但仍然作为一个 era 出现在生效区间列表里（如实反映"这一段没有系统提示词"，
不是静默跳过）。

**证据文件的写入时机**：并入 §2.1(c) 的 `EnsureJourneyDetails`——`reqdetail.EnsureRendered` 内部
本来就会在 `evidenceDir != ""` 时调用 `EnsureSysPromptEvidence`/`EnsureToolsEvidence`（见
`ensure.go:50-58`），也就是说**只要 P5.2 的 `EnsureJourneyDetails` 已经跑过一遍，每个 Step 自己
的系统提示词证据文件就已经存在**——P5.3 不需要重新调用 `EnsureSysPromptEvidence`，只需要算出它
**会**写出的那个文件名。**为避免自己重新拼一遍"`sysprompt-` + hash8 + `.md`"这个命名规则**
（`evidence.go` 里这段命名是包内私有的，`contentHash8`/文件名拼接都未导出）——给
`internal/reqdetail` 加一个小导出函数：

```go
// SysPromptEvidenceFileName is the deterministic evidence filename
// EnsureSysPromptEvidence writes for a leading system block whose content
// hash equals sysHash — sysHash is normally a Manifest's own SysHash field.
// Exported so a caller that only has the hash (not rec) — e.g. a spine
// Step's "→ system prompt" link — can compute the same name without
// re-deriving evidence.go's private naming convention.
func SysPromptEvidenceFileName(sysHash ctxgraph.Hash) string {
    return "sysprompt-" + sysHash.String()[:8] + ".md"
}
```

这一个小导出函数把"文件名怎么拼"这条规则钉死在唯一一个地方（`EnsureSysPromptEvidence` 内部的
`filename = "sysprompt-" + contentHash8(text) + ".md"` 应该同步改为调用它，而不是并存两份等价
但独立的拼接逻辑）——不要在 `internal/story` 里重新拼接字符串常量。

**字符数摘要的来源**：新的 `systemPromptEra` 不再持有拼接好的 `Text` 字段——如果 `SysPromptEraSummary`
的展示仍然想给出字符数量级提示，用 `ctxgraph.LeadingSystemText(chatmsg.Messages(body),
m.LeadSys)`（`era.Owner.Rec`/`era.Owner.Manifest.LeadSys`）现算，直接复用与证据文件相同的文本
来源，语义严格一致，且不需要为一个纯展示用途多存一份文本。若判断字符数摘要不值得为此多一次
`chatmsg.Messages` 解析（era 数量通常很少，成本可忽略，倾向于直接算），也可以简化为只展示生效
区间不展示字符数——两种做法都不影响链接正确性，选择留给实现时按代码整洁度判断。

### 3.2 目标设计

**报告头部只保留生效区间与摘要**：把 `renderSystemPromptHeader` 每个 era 的折叠块内容从
"完整原文" 改为"字符数摘要 + 指向 `evidence/sysprompt-<h8>.md` 的链接"，`FromSeq`/`ToSeq` 生效
区间保留（今天已经在渲染，`t.SysPromptEraSummary(e.FromSeq, e.ToSeq, chars)`）。

**证据文件的写入时机**：并入 §2.1(c) 的 `EnsureJourneyDetails`——`reqdetail.EnsureRendered` 内部
本来就会在 `evidenceDir != ""` 时调用 `EnsureSysPromptEvidence`/`EnsureToolsEvidence`（见
`ensure.go:50-58`），也就是说**只要 P5.2 的 `EnsureJourneyDetails` 已经跑过一遍，每个 Step 自己
的系统提示词证据文件就已经存在**——P5.3 不需要重新调用 `EnsureSysPromptEvidence`，只需要在渲染
`systemPromptEras` 时，对每个 era 的起始 Step 算出 `EnsureSysPromptEvidence` **会**写出的那个
文件名（即 `"sysprompt-" + <hash8> + ".md"`，`hash8` 从 `era` 起始 Step 的 `Manifest.SysHash`
取前 4 字节 hex）。**为避免自己重新拼一遍"`sysprompt-` + hash8 + `.md`"这个命名规则**（`evidence.go`
里这段命名是包内私有的，`contentHash8`/文件名拼接都未导出）——推荐给 `internal/reqdetail` 加一个
小导出函数：

```go
// SysPromptEvidenceFileName is the deterministic evidence filename
// EnsureSysPromptEvidence writes for a leading system block whose content
// hash equals sysHash — sysHash is normally a Manifest's own SysHash field.
// Exported so a caller that only has the hash (not rec) — e.g. a spine
// Step's "→ system prompt" link — can compute the same name without
// re-deriving evidence.go's private naming convention.
func SysPromptEvidenceFileName(sysHash ctxgraph.Hash) string {
    return "sysprompt-" + sysHash.String()[:8] + ".md"
}
```

这一个小导出函数把"文件名怎么拼"这条规则钉死在唯一一个地方（`EnsureSysPromptEvidence` 内部的
`filename = "sysprompt-" + contentHash8(text) + ".md"` 应该同步改为调用它，而不是并存两份等价
但独立的拼接逻辑）——不要在 `internal/story` 里重新拼接字符串常量。

**字符数摘要的来源**：`chars` 不再需要来自 `systemPromptEras` 自己拼接的 `e.Text`（那份文本本身
仍然可以保留用于计算字符数——字符数摘要不要求与证据文件字节精确对应，只是给读者一个大致量级的
提示——**但**如果要做到语义严格一致，改用 `ctxgraph.LeadingSystemText(chatmsg.Messages(body),
m.LeadSys)`（era 起始 Step 的 `s.Rec`/`s.Manifest.LeadSys`）计算，直接复用与证据文件相同的文本
来源。两种做法都不会导致链接失效（链接只依赖 `SysHash`，与字符数计算方式无关）——选择哪种留给
实现时按代码整洁度判断，但如果保留 `systemPromptEras` 现有拼接方式用于字符数展示，**要在注释里
写清楚**"这份文本仅用于字符数估算，与证据文件内容的拼接方式不同，不能假设逐字节一致"，避免未来
有人拿它去做别的用途。

### 3.3 具体步骤

1. `internal/reqdetail/evidence.go`：加 `SysPromptEvidenceFileName(sysHash ctxgraph.Hash) string`
   （导出）；`EnsureSysPromptEvidence` 内部改为调用它，不再自己拼 `"sysprompt-" + contentHash8(text) + ".md"`
   字面量（`contentHash8(text)` 与 `sysHash.String()[:8]` 在同一份文本下数值相同，替换后行为不变，
   仅消除重复拼接点）。加一个直接单测：给定同一段文本，分别用 `md5.Sum` 手算 hash 传入
   `SysPromptEvidenceFileName`，与 `EnsureSysPromptEvidence` 实际写出的文件名比对，断言相等。
2. `internal/story/render_md_sysprompt.go`：`systemPromptEras` 整体重写为 §3.2 的状态机版本
   （`HasSys`/`SysHash` 分组，不读 `NewEvents`）；加直接单测覆盖三种场景——(a) 全程系统提示词
   不变（1 个 era）、(b) Journey 第一步之外、同一 Lineage 内部发生系统提示词变更（**这是当前
   代码检测不到、本次要修的场景**，断言新版本能正确产出 2 个 era）、(c) 缝合边界带来系统提示词
   变更（沿用原本就能检测到的路径，确认没有因重构而回归）。`renderSystemPromptHeader` 改为对每个
   `era.HasSys == true` 的 era 调用 `reqdetail.SysPromptEvidenceFileName(era.SysHash)` 算出链接
   文件名，渲染 "`../evidence/<filename>`" 链接，替换掉 `codeFence(e.Text)` 的整段全文内联；
   `era.HasSys == false` 的 era 只渲染生效区间，不渲染链接。
3. `internal/i18n/story_render.go`：`SysPromptEraSummary` 的签名/文案调整为包含链接（或新增一个
   字段专门渲染链接行，视现有函数签名改起来是否顺手）；旧的"完整原文内联"相关文案（如果有专门的
   summary 文案区分"内联"与"链接"两种模式）一并检查是否需要清理。
4. `go test ./internal/story/... ./internal/reqdetail/...`：确认通过；golden/固定断言更新。
5. 真实语料验证：
   ```bash
   ./vmrbin story -o /tmp/p5verify -journey 'j-openclaw-20260728T000544*' logs/vmr-audit-2026-07-28.jsonl.zst
   grep -A2 'System Prompt\|系统提示词' /tmp/p5verify/stories/journey-j-openclaw-*.md
   ls /tmp/p5verify/evidence/sysprompt-*.md   # 期望文件确实存在，文件名与报告里的链接一致
   diff <(md5 -q /tmp/p5verify/evidence/sysprompt-*.md 2>/dev/null || md5sum /tmp/p5verify/evidence/sysprompt-*.md) /dev/null  # 仅确认非空、可读，无需与历史基线比较
   ```

### 3.4 验收标准（对照 DevPlan P5.3）

- [x] 报告头部只保留生效区间（FromSeq-ToSeq）与摘要（字符数），正文不再内联系统提示词全文。
- [x] 系统提示词链接指向的证据文件确实存在，且与 `EnsureSysPromptEvidence` 独立计算出的哈希
      一致（不是 `story` 自己另算的一份）。
- [x] 短任务报告体积不再被系统提示词主导（真实语料对比：同一样例 Journey 改动前后的 `.md` 字节数，
      系统提示词部分从"正文体积的大头"降到"一条链接的量级"）。

---

## 4. 任务三：移除逐轮事实层（P5.1，最后执行）

### 4.1 现状回顾

在 §2/§3 落地后，`render_md.go` 的 `renderStep`/`renderLLMResponse`/`renderEvent`/
`renderCompactionInfo`/`prettyJSON`（`render_md.go:97-198`+`222-236`）承载的每一类信息都已经有
了替代落点：

| fact-layer 原内容 | P5 之后的落点 |
| --- | --- |
| `NewEvents` 原始消息正文（`renderEvent`） | 该 Step 的详单链接（P5.2），点开可见完整请求体 |
| `Reasoning`/`RespText`/`ToolCalls`（`renderLLMResponse`） | 决策脊柱的 `spineWhyLine`+`toolCallLine`/`toolResultLine`（P1.2 已覆盖全部 Step，本次未改） |
| `Edit`/`StitchEdge`/`SysChanged`/`Compaction` | 决策脊柱的 `spineTransitionLines`（P5.2 §2.1d 新增） |
| `NoReply` | 决策脊柱各 Step 内容渲染后的一行（P5.2 §2.1d 新增） |
| 系统提示词全文（`renderSystemPromptHeader`，非 `renderStep` 的一部分，但同属"该删的内联全文"） | `evidence/sysprompt-<h8>.md` 链接（P5.3） |

**确认无遗漏的方法**：把这张表和 `renderStep`/`renderLLMResponse`/`renderEvent` 的源码逐行对照
一遍（已在 §0 第 6/7 点做过一轮，本节实现前应该在实际改动 §2/§3 之后再对照一次实际代码，确认没有
在写 ActionPlan 和实际实现之间产生新的遗漏——这正是 P4 执行记录里 `writeJourneyFile` 那次遗漏的
教训：读代码得出的结论要在真实语料上再核实一遍，不能只信"文档说已经覆盖了"）。

### 4.2 目标设计

**删除**（`internal/story/render_md.go`）：`renderStep`、`renderLLMResponse`、`renderEvent`、
`prettyJSON`（§0 第 9 点已核实这四个函数只在 render_md.go 内部相互调用，无其它调用方，可以整体
删除，不留任何过渡态）。`RenderMarkdown` 里驱动 fact-layer 的双层循环
（`for ti, task := range j.Tasks { w("## t%02d...", ...); for _, step := range task.Steps {
renderStep(...) } }`，`render_md.go:53-58`）与它专属的 `isRepeatStep` 预计算
（`render_md.go:46-51`，`renderDecisionSpine` 内部已经独立算过一份同样的 `repeat` map，这份是
死代码）一并删除。

**保留但改变调用方**：`renderCompactionInfo` 函数体不变，调用点已经在 §2.1(d) 挪进
`spineTransitionLines`——本节不需要再动它，只是确认 `render_md.go` 里那个旧调用点（在
`renderStep` 内部）随 `renderStep`一起消失后，新调用点（`render_spine_step.go`）是它唯一的
调用方。`editStatsHint`/`breakReasonHint`（`render_md.go:73-95`）继续保留在 `render_md.go`——
它们同时服务于 `RenderMarkdown` 顶部的 `Break` 警告（不属于 fact-layer，本次不删）和 §2.1(d) 的
`spineTransitionLines`（跨文件调用，同包内可直接调用，不需要导出/搬迁）。

**i18n 清理**：`ReasoningSummary`/`ReplySummary`/`EmptyEvent`/`RevisionMarker`（`i18n.StoryText`，
`internal/i18n/story_render.go`）在删除 `renderLLMResponse`/`renderEvent` 后失去唯一调用方
（§0 已用 `grep` 核实这四个字段今天只在这两个函数里被引用）——按项目"不留死代码"的惯例一并删除
（EN/ZH 两份文案）。删除前重新跑一次 `grep`（实现时的代码状态可能已经因为 §2/§3 的改动而变化），
不要照抄本文这次的核实结果。

### 4.3 具体步骤

1. `internal/story/render_md.go`：删除 `renderStep`/`renderLLMResponse`/`renderEvent`/
   `prettyJSON` 四个函数；`RenderMarkdown` 删除 fact-layer 循环与 `isRepeatStep` 预计算。
2. `internal/i18n/story_render.go`：重新 `grep` 确认 `ReasoningSummary`/`ReplySummary`/
   `EmptyEvent`/`RevisionMarker` 确实已无调用方后删除（EN/ZH）。
3. `go test ./internal/story/... ./internal/archtest/...`：`render_md.go` 应该从 296 行降到
   150 行左右（大约删掉 140-150 行），远低于 350 行预算，无需登记任何豁免；`archtest` 的函数长度
   检查、行数检查应该只有"变小"没有"变大"的变化。
4. Golden/固定断言更新：`UPDATE_GOLDEN=1 go test ./internal/story/... -run TestGolden`，逐行核对
   diff——每一处删除都应该对应"这部分内容在脊柱里已经能看到"（§2/§3 落地后应该已经是这样），
   如果 diff 里出现某处内容**彻底消失、脊柱里也找不到对应展示**，说明 §2.1(d) 的表格漏了一类
   信号，退回 §2 补齐，不要在这里勉强通过。
5. `render_md_test.go`：这个文件今天大概率主要测 fact-layer 相关渲染（`renderStep`/
   `renderEvent` 等）——`go test` 报错会直接指出哪些测试函数引用了已删除的符号，逐个迁移或删除
   （迁移到 `render_spine_step_test.go`/新建的 `render_spine_step_test.go` 补充用例，覆盖
   §2.1(d) 新加的 `spineTransitionLines`；纯粹测"fact-layer 格式对不对"且已被删除功能替代的用例
   直接删除，不要为了保测试覆盖率而保留已经无意义的断言）。
6. 真实语料验证——体积对比是本任务最直接的验收证据：
   ```bash
   ./vmrbin story -o /tmp/p5verify -journey 'j-openclaw-20260728T000544*' logs/vmr-audit-2026-07-28.jsonl.zst
   wc -c /tmp/p5verify/stories/journey-j-openclaw-*.md
   # 与 P5 改动前的同一样例（改动前的 .md，若已被覆盖，可用 git show HEAD:... 取出改动前版本对比）比较字节数
   grep -c '^### ' /tmp/p5verify/stories/journey-j-openclaw-*.md   # fact-layer 的 "### Step N" 标题应为 0
   ```
7. **无损可达性的最终交叉验证**（呼应 P4 无损重建检验的验收方式，但这次验证的是"人读报告 + 一跳
   链接"这条路径，不是"机读 JSON"）：对同一样例 Journey，人工（或写一个小脚本）核对 fact-layer
   改动前展示过的每一类信息，在改动后的 `journey-<id>.md`（含它链接到的 `details/`/`evidence/`
   文件）里都能找到等价内容——这是 DevPlan P5.1 验收标准"信息可达性由 P3/P4 建立的引用与链接保证"
   的直接落地检验，比对照 §4.2 的表格更有说服力，因为它是在真实产物上做的，不是在设计阶段的推理。

### 4.4 验收标准（对照 DevPlan P5.1）

- [x] 报告体积回到目标量级——真实语料给出改动前后的字节数对比。
- [x] 不保留开关——`vmr story` 没有新增任何"要不要展示 fact-layer"的 flag。
- [x] 信息可达性由 P3/P4/P5.2/P5.3 建立的引用与链接保证——§4.3 第 7 步的交叉验证覆盖。

---

## 5. 需要在实现时确认、不预先假设的几个点

1. ~~`EnsureJourneyDetails` 的错误处理策略~~——**已在 §2.1(c) 定案**：优雅降级（`io.Writer` 输出
   警告，不中断），不是本文初稿默认选择的"整个命令报错退出"。理由见 §2.1(c) 原文：`vmr story`
   是离线只读分析工具，一个 Step 的详单写入失败不应该让用户拿不到其余 99% 已经能正常渲染的内容。
2. ~~`renderJourneys`（`-render-all`）批量场景的详单生成成本~~——**已实测，无需逃生舱**：真实语料
   （`vmr-audit-2026-07-28.jsonl.zst`，322 条记录，6 个候选 Journey）对比——`-render-all` 关闭
   详单材料化的基线耗时 12.96s，开启后 15.52s，增量约 2.56s / 306 个详单文件（约 8ms/文件，
   全部来自 `EnsureJourneyDetails` 的串行循环），相对基线增幅约 20%，远低于"一个数量级"的阈值，
   不需要 `-no-details` 逃生舱。
3. ~~`systemPromptEra` 结构体的字段调整方式~~——**已在 §3.2 定案**：状态机重写后 `systemPromptEra`
   直接持有 `Owner *Step`，不再需要在两个方向之间选择。
4. ~~`journey_test.go` 现有 `Step` 构造夹具是否需要同步补 `PrevManifest`~~——**无需改动任何现有
   夹具**：`PrevManifest` 是 `journey.go` 构建循环内部直接赋值的字段，不经过任何独立的"构造 Step"
   辅助函数，所有走 `Build`/`BuildChain` 的现有夹具自动获得正确值。新增的
   `TestStep_PrevManifest_NilAtStitchBoundary`（`internal/story/journey_prevmanifest_test.go`）
   专门验证"有 prev"（同 Lineage 内）与"prev 为 nil"（缝合边界）两种场景，复用已有的
   `s231StyleFixture`，未新增夹具类型。

---

## 6. 收尾（P5.1–P5.3 共用）

1. **全量测试与架构边界**：
   ```bash
   go test ./... -race
   go test ./internal/archtest/...
   go vet ./...
   gofmt -l .
   ```
2. **CHANGELOG.md**：`[Unreleased]` 下按 Added/Changed 分类加条目（具体措辞落地时按实际改动定），
   例如：
   - Added: 决策脊柱每步新增详单链接（`→ 详情`），渲染时按需生成，无需先运行 `vmr report -details`。
   - Added: 决策脊柱新增 Edit/StitchEdge/SysChanged/Compaction/NoReply 指示行——这些图级分析事实
     不再只存在于已删除的逐轮事实层。
   - Changed: 系统提示词从报告正文完整内联改为链接到共享证据条目（`evidence/sysprompt-<hash>.md`），
     报告头部只保留生效区间与字符数摘要。
   - Changed: 任务报告不再逐轮展开完整消息体与 LLM 回复原文——决策脊柱已完整覆盖，全文通过详单
     链接一跳可达。
3. **KNOWN_ISSUES_sonnet-5.md**：
   - `§1.20`（fact-layer 与脊柱内容重复）——本阶段完成后目标状态已达成，改写为"已完成"或按仓库
     惯例归档/移除，不再是待办。
   - `§1.26`（证据条目跨目录相对链接规则）——补一句"P5 已验证 `stories/`/`details/` 到
     `evidence/` 同深度，规则不需要为 `story` 侧另写一份"，或视内容判断是否可以直接销号。
4. **架构文档同步**：`story_report_architecture_opus-5.md` §2.2 的历史基线（fact-layer 体积、
   系统提示词内联体积等）在 P5 完成后已经是历史数字，按 P1-P4 收尾的同一做法，补一条简短说明
   指向本文档。`docs/VirtualModelRouter_Design_v4_Analytics.md` 若有描述 Journey 报告结构的段落
   （提到 fact-layer/`### Step N`/系统提示词内联的），一并核对更新。`docs/UserGuide.md`/`.zh` 若
   描述了 `journey-<id>.md` 的产物形态，同步更新。
5. **边界复核**（DevPlan §2.2 第 6 条，三个问题）——已按实际执行情况回填，详见 §8：
   - 本阶段是否产生了架构文档未预见的事实？——是，两处，均已回写：(a) `Step.PrevManifest` 在
     缝合边界必须是 `nil`（不是前一条 Lineage 的尾记录），这是并发评审发现、本文执行前修正的一处
     真实设计错误，架构文档 §7.4(b)/§7.6(a) 未曾讨论过这个边界情形；(b) `systemPromptEras` 靠扫描
     `NewEvents` 分组的旧实现，对同一 Lineage 内部的系统提示词变更结构性失明——这是一个独立于
     本次重构的既有缺陷，P5.3 顺手一并修正。§2.1(d)"五类图级分析事实无法通过详单链接触达"这一
     发现成立，已用真实实现验证，但判断不需要单独回写架构文档——它只是 §7.4(b)/§7.6(a) 既有边界
     论证的直接推论，不是一个新的论证。
   - 本阶段是否改变了 P6 及以后的前提？——是一处：P6.2"补齐导航边"里"详单 → 索引"这类边，现在
     多了一条已经建立的"脊柱 Step → 详单"边（P5.2）——P6 落地时应引用这条边为已完成前提，不要
     重新设计。`KNOWN_ISSUES §1.26` 也已收窄：`stories/`→`evidence/` 的相对路径深度问题已解决，
     P6 只需处理根目录 `vmr-report.md`/`vmr-requests.md` 链接证据条目时的深度差异（不带 `../`）。
   - 本阶段是否暴露出某个原计划任务其实不必要？——是一处：本文 §5 原计划的"`renderJourneys`
     批量场景是否需要 `-no-details` 逃生舱"经真实语料实测（详单材料化只让 `-render-all` 耗时增加
     约 20%）确认不需要，不必再为此设计一个命令行选项。

---

## 7. 验收清单（对照 DevPlan P5 的验收标准逐项勾）

- [x] 样例任务报告体积达标（真实语料给出改动前后字节数对比）。
- [x] 链接全通——每一步的详单链接、系统提示词证据链接均可达，无死链接。
- [x] 内容无丢失——以 §4.3 第 7 步的交叉验证为对照，不靠肉眼判断（DevPlan 原文"最后一项以 P4 的
      无损重建检验为对照，不靠肉眼判断"——P5 的对照对象是"改动前的 fact-layer 展示内容"与"改动后
      脊柱+链接能否等价重建"，与 P4 验证"journey.json 能否无损重建 fact-layer"是同一条逻辑链的
      延伸）。
- [x] `go test ./...`（含 `-race`）、`go test ./internal/archtest/...`、`go vet ./...`、
      `gofmt -l .` 全绿。
- [x] CHANGELOG、KNOWN_ISSUES（§1.20 移除并归档为 §3 已闭环第 24 条；§1.26 更新收窄）、
      架构文档说明性备注、`docs/VirtualModelRouter_Design_v4_Analytics.md`、
      `docs/UserGuide.md`/`.zh` 均已同步。

---

## 8. 执行记录（2026-08-20，Sonnet 5）

本节是本文写完 ActionPlan 之后、实际落地执行的过程记录与总结——按用户要求补写，不是提前写好的
计划。**所有改动均未提交，等待人工 review。**

### 8.1 执行顺序与整体结果

严格按 §0 定下的顺序执行：先修正本文自身的两处设计错误（见 §8.2），再按 P5.2 → P5.3 → P5.1 顺序
落地，每完成一个子任务立即跑相关包测试，最后统一跑 `go build ./...`、`go vet ./...`、
`gofmt -l .`、`go test ./... -race`、`go test ./internal/archtest/...`，全部通过。用本机真实审计
日志验证（`logs/vmr-audit-2026-07-28.jsonl.zst`，P1-P4 用过的同一批样本）：

| 验证项 | 结果 |
| --- | --- |
| openclaw 样例 Journey（22 步/33 次调用）：脊柱详单链接覆盖 | 22/22，全部指向真实存在的文件 |
| 同上：链接文件名与内容 | 与 `vmr report -details` 对同一批记录生成的详单逐字节相同（自动化测试 + 真实语料双重验证） |
| 同上：Compaction/StitchEdge 信息可读性 | 从"只在 journey.json 里可查"变为"在 `.md` 本身可读"（脊柱新增一行 📉 Information loss 折叠块） |
| 同上：报告体积 | 改动前 298,732 字节（fact-layer 占 68%）→ 改动后 ~107,309 字节，降幅约 64% |
| 缝合边界记录的详单 `prev` | `vmr story` 与 `vmr report -details` 均为 `nil`，逐字节一致（`TestEnsureJourneyDetails_MatchesReportDetails`，含真实缝合场景） |
| `-render-all` 详单材料化的耗时增量 | 12.96s → 15.52s（+20%，322 条记录/6 Journey），不需要逃生舱 |
| 系统提示词中途变更检测 | 新状态机正确检测同一 Lineage 内的变更（此前的 `NewEvents` 扫描版本对这类情形结构性失明，`TestSystemPromptEras_MidLineageChange` 锁定回归） |
| golden 基线 diff | 每一行改动都能对应到 P5.1/P5.2/P5.3 三者之一，无法解释的多余 diff 为零 |

### 8.2 执行前对本文自身的两处修正（并发评审发现，均已核实为真并采纳）

执行开始前，仓库里出现了一份我没有创建的文档
（`docs/future-strategy/story_report_p5_action_plan_review_gemini-3.7-flash.md`）——与 P1-P4 执行
期间同样的并发写作模式，这次是在本文写完、**尚未开始执行**时就已介入。已通读全文并逐条用源码
核实（不是因为"是评审意见"就默认正确，也不是照单全收——8 条发现里 4 条判断为可采纳，4 条判断为
不改/降级处理，详见下方分类）：

**核实为真、执行前修正的两条"致命级"发现**：

1. **`Step.PrevManifest` 不能在缝合边界赋值为前一条 Lineage 的尾记录**（评审 §1.1）：本文初稿
   §2.1(a) 原计划"两条分支都写"，即缝合边界也把 `predLineage.Manifests[len-1]` 写进
   `Step.PrevManifest`。用源码核实（`internal/report/session.go` 的 `group()`/`attach()`）确认：
   `report` 侧的会话分组严格以单个 `ctxgraph.Lineage` 为界，任何 Lineage 首条记录（不论是否被
   缝合）的 `ReqInfo.Parent` 恒为 `nil`。若 `story` 按初稿写法在缝合边界传一个非 nil 的 `prev`
   给 `reqdetail.EnsureRendered`，会在两个结构上无关的 Manifest 间触发一次没有意义的
   `ctxgraph.Classify`，且渲染出一个 `report -details` 从未为同一条记录生成过的 `PrevTurnLink`——
   两个命令对同一条记录产出内容不同的详单，直接击穿 P2 的"逐字节相同"不变量，且
   `EnsureRendered` 的存在性检查只认文件名不认内容，谁先跑谁的版本被永久固化。**这是本次执行中
   影响最大的一次修正**——已在 §0/§2.1(a)/§2.2 原地改写为"只在 `case i > 0` 分支赋值"，并新增
   `TestStep_PrevManifest_NilAtStitchBoundary` 锁定缝合边界为 `nil`，新增跨命令集成测试
   `TestEnsureJourneyDetails_MatchesReportDetails`（`cmd/vmr`）在真实缝合场景下证明两个命令生成的
   详单逐字节相同。
2. **`systemPromptEras` 靠扫描 `NewEvents` 分组，结构性检测不到同一 Lineage 内部的系统提示词
   变更**（评审 §1.2）：本文初稿 §3.2 只打算把哈希来源从"自己拼接文本"换成 `Manifest.SysHash`，
   分组逻辑本身"不变"。用源码核实（`journey.go` 的 `appendNewEvents`/`deltaStart` 计算）确认：
   只有 Journey 第一步和缝合边界这两种情形 `deltaStart` 才可能是 0（从而让 leading system 消息
   进入扫描范围）；同一 Lineage 内的正常延续 `deltaStart = LeadSys + LCP ≥ LeadSys`，结构上永远
   排除 leading system 消息，即使 `s.SysChanged == true` 也检测不到。这是一个**独立于本次重构、
   当前代码里已经存在的真实缺陷**，不是 P5 引入的——但既然要重写这个函数，一次性改对而不是延用
   一条已知有盲区的规则。已改为基于 `Manifest.HasSys`/`SysHash` 的状态机（§3.2），新增
   `TestSystemPromptEras_MidLineageChange` 复现并锁定这条回归（新状态机检测到 2 个 era，验证
   `Manifest.SysHash` 是这类判断唯一正确的判据来源）。

**核实为真、采纳的两条中等优先级发现**：

3. **`EnsureJourneyDetails` 应优雅降级，不应让单个 Step 的详单写入失败中断整个命令**（评审
   §1.3）：本文初稿 §5 把这个问题列为待实现时判断的开放项，默认倾向"整个命令报错退出"。核实后
   采纳评审建议——`vmr story` 是离线只读分析工具，一个 Step 的详单生成失败不应该让用户拿不到
   其余 99% 已经能正常渲染的内容；链接文本本身是纯函数，与文件是否成功落盘无关。已在 §2.1(c)
   实现为 `io.Writer` 输出警告 + 继续处理，`TestEnsureJourneyDetails_GracefulDegradation` 锁定
   （构造 `detailDir` 无法创建的场景，断言警告写出且不 panic、不提前退出循环）。
4. **决策脊柱对常规 `Append` 编辑应静默，只展示非常规转折**（评审 §2.2）：本文初稿 §2.1(d) 打算
   把 fact-layer 的 `s.Edge != nil` 判断原样搬进脊柱，即每个非首步都会渲染一行"> 编辑:
   Append（...）"。真实语料证实这类行占压倒性多数（openclaw 样例 22 步里 20 步是 Append），会
   严重稀释脊柱的"3 秒扫读"信噪比，与脊柱自身一贯的克制风格相悖。已采纳：`spineTransitionLines`
   加 `s.Edge.Kind != ctxgraph.Append` 判断，只有 `ReplaceTail`/`Splice` 才渲染（`Contract`/
   `Fork` 会切分 Lineage，结构上不会出现在 `s.Edge` 里）；机读层 `structure.json` 仍记录每一步的
   `EditRef`，不构成信息丢失。

**核实为真、采纳但只是补一个已有单元测试而非改动实现的一条**：

5. **跨命令详单逐字节一致性缺少自动化回归测试**（评审 §2.4）：本文初稿只在真实语料验证步骤里
   口头要求"用真实语料对比"，没有落成 `go test` 能跑的测试。已采纳，新增
   `TestEnsureJourneyDetails_MatchesReportDetails`（`cmd/vmr` 包，唯一能同时看到 `story`/`report`
   两个组合根的位置），构造一个 s231 风格的缝合边界夹具，跑 `cmdStory -render-all` 与
   `cmdReport -details` 各自的生产入口，逐文件比较 `details/` 输出——这既是发现 1 的回归锁定，
   也是评审这条建议的落地。

**核实为文档措辞问题（评审 §2.1）、已顺手修正的一条**：本文原稿 §1 的构建命令写了上一个会话
遗留的临时 scratchpad 路径，已换成本次会话的实际路径。

**核实后判断不采纳、或判断为超出本次范围的两条**：

- 评审 §1.4 建议给 `EnsureJourneyDetails` 加并发 worker 池（担心 `-render-all` 场景 I/O 阻塞）：
  已用真实语料实测（见 §8.1 表格），耗时增量约 20%，远低于"需要并发优化"的判断阈值（对照 P3.6
  用过的"一个数量级"基准），本次不加，已在本文 §5 第 2 点回填实测数据供以后复核用。
- 评审 §2.3 的死代码清单核实为真但本文原计划本来就要求"实现时重新 grep 确认"，不算独立发现，
  按原计划步骤处理（见 §8.3）。

### 8.3 执行期间发现、原计划未预见的几个点

1. **`internal/reqdetail/detail.go` 里还有第二处独立实现同一条"sysprompt 文件名"拼接规则**：
   `renderClientRequest`（渲染详单本身请求区的系统提示词证据链接）用 `"sysprompt-" +
   contentHash8(sysText) + ".md"` 字面量拼接，与 `evidence.go` 的 `EnsureSysPromptEvidence` 是
   两处独立实现同一条命名规则——本文原计划只打算给 `evidence.go` 加 `SysPromptEvidenceFileName`
   导出函数，没有预见到还有第二处需要同步替换。执行时顺手把这处也改为调用新导出函数（同一份
   `crypto/md5` 计算，行为不变，纯粹消除重复拼接点）——这正是新函数 doc comment 里承诺的"把文件名
   怎么拼这条规则钉死在唯一一个地方"，留着不改就是自相矛盾。
2. **`codeFenceLang` 在 P5.1 之后失去唯一的非空 `lang` 调用方**：删除 `renderLLMResponse`（fact-
   layer 里唯一传 `"json"` 语言标签的调用点）后，`codeFenceLang` 的 `lang` 参数只会被 `codeFence`
   自己以 `""` 调用——本文原计划没有预见到这个连带清理点。判断为"删除时顺手做"而非范围蔓延：
   把两个函数合并成一个不带语言标签的 `codeFence`，减少一个此后再也不会被有意义使用的参数。
3. **`render_md_test.go`/`compaction_test.go`/`stitch_test.go` 里有若干断言 fact-layer 具体渲染
   格式的测试用例**（`TestRenderMarkdown_BasicStructure`/`_LLMResponseSection`/
   `_EmbeddedBackticksDontBreakTheFence`、`TestRevision_SpliceEdgeTagsTheReplacedMessage`、
   `TestStitchedJourney_EndToEnd`）：本文 §4.3 第 5 步已经预见到需要"逐个迁移或删除"，但具体
   到每个测试哪些内容已经被脊柱等价覆盖、哪些是可以安全删除的验证点，只能在实现时对照真实渲染
   输出逐一判断——例如决策脊柱的 `toolCallLine` 不显示 `tool_call` 的 `id`（这是 P1.2 就已经
   确定的既有行为，不是本次改动引入的），原测试断言的 `[id=call_1]` 格式因此不能直接照抄成脊柱
   版本的期望值，必须先用真实构造跑一次实际输出再回填断言。`TestRevision_SpliceEdgeTagsTheReplacedMessage`
   的渲染层断言（🔄[revises...] 标记）判断为可以安全删除而不是迁移——它保护的"避免 Splice
   重写的消息读起来像被说了两遍"这个问题，只在 fact-layer 曾经内联全部历史消息时才存在；脊柱从不
   逐条列出 `NewEvents`，这个失效模式随 fact-layer 一起消失，不需要替代断言，数据层的
   `Event.Revises` 断言（前半段）保留即可。

### 8.4 尚待你决定的事项

1. **仓库里那份非我创建的评审文档**（`story_report_p5_action_plan_review_gemini-3.7-flash.md`）
   是否需要处理——本次执行只读取核实了内容，均未改动、未删除，留在仓库里供你查阅。
2. **KNOWN_ISSUES §1.26 收窄后剩下的那一半**（根目录 `vmr-report.md`/`vmr-requests.md` 链接证据
   条目时的相对路径深度）留给 P6 处理，不需要现在处理。
3. 所有代码/文档改动都在工作区，未 `git add`/`git commit`，等待你 review 后决定如何处理。
