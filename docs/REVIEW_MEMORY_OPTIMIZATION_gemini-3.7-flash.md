<!-- Ver 2026-08-30 20:48, by gemini-3.7-flash -->

# `vmr analyze` 内存瓶颈治理代码评审报告

本文档对分支 `perf/story-drop-step-rec` 上的所有未提交变更进行逐文件、全方位的系统级代码评审（Code Review）。
评审基准以 `docs/ANALYZE_MEMORY_ROOTCAUSE_opus-5.md`（根因复核与方案设计）与 `docs/ANALYZE_MEMORY_ACTIONPLAN_sonnet-5.md`（7 阶段落地执行计划）为准绳，重点核查：**内存问题的根本性解决度**、**架构优化空间**、**代码疏漏与边界错误**、**测试完备性**及**文档同步一致性**。

---

## 评审结论总览 (Executive Summary)

| 评审维度 | 评价等级 | 核心结论 |
| --- | --- | --- |
| **内存问题根本性解决** | **优秀 (A+)** | **彻底且根本性解决**。消除了 `Step.Rec` 对原始审计记录的长生命周期引用（切断了 O(N²) 字节驻留）；补齐了 `-corpus` 的物理字节预算分批；全量 43 文件实测峰值 RSS 从约 43 GB 假死降至 **2.41 GB**，全部 586 个 Journey 常驻仅占约 300 MB（与数据总量解耦）。 |
| **方案设计与架构质量** | **优秀 (A)** | 方案遵循奥卡姆剃刀原则，未引入复杂对象池或累加器抽象，而是将实现拉回「提取事实后即弃、按需流式回捞」的既定契约；架构复杂度净减少；`archtest` 规则严格坚守，未抬高任何预算数字。 |
| **潜在错误与代码质量** | **良好 (A-)** | 全程类型系统与编译器兜底，无内存泄漏、无数据竞态（`go test -race` 35 包全绿）；逻辑等价性完好；仅在极值边界（如零字节 manifest 时的批次退化）存在可轻微防御强化的点。 |
| **测试完备性** | **优秀 (A)** | 覆盖单元测试、并发竞态、回归 golden 测试、2498 个产物文件的逐字节 `diff` 比对（0 差异）；仅缺少一个针对 `EnsureJourneyDetails(recs != nil)` 传入非空 map 的专用单元测试用例。 |
| **文档同步性** | **完全同步 (A+)** | 设计文档（4 处关键小节）、`KNOWN_ISSUES`（1.2 更新、1.3 结案、新增 1.55/1.56、计数对齐）、`CHANGELOG.md`、`.gitignore` 全部同步到位，无陈旧字段残留。 |

---

## 一、核心问题深度评审

### 1. 内存占用问题是否得到根本性解决？

**结论：是，得到了第一性原理层面的根本性解决。**

1. **切断了 O(N²) 重复历史的物理驻留**：
   - LLM Chat Completion 协议是无状态的，每一轮对话必须重发历史，导致磁盘上一条 N 步会话的原始 JSON 呈现 O(N²) 膨胀。
   - 原实现中 `story.Step.Rec` 长期持有解压后的 `*audit.Record`，将 98.6% 的重复历史数据钉在堆内存中。
   - 变更直接在 struct 定义中删除了 `Step.Rec`，改在 `buildFrom` 阶段一次性提取标量与细粒度事实（`Context` / `Attempts` / `NewToolResults` / `SysChars`）。这由 Go 编译器提供了绝对完备性保证，杜绝了任何「某条路径遗漏释放」的隐式内存泄漏。
2. **消除了 `-corpus` 路径的无分批单体全量加载**：
   - 过去的内存崩溃并非发生在已带 `batch=20` 的 `-render-all` 路径，而是独属于 `corpusStats`。它一次性调用 `story.BuildAll` 强行将全部 588 个候选读入单张巨型 map。
   - 现已将 `corpusStats` 改为与 `renderJourneys` 共享的字节分批迭代（`batchByBytes`），每批 `FetchRecords` 解压后的工作集在批次结束后立即丢弃。
