<!-- Ver 2026-08-16 13:15, by gemini-3.7-flash -->

# Agent 运行时分析在 VMR 中的落地与演进路线图

> **版本**：v1.1（吸收历史方案深度技术规范与 Prompt 契约后的全面完善版）  
> **日期**：2026-08-16  
> **作者**：Gemini 3.7 Flash  
> **定位**：基于文献调研报告 `agent_runtime_analysis_v1.0_custom-2-agent.md`，结合 VMR（Virtual Model Router）现有架构与代码实现，全面剖析 Agent 运行时分析（Agent Runtime Analysis）的落地可行性，盘点现有功能缺陷与改进空间，评估未实现能力的多维代价与用户价值，并制定分阶段推进路线图。

---

## 0. 任务 Debrief 与执行计划

### 0.1 核心诉求与背景 Debrief
1. **背景**：VMR 现已具备透明代理三类协议（OpenAI Chat、Anthropic Messages、OpenAI Responses）并捕获完整全量 LLM 调用审计日志（`audit.Record`）的能力。前序调研报告构建了一套涵盖五层评估（黑盒、玻璃盒、白盒、行为模式、系统级）与跨 Agent 对比的方法论。
2. **核心任务**：
   - **可落地性提炼**：从学术/工业界全景指标中，区分“纯规则/确定性可落地”、“介于可落与不可落之间（可通过 LLM 模糊判断 + Prompt 调优转为可行）”以及“当前不可行/应舍弃”的边界。
   - **现有实现盘点与深度批判**：对照 VMR 当前代码库（`internal/story`、`internal/report`、`internal/ctxgraph` 等），盘点已落地的能力，深入指出其当前过于粗陋、启发式简陋、存在误判漏判的具体痛点及改进空间。
   - **未实现能力多维评估**：从未实现的能力中，从“用户价值”、“工程实现难度”、“数据积累与 LLM 识别调优难度（Eval / Feedback loop）”以及“性能/隐私/成本 ROI”等多维度综合打分。
   - **分期路线图与优先级排序**：将落地规划清晰划分为 Phase 1（近期重点/马上推进）、Phase 2（中期推进）、Phase 3（远期储备）。
   - **规范化 Prompt 与工程契约**：明确高价值判别器的输入证据包结构、Prompt 指令规范与 JSON 输出 Schema。
   - **架构与未尽事项说明**：说明零侵入只读契约、校准评测集构建、隐私与本地优先等关键工程哲学。

### 0.2 执行计划
- **Step 1**：解构调研报告，完成能力的可落地性分层分类（规则 vs LLM 模糊增强）。
- **Step 2**：深度剖析 VMR 现有代码实现（现状、局限性、优化方案）。
- **Step 3**：对待扩展能力进行多维矩阵评估（价值、工程难度、LLM 调优难度、ROI）。
- **Step 4**：输出分阶段落地路线图（Phase 1 / Phase 2 / Phase 3）与实施步骤。
- **Step 5**：给出核心 Prompt 规范与 Go 结构体契约。
- **Step 6**：总结架构约束、评测反馈闭环与未尽事项。

---

## 1. 运行时分析能力解构与落地可行性提取

基于调研报告的 5 大维度、47 项指标与 7 步分析流水线，结合 VMR **“本地运行、只读审计日志、零代码埋点（Zero-Instrumentation）、单二进制（Go）”** 的现实约束，将所有分析能力划分为三类：

```
┌────────────────────────────────────────────────────────────────────────┐
│                        能力落地可行性三层模型                          │
├────────────────────────────────────────────────────────────────────────┤
│  Layer A: 纯规则/工程级确定性落地（低延迟、零/低计算开销、100% 确定性） │
│  - 基础 Token/成本/延迟/吞吐核算、Manifest 结构性编辑分类              │
│  - 精确匹配死循环、Prompt Cache 命中曲线、工具调用 N-gram/时序图       │
├────────────────────────────────────────────────────────────────────────┤
│  Layer B: 规则初筛 + LLM 模糊增强判定（从“难以落地”转化为“高价值落地”）│
│  - 语义死循环/振荡（参数微调但语义相同）、目标漂移（Goal Drift）       │
│  - 推理-动作不一致（Reasoning-Action Mismatch）、计划合规与偏离分析   │
│  - 工具执行语义错误归因、工具结果曲解判定、跨 Agent 子目标对齐         │
├────────────────────────────────────────────────────────────────────────┤
│  Layer C: 理论上存在但当前不可落地/应舍弃（高侵入、高开销、伪需求）    │
│  - 需对 Agent 框架做代码插桩的内部白盒追踪（Memory State/Hidden State） │
│  - 全量实时 PRM（Process Reward Model）打分（推理性价比较低）          │
│  - 缺乏外部沙箱环境下的绝对真值判定（无代码沙箱执行反馈）             │
└────────────────────────────────────────────────────────────────────────┘
```

### 1.1 零埋点（Zero-Instrumentation）的物理边界
VMR 作为透明网络代理，只能看到网络上传输的 HTTP Request/Response 字节，**无法感知 Agent 进程内部的私有内存变量、未发给 LLM 的本地代码分支判断、以及非 LLM 调用的本地 I/O**。

