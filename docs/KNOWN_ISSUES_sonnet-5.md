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
- **§1 分布**：中危 4 项、低危 18 项，无高危项（另有 1 项已评估决定不做，登记备查，不计入分布）。
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

- **现状**：`AnalyzeSessions` 常驻全部记录的关键信息；原始耗时、延迟、Token 样本保存在切片中以计算真实百分位数。千万级记录规模下约占数百 MB。
- **可能方案**：利用审计日志的时间局部性按自然日分桶，跨日后即时释放原始切片。
- **为什么待定**：目标单机场景下内存占用完全可控；分桶释放依赖严格的时间单调递增保证。

### 1.3 [低] `chatmsg` 离线解析路径的 `map[string]any` 分配

- **现状**：`internal/chatmsg` 有 43 处 `map[string]any`，全部在离线消息/SSE/usage 解析路径上。转发热路径不受影响——实测 `adapter`/`audit` 为 0，`router` 唯一一处在 `WriteError` 的错误响应体，`server` 的 8 处全在 `/admin/status` 与 `/v1/models`，`probe` 1 处在后台主动探活请求体构造，没有一处在客户端转发链路上。
- **为什么待定**：**前置条件未满足**。离线路径的耗时由磁盘 I/O 与 zstd 解压主导，改用具体结构体的收益完全未经证明。先在真实审计日志上跑 benchmark 拿到 profile，再谈是否值得。没有 profile 数据之前不动。

### 1.5 [低] 几个文件贴着行数预算线

- **现状**：`internal/story/compare.go`(846/850)、`cmd/vmr/cmd_story.go`(756/850) 贴线。
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

- **现状**：分析半区（`report`/`story`/`ctxgraph`/`taskseg`/`i18n`）的代码体量已超过在线路由半区。
- **指导原则**：新的探索性 Agent 分析指标，优先用外部脚本消费稳定的 `vmr-report.json` / `journey-*.json` 数据契约做验证，证明价值后再评估是否合入主仓库。

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

### 1.19 [中] JSON 输出的语言策略：`story`/`report` 两个包目前不一致

- **现状**：在修复 phase1a/1b 复核发现的 3-1（journey JSON 缺 LLM Finding）时，`internal/story` 顺带把 `journey-<id>.json` 的 `Findings`/`LLMFindings` 文本从"固定英文"改成了跟随 `-lang`（`Summarize(j, lang)`），但没有同步改 `internal/story/compare.go`（`MetricDiff.Label` 仍固定英文）也没有改 `internal/report`（`vmr-report.json` 的 `efficiency[]` 仍固定英文，见该包 `buildFindingsForJSON`）。结果是：`journey-<id>.json` 现在跟随语言，`compare-<a>-<b>.json`/`vmr-report.json` 仍固定英文——同一个项目、同一类字段，两条不同规则并存，且 `docs/VirtualModelRouter_Design_v4_Analytics.md` §4.3 描述的"JSON 恒英文"规则目前只对后两者仍然成立。
- **为什么待定**：这不是一个"顺手修"级别的问题——统一到哪个方向（JSON 也跟随 `-lang`，还是把 `story` 这次的改动退回固定英文）会牵动 `report` 包（`Build()` 目前完全不接收 `lang`）、`story.Compare()` 的签名（目前也不接收 `lang`）、两处已经用测试锁死"JSON 恒英文"这个断言的回归测试、以及设计文档 §4.3 的整节重写，需要先定下语言策略原则再动代码。
- **推进方式**：已有完整方案文档 `docs/future-strategy/json_lang_policy_plan_sonnet-5.md`，写明了倾向的方向（叙述文本统一跟随 `-lang`，`Code`/`EvidenceAnchor` 保持语言无关的机器锚点）、需要动的模块清单、以及当前的阶段性状态。下次要推进这项工作时先读那份文档，不要凭这条条目里的只言片语重新分析一遍。
- **登记来源**：本条目对应的复核与方案讨论，2026-08-17。

### 1.20 [低] Journey 报告 fact-layer（`## t0X`/`### Step N`）与决策脊柱内容重复，理想形态是链接到 `vmr report` 的 per-request detail 文件

