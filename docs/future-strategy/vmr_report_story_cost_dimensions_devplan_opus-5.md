<!-- Ver 2026-08-12 23:40, by Opus 5 -->

# Dev Plan：成本/用量分析三维度增强（第一梯队）

**对应设计**：`docs/future-strategy/vmr_report_provider_client_cost_analysis_sonnet-5.md`（下称"方案"）。
本文只落地方案的**第一梯队三批**；第二/第三梯队不在范围内，不要顺手做。

**三批互不依赖**（不同文件、不同测试、批 3 甚至是另一个产物），可并行或按任意顺序执行。建议顺序
批 1 → 批 2 → 批 3，理由是边际信息量递减。

> **实施状态（2026-08-12，Sonnet 5）：三批全部完成，零延后、零范围削减。**
> `go build ./... && go vet ./... && go test ./...` 全绿（`internal/server` 的
> `TestActiveProbe_UpstreamFailureGoesToReportFailure` 单独重跑必过，是既有时序类 flaky
> 测试，与本次改动的三个包（`internal/report`/`internal/story`/`internal/i18n`）无关，未
> 触碰该测试所在文件）；`internal/archtest` 的行数预算全部达标（见下方各批"完成情况"）；
> 用仓库里的真实历史审计日志（`./logs/vmr-audit-2026-08-0{1,2,3}.jsonl.zst`，`config.mba.yaml`
> 的 volcengine/volcengine2 双账户挂同一模型场景）跑通 `vmr report`/`vmr story` 中英文各一次，
> §2.5/§5.5/模型使用表都渲染出了预期形状的真实数据（含一次真实检出的模型切换，见批 3 完成情况）。
> 三批的 dev plan 文档（§1.4/§2.4/§3.4）与设计文档 (`docs/VirtualModelRouter_Design_v4_Analytics.md`)、
> `docs/UserGuide.md`/`.zh.md`、`CHANGELOG.md` 均已按各批要求更新。逐批完成情况见每节末尾新增的
> "完成情况" block；本文档其余正文保持原样不动（设计意图仍然成立，不重写）。

---

## 0. 三批共用的约定与硬约束

**行数预算（`internal/archtest` 会让 `go test ./...` 直接失败，不是风格建议）**：

| 文件 | 当前 | 预算 | 余量 | 对本次的含义 |
|---|---|---|---|---|
| `internal/report/aggregate.go` | 960 | 1000 | 40 | 批 1 只许 +1 行，批 2 只许 +3 行 |
| `internal/story/render_spine.go` | 379 | 380 | 1 | 批 3 **绝不能**动这个文件 |
| `internal/story/metrics.go` | 401 | 470 | 69 | 批 3 只加结构体字段 + 2 行调用 |
| `internal/story/render_md.go` | 301 | 350 | 49 | 批 3 只加 1 行调用 |

**其它共用约定**（都是既有惯例，照做即可）：

- 新章节 = 新文件（`section_*.go`），文案 = `internal/i18n/` 下一一对应的新文件，不塞进现有 bundle。
- 文案 struct 用字段（拼错即编译失败），完整句子用 `func(...) string` 字段，中英文各写各的语序。
- `vmr-report.json` / `journey-*.json` 的叙述性字段**固定英文**，只有 Markdown 跟随 `-lang`。
- 新增排序必须有**唯一性 tie-break**（否则 Go map 遍历顺序会让两次运行产出不同文件，
  `TestBuildIsDeterministic` 会抓到）。
- 新文件首行加版本头注释：`// Ver YYYY-MM-DD HH:MM, by Opus 5`。

**每批完成后的验证命令**：

```
gofmt -l ./internal ./cmd
go build ./... && go vet ./...
go test ./internal/archtest/...          # 行数预算 + 导入边界
go test ./internal/report/... ./internal/story/... ./internal/i18n/...
go test ./...
```

---

## 1. 批 1：Provider 维度汇总 + 额度参照（`vmr report`）

### 1.1 设计要点

