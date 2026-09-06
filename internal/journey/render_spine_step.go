// Ver 2026-09-15, by pi

// Deprecated: legacy renderDecisionSpine eating in-memory *Journey was deleted
// in Phase 3 in favor of viewmodel_spine.go (D11).
// capFullWith, positionalToolResults and truncation constants retained here.
package journey

import "vmr/internal/chatmsg"

const (
	spineWhyRespCap      = 400
	spineWhyReasoningCap = 200
	spineBriefLineCap    = 120
)

func capFullWith(s string, tail func(more int) string) string {
	r := []rune(s)
	if len(r) <= spineFullCap {
		return s
	}
	return string(r[:spineFullCap]) + tail(len(r)-spineFullCap)
}

func positionalToolResults(steps []*Step, i int, byID map[string]chatmsg.ToolResult) map[string]chatmsg.ToolResult {
	s := steps[i]
	if len(s.ToolCalls) == 0 || i+1 >= len(steps) {
		return nil
	}
	knownNorm := make(map[string]bool, len(s.ToolCalls))
	var unresolved []chatmsg.ToolCall
	for _, tc := range s.ToolCalls {
		knownNorm[chatmsg.NormalizeToolCallID(tc.ID)] = true
		if _, ok := byID[tc.ID]; !ok {
			unresolved = append(unresolved, tc)
		}
	}
	if len(unresolved) == 0 {
		return nil
	}
	var leftover []chatmsg.ToolResult
	for _, r := range steps[i+1].NewToolResults {
		if !knownNorm[chatmsg.NormalizeToolCallID(r.CallID)] {
			leftover = append(leftover, r)
		}
	}
	if len(leftover) != len(unresolved) {
		return nil
	}
	out := make(map[string]chatmsg.ToolResult, len(unresolved))
	for k, tc := range unresolved {
		out[tc.ID] = leftover[k]
	}
	return out
}
