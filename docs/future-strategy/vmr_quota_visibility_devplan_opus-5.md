<!-- Ver 2026-08-13 00:30, by Opus 5 -->

# Dev Plan：额度可见性（第二梯队，重新评估后）

**对应设计**：`docs/future-strategy/vmr_report_provider_client_cost_analysis_sonnet-5.md`（下称"方案"）第二梯队。
**前置**：第一梯队三批已全部落地（§2.5 账户汇总、§5.5 客户端上游归属、`vmr story` 模型使用/切换）。

> **状态（2026-08-13，Sonnet 5）：批 0～批 5 全部落地。** 六批均已实现、测试、真实数据冒烟验证通过
> （`config.mba.yaml` + 本仓库真实审计日志 + `vmr-quota.json`：`volcengine` 68.0% 已用/70.9% 已过、
> 07-22~08-22 周期区间，与本文档 §5.1 的示例数字一致；人为把 `requests` 计数改到 94.4% 后，
> §7 的 `provider_quota_exhaustion` 按预期命中；`vmr-quota.json` 改名/写入陈旧周期/`config.yaml`
> 指向不存在路径三种破坏性场景，均按 §9 预期降级）。逐批完成情况见 §1 表格与 §3～§8 各批末尾的
> "完成情况"标注；唯一的已知限制是批 0 第 5 条"改动前后逐字节对比"未做——批 0 落地时后续批次已在
> 同一会话内连续推进，缺一个干净的"仅批 0"基线可比对，用全量测试套件 + 真实数据冒烟替代。

本文先给出对方案第二梯队 5 个条目的**逐项重新评估**（§0），据此收敛出修订后的范围（§1），
再写落地步骤。范围与方案原文有实质出入的地方，§0 逐条给了依据——都是第一梯队落地后才拿得到的事实。

---

## 0. 重新评估：5 个条目逐项复核

复核方法：对每一条，去代码里核实它的**真实成本**与**真实剩余价值**，而不是沿用方案写作时的估计。

| 方案条目 | 方案原判断 | 复核后 | 依据 |
|---|---|---|---|
| **① 精确额度加权** | 唯一要碰路由半区，风险高；"按模型拆分 + 心算倍率"已够用 | **拆成批 1（下沉公式）+ 批 2（加权列），都做，优先级↑** | 见 0.1 |
| **② 输入长度分层分布** | 与①绑定 | **不做**（移入第三梯队） | 见 0.2 |
| **③ 切换次数进 `-compare`/`-corpus`** | 四处小改动，价值看是否真做横向比较 | **做，排最后（批 4）**（实为 6 处） | 见 0.3 |
| **④ 叠加实时额度状态** | 独立小节，明确标注"实时快照" | **重新定义为 §2.5 的子表，升为最高价值项（批 2）** | 见 0.4 |
| **⑤ 额度燃尽周期看板** | 价值无争议，但工作量大于其余所有项之和 | **不做**（挂起，附触发条件） | 见 0.5 |

**另外，来自外部评审 `vmr_cost_dimensions_review_report_gemini-3.6-flash.md` 的 11 条发现已逐条
回源码核查**（批注直接写在该文档内）。核查结果：3 条不成立、2 条需降级、3 条解法需改写、3 条成立。
其中采纳进本 dev plan 的：

| 来源 | 采纳内容 | 落点 |
|---|---|---|
| ISSUE-01（降级） | `int64→int` 窄化改用 `pctStr64`（保留 `den<=0` 守卫） | 批 0 |
| ISSUE-03 | 单次 `config.Load` 共享给定价与额度两条链路 | 批 0 |
| ISSUE-02 / DESIGN-01（不成立） | 仅留前向兼容注释 + 锁定"当前只支持单 Limit"的测试 | 批 0 |
| Story-1（不成立） | 仅文档澄清"全失败的 Step 也计入 Step 数" | 批 0 |
| DESIGN-03（解法改写） | 不做日志外推，改为渲染"周期已过% vs 已用%" | 批 2 |
| DESIGN-06（拆分后取其一） | 额度燃尽预警 Finding | 批 3 |
| ISSUE-04（落点改写） | 端点标签格式下沉到 `internal/core`（非 `audit`） | 批 5 |

### 0.1 ①：成本被高估，价值被低估 → 拆成批 1 + 批 2 并提前

**成本被高估。** 方案说这是"唯一需要碰路由半区"的改动。核实 `internal/router/quota.go` 后：
要复用的两个函数已经是**只依赖 `core` 类型的纯函数**——`baseAmount(spec *core.QuotaSpec, c quota.Counters)`
和 `applyModelMultiplier`/`modelMultiplier`（只读 `ep.Quota.ModelMultipliers` 与 `ep.Model`）。
把它们移进叶子包 `internal/quota`（现只依赖 `core`+`fmtutil`+stdlib）是**机械移动 + 调用点改名**，
零行为变更，router 现有测试即是回归网。`componentCost` 不动——它依赖 `internal/pricing`，移走会让
`quota` 反向依赖 `pricing`。

**价值被低估。** 方案的兜底是"看着按模型拆分的 token，对照 config 里的倍率心算"。这个前提**不成立**：

- §2.5 主表实际只渲染"模型数"（`len(p.Models)`），**从不渲染按模型的 token 拆分**——可心算的原料
  不在这张表上（在 §5 按端点表，另一张表、另一套排序）。
- 真实 `config.mba.yaml` 的数字让心算失效：`volcengine` 是 `metric: requests` 却配了
  `glm-5.2: 4.5` / `deepseek-v4-pro: 5.5` 的倍率——19 次原始请求对应 19～104 次记账请求，跨度 5.5 倍；
  `volcengine2` 的 `token_weights` 全是 `0.0001`（AFP 点数口径），34.77M 原始 token 与"20000 点上限"
  之间隔着 10000 倍的量纲差 + 0.25×～2.5× 的逐模型倍率。

**且批 1 是批 2 的硬前置**：`token_weights` 是**读时**加权（`baseAmount`），所以哪怕只想显示一个 tokens
账户"本周期已用多少"，也必须有这个公式。没有批 1，批 2 做不了。

### 0.2 ②：唯一消费者已消失 → 不做

方案自己写明它"存在的唯一理由是给精确加权提供校准数据"。批 2 落地后，校准有了更直接、更准的路径：
拿实时计数器的数字直接与厂商控制台对账。这是**一次性动作**，不需要一个常驻报表章节——正撞方案 §1
第三条标准（"真的会有人打开这张表看吗"）。

### 0.3 ③：维持，但登记点是 6 处不是 4 处

