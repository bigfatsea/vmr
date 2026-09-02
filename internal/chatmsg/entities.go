// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

// ExtractEntities is a rule-based scan for file paths, URLs, code symbols,
// and CLI commands — design doc's "规则粗筛的实体覆盖率": this deliberately
// doesn't try to understand what a swallowed message MEANT, only to point at
// concrete, checkable tokens so a human can judge for themselves whether
// losing them mattered. Moved here from internal/story once internal/report
// also needed the same scan for its compaction section.
package chatmsg

import (
	"regexp"
	"sort"
	"strings"
)

var (
	urlRe = regexp.MustCompile(`https?://[^\s"'` + "`" + `)<>\[\]{}]+`)

	// explicit paths starting with /, ./, or ~/
	explicitPathRe = regexp.MustCompile(`(?:^|[\s"'` + "`" + `(\[{<])((?:\/|\.\/|~\/)[\w.\-\/]+)`)

	// file paths with extension, e.g. main.go, README.md, internal/report/session.go
	fileExtRe = regexp.MustCompile(`\b[\w][\w.\-\/]*\.[a-zA-Z][a-zA-Z0-9]{0,7}\b`)

	// directory paths ending with slash, e.g. internal/story/ — requires at
	// least two path segments so a bare word before a slash in ordinary
	// prose ("and/or", "yes/no") doesn't get mistaken for a directory.
	dirPathRe = regexp.MustCompile(`\b[\w][\w.\-]*(?:\/[\w.\-]+)+\/`)

	// CLI command whitelist (to avoid false positives like "go read" / "go ahead")
	cliCmdRe = regexp.MustCompile(`\b(?:go\s+(?:test|build|vet|run|mod|get|fmt|vendor)|git\s+(?:commit|status|diff|log|push|pull|checkout|branch|add|clone|rebase|merge|reset)|cargo\s+(?:test|build|check|run|clippy)|npm\s+(?:test|run|install|build|start)|pnpm\s+(?:test|run|install|build|start|add)|docker\s+(?:run|build|ps|stop|compose|exec)|kubectl\s+(?:get|apply|describe|logs|delete))\b`)

	// CamelCase identifier: e.g. ExtractEntities, computeDivergence (length >= 4)
	camelIdentRe = regexp.MustCompile(`\b(?:[a-z]+[A-Z][a-zA-Z0-9]*|[A-Z][a-z0-9]+[A-Z][a-zA-Z0-9]*)\b`)

	// snake_case identifier: e.g. exact_repeat_tool_call (length >= 4)
	snakeIdentRe = regexp.MustCompile(`\b[a-z0-9]+_[a-z0-9_]+\b`)
)

// MaxEntities caps how many entities ExtractEntities returns.
const MaxEntities = 30

type rawSpan struct {
	start int
	end   int
	val   string
}

func trimPunctuation(s string) string {
	return strings.TrimRight(s, ".,;:!?)'\"`]>}\n\r\t")
}

// ExtractEntities returns the de-duplicated, order-preserved list of
// file paths, URLs, identifiers, and CLI commands found in text, capped at MaxEntities.
func ExtractEntities(text string) []string {
	return entitiesFromSpans(collectEntitySpans(text))
}

// ExtractEntitiesNear is ExtractEntities restricted to entities whose span
// falls within window bytes of any [start,end) range in anchors. Detectors
// that key off a specific phrase in a tool result (e.g. a "not found"
// marker) use this so a token elsewhere in the same blob — a path named in
// a design doc that merely discusses 404 handling, a stdlib type in an
// unrelated code snippet — is not treated as the thing that phrase reported
// missing. Empty anchors returns nil.
func ExtractEntitiesNear(text string, anchors [][]int, window int) []string {
	if len(anchors) == 0 {
		return nil
	}
	spans := collectEntitySpans(text)
	kept := spans[:0]
	for _, sp := range spans {
		for _, a := range anchors {
			if sp.start < a[1]+window && sp.end > a[0]-window {
				kept = append(kept, sp)
				break
			}
		}
	}
	return entitiesFromSpans(kept)
}

// collectEntitySpans runs the seven rule scans, drops spans strictly
// contained in a larger one, and returns what's left sorted by start index.
func collectEntitySpans(text string) []rawSpan {
	if len(text) == 0 {
		return nil
	}

	var spans []rawSpan

	// 1. URLs
	for _, idx := range urlRe.FindAllStringIndex(text, -1) {
		val := trimPunctuation(text[idx[0]:idx[1]])
		if len(val) >= 4 {
			spans = append(spans, rawSpan{start: idx[0], end: idx[0] + len(val), val: val})
		}
	}

	// 2. Explicit paths (/etc/hosts, ./main, ~/.bashrc)
	for _, idx := range explicitPathRe.FindAllStringSubmatchIndex(text, -1) {
		if len(idx) >= 4 && idx[2] >= 0 {
			val := trimPunctuation(text[idx[2]:idx[3]])
			if len(val) >= 4 {
				spans = append(spans, rawSpan{start: idx[2], end: idx[2] + len(val), val: val})
			}
		}
	}

	// 3. File paths with extension (main.go, README.md, internal/report/session.go)
	for _, idx := range fileExtRe.FindAllStringIndex(text, -1) {
		val := trimPunctuation(text[idx[0]:idx[1]])
		if len(val) >= 4 {
			spans = append(spans, rawSpan{start: idx[0], end: idx[0] + len(val), val: val})
		}
	}

	// 4. Directory paths ending with slash (internal/story/)
	for _, idx := range dirPathRe.FindAllStringIndex(text, -1) {
		val := trimPunctuation(text[idx[0]:idx[1]])
		if len(val) >= 4 {
			spans = append(spans, rawSpan{start: idx[0], end: idx[0] + len(val), val: val})
		}
	}

	// 5. CLI command whitelist
	for _, idx := range cliCmdRe.FindAllStringIndex(text, -1) {
		val := trimPunctuation(text[idx[0]:idx[1]])
		spans = append(spans, rawSpan{start: idx[0], end: idx[0] + len(val), val: val})
	}

	// 6. CamelCase identifiers
	for _, idx := range camelIdentRe.FindAllStringIndex(text, -1) {
		val := trimPunctuation(text[idx[0]:idx[1]])
		if len(val) >= 4 {
			spans = append(spans, rawSpan{start: idx[0], end: idx[0] + len(val), val: val})
		}
	}

	// 7. snake_case identifiers
	for _, idx := range snakeIdentRe.FindAllStringIndex(text, -1) {
		val := trimPunctuation(text[idx[0]:idx[1]])
		if len(val) >= 4 {
			spans = append(spans, rawSpan{start: idx[0], end: idx[0] + len(val), val: val})
		}
	}

	if len(spans) == 0 {
		return nil
	}

	// Sort spans by start index ASC, then by end index DESC so larger spans come first.
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})

	// Filter out spans that are strictly contained within larger spans in O(N).
	var outerSpans []rawSpan
	maxEnd := -1
	for _, sp := range spans {
		if sp.end > maxEnd {
			outerSpans = append(outerSpans, sp)
			maxEnd = sp.end
		}
	}
	return outerSpans
}

// entitiesFromSpans de-duplicates by value, preserves order, and caps at
// MaxEntities.
func entitiesFromSpans(spans []rawSpan) []string {
	seen := map[string]bool{}
	var out []string
	for _, sp := range spans {
		if seen[sp.val] {
			continue
		}
		seen[sp.val] = true
		out = append(out, sp.val)
		if len(out) >= MaxEntities {
			break
		}
	}
	return out
}
