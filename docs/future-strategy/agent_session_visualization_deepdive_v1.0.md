// Ver 2026-09-08 01:10, by custom_2/agent（§11 重核重写 + 附录 A 三阶段重规划，均由 pi/coding 按当前代码基线于同日完成）

# 面向 AI Coding Agent 的会话追踪、多维比对与基准评测平台全景深度调研报告 (v1.0)
## ——从数据采集、实体建模、界面呈现、比对机制到 Benchmark 体系的系统性架构解构与三梯队能力综合推演

> **文档性质**：面向 AI Coding Agent 基础设施的系统性架构调研报告。以 `trace.evot.ai` (Evot Eval) 为第一样板，横向地毯式深入解构当前开源生态中五大派系、八大核心项目（`trace.evot.ai`、`claude-tap`、`ATIF 生态`、`Inspect AI`、`Promptfoo`、`Langfuse`、`AgentOps`、`Arize Phoenix`）在**会话轨迹采集、单任务深度可视化、多会话横向比对、以及自动化基准评测（Benchmark）**上的工程设计范式。在统一的五维框架下逐项深度剖析并提炼 VMR 借鉴点，建立行业能力三级梯队（Essential / High-Value / Nice-to-Have），并严格对照自研 VMR 基线输出详尽的现状差距与演进落地路线图。

---

