// Ver 2026-08-23 13:05, by Gemini 3.7 Flash

// /status's "instance" block: the facts that let a caller who only
// has a port tell which vmr answered it (vmr.sh ps is built entirely on
// this — see cmd_status.go's -addr/-brief).
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

const instanceYAML = `
listen: 127.0.0.1:18800
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
`

type configSubBlock struct {
	Path   string         `json:"path"`
	Mtime  time.Time      `json:"mtime"`
	Stale  bool           `json:"stale"`
	Issues []config.Issue `json:"issues"`
}

type instanceBlock struct {
	PID        int             `json:"pid"`
	Listen     string          `json:"listen"`
	Config     *configSubBlock `json:"config"`
	Models     int             `json:"models"`
	Uptime     int64           `json:"uptime_seconds"`
	UptimeStr  string          `json:"uptime"`
	Started    string          `json:"started_at"`
	GoVersion  string          `json:"go_version"`
	OSArch     string          `json:"os_arch"`
	Cwd        string          `json:"cwd"`
	Executable string          `json:"executable"`
}

func fetchInstance(t *testing.T, withInstance bool, cfgPath string, started time.Time) instanceBlock {
	t.Helper()
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	s := New(rt, nil)
	if withInstance {
		s = s.WithInstance(cfgPath, started)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Instance instanceBlock `json:"instance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Instance
}

// The listen address reported is the *configured* one, not the ephemeral
// port httptest bound — that's the point: vmr.sh ps finds the port via
// lsof and asks the instance what it thinks it is serving.
func TestStatusInstanceBlock(t *testing.T) {
	started := time.Now().Add(-90 * time.Second)
	inst := fetchInstance(t, true, "testdata/rel.yaml", started)

	if inst.PID <= 0 {
		t.Errorf("pid = %d, want the current process id", inst.PID)
	}
	if inst.Listen != "127.0.0.1:18800" {
		t.Errorf("listen = %q, want the configured address", inst.Listen)
	}
	if inst.Models != 1 {
		t.Errorf("models = %d, want 1", inst.Models)
	}
	// Absolute: the process may have been started from any directory, and
	// the relative path it was handed means nothing to a reader elsewhere.
	if inst.Config == nil || !filepath.IsAbs(inst.Config.Path) {
		t.Errorf("config.path = %+v, want an absolute path", inst.Config)
	}
	if inst.Config == nil || filepath.Base(inst.Config.Path) != "rel.yaml" {
		t.Errorf("config.path = %+v, want it to still name rel.yaml", inst.Config)
	}
	// Whole seconds, derived server-side so a clock-skewed reader can't
	// compute a negative uptime from started_at.
	if inst.Uptime < 89 || inst.Uptime > 95 {
		t.Errorf("uptime_seconds = %d, want ~90", inst.Uptime)
	}
	if inst.UptimeStr == "" {
		t.Error("uptime missing")
	}
	if inst.Started == "" {
		t.Error("started_at missing")
	}
	if inst.Cwd == "" || inst.Executable == "" || inst.GoVersion == "" || inst.OSArch == "" {
		t.Errorf("expected runtime and path fields: %+v", inst)
	}
}

// Every test in this package constructs a Server without WithInstance, and
// so does anything embedding it — the block must degrade to "what the
// process can answer about itself" rather than emitting zero values that
// read as real data (pid 0, uptime 0, config "").
func TestStatusInstanceBlockWithoutWithInstance(t *testing.T) {
	inst := fetchInstance(t, false, "", time.Time{})

	if inst.PID <= 0 || inst.Listen == "" || inst.Models != 1 {
		t.Errorf("process-derived fields must still be present: %+v", inst)
	}
	if inst.Config != nil && inst.Config.Path != "" {
		t.Errorf("config.path = %q, want it omitted when unknown", inst.Config.Path)
	}
	if inst.Started != "" || inst.Uptime != 0 {
		t.Errorf("started_at/uptime must be omitted when unknown: %+v", inst)
	}
}

// TestStatusInstanceBaseURLs pins what base_urls means: an echo of the
// request that asked for this status, not of listen — whatever host the
// caller used (and whether over TLS) is exactly what it should point its
// client at.
func TestStatusInstanceBaseURLs(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	s := New(rt, nil)

	fetchBaseURLs := func(body []byte) map[string]string {
		t.Helper()
		var out struct {
			Instance struct {
				BaseURLs map[string]string `json:"base_urls"`
			} `json:"instance"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		return out.Instance.BaseURLs
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	want := "http://" + ts.Listener.Addr().String() + "/v1/"
	baseURLs := fetchBaseURLs(body)
	for _, proto := range []string{"openai", "anthropic", "openai-responses"} {
		if got := baseURLs[proto]; got != want {
			t.Errorf("base_urls[%s] = %q, want %q", proto, got, want)
		}
	}

	// The Host header is echoed verbatim: localhost stays localhost.
	req := httptest.NewRequest("GET", "/status", nil)
	req.Host = "localhost:8800"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if got := fetchBaseURLs(w.Body.Bytes())["openai"]; got != "http://localhost:8800/v1/" {
		t.Errorf("base_urls[openai] = %q, want http://localhost:8800/v1/", got)
	}

	// Over TLS the advertised scheme is https.
	tlsSrv := httptest.NewTLSServer(s.Handler())
	defer tlsSrv.Close()
	tlsResp, err := tlsSrv.Client().Get(tlsSrv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	tlsBody, _ := io.ReadAll(tlsResp.Body)
	tlsResp.Body.Close()
	wantTLS := "https://" + tlsSrv.Listener.Addr().String() + "/v1/"
	if got := fetchBaseURLs(tlsBody)["openai"]; got != wantTLS {
		t.Errorf("base_urls[openai] over TLS = %q, want %q", got, wantTLS)
	}
}

// TestStatus_AuthMatrix verifies that /status allows any source IP when no
// api_keys are configured, enforces authentication when api_keys are set,
// and rejects the old /admin/status path with 404.
func TestStatus_AuthMatrix(t *testing.T) {
	// 1. Unauthenticated server (no api_keys): allow any source IP
	cfgOpen, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	rtOpen := router.New(nil)
	snapOpen, err := router.BuildSnapshot(cfgOpen)
	if err != nil {
		t.Fatal(err)
	}
	rtOpen.Install(snapOpen)
	hOpen := New(rtOpen, nil).Handler()

	for _, remote := range []string{"127.0.0.1:1234", "203.0.113.7:1234", "10.0.0.5:5678"} {
		req := httptest.NewRequest("GET", "/status", nil)
		req.RemoteAddr = remote
		w := httptest.NewRecorder()
		hOpen.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("unauth server, remote %s: status = %d, want 200", remote, w.Code)
		}
	}

	// Old /admin/status must return 404
	reqOld := httptest.NewRequest("GET", "/admin/status", nil)
	wOld := httptest.NewRecorder()
	hOpen.ServeHTTP(wOld, reqOld)
	if wOld.Code != http.StatusNotFound {
		t.Errorf("old /admin/status: status = %d, want 404", wOld.Code)
	}

	// 2. Authenticated server (api_keys configured)
	const authedYAML = `
listen: 0.0.0.0:18800
api_keys:
  - secret-token-12345678
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
`
	cfgAuthed, err := config.Parse([]byte(authedYAML))
	if err != nil {
		t.Fatal(err)
	}
	rtAuthed := router.New(nil)
	snapAuthed, err := router.BuildSnapshot(cfgAuthed)
	if err != nil {
		t.Fatal(err)
	}
	rtAuthed.Install(snapAuthed)
	hAuthed := New(rtAuthed, nil).Handler()

	// Missing key -> 401
	{
		req := httptest.NewRequest("GET", "/status", nil)
		req.RemoteAddr = "10.0.0.5:5678"
		w := httptest.NewRecorder()
		hAuthed.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("authed server, missing key: status = %d, want 401", w.Code)
		}
	}

	// Wrong key -> 401
	{
		req := httptest.NewRequest("GET", "/status", nil)
		req.Header.Set("Authorization", "Bearer invalid-key-12345678")
		w := httptest.NewRecorder()
		hAuthed.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("authed server, wrong key: status = %d, want 401", w.Code)
		}
	}

	// Correct key via Bearer Header -> 200
	{
		req := httptest.NewRequest("GET", "/status", nil)
		req.RemoteAddr = "192.168.1.100:5678"
		req.Header.Set("Authorization", "Bearer secret-token-12345678")
		w := httptest.NewRecorder()
		hAuthed.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("authed server, valid Bearer key: status = %d, want 200", w.Code)
		}
	}

	// Correct key via x-api-key Header -> 200
	{
		req := httptest.NewRequest("GET", "/status", nil)
		req.RemoteAddr = "192.168.1.100:5678"
		req.Header.Set("x-api-key", "secret-token-12345678")
		w := httptest.NewRecorder()
		hAuthed.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("authed server, valid x-api-key: status = %d, want 200", w.Code)
		}
	}
}