**核心实现策略：不新增任何流式累计，全部从已完成的 `rep.EndpointsAll` 事后上卷。** 这是可行的，
因为除分位数外这张表需要的每一项都是可加的；分位数按方案 §4.2 已明确放弃。这让 `aggregate.go`
只多 1 行，且与 `buildTools`/`buildCompactions` 两个既有的"跑完再派生"函数完全同构。

**时序前提（必须确认）**：`buildProviders` 必须在 `finishEndpoint` 循环之后调用——`TokensInFresh`
和 `CacheEfficiency` 是在 `finishEndpoint` 里算出来的。`aggregate.go` 现有顺序天然满足：
`for _, e := range epsAll { finishEndpoint(e); rep.EndpointsAll = append(...) }`（约 618-621 行）
在 `rep.Tools = buildTools(sess)`（约 634 行）之前。**把新调用放在 `rep.Tools` 那一行旁边即可。**

### 1.2 改动清单

**① `internal/report/rows.go`（+~45 行，无预算限制）**

```go
// Report2 新增字段（放在 Sticky 之后、Pricing 之前）
Providers []ProviderRow `json:"providers,omitempty"`

// ProviderRow 是一个上游账号（config.yaml 的 providers[].name）的汇总 —— 由
// EndpointsAll 事后上卷而来，不是独立累计的桶。刻意没有 P50/P95：百分位不可加，
// 而给它们一份真分位数就要在聚合过程中再缓冲一整份逐请求切片；账户是否吃紧这个
// 问题不需要分位数，每个端点自己的分位数在 §5 已经有了。DurMSMean 是诚实的替代。
type ProviderRow struct {
    Provider string   `json:"provider"`
    Models   []string `json:"models"`            // 该账号名下实际有流量的上游模型，字典序
    Requests int      `json:"requests"`
    RequestsOK int    `json:"requests_ok"`
    Attempts int      `json:"attempts"`
    Failed   int      `json:"failed"`
    ErrorRate float64 `json:"error_rate,omitempty"`   // failed/attempts × 100
    ErrorClasses map[string]int `json:"error_classes,omitempty"`
    WastedMS int64    `json:"wasted_ms,omitempty"`

    TokensIn, TokensInCached, TokensInCacheWrite, TokensInFresh, TokensOut int64 `json:"..."`
    TokensKnown     int     `json:"tokens_known,omitempty"`
    CacheEfficiency float64 `json:"cache_efficiency,omitempty"`
    DurMSMean       int64   `json:"dur_ms_mean,omitempty"` // 均值，不是分位数

    CostEstimate *float64          `json:"cost_estimate,omitempty"`
    Quota        *ProviderQuotaRef `json:"quota,omitempty"`
}

// ProviderQuotaRef 是 config.yaml 里该账号的额度声明的只读快照，纯参照值。
// 不做模型倍率加权，也不按账号自己的计费周期重新分桶 —— 报表窗口和计费周期本来
// 就不对齐。渲染层必须原样带上这个限定说明。
type ProviderQuotaRef struct {
    Metric string  `json:"metric"`  // requests | tokens | cost
    Every  string  `json:"every"`   // 1mo / 1w / 5h ...
    Amount float64 `json:"amount"`
}
```

**② `internal/report/provider.go`（新文件，~90 行）**

- `buildProviders(rep *Report2, quotas map[string]ProviderQuotaRef) []ProviderRow`
- 遍历 `rep.EndpointsAll`，用下面的 helper 取 provider/model，按 provider 累加；
  `CacheEfficiency = cached/(cached+fresh)`（分子分母都已是汇总值）；
  `DurMSMean = DurMSSum / RequestsWithDur`；`CostEstimate` 用 `addCost` 累加（已有工具函数）。
