// Ver 2026-07-26, by Sonnet 5
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"vmr/internal/fmtutil"
)

func TestResolveCurrencyFactors(t *testing.T) {
	// {USD:1, CNY:7} means 1 USD = 7 CNY; {JPY:100, CNY:1} means 100 JPY = 1
	// CNY (1 JPY = 0.01 CNY). Neither entry connects JPY directly to USD —
	// the BFS must chain through CNY.
	pairs := []map[string]float64{
		{"USD": 1, "CNY": 7},
		{"JPY": 100, "CNY": 1},
	}
	factors := resolveCurrencyFactors("CNY", pairs)
	if factors["CNY"] != 1 {
		t.Fatalf("base currency factor should be 1, got %v", factors["CNY"])
	}
	if got := factors["USD"]; got != 7 {
		t.Fatalf("USD factor want 7, got %v", got)
	}
	if got := factors["JPY"]; got != 0.01 {
		t.Fatalf("JPY factor want 0.01, got %v", got)
	}
	// case-insensitive keys
	pairs2 := []map[string]float64{{"usd": 1, "cny": 7}}
	factors2 := resolveCurrencyFactors("CNY", pairs2)
	if factors2["USD"] != 7 {
		t.Fatalf("exchange_rate currency codes should be case-insensitive, got %v", factors2)
	}
}

func TestMoneyValueParsing(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		wantCur string
		wantAmt float64
		wantErr bool
	}{
		{"1.2", "", 1.2, false},
		{"USD1.2", "USD", 1.2, false},
		{"usd1.2", "USD", 1.2, false},
		{"jpy 1.2", "JPY", 1.2, false},
		{"  CNY  0.28  ", "CNY", 0.28, false},
		{"0", "", 0, false},
		{"-1.2", "", -1.2, false},
		{"not-a-price", "", 0, true},
		{"USD", "", 0, true}, // currency with no number is invalid
	} {
		var wrap struct {
			V moneyValue `yaml:"v"`
		}
		err := yaml.Unmarshal([]byte(fmt.Sprintf("v: %q\n", tc.raw)), &wrap)
		m := wrap.V
		if tc.wantErr {
			if err == nil {
				t.Errorf("%q: want error, got amount=%v currency=%q", tc.raw, m.amount, m.currency)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.raw, err)
			continue
		}
		if m.amount != tc.wantAmt || m.currency != tc.wantCur {
			t.Errorf("%q: got amount=%v currency=%q, want amount=%v currency=%q",
				tc.raw, m.amount, m.currency, tc.wantAmt, tc.wantCur)
		}
	}
}

func TestLoadPricingCurrencyConversion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.yaml")
	yaml := `currency: CNY
exchange_rate:
  - {USD: 1, CNY: 7}
  - {JPY: 100, CNY: 1}
rates:
  - provider: deepseek
    model: deepseek-v4-flash
    in_fresh_per_1m: USD1
    cache_read_per_1m: 0.02
    cache_write_per_1m: 0
    out_per_1m: jpy 200
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPricing(path)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := p.RateFor("deepseek", "deepseek-v4-flash", time.Now())
	if !ok {
		t.Fatal("rate not found")
	}
	if r.InFreshPer1M != 7 { // USD1 * 7
		t.Errorf("in_fresh_per_1m want 7 (USD1 -> CNY), got %v", r.InFreshPer1M)
	}
	if r.CacheReadPer1M != 0.02 { // bare number, already CNY
		t.Errorf("cache_read_per_1m want 0.02 (bare, no conversion), got %v", r.CacheReadPer1M)
	}
	if r.OutPer1M != 2 { // jpy 200 * 0.01
		t.Errorf("out_per_1m want 2 (JPY200 -> CNY), got %v", r.OutPer1M)
	}
}

func TestLoadPricingUndefinedCurrencyErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.yaml")
	yaml := `currency: CNY
rates:
  - provider: deepseek
    model: deepseek-v4-flash
    in_fresh_per_1m: EUR1.2
    cache_read_per_1m: 0
    cache_write_per_1m: 0
    out_per_1m: 0
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPricing(path)
	if err == nil {
		t.Fatal("want error for a currency not defined in exchange_rate")
	}
	if !strings.Contains(err.Error(), "EUR") {
		t.Errorf("error should name the offending currency, got: %v", err)
	}
}

