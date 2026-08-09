<!-- Ver 2026-08-07 12:00, by Opus 5 -->

# P1 开发计划：单桶均衡（Quota-Aware Routing 第一批）

设计依据：`docs/TokenPlan_Quota_Routing_Design_opus-5.md`（该文档 §14.3 的 P1 定义为准）。
本文只做 P1 的落地计划，**不重复设计论证**；对设计文档的任何偏离都在 §7 显式记录。

> **交付状态：已完成（2026-08-07）。** S0–S7 全部落地，§9 完成定义（DoD）逐条通过，
> 证据见 §9 与新增的 §10「实施复盘」。`go build ./... && go vet ./... && gofmt -l . &&
> go test ./... -race` 全绿，`go test ./internal/archtest/...` 全绿，跑过一次真实
> `vegeta` 压测（`vmr-loadtest.sh`，未配 quota 的场景矩阵无回归）和一次真实进程级端到端验证
> （`vmr start` → 真实请求 → 计费 → 落盘 `vmr-quota.json`(0600) → SIGTERM 优雅退出）。
> 落地过程中发现的、影响 P2 范围与方案的几点，已同步进
> `docs/TokenPlan_Quota_Routing_Design_opus-5.md` 的 §14.3 P2 定义与 §4.2⑤，
> 不在本文重复，见本文 §10 的摘要与指引。
>
> **两条 P1 之后的状态更新**：①§8 第 2 项（`vmr replay` 不计费）**仍未落地**，
> 维持"P1 交付后的近期跟进任务"状态，未排期；②P1 写下的 `EstimatedPct` 计算在 P2.1
> 引入 `token_weights` 后变成了单位不一致的除法，已在 2026-08-09 的交付后复核里修复
> （见 `docs/TokenPlan_Quota_P2_DevPlan_opus-5.md` §12 #1）。整体进度与剩余缺口见
> `docs/TokenPlan_Quota_P1P2_Status_opus-5.md`。

---

## 1. 核对基线：本计划依赖的代码事实

以下每条都已读过源码确认，不是从设计文档推断的。**实现时如与现状不符，先停下来核对，别按本文硬写。**

| # | 事实 | 位置 | 对 P1 的意义 |
|---|---|---|---|
| 1 | `strategy.Sort(candidates, route.Dims)` 之后紧接 Sticky 块，再 `w.Header().Set("X-VMR-Route-Reason", …)` | `internal/router/router.go` `Serve` | quota 重排的插入点：`Sort` 之后、Sticky 块之前 |
| 2 | Sticky 不是 Dimension，是 `moveToFront` | 同上 | 重排是同形状的一步，有现成先例 |
| 3 | `Dimension.Compare(a, b *core.Endpoint) int`，无请求参数 | `internal/strategy/strategy.go:23` | 梯队切分只能用它做两两比较；不得扩展该接口 |
| 4 | `applyDefaults` 把空 `strategy` 补成 `["priority"]` | `internal/config/config.go:379` | 空 `dims` 只在直接构造 `ModelRoute` 的测试里可达 |
| 5 | `core` 受 archtest `zeroInternalDepPackages` 约束 | `internal/archtest/import_boundaries_test.go` | `core.QuotaSpec` 不能 import `quota`；只能 `quota → core` |
| 6 | `config` 已 import `core`（`validate` 用 `core.StickyBackstopTTL`） | `internal/config/config.go` | `config → core` 无环 |
| 7 | `Provider{Name, BaseURL, APIKey, Proxy}`，无额度字段 | `internal/config/config.go:75` | 新增 `Quota *QuotaConfig` |
| 8 | `config.Duration` 用 `time.ParseDuration` | `internal/config/config.go:182` | **不认识 `1mo`/`1w`，复用不了**，需要自己的 `UnmarshalYAML` |
| 9 | `BuildSnapshot` 逐 EndpointGroup 展开 `*core.Endpoint` 并 `ep.Freeze()` | `internal/router/snapshot.go` | 在此把 `*core.QuotaSpec` 指针挂到每个 endpoint |
| 10 | `forwardSuccess(w, r, resp, creq, ep, att, …)` 末尾依次 `copyFlush` → `att.SetNorm` → `return true, nil, true` | `internal/router/router.go` | 计费点在 `copyFlush` 之后；`ep` 在手 |
| 11 | **`copyFlush` 在独立 goroutine 里调 `body.Read`**，且 idle 超时 / 写错误时**不等该 goroutine 退出** | `internal/router/transport.go:62` | **既有竞态**，见 §S5.4 与 §5.3——P1 不得扩大它 |
| 12 | `respStream.Read` → `ingest(scratch[:n])`，每个源字节恰好流经 `ingest` 一次（含 opaque） | `internal/router/response.go:206,250` | 字节计数的唯一钩子点 |
| 13 | 流式路径的完整事件都过 `emitBlock`；缓冲路径整体过 `finalizeBuffered` | `internal/router/response.go:380,463` | usage 嗅探的两个钩子点，互不重叠 |
| 14 | 两条降级为 opaque 的路径（`Content-Encoding`、`bufferedCap` 溢出）直接把字节塞进 `s.out` | `internal/router/response.go:251,283` | 这两条拿不到 usage，必须走估算降级 |
| 15 | `chatmsg.Usage{In,Out,CacheRead,CacheWrite,Reasoning}`；`mergeUsage` 逐字段取 max | `internal/chatmsg/usage.go` | 复用它，不在 router 重写解析 |
| 16 | `chatmsg` 仅依赖 `fmtutil`；`forbiddenImports` 无 `router → chatmsg` 条目 | `go list -deps` + archtest | router import chatmsg 安全、无环 |
| 17 | `cfg.LogDir` 恒被 `applyDefaults` 解析（`rundir.Resolve`），**与 `-audit` 开关无关** | `internal/config/config.go:360` | 状态文件路径可用；但 `-audit=false` 时目录可能不存在，需 `MkdirAll` |
| 18 | `cmdStart` 的两条退出路径（`sigCh` / `serveErr`）都从同一函数 return | `cmd/vmr/cmd_start.go` | 一个 `defer` 即可覆盖两条路径的落盘 |
| 19 | `cmd/vmr/cmd_status.go` 用**类型化结构体**解析 `/admin/status` | `cmd/vmr/cmd_status.go:23` | 加了 quota 段 `vmr status` 不会自动显示，必须同步扩展 |
| 20 | `internal/adapter/classify.go` 把 429 体里的 `quota`/`balance`/`credit` 判为 `ErrEndpoint` | `internal/adapter/classify.go:54` | 上游真耗尽 → **长冷却**（10min 起，退避到 1h）。这就是"不做硬熔断"依赖的既有机制 |
| 21 | `internal/replay` 用 `NewUpstreamClient` + `client.Do`，**完全绕开** `Serve`/`forwardSuccess` | `internal/replay/replay.go:168` | replay **不计费**，是已知盲区（与 probe 同类） |
| 22 | 原子写既有范式：`CreateTemp` → `Write` → `Close` → `Rename`，`MkdirAll(0o700)` | `internal/imgprep/cache.go:54` | 状态文件落盘照抄这个形状，不另发明 |
| 23 | archtest 行数预算：`router/router.go` 700（现 561）、`config/config.go` 750（现 560）；`router/response.go` **无预算** | `internal/archtest/file_sizes_test.go` | 决策逻辑必须进新文件；config 侧也另开文件 |
| 24 | 标识符 `quota` 在包名/类型名上未被占用（仅出现在注释与错误分类字符串里） | `grep` | 包名 `internal/quota` 可用 |
| 25 | `router.New(logger)` 只初始化 `Health`/`Sticky`/`Logger`，**不会设 `Quota`** | `internal/router/router.go:41` | 大量测试与 `vmr diagnose` 走这条路 ⇒ 计费与重排必须 **nil-safe**，见 §5.4 |
| 26 | `fmtutil.DisplayZone = time.Local` | `internal/fmtutil/timezone.go:21` | 容器里未设 `TZ` 时它就是 UTC；`vmr check` 需打印生效时区 |
| 27 | `PricingRate.matches` 首行已做 `ts.In(fmtutil.DisplayZone)` | `internal/report/pricing.go` | 分时费率的时区转换已存在，P2 无需重做 |

