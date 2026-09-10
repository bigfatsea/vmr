<!-- Ver 2026-09-09, by pi -->

# Virtual Model Router (vmr) — 设计方案 · 实时请求统计（Live Stats）

**这是 v4 版设计文档里路由半区的一个子系统专篇。** 路由核心本身（虚拟模型、协议透传、
Adapter、调度与健康、审计日志格式）见 `docs/VirtualModelRouter_Design_v4_Core.md`（Part 1）；
本文描述的实时统计挂在它的请求收尾点上，之所以独立成篇而不并进 Part 1，是因为它自带的
三层数据模型（瞬态 per-request 流 + 历史 rollup + 进程内 in-flight 注册表）、滚动与恢复生命周期，体量已够一份完整
设计文档，而 Part 1 只需要知道"audit 写完处多了一个观察者钩子"。

配套文档：
- `docs/VirtualModelRouter_Design_v4_Core.md`（Part 1）—— 审计日志格式与 `/status` 契约
  是本文的输入与邻接面；`audit.Record` 形状因本文而变化的部分（token 盖章）必须同改 report
  （编译期耦合，见 Part 1 的不变量清单）。
- `docs/VirtualModelRouter_Design_v4_Quota.md` —— token 四项计数的计算与扣费就在它的
  计量路径上（`TokenCountersSides`）；本文盖章的值与它扣费的 raw 值必须由差分测试钉住。
- `docs/VirtualModelRouter_Design_v4_Analytics.md`（Part 2）—— 离线分析半区。它不受本
  文影响，但共享"盖在 record 上的字段优先于从 body 反推"这一纪律。

本文只解决"怎么做"。范围严格限定在**内置实时统计**：单进程内存聚合 + in-flight 实时注册 +
自有落盘文件 +
自带展示页，不做任何对外导出/推送集成（当前无此需求，不为它预留配置面；将来若要接外部
观测栈，观察者钩子就是天然的挂载点，届时另立专篇）。

---

## 1. 问题定义

### 1.1 缺什么

现在对"服务器当前请求状况"没有任何实时视图。`/status` 只暴露配置与健康状况（含 quota
余量），`vmr analyze` 是纯离线的。运营一台跑着多客户端、多 provider 的本机路由，最常
要看的三组量全都看不到：

| 指标族 | 维度 | 窗口 |
| --- | --- | --- |
| 请求量 / token 量 / 成功率（token 四项分列） | protocol × provider+model × client_key_tag × key_label × virtual model | 按小时、按天，累计 |
| 进行中请求明细（到达/发出/首末块/est tokens × 状态） | per-request | 实时（当前 queued + running） |
| Time to first token | provider+key_label+model+stream | 最近 10 / 100 条，p50 / p90 |
| tokens/s（四分量 ÷ 总耗时） | provider+key_label+model+stream | 最近 10 / 100 条，p50 / p90 |

`client_key_tag`（调用方凭据的尾部推导）与 `key_label`（上游凭据的 label）是**两个相互
独立的概念**：前者回答"谁发来的"，后者回答"这个请求实际打到了哪个云厂商的哪个账号/哪把
key"。两者在所有统计面并列记录，互不替代。

### 1.2 目标函数与约束

在**不影响路由主流程性能**的前提下，让上述量在单二进制内实时可查。硬约束：

1. **热路径零阻塞**：完成时钩子必须在响应已提交之后（脱离主线）且只做 O(1) 工作；
   in-flight 盖章点（§5）在请求存续期内，每处只做原子写或单锁 O(1) 更新（§5.3），不得引入任何等待；
2. **磁盘占用有界**：不许随时间无限累积第二份 per-request 数据；
3. **无配置、无外部依赖**：不引入新配置键也能工作（有 audit 目录就能落盘）；
4. **隐私分级**：统计流不含任何对话正文——这是它与 audit 日志的本质区别。audit 是取证
   记录（字节级复现需要 body），统计是定量遥测（只要数字）。两者独立开关、独立保留，
   用户可以选择不留正文而仍要监控。

---

## 2. 设计总览：完成时账本 + in-flight 实时面

两条互补的观测面：**完成时账本**（请求结束后一次性记账，slim WAL + rollup）与
**in-flight 实时面**（请求存续期间在内存里逐块盖章，结束即消失）。两者都挂在请求生命
周期的固定点上，互相不写对方的数据。

```
请求到达（chatHandler：probe + auth 之后、进并发门之前）
  └─ in-flight 注册表登记（router 内存，queued；§5）
       ├─ tryOne 发出 → sent_at / attempt / provider / model / key_label 盖章（state → running）
       └─ copyFlush 逐块 → first_byte_at / last_byte_at / est_out 盖章
请求结束（任意路径退出）→ 注册表整条移除

请求完成（server 的 audit done() 钩子，响应已提交、脱离主线）
  ├─ 内存聚合器（进程内，完成时账本的实时面，§3.4/§4.1）
  │    ├─ 当前小时 keyed 计数
  │    └─ per provider+key_label+model+stream 的性能 ring（TTFT / token 吞吐）
  └─ 追加写 slim 小时文件（~200B/请求，当小时的 WAL，§3.2）

小时滚动（lazy：第一个跨小时请求触发；不搞 ticker）
  └─ 从 slim 文件整体聚合 → 追加进 rollup 历史 → 删除该 slim 文件（§6）

GET /stats（auth-gated）+ 内嵌控制台 Overview 页（§8）
  └─ 读时聚合：in-flight 快照 + rollup 内存映射 + 当前小时计数 + ring，JSON 输出
```

四条贯穿性原则：

- **钩子只记账，不算账。** 一切分位数、分组求和都推迟到 `/stats` 被读的那一刻；读是低频
  操作，数据量小（rollup 万行级 / ring 每键百条），读时算毫秒级。
- **slim 流是 WAL，不是第二份永久数据。** rollup 之后即删，磁盘上永远只有"一个当前小时
  slim + 一个 rollup"。
- **幂等由构造保证。** 滚动时从完整 slim 文件重新聚合（不是累加增量），同一小时滚两次、
  崩溃后重滚，结果一致。
- **in-flight 只活在内存。** 注册表条目随请求生灭，永不落盘、不结算进完成时数字——它
  回答"现在正在发生什么"，完成时账本回答"发生过什么"。

