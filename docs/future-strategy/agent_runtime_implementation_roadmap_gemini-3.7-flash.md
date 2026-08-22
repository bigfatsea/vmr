<!-- Ver 2026-08-16 13:15, by gemini-3.7-flash -->

# Agent 运行时分析在 VMR 中的落地与演进路线图

> **版本**：v1.1（吸收历史方案深度技术规范与 Prompt 契约后的全面完善版）  
> **日期**：2026-08-16  
> **作者**：Gemini 3.7 Flash  
> **定位**：基于文献调研报告 `agent_runtime_analysis_v1.0_custom-2-agent.md`（已归档，不在版本控制范围内），结合 VMR（Virtual Model Router）现有架构与代码实现，全面剖析 Agent 运行时分析（Agent Runtime Analysis）的落地可行性，盘点现有功能缺陷与改进空间，评估未实现能力的多维代价与用户价值，并制定分阶段推进路线图。
> **审阅**：Sonnet 5（2026-08-16）— 逐条核实本文件第 2 章的代码引用与第 3-5 章的建议后，加了正文内 `[审阅批注]` 与新增第 8 章《审阅意见汇总》。结论先说：**方向基本正确、可落地性分层框架有价值，但第 2 章至少两处把"设计文档里已经论证并写明的决定不修项"错当成"未发现的缺陷"提出修复方案，第 3-5 章的 Phase 1 范围与既有工程纪律（archtest 行数预算、"分析半区先外部脚本验证"的治理原则、LLM 解读层"不得生成数字"的硬约束）有具体冲突，需要收窄或改写后才能直接执行**。详见第 8 章。
>
> **CLI 表述已过期（2026-08-21 标注）**：本文档写于 2026-08-16，早于 `vmr report`/`vmr story` →
> `vmr analyze` 单一入口收敛（P9/P10）。文中的 `vmr report`/`vmr story` 命令示例应读作 `vmr analyze`
> 的等价历史写法；两者今天仍可用，但已降级为过渡别名。本文档描述的 Agent Runtime Analysis 功能
> 路线图本身尚未排期（见 `docs/KNOWN_ISSUES_sonnet-5.md`），不受此次 CLI 收敛影响。

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
2. **端到端绝对成功率的自动二元真值判别**：在缺乏真实运行沙箱（如 SWE-bench Docker 执行环境）的离线场景下，仅靠 LLM 判断开放式任务”是否绝对完成”极不可信。VMR 应坚持”揭示事实与过程异常，而非给出虚假裁判”。
3. **逐 Token 级过程奖励模型（PRM）**：需要极高算力且模型泛化性差，严重违背轻量化、本地优先原则。
4. **[审阅批注·补齐] 通用推理质量评分（Reasoning Relevancy/Coherence/Faithfulness 逐步打分）**：原调研报告 §3.3 把这列为核心质量维度之一，本 Roadmap 全文未提及，既没有采纳也没有像第 2 项那样明确列入 Layer C 说明原因——这不是疏漏可以放过，而是应该显式补齐到这里：`docs/VirtualModelRouter_Design_v4_Analytics.md` 已经确立”VMR 没有任务是否真正达成目标的信号，这个判断超出证据能支持的范围”（见该文档 6c 分叉点 LLM 解读层一节）、”无成功/失败标签”（同文档 §7 实测结论）两条架构边界，逐步推理质量打分本质上就是这类开放式质量裁决，理由与第 2 项完全一致，应当合并列入本条，而不是留白。第 3 章 E2”任务完成度四阶逼近判定”与这条边界的张力，见第 8 章审阅意见。

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

> **[审阅批注]** 这张全貌图有一处实质性遗漏：`vmr story -compare` 早已落地的**跨 Journey/跨 Agent 对比基础设施**完全没有出现——`ComparisonExtras`（模型/端点核查、Prompt 缓存曲线、System Prompt 稳定性、最终交付物节选对比）、`computeDivergence`/`flattenWithTask` 的**分叉点结构化定位**（`DivergencePoint`，区分 `DivergenceHeavy`/`DivergenceLight`）、以及 6c 分叉点专属的 LLM 解读层（`llm_divergence.go`），均已在真实语料（`j-openclaw-...-8b175da9` vs `j-lobster-...-d6b04665`，同一任务两个不同 Agent 框架）上验证过，并且是"这份数据第一次被结构化定位到具体分叉步骤"（design doc 原话）。
> 这不是小事：调研报告第 7 章"跨 Agent 对比方法论"整章讨论的正是这个主题，而本 Roadmap 第 3 章把 E11"跨 Agent 子目标对齐"列为 Phase 2 新能力时，只字未提 VMR 已经有一半的地基（结构对齐、位置定位）——E11 真正要新增的只是"把结构分叉点升级为语义子目标对齐"这一层，工程难度评分（本文档给了"4/高"）应该基于"在已有 `computeDivergence` 之上做语义细化"重新估，而不是从零假设。第 2 章的"现有实现盘点"理应把这块补进去，作为 P2.1/E11 的地基说明，而不是让读者误以为跨 Agent 对比是一片空白。

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

> **[审阅批注·补充] 影响面比本条描述的更大**：`chatmsg.ExtractEntities` 不止被本条列出的 `internal/story` 5 个检测器依赖——`internal/report` 的 Compaction 吞噬检测（`SwallowedEntities` 相关聚合）同样复用这个函数。重构这个正则是**跨越两半区共享基础设施的改动**，需要同时用 `story` 和 `report` 两侧的真实语料回归验证，不能只在 story 侧测过就认为安全。CLAUDE.md 里"`ctxgraph`/`chatmsg` 是消息哈希与解析的唯一真相来源"这条不变量本身就是在提醒这类共享基础设施改动的辐射半径——P1.1 的验收标准应该明确加一条"用 report 侧既有测试数据跑一遍回归，确认 Compaction 吞噬检测的判定没有漂移"。

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
  - **LLM 最终核验**：由 LLM 综合判断任务终步声称的”成功”是否建立在已修复前序错误的基础之上。

> **[审阅批注·纠偏]** 这两条代码引用属实（`internal/story/metrics.go:260-262` 的 `isErrorMarker`、`internal/story/findings.go:229-234` 的 `verificationLikeToolRe`），但”OpenAI 协议错误漏报”**不是未被发现的缺陷，而是 `docs/VirtualModelRouter_Design_v4_Analytics.md` §7”已知限制、暂不处理的事项”表里明确记录的决定不修项**，原文理由是：*”强行在 OpenAI 响应体里猜'这是不是一次错误结果'就是§3反复强调的'宁可粗糙也不猜语义'的反例；等 OpenAI 一侧出现可靠的结构信号再补，不用启发式凑数”*。本条建议的”跨协议错误归一化嗅探”（对 ToolResult 文本做 `fatal`/`exit status 1`/`error:` 关键字匹配）正是设计文档点名拒绝的那种”启发式凑数”——`error:` 这类关键字在正常输出里出现的概率不低（日志文本、代码本身包含这个词、被引用的报错样例文本等），规则层一旦上线就会把”文本提到 error”和”这次调用真的失败了”混为一谈，而 VMR 的”揭示而非裁决”原则恰恰是靠拒绝这类模糊猜测换来可信度的。
> 同理，`verificationLikeToolRe` 在 design doc（`error_then_unverified_success` 一行）里明确标注为”一个局部启发式，不是正式的读写分类器”——即已经有意识地把它限定在小范围、宁可漏检（安全的失败方向）。
> **建议改法**：不要用文本关键字嗅探，而是遵循同一份 design doc 里”等 OpenAI 一侧出现可靠的结构信号再补”的既定判据——具体说：(a) 若 OpenAI 工具调用协议本身有可复用的结构化错误字段（部分 Function Calling 实现允许工具端返回 `{“error”: ...}` 的结构化 JSON，需要先查证 VMR 实际语料里这个字段是否普遍存在），只对**结构化字段**做识别，不对自由文本做关键字匹配；(b) `bash`/`exec` 的验证意图识别，若要做，应作为**规则候选 + LLM 判定**的 Layer B 方案（本文第 1.3 节自己提出的范式），而不是继续叠加纯规则关键字表——纯规则关键字表只会重复”regex 打地鼠”的历史（`internal/chatmsg/entities.go` 的正则本身就是前车之鉴）。落地前应先在 `docs/KNOWN_ISSUES_sonnet-5.md` 更新或撤销该条”决定不修”记录，说明为什么现在值得重新评估——而不是在 Roadmap 里悄悄绕开它。

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
  - 将扫描窗口从”最后一句”扩大为”末尾 2~3 句的动作意图窗口（Action-Intent Window）”。
  - 将高嫌疑的脱节候选提交给第二层 LLM 进行语义一致性确认。

> **[审阅批注·纠偏]** `internal/story/findings.go:280-330` 的注释原文写得非常清楚：**”只看最后一句”是校准后的修复结果，不是有待改进的缺陷**。第一版实现（扫描整段 Reasoning、精确字符串匹配）在真实语料上几乎对每一段多步推理都产生假阳性，原因有二：(a) Reasoning 常见先叙述一个编号计划（”1. 查 X，2. 查 Y，3. 查 Z”），当前 Step 只动了其中一项，其余两项不是”脱节”而是”后续步骤”；(b) 同一实体因正则词边界起点不同，会被抽成两个不同的字符串（`~/.hermes/SOUL.md` 扫成 `hermes/SOUL.md`，`/Users/x/.hermes/SOUL.md` 扫成 `Users/x/.hermes/SOUL.md`，同一文件却没有共同子串）。”只看最后一句”+”实体匹配改为双向子串容忍”是**同一次校准**里一起做的两个修复，且校准记录在 `docs/VirtualModelRouter_Design_v4_Analytics.md`（引用 `logs/vmr-audit-2026-07-25/26/27` 真实语料）里留了痕。
> 本条”改进空间”提议的”扩大到末尾 2~3 句”，如果不重新过一遍同样的真实语料校准，很可能是在**复现第一版已经踩过的坑**（多步计划陈述里，倒数第 2、3 句提前”剧透”了后续步骤的规划部分，与当前 Step 的关联同样弱）。不是说这个方向一定不能做，而是必须先在 `internal/story/testdata/`（或按 §6.2 建议新建的黄金样本集）里补真实反例样本、验证扩大窗口后误报率有没有回升，再决定要不要改——这正是本 Roadmap 第 6.2 节自己主张的评测闭环，这里却没有先适用到自己身上。

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

