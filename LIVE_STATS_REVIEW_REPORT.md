# Live Stats 数据端点与监控页面呈现逐项 Review 分析报告

---

## 1. 任务 Debrief (任务背景与概要)

### 1.1 任务背景
在 vmr（Virtual Model Router）系统的架构设计中，本地单二进制路由系统不仅承担大模型请求的高可用路由、故障转移与模型虚拟化分发，同时提供内置的实时监控（Live Stats）与运维控制台能力。该能力通过 `GET /status`、`GET /stats` 等数据端点暴露当前进程与持久化账本（Ledger）的聚合数据，并在前端网页（主要是 `http://192.168.0.22:8800/status.html` 的 Overview 仪表盘，以及 `log.html` 中的实时监控组件）上呈现。

近期在实际运行与观测中发现，部分核心指标的底层计算逻辑、定义取值范围以及前端页面的呈现展示逻辑、Label 命名存在偏离工程直觉和通用心智模型的问题。典型表现如：
1. **分位数计算方向错位**：在性能监控中，速率（Throughput，如 `Tok OUT/s`）与耗时（Latency，如 `TTFT`）的优劣方向相反，但代码均使用单一升序排序直接取 `p90`，导致展示的数值实际代表“90% 的请求速率都比该值更慢（仅有 10% 的极速请求达到该速率）”，无法反映 SLA 保障底线或长尾卡顿恶劣体验；
2. **Token 指标展示割裂且不符合通用习惯**：在各核心数据表格中普遍采用 `Tok in+cw / cr`（将 Fresh Input 与 Cache Write 捆绑、Cache Read 独立展示），迫使用户人脑计算总 Prompt 量与缓存命中率，而页面顶部的 Vitals 卡片却已经采用了更人性化的 `tok in X (hit%), out Y` 规范，页面内认知模型前后矛盾；
3. **术语与口径定义模糊**：例如 Headroom 描述与计算公式语义倒置、Vitals Strip 中的 `total` 实际仅为 8 天持久化账本总量而非全量等。

### 1.2 任务目标与原则
- **目标**：对当前 Live Stats 相关的所有数据端点、计算逻辑、数据结构以及前端页面呈现的 Label、数值、交互和 Tooltip 进行全面、细致、逐项的审查（Review）。先拉齐完整清单，再逐项深度核对，指出不合理之处，深挖根因，并给出建设性的重构与改进建议。
- **核心原则**：
  - **只做 Review 与分析**：本轮不修改任何已有代码和文档，不提交 git commit；
  - **事实与数据说话**：基于当前正在运行的生产实例（`http://192.168.0.22:8800`）真实输出数据及源代码进行比对验证；
  - **ROI 驱动分级**：依据改造代价与收益比（ROI）对发现的问题进行科学梯队划分（建议优先改、建议改但不急、建议暂缓或搁置）；
  - **成果沉淀**：本报告作为分析与改进的唯一事实依据沉淀在工作区。

---

## 2. 简要执行计划 (Execution Plan)

整个 Review 任务分为五个标准阶段推进：

| 阶段 | 阶段名称 | 核心目标与动作 |
| :--- | :--- | :--- |
| **Step 1** | **端点与页面代码映射** | 梳理后端 `internal/server` 与 `internal/livestats` 的路由定义、数据结构定义、前端 `status.html`、`log.html` 模板及 `console.js` 共享脚本，建立“数据生产 -> API 序列化 -> 前端消费 -> 页面渲染”的全链路拓扑图。 |
| **Step 2** | **在线实例真实数据采样** | 从在线实例（`http://192.168.0.22:8800`）抓取 `/status` 与 `/stats?range=24h` 的真实 JSON 报文，提取真实环境下的并发、配额、端点健康、各模型延迟与吞吐量、时序分布等数据进行画像分析。 |
| **Step 3** | **全景指标清单梳理与逐项核对** | 按照页面功能板块拆解指标，拉出包含 7 大模块、30 余项具体数据项的完整核对清单；针对每一项核对：来源字段、计算公式、物理含义、取值范围、页面呈现形态与 Label 准确性。 |
| **Step 4** | **深度问题归因与修改方案推演** | 针对发现的问题进行分类下钻，尤其是性能分位数、Token 呈现维度、Headroom 表达、时区一致性、无用轮询等重点问题，给出根因分析及修改方案。 |
| **Step 5** | **ROI 评估与分批分类总结** | 建立问题矩阵，按“P0 高 ROI（立竿见影/低风险）”、“P1 中 ROI（体验升级/重构建议）”、“P2 低 ROI（边缘优化/可搁置）”进行分类，形成结构化报告。 |

---

## 3. 执行过程记录 (Execution Log & Progress)

### 3.1 Step 1：端点与页面代码拓扑梳理
- **执行情况**：
  - 检查了 `internal/server/server.go`：Live 状态相关的 HTTP 路由包括：
    - `GET /status`（JSON，由 `s.adminStatus` 提供）
    - `GET /stats`（JSON，由 `s.adminStats` 提供，支持 `?range=24h|3d|7d`）
    - `GET /status.html`（HTML，由 `s.statusPage` 提供，Overview 核心仪表盘）
    - `GET /log.html`（HTML，包含实时请求与最近失败两项常驻组件）
    - `GET /health`（极简存活探针）
  - 检查了 `internal/livestats` 包职责划分：
    - `livestats.go`：定义 `Sample` 结构、`TokenCounts`、`ringCap=100`、`rollupRetentionDays=7`；
    - `ring.go`：环形缓冲区管理，`nearestRankInt`、`nearestRankFloat` 分位数计算，`toksOf` 速率计算；
    - `hour.go` 与 `snapshot.go`：负责小时桶（`HourlyRow`）与本地日（`Daily`）聚合，生成 `by_provider_model`、`by_client_key_tag`、`by_key_label` 及 `overall`。
  - 检查了 `internal/server/status.html`：
    - 页面包含 5 大区域：Vitals Strip、Quota Budgets、Virtual Models & Endpoint Topology、Performance by Provider & Model、Traffic & Usage；
    - 发现历史重构痕迹：Live Requests 和 Recent Failures 已经迁移至 `log.html`，但 `status.html` 中仍残留部分空置状态变量与高频轮询代码。

### 3.2 Step 2：真实流量数据抓取与画像诊断
- **执行情况**：
  - 对 `http://192.168.0.22:8800` 进行抓取，成功获取实时报文：
    - `/status`（大小 27KB）：包含 4 个虚拟模型、13 个端点（含 4 个处于连续失败 cooldown 状态）、13 个配额规则（涉及请求数与 Token 周期桶）、系统与审计元数据；
    - `/stats?range=24h`（大小 147KB）：包含 204 条 hourly 记录、89 条 daily 记录、100 条 recent_errors、13 组 provider_model 性能统计及 1 条 overall 汇总。
  - 真实样本关键发现验证：
    - 验证了用户举例 1：`bai_free-key_1 : glm-5.3-flash` 的 `p50_toks = 39.23`，`p90_toks = 64.21`。
    - 发现了极端速率异常值：`cliproxy : gemini-3.8-flash-high` 的 `p50_toks = 1571.4`，`p90_toks = 6083.3`！经查是因为极短流式生成（`dur_ms - ttft_ms` 仅几毫秒）导致瞬时除法结果被严重放大并被升序 p90 捕获；
    - 验证了用户举例 2：在真实报文中，三个核心表格全都是 `in+cw: 615967, cr: 8373888` 这种拆分形式；
    - 验证了浮点精度瑕疵：`volc_coding_plan` 的 `used` 值为 `11479.499999999`。

