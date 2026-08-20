// Ver 2026-08-20 00:00, by Sonnet 5

# vmr 日志分析体系重构 — P4 ActionPlan（中观机读层归位）

## 0. 定位

本文是 `docs/future-strategy/story_report_dev_plan_opus-5.md` 里 **P4 阶段**的执行级细化，基于
本仓库 P4 起点（P3 已完成，见 `docs/future-strategy/story_report_p3_action_plan_sonnet-5.md`）的
真实代码状态编写。架构依据见 `docs/future-strategy/story_report_architecture_opus-5.md` 的
§7.4(b)（事件流归位：补结构，不搬正文）、§7.6(a)（detail 减法后的坐标基础）、§8 决策表对应各行。

**DevPlan review 结论**（本次 review 的产出）：P4–P6 的任务范围、边界、验收标准在当前代码库上
逐条核实后**均仍然成立，不需要改动 DevPlan 正文的任务表**。已在 DevPlan 里补齐的两处更新：
P3 状态标记为 ✅（含"个位数秒"目标部分达成的如实登记）、P6.5 补一句范围收窄说明（`vmr replay`
一侧的读取原语/统一记录选择器已由 P3 提前交付）。这两处都不影响 P4 本身的范围。

**P4 范围边界**（与 DevPlan 一致）：`journey-<id>.json`（`JourneySummary`）的机读结构与引用规则。
**不改变**任何人读产物（`journey-<id>.md` 的决策脊柱、fact-layer 渲染逻辑不动，那是 P5 的工作）；
**不删除** fact-layer（`render_md.go` 的 `renderStep`/`renderEvent` 系列函数）——P4 只是把它的等价
内容也发布到机读层，删除渲染是 P5.1 的工作，依赖本阶段建立的"无损重建检验"作为验收对照。

**已读代码确认的关键前提**（P4 直接复用，不重新设计）：

- `journey-<id>.json`（`JourneySummary`，`internal/story/metrics.go:399-408`）今天只有
  `ID/Title/From/To/Partial/Metrics/Findings/LLMFindings`，**不含任何 Task/Step/Event 结构**——
  这正是架构文档 §2.2 诊断的"中观×机读是空的"，P3 完成后这个事实没有变化，P4 要补的就是这一格。
- 内存里的 `Journey`/`Task`/`Step`/`Event` 结构（`journey.go:39-155`）**已经具备 P4.1 要求的全部
  信息**：`Step.Manifest.Req`（P2 已发布的请求级坐标；生产路径上 `Step.Manifest` 由构造保证非
  nil，`render_spine_step.go:170`、`metrics.go:161-162` 均不判空直接用——但这只是生产路径的事实，
  不是"全仓无判空先例"的断言，测试夹具可以手工构造出 `Manifest` 为 nil 的 `Step`，落地时仍要判空
  防御，见 §2.3）、`Step.ToolCalls`/
  `RespText`/`Reasoning`/`Finish`/`NoReply`/`HumanInitiated`/`SysChanged`/`Compaction`/
  `StitchEdge`，`Event.Hash`（`ctxgraph.Hash`，已有 `MarshalJSON` 渲染为十六进制字符串，
  `ctxgraph/hash.go:33-44`）、`Event.FirstStepSeq`/`Revises`。**P4.1 是"组装 + 定边界 + 发布"，
  不是"计算新事实"**。
- `j.Events`（Journey 级全局去重事件流）**在构造时机就是**每个 Step 的 `NewEvents` 按 Step 顺序
  拼接的结果（`journey.go` 的 `appendNewEvents`：`step.NewEvents = append(...); j.Events =
  append(...)`，同一次循环写两份）——**JSON 结构不需要单独再发布一个顶层 `events` 数组**，
  按 Task→Step→NewEvents 展开就是完整的、顺序一致的全局事件流，重复发布违反项目"不重复存"的原则。
- 工具调用结果的配对权威实现是 `findings_toolresult.go:50` 的 `toolResultsFor(steps, i)`
  （P1.1 已修复，exact + 归一化两级精确匹配，返回值的 `CallID` 已重写为原始 `tc.ID`）——
  `render_spine_step.go:183-203` 的 `renderSpineStep` 已经示范了标准用法（`byID` map）。
  **P4.1 的 JSON 结构直接复用这一个函数**，且**只用它**——`positionalToolResults`
  （`render_spine_step.go:213`，位置兜底，注意不在 `findings_toolresult.go`）是渲染层专用的推断，
  不进入机读结构（架构文档 §5.6 已定案：位置配对不作为 Finding 或结构化数据的证据基础）。
- `truncateText(s string, maxChars int) (string, bool)`（`compare.go:699-706`）与
  `initialInstructionExcerptChars = 2000`（`compare.go:266-270`）已经是项目里"有界摘录 + 截断
  标注"的既有实现与既有上限惯例（P1.3 定案），P4.1 的内联字段直接复用，不新造一套截断规则。
- `internal/story` 现有测试基础设施里，`corpus_test.go:88` 的 `buildTestJourney(t, n,
  injectFinding)` 通过写真实 JSONL + 调用 `Build(...)` 构造一个真实的、`Step.Manifest.Req`
  可用的合成 Journey——P4.1/P4.2 的测试直接复用或仿照它构造夹具，不需要另起一套。
- 行数预算（`internal/archtest/file_sizes_test.go`）：`journey.go` 697/850（154 行余量）、
  `metrics.go` 424/470（46 行余量）——**均不足以容纳新结构体+构造函数**，P4.1 的新类型与构造
  函数必须落在一个新文件（默认预算 700 行，见 `defaultFileLineLimit`），只在 `metrics.go` 里给
  `JourneySummary` 加一个字段（1 行）和 `Summarize` 里加一行赋值。
- `EvidencePack`（`llm.go:127-133`）/`SingleJourneyEvidencePack`（`llm_single.go`）**今天已经
  不消费 `JourneySummary`**——`BuildEvidencePack`/`BuildSingleJourneyEvidencePack` 都直接吃
  `*Journey`（内存态），自己手工拼装有界的 `ToolIndexEntry`/`TaskTitles`/`Comparison`。
  `journeyRef(s JourneySummary)`（`compare.go:175-181`）只投影 `ID/Title/From/To` 四个字段。
  **P4.1 给 `JourneySummary` 加的新字段天然不会被证据包带上**——这是 P4.3 的起点，不是要修的 bug。

