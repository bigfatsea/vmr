# vmr analyze 架构重构方案：领域微切片 JSON + 解耦双轨渲染 + 内容寻址缓存

## 0. 架构重构愿景与核心原则

当前 `vmr analyze`（含过渡别名 `vmr report` / `vmr story`）负责将底层审计日志转化为可读报表与分析数据。经过对代码库与实际使用场景的深入审查，现有体系存在三个结构性矛盾：
1. **数据与展现深度交织**：约 11,836 行展现层代码（7,087 行 Go 渲染逻辑 + 4,749 行 i18n 文本）通过大量的 `strings.Builder` 和 `fmt.Fprintf` 嵌死在 Go 二进制内。版面微调、新增维度或主题定制均需修改 Go 源码并重新编译。
2. **单体大 JSON 缺乏弹性**：`vmr-report.json` 将 16 个完全不同维度的指标强行聚合在单一文件中。关注财务、运维、Prompt 优化的不同角色无法按需获取切片；任何配置微调（如调价）都会导致整个大文件的缓存失效。
3. **概念模型碎片化与冗余**：存在 `Story` 与 `Journey` 的概念双轨重叠；`corpus` 命名晦涩且孤立；存在按客户端生成几十个物理 Markdown 文件等无谓的磁盘膨胀。

针对上述问题，本重构方案确立**四大核心支柱**：

- **领域微切片（Domain Slices）JSON 核心**：打破单体大 JSON，按业务关注点拆分为高度内聚的领域切片文件（财务、稳定性、负载、上下文效率），补齐衍生事实与置信度标注，作为系统唯一自洽的数据源。
- **HTML 动态看板外置化（Web 交互视图）**：外置纯静态模板，内嵌轻量 CSS/JS，通过浏览器直接异步加载一个或多个指定的 JSON 切片（类似 `/status.html` 模式），由 VMR 实例内置 HTTP 托管与 Auth 鉴权；**生成 JSON 即代表看板已生成，实现零渲染开销交付**。
- **Markdown 模板解耦（CLI 文本视图）**：保留 Markdown 输出以捍卫 CLI 终端纯文本与自动化排障体验；引入 **ViewModel（视图模型）** 层化解文本模板与复杂排版逻辑的冲突；支持在 JSON 切片已就绪时实现**免分析秒级重新渲染**。
- **概念模型全面归一**：彻底剔除 `Story` 冗余概念，全量收敛于 **`Journey`（任务轨迹）** 序列；将群体统计 `corpus` 明确重构为 **`Journey Benchmarks`（行为基准与群体洞察）**。
- **内容寻址与多级缓存**：结合请求坐标不可变性、`ctxgraph` 消息哈希向量与配置/模式指纹，建立切片级增量缓存体系，达到“日志未变不分析、配置微调只重算局部切片、数据未变不重绘”。

---

## 1. 现有产出物全景审查与分类重构

### 1.1 现有 22 项产出物全景分类

当前系统生产的所有产出物按数据层级与语义职责可划分为 5 个清晰类别：

```
[Layer 1: 事实层 / 明细层] ────────────────── 原始、不可变、以时间/坐标为序的调用轨迹
  ├── details/*.md                           (单请求深钻：增量消息 + 上游响应)
  ├── vmr-requests.json                      (全量请求明细行集合)
  └── vmr-requests-failed.jsonl              (异常/截断请求机读数据集)

[Layer 2: 宏观聚合层] ──────────────────────── 全量横向扫描后的统计切片（单体捆绑）
  ├── vmr-report.json                        (16 个维度合一的单体 JSON)
  ├── vmr-report.md                          (§0–§8 九个章节的人读 Markdown)
  └── tool-waste.html                        (工具形态利用率单卡，内联数据)

[Layer 3: 任务叙事层 (Journey)] ────────────── 纵向还原任务生命周期的因果链与行为指标
  ├── stories/vmr-stories.json / .md         (Journey 候选集索引与拓扑)
  ├── stories/journey-<id>.json / .md / .html(单任务完整叙事与时间轴看板)
  └── stories/compare-*.json / .md / .html   (双任务 A/B 行为剖面对照)

[Layer 4: 群体基准层 (Benchmarks / Corpus)] ─ 跨任务的群体统计学特征与因果验证
  └── stories/vmr-story-corpus.json / .md    (指标分布、Finding 检出率、Spearman 秩相关)

[Layer 5: 内部缓存层] ──────────────────────── 纯机器内部加速资产（非用户直接交付物）
  ├── .parse-cache/<hash>.json               (单源文件消息解析与哈希分片缓存)
  └── stories/.llm-cache/<key>.json          (LLM 语义推断结果缓存)
```

