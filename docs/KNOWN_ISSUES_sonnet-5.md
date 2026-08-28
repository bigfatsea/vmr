<!-- Ver 2026-08-25 23:20, by gemini-3.7-flash -->

# vmr — Known Issues（已知问题与架构取舍清单）

> **定位**：本文档是 vmr 已知问题、待评估演进项与刻意架构取舍的**唯一权威、持续维护的当前状态清单**。发现新问题先在这里查一遍，再决定它是不是新的。
>
> **维护原则**
> 1. **只记当前事实**：记录代码库今天的真实状态与技术决策理由，不堆积「哪个批次改了什么」的过程流水账——那是 `CHANGELOG.md` 和 git history 的职责。
> 2. **区分「待办」与「取舍」**：
>    - §1 **待定问题**：确实有优化空间，但需要真实负载或触发条件出现后再排期；
>    - §2 **确定不修**：经评估主动选择不处理的设计取舍，连同 First Principles 决策逻辑一并记录，避免被反复重新提出；
>    - §3 **已闭环**：曾经成立、现已彻底解决的架构项，只留一句结论防止重复立项。
>    - §4 **ROI 评估**：给 §1 每条估成本/风险/价值，回答「先做哪个、哪个该继续等」。
> 3. **每条都要能对源码核实**。核实不了的，说明它已经过期，删掉。

---

## 0. 当前状态

- **稳定性与安全性**：无凭证泄漏、并发竞态或服务阻断级别的缺陷；单机生产环境可稳定运行。`copyFlush` 异常路径下的 `respnorm` 检查方法已全部实现互斥锁同步保护，`-race` 全绿且经端到端流式客户端断开集成测试守护。曾经的一处客户端可见数据丢失（buffered 模式上游中途断流时 `s.buf` 被丢弃、客户端收到干净空 200）已修复（见 §3 第 46 项 B1）——现在会把已收字节 flush 给客户端并主动 abort 连接，客户端 SDK 因此看到传输中断而非静默空成功。
- **自动化基线**：`go test ./...` 与 `go test -race ./...` 全绿；`internal/archtest` 强制导入单向边界、文件行数预算、函数长度预算与文档引用完整性。
- **§1 分布**：**高危 0 项**、中危 2 项、低危 15 项，合计 **17 条**。`1.49`（imgprep 像素乘积 32-bit 溢出——非活跃，仅 32-bit）与 `1.50`（详单文件名 hash8 碰撞——潜在）为 2026-08 综合评审带入的低危登记（对应评审 B13/B14，B13 已在评审侧标"确定不做"）。`1.40`（工具 ID 归一化下沉 `chatmsg`）、`1.45`（Quota 孤儿 Key 修剪）与 `1.47`（Server 审计路径收敛）已全部完成落地并移入 §3；`1.21`/`1.28`/`1.31`（P7）、`1.19`（P8）、`1.30`/`1.33`/`1.34`（P9）、`1.39`（P11）、`1.41`/`1.37`（P12）、`1.35`/`1.36`（P13）、`1.38`/`1.42`/`1.43`（P14/P15）已修复并移入 §3；原 `1.24`、`1.5`、`1.15`、`1.27` 与 `1.46` 经重新分类后移入 §2；原 `1.23` 已并入 `1.1`。
- **不再有高危条目，也不再有 `[中低]` 条目**。`1.41`（曾经的第三项高危——它产出的不是多余内容而是**错误内容**，且是 `1.35`/`1.36` 的技术前置）已由 P12 修复；`1.35`/`1.36` 本身（体积纪律从未成立、详单内部约 93% 是重复拷贝）已由 P13 修复——默认 `vmr analyze` 真实语料实测从 47MB/253 份详单降到 3.0MB/0 份详单，`-render-all` 全量物化的详单体积降约 86%，详见 §3 第 38 项。`1.43`（唯一的 `[中低]` 条目——检测器覆盖率披露）已由 P14 修复，详见 §3 第 40 项。
- **文件与函数行数守卫语义一致**：两者都是「全局默认 + 豁免表」，新写的文件/函数默认受约束，不依赖有没有人记得登记。

---

## 1. 待定与待解决问题

> **标题方括号里的是严重程度（这件事现在有多糟），不是优先级。** 它与 §4 的 ROI
> （价值 ÷（成本 + 风险）——这件事现在做有多划算）是**两个正交的轴**，不该也不会一致：
> `1.8`（重名方法豁免）严重程度低而 ROI 也低，因为今天无影响；`1.13`（额度看板）
> 严重程度低而 ROI 中，因为价值高但成本也高。**把两个轴对齐会毁掉信息量**——那正是这里同时保留
> 两套评级的原因。要排期看 §4，要判断"现在有多糟"看这里。

### 1.1 [低，已部分闭环] `vmr report` 多文件输入的三趟扫描开销——会话分析那一趟（`analyzeFile`/`collect()`）仍未缓存（含原 1.23）

- **现状**：`report.Build`/`BuildCached` 对同一批输入文件实际跑三趟扫描：① `AnalyzeSessionsCached` 内部并发的 `ctxgraph.ScanCached`（manifest 解析，文件哈希+schema 版本命中即跳过）；② `AnalyzeSessionsCached` 内部并发的 `analyzeFile`/`collect()`（专供会话/任务分组用的每记录特征提取：工具签名、角色字符/token、compaction 标记、chat_id、NoReply 等），**从未查过缓存，每次全量重跑**；③ `aggState.scanFiles`（指标聚合，产出 `RequestRow`/`Report2` 各分桶）。P3.6 已经给 ③ 接上了缓存（`internal/report/factscache.go` 的 `recordFacts`/`fileFacts`，随 manifest 缓存一起落进 `.parse-cache/`）——真实 34 文件/177MB 压缩语料实测：全量热缓存耗时从 71.8s 降到 16.2s（收益 5.2×），但未缓存的 ② 使得热耗时离个位数秒目标仍有差距。
- **可能方案**：给 `collect()` 的输出设计一个可序列化投影（类比 `recordFacts` 之于 `buildRec2`），随同一份 `fileFacts` 缓存，`analyzeFile` 命中时直接重放而不重新打开文件。
- **为什么不在 P3.6 顺手做**：`collect()` 的产出（`ReqInfo`）包含 `taskseg.IndexRealUsers` 的索引结果与用于跨文件缝合判断的 `tailPrev` 消息预览文本，这些字段直接喂给 `group()`/`ctxgraph.StitchGraph` 做**会话/任务边界判定**，正确性敏感度高于纯指标聚合（聚合算错是报表数字偏差，边界判定算错是把不相关的对话缝到同一个 Session/Journey）。仓促给这一层加缓存风险高于收益。
- **触发条件**：决定投入时，先补一套对等的 cold/warm 一致性测试（参照 `TestBuildCached_WarmMatchesBuild`/`TestScanFiles_CacheHitNeverOpensFile` 的模式）再动手。在此之前 ③ 的收益已经是真实、已验证的独立进展。

### 1.2 [中] `vmr report` 全内存聚合的记录量上限

- **现状**：`AnalyzeSessions` 常驻全部记录的关键信息；原始耗时、延迟、Token 样本保存在切片中以计算真实百分位数。
- **实测（2026-08-20，本机全量语料 34 文件 / 11374 条记录，默认 `-details=false`）**：
  `/usr/bin/time -l vmr report -o … logs/*.jsonl.zst` → **峰值 RSS 1.38GB**，墙钟 83.96s（冷缓存）。
  **本条原先写的"千万级记录规模下约占数百 MB"与实测不符**：在**万级**记录上就已经是 GB 级。
  该估算不是保守了一点，是量级判断错了，它同时也是"为什么待定"里"目标单机场景下内存占用完全可控"
  这句话的全部依据——依据被实测推翻，这句话随之失效。
- **与 §1.30 是同一个故事的两半**：`vmr story`（不含 `-render-all`）在同一份语料上峰值约 1.07GB，
  `vmr report` 1.38GB，而 `vmr analyze` 在一个进程里顺序跑完两者。两个半区各自都已经站在 GB 级台阶上，
  这是 §1.30 那次 SIGKILL 的背景，不是巧合。
- **可能方案**：利用审计日志的时间局部性按自然日分桶，跨日后即时释放原始切片。
- **为什么仍然待定（理由已换过一轮）**：不再是"内存完全可控"——而是**这个量级目前仍然跑得完**
  （1.38GB 在 16GB 机器上有余量），且分桶释放依赖"记录时间严格单调递增"这个隐蔽的正确性前提，
  一旦不成立就是静默算错而不是报错。**重估触发条件从"千万级记录"下调为"单次分析的语料超过约
  3 万条记录，或峰值 RSS 超过 4GB"**——按实测斜率，这大约是三个月的历史日志。评审 §5.4 实测
  `vmr analyze` 组合路径在 1.5 万条已到 3.75GB，故 `analyze` 路径的触发点约 **2 万条**，比 `report`
  单独跑的 3 万条更早。
- **已做的一处收窄（2026-08）**：`ctxgraph/stitch.go` 的 blob 倒排索引从 `map[Hash]map[int]bool`
  改为 `map[Hash][]int`（每个 hash 一条 posting list 而非一个小 map），去掉了数百万个单元素小 map 的
  头开销——这是 §5.4 点名的最小动作，不改分桶前提，只降常数。

### 1.3 [低] `chatmsg` 离线解析路径的 `map[string]any` 分配

- **现状**：`internal/chatmsg` 有 43 处 `map[string]any`，全部在离线消息/SSE/usage 解析路径上。转发热路径不受影响——实测 `adapter`/`audit` 为 0，`router` 唯一一处在 `WriteError` 的错误响应体，`server` 的 8 处全在 `/status` 与 `/v1/models`，`probe` 1 处在后台主动探活请求体构造，没有一处在客户端转发链路上。
- **为什么待定**：**前置条件未满足**。离线路径的耗时由磁盘 I/O 与 zstd 解压主导，改用具体结构体的收益完全未经证明。先在真实审计日志上跑 benchmark 拿到 profile，再谈是否值得。没有 profile 数据之前不动。

### 1.6 [低] §2.5 表格的标记符号已达四个

- **现状**：`⭐` 超额度、`‡` 配置变更、`†` 无时间交集、`◇` 部分流量未计入价格，每个都配一条按需渲染的脚注。信息都是必要的，但四个符号叠在一张表上可能已到「标记多到没人看脚注」的临界点。
- **可能方案**：把最罕见的降级为纯 JSON 字段（数据仍可被外部脚本消费，Markdown 不渲染标记）。`◇` 是最可能的候选——它的触发条件（审计日志比 config 旧）本身就罕见。
- **为什么待定**：这是展示密度的主观判断，需要真实报表读起来觉得吵了才有依据。四个标记都是按需渲染的，健康报表一个都不出现。

### 1.7 [低] `vmr report` §2 成本表结构化透传 `CostEstimateEst`（方案 ②）

- **现状**：方案 ①（在 Markdown 渲染层增加口径提示脚注，明确估算成本包含降级估算部分）已闭环。若外部机器消费需要更精确的结构化字段，需给 `Row` / `ClientRow` 补 `CostEstimateEst` 并在表里按 §2.5 既有惯例渲染 "X% est."（需改动 `rows.go` / `accumulateCost` / 渲染层三处）。
- **为什么待定**：方案 ② 会改动 `vmr-report.json` 形状，在无明确外部程序消费需求前遵循 YAGNI。

### 1.8 [低] `archtest` 函数长度豁免的键无法区分同文件重名方法

- **现状**：`internal/archtest/func_sizes_test.go` 的 `funcLineExemptions` 以 `文件路径:函数名` 为键，同一文件里的重名方法会共用一条记录——`internal/report/ingest.go` 就有 6 个 `Ingest` 方法。今天全部远低于 120 行默认限额，无实际影响；但一旦为其中一个登记豁免，另外 5 个会一并被放宽。
- **可能方案**：键改为 `文件:接收者类型.函数名`（`ast.FuncDecl.Recv` 已有类型信息，不需要新依赖）。
- **为什么待定**：需要真的出现一个必须豁免的重名方法才有意义；现在改属于为不存在的场景加机制。

### 1.9 [低] 探针请求绕过审计日志

- **现状**：`internal/router/probe.go` 的健康探活请求直接与上游交互，不写 `audit.Record`，`vmr report` 看不到探活的 Token 与延迟消耗。
- **为什么待定**：探活消耗极低；且需先明确探针流量在报表中的呈现口径，避免污染业务 SLO 统计。

### 1.10 [低] 审计落盘的 `write` syscall 在全局锁内

- **现状**：`audit.Logger.Write` 的 JSON 编码已通过 `sync.Pool` 移到锁外，但最终写文件的系统调用仍在全局互斥锁内。
- **可能方案**：带缓冲通道 + 单独写协程，把 syscall 移出请求路径。
- **为什么待定**：异步写入队列要处理背压策略（丢弃 vs 阻塞）与优雅关停等待；当前直接写入未构成瓶颈。

### 1.13 [低] 额度燃尽看板未交付

- **现状**：`vmr report` 已有额度与消耗对照子表，但更进一步的长期燃尽曲线与预测看板尚未实现。

### 1.14 [低] 滑动时间窗（Rolling Window）限流模型

- **现状**：`internal/quota/period.go` 是日历对齐的惰性周期重置，`every: 5h` 这类滚动窗按周期近似处理。真正的滑动窗需要平滑计数器。
- **性质**：**功能演进，不是架构缺陷**——当前近似对目标场景（月度/日度 token plan）是够用的。与「额度燃尽看板未交付」同属额度可视化与配速的产品路线，不与技术债并列排期。

### 1.17 [低] `imgprep` 的解码闸门按「防炸弹」设定，其内存上界与单请求内存预算差一个数量级

- **现状**：`internal/imgprep` 的 `processImage` 在 `image.Decode` 之前先用 `image.DecodeConfig` 只读图片头取宽高，声明尺寸超过 `maxDecodePixels`（64MP）的直接放弃降采样、原样透传。**这道闸门存在且工作正常**，它的目的写在常量注释里：拦解压炸弹（一张纯色 PNG 能用几 KB 声明出巨大尺寸）。问题在阈值的量纲：64MP 按 RGBA 换算是单次解码约 **256MB**，而 `docs/UserGuide.md`「单请求内存预算」一节核算的是 ~32MB/请求。两个数字各自都对，只是回答的不是同一个问题——一个是「多大算恶意」，另一个是「一个请求该占多少内存」。图片逐张解码、逐张释放，所以多图请求不累加，峰值就是单张图的上界。
- **可能方案**：为内存预算再设一道更低的、可配置的闸门（按已配置的 `image_downscale` 目标尺寸推算，或独立开一个配置项），超过即跳过降采样。
- **为什么待定**：**前置条件未满足**。够到 64MP 需要刻意构造的输入，正常截图与照片比它低一到两个数量级，目前没有任何实测显示这段峰值在真实负载下造成过内存问题。而且方案本身带一个不能替用户默认决定的取舍——跳过降采样意味着图片以原分辨率送上游、vision token 照付，是**用账单换内存**。先在真实视觉负载上观测到内存突刺，再谈阈值该设多少。
- **已落地的部分**：`docs/UserGuide.md` / `.zh` 的「单请求内存预算」一节与 `config.example.yaml` / `.zh` 的 `image_downscale` 注释，已写明这段峰值由**像素数**而非字节数决定、逐张释放不累加、以及 64MP 闸门的存在与量纲。把一个未被记账的内存维度变成已知量——这部分零代码变更、零风险，已完成。
- **登记来源**：`archived/VMR_Comprehensive_Architecture_Review_and_Refactoring_gemini-3.7-flash-v4.md` 的阶段七 VER-02。该报告的初版把这条写成「缺少闸门」，是漏读 `processImage` 所致；闸门一直都在，真正待定的只有阈值量纲这一点。

### 1.18 [中] Phase 1b 六个 LLM 语义判别器尚未完成完整黄金样本校准