// TestStatusIssuesBlock verifies that /status includes the "issues" array
// when config.Check() flags operational issues, and omits it when the config is clean.
func TestStatusIssuesBlock(t *testing.T) {
	// Config with non-loopback listen and no API keys -> flags a SeverityWarning listen issue.
	exposedYAML := `
listen: 0.0.0.0:18800
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
`
	cfg, err := config.Parse([]byte(exposedYAML))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	s := New(rt, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Instance struct {
			Config struct {
				Issues []config.Issue `json:"issues"`
			} `json:"config"`
		} `json:"instance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	issues := out.Instance.Config.Issues
	if len(issues) != 1 || issues[0].Field != "listen" || issues[0].Severity != config.SeverityWarning {
		t.Errorf("got issues %+v, want 1 listen warning", issues)
	}
}

func TestStatusNoIssuesBlock(t *testing.T) {
	// Clean config -> issues field must be absent/omitted.
	cleanYAML := `
listen: 127.0.0.1:18800
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k1}
models:
  vm:
    endpoints:
      - {protocol: openai, providers: [p1], models: [m1]}
`
	cfg, err := config.Parse([]byte(cleanYAML))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	s := New(rt, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Instance struct {
			Config *struct {
				Issues []config.Issue `json:"issues"`
			} `json:"config"`
		} `json:"instance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Instance.Config != nil && len(out.Instance.Config.Issues) > 0 {
		t.Errorf("expected no issues block, got: %+v", out.Instance.Config.Issues)
	}
}

