<!-- Ver 2026-08-07 14:00, by Opus 5 -->

# P2 开发计划：计量准确（Quota-Aware Routing 第二批）

设计依据：`docs/TokenPlan_Quota_Routing_Design_opus-5.md`（该文档 §14.3 的 P2 定义为准，
已在 P1 交付后按实测经验修订——见该文档 §4.2⑤/§9.2/§12.1 与
`docs/TokenPlan_Quota_P1_DevPlan_opus-5.md` §10）。

**本文的核对方法**：设计文档 §14.3 P2 的"范围"一段是概念级描述，不是代码级承诺。
本文每一条计划都先去读了实际源码再落笔——`internal/report/pricing.go`
（当前定价子系统的**唯一**实现）、`internal/core/core.go`（P1 的 `Limit`/`QuotaSpec`）、
`internal/router/quota.go`（P1 的计费/评分入口）、`internal/config/config.go`/`quota.go`、
`internal/archtest`（行数预算与依赖边界）、`docs/data/`（市场参考数据是否真实存在）。
**结论是：设计文档对 P2 的字面描述低估了工作量**——不是"把 pricing.yaml 挪进
config.yaml"这么简单，而是要从零建一套标准表基础设施，且过程中发现了三处设计文档
未覆盖到的真实架构冲突（见 §1 与 §6）。本文按核实后的真实情况重新规划落地顺序，
不是逐字翻译设计文档的"范围"小节。

---

## 1. 核对基线：本计划依赖的代码事实

以下每条都已读过源码确认。**实现时如与现状不符，先停下来核对，别按本文硬写。**

### 1.1 P1 留下的挂钩点（可以直接复用）

| # | 事实 | 位置 | 对 P2 的意义 |
|---|---|---|---|
| 1 | `core.QuotaMetric` 目前只有 `MetricRequests`/`MetricTokens` 两个常量 | `internal/core/core.go:278-283` | 加 `MetricCost` 是纯增量，不用改现有两个 |
| 2 | `core.Limit` 目前只有 `Metric/EveryN/EveryUnit/EveryText/Since/Amount` 六个字段，**没有** `ModelMultipliers`/`TokenWeights` | `internal/core/core.go:295-312` | 这两项按设计文档 §3 是**账号级**，应该挂在 `core.QuotaSpec` 上，不是 `core.Limit` 上——P1 的类型划分已经预留了这个位置 |
| 3 | `core.QuotaSpec` 目前只有 `Limits []Limit` 一个字段 | `internal/core/core.go:317-319` | 加 `ModelMultipliers map[string]float64`/`TokenWeights TokenWeights`/`Pricing *pricing.Resolved`（新类型，见 §6.3）都是纯增量字段 |
| 4 | `config.LimitConfig.validate` 对 `metric: cost` 硬编码报错拒绝 | `internal/config/quota.go` 的 `LimitConfig.validate` | P2 要把这一条改成真正解析，不是删掉校验——仍要保留对四项费率不全的报错，只是报错原因变了 |
| 5 | `internal/config` 已经依赖 `internal/quota`（复用 `quota.DefaultSince`），证明"config 在 `validate()` 阶段依赖一个只依赖 core 的叶子包"这条模式可行 | `internal/config/quota.go` 顶部 import | P2 的 `internal/pricing` 直接复用同一条边，不是新发明 |
| 6 | `internal/router/quota.go` 的 `chargeQuota` 已经拿到 `ep *core.Endpoint`（含 `ep.Model`、`ep.Provider`）和 `rbody *respStream`（含 `Usage()`/`OutBytes()`） | `internal/router/quota.go:34` | model_multipliers 按 `ep.Model` 查表、cost 按 `ep.Provider+ep.Model` 查费率，两者都不需要改函数签名，直接在函数体内取 |
| 7 | `baseAmount(metric, counters) float64` 是 `QuotaStatus`/`scoreForEndpoint` 共用的唯一换算函数，签名只有 `(metric, counters)` 两个参数 | `internal/router/quota.go:108`、调用点在 `:184`/`:296` | **P2 必须改这个签名**——token_weights 需要知道账号的权重表（来自 `ep.Quota`），cost 需要直接读 `Counters.Cost`（不是从四分量重算，见 §6.2）。两个调用点都要跟着改 |
| 8 | `quota.Counters` 只有 `Fresh/CacheRead/CacheWrite/Out/Requests` 五个 `int64` 字段，没有货币字段 | `internal/quota/quota.go:19-25` | cost 计量需要新增一个字段（`Cost float64`，见 §6.2 的完整论证），**不是复用现有字段折算出来的** |
| 9 | `quota.bucket.Estimated` 是单个 `int64`，语义是"本周期由降级估算贡献的 token 数" | `internal/quota/quota.go:44-49` | 对 `metric: cost` 场景，降级估算贡献的是**货币金额**（可能有小数），`int64` 精度不够——需要拆成 `EstimatedTokens int64` + `EstimatedCost float64` 两个字段，或者把 `Estimated` 整体改 `float64`（见 §6.2） |

### 1.2 定价子系统的真实现状（这是本轮核对发现落差最大的地方）

