// Ver 2026-09-06 16:31, by Claude (pi)

# vmr analyze 架构重构方案：领域切片数据核心 + ViewModel 双轨渲染 + 内容寻址缓存

<!-- Status: 设计提案（未实施）。本文取代 analysis-report-dashboard-redesign 与
     analysis-report-dashboard-redesign-filestructure 两份草案，是这条线唯一的当前状态文档。 -->

---

## 0. 摘要与核心裁决

### 0.1 要解决的问题

`vmr analyze`（过渡别名 `vmr report` / `vmr story` 随本方案删除，见 §8）把审计日志转成机读 JSON 与人读 Markdown。
当前实现有三个结构性矛盾：

1. **渲染逻辑焊死在 Go 编译单元里**。章节排版靠 `strings.Builder` + `fmt.Fprintf` 逐格拼接，
   分布在 `internal/report/section_*.go`、`internal/story/render_*.go`、`internal/reqdetail`
   与 `internal/i18n` 四处，量级在万行以上——改一次列序、加一个主题、调一处版面都要重新编译二进制。
2. **数据层不是完全自洽的单一真源**。一部分事实（置信度标记、成本覆盖披露、脚注释义、首屏亮点句）
   只在渲染期现算，从不落进 JSON。任何"只换渲染器不重算"的想法都因此不成立。
3. **单体大 JSON 缺乏消费弹性**。`Report2` 把 21 个互不相干的顶级字段压进一个文件；
   关心成本、关心可用性、关心 Prompt 效率的三类消费者被迫处理彼此无关的字段，
   前端也无法按 Tab 渐进加载。

### 0.2 四条支柱

| 支柱 | 一句话 |
|---|---|
| **领域切片数据核心** | 打散单体 JSON 为按业务关注点内聚的切片，补齐渲染期现算的事实，使 JSON 成为无信息损失的单一真源。 |
| **ViewModel 解耦渲染** | 在领域数据与 Markdown 产物之间插入一层扁平、排版就绪、已本地化的视图模型，只驻内存不落盘；HTML 侧直接消费领域切片（§5.6）。 |
| **HTML 看板运行时加载** | `analyze` 只写 JSON；看板是常驻静态骨架，浏览器侧 `fetch` 切片渲染。生成 JSON 即等于交付看板。 |
| **内容寻址缓存** | 复用 `ctxgraph` 已有的内容哈希底座，把缓存从"解析层"补到"产物层"，实现日志未变不重算、数据未变不重绘。 |

### 0.3 关键裁决（与前两份草案的分歧点，此处为准）

前两份草案在若干处互相矛盾或与代码现状不符。本文逐条裁决，理由见对应章节：

| # | 议题 | 裁决 | 章节 |
|---|---|---|---|
| D1 | 切片拆分的动机 | **为消费弹性拆，不为缓存拆**。缓存粒度保持"整套产物一个指纹"。 | §3.1 / §7.3 |
| D2 | `vmr-report.json` 是否保留 | **不保留**。产物可再生，一步解构为切片，无兼容视图、无过渡期。 | §8.1 |
| D3 | Markdown 是否用模板引擎 | **不用**。ViewModel 模式固定，Markdown 是它的确定性序列化器；不引 `text/template`，无 `-templates`。 | §5.3 |
| D4 | 渲染层的文案与语言 | **全部文案（含章节标题）进 ViewModel**；渲染器只表达结构与顺序，天然双语。 | §5.2 |
| D5 | `-render-only` 覆盖面 | **覆盖全部常驻人读产物**（宏观报告、Journey/Benchmarks、失败索引）；懒物化的 `details/`、`evidence/` 永久排除。 | §5.4 |
| D6 | 自包含 HTML（现状唯一形态） | **废弃**。HTML 收敛为骨架 + fetch 一种形态；查看经由 `/reports/` 托管或任意静态服务器。 | §6.4 |
| D7 | 人读的请求索引 | **整族删除**（含按客户端/定时类拆分的兄弟文件）。它没有独占职责：数据留 `requests/index.json`，浏览交给 `request-browser.html`，`failed.md` 单独保留。 | §3.7 |
| D8 | 指纹算法 | 全系统一个 Digest 构造：**长度前缀的有序 sha256 链**，顺序敏感、拼接无歧义、重复不抵消；输入身份直接算内容哈希。`ctxgraph` 既有的 md5 内容寻址底座原样保留，作为分量喂进链里。 | §7.2 |
| D9 | `/reports/` HTTP 托管 | **默认关闭**；`api_keys` 未配置时硬性拒绝服务，不是"降级为不鉴权"。 | §6.5 |
| D10 | 语言与 `-render-only` | JSON 带语言（既有 P8 政策），`-render-only` **继承 JSON 的语言**；换语言必须全量重跑。 | §5.5 |
| D11 | 渲染的唯一输入 | **聚合类产物只从落盘 JSON 渲染**：全量运行 = 先写 JSON 再从 JSON 渲染，与 `-render-only` 是同一条代码路径。详单/证据是另一类产物，显式豁免。 | §5.0 |
| D12 | ViewModel 是否落盘 | **不落盘**。它是 Markdown 专用的内存层；前端直接消费领域切片，格式化漂移由跨语言 fixture 钉死。 | §5.6 |
| D13 | 看板的传参与路径 | **`#data=` hash 传参**，不用 `?query`；磁盘布局与 HTTP 路径逐字一致。 | §6.2 |
| D14 | 数据版本探测 | **banner 进骨架页，版本查 manifest**。页面先取 `manifest.json` 比对 `format`，不一致出横幅、渲染继续；检查随骨架复制继承到自定义面板；切片不带独立版本戳。 | §6.6 |
| D15 | `-redact` | **随自包含 HTML 一并删除**，不迁移到骨架形态。 | §6.4 |
| D16 | 托管目录 | **默认 `./reports`，与 `analyze -o` 同一默认值**；`analytics.serve_dir` 可覆盖。不走 `rundir`。 | §6.5 |
| D17 | 详单文件名 | **加 `r-` 类型前缀**，与 journey 的 `j-` 同一套约定。 | §1.1 |
| D18 | Journey JSON 的完整性 | **补成自包含**：`structure` 是 tree，同文件内加一张按内容哈希去重的 `bodies` blob 表；工具配对从 `matched bool` 升为三级 `match`。 | §3.6 |

### 0.4 明确不做

- 不引入前端构建链（npm / bundler / 框架）。看板是手写 HTML + 内联 CSS/JS。
- 不引入 CommonMark 解析器、任何模板引擎（含 `text/template`）、模板 DSL 或规则引擎。
- 不把 `internal/report` 与 `internal/journey`（原 `internal/story`）合并，不引入跨包共享的"通用胖 ViewModel"。
- 不动路由半区的任何热路径。分析半区依旧只读审计日志，不上请求路径。

---

## 1. 现状基线

在提任何目标之前，先把基线钉准——前两份草案的多处结论建立在过时或错误的基线上。

### 1.1 产出物全景（按数据层级）

