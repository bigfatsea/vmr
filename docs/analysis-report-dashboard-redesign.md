# vmr analyze 架构重构设计：JSON 数据核心 + 双轨解耦渲染 + 内容寻址缓存

## 0. 设计目标与核心原则

当前 `vmr analyze`（含过渡别名 `vmr report` / `vmr story`）同时生产结构化 JSON 与人读 Markdown 报表。随着系统演进，当前实现暴露出两个核心痛点：
1. **渲染逻辑与 Go 代码高度耦合**：Markdown 渲染代码混杂在 Go 代码中（约 7,087 行渲染逻辑 + 4,749 行 i18n 文本），通过大量的 `strings.Builder` 和 `fmt.Fprintf` 手工拼接，样式微调与版面扩展成本极高。
2. **重算成本高、缺乏产物级缓存**：每次调整报告格式或重新分析时，即使底层审计数据未变，仍需重新遍历聚合整个日志集合；数据层与展示层紧密绑定，无法仅针对已有数据重新“换肤”或套用新模板。

本方案基于**第一性原理**与**现代数据分析架构最佳实践**，对 `vmr analyze` 的生成与展示架构进行全面重构，确立四大核心支柱：

- **以 JSON 为绝对数据核心**：全量指标、事实切片、告警标签、统计置信度一律下沉为不可变的标准化 JSON 数据，消除“在渲染层临时发明数据”的漏洞。
- **HTML 动态看板层（Web 交互视图）**：外置纯前端模板，内嵌轻量 CSS/JS，通过浏览器直接异步加载 JSON 数据（类似 `/status.html` 模式），由 VMR 实例提供默认 HTTP 托管与权限保护，支持多主题、多视角与用户自定义看板。
- **Markdown 模板解耦（CLI 文本视图）**：保留 Markdown 以维持终端纯文本消费生态，但将其从 Go 源代码中抽离为外置模板；通过引入 **ViewModel（视图模型）** 层化解文本模板表达力不足与排版脆弱的矛盾；支持在 JSON 已就绪的前提下免分析秒级重新渲染。
- **基于内容寻址的双版本缓存机制**：融合请求坐标不可变性、`ctxgraph` 消息指纹向量与配置/模式哈希，建立严谨的增量缓存模型，实现“日志未变不重算、数据未变不重绘”。

---

## 1. 当前产出物与代码耦合现状清单

### 1.1 产出物完整清单（22 项）

| # | 产出物路径 | 格式 | 数据来源 | 语义职责 |
|---|-----------|------|---------|---------|
| 1 | `vmr-report.json` | JSON | `Report2`（schema format=10） | 宏观全量聚合数据（流量、成本、端点、配额、效率发现） |
| 2 | `vmr-report.md` | Markdown | §0–§8 九个章节 | 宏观全量人读报告 |
| 3 | `vmr-requests.json` | JSON | `RequestRow[]` | 逐请求明细行聚合（按时间有序） |
| 4 | `vmr-requests.md` | Markdown | 按 client 分组索引 | 请求全局人读索引 |
| 5 | `vmr-requests-<tag>.md` | Markdown | 按特定 client 过滤 | 客户端专属请求索引 |
| 6 | `vmr-requests-cron-<class>.md`| Markdown | 心跳/定时分类 | 定时脚手架请求索引 |
| 7 | `vmr-requests-failed.jsonl`| JSONL | 异常切片过滤 | 失败/截断请求机读数据集 |
| 8 | `vmr-requests-failed.md` | Markdown | 异常切片索引 | 失败分析与排障入口 |
| 9 | `details/*.md` | Markdown | 单条 `audit.Record` | 逐请求深钻详单（增量消息 + 上游响应） |
| 10 | `tool-waste.html` | HTML | `ToolShapeRow[]` | 独立自包含工具形态浪费卡片 |
| 11 | `.parse-cache/<hash>.json` | JSON | `ctxgraph.FileCache` | 单源文件消息解析与哈希分片缓存 |
| 12 | `stories/vmr-stories.json` | JSON | `StoryIndex` | Journey 候选集机读索引与 Lineages 拓扑 |
| 13 | `stories/vmr-stories.md` | Markdown | 同上 | Journey 候选集人读导航列表 |
| 14 | `stories/journey-<id>.json` | JSON | `JourneySummary` | 单 Journey 完整数据（含指标、Findings、Structure） |
| 15 | `stories/journey-<id>.md` | Markdown | 决策脊柱+事件流+Findings | 单任务纵向执行还原叙事 |
| 16 | `stories/journey-<id>.html` | HTML | `RenderHTML()` | 单任务可视化单页看板（内联数据型） |
| 17 | `stories/compare-*.json` | JSON | `Comparison` | 双 Journey A/B 行为剖面对比数据 |
| 18 | `stories/compare-*.md` | Markdown | 对比表格+分叉点+LLM 解读 | 双任务横向对比叙事 |
| 19 | `stories/compare-*.html` | HTML | `RenderComparisonHTML()` | 双任务对比可视化单页看板 |
| 20 | `stories/vmr-story-corpus.json`| JSON | `CorpusStats` | 语料级聚合统计（Spearman 秩相关、分组对比） |
| 21 | `stories/vmr-story-corpus.md` | Markdown | 同上 | 语料级统计人读报表 |
| 22 | `stories/.llm-cache/<key>.json`| JSON | LLM 语义解读缓存 | 模型推断与分叉点解释缓存 |

