<!-- Ver 2026-08-30, independent review -->

# `vmr analyze` 内存优化：独立评审

评审对象：`docs/ANALYZE_MEMORY_ROOTCAUSE_opus-5.md`（根因与方案，Opus 5）+ 其在当前未提交工作区里的落地改动。
本文按要求**独立做出**，未阅读 `docs/REVIEW_MEMORY_OPTIMIZATION_gemini-3.7-flash.md`，未修改任何已有文档或源码。

核心结论：**问题确已从根本上解决，方向正确、落地干净、测试完备、文档基本同步。** 发现 2 个值得注意的
实现偏差（均是良性的），若干低危观察，以及 2 处文档/数值口径不精确。没有任何会阻止合并的缺陷。

---

## 1. 评审方法与范围

- **对照实现**：逐文件读完全部未提交 diff（29 个改动文件 + 5 个新文件），对照 RootCause 文档的
  「动作 A（Journey 去 Record 化）/ 动作 B（corpusStats 分批）/ 动作 C（字节预算）」逐一核对。
- **实测验证**（真跑，不只看代码）：
  - 旧基线：在 `a73b8d3`（当前分支分叉点）用 `git worktree` 另建目录现编二进制（不碰工作区）。
  - 新实现：当前分支现编二进制。
  - 同一 3 文件子集（`logs/vmr-audit-2026-07-15/18/19.jsonl.zst`，29 MB 压缩）跑 `-corpus` / `-render-all`，
    用 `/usr/bin/time -l` 抓 max RSS + peak footprint + wall。
  - `go test ./...`、`go test -race ./...`（重点包）、`go vet ./...`、`gofmt -l`、
    `go test ./internal/archtest/...`（含 `_eval` 编译门）、`golden_test` 单独跑。
  - 对新增 JSON 产物做结构级 diff（去本地化字符串，保留结构/数字/布尔）。

---

## 2. 实测数据（旧 vs 新，同一子集）

| 路径 | 旧基线 (a73b8d3) | 新实现 | RSS 降幅 | footprint 降幅 |
| --- | --- | --- | --- | --- |
| `-corpus` | RSS 3.39 GB / footprint **5.07 GB** / 53.4 s | RSS 1.08 GB / **1.08 GB** / 56.6 s | **3.1x** | **4.7x** |
| `-render-all` | RSS 4.39 GB / footprint **4.73 GB** / 65.0 s | RSS 1.07 GB / **1.06 GB** / 65.3 s | **4.1x** | **4.5x** |

旧版 `-corpus` 的 footprint（5.07 GB）显著高于 `-render-all`（4.73 GB），证实 RootCause 文档「`-corpus`
线性于语料体积、`-render-all` 已被分批」的判断；新版把两条路径都压到约 1 GB 量级的单个批次工作集。
**原文档断言的「把实现拉回契约」确实起了作用，而不是靠分块摊薄。**

> 说明：这里的降幅是 3–5x，而不是 CHANGELOG 写的「约 18x」。原因：18x 是**全量 43 文件**语料下的数字
> （旧版在 16 GB 机器上会 ramp 到约 43 GB），我无法安全实跑全量旧版（用户明确警示会假死/占满内存），
> 只在子集上验证了**机制**成立。子集旧版只到 3–5 GB，故降幅按比例就是 3–5x。方向与量级一致，属可信。

### 产物一致性（回归门）

- 新增 JSON 产物（`stories/*.json`）41 个文件，新旧**文件名集合完全相等，结构 diff（去字符串后）= 0 处差异**。
- `golden_test`（`TestGoldenMarkdown` en/zh）字节一致通过。
- 唯一肉眼可见的 `.md` 差异是**本地化语言**：新版在 `/Volumes/SSD2T/code/vmr` 跑（该目录 `report.yaml` 设
  `language: zh`），旧版在 `/tmp` 跑（无配置，降级为 `i18n.EN`）——这是**运行环境差异，不是代码差异**，
  `resolveLanguage` 的「flag > report.yaml > EN」逻辑（`cmd/vmr/reportconfig.go:130`）解释得通。**在同一语言
  下产出字节一致**的判断成立。

---

## 3. 测试矩阵（实跑结果）

| 项 | 结果 |
| --- | --- |
| `go build ./...` | ✅ |
| `go vet ./...` | ✅ |
| `gofmt -l internal cmd _eval` | ✅（无输出） |
| `go test ./...` | ✅ 全绿 |
| `go test -race ./cmd/vmr/...` | ✅ |
| `go test -race ./internal/ctxgraph/... ./internal/story/...` | ✅（ctxgraph 274s） |
| `go test ./internal/archtest/...` | ✅，`git diff internal/archtest/` **无任何预算数字被调大** |
| `go test ./internal/archtest/... -run TestArchitecture_EvalToolsCompile` | ✅（`_eval/calibrate_p1b.go` 编译门） |
| `golden_test` | ✅ 字节一致 |

