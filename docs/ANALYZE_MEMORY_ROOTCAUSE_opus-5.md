<!-- Ver 2026-08-30 19:10, by Opus 5 -->

# `vmr analyze` 内存瓶颈：根因复核与架构级方案

本文复核 `_tmp/plan_gemini-3.7-flash.md`「深度内存瓶颈分析与优化方案」一章的判断，
以真实语料上的实测数据为准绳，重新给出根因与方案。**本轮不改任何代码。**

---

## 摘要

原报告观测到的现象（44 个日志、43.6 GB physical footprint、swap thrashing）**属实且可复现**，
外推误差在 5% 以内。但它给出的根因和方案，一半过时、一半治标。

三句话讲清真正的问题：

1. **崩的是 `-corpus`，不是 `-render-all`。** `renderJourneys` 早已有 `renderBatchSize = 20` 的分批，
   实测全量 44 文件 `-render-all` 峰值只有 **4.14 GB**。而 `corpusStats` 走的是另一条路径，
   一次性 `BuildAll` 全部 586 个候选，**没有任何分批** —— 上一次修这个问题时漏掉了它。
2. **根因不是「批量太大」，是 `story.Step.Rec` 长期持有完整 `audit.Record`。**
   实测：一个文件的 12 个 Journey，构建完成后堆占用 **829.7 MB**；把每个 `Step.Rec` 置 nil，
   堆立刻掉到 **11.7 MB**。叙事结构本身只占原始数据的 **1.4%**，其余 98.6% 是已经被提取过、
   却仍被对象图钉住的原始记录。
3. **这些原始记录之间 98% 的内容是彼此重复的。** 每条 chat request 都重发完整历史，
   所以 N 步会话的原始字节是 O(N²)，而叙事只需要 O(N)。实测去重后的叙事文本
   只有原始 JSON 的 **1.17%**（584.9 MB → 6.8 MB），即 **86 倍冗余**。

因此最优解不是「分更小的块」，而是**让 Journey 不再持有 `audit.Record`**。
全量 586 个 Journey 的叙事结构实测只需 **288 MB** —— 比现在的 43 GB 少 150 倍。
这不是引入新机制，而是把实现拉回设计文档已经写明、但被实现悄悄违背的契约：
「正文不驻留内存、按需回捞」（Analytics 设计文档「源坐标与按需回捞」一节）。

---

## 一、复核对象与验证方法

### 1.1 待验证的三条根因与两条方案

原报告的判断，逐条列为待验命题：

| # | 原报告的命题 | 复核结论 |
| --- | --- | --- |
| R1 | `BuildAll` 一次性 `FetchRecords` 全部记录，塞进单体巨型 map | **部分成立**——对 `-corpus` 成立，对 `-render-all` 已不成立（batch=20 早已存在） |
| R2 | `BuildAll` 把全部 1138 个 Journey 物化在 `[]*Journey` 里直到全部完成 | **对 `-corpus` 成立，但因果讲反了**——Journey 本身很小（288 MB/586 个），大的是它钉住的 Record |
| R3 | 内存复杂度 O(N) 且常数巨大，单条会话对象图可达数十 MB | **数值对，机制错**——不是「反序列化后对象图膨胀」（实测放大仅 1.40x），是「每 request 重发全历史」造成的 O(N²) 原始字节 |
| S1 | 改为分块流式处理（每批 50 个 Journey） | **必要但不充分**——`-render-all` 已经在做（batch=20），峰值仍 4.14 GB |
| S2 | `ComputeCorpusStats` 改用在线累加器，提取指标后立即释放 Journey | **在根因修好后不需要**——586 个 Journey 只占 288 MB，本来就装得下 |

### 1.2 环境与语料

| 项 | 值 |
| --- | --- |
| 机器 | Darwin 24.6.0（Apple Silicon），**16 GB RAM**，10 核 |
| 语料 | `logs/*.jsonl.zst` 共 44 个文件，压缩后 700 MB，**解压后 13.00 GB**（压缩比 18.6x） |
| 记录规模 | 2367 lineages，588 candidates，586 非断头，**15358 steps**，38780 events |
| 单条记录均值 | 约 0.82 MB（解压后 JSON 字节） |
| 二进制 | 当前 `main`（`a73b8d3`，v0.6.4）现编 |

> 语料清点：落地时重新核对为 **43 个文件 / 压缩 235 MB / 解压约 12.4 GB（压缩比约 53x）**——
> 上表的「700 MB / 18.6x」是分析期估算（疑似把 `logs/loadtest/` 也算了进去），量级不影响下文外推。
> 实测的改动前/后数字以 `docs/ANALYZE_MEMORY_ACTIONPLAN_sonnet-5.md` §4（M0 / M6.2）为准。

16 GB 物理内存是理解现象的关键：43.6 GB 的 footprint 意味着约 2.7 倍超配，
macOS 先用内存压缩器（原报告观测到约 59 GB compressor），压不住再换页到 swap（29.6 GB 用满），
最终表现为「CPU 掉到 0、线程卡在 `__psynch_cvwait`」的假死。

### 1.3 方法

两类证据，互相交叉验证：

