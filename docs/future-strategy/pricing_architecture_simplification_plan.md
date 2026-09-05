# VMR 定价体系终极极简重构方案与第一性原理审查 (Pricing Architecture Radical Simplification Plan)

<!-- Date: 2026-09-06 | Status: 设计终稿。本版相比上一版新增决策 6（删除 metric: cost 配额限额），并把原"Provider 级币种对审计日志/报表解析链的传导影响"一节（旧 §6）改写为该决策的连锁消解：币种退出运行态，审计日志与报表解析链不再需要任何改动。 -->

## 0. 方案核心决策 (Core Architectural Decisions)

本方案基于第一性原理与马斯克五步工作法（"质疑每一个要求，删除所有不必要的部分"），对 VMR 当前繁琐、重叠且存在多处心智混乱的定价配置体系进行**彻底的根治性重构**。

### 六大终极决策：
1. **彻底切除顶层 `pricing:` 块**：
   * 删掉顶层 `pricing.rates`、`pricing.aliases`、`pricing.standard`、`pricing.currency`、`pricing.supplement`；
   * 彻底杜绝在顶层定义模型价格的任何入口，消灭多处配置重叠的根本诱因。
2. **彻底淘汰外部 `pricing.yaml` 补充表**：
   * 物理删除仓库中的 `pricing.mock.yaml`、`pricing.example.yaml` 等 sidecar 资产，用户以后不需要、也不可能再外挂额外的价目表文件。
3. **全局客观常数 `exchange_rate`（汇率）独立提至顶层**：
   * 汇率是全系统共享的客观物理事实（`1 USD = X 货币`），直接作为顶层全局字段配置；
   * 它的两个消费者都**不在运行态**：加载期把一切非 USD 书写的价格一次性归一为 USD（见决策 4）；`vmr report` 的 `-currency` 展示折算。
   * 严格校验（Fail-Fast）：用户未显式声明某货币的汇率时，先尝试内置默认汇率表兜底（见 2.3 节）；内置表也没有的冷门货币，才在加载期直接报错拒绝启动。任何一步都不允许静默猜测或按 0/1.0 处理。
4. **模型定价、别名映射与书写币种全量收敛至 `providers[].pricing`**：
   * `currency`: **解析期标注**——声明该 provider 的 `rates` 行用什么币种书写（默认 USD，如"这份合同价按人民币开票"），加载期按顶层 `exchange_rate` 一次性折算成 USD 存入内存。它**不是运行态量**：不进 Endpoint、不进配额、不进审计日志、不进报表标签。
   * `aliases`: 本账号的私有别名/方言映射（全面替代原 `map`，命名与内置表规范统一）；
   * `rates`: 本账号的具体模型费率或相对折扣（全面替代原 `overrides`，命名与内置表规范统一）。
5. **全系统定价收敛为绝对清晰的"纯粹二层模型"**：
   * **Layer 1（定制契约）**：Provider 内部的局部私有定义（`providers[].pricing`）；
   * **Layer 2（官方底座）**：VMR 内置标准库（Generated + Curated），随二进制发布，开箱即用不可变；
   * 绝无任何中间层、扩展层或外部文件层！
6. **彻底删除 `metric: cost` 配额限额，Limit 仅支持 `requests` 与 `tokens`**：
   * Limit 是控制面机制（在不同模型/Provider 之间均衡调用量），不是计费面机制（精确记账）。token 数是调用量最直接、最稳定的度量，cost 只是 tokens × 价格的换算——为一个换算让控制面的触发时机随价目表和汇率漂移，是把两件事焊死在一起。
   * "贵模型多占额度"的表达力由 tokens Limit 已有的 `model_multipliers`/`token_weights` 承担，无表达力损失。
   * 这一删除的连锁收益是本方案最大的一笔：运行态从此**零价格、零币种**（详见第 6 节）——热路径不再折叠费率、配额计数器不再有 $ 分量、加载期完整性门禁不再存在、审计日志不需要新增任何字段。

---

## 1. 现状审查与源码实证 (Source Code Fact-Checking)

为了杜绝"文档滞后于代码"的通病，以下所有分析均直接基于当前生产代码核实：

### 1.1 痛点一：四个入口可以配价格，概念极度混乱
在现有代码（`internal/config/pricing.go`）中，用户若想为一个模型定义价格，系统提供了多达 4 个互相重叠的入口：
1. 内置标准表（`go:embed` 的 `standard_price_*.yaml`）
2. 外部补充表文件（`pricing.supplement: ./pricing.yaml`）
3. 全局内联价目表（`pricing.rates` 与 `pricing.aliases`）
4. Provider 局部定义（`providers[].pricing.map` 与 `overrides`）

用户在配置时会产生巨大的心智负担：*"这个价格我该写在顶层 rates 还是写在 provider overrides？我该写在顶层 aliases 还是 map？我要不要新建一个 pricing.yaml？"* 这违背了整洁架构"单一职责与单一事实来源"的基本原则。