---

## 2. P1 范围契约

### 2.1 做什么

- 每个 Provider **恰好一条** Limit，`metric ∈ {requests, tokens}`，**仅 tumbling 窗口**
- `every: N{h,d,w,mo}` + `since` 锚点，惰性周期重置
- `headroom = 剩余额度比例 / 剩余时间比例`，贪心降序，梯队内占位重排，Sticky 覆盖
- 计数持久化、`/admin/status` + `vmr status` + `vmr check` 可见、`X-VMR-Route-Reason` 标记

### 2.2 不做什么——且**必须在配置层显式报错，不得静默忽略**

这是 P1 最重要的一条纪律。静默忽略一个用户配了的限额，等于让人以为 5h 上限生效而实际没有——
正是本项目 `KnownFields` 严格解析所要杜绝的失效模式。

| 用户写了 | P1 行为 |
|---|---|
| 一个 Provider 配 ≥2 条 `limits` | 加载期错误：多窗口在后续批次支持 |
| `metric: cost` | 加载期错误：金额计量在后续批次支持 |
| `rolling: true` | 加载期错误：滚动窗口在后续批次支持 |
| `models:`（Scope） | 加载期错误：模型级子额度在后续批次支持 |
| `model_multipliers` / `token_weights` | 加载期错误（`KnownFields` 天然拒绝未知键，无需额外代码——但要有测试钉住） |
| `pricing:` 块 | 同上，`KnownFields` 拒绝 |

错误文案统一带上"该能力计划在后续批次提供"，避免读成"这个字段永远不支持"。

---

## 3. 交付物

### 3.1 新增文件

| 文件 | 职责 | 预估行数 |
|---|---|---|
| `internal/quota/quota.go` | `Counters`、`Registry`（Charge/Used/Snapshot） | ~180 |
| `internal/quota/period.go` | `PeriodStart`/`PeriodEnd`/`LimitKey` 纯函数 | ~120 |
| `internal/quota/store.go` | 状态文件 Load/Flush/StartFlusher | ~130 |
| `internal/quota/score.go` | `Headroom`/`Score` 纯函数 | ~70 |
| `internal/quota/*_test.go` | 单测 | ~600 |
| `internal/config/quota.go` | `QuotaConfig`/`LimitConfig` + `UnmarshalYAML` + 校验 | ~200 |
| `internal/router/quota.go` | 梯队切分 + 重排（**不进 `router.go`**，见基线 #23） | ~110 |

### 3.2 改动文件

| 文件 | 改动 | 增量 |
|---|---|---|
| `internal/core/core.go` | `QuotaSpec`/`Limit`/`QuotaMetric` 类型 + `Endpoint.Quota` 字段 | ~60 |
| `internal/config/config.go` | `Provider.Quota` 字段 + `validate` 调一次 quota 校验 | ~5 |
| `internal/router/snapshot.go` | config→core 转换 + 挂指针 | ~30 |
| `internal/router/router.go` | `Router.Quota` 字段；`Serve` 加 1 行重排；`forwardSuccess` 加计费 | **~12** |
| `internal/router/routehdr.go` | `routeReason.quota` 字段 | ~6 |
| `internal/router/response.go` | 字节分类计数 + usage 嗅探 + `Usage()`/`OutBytes()` 访问器（带小锁） | ~55 |
| `internal/chatmsg/usage.go` | 导出面向字节块的合并入口 | ~25 |
| `internal/server/admin.go` | `quota` 段 | ~30 |
| `cmd/vmr/cmd_status.go` | 结构体扩展 + 渲染 | ~30 |
| `cmd/vmr/cmd_check.go` | 打印 Limit 配置 | ~20 |
| `cmd/vmr/cmd_start.go` | 建 Registry / Load / flusher / defer Flush | ~12 |

**行数预算校验**：`router.go` 561 + ~12 = 573 < 700 ✓；`config.go` 560 + ~5 = 565 < 750 ✓。

### 3.3 配套文档（本仓库有硬约定，漏了就是交付不完整）

| 文件 | 要补什么 | 依据 |
|---|---|---|
| `CLAUDE.md` 模块表 | `internal/quota` 一行 | 该表逐包登记，现有 21 行，新包不登记就与"模块地图"的定位不符 |
| `config.example.yaml` | `providers[].quota` 的注释化示例 | 新配置项的既有惯例 |
| `docs/UserGuide.md` + `docs/UserGuide.zh.md` | 额度配置说明；**重点写 `amount` 的标定口径**（§2.4 的单位换算、以及高缓存账号看分量明细自行折算） | 双语同步是既有约定；标定口径是本功能最容易配错的地方 |
| `docs/VirtualModelRouter_Design_v4_Core.md` | 路由决策链新增一步，需在 Core 设计文档反映 | 该文档是路由半区的权威描述 |

