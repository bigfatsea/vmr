<!-- Ver 2026-08-06 22:24, by Opus 5 -->

# 阶段一设计：Token-Plan 额度感知路由（Quota-Aware Routing）

配套文档：`docs/TokenPlan_Routing_and_Forensics_Strategy.md`（战略与竞品，定义了"为什么做"）。
本文只解决"怎么做"，范围严格限定在 Router 侧。

---

## 1. 问题定义

### 1.1 要优化的目标函数

先把目标写清楚，否则很容易设计出一个"看起来在均衡、实际没解决问题"的算法。

团队手上有 N 个按周期打包的 Token-Plan（每个绑定一个厂商账号），外加 0~M 个按量计费端点。
每个 Plan 的额度**周期性作废、不结转**。真正要最小化的是：

```
浪费 = Σ(周期结束时未用完的套餐额度) + Σ(套餐用尽后溢出到按量计费的花费)
```

这两项是同一枚硬币的两面：把流量喂给 A 就是不喂给 B。所以这不是一个"负载均衡"问题
（那是要均分压力），而是一个**"在每个套餐各自过期之前恰好把它烧完"的配速问题**。

这个区分很关键，它直接否掉了战略文档里那个直觉公式 `Effective Weight = remaining / total`：

> 真实场景：三个月度套餐，购买时间不同 → 重置日分别是 1 号、15 号、20 号。今天是 16 号。
> A 已过完 50% 周期，B 刚重置（过完 3%），C 已过完 87%、4 天后作废。
> 按 `remaining/total` 加权，B 剩余比例最高、拿走绝大部分新会话；
> **而真正快要作废、最该被优先烧掉的 C，权重最低。**

也就是说，剩余比例这个"存量"信号，在重置日不对齐时会给出系统性反向的结论。而重置日不对齐
是常态而非例外——套餐是一个一个买的。

### 1.2 范围

**做**：月度/日度 Token 总量桶型套餐的用量本地累计、跨账号的新会话配速分配、状态持久化与周期重置、
可观测性。

**不做**（各有理由，见第 10 节）：滚动时间窗口限流型套餐（Claude Code Max/Pro 那一类）、
按下游用户/业务线的实时配额、多实例共享计数、金额（$）预算、额度耗尽的硬熔断。

---

## 2. 现状盘点：请求路径上已经有什么

设计必须落在这些既有事实上，先列清楚（均已核对代码，非推测）：

| 事实 | 位置 | 对本设计的意义 |
|---|---|---|
| `strategy.Sort` 是稳定排序，按 `Dimension` 链排序，`Dimension.Compare(a, b *core.Endpoint)` **看不到请求** | `internal/strategy/strategy.go` | 权重调度不能做成 Dimension——它需要请求态与跨端点共享状态 |
| Sticky 不是 Dimension，而是 `Sort` 之后的一次 `moveToFront` | `internal/router/router.go` `Serve` | **已有先例**：请求态、有状态的重排就该放在 Sort 之后独立一步 |
| `Health.Registry` 挂在 `Router` 上、不在 `Snapshot` 里，因此跨热重载存活 | `internal/health/health.go` | 计数器要跨热重载存活，套用同一形状 |
| Sticky 命中判定已经在 `Serve` 里算好（`Peek` 命中 + TTL 有效） | `router.go` | "是不是新会话"这个信号**现成可用，零额外成本** |
| `chatmsg.ExtractUsage` 已能解析 4 种 usage 形态（OpenAI/Anthropic × JSON/SSE），拆出 `In/Out/CacheRead/CacheWrite/Reasoning` | `internal/chatmsg/usage.go` | 计量口径不需要新写解析器；`chatmsg` 仅依赖 `fmtutil`，router 引用它无循环、不违反 archtest |
| `respStream` 已经在做 SSE 事件切分与逐事件扫描（model 改写、think 剥离） | `internal/router/response.go` | usage 嗅探可以挂在已有的扫描上，不新增一遍遍历 |
| `core.EstimateTextTokens` 已导出，为共享的粗估公式 | `internal/core/core.go` | 拿不到 usage 时的降级估算不必另造轮子 |
| `X-VMR-Route-Reason` 由 `Serve` 写入响应头，且 recorder 会把响应头写进审计记录 | `routehdr.go` / `server/recorder.go` | 路由理由**自动进审计**，无需为可解释性新增字段 |
| `router.go` 现 561 行，archtest 预算 700 行 | `internal/archtest/file_sizes_test.go` | 新逻辑放独立文件，不往 `router.go` 里堆 |
| 配置 `KnownFields(true)` 严格解析；`Provider{Name, BaseURL, APIKey, Proxy}` 目前无额度字段 | `internal/config/config.go` | 新增字段即新增契约，需同步校验 |
| `cmd_start.go` 已处理 SIGINT/SIGTERM + `srv.Shutdown` 优雅退出 | `cmd/vmr/cmd_start.go` | 有现成的落盘时机 |