---

## 1. 执行前置检查

```bash
git status --short                     # 确认工作区干净，P3 那批改动已在 HEAD
go build -o /private/tmp/claude-501/-Volumes-SSD2T-code-vmr/*/scratchpad/vmrbin ./cmd/vmr
go test ./... 2>&1 | tail -30           # 建立改动前的基线：全绿
ls logs/vmr-audit-2026-07-28.jsonl.zst  # P1/P2/P3 用过的同一批样本，本机已存在
```

P4.1 与 P4.2 是同一批改动的两个侧面——"内联哪些字段、引用哪些字段"的边界判断必须在设计结构体
签名的同时定下来，不能先造一个不设防的结构体、事后再补界限（那样中间会有一次"体积不设防"的
提交态）。**本文把 P4.1+P4.2 合并成一个任务批次执行**，P4.3 独立在后。

每做完一个子步骤跑一次 `go test ./internal/story/... ./internal/archtest/...`，不要攒到最后一起
查错。

---

## 2. 任务一：补齐叙事结构 + 内联/引用边界（P4.1 + P4.2 合并执行）

### 2.1 目标设计

**新文件 `internal/story/structure.go`**（新增类型 + `BuildStructure`，预计 150-220 行，在默认
700 行预算内，不需要登记豁免）：

```go
// EventRef is one message's structural identity within the Journey's
// globally de-duped event stream — a REFERENCE, never its text (architecture
// doc §7.4b: "属于对话历史的内容走引用" — an ordinary conversation message is
// history, not this turn's decision). A consumer that needs the actual text
// follows the owning Step's Req coordinate to the audit record (or its
// rendered detail page) — journey-<id>.json is tree, the audit log is blob.
type EventRef struct {
	Hash         ctxgraph.Hash  `json:"hash"`           // MarshalJSON already renders hex — reuse, don't restring it
	Role         string         `json:"role"`
	FirstStepSeq int            `json:"first_step_seq"`
	Revises      *ctxgraph.Hash `json:"revises,omitempty"`
}

// ToolCallRef is one Step's tool call plus its paired result — paired ONLY
// via findings_toolresult.go's toolResultsFor (exact + id-normalized
// matching, the same precise pairing render_spine_step.go's renderSpineStep
// and the three Finding detectors already trust). The render-layer-only
// positional fallback (positionalToolResults) never appears here — it is a
// guess, and a machine-readable structural contract does not carry guesses
// (architecture doc §5.6). Matched=false means toolResultsFor found no
// pairing for this call; Result/ResultTruncated/ResultError are all zero.
type ToolCallRef struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Args          string `json:"args,omitempty"`
	ArgsTruncated bool   `json:"args_truncated,omitempty"`
	Matched       bool   `json:"matched"`
	Result        string `json:"result,omitempty"`
	ResultTruncated bool `json:"result_truncated,omitempty"`
	ResultError   bool   `json:"result_error,omitempty"`
}

// StepStructure is one Step's complete machine-readable shape: its own req
// coordinate (for I/O back to the source record/detail page), this turn's
// OWN decision content (RespText/Reasoning/tool calls — bounded, inlined,
// architecture doc §7.4b's inlineable exception), and references to the
// conversation-history messages it introduced (NewEvents — never inlined).
type StepStructure struct {
	Seq             int           `json:"seq"`
	Req             string        `json:"req,omitempty"`
	TS              time.Time     `json:"ts"`
	HumanInitiated  bool          `json:"human_initiated,omitempty"`
	NoReply         bool          `json:"no_reply,omitempty"`
	SysChanged      bool          `json:"sys_changed,omitempty"`
	Finish          string        `json:"finish,omitempty"`
	RespText        string        `json:"resp_text,omitempty"`
	RespTextTruncated bool        `json:"resp_text_truncated,omitempty"`
	Reasoning       string        `json:"reasoning,omitempty"`
	ReasoningTruncated bool       `json:"reasoning_truncated,omitempty"`
	ToolCalls       []ToolCallRef `json:"tool_calls,omitempty"`
	NewEvents       []EventRef    `json:"new_events,omitempty"`
}

type TaskStructure struct {
	Title string          `json:"title"`
	Steps []StepStructure `json:"steps"`
}

// JourneyStructure is journey-<id>.json's new "structure" field — the
// complete Task/Step/Event/ToolCall skeleton, absent any conversation-
// history message body (see EventRef's doc comment). Concatenating every
// Step's NewEvents in Task/Step order reproduces j.Events exactly — see
// journey.go's appendNewEvents, which writes to both in the same loop —
// so this does NOT also carry a top-level Events array; that would be the
// same data stored twice.
type JourneyStructure struct {
	Tasks []TaskStructure `json:"tasks"`
}

// structureExcerptChars bounds every inlined free-text field (RespText,
// Reasoning, a tool call's Args, a paired ToolResult's text) — reuses
// compare.go's existing excerpt convention/cap (initialInstructionExcerptChars)
// rather than inventing a second bound, so "how much inlined text is too
// much" has one answer in the codebase, not two.
const structureExcerptChars = initialInstructionExcerptChars

func BuildStructure(j *Journey) JourneyStructure { ... }
```

**逐字段的内联/引用判断**（架构文档 §7.4(b) 那条边界的具体落点）：

| 字段 | 处置 | 理由 |
| --- | --- | --- |
| `Step.Manifest.Req` | 内联（`Req` 字符串本身，不是它指向的内容） | 坐标是地址，不是内容——地址必须内联，否则结构本身无法导航 |
| `Step.RespText`/`Reasoning` | 内联，`structureExcerptChars` 截断 | 架构文档明确列为"本轮决策"的内联例外；截断防御的是极端长回复，不是常规情形 |
| `ToolCall.Args` | 内联，`structureExcerptChars` 截断 | 同上"本轮决策"的例外；但真实语料存在 `write_file` 一类大 payload 的调用，不截断会让单个 Step 的体积失控，与 P4.2 的常驻检查直接冲突——截断是从"决策本身"里取一个有界摘要，不是把它降级为引用 |
| `ToolResult.Text`（配对到的） | 内联，`structureExcerptChars` 截断 | 结果内容体量与请求正文同量级，同一条纪律 |
| `Event.Msg.Text`（`NewEvents`） | **不内联，只给 `Hash`/`Role`/`FirstStepSeq`/`Revises`** | 这是对话历史正文，架构文档 §7.4(b) 的边界线原文——"属于对话历史的内容走引用"；取用路径是 `Step.Req` → 详单/审计日志 |
| `Compaction`（`CompactionInfo`） | **本批不纳入结构体** | `PredecessorTextExcerpt` 字段本身已是 ≤3000 字符的正文摘录，比其余字段的性质更接近"历史片段"而非"本轮决策"；且 `Compaction` 只在缝合边界 Step 上非 nil，覆盖面小，纳入前先看 P4 落地后是否有真实消费方需要它——不为一个尚无消费方的字段预先设计 JSON 形状 |

