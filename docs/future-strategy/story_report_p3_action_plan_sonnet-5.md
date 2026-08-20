// Ver 2026-08-20 00:00, by Sonnet 5

# vmr 日志分析体系重构 — P3 ActionPlan（证据层瘦身与共享条目）

## 0. 定位

本文是 `docs/future-strategy/story_report_dev_plan_opus-5.md` 里 **P3 阶段**的执行级细化，基于
本仓库 P3 起点（P2 已完成并提交，commit `7fa864f`）的真实代码状态编写。架构依据见
`docs/future-strategy/story_report_architecture_opus-5.md` 的 §7.6(b)(c)（共享证据层/体积纪律）、
§7.8（索引与缓存分家）、§7.9（命令层，本阶段只取其中"一个读取原语"与"统一记录选择器"两点，CLI
动词收敛本身留给 P6.5）、§7.10（重复解析与缓存载荷）、§7.11 迁移路径批 5。

**DevPlan review 结论**（本次 review 已完成，见 DevPlan 正文的编辑记录）：P3 的任务范围、边界、
验收标准在当前代码库上逐条核实后**均成立，不需要改动**——`report/detail.go` 的 `writeOneDetail`
仍在为每条记录写一份 `.json` 同构副本、`vmr replay` 仍是 `-line`/`-ts`/`-detail` 三种互斥写法且
均不认识 `req` 坐标、`-details` 仍默认 `true` 且每次全量物化、`renderClientRequest` 仍把 system
prompt 和工具声明整段内联进每一份详单、`ctxgraph.FileCache` 仍直接嵌在 `vmr-requests.json`/
`vmr-stories.json` 的 `"files"` 字段里且用 `MarshalIndent`、`CachedFile{Hash, Manifests, NoBody}`
仍不含 schema 版本戳、`report.buildInternal` 的第二遍扫描（`ingest.go` 的按端点/按记录聚合）仍
无条件重新打开并解码每个输入文件而不查缓存。P2 执行期间的两处真实设计调整（详单文件名坐标哈希无
错误子类后缀；坐标改存 `ctxgraph.Manifest.Req` 字段而非重算）已经是 P3 的既定输入，见 §0.1。

### 0.1 P3 必须直接复用的 P2 最终形态（不是本文的判断，是既成事实）

- `ctxgraph.Manifest.Path` **保持调用方传入的原始形态**（真实可 `os.Open`），坐标改由**独立存储**
  的 `Manifest.Req string`（`json:"req,omitempty"`）字段承担，构造时经 `ReqCoord(path, line)` 算
  一次存住。任何要用坐标的新代码读 `.Req`，不要对 `.Path` 做任何规范化假设。
- `ctxgraph.ReqCoord(path, line) string` = `CanonicalPath(path) + ":" + line`；
  `ctxgraph.CanonicalPath(path) string` = 去掉 `.zst` 后缀的 basename；
  `ctxgraph.ReqHash8(req string) string` = `req` 的 md5 前 4 字节转 8 位十六进制。三者都在
  `internal/ctxgraph/reqcoord.go`，本阶段新增的任何坐标解析/短哈希需求直接复用，不要重新发明。
- `internal/reqdetail.FileName(ts, virtualModel, realModel, outcome, req string) string` 是详单
  命名的唯一权威实现；`FileNameForRecord(rec, path, line)`（吃完整 `*audit.Record`）与
  `FileNameForManifest(m *ctxgraph.Manifest)`（只吃 `*ctxgraph.Manifest`）是它的两个薄封装，**永远
  算出同一个值**——`outcome` 段不带错误子类后缀，唯一性完全由 `hash8` 承担。
- `internal/reqdetail.Render(rec, path, line, m, prev *ctxgraph.Manifest, prof, lang) string` 是
  详单正文渲染的唯一权威实现，`report`/`story` 都应经它生成详单，不得各自维护一份。
- `internal/report`、`internal/story` 都只 import `internal/reqdetail`，互不 import；
  `internal/reqdetail` 依赖面 `{audit, core, chatmsg, ctxgraph, taskseg, i18n, fmtutil}`，
  本阶段新增代码不得反向依赖 `report`/`story`（archtest 会现场拦下）。

**P3 范围边界**（与 DevPlan 一致）：逐请求 JSON 副本的去留、详单的生成时机、按坐标直取原始记录
的能力、重复大块内容的共享化、解析缓存的存储形态与载荷范围。**不改变**详单正文本身的渲染逻辑
（那是已经定型的 P2 产物，本阶段只改"什么时候生成""生成到哪""内容是否内联"，不改"生成出来长
什么样"，除了 §3 把 system prompt/工具声明从内联改为链接这一处，且这处改动的落点是"少渲染什么"，
不是"渲染逻辑本身变了"）。**不**把 `internal/story` 的决策脊柱接成详单/证据链接的消费方——那是
DevPlan 明确写的 P5.2（"脊柱每步挂详单链接"），依赖 P4 补齐机读层结构，是一条 DevPlan 标注为
"绕过就会造成真实信息损失"的硬依赖，本阶段不得抢先实现，哪怕实现起来顺手。本阶段做的是**把这些
链接需要的地址和幂等生成能力准备好**，供 P4/P5/P6 直接使用。

**通用收尾要求**（DevPlan §2.2，每个任务完成后都要过一遍，本文 §7 统一收口）：全量测试 + archtest
通过、真实日志肉眼核对、golden/固定断言更新且可解释、CHANGELOG 条目、KNOWN_ISSUES 登记、边界复核。

---

## 1. 执行前置检查

```bash
git status --short                     # 确认工作区干净，P2 那批改动已在 HEAD（7fa864f）
go build -o /private/tmp/claude-501/-Volumes-SSD2T-code-vmr/*/scratchpad/vmrbin ./cmd/vmr
go test ./... 2>&1 | tail -30           # 建立改动前的基线：全绿
ls logs/vmr-audit-2026-07-28.jsonl.zst  # P1/P2 用过的同一批样本，本机已存在
```

---

## 2. 任务分批与执行顺序

DevPlan 把 P3 列成 7 个子任务（P3.1–P3.7），但它们之间有真实的交付耦合，不能按编号顺序逐条独立
提交——架构文档 §7.6(c) 第 1 条明确写"删副本、补读取原语必须同批交付"，§7.8 的缓存重构（拆分/
扩载荷/补版本戳）也是同一份存储格式改动的三个面，拆开提交会在中间留一个格式减半的过渡态。
按真实依赖分成四批，**批内合并实现，批间按顺序推进，不并行**（DevPlan §2.1"不设并行分支"同样
适用于阶段内部）：

| 批次 | 对应 DevPlan 任务 | 为什么合并 |
| --- | --- | --- |
| **批 A** | P3.1 + P3.2 | 删除 `.json` 副本与"坐标取记录原语"是同一个能力的两面：没有原语接管，删副本就是纯粹的信息损失 |
| **批 B** | P3.3 | 独立：批 A 完成后，"按需生成"才有意义（否则默认路径删了副本、又不按需生成，链接会指向真空） |
| **批 C** | P3.4 | 独立于批 B，但复用批 A 的坐标原语做"内容缺失时按需取回"，排在批 A 之后 |
| **批 D** | P3.5 + P3.6 + P3.7 | 三者是同一份缓存存储格式的三个维度（形态拆分、载荷范围、版本戳），合并成一次迁移 |

四批之间也有顺序：批 A 必须最先做（它改的坐标读取原语，批 C 的"按需取回"要用；批 D 的缓存重构
如果先做，批 A 删除 `.json` 副本时的测试基线会被批 D 的缓存格式变化污染，先后关系颠倒会让批 D
的 diff 掺进批 A 的内容，不利于逐项核对）。批 B、批 C 可以互换顺序（互不依赖），批 D 放最后——
它不影响任何产物的对外可见形态，只影响性能与文件大小，放最后不阻塞前三批的可验收性。

每批做完立即跑 `go test ./internal/reqdetail/... ./internal/report/... ./internal/story/... ./internal/ctxgraph/... ./internal/replay/... ./internal/archtest/...`，不要攒到最后一起查错。

---

## 3. 批 A：删除逐请求 JSON 副本 + 坐标取记录原语（P3.1 + P3.2）

### 3.1 现状（已读代码确认）

