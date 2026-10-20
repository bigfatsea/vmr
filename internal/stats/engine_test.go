package stats

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"testing"
	"time"
)

// memStore is a store with the given events recorded "today" (the clock
// sits just after the last event).
func memStore(t testing.TB, now time.Time, evs ...Event) *Store {
	t.Helper()
	s, err := openAt(t.TempDir(), func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, e := range evs {
		s.Record(e)
	}
	return s
}

func mustQuery(t *testing.T, s *Store, now time.Time, qs string) Result {
	t.Helper()
	v, err := url.ParseQuery(qs)
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseQuery(v, now)
	if err != nil {
		t.Fatalf("ParseQuery(%s): %v", qs, err)
	}
	return s.Query(q)
}

// col returns the named column of the totals row.
func total(t *testing.T, r Result, name string) any {
	t.Helper()
	for i, c := range r.Columns {
		if c.Name == name {
			return r.Totals[i]
		}
	}
	t.Fatalf("no column %q in %v", name, r.Columns)
	return nil
}

// TestEngine_SampleFaces pins which events feed which metrics: requests
// count everything, tokens count forwarded events whatever their outcome,
// speed counts only ok+forwarded, and TTFT/throughput only streamed ones.
func TestEngine_SampleFaces(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	ts := now.Add(-time.Hour)
	okStream := ev(ts, OutcomeOK) // dur 1000, ttft 200, out 100 → 125 tok/s
	okJSON := ev(ts, OutcomeOK)
	okJSON.Stream, okJSON.DurMS, okJSON.TTFTMS = false, 3000, 3000 // JSON: no TTFT concept
	okNoTTFT := ev(ts, OutcomeOK)
	okNoTTFT.TTFTMS, okNoTTFT.DurMS = 0, 2000 // streamed but TTFT unmeasured
	okBurst := ev(ts, OutcomeOK)
	okBurst.DurMS, okBurst.TTFTMS = 230, 200 // 30ms generation span: below the floor
	fwdCanceled := ev(ts, OutcomeCanceled)
	fwdCanceled.Forwarded, fwdCanceled.Tokens, fwdCanceled.DurMS = true, Tokens{In: 7, Out: 3}, 9000
	failed := ev(ts, OutcomeError)
	failed.Attempts = 2
	s := memStore(t, now, okStream, okJSON, okNoTTFT, okBurst, fwdCanceled, failed)

	r := mustQuery(t, s, now, "from=-2h&metrics=req,ok,err,canceled,err_rate,failover_rate,tok_fresh,tok_out,tok_prompt,cache_hit,dur_avg,dur_p50,ttft_avg,ttft_p50,toks_p50,toks_avg")
	want := map[string]any{
		"req": int64(6), "ok": int64(4), "err": int64(1), "canceled": int64(1),
		"err_rate": 0.1667, "failover_rate": 0.1667,
		"tok_fresh":  int64(4*10 + 7), // the canceled-after-forward request spent tokens too
		"tok_out":    int64(4*100 + 3),
		"tok_prompt": int64(4*65 + 7),
		"cache_hit":  0.7491,        // 200 / 267
		"dur_avg":    float64(1558), // (1000+3000+2000+230)/4: ok+forwarded only, no canceled 9000
		"dur_p50":    int64(1000),
		"ttft_avg":   float64(200), // okStream and okBurst only: JSON and unmeasured are out
		"ttft_p50":   int64(200),
		"toks_p50":   125.0, // okStream only: okBurst is below the span floor
		"toks_avg":   125.0,
	}
	for k, v := range want {
		if got := total(t, r, k); got != v {
			t.Errorf("%s = %v (%T), want %v (%T)", k, got, got, v, v)
		}
	}
}

func TestEngine_NearestRank(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	var evs []Event
	for i := 1; i <= 10; i++ {
		e := ev(now.Add(-time.Duration(i)*time.Minute), OutcomeOK)
		e.TTFTMS = int64(i * 100)
		e.DurMS = int64(i*100 + 1000) // generation span 1000ms → toks = 100/s for all
		evs = append(evs, e)
	}
	s := memStore(t, now, evs...)
	r := mustQuery(t, s, now, "from=-1h&metrics=ttft_p50,ttft_p90,ttft_p99")
	if got := []any{total(t, r, "ttft_p50"), total(t, r, "ttft_p90"), total(t, r, "ttft_p99")}; got[0] != int64(500) || got[1] != int64(900) || got[2] != int64(1000) {
		t.Errorf("p50/p90/p99 = %v, want 500/900/1000 (nearest rank)", got)
	}
	if v, ok := nearestRank([]float64{}, 0.5); ok || v != 0 {
		t.Error("empty pool must report no sample")
	}
}

func TestEngine_GroupTopNAndOther(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	var evs []Event
	for g := 0; g < 25; g++ {
		for i := 0; i <= g; i++ { // group g has g+1 requests
			e := ev(now.Add(-time.Minute), OutcomeOK)
			e.ClientKeyTag = fmt.Sprintf("c%02d", g)
			evs = append(evs, e)
		}
	}
	s := memStore(t, now, evs...)
	r := mustQuery(t, s, now, "from=-1h&group=client&metrics=req,share_req&top=20")
	if len(r.Rows) != 21 || r.Meta.GroupsTruncated != 5 {
		t.Fatalf("rows=%d truncated=%d, want 21/5", len(r.Rows), r.Meta.GroupsTruncated)
	}
	if r.Rows[0][0] != "c24" || r.Rows[0][1] != int64(25) {
		t.Errorf("first row = %v, want the largest group c24 with 25", r.Rows[0])
	}
	last := r.Rows[20]
	if last[0] != OtherGroup || last[1] != int64(1+2+3+4+5) {
		t.Errorf("other row = %v, want %s with 15", last, OtherGroup)
	}
	if r.Totals[1] != int64(325) || r.Totals[2] != 1.0 {
		t.Errorf("totals = %v, want 325 requests, share 1", r.Totals)
	}
}

func TestEngine_StepIsDenseAndAligned(t *testing.T) {
	now := at(2026, 10, 19, 12, 30)
	a := ev(at(2026, 10, 19, 10, 15), OutcomeOK)
	b := ev(at(2026, 10, 19, 12, 10), OutcomeError)
	b.ClientKeyTag = "bob"
	s := memStore(t, now, a, b)
	r := mustQuery(t, s, now, "from=-3h&step=1h&group=client&metrics=req,err_rate")
	// from = 09:30 aligned down to 09:00 → buckets 09,10,11,12 × 2 clients
	if r.Query.Step != "1h" || len(r.Rows) != 8 {
		t.Fatalf("step=%s rows=%d, want 1h/8 (dense)", r.Query.Step, len(r.Rows))
	}
	if !strings.HasPrefix(r.Query.From, "2026-10-19T09:00:00") {
		t.Errorf("from = %s, want aligned to 09:00", r.Query.From)
	}
	cells := map[string]any{}
	for _, row := range r.Rows {
		cells[row[0].(string)[11:13]+"/"+row[1].(string)] = row[3]
		if row[1] == "alice" && row[0].(string)[11:13] == "09" && row[2] != int64(0) {
			t.Errorf("empty cell must carry a 0 count: %v", row)
		}
	}
	if cells["10/alice"] != 0.0 || cells["12/bob"] != 1.0 || cells["11/bob"] != nil {
		t.Errorf("cells = %v", cells)
	}
}

func TestEngine_Filters(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	a := ev(now.Add(-time.Minute), OutcomeOK)
	b := ev(now.Add(-time.Minute), OutcomeError)
	b.Attempts = 4
	c := ev(now.Add(-time.Minute), OutcomeError)
	c.Provider, c.Model, c.Attempts, c.ErrorClass = "", "", 0, "no_candidate"
	d := ev(now.Add(-time.Minute), OutcomeOK)
	d.VModel = "deepseek:deepseek-chat" // direct routing names carry a colon
	s := memStore(t, now, a, b, c, d)
	for qs, want := range map[string]int64{
		"where=outcome!ok":                             2,
		"where=attempts:3%2B":                          1,
		"where=endpoint:":                              1, // the unrouted one
		"where=endpoint:p1:m1":                         3,
		"where=outcome:error|canceled&where=endpoint!": 1,
		"where=vmodel:deepseek:deepseek-chat":          1,
		"where=error_class:upstream_5xx|no_candidate":  2,
		"where=hour_of_day:11":                         4,
	} {
		r := mustQuery(t, s, now, "from=-1h&metrics=req&"+qs)
		if r.Totals[0] != want {
			t.Errorf("%s → %v, want %d", qs, r.Totals[0], want)
		}
	}
}

func TestEngine_LastN(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	var evs []Event
	for i := 0; i < 10; i++ {
		e := ev(now.Add(-time.Duration(10-i)*time.Minute), OutcomeOK)
		e.TTFTMS = int64(100 * (i + 1))
		evs = append(evs, e)
	}
	s := memStore(t, now, evs...)
	r := mustQuery(t, s, now, "last=3&metrics=req,ttft_avg")
	if r.Totals[0] != int64(3) || r.Totals[1] != float64(900) {
		t.Errorf("last=3 → %v, want 3 requests averaging the newest TTFTs (800,900,1000)", r.Totals)
	}
	if r.Query.From != "" || r.Query.Last != 3 {
		t.Errorf("echo = %+v", r.Query)
	}
}

func TestEngine_Compare(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	s := memStore(t, now, ev(now.Add(-30*time.Minute), OutcomeOK), ev(now.Add(-90*time.Minute), OutcomeOK), ev(now.Add(-100*time.Minute), OutcomeOK))
	r := mustQuery(t, s, now, "from=-1h&metrics=req&compare=prev")
	if r.Totals[0] != int64(1) || r.Compare == nil || r.Compare.Totals[0] != int64(2) {
		t.Errorf("now=%v prev=%+v, want 1 vs 2", r.Totals, r.Compare)
	}
}

func TestEngine_CompareBucketed(t *testing.T) {
	now := at(2026, 10, 19, 12, 34)
	s := memStore(t, now, ev(now.Add(-10*time.Minute), OutcomeOK), ev(now.Add(-24*time.Hour-10*time.Minute), OutcomeOK))
	r := mustQuery(t, s, now, "from=-24h&step=15m&metrics=req&compare=prev")
	if r.Compare == nil || len(r.Rows) != len(r.Compare.Rows) {
		t.Fatalf("bucket counts differ: %d now vs %+v", len(r.Rows), r.Compare)
	}
	from, _ := time.Parse(time.RFC3339, r.Query.From)
	pfrom, _ := time.Parse(time.RFC3339, r.Compare.From)
	if got := from.Sub(pfrom); got != 24*time.Hour {
		t.Errorf("previous period starts %v earlier, want exactly 24h", got)
	}
	i := len(r.Rows) - 2 // the 12:15 bucket, which holds both events' wall-clock time
	if r.Rows[i][1] != int64(1) || r.Compare.Rows[i][1] != int64(1) {
		t.Errorf("the same wall-clock bucket one period apart must line up: now %v prev %v", r.Rows[i], r.Compare.Rows[i])
	}
}

func TestEngine_Events(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	a := ev(now.Add(-3*time.Minute), OutcomeOK)
	b := ev(now.Add(-2*time.Minute), OutcomeError)
	c := ev(now.Add(-1*time.Minute), OutcomeError)
	s := memStore(t, now, a, b, c)
	got := s.Events(EventsQuery{Where: []Filter{{Dim: "outcome", Values: []string{"ok"}, Not: true}}, Limit: 10})
	if len(got) != 2 || !got[0].TS.After(got[1].TS) || got[0].Outcome != OutcomeError {
		t.Errorf("events = %+v, want the two errors newest first", got)
	}
	if got := s.Events(EventsQuery{Limit: 1}); len(got) != 1 || !got[0].TS.Equal(c.TS) {
		t.Errorf("limit 1 = %+v, want the newest", got)
	}
	if got := s.Events(EventsQuery{From: now.Add(-150 * time.Second), Limit: 10}); len(got) != 2 {
		t.Errorf("from filter = %d events, want 2", len(got))
	}
}

func TestParseQuery(t *testing.T) {
	now := at(2026, 10, 19, 12, 34)
	cases := map[string]string{ // query → expected error substring ("" = ok)
		"":                                   "",
		"metrics=nope":                       "unknown metric",
		"group=nope":                         "unknown dimension",
		"group=client,client":                "grouped twice",
		"group=client,vmodel,endpoint,model": "at most 3",
		"where=client=alice":                 "bad where",
		"where=nope:x":                       "unknown dimension",
		"from=-91d":                          "at most 90 days",
		"from=now&to=-1h":                    "before",
		"from=yesterday":                     "cannot parse",
		"step=2h":                            "unknown step",
		"top=0":                              "top must be",
		"last=5&from=-1h":                    "cannot be combined",
		"last=5&step=1h":                     "cannot be combined",
		"last=5000":                          "last must be",
		"compare=yes":                        "compare must be",
	}
	for qs, wantErr := range cases {
		v, _ := url.ParseQuery(qs)
		_, err := ParseQuery(v, now)
		if wantErr == "" && err != nil || wantErr != "" && (err == nil || !strings.Contains(err.Error(), wantErr)) {
			t.Errorf("%q: err = %v, want %q", qs, err, wantErr)
		}
	}

	steps := map[string]string{
		"from=-1h&step=auto":  "1m",
		"from=-6h&step=auto":  "5m",
		"from=-24h&step=auto": "15m",
		"from=-7d&step=auto":  "1h",
		"from=-30d&step=auto": "6h",
		"from=-90d&step=auto": "1d",
		"from=-7d&step=1m":    "1h", // 10080 buckets → coarsened past 500
		"from=-1d&step=1m":    "5m",
	}
	for qs, want := range steps {
		v, _ := url.ParseQuery(qs)
		q, err := ParseQuery(v, now)
		if err != nil || q.StepName != want {
			t.Errorf("%s → step %s (%v), want %s", qs, q.StepName, err, want)
		}
	}

	v, _ := url.ParseQuery("from=-6d&step=1d")
	q, _ := ParseQuery(v, now)
	if !q.From.Equal(at(2026, 10, 13, 0, 0)) || len(q.buckets()) != 7 {
		t.Errorf("-6d/1d → from %v with %d buckets, want local midnight of the 13th and 7 days", q.From, len(q.buckets()))
	}
	for s, want := range map[string]time.Time{
		"today":      at(2026, 10, 19, 0, 0),
		"2026-10-01": at(2026, 10, 1, 0, 0),
		"-90m":       now.Add(-90 * time.Minute),
		"now":        now,
	} {
		if got, err := parseTime(s, now); err != nil || !got.Equal(want) {
			t.Errorf("parseTime(%q) = %v %v, want %v", s, got, err, want)
		}
	}
}

// BenchmarkQuery90d measures the design's sizing claim: a 90-day window at
// roughly 11k requests a day, grouped by endpoint with percentiles.
func BenchmarkQuery90d(b *testing.B) {
	now := at(2026, 10, 19, 12, 0)
	s, err := openAt(b.TempDir(), func() time.Time { return now }, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	r := rand.New(rand.NewSource(1))
	const n = 1_000_000
	start := now.AddDate(0, 0, -90)
	for i := 0; i < n; i++ {
		e := ev(start.Add(time.Duration(i)*90*24*time.Hour/n), OutcomeOK)
		e.Provider, e.Model = fmt.Sprintf("p%d", r.Intn(8)), fmt.Sprintf("m%d", r.Intn(12))
		e.TTFTMS, e.DurMS = int64(r.Intn(5000)), int64(5000+r.Intn(60000))
		s.rows = append(s.rows, s.dict.toRow(e)) // all in the open day: measures the engine, not parsing
	}
	v, _ := url.ParseQuery("from=-90d&group=endpoint&metrics=req,tok_total,ttft_p50,ttft_p90,toks_p10")
	q, _ := ParseQuery(v, now)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Query(q)
	}
}
