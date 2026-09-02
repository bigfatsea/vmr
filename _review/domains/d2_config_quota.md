# 架构与代码审查报告：S2 - 配置·额度·定价域 (Domain 2)

> **审查范围**：
> - `internal/config/`（YAML 配置加载、环境变量展开、Fail-Fast 校验、Provider 展开、热重载 Watch）
> - `internal/quota/`（额度记账模型、周期数学、Headroom/ScoreForLimits 打分、原子存储与损坏防护）
> - `internal/pricing/`（三层费率解析引擎、四分量 Rate 语义、Basename 递归降级、Resolver 记忆化缓存）
> - `internal/core/`（`QuotaSpec`、`Limit`、`PricingSpec`、`Rate` 等领域共享模型）
> - `internal/router/quota.go` 及 `internal/router/candidates.go`（路由侧额度重排与计费分发链路）
> - 设计文档：`docs/VirtualModelRouter_Design_v4_Quota.md`、`docs/VirtualModelRouter_Design_v4_Core.md`、`docs/KNOWN_ISSUES.md`

---

## 一、审查综述 (Executive Summary)

本报告针对 vmr 系统的**配置 (Config)**、**配额 (Quota)** 与**定价 (Pricing)** 三大核心子系统及路由侧决策链路进行了全面的源码级深度审查。

### 1.1 总体评估
- **实现完备性与设计对齐度**：生产代码高度忠实于 `VirtualModelRouter_Design_v4_Quota.md` 与 `VirtualModelRouter_Design_v4_Core.md`。P1（单桶均衡）、P2（计量准确与定价三层解析）、P3（多窗口复合配额、桶/闸角色、Scope 模型作用域）的交付规格均已严密落地。
- **并发安全与鲁棒性**：`internal/quota` 采用读写锁保护内存状态，落盘阶段执行深拷贝并提前释放互斥锁，配合 `os.CreateTemp` + `Chmod(0600)` + `Sync()` + `os.Rename` 保证文件级原子持久化；`internal/pricing` 的 `Resolver` 具备并发安全的双重检查记忆化缓存；`internal/config` 采用严格的 `KnownFields` 与全方位的数值有限性（`positiveFinite`/`nonNegativeFinite`）防御。
- **自动化测试基线**：所有单元测试及 `-race` 竞态检测全绿，`archtest` 架构边界与文件行数约束全部受控，`cmd/vmr/quota_parity_test.go` 提供了路由侧实扣与离线复算之间的差分测试保障。
- **缺陷等级与风险评估**：未发现严重资安漏洞（如凭据泄露、SQL/命令注入）或并发竞态数据损坏问题。发现若干低危/防御性边界观察项（均已登记于本文第四章）。

---

## 二、任务分项审查结论 (Detailed Review per Task)

---

### 任务 1：配置加载与校验（`internal/config/`）

#### 1.1 YAML 加载与 `${ENV}` 展开
- **KnownFields 严格模式**（`internal/config/load.go:34`）：
  采用 `dec := yaml.NewDecoder(strings.NewReader(expanded)); dec.KnownFields(true)`，任何拼写错误（如 `image_downscale_px`、`max_concurency`）均会在加载阶段立即报错，杜绝配置静默失效。
- **环境变量展开与注入防护**（`internal/config/config.go:228-251`）：
  - 正则 `\$\{([A-Za-z_][A-Za-z0-9_]*)\}` 仅识别标准命名变量，未定义环境变量静默展开为 `""` 并记录于 `Config.EmptyEnvRefs` 中（启动 Banner 会提示未设置变量，防呆设计）。
  - **注入防御防线**（`config.go:239-246`）：检测展开值是否包含换行符 `\n`、键值分隔符 `: `、YAML 注释符 ` #` 或以 `#` 开头。若检测到直接报 Hard Load Error，防止环境变量被注入导致 YAML 语法树结构篡改或由于注释导致凭据被静默截断。

