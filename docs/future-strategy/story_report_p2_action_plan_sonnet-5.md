// Ver 2026-08-19 23:59, by Sonnet 5

# vmr 日志分析体系重构 — P2 ActionPlan（坐标层与微观层重建）

## 0. 定位

本文是 `docs/future-strategy/story_report_dev_plan_opus-5.md` 里 **P2 阶段**的执行级细化，基于
本仓库 P2 起点（P1 已完成，commit `30c5159`）的真实代码状态编写。架构依据见
`docs/future-strategy/story_report_architecture_opus-5.md` 的 §7.3（坐标层三件套）、§7.6(a)
（detail 做减法与下沉）、§7.11 批 3/批 4、§8 决策表对应各行。

**DevPlan review 结论**（本次 review 的产出，先记在这里）：P2–P6 的任务范围、边界、验收标准
在当前代码库上逐条核实后**均仍然成立，不需要改动**——`internal/report/detail.go` 仍是
1047/1150 行、`RequestRow.Path`/`Line` 仍是 `json:"-"` 未发布、`ctxgraph.FileCache` 仍以调用方
传入的原始路径字符串为 map key（未规范化）、`SessionInfo.ID` 仍是 `s%02d`、无任何自指流量过滤
逻辑。DevPlan 本身只做了 P0/P1 完成状态的登记，P2 及以后的任务表原文不动。

**P2 范围边界**（与 DevPlan 一致）：坐标的定义与发布、解析缓存键的规范化、详单渲染的减法与下沉、
详单命名规则。**不改变**详单的生成时机与产物集合（`-details` 仍默认 `true`、`WriteDetails`/
`Build` 的 onRecord 钩子仍在同一批次全量物化——那是 P3.3 的工作）；**不**把 `vmr story` 接成
详单的新调用方（那是 P3.3/P5.2 的"按需生成"），P2.3 的跨路径一致性验收用测试证明，不要求
`vmr story` 长出新 CLI 能力。

**通用收尾要求**（DevPlan §2.2，每个任务完成后都要过一遍，本文 §5 统一收口）：全量测试 + archtest
通过、真实日志肉眼核对、golden/固定断言更新且可解释、CHANGELOG 条目、KNOWN_ISSUES 登记、边界复核。

---

## 1. 执行前置检查

```bash
git status --short                     # 确认工作区干净，P1 那批改动已在 HEAD（30c5159）
go build -o /private/tmp/claude-501/-Volumes-SSD2T-code-vmr/*/scratchpad/vmrbin ./cmd/vmr
go test ./... 2>&1 | tail -30           # 建立改动前的基线：全绿
```

三项任务有**真实依赖顺序**（与 P1 不同）：P2.1（坐标定义）必须先做——P2.2/P2.3 都要用
`ctxgraph.Classify`/规范化路径的产物。P2.2（detail 内容做减法）与 P2.3（渲染下沉 + 命名重建）
建议合并成一次改动而不是分两轮提交：减法减掉的字段（`SessionID`/`TaskID`/`Compaction` 等）与
下沉时要重写的函数签名是同一批改动的两面，分两次提交会在中间留一个"签名已改但字段还没删干净"
的过渡态，golden/断言要改两次。

每做完一个子步骤跑一次 `go test ./internal/report/... ./internal/story/... ./internal/ctxgraph/... ./internal/archtest/...`，不要攒到最后一起查错——这三个包在 P2 里改动面比 P1 大。

---

## 2. 任务一：请求级坐标定义与发布（P2.1）

### 2.1 现状（已读代码确认）

- `ctxgraph.Manifest.Path`/`.Line`（`manifest.go:20-21`，`json:"path"`/`json:"line"`）已经是
  跨记录唯一的坐标原料，但存的是调用方传入的**原始路径字符串**（`BuildManifest(rec, path, line)`
  第 86 行直接 `Path: path`），从未规范化。
- `ctxgraph.ScanCached`（`cache.go:85`）的 `FileCache.Files map[string]CachedFile` 同样以调用方
  传入的原始 `path` 为 key（`cache.go:117` `next.Files[res.path] = res.entry`）——同一份日志用
  绝对路径跑一次、相对路径跑一次，会在这个 map 里产生两条重复记录（架构文档 §2.2 实测 255 条
  manifest 重复，样例即此）。
- `internal/report/rows.go:502-503` 的 `RequestRow` 已经带着 `Path string`/`Line int` 两个字段，
  但 `json:"-"` ——数据已经在，只是没有发布。