- 排序：`TokensIn` 降序，**tie-break 用 `Provider` 名字典序**。`Models` 切片 `sort.Strings`。
- **helper：端点标签有两种历史格式**。`cost.go` 的 `splitEndpointProviderModel` 只认 `:`（新格式），
  旧日志是 `/` 连接的（见 `detail.go` 的 `attemptUpstream` 注释）。本文件需要一个两种都认的小函数
  （先 `SplitN(":", 3)`，不成再 `SplitN("/", 3)`）。
  **注意**：不要顺手去改 `splitEndpointProviderModel` —— 那会改变旧日志的 $ 估算结果，属于本批范围外
  的行为变更，登记在 §5 风险表里即可。

**③ `internal/report/build_cached.go`（+1 参数）**

`buildInternal` 与 `BuildCached` 增加 `quotas map[string]ProviderQuotaRef` 参数；
**`Build` 的签名保持不变**，内部传 `nil`。

已核对的实际收益：`BuildCached` 全仓库只有 **1 个生产调用点**（`cmd/vmr/cmd_report.go:204`）
+ **6 个测试调用点**；而 `Build` 有 **26 个测试调用点**、0 个生产调用点。保持 `Build` 签名不变
= 26 处测试一行不用改。这是本批把改动面压到最小的关键，不是风格偏好。

**④ `internal/report/aggregate.go`（+1 行）**

在 `rep.Tools = buildTools(sess)` 附近加：`rep.Providers = buildProviders(rep, quotas)`。

**⑤ `internal/report/section_provider.go`（新文件，~90 行）**

`renderProviders(w, rep, lang)`，渲染为 **§2.5 账户（Provider）消耗与额度**：

- 主表：账户 / 模型数 / 请求数 / 成功率 / token 三分 / 缓存效率 / 均值耗时 / 错误率 / $ 估算
- 额度列：有 `Quota` 才显示，形如 `20000 AFP（tokens · 1mo）`，并在表下用**一句**说明限定：
  未加权、统计窗口与计费周期不对齐、仅供数量级参考。
- `len(rep.Providers) == 0` 直接 return，不渲染空章节。
- $ 列仅在 `CostEstimate != nil` 时有值（无定价的 AFP 账户会是 `-`，这正是本功能存在的场景）。

**⑥ `internal/report/render_doc.go`（+1 行）**

`Markdown()` 里 `renderCostEstimate` 之后插入 `renderProviders(w, rep, lang)`。

**⑦ `internal/i18n/report_provider.go`（新文件）**

`ProviderText` struct + `Provider(lang) ProviderText`：标题、表头数组、额度单元格格式化函数、
限定说明句。

**⑧ `cmd/vmr/cmd_report.go`（+~25 行）**

新增 `buildProviderQuotas(configPath string, tw io.Writer) map[string]ProviderQuotaRef`：
`config.Load` → 遍历 `cfg.Providers` → `p.Quota != nil && len(p.Quota.Limits) > 0` →
取 `p.Quota.Limits[0].Resolved`（`core.Limit`，字段 `Metric`/`EveryText`/`Amount`）。
**降级姿态照抄 `buildPricing`**：config 读不到/校验不过 → 返回 nil，报表照常出，只是没有额度列。
（`config.Load` 已在 `buildPricing` 里调用过一次，可以顺手复用返回值，避免读两遍——实现时按当时的
代码形状决定，不强求。）

### 1.3 测试

- **`internal/report/provider_test.go`（新）**：
  - 上卷正确性：手工构造若干 `EndpointRow`（同一 provider 多个 model），断言 token/attempts/
    errorClasses 求和正确、`Models` 去重且有序、`CacheEfficiency` 与 `DurMSMean` 计算正确。
  - 两种端点格式（`:` 与 `/`）都能正确解出 provider。
  - `Quota` 为 nil 与非 nil 两种情况。
  - **确定性**：同一输入跑两次，`Providers` 顺序逐字节一致（含 token 打平时按名字 tie-break）。
- **`internal/report/section_provider_test.go`（新）**：渲染快照式断言，参照
  `section_endpoint_value_test.go` 的写法；覆盖"有额度/无额度""有 $ /无 $ "四种组合，以及
  `Providers` 为空时不产生章节标题。
