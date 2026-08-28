<!-- Ver 2026-08-28 03:10, by Sonnet 5 -->

# VMR 综合架构与产品评审（2026-08）

> 评审对象：commit `ce27638`（main，工作树干净）
> 评审方式：6 人评审团并行深挖 + 主审交叉核实 + 独立跑测量
> 语言：中文正文，技术术语 / 标识符 / 指标保留英文
> 文档性质：一次性评审报告（历史记录，不受"docs 只留当前状态"规则约束）。**建议这是最后一份此类报告**，理由见 §6.4。
>
> **执行状态（2026-08-28 更新）**：
> - 第一轮：**B1–B9 全部已修复**（含外部反馈两轮收窄），详见 §9。
> - 第二轮：**§5.2 DX 3×P0、§5.4 2×P1、E1、E2 已落地**，详见 §10。**E3 本轮 hold**（用户决定）。
> - **仍未做**：B10–B14、§5.3 S1–S12、§6 E4–E9、§7 分发动作、§5.5 文档专项的剩余项。
> 文中带 ✅ / ⬜ / ⛔ 标记的条目状态以标记为准。

---

## 0. 任务 Debrief 与执行规划

### 0.1 用户诉求（去噪后）

从**专业架构师 + 产品经理**双视角，调动团队对 VMR 做一次**完整、基于事实、允许挑战既有定论**的评审，产出一份中文文档，包含：

1. 项目基本情况画像：是什么 / 提供什么能力 / 设计 / 实现 / 取舍原则。
2. **重点**——下一步发展方向：
   - 现有功能的深化 / 优化空间（更简单易用高效，但**不为此增加巨大复杂度**），每项给 ROI / 风险 / effort / 价值 / 优先级 / "值不值得改"。
   - 新增扩展方向，同样的评估维度。
3. 整体摸底：有没有 bug、有没有问题、实现能力上能不能优化、还能怎么扩展。
4. **第一性原理**：不被设计文档 / KNOWN_ISSUES / 注释里的"定论"束缚，有理有据可以挑战，所有依据必须基于事实（源码、参考、对标产品）。

### 0.2 本次评审的方法

| 步骤 | 做法 |
|---|---|
| 独立侦察 | 主审先读 CLAUDE.md、4 份 v4 设计文档、KNOWN_ISSUES、README、config、git log（318 commits）、GitHub API 实测、跑 build/vet/test，形成独立基线，**不预设任何文档的结论为真** |
| 团队分工 | 6 个子评审各领一个切面，每个都被明确要求"从第一性原理出发、每条论断给 `file:line` 或可引用外部事实、把看起来像过度设计 / YAGNI 违反的东西标出来即使某文档已'裁决'" |
| 交叉核实 | 主审对每个团队报出的关键缺陷回到源码逐条复核（B1 buffered 截断、B2 quota 重置、B3 LLM anchor、CLAUDE.md core 断言均已亲自验证） |
| 实测 | `go build` / `go vet` / `gofmt` / `go test ./...` / `-race` 全绿；`vmr analyze` 全量语料实跑（15,824 条 / 43 文件）测出 862s / 3.75GB RSS / 171MB 输出 |

评审团切面：① 路由核心审计 ② 分析半区审计 ③ 额度 + 定价审计 ④ 架构 + 元工作审计 ⑤ 产品 + 竞品 + GTM ⑥ DX + 首次运行。

### 0.3 后续执行的粗略规划

本报告给出结论与优先级，**不代替用户做"vmr 到底为什么而做"这个决定**。落地按下面的顺序：

```
第 0 步（用户决策，本报告无法代做）：
       明确 vmr 的目标 —— 是"争取采纳的赌注"，还是"自用工具 + 工程作品集"？
       本报告的建议是：把它当成一个 8~10 周的限时实验（方案 A），
       并预先约定失败后转入方案 B（§6.2）。

若走方案 A（限时采纳实验）：
  Week 1-2   修 3 个 P0 bug（B1/B2/B3）+ 3 个 DX P0（minimal config / 可读的缺 key 错误 / diagnose 提到台前）
             同时：冻结 analytics 半区、quota 高级面、dashboard、协议改名类工作（S1/S9）
  Week 3-5   HTML + 脱敏渲染（E1，唯一推荐的新增扩展）
             + 二选一：per-model 预算硬闸（E3，快而安全）或 软屏蔽→failover（E2，差异化更强）
  Week 5-6   改名 + README / description 重定位（§6.3）+ 写 2 个具体工件
  Week 6-10  发 linux.do / V2EX / 掘金 / Show HN，观察外部反馈
  Week 10    止损检查：仍 0 外部 issue / <20 star → 转方案 B

若走方案 B（自用 + 作品集）：
  一次性   README 顶部明说"个人工具，无 roadmap，不做 SaaS / enterprise"
           修 3 个 P0 bug + 文档修正（§7 表末）
  之后     只接受安全 / 正确性修复，停止一切特性与打磨
           设计文档冻结在当前状态（它们已经"写完了"）
```

---

## 1. 项目基本情况画像

### 1.1 一句话

**VMR 是一个本地运行、单二进制的 LLM 路由器 + AI Agent 飞行记录仪**：把任意 OpenAI / Anthropic 兼容客户端的 `base_url` 指过来（零代码改动），它用一个稳定的虚拟模型名（`coding` / `claude` / `agent`）隐藏掉背后的供应商 / 账号 / Key / 优先级 / failover；同时把每一次请求**逐字节两层**记进审计日志，一个离线套件（`vmr analyze`）再把日志重建成聚合报表和 agent 任务叙事。

### 1.2 它解决什么问题

设计文档给的目标用户是"100 人以内的 AI 研发团队 / AI 创业项目 / 重度 Agent 架构师"，痛点是：

1. **多 Token-Plan 采购的悖论**——买了智谱 / Kimi / MiniMax 等多家按月打包套餐 + 阿里 / 字节 / DeepSeek 等按量 API，容易一部分套餐跑爆溢出、一部分吃不满浪费。
2. **Prefix-Cache 跨账号隔离**——常规无状态轮询网关会让 agent 多轮对话频繁落到不同账号，缓存命中率归零，成本和延迟暴涨。
3. **无人值守 agent 的静默故障**——3 AM 的 failover、被内容策略静默替换的空回复、上下文压缩丢了什么，事后没有工具能查。
4. **传统 agent 观测工具要埋点**——LangSmith / Braintrust 依赖在业务代码里引 SDK / 装饰器 / OTel。

### 1.3 提供的能力（全部已实现、有测试覆盖）

**路由与可靠性**
- 三协议原生入口（`/v1/chat/completions`、`/v1/messages`、`/v1/responses`），**同协议族内路由，永不互译**。
- 按错误类分级的健康状态机：冷却、指数退避、半开单飞后台探针恢复；`Retry-After` 遵循（封顶 1h）；内容策略 / 上下文超限 / vendor 协议约束拒绝 → 换端点但零健康惩罚（`ErrContent` / `ErrContextLimit` / `ErrQuirk`）。
- 条件路由：端点声明 `capabilities` + `max_context_tokens`，硬能力不满足在准入阶段淘汰；上下文估算过高有回退兜底（保守估算永不误杀）。
- Sticky Model 会话亲和：`system prompt + 首条消息` 哈希绑定端点，保上游 prompt cache 不被路由打掉；默认开启。
- 额度感知路由：本地累计上游 response usage，按周期做 headroom 配速，在同一优先级梯队内部对携带额度的端点重排（`strategy.Sort` 之后、sticky 之前）；bucket / gate 双角色、多 limit 窗口、per-model scope、分数 multiplier、多币种 pricing。
- 并发闸（全局、无排队上限）、分级超时、全局 + per-provider 代理、配置热重载。
- 厂商 quirk 修复（每项有内容守卫、存疑即不改）：MiniMax `<think>` 剥离、OpenClaw "Thinking Process:" 草稿剥离、软屏蔽标记检测（仅观测）。

**可观测性**
- 两层审计 JSONL（client↔vmr、vmr↔upstream），每次 failover attempt、每项归一化动作都记录；历史文件自动 zstd 压缩 + 过期清理。
- `vmr analyze`（`vmr report` / `vmr story` 是弃用别名）：宏观聚合报表 + 中观任务叙事（Journey → Task → Step，含上下文压缩边界、9 项行为指标、规则派生 Findings、可选 LLM 解读层、跨 run 对比、语料统计）+ 微观逐请求详单。
- `internal/ctxgraph`：把审计记录建模成内容寻址 manifest（消息哈希向量）序列 + 编辑分类（Append / ReplaceTail / Splice / Contract / Fork）+ lineage 图 + 跨 lineage 缝合——这是 report 会话分组和 story 任务叙事共用的唯一事实来源。

**调试工具**
- `vmr diagnose`（配置校验 → 代理感知的 DNS/TLS → nonce 回显连通性 → 路由预览）、`vmr replay`（三种定位方式从审计记录原样重发）、`vmr check` / `status` / `smoke` / `version`、`vmr.sh` dev + service 模式、`/status.html` / `/log.html` / `/help.html` 三个零 CDN 依赖的嵌入式 web 页。

### 1.4 设计与实现取舍原则（提炼自 4 份设计文档 + 源码）

| 原则 | 具体表现 |
|---|---|
| **字节忠实透传，无中间表示（IR）** | 只解析 `model` / `stream` 两个字段用于路由，其余原样透传。仅 3 个 sanctioned deviation：model 名字节 splice 重写、`respnorm` 的证据驱动 quirk 修复（fail-open）、`imgprep` 图片降采样 |
| **两个半区一个契约** | 路由半区和分析半区在逻辑与运行态完全独立，唯一耦合是 JSONL 审计记录，`archtest` 强制 import 单向边界 |
| **KISS / YAGNI / 零调参** | 健康状态机参数硬编码（不暴露难以科学校准的旋钮）；不引入 DB / Web UI / RBAC / 运行时插件系统 / provider SDK；编译期 blank-import 注册 |
| **严格 YAML** | 未知键拒绝加载（拼错的键静默失效是配置驱动工具最常见的真实事故） |
| **fail-fast 优于静默兜底** | 配置期能查的错不留到运行期；nil 校验只加在跨包公共入口且一律 fail-fast |
| **调度 = 过滤 + 稳定多键排序** | 组合能力来自排序键叠加，新策略不改主流程；`Condition`（淘汰）与 `Dimension`（排序）保持分离 |
| **单二进制自包含** | 4 个直接依赖（fsnotify / klauspost-compress / x-image / yaml.v3），纯 Go 无 cgo；不拆分析半区成独立二进制 |
| **可执行的架构不变式** | `internal/archtest`：import 边界、per-file / per-function 行数预算、文档引用完整性 |

### 1.5 工程质量的客观证据

- `go build` / `go vet` / `gofmt -l` / `go test ./...` / `go test -race ./...` **全绿**。
- 测试代码 ~50k 行 vs 生产 ~44k 行（1.14:1）；`FuzzRewriteModel` / `FuzzRewriteStream` 实际抓到过 nil map panic 和 `[DONE]` 重复 bug；确定性 tie-break、cold/warm 缓存等价性测试、`report`↔`ctxgraph` 一致性测试、EN/ZH 双语 golden。
- 压测（150 req/s）：除图片降采样外每个场景 p95 路由开销 < 10ms。
- KNOWN_ISSUES 108KB，逐条能对源码核实，**§4 ROI 表自己的结论是"高 ROI 条目 0 条"**。

**这是一个对 1~3 人团队而言工程能力和纪律都在第一梯队的代码库。** 下面所有的批评都不改变这个判断——问题不在"做得好不好"，在"做得对不对、够不够、该不该继续这么做"。

### 1.6 项目现状（GitHub API 实测，2026-08-28）

| 项 | 数值 |
|---|---|
| 仓库年龄 / commits | 7.5 周 / 318 |
| Star / Fork / Issue / 外部贡献者 | **3 / 0 / 0 / 0** |
| Release | v0.1~v0.6.1，已带 4 架构预编译二进制，**下载数 0**（历史合计约 50 次） |
| CI / brew tap | 均已有 |
| 2 周流量 | 118 views / 17 uniques，唯一外部引流 `link.zhihu.com`（27 views / 10 uniques） |
| 生产代码分布 | 分析半区 **23.7k 行** / 路由半区 **14.5k 行** / `cmd/vmr` 4.0k 行（+ 6.1k 测试） |
| 磁盘散文 vs 代码 | docs/ 3.1MB + archived/ 4.5MB = **7.6MB** vs 代码 3.76MB |

---

## 2. 发现的问题与 Bug