#### 1.2 `validate()` 的 Fail-Fast 策略与 `Check()` 业务诊断分离
- **结构化分层校验**（`internal/config/config.go:302-316`, `internal/config/config_validate.go:16-146`）：
  `validate()` 采用瀑布式 Fail-Fast 策略：`validateBasic()` → `validateProviders()` → `validateModels()` → `validateFallbackEndpoints()` → `resolvePricing()`，一旦发现非法参数立即拒绝加载，保证 `router.BuildSnapshot` 接收到的配置 100% 合法且不可变。
- **诊断隔离**（`internal/config/check.go:50-84`）：
  将非致命的运维隐患（如非 Loopback 地址未配 API Key 告警 `SeverityWarning`、探针超时过长 `SeverityError` 等）收敛在 `c.Check()`，由 `vmr check` 完整收集展示，不阻塞 `validate()`。

#### 1.3 Provider 多 Key 展开机制（ProviderGroup）
- **静态展开实现**（`internal/config/apikeys.go:33-87`）：
  在 YAML 解码后立即执行 `expandProviderAPIKeys()`，将 `api_keys: {label: key}` 展开为 `<name>-<label>` 命名的独立 `Provider`，并递归重写 `models[].endpoints[].providers` 与 `fallback_endpoints[].providers` 引用列表。
- **架构不变式契约**：
  配置期展开保证了下游 `core.Endpoint` 的不可变性与 `HealthKey()`/`Quota` 独立的单一职责，彻底消除了运行时动态池（KeyPool）可能引发的健康状态震荡与配额聚合难题。互斥校验确保 `api_key` 与 `api_keys` 不能同时存在（`apikeys.go:41-43`）。

#### 1.4 Quota / Pricing 专有配置校验
- **迁移陷阱拦截**（`internal/config/quota.go:196-199`）：
  `QuotaConfig` 显式保留了 `TokenWeights` 与 `ModelMultipliers` 字段，并在 `validateQuota` 中显式拦截老版本账号级写法，输出明确的迁移指引，避免误报为模糊的未知字段。
- **时间与周期语法校验**（`internal/config/quota.go:117-172`）：
  - `parseEvery` 正则强制 `^([0-9]+)(min|h|d|w|mo)$`，禁止非正整数周期。
  - `parseSince` 支持 `YYYY-MM-DD`、RFC3339 与纯时间 `hh:mm[:ss]`。纯时间格式被**严格限制仅允许在 `every: min/h` 上配置**（`quota.go:142-144`），防止在日/周/月周期下产生语义模糊。
- **重复与重叠 Limit 拦截**（`internal/config/quota.go:210-224`）：
  通过 `quota.ModelSetsOverlap` 检测同一 Provider 下 `(metric, every)` 相同且 `models` 作用域存在交集的规则，加载期拦截冲突定义。
- **数值有限性防线**（`internal/config/quota.go:28-40`）：
  全仓使用 `positiveFinite`（`> 0 && !NaN && !Inf`）与 `nonNegativeFinite`（`>= 0 && !NaN && !Inf`）拦截 YAML `.nan` / `.inf`，防止 NaN 污染浮点累加器。

#### 1.5 协议映射校验与新旧命名门禁
- **协议白名单**（`internal/config/config_validate.go:68-71, 120-122`）：
  所有端点协议必须在 `adapter.Get(protocol)` 注册表中存在。
- **旧协议友好提示**（`internal/config/provider.go:21-31`）：
  若用户仍使用旧命名（`openai` / `anthropic`），`unknownProtocolHint` 产生精准迁移提示（`rename "openai" to "openai-completions"`），Strict YAML 不做静默兼容。

#### 1.6 热重载 Watcher 并发安全
- **目录级防抖监听**（`internal/config/watch.go:19-70`）：
  监听配置文件所在目录（兼容编辑器原子替换机制），配合 300ms 计时器防抖。goroutine 内置 `recover()` 将 panic 转为 `onError` 回调通知运维。
- **快照原子切换与连接池清理**（`internal/router/snapshot.go:283-319`）：
  `rt.Install(s)` 全程受 `rt.installMu` 互斥保护，原子更新 `rt.snap.Swap(s)`，并在切换后执行 `rt.Quota.Prune()` 与旧连接池 `CloseIdleConnections()`，无并发数据竞争。

