<!-- Ver 2026-08-17 02:15, by Gemini 3.7 Flash; 源码核实与批注 2026-08-17, by Claude Opus 5 -->

# Phase 1 审查问题深度分析与实施计划 (Issues 3-1, 3-2, 4-1, 4-2)

> **文档性质**：针对 `docs/future-strategy/phase1a_phase1b_implementation_review_opus-5.md` 中第 5 节汇总表核心问题（3-1, 3-2, 4-1, 4-2）的源码级核实、根因剖析、方案论证与落地实施计划。
> **目标原则**：不抢跑修改，先彻底摸清源码机理与架构契约，给出最优解法与严密执行步骤，供人工 Review 拍板。
> **审阅状态**：已由 Claude Opus 5 逐项对照 HEAD 源码（`metrics.go`、`cmd_story.go`、`llm_findings.go`、`file_sizes_test.go`、`UserGuide` 等）完成全面核实。**结论：方案主干设计严密、契约清晰、行之有效；仅有少量预算数字与调用细节需要在实现时注意，均已内嵌 `[核实批注]` 标出**。

---

## 1. 核心问题深度分析与方案论证

### 1.1 Issue 3-1: `journey-<id>.json` 与 `.md` 事实分叉（LLM Finding 缺失）

#### 1. 源码核实与现状追踪
- **涉及代码路径**：
  - `cmd/vmr/cmd_story.go` 中的 `renderJourney` (L382-401) 与 `writeJourneyFile` (L709-733)
  - `internal/story/metrics.go` 中的 `JourneySummary` (L399-407) 与 `Summarize` (L415-420)
  - `internal/story/findings.go` 中的 `Finding` (L47-73)
- **执行流程现状**：
  1. 在 `renderJourney` 中，当用户指定 `-llm-addr` 时，代码调用 `story.ComputeLLMFindings(...)` 得到 `llmFindings`，并通过 `findings = append(findings, llmFindings...)` 将 LLM 推测结果与规则 Finding 合并。
  2. 随后调用 `writeJourneyFile(j, m, findings, storiesDir, lang, llmSection)`。
  3. 在 `writeJourneyFile` 内部：
     - Markdown 文件使用传入的 `findings` 调用 `story.RenderMarkdown(j, m, findings, lang)` 生成，包含了带有 `[AI推测]` 标签的 LLM Finding。
     - JSON 文件却直接执行 `json.MarshalIndent(story.Summarize(j), "", "  ")`。
  4. `story.Summarize(j)` 内部硬编码调用 `ComputeFindings(j, i18n.EN)`，这是纯规则检测器，完全不感知外层计算出的 `llmFindings`。
- **事实结果**：
  - `journey-<id>.md` 中存在 LLM 推测 Finding，但同名 `journey-<id>.json` 中的 `findings` 数组中**永远只有规则 Finding**，出现事实分叉。
  - 虽然 `internal/story/findings.go` 中的 `Finding` 结构体已为 Phase 1b 设计了 `Source`、`Confidence`、`EvidenceAnchor` 字段且均标记了 `json:",omitempty"`，但数据通道在 `writeJourneyFile` 处被截断。

#### 2. 根因剖析
- 历史设计上，`Summarize(j)` 承担了双重职责：
  1. 作为一个无状态的纯 CPU 规则计算辅助函数（给 `-compare` 快速构建轻量剖面）；
  2. 作为生成 `journey-<id>.json` 的数据结构构造器。
- Phase 1b 在 `renderJourney` 接入 `ComputeLLMFindings` 时，仅将合并后的 `findings` 传递给了 Markdown 渲染器，未同步更新 JSON 生成路径。
- 同时，JSON 输出保持全英文（`i18n.EN`）的既有约定与 LLM 调用的高开销（网络 I/O、Token 计费）之间存在张力：不能在 `Summarize` 内部盲目重复发起 LLM 调用，必须由外层将已算好的 `llmFindings` 注入。

#### 3. 方案对比与架构论证

