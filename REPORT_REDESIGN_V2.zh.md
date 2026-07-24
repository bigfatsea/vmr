<!-- Ver 2026-07-24 15:30, by Sonnet 5 -->

# VMR 报告重新设计 V2

> 对 `REPORT_REDESIGN.zh.md`(以下简称 **V1**)的复审与迭代。
> 同样基于 `logs/vmr-audit-2026-07-24.jsonl`(182 条记录)与实际生成的 `reports/*` 校验。
> 同样是**设计文档**,不是生成的输出;未修改任何现有文件,不实现任何默认开启的新功能。
> 已阅读 `internal/report/{report,markdown,session,export}.go` 源码以核对指标的真实计算口径(而不仅凭渲染样例猜测)。

---

## 第 0 部分 - 结论(TL;DR)

V1 的核心诊断是对的:**统一宽表 + 派生信号被埋没**确实是当前 `vmr-report.md` 最大的问题,V1 提出的"八个运维问题 → 章节""七个指标族 → 表格"这套骨架也基本站得住,V2 保留约 90%。

V2 在三处做了改动:

1. **修 bug**——V1 有 4 处内部不一致或设计错误(百分位不可减、指标重复定义、量纲不统一、"低置信度"标注覆盖不全),这些不是风格取舍,是必须改的问题。
2. **补一条缺失的第一性原理**——V1 说"相关性优先于统一性",但没有回答"从主表拿掉的数据去哪了"。个人项目的报告用得少、但用时要一次拿全,所以答案必须明确:**数据永远留在 JSON,Markdown 只砍展示形式(合并/Top-N/sparkline),不砍数据本身**。这条原则解决了任务里"该有的都要有,但别拥挤"这对表面矛盾。
3. **加三样低成本高价值的东西,砍两样不该做的**——加:可选定价估算、可选的与上次报告对比、compaction 链路的 mermaid 图(仅在链路复杂时触发);砍:V1 里量纲错误的一个指标、以及本次不做的多周热力图。

---

## 第 1 部分 - 对 V1 的评审结论

### 1.1 保留不变(V1 已经做对,不需要改)

| 项目 | 为什么保留 |
|---|---|
| 8 个运维问题 → 8 个章节的映射(§3.3) | 这是"报告回答问题而非展示数据"这条原则的具体落地,映射合理,每个问题都能在真实数据里找到对应的表格。 |
| P1–P5 五条设计原则 | 相关性优先、派生信号为先、渐进式披露、量化浪费——这几条经得起推敲,和 vmr 本身"简单可维护、消灭分支"的取舍哲学一致。 |
| 七个指标族(A–G)的整体框架 | 用"族"而不是"列模板"重组是对的方向,每张表选它需要的族,天然解决了统一宽表的问题。 |
| Sparkline 表示时间序列 | Unicode block,markdown/终端/git diff 都能显示,零依赖,不需要图片渲染管线。 |
| 渐进式披露(摘要→章节→索引→详情) | 和现有 `vmr-requests-index.md` 的 Chat User → Session → Task → Turn 层级一致,不用重新设计下钻路径。 |
| 会话表/工具表去噪(合并定时一次性任务、去掉延迟列) | 现有 `markdown.go` 已经在做"合并单发会话"这件事(`collapsedAgg`),V1 只是把这个思路推广到更多表,方向正确。 |
| JSON 是权威来源、Markdown 是呈现 | 和本次任务里"所有 Markdown 报告都要伴生 JSON/JSONL"的要求完全一致。 |
| `vmr-requests-index.md` ↔ `vmr-requests.jsonl` 已经满足"MD 必有 JSON 伴生" | 核对过 `export.go`:`vmr-requests.jsonl` 逐行带 `session`/`task`/`turn` 字段,能重新分组还原出和 index.md 一样的层级,这不是遗漏,是已经成立的事实,V2 里明确写出来,避免下次又被当成"缺失项"重新讨论。 |

### 1.2 需要修正(V1 有实质性问题)