---

## 3. 核心设计：紧迫度（Urgency）

### 3.1 一个数取代一套权重规则

对每个配了额度的 Provider，定义：

```
remaining = max(0, quota.tokens - consumed_this_period)
time_left = 距本周期结束的秒数（下界 clamp，见 3.3）
urgency   = remaining / time_left          // 单位：tokens/秒
```

`urgency` 的物理含义是：**为了在作废前刚好烧完，这个套餐现在必须维持的消耗速率。**

它一举解决了 1.1 提的所有问题，且没有任何可调参数：

* **重置日不对齐**：C 剩 20M、剩 4 天 → 58 tokens/s；B 剩 100M、剩 29 天 → 40 tokens/s。C 胜出，符合直觉。
* **套餐容量不同**：量纲是速率，100M 的套餐和 10M 的套餐可直接比较，不需要归一化。
* **自稳定**：如果一个套餐正好按线性进度消耗，分子分母同比例缩小，`urgency` 恒定。偏离会自动纠正——
  超支 → urgency 下降 → 少接新会话；欠用 → urgency 上升 → 多接新会话。这是一个**无需整定的比例控制器**。
* **耗尽即退场**：`remaining = 0` → `urgency = 0` → 自然排到最后，不需要一条单独的"耗尽"分支。

### 3.2 分配策略：贪心最高紧迫度，无随机、无累加器

战略文档里提的平滑加权轮询（SWRR）在这里是多余的。因为 `urgency` 在每次计费后都会更新，
**直接取当前 urgency 最高者**就已经收敛到均衡：

> A(100) 拿走新会话 → A 的 consumed 上升 → A 的 urgency 下降 → 跌破 B(10) 后流量自动转向 B。

于是分配规则退化成一次排序：**同一梯队内按 urgency 降序稳定排序**。

这比 SWRR 好在四处：

1. **无状态**：不需要 per-provider 的平滑累加器，也就不需要考虑它的持久化、溢出、归一化。
2. **确定性**：不引入 `math/rand`，同样的输入必然给出同样的顺序，测试可断言、线上可复现。
3. **failover 顺序自动正确**：排序是对整个梯队做的，首选失败后的第二、第三顺位天然也按紧迫度排列；
   SWRR 只回答"选谁"，剩下的顺序还得另想办法。
4. **对 Prefix-Cache 友好**：贪心策略会把新会话在一段时间内集中到同一个账号，而不是均匀撒开——
   这与整个项目"保住 prompt cache"的主线目标同向。轮询式撒开反而是在制造缓存分裂。

代价是分配粒度较粗（在两个套餐 urgency 交叉之前，流量会持续压在一个账号上）。这在真实场景里
不是问题：Agent 会话本身是长尾的、稀疏的，而账号侧的并发上限触发会返回 429 → 走既有 health 冷却
与 failover，已有机制兜住。

### 3.3 边界处理

* **周期末尾的除零/爆炸**：`time_left` 下限 clamp 到 1 小时。到期前最后 1 小时内，urgency 数值上会
  冲得很高——这是**正确行为**（用不掉就作废），clamp 只是防止除以趋零值产生无意义的极值。
* **未配额度的端点**：不参与紧迫度排序（见 4.2 的"占位重排"），既不提升也不降级。
* **`consumed > tokens`（超额）**：`remaining` 截断到 0，不出现负权重。

---

## 4. 调度流程

### 4.1 在 `Serve` 中的位置

```
health 过滤
  → 硬 Condition 过滤
  → 上下文长度过滤（带兜底）
  → strategy.Sort(candidates, dims)          [不变]
  → quota 重排 (新增)                         ← 本设计
  → Sticky moveToFront                        [不变，优先级最高]
  → failover 循环
```

三条不变量：

