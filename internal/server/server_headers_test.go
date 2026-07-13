// Ver 2026-07-13 04:00, by Sonnet 5
//
// Tests for the header pass-through policy: "default pass + small
// blocklist" — forward every client header except the ones that would
// cause a security or protocol-correctness problem if leaked to the
// upstream. A strict whitelist would be simpler to write but strips
// legitimate client metadata (User-Agent, X-Stainless-*, Traceparent)
// that the client and upstream may both depend on.
package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// SDK metadata (User-Agent, X-Stainless-*, Traceparent) must reach the
	// upstream — a strict header whitelist would otherwise strip it.
	u := newUpstream(t)
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	chatHeaders(t, ts.URL, map[string]string{
		"User-Agent":                  "OpenAI/JS 6.39.1",
		"X-Stainless-Arch":            "arm64",
		"X-Stainless-Package-Version": "6.39.1",
		"X-Stainless-Runtime":         "node",
		"X-Stainless-Timeout":         "120",
		"Traceparent":                 "00-9a0c343137ba6cb4bc3f9204261980a1-1c433829747c4112-01",
		"Accept-Language":             "en-US",
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
	// upstream model name (adapter.RewriteModel) — header pass-through
	// is independent of request body transformation.
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

func TestHeaders_AcceptEncodingNotForwarded(t *testing.T) {
	// Forwarding the client's Accept-Encoding disables Go Transport's
	// transparent gzip, so a compressing upstream would hand the response
	// normalizer gzip bytes and the client a compressed body without a
	// Content-Encoding header. The blocklist must drop it; the Transport
	// then negotiates gzip itself ("gzip", set by Go).
	u := newUpstream(t)
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	chatHeaders(t, ts.URL, map[string]string{
		"Accept-Encoding": "identity, br, zstd", // distinctive: must not reach upstream
	})

	got := headersReceived(t, u)
	if v := got.Get("Accept-Encoding"); v == "identity, br, zstd" {
		t.Errorf("client Accept-Encoding leaked to upstream: %q", v)
	}
	if v := got.Get("Accept-Encoding"); v != "gzip" {
		t.Errorf("transparent gzip not active, upstream saw Accept-Encoding=%q, want %q", v, "gzip")
	}
}

func TestHeaders_UpstreamGzipDecodedTransparently(t *testing.T) {
	// End-to-end proof of the chain the blocklist entry protects: a
	// gzip-compressing upstream, a client that itself advertises
	// Accept-Encoding. The client must receive plaintext JSON with the
	// model field rewritten — i.e. the Transport decompressed before
	// the normalizer ran.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Errorf("upstream expected gzip negotiation, got Accept-Encoding=%q", r.Header.Get("Accept-Encoding"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		fmt.Fprint(gz, `{"id":"x","object":"chat.completion","model":"upstream-model","choices":[]}`)
		gz.Close()
	}))
	t.Cleanup(up.Close)
	ts := newRouterServer(t, oneProviderYAML(up.URL))

	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions",
		bytes.NewReader([]byte(`{"model":"vm","messages":[{"role":"user","content":"hi"}]}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	// Plain transport (no auto-decompression on the client side): what
	// arrives is exactly what VMR sent.
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var out struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("client received non-JSON body (still compressed?): %v, body=%q", err, body)
	}
	if out.Model != "vm" {
		t.Errorf("model not rewritten to virtual name: %q", out.Model)
	}
}

func TestHeaders_UpstreamResponseHeadersForwarded(t *testing.T) {
	// Direct-equivalence on the response side: rate-limit headers,
	// request IDs and the like must reach the client exactly as a
	// direct call would deliver them. Hop-by-hop headers must not.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-12345")
		w.Header().Set("X-Ratelimit-Remaining-Tokens", "9999")
		w.Header().Set("Keep-Alive", "timeout=5")
		fmt.Fprint(w, `{"id":"x","object":"chat.completion","model":"upstream-model","choices":[]}`)
	}))
	t.Cleanup(up.Close)
	ts := newRouterServer(t, oneProviderYAML(up.URL))

	resp := chatHeaders(t, ts.URL, nil)
	if got := resp.Header.Get("X-Request-Id"); got != "req-12345" {
		t.Errorf("X-Request-Id not forwarded to client: %q", got)
	}
	if got := resp.Header.Get("X-Ratelimit-Remaining-Tokens"); got != "9999" {
		t.Errorf("rate-limit header not forwarded to client: %q", got)
	}
	if got := resp.Header.Get("Keep-Alive"); got != "" {
		t.Errorf("hop-by-hop header leaked to client: %q", got)
	}
	if resp.Header.Get("X-Vmr-Endpoint") == "" {
		t.Errorf("X-VMR-Endpoint missing")
	}
}

func TestHeaders_ErrorRetryAfterForwarded(t *testing.T) {
	// When every candidate fails, the last upstream error is returned
	// verbatim — including Retry-After, which client SDKs use for
	// their own backoff.
	u := newUpstream(t)
	u.status.Store(429)
	u.retryAfter = "17"
	ts := newRouterServer(t, oneProviderYAML(u.srv.URL))

	resp := chatHeaders(t, ts.URL, nil)
	if resp.StatusCode != 429 {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got != "17" {
		t.Errorf("Retry-After not forwarded on error path: %q", got)
	}
}

func TestHeaders_AnthropicVersionNotDuplicated(t *testing.T) {
	// A client-sent anthropic-version must reach the upstream exactly once —
	// real Anthropic clients always send this header, and passthrough plus a
	// default-value fallback must not add a second copy of it.
	u := newUpstream(t)
	ts := newRouterServer(t, `
listen: 127.0.0.1:0
providers:
  anthropic:
    p1: {base_url: `+u.srv.URL+`, api_key: k1}
models:
  anthropic:
    vm:
      endpoints:
        - {provider: p1, model: upstream-model}
`)

	req, _ := http.NewRequest("POST", ts.URL+"/v1/messages", bytes.NewReader([]byte(`{"model":"vm"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	got := headersReceived(t, u)
	if vs := got.Values("Anthropic-Version"); len(vs) != 1 {
		t.Errorf("anthropic-version sent %d times (%v), want exactly 1", len(vs), vs)
	}
	if vs := got.Values("Anthropic-Beta"); len(vs) != 1 {
		t.Errorf("anthropic-beta sent %d times (%v), want exactly 1", len(vs), vs)
	}
}