| # | 问题 | 证据 | V2 处理 |
|---|---|---|---|
| F1 | **`stream_time_p95` 定义自相矛盾。** §4 C 族的指标定义表写的是 `dur_p95 − ttft_p95`(两个百分位相减),但百分位不是线性的——除非同一批请求里 dur 和 ttft 的排名完全相关,否则 `P95(dur) − P95(ttft) ≠ P95(dur − ttft)`。 | `report.go` 的 `finishRow`/`finishHour`/`finishEndpoint` 全部坚持"每个桶用自己的原始 `durs`/`ttfts` 切片直接算真百分位,不做跨桶近似"这一不变式(代码注释明确写了这一点,Format 9 就是为了这个而存在)。V1 §8.3 的实现说明其实写对了("需每请求 `dur−ttft`……在工作状态中与 `durs`/`ttfts` 一并收集"),但 §4 指标表和 §6 渲染样例都写成了两个百分位相减,前后矛盾。 | 统一改成:每个桶在收集 `durs`/`ttfts` 的同时,额外收集一份 `stream_ms = dur_ms − ttft_ms` 的切片,和其余百分位同样对待,算出真正的 `stream_ms_p50/p95`。删除"两个百分位相减"这个写法,任何地方都不再出现。 |
| F2 | **`cache_efficiency` 重复定义两次。** | B 族(§4)定义一次,F 族又定义一次,措辞还略有差异("派生指标"vs"杠杆而非标题")。 | 只在 B 族定义一次;F 族"效率与浪费"表里引用它,不重复定义,避免两处漂移。 |
| F3 | **`scheduled_prompt_redundancy` 量纲不统一,且和已有指标重复。** | 定义是"system_chars × scheduled_fires"——字符数乘触发次数,而报告里其余全部效率指标都是 token/字节/百分比。而且它想表达的信息("heartbeat 每次都重发一遍系统提示,很浪费")已经被 workload-class 的 `fresh` token 总量 + `cache_efficiency` 完整覆盖(V1 自己的渲染样例里就是靠这两个数字得出"heartbeat 烧掉 0.59M fresh tokens、占该负载输入 91%"这个结论,根本没用到这个新指标)。 | 直接删除这个指标。§6"效率与浪费"表里对应的"定时任务冗余"发现行,改为直接引用 §4 workload-class 表里已有的 `fresh` 列和 `cache_efficiency` 列,不新增一个量纲不一致、内容重复的派生量。 |
| F4 | **"低置信度"标注只覆盖了百分位,没覆盖比值类指标。** | V1 §5.3 规定"百分位始终带 `(n=…)`,`n<20` 标 `⚠ low-n`",但 `cache_efficiency`、`tool_declare_utilization` 这类比值的分母经常比 `requests` 小得多——实测这份日志 `tokens_known=167` 而 `requests=182`(15 条失败请求没有 usage 字段),`cache_efficiency` 是在 167 上算的,不是 182,但 V1 的渲染样例完全没提这一点。 | 把"分母远小于总量时要标注"这条规则从"仅百分位"扩展到"任何比值类派生指标":当某个比值的分母 / 总 `requests` < 90% 时,该单元格追加一个脚注引用(如 `84.0%¹`),表末用一行说明"¹ 基于 167/182 条有 usage 字段的请求"。 |

### 1.3 新增(低成本、高价值,V1 没有)

| # | 新增内容 | 为什么值得加 |
|---|---|---|
| A1 | **可选的定价/成本估算配置**(默认关闭) | 个人项目最关心的问题之一就是"这个月烧了多少钱",而 token 数字对大多数人没有直觉。V1 只写了"若配置了定价"这几个字,没给出具体 schema——V2 把它设计完整(见第 4 部分),同时明确写清楚一个必须有的免责声明:**价格是配置文件里的当前值,不代表历史请求发生时的真实价格**,避免历史报告的 $ 数字被误当作精确账单。 |
| A2 | **可选的"与上次报告对比"** | 个人项目通常是"隔一阵子才看一次报告",这时候最想知道的是"相比上次是变好了还是变差了"。成本很低——只需要能读到一份之前的 `vmr-report.json`,做减法即可,不需要额外的存储或数据库。默认不出现,只有显式传入 baseline 才触发。 |
| A3 | **compaction 链路的 mermaid 图**(仅在链路长度 ≥2 时触发) | 现有 `session.go` 的 `linkCompactions` 已经在追踪"这次 compaction 总结了哪个会话、又被哪个会话续上"(`Summarizes`/`ContinuesTo`),渲染上目前只是一个文本箭头 `s01 ← s03`。单跳链路用文本箭头完全够用,不需要图;但如果一个长期运行的 agent 会话经历了 3 次以上 compaction(常见于跑了很多天的项目),文本箭头会变成一串很难跟踪的 `s01 ← s07 ← s12 ← s19`,这时候一张 mermaid flowchart 比文本更直观。**只在这种情况下才用图**,不是所有会话都画。 |

