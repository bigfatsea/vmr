// Ver 2026-09-06 14:30, by Opus 5

# vmr analyze 架构重构方案：领域切片数据核心 + ViewModel 双轨渲染 + 内容寻址缓存

<!-- Status: 设计提案（未实施）。本文取代 analysis-report-dashboard-redesign 与
     analysis-report-dashboard-redesign-filestructure 两份草案，是这条线唯一的当前状态文档。 -->

---

## 0. 摘要与核心裁决

### 0.1 要解决的问题

`vmr analyze`（含过渡别名 `vmr report` / `vmr story`）把审计日志转成机读 JSON 与人读 Markdown。
当前实现有三个结构性矛盾：

1. **渲染逻辑焊死在 Go 编译单元里**。章节排版靠 `strings.Builder` + `fmt.Fprintf` 逐格拼接，
   分布在 `internal/report/section_*.go`、`internal/story/render_*.go`、`internal/reqdetail`
   与 `internal/i18n` 四处，量级在万行以上——改一次列序、加一个主题、调一处版面都要重新编译二进制。
2. **数据层不是完全自洽的单一真源**。一部分事实（置信度标记、成本覆盖披露、脚注释义、首屏亮点句）
   只在渲染期现算，从不落进 JSON。任何"只换模板不重算"的想法都因此不成立。
3. **单体大 JSON 缺乏消费弹性**。`Report2` 把 21 个互不相干的顶级字段压进一个文件；
   关心成本、关心可用性、关心 Prompt 效率的三类消费者被迫处理彼此无关的字段，
   前端也无法按 Tab 渐进加载。

### 0.2 四条支柱

| 支柱 | 一句话 |
|---|---|
| **领域切片数据核心** | 打散单体 JSON 为按业务关注点内聚的切片，补齐渲染期现算的事实，使 JSON 成为无信息损失的单一真源。 |
| **ViewModel 解耦渲染** | 在领域数据与文本产物之间插入一层扁平、排版就绪、已本地化的视图模型；Markdown 与 HTML 各自从它出发。 |
| **HTML 看板运行时加载** | `analyze` 只写 JSON；看板是常驻静态骨架，浏览器侧 `fetch` 切片渲染。生成 JSON 即等于交付看板。 |
| **内容寻址缓存** | 复用 `ctxgraph` 已有的内容哈希底座，把缓存从"解析层"补到"产物层"，实现日志未变不重算、数据未变不重绘。 |

### 0.3 关键裁决（与前两份草案的分歧点，此处为准）

前两份草案在若干处互相矛盾或与代码现状不符。本文逐条裁决，理由见对应章节：

| # | 议题 | 裁决 | 章节 |
|---|---|---|---|
| D1 | 切片拆分的动机 | **为消费弹性拆，不为缓存拆**。缓存粒度保持"整套产物一个指纹"。 | §3.1 / §7.3 |
| D2 | `vmr-report.json` 是否保留 | **保留一个版本周期**，作为切片的合并视图，带弃用标记。 | §8.1 |
| D3 | 模板存放形态 | **`go:embed` 内置为唯一默认**；`-templates <dir>` 是显式逃生口，不是常规路径。 | §5.3 |
| D4 | 模板里的文案与语言 | **全部文案（含章节标题）进 ViewModel**；模板只表达结构与顺序。 | §5.2 |
| D5 | `-render-only` 覆盖面 | **只覆盖宏观报告、请求索引、Journey/Benchmarks 报表**；`details/`、`evidence/` 明确排除。 | §5.4 |
| D6 | 自包含 HTML | **必须保留**。`file://` 下 `fetch` 会被同源策略拦死，纯外置模板是功能倒退。 | §6.4 |
| D7 | 按客户端拆分的 Markdown | **默认不生成 + `-split-by-client` 显式开启**，不是删除能力。 | §3.5 |
| D8 | 指纹算法 | **有序链式 sha256**。禁止 XOR 聚合、禁止 MD5、禁止 mtime 快判。 | §7.2 |
| D9 | `/reports/` HTTP 托管 | **默认关闭**；`api_keys` 未配置时硬性拒绝服务，不是"降级为不鉴权"。 | §6.5 |
| D10 | 语言与 `-render-only` | JSON 带语言（既有 P8 政策），`-render-only` **继承 JSON 的语言**；换语言必须全量重跑。 | §5.5 |

### 0.4 明确不做

- 不引入前端构建链（npm / bundler / 框架）。看板是手写 HTML + 内联 CSS/JS。
- 不引入 CommonMark 解析器、模板 DSL 或规则引擎。
- 不把 `internal/report` 与 `internal/story` 合并，不引入跨包共享的"通用胖 ViewModel"。
- 不动路由半区的任何热路径。分析半区依旧只读审计日志，不上请求路径。

---

## 1. 现状基线

在提任何目标之前，先把基线钉准——前两份草案的多处结论建立在过时或错误的基线上。

### 1.1 产出物全景（按数据层级）

```
[L1 事实层] 原始、不可变、以请求坐标为序
  ├── vmr-requests.json                     全量请求明细行（RequestRow[]）
  ├── vmr-requests-failed.jsonl             异常/截断请求机读流
  ├── details/<coord>.md                    单请求深钻详单（懒物化）
  └── evidence/sysprompt-<h8>.md            系统提示词证据块（内容寻址去重）

[L2 宏观聚合层] 全量横扫后的统计
  ├── vmr-report.json                       21 个顶级字段的单体聚合（format=10）
  ├── vmr-report.md                         §0–§8 人读报告
  ├── vmr-requests.md                       请求全局索引
  ├── vmr-requests-<tag>.md                 按客户端切分的索引
  ├── vmr-requests-cron-<class>.md          定时/心跳类索引
  ├── vmr-requests-failed.md                失败排障入口
  └── tool-waste.html                       工具形态浪费卡（自包含单文件）

[L3 任务叙事层] 纵向还原单任务生命周期
  ├── stories/vmr-stories.json / .md        Journey 候选集索引与 Lineage 拓扑
  ├── stories/journey-<id>.json             JourneySummary（含 Structure / Findings / Cost）
  ├── stories/journey-<id>.md               决策脊柱 + 事件流
  ├── stories/journey-<id>.html             单任务自包含看板
  └── stories/compare-*.{json,md,html}      双任务 A/B 对照

[L4 群体基准层] 跨任务统计
  └── stories/vmr-story-corpus.json / .md   分布、缺陷检出率、Spearman 秩相关、分组对比

[L5 内部缓存层] 可再生，非交付物
  ├── .parse-cache/<filehash>.json          消息解析 + manifest + report 事实分片
  └── stories/.llm-cache/<key>.json         LLM 语义解读缓存
```

