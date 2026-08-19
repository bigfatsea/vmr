// Ver 2026-08-19 23:50, by Sonnet 5

# vmr 日志分析体系重构 — P1 ActionPlan（中观叙事层补全）

## 0. 定位

本文是 `docs/future-strategy/story_report_dev_plan_opus-5.md` 里 **P1 阶段**的执行级细化，
基于本仓库 P1 起点的真实代码状态编写（不沿用 DevPlan/架构文档对源码行号的旧引用——已逐一核对）。
架构依据见 `docs/future-strategy/story_report_architecture_opus-5.md` 的 §4.6/§4.7/§4.2/A6/§5/§6，
以及本次 review 对 DevPlan 的四处修订（并行排期措辞、P4.2 常驻检查、设计文档同步、口径规则单点定义）。

**P0 已完成**：`git log` 确认 commit `b098ca9`（"feat(story): refine story report presentation and
exact comparison provenance"）已经落地了架构文档 §1.2 提到的六项工作区改动（system prompt 头部化、
多行/超长参数折叠、长文本折叠不截断、脊柱工具结果渲染的*实现*、compare 溯源精确化、LLM 标题层级的
*prompt 侧*调整）。**本次不需要重做 P0**，但它遗留了两个未完成的收尾，正是 P1 要接手的：

1. 脊柱工具结果渲染的实现已在（`render_spine_step.go`），但底层配对**在主力语料上 0% 命中**
   （根因是客户端改写 ID，见 §2）——这是 P1.1。
2. `KNOWN_ISSUES §1.21` 目前仍记录着错误的根因判断（"opencode 网关改写"），需要 P1.1 完成后改写。

**范围边界**（与 DevPlan 一致）：只动 `internal/chatmsg`/`internal/story` 的叙事渲染与配对逻辑、
对应的 `internal/i18n/story_*.go` 文本、以及 `docs/KNOWN_ISSUES_sonnet-5.md` 的 §1.20/§1.21 两条。
**不动**：坐标层、文件命名、`internal/report`、任何 import 边界。

**通用收尾要求**（DevPlan §2.2，每个任务完成后都要过一遍，本文 §6 统一收口）：
全量测试 + archtest 通过、真实日志肉眼核对、golden 基线更新且 diff 可解释、CHANGELOG 条目、
KNOWN_ISSUES 登记、边界复核。

---

## 1. 执行前置检查

```bash
git status --short                     # 确认工作区干净，P0 那批改动已在 HEAD
go build -o /private/tmp/claude-501/-Volumes-SSD2T-code-vmr/*/scratchpad/vmrbin ./cmd/vmr
go test ./... 2>&1 | tail -30           # 建立改动前的基线：全绿
ls logs/vmr-audit-2026-07-28.jsonl.zst logs/vmr-audit-2026-07-16.jsonl.zst \
   logs/vmr-audit-2026-07-25.jsonl.zst  # 架构文档 §5 用过的样本日志，本机已存在
```

四项任务按 P1.1 → P1.2 → P1.3 → P1.4 顺序做（相互独立，无硬依赖，但 P1.1 影响 P1.2 的验证结果
——脊柱补完后要用真实语料肉眼核对工具结果显示，此时配对最好已经修好）。每做完一项就跑一次
`go test ./internal/story/... ./internal/chatmsg/... ./internal/archtest/...`，不要攒到最后一起查错。

---

## 2. 任务一：工具调用结果三级配对（P1.1）

### 2.1 现状（已读代码确认）

- `internal/chatmsg/pairing.go`（`CheckToolPairing`，F9 不变量校验）与
  `internal/chatmsg/toolresults.go`（`ToolResultList`，内容提取）都只做**同一份 rawMsgs 内部**的
  id 精确匹配，不涉及跨请求 id 改写，**不需要改动**。
- 真正做跨 Step 配对的是 `internal/story/findings_toolresult.go:30` 的 `toolResultsFor(steps, i)`：
  用 `steps[i].ToolCalls[].ID`（来自响应流重组，`chatmsg/sse.go`，**始终带下划线**——改写只发生在
  客户端回写请求历史时）精确匹配 `steps[i+1]` 请求体里 `ToolResultList` 解出的 `CallID`。
  这个函数被三处消费：
  - `internal/story/render_spine_step.go:144`（`renderDecisionSpine` → `renderSpineStep` 的
    `byID` 查找，`tc.ID` 键）——工具结果渲染的直接来源；
  - `findings_toolresult.go` 内的 `detectUnadaptedRetry`（用 `errored[tc.ID]`）、
    `detectUnusedToolResult`、`detectUnverifiedEntityReference`——三个 Finding 检测器。
  **这意味着修好 `toolResultsFor` 会同时修好脊柱显示和三个检测器**，无需分别改动消费方。

### 2.2 目标设计

**三级降级**（架构文档 §5.4–§5.6 已用真实语料验证过安全性，直接照抄结论，不重新验证）：
精确 ID → 去下划线归一化 ID（`strings.ReplaceAll(id, "_", "")`）→ 同 Step 内按出现顺序位置配对。
前两级是精确匹配，可作为 Finding 证据；第三级只能用于渲染，且必须标注"按位置推测"。

**落点**：把 `toolResultsFor` 改成同时做精确匹配和归一化匹配（第 1/2 级），**返回的
`ToolResult.CallID` 重写为命中的那个 `tc.ID` 原值**——这样下游的 `byID[tc.ID]`、
`errored[tc.ID]` 等现有查找代码**一行都不用改**，配对逻辑改动完全收敛在这一个函数内部。