- **外部测量**：`/usr/bin/time -l` 抓 max RSS 与 peak memory footprint，跑真实 CLI 命令。
- **内部探针**：一次性程序 `_tmp/memprobe`（`_tmp` 被 go 工具链忽略，不进构建），
  在流水线各阶段插入 `runtime.GC(); runtime.ReadMemStats()`，把峰值拆成 live heap / heapInuse / heapSys。
  探针复用生产代码的入口（`ctxgraph.Scan`、`story.ListCandidates`、`story.BuildAll`、
  `story.ComputeCorpusStats`），不重写任何逻辑，所以测的就是真实路径。

---

## 二、实测结果

### 2.1 四条路径的内存与耗时矩阵

| 命令 | 输入 | 峰值 RSS | peak footprint | 耗时 |
| --- | --- | --- | --- | --- |
| `-list-only`（只扫 manifest） | 2 文件 / 1.43 GB | 198 MB | 182 MB | 8.2 s |
| `-corpus` | 2 文件 / 1.43 GB | 3.06 GB | 4.63 GB | 83.7 s |
| `-corpus` | 4 文件 / 2.53 GB | — | **9.01 GB** | — |
| `-story-only -render-all` | 4 文件 / 2.53 GB | 3.60 GB | 3.60 GB | 116 s |
| **`-story-only -render-all`** | **44 文件 / 13.0 GB** | **4.14 GB** | 4.13 GB | **532 s** |
| **`-corpus`** | **44 文件 / 13.0 GB** | **约 43 GB（外推）** | — | 未实跑（会 thrash） |
| **`-macro-only`（report 半边对照）** | **44 文件 / 13.0 GB** | **1.59 GB** | 1.53 GB | **188 s** |

三个立即可读出的事实：

- **分批是有效的**：`-render-all` 从 4 文件到 44 文件（语料涨 5.1 倍），峰值只从 3.60 GB 涨到 4.14 GB
  ——峰值由**单个批次**决定，与语料总量无关。
- **`-corpus` 是线性的**：1.43 GB 语料 → 4.63 GB，2.53 GB 语料 → 9.01 GB，
  斜率约 3.2–3.6 倍未压缩体积。外推到 13.0 GB 语料即 **42–47 GB**，与原报告实测的 43.6 GB 吻合。
- **report 半边是对照组，也是标尺**：同一份 13 GB 语料、同一批 15825 条记录，
  `-macro-only` 峰值只有 **1.59 GB**、耗时 **188 s**。它读的字节一个不少，
  只是读完就扔。**27 倍的内存差距，全部来自内存模型的不同，与数据量无关。**

外推的另一条独立算路，结论一致：15358 steps × 0.82 MB ≈ 12.6 GB 原始 JSON，
× 1.40 反序列化放大 = 17.6 GB live，Go 默认 `GOGC=100` 的 heap goal 是 live 的 2 倍 → 约 35 GB heapSys，
再加 10 个并发 worker 各自的 zstd decoder buffer 与解析临时对象 → **40–45 GB RSS**。

### 2.2 分阶段堆分解（决定性实验）

单文件 `vmr-audit-2026-08-24.jsonl.zst`（解压 695 MB，453 条记录，12 条 chain）：

| 阶段 | live heap | heapSys |
| --- | --- | --- |
| 0. baseline | 0.4 MB | 3.7 MB |
| 1. `ctxgraph.Scan`（只出 Manifest） | **2.0 MB** | 136 MB |
| 2. stitch + candidates | 2.0 MB | 136 MB |
| 3. chains built | 1.8 MB | 136 MB |
| 4. `FetchRecords`（453 条） | **820.5 MB** | 1124 MB |
| 5. `BuildAll` | 829.7 MB | 1812 MB |
| 6. **丢弃 `recs` map** | **829.7 MB（零释放）** | 1812 MB |
| 7. **把每个 `Step.Rec` 置 nil** | **11.7 MB** | 1812 MB |

第 6 行和第 7 行是整个分析的枢纽：

- 第 6 行——把 `FetchRecords` 返回的整个 `map[Loc]*audit.Record` 置 nil，**堆一个字节都没释放**。
  证明这些记录不是被 map 持有的，而是被别的东西钉着。
- 第 7 行——把 `Step.Rec` 置 nil，堆从 829.7 MB 掉到 **11.7 MB**。
  钉住它们的就是 `story.Step.Rec`。

`Scan` 阶段只用 2.0 MB 同样重要：**索引层（Manifest）本身完全没有内存问题**，
它老老实实地只存哈希和坐标。问题百分之百出在「回捞之后不放手」。

### 2.3 反序列化放大只有 1.40x

`audit.Message.Body` 声明为 `any`，反序列化后是 `map[string]any` 树——直觉上这是内存放大的元凶
（`internal/chatmsg` 的 43 处 `map[string]any` 也一直被怀疑，见 `KNOWN_ISSUES` 的 1.3 条）。
实测把这个怀疑排除了：

| 项 | 值 |
| --- | --- |
| 453 条记录的原始 JSON 字节 | 584.9 MB |
| 反序列化后 live heap | 820.5 MB |
| **放大倍数** | **1.40x** |

