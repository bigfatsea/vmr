// Ver 2026-07-07, by Fable 5

// Package openai is the passthrough adapter for OpenAI-compatible providers:
// rewrite URL, inject key, swap the model field; the body is otherwise untouched.
package openai

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

func init() { adapter.Register("openai", OpenAI{}) }

type OpenAI struct{}

func (OpenAI) Protocol() string { return "openai" }

func (OpenAI) BuildRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest) (*http.Request, error) {
	body, err := adapter.RewriteModel(req.Raw, ep.Model)
	if err != nil {
		return nil, fmt.Errorf("rewrite model: %w", err)
	}
	url := strings.TrimRight(ep.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if ep.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+ep.APIKey)
	}
	return httpReq, nil
}

func (OpenAI) TransformBody(body io.ReadCloser, stream bool) io.ReadCloser { return body }

func (OpenAI) ClassifyError(status int, body []byte) core.ErrorClass {
	return adapter.DefaultClassify(status, body)
}