### 1.2 痛点二：命名概念分裂（Table 层 vs. Provider 层）
核查 `internal/pricing/pricing.go` 与 `internal/config/pricing.go`：
* **Table 层（内置表/补充表）**：结构体 `fileTable` 中的字段是 **`aliases`** 和 **`rates`**。
* **Provider 局部层**：结构体 `ProviderPricingConfig` 中的字段突然变成了 **`map`** 和 **`overrides`**。

在同一个系统中，表达同一类概念（名称映射与价格声明）却使用了两套名词，造成了无谓的认知分裂。

### 1.3 痛点三：顶层强行统一度量衡，严重违背跨境多账号现实
核查 `internal/config/pricing.go` 的 `buildPricingContext` 与校验逻辑：
* 当前系统强迫所有账号在顶层统一配置一个 `pricing.currency`（cost 账号未配置直接加载报错）；
* 导致用户的 Anthropic 官方账号（按美元扣费）必须被迫换算成顶层货币来思考；引入欧洲供应商（EUR）或国内供应商（CNY）时，顶层的单一货币设定彻底沦为累赘。

### 1.4 痛点四：底层算账代码对"未知分量"的真实处理
核查 `internal/core/core.go` 的 `Rate.Cost` 核心计算函数：
```go
priced := func(tokens int64, perMillion *float64) float64 {
    if perMillion == nil {
        return 0 // ⚠️ 底层对于 nil 的未知分量，算账时直接当 0 计算！
    }
    return float64(tokens) / 1_000_000 * *perMillion
}
```
代码证明：所谓"未知的 rate 分量不能当 0，所以必须在 rates 留空"，仅仅是 `metric: cost` 在加载期的一道校验门禁（`Complete()` 函数）。用户若在 Provider 局部显式填 `0.0`，在底层计算上完全等价，没有任何必须保留顶层 rates 的技术必要性。

### 1.5 痛点五：内置 Curated 表已覆盖全网主流别名
核查 `internal/pricing/standard_price_curated.yaml`：
在分类 `(4) Generated-table first-party pins` 中，维护者已经把数量可观的主流公有模型裸名**全部写死了官方权威别名**（具体条目数量随表内容定期刷新而变化，不作为设计依据硬编码）。公有模型根本无需用户自己配别名；私有方言写在 Provider 局部更安全，顶层 `pricing.aliases` 绝大多数场景都是冗余的。

### 1.6 痛点六：`metric: cost` 把控制面和计费面焊死在一起
这是决策 6 的直接动因，逐条来自源码：
* **Limit 的语义随价格漂移**。`internal/quota/quota.go` 的 `Counters` 注释亲口承认了这一点：`Cost` 是唯一一个"必须在扣费那一刻冻结 $ 金额"的分量（"re-deriving it later from raw token counts at whatever price happens to be configured then would silently rewrite history on every pricing edit"）。换一句话说：**代码自己证明了 cost Limit 的触发时机取决于扣费那一刻的价目表和汇率**——一次价目表刷新或汇率修改，就会让同一个账号"什么时候撞限额"静默改变。一个控制面机制不应该有这样的性质。
* **加载期门禁把计费完整性强加给所有人**。`resolvePricing` 要求每个 `metric: cost` provider 要服务的**每一个**模型都解析出四分量齐全的费率（`pricing.Complete` 硬门禁），且强制配置 `pricing.currency`。用户只想给账号设个预算，却被迫先当一回完整价目表的维护者。
* **整条 cost 专门管线只服务这一个 metric**：`Endpoint.PricingRate`（BuildSnapshot 折叠）、`router.ChargeResponse` 的 cost 分支、`quota.ChargeCost`、`Counters.Cost`、`vmr-quota.json` 的 `estimated_cost`、`/status` 的 cost 读数、配额差分测试的 cost 用例——每一处都是为这一个 metric 修的专用桥。
* 而这一切换来的"精确"，报表侧本来就有：`vmr report`/`vmr analyze` 的 $ 估计是**离线**从 token 数、按当时最新价目表重算的（`internal/report/cost.go`），与运行态计费完全分离。运行态 cost 计费的唯一独有产出就是那个会随价格漂移的限额触发时机——恰恰是它最不该有的产出。

---

## 2. 第一性原理深度推导 (First Principles Analysis)

### 2.1 为什么汇率提至顶层，而书写币种下沉到 Provider？
* **汇率是客观常数（Global Physical Constant）**：
  `1 USD = 7.1 CNY`、`1 USD = 0.92 EUR` 是现实金融市场的客观数据，全系统只需要声明一次。
* **书写币种是账户的书写习惯（Account Notation）**：
  Anthropic 的合同价按美元开票，Minimax 的合同价按人民币开票。把 `currency` 挂在 provider 的 `pricing` 下，用户可以照抄发票上的数字，不用手工换算——但仅此而已。归一成 USD 发生在**加载期一次**，此后系统内不存在第二种标定。它不是运行态量，不需要被任何下游"解释"。

