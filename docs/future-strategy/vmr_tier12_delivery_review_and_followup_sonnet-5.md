<!-- Ver 2026-08-13 01:30, by Sonnet 5 -->

# 第一/第二梯队交付复核 + 方案自省 + 外部评审核查 + 善后计划

**性质**：一次性复核报告。不改动任何既有文件（代码、文档、配置一律未动）——包括
`vmr_implementation_and_design_review_report_gemini-3.6-flash.md`：对它的逐项批注写在本文
PART C，不写回原文。

**核查范围**：

1. `docs/future-strategy/vmr_report_provider_client_cost_analysis_sonnet-5.md`（方案，下称"方案"）
2. `docs/future-strategy/vmr_report_story_cost_dimensions_devplan_opus-5.md`（第一梯队 dev plan，下称"DP1"）
3. `docs/future-strategy/vmr_quota_visibility_devplan_opus-5.md`（第二梯队 dev plan，下称"DP2"）
4. 本仓库当前全部未提交改动（第一梯队批 1~3、第二梯队批 0~5）
5. `docs/future-strategy/vmr_implementation_and_design_review_report_gemini-3.6-flash.md`（外部评审）

**核查方法**：每一条结论都回源码实测，不采信任何文档（包括 dev plan 自己的"完成情况"）的自述。
凡引用行数、数值、渲染形态，均以本次实际执行结果为准。

**基线验证（本次实测）**：

```
gofmt -l ./internal ./cmd     → 无输出
go vet ./...                  → 无输出
go test ./...                 → 全绿
go test -race ./internal/router/... ./internal/quota/... ./internal/replay/... → 全绿
```

---

## 0. 结论摘要

**总体判断：九个批次的功能实现是扎实的，主要缺陷不在代码逻辑，而在"文档声称的东西与代码做的东西
对不上"。** 具体分布：

| 类别 | 数量 | 最严重的一条 |
|---|---|---|
| 实现与计划的**未记录偏离** | 3 | 批 2 要求"主表去掉额度列"，实现保留了，但完成情况写"全部落地" |
| **文档事实错误**（会误导后来者） | 2 | 设计文档声称 §7 Finding"判据与路由半区 `Headroom<1` 同源"——实现是固定 90% 阈值，DP2 §5.1 明文禁止这个说法 |
| 代码级口径/语义问题 | 4 | `metric: cost` 账户无定价时 `WindowConsumed` 渲染成 `0`，与"真的零消耗"不可区分 |
| 方案/DevPlan **自身**的缺口 | 7 | 90% 固定阈值对短周期账户（`every: 5h`）在周期末尾必然误报；方案从未讨论阈值是否该随周期进度变化 |
| 仓库卫生 | 1 | `config.mba.yaml` / `config.mba copy.yaml` / `vmr-quota.json` 均未被 `.gitignore` 覆盖 |

**没有发现的问题**（明确排除，避免后来者重查）：无功能性回归、无并发问题、无导入边界破坏、
无行数预算越界、无确定性排序缺失、无 i18n 硬编码中文泄漏到英文产物。DP1/DP2 反复强调的三个
"坑"（虚拟模型名、旧日志 `$` 漂移、`aggregate.go` 行数）都确实避开了，且守门测试真实存在。

**Gemini 评审的净贡献**：10 条发现里 **2 条完全成立**（超额无高亮、UserGuide 章节编号说明）、
**3 条部分成立但根因或方案要改写**、**3 条不成立**（含 1 条数字错误、1 条重复已被否决的建议）、
**2 条超出本项目定位**。它最有价值的一条（TPM/RPM 盲区）恰好指向了一个**便宜得多的真缺口**，
但它自己没看见：429 计数早已算好在 `ProviderRow.ErrorClasses` 里、早已进了 JSON，只是 §2.5
表格没渲染这一列。

---

## PART A：实现 vs 计划的符合度核查

### A.1 逐批对齐（实测）

| 批次 | 计划要求 | 实测结果 | 判定 |
|---|---|---|---|
| 一梯队 批 1 | Provider 汇总，`DurMSMean` 代替分位，`aggregate.go` +1 行 | `provider.go`/`section_provider.go`/`i18n/report_provider.go` 齐备；`aggregate.go` 965 行（预算 1000） | ✅ 符合 |
| 一梯队 批 2 | Client×完整端点标签拆分，collector 模式，+3 行 | `clientendpoint.go` 用 `clientKey\x00endpoint` 为 key，排序三级 tie-break 完整 | ✅ 符合 |
| 一梯队 批 3 | 取端点真实模型而非 `Manifest.Model`，不碰 `render_spine.go` | `stepUpstream` 优先读 `Attempts[len-1]` 结构化字段；`render_spine.go` 仍 379 行（预算 380）；守门测试真实存在 | ✅ 符合（另见 A.2-7 的粒度缺口） |
| 二梯队 批 0 | `pctStr64`、单次 `config.Load`、多 Limit 前提锁定、一句注释 | 四项均落地；`cmdReport` 确实只有一处 `config.Load` | ✅ 符合 |
| 二梯队 批 1 | 加权公式下沉 `internal/quota`，零行为变更 | `weight.go` 逐字迁移；`router`/`replay` 测试仅改调用点名；`-race` 通过 | ✅ 符合 |
| 二梯队 批 2 | §2.5 子表 + **主表去掉额度列** | 子表落地且陈旧周期守门正确；**主表额度列未去掉** | ⚠️ **偏离，见 A.2-2** |
| 二梯队 批 3 | 90% 阈值 Finding，只认实时计数器 | `findings_quota.go` 拆成新文件，`Live == nil` 不命中 | ✅ 符合（判据本身的设计问题见 B-1） |
| 二梯队 批 4 | 6 处登记 + "twelve→thirteen" | 6 处齐备；文档 12→13 已改 | ⚠️ 遗漏一处反向注释，见 A.2-4 |
| 二梯队 批 5 | 端点标签下沉 `internal/core`，不改任何调用点行为 | `EndpointLabel`/`SplitEndpointLabel` 落地，2 产地 + 1 解析点迁移 | ✅ 符合（收敛已如实记录） |

**行数预算实测**（`grep -c ''`，与 archtest 的 `bytes.Count(data, "\n")` 同口径）：

| 文件 | 实测 | 预算 | 余量 |
|---|---|---|---|
| `internal/story/render_spine.go` | 379 | 380 | **1** ← 全仓库最危险 |
| `internal/report/aggregate.go` | 965 | 1000 | 35 |
| `internal/story/metrics.go` | 411 | 470 | 59 |
| `internal/story/corpus.go` | **358** | 380 | **22** |
| `internal/story/compare.go` | 748 | 850 | 102 |
| `internal/report/detail.go` | 1062 | 1150 | 88 |

（记下这张表是因为 Gemini 的 ISSUE-4 把 `corpus.go` 写成 368 行/12 行余量并称其为"最脆弱文件之一"
——数字错了 10 行，而真正只剩 1 行余量的 `render_spine.go` 它没提。详见 PART C。）

---

### A.2 发现清单

按严重度排序。每条给出：**问题 → 实证 → 后果 → 建议**。

---

#### 🔴 A.2-1 设计文档对 §7 判据的描述与实现不符，且违反 DP2 的明文禁令

**实证**：`docs/VirtualModelRouter_Design_v4_Analytics.md:100` 写：

> `§7` 的 `provider_quota_exhaustion`（批 3）只认这份实时计数器，某账户「本周期已用%」≥ 90 时命中，
> 缺实时数据不命中，**判据与路由半区 `Headroom<1` 同源**、`§3` 可靠性、`§4` 延迟与吞吐、…

而实现是 `internal/report/findings_quota.go:19` 的 `quotaExhaustionThresholdPct = 90.0`，判据为
`r.Live.Pct >= 90`。`quota.Headroom < 1` 的等价形式是 `已用% > 周期已过%`（DP2 §5.1 自己推导过），
两者**在数学上不同**：一个是绝对量阈值、一个是相对进度比较。

DP2 §5.1 已经预先禁止了这个说法：

> 要么直接用 `Headroom < 1`（推荐），要么自己定一个百分点缓冲……但**后者就不再等价于 `Headroom`，
> 不能声称"用的是同一条判据"**。二选一，别混着写。

**同一句还有第二个缺陷**：`§7` 的整段说明被插进了 §2.6"章节运行顺序"的枚举中间，导致句子结构断裂
——"……判据与路由半区 `Headroom<1` 同源**、`§3` 可靠性、`§4` 延迟与吞吐**、……"，读者会把 §3/§4
读成"同源"的并列宾语。