### 2.2 常驻自动化检查（P4.2 的直接体现）

**新增测试**（`internal/story/structure_test.go`），断言"体积随步数增长，不随对话正文长度增长"这条
纪律本身，而不是断言某个具体数字：

1. 用 `buildTestJourney` 风格的夹具构造两个步数相同（如 N=5）、但每步 `RespText`/工具调用
   `arguments` 的原始长度相差两个数量级（如 50 字符 vs 50,000 字符）的合成 Journey。
2. 分别 `BuildStructure` 后 `json.Marshal`，比较两次输出的字节数——断言差值远小于原始文本长度的
   差值（例如：差值应落在"N 个 Step 各自的截断上限之内"的量级，而不是随注入文本线性增长）。
   具体的数值断言方式（差值的绝对上限、还是"差值 / 原始文本差值"的比例上限）落地时按
   `structureExcerptChars` 的实际取值定，但断言必须是"有界"而不是"相等"（两次输出允许因为各自
   都被截断到上限而产生同等大小的差异，不能要求两次输出完全一样大）。
3. 断言 `NewEvents` 里没有任何字段携带原始消息正文——用一个反向检查更直接：把注入的超长文本设为
   一个可 grep 的哨兵字符串（如 `"UNIQUE_SENTINEL_..."`），断言 `BuildStructure` 输出的 JSON
   **不包含**这个哨兵，除非它恰好出现在 `RespText`/`Reasoning`/`ToolCalls[].Args` 里（即：把哨兵
   放进纯对话历史消息文本时，结构体输出里必须找不到它；放进 `RespText`/`Args` 时，找到的必须是
   截断后的前缀，不是全文）。

这个测试属于 `go test ./internal/story/...` 的普通用例，天然"常驻"（每次 CI/本地跑测试都会执行），
不需要额外挂进 `archtest`——DevPlan"与 archtest 同级别"指的是**重要性和不可被绕过的程度**，不是
要求物理上放进 `internal/archtest` 包。

### 2.3 具体步骤

1. `internal/story/structure.go`：新建 `EventRef`/`ToolCallRef`/`StepStructure`/`TaskStructure`/
   `JourneyStructure` 类型定义（§2.1），以及 `BuildStructure(j *Journey) JourneyStructure`——
   遍历 `j.Tasks`/`task.Steps`，对每个 `Step`：
   - `Req` 取 `step.Manifest.Req`；
   - `RespText`/`Reasoning` 经 `truncateText(_, structureExcerptChars)`；
   - `ToolCalls` 遍历 `step.ToolCalls`，`Args` 经 `truncateText` 截断；用
     `toolResultsFor(steps, i)`（`i` 是该 Step 在 `journeySteps(j)` 里的下标——复用
     `render_spine_step.go` 已有的 `idxOf` 建法，或直接在 `BuildStructure` 内部对每个 Task 的
     `Steps` 用其在全局 `journeySteps(j)` 中的位置调用，注意 `toolResultsFor` 要求的是全局
     `steps []*Step` 而不是单个 Task 内的切片）建 `byID` map，未命中的 `ToolCall` 设
     `Matched: false`；
   - `NewEvents` 遍历 `step.NewEvents`，映射为 `EventRef{Hash: ev.Hash, Role: ev.Msg.Role,
     FirstStepSeq: ev.FirstStepSeq, Revises: ev.Revises}`（不读 `ev.Msg.Text`）。
2. `internal/story/metrics.go`：`JourneySummary` 加一个字段
   `Structure JourneyStructure \`json:"structure"\``（无 `omitempty`——与 `Metrics` 字段同级别，
   一个有效 Journey 应该总是有非空结构，除非它没有任何 Task，那种情况下 `Tasks` 为空切片本身
   就是诚实的表达，不需要额外隐藏）。`Summarize` 里加一行 `Structure: BuildStructure(j)`。
3. `internal/story/structure_test.go`：
   - 单元测试：对 `buildTestJourney(t, 3, false)` 构造的 Journey，断言 `BuildStructure` 输出的
     `Tasks[0].Steps` 数量、每个 Step 的 `Req` 非空、`ToolCalls` 数量与 `step.ToolCalls` 一致、
     `NewEvents` 数量与 `step.NewEvents` 一致。
   - **无损重建检验**（本批最重要的测试，命名建议
     `TestBuildStructure_LosslessReconstruction`，P5.1 的验收会直接引用它）：对一个真实/合成
     Journey，对每个 Step 用 `audit.LineAt` + 解码（走 `Req` 坐标这条真实 I/O 路径，不是直接读
     `step.Rec`，用来证明坐标本身是可用的，不是只在内存里凑巧对得上）取回原始记录，用
     `chatmsg.Messages` 解出消息列表，从该记录自己的 `DeltaStart` 位置切片，断言这段切片的
     `Role`/顺序与 `step.NewEvents`（进而与 `StepStructure.NewEvents`）逐一对应——这就是"给定
     `journey-<id>.json` + 审计日志，能否无损找回 fact-layer 今天展示的同一批事件"的直接证明。
     `RespText`/`Reasoning`/`ToolCalls` 的无损性则是直接断言（未截断时）`StepStructure` 字段与
     `Step` 源字段相等，因为它们本来就是同一份数据的截断投影，不需要走 I/O 往返。
   - 体积检验（§2.2 的三点）。