按严重度排列。B1~B3 建议限时实验窗口内必修；B4~B14 是代码缺陷但可排期；B15+ 是文档 / DX 卫生问题。

> **状态（2026-08-28）**：**B1–B9 已全部修复并有回归测试**（详见 §9 执行情况记录）。**B10–B12 未做**（排期，超出本轮范围）。**B13 确定不做**（32-bit 非目标/CI 平台，见 §9.6 与 KNOWN_ISSUES §1）。**B14 未做**（低优先，已登记进 KNOWN_ISSUES §1）。**B15–B20 未做**（文档/DX，超出本轮范围）。

### 2.1 代码缺陷

| # | 严重度 | 位置 | 描述 | 失败场景 | 修法 | 置信度 |
|---|---|---|---|---|---|---|
| **B1** | **中** | `respnorm/respnorm.go:339-352` + `router/router.go:536-544` | buffered 模式下上游流中途失败（连接重置 / `stream_idle` 看门狗关闭），`stream.Read` 记 `srcErr` 后**从不调 `finish()`/`finalizeBuffered()`**，`s.buf`（已收到的全部字节）被丢弃 | 客户端已收到 `200 OK` + headers，Go 写干净的 chunked 结束符 → 客户端看到**格式良好的空 200**。所有非 SSE 200 响应 + MiniMax thinking 流式响应都走 buffered 模式。中国场景 `https_proxy` 在路径上截断概率更高。审计记 `truncated` 但 outcome=`ok` | `srcErr` 时先把 `s.buf` flush 进 `s.out` 再置错（~5 行，符合本包 fail-open 原则）；配合 `panic(http.ErrAbortHandler)` 让客户端 SDK 看到断掉的传输而非干净空成功 | 高（已复现） |
| **B2** | **高** | `config/config.go:456` + `quota/period.go` `DefaultSince` + `quota/quota.go:277` | 每次 `config.Load`（启动 + 每次 fsnotify/SIGHUP 热重载）都 `quotaNow=time.Now()` fresh；省略 `since:` 的 Limit 拿到 `Since=now`。`LimitKey` 不含 `since` → bucket key 稳定，但下次 `PeriodStart` 重算到 reload 时刻 → `resetIfStaleLocked` **就地清零 bucket** | 热重载或重启后 headroom 配速把已用光的账号当"fresh"、过度路由过去；`/status` 和 `vmr report` 少报消耗；"过期作废"配速失效。实盘 `logs/vmr-quota.json` 里 `google`/`sensenova`/`bai`/`openrouter-*` 全部 `period_start` 非日历对齐 = 全部受影响。**不被现有"撤回"note 覆盖**（那条只讲相位对齐），直接矛盾 Quota 文档 line 934/1034 | `DefaultSince` 日历对齐到单位边界（截断到整点/午夜/周初/月初），`period.go` 已有全部日历原语，~15 行。加回归测试：累加 → 模拟 reload → 断言 bucket 存活 | 高（测试 + 实盘双确认） |
| **B3** | **中** | `story/llm_findings.go`（6 个 `detectLLM*`，判据均为 `EvidenceAnchor != ""`）+ 合入点 `cmd/vmr/cmd_story.go:353` | LLM 推断 Finding 只检查 anchor **非空**。"anchor 必须是真实 transcript 逐字子串"这个机械校验**只存在于 `_eval/calibrate_p1b.go:113`** | 模型幻觉出一个不存在的 anchor → 直接晋升为 `Confidence: HIGH` + `[AI推测]` 徽标的 Finding，**还会被当既定事实喂给下一次单-Journey 解读**，污染二次推理。整个"能指认证据锚点才标 HIGH"的安全架构压在这个前提上，运行期无人强制 | 把 `_eval/` 里那段 `strings.Contains(pool, anchor)` 搬进 `ComputeLLMFindings`，校验不过就丢弃该 Finding（~15 行，`searchablePool` 实现可复用） | 高 |
| **B4** | 中 | `report/render_doc.go:56`、`report/section_sessions.go:100`、`report/requests.go:517` | Markdown 渲染器不转义用户来源标题里的 `|` 和 `<!--`。P12.2/P12.3 在 `story` 侧修过的同一 bug 类，`report` 侧从未加 | 已构造实测复现：指令含 `` `ps aux \| grep vmr` `` → §6 会话表列错位；指令以未闭合 `<!--` 开头（4 字符，`truncateTitle(...,28)` 装得下）→ HTML-aware 渲染器吞掉整份 `vmr-requests-<tag>.md` | `report` 已 import `reqdetail`，直接用 `reqdetail.EscapeHTML`/`EscapeCell`；最干净是让 `mdTable.row` 集中转义 | 高（已复现） |
| **B5** | 低 | `cmd/vmr/cmd_start.go:164-193` | `reload()` 闭包未串行化，fsnotify 的 `time.AfterFunc` goroutine 与 SIGHUP goroutine 可并发调 `rt.Install` | `installLimiter`（`limiter.go:20-33`）是非原子 load→check→store，两次 Install 可各 `Store` 一个新信号量，短暂翻倍有效并发。无数据竞态，自愈，但是可避免的"为什么并发数突然是 16" | 一个 `sync.Mutex` 包 `reload` 闭包体 | 高 |
| **B6** | 低 | `router/quota.go:122` | `MetricCost` 分支把 token 计的 `estimated` 传进 `Charge` 的 `estimated` 参数，累进 `bucket.Estimated`（其 doc 说"requests/tokens 账户 only"） | 今日无害（没有代码为 cost 度量读 `bucket.Estimated`），但若未来 `/status` 暴露原始 `Estimated` 就是错数据陷阱 | cost 分支传 `0` | 高（dead-but-wrong） |
| **B7** | 低 | `router/snapshot.go:284-287` | `Install` 先 `Quota.Prune` 再 `snap.Swap`。两者之间持旧 snapshot 的 in-flight 请求可 `Charge` 进新 config 会保留但被重新 key 的 bucket，或重建刚 prune 的 bucket | 窄、自愈（下次 reload 的 Prune 修复），低影响 | 调换顺序或在 mutex 内 | 中 |
| **B8** | 低 | `quota/quota.go:307` `Used()` | 读路径经 `resetIfStaleLocked` 变更内存 bucket 但**从不 set `r.dirty`** | 只被读路径观测到的周期滚动不会被 flusher 持久化；`vmr-quota.json` 可对低流量账号无限期落后现实。report 侧有 `PeriodStartTime().Equal()` 守卫所以无错数到达决策 | 读路径变更也 set dirty，或明确文档化为"working-as-intended" | 高（机制确定） |
| **B9** | 低 | `story/journey.go:409` | `newTask := (ci==0 && i==0) \|\| atStitchBoundary`——每个 stitch 边界**无条件**开新 Task，与 `taskseg.IsNewTask`（新 trace id 或真实新指令）规则矛盾 | 一次**任务中途**为回收上下文的压缩会被渲染成全新 Task。实盘 `journey-j-hermes-...-bfdad4c8.md` 头显示"38 任务"，其中相当一部分是压缩诱发、不对应任何真实指令。虚增 `len(j.Tasks)`、`plan_exec_ratio` 分母、per-Task 检测器、`-corpus` 分组 | stitch 边界也走 `taskseg.IsNewTask` 判据；找不到新指令就沿用 `curTask` + 渲染一个"⟳ 上下文在此重建"视觉标记 | 中 |
| **B10** | 低 | 默认 `vmr analyze` 套件的 446 份 `journey-*.md` | 全部含指向未物化的 `../evidence/sysprompt-*.md` 和 `../details/*.md` 的链接（懒物化默认不建这两个目录） | 实测 171MB 输出目录里两个目录不存在，抽查链接全部 `MISSING`。**KNOWN_ISSUES #38 明文声称"不产生死链接"——在默认套件下是错的** | 默认套件也物化 sysprompt evidence（体积小、内容寻址去重），或不渲染死链接文本改为"运行 `-journey <id>` 查看详情"提示；并修正 KNOWN_ISSUES #38 和设计文档 §2.5 | 高（实测） |
| **B11** | 低-中 | `chatmsg/entities.go`（commit `ec57bae`，2026-08-16）+ 文档 §2.4/§3.3/§3.5a + `story/metrics.go:68` | `ExtractEntities` 拓宽到也抽 CamelCase / snake_case 标识符 / CLI 命令，但设计文档三处仍写"粗糙的文件路径/URL 正则扫描"，§3.5a 假阳性率校准（2026-08-05）**早于拓宽 11 天且未重跑** | 代码密集 transcript 里每个标识符成"实体"→ 抬高 `ContextUtilization`、`unused_tool_result`、`constraint_text_dropped`、compaction "吞掉的实体"列表的噪声 | 实体抽取按检测器分层（Findings 用窄集），或在拓宽后语料重跑 §3.5a 校准 + 更新文档 4 处 | 中 |
| **B12** | 低 | `chatmsg/usage.go:155-162` | `usageFromObj` 判别 Responses vs Anthropic 用 `Nested(m,"input_tokens_details","cached_tokens")!=nil`，一个带 `input_tokens_details` 但缺 `cached_tokens` 的 Responses usage 会落进 anthropic 分支 | 此时 cacheRead/cacheWrite 都是 0，`In` 结果碰巧仍对（Responses 占语料 0%），属"脆弱"非"已错" | 改用更稳判据（顶层 `output_tokens_details` 或协议标记） | 中 |
| **B13** | 低（仅 32 位，非活跃） | `imgprep/imgprep.go:499` | `cfg.Width*cfg.Height > maxDecodePixels` 在 32 位 `int` 平台可被 ~50k×50k 声明尺寸整数溢出，绕过解压炸弹守卫 | 64 位（唯一 CI/目标平台）PNG/BMP 的 int32 尺寸不可能溢出乘积，非活跃缺陷 | 乘积用 `int64` | 高（算术） |
| **B14** | 低（潜在） | `ctxgraph/reqcoord.go:69` | 详单文件名去重位是 `md5(basename:line)[:4]`（32 bit） | 单文件近 1 万条记录时 hash8 碰撞按生日界约 1%；真正文件名碰撞还需同毫秒 + 同模型 + 同 outcome，现实可忽略。随 KNOWN_ISSUES 1.2 的"3 万条"语料线性恶化 | 登记即可，不值得现在动 | 数学高 / 实际低 |

### 2.2 文档 / DX 卫生问题

| # | 位置 | 描述 |
|---|---|---|
| B15 | `CLAUDE.md` module map `core` 行 | 声称"Its package doc states the admission rule — read it before adding anything here"。**已亲自核实**：`core.go` 包注释只有一句"shared domain entities ... zero-dependency domain helpers"，`grep -i admission` 零命中。这正是 CLAUDE.md 自己 Conventions 节警告的"stale detail reads as authoritative"；`doc_refs` 守卫结构上抓不到（只验符号存在，不验断言为真） |
| B16 | `config.local.yaml` | 已 bit-rot：用旧的单数 `provider:` 键，当前二进制加载失败（`line 58: field provider not found in type config.EndpointGroup`）。连作者私有配置都跟丢了 schema——侧证"示例配置与 schema 保持同步"本身有真实维护成本 |
| B17 | flag 帮助 / 日志 / 报告 | ① `-list-only vmr story` 类 flag 渲染 bug（usage 字符串反引号被 flag 包当值占位符，实为 bool flag）② 日志和 `vmr-report.md` 泄漏 §-编号（`§5.5: 2 client(s)...`、`## §0 Summary`）——**违反项目自己 CLAUDE.md 的"No section numbers in cross-references"** ③ `vmr -h` 退出 2（应 0），`vmr version -h` 退出 1（其他子命令 `-h` 退 0）④ flag 帮助泄漏内部术语（`P14.1's story.IsNoiseCategory`、`PricingTable's doc comment`） |
| B18 | `README.md` + `release.yml` | `vmr.sh` 不在 release tarball，但 README Quick Start 把 `./vmr.sh start` / `./vmr.sh service install` 当主要运行方式。brew / 二进制用户没有任何"作为服务运行"的路径 |
| B19 | `README.md` | 好奇用户 `curl http://127.0.0.1:8800/log` 会永久挂起（无尽 `text/plain` 流），README 只在注释里提 `/log.html` |
| B20 | `README.md` "See It in Action" | `vmr report ... examples/sample-audit.jsonl` 不加 `-details` 不产出所展示的 `Attempt 1/2` 片段——首次信任受损 |

### 2.3 安全 / 可用性结论

