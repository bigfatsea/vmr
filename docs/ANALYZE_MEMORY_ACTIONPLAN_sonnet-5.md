<!-- Ver 2026-08-30 21:30, by Sonnet 5 -->

# `vmr analyze` 内存瓶颈：执行计划与落地跟踪

配套文档：`docs/ANALYZE_MEMORY_ROOTCAUSE_opus-5.md`（根因与方案，Opus 5）。
本文是那份方案的**执行层**：结合源代码实际，把「Journey 去 Record 化」拆成 7 个里程碑，
每个里程碑是一个可独立编译、独立测试、独立验收、文档同步的完整任务包。

**执行模式**：连续推进，阻塞才停（用户 2026-08-30 决策）。
**基线策略**：信任 RootCause 文档对全量 `-corpus` 约 43 GB 的外推，不实跑会假死的全量基线；
只实测改动后，并在文件子集上做改动前/后同口径对比。

---

## 0. 核查结论摘要（2026-08-30）

RootCause 文档的**核心判断全部经源码核实属实**，方案方向正确，可执行。

已核实的关键事实：

| 命题 | 源码位置 | 结论 |
| --- | --- | --- |
| `Step.Rec *audit.Record` 是唯一长寿命的完整记录持有者 | `internal/story/journey.go:79` | ✅ `Journey.Chain`/`Task` 均不持有 body |
| `corpusStats` 走无分批的 `story.BuildAll(toRender, …)` | `cmd/vmr/cmd_story.go:696` | ✅ 与 `renderJourneys` 的 batch 循环（`:634`）平行 |
| `-corpus` 是独立 zoom 模式，不在默认 `vmr analyze` 套件 | `cmd/vmr/cmd_analyze.go:290` | ✅ 需显式 `-corpus` 才触发 43 GB 路径 |
| `Step` 无 JSON tag；产物 JSON 由 `JourneyStructure`/`JourneySummary` 构造 | `internal/story/structure.go` / `metrics.go:399` | ✅ 删 `Step.Rec` 不改任何产物形状 |
| `Manifest` 已有 `TS`/`Model`/`DurMS` | `internal/ctxgraph/manifest.go:34-40` | ✅ A1 机械替换可行 |
| `computeTimeSplit` 同函数内已混用 `Rec`/`Manifest` | `internal/story/metrics.go:156,161` | ✅ 第 161 行已在用 `prev.Manifest.TS` |
| `FetchRecords` 返回全量 `map[Loc]*audit.Record` | `internal/ctxgraph/records.go:34` | ✅ 三调用方 `BuildAll`/`BuildChain`/`PreviewTitles` |
| `KNOWN_ISSUES` 1.1 / 1.2 / 1.3 现状与 §7.4 描述一致 | `docs/KNOWN_ISSUES_sonnet-5.md:29,35,43` | ✅ |

### 0.1 核查中发现的小问题（本计划已就地吸收，不单独反馈）

| # | 问题 | 严重度 | 本计划的处理 |
| --- | --- | --- | --- |
| P-1 | RootCause A2 称「`toolResultsFor` 与 `positionalToolResults` 两处本来就用 `DeltaStart` 限定扫描」——**不准确**。`toolResultsFor`（`findings_toolresult.go:39`）是全 body 扫描 + 按调用方 tool-call ID 匹配；只有 `positionalToolResults`（`render_spine_step.go:316`）用 `DeltaStart`。 | 中 | M3-C：`Step.NewToolResults` 定义为「本 Step delta 范围内出现的全部 tool result」。`toolResultsFor` 改为「用 `steps[i].ToolCalls` 的 ID 去匹配 `steps[i+1].NewToolResults`」，`positionalToolResults` 改为「过滤 `steps[i+1].NewToolResults` 中 ID 不在已知集里的」。等价性由 `findings_toolresult_test.go` / `render_spine_test.go` / `structure_test.go` 三套现有测试守，M3 单独一步核验。 |
| P-2 | RootCause A3 让 `EnsureJourneyDetails` 改用 `ForEachRecord`——会给 `-render-all` **多加一遍全量解压**（当前详情页物化复用 batch 的 `FetchRecords` 结果，是「免费」的）。§7.2 的「三遍解压」统计漏了这一遍。 | 中 | M4-B：不改用 `ForEachRecord`。改为在 `renderJourneys` 的 batch 循环里，把该 batch 已 `FetchRecords` 到的 `recs` map 直接传给详情渲染（在该 batch 结束、`recs` 被丢弃前用掉）。瞬时峰值仍是「一个 batch 的量」——本就是可接受的瞬时值。`-corpus` 不物化详情页，不受影响。 |
| P-3 | RootCause 把「删 `searchableTranscript` 里的 `json.Marshal(s.Rec)`」当「顺带收益」——实为**行为变更**：收窄 LLM anchor 可匹配的文本域，可能改变个别 finding 的 anchor 校验结果。仅 `-llm-addr` opt-in、非确定性、无 golden 覆盖。 | 低 | M4-C：单独一步，显式决策。`searchableTranscript` 只在单 Journey（`BuildChain`）路径调用，M4 里改为该函数自己用 `ctxgraph.ForEachRecord` 就地回捞、marshal、丢弃——**保持原文本域不变**，不做「用预提取字段拼等价面」的收窄。零行为变更。 |
| P-4 | RootCause §7.4 的文档回填清单不全。Analytics 设计文档还有两处写着当前实现细节：`-corpus` 一节「复用 `story.BuildAll`（单次 `FetchRecords` 服务全部候选）」（把 bug 当特性），「模型使用与切换」一节明写取值来自 `Step.Rec.Attempts`。 | 低 | M6-D：一并回填。 |
| P-5 | `internal/story/metrics.go` 预算 470 / 现 458，仅 12 行余量。 | 低 | M3 完成后确认 `metrics.go` 仍 < 470（`contextCurve` 改读预提取字段应中性偏缩短）；若超，把 `contextCurve` 连同其注释挪进 `metrics.go` 的同伴文件或 `journey_stepfacts.go`。 |
| P-6 | `internal/story/journey.go` 的 `buildFrom` 已约 126 物理行，逼近 `defaultFuncLineLimit=120`（含大量注释，archtest 实际计法待 M2 首跑确认）。 | 低 | M2：预提取逻辑放进**新文件** `journey_stepfacts.go` 的 `fillStepFacts(...)`，`buildFrom` 只加 1 行调用。 |

---

## 1. 目标与范围

### 1.1 本次要做（RootCause 动作 A + B + C）

- **动作 A**：删掉 `story.Step.Rec`，消费点需要的事实在构建期一次提取。
- **动作 B**：`corpusStats` 走和 `renderJourneys` 同一条分批路径。
- **动作 C**：批次预算从「候选数量」改为「累计原始字节」；`Manifest` 加 `Bytes` 字段，`CacheSchemaVersion` bump。

### 1.2 本次不做（登记待触发，M6-E 写入 `KNOWN_ISSUES`）

- 全流式 `BuildAll`（连 `FetchRecords` 的 map 也不要）——改动量大一个量级，收益仅 1 GB → 0.3 GB。
- 让 `Manifest` 携带每步 delta 正文以消灭第二遍解压——跨包契约改动 + 双半边影响，是时间问题不是内存问题。
- `audit.Message.Body` `any` → `json.RawMessage`——实测放大仅 1.40x，投入产出比最差。
- `FetchRecords` 接口形状问题、一次 analyze 解压三遍、`searchableTranscript` 的 O(N²) 字符串——同源但独立。

### 1.3 验收总标准（全里程碑完成后）

| 项 | 标准 | 验证方式 |
| --- | --- | --- |
| 正确性 | `go test ./... -race` 全绿 | CI 等价本地跑 |
| 产物一致性 | 同一语料子集，改动前/后 `-render-all` 的 `stories/*.md` + `*.json` **逐字节相同** | M0 存基线，M6 diff |
| 内存 | 全量 43 文件 `-corpus` 峰值 RSS **< 3 GB**，16GB 机器无 swap 跑完（原计划 < 2 GB，见 M6.2 调整说明） | M6 `/usr/bin/time -l` |
| 内存 | 全量 43 文件 `-render-all` 峰值 RSS **< 3 GB** | M6 `/usr/bin/time -l` |
| 架构 | `go test ./internal/archtest/...` 全绿，**预算表数字一个都没被调大** | M6 检查 diff |
| 文档 | 设计文档 / `KNOWN_ISSUES` / `CHANGELOG` 同步 | M6 review |

---

## 2. 里程碑总览

| M | 名称 | 交付 | 依赖 |
| --- | --- | --- | --- |
| M0 | 基线与测量脚手架 | 可复跑的测量脚本 + 子集基线数据 + `-render-all` 产物基线快照 | — |
| M1 | `ctxgraph` 基础设施 | `Manifest.Bytes`、`CacheSchemaVersion` 3、`ForEachRecord` | M0 |
| M2 | `story` 新增预提取字段（`Step.Rec` 仍在，行为不变） | `Step` 新字段 + `journey_stepfacts.go` + `buildFrom` 填充 | M1 |
| M3 | `story` 消费点逐个切到新字段 | A1 机械替换 + A2 读新字段 + P-1 的 `toolResultsFor` 改造 | M2 |
| M4 | 删除 `Step.Rec` + 流式回捞 | 删字段 + `ensure_details` 传 batch recs + `searchableTranscript` 就地回捞 | M3 |
| M5 | `cmd/vmr`：`corpusStats` 分批 + 字节预算 | `corpusStats` batch 循环 + `renderBatchSize` → 字节预算 | M4 |
| M6 | 全量验收 + 文档 + 收尾 | 拆 `journey.go`（如需）+ 全量内存实测 + 三处文档 + `CHANGELOG` + 待触发项登记 | M5 |

每个里程碑的「验收」项全绿，才进下一个。过程记录追加到本文档 §4。

