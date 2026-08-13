<!-- Ver 2026-08-13 14:20, by Opus 5 -->

# 第一/第二梯队 + 善后批次的终审复核、方案自省与收尾计划

**性质**：对当前工作区**全部未提交改动**的独立终审。本文是唯一的输出载体；除两处明确说明的例外
（对 Gemini 评审文档的就地批注、方案第三梯队表的追加），不改动任何既有文件。

**核查对象**：

1. `docs/future-strategy/vmr_report_provider_client_cost_analysis_sonnet-5.md`（方案）
2. `docs/future-strategy/vmr_report_story_cost_dimensions_devplan_opus-5.md`（DP1，一梯队三批）
3. `docs/future-strategy/vmr_quota_visibility_devplan_opus-5.md`（DP2，二梯队六批）
4. `docs/future-strategy/vmr_tier12_delivery_review_and_followup_sonnet-5.md`（前一轮复核 + 善后 E.1/E.2 落地记录）
5. `docs/future-strategy/vmr_comprehensive_code_and_design_review_report_gemini-3.6-flash.md`（外部评审）
6. 工作区全部未提交改动（35 个修改文件 + 27 个未跟踪新文件）

**核查方法**：每一条结论都回源码实测。**不采信任何文档的自述**——包括前一轮复核报告 PART F 的
"✅ 已落地"、DP1/DP2 各批的"完成情况"，以及 Gemini 报告的全部数字。凡本文给出行数、公式、渲染
形态，均为本次执行结果。

**基线验证（本次实测）**：

```
gofmt -l ./internal ./cmd                                     → 无输出
go vet ./...                                                  → 无输出
go test ./...                                                 → 全绿
go test ./internal/archtest/...                               → 全绿（行数预算 + 导入边界）
go test -race ./internal/router/... ./internal/quota/... ./internal/replay/...  → 全绿
```

---

## 0. 结论摘要

**总体判断：功能实现扎实、测试覆盖真实有效，善后批次修掉的都是真问题；但"重算列"这条主线上
留下了一个链条性的缺口——公式统一了，喂进公式的口径没统一，也从来没有被验证过。**

前一轮复核的结论是"代码是好的，文档欠了债"。经过善后批次，文档债基本清了；**这一轮的结论是
"公式是对的，口径是错的"**：`§2.5` 子表最左边那一列（本报表窗口消耗）在 `requests` 口径下用
`e.Requests`（**全部**请求，含路由半区从未记账的失败请求）作为基数，而路由半区只在成功响应上
记账；`tokens` 口径下则相反——usage 没嗅探到的请求在报表里贡献 0，路由半区却按字节数估算记了账。
一个系统性高估、一个系统性低估，而**善后批次刚刚把这一列从"带脚注的估算"升级成了"精确复现"**。

| 类别 | 数量 | 最严重的一条 |
|---|---|---|
| 新发现的代码缺陷 | 8 | `requests` 口径用 `e.Requests` 而非 `e.OK`——刚宣称"精确复现"的那一列并不精确（N-1） |
| 方案/DevPlan **自身**的缺口 | 6 | 从来没有一份计划问过"重算列怎么证明它算对了"，验收标准是一条脚注（P-1/P-5） |
| 文案与代码不符 | 1 | `-‡` 的中英文案都说"在本周期内被改过"，代码没有任何时间限定（N-3） |
| 前轮已修但仍值得记的 | 0 | E.1 八项 + E.2 十三项全部实测确认落地，无返工 |

**明确排除**（避免后来者重查）：无功能性回归、无并发问题、无导入边界破坏、无行数预算越界、
无确定性排序缺失、无 i18n 中文泄漏进英文产物。DP1/DP2 预先识别的三个坑（虚拟模型名、旧日志 `$`
漂移、`render_spine.go` 行数）全部真实避开，守门测试真实存在且有效。

**Gemini 评审的净贡献**：6 条设计缺口里 **1 条完全成立且比它自己描述的更严重**（C.2 的
`vmr-quota.json` key 永生 → 直接导致 N-3 的文案错误）、**1 条指向真缺口但解法不对**（C.6 想加日均
速率，真正缺的是"报表窗口本身根本没显示"）、**2 条不成立**（C.1 重复已否决、C.4 前提自相矛盾）、
**2 条方向对但成本被低估**（C.3 数据不存在、C.5 不便宜）。它的符合度核查部分**引入了一处新的行数
错误**（`aggregate.go` 报 965，实测 977）——正是它上一轮被指出的同一类错误。

---

## PART A：实现 vs 计划的符合度（实测）

### A.1 逐批对齐

| 批次 | 计划要求 | 实测结果 | 判定 |
|---|---|---|---|
| DP1 批 1 | Provider 汇总，`DurMSMean` 代分位，事后派生 | `provider.go`/`section_provider.go`/`i18n/report_provider.go` 齐备；`buildProviders` 在 `EndpointsAll` 填完之后调用（`aggregate.go:637`，`finishEndpoint` 循环在 617-622） | ✅ |
| DP1 批 2 | Client×完整端点标签，collector 模式 | `clientendpoint.go` 用 `clientKey\x00endpoint` 为 key；三级 tie-break 完整 | ✅ |
| DP1 批 3 | 取端点真实模型，不碰 `render_spine.go` | `stepUpstream` 优先结构化字段；`render_spine.go` 仍 379 行 | ✅ |
| DP2 批 0 | `pctStr64`、单次 `config.Load`、多 Limit 前提、一句注释 | 四项均在；`cmdReport` 确实只有一处 `config.Load` | ✅ |
| DP2 批 1 | 加权公式下沉，零行为变更 | `weight.go` 逐字迁移 + `EstimatedPct`；`router` 本地实现已删净（`math` import 一并移除） | ✅ |
| DP2 批 2 | §2.5 子表 + 主表去掉额度列 | 子表落地；主表额度列已在善后批次删除（`quotaAmountStr`/`QuotaHdr`/`QuotaCell` 全删净） | ✅（善后修正后） |
| DP2 批 3 | 阈值 Finding，只认实时计数器 | `findings_quota.go` 双条件判据；`Live == nil` 不命中 | ✅ |
| DP2 批 4 | 6 处登记 + twelve→thirteen | 6 处齐备；`corpus.go` 两处注释、设计文档、UserGuide 中英文均已改 | ✅ |
| DP2 批 5 | 端点标签下沉，不改任何调用点行为 | `EndpointLabel`/`SplitEndpointLabel` 落地；2 产地 + 1 解析点迁移，收敛已如实记录 | ✅ |
| 善后 E.1 | 8 项 | 逐项回源码确认，含 #7 的部分完成（`.gitignore` 已补、`config.mba copy.yaml` 未删——按计划留待用户裁决） | ✅ |
| 善后 E.2 | 13 项 | 逐项回源码确认；#15/#17 按计划**未做**（独立立项），前一轮报告的表格里正确地没有列它们 | ✅ |

### A.2 行数预算实测（口径：`grep -c ''`，与 archtest 的 `bytes.Count(data,"\n")` 一致）

| 文件 | 实测 | 预算 | 余量 | 备注 |
|---|---|---|---|---|
| `internal/story/render_spine.go` | 379 | 380 | **1** | 全仓库最危险，本轮全程未碰 ✅ |
| `internal/report/aggregate.go` | **977** | 1000 | **23** | 见 N-9：比 DP1 起点（960）净增 17 行，计划总额是 +5 |
| `internal/story/metrics.go` | 414 | 470 | 56 | |
| `internal/story/corpus.go` | 358 | 380 | 22 | |
| `internal/story/compare.go` | 748 | 850 | 102 | |
| `internal/report/render_doc.go` | 226 | 400 | 174 | |
| `internal/report/detail.go` | 1062 | 1150 | 88 | 本轮未改 |

### A.3 测试覆盖实测

新增/扩展测试文件 11 个、1819 行。抽查了关键守门测试是否**真的**守得住：

- `TestComputeModelUsage_DetectsSwitchDespiteConstantVirtualModel`——虚拟模型名全程不变、attempts
  真实模型改变，确实检出切换。**这是 DP1 坑一的守门人，有效。**
- `TestBuildProviderQuotaRows_RequestsMetric_NonIntegerMultiplierExactlyMatchesRouter`——锁定
  19×5.5 → 114。**公式方向正确，但基数选错了，见 N-1：这条测试锁住了一半的正确性，恰好放过了另一半。**
- `TestBuildProviderQuotas_StalePeriod_LiveStaysNil` / `_ConfigChanged_FlagsDistinctlyFromStalePeriod`
  / `_NoDataAtAll_LiveConfigChangedStaysFalse`——三种 `-`/`-‡` 成因各有独立断言，有效。