```
[L1 事实层] 原始、不可变、以请求坐标为序
  ├── vmr-requests.json                     全量请求明细行（RequestRow[]）
  ├── vmr-requests-failed.jsonl             异常/截断请求机读流
  ├── details/r-<ts>_<virt>_<real>_<outcome>_<h8>.md   单请求深钻详单（懒物化）
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

**术语：请求坐标（coord）与详单文件名不是同一个东西。**
全文出现的"请求坐标 / coord"一律指 `ctxgraph.ReqCoord(path, line)` 的返回值，形如
`vmr-audit-2026-09-01.jsonl:4213`：

- 左半是 `CanonicalPath(path)`——审计文件的 basename 去掉 `.zst` 后缀，
  所以一个日志文件的身份能扛过自身的 `plain → .zst` 轮转，且与调用方传的是绝对路径还是相对路径无关；
- 右半是 `audit.ForEachLine` 的 **1-based 逻辑行号**（不是字节偏移）。

它由 `BuildManifest` 一次算出、存进 `Manifest.Req`，是全系统唯一的请求级身份串：
`vmr-requests.json` 的 `req` 字段、`vmr replay -print -req` 的入参、详单指纹的 Key（§7.2）都是它。

**它不能直接当文件名**——中间那个 `:` 在 Windows 上非法、在 URL 里有语义。
详单文件名是它的一个派生形态（`reqdetail.FileName`），**本方案给它加上 `r-` 类型前缀**：

```
r-{ts}_{virtualModel}_{realModel}_{outcome}_{h8}.md
r-20260901-142233.117_coding_gpt-5-codex_ok_3f9a1c04.md
```

`r-` 与 journey 的 `j-<id>` 同一套约定：**光看一个 ID 或文件名就知道它是哪一类实体**，
不必先看它在哪个目录里。粘一个 ID 到聊天窗口、贴进 issue、混在一份 `grep` 结果里时，
前缀是唯一还在的上下文。

前缀之后的四段是给人扫读的装饰（`ts` 按记录自身的时区偏移渲染，不走 `fmtutil.DisplayZone`，
这样同一条记录在任何机器上生成同名文件——与 journey `deriveID` 同一条既有豁免）；
唯一保证唯一性的仍是末段 `h8 = ctxgraph.ReqHash8(coord)`。

三类内容寻址实体的前缀约定，一处列全：

| 实体 | 形态 | 说明 |
|---|---|---|
| 请求详单 | `r-<ts>_<virt>_<real>_<outcome>_<h8>.md` | 本方案新增 `r-` 前缀 |
| Journey | `j-<id>.{json,md}` | 既有 |
| 对比 | `compare-<a>-vs-<b>.{json,md}` | 既有，本身已自述类型 |
| 系统提示词证据 | `sysprompt-<h8>.md` | 既有，本身已自述类型。这里的 `h8` 是**提示词正文**的内容哈希前 4 字节，与请求坐标无关——两处同名不同源，实施时不要合并 |

**基线修正一：`evidence/` 是被两份草案完全遗漏的一整类产出物。**
它由 `internal/reqdetail` 的 `EnsureSysPromptEvidence` 写出，与 `details/` 同级，
被 `internal/report` 的详单渲染和 `internal/story` 的脊柱渲染共同引用。
它按系统提示词内容哈希去重，是全套产物里唯一天然内容寻址的人读资产。
任何目录拓扑设计必须给它位置，否则第一次实施就会撞墙。

**基线修正二：草案讨论的两个"要删的 flag"并不存在。**
仓内没有 `-html-inline`，也没有 `-split-by-client`：

- HTML **只有自包含一种形态**，由 `internal/story/render_html*.go`、`render_compare_html.go`、
  `internal/report/toolwaste_html.go` 三处 Go 渲染器直接吐出带数据的整页（`go:embed` 的
  CSS/JS 内联进去）。仓内确实有一个 `-html` 开关，但它切的是"要不要出这份 HTML"
  （且只对单匹配 `-journey` / `-compare` 生效），不是"内联还是 fetch"；`tool-waste.html`
  连这个开关都没有，无条件写出。`-html` 还带一个 `-redact` 修饰符（正文换成
  `‹text: N chars›` 占位、去掉详单链接与 Finding 文本），它随自包含 HTML 一并删除（D15，§6.4）。
  因此 §6.4 的裁决不是"删一个可选开关"，而是**废弃现有全部 HTML 渲染器**——
  这是替换，不是新增，Phase 2 的工作量与风险都因此高于草案估计。
- 按客户端/定时类拆分索引是**无条件行为**（`WriteRequestsIndex` 逐组写兄弟文件），没有开关可关。
  它当初被拆出来是为了消除"索引内联一遍、兄弟文件再导出一遍"的重复渲染，不是为了给 `grep` 提供多文件。
  这一点值得说准，因为草案"保住多文件 grep"的辩护打的是个不存在的靶子——
  而真正的问题不在拆不拆，在这份人读索引有没有职责（§3.7）。

### 1.2 展现层的真实耦合形态

草案给出的"7,087 + 4,749 = 11,836 行"是错的（`internal/i18n` 实测约 4.5k 行 / 30 个非测试文件），
且这类数字随每次重构漂移，按项目文档规约本就不该写进设计文档当作结构性指标。
真正有意义的是**耦合形态**，与行数无关：

1. **表格逐格拼接**。`newTable` / `t.row(cells...)` 之上是一层层 `xxxCell()` 函数，
   列宽、列序、修饰符全在字符流里定死。
2. **业务判断混进字符流**。"样本量 < 20 追加 `⚠️low-n`"、"覆盖率 < 90% 挂脚注 `¹`"、
   "工具参数按深度截断"——这些是数据事实或表现策略，现在都以"拼字符串时顺手判断"的形态存在。
3. **计算与渲染不可分割**。没有任何入口能表达"读现有 JSON、套新渲染器、重出 Markdown"。
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
| 详单在宏观报告与 Journey 渲染之间字节一致（`story` 是 journey 的旧称） | `KNOWN_ISSUES` | 渲染指纹必须携带 `(record, manifest, prev)` 身份与渲染器版本 |
| 产物权限 0600 / 目录 0700 | CLAUDE.md | 新增子目录、缓存目录一律照此 |
| 单一时区显示权威 | CLAUDE.md | 时间在数据层就以 `ts` + `ts_display` 双字段落盘，`ts_display` 经 `fmtutil.DisplayZone` 定稿；前端不做二次换算（§5.6） |
| 原子写用 CreateTemp+Rename，不做目录 fsync | `KNOWN_ISSUES` | 多切片写入必须定义提交顺序，见 §3.4 |
| 报告产物是可再生派生物，非对外承诺契约 | 本文裁决（§8.1）；与 `KNOWN_ISSUES` 「JSON 无外部脚本消费方」一致 | 破坏性变更一步到位；唯一跨版本兼容义务是审计日志历史格式 |
| 分析半区的瓶颈是内存不是磁盘 | `KNOWN_ISSUES`（万级记录 GB 级 RSS） | 切片化应顺手改善内存，不该拿"写盘开销"当卖点 |
| 严格 YAML（`KnownFields`） | CLAUDE.md | 新配置键必须同步 `config.example.yaml` 与 `.zh` 版 |

---

## 2. 概念模型归一

### 2.1 终结 `Story`，统一为 `Journey`

历史演进留下了命名双轨：命令叫 `vmr story`、目录叫 `stories/`、索引叫 `vmr-stories.json`，
但内存模型、单体文件与对比文件全叫 `journey` / `compare`。系统里真正的业务对象只有两个：

- **Request**：一次 HTTP 调用的原子事实（微观点）。
- **Journey**：一次用户目标驱动、跨多轮对话/工具调用/上下文压缩的连续任务旅程（中观链）。

`Story` 不指代任何第三种东西，全面废弃：目录改 `journeys/`，索引改 `journeys/index.{json,md}`；Go 包与代码符号同步更名（`internal/story` → `internal/journey`），把 `story` 一词从代码、CLI 与产物中一次清干净。

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
文件 `journeys/benchmarks.{json,md}`，CLI 主名 `-benchmark`，`-corpus` 别名一并删除（§8）。

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
草案列了前三条，其余是审查中补上的：

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
5. **会话/任务标题映射**。会话标题、别名、任务标题来自 `SessionAnalysis`，
   而 `SessionAnalysis` **当前完全没有被序列化**——请求浏览器要按会话/任务分组，
   就必须补一份精简投影进 `requests/index.json`（只含分组与标题所需的映射，
   不是整个分析结果）。
6. **跨产物链接**。`journeyLink` 这类"请求 → journey"的交叉链接现在由 cmd 层临时组装，
   没有落进任何产物。看板需要它，应作为 `requests/index.json` 的一个字段。
7. **时间的双字段形态**。每个时间点落 `ts`（epoch，供排序筛选）+ `ts_display`
   （`fmtutil.DisplayZone` 定稿串，供显示）。理由与守卫见 §5.6。

### 3.4 产物提交顺序与原子性

多切片方案引入了单体方案没有的新失败模式：写到一半崩溃，留下版本不匹配的切片集。
仓内已有 CreateTemp+Rename 惯例，但需要额外一条纪律：

> **`manifest.json` 最后写。** 所有读取方以 manifest 为准入：manifest 不存在或
> 其记录的切片指纹与实际文件不符，整套产物视为无效，而不是逐切片降级采纳。

这与 `vmr-quota.json` "结构损坏整文件拒绝，绝不部分采纳"是同一条原则。

**准入检查分两档，因为浏览器做不了完整那档。** `crypto.subtle` 只在 secure context
可用——`http://localhost` 算，局域网上的 `http://192.168.x.x` 不算，`file://` 更不算——
所以"重算每个切片的 sha256 比对 manifest"这条只对 Go 侧读取方成立：

| 读取方 | 准入检查 |
|---|---|
| `-render-only`、内部/外部脚本 | 完整：manifest 存在 **且** 逐切片重算指纹一致，否则拒绝整套 |
| 浏览器骨架页 | 弱化：manifest 存在 + `format` 比对（§6.6），不做切片哈希校验 |

弱化那档是可以接受的：切片指纹防的是"写到一半崩溃"，而 manifest 最后写这条纪律
已经让那种目录连 manifest 都没有——前端的 404 分支正好覆盖它。真正落到前端的
残余风险只有"manifest 写完之后有人手动改了某个切片"，那不是本方案要防的威胁模型。

### 3.5 产物精简决策