| # | 事实 | 位置 | 与设计文档 §4.2①②③ 的落差 |
|---|---|---|---|
| 10 | 当前**唯一**的定价实现是 `internal/report/pricing.go`（372 行）：单文件 `pricing.yaml`，`rates: [{provider, model, in_fresh_per_1m, cache_read_per_1m, cache_write_per_1m, out_per_1m, date_range, hour_range}]`，`RateFor(provider, model, ts)` 精确字符串匹配（`strings.ToLower` 大小写不敏感，**没有通配符**） | `internal/report/pricing.go` 全文件 | 设计文档假设已有"标准表 + 补充表 + 账号覆盖"三层结构——**实际是零层**，今天的 `pricing.yaml` 本身就是"账号覆盖"这一层，标准表和补充表都不存在 |
| 11 | `PricingRate` **没有 `discount` 字段**，也没有"先解出下层费率、discount 再乘上去"这套解析顺序——每一行就是最终生效的费率，没有分层 | 同上 | 设计文档 §4.2①④ 的 `overrides: [{model:"*", discount:0.6}]` 语法完全不存在，需要新写 |
| 12 | `rateKey(provider, model)` 精确匹配，**没有 `"*"` 通配符逻辑** | `internal/report/pricing.go:129-133` | 设计文档 §9.1 的 `overrides` 示例里 `model: "*"` 通配全模型——这条匹配逻辑要新写，不是已有的 |
| 13 | 定价键空间是 vmr 自己的 `provider`（config.yaml 里的名字）+ `model`（上游模型名），**不是设计文档说的"canonical key"**——没有 `map` 字段、没有"四步自动解析"、没有和上游任何标准表对齐的概念 | 同上 | 设计文档 §4.2②③ 的"标准表用 canonical key、账号覆盖用 vmr 自己的 provider+model"这个二层键空间设计，**账号覆盖那一层已经存在且形状吻合**，但 canonical key 那一层（标准表）完全没有 |
| 14 | `costFor` 计算公式是 `fresh*InFreshPer1M + cache_write*CacheWritePer1M + out*OutPer1M`——**故意不含 `cache_read`**，UserGuide.md 原文写"cache reads are treated as free, matching every provider's own billing" | `internal/report/aggregate.go:887-898`、`docs/UserGuide.md` "Cost estimate" 一节 | **这是一处需要在 P2 里正面处理的真实矛盾**：设计文档 §2.3 的核心论据之一就是"缓存读比新鲜输入便宜 5～120 倍（不是免费）"，`metric: cost` 的整套折算公式（`base(cost) = Σ_c tokens_c × Rate[...][c]`）四个分量都要计价，**必然含 cache_read**。但 `vmr report` 现有的 `$` 公式明确排除它。两条路径不能各算各的——详见 §6.1 |
| 15 | `go:embed` 在整个代码库里**从未被使用过**（`buildinfo` 用的是 `debug.ReadBuildInfo`，不是 embed） | 全仓库 `grep -rn "go:embed"` 零命中 | 设计文档 §4.2③ 要求的 `pricing/standard.generated.yaml`/`standard.curated.yaml` + `go:embed` 是全新基础设施，不是"复用现成模式" |
| 16 | `tools/` 目录**不存在** | 仓库根目录 | 设计文档 §14.3 P2 提到的"生成脚本"没有落脚点，是全新工作，包括脚本本身、CI/维护流程都要从头定义 |
| 17 | **`docs/data/` 下确有真实市场数据**：`TokenPlan_Market_Reference.md`（467 行）+ `model_prices_and_context_window.json`（46,476 行，2988 条模型记录，字段形如 `input_cost_per_token`/`cache_read_input_token_cost`/`output_cost_per_token`，是标准的 LiteLLM 价目表快照格式）+ 两份套餐快照 JSON | `docs/data/` | 这一条和设计文档一致，**不是落差**——标准表生成脚本有真实可用的输入数据，§14.5 测试计划里"两组取自市场参考文档的真实费率夹具"这条验收标准是可行的，不用改 |
| 18 | `model_prices_and_context_window.json` 的定价单位是 **per-token**（如 `3e-07`），vmr 现有的 `pricing.yaml` 单位是 **per-1M-token**（如 `0.14`）——设计文档 §4.2② 已经指出这条转换，且判定"不向上游看齐、做一次性转换" | 两处文件实测对比 | 设计文档这条决策依据仍然成立，生成脚本要做的转换是 `per_token_price * 1_000_000` |
| 19 | `internal/report/aggregate.go` 当前 **972 / 1000 行**，只剩 28 行预算 | `internal/archtest/file_sizes_test.go:29` 的 budget 与实测行数 | cost 相关改动（引入 `internal/pricing` 类型替换 `report.Pricing`、修 cache_read 公式）很可能不够 28 行余量，**要提前规划把 `costFor` 相关逻辑拆到新文件**，不能等预算炸了再拆 |
| 19b | `internal/report/pricing.go`（372 行）本身受哪个预算约束？**不受约束**——`file_sizes_test.go` 的 `fileLineLimits` 表里没有这个文件 | `internal/archtest/file_sizes_test.go` | 如果 `report.Pricing`/`PricingRate`/`LoadPricing` 整体迁移到 `internal/pricing`（见 §6.3），`report/pricing.go` 这个文件会大幅缩小甚至消失，不产生新的预算风险 |
| 20 | `pricing.yaml`（仓库根目录）是**已提交到 git 的样例文件**，不在 `.gitignore` 里（`config.yaml`/`config.local.yaml` 才是被忽略的真实密钥文件） | `git ls-files pricing.yaml` 返回非空；`.gitignore:10-11` | 破坏性变更（移除 `pricing.yaml`/`-pricing`）**要同时处理这个样例文件的归宿**——是整体删除、改写成迁移示例、还是保留但加废弃说明，需要在 §3 明确，不能遗漏 |

### 1.3 model_multipliers 与"读取侧折算"这条既定原则的真实冲突

> 这条是本轮核对里最容易被文档字面意思带偏的一处，单独展开。