- `internal/report/detail.go` 的 `writeOneDetail`（约第 82-96 行）在写 `.md` 之后，紧接着
  `json.MarshalIndent(j.rec, "", "  ")` 写一份同名 `.json`——这就是架构文档 §7.6(c) 实测"`.json`
  40MB + `.md` 20MB / 59 条记录"里那份 `.json`。
- `internal/replay/replay.go` 的 `Options` 有三个互斥定位字段：`Line`（配合位置参数 `AuditPath`
  按行号定位）、`TS`（配合 `AuditPath` 按时间戳定位）、`DetailPath`（**不需要** `AuditPath`，
  直接读一份自包含的 `details/*.json`）。`selectRecord`（约第 268 行）做互斥校验并分发；
  `loadDetailFile`（约第 312 行）是 `DetailPath` 的实现，`loadRecordByLine`（约第 382 行）是
  `Line` 的实现——后者已经具备"打开审计文件、按行定位、反序列化"的完整逻辑，只是嵌在 `replay`
  包内部、返回类型是为重放定制的 `recordView`（部分字段），不是可复用的原始字节。
- `cmd/vmr/cmd_replay.go` 把这三个定位方式暴露成 `-line`/`-ts`/`-detail` 三个 flag，都不认识
  `req` 坐标格式（`basename:line`）。
- `docs/UserGuide.md`/`docs/UserGuide.zh.md` 的 replay 一节（约第 549/570 行 EN、547/570 行 ZH）
  文档并示例了 `-detail out/details/....json` 的用法——`.json` 副本一旦删除，这段文档和示例即失效。
- **关键前提**：`req` 坐标本身只有 `basename:line`，**不含目录**——它不是一个可以脱离审计日志
  文件独立解析的自包含地址；解析坐标必须同时有"这条记录所在的那份审计文件（或至少同目录）"这个
  外部输入。这与 `vmr replay` 今天的 CLI 形状（`-line`/`-ts` 都搭配一个位置参数 `<audit.jsonl>`）
  完全吻合，`-req` 应该采用同样的搭配方式，而不是试图脱离位置参数独立解析。

### 3.2 目标设计

**(a) `internal/audit` 新增按行取原始字节的原语**（不是 `ctxgraph`，也不是 `reqdetail`——这是纯
审计文件 I/O，`audit` 包本来就是这一层，`ctxgraph`/`reqdetail` 不需要为了一个 I/O 原语反向获得
新依赖）：

```go
// internal/audit/read.go 或新文件 internal/audit/fetch.go

// LineAt returns the raw bytes of path's 1-based logical line — the same
// counting audit.ForEachLine already uses (skipped/oversized lines still
// advance the counter). It does not unmarshal: callers decode into
// whatever shape they need (a full audit.Record for a "read" consumer, a
// partial recordView for replay). Returns an error if line is out of range
// or <= 0.
func LineAt(path string, line int) ([]byte, error)
```

实现直接复用 `ForEachLine` 的计数约定（`replay.go` 的 `loadRecordByLine` 已经是现成范例，落地时
把它的计数循环抽出来即可，不需要重新设计）。

**(b) `ctxgraph` 新增坐标反解析**（`reqcoord.go` 已有 `ReqCoord`/`CanonicalPath`/`ReqHash8`，加一个
配套的反向函数，保持"坐标的构造与解析都在一处"）：

```go
// ParseReqCoord splits a "basename:line" coordinate back into its two
// parts. It does not resolve basename to a real filesystem path — the
// caller must supply that separately (see reqcoord.go's package doc: Req
// is an identity string, never an I/O path).
func ParseReqCoord(req string) (basename string, line int, err error)
```

**(c) `vmr replay` 加 `-req` flag，与 `-line`/`-ts` 同级互斥，复用同一个位置参数**：

```
-req COORD   replay the record at this coordinate ("basename:line", as published in
             vmr-requests.json's "req" field or vmr-stories.json's manifest entries);
             mutually exclusive with -line/-ts/-detail; requires the same positional
             audit file argument -line/-ts use
```

`selectRecord` 里新增一支：解析 `opts.Req` 得到 `(basename, line)`，用
`ctxgraph.CanonicalPath(opts.AuditPath) == basename` 校验位置参数与坐标是否一致（不一致直接报错，
不静默忽略 basename 部分去猜用户的意图），一致则等价于 `Line: line`——**内部直接落到
`loadRecordByLine` 这条既有路径**，不新写一套记录加载逻辑。

**(d) `-detail` flag 整体移除**（不是弃用/兼容，删除干净——项目"无历史包袱"的既定纪律，见 P2 对
详单链接失效的同一处置）：`Options.DetailPath`、`selectRecord` 里对应分支、`loadDetailFile` 函数
一并删除。

**(e) 新增一个不需要 `-provider` 的"打印原始记录"能力，接管 `.json` 副本原来的"直接 cat 一条记录"
用途**——这是架构文档 §7.6(c) 第 1 条"读取原语"里，重放本身回答不了的那一半（重放是"重新发起这
次请求"，不是"看这条记录长什么样"）。落点：`vmr replay` 加一个 `-print` flag，设置时跳过
`-provider` 必填校验，把 `selectRecord` 解析到的记录**原始字节**（用 §3.2(a) 的 `audit.LineAt`
重新取一次，而不是 `recordView`——`recordView` 为了重放特意丢了字段，"看记录长什么样"需要完整
原始 JSON）写到 stdout 后直接返回，不进入任何构造/发送逻辑。**放在 `replay` 而不是新开一个子
命令**：这是本阶段的最小化选择，不是最终形态——架构文档 §7.9 把"一个读取原语"和"CLI 收敛成一个
分析动词"绑在一起讨论，但那是 P6.5 的范围（DevPlan 明确把命令行收敛排在 P6）；P3 只需要让"删除
副本"这个动作不造成能力损失，不需要在这里预判 P6 最终会把它安在哪个动词下。把这个决定记录进
§8 的 KNOWN_ISSUES/边界复核，供 P6 直接读，不必重新分析"P3 当时为什么这样做"。

### 3.3 具体步骤

1. `internal/audit/`：新增 `LineAt(path string, line int) ([]byte, error)`（新文件或加进
   `read.go`，落地时看行数预算），配单测（含"line 超出范围报错"“line ≤ 0 报错"两个用例）。
2. `internal/ctxgraph/reqcoord.go`：新增 `ParseReqCoord`，配单测（含"格式错误"“line 非数字"
   两个用例），紧挨 `ReqCoord` 放，保持"构造/解析同一处"的可读性。
3. `internal/replay/replay.go`：
   - `Options` 加 `Req string`；`selectRecord` 的互斥计数扩到四个（`DetailPath`/`TS`/`Line`/
     `Req`）算 1，**同时删除 `DetailPath` 分支与 `loadDetailFile`**（不是先加后删，一次改完，
     避免中间态）。
   - 新增 `Options.Print bool`；`Run` 顶部：`if opts.Print { ... }` 分支——解析记录坐标定位到
     `(path, line)` 后（可以复用 `selectRecord` 已经做的定位，只是不消费其 `recordView`，改用
     `audit.LineAt(path, line)` 取原始字节直接写 stdout），**在 `Provider == ""` 校验之前返回**，
     因为 `-print` 就是为了不需要 `-provider` 才存在。**`-print` 与 `-detail` 已删除、`-print`
     与"发送请求"的其余 flag（`-dry-run`/`-record`/`-model` 等）互斥性**——落地时确认：`-print`
     应该独占，若同时传了 `-dry-run` 或 `-provider` 之类，报错还是静默忽略，选哪个更符合项目
     "宁可拒绝也不猜"的风格（参考 `-detail`+`-ts` 互斥校验的既有写法），不要预先假设。
4. `cmd/vmr/cmd_replay.go`：加 `-req`/`-print` 两个 flag，删除 `-detail` flag 及其 usage 文案；
   `-print` 模式下位置参数校验、`-provider` 必填校验都要跳过（参考现有 `*detail != ""` 分支的
   校验结构改写，而不是另起一套）。
5. `internal/report/detail.go`：`writeOneDetail` 删除 "Same-named .json alongside the .md" 那一段
   （§3.1 提到的约 82-96 行区间，落地时以实际行号为准）。
