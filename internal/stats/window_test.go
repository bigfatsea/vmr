package stats

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A range's events must be reachable wherever it sits in history, and a
// short range must not read history it cannot contain.
func TestEngine_EventsFollowRange(t *testing.T) {
	dir := t.TempDir()
	now := at(2026, 10, 19, 12, 0)
	old := at(2026, 4, 2, 9, 30)
	if err := WriteDay(dir, "2026-04-02", []Event{ev(old, OutcomeOK), ev(old.Add(time.Minute), OutcomeError)}, now); err != nil {
		t.Fatal(err)
	}
	s, err := openAt(dir, func() time.Time { return now }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Record(ev(now.Add(-10*time.Minute), OutcomeOK))

	got := s.Events(EventsQuery{From: at(2026, 4, 2, 0, 0), To: at(2026, 4, 3, 0, 0), Limit: 10})
	if len(got) != 2 {
		t.Fatalf("events in a 200-day-old range = %d, want 2 (the aggregate sees them)", len(got))
	}
	r := mustQuery(t, s, now, "from=2026-04-02&to=2026-04-03&metrics=req")
	if total(t, r, "req") != int64(2) {
		t.Fatalf("query disagrees with events: %v", r.Totals)
	}

	s.loadMu.Lock()
	s.cache = map[string]*closedDay{}
	s.loadMu.Unlock()
	if got := s.Events(EventsQuery{From: now.Add(-time.Hour), Limit: 10}); len(got) != 1 {
		t.Fatalf("last hour = %d events, want 1", len(got))
	}
	s.loadMu.Lock()
	n := len(s.cache)
	s.loadMu.Unlock()
	if n > 2 {
		t.Errorf("a one-hour events query loaded %d closed days, want at most 2", n)
	}
}

// The invariant the dashboard leans on: bars sum to the headline number.
// Bucket alignment may only choose bucket origins, never widen the rows
// that count.
func TestEngine_BucketedTotalEqualsUnbucketed(t *testing.T) {
	now := at(2026, 10, 19, 12, 34)
	var evs []Event
	for m := 0; m < 3*24*60; m += 7 {
		evs = append(evs, ev(now.Add(-time.Duration(m)*time.Minute), OutcomeOK))
	}
	s := memStore(t, now, evs...)
	for _, c := range []struct{ from, step string }{
		{"-24h", "15m"}, {"-6h", "5m"}, {"-3d", "1h"}, {"today", "5m"}, {"-3d", "6h"}, {"-2d", "1d"},
	} {
		flat := mustQuery(t, s, now, "from="+c.from+"&metrics=req")
		for _, step := range []string{c.step, "auto"} {
			b := mustQuery(t, s, now, "from="+c.from+"&metrics=req&step="+step)
			var sum int64
			for _, row := range b.Rows {
				sum += row[1].(int64)
			}
			if sum != flat.Totals[0] || total(t, b, "req") != flat.Totals[0] {
				t.Errorf("from=%s step=%s: bars sum %d, totals %v, unbucketed %v", c.from, step, sum, total(t, b, "req"), flat.Totals[0])
			}
		}
	}
}

func TestEngine_CompareWindows(t *testing.T) {
	now := at(2026, 10, 19, 12, 34)
	s := memStore(t, now,
		ev(at(2026, 10, 19, 9, 0), OutcomeOK),
		ev(at(2026, 10, 18, 9, 0), OutcomeOK),
		ev(at(2026, 10, 18, 20, 0), OutcomeOK), // yesterday evening: not "the same time yesterday"
		ev(at(2026, 10, 18, 23, 0), OutcomeOK),
	)
	byDay := mustQuery(t, s, now, "from=today&metrics=req&compare=day")
	if byDay.Compare == nil || byDay.Compare.Totals[0] != int64(1) {
		t.Fatalf("compare=day: %+v, want yesterday's 00:00-12:34 only (1 event)", byDay.Compare)
	}
	if byDay.Compare.From != fmtTime(at(2026, 10, 18, 0, 0)) || byDay.Compare.To != fmtTime(at(2026, 10, 18, 12, 34)) {
		t.Errorf("compare=day window = %s → %s", byDay.Compare.From, byDay.Compare.To)
	}
	if byDay.Query.CompareBy != "day" {
		t.Errorf("echo compare_by = %q", byDay.Query.CompareBy)
	}
	prev := mustQuery(t, s, now, "from=today&metrics=req&compare=prev")
	if prev.Compare.Totals[0] == byDay.Compare.Totals[0] {
		t.Errorf("compare=prev (the stretch before midnight) should differ from compare=day here: both %v", prev.Compare.Totals[0])
	}

	// Bucketed and unbucketed ask the same window, so a chart's dashed line
	// and the headline delta describe the same comparison.
	chart := mustQuery(t, s, now, "from=today&metrics=req&compare=day&step=5m")
	if chart.Compare.Totals[1] != byDay.Compare.Totals[0] || len(chart.Rows) != len(chart.Compare.Rows) {
		t.Errorf("chart compare %v over %d/%d buckets, headline %v", chart.Compare.Totals[1], len(chart.Rows), len(chart.Compare.Rows), byDay.Compare.Totals[0])
	}
	week := mustQuery(t, s, now, "from=-1h&metrics=req&compare=week")
	if week.Compare.From != fmtTime(now.Add(-time.Hour).AddDate(0, 0, -7)) {
		t.Errorf("compare=week from = %s", week.Compare.From)
	}
}

// Unknown is not zero: forwarded events without a token record stay out of
// the token sums and are counted, per window.
func TestEngine_TokensUnknown(t *testing.T) {
	now := at(2026, 10, 19, 12, 0)
	known := ev(now.Add(-30*time.Minute), OutcomeOK)
	bare := ev(now.Add(-20*time.Minute), OutcomeOK)
	bare.Tokens = Tokens{}
	old := ev(now.Add(-90*time.Minute), OutcomeOK)
	old.Tokens = Tokens{}
	s := memStore(t, now, known, bare, old)
	r := mustQuery(t, s, now, "from=-1h&metrics=req,tok_total,cache_hit&compare=prev")
	if r.Meta.TokensUnknown != 1 || r.Compare.TokensUnknown != 1 {
		t.Errorf("tokens_unknown = %d / compare %d, want 1 / 1", r.Meta.TokensUnknown, r.Compare.TokensUnknown)
	}
	if total(t, r, "req") != int64(2) || total(t, r, "tok_total") != int64(165) {
		t.Errorf("totals %v: both requests count, only the stamped one has tokens", r.Totals)
	}
	if _, err := ParseQuery(map[string][]string{"compare": {"bogus"}}, now); err == nil {
		t.Error("unknown compare value must be rejected")
	}
}

func TestEvent_UnknownTokensAreNotWritten(t *testing.T) {
	e := ev(at(2026, 10, 19, 12, 0), OutcomeOK)
	e.Tokens = Tokens{}
	b, _ := e.MarshalJSON()
	if strings.Contains(string(b), `"tokens"`) {
		t.Errorf("an unmeasured token record must not be persisted as zeros: %s", b)
	}
}

func TestDayOf_FilesByCompletionDay(t *testing.T) {
	e := ev(at(2026, 10, 18, 23, 59), OutcomeOK)
	e.DurMS = 90_000
	if got := DayOf(e); got != "2026-10-19" {
		t.Errorf("DayOf = %s, want the completion day 2026-10-19", got)
	}
	e.DurMS = 1000
	if got := DayOf(e); got != "2026-10-18" {
		t.Errorf("DayOf = %s, want 2026-10-18", got)
	}
}

func TestParseWhere_EscapedPipes(t *testing.T) {
	fs, err := parseWhere([]string{`vmodel:a\|b|c:d|e\\f`, `client!`})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a|b", "c:d", `e\f`}
	if fmt.Sprint(fs[0].Values) != fmt.Sprint(want) || fs[0].Not {
		t.Errorf("values = %q, want %q", fs[0].Values, want)
	}
	if len(fs[1].Values) != 1 || fs[1].Values[0] != "" || !fs[1].Not {
		t.Errorf("empty value = %+v", fs[1])
	}
}

func TestDict_Overflow(t *testing.T) {
	d := newDict()
	var last uint32
	for i := 0; i < maxDictEntries+100; i++ {
		last = d.id(fmt.Sprintf("model-%d", i))
	}
	if d.str(last) != OverflowValue || len(d.strs) > maxDictEntries {
		t.Errorf("dictionary grew past its cap: %d entries, last = %q", len(d.strs), d.str(last))
	}
	if d.str(d.id("model-5")) != "model-5" {
		t.Error("a value interned before the cap must keep resolving")
	}
}

// "Up to now" includes a request that completed in the very millisecond the
// query runs: rows carry millisecond timestamps and ranges are half-open.
func TestHandler_QueryIncludesCurrentMillisecond(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 500_000, time.Local) // 12:00:00.0005
	s := memStore(t, now, ev(now.Truncate(time.Millisecond), OutcomeOK))
	h := NewHandler(s)
	for _, path := range []string{"/stats/query?from=-1h&metrics=req", "/stats/events?from=-1h"} {
		rec := httptest.NewRecorder()
		if strings.HasPrefix(path, "/stats/query") {
			h.Query(rec, httptest.NewRequest("GET", path, nil))
			if !strings.Contains(rec.Body.String(), `"totals":[1]`) {
				t.Errorf("%s: %s", path, rec.Body.String())
			}
		} else {
			h.Events(rec, httptest.NewRequest("GET", path, nil))
			if !strings.Contains(rec.Body.String(), `"vmodel":"coding"`) {
				t.Errorf("%s: %s", path, rec.Body.String())
			}
		}
	}
}