> **[审阅批注] E2 与既有架构边界的张力**：`docs/VirtualModelRouter_Design_v4_Analytics.md` §7 实测结论明确写着"**无成功/失败标签**：VMR 零埋点前提意味着结构性拿不到任务是否真正达成目标的信号"；同文档 6c 分叉点 LLM 解读层的硬约束是"绝不判断哪一方更好——这个判断超出证据能支持的范围"。E2"任务完成度四阶逼近判定"直接让 LLM 输出 `COMPLETED_VERIFIED/UNVERIFIED/PARTIALLY_COMPLETED/FAILED_ABORTED` 四态判决，这在方向上正是这条边界想避免的东西——不是说不能做，而是**这条边界的存在本身就是本 Roadmap 需要正面回应、而不是绕过的架构决策**。E2 若要落地，必须让输出严格服从"推测必须标注推测、必须声明证据边界"这条现有硬约束（同 6c），而不是给出一个看起来像裁决结论的四态枚举——四态本身的措辞（"COMPLETED_VERIFIED"）已经在暗示"这是可信结论"而非"这是一次基于文本的合理推测"，容易被下游用户当真值使用。第 8 章给出具体改法建议。综合 ROI 打分"极高"因此偏乐观，应扣减到"高"并注明这条依赖前提。
>
> **[审阅批注] 打分维度本身的局限**：3.1 节给出的四维打分（用户价值/工程难度/LLM 调优难度/成本开销）里没有一维覆盖"是否与已确立的架构边界/治理原则冲突"——这恰恰是本文档 §2.2 已经两次踩中的问题（E1 对应的 P1.2、E4 附近的语义死循环判定）。建议在打分矩阵里补一列"架构合规性风险"，E2/E1 都应该标为需要额外论证的项，而不是和 E6/E7 这类无冲突项同等对待。
>
> **[审阅批注·核心张力，分项论证——不止 E2 一项]** 上面这条批注只单独点了 E2，但核实 `internal/story/llm.go` 后发现边界比"E2 一项冲突"更宽：现有 LLM 解读层有一条比"数字必须离散化"更根本的注释原文约束——"the LLM only ever receives numbers this package already computed for interpretation ... never generates a new number [or finding] itself"。也就是说 LLM 目前被严格限定为对规则层**已经算出的事实做解读**，不产生**新的裁决/新的 Finding**。E2、E3（工具结果曲解）、E4（语义死循环）、E5（目标漂移，注：本文档正文里 E5 与后文 P1.7"目标漂移"同指一物）、E7（Compaction 否定式约束丢失）全部要求 LLM 产出此前不存在的新判定，全部踩中这条边界，不是只有 E2。
>
> 这条边界不是不能挑战的教条——它是团队在特定假设下做出的一次设计选择，值得用第一性原理重新审视，而不是"文档这么写就照单全收"或者"文档这么写就整体推翻 Phase 1"。分项论证如下：
> - **E3、E4、E5（目标漂移）三项，越界的正当性较强**：它们判断的是"VMR 已经拿到手的两段证据（工具返回 vs 后续推理、连续几步工具调用、当前步骤 vs 用户原始诉求）内部是否自洽"，不需要 VMR 对它看不见的外部现实（沙箱执行结果、用户真实验收标准）下裁决——本质仍是"证据范围内的推测"，只是推测对象从"一个数字"扩展成了"一个语义判断"。只要严格遵守本文档 §6.2 已有的置信度分级与证据锚点纪律，这类新增 Finding 类型（`FindingToolResultMisinterpretation`、`FindingSemanticOscillation`、`FindingGoalDrift`）没有突破"只读证据、不裁决现实"的精神，只是突破了"不产生新 Finding"这条更窄的既有实现约束——这条更窄的约束可以改，但改的时候应该给 LLM 生成的 Finding 加一个统一的来源标记（例如 `Source: "llm_inferred"`），在渲染层与规则产生的 Finding 视觉/结构上区分开，保留"规则事实"与"LLM 推测"不能混为一谈的既有精神。
> - **E2（任务完成度四阶判定）风险明显更高，需要改造而非照单实现**：`COMPLETED_VERIFIED`/`FAILED_ABORTED` 判断的是"任务在现实世界里是否真的达成"，这恰好是 VMR 结构性看不到的东西（无沙箱执行反馈、无用户验收信号），也正是 design doc 论证"超出证据能支持的范围"的那类判断。建议不是"不能做"，而是把问法从"任务是否完成"改写成 VMR 证据范围内能立住的事实性问题，例如"轨迹中的最终声明是否有对应的验证动作支撑"（`CLAIM_WITH_VERIFICATION` / `CLAIM_WITHOUT_VERIFICATION` / `NO_COMPLETION_CLAIM`）——既保留了 E2 的核心用户价值（拆穿"说完成了但没验证"这种最常见的隐性失败），又不让 LLM 冒充一个它给不出的权威结论。
> - **E7（Compaction 否定式约束丢失）介于两者之间**：判断"被裁掉的文本是否包含关键约束"仍是对已捕获文本的解读，风险接近 E3/E4，可以归入"正当性较强"一类，但落地时同样应打上 `Source: "llm_inferred"` 标记。
>
> 落地前建议在 `docs/KNOWN_ISSUES_sonnet-5.md` 新增一条，把"LLM 解读层是否可以在明确置信度分级+来源标记下产出新 Finding（而不止是解读已有数字）"作为一次显式的架构决策记录下来，附上这里的分项论证，而不是让这个决定隐性地发生在某次 PR 评审里。

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

> **[审阅批注] Phase 1 范围与两条既有工程纪律冲突，需要收窄**：
>
> 1. **archtest 行数预算已经逼近上限，Phase 1 的落点文件全部被点名**。核实当前代码：`internal/story/findings.go` 547/580 行（预算仅剩 33 行）、`internal/story/findings_toolresult.go` 288/320 行（剩 32 行）、`internal/story/metrics.go` 414/470 行（剩 56 行）——见 `internal/archtest/file_sizes_test.go`。Phase 1 的 9 个模块（P1.3-P1.9）里至少 6 个（任务完成度、工具结果曲解、语义死循环、目标漂移、计划合规、Compaction 约束丢失）计划往 `findings.go`/`findings_toolresult.go`/`llm_single.go` 里加新的 Finding 检测器和字段，几乎必定当场触发 `go test ./internal/archtest/...`。CLAUDE.md 对此有明确指导："raising the number in the table is what the failure message tells you not to do"——即正确做法是**新增文件**（`findings_toolresult.go` 本身就是从 `findings.go` 拆出来的先例），而不是继续往老文件里堆。这不是本 Roadmap 现在能忽略的实现细节，而应该在第 4/7 章的行动项里明确写出"每个新检测器落在哪个新文件"，否则第一次落地就会被自己的架构纪律挡下来。
> 2. **"分析半区先外部脚本验证，证明价值后再合仓库"的治理原则没有被引用**。`docs/KNOWN_ISSUES_sonnet-5.md` §1.15"分析半区体量增长与单人可维护性"明确写着：分析半区代码量已超过路由核心，*"新的探索性 Agent 分析指标，优先用外部脚本消费稳定的 `vmr-report.json`/`journey-*.json` 数据契约做验证，证明价值后再评估是否合入主仓库"*。这条对应本文档第 1.3 节"Layer B"里几乎所有新引入 LLM 判定的能力（目标漂移、语义死循环、任务完成度、工具结果曲解）——这些恰恰是"探索性"程度最高、最需要先在生产语料上验证误报率的一批，却被 Phase 1 直接安排成"落地进 `internal/story` 的 Go 结构体 + Finding Code"。考虑到这是一个 ≤3 人的 AI-native 团队（见 CLAUDE.md 团队定位），Phase 1 一次性铺开 9 个模块（其中 6 个引入新的 LLM 判定逻辑）在人力上也不现实。
>
> **建议**：把 Phase 1 拆成两轮——「Phase 1a：纯规则修复」（P1.1 实体提取重构、P1.2 里被本文档 §2.2 批注收窄后的部分、P1.6 语料级 Context Rot 统计，均零 LLM 成本、不新增 Finding 判定逻辑，可以直接落 `internal/story`）与「Phase 1b：LLM 判定原型」（P1.3/P1.4/P1.5/P1.7/P1.8/P1.9 全部先以外部脚本 + 黄金样本集验证误报率，产出校准报告后再决定合并），第 7 章的行动项表格应按这个两轮拆分重排，而不是把全部 9 项都标"首周/近期"。

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
   - **[审阅批注·补充]** `-corpus`（`cmd/vmr/cmd_story.go`、`internal/story/corpus.go`）已经实现语料级统计（含 metric 分布与 Spearman 秩相关），这里是在既有分桶/统计管线上加一个维度，不是从零建模块——3.1 节给 E8 打"工程难度 2（低）"是对的，但应该注明"低是因为复用现有基建"，避免读者误以为要新写一套语料聚合代码。

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
   - **[审阅批注·补充]** MAST 分类里有一类在 VMR 场景下不适用，落地时应显式裁剪：MAST"多 Agent 通信失败"大类（交接信息丢失、角色边界模糊、协调死锁）建立在"能观测到多个 Agent 之间的通信"这个前提上。VMR 是单客户端代理，一个 Journey 里的所有 LLM 调用都来自同一个 Agent 进程发起的请求流，天然没有"另一个 Agent"的数据源可比对。归因分类枚举应该只取 L1/L2/L3/L4（认知编排/执行/记忆感知/环境）与 RCA 分类的"内部推理失败"子类，明确排除 MAST 的"多 Agent 通信失败"，而不是照搬三大类后发现有一类永远空转。

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

> **[审阅批注] 5.2/5.3 的 `confidence` 字段违反了现有硬约束，需要改**：`docs/VirtualModelRouter_Design_v4_Analytics.md` 对已实现的三条 LLM 解读路径（单 Journey、双 Journey 对比、分叉点）有一条写死的硬约束——"**LLM 不得生成数字**"，原文理由是"数字必须只由规则产生，混入 LLM 生成的数字会破坏'报告里的每个数字都可复现'这个约束"（同一次 LLM 调用，同一份输入，置信度浮点数在不同调用间不保证一致，破坏可复现性）。现有实现（`internal/i18n/story_llm.go`）全部用"高/中/低"三档离散置信度，且明文规定判据："能指认具体证据锚点才能标'高'……不能为了显得更确定而拔高置信度"。
> 本节 5.2 的 `"confidence": 0.95` 与 5.3 隐含的数值化设计，都是这条约束明确禁止的形态。**这不是风格问题，是会被现有 LLM 调用链路（`Interpret`/`cacheKey`/`buildUserPrompt`）的既定契约直接拒绝的设计**。落地时应统一改为 `"confidence": "HIGH" | "MEDIUM" | "LOW"`，并复用已有的判据措辞（"能指认具体证据锚点才标高，间接证据/需要推断标中，纯排除法/直觉标低"），而不是另起一套数值化标准。

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