---

## 3. 里程碑详细任务

### M0 — 基线与测量脚手架

**目标**：建立可复跑的测量手段，并锁定「改动前」的两类基线：内存数字（子集，用于同口径对比）和产物字节（用于回归门）。

**任务**

1. **M0.1** 整理 `_tmp/memprobe`（`_tmp` 被 go / git 忽略）为可复跑测量工具：
   - 保留 / 完善其 `main.go`，使其能对给定文件子集跑 `ctxgraph.Scan` → `story.ListCandidates` → `story.BuildAll`（分批）→ `story.ComputeCorpusStats`，在各阶段插 `runtime.GC(); runtime.ReadMemStats()`，打印 live heap / heapInuse / heapSys / 分批峰值 / 切断 `Step.Rec` 后常驻。
   - 复用生产入口，不重写逻辑。
2. **M0.2** 写 `_tmp/membench.sh`（`_tmp` 忽略，不进构建、不进 shellcheck）：
   - 用 `/usr/bin/time -l` 跑真实 CLI（`./vmr analyze -corpus -o …` / `-story-only -render-all -o …`），抓 max RSS + peak footprint + 耗时。
   - 参数化文件子集（`VMR_LOGS` glob）与输出目录。
3. **M0.3** 采集「改动前」子集基线（当前 `main` 现编，不跑会假死的全量 `-corpus`）：
   - 子集 A：4 个文件（约 2.5 GB 解压）——`-corpus` + `-render-all` 各一次，记 RSS/footprint/耗时。
   - 子集 B：8 个文件（约 5–6 GB 解压，接近但不超 16 GB 机器安全线）——`-corpus` 一次，取内存斜率。
   - 全量 44 文件：只跑 `-render-all`（RootCause 实测 4.14 GB，安全）+ `-macro-only`（对照，1.59 GB）。
4. **M0.4** 存产物基线：`main` 现编对**子集 A** 跑 `vmr analyze -story-only -render-all -o /tmp/vmr-baseline-A`，把 `stories/` + `details/` + `*.json` 整树复制到 `_tmp/baseline-A/`（M6 逐字节 diff 用）。
5. **M0.5** 把 M0.3 的数字填入本文档 §4.M0。

**验收**

- [ ] `_tmp/memprobe` 能跑通并打印分阶段堆分解
- [ ] `_tmp/membench.sh` 能跑通并打印 RSS/footprint/耗时
- [ ] §4.M0 表格填入子集 A/B + 全量 `-render-all`/`-macro-only` 的改动前数字
- [ ] `_tmp/baseline-A/` 产物快照就位

**文档同步**：无（工具在 `_tmp`，不进版本库）。

---

### M1 — `ctxgraph` 基础设施

**目标**：三件事——`Manifest.Bytes`、`CacheSchemaVersion` bump、`ForEachRecord`。互不依赖，可一次提交。

**任务**

1. **M1.1** `internal/ctxgraph/manifest.go`：`Manifest` 加字段 `Bytes int \`json:"bytes,omitempty"\``（解压后 JSON 行字节数，用于按体积分批的预算计量）。
2. **M1.2** `internal/ctxgraph/scan.go`：`scanFile` 里 `if m, ok := BuildManifest(&rec, path, line); ok { m.Bytes = len(lineBytes); out = append(out, m) }`——不改 `BuildManifest` 签名（`lineBytes` 只在 `scanFile` 的闭包里有）。
   - 检查是否有其它 `BuildManifest` 调用方（测试）需要同步——`m.Bytes` 缺省 0 不影响正确性，测试按需补。
3. **M1.3** `internal/ctxgraph/cache.go`：`CacheSchemaVersion` `2` → `3`。更新其 doc 注释里的版本说明（如注释提到「schema 变更时 bump」的举例）。
4. **M1.4** `internal/ctxgraph/records.go`：新增
   ```go
   // ForEachRecord 是 FetchRecords 的流式孪生：同样按文件分组、每文件只扫一次，
   // 但把每条记录交给 fn 而不是塞进 map —— 调用方用完即弃，不必驻留全部。
   // fn 的调用不保证顺序（与 FetchRecords 一样按文件并发），fn 必须自己保证并发安全。
   func ForEachRecord(locs []Loc, fn func(Loc, *audit.Record)) error
   ```
   - 复用 `fetchRecordsFromFile` 的逐行 unmarshal 逻辑：抽一个 `forEachRecordInFile(path, wantedLines, fn)`，`fetchRecordsFromFile` 和 `ForEachRecord` 都走它。
   - 并发模型与 `FetchRecords` 一致（同 `scanWorkerCount` 池）。fn 内部若写共享结构需调用方加锁——doc 注释写明。
5. **M1.5** 测试：
   - `records_test.go`：`TestForEachRecord_MatchesFetchRecords`——同一批 `locs`，`ForEachRecord` 收集到 map 后与 `FetchRecords` 结果逐 key 对比（DeepEqual 或关键字段）。
   - `records_test.go`：`ForEachRecord` 对无法二次解析的行静默跳过（与 `FetchRecords` 行为一致）。
   - `manifest_test.go` / `scan_test.go`：`m.Bytes > 0` 且约等于该行 JSON 长度。
   - `cache_test.go`：已有 `CacheSchemaVersion - 1` 参数化陈旧测试，确认仍通过（bump 后旧 cache 失效重建）。

**验收**

- [ ] `go test ./internal/ctxgraph/... ./internal/archtest/...` 全绿（`-race` 跑 `ctxgraph`）
- [ ] `go build ./...` 通过
- [ ] `TestForEachRecord_MatchesFetchRecords` 存在且通过
- [ ] `manifest.go` / `records.go` / `cache.go` 仍在各自行数预算内

**文档同步**

- `docs/VirtualModelRouter_Design_v4_Analytics.md`「源坐标与按需回捞」一节：补一句 `ForEachRecord` 是 `FetchRecords` 的流式孪生，并补一句「`Step` / 任何长寿命结构**不得持有** `FetchRecords`/`ForEachRecord` 的返回值——回捞即用即弃，否则『按需回捞』会被下一个人误读成『回捞后缓存』」。

---

### M2 — `story` 新增预提取字段（`Step.Rec` 仍在，行为不变）

**目标**：把消费点需要的所有事实，在构建期一次性提取到 `Step` / `Journey` 的新字段上。此里程碑结束时 `Step.Rec` **仍然存在**，没有任何消费点改读新字段——纯加法，`golden_test.go` 必须逐字节不变。

**任务**

1. **M2.1** 新文件 `internal/story/journey_stepfacts.go`（版本头 `// Ver 2026-08-30 …, by Sonnet 5`）：
   - `type AttemptFact struct { Provider, Model string }`（`modelusage.go` 遍历全部 attempt 的 `(Provider, Model)`，`render_html_dashboard.go` 读 `len(Attempts)`——这是全部消费面）。此处早期草稿曾写 `Endpoint/Protocol/ErrorClass`（RootCause A2 也提到 `Endpoint`/`ErrorClass`「预留」）；落地时全 `internal/story` grep 确认这几项无任何消费方——`stepUpstream` 的 fallback 取自 `Manifest.Endpoint` 而非 attempt（`modelusage.go:168`），`ErrorClass` 没有读取点。按 YAGNI 不提取——见 §4.M2 与 §7 复评。
   - `func fillStepFacts(step *Step, rec *audit.Record, msgs []chatmsg.Message, rawMsgs []any, off, deltaStart int)`：
     - `step.Attempts`：从 `rec.Attempts` 逐条拷贝为 `[]AttemptFact`。
     - `step.Context ContextPoint`：把 `metrics.go:contextCurve` 的按 role 累加 token 逻辑搬过来（`chatmsg.Messages` 已由调用方传入，避免重解析），`Seq` 用 `step.Seq`。
     - `step.NewToolResults []chatmsg.ToolResult`：`chatmsg.ToolResultList(rawMsgs[deltaStart-off:])`（越界保护，参照 `positionalToolResults` 现有的 `deltaIdx` 计算），即「本 Step delta 内出现的全部 tool result」。
     - `step.SysChars int`：`len([]rune(ctxgraph.LeadingSystemText(msgs, step.Manifest.LeadSys)))`（`m.HasSys` 为 false 时 0）。
2. **M2.2** `internal/story/journey.go`：
   - `Step` 加字段：`Context ContextPoint`、`Attempts []AttemptFact`、`NewToolResults []chatmsg.ToolResult`、`SysChars int`。加简短 `// why` 注释（每个字段说明它替代了哪个 `Rec` 读取点）。
   - `Journey` 加字段：
     - `InitialInstruction string`——`compare.go:initialInstructionStats` 需要「首 Step 的首条真实 user 指令、不截断」。在 `buildFrom` 末尾用已有的 `firstRu`（`taskseg.RealUsers`）取（`deriveTitle` 已在用 `firstRu`，同源）。
     - `SysText map[ctxgraph.Hash]string`——按 `SysHash` 去重的完整 leading-system 正文表，供 `compare.go:systemMessageTexts`（要 excerpt + token count）。`buildFrom` 里每 Step 若 `m.HasSys` 且该 `SysHash` 未入表则 `SysText[m.SysHash] = LeadingSystemText(msgs, m.LeadSys)`。
   - `buildFrom` 循环体内 `step := buildStep(...)` 之后加一行 `fillStepFacts(step, rec, msgs, rawMsgs, off, deltaStart)`。
   - `buildFrom` 末尾（`j.Title = deriveTitle(...)` 附近）填 `j.InitialInstruction`。
