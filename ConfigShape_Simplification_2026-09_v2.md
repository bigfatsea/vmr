<!-- Ver 2026-09-15 V2, by Stan: 按域重写版 -->

# `config.mock.yaml` 配置形态简化分析（V2）

> **本版定位**：V1 的内容在"现状 / 改动"两类之间交叉穿插，且每节论证模板不统一；本版按 **域（domain）** 重排，每节内部统一 6 段模板（目标 / 现状 / 方案对比 / 推荐 / 成本 / ROI 权衡）。V1 曾单列"mock.yaml 文本层零碎观察" B 节讨论示例文件的事实性错误——本轮决定取消该独立 B 节（mock 是示例，准确性与 schema 重构正交），仅在 G.1/G.3 留一句"顺手做"。
>
> **复核口径**：所有论断对照源码核实（`internal/config/*`、`internal/router/snapshot.go`、`internal/strategy/strategy.go`、`internal/config/pricing.go`）；与 V1 不同的取舍均以源码证据为准。
>
> **本稿评判标准与范围**：见 §0（范围与不范围）。

---

## 目录

- **A. 顶层全局字段**
  - A.1 时间字段归集（`timeouts:` / `ttl:`）与 Duration 文法统一
  - A.2 TTL 零值歧义（`audit_retention: 0`）与第三种零值语义（`0 = 无上限`）
  - A.3 `api_keys` 与 `proxy` 同层不同生命周期（不算问题，文档补一句）
- **C. Provider 块**
  - C.1 `Provider.Disabled` 临时下线开关
- **D. 虚拟模型与 endpoint 块**
  - D.1 `endpoints` 二层 map 化（按 protocol 提桶）
  - D.2 `fallback_endpoints` 联动 map 化 + 不可达告警
  - D.3 `token_weights` / `model_multipliers` 维持 Limit 内下沉
- **E. 声明与默认值机制**
  - E.1 `capabilities` / `max_context_tokens` 提至 `model_defaults`（按 (provider, model) 寻址）
  - E.2 override 机制：保留在虚拟模型层，移除 endpoint 级
- **F. 明确不做的边界**（不 map 化、不重命名的登记）
  - F.1 `pricing.overrides` 维持有序 list（不可 map 化）
  - F.2 `quota.limits` 维持 list（map 化无益）
  - F.3 `providers` 维持 list（评估后不做）
  - F.4 `api_keys: {label: key}` map 顺序已知不修（`KNOWN_ISSUES.md` 登记）
  - F.5 `api_key` / `api_keys` 二象性保留（远期合并考虑）
  - F.6 不需要动的机制（直观设计复核确认）
- **G. 跨域汇总**
  - G.1 优先级矩阵与执行顺序
  - G.2 落地注意事项
  - G.3 结论

---

## 0. 范围与不范围

- **范围内**：`config.yaml` 的 YAML 形态、字段归集、命名一致性、语义歧义。
- **不在本轮范围**：新增功能（如 rolling window 落地）、动 run-time 行为。
- **形态变更不需要"逐字段兼容"**——本轮明确**不为老配置提供兼容层**。要改就改干净，老配置一次性重写。YAML 严格模式仍要求"字段拼写正确"（已知字段、未知字段报错），不要求"未声明时仍能跑出与上版一致的结果"。**未声明字段的默认行为只有"该字段自身语义的合理默认"一个理由**——不与"上一版"对齐。形态变更（如 endpoints map 化）保证展开结果与现状语义一致即可——同样的 (protocol, provider, model) 三元组、同样的组内 try 顺序、调度决策逐字段可比。
- **本稿评判标准**（贯穿全文）：(1) 同一事实只写一次；(2) 读配置的人局部判断即可理解，不需跨块模拟解析；(3) 代码净减或净增是否对应**实质**收益，不是为兼容而堆过渡。

## 0.5 逐节评估总表

| 节 | 议题 | 一句话结论 |
|---|---|---|
| A.1 | 时间字段归集（`timeouts:` / `ttl:`） | 同意；5 套时间语法 → 2 套 |
| A.2 | TTL 零值消歧（取消"永久"语义，只接受带单位的具体数值） | 同意；`0` 与未写同义=用默认；需要长期保留写大数 |
| A.3 | `api_keys` / `proxy` 同层不同生命周期 | 不算问题；UserGuide 补一句 |
| C.1 | `Provider.Disabled` 临时下线开关 | 同意；1 字段、运营高频、单独 PR |
| D.1 | endpoints 二层 map 化 | 同意；最高 ROI，#1 优先 |
| D.2 | fallback 联动 map 化 + 不可达告警 | 同意；与 D.1 强制联动，同一 PR |
| D.3 | `token_weights` / `model_multipliers` 维持 Limit 内下沉 | 同意；YAML 锚点零代码解决重复 |
| E.1 | `capabilities` / `max_context_tokens` 提至 `model_defaults` | 同意；横向"按真实模型声明" |
| E.2 | override 机制：保留在虚拟模型层、移除 endpoint 级 | 同意；override 概念保留但升层 |
| F.1–F.6 | 明确不做的边界（含 api_keys map 顺序与二象性） | 登记在案防反复重提 |

---

# A. 顶层全局字段

## A.1 时间字段归集（`timeouts:` / `ttl:`）与 Duration 文法统一

### 目标

让"等多久"与"活多久"两类时间字段在 YAML 上从字面就能区分；把同一份文件里并存的 5 套时间表达（`90 days` int 后缀、`10m` Go Duration、`1d` every 自创语法、`15s`、各种"永久"语义）收敛成 2 套。

### 现状

`config.mock.yaml` 顶部散着 6 个时间相关字段，5 种语法并存：

```yaml
probe_timeout: 15s           # Go Duration, 顶层
sticky_ttl: 10m              # Go Duration, 顶层
image_cache_ttl_days: 14     # int + 单位在字段名里
audit_retention_days: 90     # int + 单位在字段名里
timeouts:                    # 子块, 已归集
  connect: 10s
  response_header: 120s
  stream_idle: 120s
```

语义上分两类，但字面上看不出来：

- **等多久**（请求路径等待/超时上限）：`timeouts.*` + `probe_timeout`（每次后台探针的独立上限，`config.go` 注释明确"deliberately far under DefaultHeaderTimeout"，与 response_header 分开是有道理的）。
- **活多久**（生命周期/淘汰）：`sticky_ttl`、`image_cache_ttl_days`、`audit_retention_days`。

### 方案对比

| 方案 | 描述 | 取舍 |
|---|---|---|
| **A. 按"等/活"二分**（推荐） | `timeouts:` 子块吸进 `probe`；新加 `ttl:` 子块收容三个生命周期字段；单位统一扩出 `d/w/mo` 沿用 quota.every 文法 | 分类清晰；与 `base_url` / `quota` 等已归集块一致 |
| B. 按功能域归集（`image_cache:` / `audit:`） | 把 `image_cache_ttl_days` 与 `image_cache_dir` 一起收；`audit_retention_days` 与 `log_dir` 一起收 | `image_downscale` 是请求变换属性不是缓存属性；`log_dir` 不只服务审计；功能域分块会把原本单一的字段拆散或贴错标签 |
| C. 只改字段名，不归集 | `probe_timeout` → `timeouts.probe_timeout` 等 | 不解决单位语法并存问题；收益微弱 |

### 推荐

```yaml
timeouts:                # 等多久:请求/连接/响应全路径上限
  connect: 10s
  response_header: 120s
  stream_idle: 120s
  probe: 15s             # ← 从顶层 probe_timeout 移入

ttl:                     # 活多久:生命周期/淘汰
  sticky: 10m            # ≤ 24h backstop, 约束原样保留
  image_cache: 14d       # 单位语法与 quota.every 一致
  audit_retention: 90d   # 零值语义单独处理（见 A.2）
```