- **现状**：`internal/story/llm_findings.go` 的六个 LLM 判别器（P1b.1~P1b.6）已实现、单测覆盖、且已用 `_eval/calibrate_p1b.go` 对真实生产日志（`logs/vmr-audit-2026-08-13~16`）跑过真实模型（`agent` 虚拟模型）验证：6 个真实 Journey 上机械核验 Evidence Anchor 有效率达到 100%（9/9），六个判别器均在真实数据上被验证过至少一次有效触发，人工抽查全部命中结果判断合理。但这仍不是本方案 §3 定义的正式合入门禁——那需要 30~50 个 Journey、每模块 ≥6 正/负例的系统性黄金样本集，并计算真实 Precision/Recall（需要人工标注 Ground Truth，逐条判断每个 Finding 对错）。
- **为什么待定**：黄金样本挑选与人工标注是需要投入实际时间的判断性工作，不是能自动化补全的一步；且目前抽样规模下六个判别器都表现良好，没有观察到需要立即处理的误报模式，不构成阻塞性风险。
- **推进方式**：`_eval/calibrate_p1b.go` 已经是一个可直接复用的真实校准工具——扩大 `-input`（覆盖更多日期的日志）与 `-limit`（采样更多 Journey），把输出交给人工逐条标注 TP/FP，即可补完 §3 要求的完整校准报告；不需要另起工具。
- **登记来源**：`archived/phase1b_implementation_plan_gemini-3.7-flash.md` §7.4.1（Claude Sonnet 5 复核记录，2026-08-17）——该复核发现原有校准脚本是自证循环的假校准（mock LLM 响应、从未调用生产判别器函数），重写为真实校准工具后才有这条真实但尚不完整的结果。

### 1.22 [低] `chatmsg.ToolResultList`/`ToolCallList` 未覆盖 OpenAI Responses API 的 `function_call`/`function_call_output` 形状

- **现状**：`chatmsg.Messages` 已经能把 Responses API 的 `function_call_output` 渲染成人读文本，但结构化提取层 `ToolResultList`/`ToolCallList`（`toolresults.go`/`messages.go`）只覆盖 OpenAI Chat Completions 的 `tool_call_id` 与 Anthropic 的 `tool_use`/`tool_result` 两种形状——`toolResultsFor` 三级配对、三个 Finding 检测器、决策脊柱渲染，对纯 Responses API 流量都不会展示任何工具调用结果。
- **为什么不在 P1 一并修**：P1.1 的范围是修那两种已有形状里的 ID 改写问题，不是扩展协议覆盖；
  这是一个独立的、更早就存在的协议覆盖缺口，需要先确认 Responses API 流量在真实语料里的占比再决定值不值得投入。
- **重新评估（2026-08-20）——本条自己提出的前置问题现在有答案了，答案是"占比为零"**：
  用本机全量语料生成的 `vmr-requests.json`（11253 条记录）按 `protocol` 字段统计：
  **`openai-completions` 11194（99.5%）、`anthropic-messages` 59（0.5%）、Responses API（`openai-responses`）0 条（0.0%）**。
  也就是说这个缺口在本项目的真实使用中**一次都没有被触发过**。按 YAGNI，**决定不做**，
  但保留登记，因为影响面已经比 P1 登记时更大（见下）。
- **影响面的变化（P4 之后）**：P1 登记时的影响只是"脊柱不展示工具结果 + 三个 Finding 检测器无证据"，
  都是展示层。P4 之后 `journey-<id>.json` 的 `structure.tasks[].steps[].tool_calls` 也走同一条
  提取路径——一旦真的出现 Responses API 流量，**机读契约会静默地报告"这一步没有工具调用"**，
  而不是报告"这种形状我不认识"。展示层降级读者能看出来，机读契约降级读者看不出来。
- **触发条件（量化，不再是"先确认占比"这种没有边界的话）**：任意一次 `vmr report` 的
  `vmr-requests.json` 里出现 `protocol == "openai-responses"` 的记录，即重新排期；在那之前不投入。
- **登记来源**：2026-08-19 P1 执行期间的独立代码审阅发现；2026-08-20 六阶段复盘用真实语料量化占比。

### 1.29 [低，暂不做] `journey-<id>.json` 的 `structure` 字段没有 schema 版本戳

- **现状**：P3.7 给解析缓存（`.parse-cache/`）补过 `CacheSchemaVersion`，理由是"改了提取逻辑，旧
  缓存会被静默复用"。`journey-<id>.json` 今天没有等价机制——P4 之前生成、躺在磁盘上的旧文件与
  P4 之后重新生成的文件字段名相同但形状不同（没有 `structure`，或未来再改了 `structure` 内部字段），
  消费者无法仅凭文件本身分辨。
- **为什么风险比 `.parse-cache/` 小、暂不处理**：`journey-<id>.json` 不是跨运行复用的缓存——它是
  `vmr story -journey`/`-render-all` 每次针对该 journey 运行都会整份重写的输出，不存在"部分命中、
  部分陈旧"的静默复用路径；触发条件收窄为"用户手头有一份很久没重新生成过的旧文件，且下游消费者
  没有做字段存在性判断就假定新形状"，比缓存的"每次运行都可能静默复用"窄得多。
- **重新评估（2026-08-20）——上面这段论证回答错了问题，但结论仍然成立**：版本戳在**缓存**上的
  作用是"别复用陈旧的"，在**发布契约**上的作用是"让消费者能发现形状变了"——这是两件事，
  用"它不是缓存"去论证"它不需要版本戳"是答非所问（没有人主张它是缓存）。正确的论证是
  **YAGNI + 已裁决的无兼容包袱**：`story_report_architecture_opus-5.md` 的"已裁决的前提"一节明确裁决"JSON 无外部脚本消费"，
  `journey-<id>.json` 至今唯一已知的程序化消费方是 `_eval/calibrate_p1b.go`，它只读
  `EvidenceAnchor`。**没有消费者，就没有人需要探测版本。** 结论不变（暂不做），
  但理由换成站得住的那个。
- **触发条件（量化）**：出现第一个 `_eval/` 之外的程序化消费方，或 `story_report_architecture_opus-5.md`
  "面向未来：如果这套工具将来要变成一个 Web 服务"一节描述的只读服务方向真的启动（那时机读层就是
  API 返回体，版本协商是硬需求）。
- **可能方案**：`structure` 内部加一个 `schema_version int` 字段（或复用 P4.3 尚待定的 JSON 语言
  策略字段一并设计），成本接近零，但**改在这一次新增字段时最便宜**——现在不加，以后每加一版都要
  多考虑一层"没有版本戳时怎么判断"。
- **登记来源**：2026-08-20，P4 独立评审提出，核实为真实但低优先级，留给 P5/P6 触及 `journey-<id>.json` 形状时一并考虑。

### 1.48 [低] 错误分类词表的长期形态：端点级 quirk 统一模块（含 sticky 降级优化）未做

- **现状**：错误分类的 vendor 知识散在 `DefaultClassify` 的全局词表里（contentHint /
  contextLimitHint / upstreamHint / vendorQuirkHint / authHint，约 30 词）。已知的厂商专属约束类
  误判（DeepSeek 思考模式 rc 回传、Google thought_signature）已由 `ErrQuirk` 类 + 词条修复覆盖
  （§3 第 45 条），词表之间尚未互相干扰。全量方案——每 vendor 一个编译期注册的 quirk profile
  （marker 表 / 建议分类 / sticky 策略字段统一声明），替代散在全局词表里的 vendor 知识——经评审
  **延后**：当前词表规模下收益前瞻，现在上机制是替一个不会自己举手的问题排队。
- **可能方案（升级时直接可用）**：每 vendor 一个编译期注册的 quirk profile；profile 按 **model
  glob** 匹配（不按 provider 名——provider 名是用户 config 自起名，改名即静默失效，且按 provider
  收窄会让经中继转发的流量退回旧行为）；字段含 marker 表、建议分类、sticky 策略；`DefaultClassify`
  保留为无 profile 命中时的兜底通用分类。
- **附带优化（一并待定）**：quirk 命中时对 sticky 会话做降级——被 quirk 拒绝的会话若粘在同一端点，
  后续每一轮都会再付一次 ~1–2s 的失败往返才 failover；降级（清除该会话粘性或降低权重）可消除这段
  重复延迟，但要动 sticky 注册表与 router 的交互面，非词表改动可比。
- **触发条件**：全局词表继续增长并开始出现互相干扰/误命中（某条 marker 在另一家厂商的错误语境下
  被复用），或 sticky 重复往返在真实负载中可观测地拖慢中毒会话。

### 1.49 [低，非活跃——仅 32-bit] `imgprep` 解压炸弹守卫的像素乘积在 32-bit `int` 平台可溢出

- **现状**：`internal/imgprep/imgprep.go` 的 `processImage` 用 `cfg.Width*cfg.Height > maxDecodePixels`（64MP）挡解压炸弹（见 §1.17 的量纲讨论——那是另一回事）。`cfg.Width`/`cfg.Height` 是 `int`；在 32-bit 平台 `int` 是 32 位，两个都接近 `int32` 上限时乘积回绕成小值，绕过守卫。
- **为什么非活跃**：Go 的 `image/png` 解码器把 IHDR 宽高钳在 `int32`（`w <= 0 || h <= 0` 检查前先转 `int32`），所以 `cfg.Width`/`cfg.Height` 恒 ≤ 2^31-1；在 **64-bit**（唯一 CI/目标平台，与 `disk_windows.go` 桩、"目标：macOS/Linux 单机"的口径一致）乘积 ≤ (2^31)² ≈ 4.6e18 < `int64` 上限，不可能溢出。BMP 等其他格式的 `DecodeConfig` 同理受各自解码器的尺寸校验约束。
- **修法（触发时直接可用）**：`int64(cfg.Width) * int64(cfg.Height) > maxDecodePixels`，一行。
- **触发条件**：32-bit 成为受支持的构建/部署目标。在此之前登记即可——2026-08 综合评审 B13 把它标为"确定不做"（当前无目标平台受影响）。

### 1.50 [低，潜在] 详单文件名去重位 `md5(basename:line)[:4]`（32 bit）

- **现状**：`internal/ctxgraph/reqcoord.go` 给详单文件名算一个 4 字节（32 位）hash 去重后缀。单个源文件接近 1 万条记录时，按生日界 hash8 碰撞概率约 1%；但要真正撞成同一个文件名还需同毫秒时间戳 + 同模型 + 同 outcome 三者一致，现实可忽略。
- **恶化曲线**：与 §1.2 的"单次分析语料超过约 2–3 万条"同步线性恶化。
- **为什么待定**：评审自身（B14）即判"登记即可，不值得现在动"。真出现碰撞时，把去重位从 hash8 提到 hash12/hash16，或改用递增序号消歧，都是局部改动。

## 2. 刻意取舍，不是缺陷

> 以下条目基于项目核心哲学（KISS / YAGNI / 单二进制 / 零代码侵入）做出，已经论证过，不需要重新论证。**推翻其中任何一条是允许的，但必须先知道自己在推翻它，并给出新的理由。**

### 2.1 运行时与并发

- **`health.Registry` 全局互斥锁不分片**：单机场景下锁持有时间只是纳秒级 map 读写，分片增加复杂度且无吞吐收益。
- **`HealthKey` 取 SHA-256 前 4 字节**：单实例端点规模下碰撞概率可忽略。
- **健康状态机的退避冷却参数硬编码**：坚持「零调参」，不向用户暴露难以科学校准的配置项。
- **`copyFlush` 的 goroutine + channel 流水线**：避免在底层连接层设置全局 Deadline 破坏 TLS/Header 超时语义。
- **客户端取消时不停止计费**：上游已经生成的 token 厂商照收，路由侧照收才与账单对齐；改成不计费会让 `vmr report` 系统性低估消耗，正是花了整整一批修掉的那类发散。这条只讲计费要不要停，与下面这条是两件事：取消的**传播**（让上游连接真正中止）从来不需要 `copyFlush` 感知——它已经通过 `BuildRequest(r.Context(), …)` 挂到上游请求上，客户端一断，`http.Transport` 就中止上游连接，`copyFlush` 随之退出。取消的**检测/归类**（让审计记录准确标注 `canceled` 而不是误记成 `ok`，见 §3 第 13 项）则是另一回事，`copyFlush` 为此确实需要 select 一次 `ctx.Done()`——不这样做就只能靠后续写入失败间接推断取消，命中不了「响应已完整送达、客户端读完才断开」这类没有写错误可依赖的场景。
- **`respnorm` 的 `Read` 在等待更多字节时返回 `(0, nil)`**：唯一消费方 `copyFlush` 显式处理这一形态；改成内部阻塞循环会让 idle 看门狗失去以读取为粒度的心跳。
- **`respnorm` 的 usage sniffing 不外移为 `router` 侧装饰器**：装饰器要在转发热路径上每 chunk 多付一次接口调用与边界检查；当前实现搭 `ingest` 已有的 per-chunk 循环，零额外开销。理由写在 `internal/respnorm` 的包注释末尾。注意这条只讲**位置**——sniffing 累加器与审计字段之间的同步问题是另一回事，见 §3 第 12 项（已闭环）。
- **`GET /health` 为存活探针（Liveness）而非就绪探针（Readiness），永不因上游不可用返回非 200**：200 仅代表进程存活。若与上游端点健康度绑定，当所有供应商不可用时容器编排系统（如 K8s / Docker）会触发无休止重启，无法修复上游故障反而放大雪崩效应。需要就绪度/端点健康度的调用方应消费 `/status` 的模型健康块。
- **`/status` 的 `traffic.requests` 计数含未鉴权/被拒请求**（401/413/坏 JSON/未知模型都计入 `total` 与 `by_protocol`，并按 `by_status` 记入 `error`）：口径是"进程见过多少 HTTP 请求"，不是"成功路由了多少"。区分两者需要把 `RecordRequest` 移到鉴权之后并单独统计被拒请求——为一个展示字段增加两条埋点路径不值得。被拒请求的精确语义在审计日志里，`/status` 只给趋势。
- **`traffic` 的 `by_status` 按请求（而非 attempt）计数，且不保证 `ok+canceled+error == total`**：failover 重试失败的中间 attempt 不计入（只在终态记一次），客户端在上游响应头到达前断开、`router` 未初始化、协议错配 404 等少数路径不记或记入 error。这是展示语义，不是路由语义；审计日志的 attempt 级记录才是精确账本。
- **`system.disk.free_space` 在 Windows 上是桩（恒为 0）**：`syscall.Statfs` 无 Windows 等价物，`disk_windows.go` 返回 `0, nil`，看板显示 "0 B"。为补齐它需要 `golang.org/x/sys/windows` 的 `GetDiskFreeSpaceEx` 或手写 `syscall` 胶水——而 Windows 不是目标部署平台（目标：macOS/Linux 单机 + 局域网），为一个桩写平台胶水违反 YAGNI。
- **`/log` 的慢订阅者以「丢行 + 标记」处理，永不让日志热路径阻塞**：日志行产自请求热路径（`rt.logf`），任何同步投递都可能拖垮路由。每个订阅者一条有界 channel（64 行），满则丢行并插入 `... dropped N lines ...` 标记——静默丢失会误导排障成"vmr 没打日志"，标记一行代码换回可诊断性。`log.html` 同样不做自动重连（只提供手动重试按钮），避免服务重启风暴下的重连洪水。
- **启动 banner 与 panic 直写 stderr，tee 不捕获**：banner 只在进程启动出现一次，panic 时进程将死，两者都不值得为 `/log` 引入第二条写入路径。

### 2.2 配置与协议