4. 真实语料验证：
   ```bash
   ./vmrbin story -o /tmp/p4verify -journey 'j-openclaw-20260728T000544*' logs/vmr-audit-2026-07-28.jsonl.zst
   python3 -c "
   import json
   d = json.load(open('/tmp/p4verify/stories/journey-j-openclaw-20260728T000544*.json'))  # 用实际文件名替换通配
   st = d['structure']
   steps = [s for t in st['tasks'] for s in t['steps']]
   print('steps:', len(steps))                              # 期望 22（与脊柱覆盖数一致，P1.2 已验证）
   print('with req:', sum(1 for s in steps if s.get('req')))  # 期望 == steps 总数
   print('tool_calls:', sum(len(s.get('tool_calls', [])) for s in steps))  # 期望 33（P1.1 已验证）
   print('matched:', sum(1 for s in steps for tc in s.get('tool_calls', []) if tc.get('matched')))  # 期望 33
   "
   du -h /tmp/p4verify/stories/journey-j-openclaw-*.json   # 与架构文档 §2.2 记录的 6,428 字节基线对比，
                                                            # 增长应与步数/工具调用数成比例，不应
                                                            # 跳变到 KB×步数 的量级
   ```

### 2.4 验收标准（对照 DevPlan P4.1 + P4.2）

- [x] `journey-<id>.json` 的 `structure` 字段可由无损重建检验证明：能无损重建当前人读事实层
      （fact-layer）的等价内容——`TestBuildStructure_LosslessReconstruction` 锁定，且经两轮独立
      评审后改为哈希匹配（不是 DeltaStart 切片），加了一个跨越去重场景的夹具，见执行记录 §8.2。
- [x] 产物体积保持在结构量级，不随对话长度线性膨胀——§2.2 的常驻检查锁定
      （`TestBuildStructure_VolumeBoundedByStepsNotProseLength`），真实语料实测（22 步/33 调用）
      92KB，与架构文档 §2.2 记录的基线（6,428 字节）对比，增长比例可解释（详见执行记录 §8.3）。
- [x] 两者必须同时成立（DevPlan 原文强调）：无损重建检验通过*且*体积检验通过，均已用真实语料
      与合成夹具验证。

---

## 3. 任务二：LLM 解读证据包按需取用（P4.3）

### 3.1 现状（已读代码确认）

- `EvidencePack`（`llm.go:127-133`）与 `SingleJourneyEvidencePack`（`llm_single.go`）**今天都不
  经过 `JourneySummary`**：`BuildEvidencePack(jA, jB *Journey, cmp Comparison, lang)` 与
  `BuildSingleJourneyEvidencePack(j *Journey, m Metrics, findings []Finding, lang)` 都直接吃内存里
  的 `*Journey`，自己拼一份有界视图（`journeyTaskTitles`、`buildToolIndex` 的
  `ToolIndexEntry{Seq, Tools, Brief}`——`Brief` 是 `taskseg.Preview(s.RespText)` 截断预览，不是
  全文）。
- `journeyRef(s JourneySummary)`（`compare.go:175-181`）是 `JourneySummary` 唯一的消费点，只投影
  `ID/Title/From/To` 四个标量字段，构造 `JourneyRef`（`compare.go:167-173`）。
- **结论**：2.1 节新增的 `JourneySummary.Structure` 字段，在今天的调用链上**没有任何路径**能被
  自动带进 `EvidencePack`/`SingleJourneyEvidencePack`——两者都不读 `JourneySummary`，
  `journeyRef` 的投影也不会因为源结构体多了字段而多输出什么。DevPlan 担心的"证据包体积因机读层
  补全而增长"，在当前调用链形状下**不会自动发生**。

### 3.2 目标设计

**核心任务是把这条"结构性隔离"钉成一个测试，防止以后被改坏**，而不是修一个当前存在的 bug：

1. **回归测试**（新增，放 `llm_packs_test.go` 或新文件）：构造一个"结构膨胀"的场景——一个 Task
   数、Step 数固定的 Journey，但每个 Step 塞入远超正常量级的 `RespText`/`ToolCalls[].Args`
   （模拟 §2.2 用过的超长 payload 手法）。断言 `BuildEvidencePack(...).EstimateChars()` /
   `BuildSingleJourneyEvidencePack(...).EstimateChars()` 的结果与"正常量级"输入构造的证据包相比，
   增长幅度落在 `buildToolIndex`（`Brief` 有界预览）本身的截断上限之内，**不随 `Structure` 字段
   的引入或体积变化而联动增长**——用同一个 Journey 分别在"未调用 `Summarize`（不产生
   `JourneySummary`）"与"已调用 `Summarize`"两种前置下构造证据包，断言两次 `EstimateChars()`
   结果完全相同，直接证明证据包的构造路径与 `JourneySummary`/`Structure` 无关。
2. **低风险的顺手改进（推荐但非 DevPlan 硬性要求）**：`ToolIndexEntry`（`llm.go:91-95`）加一个
   `Req string \`json:"req,omitempty"\`` 字段，`buildToolIndex`（`llm.go:101-117`）填
   `s.Manifest.Req`。理由：这恰好是"按需取用真正需要的片段"最直接的落地——LLM 解读今天只能引用
   一个 `Seq` 序号（人不好定位，机器也不能直接导航），加上 `req` 之后，无论是 LLM 输出里引用某个
   具体判断依据、还是未来 P6.2 要把解读小节的结论链接回具体请求，都有一个现成的坐标可用，且成本
   是给已经在遍历的每个 Step 多读一个已有字段，不新增任何 I/O 或体积膨胀风险（`req` 坐标字符串本
   身只有几十字节）。**这条如果与 §2 的改动在同一批提交，注意 `ToolIndexEntry`/`buildToolIndex`
   两处改动是 `llm.go` 现有代码，不是新文件，落地前确认 `llm.go`（当前 476 行，无专门预算，走
   默认 700 行上限）有余量，预计增量 2-3 行，无风险。**

### 3.3 具体步骤

1. `internal/story/llm_packs_test.go`（新文件，或按落地时惯例并入已有 `llm_test.go`/
   `llm_packs_test.go` 如果已存在同名测试文件）：
   - `TestBuildEvidencePack_SizeIndependentOfJourneySummary`：如 §3.2 第 1 点所述。
   - `TestBuildSingleJourneyEvidencePack_SizeIndependentOfJourneySummary`：同上，单 Journey 版本。