并发正确性：`ForEachRecord` 的 fn 由 per-file worker 并发调用，`EnsureJourneyDetails` 的 fallback 路径用
`sync.Mutex` 串行化（写 evidence blob 会冲突），`searchableTranscript` 用 mutex 保护 builder——`-race` 全绿证明
这些同步是充分的。`fillStepFacts` 里对 `j.SysText`/`j.InitialInstruction` 的写入只在**单链内**（`buildFrom` 顺序
执行），无跨链竞争。

---

## 4. 根因是否根本解决？—— 是，且设计上更优

1. **`Step.Rec` 已删除**，编译器兜底（删字段后所有引用是编译错误），不存在「某路径忘了释放」的静默失效。
   全 `internal/story` 非测试代码仅剩 4 处注释提及 `Step.Rec`，一处 `journey.go:591` 的 `predRec` 是
   `buildCompactionInfo` 按需重读前驱 body 的**局部变量**（设计与文档均允许），不是长寿命持有。
2. **消费点全部改读预提取事实**：`computeTimeSplit` 改读 `s.Manifest.DurMS`（已确认 `BuildManifest` 在
   `manifest.go:115` 复制了 `DurMS: rec.DurMS`，逐字相等）；`contextCurve` 改读 `s.Context`；`modelusage`
   改读 `s.Attempts`；`sysPromptEraChars` 改读 `s.SysChars`；`sysPromptStats` 改读 `j.SysText[SysHash]`
   （SysHash 内容寻址，同一哈希必同文，故与原先「取 target 步正文」等价）；`initialInstructionStats`
   改读 `j.InitialInstruction`。
3. **`corpusStats` 与 `renderJourneys` 共用同一条按字节预算分批路径**，无第二套累加逻辑，符合 RootCause
   对「不在线累加器」的决定。
4. **`EnsureJourneyDetails` 用 `BuildAllWithRecords` 复用批次已解压记录**（P-2），避免给 `-render-all`
   多加成一遍解压；单 journey 路径走 `ForEachRecord` 流式回捞（一 journey 的重解压可忽略）。

结论：内存模型已**收敛到 report 半边的「提取完即弃」**，下次有人写「遍历所有 Journey 做点什么」时不会
再踩回同一坑——这正是 RootCause 把方案评价为「架构复杂度净减少」的实质，实测认可。

---

## 5. 发现的问题与观察（按严重度）

### 5.1 实现与方案的偏差（良性，但计划文档未记录）

**偏差 A：`AttemptFact` 瘦身成 2 字段，Action Plan 写的是 5 字段。**
- Action Plan M2.1 与 RootCause A2 都写明：`type AttemptFact struct { Endpoint, Protocol, Provider, Model, ErrorClass string }`。
- 实现（`journey_stepfacts.go:22-27`）只有 `{ Provider, Model }`。
- **影响**：无。我已全 `internal/story` grep，没有任何消费点读 `Attempts[i].ErrorClass` / `.Endpoint` /
  `.Protocol`（这些只从 `Manifest.Endpoint` 读）。代码注释解释了「routing-half 的细节本无 story 消费方」。
- **建议**：要么把 Action Plan 的提法收窄成 `{Provider, Model}`，要么补一句「Endpoint/Protocol/ErrorClass
  经实测无消费方，故未提取」。当前 Action Plan 与实现不一致，会让下一个读回的人困惑。低危。

**偏差 B：`AttemptFact` 也丢了 `ErrorClass`，而 RootCause A2 注释说它是「预留给未来」。**
- 如果未来要有 consumer，得回填。这是架构层的一个「预留」被悄悄砍掉，属 YAGNI 取向（可辩护），
  但同样应与计划文档对齐。低危。

### 5.2 值得注意的实现点（需知悉，非缺陷）

**P-1 lookahead 转向（`toolResultsFor` / `positionalToolResults`）——最高风险的一处，已核实安全。**
- 旧 `toolResultsFor` 无条件扫 `steps[i+1].Rec` 的**整段 resent 历史**再按 call-id 匹配；新实现只遍历
  `steps[i+1].NewToolResults`（该步 delta 引入的 tool result）。
- 协议保证（F9）：一轮要推进，前一轮的挂起 tool call 必先被 answer，故 answer 必然出现在下一步的 delta 里。
  三套现有测试（`findings_toolresult_test` / `render_spine_test` / `structure_test`）全绿，等价性成立。
