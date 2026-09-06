// Ver 2026-08-05, by Sonnet 5

// Direct unit tests for render_spine_args.go's helpers (scalarSummary, capFull).
package journey

import (
	"strings"
	"testing"

	"vmr/internal/i18n"
)

// --- scalarSummary --------------------------------------------------------

func TestScalarSummary(t *testing.T) {
	t.Run("short string: not big", func(t *testing.T) {
		s, big := scalarSummary("hello")
		if s != "hello" || big {
			t.Errorf("scalarSummary(short string) = (%q, %v), want (%q, false)", s, big, "hello")
		}
	})

	t.Run("long string (>spineShortFieldLen): big", func(t *testing.T) {
		long := strings.Repeat("a", spineShortFieldLen+1)
		s, big := scalarSummary(long)
		if s != long || !big {
			t.Errorf("scalarSummary(long string) = (len %d, big=%v), want (unchanged, true)", len(s), big)
		}
	})

	t.Run("multi-line string: big even if short overall", func(t *testing.T) {
		s, big := scalarSummary("a\nb")
		if s != "a\nb" || !big {
			t.Errorf("scalarSummary(multi-line string) = (%q, %v), want (%q, true)", s, big, "a\nb")
		}
	})

	t.Run("numbers: formatted, not big", func(t *testing.T) {
		s, big := scalarSummary(float64(42))
		if s != "42" || big {
			t.Errorf("scalarSummary(42.0) = (%q, %v), want (\"42\", false)", s, big)
		}
	})

	t.Run("booleans: formatted, not big", func(t *testing.T) {
		s, big := scalarSummary(true)
		if s != "true" || big {
			t.Errorf("scalarSummary(true) = (%q, %v), want (\"true\", false)", s, big)
		}
	})

	t.Run("nil: null, not big", func(t *testing.T) {
		s, big := scalarSummary(nil)
		if s != "null" || big {
			t.Errorf("scalarSummary(nil) = (%q, %v), want (\"null\", false)", s, big)
		}
	})

	t.Run("empty array: [0], big", func(t *testing.T) {
		s, big := scalarSummary([]any{})
		if s != "[0]" || !big {
			t.Errorf("scalarSummary(empty array) = (%q, %v), want (\"[0]\", true)", s, big)
		}
	})

	t.Run("string array: count + first item, big", func(t *testing.T) {
		s, big := scalarSummary([]any{"first", "second"})
		if s != "[2] first" || !big {
			t.Errorf("scalarSummary(string array) = (%q, %v), want (\"[2] first\", true)", s, big)
		}
	})

	t.Run("object array with labeled step: count + first step, big", func(t *testing.T) {
		s, big := scalarSummary([]any{map[string]any{"step": "do the thing"}, map[string]any{"step": "do another"}})
		if s != "[2] do the thing" || !big {
			t.Errorf("scalarSummary(object array) = (%q, %v), want (\"[2] do the thing\", true)", s, big)
		}
	})

	t.Run("object array without recognized label: [N], big", func(t *testing.T) {
		s, big := scalarSummary([]any{map[string]any{"unrelated_key": "x"}})
		if s != "[1]" || !big {
			t.Errorf("scalarSummary(unlabeled object array) = (%q, %v), want (\"[1]\", true)", s, big)
		}
	})

	t.Run("object: {N}, big", func(t *testing.T) {
		s, big := scalarSummary(map[string]any{"a": 1, "b": 2})
		if s != "{2}" || !big {
			t.Errorf("scalarSummary(object) = (%q, %v), want (\"{2}\", true)", s, big)
		}
	})
}

// --- capFull ---------------------------------------------------------------

func TestCapFull(t *testing.T) {
	et := i18n.Spine(i18n.EN)

	t.Run("within cap: unchanged", func(t *testing.T) {
		s := strings.Repeat("x", spineFullCap)
		if got := capFull(s, et); got != s {
			t.Error("capFull should not alter a value exactly at the cap")
		}
	})
	t.Run("over cap: truncated with a localized note naming how much more", func(t *testing.T) {
		s := strings.Repeat("x", spineFullCap+37)
		got := capFull(s, et)
		wantPrefix := strings.Repeat("x", spineFullCap)
		if !strings.HasPrefix(got, wantPrefix) {
			t.Errorf("capFull(over-cap) should keep the first spineFullCap runes unchanged")
		}
		if !strings.Contains(got, "37") {
			t.Errorf("capFull(over-cap) = %q, want it to name the exact overage (37)", got)
		}
		if strings.HasSuffix(got, "x") {
			t.Errorf("capFull(over-cap) = %q, must append a truncation note, not just cut silently", got)
		}
	})
}