### 1.2 产出物精简、拆分与补全决策（Keep / Drop / Merge / Split / Add）

通过对业务必要性与使用模式的审查，实施如下重构决策：

| 原产出物 | 决策 | 处置方案与理由 |
|---|:---:|---|
| `vmr-requests-<tag>.md`<br>`vmr-requests-cron-*.md` | **Drop**<br>（彻底删除） | **无谓的磁盘膨胀**。在有多客户端或定时任务的环境下，会在根目录刷出数十个大 Markdown 文件。客户端过滤本应是动态查询，在 HTML 看板中通过下拉筛选完成；CLI 仅提供按需参数输出，不再默认物化落盘。 |
| `stories/` 命名空间 | **Rename & Split**<br>（重构与归类） | 统一为 `journeys/`，彻底消除 `Story` 与 `Journey` 混用；单任务实例下沉入 `journeys/details/`，A/B 对比独立提至根级同级 `compares/`。 |
| `stories/vmr-story-corpus.*` | **Reposition**<br>（重定位） | 迁入 `journeys/benchmarks.json` / `.md`。明确其作为“跨任务评测基准与群体洞察”的定位，破除冷僻的学术命名。 |
| `tool-waste.html` | **Split & Decouple**<br>（解耦补全） | 补齐独立数据切片 `macro/context-efficiency.json`（或 `macro/tools.json`），HTML 改为纯静态模板外置到 `templates/html/`。 |
| `vmr-report.json` | **Decompose**<br>（解构切片） | 拆解为 5 个领域微切片（Summary、Finance、Reliability、Workloads、Context-Efficiency），并保留轻量 `manifest.json`。 |
| `details/*.md` | **Relocate & Symmetry**<br>（下沉与对称化） | 从根目录迁入 `requests/details/req-<hash>.{json|md}`，与 `journeys/details/` 形成同构对称；维持懒物化。 |
| `.parse-cache/`<br>`.llm-cache/` | **Consolidate**<br>（缓存归一化） | 散落在根部和 stories 下的隐藏缓存统一收敛至 `.cache/parse/` 与 `.cache/llm/`，消除散落点状目录。 |

---

## 2. 概念模型统一：Story 归一与 Corpus 深度解读

### 2.1 终结 `Story` 概念冗余，全面统一为 `Journey`

在历史演进中，“`vmr story`”作为 CLI 动词出现，而其内存模型与输出实体一直被命名为“`Journey`”（一条缝合链渲染的连续叙事）。这导致了文件命名与目录结构的混乱：
- 命令叫 `vmr story`，输出在 `stories/`，索引叫 `vmr-stories.json`，但单体文件叫 `journey-<id>.json`，对比叫 `compare-*.json`。

**第一性原理**：系统分析的核心业务对象只有两个——
1. **Request**：原子性的 HTTP 调用事实（微观点）。
2. **Journey**：由用户单次目标驱动、跨越多次轮次、工具调用与上下文压缩的连续任务旅程（中观链）。

**重构后全面废弃 `Story` 术语**：
- 目录全面统一为 **`reports/journeys/`**。
- 索引文件更名为 **`journeys/index.json`**（Markdown 对应 `journeys/index.md`）。
- 单任务轨迹明细下沉至 **`journeys/details/j-<id>.{json|md}`**，避免海量单任务文件在根目录下与索引混杂。
- 双任务对比独立提至根级同级目录 **`reports/compares/`**（对应 `compares/compare-<a>-vs-<b>.{json|md}`），彻底避免被淹没在海量 `j-<id>` 任务文件中。