### 1.2 代码量分布与耦合痛点

当前系统中与展现相关的 Go 源代码规模：

| 代码分区 | 文件数量 | 纯代码行数（排除测试） | 承担职责 |
|---------|---------|---------------------|---------|
| `internal/report/` 渲染层 | 16 个 | **4,012** | `section_*.go` 章节排版、`render_cells.go` 单元格拼装、`requests.go` 索引生成 |
| `internal/story/` 渲染层 | 14 个 | **3,075** | `render_spine*.go` 决策脊柱、`render_html*.go` HTML 生成、`render_compare*.go` |
| `internal/i18n/` 双语文本 | 31 个 | **4,749** | 中英文 struct 静态文本与插值拼句函数 |
| **展现层合计** | **61 个** | **11,836** | 占整个 Analytics 半区代码量的一半以上 |

**核心痛点**：
1. **硬编码字符串拼接**：章节表格均使用 `w("| %s | %s |\n", ...)` 循环写入，修改列宽、列序、增加修饰样式必须重新编译整个 Go 二进制。
2. **逻辑与表现缠绕**：诸如“样本量低于 20 时追加 `⚠️low-n`”、“覆盖率低于 90% 时加脚注 `¹`”、“工具参数按 AST 深度截断”等本属于业务或表现层的判断，深度交织在字符流输出中。
3. **计算与渲染不可分割**：无法实现“只读现有 JSON，仅套用新版模板重新生成 Markdown”，只要运行 `vmr analyze`，就必须执行一遍昂贵的数据提取与聚合流程。

---

## 2. JSON 数据核心的完整性审查与 Schema 演进

实现渲染解耦的前提是：**JSON 数据层必须是完全自洽、无信息损失的单一真实数据源（Single Source of Truth）**。

### 2.1 数据完整性审查现状

经过对当前代码库的全面对比核验，`Report2` 和 `JourneySummary` 的数据覆盖率已经达到 95% 以上：
- `rows.go` 的 `Report2`（format=10）完整定义了宏观分桶与派生指标。
- `structure.go` 的 `JourneyStructure`（P4 引入）已经实现了对 `render_spine.go` 人读决策脊柱全部事实的机读对齐（包含 Task/Step 拓扑、有界参数截断、图编辑类型、缝合边界、Compaction 吞噬实体、配对状态）。`TestBuildStructure_LosslessReconstruction` 回归测试锁定了其无损性。

### 2.2 需下沉至 JSON 的“最后 5%”缺口清单

当前仍有少量关键信息仅在 Go 渲染代码（`render_cells.go` / `render_doc.go`）中即时计算，必须补齐至 JSON Schema 中：

1. **统计置信度标记（Confidence Flags）**：
   - 现存逻辑：`cacheEffCell` 在 `basis/total < 0.9` 时动态加 `¹`；`ppCell` 在 `n < 20` 时动态加 `⚠️low-n`。
   - JSON 演进：在相关 Row 与 TrafficStats 中显式暴露 `TokensCoveragePct float64` 与 `DurLowN bool`，由数据生成侧确定性填充，模板层仅按布尔值或阈值决定渲染样式。
