<!-- Ver 2026-08-22 11:42, by gemini-3.7-flash -->

# VMR Quota 配额体系重构与多 Limit 门控设计方案

## 1. 方案背景与目标

### 1.1 现状与痛点
1. **单一 Limit 限制**：现有系统仅允许每个 Provider 配置一条 Limit，无法满足现实中厂商复合配额管理的需求（例如：同时具备 60 RPM 短窗限流与月度 1 亿 Token 账单限额）。
2. **系数与权重未解耦**：`token_weights` 和 `model_multipliers` 配置在 Provider 根级别。当 Provider 混合使用请求数和 Token 计量时，无法为不同 Limit 独立定义倍率。
3. **缺少模型级独占模式**：部分上游厂商对不同模型分配独立并发/速率额度（Per-Model），现有系统仅支持全部模型共享（Shared）。
4. **时间窗口粒度受限**：不支持分钟级（`min`）限流，且时间起点缺乏清晰的“自然时间对齐”与“显式锚点格式校验”规则。
5. **缺乏硬限额防护**：当前超额仅作为软调度降级（Score = 0），在单 Provider 或 Failover 耗尽场景下仍会向上游发送必定失败的请求，缺少确定性的硬门控拦截与标准 429 响应。

### 1.2 核心设计目标
* **高内聚 Limit 实体**：将 `mode`、`model_multipliers`、`token_weights` 完全下沉至具体 Limit 规则，消除顶层冗余配置。
* **双模式支持**：支持 `mode: shared`（默认，共享总额度）与 `mode: per_model`（按模型独立独占额度）。
* **全粒度时间窗口**：支持 `min`, `h`, `d`, `w`, `mo`；缺省按自然日历起点对齐，显式指定时支持 5 种标准时间格式并严格校验。
* **双层门控与调度**：
  * **硬门控（Hard Gating）**：任意 Limit 超额（$Used \ge Amount$）即判定不可用，从候选集剔除；全部超额时快速熔断并返回 `429 Too Many Requests` 与 `Retry-After`。
  * **软调度（Soft Reordering）**：未超额端点按多 Limit 的木桶最小分数 $Score = \min(S_1, S_2, \dots)$ 排序。
  * **零状态惰性恢复（Lazy Reset）**：跨周期边界瞬间自动归零恢复，无定时器开销，故障自愈。

---

## 2. YAML 配置规范 (Configuration Specification)

### 2.1 结构定义
Provider 下的 `quota` 块直接由 `limits` 列表构成，所有计量规则与修饰系数均封装在单条 Limit 内部：

```yaml
providers:
  - name: <provider_name>
    base_url: { ... }
    api_key: <key>
    quota:
      limits:
        - metric: requests | tokens | cost      # 必填：计量单位
          every: <N>(min|h|d|w|mo)             # 必填：周期长度（支持 min/h/d/w/mo）
          amount: <float>                       # 必填：额度数值（必须 > 0）
          mode: shared | per_model              # 可选：额度模式，默认 shared
          since: <time_string>                  # 可选：起始时间锚点，缺省为自然周期起点
          token_weights:                        # 可选：仅 metric: tokens 时允许配置
            in_fresh: <float>                   # 缺省 1.0
            cache_read: <float>                 # 缺省 1.0
            cache_write: <float>                # 缺省 1.0
            out: <float>                        # 缺省 1.0
          model_multipliers:                    # 可选：仅 metric 为 requests/tokens 时允许配置
            <model_name>: <float>               # 对应模型在当前 Limit 下的消耗折算系数
            "*": <float>                        # 通配兜底系数
```

### 2.2 完整配置示例

```yaml
providers:
  - name: siliconflow-prod
    base_url: {openai: https://api.siliconflow.cn/v1}
    api_key: sk-prod-xxx
    quota:
      limits:
        # 规则 1：RPM 短窗速率限制，每个模型独占 60 次/分
        - metric: requests
          every: 1min
          amount: 60
          mode: per_model                   # 独立模式：每个模型各算各的 60 次/分
          # since 省略 -> 自动按当前分钟 0 秒对齐
          model_multipliers:
            deepseek-ai/DeepSeek-R1: 2.0     # R1 模型每次请求扣减 2 次额度

        # 规则 2：账号天级请求安全水线，所有模型共享 10000 次/天
        - metric: requests
          every: 1d
          amount: 10000
          mode: shared                      # 共享模式（可显式声明或省略）
          since: "2026-08-01 00:00:00"

        # 规则 3：月度 Token 预算，所有模型共享 1 亿 Token，带 Prompt Cache 折算
        - metric: tokens
          every: 1mo
          amount: 100000000
          mode: shared
          token_weights:                    # 仅在 tokens 规则下合法
            in_fresh: 1.0
            cache_read: 0.1                 # 缓存命中仅计 0.1 倍
            cache_write: 1.0
            out: 2.0                        # 输出 Token 计 2 倍
          model_multipliers:
            deepseek-ai/DeepSeek-R1: 1.5     # R1 的 Token 消耗整体乘 1.5 倍
```

### 2.3 严格校验规则 (Fail-Fast Validation)

1. **数值有效性**：`amount`、`token_weights.*`、`model_multipliers.*` 必须为有限正数（`> 0` 且非 `NaN`/`Inf`）。
2. **字段互斥与约束**：
   * 若 `metric != tokens`，配置 `token_weights` 必须报错。
   * 若 `metric == cost`，配置 `model_multipliers` 必须报错（价格差异必须通过 `pricing:` 费率表定义）。
3. **时间格式与周期匹配约束**：
   * `since` 为纯时间格式（`hh:mm` / `hh:mm:ss`）时，**仅允许**周期单位为 `min` 或 `h`；若用于 `d`、`w`、`mo` 则必须在配置加载期报错。
   * 时区解析一律采用本地系统时区（`fmtutil.DisplayZone`）。

---

## 3. 时间窗口与对齐算法 (Time Window & Alignment)

### 3.1 周期单位与步进
支持的单位及其时间步进实现：
* `min` (分钟)：$\Delta t = \text{everyN} \times 1\text{ Minute}$
* `h` (小时)：$\Delta t = \text{everyN} \times 1\text{ Hour}$
* `d` (天)：$\Delta t = \text{everyN} \times 24\text{ Hours}$（日历日步进）
* `w` (周)：$\Delta t = \text{everyN} \times 7\text{ Days}$（ISO 周步进）
* `mo` (月)：按日历月步进，处理月末天数截断（`addMonthsClamped`，如 1 月 31 日加 1 个月对齐至 2 月 28/29 日）。

### 3.2 缺省自然周期对齐 (`DefaultSince`)
当 `since` 为空时，基准时间按当前时间 $now$ 的自然起点对齐：
* `min` $\to$ 当前分钟 `:00` 秒（微秒归零）。
* `h` $\to$ 当前小时 `:00:00`。
* `d` $\to$ 当天 `00:00:00`。
* `w` $\to$ 当前 ISO 周的周一 `00:00:00`。
* `mo` $\to$ 当月 1 日 `00:00:00`。

### 3.3 显式时间格式解析表

| 格式名称 | 语法模板 | 适用周期 | 解析行为 |
| :--- | :--- | :--- | :--- |
| **标准完整日期时间** | `2006-01-02 15:04:05` / `T` 分隔 | 全部 | 精确对齐到指定秒 |
| **分钟级日期时间** | `2006-01-02 15:04` / `T` 分隔 | 全部 | 秒归零 |
| **纯日期** | `2006-01-02` | 全部 | 当日 `00:00:00` |
| **纯时间（含秒）** | `15:04:05` | 仅 `min`, `h` | 当日 `15:04:05`，若用于 `d/w/mo` 则报错 |
| **纯时间（分）** | `15:04` | 仅 `min`, `h` | 当日 `15:04:00`，若用于 `d/w/mo` 则报错 |

---