- `TestQuotaExhaustionFinding_EqualUsedAndElapsedDoesNotFire`——双条件的边界，有效。
- `TestComputeModelUsage_RepeatedSameEndpointStepsAllCounted`——前一轮 PART F 自己发现并修掉的
  `Steps` 漏计 bug，回归测试真实存在。

---

## PART B：本轮新发现的问题

按严重度排序。每条：**问题 → 实证 → 后果 → 建议**。

---

### 🔴 N-1 `requests` 口径的"精确复现"用错了基数：`e.Requests` 应为 `e.OK`

**实证**。`internal/report/providerquota.go:53-69`：

```go
case core.MetricRequests:
    unit, _ := quota.ApplyModelMultiplier(ref.Spec, model, quota.Counters{Requests: 1}, 0)
    d := quota.Counters{Requests: unit.Requests * int64(e.Requests)}
```

而路由半区的记账时机是 `internal/router/router.go:525`——`rt.chargeQuota(ep, ...)` 在
`forwardSuccess` 里，**只有拿到 2xx 响应的那次 attempt 才记账**。

`internal/report/rows.go:218` 对 `EndpointRow.Requests` 的注释原文是
"requests this endpoint actually served (request-level, ≠ Attempts)"，`aggregate.go:291-294` 的
`addEndpointReq` 无条件 `e.Requests++`，只有 `rc.outcome == "ok"` 才 `e.RequestsOK++`。也就是说
**一个所有 attempt 都失败的请求，仍然给最后那个端点计了 1 次 `Requests`，而路由半区一分钱没记。**

**正确的基数是"进入过 `forwardSuccess` 的 attempt 数"**，而 `aggregate.go:263-265` 的 `addAttempt`
里已经有一个非常接近的计数：

```go
e.Attempts++
if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
    e.OK++
```

**但 `e.OK` 也不是恒等式，差一点点**：一个 2xx 响应在转发途中流断掉时，`router.go:519` 的
`att.SetTruncated(copyErr)` 会写入 `a.Error = "truncated: …"`（`audit.go:264`），于是这次 attempt
**不计进 `e.OK`**——而 `chargeQuota` 在同一段代码里明确写着"Charged here regardless of copyErr"，
**它记了账**。所以三个候选基数与真值的关系是：

| 基数 | 与路由半区实际记账数的偏差 | 方向 |
|---|---|---|
| `e.Requests`（当前实现） | 多算「所有 attempt 都失败的请求数」 | **高估** |
| `e.RequestsOK` | 漏算「上游 2xx 但整体 outcome 非 ok（canceled/truncated）的请求」 | 低估 |
| `e.OK` | 漏算「2xx 但流截断的 attempt」 | 略低估 |
| `Response != nil && Status < 400` 的 attempt 数 | **无偏差——这就是 `forwardSuccess` 的触发条件本身** | **恒等** |

最后一个 `EndpointRow` 上还没有，但它就在 `addAttempt` 手边：现有的 `if` 条件去掉
`a.Error == ""` 这一项即可，多一个计数器 3 行。也就是说：

```
路由半区实际记账总量 ≡ (Response!=nil && Status<400 的 attempt 数) × ceil(mult)   ← 恒等式
当前实现              =  e.Requests × ceil(mult)   ← 多算「彻底失败的请求数 × ceil(mult)」
```

**后果**。善后批次 E.2 条目 9 的整个立论是"把这一列从带脚注的估算升级成精确复现"，i18n 文案也
照此写死了（`report_provider.go` 中文："requests 口径按 `ceil(倍率) × 请求数` **精确复现**路由半区的
记账公式，不受此影响"）。实际上它在另一个轴上仍然系统性高估，**且偏差量级远大于它刚修掉的那个**：
`volcengine`（`deepseek-v4-pro: 5.5` → `ceil = 6`）如果有 20% 的请求彻底失败，高估 20%；而刚修掉的
聚合取整偏差在真实数据上是 8%。**修了小的，留下了大的，还宣布精确了。**

更糟的是这条脚注同时把"失败尝试不计费"列为"已知出入来源"——**这条出入本来是可以彻底消除的，
消除它所需的字段（`e.OK`）就在同一个结构体上、同一个循环里。**

**建议**（二选一）：

- **完整版（推荐，约 5 行 + 1 条测试）**：`EndpointRow` 增一个 attempt 级计数器（暂名
  `Forwarded`），在 `addAttempt` 里按 `a.Response != nil && a.Response.Status < 400` 累加
  （即现有 `OK` 的条件去掉 `a.Error == ""`），`providerquota.go` 改用它。**这是恒等式，
  才配得上"精确复现"这个说法。**
- **最小版（1 行）**：先改用 `e.OK`。它消掉了主要偏差（彻底失败的请求），只剩"2xx 但流截断"
  这个小得多的反向残差。若选这条，脚注里"失败尝试不计费"必须保留、并补一句"流截断的成功响应
  已记账但本列未计"。

无论选哪条，`requests` 口径下"失败尝试不计费"这条免责说明的措辞都要重写——完整版下它彻底不再
成立（该从 `requests` 的列表里删掉），最小版下它的含义变了（从"高估的来源"变成"低估的来源"）。
`tokens`/`cost` 两个口径不受影响，那两个口径的分量本来就只在有 usage 的成功请求上累计。

---

### 🟠 N-2 `tokens` 口径反向漏算：usage 未嗅探的请求，报表计 0、路由半区按字节估算记账

**实证**。报表侧 `aggregate.go:295-301`：token 四分量**只在 `rc.usageOK` 为真时**累加。
路由半区侧 `internal/router/quota.go` 的 `tokenCharge`：

```go
// 上游没返回 usage / 响应不透明 / 流被截断时，降级为字节数估算
return quota.Counters{Fresh: inEst, Out: outEst}, inEst + outEst
```

即：**同一批请求，报表贡献 0 token，路由半区记了一笔（估算的）账。**

**后果两层**：

1. `tokens` 口径的重算列系统性**低估**（与 N-1 的高估方向相反，两者不会互相抵消——它们在不同口径上）。
2. **这条出入源既不在渲染层的脚注里，也不在 `ProviderQuotaRow` 的文档注释里。**
   脚注列了四条已知出入（失败尝试、config 中途变更、cost 定价时点、逐请求 ceil），**唯独漏了这一条**
   ——而它恰恰是唯一一条读者完全无法从表面察觉的（前四条都有外部线索，这一条没有）。

**且这里还藏着 A.2-6 那条纪律的一个未覆盖分支**：善后批次把 `metric: cost` 的"有流量但全无定价"
做成了 `nil → -`（区别于真零流量的 `0`）。但 `tokens` 口径下"有流量但 `TokensKnown` 全为 0"是完全
同构的情形——重算列会渲染成 `0`，读者合理地读成"这个账户这段时间没消耗"。**同一条纪律只贯彻了一半。**

**建议**（两步，都小）：

- 脚注的中英文各补一句"上游未返回精确 usage 的请求，路由半区按字节数估算记了账，本列计 0"。
- `tokens` 分支参照 cost 分支引入 `tokensSawTraffic`/`tokensAnyKnown`：有流量但 `TokensKnown` 全为 0
  时同样渲染 `-`。约 10 行 + 1 条测试，与 cost 分支完全同构。

---

### 🟠 N-3 `-‡` 的中英文案都声称"在本周期内被改过"，代码没有任何时间限定

**实证**。`cmd/vmr/cmd_report.go:212` 的判据是纯存在性判断，**不含任何时间条件**：

```go
} else if _, exists := live[p.Name][limitKey]; !exists && len(live[p.Name]) > 0 {
    ref.LiveConfigChanged = true
```

而 `internal/quota/store.go` 的 `Registry` 是**整份 map 读入、整份 map 写回**（`r.accounts = ff.Accounts`
/ `fileFormat{Accounts: r.accounts}`），**从不删除任何 key**。于是：

> 半年前把 `every: 1mo` 改成 `1w`，旧的 `requests/1mo` 桶会**永远**留在 `vmr-quota.json` 里，
> 于是 `-‡` 会在**此后每一次** `vmr report` 上出现。

但两种语言的文案都写死了时间限定：

- 中：`> ‡ 该账户的 quota: metric/every **在本周期内**被改过——盘上还留着旧配置写下的计数器……`
- 英：`> ‡ This account's quota: metric/every changed **during the current period** ……`

**后果**。这是 E.1 条目 1 修掉的那一类缺陷（"文档/文案说的与代码做的不一致"）在同一批改动里的
**新实例**，而且更隐蔽：它不在设计文档里，在用户每次都会读到的产物脚注里。一个半年前改过配置的
用户，会每次都读到"本周期内被改过"，然后去找一次并不存在的近期变更。