2. **合计行豁免与缺失披露（Omission Disclosures）**：
   - 现存逻辑：`renderCostByEndpoint` 中对于未定价端点行、降级估算端点行、分量不全端点的统计汇总与免责说明，由 Go 遍历切片后生成文本。
   - JSON 演进：在 `Report2.Pricing` 或各自分桶内增加 `CostCoverage` 结构体，显式列出 `UnpricedCount`、`IncompleteRateCount`、`DegradedEstimatePct`。
3. **免责声明与动态脚注内容（Footnotes Registry）**：
   - 现存逻辑：Markdown 末尾根据当前分桶命中情况打印的 `¹`、`⭐` 释义文本。
   - JSON 演进：在 `Report2.Meta` 中引入结构化的 `Disclaimers []string` 与 `Footnotes map[string]string`，确保下游消费方知晓每一个标记的权威解释。

---

## 3. HTML 动态看板与 VMR 自服务体系

### 3.1 架构模式：从“内嵌静态页”到“外置动态看板”

现有的 `journey-<id>.html` 与 `/status.html` 验证了自包含前端页面的可行性。重构后将其升维为一套**外置化、模板化、自服务的完整 Web 体系**。

```
项目工作区 / 安装目录
├── templates/
│   └── html/
│       ├── macro-dashboard.html       # 宏观运营大屏（绑定 vmr-report.json）
│       ├── request-browser.html       # 请求明细检索器（绑定 vmr-requests.json）
│       ├── journey-viewer.html        # 单任务执行时间轴看板（绑定 journey-<id>.json）
│       ├── compare-viewer.html        # 双任务 A/B 对照看板（绑定 compare-*.json）
│       └── tool-waste.html            # 工具形态利用率卡片
└── reports/                           # analyze 输出目录
    ├── vmr-report.json
    ├── vmr-requests.json
    └── stories/
        ├── vmr-stories.json
        └── journey-j-xxxx.json
```

### 3.2 HTML 模板规范与工作流

1. **静态骨架与零依赖**：
   - 每一个 `.html` 文件是一个纯静态的前端页面，内联基础 CSS 变量系统与核心 JS 交互逻辑（支持 Dark/Light 响应式主题切换），无外部 CDN 依赖，在离线与内网环境下开箱即用。
2. **动态数据加载协议（URL Auto-Discovery）**：
   - 页面启动时解析 URL 查询参数加载指定数据：
     `http://localhost:8800/reports/journey-viewer.html?data=stories/journey-j-lobster-01.json`
   - 未指定参数时，默认探测同级或标准约定的 JSON 文件（例如 `macro-dashboard.html` 默认加载 `./vmr-report.json`）。
3. **“生成即交付”的零开销特性**：
   - **`vmr analyze` 运行期间完全不需要生成或转录庞大的 HTML 文件**。模板文件作为静态资产常驻于 `templates/html/` 或嵌入二进制。
   - 分析命令只需要专心生成高性能的 JSON 数据文件。只要 JSON 落盘，前端看板即可通过浏览器访问实时刷新呈现。
4. **多主题与用户自定义扩展**：
   - 支持通过 CSS Variables 注入主题包（如 Github Dark、Monokai、Corporate Clean）。
   - 高级用户可以在自定义目录编写特定业务维度的 HTML 模板（例如“Prompt 成本核算专属看板”），只需调用相同的 JSON 字段即可完成呈现，无需向 VMR 提交任何 Go 代码。

### 3.3 VMR 实例自服务与安全隔离

```
+-------------------------------------------------------------+
|                     VMR HTTP Server                         |
|                                                             |
|  [Chat Protocols]         [Admin/Ops]         [Analytics]   |
|  POST /v1/messages        GET /status         GET /reports/ |
|  POST /v1/chat/completions GET /status.html         │       |
+-----------------------------------------------------┼-------+
                                                      │
                       +------------------------------+
                       │
                       ▼
           [Auth Guard (Same as /status)]
                       │
           +-----------┴-----------+
           │                       │
           ▼                       ▼
    Static Templates         JSON Data Files
    (templates/html/*.html)  (reports/*.json)
    [Public Shell]           [Auth-Gated 0600 Data]
```