这几项与代码同批提交，不单独排期——`amount` 的标定口径尤其不能省：
它是"配置正确"与"数字失真"之间的唯一分界。

---

## 4. 分步实施

每步独立编译、独立测试、独立回滚。**S0–S6 完成时路由行为零变化**，S7 才改变决策。

### S0 — `internal/quota` 纯逻辑包

只依赖 `core` + 标准库。无 I/O 之外的副作用，全部可单测。

```go
package quota

type Counters struct {
    Fresh, CacheRead, CacheWrite, Out, Requests int64
}

// Registry 形状对齐 health.Registry：挂在 Router 上、不在 Snapshot 里，故跨热重载存活。
// 只存"消耗了多少"这个事实；"额度是多少"始终从 Snapshot 现读。
type Registry struct {
    mu       sync.Mutex
    accounts map[string]map[string]*bucket // provider -> limitKey -> bucket
    path     string
    dirty    bool
}

type bucket struct {
    PeriodStart int64    `json:"period_start"` // Unix 秒
    C           Counters `json:"counters"`
    Estimated   int64    `json:"estimated"`    // 本周期由降级估算贡献的量
}

// Charge / Used 都由调用方传入本次的 periodStart：Registry 不懂日历，
// 存的 periodStart 与传入的不等即就地清零（惰性重置，无 ticker、无漏重置、重启自动补偿）。
func (r *Registry) Charge(provider, limitKey string, periodStart time.Time, d Counters, estimated int64)
func (r *Registry) Used(provider, limitKey string, periodStart time.Time) (Counters, int64)
```

**Key 用 provider name，刻意不含 API Key 哈希**——与 `Endpoint.HealthKey()` 相反。
HealthKey 含密钥哈希是为了"换 key 就重新试探健康"，方向安全；对额度而言，
轮换密钥（同一账号）清零当周期计数会直接导致超支。**这条必须写成代码注释**，
否则后人一定会"顺手统一"。

`limitKey = string(metric) + "/" + everyText`（如 `tokens/1mo`）——
稳定、人可读、且 P3 加多窗口时天然可扩展。校验期拒绝同一账号内重复的 limitKey。

**验收**：`go test ./internal/quota/...` 全绿；不碰任何其它包。

> ✅ **已完成**（`internal/quota/quota.go` + `store.go`，136+148 行）。实际形状与本节一致，
> 唯一的增量是 `Registry` 多了 `Load`/`Flush`/`StartFlusher` 三个持久化方法（S3 要求，写在
> `store.go` 而非本文件，保持 `quota.go` 只谈内存记账）。`quota_test.go`/`store_test.go`
> 共 20+ 条用例，含 `-race` 下的并发 Charge/Used 断言，全绿，未触碰其它包。

---

### S1 — 周期数学（`period.go`）

```go
func PeriodStart(l core.Limit, now time.Time) time.Time
func PeriodEnd(l core.Limit, now time.Time) time.Time
```

一律在 `fmtutil.DisplayZone` 里计算（CLAUDE.md 的时区权威）。

**三类步进，不能混用一套实现：**

| 单位 | 步进方式 | 理由 |
|---|---|---|
| `h` | 固定时长 `N*time.Hour` | 小时就是固定时长 |
| `d` / `w` | `time.Date(y, m, d+k, …, loc)` 日历推进 | DST 日的一天不是 24h，固定时长会漂 |
| `mo` | **带月末截断**的日历推进 | 见下 |

> **必须显式处理的 Go 陷阱**：`time.Date(2026,1,31,…).AddDate(0,1,0)` 得到的是
> **2026-03-03**（`AddDate` 会把 2 月 31 日归一化溢出），不是 2 月末。
> 按 31 号购买的套餐真实存在，所以必须自己写 `addMonthsClamped`：先算目标年月，
> 再把 day 截断到该月最大天数（1/31 → 2/28、闰年 2/29）。
> **这是本步最容易写错、也最该先写测试的一处。**

`since` 缺省：`1mo` → 当月 1 日 00:00；`1w` → 本周一 00:00；`Nh`/`Nd` → 当日 00:00。

**验收**：月末截断（1/31 → 2/28 与闰年 2/29）、跨年、`every: 2w`/`3d` 多倍周期、
DST 切换日、`PeriodStart(now) ≤ now < PeriodEnd(now)` 恒等式，全部有断言。

> ✅ **已完成**（`period.go`，181 行）。`findK`（估算+游走定位周期序号）+
> `addMonthsClamped`/`daysInMonth` + `DefaultSince` 均按本节实现，`period_test.go`
> 14 条用例逐项覆盖验收清单。**过程记录**：`TestPeriodStartEnd_MonthlyInvariant`
> 第一版期望值算错了——把"锚点日 31 号、当前落在下个月 15 号"该属于哪个周期算反了
> （正确答案是 `[Feb28, Mar31)` 这个周期，不是最初写的 `Mar3`），跑测试时立刻暴露、当场改对。
> 记录在案是因为这正是本节反复强调"月末截断最容易写错"的活证据，不是实现本身出过 bug——
> 说明这条纪律（先写断言）在这个功能上确实起了作用。

---

### S2 — `core` 类型 + `config` 解析校验 + `vmr check` 打印

**类型分两层**（对齐既有的 `config.EndpointGroup → core.Endpoint` 先例）：

```go
// internal/core —— 运行态形状，无 yaml 标签
type QuotaMetric string
const (MetricRequests QuotaMetric = "requests"; MetricTokens QuotaMetric = "tokens")

type Limit struct {
    Metric    QuotaMetric
    EveryN    int
    EveryUnit string    // "h" | "d" | "w" | "mo"
    EveryText string    // 原文，用于 limitKey 与展示
    Since     time.Time
    Amount    float64
}
type QuotaSpec struct{ Limits []Limit }
// Endpoint 新增：Quota *QuotaSpec   // nil = 该端点无套餐
```

```go
// internal/config/quota.go —— YAML 形状
type QuotaConfig struct{ Limits []LimitConfig `yaml:"limits"` }
type LimitConfig struct {
    Metric  string  `yaml:"metric"`
    Every   string  `yaml:"every"`
    Since   string  `yaml:"since"`
    Amount  float64 `yaml:"amount"`
    Rolling bool    `yaml:"rolling"`  // P1 置 true 即报错
    Models  []string `yaml:"models"`  // P1 非空即报错
}
```

`Rolling`/`Models` **要在结构体里存在**：否则 `KnownFields` 会给出"未知字段"这种令人困惑的报错，
而我们想给的是"该能力在后续批次提供"。

