<!-- Ver 2026-09-07, by Claude -->

# Virtual Model Router (vmr) — 设计方案 · 实时请求统计（Live Stats）

**这是 v4 版设计文档里路由半区的一个子系统专篇。** 路由核心本身（虚拟模型、协议透传、
Adapter、调度与健康、审计日志格式）见 `docs/VirtualModelRouter_Design_v4_Core.md`（Part 1）；
本文描述的实时统计挂在它的请求收尾点上，之所以独立成篇而不并进 Part 1，是因为它自带的
双层数据模型（瞬态 per-request 流 + 历史 rollup）、滚动与恢复生命周期，体量已够一份完整
设计文档，而 Part 1 只需要知道"audit 写完处多了一个观察者钩子"。

配套文档：
- `docs/VirtualModelRouter_Design_v4_Core.md`（Part 1）—— 审计日志格式与 `/status` 契约
  是本文的输入与邻接面；`audit.Record` 形状因本文而变化的部分（token 盖章）必须同改 report
  （编译期耦合，见 Part 1 的不变量清单）。
- `docs/VirtualModelRouter_Design_v4_Quota.md` —— token 四项计数的计算与扣费就在它的
  计量路径上（`TokenCountersSides`）；本文盖章的值与它扣费的 raw 值必须由差分测试钉住。
- `docs/VirtualModelRouter_Design_v4_Analytics.md`（Part 2）—— 离线分析半区。它不受本
  文影响，但共享"盖在 record 上的字段优先于从 body 反推"这一纪律。

本文只解决"怎么做"。范围严格限定在**内置实时统计**：单进程内存聚合 + 自有落盘文件 +
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
| 请求量 / token 量 / 成功率（token 四项分列） | protocol × provider+model × keytag × virtual model | 按小时、按天，累计 |
| Time to first token | provider+model | 最近 10 / 100 条，p50 / p90 |
| tokens/s | provider+model | 最近 10 / 100 条，p50 / p90 |

### 1.2 目标函数与约束

在**不影响路由主流程性能**的前提下，让上述三组量在单二进制内实时可查。硬约束：

1. **采集点必须在响应已提交之后**（脱离主线），且钩子内只做 O(1) 工作；
2. **磁盘占用有界**：不许随时间无限累积第二份 per-request 数据；
3. **无配置、无外部依赖**：不引入新配置键也能工作（有 audit 目录就能落盘）；
4. **隐私分级**：统计流不含任何对话正文——这是它与 audit 日志的本质区别。audit 是取证
   记录（字节级复现需要 body），统计是定量遥测（只要数字）。两者独立开关、独立保留，
   用户可以选择不留正文而仍要监控。

---

## 2. 设计总览：三层一钩子

```
请求完成（server 的 audit done() 钩子，响应已提交、脱离主线）
  ├─ 内存聚合器（进程内，唯一真相的实时面）
  │    ├─ 当前小时 keyed 计数（见 §4.1）
  │    └─ per provider+model 的 TTFT / TPS 环形缓冲（见 §4.2）
  └─ 追加写 slim 小时文件（~200B/请求，当小时的 WAL，见 §3.2）

小时滚动（lazy：第一个跨小时请求触发；不搞 ticker）
  └─ 从 slim 文件整体聚合 → 追加进 rollup 历史 → 删除该 slim 文件（见 §5）

GET /stats（auth-gated）+ 内嵌 stats.html 轮询页（见 §7）
  └─ 读时聚合：rollup 内存映射 + 当前小时计数 + ring，JSON 输出
```

三条贯穿性原则：

- **钩子只记账，不算账。** 一切分位数、分组求和都推迟到 `/stats` 被读的那一刻；读是低频
  操作，数据量小（rollup 万行级 / ring 每键百条），读时算毫秒级。
- **slim 流是 WAL，不是第二份永久数据。** rollup 之后即删，磁盘上永远只有"一个当前小时
  slim + 一个 rollup"。
- **幂等由构造保证。** 滚动时从完整 slim 文件重新聚合（不是累加增量），同一小时滚两次、
  崩溃后重滚，结果一致。

---

## 3. 数据模型

### 3.1 盖章：token 四项计数上 record（前置改动）

现状缺口：quota 扣费路径（`router.TokenCountersSides`）算过一次 token 四项计数，但值只
进了 quota 账本，没盖到 audit record 上；分析半边要从原始 response body 反解析一遍。
这违反"routing half 能在它动作的地方盖戳就不让下游反推"的既有纪律（`Attempt.Forwarded`
就是这个模式的先例）。