- `internal/report/session.go` 里有**两个**独立的 `(path, line)` 查找点，都要跟着规范化：
  - `collect(rec, path, line, prof)`（`session.go:372`）：构造 `ReqInfo{Path: path, Line: line, ...}`，
    是 `RequestRow.Path`/`Line` 的最终来源。
  - `SessionAnalysis.Lookup(path, line)`（`session.go:172-173`）：`return a.byKey[fmt.Sprintf("%s\x00%d", path, line)]`。
    `byKey` 本身在 `AnalyzeSessionsCached` 里用 `r.Path`/`r.Line`（即 `collect` 已经写好的值）建
    （`session.go:261`）。`Lookup` 有两个跨包调用方，都是**各自独立扫描同一批 `paths []string`
    时的本地循环变量**，直接传未规范化的原始 `path`：`aggregate.go:212`
    （`st.sess.Lookup(path, line)`）与 `detail.go:245`（`sess.Lookup(path, line)`，`WriteDetails`
    自己的第二遍扫描循环）。
- **关键约束，落地时不能踩**：`audit.OpenLogFile(path)`（`scan.go`/`WriteDetails` 都在用）需要
  **能实际打开文件**的路径（含目录、含 `.zst` 后缀）。规范化只能作用于"作为坐标/key 用"的字符串，
  绝不能替换掉用来做文件 I/O 的那个 `path` 变量——这是本任务最容易踩的坑。
- 唯一的压缩后缀是 `.zst`（`internal/audit/housekeep.go:19`
  `auditFileRE = ^vmr-audit-(\d{4}-\d{2}-\d{2})\.jsonl(\.zst)?$`），不需要处理 `.gz` 等其它形式。
- `ctxgraph.Classify(prev, cur *Manifest) Edit`（`edit.go:117`）已经是不依赖 report/story 状态的
  纯函数——`session.go:650-651` 的 `e := ctxgraph.Classify(p.manifest, r.manifest); r.DeltaStart =
  r.manifest.LeadSys + e.LCP` 就是现成的调用范例，P2.2/P2.3 的"叶子重算 DeltaStart"直接照抄这行。
- `internal/story/journey.go:566-575` 的 `deriveID` 已经有"用记录自带时区格式化时间戳、不走
  `fmtutil.DisplayZone`"的先例（`root.TS.Format(idTimeLayout)`，没有 `.In(...)` 转换），P2.3 的
  文件名时间戳直接复用这个模式，不需要发明新机制。
- `internal/report/session.go:507-508` 的 `toolsSig`（`fmt.Sprintf("tools:%d/%x", len(names),
  sum[:4])`，md5 取前 4 字节）是项目里"内容寻址取前 4 字节渲染成 8 位十六进制"的既有先例，
  P2.3 的 `hash8` 直接照抄这个口径。

### 2.2 目标设计

**规范化函数放在 `internal/ctxgraph`**（不是新叶子包，也不是 `internal/core`）：两侧本来就已经
`import ctxgraph`（`report/session.go` 早就在用 `ctxgraph.Manifest`/`Classify`），坐标的原料
（`Path`/`Line`）也是 `ctxgraph.Manifest` 自己的字段，架构文档 §7.3 明确写"都不改任何 import
边界（两侧都已依赖 `ctxgraph`）"，放在这里零新增依赖边。

```go
// internal/ctxgraph/reqcoord.go（新文件，纯函数，无新依赖）

// CanonicalPath normalizes a scan input path to its coordinate form: the
// basename with any compression suffix stripped, so a log file's identity
// survives housekeeping's plain→.zst rotation. It is NEVER the string to
// pass to os.Open/audit.OpenLogFile — those need the real, resolvable path;
// this is only for identity (cache keys, published coordinates).
func CanonicalPath(path string) string {
    return strings.TrimSuffix(filepath.Base(path), ".zst")
}

// ReqCoord formats the request-level coordinate: CanonicalPath(path) + ":"
// + line. line is the 1-based logical line ctxgraph already uses
// (Manifest.Line / audit.ForEachLine's counting) — no new convention.
func ReqCoord(path string, line int) string {
    return CanonicalPath(path) + ":" + strconv.Itoa(line)
}

// Req is m's own coordinate — m.Path is already canonical by construction
// (BuildManifest normalizes it), so this is just formatting.
func (m *Manifest) Req() string { return m.Path + ":" + strconv.Itoa(m.Line) }
```

**四个规范化注入点**（只在这四处改，其余调用方不用碰——见 §2.1 已经确认的调用链）：

