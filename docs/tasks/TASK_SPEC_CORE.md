<!-- Ver 2026-09-06, by Sonnet 5 -->

# 任务说明书：Pricing Architecture Simplification — CORE (config + pricing 包重构)

## 零、你的技术规格书

`docs/future-strategy/pricing_architecture_simplification_plan.md`（设计终稿）是本任务的
主技术规格：六大决策（§0）、第一性原理推导（§2）、终极配置形态 before/after（§3）、两层查找
流程（§4）、`ProviderPricingConfig` 的具体 Go 代码骨架与向后兼容实现（§7.2）都已经写死在那
份文档里，照着实现即可，不要另起炉灶重新设计形状。本说明书只补充：文件白名单、该文档遗漏
的部分、以及协作纪律。

`docs/prompts/prompt-multi-agent-guide.md` 的 §6.6（Worker 执行纪律与独立判断）同样适用：
第一性原理独立判断，敢于指出设计文档或本说明书里可能过时/错误的地方——但**改动范围仍必须
守住下面的白名单**，发现的问题记录进 `NOTES_FOR_LEAD.md`（不提交），不要自行扩大修改面。

## 一、协作原则与红线约束（铁律）

1. **工作区限制**：仅在你的 worktree 目录下操作，不要访问 `../vmr`（主工作区，Lead 与其他
   worker 也在用）或其他 `../vmr-wt-*` 目录。
2. **文件修改白名单（极度关键）**：
   - ✅ 允许修改/新增/删除：
     - `internal/config/**`（含 `config.go`、`pricing.go`、`quota.go`、`provider.go` 及全部
       `*_test.go`，包括 `pricing_test.go`、`quota_test.go`、`providergroup_test.go`）
     - `internal/core/core.go`（及 `internal/core/core_test.go` 中涉及
       `MetricCost`/`PricingRate`/`PricingSpec.Currency` 的用例）
     - `internal/pricing/**`（`pricing.go`、`resolve.go`、`resolver.go` 及全部 `*_test.go`、
       `example_test.go`；新增 `internal/pricing/standard_exchange_rate.yaml`）
     - `config.mock.yaml`、`config.example.yaml`、`config.example.zh.yaml`
     - `cmd/vmr/check_pricing.go`、`cmd/vmr/cmd_check.go`（及对应 `*_test.go`，如存在
       `cmd_check_quota_test.go` 中涉及 pricing/cost 显示的部分）
     - 删除：`pricing.mock.yaml`、`pricing.example.yaml`、`pricing.example.zh.yaml`
   - ❌ 严禁修改：白名单以外的任何文件，尤其是：
     - `internal/router/**`、`internal/quota/**`、`internal/replay/**`、
       `internal/report/**`、`internal/i18n/**`、`cmd/vmr/cmd_status_render.go`、
       `cmd/vmr/cmd_report_quota.go`、`cmd/vmr/quota_parity_test.go`——这些是 Stage 2
       CONSUMERS worker 的范围，它依赖你这一步产出的新类型签名（`core.MetricCost` 被删除、
       `core.Endpoint.PricingRate` 被删除等），必须在你之后跑
     - `internal/audit/audit.go`、`internal/report/cost.go`、`internal/story/cost.go`、
       `cmd/vmr/cost_basis_parity_test.go`——方案明确这些不动
     - `CHANGELOG.md`、`docs/KNOWN_ISSUES.md`、任何 `docs/VirtualModelRouter_Design_v4_*.md`、
       `docs/UserGuide.md`/`.zh`、本任务说明书、`docs/future-strategy/pricing_architecture_action_plan.md`
3. **代码风格与架构门禁**：遵循现有代码的注释风格（只写非显然的"为什么"，不写"是什么"）；
   `go test ./internal/archtest/...` 必须全绿——改动了包边界或让某个函数/文件变长时尤其要跑。
4. **Git 规范**：commit message 简短祈使句，**不加任何 trailer**（含 Co-Authored-By）。可以
   分多个 commit（例如 config 包一个、pricing 包一个、examples 一个），但每个 commit 都必须
   自身可编译。
5. **共享文件禁改**：见白名单里的"严禁修改"清单。任何你认为需要登记进 `CHANGELOG.md`/
   `docs/KNOWN_ISSUES.md`/设计文档的事项，写进你 worktree 根目录下的 `NOTES_FOR_LEAD.md`
   （不要 git add 它——它不提交，Lead 合并后会读取并处理）。