| # | 事实/推理 | 依据 | 结论 |
|---|---|---|---|
| 21 | 设计文档 §9.2/§12.1 的既定原则是"`Counters` 存原始事实，折算全部在**读取侧**套用"，理由是"改配置不用做数据迁移" | 设计文档原文 + P1 已验证这条原则对 `token_weights` 成立（P1 §10 postmortem 已确认） | 这条原则对 **`token_weights`** 成立：账号内所有请求共用同一套四分量权重，与"这一笔具体是哪个模型"无关，读取时用当前配置的权重重新加权，历史数据永远有效 |
| 22 | 但设计文档 §3 给的统一公式是 `charge = base(metric) × ModelMultipliers[model]`——**model** 是这一笔请求当时实际打到的上游模型，且 `quota.Counters` 是**按 provider 聚合**的（`internal/quota/quota.go` 的 `Registry.accounts map[string]map[string]*bucket`，key 是 provider 名，不细分到 model） | `internal/quota/quota.go` 的 `Registry`/`bucket` 结构（P1 已定型） | 如果账号下有 3 倍/9 倍等多档模型倍率，同一个 provider 在同一个计费周期内可能既有 1 倍的普通模型调用、又有 9 倍的重模型调用——**读取时已经无法从聚合后的 `Counters.Requests`/`Counters.Fresh` 里反推出"这些量分别来自哪个模型、该乘几倍"**。原始信息在聚合那一刻就丢失了 |
| 23 | 结论 | 上面两条 | **`model_multipliers` 必须在计费时（charge time）就把倍率乘进去、把加权后的值写进 `Counters`，不能像 `token_weights` 那样留到读取时**。这意味着一旦账号配了 `model_multipliers`，`Counters.Requests`/`.Fresh` 等字段的语义从"原始请求数/token 数"变成"模型加权后的请求数/token 数-等价单位"——这是一个需要在文档和 `/admin/status` 展示上都讲清楚的**语义变化**，不是纯粹的内部实现细节 |

### 1.4 metric: cost 与"读取侧折算"原则的第二处冲突（比 model_multipliers 更根本）

| # | 事实/推理 | 依据 | 结论 |
|---|---|---|---|
| 24 | `PricingRate` 天然带 `date_from`/`date_to`/`hour_from`/`hour_to`——同一个 provider+model 在同一个计费周期内，不同时间段的费率可以不同（促销、分时） | `internal/report/pricing.go` 的 `PricingRate.matches` | 费率是**随时间变化的政策**，不是账号级一成不变的常量 |
| 25 | 如果 `Counters` 继续只存原始四分量、cost 在读取时才用"当前生效费率"重新计算，那么一个月度周期里，促销期消耗的 token 和非促销期消耗的 token 混在同一个聚合桶里之后，读取时只能用**读取那一刻**生效的费率去乘**整个周期累计的原始 token 数**——促销期实际是打折价，但读出来的账单会按当前费率整体重算，金额是错的 | 上面 24 + `quota.Registry` 的聚合结构（同 22） | **`metric: cost` 比 `model_multipliers` 更彻底地打破"读取侧折算"原则**——不是"这一笔该按哪个系数"这么简单，而是"哪怕系数一样，几点几分打的这一笔历史上到底该值多少钱"这件事，只有在计费的那一刻才能被正确回答，之后费率表一变就再也回答不了 |
| 26 | 结论 | 上面两条 | **`metric: cost` 必须在计费时把这一笔的 $ 金额算好、直接写进 `Counters.Cost`（新增字段），绝不能在读取时重新套费率计算**。这不是实现选择，是费率随时间变化这个事实决定的——`Counters` 对 `cost` 这个 metric 而言，本质上从"事实存储"退化成了"预计算结果的累加器"，这与 §9.2 对 `requests`/`tokens` 两档"存事实、读取套政策"的定位不同，需要在设计文档里补一条明确说明（本文档 §8 已记录为对设计文档的偏离，需要设计文档同步这条结论） |

---

## 2. 范围重估：P2 是否需要拆分（读完 §1 之后的诚实判断）

设计文档自己在 §14.1 做过一次"这份设计是不是过重了"的自评，把终态设计拆成四批；
现在 P1 实测数据在手，有必要对 P2 做同一层级的自评。

**P2 按设计文档字面范围包含的新增机制清单**：`token_weights`、`model_multipliers`
（且已确认要在计费时而非读取时套用，见 §1.3）、全新的 `internal/pricing` 叶子包、
`go:embed` 标准表（`standard.generated` + `standard.curated` 两个文件的合并逻辑）、
上游数据 → 标准表的生成脚本（`tools/` 下，全新）、用户补充表合并、`providers[].pricing`
的 `map`（四步自动解析）+ `overrides`（`discount` / 显式费率 / 时间窗，且需要新增
`"*"` 通配符匹配）、`metric: cost` 的计费路径（且已确认需要 `Counters.Cost` 新字段而非
读取时套用，见 §1.4）、`vmr report` 的 `costFor` 公式订正（含 cache_read，见 §6.1）、
以及"独立 `pricing.yaml` 废弃"这条破坏性迁移。

**对比 P1**：P1 的核心复杂度集中在一个新算法（headroom）+ 一套新状态（Registry）+
一个新的路由决策插入点，三件事互相独立、可以分步验证。P2 字面范围里，
**"账号级折算"（token_weights/model_multipliers）** 和 **"metric: cost + 定价基础设施"**
是两件几乎不共享实现的事——前者只碰 `internal/quota`/`internal/router/quota.go`/
`internal/core`/`internal/config`，后者额外牵扯一个全新叶子包、一个生成脚本、
`internal/report` 的一次公式订正、一次破坏性配置迁移。把两者按同一个"P2"一次性验收，
会重演设计文档 §14.1 自己批评过的"看起来是一批,实际是压缩在一起的好几件事"。

**结论（建议，非强制）**：P2 内部分两个可独立验收、可独立上线的子阶段——
**P2.1（账号级折算）** 和 **P2.2（cost 计量 + 定价基础设施）**，两者共用一份 DoD 和
测试纪律，但 P2.1 完成即可发布（`token_weights`/`model_multipliers` 独立于 `cost` 有效），
不必等 P2.2 的全新基础设施就绪。这不是提议改设计文档的批次编号（两者仍然都属于
设计文档定义的"P2"，不重新计入 P3/P4），只是本文档内部按"能不能独立验收"切一刀，
与 P1 用 S0–S7 做步骤切分是同一个纪律，不是新发明。下面 §4/§5 按这个两段式组织。