2. （推荐）`internal/story/llm.go`：`ToolIndexEntry` 加 `Req` 字段；`buildToolIndex` 填值；
   现有断言 `ToolIndexEntry` 字段集合的测试（若有）同步更新。
3. `go test ./internal/story/...`：确认新增测试通过，现有 `llm_test.go`/`llm_packs_test.go`/
   `compare_test.go` 的证据包大小相关断言（若有硬编码字符数）不受影响。
4. 真实语料验证：
   ```bash
   ./vmrbin story -o /tmp/p4verify -compare <id1>,<id2> -llm-dry-run -llm-addr x:1 -llm-model x \
       logs/vmr-audit-2026-07-28.jsonl.zst
   # 与 P1 §4.3 步骤 5 记录的 69,936 字符基线对比——预期证据包估算字符数基本不变
   # （新增的 req 字段每条目几十字节 × Step 数，量级远小于证据包本身）
   ```

### 3.4 验收标准（对照 DevPlan P4.3）

- [x] 证据包体积不因机读层补全而增长——`TestBuildEvidencePack_SizeBoundedRegardlessOfStructureRichness`/
      `TestBuildSingleJourneyEvidencePack_SizeBoundedRegardlessOfStructureRichness`（实现时改用
      更直接的形式：对同一个"参数体积相差 4 个数量级但步数相同"的 Journey 对，分别构造证据包并比较
      `EstimateChars()`，而不是"调不调用 `Summarize`"这种间接对照——见执行记录 §8.4）锁定；真实
      语料 `-compare` 实测证据包 22,635 字符，量级与 P1 §4.3 记录的 69,936 字符基线一致（不同
      Journey 对，非直接可比，但未出现异常跳变）。`ToolIndexEntry.Req` 已加。

---

## 4. 需要在实现时确认、不预先假设的几个点

1. **`structureExcerptChars` 是否直接复用 `initialInstructionExcerptChars`（2000）**：§2.1 的设计
   默认直接复用，理由是"一个上限，不是两个"；但 `RespText`/`ToolCalls[].Args` 与"初始指令"的典型
   长度分布不一定相同（工具参数可能远超对话文本）。落地时用真实语料跑一次分布统计（如
   `ToolCalls[].Args` 原始长度的 p50/p90/p99），如果 2000 字符明显偏离典型场景（比如 p50 就超过
   2000，导致几乎每条都被截断，截断标注失去信息量），再定一个专属常量——但要在注释里写清楚"为什么
   这里的上限和那里不一样"，不要没有理由地分叉。
2. **`Compaction` 是否真的不需要纳入结构体**：§2.1 的表格给出的判断是"本批不纳入"，落地时如果
   发现 P5.1 删除人读事实层后，`CompactionInfo`（token 增减、swallowed/survived entities）在
   人读侧也没有其它落点（`render_spine.go` 的脊柱是否已经展示了这些信息？需要在写 P5 ActionPlan
   前确认），可能需要回头补一个精简版本（不含 `PredecessorTextExcerpt` 那段历史正文摘录，只留
   token 计数与 entity 列表这些"分析结论"而非"历史内容"的部分）。这个决定留给 P5 的边界复核，
   不在本次预判。
3. **`ToolCallRef`/`EventRef` 的字段命名（snake_case JSON key 的具体拼法）**：本文给出的字段名
   是设计意图的直接体现，落地时如与 `journey-<id>.json` 其它既有字段的命名习惯（如 `Metrics`/
   `Finding` 的 JSON tag 风格）有细微不一致，以保持同一份 JSON 内命名风格统一为准，不必逐字照抄
   本文，但字段承载的语义（内联 vs 引用、截断标注）不能变。
4. **`toolResultsFor` 需要的 `i`（Step 在全局 `steps` 里的下标）如何在 `BuildStructure` 里取得**：
   `journeySteps(j)`（`metrics.go:139`）已经是"展平成全局顺序"的现成函数，`BuildStructure` 内部
   建议先调用一次 `journeySteps(j)` 得到 `steps []*Step`，再按 `Task`/`Step` 的原有嵌套结构遍历
   时，用 `steps` 的下标（与遍历 `j.Tasks`/`task.Steps` 时的全局计数器同步递增）传给
   `toolResultsFor`——不要为每个 Step 重新扫一遍 `journeySteps(j)` 去 `search` 自己的下标，
   这是一个可以在实现时用一个简单的运行计数器避免的 O(n²)。

---

## 5. 收尾（P4.1–P4.3 共用）

1. **全量测试与架构边界**：
   ```bash
   go test ./... -race
   go test ./internal/archtest/...
   go vet ./...
   gofmt -l .
   ```
2. **CHANGELOG.md**：`[Unreleased]` 下按 Added 分类加条目（具体措辞落地时按实际改动定），例如：
   - Added: `journey-<id>.json` 新增 `structure` 字段——完整的 Task/Step/Event/ToolCall 结构，
     对话历史消息按内容哈希与请求坐标引用，不重复存储正文；本轮决策内容（回复摘要、推理摘要、
     工具调用参数与配对结果）有界内联。
3. **KNOWN_ISSUES_sonnet-5.md**——已完成，见执行记录 §8.6：
   - `§1.20` 已改写为"机读层结构（P4）已完成，脊柱挂链接与删除 fact-layer 排在 P5"。
   - 新增 `§1.28`（`-llm-addr ''` 无法覆盖 `report.yaml` 默认地址，验证命令易打真实 LLM 调用）、
     `§1.29`（`structure` 字段无 schema 版本戳，低优先级、暂不做）。
4. **架构文档同步**——已完成：`story_report_architecture_opus-5.md` §2.2 的历史基线说明块已扩展
   为含 P4 的一句话状态更新（真实语料 92KB、无损重建/体积两条检验通过）。