```go
// findings_toolresult.go 内部改法示意（不是最终代码，落地时按实际签名调整）
func normalizeToolCallID(id string) string { return strings.ReplaceAll(id, "_", "") }

func toolResultsFor(steps []*Step, i int) []chatmsg.ToolResult {
    ... // 现有前置判断不变
    ids := make(map[string]string, len(steps[i].ToolCalls))      // 归一化ID -> 原始tc.ID
    exact := make(map[string]bool, len(steps[i].ToolCalls))       // 原始ID是否存在
    for _, tc := range steps[i].ToolCalls {
        exact[tc.ID] = true
        ids[normalizeToolCallID(tc.ID)] = tc.ID
    }
    for _, r := range chatmsg.ToolResultList(...) {
        if exact[r.CallID] {
            out = append(out, r) // 第1级：CallID 已经等于某个 tc.ID，原样保留
            continue
        }
        if orig, ok := ids[normalizeToolCallID(r.CallID)]; ok {
            r.CallID = orig // 第2级：重写为原始 tc.ID，下游按 tc.ID 查找时无感
            out = append(out, r)
        }
    }
    return out
}
```

第 3 级（位置兜底）**只能加在渲染层**，不能混进 `toolResultsFor`（它的输出被三个 Finding
检测器消费，Finding 的证据基础不接受推断）。设计：

- 在 `render_spine_step.go` 的 `renderSpineStep` 里，`byID` 是从（已含第 1/2 级的）
  `toolResultsFor` 结果建的。渲染每个 `tc` 时，若 `byID[tc.ID]` 未命中，尝试第 3 级：
  取下一 Step 请求体里**全部** `chatmsg.ToolResultList` 结果中"既没有精确匹配也没有归一化匹配
  任何一个 tc.ID"的那些（即 `toolResultsFor` 因为完全对不上号而丢弃掉的），按各自在
  `s.ToolCalls`/该结果列表里的出现顺序位置配对——**仅当两边数量相等**（架构文档 §5.5 验证过的
  唯一安全条件；数量不等时不猜，宁可不渲染）。
  这需要一个新的小函数（建议名 `positionalToolResultsFor(steps, i, unmatchedIDs map[string]bool)`
  或类似，具体命名与放置——`findings_toolresult.go` 还是 `render_spine_step.go`——按落地时哪边
  依赖更自然决定；倾向放 `render_spine_step.go`，因为它是纯渲染层关注点，不该让
  `findings_toolresult.go` 承担"这是用于渲染还是用于检测"的分支判断）。
- 命中第 3 级的结果，`toolResultLine` 渲染时追加一个新的 i18n 标注（`SpineText` 新增字段，
  例如 `SpinePositionalMatch string`，ZH `"（按位置推测，ID 未匹配）"` / EN
  `" (matched by position — ID unmatched)"`），紧跟在结果预览之后。

### 2.3 具体步骤

1. `internal/story/findings_toolresult.go`：改造 `toolResultsFor`，加精确+归一化两级匹配，
   命中后重写 `ToolResult.CallID` 为原始 `tc.ID`。加包级测试（新建或扩展
   `findings_toolresult_test.go`）：构造一个"响应侧带下划线、请求回写侧不带下划线"的合成 Step 对，
   断言 `toolResultsFor` 返回非空且 `CallID` 等于原始 `tc.ID`。
2. `internal/story/render_spine_step.go`：加第 3 级位置兜底的小函数 + 在 `renderSpineStep` 里接入；
   `toolResultLine` 加 `positional bool` 参数（或等价方式），命中时附加标注文案。
3. `internal/i18n/story_spine.go`：`SpineText` 加一个字段（EN/ZH 都要写），供 §2.2 的标注复用。
4. `go test ./internal/story/... ./internal/chatmsg/...`：确认新增单测通过，且现有测试
   （尤其 `findings_toolresult_test.go`、`render_spine_test.go`、`invariants_test.go` 的 F9 用例）
   不受影响——F9 的 `CheckToolPairing` 走的是同请求内比较，不涉及本次改动，理论上零影响，
   但仍要跑一遍确认。
5. 真实语料验证（架构文档 §5.4 的对照表是验收基准，不是要重新发现，是要复现）：
   ```bash
   ./vmrbin story -o /tmp/p1verify -journey 'j-openclaw-*' logs/vmr-audit-2026-07-28.jsonl.zst
   ./vmrbin story -o /tmp/p1verify -journey 'j-lobster-*'  logs/vmr-audit-2026-07-28.jsonl.zst
   grep -c '↩️\|❌' /tmp/p1verify/stories/journey-j-openclaw-*.md   # 期望从 0 变为接近 33
   grep -c '↩️\|❌' /tmp/p1verify/stories/journey-j-lobster-*.md
   grep -c '按位置推测' /tmp/p1verify/stories/journey-*.md          # 应为 0 或极少——07-28 这批
                                                                    # 是归一化能 100% 解决的那批
   ```
   （若某条 Journey 上归一化后仍有残差，对照架构文档 §5.4 的"07-16/08-14/08-16 未达 100%"
   说明——那是跨文件集合比较的自然残差，不是本次改动的 bug，不需要为此再加逻辑。）

### 2.4 KNOWN_ISSUES 收口