1. **quota 重排在 `Sort` 之后**：绝不跨越 Dimension 链已经确立的次序（比如 `priority` 把按量计费端点
   压在低优先级作兜底，这个语义必须原样保留）。
2. **Sticky 在 quota 之后**：会话黏性优先级最高。已建立的会话即使命中的账号额度已耗尽，只要端点健康
   就继续走它——打断 prefix cache 的重算成本，通常高于短暂溢出的计费成本。这是既定取舍，
   本设计不得覆盖它。
3. **quota 只重排、从不淘汰**：候选集大小不变，failover 语义完全不受影响。

### 4.2 重排算法

```
func reorderByQuota(candidates []*core.Endpoint, dims []strategy.Dimension,
                    reg *quota.Registry, now time.Time) (changed bool)

  // 1. 切分并列梯队：沿 candidates 走，相邻两个端点若对 dims 链中每个
  //    Dimension.Compare 都返回 0，则同属一个梯队。
  //    （Sort 是稳定排序，梯队内保持配置文件顺序。dims 为空时全体同属一个梯队。）
  //
  // 2. 对每个梯队：取出其中"配了额度"的成员及其下标位置，
  //    按 urgency 降序稳定排序后，写回原来那些下标。
  //    未配额度的成员位置纹丝不动。
```

第 2 步的"占位重排"是刻意的：如果把未配额度的端点也塞进排序（无论按 urgency=0 排到最后，还是排到
最前），只要用户只给三个账号里的一个配了额度，另外两个就会被意外降级或提升。占位重排让"给某个
Provider 配额度"这件事的影响**严格局限于那个 Provider 自己**，没有任何隔空作用。

复杂度：候选数 n 通常 2~6，`O(n²)` 的两两 Compare 完全可忽略。

### 4.3 触发时机：只对新会话

```
if sticky 命中并通过 TTL 校验:
    moveToFront(pinned)          // quota 重排的结果被覆盖，符合 4.1 不变量 2
else:
    reorderByQuota(...)          // 这就是"新会话"
```

`Serve` 里已经算出 sticky 是否命中，所以"新会话"这个判定**零额外成本**。三种情况会走到 `else`：

* 首轮对话（指纹未见过）；
* sticky TTL 过期（缓存本来就凉了，重新按额度挑一个，正确）；
* 该虚拟模型 `sticky: false` 或指纹提取失败 → 每请求都按额度重排，对无黏性模型正是想要的行为。

实现上不必写成显式 if/else 两分支：quota 重排无条件先跑，sticky 命中时的 `moveToFront` 天然覆盖它。
一条直线，无分支。

---

## 5. 计量：消耗了多少 token

### 5.1 数据来源与降级链

```
1) 上游 usage（权威）    ← respStream 嗅探，复用 chatmsg 的解析口径
2) 本地估算（降级）      ← core.EstimateTextTokens(请求体) + core.EstimateTextTokens(响应体)
```

为什么不干脆只用估算？因为估算误差在 ±30% 量级。对"三个账号里选哪个"这种粗决策，±30% 无所谓；
但计数器同时还要回答"这个月的套餐烧到 80% 了没有"，并驱动阶段二的预测看板——那里 ±30% 是不可接受的。
所以：**能拿到真值就用真值，拿不到才退回估算，并且在状态里标出本周期估算占比**，让使用者知道数字有多可信。

拿不到 usage 的情况：上游响应带 `Content-Encoding`（`respStream` 按设计对压缩响应完全不做解析）、
上游根本不返回 usage 字段、流被中途截断。

### 5.2 嗅探的实现约束

* 挂在 `respStream` **已有的事件切分**上，不新增一遍全文遍历。
* 每个事件先做一次 `bytes.Contains(ev, []byte("\"usage\""))` 廉价门禁，命中才 JSON 解析。
  绝大多数事件（token delta）直接跳过，开销约等于零。
* 解析与合并逻辑不在 router 里重写：在 `chatmsg` 暴露一个逐块入口
  （形如 `UsageFromChunk([]byte) (Usage, bool)`，内部就是现有的 `mergeUsage`），
  router 调用它。这条守住 CLAUDE.md 的"`chatmsg` 是消息解析的唯一真相源"不变量——
  否则 Anthropic 的 `message_start`/`message_delta` 两段式累计这类知识就会出现第二份实现。