### 1.4 明确不做(以及为什么)

| 项目 | 为什么不做 |
|---|---|
| 多日/多周热力图(weekday × hour) | 个人项目的审计窗口通常是单日或几天,YAGNI。真要看这个,`hours[]`/`hours_of_day[]` 已经在 JSON 里,用 DuckDB/pandas 五分钟能画出来,不值得为了一个低频需求在报告生成器里加渲染逻辑。 |
| 统一 `loadtest-report.md` 的格式 | 数据来源完全独立(`loadtest/runner`),不是 `vmr-report` 这条管线,超出这次"报告重新设计"的范围,V1 也没碰它,V2 保持一致。 |
| 接入外部实时价格 API | 违反"本地单二进制、不依赖外部服务"的项目定位。定价必须是本地手工维护的配置文件,过时是用户自己的责任,报告只负责在页脚提醒。 |

---

## 第 2 部分 - 修订后的第一性原理

沿用 V1 §3.1/3.2 的问题清单和 P1–P5,只补充两条:

**关于报告的使用场景**(补充说明,不是新原则):`vmr` 是个人/≤3 人团队自用的本地路由器,报告的读者就是部署者本人。这份报告**不是**面向团队的周报看板,也不是要给别人看的对外汇报——它是"运维者自己事后查账"的工具:用的频率不高,但每次打开,都不希望因为"这个数字报告里没有"而不得不重新跑一遍 `jq`/DuckDB 去补数据。这直接决定了本次任务里明确给出的取舍标准:**一个指标可有可无时,倾向于有**,除非成本特别高或特别难做。

- **P6(新)- 数据留 JSON,Markdown 只砍展示。** 任何从主报告 Markdown 里拿掉的明细,必须能在 `vmr-report.json`/`vmr-requests.jsonl` 里无损找回,否则不能拿掉,只能压缩展示形式(Top-N、合并、sparkline)。这是解决"该有的数据都要有,又不能拥挤"这对矛盾的具体机制——不是"少显示等于少数据",而是"数据在,只是不在你第一眼看到的那张表里"。
- **P7(新)- 可选功能默认零配置零成本。** 定价估算、与上次报告对比这类需要额外配置或状态的功能,在没有配置/没有 baseline 时必须完全不产生视觉噪音(除了一行"如何开启"的提示,和 V1 已有的"未配置定价 -> 不显示 $"处理方式一致)。

---

## 第 3 部分 - 修订后的指标族

沿用 V1 的 A/D/E/G 族(体量结果、线路载荷、工作负载形态、端点健康)不变,以下是有改动的族:

### B 族 - Token 经济(改动:无重复定义,新增定价相关字段标注为"仅配置定价时")

| 指标 | 定义 | 基数 | 备注 |
|---|---|---|---|
| `tokens_in` / `tokens_out` | 同 V1 | 带 usage 的记录 | |
| `tokens_in_fresh` | in − cached − cache_write | 带 usage 的记录 | |
| `tokens_in_cached` / `tokens_in_cache_write` | 同 V1 | 带 usage 的记录 | |
| `cache_hit_rate` | cached / in | 带 usage 的记录 | |
| **`cache_efficiency`**(唯一定义处) | cached / (cached + fresh) | 带 usage 的记录,分母 = tokens_known;当 tokens_known/requests < 90% 时脚注标注 | F 族"效率与浪费"表引用此处,不重复定义 |
| `tokens_reasoning` / `reasoning_share` | 同 V1 | 报告了它的记录 | |
| `avg_tokens_in` / `avg_tokens_out` | 同 V1 | tokens_known | |
| `cost_estimate`(★新,仅配置定价时) | fresh·price_in + cache_write·price_cw + out·price_out,按 endpoint 定价 | 若该 endpoint 有定价配置 | 见第 4 部分定价 schema |

### C 族 - 延迟与速度(改动:修正 stream_time 定义)