> **[审阅批注] 核实无误**：核对 `internal/story/llm_single.go`，当前 `SingleJourneyEvidencePack` 确实只有 `Journey`/`Metrics`/`Findings`/`TaskTitles`/`ToolIndex` 五个字段，本节新增的 `UserIntent`/`FinalOutcome`/`SuspiciousPairs`/`ToolResponsePair` 均不存在，属于新提议，不冲突。补一条既有约束需要遵守：证据包的组装函数 `BuildSingleJourneyEvidencePack` 目前接收调用方已经算好的 `m Metrics, findings []Finding`（不在内部重算），新增字段的组装逻辑也应遵循这个模式——`UserIntent`/`FinalOutcome`/`SuspiciousPairs` 应该在 `cmd_story.go` 已经拥有 `Journey`/`Metrics`/`Findings` 之后一次性派生，不要在 `BuildSingleJourneyEvidencePack` 内部重新扫描 `Journey.Tasks`，否则会重复 `internal/story` 里已经明确记录过的"避免二次计算"惯例（design doc 原话："m/findings are passed in rather than recomputed so a caller that already has them ... doesn't pay for ComputeMetrics/ComputeFindings a second time"）。

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
  <json>
  {
    "status": "COMPLETED_VERIFIED | COMPLETED_UNVERIFIED | PARTIALLY_COMPLETED | FAILED_ABORTED",
    "confidence": 0.95,
    "achieved_goals": ["目标1", "目标2"],
    "missed_goals_or_violations": ["未满足项或违背的约束"],
    "verdict_reason": "判定依据简述"
  }
  </json>
```

### 5.3 工具结果曲解检测 Prompt 规范

```yaml
System Prompt: |
  请审计以下给出的 [工具返回结果] 与 [Agent 随后推理文本] 对。
  重点检查：Agent 是否产生了“颠倒黑白的幻觉解读”？
  例如：工具明确报错、返回 404 或空数据，Agent 的推理却声称执行成功或编造了不存在的数据。

  如果发现曲解，输出：
  <json>
  {
    "has_misinterpretation": true,
    "step_seq": 12,
    "severity": "HIGH | MEDIUM",
    "evidence_tool": "工具返回的关键否定证据",
    "evidence_reasoning": "模型产生幻觉的推理语句",
    "explanation": "为什么构成曲解"
  }
  </json>
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

> **[审阅批注·纠偏] 6.1 第 2 点描述的机制已经落地，不是待建原则**：核实 `internal/story/llm.go`——`Interpret` 任何失败（无 API key、网络失败、解析失败）都返回 error，由调用方降级为"本次无 LLM 小节"，不中断规则层输出。Fail-open 已经是现有代码的实现细节，不需要作为"必须遵守"的新原则再提一遍；Phase 1 新增的 LLM 判别器只需要复用同一条错误处理路径，不需要另起一套降级方案。

### 6.2 评测闭环与 Prompt 迭代优化体系（Eval & Calibration）
引入 LLM 做模糊判断的关键在于**防止误报失控与幻觉**：
1. **黄金样本集（Golden Benchmark Set）**：
   - 在 `internal/story/testdata/` 下维护 30-50 个具备真实人工标注的代表性 Journey 轨迹（包含已知死循环、真实目标漂移、真实工具报错、成功完成等各类型样本）。
2. **双重置信度输出约束**：
   - 所有 LLM 判别器必须强制输出结构化 JSON，包含：`is_matched (bool)`、`confidence ("HIGH"|"MEDIUM"|"LOW")`、`evidence_anchor (string)` 以及 `rationale (string)`。
   - 只有置信度为 `HIGH` 且能明确指认上下文证据锚点的判别结果，才允许标注为 ⚠️ Finding，其余仅作为辅助解读参考。
3. **提示词版本化与磁盘缓存隔离**：
   - 每个 LLM 判别模块拥有独立的 `promptVersion`，更新 Prompt 自动使旧缓存失效，确保判定逻辑的可追溯性与可重复性。

> **[审阅批注·纠偏] 6.2 第 3 点描述的机制也已经落地**：`evidencePackKind.promptSpec` 已经返回一个 `Version` 字符串，并作为磁盘缓存 key 的一部分——Prompt 改了版本号，旧缓存自然失效。Phase 1 新增的 LLM 判别器只需要为自己的 `evidencePackKind` 定义新的 `Version`，复用既有缓存机制即可，不需要另起一套版本化方案。

### 6.3 隐私保护与本地优先（Local-First & Data Sovereignty）
- VMR 运行于用户本地或企业私有环境中，审计日志可能包含大量敏感代码、Token 和私有业务数据。
- 在使用 LLM 解读层时：
  - 严格遵守 `EvidencePack` **最小必要信息原则**，仅提取脱敏后的元数据、关键工具参数截断及必要上下文片段，严禁无脑塞入全量对话历史。
  - 支持配置私有本地 LLM 端点（如通过 Ollama 或本地 vLLM 实例），确保数据不出本地网络。

> **[审阅批注·纠偏] 本地 LLM 端点支持已经实现，不是待建能力**：`internal/story` 的 LLM 解读层不直接对接 OpenRouter 或云端 API，而是通过 `-llm-addr host:port` 指向"一个正在运行的 VMR 实例"，走标准 `POST /v1/chat/completions`。只要该 VMR 实例的 `config.yaml` 把某个虚拟模型指向本地 Ollama/vLLM 端点，"数据不出本地网络"这个诉求已经天然满足，不需要另建一套本地端点配置逻辑。这条顺带回答了 Phase 1 新增 LLM 判定调用的成本失控担忧——只要延续 `-llm-addr` 机制，这些调用会自然被目标 VMR 实例自己的 quota/pricing 记账覆盖，不存在脱离配额追踪、用户没预料到的额外调用成本的风险。本节应该把这条改写成"现状说明"，而不是留在"支持配置"这种建议口吻里。

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

通过上述务实、渐进、以第一性原理驱动的落地演化，VMR 将从一个”基础流量路由与简单指标统计工具”，全面跃升为**业界领先的、本地优先的 Agent 运行时深度可观测与智能诊断平台**。

---

## 8. 审阅意见汇总（Sonnet 5，2026-08-16）

本章回应用户提出的三个问题：能力可落地性判断是否合理、分期逻辑是否合理、是否有疏漏。结论均基于对 `internal/story`/`internal/report`/`internal/chatmsg` 真实源码与两份设计文档（`docs/VirtualModelRouter_Design_v4_Analytics.md`、`docs/KNOWN_ISSUES_sonnet-5.md`）的逐条核实，不是对本文档 claim 的转述。

### 8.1 第一问：能力判断与建议是否合理？

**三层可行性框架（Layer A/B/C）本身是合理且有价值的**——把”规则初筛 + LLM 模糊判定”作为把过去认为”学术化、不可落地”的过程级指标转化为可行能力的路径，方向正确，也符合 VMR 现有 LLM 解读层（`llm.go`/`llm_divergence.go`）已经验证过的”规则出事实、LLM 做推测、推测必须标注置信度”范式。

但**第 2 章至少两处把设计文档里已经论证、记录在案的”决定不修”项，误判为”未被发现的实现缺陷”**（详见正文 §2.2 批注）：
- OpenAI 协议下工具错误漏报——`docs/VirtualModelRouter_Design_v4_Analytics.md` §7 明确记录为决定不修（”宁可粗糙也不猜语义”），本 Roadmap 的 P1.2 建议的关键字嗅探正是该文档点名拒绝的做法。
- `verificationLikeToolRe` 的窄范围——design doc 原话是”一个局部启发式，不是正式的读写分类器”，即刻意为之，不是遗留缺陷。
- `reasoning_action_mismatch` 只看”最后一句”——这是两轮真实语料校准后的**修复结果**（第一版扫描全文导致假阳性），本 Roadmap 建议”扩大到 2~3 句”有重新踩坑的风险，必须先过校准集才能改。

这类误判的根源是**只读了代码、没读设计文档里的”决定不修”表**——而 CLAUDE.md 与 `docs/KNOWN_ISSUES_sonnet-5.md` 都明确要求”check it before 'fixing' something that looks odd”。这是本次审阅里最需要修正的一类问题，因为如果直接按 Phase 1 执行，会在评审或实现阶段与既有架构决策正面冲突，返工成本比现在改正 Roadmap 本身更高。

同一根源还导致了**反向的误判**——把已经实现的机制当成待建议提出：LLM 解读层的本地端点支持（`-llm-addr` 可指向本地 Ollama/vLLM 后端的 VMR 实例，§6.3）、Fail-open 降级（§6.1）、Prompt 版本化缓存失效（§6.2）三条现有代码里都已经落地，本 Roadmap 把它们重新表述成"必须遵守的原则"或"建议支持"，虽然不影响方向对错，但会让读者误以为这是待办工作量，冲淡了 Phase 1 真正的新增工作量估算。

更关键的一处是**架构张力被低估了范围**：§3 批注指出，"LLM 只解读已算出的数字、不产生新裁决"这条现有约束不只卡住 E2，E3/E4/E5（目标漂移）/E7 全部要求 LLM 产出此前不存在的新判定，全部踩线。这不是要否决 Phase 1 的 LLM-as-judge 方向——从第一性原理看，这条约束本身是可以挑战的设计选择，不是不可动摇的教条——但挑战需要分项论证，不能整体绕过也不能整体照单实现：E3/E4/E5/E7 判断的是"VMR 已捕获证据内部是否自洽"，越界正当性较强；E2 判断的是"任务在现实世界是否真的达成"，这恰好是 VMR 结构性看不到的东西，正当性弱，需要把输出问法改造为证据范围内能立住的事实性问题，而不是照单实现四阶裁决枚举。详见正文 §3 批注的完整分项论证。

其余大部分 Layer B 建议（目标漂移、语义死循环、计划合规升级、Compaction 否定式约束丢失）判断合理，是真实存在的能力缺口，方案设计也基本合理，只是需要按 §8.3 的落地纪律执行。

### 8.2 第二问：分期逻辑是否合理？