### 2.2 为什么顶层价目表（`rates`/`aliases`）必须彻底切除？
1. **公有模型全由内置库覆盖**：内置库包含 LiteLLM 抓取的完整列表及手工校准的 Curated 表，开箱即用；
2. **私有/特价模型直接在 Provider 定义折后净价**：如果用户接入了一个冷门或自建模型，用户需要手工输入价格。真实业务中，用户会直接填入最终有效价格，绝不会有人在顶层定义一个原价再跑到局部去打折；
3. **消除选择困难**：彻底杜绝"到底写在顶层还是局部"的困扰。整个系统有且仅有一个入口定制价格——那就是具体的 `provider.pricing`！

### 2.3 汇率兜底：能不能内置一份合理默认值，减少"用户必须手填 exchange_rate"这一步摩擦？
可以，而且这个思路在这个代码库里有现成先例，不是新架构：内置标准价目表（`standard_price_curated.yaml`/`standard_price_generated.yaml`）走的就是"内置合理默认 + 用户可覆盖 + `vmr check` 表龄超期提示刷新"这一套（`cmd/vmr/check_pricing.go` 的 `pricingStaleAfter` 常量）。汇率表体量比价目表小得多（几十种常见货币 vs. 数百个模型），直接复用同一套机制即可：

* **查找顺序**：用户 `exchange_rate[ccy]` → 内置默认汇率表 `[ccy]` → 报错。用户显式声明的条目永远覆盖内置默认，语义上和 `pricing.Merge` 已经在用的"覆盖层胜出"完全一致。
* **不能因为有了兜底就放弃 fail-fast**：内置表没有覆盖到的冷门货币，依旧在加载期硬错——这条底线的意义是"没查到就说没查到"，不是"查不到就编一个"。
* **如实标注局限**：汇率天天在变，不像模型报价那样几周不动，表龄提示对它意义有限；应当把它定位为"给你一个开箱可用的估算，不是记账精度的汇率"。`vmr check` 必须清楚标注某条汇率来自"用户配置"还是"内置默认（生成于某日）"，不能让用户分不清自己有没有配置生效。
* **它只影响估计精度，不影响任何控制行为**：汇率在本架构下只触达加载期归一和报表展示折算，改错一个汇率最多让 $ 估计偏几个点，永远不会改变路由或限额行为——这是决策 6 带来的误差隔离。

### 2.4 为什么 `metric: cost` 必须整个删掉，而不是"保留但降级"？
* **质疑要求本身**：Limit 要解决的问题是什么？是"别让某一个账号/模型把流量吃光"——一个跨模型、跨 Provider 的**量**的均衡问题。这个"量"的最直接 substrate 是什么？是 token 和请求次数。cost 从来不是 substrate，它是 substrate（tokens）乘以一个随时间变化的系数（价格）之后的投影。控制面直接作用于 substrate，而不是投影。
* **等价性**：cost Limit 表达的一切，tokens Limit 都能表达——把预算除以价格即可，且这个换算由用户做一次、写死在配置里，语义从此稳定。贵模型多占额度的诉求由 `model_multipliers`（按模型倍率）和 `token_weights`（四分量权重）承担，两者都是 tokens Limit 的既有字段。
* **删优于修的判定标准**：如果保留 cost Limit，哪怕把它"降级"为非精准模式，价格表仍然是 Limit 语义的隐性输入，第 1.6 节列出的那条专门管线就一处都删不掉。**凡是需要靠"冻结扣费时刻的价格"来维持语义稳定的机制，都是把计费面漏进了控制面**——代码注释里那句 "must be frozen at charge time" 就是病理报告。
* **误差隔离**：删除后，价格与汇率的一切不精确只触达报表的 $ **估计**列（本来就标注为估计、可缺省、可追溯），永远触达不到路由与限额行为。控制面（token/request 基准，绝对精确）与计费面（价格估计，允许漂移）彻底各归其位。

---

## 3. 终极目标架构与配置全景对比 (Target Architecture: Before vs. After)

### 3.1 重构前配置（现状：冗余、重叠、割裂、控制面混入计费面）
```yaml
# ❌ 旧架构：顶层既有货币，又有 rates/aliases，还引用外部文件；Limit 依赖价格体系
pricing:
  currency: CNY
  exchange_rate: {USD: 1.0, CNY: 7.1}
  supplement: ./pricing.mock.yaml  # 外部 sidecar 文件
  rates:                           # 与 provider.overrides 严重重叠
    - key: custom/my-model
      in_fresh: 2.0, out: 8.0
  aliases:                         # 与 provider.map 严重重叠
    my-alias: anthropic/claude-3-7-sonnet

providers:
  - name: anthropic
    # 一个 $500/月 的预算，被迫以顶层货币表述，且语义随价目表漂移
    quota:
      limits:
        - {metric: cost, every: 1mo, amount: 3550}
    pricing:
      map:                         # 命名与顶层 aliases 不对称
        claude-fast: claude-3-7-sonnet
      overrides:                   # 命名与顶层 rates 不对称
        - model: "*"
          discount: 0.9
```

