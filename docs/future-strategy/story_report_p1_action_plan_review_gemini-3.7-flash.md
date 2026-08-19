// Ver 2026-08-19 21:55, by Gemini 3.7 Flash

# vmr 日志分析体系重构 — P1 执行计划（ActionPlan）源码级事实核查与审阅报告

**重要声明：依据本文档开展问题核查工作时，须以客观事实为核心依据，严格遵循既定开发计划与开发原则，不得被文档中的问题描述及相关主张误导。核查评估需优先判定问题是否真实存在、是否具备处理价值：对无处理价值的问题，直接说明情况并予以忽略；对具备处理价值的问题，再进一步核查其根因分析、解决方案的合理性，并研判是否存在优化完善空间，最终完成问题处置工作。**

## 0. 概述

本报告对 [story_report_p1_action_plan_sonnet-5.md](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_p1_action_plan_sonnet-5.md)（以下简称 P1 ActionPlan）进行了严格的源码级事实核查（Fact-Checking）、工作区落地代码比对以及架构/策略审阅，基准对照文档为 [story_report_architecture_opus-5.md](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md) 与 [story_report_dev_plan_opus-5.md](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_dev_plan_opus-5.md)。

审阅结论：P1 ActionPlan 以及当前工作区对应的具体落地代码在核心方向上完成了 P1 的关键目标（如工具 ID 归一化、脊柱全覆盖等），但在**多轮上下文位置配对机制**、**文档引用与构建基线**、**真实指令提取的方言过滤**以及**LLM 标题相对降级算法**四个关键点上存在被源码证实的事实缺陷。

以下按**严重程度 + ROI（收益/成本比）**进行梯队分级排序。严重缺陷与高 ROI 改进全部列入第一梯队，并逐条附上详细的代码核实证据（`file:line` 与逻辑分析）。

---

## 1. 梯队总览与严重度 / ROI 矩阵

