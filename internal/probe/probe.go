// Ver 2026-07-18 22:45, by Sonnet 5

// Package probe builds the minimal, verifiable request vmr uses to ask "is
// this endpoint actually alive right now" — shared by internal/diagnose (the
// one-shot `vmr diagnose` CLI) and internal/router (the runtime active health
// probe). It lives in its own package because internal/diagnose already
// imports internal/router; putting this here instead of in either of them
// avoids an import cycle while letting both share one implementation.
package probe

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// Request builds a minimal chat-completion body that asks the model to echo
// back a one-time nonce, and returns the nonce alongside it for the caller to
// verify against the raw response. The shape (model/messages/max_tokens) is
// recognized by every OpenAI- and Anthropic-compatible provider vmr targets.
//
// Asking for an echo (rather than just checking the HTTP status) catches a
// class of failure a bare 200 can't: a relay/gateway layer answering with a
// cached or canned response while the real model never ran. A plain
// substring match on the raw response body is enough to verify it — see
// Echoed.
func Request(model string) (body json.RawMessage, nonce string) {
	nonce = newNonce()
	b, err := json.Marshal(map[string]any{
		"model": model,
		// 300, not just enough room for the nonce: several reasoning-style
		// models spend their whole token budget on a <think>...</think>
		// block before ever reaching the answer — a smaller max_tokens made
		// real, healthy endpoints hit finish_reason:"length" mid-thought and
		// come back with no nonce at all, a false "unverified" rather than a
		// real endpoint problem.
		"max_tokens": 300,
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with exactly this token and nothing else: " + nonce},
		},
	})
	if err != nil {
		// Marshaling a map of string literals cannot fail; this exists only
		// so the function keeps a single, ordinary (value, error-free) return
		// shape instead of a panic path a caller would never expect.
		return json.RawMessage(`{}`), nonce
	}
	return b, nonce
}

// newNonce returns a short, effectively-unique token. It doesn't need to be
// cryptographically unpredictable — only distinct enough that seeing it in a
// response body is proof this response was generated for this request, not
// replayed from a cache — so a read failure degrades to an all-zero token
// (a valid, merely less distinctive, nonce) rather than aborting the probe.
func newNonce() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "VMR-PROBE-" + hex.EncodeToString(b[:])
}

// Echoed reports whether body — a raw upstream response, whatever its shape
// or protocol — contains nonce. A plain substring search is deliberately
// protocol-agnostic: the model was asked to output only the nonce, so its
// presence anywhere in the response text is sufficient proof of a fresh, real
// completion. Parsing OpenAI's choices[].message.content vs Anthropic's
// content[].text separately just to find eight bytes of hex would be
// complexity this check doesn't need.
func Echoed(body []byte, nonce string) bool {
	return bytes.Contains(body, []byte(nonce))
}