| 原产出物 | 决策 | 理由 |
|---|---|---|
| `vmr-requests.md` 与全部 `vmr-requests-<tag>.md` / `-cron-*.md` | **整族删除**（D7） | 人读请求索引没有独占职责，详见 §3.7。数据留在 `requests/index.json`，浏览交给 `request-browser.html`。 |
| `vmr-requests-failed.md` | **保留** | 唯一的例外：它是排障入口，量小（实测数十行），"出错了"是不该先开浏览器的那一类信息。 |
| `stories/` 命名空间 | **改名 + 下沉** | → `journeys/`，单任务实例进 `journeys/details/`，对比提至根级 `compares/`，避免海量 `j-<id>` 淹没索引 |
| `stories/vmr-story-corpus.*` | **重定位 + 更名** | → `journeys/benchmarks.{json,md}` |
| `tool-waste.html` | **拆数据** | 数据源下沉为 `macro/context-efficiency.json`；HTML 归一为骨架 + fetch 形态（§6.4），不再有自包含单文件 |
| `vmr-report.json` | **解构** | 一步拆为五切片，不留兼容视图（§3.2、§8.1） |
| `details/`、`evidence/` | **下沉对称** | → `requests/details/`、`requests/evidence/`，与 `journeys/details/` 同构；维持懒物化 |
| `.parse-cache/`、`stories/.llm-cache/` | **归一** | → `.cache/parse/`、`.cache/llm/`，消除散落隐藏目录 |

### 3.6 Journey 切片自包含：tree + 同文件 blob 表

**裁决 D18。** `journeys/details/j-<id>.json` 现在**装不下**同名 `.md` 渲染的内容，
这是本方案里唯一一处 JSON 不是无损真源的地方，必须补齐——否则 §5.0 的单一渲染路径
在 journey 半区不成立，而"机器读 JSON 看到的比人读 Markdown 少"本身就是数据层的缺陷。

现状缺的四样，都是渲染器天天在渲染的：

| 内容 | `JourneyStructure` 现状 | `.md` 现状 |
|---|---|---|
| 工具结果正文 | `ToolCallRef` **没有承载它的字段** | `toolResultLine` 渲染完整结果 |
| 工具配对的置信度 | `Matched bool`，只区分"精确/归一化"与"无" | 位置推断的配对也渲染，带"按位置推测"badge |
| Compaction 前驱摘录 | `CompactionRef` 显式排除 | `renderCompactionInfo` 渲染它 |
| 长正文尾巴 | 一律截到 2000 | 工具参数/结果截到 3000，RespText 不截 |

`Matched bool` 那一行不是取舍，是缺陷：它把三级配对压成两级，
于是一条把 23 个工具结果全靠位置推断配上的 journey，在 JSON 里报告的是
"23 个调用全部 `matched: false`"——机器读到"一个都没配上"，人读到 23 段结果。

**补齐的形态是 tree + blob，不是把正文摊平内联：**

```json
{
  "structure": { "tasks": [{ "steps": [{
      "resp_ref": "<h>",
      "tool_calls": [{ "id": "…", "name": "exec", "args_ref": "<h>",
                       "result": { "ref": "<h>", "match": "positional", "is_error": false } }],
      "new_events": [{ "hash": "<h>", "role": "tool" }],
      "compaction": { "tokens_before": 0, "predecessor_excerpt_ref": "<h>" }
  }]}]},
  "bodies": { "<h>": "文本…" }
}
```

`bodies` 按内容哈希去重，一段文本无论被工具结果、`new_events` 还是别处引用几次都只存一份。
**blob/tree 分离这条原则被保留了**——`ToolCallRef` 原注释反对的是"同一个 blob 在同一棵树里
挂两个地址"，那是在论证需要一张共享表，不是在论证正文不能进 JSON。
唯一变的是 blob store 的位置：从"外部审计日志"搬进同一个文件，JSON 因此自包含。

**体积可控，因为要补的东西全都已经被渲染器自己截过**：工具结果与 compaction 摘录
本就上限 3000 字符，工具参数上限 3000。截断口径随之收敛为**数据层截一次、Markdown 渲染
它拿到的东西**，"structure 截 2000、md 截 3000"这个漂移面一并消失。
唯一放开上限的是 RespText/Reasoning（代码注释已判定"一个 Step 自陈的理由从来不会大到需要截"）。
去重还顺手带来一个现在没有的收益：agent 原地打转重复执行同一条命令得到同样输出时
（`exact_repeat_tool_call` 正是在检测这个），`.md` 里逐份铺开的重复结果在 blob 表里折叠成一份。

**为什么请求详单不照此办理**（§5.0 的豁免因此是有原则的，不是随手划的）：
journey 层的 `appendNewEvents` 已经做完 seen-hash 去重，一条消息在一条 journey 里只出现一次，
正文量是 O(N)；而请求详单按定义要展示**那一轮的全部历史**，agent 每轮重发全历史，
全量物化是 O(N²)——`KNOWN_ISSUES` 有实测，`story.Step` 曾经持有完整记录时峰值 RSS 达 43GB。
同一条"自包含"原则，两个量级不同的结论。线画在"去重之后是 O(N) 还是 O(N²)"上，
不画在"是不是对话正文"上。

### 3.7 为什么删掉人读的请求索引

**裁决 D7。** 不是"合并还是拆分"的版式问题——是这份产物有没有独占职责的问题。

把人实际会带来的问题逐条归位，请求索引只剩一个候选职责：

| 问题 | 谁回答 |
|---|---|
| 整体怎么样：成本、成功率、哪个端点拖后腿 | `vmr-report.md` |
| 某个任务为什么绕了那么多弯 | `journeys/details/j-<id>.md` |
| 有哪些任务，挑一个看 | `journeys/index.md` |
| 这一条请求发了什么、收到什么 | `requests/details/`、`vmr replay -print -req` |
| 哪些请求失败了 | `requests/failed.md` |
| **按维度筛选、排序、定位一条请求** | ← 只剩这一条 |

而 Markdown 恰好是干这件事最差的形态。实测语料 4550 条请求 / 718 个会话，
人读索引 11800 行——没有人线性读它。真实用法只有两种：**精确定位**（`grep` 一个坐标或模型名，
打 `requests/index.json` 同样有效，`jq` 还能按结构筛）与**探索性筛选**
（"上周 cache 命中率低的那些请求"，Markdown 根本做不到，这正是 `request-browser.html` 的活）。

它还和 journey 半区大面积重叠：111 条 journey 覆盖 94% 的请求；
journey 索引已经按 `task / cron / heartbeat` 分类，与请求索引按客户端/定时类拆兄弟文件
是同一套分类做了两遍；会话卡片头几乎逐字是 journey 索引那一行，而且卡片自己就链过去。
真正独占的只有逐轮的 token/延迟/cache-eff 列——那恰恰是最典型的"要排序要筛选"的表格数据。

所以：`requests/index.json` 留作数据真源（喂看板、`jq`、LLM），
`requests/failed.md` 留作排障入口，人读的全量索引整族删除。
将来若出现只有它能回答的问题，从 `index.json` 再生成一份是低成本的——
现在为一个想不出用途的产物挑版式不是。

---

## 4. 目标目录拓扑

```
reports/
├── manifest.json                     # 全局元数据 + 切片清单 + 指纹（最后写，读取方准入）
├── vmr-report.md                     # 由 ViewModel 渲染的宏观报告
├── macro/
│   ├── summary.json
│   ├── finance.json
│   ├── reliability.json
│   ├── workloads.json
│   └── context-efficiency.json
├── requests/
│   ├── index.json                    # RequestRow[] + 会话映射投影 + journey 交叉链接（无 .md 对应物，§3.7）
│   ├── failed.jsonl
│   ├── failed.md                     # 唯一保留的人读请求文档：排障入口
│   ├── details/r-<ts>_<virt>_<real>_<outcome>_<h8>.md  # 懒物化，命名见 §1.1 术语
│   └── evidence/sysprompt-<h8>.md    # 懒物化，内容寻址去重
├── journeys/
│   ├── index.json / index.md
│   ├── benchmarks.json / benchmarks.md
│   └── details/j-<id>.{json,md}      # .json 自包含：structure（tree）+ bodies（blob 表），§3.6
├── compares/
│   ├── index.json / index.md
│   └── compare-<a>-vs-<b>.{json,md}
├── *.html                            # 看板骨架页（go:embed 资产，每次运行幂等覆盖，零业务数据）
└── .cache/
    ├── parse/<filehash>.json
    └── llm/<key>.json
```

目录 0700、文件 0600。

---

## 5. Markdown 渲染重构：ViewModel 分层

### 5.0 单一渲染路径：把"两条路径要一致"消解成"只有一条路径"

**裁决 D11。** 草案（与本文早先版本）默认存在两条路径：全量运行从内存聚合结果直接渲染，
`-render-only` 从落盘 JSON 渲染。于是必须再加一条测试去证明两者字节一致（原 §9 的核心断言）。
这是典型的"造出边缘情况再去守住它"。

正确的做法是让它不可能发生：