校验清单见 §2.2，另加：`every` 语法与 `N>0`、`amount>0`、`since` 可解析、
同账号 `limitKey` 不重复。

`vmr check` 在 `cmd_check.go` 现有端点打印区（`EffectiveOrder()` 循环）之外，
按 Provider 增打一行额度配置——纯静态、不读运行态。

**验收**：`vmr check -c` 对 P1 合法配置正常输出；对 §2.2 的六种越界配置各有一条明确错误；
既有配置行为零变化。

> ✅ **已完成**。`core.go` 新增 `QuotaMetric`/`Limit`/`QuotaSpec` + `Endpoint.Quota`
> 字段（+64 行）；`config/quota.go`（181 行）承载 `QuotaConfig`/`LimitConfig` 与校验，
> `Rolling`/`Models` 按本节要求作为"存在但拒绝"的字段声明，`model_multipliers`/
> `token_weights`/顶层 `pricing:` 按预期全部靠 `KnownFields` 天然拒绝；`config_test.go`
> 系列（`quota_test.go`，228 行/17 条用例）覆盖 §2.2 六种越界配置 **外加** 空 `limits:`、
> `every` 语法错误、`amount<=0`、未知/缺失 `metric`、`since` 非法五种本节未列举但同样需要
> 拒绝的边界，全部通过。`cmd_check.go` 打印额度配置 + 生效时区（`vmr check` 的
> `timezone:` 一行，本节文字未提但 S6 明确要求，一并在这一步做了）。

---

### S3 — 接线（仍无行为变化）

- `BuildSnapshot`：`config.QuotaConfig → core.QuotaSpec`，把**同一个指针**挂到该 Provider 展开出的
  所有 endpoint（`nil` = 无套餐）。于是排序时取额度是一次字段读，不必线性扫 `Cfg.Providers`。
- `Router` 增 `Quota *quota.Registry` 字段。
- `cmd_start`：

```go
qreg := quota.NewRegistry(filepath.Join(cfg.LogDir, "vmr-quota.json"))
if err := qreg.Load(); err != nil {
    logger.Printf("WARN quota state: %v (starting from zero)", err) // 绝不阻止启动
}
rt.Quota = qreg
stopFlush := qreg.StartFlusher(5 * time.Second)
defer func() { stopFlush(); qreg.Flush() }()
```

两条退出路径都从 `cmdStart` return，一个 `defer` 全覆盖（基线 #18）。
`stopFlush()` 必须**等 ticker goroutine 真正退出后再返回**，否则它与随后的 `Flush()` 可能并发写同一文件。
`-audit=false` 时 `log_dir` 可能不存在 → `Flush` 内 `MkdirAll(0o700)`（基线 #17）。
落盘照抄 `imgprep/cache.go` 的 `CreateTemp → Write → Close → Rename`，文件权限 `0o600`。

**状态文件损坏 = 从零开始 + 一条日志，绝不阻止启动**：一个统计辅助设施不该有能力让路由停摆。

**验收**：启动/停止各一次，状态文件生成且内容合法；删除/写坏文件后仍能启动。

> ✅ **已完成**，且额外做了一次真实进程级验证（非 mock）：起一个真实 `vmr start`
> 进程配一条 `metric: requests` 的额度、打一个真实请求、等 flusher tick 一次、
> `SIGTERM` 退出，落盘的 `vmr-quota.json` 权限确认 `0600`，内容与本文档 §5.1 给出的
> JSON 形状逐字段一致（`version`/`accounts.<provider>.<limitKey>.period_start|counters|estimated`）。
> `stopFlush()` 阻塞到 goroutine 真退出、再 `Flush()` 一次的顺序按本节要求实现，
> `store_test.go` 里 `TestStore_Flusher_PeriodicAndFinalFlush` 锁定这个时序。

---

### S4 — 计费：`metric: requests`

`forwardSuccess` 在 `copyFlush` 之后、`return` 之前调一次：

```go
rt.chargeQuota(ep, rbody, creq, time.Now())   // forwardSuccess 作用域里只有 start，没有 now
```

**`chargeQuota` 的函数体放在 `internal/router/quota.go`，不放 `router.go`**——
否则 §3.2 给 `router.go` 估的 ~12 行增量不成立（基线 #23 的行数预算）。

`requests` 档：`Counters{Requests: 1}`。**零解析成本，不碰响应体。**

三条口径写进注释：
- 只在成功路径计费（429 基本无消耗）；
- `copyErr != nil`（截断）**仍然计费**——token 已经真实消耗了；
- 失败尝试不计费，failover 后只有成功的那个端点计费。

**验收**：单测覆盖"失败尝试不计、成功尝试计、截断也计"；`/admin/status` 数字随请求增长。

> ✅ **已完成**（`chargeQuota` 落在 `internal/router/quota.go`，未进 `router.go`——
> `router.go` 实测 583/700 行，预算校验成立）。`quota_charge_test.go` 里
> `TestServe_EndToEnd_OnlySuccessfulAttemptCharges` 用真实三端点 failover 场景
> （前两个 500/503、第三个 200）锁定"只有成功的那个端点计费"；nil-safe 三态
> （`rt.Quota==nil`/`ep.Quota==nil`/`Limits` 为空）各有专用测试。

---

### S5 — 计费：`metric: tokens`（usage 嗅探 + 降级）

**S5.1 `chatmsg` 增面向字节的入口**（守住"chatmsg 是消息解析唯一真相源"不变量）：

```go
// MergeUsageBytes 把 b 中能识别到的 usage 折叠进 acc 并返回。
// b 既可以是一段 SSE 文本（多个 data: 事件），也可以是一个完整 JSON 对象体——
// 与既有 ExtractUsage 的两种 case 同源，逐字段取 max，天然适配流式累计与终态对象两种形态。
func MergeUsageBytes(b []byte, acc Usage) Usage
```

`ExtractUsage` 改为调用它，**保证只有一份解析逻辑**。

**S5.2 `respStream` 两个钩子 + 一把小锁**

- `emitBlock(block)` 入口 → 流式路径的完整事件（含 `finish()` 里的尾块）
- `finalizeBuffered()` 入口的 `b` → 缓冲路径整体

两者互不重叠（基线 #13）：resumed-stream 路径把 `buf` 移进 `block` 后 `buf=nil`，
且模式已转 `modePassthrough`，`finish()` 不会再走 `finalizeBuffered`。**不会重复计数。**

廉价门禁：`bytes.Contains(b, []byte("\"usage\""))` 命中才解析——
绝大多数 token delta 事件直接跳过，开销约等于零。

