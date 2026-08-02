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

// RoleCompatRequest builds a two-message probe: a leading message under role
// (e.g. "developer", the role OpenAI's o1/o3-series introduced that some
// self-described-OpenAI-compatible providers reject outright — see
// config.example.yaml's role_map), followed by an ordinary "user" message
// asking for the nonce echo. Two messages, not Request's single one: some
// providers reject a request whose ONLY message isn't role "user" regardless
// of what that other role is — a message-array-shape rejection that has
// nothing to do with role support, and Request's single-message shape can't
// tell the two apart. A leading role/user pair is also simply what every
// real client sends (a system/developer preamble followed by a user turn),
// so this is the shape actually worth validating end to end. Sending it
// through the exact same adapter.BuildRequest/RoleMap path real traffic uses
// is how `vmr diagnose` verifies a provider actually accepts role, or that a
// configured role_map correctly rewrites it before the request ever leaves
// vmr — see internal/diagnose's testEndpointRole. Only that one-shot
// diagnostic calls this; the runtime active health probe intentionally
// stays on Request's minimal single-message shape.
func RoleCompatRequest(model, role string) (body json.RawMessage, nonce string) {
	nonce = newNonce()
	b, err := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 300,
		"messages": []map[string]string{
			{"role": role, "content": "You are a helpful assistant."},
			{"role": "user", "content": "Reply with exactly this token and nothing else: " + nonce},
		},
	})
	if err != nil {
		return json.RawMessage(`{}`), nonce
	}
	return b, nonce
}

// ResponsesRequest is Request's Responses-protocol counterpart: same
// echo-nonce liveness check, built in the Responses shape (top-level
// "input" instead of "messages", no "max_tokens" — Responses' analogous
// field is named differently and unconfirmed against a real endpoint, so it
// is deliberately omitted rather than guessed; leaving an optional field
// out is always safe, a wrong field name is not). Used both by the runtime
// background recovery probe (internal/router/probe.go's runProbe) and `vmr
// diagnose` for any endpoint whose protocol is "openai-responses" — sending
// Request's messages-shaped body to a /responses endpoint would be rejected
// as a missing required "input" field, which is exactly the bug this
// function exists to avoid.
func ResponsesRequest(model string) (body json.RawMessage, nonce string) {
	nonce = newNonce()
	b, err := json.Marshal(map[string]any{
		"model": model,
		"input": "Reply with exactly this token and nothing else: " + nonce,
	})
	if err != nil {
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
