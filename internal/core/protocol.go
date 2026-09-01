// Ver 2026-08-27, by Sonnet 5

package core

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
