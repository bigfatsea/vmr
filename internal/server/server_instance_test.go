// Ver 2026-07-28 12:40, by Opus 5

// /admin/status's "instance" block: the facts that let a caller who only
// has a port tell which vmr answered it (vmr.sh ps is built entirely on
// this — see cmd_status.go's -addr/-brief).
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

const instanceYAML = `
listen: 127.0.0.1:18800
providers:
  openai:
    p1: {base_url: http://127.0.0.1:1, api_key: k1}
models:
  openai:
    vm:
      endpoints:
        - {provider: p1, model: m1}
`

type instanceBlock struct {
	PID     int    `json:"pid"`
	Listen  string `json:"listen"`
	Config  string `json:"config"`
	Models  int    `json:"models"`
	Uptime  int64  `json:"uptime_seconds"`
	Started string `json:"started_at"`
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

	resp, err := http.Get(ts.URL + "/admin/status")
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
func TestAdminStatusInstanceBlock(t *testing.T) {
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
	if !filepath.IsAbs(inst.Config) {
		t.Errorf("config = %q, want an absolute path", inst.Config)
	}
	if filepath.Base(inst.Config) != "rel.yaml" {
		t.Errorf("config = %q, want it to still name rel.yaml", inst.Config)
	}
	// Whole seconds, derived server-side so a clock-skewed reader can't
	// compute a negative uptime from started_at.
	if inst.Uptime < 89 || inst.Uptime > 95 {
		t.Errorf("uptime_seconds = %d, want ~90", inst.Uptime)
	}
	if inst.Started == "" {
		t.Error("started_at missing")
	}
}

// Every test in this package constructs a Server without WithInstance, and
// so does anything embedding it — the block must degrade to "what the
// process can answer about itself" rather than emitting zero values that
// read as real data (pid 0, uptime 0, config "").
func TestAdminStatusInstanceBlockWithoutWithInstance(t *testing.T) {
	inst := fetchInstance(t, false, "", time.Time{})

	if inst.PID <= 0 || inst.Listen == "" || inst.Models != 1 {
		t.Errorf("process-derived fields must still be present: %+v", inst)
	}
	if inst.Config != "" {
		t.Errorf("config = %q, want it omitted when unknown", inst.Config)
	}
	if inst.Started != "" || inst.Uptime != 0 {
		t.Errorf("started_at/uptime must be omitted when unknown: %+v", inst)
	}
}

// The admin surface stays loopback-only after this change — the instance
// block adds a config path to a response that must never leave the host.
func TestAdminStatusStillLoopbackOnly(t *testing.T) {
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
	h := New(rt, nil).WithInstance("config.yaml", time.Now()).Handler()

	for _, remote := range []string{"203.0.113.7:1234", "10.0.0.5:5678"} {
		req := httptest.NewRequest("GET", "/admin/status", nil)
		req.RemoteAddr = remote
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("remote %s: status = %d, want 403 (body: %s)", remote, w.Code, w.Body.String())
		}
		if got := w.Body.String(); strings.Contains(got, "config") {
			t.Errorf("remote %s: response leaked config info: %s", remote, got)
		}
	}
}
