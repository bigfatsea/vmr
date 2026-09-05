// Ver 2026-09-05, by Pkg-A
package config

import (
	"strings"
	"testing"
	"time"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
	_ "vmr/internal/adapter/openairesponses"
)

// mustParseTTL parses a config with just a ttl: block spliced into the
// standard fixture.
func mustParseTTL(t *testing.T, ttl string) *Config {
	t.Helper()
	return mustParse(t, strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\n"+ttl, 1))
}

// TestCalendarDurationUnits locks the fixed-length unit table: d=24h, w=7d,
// mo=30d, y=365d — calendar approximations, the same convention quota's
// every: 1mo uses, not calendar-aware month math.
func TestCalendarDurationUnits(t *testing.T) {
	cases := map[string]time.Duration{
		"14d": 14 * 24 * time.Hour,
		"2w":  14 * 24 * time.Hour,
		"3mo": 90 * 24 * time.Hour,
		"1y":  365 * 24 * time.Hour,
		"0":   0,
		"0d":  0,
		"7":   7 * 24 * time.Hour, // bare integer = days (the old *_days spelling)
	}
	for in, want := range cases {
		got, err := parseCalendarDuration(in)
		if err != nil {
			t.Errorf("parseCalendarDuration(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseCalendarDuration(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestCalendarDurationCaseInsensitive: units accept any casing.
func TestCalendarDurationCaseInsensitive(t *testing.T) {
	for _, in := range []string{"14D", "2W", "3MO", "3Mo", "1Y"} {
		if _, err := parseCalendarDuration(in); err != nil {
			t.Errorf("parseCalendarDuration(%q): %v", in, err)
		}
	}
	if d, _ := parseCalendarDuration("3MO"); d != 90*24*time.Hour {
		t.Errorf("3MO = %v, want 90d", d)
	}
}

// TestCalendarDurationRejectsForeverKeywords: permanent-retention semantics
// were removed outright — the keywords must be load errors naming the
// concrete-large-value migration, not silent accepted synonyms for "huge".
func TestCalendarDurationRejectsForeverKeywords(t *testing.T) {
	for _, in := range []string{"forever", "permanent", "never", "FOREVER", "Never"} {
		_, err := parseCalendarDuration(in)
		if err == nil {
			t.Errorf("parseCalendarDuration(%q) accepted; want a rejection", in)
			continue
		}
		if !strings.Contains(err.Error(), "90000d") {
			t.Errorf("parseCalendarDuration(%q) error %v should name the 90000d migration", in, err)
		}
	}
}

// TestCalendarDurationRejectsOtherGarbage: Go-grammar units (m/h/s) are not
// this grammar, and a bare suffix-less word is not either.
func TestCalendarDurationRejectsOtherGarbage(t *testing.T) {
	for _, in := range []string{"5m", "2h", "90s", "d", "mo", "-d", "1.5d", ""} {
		if _, err := parseCalendarDuration(in); err == nil {
			t.Errorf("parseCalendarDuration(%q) accepted; want a rejection", in)
		}
	}
}

// TestTTLZeroAndNegativeUseDefaults pins the uniform ttl.* zero-value
// polarity: 0 (incl. 0d), absent, and negative all mean "use the default" —
// applyDefaults fills any value <= 0, and there is no third reading.
func TestTTLZeroAndNegativeUseDefaults(t *testing.T) {
	wantSticky := DefaultStickyTTL
	wantImage := time.Duration(DefaultImageCacheTTLDays) * 24 * time.Hour
	wantAudit := time.Duration(DefaultAuditRetentionDays) * 24 * time.Hour

	// Absent entirely.
	cfg := mustParseTTL(t, "")
	if cfg.TTL.Sticky.D() != wantSticky || cfg.TTL.ImageCache.D() != wantImage || cfg.TTL.AuditRetention.D() != wantAudit {
		t.Errorf("absent ttl block: sticky=%v image_cache=%v audit_retention=%v, want defaults %v/%v/%v",
			cfg.TTL.Sticky.D(), cfg.TTL.ImageCache.D(), cfg.TTL.AuditRetention.D(), wantSticky, wantImage, wantAudit)
	}

	// Explicit zero, mixed forms.
	cfg = mustParseTTL(t, "ttl:\n  sticky: 0s\n  image_cache: 0d\n  audit_retention: 0")
	if cfg.TTL.Sticky.D() != wantSticky || cfg.TTL.ImageCache.D() != wantImage || cfg.TTL.AuditRetention.D() != wantAudit {
		t.Errorf("explicit zeros: sticky=%v image_cache=%v audit_retention=%v, want defaults", cfg.TTL.Sticky.D(), cfg.TTL.ImageCache.D(), cfg.TTL.AuditRetention.D())
	}

	// Negatives are not rejected — they take the default like any other <=0.
	cfg = mustParseTTL(t, "ttl:\n  sticky: -5m\n  image_cache: -5d\n  audit_retention: -5d")
	if cfg.TTL.Sticky.D() != wantSticky || cfg.TTL.ImageCache.D() != wantImage || cfg.TTL.AuditRetention.D() != wantAudit {
		t.Errorf("negatives: sticky=%v image_cache=%v audit_retention=%v, want defaults", cfg.TTL.Sticky.D(), cfg.TTL.ImageCache.D(), cfg.TTL.AuditRetention.D())
	}
}

// TestTTLAuditRetentionNeverMeansForever pins the breaking change: 0 used to
// mean "never delete audit files"; it must now land on the finite default.
func TestTTLAuditRetentionNeverMeansForever(t *testing.T) {
	cfg := mustParseTTL(t, "ttl:\n  audit_retention: 0d")
	if cfg.TTL.AuditRetention.D() != time.Duration(DefaultAuditRetentionDays)*24*time.Hour {
		t.Errorf("audit_retention: 0d = %v, want the %dd default (the old \"0 = never delete\" reading is gone)", cfg.TTL.AuditRetention.D(), DefaultAuditRetentionDays)
	}
}

// TestTTLStickyAboveBackstopRejected: ttl.sticky is a plain Go-grammar
// Duration (minutes/hours scale) but keeps the same memory-eviction backstop
// constraint the old top-level sticky_ttl had.
func TestTTLStickyAboveBackstopRejected(t *testing.T) {
	_, err := Parse([]byte(strings.Replace(validYAML, "listen: 127.0.0.1:9900",
		"listen: 127.0.0.1:9900\nttl:\n  sticky: 25h", 1)))
	if err == nil || !strings.Contains(err.Error(), "ttl.sticky") {
		t.Errorf("ttl.sticky above the 24h backstop must be rejected naming ttl.sticky, got %v", err)
	}
}

// TestTimeoutsProbeDefault pins the relocated probe default: probe lives
// under timeouts: now, and an unset value still resolves to
// DefaultProbeTimeout (<=0 values included — same polarity as its siblings).
func TestTimeoutsProbeDefault(t *testing.T) {
	cfg := mustParse(t, validYAML)
	if cfg.Timeouts.Probe.D() != DefaultProbeTimeout {
		t.Errorf("timeouts.probe default = %v, want %v", cfg.Timeouts.Probe.D(), DefaultProbeTimeout)
	}
	cfg = mustParseTTL(t, "timeouts:\n  probe: 0s")
	if cfg.Timeouts.Probe.D() != DefaultProbeTimeout {
		t.Errorf("timeouts.probe 0s = %v, want default %v", cfg.Timeouts.Probe.D(), DefaultProbeTimeout)
	}
}

// TestLegacyTopLevelTimeFieldsRejected pins the field gathering: the four
// removed top-level keys are unknown fields now, not silently-accepted
// aliases — a config still using them fails to load.
func TestLegacyTopLevelTimeFieldsRejected(t *testing.T) {
	for _, key := range []string{"probe_timeout: 15s", "sticky_ttl: 10m", "image_cache_ttl_days: 7", "audit_retention_days: 30"} {
		_, err := Parse([]byte(strings.Replace(validYAML, "listen: 127.0.0.1:9900",
			"listen: 127.0.0.1:9900\n"+key, 1)))
		if err == nil || !strings.Contains(err.Error(), strings.SplitN(key, ":", 2)[0]) {
			t.Errorf("legacy top-level key %q must be a load error, got %v", key, err)
		}
	}
}

// TestCalendarDurationDaysRoundsUp: the day-granular consumers
// (audit.SetRetentionDays, imgprep's CacheTTLDays) both keep a legacy
// "0 = never delete / disabled" reading, so a sub-day config must convert to
// at least one whole day, never truncate to 0.
func TestCalendarDurationDaysRoundsUp(t *testing.T) {
	cases := map[CalendarDuration]int{
		CalendarDuration(90 * 24 * time.Hour):  90,
		CalendarDuration(12 * time.Hour):       1,
		CalendarDuration(25 * time.Hour):       2,
		CalendarDuration(1 * time.Hour):        1,
		CalendarDuration(3 * 24 * time.Hour):   3,
		CalendarDuration(30 * 24 * time.Hour):  30,
		CalendarDuration(365 * 24 * time.Hour): 365,
	}
	for d, want := range cases {
		if got := d.Days(); got != want {
			t.Errorf("CalendarDuration(%v).Days() = %d, want %d", time.Duration(d), got, want)
		}
	}
}

// TestCalendarDurationString: whole days render as "Nd" (the check-output
// form); anything else falls back to Go's duration spelling so 12h doesn't
// read as the misleading "0d".
func TestCalendarDurationString(t *testing.T) {
	cases := map[CalendarDuration]string{
		CalendarDuration(90 * 24 * time.Hour): "90d",
		CalendarDuration(14 * 24 * time.Hour): "14d",
		CalendarDuration(0):                   "0d",
		CalendarDuration(12 * time.Hour):      "12h0m0s",
	}
	for d, want := range cases {
		if got := d.String(); got != want {
			t.Errorf("CalendarDuration(%v).String() = %q, want %q", time.Duration(d), got, want)
		}
	}
}