**基线修正一：`evidence/` 是被两份草案完全遗漏的一整类产出物。**
它由 `internal/reqdetail` 的 `EnsureSysPromptEvidence` 写出，与 `details/` 同级，
被 `internal/report` 的详单渲染和 `internal/story` 的脊柱渲染共同引用。
它按系统提示词内容哈希去重，是全套产物里唯一天然内容寻址的人读资产。
任何目录拓扑设计必须给它位置，否则第一次实施就会撞墙。

### 1.2 展现层的真实耦合形态

草案给出的"7,087 + 4,749 = 11,836 行"是错的（`internal/i18n` 实测约 4.5k 行 / 30 个非测试文件），
且这类数字随每次重构漂移，按项目文档规约本就不该写进设计文档当作结构性指标。
真正有意义的是**耦合形态**，与行数无关：

1. **表格逐格拼接**。`newTable` / `t.row(cells...)` 之上是一层层 `xxxCell()` 函数，
   列宽、列序、修饰符全在字符流里定死。
2. **业务判断混进字符流**。"样本量 < 20 追加 `⚠️low-n`"、"覆盖率 < 90% 挂脚注 `¹`"、
   "工具参数按深度截断"——这些是数据事实或表现策略，现在都以"拼字符串时顺手判断"的形态存在。
3. **计算与渲染不可分割**。没有任何入口能表达"读现有 JSON、套新模板、重出 Markdown"。
4. **i18n 与章节一一配对**。`internal/i18n/report_*.go` 与 `internal/report/section_*.go`
   逐文件配对，由 `archtest` 强制。这是有意为之的纪律（见 `KNOWN_ISSUES` 里"i18n 微文件不合并"一条），
   重构时必须重新表述而不是绕开。

### 1.3 已有的缓存资产（草案的最大事实错误）

草案称当前"缺乏产物级缓存"。实际上仓内已有两层成熟缓存：

- `report.BuildCached` + `ctxgraph.FileCache` + `internal/report/factscache.go`：
  按**文件内容 sha256** 为 key 缓存消息解析、manifest 与 report 侧的逐记录聚合事实。
  这一层覆盖的正是最贵的开销——解压、JSONL 解析、消息哈希、建图。
- `stories/.llm-cache/`：LLM 推断结果缓存。

**真实缺口只有一层**：从"内存里的聚合结果"到"磁盘上的 Markdown/JSON 产物"之间，没有任何复用。
把已有的说成没有，会让实施阶段重复造轮子，也会让"缓存收益"被系统性高估——
详见 §7.3 对切片级缓存 ROI 的重估。

### 1.4 必须遵守的既有约束

| 约束 | 出处 | 对本方案的影响 |
|---|---|---|
| 两半区一条契约 | CLAUDE.md，`archtest` 强制 | 分析半区不得 import `router`/`server`/`config`；`/reports/` 托管只能是 `server` 单向读目录，不得反向依赖 |
| 详单在 report 与 story 之间字节一致 | `KNOWN_ISSUES` | 渲染指纹必须携带 `(record, manifest, prev)` 身份；引入模板后还要加模板指纹 |
| 产物权限 0600 / 目录 0700 | CLAUDE.md | 新增子目录、模板目录、缓存目录一律照此 |
| 单一时区显示权威 | CLAUDE.md | ViewModel 里所有时间串必须经 `fmtutil.DisplayZone` 生成，前端不做二次时区换算 |
| 原子写用 CreateTemp+Rename，不做目录 fsync | `KNOWN_ISSUES` | 多切片写入必须定义提交顺序，见 §3.4 |
| `vmr-report.json` / `journey-*.json` 已是对外契约 | `KNOWN_ISSUES`（探索性指标优先用外部脚本消费这两个契约） | 破坏性重命名必须带兼容期，见 §8 |
| 分析半区的瓶颈是内存不是磁盘 | `KNOWN_ISSUES`（万级记录 GB 级 RSS） | 切片化应顺手改善内存，不该拿"写盘开销"当卖点 |
| 严格 YAML（`KnownFields`） | CLAUDE.md | 新配置键必须同步 `config.example.yaml` 与 `.zh` 版 |

---

## 2. 概念模型归一

### 2.1 终结 `Story`，统一为 `Journey`

历史演进留下了命名双轨：命令叫 `vmr story`、目录叫 `stories/`、索引叫 `vmr-stories.json`，
但内存模型、单体文件与对比文件全叫 `journey` / `compare`。系统里真正的业务对象只有两个：

- **Request**：一次 HTTP 调用的原子事实（微观点）。
- **Journey**：一次用户目标驱动、跨多轮对话/工具调用/上下文压缩的连续任务旅程（中观链）。

`Story` 不指代任何第三种东西，全面废弃：目录改 `journeys/`，索引改 `journeys/index.{json,md}`。

### 2.2 `corpus` → `Journey Benchmarks`

`internal/story/corpus.go` 算的不是求和，是一套**非参数统计**的群体行为分析：

1. 十余项行为指标的全语料分布（Mean / Median / Min / Max / P90）；
2. 缺陷模式检出率（如 `exact_repeat_tool_call` 原地打转、计划偏离在多少比例的任务中出现）；
3. Spearman 秩相关，跨任务寻找指标间的线索；
4. 缺陷效应量分组对比（命中组 vs 未命中组的中位数差异）。

