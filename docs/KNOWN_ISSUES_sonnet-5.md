<!-- Ver 2026-08-16 01:30, by gemini-3.7-flash -->

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

- **稳定性与安全性**：无数据丢失、凭证泄漏、并发竞态或服务阻断级别的缺陷；单机生产环境可稳定运行。`copyFlush` 异常路径下的 `respnorm` 检查方法已全部实现互斥锁同步保护，`-race` 全绿且经端到端流式客户端断开集成测试守护。
- **自动化基线**：`go test ./...` 与 `go test -race ./...` 全绿；`internal/archtest` 强制导入单向边界、文件行数预算、函数长度预算与文档引用完整性。
- **§1 分布**（2026-08-20 P8 收尾后重算）：中危 5 项、低危 16 项，无高危项（另有 1 项已评估决定不做，登记备查，不计入分布）。`1.32`（`vmr analyze` 强制 `-render-all`，中危）仍待 P9 处理；`1.21`/`1.28`/`1.31`（P7）、`1.19`（P8，JSON 语言策略统一）已修复并移入 §3；`1.33`/`1.34`（集中登记的文档与小口径缺口）各自四项/三项已由 P7 处理，剩余子项留给 P9；原 `1.24`（浮点 1 ULP）经重新分类后移入 §2.5——它是浮点算术的性质，不是待办。
- **文件与函数行数守卫语义一致**：两者都是「全局默认 + 豁免表」，新写的文件/函数默认受约束，不依赖有没有人记得登记。

---

## 1. 待定与待解决问题

### 1.1 [低，已部分闭环] `vmr report` 多文件输入的三趟扫描开销——聚合那一趟已缓存，会话分析那一趟仍未缓存

- **现状**（P3 批 D 更新，2026-08-20）：`report.Build`/`BuildCached` 对同一批输入文件实际跑三趟扫描，不是两趟：
  ① `AnalyzeSessionsCached` 内部并发的 `ctxgraph.ScanCached`（manifest 解析，文件哈希+schema
  版本命中即跳过）；② `AnalyzeSessionsCached` 内部**另一条独立并发通道**——`analyzeFile`/`collect()`
  （§3.4 提到的、专供会话/任务分组用的每记录特征提取：工具签名、角色字符/token、compaction 标记、
  chat_id、NoReply 等），**从未缓存，每次全量重跑**；③ `aggState.scanFiles`（指标聚合，产出
  `RequestRow`/`Report2` 各分桶）。P3.6 已经给 ③ 接上了缓存（`internal/report/factscache.go` 的
  `recordFacts`/`fileFacts`，随 manifest 缓存一起落进 `.parse-cache/`）——真实 34 文件/177MB 压缩
  语料实测：全量热缓存耗时从 71.8s 降到 16.2s（收益从 1.17× 提到 5.2×），但离"个位数秒"的目标仍有
  差距，差距的主要来源就是仍未缓存的②。
- **可能方案**：给 `collect()` 的输出设计一个对等的可序列化投影（类比 `recordFacts` 之于
  `buildRec2`），随同一份 `fileFacts` 一起缓存，`analyzeFile` 命中时直接重放而不重新打开文件。
- **为什么不在 P3.6 顺手做**：`collect()` 的产出（`ReqInfo`）比聚合用的 `rec2`复杂得多——除标量
  字段外还有 `taskseg.IndexRealUsers` 的索引结果与用于跨文件缝合判断的 `tailPrev` 消息预览文本，
  这些字段直接喂给 `group()`/`StitchGraph` 做**会话/任务边界判定**，正确性敏感度高于纯指标聚合
  （聚合算错是数字偏差，边界判定算错是把不相关的对话缝到一起）。仓促给这一层加缓存、又没有像
  `TestBuildCached_WarmMatchesBuild`/`TestScanFiles_CacheHitNeverOpensFile` 那样的严格一致性测试
  兜底，风险高于收益。
- **触发条件**：有精力时把这条也纳入同一套缓存基础设施，补齐对等的 cold/warm 一致性测试后再关闭
  这条目；在此之前 ③ 的收益已经是真实、已验证的独立进展，不依赖①②是否补齐。

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
  3 万条记录，或峰值 RSS 超过 4GB"**——按实测斜率，这大约是三个月的历史日志。

### 1.3 [低] `chatmsg` 离线解析路径的 `map[string]any` 分配

- **现状**：`internal/chatmsg` 有 43 处 `map[string]any`，全部在离线消息/SSE/usage 解析路径上。转发热路径不受影响——实测 `adapter`/`audit` 为 0，`router` 唯一一处在 `WriteError` 的错误响应体，`server` 的 8 处全在 `/admin/status` 与 `/v1/models`，`probe` 1 处在后台主动探活请求体构造，没有一处在客户端转发链路上。
- **为什么待定**：**前置条件未满足**。离线路径的耗时由磁盘 I/O 与 zstd 解压主导，改用具体结构体的收益完全未经证明。先在真实审计日志上跑 benchmark 拿到 profile，再谈是否值得。没有 profile 数据之前不动。

### 1.5 [低] 几个文件贴着行数预算线

- **现状（2026-08-20 复核）**：`internal/story/compare.go`(846/850)、`cmd/vmr/cmd_story.go`(**780**/850) 贴线。
  P6 期间 `cmd_story.go` 又长了 24 行（自指流量过滤、类别分组），余量从 94 行降到 70 行；
  `compare.go` 未变。趋势方向是收紧的，但仍未触线。
  处置不变：都已被 `archtest` 的 `file_sizes_test.go` 纳管，超预算会自己举手——预算报警了再拆，
  拆完按实测 +15~20% 重新登记，提前拆是替一个会自己举手的问题排队。
- **`internal/report/detail.go` 那一条已闭环**：P2 阶段（详单渲染下沉为 `internal/reqdetail`）把它从
  1047/1150（91%，四项正交职责混在一个文件）拆成了 286 行的薄封装层（worker pool 调度 +
  `ReqInfo`→`reqdetail` 的参数翻译），渲染/字段提取/diffing 三项职责随之下沉到
  `internal/reqdetail` 的独立文件——与本条早先"自然的拆法是三分"的预判一致。见
  `docs/future-strategy/story_report_p2_action_plan_sonnet-5.md`。

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