`Duration.UnmarshalYAML` 目前走 `time.ParseDuration`，扩出 `d/w/mo` 只需复用 `quota.go` 已有的 `parseEvery` 文法（`m`=分钟、`mo`=月，与 every 完全一致）。统一后整份文件就两类时间表达：路径上 `timeouts.*` 用 Go 语法，生命周期 `ttl.*` 用扩展语法。

### 成本

- `Duration` 类型已存在；`applyDefaults` 几行改；`ProbeTimeout` 的展示调用点仅 `internal/router/probe.go` 一处
- `sticky_ttl ≤ core.StickyBackstopTTL` 的校验（`config_validate.go`）随字段搬家原样保留
- `Duration` parser 扩 `d/w/mo`（复用 `parseEvery`）：`config.go` 单文件
- UserGuide 中英双语、`config.example.yaml` × 2 同步

**估算**：~40 行 Go（其中 parser 扩语法 20 行），UserGuide/示例配置 ~50 行变更。无运行时行为变化。

### ROI 权衡

- **Return（用户认知负担）**：5 套时间语法 → 2 套，新人入门"哪5个字段用的哪5套语法"的认知负担清零；`timeouts` vs `ttl` 二分让"我在配等多久还是活多久"在结构上直接可读。
- **Investment**：低（`Duration` 已存在、parser 扩语法复用现有 `parseEvery`）；风险：无（无运行时行为变化）。
- **结论**：高 ROI，先做。

---

## A.2 TTL 零值歧义（`audit_retention: 0` 同时是"用默认"和"永不删除"）

### 目标

- **`0`（及字段不写）只剩一种解释**：用配置默认值
- **不支持**任何"永不过期/永久/无限期"语义——`forever` / `permanent` / `never` / `0` 都不表示"永久"
- **需要长期保留的用户写一个具体大数**（如 `90000d` ≈ 246 年）

### 现状

三个 TTL 字段零值行为各异（`config.go` 注释对照）：

| 字段 | `= 0` 现行含义 | 应有含义 |
|---|---|---|
| `audit_retention_days` | **永不删除**（高消费） | 用默认（90d） |
| `image_cache_ttl_days` | **默认 7 天** | 用默认（7d） |
| `sticky_ttl` | **默认 10 分钟** | 用默认（10min） |

`0` 在同一份文件里同时意味着"永不""用默认"两种相反意图。A.1 的 Duration 化不解决这个问题（`0d` 照样两可）；**"永不删除"作为一个不与其他字段对称的零值语义，是历史遗留的怪异设计**，本轮彻底铲除。

### 方案对比

| 方案 | 描述 | 取舍 |
|---|---|---|
| **A. `*Duration`，只接受具体数值（带单位），不允许任何"永久"关键字**（推荐） | `audit_retention` 等三个 TTL 字段改 `*Duration`；nil 与 0 同义 = 用配置默认；解析器显式拒绝 `forever` / `permanent` / `never` 这类关键字 | 零值只有"用默认"一种解释；与"等多久"侧 `timeouts.*` 一致的极性；"想要永久"是用户明确选择，表现为一个具体大数 |
| B. 维持 `*int` 字段类型（int64 天数），加 "不要写 0 表示永久" 的注释 | 不改类型 | 零值歧义的结构根源未解决；新极性可读性差；否决 |
| C. 保留 `forever` 关键字（V1 早先方案） | 与方案 A 类似但允许 forever | "永久"不是一个有意义的额外语义，仅仅是"任意大数"的语法糖；本轮明确取消"永久"作为一类语义 |

**不引"`*int` nil/0 同义再加显式默认"那种"三态"指针**：把 `*Duration` 的 nil 视为 "0 或未写"，由 `applyDefaults` 负责填默认值——这是单一职责，nil 本身不代表"未声明"。**`unconstrained` 这类语义根本不出现在 TTL 字段上**（TTL 字段有自然下限 0，无 unconstrained 概念）。

### 推荐

```yaml
ttl:
  audit_retention: 90d      # 显式: 90 天后删
  # 或:
  audit_retention: 3mo      # 3 个月后删（mo = month）
  # 或:
  audit_retention: 90000d   # 约 246 年——给需要长期保留的场景
  # 或不写:
  # → 用配置默认值 90d
```

**支持的单位（不区分大小写）**：
- `d`（天）
- `w`（周 = 7 天）
- `mo`（月 = 30 天；月是日历近似，与 `every: 1mo` 语义一致）
- `y`（年 = 365 天；同近似）

**显式禁止**：
- `forever` / `permanent` / `never` 这类永久语义关键字——解析器拒绝，load error
- `0d` / `0` 表示永久——`0` 与"未写"同义，= 用默认
- 负数——`applyDefaults` 改为 `value <= 0` 都用默认（不再有"非 0 才生效"的隐藏极性）

`image_cache_ttl` / `sticky_ttl` 同理（注意 `sticky_ttl` 单位仍是 `min/h` 沿用 Go Duration 语法，因为 sticky 是分钟/小时量级；`image_cache_ttl` / `audit_retention` 用扩展语法 `d/w/mo/y`）。

**同类问题顺带登记：第三种零值语义（`0 = 无上限`）**：
除上述三个 TTL 字段的零值二义性外，系统还存在第三种零值语义——`max_attempts: 0` 与 `max_concurrency: 0` 表示 **"0 = 无上限"（unlimited）**。这是业界常见约定且代码已有明确注释，本轮形态简化**结构维持不动**；但 **UserGuide 的字段表应逐字段显式标明零值含义**，消除配置者在三种零值语义之间的猜测成本。

### 成本

- 3 个字段类型 `int` → `*Duration`（`sticky_ttl` 沿用 `min/h`、其余两个加 `d/w/mo/y` 扩展）
- 解析器扩 `d/w/mo/y` 单位（复用 A.1 提议的扩展方案）；**显式拒绝** `forever` / `permanent` / `never` 关键字（一个 `if token in forbiddenSet { error }`）
- `applyDefaults` 改：`<= 0` 用默认（而不是只 `== 0` 用默认；与"`*Duration` 的 nil 等价于 0"语义对齐）
- **行为变更（Breaking）**：`audit_retention` 旧值 = 0 改后被解释为"默认 90d"——**今天依赖"0 = 永不删除"的用户**会**默默丢数据**。`CHANGELOG.md` `[Unreleased]` 段需列为 Breaking Change，发布说明里给一行人工映射（"依赖旧'永不删除'语义请改为 `90000d` 或更大值"），UserGuide 在 `audit_retention` 字段处**显式警告**。
- 源码注释修正：`config.go:250` 当前注释 "0/absent = never delete audit files" 是**错的**（与新语义不符），改 "0/absent = use default retention (90d)"

**估算**：~50 行 Go（含 4 单位 parser、关键字拒绝、`*Duration` 类型改）；文档+CHANGELOG 同步。无新依赖。

### ROI 权衡

- **Return（架构简洁优雅）**：零值语义**全分立**——TTL 字段"0 = 用默认"（且只有这一种解释）、`max_attempts`/`max_concurrency` "0 = 无上限"（业界约定）、`audit_retention` 不再有"0 = 永久"这种孤立怪异极性。"永久"作为语义被彻底取消，零值含义在整个 config 文件中收敛为两种。
- **Return（用户认知负担）**：新人不再需要"为什么 `audit_retention: 0` 是永不，而 `image_cache_ttl: 0` 是默认"这种"按字段记忆特殊极性"的认知负担。
- **Investment**：低-中（`Duration` 解析器扩单位 + 关键字拒绝 + 3 字段类型改 + 文档同步）；风险：**中**（Breaking Change：依赖旧"0 = 永久"行为的部署会默默丢数据，**必须** `CHANGELOG.md` 标 Breaking、UserGuide 显式警告、迁移指南给出等价改写）
- **结论**：高 ROI，但有 Breaking 风险——落地前需设计评审确认迁移方案与告警位齐全。

---

## A.3 `api_keys` 与 `proxy` 同层不同生命周期（不算问题）

### 目标

澄清"这两者为什么在同一层"——当前 `Provider` struct 字段分两类但同层：