1. `manifest.go` 的 `BuildManifest`：`Path: path` → `Path: CanonicalPath(path)`。
2. `cache.go` 的 `scanCachedFile`（或 `ScanCached` 循环体）：`HashFile(path)`/`scanFile(path)`
   继续吃原始 `path`（要真的打开文件），但返回值/写入 `next.Files`/`prior.Files` 时的 key 换成
   `CanonicalPath(path)`。
3. `session.go` 的 `collect`：`Path: path` → `Path: ctxgraph.CanonicalPath(path)`。
4. `session.go` 的 `Lookup` 方法本身：`a.byKey[fmt.Sprintf("%s\x00%d", path, line)]` →
   `a.byKey[fmt.Sprintf("%s\x00%d", ctxgraph.CanonicalPath(path), line)]`——**规范化收在方法内部**，
   这样 `aggregate.go:212`、`detail.go:245`（`WriteDetails` 自己的扫描循环）两个调用方**一行都不用改**，
   它们继续传各自循环里的原始 `path`，`Lookup` 内部转换后去匹配 `collect` 已经写好的规范化 key。
   这是本任务里最容易走偏的一步——如果反过来去改三个调用方各自规范化一遍，等于把同一条规则实现
   三次，且容易漏改一处导致 `byKey` 查找静默 miss（`ReqInfo` 变 nil，`renderSessionHeader`/
   `roleStatLine` 等下游会以为这条记录没有 session 上下文，不会报错，只会安静地退化）。

**同名冲突断言**：`Scan`/`ScanCached` 入口处加一个前置校验——对 `paths []string` 里每个路径算
`CanonicalPath`，若两个不同的原始路径规范化到同一个 basename，直接返回错误（響亮失败，不是新建
一条更复杂的消歧规则）。建议实现成 `ctxgraph.CheckPathCollisions(paths []string) error`，`Scan`
与 `ScanCached` 各自在函数体最开头调用一次（两处独立调用，不是共享一次结果——`Scan`/`ScanCached`
本来就是两个不共享调用栈的入口）。

**`RequestRow` 发布坐标**：`rows.go` 把 `Path string \`json:"-"\`` / `Line int \`json:"-"\`` 换成
新增一个 `Req string \`json:"req,omitempty"\`` 字段（`Path`/`Line` 两个原字段保留 `json:"-"`
不变——它们仍是 `Lookup`/`byKey` 内部用的原始坐标，不需要跟着对外发布，对外只发布拼好的
`req` 字符串，避免外部消费者自己再拼一遍拼错分隔符）。在 `RequestRows`（`rows.go:506` 附近，
构造每一行 `RequestRow` 的地方）填这个字段：`Req: ctxgraph.ReqCoord(r.Path, r.Line)`。

**Story 侧不需要新增任何 JSON 字段就已经可以按坐标 join**：`vmr-stories.json` 的 `files` 段
（`ctxgraph.FileCache`）本来就序列化每个文件的全部 `Manifest`，其 `Path`/`Line`
（`json:"path"`/`json:"line"`）在 P2.1 落地后已经是规范化后的坐标原料——外部脚本用
`path + ":" + line` 就能拼出与 `vmr-requests.json` 的 `req` 字段完全相同的字符串，不需要
`vmr-stories.json` 再额外发布一个 `req` 字段（那会是纯冗余，违反项目"不重复存"的原则）。
**这与架构文档 §7.3(a) 举的 `journey-<id>.json` 里 `{"seq":7,"req":"..."}` 示例不矛盾**——那是
P4.1（"每步携带坐标"）要往 `JourneySummary` 里新增的 Step 级结构，P2 不改 `journey-<id>.json`
的形状；P4 落地时，Step 级的 `req` 字段用同一个 `Manifest.Req()`/`ReqCoord` 计算，不需要
重新发明格式。**跨命令 join 的验收（本节验收标准第一条）用 `vmr-requests.json` 的 `req` 字段
对 `vmr-stories.json` 的 `files[*].manifests[*].{path,line}` 做，不依赖 journey JSON。**

### 2.3 具体步骤

1. 新建 `internal/ctxgraph/reqcoord.go`：`CanonicalPath`/`ReqCoord`/`(*Manifest).Req`/
   `CheckPathCollisions`，各配一个直接的单测（含"`.zst` 后缀被剥离"“两个不同目录下同名文件触发
   collision 错误”两个用例）。