**无安全或可用性回归。** 无凭证泄漏（审计凭证掩码留末 4 位）、无 goroutine / body 泄漏（probe goroutine 单飞有界自释放，`copyFlush` reader 恒经 deferred `resp.Body.Close()` 排空）、请求路径无无界内存、字节忠实守到恰好 3 个 sanctioned deviation。B1 是唯一客户端可见的数据丢失，且是错误路径 only + 审计有记录。

---

## 3. 对既有"定论"的第一性原理挑战

用户明确要求：不被文档 / KNOWN_ISSUES / 注释里的裁决束缚。下面每条都基于事实。

### 3.1 【挑战】KNOWN_ISSUES §0："无数据丢失 / 凭证泄漏 / 并发竞态 / 服务阻断级别的缺陷"——"无数据丢失"夸大

B1（buffered 模式截断 → 客户端收到干净空 200）就是客户端可见的数据丢失。是错误路径 only、审计有记录，但"无数据丢失"太绝对。**应移入 §1，给出具体触发条件。**

### 3.2 【挑战】CLAUDE.md 第一句："two co-equal halves ... neither is an afterthought to the other"——数据已推翻

生产代码：analytics **23.7k 行** vs routing **14.5k 行** = **1.64×**（加 CLI 接近 1.7:1）。按周 file-touch，W31 起 analytics 反超 routing 再没让回去。`i18n` 包 3.4k 行**全部**服务 analytics（routing 的 log / error 是纯英文，零字进 i18n），这一个包就比整个 `router` 包大。

"co-equal" 是一个被数据推翻、但仍写在项目宪法第一句的定位。它现在的实际作用是**心理许可**——它让"再给 `story` 加一个 corpus 维度"感觉像在履行项目宪法，而不是往一个没人用的子系统里加复杂度。

**建议改写为诚实版本**：*"vmr 是一个字节透传路由器，配一个离线取证套件；取证套件目前是代码量的大头，但路由正确性永远优先。"*

### 3.3 【挑战】KNOWN_ISSUES §2.5 / §14："行数只是提醒式绊线，真正的可维护性是整体架构与设计复杂度"——架构那半对，但结论被误用

架构那一半**是对的**——33 个包边界干净、单向、`archtest` 真正锁住，"两半区只靠 JSONL 耦合"经 `go list -deps` 验证真实成立。但这句话默认了一个前提：文档和测试是"可维护性资产"。事实：

- 磁盘散文 7.6MB 对代码 3.76MB；全历史 markdown 新增 **83,192 行** > 生产 Go 新增 **67,571 行**。
- `doc_refs` 守卫把 900KB+ 设计文档 + 108KB KNOWN_ISSUES 里每个 `pkg.Symbol` 反引用变成 CI 红线——每次重命名符号要改 6+ 份大文档，**至少 9 个 commit 是纯"fix stale doc references"**。
- 每加一个 config 字段要同步 `config.example.yaml`(32KB) + `.zh`(32KB) + Core 设计文档 §10 + `UserGuide.md`(120KB) + `.zh`(112KB)——**至少 5 处，一半要翻译**。
- 133 个 `.md` 文件里 **98 个**匹配 review/plan/analysis/audit 模式；同一份 "comprehensive architecture review" 写了 **4 个版本**（gemini v1-v4 合计 ~4,900 行），另加 sonnet / opus / Fable / 3.6 版；**170 个 `.md` 在项目生命周期里被创建后又删除**。
- KNOWN_ISSUES §4 ROI 表**自己算出的待办价值是零**（"高 ROI：0 条，全部 15 条都是等触发条件"），但维护这份 108KB 文档的成本不是零。

**修正后的立场**：line budget 不是问题，`archtest` 值得留，`doc_refs` 单看也合理。问题是这套纪律**保护的产品面**（analytics + quota 全旋钮 + 3 个 web dashboard + 4 个诊断入口 + 双语一切）对 0 外部用户来说大了约 5 倍。**正确的动作不是放松 `archtest`，是缩小需要被 `archtest` 保护的表面积。**

### 3.4 【挑战】战略文档："分析半区是核心壁垒（agent flight recorder）"——部分成立，但壁垒的实体不是整个 23k 行

分析半区可分三层：

| 层 | 代码占比 | 判断 |
|---|---|---|
| **事实层**：`ctxgraph`（内容寻址 manifest + 编辑分类 + lineage + stitch）+ journey 重建 + 决策脊柱 + 逐请求详单 | ~40% | **真差异化、真扎实**。git 模型类比不是营销话术（95.86% append / 1% break 的实测分布正当化了"切分便宜、缝合贵"的架构判断），确定性 tie-break / cold-warm 等价 / `report`↔`ctxgraph` 一致性测试都到位。保留并投资 |
| **剖面层**：9 项指标 + 规则 Findings | ~30% | 有价值但校准脆弱；实体抽取 2026-08-16 拓宽后从未重新校准（B11） |
| **解读层**：LLM 6 检测器 + compare/divergence/single 三套 prompt（~1.5k 行 + 41KB 双语 prompt i18n）+ `-corpus` 语料统计（~900 行） | ~30% | **投机泛化**。0 用户在跑，`corpusMinCorrelationN=5`（文件自己的注释："below N, a correlation coefficient is noise dressed up as a number"），设计文档 §3.9 举出的唯一真实产出是"命中组净工作时长中位数高 34%/38%"——900 行换一条逸事。且解读层有一个没兑现的安全承诺（B3）。学术基线（Who&When / TRAIL）step 级根因定位准确率仅 11–14% |

把"壁垒"整体等同于"23k 行分析代码"会误导投入决策——**真正要守和投资的是那 40%**。

### 3.5 【挑战】战略文档 §4："为什么是配速而不是负载均衡"——方向对，但夸大了已交付的价值

- 具体的赢面（一个 4 天后到期、priority 不是第 1 的套餐，把没用完的额度从作废里救出来）**是真的，但有界**：约一个套餐 10~30% 的价值 / 每个坏周期，且只在总需求处于 1× 到 N× 单套餐容量之间、且重置日错开时成立。一个 ~30 行的"优先选周期最快到期的未耗尽账号"启发式能拿到大部分价值。
- **最常见的 vmr 场景——多个 Claude Code Pro/Max 订阅——正是这个算法明确不服务的 rolling-window gate 场景**（Strategy §4 line 78、Quota §2.1 自己承认）。交付的价值针对更窄的"多个月度/日度 token-bucket 套餐"（国产 coding plan），这是真实的但不是"任何有多个 LLM 订阅的人"。
- 无状态贪心（文档："无状态的贪心"当纯粹优点讲）意味着一批并发新会话全看到同一个 stale `Used()` 读、全堆到当前最优账号（scoring window 内的 thundering herd）。solo-dev QPS 下可忽略，但文档没提。

### 3.6 【挑战】Core 设计文档 §6.6 "计量" 段落——STALE，与已发布代码矛盾（违反"docs are current state"）

- 仍写"多条 Limit 并存、`rolling` 滚动窗口、按模型的 `models:` 子额度**仍未交付（P3），本批配置里写了会在加载期直接报错**"——multi-limit 和 `models:` scope **2026-08-22 已交付**，写了**不报错**。只有 `rolling` 还报错。
- 仍描述"**账号级** `model_multipliers` / `token_weights`"——P3 已移到 per-Limit，账号级现在是**显式 rejection trap**（`config/quota.go:259`）。

### 3.7 【挑战】Quota 设计文档 line 934 / 1034："热重载改它们立刻生效、且不重置计数" / "计数跨重载存活"——对 default-`since` 的常见情况是 FALSE

见 B2。这只对有显式 `since:` 的 Limit 成立。Quota 文档 line 1133 的"决定不修"条目（"未配 `since` 时的热重载锚点漂移问题｜非问题（撤回）"）**只 reason 了相位对齐，从未提 `resetIfStaleLocked` 清零后果**——那是一个真 bug，不是相位选择。这条应重开。

### 3.8 【挑战】战略文档 §7："可继承性已经做得比较扎实，不需要额外投入"——隐含前提是有人会继承

"可继承性"的隐含前提是有人会来继承。3 star / 0 fork / 0 贡献者、7.5 周 318 commit。当前证据下，167KB 中文设计文档 + 逐条实测的 KNOWN_ISSUES **更像是给作者自己看的、防止自己忘记为什么这么设计的备忘**——这本身有价值，但它的规模应该按"1 个人需要多少上下文才能 3 个月后重新进入状态"来定，不是按"一个陌生团队接手需要多少"。后者的标准正在生产远超必要的文档。

### 3.9 【挑战】"byte-faithful 天然免疫上游 API 变更"被当结构性优势反复讲——对，但掩盖了注意力分配是反的

byte-faithful 让**路由核心**很小很稳（14.5k 行、p95 < 10ms），恰恰因为路由这半已经"做完了"。项目继续投入的地方全在 analytics——而 **analytics 不享受 byte-faithful 的免疫力**，它要跟着每家厂商的 SSE 格式、usage 字段、tool-call 形状变（KNOWN_ISSUES 1.22 已承认 `chatmsg` 没覆盖 Responses API）。**真正的协议演进技术债风险在 analytics 的解析层，不在路由层**，但文档和"常设义务"的注意力分配是反的。

### 3.10 【不挑战，复核后确认成立的既有裁决】

以下逐条复核后确认站得住，**不推翻**：

- **KNOWN_ISSUES §2.2** "`metric: cost` 费率在 `config` 层加载期解析"——`internal/pricing` 是干净 leaf，解析全离线，买到的不变式（"misconfig = 加载期错误、`vmr check` 不联网可查"）真实有价值，后置会制造两条解析路径。
- **KNOWN_ISSUES §2.2** "多协议 adapter 保持独立子包"——3 个子包 66-89 行/个，有真实分叉（Anthropic 529、`RewriteInputRoles`、`x-api-key` vs `Authorization`），合并成参数化结构体只是把多态改成字符串 `if` 分支。
- **`cmd/vmr/quota_parity_test.go`** 不变式——router 侧真的调自己的 exported entry points（`router.TokenCounters` / `router.ChargeResponse`），没有 restate 公式（唯一手抄的是 charge *资格*谓词 `forwarded()`，是可辩护的 scope 线）。
- **KNOWN_ISSUES §2.4** "6 种聚合桶类型不泛型化"、"i18n 26 微文件不合并"——泛型重写换边际行数、改 JSON 契约、丢类型安全；微文件与 `section_*.go` 一一配对且合并击穿 archtest 预算。**论证成立**（但 §5 会指出 i18n EN 分支本身是投机）。
- **健康状态机、Sticky、`respnorm` 双模式、failover 穷尽候选、错误分类含 body 嗅探**——路由核心的这些决策复核后都有清晰的量化依据或触发条件，不推翻。

---

## 4. 战略层判断（核心）

用户要求"重点看下一步发展方向"，并且"看继续按现在的方式投入是否正确"。这是本报告最重要的一节。

### 4.1 一个月前的判断，现在更强了

`docs/future-strategy/vmr_future_strategy_v2_sonnet-5.md`（2026-07-28）的结论是：**"分发压倒特性；产品侧已经过关；继续投入特性开发的边际价值接近零；当前瓶颈 100% 在分发侧。"**

一个月后（237 个 commit）：

| 那时说 | 现在的事实 |
|---|---|
| "第 82 次提交不会带来第 3 个 star" | 237 个 commit 后：**2 star → 3 star** |
| 建议 D1-D8 分发动作，D1-D7 共 3-4 人天 | 只做了最便宜的：二进制 / CI / brew。**未做**：文章推广、进 agent 工具 setup 文档、`vmr.sh doctor` + `check`/`status` 的 `--json` |
| "D1–D7 做完之前，不应该开工任何新特性" | 之后建成：`vmr story` 整个特性（~8k 行）、quota P1→P3、`/status.html` + `/log.html` + `/help.html`+`.zh`（3,100 行双语 HTML）、协议枚举重命名（附 78KB 评审文档）、favicon/tab 标题打磨。**49 个 commit 里分发相关约 1 个** |

### 4.2 竞品格局这一个月明确恶化（⑤号评审，GitHub API 实测 2026-08-28）

