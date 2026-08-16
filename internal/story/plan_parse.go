// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"regexp"
	"strconv"
	"strings"
)

// PlanItem represents a single parsed item from an execution plan.
type PlanItem struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
	Kind  string `json:"kind"` // "numbered", "checklist", "step"
}

const (
	minPlanItems = 2
	maxPlanItems = 8
)

var (
	// Numbered list: 1. / 1、 / 1) / (1)
	numberedRe = regexp.MustCompile(`(?m)^\s*(?:\(?(\d+)\)?[.、\)]|\d+[.、\)])\s*(.+)$`)

	// Markdown checklist: - [ ] / - [x] / * [ ] / + [ ]
	checklistRe = regexp.MustCompile(`(?m)^\s*(?:[-*+]\s*\[[ xX]\])\s*(.+)$`)

	// Step/Phase prefix: Step 1: / Phase 1: / Stage 1: / 步骤 1: / 步骤一: / 阶段一:
	stepPrefixRe = regexp.MustCompile(`(?m)^\s*(?:(?:Step|Phase|Stage|步骤|阶段)\s*(\d+|[一二三四五六七八九十]+)[：:]\s*(.+))$`)
)

var chineseNumMap = map[string]int{
	"一": 1, "二": 2, "三": 3, "四": 4, "五": 5,
	"六": 6, "七": 7, "八": 8, "九": 9, "十": 10,
}

func parseStepNum(raw string) int {
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	if n, ok := chineseNumMap[raw]; ok {
		return n
	}
	return 0
}

type candidateRun struct {
	items  []PlanItem
	endPos int
}

// ExtractActionablePlan extracts the last contiguous actionable plan items from text,
// supporting standard numbered lists, markdown checklists, and step/phase prefixes.
func ExtractActionablePlan(text string) []PlanItem {
	var candidates []candidateRun

	// 1. Scan numbered lists
	if matches := numberedRe.FindAllStringSubmatchIndex(text, -1); len(matches) > 0 {
		var cur []PlanItem
		prevN := 0
		lastEnd := 0
		for _, m := range matches {
			numStr := text[m[2]:m[3]]
			itemText := strings.TrimSpace(text[m[4]:m[5]])
			n, _ := strconv.Atoi(numStr)
			if len(cur) > 0 && n <= prevN {
				candidates = append(candidates, candidateRun{items: cur, endPos: lastEnd})
				cur = nil
			}
			cur = append(cur, PlanItem{
				Index: n,
				Text:  itemText,
				Kind:  "numbered",
			})
			prevN = n
			lastEnd = m[1]
		}
		if len(cur) > 0 {
			candidates = append(candidates, candidateRun{items: cur, endPos: lastEnd})
		}
	}

	// 2. Scan Markdown checklists. Unlike numbered/step items, checklist
	// items carry no ordinal to detect a run boundary from, so continuity
	// is judged by line proximity instead: consecutive checklist lines (or
	// separated by a single blank line) are the same run; a bigger gap
	// (unrelated prose, a different checklist elsewhere in the turn) starts
	// a new one. Without this, two unrelated checklists anywhere in the same
	// text — e.g. an "already done" list followed by a "still to do" list —
	// would merge into one candidate, and the earlier list's items (never
	// referenced again because they were already completed) would misread
	// as a real plan-execution gap.
	if matches := checklistRe.FindAllStringSubmatchIndex(text, -1); len(matches) > 0 {
		var cur []PlanItem
		lastEnd := 0
		prevLine := -1
		for _, m := range matches {
			line := strings.Count(text[:m[0]], "\n")
			if len(cur) > 0 && line-prevLine > 2 {
				candidates = append(candidates, candidateRun{items: cur, endPos: lastEnd})
				cur = nil
			}
			itemText := strings.TrimSpace(text[m[2]:m[3]])
			cur = append(cur, PlanItem{
				Index: len(cur) + 1,
				Text:  itemText,
				Kind:  "checklist",
			})
			lastEnd = m[1]
			prevLine = line
		}
		if len(cur) > 0 {
			candidates = append(candidates, candidateRun{items: cur, endPos: lastEnd})
		}
	}

	// 3. Scan Step/Phase prefixes
	if matches := stepPrefixRe.FindAllStringSubmatchIndex(text, -1); len(matches) > 0 {
		var cur []PlanItem
		prevN := 0
		lastEnd := 0
		for _, m := range matches {
			numStr := text[m[2]:m[3]]
			itemText := strings.TrimSpace(text[m[4]:m[5]])
			n := parseStepNum(numStr)
			if len(cur) > 0 && (n <= prevN && n != 0) {
				candidates = append(candidates, candidateRun{items: cur, endPos: lastEnd})
				cur = nil
			}
			cur = append(cur, PlanItem{
				Index: n,
				Text:  itemText,
				Kind:  "step",
			})
			prevN = n
			lastEnd = m[1]
		}
		if len(cur) > 0 {
			candidates = append(candidates, candidateRun{items: cur, endPos: lastEnd})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Pick candidate run ending latest in text
	latestIdx := 0
	latestEnd := -1
	for i, c := range candidates {
		if c.endPos > latestEnd {
			latestEnd = c.endPos
			latestIdx = i
		}
	}

	return candidates[latestIdx].items
}