| 指标 | 定义 | 基数 |
|---|---|---|
| `ttft_p50` / `ttft_p95` | 同 V1,始终标 n | ttft_known |
| `dur_p50` / `dur_p95` / `dur_max` | 同 V1 | requests_with_dur |
| **`stream_ms_p50` / `stream_ms_p95`**(修正) | 每请求 `dur_ms − ttft_ms` 组成独立切片,和 `durs`/`ttfts` 一样在桶内直接算真百分位——**不是两个百分位相减** | 同时有 dur 和 ttft 的记录 |
| `slow_requests` / `throughput` / `bytes_per_sec` | 同 V1 | 同 V1 |

### F 族 - 效率/浪费(改动:删掉量纲不统一的指标,其余引用 B 族)

| 指标 | 定义 | 为何重要 |
|---|---|---|
| `cache_miss_tokens` | = tokens_in_fresh(引用 B 族) | 本可缓存却未缓存的输入 |
| ~~`cache_efficiency`~~ | 已移至 B 族,此处直接引用 | 避免重复定义漂移 |
| `tool_schema_bytes_shipped` / `tool_declare_utilization` / `tool_schema_waste_bytes` | 同 V1 | |
| ~~`scheduled_prompt_redundancy`~~(已删除) | 用 §4 workload-class 表的 `fresh` + `cache_efficiency` 表达同一件事 | 量纲(字符×次数)和其余指标不一致,且信息重复 |
| `output_truncation_rate` / `slow_request_share` | 同 V1 | |

### H 族(新增,可选)- 成本估算

仅在存在定价配置时出现,不出现时整族不存在(不留任何占位符,除了一行"配置定价以查看 $ 估算"的提示,与 V1 处理方式一致)。

| 指标 | 定义 | 基数 |
|---|---|---|
| `cost_estimate` | 见 B 族 | 该行有定价配置的 endpoint |
| `cost_currency` | 定价配置里声明的货币(不做汇率换算) | 全局,来自 pricing 配置 |
| `cost_as_of` | 定价配置的更新时间戳,渲染为免责声明 | 全局 |

---

## 第 4 部分 - 定价配置 schema(第一次给出具体设计,V1 只是提了一句)

本地文件,例如 `pricing.yaml`(sidecar,不进 `config.yaml` 主配置,避免定价这种"经常需要手改"的内容和路由配置混在一起):

```yaml
currency: USD          # 仅用于展示,不做汇率换算
updated_at: 2026-07-20  # 价格生效日期——报告页脚会提醒这不是历史价格
rates:
  - endpoint: "openai:volcengine:doubao-seed-2.0-lite"
    in_fresh_per_1m: 0.28
    cache_read_per_1m: 0.028
    cache_write_per_1m: 0            # 该 provider 无溢价缓存写入时填 0
    out_per_1m: 1.10
  - endpoint: "anthropic:volcengine:doubao-seed-2.0-lite"
    in_fresh_per_1m: 0.28
    cache_read_per_1m: 0.028
    cache_write_per_1m: 0.35         # Anthropic 缓存创建按溢价计费
    out_per_1m: 1.10
```

设计要点:

- **按 `endpoint`(`provider:real-model`)定价,不是按虚拟模型。** 同一个虚拟模型(如 `coding`)可能路由到不同 provider/真实模型,定价必须跟着真实计费方跑,而不是路由层的抽象名字——这和 `EndpointRow` 现有的分组粒度天然一致,不需要新的分组逻辑。
- **未在 `rates` 里出现的 endpoint,该行只显示 token 类别,不显示 $。** 不允许用平均价格/默认价格做估算,宁可缺失也不给错误数字。
- **`updated_at` 必须渲染成免责声明**,格式类似:"成本估算基于 2026-07-20 的价格配置,不代表报告所涵盖历史请求实际发生时的价格。"——避免几个月后回看一份旧报告时把 $ 数字误当作真实账单。
- 完全不存在这个文件时,B/H 族的 `cost_estimate` 相关字段一律省略(Go 里对应 `omitempty`),Markdown 侧只显示 token 类别 + 一行提示,和 V1 已有做法一致。

---

## 第 5 部分 - 修订后的报告结构

### 5.1 目录(与 V1 §5.1 基本一致,标注改动处)

