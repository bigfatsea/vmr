# VMR 定价体系终极极简重构方案与第一性原理审查 (Pricing Architecture Radical Simplification Plan)

<!-- Date: 2026-09-06 | Status: 设计终稿（含 Provider 级计价币种对审计日志/报表解析链的传导影响独立说明） -->

## 0. 方案核心决策 (Core Architectural Decisions)

本方案基于第一性原理与马斯克五步工作法（“质疑每一个要求，删除所有不必要的部分”），对 VMR 当前繁琐、重叠且存在多处心智混乱的定价配置体系进行**彻底的根治性重构**。

### 五大终极决策：
1. **彻底切除顶层 `pricing:` 块**：
   * 删掉顶层 `pricing.rates`、`pricing.aliases`、`pricing.standard`；
   * 彻底杜绝在顶层定义模型价格的任何入口，消灭多处配置重叠的根本诱因。
2. **彻底淘汰外部 `pricing.yaml` 补充表**：
   * 删掉 `pricing.supplement` 配置项；
   * 物理删除仓库中的 `pricing.mock.yaml`、`pricing.example.yaml` 等 sidecar 资产，用户以后不需要、也不可能再外挂额外的价目表文件。
3. **全局客观常数 `exchange_rate`（汇率）独立提至顶层**：
   * 汇率是全系统共享的客观物理事实（`1 USD = X 货币`），直接作为顶层全局字段配置；
   * 严格校验（Fail-Fast），分两步：用户未显式声明某货币的汇率时，先尝试内置的合理默认汇率表兜底（见 2.3 节）；内置表也没有的冷门货币，才在加载期直接报错拒绝启动。任何一步都不允许静默猜测或按 0/1.0 处理。
4. **所有模型定价、别名映射与结算币种全量收敛至 `providers[].pricing`**：
   * `currency`: 本账号真实的结算货币（如 USD、CNY、EUR 等，默认 USD），解决跨境多账号混合计费硬伤；
   * `aliases`: 本账号的私有别名/方言映射（全面替代原 `map`，命名与内置表规范统一）；
   * `rates`: 本账号的具体模型费率或相对折扣（全面替代原 `overrides`，命名与内置表规范统一）。
   * 这一条本身不引入架构风险——账户按什么币种结算是账户自己的客观事实，理应挂在账户（provider）身上。它唯一的连锁影响是：审计日志与 `vmr report`/`vmr analyze` 事后重新解析历史记录时，“这条记录该按哪个币种解释”不能再无条件地问“当前 config.yaml”。这个连锁问题独立在**第 6 节**完整展开，不在这里冲淡决策本身。
5. **全系统定价收敛为绝对清晰的“纯粹二层模型”**：
   * **Layer 1（定制契约）**：Provider 内部的局部私有定义（`providers[].pricing`）；
   * **Layer 2（官方底座）**：VMR 内置标准库（Generated + Curated），随二进制发布，开箱即用不可变；
   * 绝无任何中间层、扩展层或外部文件层！

---

## 1. 现状审查与源码实证 (Source Code Fact-Checking)

为了杜绝“文档滞后于代码”的通病，以下所有分析均直接基于当前生产代码核实：

### 1.1 痛点一：四个入口可以配价格，概念极度混乱
在现有代码（`internal/config/pricing.go`）中，用户若想为一个模型定义价格，系统提供了多达 4 个互相重叠的入口：
1. 内置标准表（`go:embed` 的 `standard_price_*.yaml`）
2. 外部补充表文件（`pricing.supplement: ./pricing.yaml`）
3. 全局内联价目表（`pricing.rates` 与 `pricing.aliases`）
4. Provider 局部定义（`providers[].pricing.map` 与 `overrides`）

用户在配置时会产生巨大的心智负担：
*“这个价格我该写在顶层 rates 还是写在 provider overrides？我该写在顶层 aliases 还是 map？我要不要新建一个 pricing.yaml？”* 这违背了整洁架构“单一职责与单一事实来源”的基本原则。