* 合并语义沿用现有的**逐字段取 max**，天然适配"流式累计"与"单个终态对象"两种形态。
* 只在 `forwardSuccess` 成功路径计费。失败尝试不计费（429 基本没消耗；中途截断的输入消耗算作
  可接受的低估）。

### 5.3 计费口径：存分量、算时定策

计数器**分四项存**（`fresh / cache_read / cache_write / out`），而不是存一个总数。
`fresh = In - CacheRead - CacheWrite`（`chatmsg.Usage` 的文档已明确 CacheRead/CacheWrite 都包含在 In 里）。

* 阶段一的计费公式固定为 `fresh + cache_read + cache_write + out`，即**上游报什么就算什么**，
  零配置、零惊喜。
* 分项存储是为了以后能加权（各厂商对 cache write 溢价、cache read 折扣的口径不同）而
  **不必让历史数据作废**——政策放在读取侧，事实放在存储侧。多存三个 int64，成本为零。
* `/admin/status` 也因此能直接给出分项明细，而不是一个无法追问的总数。

### 5.4 为什么不做"预扣"（reservation）

请求发出前先按 `RequestFacts.EstimatedTokens` 预扣、完成后与真值对账——这样可以避免并发的多个
新会话在任何一个上报 usage 之前全部选中同一个账号。

不做。因为超冲量的上界是 `并发新会话数 × 单请求 token 数`，量级约 10 × 50K = 500K，
对一个 100M 的月度套餐是 0.5% 的噪音；而预扣要引入"预扣—对账—回滚（请求失败时）"三段状态机，
以及失败路径上的泄漏风险。**用 0.5% 的精度换掉一个有状态子系统，是划算的。**

---

## 6. 数据模型与配置

### 6.1 配置

```yaml
providers:
  - name: zhipu-coding-plan
    base_url: {anthropic: https://open.bigmodel.cn/api/anthropic}
    api_key: ${ZHIPU_KEY}
    quota:
      tokens: 100000000      # 本周期额度（必填，>0）
      period: monthly        # monthly | daily
      reset_day: 14          # period=monthly 时的重置日，1..31，默认 1
```

**命名**：字段用 `quota` 而不是战略文档里暂定的 `budget`。理由：`vmr report` 已经有一套以金额为
中心的成本估算（`pricing.yaml`），在配置里再出现一个 `budget` 却指 token 而非 $，是可预见的混淆源。
`quota.tokens` 无歧义。

**校验**（`config.validate`，遵循现有 fail-fast 风格）：
* `tokens > 0`；
* `period ∈ {monthly, daily}`；
* `reset_day` 仅在 `monthly` 下允许出现，取值 1..31；
* `reset_day` 大于当月天数时**截断到当月最后一天**（2 月的 `reset_day: 31` → 28/29 号），
  而不是报错——按 31 号买的套餐是真实存在的。

### 6.2 周期的表示：惰性重置，不用定时器

周期用一个**字符串 key** 表示（`monthly` → `"2026-08"`，跨 `reset_day` 后归属下一个 key；
`daily` → `"2026-08-06"`）。计数器行存 `{period_key, 四个分项}`。

**任何一次读或写，先比较 `stored.period_key` 与 `PeriodKey(spec, now)`，不等就地清零。**

于是：没有 ticker goroutine、没有定时任务、没有"重置时刻进程恰好没跑"的漏重置、
没有时钟回拨导致的重复重置。重置退化成一次字符串比较。进程重启后从文件加载，同一条比较自动完成
补偿。这是本设计里最省事的一处。

**时区**：周期边界按 `fmtutil.DisplayZone`（= `time.Local`）计算。厂商真实账单周期的时区不公开，
本地时区最贴近运维者的心智模型，且符合 CLAUDE.md "人可见的一切都走 DisplayZone" 的既定权威。
这是**近似而非精确复刻**，需在用户文档里写明。

### 6.3 运行态

```go
package quota   // internal/quota，仅依赖 core（周期数学是纯函数，无 I/O）

type Counters struct{ Fresh, CacheRead, CacheWrite, Out int64 }

type Registry struct {           // 形状对齐 health.Registry：挂在 Router 上，不在 Snapshot 里
    mu       sync.Mutex
    accounts map[string]*account // key: provider name
    path     string
    dirty    bool
}

type account struct {
    period    string
    c         Counters
    estimated int64   // 本周期中由降级估算贡献的 token 数，用于标注可信度
}

func (r *Registry) Charge(provider, period string, c Counters, estimated bool)
func (r *Registry) Consumed(provider, period string) (Counters, int64 /*estimated*/)
func PeriodKey(spec *core.QuotaSpec, now time.Time) string
func PeriodEnd(spec *core.QuotaSpec, now time.Time) time.Time
```