> **聚合类产物的渲染只有一条路径：`聚合 → 写 JSON → 读 JSON → ViewModel → 序列化 → 写 Markdown`。**
> 全量运行不是"顺便也写一份 JSON"，而是**先写 JSON，再走与 `-render-only` 完全相同的后半段**。
> `-render-only` 因此不是一个平行实现，只是**跳过前半段**。

三个直接后果：

1. **一致性不再靠测试保证，靠结构保证。** §9 里"render-only 与全量运行字节一致"从核心正确性主张
   降级为廉价的回归守卫。
2. **JSON 无损性由构造保证。** 任何"只在渲染期算得出"的事实会立刻在这条路径上暴露为缺字段，
   而不是等到有人去跑 `-render-only` 才发现——§3.3 那一串缺口正是靠这条路径被逼出来的。
3. **`journeys/details/j-<id>.md` 的重写不再是可选项。** 现状 `RenderMarkdown` 吃内存 `*Journey`，
   在本原则下必须改为吃 `JourneySummary`——这不是"为了支持 render-only 额外做的事"，
   而是唯一的渲染路径本身。前提是 §3.6 把 journey JSON 补成自包含；
   那件事本来也该做，它是数据层的缺陷修复，不是为渲染路径付的钱。

**显式豁免：详单与证据（`requests/details/`、`requests/evidence/`）是另一类产物。**
它们的输入是原始审计字节，且是懒物化的按需产物——把它们的输入放进 JSON 是 O(N²)
（每轮重发全历史），而 journey 层去重后是 O(N)，所以那边能自包含、这边不能。
理由展开见 §3.6 末段。两类产物各自内部无分支，边界清晰——这比"一类产物、两条路径"简单得多。

### 5.1 为什么不直接上模板引擎

把现有 Markdown 逻辑原样搬进任何模板（含 `text/template`）会造出更糟的东西：

- **表达力不足**。决策脊柱的参数智能截断、多阶标签计算在模板语言里会比 Go 更晦涩，且几乎无法调试。
- **排版脆弱**。Markdown 表格对 `|` 和换行极度敏感，模板的空格/缩进控制极易破格。
- **丢失编译期检查**。字段名拼错在 Go 里是编译错误，在模板里是运行时错误或静默空串。

### 5.2 分层结构

```
[ 领域切片 JSON ]  macro/*.json, journeys/details/j-*.json, requests/index.json
        │
        ├────────────────────────────────┐
        ▼  纯 Go 函数（类型安全、已本地化、可单测）   ▼  浏览器侧 fetch
[ ViewModel ]  扁平、排版就绪、只驻内存        [ 看板 JS ]  自带格式化（§5.6）
        │                                        │
        ▼  固定序列化器（§5.3）                    ▼
   [ *.md 产物 ]                              [ DOM / 内联 SVG ]
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
    ID     string   // 稳定标识，用于锚点，与语言无关
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

**裁决 D4：全部文案（含章节标题）进 ViewModel，渲染器只表达结构与顺序。**

同一份数据要渲染 en / zh 两套输出。文案进模板就意味着每语言一套模板（翻译漂移，
加一个章节要改 N 份）或模板内 i18n 查表（把编译期类型安全换成运行时 key 拼写错误）。
文案全部进 ViewModel 后，渲染器退化为纯结构骨架，天然双语；"自由排版"的空间变小，
但语言正确性是硬要求。

序列化器按固定顺序输出：标题行 → 时间戳/范围 → highlights → 各 Section（`##` 标题、
Intro、`renderTable` 表格）→ Disclaimers/Footnotes，全文无一处字面文案（§5.3）。

配套地，`internal/i18n/report_*.go` 与 `internal/report/section_*.go` 的一一配对纪律
改为与 **ViewModel 构建器**配对（`section_cost.go` → `viewmodel_cost.go` → `i18n/report_cost.go`），
`archtest` 的 i18n 检查随之更新——纪律保留，配对对象换一个。

### 5.3 渲染器：固定序列化器，不引模板引擎

**裁决 D3。** 模板引擎的价值前提是"结构可变"或"排版由非 Go 代码维护"。这里两条都不成立：

1. **结构不可变**。ViewModel 把全部输出归一为 Section/Table 两级固定模式（§5.2），
   Markdown 渲染因此是 VM 的确定性序列化函数——约百行 Go，没有模板语法可写错。
2. **没有"不重编译改排版"的需求**。这是单二进制本地工具，改排版的就是本仓作者；
   `text/template` 反而引入运行期字段拼写错误这一全新错误类，换不来任何表达力——纯亏。

所以 Markdown 渲染器就是一个固定结构的序列化器：改排版 = 改这段 Go = 重编译。
HTML 侧的"模板"本就是必然存在的骨架页 + JS 渲染函数（§6.1），与 Markdown 序列化器同构——
**全系统只有一种模板体系：数据契约 + 渲染函数**，不出现第二种。

推论：无 `go:embed` 模板、无 `-templates <dir>` 逃生口、无模板指纹、无"外置模板不参与
golden 比对"的特例。L3 缓存指纹只含 ViewModel 指纹 + 渲染器版本 + 语言（§7.1）。

**关于"先把 HTML 模板化，再回头重构 Markdown"的分步提议**：路线图已经是这个顺序（Phase 2 → Phase 3），
但要点不在先后，而在两者**无依赖**——HTML 侧消费的是领域切片（§5.6），Markdown 侧消费的是同一批切片经
ViewModel 的投影，中间没有共享模板资产。所以先做哪个都不会给另一个留债，
也不存在"等 HTML 模板化完成后 Markdown 才能沿用其模板体系"这条路径——那条路径需要一个跨 Go/JS 的
模板运行时，是比 `text/template` 更重的引入。

### 5.4 `-render-only`：覆盖面必须诚实

```bash
vmr analyze -render-only [-o reports]
```

检测到 `manifest.json` 与各切片存在且指纹自洽，跳过全部日志解压、解析、哈希与建图，
只做 `JSON 读取 → ViewModel 构建 → 序列化渲染 → 写盘`。

**裁决 D5：覆盖全部人读的聚合类产物，详单与证据永久除外。**

| 产物 | 能否 render-only | 依据 |
|---|---|---|
| `vmr-report.md` | ✅ | 补齐 §3.3 的缺口后，五个 macro 切片即全部输入 |
| `journeys/index.md`、`benchmarks.md` | ✅ | 输入本就是已落盘的聚合结构 |
| `journeys/details/j-<id>.md` | ✅ | 依赖 §3.6 把 journey JSON 补成自包含；`RenderMarkdown` 随之从吃 `*Journey` 改吃 `JourneySummary` |
| `requests/failed.md` | ✅ | 输入是 `failed.jsonl`，本就已落盘 |
| `requests/details/*.md`、`evidence/*.md` | ❌ 永远不行 | 输入是原始 `audit.Record` + manifest。把它们放进 JSON 是 O(N²)（§3.6 末段），且默认套件本就不物化它们 |

最后一行不必遗憾：`KNOWN_ISSUES` 里"默认分析套件不物化 `details/`"一条已把这条纪律用测试锁死，
详单靠 `vmr replay -print -req <coord>` 按需取。
`-render-only` 的语义因此是**"重绘全部常驻人读产物"**——除了那两类懒物化的按需资产。

骨架页不属于聚合类文本产物，不在上表覆盖面之内；但实现纪律是**每次 analyze 调用
（含 `-render-only`）都幂等刷新骨架页**，让 `/reports/` serve 出去的页面始终与二进制同代。
代价是一个新组合：`-render-only` 会写出"新骨架 + 旧 JSON"——正常路径上骨架与数据同进同退
（格式版本升级按 §7.4 强制 JSON 重算），这正是 §6.6 的探测 banner 要兜底的窗口之一。

耗时不写死数字：这一路径的开销只有 JSON 反序列化 + 字符串拼接 + 写盘，
与日志规模解耦，与产物规模线性相关。实测值在实施后填进 CHANGELOG，不写进设计文档。

### 5.5 语言与 render-only 的关系

**裁决 D10。** 既有的 lang-follows-everywhere 政策让 JSON 里的人读文本（Finding 叙述、
Action 建议、亮点句）跟随 `-lang` 落盘，只有 Finding 的 `Code` 是跨语言稳定标识。
因此 **`-render-only` 只能渲染出与 JSON 同语言的 Markdown**。

草案把"切换中英文输出"列为 `-render-only` 的应用场景，这是错的。
要支持跨语言重绘，必须把全部人读文本改为渲染侧从 `Code` + 结构化参数派生——
那是对既有语言政策的推翻，影响 JSON 契约的所有消费者，应作为独立议题单独评审，不塞进本次重构。

### 5.6 ViewModel 不落盘：谁负责格式化，以及漂移怎么防

**裁决 D12。** ViewModel 落盘看起来很诱人——Markdown 与 HTML 从同一份排版就绪数据出发，
格式化只做一次。否决它的是两条：

