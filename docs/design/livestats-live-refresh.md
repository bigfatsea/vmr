<!-- Ver 2026-09-15, by pi -->

# Live Stats 实时刷新改造：Slot 列表 + 数字动效方案

**状态：已实现。** 本文是 LiveStats 子系统专篇（`docs/VirtualModelRouter_Design_v4_LiveStats.md`）的增补设计，聚焦控制台 Overview 页 Live Requests 区的刷新体验。

方案的立足点是：**实时感的瓶颈不在数据管道，在呈现模型。** 现状自适应轮询（忙时 2s / 闲时 15s）的数据延迟本身可以接受，真正的问题是在"行随请求结束突然消失"——用户看到的是一张不断跳变、行凭空出现又凭空蒸发的表。本方案把解决这个问题的主要工作放在呈现层（固定 slot 列表 + 数字动效），数据管道只做一处小改（已结束请求的终态快照），整体轮询维持在 1s。

---

## 1. 问题定义

Live Requests 表当前由自适应轮询驱动，每次 poll 后以 `innerHTML` 整体重建。两个体验问题：

| # | 现象 | 根因 |
|---|---|---|
| ① | 请求一结束，行立刻消失；并发几条时表反复跳变 | 渲染模型 = in-flight 集合的直接投影，结束即从载荷中消失 |
| ② | est_out 等 token 数在两次 poll 之间静止，变化瞬间突兀地跳到新值 | 无过渡动效；1s 粒度的跳变在视觉上是"闪变" |

Elapsed 列按轮询节拍刷新（忙时每秒一跳）即可接受，不需要任何本地计时或动画机制——poll 算出多少就显示多少。排队请求（超过并发上限时的 waiting）目前只有并发指标区的一个数字，Live 表上看不到直观提示。

**负载前提**：并发客户端 ≤5（通常 1 个），1s 轮询 = 每秒个位数个小 JSON 响应；账本折叠已有 3s 读缓存（`snapCacheTTL`）兜底，in-flight 快照每次现算（锁 + 拷贝几条记录）。轮询成本不构成任何问题，本文不再讨论节拍优化。

---

## 2. 设计总览

三层分工：

```
┌─ 数据管道（1s 自适应轮询，骨架沿用现状，忙时 2s→1s）────────────┐
│                                                                │
│  后端唯一改动：InflightRegistry 增加"已结束环"（recently_ended）│
│    remove() 时把该条目的最终快照推入有界环；                    │
│    /stats 载荷新增 recently_ended[] 字段带出终态数据。          │
│                                                                │
│  前端两个机制：                                                 │
│    A. Slot 列表 —— 固定行数、seq 关联、结束行保留并冻结；       │
│    B. 数字 tween —— 可复用的 0.5s 数字过渡动效（console.js）。  │
└────────────────────────────────────────────────────────────────┘
```

核心取舍：**用 UX 设计吸收实时性需求。** 行不消失（①被呈现模型消灭），token 数的变化用动效修饰（②从"闪变"变"流动"）。数据管道因此可以停留在最简单的 1s 轮询，不引入任何推送/条件请求机制，也不引入任何本地计时机制——所有显示值都直接来自最近一次 poll 的快照。

---

## 3. 后端设计：已结束环（recently_ended）

### 3.1 契约

`GET /stats` 载荷新增一个顶层字段：

```json
"recently_ended": [
  { "seq": 41, "state": "ended", "ended_at": "2026-09-15T10:30:05+08:00",
    "ts": "...", "sent_at": "...", "attempt": 1, "provider": "...", "model": "...",
    "key_label": "...", "first_byte_at": "...", "last_byte_at": "...",
    "est_in": 1234, "est_out": 5678, "protocol": "anthropic", "vmodel": "coding",
    "stream": true, "client_key_tag": "...", "addr": "..." }
]
```

- 元素结构与 `InflightEntry` 完全一致，加两个字段：`state: "ended"`（区别于 live 行派生出的 `"queued"`/`"running"`）和 `ended_at`（remove 时刻，RFC3339，与其它时间戳同规范）。
- **`inflight[]` 语义不变**：仍然只含 in-flight 条目。已结束请求走独立字段而不是往 `inflight[]` 里混入 `state:"ended"` 行——LiveStats 专篇的计数平面分离原则（in-flight 注册表只收进行中请求）不因呈现需求破例，且加法式扩展对既有消费方（`/status` 页其它区块、外部监控脚本）零影响。
- 排序约定：`recently_ended` 按 seq 降序（新结束的在前）。前端对两个数组按 seq 统一归并——seq 单调递增，live 行的 seq 必然大于已结束行的 seq，归并即得到"活行在上、结束行按结束先后在下"的自然次序。