### 1.15 [中] 分析半区体量增长与单人可维护性

- **现状**：分析半区（`report`/`story`/`ctxgraph`/`taskseg`/`chatmsg`/`reqdetail`/`i18n`）的代码体量已超过在线路由半区。
  **实测生产代码行数（2026-08-20，不含 `_test.go`）**：分析半区 `story` 7983 + `report` 7065 +
  `i18n` 3327 + `ctxgraph` 1749 + `reqdetail` 1589 + `chatmsg` 1151 + `taskseg` 395 ≈ **23,300**；
  路由半区四个主包 `router` 2050 + `config` 1678 + `quota` 809 + `server` 735 ≈ **5,300**。
  比例约 **4.4 : 1**。P1–P6 六个阶段又净增了一个新叶子包（`reqdetail`）与约两千行。
- **指导原则**：新的探索性 Agent 分析指标，优先用外部脚本消费稳定的 `vmr-report.json` / `journey-*.json` 数据契约做验证，证明价值后再评估是否合入主仓库。
- **一条补充判据（2026-08-20 加，P7 收尾后更新）**：这条不是"分析半区太大了要减肥"——`CLAUDE.md`
  早已把两半区定为 co-equal，体量差本身不是缺陷。它真正约束的是**下一次要不要给分析半区加东西**：
  在 §1.32 这类"上一轮工作的最后一公里"清干净之前（§1.31 已由 P7 清干净），新增分析能力会让维护面
  继续单向扩大。

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
  **`openai` 11194（99.5%）、`anthropic` 59（0.5%）、Responses API（`openairesponses`）0 条（0.0%）**。
  也就是说这个缺口在本项目的真实使用中**一次都没有被触发过**。按 YAGNI，**决定不做**，
  但保留登记，因为影响面已经比 P1 登记时更大（见下）。
- **影响面的变化（P4 之后）**：P1 登记时的影响只是"脊柱不展示工具结果 + 三个 Finding 检测器无证据"，
  都是展示层。P4 之后 `journey-<id>.json` 的 `structure.tasks[].steps[].tool_calls` 也走同一条
  提取路径——一旦真的出现 Responses API 流量，**机读契约会静默地报告"这一步没有工具调用"**，
  而不是报告"这种形状我不认识"。展示层降级读者能看出来，机读契约降级读者看不出来。
- **触发条件（量化，不再是"先确认占比"这种没有边界的话）**：任意一次 `vmr report` 的
  `vmr-requests.json` 里出现 `protocol == "openairesponses"` 的记录，即重新排期；在那之前不投入。
- **登记来源**：2026-08-19 P1 执行期间的独立代码审阅发现；2026-08-20 六阶段复盘用真实语料量化占比。

### 1.23 [低] `session.go` 的 `collect()`/`analyzeFile` 是报表读取里唯一仍未接入缓存的一遍

- **现状**：见 §1.1 已更新的现状说明——`analyzeFile` 是 `AnalyzeSessionsCached` 内部与
  `ctxgraph.ScanCached` 并发跑的另一条通道，专供会话/任务分组用的每记录特征提取，从未查过任何
  缓存，每次全量重新打开文件、逐行解码、跑 `collect()`。这是本条与 §1.1 唯一的区别：§1.1 是
  "现象"（三趟里还有一趟没缓存），本条是这一趟本身的独立登记条目，供后续单独立项时不用重新翻
  §1.1 的历史脉络。
- **为什么单独立项而不是揉进 §1.1 一起做**：`collect()` 的输出（`ReqInfo`）里喂给
  `group()`/`ctxgraph.StitchGraph` 做会话/任务边界判定的那部分字段（`taskseg.IndexRealUsers`
  的索引结果、用于跨文件缝合判断的 `tailPrev` 消息预览文本）不是纯标量，需要专门设计一套可序列化
  投影，且正确性风险高于 P3.6 已经完成的聚合缓存（`internal/report/factscache.go` 的
  `recordFacts`）——聚合算错是报表数字偏差，这里算错是把不相关的对话缝到同一个 Session/Journey
  里，属于更严重的一类错误。
- **触发条件**：决定投入时，先补一套对等的 cold/warm 一致性测试（参照
  `TestBuildCached_WarmMatchesBuild`/`TestScanFiles_CacheHitNeverOpensFile` 的模式）再动手，不要
  只凭"结果看起来对"就上线。
- **登记来源**：2026-08-20 P3 批 D（`story_report_p3_action_plan_sonnet-5.md`）执行期间，为验证
  P3.6 缓存收益在真实语料上的实际效果（34 文件/177MB 压缩语料）时发现——原计划预期热缓存降到
  个位数秒，实测 16.2s，差距诊断到这一遍身上。

### 1.27 [已决定不做，登记备查] `.parse-cache/` 分片不做孤儿回收（旧 hash 分片、schema 升级后的旧版本分片）

- **现状**：`ctxgraph.SaveCacheDir` 只增量写入当前 `FileCache.Files` 里存在的分片，从不删除磁盘上
  多出来的（文件改名/轮转后旧 hash 对应的分片、`CacheSchemaVersion` 升级后不再被任何 hash 命中的
  旧版本分片）。
- **决定**：不做主动回收。理由与架构文档 §7.6(c) 对 `evidence/` 目录的同一条判断一致——`.parse-
  cache/` 是完全可再生的派生产物，引入引用计数或 GC 扫描是过度设计；需要清理时整目录删除重建
  即可，`vmr report`/`vmr story` 都能从空缓存目录正常冷启动。
- **⚠️ 体量数字订正（2026-08-20 实测）**：本条原文写的"单份几 KB 到几十 KB 量级"**错了两个数量级**。
  实测全量语料 34 个审计文件 → 34 个分片、合计 **51MB、平均 1.5MB/分片**（P3.6 把每条记录的
  事实提取结果也装进了缓存，载荷本来就该比只装 manifest 时大得多——是本条写就时没跟上 P3.6 的改动）。
  这不改变结论，但**改变了它的边界**：一次 `CacheSchemaVersion` 升级会一次性把整份缓存
  （当前 51MB，随语料线性增长）变成孤儿，而 `SaveCacheDir` 永不删除，多升几次就是几百 MB。