1. **路由挂载**：
   - `server.go` 增加 `/reports/` 路由组，直接映射到当前配置的 `output` 目录与模板目录。
2. **安全隔离机制（对齐 `/status.html`）**：
   - 审计日志及派生报表包含敏感的对话正文与系统提示词，物理磁盘权限强制保持 `0600`（文件）/ `0700`（目录）。
   - HTML 模板文件本身不包含任何敏感业务数据，可对外公开（或随二进制只读嵌入）。
   - **JSON 数据接口强制通过 `s.auth()` 保护**：HTML 页面加载后，JS 发起 `fetch('vmr-report.json')` 请求，如果 VMR 配置了 `api_keys`，浏览器会触发 HTTP Basic/Bearer 认证或由前端弹出 Token 填报框，完全避免未授权的数据泄露。

---

## 4. Markdown 渲染重构：ViewModel 驱动的外置模板系统

用户明确要求保留 Markdown 输出以维系 CLI 终端体验，但同时希望**将模板内容从 Go 编译单元中剥离**，允许像 HTML 一样进行独立维护与更新。

### 4.1 传统文本模板的缺陷与第一性原理剖析

如果直接将现有 Go 代码中的 Markdown 逻辑粗暴移植为 Go `text/template`，会导致新的技术灾难：
- **逻辑表达受限**：Go `text/template` 缺少复杂控制流与临时变量运算能力。遇到复杂的决策脊柱参数智能截断、多阶标签计算时，模板将变得比 Go 代码更加晦涩，且调试极其困难。
- **表格排版与转义脆弱**：Markdown 表格对 `|` 分隔符和换行非常敏感，文本模板的空格/缩进微调极易破坏表格格式。
- **运行时崩溃风险**：Go 代码具有编译期强类型检查，模板变量拼错只会导致运行时报错或静默渲染空字符串。

### 4.2 破局方案：ViewModel（视图模型）分层架构

解决此矛盾的最佳实践是引入 **Presentation Model / ViewModel 分层**，将渲染拆解为两条正交管道：

```
[ Domain JSON ] (Report2 / JourneySummary)
       │
       ▼ (Pure Go Functions: Type-safe, Localized, Unit-tested)
[ ViewModel ] (扁平化、无歧义、排版就绪的纯文本数据模型)
       │
       ├──────────────────────────────────────────┐
       ▼                                          ▼
[ Standard Go Markdown Formatter ]        [ External Template Engine ]
(内置稳定渲染器: 零配置, 极速)             (templates/md/*.tmpl: 用户完全定制)
       │                                          │
       └────────────────────┬─────────────────────┘
                            ▼
                    [ Output *.md ]
```

#### ViewModel 的结构设计

ViewModel 负责吸收所有复杂的业务格式化、本地化文本查找与排版逻辑，产出完全由“字符串、表格对象、显隐开关”构成的平坦结构：

```go
type TableViewModel struct {
    Headers []string
    Aligns  []string      // "left", "right", "center"
    Rows    [][]string    // 已经过 EscapeCell, 百分比与货币已排版完毕
    Footers []string
}

type MacroReportViewModel struct {
    Title          string
    GeneratedAt    string
    Highlights     []string
    OverallTable   TableViewModel
    CostTables     map[string]TableViewModel
    Disclaimers    []string
    Sections       []SectionViewModel
}
```

#### 外置 Markdown 模板的优雅落地

有了 ViewModel 之后，外部模板变得极为干净，只关注版面组织，完全不需要在模板内部进行复杂的数学计算与条件推导：

```markdown
# {{ .Title }}

> {{ .GeneratedAt }}

## 核心亮点
{{ range .Highlights }}
- {{ . }}
{{ end }}

## §1 成本与 Token 经济
{{ renderTable .OverallTable }}

## §2 成本估算
{{ renderTable .CostTables.ByModel }}

{{ range .Disclaimers }}
> ⚠️ {{ . }}
{{ end }}
```

### 4.3 “JSON 驱动的秒级重绘”