func TestPricingRateTimeWindows(t *testing.T) {
	// matches() interprets DateFrom/DateTo/HourFrom/HourTo in
	// fmtutil.DisplayZone — pin it to UTC (matching this test's at() helper)
	// so the assertions below don't depend on the host machine's real
	// timezone.
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.yaml")
	// Two rules for the same provider+model: an off-peak discount
	// (22:00..06:00, wraps midnight) listed first, then a full-price
	// catch-all with no window — first-match-wins means the discount only
	// applies inside its own window.
	yaml := `currency: CNY
rates:
  - provider: volcengine
    model: ark-code-latest
    in_fresh_per_1m: 0.5
    cache_read_per_1m: 0
    cache_write_per_1m: 0
    out_per_1m: 4
    hour_range: ["22:00", "06:00"]
  - provider: volcengine
    model: ark-code-latest
    in_fresh_per_1m: 1.2
    cache_read_per_1m: 0
    cache_write_per_1m: 0
    out_per_1m: 8
  - provider: deepseek
    model: deepseek-v4-pro
    in_fresh_per_1m: 3
    cache_read_per_1m: 0
    cache_write_per_1m: 0
    out_per_1m: 6
    date_range: ["2026-08-01", "2026-08-31"]
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPricing(path)
	if err != nil {
		t.Fatal(err)
	}

	at := func(y, mo, d, h, mi int) time.Time { return time.Date(y, time.Month(mo), d, h, mi, 0, 0, time.UTC) }

	// 23:00 falls inside the wrapping 22:00..06:00 window -> discount rate.
	if r, ok := p.RateFor("volcengine", "ark-code-latest", at(2026, 7, 26, 23, 0)); !ok || r.InFreshPer1M != 0.5 {
		t.Errorf("23:00 should hit the off-peak rate (0.5), got ok=%v rate=%+v", ok, r)
	}
	// 10:00 falls outside the wrapping window -> falls through to the
	// catch-all full-price rule.
	if r, ok := p.RateFor("volcengine", "ark-code-latest", at(2026, 7, 26, 10, 0)); !ok || r.InFreshPer1M != 1.2 {
		t.Errorf("10:00 should hit the catch-all rate (1.2), got ok=%v rate=%+v", ok, r)
	}
	// exact boundary: 22:00 and 06:00 are both inside the window.
	if r, ok := p.RateFor("volcengine", "ark-code-latest", at(2026, 7, 26, 22, 0)); !ok || r.InFreshPer1M != 0.5 {
		t.Errorf("22:00 boundary should hit the off-peak rate, got ok=%v rate=%+v", ok, r)
	}
	if r, ok := p.RateFor("volcengine", "ark-code-latest", at(2026, 7, 26, 6, 0)); !ok || r.InFreshPer1M != 0.5 {
		t.Errorf("06:00 boundary should hit the off-peak rate, got ok=%v rate=%+v", ok, r)
	}

	// date_range: only August 2026 has a rate for this provider+model.
	if _, ok := p.RateFor("deepseek", "deepseek-v4-pro", at(2026, 7, 26, 12, 0)); ok {
		t.Errorf("July should not match an August-only date_range")
	}
	if r, ok := p.RateFor("deepseek", "deepseek-v4-pro", at(2026, 8, 15, 12, 0)); !ok || r.InFreshPer1M != 3 {
		t.Errorf("mid-August should match the date_range rate, got ok=%v rate=%+v", ok, r)
	}

	// unknown provider/model -> no match at all.
	if _, ok := p.RateFor("nope", "nope", time.Now()); ok {
		t.Errorf("unconfigured provider/model should never match")
	}

	// case-insensitive provider/model lookup.
	if _, ok := p.RateFor("VolcEngine", "ARK-Code-Latest", at(2026, 7, 26, 23, 0)); !ok {
		t.Errorf("provider/model lookup should be case-insensitive")
	}
}

// TestPricingRateMatchesConvertsToDisplayZone proves matches() interprets a
// timestamp in fmtutil.DisplayZone rather than trusting the time.Time's own
// embedded offset — a record written from a different timezone than the
// machine running `vmr report` must still land in the correct hour/date
// window. Constructed so the two interpretations disagree: 23:00 in a
// fixed +05:00 zone is 18:00 UTC, which falls outside the 22:00..06:00
// off-peak window a naive (non-converting) read of the record's own "23:00"
// would have matched.
func TestPricingRateMatchesConvertsToDisplayZone(t *testing.T) {
	origZone := fmtutil.DisplayZone
	fmtutil.DisplayZone = time.UTC
	defer func() { fmtutil.DisplayZone = origZone }()

	rate := PricingRate{HourFrom: "22:00", HourTo: "06:00"}

	plusFive := time.FixedZone("+05:00", 5*3600)
	tsLocalLooking := time.Date(2026, 7, 26, 23, 0, 0, 0, plusFive) // == 18:00 UTC
	if rate.matches(tsLocalLooking) {
		t.Errorf("matches() should convert to DisplayZone (UTC) before comparing — 23:00+05:00 is 18:00 UTC, outside 22:00..06:00")
	}

	tsUTC := time.Date(2026, 7, 26, 23, 0, 0, 0, time.UTC)
	if !rate.matches(tsUTC) {
		t.Errorf("23:00 UTC should match the 22:00..06:00 window when DisplayZone is UTC")
	}
}

func TestLoadPricingInvalidDateHourRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pricing.yaml")
	yaml := `currency: CNY
rates:
  - provider: deepseek
    model: deepseek-v4-flash
    in_fresh_per_1m: 1
    cache_read_per_1m: 0
    cache_write_per_1m: 0
    out_per_1m: 1
    hour_range: ["22:00"]
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPricing(path); err == nil {
		t.Fatal("want error: hour_range must have exactly 2 entries")
	}
}