## 目录
- [0. 调研背景与统一分析框架构建](#0-调研背景与统一分析框架构建)
- [1. 第一样板剖析：trace.evot.ai (Evot Eval) 深度解构](#1-第一样板剖析traceevotai-evot-eval-深度解构)
- [2. 派系一：Coding Agent 本地 Trace 实时观测器 —— claude-tap](#2-派系一coding-agent-本地-trace-实时观测器--claude-tap)
- [3. 派系二：标准化轨迹格式与开放数据生态 —— ATIF 生态工具链](#3-派系二标准化轨迹格式与开放数据生态--atif-生态工具链)
- [4. 派系三：国家安全级前沿 Agent 评测工作台 —— UK AISI Inspect AI](#4-派系三国家安全级前沿-agent-评测工作台--uk-aisi-inspect-ai)
- [5. 派系四：声明式提示词与模型矩阵自动化测试工具 —— Promptfoo](#5-派系四声明式提示词与模型矩阵自动化测试工具--promptfoo)
- [6. 派系五（A）：企业级全栈 LLM/Agent 可观测性平台 —— Langfuse](#6-派系五a企业级全栈-llmagent-可观测性平台--langfuse)
- [7. 派系五（B）：自主 Agent 录像回放与死循环阻断系统 —— AgentOps](#7-派系五b自主-agent-录像回放与死循环阻断系统--agentops)
- [8. 派系五（C）：标准化 OpenInference 观测与图谱分析引擎 —— Arize Phoenix](#8-派系五c标准化-openinference-观测与图谱分析引擎--arize-phoenix)
- [9. 八大系统全景横向能力矩阵大表](#9-八大系统全景横向能力矩阵大表)
- [10. 核心能力三梯队提炼：Agent 运行时分析系统的本质要求](#10-核心能力三梯队提炼agent-运行时分析系统的本质要求)
- [11. VMR 对照差距分析与架构演进落地路线图](#11-vmr-对照差距分析与架构演进落地路线图)
- [12. 置信度评估与信息缺口](#12-置信度评估与信息缺口)

---

## 0. 调研背景与统一分析框架构建

### 0.1 调研背景与核心问题意识
随着 AI Coding Agent（Claude Code、Cursor、Codex CLI、OpenClaw、Pi、Evot 等）在高复杂度真实软件工程中的普及，开发者的核心痛点已从“单个 Prompt 能否生成正确函数”全面转移为**“长程（Multi-turn）、多步（Multi-step）、高并发异步工具调用任务下的可观测性危机、执行偏航排障、Prompt Cache 经济学损失以及跨模型/跨 Prompt 的确定性基准评测”**。

一个面向 Agent Runtime 的完整观测、比对与评测系统，大体上由数据采集、单任务深度可视化、多会话横向对比以及 Benchmark 评测四大阶段构成。为了避免陷入零散的功能罗列，必须确立严谨、公允且可横向对齐的**五维统一分析框架**。

### 0.2 五维统一分析框架（Unified 5-Dimension Framework）
本报告对所涉及的全部 8 个系统/生态，严格统一采用以下 5 个维度展开深度解剖：

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        五维统一分析框架 (Unified Analysis Framework)                   │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ 维度一：逻辑框架与概念实体模型 (Conceptual Entity Models & Lifecycle)                  │
│   • 抽象层级结构（Session / Turn / Step / Trace / Span / Observation / Event / Run）   │
│   • 实体生命周期管理、实体间映射与绑定关系、状态机模型                                 │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ 维度二：数据结构与 Schema 设计 (Data Structures & Schema Specification)                │
│   • 核心持久化存储引擎与 DDL / Schema 规范定义                                         │
│   • 字段命名与分组逻辑（Context / System / Messages / ToolCalls / Observations）        │
│   • Token 会计核算公式（显式区分 Cache Read/Write、Reasoning Tokens）与去重压缩机制     │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ 维度三：界面布局与可视化呈现 (Information Architecture & Visual Hierarchy)             │
│   • 页面顶层拓扑架构（Master-Detail 双栏 / 三栏 Resizable / 2D 矩阵大盘）               │
│   • 深度折叠策略（Progressive Disclosure）：系统提示词、Schema、历史消息与增量上下文    │
│   • 时序图谱、甘特图（Gantt）、状态机 DAG、微观 Diff 与富媒体渲染                       │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ 维度四：交互细节与控制机制 (Interaction Dynamics & Control Mechanics)                  │
│   • 全局检索与 DOM 联动展开、键盘导航流（j/k、视觉序绑定）                             │
│   • 多会话对比对齐算法（Tool Sequence Alignment、LCS、时间线平滑对齐）                 │
│   • 筛选控制机制（`Different` 差异一键过滤、状态码/标签组合筛选、时间轴 Scrubbing）     │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ 维度五：VMR 吸收借鉴与落地映射 (VMR Tactical Takeaways & Actionable Insights)           │
│   • 哪些具体算法、Schema 字段、视觉范式或控制机制可直接移植进 VMR 的分析半区           │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 1. 第一样板剖析：trace.evot.ai (Evot Eval) 深度解构

`trace.evot.ai` 是由 Databend (Datafuse Labs) 创始人 Bohu 团队主导构建的云端 Agent 追踪、对比与 SWE 评测平台，底层依托 Databend Cloud 云数仓。

### 1.1 维度一：逻辑框架与概念实体模型
* **实体层级结构**：
  $$\text{Task} \longrightarrow \text{Run} \longrightarrow \text{Comparison} \longrightarrow \text{Session} \longrightarrow \text{Span} \longrightarrow \text{ContentBlock}$$
* **实体关系与生命周期**：
  - **Task**：SWE-bench 风格基准测试集定义，包含环境镜像、`config.yaml`、初始提示词 `prompt.md` 与验证脚本。
  - **Run**：一次自动化跑测批次，调度指定 Agent 集合（`evot`, `claude-code`, `pi`, `dsh` 等）在指定 Model 下并行跑测任务。
  - **Session**：单个 Agent 实例完成特定任务的完整生命周期交互轨迹。
  - **Span**：单次 LLM 调用的原子封装，以自增序号 `seq` 严格标识，封装了由用户/环境触发、经模型推理产生工具调用、再到工具返回的闭环。
  - **Evaluator**：专职裁判模型，在任务完成后读取最终产生的 Git Diff，进行 `PASS` / `FAIL` 最终裁定。

### 1.2 维度二：数据结构与 Schema 设计
* **底层存储架构**：前端 SPA 原生调用后端 REST API，底层全量交互数据结构化入库至 Databend Cloud 数仓。
* **Span 核心 Schema 实测提取**：
  ```json
  {
    "id": "task3_FixJsonParsingBug-evot-20260906-152342/0000",
    "seq": 0,
    "start_time": "2026-09-06T08:12:25.214Z",
    "end_time": "2026-09-06T08:12:31.016Z",
    "duration_ms": 5802,
    "request_model": "gpt-6-astra",
    "status_code": 200,
    "stop_reason": "tool_use",
    "input_tokens": 0,
    "output_tokens": 38,
    "cache_read_input_tokens": 0,
    "cache_creation_input_tokens": 0,
    "n_messages": 1,
    "n_tools": 4,
    "n_tool_use": 1,
    "system_instructions": "...",
    "tool_definitions": [ ... ],
    "input_messages": [ ... ],
    "output_messages": [ ... ]
  }
  ```
* **Token 会计核算特色**：
  严格区分基础输入与缓存读取，并在前端强制计算：
  $$\text{cache\_pct} = \frac{\text{cache\_read}}{\text{input\_tokens} + \text{cache\_read}} \times 100\%$$

### 1.3 维度三：界面布局与可视化呈现
* **双栏 Master-Detail 布局**：
  - 左栏（Span List）：清晰罗列单步 `seq` 序号、Stop Reason、模型名、耗时条（Duration Bar，分色区分模型生成时间与等待时间）以及 Token 量。
  - 右栏（Span Detail）：
    1. **IO Params Bar**：模型名、max_tokens、Thinking 挡位、温度、流式标志；
    2. **Folds 区域**：System Instructions 与 Tool Definitions 默认折叠收拢，标明 Token 数量；
    3. **Context Flow**：区分“历史继承消息（Carried-over）”与“本次增量消息（New Messages）”，历史默认收起，增量高亮展开；
    4. **Cache 强渲染**：表头以大字号琥珀色渲染 `↓ 12,450 tokens · 78% cached`；
    5. **Response & Execution**：将模型的 `thinking` 过程、`text` 描述与发起的 `tool_use` 紧密成对吸附在 `tool_result` 上。
* **多会话对比看板（Comparisons）**：
  - **Execution Timeline**：跨 Agent 耗时横向甘特图；
  - **Tool Call Sequence Alignment**：工具调用序列左右并排对齐，缺失步骤补齐对齐虚线；
  - **Token Breakdown**：横向对比各 Agent 的请求次数、Token 构成、美元总成本与总耗时。

### 1.4 维度四：交互细节与控制机制
* **两两请求 Diff 模式 (`Diff: off/on`)**：在 Trace 列表中勾选任意两步，右侧自动切换为分栏 JSON 对比，精准高亮 Prompt 或环境反馈的细微差异。
* **动态 Agent 过滤**：在多 Agent 对比视图中，支持在列表头部即时勾选/隐藏特定 Agent，甘特图与矩阵自动重算。
* **实时轮询机制**：针对处于 `RUNNING` 状态的任务，前端每 5 秒轮询增量数据并展示呼吸灯状态点（`pulse-dot Live`）。

### 1.5 维度五：VMR 吸收借鉴点
1. **Tool Sequence 对齐矩阵**：直接作为 `vmr story -compare` 文本与报表输出的核心范式；
2. **显式 Cache Read 百分比渲染**：在 `vmr report` 表头强化 `Cache Hit: XX.X%` 渲染；
3. **Context Flow 增量高亮设计**：区分历史继承上下文与新引入上下文，极大提升长对话排障效率。

---

## 2. 派系一：Coding Agent 本地 Trace 实时观测器 —— claude-tap

### 2.1 维度一：逻辑框架与概念实体模型
* **定位**：面向本地开发环境的零侵入网络层黑匣子记录仪（⭐3.2k，Python）。
* **实体映射**：
  $$\text{Session} \longrightarrow \text{Turn / Record} \longrightarrow \text{Action (ToolUse/Thinking)} + \text{Observation (ToolResult)}$$
  每次 HTTP/WS 交互生成一个 Record，语义轮次自增分配 `turn`；引入不可变内容寻址大块实体 **`Blob`** 解决系统提示词与工具集的高频重复问题。

### 2.2 维度二：数据结构与 Schema 设计
* **SQLite3 存储规范 (`traces.sqlite3`)**：
  采用 `sessions`、`records`（复合主键 `session_id + record_index`）与 `record_blobs`（内容寻址表，主键 `session_id + hash`）三表架构。
* **`json-blob-ref` 内容寻址压缩机制**：
  当 `instructions` 或 `tools` 序列化体积超过 512 字节时，自动替换为：
  ```json
  {
    "__claude_tap_blob_ref__": {
      "hash": "e3b0c442...",
      "size": 34821,
      "kind": "json"
    }
  }
  ```
  该设计使长任务日志体积缩减 **80%~95%**。

### 2.3 维度三：界面布局与可视化呈现
* **Sticky Action Bar 布局**：右侧详情视窗滚动时，当前 Turn 序号、模型名、花费与 Diff 触发按钮强制吸顶。
* **Prompt Cache 断点可视化诊断 (`diffCachedRegion`)**：
  在前端视觉上以琥珀色精确标注：System Prompt 漂移、Tool 签名变动、历史截断点等导致 KV 缓存失效的物理断点。
* **Mobile-First 零横向滚动**：移动端自动切换为单栏栈式布局与堆叠式 Diff。

### 2.4 维度四：交互细节与控制机制
* **严格 DOM 视觉序键盘导航**：`j`/`k` 按键移动严格绑定侧边栏当前可见 DOM 顺序，折叠项自动跳过。
* **全局搜索与自动展开联动 (`autoExpandSearchMatches`)**：搜索命中时，自动递归展开收拢的 JSON 树与折叠面板，高亮并平滑居中。
* **原生 URL Embed 参数控制**：支持 `?embed=true&hideHeader=1&density=compact` 供第三方控制台无缝 iframe 嵌入。

### 2.5 维度五：VMR 吸收借鉴点
1. **移植 `diffCachedRegion` 缓存击穿断点算法**；
2. **移植 `json-blob-ref` 存储去重压缩机制至 VMR 持久化层**；
3. **采用 `autoExpandSearchMatches` 交互逻辑优化搜索体验**。

---

## 3. 派系二：标准化轨迹格式与开放数据生态 —— ATIF 生态工具链

本派系围绕 Harbor Framework 提出的 **ATIF (Agent Trajectory Interchange Format v1.7/v1.8)** 标准及其周边生态展开。

### 3.1 维度一：逻辑框架与概念实体模型
* **核心标准 (Harbor RFC-0001)**：
  - **原子性模型**：单个 Step（`source: "agent"`）**同时封装该步的推理、工具调用动作（`tool_calls`）以及环境对其返回的观察反馈（`observation`）**，终结了传统网络监控将请求与响应割裂成两个孤立回合的弊端。
  - **子代理双解析度（Dual-Resolution）**：清晰区分 `session_id`（运行期父子共享）与 `trajectory_id`（文档唯一），支持将 Subagent 作为合法的独立 Trajectory 递归内嵌或通过引用解耦。
  - **确定性编排与压缩标记**：`llm_call_count = 0` 标识规则/代码分发步骤；`is_copied_context` 标识为压缩滑动窗口复制进来的历史，SFT 数据导出时自动过滤。

### 3.2 维度二：数据结构与 Schema 设计
* **ATIF 严密 Token 会计核算公式**：
  $$\text{non\_cached\_prompt} = \text{prompt\_tokens} - \text{cached\_tokens}$$
  $$\text{cost\_usd} = (\text{non\_cached\_prompt} \times P_{\text{in}}) + (\text{cached} \times P_{\text{cache}}) + (\text{completion} \times P_{\text{out}})$$
  杜绝了各模型厂商对输入 Token 是否包含 Cache 定义模糊的问题。
* **DuckDB 零拷贝即席数仓架构 (`atif-sql`)**：
  利用 DuckDB 的 `read_json` 与 `UNNEST`，无需预处理 ETL，直接在内存中将磁盘上的 ATIF JSON 映射为纯关系型表（`steps`, `tool_calls`, `todo_state_current`）。

### 3.3 维度三：界面布局与可视化呈现
* **`atif-lens` 的 Activity 步骤智能折叠**：
  将 Agent 连续执行的多次只读工具（如连续 8 次 read/glob）合并为单个紧凑卡片（“*8 actions executed: read, glob...*”），并内置前后保留 3 行的 **3-Line Context Diff** 代码查看器。
* **`ATIF-trajectory-viewer` 的三栏 Resizable “Film Screen” 画布**：
  - 左栏（19%）：时间轴，带有 `±N` 文件资产突变徽章与 AFT 失败归因标识；
  - 中栏（53%）：**Environment Stage 画布**，像电影荧幕一样真实还原当前步代码编辑器瞬时全貌或终端渲染；
  - 右栏（28%）：原始报文、评测得分、以及全局修改文件清单（`Changes`）。

### 3.4 维度四：交互细节与控制机制
* **时间轴自动播放器（Playback Controls）**：支持以 1x/2x 速率自动遍历播放 Agent 解题全过程。
* **资产突变驱动跳步（RunArtifacts Jump）**：点击右侧修改文件列表中的任意文件名，时间轴自动精准定位并展开修改该文件的具体 Step。
* **三阶用户摩擦与异常挖掘流水线 (`atif-sql`)**：
  - 第一阶：Regex Fast-Path（毫秒级拦截用户的短否定词/停止词，置信度 0.9）；
  - 第二阶：确定性 SQL 规则（连续重复输入、报错后跟问号）；
  - 第三阶：后台 LLM 深度结构化归因并设置熔断预算。

### 3.5 维度五：VMR 吸收借鉴点
1. **全面将 ATIF v1.7 作为 VMR 标准轨迹导出与互通格式**；
2. **在 VMR 分析半区内置嵌入式 DuckDB 虚拟视图引擎**；
3. **吸收三栏 Resizable 布局与 Activity 智能折叠机制**。

---

## 4. 派系三：国家安全级前沿 Agent 评测工作台 —— UK AISI Inspect AI

### 4.1 维度一：逻辑框架与概念实体模型
* **定位**：英国 AI 安全研究所（UK AISI）开源的科研级评测框架（⭐2.7k，Python）。
* **实体模型**：
  $$\text{EvalSet} \longrightarrow \text{Task (Dataset + Solver/Agent + Scorer + Reducer + Sandbox)} \longrightarrow \text{Sample} \longrightarrow \text{TaskState} \longrightarrow \text{EvalLog}$$
* **科研严谨性设计**：
  - 引入沙箱与人机审批策略（`ApprovalPolicy`）；
  - 区分被测模型的真实 Answer 抽取与 Target 对比；
  - 引入 `Reducer` 实现跨 Epochs 的严密统计学聚合（均值、标准差、pass@k）。

### 4.2 维度二：数据结构与 Schema 设计
* **`ScoreReason` 细粒度归因机制**：
  明确区分为模型自身问题（`invalid_response_format`, `refusal`, `no_response`）与测量仪器/环境问题（`grader_failed`, `scoring_failed`）。
* **科学计分 `float("nan")` 哨兵**：
  当评分器异常时返回 `NaN`，在聚合统计中自动剔除，绝不将系统故障粗暴记为模型 0 分。
* **`ScoreEdit` 审计链**：保留机器初评与人工复核修改的历史轨迹。

### 4.3 维度三：界面布局与可视化呈现
* **三级宏观到微观导航**：`/tasks`（多 Run 指标矩阵大盘） $\rightarrow$ `/folders` $\rightarrow$ `/samples`（跨任务样本池）。
* **紧凑表头与热力图色阶 (Compact Scores & Heatmap)**：
  面对几十个评分指标，表头文字自适应 **45°倾斜排列**，列宽压缩至 38px；分数值映射为归一化渐变色阶热力图。
* **Agent 状态演化差分 (`StateDiffView`)**：
  内嵌 `jsondiffpatch` 引擎，深层 Object Diff 呈现每一步导致的沙箱环境与内部状态变更。

### 4.4 维度四：交互细节与控制机制
* **高级布尔检索 DSL**：支持在过滤框输入 `score == 'I' and metadata.difficulty == 'hard'`，附带动态字段自动补全。
* **HTTP 206 Range-Request 静态站点打包 (`inspect view bundle`)**：
  将巨型评测日志与前端 SPA 打包，利用 Range 请求按需流式分片拉取，无需起服务即可在静态云端秒级浏览万级样本。

### 4.5 维度五：VMR 吸收借鉴点
1. **引入 `ScoreReason` 异常归因与 `NaN` 哨兵机制**；
2. **在单步详情中集成 `jsondiffpatch` 状态差分树**；
3. **借鉴 45°倾斜紧凑表头优化多指标宽表展示**。

---

## 5. 派系四：声明式提示词与模型矩阵自动化测试工具 —— Promptfoo

### 5.1 维度一：逻辑框架与概念实体模型
* **定位**：业界事实标准的 CI/CD 提示词与大模型矩阵测试平台（⭐10k+，TypeScript）。
* **核心模型**：
  - `TestSuite` 定义笛卡尔积（$\text{Prompts} \times \text{Providers} \times \text{Tests}$）；
  - `defaultTest` 顶层统一继承安全与合规断言；
  - 自动展开为并发执行单元 `AtomicTestCase`，经过断言树（40+ 种 Assertions）计算，归约为二维对比宽表 `EvaluateTable`。

### 5.2 维度二：数据结构与 Schema 设计
* **声明式断言体系 (`AssertionSchema`)**：
  支持确定性规则（`contains`, `regex`, `is-json`）、代码脚本（Python/JS）、语义裁判（`llm-rubric`）、RAG 评价（`context-faithfulness`）以及智能体轨迹断言（`tool-call-f1`, `trajectory:tool-used`）。
* **`EvaluateTable` 二维结构**：严格将结构划分为自变量列（`head.vars`）与模型/Prompt 输出列（`head.prompts`），行内单元格（`outputs`）封装得分、耗时、成本与断言结果。

### 5.3 维度三：界面布局与可视化呈现
* **高密度 2D 矩阵看板 (`promptfoo view`)**：
  - 列头卡片展示 Provider 徽标、通过率百分比、动态过滤通过率、平均耗时与成本；
  - 单元格内嵌 `FailReasonCarousel`（失败原因轮播器，不撑破表格行高）；
  - 支持搜索关键词全表实时 Regex 高亮与多模态直接渲染。

### 5.4 维度四：交互细节与控制机制
* **跨历史 Run 水平级联比对 (`mergeComparisonTables`)**：
  勾选历史评测后，以 `testIdx` 为锚点在行内做水平拼接，列头带上 `[evalId]` 前缀，Prompt 调优的回归情况一目了然。
* **`Different` 杀手级筛选模式**：一键过滤掉全模型一致的行，100% 精力聚焦在最具争议和分歧的用例上。
* **零依赖单文件 HTML 看板 (`tableOutput.html`)**：
  仅 43KB 的单文件，内嵌纯 CSS 变量与原生 ES6 脚本，自带实时搜索、状态过滤与滑出式抽屉（Detail Drawer），无需任何服务器即可秒开。

### 5.5 维度五：VMR 吸收借鉴点
1. **引入 `mergeComparisonTables` 实现跨实验水平大比对**；
2. **引入 `Different` 差异过滤模式作为 VMR 默认比对视图**；
3. **开发零依赖单文件离线 HTML 报告导出器 (`vmr story --html`)**。

---

## 6. 派系五（A）：企业级全栈 LLM/Agent 可观测性平台 —— Langfuse

### 6.1 维度一：逻辑框架与概念实体模型
* **定位**：企业级全栈 LLM 追踪与观测平台（⭐34k+，TS / Next.js / ClickHouse / PG）。
* **核心模型**：
  $$\text{TraceSession} \longrightarrow \text{Trace} \longrightarrow \text{Observation (Span/Gen/Event/Agent/Tool)} \longrightarrow \text{Score}$$
  ClickHouse 底层采用**单表单态化物理存储、逻辑多态化枚举呈现**，通过 `parent_observation_id` 形成严格自引用调用树。

### 6.2 维度二：数据结构与 Schema 设计
* **ClickHouse 极速分析表族**：针对 `traces` 与 `observations` 表，以 `(project_id, toDate(timestamp), id)` 为主键，配合 Bloom Filter 索引与 ZSTD(3) 压缩。
* **完整 OTel 映射器 (`ObservationTypeMapper`)**：原生兼容 W3C TraceContext 与 OTel 属性。

### 6.3 维度三：界面布局与可视化呈现
* **ELK WebWorker 驱动的 DAG 拓扑图 (`ElkGraphRenderer`)**：
  支持 `aggregated`（折叠循环为状态机环路）与 `expanded`（展开为时序 DAG）双模式。
* **高密度时间轴与多实验比对大盘 (`DatasetCompareRunsTable`)**：
  横轴为 Runs，纵轴为 Dataset Items，单元格内并排展示多版本输出、延迟与方差。

### 6.4 维度四：交互细节与控制机制
* **60fps DOM 游标驱动引擎 (`playheadStore`)**：
  状态分离，位移计算完全绕过 React 虚拟 DOM 直接操作样式；无论 Trace 多长，统一平滑压缩至最多 10 秒回放完毕，微小 Span 保证至少 0.2 秒高亮闪烁。
* **Sentry 式空闲间隔压缩算法 (`timeCompression`)**：
  超过 Trace 总长 5% 的空闲时间折叠为固定 28px 的间歇波浪标。
* **超长 JSON 字节索引懒渲染 (`byteJsonIndex`)**：视口内动态解析，支撑数 MB 级上下文滚动。

### 6.5 维度五：VMR 吸收借鉴点
1. **空闲间隔压缩算法（`timeCompression`）直接搬入 VMR 耗时渲染**；
2. **TraceContext 标准透传与路由 Span 注入**；
3. **播放控制高频直写 DOM、低频响应式的性能纪律**。

---

## 7. 派系五（B）：自主 Agent 录像回放与死循环阻断系统 —— AgentOps

### 7.1 维度一：逻辑框架与概念实体模型
* **定位**：专注 Autonomous Agent 监控与会话回放的专业系统（⭐5.8k+，Python / Next.js）。
* **核心模型**：深度绑定原生 OTel Span，针对 CrewAI/Agno/AG2 等多框架提取特化属性（Persona, Role, Goal, Task）。

### 7.2 维度二：数据结构与 Schema 设计
* **细分 Reasoning Tokens 计量**：显式独立提取 `metrics.reasoning_tokens`（针对 o1/DeepSeek-R1 思考链）。
* **ClickHouse 原生 OTel 存储表族 (`otel_traces`)**。

### 7.3 维度三：界面布局与可视化呈现
* **自适应泳道 Gantt 耗时图 (`spans-gantt-chart`)**：利用 Recharts 堆叠条形图并注入透明 Filler Spans 占位块，以轻量图表库实现高精度甘特流。
* **ReactFlow 节点拓扑图与多框架专属视检器 (Framework Visualizer)**。

### 7.4 维度四：交互细节与控制机制
* **死循环与递归思维检测机制 (Loop & Recursive Thought Detection)**：
  采用滑动窗口特征哈希：
  $$\text{Fingerprint} = \text{Hash}(\text{span\_name}, \text{tool\_name}, \text{normalized\_args})$$
  并在连续窗口内检测重复调用与递归深度上限，自动触发熔断警示。
* **时间轴 Scrubbing 联动录屏回放**。

### 7.5 维度五：VMR 吸收借鉴点
1. **在 VMR 路由半区引入“死循环在线熔断器（Loop Breaker）”**，当下游 Agent 发生死循环振荡时主动返回 422 短路拦截，杜绝资金浪费；
2. **强化对思考链 Reasoning Tokens 的独立统计与计费**；
3. **从单请求重放扩展为全会话连续重放 (`vmr replay --journey`)**。

---

## 8. 派系五（C）：标准化 OpenInference 观测与图谱分析引擎 —— Arize Phoenix

### 8.1 维度一：逻辑框架与概念实体模型
* **定位**：推动并践行事实标准 OpenInference 的 AI 观测先驱（⭐11k+，Python / Relay React）。
* **实体模型**：`ProjectSession` $\rightarrow$ `Trace` $\rightarrow$ `Span`（标准 OpenInference 语义）。

### 8.2 维度二：数据结构与 Schema 设计
* **OpenInference 核心语义规范**：
  确立 `openinference.span.kind`、`graph.node.*` 拓扑属性及多级 `SpanCost` 明细。
* **树形预聚合计量字段**：在 Span 表结构中预先维护 `cumulative_error_count` 与 `cumulative_llm_token_count`，加速长程链路查询。
* **OTel Dot-Notation 智能解包算法**：支持将扁平点号键值无损还原为深层嵌套字典与数组。

### 8.3 维度三：界面布局与可视化呈现
* **树节点内嵌耗时进度条 (`TimelineBar`)**：
  直接在树列表行内，通过纯 CSS 百分比定位计算当前 Span 相对全局 Trace 的起止区间：
  $$\text{start\%} = \frac{\text{span.start} - \text{trace.start}}{\text{trace.duration}} \times 100\%$$
* **多 Agent 交接图谱 (Handoff Graph)** 与 **Token 比例双色切片条**。

### 8.4 维度四：交互细节与控制机制
* **树节点搜索穿透与自动展开 (`filterSpanTree`)**；
* **Relay 分页流式加载与 DOM 节点过载防护**。

### 8.5 维度五：VMR 吸收借鉴点
1. **借鉴行内内嵌微型 `TimelineBar` 的轻量可视化方案**；
2. **在离线分析状态机中引入累积预聚合索引（Cumulative Metrics）**；
3. **原生透传与规范化生成 W3C `traceparent`**。

---

## 9. 八大系统全景横向能力矩阵大表

| 系统 / 项目 | 派系分类 | 数据采集层机制 | 单会话呈现特色 (Single Session) | 多会话比对机制 (Multi-Session) | 基准评测能力 (Benchmark) | 离线看板形态 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **trace.evot.ai** | 第一样板 / 评测平台 | 应用层上报 + Databend Cloud | Span 模型 / Cache 显式百分比 / 增量消息高亮 | 时间线甘特图 / **Tool 序列对齐矩阵** / 任意两步 Diff | SWE 任务集 / 批量 Run 调度 / Evaluator 裁决 | Web SPA 平台 |
| **claude-tap** | 派系1：本地观测器 | 网络反向/正向代理拦截 (SQLite3) | Sticky Action Bar / **KV 缓存断点诊断** / 移动端自适应 | 上下文感知前一步 Diff / 手动跨步比对 | 无系统级评测体系 | 本地轻量 Web (带 Blob 去重) |
| **ATIF 生态** | 派系2：标准与工具链 | 日志转译 (atifact) / 嵌入式 DuckDB (atif-sql) | **3-Line Context 紧凑 Diff** / **Activity 连续步骤折叠** | DuckDB SQL 状态机重构 / 资产突变追踪跳转 | Harbor 评测基准数据底座 | atif-lens 组件库 / Viewer 三栏画布 |
| **Inspect AI** | 派系3：权威评测台 | 评测 Runner 内置驱动 (Docker 沙箱) | `inspect view` 树形流 / **`jsondiffpatch` 状态差分** | `/tasks` 跨 Run 指标大盘 / 样本池多维度对比 | 严谨科学评测 / `ScoreReason` 归因 / NaN 哨兵 | `bundle` 静态站 (HTTP Range 按需分片) |
| **Promptfoo** | 派系4：矩阵断言测试 | CLI 驱动批量矩阵跑测 | 格子内嵌失败轮播 / 实时 Regex 搜索高亮 | **`mergeComparisonTables` 水平拼接** / **`Different` 差异过滤** | 40+ 声明式断言 / 笛卡尔积测试矩阵 / 断言集 | **`tableOutput.html` 单文件完全零依赖 HTML** |
| **Langfuse** | 派系5：全栈可观测 | 客户端 SDK / OTel Span Ingestion | ELK 拓扑图 (聚合/展开) / **空闲时间折叠** / 懒解析 | `DatasetCompareRunsTable` 跨 Run 指标与输出 Diff | 在线数据集跑测 / Evaluator 在线评分 | Next.js 全功能云平台 |
| **AgentOps** | 派系5：可观测与回放 | Python SDK 自动 Hook / OTel | Recharts 堆叠 Gantt 瀑布流 / 框架专属视检器 | 录屏联动时间轴回放 / 多步错误定位 | 基础指标统计与通过率 | ReactFlow + Gantt 交互大盘 |
| **Arize Phoenix** | 派系5：标准化观测 | OpenInference 规范 / OTel Collector | **列表行内内嵌 TimelineBar** / Token 双色切片 | Handoff 多 Agent 拓扑流转 / 多版本实验对比 | Evals 模板与回归测试大盘 | Relay GraphQL 交互看板 |

---

## 10. 核心能力三梯队提炼：Agent 运行时分析系统的本质要求

基于对全网八大系统的解构与提炼，我们打破单项功能的碎片化认知，站在**“面向 AI Coding Agent 的运行时分析系统”**的高度，将其能力收敛为清晰的**三大梯队（Tiers of Capabilities）**：

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        Agent 运行时分析系统核心能力三梯队                              │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ 【第一梯队：必备基石 (Essential · 不可妥协)】                                          │
│   1. 网络层零侵入原始字节捕获与高保真还原 (Byte-Faithful Flight Recorder)              │
│   2. 基于原子 Turn/Span 的单任务深度折叠呈现 (Progressive Disclosure)                 │
│   3. Prompt Cache 显式化度量与击穿断点诊断 (Cache Economics & Breakpoint Diagnosis)    │
│   4. 任务资产突变追踪 (Artifact Mutation Tracking & Step Jump)                        │
│   5. 确定性两两会话跨步结构 Diff (Side-by-side Structural Diff)                        │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ 【第二梯队：高价值分水岭 (High-Value · 显著壁垒)】                                    │
│   6. 工具调用序列对齐矩阵 (Tool Sequence Alignment Algorithm)                          │
│   7. 跨历史版本横向水平级联对比大盘 (Horizontal Multi-Run Matrix with `Different` Mode)│
│   8. 下游 Agent 行为指纹与死循环在线熔断拦截 (Loop-Breaker Circuit Breaker)            │
│   9. 零依赖、单文件可交互离线 HTML 报告导出 (Zero-Dependency Offline HTML Deliverable)│
│  10. 机器可读的结构化异常归因体系与未评分哨兵机制 (ScoreReason Taxonomy & NaN Sentry)  │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ 【第三梯队：进阶与锦上添花 (Nice-to-Have · 锦上添花)】                                │
│  11. 状态机有向图拓扑渲染 (DAG Graph View: Aggregated vs Expanded)                    │
│  12. 时间伸缩平滑录像回放 (60fps Normalized Session Playback)                         │
│  13. 嵌入式轻量即席 SQL 数仓视图 (Embedded DuckDB SQL Engine over Transcripts)         │
│  14. 静态站点 Range-Request 分片发布器 (Static Range-Bundle for Massive Corpora)      │
│  15. 标准化轨迹交换规范互通导出 (Native ATIF v1.7/v1.8 & W3C TraceContext Export)      │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

### 10.1 第一梯队：必备基石能力（Essential · 缺一不可）
这是任何定位于严肃 Agent 运行时分析的工具**最基本、最核心必须具备的能力**。缺乏其中任何一项，该系统在实际工程排障与评估中就会出现致命盲区：
1. **网络层零侵入原始字节捕获与高保真还原**：必须具备在 L1/L2 网络代理层无损记录双层报文的能力，绝对保证 Tool Call 签名与流式 Chunk 100% 原始保真，不依赖侵入客户端代码。
2. **基于原子 Turn/Span 的单任务深度折叠呈现**：必须支持 Progressive Disclosure，将庞大的 System Prompt、工具 Schema 和历史轮次默认折叠收敛，仅展开关键增量上下文与工具交互结果。
3. **Prompt Cache 显式化度量与击穿断点诊断**：必须精确呈现 `X% cached`，并在发生未命中时能够指出是 System 变动、Tool 调整还是 Context 阶段导致的断点。
4. **任务资产突变追踪**：必须能够识别 Agent 在执行过程中所触碰的文件和命令，并支持从修改的文件一键反查对应的执行步骤。
5. **确定性两两跨步结构 Diff**：必须支持勾选任意两步或两次请求，进行结构化 JSON 与代码级文本比对。

### 10.2 第二梯队：高价值分水岭能力（High-Value · 决定行业壁垒）
具备第一梯队后，系统已经可用；而**第二梯队能力决定了产品是普通日志工具还是顶级专家分析平台**：
6. **工具调用序列对齐矩阵（Tool Sequence Alignment）**：采用对齐算法在多会话横向比对中左右展开工具链，一眼揭穿死循环与无效探索。
7. **跨历史版本横向水平级联对比大盘（含 `Different` 差异过滤）**：借鉴 promptfoo，以用例为锚点水平拼接多次 Run，并能一键过滤掉结果一致的无意义项，100% 聚焦在退化和分歧用例上。
8. **下游 Agent 行为指纹与死循环在线熔断拦截**：利用滑动窗口识别 Agent 自毁式循环，并在代理层主动拦截报错，直接保护企业资金安全。
9. **零依赖单文件可交互离线 HTML 报告导出**：单二进制直接渲染出内嵌 CSS/JS 的单文件 HTML，实现极低沟通成本的分发与交付。
10. **机器可读的结构化异常归因与 NaN 哨兵机制**：严格区分模型拒绝、格式错误与裁判自身崩溃，不污染科学评测的得分分母。

### 10.3 第三梯队：进阶特色功能（Nice-to-Have · 锦上添花）
在第一、第二梯队齐备后，系统完整度已达到行业标杆水平。第三梯队可在有余力时进一步拔高用户体验：
11. **状态机有向图拓扑渲染（DAG Graph View）**；
12. **时间伸缩平滑录像回放（Normalized Session Playback）**；
13. **嵌入式轻量即席 SQL 数仓视图（基于 DuckDB 零预处理查询）**；
14. **静态站点 Range-Request 分片发布器**；
15. **标准化轨迹交换规范互通（ATIF 导出与 W3C TraceContext 注入）**。

---

## 11. VMR 对照差距分析与架构演进落地路线图

> **本节于 2026-09-08 按 VMR 当前代码基线全面重核并重写**（重核起点 `050ad25`，121 个提交叠加，
> 独立审计记录见同目录 `analyze_redesign_final_audit_pi-coding.md`）。初版基线描述
> （"`vmr story` 纯文本"、"仅输出 Markdown/JSON 报表"、"零 Web UI"）已因 analyze 架构重构落地而整体过时，现校正如下。

### 11.0 基线校正（初版 §11 之后的重大变化）

- **CLI 已收敛**：`vmr report` / `vmr story` 子命令与 `-corpus` / `-story-only` / `-html` / `-redact` 旗标全部删除，
  单一入口 `vmr analyze`（zoom 模式：`-journey <id>` / `-compare <a>,<b>` / `-benchmark`）。初版 §11 中所有
  `vmr story --html` 式路线条目所引用的 CLI 形态已不存在。
- **HTML 看板已落地**：六个零依赖骨架页（`macro-dashboard` / `request-browser` / `journey-viewer` /
  `journey-compare` / `benchmarks` / `tool-waste`），`go:embed` + `common.js` 运行时内联、snake_case 数据契约、
  `<details>` 渐进折叠、零构建链（`TestSkeletonPages_NoExternalDependencies` 守卫）。"零 Web UI" 一说已不成立；
  但形态是**骨架 + fetch**，`file://` 直开不支持（设计裁决 D6/D13，`KNOWN_ISSUES` 登记）。
- **数据层已切片化**：五 macro 切片 + `manifest.json`（format=11，最后写准入）+ `requests/index.json`
  （`ts` epoch ms + `ts_display` 双字段、sessions 投影、journey_link 交叉链接）+ journey JSON 自包含
  （`bodies` blob 去重表、三级 `match` 工具配对、截断口径统一、`llm_interpretation` / `llm_divergence` 落 JSON）。
- **对比已结构化**：`-compare` 输出 9 项指标 diff + `ComparisonExtras`（端点 / 缓存逐轮曲线 / 系统提示词稳定性
  changes+excerpt / 最终上下文 / 时长与终止 / 最终交付物 / 成本 / 初始指令 / 证据溯源）+ **结构化分叉点**
  （两条 journey 对齐前缀中首个工具使用分歧步，数据层事实而非 LLM 推断）。
- **行为检测已成体系**：13 种行为 finding 码（`exact_repeat_tool_call`、`semantic_oscillation`、
  `error_retry_unadapted`、`goal_drift`、`plan_execution_misalignment` 等）+ benchmarks 群体检出率 +
  工具序列 N-gram 挖掘（含序列尾步错误率）。
- **规模**：~124K 行 Go，版本 v0.6.4+（Unreleased）。仍为单二进制、零外部数据库；分析半区离线只读审计日志。

### 11.1 能力对照矩阵（按当前代码逐项重核）

```
┌────────────────────────────────────────┬──────────┬────────────────────────────────────────────┐
│ 核心能力项                             │ 所属梯队 │ VMR 当前实现状态与差距评估                  │
├────────────────────────────────────────┼──────────┼────────────────────────────────────────────┤
│ 1. 网络层零侵入双层字节保真捕获        │ 第一梯队 │ ✅ 已完全具备（audit 双层记录；replay 复用  │
│                                        │          │    真实 Adapter 验证字节一致）              │
│ 2. 原子 Turn/Span 多级折叠呈现         │ 第一梯队 │ ✅ 基本具备（journey-viewer 决策脊柱：args  │
│                                        │          │    ≤160 内联 / <details> 展开、bodies 结果  │
│                                        │          │    正文、三级 match 徽标；request-browser   │
│                                        │          │    全量明细 + 分面筛选）                    │
│ 3. Cache 显式度量与击穿断点诊断        │ 第一梯队 │ 🟡 部分具备（cache_efficiency/cache_hit_rate│
│                                        │          │    双口径、逐轮命中率曲线、SysPrompt 稳定性 │
│                                        │          │    changes+excerpt/diff；缺工具签名变动检测 │
│                                        │          │    与 per-request 击穿断点定位）            │
│ 4. 任务资产突变追踪 (Artifacts)        │ 第一梯队 │ 🔴 缺失（未解析 edit/write 类工具产出触碰  │
│                                        │          │    文件清单；仅 compare 有 Final Deliverable│
│                                        │          │    单点）                                   │
│ 5. 确定性两两跨步结构 Diff             │ 第一梯队 │ 🟡 会话级已具备（compare 结构化分叉点 +    │
│                                        │          │    Extras + 看板 13 章节并排）；请求级两两  │
│                                        │          │    diff（vmr diff）缺失                     │
├────────────────────────────────────────┼──────────┼────────────────────────────────────────────┤
│ 6. 工具调用序列对齐矩阵 (Tool Align)   │ 第二梯队 │ 🟡 部分具备（分叉点用对齐前缀算法；        │
│                                        │          │    benchmarks 有 N-gram 序列挖掘；缺       │
│                                        │          │    compare 逐步 LCS 并排矩阵与缺步补齐）    │
│ 7. 跨历史水平级联比对 (`Different`)    │ 第二梯队 │ 🔴 缺失（benchmarks 是群体统计/分组对照，  │
│                                        │          │    非以用例为锚的水平拼接矩阵）             │
│ 8. 死循环在线熔断 (Loop Breaker)       │ 第二梯队 │ 🟡 检测✅ / 拦截❌（分析半区 13 种 finding │
│                                        │          │    + N-gram 挖掘；路由半区无会话级在线熔断）│
│ 9. 零依赖单文件离线 HTML               │ 第二梯队 │ 🟡 零依赖✅ / file://❌（六骨架页零外部   │
│                                        │          │    依赖、运行时内联；自包含单文件被 D6     │
│                                        │          │    裁决刻意废弃，fetch 需静态服务器）       │
│ 10. 结构化异常归因与未评分哨兵         │ 第二梯队 │ 🟡 同构机制在位（LLM 解读 status ok/failed │
│                                        │          │    +error 落 JSON；dur_low_n 低样本标记；  │
│                                        │          │    非 Anthropic 协议指标 n/a 免责；未定价 → │
│                                        │          │    CostCoverage 披露、成本行省略而非 $0；  │
│                                        │          │    评测计分体系不在 VMR 定位内 → N/A）      │
├────────────────────────────────────────┼──────────┼────────────────────────────────────────────┤
│ 11. 状态机 DAG 拓扑渲染                │ 第三梯队 │ 🟡 部分具备（ctxgraph 内存图 + lineage；   │
│                                        │          │    journey .md 有 ASCII/mermaid 工具时序；  │
│                                        │          │    无交互式 DAG UI）                        │
│ 12. 时间伸缩平滑录像回放               │ 第三梯队 │ 🟡 部分具备（replay 单请求 -print -req；   │
│                                        │          │    全旅程重放缺失）                         │
│ 13. 嵌入式即席 SQL 数仓视图            │ 第三梯队 │ 🔴 缺失（切片 JSON + jq/DuckDB read_json   │
│                                        │          │    即可查询，优先级低）                     │
│ 14. 静态站点 Range-Request 分片发布    │ 第三梯队 │ 🔴 暂不需要（D6 裁决：极简坚守，规模未到） │
│ 15. ATIF / W3C TraceContext 互通       │ 第三梯队 │ 🔴 缺失（内部 RequestFacts/coord 私有约定）│
└────────────────────────────────────────┴──────────┴────────────────────────────────────────────┘
```

**已吸收项对照初版各维度"VMR 借鉴点"清单**：Cache 显式百分比渲染（macro/compare 双口径，trace.evot.ai 范式）、
Context Flow 增量区分（journey-viewer 的 new_events 与 compaction 转场行）、Activity 智能折叠（看板
`<details>` 折叠 + journey 索引心跳类折叠）、会话级结构 Diff（compare Extras + 分叉点）、行为指纹检测
（13 finding 码 + N-gram）、Idle/低样本诚实呈现（`dur_low_n` / `n/a`，对应 Inspect AI 的 NaN 哨兵精神）等
均已落地。初版 §11.2 第 3 条（自包含单文件 HTML 导出器）与设计裁决 D6 冲突，按 D6 废弃——零依赖骨架形态
已覆盖其交互能力，唯一让渡是 `file://` 直开。

### 11.2 第一阶段：夯实第一梯队缺口（v0.7.0 · 短期速胜）

1. **Cache 击穿断点归因补全**（对齐 claude-tap `diffCachedRegion`）：
   - 现有底座：`SysPromptFact`（系统提示词漂移已检测）、`ctxgraph` 逐消息哈希与 compaction/编辑分类、
     compare 的逐轮 CacheFact 曲线。缺口是**工具签名（toolset）变动检测**与**逐请求"击穿因素定位"**。
   - 落法：在 journey/compare 侧增加 toolset-stability fact（manifest 已持有每步工具集数据）；request-browser
     明细行在 cache_read 显著低于前序请求时给出一行归因（`system changed` / `tools changed` /
     `history truncated`），数据来自既有哈希对比，不引入新解析。
2. **资产突变追踪器（Artifacts Extractor）**：
   - 在 `internal/chatmsg`（单点解析层，report/journey/ctxgraph 共享）增加工具调用特征识别：
     `edit`/`write`/`str_replace` 类结构化参数与 bash 写命令启发式，产出 (file, op, first_step, count) 清单，
     落 `j-<id>.json`（blob 去重底座已有）。
   - 消费端两处：journey-viewer 详情页"触达文件"面板 + 时间轴跳步（点文件名定位首次修改步，即 ATIF 生态
     RunArtifacts Jump 范式）；macro 侧 workloads 增加每任务触碰文件数分布。
3. **请求级两两 Diff（`vmr diff <coordA> <coordB>`）**：
   - 复用 `ctxgraph.ReqCoord` 请求身份与 reqdetail 已有的 `(record, manifest, prev)` 渲染底座，输出结构化差分：
     模型/端点/参数、system 差异（哈希对比）、messages 增量（ctxgraph 消息哈希集合差）、usage/缓存对比。
     比 claude-tap 的 JSON 树 diff 更强：消息级哈希让"差异出现在第几条历史消息"可精确定位。

### 11.3 第二阶段：突破第二梯队壁垒（v0.8.0 · 中期跨越）

1. **compare 工具序列全对齐矩阵**：
   - 现有分叉点计算已实现"对齐前缀"概念，把它升级为全序列 LCS 对齐并排矩阵（缺步补虚线），落
     `compare-*.json` 的 `extras.alignment`（数据先行，看板与 .md 同步消费），并排标注
     `Redundant Retry` / `Thrashing`（`exact_repeat_tool_call` finding 已提供判定依据）。
2. **路由半区 Loop Breaker（会话级在线熔断）**：
   - 复用分析半区已验证的指纹思路（工具名 + 归一化参数哈希滑窗），在 `internal/server` 请求事实层维护每会话
     滑动窗口；同一会话连续 N 步指纹重复即对该请求返回 `422` 并在响应头标注原因，冷却后放行。与既有熔断
     （上游健康 cooldown）正交：那是"上游不可用"，这是"下游在空转"。
   - 需要专门裁决：误杀率阈值标定（Agent 合法重试 vs 死循环），且默认关闭、按客户端 tag 开启。
3. **水平级联比对大盘 + `Different` 过滤**：
   - 以任务初始指令相似度 / 相同 taskseg profile 为锚，把多次 `-compare` 与 `-benchmark` 语料水平拼接为
     用例 × Run 矩阵（复用 `compares/index` 发现入口与 benchmarks 分组底座）；默认开启"仅显示分歧行"。
   - 这是把现有"一次一组"的 compare 升维成"跨历史批量"，数据层切片化后增量成本可控。

### 11.4 第三阶段：有节制地吸收第三梯队（v1.0.0 · 远期布局）

1. **ATIF v1.x 规范导出**（`vmr export --format atif`）：journey JSON 已自包含（tree + bodies），到 ATIF 的
   映射是纯转换层；价值在对接外部评测/微调工具链，属开放生态投资，按需启动。
2. **W3C `traceparent` 透传**：VMR 是路由层，注入/透传 trace 上下文的成本集中在 `server` 入口与上游
   transport 两处；受益场景是接入 Langfuse/Phoenix 的团队，非自研看板必需。
3. **全旅程重放（`vmr replay -journey <id>`）**：把一条 journey 的请求序列按序重放到另一个虚拟模型，
   复用 `replay` 既有真实 Adapter 底座；难点在会话状态的确定性重建（failover/sticky 语义需冻结），
   宜在请求级 diff 引擎（§11.2 第 3 条）稳定后实施。
4. **维持不做**（与初版一致的裁决）：嵌入式 SQL 视图（切片 JSON 已机读可查）、Range-Request 静态发布
   （产物规模未到阈值）。另按 D6 裁决，`file://` 自包含单文件不回归；若未来出现强离线分发需求，
   以"可选 bundle 模式"单独评审，不推翻骨架 + fetch 主形态。

---

## 12. 置信度评估与信息缺口

### 12.1 调研结论置信度评估
* **整体置信度**：**极高（Very High，96%）**。
* **事实依据来源**：
  - `trace.evot.ai`：直接实测调用其生产环境 REST API，深挖前端 `span-renderer.js` 与 `compare.js` 源码；
  - `claude-tap`：直接研读 `trace_store.py` SQLite DDL、`compact_trace.py` 压缩算法与前端 Diff 算法；
  - `ATIF 生态`：直接研读 Harbor RFC-0001 规范原文、`atifact` 解析状态机源码、`atif-lens` React 组件库与 `atif-sql` DuckDB DDL；
  - `Inspect AI`：直接研读 UK AISI 官方 Python Pydantic 源码（`EvalLog`, `Score`）与 `ts-mono` 前端实现；
  - `Promptfoo`：直接研读 `AssertionSchema`、`mergeComparisonTables` 源码及 `tableOutput.html` 模板实现；
  - `Langfuse / AgentOps / Phoenix`：直接研读 ClickHouse DDL、OpenInference 规范与 React 视图源码。

### 12.2 信息缺口与待跟踪项 (Information Gaps)
1. **ATIF 工业界被闭源商业工具采纳的进程**：ATIF 目前在开源社区与学术界进展迅猛，但 Cursor、Windsurf 等闭源巨头是否会在客户端原生导出 ATIF 仍需保持观察；
2. **Databend Cloud 面向海量高维 JSON 轨迹的成本曲线**：`trace.evot.ai` 支撑上万组长程轨迹的实际存储和分析查询单价数据未公开；
3. **AgentOps 死循环算法面对复杂反思流的误杀率**：当 Agent 自身具备 CoT 反思（Self-Correction）机制时，连续多次修改同一文件是否会被滑动窗口误判为死循环，其工业阈值配置需要进一步实践标定。

---

## 13. 交付物归档与局域网直链

* **文件系统落盘路径**：
  `projects/vmr_project/comparison_analysis/agent_session_visualization_deepdive_v1.0.md`
* **局域网在线直链（HTTP）**：
  <http://192.168.0.22:1980/data/lobsterai/deep-researcher/projects/vmr_project/comparison_analysis/agent_session_visualization_deepdive_v1.0.md>

---

## 附录 A · VMR Analyze 演进三阶段重规划 —— 基于代码的逐项可行性分析与 ROI 重估 (v1.1, 2026-09-08)

> 本附录是对 §11 路线图的**代码级重估与重构**。方法：对 §11.2–11.4 的每个要点回到源码勘察可行底座，
> 给出当前状况 / 具体做法 / 简略步骤 / 难度 / ROI；再从 §10 三梯队的第一性原理出发重新划定阶段范围。
> 所有行号与字段名核对自 `050ad25..68283e5`（121 commits）后的当前代码；独立审计记录见
> `analyze_redesign_final_audit_pi-coding.md`。
>
> **关键发现先行**：逐项勘察后，初版三阶段划分需要三处实质性修正——
> ① **W3C TraceContext 大半已具备**（不在初版路线图的假设里）；② **"水平级联比对矩阵"与 VMR 真实用例错位**
> （promptfoo 范式预设受控评测集，VMR 分析的是真实流量语料，无 pass/fail 真值），应降维为"重复任务聚类"；
> ③ **"全旅程重放"在语义上不成立**（工具结果来自原执行环境，重放到另一模型后第 2 步即失真），
> 应重构为"单步影子对比"。以下逐项展开。

### A.1 逐项代码勘察

#### A.1.1 Cache 击穿断点归因（初版 §11.2-1；**上调为第一阶段首选项**）

**相关源码**：
- `internal/journey/journey.go:112` — `Step.SysChanged`（系统提示词逐步变更检测，**已存在**）
- `internal/journey/journey.go:88` + `internal/ctxgraph/edit.go:22-72` — `Step.Edge.Kind`
  （EditKind 五分类：`Append` / `ReplaceTail` / `Splice` / `Contract` / `Fork`，逐边历史变更分类，**已存在**）
- `internal/ctxgraph/manifest.go:110-113` — `Manifest.SysHash`（系统块哈希，**已存在**）
- `internal/ctxgraph/manifest.go:103-105` — `Manifest.TraceID`（traceparent trace-id 已解析，**已存在**）
- `internal/journey/compare.go:241` — `CachePoint`（每步 cache hit ratio，**已存在**）
- `internal/ctxgraph/manifest.go:40` — `Manifest.Endpoint/ServedEndpoint`（每步实际服务端点，可检 failover 换端）

**当前状况**：缓存度量完备（双口径命中率 + 逐轮曲线），**但"为什么这一步没命中"没有归因**。
关键洞察：`EditKind` 本身就是缓存延续性的结构化判据——`Append` 意味着消息前缀完全复用（KV 缓存应延续），
`ReplaceTail` 意味着尾部改写（缓存在该点断开），`Contract`/`Fork` 意味着大段失效。把三者叠加
（EditKind + SysChanged + 端点切换 + 实测 cache ratio），**85% 的归因数据已经在落盘 JSON 里**。

**缺口**：唯一缺的输入是**工具签名（toolset）哈希**——`Manifest` 没有 `ToolsHash` 字段。

**具体做法**：
1. `ctxgraph.BuildManifest` 增加一行：`ToolsHash = hashMsgJSON(body["tools"])`（`chatmsg.ToolNames`
   已证明 `tools` 数组在 openai/anthropic 双协议下的位置一致；`hashMsgJSON` 已导出为 `HashMsgJSON`）。
   连带：`ctxgraph.CacheSchemaVersion` 8→9（NEW-A 的同一条纪律）。
2. `internal/journey` 新增 `CacheBreak` fact：对每个非首步，判定
   `{none | system | tools | history:<editkind> | provider_switch | unexplained}`——
   `unexplained` 是黄金输出：EditKind=Append 且 SysHash/ToolsHash 未变且端点未切，但实测
   cache_read 比例骤降 → 供应商侧缓存驱逐，这是用户自己查不出来的问题。
3. 渲染三处：journey `.md` 脊柱行、journey-viewer 步骤条、compare 的 Cache 章节加"击穿归因"列。

**步骤**：ctxgraph 字段 + schema bump + golden 重生成（0.5d）→ journey fact 计算 + 测试（1d）→ 三处渲染 + golden（1d）。

**难度** ★★☆☆☆ ｜ **ROI** ★★★★★（VMR 的核心差异化就是 cache 经济学；这是把"度量"升级为"法医学"的最后一公里，且几乎全部数据已就位）

#### A.1.2 资产突变追踪器 / Artifacts Extractor（初版 §11.2-2；第一阶段）

**相关源码**：
- `internal/journey/compare.go:519-529` — `deliverableFileKeys`/`deliverableContentKeys`：
  **参数形状匹配的现成先例**（`path/file_path/filepath/filename/file` 五键 + `content/text/body/data` 四键）
- `internal/journey/compare.go:535-557` — `deliverableStats`：从 `ToolCall.Args` 反序列化取字段、
  定位 step 序号、截断 —— **资产提取的全部机械动作已有可抄的模板**
- `internal/journey/structure.go:100-105` — `ToolCallRef.Repeat` 的注释明示纪律：**截断前的完整 args
  只在 build 时可用**，要盖章就趁 build 时（资产提取同理，必须在 structure build 阶段做，不能在渲染层做）
- `internal/chatmsg/messages.go:319` — `ToolCallList`（openai 形态）；anthropic 的 tool_use 块
  走 `RenderPart` 的 content-block 分支（`messages.go:82-96`）

**当前状况**：完全不追踪。仅有 `DeliverableStats` 单点（最终交付物），无全过程触达文件清单，无跳步。

**具体做法**：
1. `internal/journey` 新文件 `artifacts.go`：build 时遍历每步 `ToolCalls`，
   - 结构化工具：args 含 `deliverableFileKeys` 之一 → 记 `{file, op(write/edit/read→可省略), first_step, count}`；
   - bash/exec 类：对 args 里的命令串做窄正则（`>` `>>` `tee` `sed -i` `rm` `mv` `cp` `git apply` `mkdir`）——
     窄规则 + 免责标注（"启发式"），不做通用 shell 解析。
2. `JourneySummary.Artifacts []Artifact json:"artifacts,omitempty"`（D18 自包含契约的自然扩展；blob 去重已有）。
3. 消费端：journey-viewer 详情页"触达文件"面板（点文件 → `#data` 跳到首次修改步，RunArtifacts Jump 范式）；
   `.md` 决策脊柱后附一节；`journeys/index.md` 可选列。

**步骤**：提取规则 + 单测（1d）→ summary/structure 接线 + 守卫（0.5d）→ viewer 面板 + md 渲染（1d）→ macro 分布（可选 0.5d）。

**难度** ★★☆☆☆ ｜ **ROI** ★★★★☆（第一梯队 15 项里 VMR 唯一的整项空白；交互价值高；风险仅在于 bash 启发式的噪音，用免责标注对齐"never fake certainty"纪律）

#### A.1.3 请求级两两 Diff — `vmr diff <coordA> <coordB>`（初版 §11.2-3；第一阶段）

**相关源码**：
- `cmd/vmr/cmd_replay.go:20-42` — **坐标解析器现成**：`-req "basename:line"` 定位器已支持
  "省略文件 / 传目录 / 精确文件"三级解析，`vmr diff` 直接复用同一套定位逻辑（抽成共享 helper）
- `internal/ctxgraph/hash.go:95` — `HashMsgJSON` 已导出（消息级内容哈希，diff 的最小比对单元）
- `internal/ctxgraph/manifest.go:148` — `BuildManifest(rec, path, line)`：一条记录 → 消息哈希序列 + SysHash，即 diff 所需的全部结构化输入
- `internal/chatmsg/messages.go:191` — `chatmsg.Messages`（跨三协议的消息归一化）

**当前状况**：`vmr replay -print -req` 能倒出单条原始 JSON；`reqdetail` 渲染单请求全历史；**但没有"两条请求的结构化对比"**——用户只能靠肉眼 diff 两个 detail 页。

**具体做法**：
1. `cmd/vmr/cmd_diff.go`：定位两条 coord → 各自 `BuildManifest` → 对比输出：
   - 头部：模型/端点/outcome/usage/缓存 对比表；
   - system：`SysHash` 是否一致 + 首个差异摘要；
   - messages：哈希序列求 LCP 与逐位差异 → "第 N 条消息起分歧（role，来源：新增/改写/截断）"，
     内容差异给双方摘要（复用 reqdetail 的截断纪律）；
   - 复用 `i18n` 双语与 `fmtutil.DisplayZone` 时间显示纪律。
2. 纯 CLI 产物，不落 reports/、不进缓存指纹（无 L2 交互，零缓存纪律负担）。

**步骤**：定位器复用 + 骨架（0.5d）→ manifest 对比逻辑 + 测试（1d）→ 输出格式化 + i18n（0.5d）。

**难度** ★★☆☆☆ ｜ **ROI** ★★★★☆（把 claude-tap 的招牌交互以更强的形态拿到手：消息哈希让"差在第几条历史消息"可机器定位，这是对手做不到的）

#### A.1.4 compare 工具序列全对齐矩阵（初版 §11.3-1；第二阶段）

**相关源码**：
- `internal/journey/compare.go:615-637` — `toolSignature`/`stepToolSignature`（逐步工具签名，**已存在**）
- `internal/journey/compare.go:654` — `stepArgsEqual`（同工具名下参数对比，light/heavy 分级的依据，**已存在**）
- `internal/journey/compare.go:696-719` — `computeDivergence`：**只做前缀线性扫描，遇第一个分歧即返回**
- `internal/journey/structure.go:95-106` — `ToolCallRef`（每步工具调用 ref + args_ref，矩阵渲染的数据源）

**当前状况**：分叉点 = "对齐前缀的首个分歧步"。它回答"从哪一步开始不同"，但之后两条链各自怎么绕的
（A 重试了两次、B 换了路径、C 进了死循环）没有并排呈现。

**具体做法**：
1. `compare.go` 新增 `AlignmentFact{Pairs []AlignPair}`：对两链的步序列做 LCS 对齐
   （键 = `toolSignature.names` 有序集合；步数 ≤200，O(n·m) 可忽略）；
   每对状态：`same` / `args-differ`（复用 `stepArgsEqual`）/ `missing-a` / `missing-b`；
   已有的 `DivergencePoint` 成为对齐结果的自然衍生（首个非 same 对），两者同源不出两套算法。
2. 落 `compare-*.json` 的 `extras.alignment`（数据先行）；渲染：`.md` 并排表 +
   journey-compare.html 加"Sequence Alignment"章节（复用既有 13 章节骨架）；`Repeat` 盖章标 🔄。
3. 守卫：对齐结果与 `DivergencePoint` 一致性测试（同一对齐扫描派生两者）。

**步骤**：LCS + 单测（1d）→ json/md 渲染（0.5d）→ 看板章节 + smoke 断言（0.5d）。

**难度** ★★☆☆☆ ｜ **ROI** ★★★☆☆（分析价值真实但边际——分叉点已覆盖 80% 排障需求；成本极低所以仍值得做）

#### A.1.5 死循环检测与熔断（初版 §11.3-2；**拆成两半，第二阶段只做检测**）

**相关源码**：
- 检测侧（分析半区，已强）：`internal/journey/findings.go:82-98` — 13 种 finding 码含
  `exact_repeat_tool_call`（`findings.go:134` 注释明确"重复身份哈希在 build 时对未截断 args 计算"）、
  `semantic_oscillation`、`error_retry_unadapted`；`internal/journey/structure.go:100` — `ToolCallRef.Repeat` 逐调用盖章
- 路由侧底座：`internal/adapter/fingerprint.go:46` — `SessionFingerprint(raw, protocol) → (sysHash, firstMsgHash)`
  （**sticky 会话键现成**，`internal/sticky/sticky.go:93` Registry 已按此键组织，含 `NewBounded` 有界注册表先例）
- 热路径解析：`internal/server/facts.go:75` `computeRequestFacts` 已对每请求做 body 顶层扫描（tools/images）；
  `internal/chatmsg/messages.go:319` `ToolCallList` 可复用（注意它是 openai 形态，anthropic 形态需走 content-block 分支——与 A.1.2 同一坑）
- 配置纪律：严格 YAML `KnownFields`（`config.go:279` analytics.serve 先例），双语文档同步

**当前状况**：分析半区检测体系行业领先（13 码 + 检出率 + N-gram）；**路由半区零拦截**——同一会话原地打转时 VMR 照单全收，费用照烧。

**具体做法（拆两半，风险分离）**：
1. **第二阶段（检测，warn-only）**：`server` 在既有 facts 扫描处顺带提取末尾 N 个 tool_use 指纹
   （`hash(tool_name, normalized_args)`，滑窗比对本会话 sticky 键下的历史窗口）；命中则
   (a) 盖进 `audit.Record.Facts`（新字段 `loop_suspect bool`）——**路由半区盖章、分析半区消费**，
   完全符合 AGENTS.md "record it instead of reverse-deriving" 模式（`Attempt.IsForwarded` 同款）；
   (b) `/status` 计数器 + 日志告警。**不拦截任何请求**，零业务风险，且立即产生分析价值
   （report §3 可靠性侧新增"疑似空转会话"统计）。
2. **第三阶段（拦截，须裁决）**：`analytics.loop_breaker: {enabled: false, window, threshold, action}` 默认关，
   按客户端 tag 白名单开启；动作 `422` + 响应头标注。**前置条件**：用检测阶段的真实数据标定误杀率
   （合法重试 vs 空转），并出一节 Part 1 设计裁决（路由半区行为变更按仓库纪律须回写设计文档）。

**步骤（检测半）**：指纹提取 + 会话滑窗（bounded map 复用 Registry 模式）（1.5d）→ audit 盖章 + 守卫（0.5d）→ /status 与 report 消费（1d）。

**难度** ★★★★☆（整体）／★★☆☆☆（仅检测） ｜ **ROI** 检测 ★★★★☆ ／ 拦截 ★★★☆☆（价值高但频率低、误杀代价大；warn-only 先行把"值不值得拦"变成有数据支撑的裁决）

#### A.1.6 重复任务聚类（**替代**初版 §11.3-3 水平级联矩阵；第二阶段）

**相关源码**：
- `internal/journey/compare.go` — `InitialInstructionFact`（双侧开场指令全文已有提取器，`initialInstructionStats`）
- `cmd/vmr/cmd_journey_setup.go:78-87` — `journey.PreviewTitles` 批量标题提取（候选集锚点现成）
- `internal/journey/benchmarks.go:199-207` — `GroupComparison`（分组对照的先例形态）
- `internal/journey/journeyindex.go` + 实测语料（`reports/journeys/index.json`：46 journey / 29 task / 多客户端）

**当前状况**：`compares/index` 只列"跑过哪些对比"（D21），**没有任何机制发现"哪些 journey 其实是同一个任务的多次尝试"**。真实语料已出现同任务重跑（换模型后重试同一任务正是 VMR 用户的核心场景）。

**为什么不做初版原案**：promptfoo 的 `mergeComparisonTables` 以**受控评测集的用例为锚**（同一 testIdx 跨 Run 拼接），前提是存在稳定的用例身份与 pass/fail 真值。VMR 分析的是**真实流量**：没有人工标注的用例集、没有裁判打分，硬套"用例 × Run 矩阵"没有锚点也没有判据。第一性原理下，真实语料能回答的问题是：**"这个任务跑过几次？哪次花得最少/最快/结果最好？"**——这才是水平比对的正确形态。

**具体做法**：
1. `internal/journey` 新增聚类：键 = 初始指令的归一化 token 集合 Jaccard 相似度 ≥ 阈值（无 LLM、可解释；
   taskseg 已有指令边界切割 `NewUserWindow`，锚点文本质量有保证）；输出
   `clusters: [{anchor_title, members: [{id, model, cost, wall, net_working_ms}]}]` 落 `journeys/index.json`。
2. 消费端：`journeys/index.md` 聚类分组呈现 + 每簇给出"最快/最省"标注；
   journey-viewer 候选列表按簇分组；每成员旁附可复制的 `vmr analyze -compare <a>,<b>`。
3. 阈值进 report.yaml 并入 L2 分析参数指纹（`ComputeAnalysisParamsFingerprint` 已有位，加一个字段）。

**步骤**：相似度 + 聚类 + 单测（1d）→ index.json/md 渲染（0.5d）→ viewer 分组（0.5d）。

**难度** ★★☆☆☆ ｜ **ROI** ★★★☆☆（把 compare 的发现成本从"记得两个 ID"降到"从簇里点选"；是 D21 发现入口的自然延伸；诚实降维后反而可落地）

#### A.1.7 ATIF 规范导出（初版 §11.4-1；第三阶段，触发驱动）

**相关源码**：`internal/journey/summary.go:29-59`（`JourneySummary`：tree + bodies + metrics 全量自包含）+
`structure.go`（`StepStructure` 的 tool_calls/args_ref/result/match 三级）——**导出所需的全部事实已自包含**，
映射是纯转换层，零新解析。

**当前状况**：无任何外部轨迹格式接口；内部 `ReqCoord` / `EndpointLabel` 为私有约定。

**具体做法**：`vmr export -format atif` 读 `j-<id>.json`（不碰审计日志）→ 按 ATIF step 语义映射
（Step → step，ToolCall+Result → tool_calls + observation，match 三级保留进扩展字段）。
golden 测试钉住映射。

**难度** ★★☆☆☆ ｜ **ROI** ★★☆☆☆（转换本身容易，但当前用户群无 ATIF 消费方——**价值取决于外部生态对接需求出现**；列为触发驱动：一旦有对接 Harbor/评测管道的实际需求再启动，预计 2 人天）

#### A.1.8 W3C TraceContext 互通（初版 §11.4-2；**大幅降级：大半已具备**）

**相关源码（勘察结论与初版假设相反）**：
- `internal/router/clientheaders.go:21-46` — `headerBlocklist` **不含 `traceparent`/`tracestate`**
  → 客户端的 W3C TraceContext 头**已经字节保真透传到上游**（这是路由半区的既有行为，无需开发）
- `internal/ctxgraph/manifest.go:103-105` — `Manifest.TraceID` **已在分析半区被消费**
  （注释明示"trace-id 变更是强'新任务'信号"）——VMR 已经是 traceparent 的**受益者**
- 残余缺口仅一处：客户端**没发** traceparent 时 VMR 不会**生成**自己的 trace 链（route span 注入）

**当前状况**：透传 ✅、解析入图 ✅、生成 ❌。

**具体做法（若做）**：`server.chatHandler` 在入向无 traceparent 且 `analytics.generate_traceparent: true` 时
生成根 trace，出向 adapter 注入；`Manifest.TraceID` 已能接住。半天工作量 + 配置双语文档。

**难度** ★☆☆☆☆ ｜ **ROI** ★☆☆☆☆（仅当用户接入 Langfuse/Phoenix 做外部观测时有价值；VMR 自身看板已覆盖。登记为"已具备项 + 可选尾巴"，从路线图主线移除）

#### A.1.9 全旅程重放 → **重构为"单步影子对比"**（初版 §11.4-3；第三阶段）

**相关源码**：`cmd/vmr/cmd_replay.go`（单记录重放，复用 `adapter.BuildRequest` + `router.NewUpstreamClient`
真实链路——字节级可信）+ `internal/audit` 双层记录（含**上游响应**）。

**当前状况**：单请求重放已可用；无多目标、无 journey 级。

**为什么不照做初版"journey 顺序重放"**：语义上不成立——第 2 步起的请求体里携带的是**原环境的工具结果**，
换一个模型后它发出的 tool_use 没有真实环境执行、后续历史与实测脱节，产物既不是原任务的复现也不是新任务的开始，
是两边都不是的合成物。Agent 运行时在客户端侧，VMR 是路由器，这是定位边界不是工程缺口。

**可行的真需求**："**同一条上下文，两个模型各自怎么答**"——这是模型 A/B 评测的原语：
1. `vmr replay -req <coord> -provider A,B,C`（多目标）：同一请求体逐个发往多个 provider，
   输出并排对比表（响应摘要 / usage / cache / 耗时 / $）。
2. 可选 `-json out.json` 落盘（含双侧响应 hash），供后续 diff。
3. 注意事项：重放会真实计费与消耗配额（replay 已有的既有语义）；多目标 = N 倍费用，输出里如实标注。

**难度** ★★☆☆☆ ｜ **ROI** ★★★☆☆（换模型前的一键 A/B，贴合 VMR "稳定虚拟模型名背后随意切换" 的产品叙事；工作量小）

#### A.1.10 维持不做项（复核后维持，附触发条件）

| 项 | 维持不做的依据 | 触发条件 |
|---|---|---|
| 嵌入式 SQL 数仓视图 | 五切片 + `requests/index.json` 已机读，jq/DuckDB `read_json` 即查（分析半区瓶颈是内存不是查询，`KNOWN_ISSUES` 既有结论） | 用户出现复杂即席分析的重复需求 |
| Range-Request 静态发布 | 产物规模未到阈值（单目录数千文件的是懒物化 details/，本就不该被列表） | 单套产物 > 数千 journey/百万行请求 |
| `file://` 自包含单文件 | D6 裁决（fetch 需静态服务器；`vmr start` / `python3 -m http.server` 均可） | 出现强离线分发需求 → 以"可选 bundle"单独评审 |
| 交互式 DAG UI | ctxgraph 图已服务 lineage/stitch/compaction 分析；journey .md 有 ASCII 时序网格；看板时间线已覆盖导航 | 排障中反复出现"看不清 lineage 分叉结构"的诉求 |

### A.2 第一性原理重估：从 §10 三梯队出发的范围调整

以 §10 的本质要求为标尺重读 VMR 的定位——**"路由层黑匣子 + 离线法医学"，不做 Agent 运行时**——
得到三条修正原则，全部以上述代码事实为据：

1. **数据已就位的归因类能力优先于任何新采集**（A.1.1/A.1.2/A.1.3 的底座 85% 存量复用）。
   §10 第一梯队的五项里，VMR 的短板从来不是"采不到"，而是"采到了没说出来"：SysHash、EditKind、
   TraceID、CachePoint、deliverableFileKeys 这些事实早已逐步落盘，缺的是把它们**叠加成答案**的最后一层。
   这是全表 ROI 最高的一族。
2. **与"路由器不执行工具"的定位冲突的能力，重构为不冲突的形态**：
   journey 顺序重放（✗）→ 单步影子对比（✓）；受控用例矩阵（✗）→ 真实语料任务聚类（✓）；
   在线拦截（先 ✗）→ 盖章检测先行、拦截后置裁决（✓）。
3. **分析半区能力全速推进，路由半区行为变更单独设闸**：分析半区是只读旁路，错了改了重来；
   路由半区动的是热路径与资金安全，任何行为变更须过 Part 1 设计裁决 + 误杀率数据。
   Loop Breaker 按此拆半：检测（分析消费、审计盖章）进第二阶段，拦截进第三阶段且默认关。

### A.3 重定的三阶段路线图

#### 阶段一（v0.7.0 · "把度量变成法医学"）—— **已全部实施落地（2026-09-08）**

> 实施状态：全四项任务已全部落地并通过全套单元测试、回归守卫与真实审计日志实测验证。
> 包括：ToolsHash 入库与 schema bump 9、Step 层 CacheBreak 归因事实与三处渲染、Touched Artifacts 资产突变追踪与跳步、
> `vmr diff` 请求级结构化对比命令、以及基于 Jaccard 相似度的任务聚类（Task Clusters）。

| # | 任务 | 状态 | 落地内容概要 |
|---|---|:---:|---|
| 1 | ToolsHash 入 Manifest + Cache 击穿归因 fact + 三处渲染 | ✅ 已完成 | `ctxgraph.CacheSchemaVersion` 8→9；Manifest 增加 `ToolsHash`；Step 增加 `cache_break`（`system`/`tools`/`provider_switch`/`history:*`/`unexplained`）；在 decision spine、journey-viewer 徽标及 compare 渲染 |
| 2 | 资产突变追踪器 + journey-viewer 跳步面板 | ✅ 已完成 | `internal/journey/artifacts.go` 提取结构化文件参数与 Shell 启发式；`JourneySummary.Artifacts` 落盘并在 Markdown 附录与看板表格呈现，支持一键平滑滚动跳步 |
| 3 | `vmr diff` 请求级结构化对比 | ✅ 已完成 | `cmd/vmr/cmd_diff.go` 支持坐标解析，对比 Header/System/Tools/Messages LCP 与差异，输出结构化免责 Verdict |
| 4 | 重复任务聚类 + index/viewer 呈现 | ✅ 已完成 | `internal/journey/clusters.go` Jaccard 相似度聚合重复任务，`journeys/index.json` 与 `index.md` 输出对比分组与一键 `vmr analyze -compare` 建议 |

合计 ~9 人天；全部产出进 manifest 盖章范围或为纯 CLI；失效矩阵 / L2 指纹按既定纪律同步
（新增 report.yaml 键入 `ComputeAnalysisParamsFingerprint`）。

#### 阶段二（v0.8.0 · "把检测变成对齐与保护"）

| # | 任务 | 预估 | ROI | 依赖/闸门 |
|---|---|---|---|---|
| 1 | compare LCS 全对齐矩阵（A.1.4） | 2 人天 | ★★★☆☆ | 无 |
| 2 | Loop 检测 warn-only：审计盖章 + /status + report 消费（A.1.5 前半） | 3 人天 | ★★★★☆ | 路由半区；只增观测不改行为 |
| 3 | Loop 拦截（A.1.5 后半） | 2 人天 | ★★★☆☆ | **闸门**：用任务 2 的真实数据标定误杀率 + Part 1 设计裁决 + 默认关 |

#### 阶段三（v1.0.0 · "生态与评测接口"，触发驱动）

| # | 任务 | 预估 | ROI | 触发条件 |
|---|---|---|---|---|
| 1 | 多目标影子对比 `replay -provider A,B,C`（A.1.9） | 1.5 人天 | ★★★☆☆ | 用户有换模型 A/B 需求 |
| 2 | ATIF 导出（A.1.7） | 2 人天 | ★★☆☆☆ | 出现外部评测管道对接需求 |
| 3 | traceparent 生成尾巴（A.1.8 残余） | 0.5 人天 | ★☆☆☆☆ | 用户接入外部 OTel 观测 |
| 4 | 维持不做项复核（A.1.10） | — | — | 按触发条件逐年复核 |

### A.4 与初版（§11.2–11.4）的差异对照

| 初版条目 | 重估后 | 一句话理由 |
|---|---|---|
| §11.2-1 Cache 断点 | **保留，上调为第一优先** | 底座 85% 已在（EditKind/SysChanged/SysHash/CachePoint），只缺 ToolsHash 一个字段 |
| §11.2-2 资产追踪 | 保留，第一阶段 | deliverableStats 已提供参数形状匹配模板，难度低于初版估计 |
| §11.2-3 请求级 diff | 保留，第一阶段 | replay 定位器 + HashMsgJSON 现成 |
| §11.3-1 对齐矩阵 | 保留，第二阶段（降半档） | 分叉点已覆盖八成价值；成本极低故仍做 |
| §11.3-2 Loop 熔断 | **拆半**：检测进二、拦截进三且默认关 | 误杀率需要真实数据标定；路由半区行为变更须设闸 |
| §11.3-3 水平级联矩阵 | **替换为"重复任务聚类"** | promptfoo 范式预设受控用例集与 pass/fail 真值，与真实流量语料错位 |
| §11.3 原"单文件 HTML" | （D6 裁决已废弃，见 §11.1） | 零依赖骨架形态已交付其交互能力 |
| §11.4-1 ATIF | 保留，触发驱动 | 转换容易但当前无消费方 |
| §11.4-2 traceparent | **降级为"已具备 + 可选尾巴"** | 勘察发现：不在 blocklist → 已透传；TraceID 已入图 |
| §11.4-3 全旅程重放 | **重构为单步影子对比** | 顺序重放语义不成立（工具结果无法复现）；单步同上下文 A/B 才是真需求 |