改写 `docs/KNOWN_ISSUES_sonnet-5.md` 的 `### 1.21` 一节：
- 标题与"现状"改为：根因是 **OpenClaw 家族客户端在回写工具调用历史时去掉了下划线**
  （部分链路自造 `name+timestamp` 形态 id），与上游 provider/网关无关——附架构文档 §5.3 的
  按 `client_key_tag` 分组对照表要点（hermes/pimini 两侧 100% 一致，lobster/openclaw 请求侧 0%
  带下划线）。
- "为什么不现在修"整段删除，替换为"已按三级降级修复：精确 ID → 归一化 ID → 位置兜底（仅渲染层，
  已标注），见 P1 ActionPlan"。
- 若条目全部解决且没有残留风险，可以整节移除或降级为"已闭环"记录（是否保留取决于团队对
  KNOWN_ISSUES 存留已解决条目的惯例——检查文件里其它条目是否有"已解决"归档区，照此处理）。

---

## 3. 任务二：脊柱覆盖补完（P1.2）

### 3.1 现状（已读代码确认，`internal/story/render_spine_step.go:99-147`）

`renderDecisionSpine` 当前有三处会让内容"整段消失"，均已在源码里逐行核实：

1. 第 120-129 行：`anyCalls` 扫描整个 Journey，若没有任何 Step 有 `ToolCalls`，直接
   `return`——纯问答 Journey 完全没有决策脊柱。
2. 第 133-141 行：每个 Task 内部先过滤出 `acting`（有 `ToolCalls` 的 Step），
   `len(acting) == 0` 时 `continue`——**整个 Task 消失**，即使它有 Step 说了话（`RespText`）。
3. 第 143-145 行：即使 Task 有 `acting` Step，`acting` 里也只挑了有 `ToolCalls` 的 Step——
   任何 `HumanInitiated` 但恰好没触发 `ToolCalls` 的中途追加指令，或纯汇报 Step，都不出现。

`idxOf`/`repeat`/`hit` 三个 map（第 108-118、131 行附近）已经是基于 `journeySteps(j)`
（**全部** Step，不是过滤后的）建的——**这部分不用改**，改动完全收敛在渲染循环本身。

### 3.2 目标设计

三处改动对应架构文档 §7.4(a) 的三个补齐点，逐一映射到本仓库的实际结构：

**(a) 移除两处提前退出**：`anyCalls` 检查改为"即使整个 Journey 没有 ToolCalls，
也渲染 `t.SpineTitle` + 每个 Task 的单行内容"；`len(acting) == 0 { continue }` 同理移除，
改为渲染 Task 行 + 该 Task 每个 Step 的内容（不再要求 Step 必须有 ToolCalls）。

**(b) 每个 Step 都渲染，按类型分流**（在原 `acting` 循环位置，改为遍历 `task.Steps` 全部）：

| Step 情形 | 判据（已有字段，无需新计算） | 渲染形态 |
| --- | --- | --- |
| 有 `ToolCalls` | `len(s.ToolCalls) > 0` | 现有 `renderSpineStep` 全量块（why + 工具调用 + 结果），不变 |
| 任务中途追加指令 | `s.HumanInitiated == true` 且该 Step **不是**它所在 Task 的第一个 Step（Task 自己的第一个 Step 已经通过 Task 标题体现指令，不重复渲染） | 新的单行："💬 指令 · `<原话>`"（见下方文本来源） |
| 纯汇报/观察 | 上面两种都不是，但 `s.RespText != ""` 或以上都不是 | 新的单行："💬 汇报 · `<RespText 预览>`" |

**指令文本来源**：不新增 `Step` 字段（避免扩大 `Journey`/`Step` 结构体，`journeySteps`/`journey.go`
的构造已经很复杂）。改为在渲染时扫描 `s.NewEvents`，取第一个 `Msg.Role == "user"` 的
`Msg.Text`——`NewEvents` 已经是"该 Step 首次引入的事件"，`HumanInitiated == true` 的 Step
其 `NewEvents` 里必然包含那条新用户消息（`journey.go:379` 附近 `hasNewInstr` 与 `NewEvents`
的构造是同一遍扫描的产物）。**落地前用真实日志核实这个假设**（见 §3.3 第 3 步），
如果扫描不到，退回方案是在 `journey.go` 给 `Step` 加一个轻量字段——但优先尝试不加字段的路径。

**单行渲染不套用 `foldWhyLine` 的折叠惯例**——那是给 Step 自己的主要 why-line 用的，
这里是脊柱里的次要提示行，用 `oneLineTruncate`（已存在，`render_spine.go:118`）截断到一个
合理长度（建议复用 `spineWhyRespCap` 或专门定一个更短的常量，如 120 字符）即可，不必再套
`<details>` 一次——这是一处需要在实现时确认的排版判断，若真实语料显示常有超长指令导致单行观感差，
再补折叠。

**(c)最终交付物一节**（架构文档 §7.4a 第三项）：**先厘清一个容易混淆的点**——架构文档举的样例
（Step 22"没有工具调用、只有最终报告正文"）在补完 (b) 之后**已经**能通过"💬 汇报"单行浮现出来，
不再整段消失。§7.4a 描述的"最终交付物"是在此之上的**加强展示**：对于确实以工具调用形式写文件的
Journey（如 `write_file(path=..., content=...)` 收尾），在脊柱最后单独起一节，给它比一行摘要更多
的展示空间（完整节选，而不是被截断的一行）。

