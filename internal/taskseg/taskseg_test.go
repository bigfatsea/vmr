// Ver 2026-08-14, by Sonnet 5
package taskseg

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestCapStrRuneSafe locks in that CapStr's byte cap never cuts through a
// UTF-8 sequence — Chinese/emoji session titles and compaction needles near
// the cap must stay valid UTF-8. Moved from internal/report/session_test.go
// alongside CapStr's own implementation (architecture review's B2 batch).
func TestCapStrRuneSafe(t *testing.T) {
	s := strings.Repeat("审", 100) // 3 bytes per rune
	for n := 0; n <= 12; n++ {
		got := CapStr(s, n)
		if !utf8.ValidString(got) {
			t.Errorf("CapStr(…, %d) produced invalid UTF-8: %q", n, got)
		}
		if len(got) > n {
			t.Errorf("CapStr(…, %d) exceeded the byte cap: %d bytes", n, len(got))
		}
	}
	if got := CapStr("ascii only", 200); got != "ascii only" {
		t.Errorf("short string must be returned whole: %q", got)
	}
}
