// Ver 2026-08-20 19:00, by Sonnet 5

// Self-traffic exclusion (P6.4): the SessionAnalysis-level half of it —
// see aggregate.go's ingestRecord/scanAndCacheFile for the per-record
// half (Overall/ByClient/RequestRows/onRecord). Split into its own file
// per archtest's file-size budget, not because it's a separate concern.
package report

// excludeSelfTrafficFromSessionAnalysis drops sess.Recs/Compactions
// entries whose ClientKeyTag is in excludeClientTags, in place — the two
// flat slices buildTools/buildCompactions (§5/§6.7) read directly,
// bypassing ingestRecord's own per-record check entirely (that check only
// ever sees records ingestRecord itself processes; these two report
// sections are built straight from sess after the scan, a separate read).
// A no-op when excludeClientTags is empty. sess.Sessions is deliberately
// left untouched: rep.Sessions (§6) is already correctly filtered because
// a self-traffic session never gets a SessionRow in the first place
// (every one of its records is skipped in ingestRecord), so nothing reads
// sess.Sessions directly that this would need to protect.
func excludeSelfTrafficFromSessionAnalysis(sess *SessionAnalysis, excludeClientTags map[string]bool) {
	if len(excludeClientTags) == 0 {
		return
	}
	keptRecs := sess.Recs[:0]
	for _, r := range sess.Recs {
		if excludeClientTags[r.ClientKeyTag] {
			continue
		}
		keptRecs = append(keptRecs, r)
	}
	sess.Recs = keptRecs

	keptCompactions := sess.Compactions[:0]
	for _, c := range sess.Compactions {
		if excludeClientTags[c.ClientKeyTag] {
			continue
		}
		keptCompactions = append(keptCompactions, c)
	}
	sess.Compactions = keptCompactions
}
