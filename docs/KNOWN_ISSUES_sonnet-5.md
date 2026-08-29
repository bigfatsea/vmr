<!-- Ver 2026-08-29, by Sonnet 5 -->

# vmr — Known Issues（已知问题与架构取舍清单）

> **定位**：vmr 已知问题、待评估演进项与刻意架构取舍的**唯一权威、持续维护的当前状态清单**。
> 发现新问题先在这里查一遍，再决定它是不是新的。
>
> **维护原则**
> 1. **只记当前事实**，不堆「哪个批次改了什么」的过程流水账——那是 `CHANGELOG.md` 和 git history 的职责。
> 2. **四类分区**：§1 待定问题（有优化空间，等真实负载/触发条件）｜§2 确定不修（连同 First Principles 决策逻辑，避免被反复重新提出）｜§3 已闭环（一句结论防重复立项）｜§4 ROI（给 §1 估成本/风险/价值）。
> 3. **每条都要能对源码核实**。核实不了的，说明已过期，删掉。
> 4. **`§1.N`/`§2.N`/`§3 item N` 是稳定 ID**，被源码注释引用——压缩散文可以，重编号不行。

---

## 0. 当前状态

- **稳定性与安全性**：无凭证泄漏、并发竞态或服务阻断级别的缺陷；单机生产环境可稳定运行。`copyFlush` 异常路径下的 `respnorm` 查询方法全部互斥锁同步，`-race` 全绿并经端到端流式断开集成测试守护。曾经 buffered 模式上游中途断流导致的客户端可见数据丢失（收到干净空 200）已修复（§3「B1」）。
- **自动化基线**：`go test ./...` 与 `go test -race ./...` 全绿；`internal/archtest` 强制导入单向边界、文件/函数行数预算、文档引用完整性。
- **§1 分布**：高危 0、中危 3（`1.2`/`1.17`/`1.18`）、低危 16，合计 19。无 `[中低]` 条目。§4 ROI 表结论：高 ROI 0 条——待办里没有「价值高、成本低、却一直没做」的异常。

---

## 1. 待定与待解决问题

> 标题方括号里是**严重程度**（现在有多糟），不是优先级。它与 §4 的 **ROI**（现在做有多划算）
> 是两个正交轴，不该也不会一致。要排期看 §4，要判断「现在有多糟」看这里。

### 1.1 [低，已部分闭环] `vmr report` 多文件输入：会话分析那一趟（`collect()`）仍未缓存（含原 1.23）

- **现状**：`report.Build`/`BuildCached` 跑三趟扫描——① `ctxgraph.ScanCached`（manifest，已缓存）② `collect()`（会话/任务分组用的每记录特征）**未缓存，每次全量重跑** ③ `aggState.scanFiles`（指标聚合，P3.6 已接 `factscache.go` 缓存）。③ 接缓存后真实语料实测热耗时 5.2×，但 ② 未缓存使热耗时离个位数秒仍有差距。
- **为什么不顺手做**：`collect()` 产出（`ReqInfo`）直接喂 `group()`/`ctxgraph.StitchGraph` 做会话/任务边界判定，正确性敏感度高于纯指标聚合（算错是把不相关对话缝到同一 Journey）。
- **触发条件**：投入前先补一套对等的 cold/warm 一致性测试（参照 `TestBuildCached_WarmMatchesBuild`）。

### 1.2 [中] `vmr report` / `vmr analyze` 全内存聚合的记录量上限

- **现状**：`AnalyzeSessions` 常驻全部记录关键信息 + 原始耗时/延迟/Token 样本切片（算真实百分位）。实测万级记录即 GB 级 RSS（`report` 单跑约 1.4GB / 1.1 万条；`analyze` 组合路径约 3.75GB / 1.5 万条）——原文「千万级约数百 MB」是量级判断错误，已作废。
- **可能方案**：按审计日志的时间局部性分自然日分桶，跨日即时释放原始切片。
- **为什么仍待定**：这个量级目前仍跑得完（4GB 在 16GB 机器上有余量），且分桶释放依赖「记录时间严格单调递增」这个隐蔽正确性前提，不成立就是静默算错而非报错。**触发条件：单次分析语料 > 约 2 万条（`analyze` 路径）/ 3 万条（`report` 单跑），或峰值 RSS > 4GB**——按实测斜率约三个月历史日志。
- **一处已做的收窄**：`ctxgraph/stitch.go` blob 倒排索引 `map[Hash]map[int]bool` → `map[Hash][]int`（去掉数百万单元素小 map 头开销），只降常数、不改分桶前提。
- **相关未做项（warm-path，登记待触发）**：语料不变、只渲染单个 journey 时，`setupStoryRun` 仍无条件全量 `ScanCached` + `buildGraph` + `StitchGraph`。窄路径需给 `vmr-stories.json` 的 `JourneyIndexRow` 补 `stitch_edges`（每条 lineage 的前驱边持久化，按内容寻址 `LineageID` 重放，避开 tie-break 不确定性）+ 一条陈旧性闸 warm path。触发条件同上（语料 > 约 3 万条，或大语料上反复 `-journey` 调查）。

### 1.3 [低] `chatmsg` 离线解析路径的 `map[string]any` 分配

- **现状**：`internal/chatmsg` 43 处 `map[string]any`，全在离线消息/SSE/usage 解析路径。转发热路径实测零命中。
- **为什么待定**：离线路径耗时由磁盘 I/O 与 zstd 主导，改具体结构体的收益未经证明。先在真实审计日志跑 benchmark 拿 profile，没有 profile 数据前不动。

### 1.6 [低] §2.5 表格的标记符号已达四个

- **现状**：`⭐` 超额度 / `‡` 配置变更 / `†` 无时间交集 / `◇` 部分流量未计价，各配一条按需渲染脚注。信息都必要，但四个符号叠一张表可能已到「标记多到没人看脚注」的临界。
- **为什么待定**：主观展示密度判断，四个标记都按需渲染，健康报表一个都不出现。真实报表读起来觉得吵了再动（`◇` 是最可能降级为纯 JSON 字段的候选）。

### 1.7 [低] `vmr report` §2 成本表结构化透传 `CostEstimateEst`（方案 ②）

- **现状**：方案 ①（Markdown 口径提示脚注）已闭环。方案 ② 要给 `Row`/`ClientRow` 补 `CostEstimateEst`、改 `rows.go`/`accumulateCost`/渲染层三处，并再次改 `vmr-report.json` 形状。
- **为什么待定**：无明确外部程序消费需求前遵循 YAGNI。

### 1.8 [低] `archtest` 函数长度豁免的键无法区分同文件重名方法

- **现状**：`funcLineExemptions` 以 `文件:函数名` 为键，同文件重名方法共用一条（如 `report/ingest.go` 6 个 `Ingest`）。今天全部远低于默认限额，无影响；一旦为其一登记豁免，其余会一并放宽。
- **可能方案**：键改 `文件:接收者类型.函数名`（`ast.FuncDecl.Recv` 已有类型信息）。
- **为什么待定**：需真的出现一个必须豁免的重名方法才有意义。

### 1.9 [低] 探针请求绕过审计日志

- **现状**：`internal/router/probe.go` 的健康探活请求不写 `audit.Record`，`vmr report` 看不到探活消耗。
- **为什么待定**：探活消耗极低；且需先明确探针流量在报表中的呈现口径，避免污染业务 SLO 统计。

### 1.10 [低] 审计落盘的 `write` syscall 在全局锁内

- **现状**：`audit.Logger.Write` 的 JSON 编码已用 `sync.Pool` 移到锁外，但写文件的系统调用仍在全局互斥锁内。
- **可能方案**：带缓冲通道 + 单独写协程。
- **为什么待定**：异步队列要处理背压（丢弃 vs 阻塞）与优雅关停等待；当前直接写入未构成瓶颈。

### 1.13 [低] 额度燃尽看板未交付

- **现状**：`vmr report` 已有额度与消耗对照子表，更进一步的长期燃尽曲线与预测看板未实现。属产品路线，不与技术债并列排期。

### 1.14 [低] 滑动时间窗（Rolling Window）限流模型

- **现状**：`internal/quota/period.go` 是日历对齐的惰性周期重置，短 tumbling 窗（如 `every: 5h`）按周期近似。真正的滑动窗需要平滑计数器（Ring）。
- **性质**：功能演进，不是缺陷——当前近似对目标场景（月度/日度 token plan）够用；滚动窗类套餐（Claude Code Pro/Max）的瞬时拒绝由健康状态机的冷却/退避兜底。**除非实测到某厂商套餐的密集 429 冲击，否则不做**——不在 README / Strategy 里当卖点讲。

### 1.17 [中] `imgprep` 解码闸门按「防炸弹」设定，其内存上界与单请求内存预算差一个数量级