- **现状**：决策脊柱之后的 `## t01 · ...`/`### Step N ...` fact-layer 完整重复展示了脊柱已经摘要过的同一批底层数据（带完整消息体）。理想形态是脊柱直接链接到 detail 文件，点开才看详情。
- **原"不可行"论证已被推翻**：本条目曾经的判断——`story`/`report` 的 import 边界禁止互相引用、且 `detail.go` 的文件名依赖跨批次去重计数器导致 `story` 侧无法确定性推算——已被 `docs/future-strategy/story_report_architecture_opus-5.md` §4.8/§7.6(a) 推翻：detail 渲染先做减法（砍掉只在 report 聚合阶段才存在的位置坐标），下沉为两半区共用的叶子包，文件名改为由请求级坐标（`basename:line` 的哈希）派生，不再依赖批次计数器，也不需要跨包 import。
- **P2 已完成前提，链接本身排在 P5.2**：请求级坐标（`basename:line`，`ctxgraph.Manifest.Req`/
  `RequestRow.Req`）与详单确定性命名（`internal/reqdetail.FileName`，坐标哈希，无批次计数器、
  无本机时区依赖）均已落地——同一条记录经 `vmr report` 全量路径与子集路径生成的详单文件名与正文
  逐字节相同，已用真实语料验证。**脊柱挂链接本身**（把这个坐标/命名接到 `vmr story` 的决策脊柱
  渲染上）仍未做，按 `docs/future-strategy/story_report_dev_plan_opus-5.md` 排在 P5.2，因为它
  同时依赖 P4 补齐的机读层结构。
- **登记来源**：本条目对应的复核，2026-08-19；"不可行"论证的推翻见架构文档同日复核；P2 完成状态
  见 `docs/future-strategy/story_report_p2_action_plan_sonnet-5.md`。

### 1.21 [低] 决策脊柱的指令展示未复用 P1.3 为 Compare 新增的方言过滤/全文能力

- **现状**：P1.2 的"💬 指令"单行只在任务中途追加指令时渲染（`renderSpineBriefStep` 的 `taskStepIdx > 0` 分支），任务自己的开篇 Step 仍只靠 Task 标题（`taskseg.Preview` 截断到约 80 字符）展示指令，没有 P1.3 给 Compare 报告加的"有界折叠、完整原文"那一层。而取中途追加指令文本的 `firstNewUserText`（`render_spine_step.go`）只取该 Step `NewEvents` 里第一个 `Role=="user"` 消息，没有像 P1.3 的 `initialInstructionStats` 那样经过 `taskseg.Profile.RealUserText` 的方言过滤——如果 OpenClaw 家族客户端在真实指令前注入了一条同样标记为 `role=user` 的脚手架消息（如工具结果图片附件提示，见 `openclaw.go` 的 `RealUserText` 已识别的几类噪声），会取到错误的文本。
- **为什么不在本次一并修**：影响面有界（渲染层的一行预览，不影响 Finding 证据或落盘数据），但要把 `taskseg.Profile` 一路串到 `RenderMarkdown` → `renderDecisionSpine` → `renderSpineBriefStep` 的调用链上（`writeJourneyFile`/`RenderMarkdown` 目前都不接收 Profile），改动面比 P1.3 的对应修复更大。P1.3 已经证明了正确做法（`prof.RealUserText`，而不是裸扫 `NewEvents`/`Events`），下次改这条链路时应该复用同一模式，不要重新发明。
- **登记来源**：2026-08-19 P1 执行期间的独立代码审阅发现。

### 1.22 [低] `chatmsg.ToolResultList`/`ToolCallList` 未覆盖 OpenAI Responses API 的 `function_call`/`function_call_output` 形状

- **现状**：`chatmsg.Messages` 已经能把 Responses API 的 `function_call_output` 渲染成人读文本，但结构化提取层 `ToolResultList`/`ToolCallList`（`toolresults.go`/`messages.go`）只覆盖 OpenAI Chat Completions 的 `tool_call_id` 与 Anthropic 的 `tool_use`/`tool_result` 两种形状——`toolResultsFor` 三级配对、三个 Finding 检测器、决策脊柱渲染，对纯 Responses API 流量都不会展示任何工具调用结果。
- **为什么不在本次一并修**：P1.1 的范围是修那两种已有形状里的 ID 改写问题，不是扩展协议覆盖；这是一个独立的、更早就存在的协议覆盖缺口，需要先确认 Responses API 流量在真实语料里的占比再决定值不值得投入。
- **登记来源**：2026-08-19 P1 执行期间的独立代码审阅发现。

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

### 1.24 [低] 真实语料上，`vmr-report.json` 极少数聚合浮点字段在冷/热缓存两次运行间存在 1 ULP 级差异