### 3.3 Step 3：全景指标核对（详见第 4 节）
- 完成对 7 个功能模块、32 个细分指标的端到端穿透审查。

### 3.4 Step 4 & 5：归因与分级评估（详见第 5、6 节）
- 完成深层次数学逻辑与认知模型分析，输出 P0、P1、P2 梯度优化策略与 ROI 评估。

---

## 4. Live Stats 全项数据逐项核对清单 (Complete Inventory & Detailed Review)

本节将所有在 Live Stats 相关端点及页面上出现的数据项逐一列出，进行数据来源、计算公式、页面呈现、合理性与缺陷的深度核对。

```
========================================================================================
全景审查指标清单总览：
【模块 1】Header 与 System 底栏事实行 (Uptime, Memory, Disk, Audit, ImageCache, Alerts)
【模块 2】Vitals Strip 核心指标横幅 (Concurrency, Requests, Tokens, Endpoints)
【模块 3】Quota Budgets 配额预算表格 (Provider, Limit, Models, Amount, Used, Progress, Headroom, Req 24h, Resets)
【模块 4】Virtual Models & Endpoint Topology 表格 (Virtual Model, PRI, Provider:Model, Health, Headroom, Req 24h, Context/Caps)
【模块 5】Performance by Provider & Model 表格 (Requests, Tok in+cw/cr, Tok out, TTFT p50/p90, Tok OUT/s p50/p90)
【模块 6】Traffic & Usage 图表与用量表格 (SVG Chart, Usage by Provider/Model, Usage by Caller)
【模块 7】Live 控制台常驻组件 (Live Requests 运行表, Recent Failures 失败表)
========================================================================================
```

---

### 4.1 模块 1：Header 与 System 底栏事实行 (System Facts)

#### 项 1.1：`Uptime` (运行时间)
- **数据来源**：`status.instance.started_at`, `uptime_seconds`, `uptime`
- **计算逻辑**：后端 `time.Since(s.inst.startedAt)`，由 `fmtutil.FmtDuration` 格式化为 `19h 17m 33s`。前端备用 `fmtDur(uptime_seconds)`。
- **页面呈现**：顶部 Header 右侧 `up 19h 17m 33s`。
- **合理性与 Label 核对**：
  - **合理**。计算与展示清晰，容错机制完整。

#### 项 1.2：`Alerts` (告警横幅)
- **数据来源**：`status.alerts[]` (`severity`, `kind`, `message`, `ref`)
- **计算逻辑**：由 `internal/server/alerts.go` 汇总配置告警（如无鉴权开放监听、引用的 Provider 被禁用）、端点告警（连续失败进入 cooldown）、配额告警（预算耗尽）。
- **页面呈现**：顶部折叠/展开横幅（带计数与警告色）。
- **合理性与 Label 核对**：
  - **合理**。信息分级与交互正常。

#### 项 1.3：`Memory (heap / sys)` (内存用量)
- **数据来源**：`status.system.memory.heap_alloc`, `sys`
- **计算逻辑**：后端调用 `runtime.ReadMemStats`，通过 `fmtutil.FmtBytes` 转换为人类可读文本。
- **页面呈现**：Sysline 底栏 `heap 4.8MB / sys 91.4MB · 13 goroutines`。
- **合理性与 Label 核对**：
  - **合理**。展示了实际堆占用与系统保留内存，符合 Go 运维常识。

#### 项 1.4：`Disk Free` (日志磁盘可用空间)
- **数据来源**：`status.system.disk.free_space`
- **计算逻辑**：探测 `snap.Cfg.LogDir` 所在挂载盘的可用字节数（30s 读取缓存）。
- **页面呈现**：Sysline 底栏 `disk free 196.6GB`。
- **合理性与 Label 核对**：
  - **合理**。为日志落盘提供前置磁盘耗尽预警。

#### 项 1.5：`Audit & Image Cache` (审计与图片缓存状态)
- **数据来源**：`status.audit` 与 `status.image_cache`
- **计算逻辑**：读取活跃文件大小、总审计大小、保留天数、图片缓存当前大小与容量上限。
- **页面呈现**：`audit on · 1.5GB · 90d`，`image cache on · 536.7KB`。
- **合理性与 Label 核对**：
  - **合理**。指标真实准确。

---

### 4.2 模块 2：Vitals Strip 核心指标横幅卡片

#### 项 2.1：`Concurrency · now` (瞬时并发)
- **数据来源**：`stats.concurrency.in_flight`, `limit`, `waiting`（或回退到 `status.instance.concurrency`）
- **计算逻辑**：由 `router.Concurrency()` 实时原子读取，每次 `/stats` 调用现算，不进入缓存。
- **页面呈现**：
  - 主文本：`0 / 6`（或 `0 / ∞`）；
  - 副文本：`<span class="t-accent">0</span> queued`，当 waiting > 0 时黄色高亮。
- **合理性与 Label 核对**：
  - **基本合理**。但 Label 写为 `Concurrency · now`，主数值为 `in_flight / limit`。当 limit 未配置时显示 `0 / ∞`，普通用户可能会对“∞ 并发”产生疑问（实际为不限流），可优化 Tooltip。

#### 项 2.2：`Requests · today` (今日请求概况与全量对比)
- **数据来源**：`stats.daily[]` 或回退到 `status.traffic.requests`
- **计算逻辑**：
  - 前端 `sumDaily()` 匹配日期为服务端当日（`serverDay(statusData.current_time)`）的小时行，累加 `ok + canceled + error` 得出 `todayReq`；
  - 小字累加 `stats.daily[]` 中所有的请求得出 `totalReq`；
  - 错误率计算：`todayErr / todayReq * 100`。
- **页面呈现**：
  - 主数值：`420 / 3,014 total`；
  - 副数值：`400 ok  12 cancel  8 err · 1.90%`。
- **合理性与 Label 严重问题审查**：
  - **【重大歧义】`... total` 语义严重误导**：
    - 小字显示 `/ 3,014 total`，hover title 是 `requests recorded in the stats ledger (durable, ~last 8 days — survives restarts)`。
    - **问题**：在 UI 界面上直接展示为 `total`，用户 100% 会误认为是“服务创建以来的历史全量请求”！实际上 `livestats` 在设计上只保留 **8 天** 的 daily ledger。如果服务连续运行了 3 个月，总请求有 100 万，这里的 total 却永远只显示最近 8 天的 3000 次！
    - 此外，如果 ledger 为空时，它会退回到 `traffic.requests.total`（这是进程内存计数，重启就归零）。两种完全不同生命周期的数据源塞给同一个 `total` Label，极具欺骗性。

#### 项 2.3：`Tokens · today` (今日 Token 用量与全量对比)
- **数据来源**：`stats.daily[]` 或回退到 `status.traffic.tokens`
- **计算逻辑**：
  - 今日总 Token：`todayIn + todayCw + todayCr + todayOut`；
  - 今日 Prompt Token：`todayIn + todayCw + todayCr`；
  - 今日缓存命中率：`todayCr / todayPromptTotal * 100`；
  - 今日输出 Token：`todayOut`。
- **页面呈现**：
  - 主数值：`32.5M / 274.9M total`；
  - 副数值：`tok in 31.8M (90.2%), out 710K`。
