// Ver 2026-08-22 22:48, by Gemini

package tokenutil

import (
	"testing"
)

func TestEstimate(t *testing.T) {
	t.Parallel()

	if got := Estimate(nil); got != 0 {
		t.Errorf("empty input = %d, want 0", got)
	}

	// 10 English letters: 10 * 0.206 = 2.06 -> 2
	if got := Estimate([]byte("abcdefghij")); got != 2 {
		t.Errorf("english letters = %d, want 2", got)
	}

	// 10 Digits: 10 * 0.746 = 7.46 -> 7
	if got := Estimate([]byte("0123456789")); got != 7 {
		t.Errorf("digits = %d, want 7", got)
	}

	// 10 CJK characters: 10 * 0.507 = 5.07 -> 5
	if got := Estimate([]byte("一二三四五六七八九十")); got != 5 {
		t.Errorf("cjk = %d, want 5", got)
	}

	// 10 English symbols: 10 * 0.488 = 4.88 -> 5
	if got := Estimate([]byte("!@#$%^&*()")); got != 5 {
		t.Errorf("symbols = %d, want 5", got)
	}

	// 10 Spaces: 10 * 0.043 = 0.43 -> 0
	if got := Estimate([]byte("          ")); got != 0 {
		t.Errorf("spaces = %d, want 0", got)
	}

	// Emoji (OtherChars): 2 emojis * 1.830 = 3.66 -> 4
	if got := Estimate([]byte("🚀🎉")); got != 4 {
		t.Errorf("emojis = %d, want 4", got)
	}

	// EstimateText wrapper: 'H','e','l','l','o' -> 5 letters (1.03); ',' '!' ->
	// 2 symbols (0.976); ' ',' ' -> 2 spaces (0.086); '世','界' -> 2 CJK
	// (1.014); '1','2','3' -> 3 digits (2.238). Total 5.344 -> 5.
	if got := EstimateText("Hello, 世界! 123"); got != 5 {
		t.Errorf("EstimateText = %d, want 5", got)
	}
}

// TestIsCJK_FullwidthPunctuation locks in that CJK Symbols/Punctuation
// (U+3000-303F) and Halfwidth/Fullwidth Forms (U+FF00-FFEF) — the
// full-width ，。！？（） etc. ordinary Chinese/Japanese text actually uses —
// classify as CJK, not OtherChars. Getting this wrong silently inflates
// every estimate over real Chinese text: OtherChars carries the highest
// weight of all six categories (1.830 vs CJK's 0.507).
func TestIsCJK_FullwidthPunctuation(t *testing.T) {
	t.Parallel()

	stats := Analyze([]byte("你好，世界！"))
	if stats.OtherChars != 0 {
		t.Errorf("OtherChars = %d, want 0 (fullwidth ，！ must classify as CJK)", stats.OtherChars)
	}
	if stats.CJKChars != 6 {
		t.Errorf("CJKChars = %d, want 6", stats.CJKChars)
	}
}

// TestCharStats_Add_MatchesSinglePassAnalyze locks in the invariant
// respnorm's incremental countTokens relies on: summing CharStats across
// arbitrarily split chunks and rounding once (EstimateFromStats) must equal
// running Analyze/Estimate over the whole concatenated input in one pass —
// unlike rounding per chunk and summing the rounded results, which drifts
// with chunk size (see internal/respnorm's countTokens doc comment).
func TestCharStats_Add_MatchesSinglePassAnalyze(t *testing.T) {
	t.Parallel()

	text := "Hello, 世界! 123 你好，测试 🚀🎉 more text here to pad it out a bit."
	whole := Analyze([]byte(text))

	var acc CharStats
	for _, r := range text { // one rune per "chunk", the worst case for fragmentation
		acc.Add(Analyze([]byte(string(r))))
	}

	if acc != whole {
		t.Errorf("accumulated CharStats = %+v, want %+v (single-pass Analyze)", acc, whole)
	}
	if got, want := EstimateFromStats(acc), EstimateFromStats(whole); got != want {
		t.Errorf("EstimateFromStats(accumulated) = %d, want %d", got, want)
	}
}

func TestAnalyze(t *testing.T) {
	t.Parallel()

	stats := Analyze([]byte("Hi! 123 你好 🚀"))
	if stats.EnglishLetters != 2 { // 'H', 'i'
		t.Errorf("letters = %d, want 2", stats.EnglishLetters)
	}
	if stats.EnglishSymbols != 1 { // '!'
		t.Errorf("symbols = %d, want 1", stats.EnglishSymbols)
	}
	if stats.Digits != 3 { // '1', '2', '3'
		t.Errorf("digits = %d, want 3", stats.Digits)
	}
	if stats.CJKChars != 2 { // '你', '好'
		t.Errorf("cjk = %d, want 2", stats.CJKChars)
	}
	if stats.Spaces != 3 { // 3 spaces
		t.Errorf("spaces = %d, want 3", stats.Spaces)
	}
	if stats.OtherChars != 1 { // '🚀'
		t.Errorf("others = %d, want 1", stats.OtherChars)
	}
}

func TestAnalyzeString_MatchesAnalyze(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"Hello, World!",
		"0123456789",
		"你好，世界！",
		"Hi! 123 你好 🚀🎉",
		"混合 text with CJK 汉字, digits 456, symbols !@#, spaces   and emojis 🌟.",
	}
	for _, text := range cases {
		fromBytes := Analyze([]byte(text))
		fromString := AnalyzeString(text)
		if fromBytes != fromString {
			t.Errorf("AnalyzeString(%q) = %+v, want %+v (Analyze)", text, fromString, fromBytes)
		}
		if estBytes, estStr := Estimate([]byte(text)), EstimateText(text); estBytes != estStr {
			t.Errorf("EstimateText(%q) = %d, want %d (Estimate)", text, estStr, estBytes)
		}
	}
}

func TestEstimateText_ZeroAlloc(t *testing.T) {
	s := "Hello, 世界! 123 你好，测试 🚀🎉 more text here to verify zero heap allocations."
	allocs := testing.AllocsPerRun(100, func() {
		_ = EstimateText(s)
	})
	if allocs != 0 {
		t.Errorf("EstimateText allocated %v allocs/op, want 0", allocs)
	}
}

func BenchmarkEstimateText(b *testing.B) {
	s := "Hello, 世界! 123 你好，测试 🚀🎉 more text here to test token estimation performance."
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EstimateText(s)
	}
}
