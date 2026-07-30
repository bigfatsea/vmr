// Ver 2026-07-30, by Sonnet 5
package config

import (
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

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
	if p := on.Proxy; p == nil || !*p {
		t.Error("proxy: true not parsed")
	}
	if p := off.Proxy; p == nil || *p {
		t.Error("proxy: false not parsed")
	}
	if unset.Proxy != nil {
		t.Error("absent proxy switch should stay nil (tri-state)")
	}
}

func TestProxySpecFor(t *testing.T) {
	mkP := func(baseURL string, proxy *bool) Provider {
		return Provider{BaseURL: map[string]string{"openai": baseURL}, Proxy: proxy}
	}
	// Global proxy default off (the new recommended baseline): an unset
	// per-provider switch means direct, even with proxy URLs configured.
	off := &Config{HTTPProxy: "http://hp:1", HTTPSProxy: "http://sp:2"}
	offCases := []struct {
		name     string
		p        Provider
		wantMode string
		wantURL  string
	}{
		{"global default off: unset provider stays direct", mkP("https://x", nil), ProxyDirect, ""},
		{"provider proxy:false stays direct", mkP("https://x", boolPtr(false)), ProxyDirect, ""},
		{"provider proxy:true opts in over https", mkP("https://x", boolPtr(true)), ProxyURL, "http://sp:2"},
		{"provider proxy:true opts in over http", mkP("http://x", boolPtr(true)), ProxyURL, "http://hp:1"},
	}
	for _, c := range offCases {
		mode, u := off.ProxySpecFor(c.p, "openai")
		if mode != c.wantMode || u != c.wantURL {
			t.Errorf("%s: got (%s, %q), want (%s, %q)", c.name, mode, u, c.wantMode, c.wantURL)
		}
	}

	// Global proxy default on: an unset per-provider switch now follows it;
	// a provider's own switch still overrides either way.
	on := &Config{Proxy: true, HTTPProxy: "http://hp:1", HTTPSProxy: "http://sp:2"}
	onCases := []struct {
		name     string
		p        Provider
		wantMode string
		wantURL  string
	}{
		{"global default on: unset provider follows it (https)", mkP("https://x", nil), ProxyURL, "http://sp:2"},
		{"global default on: unset provider follows it (http)", mkP("http://x", nil), ProxyURL, "http://hp:1"},
		{"provider off beats global default on", mkP("https://x", boolPtr(false)), ProxyDirect, ""},
		{"provider true redundant with global default on", mkP("https://x", boolPtr(true)), ProxyURL, "http://sp:2"},
	}
	for _, c := range onCases {
		mode, u := on.ProxySpecFor(c.p, "openai")
		if mode != c.wantMode || u != c.wantURL {
			t.Errorf("%s: got (%s, %q), want (%s, %q)", c.name, mode, u, c.wantMode, c.wantURL)
		}
	}

	// No config proxy → direct. There is no environment fallback.
	empty := &Config{}
	if mode, _ := empty.ProxySpecFor(mkP("https://x", nil), "openai"); mode != ProxyDirect {
		t.Errorf("no config proxy: got %s, want %s", mode, ProxyDirect)
	}
	// https base with only http_proxy set, provider opted in → direct (scheme-matched).
	onlyHTTP := &Config{Proxy: true, HTTPProxy: "http://hp:1"}
	if mode, _ := onlyHTTP.ProxySpecFor(mkP("https://x", nil), "openai"); mode != ProxyDirect {
		t.Errorf("scheme mismatch must mean direct, got %s", mode)
	}
}

func TestProxyTrueRequiresGlobalProxy(t *testing.T) {
	mk := func(head, providerFields string) []byte {
		return []byte(head + `
listen: 127.0.0.1:0
providers:
  - {name: p1, ` + providerFields + `}
` + proxyTestModels)
	}
	// proxy: true with no global proxy at all → rejected at load.
	if _, err := Parse(mk("", `base_url: {openai: https://a.example}, api_key: k, proxy: true`)); err == nil ||
		!strings.Contains(err.Error(), "proxy: true") {
		t.Errorf("proxy: true without a global proxy accepted (err=%v)", err)
	}
	// proxy: true with a matching global proxy → fine.
	if _, err := Parse(mk("https_proxy: http://127.0.0.1:7890", `base_url: {openai: https://a.example}, api_key: k, proxy: true`)); err != nil {
		t.Errorf("proxy: true with https_proxy rejected: %v", err)
	}
	// proxy: true on an http base_url with only https_proxy set → still a contradiction.
	if _, err := Parse(mk("https_proxy: http://127.0.0.1:7890", `base_url: {openai: http://10.0.0.9:8000}, api_key: k, proxy: true`)); err == nil {
		t.Error("proxy: true with a scheme-mismatched global proxy accepted")
	}
}

// TestGlobalProxyDefaultOff locks in the recommended shape: http_proxy/
// https_proxy alone only declare the proxy server's URL — they must not
// turn proxying on for a provider that never opted in.
func TestGlobalProxyDefaultOff(t *testing.T) {
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
		t.Errorf("https_proxy set with no proxy:true anywhere: got %s, want %s (proxy default must stay off)", mode, ProxyDirect)
	}
}

// TestGlobalProxyTrueAppliesToUnsetProviders verifies the opt-in global
// switch: once proxy: true is set, providers without their own switch
// follow it, and a provider's own proxy: false still carves out an
// exception.
func TestGlobalProxyTrueAppliesToUnsetProviders(t *testing.T) {
	cfg, err := Parse([]byte(`
listen: 127.0.0.1:0
proxy: true
https_proxy: http://127.0.0.1:7890
providers:
  - {name: p1, base_url: {openai: https://a.example}, api_key: k}
  - {name: p2, base_url: {openai: https://b.example}, api_key: k, proxy: false}
` + proxyTestModels))
	if err != nil {
		t.Fatal(err)
	}
	p1, _ := cfg.ProviderByName("p1")
	p2, _ := cfg.ProviderByName("p2")
	if mode, u := cfg.ProxySpecFor(p1, "openai"); mode != ProxyURL || u != "http://127.0.0.1:7890" {
		t.Errorf("unset provider under global proxy:true: got (%s, %q), want (%s, %q)", mode, u, ProxyURL, "http://127.0.0.1:7890")
	}
	if mode, _ := cfg.ProxySpecFor(p2, "openai"); mode != ProxyDirect {
		t.Errorf("proxy:false must still override global proxy:true: got %s, want %s", mode, ProxyDirect)
	}
}

// TestGlobalProxyTrueRequiresURL mirrors the per-provider contradiction
// check: global proxy: true with no http_proxy/https_proxy at all is a
// config that states its own contradiction and must be rejected at load.
func TestGlobalProxyTrueRequiresURL(t *testing.T) {
	base := `
listen: 127.0.0.1:0
providers:
  - {name: p1, base_url: {openai: https://a.example}, api_key: k}
` + proxyTestModels

	if _, err := Parse([]byte("proxy: true\n" + base)); err == nil ||
		!strings.Contains(err.Error(), "proxy: true") {
		t.Errorf("global proxy: true without any proxy URL accepted (err=%v)", err)
	}
	if _, err := Parse([]byte("proxy: true\nhttps_proxy: http://127.0.0.1:7890\n" + base)); err != nil {
		t.Errorf("global proxy: true with https_proxy rejected: %v", err)
	}
}