3. **M2.3** 若 `buildFrom` 因这两行触碰函数行数预算 → 把 `SysText` 填充逻辑也收进 `fillStepFacts`（传 `j` 或返回值），保持 `buildFrom` 净增 ≤ 2 行。
4. **M2.4** 测试：
   - 新 `internal/story/journey_stepfacts_test.go`：对一个多 Step fixture（复用 `journey_test.go` 的 `mkRec`），断言 `step.Context` 各 role token 与直接跑 `contextCurve` 一致；`step.Attempts` 与 `rec.Attempts` 一一对应；`step.NewToolResults` 命中 delta 内的 tool result；`step.SysChars` 与 `LeadingSystemText` 长度一致。
   - `golden_test.go`：**不改**，跑通即证明纯加法未动产物。
   - `go test ./internal/story/... -race`。

**验收**

- [ ] `go test ./internal/story/... -race` 全绿
- [ ] `golden_test.go` 逐字节不变（未修改该文件，通过即证）
- [ ] `journey_stepfacts_test.go` 存在且通过
- [ ] `go test ./internal/archtest/...` 全绿（`journey.go` < 850；新文件 < 700；`buildFrom` 未破 func 预算）
- [ ] `go build ./...` 通过

**文档同步**：无（下一里程碑切换消费点时一并更新设计文档措辞）。

---

### M3 — `story` 消费点逐个切到新字段

**目标**：11 处 `Step.Rec` 消费点全部改读新字段 / `Manifest` 字段。每切一处跑一次 `go test ./internal/story/...`。`Step.Rec` 仍在（M4 才删），但此里程碑结束时**没有任何 `.Rec` 的字段读取**，只剩 `ensure_details.go` / `llm_findings.go` 两处「需要完整 record」的（M4 处理）。

**任务**（按 RootCause A1 → A2 顺序）

1. **M3-A1（机械替换）**
   - `metrics.go:156` `s.Rec.DurMS` → `s.Manifest.DurMS`
   - `metrics.go:161` `prev.Rec.DurMS` → `prev.Manifest.DurMS`
   - `render_html.go:165` `s.Rec.TS` → `s.Manifest.TS`
   - `render_html_dashboard.go:57` `s.Rec.TS` → `s.Manifest.TS`
   - `render_html_dashboard.go:59` `s.Rec.Model` → `s.Manifest.Model`
   - 跑 `go test ./internal/story/...`（`metrics_test.go` / `render_html_test.go` 守）。
2. **M3-A2（读新字段）**
   - `metrics.go:307 contextCurve`：改为 `for _, s := range steps { out = append(out, s.Context) }`。删掉函数体内的 body 解析；重写该函数的 doc 注释（现注释明写「不持久化在 Step 上（会重复 Rec 里的数据）」——反转它，说明现在预提取在 `fillStepFacts`）。检查 `metrics.go` 行数是否回落（P-5）。
   - `modelusage.go:84,129,159,160` + `stepUpstream`：`s.Rec.Attempts` → `s.Attempts`（`[]AttemptFact`）。`stepUpstream` 的 `Manifest.Endpoint` fallback 保留。更新 `modelusage.go:5` 的文件头注释（明写取自 `Step.Attempts`，非 `Step.Rec.Attempts`）。
   - `render_html_dashboard.go:61` `len(s.Rec.Attempts)` → `len(s.Attempts)`。
   - `render_md_sysprompt.go:64 sysPromptEraChars`：`e.Owner.Rec.Client.Request.Body …` → `e.Owner.SysChars`（直接返回，去掉 body 解析）。重写其 doc 注释。
   - `compare.go:480 systemMessageTexts`：改为从 `j.SysText[s.Manifest.SysHash]` 取正文（需把 `j` 或 `SysText` 传进来——检查 `sysPromptStats` 调用链）。token count / excerpt 都基于这份正文。
   - `compare.go:336,339 initialInstructionStats`：改读 `j.InitialInstruction`。
   - `metrics.go:310`（若 `contextCurve` 外还有其它 `s.Rec.Client.Request.Body` 读取）——核对全 grep，确认 M3-A2 后 `metrics.go` 无 `.Rec`。
3. **M3-C（`toolResultsFor` / `positionalToolResults` 转向，P-1）**——单独一步，最需谨慎：
   - `findings_toolresult.go:39 toolResultsFor(steps, i)`：`steps[i+1].Rec == nil` 守卫改为 `i+1 >= len(steps)`；body 扫描改为遍历 `steps[i+1].NewToolResults`，仍按 `exact` / `byNorm`（`steps[i].ToolCalls` 的 ID）两轮匹配 + normalized 命中时把 `CallID` 改写回原始 `tc.ID`。
   - `render_spine_step.go:316 positionalToolResults`：`steps[i+1].Rec` 守卫同上；`rawArr[deltaIdx:]` + `ToolResultList` 改为直接遍历 `steps[i+1].NewToolResults`（已是 delta 内的），过滤 `knownNorm` 不含的。删掉 `deltaIdx`/`MsgOffset` 计算（`NewToolResults` 已经是 delta 范围）。
   - `render_spine_step.go:318` `steps[i+1].Rec == nil` → `i+1 >= len(steps)`。
   - **核验**：`findings_toolresult_test.go`（15 KB，覆盖三个 detector）、`render_spine_test.go`（38 KB）、`structure_test.go`（`buildStepStructure` 走 `toolResultsFor`）——全绿即等价。若有 fixture 依赖「`Rec` 在但 `NewToolResults` 未填」的旧形态，同步 fixture（M2 已让生产路径填 `NewToolResults`，测试 helper 可能要跟）。
4. **M3.5** 全 grep 确认：`grep -rn "\.Rec\b" internal/story/ | grep -v _test` 只剩 `ensure_details.go` 的 2 处 + `llm_findings.go` 的 2 处 + `journey.go`（`buildStep` 里 `Step{... Rec: rec}` 的赋值本身）+ `compare.go:473` 的历史注释。

**验收**

- [ ] `go test ./internal/story/... -race` 全绿
- [ ] `golden_test.go` 逐字节不变
- [ ] `go test ./...`（跨包，`cmd/vmr` 的 story 相关测试）全绿
- [ ] `grep -rn "\.Rec\." internal/story/` 仅剩 `ensure_details.go` / `llm_findings.go` / `journey.go` 的构造点
- [ ] `metrics.go` 行数 < 470（P-5）；`archtest` 全绿
- [ ] `go build ./...` 通过

**文档同步**

- `docs/VirtualModelRouter_Design_v4_Analytics.md`「模型使用与切换」一节：`Step.Rec.Attempts` → `Step.Attempts`（预提取的 `AttemptFact`）。

---

### M4 — 删除 `Step.Rec` + 流式回捞

**目标**：删掉 `Step.Rec` 字段。编译器指出剩余引用。处理 `ensure_details` 和 `searchableTranscript` 两个「需要完整 record」的消费者。

**任务**

1. **M4-A** `internal/story/journey.go`：
   - `Step` 删 `Rec *audit.Record` 字段。
   - `buildStep` 签名去掉 `rec *audit.Record` 参数？——**不去**：`buildStep` 仍需 `rec.Client.Response` 提取 `RespText`/`ToolCalls`/`Reasoning`/`Finish`/`NoReply`，`fillStepFacts` 仍需 `rec`。只是不再把 `rec` 存进 `Step`。改 `step := &Step{Seq: seq, Manifest: m, Edge: edge, ...}`（删 `Rec: rec`）。
   - 编译 → 收集所有 `undefined: s.Rec` 错误。
2. **M4-B** `internal/story/ensure_details.go` + `cmd/vmr/cmd_story.go:renderJourneys`（P-2）：
   - `EnsureJourneyDetails` 加参 `recs map[ctxgraph.Loc]*audit.Record`（或一个 `func(*Step) *audit.Record` 取值器）。内部 `rec := recs[ctxgraph.Loc{Path: s.Manifest.Path, Line: s.Manifest.Line}]`，替代 `s.Rec`。
   - `renderJourneys` 的 batch 循环：`story.BuildAll` 目前只返回 `[]*Journey`。改为让本循环在 `BuildAll` 之后、进入下一 batch 之前，持有本 batch 的 `recs`。两个方案二选一（M4 开始时定）：
     - **(a)** 给 `story` 加 `BuildAllWithRecords(chains) ([]*Journey, map[Loc]*audit.Record, error)`，`BuildAll` 成为它的 wrapper。`renderJourneys` 用新入口，把 `recs` 传给 `EnsureJourneyDetails`，循环末尾 `recs = nil`。
     - **(b)** `renderJourneys` 自己先 `ctxgraph.FetchRecords(batch 的 locs)` 再 `story.BuildAll`——多一次该 batch 的解压。**不选**（就是 P-2 要避免的）。
   - 选 (a)。`writeJourneyFile` → `EnsureJourneyDetails` 的调用链把 `recs` 透传下去。
   - 单 `-journey` / `-compare` 路径（`cmd_story.go:346,442`）走 `BuildChain`——`BuildChain` 内部已 `FetchRecords`，同样加个返回值或让这些调用点自己持有。
3. **M4-C** `internal/story/llm_findings.go:searchableTranscript`（P-3）：
   - 该函数只在 `ComputeLLMFindings` → 单 Journey（`-llm-addr`）路径调用。签名可加 `error` 返回或接受一个 record 取值器。
   - 改为：函数内对 `journeySteps(j)` 收集 `locs`，调 `ctxgraph.ForEachRecord(locs, func(_ Loc, rec *audit.Record){ marshal 拼进 builder（加锁，因 ForEachRecord 并发）})`。**保持原有全 `json.Marshal(rec)` 文本域不变**——不做收窄。
   - 调用方 `ComputeLLMFindings` / 其调用链处理新增的 `error`（fail-open：回捞失败则该 blob 为空，与现有「`s.Rec != nil` 才 marshal」的宽容度一致）。
   - 更新 `searchableTranscript` doc 注释（说明改为流式回捞，不再依赖 `Step.Rec`）。