- **合理性与 Label 审查**：
  - **正面范例**：副数值写为 `tok in 31.8M (90.2%), out 710K`，清晰直观地将输入总规模与缓存命中率并列表达，极度符合直觉！
  - **反差问题**：此处的优秀表达，与下文 Performance 和 Usage 表格中的 `Tok in+cw / cr` 产生了全站心智割裂；同时主数值的 `/ ... total` 同样存在上述“8天假全量”问题。

#### 项 2.4：`Endpoints · now` (端点健康度)
- **数据来源**：`status.models[].endpoints[]`
- **计算逻辑**：遍历去重端点，比对 `cooldown_until` 与当前时间戳，以及 `consecutive_failures`。
- **页面呈现**：
  - 主数值：`18 / 20 healthy`；
  - 副数值：`all endpoints healthy`，或者 `X half-open  Y cooldown`。
- **合理性与 Label 核对**：
  - **合理**。统计清晰，且对多虚拟模型共用同一上游端点的情况做了正确的 Set 去重。

---

### 4.3 模块 3：Quota Budgets (配额预算表格)

#### 项 3.1：`Provider` 列
- **数据**：配额关联的 Provider 标识。
- **呈现**：通过正则去除内部 key 后缀，还原为实际账号名。
- **合理性**：合理。

#### 项 3.2：`Limit` 列
- **数据**：`q.role` (`bucket` 或 `gate`)、`q.metric` (`requests` 或 `tokens`)、`q.every` (`5h`, `1d`, `1mo` 等)。
- **呈现**：Badge 形式：`bucket · requests / 5h` 或 `gate · requests / 1min`。
- **合理性与 Label 问题审查**：
  - **术语偏内部化**：`bucket` 与 `gate` 是 VMR 内部的设计词汇（bucket 代表长周期配额桶，未使用会浪费因此 headroom 高时提高优先级；gate 代表短周期防被封速率闸门，接近打满时降低优先级）。对于普通运维来说，仅凭这两个词无法理解其调度影响，建议在 Tooltip 中强化中文或更通俗的释义。

#### 项 3.3：`Models` 列
- **数据**：`q.models` 数组。
- **呈现**：如果为空显示 `all models`，有值则显示彩色 `modelSpan`。
- **合理性**：合理。

#### 项 3.4：`Amount` 与 `Used` 列
- **数据**：`q.amount` 与 `q.used`。
- **呈现**：若是请求数用 `fmtInt`，若是 Token 用 `fmtKMG`。
- **合理性与数据精度问题审查**：
  - **【底层浮点精度瑕疵】**：对于火山等具有 `model_multipliers` 的计划，消耗量按权重累加浮点数，如前面抓取到的真实数据：`"amount": 14000, "used": 11479.499999999`。JSON 数据未在后端取整或安全舍入，直接暴露出 `.499999999` 的 IEEE 754 精度漂移，在某些未过滤的 Tooltip 中会显现。

#### 项 3.5：`Progress` 列 (进度条与使用率)
- **数据**：`q.pct`（或 `used / amount * 100`）。
- **呈现**：进度条（正常蓝色，>=60% 黄色，>=90% 红色），下方提示 `${pct}% used`，危险时追加 `· ${left} left`。
- **合理性**：合理，警示阈值设置得当。

#### 项 3.6：`Headroom` 列 (配额步调裕度)
- **数据**：`q.headroom`。
- **定义公式**：`headroom = (1 - used_frac) / time_left_frac`，截断至 `[0, 5]`。
- **呈现**：`fmtHeadroom` 格式化为 2 位小数，根据 `<=0`(红)、`<1`(黄)、`>=1`(绿) 赋色。
- **合理性与说明文案严重倒置审查**：
  - **【核心概念与说明倒置】**：
    - 表头描述写着：`headroom = pace of spend vs pace of time; 1.00 is exactly on pace`；
    - **事实分析**：如果花得很快（pace of spend 很高），剩余配额很少，`used_frac` 很大，`1 - used_frac` 很小，Headroom 算出来是 **很小的数（趋向于 0）**！如果花得很慢，Headroom 是 **很大的数（例如 2.86）**！
    - 所以它根本不是 `pace of spend vs pace of time`（如果是 spend/time，花得越快比值应该越大！），它的物理本质是 **“剩余配额比例 vs 剩余时间比例”**（Remaining Budget Pace vs Remaining Time Pace）！
    - 原文将比值关系彻底说反，导致任何初次查看的用户无法理解为什么“花费越快，数值反而越低”。

#### 项 3.7：`Req 24h` 列 (24小时配额承载量及全网占比)
- **数据**：聚合最近 24 小时属于该 Provider+Model 的请求数，除以全网 24 小时总转发请求数。
- **呈现**：`39 · 1.30%`。
- **合理性**：合理，反映该配额账户在全网流量中的实际承载比例。

#### 项 3.8：`Resets` 列 (周期重置点)
- **数据**：`q.period_ends_at`。
- **呈现**：当天显示 `16:00`，跨天显示 `09-13 00:00`，title 显示倒计时 `in 3h45m`。
- **合理性**：合理。

---

### 4.4 模块 4：Virtual Models & Endpoint Topology (虚拟模型与端点拓扑表格)

#### 项 4.1：`Virtual Model` 列
- **数据**：虚拟模型 ID 与协议。
- **呈现**：跨行合并单元格（rowspan），第一行显示 `openai-completions:` 换行加粗 `agent`。
- **合理性**：合理，分组清晰。

#### 项 4.2：`PRI` 列 (优先级 Tier)
- **数据**：`ep.priority` 及 `ep.from_fallback`。
- **呈现**：普通端点为 `P0`, `P1`, `P2`，兜底端点为 `FB`。
- **合理性与样式 Bug 审查**：
  - **【前端三元表达式样式遗漏】**：
    - 代码为：`${ep.priority === 1 ? 'pri-1' : (ep.priority === 2 ? 'pri-2' : 'pri-n')}`；
    - 在实际生产配置中，最重要、最优先的主路由往往设置的是 `priority: 0`（即 P0）！
    - 但因为条件分支中未写 `priority === 0`，导致**最重要的 P0 端点被赋予了最暗淡无光的 `pri-n` 样式**，反而让次优的 P1、P2 拥有亮丽色彩，严重破坏视觉引导。

#### 项 4.3：`Provider : Model` 列
- **数据**：端点上游提供商与模型名。
- **呈现**：提供商名加冒号，模型名通过散列算法分配独有色相（`modelSpan`），全站同名同色。
- **合理性**：优秀设计，强化了跨表格识别效率。

#### 项 4.4：`Health` 列 (端点健康状态)
- **数据**：`ep.consecutive_failures` 与 `ep.cooldown_until`。
- **呈现**：
  - 正常：绿色 Badge `healthy`；
  - 冷却中：红色 Badge `cooldown 26m32s`，Tooltip 说明失败原因与连续失败次数；
  - 冷却到期半开：黄色 Badge `half-open`。
- **合理性**：非常严密且直观。

#### 项 4.5：`Req 24h` 列 (端点在虚拟模型下的分流占比)
- **数据**：该端点 24h 请求数，除以该**虚拟模型** 24h 总请求数。
- **呈现**：`400 · 85.0%`。
- **合理性**：与 Quota 表格不同，这里的分母限定在当前虚拟模型内，准确反映负载均衡或优先级分配效果。