### 2.2 深度解读：`Corpus`（语料统计）到底是什么？它有用吗？

#### (1) 它到底计算了什么？
`corpus.go` 计算的绝不是简单的求和，而是一套**基于无参数统计学（Non-parametric Statistics）的群体行为分析**：
1. **13 项核心行为指标的全局分布（Distribution）**：为净工作时长（`NetWorkingMS`）、模型耗时比、重复动作率、计划执行比等指标提供全语料级的 `Mean`、`Median`、`Min`、`Max`、`P90`。
2. **缺陷模式命中率（Finding Hit Rates）**：统计“原地打转（`exact_repeat_tool_call`）”、“计划偏离”在所有任务中出现的百分比。
3. **Spearman 秩相关性分析（Spearman's Rho）**：跨任务寻找指标间的因果线索（例如：“重复动作率高的任务，净工作时间是否显著变长？”）。
4. **缺陷效应量分组对比（Group Comparison）**：比较命中某项缺陷的任务组与未命中任务组的中位数耗时差异（实测中，计划偏离组的净耗时中位数高出 38%）。

#### (2) 它有用吗？在什么场景下使用？
- **单次排查（Incident Debugging）场景：无用**。排查昨天某一个任务为什么失败，直接读 `journey-<id>.html` 即可，不需要关心整体分布。
- **Agent 算法评测与 Prompt 调优（Benchmark）场景：极具价值且不可替代**。
  - 当团队更新了系统提示词（System Prompt）、优化了工具描述、或者尝试将主模型从 Claude 切换为开源大模型时，通常会运行几十到几百个标准测试任务。
  - **`corpus` 是全系统唯一能够衡量“整体稳定性是提升了还是退步了”、“异常循环发生率是否下降”的宏观量化依据**。

#### (3) 重构收敛方案
消除学术黑话 `corpus`，将其重塑为 **`Journey Benchmarks`（任务行为基准与群体洞察）**：
- 文件路径定义为 **`journeys/benchmarks.json`** 与 **`journeys/benchmarks.md`**。
- CLI 统一为 `vmr analyze -benchmark`（保持 `-corpus` 兼容别名）。

---

## 3. 单体 JSON 的解构：领域微切片（Domain Slices）设计

### 3.1 为什么必须打破单体聚合？

当前 `Report2`（`rows.go`）将 16 个顶级 Key 揉成一个单一的 `vmr-report.json`。这种设计在规模化后暴露出的瓶颈：
1. **关注点缠绕**：财务人员需要查看花费，安全与质量团队关注工具浪费与 Finding，SRE 团队关注端点可用性与网络延迟。单体结构迫使所有下游消费者处理无用字段。
2. **缓存失效爆炸半径（Cache Invalidation Blast Radius）**：
   - 场景：用户修改了 `config.yaml` 中某个 Provider 的费率覆盖。
   - 此时：底层网络流量、错误分类、延迟分布、会话轮次**完全没有改变**，仅仅是成本（Finance）计算需要调整。在单体结构下，整个大报告被判为过期失效，必须重新全量聚合。
3. **前端异步按需加载受阻**：在开发交互式 Web 看板时，不同 Tab 页若想做到按需渐进式加载，需要服务端或静态存储提供独立的微切片，避免首屏强行下载数百 KB 的全量数据。

### 3.2 领域微切片架构与字段划分

将宏观报表严格按照业务领域拆解为 5 个原子切片，并由一个轻量的 `manifest.json` 进行拓扑连接：

