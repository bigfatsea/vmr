// Ver 2026-09-07, by Claude

package report

import (
	"encoding/json"
	"testing"
	"time"
)

// TestSliceTimePoints_CarryBothForms is §9's dual-time guard: every slice
// field that carries an absolute instant the browser dashboard renders must
// emit BOTH a machine form (epoch ms) and a DisplayZone-formatted string, so
// the frontend never does timezone math (§5.6, §11.1 #4). Bucket labels
// (Row.Date / HourRow.Date+Hour / EndpointRow.Date) are display-ready
// strings/ints with a natural sort order and are intentionally out of scope.
//
// Fields consumed only by Go-side markdown rendering (SessionRow.from/to,
// Meta.time_range, ProviderQuotaRow.Period*) render through
// fmtutil.DisplayZone in the renderer and are not read by the browser, so
// they are not required to carry a companion here — if a future dashboard
// view renders one of them, add the companion and extend this test.
func TestSliceTimePoints_CarryBothForms(t *testing.T) {
	sample := time.Date(2026, 8, 24, 3, 15, 42, 0, time.UTC)

	assertBothForms := func(t *testing.T, raw []byte, msKey, displayKey string) {
		t.Helper()
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		msRaw, ok := m[msKey]
		if !ok {
			t.Fatalf("missing machine-form key %q in %s", msKey, raw)
		}
		var ms int64
		if err := json.Unmarshal(msRaw, &ms); err != nil {
			t.Errorf("machine-form key %q is not a number (%s) — §5.6 wants epoch ms", msKey, msRaw)
		}
		dispRaw, ok := m[displayKey]
		if !ok {
			t.Fatalf("missing display-form key %q in %s", displayKey, raw)
		}
		var disp string
		if err := json.Unmarshal(dispRaw, &disp); err != nil || disp == "" {
			t.Errorf("display-form key %q is not a non-empty string (%s)", displayKey, dispRaw)
		}
	}

	t.Run("RequestRow", func(t *testing.T) {
		rr := RequestRow{TS: sample.UnixMilli(), TSDisplay: "2026-08-24 11:15:42", Outcome: "ok"}
		raw, err := json.Marshal(rr)
		if err != nil {
			t.Fatal(err)
		}
		assertBothForms(t, raw, "ts", "ts_display")
	})

	t.Run("CompactionRow", func(t *testing.T) {
		cr := CompactionRow{TSMS: sample.UnixMilli()}
		raw, err := json.Marshal(cr)
		if err != nil {
			t.Fatal(err)
		}
		assertBothForms(t, raw, "ts", "ts_display")
	})

	t.Run("manifest.generated_at", func(t *testing.T) {
		tp := NewTimePoint(sample)
		raw, err := json.Marshal(tp)
		if err != nil {
			t.Fatal(err)
		}
		assertBothForms(t, raw, "ts", "ts_display")
	})

	// buildRequestRow must never regress RequestRow.ts back to a string.
	t.Run("buildRequestRow emits epoch ms", func(t *testing.T) {
		rc := &rec2{ts: sample, outcome: "ok"}
		rr := buildRequestRow(rc)
		if rr.TS != sample.UnixMilli() {
			t.Errorf("RequestRow.TS = %d, want %d (epoch ms)", rr.TS, sample.UnixMilli())
		}
		if rr.TSDisplay == "" {
			t.Error("RequestRow.TSDisplay is empty")
		}
	})
}
