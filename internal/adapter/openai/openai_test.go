// Ver 2026-07-07 02:20, by Fable 5
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"vmr/internal/core"
)

func TestBuildRequestRewritesModelAndKeepsUnknownFields(t *testing.T) {
	raw := []byte(`{"model":"coding","stream":true,"messages":[{"role":"user","content":"hi"}],"some_future_param":{"x":[1,2]}}`)
	ep := &core.Endpoint{Provider: "p", BaseURL: "https://api.example.com/v1/", APIKey: "sk-1", Model: "real-model"}
	ep.FullURL = OpenAI{}.ResolveURL(ep.BaseURL)
	req, outBody, err := OpenAI{}.BuildRequest(context.Background(), ep, &core.CanonicalRequest{Model: "coding", Stream: true, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "https://api.example.com/v1/chat/completions" {
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

// TestBuildRequestAppliesRoleMap closes the gap between internal/jsonscan's
// coverage of RewriteRoles itself and its actual wiring into BuildRequest —
// an endpoint with a configured RoleMap must see it applied to the outbound
// body, and one without must not.
func TestBuildRequestAppliesRoleMap(t *testing.T) {
	raw := []byte(`{"model":"vm","messages":[{"role":"developer","content":"be helpful"},{"role":"user","content":"hi"}]}`)

	mapped := &core.Endpoint{Provider: "p", BaseURL: "https://api.example.com/v1", APIKey: "sk-1", Model: "m", RoleMap: map[string]string{"developer": "system"}}
	mapped.FullURL = OpenAI{}.ResolveURL(mapped.BaseURL)
	_, outBody, err := OpenAI{}.BuildRequest(context.Background(), mapped, &core.CanonicalRequest{Model: "vm", Raw: raw})
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
	plain.FullURL = OpenAI{}.ResolveURL(plain.BaseURL)
	_, outBody, err = OpenAI{}.BuildRequest(context.Background(), plain, &core.CanonicalRequest{Model: "vm", Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(outBody, []byte(`"role":"developer"`)) {
		t.Errorf("without a configured RoleMap, developer role must pass through untouched: %s", outBody)
	}
}

// TestClassifyError verifies that OpenAI adapter delegates its error
// classification to adapter.DefaultClassify. The full error classification
// matrix (covering all status codes and vendor-specific quirks) is tested
// in internal/adapter/classify_test.go.
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
		if got := (OpenAI{}).ClassifyError(c.status, []byte(c.body)); got != c.want {
			t.Errorf("status=%d body=%s: got %v want %v", c.status, c.body, got, c.want)
		}
	}
}