- **协议枚举值 2026-08 重命名为 `openai-completions` / `anthropic-messages`（`openai-responses` 不变），与 Pi Agent 等生态工具对齐**：全链路（代码、配置、文档、测试、新审计日志）一步到位用新名，路由侧零兼容负担。**唯一的兼容咽喉点**是 `audit.Record.UnmarshalJSON`（`internal/audit/audit.go`）：读到旧名 `"openai"`/`"anthropic"` 时经 `core.CanonicalProtocol` 归一化为新枚举，`Attempts[].Endpoint` 标签的 protocol 段经 `core.NormalizeEndpointLabel` 一并归一化（只改前导 token，分隔符与其余字节原样）。这层只服务分析侧（report/story/reqdetail/ctxgraph 都解码进 `audit.Record`）读历史日志；`vmr replay` **不做兼容**，只认新枚举。`ctxgraph.CacheSchemaVersion` 已 1→2 使旧事实缓存失效重建。**这是「版本必须匹配、不做兼容」原则的唯一刻意例外**——历史审计文件不像 CLI/Server 那样可随时一起升级，它们是不可变的既存事实。**TODO(2026-10)：过渡期约一个月，届时的完整拆除清单**：① 删 `core.CanonicalProtocol` / `core.NormalizeEndpointLabel`（`internal/core/protocol.go`，常量 `Protocol*` 保留）；② 删 `Record.UnmarshalJSON`（`internal/audit/audit.go`）**并同步撤掉本次为它新增的 `vmr/internal/core` import**（`internal/audit` 在此之前不依赖 `core`）；③ 删 `internal/audit` 的 `TestRecordUnmarshalJSON_NormalizesLegacyProtocolNames` 与 `internal/report` 的 `TestBuild_LegacyProtocolNamesNormalized` 两个测试；④ `ctxgraph.CacheSchemaVersion` **保持 2 不回退**（回退会让分析侧重新接纳 schema v1 的旧事实缓存，正是当初 bump 要挡的）；⑤ `examples/sample-audit.jsonl` 已随本轮改用新枚举名，无需处理。
- **CLI 与 Server 版本必须匹配，任何不一致造成的问题直接报错，不做兼容性处理**：单二进制、可随时重启的项目里，`vmr status` 与 `vmr start` 理应始终是同一个版本——版本不一致说明升级流程没走完，报错（而不是降级渲染）正是在暴露这个没走完的升级。`json.RawMessage` 式的兼容层只覆盖一个滚动升级窗口，却会永久留在代码里，违反 KISS。`vmr.sh ps` 的 `|| true` 退化为标注行是它自己的容错，不受此限；错误信息会明确提示"server and client vmr versions differ"。这条原则覆盖字段*新增*与形状*变更*两种情形：曾为"旧 server 缺失新 key"保留的 `serving` 字段 `*bool` 兜底，在 `instance.config` 由 string 改为 object 后实际已不可达（新 CLI 解析旧 server 响应时在 `config` 处即硬失败，永远走不到 `serving`），已作为死代码删除（2026-08-23，`vmr status` review P4 落地）——版本必须匹配的原则不再留任何字段级例外。`models` 从 `"name [protocol]"` 拼接键 map 改为结构化数组（2026-08-26，为携带模型级 capabilities/context）同受此原则覆盖。
- **`/status` 的 `instance.base_urls` 回显请求自身的地址而非 `listen` 配置**：host 取自 HTTP Host 头、scheme 取自是否 TLS——调用方用什么地址访问 `/status`，就广告什么地址（`127.0.0.1` stays `127.0.0.1`，`localhost` stays `localhost`，局域网 IP 原样），这正是客户端该填进自己配置的值；反代场景下 Host 头恰好就是代理对外的地址，刻意不做 `X-Forwarded-Host` 解析。该字段纯展示、不参与鉴权或路由，Host 可伪造无安全影响；同一实例被不同地址访问时 `base_urls` 不同是设计意图，不要缓存/固定它。
- **`/status` 的 `traffic.by_status` 在流式中途截断时记为 `error`，与审计顶层 `outcome` 对同一请求记 `ok` 口径不同（刻意保留，不做对齐）**：`forwardSuccess` 里 `RecordOutcome` 把 TRUNCATED 计入 `error`——它回答的问题是"客户端是否拿到了完整响应"；而审计侧对 HTTP 200 且非取消的请求顶层记 `ok`，截断信息记录在 attempt 的 `ErrorClass` 上——它回答的问题是"HTTP 交换是否在传输层正常完成"。两个账本回答的问题不同，各自口径内部自洽；强行对齐会让其中一个失去自己的语义。两处代码（`Telemetry.RecordOutcome` 的 doc comment 与 `forwardSuccess` 调用点）已互相指向本条。若未来有人对账时发现 `/status` 错误数与 `vmr report` 错误数不一致，先查这里再报 bug。**截断的客户端信号**（B1，见 §3 第 46 项）：TRUNCATED 时 `forwardSuccess` 在 `respnorm` 把可安全交付的已收字节 flush 给客户端之后 `panic(http.ErrAbortHandler)`——net/http 静默 abort 连接、不写终止 chunk，客户端 SDK 因此看到断掉的传输而非干净成功。此账本口径（`by_status=error` vs 审计 `outcome=ok`）不受影响。
- **`/status` 端点项刻意不加端点级累计计数器（requests / ok / failed / tokens）**（2026-08-28，status.html 拓扑视图 review 复议后定案）：`consecutive_failures` 在 `/status` 里出现，是因为它是一个**当前健康状态**读数（此刻连续失败几次、在不在退避里）——属于 liveness 视图。而「每个端点累计请求多少、成功多少、失败多少、消费多少 token」是**分析半区**的职责，`internal/report` 的 `EndpointRow`（Attempts/OK/Forwarded/Failed/Availability/ErrorClasses/Requests/tokens/cost，含 by-date 与 cross-date）已经完整产出，数据源是可持久化、可按时间切片的审计日志。给 `/status` 再塞一份进程内、重启即失的实时副本：① 与分析半区重复计数（正是「一个分析数字复现一个路由数字必须差分测试锁定」那条不变式要防的负担）；② 做全（4–8 个计数器 × N 端点）等于给 `router.Telemetry` 加一张按端点的动态 map，破坏它「全固定原子、热路径零 map 零锁争用」的设计；③ 只做 `requests` 一个又与旁边的健康量表列语义打架、价值不大。结论：`FAILS` 列维持现状（健康量表），端点级累计账走 `vmr analyze` / `vmr report`。

- **环境变量未定义时静默展开为空串，且不支持 `${VAR:-default}`**：保持配置解析简单明确，默认值应在 YAML 里显式写出。
- **`internal/config` 的三层费率解析不后置到 `router.BuildSnapshot`**：`config` import `pricing`、在 `validate()` 阶段跑完解析，看起来像「配置层反向侵入用例层」，因此被反复提出。但这是 `docs/VirtualModelRouter_Design_v4_Quota.md` 决策表「定价的落点」一行**明文选定的方案**，「只让 report 一侧解析、`metric: cost` 另开一条运行时校验路径」正是同一行里已否决的备选（理由：两份实现容易漂移）。后置还会摧毁「`metric: cost` 费率不齐 = **加载期**错误」这条硬要求——`vmr check` 将不再能在不联网的情况下告诉你费率配错了，一个确定的加载期失败被换成运行期意外。
- **多协议适配器（`adapter/{openai,anthropic,openairesponses}`）保持独立子包，不合并也不抽取通用骨架**：三个协议看似相似，底层已存在真实分叉（如 Anthropic 529 错误重试特判、Responses 顶层 `input` 数组与 `RewriteInputRoles`）；独立子包支持编译期 `init()` 静态注册与独立单测，新增协议零侵入。合并成统一参数化结构体只是把类型多态改写为字符串 `if` 分支，可读性与扩展性反而劣化。
- **不引入端点级通用运行时 quirks 插件系统**：坚持编译期确定性，只对已证实的厂商行为差异做受控修复。
- **不合并 `Dimension`（排序）与 `Condition`（淘汰）**：淘汰依赖请求事实，排序只比较端点属性，职责分离保证接口纯粹。
- **ProviderGroup 方案的多 Key 部分（`api_keys:`）已实现（2026-08-22，`internal/config/apikeys.go`），均衡策略与分级 Failover 仍然不做**：早先设想的"运行时 KeyPool"（一个 Provider 账号内部再拆多把 Key、请求期在池内随机选）会违反 `core.Endpoint` "构造后不可变、`HealthKey()` 只算一次"这条贯穿 health/sticky/quota 三个子系统的不变式——`HealthKey()` 在 `Classify`/`Acquire`/`ReportFailure`/`sticky.Peek`/`findByHealthKey` 等多处调用点都被当作端点的终身稳定身份使用，运行时换 Key 会让这个身份跟着漂移。实际落地的是"配置期展开成多个独立 `core.Endpoint`"：`Provider.APIKeys`（具名映射表，`{label: key, ...}`）在 `config.Parse` 里、`validate()`/`BuildSnapshot` 之前展开成 `<name>-<label>` 命名的独立 `Provider`，并就地重写 `endpoints[].providers`/`fallback_endpoints[].providers` 的引用——下游 quota/health/sticky/audit/report/story 全部按 `Provider.Name` 字符串解析，零改动。当初设想的三处工作量，前两处被这个展开形状本身架构性绕开，不是被实现：①**均衡策略**——`strategy.Sort` 稳定排序本身不变，同优先级仍按 `Config.Providers` 列表顺序决出第一名；但 `api_keys:` 展开出的几个 Provider 在这份列表里的相对顺序不保证等于 YAML 书写顺序（`Provider.APIKeys` 是普通 Go map，特意不做有序解析——round-robin/random `strategy.Dimension` 依然不做，谁排第一因此也不可预先指定，只能读 `vmr check`/启动日志的实际展开结果），没配 quota 时排第一的那个继续吃全部流量、其余纯冷备；②**配额聚合**——因为每把展开出来的 key 是独立 Provider 名、独立 quota 池，"同一 Provider 名下几把 Key 共享 `{every, since, amount}`"这个对齐难题根本不存在了。第三处维持原判：③**分级 Failover**（402 跳 Key / 5xx 跳 Provider）仍不做——`internal/adapter/classify.go` 目前共用 `ErrEndpoint` 的 402/404 未拆分，候选列表经 `strategy.Sort`/配额重排/Sticky 重排后也不保证同源 key 仍然相邻，这两个前提没变，仍然留到看到真实需求后再单独立项。

### 2.3 校验与防御性编程

- **`/status` 的网络可达性与身份认证解耦，且复用聊天入口的同一把 `api_keys`**：网络范围由 `listen` 绑定决定，认证由 `api_keys` 决定——未配 `api_keys` 时任何能连到端口的人都能读 `/status`（这是 2026-08-23 的显式决策，替代了旧的 loopback-only 拦截）。这把 key 同时是聊天客户端的管理凭证：任何持有客户端 key 的用户都能看到全部端点名、provider 身份、quota 消耗与配置路径。对一个单人/小团队的代理这是正确的简化；如果未来出现"多租户客户端 + 独立管理面"的真实需求，再加独立的 `admin_keys`，而不是现在就分。`config.Check()` 对"非 loopback 且无 api_keys"给出 warning 级告警（`vmr check` 与 `/status` 的 `issues` 都会显示）作为安全兜底。
- **`vmr status -addr` 回退读取本地 config 的 `api_keys[0]` 并发送到目标地址**：`-addr` 显式指向别的实例时，若本地 `./config.yaml`（或 `-c`）配了 key，会把它当作 Bearer 发给那个目标。设计意图是让 `./vmr.sh ps` 对本机多实例免手工传 key；代价是"你本地 config 里的 key 会被发给任何你显式指定的地址"。这是 CLI 的默认便利行为、不是网络层漏洞——目标地址是使用者自己敲的；且只发 key、key 不出现在 URL 或日志。若未来出现"同一台机器上多份 config 各自有 key 且互相不信任"的场景，再改成 loopback-only 回退。
- **看板（`/status.html`）把 API key 存 `localStorage`，且静态外壳免鉴权直出**：外壳不含任何数据，数据请求走 `/status` 的 `s.auth()`；key 只在浏览器本地持久化，不进 URL、不进服务端日志。内插到 `innerHTML` 的所有配置派生字符串（provider/endpoint/model 名、YAML 错误文本）均已 HTML 转义（`esc()`），"配置内容注入标记"只剩配置所有者自己攻击自己这一种残余面。
- **`/log` 与 `/status` 同用一把 `api_keys` 鉴权；`/log.html` 免鉴权直出静态外壳且复用 `localStorage['vmr_status_key']`**：日志行含 client key tag / endpoint / 模型名 / 用量，属敏感面，绝不裸奔；外壳不含任何数据，与 `/status.html` 同一模式，key 只存浏览器本地、不进 URL 与服务端日志——浏览器导航到裸入口 `/log` 带不了 header，故页面靠 JS 请求带信。**输出为 `text/plain` 而非 SSE/JSONL**：日志在源头（`logfmt.go`）就已是格式化文本，出 JSONL 需自解析或双路渲染；若未来出现机器消费方，再增量加 `/log.jsonl`（同一 tee、不同渲染），不是欠债。**无查询参数**——回放窗口就是固定 512 行缓冲，参数化会制造"要了 5000 只给 512"的错觉。

- **`/help.html` / `/help.zh.html` 的 Agent 配置片段在浏览器就地装配，不做服务端模板渲染**：`/help` 按架构必须公开免鉴权，改服务端渲染会逼它强制鉴权、或让服务端拿不到用户的 API Key——两条都推翻"复制即用"的初衷。所以片段填充全在客户端：`/status` 可读后，单模型片段就地替换 model id / context / token 预算；四个列表型配置（Pi `models.json`、`opencode.json`、Continue `config.yaml`/`.json`、Aider `.aider.model.metadata.json`）按协议全量重生成，逐个虚拟模型列出。API Key 复用 `localStorage['vmr_status_key']`（与看板同一把），只存浏览器本地。服务端下发的 HTML 里保留写死默认值（`coding` / `claude`、200k context、`high` effort、`YOUR_VMR_API_KEY`），保证无 JS / 未鉴权时也是一份自洽配置；JS 只在 `/status` 可读后替换它们。四点刻意取舍：① max-output 预算 `maxOutFor` 是按 context 分档的经验估计（VMR 无模型级 max-output 元数据），钳到不超过 context 本身；reasoning/thinking effort 统一默认 `high`；② 生成的片段一律 vision-on（空 capabilities = 不受约束 = 全支持，见 `internal/core/core.go` 包注释），不按能力集条件化增删视觉行；③ 四个列表型生成器只枚举 `openai-completions` 模型，纯 `anthropic-messages` 实例这四个片段回退到静态块（那几个 Agent 本就是 OpenAI 协议，实例连不上就是连不上，无信息损失）；④ 无浏览器 JS 测试基建，`TestHelpPage_SnippetFillEngine`（`internal/server/status_page_test.go`）只做构建期字符串守卫（引擎函数 + 生成器 + 静态默认字面量在位），JS 行为靠人工复核。

- **`nil` 校验只加在跨包公共入口，且一律 fail-fast，绝不静默兜底**：已加校验的是 `report.AnalyzeSessionsCached` 与 `story.BuildChain`/`BuildAll`/`PreviewTitle`/`PreviewTitles` 五个入口——判据是「跨包公共 API + 后接并发扇出或递归组装」，深处 panic 会带崩整个进程而不是给出一条可读错误。`taskseg.IndexRealUsers`、`taskseg.HasNewInstruction` 这类**包内被上述入口保护的函数**不再重复校验：调用链上游已经拦过，重复校验只是噪音。
- **`fmtutil.DisplayZone` 保持裸 `var`，不封装线程安全访问器**：生产代码**零写入点**——全仓 8 处写入全在 `_test.go`，且相关测试包无一使用 `t.Parallel()`，`-race` 全绿。「让测试能确定性覆盖」本就是这个 var 存在的理由之一（写在它自己的注释里），加锁保护的是一条不存在的写路径，与本节「不为不可能发生的场景加校验」同判据。
- **尤其不做「`prof == nil` 就回退到 `Generic`」这类静默兜底**：`OpenClawAware` 与 `Generic` 会给出**不同的任务标题与任务边界**。真的传进 nil 说明调用方有 bug，静默换一个 Profile 会让报表照常跑完、产出一份错误但看起来完全正常的分析结果，比直接 panic 难查得多。同理，`HasNewInstruction` 的 `cur` 参数也不加判空——构造上不可能为 nil，属于「为不可能发生的场景加校验」。

### 2.4 包边界与依赖