改动：`audit.Attempt` 增加一级 raw 计数块：

```json
"tokens": {"in": 1200, "out": 900, "cache_read": 0, "cache_write": 0}
```

- 只在 `forwardSuccess` 处盖章（与 `Forwarded=true` 同点），数据源与喂给 quota 扣费的
  raw 侧**同一来源、同一时刻取值**；
- 值是 int64 原始计数，**不做** model_multipliers 折算（折算后的 float64 是 quota 的
  计费口径，不是流量口径；两者不得混存）；
- 差分测试钉住：盖章值 vs quota 扣费入账的 raw 计数必须一致（复用
  `cmd/vmr/quota_parity_test.go` 的模式——router 侧调 router 自己的导出入口，不复述公式）。

连带义务：`internal/report` 与 `audit.Record` 形状编译期耦合，同改同测。

### 3.2 slim 小时文件（瞬态 WAL）

路径：`<log_dir>/vmr-stats-YYYYMMDD-HH.jsonl`（与 audit 同目录，受同一把 `log_dir`
flock 保护——双进程写坏归档的问题已由 audit 的目录锁一并覆盖）。0600。每行一个请求：

```json
{"ts":"2026-09-07T14:32:01+08:00","vmodel":"coding","protocol":"anthropic-messages",
 "stream":true,"outcome":"ok","keytag":"k1",
 "provider":"p1","model":"claude-sonnet-4","dur_ms":12340,"ttft_ms":412,
 "tok_in":1200,"tok_out":900,"tok_cr":0,"tok_cw":0}
```

字段语义：

| 字段 | 语义 |
| --- | --- |
| `ts` | 请求到达时刻（rec.TS）。**小时归属按它算**，跨边界请求归到达的小时 |
| `vmodel` | 虚拟模型名（`Record.Model`） |
| `protocol` / `keytag` | ingress 协议 / `ClientKeyTag` |
| `stream` | 请求是否流式 |
| `outcome` | `ok` / `error` / `canceled`（`audit.OutcomeFor` 的产物，不重造） |
| `provider` / `model` | **实际服务的** provider 与真实上游模型名（来自 winning attempt 的 `Attempt.Provider`/`Attempt.Model`）；从未转发成功时为空串 |
| `dur_ms` / `ttft_ms` | client-view 总耗时 / 首字节延迟（`Record.TTFTMS`，0 = 未测量） |
| `tok_*` | 四项 token，取自 winning attempt 盖章的 `tokens`（§3.1）；未转发为 0 |

一行 ~200B。**没有任何请求/响应正文、URL、header**——这就是它与 audit 的隐私分界。

### 3.3 rollup 历史文件（永久，压缩态）

路径：`<log_dir>/vmr-stats-rollup.jsonl`。append-only，每行一个 (hour × dims) 组的聚合计数：

```json
{"hour":"2026-09-07T13:00:00+08:00",
 "dims":{"vmodel":"coding","protocol":"anthropic-messages","provider":"p1",
          "model":"claude-sonnet-4","keytag":"k1","stream":true},
 "ok":12,"error":1,"canceled":0,
 "tok_in":14400,"tok_out":10800,"tok_cr":0,"tok_cw":0,
 "dur_ms_sum":148080,"dur_n":13,
 "ttft_ms_sum":4944,"ttft_n":12}
```

要点：

- **key 基数有界**：小时数 × dims 组合。dims 各轴都来自 config（vmodel/provider/keytag）
  或固定枚举（protocol/stream），量级为每小时几十行、单文件年万行级，单文件够用，无需
  按月/年再分。若远期基数失控（keytag 不再来自 config），届时再引入 key 归一化，不预做。
- **存和，不存直方图**：`*_sum` + `*_n` 支撑均值与总量；分位数只活在运行态 ring（§4.2）。
  历史分位数漂移是接受的取舍——为历史保留直方图块，复杂度与收益完全不成比例。
- **append + 读取时 last-wins，不做 read-modify-write upsert**。小行 append 在 POSIX 下
  近乎原子，崩溃最多留一行残缺（读取时丢弃/被覆盖）；upsert 的读-改-写窗口大得多。启动
  全量加载时按 (hour, dims) 取最后一条即可。
- 0600，永不自动删除（体量可忽略；真要清理由人工动手）。

### 3.4 内存态

聚合器进程内持有三块，全部小而有界：