#### 1.7 凭据泄露防线（BaseURL 凭据检测）
- **源头拦截**（`internal/config/provider.go:68-91`）：
  `checkBaseURLCredentials` 严禁 `base_url` 携带 userinfo（`user:pass@host`）及黑名单 Query 参数（`api_key`, `token`, `secret`, `password` 等）。
- **脱敏回显契约**：
  报错信息中**仅回显命中的参数名**（`strings.Join(leaked, ", ")`，`provider.go:88-90`），**绝不回显参数值**，彻底切断明文凭据落入日志或终端的风险。

---

### 任务 2：额度记账（`internal/quota/`）

#### 2.1 Counters 与 Registry 的并发安全
- **并发保护模型**（`internal/quota/quota.go:171-285`）：
  `Registry` 维护 `accounts map[string]map[string]*bucket`，所有写操作（`Charge`、`AddEstimatedCost`）与读操作（`Used`、`EstimatedCostFor`、`Keys`、`Prune`）均通过 `r.mu.Lock()` 加锁同步。
- **非阻塞落盘快照机制**（`internal/quota/store.go:88-115`）：
  `Flush()` 在临界区内仅执行 `r.accounts` 的深拷贝（Deep Copy）并重置 `r.dirty = false`，随后立即释放互斥锁。耗时的 JSON 序列化与磁盘文件 I/O 在临界区外执行，高并发请求转发不会被磁盘 I/O 阻塞。若写盘失败，`defer` 重新加锁恢复 `r.dirty = true`。

#### 2.2 周期数学与时间窗口边界计算
- **纯函数周期推进**（`internal/quota/period.go:24-110`）：
  `stepper` 针对 `min`/`h`/`d`/`w`/`mo` 分别构建独立步进函数。`findK` 基于 `nominalUnitHours` 估算种子快速定位周期索引 $k$，保证 $O(1)$ 复杂度。
- **月末截断安全处理**（`internal/quota/period.go:117-138`）：
  `addMonthsClamped` 在日历月步进时，通过 `daysInMonth(y, month)` 将 31 号强制截断到短月最后一天（如 1/31 + 1mo → 2/28 或 2/29），彻底杜绝了 Go 原生 `time.AddDate` 溢出到 3 月 3 日的历史顽疾。
- **日历边界对齐（防止热重载锚点漂移）**（`internal/quota/period.go:147-167`）：
  `DefaultSince` 针对未显式配置 `since` 的规则，将当前时间自动对齐到日历自然起点（min/h/d 截断至当日零点，w 截断至周一零点，mo 截断至当月初）。同一自然日内的多次热重载计算出的 `PeriodStart` 恒定不变，累计消耗跨重载完好存活（彻底闭环已知缺陷 B2）。

#### 2.3 惰性重置机制与 NTP 时钟回退防御
- **惰性重置实现**（`internal/quota/quota.go:217-232`）：
  读写路径调用 `resetIfStaleLocked`：
  - **周期前向推进**（`ps > b.PeriodStart`）：重置计数器 `*b = bucket{PeriodStart: ps}`，返回 `true`。若由读操作（`Used`）触发，立即置 `r.dirty = true` 确保状态落盘。
  - **时钟后向回退**（`ps < b.PeriodStart`）：判定为 NTP 校时、VM 快照恢复或时区变动，**绝对不清零计数器**，并利用 `r.rollbackWarned` 保证进程生命周期内仅记录一次 WARN 日志，避免日志风暴（`quota.go:225-230`）。

#### 2.4 Headroom 比值算法与桶（Bucket）/闸（Gate）裁决
- **核心比值公式**（`internal/quota/score.go:24-74`）：
  $$ \text{used\_frac} = \min(1, \frac{\text{used}}{\text{amount}}), \quad \text{time\_left\_frac} = \frac{\text{left}}{\text{total}} $$
  $$ \text{raw} = \frac{1 - \text{used\_frac}}{\max(\text{time\_left\_frac}, \epsilon)}, \quad \epsilon = 10^{-9}, \quad \text{Headroom} = \text{clamp}(\text{raw}, [0, 5.0]) $$