- **`build_cached_test.go`**：补新参数（传 nil 即可，除非要顺带测额度透传）。

### 1.4 文档

- **`docs/VirtualModelRouter_Design_v4_Analytics.md`**：`vmr report` 的数据形状一节（列举
  `Overall`/`ByModel`/... 那段）加上 `Providers`；章节运行顺序那段加 §2.5 一句话说明，需含"额度是
  未加权参照值"这个限定。
- **`docs/UserGuide.zh.md`（约 338 行 §2 那条）+ `docs/UserGuide.md`（对应英文条目）**：各加一条
  §2.5 说明。**注意英文版第 336 行的 `nine numbered sections` 不用改**——§2.5 是小数编号，
  `vmr report` 的编号章节仍是 §0–§8 九个。
- **`CHANGELOG.md`**：`[Unreleased]` 的 `Added` 下一条。
- `README.md`/`README.zh.md`：无需改动（已核对）。

### 1.5 完成情况（2026-08-12，Sonnet 5）

✅ **按计划完成，无偏离。** `internal/report/rows.go`（+`ProviderRow`/`ProviderQuotaRef`）、
`internal/report/provider.go`（新文件，`buildProviders` + 两种端点标签格式都认的
`splitEndpointProviderModelAny`）、`internal/report/section_provider.go`（新文件）、
`internal/i18n/report_provider.go`（新文件）、`internal/report/build_cached.go`
（`Build` 签名不变，`BuildCached` 加 `quotas` 参数）、`cmd/vmr/cmd_report.go`
（`buildProviderQuotas`，降级姿态照抄 `buildPricing`）均已落地；`aggregate.go` 净增 1 行
（`rep.Providers = buildProviders(rep, quotas)`），符合预算。测试：`provider_test.go`
（上卷正确性、两种端点格式、额度 nil/非 nil、确定性排序）+ `section_provider_test.go`
（有额度/无额度、有 $/无 $、空 Providers 不出章节、中英文）全部通过。文档（设计文档数据形状
+ 章节顺序、`UserGuide.md`/`.zh.md` 的 §2.5 条目、`CHANGELOG.md`）均已更新。真实历史审计日志
冒烟测试：`config.mba.yaml` 场景下 `volcengine` 账户正确显示配置的额度参照
（`18000 (requests · 1mo)`），另外两个历史账户（无 quota 配置）额度列显示 `-`，无 $ 定价的账户
$ 列显示 `-` 而不是报错或跳过整行——AFP 类账户场景验证通过。

### 2.1 设计要点

按 `(client_key_tag, endpoint)` 累计 token，**呈现上按 client 分组、组内按 token 降序**，不是二维
矩阵（方案 §3.2：这样既回答了"谁在消耗哪个账户"，又没有矩阵的稀疏可读性问题）。

**key 用完整端点标签（`protocol:provider:model`）而不是模型名**——`config.mba.yaml` 里
`deepseek-v4-flash` 同时挂在 `volcengine` 和 `volcengine2` 下，只按模型名拆会把两个账户合并，
恰好抹掉最需要的那一刀。

这一批**必须**在流式过程中累计（没有任何现成桶是按这个 key 分的），所以走 `sticky.go` 的
collector 模式。**只累计 token 与请求数，不做分位数、不做 $ **（$ 的按客户端/按端点视图 §2 已有），
从而不需要缓冲任何原始切片。

### 2.2 改动清单

- **`internal/report/rows.go`**：`Report2` 加 `ClientEndpoints []ClientEndpointRow`；新类型字段：
  `ClientKey`/`Endpoint`/`Requests`/`TokensIn`/`TokensInCached`/`TokensInFresh`/`TokensOut`。
- **`internal/report/clientendpoint.go`（新文件，~70 行）**：`clientEndpointCollector` +
  `newClientEndpointCollector()` / `.add(rc *rec2)` / `.result() []ClientEndpointRow`。
  - `.add`：`rc.clientKey == "" || rc.endpoint == ""` 直接跳过；`rc.usageOK` 才累加 token。
  - `.result` 排序：ClientKey 字典序 → 组内 TokensIn 降序 → **tie-break 用 Endpoint 字典序**。