1. **前端要的是 raw，不是 display。** 看板要按成本排序、按延迟筛选、画分位图，
   拿到的若是 `"$1.23"` / `"1.2M"` 这样的字符串就得再解析回去。
   ViewModel 的纯字符串形态恰恰是前端最不需要的形态。
2. **落盘即成第二本账。** VM 是领域切片的派生投影，落盘后就有了两份可能不一致的数值来源，
   直接违反 §11.1 第 5 条。

因此：**领域切片是唯一落盘数据（raw 值）；ViewModel 是 Go 内存中的 Markdown 专用层；
HTML 前端有自己的一份格式化函数。** 代价是格式化逻辑存在 Go 与 JS 两份实现，
两条纪律把漂移钉死：

- **时间不给前端算。** 切片里每个时间点同时落 `ts`（epoch，供排序/筛选）与
  `ts_display`（`fmtutil.DisplayZone` 定稿串，供显示）。前端只显示后者、只用前者排序，
  一行时区代码都不写。§11.1 第 4 条从"靠自律"变成"靠数据结构"。
- **数值格式化用共享 fixture 钉住。** 一份 `testdata/fmt_cases.json`（raw 值 → 期望显示串），
  Go 侧由 `fmtutil` 测试消费，JS 侧由一个纯函数测试消费（Node 跑，不进构建链）。
  `FmtTokens` / `FmtBytes` / `FmtPercent` / 货币的任何改动，两侧同时红。

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
| `request-browser.html` | `requests/index.json`。**人读请求索引删除后（§3.7），这是浏览逐条请求的唯一交互入口**，因此筛选/排序/分面是它的必需能力而非加分项：按客户端、模型、端点、outcome、时间窗筛，按耗时/token/cache-eff 排，行内链向 `requests/details/` 与所属 journey |
| `journey-viewer.html` | `journeys/details/j-<id>.json` |
| `journey-compare.html` | `compares/compare-*.json` |
| `benchmarks.html` | `journeys/benchmarks.json` |
| `tool-waste.html` | `macro/context-efficiency.json` |

**裁决 D13：hash 传参，路径逐字一致。**

骨架页全部平铺在 `reports/` 根级（§4 的拓扑），`#data=` 的值是**相对骨架页自身目录**的路径：

```
reports/journey-viewer.html#data=journeys/details/j-a1b2.json     (file 布局)
/reports/journey-viewer.html#data=journeys/details/j-a1b2.json    (HTTP 路径)
```

（骨架页不下沉进 `journeys/`：一个页面要能加载 `journeys/` 与 `compares/` 两处的数据，
把它塞进其中一个目录只会让另一处的相对路径变成 `../`。根级平铺让所有 `#data=`
与 §4 拓扑图里的路径逐字相同。）

- **参数走 `#` 而非 `?`。** hash 不进 HTTP 请求行：服务端不需要为参数额外做一套路径校验
  （§6.5 只需守住静态文件路由本身），参数不进服务端访问日志，前端 `location.hash` 自己解析，
  同一份页面在任何托管方式下行为完全一致。查询参数把一个纯前端的选择变成一次服务端输入，
  是白白扩大攻击面。
- **磁盘布局 = HTTP 路径布局。** `/reports/` 之下逐字对应 `reports/` 目录，页面只用相对路径 fetch。
  于是 vmr 自带托管、`python3 -m http.server`、任意静态服务器三者等价，换一个托管方式不改一行前端代码。
- **`file://` 直开不是支持路径，必须写明。** 浏览器给 `file://` 页面 opaque origin，
  同源策略拦死一切 `fetch`（Chrome/Firefox/Safari 一致），相对路径一致性在这里也救不了。
  骨架页在 `fetch` 失败且 `location.protocol === 'file:'` 时显示一行提示，
  给出"用 vmr 自带托管或任意静态服务器打开"的指引——**不做静默空白页**。

缺省行为：无 hash 参数时按约定探测——每个骨架页内置自己的默认索引路径
（`journey-viewer.html` → `journeys/index.json`，`journey-compare.html` → `compares/index.json`），
取到后列出候选让用户点选。

**零编译扩展**：切片是稳定契约，看板只是它的一个消费者。
需要一块专属大屏（例如只看成本的财务视图）时，复制一个骨架页、改 JS、
指向同一份 `macro/finance.json` 即可，不触碰任何 Go 源码、不需要重新构建二进制。
这正是切片拆分在"关注点分离"之外的第二重回报。
代价与边界随之而来：从复制那一刻起，这个副本就是切片 schema 的消费者——
版本探测样板随复制继承（§6.6），schema 演进纪律见 §8.1 的作用域注记。

### 6.3 前端工程化边界

草案对图表只字未提，但宏观看板要画的东西（延迟分位、按小时热力、Spearman 散点、
成本堆叠）不是表格能替代的。两条路必须现在就选，否则工作量估算无意义：

- **选定：零依赖内联 SVG**。手写坐标轴 + path，够用于折线/柱状/热力/散点四种形态。
  可复用的是**视觉系统**（`internal/story/render_html_assets.go` 的 "VMR Forensics"：
  暗色飞行数据记录仪 / 亮色工程方格纸），不是图表代码：现状全部的 SVG 只有
  `render_html_dashboard.go` 里一条无坐标轴的 sparkline `<polyline>`。
  四种图元连同坐标轴、刻度、图例基本是从零写——Phase 2 的估算按此计。
- 否决：引入图表库。即使 `go:embed` 进二进制，也会带来体积、升级与 CSP 三重负担，
  换来的只是省几百行手写 SVG。

主题通过 CSS 变量注入，暗/亮双色 + 系统跟随，与 `/status.html`、`/log.html` 保持同一套视觉。

### 6.4 自包含 HTML：废弃现有渲染器

**裁决 D6（翻转草案）。** 先把事实摆正：自包含不是一个可关的开关，而是**现状唯一形态**
（§1.1 基线修正二），废弃它意味着删掉三处 Go HTML 渲染器并重建为骨架页——这是替换，工作量记在 Phase 2。

自包含形态的唯一独占场景是 `file://` 直开——`fetch` 在 `file://` 源下被同源策略拦死（§6.2）。
但这个场景没有真实用户路径：

- vmr 本身就是常驻 HTTP 服务，`/reports/` 托管（§6.5）是必然存在的能力；
  骨架 + fetch 是纯静态资产，任何静态文件服务器指向 `reports/` 也可用。
- "把单个文件转交他人"的价值已被 Markdown 覆盖：`journeys/details/j-<id>.md`
  就是给人读的完整详单。转发一个 URL + auth key，或转发一份 `.md`。

而自包含形态的代价是实打实的：数据变更必须重跑 Go 才能刷新页面，
渲染逻辑被锁在 Go 里改一行版式就要重编译，且它把决策正文（RespText、工具参数、
Finding 文本）复制成第三份 0600 资产。
（说清边界：journey HTML 本就**不内联逐步对话**，每行链出到 `details/*.md`——
泄露面比"把全语料再抄一份"小得多，但不是零。）
功能能被必然存在的特性替代，就删——三处 HTML 渲染器与 `internal/story/assets/` 一并退场，
不保留任何"内联 / fetch"双分支。

**裁决 D15：`-redact` 一并删除，不迁移。** 它是自包含 HTML 的修饰符，
唯一用途是产出"一份可以带出本机的脱敏文件"。骨架 + fetch 形态下这个用途无处安放
（脱敏后是一组 JSON 切片加一个骨架页，"转交一个文件"变成"转交一个目录"），
而实际使用频次接近零——为一个没人用的能力保留一整条自包含渲染分支，
恰好是本方案要消灭的那种"双形态"债务。真需要脱敏分享时，
`-journey` 的 Markdown 详单加人工删节是更诚实的路径：它至少不会让人误以为
自动脱敏覆盖了所有正文字段。同步删除：`-redact` flag、`render_html*.go` 里的
全部 `redact bool` 分支、`i18n` 侧对应的占位文案、UserGuide 双语的 `-redact` 段落。

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

**裁决 D16：托管目录默认 `./reports`，与 `analyze -o` 同一个默认值，启动时可覆盖。**
两侧共用同一个默认，是"生成即交付"（§6.1）能成立的前提——用户跑一次 `vmr analyze`、
再 `vmr start`，不配任何东西就该看得到看板。

- 分析侧：`analyze -o` 的默认值维持现状 `./reports`（相对进程工作目录，
  与 `config.yaml` 的默认查找位置同一个目录，本方案不改）。
- 服务侧：`analytics.serve_dir` 默认同为 `./reports`；显式配置时按 `vmr start`
  进程的工作目录解析相对路径。输出目录被 `-o` 指到别处时，把 `serve_dir` 配成同一个值。