---

## 3. P2 范围契约

### 3.1 P2.1 做什么

- `providers[].quota.token_weights`：四个分量权重 `{in_fresh, cache_read, cache_write, out}`，缺省全 1.0
- `providers[].quota.model_multipliers`：`map[string]float64`，支持 `"*"` 通配缺省，缺省 1.0，
  **只作用于 `requests`/`tokens`**（设计文档 §4.2④ 已定案：cost 档的价格分化全部走定价层，
  不重复通过 model_multipliers 表达）
- **计费时（不是读取时）套用 model_multipliers**——见 §1.3，这是本文档对设计文档字面表述的
  一处必要澄清，不是范围变化
- 配置校验：`token_weights` 出现在无 `tokens` Limit 的账号上 → 报错；某 provider 的 Limit 全是
  `requests` 却配了 `token_weights` 类似地报错（`token_weights` 只对 `tokens` 档有意义）

### 3.2 P2.1 明确不做

`metric: cost`、`internal/pricing` 包、标准表/补充表/账号定价覆盖、`vmr report` 的
`costFor` 公式改动、`pricing.yaml` 迁移——这些全部推到 P2.2。

### 3.3 P2.2 做什么

- 新叶子包 `internal/pricing`：标准表（`go:embed` 的 `standard.generated.yaml` +
  `standard.curated.yaml`）+ 用户补充表合并 + `PricingRate`（含 `discount`、`"*"` 通配、
  `date_*`/`hour_*` 时间窗）+ 三层解析顺序（账号覆盖 → 补充表∪标准表 → 无费率）
- `tools/` 下的生成脚本：`docs/data/model_prices_and_context_window.json`
  （per-token）→ `pricing/standard.generated.yaml`（per-1M，canonical key），
  缺失分量不补 0，保留 MIT 署名
- `providers[].pricing`：`map`（四步自动解析）+ `overrides`
- `internal/config` 依赖 `internal/pricing`，在 `validate()` 阶段解析并做"四项费率不齐即报错"校验
- `metric: cost`：`core.MetricCost`、`quota.Counters.Cost`（新字段）、
  `internal/router/quota.go` 的计费时 $ 计算（见 §6.2）
- `internal/report` 迁移到消费 `internal/pricing`（替换 `report.Pricing`/`PricingRate`/
  `LoadPricing`），顺带订正 `costFor` 把 `cache_read` 计入公式（见 §6.1，一次**明确记录**的
  行为变化，不是静默改动）
- 破坏性变更：`pricing.yaml`/`vmr report -pricing` 移除，迁移指引写进 UserGuide

### 3.4 P2.2 明确不做

多窗口/rolling/Scope（P3 范围）、官方用量 API 校准（P4 范围）、`vmr report` 的额度燃尽看板
（P4 范围）。

---

## 4. 交付物

### 4.1 P2.1 新增/改动文件

| 文件 | 改动 | 预估行数 |
|---|---|---|
| `internal/core/core.go` | `QuotaSpec` 加 `ModelMultipliers map[string]float64`、`TokenWeights TokenWeights`（新结构体，四个 float64 字段） | ~30 |
| `internal/config/quota.go` | `QuotaConfig` 加 `TokenWeights`/`ModelMultipliers` 字段 + 校验（互斥规则见 §3.1） | ~60 |
| `internal/router/quota.go` | `chargeQuota` 在算出 base(tokens) 之后、写入 `Counters` 之前套 `ModelMultipliers[ep.Model]`；`baseAmount` 签名改为接收 `*core.QuotaSpec`（读 `TokenWeights`） | ~40 |
| `internal/router/quota_charge_test.go`/`quota_reorder_test.go` 等 | 新增/改动测试 | ~150 |
| `internal/config/quota_test.go` | 新增校验测试 | ~80 |
| `docs/UserGuide.md`/`.zh.md` | 补充 `token_weights`/`model_multipliers` 一段，**说明配了倍率之后 `/admin/status` 的四分量明细含义变化**（见 §1.3 的语义变化） | — |

### 4.2 P2.2 新增/改动文件

| 文件 | 职责 | 预估行数 |
|---|---|---|
| `internal/pricing/pricing.go` | `Pricing`/`PricingRate` 类型（从 `internal/report/pricing.go` 迁移+扩展：加 `Discount float64`、`ModelPattern` 通配匹配） | ~150 |
| `internal/pricing/resolve.go` | 三层解析顺序（账号覆盖 → 补充表∪标准表 → 无费率）、四步 canonical key 自动解析 | ~150 |
| `internal/pricing/standard.generated.yaml` | `go:embed` 的生成表（脚本产出，不手工维护） | 数据文件 |
| `internal/pricing/standard.curated.yaml` | `go:embed` 的手工补充表（国产厂商为主） | 数据文件，起始可以很短 |
| `internal/pricing/embed.go` | `go:embed` 指令 + 合并两个内置表 | ~30 |
| `internal/pricing/*_test.go` | 单测（含从 `internal/report/pricing_test.go` 迁移的用例） | ~350 |
| `tools/gen_standard_pricing/main.go`（或类似路径） | 生成脚本：读 `docs/data/model_prices_and_context_window.json`，产出 `standard.generated.yaml` | ~150 |
| `internal/config/pricing.go`（新文件，不进 `config.go`，理由同 `quota.go` 当初拆分） | `PricingConfig`（全局 `pricing:` 块）+ `ProviderPricingConfig`（`providers[].pricing`）+ 校验 | ~180 |
| `internal/core/core.go` | `MetricCost` 常量；`QuotaSpec` 或 `Endpoint` 挂已解析费率（具体挂点见 §6.3） | ~20 |
| `internal/quota/quota.go` | `Counters` 加 `Cost float64`；`bucket.Estimated` 拆分或改型（见 §1.1 #9） | ~15 |
| `internal/router/quota.go` | `chargeQuota` 加 `case core.MetricCost` 分支 | ~40 |
| `internal/report/pricing.go` | 大幅缩减或删除（类型迁移到 `internal/pricing` 后，这里只剩薄薄一层胶水，或者直接删除、`aggregate.go` 直接 import `internal/pricing`） | 由 372 行降至 ~0-50 |
| `internal/report/aggregate.go` | `costFor` 订正含 `cache_read`；如逼近 1000 行预算，`costFor` 及其辅助函数拆到新文件 `internal/report/cost.go` | 净增 ~20，或整段搬出 |
| `cmd/vmr/cmd_report.go` | 移除 `-pricing` 标志与 `defaultPricingFile` 自动加载逻辑；`LoadPricing` 调用点改为从 `*config.Config` 取已解析定价 | ~-20/+20 |
| `pricing.yaml`（仓库根目录） | 移除，或改写成"如何在 config.yaml 里表达同样的覆盖"迁移示例（见 §1.2 #20，需要明确取舍） | — |
| `config.example.yaml` | 新增 `pricing:`/`providers[].pricing` 注释示例 | — |
| `docs/UserGuide.md`/`.zh.md` | 「Cost estimate and pricing.yaml」整节重写为"配置在 config.yaml 里的定价" + 迁移指引 | — |
| `docs/TokenPlan_Quota_Routing_Design_opus-5.md` | §4.2① `discount`/`"*"` 通配、`Counters.Cost` 的新增字段，按本文档 §8 的偏离表同步 | — |