2. `manifest.go`：`BuildManifest` 改 `Path: CanonicalPath(path)`。
3. `cache.go`：`scanCachedFile` 与 `ScanCached` 的 map key 改为 `CanonicalPath(path)`；`Scan`/
   `ScanCached` 入口各加一次 `CheckPathCollisions(paths)` 调用。
4. `session.go`：`collect` 改 `Path: ctxgraph.CanonicalPath(path)`；`Lookup` 方法内部规范化
   （§2.2 第 4 点）。**不改** `aggregate.go:212`、`detail.go:245` 这两个调用方。
5. `rows.go`：`RequestRow` 加 `Req` 字段，`RequestRows` 构造处填值。
6. `go test ./internal/ctxgraph/... ./internal/report/...`：跑通已有测试——预期
   `TestDetailFileName` 等断言字符串里如果硬编码了绝对路径样例，需要跟着改成规范化后的形式
   （落地时以编译器/测试报错为准，不要预先猜哪些会受影响）。
7. 真实语料验证：
   ```bash
   ./vmrbin report -o /tmp/p2verify logs/vmr-audit-2026-07-28.jsonl.zst
   ./vmrbin story  -o /tmp/p2verify logs/vmr-audit-2026-07-28.jsonl.zst
   python3 -c "
   import json
   req = json.load(open('/tmp/p2verify/vmr-requests.json'))
   sto = json.load(open('/tmp/p2verify/stories/vmr-stories.json'))
   a = {r['req'] for r in req['requests'] if 'req' in r}
   b = {f\"{m['path']}:{m['line']}\" for f in sto['files'].values() for m in f.get('manifests', []) if m}
   print('report side:', len(a), 'story side:', len(b), 'joinable:', len(a & b), 'report-only:', len(a - b))
   "
   ```
   期望 `joinable` 覆盖 `report side` 的绝大多数（`report-only` 的残差应该只是 story 端结构上排除
   的单发请求——架构文档 §2.2 的 92.49% 覆盖率，不是 P2 要修的东西）。
   同一批日志分别用绝对路径与相对路径各跑一次 `vmr story`，`diff` 两次的 `vmr-stories.json` 里
   `files` 段的 key 数量应该相同（不再因路径写法产生重复条目）。

### 2.4 验收标准（对照 DevPlan P2.1）

- [ ] 两条命令对同一批日志的机读产物可按坐标互相 join，命中率符合 story 自身的结构性覆盖率
      （不要求 100%——单发请求本来就不在 story 的 Journey 集合里，这不是 P2 的问题）。
- [ ] 同一份日志分别用绝对路径/相对路径扫描，`FileCache` 不再产生重复条目。
- [ ] 坐标唯一性有断言保护（`CheckPathCollisions`），且有测试覆盖真实触发路径。

---

## 3. 任务二 + 三：详单做减法 + 渲染下沉与命名重建（P2.2 + P2.3 合并执行）

### 3.1 现状（已读代码确认）

`internal/report/detail.go`（1047/1150 行，archtest 预算见 `file_sizes_test.go:57`）今天的结构：

| 内容 | 位置 | 性质（对照架构文档 §7.6(a) 分类表） |
| --- | --- | --- |
| `SessionID`/`TaskID`/`TaskSeq`/`SessSeq` | `renderSessionHeader`（约 483-486 行，`t.SessionTaskLine(...)`） | 视图层位置命名，run-scoped，**砍** |
| `Compaction`/`Summarizes`/`ContinuesTo` | `renderSessionHeader` 前段（约 476-481 行） | report §6.7 专属跨记录结论，**砍** |
| `Parent.DetailFile`（上一轮链接） | `renderSessionHeader`（约 487 行，`t.PrevTurnLink(...)`） | 关系，**保留，改由 ctxgraph 同 lineage 前驱给出** |
| `DeltaStart`（🆕 增量高亮） | `renderClientRequest` 里多处 | 关系，**保留**，来自 `ctxgraph.Classify(prev, cur)`，不是 report 私有计算 |
| `RoleTokens` | `renderClientRequest`（约 588-590 行，`info.RoleTokens` 有纯函数回退 `roleTokens(req.Body)`） | per-record 纯计算，**保留，直接算，去掉"有 info 就复用"的分支** |
| `ToolCalls`/`TraceID`/`ChatID`/`ToolsSig`/`Truncated`/`NoReply` | `renderSessionHeader` 中段 | per-record 提取，**保留，直接算**（`NoReply` 需要 `taskseg.Profile`） |
| `Usage`/`UsageOK` | `recordUsage(rec, info)` | per-record，**保留**，已有 `info == nil` 时的纯函数回退路径 |
| `DetailFileName` 的 `used map[string]int` 批次计数器 | `detailFileName`（342-368 行） | 批次状态，**砍**，改用坐标哈希 |
| `fmtutil.DisplayZone` 依赖 | `detailFileName` 第 356 行 | 本机时区依赖，**砍**，改用记录自带时区（`deriveID` 先例） |