**且这正是 Gemini C.2 真正指向的问题**——它把它描述成"磁盘浪费 + 技术债噪音"，实际后果比这重：
**是一个会对健康系统持续显示、且措辞错误的标记**（方案第三梯队否决"Client 单点倾斜 Finding"的
同一条理由：会对正确配置持续报警的东西只会训练用户忽略它）。

**建议**（分两层，第一层必做）：

1. **文案改对**（2 行）：去掉"在本周期内 / during the current period"，改成"该账户的 `quota:`
   metric/every 曾被改过（改动时间无法从计数器文件判断）——盘上留着旧配置写下的计数器……"。
2. **考虑加时间限定**（可选，见 E.2）：只有当旧 key 的 `PeriodStartTime()` 落在当前周期或上一个
   周期内时才打 `-‡`，更早的一律按"无数据"处理。数据都在 `Bucket` 上，约 5 行。这比 Gemini 提议的
   `vmr quota gc` 便宜得多，也不需要碰路由半区的写路径。

---

### 🟠 N-4 重算列与路由记账之间没有任何差分测试——N-1 正是这个缺口放进来的

**实证**。DP2 批 1 的立意是"一份实现，两个消费者"，落地也确实做到了：`BaseAmount`/
`ApplyModelMultiplier`/`EstimatedPct` 三个函数现在只有一份。**但被统一的是公式，不是口径。**
"哪些原始计数喂进公式"这件事在两边各写了一遍：

| | 路由半区 | 报表半区 |
|---|---|---|
| requests | `Counters{Requests: 1}`，每次进入 `forwardSuccess` 一次 | `Counters{Requests: e.Requests}`（N-1：多算了彻底失败的请求） |
| tokens | `respStream` 嗅探的 usage，嗅不到则字节估算 | `e.TokensInFresh/Cached/CacheWrite/Out`，嗅不到则 0（N-2） |
| cost | `componentCost(d, EffectiveRate(ep.PricingRate))` | `Σ e.CostEstimate`（报表自己的定价解析） |

三行里有两行不等价，**而现有测试一条都发现不了**——`providerquota_test.go` 的 14 条用例全部是
"给定 `EndpointRow`，断言 `buildProviderQuotaRows` 的输出"，路由半区从头到尾没出现在断言的另一侧。
`TestBuildProviderQuotaRows_RequestsMetric_NonIntegerMultiplierExactlyMatchesRouter` 这个名字里带着
"ExactlyMatchesRouter"，实际断言的却是一个**手算的期望值**，不是驱动 `router.ChargeResponse` 得到的
真实值。

**后果**。这一列的正确性目前**没有任何自动化保障**，只有代码注释里的推导。而这类"两边各推导一遍"
的等价性，正是本项目 CLAUDE.md 反复警告的、并且已经被坑过一次（`report` 私有会话分组 vs `ctxgraph`）
的那一类。

**建议**（这是本文最推荐的一条）。写一个**差分测试**，放在 `internal/report` 之外的一个能同时
import `router` 与 `report` 的测试包（或直接在 `cmd/vmr` 的测试里）：

```
给定一组合成的 (endpoint, model, 成功/失败, usage) 记录：
  A) 逐条驱动 router.ChargeResponse 到一个真实 quota.Registry，读出 BaseAmount(spec, Used(...))
  B) 把同一组记录聚合成 EndpointRow，跑 buildProviderQuotaRows
断言 A == B（requests 口径应完全相等；tokens 口径给一个明确的、被文档化的容差）
```

这条测试会**当场抓到 N-1 和 N-2**，且此后每一次口径改动都受它保护。它也正好是 Gemini C.4
（`DiscrepancyPct` 告警）真正该有的形态——把校验放在测试里，而不是放在给用户看的报表列里。

---

### 🟡 N-5 子表给了"周期区间"，却从不显示"本报表窗口"是哪一段

**实证**。`renderProviderQuotaTable` 的 8 列表头（中）：
`账户 | metric | 本报表窗口消耗¹ | 本周期已用² | 上限 | 已用% | 周期已过% | 周期区间`。
右边那个数字配了完整的区间（`07-22 ~ 08-22`）和进度百分比；**左边那个数字什么时间锚点都没有**。
`Meta.From`/`Meta.To` 确实算出来了（`aggregate.go:594`），也确实传进了 `buildProviderQuotaRows`
（用来判 `WindowNoOverlap`），但**从未渲染到这张表上**——读者要知道左边这个数字覆盖多长时间，
得翻到报表附录去找。

**后果**。这张表存在的全部理由是"两个不同窗口的数字并排、各自标注来源"。现在**只有一个窗口被标注了**。
一个"本报表窗口消耗 = 30000"的数字，覆盖 3 小时还是 30 天，含义相差 240 倍，而表上看不出来。

**这也是 Gemini C.6（日均速率）真正指向的东西**，但它开的药方不对：加一个 `10.0k/day` 派生量，
不如直接把已经算好的窗口区间摆出来——**后者是事实，前者是把事实除以一个读者看不见的数**。

**建议**。在子表的 `¹` 脚注里补一句该报表窗口的区间与时长（`本报表窗口：08-10 ~ 08-13（3.0 天）`），
数据现成（`rep.Meta.From`/`To`），不加列、不加宽度。约 5 行。若之后仍觉得需要日均速率，那时读者
已经有了心算所需的两个数。

---

### 🟡 N-6 三个标记，两套渲染约定：`⭐` 的脚注无条件输出

**实证**。`section_provider.go:146-154`：

```go
w("\n%s", t.WindowFootnote)
w("%s", t.StalePeriodFootnote)
if anyConfigChanged { w("%s", t.ConfigChangedFootnote) }
w("%s", t.OverQuotaFootnote)        // ← 无条件
if anyNoOverlap    { w("%s", t.NoOverlapFootnote) }
```

`‡` 和 `†` 的脚注都按"真的有行带这个标记"门控，`⭐` 的没有。于是一份所有账户都健康的报表，
底下照样挂着一句"⭐ 已用% ≥ 100% 时的标记：该账户本周期已超出配置的额度上限"。

**后果**。轻微，但它是三个同批引入的标记里唯一不守自己约定的那个，且会让健康报表读起来像有事。

**建议**。加一个 `anyOverQuota` 门控，与另外两个对齐。约 3 行。（`WindowFootnote`/
`StalePeriodFootnote` 是列本身的说明而非标记说明，无条件输出是对的，不要一起改。）

---

### 🟡 N-7 同一条格式化规则，两份实现——A.2-2 修掉的缺陷跨包重现

**实证**。`internal/report/render_cells.go`：

```go
func numStr(v float64) string {
    if v == math.Trunc(v) { return strconv.FormatInt(int64(v), 10) }
    return strconv.FormatFloat(v, 'f', 2, 64)
}
```

`internal/i18n/report_provider.go`：

```go
func liveUsedNumStr(v float64) string {
    if v == float64(int64(v)) { return strconv.FormatInt(int64(v), 10) }
    return strconv.FormatFloat(v, 'f', 2, 64)
}
```

同一张表的两列（`上限`/`本报表窗口消耗` 走 `numStr`，`本周期已用` 走 `liveUsedNumStr`），
**两份实现，连判整数的写法都不同**。`liveUsedNumStr` 的注释诚实说明了理由（i18n 是零依赖叶子包），
理由本身成立。

**后果**。当前两者对现实取值行为等价，所以这是**潜伏风险不是活跃 bug**。但 A.2-2 花力气修掉的
正是"同一个 `Amount` 在两处用两个 formatter 渲染成两个字符串"——那次是跨表，这次是跨包，
同一张表内。任何一边以后改精度，另一边不会跟着改，也没有测试会红。

**建议**（二选一，都便宜）：

- **推荐**：让 `FormatLiveUsed` 只负责"要不要加估算标注"，数字本身由 `section_provider.go` 用
  `numStr` 格好了传进去（签名从 `func(used, estimatedPct float64) string` 改成
  `func(usedStr string, estimatedPct float64) string`）。i18n 保持零依赖，formatter 回到一份。
- 或：在 `internal/i18n` 补一条测试，断言两个函数在一组样本上输出相同——但这需要 i18n 的测试
  import `report`，反向依赖，不干净。**选前者。**

---

### 🟡 N-8 `allPathsOutsideDir` 是纯词法比较，macOS 的 symlink 会产生假的跨实例警告

**实证**。`cmd/vmr/cmd_report.go:242-264` 用 `filepath.Abs` + `filepath.Rel`，**没有
`filepath.EvalSymlinks`**。macOS 上 `/tmp` 是 `/private/tmp` 的符号链接（本项目的 scratchpad 路径
就是这个形态）；`~` 下经由 symlink 挂载的目录同理。于是：

