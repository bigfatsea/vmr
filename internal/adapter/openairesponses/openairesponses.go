// Ver 2026-08-02 12:30, by Sonnet 5

// Package openairesponses is the passthrough adapter for OpenAI's Responses
// API (POST /v1/responses) and its OpenAI-compatible implementations
// (DeepSeek, OpenRouter, ...). Same passthrough contract as
// internal/adapter/openai: rewrite URL, inject key, swap the model field;
// every other byte of the request (input/instructions/tools/... instead of
// Chat Completions' messages) reaches the provider untouched. VMR does not
// interpret Responses-specific semantics (previous_response_id, store,
// encrypted reasoning items) — it only routes; a client that sets a field
// an upstream doesn't support gets that upstream's own rejection back,
// same as any other passthrough request.
package openairesponses

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"vmr/internal/adapter"
	"vmr/internal/core"
	"vmr/internal/jsonscan"
)

func init() { adapter.Register("openai-responses", OpenAIResponses{}) }

// responsesPath is the bare protocol path; base_url must already carry the
// provider's own API version (see adapter.ResolveURL). Distinct from
// internal/adapter/openai's "/chat/completions" path even though several
// providers (DeepSeek, OpenRouter) serve both from the same host — the two
// are different endpoints with different request/response shapes, so they
// get different Endpoint entries (protocol: openai vs protocol:
// openai-responses) even when base_url is written identically under both
// provider.base_url keys.
const responsesPath = "/responses"

type OpenAIResponses struct{}

func (OpenAIResponses) Protocol() string { return "openai-responses" }
func (OpenAIResponses) ResolveURL(baseURL string) string {
	return adapter.ResolveURL(baseURL, responsesPath)
}

func (OpenAIResponses) BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, []byte, error) {
	body, err := jsonscan.RewriteModel(req.Raw, ep.Model)
	if err != nil {
		return nil, nil, fmt.Errorf("rewrite model: %w", err)
	}
	// Responses' role-bearing array is the top-level "input" (not
	// "messages") — RewriteInputRoles is the protocol-specific counterpart
	// to internal/adapter/openai's RewriteRoles, sharing the same byte-splice
	// scanner underneath (see jsonscan's unexported rewriteRolesInTopLevelArray).
	if body, err = jsonscan.RewriteInputRoles(body, ep.RoleMap); err != nil {
		return nil, nil, fmt.Errorf("rewrite roles: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.FullURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	// Copy the protocol+passthrough headers assembled by the server layer
	// (see chatHandler) — same as the openai/anthropic adapters.
	for k, vs := range req.Header {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	// Content-Type and Authorization must come from the adapter, not the
	// client — see internal/adapter/openai for the same reasoning (the
	// client's Authorization is the VMR credential, not the upstream's).
	httpReq.Header.Set("Content-Type", "application/json")
	if ep.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+ep.APIKey)
	}
	return httpReq, body, nil
}

func (OpenAIResponses) ClassifyError(status int, body []byte) core.ErrorClass {
	// Starts identical to the Chat Completions face: both speak the same
	// OpenAI error envelope ({"error":{"message":...}}), and DeepSeek/
	// OpenRouter document their Responses endpoints as returning the same
	// shape as their Chat Completions ones. Per the design doc's "must do
	// body sniffing" principle, any vendor-specific quirk on this specific
	// endpoint (a wording DefaultClassify's sniff tables don't already
	// cover) should be added here once observed against a real response,
	// not guessed at now — mirrors why internal/adapter/anthropic only
	// overrides the 529 case it has actually verified.
	return adapter.DefaultClassify(status, body)
}