---

## 5. 分步实施

### 阶段 P2.1（账号级折算，规模接近 P1 的 S4，独立可发布）

#### S1 — `core`/`config` 类型：`TokenWeights`/`ModelMultipliers`

```go
// internal/core
type TokenWeights struct{ InFresh, CacheRead, CacheWrite, Out float64 } // 缺省全 1.0

type QuotaSpec struct {
    Limits           []Limit
    TokenWeights     TokenWeights      // 零值即 {0,0,0,0}，不能直接当"全 1.0"用，
                                        // BuildSnapshot 转换时必须显式给缺省值
    ModelMultipliers map[string]float64
}
```

> **陷阱提醒**：`TokenWeights{}` 的零值是全 0，不是设计要的"缺省全 1.0"——转换代码
> （`config` 侧 `validate()` 或 `router` 侧 `BuildSnapshot`）必须显式把未设置的字段填 1.0，
> 不能依赖 Go 的零值语义。这类"零值恰好不是想要的缺省值"的陷阱在 P1 的 `HeadroomCap`
> 常量、`epsilon` 常量上也出现过，是这个功能反复踩的一类坑，值得写进测试名字里直接钉住
> （如 `TestTokenWeights_ZeroValueIsNotDefault`）。

**验收**：`config.QuotaConfig` 加两个可选字段，均未设置时 `base(tokens)` 与 P1 逐字节一致
（复用 P1 已有的 `token_weights 全 1.0` 等价性测试思路）。

#### S2 — `chargeQuota` 套用 model_multipliers（计费时，不是读取时）

按 §1.3 的结论，在 `tokenCharge`/`chargeQuota` 算出 `quota.Counters` 之后、调用
`rt.Quota.Charge` 之前，用 `ep.Model` 查 `ep.Quota.ModelMultipliers`（先精确匹配，
未命中查 `"*"`，都未命中用 1.0），把 `Counters` 的每个分量乘上去再写入。
`requests` 档同理，把 `Counters.Requests` 乘上倍率（允许非整数结果吗？
**不允许**——`Requests` 字段是 `int64`，倍率导致的小数需要**四舍五入**还是**向上取整**？
设计文档类型 B 的例子是"3 倍模型倍率"这种整数倍率，但没有排除非整数倍率的可能——
**本步需要显式决定并写进代码注释**：建议向上取整（`math.Ceil`），因为多算一点请求数
只会让该账号提前一点降权，方向安全；向下取整则可能让一个已经用满倍率额度的模型
调用还被记成没超。

**验收**：一条 `model_multipliers: {"heavy": 9}` 的账号，`heavy` 模型的一次成功调用
让 `Counters.Requests`（或 `.Fresh`/`.Out`，视 metric）变成普通调用的 9 倍；
`"*"` 通配和具名 key 都有测试；非整数倍率四舍五入方向有专门断言。

#### S3 — `baseAmount` 签名调整 + token_weights 读取时套用

`baseAmount(spec *core.QuotaSpec, c quota.Counters) float64`（不再是
`baseAmount(metric, counters)`——`QuotaStatus`/`scoreForEndpoint` 两个调用点都要跟着改，
从 `ep.Quota` 里取 `TokenWeights` 应用到四分量求和上）。

**验收**：`TestBaseAmount_TokenWeightsApplied`；两个调用点（`QuotaStatus`、
`scoreForEndpoint`）改动后 P1 的既有测试全部不受影响地重跑一遍确认无回归。

#### S4 — 配置校验 + 可观测性

- `token_weights` 出现在无 `metric: tokens` Limit 的账号 → 报错
- 某账号 Limit 全是 `requests` 却配了 `token_weights` → 报错（`token_weights` 对 `requests`
  档无意义）
- `/admin/status` 的 `quota` 段展示当前生效的 `model_multipliers`/`token_weights`
  （设计文档 §11 已经要求这个，P1 没做是因为 P1 没有这两项配置可展示）
- `vmr check` 打印账号的 `model_multipliers`/`token_weights`（同上）

**验收**：`vmr check`/`/admin/status` 对配了权重的账号可见；两条越界配置各有明确报错。

### 阶段 P2.2（cost 计量 + 定价基础设施，规模超过整个 P1）

#### S5 — `internal/pricing` 包：类型 + 三层解析（无 I/O 之外副作用，可脱离标准表单测）

