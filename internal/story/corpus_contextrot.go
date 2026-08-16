// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"fmt"
	"strings"
	"vmr/internal/i18n"
)

// ContextRotBucket summarizes step count, finding count, and error rate
// within a specific context window size range (measured in tokens).
type ContextRotBucket struct {
	Range          string  `json:"range"` // "0-32k", "32k-64k", "64k-128k", "128k-256k", "256k+"
	StepCount      int     `json:"step_count"`
	FindingCount   int     `json:"finding_count"`
	FindingDensity float64 `json:"finding_density"` // finding_count / step_count
	ErrorStepCount int     `json:"error_step_count"`
	ErrorRate      float64 `json:"error_rate"` // error_step_count / step_count
}

var contextRotRanges = []struct {
	name string
	min  int64
	max  int64 // inclusive upper bound, -1 for unbounded
}{
	{name: "0-32k", min: 0, max: 32000},
	{name: "32k-64k", min: 32000, max: 64000},
	{name: "64k-128k", min: 64000, max: 128000},
	{name: "128k-256k", min: 128000, max: 256000},
	{name: "256k+", min: 256000, max: -1},
}

func bucketIndexForTokens(tokens int64) int {
	for i, r := range contextRotRanges {
		if r.max == -1 {
			if tokens >= r.min {
				return i
			}
		} else {
			if tokens >= r.min && tokens < r.max {
				return i
			}
		}
	}
	return 0
}

func isErrorStep(s *Step) bool {
	for _, ev := range s.NewEvents {
		if ev != nil && strings.Contains(ev.Msg.Text, isErrorMarker) {
			return true
		}
	}
	return false
}

// computeContextRot computes step counts, finding densities, and error rates across context size buckets.
// findingsPerJourney can be passed in to reuse already computed findings from ComputeCorpusStats.
func computeContextRot(journeys []*Journey, findingsPerJourney [][]Finding) []ContextRotBucket {
	buckets := make([]ContextRotBucket, len(contextRotRanges))
	for i, r := range contextRotRanges {
		buckets[i].Range = r.name
	}

	for idx, j := range journeys {
		stepBucket := map[int]int{}
		for _, t := range j.Tasks {
			for _, s := range t.Steps {
				var tokens int64
				if s.Manifest != nil {
					tokens = s.Manifest.Usage.In
				}
				bIdx := bucketIndexForTokens(tokens)
				stepBucket[s.Seq] = bIdx

				buckets[bIdx].StepCount++
				if isErrorStep(s) {
					buckets[bIdx].ErrorStepCount++
				}
			}
		}

		var findings []Finding
		if idx < len(findingsPerJourney) {
			findings = findingsPerJourney[idx]
		} else {
			findings = ComputeFindings(j, i18n.EN)
		}
		for _, f := range findings {
			if bIdx, ok := stepBucket[f.StepSeq]; ok {
				buckets[bIdx].FindingCount++
			}
		}
	}

	for i := range buckets {
		if buckets[i].StepCount > 0 {
			buckets[i].FindingDensity = float64(buckets[i].FindingCount) / float64(buckets[i].StepCount)
			buckets[i].ErrorRate = float64(buckets[i].ErrorStepCount) / float64(buckets[i].StepCount)
		}
	}

	return buckets
}

// renderContextRotSection writes the Context Rot analysis section to the Markdown builder.
func renderContextRotSection(b *strings.Builder, buckets []ContextRotBucket, lang i18n.Lang) {
	if len(buckets) == 0 {
		return
	}
	hasData := false
	for _, bk := range buckets {
		if bk.StepCount > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		return
	}

	t := i18n.Corpus(lang)
	b.WriteString(t.ContextRotTitle)
	b.WriteString(t.ContextRotHeader)
	for _, bk := range buckets {
		fmt.Fprintf(b, "| %s | %d | %d | %.2f | %d | %s |\n",
			bk.Range, bk.StepCount, bk.FindingCount, bk.FindingDensity, bk.ErrorStepCount, pctStr(bk.ErrorRate))
	}
	b.WriteString("\n")
	b.WriteString(t.ContextRotFootnote)
}
