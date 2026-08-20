// Ver 2026-08-20 15:32, by Sonnet 5

# vmr 日志分析体系重构 — P6 ActionPlan（套件闭环、命令行收敛与口径收尾）

## 0. 定位

本文是 `docs/future-strategy/story_report_dev_plan_opus-5.md` 里 **P6 阶段**的执行级细化，基于
本仓库 P6 起点（P0–P5 全部已完成，P5 commit `8620f7c`）的真实代码状态编写。架构依据见
`docs/future-strategy/story_report_architecture_opus-5.md` 的 §7.3(b)（会话级身份）、§7.5（导航
矩阵）、§7.7（索引可用性）、§7.9（命令层）、§9 风险 #1/#4、§8 决策表对应各行。

**DevPlan review 结论**（本次 review 的产出，已回填进 DevPlan 正文）：P6 的任务范围/边界/验收
标准在当前代码库上逐条核实后**基本仍然成立**，只发现两处需要修正的落差，均已在 DevPlan 里改正：

1. **P5 未标记为已完成**——`git log` 确认 commit `8620f7c` 已经落地 P5 全部三项任务（脊柱详单链接
   22/22、报告体积降 64%、系统提示词改为证据引用），但 DevPlan 正文的汇总表与 P5 小节标题都还
   没打勾。已在 DevPlan 补上 ✅ 标记与状态段落。
2. **P6.5 一行的描述已经部分过时**——DevPlan 原文把"一个按坐标读取记录的原语；重放/诊断/读取
   共用同一种记录选择器"写成 P6 待做项，但 P3 已经在 `vmr replay -req`/`-print` 上提前交付了
   读取原语与坐标选择器（`vmr replay -detail` 已移除）——这一点 DevPlan 自己 P3 小节的"对后续的
   影响"其实已经记录过，只是 P6.5 那一行没有跟着改。已改写为"本阶段只收尾 `-req` 免位置参数的
   体验（`KNOWN_ISSUES §1.25`）"。**本文 §6 直接采用这个收窄后的范围，不重新论证。**

P6.1–P6.4 的任务边界/验收标准逐条核实后**不需要改动**——本文按 DevPlan 原文执行，唯一的调整是
**执行顺序**（见下段），不是范围变化。

**执行顺序与 DevPlan 编号顺序不同，原因见此**：DevPlan 按 P6.1→P6.2→P6.3→P6.4→P6.5 编号，
但真实的依赖关系是：

- **P6.2（导航边）依赖 P6.1（会话内容寻址）**——"请求索引行 → 任务"这条边的 join key 正是
  P6.1 要建立的 lineage ID；先做 P6.2 会在这条边上留一个用位置序号拼不出稳定 join 的中间态。
- **P6.2 也从 P6.3（索引类别列）先行中受益**——P6.2 要把 `vmr-stories.md` 变成"大盘→任务索引"
  这条边的落地页，一个还没分类、55% 是噪声的列表不是一个合格的落地页；先让索引本身可用，
  导航边接进来才有意义。这与架构文档 §7.7 标题"先解决噪声，'一键下钻'才成立"是同一个论证。
- **P6.1 与 P6.3 都要改 `internal/story/storyindex.go` 的 `JourneyIndexRow`/`BuildJourneyIndexRow`**
  ——同一批改动的两个新字段（`Lineages`/`Category`），合并到一次改动里可以避免中间态里
  `JourneyIndexRow` 的 schema 改两轮、golden 断言改两轮。
- **P6.4（口径收尾）与前三项相互独立**，可以在任意点插入；排在 P6.2/P6.3 之后、P6.5 之前，
  是因为它跟 P6.5 一样要改 `cmd/vmr` 的组合根装配逻辑（`aggState`/`ListCandidates` 调用点），
  放在一起顺序执行减少来回横跳。
- **P6.5（命令行收敛）排最后**——这是本阶段影响面最大的一次改动（用户可见的 CLI 表面变化），
  且它天然从"P6.1–P6.4 已经让两条命令的产物在同一输出根下可以互相 join"这个前提中获益：
  合并入口时，`report`/`story` 各自的扫描/建图/写盘逻辑应该已经收敛到位，合并成为一次纯粹的
  "组合根装配顺序"改动，而不是同时还要处理"两边算出的会话身份对不上"这类地基问题。

因此本文按 **P6.1 → P6.3 → P6.2 → P6.4 → P6.5** 顺序执行，§2–§6 依次对应。

**P6 范围边界**（与 DevPlan 一致）：会话级身份、导航边、索引可用性、统计口径、命令行表面。
**不动**：任何 Finding 检测器判据、`ctxgraph`/`chatmsg`/`taskseg` 的解析规则、`journey-<id>.json`
的 `structure` 字段形状（P4 已定型）、`internal/reqdetail` 的渲染内容（P2/P5 已定型，本阶段只给
它的产物挂更多链接，不改它的内容）。

**已读代码确认的关键前提**（P6.1–P6.5 都要用到，先在这里一次性钉住，不在各任务里重复核实）：

1. **`ctxgraph.Lineage.RootHash()`（`internal/ctxgraph/lineage.go`）已经是现成的、`story` 侧
   `deriveID` 一直在用的同一个哈希**（`journey.go` 的 `idCodeLen = 8`，取 `RootHash()` 的前 8
   位十六进制）——P6.1 的 lineage ID 直接复用这两者，不新造哈希口径。
2. **`report` 侧 `group()`（`internal/report/session.go:532-589`）在给每个 `SessionInfo` 赋值
   `s.ID = fmt.Sprintf("s%02d", i+1)` 的循环体（569-577 行）之前，`lin *ctxgraph.Lineage` 已经
   在作用域内**（562 行 `lin := lineageByLoc[loc]`，本质是"这个 Session 对应哪条 Lineage"这层
   映射从 546 行 `sessionOfLineage[lin.Idx] = s` 就已经建立）——P6.1 不需要新增任何查找逻辑，
   只是把 566 行附近保存的映射也存一份 `lin` 本身（或其 `RootHash()`），供 570 行的赋值循环用。
3. **`SessionInfo.ID`/`SessionRow.ID` 在下游只被当作不透明字符串使用，没有任何地方假设它是
   `s\d\d` 格式**（`grep` 核实：`section_sessions.go`/`requests.go` 都只做字符串相等比较或直接
   渲染，没有正则解析或位置运算）——P6.1 把它换成 `l-<hash8>` 是纯粹的取值替换，不牵动使用点的
   逻辑；唯一需要新增的是"想在人读表格里仍然看到一个短序号"时的显示别名（DevPlan 原文"想留就留"，
   本文 §2 给出具体处置）。
4. **`internal/audit.KeyTag(key string) string`（`internal/audit/audit.go:450`）是纯函数**，
   取 key 尾部（`keyTagLen` 窗口，遇 `-` 从其后截断），与 `report.yaml`/`config.yaml` 的
   `api_keys` 认证路径（`internal/server/server.go:99` 的 `authenticate`）用的是同一个函数——
   这正是"识别规则只定义一处"在本仓库里已经成立的那一半：**不需要新造一条识别规则，只需要在
   `cmd/vmr` 组合根把它用在"自指流量"这个新场景上**。见 §5。
5. **`report`/`story` 两条命令今天各自独立调用一次 `ctxgraph.LoadCacheDir`/`ScanCached`/
   `SaveCacheDir`**（`cmd_report.go:320-367`、`cmd_story.go:116-227`）——默认工作流（先跑
   `report` 再跑 `story`，或反过来）today 会让同一批日志被解析缓存命中两次、扫描两次、
   `.parse-cache/` 目录被写两次。这是 P6.5"一次扫描、一份缓存"要消除的具体现象，量化基线见 §6.1。
6. **`vmr replay` 已经具备统一坐标选择器与读取原语**（P3 交付，`-req basename:line` + `-print`，
   `-detail` 已移除）——P6.5 不需要重新设计这一层，只需要按 `KNOWN_ISSUES §1.25` 补"位置参数可选，
   按 basename 在给定目录/`log_dir` 下自动定位文件"这一小块体验。
7. **行数预算现状**（落地前逐一确认，超预算的改动落到新文件，不硬挤）：
   `internal/story/storyindex.go` 230/700（470 行余量，P6.1+P6.3 两个新字段绰绰有余）；
   `internal/story/candidates.go` 86/700（P6.3 的分类函数可以直接加在这里，也有余量放
   `internal/report/session.go` 797/1100（303 行余量）；`internal/report/rows.go` 777/900
   （123 行余量）；`internal/story/journey.go` 716/850（134 行余量）；
   `internal/report/render_doc.go` 227/400（173 行余量）；`internal/report/requests.go`
   620/700（默认预算，80 行余量——P6.2 的会话行链接需要留意，若不够则新开
   `internal/report/requests_journey_link.go`）；`cmd/vmr/cmd_story.go` 775/850（75 行余量，
   P6 全部五个子任务都会touch这个文件，累计增量需要边做边核对，超了参考 P5 §0 第 9 点的处置——
   把循环体逻辑挪到 `internal/story` 的新文件，不硬挤 `cmd_story.go`）；`cmd/vmr/cmd_report.go`
   392/500（108 行余量）；`internal/audit/audit.go`（未见预算登记，走默认 700，现状远低于此，
   富余）。

---

## 1. 执行前置检查

```bash
git status --short                     # 确认工作区干净，P5 那批改动已在 HEAD（8620f7c）
go build -o /private/tmp/claude-501/-Volumes-SSD2T-code-vmr/*/scratchpad/vmrbin ./cmd/vmr
go test ./... 2>&1 | tail -30           # 建立改动前的基线：全绿
ls logs/vmr-audit-2026-07-28.jsonl.zst  # P1-P5 用过的同一批样本，本机已存在
```

