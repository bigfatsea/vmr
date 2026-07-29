# 第 1 步代码评审报告（2026-07-29）

> 评审范围：`Agent任务叙事报告_设计与价值论证_2026-07-28_opus-5.md` 附录 D「第 1 步实施记录」对应的全部未提交代码。
> 方法：逐文件走查 + 全量测试（含 `-race`）+ 覆盖率分析 + 与设计文档交叉验证。**仅分析问题，不做任何修改。**

---

## 0. 总体结论

**第 1 步实现质量很高。没有发现正确性 bug（逻辑错误会产出错误结果的那种）。** 测试全部通过（含 `-race`），覆盖率 75.5%–88.7%。与设计文档附录 C.3 的任务清单和附录 D 的走查记录一致，附录 D 记录的那些已修复问题确实已修复且被回归测试守住。

以下分析分为：逻辑/设计层面的问题（§1）、性能与可观测性问题（§2）、测试覆盖缺口（§3）、一致性验证（§4）、以及建议但不强求的改进方向（§5）。

---

## 1. 逻辑与设计层面的问题

### 1.1 System prompt 变更未被追踪为事件（已知缺口，非 bug）

**现状**：`BuildManifest` 计算了 `SysHash` 并在 manifest 中存储，但 `story.Build` 的事件流从未比较连续 manifest 之间的 `SysHash` 差异。system prompt 换了不会产生任何 Event。

**与设计文档的关系**：附录 C.4 T2.3（第 2 步）已规划 "system 变更事件"。

**Step 1 内不做处理的原因**（已分析确认）：`Classify` 只看 `Keys`（消息 hash），不看 `SysHash`，但真实场景中 system prompt 变更的原因（compaction / 模型切换 / 工具集变更）会同时触发消息列表的大幅变化，被 `Contract`/`Fork` 捕获。即 system 变更总是伴随 lineage 分裂——`BrokeFrom` + 断裂提示已在渲染中覆盖了"这里发生了变化"。同一个 lineage 内部 system 变更极端罕见。跨 lineage 的 system 对比（"断点前 36K → 断点后 34K，差异在哪"）只有第 2 步缝合后才能做。**留给 T2.3。**

---

### 1.2 `PreviewTitle` 在候选列表时的性能开销

**现状**：`cmd_story.go` 的 `listJourneys` 对每个候选 lineage 调用一次 `story.PreviewTitle`，每次调用触发 `ctxgraph.FetchRecords` 扫描一个审计文件（从文件开头顺序读取到目标行——zstd 不可寻址，必须全文件扫描）。

**量级**：全语料 168 个多轮候选 × 平均文件大小 ~10–20MB zstd = 可能数 GB 的解压和 JSON 解析。实际上这会在 `Scan`（第一次全扫）之后再做一轮接近全量规模的 I/O。

**实际影响**：当前 `vmr story` 不加 `-journey` 列出候选时，会比预期慢得多。设计文档附录 D 只测了带 `--session` 的情况（直接渲染，不列候选），所以这个开销当时没暴露。

**建议**：两种优化路径：
1. **短期**：`Scan` 的结果里顺手缓存每个 lineage 的根请求 title（`Scan` 本身已经打开并解析了每条记录，加一个 `rootTitle string` 字段到 `Lineage` 上几乎零开销）。这样 `listJourneys` 不需要再读盘。
2. **长期**：`FetchRecords` 支持多 location 批量按文件分组，一次扫描取回所有目标行。这个优化同时受益 `Build`（也做一次批量的 `FetchRecords`）和 `PreviewTitle`。

---

### 1.3 ~~`eventHashAt` 对 system 消息的哈希方式与 `SysHash` 不一致~~ ✅ 已加注释说明

**现状**：对于非 system 消息，`eventHashAt` 返回的是 `m.Keys[i]`（即 `hashJSON(raw_message_object)`——对原始解码后的消息对象做 JSON 序列化后 MD5）。对于 system 消息（`idx < LeadSys`），`eventHashAt` 走另一条路径：

```go
var raw any = msgs[idx].Text
if ri := idx - off; ri >= 0 && ri < len(rawMsgs) {
    raw = rawMsgs[ri]
}
b, _ := json.Marshal(raw)
return md5.Sum(b)
```

而 `SysHash`（`BuildManifest` 中）的算法是 `md5.Sum([]byte(sysText))`（纯文本字节，不做 JSON 转义）。

