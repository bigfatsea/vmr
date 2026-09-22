// Ver 2026-09-22 02:40, by Sonnet 5

// Pairs with internal/journey/findings.go — the rule-derived, Step-level
// "suspect list" findings (design doc's "候选/嫌疑清单，不是判决").
// Findings text is localized in the target language (for both
// j-<id>.md and j-<id>.json), while FindingCode
// (journey.FindingCode) is the stable identifier and never varies by language.
package i18n

import "fmt"

// JourneyFindingText is one Step-level finding's localized parts — mirrors
// report's FindingText but without a Metric field (a journey Finding is
// located by StepSeq, not by a report metric name).
type JourneyFindingText struct {
	Finding, Evidence, Action string
}

// JourneyFindingsText is findings.go/findings_toolresult.go's nine detectors'
// text, in one language. Each closure's own doc lives on the matching
// detector, not duplicated here.
type JourneyFindingsText struct {
	ExactRepeatToolCall       func(tool string, count int) JourneyFindingText
	NarrationWithoutAction    func(runLen int) JourneyFindingText
	UnverifiedSuccess         func(errorSeq int) JourneyFindingText
	ReasoningActionMismatch   func(entities string) JourneyFindingText
	PlanExecutionMisalignment func(skipped, total int) JourneyFindingText

	// Phase 2 (findings_toolresult.go)
	UnadaptedRetry            func(tool string) JourneyFindingText
	UnusedToolResult          func(entities string) JourneyFindingText
	UnverifiedEntityReference func(entities string) JourneyFindingText
	ConstraintTextDropped     func(entities string, total int) JourneyFindingText

	// Phase 1b (llm_findings.go)
	ToolResultMisinterpretation func(tool, explanation string) JourneyFindingText
	SemanticOscillation         func(tool string, explanation string) JourneyFindingText
	GoalDrift                   func(driftSeq int, explanation string) JourneyFindingText
	UnverifiedCompletionClaim   func(missing string) JourneyFindingText
	LLMConstraintDropped        func(anchor string) JourneyFindingText
}

// journeyFindingsRow holds this file's literal templates, one row per Lang
// (Table's own doc comment). No detector here has internal branching — every
// field is pure interpolation, so unlike report_efficiency.go/report_doc.go
// there is no shared-logic closure to write; JourneyFindings below just
// plugs each row's templates into fmt.Sprintf.
type journeyFindingsRow struct {
	exactRepeatFindingFmt  string
	exactRepeatEvidenceFmt string
	exactRepeatAction      string

	narrationFindingFmt  string
	narrationEvidenceFmt string
	narrationAction      string

	unverifiedSuccessFindingFmt  string
	unverifiedSuccessEvidenceFmt string
	unverifiedSuccessAction      string

	reasoningMismatchFindingFmt  string
	reasoningMismatchEvidenceFmt string
	reasoningMismatchAction      string

	planMisalignmentFindingFmt  string
	planMisalignmentEvidenceFmt string
	planMisalignmentAction      string

	unadaptedRetryFindingFmt  string
	unadaptedRetryEvidenceFmt string
	unadaptedRetryAction      string

	unusedResultFindingFmt  string
	unusedResultEvidenceFmt string
	unusedResultAction      string

	unverifiedEntityFindingFmt  string
	unverifiedEntityEvidenceFmt string
	unverifiedEntityAction      string

	constraintDroppedFindingFmt  string
	constraintDroppedEvidenceFmt string
	constraintDroppedAction      string

	toolMisinterpretationFindingFmt string
	toolMisinterpretationAction     string

	semanticOscillationFindingFmt string
	semanticOscillationAction     string

	goalDriftFindingFmt string
	goalDriftAction     string

	unverifiedCompletionFinding string
	unverifiedCompletionAction  string

	llmConstraintDroppedFindingFmt string
	llmConstraintDroppedAction     string
}