- **`imgprep.ImageInfo` 到 `audit.ImageInfo` 的字段拷贝**：换取 `imgprep` 不依赖 `audit`，保住公共工具包的零依赖边界。
- **`chatmsg.ReassembleSSE` 与 `respnorm` 的 SSE 状态机保持分离**：前者面向离线完整语义提取，后者面向在线字节级保真转发，关注点不同，强行复用只会增加耦合。
- **`internal/report/cost.go` 的端点标签切分不并入 `core.SplitEndpointLabel`**：后者兼容 `:` 与 `/` 两种格式，前者只认 `:`。把 `$` 成本估算那个调用点放宽到也接受 `/`，会改变旧格式日志的历史报表金额——这是一次需要单独评审的行为变更，不是「统一实现」的顺带产物。理由写在 `core.SplitEndpointLabel` 的注释里。
- **`core.StickyBackstopTTL` 不迁回 `internal/sticky`**：迁回会制造一条 `config` → `sticky` 的新依赖边，用途仅是读一个常量；而不做这个校验，`sticky_ttl` 超过 backstop 的配置会「看起来被接受、实际静默失效」（条目被 `Set` 的清扫丢出 map，路由从 sticky 悄悄退化成 not sticky，且不报错）——正是 fail-fast 配置哲学要拦的那类惊喜。校验点在 `internal/config/config.go` 的全局 `sticky_ttl` 与 endpoint group 覆盖两处；理由写在 `core.StickyBackstopTTL` 与 `sticky.BackstopTTL` 两处注释里。
- **`adapter` 的协议字段字面量不从 `jsonscan` 导出复用**：`"model"`/`"stream"`/`"messages"`/`"input"` 是不可变字节常量而非共享状态；「知道这些字段名的含义」正是把 `TopLevelProbe`/`SessionFingerprint` 留在 `adapter` 的那部分领域知识，也是 `CLAUDE.md` 里「需要具体字段名的函数不属于 `jsonscan`」这条规则的由来。`TopLevelProbe` 本身已经完全构建在 `jsonscan` 的 `Skip*`/`TopLevelValues` 原语之上，**不是第二套扫描器**——把它改成 `ProbeTopLevelFields(raw, fields...)` 等于把字段名当参数传进 `jsonscan`，正面推翻上述规则。理由写在 `adapter/fingerprint.go` 的字面量变量块与 `jsonscan` 的包注释里。
- **不把分析半区拆成独立二进制**：坚持「单二进制单文件分发」这个核心体验。
- **不引入 DuckDB / cgo 做数据聚合**：保持纯 Go、跨平台零 C 依赖。
- **`i18n` 的 26 个微文件不合并**：它与 `internal/report/section_*.go` 的「一节一文件」硬规则一一配对（`archtest` 强制），合并会让「改一节文案」从打开一个几十行的小文件变成在几百行大文件里找。此外，文案中包含大量带参闭包函数（非纯静态字符串），强行合并为单一文件（仅 report 侧即超 1500 行）将直接击穿 `archtest` 700 行全局预算。
- **`i18n` 的 `type XxxText` + `if lang == ZH` 样板不改写成 `map[Lang]T` + 泛型 `pick`**：这个改写只消掉每个文件 2 行的分支——真正占体量的 struct 定义与两份字段赋值一行都省不掉，26 个文件全改换约 50 行，还要新引入一个泛型 helper 和「key 缺失怎么办」的新问题。收益为负。
- **`internal/probe` 不登记进 `zeroInternalDepPackages`**：它今天确实零内部依赖，但那张表的语义不是「当前碰巧零依赖的包全登记」，而是「**承诺**永远零依赖、任何包都可无顾虑 import 的叶子工具包」。`probe` 的包注释写明它独立成包是为了避免 `diagnose`→`router` 的 import cycle，是路由半区的协议原语；未来它需要 import `core` 完全合理。登记等于给一个从未做出的承诺加锁。同理不登记 `rundir` / `buildinfo`。作为对照，新提取的字符级 Token 估算包 `internal/tokenutil` 因承诺永不依赖内部包，已如实登记进 `zeroInternalDepPackages`。
- **`internal/core/core.go` 不按领域拆成 `endpoint.go`/`quota.go`/`pricing.go`**：同包拆文件不改变任何编译依赖（`go list -deps` 逐字节相同），所以它是代码导航整理，不是架构重构，也就不该被当成一个「问题」。真正解决「core 会不会长成上帝包」的是**准入规则**，那条规则已经写在 `internal/core` 的包注释里并对存量逐条复核过。真要拆是零成本搭车项，但没有它要解决的问题。
- **`imgprep` 的 `map[string]json.RawMessage` 不与 `jsonscan` 的字节扫描统一**：图片降采样要重算尺寸并重编码图像，是深度结构化重写，字节 splice 做不到。这是三个 sanctioned deviation 里最大的一个，`CLAUDE.md` 已记载。
- **不向 Clean Architecture 四层同心圆靠拢做整体重构**：要把横跨环边界的包「归位」，就得为满足图示而拆包插接口，代价是新的包边界与一层不解决任何真实问题的间接性，违反 KISS/YAGNI 与「编译期注册、无运行时插件系统」不变式。项目已有更强且**可执行**的架构模型（两半区 + `archtest`）。附一条常被忽略的反证：`internal/config` import `internal/adapter`（校验期需要知道协议注册表里有哪些 adapter），按 CA 映射这是一条「外环依赖内环」的合法边——CA 本就不是这个项目合适的透镜。
- **不对 OpenAI 工具返回做 `error:` 关键字模糊嗅探**：经对真实生产语料库全量 495,672 条 OpenAI 工具调用结果实测扫描，确认其全部为自由文本 stdout/stderr（包含 `{ "error": ... }` 等结构化 JSON 错误字段为 0 条，占比 0.00%）。若强行做子串模糊嗅探将引入海量代码输出与测试用例文本的假阳性误报。维持架构取舍：仅对协议原生携带结构化错误标记（如 Anthropic `is_error` 字段）的工具结果进行确定性统计，不基于自由文本做不可控的语义猜测。
- **`go.mod` 保持裸模块名 `vmr`**：改名要动全项目 import 路径，无实质收益。
- **模型/端点展示面的一致性靠统一口径 + 契约测试，不靠共享结构体**：运行时视图以 `/status` 的 `models` 数组为唯一权威（`vmr status` CLI 与 `status.html` 直接消费同一 JSON，天然同步）；人类可读模型标签 `"<name> [<protocol>]"` 只在 `core.ModelLabel` 一处定义（diagnose route Group 与 `vmr status` 都从它取）。刻意不统一的三处：`/v1/models` 是协议面 schema，由 OpenAI/Anthropic 客户端约定决定；`vmr check` 的分层 config 视图（模型基线 + 端点叠加/覆盖逐项可见，看配置缺口是其存在意义）与 `/status` 的聚合运行时视图（并集/最大值）回答的是两个不同问题；`vmr diagnose` 的扁平 Result 数组是逐检查语义，重构成嵌套树没有消费方受益。`/status` JSON 形状由 `internal/server/admin_status_test.go` 的契约测试锁定——改形状必须连带更新 CLI/dashboard 消费方，测试会先红。
- **`.parse-cache/` 不做分片孤儿回收 GC**（原 §1.27 重新归类）：`ctxgraph.SaveCacheDir` 只增量写入当前 `FileCache.Files` 存在的分片，不主动扫描删除磁盘上因文件轮转或 `CacheSchemaVersion` 升级留下的旧 hash 孤儿分片。理由与 `evidence/` 目录一致——缓存是完全可再生的派生产物，引入引用计数或后台 GC 扫描属于过度设计，`vmr report`/`vmr story` 均可从空缓存目录冷启动。重估触发条件：`.parse-cache/` 目录体积超过同批压缩审计日志总体积（当前全量 34 文件实测 51MB vs 177MB，尚有安全距离），或一次升级后出现异常磁盘占用；在那之前，"需要时整目录删除重建"是比任何 GC 机制更简单可靠的解法。

### 2.5 产出与工程惯例

- **用 Go 结构化代码而非 `text/template` 渲染 Markdown**：复杂条件列、对齐与动态脚注在 Go 里更容易保持类型安全和可读性。
- **不维护外部贡献者 `CONTRIBUTING.md`**：与小团队运作方式不匹配。
- **`archtest` 的文档守卫不扩展到 review 报告类文档**：守卫有意只覆盖 `CLAUDE.md`、设计文档、本文件与用户指南。review 报告会正当地讨论已删除的文件与「建议新增的 XXX 函数」，逼它们与当前状态一致等于逼人改写历史与论证。真正的风险——一份陈旧 review 被当成施工依据——**用定位而非机制解决**：权威的当前状态清单只有本文件一份（它在守卫范围内），review 报告一律是历史记录。
- **`archtest` 不加圈复杂度检查**：一次只加一个守卫。函数长度预算（全局默认 120 + 豁免）落地不久，在它跑满一段时间、确认不够用之前，不引入第二个会同时报警、且更难解释的复杂度指标。
- **`buildinfo` 只输出 VCS commit 哈希，不人工编造语义化版本**：如实反映构建来源。
- **官方用量 API 不预先抽象 `Source` 接口**：遵循 YAGNI，等真正接入第一个厂商私有用量接口时再设计。
- **聚合浮点字段在冷/热缓存两次运行间的 1 ULP 级差异不追查、不消除**（原 §1.24，2026-08-20 重新归类）：真实语料（34 文件/11374 条记录）上，`vmr-report.json` 的 `providers[].cost_estimate` 在冷启动与热缓存两次运行间出现过第 13 位有效数字的差异（`3.35415184208` vs `3.3541518420800003`），其余全部字段（含 11274 行的 `vmr-requests.json`）逐字节相同；两次**独立冷启动**之间无差异。这是浮点加法不满足结合律的教科书现象——不同累加顺序相差 1 ULP——**不是一个可以"修好"的缺陷，是浮点算术的性质**。它原先被登记在 §1（待定问题）里，是分类错误：待定意味着"以后要做点什么"，而这里唯一该做的事（差分/一致性测试用容差而不是逐字节相等）**已经是现状**（`internal/report/e2e_test.go` 用 `1e-6`、`cmd/vmr/quota_parity_test.go` 用 `1e-9*want`）。受影响字段是界面上会四舍五入显示的成本估算，`report.Pricing.Disclaimer()` 本来就声明它是估算。**唯一需要重新当作 bug 的情形**：差异大到影响判断（远超浮点精度量级），或开始出现在 `cost_estimate` 之外的字段上——那说明性质变了，不是这一条了。
- **文件与函数行数预算线是提醒式绊线（Notification / Tripwire），非架构缺陷与必须提前预防的问题**（原 §1.5 重新归类，2026-08-23）：代码随着业务迭代增删而变长变短是自然演进。`internal/archtest` 中的文件行数与函数长度预算（全局默认 700 / 120 + 豁免表）本质上是轻量级的提醒机制（Notification），连 Warning 都算不上，绝非不可逾越的戒律。在未触线前完全无需提前焦虑、更不需要在常规 review 中逐个排查哪些文件接近上限并单列为 Issue；一旦触线，选择合适时机针对性处理即可——或按职责合理拆分重构（如 `detail.go` 拆为 `internal/reqdetail`、`config.go` 拆出 `apikeys.go`），或在逻辑内聚无需强行拆分时临时按实测 +15~20% 调高豁免让其通过。
- **可维护性的核心在整体架构与设计复杂度，而非代码行数或文件长短**（原 §1.15 重新归类，2026-08-23）：单人可维护性取决于系统是否守住 First Principles、KISS/YAGNI，是否消除了不必要的过度设计、抽象包装和复杂分支，而非机械地度量代码行数或两半区的体量比例。代码长了简单拆成更多文件并不会降低真实理解与维护成本，只是形式上符合了数字标准，甚至可能增加概念跳转的间接性。评估系统健康度的真正重点在于「是否为了一个简单功能把问题搞复杂了、是否有更直接简单的解法」。对于探索性的新分析指标，优先用外部脚本消费稳定的 `vmr-report.json` / `journey-*.json` 数据契约进行验证，确认真实价值后再评估主库实现。
- **LLM 解读层生成结构化 Finding 的准入与置信度契约**：允许 LLM 判别器产出结构化 Finding，但必须强制标记 `Source: "llm_inferred"`、结构化离散置信度（`HIGH/MEDIUM/LOW`）与原文 `EvidenceAnchor`。仅 `HIGH` 置信度且具备直接证据锚点的项在报告中以 Finding（⚠️）呈现并在标题标注 `[AI推测]`；`MEDIUM`/`LOW` 仅降级为参考提示，不混入确定性规则事实。**锚点在运行期强制校验**（2026-08，B3，见 §3 第 46 项）：`ComputeLLMFindings` 收集完全部 detector 输出后，逐条按 `strings.Contains(真实 transcript, EvidenceAnchor)` 校验，锚点非逐字子串即丢弃——此前这个机械校验只存在于 `_eval/calibrate_p1b.go`，模型幻觉一个不存在的锚点就能晋升为 `HIGH` Finding 并被当既定事实喂给下一次单-Journey 解读。问法严格约束在有证据支撑的事实性问题上（如 E2 任务完成度重塑为"终步完成声明是否有验证动作支撑"的 `unverified_completion_claim`，拒绝开放式主观质量打分），守住"揭示事实与过程异常而非冒充裁判"的架构边界。

---

## 3. 已闭环，不再重复提出

以下架构问题曾经成立，现已彻底解决并有测试守护：