---

### 3.2 重构后配置（终极形态：极简、对称、各司其职、控制面零价格）
```yaml
listen: 0.0.0.0:8800
api_keys:
  - vmr-devkey-prod-001

# ✅ 顶层仅保留客观常数：汇率表（无 pricing 块，无外部文件）
# 消费者只有两个，都不在运行态：加载期把非 USD 书写的价格归一为 USD；
# vmr report 的 -currency 展示折算。未声明的货币先查内置默认表兜底（见 2.3 节）。
exchange_rate:
  CNY: 7.1
  EUR: 0.92

providers:
  # 场景 A：按美元开票的国际厂商
  - name: anthropic
    base_url: {anthropic-messages: https://api.anthropic.com}
    api_key: ${ANTHROPIC_API_KEY}
    quota:
      limits:
        # 控制面只认 token 与请求数，与任何价格表无关。
        # 要表达 "$500/月" 的预算，自行换算一次写成 token 数即可；
        # 贵模型多占额度用 model_multipliers/token_weights 表达。
        - {metric: tokens, every: 1mo, amount: 200000000}
    pricing:
      currency: USD                # 解析期标注：rates 行的书写币种（默认 USD，可省）
      aliases:                     # 统一命名为 aliases (原 map)
        claude-fast: anthropic/claude-3-7-sonnet-20250219
      rates:                       # 统一命名为 rates (原 overrides)
        - model: "*"
          discount: 0.9            # 官方内置标准价打 9 折

  # 场景 B：按人民币开票的国内厂商
  - name: minimax
    base_url: {openai-completions: https://api.minimaxi.com/v1}
    api_key: ${MINIMAX_API_KEY}
    quota:
      limits:
        - {metric: tokens, every: 1mo, amount: 1000000000}
    pricing:
      currency: CNY                # 解析期标注：下面这些价格照抄人民币发票，
                                   # 加载期按顶层 exchange_rate 一次性归一为 USD
      rates:
        - model: MiniMax-M3
          in_fresh: 1.5
          cache_read: 0.3
          cache_write: 1.5
          out: 6.0                 # 显式价格，未知分量不可留空（四分量全给或全不给）
```

---

## 4. 两层极简查找流程 (Lookup Sequence)

全新架构下，系统解析任意一个 `(provider, request_model)` 的最终费率时，遵循确定性的纯粹二层流程。**这整条解析只服务于一件事：`vmr report`/`vmr analyze` 的 $ 估计列**——它不在请求路径上，不参与路由，不参与限额，解析不出也不影响任何运行行为：

```
                 输入: Provider 实例, 请求模型名 (Model)
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────┐
│ 【第一层：Provider 局部私有契约】                                 │
│  1. 别名重映射：                                                │
│     TargetModel = provider.pricing.aliases[Model] (若未命中则不变)│
│  2. 规则匹配：                                                  │
│     提取 provider.pricing.rates 中匹配 TargetModel 的规则        │
│     （具体模型精准匹配优先，其次 "*" 通配符；规则已在加载期      │
│      按该 provider 的 currency 归一为 USD）                      │
└───────────────────────────────┬──────────────────────────────────┘
                                │
         ┌──────────────────────┴──────────────────────┐
         ▼ 命中显式 rates                              ▼ 仅命中 discount 或未命中 rates
  【直接采用局部费率】                                   │
  查找终结！（无需查询内置库）                           │ 必须向官方底座借用基准列表价
                                                        ▼
┌──────────────────────────────────────────────────────────────────┐
│ 【第二层：VMR 内置标准库 (Built-in Table: Generated + Curated)】 │
│   Step 1: <provider_name>/<TargetModel> (当 Provider 恰为 Vendor)│
│   Step 2: 裸名 TargetModel 直查                                  │
│   Step 3: 内置 Curated 权威别名直查（具体条目数随表内容刷新变化）│
│   Step 4: 后缀扫描 (*/TargetModel) 与第一方厂商优先级决胜        │
│   Step 5: Basename(TargetModel) 剥离 org 路径前缀递归重试        │
└───────────────────────────────┬──────────────────────────────────┘
                                │
               ┌────────────────┴────────────────┐
               ▼ 命中                            ▼ 未命中
      【折扣叠合，全 USD，零换算】          【该行报表无 $ 估计】
      若有局部 discount:                    其余功能不受任何影响：
        EffectiveRate = Base * discount     路由、限额、日志、导出全部照常，
      若无:                                 仅 $ 列对该行留空。
        EffectiveRate = Base               （原方案此处是"加载期报错拒绝
                                            启动"——那是 metric: cost 门禁，
                                            随决策 6 一并删除。）
```