1. **rollup 键表**：`(hour, dims) → counters`，即 §3.3 的内存映像；
2. **当前小时计数**：`(dims) → counters`（hour 固定为当前），由钩子实时累加；
3. **TTFT / TPS ring**：per `(provider, model, stream)` 各一个容量 100 的环形缓冲。
   ring 条目存**原始四元组** `(ts, dur_ms, ttft_ms, tok_out)`，分位数与 TPS 在读时算——
   公式若要修，历史数据不用迁移。ring 只收 `outcome=ok` 且已转发的样本（见 §6 归因规则）。
   容量 100 对"最近 10 / 100 条 p50/p90"刚好：最近 10 条是 ring 的尾部切片。

---

## 4. 写路径

### 4.1 钩子位置与成本预算

挂在 `server.beginAudit` 返回的 `done()` 里、`audit.Write` 之后。此刻响应已提交（客户端
侧计时已闭环），处于脱离主线的收尾段。铁律：

- 钩子内只做：构造一行 slim JSON、append 文件、更新内存计数与 ring——全部 O(1)；
- 一次请求至多一把锁（聚合器自身 mutex），且**不与 audit 写锁嵌套**（先释放 audit 的再进
  聚合器的）；
- slim 写失败只降级统计精度（该请求缺样本），绝不影响请求结果，也绝不回写错误到客户端；
  内存聚合与 slim 写互不阻塞对方成功——内存计数照常累加。

### 4.2 归因规则

一个请求有两层事实，归属要分清：

| 事实层 | 归属维度 | 规则 |
| --- | --- | --- |
| 请求面（总能观测） | `ts/vmodel/protocol/stream/outcome/keytag` | 直接取自 record；失败、取消、404 的请求也计入这些维度的 outcome 计数 |
| 服务面（仅转发成功时有意义） | `provider/model`、token、TTFT、TPS | 只取 winning attempt（`IsForwarded` 的那个）；全尝试失败的请求在 provider 维度上**缺席**（outcome 计数已覆盖其存在性） |

- `ttft_ms=0` 是"未测量"（Part 1 的既有语义：本地快速拒绝、瞬时响应），聚合时排除，
  不计入 `ttft_sum/ttft_n`，也不入 ring；
- token 只在转发成功时有值，与 quota 扣费同基准；
- probe 流量不写 audit（既有决定），自然也不进统计——监控只反映真实客户端流量，正确。

### 4.3 并发模型

聚合器一个 mutex 覆盖全部内存态。读路径（`/stats`）持锁做读时聚合——读低频、数据小，
可接受；这与"init 注册表用原子读 + COW 写"的惯例不冲突（那条针对的是注册表，这里是一
个整体状态的账本，粗锁简单且正确性一目了然）。

---

## 5. 小时滚动

**lazy 关账**（与 quota 的 lazy reset 同构）：钩子发现样本的 `ts` 小时 ≠ 聚合器当前打开
的小时时，先关旧账再开新账：

1. 找到所有**未滚动的** slim 文件（hour < 当前小时的；可能不止一个——停机跨了多小时）；
2. 逐文件流式读取 → 按 (dims) 聚合出该小时的 rollup 行 → append 进 rollup 文件；
3. 删除已滚的 slim 文件；打不开新小时文件则本小时统计降级为纯内存（见 §8）。

两个细节：

- **为何从文件聚合而不是从内存**：内存里当前小时的计数在滚动瞬间就是完整的小时数据，似
  乎可以直接搬——但崩溃恢复路径（§6）里"重启后补滚"只能从文件来，两处走同一条代码才是
  一致性；且"从完整 slim 文件重算"天然幂等，内存搬家则要求"只搬一次"的额外状态。
- **崩溃窗口**：rollup append 成功、slim 删除之前崩溃 → 下次启动重滚同一文件，rollup 里
  出现重复 key 行。**last-wins 读取规则天然消化它**：重滚行覆盖旧行，数值一致。

---

## 6. 重启恢复

启动顺序（全在聚合器构造函数里，同步完成；本地量级，毫秒到十毫秒级）：

1. 扫 `<log_dir>`，加载 rollup 文件为内存键表（last-wins）；
2. 对 hour < 当前小时的 slim 文件执行 §5 的补滚；
3. 读当前小时的 slim 文件：喂当前小时计数 + ring（ring 取末尾 ≤100 条，不足即缺，不追）；
4. 打开当前小时的 slim 文件进入写状态。

性质：

- **audit 不参与恢复**——统计子系统对 audit 完全独立（audit off 时统计照常工作，见 §1.2
  隐私分级）；
- ring 是 best-effort 的运行态指示器：跨小时的历史不回填（rollup 只有和，没有样本），重
  启后 ring 只反映当前小时内已发生的请求。这符合它的定位——反映"最近一段时间各模型的性
  能表现"，不追求精确；
