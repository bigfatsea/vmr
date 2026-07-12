// Ver 2026-07-12 16:30, by Fable 5

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

const defaultVersion = "2023-06-01"

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
	// Default anthropic-version if the client didn't send one.
	if httpReq.Header.Get("anthropic-version") == "" {
		httpReq.Header.Set("anthropic-version", defaultVersion)
	}
	return httpReq, body, nil
}

func (Anthropic) ClassifyError(status int, body []byte) core.ErrorClass {
	if status == 529 { // Anthropic-specific: overloaded_error
		return core.ErrTransient
	}
	return adapter.DefaultClassify(status, body)
}
