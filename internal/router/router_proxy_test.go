// Ver 2026-07-12 17:00, by Fable 5
package router

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"vmr/internal/config"
	"vmr/internal/core"
)

// TestProxyResolutionGrouping verifies Install builds one shared client per
// distinct proxy resolution: same resolution → same *http.Client (shared
// connection pool), proxy: false → a direct transport (nil Proxy), config
// proxy → a transport whose Proxy func yields the configured URL.
func TestProxyResolutionGrouping(t *testing.T) {
	cfg, err := config.Parse([]byte(`
listen: 127.0.0.1:0
https_proxy: http://127.0.0.1:7890
providers:
  openai:
    a: {base_url: https://a.example/v1, api_key: k}
    b: {base_url: https://b.example/v1, api_key: k, proxy: false}
    c: {base_url: https://c.example/v1, api_key: k, proxy: true}
models:
  openai:
    m:
      endpoints:
        - {provider: a, model: x}
        - {provider: b, model: x}
        - {provider: c, model: x}
`))
	if err != nil {
		t.Fatal(err)
	}
	rt := New(nil)
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)

	eps := snap.Models["openai"]["m"].Endpoints
	ca, cb, cc := snap.clientFor(eps[0]), snap.clientFor(eps[1]), snap.clientFor(eps[2])
	if ca == nil || cb == nil || cc == nil {
		t.Fatal("clientFor returned nil")
	}
	if ca != cc {
		t.Error("providers with the same proxy resolution must share one client")
	}
	if ca == cb {
		t.Error("direct provider must not share the proxied client")
	}
	if tr := cb.Transport.(*http.Transport); tr.Proxy != nil {
		t.Error("proxy: false must build a transport with nil Proxy")
	}
	tr := ca.Transport.(*http.Transport)
	if tr.Proxy == nil {
		t.Fatal("proxied provider got a direct transport")
	}
	u, err := tr.Proxy(httptest.NewRequest("POST", "https://a.example/v1/chat/completions", nil))
	if err != nil || u == nil || u.String() != "http://127.0.0.1:7890" {
		t.Errorf("proxy resolution: url=%v err=%v, want http://127.0.0.1:7890", u, err)
	}
}

// TestProxyEndToEnd proves traffic actually flows where the switches say:
// a provider following the global http_proxy reaches an unresolvable host
// through the proxy server (which sees the absolute-form request line), and
// a proxy: false provider hits its upstream directly with the proxy
// configured and running.
func TestProxyEndToEnd(t *testing.T) {
	var proxied, direct atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.RequestURI, "http://") {
			t.Errorf("proxy expected an absolute-form request line, got %q", r.RequestURI)
		}
		proxied.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"via-proxy","model":"real"}`))
	}))
	defer proxy.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		direct.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"direct","model":"real"}`))
	}))
	defer upstream.Close()

	// upstream.invalid never resolves — the request can only succeed if the
	// transport hands it to the proxy instead of dialing the host itself.
	cfg, err := config.Parse(fmt.Appendf(nil, `
listen: 127.0.0.1:0
http_proxy: %s
providers:
  openai:
    viaproxy: {base_url: http://upstream.invalid/v1, api_key: k}
    directp:  {base_url: %s/v1, api_key: k, proxy: false}
models:
  openai:
    m-proxy:  {endpoints: [{provider: viaproxy, model: real}]}
    m-direct: {endpoints: [{provider: directp, model: real}]}
`, proxy.URL, upstream.URL))
	if err != nil {
		t.Fatal(err)
	}
	rt := New(nil)
	snap, err := BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)

	serve := func(model string) *httptest.ResponseRecorder {
		body := []byte(`{"model":"` + model + `"}`)
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
		w := httptest.NewRecorder()
		rt.Serve(w, req, &core.CanonicalRequest{Model: model, Raw: body}, "openai", nil)
		return w
	}

	if w := serve("m-proxy"); w.Code != http.StatusOK {
		t.Fatalf("proxied request failed: %d %s", w.Code, w.Body)
	}
	if proxied.Load() != 1 {
		t.Error("request for the proxied provider did not pass through the proxy")
	}
	if w := serve("m-direct"); w.Code != http.StatusOK {
		t.Fatalf("direct request failed: %d %s", w.Code, w.Body)
	}
	if direct.Load() != 1 {
		t.Error("direct provider did not reach its upstream")
	}
	if proxied.Load() != 1 {
		t.Error("proxy: false provider leaked through the proxy")
	}
}