**职责切分**：Registry 只存"消耗了多少"这个**事实**；"额度是多少"这个**政策**始终从 Snapshot 现读。
于是热重载改 `quota.tokens` 立刻生效且不重置计数，无需任何迁移逻辑。

**Key 用 provider name，刻意不含 API Key 哈希**——这与 `Endpoint.HealthKey()` 的做法相反，是有意为之：
HealthKey 含密钥哈希是为了"换了 key 就重新试探健康"，方向安全；但对额度而言，轮换密钥（同一账号）
会把当月计数清零，进而导致超支。两个方向的风险不对称，所以两处的 key 策略不同，且必须在代码注释里
写明原因，否则后人一定会"顺手统一"。

**Spec 的落点**：`core.QuotaSpec` 定义在 `core`（`config` 校验它、`quota` 计算它、`router` 读它，
放 `core` 是唯一不产生环的位置，与 `core.StickyBackstopTTL` 同一个先例）。
`BuildSnapshot` 时把同一个 `*core.QuotaSpec` 指针挂到该 Provider 展开出的所有 `core.Endpoint` 上
（`nil` = 该端点无套餐），于是排序时取额度是一次字段读，而不是对 `Cfg.Providers` 做线性查找。

### 6.4 持久化

* 文件：`<log_dir>/vmr-quota.json`，权限 0600（与审计文件同级同权限；不匹配 `vmr-audit-*` glob，
  不会污染 `vmr report` 的输入）。不新增配置项。
* 写：`Charge` 只置 `dirty`；由 `vmr start` 启动的单个 flusher goroutine 按固定间隔（5s）落盘，
  进程退出时（已有的 SIGTERM/SIGINT + `srv.Shutdown` 路径）强制 flush 一次。
  临时文件 + `rename` 原子替换。
* 硬 kill 的最坏损失是 5 秒的计量，对月度额度是可忽略的量级。
* 文件缺失/损坏 = 从零开始，仅打一条日志，**绝不阻止启动**：一个统计辅助设施不该有能力让路由停摆。

---

## 7. 与既有机制的交互

| 机制 | 交互 | 结论 |
|---|---|---|
| Sticky | quota 重排先跑，sticky `moveToFront` 后跑并覆盖 | 会话黏性优先，明确不为省钱打断 cache |
| Health | 额度耗尽**不**触发冷却；真正的耗尽信号是上游的 402/429，由既有状态机处理 | 两套机制各管各的，不交叉 |
| Failover | quota 只重排不淘汰，候选集大小不变 | failover 语义零改动 |
| 热重载 | Registry 挂 Router、不在 Snapshot 里 | 计数跨重载存活；额度值现读现用，改配置立刻生效 |
| 并发 | `Charge` 每次成功响应一次，`Consumed` 每个新会话一次 | 普通 `sync.Mutex` 足够（对比一次 HTTP 往返，锁竞争不值一提），沿用 `health.Registry` 的形状 |
| `max_concurrency` 限流 | 无关 | — |
| `vmr replay` | 重放会真实调用上游，因此**会计费** | 与真实流量一致，符合直觉，无需特殊处理 |
| 后台探针 `probe` | 会消耗少量 token，但不走 `forwardSuccess` | 不计费。与审计不记探针是同一个已知口径，`docs/OUTSTANDING_ISSUES_opus-5.md` 已有记录 |

---

## 8. 可观测性

沿用"能复用就不新增"的原则：

* **`X-VMR-Route-Reason`**：`routeReason` 增加一个 `quota bool`，重排真正改变了队首时渲染成 `pick=quota`
  （sticky 命中时仍渲染 `pick=sticky`）。**因为 recorder 已经把响应头写进审计记录，这条路由理由
  自动进入飞行记录仪，无需新增任何审计字段。**
* **`/admin/status`**：新增 `quota` 段，每个配了额度的 Provider 一行：
  `period` / `used`（含四分项）/ `total` / `remaining` / `pct` / `urgency` / `period_ends_at` / `estimated_pct`。
  这是运维者回答"为什么流量都压在这个账号上"的第一现场。