#### 项 4.6：`Context / Capabilities` 列 (上下文与能力标签)
- **数据**：`ep.max_context_tokens` 与 `ep.capabilities` 数组。
- **呈现**：斜杠连接纯文本，例如：`512K/text/tools/thinking`。
- **合理性与展示排版问题审查**：
  - **【属性维度混淆与可读性差】**：
    - `512K` 是容量数值（Context Window）；
    - `text/tools/thinking` 是功能特性标签（Feature Capabilities）；
    - 用单斜杠生硬拼接在一起，既缺少视觉层级，又在长文本时造成换行杂乱，建议拆解为规格 Badge 与能力 Chip。

---

### 4.5 模块 5：Performance by Provider & Model (性能表格) —— 重点审查！

#### 项 5.1：`Requests` 列 (窗口采样有效样本数)
- **数据**：`wb.n`（窗口内的有效请求数，支持 last 10 / last 100 切换）。
- **呈现**：若样本未填满窗口容量（如不满 100），显示为黄色警告色并提示 `Window not full: this row has only X of Y samples`。
- **合理性**：合理，有效防范小样本下的统计偏差误导。

#### 项 5.2：`Tok in+cw / cr` 列 (输入 Token 结构) —— 【用户例子 2】
- **数据**：`incw = tokens.in + tokens.cache_write`，`cr = tokens.cache_read`。
- **呈现**：`616K / 8.37M`，表头写为 `Tok in+cw / cr`。
- **合理性与反人类设计严重审查**：
  - **【展示逻辑错位，认知成本极高】**：
    1. 用户无法直接一眼获知“总共输入了多少 Token”，必须人脑把左边的 `616K` 和右边的 `8.37M` 加在一起；
    2. 用户无法直接获知“Cache 命中率到底是多少”，必须人脑做除法 `8.37M / (616K + 8.37M) ≈ 93.1%`；
    3. `in+cw` 这个缩写对于多数开发者而言非常晦涩；
    4. 本页顶部的 Vitals Strip 已经证明了：用户真正需要的是 `Tok in (hit %)`（如 `8.99M (93.1%)`）。

#### 项 5.3：`Tok out` 列 (生成 Token 总量)
- **数据**：`wb.tokens.out`。
- **呈现**：`fmtKMG` 格式化，如 `49.7K`。
- **合理性**：合理。

#### 项 5.4：`TTFT p50 / p90` 列 (首字延迟分位数)
- **数据**：`wb.ttft_p50_ms` 与 `wb.ttft_p90_ms`。
- **计算逻辑**：对窗口内成功请求的 `ttft_ms`（剔除未测得的 0）升序排序，通过 `nearestRankInt(ttfts, 0.5)` 和 `0.9` 提取。
- **呈现**：`4.76s / 8.54s`（<1s 时显示毫秒如 `450ms`）。
- **合理性分析**：
  - **合理**。TTFT 是耗时/成本型指标，越小越好。升序排在第 90% 位置的较大数值，反映的是“90% 的请求首字响应均不慢于该值，仅有 10% 的极慢请求劣于该值”，属于经典的 SLA 尾部恶劣体验监控逻辑。

#### 项 5.5：`Tok OUT/s p50 / p90` 列 (生成吞吐率分位数) —— 【用户例子 1】
- **数据**：`wb.toks_p50` 与 `wb.toks_p90`。
- **底层计算源码（`internal/livestats/ring.go`）**：
  ```go
  slices.Sort(toks) // 升序排序
  wb.ToksP50 = nearestRankFloat(toks, 0.5)
  wb.ToksP90 = nearestRankFloat(toks, 0.9)
  ```
- **呈现形态**：`39.23 / 64.21`，表头为 `Tok OUT/s p50 / p90`。
- **合理性与计算逻辑方向错误深度审查**：
  - **【致命缺陷 1：分位数方向反转与用户直觉相悖】**：
    - 速率（Tok/s）是**收益/产出型指标（越大越好，越小越差）**！
    - 当升序排列并取 0.9 位置时，取出的其实是数组中前 10% 的**极快高分**！
    - **导致的结果是**：这里的 P90 数值反映的是“只有 10% 的请求能达到 64.21 tok/s 这么快，而 **90% 的请求速率都比它慢！**”
    - 然而在同一表格中，左边一列是 `TTFT p50 / p90`（右边数值大代表更差），右边一列是 `Tok OUT/s p50 / p90`（右边数值大代表更好）。用户以习惯性的 SLA 思维查看时，会误以为“我们系统有 90% 的概率能保证达到 64.21 tok/s 的生成速度”，造成严重的虚假乐观和性能误判！
    - 如果用户想看长尾恶劣体验（最慢的 10% 请求卡成了什么样），代码算出来的 P90 恰恰把最慢的 10% 彻底抛弃了，反而展示了最快的 10%！
  - **【致命缺陷 2：除法极小分母导致毛刺异常放大，直接污染 P90】**：
    - `toksOf` 计算公式：`spanMS = durMS - ttftMS`，`rate = tokens.Out / (spanMS / 1000)`。
    - 当某个流式请求的首包与尾包几乎同时到达（网络闪断后一次性推过来，或仅输出几个 token），`spanMS` 极小（如 2ms），计算出的速率瞬间达到 `5000 ~ 10000 tok/s`！
    - 在抓取的真实数据中：`cliproxy : gemini-3.8-flash-high` 的 `Tok OUT/s p50 / p90` 竟然高达 **`1571.4 / 6083.3`**！
    - 因为当前代码取的是升序第 90% 位（向极速倾斜），导致这种网络微突发造成的虚假极速噪点被精准捕获，直接将 P90 顶爆到离谱的 6083 tok/s！

---

### 4.6 模块 6：Traffic & Usage (流量与用量图表及用量表格)

#### 项 6.1：`Traffic Chart` (SVG 混合堆叠图表)
- **数据**：`stats.hourly[]`（根据当前选中的 24h / 3d / 7d 范围）。
- **图表组成**：
  - 左柱（输入）：fresh (蓝) + cache write (紫) + cache read (青) 堆叠；
  - 右柱（输出）：tok out (绿) 单柱；
  - 折线（请求）：requests 数量（黄色线，对应右 Y 轴）；
  - 错误打点：带错误的桶用红圈高亮。
- **合理性与时区/刻度问题审查**：
  - **【客户端时区与服务端日切冲突】**：
    - 前端 `bucketLabel` 使用 `d = new Date(...)` 和 `d.getHours()`，直接按**浏览器本地时区**计算横坐标文本；
    - 但服务端 `HourlyRow` 的数据桶是按服务端的 `time.Local` 划分的。如果跨时区访问，图表底部的时间戳与服务端日志记录无法对应。
  - **双 Y 轴遮挡**：当 Token 极大但请求较少，或请求极多但 Token 很少时，折线与柱状图容易重叠，缺少自动规避或分离轴模式。