- **现状**：在 §1.23 提到的同一次 34 文件/177MB 语料实测中，冷启动（全量重新解码）与热缓存
  （`ingestCachedFile` 命中路径）两次运行的 `vmr-report.json` 里，`providers[]` 的某一项
  `cost_estimate` 出现了第 13 位有效数字级别的浮点差异（如 `3.35415184208` vs
  `3.3541518420800003`），其余全部字段（含逐条 `vmr-requests.json`，11274 行）逐字节相同。两次
  **独立的**冷启动运行（互不共享任何缓存）之间未观测到任何差异——已用同一份语料复现验证。
- **判断**：现象与浮点数结合律不成立（`(a+b)+c` 在不同累加顺序下可能相差 1 ULP）的经典特征完全
  吻合，而不是某个字段的值算错了——受影响字段是一个用户界面上会四舍五入到小数点后几位显示的
  成本估算金额，`report.Pricing` 自己的 `Disclaimer()` 已经声明这类数字是估算，不是精确账单。诊断
  过程已经排除了 P3.6 缓存重放路径本身的系统性 bug（`vmr-requests.json`——即每条记录的实际业务
  数据——在两次运行间逐字节相同，证明 `recordFacts`/`buildRec2` 的重放结果完全正确；差异只出现在
  跨全部记录做浮点累加的下游聚合步骤里）。
- **为什么不现在追查到底**：影响面确认为纯展示精度、非业务数据，追查具体是哪一步累加顺序不同
  需要给聚合路径加临时插桩，对一个不影响正确性的现象而言投入产出比低。
- **触发条件**：如果发现同一份数字在两次报表之间的差异大到影响判断（远超浮点精度量级），或者
  这类差异开始出现在 `cost_estimate` 之外的字段上，说明性质变了，需要重新当作真实 bug 排查。
- **登记来源**：2026-08-20 P3 批 D 收尾阶段，冷/热缓存输出一致性核实时发现。

### 1.25 [低] `vmr replay -req` 要求同时给坐标和位置参数，允许贴入即用会更顺手

- **现状**：`-req basename:line` 目前必须搭配一个显式的位置参数（原始审计文件路径），且要求它的
  `CanonicalPath` 与坐标的 basename 一致，否则报错——这是 P3.2 的既定设计（坐标本身不含目录，
  解析必须有外部输入补全）。代价：从 `vmr-requests.json`/`journey-*.json` 复制到的 `req` 字段
  （如 `"vmr-audit-2026-07-28.jsonl:317"`）不能直接贴进命令行执行，还要再补一遍文件路径。
- **可能方案**：位置参数缺省或传入一个目录时，按坐标的 basename 在该目录（或
  `config.yaml` 的 `log_dir`）下自动定位文件（含 `.zst` 变体），只有显式给了具体文件路径才做
  一致性校验。
- **为什么不在 P3 顺手做**：这不是修 bug，是加一项 `vmr replay` 今天完全没有的能力
  （`-line`/`-ts` 同样要求显式文件路径，`replay` 没有任何"按 log_dir 找文件"的既有逻辑可复用）——
  跟 DevPlan P6.5"统一的记录选择器"是同一件事的一部分，等 P6 做 CLI 收敛时一并设计更连贯
  （避免 `-req` 一个 flag 先斩后奏出一套目录搜索规则，等 P6 到了又要跟 `-ts`/新读取原语的规则
  合并对齐）。
- **登记来源**：2026-08-20，P3 ActionPlan 的独立并发评审（`story_report_p3_action_plan_review_
  gemini-3.7-flash.md`）指出，核实为真实可改善的体验问题，非阻断项。

### 1.26 [低] 证据条目跨目录深度的相对链接规则，只有 `details/` 一处写清楚了

- **现状**：`internal/reqdetail` 目前只有一个证据链接来源——`details/<file>.md` 到
  `evidence/<file>.md` 的 `../evidence/<file>.md` 相对路径（见 `EnsureRendered`/`renderClientRequest`
  的实现与 `NewDetailWriter` 的 `evidenceDir` 约定）。P4/P5 让 `vmr story` 的 `stories/journey-
  <id>.md`/未来的 `vmr-report.md` 也链接同一批证据条目时，各自到 `evidence/` 的相对路径深度不同
  （`stories/` 同样是 `../evidence/`，根目录的 `vmr-report.md` 则是 `evidence/`，不带 `../`），
  需要显式对齐，不能想当然复制 `reqdetail` 这一份写法。
- **为什么现在不需要处理**：P3 范围内没有第二个证据链接来源，写一条"以防万一"的通用规则没有
  消费方能验证对不对。