与旧流程的两处关键差异：
1. **多币种折算从查找流程中消失**：所有价格（内置表天然 USD；provider 的 rates 行加载期归一 USD）在进入查找前就已同标定，运行态解析零换算。
2. **未命中不再是加载期错误**：原"提示：模型未定价，无法启动，拒绝未定价上路"的分支删除。那个分支存在的唯一理由是 cost Limit 的完整性门禁；门禁随决策 6 消失后，"未定价"回归它本来的语义——报表少一个估计值，仅此而已。（provider `rates` 行本身"四分量全给或全不给"的**形状**校验保留——它的理由是消除"没写 = 免费？"的歧义，与门禁无关。）

---

## 5. 新架构下的运行态与报表态

### 5.1 运行态（路由与限额）：零价格、零币种
* **配额只计 `tokens` 与 `requests`**：`Counters` 保留 Fresh/CacheRead/CacheWrite/Out/Requests 五个原始分量，`Cost` 分量与 `ChargeCost`/`EstimatedCost` 整条链删除。`Counters` 注释里"Cost 是唯一必须在扣费时刻冻结 $ 金额的例外"这一特例类整体消失——不再有任何计数器的语义依赖扣费时刻的价格。
* **`core.Endpoint` 不再有 `PricingRate`**：BuildSnapshot 不再折叠任何费率，`FoldSpec` 删除，`Config.ResolvedPricing` 删除。KNOWN_ISSUES §1.0 的红线（价目表不进实时路由热路径）从"靠纪律守住的边界"变成"结构上已不存在的东西"——热路径上连价格数据都没有了。
* **各账号按 token/request 均衡，天然同标定**：不同 provider 的 Limit 分数（UsedFrac）各自内部自洽，评分与抢跑排序不引入任何跨币种换算，也不依赖"这个 provider 叫什么、按什么币种开票"。
* **`quota.limits[].amount` 语义纯净**：就是一个 token 数或请求次数，无币种、无价格、无时间漂移。

### 5.2 报表态（`vmr analyze` / `vmr report`）：USD 归一 + 单一展示折算
* 所有费率解析在**内存中永远是 USD 标定**（内置表原生 USD；provider rates 行加载期归一）。报表侧 `pricing.Resolver` 的解析语义、缓存键、`RateFor`/`RateForEndpoint` API **原样保留**。
* 展示币种折算维持现有的单一 `displayFactor` 机制（`WithDisplayFactor`）：解析在 USD、展示按 `-currency`/report.yaml 一次性线性缩放。`-currency` 的默认链精确为：**report.yaml 的 `currency` → USD**（原"config 的 pricing.currency"一环随顶层 pricing 块删除而消失；报表聚合中不存在"每条记录各自的币种"，也就不存在"第一个检测到的币种"这类含糊默认）。
* 汇率来源与 2.3 节一致：用户 `exchange_rate` → 内置默认表兜底；`vmr check` 标注来源。
* 报表的 $ 列保持其一贯的诚实性：解析不出 → 去掉 $ 列或标记不完整（`CostRateIncomplete` 机制原样保留）；价格表刷新后重算出不同数字 → 这是"估计"的固有属性，报表头部的时间戳与免责声明负责说明这一点。**报表从不记账，所以允许漂移；限额从不看价格，所以不会漂移。**

---

## 6. 币种基准问题与它的消解（为什么审计日志一个字段都不用加）

### 6.1 问题的原始形态（本节分析仍然成立，值得留档）
前一度方案的思路是：`providers[].pricing.currency` 成为持久的账户属性后，审计日志与报表重新解析历史记录时，"这条记录该按哪个币种解释"不能再无条件地问"当前 config.yaml"——provider 会改名、下线、被 `api_keys` 拆分。当年的事实链（逐条经源码核实）：

* 路由半在配置加载期把每个 provider+model 的价格解析成 `core.PricingSpec{Base, Overrides, Currency}`，但 `BuildSnapshot` 调 `pricing.FoldSpec` 把这条链**折叠**成纯数字的 `*core.Rate` 挂在 `Endpoint.PricingRate` 上——从那一刻起"这几个数字是什么币种"就只活在当次生成快照用的 config.yaml 里；
* 审计日志只记 `Attempt.Provider`/`Attempt.Model`（"名字"），从名字反查币种靠事后回头问 config，而 config 会变；
* 报表侧 `pricing.Resolver` 无条件拿 provider 名字查"当前 config 生成的" `perProvider` map。
* 历史教训（`internal/pricing/resolver.go` 的 `ProviderPolicy` 注释）：早期曾把"USD→记账币种"的折算因子按 provider 缓存，一个改名/删除的 provider 查不到缓存条目时，零值兜底成了"不换算"，同一张报表里一行按 USD 一行按 CNY，看起来都很正常。病根是"把本该只有一份的值，冗余复制进一个查不到就静默给错默认值的 map 里"。