#### 项 6.2：`Usage by Provider & Model` 与 `Usage by Caller` 表格 (用量明细与错误归属)
- **数据**：从 `stats.hourly[]` 按 `provider + model` 或 `client_key_tag` 聚合。
- **列**：`Req ok / err`, `Tok in+cw / cr`, `Tok out`, `Tok Share`。
- **合理性与严重缺陷审查**：
  - **【重大底层归因 Bug：具体模型 Error 全为 0，最后有一行 “-” 显示 “0/10”】**：
    - 现象：在线实例的 `Usage by Provider & Model` 表格中，所有有明确 Provider 和 Model name 的行，其 Error 计数器竟然**全都是 0**；但在表格最底部却出现了一行 Provider Model 为 `"—"` 的记录，Req 显示为 `0 / 10`（即 10 次错误全被算到了该行头上）！
    - 代码溯源：在 `internal/server/stats.go` 的 `sampleFromRecord` 中，仅当 `att.IsForwarded()` 为 true 时才会提取并赋值 `s.Provider` 和 `s.Model`。但失败的请求（`outcome == "error"`）从未成功将响应写回客户端，因此 **没有任何 Attempt 会被标记为 Forwarded**！这导致所有失败请求的 Provider 和 Model 在样本中被无条件置空（`""`），聚合到小时账本后，前端只能渲染成 Provider 为 `"—"`，造成正常模型无法统计到错误、所有错误全被“吃”到匿名行的严重失真！
  - **【同问题 5.2】`Tok in+cw / cr` 问题重现**：同样的认知负担；
  - **【`Tok Share` 权重未说明】**：代码计算 `(incw + cr + out) / total * 100`，即把 1 个低价值的 cache read token 与 1 个高成本的 fresh/output token 1:1 等权求和计算份额。如果不标明这是“Unweighted Token Volume Share”，会导致部分重度依赖缓存的模型用量份额被过度虚高放大；
  - **【布局局促与指标扩展诉求】**：目前使用 `<div class="grid-2">` 左右各占 50% 分栏，排版极为拥挤，无法容纳更多的生成性能指标（如平均吐字速率 `Tok OUT/s`）。建议将其改为横向通栏（100% 宽度），并补充输出生成速率。

---

### 4.7 模块 7：Live 控制台常驻组件 (在 `log.html` 中呈现)

#### 项 7.1：`Live Requests` (正在执行的请求表)
- **数据**：`stats.inflight[]`。
- **列**：`Age`, `Client`, `Virtual Model`, `Attempt`, `Provider : Model`, `Mode`, `TTFT`, `Tok Out`, `Status`。
- **呈现**：实时展示耗时与输出 Token 增长，超过 10s 未收到上游字节标记为红色 `stalled`。
- **合理性与页面归宿研判**：
  - **设计极其优秀**。直击实时请求排查痛点，字段精炼、状态准确；
  - **【页面归宿不当】**：该表格目前被放置在 `/log.html` 页面，但它属于最典型的“系统运行大盘实时状态”。将它放在日志流页面导致 `/status.html` 失去了最高频的排障入口，同时让 `status.html` 内部残留的 2s 轮询陷入无渲染目标的空转。**强烈建议将其移回 `/status.html` 核心大盘**，使 `/log.html` 纯粹化为终端日志流。

#### 项 7.2：`Recent Failures` (最近失败折叠表)
- **数据**：`stats.recent_errors[]`（上限 100 条）。
- **呈现**：可折叠面板，顶部提供错误类别 Filter Chips，支持点击过滤特定错误码。
- **合理性与页面归宿研判**：
  - **设计优秀**。支持快速归因排查；
  - **【页面归宿不当】**：与 Live Requests 同理，最近失败列表是运维观测的核心资产，应随 Live Requests 一同**移回 `/status.html` 底部作为错误复盘兜底面板**。

---

## 5. 问题总结与合理分组 (基于 ROI 梯队划分)

根据对上述所有指标的审查结果，以**改造代价（Cost）与业务收益（Impact）的比值（ROI）**作为核心依据，将发现的问题划分为三个梯队：

```
+---------------------------------------------------------------------------------------+
|  梯队划分依据：                                                                         |
|  - P0 (高 ROI): 修改代价极低（纯计算公式/前端Label/样式微调），但直接解决严重误导或心智冲突问题   |
|  - P1 (中 ROI): 需要适度重构前后端字段或排版展示，能显著提升可用性与专业度，属于体验核心跃升     |
|  - P2 (低 ROI): 涉及边缘场景、冷门时区或视觉微瑕，改造成本相对不敏感，可暂缓或搁置              |
+---------------------------------------------------------------------------------------+
```

### 5.1 第一梯队 (P0: 高 ROI / 建议优先改)

| 编号 | 问题简述 | 所在位置 | 根因分类 | 预期收益 |
| :---: | :--- | :--- | :---: | :--- |
| **P0-1** | **Tok OUT/s P90 分位数方向倒置与虚假极速问题** | `internal/livestats/ring.go`<br>`status.html` (Perf) | 数据计算与统计方向错误 | **极高**：消除对吞吐率 SLA 的虚高误解，真实反映长尾低速卡顿恶劣体验。 |
| **P0-2** | **全站表格 `Tok in+cw / cr` 反人类展示问题** | `status.html` (Perf, Usage×2) | 前端呈现逻辑与 Label 缺陷 | **极高**：彻底降低人脑心算成本，统一全站 Token 认知模型（Prompt + Cache Hit %）。 |
| **P0-3** | **Quota Headroom 顶部说明与计算物理意义完全相反** | `status.html` (Quota Headroom) | Label 文案与定义解释错误 | **高**：避免用户因“花得越快数值越小”而对 Headroom 产生机制性误解。 |
| **P0-4** | **虚拟模型表格 P0 优先级样式缺失降级为 `pri-n`** | `status.html` (Models PRI) | 前端渲染三元判断遗漏 | **高**：仅改动一行三元判断，即可让核心主路由 P0 恢复最高视觉层级。 |
| **P0-5** | **Usage 表中错误请求归属丢失 Bug（Provider/Model 置空导致 Error 全归入 “-” 行）** | `internal/server/stats.go`<br>`status.html` (Usage) | 后端 Sample 归因判断缺陷 | **极高**：彻底修复只有成功转发才赋予模型的 Bug，让故障模型承担真实 Error 计数。 |
| **P0-6** | **控制台页面职责拆分与全局导航统一（新建 `/models.html`、移回 Live 组件至 `/status.html`、统一导航）** | `server.go`<br>`console.js`<br>`status.html`<br>`log.html` | 页面架构与运维职责划分 | **极高**：解耦静态模型拓扑与实时运行大盘；使 `/log.html` 回归纯终端日志流，让 2s 轮询物尽其用。 |
| **P0-7** | **`/status.html` 整体布局重构（删除 Rail、7 大 Section 顺排、Usage 左右分栏改横向通栏）** | `status.html`<br>`console.css` | 页面排版结构与信息流优化 | **高**：释放 40px 顶部垂直空间，信息流层次分明，通栏设计为新增关键指标提供从容空间。 |
| **P0-8** | **Usage 双表扩充 "Tok OUT/s" 吞吐率列（基于现存 DurMS/TTFTMS 聚合真实生成速度）** | `status.html` (Usage) | 性能指标呈现扩展 | **高**：无需后端改造，直接将现存耗时与输出 Token 转换为宏观窗口平均吐字速度。 |

---

### 5.2 第二梯队 (P1: 中 ROI / 建议改但不急)