6. `internal/report/detail_test.go`（及已搬到 `internal/reqdetail` 的对应测试）：所有断言
   "详单目录同时有 `.md` 和 `.json`" 的用例改为断言"只有 `.md`"；`TestBuildOnRecordMatchesWriteDetails`
   等跨路径一致性测试若依赖 `.json` 副本做内容比对（而不是重新渲染比对），改为直接调用
   `reqdetail.Render` 两次比对，不再借道 `.json`。
7. `internal/replay/replay_test.go`、`cmd/vmr/cmd_replay_test.go`：删除所有 `-detail`/`DetailPath`
   相关用例；新增 `-req` 的正向用例（坐标与位置参数一致）、反向用例（basename 不匹配报错、格式
   错误报错）；新增 `-print` 用例（输出即该行原始字节，不需要 `-provider`）。
8. `docs/UserGuide.md` 与 `docs/UserGuide.zh.md`：replay 一节的参数说明与示例同步改写——删掉
   `-detail` 的说明与示例命令，加 `-req`/`-print` 的说明与一条示例（两份文档同一改动，保持
   结构/示例值一致，只译文不同，遵守 CLAUDE.md 的 `.zh` 兄弟同步纪律）。
9. 真实语料验证：
   ```bash
   ./vmrbin report -o /tmp/p3verify -details logs/vmr-audit-2026-07-28.jsonl.zst   # 显式开启，见批B
   ls /tmp/p3verify/details/ | grep '\.json$'    # 期望：空，不再有 .json 副本
   python3 -c "
   import json
   req = json.load(open('/tmp/p3verify/vmr-requests.json'))
   print(req['requests'][0]['req'])
   "   # 拿一个真实 req 坐标
   ./vmrbin replay -c config.yaml -provider <任意已配置的 provider> -print \
       -req '<上面拿到的 req>' logs/vmr-audit-2026-07-28.jsonl.zst
   # 期望：stdout 是该条记录的完整原始 JSON（对比同一行原始日志内容）
   ./vmrbin replay -c config.yaml -provider <provider> -dry-run \
       -req '<req>' logs/vmr-audit-2026-07-28.jsonl.zst
   ./vmrbin replay -c config.yaml -provider <provider> -dry-run \
       -line <req 里的行号> logs/vmr-audit-2026-07-28.jsonl.zst
   diff <两次输出>   # 期望完全一致——req 与 line 是同一条记录的两种写法
   ```

### 3.4 验收标准（对照 DevPlan P3.1 + P3.2）

- [x] `details/` 目录不再产生 `.json` 副本——真实语料（322 条记录）验证 0 个 `.json` 文件。
- [x] 干净环境下 `vmr replay -req <坐标> -print` 与 `-provider ... -dry-run` 都能定位到正确记录
      并工作（`-print` 甚至不需要有效的 `config.yaml`，验证过）。
- [x] `vmr replay -req` 与等价的 `-line` 定位到同一条记录，打印结果逐字节一致（真实语料 diff 验证）。
- [x] `-detail` 已从 CLI、代码、文档（UserGuide 双语、Core 设计文档、main.go usage）三处一致移除。

---

## 4. 批 B：详单改为按需且幂等（P3.3）

### 4.1 现状（已读代码确认）

- `cmd/vmr/cmd_report.go`：`detailsFlag := fs.Bool("details", true, ...)`——默认全量物化。
- `internal/report/session.go` 的 `assignNames`（约第 511 行）在会话分析阶段**无条件**给每条
  `ReqInfo` 算 `DetailFile`（`reqdetail.FileName(...)`），**不依赖 `-details` 是否开启**——这意味着
  `RequestRow.DetailFile`/`Req` 今天已经总是可算，`vmr-requests.md`/`.json` 的链接文本从不因
  `-details=false`而缺失，只是链接目标文件可能不存在。**这一半的 DevPlan 验收标准
  （"索引...照常渲染链接（名称可算，不依赖文件是否已存在）"）已经在 P2 里顺带达成，批 B 不需要
  为此另写代码**，只需要在真实语料验证里确认一次即可。
- `internal/report/detail.go` 的 `writeOneDetail` 每次调用都无条件 `os.WriteFile`——重复运行会
  重复渲染、重复写盘，没有"文件已存在就跳过"的判断。
- 没有临时文件+改名的写入方式——直接 `os.WriteFile` 到最终路径，进程中途被杀会留下一个不完整
  的 `.md`，且因为文件已"存在"，本批要加的幂等跳过逻辑会把这个半截文件当成"已完成"。

### 4.2 目标设计

**(a) `internal/reqdetail` 新增"确保已渲染"的共享原语**（`report`、以及未来 P5.2 的 story 消费方
都调它，行为在两侧完全一致，不各自实现一份"要不要跳过"的判断）：

```go
// EnsureRendered writes rec's detail page under dir if it doesn't already
// exist there, returning the (always-computable) filename either way. A
// pre-existing file with this exact name is guaranteed byte-identical to
// what Render would produce now — the name is a pure function of
// (rec.TS, rec.Model, RealModel(rec), rec.Outcome, req) and Render is a
// pure function of (rec, m, prev, prof, lang) — so an existence check is a
// correct and sufficient skip condition, not an approximation. The write
// itself goes through a temp-file-then-rename so a killed process never
// leaves a half-written file that a later run's existence check would
// wrongly treat as done.
func EnsureRendered(dir string, rec *audit.Record, path string, line int, m, prev *ctxgraph.Manifest, prof taskseg.Profile, lang i18n.Lang) (filename string, err error)
```

**(b) `-details` 默认值改为 `false`**：`cmd_report.go` 的 `detailsFlag := fs.Bool("details", false,
...)`。**`report.yaml` 里对应的 `details` 配置项默认值同步改**（落地时确认 `resolveBool` 三元
优先级——flag 显式传入 > `report.yaml` > 硬编码默认——硬编码默认从 `true` 改 `false` 后，没有
显式配置的用户默认行为随之改变，这是一次预期内的破坏性默认值变更，写进 CHANGELOG 的 `Changed`）。

**(c) `DetailWriter`/`writeOneDetail` 改为调用 `reqdetail.EnsureRendered`**，不再自己做
`Render`→`WriteFile` 两步；`-details=true` 时行为不变（还是全量渲染，只是现在会跳过已存在的
文件——多次 `vmr report -details` 重跑不再重复写盘，这本身就是 DevPlan 验收标准的一部分）。

### 4.3 具体步骤

1. `internal/reqdetail/`：新增 `EnsureRendered`（新文件 `ensure.go` 或加进 `detail.go`，看行数
   预算），内部用 `os.Stat` 判存在，不存在时 `Render` 后写临时文件（`os.CreateTemp(dir, ...)`）
   再 `os.Rename` 到最终名——参考项目里额度状态持久化的临时文件+改名写法（`internal/quota` 下
   有现成先例，落地时抄它的错误处理方式，不要重新发明）。配单测：并发调用两次同一坐标（模拟
   `report`/`story` 未来各自独立调用），断言只产生一次实际渲染（可以用一个渲染次数计数器包一层，
   或直接断言文件 mtime 在第二次调用后不变）；断言预置一个"内容不同但文件名相同"的假文件时
   `EnsureRendered` **不会**覆盖它——这是幂等契约的直接体现，不是可选行为。
2. `internal/report/detail.go`：`writeOneDetail` 改为调用 `reqdetail.EnsureRendered(dir, j.rec,
   j.path, j.line, m, prev, prof, lang)`，删除自己的 `Render`+`WriteFile` 两行。
3. `cmd/vmr/cmd_report.go`：`detailsFlag` 默认值改 `false`；相邻的 usage 文案同步改
   （"default: report.yaml's details, or **false**"）。
4. `internal/config`（或 `report.yaml` 对应的默认值来源，落地时以 `resolveBool` 实际读的字段为准）：
   同步硬编码默认值。
5. `go test ./internal/reqdetail/... ./internal/report/...`：`TestBuildOnRecordMatchesWriteDetails`
   等既有测试若显式传 `details=true`（多数应该是）不受影响；新增一个"默认 `-details`（未传 flag）
   跑一次 `vmr report`，`details/` 目录不存在或为空，但 `vmr-requests.json` 的每一行仍有非空
   `req`/`detail_file`"的端到端用例。