四维打分矩阵（用户价值/工程难度/LLM 调优难度/成本开销）的方法论合理，但**遗漏了一维：”是否与既有架构边界/治理原则冲突”**——这一维会直接改变 E1、E2 的排期结论（见正文 §3 批注）：
- E2”任务完成度四阶逼近判定”与 `docs/VirtualModelRouter_Design_v4_Analytics.md` 确立的”无成功/失败标签”、”绝不判断哪一方更好”两条架构边界存在直接张力，不能像 E6/E7 一样直接进 Phase 1，必须先明确输出形态如何服从”推测需标注、需声明证据边界”的既有约束。
- Phase 1 一次排 9 个模块、其中 6 个引入新 LLM 判定逻辑，既会立刻撞上 `internal/archtest` 的文件行数预算（`findings.go`/`findings_toolresult.go` 都只剩 30 出头行的余量），也违反 `docs/KNOWN_ISSUES_sonnet-5.md` §1.15”新探索性指标先外部脚本验证、证明价值后再合仓库”的既定治理原则。对一个 ≤3 人团队而言，这个范围本身就不现实。

**建议的分期调整**：把 Phase 1 拆成”1a 纯规则修复”（零 LLM 成本，可直接落库）与”1b LLM 判定原型”（先外部脚本 + 黄金样本集验证，产出校准报告后再决定是否合入 `internal/story`），Phase 2/Phase 3 的相对顺序基本合理，不需要大改。

### 8.3 第三问：有没有疏漏？

对照调研报告逐项核对后，以下几点在调研报告里出现、但本 Roadmap 完全没有覆盖，也没有像 Layer C 那样明确说明”为什么不做”：

1. **工具调用序列模式挖掘**（调研报告 §4.4”工具使用模式挖掘”）：用 n-gram/序列模式挖掘识别”高频高成功率路径”（可固化进 prompt 的最佳实践）与”高频低成功率路径”（重点改进对象）。这是**纯 Go 统计、零 LLM 成本**的 Layer A 能力，和已经采纳的 E8（Context Rot 拐点分析）同属一类语料级统计，实现成本相近，却完全没有出现在 14 项评估矩阵里——属于真实疏漏，建议补一条 E15 归入 Phase 1a 或 Phase 2。
2. **上下文污染指数**（调研报告 §5.5：冗余率/无关率/冲突检测）：本 Roadmap 的 E10”工具输出真实利用率”只覆盖了”用了多少”，没覆盖”塞进去的内容里有多少是重复/无关/自相矛盾的”。这个和 E10 关系密切，可以合并成 E10 的一个子指标，而不是单独新增模块，但目前完全未提及。
3. **既有基础设施在盘点图里的缺失**：见正文 §2.1 批注——`vmr story -compare` 的分叉点检测/对比基础设施是本 Roadmap 第 2 章”现有实现盘点”里的实质性遗漏，直接影响 E11 的工程难度评分。
4. **调研报告 §3.3”推理质量”维度未被显式归类**：见正文 §1.4 批注，应作为 Layer C 第 4 项明确列出并说明理由，而不是留白。
5. **Token 效率比（"废话率"）**（调研报告 §2.1）：定义为"有效产出 token / 总 token"，衡量输出里有多少是无意义/重复内容。VMR 现有 `DuplicateActionRate`（`internal/story` 九项行为剖面之一）只是重复动作的代理指标，覆盖"重复调用"而非"输出内容里有多少废话"，两者不是一回事，这个维度目前是空白。可以作为 E10（工具输出利用率）的姊妹指标补充，同样走"规则先统计候选、LLM 抽样核实"的 Layer B 路径。
6. **MAST"多 Agent 通信失败"类别不适用于 VMR**：见正文 P2.2 批注——VMR 是单客户端代理，没有"另一个 Agent"的通信数据源，MAST 三大类里这一类应在 P2.2 落地时显式排除，否则归因分类枚举会有一类永远空转。
7. **决策方差/多次运行对比（E14）缺失的具体技术原因未点明**：Roadmap 已经把 E14 放进 Phase 3"审慎观望"，结论合理，但没有说明为什么——核实后发现 VMR 目前**没有跨 session 关联机制**，每个 Journey 是孤立的，无法识别"这是同一个任务被重复运行了 N 次"。这个技术缺口本身值得记录（哪怕不打算做 E14），因为它同样会挡住 E14 之外的任何"多次运行对比"类分析，建议在 Phase 3 该条下补一句技术原因，而不是只给结论。

以上七点中，第 1、2、5 点是可以直接补进 Phase 1a/2 的低成本高价值项；第 3、4、6、7 点是文档完整性/文档准确性问题，已在正文对应位置补齐批注。

### 8.4 总体结论

Roadmap 的整体判断力和工程直觉是合格的——三层可行性框架、七个模块的深度代码盘点（除上述两处误判外，其余五处：实体提取、计划解析格式、重复调用参数规范化、证据包深度、Compaction 约束丢失，逐条核实均**准确**）、Prompt 契约意识都体现了对 VMR 现有代码的认真阅读。但它是**在没有完整读过两份设计文档”决定不修”表和 `KNOWN_ISSUES` 治理原则的前提下**做的规划，导致部分建议与已有架构决策正面冲突而不自知（把决定不修的项当缺陷），也有反向的情况（把已实现的机制当未来建议：本地 LLM 端点、fail-open、prompt 版本化）。按本章的收窄与修正执行，这份 Roadmap 可以作为 Phase 1a 的直接施工依据；Phase 1b 及以后的模块，建议先补一次”该建议是否与 `docs/VirtualModelRouter_Design_v4_Analytics.md` §7 已有决定冲突”的检查，同时按 §3 批注的分项论证决定"LLM 产出新 Finding"这条边界具体在哪些模块上可以突破、在哪个模块（E2）上需要改造问法，再进入实现。

---

## 9. 最终路线图：融合审阅与代码核实后的完整执行方案

### 9.0 本章定位

本章是这份路线图的最终落地版本，独立于第 1-8 章存在——阅读本章不需要跳回前面任何一节。第 1-7 章是 Gemini 3.7 Flash 给出的原始建议，第 8 章是 Sonnet 5 基于代码与设计文档做的第一轮审阅批注；本章在此基础上，对第 8 章批注本身又做了一轮独立的代码/文档核实（重新读取了 `internal/archtest/file_sizes_test.go`、`internal/story` 全部相关文件的当前行号、`docs/VirtualModelRouter_Design_v4_Analytics.md` 与 `docs/KNOWN_ISSUES_sonnet-5.md` 的原文），确认第 8 章 19 处具体引用中 16 处逐字或语义精确，2 处存在实质出入，1 处证据不足以支撑原有措辞——这些出入会在下文对应位置直接体现，不再单独列纠错表。

本章给出的是**决策结论**，不是又一轮建议汇总：每一条任务都明确了要不要做、为什么这么做、做在哪个模块/文件、验收标准是什么。凡是与第 1-8 章的结论冲突之处，以本章为准。

---

### 9.1 分期框架总览

#### 9.1.1 三层可行性模型（沿用，重新表述）

VMR 是一个透明网络代理：只能看到网络上传输的 HTTP 请求/响应字节，看不到 Agent 进程内部的私有内存变量、未拼进 prompt 的本地判断分支、以及不经过网络的本地 I/O。基于这个物理边界，所有候选能力分三层：

- **Layer A（纯规则/工程级确定性）**：直接基于 `audit.Record` 的请求/响应元数据、Token 计量与消息结构，100% 确定性、零 LLM 调用成本。例如 Token 成本核算、时间三段式分解、上下文大小曲线、精确工具调用去重、协议原生错误标记。
- **Layer B（规则初筛 + LLM 模糊判定）**：文献中大量高价值的过程级评估（目标漂移、推理自洽性、工具使用合理性）过去被认为”学术化、不可落地”，原因是硬规则误报率极高、人工标注成本过高。用”规则提取候选集 + 针对性 Prompt 判定 + 置信度约束”的混合范式，可以把这类能力转化为可行、可控的能力。
- **Layer C（当前不落地/应舍弃）**：需要对 Agent 框架做代码插桩的白盒追踪（Memory State/Hidden State）、缺乏沙箱执行环境的绝对真值判定、逐 Token 级 PRM 打分，以及调研报告里”推理质量逐步打分”（Reasoning Relevancy/Coherence/Faithfulness）——最后一项与”绝对真值判定”同属一类问题：`docs/VirtualModelRouter_Design_v4_Analytics.md` 已经确立”VMR 没有任务是否真正达成目标的信号，这个判断超出证据能支持的范围”、”无成功/失败标签”两条架构边界，逐步推理质量打分本质上是同一类开放式质量裁决，因此归入 Layer C，不再单独讨论。

#### 9.1.2 为什么把 Phase 1 拆成 1a 和 1b

原路线图的 Phase 1 一次性排了 9 个模块，其中 6 个引入全新的 LLM 判定逻辑。本章第一版曾经把”archtest 文件行数预算逼近上限”和”团队规模只有 ≤3 人、做不完这么多模块”列为拆分的理由——这两条经不起第一性原理的推敲，在这里明确撤回，只保留真正站得住脚的那一条。

**"archtest 文件行数预算逼近上限"不是限制范围的理由，是一个早有标准解法、不影响范围的实现细节。** 实测 `internal/story/findings.go`（547/580 行）、`findings_toolresult.go`（288/320 行）、`metrics.go`（414/470 行）三个文件确实只剩 30-60 行余量，`llm.go`（415 行，走默认 700 上限）和 `llm_single.go`（52 行，同样走默认上限）余量都还很大。但文件余量紧张不构成”少做几个模块”的理由——`internal/archtest/file_sizes_test.go` 触发预算上限时给出的报错原文就是”split it into another file in the same package, don't just raise this number”，CLAUDE.md 也把这条写成明确纪律。这条预算从设计意图上就不是拿来卡住功能范围的，而是拿来强制”新能力应该落新文件”这个模块化纪律的——`internal/story/` 目录下已经有 19 个按能力拆分的独立文件（`compare.go`、`llm_divergence.go`、`llm_single.go`、`findings_toolresult.go` 都是各自独立成文件），这本来就是这个代码库一贯的组织方式，不是为了绕开预算才临时采用的变通做法。9.2 节末尾的”新文件规划表”把 Phase 1a 每个任务都分配到了新文件或有余量的既有文件——这解决的是”新代码放哪个文件”的问题，跟”要不要做这么多模块”无关。如果确实出现某个改动逻辑上必须长在一个已经顶格的老文件里、拆分反而会破坏内聚性，正确的下一步是重新评估这个文件的预算数字本身是否定得过紧（`file_sizes_test.go` 里的预算表是人为设定、可以随工程判断调整的配置，不是物理定律），而不是因此少做这个改动。**Phase 1 的范围不应该因为这条预算而收窄一分。**