- 一个理论边缘：若某 Step 的多个 call 被拆到 i+1、i+2 两步才答完，`toolResultsFor` 从来只看 i+1，新旧行为
  一致；若 call-id 被跨步复用且 i+1 的 resent 历史里含陈旧同名结果，旧版会误匹配、新版不会——**新版更正确**。
  低危。

**`searchableTranscript` 顺序变化**（`llm_findings.go` + `_eval/calibrate_p1b.go`）。
- 产物是「文本池」（做 anchor 子串校验），不依赖追加顺序；旧版按 step 顺序内联 marshal，新版先写全部文本、
  再由 `ForEachRecord` 并发 marshal。P-3 决策明说「保持原文本域不变（不做收窄）」，实现确实保留了
  `json.Marshal(rec)` 全集，且把回捞失败按 fail-open 处理（anchor 校验失败则丢弃，不误信）——与现有宽容度一致。
- `_eval/calibrate_p1b.go` 的 `transcriptPool` 同步改为 `ctxgraph.FetchRecords` 重取，经 `archtest` 编译门
  验证可编译（`TestArchitecture_EvalToolsCompile` 通过）。低危。

**guard 从 `s.Rec != nil` 改为 `s.Manifest != nil`**（`render_html.go:165` / `render_html_dashboard.go:57`）。
- 生产路径 `buildStep` 同时设置 Manifest 与（已删的）Rec，不存在「Rec 有、Manifest 无」的 Step；守卫语义等价。
  低危。

### 5.3 低危观察

- **`.gitignore` 新增 `/sample_reports/`** — 与本次内存优化无直接关系，属范围外改动。若是本批次的产物目录
  应说明来源；否则考虑单独提交。建议确认其归属。
- **`batchByBytes` 的退化输入**：`budget <= 0` 或某链 `Manifest.Bytes == 0` 时会退化（每链自成一批，或
  欠计批次）。v3 cache 已保证 `Bytes > 0`（`scanFile` 对每条 manifest 设置），且 `TestScanFile_PopulatesManifestBytes`
  覆盖，故正常路径不触发。可加一句注释说明「Bytes 为 0 是前 v3 缓存（已失效）才可能」，成本一行。
- **`ForEachRecord`/`FetchRecords` 错误聚合只返回首个非 nil 错误**，与 `FetchRecords` 原有行为一致，
  文档未改动。低危。
- **`renderBatchBudgetBytes = 160 MiB`** 靠实测校准（512→256→160），注释已诚实说明是「measured to hold
  RSS under ~2GB」，未伪装成有原则的公式——符合项目「不写非理性常数」的取向，可接受。

---

## 6. 文档同步核查

| 文档 | 状态 |
| --- | --- |
| `KNOWN_ISSUES` 1.2 | ✅ 已补 story 半边 `Step.Rec` 曾为主因 + 实测 43GB→约 2.4GB + 已解决；report 半边保留。 |
| `KNOWN_ISSUES` 1.3 | ✅ 结案为「决定不做」，引用实测 1.40x 放大。 |
| `KNOWN_ISSUES` 新增 1.55 / 1.56 | ✅ 全流式 `BuildAll` / 三遍解压，均登记待触发（触发条件写清）。 |
| `KNOWN_ISSUES` §0 计数 | ✅ 低危 18→20，合计 21→23，加 2026-08 已闭环行。 |
| 设计文档「源坐标与按需回捞」 | ✅ 补 `ForEachRecord`、`Manifest.Bytes`、「不得被长寿命结构持有」。 |
| 设计文档「模型使用与切换」 | ✅ `Step.Rec.Attempts` → `Step.Attempts`。 |
| 设计文档「`vmr story -corpus`」 | ✅ 改为按字节预算分批。 |
| 设计文档「`Build`/`BuildChain`/`BuildAll`」 | ✅ 补「提取事实后丢弃 + `BuildAllWithRecords`」。 |
| `CHANGELOG.md` `[Unreleased]` | ✅ 3 条 `Changed`。 |
| 设计文档残留 `Step.Rec` / `renderBatchSize` | ✅ grep 无残留（仅 `journey_stepfacts.go` 的 4 处注释提及历史）。 |

**两处口径不精确（已被 Action Plan 部分纠正，但 RootCause 正文仍未修）**：
- RootCause §1.2 写「44 文件 / 700 MB 压缩 / 13.00 GB / 压缩比 18.6x」。实际仓库是 **43 文件 / 235 MB 压缩 /
  ~12.4 GB / 约 53x**（Action Plan M0 已纠正，说 700MB 疑似含 `logs/loadtest/`）。RootCause 正文数字仍偏，
  但方向与量级无碍。
