// Ver 2026-09-15, by pi

// Deprecated: legacy toolCallLine eating chatmsg.ToolCall was deleted in Phase 3
// in favor of viewmodel_spine.go's vmToolCallBlocks (D11).
// Argument shape analysis and truncation helpers retained here.
package journey

import (
	"strconv"
	"strings"

	"vmr/internal/i18n"
)

const spineShortFieldLen = 60
const spineInlineLen = 120
const spinePreviewLen = 160
const spineFullCap = 3000

// scalarSummary renders one JSON-decoded argument value as a short display
// string, plus whether it's the call's payload as opposed to a short flag-shaped scalar.
func scalarSummary(raw any) (s string, big bool) {
	switch x := raw.(type) {
	case string:
		return x, len([]rune(x)) > spineShortFieldLen || strings.Contains(x, "\n")
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), false
	case bool:
		return strconv.FormatBool(x), false
	case nil:
		return "null", false
	case []any:
		count := "[" + strconv.Itoa(len(x)) + "]"
		if len(x) == 0 {
			return count, true
		}
		if first, ok := x[0].(string); ok {
			return count + " " + first, true
		}
		if first, ok := x[0].(map[string]any); ok {
			for _, k := range []string{"step", "title", "name", "description", "content", "text"} {
				if s, ok := first[k].(string); ok {
					return count + " " + s, true
				}
			}
		}
		return count, true
	case map[string]any:
		return "{" + strconv.Itoa(len(x)) + "}", true
	default:
		return "", false
	}
}

// capFull rune-truncates s to spineFullCap for a tool call's own arguments.
func capFull(s string, t i18n.SpineText) string {
	return capFullWith(s, t.SpineValueTruncated)
}