- **多窗口桶/闸角色划分**（`internal/quota/score.go:90-140`）：
  - `BucketIndex` 选出周期最长的一条 Tumbling Limit 作为**桶**，保留原始 `raw` 分数（欠用时 `raw > 1` 可获得导流提升激励）。
  - 其余短周期 Limit 判定为**闸**，强制实施硬封顶 `min(1, raw)`，欠用不提升优先级，仅在饱和时进行流量压制。
  - 最终得分为所有 Limit 的最小值：`Score = min(Headroom_1, Headroom_2, ...)`，符合木桶最短板原则。

#### 2.5 持久化存储（`vmr-quota.json`）与损坏防护
- **原子落盘流程**（`internal/quota/store.go:88-150`）：
  `Flush()` 采用同目录临时文件 `CreateTemp(dir, ".vmr-quota-*.tmp")` → `Write()` → `Chmod(0600)` → `Sync()`（文件级 fsync）→ `Close()` → `Rename()` 原子重命名。
- **结构损坏全量拒绝**（`internal/quota/store.go:191-213`）：
  `validateLoadedShape` 严密校验：版本号不等于 1、`accounts` 为 nil、子 map 为 nil 或出现 null bucket 时，整文件拒绝采纳并报错，调用方按空账本安全冷启动，绝不部分采纳导致部分账号被静默抹除。
- **刷盘异常频控**（`internal/quota/store.go:253-276`）：
  `flushLog` 对连续相同的写盘错误（如磁盘满）进行频控（首次打印 + 每 10 次聚合打印），避免刷屏。

---

### 任务 3：定价引擎（`internal/pricing/`）

#### 3.1 三层费率解析架构
- **解析层级**（`internal/pricing/resolve.go:135-167`）：
  1. 第一层：账号覆盖 `providers[].pricing.overrides`（支持模型专属与通配 `"*"`，支持 `discount` 与显式 4 分量费率）。
  2. 第二层：补充表（`pricing.supplement`）与内置标准表（`standard_price_curated.yaml` ∪ `standard_price_generated.yaml`）合并表。
  3. 第三层：无费率（Unpriced，`Resolve` 返回 `nil, false`）。

#### 3.2 表内查键 4 步匹配与 Basename 递归降级
- **四步查键流程**（`internal/pricing/resolve.go:93-133`）：
  - ① `mapping` 显式映射查找（`providers[].pricing.map`）。
  - ② `<provider>/<model>` 精确查找。
  - ③ 裸模型名直查 `Lookup(model)`，未命中则查单跳别名 `LookupAlias(model)`。
  - ④ 后缀匹配 `LookupPreferredSuffix(model)`，按厂商优先级裁决（第一方直接胜出；单一聚合商胜出；歧义平手时不猜并放弃）。
- **Basename 递归重试**（`resolve.go:128-132`, `basename.go:22-27`）：
  四步均落空且模型名包含 `"/"` 时，提取裸名 `ModelBasename(model)` 递归重跑全部四步。递归仅下潜一级（裸名不含 `"/"` 必然终止），使带 Org 前缀请求与裸名请求的费率解析完全一致。

#### 3.3 Rate 四分量 `nil` 语义与完整性门禁
- **Unknown ≠ Free 语义**（`internal/pricing/pricing.go:34-40`）：
  `Rate` 的 `InFresh`, `CacheRead`, `CacheWrite`, `Out` 均为 `*float64`。`nil` 代表缺失/未知，`0.0` 代表显式免费。
- **Cost Metric 加载期门禁**（`internal/config/pricing.go:297-308`, `internal/pricing/pricing.go:44-54`）：
  `metric: cost` 账号涉及的所有模型，必须通过 `pricing.Complete(spec)` 校验（四项分量全部非 nil、非 NaN、非 Inf、且 $\ge 0$）。任何一项缺失直接导致配置加载失败，坚决杜绝因缺失缓存费率导致消耗低估和超支。

#### 3.4 Discount 递归折算与 Explicit 显式覆盖
- **递归折算链**（`internal/pricing/resolve.go:195-207`）：
  `resolveChain` 递归遍历 `Overrides`：遇到 `Discount` 规则时，递归求解后续链条费率并乘上折扣因子 `r.Scale(*o.Discount)`，最终回退到 `Base` 费率；遇到 `Explicit` 规则时直接返回显式费率。