**后果**：这是全部改动里唯一一处会让后来者**基于错误前提做决策**的文档缺陷。有人照着这句话去改
阈值、或去论证"报表与路由的额度判定一致"，会直接踩空。

**建议**：改写为"判据是固定 90% 绝对阈值，**刻意不同于**路由半区的 `Headroom<1`（后者等价于
已用% > 周期已过%）——报警要的是绝对吃紧，路由要的是相对超速"，并把 §7 的整段说明从枚举中间
移到句号之后独立成句。**同时参考 B-1：判据本身也建议调整。**

---

#### 🔴 A.2-2 批 2 的"主表去掉额度列"未执行，但完成情况写"全部落地"

**实证**：DP2 §5.1 明确写"把'额度参照'列**移出**主表（主表因此不变宽）"，§5.3 写"主表去掉额度列，
其下渲染子表"，§5.4 的测试要求写"主表不再含额度列"。

实际 `internal/report/section_provider.go:44-46,72-78` 保留了该列：

```go
if hasQuota { headers = append(headers, t.QuotaHdr) }   // 主表仍有"额度参照"列
...
cells = append(cells, t.QuotaCell(p.Quota.Amount, p.Quota.Metric, p.Quota.Every))
```

对应测试也换成了另一个断言（`TestRenderProvidersNoQuotaAnywhereOmitsQuotaColumn`——测的是"没有任何
账户配额度时不出这一列"，不是计划要求的"主表不再有这一列"）。DP2 §5 完成情况写的是"全部落地"。

**后果有两层**：

1. **信息重复**：同一个 `Amount` 在主表"额度参照"列和子表"上限"列各出现一次。
2. **同一数字可能渲染成两个不同字符串**——这是重复带来的真实副作用：主表走
   `i18n/report_provider.go` 的 `quotaAmountStr`（`FormatFloat(amount,'f',-1,64)`），子表走
   `report/render_cells.go` 的 `numStr`（`FormatFloat(v,'f',2,64)`）。一个 `amount: 19.995` 的
   `metric: cost` 账户，主表显示 `19.995`，子表显示 `20.00`。

**建议**：二选一，别两张表都留。**推荐按原计划去掉主表的额度列**（子表已经把 `Amount` 与它的
分子、周期、进度一起给全了，主表那一列现在是纯冗余）；若决定保留，必须把两个 formatter 统一成
一个。无论选哪个，DP2 §5 的"完成情况"要如实修订。

---

#### 🟠 A.2-3 `LiveQuota` 丢掉了计划里的 `Estimated` / `EstimatedCost`

**实证**：DP2 §5.3 要求 `Live *LiveQuota`（新类型：`PeriodStart time.Time` + `quota.Counters` +
**`Estimated`/`EstimatedCost`**）。批 1 的 `quota.Bucket` 也确实把这两个字段导出了。但实现的
`report.LiveQuota`（`rows.go`）只有四个字段：

```go
type LiveQuota struct {
    Used, Pct float64
    PeriodStart, PeriodEndsAt time.Time
}
```

`buildProviderQuotas` 读到 `b.Estimated`/`b.EstimatedCost` 后直接丢弃。

**后果**：`AddEstimatedCost` 存在的全部意义就是标注"这个计数器里有多少是降级估算出来的"。丢掉它
之后，一个 `metric: cost` 账户的"本周期已用"哪怕 100% 来自估算，报表也会把它渲染成一个和权威记账
无法区分的确定数字——而这张表的整个卖点就是"它是权威记账、区别于左边那列的重算值"。这与本项目
`estimated_pct` / `⭐` / 低置信度脚注一以贯之的诚实标注纪律直接相悖。

**建议**：把 `Estimated`/`EstimatedCost` 加回 `LiveQuota`，渲染上沿用既有 `⭐` 或
`cacheEffCell` 的低置信度标记惯例——当 `EstimatedCost / Used` 超过某个比例时给"本周期已用"单元格
加标记，脚注说明"其中 X% 来自降级估算"。这是**低成本、高纪律价值**的一条。

---

#### 🟠 A.2-4 批 4 之后，`metrics.go` 的注释变成了自相矛盾

**实证**：`internal/story/metrics.go:80-86`（批 3 写入，批 4 未更新）：

```go
// ModelUsage/ModelSwitches (modelusage.go) are list-typed, unlike every
// other field above — they don't participate in the "nine numbered
// indicators" scalar set corpus.go's Spearman/diff machinery consumes
```

而批 4 恰恰把 `len(m.ModelSwitches)` 注册成了 `MetricModelSwitchCount`，进了
`corpusMetricCodes` / `corpusMetricKinds` / `metricValue` / `Compare` 的 `rows`。

**后果**：一个只读这段注释的人会得出"模型切换数据不参与相关性分析"的错误结论，而 §3.9 的
Spearman 矩阵里它就在。这正是 CLAUDE.md 反复警告的"陈旧细节比没有细节更糟，因为它读起来像权威"。

**建议**：改成"`ModelUsage` 是列表型、不进标量机制；`ModelSwitches` 本身是列表型，但它的**长度**
作为 `MetricModelSwitchCount` 已登记为第 13 项标量（见 compare.go）"。

---

#### 🟡 A.2-5 `metric: requests` 账户的 `WindowConsumed` 本可精确复现，却做成了估算

**实证**：`internal/report/providerquota.go:43-46`：

```go
case core.MetricRequests:
    d, _ := quota.ApplyModelMultiplier(ref.Spec, model, quota.Counters{Requests: int64(e.Requests)}, 0)
```

路由半区是**逐请求**记账：每次 `Charge` 传 `Requests: 1`，经 `ceilScale(1, mult)` = `ceil(mult)`。
报表是**按端点汇总后**取整：`ceil(e.Requests × mult)`。

用真实 `config.mba.yaml` 的 `volcengine`（`metric: requests`，`deepseek-v4-pro: 5.5`）算：
19 次请求 → 路由记 `19 × ceil(5.5) = 19 × 6 = 114`，报表算 `ceil(19 × 5.5) = 105`。偏差 9（8%），
且**随请求数线性放大**。

**关键点是：这个偏差在 `requests` 口径下是可以完全消除的**，因为每次记账的 `Requests` 恒为 1：

```
路由总量 = e.Requests × ceil(mult)     ← 精确恒等式，不是近似
```

`tokens` 口径下则不可恢复（逐请求 token 数不同，汇总后无法反推），但那里的 `ceil` 偏差上限是
每请求每分量 1 个 token，相对于百万级 token 完全可忽略。**也就是说，唯一有实质偏差的那个口径，
恰好是唯一可以做到精确的口径。**

**已有缓解**：`i18n/report_provider.go` 的 `WindowFootnote` 已经写明"逐请求 ceil 与汇总后取整"
是已知出入来源之一——诚实标注做到位了（Gemini 建议补这句话，其实早已存在，见 PART C）。

**建议**：`requests` 分支改为 `ceilScale(1, mult) * e.Requests`，把这一列从"带脚注的估算"升级成
"精确复现"，同时保留脚注里其余三条（失败尝试不计费、config 中途变更、cost 定价时点）。约 3 行改动。

---

#### 🟡 A.2-6 `metric: cost` 账户无定价时，`WindowConsumed` 渲染成 `0`，与"真的零消耗"不可区分

**实证**：`providerquota.go` 的 cost 分支只在 `e.CostEstimate != nil` 时累加；无定价时
`windowCost[provider]` 保持 0，`numStr(0)` 渲染成 `"0"`。而本项目的既有惯例是——`section_provider.go`
主表的 `$ 估算` 列在 `CostEstimate == nil` 时显示 `-` 而不是 `0`，`Live` 缺失时也显示 `-`。

**后果**：一个 `metric: cost` 但定价解析失败的账户（正是 DP1 反复强调的 AFP 类场景的近亲），
子表会显示"本报表窗口消耗 = 0"，读者合理地读成"这个账户这段时间没花钱"。这是**唯一一处新代码
违反了项目自己"缺数据显示 `-`、不显示 0"的既有纪律**的地方。

**建议**：`ProviderQuotaRow.WindowConsumed` 改为 `*float64`，cost 分支下所有端点都无 `CostEstimate`
时保持 nil，渲染 `-`。`requests`/`tokens` 口径不受影响（0 在那里是真实的零流量）。

---

#### 🟡 A.2-7 `vmr story` 的模型切换检测漏掉了 Step 内的 failover 切换

**实证**：`internal/story/modelusage.go` 的 `stepUpstream` 只读 `s.Rec.Attempts[len-1]`——**只有
最后一次尝试**。于是一个 attempts 为 `[volcengine/A 失败, opencode/B 成功]` 的 Step：