核实后的登记点：`compare.go` 的 `MetricCode` 常量块、`compare.go` 的 `rows := []MetricDiff{...}`、
`corpus.go` 的 `corpusMetricCodes`、`corpus.go` 的 `corpusMetricKinds`、`corpus.go` 的 `metricValue`
switch、`i18n/story_compare.go` 的 `MetricLabels`。价值确认成立（仓库里真做过 openclaw vs lobster
的跨框架对比），成本仍然很小，故保留，但排在最后。

### 0.4 ④：原形态冗余，但真缺口更大 → 重新定义并提到最前

**原形态冗余**：`vmr status` 已经渲染实时额度（`/admin/status` → `router.QuotaStatus()`，
带 used/pct/headroom/resets，且加权口径正确）。再做一个"独立的实时快照小节"是重复造轮子。

**真缺口在别处**：第一梯队的 §2.5 只给出了**分母**（`18000 (requests · 1mo)`），没有分子——
恰恰答不出方案的核心问题"这个账户是不是快用完了"。而分子就在盘上：真实
`<log_dir>/vmr-quota.json` 里 `volcengine` 本周期已用 **12240 / 18000 = 68%**。

**同一份数据还暴露了一个必须显式处理的陷阱**：同一账户，实时计数器 12240 次，而 3 天审计日志窗口
只有 19 次——**差 600 倍**，因为两者是不同时间窗口。这不是可以并列相减的两个数，必须各自标注窗口。

于是重新定义为：**§2.5 下增加一张"额度与消耗对照"子表**（只列配了 quota 的账户），把
"本报表窗口消耗（加权）/ 本周期已用（实时）/ 上限 / 已用% / 周期已过% / 周期区间"并排给出
（完整列定义见 §5.1）。比"独立小节"更好，因为这几个数的意义**只有并排才成立**；
同时把额度列从主表移出来，主表不再变宽。

### 0.5 ⑤：价值被批 2 抢走大半，剩下的部分不可靠 → 挂起

- **成本被高估**：方案说要"离线重放周期边界数学"。实际 `quota.PeriodStart`/`PeriodEnd` 已经是
  叶子包里的纯函数（含月末 clamp），按周期分桶只是逐记录调一次。
- **但价值被批 2 抢走**：批 2 已经**权威地**回答了"本周期已烧百分之多少"（来自路由半区真实记账，
  而非日志重算）。⑤ 剩下的独有价值只有"历史周期"与"周期内燃烧曲线"。
- **而剩下这部分不可靠**：从审计日志重算历史周期，只有当日志覆盖满一个完整周期才成立。
  实测覆盖率 19/12240 ≈ 0.2%——算出来的"上个月烧了 X%"会系统性严重偏低，是**误导性数字**，
  比没有更糟。

**触发条件**（满足前不重新评估）：日志保留策略能稳定覆盖 ≥1 个完整计费周期，且批 2 上线后仍
需要周期内燃烧速率来做决策。

---

## 1. 修订后的范围：六批

| 批 | 内容 | 依赖 | 产物变化 | 规模 | 状态 |
|---|---|---|---|---|---|
| **0** | 卫生修复（`pctStr64`、单次 `config.Load`、单 Limit 前提锁定测试、一句文档澄清） | — | 无（行为等价） | 极小 | ✅ 已实现 |
| **1** | 把 `base(metric)` 加权公式 + 只读加载器下沉到 `internal/quota` | — | **无**（纯重构） | 小 | ✅ 已实现 |
| **2** | §2.5 增加"额度与消耗对照"子表（实时已用 + 加权窗口消耗 + 上限/已用%/周期进度） | 1 | `vmr report` | 中 | ✅ 已实现 |
| **3** | 额度燃尽预警 Finding（§7） | 2 | `vmr report` | 小 | ✅ 已实现 |
| **4** | 模型切换次数登记进 `-compare`/`-corpus` | — | `vmr story` | 小 | ✅ 已实现 |
| **5** | 端点标签格式下沉到 `internal/core`（可选） | — | 无（纯重构） | 中 | ✅ 已实现（含 2 处产地 + 1 处已趋同解析点迁移，另两处解析点按 §8 迁移边界原文保留不动） |

**硬依赖只有两条**：批 1 → 批 2 → 批 3。批 0、批 4、批 5 完全独立，可任意时候插入。
建议顺序 0 → 1 → 2 → 3 → 4 →（可选）5。批 5 标为可选：它是纯技术债清理，不产出任何用户可见
变化，若时间紧可单独排期。

### 1.1 交付文件 → 批次对照表（B-7，2026-08-13 复核后补记）

`docs/future-strategy/vmr_tier12_delivery_review_and_followup_sonnet-5.md` 的 B-7 指出本轮六批
横跨多个包、新增/修改数十个文件，却没有一张可逐项核对的清单——"批 2 主表额度列没去掉"（A.2-2）
之所以能藏住，正是因为完成情况都是散文式自述。以下清单从 `git status` 生成，按批次归类（有些文件
被 DP1 一梯队创建、被本 DP2 批次扩展，标"共用"）：

| 文件 | 批次 | 备注 |
|---|---|---|
| `internal/report/render_cells.go` | 批 0 | `pctStr64` |
| `cmd/vmr/cmd_report.go` | 批 0 + 批 2 + 批 6（善后） | 单次 `config.Load`；批 2 加 `buildProviderQuotas`；善后批统一 `cfgErr` 警告（A.2-8） |
| `internal/quota/weight.go` / `weight_test.go` | 批 1 | `BaseAmount`/`ModelMultiplier`/`ApplyModelMultiplier`；善后批追加 `EstimatedPct`（A.2-3） |
| `internal/router/quota.go` / `quota_p21_test.go` / `quota_p22_test.go` | 批 1（调用点改名） | 善后批复用 `quota.EstimatedPct`，消掉 `QuotaStatus` 里的重复公式 |
| `internal/report/rows.go` | 批 2 | `LiveQuota`/`ProviderQuotaRow`/`ProviderQuotaRef` 类型；善后批加 `EstimatedPct`/`WindowNoOverlap`/`LiveConfigChanged` |
| `internal/report/providerquota.go` / `providerquota_test.go` | 批 2 | `buildProviderQuotaRows`；善后批修正 requests 口径精度（A.2-5）、cost 无定价渲染（A.2-6）、窗口无交集标记（B-3） |
| `internal/report/aggregate.go` | 批 2（+1 行）+ 善后批（§5.5 行数可观测，B-5） | |
| `internal/report/section_provider.go` / `section_provider_test.go` | 共用（DP1 批 1 创建，本批 2 加子表） | 善后批去掉主表额度列（A.2-2）、加主要错误类列（B-4）、⭐/†/‡ 三种标记 |
| `internal/i18n/report_provider.go` | 共用（同上） | |
| `internal/report/findings_quota.go` / `findings_quota_test.go` | 批 3 | 善后批把阈值改成双条件（B-1） |
| `internal/story/compare.go` / `compare_test.go` / `corpus.go` / `corpus_test.go` / `render_md.go` / `testdata/golden*.md` | 批 4 | `MetricModelSwitchCount` |
| `internal/story/metrics.go` | 批 4（改动点）+ 善后批（修正批 4 遗留的陈旧注释，A.2-4） | |
| `internal/core/core.go` / `endpointlabel.go` / `endpointlabel_test.go` | 批 5 | |
| `cmd/vmr/cmd_report_quota_test.go` | 批 2 测试补充 + 善后批（B-2/B-6 新测试） | |