**关键澄清（避免过度设计）**：`RoleTokens`/`ToolCalls`/`TraceID`/`ChatID`/`ToolsSig`/`Truncated`/
`NoReply`/`Usage` 这批字段今天都在 `report/session.go` 的 `collect()`/`collectResponse()` 里
**已经实现过一遍**（供 `ReqInfo` 聚合期使用），且 `detail.go` 自己也已经有"`info != nil` 时复用，
否则纯函数回算"的双路径（`recordUsage`、`RoleTokens` 的回退分支）。新叶子包**不需要 import
`report`** 去复用这份逻辑（那会违反 archtest 的单向边界），而是把这批**纯粹只依赖
`rec *audit.Record`（外加 `NoReply` 需要的 `prof`）**、跟 report 的会话分组状态完全无关的
提取函数**搬进新叶子包**，`report/session.go` 的 `collect()` 改成调用叶子包导出的同名函数
（而不是保留一份平行实现）。这是本次改动里唯一"顺手多做一步"的地方，理由是：这批函数今天已经
是纯函数（架构文档已核实），不复用只会让同一段逻辑永久存在两份、以后改一处忘改另一处。**如果
这一步实现起来发现某个字段其实没有看起来那么纯（隐藏了 report 包级状态），就地退回"detail 自己
重新实现一份"，不要为了强行复用而给叶子包引入不该有的依赖**——判断标准就是 archtest 的单向边界
测试，它会在错误发生的第一时间报红。

### 3.2 目标设计

**新叶子包**：建议命名 `internal/reqdetail`（区别于 P3/P6 阶段"`evidence/` 输出目录"这个不同的
概念——那是输出路径的一个子目录名，不是 Go 包名，两者不要混淆）。依赖面
`{audit, core, chatmsg, ctxgraph, taskseg, i18n, fmtutil}`，全部是既有叶子，`archtest` 的
`forbiddenImports`/`zeroInternalDepPackages` 都不需要改一行（新增文件默认受 `file_sizes_test.go`
的全局 700 行预算约束，不需要专门登记，除非落地后超了才登记豁免）。

**导出函数**（签名细节是本任务的核心判断，实现时如与下面的形态有出入，以能让 §3.1 表格里"保留"
的每一项都能算出来为准）：

```go
// internal/reqdetail 包

// Render renders one audit record's detail page. m is rec's own Manifest
// (nil for records that never parsed as a chat object — rejected/malformed
// requests still get a page, just without the session-relationship parts).
// prev is the immediately preceding Manifest in the SAME ctxgraph.Lineage
// (nil for a lineage's first record, or when m is nil) — NOT story's
// stitched-chain predecessor; that distinction is a mid-tier concern (see
// architecture doc §7.6a), the leaf only knows about its own lineage.
func Render(rec *audit.Record, path string, line int, m, prev *ctxgraph.Manifest, prof taskseg.Profile, lang i18n.Lang) string

// FileName is Render's companion: the deterministic filename for this
// exact (path, line) coordinate. Callable from a bare Manifest (or even
// just path+line) without rec's full body — this is what lets a spine Step
// or a requests-index row compute a detail link before the file exists.
func FileName(rec *audit.Record, path string, line int) string
```

`path`/`line` 与 `m`/`prev` 分开传，而不是只吃 `m`：坐标（文件名、`req`）对**所有**记录都必须
可算，包括 `ctxgraph.BuildManifest` 判定 `ok=false` 的那批（请求体没解析成聊天对象的拒绝态
记录）——这批记录今天 `detail.go` 仍然要渲染（`info` 传 `nil` 时的现有回退路径），减法之后
坐标不能跟着"没有 Manifest 就没有文件名"一起丢掉。

**文件名**：`{ts}_{virtual}_{real}_{outcome}_{hash8}.md`——`ts` 用 `rec.TS.Format(...)`（记录
自带时区，不经 `fmtutil.DisplayZone`，抄 `journey.go` 的 `idTimeLayout`/`deriveID` 写法）；
`hash8` = `ReqCoord(path, line)` 的 md5 前 4 字节转 8 位十六进制（抄 `toolsSig` 的口径）。
**去掉 `used map[string]int` 批次计数器与 `-N` 后缀**——坐标天然唯一，不再需要碰撞消歧。