1. **响应流正规化独立成包**（`internal/respnorm`）：在线响应流状态机、模型名重写与厂商修复脱离 Router，可在纯 `io.Reader` 层面 fuzz。
2. **JSON 字节扫描引擎独立成包**（`internal/jsonscan`）：零内部依赖，消除 `adapter` 与路由层的重复扫描，有 fuzz 保障边界安全。
3. **Agent 方言与任务分段收敛**（`internal/taskseg`）：`report` 与 `story` 共用同一份方言识别、任务切分与真用户指令索引，不再各自实现。
4. **报表聚合与提取解耦**（`internal/report`）：共享 `TrafficStats` 组合结构，单体大函数拆解到 `ingest.go` / `recextract.go`。
5. **公共叶子层职责净化**（`internal/core`、`internal/fmtutil`）：展示层格式化统一下沉到 `fmtutil`，HTTP 响应辅助函数与客户端 header 黑名单（`WriteJSON`/`WriteError`/`FilterClientHeaders`）回到 `router`，`core` 的准入规则写进包注释并对自身存量逐条复核过。
6. **额度与定价引擎精简**（`internal/quota`、`internal/pricing`）：移除分时促销等冗余功能面，固化静态费率覆盖与三层解析。
7. **架构与文档守卫可执行化**（`internal/archtest`）：包依赖边界、文件行数预算、函数长度预算、文档引用有效性全部变成会失败的测试。
8. **文件行数守卫从白名单反转为「全局默认 + 豁免」**（`internal/archtest/file_sizes_test.go`）：与 `func_sizes_test.go` 语义对齐。此前 11 个 ≥400 行生产文件因无人登记而完全不受约束，而已登记文件多 1 行就红——守卫在惩罚已被治理过的地方、放行没被治理过的地方。默认 700 取自实际分布（169 个生产文件，p50 131 / p90 503），恰好使所有超标文件都已在原表中，反转零新增登记。`internal/audit`、`diagnose`、`replay` 的裸奔随之闭环。
9. **`metric: cost` 混合定价端点的静默低估**：`ProviderQuotaRow.WindowUnpricedPct` + §2.5 的 `◇` 标记与脚注。这是「部分退化渲染成精确已知」这一失效模式的第四个也是最后一个实例（前三个：tokens 假零、cost 假零、cost 假 UNKNOWN）。份额以**请求数**而非金额计——没有费率正是它缺失的原因，任何金额都是编造。
10. **`/status` 暴露 `config.Check()` 操作性告警**（`internal/server`、`cmd/vmr`）：当配置存在非 loopback 暴露或探针超时超标等操作性风险时，`/status` 响应直接返回结构化 `issues` 数组，`vmr status` 亦同步渲染 WARNING 提示。
11. **`vmr report` §2 成本表口径提示脚注**（`internal/report/section_cost.go`、`internal/i18n`）：在 §2 成本估算表末尾补充口径提示脚注，明确说明估算成本包含未嗅探 usage 的降级估算部分，而 Token 列仅统计已确认数量，消除按「估算成本 ÷ Token」反推单价偏高的误导。
12. **`respnorm` 检查方法并发安全与 `copyFlush` 生命周期同步**（`internal/respnorm`、`internal/router`）：`NormalizerStream` 的所有导出查询方法（`Applied`、`RawPreStrip`、`ObservedModel`、`Usage`、`OutTokens`）统一由互斥锁同步，彻底消除客户端断开或超时提前返回时 reader goroutine 尾读导致的数据竞态风险。
13. **客户端流式中途断开在审计日志中精确标注 `canceled`**（`internal/router`、`internal/server`）：当客户端在流式传输中途断开或主动取消时，`router` 将 attempt 标记为 `canceled`（`canceled by client`），`server` 将审计记录的 `Outcome` 标注为 `canceled`，消除将未完成请求误计为成功的统计失真。
14. **图片降采样磁盘缓存容量上限**（`internal/imgprep`）：在 TTL 清理的基础上增加 `defaultCacheCapBytes`（50MB）全局容量上限，超出时按 mtime 自动淘汰最旧条目，且与 TTL 开关解耦——`image_cache_ttl_days<=0`（保留全部条目）时容量上限依然生效，不会跟着一起失效。清扫本身沿用既有的「至多每天触发一次」节流，所以这是一个最终收敛到 50MB 的上限，不是任意时刻都成立的硬顶：单日内的突发写入可能在下一次触发的清扫之前短暂超出。
15. **Compare 报告"证据溯源"改为按 Journey 精确定位**（`internal/story/storyindex.go` 新增 `SourceFiles`、`cmd/vmr/cmd_story.go`）：此前直接透传本次 `vmr story` 扫描到的全部输入文件，跟两个被比较的 Journey 实际用到哪些文件无关；改为从 `vmr-stories.json` 已经算好的每个 Journey 的 `Files` 字段取并集去重。用真实日志验证：无关文件从 9 个降到 0 个。
16. **Compare 报告 LLM 解读标题层级**（`internal/i18n/story_llm.go`）：两段 LLM 解读（整体对比、分叉点）各自内部的三个子标题原来跟外层 `## LLM 解读（...）` 平级，改为 `###` 子标题——改动在发给 LLM 的 prompt 文本里，不在渲染代码。
17. **Journey 报告决策脊柱多行/超长工具调用参数默认折叠**（`internal/story/render_spine_args.go`）：`payloadBlock` 原先对多行或超长单行参数直接展开成一个不折叠的围栏代码块；现在折叠，`<summary>` 里放一段拉平截断的预览（`spinePreviewLen`），展开才是完整内容。
18. **Journey 报告决策脊柱 Step 的原始消息不再截断**（`internal/story/render_spine_step.go` 的 `foldWhyLine`，取代原 `spineWhyLine` 的硬截断）：原先 `RespText`/`Reasoning` 超过 400/200 字符直接截断丢弃尾部；现在改为跟 `payloadBlock` 一致的折叠惯例——短文本内联，长文本折叠展示预览、展开为完整原文，永不丢内容。
19. **Journey 报告 system prompt 移至文档头部、按出现顺序折叠一次**（`internal/story/render_md_sysprompt.go`）：原先只在 Step 1（或后续 `SysChanged` 的 Step）的 Messages 区块里出现一次，但恰好挡在决策脊柱与 Step 内容前面；现在在文档最开头单独渲染一个折叠区块（有多个版本时逐个列出各自的 Step 覆盖范围），Step 的 Messages 区块不再重复它。
20. **决策脊柱工具调用结果配对改为三级降级**（`internal/story/findings_toolresult.go`、`render_spine_step.go`）：原先只按精确 `tool_call_id` 匹配，而 OpenClaw 家族客户端在回写工具调用历史时会去掉下划线（根因是客户端，不是上游 provider/网关），导致这条链路的配对成功率实测为 0%。现按"精确 ID → 归一化 ID（去下划线）→ 同 Step 内按位置"三级降级：前两级仍是精确的一一匹配，可作为 Finding 检测器的证据；只有第三级是推断，限渲染层使用且标注"按位置推测，ID 未匹配"。
21. **决策脊柱覆盖补全至 100%**（`internal/story/render_spine_step.go`）：原先只渲染有 `ToolCalls` 的 Step，一个 Task 若没有任何 Step 调用工具、或整条 Journey 都没有工具调用，会整段/整个不渲染。现在每个 Step 都渲染：无工具调用的 Step 降级为一行摘要（任务中途的新用户指令渲染"💬 指令"，否则渲染"💬 汇报"取自 `RespText`/`Reasoning`），脊柱末尾新增"最终交付物"小节（复用 `-compare` 已有的 `deliverableStats` 检测）。
22. **Compare 报告开篇展示两侧完整初始 User Message**（`internal/story/compare.go` 新增 `InitialInstructionFact`/`initialInstructionStats`、`render_compare.go` 的 `renderInitialInstruction`）：从 `Journey.Events` 的首个 user-role 事件取未截断原文，以 2000 字符为界折叠展示在两侧摘要下方；挂在 `ComparisonExtras` 上，随之自动进入 `-llm-addr` 的证据包，无需额外接线。
23. **LLM 解读小节标题层级渲染层兜底**（`internal/story/llm.go` 的 `downgradeH2Headings`）：条目 16 的 prompt 侧调整只是第一道防线，不保证模型遵从；现在 `RenderLLMSection` 对返回文本做一次确定性降级——围栏代码块之外、行首 `## ` 一律降为 `### `——文档目录结构不再依赖模型的指令遵从度。
24. **Journey 报告 fact-layer 删除，脊柱挂详单链接，系统提示词改为引用**（P5，`internal/story/render_md.go`/`render_md_sysprompt.go`/`render_spine_step.go`/`internal/story/ensure_details.go`）：本清单曾经的 §1.20 记录的目标状态已完全达成——决策脊柱之后 `## t01 · ...`/`### Step N ...` 那一整段重复的 fact-layer 渲染函数（`renderStep`/`renderLLMResponse`/`renderEvent`）已整体删除；每个 Step 改为携带一条指向自己完整记录的"→ detail"链接（`reqdetail.FileNameForManifest`，渲染时按需生成，无需先跑 `vmr report -details`）；`Edit`/`StitchEdge`/`SysChanged`/`Compaction`/`NoReply` 这五类跨记录分析事实（详单渲染器物理上无法重建）原样搬进决策脊柱本身，常规 `Append` 编辑不再逐步显示（默认状态，无信息量）；系统提示词头部从内联全文改为链接到共享证据条目。**顺带修复了一个独立于本次重构的既有缺陷**：系统提示词版本分组（`systemPromptEras`）原先靠扫描每个 Step 新引入的消息判定分组边界，但结构上只能在 Journey 第一步或缝合边界检测到变更——同一条 Lineage 内部发生的系统提示词变更（如会话中途切换模型/工具集）完全检测不到；现在改为直接按 `Manifest.HasSys`/`SysHash` 状态机分组，与决策脊柱自己的"系统提示词变更"判据用同一对字段，不会再互相矛盾。真实语料验证：22 步/33 次调用的样例 Journey，报告体积从 ~312KB 降到 ~107KB；`vmr story` 生成的详单与 `vmr report -details` 对同一条记录（含缝合边界记录，`prev` 均为 `nil`）生成的详单逐字节相同（`TestEnsureJourneyDetails_MatchesReportDetails`）。
25. **`vmr replay -req` 免位置参数**（P6.5，`internal/replay/replay.go` 的 `resolveReqAuditPath`/`statAuditPathArg`）：曾经的 §1.25——`-req` 现在可以省略位置参数（按坐标 basename 在当前目录/`config.yaml` 的 `log_dir` 下自动定位，含 `.zst` 变体），或传一个目录代替 cwd；传具体文件路径时仍保留原有一致性校验。从 `vmr-requests.json`/`journey-*.json` 复制的 `req` 字段现在可以直接贴进命令行执行。
26. **导航矩阵六条边补齐，会话身份改为内容寻址，索引分类，自指流量默认排除**（P6.1–P6.4）：`report` 的 `SessionInfo.ID`/`SessionRow.ID` 从 run-scoped `s%02d` 改为底层 `ctxgraph.Lineage` 的内容寻址身份（`l-<hash8>`），与 `story` 的 `JourneyIndexRow.Lineages` 直接集合可 join，位置序号降级为仅供人读的 `alias`；`vmr-report.md`→`stories/vmr-stories.md`、会话行→journey、journey→返回入口、详单→返回索引等六条导航边补齐，真实语料端到端走查确认无死链接；`vmr-stories.md` 按标题内容标记（`[cron:...]`/`[OpenClaw heartbeat poll]`/`[Subagent Context]`，真实语料核实拼法）分类，噪声类默认折叠；`vmr story -llm-addr` 自身产生的分析流量默认从 `vmr report` 的成本统计与 `vmr story` 的候选任务列表中排除（识别规则基于 `report.yaml` 的 `llm_key` 派生，两侧共用同一次计算），`-include-self-traffic` 可关闭。
27. **默认 `vmr report` 产出的请求索引死链接清零**（P7.1，`internal/report/requests.go`）：`detailLink` 改为 `detailCell(r, detailsOn)`——默认（`-details=false`）模式渲染 `r.Req`（已发布的 `basename:line` 坐标）为行内代码，`-details` 模式保持原有 Markdown 链接。真实语料验证：单日样本 `vmr-requests.md`/`vmr-requests-failed.md` 默认路径下死链接从 322/322、7/7 降到 0；`-details` 模式 322/322 链接 100% 可达。曾登记为 §1.31。
28. **决策脊柱指令展示的方言过滤漏洞补齐**（P7.2，`internal/taskseg/openclaw.go`/`openclaw_brackets.go`、`internal/story/journey.go`/`render_spine_step.go`）：`openClawAware.RealUserText` 此前只在 `(untrusted metadata)` 信封分支内间接处理时间戳方括号，"裸"消息（无信封）上的 `[timestamp]`/`[message_id: ...]` 脚手架前缀完全未被剥离；新增 `messageIDBracketRe` 与窄范围的 `timestampBracketRe`（仅匹配 OpenClaw 的日期前缀形状，不是通配任意方括号——P7 内部独立复核发现首版误用了通配的 `leadingBracketRe`，会误伤"用户消息本来就以方括号开头"的合法场景，如 `[Bug] fix the crash`，已改用窄正则并补回归测试），裸消息路径循环剥离两种已知前缀。脊柱"💬 指令"行同时从"直接读未过滤的 `NewEvents` 原文（`firstNewUserText`）"改为读 `buildFrom` 构建期已算好并存入 `Step.Instruction` 的过滤后文本（`taskseg.LastInstruction`），与任务标题走同一套过滤规则；**同一次复核还发现该行原先只在无工具调用的 Step 上渲染**（P1.2/P6 遗留缺口，`renderSpineStep` 从未接入过），中途指令绝大多数情况下会立即触发工具调用，导致该行此前在真实场景里近乎不可见——已同步补齐 `renderSpineStep` 的渲染分支。真实语料验证：含 `[message_id: ...]` 标记的样例任务标题不再泄漏该标记（此前 `**t01 · [Tue 2026-07-28 00:05 GMT+8] [message_id: om_x100b694c53b4eca8b1cd50932b7aefe] o…**`，现为 `ou_ad279066d244fb4f7d91240743d30935: 去统计一下…`）。曾登记为 §1.21。
29. **`-llm-addr ''` 现在能真正关闭 LLM 调用**（P7.3，`cmd/vmr/reportconfig.go`/`cmd_story.go`）：新增 `resolveStringExplicit`（与既有 `resolveBool(explicit, ...)` 同构），`cmd_story.go` 的 `-llm-addr` 解析改用它——显式传空串不再回退到 `report.yaml` 的默认地址。真实验证：配置了 `llm_addr` 的目录下，`vmr story -llm-addr ''` 不再发起 LLM 调用；不传该 flag 时仍按 `report.yaml` 默认值触发（沿用既有行为）。曾登记为 §1.28。
30. **JSON 输出的语言策略统一：`journey-<id>.json`/`compare-*.json`/`vmr-report.json` 三种产物同一次 `-lang` 下语言一致**（P8，`internal/story/compare.go`/`compare_metrics.go`（新增）、`internal/report/metrics.go`、`cmd/vmr/cmd_report.go`/`cmd_story.go`）：落地方向见 `docs/future-strategy/json_lang_policy_plan_sonnet-5.md`，回填进 `docs/VirtualModelRouter_Design_v4_Analytics.md` §4.3（整节重写，不是打补丁）。`story.Compare(a, b JourneySummary)` 改为 `Compare(a, b JourneySummary, lang i18n.Lang)`，循环体改用 `i18n.MetricLabel(lang, string(spec.Code))` 直接算出本地化 `Label`（`metricSpec.Label` 字段随之删除——改动后全仓唯一读取点消失）；`render_compare.go` 的渲染层不再重复查表，直接读 `cmp.Rows[].Label`。`report` 侧刻意**不**给 `Build`/`BuildCached` 加 `lang` 参数（两者已有 6/9 个参数，且没有任何内部逻辑依赖 `rep.Efficiency` 的英文文本本身），改为新增导出函数 `report.LocalizeEfficiency(rep, lang)`，`cmd_report.go` 在写 JSON 前调用它覆写 `rep.Efficiency`；`section_efficiency.go` 的 Markdown 渲染路径刻意保留独立计算，不依赖这条覆写的调用顺序。真实语料验证：同一次 `vmr report -lang zh` 下 `vmr-report.json` 的 `efficiency[].finding`（如"工具 schema 浪费"）与 `vmr-report.md` §7 表格文案逐字一致，且与 `-lang en` 选中的 `Code` 集合完全相同（证明覆写只改文本、不改选择逻辑）；`vmr story -compare` 的 `compare-*.json` 的 `rows[].label`（如"模型时间"）与 `compare-*.md` 表格逐字一致。曾登记为 §1.19。
31. **`vmr analyze` 大语料 SIGKILL 根治：默认渲染范围收窄 + 批处理，两者缺一不可**（P9.2，`cmd/vmr/cmd_analyze.go` 的 `taskOnlyCandidates`、`cmd_story.go` 的 `renderJourneys`/`renderBatchSize`）：`vmr analyze` 默认套件（无选择器）现在只物化 `category == task` 的候选（`cron`/`heartbeat`/`subagent` 仍进索引，`heartbeat`/`subagent` 按既有规则折叠、`cron` 与 `task` 一样留在主表，只是"报告"列显示未生成），`-render-all` 保留全量物化。**但真实全量语料（34 文件、11274 条记录）验证发现，仅做分类过滤不足以解决 SIGKILL**：`task` 类候选虽只占全部候选数的 49%（234/473），却占了 83.5%的请求量（9259/11086）——真正长的任务对话本来就分在 `task` 类，过滤类别并不能按比例削减渲染批次的实际数据量。根因定位到 `story.BuildAll` 内部对整批候选做**一次性** `ctxgraph.FetchRecords`（`internal/story/journey.go`）——这原本是刻意的 I/O 优化（共享一次文件扫描），但在大批量下变成无界内存：`renderAllJourneys` 默认套件仅按类别过滤后拿 234 个候选一次性构建，实测峰值内存（`peak memory footprint`）约 35.5GB，进程被系统杀死。修法：`cmd_story.go` 的 `renderJourneys` 改为按 `renderBatchSize`（20）分批调用 `story.BuildAll`，每批构建完立即写盘、下一批开始前上一批可以被 GC——不改变 `internal/story`/`internal/report` 任何一行（改动全在 `cmd/vmr`）。真实语料验证：同一份 34 文件语料，`vmr analyze` 默认套件（234 个 task 候选、9259 条请求）从 SIGKILL（约 35.5GB 峰值）→ **正常退出**，峰值内存 **4.59GB**，总耗时 413s（~6.9 分钟），输出目录 3.5GB（`stories/` 58MB + `details/` 8343 个文件，约 3.44GB）。曾登记为 §1.30/§1.32。**2026-08-21 复查订正：本项闭环的是 SIGKILL（`§1.30`）这一个症状，`§1.32`（`analyze` 抵消 P3.3 的"默认按需"）的闭环判定已撤回**——形态只是从"强制 `-render-all`"换成了"默认渲染全部 task 候选、每步物化详单"，默认路径仍写 3.5GB 派生产物，纪律未成立。真实根因不是"渲染了不该渲染的候选类别"，而是 `writeJourneyFile` 无条件调用 `EnsureJourneyDetails`，不区分单条下钻与批量渲染。重新登记为 `§1.35`。
32. **`vmr report`/`vmr story` 降级为过渡别名，`vmr analyze` 成为单一分析入口**（P9.1/P9.3，`cmd/vmr/cmd_analyze.go`/`cmd_report.go`/`cmd_story.go`/`cmd_story_setup.go`）：`vmr analyze` 收敛为一套 flag 集合（`vmr report`/`vmr story` 曾各自拥有的并集），`-journey`/`-compare`/`-corpus` 三个互斥的变焦选择器各自只跑对应的 story 侧单一视图（不跑宏观报表）；不带选择器是默认套件，先跑 story 半区再跑 report 半区（`report.Markdown` 需要 `stories/vmr-stories.md` 已存在才挂链接，story 先跑让这条边在首次调用就命中）。`cmd_report.go`/`cmd_story.go` 提炼出共享执行函数 `runReport`/`setupStoryRun`（连同既有的 `renderJourney`/`renderJourneys`/`renderAllJourneys`/`compareJourneys`/`corpusStats`），`cmd_analyze.go` 只做 flag 解析与按选择器分派，不重新实现任何渲染/聚合逻辑；`vmr report`/`vmr story` 各自保留独立的 `flag.NewFlagSet`、独立默认值，产出与收敛前逐字节相同，仅在调用时向 stderr 打印一行迁移提示。全程未改动 `internal/report`/`internal/story` 任何一行（`git diff` 验证为空）。曾登记为 §1.33 两项遗留（执行顺序文档订正、README 补 `vmr analyze` 示例）。
33. **四处文档"先 report 后 story"执行顺序订正**（P9.4，`docs/UserGuide.md`/`.zh`、`docs/VirtualModelRouter_Design_v4_Analytics.md`、`CHANGELOG.md`）：均已改为准确描述"story 先、report 后"；`README.md`/`README.zh.md` 补充 `vmr analyze` 快速上手示例与能力条目（`grep -c 'vmr analyze'` 从 0 变为 2）。曾登记为 §1.33。
34. **自指流量识别规则的输入不对称随统一 flag 集合自然消失**（P9.5，`cmd/vmr/cmd_analyze.go`）：`cmdAnalyze` 只解析一次 `llmKey`（`-llm-key` flag 或 `report.yaml` 的 `llm_key`），同时喂给 story 半区（`filterSelfTrafficCandidates`）与 report 半区（`selfTrafficExcludeTags` → `runReport` 的 `excludeClientTags`），不再是"`vmr story` 能被 flag 覆盖、`vmr report` 只认 `report.yaml`"两条独立路径。落地过程中发现一个真实的连带缺陷并修复：`resolveLLMOptions`（校验 `-llm-addr`/`-llm-model`/`-llm-key` 组合）若无条件调用，会让单独设置 `-llm-key`（不带 `-llm-addr`，这正是"仅用于排除自指流量、不在这次运行发起 LLM 调用"的合理用法）在 `-corpus`/默认套件模式下直接报错退出——即便这两种模式从不消费 `llmOpts`。修法：`resolveLLMOptions` 只在 `-journey`/`-compare` 分支里按需调用（`dispatchAnalyze` 的 `resolveLLMOpts` 闭包），`llmKey` 本身的解析与自指流量排除逻辑不再依赖它。真实语料验证：`vmr analyze -llm-key <key>`（`report.yaml` 不存在、无 `-llm-addr`）在默认套件下正常运行，`vmr-stories.json` 与 `vmr-report.json` 的 `meta.self_traffic_excluded` 排除同一批自指流量记录。曾登记为 §1.34。
35. **P2/P3 遗留死代码清空，文档引用守卫扩展到源码注释**（P11.1/P11.2/P11.4，`internal/ctxgraph`/`internal/story`/`internal/chatmsg`/`internal/reqdetail`/`cmd/vmr`/`internal/archtest`）：`story_report_full_review_opus-5.md` §2.6 把六个"非缓存版/单条版"函数整批列为待删死代码，P11 开工前用 `deadcode` 的 `main()` 可达性分析 + 逐个读调用点复核，发现这个判定对其中五个是错的——它们要么是缓存正确性/单双遍等价性差分测试的独立参照实现，要么是 `internal/ctxgraph`/`internal/story` 两个包几乎每个测试文件搭建 fixture 的公共构造入口，且都被 Phase 1b 校准工具（`§1.18`）直接调用，只是该工具所在目录名前缀下划线，`go build ./...` 的可达性分析天然看不到它，不代表是死代码；`health.Registry.Available` 同样从待删名单移出，它是唯一无副作用的可用性查询方法，两个包的测试断言依赖它检查状态而没有替代路径。**实际删除的**：一个完全自我闭环、除自己的专属测试外无任何消费方的废弃索引子系统（连同其在 `Graph` 上的公开字段与扫描期的填充循环）、六个真正零引用的小函数（含 `deadcode` 复扫时新发现、原复查未列出的一个 —— `vmr check` 的旧版本 provider-proxy 渲染器，早被同文件里的新版本取代，其三个专属测试改为直接测试两者共享的底层逻辑，覆盖不丢）。同时把 `internal/archtest` 的文档引用守卫从"只覆盖顶层 docs/ 与 README"扩展为常驻测试 `TestArchitecture_DocReferences_SourceComments`，覆盖 `internal/`+`cmd/` 全部非测试源文件的注释——本次复查曾指出这类守卫"只守文档、不守源码注释"是本轮死引用两阶段未被发现的原因，扩大扫描范围后当场额外揪出两处同类历史死引用与三处因包名列表未加分隔符导致路径正则误判的假阳性，均已订正。曾登记为 §1.39（判定与范围均在 P11 开工前的重新核实中大幅修正）。
36. **详单跳过谓词从"假设"改为"校验"**（P12.1，`internal/reqdetail/render.go`/`detail.go`/`ensure.go`）：`EnsureRendered` 曾经的跳过条件是 `os.Stat(target) == nil`，把"文件名可从 Manifest 算出"（成立）误当成"同名文件内容正确"（不成立）——`Render` 的输出还依赖文件名不携带的 `lang`/`linkEvidence` 两个轴。改法：`Render` 输出的第一行固定写入一行机器可读、渲染时不可见的 HTML 注释指纹（`<!-- reqdetail:v<模板版本> lang=<en|zh> evidence=<true|false> -->`，`renderFingerprint`），`EnsureRendered` 改为有界读取目标文件的第一行（`readRenderFingerprint`，只读一行，不读整份文件）与期望指纹比对，不匹配（含"文件不存在"）才重新渲染并原子覆盖写入；新增的 `renderTemplateVersion` 常量是给未来"改变 `Render` 输出形状但不改变文件名"的改动（例如详单内容减法）预留的第三个失效维度，届时只需把这个数字加一，不需要再引入新参数——`TestEnsureRendered_RewritesOnStaleTemplateVersion` 用一个手写的旧版本指纹钉住这条路径本身可用，不必真的先动这个常量。**独立外部审阅（gemini-3.7-flash）当场指出首版实现有一处真实回归**：`linkEvidence` 分支被放在指纹比对之后，导致"evidence 模式不变、detail 页面指纹已匹配"这一种情况会在函数第一个 `return` 处直接退出，`EnsureSysPromptEvidence`/`EnsureToolsEvidence` 根本没有机会执行——这恰好是本条要修的 M9 场景本身（删掉 `evidence/` 后重跑），只是从"文件层面整体命中跳过"换成了"指纹匹配后跳过"，同一个 bug 换了个位置原样复现。修法：两个 `Ensure*Evidence` 调用挪到指纹比对**之前**、只要 `linkEvidence` 为真就无条件执行——它们自带的内容寻址幂等检查（`ensureEvidenceFile` 内部的 `os.Stat`）让命中的常见情形仍是一次廉价 stat，不是每次都重新生成。复核方法：先按审阅指出的错误顺序在本地临时还原一遍，确认新增的 `TestEnsureRendered_RebuildsDeletedEvidence`（evidence 模式全程不变、只删除 `evidence/` 目录后重跑，断言证据文件被重建）确实先失败、修复后再通过，而不是仅凭代码走查判定。真实语料验证（`~/.vmr/logs` 下 2 个文件 18 条真实记录，改动前后各跑一次 `vmr report -details`）：18 份详单逐份核对均为**恰好新增一行指纹**，无其它字节差异（这批真实记录都不含 `<`/`>`/`&`，未触发 P12.2/P12.3 的转义路径）。常驻回归测试：`TestEnsureRendered_RewritesOnLanguageChange`（同目录先 EN 后 ZH，详单从"① Client → VMR Request"变为"① Client → VMR 请求"）、`TestEnsureRendered_RewritesOnEvidenceModeChange`（内联切到证据链接）、`TestEnsureRendered_RebuildsDeletedEvidence`（真正的 M9 场景：evidence 模式不变，删除后重建）、`TestEnsureRendered_RewritesAFileWithoutAMatchingFingerprint`（无指纹的旧版本详单文件被判定为过期并重写）、`TestEnsureRendered_RewritesOnStaleTemplateVersion`。曾登记为 §1.41。
37. **`internal/story` 的原文注入点补齐转义**（P12.2/P12.3，`internal/story/render_spine_args.go`/`render_spine_step.go`/`render_md.go`/`storyindex.go`/`render_compare.go`）：把 `internal/reqdetail` 已有的 `escapeHTML`/`escapeCell` 分别导出为 `EscapeHTML`/`EscapeCell`（`internal/reqdetail/render.go`），`internal/story` 新增两个三行的本地薄包装直接调用它们（`render_md.go`），不再维持独立实现——`internal/story` 已经在生产代码里 import `internal/reqdetail`（`ensure_details.go`/`render_spine_step.go` 的 `FileNameForManifest`），下沉不新增依赖边界，也是这次真正堵住"两份拷贝会静默漂移"这个已经发生过的失效模式的地方（`codeFence` 继续按原样各自保留一份，理由不变——它不依赖这个失效模式）。**修法范围比原登记的 3 处 `<summary>` + 2 处 inline 分支扩大了 7 处，分两批发现**：P12 开工前通读决策脊柱两个文件全文（而不只是原登记点名的行号）先发现 4 处（`toolCallLine` 的 `allShort` 分支、`SpineTaskLine`/`SpineInstructionLine`（两处调用点）/`SpineReportLine`（两处调用点）），全部按"截断/拉平在先、转义在后"处理，避免转义膨胀打乱既有截断长度语义；`codeFence` 包裹的完整内容不转义（CommonMark 不解析围栏代码块内的 HTML，这是判断要不要转义的唯一标准）。**独立外部审阅（gemini-3.7-flash）随后指出这一批的边界划得不对**：`render_md.go` 已经因为要放 `escapeHTML` 薄包装而被本次改动触及，声称它是"未计划修改的文件"不成立；更重要的是 `storyindex.go` 索引表格里的标题列比已修的 `<summary>` 注入点更严重——原文里一个 `|` 字符（例如任务标题引用了 `ps aux | grep vmr` 这样稀松平常的 shell 管道）会被 GFM 表格解析成额外一列，撕裂 `vmr-stories.md`（**首要导航入口**）的那一整行，不是"读者少看一段内容"，是"这一行、以及视觉上跟在后面的整张表格都乱了"。核实后追加修复 3 处：`render_md.go` 的 `j.Title`（顶层引用块）、`storyindex.go` 的标题列（`escapeCell(escapeHTML(...))`，两个函数一起用——单独 `escapeCell` 挡不住 `<!--`，单独 `escapeHTML` 挡不住 `|` 撕裂表格）、`render_compare.go` 的 `SideBlock`/`DivergenceHeavy`/`DivergenceLight` 的标题参数（复核 `storyindex.go`/`render_compare.go`/`render_md.go` 全文时一并发现，审阅本身未点名后两处的 `Divergence*` 部分）。至此全部 12 处原文注入点统一处理完毕，不再保留"明确不做的扩大"这条登记（原计划留出的 `§1.44` 已随之撤回）。常驻回归测试用 DevPlan 引用的真实反例形状（`"<!-- Ver 2026-07-24 14:45, by Sonnet 5 --> real content after"`）钉住：`render_spine_args_test.go` 的 `TestToolCallLine` 新增 4 个子测试（inline/折叠/`allShort`/`<script>`+`&` 组合）、`render_spine_test.go` 新增 `TestRenderDecisionSpine` 的 4 个子测试加独立的 `TestFoldWhyLine_Escapes`/`TestToolResultLine_EscapesSummary`、`storyindex_test.go` 的 `TestRenderStoryIndexMarkdown_EscapesTitle`（断言标题里的 `|` 转义为 `\|` 而非撕裂列数）、`render_md_test.go` 的 `TestRenderMarkdown_EscapesJourneyTitle`、`compare_test.go` 的 `TestRenderComparisonMarkdown_EscapesTitles`。曾登记为 §1.37。
38. **证据层体积纪律归位：批量渲染只挂链接、详单内部两处逐字复制削减、链接判据从 flag 改为事实**
    （P13.1–P13.4，`cmd/vmr/cmd_story.go`/`cmd_analyze.go`、`internal/reqdetail/detail.go`/
    `render.go`、`internal/report/requests.go`/`rows.go`、`cmd/vmr/cmd_report.go`）：这条纪律
    第四次被提出（P3.3 → P6.5 → P9.2 → 本次），前三次都因为没有守卫而退化，详见下文。
    - **P13.1**：`writeJourneyFile`/`renderJourneys`/`renderAllJourneys` 新增 `materializeDetails`
      入参——单条下钻（`renderJourney`、`-compare` 的 `ensureJourneyFile`）与 `-journey` 命中多个
      （即使解析出多个目标，仍是用户点名，不是默认套件的隐式批量）恒传 `true`；`-render-all`
      （无论是 `vmr story -render-all` 的完整全量，还是 `vmr analyze -render-all` 把默认套件范围从
      task-only 扩到全部候选）也传 `true`——这是用户主动要求"我全都要"；只有无 selector 的默认套件
      （`cmd_analyze.go` 的 `taskOnlyCandidates` 分支）传 `false`。脊柱的"→ detail"链接文本是
      Manifest 的纯函数，与目标文件是否存在无关（`EnsureJourneyDetails` 自身 doc comment 已经这样
      论证），所以批量模式下链接照常渲染，只是可能暂未物化，不产生死链接。
    - **P13.2**：`renderClientResponse` 的 `Details(t.RawSSEFull(...), codeFence(body))`
      （响应体原始 SSE 全文，`rec.Client.Response.Body` 的逐字复制）改为一行带 `ctxgraph.ReqCoord`
      坐标的取用提示（`RawSSERef`，指向 `vmr replay -print -req <coord>`）；`renderStreamSummary`
      重组后的模型输出（reasoning/content/tool_calls）不变——那是解读，不是复制。
    - **P13.3**：`renderClientRequest` 的消息渲染循环新增折叠分支——`haveDelta && i < deltaStart`
      时不再逐条渲染历史消息，只在第一次命中时输出一行指向 `prev` 自己详单页的链接
      （`HistoryFoldedNote`，复用 `FileNameForManifest`）；`prev == nil`（lineage 首条、缝合边界）
      或 `deltaStart == 0`（同一 lineage 内容全量重置）时不折叠，全文渲染，链条有起点。
      `renderTemplateVersion`（P12 交付）从 1 提到 2，让所有已写盘的旧详单在下次 `EnsureRendered`
      时被判定为过期并重写，不需要引入第四个指纹参数。
    - **P13.4**：`internal/report` 的 `detailCell` 判据从"本次是否开了 `-details` 开关"改为
      `r.DetailFile` 是否真实存在于 `details/` 目录——`vmr analyze` 的 story/report 两个半区各自
      可能独立物化，纯 flag 判据在这之后就不可靠。`Meta.DetailsEnabled`（喂给报表 §8 的整体性
      判据）改为 `opts.detailsOn || detailDirHasFiles(detailDir)`（`detailsPresentFor`，
      `cmd_report.go`），在 `report.Markdown` 渲染前、`BuildCached` 之前检查——即报表半区自己的
      `-details` 写入尚未落盘时，也能看到 story 半区（P13.1 的 `-render-all` 路径）已经写下的文件。
    - **P13.5**：新增 `TestCmdAnalyze_DefaultSuiteDoesNotMaterializeDetails`（默认套件 `details/`
      为 0 或不存在、脊柱链接仍渲染；`-render-all` 材料化非空）——人为把 P13.1 的判断改回"无条件
      物化"，测试当场失败，改回来后通过，不是只凭代码走查判定守卫有效。
    - **真实语料验证**（本仓库 `logs/` 下 2026-07-15 单日 394 条记录、07-15 全量 34 文件 11353 条
      记录）：默认 `vmr analyze` 从 **47MB / 253 份详单 → 3.0MB / 0 份详单**；`-render-all` 从
      **49MB（`details/` 45MB）→ 10MB（`details/` 6.1MB）**，同一批 302 份详单体积降约 86%；
      `vmr report -details` 与 `vmr analyze -render-all` 对同一批记录生成的 302 份共有详单逐字节
      比对**全部相同**（P2 核心不变量，改动前后各跑一遍二进制对照）。
    - **独立外部审阅（gemini-3.7-flash）核查处置**：审阅走查 `ensureJourneyFile`
      指出一处 P13.1 引入的真实回归——该函数发现 `journey-<id>.md` 已存在（例如默认套件先渲染过、
      未物化详单）时直接 `return nil`，导致后续 `-compare` 点名同一个候选永远不会补齐它的详单，
      链接永久 404，且无论重跑多少次都不会自愈（P13.1 之前"`.md` 存在 ⇒ 详单也存在"这条假设一直
      成立，P13.1 把它悄悄破坏了）。**采纳，已修复**：`ensureJourneyFile` 的早退分支也无条件调用
      `EnsureJourneyDetails`（P12 的指纹幂等检查让已物化的 Step 是一次快速跳过，不是重新渲染）；
      复核方法与 §36 一致——先在本地还原审阅指出的错误顺序，确认新增的
      `TestCmdAnalyze_CompareMaterializesDetailsEvenIfReportAlreadyExists` 先失败，再验证修复后
      通过。审阅还指出 `detailCell` 若对全量语料的每一行都做一次 `os.Stat`，请求索引的多个渲染
      函数（会话卡片、定时任务卡片、失败索引、全量表）会叠加出两万次以上的系统调用；**采纳，
      已优化**：新增 `buildDetailFileSet`（一次 `os.ReadDir` 建立 `map[string]struct{}`，过滤隐藏
      文件与非 `.md` 条目），`WriteRequestsIndex`/`WriteFailedIndex` 各自在入口调用一次，
      `detailCell` 改为纯内存 `map` 查找；外部 API（两个 `Write*Index` 函数自身的签名）不变，
      `cmd_report.go`/既有测试无需跟着改。审阅额外建议的两个测试补强（`-journey` 精准物化断言、
      `linkEvidence=true` 且 `LCP=0` 边界）均已采纳，作为
      `TestCmdAnalyze_JourneySelectorMaterializesOnlyItsOwnDetails`/
      `TestRenderClientRequest_EvidenceLinkedZeroLCPDoesNotFold` 落地。
    - **本条闭环的是 §1.35（体积纪律从未成立）与 §1.36（详单内部约 93% 是重复拷贝）两项**，详见 Phase 13 执行记录。