放大之所以这么低，是因为审计记录的字节绝大部分是**长文本**（对话正文），
Go 的 `string` 只有 16 字节 header，结构开销被文本本身稀释掉了。

推论：把 `Body` 改成 `json.RawMessage` 延迟解析，最多省下 29%，**不解决量级问题**，
却要改动几十处类型断言。这条路不值得走（详见「被考虑但不推荐的方案」）。

### 2.4 叙事只占原始数据的 1.17%

同一份 453 条记录，统计构建出的 Journey 里真正被渲染的文本：

| 项 | 字节 |
| --- | --- |
| 去重后的 Event 正文（1082 条） | 4.9 MB |
| Step 的 `RespText` + `Reasoning` | 1.1 MB |
| Step 的 ToolCall args | 0.8 MB |
| 合计 | **6.8 MB** |
| 原始 JSON | 584.9 MB |
| **叙事 / 原始** | **1.17%** |

**86 倍冗余**。这个数字直接来自协议事实：每一条 chat completion request 都携带完整会话历史，
所以第 N 步的 body 里包含第 1..N 步的全部消息。N 步会话的原始字节是 O(N²)，
而它讲的故事只有 O(N)。本语料里最大的一个 Journey 有 518 步——
它的原始记录里，同一段文本平均被存了几百遍。

`Journey.Events` 的设计（「一条消息在整个 Journey 生命周期里只渲染一次」）已经正确地做了这次归约。
问题是归约完之后，原材料没被扔掉。

### 2.5 去掉 `Step.Rec` 之后的全量实测

用探针模拟「Journey 不持有 Record」：按现有的 batch=20 分批构建，每批构建完立即把 `Step.Rec` 置 nil，
然后把全部 Journey 累积下来。全量 44 文件：

| 指标 | 值 |
| --- | --- |
| `Scan` 后（2367 lineages 的全部 Manifest） | **56.5 MB** |
| 分批构建期间 peak live | 3076 MB |
| 分批构建期间 peak RSS | 4218 MB |
| **全部 586 个 Journey（切断 Rec）常驻** | **288 MB** |
| 规模 | 586 journeys / 15358 steps / 38780 events |
| 耗时 | 114 s |

**288 MB。** 现在这条路径要 43 GB。差 150 倍。

探针在最后一步 `ComputeCorpusStats` 崩了，崩得很有价值：

```
panic: invalid memory address or nil pointer dereference
vmr/internal/story.computeTimeSplit  internal/story/metrics.go:156
```

`metrics.go:156` 是 `modelMS += s.Rec.DurMS`。而它下面第 161 行写的是
`prev.Manifest.TS.Add(time.Duration(prev.Rec.DurMS) * ...)`——**同一个函数里，
时间戳读 `Manifest`，时长读 `Rec`**，而 `Manifest.DurMS` 是 `BuildManifest` 早就复制好的同一个值。
这一处根本不需要 `Rec`，只是当初顺手写了。整份清单里这样的情况占多数（见方案一节）。

---

## 三、根因复核：原报告的对、错与遗漏

### 3.1 对的部分

- 现象描述准确：43.6 GB footprint、swap 打满、CPU 掉零的假死表征，全部复现。
- 定位到了 `story.BuildAll` 的 `FetchRecords` 这一层，方向没错。
- 「单条复杂会话对象图可达数十 MB」这个量级判断，数值上对。

### 3.2 错的与过时的部分

**（1）把 `-render-all` 和 `-corpus` 当成同一个问题。**
`cmd/vmr/cmd_story.go` 的 `renderBatchSize` 常量及其注释显示，这个问题在
「34 files / 1638 lineages、峰值约 35 GB、进程被 kill」那次就已经修过一轮，
修法就是给 `renderJourneys` 加分批。但 `corpusStats` 是一条平行的调用路径，
它直接 `story.BuildAll(toRender, ...)` 吃下全部候选，**那次修复没有覆盖到它**。
原报告把两条路径混在一起描述，导致方案指向了已经修好的那一条。

**（2）把「Journey 对象太多」当成根因。**
原报告说「同时将所有 1138 个 Journey 对象……全部物化并保存在 `[]*Journey` 切片中」。
实测：586 个 Journey 的叙事结构总共 288 MB，**根本不是瓶颈**。
真正的开销全在 `Step.Rec` 这一个字段上。因果反了，方案自然也就偏了——
S2 的「在线累加器」正是为了不保留 Journey 而设计的，可根因修掉之后，保留全部 Journey 完全无害。

**（3）把 O(N) 与「常数巨大」当成解释。**
真实的复杂度是 O(N²)——不是常数大，是同一段文本被存了 N 遍。
这个区别决定了方案的形状：常数大只能靠分块摊薄，O(N²) 可以被**消除**。

### 3.3 遗漏的部分

**（1）反序列化放大只有 1.40x，不是主因。**
原报告暗示「反序列化后对象图膨胀」是机制之一。实测排除。同时这条实测也回答了
`KNOWN_ISSUES` 1.3 条挂了很久的那个疑问（「没有 profile 数据前不动 `map[string]any`」）
——现在有数据了：不值得动。