实现：脊柱渲染完所有 Task 后，调用 `deliverableStats(j)`（已存在，`compare.go:594`，纯函数，
`story` 包内直接可调，无需跨包）。若 `Found == true`，渲染一个新小节（复用
`compare.go` 已有的 `truncateText`/`renderExcerpt` 或为脊柱单独写一个更简形态——`compare` 那边的
`renderExcerpt` 签名吃的是 `i18n.CompareText`，脊柱这边应该在 `i18n.SpineText` 加对应字段，
不要跨结构体去借用 `CompareText` 的方法，两个渲染层的文本表保持独立）。
`Found == false`（大多数 Journey 不是靠工具调用收尾）时**不渲染这一节**——不是每个 Journey
都有"文件交付物"，(b) 的汇报单行已经覆盖了"最后一步说了什么"这个更通用的问题，这一节只是锦上添花。

### 3.3 具体步骤

1. `internal/i18n/story_spine.go`：`SpineText` 新增（EN/ZH 都写）：
   `SpineInstructionLine func(text string) string`、`SpineReportLine func(text string) string`、
   `SpineFinalDeliverableTitle string`、`SpineFinalDeliverableFound func(stepSeq int, toolName string) string`。
2. `internal/story/render_spine_step.go`：`renderDecisionSpine` 去掉 `anyCalls` 提前返回；
   Task 循环去掉 `acting` 过滤，改为遍历 `task.Steps` 全部；按 §3.2(b) 的判据分流到
   `renderSpineStep`（原有全量块）或新的单行渲染函数。**注意**：`renderSpineStep` 内部读取
   `toolResultsFor(steps, idxOf[s])` 时 `idxOf[s]` 依赖 `s` 在全局 `steps` 里的下标——分流后
   仍要保证每个 Step（不只是 acting 的）都能算出正确的 `idxOf[s]`，这一点在现有代码里已经成立
   （`idxOf` 建立时遍历的是全部 `steps`），改动时不要把这层去掉。
3. 新增 `renderFinalDeliverable(w, j, t)`（可放 `render_spine.go` 或新文件，看行数预算
   ——`render_spine.go` 目前 278/380，加这一节大概率仍在预算内；`render_spine_step.go` 172/700
   预算更宽松，也可以放这里），末尾调用点加在 `renderDecisionSpine` 循环结束之后。
4. `go test ./internal/story/...`：跑通已有测试，预期 golden 基线会变（`UPDATE_GOLDEN=1`，
   见 §6）。
5. 真实语料核实（架构文档 §2.2 的样例 Journey，22 步 33 次调用）：
   ```bash
   ./vmrbin story -o /tmp/p1verify -journey 'j-openclaw-20260728T000544*' logs/vmr-audit-2026-07-28.jsonl.zst
   grep -c '^\*\*.*Step ' /tmp/p1verify/stories/journey-j-openclaw-20260728T000544*.md   # 期望 22（不是 21）
   ```
   同时挑一条纯问答 Journey（`vmr story` 列表里轮数很少、`ToolCallCount` 为 0 的样例——可以先跑
   `./vmrbin story -o /tmp/p1verify logs/vmr-audit-2026-07-28.jsonl.zst` 看候选列表 JSON 里
   `tool_call_count` 找一条），确认脊柱不再整节消失（至少有 `SpineTitle` + 若干 Task 行 + 单行内容）。
   再挑一条真的以文件写入收尾的 Journey（若语料里存在），确认"最终交付物"节渲染且非空；
   若语料里找不到这类样例，如实在 PR/提交说明里注明"未在真实语料上覆盖到这条路径，靠单元测试兜底"。

---

## 4. 任务三：Compare 开篇初始指令（P1.3）

### 4.1 现状（已读代码确认）

- `internal/story/compare.go:166-179`：`JourneyRef`（`Comparison.A`/`.B`）只从 `JourneySummary`
  构建（`journeyRef` 函数），**没有**、也**不该**接触完整消息正文——`JourneySummary` 本来就是
  "不含任何消息正文"的机读契约（架构文档 §2.2 已验证）。所以初始指令这段全文**不能**挂在
  `JourneyRef` 上。
- `ComputeComparisonExtras(jA, jB *Journey, ma, mb Metrics) ComparisonExtras`
  （`compare.go:405`）**已经**拿到完整 `*Journey`（`jA`/`jB`），`cmd/vmr/cmd_story.go:451-452`
  的调用点两者都在作用域内——这是唯一该挂初始指令的地方。
- `ComparisonExtras` 结构体在 `compare.go:274-296`，已有 `Deliverable DeliverableFact` 这个同类型
  的"单侧统计 + A/B 打包"先例，直接照抄这个模式。
- 渲染入口 `RenderComparisonMarkdown`（`render_compare.go:21`）在两行 `SideBlock` 之后
  （第 27-29 行）就是架构文档要求的"开篇"位置；`cmp.Extras` 在这里可能为 nil
  （某些单测直接构造 `Comparison{}` 不填 Extras），渲染时必须判空。
- `renderExcerpt(w, summary, text string, truncated bool, t i18n.CompareText)`
  （`render_compare.go:228`）已经是"折叠块 + 截断标注"的现成实现，`Deliverable`/`SysPrompt`
  两节都在用，直接复用，不要重新发明。
- `EvidencePack.Comparison` 字段（`llm.go:127`）整体序列化 `cmp`（含 `cmp.Extras`）进证据包，
  **新字段只要挂在 `ComparisonExtras` 上就自动进了 `-llm-addr` 的证据包，`llm.go` 不需要改一行**。