- 目录不存在不是启动错误——只是 `/reports/*` 一律 404，日志提示"尚未生成分析产物"。
  分析产物是可再生派生物，让路由器为了一个还没跑过的报表拒绝启动，本末倒置。

**不引入新的路径概念**：这里刻意没有走 `rundir`。`rundir`（`~/.vmr`）是**运行期状态**
（`vmr-quota.json`）的归属，而报表是**用户产物**，用户要 `cd` 进去 `grep`、要 `git` 忽略、
要按项目分开放——它属于项目目录，不属于用户主目录。

依赖方向：`internal/server` 只是按路径读文件目录，**不 import 任何分析半区的包**，
两半区契约不破。`serve_dir` 是一个字符串配置项，不是分析半区传过来的对象。

### 6.6 数据版本探测：manifest format banner

**裁决 D14。** "零编译扩展"（§6.2）把切片 schema 变成了第三方代码的消费契约：
Phase 2 落地那一刻，`KNOWN_ISSUES` 里"JSON 无外部脚本消费方"的既有条目失效
（作用域注记见 §8.1）。对 vmr 自带的骨架页这不是问题——二进制与切片同步发版，永远一致；
会错配的只有两个窄窗口：

1. **`-render-only` 打在旧产物目录上**。按 §5.4 的实现纪律每次 analyze 调用都幂等刷新
   骨架页，于是可能写出"新骨架 + 旧 JSON"的组合。
2. **用户自定义副本**。老副本 JS 消费新切片——现实中主要的错配来源。

机制：**每个骨架页启动时先 `fetch('manifest.json')`**，比对其中的 `format` 与页面内置的
期望版本常量（`go:embed` 资产随二进制同步更新），三条分支：

- **一致** → 正常渲染。
- **不一致** → 顶部 banner，**渲染继续**。banner 写明期望/实际两个版本号，修复动作指向
  全量 `vmr analyze`（不是 `-render-only`，后者按定义不重生成切片）。不阻断是刻意的：
  加性变更（只加字段）下老页面大概率照常渲染成功；不一致只说明"无保证"，不说明"必然坏"。
- **manifest 404** → 另一种提示："这不是一套完整的 analyze 产物"（目录不全或路径指错）；
  `file://` 下 fetch 本就被同源策略拦死，与 §6.2 的降级指引共用同一套 UI。

渲染异常一律 catch，错误提示把"manifest format 不一致"列为首要嫌疑并指向 banner——
用户看到的是"渲染不出来可能因为版本不一致，重新 analyze 即可"，而不是一片空白面板。

**为什么放骨架页样板，而不是注入用户副本**：注入做不到——副本 JS 完全归用户所有。
但副本是从骨架页**复制**出来的：版本检查与 fetch 封装、主题变量同层，是"复制起点"代码
的一部分，复制即继承（除非刻意删除）。配套纪律：**升级二进制后，自定义面板从最新骨架页
重新复制起点**。对完全自建、不基于骨架页的第三方代码没有任何手段，也不追——
这条机制的价值边界就是"自带页面全覆盖 + 派生面板靠复制继承 + 自建面板自担风险"。

**版本戳不进切片。** 逐片盖章是 N 份冗余副本，只会引入"切片与 manifest 版本不一致"的
漂移面；§8.1 已裁决整套产物一个版本单位、版本戳统一收在 manifest。页面检查走 manifest
正好与"读取方以 manifest 为准入"（§3.4，前端本就在列）同构；缓存侧同理——L2 的 Digest
已含格式版本（§7.1），Phase 4 的 `-no-cache` 旁路覆盖缓存对照，无需第二套版本源。

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
   价值：整套产物有效则跳过聚合与叙事构建 —— 其中被低估的一块是 Journey 半区：
        L1 只覆盖 ctxgraph 的 manifest 扫描与 report 侧逐记录事实，
        `PreviewTitles` 与每批 `BuildAll` 的 `FetchRecords` 各自还要全量解压一遍
        （`KNOWN_ISSUES` 的「一次 analyze 至少解压三遍」条目记为 `-render-all` 耗时主因）。
        输入未变时 L2 直接跳掉这两遍，对反复重跑的开发循环是分钟级而非毫秒级的收益

L3 表现层缓存（新增）
   reports/**/*.md               依赖：Digest(ViewModel 指纹 ‖ 渲染器版本 ‖ 语言)
   价值：数据或渲染器任一未变则跳过渲染与写盘
```

### 7.2 指纹算法规范

**裁决 D8。** 全系统只有**一个** Digest 构造函数，所有缓存判据都是它的调用点。
先给算法，再给每个产物的参数。

#### 唯一的 Digest 构造：长度前缀的有序链

```
Digest(c₁, c₂, …, cₙ) :=
    h ← sha256.New()
    for i = 1..n:
        h.Write(uvarint(len(cᵢ)))   // 长度前缀，先写
        h.Write(cᵢ)                 // 再写内容，原始字节
    return h.Sum(nil)               // 32 字节，hex 编码后入 manifest