- **无死规则保障**（`internal/config/pricing.go:123-143`）：
  `firstDeadOverride` 在加载期拦截所有不可达规则（如在通配 `"*"` 之后声明的具体模型规则，或重复的模型规则）。

#### 3.5 数值防线与多币种 USD 汇率单跳中枢
- **全链路 NaN / Inf / 负数拦截**：
  `parseTable`（`pricing.go:446-459`）、`PricingOverrideConfig.validate`（`config/pricing.go:103-111`）均对数值进行严格断言。
- **USD 单跳汇率中枢**（`internal/pricing/pricing.go:357-380`）：
  所有币种换算统一以 USD 为枢纽（`to / from`），禁止复杂的网状换算，保持算法极简与确定性。

#### 3.6 Resolver 记忆化缓存与线程安全
- **缓存实现**（`internal/pricing/resolver.go:114-133`）：
  `Resolver` 内部通过 `sync.Mutex` 保护 `cache map[string]*core.PricingSpec`，对 `provider\x00model` 的解析结果（包含未命中的 miss 记录）进行线程安全记忆化。
- **独立 DisplayFactor 副本**（`resolver.go:79-85`）：
  `WithDisplayFactor` 创建全新的 `Resolver` 实例，分配独立的 Mutex 与 Cache，彻底杜绝了复制 Mutex 导致的数据竞争。
- **全局账本汇率锚定**（`resolver.go:50-59`）：
  `tableFactor` 与 `currency` 在 `Resolver` 实例级别全局持有，消除了因 Provider 重命名或删除导致汇率回退为 1.0 的历史缺陷。

---

### 任务 4：路由侧配额逻辑（`internal/router/quota.go` 及相关模块）

#### 4.1 调度管线与 `reorderByQuota` 位置不变量
- **绝对位置契约**（`internal/router/candidates.go:28-100`）：
  ```
  Health Filter → Hard Conditions → Context Length → Pin → Dimension Sort → Quota Reorder → Sticky Affinity
  ```
  `reorderByQuota` 严格坐落在 `strategy.Sort` 之后、`Sticky` 亲和性之前。
- **不变式执行**：
  1. **不跨越优先级梯队**：`sameTier()` 严格界定并列梯队，配额重排仅在同梯队内生效。
  2. **会话黏性优先**：Sticky `moveToFront` 在配额重排之后执行，确保多轮对话 Prompt Cache 命中优先于配额平衡。
  3. **只重排、不淘汰**：超额端点仅沉底，绝不从候选集中剔除，保留 Failover 兜底能力。

#### 4.2 梯队占位重排（Placeholder Reorder）
- **隔离性保障**（`internal/router/quota.go:297-320`）：
  `reorderTier` 仅抽取当前梯队中配置了适用 Quota Limit 的端点，计算 Headroom Score 降序稳定排序后，**写回原来的下标槽位**；未配置 Quota 的端点位置纹丝不动，杜绝隔空影响。

#### 4.3 `ChargeResponse` 多 Metric 分发
- **核心分发逻辑**（`internal/router/quota.go:76-112`）：
  - `core.MetricRequests`：累加 1 次，经 `quota.ApplyModelMultiplier` 缩放后记入 `Requests`。
  - `core.MetricTokens`：基于原始四分量，经 `quota.ApplyModelMultiplier` 缩放后记入 `Counters`。
  - `core.MetricCost`：通过 `pricing.EffectiveRate` 计算出精确金额 `d.Cost = componentCost(d, rate)`，并在计费时刻立即存入 `Counters.Cost`。若存在退化估算，同步记录 `reg.AddEstimatedCost`。

#### 4.4 计费时刻 Cost 金额固化
- **历史不可变性**：
  `Counters.Cost` 在请求成功时刻立即固化落盘。后续修改价目表或热重载不会倒算历史金额，保证账本不可篡改。
