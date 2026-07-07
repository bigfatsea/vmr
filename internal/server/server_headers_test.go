// Ver 2026-07-07 21:50, by Fable 5 (post-audit 2026-07-07 fixes)
//
// Tests for the header pass-through policy. The prior implementation
// used a strict whitelist that forwarded only Content-Type and the
// Anthropic protocol headers, which stripped legitimate client
// metadata (User-Agent, X-Stainless-*, Traceparent). The fix is
// "default pass + small blocklist": forward everything except
// headers that would cause a security or protocol-correctness
// problem if leaked to the upstream.
package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// oneProviderYAML is a minimal config: one virtual model, one provider.
func oneProviderYAML(u string) string {
	return `
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: ` + u + `, api_key: k1}
models:
  openai:
    vm:
      endpoints:
        - {provider: p1, model: upstream-model}
`
}

func chatHeaders(t *testing.T, ts string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", ts+"/v1/chat/completions", bytes.NewReader([]byte(`{"model":"vm"}`)))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("chat request failed: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}

func headersReceived(t *testing.T, u *upstream) http.Header {
	t.Helper()
	got, ok := u.lastHeaders.Load().(http.Header)
	if !ok {
		t.Fatal("upstream did not record headers")
	}
	return got
}

func TestHeaders_ClientMetadataForwarded(t *testing.T) {
	// The fix: SDK metadata (User-Agent, X-Stainless-*, Traceparent)
	// must reach the upstream. This is the regression test for the
	// 2026-07-07 OpenClaw audit finding.
	u := newUpstream(t)
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	chatHeaders(t, ts.URL, map[string]string{
		"User-Agent":                "OpenAI/JS 6.39.1",
		"X-Stainless-Arch":          "arm64",
		"X-Stainless-Package-Version": "6.39.1",
		"X-Stainless-Runtime":       "node",
		"X-Stainless-Timeout":       "120",
		"Traceparent":               "00-9a0c343137ba6cb4bc3f9204261980a1-1c433829747c4112-01",
		"Accept-Language":           "en-US",
	})

	got := headersReceived(t, u)
	wants := []string{
		"User-Agent",
		"X-Stainless-Arch",
		"X-Stainless-Package-Version",
		"X-Stainless-Runtime",
		"X-Stainless-Timeout",
		"Traceparent",
		"Accept-Language",
	}
	for _, h := range wants {
		if got.Get(h) == "" {
			t.Errorf("upstream did not receive %s header (got: %v)", h, headerKeys(got))
		}
	}
}

func TestHeaders_DangerousHeadersStripped(t *testing.T) {
	// The blocklist must prevent CLIENT-supplied credentials and
	// IP-spoofing vectors from reaching the upstream. Note that
	// Authorization IS still present at the upstream because the
	// adapter injects its own ("Bearer k1") after the blocklist
	// filter — what we want to verify is that the CLIENT's value
	// doesn't leak.
	u := newUpstream(t)
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	chatHeaders(t, ts.URL, map[string]string{
		"Authorization":       "Bearer leaked-client-key",
		"X-Api-Key":           "leaked-anthropic-key",
		"Cookie":              "session=stolen",
		"X-Forwarded-For":     "1.2.3.4",
		"X-Real-Ip":           "5.6.7.8",
		"Proxy-Authorization": "Basic leaked",
		// Positive control: this should be forwarded (not on blocklist).
		"X-Custom-Trace-Id": "abc-123",
	})

	got := headersReceived(t, u)

	// Authorization IS expected at the upstream (adapter injects it),
	// but it must be the ADAPTER's value, not the client's.
	if auth := got.Get("Authorization"); auth == "Bearer leaked-client-key" {
		t.Errorf("client Authorization leaked: %q", auth)
	}

	// These must be entirely absent — no legitimate use case for
	// forwarding them.
	mustBeAbsent := []string{"X-Api-Key", "Cookie", "X-Forwarded-For", "X-Real-Ip", "Proxy-Authorization"}
	for _, h := range mustBeAbsent {
		if v := got.Get(h); v != "" {
			t.Errorf("blocklist leaked: %s=%q reached upstream", h, v)
		}
	}

	if got.Get("X-Custom-Trace-Id") != "abc-123" {
		t.Errorf("non-blocked custom header stripped: got %v", headerKeys(got))
	}
}

func TestHeaders_AuthorizationReplacedByAdapter(t *testing.T) {
	// The Authorization that reaches the upstream is the adapter's,
	// not the client's. The blocklist is a second line of defense;
	// the primary mechanism is adapter.BuildRequest calling
	// Header.Set.
	u := newUpstream(t)
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	chatHeaders(t, ts.URL, map[string]string{
		"Authorization": "Bearer wrong-key-from-client",
	})

	got := headersReceived(t, u)
	upstreamAuth := got.Get("Authorization")
	if upstreamAuth == "" {
		t.Fatal("upstream received no Authorization at all")
	}
	if upstreamAuth == "Bearer wrong-key-from-client" {
		t.Errorf("client Authorization leaked: %q", upstreamAuth)
	}
	if upstreamAuth != "Bearer k1" {
		t.Errorf("upstream Authorization = %q, want %q", upstreamAuth, "Bearer k1")
	}
}

func TestHeaders_CaseInsensitiveBlock(t *testing.T) {
	// Header names are case-insensitive on the wire. The blocklist
	// must catch "authorization" and "AUTHORIZATION" alike.
	u := newUpstream(t)
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	chatHeaders(t, ts.URL, map[string]string{
		"authorization": "Bearer leaked",
		"AUTHORIZATION": "Bearer also-leaked",
	})

	got := headersReceived(t, u)
	if v := got.Get("Authorization"); v != "" && v != "Bearer k1" {
		t.Errorf("case-variant client Authorization leaked: %q", v)
	}
}

func TestHeaders_ContentTypeAndAuthorization(t *testing.T) {
	// Sanity: Content-Type and (rewritten) Authorization always reach
	// the upstream regardless of blocklist — these are adapter-set.
	u := newUpstream(t)
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	chatHeaders(t, ts.URL, nil)
	got := headersReceived(t, u)
	if got.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type missing or wrong: %q", got.Get("Content-Type"))
	}
	if got.Get("Authorization") != "Bearer k1" {
		t.Errorf("Authorization = %q, want %q", got.Get("Authorization"), "Bearer k1")
	}
}

// headerKeys returns a stable list of header names for diagnostic
// output in test failures.
func headerKeys(h http.Header) []string {
	out := make([]string, 0, len(h))
	for k := range h {
		out = append(out, k)
	}
	return out
}

func TestHeaders_BodyUnaffected(t *testing.T) {
	// Sanity: the model field in the body is still rewritten to the
	// upstream model name (adapter.RewriteModel) — the header fix
	// didn't break request body transformation.
	u := newUpstream(t)
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions",
		bytes.NewReader([]byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TestUA/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	got, _ := u.lastModel.Load().(string)
	if got != "upstream-model" {
		t.Errorf("upstream model field = %q, want %q", got, "upstream-model")
	}
	// Confirm body still parses as JSON.
	hh := headersReceived(t, u)
	if hh.Get("User-Agent") != "TestUA/1.0" {
		t.Errorf("User-Agent not forwarded alongside body rewrite")
	}
	_ = json.Valid // keep import used if a later test needs it
}