当年据此开出的药方是一整套机器：`Attempt.PricingCurrency` 审计字段（per-attempt 粒度、`forwardSuccess` 写入）、`Endpoint.PricingCurrency`、`Resolver` 增加"优先采信记录标签"分支、缓存键携带币种基准、per-record 展示折算（单一 `displayFactor` 在多币种下数学上不成立）、新增差分测试钉币种基准。

### 6.2 为什么这整套机器不再需要：基准变成了结构不变式
`metric: cost` 删除（决策 6）+ 解析期 USD 归一（决策 3/4）之后：

* 运行态不再消费价格——`Attempt` 没有币种可钉，也没有必要钉；
* 报表侧每一次解析（无论哪个 provider、哪条历史记录、哪一版 config、provider 改没改名、下没下线）产出的数字**标定恒为 USD**。"这条记录该按哪个币种解释"重新变成当年那句"反正全系统只有一个答案，问谁都一样"——而且这一次不是运气（历史上是因为从未引入第二币种才没暴露），是结构保证：归一发生在加载期，内存里根本不存在第二种标定的数字。
* 对照当年的药方逐项检查，每一项的存在前提都是"运行态/记录级存在多种币种"：
  | 当年药方 | 前提 | 现状 |
  | --- | --- | --- |
  | `Attempt.PricingCurrency`（per-attempt 钉基准） | 记录级币种可变 | 运行态零币种，无可钉之物 |
  | `Endpoint.PricingCurrency` | Endpoint 携带价格 | `PricingRate` 本身已删除 |
  | `Resolver` 增加"优先采信记录标签"分支 | 解析需按记录选基准 | 基准恒 USD，无分支可加 |
  | 缓存键携带币种基准 | 同一 (provider, model) 可能多基准解析 | 恒单基准，`provider\x00model` 键不歧义 |
  | per-record 展示折算（`displayFactor` 退役） | 同一聚合内多记账币种混加 | 全 USD 解析，单一 `displayFactor` 数学上成立 |
  | 新增差分测试钉币种基准 | 基准可能两侧不一致 | 基准结构性一致，无可漂移之物 |

  **删优于修**：修，是给"运行态存在多币种"这个状态打六块补丁；删，是让这个状态不存在。
* 查无 provider 的降级路径也回归既有的 best-effort 契约：audit log 里的 provider 在当前 config 已不存在时，`Resolver` 走零值 policy 用内置标准表解析（USD 原值），解析不出则该行无 $ 估计——这与今天无 supplement、无 account override 时的行为完全一致，无币种歧义可引入。

### 6.3 从这次消解中沉淀下来的两条纪律
1. **任何解析路径查不到时，不得静默选择一个"看似合理"的基准。** 这是 per-provider factor 历史 bug 的真正教训，它与"per-provider 有没有不同的值"无关——只要存在"查不到 → 拿一个默认值顶上"的路径，报表就会悄悄错。在本架构里这条纪律的当代形态是：解析不出就没有 $（留空），绝不用别家的数顶着。
2. **"基准必须不变"应当是结构性质，而不是靠冻结、打标、迁移来维持的性质。** 当年 cost 计费靠"扣费时刻冻结 $ 金额"维持语义稳定，本方案把它消灭在结构里：控制面根本不接触价格。今后任何想把价格重新引入控制面的提议，都应当先回答："这个机制的语义，凭什么不应该随价目表漂移？"

---

## 7. 源码重构实施与迁移计划 (Action Plan)

改动分两个 workstream：**A. 定价配置收敛**（决策 1–5）、**B. metric: cost 删除**（决策 6）。两者交汇于 `internal/config`，但除交汇点外彼此独立，可分两步落地、分步验证。

### 7.1 需修改的源码文件清单