**"团队规模只有 ≤3 人"推不出"6 个模块做不完"，这是套用了 AI 辅助编程时代之前的产能假设。** VMR 这整个项目——路由核心加上体量已经超过路由核心的分析半区——本身就是一个人在大约一个月内、依靠大量 AI 辅助编程完成的。用”团队人数”去估算产能，是在用前 AI 时代”一个人一天能写多少行代码”的心智模型，套在一个实际产能已经被 AI 辅助显著放大的团队身上，两者不是一回事。真正决定 Phase 1b 六个模块能不能同时推进的，不是”有没有足够多的人手”，而是这六个模块的校准工作彼此是否独立——事实上它们确实独立（各自有自己的黄金样本子集、各自的 Prompt 迭代、各自的误报率评估），没有理由不能并行推进。**这条不构成拆分 Phase 1 的理由，本章不再用它来论证任何范围收窄。**

**真正站得住的理由只有一条：`docs/KNOWN_ISSUES_sonnet-5.md` §1.15 的治理原则，它的依据是风险控制，不是产能不够。** 原文是：”新的探索性 Agent 分析指标，优先用外部脚本消费稳定的 `vmr-report.json` / `journey-*.json` 数据契约做验证，证明价值后再评估是否合入主仓库。”该条目在同文档的汇总表里被标注为”不是一个可修的条目，是一条持续性约束”——即这不是”等以后有空再照做”的建议，而是团队已经确立、当前仍然生效的工作方式。这条原则要解决的问题，与团队有多少人手、AI 辅助能放大多少产能都无关——它解决的是：VMR 的核心价值主张是可信的、基于证据的审计数据，一个还没有验证过误报率的 LLM 判定逻辑，一旦直接焊进正式的 Finding 分类体系，污染的是这份数据本身的可信度，而不是消耗了多少人力。哪怕有无限算力和无限并行的 AI 辅助，一个还没跑过黄金样本集校准的判定逻辑，也不应该直接出现在生产报告里冒充”规则核实的事实”——这是一个信任边界问题，不是一个吞吐量问题。这才是 Phase 1 应该拆成 1a（可以立即产出、立即合入）和 1b（必须先完成校准、证明误报率可控，再决定要不要合入）的唯一站得住脚的理由。

基于这一条理由，Phase 1 拆分为：

- **Phase 1a**：纯规则/统计增强，零 LLM 成本，不产生新的”裁决类” Finding，可以直接落 `internal/story`/`internal/chatmsg` 正式代码路径。
- **Phase 1b**：LLM 判定原型，先用外部脚本消费 `vmr-report.json`/`journey-*.json` 数据契约、对照黄金样本集验证误报率，产出校准报告后再决定是否合入正式代码路径——这六个模块（E3/E4/E5/E7/计划语义核销/E2 改造版）彼此独立，**没有理由必须排成一条串行队列**，应该视 AI 辅助产能情况尽量并行推进；9.7 节给出的排序建议不是”人手不够所以先做几个”，而是”先用 1-2 个模块把校准方法论本身打磨到位、再复用到其余模块”的效率考虑。

Phase 1a、Phase 1b 都不是"因为忙不过来所以只做这些"的妥协范围——原路线图第 3 章列出的所有 Layer A/Layer B 能力，本章全部纳入了 Phase 1a 或 Phase 1b，没有因为预算或人力砍掉任何一项。两者的区别只在于：Phase 1a 是"改完测试通过即完成"的确定性工程改动，Phase 1b 是"改完还要用黄金样本集证明误报率可控才算完成"的信任边界改动——这是任务性质不同，不是范围妥协。

Phase 2、Phase 3 的相对顺序沿用原路线图，不需要大改，但个别模块的工程难度评分需要基于本章 9.4 节核实到的既有基础设施重新估计（尤其是跨 Agent 子目标对齐）。

#### 9.1.3 评分维度补一维：架构合规性风险

原路线图的四维评分（用户价值/工程难度/LLM 调优难度/成本开销）里没有一维覆盖”这条建议是否与已确立的架构边界或治理原则冲突”。这不是吹毛求疵——第 2 章的深度代码盘点里，两处最实质的问题（OpenAI 协议错误漏报该不该修、`reasoning_action_mismatch` 只看最后一句该不该扩大窗口）都不是工程能力问题，而是”这条建议是不是在推翻一个团队已经用真实语料验证过的决定”。9.6 节会把这一维打分补齐到关键任务上。

---

### 9.2 Phase 1a：纯规则修复与统计增强（零 LLM 成本）

#### 目标

在不引入任何 LLM 调用的前提下，修复现有规则检测器里已确认的、导致系统性误报/漏报的具体缺陷，并补齐语料级统计能力。这一阶段的产出可以直接进入 `internal/story`/`internal/chatmsg` 正式代码路径，因为它不涉及”探索性 LLM 判定”，不受 KNOWN_ISSUES §1.15 治理原则约束。

#### P1a.1 实体提取重构

**现状**：`internal/chatmsg/entities.go` 里 `entityRe` 的正则只匹配 URL 和带 1-6 位扩展名的文件名（如 `main.go`），对无扩展名的系统路径（`/etc/hosts`、`cmd/vmr`）、目录路径（`internal/story/`）、代码符号/函数名（`ExtractEntities`）、CLI 子命令（`git commit`）系统性漏报。

**影响面比表面看到的更大**：这个函数不止被 `internal/story` 里 5 个检测器依赖（`unused_tool_result`、`unverified_entity_reference`、`reasoning_action_mismatch`、`plan_execution_misalignment`、`constraint_text_dropped`），`internal/report` 半区也在用——具体是 `internal/report/recextract.go` 里两处调用点，结果写入 `internal/report/rows.go` 定义的 `SwallowedEntities` 字段，最终在 `internal/report/section_compaction.go` 里渲染成 Compaction 吞噬检测的输出。这是一次**跨越两半区共享基础设施的改动**。CLAUDE.md 明确把”`ctxgraph`/`chatmsg` 是消息哈希与解析的唯一真相来源”列为不可破坏的不变量，这条不变量本身就是在提醒这类共享基础设施改动的辐射半径。

**实现路径**：重构 `entityRe`，扩展匹配规则支持：(1) 以 `/` 或 `./` 开头的标准路径；(2) 常见代码标识符（驼峰/下划线命名的符号）；(3) 已知 CLI 子命令模式。改动落在 `internal/chatmsg/entities.go` 本文件，不涉及行数预算风险（该文件小，未在 `fileLineExemptions` 里登记）。

**验收标准**：除了 `internal/story` 侧现有测试，必须补一轮 `internal/report` 侧回归——用真实语料跑一遍 Compaction 吞噬检测，确认 `SwallowedEntities` 的判定结果没有因为正则扩大而漂移（新正则如果把误报模式引入到目录路径提取，`report` 半区会先于 `story` 半区暴露问题，因为它是纯批量统计，没有 LLM 兜底层）。

#### P1a.2 重复工具调用参数规范化

**现状**：`exact_repeat_tool_call` 判定要求参数字符串完全相等，JSON 键顺序不同、多一个空格、多一个 `--verbose` 标记都会逃逸检测。

**实现路径**：在比对前对参数 JSON 做键排序 + 空白规范化（JSON Canonicalization）。这是一个独立的纯函数，建议放进新文件（如 `internal/story/toolcall_normalize.go`），不要塞进已经只剩 33 行余量的 `findings.go`。

#### P1a.3 通用 Shell 验证意图识别（规则候选层，不做 ToolResult 文本关键字嗅探）

**现状与两个需要分开处理的问题**：`ErrorRecoveryCount` 依赖的 `isErrorMarker`（`internal/story/metrics.go:260-262`）只识别 Anthropic 协议原生的 `is_error: true` 标记；`verificationLikeToolRe`（`internal/story/findings.go:229-234`）只匹配固定动词（`read|get|list|check|verify|...`），当验证动作是通过 `bash` 执行 `go test ./...` 时不会命中。

这两个问题看起来像同一类”规则覆盖不全”，但核实设计文档后发现团队对第一个问题已经有过明确的、记录在案的决定，第二个问题也有意图声明，两者的处理方式必须分开：

**OpenAI 协议错误漏报——这是一条已确立的”决定不修”，不是待修缺陷。** `docs/VirtualModelRouter_Design_v4_Analytics.md` 里原文记录：”`Metrics.ErrorRecoveryCount` 在纯 OpenAI 语料上偏低估 | 只识别 Anthropic 协议的 `is_error` 内容块标记，OpenAI 协议没有对应的标准字段 | 强行在 OpenAI 响应体里猜’这是不是一次错误结果’就是’宁可粗糙也不猜语义’的反例；等 OpenAI 一侧出现可靠的结构信号再补，不用启发式凑数”。这条决定的理由是可验证的工程判断，不是想当然：对 ToolResult 自由文本做 `fatal`/`exit status 1`/`error:` 关键字匹配，会把”日志文本里提到 error”、”被引用的报错样例”、”代码本身包含 error 字符串”和”这次调用真的失败了”混为一谈——这正是 VMR”揭示事实而非裁决”的原则要拒绝的那类模糊猜测。**本章维持这条决定不修，不推翻。**

但这不代表这个问题永远没有解法。设计文档给出的判据是”等 OpenAI 一侧出现可靠的结构信号再补”——这是一个可以被证据推翻的条件句，不是永久封闭。**因此本章新增一个调查型子任务**：核查 VMR 实际语料里，OpenAI Function Calling 的工具返回是否普遍存在可复用的结构化错误字段（部分实现允许工具端返回 `{"error": ...}` 形式的结构化 JSON）。如果语料证据支持存在这样的结构化信号，才对该信号本身做识别——绝不对自由文本做关键字匹配。这个调查任务产出的是一份证据报告，不是代码改动；如果证据支持，才在 `docs/KNOWN_ISSUES_sonnet-5.md` 里更新该条记录并说明推翻的理由，再进入实现；如果证据不支持，就把调查结论本身记录进 KNOWN_ISSUES，避免下一个人重新提出同一个已经被证伪的方案。

**`verificationLikeToolRe` 范围窄——这也是刻意为之，不是遗留缺陷。** 设计文档原文把它描述为”一个局部启发式，不是正式的读写分类器”，代码里的注释同样写”deliberately local, self-limited heuristic”——即团队已经有意识地把它限定在小范围、宁可漏检（安全的失败方向）。但这不意味着不能扩展，只是扩展的方式要符合 Layer B 范式：`bash`/`exec` 的验证意图识别如果要做，应该是**规则候选 + LLM 判定**（先用规则从命令行参数里筛出包含 `test`/`verify`/`check`/`lint` 等关键字的 `bash` 调用作为候选，再由 LLM 判断这次调用在上下文里是不是真的起到了验证作用），归入 Phase 1b，不在 Phase 1a 里用纯规则关键字表强行扩大——纯规则关键字表只会重复 `entities.go` 那种正则打地鼠的历史。