**它在单次排障里毫无用处**——查昨天某个任务为什么失败，直接读那条 journey 即可。
**它在 Agent 评测里不可替代**：改系统提示词、改工具描述、换主模型之后跑几十上百个标准任务，
这是全系统唯一能回答"整体是变好还是变差"的量化依据。

价值成立，命名不成立。`corpus` 是学术黑话，收敛为 **Journey Benchmarks**：
文件 `journeys/benchmarks.{json,md}`，CLI 主名 `-benchmark`，保留 `-corpus` 别名。

---

## 3. 数据层：领域切片与 Schema 演进

### 3.1 为什么拆切片——以及为什么不是为了缓存

拆分成立的理由只有两条：

1. **关注点分离**。成本核算、SLA 可用性、上下文效率是三类互不重叠的消费场景，
   外部脚本消费某一类时不应被迫解析另外两类。
2. **前端渐进加载**。看板按 Tab 异步取切片，首屏不必下载全量。

草案给出的第三条理由——"改价只失效 finance 切片"——**不成立**，理由见 §7.3。
把缓存动机剥离出去很重要：它决定了切片边界按"消费者关注点"划，而不是按"失效源"划。

### 3.2 切片划分

```
manifest.json                   全局元数据 + 切片清单 + 一致性指纹
macro/summary.json              总流量、成功率、总支出、全局 Findings、首屏亮点
macro/finance.json              按模型/客户端/端点的成本、Provider 与配额对照、定价覆盖披露
macro/reliability.json          端点可用率、错误分类、Failover、延迟分位、Sticky 效果
macro/workloads.json            按日/按小时/时段分布、Workload 归属、客户端-端点矩阵
macro/context-efficiency.json   会话膨胀、Compaction 损失、工具 Schema 浪费
```

原 `Report2` 字段到切片的映射：

| 切片 | 承载的原字段 |
|---|---|
| `manifest.json` | `Meta` 全体、`Pricing` 元数据、输入文件哈希集、各切片指纹 |
| `macro/summary.json` | `Overall`、`Efficiency`、渲染期现算的 `highlights` |
| `macro/finance.json` | `ByModel`、`ByClient`、`Providers`、`ProviderQuotas`、`ProviderQuotaSkipped*`、成本覆盖披露 |
| `macro/reliability.json` | `Endpoints`、`EndpointsAll`、`Sticky` |
| `macro/workloads.json` | `ByDate`、`Hours`、`HoursOfDay`、`Workloads`、`ClientEndpoints` |
| `macro/context-efficiency.json` | `Sessions`、`Compactions`、`Tools` |

**切片间禁止交叉引用具体数值。** `summary.json` 里的"总支出"是独立计算并落盘的标量，
不是"去 finance 里查"。切片是快照，不是关系型视图——一旦允许交叉引用，
就必须保证跨切片事务一致性，而那正是 §7.3 要消除的复杂度。

### 3.3 补齐渲染期现算的事实

要让 JSON 成为无损真源，以下现在只存在于渲染代码里的事实必须下沉。
草案列了前三条，后三条是审查中补上的：

1. **统计置信度**。`cacheEffCell` 在覆盖率 < 0.9 时挂 `¹`、`ppCell` 在 n < 20 时挂 `⚠️low-n`——
   改为在对应 Row 上显式暴露 `tokens_coverage_pct`、`dur_low_n`，由数据侧确定性填充，
   渲染侧只按值决定样式。
2. **成本覆盖披露**。未定价端点、降级估算端点、分量不全费率的汇总与免责说明，
   改为结构化的 `CostCoverage{ unpriced_count, incomplete_rate_count, degraded_estimate_pct }`。
3. **脚注与免责注册表**。`meta.footnotes: map[marker]text` 与 `meta.disclaimers: []text`，
   让下游消费者拿得到每个标记的权威释义，而不是各自猜。
4. **首屏亮点句**。`render_doc.go` 的 `highlights()` 现在是渲染期现算。
   它必须进 `macro/summary.json`——否则 `-render-only` 拿不到它。
   进 JSON 意味着它带语言，这符合既有的 lang-follows-everywhere 政策（§5.5）。
5. **会话/任务标题映射**。请求索引的分组、会话标题、别名、任务标题来自 `SessionAnalysis`，
   而 `SessionAnalysis` **当前完全没有被序列化**。这是 §5.4 里 `-render-only` 覆盖面
   受限的直接原因，必须显式补一份精简投影（只含索引渲染需要的映射，不是整个分析结果）。
6. **跨产物链接**。`journeyLink` 这类"请求 → journey"的交叉链接现在由 cmd 层临时组装，
   没有落进任何产物。索引与看板都需要它，应作为 `requests/index.json` 的一个字段。

### 3.4 产物提交顺序与原子性

多切片方案引入了单体方案没有的新失败模式：写到一半崩溃，留下版本不匹配的切片集。
仓内已有 CreateTemp+Rename 惯例，但需要额外一条纪律：

> **`manifest.json` 最后写。** 所有读取方（前端、`-render-only`、外部脚本）
> 以 manifest 为准入：manifest 不存在或其记录的切片指纹与实际文件不符，
> 整套产物视为无效，而不是逐切片降级采纳。

这与 `vmr-quota.json` "结构损坏整文件拒绝，绝不部分采纳"是同一条原则。

### 3.5 产物精简决策