5. **边界复核**（DevPlan §2.2 第 6 条，三个问题）——已按实际执行情况回填，见执行记录 §8 的完整
   叙述，这里只给结论：
   - **本阶段是否产生了架构文档未预见的事实？**——是，两处：(a) `journey-<id>.json` 的生产写入点
     `cmd/vmr/cmd_story.go` 的 `writeJourneyFile` 手工构造 `JourneySummary` 字面量、不经过
     `Summarize`——本文 §0 的"已读代码确认"覆盖了 `internal/story` 全部相关文件，但没读
     `cmd/vmr` 的写入路径，导致这处遗漏直到真实语料验证才现形；已修复为共用的
     `story.NewJourneySummary` 构造函数，并在 `cmd_story_test.go` 补了产物级回归测试。
     (b) `StepStructure` 的原始设计遗漏了图层级分析事实（`Edit`/`StitchEdge`/`Compaction`）与
     单步性能/成本字段（`Endpoint`/`DurMS`/`TTFTMS`/`Usage`）——这些是 fact-layer 今天就在渲染、
     但无法从单条审计记录重新算出的信息，P5.1 删除 fact-layer 前若不补上会造成永久性信息丢失。
     两轮独立评审（gemini、pi）都指出了这一点，已按其建议补全。
   - **本阶段是否改变了 P5 及以后的前提？**——是，一处修正：`TestBuildStructure_
     LosslessReconstruction` 的正确重建机制是哈希匹配（对每个 `EventRef.Hash`，在 `Req` 取回的
     记录里重建 `ctxgraph.Manifest` 并匹配 `Keys`/`MsgIdx`），不是最初设想的 `DeltaStart` 切片——
     后者在缝合边界或消息去重场景下会因 Journey 全局去重而失效（`msgs[DeltaStart:]` 是
     `NewEvents` 的超集，不是相等）。P5.1 若要复用这个检验模式，应该复用哈希匹配，不要复用
     DeltaStart 切片这条已被证明不完备的路径。
   - **本阶段是否暴露出某个原计划任务其实不必要？**——是一处：原计划把
     "证据包体积因机读层补全而增长"设想为需要专门验证的风险点，但已读代码确认后发现
     `EvidencePack`/`SingleJourneyEvidencePack` 的构造路径本来就不经过 `JourneySummary`，风险
     在当前架构下不会自动发生——P4.3 因此从"防一个真实风险"降级为"给这条已经成立的隔离性钉一个
     回归测试防止以后被改坏"，任务性质变了但没有变得不必要。

---

## 6. 验收清单（对照 DevPlan P4 的验收标准逐项勾）

- [x] 无损重建检验通过：`journey-<id>.json` 的 `structure` 字段可无损重建当前人读事实层
      （fact-layer）的等价内容——含图层级分析事实（Edit/StitchEdge/Compaction）与单步性能/成本
      字段，两轮独立评审后补全，见执行记录。
- [x] 体积检验通过：产物体积保持在结构量级，不随对话正文长度线性膨胀，且该判据落成一条常驻
      自动化检查（`internal/story/structure_test.go` 的
      `TestBuildStructure_VolumeBoundedByStepsNotProseLength`）。
- [x] 两项必须同时成立（DevPlan 明确要求，不能只满足一项）——均已验证成立。
- [x] 证据包体积不因机读层补全而增长——`EvidencePack`/`SingleJourneyEvidencePack` 的回归测试
      锁定，真实语料 `-llm-dry-run` 估算字符数无异常增长。
- [x] `go test ./...`（含 `-race`）、`go test ./internal/archtest/...`、`go vet ./...`、
      `gofmt -l .` 全绿。
- [x] CHANGELOG、KNOWN_ISSUES（§1.20 更新，另新增 §1.28/§1.29）、架构文档说明性备注、
      `docs/VirtualModelRouter_Design_v4_Analytics.md`、`docs/UserGuide.md`/`.zh` 均已同步。

---

## 8. 执行记录（2026-08-20，Sonnet 5）

本节是本文写完 ActionPlan 之后、实际落地执行的过程记录与总结——按用户要求补写，不是提前写好
的计划。**所有改动均未提交，等待人工 review。**

### 8.1 执行顺序与整体结果

按 §2（P4.1+P4.2 合并）→ §3（P4.3）顺序实现，每步做完立即 `go build`/`go test
./internal/story/...`；真实语料验证发现 §2 的 `writeJourneyFile` 遗漏（见 §8.2）后原地修复，
未跳过重新验证。**执行期间出现了两份独立的并发评审**
（`docs/future-strategy/story_report_p4_action_plan_review_gemini-3.7-flash.md`、
`docs/future-strategy/story_report_p4_action_plan_review_pi.md`）——与 P1/P2/P3 执行期间同样的
并发写作模式（另外的会话在独立评审本文档/本次落地代码）。两份评审均已通读并逐条核实（不是照单
全收，也不是因为"是评审意见"就默认正确），核实结果与处置见 §8.2。最终统一跑
`go build ./...`、`go vet ./...`、`gofmt -l .`、`go test ./... -race`、
`go test ./internal/archtest/...`，全部通过。

用本机真实审计日志验证（`logs/vmr-audit-2026-07-28.jsonl.zst`，P1/P2/P3 用过的同一批样本，
以及全部 6 条候选 Journey 的 `-render-all`）：

| 验证项 | 结果 |
| --- | --- |
| openclaw 样例 Journey（22 步/33 次调用）：`req` 覆盖率 | 22/22 |
| 同上：工具调用配对率 | 33/33，`result` 字段在任何 tool_calls 条目里均不出现（确认 T3 修复生效） |
| 同上：`structure` 字段体积 | 92KB（修正 Result 内联问题、加入 Edit/Stitch/Compaction/Usage 后的最终值；较中间版本的 112KB 下降，较 6,428 字节基线上升但有界，见 §8.3） |
| 同上：`edit`/`stitch_edge`/`compaction` 出现次数 | 20 步有 `edit`（首步与一处缝合边界无）、1 步有 `stitch_edge`、1 步有 `compaction`——首次在真实语料上证实这条 Journey 确实经过一次缝合 |
| 全部 6 条候选 Journey（`-render-all`，22～126 步不等） | 每条 `unmatched=0`、`has_result_field=False`，无 panic |
| `TestCmdStory_ListAndRender`（`cmd/vmr` 产物级测试，新增断言） | `structure.tasks` 非空且步数与夹具一致——直接命中 `writeJourneyFile` 那处曾经的遗漏点 |
| `-compare` 真实语料证据包 | 22,635 字符，与 P1 §4.3 记录的 69,936 字符基线量级一致（不同 Journey 对，非直接可比） |

### 8.2 两轮独立评审的处理：一处我自己独立发现，五处评审发现且核实为真