**本节 Phase 1a 范围内实际要做的事**：只是这个调查子任务本身，以及为 Phase 1b 的规则候选层准备好”从 `bash`/`exec` 命令行参数里提取候选验证意图”的规则提取函数（零 LLM 成本的纯规则部分）。

#### P1a.4 语料级 Context Rot 拐点分析

**现状**：`vmr story -corpus`（`internal/story/corpus.go`，当前 291 行）已经实现了分桶分布统计（`computeDistribution`/`percentile`）和秩相关系数计算（`spearman`/`rankValues`/`pearson`），是 `ComputeCorpusStats` 的一部分。

**实现路径**：在这套既有统计基建上加一个新维度——按 Context 窗口大小分桶（0-32k、32-64k、64-128k……），统计每个桶内的 Finding 密度与工具报错率，拟合质量拐点。这是复用现有管线加一个统计维度，不是从零建模块——原路线图给这项打”工程难度 2（低）”是对的，理由应该明确写成”复用 `corpus.go` 现有基建”，避免误以为要新写一套语料聚合代码。

#### P1a.5（新增）工具调用序列模式挖掘

调研报告里提到、但原路线图 14 项评估矩阵完全没有覆盖的一项能力：用 n-gram/序列模式挖掘识别”高频高成功率路径”（可以固化进 Prompt 的最佳实践）与”高频低成功率路径”（重点改进对象）。这是纯 Go 统计、零 LLM 成本的能力，和 P1a.4 的 Context Rot 拐点分析同属语料级统计，实现成本相近，理应一起排进 Phase 1a——原路线图完全没有讨论过这项能力是真实的疏漏，不是”评估后判断优先级低”，因此本章直接把它补进来，而不是单独留作待议项。

**实现路径**：同样落在 `internal/story/corpus.go`，与 P1a.4 共享分桶/统计管线；对工具调用序列做 n-gram 提取，按序列的下游成功率排序。

#### P1a.6（新增）Token 效率比统计代理指标

调研报告里的”Token 效率比”（有效产出 token / 总 token，衡量输出里有多少是无意义/重复内容）目前在 VMR 里没有对应指标——现有的 `DuplicateActionRate`（`internal/story` 九项行为剖面之一）覆盖的是”重复工具调用”，不是”输出内容本身有多少是废话”，两者不是一回事。

**Phase 1a 范围内能做的部分**：一个纯规则的代理指标——统计 Assistant 输出文本内部的 n-gram 重复率（例如连续多段近乎相同的解释性文字），作为”废话率”的粗糙下界代理。这不能替代真正的语义判断（同一件事换一种说法说两遍，n-gram 重复检测抓不到），完整版本需要 LLM 参与语义判断，归入 Phase 2 与 E10（工具输出利用率）合并处理（见 9.4 节）。

#### P1a.7 计划解析格式扩展（纯规则部分）

**现状**：`plan_execution_misalignment` 依赖的 `numberedListRe`（`internal/story/findings.go`）只捕获 `1. 2. 3.` 格式，对 Markdown 任务列表（`- [ ]`/`- [x]`）、`Step N:` 等格式完全无法识别。

**Phase 1a 范围内能做的部分**：只做格式解析扩展本身——支持 Markdown Checklist 与 `Step N:` 两种主流规划表达的正则/解析扩展，产出结构化的计划条目列表。这一步是纯规则、零 LLM 成本的。

**不在 Phase 1a 范围内的部分**：动态重规划识别（识别”中途出现新 Bug 后重拟计划”并重置跟踪基准为 Plan v2）和”逐项语义完成度核销”都需要语义判断，归入 Phase 1b（见 9.3 节）。

#### Phase 1a 新文件规划表

实测行数：`internal/story/findings.go`（547/580 行，剩 33 行）、`findings_toolresult.go`（288/320 行，剩 32 行）、`metrics.go`（414/470 行，剩 56 行）——这三个文件的预算余量都只有 30-60 行。这张表要解决的纯粹是”新代码放哪个文件”的问题，**不代表 Phase 1a 的任务范围因此被压缩**：7 项任务全部落地，只是分散到合适的文件里，这也是这个代码库一贯的组织方式（`compare.go`/`llm_divergence.go`/`llm_single.go` 都是各自独立的文件），不是为了绕开预算才采用的临时变通：

| 任务 | 改动位置 | 是否新建文件 |
|---|---|---|
| P1a.1 实体提取重构 | `internal/chatmsg/entities.go` | 否，原地改（文件小，无预算风险） |
| P1a.2 参数 JSON 规范化 | 新文件 `internal/story/toolcall_normalize.go` | 是 |
| P1a.3 调查子任务 + 规则候选提取 | 新文件 `internal/story/verification_intent.go` | 是 |
| P1a.4 Context Rot 拐点 | `internal/story/corpus.go` | 否（该文件未在预算表登记，走默认 700 行上限，有余量） |
| P1a.5 工具序列模式挖掘 | `internal/story/corpus.go`（与 P1a.4 共享） | 否 |
| P1a.6 Token 效率比代理指标 | 新文件 `internal/story/verbosity.go` | 是 |
| P1a.7 计划格式解析扩展 | 新文件 `internal/story/plan_parse.go` | 是 |

落地前跑一次 `wc -l internal/story/corpus.go`，确认 P1a.4+P1a.5 两项加起来不会把这个文件推过默认 700 行上限；如果接近，同样拆成新文件（如 `internal/story/corpus_contextrot.go`）。如果未来某个改动确实在逻辑上必须长在一个已经顶格的老文件里、拆分反而破坏内聚性，正确做法是重新评估该文件的预算数字本身（`fileLineExemptions` 表是人为设定的配置，可以随工程判断调整），而不是因为预算放弃这个改动。

---

### 9.3 Phase 1b：LLM 判定原型（外部脚本验证优先，暂不合仓库）

#### 目标

验证 Layer B 能力（目标漂移、语义死循环、任务完成度、工具结果曲解、Compaction 否定式约束丢失、计划语义核销）在真实语料上的误报率，产出校准报告；只有校准结果达标，才决定是否合入 `internal/story` 正式代码路径。这一阶段的产出物首先是**校准报告**，其次才是代码。

#### 治理规则：为什么先用外部脚本

`docs/KNOWN_ISSUES_sonnet-5.md` §1.15 的原话已经在 9.1.2 节引用过，这里不再重复其文字，只重申其含义：Phase 1b 的所有新判定逻辑，第一步是写成消费 `vmr-report.json`/`journey-*.json` 这两个稳定 JSON 数据契约的独立脚本（可以用 Python 或 Go，跑在仓库之外或 `_tmp/` 下），不直接改 `internal/story` 的正式代码路径。原因很直接：这批能力全部要求 LLM 输出此前不存在的新判定（不是解读已有数字），在没有验证误报率之前把它们焊进正式 Finding 体系，一旦误报率高，返工成本（删除 Finding Code、改渲染逻辑、处理已发布报告里的历史数据）远高于”先在脚本层跑一遍再决定”。

#### 黄金样本集

在正式合入代码之前，先建立一个具备真实人工标注的代表性 Journey 样本集：30-50 个 Journey，覆盖已知死循环、真实目标漂移、真实工具报错、正常成功完成等各类型场景。合入决策前，样本集暂存在仓库外或 `_tmp/` 下（例如 `_tmp/story-eval-corpus/`）；一旦某个检测器的校准报告确认可以合入，再把对应的样本迁移进 `internal/story/testdata/`，作为该检测器的长期回归基准。

#### 架构决策：LLM 解读层能不能产出新的 Finding

这是 Phase 1b 六个模块共同踩中的一条边界，必须先做出决策，而不是每个模块单独讨论一遍。

**现状核实**：`internal/story/llm.go` 里有一条注释，原文是”the LLM only ever receives numbers this package already computed (Comparison/ComparisonExtras), never generates a new number itself, and its output is rendered as a clearly-labeled, separately-cached block”——这条约束准确的表述是”LLM 不生成新数字”，字面上并没有禁止”新 Finding”（这是需要澄清的一处细节：约束原文只提到 number，但从”clearly-labeled, separately-cached block”这个精神看，约束的意图显然也覆盖”不能让 LLM 的判断和规则层的判断混在一起、被误当成同等权威的事实”）。同时，`vmr story` 单 Journey 场景的 LLM 交互里已经存在一条独立的自然语言层面的例外通道——system prompt 明确禁止”自己发现清单之外的新问题”，除非明确标注”这是我自己的阅读判断，不是规则核实的 Finding”。也就是说，**”LLM 可以给出标注为推测的新判断”这件事本身在现有实现里已经有一个非正式的先例**，只是目前这个标注是自由 Markdown 文本，不是结构化字段。

**决策**：允许 Phase 1b 的 LLM 判别器产出新的、结构化的 Finding，但必须满足以下条件，把现有的非正式”标注为推测”惯例升级成结构化机制：

1. 每个 LLM 产出的 Finding 必须带 `Source: "llm_inferred"` 字段，与规则产生的 Finding 在数据结构层面明确区分；渲染层（Markdown/CLI 输出）必须视觉上区分开——不能让读者混淆”这是规则算出来的事实”和”这是 LLM 给出的推测”。
2. 置信度必须是结构化的三档离散值 `"HIGH"|"MEDIUM"|"LOW"`，不能是数值（如 `0.95`）。这里要澄清一个和原路线图设计不完全一致的地方：现有的 LLM 解读层里，三档置信度目前是要求 LLM 在自由 Markdown 文本/表格里填写的文字（`internal/i18n/story_llm.go` 里多处定义了这个判据措辞），并不是某个 Go 结构体上的结构化字段——Phase 1b 要做的是把这个”三档”约定从自由文本形式升级为结构化 JSON 字段，这是一次形式升级，不是简单复用现状，需要在设计新的证据包/Prompt 契约时明确写清楚。判据措辞沿用现有的：”能指认具体证据锚点才能标’高’；间接证据/需要推断标’中’；纯排除法/直觉标’低’，不能为了显得更确定而拔高置信度。”
3. 必须有明确的证据锚点（`evidence_anchor`）——能具体指认触发判定的原文片段（工具返回文本、推理文本的具体句子），不能是笼统的”整体感觉不对”。
4. 只有置信度为 `HIGH` 且能明确指认证据锚点的判定，才允许在报告里以 Finding（⚠️ 图标）形式呈现；`MEDIUM`/`LOW` 只作为辅助解读参考，不计入 Finding 统计。

**分项决定哪些模块可以直接做、哪个模块需要改造问法**：