**渲染内容的具体减法**：
- `renderSessionHeader` 里 `SessionID`/`TaskID`/`TaskSeq`/`SessSeq`/`Compaction`/`Summarizes`/
  `ContinuesTo` 这一整段判断连同 `i18n.DetailText` 里对应的 `SessionTaskLine`/`CompactionCallLabel`/
  `CompactionSummarizes`/`CompactionContinues` 四个字段一并删除（EN/ZH 都删，`report_detail.go`）。
- `PrevTurnLink` 保留，但入参从 `info.Parent.DetailFile` 改为**当场用 `prev` 算**：
  `if prev != nil { link := FileName(prevRec场景不可得...) }`——**这里有个真实的接口缺口需要在
  实现时解决**：`PrevTurnLink` today 需要"上一轮的 detail 文件名"，而叶子函数算文件名需要上一轮的
  `(rec, path, line)`，但 `Render` 只拿到上一轮的 **Manifest**（`prev`），没有上一轮的完整
  `*audit.Record`。两个可行方向，选哪个留给实现时定：(a) 给 `FileName` 加一个只吃 `Manifest`
  的重载/变体——文件名里 `virtual`/`real`/`outcome` 这三段其实 `Manifest` 自己也有对应字段
  （`Model`/`Endpoint`/`Outcome`），不一定需要完整 `audit.Record`；(b) 调用方（`DetailWriter`/
  `story` 未来的按需生成）在拿到 `prev` 的同时也想办法拿到上一轮的 `*audit.Record`（report 侧
  今天 `ReqInfo.Parent` 已经间接关联着上一条记录，可行）。**判断标准**：无论选哪个，"上一轮链接"
  必须在没有任何 detail 文件已经生成的情况下也能算出正确文件名（§7.3(c) 的"渲染即生成"前提），
  不能依赖读盘探测。
- `DeltaStart` 改为当场算：`prev != nil` 时 `e := ctxgraph.Classify(prev, m); deltaStart :=
  m.LeadSys + e.LCP`（直接照抄 `session.go:650-651`），`prev == nil` 时 `deltaStart = 0`。
- `RoleTokens`/`ToolCalls`/`TraceID`/`ChatID`/`ToolsSig`/`Truncated`/`NoReply`/`Usage`：按
  §3.1 表格"保留，直接算"——把这批提取逻辑从 `session.go` 的 `collect`/`collectResponse` 搬到
  `internal/reqdetail`（保持纯函数签名 `func(rec *audit.Record[, prof taskseg.Profile]) T`），
  `session.go` 改成调用 `reqdetail.XxxFrom(rec, prof)`。**逐字段确认这批函数确实不读
  `ReqInfo`/`SessionInfo` 的任何字段**（架构文档已经做过这个确认，落地时用编译器强制——如果
  硬要传 `ReqInfo` 进去会在 import 边界测试上现形）。

### 3.3 具体步骤

1. 新建 `internal/reqdetail/` 包骨架：把 `detail.go` 里与 §3.1"保留"标注的函数原样搬过去
   （`renderClientRequest`/`renderAttempts`/`renderMessageSection`/`headerTable`/`codeFence`/
   `jsonIndent`/`details`/`escapeHTML`/`fmtBytes`/`ms`/`outcomeMark`/`attemptUpstream`/
   `errorClass`/`recordUsage`/`roleTokens`/`roleStatLine`/`callsCell` 等——这些今天已经是
   `detail.go` 里独立的纯函数，直接剪切，不用重写）；`renderDetail`/`renderSessionHeader`/
   `detailFileName` 按 §3.2 的签名重写；`DetailWriter`/`NewDetailWriter`/`writeOneDetail`/
   `WriteDetails` 也搬过去（它们是"渲染 + 写盘"的调度层，不是 report 特有逻辑）。
2. `internal/i18n/`：新建 `i18n/reqdetail_detail.go`（沿用"一节一文件，紧挨对应渲染代码"的既有
   规则），把 `report_detail.go` 的 `DetailText` 结构体连同 EN/ZH 两份文案搬过去，删除
   `SessionTaskLine`/`CompactionCallLabel`/`CompactionSummarizes`/`CompactionContinues` 四个字段。
   旧的 `internal/i18n/report_detail.go` 删除。