从 `internal/report/pricing.go` 原样迁移 `Pricing`/`PricingRate`/`matches`/
`resolveCurrencyFactors`/`moneyValue` 逻辑（已经过 P1 之前的实测验证，不必重写），
在此基础上新增：`Discount float64` 字段 + "先解出下层费率、`discount` 再乘上去"的解析顺序、
`"*"` 通配符匹配（`model` 字段精确匹配优先于 `"*"`，命中即停，不做"多条通配符取并集"这种
复杂语义）。

**验收**：`internal/report/pricing_test.go` 的全部既有用例迁移到 `internal/pricing` 后
依然通过（证明迁移没有丢行为）；新增 `discount`/通配符专项测试。

#### S6 — 标准表：生成脚本 + `go:embed`

`tools/` 下的脚本读 `docs/data/model_prices_and_context_window.json`，输出
`internal/pricing/standard.generated.yaml`：per-token → per-1M 换算、字段改名
（`input_cost_per_token`→`in_fresh`、`cache_read_input_token_cost`→`cache_read` 等）、
**缺失分量不补 0**（用一个可区分"缺失"与"显式 0"的 YAML 写法，例如缺失字段整体不出现
在该行，而不是写 `0.0`）、canonical key 直接用上游 JSON 的顶层 key。
`standard.curated.yaml` 起始可以是空文件或只有个位数条目（国产厂商），后续人工增补。

**验收**：脚本跑一次产出的文件能被 `internal/pricing` 加载不报错；抽查两个真实模型
（如设计文档验收要求的两组市场参考费率夹具）核对换算结果正确；`standard.curated.yaml`
里手写的条目在重新跑脚本后不被覆盖（脚本只写 `standard.generated.yaml`）。

#### S7 — `internal/config` 接入：`pricing:`/`providers[].pricing` 解析与校验

`config.Config` 加全局 `Pricing *PricingConfig`（`currency`/`exchange_rate`/`supplement`）；
`config.Provider` 加 `Pricing *ProviderPricingConfig`（`map`/`overrides`）。`validate()`
阶段调用 `internal/pricing` 完成三层解析，对每个配了 `metric: cost` 的账号，涉及的每个
上游模型必须解析出四项费率齐全（含"显式 0.0"与"字段缺失"的区分，见设计文档 §4.2①）。

**验收**：`internal/config/quota_test.go`/新增 `pricing_test.go` 覆盖：费率不全报错、
`discount` 与显式费率二选一冲突报错、`pricing.supplement` 指了路径但文件不存在报错、
四步 canonical key 自动解析（含"有歧义不猜"）。

#### S8 — `metric: cost` 计费路径

`core.MetricCost` 常量；`quota.Counters.Cost float64` 新字段；`chargeQuota` 新增
`case core.MetricCost`，用 `ep.Provider`+`ep.Model`+计费时间戳查 `internal/pricing` 解析好的
费率（挂点见 §6.3），算出这一笔的 $ 金额直接写进 `Counters.Cost`（**不经过读取侧折算**，
按 §1.4 的结论）；`baseAmount` 对 `metric: cost` 直接返回 `Counters.Cost`。

**验收**：`cost` + `discount: 0.6` 的结果恰为纯 `cost` 的 0.6 倍（折扣层未被二次套用，
设计文档验收原文）；两组真实费率夹具下 `cost` 与等权总 token 的比值落在 3～8 倍区间
（设计文档验收原文，`docs/data/` 数据已确认可用）；促销时间窗内外收费不同的一笔请求，
`Counters.Cost` 记的是**计费那一刻**生效的费率，事后改价不影响已记录的历史值。

#### S9 — `vmr report` 迁移 + `costFor` 订正

`internal/report` 改为消费 `internal/pricing` 的类型（`aggregate.go` 直接 import，
不再有 `report.Pricing`/`report.LoadPricing`）；`costFor` 加入 `cache_read` 分量
（**这是一次会改变现有 `$` 输出数值的行为变化**——对当前仓库自带的 `pricing.yaml` 样例
影响为零，因为那四个 provider 的 `cache_read_per_1m` 都是 0，但对未来配了非零
`cache_read` 费率的账号，报表 `$` 数字会比修复前更高、更准确，需要在 CHANGELOG 级别的
地方——这里没有 CHANGELOG.md，就写进 UserGuide 的相应段落——说明这是修复不是回归）。

**验收**：`internal/report` 全部既有 pricing 相关测试迁移后通过；新增一条断言
"配了非零 `cache_read` 费率时 `$` 数字比排除 cache_read 时更高"，钉住这次订正确实生效。

#### S10 — 破坏性变更收尾

移除 `cmd/vmr/cmd_report.go` 的 `-pricing`/`defaultPricingFile`；仓库根目录的
`pricing.yaml` 按 §1.2 #20 的决定处理（建议：删除，UserGuide 的迁移指引里给出
"如何把这四个 provider 的费率改写成 `config.yaml` 里的 `providers[].pricing.overrides`"
的对照示例，比留一个不再被任何代码读取的死文件更清楚）；`vmr check`/`vmr report` 的
帮助文本同步更新。

**验收**：`vmr report -pricing xxx.yaml` 返回"unknown flag"错误（而不是静默忽略）；
`config.example.yaml` 能被 `vmr check` 通过；UserGuide 的迁移指引经人工核对可执行。

---

## 6. 关键实现细节

### 6.1 `cache_read` 定价公式订正：为什么必须改，以及影响范围

现状（§1.2 #14）：`internal/report/aggregate.go` 的 `costFor` 不计 `cache_read`，
理由写在 `pricing.yaml` 的注释里——"每个 provider 都把缓存读当免费/近乎免费"。这句话
描述的是**样例文件里那四个 provider 的真实费率**（碰巧都是 0 或近 0），不是"所有 provider
的缓存读都不计价"这个普遍事实——设计文档 §2.3 的市场统计明确指出缓存读比新鲜输入便宜
5～120 倍（不是免费）。`metric: cost` 的公式如果排除 `cache_read`，会系统性低估所有
配了非零 `cache_read` 费率账号的消耗——方向恰好是设计文档在 §9.1 反复强调的"最危险的
失效方向"（低估 → 账号显得比实际宽裕 → 超支）。**订正是必须的，不是可选的代码整洁性改动**。

