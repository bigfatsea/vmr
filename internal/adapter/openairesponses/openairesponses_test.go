// Ver 2026-08-02 12:30, by Sonnet 5
package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"vmr/internal/core"
)

func TestBuildRequestRewritesModelAndKeepsUnknownFields(t *testing.T) {
	raw := []byte(`{"model":"coding","stream":true,"input":[{"role":"user","content":"hi"}],"some_future_param":{"x":[1,2]}}`)
	ep := &core.Endpoint{Provider: "p", BaseURL: "https://api.example.com/v1/", APIKey: "sk-1", Model: "real-model"}
	ep.FullURL = OpenAIResponses{}.ResolveURL(ep.BaseURL)
	req, outBody, err := OpenAIResponses{}.BuildRequest(context.Background(), ep, &core.CanonicalRequest{Model: "coding", Stream: true, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "https://api.example.com/v1/responses" {
		t.Errorf("url: %s", req.URL)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer sk-1" {
		t.Errorf("auth header: %q", got)
	}
	var m map[string]json.RawMessage
	body, _ := req.GetBody()
	if err := json.NewDecoder(body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	var model string
	json.Unmarshal(m["model"], &model)
	if model != "real-model" {
		t.Errorf("model not rewritten: %q", model)
	}
	if string(m["some_future_param"]) != `{"x":[1,2]}` {
		t.Errorf("unknown field mangled: %s", m["some_future_param"])
	}
	if string(m["stream"]) != "true" {
		t.Errorf("stream field mangled: %s", m["stream"])
	}
	// The returned body must be exactly what the request will send.
	sent, _ := io.ReadAll(func() io.Reader { r, _ := req.GetBody(); return r }())
	if !bytes.Equal(outBody, sent) {
		t.Errorf("returned body differs from request body:\n%s\nvs\n%s", outBody, sent)
	}
}

// TestBuildRequestAppliesRoleMap closes the same gap
// openai_test.go's TestBuildRequestAppliesRoleMap does for the Chat
// Completions adapter, but on Responses' "input" array — proving
// RewriteInputRoles (not RewriteRoles) is what's actually wired into this
// adapter's BuildRequest.
func TestBuildRequestAppliesRoleMap(t *testing.T) {
	raw := []byte(`{"model":"vm","input":[{"role":"developer","content":"be helpful"},{"role":"user","content":"hi"}]}`)

	mapped := &core.Endpoint{Provider: "p", BaseURL: "https://api.example.com/v1", APIKey: "sk-1", Model: "m", RoleMap: map[string]string{"developer": "system"}}
	mapped.FullURL = OpenAIResponses{}.ResolveURL(mapped.BaseURL)
	_, outBody, err := OpenAIResponses{}.BuildRequest(context.Background(), mapped, &core.CanonicalRequest{Model: "vm", Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(outBody, []byte(`"role":"system"`)) {
		t.Errorf("developer role not remapped: %s", outBody)
	}
	if bytes.Contains(outBody, []byte(`"developer"`)) {
		t.Errorf("developer role still present: %s", outBody)
	}

	plain := &core.Endpoint{Provider: "p", BaseURL: "https://api.example.com/v1", APIKey: "sk-1", Model: "m"} // no RoleMap
	plain.FullURL = OpenAIResponses{}.ResolveURL(plain.BaseURL)
	_, outBody, err = OpenAIResponses{}.BuildRequest(context.Background(), plain, &core.CanonicalRequest{Model: "vm", Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(outBody, []byte(`"role":"developer"`)) {
		t.Errorf("without a configured RoleMap, developer role must pass through untouched: %s", outBody)
	}
}

// TestBuildRequestStringInput proves a bare-string "input" (Responses'
// simplest valid request shape — no message array at all) survives
// BuildRequest unchanged apart from the model splice: RewriteInputRoles
// must not choke on (or misinterpret) a non-array input value.
func TestBuildRequestStringInput(t *testing.T) {
	raw := []byte(`{"model":"vm","input":"hello there"}`)
	ep := &core.Endpoint{Provider: "p", BaseURL: "https://api.example.com/v1", APIKey: "sk-1", Model: "real-model", RoleMap: map[string]string{"developer": "system"}}
	ep.FullURL = OpenAIResponses{}.ResolveURL(ep.BaseURL)
	_, outBody, err := OpenAIResponses{}.BuildRequest(context.Background(), ep, &core.CanonicalRequest{Model: "vm", Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(outBody, []byte(`"input":"hello there"`)) {
		t.Errorf("string input mangled: %s", outBody)
	}
}

func TestResolveURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://api.example.com/v1", "https://api.example.com/v1/responses"},
		{"https://api.example.com/v1/", "https://api.example.com/v1/responses"},
	}
	for _, c := range cases {
		if got := (OpenAIResponses{}).ResolveURL(c.in); got != c.want {
			t.Errorf("ResolveURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestProtocol(t *testing.T) {
	if got := (OpenAIResponses{}).Protocol(); got != "openai-responses" {
		t.Errorf("Protocol() = %q", got)
	}
}

// TestClassifyError mirrors openai's TestClassifyError with a smaller case
// set: this adapter starts as a pure DefaultClassify passthrough (see the
// doc comment on ClassifyError), so this is really a regression net that it
// stays wired to DefaultClassify, not an independent quirk table.
func TestClassifyError(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   core.ErrorClass
	}{
		{400, `{"error":{"message":"invalid temperature"}}`, core.ErrClient},
		{401, `{}`, core.ErrAuth},
		{404, `{"error":{"message":"model not found"}}`, core.ErrEndpoint},
		{429, `{"error":{"message":"rate limit exceeded, slow down"}}`, core.ErrRateLimit},
		{500, `{}`, core.ErrTransient},
	}
	for _, c := range cases {
		if got := (OpenAIResponses{}).ClassifyError(c.status, []byte(c.body)); got != c.want {
			t.Errorf("status=%d body=%s: got %v want %v", c.status, c.body, got, c.want)
		}
	}
}