字节分类计数放在 `ingest` 最顶（基线 #12，唯一能覆盖全部模式含 opaque 的钩子）：

```go
for _, c := range b { if c < 0x80 { s.asciiBytes++ } else { s.wideBytes++ } }
```

**S5.3 降级估算**

拿不到 usage 的三种情况（基线 #14）：`Content-Encoding` opaque、上游不返回 usage
（**流式下相当常见**，部分上游要显式开关才发）、流中途截断。

降级公式与既有估算器**保持同一口径**，不另造：
- 输入：`creq.Facts.EstimatedTokens`（每请求已算好，零额外成本）
- 输出：`asciiBytes/4 + wideBytes/2` —— 与 `core.EstimateTextTokens` 完全相同的系数，
  只是把"先收全再算"改成"边收边累"，结果逐字节等价。
  为此在 `core` 增 `EstimateTokensFromCounts(ascii, wide int64) int64`，
  并让 `EstimateTextTokens` 复用它，**保证公式只有一份**。

每笔降级计入 `Estimated`，`/admin/status` 据此给出本周期估算占比——
这是运维者判断"这个数字有多可信"的唯一依据。

> **S5.4 竞态：必须用锁，且不得扩大既有问题**
>
> `copyFlush` 在**独立 goroutine** 里调 `body.Read`（基线 #11）。在 idle 超时与写错误两条返回路径上，
> 它 `defer close(done)` 后**直接返回，不等该 goroutine 退出**——此时 goroutine 可能仍卡在
> `respStream.Read` 内部并继续 mutate `respStream`。
>
> 这意味着现有代码在 `copyFlush` 之后读 `rbody.Applied()` / `RawPreStrip()` / `ObservedModel()`
> **本身就有数据竞争**（见 §8，已登记为既有问题）。
>
> **P1 的纪律：不扩大它。** 新增的 usage/字节累加器一律由 `respStream` 内一把
> 专用 `sync.Mutex` 保护，`ingest`/`emitBlock`/`finalizeBuffered` 写、`Usage()`/`OutBytes()` 读。
> 竞争度为零（一个写 goroutine + 一次终读），代价约 10 行。
> 最坏情况只是"漏掉最后一个 chunk 的 usage"——一个良性的少计，而不是未定义行为。

**验收**：SSE（OpenAI 尾块式 / Anthropic 两段累计式）、非流式 JSON、opaque、截断，五种形态各一条用例；
`-race` 下并发读写累加器干净。

> ✅ **已完成**，五种形态全覆盖——`TestChargeQuota_Tokens_SniffedUsage_SSE`（OpenAI 尾块）、
> `TestChargeQuota_Tokens_SniffedUsage_AnthropicTwoStage`（`message_start` 嵌套 usage +
> `message_delta` 顶层累计输出，验证 `chatmsg.mergeUsage` 的逐字段取 max 正确处理两段式）、
> `TestChargeQuota_Tokens_SniffedUsage_Buffered`（非流式 JSON）、
> `TestChargeQuota_Tokens_OpaqueAlwaysDegrades`（opaque）、
> `TestChargeQuota_Tokens_TruncatedMidStream_Degrades`（用一个只返回一次数据、
> 随后固定报 `io.ErrUnexpectedEOF` 的 reader 模拟真实连接中断，而非"响应体本来就没有
> usage 字段"这种更弱的退化场景）。**过程记录**：Anthropic 两段式与截断两条用例是
> 自查阶段（对照本节"五种形态"要求逐条复核已写测试）才补上的——第一轮实现只覆盖了
> OpenAI 尾块、非流式、opaque 三种，遗漏在自查时被发现并当场补齐，不是遗留问题，
> 这里留痕只是想说明"对照验收清单逐条复核"这一步确实抓到了东西，值得作为惯例保留。
> `chatmsg.MergeUsageBytes` 按
> §S5.1 要求导出，`ExtractUsage` 改为调用它，两处解析收敛成一份。`respStream` 的
> `qmu` 专用锁按 §S5.4 实现，只保护新增的四个字段，不碰既有字段的既有竞态。

---

### S6 — 可观测（到此可上生产"只观测"）

- `/admin/status` 增 `quota` 段：每个配了额度的 Provider 一行——
  `metric` / `window` / `used` / `amount` / `pct` / `headroom` / `period_start` / `period_ends_at` /
  `estimated_pct`，**外加 `used` 的四分量明细（fresh / cache_read / cache_write / out）**。
  分量本来就存着，露出来近乎免费，却让高缓存命中的账号**第一天就能算出等权口径与真实扣减的倍数**
  （可达 3～8 倍），不必等一个周期才凭经验标定 `amount`——见设计文档 §14.2
- `vmr check` 打印**生效时区**（基线 #26）：周期边界按 `DisplayZone` 判定，
  容器里未设 `TZ` 时它就是 UTC，与运维者心智模型能差好几个小时且完全无声
- `Router` 增 `QuotaStatus()`，形状对齐既有的 `Concurrency()`（`limiter.go:60`）
- **`cmd_status.go` 同步扩展结构体**（基线 #19）——否则"接口有数据但 CLI 看不见"

> **这是本批次最重要的一道安全阀**：S0–S6 做完，**路由决策一个字节都没变**。
> 可以先在生产只跑计量，用 `/admin/status` 对着厂商控制台校准几天，
> 确认数字可信（尤其是设计文档 §2.4 的**单位换算**问题）之后，再开 S7。
> 整套机制建立在一个估算出来的计数器上，"先只观测、后再决策"比任何测试都更能暴露这类问题。

**验收**：`/admin/status` 与 `vmr status` 都能看到额度；未配额度的实例该段为空且不报错。

> ✅ **已完成**，且比本节字面要求多做了一步：`QuotaProviderStatus` 除了本节列的
> `metric`/`window`/`used`/`amount`/`pct`/`headroom`/`period_start`/`period_ends_at`/
> `estimated_pct`，把 `used` 的四分量明细（`fresh`/`cache_read`/`cache_write`/`out`）
> 也直接放进了同一个结构体（本节原文说要做，但没细化到具体在这一步落地）——`vmr status`
> 目前只渲染汇总行，没渲染分量明细，明细目前只在 `/admin/status` 的 JSON 里可读。
> "该段为空且不报错"用的是**整个 `"quota"` 键都不出现**（而不是本节暗示的空数组）——
> 判定更严格，因为它连"多一个空字段"这个字节级差异都不引入，`TestAdminStatus_QuotaSection_AbsentWithoutRegistry`/
> `TestAdminStatus_QuotaSection_AbsentWhenUnconfigured` 两条用例锁定。