| 文件路径 | Workstream | 具体修改内容 |
| :--- | :--- | :--- |
| `internal/config/config.go` | A | 顶层新增 `ExchangeRate map[string]float64`；移除 `Pricing *PricingConfig` 字段 |
| `internal/config/pricing.go` | A+B | 删除 `PricingConfig`（保留轻量壳用于 legacy `pricing:` 块的 deprecation 文案，见 7.2）；`ProviderPricingConfig` 重塑为 `Currency`（解析期标注）/`Aliases`/`Rates` + legacy `map`/`overrides` 兼容；`resolvePricing` 删除 `hasCostLimit`/`costProviders`/`ResolvedPricing`/`Complete()` 门禁/"currency is not set" 错误；provider `rates` 行按其 `currency` 经 `pricing.FactorBetween` 在 validate 期归一 USD |
| `internal/config/quota.go` | B | `metric: cost` 解析分支删除，改为显式报错并指引迁移（值错误 `KnownFields` 抓不到，必须显式校验，见 7.2）；`hasCostLimit` 删除；cost 与 `model_multipliers` 的互斥校验删除 |
| `internal/core/core.go` | A+B | `QuotaMetric` 删除 `MetricCost`；`Endpoint.PricingRate` 删除；`PricingSpec` 保留（override 链仍是报表解析的载体）但删除 `Currency` 字段；`Rate` 与 `Rate.Cost` **保留**（报表侧成本公式的 SSOT 地位不变） |
| `internal/pricing/resolve.go` | B | `ResolveOptions` 移除 `ExchangeRateToTarget`/`Currency`；`Resolve` 的换算分支删除；`FoldSpec` 删除（无挂点）；`EffectiveRate`/`resolveChain` 保留；spec 级 `Complete()` 删除（门禁消失），`Rate.Complete()` 保留（报表 incomplete 标记仍用） |
| `internal/pricing/resolver.go` | B | `NewResolver` 签名瘦身（少 `tableFactor`/`currency` 两参）；**缓存键、`RateFor`/`RateForEndpoint` API、`WithDisplayFactor` 机制原样不动**——这是本方案"删优于修"的关键验收点 |
| `internal/pricing/pricing.go` | A | 移除外部补充表解析代码（`ParseTableWithRates` 的 supplement 用途），只保留内置表加载；`RateRow.Currency` 保留（内置表行级归一仍用） |
| `internal/pricing/`（新增 `standard_exchange_rate.yaml`） | A | 常见货币对 USD 汇率的内置默认表；用户 `exchange_rate` 条目始终优先覆盖（见 2.3 节） |
| `internal/router/quota.go` | B | `ChargeResponse` 的 `MetricCost` 分支删除；`needsTokenCharge` 语义收窄为"仅 tokens"；`tokenCharge`/`ChargeResponse` 的 tokens/requests 路径不动 |
| `internal/router/snapshot.go` | B | `buildEndpoints` 不再折叠 `PricingRate` |
| `internal/quota/` | B | `Counters.Cost`、`ChargeCost`、`EstimatedCost`、`weight.go` 的 cost 分支删除；`store.go` 的 `Bucket.EstimatedCost` 删除（旧 `vmr-quota.json` 里的 `cost`/`estimated_cost` 键被 JSON 解码自然忽略，读侧无迁移） |
| `internal/replay/replay.go` | B | 手工构造的 `Endpoint` 不再挂 `PricingRate`；`chargeReplay` 的 cost 路径删除 |
| `internal/audit/audit.go` | — | **不动。** 明确不新增 `PricingCurrency`（第 6 节） |
| `internal/report/providerquota.go`、`cmd/vmr/cmd_report_quota.go` | B | §2.5 额度对照的 metric:cost 行修剪（该行随 config 不再出现，代码路径变死代码，按死代码清理）；`internal/report/cost.go`、`internal/story/cost.go`、`internal/i18n` **不动** |
| `cmd/vmr/check_pricing.go`、`cmd/vmr/cmd_check.go` | A | `vmr check` 输出更新：打印顶层 `exchange_rate`（标注来源：用户配置 / 内置默认 + 生成日期）、按 Provider 打印各自的 `currency`（解析期标注）、`aliases` 与 `rates`；不再打印 cost 完整性门禁相关内容 |
| `cmd/vmr/quota_parity_test.go` | B | 删除 cost-metric 差分用例（`TestQuotaParity_CostMetric_ReportMatchesRouter` 等）；requests/tokens 用例保留 |
| `cmd/vmr/cost_basis_parity_test.go` | — | **不动**（report/story 两侧成本基准差分，与本次改动正交） |
| `config.mock.yaml` / `*.example.yaml` | A+B | 移除顶层 `pricing:` 块、`metric: cost`；更新 `exchange_rate` 与 Provider 局部定价示例 |
| 外部文件删除 | A | 删除 `pricing.mock.yaml`、`pricing.example.yaml` 及相关文档引用（含 `internal/pricing/example_test.go` 里对 `pricing.example.yaml` 的直接解析测试） |
| `docs/KNOWN_ISSUES.md`、`CHANGELOG.md` | A+B | 落地时按仓库规约注册：`metric: cost` 删除是 breaking change，CHANGELOG `[Unreleased]` 写明迁移路径；控制面/计费面分离的裁决若以 deliberate non-fix 形式存在（如"不再提供 cost 预算"），注册进 KNOWN_ISSUES |

### 7.2 平滑迁移与向后兼容实现（配置层，代码级）

在 `internal/config/pricing.go` 中，重塑 `ProviderPricingConfig`，实现旧键（`map`/`overrides`）到新键（`aliases`/`rates`）的无缝兼容：

