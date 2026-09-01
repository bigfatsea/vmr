// Ver 2026-09-01, by Sonnet 5

// Dedicated failed requests index rendering: WriteFailedIndex, FailedRequestRows,
// and temporal error clustering (问题 28). Split from requests.go to respect
// archtest line budgets.
package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
)

// FailedRequestRows filters rows down to the error-analysis surface: outcome
// "error" (upstream/vmr rejected the request), "canceled" (client hung up
// mid-request), and "ok" rows with Truncated==true (client got a 2xx but the
// stream broke off mid-response — a usable response was still not fully
// delivered).
func FailedRequestRows(rows []RequestRow) []RequestRow {
	var out []RequestRow
	for _, r := range rows {
		if r.Outcome == "error" || r.Outcome == "canceled" || (r.Outcome == "ok" && r.Truncated) {
			out = append(out, r)
		}
	}
	return out
}

func clusterFailedRequests(failed []RequestRow) (clusters int, maxCount int, maxSpan string, maxClasses string) {
	if len(failed) == 0 {
		return 0, 0, "", ""
	}
	type cluster struct {
		start, end time.Time
		rows       []RequestRow
	}
	var list []cluster
	var cur cluster
	for _, r := range failed {
		t, err := time.Parse(time.RFC3339, r.TS)
		if err != nil {
			continue
		}
		if len(cur.rows) == 0 {
			cur = cluster{start: t, end: t, rows: []RequestRow{r}}
			continue
		}
		diff := t.Sub(cur.end)
		if diff >= -2*time.Minute && diff <= 2*time.Minute {
			if t.After(cur.end) {
				cur.end = t
			}
			if t.Before(cur.start) {
				cur.start = t
			}
			cur.rows = append(cur.rows, r)
		} else {
			if len(cur.rows) > 0 {
				list = append(list, cur)
			}
			cur = cluster{start: t, end: t, rows: []RequestRow{r}}
		}
	}
	if len(cur.rows) > 0 {
		list = append(list, cur)
	}
	clusters = len(list)
	var maxClust cluster
	for _, c := range list {
		if len(c.rows) > maxCount {
			maxCount = len(c.rows)
			maxClust = c
		}
	}
	if maxCount > 0 {
		f := maxClust.start.In(fmtutil.DisplayZone)
		e := maxClust.end.In(fmtutil.DisplayZone)
		if f.Format("2006-01-02") == e.Format("2006-01-02") {
			maxSpan = f.Format("01-02 15:04:05") + " ~ " + e.Format("15:04:05")
		} else {
			maxSpan = f.Format("01-02 15:04:05") + " ~ " + e.Format("01-02 15:04:05")
		}
		clsCounts := map[string]int{}
		for _, r := range maxClust.rows {
			cls := r.ErrorClass
			if cls == "" {
				cls = r.Outcome
			}
			clsCounts[cls]++
		}
		var clsParts []string
		for k, v := range clsCounts {
			clsParts = append(clsParts, fmt.Sprintf("%s×%d", k, v))
		}
		sort.Strings(clsParts)
		maxClasses = strings.Join(clsParts, ", ")
	}
	return
}

// WriteFailedIndex writes vmr-requests-failed.md: a flat, time-ordered index
// of every failed request (FailedRequestRows), each row's "文件" column a
// detailCell (a details/*.md link when the target actually exists on disk,
// else the req coordinate — see detailCell's own doc comment, P13.4). This
// is a dedicated error-analysis index — it does not remove or alter failed
// requests anywhere else; vmr-requests.md and every per-group sibling keep
// listing them exactly as before.
func WriteFailedIndex(rows []RequestRow, dir string, lang i18n.Lang, detailDir string) error {
	detailSet := buildDetailFileSet(detailDir)
	t := i18n.Requests(lang)
	failed := FailedRequestRows(rows)
	sort.SliceStable(failed, func(i, j int) bool {
		ti, erri := time.Parse(time.RFC3339, failed[i].TS)
		tj, errj := time.Parse(time.RFC3339, failed[j].TS)
		if erri == nil && errj == nil {
			return ti.Before(tj)
		}
		return failed[i].TS < failed[j].TS
	})

	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	w("# %s\n\n", t.FailedIndexTitle)
	w("%s", t.FailedIndexIntro(len(failed)))
	if len(failed) == 0 {
		return os.WriteFile(filepath.Join(dir, "vmr-requests-failed.md"), []byte(b.String()), 0o600)
	}
	clusters, maxCount, maxSpan, maxClasses := clusterFailedRequests(failed)
	if clusters > 1 || (clusters == 1 && maxCount > 1) {
		w("%s", t.FailedClusterSummary(len(failed), clusters, maxCount, maxSpan, maxClasses))
	}
	w("%s", t.FailedTableHeader)
	for _, r := range failed {
		w("| %s | %s | %s/%s | %s | %s | %s |\n",
			fmtDisplayFull(r.TS), sessTaskCell(r), r.Protocol, orDashModel(r.Model),
			outcomeCell(r), fmtDurMS(r.DurMS), detailCell(r, detailSet))
	}
	return os.WriteFile(filepath.Join(dir, "vmr-requests-failed.md"), []byte(b.String()), 0o600)
}