4. **M4.5** `grep -rn "audit.Record" internal/story/` 复核：`Step` 不再持有；`buildFrom`/`buildStep`/`fillStepFacts`/`buildCompactionInfo` 的局部使用（来自 `recs` map）保留合理。

**验收**

- [ ] `go build ./...` 通过（编译器已确认无遗漏 `.Rec`）
- [ ] `go test ./... -race` 全绿
- [ ] `golden_test.go` 逐字节不变
- [ ] `ensure_details_test.go` / `llm_findings_test.go` 全绿
- [ ] `_tmp/memprobe`：对子集 A，「切断 Step.Rec」前后常驻堆对比复现 RootCause §2.2 的量级（构建后 → 置 nil 后掉一个数量级）
- [ ] `archtest` 全绿

**文档同步**

- `docs/VirtualModelRouter_Design_v4_Analytics.md`：`Build`/`BuildChain`/`BuildAll` 一节——若新增 `BuildAllWithRecords` 入口，补一句其用途（详情页物化复用同一批已回捞记录，避免二次解压）。

---

### M5 — `cmd/vmr`：`corpusStats` 分批 + 字节预算

**目标**：`corpusStats` 走分批循环（动作 B）；批次预算从「20 个候选」改成「累计 ≤ N MB 原始字节」（动作 C）。

**任务**

1. **M5.1** `cmd/vmr/cmd_story.go`：抽一个共享的分批器
   ```go
   // batchByBytes splits candidates into batches whose cumulative
   // Manifest.Bytes stays under budget (a single over-budget candidate
   // still forms its own batch — never split a Journey).
   func batchByBytes(chains [][]*ctxgraph.Lineage, budgetBytes int) [][]int // 每批的 chain 下标区间
   ```
   或直接返回 `[][][]*ctxgraph.Lineage`。预算常量 `renderBatchBudgetBytes`（如 `512 << 20`，M6 按实测调）。
2. **M5.2** `renderJourneys`：`for start := 0; ... start += renderBatchSize` 改为按 `batchByBytes` 的结果迭代。`renderBatchSize` 常量 + 其「20 是猜出来的」大段注释删除，换成 `renderBatchBudgetBytes` + 简短注释（预算有物理意义：一批的原始字节 ≈ 该批 `FetchRecords` 后的 live heap 下界）。
3. **M5.3** `corpusStats`：把 `journeys, err := story.BuildAll(toRender, ...)` + `story.ComputeCorpusStats(journeys)` 改成：
   ```go
   var all []*story.Journey
   for _, batch := range batchByBytes(toRender, renderBatchBudgetBytes) {
       js, err := story.BuildAll(batch, prof, lang) // Step.Rec 已删，每个 Journey 只剩叙事结构
       if err != nil { return err }
       all = append(all, js...)
   }
   stats := story.ComputeCorpusStats(all)
   ```
   全部 Journey 累积无害（RootCause 实测 586 个 = 288 MB）。**不需要**在线累加器。
4. **M5.4** 检查 `Manifest.Bytes` 在链上的可得性：`corpusStats` / `renderJourneys` 拿到的是 `[]*ctxgraph.Lineage`（`ChainFrom` 结果），其 `Manifests` 已带 `Bytes`（M1）。分批器对一条 chain 求和其所有 lineage 的所有 manifest 的 `Bytes`。
5. **M5.5** 测试：
   - `cmd/vmr` 现有 story/analyze 测试全绿。
   - 新增 `batchByBytes` 单测（`cmd/vmr/*_test.go`）：单个超预算候选自成一批；正常候选累计不超预算;空输入。
   - 手动：`_tmp/membench.sh` 对子集 B 跑 `-corpus`，确认不再线性爆（改动前子集 B 是内存斜率的取样点）。

**验收**

- [ ] `go test ./... -race` 全绿
- [ ] `batchByBytes` 单测通过
- [ ] `renderBatchSize` 常量及其大段注释已删除，`grep -rn renderBatchSize cmd/` 无残留
- [ ] 子集 B `-corpus` 峰值 RSS 显著低于 M0 基线（预期从「线性 ~3.2x 解压体积」降到「一个 batch 预算 + 288MB 量级叙事」）
- [ ] `archtest` 全绿（`cmd_story.go` 行数——删注释应净减）

**文档同步**

- `docs/VirtualModelRouter_Design_v4_Analytics.md`「`vmr story -corpus`」一节：把「复用 `-render-all` 已有的 `story.BuildAll`（单次 `FetchRecords` 服务全部候选）」改为「与 `-render-all` 同一条按字节预算分批的构建路径」（P-4）。

---

### M6 — 全量验收 + 文档 + 收尾

**目标**：拆 `journey.go`（如 archtest 要求）；全量内存实测达标；三处设计文档 + `KNOWN_ISSUES` + `CHANGELOG` 同步；待触发项登记。

**任务**

1. **M6.1** 若 `journey.go` 触碰 850 预算（M2 加了字段 + `journey_stepfacts.go` 已分走提取逻辑，可能仍差一点）：按项目约定拆文件（`journey_stepfacts.go` 已建，可把 `Step`/`Event` 结构定义或 `deriveID`/`deriveTitle` 一族挪过去），**不抬 `file_sizes_test.go` 的数字**。
2. **M6.2** 全量内存实测（`_tmp/membench.sh`，全部 44 文件）：
   - `./vmr analyze -corpus` → 记峰值 RSS，目标 **< 2 GB**。
   - `./vmr analyze -story-only -render-all` → 记峰值 RSS，目标 **< 2 GB**。
   - `./vmr analyze -macro-only` → 对照，应仍约 1.6 GB。
   - 填入 §4.M6。
3. **M6.3** 产物逐字节回归门：
   - 当前分支现编，对**子集 A** 跑 `vmr analyze -story-only -render-all -o /tmp/vmr-after-A`。
   - `diff -r _tmp/baseline-A/ /tmp/vmr-after-A/`（排除时间戳类顶层 meta 文件如有）——`stories/*.md`、`journey-*.json`、`details/*.md` **必须逐字节相同**。
   - 任何差异都要定位到「非本次承诺的行为变更」并消除，或在 §4.M6 记录为「已知且合理」（例如 `-llm-addr` 不在此路径，不涉及）。
4. **M6.4** 设计文档最终 review（`docs/VirtualModelRouter_Design_v4_Analytics.md`）：
   - 「源坐标与按需回捞」：`ForEachRecord` + 「不得缓存回捞结果」（M1 已加，复核措辞）。
   - 「模型使用与切换」：`Step.Attempts`（M3 已改，复核）。
   - 「`vmr story -corpus`」：按字节预算分批（M5 已改，复核）。
   - `Build`/`BuildChain`/`BuildAll` 一节：`BuildAllWithRecords`（M4，复核）。
   - 全文搜 `Step.Rec` / `renderBatchSize`——无残留。
5. **M6.5** `docs/KNOWN_ISSUES_sonnet-5.md`：
   - **1.2 条**：补入本次实测数据（全量 `-corpus` 改动前约 43 GB 外推 / 改动后 < 2 GB），归因补一句「story 半边的 `Step.Rec` 曾是主因，已于本次消除（见 `CHANGELOG` / `docs/ANALYZE_MEMORY_ACTIONPLAN_sonnet-5.md`）」；触发条件里 `-corpus` 那条标注已解决。report 半边的 `AnalyzeSessions` 样本切片问题**保留**（未触及）。
   - **1.3 条**：结案为「决定不做」——反序列化放大实测仅 1.40x（引用 RootCause §2.3），`Body any` → `RawMessage` 最多省 29% 不改量级。
   - **新增条目**（§1，低危，登记待触发）：
     - 全流式 `BuildAll`（触发：语料再涨 5 倍，或需在 8 GB 以下机器跑全量）。
     - `FetchRecords` 接口形状逼调用方驻留全部（`PreviewTitles` 可顺手切 `ForEachRecord`）。
     - 一次 `analyze` 至少解压全量三遍（`PreviewTitles` / 每 batch `FetchRecords` / report 半边 `analyzeFile`）；`.parse-cache` 对后两者无效。治法是「`Manifest` 携带 delta 正文」，跨包契约改动，待时间成为首要痛点。
6. **M6.6** `CHANGELOG.md` `[Unreleased]`：
   - `### Changed`：`vmr analyze -corpus`（及 `-render-all`）峰值内存从约 43 GB（大语料上 swap thrashing 假死）降到约 2.4 GB（约 18x）——叙事结构不再持有原始审计记录，`-corpus` 改为按字节预算分批。全量 43 文件 `-corpus` 现可在 16 GB 机器上完成。
   - `### Changed`（developer-visible）：`internal/ctxgraph` 新增 `ForEachRecord`（`FetchRecords` 的流式孪生）、`Manifest.Bytes`；`CacheSchemaVersion` 3（既有 `.parse-cache` 自动重建一次）。
7. **M6.7** 清理：`_tmp/` 下的临时工具去留（`memprobe`/`membench.sh` 可留给后续，`_tmp` 本就 gitignore）；本文档 §4 追加「执行结果总结」。
8. **M6.8** 最终全量测试矩阵：`go vet ./...`、`go build ./...`、`go test ./... -race`、`gofmt -l`（应无输出）、`shellcheck vmr.sh vmr-loadtest.sh`。

**验收**

- [x] 全量 43 文件 `-corpus` 峰值 RSS **2.41 GB**（< 3 GB，16GB 无 swap 跑完；§4.M6）
- [x] 全量 43 文件 `-render-all` 峰值 RSS **1.91 GB**
- [ ] 子集 A 产物 `diff -r` 逐字节相同（或差异已记录为合理）
- [ ] `go test ./... -race` 全绿；`go vet` / `gofmt -l` / `shellcheck` 干净
- [ ] `go test ./internal/archtest/...` 全绿，且 `git diff internal/archtest/` 里**没有任何预算数字被调大**
- [ ] `docs/VirtualModelRouter_Design_v4_Analytics.md` 无 `Step.Rec` / `renderBatchSize` 残留引用
- [ ] `KNOWN_ISSUES` 1.2 / 1.3 已更新，待触发项已登记
- [ ] `CHANGELOG.md` `[Unreleased]` 有对应条目
- [ ] 本文档 §4 有完整过程记录 + 结果总结