### 4.2 目标设计

架构文档 §4.7 已定案的三点，逐一对应实现：

1. **设上限**：2000 字符（架构文档明确给出的数字），用 `compare.go:631` 已有的 `truncateText`。
2. **放在哪**：`SideBlock` 短摘要下面新增一个折叠块，不替换现有摘要。
3. **进不进 JSON**：进——通过挂在 `ComparisonExtras` 上自动达成，见上面对 `EvidencePack` 的分析。

**文本来源**：`Journey.Events`（全局去重、首次出现顺序的事件流）里第一个 `Msg.Role == "user"`
的 `Msg.Text`，未截断原文——`Journey.Events` 就是 KNOWN_ISSUES §1.20 提到的"其实已经在内存里"的
那份数据，不需要重新解析请求体。

### 4.3 具体步骤

1. `compare.go`：新增
   ```go
   type InitialInstructionStats struct {
       Text      string `json:"text"`
       Truncated bool   `json:"truncated"`
   }
   type InitialInstructionFact struct {
       A InitialInstructionStats `json:"a"`
       B InitialInstructionStats `json:"b"`
   }
   ```
   `ComparisonExtras` 加一个字段 `InitialInstruction InitialInstructionFact \`json:"initial_instruction"\``。
   新增 `initialInstructionStats(j *Journey) InitialInstructionStats`：遍历 `j.Events`，
   找第一个 `Role == "user"` 的 `Msg.Text`，过 `truncateText(text, 2000)`。
   `ComputeComparisonExtras` 里加一行 `InitialInstruction: InitialInstructionFact{A: initialInstructionStats(jA), B: initialInstructionStats(jB)}`。
   **留意 `compare.go` 当前 778/850 行的 archtest 预算**——这批新增预计 30-40 行，应该够用，
   但落地后务必跑一次 `go test ./internal/archtest/...` 确认没有顶到预算；顶到了就把
   `InitialInstructionStats`/`initialInstructionStats` 挪到一个新文件（如
   `compare_initial_instruction.go`），不要为了塞进一个文件而硬缩注释。
2. `internal/i18n/story_compare.go`：`CompareText` 加
   `InitialInstructionTitle string`（或直接复用 `renderExcerpt` 的 `summary` 参数不加专门 Title，
   看现有 `Deliverable`/`SysPrompt` 两节是否各有独立 Title 再决定风格一致性）、
   `InitialInstructionExcerptLabel func(side string) string`（照抄
   `DeliverableExcerptLabel` 的写法，`side + " 的初始指令"` / `side + "'s initial instruction"`）。
3. `render_compare.go`：`RenderComparisonMarkdown` 在两行 `SideBlock`（第 27-29 行）之后、
   `w("%s", t.ProfileTitle)` 之前插入：
   ```go
   if cmp.Extras != nil {
       renderInitialInstruction(w, cmp.Extras.InitialInstruction, t)
   }
   ```
   新增 `renderInitialInstruction`，仿照 `renderDeliverable`/`renderDeliverableSide`
   （`render_compare.go:213-226`）的双侧循环写法，内部调用 `renderExcerpt`。
4. `go test ./internal/story/...`：`compare_test.go` 大概率需要补一个新用例（两个 Journey 各自的
   `j.Events` 里放一条 user 消息，断言 `Comparison.Extras.InitialInstruction.A/B.Text` 命中）；
   golden/固定输出类测试若断言了 Markdown 全文，需要 `UPDATE_GOLDEN=1` 重跑并肉眼核对新增块。
5. 真实语料验证：
   ```bash
   ./vmrbin story -o /tmp/p1verify -compare <id1>,<id2> -llm-dry-run -llm-addr x:1 -llm-model x \
       logs/vmr-audit-2026-07-28.jsonl.zst
   grep -A3 "初始指令\|initial instruction" /tmp/p1verify/stories/compare-*.md
   python3 -c "import json;d=json.load(open('/tmp/p1verify/stories/compare-<id1>-<id2>.json'));print(d['extras']['initial_instruction'])"
   ```
   （`<id1>,<id2>` 换成 `vmr story` 列表里挑的两条真实 Journey id；`-llm-dry-run` 只是为了顺带看一眼
   证据包大小估算有没有明显异常，不是这个任务的验收点。）

### 4.4 KNOWN_ISSUES 收口

`docs/KNOWN_ISSUES_sonnet-5.md` 的 `### 1.20` 整节移除（三个"待拍板"都已按架构文档 §4.7 定案并
实现），或按仓库对已解决条目的惯例归档。

---

## 5. 任务四：LLM 解读小节标题层级兜底（P1.4）

### 5.1 现状（已读代码确认）

- Prompt 侧的调整已在 P0（commit `b098ca9`）落地：`internal/i18n/story_llm.go` 已把 LLM 输出的
  子标题指令改成三级标题。**这不保证模型 100% 遵守**，架构文档 §4.2 的结论是"文档结构不该外包给
  LLM 的指令遵从度"，渲染层需要一道确定性兜底。
- `RenderLLMSection`（`llm.go:403-413`）目前把 `res.Text`（LLM 原始返回）原样
  `b.WriteString(res.Text)`，没有任何后处理。
- 缓存写入发生在 `Interpret()` 内部（`llm.go` 第 383-387 行附近），**在 `RenderLLMSection` 之前**、
  写的是**未降级**的原始文本——也就是说降级处理天然只应该发生在渲染这一步，**不要**把降级搬到
  `Interpret()` 里去（那样会把降级后的文本当成"权威结果"存进缓存，下次改降级规则时旧缓存已经
  不可逆地损失了原文）。