- **重估触发条件（新增，原文没有给）**：`.parse-cache/` 目录体积超过同批审计日志压缩后总体积
  （当前 51MB vs 177MB，尚有距离），或一次 schema 升级后用户明确反馈磁盘占用异常。在那之前，
  "需要时整目录删掉"仍然是比任何 GC 机制都便宜的答案。
- **登记来源**：2026-08-20，同上并发评审 §2.3 提议补一个 `CleanCacheDir` 辅助函数；核实后判断为
  主动不做，登记在这里防止被重新提出；同日六阶段复盘订正体量数字并补触发条件。

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
  **YAGNI + 已裁决的无兼容包袱**：架构文档 §1.2 明确裁决"JSON 无外部脚本消费"，
  `journey-<id>.json` 至今唯一已知的程序化消费方是 `_eval/calibrate_p1b.go`，它只读
  `EvidenceAnchor`。**没有消费者，就没有人需要探测版本。** 结论不变（暂不做），
  但理由换成站得住的那个。
- **触发条件（量化）**：出现第一个 `_eval/` 之外的程序化消费方，或架构文档 §7.12 的只读服务方向
  真的启动（那时机读层就是 API 返回体，版本协商是硬需求）。
- **可能方案**：`structure` 内部加一个 `schema_version int` 字段（或复用 P4.3 尚待定的 JSON 语言
  策略字段一并设计），成本接近零，但**改在这一次新增字段时最便宜**——现在不加，以后每加一版都要
  多考虑一层"没有版本戳时怎么判断"。
- **登记来源**：2026-08-20，P4 ActionPlan 独立评审（`story_report_p4_action_plan_review_pi.md`
  §3 M3）提出，核实为真实但低优先级，留给 P5/P6 触及 `journey-<id>.json` 形状时一并考虑。

### 1.30 [中] `vmr story -render-all`（含经由 `vmr analyze` 触发）在大语料上被系统杀死，不只是慢

- **现状**：本机全量语料（34 个审计文件、11374 条记录）冷启动跑 `vmr analyze`（内部先执行
  `vmr story -render-all`）在约 10 分钟墙钟、~300s 累计 CPU 时间后被**信号杀死**（进程退出码
  137 = SIGKILL），story 阶段连候选索引都没写出（`stories/` 目录为空）——不是跑得慢，是没跑完；
  单日样本（322 条记录、6 个 Journey）同一命令 ~18s 正常完成。**已定位到具体是哪一步**：同一份
  34 文件语料上，`vmr story`（不带 `-render-all`，只列出候选）18.22s 正常完成，峰值内存
  （`/usr/bin/time -l` 的 `peak memory footprint`）约 1.07GB——扫描、建图、候选分类这些步骤都不是
  瓶颈。问题精确定位在 `-render-all` 对全部候选（该语料约 350+ 个非断头候选）逐个渲染 Journey
  详单+证据这一步。**这不是 P6 引入的回归**：`cmd_analyze.go` 的 `vmr analyze` 内部就是原样调用
  `cmdStory(storyArgs)`（`storyArgs` 带 `-render-all`），跟用户直接跑 `vmr story -render-all` 是
  同一条代码路径——这个行为在 P6 之前就存在，P6 只是让它成为 `vmr analyze` 的默认路径，从而更容易
  被撞到。本机 16GB 物理内存，是否为 OOM 触发的 SIGKILL 未做进一步确认（复现一次完整的
  `-render-all` 杀死过程再采样内存曲线，成本较高，留给专门排期时做）。
- **为什么定为中危而不是低危**：`vmr analyze` 是 P6 新增的默认推荐入口，"被杀死而不是报错退出"
  对用户是一个体验很差的失败模式（无提示、无部分产物、不知道该怎么应对），且触发条件不算罕见——
  一个月的历史日志量级就可能达到本次实测的规模。
- **为什么不在 P6 里当场修**：诊断需要真正定位内存/资源峰值发生在哪个阶段（`ctxgraph.Scan` 建图、
  `story.BuildAll` 逐 Journey 构建、`EnsureJourneyDetails` 材料化详单，还是三者叠加），这本身是
  一次独立的性能剖析工作，不是本次 P6 范围内能顺手完成的。
- **重新评估（2026-08-20 六阶段复盘）——这条被归错类了：它首先是一个默认值设计问题，其次才是性能问题**：
  1. **根因不在渲染实现，在渲染范围**。`vmr analyze` 无条件给 story 侧加 `-render-all`
     （`cmd/vmr/cmd_analyze.go`，代码注释里论证过"套件的链接要能落到真文件上"），于是
     **每一个候选 journey 的每一步都会触发 `EnsureJourneyDetails`**。单日样本实测：
     `vmr report` 默认产出 1.5MB，`vmr analyze` 默认产出 **164MB**（其中 `details/` 160MB、
     306 份、平均 532KB），而这一批的源日志压缩后只有 5.0MB。按同比例外推全量语料约 5.7GB。
  2. **这直接抵消了 P3.3 的核心交付**。P3.3 的验收标准原文是"默认运行的产物集合回到索引量级"；
     `vmr analyze` 把"默认全量物化详单"从 `-details=true` 这条路搬到了 `-render-all` 这条路，
     换了个开关，行为回到原点。两个阶段的决定在这里直接冲突，而没有任何一处文档登记过这个冲突
     ——这正是本条最该被记住的部分。（`vmr analyze -details` 因此也是误导性的：不传它，
     95% 的详单照样会被写出来。）
  3. **最便宜的修法不需要任何性能剖析**：P6.3 已经算出了每个候选的 `category`
     （`task`/`cron`/`heartbeat`/`subagent`），`vmr-stories.md` 也已经按它折叠噪声类，
     但**渲染范围没有复用这个分类**。默认只渲染 `category == task`（单日样本 6 条候选里
     多数是 task，全量语料 477 条候选里 238 条是 task、239 条是 cron/heartbeat/subagent），
     体积与内存峰值立刻减半以上，且删掉的正是最没人读的那一半。这是一次默认值决策 + 一处
     条件判断，不是一次重构。
  4. 剖析（pprof / 流式渲染 / 分批）仍然值得做，但它应该排在第 3 点**之后**：先把不该做的工作
     不做，再优化剩下该做的工作。反过来做等于花力气把一堆没人要的产物生成得更快。