* **`vmr check`**：在既有的路由表预览里打印各 Provider 的额度配置（纯静态，不读运行态）。
* **live log**：**不加**。日志行已经很密，额度是"当前状态"而不是"本次请求发生了什么"，
  它属于 `/admin/status`，不属于每行日志。
* **`vmr report`**：属于阶段二，本设计不涉及。

---

## 9. 决策与取舍

| 决策 | 选择 | 理由 | 放弃的备选 |
|---|---|---|---|
| 权重信号 | `urgency = remaining / time_left` | 目标是"过期前烧完"，配速问题不是存量问题；重置日不对齐时存量信号会给出反向结论 | `remaining/total`（战略文档初稿）——在重置日不对齐时系统性错误 |
| 分配策略 | 贪心取最高 urgency（稳定排序） | 无状态、确定性、failover 顺序自动正确、对 prefix cache 友好 | SWRR——需要持久化累加器、只解决"选谁"不解决"顺序"、撒开流量反而伤 cache |
| 接入点 | `Sort` 之后、`sticky` 之前的独立一步 | Sticky 已是同形状先例；`Dimension.Compare` 结构上看不到请求，且是 CLAUDE.md 明令不得扩展的接口 | 新增一个 `Dimension`——需要给接口塞请求参数，破坏既定边界 |
| 重排粒度 | 并列梯队内，且只在"配了额度"的成员之间做占位重排 | 保住 `priority` 语义；让配置一个 Provider 的额度不产生隔空影响 | 全局重排（破坏优先级兜底）；把未配额端点当 urgency=0（会意外降级） |
| 耗尽处理 | 只降到梯队末位，不熔断 | 计数器是估算值，按估算值执行破坏性动作 = 自制故障；真正的硬信号是上游 402/429，既有 health 已覆盖 | `on_exhausted: block` 配置项——用估算值制造停机 |
| 周期重置 | 惰性比较 period key | 无 goroutine、无漏重置、无时钟回拨问题、重启自动补偿 | 定时器/cron 式重置 |
| 计量存储 | 分四项存，读取时套公式 | 将来加权时历史数据不作废；`/admin/status` 可给明细 | 只存总数 |
| 计量精度 | usage 为准，估算降级，并标注估算占比 | 粗决策容忍 ±30%，但阶段二的额度预测看板不容忍 | 全程估算（预测失真）；无 usage 时放弃计费（静默漏计更糟） |
| 并发超冲 | 不做预扣 | 超冲上界约 0.5%，换掉一个三态子系统与其泄漏风险 | 预扣 + 对账 + 回滚 |
| Registry key | Provider name，不含密钥哈希 | 轮换密钥不应清零当月计数；与 HealthKey 的风险方向相反 | 照抄 `HealthKey()` |
| 配置命名 | `quota:` | `vmr report` 的 `budget` 语境是金额；同名不同义是可预见的混淆 | `budget:`（战略文档暂定名） |
| 落盘位置 | `<log_dir>/vmr-quota.json` | 复用既有目录与 0600 权限，不新增配置项 | 新增 `state_dir` 配置 |

---

## 10. 明确不做的事

1. **滚动窗口限流型套餐**（Claude Code Max/Pro 等按"每 5 小时窗口"限流）。它不是 token 总量桶，
   `tokens/period` 模型套不上去，本质是"限流感知的退避与切流"问题。
   **但设计上留了口子**：上游普遍在响应头里返回 `x-ratelimit-remaining-*` / `anthropic-ratelimit-*`，
   而 router 本来就在转发响应头。未来把这类头嗅探成一个**上游权威的 urgency 直接来源**，
   可以复用本设计除计量以外的全部结构（重排点、梯队规则、Sticky 优先级）。这是阶段 1.5 的自然延伸。
2. **按下游用户/业务线的实时配额**。需要在请求路径上引入下游身份维度的配额状态，比 Provider 级
   重得多。事后可见性另有低成本路径：`ClientKeyTag` 已逐请求进审计，`report`/`story` 已能按它分组
   （详见战略文档）。
3. **多实例共享计数**。单二进制、零 DB 是产品前提。多实例各算各的，会各自低估。写进文档，不做。
4. **金额（$）预算**。`pricing.yaml` 已在分析侧做成本估算，路由侧再引入一套价格解析是重复建设。
5. **额度耗尽硬熔断**。见决策表。
6. **把绕过 vmr 的直连流量计入**。结构上不可能，是"本地统计"这条路线的固有代价，不是缺陷。