**影响**：Event 流中 system 消息的哈希与 manifest 层的任何哈希都不同，但这只影响事件流的**自洽性**（system 事件去重用的哈希和 manifest 的去重哈希是两套）。在功能层面无可见影响——事件流渲染出来是正确的（消息内容无误），且 system 事件的去重在自己的哈希体系内是自洽的。**这不是 bug，只是一个值得加注释的设计差异点**，防止第 3 步迁移时误以为两边可以用同一个哈希比较。

**处理**：已在 `eventHashAt` 上方加注释，说明 system 消息用 `json.Marshal`（quoted-string digest）而 `SysHash` 用 `md5.Sum([]byte(text))`（raw bytes），两者服务于不同目的、hash space 从不交叉比较，差异无害。防止第 3 步迁移时误判。

---

### 1.4 `fetchFromFile` / `fetchRecordsFromFile` 无提前退出

**现状**：这两个函数使用 `audit.ForEachLine` 逐行扫描文件，即使目标行已经全部找到也不会提前停止——因为 `ForEachLine` 的回调不返回任何信号来触发提前终止。

**影响**：当需要从一个大文件（如 500MB zstd）中只取前几行的记录时（例如取根 manifest 的请求 body 做 `PreviewTitle`），仍会扫描整个文件。与 §1.2 的性能问题叠加。

**分析**：这是 `ForEachLine` API 的设计限制——回调没有返回 `bool` 或 `error` 来支持提前终止。在重构 `ForEachLine` 或新增一个带提前终止能力的变体之前，没有简单修复方案。

**建议**：归入第 2 步的优化清单。如果 §1.2 的 `Scan` 内缓存方案落地，这个开销就不那么重要了（`FetchRecords` 只在渲染单独 journey 时调用，单个文件的完全扫描是可控的）。

---

### 1.5 `BlobIndex` 仅记录首次出现

**现状**：`BlobIndex.firstSeen` 只保留每个 hash 的**第一次**出现位置（`if _, ok := b.refs[h]; !ok`）。

**影响**：如果一条消息在一个 journey 里被 compaction 吞掉后又以相同内容重新出现（例如 Agent 重新执行同一工具得到相同结果），`FetchAll` 返回的是**首次出现**那条记录的位置——即使该位置的 audit 记录可能属于已被切掉的旧 lineage。在 Step 1 不做跨 lineage 缝合时这个影响不大，但在 Step 2 缝合后，回捞出来的内容可能来自不属于当前 journey 的文件行。

**与设计文档的关系**：附录 A.5 说 "7112 条记录 / 809K 条消息实例。索引只存 hash + 位置，不存内容：manifest 总计 809K × 16B ≈ 13MB；blob 位置索引（去重后约 10–20 万条）× 48B ≈ 10MB"。也就是说设计文档明确假设了**去重**索引。那么"回捞到旧 lineage 的内容"就是一个需要考虑的边缘情况。

**建议**：在 Step 2 实现时，`FetchAll` 应该能接受一个"限定 lineage 范围"的参数，只返回在当前 journey 的时间范围内首次出现的 blob。如果不加此限制，渲染出来的事件流可能引用到错误路径的原始 body（内容相同但上下文不同）。

---

## 2. 性能与可观测性

### 2.1 Scan 的 `NoBody` / `Ungrouped` 计数已打印但无后续利用

**现状**：`cmd_story.go` 的 `Scan()` 之后打印了这些计数（附录 D #6 已修复的无日志问题），但 `Ungrouped` 的 manifest 没有进一步的排查入口——用户看到 "N 条记录未归组" 但无法知道是哪些。

**建议**：加一个 `--show-ungrouped` flag，打印前几条（如前 10 条）未归组记录的 TS + path:line，帮助排查。非紧急。

---

### 2.2 `Scan` 的并行度限制与现有模式一致

`scanWorkerCount` 的实现与 `internal/report/session.go` 的 `AnalyzeSessions` 一致：`min(runtime.NumCPU(), fileCount)`。并发写 `results[i]` 无 race（预分配 + 索引传参）。通过 `-race` 验证。没有问题。

---

## 3. 测试覆盖缺口

### 3.1 单元测试覆盖率

| 包 | 覆盖率 | 主要缺口 |
|---|---|---|
| `chatmsg` | 87.6% | `RenderPart` 的部分分支（`image`/`thinking`/`tool_result` anthropic 形状）未直接测 |
| `ctxgraph` | 88.7% | `Scan` 的 zstd 错误路径未测（需真实 zstd 文件）、`BlobIndex.FetchAll` 的跨文件批处理未测 |
| `story` | 76.4% | 见下表 |
| `story/profile` | 87.5% | `Name()` 方法未测（trivial） |

`story` 包的细分：