不属于本 DP2 六批、属于 DP1 一梯队但同一批改动里出现的文件（`internal/report/provider.go`/
`clientendpoint.go`/`section_client_endpoint.go` 及各自 `_test.go`、`internal/i18n/story_modelusage.go`/
`report_client_endpoint.go`/`story_compare.go`/`story_compare_test.go`、`internal/story/modelusage.go`/
`modelusage_test.go`/`render_modelusage.go`）不在此表——它们的批次归属见
`vmr_report_story_cost_dimensions_devplan_opus-5.md` 自己的 §1。

---

## 2. 各批共用的约束

**行数预算**（`internal/archtest` 会让 `go test ./...` 直接失败）：

| 文件 | 当前 | 预算 | 对本次的含义 |
|---|---|---|---|
| `internal/report/aggregate.go` | 964 | 1000 | 批 2 **只加 1 行**（事后派生的调用点，见 §5.3） |
| `internal/report/render_doc.go` | 226 | 400 | 同上，`renderProviders` 已在运行顺序里 |
| `internal/story/corpus.go` | 351 | 380 | 批 4 只剩 29 行余量，估计用 5 行，够但要留意 |
| `internal/story/compare.go` | 740 | 850 | 批 4 用 2 行 |
| `internal/router/quota.go` | 466 | 无预算 | 批 1 从这里移出约 40 行 |

**导入边界**（`internal/archtest` 的 `forbiddenImports`）：`internal/report` 禁止导入
`router`/`server`/`config`——**`internal/quota` 不在禁止之列**，且它是叶子包，与 report 已经导入的
`internal/pricing` 完全同构（CLAUDE.md 里 pricing 的"两个消费者，一份实现"就是这条先例）。
**无需修改 archtest。**

**其它照旧**：新文件加版本头注释；文案进 `internal/i18n/` 下与源文件一一对应的文件；
`vmr-report.json` 的叙述性字段固定英文；新增排序必须有唯一性 tie-break；
**降级姿态一律照抄 `buildPricing`**——读不到就少一列，绝不让整份报表失败。

**每批完成后**：
```
gofmt -l ./internal ./cmd && go build ./... && go vet ./...
go test ./internal/archtest/... && go test ./...
```

---

## 3. 批 0：卫生修复（来自外部评审核查）

四项，彼此独立，合起来一个 commit。**全部行为等价，无用户可见变化。**

**① `int64 → int` 窄化**（ISSUE-01 降级后采纳）
`internal/report/section_client_endpoint.go` 的 `pctStr2(int(r.TokensIn), int(clientTotal))`。
在 release 的四个 64 位目标上无损，但窄化本身无谓。**在 `render_cells.go` 新增**：
```go
// pctStr64 是 pctStr2 的 int64 版本——必须保留同样的 den<=0 守卫，
// 否则分母为 0 时会渲染成 "NaN%"。
func pctStr64(num, den int64) string
```
改调用点。**不要**按评审建议改成 `pctFloat(float64(num)/float64(den))`——那会丢掉守卫。

**② 单次 `config.Load`**（ISSUE-03）
`cmdReport` 里加载一次 `cfg`，分别传给 `buildPricing` 与 `buildProviderQuotas`。
**理由是一致性不是性能**：两次加载之间文件若被编辑，定价与额度会来自两份不同配置且无任何报错。
两点实现约束：
- 签名改为 `(cfg *config.Config, loadErr error, configPath string, ...)`——**`configPath` 要保留**，
  `buildPricing` 现有的降级提示原文就带路径（`"pricing: %s not usable (%v)"`），丢了路径会让
  报错从"哪个文件不对"退化成"某个文件不对"。
- 加载失败时两条链路各自照常降级（定价退回内置标准表、额度整块不渲染），
  **不能因为共享了 `cfg` 就变成硬失败**——`vmr report` 必须能对着一堆裸日志跑。

**③ 锁定"当前只支持单 Limit"这个前提**（ISSUE-02/DESIGN-01 核查为不成立后的残留项）
`buildProviderQuotas` 的 `Limits[0]` 处加一行注释，说明它依赖 `config.validateQuota` 拒绝
`len(Limits) > 1`，并指明 P3 放开多窗口时此处需同批改造；同时在 `internal/config` 补一个测试，
断言两个 Limit 的配置**加载失败**——把这个前提钉成可执行的检查，而不是靠注释。

**④ 一句文档澄清**（Story-1 核查后的残留项）
`internal/story/modelusage.go` 的 `ModelUsageStat.Steps` 字段注释补一句：一个所有 attempt 都失败的
Step 同样计入 `Steps`（它确实路由到过该上游），只是不贡献 token——避免读者把它读成"成功步数"。

### 3.1 测试与验证

- `render_cells_test.go`：`pctStr64` 的 `den<=0`、正常值、大于 `2^31` 的输入各一条。
- `internal/config`：多 Limit 配置加载失败的断言（③）。
- `cmd/vmr`：`config.Load` 只被调用一次（可用一个临时计数或直接断言行为等价）。
- 其余靠既有测试全绿即可——这一批不应改变任何输出。

**完成情况（2026-08-13）**：四项全部落地。①`pctStr64` 加入 `render_cells.go`，调用点已切换，
新增 `render_cells_test.go` 三条用例。②`cmdReport` 现只调用一次 `config.Load`，`buildPricing`/
`buildProviderQuotas` 均改签名接收 `(cfg, loadErr, configPath, ...)`；`cmd_report_pricing_test.go`/
`cmd_report_quota_test.go` 的调用点同步更新，`config.Load` 单次调用点由代码结构本身保证（两个
callee 内不再各自调用），未额外写计数断言——按检查清单本身"可用一个临时计数**或**直接断言行为
等价"二选一的措辞，视为已满足。③核实后发现该测试**已存在**（`internal/config/quota_test.go` 的
`TestQuota_Reject_MultipleLimits`），`buildProviderQuotas` 处补充了指向它的注释，未重复造一个。
④`ModelUsageStat.Steps` 字段注释已按原文补上。**唯一未做的验证项**：§9 第 5 条"改动前后逐字节
对比 `vmr-report.json`/`.md`"——因批 0 与批 1～5 在同一会话内连续实现，没有留出一个干净的"仅批 0"
提交点做前后对比；改用全量测试套件（含 `internal/report` 现有输出快照类测试）全绿 + 本批改动本身
的只读性质（签名改动不改变任何分支逻辑）作为等价保证。