```

三条性质就是选它的全部理由，实施时不要弱化任何一条：

- **顺序敏感**。`Digest(a, b) ≠ Digest(b, a)`。Journey 的步骤序列、切片清单都是有序的，
  重排必须换指纹。
- **无歧义拼接**。长度前缀让 `("ab", "c")` 与 `("a", "bc")` 得到不同结果。
  没有它，`模型名 ‖ 端点名` 这类拼接会在两个字段间制造静默碰撞。
- **重复不抵消**。`Digest(a, a) ≠ Digest()`。Compaction 后的历史重放会让同一个 manifest
  哈希在一条 Journey 里出现两次，任何"把分量异或/求和到一起"的聚合都会让这两次互相消掉。

**分量（`cᵢ`）的取值规范**：每个分量都是**原始字节**，不是它的十六进制字符串
（少一半写入量，且避免"某个调用点忘了统一大小写"这类漂移）；标量先按固定宽度编码
（`int64` 走 `binary.BigEndian`，`float64` 先 `math.Float64bits`），不用 `fmt.Sprintf`——
一个格式动词的改动会静默换掉全部指纹。

#### 输入哈希：直接算内容，没有 fast path

审计日志是**追加写的活文件**，同一秒内的追加对 mtime 完全隐形，
所以文件身份一律走 `ctxgraph.HashFile` 的 sha256（`.parse-cache` 在 2026-09 已经
从 mtime 消歧改成按内容哈希为 key，理由记在 `KNOWN_ISSUES`，本方案沿用同一条结论）。
实测缓存体积远小于日志体积，全量 sha256 的成本已被验证可接受。

#### 摘要函数：sha256 只用在缓存判据上，不去动内容寻址底座

这两件事必须分开，混为一谈会引出一次没有收益的大迁移：

- **缓存判据**（本节的全部 Digest 调用）**一律 sha256**。它决定"能不能复用一份产物"，
  一次静默碰撞就是一份错误的报表，要按最强的标准来。
- **`ctxgraph` 的内容寻址底座是 md5，保持不变**。`ctxgraph.Hash` 是 `[16]byte`，
  `hashJSON` / `HashMsgJSON` / `Manifest.SysHash` / `Lineage` 的根哈希、
  `reqdetail` 证据文件的 `h8`、`ReqHash8` 的文件名去重位——全都是 md5。
  它们是**语料内部的身份标签**，不是安全边界，也不参与"这份产物还能不能用"的判断；
  换 sha256 要重算全部 `.parse-cache`、改 `Hash` 的宽度、动 Journey ID 的形态，
  收益为零。**实施时不要顺手改。**

两者的接缝很干净：**md5 摘要作为字节分量喂进 sha256 的链**——
链的强度由 sha256 保证，分量只需要是"内容变了就变"的稳定标签，md5 在这个角色上足够。

#### 各产物的身份与验证指纹

| 产物 | 身份 Key | 验证指纹 |
|---|---|---|
| 请求详单 | 请求坐标（`ReqCoord`，§1.1 术语） | `Digest(记录原始字节, manifest 身份, prev 身份, 渲染器版本, 语言)` |
| 系统提示词证据 | 提示词正文的 md5（既有 `h8` 前缀即其短形） | 身份即内容，无需二次验证 |
| 单 Journey | `Journey.ID`（已含根哈希前缀） | `Digest(步骤 manifest 哈希按序展开…, 定价指纹, 格式版本)` |
| 整套产物 | 输出目录 | `Digest(输入文件哈希按序…, 配置指纹, 格式版本, 分析参数)`，即 §7.1 的 L2 依赖 |

详单指纹必须携带 `manifest` / `prev` 身份而不只是语言——
这是 `KNOWN_ISSUES` 里"详情页在宏观报告与 Journey 渲染之间字节一致"那条不变量的直接要求，
只做一半会重现"同名文件先写者赢"的旧 bug。

**"配置指纹"与"分析参数"的取值必须写死，不能靠实施者临场判断**——
少算一项就是一次静默的错误命中（§7.0 第 3 条正是这个失败模式）：

- 配置指纹 = 生效的 `config.yaml` 中**影响金额的那部分**：各 provider 的 `pricing.rates`
  覆盖、顶层 `exchange_rate`、内嵌标准表的 `GeneratedAt`。不是整个配置文件的哈希——
  改一个 `listen` 地址不该让报表全量重算。
- 分析参数 = 时间窗（`-from`/`-to`）、`-lang`、taskseg profile、自流量排除集
  （`report.yaml` 的 `llm_key` / `self_traffic_client_tags`）、以及所有改变**取样口径**的 flag。
  判据是"改了它会不会改变任何一个落盘数值或人读文本"——会，就进指纹。

### 7.3 缓存粒度：整套一个指纹，不做切片级隔离

**裁决 D1（后半）。** 草案的核心卖点是"改价只失效 `finance.json`，可靠性与负载切片 100% 命中"。
三条理由否决它：

1. **收益接近零**。定价变更不改审计日志，L1 全命中；剩下的只是内存里重跑一遍分桶累加。
   切片级隔离省下的是这一段的一部分——毫秒级。
   （注意这与 §7.1 里 L2 的收益不矛盾：L2 省的是**输入未变时整套跳过**，
   包含 Journey 半区那两遍未被 L1 覆盖的解压；切片级隔离省的是**输入未变、只有配置变**时的分桶累加。
   前者量级大，后者量级小，而复杂度全在后者。）
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
| 渲染器改动（含 VM 结构） | 命中 | 命中 | 失效 |
| 格式版本升级 | 按解析器版本判 | 失效 | 失效 |
| 二进制版本变化（无格式变更） | 命中 | 命中 | 命中 |

最后一行是有意的：把二进制版本纳入指纹会让每次构建都全量失效，收益为零。
格式版本与解析器版本才是正确的失效锚点。

---

## 8. 兼容边界与实施纪律

### 8.1 产物无兼容期，审计日志才是兼容边界

reports/ 下的一切产物都是审计日志的派生缓存：删掉整个目录重跑 `vmr analyze` 即可完整重建。
因此本方案的全部破坏性变更——切片化、`vmr-report.json` 解构、`stories/` → `journeys/`、
`corpus` → `benchmark`、人读请求索引整族删除、journey JSON 补 `bodies` 表与三级 `match`、
详单文件名加 `r-` 前缀、自包含 HTML 与 `-redact` 废弃、`vmr report` / `vmr story` 别名删除——
**一步到位，无兼容视图、无弃用期、无过渡别名**，`meta` 不带 `deprecated` 标记；
CHANGELOG 记一条 Breaking Change 即可。前提是所有仓内消费者（脚本、看板、测试）在同一变更内同步更新。

唯一跨版本的兼容义务在**审计日志格式**：历史日志必须永远可解读。这是解析层的既有不变量
（`.parse-cache` 按解析器版本判失效），不在本方案改动范围之内。

`manifest.json` 的 `format` 从 10 直接递进到 11。切片各自不带独立版本号——
整套产物是一个版本单位，与 §7.3 的整套指纹一致。
这顺带闭环 `KNOWN_ISSUES` 里"`journey-<id>.json` 的 `structure` 无 schema 版本戳"一条：
版本戳统一收在 manifest，单文件不再各自长一个。

**"无兼容期"的作用域注记：它只覆盖本方案实施之时。** 此刻切片 schema 确无外部消费者，
破坏性变更免费；但 Phase 2 的"零编译扩展"（§6.2）一旦落地，用户副本面板就成为事实上的
schema 消费者，`KNOWN_ISSUES` 里"JSON 无外部脚本消费方"的条目随之失效
（实施 Phase 2 时登记）。此后切片 schema 的演进收敛为**加性优先**：新增字段不算破坏；
删改字段视为 breaking，必须 bump manifest `format` 并在 CHANGELOG 标注 Breaking。
这不冻结 schema 的演进自由——代价只是把"静默破坏"变成"版本可见的破坏"，
浏览器侧由 §6.6 的探测 banner 兜底。

实施纪律：单步改动若宽到无法收敛为局部、正交的任务集，则开新包并行实现新方案，
完成后整体切换、删除旧簇——避免在旧代码上做渐进补丁把两边都改乱。

### 8.2 `manifest.json` 的职责

草案没定义它是给谁用的，导致字段无从设计。定死为**三个职责，一份文件**：

1. **准入与一致性**：切片清单 + 每个切片的内容指纹（读取方的唯一入口，见 §3.4）。
2. **溯源**：输入文件列表与哈希、生成时间、时区、时间窗、语言、格式版本、实际生效的配置文件路径。
3. **发现**：切片路径 → 语义标签的映射，供前端与内部脚本免硬编码路径地遍历。

它**不承载任何指标数值**。任何"顺手把总请求数也放 manifest 里"的提议一律否决——
那会立刻制造出与 `summary.json` 的双账本。

### 8.3 实施纪律

不设回滚方案——产物可再生、无外部消费者，"退回旧版本"的成本就是 `git revert` 加一次重跑。
需要的只是每个 Phase 内部可验证：

- **Phase 1**：旧渲染路径在切片化落地后仍能工作，作为数据层正确性的对照物，随 Phase 3 一并删除。
- **Phase 2**：新骨架页与被替换的自包含 HTML 在同一份数据上目视比对，确认无信息丢失后删旧渲染器。
- **Phase 3**：新旧 Markdown 渲染器并存一个开发周期，golden 比对确认字节一致后删旧路径。
  这是唯一需要"并行两条实现"的阶段，因为它是唯一会改变既有产物字节的阶段。
- **Phase 4**：`-no-cache` 旁路常驻保留（不是过渡开关），任何缓存可疑行为都能立刻退回全量重算对照。

---

## 9. 测试与守卫

草案完全没提测试，而这套重构恰好会动到仓内最强的几条守卫。逐条给出去处：

| 现有守卫 | 重构后 |
|---|---|
| `internal/story/golden_test.go` | **下沉到 ViewModel 层**：比对 ViewModel 结构而非最终字符串。结构比对的 diff 可读、对无关排版改动不脆。序列化器另留少量端到端 smoke |
| `internal/story/structure_test.go` 的 `LosslessReconstruction` | **改写为真正的自包含断言**：现状证的是「structure **加上审计日志**能重建」，D18 之后该证的是「只给 `journeys/details/j-<id>.json`，`.md` 能逐字节渲染出来」——审计日志不在输入里 |
| `internal/report/e2e_test.go` | 保留；迁移期增加"切片并集 ≡ 旧 `Report2` 字段集"的等价断言，随旧路径一并删除 |
| 详单在宏观报告与 Journey 渲染之间字节一致 | 保留且加强：指纹携带渲染器版本（§7.2） |
| `cmd/vmr/i18n_e2e_test.go` | 保留；新增"ViewModel 中不存在任何未经 i18n 的裸字面量"的守卫 |
| `internal/archtest` 行数预算 | 新增 `viewmodel_*.go` 需登记；`section_*.go` 随职责迁移会缩水，预算下调 |
| `internal/archtest` i18n 配对 | 配对对象从 `section_*.go` 改为 ViewModel 构建器（§5.2） |
| `internal/archtest` 导入边界 | 新增断言：`internal/server` 不得 import 分析半区任何包 |
| — | **新增**：`-render-only` 产物与全量运行产物字节一致。按 §5.0 两者本就是同一条路径，这只是防止将来有人抄近道再分叉的廉价回归守卫，不再是核心正确性主张 |
| — | **新增**：缓存命中路径与冷启动路径产物一致（浮点用容差，沿用现有 `1e-6` 惯例） |
| — | **新增**：跨语言格式化 fixture（§5.6）——`testdata/fmt_cases.json` 由 `fmtutil` 与看板 JS 各跑一遍，防 Go/JS 显示漂移 |
| — | **新增**：切片内每个时间点必须同时有 `ts` 与 `ts_display`（§5.6），缺一即失败——把"前端不做时区换算"从约定变成可测断言 |
| — | **新增**：骨架页版本探测逻辑写成纯函数（期望版本 × manifest 版本 → 行为），随看板 JS 的 Node 测试一并覆盖（§6.6） |
| — | **新增**：`bodies` 表无孤儿、无悬引用——每个 `*_ref` 都能解析，每个 blob 都至少被引用一次（§3.6）。两个方向都要查：悬引用会让渲染缺内容，孤儿 blob 会让文件白白变大 |
| — | **新增**：工具配对的三级 `match` 与决策脊柱实际渲染的配对一致（§3.6）——这条正是为了钉死「JSON 说没配上、Markdown 渲染了 23 段」那个缺陷不再复现 |

---

## 10. 路线图

工作量按"改动面 + 未知度"给区间，不追求精确。
草案给的 14~19 人天低估了两处：journey JSON 的自包含化（§3.6）与图表实现（§6.3）。

```
Phase 1 — 数据层闭环                                        4~6 人天
  ├── Report2 一步解构为五切片 + manifest.json（无兼容视图）
  ├── 补齐 §3.3 全部渲染期现算事实（含 SessionAnalysis 投影、时间双字段）
  ├── journey JSON 自包含化（D18/§3.6）：bodies blob 表、三级 match、截断口径归一
  ├── 目录拓扑归位：journeys/ 、compares/ 、requests/details|evidence 、.cache/
  ├── 详单文件名加 r- 前缀（D17）——reqdetail.FileName 与其两个 wrapper 的单点改动
  ├── 产物提交顺序与原子性（manifest 最后写）
  ├── 人读请求索引整族删除（D7/§3.7），vmr-report.md 的相关链接改指看板与 journeys/index.md
  └── CLI 面收敛：vmr report / vmr story / -corpus 别名删除，internal/story 更名 internal/journey