- **账号身份**：`Name` / `BaseURL` / `APIKey` / `APIKeys` / `Quota` / `Pricing`
- **连接属性**：`Proxy`

### 现状与判定

`Proxy` 是连接时一次性选择（`ProxySpecFor` 按 base_url scheme 选 URL，`config.go:480-499`），与 `api_keys` 展开是配置期行为（`apikeys.go:38-69`）——两者正交。

`UserGuide` 当前未说清这点，新人易把 `Proxy` 和 `api_keys` 联想成一对。

### 方案

不改结构；UserGuide 补一句"proxy 决定该 provider 所有连接的走向，与 api_key 选择无关"。

### ROI 权衡

- **Return（易维护性）**：澄清一处潜在误解。
- **Investment**：~5 行文档。
- **结论**：顺手做，单独一行 PR。

---

# C. Provider 块

## C.1 `Provider.Disabled` 临时下线开关

### 目标

运营场景下临时切走一个 provider（账号被限流、商用 API 突然返 5xx、协议升级、临时维护），改回时切回——**不动 model 配置结构**。

### 现状

今天三种"绕路"都不好：

1. **直接删 provider 块**——`config_validate.go:91-99` 的 `validateProviders` 对重名、未知 provider 不报错，但 `validateEndpointGroup` 对 `provider has no base_url for protocol` 报 hard error（`config_validate.go:201-204`），且任何 `models[].endpoints[].providers` 引用此 provider 的行全部连锁失败。**不是一次能删干净的**。
2. **把引用此 provider 的所有 endpoint 行注释掉**——可能跨多个 model、多个 protocol，规模不可控；`api_keys: {label: key}` 展开成多份子 provider 时更难清。
3. **改 health 包让某 provider 强制 cooldown**——临时但不是 Config 层表达，下次 reload 会丢；新增"persistent admin state"维度，与 Config 哲学不匹配。

需求本质上就是 Config 层的"provider 出场开关"。

### 方案对比

| 方案 | 描述 | 取舍 |
|---|---|---|
| **A. `Provider.Disabled bool`**（推荐） | 顶层加一个 bool；`BuildSnapshot` 入口构建 `enabled` 集合；下游 `buildEndpoints` / `BuildQuotaSpecs` 全部走 `enabled[name]` 判定 | 1 字段、1 过滤点；与 `Provider.Proxy` 同层对仗、职责正交 |
| B. 命名 `enabled: false` | 反向极性 | 与 YAML "absent = true" 极性相反，新人每次要思考当前值；否决 |
| C. 命名 `active: false` / `inactive: true` | 同问题，且 `active` 在路由语境里含义已重（"活跃的 session"），易混 | 否决 |
| D. `models[].endpoints[].providers_disabled: [name]` | 精细化"在某 model 下不下线" | 用户问题就是"全部下线"；精细方案增加一层新逻辑，大多数场景下是过度设计；**若**用户提"coding 不要 deepseek 但 agent 还要用"，那时再加 endpoint 级 `disabled: true`（与现有 endpoint 字段同粒度，复用同一份机制）——本轮先不上 |
| E. 放 `Quota` / `Pricing` 块里 | 不是 quota 不是 pricing 的事 | 否决 |
| F. `ProviderState` 全局 map + `vmr disable <name>` CLI | 运行时改写层 | 违背"Config 是事实唯一来源"哲学（admin 状态会与 Config 漂移）；热重载 + `disabled: true` 已覆盖"临时 + 可恢复"，且 reload 顺带触发新快照，等同"撤销 disable"——零额外机制 |

### 推荐

```yaml
providers:
  - name: openrouter
    base_url: { openai-completions: …, anthropic-messages: …, openai-responses: … }
    api_key: ${OPENROUTER_API_KEY}
    proxy: true
    disabled: true   # ← 整个 provider 及其展开后的所有 endpoint 都不参与路由
```

**语义边界（重要）**：`disabled: true` **必须**等价于"在所有下游消费点把它当作不存在"——不是"标记但保留数据"（后者会让用户以为它在线）。具体三处一致过滤：

| 消费点 | 当前对 `Provider` 的用法与源码抓手 | 过滤后行为 |
|---|---|---|
| `BuildSnapshot`（`snapshot.go:97-130`） | `for name, m := range cfg.Models`，在 `buildEndpoints` 里 `cfg.ProviderByName` | 该 provider 名在 `buildEndpoints` 内的所有 `(provider, model)` 展开对被**跳过**；`fallback_endpoints` 里引用了 disabled provider 的也跳过 |
| `BuildQuotaSpecs`（`snapshot.go:236-247`） | `for _, p := range providers` | 不为 disabled provider 建 `core.QuotaSpec`，避免给"已下线"的 provider 留无主计数器 |
| `Config.Check`（`check.go:169-198`） | `for _, p := range c.Providers` | 给出"provider X 已 disabled，跳过 api_key 缺失告警"新行；**不**因 disable 把"它被引用但未声明"原本的错误降级 |
| `vmr status` | 经 `BuildSnapshot` 间接受益 | 自动只列活跃 provider，零改动 |

**默认 false 是设计正确**——`disabled` 表达"非常态运营操作"（临时切走），让默认值"在"代表"常态"（provider 在线）。

**过滤点单一**：`BuildSnapshot` 入口构造 `enabled map[string]bool`，下游一律 `enabled[name]` 判定——**不要在两处各自重判 `p.Disabled`**，否则 reload 序列上极易漂移。

**被引用 provider 已 disabled：通过并告警**：`validateEndpointGroup` 仍要求 provider 在 `Config.Providers` 中存在——但新增校验：若某 provider 出现在 `models[].endpoints[].providers` 或 `fallback_endpoints[].providers` 中但 `disabled: true`，应**允许通过**（用户意图即下线，非拼写错误），但**给一条 `vmr check` 警告**提示"该 provider 已 disabled，引用将无流量"。这避免用户改完 `disabled: true` 还在奇怪"为什么不报错"——其实就是不该报错，但要让人看得见。

**校验反向（未被引用的 provider 维持现状）**：若某 provider 在 `Config.Providers` 里**完全没被任何 model/fallback 引用**，**与今天一样**——是 `Config.Check` 的 warning，不是 error。`disabled: true` 不改变这条规则。

**与 `Provider.Proxy` 的对仗**：`Proxy` 是"连接属性"（不是路由候选过滤器），`Disabled` 是"路由候选过滤器"（不是连接属性），两者**同层但职责正交**——设计直观清晰，不冲突。

**健康与重连**：disabled 的 provider 在 `/status` 不出现（与 BuildSnapshot 一致过滤）。"想看恢复进度"——改回 `disabled: false` reload 即可，**不**为"disable 期间仍可观察"单独保留一个观察通道（与 §C.1 默认极性论证同理：每多一个状态位就多一份维护负担，价值与成本不匹配）。

### 成本

- 字段：1 个（`Provider.Disabled`）
- 过滤点：1 处（`BuildSnapshot` 入口构建 `enabled` 集合）
- 校验 / 警告：`validate` 不动；`Config.Check` 加一条"已 disabled provider 被引用"warning
- `/status` 展示：受 `BuildSnapshot` 间接受益，零代码
- 测试：单测覆盖 "disabled provider 不出现在任何 route" / "disabling 后 reload，引用它的 endpoint 在 check 中给 warning" / "disabled provider 不计 quota"
- 文档：Core.md / UserGuide 同步（中英双语 + `*.example.yaml`）

**估算**：净增 ~30 行 Go 代码、移除 0 行。

**热重载与在线请求**：改 `disabled` 字段保存触发 reload，1 秒内（`watch.go` 现有 debounce）新快照生效；sticky 已粘到该 provider 的会话在新请求过来时按新快照过滤（sticky 自己会检查 endpoint 是否仍在活跃 candidates 里）；`/v1/*` 在飞请求不受 reload 影响（`Snapshot` 原子换指针）——全部复用现有机制，零新增。