### 1.2 痛点二：命名概念分裂（Table 层 vs. Provider 层）
核查 `internal/pricing/pricing.go` 与 `internal/config/pricing.go`：
* **Table 层（内置表/补充表）**：
  结构体 `fileTable` 中的字段是：**`aliases`** 和 **`rates`**。
* **Provider 局部层**：
  结构体 `ProviderPricingConfig` 中的字段突然变成了：**`map`** 和 **`overrides`**。

在同一个系统中，表达同一类概念（名称映射与价格声明）却使用了两套名词，造成了无谓的认知分裂。

### 1.3 痛点三：顶层强行统一度量衡，严重违背跨境多账号现实
核查 `internal/config/pricing.go` 的 `buildPricingContext` 与校验逻辑：
* 当前系统强迫所有账号在顶层统一配置一个 `pricing.currency`（如 `CNY`）；
* 导致用户的 Anthropic 官方账号（按美元扣费，$500 月预算），必须被迫人工折算成 3550 元人民币配进 `quota.limits` 中；
* 当引入欧洲供应商（EUR）或国内供应商（CNY）时，顶层的单一货币设定彻底沦为累赘。

### 1.4 痛点四：底层算账代码对“未知分量”的真实处理
核查 `internal/core/core.go` 的 `Rate.Cost` 核心计算函数：
```go
priced := func(tokens int64, perMillion *float64) float64 {
    if perMillion == nil {
        return 0 // ⚠️ 底层对于 nil 的未知分量，算账时直接当 0 计算！
    }
    return float64(tokens) / 1_000_000 * *perMillion
}
```
代码证明：所谓“未知的 rate 分量不能当 0，所以必须在 rates 留空”，仅仅是 `metric: cost` 在加载期的一道校验门禁（`Complete()` 函数）。用户若在 Provider 局部显式填 `0.0`，在底层计算、计费和报表估算上完全等价，没有任何必须保留顶层 rates 的技术必要性。

### 1.5 痛点五：内置 Curated 表已覆盖全网主流别名
核查 `internal/pricing/standard_price_curated.yaml`：
在分类 `(4) Generated-table first-party pins` 中，维护者已经把数量可观的主流公有模型裸名（`claude-3-7-sonnet`, `deepseek-v4-flash`, `chatgpt-4o-latest`, `glm-5.2` 等）**全部写死了官方权威别名**（具体条目数量随表内容定期刷新而变化，不作为设计依据硬编码）。公有模型根本无需用户自己配别名；私有方言写在 Provider 局部更安全，顶层 `pricing.aliases` 绝大多数场景都是冗余的。

---

## 2. 第一性原理深度推导 (First Principles Analysis)

### 2.1 为什么汇率提至顶层，而计价货币下沉到 Provider？
* **汇率是客观常数（Global Physical Constant）**：
  `1 USD = 7.1 CNY`、`1 USD = 0.92 EUR` 是现实金融市场的客观数据，全系统只需要声明一次。
* **计价货币是商业契约（Account Contract）**：
  Anthropic 账号的账单货币是 USD，Minimax 账号的账单货币是 CNY。将 `currency` 归位到具体的 `provider.pricing` 下，每个 Provider 的额度 `quota.limits.amount` 就能天然以各自的真实币种结算（$500 刀就是 500，2000 元就是 2000），不再需要人工换算。

### 2.2 为什么顶层价目表（`rates`/`aliases`）必须彻底切除？
1. **公有模型全由内置库覆盖**：内置库包含 LiteLLM 抓取的完整列表及手工校准的 Curated 表，开箱即用；
2. **私有/特价模型直接在 Provider 定义折后净价**：如果用户接入了一个冷门或自建模型，用户需要手工输入四分量价格。真实业务中，用户会直接填入最终有效价格，绝不会有人在顶层定义一个原价再跑到局部去打折；
3. **消除选择困难**：彻底杜绝“到底写在顶层还是局部”的困扰。整个系统有且仅有一个入口定制价格——那就是具体的 `provider.pricing`！