- **`internal/report/aggregate.go`（+3 行）**：`newClientEndpointCollector()` / 主循环里
  `.add(rc)`（挨着已有的 `stickyCol.add(rc)`）/ 结尾 `rep.ClientEndpoints = ...result()`。
- **`internal/report/section_client_endpoint.go`（新文件，~70 行）**：渲染
  **§5.5 按客户端的上游归属**——每个 client 一个小标题 + 一张表（端点 / 请求数 / fresh / cached /
  out / 占该 client token 的百分比）。空则整节不渲染。
- **`internal/report/render_doc.go`（+1 行）**：`renderWorkload` 之后调用。
- **`internal/i18n/report_client_endpoint.go`（新文件）**。

### 2.3 测试

- **`internal/report/clientendpoint_test.go`（新）**：分组与排序正确；空 clientKey/空 endpoint 被
  跳过；`usageOK=false` 的记录只计请求数不计 token；确定性（两次运行顺序一致）。
- **`section_client_endpoint_test.go`（新）**：渲染断言 + 空数据不出标题。

### 2.4 文档

- 设计文档数据形状一节加 `ClientEndpoints`，章节运行顺序那段加 §5.5。
- `docs/UserGuide.zh.md`（约 341 行，现写"按虚拟模型、按工作负载类、按端点、按客户端（后两张表
  还带每请求输入/输出 token 分位数）"）与 `docs/UserGuide.md` 对应条目：各补一句 §5.5。
- `CHANGELOG.md` 一条。
- `README.md`/`README.zh.md`：无需改动。

### 2.5 完成情况（2026-08-12，Sonnet 5）

✅ **按计划完成，无偏离。** `internal/report/clientendpoint.go`（新文件，`clientEndpointCollector`，
key 用完整端点标签而非模型名）、`internal/report/section_client_endpoint.go`（新文件，按 client
分组、组内 token 降序）、`internal/i18n/report_client_endpoint.go`（新文件）均已落地；
`aggregate.go` 净增 3 行（collector 初始化 + `.add(rc)` + 结果赋值），符合预算。测试：
`clientendpoint_test.go`（分组排序、按完整端点标签而非模型名区分账户、空 key 跳过、
`usageOK=false` 只计请求数、确定性）+ `section_client_endpoint_test.go`（分组渲染、空数据不出
标题、中英文）全部通过。文档已更新。真实数据冒烟测试正是 dev plan 强调的场景：
`config.mba.yaml` 里 `deepseek-v4-flash` 同时挂在 `volcengine`/`volcengine2` 两个账户下——真实
历史日志渲染出的表格证实按端点标签分组确实避免了把两个账户的消耗合并成一行（见批次完成后的
综合冒烟：`hermes`/`lobster` 等客户端的表格里 `opencode`/`sub2api`/`volcengine` 分列显示）。

---

## 3. 批 3：Story 的模型使用与切换（`vmr story`）

### 3.1 设计要点（含两处必须避开的坑）

**坑一：模型名不能取 `Manifest.Model`。** 那是 `audit.Record.Model`，即**虚拟模型名**
（`coding`/`agent`），一个 Journey 内客户端始终请求同一个，照它实现会得到一张永远显示"未换过模型"
的表。真实上游模型在 `Manifest.Endpoint`（最后一次 attempt 的 `protocol:provider:model`）与
`Step.Rec.Attempts[i]` 的结构化字段 `Protocol`/`Provider`/`Model`。

**坑二：端点标签两种历史格式**（`:` 新 / `/` 旧），且 `internal/story` **不能 import
`internal/report`**（archtest 禁止），拿不到现成的 `attemptUpstream`。

**取值策略（推荐）**：优先读 `Step.Rec.Attempts[len-1]` 的结构化 `Model`/`Provider` 字段；为空
（很旧的日志）再回退到切分 `Manifest.Endpoint`（先 `:` 后 `/`）。