Phase 2 — HTML 看板（替换现有三处自包含渲染器，非纯新增）   5~7 人天
  ├── go:embed 骨架 + 主题变量系统（复用 VMR Forensics 视觉）
  ├── 零依赖内联 SVG 图表基元（折线/柱状/热力/散点）
  ├── 六个看板页 + #data= 加载协议 + file:// 提示降级
  ├── 骨架页版本探测 banner + 渲染异常归因（§6.6）
  ├── 前端格式化函数与跨语言 fixture（§5.6）
  ├── 删除 render_html*.go / render_compare_html.go / toolwaste_html.go 与 story/assets/
  └── server 挂载 /reports/*：显式开关、无 key 拒绝、路径校验、禁目录列表

Phase 3 — ViewModel 与固定序列化器                          4~6 人天
  ├── report 侧 ViewModel（各 section 改为构建器）
  ├── journey 侧 ViewModel —— 含 render_spine 重建到自包含的 journey JSON 之上
  ├── 固定结构 Markdown 序列化器（无模板引擎，§5.3）
  ├── golden 测试下沉到 ViewModel 层
  └── -render-only（覆盖面按 §5.4 表格）

Phase 4 — 产物级缓存                                        2~3 人天
  ├── 整套产物指纹（有序链式 sha256）与失效矩阵
  ├── L3 表现层指纹（含渲染器版本）
  └── -no-cache 旁路 + 冷热一致性测试
```

Phase 1 与 Phase 2 之间无强依赖，可并行。Phase 4 因为放弃了切片级隔离而大幅缩小。

**收益判断**（不逐项打分，只说结论）：

- 真正的收益是**可维护性**——Go 专心算数据，排版归 ViewModel + 序列化器，样式归 CSS。这是主要动机。
- 次要收益是**看板能力**与**重绘速度**。前者是新增能力，后者受益者是反复调排版的开发者本人。
- 缓存收益需要分开说：宏观报告侧最贵的一段早已被 `.parse-cache` 覆盖，L2 在那里只是补完整性；
  但 Journey 半区还有两遍未被缓存的全量解压，L2 在输入未变时把它们整体跳掉——那一块是真收益（§7.1）。
  被高估的是**切片级**隔离，不是产物级缓存本身。
- 代价是**新增了一个抽象层**。ViewModel 引入不当会变成"多一层无意义搬运"。
  判据很简单：如果一个字段在 ViewModel 里只是原样透传且不涉及任何格式化或本地化，它就不该存在。

---

## 11. 不变量与已知取舍

### 11.1 必须守住的不变量

1. **两半区一条契约**。`report` / `journey` / `ctxgraph` / `taskseg` / `chatmsg` / `reqdetail`
   不得 import `router` / `server` / `config`；`server` 不得 import 分析半区。
   ViewModel 各自内聚在 `internal/report` 与 `internal/journey` 内，
   **不建跨包共享的通用胖对象**——两侧的表格语义并不相同，强行共享会同时污染两边。
2. **内容寻址底座不可动摇**。缓存判据只能是内容哈希，绝不回退为文件时间启发式。
3. **权限底线**。所有产物 0600 / 目录 0700，含看板骨架页与缓存目录。
4. **单一时区权威**。时间在数据层就以 `ts` + `ts_display` 双字段落盘（§5.6），
   `ts_display` 由 `fmtutil.DisplayZone` 定稿；Markdown 与看板都只显示它，前端不做二次换算。
5. **不新增双账本**。manifest 不放指标，切片间不交叉引用数值，
   看板不在前端重算任何已在 JSON 里的量。

### 11.2 已知取舍（避免下一个评审重提）

| 决定 | 为什么 |
|---|---|
| 不做切片级缓存隔离 | §7.3：收益毫秒级，代价是产物内部一致性缺陷 |
| Markdown 不用模板引擎 | §5.3：VM 模式固定后序列化即渲染；text/template 只引入运行期拼写错误类，换不来表达力 |
| 不支持跨语言 `-render-only` | §5.5：JSON 按既有政策带语言；要改先改语言政策，不在本次范围 |
| `requests/details/*.md` 永不进 `-render-only` | §3.6 末段：请求详单按定义展示那一轮的全历史，全量物化是 O(N²)；journey 去重后是 O(N)，所以那边自包含、这边不 |
| 不引入图表库 | §6.3：体积 + 升级 + CSP 三重负担，换几百行手写 SVG |
| 二进制版本不进缓存指纹 | §7.4：每次构建全量失效，收益为零 |
| journey JSON 携带正文，体积翻倍也接受 | §3.6：实测 json 与 md 已同量级（12.1 vs 12.3 MB / 81 条），补齐后约 18 MB。「JSON 比 Markdown 少」本身就是缺陷，不是节俭；blob 表按内容哈希去重，且所有补入字段都已被渲染器截到 3000 字符 |
| 废弃自包含 HTML（现状唯一形态），`-redact` 一并删除 | §6.4：价值被托管 + Markdown 覆盖；代价是决策正文第三份副本与"改版式要重编译"。`-redact` 是自包含形态的修饰符，骨架形态下无处安放且实际用量接近零（D15） |
| 删除全部人读请求索引 | §3.7：它唯一的候选职责是「按维度筛选定位一条请求」，而 Markdown 是干这件事最差的形态；4550 条请求没人线性读，精确定位靠 `jq`/`grep` 打 `index.json`，探索性筛选靠 `request-browser.html`。与 journey 半区还大面积重叠（94% 覆盖、分类做两遍）。`failed.md` 例外保留 |
| 报告产物无兼容期 | §8.1：产物是审计日志的可再生派生物；兼容边界只在审计日志格式 |
| 聚合类产物只有一条渲染路径 | §5.0：与其造出两条路径再用测试证明其一致，不如让第二条不存在 |
| ViewModel 不落盘 | §5.6：前端要 raw 不要 display，落盘即成第二本账；漂移改用跨语言 fixture 守卫 |
| 看板参数走 `#` 不走 `?` | §6.2：hash 不进服务端，托管方式无关；`?` 把纯前端选择变成服务端输入 |
| `file://` 直开不支持 | §6.2：opaque origin 下 `fetch` 全被拦，无解；给提示页而非静默空白 |
| 托管目录不走 `rundir` | §6.5：`rundir` 是运行期状态的归属，报表是用户产物——要 `cd` 进去 grep、要 gitignore、要按项目分开放，属于项目目录（D16） |
| 详单文件名加 `r-` 前缀 | §1.1：与 `j-<id>` 同一套约定，一个 ID 脱离目录上下文后仍能自述类型（D17） |
| `ctxgraph` 的 md5 内容寻址底座不改 | §7.2：`Hash`/`SysHash`/`Lineage` 根哈希/`ReqHash8` 都是语料内部身份标签，不参与缓存判据；换 sha256 要重算全部 `.parse-cache`、改 `Hash` 宽度、动 Journey ID 形态，收益为零 |
| 版本探测进骨架页样板，不注入用户副本 | §6.6：注入做不到；"复制即继承"让检查随副本传播，自建面板自担风险 |
| 版本不一致只警告不阻断渲染 | §6.6：加性变更下老页面大概率照常渲染；阻断会把可渲染的数据挡在门外 |
| 切片不带独立版本戳，版本查 manifest | §6.6：整套一个版本单位（§8.1）；逐片盖章是 N 份冗余，徒增漂移面 |

### 11.3 顺带机会（不属于本方案，但实施时应留出口子）

`KNOWN_ISSUES` 记录的分析半区真实瓶颈是**内存**（万级记录 GB 级 RSS），
不是草案臆测的"写盘开销"。切片化天然把产物写入拆成了若干段，
如果 Phase 1 的序列化写成流式（逐切片构建、写完即释放），可以顺手削掉一部分峰值。
这不是本方案的目标，但实施时不要把结构写死成"必须全部切片同时驻留内存"。

---

*文档状态：设计提案，待评审*
*跟踪基线：vmr @ main*
