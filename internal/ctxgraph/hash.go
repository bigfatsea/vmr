// Ver 2026-07-29 23:55, by Sonnet 5

// Package ctxgraph builds a content-addressed model of an agent's context
// over time: each request's message list becomes a Manifest (a vector of
// message hashes), and the edit between consecutive manifests within the
// same raw session grouping is classified (append / replace-tail / contract
// / fork) purely structurally — no template matching, no agent-specific
// knowledge. See docs/VirtualModelRouter_Design_v4_Analytics.md's First
// Principles and internal/ctxgraph content-addressing layer sections for
// the derivation, and its Empirical Validation section for the corpus
// evidence this design is calibrated against.
//
// This package must not depend on vmr/internal/{router,server,config,report,
// story} — see internal/archtest's import boundary test. It depends only on
// {audit, chatmsg} to read records and parse chat bodies.
package ctxgraph

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// MarshalJSON/UnmarshalJSON render Hash as its hex string (same as
// String()) instead of encoding/json's default byte-array-of-numbers
// rendering for a fixed-size array — a Manifest's Keys can hold dozens of
// these, and a cached file's worth of Manifests (internal/ctxgraph/cache.go)
// can hold thousands, so the size difference isn't cosmetic: hex is under a
// third the bytes of "[26,88,3,...]" per hash, both on disk and to parse
// back.
func (h Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.String())
}

func (h *Hash) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(h) {
		return fmt.Errorf("ctxgraph: invalid Hash %q", s)
	}
	copy(h[:], b)
	return nil
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