| 原产出物 | 决策 | 理由 |
|---|---|---|
| `vmr-requests-<tag>.md`、`vmr-requests-cron-*.md` | **默认关闭 + `-split-by-client` 开启** | 实测单次运行会刷出数百 KB 级的多份大文件，默认生成是磁盘噪声。但草案的"彻底删除"过激：这些文件的价值恰恰是 `grep`，改用"开浏览器下拉筛选"是把 CLI 场景换成 Web 场景，不是等价替代。 |
| `stories/` 命名空间 | **改名 + 下沉** | → `journeys/`，单任务实例进 `journeys/details/`，对比提至根级 `compares/`，避免海量 `j-<id>` 淹没索引 |
| `stories/vmr-story-corpus.*` | **重定位 + 更名** | → `journeys/benchmarks.{json,md}` |
| `tool-waste.html` | **拆数据 + 保留自包含导出** | 数据源下沉为 `macro/context-efficiency.json`；HTML 有两种形态，见 §6.4 |
| `vmr-report.json` | **解构 + 兼容期保留** | 见 §3.2 与 §8.1 |
| `details/`、`evidence/` | **下沉对称** | → `requests/details/`、`requests/evidence/`，与 `journeys/details/` 同构；维持懒物化 |
| `.parse-cache/`、`stories/.llm-cache/` | **归一** | → `.cache/parse/`、`.cache/llm/`，消除散落隐藏目录 |

---

## 4. 目标目录拓扑

```
reports/
├── manifest.json                     # 全局元数据 + 切片清单 + 指纹（最后写，读取方准入）
├── vmr-report.json                   # 兼容视图：五切片的合并（弃用中，见 §8.1）
├── vmr-report.md                     # 由 ViewModel 渲染的宏观报告
├── macro/
│   ├── summary.json
│   ├── finance.json
│   ├── reliability.json
│   ├── workloads.json
│   └── context-efficiency.json
├── requests/
│   ├── index.json                    # RequestRow[] + 会话映射投影 + journey 交叉链接
│   ├── index.md                      # 全局索引（默认唯一的人读索引）
│   ├── failed.jsonl
│   ├── failed.md
│   ├── by-client/<tag>.md            # 仅 -split-by-client 时生成
│   ├── details/<coord>.md            # 懒物化
│   └── evidence/sysprompt-<h8>.md    # 懒物化，内容寻址去重
├── journeys/
│   ├── index.json / index.md
│   ├── benchmarks.json / benchmarks.md
│   └── details/j-<id>.{json,md}
├── compares/
│   ├── index.json / index.md
│   └── compare-<a>-vs-<b>.{json,md}
├── html/                             # 仅 -html-inline 时生成的自包含单文件（§6.4）
└── .cache/
    ├── parse/<filehash>.json
    └── llm/<key>.json
```

目录 0700、文件 0600，`html/` 下的自包含导出同样 0600——它内联了对话正文。

---

## 5. Markdown 渲染重构：ViewModel 分层

### 5.1 为什么不能直接上 `text/template`

把现有 Markdown 逻辑原样搬进 Go 模板会造出更糟的东西：

- **表达力不足**。决策脊柱的参数智能截断、多阶标签计算在模板语言里会比 Go 更晦涩，且几乎无法调试。
- **排版脆弱**。Markdown 表格对 `|` 和换行极度敏感，模板的空格/缩进控制极易破格。
- **丢失编译期检查**。字段名拼错在 Go 里是编译错误，在模板里是运行时错误或静默空串。

### 5.2 分层结构

```
[ 领域切片 JSON ]  macro/*.json, journeys/details/j-*.json, requests/index.json
        │
        ▼  纯 Go 函数：类型安全、已本地化、可单测
[ ViewModel ]  扁平、无歧义、排版就绪
        │
        ├──────────────────────┬──────────────────────┐
        ▼                      ▼                      ▼
[ 内置 Markdown 渲染器 ]  [ 外置模板（可选） ]   [ HTML 看板的 JSON 投影 ]
        │                      │
        └──────────┬───────────┘
                   ▼
              [ *.md 产物 ]
```

ViewModel 吸收全部业务格式化、本地化查表与排版决策，产出只由字符串、表格对象、显隐开关构成的平坦结构：

```go
type TableVM struct {
    Title   string
    Headers []string
    Aligns  []string   // "left" | "right" | "center"
    Rows    [][]string // 已转义、已排版（百分比、货币、Token 单位）
    Notes   []string   // 该表专属的脚注/免责，已按本次数据命中情况筛过
}

type SectionVM struct {
    ID     string   // 稳定标识，用于锚点与模板选择，与语言无关
    Title  string   // 已本地化
    Intro  []string
    Tables []TableVM
}

type MacroReportVM struct {
    Title       string
    GeneratedAt string
    TimeRange   string
    Highlights  []string
    Sections    []SectionVM
    Disclaimers []string
    Footnotes   []FootnoteVM
}
```

**裁决 D4：全部文案（含章节标题）进 ViewModel，模板只表达结构与顺序。**

草案给的模板样例里直接写死了中文标题（`## 核心亮点`、`## 1. 成本与 Token 经济`），
而 vmr 的实际用法是同一份数据渲染 en / zh 两套输出。三条出路各有代价：

| 方案 | 代价 |
|---|---|
| 每语言一套模板 | 翻译漂移；加一个章节要改 N 份模板 |
| 模板内 `{{ t "cost.title" }}` | 在模板里重建 i18n 查表，把编译期类型安全换成运行时 key 拼写错误 |
| **标题也进 ViewModel（选定）** | 模板退化为纯结构骨架，"自由排版"的空间变小 |

选第三条：语言正确性是硬要求，模板的表达自由度不是。
模板于是长这样——没有一个字面文案，因此天然双语：

```
{{ .Title }}
{{ .GeneratedAt }} | {{ .TimeRange }}

{{ range .Highlights }}- {{ . }}
{{ end }}
{{ range .Sections }}
## {{ .Title }}
{{ range .Tables }}{{ renderTable . }}
{{ end }}{{ end }}
{{ range .Disclaimers }}> {{ . }}
{{ end }}
```

配套地，`internal/i18n/report_*.go` 与 `internal/report/section_*.go` 的一一配对纪律
改为与 **ViewModel 构建器**配对（`section_cost.go` → `viewmodel_cost.go` → `i18n/report_cost.go`），
`archtest` 的 i18n 检查随之更新——纪律保留，配对对象换一个。

### 5.3 模板的存放形态

**裁决 D3：`go:embed` 内置为唯一默认，`-templates <dir>` 是显式逃生口。**