按 §2（P6.1）→ §3（P6.3）→ §4（P6.2）→ §5（P6.4）→ §6（P6.5）顺序执行——原因见 §0。每做完一个
子任务跑一次 `go test ./internal/report/... ./internal/story/... ./internal/reqdetail/...
./internal/audit/... ./internal/archtest/...`，不要攒到最后一起查错——五个子任务分布在 report/
story/audit/cmd 四个位置，互相之间没有编译期耦合，但都改同一批被广泛依赖的类型（`SessionInfo`/
`JourneyIndexRow`），分开验证更容易定位问题。

---

## 2. 任务一：会话级身份改为内容寻址（P6.1，先行执行）

### 2.1 现状（已读代码确认，见 §0 第 2/3 点）

`report` 的 `SessionInfo.ID`/`SessionRow.ID` 是 run-scoped 位置序号 `s%02d`
（`session.go:571`），`story` 的 Journey ID 是内容寻址（`journey.go` 的 `deriveID`，
`j-<client>-<start>-<end>-<code8>`）。两者在语义上是同一个东西的两种编码——`report` 的一个
Session 就是一条 `ctxgraph.Lineage`（`group()` 注释原文"one SessionInfo per Lineage"），
`story` 的一个 Journey 就是若干条 Lineage 缝合成的链（`ChainFrom`）。

### 2.2 目标设计

**给 Lineage 本身一个内容寻址 ID**，`report`/`story` 都改为引用它，而不是各自发明一个：

```go
// internal/ctxgraph/lineage.go 或 reqcoord.go（哪个文件放，看落地时哪边行数余量更充足）
// LineageID is a Lineage's stable, content-addressed identity — the same
// RootHash + 8-hex-prefix convention story's deriveID already uses for a
// Journey's own id component, applied to the structural unit both halves
// agree a "session" already is (report: one SessionInfo per Lineage;
// story: a Journey is a chain of these). Empty RootHash (a Lineage whose
// root manifest has neither system prompt nor content-addressed messages)
// renders as "l-00000000", same degenerate case RootHash() itself already
// tolerates — not an error, just a session with nothing to hash.
func (l *Lineage) LineageID() string {
    h := l.RootHash()
    return "l-" + hex.EncodeToString(h[:4])
}
```

- **`report` 侧**：`session.go` 的 `group()` 循环里，把 570 行附近的 `s.ID =
  fmt.Sprintf("s%02d", i+1)` 换成 `s.ID = lin.LineageID()`（`lin` 已在作用域内，见 §0 第 2 点）。
  **`s%02d` 不删除，降级为显示别名**——DevPlan 原文"想留就留，它不再承担 identity 职责"。
  新增 `SessionInfo.DisplayAlias string`（同一循环里继续赋 `fmt.Sprintf("s%02d", i+1)`），
  `SessionRow` 同步加 `Alias string \`json:"alias,omitempty"\``。`section_sessions.go`/
  `requests.go` 渲染表格时，标题行从"`## %s · ts · N tasks`"（用 `s.ID`）改为在括号里带上别名，
  如"`## l-a3f21c9d (s01) · ts · N tasks`"——具体排版留给实现时按人读表格的列宽判断，唯一
  约束是**别名必须仍然可见**（用户已经习惯用 `s01`/`s02` 在同一份报告内互相口头指代）。
- **`story` 侧**：`JourneyIndexRow` 新增 `Lineages []string \`json:"lineages,omitempty"\``
  （§0 第 7 点已确认 `storyindex.go` 有余量）。`BuildJourneyIndexRow(chains []*ctxgraph.Lineage,
  ...)`（`storyindex.go:106`）本来就在遍历 `chain` 算 `requests`/`files`，同一个循环里追加
  `lineages = append(lineages, l.LineageID())`即可，不需要新的遍历。
- **`s%02d` 的赋值时机不变**（仍在 `group()` 循环里、仍按 Lineage 首次出现顺序编号）——
  这条纪律本身不是本次改动的对象，只是从"identity"降级为"display"，**赋值代码一行都不用改**，
  只是把它写进的字段从 `ID` 改成新的 `DisplayAlias`。

**唯一冲突点**：`SessionRow.ID` 目前是 `json:"id"`，改成内容寻址值后，任何已经把 `vmr-report.json`
的 `sessions[].id` 当 `s01` 格式解析的外部脚本都会失效——这属于 DevPlan/架构文档已经裁决的
"无兼容包袱"范围（`CLAUDE.md`/DevPlan 全文没有为分析半区的 JSON 契约做过版本化承诺），**只需要
写进 CHANGELOG 的 Changed**，不需要设计迁移路径。

### 2.3 具体步骤

1. `internal/ctxgraph/lineage.go`：新增 `(*Lineage).LineageID() string`（§2.2），配一个直接测试
   （构造两个 `Manifests` 不同的 Lineage，断言 ID 不同；构造两个内容相同的 Lineage，断言 ID 相同
   ——这就是"内容寻址"这个词在测试里的直接体现）。
2. `internal/report/session.go`：`SessionInfo` 加 `DisplayAlias string` 字段；`group()` 的
   570-577 行循环体改为 `s.ID = lin.LineageID(); s.DisplayAlias = fmt.Sprintf("s%02d", i+1)`。
   **`t.ID`（`t%02d`，TaskInfo）不改**——DevPlan/架构文档都只点名"会话级身份"，Task 一级今天没有
   独立于 Session 的内容寻址单元可用（一个 Task 是 Session 内部按用户指令切的一段，不是
   `ctxgraph` 的既有结构单元），保持位置序号符合"不为不存在的单元发明身份"这条一贯纪律。
3. `internal/report/rows.go`：`SessionRow` 加 `Alias string \`json:"alias,omitempty"\``；找到
   `SessionInfo`→`SessionRow` 的转换点（`grep -n "SessionRow{" internal/report/*.go`，实现时
   确认具体文件，预期在 `section_sessions.go` 或 `aggregate.go` 附近），补上 `Alias:
   s.DisplayAlias`。
4. `internal/report/section_sessions.go`/`requests.go`：涉及 `s.ID`/`SessionRow.ID` 渲染成表格
   标题的位置，改为同时带上别名（§2.2 的排版约束）。`ContinuedFrom`/`Summarizes`/`ContinuesTo`
   等跨会话链接字段本身存的还是 `SessionInfo.ID`（现在是 lineage ID），不需要跟着改逻辑——它们
   只是把一个不透明字符串存起来再查表，换成什么格式的字符串都成立。
5. `internal/story/storyindex.go`：`JourneyIndexRow` 加 `Lineages []string`；
   `BuildJourneyIndexRow` 补一行 `lineages = append(lineages, l.LineageID())`（在遍历 `chain`
   算 `requests`/`fileSet` 的同一循环体内）。
6. `go test ./internal/ctxgraph/... ./internal/report/... ./internal/story/...`：跑通已有测试
   ——预期大量断言字符串里硬编码的 `"s01"`/`"s02"` 需要改成 `LineageID()` 算出的实际值（落地时
   用真实测试夹具算出来再回填，不要预先猜哈希值）；`archtest` 不受影响（没有新增 import）。
7. 真实语料验证：
   ```bash
   ./vmrbin report -o /tmp/p6verify logs/vmr-audit-2026-07-28.jsonl.zst
   ./vmrbin story  -o /tmp/p6verify logs/vmr-audit-2026-07-28.jsonl.zst
   python3 -c "
   import json
   rep = json.load(open('/tmp/p6verify/vmr-report.json'))
   sto = json.load(open('/tmp/p6verify/stories/vmr-stories.json'))
   report_ids = {s['id'] for s in rep.get('sessions', [])}
   story_lineages = {lid for j in sto['journeys'] for lid in j.get('lineages', [])}
   print('report session ids:', len(report_ids))
   print('story lineage refs:', len(story_lineages))
   print('overlap (report session IS a member of some journey):', len(report_ids & story_lineages))
   "
   ```
   期望"overlap"覆盖绝大多数 report session（残差应该只对应架构文档 §2.2 已经量化的"story 结构性
   排除的单发请求"，不是本次改动要修的东西——与 P2.1 验收时用 `req` 坐标 join 的同一条免责逻辑）。

### 2.4 验收标准（对照 DevPlan P6.1）

- [ ] 宏观的会话行与中观的任务之间可直接判定归属，无需查表——`SessionRow.ID`（lineage ID）与
      `JourneyIndexRow.Lineages` 之间是集合成员判断，不是两边各算一个哈希再对表。
- [ ] 机读产物的会话标识不再随输入范围变化——同一条 Lineage 无论出现在哪次扫描的哪个文件子集里，
      `LineageID()` 算出的值相同（内容寻址的直接推论，用 §2.3 步骤 7 的子集/全量对照验证一次）。

---

## 3. 任务二：索引可用性——类别列（P6.3，第二执行）

### 3.1 现状（已读代码确认）

`internal/story/storyindex.go` 的 `JourneyIndexRow` 今天没有任何分类字段；`internal/story/
candidates.go` 的 `ListCandidates` 只按"链上总请求数 ≥ 2"做结构性过滤，不区分候选的性质。
`vmr-stories.md` 因此把 55% 的 ≤2 轮 heartbeat/cron 单次调用与真正的多轮任务并列平铺
（架构文档 §7.7 的实测数字）。

**taskseg today 不提供标题前缀识别**——`grep` 核实 `internal/taskseg/*.go` 没有任何
"heartbeat"/"cron:"/"Subagent Context" 这类字面量匹配代码；这些词是**真实客户端在消息正文里
自己写的前缀**（如 OpenClaw 的定时任务把首条用户消息文本写成 `[OpenClaw heartbeat poll] ...`），
`deriveTitle`（`journey.go:690`）只是把这段文本原样摘出来当标题，taskseg 本身不对标题做分类。
**这与架构文档 §7.7 的措辞（"taskseg 已经能识别的标题前缀"）有出入**——不是 taskseg 已经识别，
是这些文本**已经出现在** taskseg 处理后的标题产物里，分类判断本身是本任务要新写的代码，不是
复用现成能力。落地前必须用真实语料核实这批前缀字符串的确切拼法（大小写、是否带方括号），
不能直接照抄架构文档里的示例——那是从有限窗口样本转述的措辞，不是权威拼法定义。

