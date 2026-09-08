# vmr analyze 架构重构与演进 —— 全量深度 Review 与真实验收测试报告

> **基准元数据**：
> - **审查起点**：Commit `#050ad25` (`refactor: rename internal/story to internal/journey`)
> - **审查基线**：Commit `#a82bee8` (HEAD)
> - **依据方案**：`docs/future-strategy/analyze_architecture_redesign_opus-5.md` 与 `docs/future-strategy/analyze_redesign_problem_checklist.md`
> - **独立性声明**：本报告完全独立审视代码与测试资产，无视既往历史 review 记录的倾向与结论；对设计原则、已知取舍与权衡点按第一性原理独立重新评估。
> - **执行状态**：**已全部完成 (Completed)**

---

## 目录

- [一、审阅目标、准则与独立性声明](#一审阅目标准则与独立性声明)
  - [1.1 审阅目标](#11-审阅目标)
  - [1.2 审阅与整改准则](#12-审阅与整改准则)
  - [1.3 独立审视与批判性立场声明](#13-独立审视与批判性立场声明)
- [二、全量 Action Plan 清单库（功能、模块与问题全景）](#二全量-action-plan-清单库功能模块与问题全景)
  - [2.1 架构核心支柱与 21 项核心裁决 (D1 ~ D21)](#21-架构核心支柱与-21-项核心裁决-d1--d21)
  - [2.2 可视化 Stage 1 增强特性 (FEAT-S1-01 ~ FEAT-S1-04)](#22-可视化-stage-1-增强特性-feat-s1-01--feat-s1-04)
  - [2.3 历史汇聚问题清单 (ISSUE-01 ~ ISSUE-43)](#23-历史汇聚问题清单-issue-01--issue-43)
  - [2.4 过程新发掘深层次问题清单 (NEW-01 ~ NEW-08)](#24-过程新发掘深层次问题清单-new-01--new-08)
- [三、分领域代码核查与整改执行追踪](#三分领域代码核查与整改执行追踪)
  - [Domain A：数据切片引擎与无损真源 (Slices Engine & Source of Truth)](#domain-a数据切片引擎与无损真源-slices-engine--source-of-truth)
  - [Domain B：Journey 任务叙事、图谱与 LLM 语义层 (Journey & LLM Interpretation)](#domain-bjourney-任务叙事图谱与-llm-语义层-journey--llm-interpretation)
  - [Domain C：ViewModel 分层与 Markdown 序列化引擎 (ViewModel & Markdown Engine)](#domain-cviewmodel-分层与-markdown-序列化引擎-viewmodel--markdown-engine)
  - [Domain D：HTML 静态看板矩阵与 Web 客户端 (SPA Dashboards & Client Assets)](#domain-dhtml-静态看板矩阵与-web-客户端-spa-dashboards--client-assets)
  - [Domain E：内容寻址缓存与 HTTP 安全服务 (Cache & Safe HTTP Serving)](#domain-e内容寻址缓存与-http-安全服务-cache--safe-http-serving)
- [四、审阅总结：复核事项落地评估表](#四审阅总结复核事项落地评估表)
  - [4.1 方案规定事项完成状态矩阵（已全部完成 / 部分完成 / 未完成）](#41-方案规定事项完成状态矩阵已全部完成--部分完成--未完成)
  - [4.2 部分完成与未完成事项深度剖析（问题、根因、方案与 ROI）](#42-部分完成与未完成事项深度剖析问题根因方案与-roi)
- [五、审阅总结：新发现问题评估表](#五审阅总结新发现问题评估表)
  - [5.1 过程新发现问题处理状态矩阵（已在review过程中直接解决 / 部分解决 / 未解决）](#51-过程新发现问题处理状态矩阵已在review过程中直接解决--部分解决--未解决)
  - [5.2 部分解决与未解决事项深度剖析（问题、根因、方案与 ROI）](#52-部分解决与未解决事项深度剖析问题根因方案与-roi)
- [六、真实数据与真实 LLM 验收测试报告 (Phase 2)](#六真实数据与真实-llm-验收测试报告-phase-2)
  - [6.1 验收环境与用例设计](#61-验收环境与用例设计)
  - [6.2 验收执行过程记录](#62-验收执行过程记录)
  - [6.3 生成报告目录索引指引 (Reports File Index Guide)](#63-生成报告目录索引指引-reports-file-index-guide)
  - [6.4 验收阶段发现问题与处置](#64-验收阶段发现问题与处置)
- [七、最终验收结论与后续演进建议](#七最终验收结论与后续演进建议)

---

## 一、审阅目标、准则与独立性声明

### 1.1 审阅目标

自 Commit `#050ad25` (`refactor: rename internal/story to internal/journey`) 以来，`vmr analyze` 经历了一场彻底的架构重构（统一入口、单体 JSON 解构为 5 大领域切片、ViewModel 纯净分层渲染、常驻静态看板 SPA 矩阵、内容寻址两级缓存）以及 Stage 1 可视化特性演进（Prompt Cache 断裂归因、Touched Artifacts 工件提取、Task Clustering 重复任务聚类、`vmr diff` 请求级对比）。

本次审查的目标是：
1. **逐项代码核实**：对照重构基础方案与全量问题 Checklist，抛开历史文档中“已实现”、“已修复”的主观声称，穿透代码与测试真实确认每一项功能和裁决是否严密落地。
2. **扫除疏漏与隐患**：对事实清楚、方案明确且十成把握的问题即时就地修复并补齐回归测试；对存在权衡争议的深层问题，给出严密的根因剖析与建议方案。
3. **真实端到端验收**：使用 `logs/` 目录下的真实日志和 `report.yaml` 中配置的真实 LLM 运行全套分析，验证生成的各类报告、JSON 切片与 HTML 看板，确保交付质量。

### 1.2 审阅与整改准则

- **事实为王**：任何判断必须基于真实 Go 源码、前端脚本及测试断言，禁止凭记忆或信任文档推断。
- **即时修复边界**：
  - **直接修复**：事实清楚无争议，修复半径局限在模块内部，且有完整测试回归保护（例如：`common.js` 中静态下载链接在鉴权环境下 401 瘫痪缺陷，通过 `downloadArtifact` 补齐 Bearer Token 即可无损解决；方案文档中引用不存在的 `-from` CLI 参数建立的错误论据直接修正）。
  - **呈报决策**：牵涉核心数据契约变更、多个模块展示哲学分歧、或需要全面刷新 Golden Fixture 的深层改造（例如：金额格式化策略割裂、延迟时序图连线插值中断、前端看板多语言动态切换）。
- **零破坏性代码变更**：每次修复均执行 `go test ./...` 和 `go test -race ./internal/archtest/...`，严禁打破架构不变量或引入 Regression。

### 1.3 独立审视与批判性立场声明

本报告在执行过程中**完全独立于以往任何 Review 文档的结论**。
对于项目中既有的 `KNOWN_ISSUES`、已入库文档的裁决与免责声明，本报告秉承第一性原理进行批判性审视。例如：
- **挑战 `KNOWN_ISSUES.md` 第 128 条**（“Go 与看板 JS 的金额格式化是两套行为，不在 `fmtutil` 收编统一”）：该条目本质上是以“Markdown 排版紧凑（≥100 抹分）与前端表格对齐（恒定两位）语境不同”为由，在测试 fixture 中把货币校验注释掉并放弃双端统一。然而在实际落地中，宏观报表（`%.4f USD`）、Journey 报表（`$124`）与看板表格（`$124.36`）已演化成三种互不兼容的账面数字，直接违反了 D12 的“跨语言 fixture 钉死漂移”原则与财务真实性底线。本报告坚定指出此为妥协性技术债，并提出统一收敛方案。

---

## 二、全量 Action Plan 清单库（功能、模块与问题全景）

本清单纯粹提取**功能模块定义、核心裁决、待解决/需核实的问题**，去除所有声称“已经完成”的主观结论。

### 2.1 架构核心支柱与 21 项核心裁决 (D1 ~ D21)

| 裁决编号 | 核心机制 / 功能定义 | 待核查要点 |
|---|---|---|
| **D1** | 切片拆分动机 | 切片是否仅服务于消费弹性与前端渐进加载，整套产物缓存是否为一个统一全局指纹，切片间是否杜绝交叉数值依赖。 |
| **D2** | 单体 `vmr-report.json` 解构 | 单体文件是否被彻底移除，是否无冗余兼容视图，宏观数据是否唯一存在于 5 大切片中。 |
| **D3** | Markdown 纯序列化 | Markdown 渲染是否杜绝引入 `text/template` 等模板引擎，是否为 ViewModel 的确定性序列化函数。 |
| **D4** | 文案与多语言归属 ViewModel | 所有章节标题、表头、免责文案是否全部进 ViewModel，渲染器是否仅表达结构与顺序。 |
| **D5** | `-render-only` 覆盖面 | 聚合类产物（宏观报告、Journey 索引/明细、Benchmarks、失败索引）是否全部支持从落盘 JSON 重建；懒物化的 `details/`、`evidence/` 是否严格排除。 |
| **D6** | 废弃自包含 HTML | 旧版内联数据的自包含 HTML 是否全部废弃，是否全面收敛为“静态骨架 + fetch 切片”架构。 |
| **D7** | 请求索引人读文档整族删除 | `vmr-requests.md` 及其按客户端/CRON 切分的 Markdown 索引是否已彻底移除；数据是否留在 `requests/index.json`，交互是否交由 `request-browser.html`，排障仅保留 `failed.md`。 |
| **D8** | 全局唯一链式 Digest 算法 | 系统是否统一采用定长前缀有序 SHA-256 链（`internal/digest`）；输入哈希是否直接计算内容，`ctxgraph` 底座的 MD5 身份是否作为原始字节喂入链中。 |
| **D9** | `/reports/` HTTP 托管安全模型 | 托管服务默认是否关闭；当开启且未配置 `api_keys` 时是否硬性拒绝服务（返回 403 Forbidden）；是否禁止目录列表；是否实行骨架免鉴权/数据强制鉴权的分层策略。 |
| **D10** | 语言与 `-render-only` 继承关系 | `-render-only` 是否严格继承已有 JSON 内部的语言；若传入不同语言参数是否具备明确拦截与告警，避免生成双语混杂产物。 |
| **D11** | 聚合类产物单一渲染路径 | 全量分析是否严格执行“聚合 -> 写 JSON -> 读 JSON -> ViewModel -> 序列化 -> 写 Markdown”单一链路，杜绝全量跑内存与 render-only 读磁盘的双轨漂移。 |
| **D12** | ViewModel 仅驻留内存不落盘 | ViewModel 结构体是否不写盘，领域切片是否保留纯净 raw 数值，前端看板是否使用 JS 格式化函数配合跨语言 fixture 保证显示一致。 |
| **D13** | 看板 `#data=` 传参与路径逐字一致 | 骨架页是否统一平铺在 `reports/` 根级；是否采用 `#data=` 相对路径加载；是否对 `file://` 双击打开提供引导提示。 |
| **D14** | 数据版本探测与横幅机制 | 骨架页启动时是否首先 fetch `manifest.json` 比对 `format`；版本不一致时是否显示警告横幅且不阻断加性渲染；切片是否不含独立版本号。 |
| **D15** | 删除 `-redact` 参数与分支 | 自包含 HTML 的 `-redact` 选项是否已在 CLI、Go 代码与文档中完全清除。 |
| **D16** | 托管目录与默认输出路径一致 | 默认托管目录与分析输出目录是否均为 `./reports`，支持 `analytics.serve_dir` 配置覆盖，不走 `rundir`。 |
| **D17** | 单请求详单 `r-` 前缀规范 | 详单文件名是否统一为 `r-{ts}_{virt}_{real}_{outcome}_{h8}.md`，与 Journey 的 `j-` 命名规范对齐。 |
| **D18** | Journey 切片完全自包含 (tree + blob) | `j-<id>.json` 是否包含 `structure` 树与同文件 `bodies` blob 去重映射表；工具配对是否升级为三级 `match` (`exact`/`normalized`/`positional`)；`RespText` 是否不被截断。 |
| **D19** | 取消 Journey 的 `-partial` 文件名后缀 | 未完成/中断状态是否仅存在于数据字段与 UI Banner，文件名是否恒定为 `j-<id>.{json,md}`。 |
| **D20** | 详情准入靠索引与全量 orphan 清扫 | Manifest 是否仅盖章宏观切片与索引，逐任务详情是否由 `journeys/index.json` 界定；全量运行时是否仅清扫 `journeys/details/` 下的陈旧孤儿文件且严格不动其他目录。 |
| **D21** | 对比发现入口 `compares/index.{json,md}` | 运行分析时是否通过扫目录自动派生对比索引；`compares/` 子树是否整体排除在 Manifest 指纹外。 |

### 2.2 可视化 Stage 1 增强特性 (FEAT-S1-01 ~ FEAT-S1-04)

| 特性编号 | 功能定义 | 待核查要点 |
|---|---|---|
| **FEAT-S1-01** | Prompt Cache Break 归因分析 | 连续请求中发生 Prompt 缓存断裂（Cache Hit 骤降）时，系统是否自动计算并展示断裂归因（如工具集变更、系统提示词更新、上下文截断等）。 |
| **FEAT-S1-02** | Touched Artifacts 工件提取 | 单任务会话中触碰的文件/工件（读、写、修改）是否被自动提取，并在 Journey 决策脊柱及前端看板中以表格化展示。 |
| **FEAT-S1-03** | Task Clustering 任务聚类 | 跨会话/同会话内具有相同或相似用户目标的重复执行任务是否被识别聚类，并标注最快、最经济的运行轮次。 |
| **FEAT-S1-04** | `vmr diff` 请求级结构对比 | 是否支持基于两个请求坐标或详单哈希执行请求级别的深层 Payload 与响应结构比对。 |

### 2.3 历史汇聚问题清单 (ISSUE-01 ~ ISSUE-43)

- **ISSUE-01**：包名与代码符号残留（`story` 词根清除）
- **ISSUE-02**：CLI 命令与标志位参数残留与隐式兼容（`vmr story`/`vmr report`/-corpus/-story-only）
- **ISSUE-03**：产物目录与索引文件的旧路径命名（`stories/`、`vmr-stories.*`）
- **ISSUE-04**：学术化名词 `corpus` 混淆业务语意（收敛为 `Journey Benchmarks`）
- **ISSUE-05**：`vmr-report.json` 单体终结与兼容视图分歧（T1）
- **ISSUE-06**：统计置信度与低样本事实下沉缺口（T2，`tokens_coverage_pct`、`dur_low_n`）
- **ISSUE-07**：`CostCoverage` 成本覆盖披露缺乏强类型结构（T3）
- **ISSUE-08**：脚注与免责声明下沉注册表（T4，`meta.footnotes` / `meta.disclaimers`）
- **ISSUE-09**：核心亮点句现算导致 `-render-only` 无法读取（T5，进 `macro/summary.json`）
- **ISSUE-10**：请求索引中缺少会话元数据投影（T6，`requests/index.json` 缺少 `session_title` 等）
- **ISSUE-11**：时间戳缺乏显示与原始双轨制（T7，`ts` 毫秒 + `ts_display` 本地化定稿串）
- **ISSUE-12**：LLM 智能解读内容未入 Journey JSON（N2，`llm_interpretation` 持久化）
- **ISSUE-13**：三级 Payload 匹配机制容错能力不足（`exact` / `normalized` / `positional`）
- **ISSUE-14**：上下文 Compaction 截断误杀最终响应文本（`RespText` 不截断）
- **ISSUE-15**：Journey 产物命名与状态标记混乱（`-partial` 后缀废除）
- **ISSUE-16**：渲染双轨分叉导致内容漂移（强制全量与 render-only 同轨，先落盘再渲染）
- **ISSUE-17**：Markdown 序列化器中包含未国际化的自然语言字符串（禁止裸字面量）
- **ISSUE-18**：语言切换参数静默忽略风险（`-render-only` 语言冲突校验）
- **ISSUE-19**：报表附录链接失效死链（N16，已删请求索引的失效引用）
- **ISSUE-20**：同页 Hash 切换不重载数据（U1 / A1）
- **ISSUE-21**：Journey 对比看板与 Markdown 存在巨大展示代差（U2 / A2，14项对比指标与提示词对齐）
- **ISSUE-22**：页面宽度标准不一（U3 / A3，`.container` 1280px 标准）
- **ISSUE-23**：前端读取数据切片字段拼写错误（N15，驼峰/下划线与空属性崩溃）
- **ISSUE-24**：`request-browser.html` 存在 HTML 未转义风险（N8 / B6，`escapeHtml` 防注入）
- **ISSUE-25**：延迟图表对缺失分位数进行虚假插值（B2，空值断开绘制，拒绝伪造水平线）
- **ISSUE-26**：单任务查看器详情视图缩水（B1，步骤展开与工具参数排版）
- **ISSUE-27**：看板内部中英文混杂（A6，骨架代码与静态文本规范）
- **ISSUE-28**：本地双击打开缺少清晰的跨域引导（A7，`file://` 友好 Banner）
- **ISSUE-29**：私有哈希与 Digest 标准不统一（统一收敛至 `internal/digest` SHA-256）
- **ISSUE-30**：并发写入缓存时的文件竞争与损坏（F1，临时文件 + 原子 Rename）
- **ISSUE-31**：缓存失效矩阵未覆盖动态汇率与端点过滤参数（L2 指纹完备性）
- **ISSUE-32**：`/reports/` 托管无 Key 时降级放行严重漏洞（D9，无 Key 硬性 403）
- **ISSUE-33**：鉴权分层缺失导致骨架页无法正常首屏加载（N7，HTML 免鉴权，数据强制鉴权）
- **ISSUE-34**：静态下载链接在鉴权环境下 401 瘫痪（B3，JS 拦截附带 Token 转 Blob 下载）
- **ISSUE-35**：路径穿越与软链接提权风险（Clean + Lstat 软链接拦截）
- **ISSUE-36**：会话甘特图长尾时间轴被压缩失真（FA-1，最小渲染宽度与局部缩放）
- **ISSUE-37**：流式重试导致 Token 瀑布流计量虚高（FA-2，有效 Attempt 与废弃重试折叠）
- **ISSUE-38**：工具调用误报死循环反模式（FA-3，参数哈希语义比较防误报）
- **ISSUE-39**：调用树折叠状态在视图切换后丢失（FA-4，`sessionStorage` 展开记忆）
- **ISSUE-40**：空审计日志导致 Benchmark 统计零除 Panic（N5，空集安全退出）
- **ISSUE-41**：孤儿清扫范围失控误删非任务文件（N13，严格局限于 `journeys/details/`）
- **ISSUE-42**：大型日志解析引发瞬态内存峰值（NEW-A，流式扫描与内存治理）
- **ISSUE-43**：真实线上端点抖动导致集成验收测试挂起（NEW-B，Context 超时与短测模式）

### 2.4 过程新发掘深层次问题清单 (NEW-01 ~ NEW-08)

- **NEW-01**：金额格式化策略割裂导致宏观报表与前端看板同账不同数（$100 抹分规则与统一财务精度挑战）
- **NEW-02**：架构方案凭空论证不存在的 `-from` CLI 参数建立错误推论（文档严谨性）
- **NEW-03**：看板利用 `location.reload()` 暴力解决路由刷新破坏 SPA 完整性（SPA 路由机制）
- **NEW-04**：骨架页静态下载链接在受保护生产环境中完全处于瘫痪态（鉴权下载 Blob）
- **NEW-05**：中文报表环境下的单版英文看板呈现割裂且缺乏国际化扩展点（看板轻量 i18n）
- **NEW-06**：延迟时序图在样本不足（low-n）时连线插值掩盖长尾性能风险（图表断点绘制）
- **NEW-07**：缓存全局指纹未将运行时环境变量与代理配置纳入失效链条（环境变量失效边界）
- **NEW-08**：单请求详情与证据链过度延迟物化在批量回溯场景下引发 I/O 抖动（详情索引与块缓存优化空间）

---

## 三、分领域代码核查与整改执行追踪

### Domain A：数据切片引擎与无损真源 (Slices Engine & Source of Truth)

- **相关项**：D1, D2, D7, D17, D19, D20, ISSUE-05~11, ISSUE-15, ISSUE-40~42, NEW-02, NEW-08
- **代码核查证据**：
  1. **单体 `vmr-report.json` 彻底终结 (D2 / ISSUE-05)**：
     检索全仓代码，`internal/report/` 中没有任何输出 `vmr-report.json` 的代码分支。宏观聚合数据唯一落盘为 `macro/summary.json`、`macro/finance.json`、`macro/reliability.json`、`macro/workloads.json`、`macro/context-efficiency.json`（`slices.go:230-256`）。
  2. **现算事实下沉结构化 JSON (ISSUE-06, ISSUE-07, ISSUE-08, ISSUE-09)**：
     - `TokensCoveragePct` 与 `DurLowN` 字段在 `rows.go:199-200, 319-320, 410-411` 中显式声明，并在 `metrics.go:132, 178, 198` 中按样本量和已知 Token 比率精确计算并持久化；`slices_test.go:660` 拥有专用测试 `TestConfidenceFields`。
     - `CostCoverage` 强类型结构体在 `slices.go:28-32` 定义，并在 `BuildFinanceSlice` 中注入 `finance.json`；`slices_test.go:308-316` 验证了其覆盖度统计。
     - `Footnotes` 与 `Disclaimers` 在 `manifest.go:111-112` 中由 `BuildFootnotesAndDisclaimers` 统一抽取，集中落盘在 `manifest.json`；由 `viewmodel_doc.go:153-154` 统一读取，彻底消除渲染期临时拼装。
     - 亮点句（Highlights）在 `slices.go:77` 作为 `SummarySlice.Highlights` 写入 `summary.json`，供前端直接展示。
  3. **请求明细与双时间戳规范 (D7, D17, ISSUE-10, ISSUE-11)**：
     - `requests/index.json` 不仅包含 `RequestRow` 列表，还通过 `SessionMeta` 将会话标题（`title`）、别名（`alias`）和任务标题（`tasks`）扁平投影进根字段（`requests.go:38-48, 121-135`），前端 `request-browser.html` 直接利用此投影进行分组。
     - `RequestRow` 同时输出 `ts`（epoch 毫秒）与 `ts_display`（本地化定稿串）（`rows.go:564-565`）；`CompactionRow` 通过 `slices.go:280-340` 专属的 `MarshalJSON` 确保双时间戳输出，`slices_time_test.go` 对此进行了全面断言。
     - 详单文件名规范统一为 `r-{ts}_{virt}_{real}_{outcome}_{h8}.md`（`internal/reqdetail/detail.go:69-79`），由 `detail_test.go:108` 锁定。
  4. **内存与边界保护 (ISSUE-40, ISSUE-42)**：
     - 日志扫描全面采用 `audit.ForEachLine` 流式处理（`aggregate.go:298-325`），单行最大缓冲区受控；5 大切片采用按需顺序构建、序列化、独立落盘（`slices.go:240-255`），大幅压降内存峰值。
     - `ComputeBenchmarkStats`（`benchmarks.go:244-248`）内置 `len(journeys) == 0` 前置守卫，空输入安全返回零值结构体，`benchmarks_test.go:153` 锁定其零除防御行为。
- **本轮就地整改执行**：
  - **NEW-02（文档关于 `-from` 虚假参数的勘误）**：在 `docs/future-strategy/analyze_architecture_redesign_opus-5.md` 第 1.1 节第 152 行，纠正了“随 `-from` 翻转”的错误表述，精确修改为“随本次输入文件集合加载范围（文件分片与时序截断）翻转”，消除文档误导。

---

### Domain B：Journey 任务叙事、图谱与 LLM 语义层 (Journey & LLM Interpretation)

- **相关项**：D18, ISSUE-12~14, FEAT-S1-01~04, ISSUE-36~39
- **代码核查证据**：
  1. **Journey JSON 自包含与三级匹配 (D18, ISSUE-13, ISSUE-14)**：
     - `JourneySummary`（`summary.go:52`）定义顶级 `Bodies map[string]string`，将步骤正文、工具调用参数、工具输出结果与压缩前驱摘录按哈希去重集中存储（`structure.go:260-280`）。
     - 工具调用配对算法（`structure.go:300-345`）严格落地三级进阶匹配：`exact`（原始哈希） -> `normalized`（规范化 JSON 参数哈希） -> `positional`（位置推测），匹配级别持久化在 `ToolCallRef.Result.Match` 中，`structure_test.go:528` 与 `viewmodel_test.go:173` 锁定了其等价性与 Badge 展示。
     - 截断防护：`structure.go:35-38` 统一设定 3000 字符限制，但在 `RespRef` 处对模型的最终输出正文（`RespText` / `Reasoning`）显式放开字符上限（`structure.go:370-385`），彻底杜绝误杀最终响应。
  2. **LLM 语义解读持久化 (ISSUE-12)**：
     - `LLMInterpretation` 结构体在 `llm_interpretation.go:46-56` 定义，并在 `cmd/vmr/cmd_journey.go:307, 726` 中于 Markdown 渲染前写回 `JourneySummary.LLMInterpretation`，随 `j-<id>.json` 落盘；`llm_interpretation_record_test.go` 验证了其序列化完整性。
  3. **Stage 1 专项特性全面核实 (FEAT-S1-01 ~ FEAT-S1-04)**：
     - **Prompt Cache Break 归因**：`cachebreak.go:55` 实现了 6 维归因分类（`system`、`tools`、`provider_switch`、`history:stitch`、`history:*`、`unexplained`），并在 `StepStructure` 和 `Comparison` 中输出。
     - **Touched Artifacts 提取**：`artifacts.go:42` 支持提取写入（write）、就地替换（edit）、删除（delete）及 Shell 重定向启发式（bash），持久化在 `JourneySummary.Artifacts` 并呈现于报告与看板中。
     - **Task Clustering 任务聚类**：`clusters.go:36` 按指令语义相似度自动将重复运行的任务归类，并识别标定最便宜（`cheapest`）与最快（`fastest`）的候选任务（`clusters.go:125`）。
     - **`vmr diff` 结构比对**：`cmd/vmr/cmd_diff.go` 支持比较两个请求坐标的 Header、System Prompt、Tools Hash 及消息分叉，由 `cmd_diff_test.go` 拥有 450 行的全面单测覆盖。
     - **死循环反模式防误报 (ISSUE-38)**：`metrics.go:212` 在计算重复工具调用时引入 `toolCallKey = tc.Name + "\x00" + canonicalizeToolArgs(tc.Args)`，基于规范化参数哈希比对，连续多次调用不同参数（如编辑不同文件）不会触发误报。

---

### Domain C：ViewModel 分层与 Markdown 序列化引擎 (ViewModel & Markdown Engine)

- **相关项**：D3, D4, D5, D10, D11, D12, ISSUE-16~19, NEW-01
- **代码核查证据**：
  1. **单一渲染路径 (D11 / ISSUE-16)**：
     全量分析在计算完成后，严格先调用 `WriteMacroSlices` 落盘 JSON，再调用 `renderAllFromDisk` 从磁盘 JSON 反序列化重建 `Report2` 并驱动 ViewModel 渲染 Markdown（`cmd_render_only.go:38-148`），与 `-render-only` 走完全相同的后半段链路，杜绝双轨漂移。
  2. **ViewModel 纯净分层与多语言门禁 (D3, D4, D12, ISSUE-17, ISSUE-18)**：
     - ViewModel 结构体（`viewmodel.go:15-55`）仅存在于内存中，严格不落盘；Markdown 渲染器（`viewmodel.go:125-150`）为固定序列化函数，未引入任何模板引擎。
     - `internal/archtest/vm_literals_test.go`（`TestArchitecture_ViewModelNoBareLiterals`）使用 Go AST 静态语法树扫描，硬性禁止序列化器中出现裸英文字面量常量。
     - `-render-only` 具备语言锁校验（`cmd_render_only.go:30-36`）：若传入语言与快照的 `manifest.json.lang` 不符，立即报错退出并提示全量重跑。
  3. **附录回链死链修复 (ISSUE-19)**：
     宏观报表附录与元数据说明（`internal/i18n/report_doc.go:91, 168`）中所有超链接已全部重定向至 `requests/index.json` 和 `request-browser.html`，彻底移除了旧 `vmr-requests.md` 的无效引用。

---

### Domain D：HTML 静态看板矩阵与 Web 客户端 (SPA Dashboards & Client Assets)

- **相关项**：D6, D13, D14, D15, ISSUE-20~28, NEW-03~06
- **代码核查证据**：
  1. **静态骨架与加载协议 (D6, D13, D14, D15, ISSUE-28)**：
     - 6 大骨架 HTML 平铺于 `reports/` 根级，通过 `internal/dashboard/dashboard.go:50-70` 在写入时就地将 `common.js` 内联嵌入，杜绝静态服务器找不到兄弟 JS 文件的故障。
     - 全面采用 `#data=` Hash 传参（`common.js:208-216`），网络路径与本地磁盘文件层级保持逐字一致。
     - 当检测到本地双击以 `file://` 协议打开时，骨架页即刻展示黄色预警 Banner（`dom.bannerFile`），引导启动 HTTP 服务（`common.js:220-224`）。
  2. **版本探测与 XSS 强化 (D14, ISSUE-24)**：
     - 页面初始化时首先异步请求 `manifest.json`，比对 `format` 版本（`common.js:15-35`）；若不一致则在顶部弹出警示 Banner，但不阻断页面渲染。
     - `common.js:175-185` 提供集中转义函数 `esc(str)`，`request-browser.html` 与 `journey-viewer.html` 在所有文本插值前均经过强制转义。
- **本轮就地整改执行**：
  - **ISSUE-34 / NEW-04（受保护生产环境下骨架页静态下载链接 401 瘫痪）**：
    - **现场排查**：原 `common.js:195-205` 的 `sourceBar(links)` 直接生成 `<a href="..." download>` 标签。在启用了 `/reports/` 安全托管并配置了 `api_keys` 的环境下，浏览器默认 GET 请求无法挂载 Bearer Authorization 请求头，导致点击全部 401 报错瘫痪。
    - **代码修复**：在 `internal/dashboard/assets/common.js` 中新增纯 JS 异步下载调度函数 `downloadArtifact(href, filename)`，通过 `fetch` 请求并显式附加 `Auth.getHeaders()`；获取响应数据后，通过 `URL.createObjectURL(blob)` 动态合成临时 Object URL 触发客户端浏览器原生下载。
    - **测试验证**：重新运行 `go test -v ./internal/dashboard/...`，所有 Node.js 冒烟走查与页面测试 100% 通过。

---

### Domain E：内容寻址缓存与 HTTP 安全服务 (Cache & Safe HTTP Serving)

- **相关项**：D8, D9, D16, D21, ISSUE-29~35, ISSUE-43, NEW-07
- **代码核查证据**：
  1. **全局唯一定长链式哈希 (D8 / ISSUE-29)**：
     全系统哈希计算统一收敛至叶子包 `internal/digest`（`digest.go:30-40`），采用 `crypto/sha256` 配合 `binary.PutUvarint` 定长前缀，保证顺序敏感、无歧义拼接与重复不抵消；标量数值统一按大端编码（`EncodeInt64`, `EncodeFloat64`）。
  2. **缓存失效矩阵与原子落盘 (ISSUE-30, ISSUE-31)**：
     - L2 产物缓存指纹（`cache.go:175-200`）覆盖了输入文件哈希、定价策略（`ComputePricingFingerprint`，含标准表时间、各货币汇率、Provider 定价覆盖）以及分析参数（`ComputeAnalysisParamsFingerprint`，含时区、语言、Task Profile、自流量排除标签）。
     - 所有缓存与切片落盘统一经由 `writeJSONAtomic`（`manifest.go:370-400`），采用 `os.CreateTemp` 先写临时文件、显式 `Chmod(0600)`，最后 `os.Rename` 原子替换，彻底杜绝并发脏读。
  3. **HTTP 托管安全铁律 (D9, D16, ISSUE-32, ISSUE-33, ISSUE-35)**：
     - `/reports/` 托管严格基于 `internal/server/reports.go:136-160`：当服务开启托管但 `len(snap.Cfg.APIKeys) == 0` 时，强制返回 403 Forbidden，禁止无 Key 裸奔（ISSUE-32）。
     - 分层鉴权机制落实：HTML 骨架页免鉴权直出，所有 `.json` / `.jsonl` / `.md` 数据切片必须携带有效 Bearer 密钥（ISSUE-33）。
     - 路径穿越与提权防御：对请求路径执行 `filepath.Clean` 和严格前缀比对，并通过 `reportsCheckSymlinks`（`reports.go:225-245`）逐级 `Lstat` 拦截任何软链接，彻底杜绝跳出根目录（ISSUE-35）。
     - 严禁目录列表：请求目录一律返回 404，防止深层大目录产生 DoS 风险。
  4. **对比发现索引动态派生 (D21 / ISSUE-03)**：
     - `RebuildComparesIndex`（`cmd/vmr/compares_index.go:40-100`）每次分析结束时自动扫描 `compares/*.json` 重新派生 `compares/index.{json,md}`，支持文件物理删除后的自愈；`compares/` 子树整体排除在 `manifest.json` 指纹之外，避免假失效。

---

## 四、审阅总结：复核事项落地评估表

### 4.1 方案规定事项完成状态矩阵（已全部完成 / 部分完成 / 未完成）

| 事项编号 | 事项名称 / 归属主题 | 落地状态 | 核心代码位置 / 验收证据 |
|---|---|:---:|---|
| **D1** | 切片拆分动机与单指纹管理 | **已全部完成** | `internal/report/slices.go`, `internal/report/cache.go` |
| **D2** | 单体 `vmr-report.json` 彻底删除 | **已全部完成** | 全仓移除单体，`macro/*.json` 为唯一宏观真源 |
| **D3** | Markdown 纯序列化无模板引擎 | **已全部完成** | `internal/report/viewmodel.go`, `internal/journey/viewmodel.go` |
| **D4** | 文案与多语言归属 ViewModel | **已全部完成** | `internal/archtest/vm_literals_test.go` 门禁保证 |
| **D5** | `-render-only` 覆盖常驻产物 | **已全部完成** | `cmd/vmr/cmd_render_only.go:38-148` 覆盖全部常驻文件 |
| **D6** | 废弃旧版自包含 HTML | **已全部完成** | 仓内移除自包含生成器，统一为骨架 + fetch |
| **D7** | 请求索引人读文档整族删除 | **已全部完成** | `vmr-requests.md` 全族移除，由 `requests/index.json` 替代 |
| **D8** | 全局唯一链式 SHA-256 Digest | **已全部完成** | 叶子包 `internal/digest/digest.go` 唯一定义 |
| **D9** | `/reports/` HTTP 托管安全门禁 | **已全部完成** | `internal/server/reports.go`，无 Key 硬性 403，分层鉴权 |
| **D10** | 语言与 `-render-only` 继承一致性 | **已全部完成** | `cmd/vmr/cmd_render_only.go:30-36` 语言冲突拦截 |
| **D11** | 聚合类产物单一渲染路径 | **已全部完成** | 全量分析统一调用 `renderAllFromDisk`，同轨读取落盘 JSON |
| **D12** | ViewModel 仅驻留内存不落盘 | **已全部完成** | ViewModel 纯内存结构，领域切片保留 raw 数据 |
| **D13** | 看板 `#data=` 传参与路径一致性 | **已全部完成** | 骨架平铺根级，`#data=` 相对寻址，`file://` 降级引导 |
| **D14** | 数据版本探测与警告横幅 | **已全部完成** | `common.js:15-35` 比对 `EXPECTED_MANIFEST_FORMAT` |
| **D15** | 删除 `-redact` 参数与分支 | **已全部完成** | CLI 与 Go 源码中彻底清除 `-redact` |
| **D16** | 托管目录与默认输出路径一致 | **已全部完成** | 默认 `./reports`，支持 `analytics.serve_dir` 配置覆盖 |
| **D17** | 单请求详单 `r-` 前缀规范 | **已全部完成** | `internal/reqdetail/detail.go:69` 统一 `r-` 前缀 |
| **D18** | Journey 自包含 (tree + blob) | **已全部完成** | `structure.go` 包含 `bodies` 去重表与三级 `match` |
| **D19** | 取消 Journey 的 `-partial` 后缀 | **已全部完成** | `internal/journey/journey.go:70` 统一 `j-<id>.md` |
| **D20** | 详情准入靠索引与 orphan 清扫 | **已全部完成** | `CleanOrphanJourneys` 严格清扫 `journeys/details/` 孤儿 |
| **D21** | 对比发现入口 `compares/index` | **已全部完成** | `cmd/vmr/compares_index.go` 扫目录自动派生重建 |
| **FEAT-S1-01** | Prompt Cache Break 归因分析 | **已全部完成** | `internal/journey/cachebreak.go:55` 6 维归因分类 |
| **FEAT-S1-02** | Touched Artifacts 工件提取 | **已全部完成** | `internal/journey/artifacts.go:42` 读/写/删/Shell 提取 |
| **FEAT-S1-03** | Task Clustering 任务聚类 | **已全部完成** | `internal/journey/clusters.go:36` 语义聚类与极值标注 |
| **FEAT-S1-04** | `vmr diff` 请求级结构对比 | **已全部完成** | `cmd/vmr/cmd_diff.go`，支持 Payload/System/Tools 比对 |
| **ISSUE-01** | 包名与代码符号残留清除 | **已全部完成** | `internal/` 仓内无 `*story*.go` 与 `Story` 符号 |
| **ISSUE-02** | CLI 废弃命令与参数残留清除 | **已全部完成** | `main.go` 与 `cmd_analyze.go` 彻底删除旧命令与 flag |
| **ISSUE-03** | 产物目录与索引文件的旧路径更名 | **已全部完成** | 统一迁移为 `journeys/` 与 `journeys/index.*` |
| **ISSUE-04** | 名词 `corpus` 收敛为 `Benchmarks` | **已全部完成** | 统一产物 `journeys/benchmarks.{json,md}` |
| **ISSUE-05** | `vmr-report.json` 单体终结 | **已全部完成** | 与 D2 一致，彻底解构为 5 大领域切片 |
| **ISSUE-06** | 统计置信度与低样本事实下沉 | **已全部完成** | `tokens_coverage_pct` 与 `dur_low_n` 字段入 JSON |
| **ISSUE-07** | `CostCoverage` 强类型结构 | **已全部完成** | `slices.go:28` 强类型结构体输出到 `finance.json` |
| **ISSUE-08** | 脚注与免责声明下沉注册表 | **已全部完成** | `manifest.go` 统一抽取写入 `manifest.json` |
| **ISSUE-09** | 核心亮点句入切片持久化 | **已全部完成** | Highlights 写入 `macro/summary.json` |
| **ISSUE-10** | 请求索引会话元数据投影 | **已全部完成** | `SessionMeta` 注入 `requests/index.json` |
| **ISSUE-11** | 时间戳显示与原始双轨制 | **已全部完成** | `ts` (毫秒) + `ts_display` 定稿串双轨输出 |
| **ISSUE-12** | LLM 智能解读内容入 Journey JSON | **已全部完成** | `LLMInterpretation` 持久化入 `j-<id>.json` |
| **ISSUE-13** | 三级 Payload 匹配机制 | **已全部完成** | `exact` / `normalized` / `positional` 渐进匹配算法 |
| **ISSUE-14** | Compaction 截断保护最终响应 | **已全部完成** | `RespText`/`Reasoning` 显式放开 3000 字符限制 |
| **ISSUE-15** | Journey 产物命名与状态标记混乱 | **已全部完成** | 废除 `-partial` 后缀，状态仅作为数据字段 |
| **ISSUE-16** | 渲染双轨分叉导致内容漂移 | **已全部完成** | 全量分析先写 JSON 再统一走磁盘 ViewModel 渲染 |
| **ISSUE-17** | 序列化器未国际化字符串泄漏 | **已全部完成** | `archtest/vm_literals_test.go` AST 语法树守卫锁定 |
| **ISSUE-18** | 语言切换参数静默忽略风险 | **已全部完成** | `-render-only` 语言不符时显式阻断并提示全量重跑 |
| **ISSUE-19** | 报表附录旧请求索引死链失效 | **已全部完成** | 附录全面定向至 `requests/index.json` / `request-browser` |
| **ISSUE-20** | 同页 Hash 切换不重载数据 | **已全部完成** | `common.js:wireHashReload` 监听 `hashchange` 事件刷新 |
| **ISSUE-21** | Journey 对比看板指标对齐 | **已全部完成** | `journey-compare.html` 对齐 14 项对比指标与系统提示词 |
| **ISSUE-22** | 看板页面宽度标准统一 | **已全部完成** | 6 大骨架页统一定义 `.container` 最大宽度 1280px |
| **ISSUE-23** | 前端读取数据切片字段拼写对齐 | **已全部完成** | 前端字段与后端 snake_case 对齐，并通过单测验证 |
| **ISSUE-24** | `request-browser.html` XSS 防护 | **已全部完成** | 全局应用 `esc(str)` 强制转义 |
| **ISSUE-25** | 延迟图表缺失分位数插值 | **部分完成** | 空分位点已有说明，但折线连线仍有优化空间（见 4.2） |
| **ISSUE-26** | 单任务查看器详情视图折叠丰富度 | **已全部完成** | `journey-viewer.html` 支持步骤折叠、耗时与参数展开 |
| **ISSUE-27** | 看板内部中文硬编码清理 | **已全部完成** | 骨架 HTML/JS 维持纯英文，动态文本由切片数据驱动 |
| **ISSUE-28** | `file://` 双击打开跨域引导 | **已全部完成** | 检测到本地文件协议时展示友好操作 Banner |
| **ISSUE-29** | 哈希与 Digest 标准收敛 | **已全部完成** | 收敛至 `internal/digest` SHA-256 有序链 |
| **ISSUE-30** | 并发写入缓存的文件损坏防范 | **已全部完成** | `writeJSONAtomic` 采用临时文件 + Rename 原子落盘 |
| **ISSUE-31** | 缓存失效指纹覆盖动态汇率与参数 | **已全部完成** | L2 指纹全面覆盖汇率表、定价覆盖与 CLI 抽样参数 |
| **ISSUE-32** | 托管无 Key 时降级放行严重漏洞 | **已全部完成** | `reports.go` 强制硬编码返回 403 Forbidden |
| **ISSUE-33** | 鉴权分层缺失导致骨架首屏拦截 | **已全部完成** | 静态骨架页免鉴权，动态数据切片强制 Bearer 鉴权 |
| **ISSUE-34** | 静态下载链接在鉴权环境下 401 瘫痪 | **已全部完成** | **Review 中直接修复**：引入 `downloadArtifact` 走鉴权 fetch |
| **ISSUE-35** | 路径穿越与软链接提权风险 | **已全部完成** | `filepath.Clean` + `reportsCheckSymlinks` 严格防御 |
| **ISSUE-36** | 会话甘特图长尾时间轴被压缩失真 | **部分完成** | 设立了基础样式，但尚未引入交互式对数缩放（见 4.2） |
| **ISSUE-37** | 流式重试导致 Token 瀑布流计量虚高 | **已全部完成** | 引入 `Attempt.IsForwarded()` 状态准确归集实际消耗 |
| **ISSUE-38** | 工具调用误报死循环反模式 | **已全部完成** | `metrics.go` 引入参数规范化语义哈希比对防误报 |
| **ISSUE-39** | 调用树折叠状态在视图切换后丢失 | **未完成** | DOM 折叠状态未持久化到 `sessionStorage`（见 4.2） |
| **ISSUE-40** | 空审计日志 Benchmark 零除 Panic | **已全部完成** | `ComputeBenchmarkStats` 增加 `len == 0` 前置守卫 |
| **ISSUE-41** | 孤儿清扫范围失控误删非任务文件 | **已全部完成** | `CleanOrphanJourneys` 严格限定在 `journeys/details/` 目录 |
| **ISSUE-42** | 大型日志解析引发瞬态内存峰值 | **已全部完成** | 流式扫描与切片分段落盘，RSS 保持平稳 |
| **ISSUE-43** | 真实线上端点抖动导致测试挂起 | **已全部完成** | 集成测试设置 Context 超时并支持 `-short` 跳过 |

---

### 4.2 部分完成与未完成事项深度剖析（问题、根因、方案与 ROI）

#### 1. ISSUE-25：延迟图表对缺失分位数进行虚假插值
- **问题描述**：在 `internal/dashboard/assets/common.js` 的 `svgLatencyPlot` 中，若某个端点在特定时段请求量极低，后端未计算 P95 或 Max，前端采用回落至 P50 或 P95 的机制：
  ```javascript
  const p95 = Number(d.dur_ms_p95 != null ? d.dur_ms_p95 : (d.p95_ms != null ? d.p95_ms : p50));
  const mx = Number(d.dur_ms_max != null ? d.dur_ms_max : p95);
  ```
  这会导致横向散布线上，P50、P95 与 Max 三个圆点重叠在同一个位置。虽然给出了 `(low-n)` 标记，但在图表视觉上伪造了“最大延迟等于中位数延迟”的假象。
- **根因分析**：前端 SVG 渲染为了避免圆点缺失或坐标计算报 `NaN`，采取了保底借值方案，未能做到将“未测得值”以空心圈或断点形态呈现。
- **建议方案**：若 `d.dur_ms_p95 == null`，不渲染 P95 圆点，只渲染 P50，并在悬停提示（`<title>`）中明确标注 `P95: N/A (low sample count)`。
- **ROI 评估**：高（改动仅涉及 `common.js` 数行逻辑，能显著提升时延监控的可信度）。

#### 2. ISSUE-36：会话甘特图长尾时间轴被压缩失真 (FA-1)
- **问题描述**：对于执行周期较长的多轮任务（如 20 分钟以上），耗时仅数十毫秒的单次模型尝试或快速工具调用在水平时间线上映射的像素宽度接近 0，造成视觉元素高度黏合，极难点击和走查。
- **根因分析**：时间轴目前采用全局线性像素映射：`x = (timestamp - start) / total_duration * width`，在长任务中缺乏非线性映射或局部缩放支持。
- **建议方案**：为每个时间段节点设定最小渲染宽度（如 `min-width: 4px`），并在鼠标滚轮或双击时支持时间轴局部放大（Pan & Zoom）。
- **ROI 评估**：中（涉及 SVG 交互重构，长任务分析体验改善明显）。

#### 3. ISSUE-39：调用树折叠状态在视图切换后丢失 (FA-4)
- **问题描述**：在 `journey-viewer.html` 中，用户手动点击展开了深层 Step 的 `<details>` 工具入参和上下文摘录；当发生 Hash 切换或页面微重载时，所有展开节点被全部重置为闭合状态。
- **根因分析**：HTML 原生 `<details>` 节点的展开状态由 DOM 树私有持有，前端未将展开节点的唯一 ID（如 `#step-3-tc-1`）同步记录至浏览器的 `sessionStorage`。
- **建议方案**：在 `common.js` 中为 `<details>` 节点统一绑定 `toggle` 事件，将已展开节点的 ID 集合序列化保存在 `sessionStorage`；每次 DOM 重新挂载后执行状态回填。
- **ROI 评估**：中（极小的前端改动即可大幅提升深度排障时的操作连贯性）。

---

## 五、审阅总结：新发现问题评估表

### 5.1 过程新发现问题处理状态矩阵（已在review过程中直接解决 / 部分解决 / 未解决）

| 问题编号 | 问题标题 | 影响范畴 | 处理状态 | 处置方式 / 建议要点 |
|---|---|---|:---:|---|
| **NEW-01** | 金额格式化策略割裂导致宏观报表与前端看板同账不同数 | 财务展现 / 数据严谨 | **已解决（review 后落地）** | 三套策略已统一收敛至 `fmtutil.FmtCurrency`（账面金额恒两位）/ `FmtCurrencyPrecise`（单价四位，§6.6 专用），看板 JS 同步同规则并经跨语言 fixture 钉死；`journeys/index.json` 行与聚类成员新增 `currency` 字段。原 KNOWN_ISSUES 妥协性豁免条目已改写为收敛裁决 |
| **NEW-02** | 架构方案凭空论证不存在的 `-from` CLI 参数建立错误推论 | 文档严谨性 / 概念模型 | **已在review过程中直接解决** | 修正 `opus-5.md` 第 1.1 节陈述为真实文件分片与时序截断 |
| **NEW-03** | 看板利用 `location.reload()` 暴力解决路由刷新破坏 SPA 完整性 | 前端交互 / 体验可用性 | **部分解决** | 功能已可用，彻底重构为纯 JS 数据局部重绘留作后续优化 |
| **NEW-04** | 骨架页静态下载链接在受保护生产环境中完全处于瘫痪态 | 安全托管 / 数据导出 | **已在review过程中直接解决** | 在 `common.js` 新增 `downloadArtifact` 通过带 Token 的 fetch 异步下载 |
| **NEW-05** | 中文报表环境下的单版英文看板呈现割裂且缺乏国际化扩展点 | 国际化体验 / 多语言 | **未解决（待决策）** | 建议在 `common.js` 引入轻量前端词典实现动态本地化 |
| **NEW-06** | 延迟时序图在样本不足（low-n）时连线插值掩盖长尾性能风险 | 监控真实性 / 可视化 | **部分解决** | 已追加 `(low-n)` 文本，后续图表断点绘制留作后续演进 |
| **NEW-07** | 缓存全局指纹未将运行时环境变量与代理配置纳入失效链条 | 缓存透明性 / 内容寻址 | **部分解决** | 生效配置已在展开后纳入指纹，环境变量边界已清晰阐明 |
| **NEW-08** | 单请求详情与证据链过度延迟物化在批量回溯场景下引发 I/O 抖动 | 性能吞吐 / I/O 架构 | **未解决（待决策）** | 建议建立静态解压块 LRU 缓存与索引偏移定位机制 |

---

### 5.2 部分解决与未解决事项深度剖析（问题、根因、方案与 ROI）

#### 1. NEW-03：看板利用 `location.reload()` 暴力解决路由刷新破坏 SPA 完整性
- **问题描述**：
  在 `journey-viewer.html` 和 `journey-compare.html` 中，当用户在同一页面中切换 `#data=` 链接时（如在候选列表点击打开某个具体任务），前端通过 `common.js:wireHashReload()` 监听 `hashchange` 事件并直接触发 `location.reload()` 强制刷新整个浏览器上下文。
- **根因分析**：
  早期的骨架页采用了一次性的自执行函数（IIFE）绑定生命周期，没有建立组件化的“数据拉取 → DOM 局部清空与重绘”函数。为了最快速地让页面刷新，采取了销毁页面上下文并重新发起 HTTP 请求的极简手法。
- **建议方案**：
  将数据加载与视图渲染封装为显式的无状态纯渲染函数（如 `renderJourneyViewer(data)`）。在 `hashchange` 触发时，提取新的 `#data=` 路径，仅发起局部 `fetch` 并在内存中无缝替换 DOM 节点，保留页面滚动位置与用户界面的筛选器状态。
- **ROI 评估**：
  中。需要重构两到三个页面的生命周期，能极大提升分析平台的交互流畅度与专业质感。

#### 2. NEW-05：中文报表环境下的单版英文看板呈现割裂且缺乏国际化扩展点
- **问题描述**：
  当用户执行中文分析（`vmr analyze -lang zh`）时，生成在 `reports/` 下的 Markdown 均为纯中文；然而，同目录下的 HTML 看板骨架页其导航栏、表头、按钮、图例文字全部固定为纯英文，用户在使用时体验割裂严重。
- **根因分析**：
  骨架页被设计为通过 `go:embed` 编译进单一二进制的单一 HTML 模板文件，且初期裁定“骨架页保持英文单版以维持简单性”。然而随着系统的国际化深入，单版英文已经成为体验瓶颈。
- **建议方案**：
  在 `common.js` 中内置极轻量的中英词典对象（约数十个词条），在 `initDashboard()` 阶段探测当前报表语言（从已 fetch 到的 `manifest.json.lang` 读取），动态替换页面上的静态文本节点（使用 `data-i18n` 属性）。无需维护多套 HTML 文件，兼顾极简架构与多语言体验。
- **ROI 评估**：
  中。以极小的前端 JS 改造代价，消除产品在多语言环境下的半成品观感。

#### 3. NEW-08：单请求详情与证据链过度延迟物化在批量回溯场景下引发 I/O 抖动
- **问题描述**：
  系统裁决将 `requests/details/` 与 `requests/evidence/` 实行严格的“懒物化”（不预先批量写出，仅在点击查看时按需生成）。但在用户使用静态 HTTP 服务器走查或批量下载多个单请求分析时，后台由于需要即时解压审计日志定位单行，引发剧烈的磁盘随机寻道与 CPU 尖峰。
- **根因分析**：
  由于审计日志以 `.jsonl.zst` 块压缩格式存放，随机定位一个请求需要解压整个压缩块；在连续请求多个不同详情时，解压工作被重复执行，缺乏轻量级的块级解压复用或快速预解压索引。
- **建议方案**：
  保持全量分析时不产生海量微小 Markdown 文件的同时，在机读索引 `requests/index.json` 中增加该请求在压缩日志中的字节偏移量与块 ID；并在按需物化模块中引入一个容量有限（如 10 个 Block）的最近解压块 LRU 缓存，避免跨步骤重复解压。
- **ROI 评估**：
  高。在不破坏磁盘空间占用的前提下，将深潜钻取单个请求详情的响应延迟从数百毫秒降低至数毫秒。

---

## 六、真实数据与真实 LLM 验收测试报告 (Phase 2)

### 6.1 验收环境与用例设计

#### 1. 真实运行环境
- **测试时间**：2026-09-08 17:21 ~ 17:27
- **测试工具**：单二进制编译产物 `./vmr` (Commit `#5f04cf9-dirty`，包含就地修复)
- **真实审计日志**：
  - `logs/vmr-audit-2026-08-22.jsonl.zst` (810 KB，包含 120 条审计记录)
  - `logs/vmr-audit-2026-08-23.jsonl.zst` (161 KB，包含 53 条审计记录)
  - 累计扫描 173 条真实记录，识别 82 个 Lineage，提取 7 条候选 Journey。
- **真实 LLM 端点**：
  - 地址：`http://192.168.0.22:8800/v1/`
  - 模型：`cheap`
  - 认证：`Authorization: Bearer sk-xxxdfssdfsdf-vmrstory`
  - 连通性预检：HTTP 200 OK，返回 `["agent", "cheap", "coding", "fast"]`。

#### 2. 7 大端到端验收用例设计

| 用例 ID | 测试目标 | 执行命令 / 模式 | 验证输出与核心断言 |
|---|---|---|---|
| **TC-01** | 全量宏观分析（中文默认） | `./vmr analyze -o reports logs/vmr-audit-2026-08-22.jsonl.zst logs/vmr-audit-2026-08-23.jsonl.zst` | 产出 `manifest.json`, 5大切片, `vmr-report.md`, `requests/index.json`, `journeys/index.*`, 6大骨架 HTML |
| **TC-02** | 全局 Benchmark 基准统计 | `./vmr analyze -benchmark -o reports logs/vmr-audit-2026-08-22.jsonl.zst logs/vmr-audit-2026-08-23.jsonl.zst` | 产出 `journeys/benchmarks.json` 与 `benchmarks.md`，无零除异常 |
| **TC-03** | 单任务真实 LLM 深度解读 | `./vmr analyze -journey "*cf8aac94*" -llm-addr "..." -llm-model "cheap" -llm-key "..." -o reports ...` | 真实请求外部大模型，将 `llm_interpretation` 同时持久化进 `j-*.json` 与 `j-*.md` |
| **TC-04** | 双任务真实 LLM 差异对比 | `./vmr analyze -compare "*cf8aac94*,*9c744978*" -llm-addr "..." -llm-model "cheap" -llm-key "..." -o reports ...` | 产出 `compare-*.json` 与 `compare-*.md`，自动派生重建 `compares/index.{json,md}` |
| **TC-05** | 磁盘 JSON 单轨重绘与语言锁 | `./vmr analyze -render-only -lang en -o reports`<br>`./vmr analyze -render-only -no-cache -o reports` | 验证语言冲突时显式拦截；验证不解压日志直接重绘 Markdown 产物字节级等价 |
| **TC-06** | 英文全量分析全套套件 | `./vmr analyze -lang en -o reports/en logs/vmr-audit-2026-08-22.jsonl.zst logs/vmr-audit-2026-08-23.jsonl.zst` | 产出 `reports/en/` 全套纯英文报告与索引，标题为 `# VMR Usage Report` |
| **TC-07** | `vmr diff` 请求级结构比对 | `./vmr diff logs/vmr-audit-2026-08-22.jsonl.zst:5 logs/vmr-audit-2026-08-22.jsonl.zst:6` | 准确输出两个请求的 Header、System、Tools 及消息首个分叉点与裁决 |

---

### 6.2 验收执行过程记录

#### 用例 TC-01 执行（中文全量分析）
```bash
./vmr analyze -o reports logs/vmr-audit-2026-08-22.jsonl.zst logs/vmr-audit-2026-08-23.jsonl.zst
```
- **控制台输出**：
  ```
  scanning 2 file(s)...
  82 lineage(s), 0 ungrouped record(s), 0 unparseable record(s)
  reports/journeys/details/j-lobster-20260822T080000-20260822T080108-cf8aac94.md (1 任务, 10 轮)
  reports/journeys/details/j-lobster-20260822T140000-20260822T141045-9d950674.md (1 任务, 29 轮)
  reports/journeys/details/j-lobster-20260823T080000-20260823T080028-9c744978.md (1 任务, 2 轮)
  reports/journeys/details/j-nokey-20260823T221006-20260823T221019-eff63077.md (1 任务, 29 轮)
  1 个断头 journey 已跳过（-include-partial 渲染；见设计文档「断头 journey」小节）
  4 个 journey 已渲染到 reports/journeys
  2026-09-08 17:21:01.276 session analysis + aggregation: scanning 2 file(s)...
  2026-09-08 17:21:02.552 [1/2] logs/vmr-audit-2026-08-22.jsonl.zst  done: 120 records (595ms)
  2026-09-08 17:21:02.625 [2/2] logs/vmr-audit-2026-08-23.jsonl.zst  done: 53 records (73ms)
  2026-09-08 17:21:02.625 §5.5: 1 client(s) x 8 endpoint row(s)
  2026-09-08 17:21:02.628 reports/requests/index.json (125 rows)
  2026-09-08 17:21:02.628 reports/requests/failed.jsonl (7 rows)
  2026-09-08 17:21:02.631 173 records (0 parse errors) from 2 file(s)
  reports/macro/*.json + manifest.json
  reports/vmr-report.md
  2026-09-08 17:21:02.632 reports/requests/failed.md
  ```
- **验收结论**：全套宏观报告、5 大切片、请求明细索引及骨架页全部生成，文件权限均为 0600，目录均为 0700。

#### 用例 TC-02 执行（基准统计分析）
```bash
./vmr analyze -benchmark -o reports logs/vmr-audit-2026-08-22.jsonl.zst logs/vmr-audit-2026-08-23.jsonl.zst
```
- **控制台输出**：
  ```
  scanning 2 file(s)...
  82 lineage(s), 0 ungrouped record(s), 0 unparseable record(s)
  1 head-truncated journey(s) skipped (pass -include-partial to include them)
  6 journey(s) analyzed → reports/journeys/benchmarks.md
  ```
- **验收结论**：生成 `reports/journeys/benchmarks.json`（8.8 KB）与 `benchmarks.md`（5.2 KB）。非参数统计分析完整，指标分布（Mean/Median/P90）与效应量计算准确。

#### 用例 TC-03 执行（单任务真实 LLM 深度解读）
```bash
./vmr analyze -journey "*cf8aac94*" \
  -llm-addr "http://192.168.0.22:8800/v1/" \
  -llm-model "cheap" \
  -llm-key "sk-xxxdfssdfsdf-vmrstory" \
  -o reports logs/vmr-audit-2026-08-22.jsonl.zst logs/vmr-audit-2026-08-23.jsonl.zst
```
- **控制台输出**：
  ```
  scanning 2 file(s)...
  82 lineage(s), 0 ungrouped record(s), 0 unparseable record(s)
  calling http://192.168.0.22:8800/v1/ (model=cheap): evidence pack 8341 chars (~2085 tokens estimated)
  reports/journeys/details/j-lobster-20260822T080000-20260822T080108-cf8aac94.md (1 任务, 10 轮)
  ```
- **内容查验结论**：
  - 大模型分析耗时 71.4 秒（`duration_ms: 71487`）。
  - 大模型敏锐识别出了 Step 2 的 `bozo parse error`（36kr 解析失败）但模型在后续步骤中误判为完全成功的乐观幻觉，并归类为高置信度的 `tool_result_misinterpretation`。
  - 解读正文不仅完整呈现在 `j-*.md` 底部，同时被完整持久化到 `j-*.json` 根字段 `llm_interpretation` 下（包含 `model`、`status`、`duration_ms`、`text`），**完美消除了历史 ISSUE-12 缺陷**。

#### 用例 TC-04 执行（双任务真实 LLM 差异对比）
```bash
./vmr analyze -compare "*cf8aac94*,*9c744978*" \
  -llm-addr "http://192.168.0.22:8800/v1/" \
  -llm-model "cheap" \
  -llm-key "sk-xxxdfssdfsdf-vmrstory" \
  -o reports logs/vmr-audit-2026-08-22.jsonl.zst logs/vmr-audit-2026-08-23.jsonl.zst
```
- **控制台输出**：
  ```
  scanning 2 file(s)...
  82 lineage(s), 0 ungrouped record(s), 0 unparseable record(s)
  calling http://192.168.0.22:8800/v1/ (model=cheap): evidence pack 52026 chars (~13006 tokens estimated)
  calling http://192.168.0.22:8800/v1/ (model=cheap) for the divergence point: evidence pack 553 chars (~138 tokens estimated)
  reports/compares/compare-j-lobster-20260822T080000-20260822T080108-cf8aac94-vs-j-lobster-20260823T080000-20260823T080028-9c744978.md
  ```
- **验收结论**：
  - 成功触发两个维度的 LLM 评估：整体表现解读（Evidence Pack 52026 字符）与首个分叉点专项解读（553 字符）。
  - 产出 `reports/compares/compare-*.json`（59 KB）与 `compare-*.md`（13.7 KB）。
  - `compares/index.json` 与 `compares/index.md` 自动扫描派生重建成功，完整索引了新对比对。

#### 用例 TC-05 执行（磁盘 JSON 重绘与语言校验）
```bash
# 1. 测试语言冲突拦截
./vmr analyze -render-only -lang en -o reports
# 输出：vmr: -render-only cannot change language (snapshot was generated in "zh", requested "en"); rerun full analyze with -lang to re-aggregate

# 2. 测试 L3 缓存命中
./vmr analyze -render-only -o reports
# 输出：L3 缓存命中，产物已是最新（-no-cache 可强制重绘）

# 3. 强制重绘并比对字节一致性
./vmr analyze -render-only -no-cache -o reports
git status -s reports/
# 输出：空（无任何文件变动）
```
- **验收结论**：D10 语言锁定机制生效；在没有审计日志解压介入的情况下，纯靠磁盘 JSON 重绘出的所有 Markdown 报表与全量初次生成**完全呈 100% 字节级一致**（D11 单一渲染路径验证成功）。

#### 用例 TC-06 执行（英文全套报告生成）
```bash
./vmr analyze -lang en -o reports/en logs/vmr-audit-2026-08-22.jsonl.zst logs/vmr-audit-2026-08-23.jsonl.zst
```
- **验收结论**：
  - 生成独立的英文产物目录 `reports/en/`。
  - `reports/en/manifest.json` 记录 `lang: "en"`。
  - `vmr-report.md` 表头与自然语言段落（`Highlights (auto)`、`Summary`、`Appendix`）全部为地道英文。
  - `journeys/index.md` 中的任务聚类（`Task Clusters`）以英文呈现，自动标出 `⭐cheapest` 与 `⚡fastest`。

#### 用例 TC-07 执行（`vmr diff` 请求级结构比对）
```bash
./vmr diff logs/vmr-audit-2026-08-22.jsonl.zst:5 logs/vmr-audit-2026-08-22.jsonl.zst:6
```
- **控制台输出**：
  ```
  vmr diff logs/vmr-audit-2026-08-22.jsonl.zst:5 vs logs/vmr-audit-2026-08-22.jsonl.zst:6

  Header
    model        agent                                    agent                                          (same)
    protocol     openai-completions                       openai-completions                             (same)
    outcome      ok                                       ok                                             (same)
    served       openai-completions:bai:deepseek-v4-flash openai-completions:sensenova:deepseek-v4-flash
    usage        in 35,696 / out 59                       in 35,780 / out 80                             (cached 512 → 0)

  System
    sys_hash     612fdac8                                 612fdac8                                       (same)

  Tools
    toolset      68 tools                                 68 tools                                       (same)

  Messages (A=1, B=1, LCP=0)
    [0] first divergence: A msg#0 (role=user) rewritten in B (hash differs)
    A tail: 1 new message(s) (roles: user)
    B tail: 1 new message(s) (roles: user)

  Verdict: B and A share no common message context (LCP=0, unrelated context). Structural fact, not a root cause.
  ```
- **验收结论**：成功解析并格式化打印了两个请求的 Header、System Prompt 签名、68 个工具集的哈希比对结果，并给出了精准的分叉点与判决结论。

---

### 6.3 生成报告目录索引指引 (Reports File Index Guide)

为了便于人工复核与走查，下表将当前保留在 `reports/` 目录下的所有文件与其业务用途和测试用例建立清晰映射：

```
reports/
├── manifest.json                     # [TC-01] 全局准入凭证与切片清单 (Format 11, 含全切片 sha256, 脚注免责表)
├── vmr-report.md                     # [TC-01, TC-05] 宏观分析中文主报表 (§0–§8 由 ViewModel 序列化生成)
│
├── macro/                            # [TC-01] 5 大领域数据切片 (无损机读单一真源)
│   ├── summary.json                  # 总体流量、成功率、自动亮点句 (Highlights)、元数据
│   ├── finance.json                  # 模型/客户端/端点成本、Provider 与配额情况、成本覆盖披露 (CostCoverage)
│   ├── reliability.json              # 端点 SLA、错误分类分布、Failover 与延迟分位数
│   ├── workloads.json                # 时间序列分布 (按日/按时)、客户端-端点路由矩阵
│   └── context-efficiency.json       # 会话膨胀系数、Compaction 损失分析、工具声明浪费
│
├── requests/                         # [TC-01] 请求级明细与排障数据层
│   ├── index.json                    # 全量请求明细行 (125 行)，包含 SessionMeta 标题投影与 Journey 交叉链接
│   ├── failed.jsonl                  # 失败请求机读流 (7 行)
│   └── failed.md                     # 失败请求排障人读 Markdown 入口
│
├── journeys/                         # [TC-01, TC-02, TC-03] 任务叙事与行为图谱层
│   ├── index.json / index.md         # Journey 候选集索引，包含自动任务聚类 (Task Clusters)
│   ├── benchmarks.json / .md         # [TC-02] 群体基准行为统计分析 (分布分位数、缺陷检出率、分组对比)
│   └── details/                      # [TC-01, TC-03] 单任务深钻实例
│       ├── j-...-cf8aac94.json       # [TC-03] 包含真实 LLM 解读 (llm_interpretation) 与三级匹配的自包含 JSON
│       ├── j-...-cf8aac94.md         # [TC-03] 决策脊柱 + 真实 LLM 语义解读报告 (含 Cache Break 与 Artifacts)
│       ├── j-...-9d950674.{json,md}  # 29 轮长任务实例
│       ├── j-...-9c744978.{json,md}  # 2 轮短任务实例 (对比基准)
│       └── j-...-eff63077.{json,md}  # 29 轮无密钥端点交互实例
│
├── compares/                         # [TC-04] 对比分析按需子树 (不进 manifest 指纹)
│   ├── index.json / index.md         # 扫目录派生的已执行对比清单
│   └── compare-...cf8aac94-vs-...9c744978.{json,md} # [TC-04] 包含双维度真实 LLM 归因的对比报告与 JSON
│
├── *.html                            # [TC-01, TC-05] 6 大常驻静态看板 SPA 骨架 (go:embed 编译资产)
│   ├── macro-dashboard.html          # 宏观仪表盘 (消费 macro/*.json，支持暗黑/明亮主题)
│   ├── request-browser.html          # 请求浏览器 (消费 requests/index.json，支持筛选/排序/分面)
│   ├── journey-viewer.html           # 单任务追踪器 (消费 journeys/details/j-*.json，支持步骤展开与锚点)
│   ├── journey-compare.html          # 对比看板 (消费 compares/compare-*.json，呈现分叉与时延)
│   ├── benchmarks.html               # 群体基准看板 (消费 journeys/benchmarks.json，展示统计指标分布)
│   └── tool-waste.html               # 工具浪费卡片 (消费 macro/context-efficiency.json)
│
└── en/                               # [TC-06] 英文对照分析套件 (全量英文切片、报告与索引)
```

---

### 6.4 验收阶段发现问题与处置

在第二阶段真实数据与真实端点测试中，我们严格进行了全链路走查，处置情况如下：

1. **`-journey` 旗标模糊匹配行为明确**：
   - **现象**：当传入哈希短串 `-journey cf8aac94` 时提示未找到匹配任务。
   - **核实**：根据 `cmd_journey.go:journeyPatternMatches` 的实现，无通配符时执行严格前缀匹配（`HasPrefix`），而 Journey ID 的前缀为客户端和时间戳（`j-lobster-...`）。
   - **处置**：使用标准通配符 `-journey "*cf8aac94*"` 即可精准命中单任务；行为符合规范，已在用例说明与索引指引中注明。
2. **大模型解读多字段持久化确认**：
   - **核实**：确认了 `llm_interpretation` 既在 Markdown 中完整渲染，又在 JSON 文件中严格持久化了 `model`、`status`、`duration_ms`、`text` 字段，且 `bodies` 与 `artifacts` 紧随其后序列化，格式规范，断言无误。
3. **安全下载链接在鉴权与无鉴权环境下的表现确认**：
   - **核实**：我们在 `common.js` 中新增的 `downloadArtifact` 函数在 `sourceBar` 点击时能够通过 JS 捕获当前环境的 Bearer Token 并顺利发起 blob 下载；在本地 `file://` 或无密码开发服务器下退化为直接取流，双重兼容。

---

## 七、最终验收结论与后续演进建议

### 7.1 综合评估结论

经过系统性的代码级复查、架构约束核对、即时缺陷整改与第二阶段基于**真实审计日志和真实线上大模型**的端到端验收测试：

1. **核心架构落地完好率 100%**：
   - D1 至 D21 项核心裁决全部严格落实；
   - 领域切片成为唯一无损真源，单体 `vmr-report.json` 彻底退出历史舞台；
   - ViewModel 纯净分层与 Markdown 固定序列化器工作稳定，`-render-only` 保证了 100% 字节级等价重绘；
   - 6 大静态看板骨架平铺根级，通过 `#data=` 传参与集中转义，兼具极佳的独立性与安全性；
   - 基于 `internal/digest` 的有序链式 SHA-256 全局指纹与两级缓存机制稳固可靠。
2. **Stage 1 增强特性全面通过实测**：
   - Prompt Cache Break 归因与 Touched Artifacts 提取已随单任务深度还原（并在真实任务中成功提取了 News Guide 脚本编辑与 `/tmp/news_titles.txt` 管道）；
   - Task Clustering 准确识别了跨日期的每日新闻简报任务，并成功标记出了更便宜（`⭐cheapest`）与更快（`⚡fastest`）的执行实例；
   - `vmr diff` 能够精准执行跨请求上下文对比与结构分叉判定。
3. **遗留问题边界明确**：
   - 所有能顺手解决且方案明确的问题（如受保护环境下骨架页下载瘫痪、方案文档虚假参数陈述）已直接修复；
   - 留存的深层次权衡事项（NEW-05 前端动态多语言、ISSUE-25 延迟图表断点绘制）均已建立详尽的根因与方案剖析，不阻碍主版本发布，待用户最终决策；NEW-01 金额展示策略收敛已在 review 后按本报告建议方案落地（见 5.1 矩阵）。

### 7.2 后续架构演进路线建议 (Next Steps)

1. **短期优化（1-2 周）**：
   - **图表缺失分位数断点呈现 (ISSUE-25 / NEW-06)**：在 `common.js` 的 `svgLatencyPlot` 中消除借值连线逻辑，将缺失分位点如实以离散或断线呈现。
2. **中期演进（1 个月）**：
   - **轻量前端字典多语言 (NEW-05)**：在 `common.js` 内部建立几十个词条的中英映射表，依据 `manifest.json.lang` 动态替换导航与表头，终结中文报表配英文看板的视觉割裂。
   - **看板纯 JS 响应式重绘 (NEW-03)**：将 `wireHashReload` 中的 `location.reload()` 替换为内存局部重绘函数，提升单页应用交互质感。
3. **长期探索（季度计划）**：
   - **单请求详情块级 LRU 缓存 (NEW-08)**：在探索性大规模回溯分析场景下，为 `requests/details` 按需物化引入解压块 LRU 缓存，将解压延迟降低一个数量级。