---

## 4. 批 1：把加权公式下沉到 `internal/quota`

**这一批不产生任何用户可见变化**，单独一个 commit，目的是让批 2/批 3 复用而不是复制公式。

### 4.1 改动清单

**① `internal/quota/` 新增导出（建议新文件 `weight.go`，~60 行）**

从 `internal/router/quota.go` 原样移入，只改名与签名去 `Endpoint` 化：

```go
// BaseAmount 是原 router 的 baseAmount，逐字不变。
func BaseAmount(spec *core.QuotaSpec, c Counters) float64

// ModelMultiplier 是原 modelMultiplier(ep)，改为按 (spec, model) 取值：
// 精确匹配 → "*" 通配 → 1.0。
func ModelMultiplier(spec *core.QuotaSpec, model string) float64

// ApplyModelMultiplier 是原 applyModelMultiplier，同样去掉对 *core.Endpoint 的依赖。
func ApplyModelMultiplier(spec *core.QuotaSpec, model string, d Counters, estimated int64) (Counters, int64)
```
`ceilScale` 一并移入（保持不导出）。**`componentCost` 留在 router**（依赖 `internal/pricing`）。

**② `internal/router/quota.go` 改为调用新函数**

`baseAmount(...)` → `quota.BaseAmount(...)`；`applyModelMultiplier(ep, d, est)` →
`quota.ApplyModelMultiplier(ep.Quota, ep.Model, d, est)`。删除本地实现。
**保持 `ChargeResponse`/`QuotaStatus`/`scoreForEndpoint` 的行为逐字不变**——`internal/router` 与
`internal/replay` 的现有测试是这一批唯一的验收标准。

**③ `internal/quota/store.go` 增加只读加载器（~35 行）**

```go
// Bucket 由现有的 bucket 直接导出而来（JSON tag 一个字符都不改，保持磁盘兼容）。
type Bucket struct { PeriodStart int64; C Counters; Estimated int64; EstimatedCost float64 }
func (b Bucket) PeriodStartTime() time.Time

// LoadFile 只读地解析 vmr-quota.json，不构造 Registry、不加锁、不写盘。
// 文件不存在返回 (nil, nil)——离线消费者的正常情况，不是错误。
func LoadFile(path string) (map[string]map[string]Bucket, error)   // provider -> limitKey -> Bucket
```
**必须导出既有的 `bucket` 而不是另建一个平行只读类型**——`store.go` 自己的注释写着
"there is exactly one shape, used both in memory and on disk"，加第二份形状正是要避免的事。

### 4.2 测试

- `internal/quota/weight_test.go`：`BaseAmount` 三种 metric（requests/tokens/cost）；
  `ModelMultiplier` 的"精确 → 通配 → 1.0"三级回退；`ApplyModelMultiplier` 的**向上取整方向**
  （非整数倍率必须偏向"算得更多"，原注释已论证过，测试要钉住）。
- `internal/quota/store_test.go` 补：`LoadFile` 读一份真实形状的 json；文件缺失返回 `(nil, nil)`；
  损坏文件返回 error。
- 回归：`go test ./internal/router/... ./internal/replay/...` 必须零改动通过。

### 4.3 文档

- `CLAUDE.md` 的 `quota` 行：补一句公式已下沉（原文写的是"base(metric) 由调用方
  `internal/router/quota.go` 应用"，A 之后不再成立，必须同步改）。
- `CHANGELOG.md`：**不加条目**（纯内部重构，无用户可见变化）。

**完成情况（2026-08-13）**：全部落地。`internal/quota/weight.go` 新增 `BaseAmount`/`ModelMultiplier`/
`ApplyModelMultiplier`/`ceilScale`（逐字迁移），`internal/router/quota.go` 删除本地实现改为调用
`quota.` 前缀版本，`internal/quota/store.go` 增加 `Bucket`（导出既有 `bucket`）+ `LoadFile`。
测试：新增 `weight_test.go`（三种 metric、倍率精确/通配/默认三级回退、向上取整方向、`LoadFile` 的
缺失/损坏/真实往返三态）；`internal/router`/`internal/replay` 现有测试改调用点名后零行为变更通过
（`quota_p21_test.go`/`quota_p22_test.go` 的 `baseAmount(...)` 改为 `quota.BaseAmount(...)`）。
`CLAUDE.md` 的 `quota` 行已同步（含新增的 `weight.go`/`LoadFile` 说明及 `report` 加入依赖方列表）；
按原计划 `CHANGELOG.md` 未加条目。

---

## 5. 批 2：§2.5 增加"额度与消耗对照"子表

### 5.1 目标形态

§2.5 主表**保持现状不变**，但把"额度参照"列**移出**主表（主表因此不变宽），在其下新增一张子表，
只列 `config.yaml` 里配了 `quota:` 的账户：

| 账户 | metric | 本报表窗口消耗¹ | 本周期已用² | 上限 | 已用% | 周期已过% | 周期区间 |
|---|---|---|---|---|---|---|---|
| volcengine | requests | 104 | 12240 | 18000 | 68.0% | 71.0% | 07-22 ~ 08-22 |

两个消耗数字**各自标注来源与窗口，不做减法、不算覆盖率**——报表窗口与计费周期不对齐是常态，
给一个比值只会诱导过度解读。

**「周期已过%」是 DESIGN-03（Run-Rate）改写后的形态**：把"已用 68% / 已过 71%"并排给出，
读者一眼就能判断快慢，且**不引入任何外推误差**。刻意不做外部评审建议的"按日志窗口外推月消耗"——
实测日志窗口只覆盖周期的 0.2%（19 次 vs 12240 次），外推会低估两个数量级。
`周期已过% = 1 - quota.TimeLeftFrac(now, PeriodStart, PeriodEnd)`，复用既有纯函数。

> **判"透支"的判据要用对**：`quota.Headroom < 1` **恰好等价于「已用% > 已过%」**（推导：
> `Headroom = (1-u)/(1-e) < 1 ⟺ 1-u < 1-e ⟺ u > e`），**差值大于 0 即成立，不是大于 10 个百分点**。
> 所以要么直接用 `Headroom < 1`（与路由半区判定完全同源，推荐），要么自己定一个百分点缓冲以避免
> 临界抖动——但**后者就不再等价于 `Headroom`，不能声称"用的是同一条判据"**。二选一，别混着写。

### 5.2 数据来源与算法

