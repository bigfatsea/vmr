// Ver 2026-07-07, by Fable 5
package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"vmr/internal/core"
)

func TestBuildRequest(t *testing.T) {
	raw := []byte(`{"model":"claude","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}],"future_param":1}`)
	ep := &core.Endpoint{Provider: "p", BaseURL: "https://api.deepseek.com/anthropic/v1/", APIKey: "sk-1", Model: "deepseek-v4-pro"}

	req, outBody, err := Anthropic{}.BuildRequest(context.Background(), ep, &core.CanonicalRequest{Model: "claude", Stream: true, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if len(outBody) == 0 {
		t.Fatal("BuildRequest returned empty outbound body")
	}
	if req.URL.String() != "https://api.deepseek.com/anthropic/v1/messages" {
		t.Errorf("url: %s", req.URL)
	}
	if got := req.Header.Get("x-api-key"); got != "sk-1" {
		t.Errorf("x-api-key: %q", got)
	}
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("default version: %q", got)
	}
	var m map[string]json.RawMessage
	body, _ := req.GetBody()
	if err := json.NewDecoder(body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	var model string
	json.Unmarshal(m["model"], &model)
	if model != "deepseek-v4-pro" {
		t.Errorf("model not rewritten: %q", model)
	}
	if string(m["future_param"]) != "1" || string(m["max_tokens"]) != "16" {
		t.Error("unknown/other fields mangled")
	}
}

func TestBuildRequestForwardsProtocolHeaders(t *testing.T) {
	hdr := http.Header{}
	hdr.Set("anthropic-version", "2024-10-22")
	hdr.Set("anthropic-beta", "context-1m-2025-08-07")
	ep := &core.Endpoint{Provider: "p", BaseURL: "https://x.example/v1", APIKey: "k", Model: "m"}
	req, _, err := Anthropic{}.BuildRequest(context.Background(), ep,
		&core.CanonicalRequest{Model: "vm", Raw: []byte(`{"model":"vm"}`), Header: hdr})
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("anthropic-version"); got != "2024-10-22" {
		t.Errorf("version not forwarded: %q", got)
	}
	if got := req.Header.Get("anthropic-beta"); got != "context-1m-2025-08-07" {
		t.Errorf("beta not forwarded: %q", got)
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   core.ErrorClass
	}{
		{400, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens required"}}`, core.ErrClient},
		// DeepSeek anthropic endpoint, live-captured wording for a wrong model name:
		{400, `{"error":{"message":"The supported API model names are deepseek-v4-pro or deepseek-v4-flash, but you passed no-such."}}`, core.ErrEndpoint},
		{401, `{"type":"error","error":{"type":"authentication_error","message":"login fail"}}`, core.ErrAuth},
		{404, `{"type":"error","error":{"type":"not_found_error","message":"model not found"}}`, core.ErrEndpoint},
		{429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, core.ErrRateLimit},
		{500, `{}`, core.ErrTransient},
		{529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, core.ErrTransient},
	}
	for _, c := range cases {
		if got := (Anthropic{}).ClassifyError(c.status, []byte(c.body)); got != c.want {
			t.Errorf("status=%d body=%s: got %v want %v", c.status, c.body, got, c.want)
		}
	}
}

func TestProtocol(t *testing.T) {
	if (Anthropic{}).Protocol() != "anthropic" {
		t.Error("protocol")
	}
}