CLAUDE.md 第一句就是 "Local-run, single-binary"。把 `templates/` 变成必需的磁盘目录
会引入一整套新的部署问题：目录找不到怎么办、模板版本与二进制不匹配怎么办、权限给多少。
`internal/story/assets/{style.css,dashboard.js}` 已经用 `go:embed` 解决过同类问题，路子是验证过的。

使用外置模板时：

- 产物 `meta` 里记录模板目录路径与**模板内容指纹**，否则 golden 测试与
  "详单在 report / story 之间字节一致"这条不变量都失去判据；
- 外置模板产出的 Markdown 不参与 golden 比对，只做"能渲染、不 panic"的 smoke；
- 缺失的模板逐个回落到内置版本，不是整体失败——模板是排版，不是数据。

### 5.4 `-render-only`：覆盖面必须诚实

```bash
vmr analyze -render-only [-o reports]
```

检测到 `manifest.json` 与各切片存在且指纹自洽，跳过全部日志解压、解析、哈希与建图，
只做 `JSON 读取 → ViewModel 构建 → 模板渲染 → 写盘`。

**裁决 D5：覆盖面严格限定，草案宣称的"全量重绘"做不到。** 代码层面的硬约束：

| 产物 | 能否 render-only | 阻塞点 |
|---|---|---|
| `vmr-report.md` | ✅ | 补齐 §3.3 的六项缺口后成立 |
| `requests/index.md` | ✅（需先补 §3.3 第 5 项） | `WriteRequestsIndex` 当前需要 `*SessionAnalysis`，而它从未被序列化 |
| `journeys/index.md`、`benchmarks.md` | ✅ | 输入本就是已落盘的聚合结构 |
| `journeys/details/j-<id>.md` | ⚠️ 需新建管线 | `RenderMarkdown` 的输入是内存 `*Journey`，不是 `JourneySummary`。`JourneyStructure` 有无损重建测试，但**没有 Structure → 渲染**的代码路径。要支持，等于把 `render_spine` 整条重写到 Structure 之上——这是 Phase 3 里被草案严重低估的一块。 |
| `requests/details/*.md`、`evidence/*.md` | ❌ 永远不行 | `reqdetail.Render` 的输入是原始 `audit.Record` + manifest。这些字节从不进 JSON（也不该进——那等于把全部对话正文复制一份）。 |

对最后一行不必遗憾：详单本就是懒物化的，默认套件根本不生成它们
（`KNOWN_ISSUES` 里"默认分析套件不物化 `details/`"一条已把这条纪律用测试锁死）。
`-render-only` 的正确语义是**"重绘聚合类文本产物"**，详单靠 `vmr replay -print -req <coord>` 按需取。

耗时不写死数字：这一路径的开销只有 JSON 反序列化 + 字符串拼接 + 写盘，
与日志规模解耦，与产物规模线性相关。实测值在实施后填进 CHANGELOG，不写进设计文档。

### 5.5 语言与 render-only 的关系

**裁决 D10。** 既有的 lang-follows-everywhere 政策让 JSON 里的人读文本（Finding 叙述、
Action 建议、亮点句）跟随 `-lang` 落盘，只有 Finding 的 `Code` 是跨语言稳定标识。
因此 **`-render-only` 只能渲染出与 JSON 同语言的 Markdown**。

草案把"切换中英文输出"列为 `-render-only` 的应用场景，这是错的。
要支持跨语言重绘，必须把全部人读文本改为渲染侧从 `Code` + 结构化参数派生——
那是对既有语言政策的推翻，影响 JSON 契约的所有消费者，应作为独立议题单独评审，不塞进本次重构。

---

## 6. HTML 看板

### 6.1 工作流：生成即交付

```
vmr analyze
   └─► 只写 JSON 切片（无任何 HTML 渲染开销）

浏览器
   └─► GET .../macro-dashboard.html      纯静态骨架，零业务数据
        └─► Promise.all([fetch('macro/summary.json'), fetch('macro/finance.json')])
             └─► 客户端渲染 DOM / 内联 SVG / 主题切换
```

### 6.2 看板清单

| 页面 | 消费的切片 |
|---|---|
| `macro-dashboard.html` | `macro/*.json` |
| `request-browser.html` | `requests/index.json`（客户端筛选、链向 `requests/details/`） |
| `journey-viewer.html` | `journeys/details/j-<id>.json` |
| `journey-compare.html` | `compares/compare-*.json` |
| `benchmarks.html` | `journeys/benchmarks.json` |
| `tool-waste.html` | `macro/context-efficiency.json` |

数据源解析协议：URL 查询参数 `?data=<相对路径>` 显式指定；缺省时按约定探测
（`journey-viewer.html` 无参数则读同目录 `index.json` 并列出候选）。
`?data=` 的值必须做与 §6.5 相同的路径校验——它最终会变成一次服务端文件读取。

**零编译扩展**：切片是稳定契约，看板只是它的一个消费者。
需要一块专属大屏（例如只看成本的财务视图）时，复制一个骨架页、改 JS、
指向同一份 `macro/finance.json` 即可，不触碰任何 Go 源码、不需要重新构建二进制。
这正是切片拆分在"关注点分离"之外的第二重回报。

### 6.3 前端工程化边界

草案对图表只字未提，但宏观看板要画的东西（延迟分位、按小时热力、Spearman 散点、
成本堆叠）不是表格能替代的。两条路必须现在就选，否则工作量估算无意义：

- **选定：零依赖内联 SVG**。手写坐标轴 + path，够用于折线/柱状/热力/散点四种形态。
  `internal/story/render_html_dashboard.go` 现有的图表实现是基线，直接复用其视觉系统
  （"VMR Forensics"：暗色飞行数据记录仪 / 亮色工程方格纸）。
- 否决：引入图表库。即使 `go:embed` 进二进制，也会带来体积、升级与 CSP 三重负担，
  换来的只是省几百行手写 SVG。

主题通过 CSS 变量注入，暗/亮双色 + 系统跟随，与 `/status.html`、`/log.html` 保持同一套视觉。

