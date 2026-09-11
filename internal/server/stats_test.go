// Ver 2026-09-09, by pi

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/config"
	"vmr/internal/livestats"
	"vmr/internal/router"
)

// TestStatsEndpointAuthAndContent verifies GET /stats authentication,
// in-flight snapshot reflection during execution, and post-completion ledger
// update.
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

	// 1. GET /stats without auth returns 401
	resp, _ := http.Get(ts.URL + "/stats")
	if resp.StatusCode != 401 {
		t.Fatalf("GET /stats without auth status=%d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Start a background chat request that pauses inside upstream
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
	// First wait (against the uncached Snapshot) for the done() hook to book
	// the sample — chatWG.Wait() can return a hair before the deferred hook
	// runs. Then read /stats once on a range tail step 3 did NOT warm: the
	// completed-ledger portion is read-cached per tail (CachedSnapshot,
	// snapCacheTTL), so a cold key folds fresh and must carry the sample,
	// with no dependence on the TTL window.
	for i := 0; i < 200; i++ {
		if len(lstats.Snapshot(livestats.HourlyTailDefault).ByProviderModel) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	var doneSnapshot statsResponse
	{
		req, _ := http.NewRequest("GET", ts.URL+"/stats?range=24h", nil)
		req.Header.Set("Authorization", "Bearer sk-vmr-team-alice")
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("GET /stats after completion: %v, status=%d", err, res.StatusCode)
		}
		_ = json.NewDecoder(res.Body).Decode(&doneSnapshot)
		res.Body.Close()
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
	snapshot := lstats.Snapshot(livestats.HourlyTailDefault)
	if len(snapshot.Hourly) != 0 || len(snapshot.Daily) != 0 || len(snapshot.ByProviderModel) != 0 {
		t.Errorf("pre-probe failures booked: hourly=%d daily=%d providers=%d",
			len(snapshot.Hourly), len(snapshot.Daily), len(snapshot.ByProviderModel))
	}

	// Contrast: a request that routes IS booked.
	if resp, _ := chat(t, ts, simpleReq, map[string]string{"Authorization": "Bearer sk-vmr-team-alice"}); resp.StatusCode != 200 {
		t.Fatalf("valid request status = %d, want 200", resp.StatusCode)
	}
	time.Sleep(50 * time.Millisecond)
	if got := lstats.Snapshot(livestats.HourlyTailDefault).Hourly; len(got) == 0 {
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
	if got := lstats.Snapshot(livestats.HourlyTailDefault).ByProviderModel; len(got) == 0 || got[0].OK != 1 {
		t.Errorf("audit-off request not booked: %+v", got)
	}

	// A request with no candidates (e.g. unknown model) without audit:
	// recorder is in status-only mode, but recent_errors must still carry
	// the client-facing status (404) and synthesized "no_candidate" class.
	if resp, _ := chat(t, ts, `{"model":"missing","messages":[{"role":"user","content":"hi"}]}`, nil); resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	time.Sleep(50 * time.Millisecond)
	snapNoAudit := lstats.Snapshot(livestats.HourlyTailDefault)
	if len(snapNoAudit.RecentErrors) == 0 {
		t.Fatalf("recent_errors empty in status-only mode")
	}
	lastErr := snapNoAudit.RecentErrors[0]
	if lastErr.Status != 404 || lastErr.ErrorClass != "no_candidate" || lastErr.Attempt != 0 {
		t.Errorf("recent_errors[0] = %+v, want status 404 / error_class no_candidate / attempt 0", lastErr)
	}
}

// TestStatsJSONContract guards the /stats JSON keys the Overview page reads by
// exact name: json.Marshal of the wire types must emit them, and the page must
// not carry the three access patterns that were silently wrong (ttft_p50 vs
// ttft_p50_ms in the WindowBlock tag, r.name vs r.value in DimensionRow, and
// taking the last (period x dims) row of hourly[]/daily[] as if it were the
// period total). The revoked tps rate keys (design §8) must not reappear.
func TestStatsJSONContract(t *testing.T) {
	var resp statsResponse
	resp.Concurrency.Limit, resp.Concurrency.InFlight, resp.Concurrency.Waiting = 8, 3, 1
	wb := &livestats.WindowBlock{N: 43, Tokens: livestats.TokenCounts{In: 1200, Out: 900}, TTFTP50: 412, TTFTP90: 680, ToksP50: 41.2, ToksP90: 55.7}
	resp.Inflight = []router.InflightEntry{{Seq: 1, State: "running", VModel: "coding"}}
	resp.Overall = wb
	resp.RecentErrors = []livestats.RecentErrorRow{{
		TS: time.Now(), VModel: "agent", Protocol: "anthropic-messages", Stream: true,
		ClientKeyTag: "openclaw", Provider: "p1-main", KeyLabel: "main",
		Model: "claude-opus-4.6", Attempt: 2, Outcome: "error",
		ErrorClass: "upstream_5xx", Status: 502, DurMS: 4100,
	}}
	resp.ByProviderModel = []livestats.ProviderRow{{
		Provider: "p1", KeyLabel: "main", Model: "m1", OK: 5,
		Tokens: livestats.TokenCounts{In: 100, Out: 60}, Last10: wb, Last100: wb,
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
		`"toks_p50":`, `"toks_p90":`, `"n":43`, `"tokens":`,
		`"key_label":`, `"overall":`, `"recent_errors":`,
		`"attempt":2`, `"error_class":"upstream_5xx"`, `"status":502`,
		`"value":`, `"counters":`, `"last_100":`,
	} {
		if !strings.Contains(js, key) {
			t.Errorf("statsResponse JSON missing a key the dashboard reads: %s", key)
		}
	}
	if strings.Contains(js, `"tps_`) {
		t.Error("statsResponse JSON still carries a revoked tps rate key (design §8: only toks)")
	}
}

// TestParseRangeTail pins the ?range= vocabulary (contracts §1.5): 24h/3d/7d
// select the 24/72/168-hour tails; anything else — missing, malformed,
// out-of-vocabulary — falls back to the 48h default. The 7d cap is the
// deliberate ceiling.
func TestParseRangeTail(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", livestats.HourlyTailDefault},
		{"24h", 24},
		{"3d", 72},
		{"7d", 168},
		{"48h", livestats.HourlyTailDefault},
		{"1d", livestats.HourlyTailDefault},
		{"30d", livestats.HourlyTailDefault},
		{"24H", livestats.HourlyTailDefault},
		{"bogus", livestats.HourlyTailDefault},
	}
	for _, c := range cases {
		if got := parseRangeTail(c.in); got != c.want {
			t.Errorf("parseRangeTail(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestSampleFromRecordTerminalAttempt pins the recent_errors inputs
// (contracts §1.6): error_class/status quote the terminal attempt — the
// winning attempt when the request forwarded, else the last attempt —
// verbatim (no re-classification), and Attempt is the 1-based try count.
func TestSampleFromRecordTerminalAttempt(t *testing.T) {
	forwarded := func(p, kl, m string) audit.Attempt {
		return audit.Attempt{
			Provider: p, KeyLabel: kl, Model: m, Forwarded: true,
			Response: &audit.Message{Status: 200},
			Tokens:   &audit.TokenCount{In: 10, Out: 5},
		}
	}
	failed := func(class string, status int) audit.Attempt {
		a := audit.Attempt{ErrorClass: class}
		if status != 0 {
			a.Response = &audit.Message{Status: status}
		}
		return a
	}

	cases := []struct {
		name       string
		attempts   []audit.Attempt
		wantClass  string
		wantStatus int
		wantTry    int
	}{
		{"no attempts", nil, "no_candidate", 0, 0},
		{"all failed: last attempt wins the stamp",
			[]audit.Attempt{failed("auth", 401), failed("upstream_5xx", 502)},
			"upstream_5xx", 502, 2},
		{"winning attempt takes precedence over later failures",
			[]audit.Attempt{forwarded("p1", "main", "m1"), failed("upstream_5xx", 502)},
			"", 200, 2},
		{"single forwarded attempt",
			[]audit.Attempt{forwarded("p1", "main", "m1")},
			"", 200, 1},
		{"failed attempt without a response keeps status 0",
			[]audit.Attempt{failed("network", 0)},
			"network", 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := sampleFromRecord(&audit.Record{Outcome: "error", Attempts: c.attempts})
			if s.ErrorClass != c.wantClass {
				t.Errorf("error_class = %q, want %q (verbatim, never re-classified)", s.ErrorClass, c.wantClass)
			}
			if s.Status != c.wantStatus {
				t.Errorf("status = %d, want %d", s.Status, c.wantStatus)
			}
			if s.Attempt != c.wantTry {
				t.Errorf("attempt = %d, want %d", s.Attempt, c.wantTry)
			}
		})
	}

	// Winning-attempt service identity still rides the forwarded attempt.
	s := sampleFromRecord(&audit.Record{Outcome: "ok", Attempts: []audit.Attempt{forwarded("p1", "main", "m1")}})
	if s.Provider != "p1" || s.KeyLabel != "main" || s.Model != "m1" {
		t.Errorf("service identity = %s/%s/%s, want p1/main/m1", s.Provider, s.KeyLabel, s.Model)
	}
	if s.Tokens.In != 10 || s.Tokens.Out != 5 {
		t.Errorf("tokens = %+v, want in=10 out=5", s.Tokens)
	}

	// A never-forwarded failure keeps the service face empty (§4.2).
	s = sampleFromRecord(&audit.Record{Outcome: "error", Attempts: []audit.Attempt{failed("auth", 401)}})
	if s.Provider != "" || s.KeyLabel != "" || s.Model != "" {
		t.Errorf("unforwarded failure must carry no service identity: %+v", s)
	}

	// No attempt at all (every candidate cooling down → vmr_no_candidates):
	// Status falls back to the client-facing code, the only terminal fact,
	// and ErrorClass synthesizes "no_candidate".
	s = sampleFromRecord(&audit.Record{
		Outcome: "error",
		Client:  audit.Exchange{Response: &audit.Message{Status: 503}},
	})
	if s.Attempt != 0 || s.ErrorClass != "no_candidate" {
		t.Errorf("no-attempt failure: want attempt 0 / error_class \"no_candidate\", got %+v", s)
	}
	if s.Status != 503 {
		t.Errorf("no-attempt failure Status = %d, want 503 (client-facing fallback)", s.Status)
	}

	// Client-canceled with no attempt: ErrorClass stays empty so the
	// frontend falls back to outcome ("canceled").
	s = sampleFromRecord(&audit.Record{
		Outcome: "canceled",
		Client:  audit.Exchange{Response: &audit.Message{Status: 499}},
	})
	if s.Attempt != 0 || s.ErrorClass != "" {
		t.Errorf("no-attempt canceled: want attempt 0 / empty error_class, got %+v", s)
	}

	if got := sampleFromRecord(nil); got != (livestats.Sample{}) {
		t.Errorf("nil record must map to the zero sample, got %+v", got)
	}
}

// TestStatsRangeHourlyWindow drives /stats?range= end to end: 30 distinct
// observed hours land in every tail that covers them, the 24h tail keeps
// only the newest 24, invalid values fall back to the default, daily[] is
// untouched by the range, and concurrent polls of different ranges share
// the per-tail cache cleanly (run under -race).
func TestStatsRangeHourlyWindow(t *testing.T) {
	cfg, err := config.Parse([]byte(`listen: 127.0.0.1:0
providers:
  - name: p1
    base_url: {openai-completions: http://127.0.0.1:9}
    api_key: sk-up
models:
  vm:
    endpoints:
      openai-completions:
        - {providers: [p1], models: [m1]}
`))
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

	// 30 distinct observed hours ending at the current hour.
	now := time.Now()
	for i := 0; i < 30; i++ {
		lstats.Record(livestats.Sample{
			TS:     now.Add(-time.Duration(i) * time.Hour),
			VModel: "coding", Outcome: livestats.OutcomeOK,
		})
	}

	ts := httptest.NewServer(New(rt, nil).WithLiveStats(lstats).Handler())
	t.Cleanup(ts.Close)

	get := func(rq string) (*statsResponse, string) {
		res, err := http.Get(ts.URL + "/stats" + rq)
		if err != nil {
			t.Fatalf("GET %s: %v", rq, err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("GET %s status = %d", rq, res.StatusCode)
		}
		b, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatalf("read %s: %v", rq, err)
		}
		body := string(b)
		var out statsResponse
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("decode %s: %v", rq, err)
		}
		return &out, body
	}

	if _, body := get("?range=24h"); lenHourly(t, body) != 24 {
		t.Errorf("24h tail kept %d hourly rows, want 24", lenHourly(t, body))
	}
	if _, body := get("?range=3d"); lenHourly(t, body) != 30 {
		t.Errorf("3d tail kept %d hourly rows, want 30", lenHourly(t, body))
	}
	if _, body := get("?range=7d"); lenHourly(t, body) != 30 {
		t.Errorf("7d tail kept %d hourly rows, want 30", lenHourly(t, body))
	}
	def, defBody := get("")
	if len(def.Hourly) != 30 {
		t.Errorf("default tail kept %d hourly rows, want 30", len(def.Hourly))
	}
	if strings.Contains(defBody, `"overall":`) {
		t.Error("overall must be omitted when no ring samples exist")
	}
	if !strings.Contains(defBody, `"recent_errors":[]`) {
		t.Errorf("recent_errors must be present (empty array) in every payload: %s", defBody)
	}

	// daily[] is fixed regardless of range: same rows under every tail.
	days := len(def.Daily)
	if days == 0 {
		t.Fatal("expected daily rows")
	}
	for _, rq := range []string{"?range=24h", "?range=3d", "?range=7d", "?range=bogus"} {
		r, _ := get(rq)
		if len(r.Daily) != days {
			t.Errorf("%s: daily rows = %d, want %d (daily is range-invariant)", rq, len(r.Daily), days)
		}
	}

	// Concurrent mixed-range polls: the per-tail cache must hold up under
	// -race with no cross-range contamination.
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			rqs := []string{"", "?range=24h", "?range=3d", "?range=7d", "?range=bogus"}
			for i := 0; i < 20; i++ {
				r, _ := get(rqs[(w+i)%len(rqs)])
				want := 30
				if rqs[(w+i)%len(rqs)] == "?range=24h" {
					want = 24
				}
				if len(r.Hourly) != want {
					t.Errorf("concurrent %s: hourly rows = %d, want %d", rqs[(w+i)%len(rqs)], len(r.Hourly), want)
					return
				}
			}
		}(w)
	}
	wg.Wait()
}

// lenHourly counts hourly rows in the decoded payload. daily[] rows share
// the same row shape, so the count is taken from the struct, not by
// pattern-matching the wire.
func lenHourly(t *testing.T, body string) int {
	t.Helper()
	var out statsResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return len(out.Hourly)
}
