// Ver 2026-09-09, by pi

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vmr/internal/config"
	"vmr/internal/livestats"
	"vmr/internal/router"
)

// TestStatsEndpointAuthAndContent verifies GET /stats authentication,
// in-flight snapshot reflection during execution, post-completion ledger
// update, and /stats.html page rendering.
func TestStatsEndpointAuthAndContent(t *testing.T) {
	// Upstream with a latch so we can inspect GET /stats while a request
	// is actively in-flight.
	releaseInflight := make(chan struct{})
	u := newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		<-releaseInflight
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"model-two","choices":[],"usage":{"prompt_tokens":40,"completion_tokens":20}}`))
	})

	yaml := `listen: 127.0.0.1:0
api_keys:
  - sk-vmr-team-alice
providers:
  - name: p1
    base_url: {openai-completions: ` + u.URL + `}
    api_keys:
      prod: sk-upstream-secret-key-123
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [model-two]}
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)

	statsDir := t.TempDir()
	lstats, err := livestats.New(statsDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lstats.Close() })

	srv := New(rt, nil).WithLiveStats(lstats)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// 1. /stats.html is accessible unauthenticated
	resp, err := http.Get(ts.URL + "/stats.html")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("GET /stats.html status=%d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. GET /stats without auth returns 401
	resp, _ = http.Get(ts.URL + "/stats")
	if resp.StatusCode != 401 {
		t.Fatalf("GET /stats without auth status=%d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// 3. Start a background chat request that pauses inside upstream
	var chatWG sync.WaitGroup
	chatWG.Add(1)
	go func() {
		defer chatWG.Done()
		_, _ = chat(t, ts, simpleReq, map[string]string{
			"Authorization": "Bearer sk-vmr-team-alice",
		})
	}()

	// Wait briefly for the request to enter in-flight
	var inflightSnapshot statsResponse
	for i := 0; i < 50; i++ {
		time.Sleep(10 * time.Millisecond)
		req, _ := http.NewRequest("GET", ts.URL+"/stats", nil)
		req.Header.Set("Authorization", "Bearer sk-vmr-team-alice")
		res, err := http.DefaultClient.Do(req)
		if err == nil && res.StatusCode == 200 {
			_ = json.NewDecoder(res.Body).Decode(&inflightSnapshot)
			res.Body.Close()
			if len(inflightSnapshot.Inflight) > 0 {
				break
			}
		}
	}

	if len(inflightSnapshot.Inflight) == 0 {
		close(releaseInflight)
		t.Fatal("expected 1 in-flight request while paused in upstream")
	}

	ent := inflightSnapshot.Inflight[0]
	if got, want := ent.VModel, "vm"; got != want {
		t.Errorf("inflight VModel = %q, want %q", got, want)
	}
	if got, want := ent.ClientKeyTag, "alice"; got != want {
		t.Errorf("inflight ClientKeyTag = %q, want %q", got, want)
	}
	if ent.Seq == 0 || ent.TS == "" {
		t.Errorf("inflight Seq=%d, TS=%q must be set", ent.Seq, ent.TS)
	}

	// 4. Release upstream so request finishes
	close(releaseInflight)
	chatWG.Wait()

	// 5. In-flight entry must now be gone, and completed ledger populated
	req, _ := http.NewRequest("GET", ts.URL+"/stats", nil)
	req.Header.Set("Authorization", "Bearer sk-vmr-team-alice")
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("GET /stats after completion: %v, status=%d", err, res.StatusCode)
	}
	var doneSnapshot statsResponse
	_ = json.NewDecoder(res.Body).Decode(&doneSnapshot)
	res.Body.Close()

	if len(doneSnapshot.Inflight) != 0 {
		t.Errorf("expected 0 in-flight requests after completion, got %d", len(doneSnapshot.Inflight))
	}

	if len(doneSnapshot.ByProviderModel) == 0 {
		t.Fatal("expected ByProviderModel to have 1 row")
	}
	pr := doneSnapshot.ByProviderModel[0]
	if pr.Provider != "p1-prod" || pr.Model != "model-two" {
		t.Errorf("provider row = %s:%s, want p1-prod:model-two", pr.Provider, pr.Model)
	}
	if pr.OK != 1 {
		t.Errorf("provider OK = %d, want 1", pr.OK)
	}

	// 6. Check by_key_label carries the upstream label "prod"
	if len(doneSnapshot.ByKeyLabel) == 0 {
		t.Fatal("expected ByKeyLabel to have 1 row")
	}
	if got, want := doneSnapshot.ByKeyLabel[0].Value, "prod"; got != want {
		t.Errorf("ByKeyLabel = %q, want %q", got, want)
	}

	// 7. Check by_client_key_tag carries "alice"
	if len(doneSnapshot.ByClientKeyTag) == 0 {
		t.Fatal("expected ByClientKeyTag to have 1 row")
	}
	if got, want := doneSnapshot.ByClientKeyTag[0].Value, "alice"; got != want {
		t.Errorf("ByClientKeyTag = %q, want %q", got, want)
	}
}

// TestStatsHTMLContainsNavMarkers confirms /stats.html links to sibling pages.
func TestStatsHTMLContainsNavMarkers(t *testing.T) {
	for _, marker := range []string{
		"VMR Live Stats",
		`href="/status.html"`,
		`href="/log.html"`,
		`href="/help.html"`,
		"In-flight Requests",
		"Performance by Provider & Model",
		"Usage by Upstream Key Label",
	} {
		if !strings.Contains(string(statsHTMLPage), marker) {
			t.Errorf("stats.html missing marker: %s", marker)
		}
	}
}