### 6.4 自包含 HTML 必须保留

**裁决 D6。** `internal/story/render_html_assets.go` 的包注释明确写着设计意图：
"an artifact you can open from a file:// URL or hand to someone with no network"。

把 `journey-<id>.html` 和 `tool-waste.html` 改成"骨架 + fetch JSON"会直接摧毁这个能力：
浏览器对 `file://` 源的 `fetch` 默认被同源策略拒绝，页面只能在 HTTP 服务下访问。
这是本方案里唯一会被用户直接感知到的功能倒退，不接受。

**两种形态并存，同一份骨架代码：**

| 形态 | 触发 | 数据来源 | 用途 |
|---|---|---|---|
| 运行时加载 | 默认 | `fetch` 切片 | 日常浏览、多任务切换、局域网分享 |
| 内联自包含 | `-html-inline` | 构建期把 JSON 内联进 `<script type="application/json">` | `file://` 直开、转发给同事、附进工单 |

骨架代码只需在启动时判断：内联数据块存在就用它，否则走 `fetch`。零分叉成本。

### 6.5 `/reports/` HTTP 托管与安全模型

**裁决 D9：默认关闭，且 `api_keys` 未配置时硬性拒绝。**

草案说"JSON 强制通过 `s.auth()` 保护"。但 `internal/server` 的实际实现是：
**未配置 `api_keys` 时鉴权直接放行**（这对聊天协议端点是合理的本机默认，
对承载全部对话正文的报表目录不是）。照搬这个模型，等于在
`listen` 被改成非回环地址且没配 key 的常见场景下，把整个语料裸奔在局域网上。

必须同时满足的四条：

1. **显式开关**。新增配置项（如 `analytics.serve: false` 为默认），不开不挂路由。
   同步更新 `config.example.yaml` 与 `config.example.zh.yaml`（严格 YAML + 双语示例是硬规则）。
2. **无 key 即拒绝**。`/reports/*` 的处理器在 `len(APIKeys) == 0` 时一律 403 并在
   启动日志里说明原因——不复用会放行的通用 `auth` 包装器。
3. **路径校验**。`filepath.Clean` + 输出目录前缀校验 + 拒绝符号链接 + **禁用目录列表**
   （`requests/details/` 单目录可达数千文件，列表响应本身就是一次拒绝服务）。
4. **分层鉴权，对齐 `/status.html`**。HTML 骨架不含业务数据，免鉴权直出；
   所有 `.json` / `.jsonl` / `.md` 数据请求走鉴权。前端从 `localStorage` 取 key
   （复用 `/status.html` 已有的 key 存储），不进 URL、不进服务端日志。

依赖方向：`internal/server` 只是按路径读文件目录，**不 import 任何分析半区的包**，
两半区契约不破。

---

## 7. 缓存层

### 7.0 为什么缓存键不能是"标题 + 起止时间 + 请求数"

这是最直觉的方案，也是最危险的：它在正常场景下一直正确，只在少数场景下静默给出错误答案。
Agent 语料里这些场景并不罕见：

1. **Compaction 与截断的非单调性**。历史压缩会让会话步数变化，或在同一时间窗内分叉重试——
   请求数与起止时间可以完全一致，而正文与调用轨迹已彻底改变。
2. **Failover 的同构隐匿**。同一条 Journey 的请求总数与时间不变，
   但上游冷却让某次尝试从一个端点切到了另一个，成本与延迟剖面剧变。
3. **配置变更不改日志**。用户改了 `config.yaml` 的费率覆盖或汇率，审计记录一字未动，
   但报表里的每一个金额都变了。

结论：缓存判据必须是**内容**，不是**元数据**。这正是 `ctxgraph` 内容寻址底座存在的意义，
本方案完整复用它，不另起一套。

### 7.1 三级模型

```
L1 输入解析缓存（已存在，只做归位）
   .cache/parse/<filehash>.json  依赖：审计文件内容 sha256 + 解析器版本
   .cache/llm/<key>.json         依赖：模型 + Prompt + 输入指纹
   价值：跳过解压、JSONL 解析、消息哈希、建图、LLM 重复推断 —— 全流程最贵的一段

L2 产物数据缓存（新增，本次重构的真实缺口）
   reports/**/*.json             依赖：Digest(输入哈希集 ‖ 配置指纹 ‖ 格式版本 ‖ 分析参数)
   价值：整套产物有效则跳过聚合与叙事构建

L3 表现层缓存（新增）
   reports/**/*.md               依赖：Digest(ViewModel 指纹 ‖ 模板指纹 ‖ 语言)
   价值：数据或模板任一未变则跳过渲染与写盘
```

### 7.2 指纹算法规范

**裁决 D8。** 草案的算法有两处硬错误，必须改正：

1. **禁止 XOR 聚合**。草案写 `MD5(⊕_{s∈Steps} ManifestHash(s) ‖ ...)`。
   XOR 对顺序不敏感（步骤重排得到同一指纹），且 `a⊕a = 0`——同一 manifest 出现两次会相互抵消，
   而 Compaction 后的历史重放恰恰会产生重复 manifest。这是会静默命中错误缓存的算法。
   改为**有序链式 sha256**：`h ← sha256(h ‖ uvarint(len(hᵢ)) ‖ hᵢ)`，逐步骤按序推进。
2. **禁止 MD5**。仓内内容寻址底座统一是 sha256（`ctxgraph.HashFile`），不引入第二种摘要。
3. **禁止 `Size + MTime` 快判**。草案把它当 fast path，但 `.parse-cache` 恰恰在 2026-09
   刚从 mtime 消歧改为按内容哈希为 key，理由已记录在 `KNOWN_ISSUES`。
   审计日志是**追加写的活文件**，同秒内的追加对 mtime 完全隐形。
   直接 sha256——实测缓存体积远小于日志体积，成本已被验证可接受。

各产物的身份与验证指纹：