## 4. 计数模型与存储隔离 (Metering & Storage Isolation)

### 4.1 统一 Bucket Key 命名空间
[`quota.Registry`](file:///Users/stanford/code/vmr/internal/quota/quota.go) 维持扁平 Key 寻址结构，避免多层嵌套：
* **`shared` 模式**：
  $$\text{bucketKey} = \text{limitKey} \quad (\text{例: } \texttt{"requests/1d"})$$
* **`per_model` 模式**：
  $$\text{bucketKey} = \text{limitKey} + \texttt{"#"} + \text{model} \quad (\text{例: } \texttt{"requests/1min#deepseek-ai/DeepSeek-R1"})$$

### 4.2 多 Limit 计费流程 (`ChargeResponse`)
当一次调用成功完成后，路由器遍历该端点所属 Provider 的所有 Limit：
1. **获取 Bucket Key**：根据 `Limit.Mode` 生成 `bucketKey`。
2. **计算周期起点**：$periodStart = \text{PeriodStart}(Limit, now)$。
3. **计算单项增量**：
   * `metric: requests`：基础消耗为 1；应用该 Limit 的 `model_multipliers[model]` 缩放。
   * `metric: tokens`：读取该次请求的原始四分量，应用该 Limit 的 `model_multipliers[model]` 缩放后存入。
   * `metric: cost`：读取结算金额存入。
4. **累加写入**：调用 `Registry.Charge(provider, bucketKey, periodStart, delta, estimated)`。

---

## 5. 双层门控与调度架构 (Two-Tier Gating & Routing)

```mermaid
flowchart TD
    Req([客户端请求到达]) --> PreFilter[健康检查 & 基础条件过滤]
    
    subgraph Layer1_HardGating [第一层：硬门控可用性检查 (Hard Gating)]
        PreFilter --> CheckEndpoint{遍历候选端点}
        CheckEndpoint --> EvalLimits{遍历该端点关联的所有 Limits}
        EvalLimits -- 存在任意 Limit 消耗 Used >= Amount --> MarkExhausted[标记端点 QuotaExhausted<br/>从可用候选集剔除]
        EvalLimits -- 所有 Limits 均满足 Used < Amount --> MarkEligible[保留在可用候选集]
    end

    MarkExhausted --> CheckEmpty{可用候选集是否为空?}
    CheckEmpty -- 全部超额 (空) --> FastFail[快速熔断: 返回 429 Too Many Requests<br/>Header 带 Retry-After: 最短等待秒数]
    CheckEmpty -- 仍有可用端点 --> Layer2_SoftScheduling

    subgraph Layer2_SoftScheduling [第二层：软调度评分重排 (Soft Headroom Reordering)]
        MarkEligible --> Layer2_SoftScheduling
        Layer2_SoftScheduling --> CalcMinScore["计算综合 Headroom 分数:<br/>Score = min(Headroom(L1), Headroom(L2), ...)"]
        CalcMinScore --> TierSort[同优先级梯队内按 Score 降序重排]
    end

    TierSort --> StickyCheck{Sticky 检查}
    StickyCheck --> UpstreamCall[发起上游调用]
    UpstreamCall -- 调用成功 --> ChargeAllLimits[遍历所有 Limits 独立记录消耗]
    UpstreamCall -- 调用失败 --> Failover[Failover 尝试下一候选端点]
```

### 5.1 第一层：硬门控（Hard Availability Gating）
* **判定准则**：在路由筛选阶段，对每个候选端点遍历其关联的所有 Limit：
  $$\text{IsAvailable}(ep, now) = \bigwedge_{L \in ep.\text{Quota}.\text{Limits}} \Big(\text{Used}(ep, L, now) < L.\text{Amount}\Big)$$
* **熔断机制（All-Exhausted Fallback）**：
  * 若模型下的所有候选端点均已超额，路由器立即终止请求，返回标准 HTTP `429 Too Many Requests`。
  * **`Retry-After` 计算**：取所有超额 Limit 中距离周期结束最近的时间：
    $$\text{RetryAfterSeconds} = \max\left(1, \left\lceil \min_{L} (\text{PeriodEnd}(L, now) - now).\text{Seconds}() \right\rceil\right)$$
  * 响应示例：
    ```http
    HTTP/1.1 429 Too Many Requests
    Content-Type: application/json
    Retry-After: 14

    {
      "error": {
        "message": "All upstream providers have exhausted their configured quota limits. Earliest reset in 14s.",
        "type": "quota_exhausted",
        "code": 429
      }
    }
    ```

### 5.2 零状态惰性恢复机制（Lazy Reset）
* **无需定时器**：系统无需运行后台轮询或 Cron 任务。
* **原理**：`Registry.Used` 和 `Registry.Charge` 在访问 Bucket 时，传入通过纯函数计算出的最新 $PeriodStart$。若 Bucket 内记录的周期起点已落后，则在互斥锁内**原地瞬间清零**（`*b = bucket{PeriodStart: ps}`）。
* **效果**：一旦系统时钟跨过周期边界（哪怕仅超前 1ms），下一个请求到达时立即读取到 $Used = 0$，端点瞬间满血恢复。

### 5.3 第二层：软调度评分（Soft Headroom Reordering）
* 对所有通过硬门控的端点，计算其综合健康余量分数：
  $$\text{Score}(ep, now) = \min_{L \in ep.\text{Quota}.\text{Limits}} \text{ScoreForLimit}(L, \text{Used}(ep, L, now), now)$$
* **排序规则**：同优先级（Priority Tier）内按 $\text{Score}$ 降序稳定排序（余量更充裕的优先接流），不同优先级梯队之间严禁跨梯队越权。

---

## 6. 核心数据结构重构定义

### 6.1 `internal/core/core.go`
```go
type QuotaMode string

const (
	QuotaModeShared   QuotaMode = "shared"
	QuotaModePerModel QuotaMode = "per_model"
)

type Limit struct {
	Metric           QuotaMetric
	EveryN           int
	EveryUnit        string               // "min", "h", "d", "w", "mo"
	EveryText        string               // e.g. "1min", "1d", "1mo"
	Since            time.Time            // 已解析的基准时间锚点 (DisplayZone)
	Amount           float64
	Mode             QuotaMode            // QuotaModeShared 或 QuotaModePerModel
	TokenWeights     TokenWeights         // 仅 Metric == MetricTokens 时有效，缺省全 1.0
	ModelMultipliers map[string]float64   // 仅 Metric == requests/tokens 时有效
}

type QuotaSpec struct {
	Limits []Limit                        // Provider 下的所有 Limit 规则列表
}
```

### 6.2 `internal/config/quota.go`
```go
type LimitConfig struct {
	Metric           string               `yaml:"metric"`
	Every            string               `yaml:"every"`
	Since            string               `yaml:"since"`
	Amount           float64              `yaml:"amount"`
	Mode             string               `yaml:"mode"`
	TokenWeights     *TokenWeightsConfig  `yaml:"token_weights"`
	ModelMultipliers map[string]float64   `yaml:"model_multipliers"`

	Resolved         core.Limit           `yaml:"-"`
}

type QuotaConfig struct {
	Limits []LimitConfig `yaml:"limits"`
}
```

---

## 7. 实施计划与影响评估

### 7.1 代码重构步骤
1. **配置层**（[`internal/config`](file:///Users/stanford/code/vmr/internal/config)）：重构 YAML 结构体，添加 `min` 单位与多时间格式解析器，编写全量校验逻辑与单元测试。
2. **核心层**（[`internal/core`](file:///Users/stanford/code/vmr/internal/core)）：重构 `core.Limit` 与 `core.QuotaSpec` 数据结构。
3. **配额层**（[`internal/quota`](file:///Users/stanford/code/vmr/internal/quota)）：
   * 在 `period.go` 中增加 `min` 步进支持与自然分/时起点对齐；
   * 在 `quota.go` 中实现基于 `(provider, bucketKey)` 的存储隔离与惰性归零；
   * 在 `weight.go` 中适配单 Limit 作用域下的 `BaseAmount` 与倍率缩放。
4. **路由层**（[`internal/router`](file:///Users/stanford/code/vmr/internal/router)）：
   * 在 `router.go` 的 `Serve` 流程中注入硬门控过滤与全耗尽 429 熔断；
   * 在 `quota.go` 中适配多 Limit 遍历计费与 $\min(Score)$ 评分聚合。
5. **观测与工具链**（[`cmd/vmr`](file:///Users/stanford/code/vmr/cmd/vmr)）：适配 `vmr check`、`vmr status` 及 `/admin/status` 的多 Limit 结构展示。

### 7.2 兼容性说明
* **配置迁移**：旧版写在 Provider 根部的 `token_weights` / `model_multipliers` 将在启动时被明确提示并报错（Fail-Fast），指导用户迁移至具体 `limits[]` 项内，杜绝歧义。
* **磁盘持久化**：`vmr-quota.json` 中的 Bucket Key 自动采用新格式（`key` 或 `key#model`），不同周期间平滑过渡。

---

## 附录 A：与实际落地方案的差异对照

<!-- Ver 2026-08-22, by Sonnet 5 -->

本节记录这份提案与最终实际落地方案（`docs/VirtualModelRouter_Design_v4_Quota.md` 已同步更新）
之间的差异，以及每一处差异的理由。落地前已就每一条与用户逐项确认过，这里只做书面归档，不重复
决策过程本身。

| 提案条目 | 是否采纳 | 实际落地 | 理由 |
|---|---|---|---|
| **Hard Gating + 429 快速失败**（§5.1、§5.2） | ❌ 不采纳 | 维持既有设计的"只重排、从不淘汰"——耗尽的 Limit 只让端点排到梯队末位，候选集大小不变，从不返回 429，也不淘汰候选 | 现有设计文档 §12.1/§13 已经明确论证过"额度耗尽硬熔断"这条并给出结论"不做"：本地额度计数本质是估算值（token 计量靠 usage 嗅探 + 字节降级估算、多实例部署互相看不见彼此的消耗、厂商计数单位与 vmr 可观测单位经常对不上），拿一个估算值去做"直接拒绝一个本来能成功的请求"这种不可逆动作，一旦估算偏差方向不对就是自制故障。真正的硬信号是上游自己的 402/429，已经由 `internal/health` 状态机（冷却 + 半开单飞）兜底。用户确认后维持这条既有决策，未推翻 |
| **`mode: shared \| per_model`**（按实际遇到的模型名自动裂变 bucket，§2.1） | ❌ 不采纳 | 改用既有的 Scope（`models:` 显式列表）机制——只对显式列出的模型开子额度，其余模型仍走共享池 | `mode: per_model` 会让一个配了 N 个模型的 Provider 自动裂变出 N 个独立计数器，证据基础比既有 Scope 设计（`models:` 显式声明，且此前因证据不足被"降级为有真实案例才做"）更弱：厂商用量接口返回的"模型级明细"很可能只是展示明细，不是独立额度池。用户确认走既有的、更保守的 Scope 路线 |
| **多 Limit 的启动触发条件** | 部分采纳 | 现在启动了多 Limit（P3），但触发方式与两份文档原先约定的都不同 | 设计文档把 P3 的触发条件定义为"P1/P2 上线后的运行数据显示 429 频率/尾延迟代价确实值得"，本提案则隐含"排期到了就做"。两者都没有成立——是用户有一个具体 Provider 需要复合窗口配额（短窗 RPM 限速 + 长周期账单），直接以这个真实需求触发的。已在 `docs/VirtualModelRouter_Design_v4_Quota.md` §14.3 如实记录这次偏离，不让"P3 已交付"被误读成"429 数据已经证明它值得" |
| **`token_weights`/`model_multipliers` 下沉到每条 Limit**（§2.1） | ✅ 采纳 | 与提案一致：两个字段从账号级（`providers[].quota`）移到每条 `limits[]` 内部 | 这一点提案是对的，且与既有设计文档原决策相反——原决策（P1/P2）认为"一次调用扣多少"是账号级记账方式，应该账号内所有窗口共享一套系数；但 P3 引入多窗口后的真实场景证明了这个假设不成立：同一账号的短窗速率闸与长周期账单桶经常需要不同的折算方式（例：闸按次数不分缓存命中、桶按 Credits 精确折算），账号级字段装不下这种差异。已在设计文档 §12.1「折算规则的层级」一行订正并写明白推翻的理由，不是简单地照抄提案，是独立复核后认为提案这一点判断是对的 |
| **`every` 增加 `min` 粒度**（§3.1） | ✅ 采纳 | `every`/`since`/周期数学全线支持 `min` 单位，`DefaultSince` 对齐到当前分钟 `:00` | 短窗 RPM 限速是这次多 Limit 需求的直接动机，没有分钟粒度就表达不了 |
| **`since` 的 5 种显式时间格式校验表**（§3.3） | ❌ 不采纳 | 沿用既有设计的两种格式（纯日期 `YYYY-MM-DD`、`RFC3339`），只是把 `DefaultSince` 的隐式对齐规则扩展了一个 `min` 分支 | 5 种格式（含纯时间 `hh:mm[:ss]`、按周期单位限制哪些格式合法）是这次真实需求用不上的额外校验面——现有两种格式已经能表达"从哪一刻开始算周期"，多出来的格式只是为了"看起来更完备"，不是为了解决一个具体问题，不符合"简单有效"的取舍 |
| **多 Limit 打分归并：`Score = min(Headroom(L1), Headroom(L2), ...)`**（§5.3） | ❌ 不采纳（未采用提案的扁平化写法） | 采用现有设计文档 §5.2 已经设计好的桶/闸模型：账号内周期最长的 Limit 当"桶"（余量真的没花等于浪费，欠用应主动加分），其余更短的 Limit 当"闸"（只应压制，绝不提升），`GateReserve=0.5` | 提案的扁平 `min(Score)` 在"桶宽裕、闸也宽裕"的角落场景下会让一个短窗闸自己的 `raw>1` 直接决定整个账号的分数，等于让闸获得了"提升流量"的资格——这正是既有设计文档一直强调"闸只能压制，不能提升"这条边界要防的事。桶/闸方案是已经完整论证过的公式，照抄不是重新发明；单 Limit 时严格退化成 P1/P2 的既有行为（零回归）；代码量比扁平 `min()` 只多几行（找最长周期的下标 + 对非桶下标套一次 `min(1, raw/GateReserve)`）。这条不变量已经用测试钉住（`internal/quota/score_test.go` 的 `TestScoreForLimits_GateNeverBoosts`） |
| **Bucket Key 加 `#model` 后缀**（§4.1，为 `mode: per_model` 服务） | 部分采纳，落点不同 | `quota.LimitKey` 确实会在 `models:` 非空时追加 `#` + 排序后的模型列表作后缀，但这是为 Scope（既有机制）服务的，不是为提案的自动裂变 `mode: per_model` 服务——两条不同 Scope 的 Limit 才会撞 key，不是"每个模型自动裂变" | 提案里"用模型名给 bucket key 消歧"这个具体机制是对的、也确实被采纳了，只是服务的对象换成了范围更保守的 Scope，理由同上一条 |

> **勘误（2026-08-22 第二轮）**：这张表原本还有一行"Rolling 窗口 + 环形分桶"，标注"提案未提及，仅供对照"——这是我的错误，放错了地方。这张表是"提案 vs 实际落地的**差异**对照"，双方都没提过的东西不构成差异，不该出现在这里，混进来只会让人误以为提案漏提了什么。Rolling 现状记录见附录 B「没有发现的」一段，这里不再重复。

---

## 附录 B：方案复盘——剩余的优化/简化空间

<!-- Ver 2026-08-22, by Sonnet 5 -->

全部实现、文档、测试完成并跑绿之后，对这一版最终方案做的一次独立复盘。以下几条不是本次交付的
阻塞项——都已在生产可用的状态下记录为"值得关注"或"值得做但不紧急"，不建议为它们推迟已经完成
的交付。

1. **【❌ 已撤回，判断错误】~~Scope（`models:`）里的模型名没有校验"是否真的出现在这个 Provider 的路由表里"~~。**
   撤回理由见附录 C.5——我提议校验的对象（`providerModels`，路由表里实际用到的模型）和"这个 Provider
   真实提供哪些模型"根本是两件事，前者只是"当前恰好路由了哪些"，会把"预先配置、暂未路由"的正常用法
   错判成 typo。项目里也没有任何地方维护"某个 Provider 真实支持哪些模型"这份权威数据（标准定价表本身
   覆盖率都不完整），没有校验基准，这条检查做不出来，不是"值不值得做"的问题，是做不了、也不该做。

2. **【已重新判断：不做】`router/quota.go` 的 `applicableLimits` 在没有任何 Limit 配置 Scope 时仍然
   分配一个新切片。** 重新按"是否真的简化逻辑"这条标准过一遍：这个改动是加一个前置分支去省掉一次
   切片分配，是性能微调，不是逻辑简化——分支数不降反升。不满足"只有真正简化逻辑才做"的标准，维持
   现状，不排期。

3. **【已撤回】~~`since` 未显式配置时，锚点在每次加载/热重载时都用"重载那一刻"重新计算~~，这不是
   一个问题。** 见附录 C.7——不配 `since` 本身就是"我不关心精确对齐到哪一刻"的显式声明；真的在意
   分钟级相位的场景，写一个显式 `since` 就行（现在已经支持，见附录 C.3）。这条撤回。

4. **【已展开为独立讨论，见附录 C.4】`quota.BucketIndex`/桶闸模型的复杂度问题，不再作为一条孤立的
   "并列打平"边角案例处理**——第二轮讨论里用户指出这整套桶/闸机制本身没有讲清楚在解决什么问题，
   已经从第一性原理重新推导，给出三个选项，见附录 C.4。

5. **【维持：已评估，ROI 不足，登记备查】`quota.LimitKey` 用"排序后的模型名以逗号拼接"给 Scope 做
   Bucket Key 后缀，理论上如果模型名本身包含逗号会撞 key。** 现实中上游模型名不会包含逗号，风险
   接近零，真要堵成本也很低（换成 `"\x00"` 分隔符），但没有理由现在做，记录在案。

**没有发现的**：没有发现需要撤销已完成改动的问题（架构方向、Scope 落点、per-Limit 折算字段的选型
判断是站得住的）；没有发现遗漏的关键测试。上面第 1/3 条恰恰是第二轮讨论发现"复盘本身判断错了"的
两个例子——记录在这里而不是悄悄删掉，是因为这类误判本身也是有价值的信息。

---

## 附录 C：第二轮追问与讨论（不改代码，仅记录讨论）

<!-- Ver 2026-08-22, by Sonnet 5 -->

以下按用户追问的顺序逐条回应。要求：不预设"文档已经写了就是对的"，只看论证本身是否站得住；能推翻
的地方不含糊地推翻，不能推翻的地方把理由摊开讲清楚（而不是"设计文档已经论证过"这种诉诸权威的说法）。

### C.1 硬门控（用淘汰代替重排）真的能简化逻辑吗？

**先把"简化"这个词拆开，因为它可能指两件不同的事：**

**(a) 如果只是把"耗尽的 Limit 排到梯队末位"换成"耗尽的 Limit 从候选集里物理删除"，其余排序逻辑
不变** —— 这**不会**简化代码，反而会更复杂：现在的 `reorderTier` 只做一件事（对挂了 Limit 的成员
按 score 排序，位置不变，长度不变）；换成淘汰之后，还要处理"删除后梯队变短、要不要继续往下一梯队
借位"“如果所有候选都被删空了怎么办"这些新状态，之前又要重新引入 429/Retry-After 那一整套。这不是
简化，是搬了一块石头到另一个地方。

**(b) 如果是指"干脆不要 headroom 打分这套东西了，只做二元判断：没超限的按 priority 原样走，超限的
直接跳过"** —— 这个**才是真正的简化**，而且简化幅度很大：`quota.Headroom`/`UsedFrac`/`TimeLeftFrac`/
`BucketIndex`/`ScoreForLimits`/`GateReserve`/`HeadroomCap` 这一整套都可以删掉，换成每条 Limit 一个
`used >= amount` 的布尔判断，代码量会显著下降。这个简化是真实的，我之前没有把这一点讲清楚。

**但这个简化不是免费的，代价是这个功能存在的原始理由。** 设计文档最初提出这个功能，动机是"多个账号
的账期/重置日经常不对齐，该选哪个"这个具体问题：三个账号里，一个刚重置（看起来剩得最多）、一个正常
消耗、一个马上要到期但还有大量没花的额度——只看"是否超限"这个二元信号，三个账号在没到硬顶之前全部
"合格"，选哪个只能看 `priority`/配置文件顺序，完全分不出"哪个更快就要浪费掉预付费额度"。这不是我在
护着一个旧决定——这是"要不要在多个都合格的账号之间做出更聪明的选择"这个问题本身还要不要解决。如果
你的真实场景里账号数量少、额度都很宽裕、根本不在乎这种"智能分流"（比如就一两个账号，几乎不会出现
"该选谁"的两难），那么方案 (b) 这种更简单的二元判断确实更合适，直接砍掉整套打分机制是合理的。如果
你还是希望在"都合格"的多个账号里挑一个更不容易浪费预付费额度的，那这套打分逻辑就还有存在的必要，
"淘汰"和"打分"要解决的不是同一个问题，不能互相替代。

**独立于要不要打分，"淘汰 + 429"这个动作本身还有一层额外的、纯粹关于风险的问题**，和"要不要打分"
无关：本地额度计数不是权威值，是一个估算——`internal/quota`/`internal/respnorm` 里能验证到的事实是：
usage 只要拿不到（压缩响应、厂商不返回 usage 字段、流被截断）就会降级成按字节数估算（±30% 误差）；
`token_weights`/`model_multipliers` 要用户自己校准，新部署的账号在校准完成之前，`amount` 的口径大概率
是错的（`/admin/status` 专门暴露 `estimated_pct` 这个"这个数字有多少是猜的"信号，这个信号存在的
唯一理由就是这些数字本身不够可信，值得让人去核对）。用一个知道自己不完全可信的数字去做"直接拒绝一个
本来可能成功的请求"这种不可逆动作，下行风险是：一个刚上线、`amount` 还没校准好的新账号，会被系统性地
拒绝服务，而它本来是能正常处理请求的——这比"多打一次上游、失败了走 failover"的成本要高得多，而且是
新用户第一次接触这个功能时最容易撞见的场景（因为"校准 `amount`"这一步天然发生在部署之后）。

**结论**：如果只是要"简化实现"，真正的简化点在于要不要保留打分机制，不在于"淘汰 vs 重排"这个动作
本身——保留打分则淘汰不会让代码变简单；不要打分则可以砍掉一大块代码，但代价是放弃"多账号智能分流"
这个能力。硬门控 + 429 这个具体机制额外还背着"拿不可信的数字做不可逆动作"这条风险，这条和是否简化
无关，是独立的一条理由。如果你评估下来觉得你的实际场景不需要"智能分流"，我建议的方向是"精简掉整套
打分机制，改成二元判断 + 现有 priority 排序"，而不是"加一层硬门控叠在打分之上"——后者只会让系统
更复杂而不是更简单。

### C.2 Scope 使用示例与通配符

**示例缺口，属实。** `config.example.yaml` 目前只有一行注释掉的 `# models: [premium-model]`，没有
一个完整的、能直接照抄的"账号总限 + 某模型子限额"组合场景。设计文档 `docs/VirtualModelRouter_Design_v4_Quota.md`
§9.1 的 `plan-with-submodel-cap` 才是完整例子，但那份文档不是大多数人配置时会先打开的文件。下次落地
时会把这个完整例子搬到 `config.example.yaml`/`.zh` 里（这轮不动文件，先记在这里）。

**通配符：好消息是这个需求已经被满足了，只是换了一种写法。** 当前设计里，**不写 `models:` 字段本身
就等价于"覆盖所有模型"**——这正是"省去罗列模型名称的麻烦"要的效果，你完全不需要为"其余所有模型"
专门写点什么：

```yaml
quota:
  limits:
    - {metric: requests, every: 1mo, amount: 50000}                        # 不写 models: → 覆盖全部模型，不用罗列
    - {metric: requests, every: 1d, amount: 200, models: [premium-model]}  # 只需要罗列"例外"的那一个
```

如果你想要的是"显式写一个 `*` 表示'我知道这条覆盖全部模型'"（纯粹图心里更清楚、不依赖"忘写等于全部"
这种隐式规则），这个可以做，成本很低——校验时把 `models: ["*"]` 当成和"不写"完全等价处理即可。但这
会让"覆盖全部模型"多出一种拼写方式（不写 / 写 `"*"`），两种写法表达同一件事，算是一种表达力上的
重复，值得掂量是不是真的需要。

如果你想要的其实是**真正的通配符匹配**（比如 `models: ["gpt-*"]` 匹配所有以 `gpt-` 开头的模型，用来
减少"罗列"这个动作本身，而不只是省去"罗列全部"这一种特殊情况），那是一个完全不同、明显更复杂的功能——
需要引入前缀/glob 匹配逻辑，还要想清楚"一个模型同时命中好几条 Limit 的通配规则时该怎么办"（类似
`pricing.overrides` 那边已经在处理的 first-match-wins 问题，Scope 目前完全没有这层复杂度，因为一个
模型只可能落在"被显式列出"或"没被列出（全部覆盖）"这两种状态里，从不会有歧义）。如果这是你的真实
诉求，请告诉我具体场景（比如某个供应商真的会用命名前缀区分一组模型），我可以单独评估，不建议现在
顺手加一个星号了事——这类"看起来简单实则打开新问题"的功能面，值得单独想清楚再做。

### C.3 `since` 时间格式——收回之前的判断，采纳"纯时间"格式

**之前的判断错了，原因是我没有讲清楚 RFC3339 完整时间戳其实已经能表达任意的时间对齐点。** 举例：
`since: "2000-01-01T09:00:00+08:00"` 这一行，年份月份日期完全是随便选的（只是凑一个合法日期），
真正起作用的只有"09:00:00"和"+08:00"这两部分——`PeriodStart` 是从这个锚点按 `every` 的步长往前推的，
所以无论 `every` 是 `5h` 还是 `1w`，这个账号的窗口永远会落在"09:00、14:00、19:00……"（`5h`）或者
"这个锚点对应的周几 08:00"（`1w`）这样的节奏上，能力上没有缺失，只是写法比较绕（要编一个无意义的
日期才能把"几点几分"这个真正想表达的东西塞进去）。

**重新过一遍提案 §3.3 那张五格式表，逐项看是否值得补：**
- "完整日期时间"（空格分隔）——RFC3339（`T` 分隔）已支持，空格分隔只是排版偏好，两种写法表达同一件
  事没有实际收益，不建议加。
- "分钟级完整日期时间"（秒归零）——RFC3339 里把秒写成 `:00` 就是完全等价的效果，不需要单独格式。
- **"纯时间 `hh:mm[:ss]`"（仅 `min`/`h` 合法）——这条是真正有缺口的，采纳。** 对一个只关心"每小时/
  每分钟的哪个时刻对齐"、根本不关心"哪一天"的场景（典型例子正是 RPM 闸这种 `min`/`h` 级窗口），逼着
  写一个假日期只是为了凑一个完整时间戳，确实是不必要的啰嗦——这条缺口是真实的，应该补。而且提案把它
  限制在"仅 `min`/`h` 合法"这一点本身是对的，不是我之前笼统否掉整张表时应该一起否掉的部分：`week`/
  `month` 级别的对齐光有"几点几分"是不够的，还需要知道"哪一天/哪一周几"，纯时间格式表达不了这个信息，
  写在 `w`/`mo` 上会有歧义，所以提案把它限定在 `min`/`h`，这个限制是必要的语义约束，不是随手加的
  复杂度。

**结论**：追加支持"纯时间 `hh:mm[:ss]`"格式，仅允许 `every` 单位是 `min`/`h` 的 Limit 使用，与提案
§3.3 一致；其余四种格式（空格分隔完整时间戳、分钟级完整时间戳）维持现状不追加——它们和已支持的
RFC3339 表达的是同一件事，多出来的只是写法，不是能力。

### C.4 桶 vs 闸——从第一性原理重新推导

这条被问得最多、也最值得认真拆开讲。先完全抛开"设计文档已经这么定了"这件事，只看问题本身。

**先厘清一件事：你说的"任何一条 Limit 命中限制都要降级"，这条现在无论桶还是闸都已经完全满足**，
和桶闸角色完全无关——`score = min(所有 Limit 的信号)`，`min()` 这个操作本身就保证了"最紧的那条说了
算"：任何一条 Limit 接近或到达上限，无论它被标成桶还是闸，都会把最终分数拉低。桶闸角色**唯一**影响
的是相反方向的情况：**一条 Limit 明显没用到多少的时候，要不要因此主动提高这个账号的优先级**（也就是
"分数能不能超过 1.0"）。这不是"任何一条命中限制就降级"这条规则的一部分，是一条额外加的、单独的
规则。

**这条额外规则从哪来？** 回到最初的问题定义：多个按周期计费的套餐，重置日经常不对齐，一个账号 87%
的周期已经过完但只用了 60% 额度，这份没花完的额度过了这个周期就作废了——所以"主动多用一点这个账号"
是有实际经济收益的（不然就是白白浪费已经付过钱的额度）。这个"欠用应该主动加分"的逻辑，只在"额度是
预付费、过期作废"这个前提下成立。**厂商拿来限流用的短窗（比如 RPM 5 分钟/小时闸），性质完全不同**：
这个窗口用不满，本身没有任何经济价值——不存在"5 分钟窗口没跑满，这部分'配额'作废浪费了"这种说法，
它纯粹是厂商保护自己基础设施的节流阀，跑不满是正常状态，不是"浪费"。**这是桶和闸应该区别对待的
根本原因——不是因为哪份文档这么写，是因为这两种 Limit 背后对应的是两件性质不同的事**：一个是真实
花了钱、会过期作废的预付费额度；一个是没有经济含义的速率保护阀。把两者的"欠用"都同等地当作"应该
主动加分"的信号，会导致一个账号仅仅因为**这一秒**它的 RPM 窗口恰好空着（下一秒可能就填满了，一分钟
一次的窗口天然波动很大），就获得了和"这个月真的还有大把预付费额度没花"同等的优先级提升——这个信号
是噪声，不是真实的"这个账号更值得多用"。

**那 `GateReserve=0.5` 这个具体系数呢？** 这个我认为站不住，应该拿掉——它是一个没有任何数据支撑的
调参数字（"闸的剩余额度比例只要不低于剩余时间比例的一半就保持中性"，为什么是一半不是三分之一或者
四分之三，没有依据）。闸真正需要的性质只有一条：**永远不能把分数往上抬**。要做到这一点，不需要一个
可调的"提前预警"缓冲带，直接把闸的信号硬性封顶在 1.0 就够了：`headroom(闸) = min(1, raw)`——闸自己
的 `raw` 只要还 ≥ 1（没有落后于节奏），就贡献一个中性的 1.0；一旦落后（`raw < 1`，也就是消耗速度已经
超过了这个窗口本该有的节奏），才开始跟桶一样往下掉。这比现在的"提前打八折"式渐进封顶更直接，也去掉
了 `GateReserve` 这一个魔数。

**把三个可选项摆在一起，供你选择：**

| 方案 | 桶（长周期）行为 | 闸（短窗）行为 | 复杂度 | 代价 |
|---|---|---|---|---|
| **A（现状）** | `raw` 不封顶，可以 >1 主动加分 | `min(1, raw/0.5)`，提前渐进压制 | 最高——多一个 `GateReserve` 魔数、一个"哪条是桶"的判定 | 闸的渐进封顶没有数据支撑 |
| **B（建议）** | `raw` 不封顶，可以 >1 主动加分 | `min(1, raw)`，硬封顶、不渐进 | 中——仍需要"哪条是桶"的判定，但去掉了 `GateReserve` | 无——去掉了一个没依据的魔数，"任何 Limit 命中限制就降级"和"只有真预付费额度能加分"两条都还成立 |
| **C（最简）** | `min(1, raw)`，和闸一样硬封顶，从不加分 | `min(1, raw)`，硬封顶 | 最低——不再需要区分哪条是桶、哪条是闸，`score = min(所有 Limit 的 min(1,raw))` | 放弃"账号周期过完前主动多用没花完的预付费额度"这个能力——回到最初的问题定义（§1.1 三个重置日错开的账号该选谁），方案 C 会退化成三个账号只要没超限就是同一优先级，选不出"哪个更快就要浪费"，这不是 P3 新引入的问题，是连 P1/P2 已经在生产跑着的单 Limit 行为都会被这个简化收窄 |

**我的建议**：采纳 B——去掉 `GateReserve` 这个无依据的魔数，把闸的规则从"渐进压制"简化成"硬封顶"，
这一步是纯粹的简化，没有看到需要保留渐进带的理由。至于要不要进一步走到 C（彻底放弃"欠用主动加分"
这个能力，把 P1/P2 已经在跑的单 Limit 行为也一起简化掉）——这个我不替你做主，因为它取决于一件我不
知道的事实：**你的账号是不是真的存在"过期作废的预付费额度"这种东西**（如果你所有的 Limit 本质上都是
限流阀、没有"过期不用就浪费"的账单周期，那 C 反而是更贴合实际情况的模型，A/B 里那个"桶"角色根本
无的放矢）；如果确实有真实按月/按周计费、过期作废的额度，C 会让"该选哪个账号"这个问题重新变得和
"完全没有额度感知"一样笨。这是一个需要你根据自己面对的真实供应商合同判断的问题，不是一个能从代码
本身推出答案的问题。

### C.5 撤回：Scope 模型名不应该校验"是否在路由表里"

你的反驳是对的，撤回这条。原文提议校验的对象是 `providerModels`——`config.go` 在校验 `models[].endpoints[]`
时收集出来的、"这个 Provider **当前实际被路由到**的模型集合"，这和"这个 Provider **真实能提供**哪些
模型"完全是两回事。用前者去校验 `quota.limits[].models`，等于强迫用户的额度配置和路由配置时刻保持
同步——你提前给一个还没接进路由表的模型开好子额度、或者临时把某个模型从路由表摘掉但保留它的额度配置，
都会被误判成"typo"。而且项目里压根没有一个"这个 Provider 真实支持哪些模型"的权威数据源可以拿来对
——连标准定价表自己都不完整（`docs/VirtualModelRouter_Design_v4_Quota.md` §13 已经如实写了国产第一方
厂商的覆盖率问题），没有基准的校验做不出来，不是"值不值得做"的问题，是这个检查从设计上就立不住。

### C.6 撤回：`since` 默认值的热重载漂移不是问题

你的反驳是对的，撤回这条。逻辑上确实站得住：**不写 `since` 这件事本身，就是"我不在乎精确对齐到
哪一刻"的显式声明**——如果真的在乎分钟级的相位（比如"我这个 RPM 闸必须严格对齐到每个整分钟"），
写一个显式 `since` 就行（现在已经支持到分钟级：`since: "2026-01-01T00:00:00+08:00"`，附录 C.3 还会
再加一个更省事的纯时间写法）。既然"不配"和"配了但不关心具体是哪一分钟"在语义上是一回事，那"每次
热重载都用当下重新计算锚点"就是这条隐含语义下完全合理的行为，不是缺陷。之前把这个当成一条"值得
关注"的问题，是想多了。

---

## 附录 D：第三轮追问与定案

<!-- Ver 2026-08-22, by Sonnet 5 -->

### D.1 硬门控澄清——确认维持现状，不做任何改动 ✅ 已确认，无需改动

先回应"删除了怎么恢复"这个具体疑问：**不会有"恢复不了"这个问题，因为根本不需要专门写恢复逻辑。**
`quota.Registry` 的重置是惰性的——`Used`/`Charge` 每次被调用时都会用当下时间现算一次 `PeriodStart`，
和桶里存的对不上就地清零（`resetIfStaleLocked`）。这意味着不管一个耗尽的端点在周期没转之前是被"排到
队尾"还是被标记成"暂不可用"，只要周期一转，下一次读它的额度就自动是 0/满血——这条对现在的实现和你
描述的"标记不可用、周期过了自动恢复"是完全一样的，"怎么恢复"这件事在两种做法下都是免费的，不需要
专门写代码维护"删除了的东西怎么放回来"这种状态，这也是你的直觉（"不可用，但不是删除，过一段时间会
恢复"）判断对的地方——现有实现本来就不是"删除"，从来没有物理删除过任何候选。

那"排到队尾"和你说的"暂不可用"两种说法，实际效果上唯一的差别在哪？**只在"这个虚拟模型下所有候选
账号同时都超限"这一种边界情况里**：
- **排到队尾（现状）**：即使全部超限，还是会矬子里拔将军，挑一个分数最高（通常是最快要恢复/超限
  程度最轻）的去试。这次尝试可能成功（因为本地估算的额度本来就可能是错的，上游实际没超），也可能
  失败然后走 failover，最终所有候选都试过还是失败，请求本身才失败。
- **标记为暂不可用（如果做成"全部不可用就不发起任何尝试"）**：直接判定请求失败，一次上游请求都不
  发。少打了一次可能白打的请求，但代价是：如果本地额度估算恰好是错的（这个账号其实没超），这个请求
  本来能成功，现在却被无端拒绝了。

这正是附录 C.1 里已经展开过的"硬门控风险"那部分论证——不管中间叫它"淘汰"还是"标记不可用"，只要
最终行为是"不去尝试"，就是在用一个不保证准确的本地计数器去做一次不可逆的拒绝动作。**这条风险和
"淘汰"这个词本身没关系，是"要不要在全部超限时仍然去试一次"这个行为选择带来的**。

**你已经在追问里给出了结论**："如果说目前就从简化的角度来讲，排到队尾已经足够简化了，不需要改了，
我觉得那我也接受"——这条**予以确认，定案**：维持现状，`reorderByQuota`/`reorderTier` 不做任何改动，
耗尽的端点永远留在候选集里、只是排到本梯队末位，不引入任何"标记不可用"这类新状态，也不会在全部超限
时提前熔断。这个话题到此结束，不再是待讨论项。

### D.2 Scope 语义重新设计——采纳你的三分法，这是对现有实现的一次真实修正 ✅ 已实现

先复述一遍确认我理解对了：

| 写法 | 语义 | 计费/打分粒度 |
|---|---|---|
| 不写 `models:` | **Shared**——这条 Limit 覆盖 Provider 下所有模型 | 所有命中的模型**共用同一个**计数器 |
| `models: ["*"]` | **Per-model，无限成员**——这条 Limit 规则应用到所有模型，但每个模型各自独立 | 每个实际命中的模型**各自拥有一个独立**计数器 |
| `models: [具体列表]` | **Per-model，限定成员**——只有列出的模型适用这条规则，没列出的模型完全不受这条 Limit 约束 | 列表里**每个**模型**各自拥有一个独立**计数器；不影响列表外的模型 |

这个理解对吗——如果对，这**不是我原来 Scope 实现的行为**，需要在下一次动代码时改，这里先记录下来
是一处真实的设计缺口，不是文字游戏：

**现在的实现（有问题）**：`models: [a, b, c]` 会把 a/b/c **三个模型的消耗算进同一个共享计数器**，
只是把 d/e/f… 排除在外——本质上还是"Shared，只是缩小了成员范围"，不是"a/b/c 各自独立"。这既不是你
说的第二种（per-model，无限成员），也不是第三种（per-model，限定成员），是一个你没有要求过、也不
需要的第四种形态（"共享，但限定子集"）——是我在设计 Scope 时想当然加上去的，没有对应你的真实需求。

**目标设计（下次实现时按这个改）**：

1. **一条统一规则，不需要单独的 `mode:` 字段**——`models:` 字段本身**同时**承担"哪些模型适用"和
   "是否独立计数"两件事：**只要 `models:` 被设置了（无论是 `"*"` 还是具体列表），这条 Limit 就是
   per-model 语义**；`models:` 完全不写，才是 shared 语义。`"*"` 和"具体列表"的唯一区别是成员范围
   （全部 vs 限定），计数粒度（各自独立）是一样的。比提案原来的 `mode: shared|per_model` + `models:`
   两个字段更省一个字段，且不会出现"`mode` 和 `models` 互相矛盾（比如 `mode: shared` 却给了
   `models:` 列表）"这类需要额外校验的组合。
2. **Bucket Key 需要按实际命中的模型区分，不能再是"整条 Limit 一个 key"**。现在的 `quota.LimitKey`
   只看 `Limit` 本身（`metric/every`，Scope 非空时加一个"排序后的模型列表"做后缀）——这个后缀是**静态
   的、来自配置**，不是"这次实际是哪个模型"。要支持"每个模型各自独立"，per-model 语义下的 key 必须
   带上**这次实际命中的那个模型**，形如 `requests/1d#model=deepseek-r1`、`requests/1d#model=llama3`，
   而不是 `requests/1d#deepseek-r1,llama3`（现在这种把整个候选列表拼进 key 里的做法）。这意味着
   `LimitKey` 要从"只看 `Limit`"变成"看 `Limit` + 实际发生这次计费/打分的模型"，`per_model` 时按
   模型参数生成 key，`shared` 时忽略模型参具、退化成现在的行为。
3. **校验层面的新增约束**：`"*"` 应该是一个保留token，不是一个真实模型名——如果 `models:` 里同时
   出现 `"*"` 和其他具名条目（比如 `models: ["*", "deepseek-r1"]`），这是矛盾写法（`"*"` 已经覆盖了
   `deepseek-r1`），应该在加载期报错，不能静默接受其中一种解释。
4. **看板展示会因此变得更复杂，这是一个诚实的额外成本，不是可以忽略的细节**：现在 `/admin/status`/
   `vmr check` 是"每条配置的 Limit 对应一行"，因为一条 Limit 到底会落在哪些 bucket 上，配置阶段就能
   完全确定。**per-model 语义下这条假设不成立**——`models: ["*"]` 覆盖"所有模型"，但配置阶段并不
   知道未来实际会有哪些模型命中它（尤其是这个 Provider 下模型列表以后还会增减），只有等真的发生过
   计费之后，`vmr-quota.json` 里才会实际出现这些 per-model 的 bucket。这意味着展示这一层要从"遍历
   `Snapshot` 里配置的 Limit 列表"改成"额外遍历 `Registry` 里实际落盘的 bucket"，才能看到 `"*"` 
   实际展开出了哪些模型的独立计数——这个改动比"改一下 `LimitKey` 的签名"要大一些，需要在下次实现时
   一并规划，不是顺手带过的小事。

**明确不做的**：不支持 `gpt-*` 这类前缀/glob 通配（你已经确认不需要），`"*"` 是唯一的保留字面量，
不是一个模式匹配语法——这让校验和实现都简单很多，不需要引入任何匹配引擎。

**落地记录（2026-08-22）**：`core.Limit.Models` 语义按上表实现——`quota.IsWildcardModels`/
`quota.PerModel`/`quota.AppliesToModel`/`quota.ModelSetsOverlap` 是新增的判定函数；
`quota.LimitKey(l, model)` 签名改为显式接收"这次实际计费的模型"，共享 Limit 忽略这个参数，
per-model Limit 用它拼出 `metric/every#model=<model>` 的 bucket key；`quota.Registry.Keys(provider)`
新增，用来枚举一个账号名下实际存在过的 bucket key（per-model Limit 的真实成员集合，尤其是通配写法，
无法在配置阶段推算出来，只能从 Registry 里现读）；`router.QuotaStatus`/`vmr report` §2.5 都改成
"每条共享 Limit 一行，每条 per-model Limit 按实际计费过的模型各一行"；`config.validateQuota` 的
重复 key 检测从"比较两个静态 key 是否相等"改成"比较两条 Limit 的 Scope 是否可能命中同一个模型"
（`quota.ModelSetsOverlap`），因为 per-model Limit 不再有一个能在配置阶段算出来的静态 key。
`"*"` 与具名条目混写在 `config.validateQuota`/`resolveModelsScope` 里报错拒绝。测试覆盖：
`internal/quota/scope_test.go`（判定函数单测）、`internal/router/quota_multilimit_test.go`
（端到端：通配 Limit 给不同模型开出独立 bucket、`QuotaStatus` 按实际计费的模型枚举行、限定列表
只对列表内模型产生行）、`internal/config/quota_test.go`（通配校验、重叠检测）。

### D.3 桶 vs 闸——采纳方案 B，定案 ✅ 已实现

按你的指示，采纳附录 C.4 的方案 B：保留桶/闸角色区分（长周期 Limit 当桶、可以 `raw>1` 主动加分；
其余更短的 Limit 当闸），但把闸的公式从"`min(1, raw/GateReserve)` 渐进压制"简化成"`min(1, raw)`
硬封顶"，去掉 `GateReserve=0.5` 这个没有数据支撑的魔数。

**落地记录（2026-08-22）**：`internal/quota/score.go` 的 `GateReserve` 常量已删除，`ScoreForLimits`
里闸分支从 `min(1, raw/GateReserve)` 改为 `min(1, raw)`。`internal/quota/score_test.go` 里依赖
旧数值的断言已同步换算（§5.2 类型 A 算例：闸的最终分数从 0.833 变成 0.417 = 自己的 `raw` 值，
因为 `raw=0.417 < 1` 时硬封顶根本不生效）。

---

## 附录 E：第四轮——逐条对源码核实，并补齐两处"定案未落地"

<!-- Ver 2026-08-22, by Sonnet 5 -->

不采信附录 C/D 里任何一处"已实现"/"落地记录"的自我陈述，逐条回源码核实。方法：读实现文件本身，
跑相关单测，而不是读文档上一版怎么写的。结论按条目标注：

- ✅ 已落实（读了实现代码，且测试覆盖、跑绿）
- ❌→✅ 本轮发现"文档已定案但代码没跟上"，且判断属于低风险无争议，直接补上了
- ✅ 撤回已生效（确认代码里确实没有被撤回的那处逻辑）

### 核实结果

| 条目 | 结论 | 核实方式 |
|---|---|---|
| **D.2 Scope 三分法重构**（`models:` 决定"哪些模型适用"+"是否独立计数"） | ✅ 已落实 | 读 `internal/quota/scope.go`（`IsWildcardModels`/`PerModel`/`AppliesToModel`/`ModelSetsOverlap`/`LimitKey`/`PerModelPrefix`/`ExtractModel` 全部存在，逻辑与文档描述一致）+ `internal/config/quota.go` 的 `resolveModelsScope`/`validateQuota` 重叠检测 + `router.QuotaStatus`/`cmd_report_quota.go` 按实际计费模型展开成多行。`go test ./internal/quota/... ./internal/router/... -run "TestScope|TestPerModel|TestQuotaStatus_PerModel"` 全绿 |
| **D.3 桶/闸方案 B**（去掉 `GateReserve`，闸改 `min(1,raw)` 硬封顶） | ✅ 已落实 | 读 `internal/quota/score.go`：`GateReserve` 常量确实已不存在，`ScoreForLimits` 就是 `min(1, raw)`。`score_test.go` 断言值与文档记录的换算一致 |
| **C.5 撤回**（Scope 模型名不校验"是否在路由表里"） | ✅ 撤回已生效 | 读 `internal/config/config.go`/`pricing.go`：`providerModels` 只喂给 `resolvePricing`，`quota.go` 的 `resolveModelsScope` 完全没有引用它，确认没有被撤回的那处校验逻辑残留 |
| **C.6 进一步简化**（不写 `since` 时，直接用配置加载/热重载那一刻的当前时间，去掉自然周期对齐） | ✅ 本轮已实现 | 按你这一轮的明确指示做的新改动，见下方"本轮代码改动"第 1 条——比 C.6 原先"撤回"（只是承认漂移不是问题）又往前走了一步：不再是"保留自然对齐逻辑、只是说漂移可以接受"，而是把整套按 `min/h/d/w/mo` 分支对齐的逻辑直接删掉，`DefaultSince` 退化成恒等函数 |
| **C.3 结论**（追加纯时间 `hh:mm[:ss]` 格式，仅 `min`/`h` 合法） | ❌→✅ 发现遗漏，本轮补上 | C.3 原文写了明确结论"采纳"，但读 `internal/config/quota.go` 的 `parseSince` 发现当时只有两种格式（`YYYY-MM-DD`/RFC3339），纯时间格式从未真正写进代码——是一条"讨论定案但没有落地"的遗漏，不是文档撒谎，大概率是讨论完就直接跳到下一个话题、代码这一步漏掉了。方案确定、低风险（纯加法，不改变任何既有格式的解析结果）、无争议（原文已有明确结论），本轮直接实现，见下方第 2 条 |
| **附录 C.2 缺口**（完整的"账号总限 + 某模型子限额"组合示例，原文写"这轮不动文件，先记在这里"） | ❌→✅ 发现遗漏，本轮补上 | 读 `config.example.yaml`/`.zh.yaml`：确认仍只有孤立的单行注释示例，没有一个把共享池 Limit 和按模型子限额 Limit 叠在一起、可以直接照抄的组合场景。原文自己承认这轮没做，等于是一条已知欠账，非新问题；同样低风险无争议（纯文档，抄的是设计文档 §9.1 `plan-with-submodel-cap` 已经定案的写法），本轮直接补上，见下方第 3 条 |

### 本轮代码改动（均已跑 `go build ./...`、`go test ./...`、`go test -race` 相关包、`archtest`，全绿）

1. **`internal/quota/period.go`**：`DefaultSince(unit, now)` 改签名为 `DefaultSince(now)`，函数体从五个分支的日历对齐逻辑简化为 `return now.In(fmtutil.DisplayZone)`。`internal/config/quota.go` 调用点同步改。`internal/quota/period_test.go`/`internal/config/quota_test.go` 里断言"自然对齐到某个 Monday/整点"的用例改为断言"落在 `[before, after]` 这个加载耗时区间内"。
2. **`internal/config/quota.go`**：`parseSince` 新增第三种格式——纯时间 `hh:mm[:ss]`（正则 `pureTimePattern`），仅当 `every` 单位是 `min`/`h` 时接受，否则加载期报错并在错误信息里点名"min/h"。新增测试 `TestQuota_HappyPath_PureTimeSince_MinAndH`/`TestQuota_Reject_PureTimeSince_OnNonMinH`。
3. **`config.example.yaml`/`.zh.yaml`**：在 Scope 说明注释块后追加一段完整的、可直接取消注释使用的组合示例（账号总限 `every: 1mo` 共享池 + `premium-model` 的 `every: 1d` 独立子限额），对齐设计文档 §9.1 的 `plan-with-submodel-cap`。
4. **文档同步**：`docs/VirtualModelRouter_Design_v4_Quota.md` §（窗口级字段表）、`docs/UserGuide.md`/`.zh.md`、`config.example.yaml`/`.zh.yaml` 里所有描述 `since` 缺省行为的地方，一并把"自然周期对齐"的旧描述换成"直接取加载/热重载那一刻"，并补上纯时间格式的说明。`CHANGELOG.md` `[Unreleased]` 补了对应的 Added/Changed 条目（这两处都还没发过版，按仓库约定是直接改这两行进 `[Unreleased]`，不是另开一条新记录）。

### 本轮没有发现需要提请你决策的新问题

以上两处遗漏（C.2 示例、C.3 纯时间格式）方案在上一轮讨论里已经定案，本轮只是把"说了但没做"的部分补齐，
不需要你重新拍板。除此之外，逐条核对 D.1/D.2/D.3 与 C.5/C.6 的源码状态，没有发现文档描述与实际代码
不一致的地方，也没有发现新的边角案例——这次是一次"核实通过"的复核，不是又发现了一批新分歧。

### 复查（第五轮）：对自己这一轮的改动做了一次复核，发现并修正一处

不轻信自己上一轮"做完了"的自我陈述，把本轮改动过的每个文件重新读了一遍，外加对全仓库做了
`go build`/`go vet`/`gofmt -l`/`go test ./... -race`/`archtest` 的完整复跑（不只是挑相关包跑）。

**发现的问题（已修正）**：第一版给 `config.example.yaml`/`.zh.yaml` 补 C.2 组合示例时，把新的两条
`limits:`（`every: 1mo` 共享池 + `every: 1d` 按模型子限额）直接追加进了同一个 provider 现有的
`quota.limits:` 注释块末尾——但那个块里已经有一条 `every: 1mo, metric: requests`（缺省 Scope 的
共享池 Limit，前面演示 `since`/`token_weights` 用的）。如果有人把这一整段注释原样取消注释，会得到
两条 `(metric: requests, every: 1mo, 无 Scope)` 的 Limit，触发 `config.validateQuota` 的"重复
limit key"加载期错误——一个本意是"可以直接照抄运行"的示例，实际会验证失败，这比没有示例更糟。
修法：把 C.2 示例从"追加进已有 provider 的 limits 列表"改成独立的 `- name: plan-with-submodel-cap`
provider 条目（对齐设计文档 §9.1 的写法，也对齐 `config.example.yaml` 里其它同类型独立示例——比如
紧挨着的 `anthropic` cost 档示例——的组织方式），不再和任何已有示例共用同一个 Limit 列表。
`TestLoad_RepoExampleConfig_Parses` 复跑确认两份示例文件仍能正常解析。

**结论**：这一处是本轮引入的新错误（不是在核对旧文档时漏看的），已经改正；改正后的全仓库测试
（含 `-race`、`archtest`）全部重新跑绿。除此之外没有再发现新的不一致——`DefaultSince`/`parseSince`
的签名在所有调用点（`internal/config/quota.go`、两处测试文件）保持一致，`GateReserve` 相关的历史
提及全部只出现在"解释这条魔数为什么被删掉"的说明性文字里（`score.go` 注释、设计文档订正段落、本
讨论文档自己的讨论过程），代码里没有任何残留引用；`docs/KNOWN_ISSUES_sonnet-5.md`/`cmd/vmr/cmd_check.go`
里没有需要跟着 `since` 缺省行为变化一起改的文字。
