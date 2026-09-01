// Ver 2026-07-22 21:00, by Sonnet 5

// Package openai is the passthrough adapter for OpenAI-compatible providers:
// rewrite URL, inject key, swap the model field; the body is otherwise untouched.
package openai

import (
	"context"
	"net/http"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/jsonscan"
)

func init() { adapter.Register(core.ProtocolOpenAICompletions, OpenAI{}) }

// chatCompletionsPath is the bare protocol path; base_url must already
// carry the provider's own API version (see adapter.ResolveURL).
const chatCompletionsPath = "/chat/completions"

type OpenAI struct{}

func (OpenAI) Protocol() string { return core.ProtocolOpenAICompletions }
func (OpenAI) ResolveURL(baseURL string) string {
	return adapter.ResolveURL(baseURL, chatCompletionsPath)
}

func (OpenAI) BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, []byte, error) {
	return adapter.BuildUpstreamRequest(ctx, ep, req, jsonscan.RewriteRoles, "Authorization", "Bearer "+ep.APIKey)
}

func (OpenAI) ClassifyError(status int, body []byte) core.ErrorClass {
	return adapter.DefaultClassify(status, body)
}