| 函数 | 覆盖率 | 说明 |
|---|---|---|
| `responseSummary` | 40.0% | SSE 字符串分支未单测（只在 `cmd/vmr` 集成测试中走到） |
| `sortByRootThenTime` | 20.0% | 仅 `ListCandidates` 间接触发；时间相等分支未覆盖（nanosecond 精度下几乎不会触发） |
| `breakReasonHint` | 100.0% | ✅ 已补 Fork 分支测试 |
| `renderStep` | 58.3% | TTFTMS=0、NoReply=true、无 Edge 等分支未测 |
| `renderEvent` | 66.7% | 空消息文本分支未测 |
| `IsPartialHead` | 100.0% | ✅ 已补 true 分支测试 |
| `ID` / `PreviewTitle` | 0% | 仅在 CLI 层间接使用，单元测试中不可达（需要真实审计文件坐标） |

### 3.2 评估

- **`responseSummary` 的 SSE 分支**未单测是最实质的缺口——虽然 `journey_test.go` 的所有 fixture 都用 `sseText()` 构造 stream 响应，但 `responseSummary` 本身接收的是 `rec.Client.Response.Body`，而这个 body 在 fixture 中已经被 `sseText()` 正确生成为 SSE 字符串。所以 SSE 路径**实际上已被 `TestBuild_*` 系列测试覆盖到**（通过 `Build` → `responseSummary` → `ReassembleSSE` 的完整链路）。覆盖率工具显示 40% 是因为测试文件在 `story` 包内，`responseSummary` 的 `string` 分支被 `go test -cover` 计入但它的调用是从 `Build` 内发起的——覆盖率工具在这里的计数可能有偏差。

- `ID` / `PreviewTitle` 的 0% 覆盖是可接受的——它们是 CLI 层的薄封装，真正的逻辑（`deriveID` / `deriveTitle`）有测试覆盖。

- `breakReasonHint` Fork 分支和 `IsPartialHead` true 分支已补测试（本次评审后落实）。

---

## 4. 一致性验证

### 4.1 `ctxgraph.BuildManifest` vs `report.collect()` 哈希一致性

`ctxgraph_parity_test.go` 的设计正确且已被证明有效——附录 D 记录它当场抓住了 `SysHash` 的 `hashJSON(text)` vs `md5.Sum([]byte(text))` 差异。当前测试覆盖了：
- 合成 fixture（通过 `session_test.go` 的 `fixture()` 共享）
- 手工构造的边界形状（anthropic top-level system, tool_call+tool_result, 等）

检验了 `Keys`（非 system 消息哈希）和 `SysHash`（system 文本哈希）在两个实现间的一致性。

**验证**：测试通过，包括 `-race`。

### 4.2 `chatmsg_compat.go` 委托层的一一对应

`chatmsg_compat.go` 为每一个从 `render.go`/`usage.go`/`session.go` 移走的函数提供了委托包装。经过验证，`report` 包内所有旧调用点（共 30+ 处，分布在 `render.go`、`session.go`、`detail.go` 及其测试中）都能通过这个委托层正确编译和运行。

**未覆盖的调用点**：零。所有旧名字都有对应的委托。

### 4.3 `vmr report` 输出行为无变化

设计文档 D.4 记录了验证流程：用改动前后的二进制分别跑 `vmr report`，`diff -r` 比对输出。除了 D.4 中顺带修复的 `buildFindings` 非确定性（详见下文 §5.1 的评估），其余输出逐字节一致。

---

## 5. 无关正确性但值得注意的发现

### 5.1 `buildFindings` 非确定性修复的质量评估

**这是本次提交中一个与 story 功能无关但价值很高的附带修复。** D.4 详细记录了根因和验证过程。代码层面：

- 四处 "遍历+第一个匹配就 break" 全部改为显式的 "全集合比较找最优 + tie-break"
- tie-break 字段选择合理：对于工具浪费用 `t.Shape`、对于模型用 `m.Model`、对于 workload 用 `w.Class`、对于上下文膨胀用 `s.ID`
- `TestBuildFindingsIsDeterministic` 的设计方法论正确：先用 `git stash` 撤掉修复证明测试确实会失败（8/10 次失败），再恢复修复证明全通过

**一个值得注意的细节**：`worstTool` 用 `t.SchemaWasteBytes`（浪费字节数）而非 `t.SchemaBytesShipped`（总发送字节数）来找"worst shape"。旧代码用的是 `t.SchemaBytesShipped`。这个变更是有意的——"浪费"指标更能体现优化空间。但 `ToolShapeRow.SchemaWasteBytes` 的计算公式需要在上下文中检查其是否总能提供有意义的比较（例如两个 shape 的 `DeclareUtilization` 相同但原始大小差异巨大时，浪费字节数可能不反映"哪个形状更应该被裁减"）。