6. 真实语料验证：
   ```bash
   ./vmrbin report -o /tmp/p3verify-default logs/vmr-audit-2026-07-28.jsonl.zst   # 不传 -details
   ls /tmp/p3verify-default/details/ 2>&1   # 期望：目录不存在，或存在但为空
   python3 -c "
   import json
   req = json.load(open('/tmp/p3verify-default/vmr-requests.json'))
   print(all(r.get('detail_file') for r in req['requests']))   # 期望 True
   "
   ./vmrbin report -o /tmp/p3verify-default -details logs/vmr-audit-2026-07-28.jsonl.zst  # 补跑
   ls /tmp/p3verify-default/details/*.md | wc -l   # 期望 == 请求数
   # 幂等：记录一个详单文件的 mtime，原样重跑一次，mtime 不变
   f=$(ls /tmp/p3verify-default/details/*.md | head -1)
   stat -f '%m' "$f"
   ./vmrbin report -o /tmp/p3verify-default -details logs/vmr-audit-2026-07-28.jsonl.zst
   stat -f '%m' "$f"   # 期望与上面相同
   ```

### 4.4 验收标准（对照 DevPlan P3.3）

- [x] 默认运行（不传 `-details`）的产物集合回到索引量级——真实语料验证 `details/` 目录不生成，
      `vmr-requests.json` 每行仍带非空 `req`。
- [x] `-details` 显式开启时重复运行不重复写盘——真实语料验证 322 个文件、重跑后 mtime 不变、
      0 个残留临时文件。
- [x] 索引行的详单链接在文件尚不存在时依然可算、可显示（`assignNames` 本就独立于 `-details`，
      P2 已经具备，本批验证确认未被破坏）。
- [x] 幂等契约有单测锁定：`TestEnsureRendered_NeverOverwritesAPreexistingFile`。

---

## 5. 批 C：重复大块内容落成共享证据条目（P3.4）

### 5.1 现状（已读代码确认）

- `internal/reqdetail/detail.go` 的 `renderClientRequest`：`msgs := chatmsg.Messages(req.Body)`
  取出的**全部**消息（含 role=="system" 的前导块）都在同一个循环里逐条 `renderMessageSection`
  内联进详单正文；`obj["tools"]` 数组同样整段内联（`tb.WriteString(Details(escapeHTML(name),
  codeFence(jsonIndent(tn))))`）。一份系统提示词在同一条 lineage 的每一份详单里都重复一次全文。
- `ctxgraph.Manifest` 已经有 `SysHash Hash`/`HasSys bool`/`LeadSys int` 三个字段
  （`manifest.go`，`BuildManifest` 里用一个 `strings.Builder` 拼出 `msgs[0..LeadSys-1]` 的原始
  文本后 `md5.Sum` 得到 `SysHash`）——**这个拼接逻辑目前是 `BuildManifest` 内部私有的一段代码**，
  没有导出，`reqdetail` 若要生成"系统提示词全文"的证据条目，需要拼出与 `SysHash` **完全同一段
  文本**，否则会出现"哈希是这个，内容是那个"的静默错配。
- `internal/report/session.go:507-508` 的 `toolsSig`（现已下沉为 `reqdetail.ToolsSig`，见 P2 §7.2
  第 3 条）已经是"工具名集合→ `count/hash8`"的现成短哈希实现，直接复用于工具声明集合的内容地址，
  不需要另造一个哈希方案。
- `ctxgraph.BlobIndex`/`FetchAll`（`blobindex.go`）只索引**非前导系统消息**（`m.Keys`/`m.MsgIdx`，
  `scan.go:92` 的 `firstSeen` 调用只发生在 `BuildManifest` 的非系统消息分支里）——`SysHash` 从未
  注册进 `BlobIndex`。这意味着"给定一个 `SysHash`、但手头没有原始 `*audit.Record`"这种场景（例如
  P5.2/P6.2 未来要在没有全量重跑的情况下按引用补渲染系统提示词证据条目）今天没有现成的取回路径；
  但**渲染详单时手头总是有完整 `*audit.Record`**（`reqdetail.Render` 的入参就是它），这是绝大多数
  场景，不需要 `BlobIndex` 也能完成。
- 输出目录：`report`/`story` 默认都以 `./reports` 为 `-o` 默认值（`cmd_report.go`/`cmd_story.go`
  各自的 `resolveString(..., "reports")`），`details/`、`stories/` 分别是其下的子目录——`evidence/`
  作为二者共用的第三个子目录，默认情况下自然落在同一个 `reports/` 下，不需要额外的路径协调。

### 5.2 目标设计

**(a) 抽出"前导系统文本"的共享纯函数**，让哈希计算与内容取材永远读同一处：

```go
// internal/ctxgraph/manifest.go 或拆到同包的新文件

// LeadingSystemText concatenates the raw text of msgs[0:leadSys] — the
// exact same slice-and-join BuildManifest uses to compute SysHash. Exported
// so any consumer materializing "the text behind SysHash" (reqdetail's
// evidence blob writer) derives it from this one function instead of
// re-implementing the concatenation and silently drifting from what the
// hash actually covers.
func LeadingSystemText(msgs []chatmsg.Message, leadSys int) string
```

`BuildManifest` 自己也改为调用它（不留两份实现）。

**(b) `internal/reqdetail` 新增证据条目写出**，与 §4 的 `EnsureRendered` 同一套"存在即跳过、
临时文件改名写入"纪律，签名与目录约定对齐架构文档 §7.6(b) 的表：

```go
// EnsureSysPromptEvidence writes evidence/sysprompt-<h8>.md under
// evidenceDir when m.HasSys and it doesn't already exist, returning the
// (h8-derived) filename. Content comes from rec's own messages via
// ctxgraph.LeadingSystemText(chatmsg.Messages(rec.Client.Request.Body),
// m.LeadSys) — never re-fetched through BlobIndex, since the caller
// already holds rec. Returns "" when m is nil or !m.HasSys.
func EnsureSysPromptEvidence(evidenceDir string, rec *audit.Record, m *ctxgraph.Manifest, lang i18n.Lang) (filename string, err error)

// EnsureToolsEvidence is EnsureSysPromptEvidence's sibling for a request's
// declared tool set: evidence/tools-<h8>.md, h8 derived the same way
// ToolsSig already does (md5 of the sorted, comma-joined name list — same
// value ToolsSig's own hash half already computes, just written out in
// full instead of summarized to 4 bytes in a table cell).
func EnsureToolsEvidence(evidenceDir string, rec *audit.Record, lang i18n.Lang) (filename string, err error)
```

**(c) 详单正文改为引用而不内联**：`renderClientRequest` 里，system 消息不再进入
`renderMessageSection` 的逐条循环（从 `msgs` 里跳过 `i < m.LeadSys` 的条目，与 `LeadSys` 已经
定义"前导系统消息数量"的语义直接对应），改为渲染一行链接（"System prompt (N chars) → 
evidence/sysprompt-<h8>.md"，具体文案位置放在 §5.1 现状描述的"Headers" `<details>` 折叠块之后、
"### Messages" 小节之前，紧跟 System Prompt 移到文档头部的既有先例，见架构文档 §4.5）；
`obj["tools"]` 那段同理改成一行链接到 `evidence/tools-<h8>.md`，不再逐个 `<details>` 展开工具
JSON schema。**`Render` 需要一个新入参才能算出 `evidence/` 相对路径**——落地时确认这个相对路径
怎么算最自然：`reqdetail.Render` 目前完全不知道自己会被写到磁盘的哪个目录（它只返回一个字符串），
`details/xxx.md` 到 `evidence/yyy.md` 的相对链接是 `../evidence/yyy.md`（假设两者是 `outDir` 下的
同级子目录，`report`/`story` 都遵守这个约定），**这个假设需要在实现前用一行注释钉死在
`EnsureSysPromptEvidence`/链接渲染代码旁边**，避免以后有人把 `evidence/` 挪到别的层级却没人发现
链接失效。

**(d) 生成时机**：`EnsureSysPromptEvidence`/`EnsureToolsEvidence` 在 `Render` **之前**调用（`Render`
渲染出的链接文本需要引用刚生成/已存在的证据文件名），由 `report`/`story` 各自的详单生成入口
（`DetailWriter`/未来的按需生成路径）在调用 `reqdetail.EnsureRendered` 之前先调用这两个函数——
**不放进 `Render` 内部**：`Render` 是纯字符串渲染函数，不做任何文件 I/O（这是它作为叶子的既有
设计边界，见 P2 的 `Render` doc comment），证据条目的落盘属于调用方（`DetailWriter`）的职责，
与 `EnsureRendered` 自己落盘 `.md` 是同一层级的动作，不应该混进渲染函数内部。