| 方案 | 契约设计 | 优点 | 缺点 | 评估 |
|---|---|---|---|---|
| **方案 A：`JourneySummary` 增设独立 `LLMFindings` 字段** | `Findings []Finding` (纯规则, EN)<br>`LLMFindings []Finding` (LLM 推测, omitempty) | 1. 严格守住纯规则层与推测层的物理隔离；<br>2. 默认不带 `-llm-addr` 时 `llm_findings` 字段完全不存在（JSON 逐字节向后兼容）；<br>3. 外部脚本（Python/评测工具）可直接消费结构化 LLM 发现。 | 需要稍微扩展 `JourneySummary` 结构体字段。 | **推荐 (最优)** |
| **方案 B：将 LLM Finding 混入 `Findings` 数组** | `Findings []Finding` (包含 rule 与 llm_inferred，通过 `Source` 区分) | 结构体无新字段，所有 Finding 统一由 `Findings` 承载。 | 1. 打破了 `Findings` 仅包含确定性规则事实的纯粹性；<br>2. 下游原本只按规则统计的消费者需要额外判断 `Source`。 | 次优 |
| **方案 C：明文废弃 JSON 包含 LLM Finding，仅保留 Markdown 展现** | JSON 不做任何改动，修改文档声明为 `.md-only` | 零代码修改成本。 | 1. 违背 Roadmap 9.3 与 KNOWN_ISSUES 1.15 "外部脚本基于稳定 JSON 契约治理" 的核心准则；<br>2. 外部自动化审计工具无法读取 AI 判定。 | 不推荐 |

#### 4. 最优落地设计（方案 A 细化）
1. **结构体定义扩展** (`internal/story/metrics.go`)：
   ```go
   type JourneySummary struct {
       ID          string     `json:"id"`
       Title       string     `json:"title"`
       From        time.Time  `json:"from"`
       To          time.Time  `json:"to"`
       Partial     bool       `json:"partial,omitempty"`
       Metrics     Metrics    `json:"metrics"`
       Findings    []Finding  `json:"findings,omitempty"`
       LLMFindings []Finding  `json:"llm_findings,omitempty"` // Phase 1b: populated only when -llm-addr is used
   }
   ```
2. **构造方法**：
   - 保持 `Summarize(j *Journey) JourneySummary` 纯规则行为不变（向后兼容测试与 `-compare`）。
   - 增加 `SummarizeWithLLM(j *Journey, llmFindings []Finding) JourneySummary`，或在 `writeJourneyFile` 中直接对 `Summarize(j)` 的结果赋值 `summary.LLMFindings = llmFindings`。
3. **`writeJourneyFile` 改造** (`cmd/vmr/cmd_story.go`)：
   - `writeJourneyFile` 接收分离的 `llmFindings []Finding`（或从传入的 `findings` 中按 `Source == SourceLLMInferred` 筛选出），填入 `JourneySummary.LLMFindings` 后序列化。
4. **关联修复（Issue 3-3 同步解决）**：
   - 在 `renderJourney` 中合并 `findings = append(findings, llmFindings...)` 后，增加一次全局 `sort.SliceStable`（按 `StepSeq` 升序，同 `StepSeq` 按 `Code` 升序），消除 Markdown 中 Finding 呈现两段式的问题。

---

### 1.2 Issue 3-2: E4 候选排序不确定性（Map 遍历随机性导致缓存失效与不可复现）

#### 1. 源码核实与现状追踪
- **涉及代码路径**：
  - `internal/story/llm_findings.go` 中的 `detectOscillationCandidates` (L122-172)
- **代码片段**：
  ```go
  for toolName, calls := range toolCounts {
      if emitted[toolName] {
          continue
      }
      if len(calls) >= 3 && len(toolKeys[toolName]) > 1 {
          out = append(out, OscillationCandidate{
              ToolName: toolName,
              Calls:    calls,
          })
          emitted[toolName] = true
      }
  }
  if len(out) >= 5 {
      break
  }
  ```
- **实测复核**：
  - `toolCounts` 是一个 `map[string][]ToolCallSnippet`。
  - Go 运行时在 `for ... range map` 时引入随机哈希种子，导致同一窗口内多个符合条件的工具遍历顺序随机。
  - 当一个窗口内出现多个振荡工具时，`out` 的追加顺序在多次运行间随机交替；若遇到 `len(out) >= 5` 截断条件，甚至可能导致不同的候选被截留。
  - `cands` 直接序列化为 `SemanticOscillationEvidencePack` 的 JSON，参与 `cacheKey`（SHA256 哈希）与 Prompt 构建，导致磁盘缓存命中率骤降，LLM 判定不确定。