### 2.3 汇率兜底：能不能内置一份合理默认值，减少“用户必须手填 exchange_rate”这一步摩擦？
可以，而且这个思路在这个代码库里有现成先例，不是新架构：内置标准价目表（`standard_price_curated.yaml`/`standard_price_generated.yaml`）走的就是“内置合理默认 + 用户可覆盖 + `vmr check` 表龄超期提示刷新”这一套（`cmd/vmr/check_pricing.go` 的 `pricingStaleAfter` 常量）。汇率表体量比价目表小得多（几十种常见货币 vs. 数百个模型），直接复用同一套机制即可：

* **查找顺序**：用户 `exchange_rate[ccy]` → 内置默认汇率表 `[ccy]` → 报错。用户显式声明的条目永远覆盖内置默认，语义上和 `pricing.Merge` 已经在用的“覆盖层胜出”完全一致。
* **不能因为有了兜底就放弃 fail-fast**：内置表没有覆盖到的冷门货币，依旧在加载期硬错——这条底线的意义是“没查到就说没查到”，不是“查不到就编一个”。
* **如实标注局限**：汇率天天在变，不像模型报价那样几周不动，表龄提示对它意义有限；应当把它定位为“给你一个开箱可用的估算，不是记账精度的汇率”。`vmr check` 必须清楚标注某条汇率来自“用户配置”还是“内置默认（生成于某日）”，不能让用户分不清自己有没有配置生效。真的拿 `metric: cost` 的 `amount` 当预算硬闸门用、在意几个点误差的账号，应该自己显式写一条 `exchange_rate` 覆盖它。

---

## 3. 终极目标架构与配置全景对比 (Target Architecture: Before vs. After)

### 3.1 重构前配置（现状：冗余、重叠、割裂）
```yaml
# ❌ 旧架构：顶层既有货币，又有 rates/aliases，还引用外部文件
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
    # 强行被迫适应顶层的 CNY
    quota:
      limits:
        - {metric: cost, every: 1mo, amount: 3550} # $500 被迫折算成人民币
    pricing:
      map:                         # 命名与顶层 aliases 不对称
        claude-fast: claude-3-7-sonnet
      overrides:                   # 命名与顶层 rates 不对称
        - model: "*"
          discount: 0.9
```

---

### 3.2 重构后配置（终极形态：极简、对称、各司其职）
```yaml
listen: 0.0.0.0:8800
api_keys:
  - vmr-devkey-prod-001

# ✅ 顶层仅保留客观常数：汇率表（无 pricing 块，无外部文件）
# 未声明的货币先查内置默认汇率表兜底（见 2.3 节），仍查不到才报错。
exchange_rate:
  CNY: 7.1
  EUR: 0.92

providers:
  # 场景 A：美元结算的国际厂商
  - name: anthropic
    base_url: {anthropic-messages: https://api.anthropic.com}
    api_key: ${ANTHROPIC_API_KEY}
    quota:
      limits:
        - {metric: cost, every: 1mo, amount: 500} # 纯粹直观的 500 美元！
    pricing:
      currency: USD                # 独立结算货币（默认即 USD，不写亦可）
      aliases:                     # 统一命名为 aliases (原 map)
        claude-fast: anthropic/claude-3-7-sonnet-20250219
      rates:                       # 统一命名为 rates (原 overrides)
        - model: "*"
          discount: 0.9            # 官方内置标准价打 9 折

  # 场景 B：人民币结算的国内厂商
  - name: minimax
    base_url: {openai-completions: https://api.minimaxi.com/v1}
    api_key: ${MINIMAX_API_KEY}
    quota:
      limits:
        - {metric: cost, every: 1mo, amount: 2000} # 纯粹直观的 2000 元人民币！
    pricing:
      currency: CNY                # 独立结算货币：人民币
      rates:
        - model: MiniMax-M3
          in_fresh: 1.5
          cache_read: 0.3
          cache_write: 1.5
          out: 6.0                 # 显式人民币价格，未知分量可填 0.0
```