3. **数据规模彻底解耦**：
   - 提取后的 Journey 结构体仅占原始 JSON 数据的 **1.17%**（约 86 倍冗余被消除）。全量 586 个 Journey、15,358 steps 的纯叙事结构体常驻堆仅 **288 MB**，完全可以常驻于 16GB 机器上供 `ComputeCorpusStats` 一次性完成统计。
   - 实测指标：全量 43 文件（解压 12.4 GB），`-corpus` 峰值 RSS 从约 43 GB 降到 **2.41 GB**（降幅 ~18x），`-render-all` 从 4.14 GB 降到 **1.91 GB**（降幅 ~2.2x），16GB 机器无任何 swap 顺畅跑完。

---

### 2. 方案是否有优化与演进空间？

**结论：当前方案在「投入产出比与架构简洁度」上达到了最优平衡点；但在极致防御与后续性能演进上存在以下 3 点可优化空间：**

#### 优化点 1：`batchByBytes` 对零字节 Manifest 的防御性保底 (Defensive Fallback)
- **现状**：`cmd_story_batch.go` 中的 `chainBytes` 通过累加 `m.Bytes` 计算预算。
  ```go
  func chainBytes(chain []*ctxgraph.Lineage) int {
      total := 0
      for _, l := range chain {
          for _, m := range l.Manifests {
              total += m.Bytes
          }
      }
      return total
  }
  ```
  如果某个 candidate chain 包含的所有 manifest 的 `Bytes` 均为 0（例如：单元测试中构造的伪 manifest、或极端异常情况下的空行），`curBytes + n > budget` 将永远不成立，导致所有 candidates 被一股脑塞入同一个批次 `[0, len(chains)]`。
- **评估与建议**：生产环境由于 `CacheSchemaVersion = 3` 会强制全量重新扫描，且 `scanFile` 中 `m.Bytes = len(lineBytes) > 0`，因此生产路径不会触发。但从防御性编程角度，可考虑增加单 step 最小字节估算（如 `if n == 0 { n = 1024 }`），避免在异常输入下静默退化为全量单批。

#### 优化点 2：`searchableTranscript` 的文本重组可消除二次回捞与 Marshal
- **现状**：`internal/story/llm_findings.go` 中的 `searchableTranscript` 为校验 LLM 发现的 `EvidenceAnchor` 是否真实存在于记录中，通过 `ctxgraph.ForEachRecord` 再次从磁盘流式读取原始 JSON，并调用 `json.Marshal(rec)` 拼装成文本池。
- **评估与建议**：ActionPlan（P-3）出于保守原则保留了与老版本 100% 一致的字符搜索域。但实际上，Anchor 主要匹配的是 prompt、response、tool_calls 参数与 tool_results。由于 `Step` 已经提取了 `NewToolResults` 与 `ToolCalls`，未来单 Journey LLM 模式若想完全省去这一次磁盘 I/O 和 `json.Marshal` 开销，可以直接基于 Step 的已有字段拼接搜索池（已在 `KNOWN_ISSUES` 1.56 登记待触发）。

#### 优化点 3：多遍解压的治本方案（跨包契约）
- **现状**：一次 `vmr analyze` 仍需要解压全量语料 3 遍（`PreviewTitles` 一遍、`BuildAll` 按批次解压、`report` 半边的 `analyzeFile` 一遍）。
- **评估与建议**：这是时间瓶颈（~500s）而非内存瓶颈。ActionPlan 正确地将其延后，不在本次混入跨包契约改造。

---

### 3. 是否存在明显的疏漏、错误或并发风险？

**结论：无功能性缺陷与安全漏洞，通过代码级静态分析确认逻辑严密。发现 2 处细微边界细节值得记录：**

1. **`ForEachRecord` 并发回调与共享状态保护**：
   - `ctxgraph.ForEachRecord` 会在 worker 池中并发调用 `fn(loc, rec)`。
   - 核查所有调用点：
     - `internal/story/ensure_details.go`：在调用 `render(byLoc[loc], rec)` 时正确使用了 `sync.Mutex` 互斥锁，保护了详情页与 evidence 文件的并发写入。
     - `internal/story/llm_findings.go`：在 `b.Write(data)` 时正确使用了 `sync.Mutex` 互斥锁。
     - `internal/ctxgraph/records_test.go`：测试用例收集 map 时正确加锁。
   - **结论**：并发安全性得到严格保障。