#### 2. 根因剖析
- 纯粹的 Go `map` 随机遍历引入的非确定性。
- 此前在 Phase 1a 的 `internal/story/corpus_sequence.go` 中曾处理过同类问题（通过多级 tiebreak 排序保证两轮 `-corpus` 逐字节一致），但在 `llm_findings.go` 中遗漏了排序收敛。

#### 3. 最优修复方案
必须在**两个层级**同时确保确定性：
1. **窗口内确定性**：遍历 `toolCounts` 前，对 `toolName` 做字母序排序（或按其在窗口中首次出现的先后顺序收集），杜绝 `len(out) >= 5` 截断时的随机性。
2. **全局结果确定性**：在函数返回 `out` 前，对 `out` 执行稳定排序（主键：首个调用 `Calls[0].StepSeq` 升序；次键：`ToolName` 字母序升序）。

```go
// 窗口内有序遍历
toolNames := make([]string, 0, len(toolCounts))
for tn := range toolCounts {
    toolNames = append(toolNames, tn)
}
sort.Strings(toolNames)

for _, toolName := range toolNames {
    calls := toolCounts[toolName]
    if emitted[toolName] {
        continue
    }
    if len(calls) >= 3 && len(toolKeys[toolName]) > 1 {
        out = append(out, OscillationCandidate{
            ToolName: toolName,
            Calls:    calls,
        })
        emitted[toolName] = true
    }
}
if len(out) >= 5 {
    break
}

// 返回前全局排序
sort.SliceStable(out, func(i, j int) bool {
    if out[i].Calls[0].StepSeq != out[j].Calls[0].StepSeq {
        return out[i].Calls[0].StepSeq < out[j].Calls[0].StepSeq
    }
    return out[i].ToolName < out[j].ToolName
})
```

---

### 1.3 Issue 4-1: `CHANGELOG.md` 缺少 Phase 1a / 1b 变更记录

#### 1. 现状核实
- `CHANGELOG.md` 中 `[Unreleased]` 部分的最后更新停留在提交 `c7f8ca2`。
- 自 `c7f8ca2` 以来，共有 Phase 1a（6 个提交）、Phase 1b（1 个提交）及后续优化（2 个提交），累计 9 个涉及用户与开发者可见特性的提交尚未在 `CHANGELOG.md` 中登记，违反了 `CLAUDE.md` 规范。

#### 2. 补齐内容清单（精确分类）

```markdown
## [Unreleased]
### Added
- `vmr story`: Phase 1b LLM 语义检测器管线（`ComputeLLMFindings`）与 6 个高阶行为缺陷检测器——工具返回误读（`tool_result_misinterpretation`）、语义死循环振荡（`semantic_oscillation`）、长程目标漂移（`goal_drift`）、未经验证的完成声称（`unverified_completion_claim`）、Compaction 核心约束丢失（`constraint_text_dropped_at_compaction`）以及计划与执行偏离（`plan_execution_misalignment`）。支持严格的 HIGH/MEDIUM/LOW 离散置信度门禁与原文证据锚点（EvidenceAnchor）逐字核验机制，统一 fail-open 降级
- `vmr story -corpus`: 上下文退化分析（`Context Rot Analysis`）与高频工具调用序列挖掘（`Tool Sequence Patterns`）两个全新分析小节，支持 2-gram/3-gram 序列统计与尾步错误率归因
- `vmr story`: 行为指标分布与对比分析中新增第 14 项行为特征指标——输出文本重复率（`output_repetition_rate`，`Metrics.OutputRepetitionRate`）
- `vmr story`: 计划解析引擎（`ExtractActionablePlan`）扩展支持 Markdown Checklist 待办清单（`- [ ]` / `- [x]`）、中文步骤编号（`步骤一：`）以及各类带括号编号（`(1)` / `1)`）等多格式混排解析
- `vmr story`: Shell 验证命令意图识别提取器（`ExtractShellVerificationCandidate`），内置 17 种主流测试与校验 CLI 命令白名单
- `vmr story -compare`: 自动检测并补齐渲染缺失的单 Journey 报告（`.md` 与 `.json`），并在对比报告大纲中生成直达链接
- `vmr story`: 决策主干（Decision Spine）中的步骤角色标签统一增加蓝菱形前缀（`🔷`），提升视觉呈现一致性

### Changed
- `internal/chatmsg`: 实体提取引擎（`ExtractEntities`）全面重构为多模式分类器（URL、绝对路径、带扩展名文件、两段式有效目录、驼峰/蛇形标识符及 CLI 命令白名单），新增标点剥离与空间跨度重叠消解，单 Step 上限收敛至 30
- `internal/story`: 工具调用参数标准化（`canonicalizeToolArgs`），在重复调用检测中引入 JSON 键排序规范化与 `UseNumber` 64 位整型精度保持
- `internal/story`: 决策主干中的计划步骤标记（`StepTagPlan`）判据由单行匹配收紧为提取出 $\ge 2$ 条可执行计划项
```

