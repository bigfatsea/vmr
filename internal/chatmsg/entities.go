// Ver 2026-07-29 23:00, by Sonnet 5

// ExtractEntities is a rough, rule-based scan for file paths and URLs —
// design doc's "规则粗筛的实体覆盖率": this deliberately doesn't try to
// understand what a swallowed message MEANT, only to point at concrete,
// checkable tokens (a file path, a URL) so a human can judge for themselves
// whether losing them mattered. Moved here from internal/story once
// internal/report also needed the same scan for
// its own compaction section — both packages already depend on chatmsg, so
// this is the one shared point that doesn't violate either package's
// archtest boundary.
package chatmsg

import "regexp"

var entityRe = regexp.MustCompile(`https?://[^\s"'` + "`" + `)]+|\b[\w][\w./\-]*\.[a-zA-Z]{1,6}\b`)

// MaxEntities caps how many entities ExtractEntities returns — a
// long-running lineage's swallowed tail can mention hundreds of file paths;
// this is a triage aid, not an audit log.
const MaxEntities = 30

// ExtractEntities returns the de-duplicated, order-preserved list of
// file-path-like or URL tokens found in text, capped at MaxEntities.
func ExtractEntities(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range entityRe.FindAllString(text, -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
		if len(out) >= MaxEntities {
			break
		}
	}
	return out
}