```go
type ProviderPricingConfig struct {
    // Currency is a load-time annotation: the currency the rates rows below
    // are written in (default USD). Converted to USD once at load time via
    // the top-level exchange_rate table. It is NOT a runtime quantity —
    // it never reaches core.Endpoint, the audit log, or report labels.
    Currency string `yaml:"currency"`

    // Aliases maps local model names to canonical keys in the built-in
    // standard table. (Unified replacement for `map`.)
    Aliases map[string]string `yaml:"aliases"`

    // Rates defines explicit rates or discount modifiers for this provider.
    // (Unified replacement for `overrides`.)
    Rates []PricingOverrideConfig `yaml:"rates"`

    // --- 向后兼容过渡字段 (Deprecated) ---
    MapLegacy       map[string]string       `yaml:"map,omitempty"`
    OverridesLegacy []PricingOverrideConfig `yaml:"overrides,omitempty"`
}

// normalize 在反序列化后执行，自动完成兼容转换与排他性校验
func (p *ProviderPricingConfig) normalize(providerName string) error {
    if len(p.MapLegacy) > 0 {
        if len(p.Aliases) > 0 {
            return fmt.Errorf("provider %q: cannot configure both 'pricing.map' and 'pricing.aliases' — please use 'pricing.aliases'", providerName)
        }
        p.Aliases = p.MapLegacy
    }
    if len(p.OverridesLegacy) > 0 {
        if len(p.Rates) > 0 {
            return fmt.Errorf("provider %q: cannot configure both 'pricing.overrides' and 'pricing.rates' — please use 'pricing.rates'", providerName)
        }
        p.Rates = p.OverridesLegacy
    }
    return nil
}
```

顶层旧版写法的兼容：
* 仍写有顶层 `pricing:` 的配置：若包含 `exchange_rate`，自动迁移至顶层 `exchange_rate` 并提示；若包含 `rates`、`aliases`、`supplement`、`standard`，在 `vmr check` 中输出明确的 Deprecation 错误指引用户移入具体的 Provider 内部。
* 注意：`config.yaml` 走严格 `KnownFields` 解码，一旦 `PricingConfig` 结构体被整体删除，旧配置会直接得到 YAML 解码器自己的"未知字段"报错，而不是上面这条定制提示——想要保留友好的 Deprecation 文案，`PricingConfig` 至少要作为一个仅用于探测遗留写法的轻量壳保留到迁移窗口结束。这是实现兼容承诺时需要预先规划的一个具体缺口，不是事后才发现的意外。
* **`metric: cost` 是值不是键，`KnownFields` 抓不到**：`quota.go` 的 metric 解析必须显式拒绝并给出迁移指引，例如：`metric: cost is no longer supported — express the same budget as a tokens limit (convert once: budget ÷ price), optionally with model_multipliers/token_weights to weight expensive models; $ cost estimates remain available in vmr report`。

### 7.3 审计日志与运行态持久化的兼容（本方案为零改动）
* **审计日志没有任何 schema 变更**——不新增字段、不改格式、无迁移（第 6 节）。
* `vmr-quota.json`：写侧停止输出 `cost`/`estimated_cost`，旧文件中的这两个键被 JSON 解码自然忽略。计数历史（tokens/requests 分量）完整保留，无需迁移脚本或版本号机制。

---

## 8. 最终价值评估 (Value Assessment)

| 评估维度 | 重构前 (Before) | 重构后 (After) | 改善收益 |
| :--- | :--- | :--- | :--- |
| **定价配置入口** | **4 处**（内置表、外部文件、顶层 rates/aliases、Provider 局部） | **1 处**（仅 Provider 局部） | 心智负担彻底归零，杜绝多处配置重叠 |
| **外部依赖文件** | 必须理解并管理 `pricing.yaml` 补充表 | **完全不需要额外文件**，单配置即全貌 | 部署更简单，容器化/配置分发更轻量 |
| **命名一致性** | Table 叫 `aliases`/`rates`，Provider 叫 `map`/`overrides` | 全系统统一规范为 **`aliases`** 与 **`rates`** | 概念完全对称，阅读直观度大幅提升 |
| **多币种** | 顶层强行统一单一货币，且币种是运行态量（cost 计费依赖它，历史 bug 由此而生） | 顶层全局汇率（含内置默认表兜底）；**运行态零币种**，币种只是加载期书写标注 | 币种问题的整类复杂性（日志钉标、报表基准、缓存歧义）被结构性消除，而非被修补 |
| **配额指标** | requests/tokens/cost 三种，cost 的语义随价目表与汇率漂移（靠扣费时刻冻结 $ 金额维持） | **requests/tokens 两种**，语义绝对稳定；控制面与计费面彻底分离 | 限额触发时机不再随价格表刷新漂移；加载期完整性门禁消失，配价格不再被强制完整 |
| **系统架构层级** | 复杂的四层混叠继承 + 热路径上的折叠费率 | **纯粹的二层模型**（局部契约 + 官方底座），热路径零价格数据 | KNOWN_ISSUES §1.0 红线从纪律变成结构事实；删除整条 cost 专门管线与数百行合并/回退/门禁代码 |
| **一次性工程成本** | — | 定价配置收敛 + cost metric 删除（两条独立 workstream，可分步落地）；审计日志与报表解析链**零改动** | 相比"保留 cost Limit + 修补币种基准"的旧路线（6 块补丁 + 新审计字段 + Resolver API 变更 + 新差分测试），本路线以更小的改动面换来更大的删除量 |