### 5.2 目标设计

在 `RenderLLMSection` 写入 `res.Text` 之前，跑一次确定性的标题降级：逐行扫描，用一个 `inFence
bool` 状态跟踪是否在围栏代码块内（trim 后以 `` ``` `` 开头的行切换状态，不管语言标签），
只对**不在围栏内**、行首匹配 `## `（恰好两个 `#` 加一个空格）的行，改写成 `### `。
不处理 `#`/`#### ` 等其它层级——架构文档描述的具体故障是"LLM 输出的 `## 候选根因`"，只需要精确
处理这一种。

### 5.3 具体步骤

1. `internal/story/llm.go`：新增 `downgradeH2ToH3(text string) string`（纯函数，无 i18n 依赖，
   处理的是 Markdown 语法层面的东西，不是语言相关文本）。`RenderLLMSection` 里
   `b.WriteString(res.Text)` 前加一行 `text := downgradeH2ToH3(res.Text)`，改写为
   `b.WriteString(text)`。
2. 新增/扩展 `llm_test.go`（或新文件 `llm_heading_test.go`）：三个用例——
   (a) 围栏外的 `## X` → `### X`；
   (b) 围栏内的 ` ```\n## X\n``` ` 原样保留（不降级）；
   (c) 混合场景（围栏前后各一个 `## `，只有围栏外的那个被改）。
3. `go test ./internal/story/...`：确认新测试通过，且不影响 `llm_test.go` 里已有的
   `RenderLLMSection` 快照类断言（若有，同步更新）。
4. 真实/合成验证：找一份已缓存的 `-llm-addr` 解读结果（若本机 `reports/stories/.llm-cache/`
   下有历史缓存文件），或手工构造一个"模型没有遵守三级标题指令、返回了 `## 候选根因`"的
   `InterpretResult{Text: ...}`，跑 `RenderLLMSection` 确认输出里是 `### 候选根因`
   且目录结构（若报告有 TOC 生成逻辑）正确嵌套在 `## LLM 解读` 之下，而不是并列。

---

## 6. 收尾（四项任务共用）

1. **Golden 基线更新**：
   ```bash
   UPDATE_GOLDEN=1 go test ./internal/story/... -run TestGolden
   git diff internal/story/testdata/golden.md internal/story/testdata/golden_zh.md
   ```
   逐行核对 diff——每一处变化都应该能对应到 P1.1-P1.4 里的某一项，对不上的改动说明动到了范围外的
   东西，要退回重做。`compare_test.go`/其它含内嵌期望字符串的测试若失败，按测试自身的更新方式处理
   （非 golden 机制的，手改断言字符串）。
2. **全量测试与架构边界**：
   ```bash
   go test ./... -race
   go test ./internal/archtest/...
   ```
3. **CHANGELOG.md**：在 `[Unreleased]` 下按 Added/Changed/Fixed 分类各加条目，例如
   （具体措辞落地时按实际改动定，不要照抄）：
   - Fixed: 决策脊柱工具调用结果配对不再对 OpenClaw 家族客户端（id 被去下划线）静默失效；
     ID 不匹配时按位置推测并标注来源。
   - Fixed: 决策脊柱不再跳过无工具调用的 Step 或纯问答 Journey；新增最终交付物小节。
   - Added: Compare 报告开篇展示两侧完整初始用户指令（有界折叠），并纳入 LLM 解读证据包。
   - Fixed: LLM 解读小节的标题层级由渲染层确定性兜底，不再依赖模型指令遵从度。
4. **KNOWN_ISSUES_sonnet-5.md**：按 §2.4、§4.4 完成 1.20/1.21 的收口；检查文件顶部或附录是否有
   汇总表/索引需要同步移除这两条的引用。
5. **架构文档同步**（DevPlan 完成定义第 4 条新增的要求）：`story_report_architecture_opus-5.md`
   §2.2 的实测基线表（工具结果 0 条、脊柱 21/22 步等数字）在 P1 完成后已经过时——不需要重写整份
   架构文档，但应在该文档或 DevPlan 里补一条简短说明"P1 已完成，§2.2/§6 的相关数字是修复前基线"，
   避免下一个阶段的执行者误把旧数字当成当前状态。若 `docs/VirtualModelRouter_Design_v4_Analytics.md`
   有对应旧描述（如"脊柱不展示工具结果"一类的现状描述），一并核对更新。
6. **边界复核**（DevPlan §2.2 第 6 条，三个问题）：
   - 本阶段是否产生了架构文档未预见的事实？—— 重点检查 §3.2(c) 与架构文档 §7.4a 字面表述的
     出入（"最终交付物"检测机制 vs. 样例 Step 22 实际是无工具调用文本）是否需要回写进架构文档，
     避免下一个读者困惑。
   - 本阶段是否改变了 P2 及以后的前提？—— 预期不改变（P1 明确不涉及坐标/命名/包结构），
     但如果 §3.2(b) 落地时发现需要给 `Step` 加字段（`NewEvents` 扫描假设不成立的退路），
     记录下来，供 P2 设计坐标契约时留意 `Step` 结构体是否会被后续阶段进一步扩展。
   - 本阶段是否暴露出某个原计划任务其实不必要？—— 留空，按实际执行情况填。

---