- 在模型使用表里只出现 `opencode:B`，`volcengine:A` **完全不可见**——尽管这次任务确实路由到过它；
- 不产生任何 `ModelSwitch` 记录，尽管这正是一次货真价实的模型切换。

同时 `ModelUsageStat.Steps` 的注释声称"**INCLUDING** a Step where every attempt against it failed"
——这只在"全部 attempt 都落在同一个端点上"时成立。多端点 failover 的 Step 里，除最后一个之外的
端点一个都不计。注释与行为不完全一致。

**这不是实现偷懒，是方案的粒度选错了**：方案 §5.4 把切换定义成"相邻 **Step** 之间上游不同"，
而 failover——三种切换成因里最确定、最值得记录的那一种——**发生在 Step 内部**。`OnFailoverStep`
这个"观察性标记"实际上是在给一个它看不见的事件打旁证：它只能说"这个 Step 试过不止一次"，
说不出"这次切换就是那次 failover"。

**建议**（拆两步）：
- **便宜的一半**：`ModelUsageStat` 的统计改为遍历 `s.Rec.Attempts` 全部条目（token 仍只归属最后
  一次成功的），让被 failover 掉的端点在模型使用表里可见，并修正 `Steps` 的注释措辞。
- **完整的一半**：新增 Step 内切换的记录形态（`ModelSwitch` 加一个 `WithinStep bool`）。这一项
  改动面较大且会影响 golden 文件与 `MetricModelSwitchCount` 的取值口径，建议单独排期。

---

#### 🟢 A.2-8 config.yaml 读不到时打印两条几乎同义的警告

**实证**：`cmd_report.go` 的单次 `config.Load` 失败后，`buildPricing` 与 `buildProviderQuotas`
各打印一条：

```
pricing: <path> not usable (<err>) — $ estimates use the standard price table only, no account overrides
provider quotas: <path> not usable (<err>) — §2.5 renders without quota references
```

批 0 的整改动机是"两次加载可能读到两份不同配置"，这个目的达到了；但输出上的重复是新引入的。
对着一堆裸日志跑 `vmr report`（DP2 §3 明确保护的场景）会稳定看到两条报同一个文件的警告。

**建议**：在 `cmdReport` 里对 `cfgErr` 打印一次统一的降级说明，两个 callee 不再各自打印路径。

---

#### 🟢 A.2-9 `Bucket.PeriodStartTime` 的注释说 UTC，实际返回 Local

**实证**：`internal/quota/store.go` 新增：

```go
// PeriodStartTime is PeriodStart converted back to a time.Time (UTC — ...)
func (b Bucket) PeriodStartTime() time.Time { return time.Unix(b.PeriodStart, 0) }
```

`time.Unix` 返回的 `Time` 携带 `time.Local`，不是 UTC。注释后半句（"比较两个 time.Time 是否相等
不关心时区，只关心是否指向同一时刻"）是对的，所以**行为无误**，只是括号里那个词写错了。在一个
把时区不变量写进 CLAUDE.md 的项目里，这种注释错误值得改。

**建议**：把 "(UTC — " 改成 "(in time.Local, which is irrelevant here — "。

---

#### 🟢 A.2-10 仓库卫生：三个不该进版本库的文件未被忽略，两份文档被无声删除

**实证**：`git status` 里未被 `.gitignore` 覆盖的新文件：

- `config.mba.yaml`——个人部署配置（密钥用 `${ENV}` 引用，**没有明文泄漏**，但它是私有配置，
  `.gitignore` 已经忽略了 `/config.yaml` 和 `*.local.yaml`，唯独漏了这个命名）
- `config.mba copy.yaml`——文件名里带 " copy" 的临时副本，明显是误留
- `vmr-quota.json`——仓库根目录下的运行时产物（批 2 冒烟测试的副产品）

同时本次改动删除了两份文档，三份计划里**都没有任何一处提到要删它们**，`CHANGELOG.md` 也没记：

- `docs/dependabot_branch_cleanup_2026-08-11_sonnet-5.md`（131 行）
- `docs/future-strategy/VMR_综合评审与发展建议_report_v2.md`（253 行）

**建议**：`.gitignore` 补 `/config.mba*.yaml`、`/vmr-quota.json`、`* copy.*`；删掉那个 copy 文件；
两份文档的删除要么撤销、要么在 commit message 里说明理由（CLAUDE.md 明确把评审报告类文档列为
"预期会累积"的一类，无声删除与那条约定相抵触）。

---

#### ⚪ A.2-11 两处轻微的计划偏离（记录备查，不建议改）

- DP2 §5.2 写"`cost` 口径**仍然走 `BaseAmount`**……保持三种 metric 走同一条出口，**别为 cost 开
  分支**"。实现开了分支（`windowCost` 独立 map + `if Metric == MetricCost` 覆写）。**这个偏离其实
  是对的**——独立 map 让 A.2-6 的 `-` 修复变得容易，走 `Counters.Cost` 反而会把"没有定价"和
  "定价为 0"混在同一个 float64 里。建议保留实现，修订 dev plan 的措辞。
- DP1 §5 风险表要求"`Build` 的 doc comment 补一句'额度参照只走 `BuildCached`'"。这句话写在了
  `BuildCached` 的注释里，不在 `Build` 的。相邻可见，影响可忽略。

---

## PART B：方案与 DevPlan **自身**的缺口

这一部分不看代码是否照做，只问"照做了也仍然不够好的地方在哪"。

---

### 🔴 B-1 90% 固定阈值对短周期账户在周期末尾必然误报——方案从未讨论阈值是否该随周期进度变化

**问题**：方案与 DP2 讨论 §7 Finding 时，把注意力全放在"用实时计数器而非重算值"（对的）和
"阈值不进 config.yaml"（也对的），但**从没问过一个更前置的问题：90% 这个绝对阈值在不同周期长度
下意味着同一件事吗？**

不意味着。对 `every: 1mo` 的账户，用到 90% 通常确实值得警惕。但对 `every: 5h` 的账户
（`config.example.yaml` 里就有这种配置），一个稳定匀速消耗的健康账户，在每个 5 小时周期的最后
半小时都会稳定地越过 90%——然后每次跑报表都报一次警。

DP2 §5.1 自己已经把正确的判据推导出来了（`Headroom < 1 ⟺ 已用% > 周期已过%`），甚至把
`PeriodElapsedPct` 算好了放进 `ProviderQuotaRow`——**但批 3 的 Finding 一个字都没用它**。

**这一点的严重性在于本项目已有的先例**：方案第三梯队否决"Client 单点倾斜 Finding"的理由原文是
"**一个会对正确配置持续报警的检测器，只会训练用户忽略整个 §7**"。90% 固定阈值在短周期账户上，
就是同一个毛病。方案自己写下了这条判据，却没有拿它检查自己新增的这条 Finding。

**建议**：判据改为**两个条件同时满足**：

```
Live.Pct >= 90  &&  Live.Pct > PeriodElapsedPct
```

含义是"绝对量已经吃紧，**且**消耗速度快于周期流逝"。这样：
- 长周期账户的行为几乎不变（用到 90% 时周期通常还没过 90%）；
- 短周期账户在周期末尾的正常满载不再报警；
- 判据里那个相对项**真的**与路由半区的 `Headroom < 1` 同源了——顺带让 A.2-1 那句文档描述变成
  一句可以成立的话（改成"绝对阈值 + Headroom 同源的相对项"）。

数据都在 `ProviderQuotaRow` 上，改动量约 2 行 + 1 条测试。

---

### 🔴 B-2 `vmr-quota.json` 与输入日志可以来自两台不同机器，三份文档都没处理

**问题**：`buildProviderQuotas` 从 `cfg.LogDir` 拼出 `vmr-quota.json` 的路径；而报表分析的审计日志
来自命令行位置参数（`resolveInputPaths`）。**这两者之间没有任何一致性约束。**

真实会发生的场景：把同事/另一台机器的 `vmr-audit-*.jsonl.zst` 拷过来分析，本机 `config.yaml`
正常、本机 `vmr-quota.json` 也正常——于是子表会把**本机的额度状态**贴在**别人的日志报表**上，
两列并排，看起来像同一个账户的两个窗口。而脚注只说"两者是不同的时间窗口"，从不说"它们可能
根本不是同一个实例"。

Gemini 的"多节点部署"担忧（PART C-2.1）其实是这个问题的一个特例，且是本项目定位下不成立的那个
特例。**这个非分布式版本才是真实存在的误导路径**，而且今天就能发生。