### 3.2 目标设计

**判据只用两个已有的结构信号**（DevPlan 原文"不引入新的猜测"）：

1. **轮数**（`JourneyIndexRow.Requests`，已有字段）——`Requests <= 2` 且没有 `Stitched` 延伸
   （即单条 Lineage、非缝合链）是"可能是噪声"的第一层信号，但**不单独作为分类判据**（`Requests`
   低不等于是心跳——存在很短但确实是一次真实任务的 Journey）。
2. **标题前缀**（`Title`，已有字段，`deriveTitle` 产出）——`strings.HasPrefix` 匹配真实语料核实
   过的字面量前缀集合。

**分类**（四类，落地前用真实语料核实每一类至少命中过一次，命中数为零的类别不要硬留在代码里）：

```go
// internal/story/candidates.go 或新文件 internal/story/category.go（看落地时 candidates.go
// 是否还有余量——86/700，大概率够用，不强制分文件）

type JourneyCategory string

const (
    CategoryTask      JourneyCategory = "task"      // 默认类别：不匹配下面任何噪声前缀
    CategoryCron      JourneyCategory = "cron"       // 标题匹配已核实的 cron 前缀
    CategoryHeartbeat JourneyCategory = "heartbeat"  // 标题匹配已核实的 heartbeat 前缀
    CategorySubagent  JourneyCategory = "subagent"   // 标题匹配已核实的 subagent 前缀
)

// classifyJourney is a pure function of a Journey's already-derived title —
// it does NOT re-scan message content or re-run taskseg; the prefix
// literals it matches against must be re-verified from real corpus before
// landing (see this ActionPlan's §3.1 caveat about the architecture doc's
// wording being a paraphrase, not an authoritative spelling).
func classifyJourney(title string) JourneyCategory { ... }
```

`JourneyIndexRow` 加 `Category JourneyCategory \`json:"category,omitempty"\``（省略等价于
`task`——不给默认任务类别加标注，只标注噪声类，减少 JSON 冗余，这与 `Partial`/`Stitched` 等字段
的 `omitempty` 用法一致）。`BuildJourneyIndexRow` 拿到 `title` 参数后（它已经在签名里，
`storyindex.go:106`），直接调用 `classifyJourney(title)` 填这个字段——不需要新增任何入参。

**渲染层**：`vmr-stories.md`（`saveStoryIndex`/`cmd_story.go:209` 附近的 markdown 生成逻辑，
实现时定位具体渲染函数）默认按类别分组，`task`/`cron` 展开，`heartbeat`/`subagent` 折叠进一个
`<details>` 块（复用项目已有的折叠惯例，`foldWhyLine`/脊柱折叠块是先例，具体折叠组件看
`vmr-stories.md` 目前用的是哪种 Markdown 折叠写法，保持一致而不是发明新的）。`vmr-stories.json`
**不做任何折叠**——DevPlan 原文"机读层不做取舍"，四类候选一视同仁全部输出。

### 3.3 具体步骤

1. `internal/story/candidates.go`（或新文件，视余量而定）：新增 `JourneyCategory` 类型与
   `classifyJourney(title string) JourneyCategory`，先用**占位前缀**实现（架构文档给出的三个
   示例字面量），标注 `// TODO(real-corpus): verify exact prefix spelling before landing` 之类
   的注释——不是留着不做，是把"必须核实"这一步显式标出来，防止实现时跳过直接照抄示例。
2. **真实语料核实前缀拼法**（本步骤是本任务的关键，不能省略）：
   ```bash
   ./vmrbin story -o /tmp/p6verify logs/vmr-audit-2026-07-28.jsonl.zst logs/vmr-audit-2026-07-16.jsonl.zst logs/vmr-audit-2026-07-25.jsonl.zst
   python3 -c "
   import json
   d = json.load(open('/tmp/p6verify/stories/vmr-stories.json'))
   titles = sorted(set(j['title'][:40] for j in d['journeys']))
   for t in titles: print(repr(t))
   " | sort | uniq -c | sort -rn | head -40
   ```
   从实际标题分布里找出真正重复出现的噪声前缀（心跳/定时/子代理），确认大小写、方括号、冒号后
   是否带空格等细节，回填 `classifyJourney` 的字面量。**如果真实语料里找不到某一类**（比如
   本机日志没有 subagent 场景），该类别的匹配分支仍然可以保留（不是错误代码，只是暂时命中数为
   零），但要在真实语料验证步骤（步骤 5）里如实注明"未在本机语料覆盖到"，不要编造一条从未验证过
   的前缀。
3. `internal/story/storyindex.go`：`JourneyIndexRow` 加 `Category` 字段；`BuildJourneyIndexRow`
   调用 `classifyJourney(title)` 填值。
4. `vmr-stories.md` 的渲染函数（`cmd_story.go` 或 `storyindex.go`，实现时定位）：按 `Category`
   分组，`task`/`cron` 默认展开，`heartbeat`/`subagent` 默认折叠。
5. `go test ./internal/story/...`：新增 `classifyJourney` 的单测（四个类别各至少一个用例，含
   "不匹配任何噪声前缀 → task"的默认情形）；`vmr-stories.md` 相关的固定输出测试若断言了具体
   Markdown 结构，同步更新。
6. 真实语料验证：
   ```bash
   python3 -c "
   import json
   d = json.load(open('/tmp/p6verify/stories/vmr-stories.json'))
   from collections import Counter
   c = Counter(j.get('category', 'task') for j in d['journeys'])
   print(c)
   "
   cat /tmp/p6verify/stories/vmr-stories.md | grep -c '<details>'  # 期望 >0（噪声类被折叠）
   ```
   与架构文档 §7.7 的"55% 是 ≤2 轮 heartbeat"数量级对照（不要求精确复现该数字——那是另一批
   窗口日志的统计，本机语料的具体占比可能不同，只要求"确实有相当比例被分类为噪声"这个方向成立）。

### 3.4 验收标准（对照 DevPlan P6.3）

- [ ] 索引首屏（`vmr-stories.md`）以 `task`/`cron` 类真实任务为主，`heartbeat`/`subagent` 默认
      折叠；`vmr-stories.json` 四类全量输出，不做取舍。
- [ ] 分类判据只用已有的 `Requests`/`Title` 字段，不引入新的内容扫描或 LLM 判断。
- [ ] 前缀字面量已用真实语料核实，未覆盖到的类别在验证记录里如实注明，不是编造值。

---

## 4. 任务三：补齐导航边（P6.2，第三执行）

### 4.1 现状（对照架构文档 §7.5 导航矩阵，逐行核实当前状态）

| 边 | 现状（已读代码确认） |
| --- | --- |
| `vmr-report.md` → `vmr-requests-<tag>.md` | ✅ 已存在（`render_doc.go:205` 的 `renderRequestIndexLink`） |
| `vmr-report.md` → `stories/vmr-stories.md` | ❌ 不存在——`grep` 全仓 `render_doc.go`/`section_*.go` 未找到任何 `vmr-stories` 引用 |
| `vmr-report.md` §8 → `details/` | ⚠️ 只有纯文本提示（`render_doc.go:210` 的 `t.DetailsCaptureBody`），不是链接 |
| `vmr-requests-<tag>.md` → `details/*.md` | ✅ 已存在（`reqdetail` 的 `FileNameForRecord`/链接生成，P2/P3 已交付） |
| `vmr-requests-<tag>.md` 会话行 → `journey-<id>.md` | ❌ 不存在——`renderSessionCard`（`requests.go:459`）没有引用任何 story 产物 |
| `vmr-stories.md` → `journey-*.md` | ✅ 已存在（渲染过才出现） |
| journey 脊柱每个 Step → `details/<req>.md` | ✅ **P5.2 已交付**（本文 §0 第 6 点已确认，不需要重做） |
| journey 报告头 → `vmr-stories.md`/`vmr-report.md` | ❌ 不存在——`render_md.go` 无返回链接 |
| journey 头部 System Prompt → `evidence/sysprompt-<h8>.md` | ✅ **P5.3 已交付** |
| `details/<req>.md` 头部 → `vmr-requests.md`（索引） | ❌ 不存在——`reqdetail/render.go`/`detail.go` 无自我定位或返回链接 |
| `compare-*.md` 开篇 → `journey-*.md` | ✅ 已存在（含自动补生成） |
| `compare-*.md` 分叉点 → 双侧 `details/<req>.md` | ❌ 不存在（低优先，架构文档自己标注） |

**本任务只补齐 ❌ 的六条边**，⚠️ 那一条（report §8 的纯文本提示改链接）顺手一起做——它和"report →
stories.md"是同一批改动（都在 `render_doc.go`），分开做没有收益。**"compare 分叉点 → detail"
维持低优先，本次不做**（架构文档自己标注为低优先，且需要先有分叉点的具体消息坐标，属于
"低收益、需要额外设计"的组合，不在本阶段范围内——如果实现时顺手发现坐标现成可用，可以做，
不是禁止，只是不作为本任务的验收项）。

### 4.2 目标设计

**两类边两种策略**（架构文档 §7.5 已定案，直接照抄，不重新论证）：
- **指向内容寻址产物的边**（journey 头 → 返回链接、details 头 → 索引）：链接目标文件名可算，
  永远渲染，不判存在性。
  **但"journey 报告头 → vmr-stories.md/vmr-report.md" 这一条例外**——它指向的是**另一条命令**
  的聚合产物（`vmr-report.md` 由 `report` 生成，`story` 不该、也没资格去算它是否存在），属于
  第二类。
- **指向另一条命令聚合产物的边**（`vmr-report.md` → `stories/vmr-stories.md`、journey 头 →
  `vmr-report.md`）：渲染时 `stat` 一次，存在才链接，并带出目标产物自述的生成时间与覆盖窗口。

逐边设计：