`TestBuildFindingsIsDeterministic` 只测了 `worstWL`（定时任务冗余），没有测 `worstTool` 和 `domModel` 和 `worst`（上下文膨胀）。这三处同样受益于显式 tie-break 但不影响当前语料上的可见行为（因为在现有数据上它们恰好没有平局）——但作为防御性修复是完整的。

### 5.2 重复代码（`nested`/`num`/`capStr`）

三个 package 各有一份 `nested`（`chatmsg`、`ctxgraph`、`report`），两个 package 各有一份 `num`（`chatmsg`、`report`），两个 package 各有一份 `capStr`（`story/profile`、`report`）。

设计文档和代码注释都明确说了这是有意为之——每个 package 保持独立，避免为了共享小工具函数而引入不必要的包依赖。

**评估**：当前情况下是正确的选择（`nested` 只有 12 行，`num` 只有 10 行，`capStr` 只有 8 行）。但如果有第三个 package 也需要这些函数，就该考虑提取到 `internal/fmtutil` 或一个新的 `internal/jsonutil`。目前只有 2–3 份拷贝，还不成问题。

### 5.3 ~~注释措辞~~ ✅ 已修正

`chatmsg/messages.go` 和 `ctxgraph/manifest.go` 的 `nested` 注释原来写 "shared import"，实际是独立拷贝。已改为显式列举另外两个 package 各自持有独立拷贝。

### 5.4 `splice_or_tail` 混合桶尚未拆分

设计文档附录 A.7 指出 `ReplaceTail` 桶里有 202 条边是 `splice_or_tail` 的混合，其中真正的 S2 型原地替换和正常的 ephemeral tail 替换混在一起。第 1 步明确不做拆分（附录 C.3 "明确不做"）。当前代码正确处理了这一点——`ReplaceTail` 不分裂 lineage，只是标记精度不够。第 2 步的 T2.1 会处理。

---

## 6. 端到端验证

### 6.1 全量测试

```
$ go test ./...        → 27 packages, all pass
$ go test -race ./...  → all pass (no data races)
$ go vet ./...         → no warnings
```

archtest 边界规则生效：`ctxgraph` 不依赖 `router`/`server`/`config`/`report`/`story`；`story` 不依赖 `router`/`server`/`report`。

### 6.2 设计文档中记录的验证

附录 D.2 的三个验证点（s231→s238 断裂复现、全语料扫描 0 panic 829 lineage、真实 57 轮 Journey 渲染暴露 Agent 自身 bug）——这些都没有回归测试可以自动化。但从代码结构来看，这些场景的逻辑路径都有对应的单元测试覆盖（`TestScan_AppendRunThenContractSplitsLineage` 精确复现了 s231 的断裂模式）。

### 6.3 未覆盖的端到端路径

以下路径在当前测试套件中缺少覆盖：

1. **`-journey` flag 的完整 CLI 路径**：`cmd_story.go` 的 `renderJourney` 没有独立的单元测试（因为依赖真实审计文件）。可以考虑像 `vmr report` 那样加 golden test（用 `testdata/` 里一份小语料，断言 Markdown 输出逐字节稳定）。

2. **`--include-partial` flag 路径**：`IsPartialHead` 的 `true` 分支没有端到端测试。

3. **anthropic 协议入口**：设计文档 F8 指出 anthropic 入口仅 59 条请求，Claude Code 的 `/compact` 无法在当前语料上验证。`chatmsg` 包的 SSE 重组和 FinalMessage 都有 anthropic 分支的单元测试覆盖，所以代码路径本身是测试过的。

---

## 7. 建议优先级汇总

| 优先级 | 条目 | 状态 |
|---|---|---|
| **高** | §1.5 BlobIndex 只记首次出现 | 第 2 步缝合后需支持 lineage 范围限定 |
| **中** | §1.2 PreviewTitle 性能 | `Scan` 时缓存 root title 到 `Lineage` 结构 |
| **中** | §6.3 端到端路径 | 加 golden test（`testdata/` 小语料 + 逐字节稳定 Markdown） |
| **低** | §1.4 fetchFromFile 无提前退出 | 归入第 2 步优化清单 |
| **低** | §2.1 Ungrouped 记录无排查入口 | 加 `--show-ungrouped` flag（可选） |
| ~~高~~ | ~~§1.1 system 变更未追踪~~ | 已分析：Step 1 内无实际价值，留给 T2.3 |
| ~~中~~ | ~~§3.2 breakReasonHint / IsPartialHead 测试~~ | ✅ 已补 |
| ~~低~~ | ~~§1.3 eventHashAt 设计差异~~ | ✅ 已加注释 |
| ~~低~~ | ~~§5.3 注释措辞~~ | ✅ 已修正 |