- 若 rollup 或 slim 损坏（半行 JSON）：跳过该行继续；rollup 不可读则从空表开始（历史统计
  清零是可接受的降级，绝不阻塞启动）。

---

## 7. 读路径：`GET /stats` 与 stats.html

- **`GET /stats`**：与 `/status` 同一 auth 门槛（`s.auth`），同一契约家族——统计按 keytag
  分组，等于暴露用量画像，绝不能无认证暴露。JSON 结构（读时聚合生成）：
  - `hourly[]`：近期逐小时 × dims 的计数（来自 rollup 尾部 + 当前小时）；
  - `daily[]`：按天折叠的同一套计数；
  - `by_provider_model[]`：累计与均值（token 四项、dur/ttft 均值），含 `last_10` /
    `last_100` 的 TTFT、TPS p50/p90（来自 ring，nearest-rank，读时排序副本）；
  - `by_keytag[]`：按 key 的用量画像。
- **`stats.html`**：`go:embed` 内嵌，与 `status.html`/`log.html` 同模式；JS 轮询
  `/stats`（秒级间隔足够），不引入 SSE/websocket。从 `/help` 页与 status 页互链。
- TPS 与 TTFT 的**分母定义**（读时应用，写在 §8 的决策表里防漂移）：
  - 流式：`tps = tok_out / ((dur_ms − ttft_ms) / 1000)`——纯生成段速率，首 token 慢不
    惩罚 TPS；
  - 非流式：`tps = tok_out / (dur_ms / 1000)`——端到端吞吐；
  - 两类样本在 ring 键里就分开了（`stream` 是 ring key 的一部分，§3.4），绝不互混分位数。

---

## 8. 决策与取舍表

| 决策 | 理由 | 代价 / 保留意见 |
| --- | --- | --- |
| token 四项盖章到 `Attempt.tokens`（新） | 下游反推 body 是既有纪律的反面；一次计算多方受益 | report 同改；差分测试钉住与 quota 扣费同源 |
| 独立 slim 流，不寄生于 audit | 隐私分级（无正文）；audit off 时监控可用；恢复读取 200B/行 vs 数 KB/行 | 多一个 append 写点（O(1)，已预算）；有 audit 时数字上有轻微重复 |
| slim rollup 后即删 | 磁盘有界；rollup 后即冗余 | 历史分位数不可恢复（只有和与均值）——接受，ring 定位本就是运行态指示 |
| rollup append + last-wins，不 upsert | append 近乎原子，崩溃窗口最小 | 同 key 可能留重复行，读取侧消化 |
| lazy 关账，不 ticker | 与 quota 同构；零流量零开销 | 停机跨小时由启动补滚兜住（§6） |
| rollup 存和不存直方图 | 历史分位数的需求从未成立，ring 覆盖"最近"语义 | 历史只能给均值；将来真要，加直方图块是向后兼容的 |
| ring 存原始四元组，读时算 TPS/分位 | 公式可修，数据不迁 | 读时排序 100 条，微不足道 |
| 钩子放 audit done() 内 | 响应已提交、计时已闭环、单点 | 与 audit 写共享收尾段；不嵌锁已写明（§4.1） |
| 小时归属按到达时刻 | 与 audit 的请求语义一致 | 跨边界长请求把全部 token 记入到达小时——接受 |
| 聚合器粗 mutex | 状态是一个账本整体，粗锁正确性一目了然 | 与 init 注册表的原子读惯例场景不同，不适用 |
| probe 流量不进统计 | 不写 audit 的既有决定自然延伸 | —— |
| provider 维度在全失败请求上缺席 | 无服务发生就没有服务面事实 | outcome 计数（请求面）覆盖其存在性 |

---

## 9. 落地范围

- 新包 `internal/livestats`：聚合器、slim/rollup 文件 IO、恢复、读时聚合。**零内部依赖**
  （自有输入样本结构，server 从 `audit.Record` 构造后喂入；不 import audit，保持与取证
  格式解耦——record 形状再变，统计输入契约不动）。
- `internal/server`：done() 钩子一行接线 + `/stats`、`stats.html`。
- `internal/audit`：`Attempt.tokens` 盖章字段（§3.1），`internal/report` 同改。
- `archtest`：`livestats` 加入 leaf 包清单；行预算按增量常规调整。
- 测试重点：滚动幂等（同文件滚两次 rollup 数值一致）；重启恢复（rollup + slim 补滚 + ring
  重建）；token 盖章 vs quota 扣费差分；`ttft=0` 排除；last-wins 消化重复 rollup 行。
