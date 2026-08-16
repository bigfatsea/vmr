// Ver 2026-08-05, by Sonnet 5

// Pairs with internal/story/findings.go — the rule-derived, Step-level
// "suspect list" findings (design doc's "候选/嫌疑清单，不是判决").
// Findings text is localized in the target language (for both
// journey-<id>.md and journey-<id>.json), while FindingCode
// (story.FindingCode) is the stable identifier and never varies by language.
package i18n

import "strconv"

// StoryFindingText is one Step-level finding's localized parts — mirrors
// report's FindingText but without a Metric field (a story Finding is
// located by StepSeq, not by a report metric name).
type StoryFindingText struct {
	Finding, Evidence, Action string
}

// StoryFindingsText is findings.go/findings_toolresult.go's nine detectors'
// text, in one language. Each closure's own doc lives on the matching
// detector, not duplicated here.
type StoryFindingsText struct {
	ExactRepeatToolCall       func(tool string, count int) StoryFindingText
	NarrationWithoutAction    func(runLen int) StoryFindingText
	UnverifiedSuccess         func(errorSeq int) StoryFindingText
	ReasoningActionMismatch   func(entities string) StoryFindingText
	PlanExecutionMisalignment func(skipped, total int) StoryFindingText

	// Phase 2 (findings_toolresult.go)
	UnadaptedRetry            func(tool string) StoryFindingText
	UnusedToolResult          func(entities string) StoryFindingText
	UnverifiedEntityReference func(entities string) StoryFindingText
	ConstraintTextDropped     func(entities string, total int) StoryFindingText

	// Phase 1b (llm_findings.go)
	ToolResultMisinterpretation func(tool, explanation string) StoryFindingText
	SemanticOscillation         func(tool string, explanation string) StoryFindingText
	GoalDrift                   func(driftSeq int, explanation string) StoryFindingText
	UnverifiedCompletionClaim   func(missing string) StoryFindingText
	LLMConstraintDropped        func(anchor string) StoryFindingText
}