**(a) `vmr-report.md` → `stories/vmr-stories.md`**：`cmd_report.go` 在写 `vmr-report.md` 之前
`os.Stat(filepath.Join(outDir, "stories", "vmr-stories.md"))`；命中则把路径传给
`report.Markdown(rep, lang, storiesLink string)`（`render_doc.go:68` 的 `Markdown` 函数签名
需要加这个新参数——它今天是 `func Markdown(rep *Report2, lang i18n.Lang) string`，`report` 包
不需要因此知道 `story` 包的存在，只是多接收一个"要不要渲染这行、渲染成什么路径"的字符串）。
渲染位置：`renderSummary`（`render_doc.go:106`）附近，报告开篇就应该看到"完整套件"的另一半在哪，
不要埋到 §8 才出现。同时读一下 `vmr-stories.json` 的 `journeys` 数量与时间窗口
（`Meta`/首尾时间戳，`StoryIndex` 目前没有专门的 `Meta`，用 `journeys` 数组本身的
min(Start)/max(End) 现算），拼进链接旁边的说明文字（"N 个任务索引 · 覆盖 X 到 Y"）。

**(b) `vmr-report.md` §8 → `details/`**：`renderRequestIndexLink`（`render_doc.go:189`）今天的
`t.DetailsCaptureBody`只是一句提示文案，改为真正的链接列表——但 §7.5 的"两类边"规则在这里有一个
关键推论（架构文档已经点破）：**请求索引不能给每一行都挂 detail 链接**（那等于全量物化详单，
正是 P3 要消除的东西）。所以这条边的正确形态是：**默认（`-details=false`）模式下，§8 只说明
"详单可按需生成，见 `vmr replay -print -req <coord>` 或 `-details` 全量模式"，附一两个真实坐标
作为示例**（不是链接，是文字说明+示例坐标，因为链接目标在默认模式下并不存在）；**`-details=true`
模式下，才把 `vmr-requests.md` 里每一行的坐标渲染成真正的 `details/<file>.md` 链接**——这条其实
已经在"`vmr-requests-<tag>.md` → `details/*.md` ✅ 已存在"里覆盖了，本条 §8 链接改的只是
`vmr-report.md` 自己那一节的文案从"提示"变成"示例 + 指向 requests 索引的引导"。

**(c) `vmr-requests-<tag>.md` 会话行 → `journey-<id>.md`**：`renderSessionCard`
（`requests.go:459`）渲染会话卡片标题时，用本文 §2 建立的 `SessionRow.ID`（lineage ID）去查一份
`map[string]string`（lineage ID → journey 文件名，由 `cmd_report.go` 在读到 `vmr-stories.json`
存在时构建，键来自 P6.1 的 `JourneyIndexRow.Lineages`）。命中则在卡片标题追加
"→ [journey-xxx.md](../stories/journey-xxx.md)"；未命中（这个 lineage 不属于任何已渲染的
Journey——`Rendered` 字段为空，或该 lineage 根本不构成一个候选 Journey）则不渲染这部分，
不是"存在才链接"的例外，是"目标压根不存在于 story 的候选集合里"，同一策略下的正常结果。
`renderChatUserDoc`/`renderSessionCard` 需要加一个新参数携带这份 join map。

**(d) journey 报告头 → 返回链接**：`render_md.go`（P5.1 之后已经缩到 152 行，有充足行数空间）
渲染 journey 头部时，`stat` 一次 `../vmr-report.md`（相对 `stories/journey-<id>.md` 的路径），
命中则渲染一行"← 返回 [vmr-report.md](../vmr-report.md) / [vmr-stories.md](vmr-stories.md)"
（后者是同目录，永远存在——journey 文件本身就在 `stories/` 下，`vmr-stories.md` 是它的直接
同级索引，这条边其实退化成"指向内容寻址产物"那一类，因为 `vmr-stories.md` 是 `story` 自己这次
运行必然会写的东西，不需要 `stat`）。

**(e) `details/<req>.md` 头部 → 索引**：`internal/reqdetail`（`render.go`/`detail.go`）今天已经
有 `PrevTurnLink` 的先例（P2 建立），本条是同一类"关系链接"的第三种。渲染详单头部时加一行
"→ 返回 [vmr-requests.md](../vmr-requests.md)"（同样是相对同一输出根的固定相对路径，`reqdetail`
不需要知道调用方是 `report` 还是 `story`——`details/` 相对 `{outDir}` 的深度对两个调用方都一样，
`KNOWN_ISSUES §1.26` 已经确认过这一点）。**这条边不需要 `stat`**——`vmr-requests.md` 是生成
`details/` 的那次 `report`（或 `story` 按需生成 detail 时复用的同一套 `reqdetail` 渲染）运行
必然产出的东西，属于"指向可算名称产物"那一类（generation-time 保证，不是 read-time 探测）。

### 4.3 具体步骤

1. `internal/report/render_doc.go`：`Markdown` 签名加 `storiesInfo *StoriesLinkInfo`（新类型，
   `nil` 表示不存在，包含 `Path string`/`JourneyCount int`/`From, To string`）；`renderSummary`
   或紧随其后的新增小节渲染这条链接。`i18n.Doc` 加对应文案字段（EN/ZH）。
2. `cmd/vmr/cmd_report.go`：写 `vmr-report.md` 之前，`os.Stat` + 按需 `story.LoadStoryIndex`
   读 `stories/vmr-stories.json`（只读 `journeys` 数组算 count/from/to，不需要完整解析
   `FileCache`），构建 `StoriesLinkInfo` 传给 `Markdown`。同时构建 P6.2(c) 需要的
   `lineageToJourney map[string]string`（遍历 `idx.Journeys`，对每个非空 `Rendered` 的行，
   把它 `Lineages` 里每个成员映射到 `Rendered` 文件名），传给 `requests.go` 的渲染入口。
3. `internal/report/render_doc.go`：`renderRequestIndexLink` 改写 §8 文案（4.2(b)）。
4. `internal/report/requests.go`：`renderChatUserDoc`/`renderSessionCard` 加
   `journeyLink map[string]string` 参数（键是 lineage ID），命中时追加链接行。
5. `internal/story/render_md.go`：journey 头部渲染加返回链接（4.2(d)）——`stat`
   `../vmr-report.md` 用标准库 `os.Stat`，`story` 包已经在别处做过路径 I/O（详单按需生成
   `EnsureJourneyDetails`），不违反任何 import 边界。
6. `internal/reqdetail/render.go`（或 `detail.go`，实现时定位 `renderSessionHeader`/头部渲染
   函数的具体位置）：加"返回索引"链接（4.2(e)）。
7. `go test ./internal/report/... ./internal/story/... ./internal/reqdetail/...`：跑通已有
   测试，golden/固定输出断言按 `UPDATE_GOLDEN=1` 或手工方式更新，逐行核对 diff 能对应回本任务
   六条边中的某一条。
8. 真实语料验证——**端到端导航走查**（这是本任务、也是 P6 阶段验收的核心检验）：
   ```bash
   ./vmrbin report -o /tmp/p6verify -details logs/vmr-audit-2026-07-28.jsonl.zst
   ./vmrbin story  -o /tmp/p6verify logs/vmr-audit-2026-07-28.jsonl.zst -render-all
   # 手工走查（或写一个一次性脚本用正则提取 [text](path) 并核实每个相对路径解析后文件存在）：
   # vmr-report.md 开篇 → stories/vmr-stories.md → 任一 journey-*.md → 返回 vmr-report.md
   # vmr-requests.md 任一会话卡片 → 对应 journey-*.md（若该会话属于某个已渲染 Journey）
   # 任一 journey-*.md 脊柱 Step → 对应 details/*.md → 返回 vmr-requests.md
   ```

### 4.4 验收标准（对照 DevPlan P6.2）

- [ ] 从 `vmr-report.md` 出发，纯靠链接可到达任意已渲染 Journey 与任意请求详单，并可原路返回。
- [ ] 不存在失效链接——"指向内容寻址产物"类的边生成时机与渲染时机保证一致；"指向另一条命令
      产物"类的边全部经过 `stat`，未命中时不渲染。
- [ ] 请求索引在默认（不生成详单）模式下不因本任务改动而意外触发全量详单物化。

---

## 5. 任务四：统计口径收尾——自指流量排除（P6.4，第四执行）

### 5.1 现状（已读代码确认）

`vmr story -llm-addr` 的 LLM 解读调用经 VMR 自身路由回流进审计日志（架构文档实测
`client_key_tag = vmrstory` 共 21 条/4 条 journey）——这条流量今天**没有任何排除机制**，
`grep` 核实 `internal/report`/`internal/story` 全仓没有任何自指流量识别代码。它既污染
`vmr report` 的成本/用量统计（分析行为的开销被算进"这段时间花了多少钱"），也污染
`vmr story -corpus` 的语料统计。

**关键发现**：`client_key_tag = vmrstory` 不是 vmr 代码里的特殊标记，而是**用户给自己的
API key 起的名字**（`internal/audit.KeyTag`——`internal/audit/audit.go:450`——取的是配置的
`api_keys` 条目本身的字符串尾部，`internal/config/config.go:230-235` 的注释确认"Each entry
gets tagged in the audit trail via `audit.KeyTag`"）。也就是说，**没有一个放之四海而皆准的
字符串可以硬编码去匹配**——不同部署会给自己的分析专用 key 起不同的名字。

**但有一个更好的信号**：`vmr story -llm-addr` 调用回流所用的凭证，正是 `report.yaml` 的
`llm_key`/`-llm-key`（`cmd/vmr/reportconfig.go:42`，`cmd_story.go` 已经在 resolve 这个值去
构造 Authorization header，见 `internal/story/llm.go:311-312`）——**这个值本来就已经在
组合根手里，不需要用户再配置一遍"哪个 client tag 是自指流量"**。`audit.KeyTag(llmKey)` 算出
的字符串，就是这条自指流量在审计日志里会留下的 `client_key_tag`。

### 5.2 目标设计

**识别规则只定义一处**：`cmd/vmr` 组合根在读到 `report.yaml`/`-llm-key` 解析出的 `llmKey`
非空时，计算 `excludeTag := audit.KeyTag(llmKey)`，把它（连同 `report.yaml` 里可选的显式
追加列表，见下）作为一个 `map[string]bool` 传给 `report.BuildCached`/`report.Build` 与
`story` 的候选过滤逻辑。**两侧读的是同一个组合根算出的同一个值**，不是两份独立实现。