这一分层为“模板更新”带来了质的飞跃：
- **免分析重绘**：当用户修改了 `templates/md/report.md.tmpl` 的排版或语言配置时，只需执行：
  ```bash
  vmr analyze -render-only [-o reports]
  ```
- 系统检测到 `reports/vmr-report.json` 已经存在，**跳过全部底层日志解压、扫描、哈希与建图过程**（这部分通常消耗 10~60 秒），仅执行：
  `JSON Read -> BuildViewModel -> Template Render -> Write Markdown`
- **整个过程耗时控制在 50ms 以内**，真正实现数据与展现的彻底解耦。

---

## 5. 增量缓存机制的深度设计：内容寻址指纹与双版本控制

用户提出利用“标题/起止时间/Request 数量作为 Key”来实现缓存，以避免重复生成。这是一个极具价值的业务直觉。我们从第一性原理对其进行深化和严谨化。

### 5.1 边缘场景分析与为何需要“指纹向量”

在 Agent 的高动态交互场景中，仅凭“起止时间 + Request 数量”作为缓存唯一依据存在以下致命缺陷：
1. **压缩与截断的非单调性**：Agent 发生 Compaction（历史压缩）时，会话步数可能减少，或者在同一时间段内分岔重试，导致请求数量和时间完全一致，但正文与调用轨迹已彻底改变。
2. **上游 Failover 的同构隐匿性**：同一个 Journey 的请求总数与时间不变，但由于上游故障冷却，某次尝试从 Claude 切换到了 DeepSeek，指标发生剧烈变化。
3. **定价与规则配置变更**：审计记录本身未变，但用户在 `config.yaml` 中修改了 Pricing 覆盖费率或更新了额度周期，此时如果直接复用旧 JSON，会展示完全错误的成本数据。

### 5.2 第一性原理缓存模型：三级内容寻址机制

充分复用 `vmr` 底层现有的 `ctxgraph` 内容寻址基础设施，构建真正的工业级缓存体系：

```
[ Level 1: 原文分片解析缓存 ] (已有: .parse-cache/<filehash>.json)
  - 依赖: 原始 audit 文件 sha256 + 解析器版本
  - 价值: 跳过 JSON 行反序列化与消息哈希计算

[ Level 2: 核心数据产物缓存 ] (核心演进: vmr-report.json, journey-*.json)
  - 依赖: 数据指纹 Digest(InputsHashes + ConfigHash + FormatVersion)
  - 价值: 跳过会话分组、Lineage 拓扑构建与指标聚合，重跑直接命中完整数据

[ Level 3: 表现层视图缓存 ] (新增: *.md, details/*.md)
  - 依赖: 视图指纹 Digest(DataJSONHash + TemplateHash + Language)
  - 价值: 模板或数据任一未变，跳过磁盘写与字符串渲染
```

#### 指纹计算算法规范

1. **单个 Request 详单缓存**：
   - Key: `ReqCoord` (`CanonicalPath:Line`)
   - 验证指纹: `Digest(RecordRawBytes + DetailTemplateVersion)`
   - 保证单条请求详单在文件未修改时绝对不可变。

2. **单任务 Journey 缓存 (`journey-<id>.json`)**：
   - 身份 Key: `Journey.ID`（已经是 `"j-" + RootHash[:8] + ...`，天然具备内容寻址特征）。
   - 数据指纹（Data Fingerprint）：
     $$\text{Fingerprint} = \text{MD5}\Big(\bigoplus_{s \in \text{Steps}} \text{ManifestHash}(s) \parallel \text{PricingVersion} \parallel \text{FormatVersion}\Big)$$
   - 如果目标目录已存在 `journey-<id>.json`，且内部存储的指纹与当前内存重建的轻量级链式指纹完全相等，**直接跳过全量 Step 事实回捞与指标重算，直接复用文件**。

3. **宏观全局报表缓存 (`vmr-report.json`)**：
   - 在 `vmr-report.json` 的 `meta` 块中固化生成特征：
     ```json
     {
       "meta": {
         "format": 10,
         "cache_fingerprint": "a3f8c9...",
         "inputs_fingerprint": "8b2d1e...",
         "config_fingerprint": "c7a40f..."
       }
     }
     ```
   - 再次运行时，快速 stat 输入文件的 `Size + MTime` 组合（毫秒级判断）；若命中则比对指纹，一致则直接宣告缓存有效。

