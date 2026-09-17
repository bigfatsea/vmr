// Ver 2026-09-15, by coding

// Header redaction for the audit trail (split out of audit.go purely for
// archtest's file-size budget): credential-header masking for recorded
// request/response headers, plus KeyTag — the non-secret client-identity
// label derived from an api_keys entry's tail.
package audit

import (
	"net/http"
	"strings"
)

// SetExtraRedactHeaders updates the extra redaction list (config's
// extra_redact_headers); nil or empty is a no-op difference from the
// built-in credentialHeaders list, not an error.
func SetExtraRedactHeaders(names []string) {
	cp := append([]string(nil), names...)
	extraRedactHeaders.Store(&cp)
}

// IsCredentialHeader reports whether h is one of the header names Redact
// masks — the built-in credentialHeaders list plus whatever
// SetExtraRedactHeaders configured. A stored audit record's value for such
// a header is a placeholder ("Bearer ***c1d4"), never the real credential —
// a consumer that reconstructs a request from an audit record
// (internal/replay) must strip these in addition to whatever headers it
// would otherwise block, or it forwards the masked placeholder to a live
// upstream as if it were real.
func IsCredentialHeader(h string) bool {
	for _, k := range credentialHeaders {
		if strings.EqualFold(k, h) {
			return true
		}
	}
	if p := extraRedactHeaders.Load(); p != nil {
		for _, k := range *p {
			if strings.EqualFold(k, h) {
				return true
			}
		}
	}
	return false
}

// Redact copies headers, masking credential values ("Bearer sk-…" → "***c1d4")
// for both the built-in credentialHeaders list and whatever
// SetExtraRedactHeaders configured.
func Redact(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	out := make(http.Header, len(h))
	for k, vs := range h {
		out[k] = append([]string(nil), vs...)
	}
	maskNames(out, credentialHeaders)
	if p := extraRedactHeaders.Load(); p != nil {
		maskNames(out, *p)
	}
	return out
}

func maskNames(h http.Header, names []string) {
	for _, k := range names {
		if vs := h.Values(k); len(vs) > 0 {
			masked := make([]string, len(vs))
			for i, v := range vs {
				masked[i] = mask(v)
			}
			h[http.CanonicalHeaderKey(k)] = masked
		}
	}
}

func mask(v string) string {
	// Keep an auth-scheme prefix ("Bearer ") readable, mask the credential.
	cred := v
	prefix := ""
	if i := strings.IndexByte(v, ' '); i > 0 {
		prefix, cred = v[:i+1], v[i+1:]
	}
	if len(cred) <= 4 {
		return prefix + "***"
	}
	return prefix + "***" + cred[len(cred)-4:]
}

// keyTagLen bounds how many trailing characters of a matched config.APIKeys
// entry ever become its ClientKeyTag. 8 (vs. mask()'s 4) trades a couple
// more characters of exposure for a label that reads as deliberate — see
// config.example.yaml's "end your key in -something-readable" convention.
// This is independent of mask()'s redaction length: mask() protects every
// credential header generically; KeyTag only ever runs on vmr's own
// api_keys entries, whose minimum length config.Config.validate already
// enforces specifically so this never exposes a whole key (see keyTagLen's
// config-side counterpart, the 16-char minimum).
const keyTagLen = 8

// KeyTag derives a short, non-secret label from a credential's tail — the
// caller-facing "who sent this" identity for `vmr analyze` grouping. Called
// on a matched config.APIKeys entry, and (server.authenticate, when
// APIKeys is not configured at all) on whatever unvalidated value a
// client voluntarily sends — KeyTag itself doesn't care which; either way
// the input is just a string, and the 16-character minimum that keeps the
// former case safe (see below) simply doesn't apply to the latter, since an
// unconfigured, unvalidated value was never a secret to begin with.
//
// Rule: take the last keyTagLen raw characters first, then, if that window
// contains a hyphen, keep only what follows the LAST hyphen inside it —
// this lets a meaningful suffix shorter than keyTagLen (e.g. "-al", 2
// chars) survive intact instead of being padded with whatever unrelated
// characters preceded it in the fixed-length window. A suffix longer than
// keyTagLen simply loses its hyphen and everything before it once the
// window no longer reaches back that far — capped, never longer.
//
// Examples (keyTagLen = 8):
//
//	...am-alice   → window "am-alice"   → tag "alice"     (5 chars, hyphen at 2)
//	...roj-al     → window "roj-al" (key shorter than window, so window = whole key) → tag "al" (2 chars, hyphen at 3)
//	...x-abcd     → window "x-abcd" (key shorter than window, so window = whole key) → tag "abcd" (4 chars, hyphen at 1)
//	...-abcdefghi → window "bcdefghi"   → tag "bcdefghi"  (hyphen 9 back, outside the window — none found, window kept whole)
//	...am9k3f7a   → window "am9k3f7a"   → tag "am9k3f7a"  (no hyphen anywhere — window kept whole)
//
// Assumes the key is ASCII (true for every real bearer-token format). A key
// shorter than keyTagLen is used whole as the window, then the same hyphen
// rule applies — which is why config validation rejects api_keys entries
// under 16 characters: short enough for the window to be the entire secret
// would otherwise leak it into every report and filename this tag ends up
// in.
func KeyTag(key string) string {
	window := key
	if len(key) > keyTagLen {
		window = key[len(key)-keyTagLen:]
	}
	// i+1 < len(window) excludes a hyphen that is itself the window's last
	// character (nothing follows it) — trimming there would produce an
	// empty tag, so the whole window is kept instead.
	if i := strings.LastIndexByte(window, '-'); i >= 0 && i+1 < len(window) {
		return window[i+1:]
	}
	return window
}