| 产物 | 身份 Key | 验证指纹 |
|---|---|---|
| 请求详单 | `ReqCoord`（`CanonicalPath:Line`） | `Digest(记录原始字节 ‖ manifest 身份 ‖ prev 身份 ‖ 模板指纹 ‖ 语言)` |
| 系统提示词证据 | 提示词内容哈希 | 身份即内容，无需二次验证 |
| 单 Journey | `Journey.ID`（已含根哈希前缀） | 步骤 manifest 的有序链式 sha256 ‖ 定价指纹 ‖ 格式版本 |
| 整套产物 | 输出目录 | 见 §7.3 |

详单指纹必须携带 `manifest` / `prev` 身份而不只是语言——
这是 `KNOWN_ISSUES` 里"详情页在 report 与 story 之间字节一致"那条不变量的直接要求，
只做一半会重现"同名文件先写者赢"的旧 bug。

### 7.3 缓存粒度：整套一个指纹，不做切片级隔离

**裁决 D1（后半）。** 草案的核心卖点是"改价只失效 `finance.json`，可靠性与负载切片 100% 命中"。
三条理由否决它：

1. **收益接近零**。改价之后，昂贵的那一段（解压 + 解析 + 哈希 + 建图）已经被 L1 覆盖，
   剩下的只是内存里重跑一遍分桶累加。切片级隔离省下的是这一段的一部分——毫秒级。
2. **隔离并不干净**。`summary.json` 里有总支出、`context-efficiency.json` 的 Finding 也可能带成本口径。
   定价变更实际会牵连多个切片，"只有 finance 失效"是理想化的。
3. **引入一致性缺陷**。允许 finance 用旧缓存、reliability 重算，就允许同一份报表内部
   出现来自不同输入快照的数字，而没有任何机制发现它。这类"看起来对、其实自相矛盾"的产物
   比重算慢几百毫秒危险得多。

**决定：切片拆分保留（为消费弹性），缓存粒度是整套产物一个指纹。**
要么全套有效，要么全套重算。Phase 4 的复杂度因此砍掉一大半。

### 7.4 失效矩阵

| 变化 | L1 | L2 | L3 |
|---|:---:|:---:|:---:|
| 审计日志新增/改动 | 部分失效（按文件） | 失效 | 失效 |
| `config.yaml` 定价/汇率改动 | 命中 | 失效 | 失效 |
| `report.yaml` 改动（自流量排除等） | 命中 | 失效 | 失效 |
| 分析参数改动（时间窗、profile、`-lang`） | 命中 | 失效 | 失效 |
| 模板改动 | 命中 | 命中 | 失效 |
| 格式版本升级 | 按解析器版本判 | 失效 | 失效 |
| 二进制版本变化（无格式变更） | 命中 | 命中 | 命中 |

最后一行是有意的：把二进制版本纳入指纹会让每次构建都全量失效，收益为零。
格式版本与解析器版本才是正确的失效锚点。

---

## 8. 兼容、迁移与回滚

### 8.1 JSON 契约的破坏性变更

`vmr-report.json` 与 `journey-*.json` 已经是**被明确背书的对外契约**——
`KNOWN_ISSUES` 里写着"探索性新分析指标优先用外部脚本消费这两个稳定契约验证"。
草案一次性做了六项破坏性变更（格式版本、单体拆分、目录改名、`corpus` 改名、
详单迁位、按客户端文件删除）却没有一句迁移策略。补上：

| 阶段 | 动作 |
|---|---|
| **N（本次）** | 切片落地；`vmr-report.json` 与 `stories/` 继续生成，内容等价，`meta` 里带 `deprecated: true` 与替代路径；`-corpus` 作为 `-benchmark` 别名；CHANGELOG 记 Breaking Change 与迁移指引 |
| **N+1** | 生成兼容产物时打 stderr 警告 |
| **N+2** | 删除兼容产物与别名 |

`manifest.json` 的 `format` 从 10 递进到 11。切片各自不带独立版本号——
整套产物是一个版本单位，与 §7.3 的整套指纹一致。

### 8.2 `manifest.json` 的职责

草案没定义它是给谁用的，导致字段无从设计。定死为**三个职责，一份文件**：

1. **准入与一致性**：切片清单 + 每个切片的内容指纹（读取方的唯一入口，见 §3.4）。
2. **溯源**：输入文件列表与哈希、生成时间、时区、时间窗、语言、格式版本、实际生效的配置文件路径。
3. **发现**：切片路径 → 语义标签的映射，供前端与外部脚本免硬编码路径地遍历。

它**不承载任何指标数值**。任何"顺手把总请求数也放 manifest 里"的提议一律否决——
那会立刻制造出与 `summary.json` 的双账本。

### 8.3 回滚

每个 Phase 独立可回滚：Phase 1（数据层）落地后旧渲染路径仍然工作；
Phase 2（HTML）纯新增；Phase 3（ViewModel）内置渲染器与旧渲染器可并行一个版本，
用 golden 对比确认字节一致后再删旧路径；Phase 4（缓存）由环境变量或 flag 一键旁路
（`-no-cache`），任何缓存可疑行为都能立刻退回全量重算验证。

---

## 9. 测试与守卫

草案完全没提测试，而这套重构恰好会动到仓内最强的几条守卫。逐条给出去处：

| 现有守卫 | 重构后 |
|---|---|
| `internal/story/golden_test.go` | **下沉到 ViewModel 层**：比对 ViewModel 结构而非最终字符串。结构比对的 diff 可读、对无关排版改动不脆。模板层另留少量端到端 smoke |
| `internal/report/e2e_test.go` | 保留；增加"切片并集 ≡ 兼容 `vmr-report.json`"的等价断言，兼容期内一直有效 |
| 详单在 report / story 之间字节一致 | 保留且加强：指纹增加模板维度（§7.2） |
| `cmd/vmr/i18n_e2e_test.go` | 保留；新增"ViewModel 中不存在任何未经 i18n 的裸字面量"的守卫 |
| `internal/archtest` 行数预算 | 新增 `viewmodel_*.go` 需登记；`section_*.go` 随职责迁移会缩水，预算下调 |
| `internal/archtest` i18n 配对 | 配对对象从 `section_*.go` 改为 ViewModel 构建器（§5.2） |
| `internal/archtest` 导入边界 | 新增断言：`internal/server` 不得 import 分析半区任何包 |
| — | **新增**：`-render-only` 产物与全量运行产物字节一致（这是整个方案的核心正确性主张，必须由测试而非注释保证） |
| — | **新增**：缓存命中路径与冷启动路径产物一致（浮点用容差，沿用现有 `1e-6` 惯例） |