| 可见（物理真实现实） | 不可见（物理绝对盲区） |
|---|---|
| 发给 LLM 的完整 `messages`（含 System, User, Assistant, Tool） | Agent 内部未拼入 prompt 的本地临时变量 |
| LLM 返回的 Thought/Reasoning、ToolCalls、Text | 本地没有发起的纯代码循环与死锁 |
| HTTP 状态码、网络延迟、Token 消耗、模型切换 | 客户端本地 CPU 消耗、非代理网络的本地 Disk I/O |
| 上下文拼接历史、Compaction 裁剪丢失的文字块 | 开发者心中未写入 prompt 的隐含预期 |

### 1.2 纯规则与工程级能力（Layer A：高确定性）
这类能力直接基于 `audit.Record` 的请求/响应元数据、Token 计量与消息结构向量，具备 100% 确定性与零额外 API 调用成本：
1. **Token 经济学与成本归因**：Input/Output/Cached Tokens，按模型/账号/客户端切分的成本核算。
2. **时间三段式分解**：模型推理耗时（`ModelMS`）、Agent 本地执行耗时（`AgentExecMS`）、人类等待耗时（`HumanIdleMS`）。
3. **结构化上下文演进**：上下文大小曲线、各 Role（System/User/Assistant/Tool）Token 占比演化、历史压缩（Compaction）前后 Token 变化。
4. **精确工具调用特征**：工具调用频次分布、N-gram 序列模式、完全重复调用（`(tool_name, args_hash)` 精确匹配）。
5. **协议级因果与错误标记**：Anthropic 协议原生的 `is_error` 响应、HTTP 状态码错误、连接中断。

### 1.3 LLM 模糊增强能力（Layer B：从不确定转为可行）
文献中大量高价值的过程级评估（如 Goal Drift、推理自洽性、工具使用合理性、结果曲解）过去常被视为“学术化不可落地”，核心原因是**硬规则极易产生海量误报（False Positives），而人工标注成本过高**。
然而，借助现代大语言模型强大的泛化与上下文理解能力，通过**“规则提取结构候选集（Candidate Filtering） + 针对性轻量 Prompt 判定（LLM-as-Judge） + 置信度约束”** 的混合范式，可以实现低成本高准确率落地：

| 评估领域 | 为什么纯规则难以落地 | LLM 模糊增强如何实现落地 | 预期识别潜力与优化空间 |
|---|---|---|---|
| **任务完成度四阶逼近** | 缺乏外部沙箱环境验证真实副作用 | 比对用户初始需求与最终输出，给出四阶状态（`COMPLETED_VERIFIED` / `COMPLETED_UNVERIFIED` / `PARTIALLY_COMPLETED` / `FAILED_ABORTED`） | 消除二元判断偏置，直击“声称完成但缺乏验证”的隐性缺陷 |
| **工具结果曲解检测** | 工具返回与模型推理存在自然语言鸿沟 | 扫描 `(ToolResult, NextReasoning)` 对，判定模型是否对否定/错误结果产生了相反的幻觉解读 | 捕获 RCA 调研中 71.2% 的高发推理故障（指鹿为马） |
| **语义死循环 / 振荡检测** | Agent 往往微调参数（如搜索词加同义词、分页偏移量+1、稍微修改命令），Hash 匹配失效 | 规则检测连续多次相同工具调用；LLM 判定这几步是否在**“语义停滞/原地打转”** | 随小模型推理能力提升，Prompt 可进一步指导模型识别探索性重试 vs 无效振荡 |
| **目标漂移 (Goal Drift)** | 关键词匹配无法区分正常子任务拆解与真正的主题跑偏 | 规则提取用户原始 Prompt 与当前 Step 的推理/动作；LLM 评估当前子目标是否仍服务于根目标 | 引入漂移置信度评分（0-100）及漂移点定位，支持阶段性总结提醒（Plan Reminder） |
| **推理-动作不一致** | 纯正则提取路径/实体与参数匹配存在巨大分词/语法格式偏差（误报率曾高达 90%） | 规则提取思维链推理最后一段与对应 Tool Call；LLM 判断动作是否忠实于推理意图 | 消除硬编码正则误报，支持复杂嵌套参数（如 JSON 内嵌脚本）的语义对照 |
| **非协议级错误与隐式失败** | OpenAI 协议下工具报错常封装在正常 200 OK 文本中（如 `FileNotFound`、`Command failed`） | 规则对工具输出进行错误特征初筛；LLM 语义判定该工具返回是否为“阻断性失败” | 建立错误分类器，精准识别 Agent 是否在面对失败时“假装成功” |
| **跨 Agent 轨迹子目标对齐** | 不同 Agent 步骤粒度不同（A 框架 1 步完成，B 框架拆 3 步），序列编辑距离失效 | 规则提取两侧的 Milestone 动作；LLM 将两侧轨迹对齐到相同的虚拟子目标骨架 | 真正实现“同题异构”Agent 在相同子目标下的步数、Token 与耗时公平对比 |

### 1.4 暂不落地/舍弃的能力（Layer C：伪需求与强外部依赖）
1. **Agent 内部未发出的隐藏思维/Memory 状态**：VMR 是网关代理，无法也不应侵入 Agent 框架内存。任何需要 Framework-level 插桩的指标（如 LangChain 内部状态变量）均不列入。
2. **端到端绝对成功率的自动二元真值判别**：在缺乏真实运行沙箱（如 SWE-bench Docker 执行环境）的离线场景下，仅靠 LLM 判断开放式任务“是否绝对完成”极不可信。VMR 应坚持“揭示事实与过程异常，而非给出虚假裁判”。
3. **逐 Token 级过程奖励模型（PRM）**：需要极高算力且模型泛化性差，严重违背轻量化、本地优先原则。