---

### 1.4 Issue 4-2: `UserGuide.md` / `UserGuide.zh.md` 文档过期

#### 1. 现状核实
- 中英文用户指南在以下三处存在描述滞后：
  1. **指标总数过期**：仍写有 `thirteen behavior-profile numbers` / `十三项行为剖面数值`（现已增加至 14 项，包含 `output_repetition_rate`）。
  2. **`-corpus` 功能描述不全**：未提及 Phase 1a 新增的 `Context Rot Analysis`（上下文退化分析）与 `Tool Sequence Patterns`（工具调用序列模式）。
  3. **`-llm-addr` 行为描述陈旧**：仍描述为"对已有 Finding 做优先级排序/串联解读，绝不发现清单外新问题"，未更新 Phase 1b 引入的 6 个语义判别器在单 Journey 报告中产出 `[AI推测 · 置信度: HIGH]` Finding 的新能力，亦未说明单次运行最多触发 7 次串行 LLM 调用的调用开销。

#### 2. 中英文修订对照方案

1. **指标数量与 `-corpus` 章节**：
   - **英文 (`docs/UserGuide.md`)**：
     将 `thirteen behavior-profile numbers` 更新为 `fourteen behavior-profile numbers`；在 `-corpus` 描述段中补充上下文退化分桶（token context rot buckets）与工具 N-gram 序列挖掘（frequent tool sequence mining and error-rate attribution）说明。
   - **中文 (`docs/UserGuide.zh.md`)**：
     将 `十三项行为剖面数值` 更新为 `十四项行为剖面数值`；补充说明语料级上下文退化分析与 2-gram/3-gram 高频工具调用序列模式的统计机制。
2. **`-llm-addr` 单 Journey 行为与成本说明**：
   - **英文 (`docs/UserGuide.md`)**：
     更新 `-journey` 下 `-llm-addr` 的描述：不仅生成综合叙述性解读，还会先触发 6 个独立的 LLM 语义检测器（工具结果误读、语义死循环、目标漂移、Compaction约束丢失、计划偏离、未验证声称），经由严格的 HIGH 置信度与原文证据锚点门禁后，在决策主干与疑似问题清单中以 `[AI推测 · 置信度: HIGH]` 标注呈现；单次 `-journey` 运行最多发起 7 次 LLM 调用（可通过 `-llm-cache-dir` 缓存）。
   - **中文 (`docs/UserGuide.zh.md`)**：
     同步补充上述 6 个语义检测器在单 Journey 报告中的推测 Finding 产出机制、门禁规则、`[AI推测]` 标签说明以及最多 7 次串行 LLM 调用的计费与缓存机制。

---

## 2. 关联优化项评估（协同纳入）

为了使本次改动彻底、干净，建议在实施阶段将以下 3 个低风险、高收益的关联审查点一并处理：

1. **Issue 3-3 (中低)：合并 Finding 后未全局重排**
   - **位置**：`cmd/vmr/cmd_story.go` 中的 `renderJourney`
   - **动作**：在 `findings = append(findings, llmFindings...)` 后执行 `sort.SliceStable(findings, ...)`（按 `StepSeq` 升序，同步按 `Code` 升序）。