2. **`firstRealInstruction` 与 `firstRu` 的多轮会话语义一致性**：
   - `journey.go` 在 `!firstRuSet`（即处理链上第一个 step）时提取 `j.InitialInstruction`。
   - 经与老代码 `compare.go` 对比，老代码同样是在 `steps[0].Rec` 上提取首条 instruction。两者行为与截断策略完全一致。

---

### 4. 测试覆盖是否完备？

**结论：测试覆盖极为全面，构建了从单元测试、架构约束到逐字节黑盒回归的多层防护网。**

1. **新增与强化的测试**：
   - `cmd/vmr/cmd_story_batch_test.go`：覆盖了正常连续分批、单个超限自成一批、全量单批、空切片、区间无缝连续覆盖等 5 个子场景。
   - `internal/story/journey_stepfacts_test.go`：覆盖了 `Attempts` / `Context` / `NewToolResults` / `SysChars` / `SysText` / `InitialInstruction` 的全部提取逻辑，并锁定了 delta 范围隔离。
   - `internal/ctxgraph/records_test.go`：测试了 `ForEachRecord` 与 `FetchRecords` 的逐条结果比对、空输入、文件缺失、`Manifest.Bytes` 注入。
2. **端到端黄金回归与字节一致性门禁**：
   - 子集 A 上 2,498 个生成文件（`stories/*.md`、`journey-*.json`、`details/*.md`）在改动前后的 `diff -rq` 结果为 **0 差异**。
   - `internal/story/golden_test.go` 端到端 Markdown 比对完全通过。
   - `invariants_test.go` 工具调用 100% 配对不变量测试通过。
3. **唯一的微小测试盲区 (Minor Test Gap)**：
   - `internal/story/ensure_details_test.go` 中现有的 3 个测试用例均传入了 `recs = nil`，仅直接覆盖了 `ForEachRecord` 的回捞分支。
   - 虽然 `cmd_story.go` 中的 `renderJourneys` 在端到端运行时实际执行了 `recs != nil` 分支，但在 `ensure_details_test.go` 内部建议未来补充一个传入预先填充好的 `map[Loc]*audit.Record` 的单测用例。

---

## 二、逐文件详细评审表 (File-by-File Review)

### 1. 基础构架层：`internal/ctxgraph/`

| 文件 | 改动性质 | 代码级剖析与评审意见 | 结论 |
| --- | --- | --- | --- |
| `internal/ctxgraph/manifest.go` | 字段新增 | `Manifest` 增加 `Bytes int`（解压 JSON 行字节数），带清晰的 omitempty 和 doc 注释，为后续字节预算分批提供计量基准。 | ✅ 通过 |
| `internal/ctxgraph/scan.go` | 计量填充 | `scanFile` 中在 `BuildManifest` 成功后执行 `m.Bytes = len(lineBytes)`。不改动 `BuildManifest` 签名，架构解耦良好。 | ✅ 通过 |
| `internal/ctxgraph/cache.go` | 版本递增 | `CacheSchemaVersion` 从 2 升至 3。迫使包含旧 Manifest（`Bytes=0`）的 `.parse-cache` 自动失效并安全重建，杜绝脏缓存。 | ✅ 通过 |
| `internal/ctxgraph/records.go` | 新增流式接口 | 抽取 `forEachRecordInFile` 共享扫描核；新增 `ForEachRecord` 流式回捞接口，采用 `scanWorkerCount` 信号量池控制并发，每行解码后立即传给回调。并发调用契约在注释中明确约定。 | ✅ 通过 |
| `internal/ctxgraph/records_test.go` | 单元测试 | 新增 `TestForEachRecord_MatchesFetchRecords`（对比 DeepEqual）、`TestForEachRecord_MissingFile`、`TestForEachRecord_EmptyLocs`、`TestScanFile_PopulatesManifestBytes`，覆盖充分。 | ✅ 通过 |

---

### 2. 叙事核心层：`internal/story/`

