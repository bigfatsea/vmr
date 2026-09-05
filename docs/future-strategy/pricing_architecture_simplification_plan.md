# VMR 定价体系终极极简重构方案与第一性原理审查 (Pricing Architecture Radical Simplification Plan)

<!-- Date: 2026-09-16 | Status: 设计终稿（彻底消除顶层价目表与外部 sidecar，实现纯粹二层模型） -->

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
   * 严格校验（Fail-Fast）：只要任何 Provider 声明使用了非 USD 货币，若顶层 `exchange_rate` 缺失对应条目，系统在加载期直接报错拒绝启动，杜绝静默错误。
4. **所有模型定价、别名映射与结算币种全量收敛至 `providers[].pricing`**：
   * `currency`: 本账号真实的结算货币（如 USD、CNY、EUR 等，默认 USD），解决跨境多账号混合计费硬伤；
   * `aliases`: 本账号的私有别名/方言映射（全面替代原 `map`，命名与内置表规范统一）；
   * `rates`: 本账号的具体模型费率或相对折扣（全面替代原 `overrides`，命名与内置表规范统一）。
5. **全系统定价收敛为绝对清晰的“纯粹二层模型”**：
   * **Layer 1（定制契约）**：Provider 内部的局部私有定义（`providers[].pricing`）；
   * **Layer 2（官方底座）**：VMR 内置标准库（Generated + Curated，随二进制发布，开箱即用不可变）。
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
在分类 `(4) Generated-table first-party pins` 中，维护者已经把数百个主流公有模型裸名（`claude-3-7-sonnet`, `deepseek-v4-flash`, `chatgpt-4o-latest`, `glm-5.2` 等）**全部写死了官方权威别名**。公有模型根本无需用户自己配别名；私有方言写在 Provider 局部更安全，顶层 `pricing.aliases` 99.9% 的场景都是冗余的。

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
│   Step 3: 内置 Curated 权威别名直查 (349+ 官方第一方别名)        │
│   Step 4: 后缀扫描 (*/TargetModel) 与第一方厂商优先级决胜        │
│   Step 5: Basename(TargetModel) 剥离 org 路径前缀递归重试        │
└───────────────────────────────┬──────────────────────────────────┘
                                │
               ┌────────────────┴────────────────┐
               ▼ 命中内置 Base 费率              ▼ 未命中 Base 费率
      【多币种折算与折扣叠合】                    【加载期报错 Fail-Fast】
      1. 汇率换算：                               提示：模型未定价，无法启动。
         若 Provider 声明 currency != "USD":      拒绝未定价上路！
           Base = Base * exchange_rate[currency]
           （若 exchange_rate 缺失该货币则报错）
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
* 彻底杜绝了不同账号之间的币种污染，路由阶段无需发生跨账号货币换算。

### 5.2 报表态（`vmr analyze` / `vmr report`）：统一汇率折算
* 报表具有全局视角，支持 `-currency <CCY>` 参数（默认 `USD` 或第一个检测到的币种）；
* 汇总统计总支出时：
  * 若目标展示货币为 `CNY`：
    * Minimax 的支出（CNY）直接累加；
    * Anthropic 的支出（USD）通过顶层 `exchange_rate.CNY`（7.1）乘入累加；
* 产出的全局支出报表精确透明，且支持单行明细追溯原始币种。

---

## 6. 源码重构实施与迁移计划 (Action Plan)

### 6.1 需修改的源码文件清单

| 文件路径 | 修改性质 | 具体修改内容 |
| :--- | :--- | :--- |
| `internal/config/config.go` | 配置模型 | 顶层新增 `ExchangeRate map[string]float64`；彻底移除 `Pricing *PricingConfig` |
| `internal/config/pricing.go` | 配置解析与校验 | 彻底删除 `PricingConfig` 及其关联的 Table 合并逻辑；`ProviderPricingConfig` 新增 `Currency`、`Aliases`、`Rates` |
| `internal/pricing/resolver.go` | 解析引擎 | 重构 `Resolver` 与 `ProviderPolicy`，支持 per-provider 独立 currency 与顶层 exchange_rate 联动 |
| `internal/pricing/resolve.go` | 核心算法 | `ResolveOptions` 移除已废弃的全局 Table 扩展参数，支持从 provider 继承 currency |
| `internal/pricing/pricing.go` | 数据结构 | 移除 `ParseTableWithRates` 等外部补充表解析代码，只保留内置表加载；清理无用接口 |
| `cmd/vmr/check_pricing.go` | CLI 诊断输出 | `vmr check` 更新输出格式：打印顶层 `exchange_rate`，按 Provider 打印各自的 `currency`、`aliases` 与 `rates` |
| `config.mock.yaml` / `.example.yaml` | 配置文件模板 | 移除顶层 `pricing:` 块及 `supplement`；更新 `exchange_rate` 与 Provider 局部定价示例 |
| 外部文件删除 | 资产清理 | 彻底删除 `pricing.mock.yaml`、`pricing.example.yaml` 及相关文档引用 |

---

### 6.2 平滑迁移与向后兼容实现（代码级）

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

---

## 7. 最终价值评估 (Value Assessment)

| 评估维度 | 重构前 (Before) | 重构后 (After) | 改善收益 |
| :--- | :--- | :--- | :--- |
| **定价配置入口** | **4 处**（内置表、外部文件、顶层 rates/aliases、Provider 局部） | **1 处**（仅 Provider 局部） | **心智负担彻底归零**，杜绝多处配置重叠 |
| **外部依赖文件** | 必须理解并管理 `pricing.yaml` 补充表 | **完全不需要额外文件**，单配置即全貌 | 部署更简单，容器化/配置分发更轻量 |
| **命名一致性** | Table 叫 `aliases`/`rates`，Provider 叫 `map`/`overrides` | 全系统统一规范为 **`aliases`** 与 **`rates`** | 概念完全对称，阅读直观度大幅提升 |
| **多币种支持** | 顶层强行统一单一货币，跨国账号被迫人工折算 | **顶层全局汇率，各 Provider 独立货币** | 真正契合多云、跨境账号商业计费现实 |
| **系统架构层级** | 复杂的四层混叠继承 | **纯粹的二层模型**（局部契约 + 官方底座） | 删除了数百行无用合并与回退代码，代码更整洁 |