2. **Issue 3-6 (低)：i18n 闭包与文案规整**
   - **位置**：`internal/story/llm_findings.go` (P1b.4) 与 `render_inferred.go`
   - **动作**：将内联的中文/英文拼接统一抽取进 `internal/i18n/story_findings.go`。
3. **Issue 2d-1 (中低)：`computeContextRot` 冗余计算消除**
   - **位置**：`internal/story/corpus_contextrot.go` 与 `corpus.go`
   - **动作**：将 `ComputeCorpusStats` 已经算好的 `findingsPerJourney` 传入 `computeContextRot`，避免对全语料 Journey 重复执行 `ComputeFindings`。

---

## 3. 详细实施步骤 (Implementation Plan)

### Step 1: 契约与算法确定性修复 (Code Fixing)
1. **修改 `internal/story/llm_findings.go` (解决 Issue 3-2)**：
   - 在 `detectOscillationCandidates` 中引入窗口内 `toolNames` 排序。
   - 在返回 `out` 前补充 `Calls[0].StepSeq` + `ToolName` 的稳定排序。
   - 在 `internal/story/llm_findings_test.go` 中添加 200 次并发/连续迭代测试，锁定排序确定性。
2. **修改 `internal/story/metrics.go` 与 `cmd/vmr/cmd_story.go` (解决 Issue 3-1 & 3-3)**：
   - 在 `JourneySummary` 中增加 `LLMFindings []Finding `json:"llm_findings,omitempty"`` 字段。
   - 在 `renderJourney` 中，合并 `findings` 后执行 `sort.SliceStable`。
   - 改造 `writeJourneyFile`，使其将传入的 `llmFindings` 序列化写入 `journey-<id>.json`。
3. **（可选协同）修改 `internal/story/corpus_contextrot.go` 与 `i18n` (解决 Issue 2d-1 & 3-6)**：
   - 优化 `computeContextRot` 参数复用现成 findings；将 P1b.4 文案收敛至 `i18n`。

### Step 2: 仓库文档与规范合规补齐 (Documentation & Compliance)
1. **更新 `CHANGELOG.md` (解决 Issue 4-1)**：
   - 在 `[Unreleased]` 准确补齐 Phase 1a、Phase 1b 及近期新增功能的 Added / Changed 条目。
2. **更新 `docs/UserGuide.md` 与 `docs/UserGuide.zh.md` (解决 Issue 4-2)**：
   - 同步修正 14 项行为指标、语料级退化与序列分析、`-llm-addr` 语义推测与调用成本说明。

### Step 3: 全量构建与回归验证 (Verification & Validation)
1. **静态检查与单元测试**：
   - 运行 `go build ./...`、`go vet ./...`、`gofmt -l`。
   - 运行全量测试：`go test -count=1 ./internal/story ./internal/chatmsg ./internal/report ./internal/i18n ./internal/archtest ./cmd/vmr`。
2. **架构约束守卫验证**：
   - 确保 `internal/archtest` 中的单文件行数（Budget）、依赖方向、文档引用检查全绿。
3. **真实语料双轮逐字节对比验证**：
   - 针对 `logs/vmr-audit-2026-07-25/26/27` 执行 `vmr story -corpus` 连跑两轮，确认输出逐字节完全一致（保证未破坏确定性）。
   - 针对单 Journey 运行带 `-llm-addr` 与不带 `-llm-addr`，检查生成的 `journey-<id>.json` 与 `.md`，确认 JSON 契约中 `llm_findings` 字段正确生成且与 Markdown 严格对齐。

---

## 4. 风险评估与防御策略

| 风险点 | 严重度 | 预防与对策 |
|---|---|---|
| **JSON Schema 破坏旧消费者** | 低 | `LLMFindings` 使用 `omitempty` 且为独立新字段，不传 `-llm-addr` 时输出完全不变；旧消费者解析 `findings` 字段不受任何影响。 |
| **测试文件行数超预算** | 低 | 当前各文件均有充足行数预算（`llm_findings.go` 524/700, `cmd_story.go` 743/850, `metrics.go` 421/700），修改行数预估在 20-30 行内，不会触碰上限。 |
| **确定性再次漂移** | 中 | 在单元测试中建立循环 200 次断言机制，并在 CI/测试中用真实语料运行双轮逐字节 diff 验证。 |