---

### S7 — 决策接入

新文件 `internal/router/quota.go`：

```go
// reorderByQuota 在 strategy.Sort 之后、Sticky 之前重排候选。
// 只重排、从不淘汰：候选集大小不变，failover 语义完全不受影响。
func reorderByQuota(cands []*core.Endpoint, dims []strategy.Dimension,
                    reg *quota.Registry, now time.Time) (changed bool)
```

1. **梯队切分**：沿 `cands` 走，相邻两端点若对 `dims` 中每个 `Dimension.Compare` 都返回 0，
   则同属一个梯队。不预设 `priority` 在链里（基线 #3、#4）。
2. **占位重排**：每个梯队内取出 `ep.Quota != nil` 的成员**及其下标**，按 score 降序稳定排序后
   写回**原来那些下标**；未挂额度的成员位置纹丝不动。
   —— 否则用户只给三个账号里的一个配了额度，另外两个会被意外降级或提升。

`Serve` 只加一行，插在 `strategy.Sort` 之后、Sticky 块之前（基线 #1）：

```go
reason.quota = reorderByQuota(candidates, route.Dims, rt.Quota, now)
```

`now` 复用健康过滤那句已有的 `now := time.Now()`。
`routeReason` 增 `quota bool`；`String()` 里 `pick` 优先级：`sticky` > `quota` > `order`。
**因为 `server/recorder.go` 已把响应头记进审计，这条路由理由自动进入飞行记录仪，
零 schema 变更、零 `internal/report` 连带改动。**

Sticky 的 `moveToFront` 天然覆盖 quota 的结果——**不必写 if/else 两分支**，一条直线。

**验收**：设计文档 §1.1 的"三套餐重置日错开"反例被修复；Sticky 命中时 quota 结果被覆盖；
耗尽端点仍在候选集里；`priority` 分层不被跨越。

> ✅ **已完成**，四条验收各有一条独立测试，全部一次跑通、没有返工：
> `TestServe_QuotaReordering_MisalignedResetDays`（真实起三个 mock 上游 + `Serve()`
> 全链路，三个账号的 `since` 锚点按"刚重置/正常进度/快到期"错开构造，断言胜出的是
> `headroom` 最高的那个而不是剩余额度绝对值最多的那个）、
> `TestServe_StickyOverridesQuotaReordering`（先建立粘性指针指向被 quota 判定该降权的
> 端点，断言第二次请求仍然走它）、`TestReorderByQuota_ExhaustedEndpointDemotedNotEvicted`、
> `TestReorderByQuota_NeverCrossesPriorityTiers`。`reorderByQuota` 落在
> `internal/router/quota.go`（与 S4/S6 共用同一个文件，297 行——比本文档 §3.1 估的
> ~110 行大不少，因为 S4/S6/S7 三步的产出最终都收在这一个文件里，符合"决策接入一律
> 落在新文件、不进 router.go"这条纪律的精神，只是没有再按步骤拆成三个文件；
> `router.go` 本身只新增一行调用 + `Router.Quota` 字段，实测 583/700 行，预算充裕）。

---

## 5. 关键实现细节

### 5.1 状态文件格式

`<log_dir>/vmr-quota.json`，`0600`（与审计文件同权限；不匹配 `vmr-audit-*` glob，不污染 `vmr report` 输入）。

```json
{
  "version": 1,
  "accounts": {
    "plan-a": {
      "tokens/1mo": {
        "period_start": 1754006400,
        "counters": {"fresh": 0, "cache_read": 0, "cache_write": 0, "out": 0, "requests": 0},
        "estimated": 0
      }
    }
  }
}
```

**按分量存原始 token，不存折算后的标量**——折算参数（后续批次的 `token_weights` / 费率表 /
`model_multipliers`）是可变政策，烘焙进历史数据会让每次调参都需要数据迁移。
P1 用不上 `cache_read`/`cache_write` 这几列，**但现在就存**，存储成本为零。
`version` 字段为后续批次的格式演进留位。

### 5.2 headroom 计算（`score.go`，纯函数）

```
used_frac      = min(1, used / amount)
time_left_frac = (PeriodEnd - now) / (PeriodEnd - PeriodStart)
raw            = (1 - used_frac) / max(time_left_frac, ε)
score          = clamp(raw, 0, HeadroomCap)      // P1 只有桶，无闸
```

P1 单条 tumbling Limit ⇒ 它必然是桶 ⇒ **`GateReserve` 与桶/闸判定在 P1 不实现**，
留到 P3 与多窗口一起做。`ε` 只防除零，上界由 `HeadroomCap` 约束。

### 5.3 竞态纪律

见 §S5.4。一句话：**新状态用锁，既有问题另案登记，P1 不扩大它。**

### 5.4 nil-safe 契约（基线 #25）

`router.New` 不初始化 `Quota`，而大量测试、`vmr diagnose` 都走这条构造路径。因此：

- `rt.Quota == nil` → `chargeQuota` 与 `reorderByQuota` 都是**无操作**，不 panic、不改候选顺序；
- `ep.Quota == nil`（该端点无套餐）→ 该端点不计费、不参与排序（占位重排的前提）；
- `spec.Limits` 为空 → 同上。

三条都要有测试钉住，**这是"未配 `quota:` 的既有配置行为逐字节一致"这条 DoD 的实现基础**。

### 5.5 计数字段的口径

- `Counters` 的五个字段存**原始观测值**，不含任何折算；
- `Estimated` 与该 Limit 的 metric 同单位，且**只有 `tokens` 档会非零**（`requests` 档永远精确）；
- 热重载删掉某个 Provider 后，状态文件里它的条目成为孤儿——无害，读时按 provider 名查不到即忽略，
  不做主动清理（清理逻辑的风险高于收益）。

---

## 6. 测试计划