- **现状**：`processImage` 在 `image.Decode` 前用 `image.DecodeConfig` 只读头取宽高，声明尺寸 > `maxDecodePixels`（64MP）直接放弃降采样原样透传。**闸门存在且工作正常**，目的是拦解压炸弹。问题在阈值量纲：64MP 按 RGBA 约 256MB/次解码，而 UserGuide「单请求内存预算」核算的是 ~32MB/请求——两个数字各自都对，回答的不是同一个问题（「多大算恶意」vs「一个请求该占多少」）。图片逐张解码逐张释放，多图不累加。
- **可能方案**：为内存预算再设一道更低的、可配置的闸门。
- **为什么待定**：够到 64MP 需刻意构造，正常截图/照片低一到两个数量级，无实测显示真实负载下造成过内存问题；且方案自带「用账单换内存」的取舍，不能替用户默认决定。零风险的一半（UserGuide/.zh + `config.example` 注释写明这段峰值由像素数决定、逐张释放）已落地。

### 1.18 [中] Phase 1b 六个 LLM 语义判别器尚未完成完整黄金样本校准

- **现状**：`internal/story/llm_findings.go` 六个判别器已实现、单测覆盖、且用 `_eval/calibrate_p1b.go` 对真实生产日志跑过真实模型验证（6 个真实 Journey 上机械核验 Evidence Anchor 有效率 100%，人工抽查合理）。但不是正式合入门禁——那需 30~50 个 Journey、每模块 ≥6 正/负例的系统性黄金样本集 + 人工标注 Ground Truth 算真实 Precision/Recall。
- **为什么待定**：黄金样本挑选与人工标注是需实际投入时间的判断性工作，无法自动化；当前抽样规模下无需立即处理的误报模式，不构成阻塞。`_eval/calibrate_p1b.go` 已是可直接复用的校准工具，扩大 `-input`/`-limit` 即可推进——**成本在人力时间，不在代码**。

### 1.22 [低，决定不做] `chatmsg.ToolResultList`/`ToolCallList` 未覆盖 OpenAI Responses API 的 `function_call`/`function_call_output` 形状

- **现状**：`chatmsg.Messages` 已能把 `function_call_output` 渲染成人读文本，但结构化提取层只覆盖 OpenAI Chat Completions 与 Anthropic 两种形状。纯 Responses API 流量下脊柱不展示工具结果、三个 Finding 检测器无证据、`journey-<id>.json` 的 `tool_calls` 会静默报告「这一步没有工具调用」（机读契约降级读者看不出来）。
- **决定不做**：真实语料按 `protocol` 统计 `openai-responses` **0 条 / 0.0%**——一次都没触发过。**触发条件（量化）**：任意一次 `vmr report` 的 `vmr-requests.json` 出现 `protocol == "openai-responses"` 的记录，即重新排期。

### 1.29 [低，暂不做] `journey-<id>.json` 的 `structure` 字段没有 schema 版本戳

- **现状**：`.parse-cache/` 有 `CacheSchemaVersion`，`journey-<id>.json` 无等价机制——P4 前后生成的旧/新文件字段名相同、形状不同，消费者无法仅凭文件本身分辨。
- **为什么暂不做**：YAGNI + 已裁决「JSON 无外部脚本消费」——`journey-<id>.json` 至今唯一已知程序化消费方是 `_eval/calibrate_p1b.go`（只读 `EvidenceAnchor`）。没有消费者，就没有人需要探测版本。
- **触发条件**：出现第一个 `_eval/` 之外的程序化消费方。加 `schema_version int` 成本接近零，但改在下次新增字段时最便宜。

### 1.48 [低] 错误分类词表的长期形态：端点级 quirk 统一模块（含 sticky 降级优化）未做

- **现状**：vendor 知识散在 `DefaultClassify` 的全局词表里（`contentHint`/`contextLimitHint`/`upstreamHint`/`vendorQuirkHint`/`authHint`，约 30 词）。已知厂商专属误判已由 `ErrQuirk` 类 + 词条修复覆盖（§3「45」），词表之间尚未互相干扰。
- **可能方案（升级时直接可用）**：每 vendor 一个编译期注册的 quirk profile，按 **model glob** 匹配（不按 provider 名——用户自起名，改名即静默失效），字段含 marker 表 / 建议分类 / sticky 策略；`DefaultClassify` 保留为兜底。**附带**：quirk 命中时对 sticky 会话降级（清粘性/降权），消除中毒会话每轮 ~1–2s 的重复失败往返。
- **触发条件**：全局词表增长到出现互相干扰/误命中，或 sticky 重复往返在真实负载中可观测地拖慢中毒会话。

### 1.49 [低，非活跃——仅 32-bit] `imgprep` 解压炸弹守卫的像素乘积在 32-bit `int` 平台可溢出

- **现状**：`processImage` 用 `cfg.Width*cfg.Height > maxDecodePixels` 挡炸弹（`cfg.Width`/`Height` 是 `int`）；32-bit 平台两值接近 `int32` 上限时乘积回绕成小值绕过守卫。
- **为什么非活跃**：Go `image/png` 把 IHDR 宽高钳在 `int32`，64-bit（唯一 CI/目标平台）乘积 ≤ (2³¹)² < `int64` 上限，不可能溢出。
- **修法（触发时）**：`int64(cfg.Width) * int64(cfg.Height)`，一行。**触发条件**：32-bit 成为受支持的构建/部署目标。

### 1.50 [低，潜在] 详单文件名去重位 `md5(basename:line)[:4]`（32 bit）

- **现状**：`internal/ctxgraph/reqcoord.go` 的 `ReqHash8` 给详单文件名算 4 字节 hash 去重后缀。单源文件近 1 万条记录时按生日界碰撞概率约 1%；真正撞成同一文件名还需同毫秒 + 同模型 + 同 outcome，现实可忽略。
- **恶化曲线**：与 §1.2 的语料上限同步线性恶化。真出现时把去重位提到 hash12/16 或改用递增序号，都是局部改动。

### 1.51 [低] `internal/story/mdlite.go` 行内代码里的 `**` 会在 `<code>` 内注入 `<strong>`