39. **索引"显示"与套件"渲染"两条噪声判据合一**（P14.1，`internal/story/storyindex.go` 新增
    `IsNoiseCategory`、`cmd/vmr/cmd_analyze.go` 的 `renderableCandidates`）：只把 `heartbeat` 归为
    噪声——真实语料实测 107 条 heartbeat 候选无一条达到 10 请求，而 cron（112 条里 44 条 ≥10 请求，
    max 30）与 subagent（20 条里 16 条 ≥10 请求，max 91——全语料最长的一条）都不该被折叠/不渲染。
    `internal/i18n/story_index.go` 的 `NoiseFoldSummary` 文案此前已经与代码行为脱节（声称折叠
    "定时/心跳/子代理"，实际代码从未折叠 cron）——这是复查阶段未发现的第三处矛盾，随本次改动一并
    订正为只提 heartbeat。真实语料验证（07-15 单日）：默认 `vmr analyze` 从"12 task + 10 cron 全部
    可见，仅 8 行有链接"变为 18/19 行全部可点（1 条 heartbeat 折叠）；全量语料默认渲染候选数从
    238 条增至约 370 条，`details/` 目录默认路径下仍为空（P13 的"批量不物化"纪律不受影响）。
    曾登记为 §1.42。
40. **检测器/指标覆盖率披露：语料非 100% anthropic-messages 协议时，读者能分辨"检查过没问题"与"结构性
    测不出来"**（P14.2，`internal/story/corpus_coverage.go`（新增）、`render_corpus.go`、
    `render_spine.go`、`internal/i18n/story_corpus.go`/`story_spine.go`）：`chatmsg.ToolResult.
    IsError` 只在 anthropic-messages 协议下有意义，实测全量语料协议分布 openai-completions 99.48% / anthropic-messages 0.52% ——
    依赖它的信号在 99.48% 的语料上结构性沉默，但产物里此前没有任何地方区分"沉默"与"干净"。
    首版实现只登记了 `error_retry_unadapted`/`error_recovery_count` 两项、只在 `-corpus` 报告
    披露，一次并行的独立审阅（gemini-3.7-flash，2026-08-21）当场发现两处遗漏并已核实修复：
    - **遗漏的受限项**：`chatmsg.RenderPart` 把 `IsError` 编码进文本标记 `isErrorMarker`
      （`"❌ is_error"`），除 `ToolResult.IsError` 直接读取的两项外，还有
      `FindingUnverifiedSuccess`（`error_then_unverified_success`）、决策脊柱自身的工具结果
      ❌ 徽标（`render_spine_step.go`）、`structure.json` 的 `ToolCalls[].ResultError`、以及
      `-corpus` 独有的 Context Rot／Tool Sequence 两个错误率栏目，同样经由这个文本标记判定，
      同样在 openai-completions 协议上恒为假/恒为零。`anthropicOnlyCoverage` 现列出全部受影响项（Finding/
      Metric 用类型化 code，两组 corpus-only/journey-only 的自由文本栏目分列）；
      `llm_findings.go` 的证据包构造未列入——那是 LLM 判断的弱化证据，不是规则层面的结构性沉默，
      性质不同。
    - **披露范围收窄至 `-corpus` 导致默认套件（`vmr analyze` 无选择器，读者最常用的路径）100%
      绕过**：`-corpus` 是一个低频变焦参数，默认套件从不调用它。现在单条 journey 报告
      （`journey-<id>.md` 的"疑似问题"章节）在该 journey 全部 Step 都非 anthropic-messages 协议时同样
      披露——真实语料验证：07-15 单日默认 `vmr analyze` 渲染的 18 份 journey 报告全部正确携带
      该注记（黄金测试 `golden.md`/`golden_zh.md` 同步更新，仅新增这一处折叠块，无其它字节差异）。
    - **1% 断崖阈值本身是缺陷，非临时数字**：原设计低于 1% 才披露，但 1.2% Anthropic 的语料会
      整体噤声——剩余 98.8% 依然结构性测不出来，阈值制造了它本该消除的同一种假安全感。已改为
      "非 100% Anthropic 即披露"（无中间阈值，只留一个处理浮点误差的 epsilon），单条 journey
      则是"该 journey 一条 Anthropic Step 都没有即披露"（journey 内部通常不混协议，二元判据
      比算比例更直接）。
    - **明确不做**：不给 OpenAI 工具返回加内容嗅探（`§2.4` 已用 495,672 条实测否决，见下文
      §2）；不用 LLM 判断的弱证据反推规则层结论。
    曾登记为 §1.43。