| 信号 | 事实 |
|---|---|
| **Pillar A 被独立复刻并跑赢 160×** | `dario`（askalf/dario，491 star，2026-04 创建）README 逐字用 byte-faithful / wire-fidelity / session-affinity routing / multi-account pooling / drift detection / overage guard / 0600-0700 / SLSA。仅 Claude 订阅、无 Pillar B |
| **"flight recorder" 品类名被大厂占** | Microsoft AgentRx / Agent Governance Toolkit 自称 "black-box flight recorder for AI agents"；Opik（21.6k star）营销自称 flight recorder；多篇 "local-first agent flight recorder" 博客。vmr 现在说这个词是**跟随者姿态** |
| **两个跨厂商等价物被有能力团队建完弃坑** | `claude-code-mux`（Rust，518，2026-02 archived）、`OpenMined/alex`（Rust，81，2026-08-13 archived——有 body 捕获 + subagent 重建 + session 分组 + replay + 跨账号 quota + prompt cache）。**这个精确的产品概念反复被造、反复拿不到用户** |
| **Pillar A 每一项被追平** | CCR（36.9k）有 failover + 元数据日志 + 成本估算；LiteLLM（57.4k）有专门的 "Claude Code Prompt Cache Routing" 文档 + `session_affinity`；OpenRouter 有 sticky routing |
| **市场奖励的是别的东西** | cc-switch（129.8k，Rust GUI 切换器）、CLIProxyAPI（49.0k，OAuth 订阅白嫖）、CCR（极大化控制面） |

护城河复核：8 项声称差异化 → **2 项明确追平**（session affinity、成本）、**2 项从"独占"降"稀有"**（byte-faithful、两层 body）、**4 项仍独占且全在 Pillar B 的事实层**（agent 语义 story、跨 run 对比、任意-agent 零埋点 replay、quirk 修复）。而 Pillar B **无任何可测量的用户拉力**——OpenAI Agents SDK 那个 replay+divergence feature request 0 赞 0 评论。

**第三方对该赛道的结构性总结**（可直接引用）："Proxy/gateway tools log calls with zero instrumentation but miss the orchestration graph; SDK/OTel tools trace agent steps properly."——vmr 恰好卡在这句话的缝里（零埋点代理**且**重建 orchestration graph），这是它唯一真正独占的位置，但也是众多角度之一，不是蓝海。

### 4.3 元工作已经从"资产"翻转成"税"

④号评审的量化（§3.3 已列部分）指向一个结论：**这套"给一个不存在的接手团队降低成本"的纪律，正在给唯一的作者持续加税。**

一个标本：`docs/REVIEW_STATUS_PAGE_UPDATE.md`——**24KB 评审文档**，为的是把 `/status.html` 拓扑表的几列重新排版。24KB 文档 : 约 30 行 HTML 改动。

战略文档自己的风险矩阵把"在无人使用的状态下持续投入而耗尽动力"标为"**高（当前最大风险）**"，把"过度工程相对于用户规模"标为"**高（已发生）**"。证据完全支持这两条。

### 4.4 最可能的真相

**这个精确的产品——跨厂商 + 跨协议 + 无 DB + 离线 agent 取证——是一个反复被有能力的人构建、反复拿不到用户的形态。** 不是能力问题，是市场问题：市场奖励的是极简 GUI 切换器和订阅白嫖代理，而"取证 / 可回放"这类离线价值，中文社区付费/关注意愿低，英文社区的品类红利刚被 Microsoft 稀释。

同时，事实层（`ctxgraph` + journey + spine + detail）是一块**真实的、无第二个完整实现的**技术资产。它现在的问题不是"不够好"，是**锁在一个不能给别人看的产物里**（171MB Markdown，含完整对话正文 + 用户 `~/.zshrc` + 内部路径）——壁垒锁在保险箱里就不是壁垒。

### 4.5 结论

**不建议无限期继续按现在的方式投入。** 建议：

1. **把"争取采纳"当成一个 8~10 周的限时实验**，窗口内只做 §0.3 列的那几件（修 P0 bug、DX P0、HTML+脱敏、一个新能力锚点、改名、写工件、发出去），**冻结其余一切**。
2. **预先约定止损**：窗口结束仍 0 外部 issue / <20 star → 有意识地转入"自用工具 + 工程作品集"模式，这是一个**体面的结果，不是失败**——竞品坟场已经证明，在这个形态里继续磨特性不会改变结果。
3. **无论走哪条路，都立即停止过度供给**：冻结 analytics 半区和 quota 高级面、文档预算按"作者自重入状态"重定、停写一次性大 review（**包括本轮之后**）。

---

## 5. 现有能力的深化 / 优化机会

用户要求：更简单易用高效，但不为此增加巨大复杂度。评分口径——**成本** = 工作量 + 长期复杂度；**风险** = 改错的爆炸半径（是否动契约 / 碰热路径 / 要同步几处）；**价值** = 真实痛点 + 架构收益；**ROI** = 价值 ÷（成本 + 风险），三档。

### 5.1 Bug 修复（本身就是"深化"）

| 项 | 成本 | 风险 | 价值 | ROI | 优先级 | 状态 |
|---|:---:|:---:|:---:|:---:|:---:|---|
| B1 buffered 截断 + `http.ErrAbortHandler` 信号 | 低（~0.5天） | 低 | 高（无人值守 agent 静默空成功是最危险场景） | **高** | **P0** | ✅ 已修复（含外部反馈收窄：SSE thinking 阶段扣住不 flush）。§9 |
| B2 quota default-`since` 重置（`DefaultSince` 日历对齐） | 低（~1天） | 低 | 高（静默摧毁子系统核心状态 + 让持久化层对常见情况失效） | **高** | **P0** | ✅ 已修复（含外部反馈收窄：锚点定在午夜而非整点，残余收到"跨日重启 + 奇数 N"）。§9 |
| B3 LLM `EvidenceAnchor` 运行期校验 | 低（~0.5天） | 低 | 中高（兑现安全承诺；否则解读层不该保留） | **高** | **P0**（若保留 LLM 层） | ✅ 已修复（选择"修"而非冻结 S4——S4 本身未做，仍待排期） |
| B4 `report` Markdown 转义 | 低（~0.5天） | 低 | 中 | 高 | P1 | ✅ 已修复（`mdTable.row` 集中 `EscapeCell` + 标题 `EscapeHTML`） |
| B5-B9 小竞态 / 卫生 | 各 <0.5天 | 低 | 低-中 | 中 | P2 | ✅ 全部已修复。§9 |
| B10-B12 | 各 <0.5天 | 低 | 低 | 低-中 | P2-P3 | ⬜ 未做（排期，超出本轮范围） |
| B13 imgprep 32-bit 整数溢出 | <0.5天 | 0 | 低 | 低 | P3 | ⛔ 确定不做——32-bit 非目标/CI 平台（Go `image/png` 已用 `int32` 上限，64-bit 乘积不溢出）。登记进 KNOWN_ISSUES §1。若 32-bit 成为目标平台则改 `int64` 乘积 |
| B14 详单文件名 hash8 碰撞 | <0.5天 | 低 | 低 | 低 | P3 | ⬜ 未做（评审自身即判"登记即可，不值得现在动"）。已登记进 KNOWN_ISSUES §1 |

### 5.2 DX / 分发就绪（⑥号评审——首次配置体验与"零配置单二进制"营销之间有真实鸿沟）

> **状态（2026-08-28 第二轮）**：**3×P0 已全部落地**（✅ 行，详见 §10.2）。P1/P2 行未做。

| 项 | 成本 | 风险 | 价值 | ROI | 优先级 | 建议 |
|---|:---:|:---:|:---:|:---:|:---:|---|
| ✅ **`config.minimal.yaml`（~10 行）+ README Quick Start 改用它** | 0.5天 | 极低 | 高（消除最大摩擦点——当前唯一模板 470 行 83% 注释 + 投机模型名） | **高** | **P0** | 实测 9 行就能端到端跑通。`config.example.yaml` 顶部第一行改为"这是完整注解参考，只想跑起来用 minimal" |
| ✅ **让"忘设 key"失败可读** | 1天 | 低（不碰成功路径 byte-faithful） | 高 | **高** | **P0** | `vmr start` 缺 key 打醒目 banner（非埋在 config dump 里的 WARN）；全 endpoint 无 key 的请求合成 vmr 侧错误（"all N endpoints for 'coding' have no API key — set ${...}"）而非透传上游 401 + Cloudflare 噪声 |
| ✅ **README Quick Start 加 `vmr diagnose` 作为第 3 步** | 0.2天 | 0 | 高（它做 DNS+TLS+key+真实 echo+路由表一屏红绿灯 = 战略文档 §6.2 想要的演示素材，但 `grep -c diagnose README.md` = 0） | **高** | **P0** | 纯文档改动 |
| `vmr check -json` + `vmr diagnose`/`check`/`status` 三者 `-json` 齐全 + 一个 `doctor` 摘要 | 0.5-1天 | 低 | 中（演示素材；战略文档自列的 P0 未完成项） | 中 | P1 | |
| `vmr service install` 做进二进制，砍 `vmr.sh` 的 service 模式（~150 行 launchd/systemd 渲染） | 2-3天 | 低 | 中高（更符合"单二进制"叙事；当前生产部署功能锁在不随 release 发的 dev 脚本里） | 中 | P1 | |
| CLI 打磨：修 flag 渲染 bug、从 flag 帮助和日志清除 §-编号 + 内部术语、`-h` 退出 0 | 1天 | 低 | 中（B17；违反项目自己的 CLAUDE.md） | 中 | P1 | |
| 修 README "See It in Action"（B20）、`config.local.yaml` bit-rot（B16）、`curl /log` 挂起提示（B19） | <0.5天 | 0 | 中（首次信任） | 高 | P1 | |
| `config.example.yaml` 拆"最小可用 + 完整参考"两文件，83% 注释挪进 UserGuide | 1天 | 低 | 中（降认知墙 + 减少多处同步点） | 中 | P1 | |
| UserGuide 加"5 分钟路径"章节 + troubleshooting 表（"connection refused → ?"、"全是 401 → 检查 key/cooldown"） | 1天 | 低 | 中 | 中 | P2 | |

### 5.3 简化 / 删减（不牺牲能力，减复杂度）

| 项 | 减掉什么 | 成本 | 风险 | 建议 |
|---|---|:---:|:---:|---|
| **S1 冻结 analytics 半区新增能力** | 标 v1-complete，只修 bug 不加 section/detector/corpus 维度。P11-P15 五个 phase 在战略文档说"暂停"之后落地，6 个 LLM 判别器仍未校准，0 用户反馈 | 0 | 0 | **立即。把"再加一个维度"从默认冲动变成需要理由的例外** |
| **S9 停写一次性 comprehensive architecture review** | 已有 gemini v1-v4 + sonnet + opus + Fable + 3.6 版 + 2 版 audit_report + 2 版 tier12 review。`archtest` + KNOWN_ISSUES 已是可执行连续守卫，一次性大 review 边际信息量≈0 | 0 | 0 | **立即。真要 review 写进 KNOWN_ISSUES 的 diff，不新建文件。本报告是最后一份** |
| S2 删 `vmr report` / `vmr story` 过渡别名 | `cmd_report.go`(388) + `cmd_story.go`(798) 的独立 flag 定义、默认值解析、逐字节等价性测试。迁移提示给谁看？0 外部用户 | 0.5天 | 极低 | **cut**。内部 muscle memory 用 shell alias。省 ~1k 行 + 一批等价性测试维护 |
| S3 冻结 `-corpus` 语料统计 | `corpus*.go` + `render_corpus.go` ~900 行 + `i18n/story_corpus.go`(9KB)。`corpusMinCorrelationN=5`（"noise dressed up as a number"）；语料无成功/失败标签 | 0.5天 | 低 | **defer**（`//go:build corpus` 或移 `_eval/`）。探索性指标用外部脚本消费 `journey-*.json` 契约更合适（KNOWN_ISSUES §2.5 自己也这么说） |
| S4 LLM 解读层 6 检测器缩到 2 个 | `llm_findings.go`(15KB) + `llm.go`(20KB) + `llm_single.go` + `llm_divergence.go` + `i18n/story_llm.go`(**41KB 双语 prompt**) ~1.5k 行。架构上完全隔离（`opts.Enabled()` 门控） | 1天 | 低 | **defer**。先修 B3，缩到最有信心的 2 个（`error_then_unverified_success` / `goal_drift`），其余移 `_eval/`。真有用户要了再扩 |
| S5 删 `respnorm` 观测-only 检测器 | `thinking_process_pattern_detected`、`crlf_framing_suspected`——~50 行热路径邻近记账，只往审计 `norm` 串加一个字符串，**无下游消费者** | 0.5天 | 低 | 删到有分析消费者为止。形状 trivially 可再加 |
| S6 删 `health.Status.Available` | 3 个重叠布尔，`Available` 注释自承"narrower than its name suggests"、"Kept for backward compatibility"——**直接违反项目自己 §2.2 的"不做兼容"原则**（`serving *bool` shim 已按该原则删除） | 0.5天 | 低（契约测试先红） | 删 `Available` 留 `Serving`。它保留所依据的兼容豁免已不存在 |
| S8 quota example 只铺基础旋钮 | bucket/gate/scope/token_weights/model_multipliers/多币种 overrides 从 example 主体移到 UserGuide "advanced" | 0.5天 | 低 | 至少 example 里别把全部旋钮铺开——1,061 行 + 1,537 行设计文档，rolling window（headline 用例）本就没实现 |
| S11 `/help`+`/help.zh` HTML（3,100 行双语）收敛 | 砍虚构/极小众 agent（OpenClaw/WorkBuddy/Hermes/OpenDesign——与配置示例 `gemini-3.6-flash-high` 同款"未来命名"）；或 per-agent snippet 从嵌入 HTML 移到运行时从一个 JSON 表生成 | 中 | 低 | dashboard 类 UI 与"不做 Web UI"自我定位冲突；这不是首次摩擦点（"怎么把 Claude Code 指过来" = 2 个环境变量，README 里已有）——它打磨的是本来就不难的表面 |
| S12 rolling-window quota 从"planned"改"除非测到具体 429-thrash 案例否则不做" | 文档措辞，0 代码 | 0 | 0（诚实） | 实际下行风险正是健康状态机已处理的（一个请求打到仍受限账号 → 一个 429 → failover → 封顶冷却 + 后台探针） |
| S10 `archived/` 从工作记忆划掉 | 4.5MB / ~247k 词的评审和计划文档坟场，已 gitignore | 0 | 0 | 别再往里加。文档预算按"作者 3 个月后重新进入状态需要多少"定 |
| S7（低优先）`internal/strategy` 折叠 `Dimension` 接口 | priority 是唯一维度，config 注释自认"nothing else to name yet"，CLAUDE.md 为它维护一条不变式 | 小-中 | 中（碰 router sort + quota reorder 交互） | **低优先**，不值得单独做——但**下次有人想加维度时，先质疑是不是 YAGNI** |