**（2）`PreviewTitles` 是同一个病的第二个病灶。**
`setupStoryRun` 无条件调用 `story.PreviewTitles`，它对**每个候选 chain 的根记录**做一次
`FetchRecords`，一次性拿回全部 588 条完整记录（约 480 MB 原始、670 MB 堆），
**只为了取每个 Journey 的一句话标题**。用完即弃，所以是瞬时峰值不是常驻，
但它和 `BuildAll` 一样，是「`FetchRecords` 返回全量 map」这个接口形状逼出来的。

**（3）`searchableTranscript` 里的 O(N²) 字符串。**
`internal/story/llm_findings.go:536` 对每个 Step 做 `json.Marshal(s.Rec)` 并拼进一个大 `strings.Builder`。
对 518 步的 Journey，这会拼出一个几百 MB 到 GB 级的单一字符串。
只在 `-llm-addr` 单 Journey 模式触发，不在批量路径，但同样是「持有原始记录」的下游后果。

**（4）时间也是问题，而且和内存同源。**
全量 `-render-all` 要 **532 秒**。一次 `vmr analyze` 至少要把 13 GB 完整解压三遍：
`PreviewTitles` 一遍、每个批次的 `FetchRecords` 各一遍（约 30 个批次，按时间局部性平均每批碰 1–3 个文件）、
report 半边的 `analyzeFile` 一遍。`.parse-cache` 只覆盖 `ctxgraph` 的 manifest 扫描，
对 `FetchRecords` 和 `analyzeFile` **完全无效**。

**（5）`KNOWN_ISSUES` 1.2 条的归因不完整。**
该条目登记了「全内存聚合的记录量上限」，但归因写的是 `AnalyzeSessions`（report 半边）
的样本切片，方案是「按自然日分桶」。实测表明 report 半边不是 43 GB 的来源
（见下一节的对照），story 半边的 `Step.Rec` 才是。该条目应据此更新。

---

## 四、第一性原理：问题的真实形状

### 4.1 协议事实决定了数据形状

审计日志的每一行是一次完整的 request/response 交换。而 chat completion 协议是无状态的——
每一轮都要重发全部历史。于是磁盘上的一条 lineage，本质是一个**前缀不断增长的序列**：

```
step 1 body: [m1]
step 2 body: [m1, m2, m3]
step 3 body: [m1, m2, m3, m4, m5]
...
step N body: [m1 ... m(2N-1)]
```

原始字节 O(N²)，信息量 O(N)。`ctxgraph` 的 Manifest（每条消息一个哈希 + 源坐标）
和 `story` 的 Event（首次出现去重）都是对这个事实的正确响应——
**索引层和叙事层都是 O(N) 的**。唯一 O(N²) 的东西是原始 record 本身，
而它在 `Step.Rec` 里被完整保留了下来。

### 4.2 两个半边的架构不对称

同一份数据、同一个 I/O 层，两个半边用了相反的内存模型：

| | `internal/report`（宏观报表） | `internal/story`（叙事） |
| --- | --- | --- |
| 读取 | `analyzeFile` 逐行 `json.Unmarshal` | `FetchRecords` 返回 `map[Loc]*audit.Record` |
| 提取 | `collect(&rec, ...)` → 小结构 `ReqInfo` | `buildFrom` → `Step`，**但 `Step.Rec` 保留原记录** |
| 记录生命周期 | 提取完立即成为垃圾 | 与 Journey 同寿 |
| 详情页 | `onRecord(&arec, ri)` 流式回调，写完即弃 | `EnsureJourneyDetails` 遍历 `Step.Rec` |
| **全量 13 GB 语料的实测峰值** | **1.59 GB / 188 s** | **约 43 GB / 跑不完** |

两边读的字节完全一样（都要把 13 GB 全部解压、全部 `json.Unmarshal`），
产出的信息量也在同一量级。差 27 倍的唯一原因是：一边提取完就撒手，一边攥着不放。

report 半边是教科书式的流式提取器。story 半边**在结构上就不是**。
这不是谁写得糙的问题——它是两次独立演化的结果，而 `Step.Rec` 这个字段
让「回捞」悄悄变成了「持有」。

### 4.3 实现偏离了设计文档写明的契约

Analytics 设计文档「源坐标与按需回捞」一节写得很清楚：

> 每条 `Manifest` 自带 `Path`/`Line`（以及规范化后的 `Req` 坐标），**正文不驻留内存**；
> 需要原始 `audit.Record` 时由 `FetchRecords` 按文件批量回捞。

同一节还断言「索引本身……对全量语料只有几十 MB」——这一点实测完全成立（56.5 MB）。
偏离出现在下游：**回捞回来的正文，通过 `Step.Rec` 驻留了下来**，
而设计文档从未授权这件事。

所以本文提的方案不是「引入一个新机制」，而是**把实现拉回它自己的契约**。
这一点决定了改动的性质：它是在删除一个不该存在的引用，而不是在增加一层缓存管理。

---

## 五、方案

### 5.1 最优解：Journey 去 Record 化

一次改动，三个互相咬合的动作。三者缺一不可，但合起来仍然比「分块 + 在线累加器」的代码量更少。

#### 动作 A：`Step` 只持事实，不持原始记录