---

## 3. 数据模型

### 3.1 盖章：token 四项计数 + 上游 key 标识上 record（前置改动）

现状缺口：quota 扣费路径（`router.TokenCountersSides`）算过一次 token 四项计数，但值只
进了 quota 账本，没盖到 audit record 上；分析半边要从原始 response body 反解析一遍。
这违反"routing half 能在它动作的地方盖戳就不让下游反推"的既有纪律（`Attempt.Forwarded`
就是这个模式的先例）。

改动一：`audit.Attempt` 增加一级 raw 计数块：

```json
"tokens": {"in": 1200, "out": 900, "cache_read": 0, "cache_write": 0}
```

- 只在 `forwardSuccess` 处盖章（与 `Forwarded=true` 同点），数据源与喂给 quota 扣费的
  raw 侧**同一来源、同一时刻取值**；
- 盖章**不受 `needsTokenCharge` 门控**——quota 扣费在 provider 没有 `metric: tokens`
  的 Limit 时会跳过 usage 提取（热路径优化），但审计证据不能依赖 quota 配置，所以每个
  forwarded 请求都盖。代价：完全不配 quota 的纯路由部署，每个 forwarded 请求多一次
  post-stream 的 usage fold——此刻 respnorm 的 usage/meter 已是缓存字段读，成本是缓存读
  加算术，微秒级；
- 值是 int64 原始计数，**不做** model_multipliers 折算（折算后的 float64 是 quota 的
  计费口径，不是流量口径；两者不得混存）；
- `tokens.in` 是 **fresh input**（`usage.In − cache_read − cache_write`，与 quota 计量的
  `Fresh` 分量同义），**不是** gross prompt——`in + cache_read + cache_write` 才等于上游
  报告的总 prompt tokens。JSON key 仍叫 `in`（对齐 `Attempt.tokens` 与 slim/rollup 的
  落盘格式），语义按此理解；
- 差分测试钉住：盖章值 vs quota 扣费入账的 raw 计数必须一致（复用
  `cmd/vmr/quota_parity_test.go` 的模式——router 侧调 router 自己的导出入口，不复述公式）。

改动二：同一位置（`forwardSuccess`）再盖上游凭据标识 `Attempt.key_label`：

```json
"key_label": "main"
```

- 值的推导在 config 层完成、随 Provider 进 `core.Endpoint` 快照：provider 用的是带
  label 的 `api_keys` 展开（`p1` + `main`/`backup` → `p1-main`/`p1-backup`）→
  `key_label` 就是 label 本身（沿用 config 的叫法，就叫 label，不叫 tag）；单一
  `api_key`（无 label）→ 密钥尾 6 位。展开后的 provider 名虽内含 label，但靠解析名字
  反推是脆弱的（基础名自身可含连字符），展开期记住即可；
- `client_key_tag`（Record 层，调用方凭据）与 `key_label`（Attempt 层，上游凭据）是
  **两个相互独立的概念**——前者回答"谁发来的"，后者回答"实际打到了哪个云厂商的哪个
  账号/哪把 key"——名字不同是刻意的，不是疏漏。slim/rollup（§3.2/§3.3）逐字引用这两个
  字段的**名字与值**，不新造第三套 key 推导；
- 它与客户端侧的 `audit.KeyTag`（尾 8 位窗口 + 连字符截断）是**两套有意的不同推导**，
  见 §9 决策表——不要顺手统一。

连带义务：`internal/report` 与 `audit.Record` 形状编译期耦合，同改同测。

### 3.2 slim 小时文件（瞬态 WAL）

路径：`<log_dir>/vmr-stats-YYYYMMDD-HH.jsonl`（与 audit 同目录）。0600。双进程写坏 slim/rollup
的问题由 livestats **自己的** advisory flock（`<log_dir>/.vmr-stats.lock`，与 audit 的
`.vmr-audit.lock` 独立、同机制）挡住——不能寄生 audit 的目录锁，因为 `-audit=false` 时那把锁
根本不存在，而"不留正文仍要监控"（§1.2）恰恰是要支持的场景。第二个指向同 `log_dir` 的实例
拿不到锁 → `livestats.New` 返回 error → 该实例降级为纯内存统计（不写文件，不污染首个实例的
归档）。每行一个请求：

```json
{"ts":"2026-09-07T14:32:01+08:00","vmodel":"coding","protocol":"anthropic-messages",
 "stream":true,"outcome":"ok","client_key_tag":"jason",
 "provider":"p1","model":"claude-sonnet-4","key_label":"main",
 "dur_ms":12340,"ttft_ms":412,
 "tokens":{"in":1200,"out":900,"cache_read":0,"cache_write":0}}
```

字段语义：

| 字段 | 语义 |
| --- | --- |
| `ts` | 请求到达时刻（rec.TS）。**小时归属按它算**，跨边界请求归到达的小时 |
| `vmodel` | 虚拟模型名（`Record.Model`）。**全行唯一改名的字段**：一行同时出现虚拟名与上游名，audit 靠分层区分（`Record.model` vs `Attempt.model`），扁平行只能改名；`upstream_model` 在 audit 里已被占用（上游自报名），不可借用 |
| `protocol` / `client_key_tag` | ingress 协议 / 调用方 key 标识——**逐字取** `Record.ClientKeyTag`，与 audit 同名同值，不新造推导 |
| `stream` | 请求是否流式 |
| `outcome` | `ok` / `error` / `canceled`（`audit.OutcomeFor` 的产物，不重造） |
| `provider` / `model` | **实际服务的** provider 与真实上游模型名（来自 winning attempt 的 `Attempt.Provider`/`Attempt.Model`）；从未转发成功时为空串 |
| `key_label` | 上游凭据的 label，取 winning attempt 盖章的 `Attempt.key_label`（§3.1）；未转发为空串 |
| `dur_ms` / `ttft_ms` | client-view 总耗时 / 首字节延迟（`Record.TTFTMS`，0 = 未测量） |
| `tokens` | 四项 token 对象，**原样拷贝** winning attempt 盖章的 `Attempt.tokens`（§3.1）——键空间与 audit 完全一致；`in` 是 fresh（净 cache，见 §3.1）；未转发为全 0 |