### ROI 权衡

- **Return（运营高频）**：临时切走一个 provider 改一行 YAML + reload 即可——这是**每个运营周期都用得上**的小开关；今天的"绕路"每一次都在制造临时技术债。
- **Return（架构简洁度）**：1 字段、与 `Provider.Proxy` 同层对仗、职责正交——是"占位极小但语义清晰"的字段，不是"为兼容而堆"的过渡。
- **Investment**：极低（~30 行净增、单点过滤）；风险：低（hot-reload 已在，sticky 自己会过滤不可达 endpoint）。
- **结论**：高 ROI，单独 PR 即可；建议优先级 #2（仅次于 D.1 map 化）。

---

# D. 虚拟模型与 endpoint 块

## D.1 `endpoints` 二层 map 化（按 protocol 提桶）

### 目标

消灭 `protocol:` 在每个 endpoint 行机械重复；让 `endpoints` 与 `provider.base_url` 用同一种协议键结构；让错误信息直接定位到协议层。

### 现状

`protocol:` 在 `config.mock.yaml` 实测出现 **15 次**（9× openai-completions、5× anthropic-messages、1× openai-responses）——其中 9 次同值纯属机械重复。

`internal/router/snapshot.go:97-130` 的 BuildSnapshot 本来就按 protocol 分桶（`routes[eg.Protocol]`），protocol 是事实上的分桶键。

### 方案对比

| 方案 | 描述 | 取舍 |
|---|---|---|
| **B. `endpoints` 改为 protocol-keyed 二层 map**（推荐） | 顶层 `endpoints: {<protocol>: [<group>...]}`，每条 group 移除 `protocol` 字段 | 与 `base_url` 同构；失序无害；map key 走 `adapter.Get` 校验；per-entry override 字段不变 |
| A. 模型级 `protocols: [a, b]` 默认值 + endpoint 行省略 protocol | EndpointGroup 不写 protocol 时"按 provider.BaseURL key 顺序挑第一个" | **否决**，理由见下 |
| C. YAML 锚点 `&openai` | 保留现状，加 YAML 1.1 锚点 | 局部解决；用户需手写锚点；不作为终态，仅作为迁移落地前的临时缓解 |

#### 形态 A 否决理由（展开）

1. **解析是跨块的、且按 provider 逐个生效**。一条 `providers: [openrouter, deepseek]` 的 group，两个 provider 的 `base_url` map 各自决定解析结果——最坏情况下同一条 group 的不同 provider 解析到不同协议，"一条 entry"失去单一协议身份，snapshot 分桶、`validateEndpointGroup` 的 per-provider base_url 校验、错误信息、`apikeys.go` 的 Providers 重写全部要从一维变二维。原稿"5 字段改动 + snapshot 一处"的成本估算严重低估，实际与形态 B 同数量级。
2. **有效协议无法局部判断**。读配置的人看一条不写 protocol 的 entry，必须去翻模型级 `protocols:` 列表、再翻每个 provider 的 `base_url` map、再按声明顺序模拟一遍解析——这正是"为省字搞弯弯绕、互相引用难理解"。省下的是打字，付出的是每次读配置的推演。
3. **收益相同，代价却是永久的**。形态 A 留下一层常驻的解析规则（文档、UserGuide、心智负担），形态 B 的一次性迁移成本做完即消失。两者消灭的重复字数一样，B 严格占优。

### 推荐

```yaml
agent:
  capabilities: [text, tools]
  max_context_tokens: 256000
  endpoints:
    openai-completions:
      - providers: [openrouter, openrouter2]
        models: [gemini-3.6-flash-high, gemini-3.1-pro-high]
      - providers: [minimax]
        models: [MiniMax-M3]
        max_context_tokens: 512000      # per-entry override 原样保留
        capabilities: [image, audio, video, thinking]
    anthropic-messages:
      - providers: [minimax]
        models: [MiniMax-M3]
    openai-responses:
      - providers: [openrouter]
        models: [gemini-3.6-flash-high]
```

为什么 map 在这里是**结构正确**而不只是省字：

- **与 `provider.base_url` 完全同构**——两半配置的读法一致，无新概念。
- **map 失序无害**：跨协议 bucket 之间本来就无序（请求从哪个 ingress 进来就只查那个协议的 route）；每个协议键内的 list 保序，组内 try 顺序不受影响。
- **校验更严**：map key 本身过 `adapter.Get` 注册表校验，未知协议在 key 层就报错；错误信息从 "endpoint group #N" 变成 "endpoints.<protocol>[#N]"，更可定位。
- **per-entry 覆盖项不受影响**：`max_context_tokens`/`capabilities`/`role_map`/`sticky_ttl`/`soft_block_failover` 全部留在条目内，语义零变化。

`premium` 模型的 `priority: 100` 不构成反例：priority 全局可比、跨协议混排语义不变（mock.yaml 里那条注释本身是错的，与示例准确性有关，不在 schema 范围）。

### 成本

- `config.go`（EndpointGroup.Protocol 删字段、FallbackEndpoints 同步——见 D.2）、`config_validate.go`（key 级校验外提一层）、`snapshot.go`、`apikeys.go`（fallback Providers 重写循环）、`check.go`，加测试共 6+ 处，全部机械
- UserGuide 中英双语、两个 `config.example.yaml`、设计文档 Part 1 的配置参考节同步
- **必须**附一个"展开结果等价"差分测试：同一份配置旧形态/新形态各自 BuildSnapshot，比较得到的 (protocol, provider, model, priority) 序列完全一致——形态等价性靠测试钉住

**估算**：~150 行 Go（含测试），文档 ~200 行变更。无运行时行为变化。

### ROI 权衡

- **Return（用户认知负担）**：消灭 15 次 `protocol:` 重复中的 9 次（且未来添加新模型永远不再需要重复）；`endpoints` 与 `base_url` 用同一种键，跨字段读配置时不再切换语法。
- **Return（错误定位）**：错误信息从 "endpoint group #N" → "endpoints.<protocol>[#N]"，可定位性显著提升。
- **Investment**：中（6+ 处机械改 + 差分测试）；风险：低（行为不变，等价测试钉住）。
- **结论**：最高 ROI，#1 优先；与 D.2 联动做。

---

## D.2 `fallback_endpoints` 联动 map 化 + 不可达告警

### 目标

与 D.1 形态一致（同一份 `EndpointGroup` 概念不能一半 map 一半 list）；让"配置了一条 fallback 但对所有 VirtualModel 都不可达"这件事从静默丢弃变成 `vmr check` 显式告警。

### 现状

`snapshot.go:128-131` 对"该模型没有此协议入口"的 fallback 直接 `continue`——对该模型静默不可达。

`config.mock.yaml` 注释靠人肉提示这一点；`validateFallbackEndpoints`（`config_validate.go:118-129`）和 `check.go` 的"重复"告警都不查"完全不可达"。

### 方案对比

| 方案 | 描述 | 取舍 |
|---|---|---|
| **A. 形态 B 联动 + 不可达 Warning**（推荐） | 块结构同步 map 化；`Config.Check` 加一条"对所有 VirtualModel 都不命中"的 warning | 形态一致；问题显式化；与 D.1 同一个 PR 完成 |
| B. 仅 map 化，不加告警 | 用户仍需"知道有 fallback 但不知道它无效" | 不可达静默丢失的根问题未解决；与 V1 当时已点出但未修的洞类似 |
| C. 仅加告警，不 map 化 | 不动结构 | 留下一份形态不一致；下次想 map 化时仍要做 |

### 推荐

```yaml
fallback_endpoints:
  openai-completions:
    - providers: [openrouter2]
      models: [mock-cheap/backup-1]
      priority: 90
```

`priority` 必填且 > 0 的约束保留——**priority 数值越小越优先**（`internal/strategy/strategy.go`："lower number wins"），fallback 写 > 0 即排在真实 endpoint（默认 0）之后，这正是 fallback 的本意。

