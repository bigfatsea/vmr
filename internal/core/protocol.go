// Ver 2026-08-27, by Sonnet 5

package core

import "strings"

// Protocol enum — the ingress adapter's registered type string, also the
// protocol segment of an EndpointLabel. Centralized here so both halves
// (routing and analytics) reference one definition instead of scattered
// literals. Values follow the ecosystem convention (Pi Agent et al.):
// "<vendor>-<surface>".
const (
	ProtocolOpenAICompletions = "openai-completions"
	ProtocolAnthropicMessages = "anthropic-messages"
	ProtocolOpenAIResponses   = "openai-responses"
)

// CanonicalProtocol normalizes a legacy protocol name from a historical
// audit record to the current enum value. "openai" and "anthropic" were the
// registered names before the 2026-08 rename.
//
// TODO(2026-10): transitional shim for reading pre-rename audit logs only —
// remove once historical logs have aged out. See the protocol-rename entry
// in docs/KNOWN_ISSUES.md's 配置与协议 section for the full removal list.
// MUST NOT be called on any write path.
func CanonicalProtocol(p string) string {
	switch p {
	case "openai":
		return ProtocolOpenAICompletions
	case "anthropic":
		return ProtocolAnthropicMessages
	default:
		return p
	}
}

// NormalizeEndpointLabel rewrites only the leading protocol token of an
// endpoint label ("protocol:provider:model", or the legacy "/"-joined form)
// read from a historical audit record — separator and everything after it
// are left byte-for-byte intact, and a label whose protocol is already
// current (or that has no separator) is returned unchanged.
//
// TODO(2026-10): transitional, remove with CanonicalProtocol.
func NormalizeEndpointLabel(label string) string {
	ci := strings.IndexByte(label, ':')
	si := strings.IndexByte(label, '/')
	i := ci
	if i < 0 || (si >= 0 && si < i) {
		i = si
	}
	if i < 0 {
		return label
	}
	proto := label[:i]
	canon := CanonicalProtocol(proto)
	if canon == proto {
		return label
	}
	return canon + label[i:]
}