一行 ~200B。**没有任何请求/响应正文、URL、header**——这就是它与 audit 的隐私分界。

### 3.3 rollup 历史文件（永久，压缩态）

路径：`<log_dir>/vmr-stats-rollup.jsonl`。append-only，每行一个 (hour × dims) 组的聚合计数：

```json
{"hour":"2026-09-07T13:00:00+08:00",
 "dims":{"vmodel":"coding","protocol":"anthropic-messages","provider":"p1",
          "model":"claude-sonnet-4","key_label":"main","client_key_tag":"jason","stream":true},
 "ok":12,"error":1,"canceled":0,
 "tokens":{"in":14400,"out":10800,"cache_read":0,"cache_write":0},
 "dur_ms":{"sum":148080,"n":13},
 "ttft_ms":{"sum":4944,"n":12}}
```

要点：

- **键空间与 audit 同形**：`dims` 内的 `client_key_tag`/`key_label` 与 `tokens` 对象都逐字
  引用 audit 的字段名与值（§3.1/§3.2），读取侧用同一形状解析，没有第二套键名。`vmodel`
  是唯一的扁平化改名（§3.2 字段表）；
- **key 基数有界**：小时数 × dims 组合。dims 各轴都来自 config（vmodel/provider/key_label）
  或固定枚举（protocol/stream）；`client_key_tag` 的基数即客户端键表（未配 `api_keys`
  时为客户端自报凭据的 tag，与 report 的 client_key 分组同基数）。量级为每小时几十行、
  单文件年万行级，单文件够用，无需按月/年再分。若远期基数失控（key 维度不再有界），
  届时再引入 key 归一化，不预做。
- **存和，不存直方图**：`{"sum":…,"n":…}` 支撑均值与总量；分位数只活在运行态 ring（§4.2）。
  历史分位数漂移是接受的取舍——为历史保留直方图块，复杂度与收益完全不成比例。
  `{sum,n}` 嵌套只用于 `dur_ms`/`ttft_ms` 这两个需要 n 求均值的量；`tokens` 保持与
  `Attempt.tokens` 相同的扁平形状（分量各自求和），不为对称而加 n。
- **append + 读取时 last-wins，不做 read-modify-write upsert**。小行 append 在 POSIX 下
  近乎原子，崩溃最多留一行残缺（读取时丢弃/被覆盖）；upsert 的读-改-写窗口大得多。启动
  加载时按 (hour, dims) 取最后一条即可。
- 0600，**文件永不自动删除**——它是全量归档。**内存只是它的近 7 天滑动窗口**（`rollupRetentionDays`
  个整天 + 当天，§3.4）：启动只加载窗口内的行，之后每次日切逐出掉出窗口的那一天。
  文件按年万行级增长，体量可忽略；若启动解析真的变慢（启动日志会显示恢复行数与耗时），
  届时再按天分文件，现在不做。

### 3.4 内存态

聚合器进程内持有三块，全部小而有界：

1. **rollup 键表**：`(hour, dims) → counters`，是 §3.3 rollup 文件**近 7 天**（`rollupRetentionDays`
   个整天 + 当天）的内存映像（不是全量）。窗口边界是**日历日**：`now` 是周三中午 12:00 时，
   窗口 = 上周三 00:00 起（7 个整天 + 周三半天）；`now` 是 0:00 时正好 7 整天。启动按此窗口
   过滤加载；运行中每次日切逐出掉出窗口的那一天。所以键表大小是常数 `≈ 8 × 24 × dims 基数`，
   读时 fold 成本不随部署年限增长——这正是 `/stats` 读缓存能真正兜住高频轮询的前提。窗口取 7 天：
   `?range=` 最宽就是 7d，内存里正好是控制台能显示的量；要看更久跑 `vmr analyze`（它读 audit log，
   不读这个文件），`by_*` 累计也因此是"滚动近一周"的口径；
2. **当前小时计数**：`(dims) → counters`（hour 固定为当前），由钩子实时累加；
3. **性能 ring**：per `(provider, key_label, model, stream)` 各一个容量 100 的环形缓冲。
   ring 条目存**原始元组** `(ts, dur_ms, ttft_ms, tokens{in,out,cache_read,cache_write})`，
   分位数与速率在读时算——公式若要修，历史数据不用迁移。ring 只收 `outcome=ok` 且已转发
   的样本（见 §4.2 归因规则）。容量 100 对"最近 10 / 100 条 p50/p90"刚好：最近 10 条是
   ring 的尾部切片。

   两处与"只存 tokens.out"的旧形态不同，都是被读侧需求逼出来的：

   - **key 里加 `key_label`**：展开后的 provider 名虽然内含 label（`p1-main`），但 §3.1 已经
     判定"靠解析名字反推是脆弱的"——把这条纪律只用在写侧、却让读侧的消费者去拆
     `p1-main`，等于自己破自己的规矩。ring key 与输出行都带上 label，下游拿到的就是
     `(provider, key_label, model, stream)` 四元组本身。
   - **条目存四分量而不只是 `out`**：读侧要的是"这一个窗口内的 token 用量"与
     "单请求 token 吞吐"，两者都需要四分量。四个 int64 × 100 条 × 键数，量级可忽略——
     这是拿确定的、可忽略的内存换掉一整类"窗口内的数字对不上"的歧义。

in-flight 进行中请求的注册表**不在本包**——它是 router 运行态（§5），`/stats` 读时合并。

---

## 4. 写路径

### 4.1 钩子位置与成本预算

挂在 `server.beginAudit` 返回的 `done()` 里、`audit.Write` 之后。此刻响应已提交（客户端
侧计时已闭环），处于脱离主线的收尾段。铁律：

- 钩子内只做：构造一行 slim JSON、append 文件、更新内存计数与 ring——全部 O(1)。
  **唯一的例外是滚动边界那一次**：跨小时的第一个请求触发 `rollHourLocked`，它在持锁期间
  流式读完该小时的整个 slim 文件、逐行 parse、append rollup、删 slim（§6），成本是
  O(该小时 slim 行数)。每小时一次、脱离主线、本地量级——用这一次 O(n) 换掉 ticker 与
  "从内存搬家只搬一次"的额外状态，是刻意取舍（§9 决策表）；
- 一次请求至多一把锁（聚合器自身 mutex），且**不与 audit 写锁嵌套**（先释放 audit 的再进
  聚合器的）；
