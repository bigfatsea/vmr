// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"fmt"
	"sort"
	"strings"
	"vmr/internal/i18n"
)

// ToolSequencePattern represents an N-gram sequence of tool calls observed across tasks.
type ToolSequencePattern struct {
	Length      int      `json:"length"` // 2 or 3
	Sequence    []string `json:"sequence"`
	Occurrences int      `json:"occurrences"`
	ErrorRate   float64  `json:"error_rate"` // rate of sequence tail step having an error
}

const corpusMaxToolSequences = 10

type patternStats struct {
	seq        []string
	length     int
	count      int
	errorCount int
}

// computeToolSequences extracts frequent 2-gram and 3-gram tool call patterns across all journeys.
func computeToolSequences(journeys []*Journey) []ToolSequencePattern {
	statsMap := map[string]*patternStats{}

	type toolOccur struct {
		name    string
		isError bool
	}

	for _, j := range journeys {
		for _, t := range j.Tasks {
			var taskTools []toolOccur
			for _, s := range t.Steps {
				errStep := isErrorStep(s)
				for _, tc := range s.ToolCalls {
					if name := strings.TrimSpace(tc.Name); name != "" {
						taskTools = append(taskTools, toolOccur{name: name, isError: errStep})
					}
				}
			}

			// Extract 2-grams and 3-grams within task boundaries
			for n := 2; n <= 3; n++ {
				if len(taskTools) < n {
					continue
				}
				for i := 0; i <= len(taskTools)-n; i++ {
					window := taskTools[i : i+n]
					names := make([]string, n)
					for k := 0; k < n; k++ {
						names[k] = window[k].name
					}
					key := strings.Join(names, " \u2192 ")
					if statsMap[key] == nil {
						statsMap[key] = &patternStats{
							seq:    names,
							length: n,
						}
					}
					statsMap[key].count++
					if window[n-1].isError {
						statsMap[key].errorCount++
					}
				}
			}
		}
	}

	var list []*patternStats
	for _, ps := range statsMap {
		if ps.count >= 1 {
			list = append(list, ps)
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		if list[i].length != list[j].length {
			return list[i].length > list[j].length
		}
		// Final tiebreak on the sequence itself: list iterates a map, so
		// without this, a tie at the Top-10 cutoff would order (and
		// therefore which patterns survive the cutoff) differently on every
		// run of the same input — reports must be byte-identical for
		// identical input.
		return strings.Join(list[i].seq, " ") < strings.Join(list[j].seq, " ")
	})

	if len(list) > corpusMaxToolSequences {
		list = list[:corpusMaxToolSequences]
	}

	out := make([]ToolSequencePattern, len(list))
	for i, ps := range list {
		var errRate float64
		if ps.count > 0 {
			errRate = float64(ps.errorCount) / float64(ps.count)
		}
		out[i] = ToolSequencePattern{
			Length:      ps.length,
			Sequence:    ps.seq,
			Occurrences: ps.count,
			ErrorRate:   errRate,
		}
	}
	return out
}

// renderToolSequenceSection writes the Tool Sequence Patterns section to the Markdown builder.
func renderToolSequenceSection(b *strings.Builder, patterns []ToolSequencePattern, lang i18n.Lang) {
	t := i18n.Corpus(lang)
	b.WriteString(t.ToolSeqTitle)
	if len(patterns) == 0 {
		b.WriteString(t.NoToolSeq)
		return
	}
	b.WriteString(t.ToolSeqHeader)
	for _, p := range patterns {
		fmt.Fprintf(b, "| %s | %d | %s |\n",
			strings.Join(p.Sequence, " \u2192 "), p.Occurrences, pctStr(p.ErrorRate))
	}
	b.WriteString("\n")
	b.WriteString(t.ToolSeqFootnote)
}