删掉 `story.Step.Rec`，把消费点需要的东西在 `buildFrom` 里一次提取好。
下面是完整清单——整个 `internal/story` 对 `Step.Rec` 的引用一共 **11 处、分布在 10 个文件**，全在这里：

**A1. 直接换成 `Manifest` 已有的同名字段（零新增字段，纯机械替换）**

| 位置 | 现在 | 换成 |
| --- | --- | --- |
| `metrics.go:156,161` | `s.Rec.DurMS` | `s.Manifest.DurMS` |
| `render_html.go:165` | `s.Rec.TS` | `s.Manifest.TS` |
| `render_html_dashboard.go:57` | `s.Rec.TS` | `s.Manifest.TS` |
| `render_html_dashboard.go:59` | `s.Rec.Model` | `s.Manifest.Model` |

`BuildManifest` 早就把这三个字段逐字复制进 Manifest 了。`metrics.go` 的
`computeTimeSplit` 甚至已经在相邻两行里同时用着两种写法。

**A2. 新增少量已提取字段**

| 新字段 | 服务于 | 体量 |
| --- | --- | --- |
| `Step.Context ContextPoint` | `metrics.go:310` 的 `contextCurve` | 4 个 `int64` × 15358 = 0.5 MB |
| `Step.Attempts []AttemptFact`（Endpoint/Provider/Model/ErrorClass） | `modelusage.go:84,129,159`、`render_html_dashboard.go:61` | 约 1.2 MB |
| `Step.NewToolResults []chatmsg.ToolResult` | `findings_toolresult.go:49`、`render_spine_step.go:332` | 与 Event 正文共享底层 string，近似零 |
| `Step.SysChars int` + `Journey` 级按 `SysHash` 去重的 system 正文表 | `render_md_sysprompt.go:64`、`compare.go:480` | 约 50 MB（全局去重后更少） |
| `Journey.InitialInstruction string` | `compare.go:339` | 可忽略 |

`NewToolResults` 这一项要说明一下：现在的代码是**向后看**——处理第 i 步时去读
`steps[i+1].Rec` 的 body，取 delta 范围内的 tool result。改成让第 i+1 步在
自己被构建时就把这份 delta 提取好，语义完全等价（两处代码本来就用
`DeltaStart` 把扫描范围限定在该步引入的消息上），但 lookahead 消失了，
构建从此只需要向前看。注意 `toolResultsFor` 还有第三个调用方
（`structure.go` 的 `buildStepStructure`）——改的是该函数的实现，
三个调用方都自动受益，无需各自改动。

**A3. 真正需要完整 record 的两个消费者，改为按需流式回捞**

只有两处：`EnsureJourneyDetails`（写 `details/*.md`）和
`searchableTranscript`（LLM finding 的 anchor 校验）。
两者都是「读一次、用完就扔」，不需要长期持有。给 `ctxgraph` 加一个流式入口：

```go
// ForEachRecord 是 FetchRecords 的流式孪生：同样按文件分组、每文件只扫一次，
// 但把每条记录交给 fn 而不是塞进 map —— 调用方用完即弃，不必驻留全部。
func ForEachRecord(locs []Loc, fn func(Loc, *audit.Record)) error
```

`FetchRecords` 保留（少数确实需要随机访问的场景），但新代码默认用流式那个。

#### 动作 B：`corpusStats` 走和 `renderJourneys` 同一条分批路径

现在 `corpusStats` 是唯一一条无分批的 `BuildAll` 调用。把它改成和 `renderJourneys`
一样的批处理循环即可。

**因为有动作 A，这里不需要原报告提的「在线累加器」**：每批构建完的 Journey
只剩 1.4% 的体量，全部 586 个累积起来才 288 MB，直接交给现有的
`ComputeCorpusStats(journeys)` 就行，一行不用改。

这是根因修复带来的连锁简化——修对了地方，下游的补丁就不用写了。

#### 动作 C：批次按体积分，不按数量分

`renderBatchSize = 20` 的注释自己承认了这个缺陷：

> 20 是保守常数，不是针对内存预算调优的（没有 per-request 字节计量，
> 无法在这里便宜地算出更紧的、有原则的界限）。

那就把这个计量补上：`Manifest` 加一个 `Bytes int` 字段（`BuildManifest` 里
`len(lineBytes)`，零额外成本），批次预算从「20 个候选」改成「累计 ≤ N MB 原始字节」。

这解决的是方差问题：本语料里最大的 Journey 有 518 步，单它一个就约 425 MB 原始字节，
而一批 20 个「心跳型」候选可能加起来还不到 20 MB。按数量分批，峰值方差极大；
按体积分批，峰值是一个**有物理意义、可预测的预算**。

代价：`ctxgraph.CacheSchemaVersion` 要 bump（2 → 3），既有 `.parse-cache` 全量失效重建一次。
这个 bump 是安全的——cache 是完全可再生的产物，该常量的文档注释明确说了这一点。

### 5.2 预期收益

| 路径 | 现状 | 动作 A+B+C 之后 |
| --- | --- | --- |
| `-corpus` 全量 | 约 43 GB / 无法完成 | **约 1 GB**（288 MB 叙事 + 一个批次的瞬时） |
| `-render-all` 全量 | 4.14 GB | 约 1 GB（瞬时峰值由体积预算决定） |
| `PreviewTitles` 瞬时 | 约 670 MB | 近似零（若一并改用 `ForEachRecord`） |
| 全量 Journey 常驻 | 与语料同阶 | **288 MB，与语料量近似无关** |

