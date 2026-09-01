// Ver 2026-07-22 21:00, by Sonnet 5

// Package anthropic is the passthrough adapter for Anthropic-compatible
// providers (Anthropic, MiniMax, DeepSeek, …): append /messages to the base
// URL, inject x-api-key, swap the model field. No protocol conversion.
package anthropic

import (
	"context"
	"net/http"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/jsonscan"
)

func init() { adapter.Register(core.ProtocolAnthropicMessages, Anthropic{}) }

// messagesPath is the bare protocol path; base_url must already carry
// the provider's own API version (see adapter.ResolveURL).
const messagesPath = "/messages"

type Anthropic struct{}

func (Anthropic) Protocol() string { return core.ProtocolAnthropicMessages }
func (Anthropic) ResolveURL(baseURL string) string {
	return adapter.ResolveURL(baseURL, messagesPath)
}

func (Anthropic) BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, []byte, error) {
	// No default anthropic-version: a client that omits it gets exactly
	// what a direct connection to the provider would see — the provider's
	// own default, not one vmr picks on its behalf. Forwarding nothing
	// here is a deliberate passthrough choice (see the design doc's
	// header-passthrough policy), not an oversight.
	return adapter.BuildUpstreamRequest(ctx, ep, req, jsonscan.RewriteRoles, "x-api-key", ep.APIKey)
}

func (Anthropic) ClassifyError(status int, body []byte) core.ErrorClass {
	if status == 529 { // Anthropic-specific: overloaded_error
		return core.ErrTransient
	}
	return adapter.DefaultClassify(status, body)
}