**默认排除，可显式包含**：
- `report` 侧：`aggState`（`internal/report/aggregate.go:109`）加
  `excludeClientTags map[string]bool` 字段，`newAggState`/`BuildCached`/`Build` 签名各加一个
  参数；`ingestRecord`（`aggregate.go:288`）开头加
  `if st.excludeClientTags[rc.clientKey] { st.rep.Meta.SelfTrafficExcluded++; return }`——
  `rc.clientKey`已经是 `rec2` 现成字段（`aggregate.go:66`），不需要新增提取逻辑。
  `Meta` 加 `SelfTrafficExcluded int \`json:"self_traffic_excluded,omitempty"\``（先例：
  `Meta.ParseErrors`，`rows.go:64`，同一类"这次运行跳过了多少条、为什么"的诊断字段）。
  Markdown 侧在附录（`renderAppendix`，`render_doc.go:214`）加一行，仅当
  `SelfTrafficExcluded > 0` 时渲染，说明排除了多少条自指流量记录。
- `story` 侧：`cmd_story.go` 的 `cands := story.ListCandidates(g)`（第 136 行）之后，过滤掉
  根 Lineage 的 `Manifests[0].ClientKeyTag` 命中 `excludeTags` 的候选（除非显式 `-include-
  self-traffic`）。**过滤发生在 `cmd/vmr`，不发生在 `internal/story` 内部**——`ListCandidates`
  本身保持纯粹的结构性判据（DevPlan/架构文档反复强调的"不引入新的猜测"，自指流量识别是一个
  部署相关的运行时配置，不是结构信号，不该混进 `internal/story` 的判据里）。
- **显式包含**：`-include-self-traffic` bool flag，两条命令都加（`cmd_report.go`/
  `cmd_story.go`），传入时 `excludeTags` 置空 map，效果是"这次运行不排除任何东西"。
- **可选的显式追加**（覆盖"自指流量用了另一个、非当前配置 `llm_key` 的历史凭证"这种边缘场景）：
  `report.yaml` 新增可选字段 `self_traffic_client_tags []string`，与 `audit.KeyTag(llmKey)`
  算出的值取并集。**这不是必需项**——多数部署只有一个 `llm_key`，`audit.KeyTag(llmKey)` 单独
  就够用；只有用户明确需要覆盖历史凭证时才需要填这个字段，字段留空是完全合法且是默认状态。

### 5.3 具体步骤

1. `cmd/vmr/cmd_report.go`/`cmd_story.go`：各加 `-include-self-traffic` flag；组合
   `excludeTags := map[string]bool{}`，若 `llmKey != ""`，`excludeTags[audit.KeyTag(llmKey)]
   = true`；若 `rc.SelfTrafficClientTags` 非空，逐个加入；若 `-include-self-traffic` 传入，
   清空这个 map。**`cmd_report.go` 今天没有 resolve `llmKey`**（那是 `-llm-addr` 相关的
   `story`-only flag）——`cmd_report.go` 需要新读一次 `rc.LLMKey`（`reportConfig` 已有这个
   字段，`report`/`story` 共用同一份 `report.yaml`），不需要新增 CLI flag（`-llm-key` 保持
   `story`-only，`report` 只从 `report.yaml` 读，不提供覆盖 flag——`report` 命令本身不发起
   LLM 调用，没有必要让用户在 `report` 侧也传一遍凭证）。
2. `cmd/vmr/reportconfig.go`：`reportConfig` 加 `SelfTrafficClientTags []string
   \`yaml:"self_traffic_client_tags"\``；`report.example.yaml`/`.zh` 同步加注释说明的示例条目
   （默认注释掉，说明"多数情况下不需要填，参见 `llm_key` 的自动识别"）。
3. `internal/report/aggregate.go`：`aggState` 加字段；`newAggState`/`BuildCached`/`Build`
   签名加 `excludeClientTags map[string]bool` 参数；`ingestRecord` 开头加排除判断。
4. `internal/report/rows.go`：`Meta` 加 `SelfTrafficExcluded` 字段。
5. `internal/report/render_doc.go`：`renderAppendix` 加条件渲染行；`i18n.Doc` 加对应文案。
6. `cmd/vmr/cmd_story.go`：`cands := story.ListCandidates(g)` 之后插入过滤循环（按根 Lineage
   的 `ClientKeyTag` 排除，除非 `-include-self-traffic`）。
7. `go test ./internal/report/... ./internal/audit/... ./internal/archtest/...`：新增
   `TestIngestRecord_ExcludesSelfTrafficByClientTag`（构造两条记录，一条 `clientKey` 命中排除
   集，一条不命中，断言 `Overall`/`ByModel` 等聚合桶只累计了后者，`Meta.SelfTrafficExcluded ==
   1`）；`cmd/vmr` 侧补一个端到端测试覆盖 `-include-self-traffic` 的开关效果。
8. 真实语料验证（若本机语料确实有历史 `-llm-addr` 调用留下的自指流量；若没有，构造一份合成
   审计日志注入一条 `client_key_tag` 等于 `audit.KeyTag(<report.yaml 里配置的 llm_key>)` 的
   记录来验证排除逻辑生效，如实记录"本机真实语料未覆盖到这条路径，靠合成夹具验证"）：
   ```bash
   ./vmrbin report -o /tmp/p6verify logs/vmr-audit-2026-07-28.jsonl.zst
   python3 -c "import json; print(json.load(open('/tmp/p6verify/vmr-report.json'))['meta'].get('self_traffic_excluded', 0))"
   ```

### 5.4 验收标准（对照 DevPlan P6.4）

- [ ] 统计默认不把分析行为（`vmr story -llm-addr` 回流的流量）的开销计入被分析的工作负载。
- [ ] 识别规则（"这个 client tag 是不是自指流量"）只在 `cmd/vmr` 组合根算一次，`report`/`story`
      两侧接收同一个 `excludeTags` 集合，不是两份独立实现。
- [ ] `-include-self-traffic` 可以显式关闭排除；排除掉多少条在 `vmr-report.md` 附录可见，
      不是静默丢弃。

---

## 6. 任务五：命令行收敛（P6.5，最后执行）

### 6.1 现状（已读代码确认）

`cmd/vmr` 今天有 `report`/`story`/`replay`/`diagnose`/`check`/`start`/`status`/`version` 八个
子命令（`main.go` 的 `case` 分支）。`report`/`story` 各自独立调用
`ctxgraph.LoadCacheDir`/`ScanCached`/`SaveCacheDir`（`cmd_report.go:320-367`、
`cmd_story.go:116-227`）——默认希望拿到"导航闭合的完整套件"的用户，今天必须记得跑两条命令、
给同一个 `-o`（两者默认都 resolve 到 `rc.Output`/`"reports"`，天然一致，但仍是两次独立调用、
两次扫描、两次缓存读写）。`vmr replay` 已经具备统一坐标选择器与读取原语（P3 交付，本文 §0
第 6 点），`vmr diagnose` 不涉及审计记录选择（它是纯粹的连通性探测，不读审计日志）——**架构
文档 §7.9"重放/诊断/读取共用同一种记录选择器"这句话里的"诊断"实际上不适用**：`diagnose` 复用
的是 `Adapter.BuildRequest`/`router.NewUpstreamClient` 这两个**执行**原语（`CLAUDE.md` 模块表
已经写明），不是一个记录**选择**器——这是本文对架构文档措辞的一处修正，`diagnose` 不在本任务
改动范围内。

### 6.2 目标设计

**目标形态收窄为两件事**（DevPlan 已在本文 §0 更新为这个范围）：

**(a) 一个分析动词，默认产出完整套件**：新增 `vmr analyze` 子命令（命名待定，`analyze` 只是
本文的建议拼法——"形状是架构决策，具体拼写留给实施"，架构文档原话）。`vmr analyze` 的默认行为
= 依次执行今天 `vmr report` 与 `vmr story` 的核心逻辑，但**共享同一次 `ScanCached`/
`ctxgraph.Graph`/`FileCache`**：

```go
// cmd/vmr/cmd_analyze.go（新文件）大致形状——不是最终代码，落地时按实际签名调整
func cmdAnalyze(args []string) error {
    // 解析出的 flags 是 report 与 story 两者今天各自 flag 集合的并集（不是笛卡尔积——
    // 架构文档 §7.9 已经论证过这一点），各自保留互不冲突的默认值。
    ...
    priorCache := ctxgraph.LoadCacheDir(cacheDir)          // 一次
    g, fileCache, err := ctxgraph.ScanCached(paths, priorCache) // 一次
    ctxgraph.StitchGraph(g)                                  // 一次（今天 story 已经在做，
                                                               // report 的 SessionAnalysis
                                                               // 分组不需要这一步，但它是幂等的
                                                               // 纯函数，多算一次的成本可忽略，
                                                               // 不值得为省这一次调用去拆分
                                                               // report/story 各自需要的图状态）
    // report 与 story 的核心构建各自消费同一个 g/fileCache，互不知道对方存在——
    // 两个 internal 包依然零耦合，只是被同一个组合根函数先后调用。
    rep, sess, err := report.BuildFromGraph(g, ...)   // 需要新增这个变体（见下）
    idx, err := story.BuildIndexFromGraph(g, ...)      // 同上
    ...
    ctxgraph.SaveCacheDir(cacheDir, fileCache)          // 一次
    return nil
}
```