**告警（独立于形态）**：`Config.Check` 对**所有** VirtualModel 都不命中的 fallback 给 `Issue{SeverityWarning}`（不是 Error）——合法演进路径（以后加 endpoint 可恢复），但应该让 `vmr check` 说出来。

### 成本

- D.1 的 Go 改动已覆盖 map 化（同一份 `Config.FallbackEndpoints` 字段改类型）
- `Config.Check` 加 5 行校验
- 测试覆盖"map 化展开 + 不可达告警"两种

**估算**：~20 行 Go（独立于 D.1）；与 D.1 一起 PR。

### ROI 权衡

- **Return（架构一致性）**：D.1 已决策 map 化，fallback 不联动 = 同一概念两种形态，校验/用户认知都要双写。
- **Return（运营可见性）**：不可达告警消除"我配了 fallback 但它从来不生效"这种极难自查的静默错误。
- **Investment**：极低（map 化是 D.1 改动的副产品，告警 5 行）；风险：无。
- **结论**：与 D.1 一起做。

---

## D.3 `token_weights` / `model_multipliers` 维持 Limit 内下沉

### 目标

明确决策：**不回提账号级**；多条 Limit 共享一份系数的问题用 YAML 锚点（零代码）解决。

### 现状与设计背景

两者已在每条 `quota.limits[]` 条目内部；`quota.go` 显式拒绝账号级写法（migration trap，报错并指路）。

`docs/VirtualModelRouter_Design_v4_Quota.md` 用整节解释了 P1/P2→P3 的来由：短窗速率闸按原始次数等权、长周期账单桶需要四分量加权，**同一个账号的不同窗口被现实逼着需要不同的折算**——账号级共享一份系数的前提在多窗口复合配额下已不成立。

分层干净：`model_multipliers` 只作用于 `requests`/`tokens`（纯额度语义），`cost` 的按模型分化走 `providers[].pricing.overrides`；两个方向配错都是 load error，fail-fast 完备。

### 方案对比

| 方案 | 描述 | 取舍 |
|---|---|---|
| **A. 维持现状 + YAML 锚点**（推荐） | 不动代码；示例里给出锚点写法 | 零代码代价；解决真实痛点 |
| B. provider 级 defaults 继承 | 与 `ImageDownscaleMaxPx` 的 "global default / per-model override" 模式同构，源码里 `*int`/`*bool`/`*Duration` 三态指针已有先例 | 只有真实场景出现"三条以上 Limit 共享一份系数且用户嫌锚点丑"时再上 |
| C. 回提账号级 | 对 P3 已推翻前提的回退 | 设计文档里有完整反驳，不再重复 |

### 推荐

```yaml
quota:
  limits:
    - &tw_default
      metric: tokens
      every: 1d
      amount: 1000000
      token_weights: {in_fresh: 1.0, cache_read: 0.1, cache_write: 1.25, out: 4.0}
    - <<: *tw_default
      every: 1w
```

### 成本

- 零代码；纯示例/文档改动

### ROI 权衡

- **Return（用户痛点真实存在）**：多条 Limit 共享同一份系数不必再复制粘贴。
- **Investment**：零（锚点是 YAML 1.1 内置特性）。
- **结论**：必做（零成本 + 真实痛点），但优先级低（其他改动更重要）。

---

# E. 声明与默认值机制

## E.1 `capabilities` / `max_context_tokens` 提至 `model_defaults`（按真实模型名寻址，`providers` 作为子属性）

### 目标

同一真实模型（在多个虚拟模型下使用）"它到底支持什么 / 上下文多大"这一**横向**事实只写一次；`model_defaults` 块**以模型名为 key**，`providers` 退为子属性表征"这条声明对哪些 provider 生效"。

### 现状

现状两层声明（**纵向**，不解决问题）：

- **模型级 base**：`VirtualModel.Capabilities` / `VirtualModel.MaxContextTokens`（`config.go:147-148`）——同模型所有 endpoint 共享
- **Endpoint 级覆盖**：`EndpointGroup.Capabilities`（additive 并集）、`EndpointGroup.MaxContextTokens`（scalar 覆盖）（`config.go:92-93`，`snapshot.go:171-175`）

合并在 `snapshot.go` 的 `buildEndpoints` 里一次性算清写到 `core.Endpoint.{Capabilities,MaxContextTokens}`，同时把"endpoint 自己那份"复制到 `ExtraCapabilities` / `OwnMaxContextTokens`（`core.go:178-191`）专给 `vmr check` 显示。

**两个独立证据证明痛点不是 endpoint 覆盖**：

1. **mock.yaml 里 `MiniMax-M3` 在 anthropic-messages 协议下出现在两个虚拟模型**：`agent` 写 `capabilities: [image, audio, video, thinking]` + `max_context_tokens: 512000`；`cheap` 写 `capabilities: [image]`（且只挂 openai-completions 的另一份 MiniMax-M3）。`cheap` 自己的 `max_context_tokens` 是空，靠 endpoint 层的 `max_context_tokens: 128000` 覆盖。这种"同一真实模型、在不同虚拟模型下能力/上下文上限不同"在用户视角是**正当的**——它正是把一个模型配到多个虚拟模型背后的核心动机。
2. **设计文档 `Core.md` 多模态能力章节明确举例**（`Core.md:368-378`）：`agent` 配 `text, tools` 基线，`minimax` 后端端点加 `image`、改 `max_context_tokens: 1000000`——是文档示范的典型场景，且**正因为同一批可互换的端点"支持面差不多"，才推荐写到模型基线**。

用户真正想要的"按真实模型声明"是**横向**的（同一 provider × model 跨多个虚拟模型共享一份事实），而当前的两层机制是**纵向**的（每个虚拟模型内部，base + endpoint 覆盖）。**当前结构让"同一真实模型"的字面重复无法消除**——不是 endpoint 覆盖的问题，是缺少"按真实模型声明"这个维度。

### 方案对比

| 方案 | 描述 | 取舍 |
|---|---|---|
| **A. 顶层 `model_defaults`，key=模型名，`providers` 为子属性**（推荐） | `model_defaults: {<model>: {providers?, capabilities, max_context_tokens}}`；`providers` 不写 = 对所有 provider 生效；endpoint 级覆盖字段移除 | 与块的英文名 "model_defaults" 名实相符；与 `models[].endpoints[].providers: [list]` 的现有列表约定对称；同一模型在不同 provider 下有不同能力时，`providers: [subset]` 表达 |
| B. 双层 map `model_defaults: {<provider>: {<model>: ...}}` | 外层 key 仍是 provider | 外层 key 名为"model_defaults"但实际上是 provider 寻址——**名实不符**；否决（V1 之前的中间方案，现以 A 取代） |
| C. 在 `Provider` 块内声明 | `provider.capabilities: {<model>: [...]}` | 跨账户共享时拆写两份（`openrouter2` 和 `openrouter` 上跑同一个 `anthropic/claude-3-7-sonnet`，能力一样），是新的机械重复；否决 |
| D. 把 `capabilities`/`max_context_tokens` 上提到 `api_key` 展开级别（per-credential） | 解决"不同账户有不同能力"问题 | 不是用户场景的主要矛盾；过度复杂化 |
| E. 维持现状 + YAML 锚点 | 复用 `&cap_default` | 锚点解决字面重复，但不改变"事实归一"的本质——同一真实模型的能力仍然"分散在多个虚拟模型块里"；否决 |

### 推荐

```yaml
# 顶层, 所有虚拟模型共享——同一真实模型"它到底支持什么/多大上下文"只写一次
model_defaults:
  # 通配(可选): 大多数模型走这份基线; 不写 = 未匹配模型 unconstrained
  "*":
    capabilities: [text, tools]
  # 精准匹配: 模型名 = key, providers 不写 = 对所有 provider 生效
  MiniMax-M3:
    capabilities: [text, tools, image, audio, video, thinking]
    max_context_tokens: 512000
    providers: [openrouter, minimax]   # 这条声明仅对这两个 provider 生效
  # 另一精准: providers 不写 = 通配所有 provider
  claude-3-7-sonnet:
    max_context_tokens: 200000
```