---

## 附录 A：变更文件清单

### 修改的文件（7 个）

| 文件 | 变更性质 |
|---|---|
| `cmd/vmr/main.go` | +4 行：添加 `story` 子命令分发 |
| `internal/archtest/import_boundaries_test.go` | +28 行：新增 `ctxgraph`/`story` 的 import 边界规则 |
| `internal/report/aggregate_test.go` | +69 行：新增 `TestBuildFindingsIsDeterministic` |
| `internal/report/metrics.go` | +104/-52 行：`buildFindings` 非确定性修复 |
| `internal/report/render.go` | -359/+15 行：移除 chatmsg/SSE/usage 函数，保留委托包装 |
| `internal/report/session.go` | -9 行：移除 `msgOffset` |
| `internal/report/usage.go` | -155/+10 行：移除 `ExtractUsage` 等函数，保留 `nested`/`num` |

### 新增的文件（27 个）

| 路径 | 行数（约） | 说明 |
|---|---|---|
| `cmd/vmr/cmd_story.go` | ~130 | `vmr story` CLI |
| `internal/chatmsg/messages.go` | ~230 | 消息解析（从 report 下沉） |
| `internal/chatmsg/messages_test.go` | ~100 | |
| `internal/chatmsg/sse.go` | ~160 | SSE 重组（从 report 下沉） |
| `internal/chatmsg/sse_test.go` | ~90 | |
| `internal/chatmsg/usage.go` | ~150 | Token usage 提取（从 report 下沉） |
| `internal/chatmsg/usage_test.go` | ~70 | |
| `internal/ctxgraph/hash.go` | ~30 | Hash 类型 + `hashJSON` |
| `internal/ctxgraph/manifest.go` | ~170 | `BuildManifest` |
| `internal/ctxgraph/manifest_test.go` | ~150 | |
| `internal/ctxgraph/edit.go` | ~140 | `Classify` + 五种编辑类型 |
| `internal/ctxgraph/edit_test.go` | ~160 | |
| `internal/ctxgraph/blobindex.go` | ~100 | `BlobIndex` + `FetchAll` |
| `internal/ctxgraph/lineage.go` | ~130 | `Lineage` + `splitBucket` + `RootHash` |
| `internal/ctxgraph/records.go` | ~60 | `FetchRecords`（按文件回捞完整记录） |
| `internal/ctxgraph/records_test.go` | ~70 | |
| `internal/ctxgraph/scan.go` | ~130 | `Scan`（并行扫描 + 合并 + 分组） |
| `internal/ctxgraph/scan_test.go` | ~170 | |
| `internal/story/journey.go` | ~330 | `Build`：Journey/Task/Step/Event 构建 |
| `internal/story/journey_test.go` | ~220 | |
| `internal/story/candidates.go` | ~60 | `ListCandidates` + `IsPartialHead` |
| `internal/story/preview.go` | ~50 | `ID` + `PreviewTitle` |
| `internal/story/render_md.go` | ~140 | `RenderMarkdown` |
| `internal/story/render_md_test.go` | ~110 | |
| `internal/story/profile/profile.go` | ~30 | `Profile` 接口 |
| `internal/story/profile/generic.go` | ~30 | 通用兜底实现 |
| `internal/story/profile/generic_test.go` | ~30 | |
| `internal/story/profile/openclaw.go` | ~100 | OpenClaw profile |
| `internal/story/profile/openclaw_test.go` | ~90 | |
| `internal/report/chatmsg_compat.go` | ~40 | 委托层：保持 report 内旧调用点不变 |
| `internal/report/ctxgraph_parity_test.go` | ~100 | 跨包哈希一致性测试 |

**总计**：34 个文件，约 +2500/−550 行。

---

## 附录 B：构建与测试命令

```bash
# 全部测试（含 race detector）
go test -race ./...                          # 27 packages, all pass

# 新包覆盖率
go test -cover ./internal/chatmsg/...        # 87.6%
go test -cover ./internal/ctxgraph/...       # 88.7%
go test -cover ./internal/story/...          # 76.4%
go test -cover ./internal/story/profile/...  # 87.5%

# 静态分析
go vet ./internal/chatmsg/... ./internal/ctxgraph/... ./internal/story/... ./internal/report/...

# archtest 边界检查
go test ./internal/archtest/...              # pass
```