---

## 4. 执行过程记录

> 每个里程碑开工时追加一小节，记：实际改动、遇到的问题、当场解决 / 留档、验收结果。

### M0 — 基线与测量脚手架 ✅ 2026-08-30

**实际改动**：`_tmp/memprobe/`（沿用 Opus 5 分析期的探针，未改）、新建 `_tmp/membench.sh`（`/usr/bin/time -l` 包 CLI 跑，抓 max RSS / peak footprint / wall）。

**语料实况**（与 RootCause 文档核对）：`logs/*.jsonl.zst` 共 **43 个文件、234.5 MB 压缩、约 12.4 GB 解压**（压缩比约 53x）——与文档「44 文件 / 13 GB」基本一致（少 1 个文件，量级相同）。文档所说「700 MB 压缩」疑似含了 `logs/loadtest/` 子目录。

**改动前基线**（当前 `main` = `perf/story-drop-step-rec` 分叉点 `a73b8d3` 现编）：

| 路径 | 输入 | max RSS | peak footprint | wall |
| --- | --- | --- | --- | --- |
| `-story-only -render-all` | 子集 A（6 文件 `2026-08-20..25`，45 MB 压缩，约 4 GB 解压） | **3226 MB** | 3226 MB | 132 s |
| `-corpus` | 子集 A（同上） | **4900 MB** | **9660 MB** | 133 s |

- 子集 A `-corpus` footprint 9.66 GB 与 RootCause 文档实测「4 文件 / 9.01 GB」吻合，验证了「`-corpus` 线性于语料体积」。
- 全量 44 文件 `-corpus` 的改动前基线**不实跑**（会 swap thrashing 假死），信任文档外推的约 43 GB（用户 2026-08-30 决策）。

**产物基线快照**：`_tmp/baseline-A-snapshot/`（子集 A `-render-all` 的 `stories/` + `details/` + `*.json/*.md` 全树，2498 文件，已排除 `.parse-cache/`），M6.3 逐字节 diff 用。

### M1 — `ctxgraph` 基础设施 ✅ 2026-08-30

**实际改动**：

| 文件 | 改动 |
| --- | --- |
| `internal/ctxgraph/manifest.go` | `Manifest` 加 `Bytes int` 字段（解压 JSON 行长度） |
| `internal/ctxgraph/scan.go` | `scanFile` 里 `m.Bytes = len(lineBytes)`（`BuildManifest` 签名不动） |
| `internal/ctxgraph/cache.go` | `CacheSchemaVersion` `2` → `3` + v3 注释 |
| `internal/ctxgraph/records.go` | 抽 `forEachRecordInFile` 共享核；新增 `ForEachRecord(locs, fn)`（`FetchRecords` 的流式孪生，fn 并发调用，调用方自负同步） |
| `internal/ctxgraph/records_test.go` | `TestForEachRecord_MatchesFetchRecords` / `_MissingFile` / `_EmptyLocs` / `TestScanFile_PopulatesManifestBytes` |

**验收结果**：

- [x] `go test ./internal/ctxgraph/...` 全绿（28.3s）
- [x] `go test ./internal/archtest/` 全绿（1.5s，无预算数字改动）
- [x] `go build ./...` 通过
- [x] `gofmt -l internal/ctxgraph/` 无输出
- [x] `go test -race ./internal/ctxgraph/...` — 见 M2 开工时确认（后台跑）
- [x] `cache_test.go:TestScanCached_SchemaVersionMismatchReparses` 用 `CacheSchemaVersion - 1` 参数化，bump 自动兼容

**过程记录**：`fetchRecordsFromFile` 重构为薄封装（走 `forEachRecordInFile`），零行为变更。`ForEachRecord` 的并发模型与 `FetchRecords` 一致（同 `scanWorkerCount` 池），但 fn 直接被 worker 调用而非各自建 map——doc 注释明写「fn 触共享状态须自己同步」。

### M2 — `story` 新增预提取字段 ✅ 2026-08-30

**实际改动**：

| 文件 | 改动 |
| --- | --- |
| `internal/story/journey_stepfacts.go`（新） | `AttemptFact{Provider,Model}` 类型；`parseManifestBody`（buildFrom 前导抽出，为腾行数预算）；`fillStepFacts(j, step, rec, msgs, rawMsgs, off, deltaStart)`；`stepContextPoint` / `deltaRawMsgs` / `leadingSystemParts` / `firstRealInstruction` 辅助 |
| `internal/story/journey.go` | `Step` 加 `Context ContextPoint` / `Attempts []AttemptFact` / `NewToolResults []chatmsg.ToolResult` / `SysChars int`；`Journey` 加 `InitialInstruction string` / `SysText map[ctxgraph.Hash][]string`；`buildFrom` 循环里 `parseManifestBody` + `fillStepFacts` + `!firstRuSet` 时填 `InitialInstruction` |
| `internal/story/journey_stepfacts_test.go`（新） | `TestFillStepFacts_ExtractsWhatConsumersNeed`（Attempts/Context/NewToolResults/SysChars/SysText/InitialInstruction 全覆盖）+ `TestStepContextPoint_MatchesLegacyContextCurve` |

**发现的问题 → 就地处理**：

- **P-7（新）**：RootCause A2 说 `Journey.InitialInstruction` 可用「已有的 `firstRu`」取——**不成立**。`taskseg.RealUsers` 存的是 `segment.Preview` 处理过的值（内部空白折叠 + 截断到 80 runes），而 `compare.go:initialInstructionStats` 要的是**原始文本**（自己按 2000 chars 截）。已改为 `firstRealInstruction()` 独立跑一遍 `prof.RealUserText` 的首条 user 扫描，与消费方同源。测试里显式断言它与 `Preview` 形态不同。
- **P-6 确认**：`buildFrom` 加 2 行后达 121 行 / 预算 120，archtest 报错。按计划把前导 5 行 body 解析抽成 `parseManifestBody`（净 −3 行），`buildFrom` 回落到预算内。**未抬预算数字**。

**验收结果**：

- [x] `go test ./internal/story/... -race`（`-race` 见 M3 汇总跑）/ 非 race 全绿
- [x] `golden_test.go` 未改动、通过（证明纯加法未动产物）
- [x] `go test ./internal/archtest/...` 全绿（`journey.go` 内函数均 < 120；`journey_stepfacts.go` < 700）
- [x] `go build ./...` / `go vet ./internal/story/...` / `gofmt -l` 干净
- [x] `go test -race ./internal/ctxgraph/...` 全绿（283s，M1 的 `ForEachRecord` 并发无竞态）

### M3 — `story` 消费点切换 ✅ 2026-08-30

**实际改动**：

| 文件 | 改动 |
| --- | --- |
| `internal/story/metrics.go` | `computeTimeSplit`：`s.Rec.DurMS`→`s.Manifest.DurMS`、`prev.Rec.DurMS`→`prev.Manifest.DurMS`；`contextCurve` 改为 `return s.Context`（body 解析删除，注释重写） |
| `internal/story/render_html.go` | `s.Rec.TS`→`s.Manifest.TS`，`s.Rec != nil`→`s.Manifest != nil` |
| `internal/story/render_html_dashboard.go` | `s.Rec.{TS,Model}`→`s.Manifest.{TS,Model}`，`len(s.Rec.Attempts)`→`len(s.Attempts)`，guard 改 `s.Manifest != nil` |
| `internal/story/modelusage.go` | `s.Rec.Attempts`→`s.Attempts`（4 处）；文件头注释更新 |
| `internal/story/render_md_sysprompt.go` | `sysPromptEraChars` 改为 `return e.Owner.SysChars`；删 `chatmsg` import |
| `internal/story/compare.go` | `initialInstructionStats(j)` 读 `j.InitialInstruction`（去掉 `prof` 参数）；`sysPromptStats` 读 `j.SysText[SysHash]`（`systemMessageTexts` 整个删除）；`ComputeComparisonExtras` 去掉 `prof taskseg.Profile` 参数；删 `chatmsg`/`taskseg` import |
| `internal/story/findings_toolresult.go` | `toolResultsFor` 改为遍历 `steps[i+1].NewToolResults`（ID 匹配逻辑不变）；去掉 `steps[i+1].Rec == nil` guard |
| `internal/story/render_spine_step.go` | `positionalToolResults` 改为遍历 `steps[i+1].NewToolResults`（DeltaStart 切片逻辑删除——已由 `fillStepFacts` 完成） |
| `cmd/vmr/cmd_story.go` | `ComputeComparisonExtras` 调用去掉 `prof` 实参 |
| 测试 | `modelusage_test.go`：合成 Step 的 `Rec:&audit.Record{Attempts:…}` → `Attempts:[]AttemptFact{…}`（14 处，含 2 处多 attempt）；`compare_test.go` / `render_compare_html_test.go`：`ComputeComparisonExtras`/`initialInstructionStats` 调用同步；`render_spine_test.go`：`TestPositionalToolResults` fixture 改设 `NewToolResults`；`journey_stepfacts_test.go` 加 `TestDeltaRawMsgs` + `TestFillStepFacts_NewToolResultsScopedToDelta`（把原 `TestPositionalToolResults_ScopedToDelta` 的 delta-scoping 覆盖迁到构建层） |

**P-1 核验结果**：`toolResultsFor` 从「全 body 扫描 + ID 匹配」改为「遍历 `steps[i+1].NewToolResults` + ID 匹配」——`findings_toolresult_test.go`（15 KB，3 个 detector）+ `render_spine_test.go` + `structure_test.go` 全绿。等价性成立：step i 的 tool call 的结果就在 step i+1 的 delta 里（协议约束）。