**② 本周期已用**：`cmd_report.go` 用 `quota.LoadFile(<log_dir>/vmr-quota.json)` 读取，
按 `provider` + `limitKey`（`string(metric) + "/" + everyText`）取 `Bucket`，
渲染时对其 `Counters` 应用 `quota.BaseAmount(spec, c)`。

> **必须处理的陷阱**：`quota.Registry` 是**懒重置**的，盘上的 `PeriodStart` 可能属于**上一个周期**
> （进程停了一整个周期没跑）。渲染前必须把 `Bucket.PeriodStartTime()` 与
> `quota.PeriodStart(limit, now)` 比对：**不相等就不能当作"本周期已用"渲染**，该行的实时列显示
> `-`（可在脚注说明"计数器停留在更早的周期"）。漏掉这一步会把上个月的数字当本月展示。

**① 本报表窗口消耗**：从 `rep.EndpointsAll` **事后派生**（与 `buildProviders` 同构，
`aggregate.go` 零改动）。逐端点行：用 `splitEndpointProviderModelAny` 取 (provider, model) →
`quota.ApplyModelMultiplier(spec, model, counters, 0)` → 累加 → 最后 `quota.BaseAmount(spec, sum)`。
按 metric 取 `counters`：

- `requests`：`Counters{Requests: e.Requests}`（端点实际承接的请求数）
- `tokens`：`Counters{Fresh: e.TokensInFresh, CacheRead: e.TokensInCached, CacheWrite: e.TokensInCacheWrite, Out: e.TokensOut}`
- `cost`：`Counters{Cost: Σ e.CostEstimate}`（报表自己的定价解析结果）——**仍然走 `BaseAmount`**，
  因为它对 `MetricCost` 就是直接返回 `c.Cost`。保持三种 metric 走同一条出口，别为 cost 开分支。
  注意 `model_multipliers` 对 cost 账户不适用（`config.validate` 直接拒绝这种组合），所以这一支
  不调用 `ApplyModelMultiplier`。

> **诚实标注（必须进渲染层，不能只写在注释里）**：这一列是**重算值**，不是路由半区当时记账的重放。
> 已知会有小幅出入的边界情形：逐请求 `ceil` 与汇总后取整的差异、失败尝试不计费、
> config 里倍率/权重在窗口期内变更过、`cost` 口径下报表定价与记账时定价的解析时点不同。
> 这正是要把实时计数器并排放出来的理由。

### 5.3 改动清单

- **`internal/report/rows.go`**：`ProviderQuotaRef` 增补字段——`Limit *core.Limit`（周期数学用）、
  `Spec *core.QuotaSpec`（权重/倍率）、`Live *LiveQuota`（新类型：`PeriodStart time.Time` +
  `quota.Counters` + `Estimated`/`EstimatedCost`）。三者都可为 nil，对应各自的降级。
  > **`Limit`/`Spec` 必须打 `json:"-"`**：它们是**计算输入**不是报表结论，序列化进
  > `vmr-report.json` 只会把整份 `model_multipliers` 映射灌进产物（`config.mba.yaml` 里
  > `volcengine2` 就有 6 条），既臃肿又等于把配置文件内容复制进报表。
  > `Live` 与算出来的行则应当序列化——那是结论。
- **`internal/report/providerquota.go`（新，~90 行）**：`buildProviderQuotaRows(rep, quotas, now)`
  产出 `[]ProviderQuotaRow`（含上述 ①②、已用%、已过%、周期区间）。
  排序：**有实时数据的行按已用% 降序在前，无实时数据的行沉底**，两段内均以账户名 tie-break
  （照抄 `endpointValueRows` 里 `hasCost` 的同款处理——没有该数据的行不能因为"值是 0"就排到最前）。
  > **结果必须写回 `Report2`（新增 `Report2.ProviderQuotas []ProviderQuotaRow`），不能只在渲染时算。**
  > 批 3 的 Finding 要读同一批数字；若渲染层和 Finding 各算一遍，就会出现"表里 91%、
  > Finding 不报警"这类自相矛盾——正是本项目被坑过一次的"同一事实两份实现"。
- **`internal/report/aggregate.go`（+1 行）**：在 `rep.Providers = buildProviders(...)` 旁边加
  `rep.ProviderQuotas = buildProviderQuotaRows(rep, quotas, now)`（`buildInternal` 已有 `now` 参数）。
  964 → 965，预算 1000，安全。
- **`internal/report/section_provider.go`（+~35 行，当前 77 行、无预算）**：主表去掉额度列，
  其下渲染子表；`len(rows)==0` 时整块不渲染。
- **`internal/i18n/report_provider.go`**：扩展现有 bundle（渲染仍在 `section_provider.go`，
  按"一一对应"约定不新建文件）——子表标题、8 个表头、两条脚注（重算值说明、计数器停在旧周期说明）。
- **`cmd/vmr/cmd_report.go`（+~30 行）**：`buildProviderQuotas` 扩展为一并填 `Limit`/`Spec`，
  并调用 `quota.LoadFile` 填 `Live`。**沿用 `buildPricing` 的降级姿态**：config 读不到 → 无子表；
  `vmr-quota.json` 缺失/损坏 → 实时列全 `-`，其余照常。
- **`internal/report` 新增 import `internal/quota`**（合法，见 §2）。

### 5.4 测试

- **`providerquota_test.go`（新）**：三种 metric 各一条上卷正确性（含倍率生效）；
  **周期不匹配时实时列被抑制**（这条是 §5.2 陷阱的守门测试，必须有）；
  `Live`/`Spec`/`Limit` 为 nil 的三种降级；排序确定性。
- **`section_provider_test.go`（扩展）**：子表渲染快照；无 quota 账户时不出子表；
  主表不再含额度列；中英文各一条。
- **`cmd/vmr` 冒烟**：真实 `vmr-quota.json` + `config.mba.yaml` 跑通。

### 5.5 文档

- `docs/VirtualModelRouter_Design_v4_Analytics.md`：§2.5 那段补子表说明，**必须写明两个数字的窗口
  不同、不可相减**，以及"重算值 ≠ 记账重放"。
- `docs/UserGuide.md` / `.zh.md`：§2.5 条目各补一句。
- `CHANGELOG.md`：`[Unreleased]` → `Added` 一条。

