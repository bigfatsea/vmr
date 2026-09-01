// Ver 2026-09-04, by pi (Q36 follow-up)
//
// Direct unit tests for BuildUpstreamRequest, the shared request
// construction helper the three passthrough adapters delegate to (Q36).
// The adapter-level tests in openai/anthropic/openairesponses exercise the
// happy path through each adapter; this file pins the helper's own
// boundary conditions — empty/malformed inputs, nil RoleMap, credential
// placement — that no single adapter test happens to cover.
package adapter

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"vmr/internal/core"
	"vmr/internal/jsonscan"
)

// testEp builds a minimal Endpoint with the fields BuildUpstreamRequest
// reads. FullURL is set directly (BuildRequest callers receive it
// pre-computed by ResolveURL at snapshot time; the helper never resolves).
func testEp(overrides func(*core.Endpoint)) *core.Endpoint {
	ep := &core.Endpoint{
		Provider: "p",
		FullURL:  "https://api.example.com/v1/chat/completions",
		APIKey:   "sk-test",
		Model:    "real-model",
	}
	if overrides != nil {
		overrides(ep)
	}
	return ep
}

func TestBuildUpstreamRequest_RewritesModelAndRole(t *testing.T) {
	t.Parallel()
	ep := testEp(nil)
	ep.RoleMap = map[string]string{"developer": "system"}
	raw := []byte(`{"model":"vm","stream":true,"messages":[{"role":"developer","content":"be helpful"},{"role":"user","content":"hi"}]}`)

	req, outBody, err := BuildUpstreamRequest(context.Background(), ep, &core.CanonicalRequest{Model: "vm", Raw: raw}, jsonscan.RewriteRoles, "Authorization", "Bearer "+ep.APIKey)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != ep.FullURL {
		t.Errorf("url = %s, want %s", req.URL, ep.FullURL)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-test")
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if string(outBody) != `{"model":"real-model","stream":true,"messages":[{"role":"system","content":"be helpful"},{"role":"user","content":"hi"}]}` {
		t.Errorf("model+role rewrite produced: %s", outBody)
	}
	// The returned body is exactly what the request will send.
	sent, _ := io.ReadAll(func() io.Reader { r, _ := req.GetBody(); return r }())
	if string(sent) != string(outBody) {
		t.Errorf("request body differs from returned body:\n%s\nvs\n%s", sent, outBody)
	}
}

func TestBuildUpstreamRequest_NilRoleMap(t *testing.T) {
	t.Parallel()
	// No RoleMap configured: roles must pass through untouched (nil map is
	// the "no remapping" default, not an error).
	ep := testEp(nil)
	raw := []byte(`{"model":"vm","messages":[{"role":"developer","content":"hi"}]}`)
	_, outBody, err := BuildUpstreamRequest(context.Background(), ep, &core.CanonicalRequest{Model: "vm", Raw: raw}, jsonscan.RewriteRoles, "Authorization", "Bearer "+ep.APIKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(outBody, []byte(`"role":"developer"`)) {
		t.Errorf("nil RoleMap must pass roles through untouched: %s", outBody)
	}
}

func TestBuildUpstreamRequest_NoAPIKey(t *testing.T) {
	t.Parallel()
	// An endpoint with no credential must not stamp an empty auth header —
	// the keyed credential head must be absent, not blank.
	ep := testEp(func(e *core.Endpoint) { e.APIKey = "" })
	raw := []byte(`{"model":"vm","messages":[]}`)
	req, _, err := BuildUpstreamRequest(context.Background(), ep, &core.CanonicalRequest{Model: "vm", Raw: raw}, jsonscan.RewriteRoles, "Authorization", "Bearer ")
	if err != nil {
		t.Fatal(err)
	}
	if v := req.Header.Get("Authorization"); v != "" {
		t.Errorf("empty APIKey must not set Authorization, got %q", v)
	}
}

func TestBuildUpstreamRequest_MalformedModelJSON(t *testing.T) {
	t.Parallel()
	// A body that jsonscan can't structurally scan (not a JSON object) is
	// declined with an error rather than sent mangled upstream.
	ep := testEp(nil)
	_, _, err := BuildUpstreamRequest(context.Background(), ep, &core.CanonicalRequest{Model: "vm", Raw: []byte(`not json`)}, jsonscan.RewriteRoles, "Authorization", "Bearer "+ep.APIKey)
	if err == nil {
		t.Fatal("malformed body: want error from RewriteModel, got nil")
	}
}

func TestBuildUpstreamRequest_PassthroughHeadersCopied(t *testing.T) {
	t.Parallel()
	// Headers the server layer assembled on CanonicalRequest (protocol
	// semantics + client passthrough metadata) must be copied verbatim, and
	// the client-supplied credential must never leak upstream.
	ep := testEp(nil)
	raw := []byte(`{"model":"vm","messages":[]}`)
	creq := &core.CanonicalRequest{
		Model: "vm", Raw: raw,
		Header: http.Header{
			"Anthropic-Version": {"2023-06-01"},
			"User-Agent":        {"vmr-test/1.0"},
			"Authorization":     {"Bearer client-credential-should-not-leak"},
		},
	}
	req, _, err := BuildUpstreamRequest(context.Background(), ep, creq, jsonscan.RewriteRoles, "Authorization", "Bearer "+ep.APIKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Anthropic-Version"); got != "2023-06-01" {
		t.Errorf("passthrough header lost: %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "vmr-test/1.0" {
		t.Errorf("client metadata lost: %q", got)
	}
	// The upstream credential (ep.APIKey) wins; the client's must be gone.
	if got := req.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("upstream Authorization = %q, want Bearer sk-test", got)
	}
}

func TestBuildUpstreamRequest_AnthropicCredentialHeader(t *testing.T) {
	t.Parallel()
	// Anthropic carries its credential as x-api-key, not Authorization.
	// The adapter-specific authHeaderKey/value must land where the vendor
	// expects.
	ep := testEp(nil)
	raw := []byte(`{"model":"vm","messages":[]}`)
	req, _, err := BuildUpstreamRequest(context.Background(), ep, &core.CanonicalRequest{Model: "vm", Raw: raw}, jsonscan.RewriteRoles, "x-api-key", ep.APIKey)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("x-api-key"); got != "sk-test" {
		t.Errorf("x-api-key = %q, want sk-test", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("anthropic-shaped request must not set Authorization, got %q", got)
	}
}

func TestBuildUpstreamRequest_ResponsesRoleRewrite(t *testing.T) {
	t.Parallel()
	// The openai-responses protocol rewrites roles inside the "input" array
	// rather than "messages" — the adapter passes RewriteInputRoles. This
	// pins that the helper honors the injected rewriter rather than
	// hardcoding a "messages" shape.
	ep := testEp(nil)
	ep.RoleMap = map[string]string{"developer": "system"}
	raw := []byte(`{"model":"vm","input":[{"role":"developer","content":"be helpful"},{"role":"user","content":"hi"}]}`)
	_, outBody, err := BuildUpstreamRequest(context.Background(), ep, &core.CanonicalRequest{Model: "vm", Raw: raw}, jsonscan.RewriteInputRoles, "Authorization", "Bearer "+ep.APIKey)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(outBody, []byte(`"role":"system"`)) {
		t.Errorf("input-array role not remapped: %s", outBody)
	}
	if bytes.Contains(outBody, []byte(`"role":"developer"`)) {
		t.Errorf("developer role still present in input array: %s", outBody)
	}
}