> `config.yaml` 的 `log_dir: /tmp/vmr/logs`，命令行传入的 glob 被 shell 展开成
> `/private/tmp/vmr/logs/vmr-audit-*.jsonl` → `filepath.Rel` 算出 `../../private/tmp/...`
> → 判定"全部在 log_dir 之外" → 打出跨实例警告。

**后果**。同一台机器、同一个实例、完全正确的用法，被警告成"实时列可能来自另一台机器"。这与 B-1
（90% 固定阈值对短周期账户误报）是同一个失效模式，只是这次落在警告文案上而不是 Finding 上。

**注意它不影响默认路径**：不传位置参数时 `resolveInputPaths` 直接从 `cfg.LogDir` 拼路径，两边同源，
不会误触发。踩中的是"显式传 glob"这一种用法。

**建议**。对 `dir` 和每个 `p` 都先试一次 `filepath.EvalSymlinks`，失败则回退到 `Abs`（文件可能已被
压缩改名，`EvalSymlinks` 对不存在的路径会报错，必须回退而不是放弃判断）。约 8 行 + 1 条测试。

---

### 🟢 N-9 `aggregate.go` 净增 17 行（计划总额 +5），B-5 的观测代码放错了地方

**实证**。DP1 §0 记录起点 960 行，本次实测 **977**。计划额度：DP1 批 1 `+1`、DP1 批 2 `+3`、
DP2 批 2 `+1`，合计 `+5`。多出来的 12 行几乎全部来自善后 E.2 条目 20（B-5 的 §5.5 规模可观测）：

```go
if progress != nil && len(rep.ClientEndpoints) > 0 {
    clients := map[string]bool{}
    for _, r := range rep.ClientEndpoints { clients[r.ClientKey] = true }
    fmt.Fprintf(progress, "§5.5: %d client(s) x %d endpoint row(s)\n", len(clients), len(rep.ClientEndpoints))
}
```

**后果**。不越界（余量 23 行），但 DP1 §5 风险表对这个文件写的原话是：

> **若实施中发现需要更多行，是设计跑偏的信号，应把逻辑挪进新文件，而不是抬高预算数字**

一个"客户端去重计数"的循环，是 `clientendpoint.go` 的职责，不是聚合主流程的。它现在占着全仓库
第二紧的行数预算。

**建议**。`clientEndpointCollector` 增加一个 `clientCount() int`（它本来就持有 `byKey`，
从 key 前缀数或另存一个 set 都行），`aggregate.go` 退回 3 行。约 10 行净移动，零行为变更。

---

### 🟢 N-10 设计文档 §2.6：`§7` 的具体检测器被解释在"§7 是什么"之前

**实证**。E.1 条目 1 把 `provider_quota_exhaustion` 那段从枚举中间拆出来独立成段（这一步是对的），
但插在了**介绍 §7 本身的那段之前**。现在的阅读顺序是：

1. 章节运行顺序枚举（…`§7` 效率与浪费、`§8` 请求详单入口，外加一段附录）
2. **`§7` 的 `provider_quota_exhaustion`（批 3）只认这份实时计数器……**（具体检测器）
3. `§7` 的自动发现表（`buildFindings`）扫描已完成聚合的各桶……（`§7` 是什么）

**建议**。把 2 和 3 对调。纯编辑，1 处。

---

### ⚪ N-11 `computeModelUsage` 的早退会连带丢掉可解析的早期 attempt（不建议改，记录备查）

`modelusage.go:73-76`：`stepUpstream(s)` 返回空就 `continue`，**跳过整个 attempt 循环**。
构造上存在这样一个 Step：最后一次 attempt 没有结构化 `Provider`/`Model`、`Manifest` 为 nil，
但**更早**的 attempt 有结构化字段——这些 attempt 会被一并丢掉。

实践中不可达（`Manifest` 为 nil 的 Step 不会进 Journey；结构化字段是逐 attempt 一起写的），
改动会让这个函数的两种取值路径纠缠。**记录备查，不建议改。**

---

## PART C：方案与 DevPlan **自身**的缺口

这一部分不看代码是否照做，只问"照做了也仍然不够好的地方在哪"。

---

### 🔴 P-1 从来没有一份计划问过"重算列怎么证明它算对了"

方案 §4.3、DP2 §5.2、`ProviderQuotaRow` 的文档注释、渲染层的两条脚注——四处都在**描述**重算列
与路由记账之间可能有哪些出入，措辞一次比一次诚实。**但没有任何一处提出过一个可执行的验收标准。**

DP2 §9 的"收尾验证"第 2 条是这样写的：

> 真实数据冒烟：`volcengine` 实时列显示 12240/18000（68.0%），「周期已过%」（70.9%）与日历一致。

这**只验证了实时列**——而实时列是对一个 JSON 文件的直读，本来就没什么可错的。**重算列（这一批
真正的新计算）从头到尾没有被验证过任何一次**，包括善后批次把它改成"精确复现"的那一次。

N-1 与 N-2 是这个缺口的直接产物：两个方向相反的口径错误，在两轮 dev plan、一轮交付复核、一轮外部
评审之后仍然完好无损地留在代码里——因为**从来没有人被要求拿它跟任何东西对过**。

**建议**：N-4 的差分测试。同时把"新增一个从审计日志重算路由半区行为的指标时，必须有一条驱动路由
半区真实代码路径的差分测试"写进 DP 模板/CLAUDE.md 的约定。

---

### 🔴 P-2 批 1 的"一份实现，两个消费者"只做到了一半，而计划把这当成了终点

DP2 §0.1 判定精确加权"成本被高估"的依据是：`baseAmount`/`applyModelMultiplier` 已经是纯函数，
下沉是机械操作。**这个判断本身完全正确**，落地也确实零行为变更。

但计划**把"公式统一"等同于"口径统一"**，于是没有人去问下一个问题：*公式的输入从哪来？*
实际答案是——路由半区从 `respStream` 的逐请求嗅探来，报表半区从 `EndpointRow` 的聚合桶来，
两者的对应关系在任何一份文档里都没有被写下来过。

这个缺口的形状值得记：**它不是"忘了做"，是"做完了一半之后，剩下那一半不再看起来像是同一件事"。**
下沉了三个函数之后，`internal/quota` 看上去已经是"额度口径的唯一权威"了，于是
`providerquota.go` 里 `quota.Counters{Requests: int64(e.Requests)}` 这一行——真正做口径决策的那一行
——读起来像是无关紧要的胶水代码。

**建议**：在 `internal/quota/weight.go` 的包/函数注释里明确写出每种 metric 的**输入契约**
（"requests 的 `Counters.Requests` 必须是成功记账次数，不是请求数"），让下一个调用方在填参数时
就能看到约束，而不是靠读 router 的代码反推。

---

### 🟠 P-3 `vmr-quota.json` 的 key 生命周期从来没有主人

三份文档都把 `vmr-quota.json` 当成一个**只读的事实源**来设计（DP2 §5.2 的陷阱处理、B-6 的
`-‡` 标记都是"读到什么就怎么显示"），**没有任何一处规定过谁负责清理它**。
`quota.Registry` 是整份读入、整份写回、从不删 key（实测确认），于是一次 `every:` 修改会在这个文件里
留下一个永久的孤儿桶，并让报表**永久**显示一个措辞错误的 `-‡`（N-3）。

**Gemini C.2 的观察成立，但它开的药方（`vmr quota gc` 或加载期清理）过重**：清理是**写**操作，
会把一个纯读路径变成读写路径，还要处理"报表进程与路由进程并发写同一个文件"（`Registry.Flush` 有
temp-file+rename，`vmr report` 没有理由参与这件事）。

**正确的归属是路由半区**：`Registry.Load` 之后、第一次 `Flush` 之前，用当前配置解析出的合法
`limitKey` 集合过滤一次——这是路由进程本来就要做的事（它是这个文件的唯一写者），成本约 10 行，
且完全不影响报表。**这条不属于本梯队**（碰路由半区），建议登记为 P3 多窗口额度改造的同批工作。

**报表侧现在就该做的只有 N-3 的文案修正。**

---

### 🟠 P-4 `§2.5` 的可靠性列少了方案 §4.2 承诺的第二项，且它正是"浪费"的另一半

前一轮复核的 B-4 指出方案 §4.2 承诺了"尝试数 / 成功率 / **错误分类** / **失败尝试墙钟耗时**"四项，
实现只渲染了成功率和错误率。善后批次补了"主要错误类"——**但 `WastedMS` 仍然没有渲染**，
且前一轮的建议里也只提了错误类。

实测：`ProviderRow.WastedMS` 已算好、已进 `vmr-report.json`、`section_provider.go` 一个字没渲染。