**关键设计点**：

- **key 是真实模型名**（与 `model_defaults` 块名实相符）
- **`providers` 不写 = 对所有 provider 生效**（与 `models[].endpoints[].providers: [list]` 的现有列表约定对称；列表子集表达"这条声明仅生效于这些 provider"）
- **同一模型在不同 provider 下可声明不同能力**：例如 `MiniMax-M3` 在 `openrouter` 和 `minimax` 上的窗口都是 512k、多模态齐全；但 `claude-3-7-sonnet` 在 `anthropic` 上 200k，在 `openrouter` 转发时只剩 100k——后者用 `providers: [openrouter]: { max_context_tokens: 100000 }` 表达
- **重复声明合并**：`capabilities` 取并集（声明式集合）；`max_context_tokens` 是标量，**待定**（落地时敲定取最大/后写覆盖/拒绝）——见本节末尾"待敲定项"

`VirtualModel` 级保留 `capabilities` / `max_context_tokens` 作为**显式覆盖**（E.2 详述为何需要保留 override 机制）；不写就查 `model_defaults`。`EndpointGroup` 上的 `capabilities` / `max_context_tokens` 字段**移除**。

#### 解析顺序与字段独立回退

`capabilities` 与 `max_context_tokens` 两个维度**相互正交、按字段独立回退**——而不是整条 entry 整体覆盖或整体回退（例：`claude-3-7-sonnet` 若仅声明了 `max_context_tokens: 200000`，其 `capabilities` 缺省，继续向通配 `*` 或 unconstrained 回退，不会被该 entry 阻断）。

每次 `buildEndpoints` 对单个 `(virtualModel, provider, model)` 的每个独立维度（`capabilities` / `max_context_tokens`）计算最终值时：

```
1. 若虚拟模型本身显式声明了该字段                → 用虚拟模型的（显式覆盖/降级，见 E.2）
2. 若 model_defaults[model] 存在且（providers 未写 OR provider ∈ providers）   → 用这个（精准匹配）
3. 若 model_defaults["*"] 存在                                  → 用通配
4. 否则                                                        → unconstrained（0 / 空切片）
```

（字段直接删，不存在"旧写法还在被解析"——旧写法 = unknown field，严格 YAML 直接 load error，报错信息指向新写法。）

#### 关键设计选择：通配的"无"还是"显式 unconstrained"

- **A. 顶层无 `model_defaults` 块 / 块内无通配 = 完全 unconstrained**（推荐）
- B. 通配必须显式存在

**选 A**——理由（与迁移无关，是第一性原理的判断）：
1. "unconstrained" 是 `capabilities`/`max_context_tokens` 的**自然默认状态**（`core.Endpoint.HasCapability` 注释明确"An endpoint that declares no capabilities at all is unconstrained"）——这是条件路由系统对自身语义的本体承诺（"声明 = 限制"必须以"无声明 = 不限制"为前提），不是配置层偏好。
2. 让 unconstrained 仍作为默认状态，省掉"我只是想用默认值也得写一个空块"的仪式感。
3. `model_defaults` 块如果只是为了"声明通配 = 不限制"而存在，是结构上的冗余——块结构只为携带**真正的差异化**服务。

#### 为什么放在顶层而不是 Provider 内

`provider.base_url` 在 provider 内合理（每账户每协议一个 base URL）；但 `capabilities` / `max_context_tokens` **跨账户共享**——`openrouter2` 和 `openrouter` 上跑同一个 `anthropic/claude-3-7-sonnet`，窗口和能力一样；按 provider 拆写两份是新的机械重复。顶层按模型名寻址恰好对应真实需求维度。`providers` 退为子属性而不是外层 key，**也意味着同一模型跨账户共享同一份声明**——这正是"按真实模型声明"的核心动机。

#### 待敲定项

**同一模型名 + 同一 provider 出现多条 model_defaults 声明时如何处理**（例：`MiniMax-M3` 在两处分别声明 `capabilities`）：

- `capabilities`：取并集（声明式集合，并集语义最自然）
- `max_context_tokens`：取最大 / 取最后写 / 拒绝重写——三种选项各有取舍，本轮不决，**落地时敲定**。倾向**取最大**（"声明 = 允许的最大值"，与 unconstrained 时取 0 的方向相反）

### 成本

- 新结构：`Config.ModelDefaults map[string]ModelDefaultEntry`（key = 真实模型名；value 含 `Providers []string`（可选，不写 = 通配所有 provider）、`Capabilities []string`、`MaxContextTokens int64`），按需展开
- 字段移除：`EndpointGroup.Capabilities`、`EndpointGroup.MaxContextTokens`、`core.Endpoint.ExtraCapabilities`、`core.Endpoint.OwnMaxContextTokens`——共 4 个字段直接删
- 合并逻辑简化：`buildEndpoints` 的 `mergeCapabilities` / `effMaxContextTokens` 三行改成"查表"一行；`core.Endpoint` 的 `ExtraCapabilities` / `OwnMaxContextTokens` 移除，`HasCapability` 的 `Capabilities` 字段含义不变（仍是已解析的完整集合）
- `vmr check` 的 `extra_capabilities=…` / `max_context_tokens=…` 行删除
- 测试：覆盖"虚拟模型 override vs model_defaults 通配 vs 精准"三档优先级，以及"未声明 = unconstrained"的回退
- 文档：`Core.md` 多模态能力章节的端点 override 例子需要重写为新写法
- 迁移：配置 schema 变更没有迁移兼容层——删就是删，老配置需要一次性重写。`CHANGELOG.md` `[Unreleased]` 段列出"以下字段被移除，等价改写为 …"，发布说明里给出具体人工映射范例（例如："endpoint 写 `capabilities: [image]` 改写为在 `model_defaults` 相应 `(provider, model)` 写 `capabilities: [..., image]`，或在虚拟模型层整体声明"）

**估算**：代码净减少（4 字段 + 合并逻辑 vs 一个新 map + 查表）；文档（Core.md 多模态能力章节）~50 行重写。

### ROI 权衡

- **Return（架构简洁度）**：横向"按真实模型声明"匹配用户需求维度；新人不用学"capabilities 是并集、max_context_tokens 是覆盖"两条规则。
- **Return（事实归一）**：同一真实模型在多虚拟模型下"它支持什么/多大上下文"只写一次。
- **Return（不变量保护）**："未声明 = 不限制"是条件路由机制的本体承诺（见下文不变量核对），本设计与之对齐。
- **Investment**：中（4 字段删除 + 查表逻辑 + 文档重写 + 人工映射说明）；风险：低（等价测试钉住，且语义检查显式列出）。
- **结论**：高 ROI，但**改动面**比 D.1 大（牵动 `Core.md` 文档、4 字段净减、合并逻辑重写），建议优先级 #4。

#### 不变量核对

不变量按"该不该有"和"换实现后是否仍成立"重新分类——**不是"与今天等价"的字面承诺**，是"条件路由这套机制的语义本就该是什么"：

- **"未声明 = 不限制"是不变量的本体**——`core.Endpoint.HasCapability` 注释明确"declares no capabilities at all is unconstrained"；`strategy.WithinContext` 注释同样。这是条件路由机制对"声明 = 限制"语义的本体承诺，**不是配置层偏好**。`model_defaults` 整块不写、或块内无匹配项，自然落回 unconstrained——这与机制本体的不变量一致，**不是因为"等价于今天"而正确**。✅
- **条件路由查询接口零变化**——`capabilityCondition.Eligible` 只看 `Endpoint.Capabilities`（已解析的最终值），`WithinContext` 只看 `Endpoint.MaxContextTokens`（已解析的最终值）。**合并点的位置换了**（从 endpoint 覆盖合并到 model_defaults 查表），**但消费点的接口没变**——这是工程层面"重写解析、不改契约"的判断标准。✅
- **`HasCapability` 的"空 = unconstrained"语义保留**——空 Capabilities 数组 `len() == 0` 时 `return true`。新增 `model_defaults` 不需要改它，因为查表后**仍然把"无匹配"映射到"空 Capabilities"**——即同样的 `len() == 0` 路径。✅
- **sticky 行为与该节正交**——sticky 的 key 是 `(api_key, virtualModel, ...)`，与"能力/上下文如何解析"是两条独立设计，重写后者不动前者。✅