---

## 2. VMR 现有实现深度盘点与改进空间

### 2.1 现有实现全貌盘点
VMR 在 `internal/story`、`internal/report`、`internal/ctxgraph`、`internal/chatmsg` 等包中已经构建了坚实的离线分析底座：

```
                    ┌────────────────────────────────────────────────────────┐
                    │               VMR 现有分析体系全貌                     │
                    └────────────────────────────────────────────────────────┘
                                               │
               ┌───────────────────────────────┴───────────────────────────────┐
               ▼                                                               ▼
  ┌─────────────────────────┐                                     ┌─────────────────────────┐
  │ `vmr report` (聚合分析)  │                                     │ `vmr story` (单任务还原) │
  ├─────────────────────────┤                                     ├─────────────────────────┤
  │ - §1 Token 经济/缓存效率│                                     │ - 行为剖面 (九项指标)   │
  │ - §2 成本估算 (三层价目) │                                     │ - 决策脊柱 (Decision    │
  │ - §2.5 额度消耗实时对照 │                                     │   Spine + 角色标注)     │
  │ - §3-5 稳定性/延迟/负载 │                                     │ - 9 类 Step 级 Findings │
  │ - §6.5 Sticky 命中度量  │                                     │ - Compaction 信息损失   │
  │ - §6.6 端点性价比       │                                     │ - 双 Journey 对比与分叉 │
  │ - §6.7 Compaction 还原  │                                     │ - 语料级 Spearman 统计  │
  │ - §7 效率发现 (6类规则) │                                     │ - 可选 LLM 整体/分叉解读│
  └─────────────────────────┘                                     └─────────────────────────┘
               │                                                               │
               └───────────────────────────────┬───────────────────────────────┘
                                               ▼
  ┌─────────────────────────────────────────────────────────────────────────────────────────┐
  │ 共享底层基础设施 (`ctxgraph` 内容寻址图 / `chatmsg` 三协议归一 / `taskseg` 任务切分)    │
  └─────────────────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 现有实现的痛点、简陋之处与改进空间 (Deep Dive)

虽然 VMR 已经跑通了全套流程，但深入代码细节可以发现，大量核心能力仍处于**“基于简单正则与粗糙启发式”**的初代阶段，存在明显的改进空间：

#### 1. 实体提取基础设施（`chatmsg.ExtractEntities`）与连带失真
- **现有代码实现**：
  ```go
  // internal/chatmsg/entities.go
  var entityRe = regexp.MustCompile(`https?://[^\s"'` + "`" + `)]+|\b[\w][\w./\-]*\.[a-zA-Z]{1,6}\b`)
  ```
- **核心缺陷分析**：
  - 该正则仅匹配 URL 和带 1~6 位扩展名的文件名（如 `main.go`, `doc.md`）。
  - **严重漏报**：无扩展名的系统路径（`/etc/hosts`, `cmd/vmr`）、目录路径（`internal/story/`）、函数名/符号名（`ExtractEntities`）、CLI 子命令（`git commit`, `npm test`）、数据库表名等。
  - **连带影响**：直接导致依赖该基础函数的 5 个 Finding 检测器（`unused_tool_result`, `unverified_entity_reference`, `reasoning_action_mismatch`, `plan_execution_misalignment`, `constraint_text_dropped`）以及 `ContextUtilization` 均存在系统性失真。
- **改进空间**：
  - **规则层升级**：重构 `ExtractEntities`，支持提取：（1）以 `/` 或 `./` 开头的标准目录路径；（2）常见代码标识符/函数名；（3）CLI 命令名。
  - **语义层辅助**：在关键分析步骤中，允许通过上下文轻量提取高阶“操作对象（Action Targets）”。

#### 2. 工具错误与恢复检测（`ErrorRecoveryCount` & `findings_toolresult.go`）
- **现有代码实现**：
  ```go
  // internal/story/findings.go & metrics.go
  var verificationLikeToolRe = regexp.MustCompile(`(?i)read|get|list|check|verify|view|stat|cat|show|fetch|status`)
  const isErrorMarker = "❌ is_error"
  ```
- **核心缺陷分析**：
  - **通用执行工具漏判（严重假阳性）**：当 Agent 使用通用 Shell（如 `bash`, `execute_command`, `run_terminal`）执行 `go test ./...` 或 `npm test` 时，工具名叫 `bash`，未命中正则，系统会错误地认为该 Agent“报错后没有做任何验证就结束了”。
  - **单厂商协议偏置（严重假阴性）**：`isErrorMarker` 仅能捕获 Anthropic 协议自带的 `is_error: true` 字段。在 OpenAI、Gemini 或 DeepSeek 等无标准错误字段的协议中，工具输出在 stdout 中的 `"Error: 404 Not Found"` 或 `"FATAL: compile failed"` 完全无法被规则捕获，导致 `ErrorRecoveryCount` 大幅低估。
- **改进空间**：
  - **命令参数语义嗅探**：当工具为 `bash`/`exec` 等通用执行器时，递归检查其命令行参数中是否包含 `test`, `verify`, `check`, `lint`, `diff` 等验证命令。
  - **跨协议错误归一化嗅探**：在 `internal/chatmsg` 层面，针对 ToolResult 内容做轻量正则嗅探（`fatal`, `exit status 1`, `error:`, `failed`），填补非 Anthropic 协议的空白。
  - **LLM 最终核验**：由 LLM 综合判断任务终步声称的“成功”是否建立在已修复前序错误的基础之上。

#### 3. 计划与执行对齐检测（`plan_execution_misalignment`）
- **现有代码实现**：
  ```go
  // internal/story/findings.go
  var numberedListRe = regexp.MustCompile(`(?m)^\s*(\d+)[.、]\s*(.+)$`)
  ```
- **核心缺陷分析**：
  - **格式局限**：仅捕获首步思考中的 `1. 2. 3.` 格式，对 Markdown 任务列表（`- [ ]` / `- [x]`）、Bullet points、英文（`Step 1:`, `Phase 1:`）完全无法识别。
  - **动态规划盲区**：只看首步（First Step），对长任务中极为常见的“中途动态重规划（Mid-journey Replanning，如出现新 Bug 后重拟计划）”完全无法追踪。
  - **字面匹配失真**：仅靠提取出的实体字符是否在后续文本中出现作为判定依据，无法识别“计划了 A 却做了相反的 B”或“计划了但因报错中途放弃”。
- **改进空间**：
  - **格式解析扩展**：支持 Markdown Checklist (`- [ ]`, `- [x]`) 与 `Step N:` 等主流规划表达。
  - **动态规划感知**：不仅扫描第 1 步，对中间 Step 出现显著规划重构的，记录为 `Plan v2` 并重置跟踪基准。
  - **LLM 语义核销**：利用单 Journey LLM 解读层，对提炼出的关键计划条目进行真正的“逐项语义完成度核销”。

#### 4. 思考与行动脱节检测（`reasoning_action_mismatch`）
- **现有代码实现**：
  - 仅截取 Reasoning 的最后一句（`lastSentence`），提取实体并与当前步 ToolCall 参数比对。
- **核心缺陷分析**：
  - 句子切分过于死板：若模型在倒数第二句做出了动作决定，最后一句写的是过渡句或总结（如“接下来我们观察运行结果。”），规则会误判为“思考与行动脱节”。
  - 复杂推理丢失：若模型的 Reasoning 是多阶段演绎（先分析 A，得出必须调用 B），规则只看末句容易误抓。
- **改进空间**：
  - 将扫描窗口从“最后一句”扩大为“末尾 2~3 句的动作意图窗口（Action-Intent Window）”。
  - 将高嫌疑的脱节候选提交给第二层 LLM 进行语义一致性确认。

#### 5. 重复工具调用与参数规范化（`exact_repeat_tool_call` & `error_retry_unadapted`）
- **现有代码实现**：
  - 严格要求参数字符串完全相等（`retry.Args == tc.Args`）。
- **核心缺陷分析**：
  - **等价参数漏网**：JSON 字典键顺序不同、参数中多了一个无意义的换行/空格、或仅仅加了一个 `--verbose` 标记，就会逃逸硬规则检测。
  - **同义死循环盲区**：例如连续 3 次调用不同的搜索工具搜索同一组关键词，或对同一个不存在的文件反复变换路径尝试。
- **改进空间**：
  - **参数 JSON 规范化 (JSON Canonicalization)**：在比对前对参数 JSON 做 Key 排序与 Whitespace 清洗。
  - **同义死循环识别**：引入滑动窗口 LLM 判断，捕获换汤不换药的无效重试。

#### 6. 单/双 Journey LLM 解读层的证据包深度（`SingleJourneyEvidencePack`）
- **现有代码实现**：
  - 目前证据包只传递了宏观的 Metrics、Findings 清单、Task 标题和单行 ToolIndex 摘要。
- **核心缺陷分析**：
  - **隔靴搔痒**：LLM 无法看到具体的报错详细内容、关键 ToolResult 文本或 Compaction 丢失的具体文字片段。
  - **产出受限**：LLM 解读只能机械复述“该任务共耗时 X 秒，发生了 2 次重复调用”，无法深入诊断“为什么第 8 步会选错工具”。
- **改进空间**：
  - **定向注入聚焦子证据包 (Focused Evidence Sub-pack)**：当规则层触发了 Finding 或定位了分叉点时，自动将触发点前后 2 步的原始 Request/Response 上下文切片打入证据包，使 LLM 具备微观病因诊断能力。

#### 7. Compaction 系统核心约束丢失检测（`FindingConstraintTextDropped`）
- **现有代码实现**：
  - 仅对比压缩前后被吞噬的实体（`SwallowedEntities`）。
- **核心缺陷分析**：
  - **非实体系统约束丢失**：如“请始终使用中文回答”、“严格禁止修改 package.json”、“保持向后兼容”等重要否定式约束在 Compaction 中被裁剪时，往往不包含具体文件名实体，导致现有检测器完全沉默。
- **改进空间**：
  - 引入 System Prompt 与前序指令的差分摘要，检测关键“否定式约束”与“行为规范”是否在压缩中丢失。

---

## 3. 待扩展能力的多维评估矩阵

为了科学排期，我们将文献中的所有潜在扩展能力，按照以下 4 个关键维度进行打分评估：
- **用户价值（User Value, 1-5 分）**：能否帮助用户发现致命 Bug、优化 Prompt、降低数百美金浪费或提升排障效率。
- **工程实现难度（Engineering Complexity, 1-5 分）**：代码结构、数据流管线、状态图处理及性能影响。
- **LLM/Prompt 调优与数据积累难度（LLM/Eval Tuning Difficulty, 1-5 分）**：Prompt 稳定性、幻觉控制、评测集校准复杂度、少量样本下的泛化能力。
- **单次运行成本与性能影响（Cost & Latency Overhead, 低/中/高）**：对内存、磁盘 I/O 及 LLM 调用费用的消耗。

### 3.1 待扩展分析能力全景评估矩阵

| 能力项 | 核心逻辑与技术路径 | 用户价值 | 工程难度 | LLM/Eval 难度 | 成本/开销 | 综合 ROI | 推荐分期 |
|---|---|:---:|:---:|:---:|:---:|:---:|:---:|
| **E1. 协议无关的通用工具错误嗅探** | 规则解析 Tool Result 中的通用 ExitCode / HTTP 状态 / JSON 错误键（覆盖 OpenAI） | **5** (高) | **2** (低) | **1** (极低) | 极低 (纯规则) | **极高** | **Phase 1** |
| **E2. 任务完成度四阶逼近判定** | 比对初始 Prompt 与最终回复，LLM 给出 `[VERIFIED/UNVERIFIED/PARTIAL/FAILED]` 状态 | **5** (高) | **2** (低) | **2** (低) | 低 (仅终步调用) | **极高** | **Phase 1** |
| **E3. 工具结果曲解与幻觉断言检测** | 扫描 `(ToolResult, NextReasoning)` 对，LLM 识别是否对报错产生了乐观幻觉解读 | **5** (高) | **2** (低) | **2** (低) | 低 (仅可疑触发) | **极高** | **Phase 1** |
| **E4. 参数微调型语义死循环/振荡检测** | 规则聚合同工具调用序列窗口 + LLM 判断参数微调是否缺乏实质进展 | **5** (高) | **2** (低) | **2** (低) | 低 (仅可疑触发) | **极高** | **Phase 1** |
| **E5. 目标漂移检测 (Goal Drift)** | 提取根目标与多阶段 Step 推理，LLM 计算漂移轨迹与子目标吻合度 | **5** (高) | **3** (中) | **3** (中) | 中 (按需/抽样) | **高** | **Phase 1** |
| **E6. 智能计划合规与偏离分析 (PCPC)** | 支持 Checklist 与动态重规划，LLM 映射后续 Action 序列计算遵循度 | **4** (中高) | **2** (低) | **3** (中) | 中 (按需) | **高** | **Phase 1** |
| **E7. Compaction 否定式约束损失评估** | 对被截断/压缩丢弃的上下文，LLM 评估是否包含关键否定式约束与规范 | **4** (中高) | **2** (低) | **2** (低) | 低 (仅缝合点) | **高** | **Phase 1** |
| **E8. 上下文腐烂 (Context Rot) 阈值发现** | 语料级统计不同上下文长度分桶下的工具错误率/重试率突变拐点 | **4** (中高) | **2** (低) | **1** (极低) | 极低 (纯Go统计) | **极高** | **Phase 1** |
| **E9. 失败模式自动归因 (Fault Taxonomy)** | 基于 RCA/MAST 分类体系，LLM 对异常 Journey 自动生成根因定位卡 | **4** (中高) | **3** (中) | **4** (中高) | 中 (仅失败任务) | **中高** | **Phase 2** |
| **E10. 工具返回结果实际利用率 (Token-level)** | 追踪工具返回的具体数据在后续 Assistant 推理/回复中的引用与转化 | **3** (中) | **3** (中) | **3** (中) | 中 | **中** | **Phase 2** |
| **E11. 跨 Agent 子目标对齐与公平对比** | LLM 自动将两套异构轨迹归一化为标准子目标序列，分段对比 Step/Token | **4** (中高) | **4** (高) | **4** (中高) | 中高 (双侧解析) | **中高** | **Phase 2** |
| **E12. 质量-成本帕累托前沿分析 (Pareto)** | 结合 TQS 综合质量分与单位成本，绘制多模型/多 Agent 帕累托散点图 | **3** (中) | **3** (中) | **4** (依赖TQS) | 极低 (纯计算) | **中** | **Phase 3** |
| **E13. 错误级联传播路径反向回溯** | 从最终失败点出发，构建跨 Step 因果图并回溯定位首个错误引入点 | **4** (中高) | **5** (极高) | **4** (高) | 中高 | **中** | **Phase 3** |
| **E14. 决策方差与稳定性自动基准测试** | 同一任务多轮执行的轨迹相似度计算（编辑距离 + Embedding 对齐） | **2** (低) | **4** (高) | **2** (低) | 极高 (多次运行) | **低** | **Phase 3** |

---

## 4. 分期推进路线图与优先级排序

基于上述评估，我们制定清晰务实的三阶段实施路线图：

```
┌──────────────────────────────────────────────────────────────────────────┐
│                        VMR Agent 运行时分析路线图                        │
└──────────────────────────────────────────────────────────────────────────┘
  
  【Phase 1: 近期重点 / 马上推进】(聚焦高 ROI、基础修复、高价值 LLM 判别与纯 Go 拐点)
  ──────────────────────────────────────────────────────────────────────────
  1. 基础设施强化：`chatmsg.ExtractEntities` 正则重构与参数 JSON 规范化
  2. 规则准确率修复：跨协议错误嗅探与通用 Shell 测试命令识别
  3. 任务完成度四阶逼近判定 (Task Completion Evaluator: Verified vs Unverified)
  4. 工具结果曲解与幻觉断言检测 (Tool Misinterpretation)
  5. 语义死循环与振荡检测 (参数微调但缺乏进展的无效重试)
  6. 语料级 Context Rot 拐点分析 (纯 Go 分桶统计)
  7. 目标漂移检测 (Goal Drift：长程任务偏离预警)
  8. 智能计划遵循分析升级 (Checklist 支持 + 动态重规划追踪)
  9. Compaction 否定式核心约束丢失评估

  【Phase 2: 中期演进 / 下一阶段】(聚焦深度行为画像、证据包深潜与跨 Agent 对齐)
  ──────────────────────────────────────────────────────────────────────────
  1. 单 Journey LLM 证据包深潜：触发 Finding 时的局部上下文切片注入
  2. 故障模式自动分类 (Fault Taxonomy：认知层/执行层/环境层归因)
  3. 跨 Agent 轨迹子目标对齐 (Sub-goal Alignment：跨框架公平成绝对比)
  4. 工具输出真实利用率深度分析 (精确量化 Context Bloat 浪费)

  【Phase 3: 远期储备 / 审慎观望】(技术前置依赖重、ROI 有限或需外部环境)
  ──────────────────────────────────────────────────────────────────────────
  1. 错误级联传播因果图反向回溯 (Compounding Error Causal Chain)
  2. 质量-成本帕累托前沿看板 (Pareto Frontier & TQS)
  3. 自动化决策一致性与方差评测体系