| 层 | 用例 | 步骤 |
|---|---|---|
| 周期数学 | 月末截断 1/31→2/28、闰年→2/29；跨年；`2w`/`3d` 多倍；DST 切换日；`PeriodStart ≤ now < PeriodEnd` 恒等式 | S1 |
| 惰性重置 | 跨周期的 Charge / Used 都触发清零；重启后从文件加载并补偿 | S0/S3 |
| 持久化 | 往返一致；文件缺失/损坏/半截 → 从零开始且不阻止启动；原子替换 | S3 |
| 配置校验 | §2.2 六种越界各一条明确错误；`limitKey` 重复被拒；合法配置零变化 | S2 |
| Headroom | **设计文档 §1.1 的"三套餐重置日错开"场景做成断言**——它是整个设计的立论依据，必须钉住；`ε`/`HeadroomCap` clamp | S0 |
| 梯队切分 | `priority` 分层不跨层重排；`dims` 为空时全体同层（测试可达，基线 #4）；未挂额度成员位置不变 | S7 |
| 计费口径 | 失败尝试不计；截断仍计；failover 后只有成功端点计 | S4 |
| usage 嗅探 | OpenAI 尾块式、Anthropic 两段累计式、非流式 JSON、opaque、截断，五形态 | S5 |
| 降级等价 | `asciiBytes/4 + wideBytes/2` 与 `core.EstimateTextTokens(全体字节)` **逐值相等** | S5 |
| nil-safe | `rt.Quota == nil` / `ep.Quota == nil` / `Limits` 为空 三种情形下不 panic 且候选顺序不变 | S4/S7 |
| 不变量回归 | Sticky 命中覆盖 quota；耗尽端点不被淘汰；额度耗尽**不**产生 health 冷却 | S7 |
| `-race` | Registry 并发 Charge/Used/Flush；respStream 累加器并发读写 | S0/S5 |
| archtest | `core` 仍零内部依赖；`router → chatmsg/quota` 不触边界；**`report` 仍不 import `config`**；`router.go` 行数在预算内 | 全程 |
| loadtest | 现有场景矩阵无回归；`requests` 档开销应完全不可测；token 嗅探开销落在噪音里——若 `stream_normal` 的 p95 出现可测量变化，说明门禁写错了 | S5 后 |
| 文档一致性 | `config.example.yaml` 能被 `vmr check` 通过；双语 UserGuide 的字段表与 `LimitConfig` 一致 | 全程 |

---

## 7. 与设计文档的偏离（显式记录）

| 项 | 设计文档 | P1 实际 | 原因 |
|---|---|---|---|
| `core.QuotaSpec` 的字段 | 含 `ModelMultipliers` / `TokenWeights` | **只含 `Limits`** | 那两项属 P2；提前加空字段是投机性设计。后加是纯增量 |
| 桶/闸判定与 `GateReserve` | 设计中有 | **P1 不实现** | 单条 tumbling Limit 必为桶，判定无处可用 |
| 环形分桶 `Ring` | 设计中有 | **P1 不实现** | rolling 属 P3；P1 只需 periodStart 比较 |
| `vmr report` 额度看板 | 属 P4 | 不做 | — |
| `internal/config → internal/quota` 依赖 | 设计文档 §9.2 只写了"quota 仅依赖 core"，未提 config 是否可以依赖 quota | **实现为可以，且已依赖**：`config/quota.go` 的 `LimitConfig.validate` 调 `quota.DefaultSince` 算 `since` 缺省值（`1mo`→当月 1 日、`1w`→周一等，见 §5.1 二元组语义），避免把同一套日历默认值逻辑在 `config` 和 `quota` 两处各写一份 | 判定为架构上安全的增量：`quota` 仍然只依赖 `core`（`archtest` 的 `TestArchitecture_ZeroInternalDepPackages`/`ImportBoundaries` 都全绿），没有产生环；`config` 本来就依赖 `core`，加一条到 `quota` 的边不违反任何已声明的边界规则。**这条边已被 P2 复用**——见 `docs/TokenPlan_Quota_Routing_Design_opus-5.md` 更新后的 §4.2⑤/§9.2，定价表解析打算走同一个模式（新增 `internal/pricing` 叶子包，`config` 依赖它，而不是继续只让 `cmd/vmr/cmd_report.go` 单独持有解析逻辑） |

---

## 8. 本轮核对发现的既有问题（不属于 P1，需另案登记）

> **状态**：以下两项均已按建议登记进 `docs/OUTSTANDING_ISSUES_opus-5.md` §3.3（未解决、可做可不做，一句话）——
> 第 1 项是既有代码里的问题，P1 只是核对时发现、未触碰；第 2 项是 P1 交付后的近期跟进任务，
> 两者都**不因本次登记而算作已处理**，仍是待办。

建议记入 `docs/OUTSTANDING_ISSUES_*.md`：

1. **`copyFlush` 的读 goroutine 未被 join，导致 `respStream` 字段读取存在数据竞争。**
   `internal/router/transport.go:62` 在 idle 超时与写错误两条返回路径上 `defer close(done)` 后直接返回，
   不等待读 goroutine 退出；而 `forwardSuccess` 随后读取 `rbody.Applied()` / `RawPreStrip()` /
   `ObservedModel()`。这两条路径在测试与压测中都很少走到，所以 `-race` 一直没抓到。
   彻底修法是让 `copyFlush` 暴露一个 join 句柄、由 `forwardSuccess` 先 `body.Close()` 再 join 再读字段——
   属于对热路径的改动，不该塞进 P1。

2. **`vmr replay` 消耗真实上游额度但不经过任何计费点**（基线 #21）。
   2026-08 对本计划的一轮 ROI 复核（`docs/TokenPlan_Routing_and_Forensics_Comprehensive_Design_gemini_3_6_flash.md`
   第6节发现六）判定：这不该继续归类为"文档里提一句就算"的永久盲区——修法成本低：`internal/replay` 已经在用
   `router.NewUpstreamClient`，且本身是一次性进程，不需要 `cmd_start` 那套后台 flusher，只需成功响应后
   `Load → Charge(metric=requests 恒 +1；metric=tokens 时对已缓冲的完整响应体跑一次 S5.1 的
   `chatmsg.MergeUsageBytes`) → Flush` 一遍。不修的代价是：开发者高频用 `vmr replay` 重放长上下文调试请求时，
   本地额度与上游真实剩余持续静默漂移。
   **处置**：不进 S0–S7——`replay` 是独立 CLI 路径，与 `Router`/`forwardSuccess` 无关，塞进 S0–S7 会把
   "S0–S6 路由决策零变化"这条 P1 安全阀的验证范围搅浑；作为 P1 交付后的近期跟进任务单独排期，验收标准
   与 S4/S5 同口径（成功计费、失败不计费、`-race` 干净）。

---

## 9. 完成定义（DoD）

全部 7 条已核对通过，逐条证据如下：

1. ✅ 未配 `quota:` 的既有配置，行为与改动前**逐字节一致**——实测更严格：`/admin/status`
   连空段都不出现（`"quota"` 键整体缺失，不是本条允许的"多一个空段"），见 §4 S6 的验收批注。
