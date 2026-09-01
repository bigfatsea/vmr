// Ver 2026-09-01, by Stan: Q36

// Shared upstream request construction. The three passthrough adapters
// (openai-completions, anthropic-messages, openai-responses) differ only in
// which role-bearing array they rewrite and how they carry the credential;
// everything else — model rewrite, request assembly, passthrough header
// copy, content-type — is identical. One helper keeps the three from
// drifting (Q36).
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"vmr/internal/core"
	"vmr/internal/jsonscan"
)

// RoleRewriter is the jsonscan role-rewrite entry point a protocol uses: its
// role-bearing array is "messages" (openai-completions, anthropic-messages)
// or "input" (openai-responses), and jsonscan exposes one function per shape.
type RoleRewriter func(raw json.RawMessage, roleMap map[string]string) ([]byte, error)

// BuildUpstreamRequest constructs the outbound HTTP request every passthrough
// adapter sends to an upstream endpoint: rewrite the model name, rewrite role
// names per the endpoint's role_map, assemble the POST, copy the passthrough
// headers the server layer assembled, then stamp Content-Type and the
// adapter's own credential. The client's Authorization/x-api-key must not
// reach the upstream (it is the VMR credential), which is why authHeaderKey/
// authHeaderValue come from the adapter, never from req.Header.
func BuildUpstreamRequest(ctx context.Context, ep *core.Endpoint, req *core.CanonicalRequest, roleRewrite RoleRewriter, authHeaderKey, authHeaderValue string) (*http.Request, []byte, error) {
	body, err := jsonscan.RewriteModel(req.Raw, ep.Model)
	if err != nil {
		return nil, nil, fmt.Errorf("rewrite model: %w", err)
	}
	if body, err = roleRewrite(body, ep.RoleMap); err != nil {
		return nil, nil, fmt.Errorf("rewrite roles: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.FullURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	// Copy the protocol+passthrough headers assembled by the server layer
	// (see chatHandler). These carry the protocol semantics (anthropic-version,
	// anthropic-beta) plus any client metadata the chatHandler didn't put on
	// the blocklist (User-Agent, X-Stainless-*, Traceparent, Accept-Language).
	for k, vs := range req.Header {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	// Content-Type and the credential come from the adapter, not the client —
	// the client's credential is the VMR one (used to authenticate against
	// VMR, not the upstream) and a client-supplied Content-Type could in
	// theory be wrong.
	httpReq.Header.Set("Content-Type", "application/json")
	if ep.APIKey != "" {
		httpReq.Header.Set(authHeaderKey, authHeaderValue)
	}
	return httpReq, body, nil
}