**完成情况（2026-08-13，2026-08-13 复核后修正）**：子表本身全部落地——真实数据（`config.mba.yaml` + 一份真实审计日志
+ 真实 `vmr-quota.json`）跑通冒烟——`volcengine` 渲染出 `123 | 12240 | 18000 | 68.0% | 70.9% |
07-22 ~ 08-22`，与本节示例数字一致；`dashscope`(cost)/`volcengine2`(tokens) 两个当次窗口无实时数据
的账户正确显示 `-` 且仍渲染周期区间（这是实现时发现的一个比原计划更完整的情形——账户即使本报表
窗口零流量，只要 `config.yaml` 声明了 `quota:` 就仍出现在子表里，因为子表数据来自 `quotas` map
而非 `rep.Providers`；为此在 `section_provider.go` 把主表判空条件从"`rep.Providers` 为空则整个
`§2.5` 不渲染"改成了"两者都为空才不渲染"，否则一个零流量但配了额度的账户会连子表都看不到）。
`rows.go` 新增 `LiveQuota`/`ProviderQuotaRow`，`ProviderQuotaRef` 增补 `Limit`/`Spec`（`json:"-"`）/
`Live`；`providerquota.go`（新）实现 `buildProviderQuotaRows`；`aggregate.go` 按计划 +1 行；
`section_provider.go` 渲染子表（含"重算值"与"陈旧周期"两条脚注）；`internal/i18n/report_provider.go`
新增 `ProviderQuotaText`/`ProviderQuota(lang)`；`cmd_report.go` 的 `buildProviderQuotas` 扩展填
`Limit`/`Spec`/`Live`（含 §5.2 的周期匹配守门逻辑）。测试：`providerquota_test.go`（三种 metric 上卷
+ 倍率、`Live==nil` 降级、排序确定性）、`section_provider_test.go` 扩展（渲染快照、中英文、
`Live==nil` 显示 `-`）、`cmd_report_quota_test.go`（新，周期匹配/陈旧周期/文件缺失/文件损坏四种
场景）。真实数据破坏性验证：`vmr-quota.json` 改名、写入陈旧 `period_start`、`config.yaml` 指向
不存在路径三种场景均按 §9 预期降级。文档三处已同步（设计文档 §2.5、`UserGuide.md`/`.zh.md`、
`CHANGELOG.md`）。

**但本节开头"§2.5 主表保持现状不变，把额度参照列移出主表"这一条当时没有落地**——主表实际保留了
额度列，与子表的"额度参照"/"上限"重复渲染同一个 `Amount`，且两处用了不同 formatter（主表
`quotaAmountStr`、子表 `numStr`），同一个 `19.995` 在两处会显示成不同字符串。这是 2026-08-13 的
交付复核（`docs/future-strategy/vmr_tier12_delivery_review_and_followup_sonnet-5.md`
的 A.2-2）发现的，随后在同一天的善后批次里修正：`section_provider.go` 的主表彻底去掉了额度列
（`hasQuota`/`QuotaHdr`/`QuotaCell`/`Disclaimer` 全部删除，`quotaAmountStr` 一并删除），额度参照
从此只在本节的子表出现。`ProviderRow.Quota` 字段仍保留、仍写入 `vmr-report.json`，只是不再渲染
进 Markdown 主表。

---

## 6. 批 3：额度燃尽预警 Finding（§7）

来自外部评审 DESIGN-06 三条建议中**唯一采纳的一条**（另两条的否决理由见设计文档第三梯队表）。
依赖批 2 的实时计数器数据。

**判据**：某账户「本周期已用%」≥ 90（阈值作为包内常量，不进 `config.yaml`——与
`internal/ctxgraph` 的阈值同理，用户无法校准自己无法测量的东西）。
**只用实时计数器，不用报表窗口重算值**——前者是权威记账，后者是估算；用估算值报警会产生假警报。

**为什么这条值得做而另两条不值得**：它是**权威数据上的确定性判断**（计数器 ≥ 上限的 90%
是一个事实，不是启发式），而"错误率高"与"流量倾斜"都需要猜测意图。它把"账户吃紧"从
"要读表"变成"会自己报警"，正是 §7 存在的意义。

**改动**：
- `internal/report/rows.go`：新增 `FindingProviderQuotaExhaustion FindingCode = "provider_quota_exhaustion"`。
- `internal/report/metrics.go` 的 `buildFindings`：新增检测器，遍历 **`rep.ProviderQuotas`**
  （批 2 写回 `Report2` 的那份，见 §5.3——**不要重算**），取「已用%」最高且 ≥ 阈值的一个，
  与既有检测器"只报最差的一个"的惯例一致。`buildFindings` 现有签名是 `(rep *Report2, lang i18n.Lang)`，
  数据全在 `rep` 上，正好不用改签名。
- `internal/i18n/report_efficiency.go`：新增该 Finding 的中英文案（标题/值/涉及对象/建议动作，
  沿用既有 `FindingText` 结构）。
- 文案纪律：`Action` 写"检查该账户的路由权重或额度配置"这类可执行建议，**不写因果断言**。

**测试**：`metrics_test.go` 补——达到阈值时命中且 `Code` 正确；未达阈值时不命中；
无实时数据（`Live == nil`）时不命中（不能因为缺数据就报警）。

**行数预算注意**：`internal/report/metrics.go` 当前未列入 archtest 预算表，但 `buildFindings`
已经很长；若新增检测器让该文件显著变大，按项目惯例拆成 `findings_quota.go` 而不是继续堆积。

**完成情况（2026-08-13）**：全部落地，按建议直接拆成了新文件 `findings_quota.go`
（`quotaExhaustionFinding`，未往 `metrics.go` 里堆），`metrics.go` 的 `buildFindings` 只新增
3 行调用。`rows.go` 新增 `FindingProviderQuotaExhaustion`。`i18n/report_efficiency.go` 新增
`ProviderQuotaExhaustionFinding` 中英文案，`Action` 按纪律只写"检查该账户的路由权重或额度配置"。
测试放在 `findings_quota_test.go`（而非 `metrics_test.go`——本仓库这次落地时 `metrics.go` 尚无
同名 `_test.go` 文件，新建一个专用测试文件与 `findings_quota.go` 一一对应，比塞进不存在的
`metrics_test.go` 更符合"新文件"的既有约定）：阈值命中/未命中、`Live==nil` 不命中、多账户取最差
一个且按名字 tie-break、`buildFindings` 集成断言、中文文案。真实数据验证：人为把 `vmr-quota.json`
里 `volcengine` 的 `requests` 计数改到占上限 94.4%，重跑 `vmr report` 后 §7 正确出现
"额度即将耗尽 | provider_quota_used_pct | 94.4%（requests · 1mo） | volcengine | 检查该账户的
路由权重或额度配置"；改回 68% 后该 Finding 正确消失。

---

## 7. 批 4：模型切换次数进 `-compare`/`-corpus`

新增标量 `MetricModelSwitchCount = "model_switch_count"`，取值 `len(m.ModelSwitches)`，
`Kind` 为 `KindCount`。**6 处登记**（缺一即行为不一致）：