```
reports/
├── manifest.json                  # 全局元数据与切片清单 (Format=11)
├── macro/
│   ├── summary.json               # 核心看板摘要（总体流量、成功率、总支出、全局 Findings）
│   ├── finance.json               # 财务与配额（模型成本、客户端成本、端点成本、额度对照）
│   ├── reliability.json           # 基础设施与健康（端点状态、错误分类、Failover、延迟 P50/P95）
│   ├── workloads.json             # 负载与时空分布（按日期、按小时、客户端-端点归属矩阵）
│   └── context-efficiency.json    # Agent 上下文与效率（会话膨胀、压缩损失、工具利用与浪费）
├── requests/
│   ├── index.json                 # 全量请求索引明细行 (RequestRow[])
│   ├── failed.jsonl               # 失败/截断请求机读流 (Outcome != OK)
│   └── details/                   # 逐请求深钻明细 (从根目录迁入，按需懒物化)
│       ├── req-<hash>.json
│       └── req-<hash>.md
├── journeys/
│   ├── index.json                 # Journey 候选集索引与 Lineages 拓扑
│   ├── benchmarks.json            # 群体任务行为基准与评测洞察 (原 corpus)
│   └── details/                   # 单任务轨迹明细 (下沉收敛，避免海量实例堆积在根部)
│       ├── j-<id>.json            # 单任务全量数据结构 (含 Structure)
│       └── j-<id>.md              # 单任务决策脊柱与事件流
├── compares/                      # 独立任务对照区 (从 journeys/ 提至根级，防海量淹没)
│   ├── index.json                 # 对照记录索引
│   ├── compare-<a>-vs-<b>.json    # 双任务对比差异数据
│   └── compare-<a>-vs-<b>.md      # 双任务对比叙事报告
└── .cache/                        # 统一机器内部加速缓存 (原散落的 .parse-cache 与 .llm-cache 归一)
    ├── parse/                     # 原始日志解析分片缓存 (原 .parse-cache)
    │   └── <hash>.json
    └── llm/                       # LLM 语义解读与推断缓存 (原 .llm-cache)
        └── <key>.json
```

#### 各领域切片的结构体映射表

| 切片文件 | 包含的数据块 (原 Report2 对应字段) | 覆盖核心指标与用途 |
|---|---|---|
| **`manifest.json`** | `Meta`、`Pricing` 元数据、输入文件哈希集合、切片校验指纹 | 运行参数、时区、输入源溯源、格式版本戳 |
| **`macro/summary.json`** | `Overall` (单桶)、`Efficiency` (Finding 列表)、核心亮点字符串 | 决策首屏：总请求、总 Token、综合成功率、核心浪费报警 |
| **`macro/finance.json`** | `ByModel.Cost`、`ByClient.Cost`、`Providers`、`ProviderQuotas`、未定价与降级估算披露 | 成本核算、账单对照、包月配额燃烧速率监控 |
| **`macro/reliability.json`** | `Endpoints`、`EndpointsAll`、`Sticky` (缓存亲和度效果) | 供应商 SLA、端点可用率、Failover 故障耗时、网络延迟分布 |
| **`macro/workloads.json`** | `ByDate`、`Hours`、`HoursOfDay`、`Workloads`、`ClientEndpoints` | 容量规划、业务高峰期探测、客户端路由倾斜度 |
| **`macro/context-efficiency.json`** | `Sessions`、`Compactions`、`Tools` (原 tool-waste 数据源) | 上下文工程优化、历史压缩信息损失、工具 Schema 冗余浪费 |

---

## 4. HTML 外置模板体系与 VMR 自服务

### 4.1 核心工作流：从“构建期生成”到“运行时动态加载”

```
[ vmr analyze 执行期 ]
      │
      ├─► 极速分析并写出: reports/macro/*.json, journeys/*.json
      │   (耗时仅涉及 JSON 序列化，写盘开销从数百 MB 降至数 MB)
      ▼
[ 生成完成: 零 HTML 渲染耗时 ]

                                  │
                                  ▼ (用户通过浏览器访问)

[ 用户消费视图: VMR HTTP / 静态文件打开 ]
      │
      ▼
GET /reports/macro-dashboard.html
      │ (返回纯静态 HTML+CSS+JS 骨架, 零业务数据, 外部模板)
      ▼
浏览器 JS 执行:
      Promise.all([
          fetch('/reports/macro/summary.json'),
          fetch('/reports/macro/finance.json')
      ])
      │
      ▼
客户端动态渲染 DOM / SVG 图表 / 切换主题 (Github Dark / Clean Light)
```