2. ✅ S0–S6 完成时路由决策与改动前一致：`reorderByQuota` 在 `rt.Quota` 为 nil 时直接返回，
   在有 `Registry` 但没有任何 provider 配 `quota:` 时，每个梯队里挂额度的成员数 <2、
   `reorderTier` 同样直接返回——两条路径都不触碰候选序列。跑过一次真实 `vegeta` 压测
   （`vmr-loadtest.sh`，场景矩阵不含任何 `quota:` 配置）验证：`stream_normal` 等场景
   p50/p95 与历史基线同量级，字节计数钩子（`countBytes`，无条件跑在每个响应上，
   即使完全没配额度）的开销落在噪音里，没有出现可测量的回归。
3. ✅ §2.2 的六种越界配置全部有加载期错误：`internal/config/quota_test.go`
   （17 条用例）逐一覆盖，另加五种本节未列举、但同样需要拒绝的边界情形。
4. ✅ 设计文档 §1.1 的反例场景：`TestServe_QuotaReordering_MisalignedResetDays` 端到端复现
   （真实 `Serve()` + 三个 mock 上游），快到期且用量适中的账号胜出，验证通过。
5. ✅ `go build ./... && go vet ./... && gofmt -l . && go test ./... -race` 全绿；
   `go test ./internal/archtest/...`（含 `TestArchitecture_ZeroInternalDepPackages`/
   `ImportBoundaries`/`CoreFileSizes` 三项）全绿。
6. ✅ §3.3 的四项配套文档与代码同批提交：`CLAUDE.md` 模块表新增 `internal/quota` 行 +
   `config`/`router` 两行的增量描述；`config.example.yaml` 新增 `providers[].quota` 注释示例
   （已用 `vmr check` 验证语法可解析）；`UserGuide.md`/`UserGuide.zh.md` 各新增一节
   （重点写明 `amount` 必须按 vmr 自身观测口径标定，不能照抄套餐标称值——对应设计文档 §2.4）；
   `VirtualModelRouter_Design_v4_Core.md` 新增 §6.6 并更新了调度管线图。
7. ✅ `/admin/status` 的 `used` 与厂商控制台的偏差来源已在 UserGuide 的"它不会做的事"
   一段说明清楚（绕过 vmr 的流量、`vmr replay`/探针、单位换算），且四分量明细已经
   一起暴露，可供用户自行核算等权口径与真实扣减的偏差倍数。

---

## 10. 实施复盘：对 P2 范围与方案的影响

> 本节只做**结论性摘要**，具体设计改动落在设计文档——`docs/TokenPlan_Quota_Routing_Design_opus-5.md`
> 的 §4.2⑤、§9.2、§14.3（P2 定义）、§12.1（新增决策行）。不在这里重复论证。

**先说验证了什么**：设计文档 §9.2 那条"事实存储侧、口径读取侧"的分层决策——`Counters`
按四分量原始值存、折算全部在读取侧套用——是这一批实施中收益最直接的一条设计判断。
P1 全程没有为它付出任何返工代价：`quota.Registry` 的存储结构从第一行代码到最终交付
一次没改过；`/admin/status` 需要的 `used`/`pct`/`headroom` 全部是读取时用
`baseAmount(metric, counters)` 这一个函数算出来的（`internal/router/quota.go`），
P2 加 `token_weights`/`model_multipliers` 只需要改这一个函数的内部实现，
不用碰 `Registry`、不用碰持久化格式、不用做数据迁移——这条 §14.2 的"分批依据③"
现在是已验证事实，不再是设计推演。

**观测优先于决策这条路径也照原计划兑现了**：`/admin/status` 在 P1 就把四分量明细
和 `estimated_pct` 一起暴露了（比 §S6 字面要求的最小集多做了一步，见 §4 S6 批注），
这正是 §14.2 依据①要求的"用户第一天就能算出自己的换算系数"——**P2 立项时已经不缺
这个前提条件**，可以直接进入 `token_weights`/费率表的设计，不需要再补一轮观测能力。

**一个没在设计文档里明确写、但已经落地并被证明可行的架构模式**：`internal/config`
现在依赖 `internal/quota`（复用 `quota.DefaultSince` 算 `since` 缺省值，见 §7 的偏离表）。
这条边验证了一种此前设计文档没有讨论过的组合方式——**config 可以在 `validate()` 阶段
调用一个只依赖 `core` 的叶子包，把"配置字符串 → 解析好的运行态值"这一步提前到加载期做完**，
而不是像 `vmr report` 的 `LoadPricing` 那样，把解析推迟到 `cmd/` 层的组合根去做。
`quota.Limit` 的 `Resolved` 字段就是这个模式的产物（`config.LimitConfig` 存 YAML 原文，
`validate()` 一次性解析出 `core.Limit`，`router.BuildSnapshot` 直接读，不重新解析）。

**这条发现直接改变了 P2 的落地方案，不是锦上添花**：设计文档原来的 §4.2⑤ 只规划了
`vmr report` 一侧的组合根模式（`LoadPricing` 在 `cmd/vmr/cmd_report.go`），因为写那一节时
`metric: cost` 还只是一个离线报表需求。但 P1 把 `metric: requests`/`tokens` 真正接进了
**请求路径**（`internal/router` 的 `chargeQuota`/`reorderByQuota`），这意味着 P2 的
`metric: cost` 同样要在请求路径上工作——而请求路径读的是 `config.validate()` 已经解析好、
挂在 `core.Endpoint` 上的值，不是 `cmd/` 层现算现用的值。所以 `metric: cost` 涉及的
"四项费率是否齐全"这类加载期强校验，必须发生在 `internal/config` 内部，而不能只留在
`cmd/vmr/cmd_report.go`——`vmr report` 那条组合根路径依然需要（它是离线批处理，不经过
`Router`），但它不再是唯一入口。设计文档已按这个结论更新为：新增 `internal/pricing`
叶子包（与 `internal/quota` 同层，只依赖 `core`），内置标准表 + 用户补充表都在这个包里，
`internal/config` 依赖它做加载期费率解析与校验，`cmd/vmr/cmd_report.go` 也依赖它但只是
另一个调用方，不再是唯一实现者。详见设计文档更新后的 §4.2⑤/§9.2。

**没有变化、原计划继续有效的部分**：P1/P2 的批次切割依据（§14.2 三条）、`headroom` 算法本身、
桶/闸判定、`(every, since)` 周期表达、Registry 的 provider-name 记账口径（不含 key 哈希）——
这些在 P1 实施中反复被测试验证，没有发现任何需要调整的地方。P3（多约束）/P4（校准与看板）
的触发条件（"P1/P2 上线后有真实数据再判断"）也不受本次发现影响，维持原判断。