### 3.2 实现（`internal/router/inflight.go`）

`InflightRegistry` 增加一个有界环：

```go
type InflightRegistry struct {
    mu    sync.Mutex
    seq   atomic.Uint64
    m     map[uint64]*inflightRec
    ended []InflightEntry // bounded ring, newest last; cap endedRingCap
}
```

- **push 点**：`remove(seq)` 在持锁删除前，先对该 `inflightRec` 调用既有的 `snapshot()` 组装终态条目，补 `state:"ended"` 与 `ended_at: time.Now()`，追加至环尾并按容量裁剪（`endedRingCap = 32`，远大于前端 slot 数，且每条 ~几百字节，内存有界）。
- **读出**：`Snapshot()` 保持只返回 live 条目；新增 `Ended() []InflightEntry` 在持锁下拷贝环内容（环内条目追加后不可变，拷贝即安全快照）。
- `server/stats.go` 的 `adminStats` 读取 `Ended()` 填入新字段；空时序列化为 `[]`。
- 并发面：push 与读出共用既有 `reg.mu`，临界区是两次切片操作，无新锁；每请求生命周期只 push 一次（remove 幂等，`sync.OnceFunc` 已保证）。
- 不落盘、不进账本：已结束环是纯瞬态呈现数据，与 §5.4"两计数平面互不写入"的边界一致，进程重启即清空（页面上表现为 slot 区重置，可接受）。

---

## 4. 前端设计

### 4.1 Slot 列表（`renderLive` 重构）

固定行数的持久化列表，替代 `innerHTML` 整体重建：

- **行数**：`slots = clamp(concurrency.limit + 2, 8, 12)`——并发 6 时给 8 行，封顶 12；limit 未知时取 8。
- **持久 DOM**：维护 `seq → <tr>` 映射，行节点跨 poll 复用。每次 poll 到新载荷后归并 `inflight[] + recently_ended[]`：
  - 新 seq（不在映射中）→ 创建行节点，**插到表格顶部**；
  - 已在映射中的 live 行 → 就地更新变化的单元格（token 数走 tween，Elapsed 直接覆写为新值）；
  - 已在映射中、本次出现在 `recently_ended` 的行 → 打 `ended` 徽标，**冻结全部数值**（Elapsed 定格在 `ended_at - ts`，est_out 定格在终值——后端终态快照保证这是权威最终值，不是前端最后一次看到的值）；
  - 行数超出 slots → 从底部移除节点并清出映射。
- **已结束但还没从环里看到**（环裁剪或进程重启后）：映射里残留的 live 行若连续两次 poll 消失于载荷，同样按 ended 冻结——前端兜底，正常情况下后端环先到。
- **排队提示**：表格上方徽标 `N in queue`（`concurrency.waiting > 0` 时显示），数据用现成字段。
- **Elapsed 列**：无本地计时、无动画。每次 poll 用快照里的 `ts`（live 行）或 `ended_at - ts`（ended 行）算一次、直接渲染；忙时 1s 节拍下自然每秒一跳，这就是全部预期行为。

### 4.2 数字 tween（`console.js` 通用工具）

封装一个页面级可复用的数字过渡函数，暂只接入 Live Requests，机制上面向所有数字会变的场景：

```js
// VMRTween.number(el, to, { formatter }) — 通用数字过渡
//  - 首次渲染（el 无记录值）：直接落值，不动画
//  - 之后每次调用：从当前显示值到 to 做 500ms 的 requestAnimationFrame
//    过渡（ease-out），逐帧整型化渲染
//  - 增减自适应；上一次动画未结束时从当前动画中间值续接，不跳变
//  - 当前值记在元素上（WeakMap），不污染 DOM 属性
```

要点：

- **必须配合持久 DOM**（§4.1）——`innerHTML` 重建会丢掉元素上的旧值，动画无从谈起。这是重构渲染方式的直接原因之一；
- 接入点：Live 表的 `est_in` / `est_out` 列。est_out 流式增长是最主要受益者（500 → 615 的 0.5s 爬升）；est_in 基本不变，走同一入口无额外成本；
- Elapsed 列**不接** tween：它按秒跳变是预期行为，走字动画反而画蛇添足；
- formatter 参数复用 `fmtInt` 等现有格式化函数，千分位等展示规则不因动画改变；
- 命名与挂载：作为 `console.js` 的顶层小工具（与 `fmtInt`、`fmtDur` 同层），后续其它区块的数字变化直接调用，不再各写各的。

### 4.3 轮询节拍

沿用现有自适应骨架：忙时 1s、闲时 15s、`document.hidden` park、`visibilitychange` 唤醒 check-in。唯一改动是 `LIVE_POLL_FAST_MS = 2000 → 1000`。闲时退避保留：slot 区全为 ended 行时没有需要追的数据，1s 空转没有意义。