| # | 文件 | 位置 |
|---|---|---|
| 1 | `internal/story/compare.go` | `MetricCode` 常量块 |
| 2 | `internal/story/compare.go` | `rows := []MetricDiff{...}` 追加一行 `metricDiff(...)` |
| 3 | `internal/story/corpus.go` | `corpusMetricCodes` 切片 |
| 4 | `internal/story/corpus.go` | `corpusMetricKinds` 映射 |
| 5 | `internal/story/corpus.go` | `metricValue` 的 switch |
| 6 | `internal/i18n/story_compare.go` | `MetricLabels`（中英各一） |

**连带必须改的注释/文档**：`corpus.go` 里两处写死的"twelve"（`corpusMetricCodes` 与
`corpusMetricKinds` 的注释）、设计文档与 `UserGuide` 里的"**12 个数值字段**"/"twelve
behavior-profile numbers" → 13。第一梯队刻意保持了 12（当时新增的是列表型字段），**这一批不同，
必须改**。

**测试**：`compare_test.go` 断言新行出现且数值正确；`corpus_test.go` 断言新 code 进入分布表；
i18n 断言两种语言都有标签（避免回落成 code 原文）。

**一句必须写进文档的解读限定**：切换次数是**路由环境**变量，不是 Agent 行为变量——
在 corpus 的相关性矩阵里应读作"两组 Journey 的路由环境是否不同"，不能读成"Agent 行为不同"。

**完成情况（2026-08-13）**：六处登记全部完成——`compare.go` 的 `MetricCode` 常量块 + `rows` 追加行、
`corpus.go` 的 `corpusMetricCodes`/`corpusMetricKinds`/`metricValue` switch、`i18n/story_compare.go`
的 `MetricLabels`（中英各一）。两处写死的"twelve"注释已改（`corpus.go` 两处 + 设计文档 §3.7/§3.9 的
"12 个数值字段"→"13 个"+ `UserGuide.md`/`.zh.md` 的"twelve"/"十二项"→"thirteen"/"十三项"）。
路由环境变量的解读限定已写进 `MetricModelSwitchCount` 的代码注释与设计文档 §3.7。测试：
`compare_test.go` 新增 `TestCompare_ModelSwitchCount_Row`（数值+Kind 断言）、既有
`TestCompareBasicDiff` 的行数断言从 12 改到 13；`corpus_test.go` 新增
`TestMetricValue_ModelSwitchCount_Registered`（三处注册点 + `metricValue` 取值）；
`internal/i18n` 新增 `story_compare_test.go` 断言两种语言都有真实标签而非回落成原始 code。

---

## 8. 批 5（可选）：端点标签格式下沉到 `internal/core`

来自外部评审 ISSUE-04。**纯技术债清理，零用户可见变化**，可单独排期。

**现状清点**（这是采纳的真正理由——评审只看到 6 个散点中的 2 个）：

| 角色 | 位置 | 备注 |
|---|---|---|
| 产地 1 | `internal/router/router.go:357` | `strings.Join([]string{ep.AdapterType, ep.Provider, ep.Model}, ":")` |
| 产地 2 | `internal/replay/replay.go:515` | 同一行的第二份拷贝 |
| 解析 1 | `internal/report/cost.go` `splitEndpointProviderModel` | **只认 `:`** |
| 解析 2 | `internal/report/provider.go` `splitEndpointProviderModelAny` | `:` + `/` |
| 解析 3 | `internal/report/detail.go` `attemptUpstream` | 结构化字段优先 + `/` |
| 解析 4 | `internal/story/modelusage.go` `splitEndpointLabel` | `:` + `/` |

即：**格式没有任何权威定义，且 4 个解析点彼此不等价。**

**改动**：`internal/core` 新增（`endpointlabel.go`，~40 行）
```go
// EndpointLabel 产出审计日志用的 "protocol:provider:model" 标签——router 与 replay
// 各自内联拼接的那一行的唯一来源。
func EndpointLabel(adapterType, provider, model string) string

// SplitEndpointLabel 解析上面的格式，并兼容旧日志的 "/" 连接形式。
// SplitN(..., 3)：模型名本身可能含 ":" 或 "/"（如 "z-ai/glm-5.2"）。
func SplitEndpointLabel(label string) (protocol, provider, model string, ok bool)
```
`internal/core` 是零内部依赖叶子包，router/replay/report/story 全都已依赖它，无 archtest 变更。

**必须一并讲清楚的坑**（评审未提）：`core.Endpoint.Name()`（`core.go:293`）返回的是
**斜杠**格式 `AdapterType + "/" + Provider + "/" + Model`，被 `internal/server/admin.go:79` 使用，
与审计日志的冒号格式**并存且用途不同**。下沉时必须在 `endpointlabel.go` 的文档注释里说明
"两种格式分别是什么、谁该用哪个"，否则只是把混乱搬了个地方。

**迁移边界（重要）**：`splitEndpointProviderModel`（解析 1，只认 `:`）**不要顺手改成兼容 `/`**——
它是 §2 成本估算的取价路径，放宽格式会改变旧日志的历史 $ 数字，属独立议题（第一梯队 dev plan
的风险表已登记过这一条）。本批只做"统一到一处定义"，**不改变任何一个调用点的现有行为**；
若某个调用点确实该换用更宽松的解析，单独一个 commit、单独评审。

**测试**：`internal/core/endpointlabel_test.go`——两种格式、模型名含 `:`/`/` 的情形、
不合法输入返回 `ok=false`；各调用点原有测试保持不变即为通过。