| 梯队 | 编号 | 问题 / 优化项 | 性质 | 严重度 | ROI | 涉及源码位置 |
| :--- | :--- | :--- | :--- | :---: | :---: | :--- |
| **第一梯队** | **F1** | 坐标兜底（第 3 级）在多轮请求中引入全量历史 Tool Results，导致判据失效与历史错配 | 逻辑/设计缺陷 | **High** | **极高** | [render_spine_step.go:206-244](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L206-L244) |
| | **F2** | `docs/KNOWN_ISSUES_sonnet-5.md` 引用已删除文件导致 `doc_refs_test.go` 失败，阻塞构建基线 | 事实/构建错误 | **High** | **极高** | [KNOWN_ISSUES_sonnet-5.md:122,130,136](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md#L122) |
| | **F3** | Compare 开篇初始指令直接读取 `j.Events` 首个 User 消息，绕过方言识别，易被脚手架伪消息污染 | 设计漏洞 | **Medium-High** | **极高** | [compare.go:436-445](file:///Users/stanford/code/vmr/internal/story/compare.go#L436-L445) |
| | **F4** | LLM 标题层级兜底单点改写 `## ` 为 `### `，导致多级子标题层级反转塌陷 | 设计缺陷 | **Medium** | **极高** | [llm.go:399-412](file:///Users/stanford/code/vmr/internal/story/llm.go#L399-L412) |
| **第二梯队** | **F5** | 决策脊柱 Task 首步与纯问答 Journey 缺乏清晰的用户指令呈现逻辑 | 体验/设计细节 | **Medium-Low** | **中** | [render_spine_step.go:262-277](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L262-L277) |
| | **F6** | `chatmsg.ToolResultList` 缺失对 OpenAI Responses API（`function_call_output`）的协议支持 | 协议完整性 | **Low** | **中** | [toolresults.go:27-53](file:///Users/stanford/code/vmr/internal/chatmsg/toolresults.go#L27-L53) |
| | **F7** | 文本型长交付物在脊柱末尾仅呈现单行截断，与“交付物小节”概念存在认知偏差 | 认知/文档对齐 | **Low** | **低** | [render_spine_step.go:301-314](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L301-L314) |
| | **F8** | `firstNewUserText` 提取中途追加指令时未经过方言过滤 | 潜在边界 | **Low** | **中** | [render_spine_step.go:285-292](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L285-L292) |

---

## 2. 第一梯队：严重问题与高 ROI 改进（源码核实与详细说明）

### F1. 位置兜底（第 3 级）在多轮历史请求中提取到全量历史 Tool Results

- **涉及文件**：[internal/story/render_spine_step.go:206-244](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L206-L244)
- **源码核实证据**：
  在实际落地的 `positionalToolResults` 函数中，代码如下：
  ```go
  // internal/story/render_spine_step.go:206
  func positionalToolResults(steps []*Step, i int, byID map[string]chatmsg.ToolResult) map[string]chatmsg.ToolResult {
      s := steps[i]
      if len(s.ToolCalls) == 0 || i+1 >= len(steps) || steps[i+1].Rec == nil {
          return nil
      }
      knownNorm := make(map[string]bool, len(s.ToolCalls))
      var unresolved []chatmsg.ToolCall
      for _, tc := range s.ToolCalls {
          knownNorm[normalizeToolCallID(tc.ID)] = true
          if _, ok := byID[tc.ID]; !ok {
              unresolved = append(unresolved, tc)
          }
      }
      if len(unresolved) == 0 {
          return nil
      }
      body, _ := steps[i+1].Rec.Client.Request.Body.(map[string]any)
      var leftover []chatmsg.ToolResult
      // 【关键核实点】：RawArray(body) 返回整个请求的全部 messages 数组（含所有历史轮次）
      for _, r := range chatmsg.ToolResultList(chatmsg.RawArray(body)) {
          if !knownNorm[normalizeToolCallID(r.CallID)] {
              leftover = append(leftover, r)
          }
      }
      // 【关键失效点】：在第 2 轮及之后，leftover 包含了 Step 1 ~ i-1 的所有历史 tool results
      if len(leftover) != len(unresolved) {
          return nil
      }
      out := make(map[string]chatmsg.ToolResult, len(unresolved))
      for k, tc := range unresolved {
          out[tc.ID] = leftover[k]
      }
      return out
  }
  ```
- **机制与事实分析**：
  1. `steps[i+1].Rec.Client.Request.Body` 是标准的 Chat 协议请求体，其 `messages` 包含整个会话**从第 1 轮开始的所有累积上下文**。
  2. `chatmsg.ToolResultList` 会从头遍历整个 `messages` 数组，将历史所有轮次出现的 `role: tool` 或 Anthropic `type: tool_result` 全部解析出来。
  3. `knownNorm` 仅仅装载了当前 `steps[i].ToolCalls` 的 ID（即当前单步发出的 1~N 个调用）。
  4. 对于历史 Step 1 ~ Step i-1 的所有 tool results，其 `CallID` 规范化后显然不会存在于 `knownNorm` 中，因此 `!knownNorm[normalizeToolCallID(r.CallID)]` 判定恒为 `true`。
  5. **后果 1（大面积假阴性）**：若会话在第 5 步出现了一个自造 ID 的 tool call 需要位置配对，但前 4 步已经累积了 4 个 tool results，此时 `len(leftover)` 为 `4 + 1 = 5`，而 `len(unresolved) == 1`，`len(leftover) != len(unresolved)` 恒成立，位置配对在多轮会话中直接失效。
  6. **后果 2（偶发假阳性/历史错配）**：若历史刚好有且仅有 1 个旧 tool result，当前 Step 也恰好有 1 个未匹配 tool call（且当前步客户端未返回结果），`len(leftover) == 1 == len(unresolved)` 成立，位置配对会将**几个轮次之前的历史工具结果错误地作为当前 Step 的工具结果进行渲染**。
- **高 ROI 修复方案**：
  位置配对必须仅对 `steps[i+1]` 的**增量消息（Delta）**区间提取 tool results。
  
  ```go
  // 修正方案：仅扫描步骤 i+1 的增量消息区间
  rawArr := chatmsg.RawArray(body)
  off := chatmsg.MsgOffset(body)
  deltaIdx := steps[i+1].DeltaStart - off
  if deltaIdx < 0 {
      deltaIdx = 0
  }
  if deltaIdx < len(rawArr) {
      rawArr = rawArr[deltaIdx:]
  }
  for _, r := range chatmsg.ToolResultList(rawArr) {
      if !knownNorm[normalizeToolCallID(r.CallID)] {
          leftover = append(leftover, r)
      }
  }
  ```

---

### F2. KNOWN_ISSUES 文档引用失效导致 `doc_refs_test.go` 失败，阻塞构建基线

- **涉及文件**：
  - [docs/KNOWN_ISSUES_sonnet-5.md:122,130,136](file:///Users/stanford/code/vmr/docs/KNOWN_ISSUES_sonnet-5.md#L122)
  - [internal/archtest/doc_refs_test.go:252](file:///Users/stanford/code/vmr/internal/archtest/doc_refs_test.go#L252)
- **源码核实证据**：
  执行 `go test ./internal/archtest/...` 时的实际报错：
  ```
  --- FAIL: TestArchitecture_DocReferences (0.13s)
      doc_refs_test.go:252: docs/KNOWN_ISSUES_sonnet-5.md: doc path docs/future-strategy/story_report_ux_review_sonnet-5.md does not exist
      doc_refs_test.go:252: docs/KNOWN_ISSUES_sonnet-5.md: doc path docs/future-strategy/story_report_ux_review_sonnet-5.md does not exist
      doc_refs_test.go:252: docs/KNOWN_ISSUES_sonnet-5.md: doc path docs/future-strategy/story_report_ux_review_sonnet-5.md does not exist
  ```
  核对 `git log`：commit `7c2346c` 删除了 `docs/future-strategy/story_report_ux_review_sonnet-5.md`，但 `docs/KNOWN_ISSUES_sonnet-5.md` 的第 122 行（§1.20）、第 130 行（§1.21）、第 136 行（§1.22）仍保留了该文件的硬编码路径。
- **影响分析**：
  DevPlan 与 ActionPlan 均强调每次提交前必须满足 `go test ./...` 全绿。当前由于该文档路径断裂，导致门禁守卫测试失败，直接违背了 ActionPlan §1 所声称的“改动前基线全绿”。
- **修复方案**：
  将 `docs/KNOWN_ISSUES_sonnet-5.md` 中的旧文档引用重定向到权威架构文档 [story_report_architecture_opus-5.md](file:///Users/stanford/code/vmr/docs/future-strategy/story_report_architecture_opus-5.md) 的对应章节（§4.7、§5、§7.6）。

---

### F3. Compare 开篇初始指令直接读取 `j.Events` 首个 User 消息，绕过方言识别，易被脚手架伪消息污染

- **涉及文件**：
  - [internal/story/compare.go:436-445](file:///Users/stanford/code/vmr/internal/story/compare.go#L436-L445)
  - 对照：[internal/story/journey.go:666-674](file:///Users/stanford/code/vmr/internal/story/journey.go#L666-L674)
  - 对照：[internal/taskseg/realuser.go](file:///Users/stanford/code/vmr/internal/taskseg/realuser.go)
- **源码核实证据**：
  `compare.go` 中的实际实现：
  ```go
  // internal/story/compare.go:436
  func initialInstructionStats(j *Journey) InitialInstructionStats {
      for _, ev := range j.Events {
          if ev.Msg.Role != "user" {
              continue
          }
          text, truncated := truncateText(ev.Msg.Text, initialInstructionExcerptChars)
          return InitialInstructionStats{Found: true, Text: text, Truncated: truncated}
      }
      return InitialInstructionStats{}
  }
  ```
  而在 `journey.go` 构建 Journey 标题时，代码明确指出了不能直接取第一个 `user` 消息：
  ```go
  // internal/story/journey.go:666
  func deriveTitle(firstRu taskseg.RealUsers, tasks []*Task, lang i18n.Lang) string {
      // 通过 taskseg.IndexRealUsers 过滤掉 agent 客户端注入的伪 user 消息
      if t := taskseg.FirstInstruction(firstRu); t != "" {
          return t
      }
      ...
  }
  ```
- **机制与事实分析**：
  在主流 Agent 框架（如 OpenClaw / Lobster）中，客户端在发起请求时经常在最前部插入伪装为 `role: "user"` 的脚手架消息（如心跳探活 `[OpenClaw heartbeat poll]` 或环境上下文注入）。
  `internal/taskseg` 包正是为了识别真实用户指令而设计了 `Profile` 与 `IndexRealUsers`。
  `compare.go` 若直接遍历 `j.Events` 抓取第一个 `Role == "user"`，在心跳或特定 Agent 客户端场景下，提取出的“初始指令”将是无意义的系统探活或环境注入文本，而不是用户下达的真实业务指令。
- **高 ROI 修复方案**：
  直接读取 `j.Tasks[0]` 已经通过 `taskseg.IndexRealUsers` 解析并确认的真实指令（或直接利用 `taskseg.FirstInstruction` 对应的未截断原始文本）。

---

### F4. LLM 标题层级兜底单点改写 `## ` 为 `### `，导致多级子标题层级反转塌陷

- **涉及文件**：[internal/story/llm.go:399-412](file:///Users/stanford/code/vmr/internal/story/llm.go#L399-L412)
- **源码核实证据**：
  `llm.go` 中的实际实现：
  ```go
  // internal/story/llm.go:399
  func downgradeH2Headings(text string) string {
      lines := strings.Split(text, "\n")
      inFence := false
      for i, line := range lines {
          // 【核实点 1】：只检查了 ``` 围栏，遗漏了 ~~~（波浪线代码块）
          if strings.HasPrefix(strings.TrimSpace(line), "```") {
              inFence = !inFence
              continue
          }
          // 【核实点 2】：单点替换 ## ，不处理 ### 等子标题
          if !inFence && strings.HasPrefix(line, "## ") {
              lines[i] = "#" + line
          }
      }
      return strings.Join(lines, "\n")
  }
  ```
- **机制与事实分析**：
  1. 如果 LLM 在回答中输出了二级标题及三级子标题（例如 `## 1. 候选根因` 与 `### 1.1 工具参数错误`）：
  2. 经过 `downgradeH2Headings` 处理后，`## 1.` 变成了 `### 1.`，而 `### 1.1` 保持为 `### 1.1`。
  3. **后果**：子章节 `1.1` 与父章节 `1.` 变成了平级标题，导致 Markdown 大纲树（TOC）发生层级反转与塌陷。
  4. 此外，部分模型在输出 Markdown 代码时会使用 `~~~` 围栏，当前仅判断 ```` ``` ```` 会导致 `~~~` 围栏内的 `## ` 被错误降级。
- **高 ROI 修复方案**：
  实现**相对层级降级算法**：在围栏外，所有 `>= 2` 级的 Markdown 标题统一增加一个 `#`（`##` → `###`, `###` → `####`, `####` → `#####`, `#####` → `######`，若已是 6 级则保持 6 级），同时支持 `~~~` 围栏与带缩进的围栏判定。

---

## 3. 第二梯队：重要改进与优化建议（源码核实）

### F5. 决策脊柱首个 Step 缺乏指令行与纯问答 Journey 的展示体验

- **涉及文件**：[internal/story/render_spine_step.go:262-277](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L262-L277)
- **源码核实证据**：
  ```go
  // internal/story/render_spine_step.go:262
  func renderSpineBriefStep(w func(string, ...any), s *Step, taskStepIdx int, repeated, flagged bool, t i18n.SpineText) {
      w("%s", spineStepHeader(s, repeated, flagged, t))
      // 【核实点】：当 taskStepIdx == 0 时，条件为 false，跳过指令渲染
      if taskStepIdx > 0 && s.HumanInitiated {
          if text := firstNewUserText(s); text != "" {
              w("%s", t.SpineInstructionLine(oneLineTruncate(text, spineBriefLineCap)))
              return
          }
      }
      if s.RespText != "" {
          w("%s", t.SpineReportLine(oneLineTruncate(s.RespText, spineBriefLineCap)))
          return
      }
      if s.Reasoning != "" {
          w("%s", t.SpineReportLine(oneLineTruncate(s.Reasoning, spineBriefLineCap)))
      }
  }
  ```
- **分析**：
  对于纯文本问答（如 1 轮问答任务），`taskStepIdx == 0`，Step 1 只渲染 `💬 汇报`（`RespText`）。
  用户在第一步究竟问了什么，脊柱中完全没有展示指令行，读者只能依赖 Task 标题里的 80 字符截断预览。建议在 Task 首步无工具调用时，也呈现一行简要的 Prompt 指令预览。

---

### F6. `chatmsg.ToolResultList` 缺失对 OpenAI Responses API 的支持

- **涉及文件**：
  - [internal/chatmsg/toolresults.go:27-53](file:///Users/stanford/code/vmr/internal/chatmsg/toolresults.go#L27-L53)
  - 对照：[internal/chatmsg/messages.go:231-234](file:///Users/stanford/code/vmr/internal/chatmsg/messages.go#L231-L234)
- **源码核实证据**：
  在 `messages.go` 中，OpenAI Responses API 的 tool result 采用 `type == "function_call_output"` 解析：
  ```go
  // internal/chatmsg/messages.go:231
  case "function_call_output":
      id, _ := m["call_id"].(string)
      return Message{Role: "tool", Text: fmt.Sprintf("↩️ call_id=%s\n%s", id, RenderContent(m["output"]))}
  ```
  但在 `toolresults.go` 的 `ToolResultList` 中，仅包含 OpenAI Chat Completions（`tool_call_id`）和 Anthropic（`tool_result`）的分支，缺少对 `function_call_output` 的支持。
- **建议**：在 `ToolResultList` 中补充对 `type == "function_call_output"` 的解析，保持协议提取的完整性。

---

### F7. 文本型长交付物在脊柱末尾仅呈现单行截断的认知对齐

- **涉及文件**：
  - [internal/story/render_spine_step.go:301-314](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L301-L314)
  - [internal/story/compare.go:641-662](file:///Users/stanford/code/vmr/internal/story/compare.go#L641-L662)
- **源码核实证据**：
  `deliverableStats`（`compare.go:641`）专门扫描 `s.ToolCalls` 中类似文件写入（带 `path` 与 `content`）的工具调用。
  对于架构文档 §2.2 提到的样例 Step 22（纯文本长回复），`deliverableStats` 返回 `Found == false`，因此不会触发 `renderFinalDeliverable`。
- **建议**：文档需明确界定，`renderFinalDeliverable` 仅捕获工具写文件型交付物；文本型交付物由 BriefStep 单行概括是预期设计，而非遗漏。

---

### F8. `firstNewUserText` 提取中途追加指令时未经过方言过滤

- **涉及文件**：[internal/story/render_spine_step.go:285-292](file:///Users/stanford/code/vmr/internal/story/render_spine_step.go#L285-L292)
- **源码核实证据**：
  ```go
  func firstNewUserText(s *Step) string {
      for _, ev := range s.NewEvents {
          if ev.Msg.Role == "user" {
              return ev.Msg.Text
          }
      }
      return ""
  }
  ```
- **分析**：
  与 F3 类似，在中途追加指令时，若客户端在该 Step 的 `NewEvents` 中同时混入了伪装成 `user` 角色的心跳或环境事件，`firstNewUserText` 简单取第一个 `Role == "user"` 可能会取到非真实用户指令。由于该函数在 `s.HumanInitiated == true` 下触发，风险较低，但若能复用 `taskseg` 的真实文本提取将更具鲁棒性。

---

## 4. 实施与验收核对清单修订建议

在推进 P1 实施时，建议对 P1 ActionPlan 的执行步骤与验收清单进行以下修订补齐：

1. **[前置修复]** 优先修复 `docs/KNOWN_ISSUES_sonnet-5.md` 中的失效引用，确保 `go test ./internal/archtest/...` 恢复全绿。
2. **[P1.1 修复]** 改造 `positionalToolResults`，将其扫描范围严格限制在 `steps[i+1]` 的增量消息区间（`DeltaStart`），彻底消除历史轮次 tool results 带来的判据污染与错配隐患。
3. **[P1.3 修复]** Compare 初始指令提取改用经过 `taskseg` 方言过滤的真实用户指令，避免提取到心跳或环境伪消息。
4. **[P1.4 修复]** `downgradeH2Headings` 改为全量相对层级增加（`level -> level + 1`），并完整支持波浪线代码块（`~~~`）。
5. **[测试守护]** 针对上述修复点，补充多轮历史 tool results 干扰下的位置配对测试用例、含脚手架伪消息的 Compare 初始指令测试用例、以及多级标题层级保持的 LLM 降级测试用例。