## 7. 验收清单（对照 DevPlan P1 的验收标准逐项勾）

- [x] 主力客户端（openclaw/lobster）样例任务，工具结果从完全不显示恢复到接近全覆盖；
      位置兜底条目在报告中可识别；Finding 检测器证据基础（`toolResultsFor` 输出）未引入推断。
- [x] 样例任务脊柱步骤覆盖率达到全覆盖（22/22）；纯问答任务也产出脊柱而不是整节消失。
- [x] Compare 报告两侧初始指令可展开查看；证据包体积增量在预期范围内（可用 `-llm-dry-run` 的
      估算数字对比改动前后）。
- [x] 即使模型不遵守层级指令，LLM 解读小节目录结构仍然正确；代码块内的 `#` 不受影响。
- [x] `go test ./...`、`go test ./internal/archtest/...` 全绿；golden 基线更新且 diff 可解释。
- [x] CHANGELOG、KNOWN_ISSUES（§1.20/§1.21，执行中改写为 §1.20/§1.21/§1.22）、
      架构文档说明性备注 均已同步（细节见 §8）。

---

## 8. 执行记录（2026-08-19，Sonnet 5）

本节是本文写完 ActionPlan 之后、实际落地执行的过程记录与总结——按用户要求补写，不是提前写好
的计划。**所有改动均未提交，等待人工 review。**

### 8.1 执行顺序与整体结果

按 §2–§5 的 P1.1 → P1.2 → P1.3 → P1.4 顺序实现，每项做完立即 `go build` + 相关包测试，
最后统一跑 `go test ./... -race`、`go vet ./...`、`gofmt -l .`、`go test ./internal/archtest/...`，
全部通过（唯一的中间失败是预期内的 golden 基线漂移，`UPDATE_GOLDEN=1` 重跑后逐行核对，
每一行改动都能对应回 P1.2 的具体改动，无法解释的多余 diff 为零）。

用本机真实审计日志验证（`logs/vmr-audit-2026-07-28.jsonl.zst`，架构文档 §5 用过的同一批样本）：

| 验证项 | 结果 |
| --- | --- |
| openclaw 33 次工具调用配对 | 33/33（此前 0/33），0 条位置兜底 |
| lobster 64 次工具调用配对 | 64/64（此前 0/64），0 条位置兜底 |
| openclaw 样例脊柱 Step 覆盖 | 22/22（此前 21/22），末尾出现"最终交付物"节，内容是 Step 21 `write` 调用的报告正文 |
| Compare 初始指令 | 两侧全文正确展开，与证据包（`-llm-dry-run` 69,936 字符）的增量占比 <1.1% |

### 8.2 一处需要澄清的地方（已在 §3.2(c) 写明，执行时确认无误）

架构文档 §7.4a 把"最终交付物"描述为复用 `deliverableStats`（工具调用形态检测），但样例 Journey
实际缺失的 Step 22 是"无工具调用、纯文本收尾"——两者初看矛盾。执行确认：P1.2 的 (b)（每个 Step
都渲染，无工具调用的降级为一行摘要）已经独立解决了"Step 22 消失"这个具体问题；(c) 的"最终交付物"
节是**另一个、锦上添花的加强**，只对以文件写入收尾的 Journey 生效。两者不重叠、不矛盾，§3.2(c)
的说明在实现前就已经写清楚，实现时未发现需要修正的地方。

### 8.3 执行期间发现的外部并发活动，及独立代码审阅的处理

执行过程中，仓库里出现了两份我没有创建的新文件（`docs/future-strategy/
cli_architecture_redesign_gemini-3.7-flash.md` 与 `docs/future-strategy/
story_report_p1_action_plan_review_gemini-3.7-flash.md`）——`/Users/stanford/code/vmr` 与
`/Volumes/SSD2T/code/vmr` 是同一仓库（符号链接确认），说明有另一个会话在并发工作，其中一份是对
本文档与本次落地代码的独立源码级审阅，列出 8 条发现（F1–F8）。已经通读全文并逐条核实（不是照单
全收，也不是因为"是审阅意见"就默认正确）：

**核实为真、已修复的三条**（均已补充回归测试）：

- **F1（高严重度，已修复）**：`positionalToolResults`（位置兜底）原实现用
  `chatmsg.RawArray(body)` 扫描了 `steps[i+1]` 请求体的**全部**历史消息，而不是该 Step 自己新增的
  那一段——chat 类 API 每轮都会带上完整历史，未加范围限制的话，更早 Step 已经解决过的历史工具结果
  会被错误地当成"剩余待配对项"计入 leftover 计数。这既会导致位置兜底在多轮对话里几乎永远因计数
  不符而失效（假阴性），也存在计数偶然吻合时把历史结果错误安在当前 Step 头上的风险（假阳性，更
  严重）。复核后确认这是真实缺陷，修法：用 `Step.DeltaStart`（该 Step 自己新增内容的起始位置）
  加 `chatmsg.MsgOffset` 的下标换算，把扫描范围收紧到该 Step 自己新增的那一段。补了两个回归测试：
  一个证明"历史里恰好剩 1 条、当前 Step 也恰好缺 1 条"时不会误配（假阳性场景的直接复现），
  一个证明限定范围后仍能正确找到真正属于当前 Step 的新增结果。