- slim 写失败只降级统计精度（该请求缺样本），绝不影响请求结果，也绝不回写错误到客户端；
  内存聚合与 slim 写互不阻塞对方成功——内存计数照常累加。

### 4.2 归因规则

一个请求有两层事实，归属要分清：

| 事实层 | 归属维度 | 规则 |
| --- | --- | --- |
| 请求面（总能观测） | `ts/vmodel/protocol/stream/outcome/client_key_tag` | 直接取自 record；失败、取消、404 的请求也计入这些维度的 outcome 计数 |
| 服务面（仅转发成功时有意义） | `provider/model/key_label`、token、TTFT、吞吐 | 只取 winning attempt（`IsForwarded` 的那个）；全尝试失败的请求在 provider 维度上**缺席**（outcome 计数已覆盖其存在性） |

- `ttft_ms=0` 是"未测量"（Part 1 的既有语义：本地快速拒绝、瞬时响应），聚合时排除，
  不计入 `ttft_ms` 的 `{sum,n}`，也不入 ring；
- token 只在转发成功时有值，与 quota 扣费同基准；
- probe 流量不写 audit（既有决定），自然也不进统计——监控只反映真实客户端流量，正确。

### 4.3 并发模型

聚合器一个 mutex 覆盖全部内存态。读路径（`/stats`）持锁做读时聚合——数据小、结构简单，
粗锁正确性一目了然；这与"init 注册表用原子读 + COW 写"的惯例不冲突（那条针对的是注册表，
这里是一个整体状态的账本）。

读时聚合是 O(内存 rollup + 当前小时)。内存 rollup 是近 7 天滑动窗口（§3.4），所以这是个
**不随部署年限增长的常数**——但控制台 Overview 页有活动时按 ~2s 轮询 `/stats`、还可能多个
标签页并存，"读低频"的假设仍不成立，一个常数成本乘以高频也值得省。`CachedSnapshot()` 兜住
这一点：它缓存上一次 `Snapshot()` 结果 `snapCacheTTL`（3 秒，**刻意大于轮询节奏**——一次
轮询通常直接复用上一次 fold，活跃期稳态成本是每几秒一次 fold 而非每拍一次），窗口内所有
轮询者直接拿缓存，`Record` 写入**不**使缓存失效（监控容忍数秒滞后）。这层缓存只作用于历史
聚合段——`/stats` 的 in-flight 快照与并发计数每次读都实时计算，不经此缓存。`/stats` 走
`CachedSnapshot()`；`Snapshot()` 保持纯聚合，供测试和需要精确即时读的调用方。

---

## 5. In-flight：进行中请求的实时视图（router 内存注册表）

完成时账本的一切都以"请求已结束"为前提——audit 在收尾才写，slim 行在 done() 才追加。
要回答"现在这 6 个并发正在干什么、队列里还压着几个、卡住的流最后一块数据是什么时候
到的"，需要一个**请求存续期间就在更新**的内存注册表。它与完成时账本是两个互不交叉的
计数面：一个请求由 done() 钩子记入 slim/内存计数**恰好一次**；in-flight 只覆盖"开始
之后、完成之前"的空窗，条目在请求结束时整条删除，不结算进任何完成时数字。

### 5.1 归属：router，不是 livestats

注册表放 `internal/router`（新文件 `inflight.go`）：

- 事件全部发生在 router 内部——排队（chatHandler → `AcquireSlot`）、逐 attempt 发出
  （`tryOne` → `client.Do`）、逐块到达（`copyFlush` 读循环）——都早于任何 audit record
  可观测的时刻，server 层拿不到这些中间点；
- router 已有同型先例：`Telemetry`（/status 的进程级计数）与 limiter 的
  `waiting`/`inFlight` 原子计数。in-flight 注册表是同一族，只是从计数升级为 per-request
  条目；
- livestats 保持"完成时账本"的单一职责，其 slim/rollup/内存计数不感知 in-flight。

`/stats` 读时把两个来源拼在一起：`inflight[]`（注册表快照）+ 完成时聚合（rollup 尾部 +
当前小时 + ring）。两者数字不重叠：in-flight 条目尚未计入任何完成时账本。

### 5.2 条目与盖章点

注册表是 `(seq → entry)` 的 map（`seq` 为进程级原子自增号，页面轮询间可关联同一条目），
一把 mutex 只覆盖 map 的增删。条目字段与盖章点：

| 字段 | 来源与盖章点 |
| --- | --- |
| `seq` | 注册时分配，进程内唯一 |
| `state` | `queued`（尚未发出，含排队与预处理）→ `running`（已发出至上游）；由 `sent_at` 是否已盖章推导，不单独存储 |
| `protocol` / `vmodel` / `stream` / `client_key_tag` | 到达时已知（TopLevelProbe + auth 之后、进并发门之前注册） |
| `addr` | 客户端地址（/stats 与 audit 同为 auth-gated，不构成新的暴露面） |
| `ts` | 到达时刻；`sent_at − ts` 即"vmr 收到 → 发出"的间隔（含排队） |
| `sent_at` / `attempt` | `tryOne` 每轮 attempt 发出请求时覆盖盖章；failover 时随当前 attempt 更新——卡在重试上是"请求卡住"的常见原因，attempt 必须可见 |
| `provider` / `model` / `key_label` | 当前 attempt 的 endpoint 三元组（随 `sent_at` 同点更新）；排队/路由中为空 |
| `first_byte_at` | 上游响应体第一个字节到达 vmr 的时刻（copyFlush 读循环首块）——上游视角，非 audit 的 client-view `ttft_ms`；非流式响应同样适用 |
| `last_byte_at` | 上游响应体最近一个字节的到达时刻，每块覆盖盖章 |
| `est_in` | `Facts.EstimatedTokens`（facts 在进并发门之后才算出，排队中的请求此字段未知） |
| `est_out` | 每块读 `rbody.OutTokens()`（respnorm 计量表，与 quota 扣费同源同值）；压缩响应体估不出来（0），展示为未知 |

移除点：chatHandler 的 defer 链上，请求以任何方式离开（完成、失败、取消、排队中放弃）
都整条删除。TRUNCATED 的 `panic(http.ErrAbortHandler)` 展开也经过 defer，注销照常。

