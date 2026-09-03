// Ver 2026-09-03, by pi-agent

package story

import (
	"regexp"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

// TestJourneyIndicatorSets_MatchMDAndHTML pins the shared-list contract: the single-journey
// behavior-indicator block must show the SAME set of metrics in Markdown
// and HTML. Both renderers iterate the shared journeyMetrics slice
// (compare_metrics.go); if one side ever hand-rolls its list again, the
// two extracted sets diverge and this test goes red. Order may differ
// between formats — only the set is pinned.
func TestJourneyIndicatorSets_MatchMDAndHTML(t *testing.T) {
	j, m, f := htmlTestJourney(t)
	lang := i18n.EN

	md := RenderMarkdown(j, m, f, lang, false, true, nil)
	html := RenderHTML(j, m, f, CostFact{}, lang, false)

	wantCodes := make(map[string]bool, len(journeyMetrics))
	for _, jm := range journeyMetrics {
		wantCodes[string(jm.Code)] = true
	}
	if len(wantCodes) == 0 {
		t.Fatal("journeyMetrics is empty — the unification regressed to nothing")
	}

	// md rows are "| Label | value |" lines inside the indicators table;
	// html stat cells are <div class="k">Label</div>. Extract both label
	// sets and compare them against the codes' i18n labels (order-free).
	mdRows := regexp.MustCompile(`(?m)^\| ([^|]+) \| [^|]+ \|$`).FindAllStringSubmatch(md, -1)
	if len(mdRows) == 0 {
		t.Fatalf("no indicator table rows found in Markdown output:\n%s", md)
	}
	htmlCells := regexp.MustCompile(`<div class="k">([^<]+)</div>`).FindAllStringSubmatch(html, -1)
	if len(htmlCells) == 0 {
		t.Fatalf("no stat cells found in HTML output:\n%s", html)
	}

	labelOf := func(code string) string {
		return i18n.MetricLabel(lang, code)
	}
	mdLabels := map[string]bool{}
	for _, r := range mdRows {
		label := strings.TrimSpace(r[1])
		if label == "Metric" { // the table header row, not a data row
			continue
		}
		mdLabels[label] = true
	}
	htmlLabels := map[string]bool{}
	for _, c := range htmlCells {
		htmlLabels[strings.TrimSpace(c[1])] = true
	}

	for code := range wantCodes {
		label := labelOf(code)
		if !mdLabels[label] {
			t.Errorf("Markdown output missing indicator %q (code %s)", label, code)
		}
		if !htmlLabels[label] {
			t.Errorf("HTML output missing indicator %q (code %s)", label, code)
		}
	}
	// And nothing extra on either side beyond the unified set.
	if len(mdLabels) != len(wantCodes) {
		t.Errorf("Markdown shows %d indicator labels, want exactly %d (the unified set): %v", len(mdLabels), len(wantCodes), mdLabels)
	}
	if len(htmlLabels) != len(wantCodes) {
		t.Errorf("HTML shows %d indicator labels, want exactly %d (the unified set): %v", len(htmlLabels), len(wantCodes), htmlLabels)
	}
}