**执行过程中我自己独立发现、评审文档尚未出现前就已修复的一处**：`cmd/vmr/cmd_story.go` 的
`writeJourneyFile` 手工构造 `JourneySummary{}` 字面量、不调用 `Summarize`——`journey-<id>.json`
的生产写入点漏了 `Structure` 字段，导致真实语料验证时看到 `{"tasks": null}`。修复：新增
`story.NewJourneySummary(j, m, findings, llmFindings)` 作为唯一构造函数，`Summarize`/
`writeJourneyFile` 都改为调用它。两份独立评审（gemini §1.3/T1、pi 的 T1）随后各自独立发现了
同一处遗漏，与我的独立发现互相印证——但**这处遗漏本身说明本文 §0"已读代码确认"的覆盖面有真实
盲区**：读了 `internal/story` 全部相关文件，没读 `cmd/vmr` 的写入路径，这是本次执行里第一个
被真实语料验证撞出来、而不是靠通读代码提前发现的问题。

**评审发现、核实为真、已采纳修复的四条**（按影响排序）：

1. **`StepStructure` 遗漏图层级分析事实（gemini §1.2/pi T2，pi 额外发现 `Edit` 同样遗漏）**：
   `Edit`（编辑分类）、`StitchEdge`（缝合证据）、`Compaction`（token 前后与实体吞噬/存活）都是
   fact-layer 今天在渲染（`render_md.go:97-119`）、但**物理上无法从单条审计记录重新算出**的
   图层级分析结论——`Edit`/`Compaction` 需要比较相邻两条记录，`StitchEdge` 需要整条语料的缝合
   图搜索。P5.1 删除 fact-layer 渲染后，若 `structure` 不承载它们，这些信息会从任何产物里
   永久消失，直接违反 DevPlan"P5 依赖 P4——先删后补会留下一个真丢结构的版本"这条硬依赖。核实
   为真实、高优先级问题，已采纳：新增 `EditRef`/`StitchRef`/`CompactionRef`（`CompactionRef`
   排除 `PredecessorTextExcerpt`——那是历史正文摘录，不是分析结论，仍守住内联/引用边界）。
   同一条评审也指出 fact-layer 步头行的 `Endpoint`/`DurMS`/`TTFTMS`/`Usage` 缺失——这些虽然
   理论上可以从 `req` 回源拿到（不像 Edit/Stitch/Compaction 那样物理上拿不到），但既然
   fact-layer 今天就展示、且已经在 `Step.Manifest` 上现成可读，同一条"无损重建"逻辑要求把它们
   也内联，已采纳。
2. **`ToolCallRef.Result` 内联违反架构文档自己划定的边界（pi 独有发现，T3，gemini 未提）**：
   架构文档 §7.4(b)/§8 的边界原文明确写的是"tool_call 的**参数**"可内联，不包括结果；工具结果
   是对话历史（客户端会把它原样回写进下一轮请求，字面意义上的"历史"），已经作为下一个 Step 的
   `NewEvents` 出现。原设计把 `Result`/`ResultTruncated` 也内联进 `ToolCallRef`，是对上位文档
   边界的一次静默扩展，且是同一份内容存两份地址（违反项目 blob/tree 原则）。核实为真，采纳
   pi 建议的方案 (b)：收敛回引用——`ToolCallRef` 只保留 `Matched`/`ResultError`（关于内容的
   元信息，不是内容本身），结果正文完全依赖下一步的 `NewEvents`。真实语料验证：体积从 112KB
   降到 92KB，且用一个专门测试（`TestBuildStructure_ToolCallRefHasNoResultText`）锁定。
3. **"无损重建检验"的 DeltaStart 切片法在缝合/去重场景下不成立（pi 独有发现，T4；gemini §1.1
   只发现"测试偷用了内存态"，但提出的修法——发布 `DeltaStart` 并改用 `msgs[ss.DeltaStart:]`
   ——本身不完整）**：`appendNewEvents` 对 `msgs[DeltaStart:]` 还叠加了 Journey 全局去重
   （`seen[hash]`），缝合边界或消息重复时 `NewEvents` 是 `msgs[DeltaStart:]` 的**严格子集**，
   位置切片会失败。核实为真，采纳 pi 建议的哈希匹配方案：对每个 `EventRef.Hash`，用
   `audit.LineAt` 取回记录后调用 `ctxgraph.BuildManifest`（导出函数，和内部构造 Manifest 走
   同一条路径）重建 `Keys`/`MsgIdx`，按哈希查表而不是按位置切片。新增一个专门构造"用户消息
   原样重复"场景的夹具，断言 `NewEvents` 确实比 `msgs[DeltaStart:]` 少一条（先证明测试真的在
   测困难场景，再验证重建正确）。`DeltaStart` 仍然发布在 `StepStructure` 里，但降级为"导航
   便利字段"，doc comment 明确说明它不是重建机制。
4. **收尾清单缺 UserGuide 与 Analytics 设计文档同步（pi 独有发现，M2）**：这两处文档逐字段
   描述了 `journey-<id>.json` 的内容，原计划的收尾清单只提了 CHANGELOG/KNOWN_ISSUES/架构文档
   三处。核实为真，已补 `docs/UserGuide.md`/`.zh` 与
   `docs/VirtualModelRouter_Design_v4_Analytics.md` 的对应段落。

**核实为真、判断为低优先级、已登记为 KNOWN_ISSUES 条目而非当场修复的两条**：

- **`-llm-addr ''` 无法覆盖 `report.yaml` 默认地址（pi M5.1）**：`resolveString` 把显式空串等同
  于未传，验证命令因此真的打了 LLM 调用（复现：`calling http://192.168.0.22:8800/...`）。这是
  `cmd/vmr/reportconfig.go` 的通用三元优先级，不是 P4/story 专属逻辑，牵动每个吃 `-llm-*` flag
  的命令，不该在验证一个无关阶段时顺手改。登记为 `KNOWN_ISSUES §1.28`。
- **`journey-<id>.json` 的 `structure` 无 schema 版本戳（pi M3）**：核实为真实缺口，但
  `journey-<id>.json` 不是跨运行复用的缓存（每次针对该 journey 运行都整份重写），风险面比
  `.parse-cache/` 窄得多。登记为 `KNOWN_ISSUES §1.29`，留给 P5/P6 触及这份 JSON 形状时一并做。

**核实为部分正确、采纳其中一半的一条**：