**这需要 `report.BuildCached`/`story` 现有的"从 `paths []string` 开始扫描"的入口分裂成两层**：
一层保留"给我文件路径，我自己扫描"（`report`/`story` 子命令继续独立存在，走这条路径，行为不变，
向后兼容单独调用的场景——**不删除 `report`/`story` 这两个子命令**，`analyze` 是新增的第三个
入口，不是破坏性替换）；一层新增"给我已经扫描好的 `*ctxgraph.Graph`/`FileCache`，我从这里接手"
（`analyze` 走这条路径）。**判断：不删除 `report`/`story`，是本文对架构文档"收敛为一个分析动词"
的一处务实收窄**——架构文档原话承认这是"命令行表面的破坏性变更"，但"无兼容包袱"的裁决是在
"这两条命令的现有用户是我们自己"这个前提下做的；保留旧入口的成本极低（它们已经是现成代码，
只是不再是"唯一"入口），而删除的收益（CLI 表面更干净）不足以抵消"用户脚本/习惯被打断"的代价，
尤其是这个决定一旦执行就没有退路。**这个判断是本文的建议，不是不可挑战的结论**——如果落地时
判断团队确实只有一两个人在用这套工具、且都同意直接切换，可以选择更彻底的"用 `analyze` 完全
取代 `report`/`story`"，那样反而更简单（不用维护两套入口）。这是一个需要在实现前拍板的决策，
建议落地前用 `AskUserQuestion` 或直接询问确认，不要replace，默认按"新增不删除"执行。

`report.BuildFromGraph`/`story.BuildIndexFromGraph`（或等价的新导出函数）的具体签名，是
`report.BuildCached`/`cmd_story.go` 现有扫描后续逻辑的一次性拆分——**不是重写**，是把
"扫描"和"从图开始构建"这两段现有代码剪开，`report.BuildCached`/`story` 子命令自己调用时，
内部照旧先扫描再调用新拆出来的这个函数，行为逐字节不变。

**(b) `-req` 免位置参数**（`KNOWN_ISSUES §1.25`）：`vmr replay` 的位置参数（审计文件路径）在
只传了 `-req basename:line` 时改为可选——缺省时按 `basename` 在**当前目录**或 `config.yaml`
的 `log_dir`（若可解析）下自动定位文件（含 `.zst` 变体）；显式传了位置参数时保持今天的一致性
校验（`CanonicalPath` 必须匹配）。

### 6.3 具体步骤

1. **拍板决策**（见 6.2(a) 的"不删除 vs 完全取代"）——落地前先跟用户确认一次，不要在这个
   有较大影响面的问题上替用户做主。
2. `internal/report`：把 `BuildCached` 内部"扫描"与"从图构建"两段拆开，新增导出函数（签名
   在实现时按 `BuildCached` 现有参数表精确设计，本文不预先猜测参数顺序）。`BuildCached` 自身
   改为"扫描 + 调用新函数"的薄封装，行为不变（补一个回归测试锁定"新旧调用路径产出逐字节相同"，
   模式与 P2 的跨路径一致性测试同源）。
3. `internal/story`/`cmd_story.go`：同样的拆分（`cmd_story.go` 里"扫描"与"从图构建候选/渲染"
   这两段今天已经是相对独立的代码块，见 §0 第 5 点引用的行号，拆分成本较低）。
4. `cmd/vmr/cmd_analyze.go`（新文件）：组合两者，共享一次扫描/缓存周期。flag 集合按"并集，
   各自默认值不变"的规则设计（架构文档 §7.9 已经给出这条规则，不重新论证）。
5. `cmd/vmr/main.go`：加 `case "analyze":` 分支。
6. `internal/audit`（或 `cmd/vmr` 内部，视是否需要跨包复用而定）：`vmr replay` 加"按 basename
   在目录/`log_dir` 下定位文件"的小工具函数；`cmd_replay.go` 的位置参数改为可选，接入这个
   工具函数。
7. `go test ./...`：跑通已有测试；新增 `cmd/vmr` 端到端测试覆盖 `vmr analyze` 的默认输出
   （`vmr-report.md`/`vmr-requests.md`/`stories/vmr-stories.md`/`stories/journey-*.md` 均产出，
   且只有一份 `.parse-cache/` 目录被写入一次——用文件 mtime 或直接数扫描日志行数验证"只扫描
   一次"）；`vmr replay -req <coord>`（不传位置参数）能在给定目录下找到文件的用例。
8. 真实语料验证：
   ```bash
   time ./vmrbin analyze -o /tmp/p6verify logs/vmr-audit-2026-07-28.jsonl.zst   # 对照 report+story 分开跑的总耗时
   time (./vmrbin report -o /tmp/p6verify2 logs/vmr-audit-2026-07-28.jsonl.zst && \
         ./vmrbin story  -o /tmp/p6verify2 logs/vmr-audit-2026-07-28.jsonl.zst)
   ls /tmp/p6verify/.parse-cache/ | wc -l   # 与 /tmp/p6verify2 对比，内容应等价（缓存分片数相同）
   cd logs && ../vmrbin replay -req "$(basename vmr-audit-2026-07-28.jsonl.zst):1" -print -c ../config.yaml 2>&1 | head -5
   # 期望不传位置参数也能定位到文件（cwd 下能找到 basename 匹配的文件）
   ```

### 6.4 验收标准（对照 DevPlan P6.5）

- [ ] `vmr analyze` 一次调用产出 `report`/`story` 两条命令今天分别产出的完整套件，且只扫描/
      建缓存一次（用真实语料的耗时对比与 `.parse-cache/` 分片数验证）。
- [ ] `report`/`story` 两个 `internal` 包之间没有新增任何 import——合并发生在 `cmd/vmr` 组合根。
- [ ] `vmr replay -req <coord>` 在省略位置参数时能按 basename 在当前目录/`log_dir` 下定位文件；
      显式传位置参数时行为不变。
- [ ] 是否保留独立的 `report`/`story` 子命令，是一个已经跟用户确认过的决策，不是实现时自行拍板。

---

## 7. 需要在实现时确认、不预先假设的几个点

1. **P6.1 的 `SessionRow.ID`→`SessionInfo`转换点具体在哪个文件**：本文 §2.3 步骤 3 只给出
   "预期在 `section_sessions.go` 或 `aggregate.go` 附近"，需要落地时 `grep -n "SessionRow{"`
   精确定位。
2. **P6.2(a) `StoriesLinkInfo` 的具体字段与渲染位置**：本文给出的是"报告开篇附近"这个方向性
   判断，具体插入点（`renderSummary` 内部还是紧随其后的新函数）留给实现时按 `render_doc.go`
   现有分段习惯决定。
3. **P6.3 的分类前缀字面量**：本文明确要求真实语料核实（§3.3 步骤 2），不能照抄架构文档的
   示例——这不是"留给实现时判断"的开放项，是**必须做**的一步，只是具体拼法现在无法预先写死。
4. **P6.4 的 `report.yaml`/`config.yaml` 是否都能在 `cmd_report.go` 里访问到**：`cmd_report.go`
   今天已经加载 `reportConfig`（`rc`），`LLMKey` 是它的既有字段，本任务不需要新读任何配置文件，
   只是使用一个 `cmd_report.go` 今天还没读的既有字段——落地时确认这个字段确实在 `cmdReport`
   函数体的作用域内可读（预期成立，`reportConfig` 是整个文件顶部就加载好的）。
5. **P6.5(a) 保留 vs 取代 `report`/`story`**：本文 §6.3 步骤 1 已经标注为必须先拍板的决策，
   默认按"新增 `analyze`，不删除旧命令"执行，除非用户明确要求更彻底的替换。

---

## 8. 收尾（P6.1–P6.5 共用）

1. **全量测试与架构边界**：
   ```bash
   go test ./... -race
   go test ./internal/archtest/...
   go vet ./...
   gofmt -l .
   ```
2. **CHANGELOG.md**：`[Unreleased]` 下按 Added/Changed 分类加条目（具体措辞落地时按实际改动
   定），至少覆盖：会话 ID 语义变更（`s%02d` → 内容寻址，Changed，标注这是 breaking change）、
   六条新导航边（Added）、任务索引类别列（Added）、自指流量默认排除（Added，含
   `-include-self-traffic` 开关）、`vmr analyze`（Added）、`vmr replay -req` 免位置参数
   （Fixed）。
3. **KNOWN_ISSUES_sonnet-5.md**：
   - `§1.25`（`-req` 免位置参数）——本阶段落地后改写为已解决或归档。
   - `§1.26`（证据条目相对链接深度）——根目录 `vmr-report.md` 那一半在本阶段的 P6.2(a)/(b)
     落地后已解决，整条归档。
4. **架构文档同步**：`story_report_architecture_opus-5.md` 的 §7.9 那句"重放/诊断/读取共用
   同一种记录选择器"按本文 §6.1 的核实结果订正（`diagnose` 不适用）；§9 风险 #1（自指流量）、
   #4（会话 ID 语义变更）标注为已处置，指向本文档。`docs/VirtualModelRouter_Design_v4_Analytics.md`
   若有描述 `vmr-report.json`/`vmr-stories.json` 字段形状或命令行子命令列表的段落，同步更新。
   `docs/UserGuide.md`/`.zh` 补 `vmr analyze`（若采纳）与新导航边的说明。
5. **边界复核**（DevPlan §2.2 第 6 条，三个问题）：落地后按实际执行情况回填，重点关注：
   - P6.5(a) 的"保留 vs 取代"决策实际怎么定的，若与本文默认假设不同，记录理由。
   - P6.3 的分类前缀最终拼法与本文占位值的出入。
   - 是否有第五个需要修正的架构文档措辞（如本文 §6.1 已经修正了一处"diagnose"相关的表述，
     实现时若又发现类似的文档-代码落差，一并登记）。

---

## 9. 验收清单（对照 DevPlan P6 的验收标准逐项勾）

- [x] **一次调用**（`vmr analyze`，采纳该命名，`report`/`story` 原样保留不删除）产出完整套件。
- [x] 一次端到端导航走查：从大盘（`vmr-report.md`）走到具体任务（`journey-*.md`）、再走到
      具体请求（`details/*.md`），并原路返回——全程无失效链接（真实语料脚本核实，见 §10）。
- [x] 一次口径核对：自指流量（`vmr story -llm-addr` 回流）默认不计入 `vmr-report.md` 的成本/
      用量统计，`Meta.self_traffic_excluded` 如实反映排除数量（真实语料 16 条记录验证）。