- **E3（工具结果曲解）、E4（语义死循环）、E5（目标漂移）、E7（Compaction 否定式约束丢失）——越界正当性强，可以按上述条件直接实现。** 它们判断的都是”VMR 已经拿到手的证据内部是否自洽”——工具返回文本 vs 后续推理文本是否矛盾、连续几次工具调用是否在原地打转、当前步骤是否还服务于用户原始诉求、被压缩掉的文本是否包含关键约束。这些判断不需要 VMR 对它看不见的外部现实（沙箱执行结果、用户真实验收标准）下裁决，本质仍然是”证据范围内的推测”，只是推测对象从”一个数字”扩展成了”一个语义判断”。
- **E2（任务完成度四阶逼近判定）——正当性弱，不能照单实现，必须改造问法。** `COMPLETED_VERIFIED`/`FAILED_ABORTED` 这类判决问的是”任务在现实世界里是否真的达成”，这恰好是 VMR 结构性看不到的东西——没有沙箱执行反馈，没有用户验收信号。`docs/VirtualModelRouter_Design_v4_Analytics.md` 里”无成功/失败标签：VMR 零埋点前提意味着结构性拿不到任务是否真正达成目标的信号”和 6c 分叉点一节”绝不判断哪一方更好——VMR 没有任务是否真正达成目标的信号，这个判断超出证据能支持的范围”这两条原话，说的正是这类判断。四态判决的措辞（尤其是”COMPLETED_VERIFIED”）本身就暗示”这是可信结论”而非”这是一次基于文本的合理推测”，容易被下游用户当真值使用。

  **改造方案**：把问法从”任务是否完成”改写成 VMR 证据范围内能立住的事实性问题——”轨迹中的最终声明是否有对应的验证动作支撑”，输出三态：`CLAIM_WITH_VERIFICATION`（最终声称完成，且轨迹中有明确的验证动作，如测试通过、读取确认）、`CLAIM_WITHOUT_VERIFICATION`（最终声称完成，但轨迹中找不到对应的验证步骤）、`NO_COMPLETION_CLAIM`（没有明确的完成声明，如中途中断或仍在进行）。这个改造保留了 E2 最核心的用户价值——拆穿”说完成了但没验证”这种最常见的隐性失败——同时不让 LLM 冒充一个它给不出的权威结论。新 Finding 类型命名为 `FindingUnverifiedCompletionClaim`，只在 `CLAIM_WITHOUT_VERIFICATION` 时触发。

#### Phase 1b 具体任务

**E3 工具结果曲解检测（`FindingToolResultMisinterpretation`）**
- 实现路径：扫描 `(ToolResult, NextReasoning)` 对，由 LLM 判断 Agent 是否对工具返回的明确否定/报错结果产生了相反的乐观幻觉解读（如工具返回 404，模型却推理”已成功获取数据”）。
- 用户价值：这类”指鹿为马”式的推理故障在学术界 RCA 调研中占比极高（71.2%），是 Layer B 里用户价值最高的一项。

**E4 语义死循环/振荡检测（`FindingSemanticOscillation`）**
- 实现路径：规则层先用滑动窗口（如连续 6 步内同一工具被调用 ≥3 次）圈出候选组，即使参数 Hash 不相等（P1a.2 的 JSON 规范化已经消除了纯格式差异导致的漏检），再由 LLM 判断这几步操作是否在”缺乏进展的情况下原地打转”（如搜索词反复加同义词、分页偏移量+1、对同一个不存在的文件反复变换路径尝试）。
- 依赖关系：这一步依赖 P1a.2 已经完成，否则候选组会被大量格式噪声污染。

**E5 目标漂移检测（`FindingGoalDrift`）**
- 实现路径：每隔 K 步抽取当前推理摘要，连同用户原始 Prompt 一起交给 LLM，判断当前子目标是否仍服务于根目标，给出漂移置信度与漂移点定位。
- 用户价值：长程任务里的隐性跑偏是排障成本最高的场景之一，人工回溯往往要重读全程对话才能定位漂移起点。

**E7 Compaction 否定式约束丢失评估（对既有 `FindingConstraintTextDropped` 的扩展，不是新 Finding 类型）**
- 现状：这个 Finding 类型已经存在，但目前只对比压缩前后被吞噬的**实体**（`SwallowedEntities`）。像”请始终使用中文回答”、”严格禁止修改 package.json”这类不含具体文件名的否定式约束，在 Compaction 中被裁剪时现有检测器完全沉默。
- 实现路径：引入 System Prompt 与前序指令的差分摘要，由 LLM 判断被裁掉的文本片段是否包含关键否定式约束或行为规范，作为对现有实体检测的补充信号源，触发同一个 `FindingConstraintTextDropped`（增加一个子类型标记区分”实体丢失”和”约束丢失”两种触发原因）。

**计划语义核销（配合 P1a.7 的规则解析）**
- 实现路径：P1a.7 已经把 Markdown Checklist / `Step N:` 解析成结构化条目列表；本任务在此基础上做两件事：(1) 动态重规划识别——对中间 Step 出现显著规划重构的，记录为 Plan v2 并重置跟踪基准；(2) LLM 语义核销——对提炼出的关键计划条目做逐项语义完成度判断（而不是原来”字面实体是否出现”的粗糙代理）。

**E2 改造版：`FindingUnverifiedCompletionClaim`**
- 方案见上文”架构决策”部分，此处不再重复。

#### 数据契约：扩展 `SingleJourneyEvidencePack`

现状核实：`internal/story/llm_single.go` 里 `SingleJourneyEvidencePack` 当前只有 `Journey`、`Metrics`、`Findings`、`TaskTitles`、`ToolIndex` 五个字段；`BuildSingleJourneyEvidencePack(j *Journey, m Metrics, findings []Finding, lang i18n.Lang)` 的函数签名明确接收调用方已经算好的 `Metrics`/`Findings`，注释原文说明这是为了让已经持有这些值的调用方（`cmd_story.go` 的 `writeJourneyFile` 就是这样的调用方）不用重复付出 `ComputeMetrics`/`ComputeFindings` 的计算成本。

Phase 1b 需要新增的字段——`UserIntent`（用户首个任务的原始需求）、`FinalOutcome`（最后一步的最终输出）、`SuspiciousPairs`（针对 E3 抽样的关键工具/响应对，每对包含 `StepSeq`/`ToolName`/截断至 500 字符的 `ToolResultText`/紧随其后的 `NextReasoning`）——同样应该遵循这个既有惯例：在调用方已经拥有 `Journey`/`Metrics`/`Findings` 之后一次性派生这些新字段，不要在 `BuildSingleJourneyEvidencePack` 内部重新扫描 `Journey.Tasks`。

#### 复用现有基础设施（不需要重新建设的部分）

以下三项是原路线图第 6 章误当作”待建议、待支持”提出的内容，核实后确认均已在现有代码里实现，Phase 1b 只需要直接复用：

- **Prompt 版本化缓存失效**：`llm.go` 里 `promptSpec` 结构体已经有 `Version string` 字段，`evidencePackKind` 接口要求每个判别器实现 `promptSpec(lang) promptSpec`，`cacheKey` 函数把 `Version` 写入缓存键的哈希——Prompt 改了版本号，旧缓存自动失效。`llm_single.go` 现有的单 Journey 解读层已经用这个机制定义了自己的版本号。Phase 1b 每个新判别器只需要定义自己的 `Version` 字符串（如 `"tool-misinterpretation-v1"`），不需要另起一套版本化方案。
- **本地 LLM 端点 / 数据不出网**：`vmr story` 的 `-llm-addr host:port` 参数指向”一个正在运行的 VMR 实例”，通过标准 `POST /v1/chat/completions` 调用，不直接对接 OpenRouter 或任何云端 API。只要该 VMR 实例的 `config.yaml` 把某个虚拟模型指向本地 Ollama/vLLM 端点，”数据不出本地网络”这个诉求已经天然满足。这同时回答了 Phase 1b 新增 LLM 调用的成本失控担忧——这些调用会被目标 VMR 实例自己的 quota/pricing 记账覆盖，不存在脱离配额追踪的意外成本。
- **Fail-open 降级**：`llm.go` 里已有注释明确”两种失败模式都是按设计 fail-open 的”——无 API Key、网络失败、缓存写入失败均不中断规则层输出，只在 stderr 打印警告，纯规则层产出的 `.md`/`.json` 完整可用。Phase 1b 新增的判别器复用同一条错误处理路径即可。

---

### 9.4 Phase 2：中期演进

#### 目标

在 Phase 1b 验证过的 LLM 判定能力基础上，构建更深度的单 Journey 诊断能力和跨 Agent/跨模型的结构化对比能力。

#### P2.1 单 Journey 证据包深潜（Focused Evidence Sub-pack）

**现状**：证据包目前只传递宏观的 Metrics、Findings 清单、Task 标题和单行 ToolIndex 摘要，LLM 看不到具体报错详情、关键 ToolResult 文本或 Compaction 丢失的具体文字片段，解读容易停留在”该任务共耗时 X 秒，发生了 2 次重复调用”这种复述层面。

**实现路径**：当规则层触发了 Finding 或定位了分叉点时，自动将触发点前后 2 步的原始 Request/Response 上下文切片打入证据包，让 LLM 具备微观病因诊断能力（例如”为什么第 8 步会选错工具”）。这一项建立在 Phase 1b 已经扩展过的 `SingleJourneyEvidencePack` 结构体之上，是同一批字段的进一步深化，不是另起一套证据包机制。

#### P2.2 故障模式自动分类（Fault Taxonomy）

**实现路径**：基于 MAST 与 Agentic AI Fault 分类体系，建立标准故障标签枚举，产出标准化《故障归因诊断卡》。

**需要显式裁剪的一类**：MAST 分类体系里的”多 Agent 通信失败”大类（交接信息丢失、角色边界模糊、协调死锁）建立在”能观测到多个 Agent 之间的通信”这个前提上。VMR 是单客户端代理，一个 Journey 里的所有 LLM 调用都来自同一个 Agent 进程发起的请求流，天然没有”另一个 Agent”的数据源可比对。归因分类枚举应该只取 MAST 里 L1（认知编排）、L2（执行工具）、L3（记忆感知）、L4（环境限制）四类，加上 RCA 分类体系里”内部推理失败”子类，明确排除”多 Agent 通信失败”这一类——否则归因分类枚举里会永远有一类挂零，产出的诊断卡看起来像是漏检而不是”这个维度本来就不适用”。

#### P2.3 跨 Agent 轨迹子目标对齐（Sub-goal Alignment）