**建议**（低成本）：
1. 渲染层在子表脚注里显式写出实时计数器的来源路径（`来自 <log_dir>/vmr-quota.json`）——让读者
   自己能判断它是不是与输入日志同源；
2. 当输入日志路径全部不在 `cfg.LogDir` 下时，额外加一句提示"输入日志不在本实例的 log_dir 下，
   实时列可能来自另一个实例"。

---

### 🟠 B-3 报表窗口与计费周期**完全不重叠**时，两列仍并排渲染且无任何提示

**问题**：`PeriodStart`/`PeriodEnd`/`PeriodElapsedPct` 全部由 `now`（报表生成时刻）算出，而
`WindowConsumed` 来自输入日志覆盖的时间段。分析三个月前的历史日志时，子表会渲染出"本报表窗口
消耗 12345（来自 5 月的日志）| 本周期已用 678（8 月的周期）| 周期区间 08-01 ~ 09-01"。

脚注说了"两者不可相减"，但没说"它们可能相隔几个月"。方案与两份 dev plan 都只讨论了"窗口不对齐"
（部分重叠），从没讨论"完全无交集"这个更极端也更容易误读的情形。

**建议**：`Report2.Meta` 已经有 `From`/`To`（日志覆盖范围）。当 `[From, To]` 与
`[PeriodStart, PeriodEndsAt]` 无交集时，在 `WindowConsumed` 单元格后加一个既有惯例的标记
（`⭐` 或 `†`），脚注说明"该列的日志窗口与右侧周期无时间交集"。

---

### 🟠 B-4 §2.5 静默丢掉了方案 §4.2 承诺的"错误分类"，而数据早已算好

**问题**：方案 §4.2 保留的可靠性指标原文是"**尝试数 / 成功率 / 错误分类 / 失败尝试墙钟耗时**"。
实现里 `ProviderRow` 四项都算了（`Attempts`/`RequestsOK`/`ErrorClasses`/`WastedMS`）、四项都进了
`vmr-report.json`，但 `section_provider.go` 只渲染了成功率和错误率——**`ErrorClasses` 与 `WastedMS`
在 Markdown 里一个字都没有**。这个缩减没有出现在任何一份 dev plan 里，也没有理由说明。

**这条的价值远超它的成本**，因为它恰好是 Gemini 那条"TPM/RPM 盲区"关切里唯一便宜且数据已就绪的
部分：`core.ErrRateLimit` 的 `String()` 是 `"rate_limit"`，`ProviderRow.ErrorClasses["rate_limit"]`
就是这个账户被 429 的次数。**"这个账户是额度耗尽，还是被短时限流"——今天数据上已经能答，只差
一列渲染。**

**建议**：§2.5 主表补一列"主要错误类"（复用 `section_reliability.go` 已有的 `topErrorClassCount`
helper 形态，如 `rate_limit 12(63%)`）。若担心主表变宽，正好与 A.2-2 的"去掉额度列"配对——一进
一出，宽度不变。

---

### 🟡 B-5 §5.5 与 §2.5 子表都没有规模上限，也没有可观测的触发条件

**问题**：DP1 §5 风险表提到"批 2 的表在 client 或 endpoint 很多时可能偏长……若真实数据下过长，
再补 Top-N"。但**没有定义"过长"是多长，也没有任何机制让人发现它变长了**——`vmr report` 不会
提示"§5.5 渲染了 400 行"。§5.5 是 client×endpoint 的笛卡尔积，本仓库真实数据里 client 少所以没
暴露，一个有 20 个 client × 15 个端点的部署会得到一个 300 行的章节。

**建议**：不预先做 Top-N（方案的判断是对的），但补一个可观测量：在 `tw`（进度输出）里打一行
"§5.5: N clients × M endpoint rows"。零渲染成本，让"过长"这件事有人能发现，而不是等某天有人抱怨。

---

### 🟡 B-6 config 热重载与报表的一次性快照读之间，`-` 有两种截然不同的含义

**问题**：子表实时列显示 `-` 有两种成因，脚注只解释了第一种：

1. 计数器停留在更早周期（进程停过一整个周期）——脚注说了；
2. **`config.yaml` 的 `every` 或 `metric` 被改过**——`limitKey = metric + "/" + everyText` 因此
   变了，盘上的旧桶键名不再匹配，`live[p.Name][limitKey]` 直接 miss。

第二种情况下计数器完全健康、进程一直在跑，但读者会照着脚注得出"进程停过"的错误结论。

**建议**：区分两种 miss。`limitKey` 完全不存在但该 provider 下有其它 key → 提示"额度配置在本周期
内变更过，旧计数器已不适用"；`limitKey` 存在但周期不匹配 → 保持现有文案。

---

### 🟡 B-7 三份文档对"这次改动动了多少个包"没有一处总账

**问题**（元层面，但影响下一次评审的成本）：九个批次横跨 `report`/`story`/`quota`/`core`/`router`/
`replay`/`i18n`/`config`/`cmd` 九个包、新增 20 个源文件。DP1 §6 有一张"与方案文档的对应关系"自查
表，DP2 §1 有一张批次表——但**没有任何一处列出"最终交付的文件清单"**。这次复核里，"批 2 的主表
额度列没去掉"（A.2-2）之所以能藏住，正是因为两份 dev plan 的完成情况都是散文式自述，没有一张
可逐项打勾的交付物清单。

**建议**：以后同规模的 dev plan，"完成情况"里附一张 `新增/修改文件 → 计划条目` 的对照表。这次
的清单可以直接从 `git status` 生成，补进 DP2 §1。

---

## PART C：对 `vmr_implementation_and_design_review_report_gemini-3.6-flash.md` 的逐项批注

**批注图例**：

| emoji | 含义 |
|---|---|
| ✅ | 问题成立，且**建议早已实现**——评审漏看了现有代码 |
| 🟢 | 问题成立，根因正确，建议可采纳（可能微调形式） |
| 🟡 | 问题部分成立，但**根因分析或建议方案需要改写** |
| 🟠 | 现象存在，但**影响被显著高估**；且其建议方案有副作用 |
| ❌ | **问题不成立**（事实错误、或超出本项目定位） |
| 🔁 | **重复一条已被明确否决的建议**，且未回应否决理由 |

---

### PART I 代码级发现（5 条）

---

#### 🟡 I-1 非整数 `model_multipliers` 的阶梯取整偏差

> 原文位置：§1.2 第 1 条，`internal/report/providerquota.go#L43-L48`

**问题是否成立**：✅ **成立**。已实测复核：路由逐请求 `ceilScale(1, mult)`，报表按端点汇总后
`ceilScale(e.Requests, mult)`。真实 `volcengine`（`deepseek-v4-pro: 5.5`）19 次请求 → 路由 114、
报表 105，偏差 8%。

**根因分析是否正确**：🟡 **大体正确，但有两处不精确**：
1. 它说"偏差规模随请求数线性放大"——在 `requests` 口径下**成立**；但它没区分口径，而在 `tokens`
   口径下偏差上限是每请求每分量 1 个 token，相对百万级 token 完全可忽略。把两者混为一谈会让人
   以为 tokens 账户也不可信。
2. 它说这是"报表重算与真实记账的偏差"——对，但**没有意识到 `requests` 口径的偏差是可以完全消除
   的**（每次记账 `Requests` 恒为 1，故路由总量 ≡ `e.Requests × ceil(mult)`，是恒等式不是近似）。

**建议方案是否最优**：❌ **不是**。它建议加一个 `MultiplierIsNonInteger` 标识来"提示用户这是下限
估算"。但正确的做法是**直接算对**，而不是给一个可以算对的数字加免责标签：

```go
case core.MetricRequests:
    // 路由逐请求 charge，每次 Requests=1 → 总量恒等于 e.Requests × ceil(mult)
    d := quota.Counters{Requests: ceilMult(mult) * int64(e.Requests)}
```

**且它建议的第二件事已经做了**——见下方 III-3 的批注：那句脚注早已存在。

**采纳结论**：问题采纳（→ A.2-5），方案改写为精确修正。

---

#### 🟢 I-2 `Live.Pct > 100%` 时缺少爆表视觉标记

> 原文位置：§1.2 第 2 条

**问题是否成立**：✅ **成立**。`pctHundred` 只做 `FormatFloat(v,'f',1,64) + "%"`，`138.9%` 与
`68.0%` 在视觉上无区别。

**根因分析是否正确**：🟢 **正确**，且它准确地指出了排序与 Finding 都已经正确处理，只有渲染层缺
标记——这个定位是对的。

