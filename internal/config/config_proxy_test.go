// Ver 2026-08-02, by Sonnet 5
package config

import (
	"strings"
	"testing"
)

const proxyTestModels = `
models:
  m: {endpoints: [{protocol: openai, provider: p1, models: [x]}]}
`

func TestProxyConfigValidation(t *testing.T) {
	base := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://example.com}, api_key: k}
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
  - {name: p1, base_url: {openai: https://example.com}, api_key: k}
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
  - {name: on, base_url: {openai: https://a.example}, api_key: k, proxy: true}
  - {name: off, base_url: {openai: https://b.example}, api_key: k, proxy: false}
  - {name: unset, base_url: {openai: https://c.example}, api_key: k}
models:
  m: {endpoints: [{protocol: openai, provider: on, models: [x]}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	on, _ := cfg.ProviderByName("on")
	off, _ := cfg.ProviderByName("off")
	unset, _ := cfg.ProviderByName("unset")
	if !on.Proxy {
		t.Error("proxy: true not parsed")
	}
	if off.Proxy {
		t.Error("proxy: false not parsed")
	}
	if unset.Proxy {
		t.Error("absent proxy switch should default to false (direct)")
	}
}

func TestProxySpecFor(t *testing.T) {
	t.Parallel()
	mkP := func(baseURL string, proxy bool) Provider {
		return Provider{BaseURL: map[string]string{"openai": baseURL}, Proxy: proxy}
	}
	// There is no global default to inherit: each provider's own switch is
	// the entire decision, false (the default) meaning direct.
	cfg := &Config{HTTPProxy: "http://hp:1", HTTPSProxy: "http://sp:2"}
	cases := []struct {
		name     string
		p        Provider
		wantMode string
		wantURL  string
	}{
		{"provider proxy unset/false stays direct", mkP("https://x", false), ProxyDirect, ""},
		{"provider proxy:true opts in over https", mkP("https://x", true), ProxyURL, "http://sp:2"},
		{"provider proxy:true opts in over http", mkP("http://x", true), ProxyURL, "http://hp:1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			mode, u := cfg.ProxySpecFor(c.p, "openai")
			if mode != c.wantMode || u != c.wantURL {
				t.Errorf("got (%s, %q), want (%s, %q)", mode, u, c.wantMode, c.wantURL)
			}
		})
	}

	// No config proxy → direct. There is no environment fallback.
	empty := &Config{}
	if mode, _ := empty.ProxySpecFor(mkP("https://x", false), "openai"); mode != ProxyDirect {
		t.Errorf("no config proxy: got %s, want %s", mode, ProxyDirect)
	}
	// https base with only http_proxy set, provider opted in → direct (scheme-matched).
	onlyHTTP := &Config{HTTPProxy: "http://hp:1"}
	if mode, _ := onlyHTTP.ProxySpecFor(mkP("https://x", true), "openai"); mode != ProxyDirect {
		t.Errorf("scheme mismatch must mean direct, got %s", mode)
	}
}

func TestProxyTrueRequiresMatchingURL(t *testing.T) {
	mk := func(head, providerFields string) []byte {
		return []byte(head + `
listen: 127.0.0.1:0
providers:
  - {name: p1, ` + providerFields + `}
` + proxyTestModels)
	}
	// proxy: true with no proxy URL at all → rejected at load.
	if _, err := Parse(mk("", `base_url: {openai: https://a.example}, api_key: k, proxy: true`)); err == nil ||
		!strings.Contains(err.Error(), "proxy: true") {
		t.Errorf("proxy: true without a proxy URL accepted (err=%v)", err)
	}
	// proxy: true with a matching proxy URL → fine.
	if _, err := Parse(mk("https_proxy: http://127.0.0.1:7890", `base_url: {openai: https://a.example}, api_key: k, proxy: true`)); err != nil {
		t.Errorf("proxy: true with https_proxy rejected: %v", err)
	}
	// proxy: true on an http base_url with only https_proxy set → still a contradiction.
	if _, err := Parse(mk("https_proxy: http://127.0.0.1:7890", `base_url: {openai: http://10.0.0.9:8000}, api_key: k, proxy: true`)); err == nil {
		t.Error("proxy: true with a scheme-mismatched proxy accepted")
	}
}

// TestProxyDefaultOff locks in the design: http_proxy/https_proxy alone
// only declare the proxy server's URL — they must not turn proxying on for
// a provider that never opted in with its own proxy: true.
func TestProxyDefaultOff(t *testing.T) {
	cfg, err := Parse([]byte(`
listen: 127.0.0.1:0
https_proxy: http://127.0.0.1:7890
providers:
  - {name: p1, base_url: {openai: https://a.example}, api_key: k}
` + proxyTestModels))
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := cfg.ProviderByName("p1")
	if mode, _ := cfg.ProxySpecFor(p1, "openai"); mode != ProxyDirect {
		t.Errorf("https_proxy set with no provider proxy:true: got %s, want %s (proxy default must stay off)", mode, ProxyDirect)
	}
}
