// Ver 2026-08-22 22:48, by Gemini

// Package tokenutil provides fast, zero-allocation token count estimation
// for raw bytes and text without performing actual heavy tokenization.
//
// It uses a linear regression model based on character classification:
//
//	tokens ≈ 0.488 * EnglishSymbols
//	       + 0.206 * EnglishLetters
//	       + 0.746 * Digits
//	       + 0.507 * CJKChars
//	       + 0.043 * Spaces
//	       + 1.830 * OtherChars
package tokenutil

import (
	"math"
	"unicode"
	"unicode/utf8"
)

// CharStats holds counts of characters categorized by script/type.
type CharStats struct {
	EnglishSymbols int64
	EnglishLetters int64
	Digits         int64
	CJKChars       int64
	Spaces         int64
	OtherChars     int64
}

// Add folds other into cs in place — for callers that tally CharStats across
// multiple chunks (e.g. a streamed response read incrementally) and must
// apply EstimateFromStats' rounding exactly once, over the combined total,
// rather than once per chunk.
func (cs *CharStats) Add(other CharStats) {
	cs.EnglishSymbols += other.EnglishSymbols
	cs.EnglishLetters += other.EnglishLetters
	cs.Digits += other.Digits
	cs.CJKChars += other.CJKChars
	cs.Spaces += other.Spaces
	cs.OtherChars += other.OtherChars
}

// IsEnglishSymbol reports whether r is an ASCII punctuation or symbol character.
func IsEnglishSymbol(r rune) bool {
	return (r >= 0x21 && r <= 0x2F) || // !"#$%&'()*+,-./
		(r >= 0x3A && r <= 0x40) || // :;<=>?@
		(r >= 0x5B && r <= 0x60) || // [\]^_`
		(r >= 0x7B && r <= 0x7E) // {|}~
}

// IsCJK reports whether r is a CJK ideograph, Japanese kana, or Korean hangul rune.
func IsCJK(r rune) bool {
	if (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Extension B
		(r >= 0x2A700 && r <= 0x2B73F) || // CJK Extension C
		(r >= 0x2B740 && r <= 0x2B81F) || // CJK Extension D
		(r >= 0x2B820 && r <= 0x2CEAF) || // CJK Extension E
		(r >= 0x2CEB0 && r <= 0x2EBEF) || // CJK Extension F
		(r >= 0x30000 && r <= 0x3134F) || // CJK Extension G
		(r >= 0xF900 && r <= 0xFAFF) { // CJK Compatibility Ideographs
		return true
	}
	if (r >= 0x3040 && r <= 0x309F) || (r >= 0x30A0 && r <= 0x30FF) { // Japanese Hiragana / Katakana
		return true
	}
	if (r >= 0xAC00 && r <= 0xD7AF) || // Korean Hangul Syllables
		(r >= 0x1100 && r <= 0x11FF) || // Hangul Jamo
		(r >= 0x3130 && r <= 0x318F) || // Hangul Compatibility Jamo
		(r >= 0xA960 && r <= 0xA97F) ||
		(r >= 0xD7B0 && r <= 0xD7FF) {
		return true
	}
	// CJK Symbols and Punctuation (、。「」『』〈〉《》【】 and friends) plus
	// Halfwidth/Fullwidth Forms (fullwidth ，！？：（） etc., the punctuation
	// CJK text overwhelmingly uses instead of the ASCII forms) — without this,
	// every full-width punctuation mark in ordinary Chinese/Japanese text
	// falls into OtherChars, the single highest-weighted category, and
	// systematically inflates the estimate for exactly the text this split
	// exists to handle.
	if (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF) {
		return true
	}
	return false
}

// Analyze counts character categories in body rune-by-rune with zero heap allocations.
func Analyze(body []byte) CharStats {
	var stats CharStats
	for len(body) > 0 {
		r, size := utf8.DecodeRune(body)
		body = body[size:]
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			stats.EnglishLetters++
		case r >= '0' && r <= '9':
			stats.Digits++
		case IsEnglishSymbol(r):
			stats.EnglishSymbols++
		case IsCJK(r):
			stats.CJKChars++
		case unicode.IsSpace(r):
			stats.Spaces++
		default:
			stats.OtherChars++
		}
	}
	return stats
}

// AnalyzeString counts character categories in s rune-by-rune with zero heap allocations.
func AnalyzeString(s string) CharStats {
	var stats CharStats
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			stats.EnglishLetters++
		case r >= '0' && r <= '9':
			stats.Digits++
		case IsEnglishSymbol(r):
			stats.EnglishSymbols++
		case IsCJK(r):
			stats.CJKChars++
		case unicode.IsSpace(r):
			stats.Spaces++
		default:
			stats.OtherChars++
		}
	}
	return stats
}

// Estimate returns the estimated token count for the given raw bytes
// using the linear regression character classification formula.
func Estimate(body []byte) int64 {
	stats := Analyze(body)
	return EstimateFromStats(stats)
}

// EstimateText returns the estimated token count for the given string.
func EstimateText(s string) int64 {
	return EstimateFromStats(AnalyzeString(s))
}

// EstimateFromStats applies the linear regression formula to pre-tallied
// character category counts.
func EstimateFromStats(stats CharStats) int64 {
	val := 0.488*float64(stats.EnglishSymbols) +
		0.206*float64(stats.EnglishLetters) +
		0.746*float64(stats.Digits) +
		0.507*float64(stats.CJKChars) +
		0.043*float64(stats.Spaces) +
		1.830*float64(stats.OtherChars)
	return int64(math.Round(val))
}