**建议方案是否最优**：🟡 **方向对，形式建议调整**。它建议追加 `⚠️` 或 `(EXHAUSTED)`。本项目已有
一套标记惯例（`⭐` + 表下脚注，见 `cacheEffCell` / `section_reliability.go`），新发明一个
`(EXHAUSTED)` 英文串还要过 i18n。建议复用既有 `⭐` 形态。

**另外补一条它没提的**：`Pct` 字段注释明写 "not clamped"（刻意不截断，正确），但 `PeriodElapsedPct`
也不截断——`TimeLeftFrac` 在 `now > end` 时返回 0，故 `PeriodElapsedPct` 最大 100，不会爆表。
两列的边界行为不同，值得在同一次改动里对齐说明。

**采纳结论**：采纳，形式改用 `⭐` 惯例；优先级低（Finding 已经在报警，这只是让表更好读）。

---

#### 🟠 I-3 多 Limit 的 `vmr-quota.json` 会被静默跳过

> 原文位置：§1.2 第 3 条，`cmd/vmr/cmd_report.go#L164-L170`

**问题是否成立**：🟠 **现象成立，但影响被高估到几乎不成立**。今天 `config.validateQuota` 拒绝
`len(Limits) > 1`（`TestQuota_Reject_MultipleLimits` 锁死），代码里已有明确的前向兼容注释指明
P3 需要重写这个函数。评审自己也承认了这两点，却仍把它列为"隐患"。

**根因分析是否正确**：❌ **不正确，且推理方向反了**。它的场景是"未来 P3 放开后，路由写入多个
limitKey，报表静默跳过"。但 P3 放开的那一刻，`buildProviderQuotas` 的 `Limits[0]` 本身就必须改
——不是"读的时候要警告"，是"这个函数整体作废"。给一个已经标记为"届时必须重写"的函数加运行期
警告，是给不会存活到那天的代码加维护成本。

**建议方案是否最优**：❌ **不是，而且会产生误报**。它建议"当 `live[p.Name]` 的 limitKey 数量 > 1
时打 debug 日志"。**这个条件今天就会误触发**：用户把 `every: 1mo` 改成 `1w`，盘上的旧 `requests/1mo`
桶不会被删（`Registry` 是懒重置，不清理旧键），于是 `live[p.Name]` 合法地有 2 个 key，与多 Limit
毫无关系。照它的建议实现，会对一次普通的配置修改持续报警——正是方案第三梯队否决"Client 单点倾斜
Finding"时的同一个毛病。

**采纳结论**：**不采纳**。现有注释 + `TestQuota_Reject_MultipleLimits` 已经足够。它的观察反而
帮我发现了一个**真问题**：旧 limitKey 残留会让实时列显示 `-`，而脚注把这解释成"进程停过"——
已单列为 B-6。

---

#### ❌ I-4 `internal/story/corpus.go` 行数预算"严重临界"

> 原文位置：§1.2 第 4 条

**问题是否成立**：❌ **事实错误**。实测 `corpus.go` 是 **358 行**（`grep -c ''`，与 archtest 的
`bytes.Count(data,"\n")` 同口径），预算 380，**余量 22 行**。评审写的是"368 行，仅剩 12 行"——
多报了 10 行。

**根因分析是否正确**：❌ **且结论完全错位**。它称 `corpus.go` 是"整个仓库中最脆弱的文件之一"。
实测全仓库最脆弱的是 `internal/story/render_spine.go`：**379 / 380，余量 1 行**——而这一点
DP1 §0 从一开始就写在表里、批 3 全程刻意绕开、评审却完全没提。它盯错了文件。

**建议方案是否最优**：🟡 建议本身（"扩展前先把 `metricValue` 拆到 `corpus_metrics.go`"）在原则上
无害，但基于错误的紧迫性判断。DP2 §10 风险表已经写了同样的处置："若超出，把 `metricValue` 拆成
独立文件而不是抬高预算"——**评审的建议是已有风险表的复述，不是新信息**。

**采纳结论**：**不采纳**。但把"`render_spine.go` 余量 1 行"这个真正的悬崖登记进善后计划的观察项。

---

#### 🟢 I-5 英文 UserGuide 的"nine numbered sections"与 §2.5/§5.5 的关系

> 原文位置：§1.2 第 5 条，`docs/UserGuide.md:336`

**问题是否成立**：🟢 **成立，且是这 5 条里最干净的一条**。实测 `UserGuide.md:336` 写
"The Markdown groups into nine numbered sections"，紧接着的项目符号列表里就并列着 §2.5 和 §5.5
——读者会数出 11 个而不是 9 个。中文版 `UserGuide.zh.md` 有同样的问题。

**根因分析是否正确**：🟢 **正确**。它也正确地指出了 DP1 §3.4"数字 nine 不用改"的判断本身没错
（`vmr report` 确实仍是 §0–§8 九个编号大节），缺的只是一句说明。

**建议方案是否最优**：🟢 **是**。加一句"§2.5 与 §5.5 是嵌在大节内的增强子章节，不占用主章节编号"
即可，中英文各一句。

**采纳结论**：**采纳**，进善后计划（改动 2 行，零风险）。

---

### PART II 设计缺口（5 条）

---

#### ❌ II-1 分布式/多节点部署下 `vmr-quota.json` 失效

> 原文位置：§2.1 第 1 条

**问题是否成立**：❌ **在本项目定位下不成立**。`CLAUDE.md` 第一句就是"**Local-run, single-binary**
LLM router"；`docs/` 全部设计文档、`rundir` 的 `~/.vmr` 默认目录、`/admin/status` 的
**loopback-only** 绑定，全部围绕单机单进程。评审假设的"Kubernetes 多 Pod 部署"不是这个产品的场景。

**根因分析是否正确**：❌ **倒置了因果**。它把这描述成"报表层的可见性缺口"。但 `quota.Registry`
本身就是**进程内状态**——多实例部署下，**路由半区的额度执行**会先失效（4 个实例各自认为自己
用了 25%，于是四个都不降权，实际超额 4 倍），报表显示偏小只是这个更根本问题的一个症状。
把症状当成病来治（"让报表去 Redis 拿全局状态"）会得到一个"报表说 100% 了但路由还在往里灌"
的更糟状态。

**建议方案是否最优**：❌ **不是**。真要支持多实例，正确的顺序是先让**路由半区**的额度状态共享
（这是 Quota-Aware Routing 设计文档层面的大改），报表自然跟着正确。

**采纳结论**：**不采纳为任务**，但它提示的单机版变体是真问题——已重写为 **B-2**（同一台机器上，
`cfg.LogDir` 的 `vmr-quota.json` 与命令行传入的审计日志可以来自两个不同实例）。另建议在设计文档
的"已知限制"表里补一行"多实例部署下额度记账本身即不共享，报表只是如实反映这一点"，一句话即可。

---

#### 🟢 II-2 缺失 TPM/RPM 滑动窗口限流的可见性

> 原文位置：§2.1 第 2 条

**问题是否成立**：🟢 **成立，这是它 10 条里最有价值的一条**。"这个账户是硬额度耗尽，还是短时
被限流"确实是运维要区分的两件事，而 §2.5 今天只有一个笼统的错误率列。

**根因分析是否正确**：🟡 **方向对，但它没看数据已经在哪儿了**。它说"现有 §2.5 完全无法回答"。
实测：`core.ErrRateLimit.String()` == `"rate_limit"`，`buildProviders` 已经把每个账户的
`ErrorClasses` 汇总好了、`ProviderRow.ErrorClasses` 已经序列化进 `vmr-report.json`。**429 次数
今天就在产物里，只是 §2.5 的 Markdown 表格没渲染这一列。**

这同时暴露出方案自己的一个静默缩减——方案 §4.2 承诺了"错误分类"，实现只渲染了错误率。
已单列为 **B-4**。

**建议方案是否最优**：🟡 **它的完整版（Peak TPM / Peak RPM）成本远高于它的估计**，需要按分钟
分桶（报表今天最细到小时），且"峰值"这个量对不同厂商的窗口定义（滑动 vs 固定）不同，容易给出
一个看着精确实则不可比的数字。而它 80% 的价值可以用**一列**拿到。

**采纳结论**：**拆两半**。
- 便宜的一半（§2.5 补"主要错误类"列，让 `rate_limit` 可见）→ **采纳进善后计划**（B-4）。
- 完整的 Peak TPM/RPM → **并入第三梯队**，触发条件：出现"错误率高但额度充裕"的真实运维案例，
  且便宜的那一半已经不足以定位。

---

#### 🟢 II-3 `vmr story` 模型切换缺少 Step 级工具上下文

> 原文位置：§2.1 第 3 条

**问题是否成立**：🟢 **成立**。切换行今天只有"第 7 步：A → B"，要知道第 7 步在干什么必须往下
滚到决策脊柱。