---

## 11. 实施与验证

### 11.1 落地顺序

每一步都可独立编译、独立验证、独立回滚：

1. `internal/quota`：`Counters` / `Registry` / `PeriodKey` / `PeriodEnd` / 持久化。纯逻辑 + 纯函数，
   全部可单测，不碰请求路径。
2. `core.QuotaSpec` + `config` 解析与校验 + `vmr check` 打印。此时行为零变化。
3. `BuildSnapshot` 把 `*QuotaSpec` 挂到 `Endpoint`；`Router` 持有 `*quota.Registry`；启动时加载、
   退出时落盘、flusher goroutine。此时仍无路由行为变化。
4. 计量：`chatmsg` 增加逐块入口 → `respStream` 嗅探 → `forwardSuccess` 计费。
   **到这一步为止，路由决策完全没变**，可以先在生产里只跑计量、用 `/admin/status` 对着厂商控制台
   校准几天，确认数字可信之后再开第 5 步。这个"先只观测、后再决策"的切分是本方案降低风险的关键。
5. `internal/router/quota.go`（新文件，不进 `router.go`）：梯队切分 + urgency 排序；
   `Serve` 里加一行调用；`routeReason` 加 `quota` 字段。

### 11.2 测试

* **周期数学**（纯函数，最容易出错也最容易测）：`reset_day` 落在月末的截断（1/31 → 2/28、2/29）、
  跨年、`reset_day` 之前/之后的归属、DST 切换日的 `daily` 周期、`PeriodEnd` 与 `PeriodKey` 的一致性。
* **urgency 排序**：1.1 那个"三套餐重置日错开"的场景直接做成断言——它是整个设计的立论依据，
  必须有一个测试钉住它。
* **梯队切分**：`priority` 分层时不跨层重排；`dims` 为空时全体同层；未配额度成员位置不变。
* **惰性重置**：跨周期的 `Charge`/`Consumed` 均触发清零；重启后从文件加载并补偿重置。
* **不变量回归**：sticky 命中时 quota 重排结果被覆盖；耗尽端点仍在候选集里（不被淘汰）；
  额度耗尽不产生 health 冷却。
* **`-race`**：`Registry` 的并发 `Charge`/`Consumed`/flush（health/audit/router 并发相关改动的既有惯例）。
* **archtest**：确认 `router` → `chatmsg`/`quota` 不触发边界规则（已核对 `chatmsg` 仅依赖 `fmtutil`，
  无环）；确认 `router.go` 行数仍在预算内（新逻辑在独立文件）。
* **loadtest**：现有场景矩阵跑一遍确认无回归。计量嗅探的额外开销预期落在噪音里
  （每事件一次 `bytes.Contains`），若 `stream_normal` 场景的 p95 出现可测量的变化，说明门禁写错了。

### 11.3 验收标准

1. 未配置 `quota:` 的现有配置，行为与改动前逐字节一致（唯一可接受的差异是 `/admin/status` 多一个空段）。
2. 配了额度但只开到第 4 步（只计量不决策）时，路由行为仍与改动前一致。
3. 三套餐错开重置日的场景下，快到期且有余量的套餐拿到新会话——即 1.1 的反例被修复。
4. `/admin/status` 报的 `used` 与厂商控制台的偏差在可解释范围内（差异来源只应是：绕过 vmr 的流量、
   探针、厂商自身的 cache 计费口径）。

---

## 12. 已知限制

* **单实例假设**：账号流量必须全部经由这一个 vmr 实例，否则本地计数低估。
* **周期边界是近似**：本地时区、`reset_day` 粒度到日，不复刻厂商账单的分秒。
* **cache 计费口径未知**：阶段一按上游报数等权求和，与厂商真实扣减可能有系统性偏差；
  分项存储是为将来修正留的口子。
* **单个长会话可以独吞一个套餐**：sticky 优先级高于额度，一个长上下文 Agent 会话可以把一个套餐烧穿。
  这是 4.1 不变量 2 的直接后果，属于已知取舍；阶段二的预测看板负责让它可见。
* **并发/RPM 上限不可见**：`urgency` 只看总量桶。账号显示"额度充足"仍可能撞并发上限，
  由 health 状态机兜底——这是另一个问题，不是本设计的缺陷。