var journeyFindingsRows = Table[journeyFindingsRow]{
	EN: {
		exactRepeatFindingFmt:  "Suspected exact-repeat loop: %s called with identical arguments %d times",
		exactRepeatEvidenceFmt: "tool %s, %d calls with byte-identical arguments",
		exactRepeatAction:      "Manually review whether these calls are spinning in place rather than making real progress each time",

		narrationFindingFmt:  "Suspected \"narration without action\": %d consecutive text-only turns with no tool call and highly similar content",
		narrationEvidenceFmt: "%d consecutive Steps carried no tool_call",
		narrationAction:      "Manually review whether the agent is stuck restating intent without ever acting on it",

		unverifiedSuccessFindingFmt:  "Suspected silent success claim: an error marker appeared at Step %d, and the task ended without any verification-looking call afterward",
		unverifiedSuccessEvidenceFmt: "error marker at Step %d; no call resembling verification/confirmation seen between there and task end",
		unverifiedSuccessAction:      "Manually confirm the task actually completed, rather than the agent having assumed an optimistic outcome",

		reasoningMismatchFindingFmt:  "Suspected reasoning-action mismatch: %s mentioned in the reasoning text does not appear in this turn's actual tool-call arguments",
		reasoningMismatchEvidenceFmt: "entities referenced in reasoning text: %s",
		reasoningMismatchAction:      "Manually verify this call's actual target matches what the reasoning described",

		planMisalignmentFindingFmt:  "Suspected plan-execution misalignment: of the %d plan items listed at the start of this turn, %d have no matching trace in the steps that followed",
		planMisalignmentEvidenceFmt: "%d/%d plan items had no later reference (string/entity matching only — not proof they were truly skipped semantically)",
		planMisalignmentAction:      "Manually check whether these plan items were skipped, replaced, or just referenced with different wording",

		unadaptedRetryFindingFmt:  "Suspected unadapted retry: after %s errored, the very next same-tool call repeated its arguments verbatim",
		unadaptedRetryEvidenceFmt: "after %s's result errored, the next call to the same tool used byte-identical arguments",
		unadaptedRetryAction:      "Manually confirm whether this is genuinely spinning in place, or an adjustment was made that just didn't show up in the arguments",

		unusedResultFindingFmt:  "Suspected unused tool result: %s mentioned in the result was never referenced again in any later step",
		unusedResultEvidenceFmt: "entities in the tool result: %s",
		unusedResultAction:      "Manually confirm this result truly had no bearing on later decisions, rather than being referenced under different wording",

		unverifiedEntityFindingFmt:  "Suspected reference to a falsified entity: a tool result reported %s as missing/not found, but a later step still refers to it",
		unverifiedEntityEvidenceFmt: "falsified entities: %s (identified only from a literal ENOENT/404/not-found marker — not a confirmed hallucination)",
		unverifiedEntityAction:      "Manually confirm the later reference isn't relying on a stale assumption instead of a re-verified result",

		constraintDroppedFindingFmt:  "Suspected constraint text dropped at compaction: %d entities present before the boundary (e.g. %s) are gone from the post-compaction content",
		constraintDroppedEvidenceFmt: "entities present before compaction, absent after: %s (an unverified, hypothesis-level check — it only names the pattern, it hasn't confirmed real impact)",
		constraintDroppedAction:      "Manually confirm whether the constraints/context these entities represent still matter and should be re-stated in a later turn",

		toolMisinterpretationFindingFmt: "Suspected tool result misinterpretation: %s returned an error or negative result, but subsequent reasoning claimed success",
		toolMisinterpretationAction:     "Manually verify whether the model developed hallucinated optimism upon tool failure and proceeded erroneously",

		semanticOscillationFindingFmt: "Suspected semantic oscillation: %s called repeatedly with slight argument variations yielding no real progress",
		semanticOscillationAction:     "Manually review whether the tool invocation is stuck in a futile retry loop; prompt the agent to change search/investigation direction",

		goalDriftFindingFmt: "Suspected goal drift: execution significantly deviated from the root user intent starting around Step %d",
		goalDriftAction:     "Manually check if the agent is stuck in an irrelevant subtask or rabbit hole; add periodic goal-alignment reminders in the prompt",

		unverifiedCompletionFinding: "Suspected unverified completion claim: final response claimed task completion, but no supporting verification action was observed in the trajectory",
		unverifiedCompletionAction:  "Manually verify whether the deliverables actually work; instruct the agent to run tests or build verification before claiming completion",

		llmConstraintDroppedFindingFmt: "Suspected core constraint/policy dropped at compaction: %s",
		llmConstraintDroppedAction:     "Manually review and re-inject the critical constraint in the system prompt or subsequent turns",
	},
	ZH: {
		exactRepeatFindingFmt:  "疑似精确重复循环：%s 已被相同参数调用 %d 次",
		exactRepeatEvidenceFmt: "工具 %s，%d 次调用参数完全一致",
		exactRepeatAction:      "建议人工复核这几次调用是否在原地打转，而不是每次都有实质进展",

		narrationFindingFmt:  "疑似“只说不做”：连续 %d 轮纯文本、无工具调用、内容高度相似",
		narrationEvidenceFmt: "连续 %d 个 Step 都没有 tool_call",
		narrationAction:      "建议人工复核 agent 是否卡在反复声明意图而未触发行动",

		unverifiedSuccessFindingFmt:  "疑似静默声明成功：Step %d 出现过错误标记，之后未见验证类调用就结束了本轮任务",
		unverifiedSuccessEvidenceFmt: "错误标记出现于 Step %d，此后到任务结束都没有再出现看起来像验证/确认的调用",
		unverifiedSuccessAction:      "建议人工确认任务是否真的完成，而不是 agent 自行“脑补”了一个乐观结论",

		reasoningMismatchFindingFmt:  "疑似推理-行动不一致：推理文本提到的 %s 未出现在本轮实际的工具调用参数里",
		reasoningMismatchEvidenceFmt: "推理文本引用的实体：%s",
		reasoningMismatchAction:      "建议人工核实这次调用的目标是否与推理描述的一致",

		planMisalignmentFindingFmt:  "疑似计划-执行错位：本轮开头列出的 %d 条计划里，有 %d 条在后续步骤里找不到对应的执行痕迹",
		planMisalignmentEvidenceFmt: "%d/%d 条计划项未见后续引用（字符串/实体匹配，不代表语义上真的被跳过）",
		planMisalignmentAction:      "建议人工核对这几条计划项是被跳过、被替代，还是只是没有用相同措辞被引用",

		unadaptedRetryFindingFmt:  "疑似无适应重试：%s 出错后，紧接着的同工具重试参数逐字相同",
		unadaptedRetryEvidenceFmt: "工具 %s 收到错误结果后，下一次同工具调用的参数与出错那次完全一致",
		unadaptedRetryAction:      "建议人工确认这是真的在原地重试，还是重试前的调整没有反映在参数里",

		unusedResultFindingFmt:  "疑似工具结果未被利用：结果中提到的 %s 在此后的步骤里再未被引用",
		unusedResultEvidenceFmt: "工具结果中的实体：%s",
		unusedResultAction:      "建议人工确认这条结果是否真的对后续决策没有影响，还是被引用时换了措辞",

		unverifiedEntityFindingFmt:  "疑似引用了已被证伪的实体：工具结果显示 %s 不存在/未找到，但后续步骤仍在引用它",
		unverifiedEntityEvidenceFmt: "已被证伪的实体：%s（仅基于 ENOENT/404/not found 类字面标记识别，不代表确认幻觉）",
		unverifiedEntityAction:      "建议人工确认后续引用是否基于过时的假设，而不是重新验证过的结果",

		constraintDroppedFindingFmt:  "疑似 compaction 丢失了约束文本：压缩前提到的 %d 个实体（如 %s）在压缩后的内容里找不到了",
		constraintDroppedEvidenceFmt: "压缩前存在、压缩后消失的实体：%s（未经验证的假设级检测，只是命名了这个现象，没有确认是否造成了实际影响）",
		constraintDroppedAction:      "建议人工确认这些实体代表的约束/上下文是否还需要，是否应该在后续轮次里重新强调",

		toolMisinterpretationFindingFmt: "疑似工具结果曲解：%s 返回报错或异常，但后续推理误判为成功",
		toolMisinterpretationAction:     "建议人工复核模型是否对工具的报错产生了乐观幻觉并在此基础上继续推进",

		semanticOscillationFindingFmt: "疑似语义原地打转：%s 连续多次调用但参数微调缺乏实质进展",
		semanticOscillationAction:     "建议人工复核该工具调用是否陷入无效重试死循环，考虑提示模型更换探索路径",

		goalDriftFindingFmt: "疑似长程目标漂移：从 Step %d 起执行行为显著脱离初始根目标",
		goalDriftAction:     "建议人工复核 Agent 是否陷入次要支线探索或调试泥潭，在 Prompt 中增加阶段性目标对齐提醒",

		unverifiedCompletionFinding: "疑似未验证宣称完成：终步明确声称完成任务，但轨迹中缺失对应验证动作",
		unverifiedCompletionAction:  "建议人工复核交付物是否真实可用，要求 Agent 在宣称完成前必须执行测试/构建验证",

		llmConstraintDroppedFindingFmt: "疑似 compaction 丢失了核心否定式约束/规范：%s",
		llmConstraintDroppedAction:     "建议在后续对话或 System Prompt 中重新注入该核心约束",
	},
}