- **可能方向**（未验证，留给专门排期时评估）：已定位到 `-render-all` 逐候选渲染这一步（见上，
  不是扫描/建图阶段），后续排查可以直接从 `renderAllJourneys`（`cmd_story.go`）与
  `EnsureJourneyDetails` 入手，用 `pprof`/内存采样确认是持有全部候选的内存膨胀，还是详单材料化
  的磁盘/内存双重开销；可能的方向包括给候选数量设一个确认阈值或分批处理、把逐 Journey 渲染改成
  真正的流式（渲染完一个就释放，不在内存里累积已渲染集合）。
- **登记来源**：2026-08-20，P6 ActionPlan 全量语料验证（`story_report_p6_action_plan_sonnet-5.md`
  §10.4）时发现；同日六阶段复盘重新定性为默认值设计问题。

### 1.32 [中] `vmr analyze` 强制 `-render-all`，把 P3.3 的"默认按需生成详单"退回成"默认全量"

- **现状与数据**：见 §1.30 的重新评估第 1–2 点（单日样本 `vmr report` 1.5MB vs `vmr analyze` 164MB）。
  本条与 §1.30 是同一个根因的两种表现：§1.30 记的是"大语料下会被 SIGKILL"这个失败模式，
  本条记的是"即使不被杀死，默认行为也已经违背了 P3 的核心交付"这个设计冲突。两条应该一起裁决，
  单独修任何一条都只能解决一半。
- **需要拍板的是范围，不是实现**：`vmr analyze` 的默认渲染集合该是"全部候选"还是"`category == task`
  的候选"？前者保证任何一条导航边都落在真文件上，代价是 100× 的产物体积；后者把体积和内存拉回
  可用区间，代价是噪声类 journey 的链接要按需生成（而按需生成本来就是 P3.3 建立的能力）。
  **本条不替这个决定给答案，只要求它被显式做一次**——今天它是被 `cmd_analyze.go` 里一行
  `append(common(), "-render-all")` 默认掉的。
- **登记来源**：2026-08-20 六阶段复盘（同上一条）。

### 1.33 [低，部分闭环] P1–P6 落地后遗留的一批文档/文案与实现不一致

每条都已对源码核实，均不影响运行正确性，但都会误导读者；集中登记以免被当成六个独立的新发现反复提出。
**P7（2026-08-20）已处理其中四项**（见下方各自的✅标注），剩余两项——执行顺序文档、README
缺失——都是 P9（CLI 收敛）范围，随那次改动一并处理。

- **`vmr analyze` 的执行顺序在四处文档里写反了**：`docs/UserGuide.md`/`.zh`、
  `docs/VirtualModelRouter_Design_v4_Analytics.md`、`CHANGELOG.md` 都写"先 `vmr report` 后
  `vmr story`"，实际是 `cmdStory` 先、`cmdReport` 后。**而这个顺序是承重的**——`cmd_analyze.go`
  的注释写明了 story 先跑才能让"报表 → 任务索引"这条边在首次调用就命中。把一个承重决策的方向写反，
  下一个读者会照着文档改回去。**留待 P9**（`story_report_dev_plan_2_sonnet-5.md` P9.4）。
- **`README.md`/`README.zh.md` 完全没有出现 `vmr analyze`**（`grep -c` = 0）。架构文档 §10.3 把
  README 的命令示例列为必须同步项，且 `analyze` 是新的推荐入口、README 是开源项目的门面。
  **留待 P9**（P9.4）。
- ✅ **失败请求索引的说明文案仍承诺 `.json` 副本**——P7.5 已改写 `internal/i18n/report_requests.go`
  EN/ZH 两处及 `internal/report/requests.go` 的 `WriteFailedIndex` doc comment，不再提 `.json`。
- ✅ **§8 的按需读取提示引用了一个不存在的列**——P7.1 顺带解决：默认模式下"文件"列本身就渲染
  `req` 坐标了，`internal/i18n/report_doc.go` 的提示语同步改写；**独立复核发现首版措辞"上表
  『文件』列"仍不准确**（该提示语出现在 `vmr-report.md` §8，那一节本身只是一行指向
  `vmr-requests.md` 的链接，没有表格，"上表"无所指），已改为"`vmr-requests.md` 的『文件』列
  （未生成详单时显示为坐标）"，并顺带把示例命令 `vmr replay -print -req <坐标> <审计文件>`
  精简为 `vmr replay -print -req <坐标>`（P6.5 已交付 `-req` 免位置参数，示例不必再带审计文件）。
- ✅ **`spineFullCap` 的注释用一个已删除的函数论证自己**——P7.5 已重写
  `internal/story/render_spine_args.go` 的该处 doc comment，改为描述当前机制（该 Step 自己的
  详单链接）。
- ✅ **工具结果的截断提示指错了地方**——P7.5 已拆分 `capFull`/`capFullWith`：工具调用参数仍用
  "本 Step 的详情链接"，工具结果改用新增的 `SpineResultValueTruncated`（"下一步的详情链接"）。
- **登记来源**：2026-08-20 六阶段复盘（同上一条）；四项已处理状态见 2026-08-20 P7。

### 1.34 [低，部分闭环] P6 落地后的四处小口径缺口

同样集中登记，单条都不值得单独立项，但都属于"契约上说好的事没做到"。
**P7（2026-08-20）已处理其中三项**（见下方✅标注），剩余一项——自指流量识别规则的输入不对称——
在 P9 单一 flag 集合收敛后自然消失，届时一并确认闭环（`story_report_dev_plan_2_sonnet-5.md` P9.5）。