```

---

### 4.1 Phase 1：近期重点（马上推进落地）

#### 目标与核心价值
解决现有规则误报率高、协议覆盖不全、长程任务“单步正确整体跑偏”的核心痛点，以极小工程改造撬动极高质量信号。

#### 具体落地模块
1. **P1.1 实体提取重构与参数规范化（纯工程，零 Token 成本）**
   - **实现路径**：重构 `internal/chatmsg/entities.go`，支持无扩展名路径（`/etc/hosts`）、目录路径（`internal/story/`）、代码符号（`ExtractEntities`）及 CLI 命令名；在 `internal/story` 中引入 JSON Canonicalization（键排序 + 空白清理）。
   - **收益**：瞬间消除 `unused_tool_result`、`exact_repeat_tool_call`、`unverified_entity_reference` 的大批假阳性与漏网之鱼。

2. **P1.2 协议无关的通用工具错误嗅探与 Shell 验证识别**
   - **实现路径**：
     - 在 `internal/chatmsg` 中扩展 `ToolResultList`，对工具返回内容进行智能检测：检查顶级 JSON 是否包含 `error`/`failed`/`exception` 字段；检查 Shell 工具输出是否包含 Exit Code > 0 或典型 Panic/Traceback 特征。
     - 修复 `looksLikeVerification`，使其在工具为 `bash`/`exec` 时深入检查命令行参数中的 `test`/`verify`/`check` 关键字。
   - **收益**：解决 OpenAI 流量上工具错误 100% 漏报的问题，并消除 Shell 执行测试时的假阳性。

3. **P1.3 任务完成度四阶逼近判定（`Task Completion Evaluator`）**
   - **实现路径**：在 `SingleJourneyEvidencePack` 中注入首步 User Prompt 与最终 Assistant 回复，由 LLM 输出 `[COMPLETED_VERIFIED, COMPLETED_UNVERIFIED, PARTIALLY_COMPLETED, FAILED_ABORTED]` 四阶枚举与未达成目标清单。
   - **收益**：打破“最终有答复就等于成功”的假象，直击“声称完成但缺乏实际验证”的隐性失败。

4. **P1.4 工具结果曲解与幻觉断言检测（`FindingToolResultMisinterpretation`）**
   - **实现路径**：扫描 `(ToolResult, NextReasoning)` 对，识别 Agent 是否对工具返回的明确否定/报错结果产生了相反的乐观幻觉解读（如工具返回 404，模型却推理“已成功获取数据”）。
   - **收益**：精准拦截学术界 RCA 调研中占比高达 71.2% 的推理层致命缺陷。

5. **P1.5 语义死循环与微调振荡检测器**
   - **实现路径**：滑动窗口（如 N=6 步）内，若同一工具连续调用 ≥ 3 次，即使参数 Hash 不相等，也将其打包为候选组，由 LLM 判断“这几步操作是否在缺乏进展的情况下原地打转”。
   - **收益**：精准捕捉翻页死循环、搜索词微调死循环等高成本浪费场景。

6. **P1.6 语料级 Context Rot 拐点分析（纯 Go 实现）**
   - **实现路径**：在 `vmr story -corpus` 中，按 Context 窗口分桶（0-32k, 32-64k, 64-128k...）统计 Finding 密度与报错率，拟合质量拐点。
   - **收益**：零 LLM 成本帮助开发者掌握当前模型在长上下文下的质量劣化阈值。

7. **P1.7 目标漂移（Goal Drift）检测**
   - **实现路径**：长程任务中，每隔 K 步抽取当前推理摘要，由 LLM 计算漂移轨迹与子任务吻合度。

8. **P1.8 计划合规性（PCPC）与动态重规划追踪**
   - **实现路径**：支持 Markdown Checklist (`- [ ]`, `- [x]`) 与 `Step N:` 解析；支持多阶段动态重规划识别（Plan v1 -> Plan v2）；接入 LLM 语义核销。

9. **P1.9 Compaction 核心约束丢失评估**
   - **实现路径**：检测 System Prompt 中的关键否定式约束（如“禁止修改 package.json”）是否在压缩中丢失。

---

### 4.2 Phase 2：中期演进（下一阶段推进）

#### 目标与核心价值
构建结构化、体系化的 Agent 行为模式与故障归因能力，深化单 Journey 诊断深度，支持高质量的跨 Agent / 跨模型横向对齐对比。

#### 具体落地模块
1. **P2.1 单 Journey LLM 证据包深潜升级（Focused Evidence Sub-pack）**
   - **实现路径**：在 `internal/story/llm_single.go` 中，当触发 Finding 或定位关键转折点时，定向注入触发点前后 2 步的原始 Request/Response 片段，让 LLM 解读从“宏观概括”升级为“代码级微观病因诊断”。

2. **P2.2 故障模式自动分类（Fault Taxonomy）**
   - **实现路径**：基于 MAST 与 Agentic AI Fault 分类，建立标准的故障标签枚举（L1 认知编排、L2 执行工具、L3 记忆感知、L4 环境限制），产出标准化《故障归因诊断卡》。

3. **P2.3 跨 Agent 轨迹子目标对齐（Sub-goal Alignment）**
   - **实现路径**：由 LLM 提取两套异构轨迹的逻辑里程碑（Milestones），映射到统一的子目标轴上，消除步数粒度不一致带来的对比偏差。

4. **P2.4 工具输出真实利用率与 Context Bloat 量化**
   - **实现路径**：量化大型工具返回内容在后续对话中被模型消费的真实比例，评估 Prompt 与工具设计中的数据冗余。

---

### 4.3 Phase 3：远期储备（审慎观望与研究）

#### 目标与策略
对实现复杂度极高、依赖多次主动运行开销或 ROI 边际效应递减的模块保持技术跟踪，暂不投入主力开发：
1. **错误级联传播因果图反向回溯**：在无代码执行语义追踪前提下，纯靠离线反推因果链成本极高，容易产生连环推测。
2. **综合质量分（TQS）与帕累托前沿**：权重设定主观性较强，行业缺乏公认标准，过度追求单项数值容易掩盖具体维度的问题。
3. **决策方差与稳定性主动压测**：属于测试平台（Benchmark Harness）职责，非 VMR 核心只读审计网关的主航道。

---

## 5. Phase 1 核心 Prompt 规范与 Go 结构体契约

为保证 Phase 1 落地特性的工程严密性与确定性，本节给出核心 LLM 判别器的证据包结构体与标准化 Prompt 规范。

### 5.1 扩展后的证据包契约（`SingleJourneyEvidencePack`）

```go
// internal/story/llm_single.go

