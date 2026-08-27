// Ver 2026-07-22 21:00, by Sonnet 5

// Package openai is the passthrough adapter for OpenAI-compatible providers:
// rewrite URL, inject key, swap the model field; the body is otherwise untouched.
package openai

import (
	"bytes"
	"context"
	"fmt"
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
	body, err := jsonscan.RewriteModel(req.Raw, ep.Model)
	if err != nil {
		return nil, nil, fmt.Errorf("rewrite model: %w", err)
	}
	if body, err = jsonscan.RewriteRoles(body, ep.RoleMap); err != nil {
		return nil, nil, fmt.Errorf("rewrite roles: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.FullURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	// Copy the protocol+passthrough headers assembled by the server
	// layer (see chatHandler). These carry the OpenAI/Anthropic
	// protocol semantics plus any client metadata the chatHandler
	// didn't put on the blocklist (User-Agent, X-Stainless-*,
	// Traceparent, Accept-Language, etc.).
	for k, vs := range req.Header {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	// Content-Type and Authorization must come from the adapter, not
	// the client — the client's Authorization is the VMR credential
	// (used to authenticate against VMR, not the upstream) and the
	// client-supplied Content-Type could in theory be wrong.
	httpReq.Header.Set("Content-Type", "application/json")
	if ep.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+ep.APIKey)
	}
	return httpReq, body, nil
}

func (OpenAI) ClassifyError(status int, body []byte) core.ErrorClass {
	return adapter.DefaultClassify(status, body)
}