- ✅ **`evidence/` 的 report 侧引用者从未实现**——P7.5 已在架构文档
  `story_report_architecture_opus-5.md` §7.6(b) 的两格加"设计预留，未实现"批注（不删除、不新增
  代码——判断仍是"大概率永远不需要做"，见批注本身）。
- ✅ **`JourneyIndexRow.Category` 用零值 + `omitempty` 表达最重要的那一类**——P7.4 已把
  `CategoryTask` 改为显式 `"task"`，`Category` 字段去掉 `omitempty`，四类候选现在都显式序列化。
- ✅ **`vmr-stories.json` 的 `journeys[].files` 用原始路径而非坐标口径**——P7.4 已把
  `BuildJourneyIndexRow` 的 `fileSet` 收集改为 `ctxgraph.CanonicalPath(m.Path)`，与 `req` 坐标
  拼法一致（真实语料核实：`vmr-audit-2026-07-28.jsonl` 与 `req` 字段的 basename 逐字节一致）。
- **自指流量的识别规则只定义一处，但输入有两处**：`selfTrafficExcludeTags` 确实只有一份实现
  （P6.4 的核心纪律成立），但喂给它的 `llmKey` 来源不同——`cmd_story.go` 走
  `resolveString(*llmKeyFlag, rc.LLMKey, "")`（flag 可覆盖），`cmd_report.go` 只用 `rc.LLMKey`
  （report 没有 `-llm-key` flag）。于是 `vmr story -llm-key X` 与 `vmr report` 会排除不同的集合。
  `vmr analyze` 不受影响（它不透传 `-llm-key`）。触发条件明确且窄，但这正是"规则一处、
  **输入两处**"这类分叉的一个变体，至少应在 doc comment 里写明前提。**留待 P9**（P9.5）。
- **登记来源**：2026-08-20 六阶段复盘（同上一条）；三项已处理状态见 2026-08-20 P7。

> 以下条目基于项目核心哲学（KISS / YAGNI / 单二进制 / 零代码侵入）做出，已经论证过，不需要重新论证。**推翻其中任何一条是允许的，但必须先知道自己在推翻它，并给出新的理由。**

### 2.1 运行时与并发

- **`health.Registry` 全局互斥锁不分片**：单机场景下锁持有时间只是纳秒级 map 读写，分片增加复杂度且无吞吐收益。
- **`HealthKey` 取 SHA-256 前 4 字节**：单实例端点规模下碰撞概率可忽略。
- **健康状态机的退避冷却参数硬编码**：坚持「零调参」，不向用户暴露难以科学校准的配置项。
- **`copyFlush` 的 goroutine + channel 流水线**：避免在底层连接层设置全局 Deadline 破坏 TLS/Header 超时语义。
- **客户端取消时不停止计费**：上游已经生成的 token 厂商照收，路由侧照收才与账单对齐；改成不计费会让 `vmr report` 系统性低估消耗，正是花了整整一批修掉的那类发散。这条只讲计费要不要停，与下面这条是两件事：取消的**传播**（让上游连接真正中止）从来不需要 `copyFlush` 感知——它已经通过 `BuildRequest(r.Context(), …)` 挂到上游请求上，客户端一断，`http.Transport` 就中止上游连接，`copyFlush` 随之退出。取消的**检测/归类**（让审计记录准确标注 `canceled` 而不是误记成 `ok`，见 §3 第 13 项）则是另一回事，`copyFlush` 为此确实需要 select 一次 `ctx.Done()`——不这样做就只能靠后续写入失败间接推断取消，命中不了「响应已完整送达、客户端读完才断开」这类没有写错误可依赖的场景。
- **`respnorm` 的 `Read` 在等待更多字节时返回 `(0, nil)`**：唯一消费方 `copyFlush` 显式处理这一形态；改成内部阻塞循环会让 idle 看门狗失去以读取为粒度的心跳。
- **`respnorm` 的 usage sniffing 不外移为 `router` 侧装饰器**：装饰器要在转发热路径上每 chunk 多付一次接口调用与边界检查；当前实现搭 `ingest` 已有的 per-chunk 循环，零额外开销。理由写在 `internal/respnorm` 的包注释末尾。注意这条只讲**位置**——sniffing 累加器与审计字段之间的同步问题是另一回事，见 §3 第 12 项（已闭环）。
- **`GET /health` 为存活探针（Liveness）而非就绪探针（Readiness），永不因上游不可用返回非 200**：200 仅代表进程存活。若与上游端点健康度绑定，当所有供应商不可用时容器编排系统（如 K8s / Docker）会触发无休止重启，无法修复上游故障反而放大雪崩效应。需要就绪度/端点健康度的调用方应消费 `/admin/status` 的模型健康块。

### 2.2 配置与协议

