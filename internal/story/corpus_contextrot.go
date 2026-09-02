// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"fmt"
	"strings"

	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
)

// ContextRotBucket summarizes step count, finding count, and error rate
// within a specific context window size range (measured in tokens).
// The special Range value "usage unknown" (contextRotUnknownRange) marks a
// pseudo-bucket that carries steps excluded from every range because their
// manifest had no in-token usage data.
type ContextRotBucket struct {
	Range          string  `json:"range"` // "0-32k", "32k-64k", "64k-128k", "128k-256k", "256k+", "usage unknown"
	StepCount      int     `json:"step_count"`
	FindingCount   int     `json:"finding_count"`
	FindingDensity float64 `json:"finding_density"` // finding_count / step_count
	ErrorStepCount int     `json:"error_step_count"`
	ErrorRate      float64 `json:"error_rate"` // error_step_count / step_count
}

// contextRotUnknownRange is the Range value for the pseudo-bucket appended
// by computeContextRot when steps have no in-token usage data (Manifest==nil
// or !UsageInOK). renderContextRotSection skips it as a table row and
// instead renders the note "N step(s) excluded: no in-token usage data".
const contextRotUnknownRange = "usage unknown"

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
	excluded := 0

	for idx, j := range journeys {
		stepBucket := map[int]int{}
		for _, t := range j.Tasks {
			for _, s := range t.Steps {
				// Steps with no in-token usage data (nil manifest or
				// UsageInOK false) would previously fall through with
				// tokens=0 and pollute the "0-32k" bucket's error rate.
				// Now they are excluded from every range and counted in
				// the "usage unknown" pseudo-bucket (S-2).
				if s.Manifest == nil || !s.Manifest.UsageInOK {
					excluded++
					continue
				}
				bIdx := bucketIndexForTokens(s.Manifest.Usage.In)
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

	if excluded > 0 {
		buckets = append(buckets, ContextRotBucket{Range: contextRotUnknownRange, StepCount: excluded})
	}
	return buckets
}

// renderContextRotSection writes the Context Rot analysis section to the Markdown builder.
func renderContextRotSection(b *strings.Builder, buckets []ContextRotBucket, lang i18n.Lang) {
	if len(buckets) == 0 {
		return
	}

	// Separate real context-range buckets from the "usage unknown"
	// pseudo-bucket (S-2: steps excluded for missing in-token usage data).
	var excluded int
	realBuckets := make([]ContextRotBucket, 0, len(buckets))
	for _, bk := range buckets {
		if bk.Range == contextRotUnknownRange {
			excluded = bk.StepCount
		} else {
			realBuckets = append(realBuckets, bk)
		}
	}

	hasData := false
	for _, bk := range realBuckets {
		if bk.StepCount > 0 {
			hasData = true
			break
		}
	}
	if !hasData && excluded == 0 {
		return
	}

	t := i18n.Corpus(lang)
	if hasData {
		b.WriteString(t.ContextRotTitle)
		b.WriteString(t.ContextRotHeader)
		for _, bk := range realBuckets {
			fmt.Fprintf(b, "| %s | %d | %d | %.2f | %d | %s |\n",
				bk.Range, bk.StepCount, bk.FindingCount, bk.FindingDensity, bk.ErrorStepCount, pctStr(bk.ErrorRate))
		}
		b.WriteString("\n")
		b.WriteString(t.ContextRotFootnote)
	}

	// S-2 disclosures: excluded steps with no usage data, and unrecognized
	// shape counts from chatmsg. Both are hardcoded English because
	// internal/i18n is the source of truth for both lines.
	if excluded > 0 {
		fmt.Fprintf(b, "%s\n", i18n.Corpus(lang).ContextRotExcludedNote(excluded))
	}
	if parts, holders := chatmsg.UnrecognizedShapeCounts(); parts > 0 || holders > 0 {
		fmt.Fprintf(b, "%s\n", i18n.Corpus(lang).UnrecognizedShapeNote(int(parts), int(holders)))
	}
}