### 5.3 更新频次：逐块盖章，不节流

`last_byte_at` 与 `est_out` 随每个响应块更新，**刻意不做任何节流**（按秒或按字节量攒批）：

- **语义压倒成本**：`last_byte_at` 的价值是"上游最后一块数据真实到达的时刻"——流卡死
  检测全靠它与当前时刻的差值。任何节流都让最后一次盖章发生在真实末块**之后**，卡死的
  流反而显得更新鲜，方向恰好错；
- **成本本来就低**：每块一次 `OutTokens()`（per-stream mutex——respnorm 的计数本来每块
  就进这把锁）加两三次 per-entry 原子写，零分配；相对每块本就承担的 SSE 解析与 JSON
  扫描，增量可忽略。千块/秒量级的极端流下也远够快。

排队聚合（"8 个请求、6 槽、2 排队"中的聚合计数）直接复用 limiter 已有的
`Concurrency()`——它返回 `(limit, in_flight, waiting)`，`/stats` 的 `concurrency`
对象逐字沿用这三个键（`in_flight` = 已获槽正在跑，`waiting` = 卡在门外排队）。
in-flight 注册表只负责 per-request 明细。

### 5.4 刻意不做

- **响应头到达时刻**：与 `first_byte_at`（体首块）只差毫秒级，单独一列是噪声；
- **队列位次**：信号量队列不内省，`queued` 聚合计数已覆盖"压了几个"的问题；
- **自适应短轮询（不做事件级 SSE 推送）**：in-flight 是易变的状态集（mutable
  state machine），不是无边界追加的事件日志（append-only stream）。逐块或逐事件推送
  （`req_queued`/`req_sent`/`req_chunk`/`req_done`）会引发长文本流下的重绘风暴与网络抖动
  导致的状态机乱序；而全量 snapshot SSE 本质上只是服务端驱动的时钟。控制台 Overview
  页采用自适应短轮询（有活动时 ~2s，空闲 15s，后台标签页自动暂停），配合服务端把
  历史聚合段读缓存 `snapCacheTTL`（大于轮询节奏），零新增服务端状态，全量快照天然自愈；
- **落盘/结算**：in-flight 永不写文件；条目结束即消失，完成时事实由 done() 钩子记账，
  两条路径不互写。重启后 in-flight 自然清空（本来就是瞬态）。

---

## 6. 小时滚动

**lazy 关账**（与 quota 的 lazy reset 同构）：钩子发现样本的 `ts` 小时 ≠ 聚合器当前打开
的小时时，先关旧账再开新账：

1. 找到所有**未滚动的** slim 文件（hour < 当前小时的；可能不止一个——停机跨了多小时）；
2. 逐文件流式读取 → 按 (dims) 聚合出该小时的 rollup 行 → append 进 rollup 文件；
3. 删除已滚的 slim 文件；打不开新小时文件则本小时统计降级为纯内存（见 §9）。

这一整段在**持聚合器 mutex** 期间同步完成（`rollHourLocked`）——触发它的那个 `Record`
因此是 O(该小时 slim 行数)，不是 O(1)（§4.1 铁律的唯一例外）。每小时至多一次、发生在
脱离主线的收尾段、本地量级（繁忙的一小时几千行、~1MB），期间并发的 `done()` 钩子
goroutine 短暂阻塞在锁上但客户端无感（响应早已提交）。把这段 IO 移出锁需要一条独立的
roll goroutine 与它跟 `bookPastSampleLocked`/下一次 roll/`Close` 的交互处理，对一个每小时
一次的操作复杂度不成比例——刻意不做（§9 决策表）。

两个细节：

- **为何从文件聚合而不是从内存**：内存里当前小时的计数在滚动瞬间就是完整的小时数据，似
  乎可以直接搬——但崩溃恢复路径（§7）里"重启后补滚"只能从文件来，两处走同一条代码才是
  一致性；且"从完整 slim 文件重算"天然幂等，内存搬家则要求"只搬一次"的额外状态。
- **崩溃窗口**：rollup append 成功、slim 删除之前崩溃 → 下次启动重滚同一文件，rollup 里
  出现重复 key 行。**last-wins 读取规则天然消化它**：重滚行覆盖旧行，数值一致。

---

## 7. 重启恢复

启动顺序（全在聚合器构造函数里，同步完成；本地量级，毫秒到十毫秒级）：

0. 建 `<log_dir>`、取 `.vmr-stats.lock` advisory flock（§3.2）；拿不到锁直接返回 error
   （调用方降级为纯内存），不继续往下；
1. 扫 `<log_dir>`，加载 rollup 文件**近 7 天窗口**（§3.4）为内存键表（last-wins）；
   更早的行读过即弃，留在盘上不入内存；
2. 对 hour < 当前小时的 slim 文件执行 §6 的补滚；
3. 读当前小时的 slim 文件：喂当前小时计数 + ring（ring 取末尾 ≤100 条，不足即缺，不追）；
4. 打开当前小时的 slim 文件进入写状态。

性质：

- **audit 不参与恢复**——统计子系统对 audit 完全独立（audit off 时统计照常工作，见 §1.2
  隐私分级）；
- ring 是 best-effort 的运行态指示器：跨小时的历史不回填（rollup 只有和，没有样本），重
  启后 ring 只反映当前小时内已发生的请求。这符合它的定位——反映"最近一段时间各模型的性
  能表现"，不追求精确；
- 恢复完成后打一行启动日志（恢复的 rollup 行数 + 耗时），作为"文件是否大到该按天分"的
  唯一判据；近 7 天窗口在这一步（加载过滤）和运行中（每次日切逐出掉队的一天）两处强制，
  恢复后内存里不会有早于窗口的小时；
- 若 rollup 或 slim 损坏（半行 JSON）：跳过该行继续；rollup 不可读则从空表开始（历史统计
  清零是可接受的降级，绝不阻塞启动）。**锁是唯一的硬失败**——rollup/slim 的问题都降级，
  但拿不到目录锁意味着另一个进程正在写同一批文件，此时继续写就是数据损坏。

---

## 8. 读路径：`GET /stats`