| 编号 | 问题简述 | 所在位置 | 根因分类 | 预期收益 |
| :---: | :--- | :--- | :---: | :--- |
| **P1-1** | **Vitals Strip 中 `... total` 伪全量标签误导** | `status.html` (Vitals Strip) | Label 展示与数据口径定义冲突 | **中**：将 `total` 明确为 `7d` 或 `8d` 窗口总量，杜绝用户误以为服务发生数据丢失。 |
| **P1-2** | **Context / Capabilities 属性硬塞单斜杠字符串** | `status.html` (Models Context) | 前端呈现与排版结构设计 | **中**：将上下文容量规格（Badge）与功能能力标签（Chips）解耦，提升视觉扫视效率。 |
| **P1-3** | **Usage 表格 `Tok Share` 缺乏未加权说明** | `status.html` (Usage Tables) | 表头定义与 Hover 说明不足 | **中**：澄清 Cache Read 与 Fresh Token 1:1 计算份额的背景，避免算力消耗误判。 |
| **P1-4** | **Quota Used 浮点数累加暴露 IEEE 754 精度漂移** | `internal/quota`<br>`internal/server` | 后端序列化精度控制不足 | **中**：消除 `11479.499999999` 这类丑陋浮点串，提升系统工程严谨度。 |

---

### 5.3 第三梯队 (P2: 低 ROI / 暂不建议改或可搁置)

| 编号 | 问题简述 | 所在位置 | 根因分类 | 搁置原因 |
| :---: | :--- | :--- | :---: | :--- |
| **P2-1** | **Traffic Chart 客户端时区与服务端日切时间偏差** | `status.html` (SVG Chart) | 跨时区时间格式化 | 绝大多数场景下 VMR 为本地单机或内网部署，浏览器与服务器处于同一时区，发生概率低。 |
| **P2-2** | **SVG 图表极端流量下的双 Y 轴折线与柱状图遮挡** | `status.html` (SVG Chart) | 可视化图表排版极限情况 | 当前图表仅作为概览趋势参考，已有交互式 Tooltip 承载精准数值，重构图表成本高。 |
| **P2-3** | **Quota 表格 Limit Badge 中的 `bucket/gate` 术语** | `status.html` (Quota Limit) | 内部设计术语可读性 | 属于 VMR 架构的核心特色机制，已有详细 Tooltip 解释，强行替换术语可能丢失设计精髓。 |

---

## 6. 重要事项深度剖析与改造建议 (Detailed Solutions for Key Issues)

### 6.1 深度剖析 1：Tok OUT/s 分位数方向颠倒与小样本异常毛刺

#### 1. 问题描述
在 `Performance by Provider & Model` 表格中，`Tok OUT/s p50 / p90` 列显示类似 `39.23 / 64.21`，在极端上游（如 `cliproxy:gemini-3.8-flash-high`）甚至显示为 `1571.4 / 6083.3`。
- **问题 A**：P90 数值比 P50 大很多，反映的是“90% 的请求速率都比它慢”，即前 10% 的极速请求。但在运维监控中，速率是产出型指标，用户看 P90（或期望的保底指标）是为了了解：**“系统在 90% 的请求中，最差能保证多快？长尾卡顿慢到了什么程度？”** 当前数据完全无法提供恶劣体验的下限保障，反而给出了虚高的乐观值；
- **问题 B**：流式请求中，如果上游网络包聚合导致首包后瞬间结束，`spanMS` 极小（几毫秒），产生数千 tok/s 的瞬时值，由于代码直接取升序第 90% 位，这些异常噪点直接主导了 P90。

#### 2. 根因分析
- **数学本质**：耗时（Cost，越低越好）与速率（Yield，越高越好）单调性相反。
  - 对于 Latency，`sorted[90%]` 代表最慢的 10% 门槛，反映长尾恶劣体验；
  - 对于 Throughput，`sorted[90%]` 代表最快的 10% 门槛，反映极速头部体验，而代表“90% 请求均达到此速率（即最慢的 10% 门槛）”的数学点位实际上是升序下的 **P10**（`sorted[10%]`）！
- **滤波缺失**：`toksOf(e)` 没有对极端小耗时（如 `spanMS < 50ms`）或极少 token（如 `tokens.Out < 5`）做防护，单点计算出的畸变速率被原样放入了百分位池。

#### 3. 建议解决方案
1. **计算逻辑改造（`internal/livestats/ring.go`）**：
   - **方案一（推荐：改看中位数与保底低速）**：
     将原本的 P90 替换为 P10（代表最慢 10% 的卡顿长尾/SLA 保障线）。
     ```go
     wb.ToksP50 = nearestRankFloat(toks, 0.5)
     wb.ToksP10 = nearestRankFloat(toks, 0.1) // 90% 的请求均快于此速率
     ```
     前端表头展示为：`Tok OUT/s p50 / p10`（并在 Tooltip 明确注明：`p50 median / p10 worst 10% SLA baseline`）。
   - **方案二（如果仍需保留高低两极区间）**：
     计算 P10 与 P90，前端展示为中位数加区间：`p50 (p10 ~ p90)`，例如 `39.2 (21.5 ~ 64.2)`。
2. **单样本畸变毛刺防护（`toksOf`）**：
   - 增加有效生成时长与 token 下限过滤：
     ```go
     if spanMS < 50 || e.tokens.Out < 5 {
         return 0 // 耗时太短或 token 极少时不计入生成速率百分位统计，防止除法除零或除微小值失真
     }
     ```

---

### 6.2 深度剖析 2：全站表格 Token 统计维度反人类 (`Tok in+cw / cr`)

#### 1. 问题描述
Performance 表格、Usage by Provider 表格、Usage by Caller 表格中，输入 Token 均被粗暴地渲染为 `Tok in+cw / cr`（例如 `1.09M / 8.26M`）。用户无法直接看出总共输入了多少 Token，也无法直接获知 Prompt 缓存命中率。

#### 2. 根因分析
- 早期后端存储结构直接映射了底层审计日志的 `in`（fresh prompt）、`cache_write`、`cache_read` 三个字段；
- 前端编写者未做二次产品化封装，直接把 `in + cache_write` 用加号拼在左边，把 `cache_read` 拼在右边；
- 实际上，Vitals Strip 卡片的开发者已经发现了这个问题，并在卡片上实现了 `tok in X (hit%), out Y`，但未同步推广至下方的三个核心表格。

#### 3. 建议解决方案
1. **重构表头与单元格呈现**：
   - **原表头**：`Tok in+cw / cr`
   - **新表头**：`Tok in (hit %)` 或 `Prompt (hit %)`
   - **单元格渲染**：
     ```javascript
     const promptTotal = (tok.in || 0) + (tok.cache_write || 0) + (tok.cache_read || 0);
     const hitPct = promptTotal > 0 ? ((tok.cache_read || 0) / promptTotal * 100) : 0;
     const cellHtml = `${fmtKMG(promptTotal)} <span class="dim">(${fmtPct(hitPct)})</span>`;
     ```
   - **悬停 Tooltip**：保留完备的三分项明细：
     `fresh: ${fmtKMG(tok.in)} · cw: ${fmtKMG(tok.cache_write)} · cr: ${fmtKMG(tok.cache_read)}`。
2. **业务价值**：
   用户一眼即可看到总输入规模和缓存节省效果，与 Vitals Strip 达到 100% 的认知统一。

---

### 6.3 深度剖析 3：Quota Headroom 解释与物理直觉相反

#### 1. 问题描述
在 Quota 表头写着：`headroom = pace of spend vs pace of time; 1.00 is exactly on pace`。但实际数值在快速消耗时变小变红（如 `0.59`），在消耗缓慢时变大变绿（如 `2.86`）。用户读了说明后产生极大的认知困惑。

#### 2. 根因分析
- 代码公式为：
  $$\text{Headroom} = \frac{1 - \text{used\_frac}}{1 - \text{time\_frac}} = \frac{\text{剩余配额占比}}{\text{剩余时间占比}}$$