对 `vmr report` 现有输出的实际影响：只要 `internal/pricing` 的费率表（无论标准表还是
账号覆盖）里某条目的 `cache_read` 是非零值，订正后那个 provider/model 的 `$` 数字会变高。
仓库自带的样例 `pricing.yaml` 四个 provider 全部是 0，所以这次订正对"跑一遍现有仓库自带
样例配置"这个具体场景零影响——但这是巧合，不是订正本身设计成"不影响现有输出"。

### 6.2 `Counters.Cost` 为什么是新增字段，不是从四分量重算出来的

见 §1.4 的完整论证（费率随时间变化，聚合后的原始 token 数无法在读取时正确还原历史
金额）。具体到类型设计：

```go
// internal/quota
type Counters struct {
    Fresh, CacheRead, CacheWrite, Out, Requests int64
    Cost float64 // 计费时直接算好的金额（币种 = 该账号 pricing.currency 或全局 currency）
                 // 只有 metric: cost 的 Limit 会写这个字段；requests/tokens 档恒为 0
}
```

`Fresh`/`CacheRead`/`CacheWrite`/`Out` 这四个字段在 `metric: cost` 场景下**依然要继续
记录原始 token 数**——不是因为路由决策需要它们（`cost` 档的路由决策只看 `Cost`），
而是因为 `/admin/status` 的四分量明细展示对 `cost` 账号同样有价值（用户能看出这一期
主要是被 cache_write 还是 fresh input 吃掉了钱），这与 P1 设计文档 §14.2 依据①"分量明细
近乎免费,让用户第一天就能自己核算"是同一个理由的延伸。

`bucket.Estimated`（§1.1 #9）对应拆成：

```go
type bucket struct {
    PeriodStart int64
    C           Counters
    EstimatedTokens int64   // 沿用 P1 语义：本周期由降级估算贡献的 token 数
    EstimatedCost   float64 // 新增：本周期由降级估算贡献的金额（仅 cost 档非零）
}
```

### 6.3 费率解析结果挂在哪：`core.Endpoint` 还是 `core.QuotaSpec`

设计文档没有明确回答这个问题（§9.1/§9.2 只说"随 Snapshot"，没说挂在哪个类型上）。
两个选项对比：

- **挂 `core.Endpoint`**（类似 P1 的 `ep.Quota` 但换成 `ep.PricingRate *pricing.Rate`）：
  优点是 `chargeQuota` 直接从 `ep` 上读，不用二次查表；缺点是同一个 provider 下不同
  model 的费率不同（费率天然按 model 分化，设计文档 §4.2⑦ 已经确认这点），如果每个
  `core.Endpoint`（provider+model 的组合）各自挂一份已解析费率，**这正好符合"费率按模型
  分化"的真实情况**，不像 `Quota` 那样需要在多个 endpoint 间共享同一个指针。
- **挂 `core.QuotaSpec`（账号级）**：不成立——§1.4/设计文档 §4.2⑦ 都已经确认价格是
  按模型分化的，账号级挂一份单一费率装不下多模型场景。

**建议**：挂 `core.Endpoint.PricingRate *pricing.Rate`（`nil` = 该端点无解析出的费率，
即使账号配了 `metric: cost`，这个端点也会在校验阶段就报错，不会跑到这里才发现 nil）。
`BuildSnapshot` 每个 `provider+model` 组合各自查一次 `internal/pricing` 的解析结果，
不像 `Quota` 指针那样可以整个 provider 共享。

### 6.4 model_multipliers 只影响 requests/tokens，不影响 cost——这条边界必须在代码里体现

设计文档 §4.2④ 已经定案"model_multipliers 收敛回纯粹的额度语义，只作用于
requests/tokens"，P1 §7 的偏离表也已经记录 `core.QuotaSpec` 目前没有这个字段。
P2.1 落地时，`chargeQuota` 的 `switch l.Metric` 分支结构决定了这条边界自然成立
（`case core.MetricCost` 走的是全新的定价路径，压根不读 `ModelMultipliers`）——
**但配置校验必须补一条**："某 provider 的 Limit 全是 `cost` 却配了 `model_multipliers`"
要报错（设计文档 §9.1 校验清单已经写了这条，P2.1 阶段这个 Limit 类型还不存在所以测不了，
必须等 P2.2 引入 `metric: cost` 之后补测，本文档 §7 的测试计划已经标注这条依赖顺序）。

---

## 7. 测试计划

