// Ver 2026-07-28 22:15, by Sonnet 5

// Usage/ExtractUsage/extractFinish moved to internal/chatmsg (see
// chatmsg_compat.go for the delegating wrappers) — nested/num stay here
// unexported since session.go's metadata.user_id lookup also needs them
// directly, independent of anything that moved.
package report

import (
	"encoding/json"
)

func nested(obj map[string]any, keys ...string) any {
	var cur any = obj
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}