### 4.2 模板规范与外置目录规划

在工程目录中建立独立的 `templates/html/`（或配置指向外部目录），支持系统内置与用户自定义扩展：

```
templates/
└── html/
    ├── macro-dashboard.html       # 宏观综合大屏 (消费 macro/*.json)
    ├── request-browser.html       # 请求检索浏览器 (消费 requests/index.json, 支持客户端筛选并链向 requests/details/)
    ├── journey-viewer.html        # 单任务轨迹还原看板 (消费 journeys/details/j-<id>.json)
    ├── journey-compare.html       # 双任务 A/B 对比看板 (消费 compares/compare-*.json)
    ├── benchmarks.html            # 群体任务行为基准看板 (消费 journeys/benchmarks.json)
    └── tool-waste.html            # 工具 Schema 浪费微看板 (仅消费 macro/context-efficiency.json)
```

#### 模板动态加载规范与开箱即用性
1. **URL 参数自适应**：
   - 访问 `http://localhost:8800/reports/journey-viewer.html?data=journeys/details/j-lobster-01.json`，JS 自动获取指定参数并渲染。
   - 访问 `http://localhost:8800/reports/journey-compare.html?data=compares/compare-j-01-vs-j-02.json`，JS 自动加载双边差异对比。
   - 缺省参数时，页面按约定自动寻找默认切片（如 `macro-dashboard.html` 自动请求 `./macro/summary.json`）。
2. **零编译扩展**：
   - 用户若需要定制一个专属的“财务报表大屏”，只需复制 `macro-dashboard.html` 为 `finance-dashboard.html`，使用标准 HTML/JS 编写定制图表，直接调用现成的 `/reports/macro/finance.json`，无需触碰任何一行 Go 源码。

### 4.3 VMR 实例服务与安全屏障

- **服务托管**：`server.go` 挂载 `GET /reports/*`，对外统一代理静态模板与生成的报告切片。
- **权限安全边界（对齐 `/status.html`）**：
  - HTML 模板文件为公开骨架，不包含对话机密；
  - **所有底层 JSON 切片请求强制经过 `s.auth()` 守口**：当 VMR 配置了 `api_keys` 时，未授权的请求将被拦截为 401，浏览器弹出凭证提示，**严格捍卫审计数据 `0600`/`0700` 的物理安全性**。

---

## 5. Markdown 渲染重构：ViewModel 驱动的解耦与秒级重绘

保留 Markdown 是捍卫 CLI 开发者体验（终端直接 `cat` / `grep` / 编辑器阅读）的底线要求。但必须将其实现从 Go 硬编码拼接中彻底解放。

### 5.1 架构分层：从硬编码拼接走向 ViewModel

```
[ 领域微切片 JSON ] (macro/*.json, journeys/j-*.json)
       │
       ▼ (Pure Go: Type-safe, Localized, Unit-tested)
[ ViewModel 构建器 ] (把原始数字转为排版就绪的纯文本结构)
       │
       ▼
[ ReportViewModel / JourneyViewModel ]
(包含: 排版好的字符串、对齐方式、转义完成的表格行、本地化章节标题)
       │
       ├──────────────────────────────────────────┐
       ▼                                          ▼
[ 内置极速 Markdown 渲染器 ]             [ 外置 Markdown 模板引擎 ]
(零外部依赖, 格式严谨, 工业级对齐)       (templates/md/*.md.tmpl, 用户自由排版)
       │                                          │
       └────────────────────┬─────────────────────┘
                            ▼
                    [ 最终 *.md 报表 ]
```

### 5.2 外置 Markdown 模板形态

外置模板（`templates/md/macro-report.md.tmpl`）中将不再包含复杂的业务逻辑，仅保留结构排版标记：

```markdown
# {{ .Title }}

> 生成时间: {{ .GeneratedAt }} | 统计范围: {{ .TimeRange }}

## 0. 核心亮点
{{ range .Highlights }}
- {{ . }}
{{ end }}

## 1. 成本与 Token 经济
{{ renderTable .CostAndTokensTable }}

## 2. 供应商与配额状态
{{ renderTable .ProviderQuotaTable }}

{{ range .Disclaimers }}
> ⚠️ {{ . }}
{{ end }}
```