- 这是典型的“余量可用步调”（Remaining Budget Pace），而不是“消耗步调”（Pace of Spend）。表头文案用词不当，将分子写成了 spend，与实际公式彻底颠倒。

#### 3. 建议解决方案
1. **修改表头说明文案（`status.html`）**：
   - **原文**：
     `headroom = pace of spend vs pace of time; 1.00 is exactly on pace`
   - **修改为**：
     `headroom = remaining budget pace vs remaining time pace; >1.0 under-budget (safe), <1.0 burning too fast, 1.00 on pace`
2. **在单元格 Tooltip 中加入大白话解释**：
   - `headroom > 1`：当前剩余配额充裕，使用步调慢于时间流逝（安全）；
   - `headroom < 1`：当前配额消耗过快，若保持当前速率将在周期结束前面临耗尽风险（预警）；
   - `headroom <= 0`：配额已耗尽（危险）。

---

### 6.4 深度剖析 4：虚拟模型 P0 优先级样式视觉降级 Bug

#### 1. 问题描述
在 `Virtual Models & Endpoint Topology` 表格中，最重要的主路由 `P0` 显示为灰暗低调的 `pri-n` 样式，反而次要的 `P1`、`P2` 拥有醒目的高亮颜色。

#### 2. 根因分析
- `status.html` 第 668 行：
  ```javascript
  const priCell = ep.from_fallback
    ? '<span class="pri pri-fb" ...>FB</span>'
    : `<span class="pri ${ep.priority === 1 ? 'pri-1' : (ep.priority === 2 ? 'pri-2' : 'pri-n')}">P${ep.priority}</span>`;
  ```
- 缺乏对 `ep.priority === 0` 的显式判断，直接落入最后的 default 分支 `pri-n`。

#### 3. 建议解决方案
- 补充 `pri-0` 专属样式或调整判断逻辑：
  ```javascript
  const priClass = ep.priority === 0 ? 'pri-0' : (ep.priority === 1 ? 'pri-1' : (ep.priority === 2 ? 'pri-2' : 'pri-n'));
  ```
- 在 CSS 中为 `.pri-0` 赋予最尊贵、最高级别的视觉标识（如明亮的品牌青色/绿色或实心徽章）。

---

### 6.5 深度剖析 5：Usage 表中错误请求归属丢失 Bug（Provider/Model 置空导致 Error 全入 “-” 行）

#### 1. 问题描述
在 `Usage by Provider & Model` 表格中，所有有明确 Provider 和 Model 的行，其 `Req ok / err` 列的 Error 数值**全都是 0**；但在表格底部却单独出现了一行 Provider Model 为 `"—"` 的记录，Req 显示为类似 `0 / 10`。所有上游错误未被记入具体模型，而是全部被推给了一个匿名的短横线行。

#### 2. 根因分析
- 核心漏洞位于 `internal/server/stats.go` 的 `sampleFromRecord(rec)` 函数：
  代码在提取 Provider 和 Model 时，仅当 `att.IsForwarded()` 为 true 时才会执行 `win = i; s.Provider = att.Provider; s.Model = att.Model`。
- 但是，根据 `internal/audit/forwarded.go` 的设计，`IsForwarded()` 仅在请求成功且响应实际写回客户端时才会被标记（`SetForwarded()` 的唯一调用方是 `router.forwardSuccess`）。
- 一旦请求彻底失败（`outcome == "error"`），没有任何一个 Attempt 会被标记为 Forwarded！这导致 `win` 永远为 `-1`，`s.Provider` 和 `s.Model` 永远保持默认空字符串 `""`！
- 这些带有空标识的失败 Sample 写入聚合账本后，在前端 `status.html` 中被合并进 `d.provider || '—'`，从而产生了所有错误全被“吞”进 `"—"` 这一行、而具体模型永远“零错误”的严重失真现象。

#### 3. 建议解决方案
1. **分流修复归因逻辑（`internal/server/stats.go: sampleFromRecord`）**：
   - 当 `win < 0`（未成功转发）但 `len(rec.Attempts) > 0`（实际向上游发起过尝试）时：
     说明该请求经历了上游报错或超时淘汰，应该**将终端尝试（Terminal Attempt，即 `rec.Attempts[len-1]`）的 `Provider`、`Model`、`KeyLabel` 赋给 `s`**！与后文提取 `s.ErrorClass` 和 `s.Status` 使用 `final` 的逻辑保持一致。
     这样，因某上游端点报错导致的请求失败，就会准确归属于该上游模型，真实反映各模型的故障率。
   - 当 `len(rec.Attempts) == 0` 时：
     说明是网关前置拦截（如客户端鉴权 401、请求体非法 400、所有端点冷却导致的 `no_candidate`），此时确实无上游端点，`s.Provider` 保持为空。
2. **前端渲染优化（`status.html`）**：
   - 对于空 Provider 行，前端不再仅渲染单一破折号 `"—"`，而是渲染为具有明确物理含义的标签：
     `<span class="ep t-dim" title="Requests failed before routing (client error, auth, or no available candidates)">(gateway / unrouted)</span>`。

---

### 6.6 深度剖析 6：控制台页面职责拆解与全局导航统一

#### 1. 问题描述
目前 `/status.html` 既承载着静态/半静态的拓扑与配额（Quota Budgets, Virtual Models & Endpoint Topology），又承载着高动态的运行时流量、性能与用量大盘；而实时在途的 `Live Requests` 与 `Recent Failures` 反而被割裂放置在 `/log.html` 页面。这导致：
1. Overview 页面过长，运维打开页面后被庞大的静态拓扑挤压，无法快速查看核心业务运行态；
2. `/log.html` 承载了实时请求表格，失去了纯粹终端日志流的定位；
3. 全站导航栏仅有 3 项（`Overview / Live & Log / Help`），缺少对模型资产的管理入口。

#### 2. 根因分析
- 控制台在早期单页设计时将所有功能塞入 Overview，后期为了给日志流补充上下文，临时将 Live Requests 搬到了 `log.html`；
- 这破坏了“大盘总览（Overview）”与“日志终端（Logs）”的职责边界，并导致 `status.html` 内部残留了无用空转的 2s 轮询逻辑。

#### 3. 建议解决方案
1. **新建 `/models.html` 页面**：
   - 将原本在 `/status.html` 中的 “Quota Budgets” 与 “Virtual Models & Endpoint Topology” 两个 section 完整迁移至 `/models.html`；
   - 后端在 `internal/server/server.go` 中挂载 `GET /models.html`，复用现有的 `GET /status` 与 `GET /stats` API，无额外后端数据开发成本。
2. **实时组件回迁至 `/status.html`**：
   - 将 `Live Requests` 与 `Recent Failures` 移回 `/status.html`；
   - `/status.html` 的自适应轮询器（`armLivePoll`：有在途请求时 2s 轮询，闲时 15s 轮询）正式接管 Live Requests 渲染，彻底消除空转；
   - `/log.html` 移除实时请求组件，恢复为纯粹的全屏实时日志流与终端（Terminal）。
3. **全站单点统一导航栏（`internal/server/assets/console.js`）**：
   - 在 `mountConsole()` 中统一配置 4 大导航入口：
     - **Overview** $\rightarrow$ `/status.html`
     - **Models** $\rightarrow$ `/models.html`
     - **Log** $\rightarrow$ `/log.html`（原 `Live & Log` 改为 `Log`）
     - **Help** $\rightarrow$ `/help.html`