```
# VMR 用量报告
  ## §0 摘要 (Executive Summary)           - 5 个数字 + ≤3 条自动亮点 [+ 可选:较上次 Δ]
  ## §1 成本与 Token 经济                   - B 族 [+ 可选成本估算列]
  ## §2 可靠性                              - A 族 + G 族
  ## §3 延迟与吞吐                          - C 族(stream 列已修正为真百分位)
  ## §4 负载分布                            - E 族(模型 / 类 / 小时 / 客户端)
  ## §5 会话与任务                          - 下钻 [+ 可选成本列] [+ 复杂 compaction 链路时的 mermaid 图]
  ## §6 效率与浪费                          - F 族 [+ 工具浪费 Top-N 表,替代全量倾倒]
  ## §7 请求详单                            - 链接到 vmr-requests-index.md
  ## 附录 数据源与方法论                     - [+ 定价免责声明,若配置][+ baseline 信息,若对比]
```

**没有新增独立章节**——"与上次对比"和"定价"都是**已有章节内的可选列/可选行**,不是新的顶层小节。这是刻意的:每多一个顶层 `##`,报告的心智负担就多一分,而这两样东西的信息量并不需要独立章节,塞进既有表格的一列或一行足够,符合"渐进式披露"和"不为了功能而膨胀结构"的取舍。

### 5.2 有实质改动的章节规格

**§0 摘要** —— 沿用 V1 的紧凑 2 行卡片。新增:若提供了 baseline(上次报告),标题数字后追加 Δ,例如 `p95 dur: 27.9s (▲+3.2s)`。没有 baseline 时这一列完全不出现,不留空占位。

**§1 成本与 Token 经济** —— 沿用 V1 的 Token 类别拆分表和按模型缓存效率表。若配置了定价,"按模型缓存效率"表追加一列 `est. cost`;表格下方追加一行免责声明(见第 4 部分)。未配置时,和 V1 一致,只显示"配置定价以查看 $ 估算"。

**§3 延迟与吞吐** —— 表头改为:
`model | protocol | req | ttft p50/p95 (n) | dur p50/p95/max (n) | stream_ms p50/p95 (n)★ | slow>30s | tok/s`
`stream_ms` 列现在是真百分位(不是两个百分位相减),n 独立标注(有些请求有 dur 没有 ttft,反之亦然,stream_ms 的基数是"两者都有"的交集,通常比单独的 dur/ttft 基数小,必须单独标)。

**§5 会话与任务** —— 沿用 V1 的下钻表(去掉延迟列)。若配置了定价,追加一列 `est. cost`——这通常是打开这份报告最想直接看到的数字:"哪个会话/哪个任务花得最多"。

若某条会话涉及 ≥2 次 compaction 链路,在会话表下方追加一个 mermaid 图,例如:

```mermaid
flowchart LR
    s01["s01<br/>audio-album-creator"] -->|compacted| s07["s07<br/>continue album work"]
    s07 -->|compacted| s12["s12<br/>continue album work"]
    s12 -->|compacted| s19["s19<br/>continue album work (current)"]
```

链路长度 = 1(只有一次 compaction)时保持 V1 的纯文本 `sNN ← sMM`,不触发图——图的成本(渲染管线、markdown 阅读器需支持 mermaid)只有在文本箭头已经读不清楚的时候才值得付。

**§6 效率与浪费** —— 沿用 V1 的"发现表",去掉"定时任务冗余"这一行(信息已在 §4 workload-class 表里,不重复),追加一张紧凑表:

```
**工具形态浪费 Top-5**(按浪费字节降序;完整 63 项明细见 vmr-report.json → tools[])

| 形态 | 请求 | 声明 | 已用 | 利用率 | 浪费字节 |
|---|---|---|---|---|---|
| tools:67/7bc83937 | 53 | 67 | 4 | 6.0% | ≈5.5 MB |
| tools:4/35a9e5d2  | 129 | 4 | 4 | 100% | 0 |
```

这张表直接回答"该裁哪个工具集"这个问题,而不需要读者自己从 63 行编号列表里心算利用率。**完整的"已调用工具排名"和"从未调用工具字母序列表"不再出现在 Markdown 里,只保留在 JSON 的 `tools[]` 里**——这是 P6 原则的具体应用:数据没丢,只是不在默认视图里。

### 5.3 排版规则(在 V1 §5.3 基础上追加一条)

