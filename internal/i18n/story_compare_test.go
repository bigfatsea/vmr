// Ver 2026-08-13, by Opus 5
package i18n

import "testing"

// TestMetricLabel_ModelSwitchCount_BothLanguages  locks in that
// MetricLabels' EN and ZH bundles both carry an entry for
// "model_switch_count" — MetricLabel falls back to the raw code string when
// a bundle is missing an entry, so a forgotten registration here would be
// silent (still renders something, just the code instead of a real label).
func TestMetricLabel_ModelSwitchCount_BothLanguages(t *testing.T) {
	if got := MetricLabel(EN, "model_switch_count"); got == "model_switch_count" || got == "" {
		t.Errorf("MetricLabel(EN, model_switch_count) = %q, want a real label, not the raw code", got)
	}
	if got := MetricLabel(ZH, "model_switch_count"); got == "model_switch_count" || got == "" {
		t.Errorf("MetricLabel(ZH, model_switch_count) = %q, want a real label, not the raw code", got)
	}
}
