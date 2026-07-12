// Ver 2026-07-12 17:00, by Fable 5
package config

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

const proxyTestModels = `
models:
  openai:
    m: {endpoints: [{provider: p1, model: x}]}
`

func TestProxyConfigValidation(t *testing.T) {
	base := `
listen: 127.0.0.1:0
providers:
  openai:
    p1: {base_url: https://example.com, api_key: k}
` + proxyTestModels

	if _, err := Parse([]byte("https_proxy: http://127.0.0.1:7890\n" + base)); err != nil {
		t.Errorf("valid https_proxy rejected: %v", err)
	}
	if _, err := Parse([]byte("https_proxy: not a url\n" + base)); err == nil ||
		!strings.Contains(err.Error(), "https_proxy") {
		t.Errorf("invalid https_proxy accepted (err=%v)", err)
	}
	if _, err := Parse([]byte("http_proxy: '127.0.0.1:7890'\n" + base)); err == nil {
		t.Error("scheme-less http_proxy accepted")
	}
}

func TestProxyConfigEnvExpansion(t *testing.T) {
	t.Setenv("VMR_TEST_PROXY", "http://10.0.0.1:8080")
	cfg, err := Parse([]byte(`
listen: 127.0.0.1:0
https_proxy: ${VMR_TEST_PROXY}
providers:
  openai:
    p1: {base_url: https://example.com, api_key: k}
` + proxyTestModels))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPSProxy != "http://10.0.0.1:8080" {
		t.Errorf("https_proxy not expanded from env: %q", cfg.HTTPSProxy)
	}
}

func TestProviderProxySwitchParsing(t *testing.T) {
	cfg, err := Parse([]byte(`
listen: 127.0.0.1:0
https_proxy: http://127.0.0.1:7890
providers:
  openai:
    on:     {base_url: https://a.example, api_key: k, proxy: true}
    off:    {base_url: https://b.example, api_key: k, proxy: false}
    unset:  {base_url: https://c.example, api_key: k}
models:
  openai:
    m: {endpoints: [{provider: on, model: x}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	ps := cfg.Providers["openai"]
	if p := ps["on"].Proxy; p == nil || !*p {
		t.Error("proxy: true not parsed")
	}
	if p := ps["off"].Proxy; p == nil || *p {
		t.Error("proxy: false not parsed")
	}
	if ps["unset"].Proxy != nil {
		t.Error("absent proxy switch should stay nil (tri-state)")
	}
}

func TestProxySpecFor(t *testing.T) {
	cfg := &Config{HTTPProxy: "http://hp:1", HTTPSProxy: "http://sp:2"}
	cases := []struct {
		name     string
		p        Provider
		wantMode string
		wantURL  string
	}{
		{"provider off beats everything", Provider{BaseURL: "https://x", Proxy: boolPtr(false)}, ProxyDirect, ""},
		{"https base uses https_proxy", Provider{BaseURL: "https://x"}, ProxyURL, "http://sp:2"},
		{"http base uses http_proxy", Provider{BaseURL: "http://x"}, ProxyURL, "http://hp:1"},
		{"explicit true same as unset", Provider{BaseURL: "https://x", Proxy: boolPtr(true)}, ProxyURL, "http://sp:2"},
	}
	for _, c := range cases {
		mode, u := cfg.ProxySpecFor(c.p)
		if mode != c.wantMode || u != c.wantURL {
			t.Errorf("%s: got (%s, %q), want (%s, %q)", c.name, mode, u, c.wantMode, c.wantURL)
		}
	}
	// No config proxy → direct. There is no environment fallback.
	empty := &Config{}
	if mode, _ := empty.ProxySpecFor(Provider{BaseURL: "https://x"}); mode != ProxyDirect {
		t.Errorf("no config proxy: got %s, want %s", mode, ProxyDirect)
	}
	// https base with only http_proxy set → direct (scheme-matched).
	onlyHTTP := &Config{HTTPProxy: "http://hp:1"}
	if mode, _ := onlyHTTP.ProxySpecFor(Provider{BaseURL: "https://x"}); mode != ProxyDirect {
		t.Errorf("scheme mismatch must mean direct, got %s", mode)
	}
}

func TestProxyTrueRequiresGlobalProxy(t *testing.T) {
	mk := func(head, providerLine string) []byte {
		return []byte(head + `
listen: 127.0.0.1:0
providers:
  openai:
    p1: ` + providerLine + `
` + proxyTestModels)
	}
	// proxy: true with no global proxy at all → rejected at load.
	if _, err := Parse(mk("", `{base_url: https://a.example, api_key: k, proxy: true}`)); err == nil ||
		!strings.Contains(err.Error(), "proxy: true") {
		t.Errorf("proxy: true without a global proxy accepted (err=%v)", err)
	}
	// proxy: true with a matching global proxy → fine.
	if _, err := Parse(mk("https_proxy: http://127.0.0.1:7890", `{base_url: https://a.example, api_key: k, proxy: true}`)); err != nil {
		t.Errorf("proxy: true with https_proxy rejected: %v", err)
	}
	// proxy: true on an http base_url with only https_proxy set → still a contradiction.
	if _, err := Parse(mk("https_proxy: http://127.0.0.1:7890", `{base_url: http://10.0.0.9:8000, api_key: k, proxy: true}`)); err == nil {
		t.Error("proxy: true with a scheme-mismatched global proxy accepted")
	}
}