- **低置信度标注扩展到所有比值类指标**(不只是百分位):当某比值的分母 / 总 `requests` < 90% 时追加脚注引用,而不是像 V1 那样只对 `n<20` 的百分位做这件事。

---

## 第 6 部分 - JSON schema 补充

在 V1 §8.1 的基础上,把新增字段的类型和位置写清楚(JSON 是权威来源,必须先把 schema 定下来,Markdown 才是它的呈现):

```jsonc
// 顶层新增,均为 omitempty —— 不存在时整个键都不出现,不是空数组/空对象
{
  "pricing": {                          // 仅当 pricing.yaml 存在时出现
    "currency": "USD",
    "updated_at": "2026-07-20",
    "rates": [ { "endpoint": "...", "in_fresh_per_1m": 0.28, "...": "..." } ]
  },
  "efficiency": [                       // 镜像 §6 发现表,始终存在(不依赖 pricing)
    { "finding": "tool_schema_waste", "metric": "tool_declare_utilization",
      "value": 0.06, "implicated": "tools:67/7bc83937", "action": "..." }
  ],
  "by_client": [                        // 数据早已存在于 audit 记录(client_key_tag),只是没聚合——零新采集成本
    { "client_key_tag": "pimini", "requests": 129, "...": "..." }
  ],
  "compare": {                          // 仅当 --compare-to 传入 baseline 报告时出现
    "baseline_generated_at": "2026-07-17T...",
    "baseline_records": 150,
    "deltas": { "p95_dur_ms": 3200, "cache_efficiency": -0.02, "...": "..." }
  }
}
```

行级新增字段(在已有 `Row`/`EndpointRow`/`WorkloadRow`/`SessionRow`/`ToolShapeRow` 上追加):

- `tokens_in_fresh`、`cache_efficiency`、`slow_requests`(V1 已提出,确认沿用)
- **`stream_ms_p50`/`stream_ms_p95`**(★V2 新增,替代 V1 里错误的"两个百分位相减"写法)——需要在 `finishRow`/`finishHour`/`finishEndpoint` 里像 `durs`/`ttfts` 一样,额外收集一份 `streamMS []int64` 工作态切片,记录时机是"该请求同时有 `dur_ms>0` 和 `ttft_ms>0`"时;计算成本和现有 `percentiles(durs)` 完全一样,零额外 I/O。
- `ToolShapeRow.SchemaBytesShipped`/`DeclareUtilization`/`SchemaWasteBytes`(V1 已提出,沿用)
- `cost_estimate_usd`(★新,仅当对应 endpoint 有定价时出现在 `Row`/`EndpointRow`/`WorkloadRow`/`SessionRow` 上)

---

## 第 7 部分 - 实施优先级(分期,供后续实现时参考,不是本文档要做的事)

**Phase 1(零 schema 破坏性改动,纯计算 + 渲染修正,建议优先做)**
`tokens_in_fresh` / `cache_efficiency`(减法+比值)、`stream_ms` 真百分位(修 F1 那个 bug)、`slow_requests`、`by_client` 摘要汇总(数据已在,只是没聚合)、工具浪费 Top-N 表、比值类指标的低置信度标注。这些全部是"现有原始值上多算一步",没有新的采集成本,应该最先做。

**Phase 2(需要新配置面,opt-in)**
定价配置 + `$` 估算(B/H 族,§1/§5/§6 的可选列)。

**Phase 3(nice-to-have,低优先级)**
与上次报告对比、compaction 链路 mermaid 图——都是真实价值存在但触发频率低的功能,不阻塞 Phase 1/2。

---

## 第 8 部分 - 范围纪律(沿用 V1 §8.4,追加一条)

- 不加图表库、不加 web UI、不加数据库(同 V1)。
- **不接入外部实时价格 API**——定价是本地手工维护的 sidecar 配置文件,过时是用户自己的责任,报告只负责在页脚给免责声明,不负责保证价格准确性。
- **mermaid 图只用于"文本/表格表达不清的关系数据"**(目前唯一场景:长 compaction 链路),不用于本可以用 sparkline 或表格说清楚的时间序列或分布——避免为了"看起来专业"而引入不必要的图表复杂度和渲染依赖。
- `loadtest-report.md` 不在本次范围内(同 V1)。