**为什么它值得补**：`§2.5` 现在能回答"这个账户错得多不多"，但回答不了"错得**贵**不贵"。
一个错误率 5% 但每次失败都卡满 60s 超时的账户，和一个错误率 30% 但失败在 200ms 内的账户，
对"该不该继续给它导流量"这个判断的影响完全相反——而这正是 `§2.5` 存在的理由。`WastedMS` 就是
这个区分度，数据现成。

**建议**：主表补一列"失败耗时"（`fmtDurMS(p.WastedMS)`）。若担心宽度，它与"主要错误类"合并成
一列（`rate_limit 12(63%) · 4.2m`）也可以。约 5 行。

---

### 🟡 P-5 三份计划都没有定义"这张表在什么规模下不再可读"

B-5 补了 §5.5 的行数观测（好），但：

- **没有补 §2.5 子表自己的**——它的行数 = 配了 `quota:` 的账户数，通常个位数，确实不急；
- **观测量只进了进度输出，没进产物**。进度输出是一次性的、不会被保存；一个把 `vmr-report.md`
  发给同事看的人，看不到"这份报表的 §5.5 有 400 行"这个事实。

**这是一个元层面的缺口**：项目有"不做静默截断"的纪律，但没有"规模必须可追溯"的对应纪律。
`Report2.Meta` 是这类事实的天然归宿（`QuotaJSONPath`/`QuotaInputOutsideLogDir` 刚刚开了先例）。

**建议**：低优先级。若将来 §5.5 真的过长，把行数写进 `Meta` 而不是只打到 stdout。

---

### 🟡 P-6 "十三项标量"的口径变更没有留下可比性说明

批 4 把 `MetricModelSwitchCount` 登记成第 13 项标量，文档也从 12 改到 13 了。**但没有任何一处说明：
用 v0.5 及之前跑出来的 `vmr-story-corpus.json` 与现在跑出来的不可直接比较**（相关性矩阵的规模从
12×12 变成 13×13，"前 15 条"的截断基准也随之变化）。

`vmr story -corpus` 的产物是一个会被存档、会被跨时间比较的分析结果。CLAUDE.md 把
`vmr-report.json` 的 schema 当成公开契约（`meta.format`），**`vmr-story-corpus.json` 没有对应的
版本字段**，于是这次口径变更是无声的。

**建议**：低优先级，但值得记。若将来还要往标量集里加东西，先给 corpus 产物加一个 `format` 版本号。

---

## PART D：对 Gemini 3.6 Flash 报告的逐项评估

> 逐项批注**已就地写入** `docs/future-strategy/vmr_comprehensive_code_and_design_review_report_gemini-3.6-flash.md`
> （沿用 DP2 对上一份外部评审的同一处理方式）。下面是结论汇总与采纳去向。

**批注图例**（与写入该文档的一致）：

| emoji | 含义 |
|---|---|
| ✅ | 问题成立，且建议**早已实现**——评审漏看了现有代码 |
| 🟢 | 问题成立，根因正确，建议可采纳（可能微调形式） |
| 🟡 | 问题部分成立，但**根因或建议需要改写** |
| 🟠 | 现象存在，但**影响被显著高估**，或其建议有副作用 |
| ❌ | **问题不成立**（事实错误、前提自相矛盾，或超出本项目定位） |
| 🔁 | **重复一条已被明确否决的建议**，且未回应否决理由 |

### D.1 逐条结论

| 原条目 | 判定 | 一句话理由 | 去向 |
|---|---|---|---|
| **0.1 总体评估**（全绿、善后质量高） | 🟢 | 实测复现：`gofmt`/`vet`/`test`/`archtest` 全绿，E.1×8 + E.2×13 逐项回源码确认 | — |
| **A.1 21 项落地表** | 🟡 | 结论正确，但**行号大面积不准**（#14 指 `modelusage.go:35` 实为注释行，真实改动在 85-100；#20 指 `aggregate.go:638` 实为 `ProviderQuotas` 赋值行）；#4 把"估算标注"说成 `⭐`——`⭐` 是超额标记，估算是内联 `(32.0% 估算)`；表列 19 行却称"21 项 1:1 比对"（漏 #15/#17，那两项本就按计划未做） | 不采纳，仅记录 |
| **A.2 行数预算表** | ❌ | **新引入一处数字错误**：`aggregate.go` 报 965，实测 **977**（差 12）；`metrics.go` 报 411，实测 414。它正确更正了上一轮自己的 `corpus.go` 368→358，却在同一张表里犯了同类错误 | 不采纳；真实数字见本文 A.2 |
| **B.1 仓库卫生待裁决** | 🟢 | 与前一轮 A.2-10/PART F 一致，两项确实仍待用户裁决 | 转入 E.1 待决 |
| **B.2 `windowFrom == windowTo` 边界** | 🟢 | 分析正确，结论也正确（**不是缺陷**）。区间相交判定退化成点与闭区间相交，数学上成立 | 无需动作 |
| **C.1 Multi-Limit 数据模型** | 🔁🟠 | 与它自己上一轮的 I-3 同源，上一轮的否决理由（`Limits[0]` 处已有前向兼容注释 + `TestQuota_Reject_MultipleLimits` 锁死 + P3 落地时该函数整体作废）**一个字都没回应**；提前泛化数据模型违反 KISS/YAGNI。**但它间接暴露了一个真东西**：`buildProviderQuotaRows` 的排序 tie-break 只用 `Provider`，多 Limit 下将不再唯一，`TestBuildIsDeterministic` 会开始 flaky | 主张不采纳；tie-break 注记进 E.2 |
| **C.2 `vmr-quota.json` GC** | 🟢 | **本报告里最有价值的一条，且它自己低估了严重性**——真实后果不是磁盘浪费，是 `-‡` 会**永久**显示且文案说谎（本文 N-3）。但它的药方（报表侧 GC / 加载期清理）把纯读路径变成写路径，归属错了（本文 P-3） | 拆两半：文案修正进 E.1；GC 归属路由半区，登记进 P3 |
| **C.3 失败 attempt 的 token 损耗** | 🟡 | 现象描述成立，**根因分析不对**：不是"归属逻辑非对称"，是 `audit.Attempt` 结构体里**根本没有 Usage 字段**（实测确认），失败 attempt 的 token 消耗在审计层就不存在，报表只是如实反映。它建议的 `WastedTokens` 无数据可算。**但"与 `WastedMS` 形成完整浪费度量对"这个方向是对的——而 `WastedMS` 本来就已经算好且未渲染**（本文 P-4） | token 部分不采纳；`WastedMS` 渲染进 E.2 |
| **C.4 定量对账协议 `DiscrepancyPct`** | ❌ | **前提自相矛盾**：整张表的设计前提是两列窗口不同、不可相减，它却要拿两者算相对偏差率，并自己加了"仅在窗口完全对齐的实验场景下触发"的限定——而窗口对齐几乎从不发生，做出来是一个永远不亮的灯。**但它指向的空白是真的**（没有任何机制验证重算公式），正确形态是**差分测试**而不是报表列（本文 N-4） | 报表列不采纳；差分测试进 E.1 |
| **C.5 TPM/RPM 并发碰撞因子** | 🟡 | 问题成立（与上一轮 II-2 同源，那一轮的"便宜的一半"已落地为"主要错误类"列）。**但"极低成本"的估计是错的**：报表最细分桶到小时，重建"429 前后 1 秒内活跃请求数"需要按 `ts + dur_ms` 重建重叠区间，是一次新的流式累计，直接压在余量 23 行的 `aggregate.go` 上 | 并入第三梯队既有的 Peak TPM/RPM 条目，作为子场景 |
| **C.6 日均速率（Daily Run-Rate）** | 🟡 | 它正确区分了"物理平均值"与"外推预测"，这一点比 DP2 §0.5 否决的那个版本站得住。**但它没看见真正的缺口**：子表连本报表窗口是哪一段都没显示（本文 N-5）——先把事实摆出来，比先给一个派生量更对 | 改写后采纳：脚注补窗口区间进 E.2；日均速率本身并入第三梯队 |
| **D.1/D.2 Roadmap** | 🟡 | 三条建议（Multi-Limit 解耦、Daily Rate、quota GC）分别对应 C.1/C.6/C.2，去向同上。Mermaid 图里的 "C.1 支持 requests/tokens 口径的准确性" 一条没有对应正文，无法评估 | 见上 |

### D.2 对这份评审的总体评价

它的**符合度核查是可靠的**——21 项善后落地的判定全部经得起复核，这一点它说对了，且这本身有价值
（独立第三方确认了前一轮 PART F 的自述不是虚报）。