### 4.4 不变项

401 处理与 `VMRAuth` 协作、整页 5 分钟刷新时钟、Recent Failures / 并发指标区的渲染、错误静默退避——全部保持现状。整页刷新会重置 slot 区（ended 行清空、live 行重建），接受。

---

## 5. 行为示例

并发上限 6，slot 8 行。请求 R41 流式输出中：

```
poll t0:  inflight=[R41 running] recently_ended=[]
          表格：R41(live, est_out 500, Elapsed 3s) + 7 空位

poll t1:  inflight=[R41 running] recently_ended=[]
          R41 Elapsed 直接刷成 4s；est_out 500→615 → tween 0.5s 爬升到 615，行不动

poll t2:  inflight=[] recently_ended=[R41]
          R41 打 ended 徽标，Elapsed 定格 6s，est_out 定格 891（终值来自后端快照）

poll t3:  inflight=[R42 queued] recently_ended=[R41]
          R42 插到顶部；R41 保持在下一行

...R42..R47 陆续进入，8 行占满；R48 到达 → 底部最老的 ended 行被挤出
```

两次 poll 之间没有任何本地更新——页面只在 poll 到达时变化，所有"活"的感觉来自 1s 节拍 + slot 稳定性 + token 动效。

---

## 6. 实现清单

| 文件 | 改动 | 量级 |
|---|---|---|
| `internal/router/inflight.go` | `ended` 环 + `Ended()`；`remove()` 内组装终态快照并 push；`endedRingCap` | ~30 行 |
| `internal/server/stats.go` | `statsResponse` 加 `recently_ended` 字段，`adminStats` 填充 | ~10 行 |
| `internal/server/status.html` | `renderLive` 重构为 slot 持久渲染 + seq 归并 + 排队徽标 | ~70 行 |
| `internal/server/assets/console.js` | `VMRTween.number` 通用数字过渡工具 | ~40 行 |

服务端 Go 约 40 行，零新依赖，零配置项。实现后跑 `go test ./internal/archtest/...`（inflight.go、stats.go 均有行预算约束）。

## 7. 测试策略

1. **已结束环**（`internal/router/inflight_test.go` 风格）：remove 后 `Ended()` 含该 seq 的终态条目，`est_out`/`ended_at` 为 remove 时刻值；环容量裁剪（33 个完成后只剩最近 32 个）；重复 remove 不重复 push；`Snapshot()` 不受环影响。
2. **stats 契约**（`internal/server/stats_test.go` 风格）：`recently_ended` 字段存在且空时序列化为 `[]`；完成一个请求后字段含其终态条目；`inflight[]` 不含 ended 行。
3. **前端结构**（`TestStatusPage_AdaptivePollerStructure` 风格）：断言 slot 渲染、seq 归并、ended 冻结、Elapsed 直刷无 tick 机制、`LIVE_POLL_FAST_MS = 1000`；console.js 断言 tween 工具存在且为 rAF 驱动。
4. 既有 `TestAdaptivePoller_E2E` 改造：节拍断言放宽为行为断言（1s 忙时 / 15s 闲时）。

## 8. 决策与不修项

| 决策 / 不修项 | 理由 |
|---|---|
| 结束标记走独立 `recently_ended[]`，不混入 `inflight[]` | in-flight 注册表只收进行中请求的计数平面边界不动摇；加法式契约对既有消费方零影响；终态快照使前端冻结的是权威最终值而非最后看到的值 |
| 轮询维持 1s 自适应，不引入 ETag/长轮询/SSE | 行消失与数字闪变这两个真实痛点已被呈现层解决，剩余的 1s 数据粒度在 slot 模型下无感知；任何推送机制在此需求下都是负收益。若未来确实需要事件即达，`/stats` 条件请求（ETag 短路）或长轮询可在此契约上增量叠加，本文不展开 |
| Elapsed 无本地计时、无动画 | poll 到多少显示多少，忙时每秒一跳即全部预期；任何走字机制都是无数据支撑的表演 |
| 环只保留 32 条、不落盘、不按时间裁剪 | 32 ≫ slot 上限 12，前端正常总能在环里看到结束行；进程重启清空可接受 |
| done 行不分成败 | in-flight 平面无 outcome 字段；成败区分是 Recent Failures 表的职责，不为视觉破分层面 |
| token 数 1s 粒度 | 慢速流下 token 数字数十秒不涨是真实值不是丢更新；变化瞬间由 tween 修饰 |
| `renderLive` 从 innerHTML 重建改为持久行 | tween 需要跨 poll 的元素记忆；顺带消除整表重建的闪烁与选区丢失 |
