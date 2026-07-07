// Ver 2026-07-07, by Fable 5

// Package anthropic is the passthrough adapter for Anthropic-compatible
// providers (Anthropic, MiniMax, DeepSeek, …): append /messages to the base
// URL, inject x-api-key, swap the model field. No protocol conversion.
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"vmr/internal/adapter"
	"vmr/internal/core"
)

const defaultVersion = "2023-06-01"

func init() { adapter.Register("anthropic", Anthropic{}) }

type Anthropic struct{}

func (Anthropic) Protocol() string { return "anthropic" }

func (Anthropic) BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, error) {
	body, err := adapter.RewriteModel(req.Raw, ep.Model)
	if err != nil {
		return nil, fmt.Errorf("rewrite model: %w", err)
	}
	url := strings.TrimRight(ep.BaseURL, "/") + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if ep.APIKey != "" {
		httpReq.Header.Set("x-api-key", ep.APIKey)
	}
	version := req.HeaderGet("anthropic-version")
	if version == "" {
		version = defaultVersion
	}
	httpReq.Header.Set("anthropic-version", version)
	if beta := req.HeaderGet("anthropic-beta"); beta != "" {
		httpReq.Header.Set("anthropic-beta", beta)
	}
	return httpReq, nil
}

func (Anthropic) TransformBody(body io.ReadCloser, stream bool) io.ReadCloser { return body }

func (Anthropic) ClassifyError(status int, body []byte) core.ErrorClass {
	if status == 529 { // Anthropic-specific: overloaded_error
		return core.ErrTransient
	}
	return adapter.DefaultClassify(status, body)
}