- **`GET /stats`**：与 `/status` 同一 auth 门槛（`s.auth`），同一契约家族——统计按
  `client_key_tag`（调用方）与 `key_label`（上游凭据）两个独立维度分组，等于暴露用量
  画像，绝不能无认证暴露。JSON 结构（读时聚合生成）：
  - `inflight[]` + `concurrency{limit,in_flight,waiting}`：进行中请求明细与并发门状态
    （§5 注册表快照 + limiter 聚合；键名逐字取自 `router.Concurrency()` 的返回，
    不是 in-flight 条目的 `state`（那是 per-request 的 `queued`/`running`））；
  - `hourly[]`：近期逐小时 × dims 的计数（来自 rollup 尾部 + 当前小时）。默认只保留最近
    有数据的 `hourlyTail`（48）个小时；**`?range=24h|3d|7d` 把这个尾窗改为 24 / 72 / 168 小时**，
    供控制台的时间序列图与按 key/caller 的区间用量表使用（三者共用同一份 `hourly[]`，
    在客户端按 dims 折叠，服务端不为每个消费者各做一次聚合）。7d 是上限：内存 rollup 窗口
    就是近 7 天，`?range=` 不可能超出它，控制台也只提供 24h/3d/7d 三档。dims 基数在单机路由上是
    个位数到几十（端点 × 调用方 × 协议），168 小时 × 几十行仍是几百 KB 量级；
  - `daily[]`：按天折叠的同一套计数，`dailyTail` 略大于内存窗口（`rollupRetentionDays + 1`），
    窗口能装几天就给几天。折叠按 **server-local 日历日**（`time.Local`，与 slim 文件名同一
    时区权威；livestats 是 leaf 不能 import `fmtutil.DisplayZone`，但生产态两者同值）。目前
    控制台不渲染 `daily[]`，同 `overall` 作为 JSON 契约保留；
  - `by_provider_model[]`：per `(provider, key_label, model, stream)` 一行——**`key_label`
    是独立字段，不让消费者去拆展开后的 provider 名**（§3.4）。每行含近 7 天窗口内的累计计数
    与均值（token 四项、dur/ttft 均值），以及 `last_10` / `last_100` 两个 **窗口块**；
    `by_client_key_tag[]` / `by_key_label[]` 同理是近 7 天的累计画像；
  - **窗口块**（`last_10` / `last_100`）不只是分位数，它是"这一段最近样本"的完整画像：
    `n`（**窗口内实际样本数**）、`tokens` 四项在该窗口内的和、`ttft_p50/p90`、
    `toks_p50/p90`。`n` 必须出现在输出里——ring 常常不满 100，消费者不知道 `n` 就会把
    一个 12 样本的 p90 当成 100 样本的 p90 来读；
  - `overall`：把所有 ring 的样本并在一起后算出的同一个窗口块。分位数不可合并，所以
    这一项**必须由服务端在读时对样本并集算**，消费者拿到分行数据后自己是算不出来的。
    它是为控制台首屏那个"全局 TTFT p50"加的——不过该 vitals 段在 console polish 轮
    据用户反馈移除了（见 console-unification 设计文档），`overall` **目前无内置消费者**，
    作为 `/stats` JSON 契约的一部分保留（无害、已测、可外部消费；若首屏日后要补延迟
    信号会重新用上）；无 ring 样本时为 `null`；
  - `recent_errors[]`：最近 50 条失败/取消请求的明细环（§8.1）；
  - `by_client_key_tag[]`：按调用方 key 的用量画像（近 7 天累计）；
  - `by_key_label[]`：按上游凭据的用量画像（多账号/多 key 的 provider 由此分账，同为近 7 天
    累计）。控制台的"按区间"用量表走 `hourly[]` 折叠，不走这里；
- **展示页**：`/stats` 的消费者是内置控制台的 Overview 页（`go:embed`，与 `log.html` /
  `help.html` 同模式）。独立的 `stats.html` 与 `/stats.html` 路由**已并入 Overview 并下线**
  ——见 console-unification 设计文档；`/stats` 这个 **JSON 契约本身不变**，本节继续有效。
  整页绝大部分区域按 5 分钟固定节奏刷新（点击头部倒计时立即刷，标签页隐藏自动暂停）；
  Live Requests 区块与并发 vitals 由前端自适应轮询单独驱动（有进行中请求时 ~2s 一拍，
  空闲退避至 15s 一拍，标签页隐藏停拍，恢复时立即探测）。in-flight 与并发计数每次读实时
  算，其余聚合段读缓存 `snapCacheTTL`（大于轮询节奏，活跃期稳态每几秒一次 fold）。live
  区块把 `last_byte_at` 渲染成"距今秒数"——它是流卡死的直接信号。
- **速率只有一个口径：`toks`**（读时应用，写在 §9 的决策表里防漂移）：
  `toks = (tokens.in + tokens.out + tokens.cache_read + tokens.cache_write) / (dur_ms / 1000)`
  ——单请求的四分量 token 总和 ÷ 整请求耗时，流式与非流式**同一个分母**。
  - 早期设计里另有一个 `tps`（只看 `tokens.out`，且流式扣掉 `ttft_ms` 只算生成段）。
    两者并列输出的结果是消费者要先分辨口径才能读数，而"首 token 慢不慢"这件事
    `ttft_p50/p90` 已经单独回答了——**`tps` 因此撤销，只留 `toks`**；
  - 流式与非流式仍在 ring 键里分开（`stream` 是 key 的一部分，§3.4）。分母虽然统一了，
    但两类请求的时间构成本就不同，混进同一个分位池仍然是无意义的平均。

### 8.1 `recent_errors[]`：失败明细环

完成时账本回答"这一小时错了几个"，in-flight 回答"现在有什么在跑"，**中间缺一个"刚刚那条
为什么失败"**——它既不在正在发生的集合里，也已经被小时聚合抹成一个计数。运维在这段空窗
里只能去翻原始日志或跑离线的 `vmr analyze`，而这恰恰是最需要快的时刻。

一个容量 50 的进程内环形缓冲补上它，与性能 ring 同族——**纯内存、瞬态、重启清空、
永不落盘、不含任何正文**，因此不改变 §1.2 的隐私分级。每条：

```json
{"ts":"2026-09-07T14:32:01+08:00","vmodel":"agent","protocol":"anthropic-messages",
 "stream":true,"client_key_tag":"openclaw","provider":"packycode-main","key_label":"main",
 "model":"claude-opus-4.6","attempt":2,"outcome":"error","error_class":"upstream_5xx",
 "status":502,"dur_ms":4100}
```

