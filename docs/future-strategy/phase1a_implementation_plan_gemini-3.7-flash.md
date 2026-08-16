<!-- Ver 2026-08-16 18:15, by gemini-3.7-flash; 核实修订 2026-08-16, by Sonnet 5 -->

# VMR Agent 运行时分析 Phase 1a 详细执行规划方案

> **状态**：Ready for Execution（经源码核实修订）
> **制定日期**：2026-08-16
> **制定模型**：Gemini 3.7 Flash
> **核实修订**：Sonnet 5（2026-08-16）— 逐项对照实际源码核实本文档 7 个任务的文件依赖、函数签名、行数预算与算法细节。结论：**任务范围本身成立，但至少 3 处存在会导致实现返工或验收失败的具体错误（P1a.2 的双文件修改点不存在、P1a.6 缺少让新指标可见所需的三处接线、P1a.4/P1a.5 完全遗漏渲染层与 i18n 层的预算约束），另有若干正则误报风险与算法细节缺口需要在编码前定案**。所有修订以 `[核实修订]` 标注块的形式内嵌在对应任务小节里，不推翻原方案结构。
> **依据方案**：[`docs/future-strategy/agent_runtime_implementation_roadmap_gemini-3.7-flash.md`](file:///Users/stanford/code/vmr/docs/future-strategy/agent_runtime_implementation_roadmap_gemini-3.7-flash.md) 第 9 章
> **核心原则**：纯规则/统计增强、零 LLM 调用成本、单任务独立闭环验证、架构行数预算严格受控。

---

## 0. Phase 1a 总体执行大纲与架构落点

### 0.1 范围与定位
Phase 1a 聚焦**纯规则修复与确定性统计增强**，不引入任何 LLM-as-Judge 外部调用，不产生探索性裁决 Finding，不受 `docs/KNOWN_ISSUES_sonnet-5.md` §1.15 外部脚本治理原则的阻断，完成后可直接合入 `internal/chatmsg` 与 `internal/story` 正式代码路径。

### 0.2 文件规划与架构行数预算（Archtest Budget）对照表
对照 [`internal/archtest/file_sizes_test.go`](file:///Users/stanford/code/vmr/internal/archtest/file_sizes_test.go)（2026-08-16 实测核实无误）：`findings.go` 预算 580 行，当前 547 行（余量 33 行）；`findings_toolresult.go` 预算 320 行，当前 288 行（余量 32 行）；`metrics.go` 预算 470 行，当前 414 行（余量 56 行）；`corpus.go` 预算 380 行，当前 291 行（余量 89 行）；`compare.go` 预算 850 行，当前 771 行（余量 79 行）；`render_corpus.go` 预算 **150 行，当前 118 行，余量仅 32 行**——这是本表原方案完全没有覆盖、但 P1a.4/P1a.5 必然会撞上的一个文件（见下方 `[核实修订]`）。`entities.go` 未登记在预算表里，走默认 700 行上限（当前仅 39 行），确认无风险。

| 任务编号 | 任务名称 | 改动文件路径 | 是否新建文件 | 预估代码行数 | 关联测试文件 |
|---|---|---|:---:|:---:|---|
| **P1a.1** | 实体提取重构与边界清洗 | [`internal/chatmsg/entities.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities.go) | 否（原地改） | +45 / -10 行 | [`internal/chatmsg/entities_test.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities_test.go) |
| **P1a.2** | 重复工具调用参数 JSON 规范化 | [`internal/story/toolcall_normalize.go`](file:///Users/stanford/code/vmr/internal/story/toolcall_normalize.go) + `metrics.go`（1 行） | **是**（+1 处原地改） | ~60 行 | `internal/story/toolcall_normalize_test.go` |
| **P1a.3** | Shell 验证意图候选提取与 OpenAI 错误调查 | [`internal/story/verification_intent.go`](file:///Users/stanford/code/vmr/internal/story/verification_intent.go) | **是** | ~80 行 | `internal/story/verification_intent_test.go` |
| **P1a.4** | 语料级 Context Rot 质量拐点分析 | [`internal/story/corpus_contextrot.go`](file:///Users/stanford/code/vmr/internal/story/corpus_contextrot.go)（含渲染函数）+ `corpus.go`（接线）+ `render_corpus.go`（2 行）+ `i18n/story_corpus.go`（新增标题字段） | **是** | ~90 行 | `internal/story/corpus_contextrot_test.go` |
| **P1a.5** | 工具调用 N-gram 序列模式挖掘 | [`internal/story/corpus_sequence.go`](file:///Users/stanford/code/vmr/internal/story/corpus_sequence.go)（含渲染函数）+ `corpus.go`（接线）+ `render_corpus.go`（2 行）+ `i18n/story_corpus.go`（新增标题字段） | **是** | ~95 行 | `internal/story/corpus_sequence_test.go` |
| **P1a.6** | Token 文本重复率代理指标 | [`internal/story/verbosity.go`](file:///Users/stanford/code/vmr/internal/story/verbosity.go) + `metrics.go`（新字段+接线）+ `compare.go`（`metricSpecs` 新条目） | **是**（+2 处原地改） | ~70 行 | `internal/story/verbosity_test.go` |
| **P1a.7** | 计划格式解析扩展（Checklist / Step N） | [`internal/story/plan_parse.go`](file:///Users/stanford/code/vmr/internal/story/plan_parse.go) | **是** | ~85 行 | `internal/story/plan_parse_test.go` |

> [!NOTE]
> **关于老文件减肥的协同收益**：P1a.7 将原本堆叠在 [`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go) 中的 `lastNumberedList` 与 `numberedListRe`（约 40 行）抽取并下沉至新文件 [`plan_parse.go`](file:///Users/stanford/code/vmr/internal/story/plan_parse.go)，将使 `findings.go` 从 547 行下降至约 505 行，释放出 75 行的宝贵预算余量。

> [!IMPORTANT]
> **[核实修订] 原表遗漏三类文件依赖，均已实测确认，编码前必须一并规划**：
> 1. **`render_corpus.go`（预算 150 行，当前 118 行，只剩 32 行余量）** 是 `CorpusStats` 唯一的 Markdown 渲染出口——`RenderCorpusMarkdown` 里每一节（指标分布、Finding 命中率、相关性、分组对比）都在这一个函数体内。P1a.4 的 Context Rot 分桶表和 P1a.5 的工具序列表如果照抄现有节的写法直接堆进这个函数，两节加起来至少 30-40 行，会当场压过预算。**解决办法**：把每节真正的表格渲染逻辑写成 `corpus_contextrot.go`/`corpus_sequence.go` 里的导出函数（如 `renderContextRotSection(b *strings.Builder, stats CorpusStats, lang i18n.Lang)`），`render_corpus.go` 里只留一行调用——这样两个新文件的预算（默认 700 行上限，目前空文件）吸收了实际的渲染代码量，`render_corpus.go` 只增加 2×1 行调用。
> 2. **`internal/i18n/story_corpus.go`（`CorpusText` 结构体，当前 92 行）** 是 `vmr story -corpus` 每一节标题/表头文案的唯一来源（EN/ZH 各一份），CLAUDE.md module map 里"i18n 与其消费方一一对应"这条约定同样适用于 `story` 半区。新增两节输出，必须在这里补齐对应的中英文标题/表头字段——原方案完全没有提到这个文件，必须补上。
> 3. **`internal/story/compare.go` 的 `metricSpecs`（当前 771/850 行）** 是"行为剖面指标"的唯一权威登记表——不只是 `-compare` 的逐指标 diff 用它，`corpus.go` 的分布统计、相关性矩阵、`render_corpus.go` 的指标分布表也都遍历这同一个 slice（`corpus.go` 的包注释原话："corpus.go used to hand-maintain its own copy... kept in sync with Compare only by a comment"，这正是当年要统一到 `metricSpecs` 的原因）。P1a.6 如果想让新指标 `OutputRepetitionRate` 和 `DuplicateActionRate` 一样出现在语料分布、相关性分析、`-compare` 对比里，必须在这里补一条 `MetricCode` + `metricSpec` 条目，否则这个指标算出来了但在任何报告里都不可见。详见 P1a.6 小节的 `[核实修订]`。
>
> P1a.4/P1a.5 不需要碰 `compare.go`——`ContextRotBucket`/`ToolSequencePattern` 是语料级聚合结构，不是逐 Journey 可比较的标量指标，不属于 `metricSpecs` 的范畴。

---

## 1. 逐任务详细实施规划与评估

---

### 任务 P1a.1：实体提取重构与边界清洗 (Entity Extraction Refactor)

#### 1. 任务背景与核心目标
现有 [`internal/chatmsg/entities.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities.go) 使用的正则表达式 `https?://[^\s"'` + "`" + `)]+|\b[\w][\w./\-]*\.[a-zA-Z]{1,6}\b` 仅识别 URL 和带 1~6 位后缀的文件名。导致无后缀系统文件（`/etc/hosts`、`Dockerfile`）、代码目录（`internal/story/`）、代码函数名（`ExtractEntities`）、CLI 命令（`git commit`）被系统性漏报，连带导致 `story` 侧 5 个 Finding 检测器及 `report` 侧 Compaction 吞噬检测失真。
本任务目标是重构实体提取规则，支持标准路径、目录、代码标识符与 CLI 常用命令，并剔除尾部标点干扰。

#### 2. 关联源码与调用链分析
- **核心定义点**：[`internal/chatmsg/entities.go`](file:///Users/stanford/code/vmr/internal/chatmsg/entities.go) 中的 `ExtractEntities(text string) []string`（`internal/story` 侧统一通过 `journey.go:459` 的 `extractEntities` 薄包装调用，不直接调 `chatmsg.ExtractEntities`）。
- **直接依赖点（跨半区辐射，行号已核实修正）**：
  1. [`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L356)：`detectReasoningActionMismatch`（L346，推理与动作实体比对，L356/L365 两处调用）、`detectPlanExecutionMisalignment`（L450，计划条目实体比对，L488/L518 两处调用）。
  2. [`internal/story/findings_toolresult.go`](file:///Users/stanford/code/vmr/internal/story/findings_toolresult.go#L182)：`detectUnusedToolResult`（**L182**，非原方案标注的 L187）、`detectUnverifiedEntityReference`（**L228**）——两者是不同函数，不共享同一行号。
  3. [`internal/story/journey.go`](file:///Users/stanford/code/vmr/internal/story/journey.go#L466)：`buildCompactionInfo`（**L466**，非原方案标注的 L497；调用 `extractEntities` 在 L497 附近）。
  4. [`internal/report/recextract.go`](file:///Users/stanford/code/vmr/internal/report/recextract.go#L223)：`buildCompactions`（**L223**，内部两处 `chatmsg.ExtractEntities` 调用在 **L233、L236**，与原方案标注一致）。

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

> [!WARNING]
> **[核实修订] 两条候选正则本身在真实语料上大概率产生系统性误报，必须先收紧再实现**：
> 1. **CLI 子命令模式 `(?:git|go|npm|...)\s+[a-z][a-z0-9\-]+`** 会把普通英文口语吃进去——`go` 本身是最常见的英语动词之一，Agent 的 Reasoning 文本里"go read the file"、"go check the logs"、"go ahead and"、"let's go through"这类短语在真实语料里出现频率极高（尤其是 `go` 这个虚拟模型名本身在 CLAUDE.md 里就出现），每一处都会被误判成 `go read`/`go check`/`go ahead` 这样的"CLI 命令实体"。这不是边角案例，是这条规则的主干命中路径。**必须收紧为已知子命令白名单**，如 `go\s+(test|build|vet|run|mod|get|fmt|vendor)\b`、`git\s+(commit|status|diff|log|push|pull|checkout|branch)\b`，而不是"关键字 + 任意小写单词"。
> 2. **目录路径正则 `[\w.\-]+\/[\w.\-\/]*`**（不带前导 `/`/`./`/`~/` 的相对目录形式）会命中大量与路径无关的英文/数字斜杠结构——`and/or`、`yes/no`、`km/h`、`1/2`、`他/她` 这类常见斜杠短语和分数表达全部满足 `\w+/\w+` 的最简形式。**必须加约束**：至少两段、每段 ≥2 字符，且不在英文停用词表（`and`, `or`, `yes`, `no`, `either` 等）里，或者干脆放弃"无前导符号的裸相对目录"这一条、只保留 `(?:\/|\.\/|~\/)` 前缀形式——后者覆盖了绝大多数真实系统路径场景（`/etc/hosts`、`cmd/vmr` 除外，但 `cmd/vmr` 这类项目内相对路径可以单独用"已知目录前缀白名单"或结合上下文兜底，不值得为了这一类路径引入一个高误报的通用规则）。
> 3. **这两条的误报会被现有的 `entityReferenced`（`internal/story/findings.go:337-344`）substring-tolerant 匹配放大**：该函数对推理侧实体和动作侧实体做 `strings.Contains` 双向包含判断，短小/通用的误报实体（如提取出的裸词 `go`、双字符路径片段）会更容易与不相关的字符串产生子串命中，进而影响 `reasoning_action_mismatch`/`plan_execution_misalignment` 的判定，不只是単纯"多了几个无意义实体"这么轻。**②中提出的长度下限（≥4 字符）必须同时套用在路径/CLI 匹配上，不能只套用在驼峰标识符上**——原方案的措辞只提到"严格限制代码标识符的长度"，需要扩大到全部新增匹配类别。

#### 6. 独立验证与验收标准
- **单测验收**：`entities_test.go` 覆盖：无后缀系统路径（`/etc/hosts`）、目录（`internal/story/`）、驼峰符号（`ExtractEntities`）、CLI 命令（`go test`）、URL 尾部标点剥离（`https://example.com/docs,` -> `https://example.com/docs`）；**新增反例覆盖**：`go read the file`/`let's go through`（不应提取出实体）、`and/or`/`yes/no`（不应提取出实体）。
- **回归验收**：`go test ./internal/chatmsg/... ./internal/story/... ./internal/report/...` 全部 PASS。
- **聚合报告验收**：确认 `recextract.go` 的 `buildCompactions`（两处调用点：`internal/report/recextract.go:233,236`）在测试用例中 `SwallowedEntities` 输出稳定，且用一份真实 `logs/` 语料跑一遍 `vmr report`，人工抽查 Compaction 章节没有出现明显的"go read"式误报实体。

---

### 任务 P1a.2：重复工具调用参数 JSON 规范化 (JSON Canonicalization)

#### 1. 任务背景与核心目标
现有 [`internal/story/findings.go`](file:///Users/stanford/code/vmr/internal/story/findings.go#L129) 和 [`internal/story/metrics.go`](file:///Users/stanford/code/vmr/internal/story/metrics.go#L208) 中的 `toolCallKey` 仅对 `tc.Args` 进行原生字符串拼接（`tc.Name + "\x00" + tc.Args`）。当 Agent 多次调用同一工具，但 JSON 字段顺序不同（例如 `{"path":"a.go","line":10}` vs `{"line":10,"path":"a.go"}`）或存在多余空格/换行时，系统会判定为不同调用，导致死循环与重复调用漏检。
本任务目标是在比对前对参数执行轻量 JSON 规范化。

#### 2. 关联源码与调用链分析
- **唯一定义点**：[`internal/story/metrics.go:208`](file:///Users/stanford/code/vmr/internal/story/metrics.go#L208)：`func toolCallKey(tc chatmsg.ToolCall) string { return tc.Name + "\x00" + tc.Args }`。
- **调用点**：[`internal/story/findings.go:129`](file:///Users/stanford/code/vmr/internal/story/findings.go#L129) 的 `groupToolCallsByKey(steps []*Step)` 只是**调用**这个共享函数（`key := toolCallKey(tc)`），并没有重新定义一份自己的 `toolCallKey`。

> [!WARNING]
> **[核实修订] `toolCallKey` 只有 metrics.go 一处定义，findings.go 不需要改动**。原方案第 3 节"接入点"写的是"修改 `metrics.go` 和 `findings.go` 中的 `toolCallKey`"，第 4 节也把 `findings.go` 列为"修改 2 行"——这是对调用链的误读。实际改动只有一行：`metrics.go:208` 把 `tc.Args` 换成 `canonicalizeToolArgs(tc.Args)`。`findings.go` 完全不需要碰，因为它调用的就是这同一个函数，函数体一改，`groupToolCallsByKey` 自动拿到规范化后的 key。

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
- **接入点**：只修改 `metrics.go` 里 `toolCallKey` 的唯一定义（`findings.go` 不改，见上方核实修订）：
  ```go
  func toolCallKey(tc chatmsg.ToolCall) string {
      return tc.Name + "\x00" + canonicalizeToolArgs(tc.Args)
  }
  ```

#### 4. 修改规模估算与成本
- **代码修改**：新文件 `toolcall_normalize.go`（约 60 行），老文件 `metrics.go`（**仅 1 行**：`toolCallKey` 函数体里 `tc.Args` → `canonicalizeToolArgs(tc.Args)`）；`findings.go` **不需要改动**（见上方核实修订）。
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

> [!NOTE]
> **[核实修订] `logs/` 目录下的真实语料大多是 zstd 压缩的（`vmr-audit-YYYY-MM-DD.jsonl.zst`，实测确认 `logs/` 下 2026-07-14~27 的文件全部是 `.zst`，只有 08-05 一份是明文 `.jsonl`）**。调查脚本不要手搓 zstd 解压逻辑，直接复用 `internal/audit`（`housekeep.go` 的 `auditFileRE` 已经原生识别 `vmr-audit-(\d{4}-\d{2}-\d{2})\.jsonl(\.zst)?` 两种后缀）或者复用 `cmd/vmr/cmd_report.go`/`cmd_replay.go` 已经在用的"混合读取 .jsonl 和 .jsonl.zst"路径——这两个 CLI 命令的输入层本来就原生支持这一点，调查脚本没有理由重新发明。

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
  1. 遍历语料中所有 Journey 的所有 Task 的所有 Step（`for _, t := range j.Tasks { for _, s := range t.Steps { ... } }`——`Step` 挂在 `Task.Steps` 下，不是 `Journey` 直接持有；`corpus.go` 当前的 `ComputeCorpusStats` 只按 Journey 聚合，没有现成的展平辅助函数，需要新写这个双层循环，代码库里 `findings.go`/`metrics.go`/`render_md.go` 等文件都各自手写同样的双层循环，是既有写法，不算异常），提取输入 Token 量。
  2. 归入预定义分桶（`0-32k`, `32-64k`, `64-128k`, `128-256k`, `256k+`）。
  3. 聚合各桶内的 Step 数、Finding 触发数与工具报错数，计算 Finding 密度与错误率。
  4. 在 `ComputeCorpusStats` 中调用 `computeContextRot(journeys)`，挂载至 `CorpusStats.ContextRot`。

> [!WARNING]
> **[核实修订] 三处需要先定案的实现细节，原方案的表述会导致编译不过或指标含义模糊**：
> 1. **不存在 `Step.TokensIn` 字段**。`Step` 结构体（`internal/story/journey.go:63-114`）里没有任何 Token 相关字段；Token 量唯一的可靠来源是 `Step.Manifest.Usage.In`（`ctxgraph.Manifest.Usage` 是 `chatmsg.Usage{In, Out, CacheRead, CacheWrite, Reasoning int64}`）——这一点 `buildCompactionInfo` 已经在用（`predManifest.Usage.In`/`curManifest.Usage.In`，`journey.go:469,472`）。原方案"`TokensIn` 或 `Manifest.Usage.In`"的措辞把一个不存在的字段和真实字段并列成"二选一"，容易误导实现者去找一个不存在的字段——**唯一正确写法是 `s.Manifest.Usage.In`**。
> 2. **"工具报错数"没有现成的单点判定函数**，需要仿照 `metrics.go:267-281` 的 `errorRecoveryCount` 写法（扫描 `s.NewEvents` 里是否有 `isErrorMarker`——`"❌ is_error"` 这个字面量），自己写一个类似的按 Step 判定"这一步是否命中错误标记"的辅助函数。**这个判定继承 `errorRecoveryCount` 已知的局限**：`isErrorMarker` 只识别 Anthropic 协议原生的 `is_error` 标记，OpenAI 语料上这个错误率会系统性偏低估——这正是 P1a.3 调查子任务要处理的同一个协议缺口。Context Rot 的"错误率"字段上线时必须在文档/字段注释里注明这个已知偏差，不能读起来像一个协议中立的准确统计；如果 P1a.3 的调查后续找到了可靠的 OpenAI 结构化信号，`errorRecoveryCount` 和这里的按桶错误率应该**同步**更新，不要只改一处。
> 3. **`CorpusStats.ContextRot` 字段挂载后，`render_corpus.go`（150 行预算，仅 32 行余量）不能直接塞入渲染逻辑**——见 0.2 节的核实修订。具体到这个任务：`corpus_contextrot.go` 里除了 `computeContextRot` 之外，还要写一个 `renderContextRotSection(...) string`（或直接写入 `*strings.Builder`）的渲染函数，`render_corpus.go` 的 `RenderCorpusMarkdown` 只加一行调用；同时 `internal/i18n/story_corpus.go` 的 `CorpusText` 结构体要新增这一节的中英文标题/表头字段。

#### 4. 修改规模估算与成本
- **代码修改**：新建 `corpus_contextrot.go`（约 90-110 行，含计算 + 渲染两部分），`corpus.go`（新增字段与调用，约 6 行），`render_corpus.go`（+2 行调用），`internal/i18n/story_corpus.go`（新增本节标题/表头字段，约 6-8 行 ×2 语言）
- **测试修改**：新建 `corpus_contextrot_test.go`（约 80 行）
- **文档修改**：无
- **总体成本**：低（比原估算多了渲染 + i18n 部分，但仍在"新文件预算宽裕"范围内）

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**极低**。纯只读内存聚合，复用已解析的 `Step` 与 `Finding` 数据，对原有统计字段完全向后兼容。

#### 6. 独立验证与验收标准
- **单测验收**：构造具有不同 Context 长度的 Step 样本，验证分桶边界、密度计算与空桶安全处理。
- **CLI/JSON 验收**：运行 `vmr story -corpus`，确认输出 JSON 中包含 `context_rot` 数组且数据自洽，**且 `vmr-story-corpus.md` 里能看到对应的 Markdown 小节**（不只是 JSON——原方案的验收标准漏掉了 Markdown 渲染这一半）。

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
- **代码修改**：新建 `corpus_sequence.go`（约 95-115 行，含计算 + 渲染两部分），`corpus.go`（修改 5 行），`render_corpus.go`（+2 行调用），`internal/i18n/story_corpus.go`（新增本节标题/表头字段）
- **测试修改**：新建 `corpus_sequence_test.go`（约 85 行）
- **文档修改**：无
- **总体成本**：低

> [!NOTE]
> **[核实修订]** 同 P1a.4：`render_corpus.go`（150 行预算，仅 32 行余量）不能直接承载这一节的渲染代码，渲染函数同样应该写在 `corpus_sequence.go` 里，`render_corpus.go` 只加一行调用；`internal/i18n/story_corpus.go` 需要同步补充中英文标题字段。"序列末步是否关联 Finding/Error"的判定同样依赖 P1a.4 里提到的按 Step 错误判定辅助函数——如果 P1a.4 先落地，这里直接复用同一个辅助函数，不要各写一份。

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**极低**。纯只读统计，零侵入性。

#### 6. 独立验证与验收标准
- **单测验收**：构造包含特定重复序列（如 A->B->C）的虚拟 Task，验证 2-gram / 3-gram 的计数准确性及跨 Task 隔离性。
- **集成验收**：`go test ./internal/story/...` 全部通过；`vmr-story-corpus.md` 里能看到对应的工具序列 Markdown 小节。

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
- **代码修改**：新建 `verbosity.go`（约 70 行）；如按下方核实修订接入行为剖面体系，还需 `metrics.go`（新增字段 + `ComputeMetrics` 里一行调用，约 4 行）与 `compare.go`（新增 `MetricCode` 常量 + `metricSpecs` 一条目，约 3 行）
- **测试修改**：新建 `verbosity_test.go`（约 60 行）
- **文档修改**：无
- **总体成本**：极低

> [!IMPORTANT]
> **[核实修订] 原方案的函数签名 `ComputeOutputRepetitionRate(j *Journey) float64` 只是个孤立函数，不会出现在任何报告里——必须先决定它要不要接入既有的"行为剖面"指标体系**。核实 `internal/story/compare.go:94-125`：`metricSpecs` 是"行为剖面指标"的唯一权威登记表，`corpus.go` 的分布统计/相关性矩阵、`render_corpus.go` 的指标分布表、`vmr story -compare` 的逐指标 diff，全部遍历这同一个 slice——`corpus.go` 包注释原话就是在讲当年"corpus.go 曾经自己维护一份重复的指标清单，后来统一收敛到 `metricSpecs`"这段历史，为的正是不让新指标游离在外。
> 原方案第 2 节把 `metrics.go` 列为"关联"，但第 4 节的成本估算完全没有提到要改 `metrics.go`（加字段）或 `compare.go`（登记 `metricSpecs`），如果只落地一个 `verbosity.go` 里的孤立函数，这个指标算出来后：不会出现在 `vmr-story-corpus.json` 的 `metric_distributions`/`correlations` 里，不会出现在 `-compare` 的逐指标对比表里，只能被某个未来的调用方手动调用——这就不是"和 `DuplicateActionRate` 同级别的第 14 项行为指标"，而是一个孤立的死代码。
> **两个可选方案，二选一，必须在编码前明确写下来**：
> - **方案 A（推荐）**：接入 `metricSpecs`。`metrics.go` 的 `Metrics` 结构体加 `OutputRepetitionRate float64`，`ComputeMetrics` 里加一行 `m.OutputRepetitionRate = ComputeOutputRepetitionRate(j)`；`compare.go` 加 `MetricOutputRepetitionRate MetricCode = "output_repetition_rate"` 常量和对应的 `metricSpec` 条目。这样它自动获得分布统计、相关性分析、`-compare` diff、Markdown 渲染——不需要在 `render_corpus.go`/`i18n` 侧再单独写代码，因为这些都是遍历 `metricSpecs` 通用生成的。
> - **方案 B**：明确定为不接入行为剖面体系的独立代理指标（例如只在单 Journey 的 `.md` 报告里作为一行文字展示，不参与语料级横向比较）——如果选这个方案，要在文档里写清楚"为什么不像 `DuplicateActionRate` 一样处理"，否则下一个读者会理所当然地以为它也在 `metricSpecs` 里。

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**极低**。只计算统计数值，无业务破坏性。若采用方案 A，`compare.go`/`corpus.go` 余量分别为 79/89 行，登记一条 `metricSpec` 不构成预算风险。

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
  func ExtractActionablePlan(text string) []PlanItem
  ```
  （原方案这里把函数签名写成 `[]string`，与紧邻定义的 `PlanItem` 结构体自相矛盾——见下方核实修订，已改为 `[]PlanItem`。）
- **对接重构**：
  在 `findings.go` 中，将原 `lastNumberedList(planText)` 替换为调用 `ExtractActionablePlan(planText)`，彻底删除 `findings.go` 中的 `numberedListRe` 及 `lastNumberedList` 实现；`detectPlanExecutionMisalignment` 现有逻辑只用到条目的文本（`item` 是字符串，见 `findings.go:518` 的 `extractEntities(item)`），改用 `PlanItem.Text` 即可，`Kind`/`Index` 暂时不消费，但保留在返回值里是为 Phase 1b"动态重规划识别"预留（Plan v2 的判定需要知道条目属于哪个格式、原始序号是多少）。

> [!WARNING]
> **[核实修订] 两处需要在编码前定案的算法细节，原方案的"支持这些格式"式描述不足以指导实现**：
> 1. **函数签名与 `PlanItem` 结构体自相矛盾**：原方案定义了带 `Kind`/`Index` 的 `PlanItem`，却让函数返回裸 `[]string`——等于白定义了这个结构体。已改为 `[]PlanItem`（见上）。
> 2. **"取最后一段连续计划"这条核心语义，在混入 Checklist / Step N: 之后如何定义"连续"，原方案完全没有说清楚**。现有 `lastNumberedList`（`findings.go:395-430`）的算法是靠"编号是否递增"判断一个列表有没有断掉、开始下一个——这个判据是针对纯数字列表校准过的（见 `findings.go:395-406` 的注释，引用了 `logs/vmr-audit-2026-07-27/28` 真实语料）。Checklist 条目（`- [ ]`）没有编号可比较"是否递增"；`Step N:`/`Phase N:` 有编号但格式和数字列表不同；`阶段一：`/`步骤一：` 用的是中文数字（一二三四五...），需要一张汉字数字到整数的映射表才能判断"是否递增"，这是原方案完全没有提到的实现工作量。
>    **需要在编码前决定的具体算法**（建议方案）：(a) 每种格式各自独立按"原格式内部编号/出现顺序是否连续"规则扫描出各自的候选列表，复用 `lastNumberedList` 已经验证过的"取最后一个连续段"逻辑；(b) 如果同一段文本里出现了不止一种格式的候选列表（如前半段是数字列表、后半段是 Checklist），选择**结尾位置最靠后**（即在原文里最后出现）的那个候选列表作为最终返回值——这是对现有"最后一段生效"语义最直接的跨格式泛化，不引入新的裁决逻辑。(c) 中文数字前缀（`阶段一`/`步骤二`）的连续性判断，需要一张 一/二/三/四/五/六/七/八/九/十 到 1-10 的定长映射表（不需要支持任意大的中文数字，长任务的计划条目数很少超过 `maxPlanItems = 8`）。

#### 4. 修改规模估算与成本
- **代码修改**：新建 `plan_parse.go`（约 85-100 行，比原估算略增——中文数字映射表和跨格式"取最后一段"逻辑比纯正则扩展多一些代码），`findings.go`（**删除约 40 行**，净缩减行数）
- **测试修改**：新建 `plan_parse_test.go`（约 90 行，需覆盖上方核实修订指出的跨格式边界场景）
- **文档修改**：无
- **总体成本**：极低且附带行数瘦身红利

#### 5. 风险评估与爆炸半径
- **破坏性风险**：**低**。扩展格式后可能捕获更多真实计划列表，需确保多段非计划性列表（如纯引用文本中的 checklist）不会被错误捕获。
- **防御机制**：保持 `minPlanItems = 2` 与 `maxPlanItems = 8` 的安全过滤区间不变。

#### 6. 独立验证与验收标准
- **单测验收**：覆盖 Markdown Checklist、Step 英文前缀、中文"步骤一"前缀、混合文本中仅取最后一段连续计划等典型场景；**新增**：同一段文本里数字列表和 Checklist 都出现时，取结尾位置更靠后的那一段（核实修订 (b) 提出的跨格式判据）；中文数字前缀连续性判断（"阶段一"、"阶段二"应被识别为连续，"阶段一"单独出现、后接不相关文本不应被误判为多条目计划）。
- **检测器验收**：`findings_test.go` 针对 `plan_execution_misalignment` 的测试用例保持全绿。

---

## 2. 全盘回顾与跨任务一致性核对 (Sanity Check)

> [!IMPORTANT]
> **[核实修订] 本节原有的行数预算表基于两处不成立的假设重新核算如下**：(a) P1a.2 不改 `findings.go`；(b) P1a.4/P1a.5/P1a.6 会分别触碰 `render_corpus.go`、`internal/i18n/story_corpus.go`，P1a.6 若采纳方案 A 还会触碰 `compare.go`。原表遗漏了这三个文件，读者据此会误以为 Phase 1a 的落点只有 7 个新文件 + 3 个老文件原地小改，实际是 7 个新文件 + 最多 6 个老文件小改。

在制定完上述 7 个任务后，进行全局视角的交叉复盘（数字已按上述修订重新核算）：

1. **架构行数预算核对**：
   - `internal/chatmsg/entities.go`：预计改动后约 75 行（未在 `fileLineExemptions` 登记，远低于 700 默认上限）。
   - `internal/story/findings.go`：因下沉 P1a.7，行数预计从 547 行**降至约 505 行**（预算 580 行，余量扩大至 75 行，大幅提升安全性）；P1a.2 不改这个文件（见其小节核实修订）。
   - `internal/story/corpus.go`：因 P1a.4 和 P1a.5 均采用新建文件（`corpus_contextrot.go`, `corpus_sequence.go`）承载计算逻辑，`corpus.go` 本身仅增约 10 行接线代码，总行数约 300 行（预算 380 行，保持 80 行安全余量）。
   - `internal/story/metrics.go`：P1a.2 改 1 行（`toolCallKey`）；P1a.6 若采纳方案 A 再加约 4 行（新字段 + 一行调用）；合计约 419 行（预算 470 行，余量约 51 行）。
   - `internal/story/compare.go`：P1a.6 若采纳方案 A，新增 1 个 `MetricCode` 常量 + 1 条 `metricSpec`，约 771→774 行（预算 850 行，余量约 76 行）。
   - `internal/story/render_corpus.go`：P1a.4、P1a.5 各加约 2 行调用（渲染主体下沉到对应的 `corpus_contextrot.go`/`corpus_sequence.go`），约 118→122 行（预算 150 行，余量约 28 行）——**这是本节原表完全遗漏、且预算最紧张的一个文件，必须严格遵守"渲染主体不落在这个文件里"的约束，否则会当场触发 archtest**。
   - `internal/i18n/story_corpus.go`：P1a.4、P1a.5 各新增中英文标题/表头字段，约 92→110 行左右——该文件未登记在 `fileLineExemptions`，走默认 700 行上限，无预算风险，但内容本身必须补齐（原表完全没提到这个文件）。
2. **依赖与接口一致性**：
   - P1a.2 的 `canonicalizeToolArgs` 位于 `internal/story` 包内，被 `metrics.go` 的 `toolCallKey` 唯一定义点调用，`findings.go` 通过 `toolCallKey` 间接受益，不需要自己改动，无循环依赖。
   - P1a.1 的改动位于底层 `internal/chatmsg`，被 `story`（经 `journey.go:459` 的 `extractEntities` 包装）和 `report`（`recextract.go` 直接调用 `chatmsg.ExtractEntities`）共同依赖，符合架构分层。
   - P1a.4/P1a.5/P1a.6 三者都以"计算逻辑所在的新文件同时提供渲染/接线代码，老文件只留一行调用"为统一模式，这是为了不撞上 `render_corpus.go`/`metrics.go`/`compare.go` 的预算余量，不是三个任务各自独立想到的巧合，编码时应按这个统一模式执行。
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

> [!WARNING]
> **[核实修订] 原方案的 `-audit` 参数不存在**。核实 `cmd/vmr/cmd_story.go`/`cmd_report.go` 的 `flag.FlagSet` 定义：两个子命令都没有注册过 `-audit` 这个 flag，输入文件是**位置参数**（glob pattern），由 `cmd/vmr/auditpaths.go` 的 `resolveInputPaths` 处理——不传位置参数时才会退回读取 `config.yaml` 的 `log_dir`。下面已改成实际可执行的调用形式（`logs/` 下真实文件名前缀是 `vmr-audit-YYYY-MM-DD.jsonl[.zst]`，`resolveInputPaths` 对 `.zst` 透明处理，不需要单独解压）：

使用现有测试语料库执行真实命令，验证无 Panic 与数据漂移：
```bash
# 验证 story 渲染（-render-all 批量渲染每条非截断候选 Journey；具体 ID 需先用不带 -render-all
# 的一次运行列出候选后，再用 -journey <id> 单独验证某条，见 cmd_story.go 的 -journey 用法说明）
go run ./cmd/vmr story -render-all logs/vmr-audit-*

# 验证 story 语料级聚合与新字段输出
go run ./cmd/vmr story -corpus logs/vmr-audit-*

# 验证 report 聚合不受实体提取重构破坏
go run ./cmd/vmr report logs/vmr-audit-*
```

### 3.3 文档与治理记录更新
1. **决策记录**：将 P1a.3 的 OpenAI 结构化错误字段调查结论写入 `docs/KNOWN_ISSUES_sonnet-5.md`，明确记录维持或调整该项的理由。
2. **临时文件清理**：清理执行过程中的所有临时分析脚本及 `_tmp/` 调试文件。
3. **Git 提交原子性**：按照任务单元独立提交 Git Commit，Commit 消息严格遵循英文规范（如 `feat(chatmsg): refactor entity extraction to support paths and symbols`）。