### 5.4 性能 / 正确性深化

> **状态（2026-08-28 第二轮）**：**两项 P1 已落地**（✅ 行，详见 §10.3）。`analyze` 内存只做了 `stitch.go` blob 索引的最小动作（降常数，不改分桶前提）——按日分桶仍未做。P2/P3 行未做。

| 项 | 成本 | 风险 | 价值 | ROI | 优先级 | 说明 |
|---|:---:|:---:|:---:|:---:|:---:|---|
| ✅（部分）**`vmr analyze` 内存曲线压缩** | 中 | 中（按日分桶释放依赖"时间严格单调"隐蔽前提） | 高（用户第一次拿季度日志跑就会 OOM） | 中高 | **P1** | 实测 15,824 条 = **3.75GB RSS / 862s**。KNOWN_ISSUES 1.2 说触发点 3 万条/4GB——那是 `report` **单独**跑；组合 `analyze` 路径在 1.5 万条已 3.75GB，**触发点约 2 万条不是 3 万**。**已做**：`stitch.go` `buildBlobLineageIndex` 的 `map[Hash]map[int]bool` → `map[Hash][]int`（去掉数百万单元素小 map 的头开销）。**未做**：按自然日分桶释放。 |
| ✅ `include_usage` 可见性告警 | 0.5天 | 0 | 高 | **高** | **P1** | `metric: tokens`/`cost` 在 openai-completions 流式端点上，客户端不发 `stream_options:{include_usage:true}` 时 vmr **永远看不到 usage**，100% 字节估算。vmr 不能注入（byte-faithful）。**已做**：`config.Check()` 新增 `checkQuotaUsageVisibility`（SeverityWarning）+ `vmr status` / `vmr report` 在 `estimated_pct ≥ 95%` 且 metric∈{tokens,cost} 时追加同因提示 + UserGuide/.zh 新增说明段。 |
| `report` 第三趟扫描缓存（KNOWN_ISSUES 1.1） | 中 | 中高（`collect()` 触及会话/任务边界判定正确性） | 中（缓存收益已证 5.2×） | 中 | P2 | 先补 cold/warm 一致性测试基建再动 |
| `story` golden 测试 fixture 加厚 | 中 | 低 | 中（当前 3 条合成记录，无 compaction/stitch/failover——价值和风险恰在这些路径） | 中 | P2 | 用 `t.TempDir()` 绕 jsonl gitignore 约束 |
| `linkCompactions` 未命中率汇总成一行 summary（而非逐条刷 stderr）；`ExtractEntities` O(n²) span 包含过滤改单趟；慢 e2e 测试改 opt-in | 各小 | 低 | 低 | 中 | P3 | 交互体验糖 |

### 5.5 文档修正（一次性，~1天）

> **状态（2026-08-28）**：B1–B9 直接相关的三项已随本轮完成（下方 ✅）。其余五项映射到 B10–B15 / §3 挑战，均**未做（⬜）**——超出本轮 B1–B9 范围，留待后续文档专项。

- ✅ KNOWN_ISSUES §0 "无数据丢失" → 已移入 §1 表述 + B1 触发条件与修复说明
- ⬜ KNOWN_ISSUES #38 "不产生死链接" → 改写（B10 实测有死链接）——未做（B10 未做）
- ⬜ KNOWN_ISSUES 1.2 触发点按 `analyze` 组合路径重标定（~2 万条）——未做（非 B1–B9）
- ✅ KNOWN_ISSUES Quota "决定不修" 表里"未配 `since` 时的热重载锚点漂移" → 已从"非问题（撤回）"改判为"已修复（B2）"，并说明原裁决只覆盖相位对齐
- ⬜ Core §6.6 计量段 → 更新到 P3 quota 形态——未做（非 B1–B9，见 §3.6）
- ✅ Quota 文档"计数跨重载存活"两处 + `since` 表格行 → 已加缺省 `since` 的日历对齐说明（比原建议的"仅对显式 `since:` 成立"更进一步——B2 修复让缺省情况也存活）
- ⬜ CLAUDE.md `core` 行的 "admission rule" 断言（B15）——未做（非 B1–B9）
- ⬜ Analytics §2.4/§3.3/§3.5a + `metrics.go` 实体抽取描述（B11）——未做（非 B1–B9）
- ⬜ CLAUDE.md 第一句 "two co-equal halves" 改写为 §3.2 诚实版本——未做（非 B1–B9）

此外，随 B1–B9 顺带做的文档同步（不在原 §5.5 清单里，但属"docs are current state"义务）：Core 设计文档 §5.5 新增"上游流中途失败"段 + norm 变换表补 `truncated_flush`/`truncated_withheld` 两行 + 六条约定第 1 条更新；Analytics 设计文档 Journey 构建段补"缝合边界 ≠ 任务边界"；`internal/report/aggregate.go` 的 norm 词表注释补两个新标记；`CHANGELOG.md [Unreleased] > Fixed` 增 B1/B2/B3/B4/B5/B9。

---

## 6. 新增扩展方向

**前置声明**：对一个 0 用户项目，"加功能"几乎都是错的方向。下表按"能否帮项目拿到第一个真实用户"排序。

> **状态（2026-08-28 第二轮）**：**E1、E2 已落地**（✅ 行，详见 §10.4 / §10.5）。**E3 本轮 hold**（用户决定——见 §10.6 的说明与遗留判断）。E4–E9 未做。

| # | 方向 | 成本 | 风险 | 价值 | ROI | 优先级 | 说明 |
|---|---|:---:|:---:|:---:|:---:|:---:|---|
| ✅ **E1** | **HTML 单文件渲染 + 脱敏**（设计文档 §8，未实现） | 中 | 低（纯渲染层，事实层不动） | **高** | **高** | **P1，唯一真心推荐的新增** | 171MB / 446 份 Markdown 是"内部工具"产物，含完整对话正文 + 用户 `~/.zshrc` + 内部路径。**脱敏是"能分享给团队外"的前置条件**，而"能分享"是这个项目从 3-star 走向有用户的唯一现实杠杆——"agent flight recorder"是核心壁垒，但壁垒锁在一个不能给别人看的产物里就不是壁垒。单文件自包含 HTML（内联 CSS/JS）+ 脱敏模式（结构和指标保留，正文替换为长度占位 + 类型标签）。**已落地为 `vmr analyze -journey <id> -html [-redact]`（仅单命中），完整瀑布 UI**。 |
| ⏸ **E3** | **per-virtual-model 预算硬闸**（战略文档 P1，仍未做） | 1.5-2天（纯 token 版） | 低 | 高 | **高** | **P1** | 现在的健康机制全是事后反应。**一个进入死循环的 agent 可以一夜烧光整月额度，vmr 全程忠实转发一句话不说**——这是每个 bootstrap 用户的恐惧。内存态预算注册表（仿健康注册表），请求入口只查一次，触顶 = 明确拒绝 + 可解析错误（**绝不静默降级到便宜模型**），每日零点 + 进程重启都重置（"防呆不防欺诈"，不引入持久化）。快、安全、普适、能自我用审计日志验证。**本轮 hold（用户决定）**——见 §10.6。 |
| ✅ **E2** | **软屏蔽（2xx 但内容为空/被替换）升级为可 failover**（战略文档 P1，仍未做） | 2-3天 | 中高（清单里架构风险最高项——要把"2xx 即刻上报健康并开始转发"这个隐含前提拆开） | 高 | 中高 | P1（若走分发路线） | 国产厂商特有痛点，agent 无感知基于空回复继续跑，无人值守时比 429 更危险。**范围收窄到非流式（buffered）路径**（流式从第一个 payload event 起已在边转发，技术上做不到 failover）。判据收窄到"标记命中 **且** 有效内容为空/极短"。复用现有 `ErrContent`。**已落地为 `soft_block_failover`（虚拟模型级 + endpoint 级两级开关，缺省关）**。 |
| E8 | `vmr analyze` 与路由核心解耦，做成能吃 CCR / LiteLLM / Helicone 日志的"agent 运行分析器" | 中高 | 中（牺牲"字节忠实所以可信"论证——翻译型 gateway 的日志已被改写） | 高 | 中 | **战略选项，需用户决策** | 如果目标是让 Pillar B（事实层这块真资产）被人用到，这可能是唯一有量的分发路径——借它们已有的用户。与 E1 不冲突，但方向上是"承认独立分发走不通" |
| E4 | rolling-window quota（Claude Code Pro/Max 严格滚动窗口） | 中（~200 行 + Ring 平滑） | 中 | 中（headline 用例但目前只近似） | 中 | P3（且应先有真用户要它） | 要么做，要么别在 README / Strategy 里当卖点讲（S12） |
| E5 | 第 4 个 ingress 协议（gemini） | 中 | 低 | 视需求 | — | 按需 | 架构真的 ready：`BuildSnapshot`/`strategy`/`config`/`CanonicalRequest` 协议无关，`IngressPath`/`runProbe`/`diagnose` 已有 per-protocol case。"新包 + 1 blank import + 1 路由行 + probe body" |
| E6 | Weight / Latency / Cost `Dimension` | 中 | 低 | 中 | 中 | 用户提出后 | `strategy.Dimension` 接口 + `reorderByQuota` 的 tier 逻辑已假设多维度 |
| E7 | per-endpoint rpm / 并发限流 | 中 | 中（新状态机） | 中 | 中 | 429 成真实痛点才做 | 现有 failover 已把 429 用户可见伤害压得很低 |
| E9 | Subagent 树（设计文档 §8） | 中高 | 中（识别信号未验证——设计文档自己说"开工前先跑采样脚本") | 中 | 低 | P3 | 先验证三条信号命中率，不过继续推迟 |

**明确不做**（复核后确认，与既有边界一致）：语义缓存（对 agent 是正确性风险）、MCP 网关（不在 LLM API 路径上）、Web UI / DB / RBAC / 分布式 / 跨实例 quota、翻译 / bypass 模式、`.so` 运行时插件、让价目表进实时路由、通用 HTTP provider（映射 DSL）、更多 LLM 检测器 / 对比维度 / corpus 维度。

---

## 7. 优先级排序的行动建议

### 7.1 无论走哪条路都要做（"过度供给止血" + P0 bug + 文档修正）

