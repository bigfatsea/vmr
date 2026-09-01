// Ver 2026-09-01, by Sonnet 5

// Pairs with internal/story/render_indicators.go — one Journey's behavior
// indicators section in Markdown (问题 9).
package i18n

import "fmt"

// IndicatorsText is render_indicators.go's text, in one language.
type IndicatorsText struct {
	Title            string
	TableHeader      string // header+separator row
	NetWorkingTime   string
	ModelTime        string
	AgentExecTime    string
	HumanIdleTime    string
	ModelToolRatio   string
	ToolCallCount    string
	DupActionRate    string
	OutputRepeatRate string
	ErrorRecovery    string
	PlanExecRatio    string
	ContextUtil      string
	CompactionCount  string
	CompactionLoss   string
	ModelSwitches    string
	SparklineTitle   string
	SparklineCaption func(start, end int64) string
}

func Indicators(lang Lang) IndicatorsText {
	if lang == ZH {
		return IndicatorsText{
			Title:            "## 行为指标\n\n",
			TableHeader:      "| 指标 | 值 |\n|---|---|\n",
			NetWorkingTime:   "净工作时长",
			ModelTime:        "模型时间",
			AgentExecTime:    "Agent 侧执行时间",
			HumanIdleTime:    "人类空闲时间",
			ModelToolRatio:   "模型/工具耗时比",
			ToolCallCount:    "工具调用次数",
			DupActionRate:    "重复动作率",
			OutputRepeatRate: "输出重复率",
			ErrorRecovery:    "错误恢复次数",
			PlanExecRatio:    "计划/执行比",
			ContextUtil:      "上下文有效利用率",
			CompactionCount:  "Compaction 次数",
			CompactionLoss:   "Compaction 损失 Token",
			ModelSwitches:    "模型切换次数",
			SparklineTitle:   "**上下文构成演化趋势**",
			SparklineCaption: func(start, end int64) string {
				return fmt.Sprintf("（起始 %d tok → 结束 %d tok）\n\n", start, end)
			},
		}
	}
	return IndicatorsText{
		Title:            "## Behavior Indicators\n\n",
		TableHeader:      "| Metric | Value |\n|---|---|\n",
		NetWorkingTime:   "Net Working Time",
		ModelTime:        "Model Time",
		AgentExecTime:    "Agent Execution Time",
		HumanIdleTime:    "Human Idle Time",
		ModelToolRatio:   "Model-to-Tool Ratio",
		ToolCallCount:    "Tool Call Count",
		DupActionRate:    "Duplicate Action Rate",
		OutputRepeatRate: "Output Repetition Rate",
		ErrorRecovery:    "Error Recovery Count",
		PlanExecRatio:    "Plan-to-Execution Ratio",
		ContextUtil:      "Context Utilization",
		CompactionCount:  "Compaction Count",
		CompactionLoss:   "Compaction Loss Tokens",
		ModelSwitches:    "Model Switches",
		SparklineTitle:   "**Context Token Trajectory**",
		SparklineCaption: func(start, end int64) string {
			return fmt.Sprintf("(start %d tok → end %d tok)\n\n", start, end)
		},
	}
}