---

## 10. 路线图

工作量按"改动面 + 未知度"给区间，不追求精确。
草案给的 14~19 人天低估了两处：Journey Markdown 的 Structure 化重写（§5.4）和图表实现（§6.3）。

```
Phase 1 — 数据层闭环                                        4~6 人天
  ├── Report2 解构为五切片 + manifest.json（含兼容视图）
  ├── 补齐 §3.3 六项渲染期现算事实（含 SessionAnalysis 投影）
  ├── 目录拓扑归位：journeys/ 、compares/ 、requests/details|evidence 、.cache/
  ├── 产物提交顺序与原子性（manifest 最后写）
  └── -split-by-client 替代默认按客户端刷盘

Phase 2 — HTML 看板                                         5~7 人天
  ├── go:embed 骨架 + 主题变量系统（复用 VMR Forensics 视觉）
  ├── 零依赖内联 SVG 图表基元（折线/柱状/热力/散点）
  ├── 六个看板页 + ?data= 加载协议
  ├── -html-inline 自包含导出（与运行时加载共用骨架）
  └── server 挂载 /reports/*：显式开关、无 key 拒绝、路径校验、禁目录列表

Phase 3 — ViewModel 与模板解耦                              6~9 人天
  ├── report 侧 ViewModel（各 section 改为构建器）
  ├── story 侧 ViewModel —— 含 render_spine 重建到 JourneyStructure 之上（最大不确定项）
  ├── 内置渲染器 + go:embed 模板 + -templates 逃生口
  ├── golden 测试下沉到 ViewModel 层
  └── -render-only（覆盖面按 §5.4 表格）

Phase 4 — 产物级缓存                                        2~3 人天
  ├── 整套产物指纹（有序链式 sha256）与失效矩阵
  ├── L3 表现层指纹（含模板维度）
  └── -no-cache 旁路 + 冷热一致性测试
```

Phase 1 与 Phase 2 之间无强依赖，可并行。Phase 4 因为放弃了切片级隔离而大幅缩小。

**收益判断**（不逐项打分，只说结论）：

- 真正的收益是**可维护性**——Go 专心算数据，排版归模板，样式归 CSS。这是主要动机。
- 次要收益是**看板能力**与**重绘速度**。前者是新增能力，后者受益者是反复调排版的开发者本人。
- 缓存收益被草案高估。最贵的一段早已被 `.parse-cache` 覆盖；本次补的 L2/L3 是补完整性，不是性能突破。
- 代价是**新增了一个抽象层**。ViewModel 引入不当会变成"多一层无意义搬运"。
  判据很简单：如果一个字段在 ViewModel 里只是原样透传且不涉及任何格式化或本地化，它就不该存在。

---

## 11. 不变量与已知取舍

### 11.1 必须守住的不变量

1. **两半区一条契约**。`report` / `story` / `ctxgraph` / `taskseg` / `chatmsg` / `reqdetail`
   不得 import `router` / `server` / `config`；`server` 不得 import 分析半区。
   ViewModel 各自内聚在 `internal/report` 与 `internal/story` 内，
   **不建跨包共享的通用胖对象**——两侧的表格语义并不相同，强行共享会同时污染两边。
2. **内容寻址底座不可动摇**。缓存判据只能是内容哈希，绝不回退为文件时间启发式。
3. **权限底线**。所有产物 0600 / 目录 0700，含自包含 HTML 导出与模板缓存。
4. **单一时区权威**。所有时间串在 ViewModel 构建期经 `fmtutil.DisplayZone` 定稿，
   前端不做二次换算——否则同一份数据在 Markdown 与看板里会显示不同时间。
5. **不新增双账本**。manifest 不放指标，切片间不交叉引用数值，
   看板不在前端重算任何已在 JSON 里的量。

### 11.2 已知取舍（避免下一个评审重提）

| 决定 | 为什么 |
|---|---|
| 不做切片级缓存隔离 | §7.3：收益毫秒级，代价是产物内部一致性缺陷 |
| 不支持跨语言 `-render-only` | §5.5：JSON 按既有政策带语言；要改先改语言政策，不在本次范围 |
| `requests/details/*.md` 永不进 `-render-only` | §5.4：输入是原始审计字节，进 JSON 等于复制一份全语料 |
| 外置模板不参与 golden 比对 | §5.3：用户自定义排版的字节输出不是本仓的正确性承诺 |
| 不引入图表库 | §6.3：体积 + 升级 + CSP 三重负担，换几百行手写 SVG |
| 二进制版本不进缓存指纹 | §7.4：每次构建全量失效，收益为零 |
| 保留自包含 HTML | §6.4：`file://` 下 `fetch` 被同源策略拦死，纯外置模板是功能倒退 |
| 保留而非删除按客户端索引 | §3.5：其价值是 `grep`，Web 下拉筛选不是等价替代 |

### 11.3 顺带机会（不属于本方案，但实施时应留出口子）

`KNOWN_ISSUES` 记录的分析半区真实瓶颈是**内存**（万级记录 GB 级 RSS），
不是草案臆测的"写盘开销"。切片化天然把产物写入拆成了若干段，
如果 Phase 1 的序列化写成流式（逐切片构建、写完即释放），可以顺手削掉一部分峰值。
这不是本方案的目标，但实施时不要把结构写死成"必须全部切片同时驻留内存"。

---

*文档状态：设计提案，待评审*
*跟踪基线：vmr @ main*