- **F3（中高严重度，已修复）**：Compare 初始指令原实现直接扫 `j.Events` 里第一个
  `Role=="user"` 的事件，没有经过 `taskseg.Profile` 的方言过滤——而 Journey/Task 标题
  （`deriveTitle`）恰恰是经过方言过滤的（`taskseg.FirstInstruction`）。对 OpenClaw 家族客户端，
  如果真实指令前面混入了一条同样标记为 `role=user` 的脚手架消息（如工具结果图片附件提示），
  两条逻辑会给出不一致的答案——摘要标题显示过滤后的真实指令，展开的"完整原文"却可能是脚手架噪声，
  自相矛盾。修法：给 `initialInstructionStats`/`ComputeComparisonExtras` 加一个 `taskseg.Profile`
  参数（`cmd_story.go` 的 `compareJourneys` 本来就有，两侧 Journey 也是用同一个 Profile
  构建的），改用 `prof.RealUserText`（`taskseg.FirstInstruction` 底层同一个断言函数，只是不经过
  它的 `Preview()` 截断）在 Journey 第一个 Step 自己的消息列表里找真实指令，而不是裸扫全局事件流。
- **F4（中严重度，已修复）**：LLM 标题层级兜底原实现只把 `## ` 单点改写成 `### `——如果模型自己在
  内部正确地做了两层嵌套（如 `## 1.` 下面嵌 `### 1.1`），改写后两者都变成 `### `，父子关系被拍平
  成同级标题。修法：改成对围栏外每一个 2–5 级 ATX 标题统一整体下移一级（`## → ###`、
  `### → ####`……到 6 级封顶），保留模型自己搭的内部层级；同时补上对 `~~~` 围栏的识别
  （原实现只认 ``` ` ``` ` ``` ```）。补了 7 个测试用例，包括"父子层级在下移后仍保持父子关系"
  这条最容易被忽略的场景。

**核实为真、判断不在 P1 范围内、已登记为独立 KNOWN_ISSUES 条目的两条**：

- **F5/F8（低严重度，合并登记为 §1.21）**：决策脊柱的指令展示（任务开篇 Step 的 80 字符标题、
  中途追加指令的 `firstNewUserText`）没有复用 F3 修复里验证过的方言过滤模式。判断依据：影响面
  有界（渲染层一行预览，不影响 Finding 证据或落盘数据），但要把 `taskseg.Profile` 串到
  `RenderMarkdown`→`renderDecisionSpine`→`renderSpineBriefStep` 整条调用链上，改动面明显大于
  F3——这条链路今天完全不接收 Profile。留给下一次touch这条链路时按 F3 已验证的模式（`prof.
  RealUserText`）一并处理，不在本次顺手做。
- **F6（低严重度，登记为 §1.22）**：`chatmsg.ToolResultList`/`ToolCallList` 不支持 OpenAI
  Responses API 的 `function_call`/`function_call_output` 形状——这是一个更早就存在的协议覆盖
  缺口，与 P1.1 修的 ID 改写问题是两件事。P1.1 的范围是修已支持的两种形状（Chat Completions、
  Anthropic）里的配对 bug，不是扩展协议覆盖；是否值得投入需要先看 Responses API 流量在真实语料
  里的占比，本次不展开。

**核实后判断已经处理妥当、无需改动的一条**：

- **F7**：文本型长交付物在脊柱里只有一行摘要，没有独立"交付物小节"——审阅意见自己的结论也是
  "这需要在文档里说清楚是设计如此，不是遗漏"，而 §3.2(c) 在实现之前就已经写清楚了这一点
  （见 §8.2）。无需改代码。

**F2**（KNOWN_ISSUES 引用已删除文件导致 archtest 失败）在审阅文档写成时其实已经在本次执行的
KNOWN_ISSUES 收口步骤里处理完——审阅基于的是我收口之前的中间状态，属于时序上的并发误差，
不是遗漏。

两份外部文档本身未删除、未修改，留在仓库里供你查阅；`cli_architecture_redesign_gemini-3.7-flash.md`
是另一个话题，本次执行未读取也未处理。

### 8.4 与最初 ActionPlan 设计的实际出入

- `normalizeToolCallID`/`positionalToolResults` 最终都落在 `internal/story`（前者在
  `findings_toolresult.go`，后者在 `render_spine_step.go`），没有下沉到 `chatmsg`——因为
  `chatmsg` 只提供解码原语，实际做"用哪个 ID 配对"判断的逻辑本来就在 `story` 包里
  （`toolResultsFor`），加一层跨包调用没有收益。
- `ComputeComparisonExtras` 因 F3 修复新增了 `prof taskseg.Profile` 参数——这是 ActionPlan 原文
  没有预见到的一处签名变化，影响了 `cmd_story.go` 一处调用与 4 处测试调用，均已同步更新。
- `downgradeH2Headings` 因 F4 修复重命名为 `downgradeHeadingLevels`（行为从"单点改写"变成"整体
  下移"，函数名也应该反映这一点）。

### 8.5 尚待你决定的事项

1. **是否需要我恢复或处理**仓库里那两份非我创建的文档（`cli_architecture_redesign_
   gemini-3.7-flash.md`、`story_report_p1_action_plan_review_gemini-3.7-flash.md`）——本次执行
   只读取核实了后者的内容，均未改动。
2. **KNOWN_ISSUES §1.21/§1.22 的后续**：这两条是本次审阅带来的新登记，不是 P1 原计划范围，
   下次有精力时再排期，不需要现在处理。
3. 所有代码/文档改动都在工作区，未 `git add`/`git commit`，等待你 review 后决定如何处理。