type SingleJourneyEvidencePack struct {
    Journey         JourneyRef           `json:"journey"`
    UserIntent      string               `json:"user_initial_prompt"`    // 用户首个任务的原始需求
    FinalOutcome    string               `json:"final_assistant_reply"`  // 最后一个 Step 的最终输出
    Metrics         Metrics              `json:"metrics"`
    Findings        []Finding            `json:"findings"`
    TaskTitles      []string             `json:"task_titles"`
    ToolIndex       []ToolIndexEntry     `json:"tool_index"`
    
    // 关键工具与响应对抽样 (针对 ToolResultMisinterpretation 判定)
    SuspiciousPairs []ToolResponsePair   `json:"suspicious_pairs,omitempty"`
}

type ToolResponsePair struct {
    StepSeq        int    `json:"step_seq"`
    ToolName       string `json:"tool_name"`
    ToolResultText string `json:"tool_result_preview"` // 截断至 500 字符
    NextReasoning  string `json:"next_reasoning"`      // 紧随其后的模型思考
}
```

### 5.2 任务完成度四阶逼近 Prompt 规范

```yaml
System Prompt: |
  你是一个严谨的 AI Agent 运行轨迹黑匣子审计专家。你的任务是根据用户初始需求、Agent 的最终答复以及运行过程中的关键指标，客观评估该任务的实际完成状态。

  评估分为四阶（严格判定，拒绝被 Agent 的客套话迷惑）：
  1. COMPLETED_VERIFIED: 初始需求中的所有核心目标与约束均已达成，且轨迹中有明确的验证动作（如测试通过、读取确认）。
  2. COMPLETED_UNVERIFIED: Agent 在最终答复中声称已完成，但轨迹中缺乏验证步骤，或者存在未被检验的假设。
  3. PARTIALLY_COMPLETED: 仅完成了部分子任务，或者违反了用户提出的部分约束条件。
  4. FAILED_ABORTED: 核心任务失败、中途崩溃、死循环耗尽预算或明确放弃。

  输出必须包含 JSON 格式的结构化判定及简要说明：
  ```json
  {
    "status": "COMPLETED_VERIFIED | COMPLETED_UNVERIFIED | PARTIALLY_COMPLETED | FAILED_ABORTED",
    "confidence": 0.95,
    "achieved_goals": ["目标1", "目标2"],
    "missed_goals_or_violations": ["未满足项或违背的约束"],
    "verdict_reason": "判定依据简述"
  }
  ```