**现状核实——这不是从零开始的能力**：`vmr story -compare` 已经落地了跨 Journey/跨 Agent 对比的基础设施——`internal/story/compare.go` 里的 `ComparisonExtras`（模型/端点核查、Prompt 缓存曲线、System Prompt 稳定性、最终交付物节选对比）、`computeDivergence`/`flattenWithTask`（分叉点结构化定位，产出 `DivergencePoint`，区分 `DivergenceHeavy`/`DivergenceLight` 两种分叉严重度）、以及 6c 分叉点专属的 LLM 解读层 `internal/story/llm_divergence.go`，均已在真实语料上验证过（同一任务两个不同 Agent 框架的对比案例），并且已经做到”这份数据第一次被结构化定位到具体分叉步骤”这种程度的落地深度，不是一个空白领域。

**实现路径**：E11 真正要新增的，只是在已有 `computeDivergence` 结构对齐、分叉点定位的基础上，把”结构分叉点”升级为”语义子目标对齐”这一层——由 LLM 提取两套异构轨迹的逻辑里程碑，映射到统一的子目标轴上，消除不同 Agent 框架步骤粒度不一致（A 框架 1 步完成，B 框架拆 3 步）带来的对比偏差。**工程难度评分应该基于”在已有基建之上做语义细化”重新估计，而不是从零假设**——原路线图给这项打”工程难度 4（高）”是基于”从零建设”的假设，实际上底层的结构对齐、位置定位地基已经存在，真正的新增工作量集中在语义对齐这一层，难度应该下调。

#### P2.4 工具输出真实利用率与上下文污染指数（合并两项）

**现状**：原路线图的 E10（工具输出真实利用率）只覆盖”用了多少”——追踪工具返回的具体数据在后续 Assistant 推理/回复中的引用与转化比例；调研报告里的”上下文污染指数”（冗余率/无关率/冲突检测）覆盖的是”塞进去的内容里有多少是重复/无关/自相矛盾的”，原路线图完全没有提及这个维度。这两者关系密切，合并成一个模块的两个子指标，而不是分别单独立项。同时，P1a.6 阶段做的”Token 效率比”n-gram 重复率代理指标在这里做语义层面的深化——把”文本层面重复”升级为”语义层面重复/无关/冲突”的 LLM 判断。

**实现路径**：规则层先做候选筛选（大段工具返回内容 + 后续对话窗口），LLM 抽样判断利用率、冗余度、冲突程度，产出综合的”Context Bloat 浪费评分”。

---

### 9.5 Phase 3：远期观望

以下三项保持”技术跟踪、暂不投入主力开发”的结论，原因需要具体说明，不能只给结论：

**错误级联传播因果图反向回溯**：在没有代码执行语义追踪的前提下，纯靠离线证据反推因果链，容易产生连环推测——每一步因果链接都是一次 LLM 推测，链条越长，复合误报率越高，成本效益极低。

**质量-成本帕累托前沿分析（TQS）**：权重设定主观性强，行业缺乏公认标准，过度追求单项综合数值容易掩盖具体维度的问题；且这类综合指标本身依赖前面提到的”任务完成度”这类判断的可靠性，而 E2 已经在 9.3 节被改造成更保守的三态判断，用一个更保守的信号去支撑一个更激进的综合评分，方向上不匹配。

**决策方差与稳定性自动基准测试**：这一项迟迟排不进 Phase 1/2，不只是”ROI 边际递减”这么简单，有一个具体的技术缺口——核实后确认 VMR 目前**没有跨 session 关联机制**，`vmr story -compare` 需要用户手动指定两个 Journey ID 才能触发对比，系统没有任何自动识别”这两个 Journey 是同一个任务被重复运行了 N 次”的机制。这个技术缺口不只挡住 E14 一项，任何”多次运行对比”类分析都会撞上同一个前提缺失。这一项本身不建议现在补（跨 session 关联本身就是一个不小的新子系统，且和”决策方差测评”绑定过紧，脱离这个具体用例先做意义有限），但记录在此，作为以后如果真的要做”多次运行对比”类分析时的已知前置依赖。

---

### 9.6 需要写入 `docs/KNOWN_ISSUES_sonnet-5.md` 的两条决策记录

本路线图的落地过程会做出两个此前没有显式记录过的架构决策，落地前应该分别在 KNOWN_ISSUES 里补一条，把决策和分项论证记录下来，而不是让决定隐性地发生在某次 PR 评审里：

**决策一：LLM 解读层是否可以在明确置信度分级 + 来源标记下产出新 Finding。** 记录 9.3 节”架构决策”部分的完整分项论证——`Source: "llm_inferred"` 标记机制、HIGH/MEDIUM/LOW 结构化置信度、证据锚点要求、E3/E4/E5/E7 与 E2 的正当性差异及 E2 的问法改造方案。

**决策二：OpenAI 协议工具错误漏报是否需要重新评估。** 记录 9.2 节 P1a.3 的调查子任务结论——无论证据是否支持存在可复用的结构化错误字段，都要把调查结论写下来：如果支持，更新原有”决定不修”条目并说明推翻理由；如果不支持，明确记录”已调查、证据不支持、维持决定不修”，避免下一次有人重新提出同一个已经被证伪的关键字嗅探方案。

---

### 9.7 落地纪律清单

在开始写代码之前，落地团队应该确认以下几点，这些不是可选的最佳实践，而是本路线图能不能真正跑起来的前提条件：

1. **Phase 1a 的每个任务都对照 9.2 节末尾的新文件规划表**，不要往 `findings.go`/`findings_toolresult.go`/`metrics.go` 这三个已经逼近预算上限的文件里加代码；改动前先跑一次 `wc -l` 确认目标文件的实际余量，因为文件行数会随着其他并行改动变化。
2. **P1a.1 实体提取重构必须跑 `internal/report` 侧回归**，不能只在 `internal/story` 侧测试通过就认为安全——这是本路线图里唯一一处明确要求跨半区回归验证的改动。
3. **Phase 1b 的六个模块彼此独立，建议尽量并行推进，不要排成一条串行队列**——排序考虑不是”人力有限所以按批次做”，而是方法论复用：先用 1-2 个”越界正当性强”（E3 或 E4——判断证据内部自洽、风险最低、方案最成熟）的模块，把黄金样本标注规范、证据锚点评分口径、外部脚本骨架这套校准方法论打磨到位，验证过一遍之后，再把这套方法论复用到 E5、E7、计划语义核销上，这几个可以并行展开，不需要等前面的模块先合入。E2 的改造版本（`FindingUnverifiedCompletionClaim`）需要单独对待——不是因为排期靠后，而是因为它的问法本身是本章新改造的（四态裁决改三态事实性判断），需要先确认这个新问法在真实语料上确实比原方案更少产生误导性输出，这是一个独立的验证目标，不适合和其他五个模块共用同一批判断标准。
4. **黄金样本集的 30-50 个 Journey 必须覆盖 Phase 1b 六个模块各自的正反例**，不能是一套通用样本集直接套用到所有判别器——语义死循环和目标漂移需要的反例类型完全不同（前者需要”看起来在重试但其实在正常探索”的样本，后者需要”子任务拆解但没有跑偏”的样本），样本集设计本身要按模块分别规划。
5. **合入决策的判据是校准报告，不是”感觉还不错”**——每个 Phase 1b 模块合入 `internal/story` 正式代码路径之前，必须有一份基于黄金样本集的误报率数据，写入对应的校准报告，校准报告本身作为该模块合入 PR 的一部分证据留存。

---

### 9.8 行动项总表

| 阶段 | 任务 | 涉及模块/新文件 | 交付产物 | 是否需要先决策/调查 |
|---|---|---|---|---|
| 1a | 实体提取重构 | `internal/chatmsg/entities.go`（改）+ `internal/report` 回归 | 消除 5 个 story 检测器 + Compaction 吞噬检测的系统性误报/漏报 | 否 |
| 1a | 参数 JSON 规范化 | 新建 `internal/story/toolcall_normalize.go` | 消除等价参数逃逸检测 | 否 |
| 1a | Shell 验证意图规则候选层 + OpenAI 结构化错误字段调查 | 新建 `internal/story/verification_intent.go` | 规则候选提取函数 + 调查报告 | 是（调查结论决定要不要更新 KNOWN_ISSUES） |
| 1a | Context Rot 拐点分析 | `internal/story/corpus.go` | 上下文质量拐点曲线 | 否 |
| 1a | 工具序列模式挖掘（E15） | `internal/story/corpus.go` | 高频高/低成功率路径清单 | 否 |
| 1a | Token 效率比代理指标 | 新建 `internal/story/verbosity.go` | n-gram 重复率统计 | 否 |
| 1a | 计划格式解析扩展 | 新建 `internal/story/plan_parse.go` | Checklist/`Step N:` 结构化解析 | 否 |
| 1b | 工具结果曲解检测（E3） | `internal/story/llm_single.go` 扩展 + 外部脚本校准 | `FindingToolResultMisinterpretation` + 校准报告 | 是（校准通过后合入） |
| 1b | 语义死循环检测（E4） | 同上 | `FindingSemanticOscillation` + 校准报告 | 是 |
| 1b | 目标漂移检测（E5） | 同上 | `FindingGoalDrift` + 校准报告 | 是 |
| 1b | Compaction 约束丢失扩展（E7） | 扩展既有 `FindingConstraintTextDropped` | 新触发子类型 + 校准报告 | 是 |
| 1b | 计划语义核销 | 依赖 1a 的 `plan_parse.go` | 动态重规划追踪 + 逐项核销 | 是 |
| 1b | 任务完成度改造版（E2） | `internal/story/llm_single.go` 扩展 | `FindingUnverifiedCompletionClaim` + 校准报告 | 是（问法已在本章改造） |
| 2 | 单 Journey 证据包深潜 | `internal/story/llm_single.go` | 微观病因诊断 | 否（建立在 1b 验证过的字段上） |
| 2 | 故障模式自动分类 | `internal/story/compare.go` 或新文件 | 标准化故障归因卡（排除 MAST 多 Agent 通信失败类） | 否 |
| 2 | 跨 Agent 子目标对齐（E11） | `internal/story/compare.go`（已有 `ComparisonExtras`/`computeDivergence` 基建） | 跨框架公平对比 | 否 |
| 2 | 工具利用率 + 上下文污染指数（E10 合并） | 新文件 | Context Bloat 浪费评分 | 否 |
| 3 | 因果图回溯 / 帕累托前沿 / 决策方差 | — | 技术跟踪，不投入开发 | — |

写入 `docs/KNOWN_ISSUES_sonnet-5.md` 的两条决策记录（9.6 节）应该在 Phase 1b 第一个模块进入外部脚本验证之前完成，不要等到合入代码时才补记录。