### 5.3 “JSON 存在即秒级重绘”机制

解耦后的关键收益是引入 `-render-only` 模式：
```bash
vmr analyze -render-only [-o reports]
```
- **执行逻辑**：检测到 `reports/manifest.json` 及各切片存在，**跳过所有底层日志的解压、词法分析、消息哈希与拓扑缝合**。
- **耗时表现**：直接读取各微切片 JSON，在内存中构建 ViewModel 并套入模板，**全流程在 50ms 内完成**。
- **应用场景**：修改模板布局、切换中文/英文输出（`-lang zh`）、调整打印列顺序时，无需再经历数分钟的日志遍历。

---

## 6. 内容寻址与微切片级增量缓存

### 6.1 缓存设计的第一性原理

用户提出通过“文件名/起止时间/请求数”判断缓存。在真实 Agent 日志中，存在以下边缘场景使得单纯的时间/数量判定失效：
1. 压缩或截断导致请求总数未变，但历史消息被改写。
2. 上游健康故障导致 Failover，总调用轮数一致，但端点与成本产生重大变化。
3. 用户更新了 `config.yaml` 的 Pricing 费率，日志记录未变，但计算金额已变。

### 6.2 三级内容寻址缓存体系

充分利用系统已有的 `ctxgraph` 内容哈希资产，构建切片级精准失效模型：

```
[ Level 1: 原始日志分片与 LLM 缓存 ] (整合收敛: .cache/parse/<filehash>.json 与 .cache/llm/<key>.json)
  - 依赖: 原始审计文件内容 SHA256 / LLM 模型+Prompt 指纹
  - 作用: 跳过原始行解析与消息哈希计算、跳过 LLM 重复推断

[ Level 2: 领域微切片产物缓存 ] (核心演进: reports/macro/*.json, journeys/details/j-*.json, compares/*.json)
  - 切片级指纹隔离:
    * macro/finance.json 指纹: Hash(InputsHashes + PricingConfigHash + FormatVersion)
    * macro/reliability.json 指纹: Hash(InputsHashes + FormatVersion)  <-- 与 Pricing 无关!
  - 收益: 调整价格时，仅重新计算 finance.json；稳定性与负载切片 100% 缓存命中!

[ Level 3: 渲染表现层缓存 ] (产物: *.md, requests/details/*.md, journeys/details/*.md)
  - 依赖: Hash(ViewModelHash + TemplateFileHash + Lang)
  - 收益: 模板未修改且数据未变，跳过磁盘写操作
```

---

## 7. 目标目录拓扑规范

实施本重构后，一次标准 `vmr analyze` 的工作目录资产全景：

```
reports/
├── manifest.json                     # 全局元数据与切片索引 (Format=11)
├── vmr-report.md                     # 由 macro/*.json 套模板渲染的宏观报告
├── macro/                            # 宏观领域微切片数据集
│   ├── summary.json                  # 核心概览与 Findings
│   ├── finance.json                  # 财务成本与配额对照
│   ├── reliability.json              # 端点可用性、Failover与延迟
│   ├── workloads.json                # 业务负载与时空分布
│   └── context-efficiency.json       # 上下文生命周期与工具利用率
├── requests/                         # 明细事实层
│   ├── index.json                    # 全量请求索引行 (供 request-browser.html 检索)
│   ├── index.md                      # 请求汇总导航 (轻量总览，不再按客户端刷盘几十个文件)
│   ├── failed.jsonl                  # 失败请求异常数据集
│   └── details/                      # 逐请求深钻明细 (从根部迁入，按需懒物化)
│       ├── req-<hash>.json
│       └── req-<hash>.md
├── journeys/                         # 任务轨迹层 (彻底剔除 Story 命名)
│   ├── index.json                    # Journey 候选集拓扑清单
│   ├── index.md                      # 任务人读清单
│   ├── benchmarks.json               # 跨任务评测基准与群体统计 (原 corpus)
│   ├── benchmarks.md                 # 评测基准报表
│   └── details/                      # 单任务轨迹明细 (下沉归纳，避免淹没根部)
│       ├── j-<id>.json               # 单任务全量机读数据 (含 Structure)
│       └── j-<id>.md                 # 单任务决策脊柱与事件流
├── compares/                         # 独立任务对照区 (从 journeys/ 提至根部同级，防海量淹没)
│   ├── index.json                    # 对照记录索引
│   ├── index.md                      # 对照人读清单
│   ├── compare-<a>-vs-<b>.json       # 双任务对比差异数据
│   └── compare-<a>-vs-<b>.md         # 双任务对比叙事报告
└── .cache/                           # 统一机器内部加速缓存 (隐藏目录收敛)
    ├── parse/                        # 原始日志解析分片缓存 (原 .parse-cache)
    │   └── <hash>.json
    └── llm/                          # LLM 语义解读与推断缓存 (原 .llm-cache)
        └── <key>.json
```