- **体积守卫测试断言门限（gemini §2.3 建议收紧到 1.5×；pi §5 交叉意见认为"不建议照单采纳"，
  除非先确认夹具只有一个字段在变）**：核实后判断 pi 的顾虑成立——原门限（4 步 × 上限 × 4）确实
  偏松，但盲目收紧到 1.5× 有误报风险。移除 `Result` 字段后重新核算：当前夹具唯一会变的量是
  `Args`（huge fixture 不触碰 `RespText`/`Reasoning`/tool 结果），门限收紧为 `4 步 × 上限 × 2`
  （覆盖 JSON 转义开销，不再包含"防未知字段意外膨胀"的双倍冗余），真实语料实测的 diff 远低于
  这个门限，测试仍然是真断言而非形式主义。

**核实为文档措辞问题、已直接修正的三条（E1/E2/E3，pi §1.3）**：`positionalToolResults` 的文件
引用（在 `render_spine_step.go` 而非 `findings_toolresult.go`）、"P2 §7 记录 6,428 字节"应为
"架构文档 §2.2"、"全仓无 nil 判空先例"的过度绝对化表述——均已在本文 §0 原地改正，不影响任何
代码决策，纯引用/措辞准确性问题。

**核实后判断无需改动，或已顺带满足的三条**：

- gemini §2.2/pi L4（`EventRef.FirstStepSeq` 冗余）：两份评审都认可"反范式设计"（服务于展平
  消费场景）这个辩护成立，只要求把理由写进注释——已在 `structure.go` 的 `EventRef` doc comment
  写明，未改代码。
- gemini §2.4（`byID` 覆盖风险）：`toolResultsFor` 的 `CallID` 重写已提供规范性保证
  （P1.1 既有设计），两份评审都认为无需代码改动。
- pi L3（截断分布实测应留档）：已在 `structureExcerptChars` 的 doc comment 里写入真实测量
  （6/33 args、0/22 resp 触发截断），不需要额外文档。

### 8.3 体积演进的完整数字（同一样例 Journey，22 步/33 次调用）

| 版本 | 体积 | 说明 |
| --- | --- | --- |
| P4 之前（架构文档 §2.2 基线） | 6,428 字节 | 无 `structure` 字段 |
| 第一版实现（含 `Result` 内联，无 Edit/Stitch/Compaction/Usage） | 112,840 字节 | 两份评审介入前的中间状态，未提交 |
| 最终版（去掉 `Result`，加入 Edit/Stitch/Compaction/Usage） | 92,160 字节 | 已用真实语料与两条常驻检查验证 |

体积没有单调下降到接近基线，是设计判断的直接结果，不是问题：Edit/Stitch/Compaction/Usage 是
P5.1 依赖的信息，缺了就是真丢数据；去掉的是本来就不该内联的 Result 文本。92KB 相对 6.4KB 基线
的增长，全部来自"把机读层从空的补齐成完整"这件事本身，而不是违反了体积随步数增长的纪律
（`TestBuildStructure_VolumeBoundedByStepsNotProseLength` 独立验证了后者）。

### 8.4 与最初 ActionPlan 设计的实际出入（技术细节，非上面已覆盖的评审发现）

- `EventRef.Hash`/`Revises` 最终直接用 `ctxgraph.Hash`/`*ctxgraph.Hash` 类型（复用其现成的
  `MarshalJSON`），中途尝试过给它起一个包内别名 `type Hash = ctxgraph.Hash` 图注释简洁，落地时
  判断这是无谓的包级 API 面（`story` 包不该为了少打几个字符多导出一个类型名），撤回，直接用
  `ctxgraph.Hash`。
- P4.3 的回归测试最终形态与原计划 §3.2 第 1 点设想的"调不调用 `Summarize` 对照"不同：改用更直接
  的"同步数、参数体积相差 4 个数量级的 Journey 对，比较两次 `EstimateChars()`"，因为它同时验证
  了"体积不因 `Structure` 存在而增长"*和*"体积不因该 Journey 内容本身很大而增长"两件事，覆盖面
  比原计划的对照方式更完整；测试名相应改为 `TestBuildEvidencePack_SizeBoundedRegardlessOf
  StructureRichness`（原计划设想的名字是 `..._SizeIndependentOfJourneySummary`）。
- `internal/story/llm_packs_test.go` 落地时发现是仓库里**已存在**的文件（`9780522` 提交，含 5 个
  与 P4 无关的既有测试），不是新文件——第一次用 `Write` 工具时误当成新文件整体覆盖，销毁了原有
  测试。执行期间自查 `git diff --stat` 时发现（diff 显示 -185/+75 而不是纯新增），已用
  `git show HEAD:...` 恢复原内容并把两个新测试追加进去，最终 diff 干净地只有 +66 行。记录在此
  是为了如实说明这个过程，不是掩盖——所有改动仍在工作区，未提交，人工 review 时能看到完整、
  正确的最终 diff。

### 8.5 尚待你决定的事项

1. **两份非我创建的评审文档**（`story_report_p4_action_plan_review_gemini-3.7-flash.md`、
   `story_report_p4_action_plan_review_pi.md`）是否需要处理——本次执行只读取核实了内容，均未
   改动、未删除，留在仓库里供你查阅。
2. `KNOWN_ISSUES §1.28`（`-llm-addr ''` 的 flag 语义）、`§1.29`（`structure` 缺 schema 版本戳）
   是否要排期——两者都判断为低优先级、非阻断项，已交付的功能不依赖它们是否处理。
3. 所有代码/文档改动都在工作区，未 `git add`/`git commit`，等待你 review 后决定如何处理。

### 8.6 KNOWN_ISSUES / CHANGELOG 收口摘要（对照 §5 收尾清单）

- `§1.20` 已改写，反映 P4 完成状态。
- 新增 `§1.28`（`-llm-addr ''` 语义）、`§1.29`（`structure` schema 版本戳，暂不做）。
- `CHANGELOG.md` 的 `structure` 字段条目已按最终设计（无 `result`，含 Edit/Stitch/Compaction/
  Usage）改写，不是初版设计的措辞。
- `docs/VirtualModelRouter_Design_v4_Analytics.md`、`docs/UserGuide.md`/`.zh`、架构文档 §2.2
  的历史基线说明块均已同步到最终设计与真实语料数字（92KB，非中间版本的 112KB）。