---

## 4. 两层极简查找与逐步降级全景流程 (Lookup & Fallback Sequence)

在全新架构下，系统解析任意一个 `(provider, request_model)` 的最终费率时，遵循确定性的纯粹二层流程：

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
│     （具体模型精准匹配优先，其次 "*" 通配符）                    │
└───────────────────────────────┬──────────────────────────────────┘
                                │
         ┌──────────────────────┴──────────────────────┐
         ▼ 命中显式 rates                              ▼ 仅命中 discount 或未命中 rates
  【直接采用局部费率】                                   │
  直接采用 rates 声明的四分量，                          │ 必须向官方底座借用基准列表价
  查找终结！（无需查询内置库）                           ▼
┌──────────────────────────────────────────────────────────────────┐
│ 【第二层：VMR 内置标准库 (Built-in Table: Generated + Curated)】 │
│  规范 5 步查找官方 Base 费率（美元计价）：                       │
│   Step 1: <provider_name>/<TargetModel> (当 Provider 恰为 Vendor)│
│   Step 2: 裸名 TargetModel 直查                                  │
│   Step 3: 内置 Curated 权威别名直查（大量官方一方别名，具体条目  │
│           数随表内容刷新而变化）                                 │
│   Step 4: 后缀扫描 (*/TargetModel) 与第一方厂商优先级决胜        │
│   Step 5: Basename(TargetModel) 剥离 org 路径前缀递归重试        │
└───────────────────────────────┬──────────────────────────────────┘
                                │
               ┌────────────────┴────────────────┐
               ▼ 命中内置 Base 费率              ▼ 未命中 Base 费率
      【多币种折算与折扣叠合】                    【加载期报错 Fail-Fast】
      1. 汇率换算：                               提示：模型未定价，无法启动。
         若 Provider 声明 currency != "USD":      拒绝未定价上路！
           查找顺序：用户 exchange_rate[currency]
             → 内置默认汇率表[currency] → 报错（见 2.3 节）
           Base = Base * 上一步查到的汇率
      2. 折扣叠合：
         若有局部 discount:
           EffectiveRate = Base * discount
         若无局部 discount:
           EffectiveRate = Base