| 文件 | 改动性质 | 代码级剖析与评审意见 | 结论 |
| --- | --- | --- | --- |
| `internal/story/journey.go` | 结构体精简与解耦 | `Step` 彻底删除 `Rec *audit.Record`；增加 `Context`、`Attempts`、`NewToolResults`、`SysChars` 四个预提取字段；`Journey` 增加 `InitialInstruction` 与 `SysText`；`BuildAll` 演变为 `BuildAllWithRecords` 的薄包装，后者向调用方暴露记录 map 供后续复用。 | ✅ 通过 |
| `internal/story/journey_stepfacts.go` | 新文件（事实提取） | 承载 `AttemptFact` 定义、`parseManifestBody`、`fillStepFacts`、`stepContextPoint`、`deltaRawMsgs`、`firstRealInstruction`。职责单一，成功分流了 `journey.go` 的复杂度，确保两文件均未突破 `archtest` 行数上限。 | ✅ 通过 |
| `internal/story/journey_stepfacts_test.go` | 新文件（单测） | 全面验证预提取事实的准确性、与旧版 `contextCurve` 的数值一致性、以及 `NewToolResults` 严格局限在 DeltaStart 范围内的边界行为。 | ✅ 通过 |
| `internal/story/metrics.go` | 消费点切转 | `computeTimeSplit` 改用 `s.Manifest.DurMS`；`contextCurve` 简化为直接收集 `s.Context`，移除了原本在指标计算时的二次 body 反序列化。函数行数减少，运行更轻量。 | ✅ 通过 |
| `internal/story/modelusage.go` | 消费点切转 | 改读 `Step.Attempts`。`stepUpstream` 优先读末次 Attempt 的 Provider/Model，回退切分 `Manifest.Endpoint`。`computeModelUsage` 的切换点与 `OnFailoverStep` 判定保持完全一致。 | ✅ 通过 |
| `internal/story/modelusage_test.go` | 测试同步 | 14 处合成 Step fixture 从 `Rec` 迁移至 `Attempts: []AttemptFact`，测试通过。 | ✅ 通过 |
| `internal/story/findings_toolresult.go` | 消费点切转 | `toolResultsFor` 改为直接从 `steps[i+1].NewToolResults` 中匹配工具调用 ID，消除了一处对下步 raw body 的全量扫描，逻辑等价性由 3 套现有 Finding 测试保障。 | ✅ 通过 |
| `internal/story/render_spine_step.go` | 消费点切转 | `positionalToolResults` 转向读取 `steps[i+1].NewToolResults`，不再执行重复的 `DeltaStart` 切片计算（已在 `fillStepFacts` 完成）。 | ✅ 通过 |
| `internal/story/render_spine_test.go` | 测试同步 | 调整 fixture 数据填充方式，验证在 delta 内未提供 tool result 时不会误认历史残留。 | ✅ 通过 |
| `internal/story/render_html.go` / `render_html_dashboard.go` | 消费点切转 | `s.Rec.TS` / `s.Rec.Model` 机械替换为 `s.Manifest` 对应字段；`len(s.Rec.Attempts)` 换为 `len(s.Attempts)`。 | ✅ 通过 |
| `internal/story/render_md_sysprompt.go` | 消费点切转 | `sysPromptEraChars` 直接返回 `e.Owner.SysChars`，移除 `chatmsg` 依赖。 | ✅ 通过 |
| `internal/story/compare.go` | 消费点切转与简化 | `initialInstructionStats` 读 `j.InitialInstruction`；`sysPromptStats` 读 `j.SysText[SysHash]`；删除无用函数 `systemMessageTexts`；`ComputeComparisonExtras` 移除了已无用途的 `prof` 参数。 | ✅ 通过 |
| `internal/story/compare_test.go` / `render_compare_html_test.go` | 测试同步 | 同步更新 `ComputeComparisonExtras` 调用签名与 fixture。 | ✅ 通过 |
| `internal/story/ensure_details.go` | 详情导出优化 | `EnsureJourneyDetails` 支持传入 `recs` map。若非空直接内存取用（复用 batch 已解压数据，零额外 I/O）；若为空则通过 `ForEachRecord` 流式回捞并使用互斥锁串行化写盘。 | ✅ 通过 |
| `internal/story/ensure_details_test.go` | 测试同步 | 传入 `nil` 验证了流式回捞分支与幂等性。 | ⚠️ 见建议 1 |
| `internal/story/llm_findings.go` | 回捞迁移 | `searchableTranscript` 改用 `ForEachRecord` 按需流式读取，保持原有 full `json.Marshal(rec)` 文本域不变，保证了 `-llm-addr` 模式下 anchor 校验判定零漂移。 | ✅ 通过 |
| `internal/story/llm_findings_test.go` | 测试同步 | 适配 `searchableTranscript` 的双返回值签名。 | ✅ 通过 |
| `internal/story/invariants_test.go` | 不变量测试维护 | 引入 `fetchStepRecords` 辅助函数按坐标重新读取记录，持续守护工具调用 100% 配对架构不变量。 | ✅ 通过 |