---

## 6. 实施路线图、改造工作量与 ROI 深度重估

结合用户调整后的目标（**不丢弃 Markdown，而是双轨化 + 外置解耦 + 数据夯实 + 智能缓存**），整体项目的性质从一次“破坏性重写”转变为一次“高扩展性的架构演进”。

### 6.1 阶段实施计划 (4 阶段)

```
Phase 1: 数据核心闭环 (2~3 人天)
  ├── 补齐 rows.go / structure.go 的披露与置信度字段
  ├── 差分测试确保 JSON 包含 Markdown 所需的一切事实
  └── 为 Report2 和 JourneySummary 固化 cache_fingerprint 字段

Phase 2: HTML 看板层外置与服务托管 (4~5 人天)
  ├── 确立 templates/html/ 目录与通用静态前端框架
  ├── 编写 macro-dashboard.html 与 journey-viewer.html
  ├── 前端接入 URL 动态加载参数 (?data=...)
  └── server.go 挂载 GET /reports/ 静态路由与 Auth 鉴权

Phase 3: Markdown ViewModel 分离与模板化 (5~7 人天)
  ├── 提取 report.ViewModel 与 story.ViewModel
  ├── 将现有的 section_*.go 逻辑重构为 ViewModel 填充函数
  ├── 引入 templates/md/ 外置 Markdown 模板机制
  └── 新增 CLI 开关: vmr analyze -render-only

Phase 4: 产物级智能缓存闭环 (3~4 人天)
  ├── 接入基于文件指纹与配置指纹的有效性判定
  ├── 实现 JSON 产物复用逻辑（命中则跳过分析流）
  └── 详单懒物化与模板版本智能重绘联动
```

### 6.2 综合 ROI 评估

| 评估维度 | 原始方案（彻底移除 Markdown，全量 HTML 替换） | 当前新方案（双轨并存，ViewModel 驱动，模板外置，数据核心） |
| :--- | :--- | :--- |
| **研发投入** | 35~52 人天（极高风险） | **14~19 人天（中等，低风险渐进演进）** |
| **CLI 体验** | **严重倒退**（丢失终端文本阅读与 grep 排障能力） | **大幅增强**（秒级重绘，可定制排版，文本体验保留） |
| **Web 体验** | 获得完整看板 | 获得完整看板，且支持多主题、URL 挂载与跨机器分享 |
| **可维护性** | 仅将复杂性从 Go 移到了 JS（复杂度转移） | **真正的复杂度降低**：Go 专心算数据，模板专心调样式 |
| **性能收益** | 一般 | **极大突破**：二次分析与排版秒级完成（增量缓存收益） |
| **综合裁决** | **否决（ROI 为负）** | **强烈推荐执行（ROI 极高）** |

---

## 7. 架构约束与不变量守护

在推进本重构时，以下核心项目原则必须受到严格保护，不得因架构重组而破损：

1. **两半区一条契约（`report` / `story` 边界）**：
   - `internal/report` 与 `internal/story` 仍保持零相互依赖，`archtest` 行数预算与模块依赖规则保持强制有效。
   - ViewModel 的引入必须各自内聚于各自的包中（如 `internal/report/viewmodel.go`），不得引入跨包共享的通用胖对象。
2. **不可篡改的内容寻址底座**：
   - 缓存机制必须建立在 `ctxgraph` 的不可变消息哈希向量之上，绝不能回退为不可靠的启发式文件时间猜测。
3. **数据敏感性与权限底线**：
   - `reports/` 目录下生成的所有机读与人读资产，文件权限必须严格维持 `0600`，目录权限必须维持 `0700`。
   - VMR HTTP 实例暴露的 `/reports/` 访问点，凡是读取 JSON 真实正文的操作，必须强行受 `s.auth()` 守护，杜绝对话泄漏。

---

*文档状态：已就绪（Current Architecture Target）*
*版本：v4.2 Analytics Redesign*
*跟踪代码基线：vmr @ main*