func JourneyFindings(lang Lang) JourneyFindingsText {
	r := journeyFindingsRows.Row(lang)
	return JourneyFindingsText{
		ExactRepeatToolCall: func(tool string, count int) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.exactRepeatFindingFmt, tool, count),
				Evidence: fmt.Sprintf(r.exactRepeatEvidenceFmt, tool, count),
				Action:   r.exactRepeatAction,
			}
		},
		NarrationWithoutAction: func(runLen int) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.narrationFindingFmt, runLen),
				Evidence: fmt.Sprintf(r.narrationEvidenceFmt, runLen),
				Action:   r.narrationAction,
			}
		},
		UnverifiedSuccess: func(errorSeq int) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.unverifiedSuccessFindingFmt, errorSeq),
				Evidence: fmt.Sprintf(r.unverifiedSuccessEvidenceFmt, errorSeq),
				Action:   r.unverifiedSuccessAction,
			}
		},
		ReasoningActionMismatch: func(entities string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.reasoningMismatchFindingFmt, entities),
				Evidence: fmt.Sprintf(r.reasoningMismatchEvidenceFmt, entities),
				Action:   r.reasoningMismatchAction,
			}
		},
		PlanExecutionMisalignment: func(skipped, total int) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.planMisalignmentFindingFmt, total, skipped),
				Evidence: fmt.Sprintf(r.planMisalignmentEvidenceFmt, skipped, total),
				Action:   r.planMisalignmentAction,
			}
		},
		UnadaptedRetry: func(tool string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.unadaptedRetryFindingFmt, tool),
				Evidence: fmt.Sprintf(r.unadaptedRetryEvidenceFmt, tool),
				Action:   r.unadaptedRetryAction,
			}
		},
		UnusedToolResult: func(entities string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.unusedResultFindingFmt, entities),
				Evidence: fmt.Sprintf(r.unusedResultEvidenceFmt, entities),
				Action:   r.unusedResultAction,
			}
		},
		UnverifiedEntityReference: func(entities string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.unverifiedEntityFindingFmt, entities),
				Evidence: fmt.Sprintf(r.unverifiedEntityEvidenceFmt, entities),
				Action:   r.unverifiedEntityAction,
			}
		},
		ConstraintTextDropped: func(entities string, total int) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.constraintDroppedFindingFmt, total, entities),
				Evidence: fmt.Sprintf(r.constraintDroppedEvidenceFmt, entities),
				Action:   r.constraintDroppedAction,
			}
		},
		ToolResultMisinterpretation: func(tool, explanation string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.toolMisinterpretationFindingFmt, tool),
				Evidence: explanation,
				Action:   r.toolMisinterpretationAction,
			}
		},
		SemanticOscillation: func(tool string, explanation string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.semanticOscillationFindingFmt, tool),
				Evidence: explanation,
				Action:   r.semanticOscillationAction,
			}
		},
		GoalDrift: func(driftSeq int, explanation string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.goalDriftFindingFmt, driftSeq),
				Evidence: explanation,
				Action:   r.goalDriftAction,
			}
		},
		UnverifiedCompletionClaim: func(missing string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  r.unverifiedCompletionFinding,
				Evidence: missing,
				Action:   r.unverifiedCompletionAction,
			}
		},
		LLMConstraintDropped: func(anchor string) JourneyFindingText {
			return JourneyFindingText{
				Finding:  fmt.Sprintf(r.llmConstraintDroppedFindingFmt, anchor),
				Evidence: "",
				Action:   r.llmConstraintDroppedAction,
			}
		},
	}
}