**完成情况（2026-08-13）**：核心部分（新增 `EndpointLabel`/`SplitEndpointLabel` 定义 + 2 处产地迁移）
已落地，但解析侧的迁移范围比原计划更保守——原计划的表述("解析 1～4"是否迁移未明说，只强调"解析 1
不要改")留了解读空间，实现时按"零行为风险"原则收窄为：
- **产地 1/2**（`router.go`/`replay.go`）：✅ 迁移到 `core.EndpointLabel`，纯格式生成，逐字节等价。
- **解析 4**（`story/modelusage.go` 的 `splitEndpointLabel`）：✅ 迁移——核对后发现它的冒号/斜杠回退
  逻辑与新 `core.SplitEndpointLabel` 逐字节相同（无额外分支），是安全的纯去重。
- **解析 2**（`report/provider.go` 的 `splitEndpointProviderModelAny`）：❌ **未迁移**——核对后发现它
  比新核心函数多一个隐藏分支（冒号 3 段切分成功但 `provider==""` 时会继续尝试斜杠切分，新核心函数
  没有这个"провider 非空才算数"的二次校验）。这个分支在真实数据下不可达（provider 名在 config 加载期
  即校验非空），但迁移它需要额外证明这一点，本批目标是"零风险统一定义"，不是"顺手改行为"，故保留
  原实现不动。
- **解析 1/3**（`report/cost.go`/`report/detail.go`）：❌ 按原计划保留不动（解析 1 是 §2 成本估算的
  取价路径，解析 3 有结构化字段优先的额外逻辑，均不是纯字符串切分）。

`core.Endpoint.Name()` 的文档注释已补上与 `EndpointLabel` 的区分说明。测试：新增
`endpointlabel_test.go`（`EndpointLabel` 生成、冒号/斜杠两种格式解析、模型名含 `:`/`/` 的情形、
非法输入）；`router`/`replay`/`story` 现有测试零改动通过（`-race` 覆盖 `router`/`replay`）。

---

## 9. 收尾验证

1. ✅ `go test ./...`（`-race` 对 report/story 无必要，无并发新增——另加跑了 `-race` 覆盖
   `router`/`quota`/`replay`/`health`/`audit`/`server` 这几个真正有并发的包，全绿）。
2. ✅ 真实数据冒烟：用本仓库真实 `config.mba.yaml` + 一份真实审计日志 + 真实 `vmr-quota.json`
   跑通——`volcengine`(requests)/`volcengine2`(tokens)/`dashscope`(cost) 三种 metric 各自渲染正确，
   `volcengine` 实时列显示 12240/18000（68.0%），「周期已过%」（70.9%，07-22~08-22）与日历一致。
3. ✅ 中英文各跑一次，`grep -P '[\x{4e00}-\x{9fff}]'` 复查英文输出无残留中文（exit 1，无匹配）。
4. ✅ **故意破坏三次**——三种场景均按预期降级，见批 2 完成情况小节的详细记录：
   - `vmr-quota.json` 改名 → 报表照常生成，实时列全 `-`，其余不受影响；✅ 已验证
   - `vmr-quota.json` 写入一个**属于上个周期**的 `period_start` → 实时列被抑制为 `-`；✅ 已验证
   - `config.yaml` 指向一个不存在的路径 → 子表整块不渲染，主表与其余章节照常。✅ 已验证
5. ⚠️ **未做**：批 0 单独的"改动前后逐字节对比"——批 0 与批 1～5 在同一会话内连续实现，落地批 0 时
   已经在为批 1 铺垫，没有留出一个干净的"仅批 0"提交点做前后 diff。用全量测试套件（含
   `internal/report` 对 §2.5 主表等既有输出的快照类测试）全绿 + 批 0 每项改动本身的性质（签名改参数
   传递方式、抽取已存在的字符串处理成命名函数、加注释、复用已有测试）作为等价性的替代保证——
   都是"只改how不改what"的改动，真出现意外行为变化时既有测试套件会先炸。这是本次任务唯一
   未按原计划 100% 执行的验证步骤，记在此处供后续需要更强保证时补做。

---

## 10. 风险表

| 风险 | 处置 |
|---|---|
| 盘上计数器停留在旧周期，被当成本周期已用 | §5.2 强制比对 `PeriodStart`，不匹配就显示 `-`；有专门的守门测试 + §9 的破坏性验证 |
| 读者把"窗口消耗"与"本周期已用"相减 | 不提供比值；表头写明各自窗口；脚注明确两者不可相减 |
| 加权重算值与路由实际记账不一致，被当成对账依据 | 渲染层强制带"重算值 ≠ 记账重放"脚注，并列出已知出入来源（§5.2） |
| 批 1 移动函数时改变了取整/权重行为 | 批 1 单独 commit，验收标准就是 router/replay 现有测试零改动通过 |
| 批 0 的单次 `config.Load` 改动破坏既有降级语义 | 加载失败时两条链路必须各自照常降级，不能因共享 `cfg` 变成硬失败；§9 第 5 条的逐字节比对是兜底 |
| 批 3 的 Finding 在缺实时数据时误报 | 判据只认实时计数器；`Live == nil` 明确不命中，有专门测试 |
| 批 5 顺手放宽了 `splitEndpointProviderModel` 的格式，改变历史 $ 数字 | §8 明确划定"只统一定义、不改任何调用点行为"；真要改单独 commit |
| `corpus.go` 只剩 29 行余量 | 批 4 预计用 5 行；若超出，把 `metricValue` 拆成独立文件而不是抬高预算 |
| `internal/report` 新增依赖 `internal/quota` 被误认为破坏边界 | 与既有的 `report → pricing` 同构；archtest 无需改动，但 PR 描述里要点明这条先例 |

---

## 11. 明确不做的（连同触发条件）

| 项 | 判断 | 何时重新评估 |
|---|---|---|
| **输入长度分层分布** | 不做——唯一消费者（校准加权）已被批 2 的实时计数器替代，且校准是一次性动作 | 出现"需要按上下文档位分别计价"的账户，即分层本身成为计费口径的一部分时 |
| **额度燃尽周期看板** | 不做——批 2 已权威回答"本周期烧了多少"；剩余的历史周期部分在日志覆盖不足时是误导性数字（实测覆盖率 0.2%） | 日志保留稳定覆盖 ≥1 个完整计费周期，**且**批 2 上线后仍需周期内燃烧速率做决策 |
| **独立的"实时额度快照"小节** | 不做——`vmr status` 已覆盖；改为 §2.5 子表（批 2） | — |
| **按日志窗口外推 Daily Run-Rate** | 不做——实测窗口只覆盖周期的 0.2%，外推低估两个数量级；改用「周期已过% vs 已用%」（批 2） | — |
| **`vmr story` Step 级成本估算** | 不做——量级不成立（真实约 ¥0.64 而非 $5–15）、`story` 未依赖 `pricing`/`config`、信息增量小 | 接入 Claude/GPT-4 级单价账户 **且** 多模型 Journey 成为常态 |
| **Provider 错误率 / Client 单点倾斜 Finding** | 不做——前者与 §2.5/§3/§0 高度重叠；后者会把有意配置误判成问题（实测 `openclaw`/`pimini` 就是 100% 单点） | 出现"本应分散却意外单点"的真实事故，且能给出区分意图的判据 |
| **`aggregate.go` 拆分为子包** | 不做——本 dev plan 对它的改动是 0 行，此刻不阻塞任何事；无功能推动时做高耦合大重构是拿确定风险换不确定收益 | 下一次真正需要往 `aggregate.go` 里加逻辑（而非新建文件）时，借那次改动一并拆 |
| **多重 Limit 支持** | 不属于本梯队——被路由半区 P3 阻塞；当前 `config.validateQuota` 显式拒绝 `len(Limits) > 1`，报表侧 `Limits[0]` 是唯一正确写法 | P3 放开多窗口额度时，与 `buildProviderQuotas` + §2.5 渲染同批改造 |
| 方案第三梯队全部条目 | 维持不做，理由见方案 §6 | — |