- **现状**：`mdInline` 先 `mdWrap` 处理 `` ` ``、再处理 `**`；若 `-compare -html` 的 LLM 解读段在行内代码里输出 `**`（如 `` `glob/**` ``），第二遍会在已生成的 `<code>` 内注入 `<strong>`。纯展示层轻微瑕疵——`html.EscapeString` 最前置，无 XSS。
- **为什么待定**：真观察到 LLM 频繁触发再微调解析状态机；`mdlite` 只覆盖解读段实际用到的 Markdown 子集这一取舍见 §2.5。

### 1.52 [低] 虚拟模型级预算硬闸（E3）未做

- **现状**：quota 的 gate/bucket 是「配速」——从不拒绝请求，只在同优先级梯队内重排端点。E3 想要的是**硬急停**：进死循环的 agent 触顶后请求被**明确拒绝**（可解析错误，绝不静默降级到便宜模型），每日零点 + 进程重启重置、不引入持久化。两者目标不同——配速降低「跑爆某套餐」概率，硬闸给「一夜烧光」设确定性上限。
- **为什么待定**：用户 hold。真要做需一个独立的内存态机制（仿 `health.Registry`，请求入口查一次），不是拧 quota 旋钮能得到的。

## 2. 刻意取舍，不是缺陷

> 以下基于项目核心哲学（KISS / YAGNI / 单二进制 / 零代码侵入）做出，已论证过，不需要重新论证。
> **推翻其中任何一条是允许的，但必须先知道自己在推翻它，并给出新的理由。**

### 2.0 永久不做（架构红线）

语义缓存（对确定性编程/Agent 任务是正确性隐患）｜MCP 网关与工具执行拦截（不在标准 LLM API 线路上）｜Web UI / 内嵌 DB / RBAC / 分布式 / 跨实例 quota｜协议互译 / bypass 模式｜`.so` 运行时插件（坚持编译期 blank-import 注册）｜让价目表进实时路由热路径｜通用 HTTP provider（映射 DSL）｜更多 LLM 检测器 / 对比维度 / corpus 维度（分析半区标 v1-complete，新增维度从默认冲动改为需理由的例外）。

### 2.1 运行时与并发

- **`health.Registry` 全局互斥锁不分片**：单机场景锁持有只是纳秒级 map 读写，分片增复杂度无吞吐收益。
- **`HealthKey` 取 SHA-256 前 4 字节**：单实例端点规模下碰撞概率可忽略。
- **健康状态机的退避冷却参数硬编码**：坚持「零调参」，不暴露难以科学校准的旋钮。
- **`copyFlush` 的 goroutine + channel 流水线**：避免在底层连接层设全局 Deadline 破坏 TLS/Header 超时语义。
- **客户端取消时不停止计费**：上游已生成的 token 厂商照收，路由侧照收才与账单对齐；改成不计费会让 `vmr report` 系统性低估消耗。取消的**传播**（中止上游连接）已通过 `BuildRequest(r.Context(), …)` 自动完成；取消的**检测/归类**（审计准确标 `canceled`，§3「13」）需 `copyFlush` select 一次 `ctx.Done()`。
- **`respnorm.Read` 等待更多字节时返回 `(0, nil)`**：唯一消费方 `copyFlush` 显式处理；改成内部阻塞循环会让 idle 看门狗失去以读取为粒度的心跳。
- **`respnorm` 的 usage sniffing 不外移为 `router` 侧装饰器**：装饰器要在转发热路径每 chunk 多付一次接口调用；当前实现搭 `ingest` 已有的 per-chunk 循环，零额外开销。理由在 `internal/respnorm` 包注释末尾。
- **`respnorm` 的观测标记 `crlf_framing_suspected` / `thinking_process_pattern_detected` 不删**：字节未改动，只往审计 `norm` 串加一个标记，看似无消费者——实则被 `internal/reqdetail` 详单页逐条叙述，`thinking_process_pattern_detected` 另进 `internal/report` 的 `diagnosticNormMarker` → `EndpointRow.NormCounts`，作为「剥离规则是否失效」的跨请求频率预警。这是在用的低成本预警，不是死代码。
- **`GET /health` 为存活探针而非就绪探针，永不因上游不可用返回非 200**：与上游健康绑定会让容器编排在所有供应商不可用时触发无休止重启，放大雪崩。需要就绪度的调用方消费 `/status` 的模型健康块。
- **`/status` 的 `traffic.requests` 含未鉴权/被拒请求**：口径是「进程见过多少 HTTP 请求」，不是「成功路由了多少」。精确语义在审计日志。
- **`traffic.by_status` 按请求（非 attempt）计数，且不保证 `ok+canceled+error == total`**：failover 中间 attempt 不计入，少数早断路径不记或记入 error。展示语义，非路由语义。
- **`/status` 的 `traffic.by_status` 在流式中途截断时记 `error`，与审计顶层 `outcome` 记 `ok` 口径不同（刻意不对齐）**：前者答「客户端是否拿到完整响应」，后者答「HTTP 交换是否在传输层正常完成」，各自口径内部自洽。两处代码（`Telemetry.RecordOutcome` doc comment、`forwardSuccess` 调用点）互相指向本条。截断的客户端信号（§3「B1」）：TRUNCATED 时 `forwardSuccess` 在把可安全交付的已收字节 flush 给客户端后 `panic(http.ErrAbortHandler)`，此账本口径不受影响。
- **`/status` 端点项刻意不加端点级累计计数器（requests / ok / failed / tokens）**：`consecutive_failures` 出现在 `/status` 因为它是**当前健康状态**读数（liveness 视图）。端点级累计账是**分析半区**职责——`internal/report` 的 `EndpointRow`（Attempts/OK/Forwarded/Failed/Availability/ErrorClasses/tokens/cost，含 by-date 与 cross-date）已完整产出，数据源是可持久化、可按时间切片的审计日志。给 `/status` 塞一份进程内、重启即失的实时副本：① 与分析半区双账本（正是「一个分析数字复现一个路由数字必须差分测试锁定」要防的负担）；② 做全（4–8 个计数器 × N 端点）等于给 `router.Telemetry` 加一张按端点的动态 map，破坏它「全固定原子、热路径零 map 零锁」的设计。
- **`system.disk.free_space` 在 Windows 上是桩（恒 0）**：`syscall.Statfs` 无 Windows 等价物，而 Windows 不是目标部署平台。
- **`/log` 慢订阅者以「丢行 + 标记」处理，永不让日志热路径阻塞**：每订阅者一条有界 channel（64 行），满则丢行插 `... dropped N lines ...` 标记；`log.html` 不做自动重连（只手动重试按钮），避免重启风暴下的重连洪水。
- **启动 banner 与 panic 直写 stderr，tee 不捕获**：banner 只出现一次，panic 时进程将死，两者都不值得为 `/log` 引入第二条写入路径。

### 2.2 配置与协议

- **协议枚举 2026-08 重命名为 `openai-completions` / `anthropic-messages`（`openai-responses` 不变），与 Pi Agent 等对齐**：全链路一步到位用新名，路由侧零兼容负担。**唯一兼容咽喉点**是 `audit.Record.UnmarshalJSON`：读到旧名经 `core.CanonicalProtocol` / `core.NormalizeEndpointLabel` 归一化，只服务分析侧读历史日志；`vmr replay` 不做兼容。`ctxgraph.CacheSchemaVersion` 已 1→2 使旧事实缓存失效。**这是「版本必须匹配、不做兼容」原则的唯一刻意例外**——历史审计文件是不可变的既存事实。config 仍带旧名时加载错误直接点名要改成什么（`internal/config/provider.go` 的 `unknownProtocolHint`），但 parser 不接受旧名（strict YAML）。**TODO(2026-10)**：过渡期约一个月后拆除 `core.CanonicalProtocol` / `core.NormalizeEndpointLabel`（`internal/core/protocol.go`，常量保留）、`Record.UnmarshalJSON` 及其为此新增的 `internal/core` import、两个 legacy-name 归一化测试；`ctxgraph.CacheSchemaVersion` 保持 2 不回退。
- **CLI 与 Server 版本必须匹配，不一致直接报错不做兼容**：单二进制、可随时重启，`vmr status` 与 `vmr start` 理应同版本——不一致说明升级没走完，报错正是暴露它。`json.RawMessage` 式兼容层只覆盖一个滚动升级窗口却永久留在代码里，违反 KISS。曾为「旧 server 缺失新 key」保留的 `serving *bool` 兜底已作为死代码删除（`instance.config` 由 string 改 object 后即不可达）——版本必须匹配的原则不再留任何字段级例外。
- **`/status` 的 `instance.base_urls` 回显请求自身地址而非 `listen` 配置**：host 取自 HTTP Host 头、scheme 取自是否 TLS——调用方用什么地址访问 `/status` 就广告什么地址，这正是客户端该填的值。纯展示、不参与鉴权或路由，Host 可伪造无安全影响；刻意不做 `X-Forwarded-Host` 解析。
- **环境变量未定义时静默展开为空串，不支持 `${VAR:-default}`**：保持配置解析简单明确，默认值在 YAML 里显式写出。
- **`internal/config` 的三层费率解析不后置到 `router.BuildSnapshot`**：`config` import `pricing`、在 `validate()` 跑完解析，看似「配置层反向侵入用例层」，但这是 Quota 设计文档决策表明文选定的方案——「只让 report 一侧解析、`metric: cost` 另开一条运行时校验路径」是同一行里已否决的备选（两份实现容易漂移）。后置还会摧毁「`metric: cost` 费率不齐 = **加载期**错误」这条硬要求。
- **多协议适配器（`adapter/{openai,anthropic,openairesponses}`）保持独立子包**：三协议底层已有真实分叉（Anthropic 529 特判、Responses 顶层 `input` 数组与 `RewriteInputRoles`、`x-api-key` vs `Authorization`）；独立子包支持编译期 `init()` 注册与独立单测，新增协议零侵入。合并成参数化结构体只是把多态改写为字符串 `if` 分支。
- **不引入端点级通用运行时 quirks 插件系统**：坚持编译期确定性，只对已证实的厂商行为差异做受控修复。
- **不合并 `Dimension`（排序）与 `Condition`（淘汰）**：淘汰依赖请求事实，排序只比较端点属性，职责分离保证接口纯粹。
- **ProviderGroup 的多 Key（`api_keys:`）已实现，运行时均衡与分级 Failover 仍不做**：早先设想的运行时 KeyPool（请求期在池内随机选 Key）会违反 `core.Endpoint` 「构造后不可变、`HealthKey()` 只算一次」这条贯穿 health/sticky/quota 的不变式。实际落地是「配置期展开成多个独立 `core.Endpoint`」：`Provider.APIKeys`（`{label: key}`）在 `config.Parse` 里展开成 `<name>-<label>` 命名的独立 `Provider` 并就地重写引用，下游全部按 `Provider.Name` 字符串解析、零改动。当初设想的三处工作，前两处被这个展开形状架构性绕开（均衡：谁排第一不可预先指定，只能读 `vmr check` 的实际展开结果，没配 quota 时排第一的吃全部流量；配额聚合：每把 key 独立 Provider 名、独立 quota 池，对齐难题不存在了），第三处（分级 Failover：402 跳 Key / 5xx 跳 Provider）维持原判，留到看到真实需求。

### 2.3 校验与防御性编程

- **`/status` 的网络可达性与身份认证解耦，且复用聊天入口的同一把 `api_keys`**：网络范围由 `listen` 决定，认证由 `api_keys` 决定——未配 `api_keys` 时任何能连到端口的人都能读 `/status`（2026-08-23 的显式决策，替代旧的 loopback-only）。这把 key 同时是管理凭证：持有客户端 key 者能看到全部端点名、provider 身份、quota 消耗与配置路径。对单人/小团队代理这是正确的简化。`config.Check()` 对「非 loopback 且无 api_keys」给 warning。
- **`vmr status -addr` 回退读取本地 config 的 `api_keys[0]` 并发送到目标地址**：`-addr` 显式指向别的实例时，把本地 key 当 Bearer 发过去。设计意图是让 `./vmr.sh ps` 对本机多实例免手工传 key；只发 key、不进 URL 或日志。目标地址是使用者自己敲的，不是网络层漏洞。
- **看板（`/status.html` / `/log.html`）把 API key 存 `localStorage`，静态外壳免鉴权直出**：外壳不含数据，数据请求走 `s.auth()`；key 只在浏览器本地持久化，不进 URL、不进服务端日志。所有配置派生字符串内插进 `innerHTML` 前均 `esc()` HTML 转义。`/log` 输出 `text/plain` 而非 SSE/JSONL（源头已是格式化文本）；无查询参数（回放窗口固定 512 行缓冲）。
- **`/help.html` / `/help.zh.html` 的 Agent 配置片段在浏览器就地装配，不做服务端模板渲染**：`/help` 按架构必须公开免鉴权，服务端渲染会逼它强制鉴权、或让服务端拿不到用户 Key。API Key 复用 `localStorage['vmr_status_key']`。服务端下发的 HTML 保留写死默认值（`coding` / `claude`、200k context、`high` effort），保证无 JS / 未鉴权时也自洽。四点取舍：max-output 预算按 context 分档经验估计（VMR 无模型级元数据）；片段一律 vision-on（空 capabilities = 不受约束）；四个列表型生成器只枚举 `openai-completions` 模型；无浏览器 JS 测试基建，`TestHelpPage_SnippetFillEngine` 只做构建期字符串守卫。
- **`nil` 校验只加在跨包公共入口且一律 fail-fast，绝不静默兜底**：已加的是 `report.AnalyzeSessionsCached` 与 `story.BuildChain`/`BuildAll`/`PreviewTitle`/`PreviewTitles` 五个入口——判据是「跨包公共 API + 后接并发扇出或递归组装」。包内被这些入口保护的函数不重复校验。
- **`fmtutil.DisplayZone` 保持裸 `var`，不封装线程安全访问器**：生产代码零写入点——全仓写入全在 `_test.go` 且相关测试无 `t.Parallel()`，`-race` 全绿。「让测试能确定性覆盖」本就是它存在的理由之一。
- **尤其不做「`prof == nil` 就回退到 `Generic`」这类静默兜底**：`OpenClawAware` 与 `Generic` 给出不同的任务标题与边界，静默换一个 Profile 会产出一份错误但看起来正常的分析结果，比 panic 难查。
- **`.parse-cache/` 不做分片孤儿回收 GC**（原 §1.27）：`ctxgraph.SaveCacheDir` 只增量写入当前存在的分片，不主动删旧 hash 孤儿分片。缓存是完全可再生的派生产物，`vmr report`/`vmr story` 均可从空缓存目录冷启动。触发条件：`.parse-cache/` 体积超过同批压缩审计日志总体积（当前实测 51MB vs 177MB），或升级后异常磁盘占用；在那之前「整目录删除重建」比任何 GC 更简单可靠。

### 2.4 包边界与依赖

- **`imgprep.ImageInfo` → `audit.ImageInfo` 的字段拷贝**：换 `imgprep` 不依赖 `audit`，保住公共工具包零依赖边界。
- **`chatmsg.ReassembleSSE` 与 `respnorm` 的 SSE 状态机保持分离**：前者面向离线完整语义提取，后者面向在线字节级保真转发，关注点不同。
- **`internal/report/cost.go` 的端点标签切分不并入 `core.SplitEndpointLabel`**：后者兼容 `:` 与 `/`，前者只认 `:`。放宽 `$` 成本估算那个调用点会改变旧格式日志的历史报表金额——一次需单独评审的行为变更，不是「统一实现」的顺带产物。
- **`core.StickyBackstopTTL` 不迁回 `internal/sticky`**：迁回制造一条 `config` → `sticky` 的新依赖边，仅用于读一个常量；不做这个校验则 `sticky_ttl` 超过 backstop 的配置会「看起来被接受、实际静默失效」。
- **`adapter` 的协议字段字面量（`"model"`/`"stream"`/`"messages"`/`"input"`）不从 `jsonscan` 导出复用**：它们是不可变字节常量而非共享状态；「知道这些字段名的含义」正是把 `SessionFingerprint`/`TopLevelProbe` 留在 `adapter` 的领域知识，也是「需要具体字段名的函数不属于 `jsonscan`」这条规则的由来。
- **不把分析半区拆成独立二进制**：坚持「单二进制单文件分发」。
- **不引入 DuckDB / cgo 做数据聚合**：保持纯 Go、跨平台零 C 依赖。
- **`i18n` 的 26 个微文件不合并**：与 `internal/report/section_*.go` 的「一节一文件」硬规则一一配对（`archtest` 强制），合并击穿 700 行全局预算，且改一节文案从打开小文件变成在大文件里找。
- **`i18n` 的 `type XxxText` + `if lang == ZH` 样板不改写成 `map[Lang]T` + 泛型 `pick`**：改写只消掉每文件 2 行分支，占体量的 struct 定义与两份字段赋值一行都省不掉，还新引入泛型 helper 与「key 缺失怎么办」。收益为负。
- **`internal/probe` / `rundir` / `buildinfo` 不登记进 `zeroInternalDepPackages`**：那张表的语义是「**承诺**永远零依赖」，不是「当前碰巧零依赖的都登记」。`probe` 独立成包是为避免 `diagnose`→`router` import cycle，未来 import `core` 完全合理。（对照：`internal/tokenutil` 承诺永不依赖内部包，已如实登记。）
- **`internal/core/core.go` 不按领域拆成 `endpoint.go`/`quota.go`/`pricing.go`**：同包拆文件不改变任何编译依赖，是代码导航整理不是架构重构。真正解决「core 会不会长成上帝包」的是准入规则，已写在包注释里并对存量逐条复核过。
- **`imgprep` 的 `map[string]json.RawMessage` 不与 `jsonscan` 的字节扫描统一**：图片降采样要重算尺寸并重编码，是深度结构化重写，字节 splice 做不到。这是三个 sanctioned deviation 里最大的一个。
- **不向 Clean Architecture 四层同心圆靠拢做整体重构**：要把横跨环边界的包「归位」就得为满足图示而拆包插接口，代价是新的包边界与一层不解决任何真实问题的间接性。项目已有更强且**可执行**的架构模型（两半区 + `archtest`）。反证：`internal/config` import `internal/adapter`（校验期需知道协议注册表）按 CA 是「外环依赖内环」的合法边——CA 本就不是这个项目合适的透镜。
- **不对 OpenAI 工具返回做 `error:` 关键字模糊嗅探**：实测全量生产语料 495,672 条 OpenAI 工具调用结果，结构化 JSON 错误字段 0 条（0.00%），全部是自由文本 stdout/stderr。子串模糊嗅探会引入海量代码输出/测试用例的假阳性。只对协议原生结构化错误标记（如 Anthropic `is_error`）做确定性统计。
- **`go.mod` 保持裸模块名 `vmr`**：改名要动全项目 import 路径，无实质收益。
- **模型/端点展示面的一致性靠统一口径 + 契约测试，不靠共享结构体**：运行时视图以 `/status` 的 `models` 数组为唯一权威（`vmr status` CLI 与 `status.html` 直接消费同一 JSON）；人类可读模型标签 `"<name> [<protocol>]"` 只在 `core.ModelLabel` 一处定义。刻意不统一的三处：`/v1/models`（协议面 schema）、`vmr check` 的分层 config 视图（看配置缺口）与 `/status` 的聚合运行时视图（并集/最大值）、`vmr diagnose` 的扁平 Result 数组。`/status` JSON 形状由 `internal/server/admin_status_test.go` 契约测试锁定。

### 2.5 产出与工程惯例

- **用 Go 结构化代码而非 `text/template` 渲染 Markdown**：复杂条件列、对齐与动态脚注在 Go 里更容易保持类型安全和可读性。
- **不维护外部贡献者 `CONTRIBUTING.md`**：与小团队运作方式不匹配。
- **`internal/story/mdlite.go` 只覆盖 `-compare -html` 的 LLM 解读段实际会用到的 Markdown 子集**（ATX 标题、段落、无序列表、GFM 竖线表格、`**粗体**`、`` `行内代码` ``——全部先转义）：`-compare` 的 LLM 提示词明确要求「结论句 + 候选根因表 + 三个三级小节」，围绕这个形状裁剪。有序列表与围栏代码块落进段落分支（已转义、无注入、不丢字符）。不引 CommonMark 解析器。已知瑕疵见 §1.19。
- **`archtest` 的文档守卫不扩展到 review 报告类文档**：守卫只覆盖 `CLAUDE.md`、设计文档、本文件与用户指南。review 报告会正当地讨论已删除的文件与「建议新增的 XXX 函数」。真正的风险（一份陈旧 review 被当施工依据）**用定位而非机制解决**：权威的当前状态清单只有本文件。
- **`archtest` 不加圈复杂度检查**：一次只加一个守卫。函数长度预算落地不久，确认不够用之前不引入第二个。
- **`buildinfo` 只输出 VCS commit 哈希，不人工编造语义化版本**：如实反映构建来源。
- **官方用量 API 不预先抽象 `Source` 接口**：YAGNI，等真正接入第一个厂商私有用量接口时再设计。
- **聚合浮点字段在冷/热缓存两次运行间的 1 ULP 级差异不追查、不消除**（原 §1.24）：浮点加法不满足结合律的教科书现象，不是可以「修好」的缺陷。唯一该做的事（差分/一致性测试用容差而非逐字节相等）**已经是现状**（`report/e2e_test.go` 用 `1e-6`、`quota_parity_test.go` 用 `1e-9*want`）。唯一需重新当作 bug 的情形：差异远超浮点精度量级，或开始出现在 `cost_estimate` 之外的字段上。
- **文件与函数行数预算线是提醒式绊线，非架构缺陷**（原 §1.5）：`internal/archtest` 的文件/函数预算（默认 700 / 120 + 豁免表）是轻量提醒机制，连 Warning 都算不上。未触线前无需焦虑、不需在常规 review 里逐个排查；一旦触线，按职责拆分重构（如 `detail.go` → `internal/reqdetail`、`config.go` 拆出 `apikeys.go`），或逻辑内聚时临时按 +15~20% 调高豁免。
- **可维护性的核心在整体架构与设计复杂度，而非代码行数**（原 §1.15）：单人可维护性取决于是否守住 First Principles / KISS / YAGNI、是否消除了不必要的过度设计与复杂分支，而非机械度量行数或两半区体量比。探索性新分析指标优先用外部脚本消费稳定的 `vmr-report.json` / `journey-*.json` 契约验证，确认真实价值后再评估主库实现。
- **LLM 解读层生成结构化 Finding 的准入与置信度契约**：LLM 判别器产出的 Finding 必须强制标记 `Source: "llm_inferred"`、离散置信度（`HIGH/MEDIUM/LOW`）与原文 `EvidenceAnchor`。仅 `HIGH` + 直接证据锚点的项以 Finding（⚠️）呈现并标 `[AI推测]`；`MEDIUM`/`LOW` 降级为参考提示。**锚点运行期强制校验**（§3「B3」）：`ComputeLLMFindings` 收完全部 detector 输出后逐条 `strings.Contains(真实 transcript, EvidenceAnchor)` 校验，非逐字子串即丢弃。问法严格约束在有证据支撑的事实性问题上（拒绝开放式主观质量打分），守住「揭示事实与过程异常而非冒充裁判」的边界。

---

## 3. 已闭环，不再重复提出

以下架构问题曾经成立，现已彻底解决并有测试守护。一句结论防重复立项；括号里是历史 §1 ID（部分源码注释按旧 ID 引用）。

1. **响应流正规化独立成包**（`internal/respnorm`）：在线状态机、模型名重写、厂商修复脱离 Router，可在纯 `io.Reader` 层 fuzz。
2. **JSON 字节扫描引擎独立成包**（`internal/jsonscan`）：零内部依赖，消除重复扫描，fuzz 保障边界。
3. **Agent 方言与任务分段收敛**（`internal/taskseg`）：`report` 与 `story` 共用方言识别、任务切分、真用户指令索引。
4. **报表聚合与提取解耦**（`internal/report`）：共享 `TrafficStats`，单体大函数拆到 `ingest.go` / `recextract.go`。
5. **公共叶子层职责净化**（`internal/core`、`internal/fmtutil`）：展示格式化下沉 `fmtutil`，`WriteJSON`/`WriteError`/`FilterClientHeaders` 回 `router`，`core` 准入规则写进包注释。
6. **额度与定价引擎精简**（`internal/quota`、`internal/pricing`）：移除分时促销等冗余功能面，固化静态费率覆盖与三层解析。
7. **架构与文档守卫可执行化**（`internal/archtest`）：包依赖边界、文件/函数行数预算、文档引用有效性全部变成会失败的测试。
8. **文件行数守卫从白名单反转为「全局默认 700 + 豁免」**：与 `func_sizes_test.go` 语义对齐。默认 700 取自实际分布（p50 131 / p90 503），反转零新增登记。
9. **`metric: cost` 混合定价端点的静默低估**：`ProviderQuotaRow.WindowUnpricedPct` + §2.5 的 `◇` 标记。「部分退化渲染成精确已知」这一失效模式的第四个也是最后一个实例。份额以**请求数**而非金额计。
10. **`/status` 暴露 `config.Check()` 操作性告警**：非 loopback 暴露、探针超时超标等风险 `/status` 返回结构化 `issues` 数组，`vmr status` 同步渲染 WARNING。
11. **`vmr report` §2 成本表口径提示脚注**：明确估算成本含未嗅探 usage 的降级估算部分，消除「估算成本 ÷ Token 反推单价偏高」的误导。
12. **`respnorm` 查询方法并发安全与 `copyFlush` 生命周期同步**：`NormalizerStream` 所有导出查询方法统一互斥锁同步，消除客户端断开/超时提前返回时 reader goroutine 尾读的数据竞态。
13. **客户端流式中途断开在审计日志中精确标注 `canceled`**：`router` 标 attempt 为 `canceled`，`server` 标审计 `Outcome` 为 `canceled`，消除误计为成功的统计失真。
14. **图片降采样磁盘缓存容量上限**：TTL 清理基础上加 `defaultCacheCapBytes`（50MB）全局上限，按 mtime 淘汰最旧、与 TTL 开关解耦；清扫沿用「至多每天一次」节流，是最终收敛到 50MB 的上限而非任意时刻的硬顶。
15. **Compare 报告「证据溯源」改为按 Journey 精确定位**（`internal/story/storyindex.go` 的 `SourceFiles`）：从 `vmr-stories.json` 已算好的每个 Journey 的 `Files` 取并集，无关文件从 9 个降到 0。
16. **Compare 报告 LLM 解读标题层级**：两段 LLM 解读的三个子标题从与外层 `##` 平级改为 `###`（改在发给 LLM 的 prompt 文本里）。
17. **决策脊柱多行/超长工具调用参数默认折叠**：`payloadBlock` 折叠，`<summary>` 放拉平截断预览。
18. **决策脊柱 Step 的原始消息不再截断**（`foldWhyLine`）：`RespText`/`Reasoning` 超长改为折叠展示预览、展开为完整原文，永不丢内容。
19. **决策脊柱 system prompt 移至文档头部、按出现顺序折叠一次**（`render_md_sysprompt.go`）。
20. **决策脊柱工具调用结果配对改为三级降级**（精确 ID → 归一化 ID 去下划线 → 同 Step 内按位置）：OpenClaw 家族回写工具历史时去下划线导致精确配对成功率实测 0%；前两级仍可作 Finding 证据，第三级标注「按位置推测」。
21. **决策脊柱覆盖补全至 100%**：每个 Step 都渲染，无工具调用的 Step 降级为一行摘要，脊柱末尾新增「最终交付物」小节。
22. **Compare 报告开篇展示两侧完整初始 User Message**（`InitialInstructionFact`）：2000 字符为界折叠，随 `ComparisonExtras` 自动进入证据包。
23. **LLM 解读小节标题层级渲染层兜底**（`downgradeH2Headings`）：`RenderLLMSection` 对返回文本做确定性降级——围栏外行首 `## ` 一律降 `### `，文档大纲不再依赖模型指令遵从度。
24. **Journey 报告 fact-layer 删除，脊柱挂详单链接，系统提示词改为引用**（P5）：重复的 fact-layer 渲染函数整体删除；每个 Step 携一条「→ detail」链接（`reqdetail.FileNameForManifest`，渲染时按需生成）；`Edit`/`StitchEdge`/`SysChanged`/`Compaction`/`NoReply` 五类跨记录事实搬进脊柱本身。顺带修复：系统提示词版本分组改为直接按 `Manifest.HasSys`/`SysHash` 状态机分组（此前 lineage 内部的 sysprompt 变更检测不到）。真实语料 22 步样例报告从 ~312KB 降到 ~107KB。
25. **`vmr replay -req` 免位置参数**（原 §1.25）：按坐标 basename 在当前目录 / `config.yaml` 的 `log_dir` 下自动定位（含 `.zst`）；从 `vmr-requests.json`/`journey-*.json` 复制的 `req` 字段可直接贴进命令行。
26. **导航矩阵六条边补齐，会话身份改为内容寻址**（P6.1–P6.4，原 §1.26）：`report` 的 `SessionInfo.ID`/`SessionRow.ID` 从 run-scoped `s%02d` 改为 `ctxgraph.Lineage` 的内容寻址身份（`l-<hash8>`），与 `story` 的 `JourneyIndexRow.Lineages` 直接集合可 join，位置序号降级为人读 `alias`；六条导航边补齐、真实语料无死链接；`vmr-stories.md` 按标题标记分类、噪声类默认折叠；`vmr story -llm-addr` 自身产生的分析流量默认从成本统计与候选任务列表排除（`-include-self-traffic` 可关闭）。
27. **默认 `vmr report` 产出的请求索引死链接清零**（P7.1，原 §1.31）：`detailCell(r, detailsOn)` 默认渲染 `r.Req` 坐标为行内代码，`-details` 模式保持 Markdown 链接。
28. **决策脊柱指令展示的方言过滤漏洞补齐**（P7.2，原 §1.21）：裸消息（无信封）上的 `[timestamp]`/`[message_id: ...]` 脚手架前缀此前完全未剥离；新增窄范围正则（仅匹配 OpenClaw 日期前缀形状，不通配任意方括号——避免误伤 `[Bug] fix the crash`）循环剥离。脊柱「💬 指令」行改读 `buildFrom` 构建期已过滤的 `Step.Instruction`，并补齐 `renderSpineStep` 的渲染分支（此前该行只在无工具调用的 Step 上渲染，中途指令几乎不可见）。
29. **`-llm-addr ''` 现在能真正关闭 LLM 调用**（P7.3，原 §1.28）：新增 `resolveStringExplicit`，显式传空串不再回退到 `report.yaml` 默认地址。
30. **JSON 输出的语言策略统一**（P8，原 §1.19）：`journey-<id>.json`/`compare-*.json`/`vmr-report.json` 同一次 `-lang` 下语言一致。`story.Compare` 加 `lang i18n.Lang` 参数、循环体改用 `i18n.MetricLabel`（`metricSpec.Label` 字段删除）；`report` 侧不给 `Build`/`BuildCached` 加参数，改新增 `report.LocalizeEfficiency(rep, lang)` 在写 JSON 前覆写 `rep.Efficiency`，Markdown 渲染路径保留独立计算不依赖调用顺序。`Code`/`EvidenceAnchor` 是稳定机器锚点、不随 `-lang` 变。落地方向见 `docs/future-strategy/json_lang_policy_plan_sonnet-5.md`，回填进 Analytics 设计文档。
31. **`vmr analyze` 大语料 SIGKILL 根治**（P9.2，原 §1.30/§1.32）：默认套件只物化 `category == task` 的候选，`-render-all` 保留全量；`cmd_story.go` 的 `renderJourneys` 改为按 `renderBatchSize`（20）分批调 `story.BuildAll`，每批写盘后可 GC（改动全在 `cmd/vmr`）。真实 34 文件语料默认套件从 SIGKILL（约 35.5GB 峰值）→ 正常退出（峰值 4.59GB）。**闭环的是 SIGKILL（原 §1.30）这一症状**；「默认路径仍写大量派生产物」的纪律问题重新登记为 §1.35（真根因是 `writeJourneyFile` 无条件 `EnsureJourneyDetails`，不区分单条下钻与批量渲染）→ 已由 §3「38」闭环。
32. **`vmr report`/`vmr story` 降级为过渡别名，`vmr analyze` 成为单一分析入口**（P9.1/P9.3，原 §1.33）：`vmr analyze` 收敛为并集 flag 集合；`-journey`/`-compare`/`-corpus` 三个互斥变焦选择器各只跑对应 story 侧视图；不带选择器是默认套件（story 先、report 后，因 `report.Markdown` 挂链接需 `stories/vmr-stories.md` 已存在）。`cmd_report.go`/`cmd_story.go` 保留独立 `flag.NewFlagSet` 与默认值、产出逐字节相同，仅打一行迁移提示；`internal/report`/`internal/story` 零改动。
33. **四处文档「先 report 后 story」执行顺序订正**（P9.4，原 §1.33）：UserGuide/.zh、Analytics 设计文档、CHANGELOG 均改为「story 先、report 后」；README/.zh 补 `vmr analyze` 快速上手示例。
34. **自指流量识别规则的输入不对称随统一 flag 集合消失**（P9.5，原 §1.34）：`cmdAnalyze` 只解析一次 `llmKey`（`-llm-key` 或 `report.yaml`）同时喂两个半区。落地修复的连带缺陷：`resolveLLMOptions` 只在 `-journey`/`-compare` 分支按需调用，单独设 `-llm-key`（不带 `-llm-addr`，仅用于排除自指流量）不再在默认套件/`-corpus` 下报错退出。
35. **P2/P3 遗留死代码清空，文档引用守卫扩展到源码注释**（P11，原 §1.39）：`story_report_full_review_opus-5.md` 列的六个「非缓存版/单条版」函数中五个判定为**错**（缓存正确性差分测试的参照实现 / 两个包几乎每个测试文件的公共 fixture 构造入口 / 被 `_eval/` 目录下的校准工具调用而 `go build` 的可达性分析看不到）；`health.Registry.Available` 同样移出待删名单（唯一无副作用的可用性查询方法，测试断言依赖它）——见 `health.go` 注释与本项。**实际删除的**：一个自我闭环的废弃索引子系统 + 六个真正零引用的小函数。`archtest` 的文档引用守卫扩展为常驻测试 `TestArchitecture_DocReferences_SourceComments`，覆盖 `internal/`+`cmd/` 全部非测试源文件注释。
36. **详单跳过谓词从「假设」改为「校验」**（P12.1，原 §1.41）：`EnsureRendered` 曾经的跳过条件 `os.Stat(target) == nil` 把「文件名可算」误当「同名文件内容正确」——`Render` 输出还依赖文件名不携带的 `lang`/`linkEvidence`。改法：`Render` 首行写一行渲染时不可见的 HTML 注释指纹（`renderFingerprint`，含模板版本 / lang / evidence），`EnsureRendered` 有界读取目标首行比对，不匹配才重渲染原子覆盖；`renderTemplateVersion` 常量给「改输出形状不改文件名」预留第三个失效维度。外部审阅指出首版把 `linkEvidence` 分支放在指纹比对之后会漏掉 evidence 重建——已修（两个 `Ensure*Evidence` 调用挪到指纹比对之前，内容寻址幂等检查让命中仍是一次廉价 stat）。
37. **`internal/story` 的原文注入点补齐转义**（P12.2/P12.3，原 §1.37）：`reqdetail` 的 `escapeHTML`/`escapeCell` 导出为 `EscapeHTML`/`EscapeCell`，`internal/story` 薄包装直接调用（不新增依赖边）。共 12 处原文注入点统一处理（截断/拉平在先、转义在后；`codeFence` 内不转义——CommonMark 不解析围栏内 HTML）。最严重的是 `storyindex.go` 索引表格的标题列：原文一个 `|`（如任务标题引用 `ps aux | grep vmr`）会被 GFM 解析成额外一列，撕裂 `vmr-stories.md`（首要导航入口）整行——用 `escapeCell(escapeHTML(...))` 两函数一起（单 `escapeCell` 挡不住 `<!--`，单 `escapeHTML` 挡不住 `|`）。
38. **证据层体积纪律归位**（P13，原 §1.35/§1.36）：这条纪律第四次被提出（P3.3 → P6.5 → P9.2 → 本次），前三次都因没有守卫退化。
    - `writeJourneyFile`/`renderJourneys`/`renderAllJourneys` 加 `materializeDetails` 入参——单条下钻、`-compare`、`-render-all` 传 `true`，无 selector 的默认套件传 `false`。
    - `renderClientResponse` 的响应体原始 SSE 全文逐字复制改为一行带 `ctxgraph.ReqCoord` 坐标的取用提示（`RawSSERef`）。
    - `renderClientRequest` 的历史消息渲染循环新增折叠分支——`haveDelta && i < deltaStart` 时只输出一行指向 `prev` 详单页的链接（`HistoryFoldedNote`）；`prev == nil`（lineage 首条/缝合边界）或 `deltaStart == 0` 时不折叠，链条有起点。`renderTemplateVersion` 从 1 提到 2 使旧详单过期重写。
    - `internal/report` 的 `detailCell` 判据从「本次是否开了 `-details`」改为 `r.DetailFile` 是否真实存在（`vmr analyze` 的 story/report 两半区各自可能独立物化，纯 flag 判据不可靠）；`buildDetailFileSet` 一次 `os.ReadDir` 建 map 避免两万次以上 `os.Stat`。
    - **P13.6（12-B）**：默认批量套件的脊柱「→ detail」与 sysprompt 证据指针改渲染为行内 `文件:行` 坐标（`SpineDetailCoord` / `SysPromptEraCoord`，复用 `Manifest.Req`），不再输出会 404 的链接。`RenderMarkdown` 加 `linkDetails bool`。
    - 常驻守卫测试（默认套件 `details/` 为 0；`-render-all` 非空；默认套件脊柱/证据指针是坐标非链接）——人为改回「无条件物化」测试当场失败。
    - 真实语料：默认 `vmr analyze` 从 47MB / 253 份详单 → 3.0MB / 0 份；`-render-all` 详单体积降约 86%；`vmr report -details` 与 `vmr analyze -render-all` 对同一批记录的详单逐字节相同。
39. **索引「显示」与套件「渲染」两条噪声判据合一**（P14.1，原 §1.42）：只把 `heartbeat` 归为噪声——真实语料实测 107 条 heartbeat 无一达到 10 请求，而 cron（112 条里 44 条 ≥10）与 subagent（20 条里 16 条 ≥10，最长一条全语料最长）都不该折叠。`i18n/story_index.go` 的 `NoiseFoldSummary` 文案同步订正为只提 heartbeat。
40. **检测器/指标覆盖率披露**（P14.2，原 §1.43）：`chatmsg.ToolResult.IsError` 只在 anthropic-messages 协议下有意义（实测语料 openai-completions 99.48% / anthropic-messages 0.52%），依赖它的信号在 99.48% 语料上结构性沉默，但产物里此前没有地方区分「沉默」与「干净」。`anthropicOnlyCoverage` 现列出全部受影响的 Finding/Metric（用类型化 code）+ corpus-only/journey-only 两组自由文本栏目。披露从 `-corpus`（低频变焦，默认套件从不调用）扩展到单条 journey 报告的「疑似问题」章节（该 journey 全部 Step 都非 anthropic-messages 时）。1% 断崖阈值本身是缺陷（1.2% Anthropic 的语料会整体噤声，剩余 98.8% 依然测不出来）——改为「非 100% Anthropic 即披露」。
41. **CLI 入口完全收敛**（P15，原 §1.38）：`vmr analyze` 补齐 `-macro-only`（等价 `vmr report`）、`-list-only`（等价裸 `vmr story`）、`-story-only`（可与 `-render-all` 组合 = `vmr story -render-all` 的公开等价写法）；`cmdReport`/`cmdStory` 删除各自分派 `switch` 改为构造 `analyzeRun` 转发 `dispatchAnalyze`，`cmdReport` 新增 `-llm-key`。真实语料：`vmr report`/`vmr story` 五种调用形态与对应 `vmr analyze` 写法产物逐字节相同（除时间戳）。
42. **工具调用 ID 归一化下沉至 `chatmsg`**（原 §1.40）：`chatmsg.NormalizeToolCallID` 导出，`internal/story` 统一复用，消除去下划线归一化逻辑的真源外移。
43. **Quota 状态文件孤儿 `limitKey` 自动修剪**（原 §1.45）：`Registry.Prune` 配合 `Snapshot.ProviderLimits` 与 `rt.Install` 在装载/热重载时按配置白名单修剪废弃 Key，设 dirty 并在下次 Flush 持久化。
44. **Server 审计日志路径单一真源收敛**（原 §1.47）：`audit.ActiveLogPath` 导出，`internal/server/admin.go` 的 `auditBlock` 统一复用。
45. **错误分类词表补齐三类误判**（新增 `ErrQuirk` 类 + `authHint` + 词条）：vendor 专属协议约束拒绝（DeepSeek 思考模式 reasoning_content 回传、Google thought_signature）归 `ErrQuirk`（切换 + 零冷却，不复用 `ErrContextLimit` 为保审计标签诚实）；OAuth 标准错误码归 `ErrAuth`；bai 的 "Input token exceed the limit" 归 `ErrContextLimit`。此前三者均落入兜底 `ErrClient`（永不 failover）而中断重试。全量 quirk 模块方向延后（§1.48）。
46. **2026-08 B1–B9 修复**：
    - **B1 · buffered 模式截断的客户端可见数据丢失**（`internal/respnorm`、`internal/router`）：`Read` 非 EOF 错误分支只置 `srcErr`、`s.buf`/`s.pending` 从不 flush——客户端已收 `200 OK` + headers，于是看到格式良好的空 200。修法：`flushRawOnError()` 在错误分支先把**可安全交付**的已收字节 flush 进 `s.out` 再置错——非 SSE 响应 flush `s.buf`（部分 JSON，直连也是这个结果）；SSE 只在 `modePassthrough` 时 flush 尾部，`modeUndecided`/`modeBuffered` 一律不 flush（避免把未闭合 `<think>` 泄漏给客户端，审计记 `truncated_withheld`）。`forwardSuccess` 在 `status == "TRUNCATED"` 时于全部记账之后 `panic(http.ErrAbortHandler)`，客户端 SDK 看到断掉的传输而非静默空成功。回归测试 `TestRespStream_BufferedTruncationFlushesReceivedBytes` / `TestRespStream_ThinkBufferedTruncationWithholds` / `TestBufferedTruncationAbortsAndFlushes`。
    - **B2 · quota 缺省 `since` 每次加载/热重载清零计数**（`internal/quota/period.go`、`internal/config/quota.go`）：`DefaultSince` 曾直接返回 `now`；`LimitKey` 不含 `since` 故桶 key 稳定，但 `PeriodStart` 每次加载重算到加载时刻、`resetIfStaleLocked` 就地清零。修法：`DefaultSince(now, unit)` 把缺省锚点对齐到固定日历边界——min/h/d→当日午夜、w→周一 0 点、mo→月初。午夜锚点使周期栅格锁死到「日」：同一自然日内任意热重载对任意 `every` N 都 `PeriodStart` 恒等、计数存活。残余收窄为 `every: Nh` 且 N∤24（如 `5h`）或 `every: Nmin` 且 N∤1440，**且**热重载/重启跨过自然日——至多一次相移重置。显式写 `since` 可钉死。回归测试 `TestDefaultSince` / `TestDefaultSince_SurvivesReload` / `TestRegistry_DefaultSinceReloadDoesNotReset`。
    - **B3 · LLM 推断 Finding 的 `EvidenceAnchor` 无运行期校验**：见 §2.5 的 LLM 解读层准入契约。
    - **B4 · `report` Markdown 渲染器不转义用户来源标题**（`render_doc.go`、`section_sessions.go`、`requests.go`、`metrics.go`）：`mdTable.row` 集中走 `reqdetail.EscapeCell`；会话/任务标题引用块、context-growth Finding 标题额外走 `reqdetail.EscapeHTML`。回归测试 `TestMarkdownEscapesUserDerivedTitles`。至此两个分析命令的全部表格/引用块标题注入点统一处理完毕（承 §3「37」）。
    - **B5 · 热重载 `reload()` 闭包未串行化**（`cmd/vmr/cmd_start.go`）：fsnotify 与 SIGHUP 两条重载路径可并发调 `rt.Install`，`installLimiter` 非原子 load→check→store 可短暂翻倍有效并发。修法：一个 `sync.Mutex` 包 `reload` 闭包体。
    - **B6 · `MetricCost` charge 把 token 计的 `estimated` 传进 `Charge`**（`internal/router/quota.go`）：`bucket.Estimated` 是 requests/tokens 账户专用，cost 账户估算信号是金额（经 `AddEstimatedCost` 单独记）。修法：cost 分支给 `Charge` 传 `0`。
    - **B7 · `Install` 先 `Quota.Prune` 再 `snap.Swap`**（`internal/router/snapshot.go`）：中间持旧 snapshot 的 in-flight 请求可 `Charge` 进刚 prune 的桶。修法：调换顺序——先 Swap，再 Prune，straggler 由下次热重载的 Prune 自愈。
    - **B8 · quota 读路径重置桶但不置 `dirty`**（`internal/quota/quota.go`）：`Used()`/`EstimatedCostFor()` 经 `resetIfStaleLocked` 变更内存桶却不 set `r.dirty`，只被读路径观测到的周期滚动不会被持久化。修法：`resetIfStaleLocked` 返回是否重置，两个读方法据此置 dirty。
    - **B9 · 每个 stitch 边界无条件开新 Task**（`internal/story/journey.go`）：与 `taskseg.IsNewTask`（新 trace id 或真实新指令）矛盾——一次任务中途为回收上下文的压缩会被渲染成全新 Task，虚增 `len(j.Tasks)`、`plan_exec_ratio` 分母。修法：stitch 边界只在 `newInstructionTitleAtStitch` 找到真实新指令时才开新 Task，否则沿用 `curTask`（该 Step 仍带 `StitchEdge`/`Compaction`，渲染器照常渲染 "🧵 Stitched" + 压缩摘要）。
47. **2026-08 DX / 内存 / E1 / E2 落地**：
    - **DX 3×P0**：新增 `config.minimal.yaml`（+ `.zh`），README Quick Start 改用它并新增 `vmr diagnose` 的 Verify 步骤；`config.Parse` 记录展开为空的 `${VAR}`（`Config.EmptyEnvRefs`），缺 `api_key` 或空 `${VAR}` 在 start/reload 打带框 `CONFIG PROBLEMS` banner；某虚拟模型全部端点无 key 时 `router.Serve` 直接回 `vmr_no_api_key` 503。
    - **analyze 内存**：`ctxgraph/stitch.go` blob 倒排索引 `map[Hash]map[int]bool` → `map[Hash][]int`（见 §1.2）。
    - **include_usage 可见性**：`config.Check()` 新增 `checkQuotaUsageVisibility`——token/cost 额度账户挂在 `openai-completions` 端点上时打 `SeverityWarning`（流式响应无 usage 块除非客户端发 `stream_options.include_usage:true`，vmr 不注入）；`vmr status` 与 `vmr report` 的额度段在 `estimated_pct ≥ 95%` 且 metric∈{tokens,cost} 时追加同因提示。
    - **E2 · 软屏蔽 → failover**（新增 `internal/router/softblock.go`、`internal/adapter/response.go`）：`soft_block_failover *bool`（`models.<name>` 与 `endpoints[]` 两级，endpoint 覆盖模型级，缺省关）。开启后 `tryOne` 对 eligible 2xx（非 SSE、非压缩）预读到 `softBlockPeekCap=64KB`，命中 `respnorm.ContainsSoftBlockMarker` 且 `adapter.ResponseAssistantText` 判定有效文本 ≤64 rune 且无 tool_call → 按 `ErrContent` 分支 failover（`ReportNeutral`、attempt 记 `content` 类、零冷却）。文本抽取按协议放 `internal/adapter`（不引 `chatmsg`）。
    - **E1 · HTML 单文件 journey / compare 看板 + 脱敏**：`vmr analyze -journey <id> -html` / `-compare a,b -html` 各写一份单文件自包含 `.html`（内联 CSS/JS，零外部请求，theme-aware，0600）——单页看板（判定条 / Task→Step 结构时间轴 / 指标 grid + SVG sparkline / Findings；compare 为两侧头 + 分岔点 + A/B 指标差异 + LLM 解读段）。数据源是 Markdown 渲染器走的同一个 `*story.Journey`，不重解析。`-redact`（需 `-html`）把正文替换为 `‹text: N chars›` 占位、去掉逐步详单链接、Findings 只留代码 + Step 锚、compaction 实体名降级为计数；compare 下另整块去掉 LLM 段。仅 `-journey` 单命中 / `-compare` 时出，默认套件不出 HTML。
    - **E3（per-virtual-model 预算硬闸）本轮 hold**（用户决定）——见 §1.15。
48. **2026-08 评审第一梯队落地**：
    - **NEW-BUG-1 · 软屏蔽 Peek 吞没超时/断流错误**（`internal/router/softblock.go`）：`checkSoftBlock` 预读 2xx body 的 `peek, _ := io.ReadAll(...)` 把 watchdog 关连接或上游中途断流的错误吞掉，截断片段被当完整 200 转发——B1「杜绝静默假成功」在 opt-in 路径复活。修法：捕获 `readErr`，非 nil 时把 body 换成 `readCloser{io.MultiReader(bytes.NewReader(peek), errReader{readErr}), resp.Body}`，让 `forwardSuccess` 的 `copyFlush` 撞上错误走既有 `TRUNCATED` → `panic(http.ErrAbortHandler)` 分支。**刻意不做 failover**（此刻 checkSoftBlock 还没写客户端）：全失败分支会把 200 + 残缺 body 原样写回，反而制造新的假成功。回归测试 `TestServe_SoftBlockPeekTruncationIsNotSilentSuccess`。
    - **NEW-DX-1 · 发布包缺文件**（`.github/workflows/release.yml`）：tarball 补 `config.minimal.yaml`/`.zh` 与 `vmr.sh`。
    - **CLI 帮助卫生**（`cmd/vmr/cmd_analyze.go`、`cmd_report.go`、`main.go`、`cmd_version.go`）：`-render-all` 去 `P14.1's story.IsNoiseCategory` 内部代号；`-c`（report）去 `PricingTable's doc comment`；`-macro-only`/`-list-only`/`-story-only` usage 串去反引号（Go `flag` 把首个反引号对当占位符名）；`vmr -h` 与 `vmr version -h` 统一退出码 0，bare `vmr` 仍 2。
    - **文档事实纠偏**：Core 设计文档「计量」段改为「multi-limit + `models:` 子额度已随 P3 交付，仅 `rolling` 报错」；`internal/core/core.go` 包注释补 admission rule（`CLAUDE.md`/`AGENTS.md` 及 `clientheaders.go`/`httpjson.go` 注释已引用但此前不存在）；Analytics 设计文档 HTML 条目补 `-compare` 看板。
    - **旧协议名迁移提示**（`internal/config/provider.go` 的 `unknownProtocolHint`）：见 §2.2。**刻意不做**：不在 parser 里接受旧名（strict YAML）、不提供转换脚本（config.yaml 带注释/`${ENV}`，round-trip 会毁格式）。
    - **E10（`vmr share <id>` 一键分享命令）本轮不做**——判断题，仅在确会用它对外分享 Journey 时才值得。

---

## 4. ROI 评估总表（针对 §1 的待定问题）

> **只评 §1**。§2 是已论证过的刻意取舍，重新打分等于重新论证；§3 已闭环。
>
> **评分口径**：成本 = 工作量 + 长期复杂度｜风险 = 改错的爆炸半径（是否动契约、碰热路径、要同步几处）｜价值 = 真实痛点 + 长远架构收益｜ROI = 价值 ÷（成本 + 风险），三档不给数字分。
>
> **一条贯穿性判据**：多数条目的 ROI 是**时间相关的**。「今天低 ROI」几乎都不是「不值得做」，而是「触发条件还没到」。最后一列写的就是它什么时候该被重估。

| # | 问题 | 成本 | 风险 | 价值 | ROI | 判据 / 何时重估 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| 1.13 | 额度燃尽看板 | 高 | 低 | 中 | **中** | 产品价值而非技术债，按产品路线排期 |
| 1.52 | 虚拟模型预算硬闸（E3） | 中 | 低 | 高 | **中** | 用户 hold；配速≠硬急停，要做需独立的内存态机制 |
| 1.18 | Phase 1b 六判别器黄金样本校准 | 高 | 低 | 中 | **中** | 成本在人工标注时间不在代码；当前无阻塞性误报信号，等黄金样本标注窗口 |
| 1.1 | `collect()` 那一趟仍未缓存（含原 1.23） | 中 | 中高 | 已证 | **中** | 聚合那一趟缓存后已证 5.2×；这一趟触及会话/任务边界判定正确性，需先补 cold/warm 一致性测试 |
| 1.2 | 全内存聚合的记录量上限（含 warm-path） | 中 | 中 | 未证 | **低→中** | 「目标场景内存完全可控」已被实测推翻；触发：单次分析 > 约 2 万条 或 RSS > 4GB |
| 1.3 | `chatmsg` 离线 `map[string]any` 分配 | 中 | 低 | 未知 | **低** | 前置条件未满足：离线耗时由 I/O 与 zstd 主导，收益未测。先跑 benchmark 拿 profile |
| 1.7 | `Row`/`ClientRow` 补 `CostEstimateEst`（方案 ②） | 中 | 中 | 低 | **低** | 要改三处 + 再动 `vmr-report.json` 形状，而方案 ① 已解决误导本身。除非外部脚本真的需要 |
| 1.6 | §2.5 标记符号已达四个 | 低 | 无 | 主观 | **低** | 没有依据：四个标记都按需渲染，健康报表一个都不出现。真实报表读起来觉得吵了再动 |
| 1.8 | `archtest` 豁免键无法区分重名方法 | 低 | 无 | 低 | **低** | 今天零影响。第一次需要豁免重名方法时再改 |
| 1.9 | 探针请求绕过审计日志 | 中 | 中 | 低 | **低** | 探活消耗极低，混入报表污染 SLO 统计——成本主要在决定呈现口径 |
| 1.10 | 审计 `write` syscall 在全局锁内 | 高 | 中 | 未证 | **低** | 异步队列换来一个尚未被证明存在的瓶颈。高并发压测顶到写锁再说 |
| 1.14 | 滑动时间窗限流模型 | 中高 | 低 | 低 | **低** | 日历对齐近似对目标场景够用。除非实测到密集 429 冲击 |
| 1.17 | `imgprep` 解码闸门的阈值量纲 | 中 | 中低 | 未证 | **低** | 闸门已存在（防炸弹），缺的只是按内存预算的更低阈值。无实测显示造成过问题，且方案自带「用账单换内存」的取舍 |
| 1.51 | `mdlite` 行内代码嵌套粗体 | 极低 | 极低 | 低 | **低** | 纯展示层瑕疵，无 XSS。真观察到 LLM 频繁触发再微调解析状态机 |
| 1.48 | 错误分类词表的长期形态 | 中 | 中 | 低→中 | **低** | 三类误判已被最小修复覆盖（§3「45」）。触发：词表出现互相干扰，或 sticky 重复往返可观测拖慢中毒会话 |
| 1.22 | `chatmsg` 未覆盖 Responses API | 中 | 低 | **零（已量化）** | **不做** | 真实语料 `openai-responses` 0 条。触发：`vmr-requests.json` 出现该 protocol |
| 1.29 | `journey-<id>.json` 无 schema 版本戳 | 极低 | 无 | 低 | **不做（暂）** | YAGNI + 已裁决「JSON 无外部脚本消费」。触发：出现第一个 `_eval/` 之外的程序化消费方 |
| 1.49 | imgprep 像素乘积 32-bit 溢出 | 极低 | 0 | 0（当前） | **不做** | 64-bit（唯一目标平台）不受影响。触发：32-bit 成为目标 |
| 1.50 | 详单文件名 hash8 碰撞 | 低 | 低 | 极低 | **不做** | 需同文件近万条 + 同毫秒 + 同模型 + 同 outcome 才撞成同名，现实可忽略 |

**分档结论**：高 ROI **0 条**。中 ROI 4 条（看触发）。明确不做 4 条（各有量化触发条件或明确前提——比一条无限期「待定」更有价值）。低 ROI 其余（其中 `1.2`/`1.3`/`1.7`/`1.10`/`1.17` 的共同点是**收益未经测量**——不是「不值得做」，是「还不知道值不值得」，而先做优化再测量正是这个项目一贯拒绝的顺序）。