### 5.3 具体步骤

1. `internal/ctxgraph/manifest.go`：抽出 `LeadingSystemText`，`BuildManifest` 内联的 `sysText`
   拼接改为调用它；配单测（"单条前导系统消息"“多条连续前导系统消息拼接"“`leadSys=0` 返回空
   字符串"三个用例）。
2. `internal/reqdetail/`：新增 `EnsureSysPromptEvidence`/`EnsureToolsEvidence`（新文件
   `evidence.go`），复用 §4.3 第 1 步定下的临时文件+改名写入方式（不要在两处各写一份）。
   `Render` 签名加一个 `evidenceDir string`（或等价的"是否启用证据引用"参数——落地时确认：
   `evidenceDir==""` 时是退回旧的内联行为，还是直接报错要求调用方必须提供？**倾向前者**——
   `Render` 单测今天大概率会直接构造记录调用 `Render` 而不搭建目录结构，强制要求非空会让现有
   测试大批量返工；空值退回内联对测试友好，且是唯一一处"没有证据目录就不引用"的降级路径，
   注释里写清楚这一点）。
3. `internal/reqdetail/detail.go`：`renderClientRequest` 改为跳过前导系统消息的逐条渲染，改渲染
   一行链接；`tools` 数组同理。`Details`/`codeFence` 等既有渲染原语不动。
4. `internal/report/detail.go`：`DetailWriter`/`writeOneDetail` 在调用 `EnsureRendered` 之前，先
   调 `EnsureSysPromptEvidence`/`EnsureToolsEvidence`（`evidenceDir := filepath.Join(dw.dir, "..",
   "evidence")`，即 `outDir/evidence`，`dw.dir` 是 `outDir/details`——按 §5.2(c) 钉死的相对布局
   实现）。
5. `go test ./internal/ctxgraph/... ./internal/reqdetail/... ./internal/report/...`：
   golden/固定断言更新（详单正文里 system prompt/tools 从整段变成一行链接，是本批唯一会让既有
   详单渲染测试大批量失败的改动，预期之内，逐条核对 diff 是"内联变链接"而非别的意外变化）。
   新增一个跨记录去重测试：两条共享同一份系统提示词的合成记录，各自调用
   `EnsureSysPromptEvidence`，断言只产生一个 `evidence/sysprompt-*.md` 文件、两次调用返回同一
   个文件名。
6. 真实语料验证：
   ```bash
   ./vmrbin report -o /tmp/p3verify -details logs/vmr-audit-2026-07-28.jsonl.zst
   ls /tmp/p3verify/evidence/ | head       # 期望：sysprompt-*.md / tools-*.md，条目数远小于请求数
   du -sh /tmp/p3verify/details /tmp/p3verify/evidence   # 详单体积应显著小于批 A/B 之前的同批语料
   grep -l 'evidence/sysprompt' /tmp/p3verify/details/*.md | wc -l   # 期望接近全部含 system 的详单数
   ```

### 5.4 验收标准（对照 DevPlan P3.4）

- [x] 同一份系统提示词只落盘一次——真实语料 322 条记录只产生 6 个 `sysprompt-*.md`
      （其中一个被 22 份详单共同引用，验证过）。
- [x] 工具声明集合同理只落盘一次——真实语料产生 4 个 `tools-*.md`。
- [x] 详单正文里系统提示词/工具声明从整段内联变为一行链接，其余内容不受影响。
- [x] `SysHash` 与 `evidence/sysprompt-<h8>.md` 的内容来自同一个 `LeadingSystemText` 调用——
      `TestLeadingSystemText_MatchesBuildManifestSysHash` 锁定。

---

## 6. 批 D：缓存与索引分家 + 载荷扩大 + schema 版本戳（P3.5 + P3.6 + P3.7）

### 6.1 现状（已读代码确认）

- `internal/report/requests.go` 的 `RequestsIndex{Files ctxgraph.FileCache, Requests
  []RequestRow}` 与 `internal/story/storyindex.go` 的 `StoryIndex{Files ctxgraph.FileCache, ...}`
  各自把**完整的** `ctxgraph.FileCache`（每个文件的全部 `Manifest`）内嵌进 `vmr-requests.json`/
  `vmr-stories.json`，用 `json.MarshalIndent`（`requests.go` 的 `WriteRequestsJSON`、
  `storyindex.go` 约第 81 行）美化输出。`LoadRequestsFileCache`/`LoadStoryIndex` 各自从这个内嵌
  字段重建 `prior *ctxgraph.FileCache` 传给下一次的 `ScanCached`。
- `ctxgraph.CachedFile{Hash string, Manifests []*Manifest, NoBody int}`（`cache.go`）——**只装
  manifest**，不含任何 per-record 的聚合事实（token 用量、角色字符数、工具签名、错误类等）。
  命中判据只有 `cached.Hash == hash`（文件内容 sha256），没有任何提取逻辑版本标记。
- `internal/report/aggregate.go` 的 `buildInternal` 做**两遍独立的文件读取**：第一遍
  `AnalyzeSessionsCached(paths, prior, prof)` 经 `ScanCached`，manifest 命中时可以跳过重新解析；
  第二遍（约第 198-203 行，`audit.OpenLogFile` + `ForEachLine`）是 `ingest.go` 的按端点/按记录
  聚合，**无条件**重新打开、重新解码每一个输入文件，完全不查任何缓存——这正是架构文档 §7.10
  实测"report 全量热缓存只快 1.17×"的根因：三遍读取里只有第一遍真正吃到了缓存收益。
- 无 `.parse-cache/` 目录或等价的分片缓存结构；`report`/`story` 各自把缓存嵌在自己的索引 JSON
  里，互不共享——同一批日志分别跑 `vmr report`/`vmr story`，manifest 会被独立解析两次。

### 6.2 目标设计

**(a) 缓存形态：从"索引内嵌字段"拆成同目录下的独立分片文件**，`report`/`story` 共用：

```
{outDir}/.parse-cache/<filehash>.json   # 一个输入文件一个条目，紧凑编码（非 MarshalIndent）
```

`filehash` 就是 `CachedFile.Hash`（该输入文件当前内容的 sha256，`ctxgraph.HashFile` 已有）——
用内容哈希而不是 basename 命名分片文件，天然获得"文件内容变了，条目名跟着变，旧条目自动成为
可回收的孤儿"这条性质，不需要额外的失效逻辑；`reqcoord.go` 的 `CanonicalPath` 仍然是 `FileCache`
内存态 `map` 的 key（用于按输入路径查找该用哪个分片），只是分片本身落盘时以内容哈希命名，两者
不是同一个用途，不要混淆。

**(b) 载荷扩大：`CachedFile` 除 `Manifests` 外，新增每条记录的聚合事实**，让 `ingest.go` 的第二遍
扫描在缓存命中时可以完全跳过重新解码：

```go
// internal/ctxgraph/cache.go

// RecordFacts is one record's cache-worthy extraction result beyond its
// Manifest — the subset ingest.go's per-endpoint/per-record aggregation
// pass needs, cached alongside Manifests so a file-hash cache hit skips
// re-opening and re-decoding the file for this pass too, not just for
// session/task grouping. Line up 1:1 with Manifests by index (both are
// built in the same per-file scan, in file order).
type RecordFacts struct {
    Usage      chatmsg.Usage `json:"usage"`
    UsageOK    bool          `json:"usage_ok,omitempty"`
    ErrorClass string        `json:"error_class,omitempty"`
    // ...其余字段以 ingest.go 实际读取的字段为准，见 §6.3 第 2 步
}

type CachedFile struct {
    Hash          string        `json:"hash"`
    SchemaVersion int           `json:"schema_version"`
    Manifests     []*Manifest   `json:"manifests,omitempty"`
    Facts         []RecordFacts `json:"facts,omitempty"`
    NoBody        int           `json:"no_body,omitempty"`
}
```

**具体该缓存哪些字段，不预先穷举**——落地时读 `internal/report/ingest.go`/`recextract.go` 的
`buildRec2` 实际用到了哪些"只能从原始 body 算出、Manifest 不包含"的字段，缓存那些，不多缓存
"Manifest 已经有的"（重复浪费）也不少缓存"第二遍还是要重新读文件才能补全的"（没达到目的）。