**根因分析是否正确**：🟢 **正确**，且它正确地把握住了本项目的措辞纪律——它明确提出"在不违反
'不推断因果'的前提下加**客观关联信息**"，这个分寸拿捏得准，比它其它几条都好。

**建议方案是否最优**：🟢 **可行**。`Step` 上已有工具调用信息（`render_spine.go` 就在用），
`ModelSwitch` 加一个 `StepTool string` 字段即可，`computeModelUsage` 顺手填。

**但优先级要下调**，理由是评审没看到的一点：切换行的正下方（`render_md.go` 的调用顺序是
`renderOverviewCard → renderModelUsage → renderDecisionSpine`）**紧接着就是决策脊柱**，按 Step
列出了每一步的工具调用。"滚动数十步找 Step 7"的痛苦被高估了。

**且它没意识到一个更严重的相邻问题**：Step **内**的 failover 切换今天根本检测不到（见 A.2-7）。
在切换行上加工具名之前，先让所有切换都被检测到，价值排序更合理。

**采纳结论**：**采纳，排在 A.2-7 之后**，作为可选项进善后计划。

---

#### 🔁 II-4 行数预算导致文件碎片化，应把 `internal/report` 拆子包

> 原文位置：§2.1 第 4 条 + §3.3 第 3 条

**问题是否成立**：❌ **不成立**，且**这是一条重复已被明确否决的建议**。

方案第三梯队表里已经有这一条，否决理由原文：

> 外部评审把"新章节 = 新文件"当成规避手段，实为 `CLAUDE.md` 的明文约定，行数预算正是其执行机制。
> 且第二梯队全部批次对 `aggregate.go` 的改动是 **0 行**，此刻不阻塞任何事；在没有功能需求推动时
> 做这个高耦合文件的大重构，是拿确定的风险换不确定的收益。

**本次评审一个字都没有回应这些理由**，只是把同样的主张重述了一遍（甚至用了同样的措辞"回避
`aggregate.go` 的重构"）。而它在第一梯队 dev plan 的对齐表里刚刚给这批改动打了"✅ 完全符合"
——一边确认实现符合约定，一边把约定本身当成问题。

**补充一条实证**：本次九个批次对 `aggregate.go` 的净改动是 **+5 行**（1 行 collector 初始化、
1 行 `.add`、3 行结果赋值/派生调用），965/1000，余量 35 行。"为了控制行数不得不大量使用事后派生
和 Collector 钩子"这个描述，与实际改动量不符——事后派生本来就是 `buildTools`/`buildCompactions`
的既有模式，不是这次为规避预算发明的。

**采纳结论**：**不采纳**，维持第三梯队原判与原触发条件（"下一次真正需要往 `aggregate.go` 里加
逻辑时，借那次改动一并拆"）。

---

#### 🟡 II-5 无法应对阶梯式 / 动态 TTL Prompt Caching 扣减

> 原文位置：§2.1 第 5 条

**问题是否成立**：🟡 **部分成立**。`core.TokenWeights` 确实是四分量线性组合，确实表达不了
"按上下文长度分档"或"按缓存驻留时长阶梯"的扣减规则。

**根因分析是否正确**：🟡 **但它跑出了本次改动的范围**。`TokenWeights` 是**路由半区**
Quota-Aware Routing 的模型（`core.QuotaSpec`），归 `docs/TokenPlan_Quota_Routing_Design_opus-5.md`
管；第一/第二梯队全部是**分析半区**的只读消费。它把一个路由半区的建模局限，写进了一份分析半区
的实现评审。

**且它有一半已经被登记过了**：方案第三梯队表里的"输入长度分层分布"条目，触发条件原文就是
"**出现'按上下文档位分别计价'的账户，即分层本身成为计费口径的一部分**"——这正是它说的前半个
场景。

**建议方案是否最优**：🟡 "扩展成 Quota Cost Evaluator 对象"这个方向没错，但在**没有任何一个
真实账户按这种规则计费**之前做这个抽象，是典型的投机性通用化。本项目的原则（KISS/YAGNI）
明确反对。

**采纳结论**：**部分采纳为触发条件的扩写**。它提出的"按 cache TTL 阶梯扣减"是既有触发条件没
覆盖的**新子场景**，值得追加进第三梯队那一条的触发条件里（见 PART E.3）。不新增条目。

---

### PART III 建议路线图（3 组）

| 原建议 | 批注 |
|---|---|
| **3.1.1** 渲染层超额高亮 | 🟢 采纳，形式改用既有 `⭐` 惯例（见 I-2） |
| **3.1.2** UserGuide 子章节编号说明 | 🟢 采纳（见 I-5） |
| **3.1.3** 在脚注补"非整数倍率取整出入"说明 | ✅ **已经实现了，评审漏看**。`internal/i18n/report_provider.go` 的 `WindowFootnote` 中英文都已包含"逐请求 ceil 与汇总后取整 / per-request vs. aggregate-then-ceil rounding"。真正该做的不是补脚注，是**把 `requests` 口径算精确**（见 A.2-5） |
| **3.2.1** Story 切换表加工具上下文 | 🟢 采纳，但排在"先让 Step 内切换可被检测"之后（见 II-3 / A.2-7） |
| **3.2.2** `corpus.go` 预重构 | ❌ 不采纳，基于错误的行数（见 I-4） |
| **3.3.1** 多节点配额快照服务 | ❌ 不采纳（见 II-1），但其单机变体已重写为 B-2 |
| **3.3.2** TPM/RPM 维度 | 🟡 便宜的一半采纳（B-4），完整版并入第三梯队 |
| **3.3.3** `internal/report` 子包化 | 🔁 不采纳，重复已被否决的建议（见 II-4） |

**对这份外部评审的总体评价**：它的**符合度核对部分是可靠的**——九个批次的对齐判断全部经得起
复核，三大工程硬伤确实都避开了，这一点它说对了。它的**代码级发现有真东西**（超额高亮、非整数
倍率），但**核查深度不够**：一处行数报错 10 行、一处建议早已实现、一处建议会产生误报。它的
**设计层发现质量参差**：一条是本项目定位外的假想（多节点）、一条是重复已否决的主张（子包拆分）、
一条越界到了路由半区（TokenWeights），但另外两条（TPM/RPM、Story 上下文）指向了真实的空白。

一个共同模式值得记下：**它的所有判断都停在文档和代码的表层，没有一次去查"这个数据是不是已经
算好了"**——429 计数、`WindowFootnote` 的既有措辞、`corpus.go` 的真实行数，三次都是。

---

## PART D：本报告自身的复查

对上文所有结论做了一次反向核验，记录如下：

**已实测确认，不是推断**：
- 全部行数（`grep -c ''`，与 archtest 同口径）
- `go build`/`go vet`/`gofmt`/`go test ./...`/`go test -race`（router/quota/replay）
- `section_provider.go` 主表确实保留额度列（读源码 + 读测试断言双重确认）
- `LiveQuota` 确实无 `Estimated`/`EstimatedCost`（读类型定义）
- `core.ErrRateLimit.String() == "rate_limit"` 且 `buildProviders` 确实汇总 `ErrorClasses`
- `i18n` 的 `WindowFootnote` 确实已含 ceil 说明（读中英文两份）
- `config.mba.yaml` 的密钥确实是 `${ENV}` 引用，**无明文泄漏**（逐行 grep 后确认）

**属于判断而非事实，可能有人不同意**：
- B-1（90% 阈值改双条件）：如果部署里根本没有短周期额度账户，这条的紧迫性大幅下降。它的价值
  取决于 `every: 5h` 这类配置是否真实存在于用户侧。
- A.2-2 的处置方向（去掉主表额度列 vs 统一 formatter）：两条路都能解决问题，我倾向前者是因为
  它同时给 B-4 的新列腾出了宽度，但保留也说得通。
- A.2-7 的"完整的一半"（Step 内切换）：它会改变 `MetricModelSwitchCount` 的取值口径，进而影响
  已经跑出来的 corpus 统计的可比性。这个代价我认为值得，但它是一个真实的代价，不是零成本。

**本报告没有覆盖的**：
- 未在真实数据上重跑 `vmr report`/`vmr story`（DP1/DP2 的完成情况已记录过冒烟结果，本次复核
  聚焦源码与文档一致性，未重复冒烟）。A.2-6（cost 账户显示 `0`）与 B-3（窗口无交集）两条如果
  要落地，建议先各造一次真实数据验证。
- 未审查 `internal/report/detail.go`、`session.go` 等本次未改动的既有文件。

---

## PART E：善后工作计划