| 优先级 | 动作 | 成本 | 状态 |
|---|---|---|---|
| P0 | 修 B1（buffered 截断）、B2（quota default-since 重置） | ~1.5天 | ✅ 已完成（连同 B3–B9，见 §9） |
| P0 | S1 冻结 analytics 半区新增能力；S9 停写一次性大 review（本报告之后） | 0 | ⬜ 未做 |
| P0 | S12 rolling-window quota 改"除非测到具体案例否则不做" | 0 | ⬜ 未做 |
| P1 | §5.5 文档修正一次性清（含 CLAUDE.md "co-equal" 改写、`core` admission-rule、Core §6.6、Quota line 934/1034/1133、KNOWN_ISSUES §0/#38/1.2） | ~1天 | 🔸 部分完成——B1–B9 相关的三项已随本轮做（见 §5.5 标记），其余待后续文档专项 |
| P1 | S2 删 `vmr report`/`vmr story` 别名；S3 冻结 `-corpus`；S5 删 respnorm 观测检测器；S6 删 `health.Available` | ~2天 | ⬜ 未做 |
| P1 | `include_usage` 可见性告警 | 0.5天 | ⬜ 未做 |

### 7.2 若走方案 A（限时采纳实验，8~10 周）

在 7.1 基础上：

| 顺序 | 动作 | 成本 |
|---|---|---|
| 1 | 3 个 DX P0：`config.minimal.yaml` + README 用它 / "忘设 key"可读 / README 加 `vmr diagnose` 第 3 步 | ~2天 |
| 2 | B3 修（LLM anchor 校验）或 S4（LLM 层缩到 2 检测器） | ~1天 |
| 3 | **E1 HTML + 脱敏渲染** | ~5天 |
| 4 | **E3 per-model 预算硬闸**（快、安全、普适）——作为"一个新能力"锚点 | ~2天 |
| 5 | §5.4 `analyze` 内存曲线压缩（`stitch.go` blobLineages 表示） | ~3天 |
| 6 | 改名 + README/description 重定位（§7.4）+ 写 2 个具体工件（一张真实 bug 的 `vmr story` 分解截图 + 一篇 `vmr story -compare` 定位分岔点的技术长文） | ~4天 |
| 7 | 发 linux.do → V2EX → 掘金 → Show HN（标题落在"字节级 + 可重放"，**不主打成本/failover/多模型**——全被追平） | 持续 |
| — | **窗口内冻结**：quota 高级面、dashboard 打磨、协议改名类、E2 之外的所有新特性 | — |
| Week 10 | **止损检查**：仍 0 外部 issue / <20 star → 转方案 B | — |

若第 4 步后还有余力且走分发路线，**E2（软屏蔽 → failover）**是差异化最强的第二个锚点（但架构风险最高，需额外回归验证）。

### 7.3 若走方案 B（自用 + 作品集）

在 7.1 基础上：

- README 顶部明说"个人工具，无 roadmap，不做 SaaS / enterprise 版"——对这类工具这是建立信任的方式，不是示弱。
- 设计文档冻结在当前状态（它们已经"写完了"）。
- 之后只接受安全 / 正确性修复。
- `_eval/` + `TestArchitecture_EvalToolsCompile` + 未校准 LLM 判别器：要么删，要么明确标 experimental 移出默认路径。

### 7.4 改名与重定位（方案 A 必做，成本近零）

- **`vmr` 是明确的负资产**：搜索污染严重（Virtual Meeting Room / Video Mixing Renderer / variance-to-mean-ratio），且讽刺性冲突——`.vmr` 是飞行模拟器的"飞机模型匹配文件"，搜 "vmr flight" 直接撞飞行模拟器。"virtual model router" 不是任何人会输入的词组。
- 保留 `vmr` 作**二进制名**（好敲），给项目一个可搜索的正式名 + tagline。方向：用一个具体动作命名（如 `bytewire` / `passthru` / `replayd` 类，需逐个核 GitHub / 商标），tagline 承担解释——"the local proxy that records your agent's runs byte-for-byte so you can replay the one that broke."
- **GitHub description 和 README 头版必须含 "Claude Code"**——那是最高流量的入口搜索词。
- 定位顺序反过来（战略文档 §4.2 已论证，零代码）：头版 = "agent 运行的黑匣子：那次失败，能查到上游到底收发了什么，还能原样重放" → 第二层 = 自动 failover → 第三层 = byte-faithful（作为"为什么可信"的依据，不是"这是什么"）。

---

## 8. 附录

### 8.1 竞品现状速查（2026-08-28 GitHub API 实测）

| 项目 | Star | 一月前 | 语言 | 与 vmr 的关系 | 威胁 |
|---|---:|---:|---|---|---|
| CLIProxyAPI | 49.0k | 45.2k | Go | OAuth 订阅转 API + 轮询负载均衡；无 body 审计 / session affinity / replay | 中（相邻） |
| claude-code-router | 36.9k | 36.2k | TS | transformer 改写型；failover / 元数据日志 / 成本 / dashboard 俱全；1,092 open issues，增长放缓 | 高（占搜索词） |
| **dario** | **491** | 新 | TS | **最收敛的对手**：byte-faithful + session affinity + 多账号 headroom 池化 + overage guard + drift 检测 + TUI。仅 Claude、无 Pillar B | 高（Pillar A 已被跑赢 160×） |
| claude-code-mux | 518 | 520 | Rust | 优先级路由 + failover + 15+ provider。**2026-02 archived** | 已死，警示强 |
| OpenMined/alex | 81 | 新 | Rust | body 捕获 + subagent 重建 + session 分组 + replay + 跨账号 quota + prompt cache。**2026-08-13 archived** | 已死，警示极强 |
| cc-switch | 129.8k | — | Rust | Claude Code/Codex/OpenCode 多 agent GUI 切换器 | 非对手，但说明市场奖励什么 |
| LiteLLM | 57.4k | 54.8k | Py→Rust | 翻译型；已有 `session_affinity` + 专门的 Claude Code prompt-cache 路由文档；$10M+ ARR | 中（注意力威胁） |
| new-api / one-api | 46.6k / 36.6k | 43.6k / 36.0k | Go / JS | Key 池赛道，正交（one-api 已停更） | 低 |
| Langfuse / Opik / Helicone | 33.8k / 21.6k / 6.1k | — | TS / Py / TS | 观测赛道；Langfuse/Opik 靠埋点；Helicone 是零埋点代理但 2026-03 被收购进维护模式 | 低-中 |

### 8.2 关键数据（评审期间实测）

| 指标 | 值 | 来源 |
|---|---|---|
| 生产代码：analytics / routing / cmd | 23.7k / 14.5k / 4.0k 行 | `wc -l` |
| 磁盘散文 / 代码 | 7.6MB / 3.76MB | `du` |
| 全历史新增行：markdown / Go-prod | 83,192 / 67,571 | `git log --numstat` |
| `.md` 文件数（其中 review/plan/audit 类） | 133（98） | `find` |
| `vmr analyze` 全量语料（15,824 条 / 43 文件） | 862s / 3.75GB RSS / 171MB 输出 / 446 journey / 0 detail | `/usr/bin/time -l` |
| 战略文档后的 commit / 分发相关 | 237 / ~1 | `git log --since=2026-07-28` |
| GitHub | 3 star / 0 issue / 0 fork / 0 贡献者 | `gh api` |

### 8.3 全部 Bug 清单

见 §2。代码缺陷 B1-B14（P0：B1/B2/B3；P1：B4；P2-P3：其余），文档/DX 卫生 B15-B20。

### 8.4 评审团各切面的"关键判断"原文摘要

- **路由核心**：状态良好，小而自洽，`-race` 全绿，无安全/可用性回归。Bug1 是唯一值得立即处理的。`respnorm`（1079 行）是复杂度热点但基本值得（每个变换都追溯到观测到的 MiniMax quirk + content guard）。
- **分析半区**：核心（`ctxgraph`/journey/spine/detail，~40% 代码）是扎实的战略资产；解读层 + corpus + compare（~30%）是 0 用户在跑的投机泛化，应冻结。事实层已经足够好，它需要的是用户，不是更多功能。
- **额度**：设计上有保留地"值得其复杂度"，时机上不值得（对 0 用户建了 3-4 批）。B2 卡住一切。最大的静默精度漏洞是 `include_usage`。多 Claude Code 订阅（最常见场景）正是这个算法明确不服务的。
- **架构 + 元工作**：架构健康（边界干净且 `archtest` 真正锁住），但项目在每个维度上都相对其阶段过度建造。工程严谨度单项都不过剩，总和严重过剩且投放对象错了。不需要更多工程，需要有人当它"已经够好了"然后去找用户。
- **产品 + 竞品**：产品命题智识上 sound、工程上过硬，但作为追求公开采纳的开源项目，过去一个月的证据让它更悬了。最可能的真相：这个精确产品是反复被有能力的人构建、反复拿不到用户的形态。单个最高杠杆动作：停止泛化，押注唯一无人竞争且中国相关的一簇能力，8 周内验证。
- **DX**：运行时和分析能力已发布级，但首次配置体验与"零配置单二进制"营销之间有真实鸿沟，近期工程精力在填错误的坑（dashboard 视觉迭代 vs 首次配置）。

---

*§1–§8 基于 commit `ce27638`（评审基线）。所有 `file:line` 引用、实测数字、竞品数据均在评审期间核实。下面的 §9 是后续执行记录。*

---

## 9. 执行情况记录（B1–B9 修复，2026-08-28，Sonnet 5）

> 范围：§5.1 表里的 **B1–B9**。B10–B14、§5.2/§5.3/§5.4、§6、§7 的分发动作均**未做**（本轮严格限定在 B1–B9）。确定不做的条目见 §9.6。所有改动最初保留在工作区供人工复核，复核通过后作为一个 commit 落地。

### 9.1 执行方式

先通读上下文（CLAUDE.md、4 份 v4 设计文档相关章节、KNOWN_ISSUES、涉及的源码文件），对 B1–B9 逐条回到源码核实根因与修法是否成立——**九条全部属实、修法方向合理**，无需推翻。随后制定 Action Plan（`_tmp/plan_sonnet-5.md`，gitignore，作为跟踪记录保留），按"修法 → 测试同步 → 验收 → 文档同步 → 收尾"逐项执行。

执行前就两点向用户确认并获答复：① B1 采用 **flush + abort**（把已收字节 flush 给客户端后 `panic(http.ErrAbortHandler)`）；② 文档同步范围**仅限 B1–B9 直接相关**（不含评审 §5.5 的 CLAUDE.md "co-equal" 改写、Core §6.6 等更宽的清单）。

### 9.2 逐项修复