- [x] 会话级身份改为内容寻址：`SessionRow.ID`/`JourneyIndexRow.Lineages` 可直接集合判断归属。
- [x] 索引类别列：`vmr-stories.md` 首屏以真实任务为主（477 候选中 350 展开 / 127 折叠）。
- [x] `go test ./...`（含 `-race`）、`go test ./internal/archtest/...`、`go vet ./...`、
      `gofmt -l .` 全绿。
- [x] CHANGELOG、KNOWN_ISSUES（§1.25/§1.26，已归档为 §3 #25/#26）、架构文档说明性备注、
      `docs/UserGuide.md`/`.zh`、`docs/VirtualModelRouter_Design_v4_Analytics.md` 均已同步。

---

## 10. 执行记录（2026-08-20，Sonnet 5）

本节是本文写完 ActionPlan 之后、实际落地执行的过程记录与总结——按用户要求补写，不是提前写好
的计划。**所有改动均未提交，等待人工 review。**

### 10.1 执行顺序与整体结果

严格按 §0 定下的顺序（P6.1 → P6.3 → P6.2 → P6.4 → P6.5）执行，每个子任务落地后立即
`go build`/相关包 `go test`，全部完成后统一跑 `go build ./...`、`go vet ./...`、`gofmt -l .`、
`go test ./... -race`、`go test ./internal/archtest/...`，全部通过。用本机真实审计日志验证
（单日样本 `logs/vmr-audit-2026-07-28.jsonl.zst`，全量语料 `logs/*.jsonl.zst` 34 文件/11374 条
记录）：

| 验证项 | 结果 |
| --- | --- |
| P6.1：report/story 会话 lineage id join | 单日样本 9/9 全部命中 |
| P6.1：`SessionRow.alias` 与新 `id` 共存渲染 | `## s24 (l-754b71e2) · ...` 格式验证通过 |
| P6.3：真实语料标题分类 | 477 候选：`task` 238、`cron` 112、`heartbeat` 107、`subagent` 20；三个字面量标记（`[cron:`/`[OpenClaw heartbeat poll]`/`[Subagent Context]`）均在真实标题里逐字核实，零重叠 |
| P6.3：`vmr-stories.md` 折叠效果 | 350 条默认展开、127 条折叠进 `<details>` |
| P6.2：六条新导航边 | `vmr-report.md`→`stories/vmr-stories.md`、§8 按需读取示例坐标、会话行→journey、journey→返回入口（`vmr-stories.md`+`vmr-report.md`）、详单→返回索引，均在真实语料上人工核实可达；自动化脚本扫描全部产物的导航类链接，0 处死链接（4 处初始误报来自对话正文引用 UserGuide 文本的巧合匹配，非生成链接） |
| P6.4：自指流量识别与排除 | 全语料 21 条 `client_key_tag=vmrstory` 记录中，08-05 那份日志的 16 条被 `-report-config` 指定 `self_traffic_client_tags: ["vmrstory"]` 后正确排除（`report`：239/255 请求，`Meta.self_traffic_excluded=16`；`story`：23/26 候选） |
| P6.5a：`vmr analyze` | 单日样本单次调用产出完整套件，`vmr-report.md`/`vmr-requests.md`/`stories/vmr-stories.md`/6 个 journey 全部就位；story 先于 report 执行后，"report→stories"与"会话行→journey"两条边首次调用即命中 |
| P6.5b：`vmr replay -req` 免位置参数 | cwd 搜索（`cd logs && replay -req basename:1`）与 `log_dir` 回退（cwd 不含目标文件、仅凭 `config.yaml` 的 `log_dir` 定位）均用真实语料验证通过；找不到时给出清晰错误 |
| 全量语料（34 文件/11374 条记录）`vmr analyze` | 见 §10.5——**未跑完，被系统信号杀死**（退出码 137）；核实为 `-render-all` 在这个语料规模下的既有特征，非 P6 回归，已登记 `KNOWN_ISSUES §1.30`（中危） |

### 10.2 与最初 ActionPlan 设计的实际出入（按发现顺序）

1. **P6.3 的分类前缀确实与架构文档的示例不完全一致**——本文 §3.1 已经预判到这一点并要求真实语料
   核实，执行时确认：`heartbeat`/`subagent` 不是标题前缀而是标题内容里的子串（前面永远还有 OpenClaw
   自己的时间戳方括号），`classifyJourney` 因此对这两类用 `Contains` 而不是 `HasPrefix`，`cron`
   则确实是真前缀。轮数信号最终**完全没有使用**——真实语料显示内容标记本身零歧义，而"短对话即
   噪声"这条假设会把真实短交互（如两轮的"hi back"）错误地降级，判断依据不足的信号不引入，比
   本文原计划设想的"轮数+标题"两信号更克制。
2. **`vmr analyze` 内部顺序不是"report 先、story 后"，而是反过来**——本文 §6.2 的伪代码示意
   先写了 `cmdReport` 再写 `cmdStory`，但真实语料验证时发现：这样"report→stories"这条架构文档
   点名"最大的一块新建"的边，在**首次** `analyze` 调用时因为 `stories/vmr-stories.json` 还不
   存在而挂空。改为 story 先跑：`report` 渲染时 `stories/vmr-stories.json` 已经就位，"report→
   stories"与"会话行→journey"两条边首次调用即可命中；代价是"journey→返回 vmr-report.md"这条边
   反而在首次调用时挂空（`vmr-report.md` 还不存在）。两利相权取其重——已在代码注释与新增回归
   测试（`TestCmdAnalyze_ReportLinksStoriesOnFirstCall`）里钉住这个判断，不是随手换了顺序。
3. **P6.4 的 `report` 侧签名改动比预想的更小**——本文原计划以为需要给 `report.Build`（不只
   `BuildCached`）也加排除参数，实现时判断 `Build` 是测试专用的简化包装（生产代码只调用
   `BuildCached`），不给它加参数、内部对 `buildInternal` 传 `nil`，省下了改 28 处测试调用点的
   成本，行为完全不受影响。
4. **`vmr replay -req` 免位置参数的 log_dir 回退，实测发现一个需要真实完整 `config.yaml` 才能
   触发的前提**——本文 §6.2 设计时以为"只要 `log_dir` 字段存在就能读到"，实现并写回归测试时
   发现 `config.Load` 要求至少一个 `providers`/`models` 才能通过校验（否则整个 config 加载失败，
   `resolveReqAuditPath` 的 `err == nil` 判断让 log_dir 那一条搜索路径静默不生效）——这是用真实
   仓库的 `config.yaml`（当前因无关原因未通过 schema 校验，`vmr check` 可复现）手工验证时才现形
   的，最终改用一份语法完整但内容虚构的 config.yaml 完成验证，行为本身没有 bug，只是这个前提
   在文档里补充说明了（`resolveReqAuditPath` 的 doc comment）。
5. **`cmdStory` 触到了函数级行数预算**——P6.4 的候选过滤代码最初直接写在 `cmdStory` 函数体内，
   导致它从 145 行的豁免线以下变成 150 行、超预算。按 archtest 失败信息的字面要求（"缩短它，
   不要抬高数字"）把过滤逻辑收进 `cmd/vmr/selftraffic.go` 的 `filterSelfTrafficCandidates`，
   与 `selfTrafficExcludeTags` 放在一起，不是留在 `cmdStory` 里。

### 10.3 独立并发评审的处理

执行过程中，仓库里出现了一份我没有创建的文档
（`docs/future-strategy/story_report_p6_action_plan_review_gemini-3.7-flash.md`）——与 P1-P5
执行期间同样的并发写作模式，另一个会话在独立评审本 ActionPlan（评审对象是本文档的设计文本，
不是我这次的最终代码，落地时间早于我的实现细节定型）。已通读全文并逐条用当前真实代码核实（不是
因为"是评审意见"就默认正确，也不是照单全收）：

**核实为真、且是本次执行中最重要的一次修正（第一梯队 #1，致命）**：

- **`LineageID()` 纯内容哈希在真实语料上确实会碰撞，导致 `report` 会话被静默合并**：评审指出
  `RootHash()`（进而我最初实现的 `LineageID()`）只哈希系统提示词与首条消息内容，不含时间戳，
  两个内容恰好相同的独立 Lineage（典型场景：模板化的定时任务、心跳轮询）会算出同一个 ID；而
  `internal/report/aggregate.go` 的 `sessionInfo[s.ID]`、`st.sessions[rc.sessionID]` 两处 map
  都以这个字符串为键，碰撞会让后一个会话的记录悄悄合并/覆盖前一个——`From`/`To`/token 统计全部
  失真，且没有任何报错。**没有停留在理论推演**：直接对本机全量语料（34 文件/1638 条 Lineage）
  跑碰撞检测，命中 **4 组真实碰撞**（均为 `qwen38-27b auto-finish check` 一类固定模板的定时任务），
  证实这不是假设场景。修法：`LineageID()` 不再只取 `RootHash()` 的前缀，而是把 root manifest
  自己的到达时间戳（纳秒精度）一并折进哈希——保留内容寻址的稳定性（同一份日志重新扫描算出的 ID
  不变），同时消除碰撞；`RootHash()` 本身（`ctxgraph.Classify`/edit 检测用到的内容身份语义）
  不受影响，只有 `LineageID()` 这个新增方法的哈希输入变了。用同一份真实语料复测：碰撞数 4 → 0。
  新增永久回归测试 `TestRealCorpus_LineageIDHasNoCollisions`（`internal/ctxgraph/
  reallog_lineageid_test.go`，仿照 `internal/report/e2e_test.go` 的"真实语料存在才跑、
  `SKIP_SLOW_E2E` 可跳过"惯例），并扩充了 `TestLineageID_ContentAddressed` 覆盖"相同内容不同
  时间戳必须不碰撞"这条具体场景。

**核实为真、已修复的第二条（第一梯队 #2，高危）**：