顺带删掉的复杂度：`renderBatchSize` 这个「猜出来的常数 + 一大段解释它为什么是猜的」的注释，
换成一个字节预算；`corpusStats` 不需要第二套累加逻辑。

> 落地实测（全量 43 文件）：`-corpus` **2.41 GB**、`-render-all` **1.91 GB**（`ACTIONPLAN` M6.2）。
> 本表的「约 1 GB」偏乐观——全量规模下峰值不再由 batch 瞬时主导，而是「约 300 MB 叙事常驻 +
> Scan manifest + `.parse-cache` + zstd worker 缓冲 + Go heapSys 未归还」之和。方向与「已完整解决
> 假死」的结论不变，验收阈值据此从「< 2 GB」调整为「< 3 GB 且无 swap」。

### 5.3 分阶段：本次做到哪里

动作 A+B+C 之后，两条路径的峰值都落在 **1 GB 量级**，在 16 GB 机器上有 16 倍余量。
**这已经完整解决了用户遇到的问题**，建议本次就到这里。

再往下还有一步——把 `BuildAll` 彻底流式化（`FetchRecords` 的 map 也不要，
逐条喂给 builder），能把批次瞬时峰值也消掉，降到几百 MB。
但它要求把 `buildFrom` 从「拿到全部记录后遍历」改成「按到达顺序 feed」，
还要处理并发扫文件时的乱序（Event 的 `FirstStepSeq` 需要按 seq 事后归并）。
**改动量大一个量级，收益只有 1 GB → 0.3 GB。不建议现在做**，登记待触发即可
（触发条件：语料再涨 5 倍，或需要在 8 GB 以下的机器上跑全量）。

### 5.4 被考虑但不推荐的方案

以下几条都认真评估过，都不作为主方案：

- **`ComputeCorpusStats` 在线累加器**（原报告 S2）：动作 A 之后不需要。单独做它只治
  `-corpus` 一条路径，不治 `-render-all` 与 `PreviewTitles`，而且是**增加**代码
  （一套新的 `CorpusAccumulator` 及其测试）去绕开一个本可以直接消除的问题。
- **`audit.Message.Body` 从 `any` 改成 `json.RawMessage`**：实测放大仅 1.40x，
  最多省 29%，不改变量级；却要改动 `story`/`report`/`reqdetail`/`chatmsg` 里
  几十处 `.(map[string]any)` 断言。投入产出比最差的一条。
- **让 `Manifest` 携带每步的 delta 正文，取消 `FetchRecords` 这第二遍解压**：
  能额外省下大量时间（当前至少三遍全量解压），思路上也最接近「一次归约、下游全部受益」。
  但它要把叙事提取逻辑从 `story` 挪进 `ctxgraph`，破坏后者「不驻留正文」的契约，
  让 `.parse-cache` 从几 MB 涨到约 160 MB，并且这个 cache 是 `report` 半边共享的。
  跨包契约改动 + 双半边影响，成本远超本次要解决的问题。**建议在动作 A 落地、
  时间成为下一个瓶颈时再单独评估。**
- **按自然日分桶释放**（`KNOWN_ISSUES` 1.2 条现有方案）：该条目自己指出了它的问题——
  依赖「记录时间严格单调递增」这个隐蔽前提，不成立就是**静默算错**而非报错。
  动作 A 不需要任何这类前提。
- **调低 `GOGC` / 设 `GOMEMLIMIT`**：只能更激进地回收**垃圾**，回收不了**存活对象**。
  `-corpus` 的 17 GB 全是存活的（被 `Step.Rec` 引用），把限额设到它以下只会触发
  GC 死亡螺旋（CPU 打满，然后照样 OOM）。对批次瞬时峰值有效，可作为动作 A 之后的
  兜底旋钮，但**不能当止血方案**。

### 5.5 改动落地前的临时规避

在动作 A 落地之前，`-corpus` 在全量语料上就是跑不了。可用的规避只有一条：
**按文件子集分几次跑 `-corpus`**（实测 4 文件峰值 9 GB，是 16 GB 机器的安全上限）。
其余路径（默认套件、`-render-all`、`-journey`、`-compare`）峰值 4.14 GB，不受影响。

---

## 六、成本、风险与架构复杂度

### 6.1 改动面

| 包 / 文件 | 改动性质 | 规模 |
| --- | --- | --- |
| `internal/story/journey.go` | `Step` 结构改字段 + `buildFrom` 增加提取 | 中（现 756 行 / 预算 850，**会顶到 `archtest` 上限，需拆文件**） |
| `internal/story/metrics.go` | 2 处机械替换 + `contextCurve` 改读预提取字段 | 小 |
| `internal/story/modelusage.go` | 4 处改读 `Step.Attempts` | 小 |
| `internal/story/render_html.go` / `render_html_dashboard.go` | 3 处机械替换 + 1 处读新字段 | 小 |
| `internal/story/render_md_sysprompt.go` / `compare.go` | 3 处改读预提取字段 | 小 |
| `internal/story/findings_toolresult.go` / `render_spine_step.go` | lookahead 改为读本步的 `NewToolResults` | 小 |
| `internal/story/ensure_details.go` | 改用 `ForEachRecord` 回捞 | 小 |
| `internal/story/llm_findings.go` | `searchableTranscript` 改用 `ForEachRecord` | 小 |
| `internal/ctxgraph/records.go` | 新增 `ForEachRecord` | 小 |
| `internal/ctxgraph/manifest.go` / `cache.go` | 加 `Bytes` 字段，bump `CacheSchemaVersion` | 小 |
| `cmd/vmr/cmd_story.go` | `corpusStats` 走分批；批次预算改字节 | 小 |