| # | 改动文件（生产代码） | 核心修法 | 新增/更新测试 |
|---|---|---|---|
| **B1** | `internal/respnorm/respnorm.go`、`internal/router/router.go` | 新增 `flushRawOnError()`：非 EOF 上游错误分支把**可安全交付**的已收字节（带模型名重写）flush 进 `s.out` 再置 `srcErr`——非 SSE flush `s.buf`（部分 JSON），SSE 只在 `modePassthrough` flush 尾部、`modeUndecided`/`modeBuffered` 一律扣住（避免泄漏未闭合 `<think>`，记 `truncated_withheld`）；`forwardSuccess` 在 `status == "TRUNCATED"` 且全部计费/审计/日志记账完成后 `panic(http.ErrAbortHandler)`，net/http 静默 abort 连接、丢掉终止 chunk | `TestRespStream_BufferedTruncationFlushesReceivedBytes`（新）、`TestRespStream_ThinkBufferedTruncationWithholds`（新，评审后追加）、`TestBufferedTruncationAbortsAndFlushes`（新，端到端断言部分字节到达 + `io.ReadAll` 报错 + 审计 attempt 带截断错误）；`TestNonSSEBodyStallAborts` 更新为接受 abort |
| **B2** | `internal/quota/period.go`、`internal/config/quota.go` | `DefaultSince(now, unit)`：缺省锚点对齐到固定日历边界——min/h/d→当日午夜、w→周一 0 点、mo→月初。午夜锚点使周期栅格锁死到"日"：同一自然日内任意热重载对任意 `every` N 都解析出同一锚点、`PeriodStart` 恒等、`resetIfStaleLocked` 不再误清零 | `TestDefaultSince`（按单位表驱动）、`TestDefaultSince_ConvertsToDisplayZone`（更新）、`TestDefaultSince_SurvivesReload`（新，同日跨 20h、跨多整点、min/h/d/w/mo × 多种 N 全绿）、`TestRegistry_DefaultSinceReloadDoesNotReset`（新）；`TestQuota_HappyPath_Tokens_DefaultSince`、`TestQuotaStatus_PerModelWildcard_OneRowPerActuallyChargedModel` 更新（后者原先隐式依赖"缺省锚点==解析时刻"的巧合） |
| **B3** | `internal/story/llm_findings.go` | 把 `_eval/calibrate_p1b.go` 的 `transcriptPool` 逻辑搬进包内（`searchableTranscript`）；`ComputeLLMFindings` 收集完全部 detector 输出后逐条按 `strings.Contains(pool, anchor)` 校验，锚点非逐字子串即丢弃（保持 fail-open） | `TestComputeLLMFindings_AnchorVerification`（新，真锚点存活/假锚点丢弃两个子测试）、`TestSearchableTranscript_CoversReconstructedAndRaw`（新） |
| **B4** | `internal/report/render_doc.go`、`section_sessions.go`、`requests.go`、`metrics.go` | `mdTable.row` 集中对每个 cell 走 `reqdetail.EscapeCell`（`\|`/换行）；会话标题、任务标题引用块、context-growth Finding 里的标题额外走 `reqdetail.EscapeHTML`（未闭合 `<!--` 吞并） | `TestMarkdownEscapesUserDerivedTitles`（新） |
| **B5** | `cmd/vmr/cmd_start.go` | `var reloadMu sync.Mutex` 包 `reload` 闭包体，串行化 fsnotify 与 SIGHUP 两条重载路径 | 依赖 `-race` + 既有热重载测试保持绿；`archtest` 函数长度预算触线后把新增注释压到一行 |
| **B6** | `internal/router/quota.go` | `MetricCost` 分支给 `reg.Charge` 传 `0` 而非 token 计的 `estimated`（金额估算信号经 `AddEstimatedCost` 单独记） | `TestChargeCost_DegradedEstimate_TracksEstimatedCost` 更新（断言 `bucket.Estimated == 0`、`EstimatedCost` 正常） |
| **B7** | `internal/router/snapshot.go` | `Install` 调换顺序：先 `rt.snap.Swap(s)` 再 `rt.Quota.Prune(...)`，straggler 由下次热重载的 Prune 自愈 | 既有 `Install`/quota 测试保持绿（窄自愈竞态，无确定性 seam，注释 + 现有覆盖） |
| **B8** | `internal/quota/quota.go` | `resetIfStaleLocked` 返回是否重置；`Used()`/`EstimatedCostFor()` 据此置 `r.dirty`，只被读路径观测到的周期滚动也会被 flusher 持久化 | `TestRegistry_UsedResetMarksDirty`（新） |
| **B9** | `internal/story/journey.go` | stitch 边界不再无条件开新 Task——只在 `newInstructionTitleAtStitch` 找到不在 `seen` 里的真实新指令时才开；否则沿用 `curTask`，该 Step 仍带 `StitchEdge`/`Compaction`，脊柱/Markdown 渲染器照常渲染 "🧵 Stitched" + 压缩摘要 | `TestStitchedJourney_EndToEnd` 更新（断言压缩缝合 = 单 Task）、`TestStitchedJourney_NewInstructionOpensTask`（新） |

### 9.3 验收结果

- `go build ./...`、`go vet ./...`、`gofmt -l internal/ cmd/`（无输出）全部通过。
- `go test ./...` 全绿（含两轮外部反馈后的重跑）。
- `go test -race`（router / health / audit / quota / sticky / config / respnorm / server / story / report / cmd/vmr / archtest）全绿。
- `go test ./internal/archtest/...` 全绿（B5 触及 `cmdStart` 函数长度预算，已把新增注释压到一行而非抬预算数字）。
- `vmr check -c config.example.yaml` 与 `-c config.example.zh.yaml` 均校验通过。

### 9.4 文档同步

- `docs/KNOWN_ISSUES_sonnet-5.md`：§0"当前状态"去掉绝对化的"无数据丢失"表述并指向 B1 修复、§1 分布计数更新；§2.2"截断口径刻意不对齐"条补记 B1 的客户端 abort 信号；§2.5"LLM 解读层准入契约"条补记锚点运行期强制校验；§3"已闭环"新增第 46 项，B1–B9 逐条列出修法与回归测试（含两轮外部反馈的收窄）；§1 新增 `1.49`（B13）/`1.50`（B14）两条低危登记，§4.1/§4.2 ROI 表同步。
- `docs/VirtualModelRouter_Design_v4_Core.md`：§5.5 响应侧归一化新增"上游流中途失败（非 EOF）"段（flush 边界 + abort 信号）；变换清单表补 `truncated_flush` / `truncated_withheld` 两行；「审计日志」六条约定第 1 条更新（norm 词表 + 截断时的客户端行为）。
- `docs/VirtualModelRouter_Design_v4_Quota.md`：`since` 表格行改为描述缺省锚点对齐到固定日历边界（午夜）及其"防清零"用途与收窄后的残余；"计数跨重载存活"两处补"缺省 `since` 亦然"；"已评估并否决的改进提案"表里"未配 `since` 时的热重载锚点漂移问题"从"非问题（撤回）"改判为"已修复（2026-08）"，说明原裁决只覆盖相位对齐、漏了 `resetIfStaleLocked` 清零后果。
- `docs/VirtualModelRouter_Design_v4_Analytics.md`：Journey 构建段补"缝合边界与任务边界是两件事"，明确任务中途压缩不开新 Task。
- `internal/report/aggregate.go`：`diagnosticNormMarker` 上方注释的 norm 词表补 `truncated_flush`/`truncated_withheld`（它们同属"vmr 自己的传输处理"，不进 per-endpoint 诊断计数）。
- `config.example.yaml` / `.zh.yaml` + `docs/UserGuide.md` / `.zh.md`（四处 EN/ZH 成对同步，遵 CLAUDE.md `.zh` 同步义务）：`since` 注释里"不写＝加载那一刻、不做日历对齐"改为"不写＝对齐到该单位的日历边界（午夜/周一/月初），使同周期热重载不清零计数"。
- `CHANGELOG.md`：`[Unreleased] > Fixed` 增 B1/B2/B3/B4/B5/B9 条目（B6/B7/B8 属内部正确性/持久化细节，无用户或开发者可见行为变化，只记入 KNOWN_ISSUES §3 第 46 项）。
- `.gitignore`：新增 `/scratch_vmr`（工作区里一个 stray 构建产物，防误提交）。

### 9.5 遗留与说明

- **B1 的行为变更**：buffered 模式上游中途断流时，客户端从"干净空 200"变为"部分字节（非 SSE / 已开始流的 SSE）或空（thinking 阶段被扣住的 SSE）+ 连接 abort"。已获用户确认采用。审计顶层 `outcome` 仍记 `ok`（HTTP 交换在传输层的口径）、attempt 级记截断——这个双账本口径本身**不变**，见 KNOWN_ISSUES §2.2。
- **B1 follow-up（外部反馈 2.2）**：初版 `flushRawOnError` 对 SSE 也 raw flush `s.buf`/`s.pending`，MiniMax thinking 流中途截断会把未闭合 `<think>` 泄漏给客户端——正是 `respnorm` 存在的意义（防模型把自己的推理喂回自己）。收紧为"SSE 只在 `modePassthrough` flush"，比反馈建议的"针对 MiniMax 特征做截断清洗"更简单（无新解析逻辑），且更贴合 buffered 模式的设计前提"thinking 阶段客户端本就什么都看不到"。
- **B2 的残余**（评审后按外部反馈收窄）：初版把 min/h 锚点定在"整点"，`every: 2h` 这类 N≥2 的小时窗在**任意跨小时**热重载都会相移清零一次（实测确认）。改为锚点定在午夜后，残余收窄为 `every: Nh` 且 N∤24（如 `5h`）或 `every: Nmin` 且 N∤1440（如 `7min`），**且**热重载/重启跨过自然日——此时至多一次相移重置。已在代码注释、`since` 表格行、"已评估并否决的改进提案"表 B2 条目、KNOWN_ISSUES §46 写明。未采用"把 `CalculatedSince` 随桶持久化进 `vmr-quota.json`"的方案——那是为一个已收窄到"跨日重启 + 奇数 N"的残余引入一个持久化 schema 变更，不划算。
- **B7 未加确定性测试**：该竞态窄且自愈，`rt.Quota` 是具体类型无 stub seam，按评审"无干净 seam 则注释 + 现有测试保持绿"的建议处理。
- 未触碰评审 §5.5 更宽的文档清单（CLAUDE.md "co-equal" 改写、`core.go` admission rule、Core §6.6 stale、实体抽取描述等）——那些不属于 B1–B9。

### 9.6 确定不做（本轮明确否决，附理由）

| 项 | 来源 | 裁决与理由 |
|---|---|---|
| **B13 imgprep 32-bit 整数溢出** | 评审 §2.1 / §5.1 | **确定不做**。`processImage` 里 `cfg.Width*cfg.Height > maxDecodePixels` 的乘积在 32-bit `int` 平台可被声明尺寸溢出、绕过解压炸弹守卫；但 Go 的 `image/png` 解码器已把宽高钳在 `int32` 上限，64-bit（唯一 CI/目标平台，同 `disk_windows.go` 桩的口径）乘积不可能溢出。为一个无目标平台受影响的理论缺陷改代码不划算。已登记进 KNOWN_ISSUES §1——若 32-bit 成为目标平台，改成 `int64(cfg.Width)*int64(cfg.Height)` 即可。 |
| **B2 残余：把生效锚点 `CalculatedSince` 随桶持久化进 `vmr-quota.json`** | 外部反馈 2.1 | **确定不做**。B2 修复（锚点对齐到午夜）已把残余收窄到"跨自然日重启/热重载 + `every` 的 N 不整除其单位的一日份额"这种窄场景，且至多一次相移重置、下一周期自愈。为此引入一个 `vmr-quota.json` 的 schema 字段 + 迁移逻辑，成本远大于收益；需要精确相位的 Limit 显式写 `since` 即可。 |
| **B3 池移除 `json.Marshal(s.Rec)`** | 外部反馈 1.3 | **确定不做**。当前校验池逐字对齐评审要求移植的 `_eval/calibrate_p1b.go` 参考实现。核实 `chatmsg.RenderContent`（经 `Step.NewEvents`）已无截断地覆盖 tool_result 正文，`json.Marshal` 主要是冗余 + 元数据；移除它换来的是极少的元数据串误放行 ↓、但极少的漏放行 ↑ + 更多代码，在 0 用户、评审已建议考虑冻结的可选功能上不划算。 |
| **转义 `SessionRow.Alias`** | 外部反馈 1.2 | **确定不做（非真实问题）**。`Alias` 恒为 `fmt.Sprintf("s%02d", i+1)`，`ID` 恒为 `l-<hex>`，均机器生成、字符集固定、零用户内容。转义它防的是不存在的注入面。 |
| **B3 的替代"直接冻结 LLM 层（S4）"** | 评审 §5.1 | 未采纳（选择"修"）。S4 本身（把 6 检测器缩到 2 个、其余移 `_eval/`）仍是一个独立的简化建议，本轮未做，留待后续。 |

---

## 10. 执行情况记录（§5.2 / §5.4 / E1 / E2，2026-08-28 第二轮，Sonnet 5）

> 范围：§5.2 DX 的 3×P0、§5.4 的 2×P1、§6 的 E1 / E2。**E3 本轮 hold**（用户决定，§10.6）。
> 其余（B10–B14、§5.3 S1–S12、E4–E9、§7 分发动作、§5.5 剩余项）**未做**。
> 每个阶段一个 commit，落在分支 `feat/review-dx-perf-ext`（其父是已 ff-merge 进 main 的 B1–B9）。

### 10.1 执行方式

先通读上下文（CLAUDE.md、4 份 v4 设计文档相关章节、KNOWN_ISSUES、涉及源码），对 6 个任务项逐条回到源码复核根因与方案是否成立、有无改进空间——结论：方向全部成立，其中
- E1 按用户选择做**完整瀑布 UI（仅 `-journey` 单命中）**，不是 MVP 样式化 HTML；
- E2 按用户提议做**虚拟模型级 + endpoint 级两级开关**（评估：与既有 `capabilities` / `fallback` 的"模型基线 + endpoint 覆盖"惯例一致，合理，采用），并沿用评审已收窄的判据（非 SSE + 标记命中 AND 有效文本极短）+ 复用 `ErrContent`；
- E3 hold（用户在澄清阶段判断闸/桶两模式已按周期长短自动判断，本轮不做）。

开工前就 4 点向用户确认并获答复：E1 形态、E3 hold、E2 两级开关、提交粒度（先把 B1–B9 分支合并 main、再开新分支、每阶段一 commit）。

### 10.2 §5.2 DX —— commit `feat(dx): minimal config, readable missing-key failure, diagnose in quickstart`