func StoryFindings(lang Lang) StoryFindingsText {
	if lang == ZH {
		return StoryFindingsText{
			ExactRepeatToolCall: func(tool string, count int) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似精确重复循环：" + tool + " 已被相同参数调用 " + strconv.Itoa(count) + " 次",
					Evidence: "工具 " + tool + "，" + strconv.Itoa(count) + " 次调用参数完全一致",
					Action:   "建议人工复核这几次调用是否在原地打转，而不是每次都有实质进展",
				}
			},
			NarrationWithoutAction: func(runLen int) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似“只说不做”：连续 " + strconv.Itoa(runLen) + " 轮纯文本、无工具调用、内容高度相似",
					Evidence: "连续 " + strconv.Itoa(runLen) + " 个 Step 都没有 tool_call",
					Action:   "建议人工复核 agent 是否卡在反复声明意图而未触发行动",
				}
			},
			UnverifiedSuccess: func(errorSeq int) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似静默声明成功：Step " + strconv.Itoa(errorSeq) + " 出现过错误标记，之后未见验证类调用就结束了本轮任务",
					Evidence: "错误标记出现于 Step " + strconv.Itoa(errorSeq) + "，此后到任务结束都没有再出现看起来像验证/确认的调用",
					Action:   "建议人工确认任务是否真的完成，而不是 agent 自行“脑补”了一个乐观结论",
				}
			},
			ReasoningActionMismatch: func(entities string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似推理-行动不一致：推理文本提到的 " + entities + " 未出现在本轮实际的工具调用参数里",
					Evidence: "推理文本引用的实体：" + entities,
					Action:   "建议人工核实这次调用的目标是否与推理描述的一致",
				}
			},
			PlanExecutionMisalignment: func(skipped, total int) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似计划-执行错位：本轮开头列出的 " + strconv.Itoa(total) + " 条计划里，有 " + strconv.Itoa(skipped) + " 条在后续步骤里找不到对应的执行痕迹",
					Evidence: strconv.Itoa(skipped) + "/" + strconv.Itoa(total) + " 条计划项未见后续引用（字符串/实体匹配，不代表语义上真的被跳过）",
					Action:   "建议人工核对这几条计划项是被跳过、被替代，还是只是没有用相同措辞被引用",
				}
			},
			UnadaptedRetry: func(tool string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似无适应重试：" + tool + " 出错后，紧接着的同工具重试参数逐字相同",
					Evidence: "工具 " + tool + " 收到错误结果后，下一次同工具调用的参数与出错那次完全一致",
					Action:   "建议人工确认这是真的在原地重试，还是重试前的调整没有反映在参数里",
				}
			},
			UnusedToolResult: func(entities string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似工具结果未被利用：结果中提到的 " + entities + " 在此后的步骤里再未被引用",
					Evidence: "工具结果中的实体：" + entities,
					Action:   "建议人工确认这条结果是否真的对后续决策没有影响，还是被引用时换了措辞",
				}
			},
			UnverifiedEntityReference: func(entities string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似引用了已被证伪的实体：工具结果显示 " + entities + " 不存在/未找到，但后续步骤仍在引用它",
					Evidence: "已被证伪的实体：" + entities + "（仅基于 ENOENT/404/not found 类字面标记识别，不代表确认幻觉）",
					Action:   "建议人工确认后续引用是否基于过时的假设，而不是重新验证过的结果",
				}
			},
			ConstraintTextDropped: func(entities string, total int) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似 compaction 丢失了约束文本：压缩前提到的 " + strconv.Itoa(total) + " 个实体（如 " + entities + "）在压缩后的内容里找不到了",
					Evidence: "压缩前存在、压缩后消失的实体：" + entities + "（未经验证的假设级检测，只是命名了这个现象，没有确认是否造成了实际影响）",
					Action:   "建议人工确认这些实体代表的约束/上下文是否还需要，是否应该在后续轮次里重新强调",
				}
			},
			ToolResultMisinterpretation: func(tool, explanation string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似工具结果曲解：" + tool + " 返回报错或异常，但后续推理误判为成功",
					Evidence: explanation,
					Action:   "建议人工复核模型是否对工具的报错产生了乐观幻觉并在此基础上继续推进",
				}
			},
			SemanticOscillation: func(tool, explanation string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似语义原地打转：" + tool + " 连续多次调用但参数微调缺乏实质进展",
					Evidence: explanation,
					Action:   "建议人工复核该工具调用是否陷入无效重试死循环，考虑提示模型更换探索路径",
				}
			},
			GoalDrift: func(driftSeq int, explanation string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似长程目标漂移：从 Step " + strconv.Itoa(driftSeq) + " 起执行行为显著脱离初始根目标",
					Evidence: explanation,
					Action:   "建议人工复核 Agent 是否陷入次要支线探索或调试泥潭，在 Prompt 中增加阶段性目标对齐提醒",
				}
			},
			UnverifiedCompletionClaim: func(missing string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似未验证宣称完成：终步明确声称完成任务，但轨迹中缺失对应验证动作",
					Evidence: missing,
					Action:   "建议人工复核交付物是否真实可用，要求 Agent 在宣称完成前必须执行测试/构建验证",
				}
			},
			LLMConstraintDropped: func(anchor string) StoryFindingText {
				return StoryFindingText{
					Finding:  "疑似 compaction 丢失了核心否定式约束/规范：" + anchor,
					Evidence: "",
					Action:   "建议在后续对话或 System Prompt 中重新注入该核心约束",
				}
			},
		}
	}
	return StoryFindingsText{
		ExactRepeatToolCall: func(tool string, count int) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected exact-repeat loop: " + tool + " called with identical arguments " + strconv.Itoa(count) + " times",
				Evidence: "tool " + tool + ", " + strconv.Itoa(count) + " calls with byte-identical arguments",
				Action:   "Manually review whether these calls are spinning in place rather than making real progress each time",
			}
		},
		NarrationWithoutAction: func(runLen int) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected \"narration without action\": " + strconv.Itoa(runLen) + " consecutive text-only turns with no tool call and highly similar content",
				Evidence: strconv.Itoa(runLen) + " consecutive Steps carried no tool_call",
				Action:   "Manually review whether the agent is stuck restating intent without ever acting on it",
			}
		},
		UnverifiedSuccess: func(errorSeq int) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected silent success claim: an error marker appeared at Step " + strconv.Itoa(errorSeq) + ", and the task ended without any verification-looking call afterward",
				Evidence: "error marker at Step " + strconv.Itoa(errorSeq) + "; no call resembling verification/confirmation seen between there and task end",
				Action:   "Manually confirm the task actually completed, rather than the agent having assumed an optimistic outcome",
			}
		},
		ReasoningActionMismatch: func(entities string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected reasoning-action mismatch: " + entities + " mentioned in the reasoning text does not appear in this turn's actual tool-call arguments",
				Evidence: "entities referenced in reasoning text: " + entities,
				Action:   "Manually verify this call's actual target matches what the reasoning described",
			}
		},
		PlanExecutionMisalignment: func(skipped, total int) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected plan-execution misalignment: of the " + strconv.Itoa(total) + " plan items listed at the start of this turn, " + strconv.Itoa(skipped) + " have no matching trace in the steps that followed",
				Evidence: strconv.Itoa(skipped) + "/" + strconv.Itoa(total) + " plan items had no later reference (string/entity matching only — not proof they were truly skipped semantically)",
				Action:   "Manually check whether these plan items were skipped, replaced, or just referenced with different wording",
			}
		},
		UnadaptedRetry: func(tool string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected unadapted retry: after " + tool + " errored, the very next same-tool call repeated its arguments verbatim",
				Evidence: "after " + tool + "'s result errored, the next call to the same tool used byte-identical arguments",
				Action:   "Manually confirm whether this is genuinely spinning in place, or an adjustment was made that just didn't show up in the arguments",
			}
		},
		UnusedToolResult: func(entities string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected unused tool result: " + entities + " mentioned in the result was never referenced again in any later step",
				Evidence: "entities in the tool result: " + entities,
				Action:   "Manually confirm this result truly had no bearing on later decisions, rather than being referenced under different wording",
			}
		},
		UnverifiedEntityReference: func(entities string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected reference to a falsified entity: a tool result reported " + entities + " as missing/not found, but a later step still refers to it",
				Evidence: "falsified entities: " + entities + " (identified only from a literal ENOENT/404/not-found marker — not a confirmed hallucination)",
				Action:   "Manually confirm the later reference isn't relying on a stale assumption instead of a re-verified result",
			}
		},
		ConstraintTextDropped: func(entities string, total int) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected constraint text dropped at compaction: " + strconv.Itoa(total) + " entities present before the boundary (e.g. " + entities + ") are gone from the post-compaction content",
				Evidence: "entities present before compaction, absent after: " + entities + " (an unverified, hypothesis-level check — it only names the pattern, it hasn't confirmed real impact)",
				Action:   "Manually confirm whether the constraints/context these entities represent still matter and should be re-stated in a later turn",
			}
		},
		ToolResultMisinterpretation: func(tool, explanation string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected tool result misinterpretation: " + tool + " returned an error or negative result, but subsequent reasoning claimed success",
				Evidence: explanation,
				Action:   "Manually verify whether the model developed hallucinated optimism upon tool failure and proceeded erroneously",
			}
		},
		SemanticOscillation: func(tool, explanation string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected semantic oscillation: " + tool + " called repeatedly with slight argument variations yielding no real progress",
				Evidence: explanation,
				Action:   "Manually review whether the tool invocation is stuck in a futile retry loop; prompt the agent to change search/investigation direction",
			}
		},
		GoalDrift: func(driftSeq int, explanation string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected goal drift: execution significantly deviated from the root user intent starting around Step " + strconv.Itoa(driftSeq),
				Evidence: explanation,
				Action:   "Manually check if the agent is stuck in an irrelevant subtask or rabbit hole; add periodic goal-alignment reminders in the prompt",
			}
		},
		UnverifiedCompletionClaim: func(missing string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected unverified completion claim: final response claimed task completion, but no supporting verification action was observed in the trajectory",
				Evidence: missing,
				Action:   "Manually verify whether the deliverables actually work; instruct the agent to run tests or build verification before claiming completion",
			}
		},
		LLMConstraintDropped: func(anchor string) StoryFindingText {
			return StoryFindingText{
				Finding:  "Suspected core constraint/policy dropped at compaction: " + anchor,
				Evidence: "",
				Action:   "Manually review and re-inject the critical constraint in the system prompt or subsequent turns",
			}
		},
	}
}
