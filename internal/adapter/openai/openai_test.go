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

func TestClassifyError(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   core.ErrorClass
	}{
		{400, `{"error":{"message":"invalid temperature"}}`, core.ErrClient},
		{400, `{"error":{"message":"invalid params, unknown model 'x' (2013)"}}`, core.ErrEndpoint}, // MiniMax style
		{401, `{}`, core.ErrAuth},
		{402, `{"error":{"message":"Insufficient credits"}}`, core.ErrEndpoint}, // OpenRouter style
		{403, `{}`, core.ErrAuth},
		{404, `{"error":{"message":"model not found"}}`, core.ErrEndpoint},
		{408, `{}`, core.ErrTransient},
		{413, `{"error":{"message":"payload too large"}}`, core.ErrClient},
		{422, `{"error":{"message":"model does not exist"}}`, core.ErrEndpoint},
		{429, `{"error":{"message":"rate limit exceeded, slow down"}}`, core.ErrRateLimit},
		// Content-policy flags: switch endpoints but never punish health.
		{403, `{"error":{"message":"Your input was flagged","metadata":{"reasons":["hate"],"flagged_input":"..."}}}`, core.ErrContent}, // OpenRouter moderation
		{403, `{"error":{"message":"Request blocked by guardrail: prompt-injection"}}`, core.ErrContent},                               // OpenRouter guardrail
		{400, `{"error":{"message":"Content Exists Risk"}}`, core.ErrContent},                                                          // DeepSeek content filter
		{400, `{"type":"error","error":{"message":"invalid params, output content violation (1027)"}}`, core.ErrContent},               // MiniMax style
		{400, `{"error":{"message":"输入包含敏感内容，请修改后重试"}}`, core.ErrContent},
		{402, `{"error":{"message":"request rejected by content moderation"}}`, core.ErrContent},             // content wording wins over the bare 402→ErrEndpoint rule
		{404, `{"error":{"message":"resource not found due to content policy violation"}}`, core.ErrContent}, // same for 404
		{451, `{}`, core.ErrContent},
		{429, `{"error":{"message":"you have exceeded your quota"}}`, core.ErrEndpoint},
		{429, `{"error":{"message":"insufficient balance"}}`, core.ErrEndpoint},
		{500, `{}`, core.ErrTransient},
		{502, `{}`, core.ErrTransient},
		{503, `{}`, core.ErrTransient},
	}
	for _, c := range cases {
		if got := (OpenAI{}).ClassifyError(c.status, []byte(c.body)); got != c.want {
			t.Errorf("status=%d body=%s: got %v want %v", c.status, c.body, got, c.want)
		}
	}
}