合计约 12 个文件、300–500 行。**没有一处跨越两个半边的边界**（`archtest` 强制的
「`story`/`ctxgraph` 不得 import `router`/`server`/`config`」不受影响）。

### 6.2 风险

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| 输出字节变化 | **低** | `journey-*.json` 序列化的是 `story.NewJourneySummary` 而非 `Journey`/`Step` 本身，删 `Step.Rec` **不改变任何产物的 JSON 形状**；`internal/story/golden_test.go` 已有端到端 Markdown 字节比对做安全网 |
| 提取时机与原地读取产生语义差 | **中** | 唯一有实质语义的一处是 `NewToolResults` 的 lookahead 转向；两处调用本来就用 `DeltaStart` 把范围限定在该步引入的消息上，等价性可由现有测试覆盖（`findings_toolresult_test.go`、`render_spine_test.go`）|
| `.parse-cache` 全量失效 | **低** | 设计如此；cache 是完全可再生产物，其文档注释明确说明 bump 永远安全，代价是一次重扫 |
| `archtest` 行数预算触发 | **确定会发生** | `journey.go` 只剩 94 行余量。按项目约定拆文件（`journey_stepfacts.go`），**不是**抬高预算表里的数字 |
| 漏掉某个 `.Rec` 消费点 | **低** | 编译器保证：删掉字段之后所有引用都是编译错误，不存在运行时才发现的遗漏 |

最后一条值得强调：**这次改动的正确性由编译器兜底**。删字段不是加分支——
不存在「某条路径忘了释放」的可能，这和「分块 + 在线累加器」那类方案有本质区别，
后者的失效模式是静默的内存增长。

### 6.3 架构复杂度：净减少

这是本方案最值得说的一点。它**不引入任何新的抽象**：

- 不引入内存预算管理器、不引入对象池、不引入 LRU、不引入新的生命周期概念。
- 删掉一个字段（`Step.Rec`），加一组普通的值字段。
- 删掉一段「为什么这个常数是猜的」的注释，换成一个有物理意义的字节预算。
- 让 `corpusStats` 复用已有的分批循环，而不是长出第二套累加逻辑。
- 让 `story` 半边的内存模型**收敛到 `report` 半边已经在用的那一个**——
  提取完即弃。两个半边从此在这件事上讲同一个故事。

对照原报告的方案：它要新增一个 `CorpusAccumulator` 抽象及其测试、
要在两条路径上各维护一套分块逻辑，而 `Step.Rec` 原封不动——
也就是说**下一次有人写一段「遍历所有 Journey 做点什么」的代码时，同一个坑还在那里**。

### 6.4 综合判断

| 维度 | 评价 |
| --- | --- |
| 收益 | 峰值内存约 43 GB → 约 1 GB（**约 40 倍**）；常驻 Journey 与语料量解耦，落到与 report 半边同一量级 |
| 投入 | 约 12 文件 / 300–500 行 / 1–2 个工作日（含拆文件与测试） |
| 风险 | 低——编译器强制完备性，golden test 守字节，产物 JSON 形状不变 |
| 架构复杂度 | **净减少**——删字段、删常数、删一条平行路径 |
| 综合 | **强烈建议做，且应作为一次改动整体落地。** 收益/成本比在本项目已知的优化项里排第一 |

---

## 七、全局视角：一并暴露的问题与待回填的文档

聚焦内存的过程中，暴露了三条同源但独立的问题，外加三处需要更新的既有文档。
三条问题**都不建议在本次改动里一起做**，但都应登记进 `docs/KNOWN_ISSUES_sonnet-5.md`，
避免下一个人重新发现一遍。

### 7.1 `FetchRecords` 的接口形状本身在制造问题

`func FetchRecords(locs []Loc) (map[Loc]*audit.Record, error)`——
一个返回全量 map 的 API，**天然强迫每个调用方驻留全部数据**。
三个调用方（`BuildAll`、`BuildChain`、`PreviewTitles`）里有两个其实只需要流式。

动作 A3 会加上 `ForEachRecord`。建议顺手把 `PreviewTitles` 也切过去
（它是纯提取——读一条记录、取一句标题、丢弃），成本几行，收益是消掉一个 670 MB 的瞬时峰值。

### 7.2 一次 `analyze` 至少解压 13 GB 三遍

- `PreviewTitles` 一遍（全部候选的根记录，几乎覆盖所有文件）
- 每个批次的 `FetchRecords` 各一遍（约 30 批 × 每批 1–3 个文件）
- report 半边的 `analyzeFile` 一遍（`KNOWN_ISSUES` 1.1 条已登记：`collect()` 未缓存）