前提：第一梯队三批 + 第二梯队六批已全部落地并通过测试。以下是**处理完这两个梯队之后**的遗留
工作，按"是否值得马上做"分三类。

### E.1 立即处理（建议在当前 commit 之前或紧随其后完成）

共同特征：**要么是文档说了假话，要么是纪律被破坏，要么改动 ≤5 行**。

| # | 条目 | 来源 | 改动量 | 为什么现在做 |
|---|---|---|---|---|
| 1 | 修正设计文档 §2.6 对 §7 判据的错误描述（"与 `Headroom<1` 同源"）+ 修复被插断的章节枚举句 | A.2-1 | 文档 2 处 | 唯一一处会让后来者**基于错误前提做决策**的缺陷 |
| 2 | §7 Finding 判据改为 `Pct >= 90 && Pct > PeriodElapsedPct` | B-1 | ~2 行 + 1 测试 | 短周期账户的必然误报；且改完之后条目 1 的文档描述才真正成立 |
| 3 | 决断并统一 §2.5 的额度列（推荐：去掉主表那列），修订 DP2 §5 的完成情况 | A.2-2 | ~10 行 + 文档 | 同一数字两处渲染、两个 formatter；完成情况目前是不实陈述 |
| 4 | `LiveQuota` 加回 `Estimated`/`EstimatedCost` 并按 `⭐` 惯例标注 | A.2-3 | ~15 行 | 违反本项目最核心的诚实标注纪律 |
| 5 | 修正 `internal/story/metrics.go` 中已被批 4 推翻的注释 | A.2-4 | 3 行 | 陈旧注释读起来像权威（CLAUDE.md 明文警告） |
| 6 | `metric: cost` 无定价时 `WindowConsumed` 渲染 `-` 而非 `0` | A.2-6 | ~8 行 | 唯一一处违反项目"缺数据显示 `-`"既有纪律的新代码 |
| 7 | `.gitignore` 补 `/config.mba*.yaml`、`/vmr-quota.json`；删除 `config.mba copy.yaml`；说明或撤销两份被删文档 | A.2-10 | 配置 3 行 | 提交前的最后一道卫生关口 |
| 8 | `UserGuide.md`/`.zh.md` 补一句"§2.5/§5.5 是子章节，不占主编号" | Gemini I-5 | 2 行 | 零风险，评审提得对 |

**小计：约 40 行代码 + 6 处文档，全部零风险。**

### E.2 值得做，但应单独排期（不阻塞当前提交）

| # | 条目 | 来源 | 规模 | 排期建议 |
|---|---|---|---|---|
| 9 | `requests` 口径的 `WindowConsumed` 改为精确公式 `ceil(mult) × Requests` | A.2-5 / Gemini I-1 | 小 | 下一个小版本。把一列"带脚注的估算"升级成"精确复现"，性价比很高 |
| 10 | §2.5 主表补"主要错误类"列（让 `rate_limit` 可见） | B-4 / Gemini II-2 | 小 | 与条目 3 配对做（一进一出，主表宽度不变） |
| 11 | 子表脚注写出实时计数器的来源路径 + 输入日志不在 `log_dir` 下时的提示 | B-2 | 小 | 下一个小版本。今天就能发生的误导路径 |
| 12 | 区分实时列 `-` 的两种成因（旧周期 vs 额度配置变更） | B-6 | 小 | 与条目 11 同批 |
| 13 | 报表窗口与计费周期无交集时加标记 | B-3 | 小 | 与条目 11 同批 |
| 14 | `ModelUsageStat` 遍历全部 attempts，让被 failover 掉的端点可见 + 修正 `Steps` 注释 | A.2-7（便宜的一半） | 小 | 独立一次。会改 golden 文件 |
| 15 | `ModelSwitch` 新增 Step 内切换检测（`WithinStep`） | A.2-7（完整的一半） | 中 | 独立立项。会改变 `MetricModelSwitchCount` 的口径，影响已有 corpus 统计的可比性 |
| 16 | `Live.Pct >= 100` 时的 `⭐` 标记 | Gemini I-2 | 极小 | 随手带上。Finding 已在报警，纯可读性 |
| 17 | `ModelSwitch` 切换行追加客观工具上下文 | Gemini II-3 | 小 | 排在条目 15 之后 |
| 18 | `cfgErr` 的重复警告合并成一条 | A.2-8 | 极小 | 随手带上 |
| 19 | `Bucket.PeriodStartTime` 注释的 UTC 措辞 | A.2-9 | 1 行 | 随手带上 |
| 20 | §5.5 规模可观测（进度输出打一行行数） | B-5 | 极小 | 随手带上 |
| 21 | DP2 §1 补一张"交付文件 → 计划条目"对照表 | B-7 | 文档 | 下次同规模 dev plan 之前 |

### E.3 建议并入方案第三梯队（长期挂起，附触发条件）

> 以下条目建议追加到 `vmr_report_provider_client_cost_analysis_sonnet-5.md` 第三梯队的表格里。
> **本报告不修改该文件**——下面是待并入的表格内容原文，供下一次编辑时直接采用。

| 条目 | 不做的理由 | 触发条件 |
|---|---|---|
| **完整的 Peak TPM / RPM 峰值统计** | 需要按分钟重新分桶（报表今天最细到小时），且"峰值"对不同厂商的窗口定义（滑动 vs 固定）不同，给出的数字看着精确实则不可比。而它 80% 的运维价值可以用一列"主要错误类"（`rate_limit` 计数）拿到——该列的数据 `ProviderRow.ErrorClasses` 早已算好并进了 JSON，只差渲染，已列入善后计划 E.2 条目 10 | 出现"错误率高但额度充裕"的真实运维案例，**且**"主要错误类"列已不足以定位 |
| **多实例部署下的集中式额度快照** | 本项目定位是 local-run 单二进制单进程（`CLAUDE.md` 首句、`/admin/status` 的 loopback-only 绑定、`rundir` 的 `~/.vmr` 默认值都建立在这个前提上）。且多实例下**先失效的是路由半区的额度执行本身**（N 个实例各自只看到 1/N 的消耗，全都不降权），报表偏小只是这个更根本问题的症状——让报表去读共享状态而路由不读，会得到"报表说 100% 但路由还在灌"的更糟状态 | 产品定位改变为支持多实例部署时，**且**必须先解决路由半区的额度状态共享，报表随之自然正确 |
| **`TokenWeights` 扩展为动态扣减规则对象**（按上下文长度分档 / 按 cache TTL 阶梯） | 属路由半区 `core.QuotaSpec` 的建模问题，不在分析半区范围。且在没有任何真实账户按这种规则计费之前做这个抽象是投机性通用化（KISS/YAGNI）。其"按上下文长度分档"的部分与本表既有的"输入长度分层分布"条目是同一个触发条件 | 与"输入长度分层分布"合并触发：出现按上下文档位分别计价的账户；**新增子场景**——出现按 cache 驻留时长（TTL）阶梯扣减的账户 |
| **§5.5 的 Top-N 截断** | 真实数据下 client 数量少，未观察到过长。预先做截断会引入"被截掉了多少"的显式标注义务（项目不做静默截断），成本大于当下收益 | E.2 条目 20 的行数观测显示 §5.5 稳定超过约 100 行时 |

---

## PART F：善后执行结果（2026-08-13，同日二次复核后落实）

**执行范围**：E.1 全部 8 项 + E.2 全部 13 项（含 A.2-7"便宜的一半"，即条目 14）。每一项落地前
都独立回源码复核，不采信本报告 PART A/B 原文的自述——过程中发现三处原分析需要改进，已在对应
条目的"结果"栏说明；两处发现存疑/有风险，按计划跳过，改动一律未做，单列在本节末尾等待决策。

**验证**：`gofmt -l ./internal ./cmd` 无输出、`go vet ./...` 无输出、`go build ./cmd/vmr` 成功、
`go test ./...` 全绿、`go test -race ./internal/router/... ./internal/quota/... ./internal/replay/...`
全绿、`go test ./internal/archtest/...`（行数预算 + 导入边界）全绿；另外用仓库自带的
`examples/sample-audit.jsonl` 跑过一次真实 `vmr report`，§2.5 主表新列、子表的 ⭐/†/‡ 三种标记、
来源路径脚注、跨实例提示在真实数据上全部按预期渲染（† 标记命中了 `dashscope`/`volcengine2`
两个账户——它们的计费周期与样例日志的时间窗确实不重叠，`volcengine` 因为周期覆盖了日志时间窗
而正确未被标记，是本次改动在真实数据上的一次有效验证）。

### E.1 落实结果