- RootCause §5.2/§6.4 预测动作 A+B+C 后「约 1 GB」，Action Plan M6.2 实测全量 `-corpus` 为 **2.41 GB**、
  `-render-all` 为 **1.91 GB**，并把验收从「< 2 GB」调整为「< 3 GB」。此调整诚实且已记录，但 RootCause 的
  「1 GB」预测偏乐观（峰值并非由 batch transient 主导，而是「约 300 MB 叙事常驻 + Scan manifest + cache +
  zstd worker 缓冲 + Go heapSys 未归还」等结构性项）。这不影响结论，但 RootCause 正文值得追加一句实测回填。

---

## 7. 优化空间（验证哪些成立、哪些不成立）

| RootCause 声称 | 复核 |
| --- | --- |
| 「`Body any`→`RawMessage` 最多省 29%，不改量级，不值得」 | ✅ 认可。实测放大 1.40x，方向正确。 |
| 「按自然日分桶释放」依赖「时间单调递增」隐蔽前提，不成立即静默算错 | ✅ 认可，动作 A 无需此前提。 |
| 「`GOMEMLIMIT` 回收不了存活对象，不能当止血」 | ✅ 认可。 |
| 「全流式 `BuildAll` 改动量一个量级，收益仅 1GB→0.3GB，不建议现在做」 | ✅ 认可，已登记 1.55。 |
| 「`Manifest` 携带 delta 正文、消灭第二遍解压」要动跨包契约，等时间成为瓶颈再做 | ✅ 认可，已登记 1.56。 |
| 「`corpusStats` 不需要在线累加器，修根因后 586 Journey 仅 288MB」 | ✅ 认可，实测子集常驻叙事结构远小于批量瞬时。 |
| 批次按体积而非数量 | ✅ 认可。实测单 Journey 体量方差确实达两个数量级。 |

没有发现能大幅压过当前方案的优化空间。若未来要再压到 1 GB 量级，唯一路径是 1.55 的全流式 `BuildAll`，寄存器即可。

---

## 8. 综合结论

**这个内存问题已从根本上解决，不是治标。** 四项判据全部成立：

1. **根因消除**：`Step` 不再持有 `*audit.Record`，O(N²) 的重复原始字节不再被钉在对象图里，改为提取后即弃
   （与 report 半边同一内存模型）。
2. **实测验证**：同子集下 footprint 4.7x（`-corpus`）/ 4.5x（`-render-all`）下降；产物结构 diff 0 差异；
   `golden_test` 字节一致；旧版线性 ramp 到 43 GB、新版压到 2.4 GB（全量，Action Plan M6 实跑）机制吻合。
3. **正确性**：`go test ./... -race` 全绿，`_eval` 编译门过，archtest 预算数字一个没调大（拆文件而非抬数字）。
4. **文档同步**：设计文档、`KNOWN_ISSUES`、`CHANGELOG` 均已更新，无 `Step.Rec` / `renderBatchSize` 残留。

**建议合并前处理的仅 3 件小事（都不阻塞，但让项目更自洽）**：
- 把 Action Plan M2.1 的 `AttemptFact` 定义收窄为 `{Provider, Model}`（或补一句「其余字段实测无消费方」）。
- 给 RootCause §5.2/§6.4 回填全量实测（2.41 GB / 1.91 GB），并把验收从「< 2 GB」更新为「< 3 GB」。
- 确认 `.gitignore` 的 `/sample_reports/` 是否属于本批次；若无关，建议单独提交并说明来源。

---

## 附录：本次评审实跑命令记录

```
# 新二进制（当前分支）
go build -o vmr ./cmd/vmr
_tmp/membench.sh corpus /tmp/vmr-review-subset-A logs/vmr-audit-2026-07-15.jsonl.zst logs/vmr-audit-2026-07-18.jsonl.zst logs/vmr-audit-2026-07-19.jsonl.zst
  → RSS 1083031552 (1.08 GB), footprint 1076987104, 56.58s
_usr/bin/time -l ./vmr analyze -story-only -render-all -o /tmp/vmr-new-render <same 3 files>
  → RSS 1068498944 (1.07 GB), footprint 1063109880, 65.31s

# 旧二进制（a73b8d3，git worktree 另建目录，未碰工作区）
cd /tmp/vmr-old-binary && go build -o /tmp/vmr-old ./cmd/vmr
/tmp/vmr-old analyze -corpus ...  → RSS 3386753024 (3.39 GB), footprint 5068181488 (5.07 GB), 53.37s
/tmp/vmr-old analyze -story-only -render-all ... → RSS 4388814848 (4.39 GB), footprint 4726312752 (4.73 GB), 65.00s

# 结构与测试
python3 结构 diff（41 json，去字符串）= 0 差异
go test ./... ./internal/archtest/... (with -race on key pkgs) 全绿
```