- 收 `outcome != ok` 的样本（`error` 与 `canceled` 都收——"客户端取消了"和"上游挂了"
  在排障时是两种完全不同的结论，合并计数会把它们抹平）；
- `error_class` **直接引用路由半区自己的 `core.ErrorClass`**，读侧绝不重新分类：分类逻辑
  只有一份，在 `DefaultClassify`；
- 从未转发成功的请求 `provider/key_label/model` 为空串，与 §4.2 的归因规则一致；
- 与性能 ring 一样，它只反映当前进程这一段时间内发生的事，不追求完整——完整的取证
  记录是 audit 的职责。

---

## 9. 决策与取舍表

| 决策 | 理由 | 代价 / 保留意见 |
| --- | --- | --- |
| token 四项盖章到 `Attempt.tokens`（新） | 下游反推 body 是既有纪律的反面；一次计算多方受益 | `report` 的按端点 token 应改用盖章值（当前仍 `chatmsg.ExtractUsageSides` 从 body 反解析——已登记 `KNOWN_ISSUES`）；差分测试钉住与 quota 扣费同源；`tokens.in` 存 fresh（净 cache）不存 gross；盖章不受 `needsTokenCharge` 门控（证据独立于 quota 配置），无 quota 部署每请求多一次 post-stream usage fold（缓存读 + 算术，微秒级） |
| 上游凭据标识盖 `Attempt.key_label`（label 或尾 6 位） | 不让下游反推（展开名内含 label，但靠解析名字反推脆弱）；analyze 的按 key 分账同步受益 | config 展开期需携带 label；report 同改 |
| `client_key_tag` 与 `key_label` 命名不同、推导不同，两个独立概念并存 | 前者=调用方凭据（`audit.KeyTag` 尾 8 位+连字符，有 16 字符下限与"-alice"约定的历史包袱，报表/文件名已定型），后者=上游凭据（config 的 label 叫法，或尾 6 位） | 刻意不统一，勿顺手合并 |
| slim/rollup 键空间逐字对齐 audit（`tokens`/`client_key_tag`/`key_label` 原样引用，仅 `vmodel` 因扁平化改名） | 同一数据两套键名 = 每个消费方都要翻译一遍；audit 是唯一格式权威 | —— |
| in-flight 注册表放 router（`inflight.go`），不放 livestats | 事件（排队/发出/逐块）全在 router 内部，早于任何 record 可观测点；`Telemetry`/limiter 计数已是同型先例 | router 包多一个文件；/stats 读时合并两来源 |
| `last_byte_at`/`est_out` 逐块盖章，不节流 | `last_byte_at` 的语义是"最后一块真实到达时刻"，节流会让卡死的流显得更新鲜（盖章滞后于真实末块），方向恰好错；成本为 per-stream 锁内一次读 + per-entry 原子写，零分配 | —— |
| queued/running 由 `sent_at` 推导，不存第三状态 | "获槽未发出"的毫秒级窗口不值得一个 preparing 态 | —— |
| in-flight 不落盘、不结算进完成时账本 | done() 恰好记一次，in-flight 只补"进行中"空窗，两本账互不交叉计数 | 重启后自然清空（本来就是瞬态） |
| 独立 slim 流，不寄生于 audit | 隐私分级（无正文）；audit off 时监控可用；恢复读取 200B/行 vs 数 KB/行 | 多一个 append 写点（O(1)，已预算）；有 audit 时数字上有轻微重复；"独立"必须包含锁——livestats 自带 `.vmr-stats.lock`（§3.2），不能寄生 audit 的目录锁 |
| slim rollup 后即删 | 磁盘有界；rollup 后即冗余 | 历史分位数不可恢复（只有和与均值）——接受，ring 定位本就是运行态指示 |
| rollup append + last-wins，不 upsert | append 近乎原子，崩溃窗口最小 | 同 key 可能留重复行，读取侧消化 |
| lazy 关账，不 ticker | 与 quota 同构；零流量零开销 | 停机跨小时由启动补滚兜住（§7）；触发滚动的那一个 `Record` 是 O(该小时 slim 行数)、持聚合器 mutex（§6）——每小时一次、脱离主线、本地量级，比 ticker + 内存搬家状态机简单，接受 |
| rollup 存和不存直方图 | 历史分位数的需求从未成立，ring 覆盖"最近"语义 | 历史只能给均值；将来真要，加直方图块是向后兼容的 |
| rollup 文件永不删（全量归档），但内存只加载近 7 天（`rollupRetentionDays` 个整天 + 当天），每次日切逐出掉队的一天 | 只截断输出尾窗不够——`by_*` 累计是全 rollup fold，读成本会随部署年限线性涨，读缓存也兜不住一个越来越贵的 fold；把内存做成滑动窗口后 fold 是常数 | `by_*` 累计口径从"自启动以来"变成"滚动近 7 天"；要看更久跑 `vmr analyze`（读 audit log）；文件随年限增长，启动解析成本 O(全历史)——有启动日志盯着，真变慢再按天分文件 |
| 内存窗口取 7 天、按日历日对齐、不做 lazy 从文件加载 | 7d = `?range=` 最宽档 = 内存里正好是控制台能显示的量，一个干净不变量；实测每 `(hour,dims)` 行 ~450B，小团队规模近 7 天约 1–3 MB，病态高基数也就几十 MB——不值得为省几 MB 把读路径搞成带文件 I/O 的（那比刚优化掉的 fold 还慢） | `by_*` 是"近一周"口径而非月度；将来真要 30 天趋势视图，改一个常量 |
| `daily[]` 按 server-local 日历日折叠（`time.Local`） | 时区一处权威——人类可见的"按天"跟运维本地墙钟；与 slim 文件名同一权威 | livestats 是 leaf 不能 import `fmtutil.DisplayZone`，直接用 `time.Local`（生产态同值）——CLAUDE.md 时区不变量的 documented exception |
| ring 存原始元组（`ts/dur/ttft/tokens` 四分量），读时算速率与分位 | 公式可修，数据不迁；四分量让「窗口内用量」与「单请求吞吐」都能在读时算出来，不必让消费者去凑 | 每键 100 条 × 4 个 int64，量级可忽略；读时排序 100 条，微不足道 |
| 钩子放 audit done() 内 | 响应已提交、计时已闭环、单点 | 与 audit 写共享收尾段；不嵌锁已写明（§4.1） |
| 小时归属按到达时刻 | 与 audit 的请求语义一致 | 跨边界长请求把全部 token 记入到达小时——接受 |
| 聚合器粗 mutex | 状态是一个账本整体，粗锁正确性一目了然 | 与 init 注册表的原子读惯例场景不同，不适用；`/stats` 读路径持锁做 O(rollup) fold，控制台 Overview 页活跃期 ~2s 轮询 × 多标签页会放大——由 `CachedSnapshot()` 的 `snapCacheTTL`（3s，大于轮询节奏）读缓存兜住（一拍通常复用上一次 fold，活跃期稳态每几秒一次；`Record` 不使缓存失效，监控容忍数秒滞后），`Snapshot()` 仍是纯聚合供测试与需精确读的调用方 |
| probe 流量不进统计 | 不写 audit 的既有决定自然延伸 | —— |
| provider 维度在全失败请求上缺席 | 无服务发生就没有服务面事实 | outcome 计数（请求面）覆盖其存在性 |
| ring key 带 `key_label`，输出行也带 | §3.1 已判定「靠解析展开后的 provider 名反推 label 是脆弱的」——这条纪律不能只用在写侧而让读侧去拆名字 | key 多一个字段；行数不变（展开名本就一账号一个） |
| 窗口块输出 `n`（窗口内实际样本数） | ring 常常不满 100；不给 `n`，一个 12 样本的 p90 会被当成 100 样本的 p90 读 | 输出多一个整数 |
| 撤销 `tps`，只留 `toks` | 两个速率口径并列，消费者要先分辨口径才能读数；「首 token 慢不慢」由 `ttft_p50/p90` 单独回答 | 失去「纯生成段速率」这个细分；真要时可由四分量与 ttft 在读时重算，数据都还在 |
| 新增 `overall` 合并窗口块 | 分位数不可合并，消费者拿到分行数据算不出全局 p50；控制台首屏要的就是这一个数 | 读时多一次对样本并集的排序（键数 × 100 条） |
| 新增 `recent_errors[]`（容量 50，纯内存） | 完成时账本与 in-flight 之间的空窗——「刚刚那条为什么失败」无处可查（§8.1） | 多一个环形缓冲；仍不含正文，隐私分级不变 |
| in-flight 采用自适应短轮询而非 SSE 长连接 | in-flight 为易变状态集，事件推送易乱序重绘；全量快照天然幂等自愈。in-flight/并发每次读实时算，其余聚合段读缓存 `snapCacheTTL`（大于轮询节奏），活跃期稳态每几秒一次 fold | 繁忙期比绝对实时有 ≤2s 观测滞后；接受，肉眼无感 |