**关于是否把 `attemptUpstream` 下沉到 `internal/audit` 共用**：方案 §5.5 ② 倾向下沉（这个项目被
"同一事实两份实现"坑过一次）。**本 dev plan 的建议是：本批先在 `internal/story` 内写一个私有小
helper，不动 `internal/audit`。** 理由是下沉会同时改动路由半区共用的 `internal/audit` 与
`internal/report` 的既有函数，把一批"纯新增、零回归风险"的工作变成"有回归面的重构"；等第二梯队真
需要第三个消费者时再下沉不迟。**如果实施时倾向下沉，必须单独一个 commit，且不与本批混在一起。**

### 3.2 改动清单

- **`internal/story/modelusage.go`（新文件，~110 行）**：
  - `ModelUsageStat{Model, Provider string; Steps int; TokensIn, TokensInCached, TokensOut int64}`
  - `ModelSwitch{StepSeq int; From, To string; OnFailoverStep bool}`
  - `computeModelUsage(steps []*Step) ([]ModelUsageStat, []ModelSwitch)`：按顺序遍历，
    按"provider+model"聚合 token（token 取 `s.Manifest.Usage`，`UsageOK` 才计），
    相邻 Step 取值不同即记一次切换；`OnFailoverStep = len(s.Rec.Attempts) > 1`。
  - 上游取值 helper（见 §3.1）。
  - `ModelUsageStat` 排序：TokensIn 降序，**tie-break 用 "provider:model" 字典序**。
- **`internal/story/metrics.go`（+~14 行）**：`Metrics` 加两个字段
  （`ModelUsage []ModelUsageStat` / `ModelSwitches []ModelSwitch`，带 json tag 与说明注释），
  `ComputeMetrics` 里加一行调用。
  **副作用（是好事，无需额外工作）**：`Summarize` 内嵌 `Metrics`，所以 `journey-<id>.json`
  自动带上这两个字段。
- **`internal/story/render_modelusage.go`（新文件，~70 行）**：`renderModelUsage(w, m, lang)`
  - 模型使用表：模型（带 provider）/ Step 数 / in / cached / out
  - 切换列表：仅 `len(ModelSwitches) > 0` 时渲染；每行"第 N 步：A → B"，
    `OnFailoverStep` 的追加一句客观描述（**不写因果断言**，措辞纪律见方案 §5.3）。
  - **绝对不要写进 `render_spine.go`**（只剩 1 行预算）。
- **`internal/story/render_md.go`（+1 行）**：`renderOverviewCard` 之后调用 `renderModelUsage`。
- **`internal/i18n/story_modelusage.go`（新文件）**。

### 3.3 测试

- **`internal/story/modelusage_test.go`（新）**：
  - **最关键的一条**：构造一个 Journey，其 `Record.Model`（虚拟名）全程不变、但 attempts 的真实
    模型中途改变，断言确实检出切换——**这条测试就是坑一的守门人**，必须有。
  - 单模型 Journey → 切换列表为空。
  - `OnFailoverStep`：多 attempt 的 Step 上发生的切换被标记。
  - 结构化字段缺失时回退切分 Endpoint（`:` 与 `/` 两种）。
  - 确定性排序。
- **`internal/story/golden_test.go` 的 golden 文件必须重生成**：
  `UPDATE_GOLDEN=1 go test ./internal/story/ -run Golden`，然后**逐行 review diff 再提交**
  （`testdata/golden.md` 与 `golden_zh.md` 两份）。
  注意现有 fixture 的 attempt 是 `openai:provider:agent`，重生成后会多出一张单模型表 + 空切换节。
- **`metrics_test.go`**：若有"Metrics 全字段"式断言需同步。

### 3.4 文档

已核对过的精确落点（不用再全文搜一遍）：