- **登记来源**：2026-08-20，同上并发评审 §3.2，判断为对 P4/P5/P6 有效的前瞻提醒，非 P3 本身缺陷。

### 1.27 [已决定不做，登记备查] `.parse-cache/` 分片不做孤儿回收（旧 hash 分片、schema 升级后的旧版本分片）

- **现状**：`ctxgraph.SaveCacheDir` 只增量写入当前 `FileCache.Files` 里存在的分片，从不删除磁盘上
  多出来的（文件改名/轮转后旧 hash 对应的分片、`CacheSchemaVersion` 升级后不再被任何 hash 命中的
  旧版本分片）。
- **决定**：不做主动回收。理由与架构文档 §7.6(c) 对 `evidence/` 目录的同一条判断一致——`.parse-
  cache/` 是完全可再生的派生产物，孤儿分片只浪费磁盘（单份几 KB 到几十 KB 量级，历史语料按天
  轮转，累积速度有限），引入引用计数或 GC 扫描是这个体量下的过度设计；需要清理时整目录删除重建
  即可，`vmr report`/`vmr story` 都能从空缓存目录正常冷启动。
- **登记来源**：2026-08-20，同上并发评审 §2.3 提议补一个 `CleanCacheDir` 辅助函数；核实后判断为
  主动不做，登记在这里防止被重新提出。

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

| # | 问题 | 成本 | 风险 | 价值 | ROI | 判据 / 何时重估 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| 1.13 | 额度燃尽看板 | 高 | 低 | 中 | **中** | 产品价值而非技术债，按产品路线排期，不与本表其他条目抢顺序 |
| 1.5 | 几个文件贴着行数预算线 | 低 | 低 | 低→中 | **低→高** | **最典型的时间相关条目**：拆分成本不随时间变化，但价值在预算报警那天从"没人被卡住"跳到"挡住了下一次提交"。守卫会自己举手，提前做纯属排队 |
| 1.1 | `vmr report` 会话分析那一趟（`collect()`）仍未缓存 | 中 | 中高 | 已证 | **中** | 聚合那一趟缓存后的实测已经证明缓存本身能把热耗时压到个位数秒量级（5.2×）；剩下这一趟触及会话/任务边界判定的正确性，风险高于聚合，需要单独配一套 cold/warm 一致性测试再动手，不是顺手就能扩展的收尾 |
| 1.2 | 全内存聚合的记录量上限 | 中 | 中 | 未证 | **低** | 按日分桶释放依赖严格的时间单调递增保证——一个隐蔽的正确性前提。**目标单机场景下内存完全可控** |
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

- **高 ROI（0 条）**：无。原高 ROI 的 2 项（`1.11`、`1.7①`）均已完成修复闭环。
- **中 ROI（2 条，看触发）**：`1.13` 按产品路线排；`1.1` 是 P3.6 之后唯一一条**收益已经用真实语料证明**（5.2× 缓存收益）、只是正确性风险需要额外测试基础设施兜底的条目——与"收益未经测量"的低 ROI 组本质不同。原 `1.16`（并发竞态）、`1.4`（流式断开审计语义）与 `1.12`（图片缓存容量上限）均已完成修复闭环。
- **低 ROI（10 条，等触发条件）**：其中 `1.2`/`1.3`/`1.7`/`1.10`/`1.17` 的共同点是**收益未经测量**——它们不是"不值得做"，
  是"还不知道值不值得"，而先做优化再测量正是这个项目一贯拒绝的顺序。`1.5`/`1.6`/`1.8` 则是守卫或真实反馈会
  自己举手的事。

**关于这张表本身的一个观察**：13 条里高 ROI 为 0 条；没有一条是"高价值但一直没做"。
这说明这份清单已经被治理得非常干净——**剩下的绝大多数是真正该等的，不是积压的**。
如果哪天这张表里出现了「价值高 + 成本低 + 却还在等」的条目，那才是需要解释的异常。

**一个补充判据**：`1.16`（原条目，现已闭环）是这张表里第一条**从源码注释里捞出来、而不是从代码里读出来**的条目——
它早就写在 `respnorm` 的 `qmu` 注释里，注释还写着「见本文件的既有条目」，而那个条目当时并不存在。
清单的价值取决于覆盖率：**一条只存在于源码注释里的已知问题，等于没有被跟踪**。
§2 同批补登的四条刻意取舍出于同一个理由——它们都曾被独立审查者当作新问题重新提出过一遍。