---

## 10. 落地范围

- 新包 `internal/livestats`：聚合器、slim/rollup 文件 IO、恢复、读时聚合、自己的
  `.vmr-stats.lock` advisory flock（`lock_unix.go`/`lock_windows.go`，仿 audit）。**零内部依赖**
  （自有输入样本结构，server 从 `audit.Record` 构造后喂入；不 import audit，保持与取证
  格式解耦——record 形状再变，统计输入契约不动）。
- `internal/router`：`inflight.go`——in-flight 注册表（§5）；`tryOne`（发出盖章、failover
  覆盖）与 `copyFlush`（逐块盖章）接盖章点。
- `internal/server`：done() 钩子接线（对 livestats 加 `rec.Model != ""` 门槛，pre-probe
  失败不入统计——与"probe 不写 audit"同构）；chatHandler 注册/移除 in-flight 条目（probe
  之后、`AcquireSlot` 之前注册，defer 链移除）；`/stats`（走 `CachedSnapshot()`，解析
  `?range=`）；`recorder` 的 `captureBody` 开关——`-audit=false` 时不缓冲响应体，
  只留 status/ttft 供 livestats。展示页并入控制台 Overview（见 console-unification）。
- `internal/audit`：`Attempt.tokens` 与 `Attempt.key_label` 盖章字段（§3.1）。`internal/report`
  的 token 来源切到盖章值是连带义务，但工作量大（跨 viewmodel 层、golden fixture 变动），
  单列为后续任务——已登记 `KNOWN_ISSUES`，当前 report 仍从 body 反解析。
- `internal/config`：`expandProviderAPIKeys` 在展开期携带 label（单一 `api_key` 推导尾 6 位），随快照进入 `core.Endpoint` 供盖章。
- `archtest`：`livestats` 加入 leaf 包清单；行预算按增量常规调整。
- **控制台 Overview 页所需的读侧增量（已全部落地）**（逐条都在本文上面有出处）：ring key 加
  `key_label`、ring 条目存四分量（§3.4）；`by_provider_model[]` 行带 `key_label`、
  `last_10`/`last_100` 升级为含 `n` 与 `tokens` 的窗口块、`tps` 撤销改 `toks`、新增
  `overall`（§8）；新增 `recent_errors[]`（§8.1）；`/stats` 支持 `?range=`（§8，读缓存按
  range 分键）。与之配套的 `/status` 增补（告警 `alerts[]`、端点行的 `provider`/`key_label`/
  `model`/`from_fallback` 拆分字段与 quota headroom join）随 console-unification 实施轮
  同批落地；展示页并入控制台 Overview（`/status.html`），`/stats.html` 退役。实施期裁定
  （告警仅 cooldown 触发等）见 `_subtasks/console-unification/contracts.md` §5 与
  `KNOWN_ISSUES` 对应条目。
- 测试重点：滚动幂等（同文件滚两次 rollup 数值一致）；重启恢复（rollup + slim 补滚 + ring
  重建）；token 盖章 vs quota 扣费差分；`ttft=0` 排除；last-wins 消化重复 rollup 行；
  in-flight 快照一致性（`-race`）与 failover 覆盖、排队取消清理、TRUNCATED panic 路径
  的注销；窗口块的 `n` 在 ring 未满时等于实际条数（不是 10/100）；`overall` 与单键
  窗口块在只有一个键时数值一致。