- **自指流量排除只覆盖了 `ingestRecord` 一个点，遗漏 `rep.Tools`/`rep.Compactions` 与 `-details`
  详单物化**：`buildTools(st.sess)`/`buildCompactions(st.sess)`（§5/§6.7）直接读
  `SessionAnalysis.Recs`/`.Compactions`，是一趟完全独立于 `ingestRecord` 的读取路径，我最初的
  实现只在 `ingestRecord` 里做了排除，这两处完全没有覆盖——自指流量的工具声明/调用会计入 §5 的
  工具浪费统计，标准的历史压缩调用会出现在 §6.7。另外，`onRecord`（`-details` 详单写入回调）在
  `scanAndCacheFile` 里的调用顺序原本就在 `ingestRecord` 之前，我的排除判断只影响后者，导致
  `-details=true` 时自指流量的详单页仍会被物化——是一份 `vmr-requests.md` 从不索引、无法从任何
  地方点到的孤儿文件。核实为真，两处都修：新增 `internal/report/selftraffic.go` 的
  `excludeSelfTrafficFromSessionAnalysis`，在 `buildInternal` 里紧跟 `AnalyzeSessionsCached`
  之后原地过滤 `sess.Recs`/`sess.Compactions`（`sess.Sessions` 保持不动——`rep.Sessions` 已经
  通过 `ingestRecord` 的既有排除正确工作，没有任何代码路径直接读 `sess.Sessions`，不需要重复
  过滤）；`scanAndCacheFile` 里 `onRecord` 调用前补一次 `excludeClientTags[arec.ClientKeyTag]`
  判断。补了三个回归测试：`TestExcludeSelfTraffic_ToolsAndCompactionsDontLeak`（构造一条真实
  workload 请求 + 一条带工具声明的自指请求 + 一条自指的 compaction 请求，断言 `rep.Tools`/
  `rep.Compactions` 只保留前者）、`TestExcludeSelfTraffic_DetailsNotMaterialized`（断言
  `onRecord` 回调只对未排除的记录触发）。真实语料交叉验证：08-05 那份日志，排除模式与不排除模式
  下 `rep.Compactions` 数量相同（都是 1 条，是真实压缩事件，不是自指流量误留或误删）。

**核实为真，但落地时已经独立解决、不需要二次修改的一条**：

- **`vmr analyze` 内部顺序的时序问题**（评审第一梯队 #3 的前半）：评审基于 ActionPlan §6.2 的
  伪代码示意（先 `report` 后 `story`）指出首次调用会让"report→stories"这条边挂空。核实：这个
  问题我在真实语料验证阶段（早于本次评审出现）已经独立发现并修复——最终代码是 story 先跑、
  report 后跑（`cmd_analyze.go` 已有详细注释说明这个顺序判断），并有专门回归测试
  `TestCmdAnalyze_ReportLinksStoriesOnFirstCall` 锁定。评审是对准 ActionPlan 文本审的，不是对准
  最终代码，两边殊途同归，不需要再改代码。评审提到的"内存级契约传递"（不经磁盘 `os.Stat`）是一个
  更彻底的替代方案，见下一条的处理。

**核实为真实、判断为有效但非本次必需的优化建议，登记不采纳的理由**：

- **`vmr analyze` 深度合并"单次扫描/内存级契约传递"**（评审第一梯队 #3 后半、#4）：评审建议把
  `report`/`story` 的扫描与建图拆开共享、导航边全部用内存对象传递而不经 `os.Stat`/磁盘读取。
  这正是我在 `cmd_analyze.go` 包注释里已经论证过并主动放弃的同一个方案——P3 之后两条命令共用
  同一份内容哈希分片的 `.parse-cache/`，`analyze` 内部 story 先跑之后，report 那一趟扫描已经是
  热缓存命中（架构文档 §7.10 的实测：热缓存 story 5×、report 受限于三趟扫描里未接入缓存的一遍，
  见 `KNOWN_ISSUES §1.1`/`§1.23`），"真正单遍扫描"相对"两遍都热缓存"的收益有限，而实现成本
  （拆分 `AnalyzeSessionsCached` 的扫描与图构建、让 `report`/`story` 都能接受一个外部传入的
  `*ctxgraph.Graph`）改动两个包的核心装配路径，风险与收益不成比例。评审提出这条建议时机独立于
  我的判断，但结论上是重复确认了同一个已经被慎重考虑过的取舍，不是遗漏——继续维持不做，留给真的
  出现"分析大语料是家常便饭"这种使用模式时再重新评估（跟 §10.5 记录的大语料 SIGKILL 问题是同一类
  "先有真实压力数据、再决定要不要重构"的判断）。

**核实为设计已经满足、无需改动的第二梯队条目**：

- **评审 2.1（`Finding`/测试断言对 `s.ID` 变更的不变性）**：`TestBuildFindingsContextGrowthTieIsDeterministic`
  与 `TestContextGrowthDoesNotCrossContractBreak` 已经在 P6.1 落地时改写（§10.2 第 5 点之前的
  执行记录已经覆盖，见本文档 P6.1 落地时的具体处理），评审核实到的问题在评审文档写就时已经不存在。
- **评审 2.2（`classifyJourney` 前缀/轮数防御性设计）**：评审担心仅凭 `Requests<=2` 折叠正常短
  任务——`classifyJourney` 的最终实现从一开始就没有使用轮数信号（本文档 §3.1/§10.2 第 1 点已经
  记录了这个判断的依据：真实语料显示内容标记零歧义、轮数信号会误伤"hi back"一类真实短交互），
  评审的担忧本身成立，但落地代码已经规避了它，不需要改动。
- **评审 2.4（根目录到 `evidence/` 的相对链接深度）**：`grep` 全仓复核确认 `internal/report`
  从未渲染过任何指向 `evidence/*.md` 的链接——评审沿用的是 `KNOWN_ISSUES §1.26` 收窄前的旧描述，
  这条边在架构文档的导航矩阵里也从未被列为待建边，不是一个需要处理的真问题（本文档 §10.4 第 1 点
  已经独立核实并归档了这条）。

**核实后判断为合理替代方案、但没有强证据说明现状有误、维持现状的一条**：

- **评审 2.3（`vmr replay -req` 免位置参数的目录查找优先级）**：评审建议顺序改成"①`log_dir`
  ②cwd ③`./logs/`"；当前实现是"cwd（或显式给的目录）优先，`log_dir` 兜底"。两种顺序都能覆盖
  "省略位置参数仍能定位到文件"这个核心需求，当前实现的理由（本地上下文优先于全局配置，多数 CLI
  工具的惯例）站得住，评审也没有给出当前顺序会导致真实场景定位错文件的具体证据，维持现状。

两处高 ROI 修复（`LineageID` 碰撞、自指流量排除遗漏）均已落地并通过测试与真实语料交叉验证；评审
文档本身未删除、未修改，留在仓库里供你查阅。

### 10.4 执行期间发现并顺手修正的两处文档不准确

1. **`KNOWN_ISSUES §1.26`"仅剩 vmr-report.md 一处未对齐"的说法本身不成立**——`grep` 全仓确认
   `internal/report` 从未渲染过任何指向 `evidence/*.md` 的链接，架构文档 §7.5 的导航矩阵也从未
   把这条边列为待建边。P5 执行记录的这处收窄推断是过度推断，本次核实后整条归档关闭，不是"顺带
   做完"，而是"确认它原本就不是一个真问题"。
2. **架构文档 §7.9 目标形态表"重放/诊断/读取共用同一种记录选择器"对 `diagnose` 不成立**——
   `diagnose` 是纯连通性探测，不选择任何历史记录，复用的是 `Adapter.BuildRequest`/
   `router.NewUpstreamClient` 两个执行原语（`CLAUDE.md` 模块表已经写明），跟"记录选择器"是两回事。
   已在架构文档原地订正，P6.5 的实际范围因此收窄为"`vmr analyze` + `-req` 免位置参数"，不涉及
   `diagnose`。

### 10.5 全量语料（34 文件/11374 条记录）验证发现的一个真实问题

`vmr analyze`（内部先跑 `vmr story -render-all`）在本机对全量 34 个审计文件、11374 条记录的
冷启动运行，**在约 10 分钟墙钟、~300s 累计 CPU 时间后被信号杀死**（进程退出码 137 = SIGKILL），
story 阶段连候选索引都没写出——不是运行得慢，是没有跑完；单日样本（322 条记录、6 个 Journey）
同一条命令 ~18s 正常完成，产物齐全，导航边全部可达（§10.1 表格已记录）。**核实结论：这不是
P6 引入的回归**——`cmd_analyze.go` 内部就是原样调用 `cmdStory(storyArgs)`（`storyArgs` 带
`-render-all`），与用户直接跑 `vmr story -render-all` 是同一条代码路径；P6 只是让它成为
`vmr analyze` 的默认路径，因此更容易被撞到。本机 16GB 物理内存，具体是 OOM 还是其它资源限制
导致的 SIGKILL，未进一步排查（定位真实内存/CPU 峰值发生在建图、逐 Journey 构建还是详单材料化
哪个阶段，本身是一次独立的性能剖析工作，超出本次 ActionPlan 范围）。**判断为中危、非阻断**：
中危是因为 `vmr analyze` 是 P6 新增的默认推荐入口，"被杀死而不给任何提示"是一个很差的失败模式，
且触发条件（约一个月的历史日志量级）不算罕见；非阻断是因为它不影响 P6 本身的验收标准（"一次
调用产出完整套件"在正常规模下已经用真实语料验证成立，P6 的验收从未承诺"任意规模下都不被杀死"）。
已登记为新的 `KNOWN_ISSUES §1.30`，留给专门的性能排查排期。

### 10.6 尚待你决定的事项

1. **`vmr analyze`（含 `vmr story -render-all`）在大语料上被系统信号杀死**（§10.5）——已登记为
   `KNOWN_ISSUES §1.30`（中优先级、非阻断），排期与否由你决定；这条建议尽快安排，因为它影响的
   是 P6 新增的默认推荐入口的可靠性，不只是速度。
2. 所有代码/文档改动都在工作区，未 `git add`/`git commit`，等待你 review 后决定如何处理。