- **环境变量未定义时静默展开为空串，且不支持 `${VAR:-default}`**：保持配置解析简单明确，默认值应在 YAML 里显式写出。
- **`internal/config` 的三层费率解析不后置到 `router.BuildSnapshot`**：`config` import `pricing`、在 `validate()` 阶段跑完解析，看起来像「配置层反向侵入用例层」，因此被反复提出。但这是 `docs/VirtualModelRouter_Design_v4_Quota.md` 决策表「定价的落点」一行**明文选定的方案**，「只让 report 一侧解析、`metric: cost` 另开一条运行时校验路径」正是同一行里已否决的备选（理由：两份实现容易漂移）。后置还会摧毁「`metric: cost` 费率不齐 = **加载期**错误」这条硬要求——`vmr check` 将不再能在不联网的情况下告诉你费率配错了，一个确定的加载期失败被换成运行期意外。
- **多协议适配器（`adapter/{openai,anthropic,openairesponses}`）保持独立子包，不合并也不抽取通用骨架**：三个协议看似相似，底层已存在真实分叉（如 Anthropic 529 错误重试特判、Responses 顶层 `input` 数组与 `RewriteInputRoles`）；独立子包支持编译期 `init()` 静态注册与独立单测，新增协议零侵入。合并成统一参数化结构体只是把类型多态改写为字符串 `if` 分支，可读性与扩展性反而劣化。
- **不引入端点级通用运行时 quirks 插件系统**：坚持编译期确定性，只对已证实的厂商行为差异做受控修复。
- **不合并 `Dimension`（排序）与 `Condition`（淘汰）**：淘汰依赖请求事实，排序只比较端点属性，职责分离保证接口纯粹。
- **ProviderGroup 方案分梯队落地，只做了多 Provider 端点组聚合（`endpoints[].providers`）与全局 FallbackEndpoints（`fallback_endpoints:`），不做多 Key（`api_keys`）与分级 Failover（402 跳 Key / 5xx 跳 Provider）**：一个 Provider 账号内部再拆多把 Key、并让请求期在池内随机选 Key 的方案（"运行时 KeyPool"），会违反 `core.Endpoint` "构造后不可变、`HealthKey()` 只算一次"这条贯穿 health/sticky/quota 三个子系统的不变式——`HealthKey()` 在 `Classify`/`Acquire`/`ReportFailure`/`sticky.Peek`/`findByHealthKey` 等多处调用点都被当作端点的终身稳定身份使用，运行时换 Key 会让这个身份跟着漂移。改成"配置期展开成多个独立 `core.Endpoint`"（在 `BuildSnapshot` 展开 `models:` 的同一循环层再展开 `api_keys:`）能规避这个不变式冲突，但仍剩三处真实工作量：①均衡策略在这个方案下退化为文件顺序优先（`strategy.Sort` 是稳定排序，同优先级严格按列表顺序），要兑现"随机自平衡"还得新注册一个 round-robin/random `strategy.Dimension`；②配额聚合要求同一 Provider 名下几把 Key 共享同一份 `{every, since, amount}`，只有物理账号的 `since` 锚点本来就对齐、或改用天级颗粒度（`every: 1d`，`since` 写哪天都自动对齐）才不失真；③分级 Failover 需要先拆分 `internal/adapter/classify.go` 里目前共用 `ErrEndpoint` 的 402/404（账号级配额耗尽 vs 模型不可用），而且候选列表经 `strategy.Sort`/配额重排/Sticky 重排后不保证同 Provider 的候选仍然相邻，"Provider 级错误跳过整组剩余 Key"这个判断本身就需要额外的分组信息。三处都是需要真实改动、且目前没有生产多 Key 流量验证过收益的工作，故意留到看到真实需求后再单独立项。

### 2.3 校验与防御性编程

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
- **`internal/probe` 不登记进 `zeroInternalDepPackages`**：它今天确实零内部依赖，但那张表的语义不是「当前碰巧零依赖的包全登记」，而是「**承诺**永远零依赖、任何包都可无顾虑 import 的叶子工具包」。`probe` 的包注释写明它独立成包是为了避免 `diagnose`→`router` 的 import cycle，是路由半区的协议原语；未来它需要 import `core` 完全合理。登记等于给一个从未做出的承诺加锁。同理不登记 `rundir` / `buildinfo`。
- **`internal/core/core.go` 不按领域拆成 `endpoint.go`/`quota.go`/`pricing.go`**：同包拆文件不改变任何编译依赖（`go list -deps` 逐字节相同），所以它是代码导航整理，不是架构重构，也就不该被当成一个「问题」。真正解决「core 会不会长成上帝包」的是**准入规则**，那条规则已经写在 `internal/core` 的包注释里并对存量逐条复核过。真要拆是零成本搭车项，但没有它要解决的问题。
- **`imgprep` 的 `map[string]json.RawMessage` 不与 `jsonscan` 的字节扫描统一**：图片降采样要重算尺寸并重编码图像，是深度结构化重写，字节 splice 做不到。这是三个 sanctioned deviation 里最大的一个，`CLAUDE.md` 已记载。
- **不向 Clean Architecture 四层同心圆靠拢做整体重构**：要把横跨环边界的包「归位」，就得为满足图示而拆包插接口，代价是新的包边界与一层不解决任何真实问题的间接性，违反 KISS/YAGNI 与「编译期注册、无运行时插件系统」不变式。项目已有更强且**可执行**的架构模型（两半区 + `archtest`）。附一条常被忽略的反证：`internal/config` import `internal/adapter`（校验期需要知道协议注册表里有哪些 adapter），按 CA 映射这是一条「外环依赖内环」的合法边——CA 本就不是这个项目合适的透镜。
- **不对 OpenAI 工具返回做 `error:` 关键字模糊嗅探**：经对真实生产语料库全量 495,672 条 OpenAI 工具调用结果实测扫描，确认其全部为自由文本 stdout/stderr（包含 `{ "error": ... }` 等结构化 JSON 错误字段为 0 条，占比 0.00%）。若强行做子串模糊嗅探将引入海量代码输出与测试用例文本的假阳性误报。维持架构取舍：仅对协议原生携带结构化错误标记（如 Anthropic `is_error` 字段）的工具结果进行确定性统计，不基于自由文本做不可控的语义猜测。
- **`go.mod` 保持裸模块名 `vmr`**：改名要动全项目 import 路径，无实质收益。

### 2.5 产出与工程惯例