6. **语义变更预警**：本任务会让以下测试大概率打红，这是预期内的（不是你的 bug）：
   - `internal/config/pricing_test.go`、`internal/pricing/pricing_test.go`：整个断言旧的
     `supplement`/顶层 `rates`/`aliases`/`pricing.currency` 硬门禁的用例都需要**改写**为断言
     新形态（provider 局部 `aliases`/`rates`、顶层 `exchange_rate`、无顶层 `pricing:` 块）。
   - 允许你直接改这些测试文件本身的断言以匹配新语义，**不允许**为了让测试通过而削弱新架构
     该有的校验（比如取消"四分量全给或全不给"的形状校验）。
   - 若某个打红的测试你判断不属于"预期语义变更",而是可能暴露了真实 bug，记录进
     `NOTES_FOR_LEAD.md`，不要自作主张删除断言了事。
7. **忽略目录**：`_tmp/`、`archived/` 及编译产物目录视为不存在，不读取不修改。
8. **并发抗干扰**：同一台机器上，Lead 与 DOCS-USER worker 会同时在别的 worktree 里跑。不要
   被外部输出干扰，`git add` 时精确指定文件路径，严禁 `git add -A`/`git add .`。

## 二、具体任务清单

### 任务 1：`internal/config` 顶层配置重构（决策 1、3）
- `config.go`：移除 `Pricing *PricingConfig` 字段；顶层新增 `ExchangeRate map[string]float64`
  （yaml tag `exchange_rate`）。
- `pricing.go`：
  - 删除本会话此前遗留、已单独提交（commit `4223585`）的顶层内联 `pricing.rates`/
    `pricing.aliases` 字段与解析逻辑——那是被本方案决策 1/2 取代的更早期思路，**不要保留、
    不要迁移它的字段名**，`providers[].pricing.aliases`/`.rates` 才是终态。
  - 删除 `pricing.supplement`/`pricing.standard`/`pricing.currency` 的顶层解析。
  - 按 §7.2 的代码骨架实现 `ProviderPricingConfig{Currency, Aliases, Rates}` +
    legacy `map`/`overrides` 字段的 `normalize()` 兼容转换（互斥校验：两者都写视为配置错误）。
  - 保留一个轻量壳类型用于探测遗留顶层 `pricing:` 写法，给出明确的 Deprecation 报错指引
    （§7.2 最后一段）；`KnownFields` 严格解码，未知字段的原生报错优先级低于这条定制提示。
  - `resolvePricing`：按 §4 两层查找流程重写；删除 `hasCostLimit`/`costProviders`/
    `Complete()` 硬门禁/"currency is not set" 报错；provider `rates` 行按其 `currency` 经
    `pricing.FactorBetween` 在 validate 期归一 USD。
  - `Config.ResolvedPricing` 字段本身**保留**（`internal/pricing/resolve.go` 仍需要它作为
    离线解析的载体），但它不再要求"每个 metric:cost provider 的每个模型四分量齐全"——未命中
    只是该行报表无 $ 估计，不是加载期错误。
- `quota.go`：`metric: cost` 值本身在 `KnownFields` 抓不到（它是值不是键），必须显式拒绝并给
  出迁移指引，例如：`metric: cost is no longer supported — express the same budget as a
  tokens limit (convert once: budget ÷ price), optionally with model_multipliers/
  token_weights to weight expensive models; $ cost estimates remain available in vmr report`。
  删除 `hasCostLimit`、cost 与 `model_multipliers` 的互斥校验。
- `provider.go`：更新引用 `metric: cost` 的注释（约 74 行附近）。

### 任务 2：`internal/core/core.go`（决策 4、6）
- `QuotaMetric` 删除 `MetricCost` 常量。
- `Endpoint` 删除 `PricingRate *Rate` 字段及其相关注释。
- `PricingSpec` 删除 `Currency` 字段（`Base`/`Overrides` 保留——报表侧 override 链仍需要）。
- `Rate`、`Rate.Cost`、`PricingOverride` **保持不变**（报表成本公式的 SSOT 地位不变，见方案
  "不变式"一节）。

### 任务 3：`internal/pricing` 包（决策 1–5）
- `pricing.go`：移除外部补充表文件解析路径（`pricing.supplement` 消费方）；只保留内置表
  （embed 的 `standard_price_*.yaml`）加载。`RateRow.Currency` 保留（内置表行级归一仍要用）。
  新增 `internal/pricing/standard_exchange_rate.yaml`：常见货币对 USD 的内置默认汇率表（见
  方案 §2.3：查找顺序为用户 `exchange_rate[ccy]` → 内置默认表 `[ccy]` → 报错；内置表没覆盖
  的冷门货币依旧加载期硬错，不允许静默按 0/1.0 处理）。