---

### 3. 命令与入口层：`cmd/vmr/`

| 文件 | 改动性质 | 代码级剖析与评审意见 | 结论 |
| --- | --- | --- | --- |
| `cmd/vmr/cmd_story_batch.go` | 新文件（批次分片） | 定义 `renderBatchBudgetBytes = 160 << 20`（160 MiB）；提供 `batchByBytes` 连续区间切分器与 `chainBytes` 体积求和。分批标准从不可预测的「候选条数」进化为有明确物理意义的「解压字节预算」。 | ✅ 通过 (见建议 2) |
| `cmd/vmr/cmd_story_batch_test.go` | 新文件（单测） | 完备测试 `batchByBytes` 与 `chainBytes` 的各种边界条件（空、超大单条、连续累加）。 | ✅ 通过 |
| `cmd/vmr/cmd_story.go` | 调度改造 | 彻底移除旧常量 `renderBatchSize = 20`；`renderJourneys` 使用 `BuildAllWithRecords` 并将 `batchRecs` 透传给 `writeJourneyFile` / `EnsureJourneyDetails`；`corpusStats` 改为按 `batchByBytes` 分批构建并累积轻量 Journey。 | ✅ 通过 |

---

### 4. 评估工具、文档与配置

| 文件 | 改动性质 | 代码级剖析与评审意见 | 结论 |
| --- | --- | --- | --- |
| `_eval/calibrate_p1b.go` | 评估工具适配 | `transcriptPool` 同步调整为通过 `ctxgraph.FetchRecords` 获取记录并 marshal，保证 `_eval` 工具链可编译且行为一致。 | ✅ 通过 |
| `.gitignore` | 规则维护 | 增加 `/sample_reports/` 临时目录忽略。 | ✅ 通过 |
| `CHANGELOG.md` | 发版日志更新 | `[Unreleased]` 补充了完整的用户级与开发者级变更条目。 | ✅ 通过 |
| `docs/VirtualModelRouter_Design_v4_Analytics.md` | 设计文档更新 | 4 处关键小节与当前实现细节完全对齐，消除过时描述。 | ✅ 通过 |
| `docs/KNOWN_ISSUES_sonnet-5.md` | 缺陷库更新 | 1.2、1.3 条目状态变更；新增 1.55 与 1.56 待触发条目；计数严格校准。 | ✅ 通过 |

---

## 三、后续改进建议 (Actionable Recommendations)

虽然当前变更已经具备合并就绪（Merge-Ready）的高质量标准，但在后续迭代中可考虑以下微调：

1. **补齐 `EnsureJourneyDetails` 的 Non-Nil Recs 单元测试**：
   在 `internal/story/ensure_details_test.go` 中增加一个测试用例，显式传入一个 `map[Loc]*audit.Record`，验证其无需触发 `ForEachRecord` 即可成功渲染详情页并生成 evidence blob。
2. **`chainBytes` 增加最小正数保底**：
   在 `cmd/vmr/cmd_story_batch.go` 的 `chainBytes` 中，若整条链 `total == 0`，可返回一个保底值（如 `1` 或 `len(l.Manifests) * 1024`），确保在任何极端缺少 `m.Bytes` 的异常语料或测试桩数据中，分批算法仍能按条数大致均分，而不是退化成单一大批。

---

## 四、最终评审意见 (Final Verdict)

**通过 (APPROVED)**。
本次重构方向精准、实施克制、逻辑严密、测试坚实，彻底根治了 `vmr analyze -corpus` 在大语料上的内存爆炸与假死问题，将系统带回了整洁架构的设计契约。未发现阻碍发布的阻塞性问题（Blocker）。