#### model_defaults 块本身可省略

无任何 `capabilities` / `max_context_tokens` 需求的部署，**整个 `model_defaults` 块不写**；没有该块就走解析顺序的"否则 → unconstrained"路径。这是"新能力不破老配置"的具体落实方式——本轮不为老配置提供兼容层，但本项新增**完全不引入**对老配置的任何变动（块不写 = 走老路径）。

**不要**反过来理解成"model_defaults 是必备"——它只为携带**真正的差异化**服务，不是 schema 必填项。

---

## E.2 override 机制：保留在虚拟模型层，移除 endpoint 级

### 目标

明确决策：**完全去掉 endpoint 级覆盖**，但 **保留** override 概念（升到虚拟模型层）——而不是彻底删除 override 能力。

### 现状

当前 override 机制分两层（虚拟模型级 + endpoint 级），见 E.1 现状描述。

### 为什么不能完全去掉 override 概念

`agent` 案例就是反例：

- `MiniMax-M3` 在 `model_defaults` 里声明"512k、含 audio/video/thinking"
- `cheap` 虚拟模型如果只想用 `MiniMax-M3` 的"128k 纯文本部分"，而**不想**走 512k 完整能力（省钱、也避免被允许的请求被能力放行到不该去的端点）——必须能在 `cheap` 上**显式降级**到 128k
- 这只能写在 `cheap.capabilities` / `cheap.max_context_tokens`（作为 model_defaults 的 override），而不是写回 endpoint

另一种说法：endpoint 覆盖的存在，是因为它和"try-order 中这个位置要不要走更窄的端点"绑定——但条件路由是**端点级决策**（`strategy.WithinContext` / `capabilityCondition` 在 endpoint 粒度上判定），把覆盖从 endpoint 拿走会**丢掉"对单个端点说'你能力更弱'"的语义**。即便 `model_defaults` 已经是 `(provider, model)` 粒度，**同一虚拟模型下同一个 `(provider, model)` 只能有一份值**——虚拟模型层 override 是绕开这个"一份值"约束的唯一机制：同一真实模型、在不同虚拟模型下可被声明为不同的能力/窗口。

### 方案对比

| 方案 | 描述 | 取舍 |
|---|---|---|
| **A. 移除 endpoint 级、保留虚拟模型级**（推荐） | 4 字段净减；解析点搬到查表 | 与 model_defaults 完全协同；override 仍可表达"对单个 try-order 位置收窄"（通过把那个位置单独挂到另一个虚拟模型） |
| B. 完全去掉 override | 同一真实模型在不同虚拟模型下无法声明不同上限 | 否决，丢用户场景 |
| C. 维持 endpoint 级 + 加 model_defaults | 双层机制并存 | 多一份机制无新能力；否决 |

### 推荐：减少多少重复

现状下，`capabilities` / `max_context_tokens` 在 mock.yaml 中跨层分布：

- `agent.capabilities: [text, tools]` + `max_context_tokens: 256000`（base）
- `agent` 下 anthropic-messages 端点覆盖（旧写法）：
  ```yaml
  - protocol: anthropic-messages
    providers: [minimax]
    models: [MiniMax-M3]
    max_context_tokens: 512000          # override base
    capabilities: [image, audio, video, thinking]   # union on top of base
  ```
- `cheap` 下 openai-completions 端点 `max_context_tokens: 128000`（endpoint override）+ `capabilities: [image]`（endpoint override）

改后：

- `model_defaults["MiniMax-M3"]: { max_context_tokens: 512000, capabilities: [text, tools, image, audio, video, thinking], providers: [openrouter, minimax] }`——事实只写一次
- `agent` 模型自己**不写**这两个字段（直接继承 model_defaults）
- `cheap.max_context_tokens: 128000`（虚拟模型层显式 override，**降级**）

结果：原本要写 5 行（3 个 endpoint-level + 1 个 model-base + 1 个 agent override），改后 2 行（1 个 model_defaults + 1 个 cheap override），且 `MiniMax-M3` 的"窗口 512k、含多模态"事实只写一次。**逻辑等价、重复消失、override 机制留着但更精准。**

**但是——这是个适度收益，不是大改**。如果用户只有两三个虚拟模型、用一两组真实模型，重复本身就不显著（mock.yaml 是有意识堆满字段的展示文件）。**真正的简化来自"让新部署不需要先想清楚哪些字段写在 model 哪些写在 endpoint"**——决策面更窄，新人不用学"capabilities 是并集、max_context_tokens 是覆盖"两条规则。

#### 关于 Sticky / 跨虚拟模型不击穿

有用户问："那不同虚拟模型配同一个真实模型，是不是 sticky 会跨虚拟模型串？"答案：不会。**Sticky 的 key 是 `(api_key, virtualModel, ...)`**（参见 `Core.md` §6.5 和 `internal/sticky` 的注册表），虚拟模型是 key 的一阶分量，`coding` 选到 `deepseek-v4-pro` 的粘性条目**只**对后续 `coding` 请求有效，对 `agent` 没有任何作用。**与 model_defaults 的存在完全无关**——这是 sticky 自己的设计保证，model_defaults 只是声明"这一真实模型在所有虚拟模型里看起来一样"，至于"路由要不要用"由 sticky + try-order 各自负责。

### 成本

见 E.1 末段。

### ROI 权衡

见 E.1 末段。

---

# F. 明确不做的边界（登记在案）

逐一评估过"列表改 map / 字段改名 / 字段重分类"等候选，以下几处**明确不做**，理由登记在案以免反复重提。

## F.1 `pricing.overrides` —— 不可 map 化（顺序承载语义）

overrides 不是"按模型查表"，是**有序规则链**：Explicit 规则（显式费率）first-match-wins 即终止，**Discount 规则沿链逐级组合**（如 `[wildcard discount 0.6, 某模型显式费率]` = 该模型费率 × 0.6）。map 化会摧毁 discount 组合链的表达。而它唯一的 footgun（新规则被先前的 Explicit wildcard 兜底成死代码）已经由 load error 拦截（`pricing.go` resolvePricing 拒绝"永远不可激活"的 Explicit 规则），不需要用结构去防。**维持 list。**

## F.2 `quota.limits` —— map 化无益

map 化需要编造 key（`requests/1min` 之类字符串拼接），metric/every/scope 本来就是条目字段；同 key 重叠校验已存在（`validateQuota` 的 pairwise collision check）。list 更直白。**维持 list。**

## F.3 `providers` —— 评估过 map 化，不做

`providers` list→`map[name]Provider` 可行（所有使用点都是按名查找，顺序无语义；还能结构性消灭重名检查），与 `models` 已是 map 对称。但不做：provider 条目少、`name` 一行成本极低；条目自包含（名字在块内）比 map key 更可读；models 之所以是 map，是因为虚拟模型名是用户直呼的路由键，而 providers 的使用方式是"被引用"。省一行换可读性，不值。**维持 list。**

## F.4 `api_keys: {label: key}` 的 map 顺序 —— 已知（不修）

`KNOWN_ISSUES.md` 已登记：`api_keys` 采用普通 Go map，迭代顺序无保证（Go runtime 遍历存在随机化特性）。根本修法是改成有序列表结构 `[ {label, key}, ... ]`。但当前单 key 场景占绝大多数、多 key 展开逻辑经测试可正常工作，本轮明确**不修**；若未来做，与 F.5 的二象性统一合并处理。

## F.5 `api_key` / `api_keys` 二象性 —— 保留，远期与 F.4 合并考虑