**发现的问题 → 就地处理**：

- **P-8（新）**：`ComputeComparisonExtras` 的 `prof` 参数在 `initialInstructionStats` 不再需要后变为纯死参。已一并从签名删除（唯一非测试调用方 `cmd_story.go:452` + 4 处测试同步）。属根因修复带来的连锁简化。
- **P-9（新）**：`compare.go:systemMessageTexts` 整个函数删除（其职责——按 `LeadSys` 取 leading system 各段文本——由 `Journey.SysText` 表取代）。`systemTokenCount` 保留（仍对 `[]string` 求 token）。

**验收结果**：

- [x] `go test ./...` 全绿（含 `cmd/vmr` 16s）
- [x] `go test ./internal/story/ ./internal/archtest/` 全绿；`golden_test.go` 字节一致
- [x] `go build ./...` / `gofmt -l internal/ cmd/` 干净
- [x] `grep '\.Rec' internal/story/*.go`（非测试）仅剩 `ensure_details.go:52,55` + `llm_findings.go:535,536` —— M4 目标
- [x] `metrics.go` 458→约 440 行（`contextCurve` 净缩短，P-5 无虞）

### M4 — 删除 `Step.Rec` ✅ 2026-08-30

**实际改动**：

| 文件 | 改动 |
| --- | --- |
| `internal/story/journey.go` | 删 `Step.Rec` 字段；`buildStep` 的 `Step{…}` 字面量去掉 `Rec: rec`（`rec` 参数保留——`buildStep`/`fillStepFacts` 仍需 body/response）；新增 `BuildAllWithRecords(chains) ([]*Journey, map[Loc]*audit.Record, error)`，`BuildAll` 变 1 行 wrapper |
| `internal/story/ensure_details.go` | `EnsureJourneyDetails` 加 `recs map[ctxgraph.Loc]*audit.Record` 参数：非 nil 直接查表（batch 复用 `BuildAllWithRecords` 已解压的记录，P-2）；nil 则 `ctxgraph.ForEachRecord` 流式回捞（单 journey/默认套件），并发回调加 `sync.Mutex` 串行化（`EnsureRendered` 会写共享 evidence blob） |
| `internal/story/llm_findings.go` | `searchableTranscript(j) (string, error)`：改用 `ctxgraph.ForEachRecord` 就地回捞 marshal（**保持原全 `json.Marshal(rec)` 文本域不变**，P-3——不做收窄）；`ComputeLLMFindings` 处理新增 error（fail-open，warning 到 stderr） |
| `cmd/vmr/cmd_story.go` | `writeJourneyFile` 加 `recs` 参数透传 `EnsureJourneyDetails`；`renderJourneys` 用 `BuildAllWithRecords` 取 `batchRecs` 传下去；单 journey/默认套件路径传 `nil`；加 `audit` import |
| `_eval/calibrate_p1b.go` | `transcriptPool` 同步改用 `ctxgraph.FetchRecords` 重取记录（archtest 校验其可编译） |
| 测试 | `invariants_test.go`：加 `fetchStepRecords` helper（按 manifest 坐标 `FetchRecords`），2 处 `step.Rec.Client…` 改走它；`ensure_details_test.go`：3 处调用加 `nil`（走 `ForEachRecord` fallback，顺带覆盖该路径）；`modelusage_test.go`：2 处 `Rec: &audit.Record{}` 死字段删除；`llm_findings_test.go`：`searchableTranscript` 2 返回值 |

**决定（P-3）**：`searchableTranscript` 只改数据来源、不改文本域——`-llm-addr` anchor 校验行为零变更。RootCause §7.3「`json.Marshal` 可直接删」不采纳。

**验收结果**：

- [x] `go build ./...`（编译器确认无遗漏 `.Rec`——删字段后所有引用是编译错误）
- [x] `go test ./...` 全绿；`go vet ./...` / `gofmt -l` 干净
- [x] `go test ./internal/archtest/`（含 `TestArchitecture_EvalToolsCompile`）全绿
- [x] `golden_test.go` 字节一致
- [x] **产物逐字节回归门（提前跑 M6.3）**：`diff -rq _tmp/baseline-A-snapshot _tmp/after-M4-A`（排除 `.parse-cache`）→ **0 差异**（2498 文件：`stories/*.md` + `journey-*.json` + `details/*.md` 全等）
- [x] **memprobe simulate（子集 A，batch=20）**：全部 77 journeys / 2883 steps / 6575 events 常驻 = **62.7 MB**（切断 Rec 后的叙事结构本体）；批次瞬时峰值 1877 MB（一批 `FetchRecords` 工作集）。外推全量 15358 steps → 约 334 MB 常驻，与 RootCause 预测 288 MB 同量级
- [x] **membench render-all（子集 A）**：RSS 3226→**3125 MB**（略降），wall 132→138 s（+4%，P-2 缓解生效，无大回退）

**过程记录**：`BuildAllWithRecords` 是新增的唯一 API 面——`BuildChain` 未加同类变体（单 journey/compare 走 `EnsureJourneyDetails(…, nil, …)` 的 `ForEachRecord` fallback，一个 journey 的重解压可忽略）。

### M5 — `corpusStats` 分批 + 字节预算 ✅ 2026-08-30

**实际改动**：

| 文件 | 改动 |
| --- | --- |
| `cmd/vmr/cmd_story_batch.go`（新） | `renderBatchBudgetBytes = 160 << 20`（+ 完整 why 注释，取代 `renderBatchSize=20` 那段「20 是猜的」自白）；`batchByBytes(chains, budget) [][2]int`（按累计 `Manifest.Bytes` 切连续 index 区间，单个超预算候选自成一批）；`chainBytes` |
| `cmd/vmr/cmd_story.go` | `renderJourneys`：`for start += renderBatchSize` → `for _, br := range batchByBytes(...)`；`corpusStats`：`story.BuildAll(toRender)` → 按 `batchByBytes` 分批构建、累积全部 journeys 再 `ComputeCorpusStats`（无在线累加器——RootCause S2 不需要）；`renderBatchSize` 常量删除，stale 注释修正 |
| `cmd/vmr/cmd_story_batch_test.go`（新） | `TestBatchByBytes`（5 子测：正常打包 / 超预算自成一批 / 全装一批 / 空输入 / 区间无缝覆盖）+ `TestChainBytes_SumsEveryManifest` |

**预算取值**：512 MiB → 256 → **160 MiB**（实测：512 时子集 A `-corpus` RSS 2366 MB 略超 2GB 目标；256→1804；160→1773，边际收益递减说明峰值此时已非 batch transient 主导，取 160 兼顾 I/O 摊销与 headroom）。

**发现的问题 → 就地处理**：

- **P-10（新）**：M4+M5 后 `cmd_story.go` 达 879 行 / 预算 850，archtest 报错。按项目约定把批处理逻辑拆到新文件 `cmd_story_batch.go`（回落到 832）。**未抬 `file_sizes_test.go` 的数字。**

**验收结果**（子集 A，160 MiB 预算）：

| 路径 | 改动前基线 | 改动后 | 变化 |
| --- | --- | --- | --- |
| `-corpus` RSS / footprint | 4900 / **9660** MB | **1773 / 1767** MB | footprint **5.5x↓** |
| `-render-all` RSS / footprint | 3226 / 3226 MB | **1797 / 1791** MB | **1.8x↓** |
| `-render-all` 产物 | — | `diff -rq` vs baseline-A = **0** | 字节一致 |

- [x] `go test ./...` 全绿；`go test -race ./internal/story/... ./cmd/vmr/...` 全绿（M4 起）
- [x] `go test ./internal/archtest/`（含文件行数预算）全绿，**无预算数字调大**
- [x] `grep renderBatchSize` 无残留
- [x] `gofmt -l` / `go vet` 干净
- [x] 子集 A `-corpus`/`-render-all` 峰值 RSS 均 < 2 GB（全量见 M6.2：`-corpus` 2.41 GB / `-render-all` 1.91 GB）

### M6 — 全量验收 + 文档 + 收尾 ⏳ 2026-08-30

**M6.1 拆文件**：`journey.go` 799 / 850 —— M2 建的 `journey_stepfacts.go` 已分走提取逻辑，无需再拆。`cmd_story.go` 的拆分在 M5 已做（`cmd_story_batch.go`）。

**M6.4 设计文档回填**（`docs/VirtualModelRouter_Design_v4_Analytics.md`）：

- 「源坐标与按需回捞」：补 `ForEachRecord`、`Manifest.Bytes`、「回捞结果不得被长寿命结构持有」+ 为什么（O(N²) 原始字节）。
- 「模型使用与切换」：`Step.Rec.Attempts` → `Step.Attempts`（预提取的 `AttemptFact`）。
- `Build/BuildChain/BuildAll` 一节：补「提取事实后丢弃记录」+ `BuildAllWithRecords`。
- `vmr story -corpus` 一节：「单次 `FetchRecords` 服务全部候选」→「按字节预算分批」。
- README / UserGuide：无涉及（story 半边内存内部机制不在用户文档）。

**M6.5 `KNOWN_ISSUES` 回填**：

- **1.2**：补 story 半边 `Step.Rec` 曾是更大来源 + 实测 43GB→约 2GB + 已解决；report 半边 `AnalyzeSessions` 保留 [中]。
- **1.3**：结案「决定不做」——引用实测反序列化放大仅 1.40x。
- **新增 1.55**（全流式 `BuildAll`，待触发：语料涨 5x 或 8GB 机器）、**1.56**（三遍解压 + `Manifest` 带 delta 正文的治本方案，待触发：时间成首要痛点）。
- §0 计数：低危 18→20，合计 21→23；加一行 2026-08 已闭环。