41. **CLI 入口完全收敛：`vmr report`/`vmr story` 退化为纯转发，别名能力不再反超主入口**
    （P15.1–P15.3，`cmd/vmr/cmd_analyze.go`/`cmd_report.go`/`cmd_story.go`）：`vmr analyze` 补齐
    `-macro-only`（等价 `vmr report`）、`-list-only`（等价不带参数的 `vmr story`）两个此前无法
    表达的默认套件模式；`cmdReport`/`cmdStory` 删除各自的分派 `switch`，改为解析自身 flag 后构造
    `analyzeRun` 转发给 `dispatchAnalyze`——两份手写分派合并为一份，`cmdStory` 的
    `resolveLLMOptions` 无条件调用随之改为按需闭包，与 `cmdAnalyze` 侧的既有修法自动对齐
    （不再需要在两处分别维护）；`cmdReport` 新增 `-llm-key` flag，`§1.34` 的输入不对称随之消失。
    - **`vmr story -render-all`（无其它选择器）从不触碰宏观报表半区，是 P9 以来的既有行为**——
      转发进 `dispatchAnalyze` 共享的默认套件分支后，首版实现用一个只有别名转发器能设置的
      内部字段（`skipMacroReport`）保住这条行为，使其在 `vmr analyze` 自己的公开 flag 下不可达。
      并行的独立审阅（gemini-3.7-flash，2026-08-21）指出这与"`vmr analyze` 是唯一入口、能力
      应完全覆盖别名"的架构前提直接冲突；征询用户后改为公开的 `-story-only` flag（与
      `-macro-only`/`-list-only` 不同，`-story-only` 刻意不与 `-render-all` 互斥而是可以组合——
      `-story-only -render-all` 就是 `vmr story -render-all` 的精确公开等价写法）。
    - 真实语料验证：`vmr report`/`vmr analyze -macro-only` 对同一输入产物逐字节相同（除
      `generated_at` 时间戳）；`vmr story`（五种调用形态：裸/`-journey`/`-compare`/`-corpus`/
      `-render-all`）与对应 `vmr analyze` 写法产物逐字节相同；`-llm-key` 在两条入口下自指流量
      排除结果一致。
42. **工具调用 ID 归一化下沉至 `chatmsg`**：`internal/chatmsg/toolresults.go` 导出 `NormalizeToolCallID`，`internal/story`（`findings_toolresult.go` 与 `render_spine_step.go`）统一复用，消除了去下划线归一化逻辑的真源外移。曾登记为 §1.40。
43. **Quota 状态文件孤儿 `limitKey` 自动修剪**：`internal/quota/store.go` 的 `Registry.Prune` 配合 `Snapshot.ProviderLimits` 与 `rt.Install` 在路由装载与热重载时基于配置白名单自动修剪废弃 Key，设置 dirty 并在下一次 Flush 时持久化到 `vmr-quota.json`，防止脏数据持久化积累并消除离线读者噪声。曾登记为 §1.45。
44. **Server 审计日志路径单一真源收敛**：`internal/audit/audit.go` 导出 `ActiveLogPath`，`internal/server/admin.go` 的 `auditBlock` 统一复用，消除了命名知识与硬编码格式字符串的重复。曾登记为 §1.47。
45. **错误分类词表补齐三类误判（新增 `ErrQuirk` 类 + `authHint` + 词条补充）**：vendor 专属协议约束拒绝（DeepSeek 思考模式 reasoning_content 回传、Google thought_signature）归 `ErrQuirk`（切换 + 零冷却，同 ErrContent/ErrContextLimit；不复用 ErrContextLimit 为保审计标签诚实性）；OAuth 标准错误码（`invalid_grant`/`invalid_token`/`token has expired`）归 `ErrAuth`；bai 的 "Input token exceed the limit"（可与 `quota_limit_reached` 同现）归 `ErrContextLimit`。此前三者均落入兜底 `ErrClient`（永不 failover）而中断重试。全量 quirk 模块方向延后，登记为 §1.48。

46. **2026-08 综合评审 B1–B9 修复**（`docs/VMR_综合评审_2026-08_sonnet-5.md` §5.1，B10–B14 未做，逐项见该文档"9. 执行情况记录"）：
    - **B1 · buffered 模式截断的客户端可见数据丢失**（`internal/respnorm/respnorm.go`、`internal/router/router.go`）：`Read` 在非 EOF 上游错误分支只置 `srcErr`，`s.buf`/`s.pending`（已收字节）从不 flush——所有非 SSE 200 与 MiniMax thinking 流走 buffered 模式，客户端已收到 `200 OK` + headers，于是看到一个格式良好的空 200。修法：新增 `flushRawOnError()`，错误分支先把**可安全交付**的已收字节（带模型名重写）flush 进 `s.out` 再置错；`forwardSuccess` 在 `status == "TRUNCATED"`（上游中途死、非客户端取消）时于全部计费/审计/日志记账之后 `panic(http.ErrAbortHandler)`——net/http 静默 abort 连接，客户端 SDK 看到断掉的传输而非静默空成功。**"可安全交付"的边界**（评审后按外部反馈收紧）：非 SSE 响应 flush `s.buf`（部分 JSON 对象——直连也是这个结果）；SSE 流只在 `modePassthrough` 时 flush 尾部，`modeUndecided`/`modeBuffered` 一律不 flush——SSE 进 `modeBuffered` 只可能是 MiniMax thinking 形态被扣住待剥离，`modeUndecided` 尾部可能是同形态的不完整事件，raw flush 会把未闭合的 `<think>` 泄漏给客户端（正是 buffered 模式要防的反馈循环），审计记 `truncated_withheld`。回归测试 `TestRespStream_BufferedTruncationFlushesReceivedBytes`、`TestRespStream_ThinkBufferedTruncationWithholds`、`TestBufferedTruncationAbortsAndFlushes`；`hang_test.go` 的 `TestNonSSEBodyStallAborts` 更新为接受 abort。
    - **B2 · quota 缺省 `since` 每次加载/热重载清零计数**（`internal/quota/period.go`、`internal/config/quota.go`）：`DefaultSince` 曾直接返回 `now`；`LimitKey` 不含 `since` 故桶 key 稳定，但 `PeriodStart` 每次加载重算到加载时刻，`resetIfStaleLocked` 就地清零。修法：`DefaultSince(now, unit)` 把缺省锚点对齐到固定日历边界——min/h/d→当日午夜、w→周一 0 点、mo→月初。锚点定在午夜（不是"整点"）使周期栅格锁死到"日"：**同一自然日内的任意热重载都解析出同一锚点、对任意 `every` N 都 `PeriodStart` 恒等、计数存活**。残余收窄为：`every: Nh` 且 N∤24（如 `5h`）或 `every: Nmin` 且 N∤1440（如 `7min`），**且**热重载/重启跨过自然日——此时栅格可能相移一次、至多一次重置。显式写 `since` 可钉死。回归测试 `TestDefaultSince`、`TestDefaultSince_SurvivesReload`（同日跨 20h、跨多个整点重载，min/h/d/w/mo × 多种 N 全部不重置）、`TestRegistry_DefaultSinceReloadDoesNotReset`。Quota 设计文档"已评估并否决的改进提案"表里那条"未配 `since` 时的热重载锚点漂移问题"原判"非问题（撤回）"只论证了相位对齐、从未提清零后果，现已改为"已修复（B2）"。
    - **B3 · LLM 推断 Finding 的 `EvidenceAnchor` 无运行期校验**（`internal/story/llm_findings.go`）：6 个 `detectLLM*` 只查 anchor 非空，"anchor 必须是真实 transcript 逐字子串"这个机械校验只存在于 `_eval/calibrate_p1b.go`。模型幻觉出一个不存在的 anchor 就会晋升为 `HIGH` + `[AI推测]` 的 Finding、并被当既定事实喂给下一次单-Journey 解读。修法：把 `_eval` 的 `transcriptPool` 逻辑搬进包内（`searchableTranscript`），`ComputeLLMFindings` 收集完全部 detector 输出后按 `strings.Contains(pool, anchor)` 逐条校验，不过关即丢弃（保持 fail-open）。回归测试 `TestComputeLLMFindings_AnchorVerification`（真锚点存活/假锚点丢弃）、`TestSearchableTranscript_CoversReconstructedAndRaw`。
    - **B4 · `report` Markdown 渲染器不转义用户来源标题**（`internal/report/render_doc.go`、`section_sessions.go`、`requests.go`、`metrics.go`）：`story` 侧 P12.2/P12.3 修过的同类 bug，`report` 侧从未加。修法：`mdTable.row` 集中对每个 cell 走 `reqdetail.EscapeCell`（`|`/换行）；会话标题、任务标题引用块、context-growth Finding 里的标题额外走 `reqdetail.EscapeHTML`（未闭合 `<!--` 吞并）。回归测试 `TestMarkdownEscapesUserDerivedTitles`。至此两个分析命令的全部表格/引用块标题注入点统一处理完毕（承 §3 第 37 项）。
    - **B5 · 热重载 `reload()` 闭包未串行化**（`cmd/vmr/cmd_start.go`）：fsnotify 的 `time.AfterFunc` goroutine 与 SIGHUP goroutine 可并发调 `rt.Install`，`installLimiter` 的非原子 load→check→store 可短暂翻倍有效并发。修法：一个 `sync.Mutex` 包 `reload` 闭包体。
    - **B6 · `MetricCost` charge 把 token 计的 `estimated` 传进 `Charge`**（`internal/router/quota.go`）：`bucket.Estimated` 是 requests/tokens 账户专用累加器，cost 账户的估算信号是金额、经 `AddEstimatedCost` 单独记。修法：cost 分支给 `Charge` 传 `0`。回归测试更新 `TestChargeCost_DegradedEstimate_TracksEstimatedCost`。
    - **B7 · `Install` 先 `Quota.Prune` 再 `snap.Swap`**（`internal/router/snapshot.go`）：两者之间持旧 snapshot 的 in-flight 请求可 `Charge` 进刚 prune 的桶。修法：调换顺序——先 Swap（新请求即用新 key），再 Prune（清掉旧 key），straggler 由下次热重载的 Prune 自愈。
    - **B8 · quota 读路径重置桶但不置 `dirty`**（`internal/quota/quota.go`）：`Used()`/`EstimatedCostFor()` 经 `resetIfStaleLocked` 变更内存桶却从不 set `r.dirty`，只被读路径观测到的周期滚动不会被 flusher 持久化。修法：`resetIfStaleLocked` 返回是否重置，两个读方法据此置 dirty。回归测试 `TestRegistry_UsedResetMarksDirty`。
    - **B9 · 每个 stitch 边界无条件开新 Task**（`internal/story/journey.go`）：`newTask := (ci==0 && i==0) || atStitchBoundary` 与 `taskseg.IsNewTask`（新 trace id 或真实新指令）矛盾——一次任务中途为回收上下文的压缩会被渲染成全新 Task，虚增 `len(j.Tasks)`、`plan_exec_ratio` 分母、per-Task 检测器、`-corpus` 分组。修法：stitch 边界只在 `newInstructionTitleAtStitch` 找到真实新指令时才开新 Task，否则沿用 `curTask`——该 Step 仍带 `StitchEdge`/`Compaction`，脊柱/Markdown 渲染器照常渲染"🧵 Stitched from an earlier fragment"+ 压缩摘要，无需新增 i18n 串。回归测试 `TestStitchedJourney_EndToEnd`（更新为断言单 Task）、`TestStitchedJourney_NewInstructionOpensTask`（新增）。