3. `internal/report/detail.go`：删掉已经搬走的函数，只留 `report` 包自己仍需要的薄封装（如果有——
   多半是空文件，判断是否整个删除 `detail.go`，`WriteDetails`/`NewDetailWriter` 等符号改为从
   `reqdetail` 重导出或调用方直接换 import）。`DetailWriter.Submit(rec, info)` 内部改为：从
   `info`（`info == nil` 时用 `path`/`line` 走无 session 分支）取出 `info.manifest`/
   `info.Parent` 对应的 `manifest`，调用 `reqdetail.Render(rec, info.Path, info.Line, m, prev,
   prof, lang)`。
4. `internal/report/session.go`：`collect`/`collectResponse` 改为调用 `reqdetail` 导出的提取
   函数（见 §3.2 最后一条）。
5. `cmd/vmr/cmd_report.go`：`onRecord`/`NewDetailWriter` 等调用点跟着新包名调整 import。
6. `go test ./internal/reqdetail/... ./internal/report/... ./internal/archtest/...`：
   - `detail_test.go` 里原有的 `TestDetailFileName`/`TestRenderDetail_*`/`TestWriteDetailsEndToEnd`
     等测试整体搬到 `internal/reqdetail`（跟着实现走），断言内容按新签名/新文件名格式改写
     （不再有 `-N` 后缀、时间戳不再受 `TZ` 环境变量影响、多一段 `hash8`）。
   - `TestBuildOnRecordMatchesWriteDetails`（`detail_test.go:672`）是本任务**最重要的回归测试**——
     它今天已经在断言"两条路径生成的 detail 一致"，改造后应该继续断言这件事，且断言范围要扩大到
     "文件名也逐字节相同"（今天大概率只断言内容，不断言文件名，因为旧命名依赖批次顺序，两条路径
     本来就可能给出不同的 `-N` 后缀——新命名去掉这个变量后，这条测试应该能收紧成完全相等）。
7. **跨路径一致性验收**（本任务的核心验收，新增一个测试，不是复用已有的）：构造同一条记录，一次
   走"全量多文件扫描"路径（模拟 `vmr report` 对整个语料跑），一次走"单文件/子集扫描"路径（模拟
   `vmr story -journey` 未来会做的事——即使 P2 不接这个 CLI 能力，也要用测试模拟这个调用形状），
   断言 `reqdetail.FileName` 与 `reqdetail.Render` 的返回值逐字节相同。这是 P2.3 验收标准的
   直接体现，必须是一个真正跑两条不同代码路径的测试，不能只是断言"这两次调用参数相同所以结果
   相同"这种同义反复。
8. **时区无关性验收**：新增一个测试，用 `t.Setenv("TZ", "...")` 切换两次不同时区（或直接构造带
   不同 `time.Location` 的 `rec.TS`），断言 `FileName` 输出不变——覆盖"记录自带时区，不受运行
   机器时区影响"这条硬约束。
9. 真实语料验证：
   ```bash
   ./vmrbin report -o /tmp/p2verify logs/vmr-audit-2026-07-28.jsonl.zst
   ls /tmp/p2verify/details/ | head -5          # 目测文件名格式：无 -N 后缀，多一段 hash8
   TZ=America/New_York ./vmrbin report -o /tmp/p2verify-tz logs/vmr-audit-2026-07-28.jsonl.zst
   diff <(ls /tmp/p2verify/details/) <(ls /tmp/p2verify-tz/details/)   # 期望完全一致
   ```

### 3.4 验收标准（对照 DevPlan P2.2 + P2.3）

- [ ] detail 内容不再随输入文件集合大小变化（同一条记录，全量扫描 vs 子集扫描，正文逐字节相同）。
- [ ] 同一条记录经两条不同路径生成，文件名与正文逐字节一致（§3.3 第 7 步的测试）。
- [ ] 更换机器时区重跑，文件名不变（§3.3 第 8 步的测试 + 第 9 步的真实语料复现）。
- [ ] `go test ./internal/archtest/...` 全绿——`internal/reqdetail` 不反向依赖 `report`/`story`，
      两者仍互不 import。

---

## 4. 需要在实现时确认、不预先假设的几个点

（按项目风格，这些留给实现阶段用编译器/测试结果确认，不在这里预判答案）

1. `internal/reqdetail` 是否需要专门登记进 `internal/archtest/file_sizes_test.go` 的豁免表——
   按"全局默认 700 行 + 报警了再拆"的既有原则（KNOWN_ISSUES §1.5 的处置逻辑），先不预先登记，
   落地后跑 `go test ./internal/archtest/...` 见分晓。
