<!-- Ver 2026-08-16 18:15, by gemini-3.7-flash -->

# VMR Agent 运行时分析 Phase 1a 详细执行规划方案

> **状态**：Ready for Execution  
> **制定日期**：2026-08-16  
> **制定模型**：Gemini 3.7 Flash  
> **依据方案**：[`docs/future-strategy/agent_runtime_implementation_roadmap_gemini-3.7-flash.md`](file:///Users/stanford/code/vmr/docs/future-strategy/agent_runtime_implementation_roadmap_gemini-3.7-flash.md) 第 9 章  
> **核心原则**：纯规则/统计增强、零 LLM 调用成本、单任务独立闭环验证、架构行数预算严格受控。

---

## 0. Phase 1a 总体执行大纲与架构落点

### 0.1 范围与定位
Phase 1a 聚焦**纯规则修复与确定性统计增强**，不引入任何 LLM-as-Judge 外部调用，不产生探索性裁决 Finding，不受 `docs/KNOWN_ISSUES_sonnet-5.md` §1.15 外部脚本治理原则的阻断，完成后可直接合入 `internal/chatmsg` 与 `internal/story` 正式代码路径。

### 0.2 文件规划与架构行数预算（Archtest Budget）对照表
对照 [`internal/archtest/file_sizes_test.go`](file:///Users/stanford/code/vmr/internal/archtest/file_sizes_test.go)，Phase 1a 的所有任务均严格规划落点，避免触发核心文件行数上限（`findings.go` 预算 580 行，当前 547 行；`metrics.go` 预算 470 行，当前 414 行；`corpus.go` 预算 380 行，当前 291 行）：

| 任务编号 | 任务名称 | 改动文件路径 | 是否新建文件 | 预估代码行数 | 关联测试文件 |
|---|---|---|:---:|:---:|---|
| **P1a.1** | 实体提取重构与边界清洗 | [`internal/chatmsg/entities.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities.go) | 否（原地改） | +45 / -10 行 | [`internal/chatmsg/entities_test.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities_test.go) |
| **P1a.2** | 重复工具调用参数 JSON 规范化 | [`internal/story/toolcall_normalize.go`](file:///Users/stanford/code/vmr/internal/story/toolcall_normalize.go) | **是** | ~60 行 | `internal/story/toolcall_normalize_test.go` |
| **P1a.3** | Shell 验证意图候选提取与 OpenAI 错误调查 | [`internal/story/verification_intent.go`](file:///Users/stanford/code/vmr/internal/story/verification_intent.go) | **是** | ~80 行 | `internal/story/verification_intent_test.go` |
| **P1a.4** | 语料级 Context Rot 质量拐点分析 | [`internal/story/corpus_contextrot.go`](file:///Users/stanford/code/vmr/internal/story/corpus_contextrot.go) | **是** | ~90 行 | `internal/story/corpus_contextrot_test.go` |
| **P1a.5** | 工具调用 N-gram 序列模式挖掘 | [`internal/story/corpus_sequence.go`](file:///Users/stanford/code/vmr/internal/story/corpus_sequence.go) | **是** | ~95 行 | `internal/story/corpus_sequence_test.go` |
| **P1a.6** | Token 文本重复率代理指标 | [`internal/story/verbosity.go`](file:///Users/stanford/code/vmr/internal/story/verbosity.go) | **是** | ~70 行 | `internal/story/verbosity_test.go` |
| **P1a.7** | 计划格式解析扩展（Checklist / Step N） | [`internal/story/plan_parse.go`](file:///Users/stanford/code/vmr/internal/story/plan_parse.go) | **是** | ~85 行 | `internal/story/plan_parse_test.go` |

> [!NOTE]
> **关于老文件减肥的协同收益**：P1a.7 将原本堆叠在 [`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go) 中的 `lastNumberedList` 与 `numberedListRe`（约 40 行）抽取并下沉至新文件 [`plan_parse.go`](file:///Users/stanford/code/vmr/internal/story/plan_parse.go)，将使 `findings.go` 从 547 行下降至约 505 行，释放出 75 行的宝贵预算余量。

---

## 1. 逐任务详细实施规划与评估

---

### 任务 P1a.1：实体提取重构与边界清洗 (Entity Extraction Refactor)

#### 1. 任务背景与核心目标
现有 [`internal/chatmsg/entities.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities.go) 使用的正则表达式 `https?://[^\s"'` + "`" + `)]+|\b[\w][\w./\-]*\.[a-zA-Z]{1,6}\b` 仅识别 URL 和带 1~6 位后缀的文件名。导致无后缀系统文件（`/etc/hosts`、`Dockerfile`）、代码目录（`internal/story/`）、代码函数名（`ExtractEntities`）、CLI 命令（`git commit`）被系统性漏报，连带导致 `story` 侧 5 个 Finding 检测器及 `report` 侧 Compaction 吞噬检测失真。
本任务目标是重构实体提取规则，支持标准路径、目录、代码标识符与 CLI 常用命令，并剔除尾部标点干扰。

#### 2. 关联源码与调用链分析
- **核心定义点**：[`internal/chatmsg/entities.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities.go) 中的 `ExtractEntities(text string) []string`。
- **直接依赖点（跨半区辐射）**：
  1. [`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L356)：`detectReasoningActionMismatch`（推理与动作实体比对）、`detectPlanExecutionMisalignment`（计划条目实体比对）。
  2. [`internal/story/findings_toolresult.go`](file:///Users/stanford/code/vmr/internal/story/findings_toolresult.go#L187)：`detectUnusedToolResult`（工具结果实体利用）、`detectUnverifiedEntityReference`（已证伪实体复引）。
  3. [`internal/story/journey.go`](file:///Users/stanford/code/vmr/internal/story/journey.go#L497)：`buildCompactionInfo`（跨 Step 压缩信息吞噬）。
  4. [`internal/report/recextract.go`](file:///Users/stanford/code/vmr/internal/report/recextract.go#L233)：`buildCompactions`（聚合报告中的 `SwallowedEntities`）。

#### 3. 具体改动设计与函数/逻辑契约
- **文件改动**：[`internal/chatmsg/entities.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities.go)
- **匹配模式扩展**：
  将单一狭窄正则升级为多层级分类扫描模式（或组合正则并做标点清理）：
  1. **URL**：`https?://[^\s"'` + "`" + `)]+`（去除尾部 `,`, `.`, `;`, `)`, `]` 等非 URL 语义标点）。
  2. **绝对/相对路径与目录**：`(?:\/|\.\/|~\/)[\w.\-\/]+` 及带斜杠的目录 `[\w.\-]+\/[\w.\-\/]*`。
  3. **带扩展名文件**：`\b[\w.\-]+\.[a-zA-Z0-9]{1,8}\b`。
  4. **代码标识符**：驼峰命名（`[a-z]+[A-Z][a-zA-Z0-9]*` / `[A-Z][a-z0-9]+[A-Z][a-zA-Z0-9]*`）与常见蛇形符号（`[a-z0-9]+_[a-z0-9_]+`）。
  5. **CLI 子命令模式**：如 `(?:git|go|npm|pnpm|cargo|docker|kubectl)\s+[a-z][a-z0-9\-]+`。
- **清理逻辑**：
  在加入 `out` 之前，通过 `trimPunctuation(token)` 剥离误吸附的括号、引号、逗号、分号。
- **去重与容量**：维持 `MaxEntities = 30` 阈值与原序去重不变量。

#### 4. 修改规模估算与成本
- **代码修改**：`internal/chatmsg/entities.go`（约 45 行修改/新增）
- **测试修改**：`internal/chatmsg/entities_test.go`（约 50 行新增单测）
- **文档修改**：无，保持内部注释更新
- **总体成本**：低（纯 Go 标准库 `regexp`/`strings` 操作）

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**中等**。由于 `internal/report` 的 `SwallowedEntities` 和 `internal/story` 的 5 个检测器共享此函数，如果正则误把普通英文单词识别为实体，可能导致 `unused_tool_result` 假阳性率增加。
- **防御机制**：严格限制代码标识符的长度（≥4 字符），不对纯全小写普通单词放开；必须执行 `internal/report` 和 `internal/story` 全量测试套件与真实语料回归。

#### 6. 独立验证与验收标准
- **单测验收**：`entities_test.go` 覆盖：无后缀系统路径（`/etc/hosts`）、目录（`internal/story/`）、驼峰符号（`ExtractEntities`）、CLI 命令（`go test`）、URL 尾部标点剥离（`https://example.com/docs,` -> `https://example.com/docs`）。
- **回归验收**：`go test ./internal/chatmsg/... ./internal/story/... ./internal/report/...` 全部 PASS。
- **聚合报告验收**：确认 `recextract.go` 的 `buildCompactions` 在测试用例中 `SwallowedEntities` 输出稳定。

---

### 任务 P1a.2：重复工具调用参数 JSON 规范化 (JSON Canonicalization)

#### 1. 任务背景与核心目标
现有 [`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L129) 和 [`internal/story/metrics.go`](file:///Users/stanford/code/vmr/internal/story/metrics.go#L208) 中的 `toolCallKey` 仅对 `tc.Args` 进行原生字符串拼接（`tc.Name + "\x00" + tc.Args`）。当 Agent 多次调用同一工具，但 JSON 字段顺序不同（例如 `{"path":"a.go","line":10}` vs `{"line":10,"path":"a.go"}`）或存在多余空格/换行时，系统会判定为不同调用，导致死循环与重复调用漏检。
本任务目标是在比对前对参数执行轻量 JSON 规范化。

#### 2. 关联源码与调用链分析
- **调用点 1**：[`internal/story/metrics.go`](file:///Users/stanford/code/vmr/internal/story/metrics.go#L208) 中的 `toolCallKey(tc chatmsg.ToolCall) string`。
- **调用点 2**：[`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L129) 中的 `groupToolCallsByKey(steps []*Step)`。

#### 3. 具体改动设计与函数/逻辑契约
- **新建文件**：[`internal/story/toolcall_normalize.go`](file:///Users/stanford/code/vmr/internal/story/toolcall_normalize.go)
- **核心函数定义**：
  ```go
  // canonicalizeToolArgs normalizes JSON arguments by sorting keys and removing
  // whitespace discrepancies. Non-JSON arguments are whitespace-trimmed.
  func canonicalizeToolArgs(raw string) string
  ```
- **算法逻辑**：
  1. 去除首尾空白，若以 `{` 开头且以 `}` 结尾（或以 `[` 开头 `]` 结尾），尝试 `json.Unmarshal([]byte(raw), &parsed)`。
  2. 若反序列化成功，调用 `json.Marshal(parsed)`。Go 标准库 `json.Marshal` 会自动对 map 的 key 进行字典序升序排序，且不包含多余换行与缩进。
  3. 若反序列化失败（如非 JSON 的纯 Shell 命令），执行紧凑空白归一化（`strings.Join(strings.Fields(raw), " ")`）。
- **接入点**：修改 `metrics.go` 和 `findings.go` 中的 `toolCallKey`：
  ```go
  func toolCallKey(tc chatmsg.ToolCall) string {
      return tc.Name + "\x00" + canonicalizeToolArgs(tc.Args)
  }
  ```

#### 4. 修改规模估算与成本
- **代码修改**：新文件 `toolcall_normalize.go`（约 60 行），老文件 `metrics.go`（修改 2 行），`findings.go`（修改 2 行）
- **测试修改**：新文件 `toolcall_normalize_test.go`（约 80 行）
- **文档修改**：无
- **总体成本**：极低

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**极低**。规范化为确定性纯函数，不改变原有 `toolCallKey` 的语义，仅提升 key 的等价命中能力。
- **性能开销**：单 Step 工具调用参数通常 < 1KB，`Unmarshal` + `Marshal` 耗时在微秒级，对离线分析无感知。

#### 6. 独立验证与验收标准
- **单测验收**：
  - 测试 Key 乱序 JSON：`{"b":2,"a":1}` 与 `{"a":1,"b":2}` 生成相同 key。
  - 测试包含换行与格式缩进的 JSON。
  - 测试非法 JSON 与普通字符串命令的回退处理。
- **回归验收**：`internal/story/findings_test.go` 与 `metrics_test.go` 全绿。

---

### 任务 P1a.3：Shell 验证意图规则候选提取与 OpenAI 结构化错误调查

#### 1. 任务背景与核心目标
1. **现状痛点**：[`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L234) 中的 `verificationLikeToolRe` 仅匹配 `read|get|list|check|verify...` 等工具名。当 Agent 通过 `bash` 工具执行 `go test ./...` 或 `npm test` 时，由于工具名叫 `bash` 而被漏判。
2. **架构边界**：维持 `docs/VirtualModelRouter_Design_v4_Analytics.md` §7 的既定决策——**不对 ToolResult 自由文本做 `error:` 关键字嗅探**。
3. **目标**：
   - 编写独立的 Shell 参数意图候选提取纯函数，为 Phase 1b LLM 最终判定准备好输入；
   - 开展 OpenAI 工具调用结构化错误字段在真实语料中的存在性调查，产出调查记录。

#### 2. 关联源码与调用链分析
- **源码关联**：[`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L236) `looksLikeVerification(s *Step)`。
- **落地文件**：新建 [`internal/story/verification_intent.go`](file:///Users/stanford/code/vmr/internal/story/verification_intent.go)。

#### 3. 具体改动设计与函数/逻辑契约
- **新建文件**：[`internal/story/verification_intent.go`](file:///Users/stanford/code/vmr/internal/story/verification_intent.go)
- **核心函数定义**：
  ```go
  type VerificationCandidate struct {
      ToolName string
      Command  string
      Pattern  string // e.g. "go test", "cargo check", "lint"
  }

  // ExtractShellVerificationCandidate scans a ToolCall for verification-like shell commands.
  func ExtractShellVerificationCandidate(tc chatmsg.ToolCall) (VerificationCandidate, bool)
  ```
- **识别规则**：
  针对 `bash`, `sh`, `zsh`, `exec`, `execute_command`, `run_terminal`, `terminal` 等工具，解析参数中是否包含 `go test`, `pytest`, `npm test`, `cargo test`, `cargo check`, `git status`, `git diff`, `eslint`, `golangci-lint`, `tsc` 等验证性指令。
- **调查子任务执行**：
  编写针对 `logs/` 目录下真实语料的离线扫描脚本或测试，检查 OpenAI 协议下是否存在类似 `{"error": ...}` 的结构化 JSON 错误字段，将结论记录备查。

#### 4. 修改规模估算与成本
- **代码修改**：新建 `verification_intent.go`（约 80 行）
- **测试修改**：新建 `verification_intent_test.go`（约 70 行）
- **文档修改**：更新调查结论至设计备注或 KNOWN_ISSUES 记录
- **总体成本**：低

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**零**。该模块在 Phase 1a 作为独立的候选提取工具函数提供，不强行接入改动既有的 `detectUnverifiedSuccess` 规则，不产生非预期假阳性。

#### 6. 独立验证与验收标准
- **单测验收**：针对 `bash -c "go test -v ./..."`、`run_terminal {command: "npm test"}` 等各种包裹形态能够精准提取验证意图。
- **调查报告**：给出关于 OpenAI 结构化错误字段在语料中的覆盖情况明确结论。

---

### 任务 P1a.4：语料级 Context Rot 质量拐点分析 (Context Rot Inflection Point)

#### 1. 任务背景与核心目标
随着上下文（Context Window）不断增长，Agent 的注意力衰减与推理幻觉会呈现非线性突增（即 Context Rot 现象）。目前 [`internal/story/corpus.go`](file:///Users/stanford/code/vmr/internal/story/corpus.go) 具备分桶统计和秩相关统计，但缺乏按 Context 窗口大小分桶的 Finding 密度与错误率分析。
本任务目标是在语料级统计中新增 Context 窗口分桶维度的质量指标，计算质量突变拐点。

#### 2. 关联源码与调用链分析
- **源码关联**：[`internal/story/corpus.go`](file:///Users/stanford/code/vmr/internal/story/corpus.go#L181) 中的 `CorpusStats` 结构体与 `ComputeCorpusStats` 函数。
- **展示关联**：[`internal/story/render_corpus.go`](file:///Users/stanford/code/vmr/internal/story/render_corpus.go)。

#### 3. 具体改动设计与函数/逻辑契约
- **新建文件**：[`internal/story/corpus_contextrot.go`](file:///Users/stanford/code/vmr/internal/story/corpus_contextrot.go)（确保 `corpus.go` 不突破 380 行上限）
- **数据结构定义**：
  ```go
  type ContextRotBucket struct {
      Range          string  `json:"range"` // e.g. "0-32k", "32k-64k", "64k-128k", "128k-256k", "256k+"
      StepCount      int     `json:"step_count"`
      FindingCount   int     `json:"finding_count"`
      FindingDensity float64 `json:"finding_density"` // finding_count / step_count
      ErrorStepCount int     `json:"error_step_count"`
      ErrorRate      float64 `json:"error_rate"`
  }
  ```
- **算法逻辑**：
  1. 遍历语料中所有 Journey 的所有 Step，提取输入 Token 量（`TokensIn` 或 `Manifest.Usage.In`）。
  2. 归入预定义分桶（`0-32k`, `32-64k`, `64-128k`, `128-256k`, `256k+`）。
  3. 聚合各桶内的 Step 数、Finding 触发数与工具报错数，计算 Finding 密度与错误率。
  4. 在 `ComputeCorpusStats` 中调用 `computeContextRot(journeys)`，挂载至 `CorpusStats.ContextRot`。

#### 4. 修改规模估算与成本
- **代码修改**：新建 `corpus_contextrot.go`（约 90 行），`corpus.go`（新增字段与调用 6 行）
- **测试修改**：新建 `corpus_contextrot_test.go`（约 80 行）
- **文档修改**：无
- **总体成本**：低

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**极低**。纯只读内存聚合，复用已解析的 `Step` 与 `Finding` 数据，对原有统计字段完全向后兼容。

#### 6. 独立验证与验收标准
- **单测验收**：构造具有不同 Context 长度的 Step 样本，验证分桶边界、密度计算与空桶安全处理。
- **CLI/JSON 验收**：运行 `vmr story -corpus`，确认输出 JSON 中包含 `context_rot` 数组且数据自洽。

---

### 任务 P1a.5：工具调用 N-gram 序列模式挖掘 (Tool Sequence Mining)

#### 1. 任务背景与核心目标
在复杂 Agent 轨迹中，高频出现的工具调用连续序列（如 `[read_file -> edit_file -> bash]` 或 `[bash -> bash -> bash]`）反映了 Agent 的行为定势与策略模式。通过 N-gram 序列挖掘，可以定位“高频高效路径”与“高频低效/易错路径”。
本任务目标是在语料统计层引入 2-gram / 3-gram 工具序列提取与关联异常率统计。

#### 2. 关联源码与调用链分析
- **源码关联**：[`internal/story/corpus.go`](file:///Users/stanford/code/vmr/internal/story/corpus.go)。
- **新建文件**：[`internal/story/corpus_sequence.go`](file:///Users/stanford/code/vmr/internal/story/corpus_sequence.go)。

#### 3. 具体改动设计与函数/逻辑契约
- **数据结构定义**：
  ```go
  type ToolSequencePattern struct {
      Length      int      `json:"length"` // 2 or 3
      Sequence    []string `json:"sequence"` // ["grep", "read_file"]
      Occurrences int      `json:"occurrences"`
      ErrorRate   float64  `json:"error_rate"` // 序列末步或后续立即报错的比例
  }
  ```
- **算法逻辑**：
  1. 提取每个 Task 内部按时间发生顺序排列的工具名称列表（跨 Task/跨 Journey 截断，不跨界拼接）。
  2. 使用滑动窗口提取 2-gram 和 3-gram 序列。
  3. 统计每个序列出现频次及尾步是否关联 Finding 或 Error，按出现频次降序截取 Top N。
  4. 挂载至 `CorpusStats.ToolSequences`。

#### 4. 修改规模估算与成本
- **代码修改**：新建 `corpus_sequence.go`（约 95 行），`corpus.go`（修改 5 行）
- **测试修改**：新建 `corpus_sequence_test.go`（约 85 行）
- **文档修改**：无
- **总体成本**：低

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**极低**。纯只读统计，零侵入性。

#### 6. 独立验证与验收标准
- **单测验收**：构造包含特定重复序列（如 A->B->C）的虚拟 Task，验证 2-gram / 3-gram 的计数准确性及跨 Task 隔离性。
- **集成验收**：`go test ./internal/story/...` 全部通过。

---

### 任务 P1a.6：Token 文本重复率代理指标 (Token Output Redundancy Proxy)

#### 1. 任务背景与核心目标
当 Agent 发生思维停滞或输出车轱辘话时，Assistant 消息内会出现大量近乎相同的解释性句子或短语。现有指标 `DuplicateActionRate` 仅度量工具调用重复，未度量文本层面的冗余唠叨。
本任务目标是在 Phase 1a 提供纯规则的 n-gram 词元重复率代理指标（`OutputRepetitionRate`），作为衡量输出废话密度的确定性下界。

#### 2. 关联源码与调用链分析
- **源码关联**：[`internal/story/metrics.go`](file:///Users/stanford/code/vmr/internal/story/metrics.go)。
- **新建文件**：[`internal/story/verbosity.go`](file:///Users/stanford/code/vmr/internal/story/verbosity.go)。

#### 3. 具体改动设计与函数/逻辑契约
- **核心函数定义**：
  ```go
  // ComputeOutputRepetitionRate calculates the n-gram redundancy ratio (0.0 ~ 1.0)
  // across all assistant text outputs in a Journey.
  func ComputeOutputRepetitionRate(j *Journey) float64
  ```
- **算法逻辑**：
  1. 收集 Journey 内所有 Step 的 `RespText` 与 `Reasoning`。
  2. 对文本按 4-gram 词组（或字符块）进行滑动分词。
  3. 统计总 n-gram 数 $N_{total}$ 与不重复 n-gram 数 $N_{unique}$。
  4. 重复率计算公式：$R = 1.0 - \frac{N_{unique}}{N_{total}}$（当 $N_{total} = 0$ 时为 0）。

#### 4. 修改规模估算与成本
- **代码修改**：新建 `verbosity.go`（约 70 行）
- **测试修改**：新建 `verbosity_test.go`（约 60 行）
- **文档修改**：无
- **总体成本**：极低

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**极低**。只计算统计数值，无业务破坏性。

#### 6. 独立验证与验收标准
- **单测验收**：
  - 干净多样文本：重复率低（< 0.15）；
  - 刻意重复复述文本（如 5 次重复同一段话）：重复率显著升高（> 0.60）；
  - 边界处理：空文本返回 0.0。

---

### 任务 P1a.7：计划格式解析扩展 (Plan Parsing Format Expansion)

#### 1. 任务背景与核心目标
现有 [`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L393) 中的 `numberedListRe` 仅匹配 `1. 2. 3.` 格式，无法识别 Markdown 任务列表（`- [ ]`, `- [x]`）以及 `Step 1:`, `Phase 1:`, `步骤 1:` 等现代 Agent 广泛使用的规划表达。
本任务目标是将计划解析逻辑从 `findings.go` 抽取独立为新文件，支持 Checklist 和 Step 前缀，同时为 `findings.go` 瘦身腾出行数预算。

#### 2. 关联源码与调用链分析
- **源码关联**：[`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L393-L435) 的 `lastNumberedList` 与 `detectPlanExecutionMisalignment`。
- **新建文件**：[`internal/story/plan_parse.go`](file:///Users/stanford/code/vmr/internal/story/plan_parse.go)。

#### 3. 具体改动设计与函数/逻辑契约
- **新建文件**：[`internal/story/plan_parse.go`](file:///Users/stanford/code/vmr/internal/story/plan_parse.go)
- **支持的规划格式**：
  1. **标准数字列表**：`1. `, `1、`, `1) `, `(1) `
  2. **Markdown Checklist**：`- [ ] `, `- [x] `, `* [ ] `, `+ [ ] `
  3. **步骤前缀**：`Step 1:`, `Phase 1:`, `Stage 1:`, `步骤 1:`, `阶段一：`
- **核心函数定义**：
  ```go
  type PlanItem struct {
      Index int
      Text  string
      Kind  string // "numbered", "checklist", "step"
  }

  // ExtractActionablePlan extracts the last contiguous actionable plan items.
  func ExtractActionablePlan(text string) []string
  ```
- **对接重构**：
  在 `findings.go` 中，将原 `lastNumberedList(planText)` 替换为调用 `ExtractActionablePlan(planText)`，彻底删除 `findings.go` 中的 `numberedListRe` 及 `lastNumberedList` 实现。

#### 4. 修改规模估算与成本
- **代码修改**：新建 `plan_parse.go`（约 85 行），`findings.go`（**删除约 40 行**，净缩减行数）
- **测试修改**：新建 `plan_parse_test.go`（约 90 行）
- **文档修改**：无
- **总体成本**：极低且附带行数瘦身红利

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**低**。扩展格式后可能捕获更多真实计划列表，需确保多段非计划性列表（如纯引用文本中的 checklist）不会被错误捕获。
- **防御机制**：保持 `minPlanItems = 2` 与 `maxPlanItems = 8` 的安全过滤区间不变。

#### 6. 独立验证与验收标准
- **单测验收**：覆盖 Markdown Checklist、Step 英文前缀、中文“步骤一”前缀、混合文本中仅取最后一段连续计划等典型场景。
- **检测器验收**：`findings_test.go` 针对 `plan_execution_misalignment` 的测试用例保持全绿。

---

## 2. 全盘回顾与跨任务一致性核对 (Sanity Check)

在制定完上述 7 个任务后，进行全局视角的交叉复盘：

1. **架构行数预算核对**：
   - `internal/chatmsg/entities.go`：预计改动后约 75 行（未在 `fileLineExemptions` 登记，远低于 700 默认上限）。
   - `internal/story/findings.go`：因下沉 P1a.7，行数预计从 547 行**降至约 505 行**（预算 580 行，余量扩大至 75 行，大幅提升安全性）。
   - `internal/story/corpus.go`：因 P1a.4 和 P1a.5 均采用新建文件（`corpus_contextrot.go`, `corpus_sequence.go`），`corpus.go` 仅增约 10 行，总行数约 300 行（预算 380 行，保持 80 行安全余量）。
   - `internal/story/metrics.go`：仅做函数调用代理，行数保持在 415 行左右（预算 470 行）。
2. **依赖与接口一致性**：
   - P1a.2 的 `canonicalizeToolArgs` 位于 `internal/story` 包内，被 `metrics.go` 和 `findings.go` 共享，无循环依赖。
   - P1a.1 的改动位于底层 `internal/chatmsg`，被 `story` 和 `report` 共同依赖，符合架构分层。
3. **零 LLM 依赖承诺**：
   - 7 个任务全部为标准 Go 算法与正则解析，零外部网络 I/O，零 API 计费风险。

---

## 3. Phase 1a 完工善后与全量验收工作清单

当 Phase 1a 的 7 个任务全部单项编码并单测通过后，必须统一执行以下善后与集成验收步骤：

### 3.1 跨半区与全量测试套件回归
```bash
# 1. 核心单元测试
go test -v ./internal/chatmsg/...
go test -v ./internal/story/...
go test -v ./internal/report/...

# 2. 架构合规与代码行数预算断言
go test -v -run "TestArchitecture_CoreFileSizes|TestArchitecture_FuncSizes" ./internal/archtest/...

# 3. 全模块无缓存全量回归
go test -count=1 ./...
```

### 3.2 真实语料稳定性验证
使用现有测试语料库执行真实命令，验证无 Panic 与数据漂移：
```bash
# 验证 story 单 Journey 渲染
go run ./cmd/vmr story -audit logs/

# 验证 story 语料级聚合与新字段输出
go run ./cmd/vmr story -corpus logs/

# 验证 report 聚合不受实体提取重构破坏
go run ./cmd/vmr report -audit logs/
```

### 3.3 文档与治理记录更新
1. **决策记录**：将 P1a.3 的 OpenAI 结构化错误字段调查结论写入 `docs/KNOWN_ISSUES_sonnet-5.md`，明确记录维持或调整该项的理由。
2. **临时文件清理**：清理执行过程中的所有临时分析脚本及 `_tmp/` 调试文件。
3. **Git 提交原子性**：按照任务单元独立提交 Git Commit，Commit 消息严格遵循英文规范（如 `feat(chatmsg): refactor entity extraction to support paths and symbols`）。