---

## 8. 实施路线图与 ROI 评估

### 8.1 四阶段演进路线

```
Phase 1: 数据模型解构与拓扑命名归一 (3~4 人天)
  ├── 将 Report2 解构为 5 个领域微切片结构体并定义 manifest.json
  ├── 全面清理 Story 遗留命名，确立 journeys/ 与根级 compares/ 拓扑
  ├── 建立 requests/details/ 与 journeys/details/ 对称明细目录
  ├── 将散落缓存统一整合收敛至 .cache/parse/ 与 .cache/llm/
  ├── 删除 vmr-requests-<tag>.md 物理文件生成逻辑
  └── 补齐最后 5% 的统计置信度与合计披露结构化字段

Phase 2: HTML 外置看板开发与自服务挂载 (4~5 人天)
  ├── 设立 templates/html/ 目录，编写纯静态无依赖的前端看板
  ├── 实现 URL 参数动态加载切片机制 (?data=...)
  └── 在 server.go 中实现 GET /reports/* 静态代理与 Auth 鉴权

Phase 3: Markdown ViewModel 分离与外置模板化 (4~5 人天)
  ├── 提取各切片的 ViewModel 生成逻辑（类型安全纯函数）
  ├── 引入 templates/md/ 外置模板机制与内置降级渲染器
  └── 实现 CLI 开关: vmr analyze -render-only（50ms 级免分析重绘）

Phase 4: 微切片级增量缓存闭环 (3~4 人天)
  ├── 建立切片级缓存指纹验证与隔离机制（定价变动仅失效财务切片）
  └── 完善缓存自愈与 Schema 版本自失效逻辑
```

### 8.2 最终 ROI 与技术价值裁决

| 评估维度 | 传统硬编码现状 | 本重构方案（领域微切片 + 解耦双轨） |
|---|---|---|
| **模板定制性** | 极差（任何排版改动必须改 Go 源码并重新编译） | **极佳**（HTML 与 Markdown 模板完全外置，前端支持即时换肤与多屏定制） |
| **产物文件整洁度** | 差（平铺在根目录，按 Client 刷出数十个废文件） | **极佳**（清晰的分层子目录，消除冗余派生文件） |
| **增量重绘性能** | 差（微调排版需重新遍历分析数分钟日志） | **极佳**（`-render-only` 模式下直接消费切片，50ms 内完成渲染） |
| **缓存失效率** | 粗放（任何配置改动全盘失效重跑） | **精准**（切片级指纹隔离，仅重算受影响的领域微切片） |
| **概念认知摩擦** | 存在 Story / Journey / Corpus 多头概念重叠 | **清晰统一**：全局仅有 Request（点）与 Journey（链）两大认知概念 |
| **研发投入** | - | **总计约 14~18 人天**（风险低、分阶段平滑演进） |
| **最终裁决** | - | **高性价比投资，彻底奠定 vmr 分析层的工业级可维护底座** |

---

*文档状态：架构设计终稿（Target Architecture Specification）*
*版本：v4.3 Analytics Domain-Slices Redesign*
*跟踪代码基线：vmr @ main*