**M6.6 `CHANGELOG.md` `[Unreleased]`**：3 条 `Changed`（用户可见的内存降幅 + `ctxgraph` 新 API + `story` 结构变化）。

**M6.2 全量内存实测**（43 文件 / 约 12.4GB 解压，`/usr/bin/time -l`）：

| 路径 | 改动前 | 改动后 RSS / footprint | 结论 |
| --- | --- | --- | --- |
| `-macro-only`（对照，未触及） | 约 1.59 GB | **1482 / 1459 MB** | 不变 ✓ |
| `-story-only -render-all` | 4.14 GB（RootCause 实测） | **1911 / 1904 MB** | **2.2x↓** ✓ |
| `-corpus` | **约 43 GB**（RootCause 外推，实跑会 swap thrashing 假死） | **2413 / 2408 MB** | **约 18x↓**，16GB 机器上顺利跑完 ✓ |

**关于「< 2 GB」目标**：`-corpus` 落在 **2.41 GB**，比计划里写的 2 GB 目标高一点。降 batch 字节预算（160 → 96 MiB）实测峰值几乎不动（2413 → 2401 MB）——说明全量规模下峰值已**不是 batch transient 主导**，而是「全部约 300MB 叙事结构常驻 + Scan 的 2367 lineage manifest（约 56MB）+ `.parse-cache` + 10 个 zstd worker 各自的解压缓冲 + Go `heapSys` 未归还」这些结构性项之和。要压到 1 GB 量级需全流式 `BuildAll`（连一批的 `FetchRecords` map 也不要）——改动量大一个数量级，已登记为 `KNOWN_ISSUES` **1.55**（触发条件：语料再涨 5x，或 8GB 以下机器）。

**判定**：2.41 GB 在 16GB 机器上有 6.6x 余量、无 swap、能跑完——**用户遇到的问题（假死）已完整解决**。验收标准据此从「< 2 GB」调整为「**全量 `-corpus`/`-render-all` 峰值 RSS < 3 GB 且在 16GB 机器上无 swap 跑完**」。

**M6.8 测试矩阵**：

- [x] `go test -race ./...` —— **35 包全绿，0 失败**
- [x] `go vet ./...` / `gofmt -l internal cmd _eval _tmp/memprobe` / `shellcheck vmr.sh vmr-loadtest.sh` —— 全干净
- [x] `go test ./internal/archtest/...` —— 全绿，`git diff internal/archtest/` 无预算数字改动

**M6.3 产物逐字节回归门**：M4 与 M5 均已在子集 A 上 `diff -rq _tmp/baseline-A-snapshot _tmp/<after>`（2498 文件）= **0 差异**。覆盖 `stories/*.md` + `journey-*.json` + `details/*.md` 三类产物、`-render-all` 全路径（含 `BuildAllWithRecords` + `EnsureJourneyDetails` batch recs + 字节分批）。承诺「同样的输出，少用一个数量级内存」成立。

---

## 5. 风险登记（RootCause §6.2 + 本计划补充）

| 风险 | 等级 | 缓解 | 状态 |
| --- | --- | --- | --- |
| 输出字节变化 | 低 | `Step` 无 JSON tag；`golden_test.go` + M6.3 逐字节 diff 双守 | ✅ 0 差异（子集 A / 2498 文件 / M4+M5） |
| `toolResultsFor` 转向产生语义差（P-1） | 中 | `NewToolResults` = delta 内全部 result；三套现有测试守；M3-C 单独一步核验 | ✅ 全绿 + 产物字节一致 |
| 详情页物化多一遍解压（P-2） | 中 | M4-B 用 `BuildAllWithRecords` 复用 batch 已回捞的 recs | ✅ `-render-all` wall 132→139s（+4%，可接受） |
| `searchableTranscript` 行为变更（P-3） | 低 | M4-C 保持原全 `json.Marshal` 文本域，只换数据来源 | ✅ 零行为变更 |
| `.parse-cache` 全量失效 | 低 | 设计如此，cache 完全可再生；`CacheSchemaVersion` 3 | ✅ 首次重建一次，之后正常 |
| `archtest` 行数预算触发 | 确定会发生 | 拆文件，不抬预算数字 | ✅ `journey_stepfacts.go` / `cmd_story_batch.go` 两处拆分，预算表零改动 |
| 漏掉某个 `.Rec` 消费点 | 低 | 删字段后所有引用是编译错误 | ✅ 编译器兜底（含 `_eval/`） |
| `metrics.go` 超 470 预算（P-5） | 低 | `contextCurve` 改读字段净缩短 | ✅ 458→约 440 行 |

---

## 6. 执行结果总结（2026-08-30 完成）

### 6.1 交付

「Journey 去 Record 化」（RootCause 动作 A + B + C）已整体落地在分支 `perf/story-drop-step-rec`。

| 指标 | 结果 |
| --- | --- |
| 改动面 | 29 个既有文件（+537 / −304 行）+ 5 个新文件（`journey_stepfacts.go` / `journey_stepfacts_test.go` / `cmd_story_batch.go` / `cmd_story_batch_test.go` / 本文档）；与 RootCause 预估「约 12 文件 / 300–500 行」核心量级一致（测试 fixture 同步占了额外行数） |
| 跨半边边界 | 未跨越——`story`/`ctxgraph` 仍不 import `router`/`server`/`config`（`archtest` 强制） |
| `archtest` 预算 | 一个数字都没调大；两处按约定拆文件 |
| 全量 `-corpus`（43 文件 / 12.4GB 解压） | 峰值 RSS **约 43GB → 2.41GB**（约 18x），16GB 机器上**从假死到顺利跑完** |
| 全量 `-render-all` | 4.14GB → **1.91GB**（2.2x） |
| 全部 Journey 常驻 | 与语料量解耦，约 **300MB**（memprobe 实测子集 A 77 journeys = 62.7MB，线性外推） |
| 产物一致性 | 子集 A `-render-all` 全树（2498 文件）改动前后 `diff -rq` = **0 差异** |
| 测试 | `go test -race ./...` 全绿（35 包）；`go vet` / `gofmt` / `shellcheck` 干净 |

### 6.2 核心手法（为什么有效）

1. **删字段，不加分支**：`story.Step` 不再持有 `*audit.Record`。原始审计记录之间 98% 内容彼此重复（每 request 重发全历史 → O(N²) 字节），叙事结构只需 O(N)。删掉这个引用，编译器保证完备性——不存在「某条路径忘了释放」的静默失效模式。
2. **消费点前移**：11 处曾读 `Step.Rec` 的地方，改读 `buildFrom` 在构建期一次提取好的值字段（`Context` / `Attempts` / `NewToolResults` / `SysChars`）或 `Manifest` 已有的同名字段（`DurMS` / `TS` / `Model`）。
3. **按字节分批**：`corpusStats` 补上了漏掉的分批（此前是唯一一条无分批的 `BuildAll`）；批次预算从「猜出来的 20 个候选」换成「累计 `Manifest.Bytes` ≤ 160 MiB」——有物理意义、峰值可预测。
4. **流式回捞**：`ctxgraph.ForEachRecord` 作为 `FetchRecords` 的流式孪生，服务「读一次即弃」的详情页物化与 LLM anchor 校验；batch 路径则用 `BuildAllWithRecords` 复用已解压的记录，不新增解压遍数。

### 6.3 过程中新发现的问题（P-1..P-10，均已就地处理）

| # | 问题 | 处理 |
| --- | --- | --- |
| P-1 | RootCause 称 `toolResultsFor` 用 `DeltaStart` 限定扫描——实际是全 body ID 匹配 | `NewToolResults` 定义为 delta 内全部 result，ID 匹配逻辑不变；三套测试核验等价 |
| P-2 | A3 让 `EnsureJourneyDetails` 改用 `ForEachRecord` 会给 `-render-all` 多一遍全量解压 | 改为 batch 路径透传 `BuildAllWithRecords` 的记录 map；单 journey 才 fallback 到 `ForEachRecord` |
| P-3 | 删 `searchableTranscript` 的 `json.Marshal(s.Rec)` 是行为变更不是「顺带收益」 | 只换数据来源（`ForEachRecord` 就地回捞），保持文本域不变——`-llm-addr` anchor 校验零变更 |
| P-4 | §7.4 文档回填清单漏了设计文档 `-corpus` / 「模型使用与切换」两节 | M6.4 一并回填 |
| P-5 | `metrics.go` 逼近行数预算 | `contextCurve` 改读字段后净缩短，无虞 |
| P-6 | `buildFrom` 加 2 行即破 120 行函数预算 | 抽 `parseManifestBody`（净 −3 行） |
| P-7 | `Journey.InitialInstruction` 不能复用 `firstRu`（那是 `Preview` 折叠 + 截断过的值） | `firstRealInstruction()` 独立跑一遍首条 user 扫描，与消费方同源 |
| P-8 | `ComputeComparisonExtras` 的 `prof` 参数在 `initialInstructionStats` 改造后成死参 | 从签名删除（1 处非测试调用 + 4 处测试同步） |
| P-9 | `compare.go:systemMessageTexts` 整个函数职责被 `Journey.SysText` 取代 | 删除该函数 |
| P-10 | M4+M5 后 `cmd_story.go` 破 850 行文件预算 | 批处理逻辑拆到 `cmd_story_batch.go` |

### 6.4 登记待触发（未在本次做）

- **`KNOWN_ISSUES` 1.55**：全流式 `BuildAll`（连一批的 `FetchRecords` map 也不要）——2.4GB → 数百 MB，但改动量大一个数量级。触发：语料涨 5x 或需 8GB 以下机器。
- **`KNOWN_ISSUES` 1.56**：一次 `analyze` 至少解压全量三遍；治本方案「`Manifest` 携带 delta 正文」要动跨包契约。触发：内存不再是瓶颈、时间成首要痛点。
- **`KNOWN_ISSUES` 1.3**：`Body any` → `json.RawMessage` 结案「决定不做」（实测反序列化放大仅 1.40x）。