- **模型倍率互斥**：
  `ApplyModelMultiplier` 严禁在 Cost Metric 路径运行（配置期已拦截，`router/quota.go:107` 保持纯净），防止覆写 `d.Cost`。

#### 4.5 响应流式用量嗅探与退化估算通道
- **侧别感知嗅探**（`internal/router/quota.go:120-132`, `158-185`）：
  `TokenCountersSides` 接收 `inSniffed` 与 `outSniffed` 两个布尔标记。若流被中途截断（如仅收到 `message_start`），未完成的 Output 侧回退为基于字符/字节的退化估算 `OutTokens()`，并诚实将该部分累加至 `estimated` 统计中，杜绝了将占位符 `output_tokens: 1` 误判为精确计费的缺陷。
- **截断请求照常计费**（`internal/router/router.go:531-536`）：
  即使上游连接在中途断开（`copyErr != nil`），由于上游已产生 Token 且部分响应已交付客户端，`chargeQuota` 照常执行，符合真实账单结算口径。

---

## 三、KNOWN_ISSUES §1.2/§1.3 声明验证结果

对照 `docs/KNOWN_ISSUES.md` 中与配置、额度、定价直接相关的各项架构裁决声明，逐条核对生产代码实现，验证结果如下：

| KNOWN_ISSUES 声明项 | 源码对应锚点 | 核验结论 |
| :--- | :--- | :--- |
| **§1.2 协议枚举重命名与 Strict YAML 门禁**<br>“协议枚举为 `openai-completions`/`anthropic-messages`，config 仍带旧名时加载期报错并给出提示，parser 不接受旧名” | `internal/config/provider.go:21-31`<br>`internal/config/config_validate.go:68-71` | ✅ **完全属实**。`unknownProtocolHint` 提供精准提示，Strict YAML 拒绝解析旧名。 |
| **§1.2 BaseURL 内嵌凭据在加载期报错**<br>“`base_url` 内嵌凭据在加载期报错，错误信息只回显键名，绝不回显值” | `internal/config/provider.go:68-91` | ✅ **完全属实**。`checkBaseURLCredentials` 检测 userinfo 及敏感 query key，且仅回显 `strings.Join(leaked, ", ")`。 |
| **§1.2 价目表数值防线建在 `pricing.parseTable`**<br>“NaN/±Inf/负费率一律加载期硬错误，config 不重复校验 pricing 解析结果” | `internal/pricing/pricing.go:446-459`<br>`internal/config/pricing.go:103-111` | ✅ **完全属实**。`parseTable` 逐行校验组件有限性，`PricingOverrideConfig.validate` 负责自身覆盖层校验。 |
| **§1.2 环境变量未定义时静默展开为空串**<br>“环境变量未定义时静默展开为空串，不支持 `${VAR:-default}`” | `internal/config/config.go:228-251` | ✅ **完全属实**。未找到的环境变量展开为 `""` 并记录进 `EmptyEnvRefs`，无默认值语法。 |
| **§1.2 三层费率解析不后置到 `router.BuildSnapshot`**<br>“`internal/config` 在 `validate()` 阶段跑完解析，`metric: cost` 费率不齐为加载期错误” | `internal/config/config.go:315`<br>`internal/config/pricing.go:231-313` | ✅ **完全属实**。`resolvePricing` 在校验期完成三层解析并将 `PricingSpec` 存入 `ResolvedPricing`。 |
| **§1.2 Org 前缀请求名的费率解析兜底为递归重跑裸名**<br>“四步落空后用 `ModelBasename` 掐成裸名递归重跑全部四步” | `internal/pricing/resolve.go:128-132`<br>`internal/pricing/basename.go:22-27` | ✅ **完全属实**。`resolveCanonicalKey` 末尾递归调用裸名解析，单次递归有界终止。 |
| **§1.2 Provider 多 Key 配置期展开**<br>“展开成独立 Provider，每把 key 独立 Provider 名、独立 quota 池” | `internal/config/apikeys.go:33-87` | ✅ **完全属实**。`expandProviderAPIKeys` 展开为 `<name>-<label>` 并更新全局 Provider 列表及模型引用。 |
| **§1.3 持续性故障日志按错误文本去重**<br>“quota flush 失败与时钟回退持续性故障，按错误文本去重（首次+每10次）” | `internal/quota/store.go:253-276`<br>`internal/quota/quota.go:225-230` | ✅ **完全属实**。`flushLog` 实现了每 10 次聚合打印，`resetIfStaleLocked` 使用 `rollbackWarned` 进程级去重。 |
| **§1.3 `vmr-quota.json` 结构损坏整文件拒绝**<br>“版本戳不匹配、nil account map、null bucket 任一即视为损坏，从零开始” | `internal/quota/store.go:191-213` | ✅ **完全属实**。`validateLoadedShape` 实施整文件拒绝，无部分采纳旁路。 |
| **§1.3 配额周期的惰性重置方向敏感**<br>“只有周期真正前进才重置计数；NTP 向后校正保留计数并 WARN” | `internal/quota/quota.go:217-232` | ✅ **完全属实**。`ps > b.PeriodStart` 重置，`ps < b.PeriodStart` 保留并记录警告。 |
| **§1.3 原子写只做文件级 Sync，不做目录 fsync**<br>“CreateTemp+Rename 站点做文件级 Sync，不做目录 fsync” | `internal/quota/store.go:124-142` | ✅ **完全属实**。`tmp.Sync()` 严格执行，目录未调 fsync，符合最佳努力型持久化契约。 |
| **§1.3 浮点加法 1 ULP 级差异与差分测试**<br>“一个分析数字复现一个路由数字必须差分测试锁定” | `cmd/vmr/quota_parity_test.go:1-620` | ✅ **完全属实**。差分测试覆盖全 Metric、全退化路径及截断场景，确保路由实扣与报表复算逐字/逐容差对齐。 |