47. **2026-08 综合评审 §5.2 / §5.4 / §6 落地**（`docs/VMR_综合评审_2026-08_sonnet-5.md` §10）：
    - **§5.2 DX 3×P0**：新增 `config.minimal.yaml`（+ `.zh`），README Quick Start 改用它并新增 `vmr diagnose` 的 Verify 步骤；`config.Parse` 记录展开为空的 `${VAR}`（`Config.EmptyEnvRefs`），缺 `api_key` 或空 `${VAR}` 在 start/reload 打带框 `CONFIG PROBLEMS` banner 而非一行淹没的 WARN；某虚拟模型全部端点无 key 时 `router.Serve` 直接回 `vmr_no_api_key` 503，不再让每个 attempt 401 上游 + 冷却。
    - **§5.4-1 analyze 内存**：`ctxgraph/stitch.go` blob 倒排索引 `map[Hash]map[int]bool` → `map[Hash][]int`（见 §1.2 补记）。
    - **§5.4-2 include_usage 可见性**：`config.Check()` 新增 `checkQuotaUsageVisibility`——token/cost 额度账户挂在 `openai-completions` 端点上时打 `SeverityWarning`（流式响应无 usage 块除非客户端发 `stream_options.include_usage:true`，vmr 不注入）；`vmr status` 与 `vmr report` 的额度段在 `estimated_pct ≥ 95%` 且 metric∈{tokens,cost} 时追加同因提示。此前全代码库零处提及 `include_usage`。
    - **E2 · 软屏蔽 → failover**（`internal/config`、`internal/core`、`internal/router/snapshot.go`、新增 `internal/router/softblock.go`、新增 `internal/adapter/response.go`、`internal/respnorm/minimax.go`）：新增 `soft_block_failover *bool`（`models.<name>` 与 `endpoints[]` 两级，endpoint 覆盖模型级，缺省关）。开启后 `tryOne` 对 eligible 2xx（非 SSE、非压缩）预读到 `softBlockPeekCap=64KB`，若命中 `respnorm.ContainsSoftBlockMarker` 且 `adapter.ResponseAssistantText` 判定有效文本 ≤64 rune 且无 tool_call，则按 `ErrContent` 分支 failover（`ReportNeutral`、attempt 记 `content` 类、零冷却）。文本抽取按协议放 `internal/adapter`（不引 `chatmsg`）。回归测试见 `router_serve_test.go` 的 `TestServe_SoftBlock*` 四例 + `adapter/response_test.go`。
    - **E1 · HTML 单文件 journey 渲染 + 脱敏**（新增 `internal/story/render_html.go`、`render_html_assets.go`、`internal/i18n/story_html.go`；`cmd/vmr/cmd_analyze.go`/`cmd_story.go` 加 `-html`/`-redact`）：`vmr analyze -journey <id> -html` 额外写 `stories/journey-<id>.html`——单文件自包含（内联 CSS + 一段内联 JS，零外部请求，theme-aware），左侧粘性时间轴 + 右侧 Step 卡片瀑布，数据源是 `RenderMarkdown` 走的同一个 `*story.Journey`。`-redact`（需 `-html`）把每段正文替换为 `‹text: N chars›` 长度占位，保留结构/角色/token 数/工具名/时间/compaction 标记。仅 `-journey` 单命中（同 `-llm-addr` 约束）；产物 0600。回归测试 `render_html_test.go`（结构 + 脱敏无泄漏 + ZH chrome）。默认套件不出 HTML index（用户决定）。Analytics 设计文档 §8 对应条目从"尚未实现"改为已实现。
    - E3（per-virtual-model 预算硬闸）本轮 **hold**（用户决定，理由见评审 §10）。

---

## 4. ROI 评估总表（针对 §1 的待定问题）

> **只评 §1**。§2 是已经论证过的刻意取舍——重新打分等于重新论证，正是那一节存在的目的所要避免的；
> §3 已闭环，无投入可言。
>
> **评分口径**
> - **成本** = 工作量 + 它给代码库留下的长期复杂度（不只是这次写多少行）
> - **风险** = 改错时的爆炸半径：是否动契约（`audit.Record` / `vmr-report.json`）、是否碰转发热路径、要同步改几处
> - **价值** = 解决的真实痛点 + 长远架构收益
> - **ROI** = 价值 ÷（成本 + 风险）。三档，不给数字分——这里没有能支撑两位有效数字的依据，
>   假精度比粗判更有害。
>
> **一条贯穿性的判据**：这张表里多数条目的 ROI 是**时间相关的**。「今天低 ROI」几乎都不是「这事不值得做」，
> 而是「触发条件还没到，现在做等于替一个会自己举手的问题排队」。最后一列写的就是它什么时候该被重估。

### 4.1 总表

> **覆盖范围声明**：本表覆盖 §1 全部待评估条目（共 17 条，无遗漏）。`1.22`/`1.49`/`1.50` 已给出量化或明确前提的"不做"结论（各占本表一行）；`1.18` 正常列出；`1.29`（暂不做）已给出明确重估触发条件；原 `1.23` 已并入 `1.1`；原 `1.46`（版本错配）经评审确认"CLI 与 Server 版本必须匹配、直接报错"后移入 §2.2，不再列入本表；`1.40`、`1.45`、`1.47`、`1.21`/`1.28`/`1.31`（P7）、`1.19`（P8）、`1.30`/`1.33`/`1.34`（P9）、`1.39`（P11）、`1.41`/`1.37`（P12）、`1.35`/`1.36`（P13）、`1.38`/`1.42`/`1.43`（P14/P15）已修复并移入 §3；原 `1.24`、`1.5`、`1.15` 与 `1.27` 重新归类为 §2 的工程惯例与架构哲学，均已移出本表。

| # | 问题 | 成本 | 风险 | 价值 | ROI | 判据 / 何时重估 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| 1.13 | 额度燃尽看板 | 高 | 低 | 中 | **中** | 产品价值而非技术债，按产品路线排期，不与本表其他条目抢顺序 |
| 1.18 | Phase 1b 六个 LLM 判别器未完成黄金样本校准 | 高 | 低 | 中 | **中** | 校准是人工标注投入（30–50 个 Journey 逐条判断 TP/FP），无法自动化；六个判别器在真实语料上已达 100% Evidence Anchor 有效率、人工抽查判断合理，当前没有观察到需要立即处理的误报模式，不构成阻塞性风险。`_eval/calibrate_p1b.go` 已是可直接复用的校准工具，扩大 `-input`/`-limit` 即可推进——**成本在人力投入的时间，不在代码**，这是它与本表其余"低 ROI、缺 profile/前置条件"条目的本质区别 |
| 1.22 | `chatmsg` 未覆盖 Responses API | 中 | 低 | **零（已量化）** | **不做** | 真实语料 11253 条里 `openai-responses` **0 条**。触发条件量化为"`vmr-requests.json` 出现该 protocol"，在那之前不投入 |
| 1.1 | `vmr report` 会话分析那一趟（`collect()`）仍未缓存（含原 1.23） | 中 | 中高 | 已证 | **中** | 聚合那一趟缓存后的实测已经证明缓存本身能把热耗时压到个位数秒量级（5.2×）；剩下这一趟触及会话/任务边界判定的正确性，风险高于聚合，需要单独配一套 cold/warm 一致性测试再动手，不是顺手就能扩展的收尾 |
| 1.2 | 全内存聚合的记录量上限 | 中 | 中 | 未证 | **低→中** | 按日分桶释放依赖严格的时间单调递增保证——一个隐蔽的正确性前提。**"目标单机场景下内存完全可控"这条判据已被实测推翻**：11374 条记录峰值 RSS 1.38GB，而原文估的是"千万级约数百 MB"。重估触发条件下调为"单次分析超约 3 万条记录，或峰值 RSS 超 4GB" |
| 1.3 | `chatmsg` 离线 `map[string]any` 分配 | 中 | 低 | 未知 | **低** | **前置条件未满足**：离线耗时由 I/O 与 zstd 主导，收益完全未测。先跑 benchmark 拿 profile |
| 1.7 | 给 `Row`/`ClientRow` 补 `CostEstimateEst`（方案 ②） | 中 | 中 | 低 | **低** | 要动 `rows.go`/`accumulateCost`/渲染层三处并再次改 `vmr-report.json` 形状，而方案 ① 已经解决了误导本身。**除非外部脚本真的需要这个字段** |
| 1.6 | §2.5 标记符号已达四个 | 低 | 无 | 主观 | **低** | 成本只是删几行，但**没有依据**：四个标记都按需渲染，健康报表一个都不出现。**真实报表读起来觉得吵了再动** |
| 1.8 | `archtest` 豁免键无法区分重名方法 | 低 | 无 | 低 | **低** | 今天零影响（重名方法全部远低于默认限额）。现在改是**为不存在的场景加机制**。第一次需要豁免重名方法时再改 |
| 1.9 | 探针请求绕过审计日志 | 中 | 中 | 低 | **低** | 探活消耗极低，而混入报表会污染业务 SLO 统计——**成本主要在决定呈现口径，不在写代码** |
| 1.10 | 审计 `write` syscall 在全局锁内 | 高 | 中 | 未证 | **低** | 异步队列要处理背压策略（丢弃 vs 阻塞）与优雅关停等待，**换来的是一个尚未被证明存在的瓶颈**。高并发压测顶到写锁再说 |
| 1.14 | 滑动时间窗限流模型 | 中高 | 低 | 低 | **低** | 当前日历对齐的近似对目标场景（月度/日度 token plan）够用。属产品路线 |
| 1.17 | `imgprep` 解码闸门的阈值量纲 | 中 | 中低 | 未证 | **低** | 闸门已存在（`maxDecodePixels`，防炸弹），缺的只是一道按内存预算设的更低阈值。**前置条件未满足**：无任何实测显示该峰值造成过问题，而方案自带「用账单换内存」的取舍，不能替用户默认决定。零风险的一半（文档记账）已落地。**真实视觉负载观测到内存突刺时重估** |
| 1.48 | 错误分类词表的长期形态：端点级 quirk 统一模块（含 sticky 降级） | 中 | 中 | 低→中 | **低** | 已知三类误判已被最小修复覆盖（§3 第 45 条），现在上机制是替一个不会自己举手的问题排队。触发条件：全局词表增长到出现互相干扰/误命中，或 sticky 重复往返在真实负载中可观测地拖慢中毒会话 |
| 1.49 | imgprep 像素乘积在 32-bit `int` 溢出 | 极低（一行 `int64`） | 0 | 0（当前） | **不做** | 64-bit（唯一目标平台）不受影响，Go `image/png` 已把宽高钳在 `int32`。触发条件：32-bit 成为构建/部署目标。2026-08 评审 B13 已标"确定不做" |
| 1.50 | 详单文件名 hash8 碰撞 | 低（提到 hash12/16 或改序号） | 低 | 极低 | **不做** | 需同文件近万条记录 + 同毫秒 + 同模型 + 同 outcome 才撞成同名，现实可忽略；随 §1.2 的语料上限线性恶化。评审 B14 自判"登记即可" |

### 4.2 分档结论

- **高 ROI（0 条）**：无。
- **中 ROI（3 条，看触发；`1.40`/`1.45`/`1.47` 已修复移入 §3，`1.28` 已由 P7 修复移入 §3，`1.39` 已由 P11 清理移入 §3，`1.37` 已由 P12 修复移入 §3，`1.38`/`1.42`/`1.43` 已由 P14/P15 修复移入 §3）**：
  `1.13` 按产品路线排；`1.1`（含 `1.23`）
  是 P3.6 之后**收益已经用真实语料证明**（5.2× 缓存收益）、只是正确性风险需要额外测试基础设施
  兜底的条目——与"收益未经测量"的低 ROI 组本质不同；`1.18` 是校准投入而非代码工作、且当前无阻塞
  性误报信号，等黄金样本标注窗口再推进。原 `1.16`（并发竞态）、
  `1.4`（流式断开审计语义）与 `1.12`（图片缓存容量上限）均已完成修复闭环。
- **明确不做（3 条）**：`1.22`——它自己提出的前置问题（Responses API 占比）已经用真实语料回答了，
  答案是 0%；`1.49`（32-bit 像素乘积溢出）——无目标平台受影响；`1.50`（文件名 hash8 碰撞）——现实概率可忽略。
  **一条给出了量化触发条件或明确前提的"不做"，比一条无限期的"待定"更有价值**：前者有明确的
  重开条件，后者只会被反复重新讨论。
- **低 ROI（8 条，等触发条件）**：其中 `1.2`/`1.3`/`1.7`/`1.10`/`1.17` 的共同点是**收益未经测量**——它们不是"不值得做"，
  是"还不知道值不值得"，而先做优化再测量正是这个项目一贯拒绝的顺序。`1.6`/`1.8` 则是真实反馈或代码演进时自然触发的事。

**关于这张表本身的一个观察**：这段原文写的是"13 条里高 ROI 为 0 条；没有一条是'高价值但一直没做'
……如果哪天这张表里出现了「价值高 + 成本低 + 却还在等」的条目，那才是需要解释的异常"。这个异常已经
出现过两轮：第一轮（`1.31`/`1.32`）由 P7/P9 排掉，第二轮（`1.35`/`1.36`）是 2026-08-21 全面复查
新登记的，其中 `1.35` 还是第一轮那条被误判为已闭环的同一件事。
**这张表的健康状态不是"从未出现过异常"，是"异常出现后被正确识别、排上日程、修掉"——
而 `1.32` 的误判说明"识别"这一步本身也会出错，识别的依据必须是实测数字，不是阶段执行记录的自述。**

**一个补充判据**：`1.16`（原条目，现已闭环）是这张表里第一条**从源码注释里捞出来、而不是从代码里读出来**的条目——
它早就写在 `respnorm` 的 `qmu` 注释里，注释还写着「见本文件的既有条目」，而那个条目当时并不存在。
清单的价值取决于覆盖率：**一条只存在于源码注释里的已知问题，等于没有被跟踪**。
§2 同批补登的四条刻意取舍出于同一个理由——它们都曾被独立审查者当作新问题重新提出过一遍。