2. `PrevTurnLink` 的接口缺口（§3.2 "上一轮链接"那段）具体走哪个方向，取决于 `DetailWriter`
   在 report 侧能多方便地拿到上一条记录的 `*audit.Record`——这个判断只有写代码时才看得清楚。
3. `RoleTokens`/`ToolCalls` 等提取函数搬进 `internal/reqdetail` 后，`report/session.go` 的
   `collect` 是否还需要保留自己的字段（`ReqInfo.RoleTokens` 等），还是可以让 `ReqInfo` 直接嵌入
   `reqdetail` 返回的结构体——如果嵌入更省代码就嵌入，如果两边字段语义有一丁点差异
   （比如聚合期还需要某个 detail 不需要的额外统计）就保持两份字段、只共享提取函数本身。

---

## 5. 收尾（P2.1–P2.3 共用）

1. **全量测试与架构边界**：
   ```bash
   go test ./... -race
   go test ./internal/archtest/...
   gofmt -l .
   ```
2. **CHANGELOG.md**：`[Unreleased]` 下按 Added/Changed/Fixed 分类加条目（具体措辞落地时按实际
   改动定），例如：
   - Added: 审计记录获得跨命令通用的请求级坐标（`basename:line`），`vmr-requests.json` 发布为
     `req` 字段。
   - Changed: 详单渲染下沉为 `report`/`story` 共用的 `internal/reqdetail` 叶子包；文件名不再
     依赖运行批次与本机时区，改由坐标哈希决定。
   - Fixed: 同一份审计日志用绝对路径与相对路径分别扫描不再在解析缓存里产生重复条目。
3. **KNOWN_ISSUES_sonnet-5.md**：
   - `§1.20`（fact-layer 与 detail 的重复）目前登记"这是 P2 阶段的工作"——P2 完成后，如果
     `§1.20` 描述的目标（脊柱链接到 detail）尚未达成（它确实没有——那是 P5.2 的工作，P2 只是
     把"detail 能被两侧确定性寻址"这个前提做好），把 `§1.20` 的"不在本次范围内"一段改写为
     "P2 已完成坐标与命名前提，脊柱挂链接本身排在 P5.2"，而不是直接销号。
   - 新增一条低优先级观察项（若实现过程中确实发现）：`PrevTurnLink` 的接口缺口如果按 §4.2 的
     方向 (a) 解决（只吃 Manifest 算文件名），记录这个决定的理由，供 P3/P5 的 ActionPlan
     直接复用而不用重新分析。
4. **架构文档同步**：`story_report_architecture_opus-5.md` §2.2 的实测基线表（detail 文件名批次
   碰撞率、时区依赖等）在 P2 完成后已经是历史数字——按 P1 收尾时的同一做法，补一条简短说明
   指向本文档，不重写整份架构文档。
5. **边界复核**（DevPlan §2.2 第 6 条，三个问题）：
   - 本阶段是否产生了架构文档未预见的事实？—— 重点检查 §3.2 "PrevTurnLink 接口缺口"这一点
     最终怎么解决的，如果解决方案比架构文档 §7.6(a) 描述的更复杂，回写进架构文档。
   - 本阶段是否改变了 P3 及以后的前提？—— 预期不改变（`req` 坐标格式、detail 文件名规则、
     `internal/reqdetail` 的包边界都是 P3 "按坐标取记录原语"/"按需生成"直接复用的地基）；如果
     `internal/reqdetail` 的导出函数签名与本文 §3.2 的预判有出入，在这里记录实际签名，供 P3
     的 ActionPlan 直接引用。
   - 本阶段是否暴露出某个原计划任务其实不必要？—— 留空，按实际执行情况填。

---

## 6. 验收清单（对照 DevPlan P2 的验收标准逐项勾，完成后填）

- [ ] 两条命令对同一批日志的机读产物可按坐标互相 join，命中率完全（在 story 结构性覆盖率之内）。
- [ ] 同一份日志不再因路径写法不同产生重复缓存条目。
- [ ] 坐标唯一性有断言保护。
- [ ] 详单内容不再随输入文件集合的大小而变化。
- [ ] 同一条记录经两条不同路径生成，文件名与正文逐字节一致。
- [ ] 更换机器时区重跑，文件名不变。
- [ ] `internal/report`/`internal/story` 都只 import 新叶子包 `internal/reqdetail`，
      `archtest` 全绿。
- [ ] `go test ./...`、`go test ./internal/archtest/...` 全绿。
- [ ] CHANGELOG、KNOWN_ISSUES（§1.20 更新）、架构文档说明性备注均已同步。