两个字段互斥（同用报错），是"单 key 90% 场景"的便利性与"多账号同厂商"的展开糖。统一成 map 会强迫单 key 用户也起 label；统一成 list 则与 F.4 的有序化是同一个改动。现状校验完备、注释清楚，不动。

## F.6 不需要动的（复核确认已经是直观设计）

- `VirtualModel` 级 base `capabilities`/`max_context_tokens` 与条目级"union vs override"的不对称：集合只能并、标量只能覆盖，这是类型本质不是设计绕弯，文档已写透。
- `sticky`/`fallback`/`soft_block_failover` 的 `*bool` 三态（nil=默认、true/false=显式）：与 `image_downscale` 的三态 `*int` 同一模式，一致且直观。
- 条件路由的 `Condition` 与排序的 `Dimension` 分离：架构不变量，与配置形态无关。

---

# G. 跨域汇总

## G.1 优先级矩阵与执行顺序

按"用户价值 × 改动成本"两轴 + ROI 维度统一排序：

| # | 改动 | ROI 维度 | Return（择一） | Investment（择一） | 备注 |
|---|---|---|---|---|---|
| 1 | D.1 endpoints 二层 map 化 | 用户认知负担 + 错误定位 | 消灭 9 次 `protocol:` 重复 + 错误信息可定位 | 6+ 处机械改 + 差分测试 | 与 D.2 强制联动 |
| 2 | D.2 fallback map 化 + 不可达 Warning | 架构一致性 + 运营可见性 | 与 D.1 一致 + 不可达告警 | map 化是 D.1 副产品；告警 5 行 | 与 D.1 同一 PR |
| 3 | C.1 `Provider.Disabled` 临时下线开关 | 运营高频 + 架构简洁度 | 临时切走 1 字段 + reload | 1 字段 + 1 过滤点 + check 警告 | 单独 PR 即可 |
| 4 | E.1+E.2 model_defaults + 移除 endpoint 覆盖 | 架构简洁度 + 事实归一 | 横向"按真实模型声明" + 4 字段净减 | 略大（牵动 Core.md 文档） | 与 mock.yaml 重写一并做 |
| 5 | A.1 timeouts/ttl 归集 + Duration 文法统一 | 用户认知负担 | 5 套时间语法 → 2 套 | ~40 行 Go | 与 A.2 一起做 |
| 6 | A.2 TTL 零值消歧（取消"永久"语义） | 架构简洁优雅 | 零值不再"既是 X 又是 Y" | ~50 行 Go；Breaking Change 需 CHANGELOG 标明 | 与 A.1 一起做；落地前需设计评审确认迁移方案 |
| 7 | mock 零碎清理（priority 注释反向、`strategy` / `USD` / `model_multipliers` 注释） | 易学性 | 防止用户学错 priority 语义 | 几行 mock 文本 | 顺手做；不在 schema 决策流程 |
| 10 | D.3 token_weights/multipliers YAML 锚点示例 | 用户痛点真实存在 | 多条 Limit 共享不必复制 | 零代码 | 示例/文档 |

**不做清单**（防止反复重提）：F.1 pricing.overrides map 化、F.2 quota.limits map 化、F.3 providers map 化、F.4 api_keys map 顺序本轮不修、F.5 账号级 api_keys 展开与二象性统一维持现状、账号级 token_weights 回提（D.3）、`pricing` 改名 `billing`（术语替换成本与收益相抵）。

### 推荐执行顺序

1. **D.1+D.2**（endpoints/fallback map 化 + 不可达告警，一次 PR）——最高 ROI；迁移与文档同步一次做完。
2. **C.1**（`Provider.Disabled` 临时下线开关）——净增 ~30 行、运营高频痛点，单独一个 PR 即可。
3. **A.1+A.2**（时间字段归集 + Duration 文法统一 + TTL 零值消歧）——源码近零成本，可读性立竿见影。
4. **E.1+E.2**（model_defaults + 端点覆盖字段移除）——略大但收益清晰，与 mock.yaml 重写一并做。
5. **mock 零碎清理**（priority 注释反向、`strategy` / `USD` / `model_multipliers` 注释）+ A.3 UserGuide 补注——纯文档/mock 收尾，不在 schema 决策流程。
6. **D.3**（YAML 锚点示例）——零成本，独立微 PR。

## G.2 落地注意事项

- 任何 schema 改动 → `archtest` 必跑（import 边界 / per-file 行预算 / per-function 行预算 / 文档引用完整性）。
- 任何 `Config` 字段位置变更 → `internal/config/{config.go, check.go, watch.go, load.go}` 同步 + `Config.PricingTable` / `Config.PricingAccounting` / `Config.ProxySpecFor` 等访问点更新。
- 对外暴露字段的 Go 名变更 → `vmr report` / `vmr status` / `vmr check` 的 JSON 契约测试同步。
- UserGuide 改动 → 中文版同步；**所有 `*.example.yaml` 与其 `.zh` 兄弟文件同改**。
- 设计文档提及字段处（Part 1 配置参考节、Quota 设计文档配置表）同步更新。
- D.1/D.2 的形态迁移必须附一个"展开结果等价"的差分测试：同一份配置旧形态/新形态各自 BuildSnapshot，比较得到的 (protocol, provider, model, priority) 序列完全一致——形态等价性靠测试钉住，不靠论证。
- 任何字段删除 → `CHANGELOG.md` `[Unreleased]` 段列移除字段 + 一行人工映射说明（`vmr check` 加载时报错信息也指向新写法）。
- C.1 落地时若发现"disabled provider 在 `/status` 不可见"引起运营不便，再考虑"在 `/status` 单独列"——本轮不做。

## G.3 结论

1. **顶层字段归集**：A.1 `timeouts:` / `ttl:` 子块 + Duration 文法统一；A.2 取消 TTL 字段的"永久"语义（拒绝 `forever` 等关键字，`0` = 用默认，需要长期保留写大数如 `90000d`），支持 `d/w/mo/y` 多单位，登记 `max_attempts` / `max_concurrency` 的 `0 = 无上限` 文档要求。**A.2 是 Breaking Change**，CHANGELOG 必标。
2. **Provider 块新增 1 字段**：C.1 `Provider.Disabled` 临时下线开关。
3. **endpoints 二层 map 化**（D.1）：与 `base_url` 同构；fallback 联动 map 化 + 不可达告警（D.2）。
4. **声明与默认值机制升级**（E.1+E.2）：`capabilities` / `max_context_tokens` 提至顶层 `model_defaults` 表（按 (provider, model) 寻址，两字段按维度独立回退）；endpoint 级覆盖字段移除（4 字段净减），但 **override 概念保留在虚拟模型层**（用户场景里"同一真实模型在不同虚拟模型下走不同上限"是正当需求）。
5. **`token_weights` / `model_multipliers` 维持 Limit 内下沉**（D.3）：账号级回提已被 Quota 设计文档 P3 推翻；多条 Limit 共享同一份系数用 YAML 锚点解决（零代码），defaults 继承仅在真实痛点出现时再上。
6. **明确不做的边界**（F）：pricing.overrides / quota.limits / providers 的 map 化、`api_keys` map 顺序有序化与二象性统一（维持现状）、`pricing` 改名 `billing`、账号级 token_weights 回提——各自的理由已在 F 节登记在案。
7. **mock.yaml 文本层零碎观察**：priority 注释反向、strategy/USD 冗余、注释过时——都是示例文件层面，不进入 schema 决策流程；仅作"顺手做"的修整项（V1 曾单列 B 节讨论，本轮取消：mock 是示例，准确与否与 schema 重构正交）。
8. **执行顺序**（G.1）：D.1+D.2 → C.1 → A.1+A.2 → E.1+E.2 → B 收尾 → D.3 零成本微 PR。
9. **本文件不构成 schema 变更的批准**——任何落地都需经设计评审，并在 `KNOWN_ISSUES.md` 登记。