- `resolve.go`：`ResolveOptions` 移除 `ExchangeRateToTarget`/`Currency`（内部解析恒定 USD，
  不再需要在 `Resolve()` 时转换到目标币种）；删除 `Resolve` 里对应的换算分支；删除
  `FoldSpec`（无挂点——`core.Endpoint.PricingRate` 已经不存在了）；`EffectiveRate`/
  `resolveChain` 保留；spec 级 `Complete()` 门禁删除，`Rate.Complete()` 保留（报表 incomplete
  标记仍用，见 `internal/report/cost.go`——你不需要碰那个文件，但它依赖 `Rate.Complete()` 继续
  存在）。
- `resolver.go`：`NewResolver` 签名瘦身，去掉 `tableFactor`/`currency` 两个已经不需要的参数
  （**这会连带影响调用方** `internal/config/pricing_test.go`——在你的白名单内，改；
  `internal/report/aggregate_test.go`、`internal/story/cost_test.go`、
  `cmd/vmr/cost_basis_parity_test.go`、`cmd/vmr/cmd_report.go`——**不在你的白名单内**，这些
  调用方的签名适配交给 Lead 在最终整合阶段处理，不要自己去改；如果你判断这个签名变更会让白
  名单外的文件编译失败，记录进 `NOTES_FOR_LEAD.md` 说明具体影响面，让 Lead 心里有数）。
  **`RateFor`/`RateForEndpoint` API、缓存键、`WithDisplayFactor` 机制原样不动**——这是本方案
  "删优于修"的关键验收点，报表侧的展示币种折算逻辑不应该因为这次重构受影响。
- `pricing_test.go`、`example_test.go`：改写以匹配新形态；`example_test.go` 目前解析
  `../../pricing.example.yaml`/`.zh` 校验其与内置表字段对齐——这两个文件本任务要删除，相应
  测试逻辑要么删除要么改为测试别的不变式，你自行判断并在 commit message 里说清楚。

### 任务 4：配置示例与 mock 文件
- `config.example.yaml`/`.zh`、`config.mock.yaml`：按方案 §3.2 的最终形态重写 `pricing:` 相关
  片段——顶层只剩 `exchange_rate:`，每个 provider 用 `pricing.currency`/`aliases`/`rates`。
  两份 `.example.yaml` 必须保持 key、结构、示例值一致，只有注释文案语言不同（项目 CLAUDE.md
  的规约）。
- 删除 `pricing.mock.yaml`、`pricing.example.yaml`、`pricing.example.zh.yaml`。

### 任务 5：`cmd/vmr/check_pricing.go`、`cmd/vmr/cmd_check.go`
- `check_pricing.go`：按方案 §7.1 重写 `vmr check` 的定价输出——打印顶层 `exchange_rate`
  （标注来源：用户配置 / 内置默认 + 生成日期）、按 provider 打印各自 `currency`（标注"解析期
  标注"）/`aliases`/`rates`；不再打印 cost 完整性门禁相关内容。
- `cmd_check.go`：删除约 344-346 行附近 `l.Metric == core.MetricCost && cfg.Pricing != nil &&
  cfg.Pricing.Currency != ""` 这类依赖已删除类型/字段的校验分支（`core.MetricCost`、
  `cfg.Pricing` 都不再存在）。**这个文件里还有大量与本任务无关的其他 check 输出逻辑（约 482
  行），只动 pricing/cost 相关的局部，不要顺手重构其他部分。**

## 三、测试与验收步骤

1. `go build ./...`
2. `go test ./internal/config/... ./internal/core/... ./internal/pricing/...`
3. `go test ./internal/archtest/...`
4. `gofmt -l $(git diff --name-only main -- '*.go')` 应为空
5. `./vmr check -c config.example.yaml`（或 `config.mock.yaml`）手工跑一遍，确认新配置形态
   能正常加载且 `vmr check` 输出符合任务 5 的预期
6. `git status -s`（确认无越界文件——只应出现白名单内的文件）
7. 分批 `git add <具体文件>` + `git commit`（不要 `git add -A`）
8. 完工后把关键决策点/需要 Lead 关注的事项写进 `NOTES_FOR_LEAD.md`（不提交），并在最后一条
   assistant 消息里总结做了什么、什么测试红了又是怎么改的、还有什么已知遗留问题
