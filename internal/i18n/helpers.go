// Ver 2026-08-01, by Sonnet 5

// Tiny numeric-formatting helpers shared by this package's text files —
// language-neutral (digits are digits in both en/zh), so they don't belong
// to any one section's *Text struct; kept here instead of duplicated in
// every file that needs them.
package i18n

import "strconv"

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// pad2 zero-pads an hour-of-day (0..23) to two digits ("05", "14").
func pad2(n int) string {
	s := strconv.Itoa(n)
	if len(s) < 2 {
		return "0" + s
	}
	return s
}

// fmtPct0 formats a 0..100 percentage with zero decimal places ("42").
func fmtPct0(pct float64) string {
	return strconv.FormatFloat(pct, 'f', 0, 64)
}