| 文件 | 位置 | 动作 |
|---|---|---|
| `docs/VirtualModelRouter_Design_v4_Analytics.md` | "九项"共 **10 处** | 行为剖面表加一行；把"九项"统一改成"九项 + 模型使用/切换"或"十项"，**10 处都要过一遍**，漏改即文档自相矛盾 |
| 同上 | 语料级统计一节的"**九项指标里的 12 个数值字段**" | **保持 12 不变**——新增的两个字段都是列表型（同 `ToolCallDist`），进不了 Spearman 那套只吃标量的机制。这里只需把"九项"随上一行一起改，数字 12 不动 |
| `docs/UserGuide.zh.md` | "九项"共 **2 处**；第 433 行"十二项行为剖面数值" | 前者改，后者**不动**（理由同上） |
| `docs/UserGuide.md` | 第 427 行 `nine rule-derived, zero-LLM-cost numbers` | 改（英文版没有"九项"字样，是这句） |
| `docs/UserGuide.md` | 第 336 行 `nine numbered sections` | **不动**——那是 `vmr report` 的章节数，批 1/批 2 用的是 §2.5/§5.5 小数编号，`vmr report` 仍是 §0–§8 九个编号章节 |
| `CHANGELOG.md` | `[Unreleased]` → `Added` | 一条 |
| `README.md` / `README.zh.md` | — | **无需改动**（已核对：README 只做能力概述，不列具体指标） |
| `internal/i18n/lang_test.go` | — | **无需改动**（已核对：只测 `Parse`，没有 bundle 枚举） |

### 3.5 完成情况（2026-08-12，Sonnet 5）

✅ **按计划完成，无偏离，含 §5.5 ② 的刻意收敛（不下沉 `attemptUpstream`，本批仅一个 commit
范围内完成，未涉及 `internal/audit`）。** `internal/story/modelusage.go`（新文件，
`computeModelUsage` + 私有 `stepUpstream`/`splitEndpointLabel`，优先读 `Step.Rec.Attempts`
最后一次尝试的结构化 `Provider`/`Model`，为空才回退切分 `Manifest.Endpoint`）、
`internal/story/render_modelusage.go`（新文件）、`internal/i18n/story_modelusage.go`（新文件）
均已落地；`metrics.go` 净增 10 行（401→411，预算 470，两个新字段 + `ComputeMetrics` 里一行调用）、
`render_md.go` 净增 1 行（301→302，预算 350），`render_spine.go` 全程未碰（仍是 379 行，
预算 380）。测试：`modelusage_test.go` 七个用例全部通过，**"坑一守门测试"
（`TestComputeModelUsage_DetectsSwitchDespiteConstantVirtualModel`）已实现并通过**——构造一个
`Record.Model`（虚拟名）全程为 `"agent"` 不变、但两次请求 attempts 的结构化 `Provider`/`Model`
不同的 Journey，`Build`+`ComputeMetrics` 后 `ModelSwitches` 确实检出了 1 次切换，证明实现没有
误取 `Manifest.Model`。golden 文件（`testdata/golden.md`/`golden_zh.md`）已用
`UPDATE_GOLDEN=1 go test ./internal/story/ -run Golden` 重生成并人工核对 diff——按预期只多出一张
单模型表 + "全程未切换"提示（fixture 的 attempt 只设了 `Endpoint`，走的正是回退切分路径，
验证了坑二的 fallback）。文档三处更新到位。真实历史审计日志冒烟测试**额外发现了一个真实场景**
（非造假数据）：`hermes` client 的一个 Journey 在真实数据里确实发生了一次上游切换
（`sub2api:gemini-3.6-flash-high → opencode:deepseek-v4-pro`，第 11 步），Markdown 与
`journey-*.json` 的 `model_usage`/`model_switches` 字段完全一致，验证了端到端链路。

---

## 4. 全批次完成后的收尾

1. ✅ `go test ./...`（含 `-race` 对 report/story 无必要，这两个包无并发新增）——全绿；唯一一次
   失败（`internal/server` 的 `TestActiveProbe_UpstreamFailureGoesToReportFailure`）单独重跑
   即过，是既有时序类 flaky 测试，与本次三个改动包无关。