它的**弱点与上一轮完全相同，且没有改善**：

1. **数字不核**。上一轮把 `corpus.go` 报多 10 行被指出；这一轮它专门更正了那个数字（甚至在表格
   里写了"之前 Gemini 评审报告误记为 368 行，现已回实测更正"），**却在同一张表里把
   `aggregate.go` 报少了 12 行**——而 `aggregate.go` 恰恰是本轮唯一真正增长过的受控文件。
2. **不查"这个数据是不是已经算好了"**。上一轮 429 计数、`WindowFootnote` 措辞三次都是这个模式；
   这一轮 C.3 建议加 `WastedTokens` 而没看到 `WastedMS` 已经算好且未渲染，是第四次。
3. **停在文档层，不进数据流**。它的六条设计缺口全部是"读设计文档想出来的架构问题"，没有一条来自
   "跟着一个数字从审计记录走到渲染层"。本文 N-1/N-2 这一类——**当前代码此刻就在算错的东西**
   ——它一条都没找到。

**净贡献**：C.2 是真的（且比它说的更严重）；C.6 指对了地方开错了药；其余四条要么重复、要么前提
不成立、要么成本估反。

---

## PART E：善后工作计划

**前提**：一梯队三批 + 二梯队六批 + 善后 E.1/E.2 已全部落地并通过全量验证。以下是**这两个梯队处理
完之后**的遗留工作，按"是否值得马上做"分三类。

### E.1 立即处理（建议在当前 commit 之前或紧随其后）

共同特征：**要么是产物在说谎，要么是刚宣称的正确性并不成立，且改动都很小。**

| # | 条目 | 来源 | 改动量 | 为什么现在做 |
|---|---|---|---|---|
| 1 | `requests` 口径的重算基数由 `e.Requests` 改为"进入过 `forwardSuccess` 的 attempt 数"（`EndpointRow` 加一个 `Response!=nil && Status<400` 的计数器；最小版可先用 `e.OK`） | N-1 | **~5 行** + 1 测试 | 善后批次刚把这一列宣布为"精确复现"，而它在另一个轴上系统性高估，偏差量级大于刚修掉的那个 |
| 2 | 中英文脚注：`requests` 口径下"失败尝试不计费"的措辞按 #1 选的方案重写；补一条 `tokens` 口径"usage 未嗅探时路由按字节估算、本列计 0" | N-1 / N-2 | 文案 4 处 | 脚注是这张表唯一的诚实性保障，它现在漏了一条、另一条的方向随 #1 改变 |
| 3 | `-‡` 文案去掉"在本周期内 / during the current period" | N-3 | **2 行** | 代码没有任何时间限定；一次半年前的配置修改会让这句话永久说谎 |
| 4 | 差分测试：驱动 `router.ChargeResponse` 与 `buildProviderQuotaRows` 跑同一组合成记录并断言相等 | N-4 / P-1 | ~80 行测试 | 这条测试会当场抓到 #1；此后每次口径改动都受保护。**这是本轮唯一一条"不做就还会再犯"的** |
| 5 | `⭐` 脚注加 `anyOverQuota` 门控，与 `‡`/`†` 对齐 | N-6 | 3 行 | 三个同批标记里唯一不守自己约定的 |
| 6 | `tokens` 口径"有流量但 `TokensKnown` 全 0"渲染 `-` 而非 `0` | N-2 | ~10 行 + 1 测试 | A.2-6 的纪律只贯彻了 cost 一半 |
| 7 | 用户裁决：`config.mba copy.yaml` 去留、两份被删文档去留 | 前轮 PART F / Gemini B.1 | — | 提交前的最后一道卫生关口，非代码，需要用户拍板 |

**小计：约 20 行生产代码 + 1 个差分测试 + 6 处文案。** 除 #4 外全部零风险。

> **#7 的现状**：`.gitignore` 已覆盖 `/config.mba*.yaml`、`/vmr-quota.json`，两个文件不会被误提交；
> `config.mba copy.yaml` 经 diff 确认不是字节重复而是 2026-07-30 的旧版本快照（无 quota/pricing 段），
> 删除不可逆。两份被删文档（`docs/dependabot_branch_cleanup_2026-08-11_sonnet-5.md`、
> `docs/future-strategy/VMR_综合评审与发展建议_report_v2.md`）在本轮改动开始前就已是 `D` 状态，
> 找不到删除理由。**本文同样不代为决定。**

### E.2 值得做，但应单独排期（不阻塞当前提交）

| # | 条目 | 来源 | 规模 | 排期建议 |
|---|---|---|---|---|
| 8 | 子表 `¹` 脚注补出本报表窗口的区间与时长 | N-5 / Gemini C.6 | 小 | 下一个小版本。数据现成（`Meta.From/To`），不加列 |
| 9 | `§2.5` 主表补"失败耗时"（`WastedMS`），或并进"主要错误类"列 | P-4 / Gemini C.3 | 小 | 与 #8 同批。补上方案 §4.2 承诺四项里的最后一项 |
| 10 | `allPathsOutsideDir` 加 `EvalSymlinks`（含失败回退） | N-8 | 小 | 与 #8 同批。macOS 上会假警报 |
| ~~11~~ | ~~`FormatLiveUsed` 改收字符串，消掉 `liveUsedNumStr`~~ | N-7 | — | ✅ **已在清理轮完成**（G.3） |
| ~~12~~ | ~~`clientEndpointScale` 提取，`aggregate.go` 退回 3 行~~ | N-9 | — | ✅ **已在清理轮完成**（G.3），`aggregate.go` 975 行 |
| ~~13~~ | ~~设计文档 §2.6 段落顺序~~ | N-10 | — | ✅ **已在 E.1 落地时顺带完成** |
| 14 | `internal/quota/weight.go` 注释写明每种 metric 的**输入契约** | P-2 | 文档 3 处 | 与 #4 同批（差分测试是执行，注释是说明） |
| 15 | `buildProviderQuotaRows` 排序 tie-break 加一句"多 Limit 下 Provider 不再唯一"的前向注记 | Gemini C.1（改写后） | 1 行注释 | 随手带上。给 P3 的作者留个路标 |
| 16 | `-‡` 加时间限定：旧 key 的 `PeriodStartTime()` 超出上一个周期则按"无数据"处理 | N-3（第二层） | ~5 行 + 1 测试 | 独立一次。#3 的文案修正是必做，这条是让标记本身更准 |
| 17 | `ModelSwitch` 新增 Step 内切换检测（`WithinStep`） | 前轮 A.2-7 完整的一半 | 中 | **仍未做，维持前轮判断**：会改变 `MetricModelSwitchCount` 的口径，影响已有 corpus 统计可比性，需独立立项 |
| 18 | `ModelSwitch` 切换行追加客观工具上下文 | 前轮 Gemini II-3 | 小 | **仍未做**，排在 #17 之后 |
| 19 | `vmr-story-corpus.json` 加 `format` 版本字段 | P-6 | 小 | 下次往标量集里加东西之前 |

### E.3 建议并入方案第三梯队（长期挂起，附触发条件）

> 以下四条**已追加进** `docs/future-strategy/vmr_report_provider_client_cost_analysis_sonnet-5.md`
> 的第三梯队表（本轮唯一的第二处既有文件改动）。

| 条目 | 不做的理由 | 触发条件 |
|---|---|---|
| **`vmr-quota.json` 的孤儿桶 GC** | 归属错了：清理是**写**操作，报表是纯读消费者，让它去写这个文件会引入"报表进程与路由进程并发写"这个本来不存在的问题。正确的执行点是路由半区的 `Registry.Load` 之后、首次 `Flush` 之前，用当前配置的合法 `limitKey` 集合过滤一次——那是这个文件的唯一写者。报表侧现在需要的只是把 `-‡` 的文案改对（E.1 #3） | 与 P3 多窗口额度同批：P3 会重写 `limitKey` 的构成规则，孤儿桶问题届时会显著放大，两件事一起做成本最低 |
| **`ProviderQuotaRow` 的 Multi-Limit 复合键解耦** | 今天 `config.validateQuota` 显式拒绝 `len(Limits) > 1`（`TestQuota_Reject_MultipleLimits` 锁死），`Limits[0]` 是唯一正确写法而非缺陷；`buildProviderQuotas` 处已有"P3 需整体重写此函数"的前向注释。在没有第二个 Limit 存在的情况下先泛化数据模型，是拿确定的复杂度换不确定的收益（KISS/YAGNI）。**唯一现在就该做的是留一句 tie-break 注记**（E.2 #15） | P3 放开多窗口额度时，与 `buildProviderQuotas` + §2.5 渲染 + 排序 tie-break 同批改造 |
| **`§2.5` 的日均消耗速率（Daily Run-Rate）** | 与已否决的"按日志窗口外推月消耗"不同，日均速率是可辩护的物理平均值，不是预测。但它是把一个事实除以一个**读者当前根本看不见的数**——子表连本报表窗口是哪一段都没显示。先把窗口区间摆出来（E.2 #8），读者就已经有了心算所需的两个数；再加一列的边际价值随之大幅下降。且日志窗口很短时（几小时），"日均"里隐含的 ×N 外推与被否决的那条并无本质区别 | E.2 #8 落地后，仍然反复出现"读者拿窗口消耗与上限心算"的真实场景；**且**约定一个最短窗口门槛（如 ≥12h）才渲染 |
| **429 并发碰撞因子 / Peak TPM·RPM 完整统计** | 与既有的 Peak TPM/RPM 条目同源（该条目的"便宜的一半"——`§2.5` 主要错误类列——已落地）。Gemini 提议的"429 前后 1 秒内活跃请求数均值"被描述为极低成本，实测不是：报表最细分桶到小时，重建瞬时并发需要按 `ts + dur_ms` 重建重叠区间，是一次新的流式累计，且直接压在余量仅 23 行的 `aggregate.go` 上（正是 DP1 风险表点名的那个文件） | 与既有 Peak TPM/RPM 条目合并触发：出现"错误率高但额度充裕"的真实运维案例，**且**"主要错误类"列已不足以定位；届时先评估把并发重建放进独立 collector 文件而非 `aggregate.go` |