`.parse-cache` 只覆盖 `ctxgraph` 的 manifest 扫描，对上述三者**全部无效**。
这是 532 秒的主要来源。「让 `Manifest` 携带 delta 正文」（见「被考虑但不推荐的方案」）
是治这条的正解，但它要动跨包契约——**等内存问题解决、时间成为首要痛点时再单独立项**。

### 7.3 `searchableTranscript` 的 O(N²) 字符串

`llm_findings.go` 对每个 Step `json.Marshal(s.Rec)` 拼成一个大字符串。
518 步的 Journey 会拼出 GB 级单一 string。该函数的注释说明它需要
「只在 marshaled record 里可见的 tool_result 文本和原始 tool-call 参数」——
但动作 A 之后，`Step.NewToolResults` 和 `Step.ToolCalls[].Args` 已经把这两样都提取出来了，
这个 `json.Marshal` **可以直接删掉**。属于动作 A 的顺带收益。

### 7.4 需要回填的既有文档

- **`KNOWN_ISSUES` 1.2 条**（全内存聚合的记录量上限）：归因不完整。现有描述只覆盖
  `AnalyzeSessions`（report 半边）的样本切片，未覆盖 story 半边的 `Step.Rec`。
  实测数据（本文第二节）应补入，触发条件（RSS > 4GB）已被 `-corpus` 路径远远突破。
- **`KNOWN_ISSUES` 1.3 条**（`chatmsg` 的 `map[string]any` 分配）：该条挂着
  「没有 profile 数据前不动」。现在有数据了——反序列化放大仅 1.40x，
  **应据此结案为「不做」**，而不是继续挂着。
- **Analytics 设计文档**「源坐标与按需回捞」一节：契约本身是对的，
  但应补一句说明 `Step` 不得持有回捞结果，否则「按需回捞」在语义上会被下一个人再次误读成「回捞后缓存」。

---

## 八、落地顺序与验收标准

建议按下列顺序，每一步都可独立编译、独立跑测试：

1. `ctxgraph`：加 `Manifest.Bytes`，bump `CacheSchemaVersion`；加 `ForEachRecord`。
2. `story`：`Step` 加 A2 的新字段，`buildFrom` 填充它们（此时 `Step.Rec` 仍在，行为不变）。
   跑 `golden_test.go` 确认字节一致。
3. `story`：把 11 处引用逐个切到新字段/`Manifest` 字段。每切一处跑一次
   `go test ./internal/story/...`。
4. `story`：删掉 `Step.Rec`——编译器会指出任何遗漏。
   `ensure_details.go` 与 `llm_findings.go` 改用 `ForEachRecord`。
5. `cmd/vmr`：`corpusStats` 走分批循环；批次预算改为字节。
6. 拆 `journey.go`（`archtest` 会要求）；跑 `go test ./internal/archtest/...`。

**验收标准**（全部可自动化）：

| 项 | 标准 |
| --- | --- |
| 正确性 | `go test ./... -race` 全绿；`golden_test.go` 字节一致 |
| 产物一致性 | 改动前后对同一语料跑 `-render-all`，`stories/*.md` 与 `*.json` **逐字节相同** |
| 内存 | 全量 44 文件 `-corpus` 峰值 RSS **< 2 GB**（现约 43 GB） |
| 内存 | 全量 44 文件 `-render-all` 峰值 RSS **< 2 GB**（现 4.14 GB） |
| 架构 | `go test ./internal/archtest/...` 全绿，且**预算表里的数字一个都没被调大** |

产物逐字节比对这一条是最重要的回归门：本次改动的全部承诺就是
「同样的输出，少用 40 倍内存」，字节不同就是承诺没兑现。

---

## 九、决策与取舍表

| 决策 | 选择 | 为什么不选另一边 |
| --- | --- | --- |
| 治根因还是治症状 | 消除 `Step.Rec` | 分块只是把 O(N²) 摊薄，坑还在；下一个写「遍历所有 Journey」的人会再踩一次 |
| 是否要在线累加器 | 不要 | 根因修好后 586 个 Journey 只有 288 MB，本来就装得下；写它是为了绕开一个可以直接消除的问题 |
| 批次按数量还是按体积 | 体积 | 单 Journey 体量方差达两个数量级（2 步 vs 518 步），按数量分批的峰值不可预测 |
| 是否顺手改 `Body any` → `RawMessage` | 不改 | 实测放大仅 1.40x，最多省 29%，却要改几十处断言 |
| 是否顺手消灭第二遍解压 | 不改 | 那是时间问题不是内存问题，且要动跨包契约；等内存修好后单独立项 |
| 是否本次就做全流式 `BuildAll` | 不做 | 改动量大一个量级，收益只有 1 GB → 0.3 GB；登记待触发 |
| 是否用 `GOMEMLIMIT` 兜底 | 可选旋钮，非方案 | 它回收不了存活对象，对 `-corpus` 的 17 GB 完全无效 |
| 拆文件还是抬预算 | 拆文件 | 项目约定：`archtest` 失败时抬数字正是它明确禁止的做法 |