- **用 Go 结构化代码而非 `text/template` 渲染 Markdown**：复杂条件列、对齐与动态脚注在 Go 里更容易保持类型安全和可读性。
- **不维护外部贡献者 `CONTRIBUTING.md`**：与小团队运作方式不匹配。
- **`archtest` 的文档守卫不扩展到 review 报告类文档**：守卫有意只覆盖 `CLAUDE.md`、设计文档、本文件与用户指南。review 报告会正当地讨论已删除的文件与「建议新增的 XXX 函数」，逼它们与当前状态一致等于逼人改写历史与论证。真正的风险——一份陈旧 review 被当成施工依据——**用定位而非机制解决**：权威的当前状态清单只有本文件一份（它在守卫范围内），review 报告一律是历史记录。
- **`archtest` 不加圈复杂度检查**：一次只加一个守卫。函数长度预算（全局默认 120 + 豁免）落地不久，在它跑满一段时间、确认不够用之前，不引入第二个会同时报警、且更难解释的复杂度指标。
- **`buildinfo` 只输出 VCS commit 哈希，不人工编造语义化版本**：如实反映构建来源。
- **官方用量 API 不预先抽象 `Source` 接口**：遵循 YAGNI，等真正接入第一个厂商私有用量接口时再设计。
- **聚合浮点字段在冷/热缓存两次运行间的 1 ULP 级差异不追查、不消除**（原 §1.24，2026-08-20 重新归类）：真实语料（34 文件/11374 条记录）上，`vmr-report.json` 的 `providers[].cost_estimate` 在冷启动与热缓存两次运行间出现过第 13 位有效数字的差异（`3.35415184208` vs `3.3541518420800003`），其余全部字段（含 11274 行的 `vmr-requests.json`）逐字节相同；两次**独立冷启动**之间无差异。这是浮点加法不满足结合律的教科书现象——不同累加顺序相差 1 ULP——**不是一个可以"修好"的缺陷，是浮点算术的性质**。它原先被登记在 §1（待定问题）里，是分类错误：待定意味着"以后要做点什么"，而这里唯一该做的事（差分/一致性测试用容差而不是逐字节相等）**已经是现状**（`internal/report/e2e_test.go` 用 `1e-6`、`cmd/vmr/quota_parity_test.go` 用 `1e-9*want`）。受影响字段是界面上会四舍五入显示的成本估算，`report.Pricing.Disclaimer()` 本来就声明它是估算。**唯一需要重新当作 bug 的情形**：差异大到影响判断（远超浮点精度量级），或开始出现在 `cost_estimate` 之外的字段上——那说明性质变了，不是这一条了。
- **LLM 解读层生成结构化 Finding 的准入与置信度契约**：允许 LLM 判别器产出结构化 Finding，但必须强制标记 `Source: "llm_inferred"`、结构化离散置信度（`HIGH/MEDIUM/LOW`）与原文 `EvidenceAnchor`。仅 `HIGH` 置信度且具备直接证据锚点的项在报告中以 Finding（⚠️）呈现并在标题标注 `[AI推测]`；`MEDIUM`/`LOW` 仅降级为参考提示，不混入确定性规则事实。问法严格约束在有证据支撑的事实性问题上（如 E2 任务完成度重塑为"终步完成声明是否有验证动作支撑"的 `unverified_completion_claim`，拒绝开放式主观质量打分），守住"揭示事实与过程异常而非冒充裁判"的架构边界。

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
10. **`/admin/status` 暴露 `config.Check()` 操作性告警**（`internal/server`、`cmd/vmr`）：当配置存在非 loopback 暴露或探针超时超标等操作性风险时，`/admin/status` 响应直接返回结构化 `issues` 数组，`vmr status` 亦同步渲染 WARNING 提示。
11. **`vmr report` §2 成本表口径提示脚注**（`internal/report/section_cost.go`、`internal/i18n`）：在 §2 成本估算表末尾补充口径提示脚注，明确说明估算成本包含未嗅探 usage 的降级估算部分，而 Token 列仅统计已确认数量，消除按「估算成本 ÷ Token」反推单价偏高的误导。
12. **`respnorm` 检查方法并发安全与 `copyFlush` 生命周期同步**（`internal/respnorm`、`internal/router`）：`NormalizerStream` 的所有导出查询方法（`Applied`、`RawPreStrip`、`ObservedModel`、`Usage`、`OutBytes`）统一由互斥锁同步，彻底消除客户端断开或超时提前返回时 reader goroutine 尾读导致的数据竞态风险。
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

> **覆盖范围的一处如实声明（2026-08-20，P8 收尾后更新）**：本表建成时覆盖的是当时的 §1 全部条目；
> 此后 P1–P6 期间陆续新增的 `1.18`/`1.22`/`1.23`/`1.29`/`1.30` 一直没有补进来，其余仍待补——
> **一张只覆盖一半条目的 ROI 表，它的"高 ROI 为 0 条"结论就只对那一半成立**，这一点下面 §4.2 已订正。
> `1.21`/`1.28`/`1.31` 三条已由 P7 修复（见 §3 第 27–29 项）、`1.19` 已由 P8 修复（见 §3 第 30 项），
> 均已移出本表。