```

### 5.3 工具结果曲解检测 Prompt 规范

```yaml
System Prompt: |
  请审计以下给出的 [工具返回结果] 与 [Agent 随后推理文本] 对。
  重点检查：Agent 是否产生了“颠倒黑白的幻觉解读”？
  例如：工具明确报错、返回 404 或空数据，Agent 的推理却声称执行成功或编造了不存在的数据。

  如果发现曲解，输出：
  ```json
  {
    "has_misinterpretation": true,
    "step_seq": 12,
    "severity": "HIGH | MEDIUM",
    "evidence_tool": "工具返回的关键否定证据",
    "evidence_reasoning": "模型产生幻觉的推理语句",
    "explanation": "为什么构成曲解"
  }
  ```
  若推理与工具结果逻辑自洽，输出 `{"has_misinterpretation": false}`。
```

---

## 6. 架构演进、实现哲学与关键考量

### 6.1 坚守 VMR 核心架构不变量
在推进上述 Agent 运行时分析能力落地的全过程中，必须无条件遵守已确立的架构纪律：
1. **两半区隔离与只读契约**：
   - 路由核心（`internal/router`/`internal/server`）与分析半区（`report`/`story`/`ctxgraph`）零双向依赖。
   - 所有分析仅离线读取只读的 `audit.Record` JSONL 日志，绝对不侵入在线请求转发路径。
2. **纯规则与 LLM 解读层的清晰解耦**：
   - **底层规则与结构事实是不可动摇的基石**。JSON 输出中必须保证规则事实的绝对确定性与幂等性。
   - **LLM 解读层永远是可选、可降级的（Fail-Open & Graceful Degradation）**。无 API Key 或网络失败时，纯规则层产出完整的 `.md`/`.json`，仅在 stderr 打印警告，绝不中断分析进程。
3. **identity 与展示文案严格分离**：
   - 所有的 Finding、Metric 无论是否由 LLM 参与判别，必须拥有全局稳定的英文字符串 Code（如 `semantic_oscillation`），多语言本地化只发生在 Markdown 渲染层。

### 6.2 评测闭环与 Prompt 迭代优化体系（Eval & Calibration）
引入 LLM 做模糊判断的关键在于**防止误报失控与幻觉**：
1. **黄金样本集（Golden Benchmark Set）**：
   - 在 `internal/story/testdata/` 下维护 30-50 个具备真实人工标注的代表性 Journey 轨迹（包含已知死循环、真实目标漂移、真实工具报错、成功完成等各类型样本）。
2. **双重置信度输出约束**：
   - 所有 LLM 判别器必须强制输出结构化 JSON，包含：`is_matched (bool)`、`confidence ("HIGH"|"MEDIUM"|"LOW")`、`evidence_anchor (string)` 以及 `rationale (string)`。
   - 只有置信度为 `HIGH` 且能明确指认上下文证据锚点的判别结果，才允许标注为 ⚠️ Finding，其余仅作为辅助解读参考。
3. **提示词版本化与磁盘缓存隔离**：
   - 每个 LLM 判别模块拥有独立的 `promptVersion`，更新 Prompt 自动使旧缓存失效，确保判定逻辑的可追溯性与可重复性。

### 6.3 隐私保护与本地优先（Local-First & Data Sovereignty）
- VMR 运行于用户本地或企业私有环境中，审计日志可能包含大量敏感代码、Token 和私有业务数据。
- 在使用 LLM 解读层时：
  - 严格遵守 `EvidencePack` **最小必要信息原则**，仅提取脱敏后的元数据、关键工具参数截断及必要上下文片段，严禁无脑塞入全量对话历史。
  - 支持配置私有本地 LLM 端点（如通过 Ollama 或本地 vLLM 实例），确保数据不出本地网络。

---

## 7. 总结与行动项（Action Items）

| 阶段 | 核心行动项 | 涉及模块 | 交付产物 |
|---|---|---|---|
| **Phase 1 (首周)** | 1. 实体提取正则重构与参数 JSON Canonicalization | `internal/chatmsg`, `internal/story` | 消除 5 大 Finding 的基础假阳性与漏网 |
| **Phase 1 (首周)** | 2. 跨协议错误嗅探与通用 Shell 测试命令识别 | `internal/chatmsg`, `story/findings_toolresult.go` | OpenAI 工具错误识别与测试命令免误报 |
| **Phase 1 (近期)** | 3. 任务完成度四阶逼近裁决器 | `internal/story/llm_single.go` | 四阶完成度状态与未达成目标清单 |
| **Phase 1 (近期)** | 4. 工具结果曲解与幻觉断言检测 | `internal/story/findings_toolresult.go` | `FindingToolResultMisinterpretation` |
| **Phase 1 (近期)** | 5. 语义死循环与微调振荡判别器 | `internal/story/findings.go`, `llm_single.go` | `FindingSemanticOscillation` |
| **Phase 1 (近期)** | 6. 语料级 Context Rot 拐点分析 (纯 Go) | `internal/story/corpus.go` | 上下文有效窗口拐点曲线与图表 |
| **Phase 1 (近期)** | 7. 目标漂移（Goal Drift）与计划合规升级 (PCPC) | `internal/story/findings.go`, `render_spine.go` | `FindingGoalDrift` + 动态规划追踪 |
| **Phase 2 (中期)** | 8. 单 Journey 局部上下文切片注入 (Focused Pack) | `internal/story/llm_single.go` | 微观代码级病因诊断报告 |
| **Phase 2 (中期)** | 9. MAST/RCA 故障模式自动归因与跨 Agent 对齐 | `internal/story/compare.go`, `corpus.go` | 标准化故障归因卡 + 跨框架公平成绝对比 |

通过上述务实、渐进、以第一性原理驱动的落地演化，VMR 将从一个“基础流量路由与简单指标统计工具”，全面跃升为**业界领先的、本地优先的 Agent 运行时深度可观测与智能诊断平台**。