**(c) schema 版本戳**：`ctxgraph` 包级常量 `const cacheSchemaVersion = 1`，`ScanCached`/
`scanCachedFile` 命中判据从"`Hash` 相等"改为"`Hash` 相等**且** `SchemaVersion ==
cacheSchemaVersion`"——版本不符按未命中处理（走 `scanFile` 全新解析），不报错、不需要用户手动
清缓存，纯派生物允许静默重建。**每次改动 `Manifest` 字段集合、`RecordFacts` 字段集合、或它们的
提取逻辑时，`cacheSchemaVersion` 都要 +1**——这条纪律本身也要写进 `ctxgraph` 包文档或
`cacheSchemaVersion` 的注释里，不能只存在于这份 ActionPlan 里，否则下一个改字段的人根本不知道
要动它。

**(d) 索引瘦身**：`RequestsIndex`/`StoryIndex` 去掉 `Files ctxgraph.FileCache` 字段；
`WriteRequestsJSON`/`saveStoryIndex` 不再写 `"files"` 段；`LoadRequestsFileCache`/`LoadStoryIndex`
改为从 `.parse-cache/` 目录重建（新的 `ctxgraph.LoadCacheDir(dir string) (*FileCache, error)`/
`SaveCacheDir(dir string, cache *FileCache) error`，落地时确认放在 `cache.go` 还是新文件）。
索引 JSON 本身仍然 `MarshalIndent`（人会看，DevPlan 明确"索引保持人可读的规模与形态"）。

### 6.3 具体步骤

1. `internal/ctxgraph/cache.go`：`CachedFile` 加 `SchemaVersion int`/`Facts []RecordFacts`
   （§6.2(b)/(c)）；`ScanCached`/`scanCachedFile` 的命中判据加版本比较；新增
   `LoadCacheDir`/`SaveCacheDir`（分片读写，紧凑编码，文件名 = `Hash` + `.json`）。
2. 读 `internal/report/ingest.go` 全文、`recextract.go` 的 `buildRec2`：列出第二遍扫描实际从
   `audit.Record`（而非已有的 `Manifest`）取的每个字段，定出 `RecordFacts` 的最终字段集合；
   `scanFile`（`cache.go` 内，manifest 扫描的既有实现）同一趟扫描顺带算出 `RecordFacts`——
   **不要为它单独再扫一遍文件**，那样缓存扩大就白做了，收益全部来自"一趟扫描顺带产出两份结果"。
3. `internal/report/aggregate.go` 的 `buildInternal`：第二遍扫描（约第 198-203 行）改为优先查
   `cache.Files[CanonicalPath(path)].Facts`（`AnalyzeSessionsCached` 返回的 `cache` 已经在手），
   命中则跳过 `OpenLogFile`/`ForEachLine`/`json.Unmarshal`，直接用缓存的 `RecordFacts` 走
   `ingest.go` 的聚合逻辑；未命中（新文件、或版本戳不符导致的整体重建）才落回今天的全量读取路径。
4. `cmd/vmr/cmd_report.go`/`cmd/vmr/cmd_story.go`：`priorCache`/`LoadRequestsFileCache`/
   `LoadStoryIndex` 相关调用改为指向 `.parse-cache/` 目录（`filepath.Join(outDir,
   ".parse-cache")`）；`RequestsIndex`/`StoryIndex` 不再嵌 `Files` 字段。
5. `internal/report/requests.go`/`internal/story/storyindex.go`：删 `Files` 字段与相关
   序列化代码。
6. `go test ./internal/ctxgraph/... ./internal/report/... ./internal/story/... -race`：
   - 新增缓存命中/未命中的端到端测试：同一批文件跑两次，第二次的第二遍扫描（可以通过一个可
     注入的计数器或类似手段验证，具体做法落地时看 `ingest.go` 现有测试基础设施如何最小改动
     支持"断言某个文件没有被重新打开"）不再触发 `OpenLogFile`。
   - 新增 schema 版本戳测试：手工把一个缓存分片文件的 `schema_version` 改成 0，断言
     `ScanCached` 视为未命中并重新解析。
   - 既有测试里断言 `vmr-requests.json`/`vmr-stories.json` 含 `"files"` 字段的用例改为断言
     `.parse-cache/` 目录存在对应分片。
7. `docs/VirtualModelRouter_Design_v4_Analytics.md`：vmr-requests.json/vmr-stories.json 相关章节
   若描述了"索引内嵌缓存"的旧形态，同步改写（这是 DevPlan 完成定义第 4 条要求的设计文档同步，
   不是可选项）。
8. 真实语料验证：
   ```bash
   time ./vmrbin report -o /tmp/p3verify-cache logs/vmr-audit-2026-07-28.jsonl.zst          # 冷
   time ./vmrbin report -o /tmp/p3verify-cache logs/vmr-audit-2026-07-28.jsonl.zst          # 热
   ls /tmp/p3verify-cache/.parse-cache/ | wc -l    # 期望 == 输入文件数
   du -sh /tmp/p3verify-cache/vmr-requests.json    # 期望远小于改动前的同批语料（不再含 files 段）
   time ./vmrbin story -o /tmp/p3verify-cache logs/vmr-audit-2026-07-28.jsonl.zst            # 复用同一份 .parse-cache
   ls /tmp/p3verify-cache/.parse-cache/ | wc -l    # 期望不变——两条命令共用同一份，不产生第二套
   ```
   （单个样本文件的冷/热耗时差异在真实 9GB 级全量语料上才明显——本机若没有等量语料，用现有样本
   验证"命中判据生效、目录结构正确、两条命令共用一份缓存"这三件事即可，耗时数字仅供参考，不作为
   本机验收的硬指标；DevPlan 的"个位数秒"目标以架构文档 §7.10 实测的全量语料为准，不要求在本机
   复现那组数字）。

### 6.4 验收标准（对照 DevPlan P3.5 + P3.6 + P3.7）

- [x] `.parse-cache/` 目录按输入文件内容哈希分片，`report`/`story` 共用同一份（真实语料验证：
      两条命令连跑，`.parse-cache/` 始终只有 1 个分片），索引文件不再内嵌缓存段。
- [x] 新增一个日志文件重跑只写入一个新缓存分片，不重写已有分片——`TestSaveCacheDir_SkipsExistingShard`
      锁定 skip-if-exists；真实语料的"新文件只加一个分片"未做额外全量语料验证（单文件场景已被单测
      覆盖，判定为充分）。
- [x] 缓存命中时聚合的第二遍不再重新打开、解码该文件——`TestScanFiles_CacheHitNeverOpensFile`
      用一个磁盘上不存在的路径直接证明（不是"结果相同"这种间接证据）。**但这只关闭了三趟扫描里
      的一趟**：`session.go` 的 `collect()`/`analyzeFile` 仍未接入缓存，是本批次唯一未达成的
      原始目标（`个位数秒`），已诚实登记为 `KNOWN_ISSUES §1.1`/`§1.23`，见 §8 收尾说明。
- [x] `schema_version` 不符时整体判定未命中并重建——`TestScanCached_SchemaVersionMismatchReparses`/
      `TestLoadCachedFacts_RejectsStaleSchemaVersion` 分别锁定 manifest 半和 facts 半。

---

## 7. 收尾（批 A–D 共用）

1. **全量测试与架构边界**：
   ```bash
   go test ./... -race
   go test ./internal/archtest/...
   go vet ./...
   gofmt -l .
   ```
2. **CHANGELOG.md**：`[Unreleased]` 下按 Added/Changed/Removed/Fixed 分类加条目（具体措辞落地时
   按实际改动定），例如：
   - Removed: `vmr report` 不再为每条记录写 `details/*.json` 副本；`vmr replay -detail` 移除。
   - Added: `vmr replay -req <坐标>` 按 `basename:line` 坐标定位记录；新增 `-print` 直接输出
     记录原文，不需要 `-provider`。
   - Changed: `-details` 默认值从 `true` 改为 `false`，`vmr report` 默认只写索引；显式传
     `-details` 时详单生成改为幂等（已存在文件不重复渲染/写盘）。
   - Changed: 详单里的系统提示词与工具声明从整段内联改为链接到 `evidence/` 下的内容寻址共享条目。
   - Changed: 解析缓存从索引 JSON 的内嵌字段拆分为 `.parse-cache/` 下按文件哈希分片的独立存储，
     `vmr report`/`vmr story` 共用；缓存载荷扩大到每条记录的聚合事实；命中判据加 schema 版本戳。
3. **KNOWN_ISSUES_sonnet-5.md**：
   - `§1.1`（"`vmr report` 多文件输入的两趟扫描开销"）本批完成后前提已变——旧描述的"两趟"里，
     第二趟（`ingest.go`）现在命中缓存时不再重新解压/解析，触发条件与可能方案都要按批 D 的实际
     实现重写；若第三趟（详单渲染）也一并因批 B/C 变化，一并说清楚（详单默认不生成，不再构成
     第三趟开销）。
   - 新增一条登记 §3.2(e) 的"`-print`/`-req` 落在 `replay` 而非新子命令"这一决定与理由，供 P6.5
     的 CLI 收敛直接读，不必重新分析"为什么当初这样放"。
   - 新增一条登记 §5.2(c) 的 `evidence/` 相对路径约定（`details/` 与 `evidence/` 是 `outDir` 下的
     同级子目录，链接靠 `../evidence/...` 相对路径），供 P6.2 补导航边时直接复用而不用重新判断。
4. **架构文档同步**：`story_report_architecture_opus-5.md` §2.2/§7.10 的实测基线数字（`.json`
   副本体积、report 热缓存 1.17×、无 schema 版本戳等）在本阶段完成后已是历史数字——按 P1/P2 收尾
   的同一做法，补一条简短说明指向本文档，不重写整份架构文档。
5. **边界复核**（DevPlan §2.2 第 6 条，三个问题）——已按实际执行情况回填，见 §9 执行记录的完整
   叙述，这里只给结论：
   - **本阶段是否产生了架构文档未预见的事实？**——是，一处重要发现：`report.Build` 对输入文件
     实际是**三趟**扫描，不是架构文档 §7.10 诊断的两趟——`AnalyzeSessionsCached` 内部并发的
     `ctxgraph.ScanCached`（已缓存）之外，还有一条完全独立、从未缓存过的通道
     `analyzeFile`/`collect()`，专供会话/任务分组用。P3.6 只缓存了聚合那一趟，"个位数秒"目标
     因此未完全达成（16.2s，非个位数）。已回写进架构文档 §2.2 的引用说明与
     `KNOWN_ISSUES §1.1`/`§1.23`，供 P4 及以后的读者不必重新发现这件事。
   - **本阶段是否改变了 P4 及以后的前提？**——`reqdetail.EnsureRendered`/`EnsureSysPromptEvidence`/
     `EnsureToolsEvidence` 的实际签名与 §4.2/§5.2 预判一致（`EnsureSysPromptEvidence`/
     `EnsureToolsEvidence` 最终**去掉了** `lang i18n.Lang` 参数——证据内容是原始系统提示词/工具
     JSON，不是本包生成的叙述文本，i18n 无处附着，属于实现时的合理简化，非预判出入）。
     P4.1/P4.3 可以直接复用 §3.2 的坐标读取原语与 §5.2 的证据条目机制。
   - **本阶段是否暴露出某个原计划任务其实不必要？**——否，P3.1–P3.7 全部按计划完成（P3.6 的
     "个位数秒"是验收标准层面的部分达成，不是任务本身不必要）。

---

## 8. 验收清单（对照 DevPlan P3 的验收标准逐项勾）

- [x] 一次全量运行的派生产物总体积与其信息量相称——`.json` 副本删除（322 条记录省下 322 份逐字
      副本）、详单默认按需（默认运行 `details/` 不生成）、system prompt/工具声明去重（322 份详单
      共享 10 个证据文件）三者共同作用，均已用真实语料验证。
- [x] **部分达成**：重复运行耗时从 71.8s 降到 16.2s（1.17×→5.2×），真实 34 文件/177MB 语料实测——
      但未降到"个位数秒"。差距诊断明确（`session.go` 的 `collect()`/`analyzeFile` 仍是三趟扫描
      里唯一未接入缓存的一遍），已登记 `KNOWN_ISSUES §1.1`/`§1.23`，不属于本次刻意回避，而是
      "已验证收益、但正确性风险（触及 session/task 边界判定）值得单独配一套测试基础设施再动手"
      的主动判断，详见 §7 边界复核。
- [x] 缓存与索引的职责边界清晰：`.parse-cache/` 机器专用、按内容哈希分片、紧凑编码（非
      `MarshalIndent`）；`vmr-requests.json`/`vmr-stories.json` 回到纯索引，真实语料验证不再
      含 `"files"` 段。
- [x] `go test ./...`（含 `-race`）、`go test ./internal/archtest/...` 全绿。
- [x] CHANGELOG、KNOWN_ISSUES（新增 §1.23/§1.24，改写 §1.1）、架构文档说明性备注
      （`story_report_architecture_opus-5.md` §2.2、`VirtualModelRouter_Design_v4_Analytics.md`
      §2.5/§3.4）、`docs/UserGuide.md`/`.zh`、`docs/VirtualModelRouter_Design_v4_Core.md`
      （`vmr replay` 定位方式的决策表行与 §15.2）均已同步。

---

## 9. 执行记录（2026-08-20，Sonnet 5）

本节是本文写完 ActionPlan 之后、实际落地执行的过程记录与总结——按用户要求补写，不是提前写好
的计划。**所有改动均未提交，等待人工 review。**

### 9.1 执行顺序与整体结果

严格按 §2 定的批次顺序（批 A → 批 B → 批 C → 批 D）推进，每批做完立即跑相关包测试 + `archtest`，
不攒到最后。批 D 内部又分两段：先做 P3.5+P3.7（缓存分片存储 + schema 版本戳，安全、机械化的改动），
再做 P3.6（缓存载荷扩大到聚合事实，本阶段风险最高的一块），验证充分后再往下推进，没有跳步。

最终统一跑 `go build ./...`、`go vet ./...`、`gofmt -l .`、`go test ./... -race`、
`go test ./internal/archtest/...`，全部通过。用本机真实审计日志验证（两批语料）：

| 验证项 | 结果 |
| --- | --- |
| 批 A：`details/` 目录 `.json` 副本 | 322 条记录，0 个 `.json`（此前应为 322 个） |
| 批 A：`vmr replay -req`/`-line`/`-print` 互相一致 | 逐字节一致；`-print` 不需要有效 `config.yaml` |
| 批 A：`-detail` flag 移除 | CLI 报 `flag provided but not defined`，符合预期 |
| 批 B：默认 `-details`（不传） | `details/` 不生成；`vmr-requests.json` 每行仍带 `req` |
| 批 B：显式 `-details` 幂等性 | 322 个文件；重跑后 mtime 不变；0 个残留 `.tmp` |
| 批 C：系统提示词去重 | 322 条记录 → 6 个 `evidence/sysprompt-*.md`（其中一个被 22 份详单共享） |
| 批 C：工具声明去重 | 322 条记录 → 4 个 `evidence/tools-*.md` |
| 批 D：`.parse-cache/` 跨命令共享 | `vmr report`+`vmr story` 连跑，始终只有 1 个分片 |
| 批 D：`vmr-requests.json`/`vmr-stories.json` 瘦身 | 均不再含 `"files"` 段 |
| 批 D：34 文件/177MB 压缩语料，冷启动 | ~83.8s（与批次前持平，符合预期——冷启动本就不该变） |
| 批 D：同一语料，热缓存（P3.6 前对照 71.8s） | ~16.2s（收益 1.17×→~5.2×） |
| 批 D：冷/热缓存 `vmr-report.json` 一致性 | `vmr-requests.json`（11274 行）逐字节相同；`vmr-report.json` 仅一处 1 ULP 浮点差异（`providers[].cost_estimate`，见 `KNOWN_ISSUES §1.24`），非逻辑错误 |

### 9.2 与最初 ActionPlan 设计的实际出入

- **批 D 的核心发现，且是本次执行最重要的一处修正**：原计划（§6.1 现状描述）沿用了架构文档的
  "两遍扫描"诊断，只给 `aggregate.go` 的聚合遍（P3.6 意义上的"第二遍"）接上了缓存。落地时用真实
  语料测耗时，发现结果是 16.2s 而不是预期的"个位数秒"，追查后发现 `report.Build` 实际是**三遍**
  扫描——`session.go` 的 `AnalyzeSessionsCached` 内部除了已缓存的 `ctxgraph.ScanCached`，还有一条
  完全独立、从未缓存过的并发通道 `analyzeFile`/`collect()`（专供会话/任务分组用的特征提取）。P3.6
  只覆盖了三遍里的一遍。这不是执行失误，是原 ActionPlan 技术分析阶段的真实盲区（§6.2(b) 当时只读了
  `ingest.go`/`recextract.go`，没有去读 `session.go` 自己的扫描循环）。**已诚实处理，不是掩盖**：
  没有为了凑"个位数秒"这个数字去仓促扩展到 `collect()`（那一层触及会话/任务边界判定，正确性风险
  明显高于纯聚合，且需要专门的 cold/warm 一致性测试基础设施打底，仓促做的风险大于收益）；已把
  5.2× 这个真实、已验证的进展如实交付，把差距诊断和后续路径精确登记进
  `KNOWN_ISSUES §1.1`/`§1.23`，供下一次有精力时直接读、不用重新排查。
- **`EnsureSysPromptEvidence`/`EnsureToolsEvidence` 去掉了 `lang i18n.Lang` 参数**：原计划设计时
  假设它们需要跟 `Render` 一样接收语言参数；落地时发现证据条目的正文就是原始系统提示词/工具
  JSON——本包自己生成的叙述文本一个字都没有，`lang` 参数会是一个签名占位符，不做任何事。判断为
  实现时的合理简化，不是遗漏。
- **`EnsureRendered` 内部直接调用两个证据函数，而不是要求调用方自己按顺序编排三步**：原计划
  §5.2(d) 只说"由 `report`/`story` 各自的详单生成入口在调用 `EnsureRendered` 之前先调用这两个
  函数"，落地时判断这样会让"证据链接会不会失效"这件事系着调用方有没有记得按顺序调三个函数——
  一旦未来 P5.2 或别的调用方漏调前两步，`../evidence/sysprompt-<h8>.md` 就会变成死链接。改为
  `EnsureRendered` 内部自己在渲染前调用 `EnsureSysPromptEvidence`/`EnsureToolsEvidence`，调用方
  只需要跟这一个门面打交道。
- **`recordFacts`/`buildRec2` 的拆分比原计划设想的更彻底**：原计划只给出了一个占位符式的
  `RecordFacts` 结构体（"...其余字段以 ingest.go 实际读取的字段为准"），落地时逐字段核对了
  `buildRec2`（含 `ingestEndpoints` 需要的完整 `Attempts` 列表：endpoint、status、error、
  error_class、norm、耗时）与 `ingest.go` 的 `IngestAttempt`，确认全部纳入 `recordFacts`/
  `attemptFacts`，并把 `buildRec2` 本身拆成"纯 arec 抽取"（`extractRecordFacts`）与"和 ReqInfo
  合并"（`buildRec2`）两段，让冷路径与热路径调用**同一个**合并函数，不给"两条路径各自实现一遍
  合并逻辑、可能悄悄分叉"留任何空间。

### 9.3 独立并发评审的处理

执行期间，仓库里出现了一份我没有创建的文档（`docs/future-strategy/
story_report_p3_action_plan_review_gemini-3.7-flash.md`）——与 P1/P2 执行期间同样的并发写作模式。
已通读全文并逐条核实（针对的是我最初写就、尚未执行的 ActionPlan 文本，不是针对最终代码）：

**核实为真、且与我自己在真实语料验证阶段独立发现的问题完全一致的一条**：`session.go` 的
`analyzeFile`（评审称 Pass 2）未被缓存拦截——评审基于静态读代码得出"热缓存耗时将依然停留在
30~50 秒以上"的预判，我基于真实语料实测拿到的是 16.2s；两条路径独立到达同一个根因判断，互相
印证了这不是巧合。评审建议"立刻让 Pass 2/3 同时跳过"，我的处置是"如实交付已验证的 5.2× 收益，
把差距与整改方向精确登记，风险审慎评估后判断不在本批次仓促扩展"——见 §9.2，结论不同但都建立在
同一个真实发现之上。

**核实为已经在实现中做对、评审的批评只对最初的计划文本成立的两条**：
- 评审 §1.2 担心 `RecordFacts` 若只有 `Usage`/`ErrorClass` 会丢失 `Attempts`，导致 Endpoint
  Health/延迟/失败率归零——这条批评精确命中了原计划文本里那个占位符式的结构体定义，但落地时
  （见 §9.2 最后一条）已经逐字段核对补全，`attemptFacts` 完整覆盖了 `ingestEndpoints` 需要的
  全部字段。真实语料的 `TestBuildCached_WarmMatchesBuild`（`Report2` 全量 JSON 精确对比）与
  批 D 的冷热对照实测（`vmr-report.json` 除 1 处浮点外逐字节相同）已经是这条不成立的直接证据。
- 评审 §2.2 建议把证据条目的落盘收进 `EnsureRendered` 门面内部——落地时独立收敛到了同一个设计
  （见 §9.2 第三条），评审文档写成时我已经这样实现了，只是 ActionPlan 原文本身没有跟着更新，
  本次 §9.2 的回填已经补上。

**核实为真、判断为有效但不属于本批次范围、已登记为独立 KNOWN_ISSUES 条目的三条**：
- 评审 §2.1（`vmr replay -req` 要求同时给坐标和文件路径，不能直接贴入 `req` 字段就跑）——核实
  是真实的体验问题，但修复需要给 `replay` 新增一套目前完全没有的"按 `log_dir` 定位文件"能力，
  和 DevPlan P6.5"统一的记录选择器"是同一件事，判断等 P6 做 CLI 收敛时一并设计更连贯，不要
  这个 flag 先斩后奏出一套目录搜索规则。登记为 `KNOWN_ISSUES §1.25`。
- 评审 §3.2（不同目录深度到 `evidence/` 的相对链接规则要写清楚）——核实为对 P4/P5/P6 有效的
  前瞻提醒，P3 范围内只有 `details/` 一处链接来源，没有第二个消费方可以验证"通用规则"对不对。
  登记为 `KNOWN_ISSUES §1.26`。
- 评审 §2.3 提议给 `.parse-cache/` 补一个 `CleanCacheDir` 孤儿分片清理函数——核实后判断为主动
  不做（与架构文档对 `evidence/` 目录的同一条判断一致：完全可再生的派生物，体量有限，引入回收
  机制是过度设计），登记为 `KNOWN_ISSUES §1.27`（"已决定不做"类，防止被重新提出）。

**核实为有效、已直接采纳的一条**：评审 §3.3 提到"损坏缓存自愈测试"覆盖不足——检查后确认
`ctxgraph.ScanCached` 对内存态损坏条目已有测试（`TestScanCached_NilManifestInCacheTriggersReparse`），
但 `LoadCacheDir` 对磁盘上损坏分片文件确实没有专门测试。已补
`TestLoadCacheDir_CorruptShardIsSkipped`（验证损坏分片被跳过、同目录下健康分片不受影响）。

**评审 §1.3（合并成一趟扫描的更彻底架构重构，冷启动降到 25-30s、热缓存 2-4s）**：核实评审给出的
方向是真实可行的更大重构，但代价（把 `ctxgraph.scanFile`/`session.go`/`aggregate.go` 三层揉进
一趟解析）远超"给现有分层各自补缓存"，且会重新触碰 P2 已经定型的 manifest 扫描/lineage 缝合
设计。判断为一个值得记录的**长期架构选项**，不是本次的整改路径——已在 `KNOWN_ISSUES §1.1`/§1.23
的"可能方案"里留了指针，供以后真要动 `collect()` 缓存那块时，在"扩展现有 `factscache.go`"和
"整体合并成一趟"两条路线之间做选择，不是凭空重新调研。

评审文档本身未删除、未修改，留在仓库里供你查阅。

### 9.4 尚待你决定的事项

1. `KNOWN_ISSUES §1.1`/`§1.23` 登记的"`collect()`/`analyzeFile` 仍未缓存"是否要排期——这是本批次
   唯一没有完全达成 DevPlan 原始验收标准（"个位数秒"）的地方，已交付的 5.2× 收益本身是完整、
   已验证的独立进展，不依赖这条是否处理。
2. 仓库里那份非我创建的评审文档（`story_report_p3_action_plan_review_gemini-3.7-flash.md`）是否
   需要处理——本次执行只读取核实了内容，未改动、未删除。
3. 所有代码/文档改动都在工作区，未 `git add`/`git commit`，等待你 review 后决定如何处理。