```

---

## 5. 多币种在运行态与报表态的数学闭环

### 5.1 运行态（路由与限额）：完全币种隔离
* **各算各的账**：
  * Anthropic 请求完成时，按其 USD 费率扣减，累加到该 Provider 的 `Counters.Cost`（USD 计数桶）；与它的 $500 Limit 直接比较。
  * Minimax 请求完成时，按其 CNY 费率扣减，累加到该 Provider 的 `Counters.Cost`（CNY 计数桶）；与它的 ¥2000 Limit 直接比较。
* `quota.limits[].amount` 天然就是这个 provider 自己的 `pricing.currency`，不需要在 Limit 上再单独配一个币种字段——一个 provider 只有一个记账币种，Limit 直接继承它。
* 彻底杜绝了不同账号之间的币种污染，路由阶段无需发生跨账号货币换算，也不依赖“历史上这个 provider 叫什么名字”——热路径永远只看**当前**配置，不存在下一节要处理的那类历史归属问题。

### 5.2 报表态（`vmr analyze` / `vmr report`）：统一汇率折算
* 报表具有全局视角，支持 `-currency <CCY>` 参数（默认 `USD` 或第一个检测到的币种）；
* 汇总时，每条历史记录先按它自己的记账币种算出原始成本，再通过顶层 `exchange_rate`（含 2.3 节的内置默认表兜底）折算进目标展示币种一次求和；
* 产出的全局支出报表精确透明，且支持单行明细追溯原始币种。
* **唯一不是“顺手就能做对”的地方**：一条历史记录的记账币种是谁，答案不能来自“当前 config.yaml 里这个 provider 叫什么名字、配了什么 currency”——provider 会改名、会下线、会被 `api_keys` 拆分，而报表处理的是过去写下的记录。这是决策 4 生效后唯一会失真的下游消费者，独立用**第 6 节**完整说明。

---

## 6. 连锁影响：Provider 级币种对审计日志与报表解析链的传导

### 6.0 这一节要解决什么，不解决什么
把 `providers[].pricing.currency` 从“不存在”变成“持久的账户属性”，这个决策本身没有问题——顶层 `exchange_rate` 是客观常数，理应全局一份；一个账户按什么币种结算，是这个账户自己的客观事实，理应挂在这个账户（provider）身上，第 2.1 节的论证成立，不需要重新讨论。

这一节要处理的，是这个决策生效之后唯一会失真的下游消费者：**`vmr report`/`vmr analyze` 重新解析历史审计日志时，“这条记录该按哪个币种解释”不能再无条件地问“当前 config.yaml”**，因为历史记录里点名的 provider，在事后重新解析的那一刻，未必还活在当前配置里（改名、下线、被 `api_keys` 拆分）。这不是要重新论证决策 4，而是给它配一个它需要的配套改动——不做这个配套，决策本身没错，但报表会在某个 provider 改名/下线之后的某一天，对着一批历史记录突然算错，而且是**悄悄**算错。

### 6.1 今天的信息在哪一步被丢掉了
路由半在配置加载期就把每个 provider+model 的价格解析成 `core.PricingSpec{Base, Overrides, Currency}`（`internal/config/pricing.go` 的 `resolvePricing`），这时候 `Currency` 字段还在。但热路径不会拿着这整条链去算账——`internal/router/snapshot.go` 的 `BuildSnapshot` 调用 `pricing.FoldSpec`（`internal/pricing/resolve.go`）把这条链**折叠**成一个纯数字的 `*core.Rate`（四个分量，不带币种标签），挂在 `core.Endpoint.PricingRate` 上（`internal/core/core.go` 的 `Endpoint` 定义）。这个折叠是刻意的：`KNOWN_ISSUES` §1.0 的红线是“价目表逻辑不能进实时路由热路径”，`PricingRate` 只应该是一堆能直接乘的数字。

后果是：**从 `BuildSnapshot` 那一刻起，“这几个数字是什么币种”这件事就已经不存在了**，只活在当次生成快照用的那份 config.yaml 里。今天全局只有一个 `pricing.currency`，这无所谓——反正全系统只有一个答案，问谁都一样，历史上从未暴露过问题。一旦币种变成 provider 的属性，这次折叠就是币种信息永久丢失的那一刻；而审计日志（`internal/audit`）此后也从未被要求记下它——它只记 `Attempt.Provider`/`Attempt.Model`（“名字”），从名字反查币种，靠的是事后回头问 config，而 config 会变。

（历史教训：`internal/pricing/resolver.go` 的 `ProviderPolicy` 类型注释记录过一次真实 bug——早期曾把“USD→记账币种”的折算因子按 provider 缓存，一个改名/删除的 provider 查不到缓存条目时，零值兜底成了“不换算”，同一张报表里一行按 USD 一行按 CNY，看起来都很正常。但那次的病根是“把本该只有一份的全局值，冗余复制进一个查不到就静默给错默认值的 map 里”，跟“provider 之间币种本来就该不同”是两回事——当时全局也只有一个币种，任何 provider 的正确答案都一样。这次要避免的不是“per-provider 有不同值”，而是重演“查不到时悄悄给一个看似合理实则错误的默认值”这个更一般的模式。）

### 6.2 唯一自洽的修复：把币种基准钉在生成记录的那一刻
这正是 CLAUDE.md 里已经写明的项目原则：“当 routing 半可以在动作发生的地方把依据钉成事实字段时，把它钉在审计记录上，不要让 analytics 半反推——差异测试钉住公式，被记录下来的字段钉住基准。” `Attempt.Forwarded`（`internal/audit/audit.go`，唯一的 setter 是 `router.forwardSuccess`）就是这个模式在这个代码库里现成的先例。

具体钉什么、不钉什么要分清楚——不是把整条定价链搬进日志：

* **要钉的**：这个 attempt 的记账币种代码（如 `"USD"`/`"CNY"`），一个字符串。它只回答“这几个数字算出来之后应该贴哪个标签”。
* **不钉的**：折算好的成本数字本身。成本永远是分析半事后从原始 token 数、用**当时能拿到的最新价目表**重算出来的——这一点不变，`config` 与 `report` 两侧各自独立解析、由差异测试钉住公式一致（`cmd/vmr/quota_parity_test.go` 的既有模式）不受影响，也不需要为这次改动而改。这次只钉**基准（币种是谁）**，不钉**结果（多少钱）**——两者混为一谈会把一次小改动变成重新设计整条记账管线，是过度工程。

### 6.3 写入侧：日志格式 + 日志写入
* **日志格式**：`internal/audit/audit.go` 的 `Attempt` 结构体新增一个字段，例如 `PricingCurrency string` `` `json:"pricing_currency,omitempty"` ``，和 `Provider`/`Model`/`Forwarded` 挂在同一层——币种是“这一次具体转发到了哪个 provider”的属性，粒度必须是 per-attempt（一次请求可能失败转移到好几个 provider，各自币种可能不同），不能挂在 `Record` 顶层。
* **日志写入**：`core.Endpoint` 需要在 `PricingRate` 旁边多带一份 `PricingCurrency string`——`BuildSnapshot` 折叠 `PricingSpec` 的那一步，顺手把折叠前还在的 `PricingSpec.Currency` 也存一份到 `Endpoint` 上（不需要改 `FoldSpec` 本身，它依旧只管算钱的四个数字，职责不混）。真正写进 `Attempt` 的时机，和 `Forwarded` 置位的地方（`router.forwardSuccess`）一致：转发成功、开始计费的那一刻，把 `ep.PricingCurrency` 抄进这次 attempt 记录。

### 6.4 读取侧：日志解析 + 报表查价格
* **`internal/pricing.Resolver`/`RateFor`**（`vmr report`/`vmr analyze` 共用）今天无条件拿 `provider` 名字去查“当前配置生成的” `perProvider` map。要改成：优先看调用方有没有传入这条记录自带的 `PricingCurrency`；有，直接采信，完全不用查当前 config；没有（见下），才退回今天这条“按名字查当前 config”的路径。
* **查无结果时不能静默**：记录没有 `PricingCurrency`（旧记录），且这个 provider 名字在当前 config 里也已经找不到，必须走“明确降级 + warning”，与 `-currency` 今天解析不出汇率时的做法一致（`cmd/vmr/cmd_report.go` 里“pricing: no exchange rate ... showing %s instead”那条先例）——报表上要看得出这一行“币种不明、未换算、按标准表原始 USD 值显示”，绝不能悄悄套用别的 provider 的币种或假设成 1.0。
* **旧审计记录不需要回填迁移**：JSONL 是只追加的日志，不做批量重写。新字段用 `omitempty`，与 `Attempt` 现有的 `UpstreamModel`/`ErrorClass` 等后加字段同等待遇——旧记录读出来这个字段就是空字符串，天然可判定为“这条记录没有钉住基准，走退化路径”，不需要任何迁移脚本。
* **不要让 `report`/`story`/`reqdetail` 各自发明一套退化规则**：所有从 `Attempt` 反推币种/成本展示的渲染路径，都必须走同一个 `Resolver` 提供的入口，不能有的走“优先读标签”、有的还在用老的“按名字查当前 config”——不统一的话就是把同一类问题分散埋进好几个包里，将来更难查。

### 6.5 这次改动之外，不需要跟着动的东西
* 汇率换算的公式、`FoldSpec`、`Rate.Cost`——原封不动，改动只发生在“标签怎么传”，不发生在“钱怎么算”。
* 热路径计费——`Endpoint.PricingRate` 依旧是纯数字，新增的 `PricingCurrency` 只在写审计记录那一刻被读一次，不进 `router` 的计费循环，`KNOWN_ISSUES` §1.0 的红线不受影响。
* “config 侧、report 侧各自独立解析、靠差异测试对齐”的既有架构——不变，这次只是给两边共享的那个“基准事实”找了一个不依赖存活 provider 的落脚点。

---

## 7. 源码重构实施与迁移计划 (Action Plan)

### 7.1 需修改的源码文件清单

| 文件路径 | 修改性质 | 具体修改内容 |
| :--- | :--- | :--- |
| `internal/config/config.go` | 配置模型 | 顶层新增 `ExchangeRate map[string]float64`；彻底移除 `Pricing *PricingConfig` |
| `internal/config/pricing.go` | 配置解析与校验 | 彻底删除 `PricingConfig` 及其关联的 Table 合并逻辑；`ProviderPricingConfig` 新增 `Currency`、`Aliases`、`Rates` |
| `internal/pricing/resolver.go` | 解析引擎 | 重构 `Resolver` 与 `ProviderPolicy`，支持 per-provider 独立 currency 与顶层 exchange_rate 联动；新增“优先采用调用方传入的历史币种标签，查无则退化”分支（见第 6 节） |
| `internal/pricing/resolve.go` | 核心算法 | `ResolveOptions` 移除已废弃的全局 Table 扩展参数，支持从 provider 继承 currency |
| `internal/pricing/pricing.go` | 数据结构 | 移除 `ParseTableWithRates` 等外部补充表解析代码，只保留内置表加载；清理无用接口 |
| `internal/pricing/`（新增内置资产，如 `standard_exchange_rate.yaml`） | 内置默认值 | 常见货币对 USD 汇率的内置合理默认表；用户 `exchange_rate` 条目始终优先覆盖（见 2.3 节） |
| `internal/core/core.go` | 类型定义 | `Endpoint` 新增 `PricingCurrency string`，与 `PricingRate` 在 `BuildSnapshot` 同一折叠点写入（见第 6 节） |
| `internal/audit/audit.go` | 审计日志 schema | `Attempt` 新增 `PricingCurrency string` `` `json:"pricing_currency,omitempty"` ``，写入点与 `Forwarded` 相同（见第 6 节） |
| `internal/router/snapshot.go`、`router.forwardSuccess` 所在文件 | 写入逻辑 | `BuildSnapshot` 折叠定价时顺带写 `Endpoint.PricingCurrency`；转发成功时把它写进对应 `Attempt`（见第 6 节） |
| `cmd/vmr/check_pricing.go` | CLI 诊断输出 | `vmr check` 更新输出格式：打印顶层 `exchange_rate`（标注来源：用户配置 / 内置默认 + 生成日期），按 Provider 打印各自的 `currency`、`aliases` 与 `rates` |
| `cmd/vmr/cmd_report.go`（及 `vmr analyze` 对应入口） | CLI | 把每条记录的 `Attempt.PricingCurrency` 传给 `Resolver`；查无历史标签且 provider 已从当前配置消失时，显式 warning 降级，不静默换算（见第 6 节） |
| `config.mock.yaml` / `.example.yaml` | 配置文件模板 | 移除顶层 `pricing:` 块及 `supplement`；更新 `exchange_rate` 与 Provider 局部定价示例 |
| 外部文件删除 | 资产清理 | 彻底删除 `pricing.mock.yaml`、`pricing.example.yaml` 及相关文档引用（含 `internal/pricing/example_test.go` 里对 `pricing.example.yaml` 的直接解析测试） |

---

### 7.2 平滑迁移与向后兼容实现（配置层，代码级）

在 `internal/config/pricing.go` 中，重塑 `ProviderPricingConfig`，实现旧键（`map`/`overrides`）到新键（`aliases`/`rates`）的无缝兼容：

```go
type ProviderPricingConfig struct {
    // Currency specifies the billing currency for this provider (e.g. USD, CNY, EUR).
    // Defaults to USD if empty.
    Currency string `yaml:"currency"`

    // Aliases maps local model names to canonical keys in the built-in standard table.
    // (Unified replacement for `map`).
    Aliases map[string]string `yaml:"aliases"`

    // Rates defines explicit rates or discount modifiers for this provider.
    // (Unified replacement for `overrides`).
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

对于顶层旧版 `pricing:` 块的兼容：
若用户配置文件中仍写有顶层 `pricing:`：
* 若包含 `exchange_rate`，自动迁移至顶层 `exchange_rate` 并给出警告提示；
* 若包含 `rates`、`aliases`、`supplement`，在 `vmr check` 中输出明确的 Deprecation 错误指引用户移入具体的 Provider 内部。

注意：`config.yaml` 走的是严格 `KnownFields` 解码（见 CLAUDE.md 的强约束），一旦 `PricingConfig` 结构体被整体删除，一份仍写着顶层 `pricing:` 的旧配置会直接得到 YAML 解码器自己的“未知字段”报错，而不是上面这条定制提示——想要保留友好的 Deprecation 文案，`PricingConfig` 至少要作为一个仅用于探测遗留写法的轻量壳保留到迁移窗口结束，不能真的“彻底删除”到编译期完全不存在这个类型。这是实现这条兼容承诺时需要预先规划的一个具体缺口，不是事后才发现的意外。

### 7.3 审计日志的向后兼容（不同于上面的 YAML 迁移）
第 6 节已经说明：`Attempt.PricingCurrency` 是纯增量字段，旧的 JSONL 记录不需要、也不做批量回填——报表侧按“字段缺失 = 走退化路径”处理即可，这条兼容不需要额外的迁移脚本或版本号机制。

---

## 8. 最终价值评估 (Value Assessment)

| 评估维度 | 重构前 (Before) | 重构后 (After) | 改善收益 |
| :--- | :--- | :--- | :--- |
| **定价配置入口** | **4 处**（内置表、外部文件、顶层 rates/aliases、Provider 局部） | **1 处**（仅 Provider 局部） | **心智负担彻底归零**，杜绝多处配置重叠 |
| **外部依赖文件** | 必须理解并管理 `pricing.yaml` 补充表 | **完全不需要额外文件**，单配置即全貌 | 部署更简单，容器化/配置分发更轻量 |
| **命名一致性** | Table 叫 `aliases`/`rates`，Provider 叫 `map`/`overrides` | 全系统统一规范为 **`aliases`** 与 **`rates`** | 概念完全对称，阅读直观度大幅提升 |
| **多币种支持** | 顶层强行统一单一货币，跨国账号被迫人工折算 | **顶层全局汇率（含内置默认表兜底），各 Provider 独立货币** | 真正契合多云、跨境账号商业计费现实，且比今天更少要求用户手填汇率 |
| **系统架构层级** | 复杂的四层混叠继承 | **纯粹的二层模型**（局部契约 + 官方底座） | 删除了数百行无用合并与回退代码，代码更整洁 |
| **一次性工程成本** | — | 新增审计日志字段（`Attempt.PricingCurrency`）、`Resolver` 退化分支、内置汇率表资产 | 第 6 节范围内的可控一次性改动，换来报表对历史 provider 变更（改名/下线/拆分）的长期正确性，不是免费的，但只需要做一次 |
