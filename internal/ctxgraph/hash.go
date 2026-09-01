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

// hashMsgJSON is the per-message hashing BuildManifest uses, as opposed to
// hashJSON's raw digest: Anthropic's cache_control breakpoint markers
// (on the message itself or on individual content blocks) are client-side
// cache-routing metadata, not conversation content — a client that moves
// its breakpoint between turns changes which message carries the marker,
// and hashing it raw turns that move into a phantom content "edit"
// (Append misread as Contract/Splice), fracturing the lineage. The marker
// is stripped deep before hashing, without mutating the caller's decoded
// body (the record is shared with every other consumer). The
// contains-check first keeps the no-marker case at hashJSON's exact cost.
func hashMsgJSON(raw any) Hash {
	if !containsCacheControl(raw) {
		return hashJSON(raw)
	}
	return hashJSON(stripCacheControl(raw))
}

func containsCacheControl(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		if _, ok := t["cache_control"]; ok {
			return true
		}
		for _, x := range t {
			if containsCacheControl(x) {
				return true
			}
		}
	case []any:
		for _, x := range t {
			if containsCacheControl(x) {
				return true
			}
		}
	}
	return false
}

// stripCacheControl deep-copies v omitting every "cache_control" key at any
// depth. Values without maps/slices are shared, not copied — only the
// containers on a marker-bearing path get rebuilt.
func stripCacheControl(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			if k == "cache_control" {
				continue
			}
			out[k] = stripCacheControl(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = stripCacheControl(x)
		}
		return out
	default:
		return v
	}
}