2. ✅ 真实数据冒烟：用仓库里真实的历史审计日志（`./logs/vmr-audit-2026-08-0{1,2,3}.jsonl.zst`，
   而非 `/tmp/rp` 示例路径下的空跑）跑了 `vmr report`/`vmr story`，人眼确认三处新内容渲染正常、且
   **在没有定价数据的 AFP 账户上也照常出表**——`opencode` 账户 $ 列显示 `-`，`volcengine` 账户的
   额度参照列正确显示 `18000 (requests · 1mo)`，都符合预期。
3. ✅ 中英文各跑一次（`-lang zh` / `-lang en`），`grep -P '[\x{4e00}-\x{9fff}]'` 复查确认 §2.5/§5.5
   两个新章节的英文输出里没有残留中文——唯一命中的中文都是既有 §6 会话标题里的真实用户提问原文
   （用户输入内容，不是硬编码 UI 文案，不算遗漏）。

---

## 5. 风险与已知边界

| 风险 | 处置 |
|---|---|
| `aggregate.go` 只剩 40 行余量，改动一多就触发预算失败 | 批 1 设计成"事后派生"（+1 行）、批 2 用 collector（+3 行）。**若实施中发现需要更多行，是设计跑偏的信号，应把逻辑挪进新文件，而不是抬高预算数字**（archtest 注释里明确写了这条） |
| `splitEndpointProviderModel`（`cost.go`）只认 `:` 格式，旧日志的 $ 估算会静默跳过 | **本次不改**——改它会变更历史报表的成本数字，属独立议题。批 1 的新 helper 自己两种都认，不受影响。登记在此备查 |
| 批 3 golden 文件必须重生成，容易漏 | 已写进 §3.3 步骤；`go test ./internal/story/` 不重生成就会红，漏不掉 |
| 设计文档多处写死"九项指标" | 已在 §3.4 列为必改项，用 `grep -n "九项" docs/` 定位 |
| Provider 额度参照可能被误读成"精确余量" | 渲染层强制带限定说明句（§1.2 ⑤），且 JSON 字段名用 `quota`（声明值）而非 `remaining`/`used` 之类暗示实时状态的词 |
| `Build` 签名不变、只改 `BuildCached`，可能让人误以为 `Build` 也支持额度 | `Build` 的 doc comment 补一句"额度参照只走 `BuildCached`，`Build` 传 nil" |
| 批 2 的表在 client 或 endpoint 很多时可能偏长 | 分组渲染天然比矩阵短；若真实数据下过长，再补 Top-N（**届时必须显式写明被截断了多少**，遵循项目"不做静默截断"的既有纪律），本批先不预设 |

---

## 6. 与方案文档的对应关系（自查表）

| 方案 | dev plan | 状态 |
|---|---|---|
| §3.2 Client 按端点拆分 | 批 2 | ✅ |
| §3.1 不补第二个缓存命中率口径 | 未列入任何批次 | ✅ 明确不做 |
| §4.2 Provider 汇总（含放弃分位数的简化） | 批 1，`DurMSMean` 替代 | ✅ |
| §4.3 额度参照（不碰 `internal/router`） | 批 1 ⑧，只读 `Resolved core.Limit` 静态字段 | ✅ |
| §4.4 输入长度分层分布 | 不做（第二梯队） | ✅ |
| §5.4 Story 最小指标集 | 批 3 | ✅ |
| §5.5 ① 模型名取端点非 `Manifest.Model` | 批 3 §3.1 坑一 + 专门的守门测试 | ✅ |
| §5.5 ② `attemptUpstream` 是否下沉 | 批 3 §3.1：**本批不下沉**，理由已写明 | ⚠️ 与方案的倾向不同，是刻意的收敛 |
| §5.5 ③ 切换次数登记进 compare/corpus | 不做（第二梯队） | ✅ |
| §7 行数预算约束 | §0 表格 + §5 风险表 | ✅ |
