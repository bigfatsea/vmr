// Ver 2026-07-29 23:55, by Sonnet 5

// Package ctxgraph builds a content-addressed model of an agent's context
// over time: each request's message list becomes a Manifest (a vector of
// message hashes), and the edit between consecutive manifests within the
// same raw session grouping is classified (append / replace-tail / contract
// / fork) purely structurally — no template matching, no agent-specific
// knowledge. See docs/VirtualModelRouter_Design_v4_Analytics.md §3.0-3.1 for
// the derivation and §5 for the corpus evidence this design is calibrated
// against.
//
// This package must not depend on vmr/internal/{router,server,config,report,
// story} — see internal/archtest's import boundary test. It depends only on
// {audit, chatmsg} to read records and parse chat bodies.
package ctxgraph

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
)

// Hash is a content digest of one decoded message value (its canonical JSON
// encoding, since encoding/json sorts map keys — deterministic regardless of
// key order in the original request body).
type Hash [16]byte

// String renders h as a lowercase hex string — internal/story uses this for
// a Journey id's trailing disambiguator (RootHash().String()[:idCodeLen]),
// so it needs to be filename/URL-safe, not just human-readable.
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

// hashJSON re-serializes v with json.Marshal (which sorts object keys) and
// returns its md5 digest. v is typically a raw decoded message object (a
// map[string]any straight from the audit record) or, when the raw form
// isn't available, a rendered text fallback (a plain string) — both cases
// json.Marshal handles directly.
func hashJSON(v any) Hash {
	b, _ := json.Marshal(v)
	return md5.Sum(b)
}