---

### 6.7 深度剖析 7：Overview 单页布局重构（删除 Rail、7 大 Section 顺排、Usage 横向通栏）

#### 1. 问题描述
- 导航栏下方存在 5 个锚点的二级快捷链接栏（Rail，占高 40px），在 Quota 和 Models 剥离后，单页内已无长页面锚点跳转需求，白白浪费首屏可视高度；
- `Usage by Provider & Model` 与 `Usage by Client` 当前使用左右 50% 栅格（`grid-2`）分栏，横向宽度仅约 600px，排版极为拥挤局促，无法承载更多的性能指标。

#### 2. 根因分析
- 二级 Rail 在页面精简后属于冗余导航结构；
- 左右分栏假定两个表格行数接近且指标精简，但实际运行中上游模型可能有十几行，而客户端可能仅两三行，且左右分栏严重压缩了数据列的延展空间。

#### 3. 建议解决方案
1. **删除快捷链接栏 Rail**：
   - `status.html` 调用 `mountConsole({ active: 'overview', rail: [] })`；
   - `console.js` 适配空 rail 情况，不渲染 `.hd-rail` DOM 节点，将顶部吸顶预留高度从 88px 收缩为 48px，瞬间释放 40px 的宝贵垂直显示空间。
2. **规范 7 大 Section 顺排**：
   严格按照如下运维心理动线自上而下顺排：
   1. `[Hero Panel, with sys info]`（核心 Vitals 卡片与系统底栏）
   2. `[Live Requests]`（当前正在执行的在途请求）
   3. `[Performance by Provider & Model]`（各模型微观窗口耗时与吞吐性能）
   4. `[Traffic Stats]`（宏观时间窗口时序混合图表）
   5. `[Usage by Provider & Model]`（横向 100% 通栏表格）
   6. `[Usage by Client]`（横向 100% 通栏表格，原 Usage by Caller）
   7. `[Recent Failures]`（最近异常折叠面板）
3. **Usage 表格改为横向通栏**：
   - 拆解 `<div class="grid-2">`，两个表格均采用 100% 通栏宽度（1280px），为后续增加吞吐率、命中率明细提供充裕的视觉空间。

---

### 6.8 深度剖析 8：Usage 双表扩充 "Tok OUT/s" 列（宏观窗口吞吐率聚合）

#### 1. 问题描述
在 `Usage by Provider & Model` 和 `Usage by Client` 两个表格中，仅有总量数据（请求数、Token 数、Share），无法获知在过去 24 小时或 7 天里，各个模型在大规模真实流量下的**宏观平均吐字速度**，也无法排查哪个客户端侧整体感受到的生成速度最慢。

#### 2. 根因分析
- 前端编写时仅累加了 `tokens` 计数，忽略了小时账本中已经具备的耗时字段；
- 实际上，后端 `HourlyRow.Counters` 在设计之初就已经完整记录了 `DurMS`（总耗时）和 `TTFTMS`（首包延迟），底层数据源完备且原生支持。

#### 3. 建议解决方案
1. **聚合算法（前端 `status.html: renderUsage`）**：
   - 在前端根据选定窗口（24h / 3d / 7d）遍历 `hourlyRows` 时，累加每个 Provider:Model 或 Client 的指标：
     `totalOut += c.tokens.out`，`totalDurMS += c.dur_ms.sum`，`totalTtftMS += c.ttft_ms.sum`；
   - 计算净生成有效时长（剔除首字 prefill 耗时）：
     $$\text{SpanMS} = \text{totalDurMS} - \text{totalTtftMS}$$
     若为非流式或无 TTFT 记录，则保底使用 $\text{totalDurMS}$；
   - 计算加权平均生成吞吐率：
     $$\text{Tok OUT/s} = \frac{\text{totalOut}}{(\text{SpanMS} / 1000)}$$
2. **表格列定义与渲染**：
   - 在两个通栏表格中插入新列 `Tok OUT/s`（位于 `Tok out` 与 `Tok Share` 之间）；
   - 使用已有的 `fmtRate(rate)` 渲染（例如 `45.2` tok/s）；当没有输出 Token 或耗时为 0 时安全回退至灰色 `—`；
   - 表头 Tooltip 标明：`Average generation throughput over the selected window: tokens.out / (dur_ms - ttft_ms)`。

---

## 7. 总结与后续落地指引 (Executive Summary & Next Steps)

### 7.1 总结陈词
本次对 vmr 系统的 Live Stats 数据端点（`/status`、`/stats`）以及前端核心运维看板（`status.html`、`log.html`）进行了逐项穿透式审查。

**核心结论如下**：
1. **系统底层架构扎实**：基于内存环形缓冲区（Ring Buffer）和轻量 WAL+Rollup 的两级 Ledger 架构性能极佳，数据采集全面，实时性与耐久性兼顾；
2. **呈现逻辑存在明显的技术视角偏差与重大归因漏洞**：
   - 存在如用户指出的典型问题：**速率 P90 分位数方向倒置** 以及 **Token 呈现 `in+cw / cr` 反人类心算**；
   - 挖掘出重大后端归因 Bug：**失败请求因非 Forwarded 导致 Provider/Model 被置空，造成模型 Error 统计全为 0，所有错误沦落至 “-” 匿名行**；
   - 梳理出控制台页面职能切分与布局架构重组方案：通过**新建 `/models.html`、回迁 Live 组件、删除 Rail、双表通栏并扩充 `Tok OUT/s`**，实现控制台体验质的跃升。
3. **改造价值极高且风险可控**：上述 P0 梯队重构项边界极度清晰，不需要变动底层数据存储协议，仅需少量 Go 映射修正与前端页面模板重组即可落地。

### 7.2 后续落地建议路线图
1. **批次 1（立即可做，P0 梯队）**：
   - **后端修正**：修复 `sampleFromRecord` 中失败请求的终端 Attempt Provider/Model 归属，彻底解决 Error 漏计与 “-” 匿名行问题；
   - **分位数修正**：修正 `ring.go` 中吞吐率的分位数取值逻辑（改用 P10 代表长尾卡顿下限，或提供区间），在 `toksOf` 中增加极小耗时防御过滤；
   - **页面拆分与导航统一**：新建 `/models.html` 承载配额与拓扑，将 Live Requests / Recent Failures 移回 `/status.html`，并在 `console.js` 中统一 4 大导航入口；
   - **Overview 布局重构**：移除 Rail 快捷栏（吸顶收缩至 48px），按 7 大 Section 重排，Usage 双表改为横向通栏；
   - **指标升级**：全站表格统一重构为 `Tok in (hit %)`，并在两个通栏 Usage 表格中利用现存耗时数据计算并新增 `Tok OUT/s` 列；
   - **细节修正**：修正 Headroom 顶部说明文案、补齐 Virtual Models 的 P0 优先级专属高亮样式。
2. **批次 2（近期规划，P1 梯队）**：
   - 将 Vitals Strip 的 `... total` 优化为 `... / 3.01K (7d total)`；
   - 将模型能力与上下文容量拆分为结构化徽章排版；
   - 后端针对浮点配额用量输出做 round 舍入保护。
3. **批次 3（长期储备，P2 梯队）**：
   - 根据后续多时区部署反馈，按需引入图表服务端时区对其支持。

*(本报告已保存至工作区 `LIVE_STATS_REVIEW_REPORT.md`，未修改现有代码，未产生 git commit。)*