---

## PART F：本报告自身的复查

**已实测确认，不是推断**：

- 全部行数（`grep -c ''`）、`gofmt`/`go vet`/`go test ./...`/`archtest`/`-race`
- `chargeQuota` 的调用点在 `forwardSuccess` 内（`router.go:525`），即只在 2xx 响应上记账
- `EndpointRow.OK` 的判据是 `a.Error == "" && a.Response != nil && a.Response.Status < 400`，
  比 `forwardSuccess` 的触发条件**多了一项** `a.Error == ""`；而 `att.SetTruncated`
  （`audit.go:264`）会给一个已经记过账的 2xx attempt 写上 `Error`，所以 `e.OK` 是真值的下界
  而非等值——见 N-1 的三候选对比表
- `addEndpointReq` 的 `e.Requests++` 无条件、token 四分量仅在 `rc.usageOK` 下累加
- `tokenCharge` 在嗅探失败时返回字节估算而非零
- `quota.Registry` 整份读入/整份写回、从不删 key（`store.go` 的 `r.accounts = ff.Accounts` /
  `fileFormat{Accounts: r.accounts}`）
- `audit.Attempt` **没有** Usage 字段（否定 Gemini C.3 的可实施性）
- `config.validateQuota` 要求 `amount > 0`（所以 `Amount <= 0` 导致 Finding 永不命中的假想不成立）
- `OverQuotaFootnote` 无条件输出、`ConfigChangedFootnote`/`NoOverlapFootnote` 有门控
- `numStr` 与 `liveUsedNumStr` 是两份实现且判整数写法不同

**属于判断而非事实，可能有人不同意**：

- **N-1 的严重度**。如果一个部署里 `metric: requests` 账户的请求成功率接近 100%，三个候选基数
  几乎相等，这条的实际影响就很小。它的价值取决于该账户的真实错误率——而 `§2.5` 主表刚补上的
  "错误率"和"主要错误类"两列，恰好就是判断这一点的地方。**这条发现的价值与其说在偏差本身，
  不如说在"刚宣布精确的东西并不精确"这个状态**：一个带脚注的估算和一个自称精确的错值，
  后者更危险。
- **E.1 #4（差分测试）的成本**。它需要一个能同时 import `router` 与 `report` 的测试位置，
  `internal/archtest` 的导入边界规则不禁止**测试**这样做，但这是本仓库第一次这么用，可能引出
  关于"测试是否该跨越架构边界"的讨论。**如果结论是不该，退而求其次的形态是：把 router 侧的期望值
  用一个不依赖 router 包的、逐字复制的最小实现算出来——但那就又变成了两份实现，价值大打折扣。**
  我认为跨边界的测试包是对的，但这是一个需要拍板的选择。
- **P-3 把 GC 归给路由半区**。也可以主张"孤儿桶是配置变更的产物，该由一个显式的 `vmr quota gc`
  子命令处理，让用户自己决定何时清"。我倾向自动化（用户不该被要求知道这个文件存在），但显式命令
  的可审计性更好。

**本报告没有覆盖的**：

- 未在真实数据上重跑 `vmr report`/`vmr story`。前轮 PART F 记录过一次真实数据验证（`⭐`/`†`/`‡`
  三种标记在 `examples/sample-audit.jsonl` 上按预期渲染），本轮聚焦源码与数据流，未重复冒烟。
  **E.1 #1（`e.Requests` → `e.OK`）落地时应当在真实日志上跑一次前后对比**——那是能直接看到偏差
  幅度的地方。
- 未审查本轮未改动的既有文件（`session.go`、`detail.go`、`internal/story/journey.go` 等）。
- 未评估 `vmr story` 的 golden 文件 diff 是否逐行合理（前轮记录已人工核对过，本轮抽查了
  `testdata/golden.md` 的新增段落形态正常，未逐行复核）。

---

## PART G：E.1 执行结果（2026-08-13 同日落实）

**执行范围**：E.1 全部 7 项。每一项落地前都独立回源码复核，过程中发现**一处原分析需要修正**
（见条目 1），已按修正后的方案实施。

**验证**：`gofmt -l ./internal ./cmd` 无输出、`go vet ./...` 无输出、`go test ./...` 全绿、
`go test ./internal/archtest/...`（行数预算 + 导入边界）全绿、
`go test -race ./internal/router/... ./internal/quota/... ./internal/replay/... ./internal/server/...` 全绿。
另做了一次真实数据前后对比（见条目 1 末尾）。

| # | 条目 | 结果 |
|---|---|---|
| 1 | `requests` 口径基数 | ✅ **按完整版落地，且原分析的"最小版"被真实数据证否**——见下方专节 |
| 2 | 脚注重写 | ✅ `WindowFootnote` 中英文都重写成按 metric 分列的形式（requests 无出入 / tokens 两处 / cost 一处 / 三者共同一处），替代原先那段把四条出入源混在一起、又给 requests 单独加免责的写法 |
| 3 | `-‡` 文案 | ✅ 中英文都去掉了"在本周期内 / during the current period"，并补一句"旧计数器不会被自动清理，所以这个标记只说明「改过」，不说明改于何时" |
| 4 | 差分测试 | ✅ `cmd/vmr/quota_parity_test.go`（新）——**并已验证它真的抓得住**：把 `e.Forwarded` 改回 `e.Requests` 后测试立即失败（`window consumed = 5, router actually charged 4`），改回即通过 |
| 5 | `⭐` 脚注门控 | ✅ 加 `anyOverQuota`，与 `‡`/`†` 对齐；新增 `TestRenderProviderQuotaTable_OverQuotaFootnoteAbsentWhenNoneFlagged` |
| 6 | tokens 口径 `-` | ✅ `tokensSawTraffic`/`tokensAnyKnown` 与 cost 分支完全同构；新增 3 条测试（有流量无 usage → `-`、零流量 → 真 0、部分有 usage → 照常求和） |
| 7 | 两项裁决 | ✅ 已裁决：`config.mba copy.yaml` **保留不动**（`.gitignore` 已覆盖，零成本）；两份被删文档**确认删除**，理由写进 commit message（见 G.4） |

**顺带做掉的 E.2 项**（同一处代码/文档，分开改反而更贵）：条目 11（`FormatLiveUsed` 改收字符串，
消掉 `liveUsedNumStr`）、条目 12（`clientEndpointScale` 提取，`aggregate.go` 退回 3 行）、
条目 13（设计文档 §2.6 把 `provider_quota_exhaustion` 那段移到"§7 是什么"之后）——详见 G.3。

### G.1 条目 1：原分析给的"最小版"是错的，真实数据证明了这一点

**原分析**（N-1）给了两个选项：完整版用"进入过 `forwardSuccess` 的 attempt 数"，最小版先用
`e.OK`，并把两者的差描述成"只剩『2xx 但流截断』这个小得多的反向残差"。

**落地时按完整版实施**：`EndpointRow` 新增 `Forwarded`（`a.Response != nil && a.Response.Status < 400`，
即 `OK` 的条件去掉 `a.Error == ""`），`addAttempt` 里累加，`providerquota.go` 的 requests 分支改用它。

**随后的真实数据检查证明最小版会引入新的偏差，而且不小**（本仓库 `logs/vmr-audit-*.jsonl.zst`
全量，按 provider 汇总 `EndpointsAll`）：

