// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"strings"
	"unicode"
)

// tokenizeText splits text into tokens (words for alphanumeric sequences, individual runes for CJK characters).
func tokenizeText(text string) []string {
	var tokens []string
	var word strings.Builder

	flushWord := func() {
		if word.Len() > 0 {
			tokens = append(tokens, strings.ToLower(word.String()))
			word.Reset()
		}
	}

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r >= 0x4e00 && r <= 0x9fff { // CJK unified ideographs
				flushWord()
				tokens = append(tokens, string(r))
			} else {
				word.WriteRune(r)
			}
		} else {
			flushWord()
		}
	}
	flushWord()
	return tokens
}

// ComputeOutputRepetitionRate calculates the 4-gram repetition ratio (0.0 ~ 1.0)
// across all assistant text outputs (RespText and Reasoning) in a Journey.
func ComputeOutputRepetitionRate(j *Journey) float64 {
	if j == nil {
		return 0.0
	}

	var allTokens []string
	for _, t := range j.Tasks {
		for _, s := range t.Steps {
			if s.RespText != "" {
				allTokens = append(allTokens, tokenizeText(s.RespText)...)
			}
			if s.Reasoning != "" {
				allTokens = append(allTokens, tokenizeText(s.Reasoning)...)
			}
		}
	}

	const n = 4
	if len(allTokens) < n {
		return 0.0
	}

	totalGrams := len(allTokens) - n + 1
	seen := map[string]bool{}

	for i := 0; i <= len(allTokens)-n; i++ {
		gram := strings.Join(allTokens[i:i+n], " ")
		seen[gram] = true
	}

	uniqueGrams := len(seen)
	rate := 1.0 - (float64(uniqueGrams) / float64(totalGrams))
	if rate < 0.0 {
		return 0.0
	}
	if rate > 1.0 {
		return 1.0
	}
	return rate
}