| # | 条目 | 结果 |
|---|---|---|
| 1 | 修正设计文档 §2.6 对 §7 判据的错误描述 | ✅ 已改写：把插在章节枚举中间的那句话拆成独立段落，并按条目 2 实际落地的双条件判据重写（不再是"与 Headroom<1 同源"这句不成立的话，改成"绝对阈值 + 与 Headroom 同源的相对项"） |
| 2 | §7 Finding 判据改为 `Pct >= 90 && Pct > PeriodElapsedPct` | ✅ 已改（`findings_quota.go`），新增 3 条边界测试（短周期健康账户不误报、Pct==PeriodElapsedPct 边界不报、真正超速时仍报） |
| 3 | 统一 §2.5 额度列（去掉主表那列） | ✅ 按推荐方案去掉主表额度列；`ProviderRow.Quota` 仍写入 JSON，只是不再渲染进 Markdown 主表；`quotaAmountStr`/`QuotaHdr`/`QuotaCell`/主表 `Disclaimer` 一并删除；DP2 §5 完成情况已补一段如实说明"这一条当时没有落地，本轮复核后修正" |
| 4 | `LiveQuota` 加回 Estimated/EstimatedCost | ✅ **比原建议更好的方案**：没有照字面加两个原始字段，而是把 `router/quota.go`（`QuotaStatus`）里已经验证过的 `EstimatedPct` 计算公式下沉到 `internal/quota/weight.go`，`report`/`router` 两个消费者复用同一份实现（同 `BaseAmount` 先例），避免复制一份微妙的按-metric 分支公式；渲染上按 `⭐` 惯例做成"12240（32.0% 估算）"内联标注 |
| 5 | 修正 `story/metrics.go` 陈旧注释 | ✅ 已改，并补充 `corpus.go` 已经写对的"十三项"措辞对齐 |
| 6 | cost 无定价渲染 `-` 而非 `0` | ✅ `WindowConsumed` 改 `*float64`；区分"有流量但全无定价"（nil/`-`）与"真的零流量"（真 0）两种情况，比原建议多覆盖了后一种边界 |
| 7 | `.gitignore` + 仓库卫生 | ⚠️ **部分完成**：`.gitignore` 已补 `/config.mba*.yaml`、`/vmr-quota.json`；**"删除 config.mba copy.yaml"未执行**——独立复核发现它不是字节级重复文件，内容与 `config.mba.yaml` 有实质差异（更早的版本，无 quota 配置），删除不可逆，改为仅通过 `.gitignore` 保护，不代为决定去留；**两份被删文档也未恢复或进一步处理**——找不到本轮之外的删除理由记录，详见本节末尾 |
| 8 | UserGuide 补充子章节说明 | ✅ 中英文各补一句，并顺带修正了 §2.5 描述里已经过时的"主表额度列"措辞（配合条目 3） |

### E.2 落实结果

| # | 条目 | 结果 |
|---|---|---|
| 9 | requests 口径精确公式 | ✅ 改为 `ceil(倍率) × 请求数`（对单位请求算一次 `ApplyModelMultiplier` 再乘请求数，而不是聚合后再 ceil），新增非整数倍率测试锁定 19×5.5 → 114（原聚合后取整公式会算出 105） |
| 10 | §2.5 主表补"主要错误类"列 | ✅ 已加，与条目 3 的"去掉额度列"配对，主表宽度基本不变；格式 `rate_limit 12(63%)`，百分比是该错误类占**失败尝试**的比例 |
| 11 | 子表脚注写来源路径 + 跨实例提示 | ✅ 已加：`Report2.Meta` 新增 `QuotaJSONPath`/`QuotaInputOutsideLogDir` 两个字段，由 `cmd_report.go`（唯一知道 `config.LogDir` 的composition root）在 `BuildCached` 返回后写入；跨实例提示按"输入日志全部不在 log_dir 下"触发，不是"任一不在"（避免误报"日志+一份旧归档"的正常混合场景） |
| 12 | 区分实时列 `-` 的两种成因 | ✅ 已加 `LiveConfigChanged` 标记（渲染为 `-‡`），专门覆盖"limitKey 不存在但该账户有其它 key"这一种情况；三种场景（同 key 陈旧周期 / 配置变更 / 完全无数据）各有独立测试 |
| 13 | 报表窗口与计费周期无交集标记 | ✅ 已加 `WindowNoOverlap`（渲染为 `†`），标准区间相交判定；`windowFrom` 为零值（本次零记录）时不判定，避免误报 |
| 16 | `Live.Pct >= 100` 的 ⭐ 标记 | ✅ 已加，边界为 `>= 100`（不是 `> 100`），已用测试锁定 |
| 18 | `cfgErr` 重复警告合并 | ✅ `cmdReport` 打一条统一警告，`buildPricing`/`buildProviderQuotas` 不再各自打印；两个函数新增"不应自行打印"的单元测试 |
| 19 | `PeriodStartTime` 注释措辞 | ✅ 已改成 "in time.Local, per time.Unix's own contract" |
| 20 | §5.5 规模可观测 | ✅ `aggregate.go` 新增一行进度输出 `§5.5: N client(s) x M endpoint row(s)`，真实数据验证过（样例数据渲染出 "2 client(s) x 3 endpoint row(s)"） |
| 21 | DP2 §1 交付文件对照表 | ✅ 已补进 DP2 文档的新 §1.1，按批次归类，跨批共用的文件也标注清楚 |
| 14（即 A.2-7"便宜的一半"） | `ModelUsageStat` 遍历全部 attempts | ✅ 已改：Step 内任意一次 attempt 命中的 `(provider, model)` 都计入 `Steps`（token 仍只归属最终解析出的那一个）；**实现过程中自己发现并修出一个 bug**——最初版本对"回退到 Manifest.Endpoint 解析"的路径只在首次创建条目时加一次 `Steps`，导致重复命中同一端点的多个 Step 被漏计（金样例文件的回归测试当场抓到：`agent(provider)` 从 3 步误算成 1 步），已修正并补了专门覆盖这个场景的回归测试 |

### 存疑/待决策事项（本轮未处理，需要用户决定）

以下两项在实施过程中发现原文档的判断不能直接采信，按"跳过、留到任务结束后单独说明"处理，
**没有做任何改动**：

1. **`config.mba copy.yaml` 是否删除**——A.2-10 原文认定它是"文件名带 copy 的临时误留副本"，
   但 `diff` 结果显示它与 `config.mba.yaml` 内容有实质差异（版本头 `2026-07-30`
   vs `2026-08-12`，缺少 quota/pricing 配置段），更像是改动前的一份旧版本快照，不是编辑器误产生
   的重复文件。它是 `.gitignore` 覆盖范围内的未跟踪文件，删除后 git 无法找回。已通过
   `.gitignore` 规则保护它不会被误提交，但去留本身留给用户判断。
2. **两份被删文档是否恢复**——`docs/dependabot_branch_cleanup_2026-08-11_sonnet-5.md`（分支清理
   与 PR 合并的操作记录）、`docs/future-strategy/VMR_综合评审与发展建议_report_v2.md`（2026-07-31
   的综合评审报告）在本次改动开始前就已经在工作区里被删除（`git status` 显示为未提交的
   `D`），但两份 dev plan 和方案文档都没有提到要删除它们，也没有 commit message 可查——找不到
   这是有意清理（例如后者已被内容更新的评审报告取代）还是意外操作的证据。`CLAUDE.md` 把评审
   报告类文档列为"预期会累积"的一类，无声删除与这条约定相抵触，但复核范围内也拿不出"这两份
   文档现在仍有价值"的实质理由。原样保留在工作区（未恢复，也未进一步删除），去留同样留给用户
   判断。

---

## 附：一句话总结

**代码是好的，文档欠了债。** 九个批次的功能实现经得起源码复核，三个预先识别的工程坑全部避开，
测试覆盖真实有效。真正需要立即处理的八件事里，有五件是"文档/注释说的与代码做的不一致"——包括
一处会让后来者基于错误前提做决策的判据描述。唯一一个**设计层面**的实质问题是 90% 固定阈值对
短周期账户的必然误报，而讽刺的是，方案自己在否决另一条 Finding 时已经写下了正确的判据
（"一个会对正确配置持续报警的检测器，只会训练用户忽略整个 §7"），只是没拿它检查自己新增的这条。

**（2026-08-13 同日更新）** 上述 E.1 全部 8 项与 E.2 全部 13 项已按 PART F 落实，测试与真实数据
双重验证通过。仓库卫生的两个子项（`config.mba copy.yaml` 去留、两份被删文档去留）在复核后判定
为存疑，未擅自决定，留待用户裁决——详见 PART F 末尾。
