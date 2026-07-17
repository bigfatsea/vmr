// Ver 2026-07-17 00:00, by Sonnet 5

// Package anthropic is the passthrough adapter for Anthropic-compatible
// providers (Anthropic, MiniMax, DeepSeek, …): append /messages to the base
// URL, inject x-api-key, swap the model field. No protocol conversion.
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"vmr/internal/adapter"
	"vmr/internal/core"
)

func init() { adapter.Register("anthropic", Anthropic{}) }

type Anthropic struct{}

func (Anthropic) Protocol() string { return "anthropic" }

func (Anthropic) BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, []byte, error) {
	body, err := adapter.RewriteModel(req.Raw, ep.Model)
	if err != nil {
		return nil, nil, fmt.Errorf("rewrite model: %w", err)
	}
	url := strings.TrimRight(ep.BaseURL, "/") + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	// Copy the protocol+passthrough headers assembled by the server
	// layer (see chatHandler). These include the Anthropic version
	// negotiation headers plus any other client metadata that wasn't
	// on the blocklist.
	for k, vs := range req.Header {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	// Content-Type and x-api-key must come from the adapter, not the
	// client — see OpenAI adapter for the same reasoning.
	httpReq.Header.Set("Content-Type", "application/json")
	if ep.APIKey != "" {
		httpReq.Header.Set("x-api-key", ep.APIKey)
	}
	// No default anthropic-version: a client that omits it gets exactly
	// what a direct connection to the provider would see — the provider's
	// own default, not one vmr picks on its behalf. Forwarding nothing
	// here is a deliberate passthrough choice (§5.4), not an oversight.
	return httpReq, body, nil
}

func (Anthropic) ClassifyError(status int, body []byte) core.ErrorClass {
	if status == 529 { // Anthropic-specific: overloaded_error
		return core.ErrTransient
	}
	return adapter.DefaultClassify(status, body)
}