func TestStatus_SystemTrafficBlocks(t *testing.T) {
	cfg, err := config.Parse([]byte(instanceYAML))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	s := New(rt, nil).WithInstance("/tmp/config.yaml", time.Now().Add(-10*time.Second))
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out struct {
		System struct {
			Goroutines int `json:"goroutines"`
			Memory     struct {
				HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
				HeapAlloc      string `json:"heap_alloc"`
				SysBytes       uint64 `json:"sys_bytes"`
				Sys            string `json:"sys"`
			} `json:"memory"`
			Disk struct {
				FreeSpaceBytes uint64 `json:"free_space_bytes"`
				FreeSpace      string `json:"free_space"`
			} `json:"disk"`
		} `json:"system"`
		Traffic struct {
			Requests struct {
				Total      uint64            `json:"total"`
				ByProtocol map[string]uint64 `json:"by_protocol"`
				ByStatus   map[string]uint64 `json:"by_status"`
			} `json:"requests"`
			Tokens struct {
				Total struct {
					In         uint64 `json:"in"`
					CacheWrite uint64 `json:"cache_write"`
					CacheRead  uint64 `json:"cache_read"`
					Reasoning  uint64 `json:"reasoning"`
					Out        uint64 `json:"out"`
				} `json:"total"`
			} `json:"tokens"`
			Sticky struct {
				Entries int `json:"entries"`
			} `json:"sticky"`
		} `json:"traffic"`
		CurrentTime string `json:"current_time"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	if out.System.Goroutines <= 0 {
		t.Errorf("expected positive goroutine count, got %d", out.System.Goroutines)
	}
	if out.System.Memory.SysBytes <= 0 || out.System.Memory.Sys == "" {
		t.Errorf("expected memory sys bytes/string, got %+v", out.System.Memory)
	}
	if out.Traffic.Requests.ByProtocol == nil || out.Traffic.Requests.ByStatus == nil {
		t.Errorf("expected initialized protocol/status maps in traffic: %+v", out.Traffic)
	}
	if out.CurrentTime == "" {
		t.Error("expected non-empty current_time")
	}
}

// TestStatus_ImageCacheEnabled_Semantics pins what "enabled" means in the
// image_cache block: the global image_downscale, OR any virtual model with a
// positive explicit override (config models[].image_downscale), which wins
// over the global per EffectiveImageDownscaleMaxPx. A global-off + one-model-on
// config must report enabled=true, since that model's images really are
// downscaled and cached.
func TestStatus_ImageCacheEnabled_Semantics(t *testing.T) {
	const perModelYAML = `
listen: 127.0.0.1:18800
providers:
  - {name: p1, base_url: {openai: http://127.0.0.1:1}, api_key: k}
models:
  vm:
    image_downscale: 256
    endpoints:
      - {protocol: openai, providers: [p1], models: [m]}
`
	cfg, err := config.Parse([]byte(perModelYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ImageDownscaleMaxPx != 0 {
		t.Fatalf("setup: global image_downscale must be 0/absent, got %d", cfg.ImageDownscaleMaxPx)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	s := New(rt, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		ImageCache struct {
			Enabled bool `json:"enabled"`
		} `json:"image_cache"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.ImageCache.Enabled {
		t.Error("image_cache.enabled = false, want true (global off but model vm sets image_downscale: 256)")
	}
}