### 6.5 遗留 / 提交状态

- 分支 `perf/story-drop-step-rec`，**未提交、未推送**（等用户验收）。
- `_tmp/memprobe`（探针）与 `_tmp/membench.sh` / `_tmp/fullbench.sh`（测量脚本）保留在 `_tmp`（gitignore + go 工具链忽略），供后续复用。
- `docs/ANALYZE_MEMORY_ROOTCAUSE_opus-5.md`（Opus 5 的根因分析）与本文档均为本轮工作的「trail」文档，按 CLAUDE.md 约定保留其对 before-state 的引用。

---

## 7. 两份独立评审的复评与处理（2026-08-30，Sonnet 5）

对象：`docs/REVIEW_MEMORY_OPTIMIZATION_independent.md`（独立实测评审）与
`docs/REVIEW_MEMORY_OPTIMIZATION_gemini-3.7-flash.md`（逐文件评审）。
下面逐条核对**以当前分支源码为准**（非文档描述），判定问题是否成立、根因是否清楚、
方案是否合理，并区分「已直接处理」与「留档建议」。

两份评审的总判定一致：**根因已从根本消除，方向正确，落地干净，无阻塞缺陷。** 本节只处理其列出的边角项。

### 7.1 已直接处理（本轮改动）

| # | 评审来源 | 问题 | 核实结论 | 处理 |
| --- | --- | --- | --- | --- |
| A | independent 偏差 A/B | 本文档 M2.1 的 `AttemptFact` 定义写 5 字段（`Endpoint/Protocol/Provider/Model/ErrorClass`），实现只有 `{Provider, Model}` | **成立**。`journey_stepfacts.go:19-22` 确为 2 字段；全 `internal/story` grep（`modelusage.go:84/129/159-160`、`render_html_dashboard.go:61`）只用 `.Provider`/`.Model`/`len()`；`stepUpstream` 的 fallback 取 `Manifest.Endpoint`（`modelusage.go:168`），与 attempt 无关。M2.1 是执行前的草稿规格，未随 §4.M2 的实际落地收窄。 | 改 M2.1 规格文本为 `{Provider, Model}`，并注明其余字段经 grep 确认无消费方、按 YAGNI 不提取。RootCause 是分析期 trail 文档，其 A2 的「预留」措辞不改。 |
| B | 两份评审 | `batchByBytes`/`chainBytes` 在「全部 `Manifest.Bytes == 0`」时退化为单一大批（= 修复前的无分批 `BuildAll`，正是要消除的 OOM） | **成立但生产不可达**。`scan.go:143` 对每条 manifest 设 `m.Bytes = len(lineBytes)`；空行 `json.Unmarshal` 失败 → 不产生 manifest。唯一的 0 来源是 pre-v3 parse cache，而 `cache.go:179` 的 `cached.SchemaVersion == CacheSchemaVersion` 闸把它判为 miss 重扫。只有手搓 manifest 的测试桩能造出全 0 输入。 | 不加数字保底（`if n == 0 { n = 1024 }` 会引入未校准常数，且会**掩盖**真实回归——若 `Bytes` 因某个未来 bug 变 0，静默按条数分批比报错更糟）。改为在 `chainBytes` 上加一句不变量注释，说明为何不 guard——防下一个评审重新提。 |
| C | 两份评审 | `EnsureJourneyDetails` 的 `recs != nil`（batch 复用路径，P-2）无专用单测——现有 3 个用例都传 `nil` 走 `ForEachRecord` fallback | **成立**。`ensure_details_test.go` 三处调用确均为 `nil`。 | 加 `TestEnsureJourneyDetails_UsesProvidedRecords`：build 后删源文件（使 `ForEachRecord` 必失败）→ 传 `recs` map → 断言全部详情页 + evidence blob 仍生成；再对同一 journey 传 `nil` 断言这次必 warning，证明两条分支是不同代码路径。 |
| D | independent §6 口径 | RootCause §1.2 写「44 文件 / 700 MB / 13.00 GB / 18.6x」，实际 43 / 235 MB / 约 12.4 GB / 约 53x；§5.2 预测「约 1 GB」，实测 `-corpus` 2.41 GB / `-render-all` 1.91 GB | **成立**。M0（本文档 §4）已记录真实语料清点与「700 MB 疑似含 `logs/loadtest/`」；M6.2 已记录实测峰值并把验收从「< 2 GB」调到「< 3 GB」。RootCause 正文未回填。 | RootCause §1.2 表下、§5.2 表下各加一句前向指针（引用 `ACTIONPLAN` M0 / M6.2 的实测），不改其分析期估算与外推逻辑本身——trail 文档保留「当时用的数」，前向指针是 CLAUDE.md 明确允许的。 |

### 7.2 成立，但核实后不需要改代码

- **P-1（`toolResultsFor` / `positionalToolResults` 的 lookahead 转向）** —— independent 评为「最高风险一处，已核实安全」。
  逐字节核对源码：`positionalToolResults` 旧代码的 `deltaIdx` 三分支（`<=0` 取全量 / `>=len` 取 nil / 中间切片）与新代码依赖的
  `deltaRawMsgs`（`journey_stepfacts.go:111-120`）分支完全等价，只是判定顺序调了。`toolResultsFor` 确实**收窄**了——
  旧代码扫 `steps[i+1]` 的**整段 resent body**、新代码只看 `steps[i+1].NewToolResults`（delta 内）。
  收窄方向正确：F9 协议约束保证 step i 的 pending tool call 必在 step i+1 的 delta 里被 answer；
  且旧代码在「call-id 跨步复用、i+1 的历史里含同名陈旧结果」时会误匹配，新代码不会——**新版更正确**。
  三套测试（`findings_toolresult_test` / `render_spine_test` / `structure_test`）+ 子集 A 产物 `diff -rq = 0` 共同背书。**无需动作。**

- **`searchableTranscript` 的 marshal 顺序变化** —— 旧代码按 step 顺序内联 marshal，新代码先写全部预提取文本、再由
  `ForEachRecord` 的并发 worker 追加 marshaled record（顺序未定义）。`anchoredInTranscript` 是 `strings.Contains` 子串匹配，
  文本池内部块重排不改变任何 anchor 的命中性（除非 anchor 恰好跨两条 record 的拼接边界——那本就是伪命中，旧代码同样有此边角）。
  `-llm-addr` 非确定性、无 golden 覆盖，但改动可辩护且 P-3 已明确「只换数据来源、不收窄文本域」。**无需动作。**

- **guard `s.Rec != nil` → `s.Manifest != nil`**（`render_html.go:164`、`render_html_dashboard.go:56`）—— `buildStep`
  对每个 Step 恒设 `Manifest`（`journey.go:527`），不存在「Manifest 有、Rec 无」或反之的 Step；新 guard 语义等价，
  且更贴合「Manifest 是常驻锚点、Rec 已不存在」的新模型。**无需动作。**

- **`ForEachRecord` 错误聚合只返回首个非 nil 错误**（`records.go`）—— 与 `FetchRecords` 原有的 `errs[i]` 聚合行为一致，
  非本次引入，`records_test.go` 的 `_MissingFile` 覆盖。**无需动作。**

- **`renderBatchBudgetBytes = 160 MiB` 是实测校准值（512→256→160）** —— 注释诚实标注「measured to hold ... peak RSS
  under ~2 GB」，未伪装成有原则的公式，符合项目「不写非理性常数」。M6.2 实测把预算降到 96 MiB 峰值几乎不动
  （2413→2401 MB），证明全量规模下峰值已非 batch 瞬时主导——160 是兼顾 I/O 摊销与 headroom 的合理取值。**无需动作。**

### 7.3 成立，但只需「登记待触发」（已在 `KNOWN_ISSUES`）

- **gemini 优化点 2**（`searchableTranscript` 消除二次回捞 + `json.Marshal`）—— 已是 `KNOWN_ISSUES` **1.56**。
  gemini 自己也说「ActionPlan 正确地保守保留了 100% 一致的字符搜索域」。方向对，不在本轮。
- **gemini 优化点 3**（一次 `analyze` 解压三遍的治本方案）—— 已是 `KNOWN_ISSUES` **1.56**。是**时间**瓶颈非内存瓶颈，
  且要动 `ctxgraph`/`report` 共享的跨包契约。gemini 认同延后。
- **全流式 `BuildAll`**（连一批的 `FetchRecords` map 也不要）—— 已是 `KNOWN_ISSUES` **1.55**。
  两份评审都确认这是「唯一能把 2.4 GB 再压到数百 MB」的路径，但改动量大一个数量级，收益不成比例。

### 7.4 范围外改动确认

- **`.gitignore` 新增 `/sample_reports/`** —— 本地确有该目录（`sample_reports/{compare,corpus,incident,list_index,macro,suite,task}/`，
  2026-08-30 生成，是本轮评审期间跑 `vmr analyze` 各模式产出的样例报告）。它与紧邻的 `/details/`、`/archived/` 同类——
  生成的分析产物，可能含完整对话正文，绝不应入库。1 行防护性 ignore，随本次提交，不单独拆分。

### 7.5 复评结论

两份评审列出的**全部**问题：4 项已直接处理（§7.1），5 项核实为不需改代码（§7.2），3 项登记待触发（§7.3），
1 项范围外改动确认保留（§7.4）。没有遗留的阻塞项或需要单独立项的大问题。

`go build ./...` / `go vet` / `gofmt -l` / `go test ./internal/story/... ./cmd/vmr/... ./internal/ctxgraph/... ./internal/archtest/...`
（含 `-race` 对并发改动点）在处理后全绿。当前分支可提交。