| 项 | 改动 | 测试 |
|---|---|---|
| `config.minimal.yaml`（+ `.zh`） | 新增 ~15 行模板（1 provider `deepseek` + `coding`/`claude` 两个虚拟模型，覆盖 OpenAI 与 Anthropic 两个 ingress）；`config.example.yaml`/`.zh` 顶部加一行指路；README/`.zh` Quick Start step 2 改 `cp config.minimal.yaml` + `export DEEPSEEK_API_KEY` | `TestLoad_RepoMinimalConfig_Parses`（两个文件都 load/validate，防 bit-rot——B16 教训） |
| 缺 key 可读 | `expandEnv` 改签名返回展开为空的 `${VAR}` 名列表 → `Config.EmptyEnvRefs`；`cmd_start` 的 `logConfigCheckIssues` 重写：`SeverityWarning` 仍是一行安静 WARN，`SeverityError`（缺 api_key）或有空 `${VAR}` → 打带框 `CONFIG PROBLEMS` banner 逐条列出；`router.Serve` 早期新增 `rejectIfAllKeyless`——某虚拟模型 `route.Endpoints` 全部 `APIKey==""` 时直接回 `vmr_no_api_key` 503（点名模型 + endpoint 数），不再让每 attempt 401 上游 + 10min 冷却 | `TestParseTracksEmptyEnvRefs`、`TestLogConfigCheckIssuesBanners{Errors,EmptyEnvRefs}`、`TestLogConfigCheckIssuesWarningStaysQuiet`、`TestServe_AllEndpointsKeyless`（断言上游 0 hits） |
| README 加 diagnose | Quick Start 新增 **Verify** 步骤（`vmr diagnose`）介于 Run 与 Connect 之间，步骤重编号；`.zh` 同步 | 纯文档 |

文档同步：`docs/UserGuide.md`/`.zh` 的"启动与热重载检查"段重写（banner + 空 env 名 + 运行期 `vmr_no_api_key`）；`CHANGELOG.md [Unreleased]`。
`router.Serve` 因新增调用触及 archtest 函数长度预算（190 行豁免），已把 keyless 检查整体抽成 `rejectIfAllKeyless` 方法 + 压掉一个空行落回 190，未抬预算数字。

### 10.3 §5.4 —— commit `perf(analyze): compact blob-lineage index; warn on invisible streaming usage`

| 项 | 改动 | 测试 |
|---|---|---|
| analyze 内存（最小动作） | `ctxgraph/stitch.go` `buildBlobLineageIndex`：`map[Hash]map[int]bool` → `map[Hash][]int`（每个 hash 一条 posting list，去掉数百万个单元素小 map 的 header + bucket 开销）。去重不用 set：外层按 lineage 迭代、同一 `l.Idx` 的 append 连续，"末元素相等则跳过"即可；消费者 `resolveStitch` 只 `for _, idx := range`（它另建 overlap map，顺序无关） | `TestBuildBlobLineageIndex_DedupsWithinLineage`（同一 lineage 多个 manifest 重复 hash 只 post 一次；每个 hash 的 list 恰为持有它的不同 lineage） |
| `include_usage` 可见性 | `config.Check()` 新增 `checkQuotaUsageVisibility`：某 provider 有 `metric: tokens`/`cost` limit 且在任一 `openai-completions` endpoint 上 → `SeverityWarning`（流式响应无 usage 块除非客户端发 `stream_options.include_usage:true`，vmr 不注入；预期 `estimated_pct` 贴近 100）。`vmr status`（`cmd_status_render.go`）与 `vmr report`（`section_provider.go` + `i18n/report_provider.go` 新增 `IncludeUsageFootnote` EN/ZH）在 `estimated_pct ≥ 95%` 且 metric∈{tokens,cost} 时追加同因提示 | `TestCheckWarnsOnStreamingUsageInvisibility`（含 `HasErrors` 不受影响断言）、`TestCheckNoUsageWarningForAnthropic` |

文档同步：`KNOWN_ISSUES §1.2` 触发点按 `analyze` 组合路径重标定为 ~2 万条 + 补记 blob 索引收窄；`KNOWN_ISSUES §3.47`；`UserGuide.md`/`.zh` 额度段新增 "include_usage gap" 说明；`CHANGELOG.md`。
`stitch.go` 的 `map[Hash]map[int]bool` 表示是实现细节，未改 Analytics 设计文档（该文档只讲"倒排索引"这一层原理，不描述内部数据结构——符合 CLAUDE.md"principles not implementation detail"）。

### 10.4 E2 软屏蔽 → failover —— commit `feat(router): opt-in soft-block failover`

| 层 | 改动 |
|---|---|
| config | `VirtualModel.SoftBlockFailover *bool` + `EndpointGroup.SoftBlockFailover *bool`（缺省 nil = 关；endpoint 显式值覆盖模型级默认——与 `capabilities`/`fallback` 同款惯例） |
| core / snapshot | `core.Endpoint.SoftBlockFailover bool`；`buildEndpoints` 解析 `eg.SoftBlockFailover ?? m.SoftBlockFailover ?? false` |
| respnorm | `ContainsSoftBlockMarker([]byte) bool` 导出（包内已有 `containsSoftBlockMarker`——MiniMax `"input_sensitive":true` / `"output_sensitive":true` 字节子串） |
| adapter | 新增 `internal/adapter/response.go` `ResponseAssistantText(protocol, body) (textRunes int, hasToolCall bool, ok bool)`——按 3 协议解析非流式 2xx body 的助手文本 rune 数 + 是否含 tool_call（`chatmsg` 属分析半区不能引，故放 adapter 协议域） |
| router | 新增 `internal/router/softblock.go` `checkSoftBlock`：`tryOne` 在 2xx 且 `ep.SoftBlockFailover` 且非 SSE、非压缩时预读到 `softBlockPeekCap=64KB`——超过即断定非空屏蔽、恢复流式转发（`io.MultiReader`）；读满且 `ContainsSoftBlockMarker` && `ResponseAssistantText` 判定 `ok && !hasToolCall && textRunes ≤ 64` → 按 `handleErrorResponse` 的 `ErrContent` 分支处理（`ReportNeutral`、attempt 记 `content` 类、零冷却、`return done=false, uerr={200,hdr,body}`）。全候选耗尽时客户端原样收到最后一次响应（与 `ErrContent` 一致） |

测试：`adapter/response_test.go`（3 协议 × 空/真实/tool_call/未知协议/不可解析/错误形状）、`router_serve_test.go` 的 `TestServe_SoftBlock{FailsOverWhenOptedIn,ForwardedWhenNotOptedIn,RealAnswerNotFailedOver,EndpointOptOutOverridesModel}`。
`router.go` 因新方法触及文件行数预算（700），已把 `checkSoftBlock` + 两个常量 + `readCloser` 整体放独立文件 `softblock.go`。
文档同步：`config.example.yaml`/`.zh`（`soft_block_failover` 注释，模型级 + endpoint 级两处）；Core 设计文档 §5.3 新增"可选：`soft_block_failover`"段 + §13.1 路线图措辞更新；`UserGuide.md`/`.zh` failover 段新增说明；`KNOWN_ISSUES §3.47`；`CHANGELOG.md`。

### 10.5 E1 HTML 瀑布 + 脱敏 —— commit `feat(analyze): single-file HTML journey renderer with redaction`

| 层 | 改动 |
|---|---|
| 渲染器 | 新增 `internal/story/render_html.go`（`RenderHTML(j, m, findings, lang, redact) string`——纯 view over 同一个 `*story.Journey`，不重解析、不新增判断）+ `render_html_assets.go`（内联 CSS + 一段内联 JS：`IntersectionObserver` 做时间轴滚动高亮；theme-aware `prefers-color-scheme`；零外部请求）。左侧粘性时间轴（Task/Step 锚点导航）+ 右侧 Step 卡片瀑布：卡片含 Step 号 / 时间 / 模型 / failover 徽章 + endpoint、指令、transition 标记（Edit / StitchEdge / SysChanged / Compaction 信息损失摘要含 swallowed/survived 实体）、"本轮进入上下文"（NewEvents 中非 assistant 角色）、"模型响应"（Reasoning 折叠 / RespText / ToolCalls 名+args 折叠 + 配对的 tool result / NoReply） |
| i18n | 新增 `internal/i18n/story_html.go`（`StoryHTMLText` + `StoryHTML(lang)`，EN/ZH 全部固定串） |
| CLI | `cmd_analyze.go` 新增 `-html` / `-redact` flag + `analyzeRun.htmlOn/redactOn`；校验：`-redact` 需 `-html`；`-html`/`-redact` 需 `-journey`；多命中 `-journey` + `-html` 报错（与 `-llm-addr` 同款"一次一个 Journey"约束）。`cmd_story.go` `renderJourney` 加 `htmlOn, redactOn` 参数，`writeJourneyFile` 之后写 `stories/journey-<id>.html`（`j.Partial` 时 `-partial.html`），**0600**（未脱敏版仍含完整正文） |
| 脱敏 | `-redact` 时 `bodyText`/`jsonBody` 把每段正文（指令、消息、RespText、Reasoning、tool args/results、标题）替换为 `‹text: N chars›` / `‹json: N chars›`（N = rune 数）；角色、token 数、工具名、时间、compaction/stitch 标记全保留 |

测试：`render_html_test.go`——`TestRenderHTML_Structure`（关键结构串 + 无外部资源引用断言）、`TestRenderHTML_RedactLeaksNothing`（构造含 `SECRET-LEAK`/`TOKEN=abc` 的合成 journey，断言脱敏输出零泄漏 + 结构存活 + 无 `<pre class="text/json">`）、`TestRenderHTML_ZHChrome`。另在 `examples/sample-audit.jsonl` 上端到端跑通、HTML5 解析器验证嵌套无误、给用户发了完整版 + 脱敏版预览。
文档同步：Analytics 设计文档 §8 对应条目从"尚未实现"改为已实现并写清用法；`UserGuide.md`/`.zh` 的 journey 渲染段 + CLI 参考表加 `-html`/`-redact`；`KNOWN_ISSUES §3.47`；`CHANGELOG.md`。默认套件**不**出 HTML index（用户选"仅 -journey"）。

### 10.6 遗留与留给用户判断

- **E3（per-virtual-model 预算硬闸）本轮 hold**——用户在澄清阶段的判断是"闸/桶两种模式我们之前的方案里已经有了，只是按周期长短自动判断，先 hold"。
  一点值得用户后续判断的差异：**quota 的 gate/bucket 是"配速"——它从不拒绝请求，只在同优先级梯队内重排端点**（Quota 文档"它不会做的事"节自陈：额度耗尽不触发降级、不拒绝，那是 health 的职责）。E3 想要的是**硬急停**——一个进死循环的 agent 触顶后请求被**明确拒绝**（可解析错误，绝不静默降级），每日零点 + 进程重启重置、无持久化。两者目标不同：配速降低"跑爆某个套餐"的概率，硬闸给"一夜烧光"设一个确定性上限。若未来要后者，仍需一个独立的内存态机制（仿 health 注册表，请求入口查一次），不是把 quota 的旋钮拧一下能得到的。**此判断留给用户**。
- **E2 的行为变更**：仅对**显式开启 `soft_block_failover` 的端点**生效，默认关。开启后非流式软屏蔽响应从"客户端收到干净空 200"变为"failover 到下一候选；全失败则收到最后一次响应"。attempt 审计记 `content` 类（一个 200 被归类为内容拦截）——分析半区 `error_classes` 分桶会看到，是刻意的（评审要这类拦截可见）。
- **E1 未覆盖**：默认套件不出 HTML index；`-compare` / `-corpus` 无 HTML；`-redact` 目前只作用于 `-html`（不脱敏 Markdown）。这些都是本轮范围的刻意边界，不是缺陷。
- **未触碰**：§5.5 更宽的文档清单（CLAUDE.md "co-equal" 改写、`core.go` admission rule、Core §6.6 stale 等）、§5.3 的任何简化/删减建议、B10–B14。

### 10.7 验收结果

- `go build ./...`、`go vet ./...`、`gofmt -l internal cmd`（无输出）全部通过。
- `go test ./...` 全绿（35 个包 ok，0 FAIL）。
- `go test -race`（router / config / adapter / ctxgraph / story / i18n / report / cmd/vmr）全绿。
- `go test ./internal/archtest/...` 全绿（`router.go` 文件预算与 `Serve` 函数预算两处触线，均以拆文件 / 拆函数解决，未抬预算数字）。
- `vmr check` 对 `config.example.yaml` / `.zh` / `config.minimal.yaml` / `.zh` 四个文件均校验通过。
- E1 在 `examples/sample-audit.jsonl` 上端到端生成完整版 + 脱敏版 HTML，脱敏版经断言确认零对话正文泄漏。