| provider | requests | ok | **forwarded** | 说明 |
|---|---|---|---|---|
| minimax | 5175 | **5132** | **5175** | **43 次转发成功但流截断**——`e.OK` 会漏掉这 43 次记账 |
| volcengine | 889 | 889 | 889 | 该窗口内无失败、无截断，三者相等 |
| opencode | 2442 | 2442 | 2442 | 同上 |
| dashscope / deepseek / openrouter / sub2api | — | — | 均相等 | 同上 |

也就是说：**如果按最小版用 `e.OK`，minimax 这类账户会系统性少算 0.83%**（43/5175）。
"小得多的反向残差"这个判断在真实数据上不成立——它与被修掉的聚合取整偏差是同一量级。
完整版没有这个问题。

**同一份真实数据上的前后对比**（唯一配了 `metric: requests` 额度的账户是 `volcengine`）：

```
OLD (e.Requests)   volcengine requests window=2705
NEW (e.Forwarded)  volcengine requests window=2705
```

**数字没有变——这是一个诚实的结果，不是修复无效**：该账户在这段日志窗口里
`requests == ok == forwarded == 889`（零失败、零截断），三种基数恰好重合。这条修复的价值在
**账户开始出错的那一天**才会显现，而那正是运维最需要这张表的时候。差分测试（条目 4）是它此后
不会再退化的保证，不依赖某一份数据是否恰好覆盖到这个分支。

`metric: tokens` 的 `volcengine2` 在该窗口渲染 `0` 而非 `-`——核对确认它在 `EndpointsAll` 里
完全没有出现（零流量），所以 `0` 是正确的"真零"，条目 6 的抑制逻辑没有误触发。

### G.2 条目 4：差分测试的位置与它证明了什么

放在 `cmd/vmr/quota_parity_test.go`，理由是 `internal/archtest` 禁止 `report → router`
（两个半区必须保持独立），而 `cmd/vmr` 是**已经合法地同时依赖两者**的组合根——也是唯一能诚实断言
"两者一致"的地方。测试用同一批合成记录：一路喂给 `router.ChargeResponse`（真实记账入口，
`internal/replay` 复用的也是它），另一路写成审计 JSONL 跑完整的 `cmdReport` 管线，读
`vmr-report.json` 的 `provider_quotas[].window_consumed`，断言两者**完全相等，零容差**。

合成记录刻意覆盖了三种此前无人验证的分支：failover（首次 429 + 二次 200，只记一次）、
全部 attempt 失败（一次都不记，但请求级计数仍算在最后那个端点上）、2xx 但流截断（**记了账，
却掉出 `OK`**）。

**这条测试是本轮唯一"不做就还会再犯"的一项**，也是唯一能让 P-1（"从来没有一份计划问过重算列
怎么证明它算对了"）这个缺口真正闭合的东西。

### G.3 终审后的清理轮（同日）

E.1 落地后又做了一轮针对性清理，**没有新增功能，全部零行为变更**：

| 类别 | 处理 |
|---|---|
| **重复实现**（N-7） | `i18n.liveUsedNumStr` 删除——它是 `report.numStr` 的第二份拷贝，连判整数的写法都不同（`float64(int64(v))` vs `math.Trunc(v)`）。`FormatLiveUsed` 的签名从 `(used float64, …)` 改为 `(usedStr string, …)`：数字格式化留在 `report`，i18n 只负责"要不要加估算标注"，零依赖叶子包的约束不变而重复消失 |
| **放错位置的代码**（N-9） | `aggregate.go` 里那段客户端去重计数循环移进 `clientendpoint.go` 的 `clientEndpointScale`——它本来就是那个 collector 的职责，却占着全包第二紧的行数预算。`aggregate.go` 984 → **975** 行 |
| **无消费者的导出符号** | `quota.ModelMultiplier` 改为不导出（`modelMultiplier`）——它在生产代码里只被同文件的 `ApplyModelMultiplier` 调用，导出是没有消费者的 API 表面。CLAUDE.md 的 `quota` 行同步 |
| **违反 CLAUDE.md 明文约定的注释**（新发现） | 见下方专段 |
| 过长注释 | `providerquota.go` 的 requests 分支（21 行→12 行）、`rows.go` 的 `Forwarded` 字段（18 行→13 行）——保留两条"为什么"，删掉与测试重复的数值推演 |

**新发现的约定违反**：CLAUDE.md 的"**No section numbers in cross-references**"条明确写着，代码注释里不得
出现 `§6.5`/`F9` 这类裸编号，要用名字或短描述；并注明"Existing code was swept once for this (2026-08);
keep new comments to the same rule"。而第一/第二梯队与善后批次往注释里写入了约 **75 处**评审发现编号
（`A.2-6`、`B-3`、`N-1`、`Gemini I-2`、`ISSUE-03`、`DESIGN-03`），生产代码 44 处、测试 31 处。

这类编号对读代码的人是纯噪声——`// B-3: …` 要求读者先知道哪份评审报告里有 B-3。已全部改写为自解释的
措辞（多数情况下句子本身已经解释清楚，直接删掉编号即可）。**未动 `CCR N-4`**：那是设计文档的需求
编号、早于本轮存在，且通过了 2026-08 的那次清扫，属于既有约定内的命名。

> 这次清理暴露了一个值得记的操作教训：批量正则改注释**弄坏了 6 处句子**（`is /the lock-in`、
> `is 's` 之类）。全部靠逐行复查 diff 找回并修好，测试是发现不了这类损伤的——它们语法合法。
> 下次同类改写应逐文件人工确认，而不是信任一个"看起来够聪明"的正则。

**清理后验证**：`gofmt`/`go vet`/`go test ./...`/`archtest` 全绿；`-race` 覆盖
`router`/`quota`/`replay`/`server` 全绿；新代码函数级覆盖率 83%–100%
（`buildProviderQuotaRows` 96.2%、`buildProviders` 98.2%、`renderProviders` 95.7%）。
另补两条此前缺失的测试：`TestAddAttempt_ForwardedCountsTruncated`（在聚合层直接钉住
"截断的 2xx 计入 Forwarded 但不计入 OK"）与 `TestClientEndpointScale`。

> 前者写的时候第一版**测试夹具自己错了**（给 status=0 的传输失败也塞了 `response` 字段，真实审计
> 记录不会有），被断言当场抓出来。真实记录里 `response` 缺失才是"从未转发"的标志，夹具已改对——
> 这条本身就是差分测试价值的一个小注脚：靠手写期望值时，错的往往是期望值。

### G.4 两份被删文档：确认删除，理由

按裁决保持删除状态。提交时 commit message 需包含：

```
docs/dependabot_branch_cleanup_2026-08-11_sonnet-5.md 与
docs/future-strategy/VMR_综合评审与发展建议_report_v2.md 已被内容更新的后续评审报告取代，
故删除；CLAUDE.md 把评审报告类文档列为"预期会累积"的一类，这里是有意的例外，不是无声清理。
```

`config.mba copy.yaml` 保持原样不动，已由 `.gitignore` 的 `/config.mba*.yaml` 覆盖，不会被误提交。

---

## 附：一句话总结

**上一轮的结论是"代码是好的，文档欠了债"；债还完之后，这一轮的结论是"公式是对的，口径是错的"。**

DP2 批 1 把 `BaseAmount`/`ApplyModelMultiplier` 下沉成"一份实现，两个消费者"，这一步做得干净利落，
零行为变更。但计划把这当成了终点——没有人再往下问一句"公式的输入从哪来"。于是
`quota.Counters{Requests: int64(e.Requests)}` 这一行，真正做口径决策的那一行，看起来像胶水代码，
一路穿过两份 dev plan、一轮交付复核、一轮外部评审，最后还被善后批次贴上了"精确复现"的标签。

值得记的不是这个 bug 本身（改 1 行），而是它为什么活了这么久：**四道复核全部在问"代码有没有照着
计划做"，没有一道在问"计划要求的这个数，怎么证明它算对了"。** 唯一能改变这一点的，是 E.1 #4 那条
差分测试——它是本轮七条立即处理项里唯一一条不做就还会再犯的。

**（2026-08-13 同日更新）** E.1 全部 7 项已按 PART G 落实，全量测试 + 真实数据双重验证通过。
落地过程本身又给这个教训添了一笔：本文 N-1 给出的"最小版"（先用 `e.OK`）**被真实数据证否**——
minimax 账户有 43 次"转发成功但流截断"的 attempt，路由半区记了账而 `e.OK` 不含它们。写这份报告
时我把那个残差判断成"小得多"，是又一次没去查数据。差分测试落地后，这类判断不再需要靠人拍脑袋。
