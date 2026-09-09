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

	// 5. In-flight entry must now be gone, and completed ledger populated.
	// /stats snapshots are read-cached ~1s (CachedSnapshot), so the just-
	// completed request can take up to that long to surface — poll for it.
	var doneSnapshot statsResponse
	for i := 0; i < 40; i++ {
		req, _ := http.NewRequest("GET", ts.URL+"/stats", nil)
		req.Header.Set("Authorization", "Bearer sk-vmr-team-alice")
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("GET /stats after completion: %v, status=%d", err, res.StatusCode)
		}
		doneSnapshot = statsResponse{}
		_ = json.NewDecoder(res.Body).Decode(&doneSnapshot)
		res.Body.Close()
		if len(doneSnapshot.ByProviderModel) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

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

// TestStatsPreProbeFailuresNotBooked: a request that never reached a virtual
// model (401, invalid JSON, missing model) must not enter the live-stats
// ledger — booking it sprays empty-dims rows into slim/rollup. Same shape as
// "probe traffic isn't audited, so it isn't counted" (design §4.2).
func TestStatsPreProbeFailuresNotBooked(t *testing.T) {
	u := newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	})
	yaml := `listen: 127.0.0.1:0
api_keys:
  - sk-vmr-team-alice
providers:
  - name: p1
    base_url: {openai-completions: ` + u.URL + `}
    api_key: sk-up
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
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

	lstats, err := livestats.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lstats.Close() })
	ts := httptest.NewServer(New(rt, nil).WithLiveStats(lstats).Handler())
	t.Cleanup(ts.Close)

	// Wrong key -> 401 (before probe); invalid JSON -> 400 (probe fails);
	// missing model -> 400 (probe yields no vmodel).
	if resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer wrong"}); resp.StatusCode != 401 {
		t.Fatalf("wrong key status = %d, want 401", resp.StatusCode)
	}
	if resp, _ := chat(t, ts, `{not json`, map[string]string{"Authorization": "Bearer sk-vmr-team-alice"}); resp.StatusCode != 400 {
		t.Fatalf("invalid JSON status = %d, want 400", resp.StatusCode)
	}
	if resp, _ := chat(t, ts, `{"messages":[]}`, map[string]string{"Authorization": "Bearer sk-vmr-team-alice"}); resp.StatusCode != 400 {
		t.Fatalf("missing model status = %d, want 400", resp.StatusCode)
	}

	// Give the done() hooks time to run, then confirm nothing was booked.
	time.Sleep(50 * time.Millisecond)
	snapshot := lstats.Snapshot()
	if len(snapshot.Hourly) != 0 || len(snapshot.Daily) != 0 || len(snapshot.ByProviderModel) != 0 {
		t.Errorf("pre-probe failures booked: hourly=%d daily=%d providers=%d",
			len(snapshot.Hourly), len(snapshot.Daily), len(snapshot.ByProviderModel))
	}

	// Contrast: a request that routes IS booked.
	if resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer sk-vmr-team-alice"}); resp.StatusCode != 200 {
		t.Fatalf("valid request status = %d, want 200", resp.StatusCode)
	}
	time.Sleep(50 * time.Millisecond)
	if got := lstats.Snapshot().Hourly; len(got) == 0 {
		t.Error("valid routed request was not booked into the ledger")
	}
}

// TestStatsWorksWithoutAudit: with the server wired for live stats but no
// audit logger, /stats still reflects completed requests and the recorder
// runs in status-only mode (no response-body buffering).
func TestStatsWorksWithoutAudit(t *testing.T) {
	u := newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":8}}`))
	})
	yaml := `listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: ` + u.URL + `}
    api_key: sk-up
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
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

	lstats, err := livestats.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lstats.Close() })
	// New(rt, nil): audit logger is nil.
	ts := httptest.NewServer(New(rt, nil).WithLiveStats(lstats).Handler())
	t.Cleanup(ts.Close)

	if resp, _ := chat(t, ts, simpleReq, nil); resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	time.Sleep(50 * time.Millisecond)
	if got := lstats.Snapshot().ByProviderModel; len(got) == 0 || got[0].OK != 1 {
		t.Errorf("audit-off request not booked: %+v", got)
	}
}

// TestStatsHTMLJSONContract guards the /stats JSON <-> stats.html JS wire
// contract. The dashboard reads these keys by exact name; json.Marshal of the
// wire types must emit them, and the embedded JS must not carry the three
// access patterns that were silently wrong (ttft_p50 vs ttft_p50_ms in the
// Quantiles tag, r.name vs r.value in DimensionRow, and taking the last
// (period x dims) row of hourly[]/daily[] as if it were the period total).
func TestStatsHTMLJSONContract(t *testing.T) {
	var resp statsResponse
	resp.Concurrency.Limit, resp.Concurrency.InFlight, resp.Concurrency.Waiting = 8, 3, 1
	q := &livestats.Quantiles{TTFTP50: 100, TTFTP90: 200, TPSP50: 50, TPSP90: 90}
	resp.Inflight = []router.InflightEntry{{Seq: 1, State: "running", VModel: "coding"}}
	resp.ByProviderModel = []livestats.ProviderRow{{
		Provider: "p1", Model: "m1", Stream: true, OK: 5,
		Tokens: livestats.TokenCounts{In: 100, Out: 60}, Last10: q, Last100: q,
	}}
	resp.ByKeyLabel = []livestats.DimensionRow{{Value: "main", OK: 5, Count: 5}}
	resp.ByClientKeyTag = resp.ByKeyLabel
	resp.Hourly = []livestats.HourlyRow{{Counters: livestats.Counters{OK: 5}}}
	resp.Daily = resp.Hourly

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, key := range []string{
		`"in_flight":`, `"waiting":`, `"ttft_p50_ms":`, `"ttft_p90_ms":`,
		`"tps_p50":`, `"tps_p90":`, `"value":`, `"counters":`, `"last_100":`,
	} {
		if !strings.Contains(js, key) {
			t.Errorf("statsResponse JSON missing a key the dashboard reads: %s", key)
		}
	}

	html := string(statsHTMLPage)
	for _, want := range []string{"ttft_p50_ms", "r.value", "sumLatestPeriod"} {
		if !strings.Contains(html, want) {
			t.Errorf("stats.html JS missing corrected access pattern: %q", want)
		}
	}
	for _, bad := range []string{"r.name", "data.daily[data.daily.length", "data.hourly[data.hourly.length"} {
		if strings.Contains(html, bad) {
			t.Errorf("stats.html JS still carries a known-broken pattern: %q", bad)
		}
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