| # | 问题 | 成本 | 风险 | 价值 | ROI | 判据 / 何时重估 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| 1.32 | `vmr analyze` 强制 `-render-all`（含 §1.30 的 SIGKILL） | 低→中 | 中 | 已证 | **高** | 实测 164MB vs 1.5MB。最便宜的修法（默认只渲染 `category == task`）复用 P6.3 已算好的分类，是一次默认值决策而非重构；性能剖析应排在它之后。P9.2 已排期处理 |
| 1.13 | 额度燃尽看板 | 高 | 低 | 中 | **中** | 产品价值而非技术债，按产品路线排期，不与本表其他条目抢顺序 |
| 1.22 | `chatmsg` 未覆盖 Responses API | 中 | 低 | **零（已量化）** | **不做** | 真实语料 11253 条里 `openairesponses` **0 条**。触发条件量化为"`vmr-requests.json` 出现该 protocol"，在那之前不投入 |
| 1.5 | 几个文件贴着行数预算线 | 低 | 低 | 低→中 | **低→高** | **最典型的时间相关条目**：拆分成本不随时间变化，但价值在预算报警那天从"没人被卡住"跳到"挡住了下一次提交"。守卫会自己举手，提前做纯属排队 |
| 1.1 | `vmr report` 会话分析那一趟（`collect()`）仍未缓存 | 中 | 中高 | 已证 | **中** | 聚合那一趟缓存后的实测已经证明缓存本身能把热耗时压到个位数秒量级（5.2×）；剩下这一趟触及会话/任务边界判定的正确性，风险高于聚合，需要单独配一套 cold/warm 一致性测试再动手，不是顺手就能扩展的收尾 |
| 1.2 | 全内存聚合的记录量上限 | 中 | 中 | 未证 | **低→中** | 按日分桶释放依赖严格的时间单调递增保证——一个隐蔽的正确性前提。**"目标单机场景下内存完全可控"这条判据已被实测推翻**：11374 条记录峰值 RSS 1.38GB，而原文估的是"千万级约数百 MB"。重估触发条件下调为"单次分析超约 3 万条记录，或峰值 RSS 超 4GB" |
| 1.3 | `chatmsg` 离线 `map[string]any` 分配 | 中 | 低 | 未知 | **低** | **前置条件未满足**：离线耗时由 I/O 与 zstd 主导，收益完全未测。先跑 benchmark 拿 profile |
| 1.7 | 给 `Row`/`ClientRow` 补 `CostEstimateEst`（方案 ②） | 中 | 中 | 低 | **低** | 要动 `rows.go`/`accumulateCost`/渲染层三处并再次改 `vmr-report.json` 形状，而方案 ① 已经解决了误导本身。**除非外部脚本真的需要这个字段** |
| 1.6 | §2.5 标记符号已达四个 | 低 | 无 | 主观 | **低** | 成本只是删几行，但**没有依据**：四个标记都按需渲染，健康报表一个都不出现。**真实报表读起来觉得吵了再动** |
| 1.8 | `archtest` 豁免键无法区分重名方法 | 低 | 无 | 低 | **低** | 今天零影响（重名方法全部远低于默认限额）。现在改是**为不存在的场景加机制**。第一次需要豁免重名方法时再改 |
| 1.9 | 探针请求绕过审计日志 | 中 | 中 | 低 | **低** | 探活消耗极低，而混入报表会污染业务 SLO 统计——**成本主要在决定呈现口径，不在写代码** |
| 1.10 | 审计 `write` syscall 在全局锁内 | 高 | 中 | 未证 | **低** | 异步队列要处理背压策略（丢弃 vs 阻塞）与优雅关停等待，**换来的是一个尚未被证明存在的瓶颈**。高并发压测顶到写锁再说 |
| 1.14 | 滑动时间窗限流模型 | 中高 | 低 | 低 | **低** | 当前日历对齐的近似对目标场景（月度/日度 token plan）够用。属产品路线 |
| 1.17 | `imgprep` 解码闸门的阈值量纲 | 中 | 中低 | 未证 | **低** | 闸门已存在（`maxDecodePixels`，防炸弹），缺的只是一道按内存预算设的更低阈值。**前置条件未满足**：无任何实测显示该峰值造成过问题，而方案自带「用账单换内存」的取舍，不能替用户默认决定。零风险的一半（文档记账）已落地。**真实视觉负载观测到内存突刺时重估** |
| 1.15 | 分析半区体量增长 | — | — | — | **N/A** | 不是一个可修的条目，是一条**持续性约束**：新的探索性分析指标先用外部脚本消费稳定数据契约验证价值，再谈合入 |

### 4.2 分档结论

- **高 ROI（原 2 条，2026-08-20 新增；`1.31` 已由 P7 修复移入 §3，剩 `1.32`）**：这是本表建成以来
  第一次出现高 ROI 条目，正是下面那条"补充判据"预言的异常形态：价值已证、成本低、却还在等。两条
  同源——都是 P1–P6 重构的"最后一公里"，都出在 `vmr analyze` 这个 P6 新增的默认入口上，都因为验收
  走查跑的是 `analyze` 而不是用户日常会跑的 `report` 而被系统性跳过。`1.31` 已在 P7 排掉；`1.32`
  留给 P9（`story_report_dev_plan_2_sonnet-5.md` P9.2 已排期）。
- **中 ROI（2 条，看触发；`1.28` 已由 P7 修复移入 §3）**：`1.13` 按产品路线排；`1.1` 是 P3.6 之后
  **收益已经用真实语料证明**（5.2× 缓存收益）、只是正确性风险需要额外测试基础设施兜底的条目——与
  "收益未经测量"的低 ROI 组本质不同。原 `1.16`（并发竞态）、`1.4`（流式断开审计语义）与
  `1.12`（图片缓存容量上限）均已完成修复闭环。
- **明确不做（1 条）**：`1.22`——它自己提出的前置问题（Responses API 占比）已经用真实语料回答了，
  答案是 0%。**一条给出了量化触发条件的"不做"，比一条无限期的"待定"更有价值**：前者有明确的
  重开条件，后者只会被反复重新讨论。
- **低 ROI（10 条，等触发条件）**：其中 `1.2`/`1.3`/`1.7`/`1.10`/`1.17` 的共同点是**收益未经测量**——它们不是"不值得做"，
  是"还不知道值不值得"，而先做优化再测量正是这个项目一贯拒绝的顺序。`1.5`/`1.6`/`1.8` 则是守卫或真实反馈会
  自己举手的事。

**关于这张表本身的一个观察（2026-08-20 更新）**：这段原文写的是"13 条里高 ROI 为 0 条；没有一条是
'高价值但一直没做'……如果哪天这张表里出现了「价值高 + 成本低 + 却还在等」的条目，那才是需要解释的异常"。
**那一天到了**：`1.31`/`1.32` 正是这样的条目。它们的解释也很清楚，值得单独记住——
**P6 的验收走查用的是 `vmr analyze`（最全的那条路径），而用户日常跑的是 `vmr report`（默认路径），
默认路径上的缺陷因此被系统性地跳过了。验收要在默认路径上做，不能在最全的那条路径上做。**
这条清单原本的干净不是假的，只是它的干净取决于**发现问题的那次检查覆盖了哪条路径**。

**一个补充判据**：`1.16`（原条目，现已闭环）是这张表里第一条**从源码注释里捞出来、而不是从代码里读出来**的条目——
它早就写在 `respnorm` 的 `qmu` 注释里，注释还写着「见本文件的既有条目」，而那个条目当时并不存在。
清单的价值取决于覆盖率：**一条只存在于源码注释里的已知问题，等于没有被跟踪**。
§2 同批补登的四条刻意取舍出于同一个理由——它们都曾被独立审查者当作新问题重新提出过一遍。