| 层 | 用例 | 阶段 |
|---|---|---|
| `TokenWeights` 零值陷阱 | `TokenWeights{}` 不能被当全 1.0 用，转换代码必须显式填缺省值 | P2.1 S1 |
| model_multipliers 计费时套用 | 精确匹配/`"*"` 通配/都未命中三种情形；非整数倍率的取整方向 | P2.1 S2 |
| `baseAmount` 签名改动无回归 | P1 既有的 `QuotaStatus`/`reorderByQuota` 测试全部重跑通过 | P2.1 S3 |
| 配置校验 | `token_weights` 配在无 `tokens` Limit 的账号、配在全 `requests` 的账号 | P2.1 S4 |
| `internal/pricing` 迁移无损 | `internal/report/pricing_test.go` 全部用例搬到 `internal/pricing` 后逐条通过 | P2.2 S5 |
| discount 分层 | `discount` 作用于**下层解析出的**费率而非列表价原值；discount 与显式费率二选一冲突报错 | P2.2 S5 |
| `"*"` 通配符 | 具名 model 优先于 `"*"`；多条通配符不做并集语义 | P2.2 S5 |
| 生成脚本 | 缺失分量不补 0；per-token → per-1M 换算；`standard.curated` 手工行不被覆盖 | P2.2 S6 |
| 真实费率夹具 | 两组取自 `docs/data/` 的真实费率，`cost` 与等权总 token 比值落在 3～8 倍区间（设计文档验收原文，已确认数据可用） | P2.2 S6/S8 |
| 三层费率解析 | 账号覆盖 → 补充表∪标准表 → 无费率，优先级；`pricing.supplement` 路径不存在报错 | P2.2 S7 |
| canonical key 四步解析 | 含"有歧义不猜"分支 | P2.2 S7 |
| 四项费率不齐 | 区分"显式 0.0"与"字段缺失"，`metric: cost` 下必须报错 | P2.2 S7 |
| `Counters.Cost` 时间不变性 | 促销窗口内计费的一笔，事后改价不影响已记录金额（模拟：改配置重启，历史 `Cost` 字段不变） | P2.2 S8 |
| `metric: cost` 与 `model_multipliers` 互斥校验 | 全 `cost` 的账号配 `model_multipliers` 报错 | P2.2 S8（依赖 S2 的校验框架） |
| `cache_read` 公式订正 | 非零 `cache_read` 费率下 `$` 数字变化方向正确 | P2.2 S9 |
| `vmr report -pricing` 移除 | 传参报未知标志错误，不是静默忽略 | P2.2 S10 |
| `-race`/`archtest` | 全程 | 每步 |
| `internal/report/aggregate.go` 行数预算 | 每次改动后检查是否逼近 1000 行，提前拆分而不是事后补救 | P2.2 S9 |

---

## 8. 与设计文档的偏离（显式记录）

| 项 | 设计文档 | P2 实际计划 | 原因 |
|---|---|---|---|
| `model_multipliers` 的套用时机 | §3 给出公式但未明确"计费时"还是"读取时" | **计费时**，写入已加权的值 | `Counters` 按 provider 聚合、不细分 model，读取时已无法反推原始构成，见 §1.3 |
| `Counters` 对 `metric: cost` 的定位 | §9.2 定位为"存事实、折算全在读取侧" | **对 `cost` 档退化为预计算结果的累加器**（新增 `Cost` 字段，计费时写入最终金额） | 费率随时间变化，读取时用当前费率重算历史消耗会得出错误金额，见 §1.4 |
| 定价解析结果的挂点 | 未明确 | 挂 `core.Endpoint.PricingRate`（逐 provider+model），不挂 `core.QuotaSpec`（账号级） | 费率按模型分化是已确认的市场事实（设计文档 §4.2⑦），账号级挂点装不下 |
| P2 是否为单一批次 | 是，一个"P2"标签 | 建议内部分 P2.1（账号级折算）/P2.2（cost + 定价基础设施）两个可独立发布的子阶段 | 两者复杂度与依赖面差异巨大，合并验收会重演设计文档 §14.1 批评过的"看着是一批，实际是压在一起的好几件事"；不改变设计文档的批次编号，只是本文档内部的步骤纪律 |
| `costFor` 的 `cache_read` 处理 | 未提及 `vmr report` 现有公式与 `metric: cost` 公式是否一致 | **发现并订正**：`vmr report` 现有公式排除 `cache_read`，与 `metric: cost` 的四分量公式矛盾，P2.2 一并订正，作为显式记录的行为变化 | 两条路径共用同一份定价数据后，公式不一致会让同一个账号的 `$` 数字在 `vmr report` 和 `metric: cost` 里对不上，比"哪个公式更对"更重要的是"两者必须一致" |

---

## 9. 本轮核对发现的既有问题（不属于 P2，需另案登记）

- `pricing.yaml`（仓库根目录）当前既是"样例文件"又是**唯一**的真实定价机制，P2.2 落地后
  这个文件的定位会变得模糊（见 §1.2 #20）——建议在 P2.2 的 S10 一次性处理掉，不要跨批次
  拖着一个"名字还在但已经没人读"的文件。
- `internal/report/aggregate.go` 逼近行数预算（§1.2 #19）不是 P2 引入的问题，是既有状态——
  P2.2 的改动会是压垮这个预算的最后一根稻草，但预算逼近本身在 P2 开始之前就已经存在，
  值得单独记入 `docs/OUTSTANDING_ISSUES_opus-5.md`（本文档不代为登记，留给 P2.2 实施时
  按当时的实际改动量决定是否需要登记或直接处理）。

---

## 10. 完成定义（DoD）

### P2.1

1. `token_weights`/`model_multipliers` 未配置时，行为与 P1 逐字节一致。
2. model_multipliers 在计费时套用（不是读取时），有专项测试钉住聚合语义（§1.3）。
3. 配置校验覆盖 §3.1 列出的越界情形，无静默忽略。
4. `go test ./... -race`、`go vet`、`gofmt -l`、`archtest` 全绿。
5. `vmr check`/`/admin/status` 能看到生效的权重/倍率。

### P2.2

1. `internal/pricing` 的三层解析 + 生成脚本 + `go:embed` 标准表全部落地，
   `internal/report/pricing_test.go` 的既有用例零丢失地迁移。
2. `metric: cost` 的计费结果不受"读取时费率已变"影响（§6.2 的不变性测试）。
3. `cache_read` 公式订正在 `vmr report`/`metric: cost` 两处保持一致，且作为显式记录的
   行为变化写进用户文档，不是静默改动。
4. 破坏性变更（`pricing.yaml`/`-pricing` 移除）有完整的迁移指引，`config.example.yaml`
   通过 `vmr check`。
5. `internal/report/aggregate.go` 未超行数预算（或已提前拆分）。
6. `go test ./... -race`、`go vet`、`gofmt -l`、`archtest` 全绿；两组真实费率夹具下
   `cost` 与等权总 token 的比值落在 3～8 倍区间。