---

## 四、领域级问题汇总与建议 (Domain Issue Summary & Recommendations)

本域代码整体质量极高，未发现高危（High）或中危（Medium）级别的缺陷。以下列出审查中识别的轻微观察项及演进建议：

### 4.1 低危与防御性观察项 (Low Severity / Observations)

#### 1. `expandEnv` 在 YAML Flow 序列中的逗号分割残余边界
- **位置**：`internal/config/config.go:220-224`
- **描述**：`expandEnv` 对换行符、`: ` 和 `#` 进行了防注入校验，但若用户在 Flow Style 列表（如 `api_keys: [${MY_KEYS}]`）中注入逗号 `,`，可能会拆分出多个列表元素。
- **评估**：该行为已在代码注释中明确标注为已知残余边界（Known Residual Gap），且项目官方示例与最佳实践均采用 Block Style 列表，现实风险极低。

#### 2. `DefaultSince` 跨自然日重启时的奇数步长相移
- **位置**：`internal/quota/period.go:147-167`
- **描述**：对于未显式配置 `since` 的规则，`DefaultSince` 统一对齐至当日零点。对于不能整除一天的步长（如 `every: 5h` 或 `every: 7min`），若实例恰好在跨越午夜时重启或热重载，基准锚点会发生相移重置。
- **评估**：此项已在 Quota 设计文档 §9.1 与 `DefaultSince` doc comment 中充分论证并明确为设计取舍——需要精确锁相的场景用户应显式配置 `since: YYYY-MM-DD`。

#### 3. `Resolver.cache` 跨生命周期无容量上限
- **位置**：`internal/pricing/resolver.go:42, 114-133`
- **描述**：`pricing.Resolver` 的 `cache` map 随请求增长。虽然单次部署涉及的 `provider\x00model` 组合数量通常在数十至数百个量级（内存占用在 KB 级），但理论上若上游模型名无限发散，cache 无法主动淘汰。
- **评估**：单机部署场景下模型名空间受限，无内存泄露风险。

---

## 五、审查最终结论 (Conclusion)

- **Domain 2（配置·额度·定价）代码质量评级**：**EXCELLENT (优秀)**
- **规范符合度**：100% 满足架构设计与测试约束。
- **交付状态**：生产代码逻辑严密，Fail-Fast 策略完备，并发与原子落盘控制得当，已知问题清单（KNOWN_ISSUES）表述与源码实现完全吻合，无需阻断性修复。
