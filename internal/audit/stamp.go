// Ver 2026-09-09, by pi

// Attempt.Tokens / Attempt.KeyLabel stamping: the LiveStats design doc's
// §3.1 前置改动 lives in its own file so the main audit.go stays inside its
// archtest line budget. The token shape is deliberately the exact key space
// the LiveStats design pins for its slim/rollup files: downstream readers
// parse one shape everywhere. Pure data — the quota.Counters → TokenCount
// mapping lives at the stamp site (router), keeping this package free of
// quota's vocabulary.

package audit

// TokenCount is Attempt.Tokens' shape — the four raw token components, in
// the key space the LiveStats design pins for its slim/rollup files.
type TokenCount struct {
	In         int64 `json:"in"`
	Out        int64 `json:"out"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// SetTokens stamps the four raw token components (see Tokens' doc comment
// for the source and why it rides on Forwarded attempts only). Passing nil
// (caller had no counters) records nothing — a nil Tokens must mean "never
// stamped", never "stamped as zero".
func (a *Attempt) SetTokens(t *TokenCount) {
	if a == nil || t == nil {
		return
	}
	a.Tokens = t
}

// SetKeyLabel stamps which upstream credential served this attempt (see
// KeyLabel's doc comment). An empty label records nothing.
func (a *Attempt) SetKeyLabel(label string) {
	if a == nil || label == "" {
		return
	}
	a.KeyLabel = label
}
