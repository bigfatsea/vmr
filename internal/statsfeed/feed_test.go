package statsfeed

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/stats"
)

func forwarded(p, kl, m string) audit.Attempt {
	return audit.Attempt{
		Provider: p, KeyLabel: kl, Model: m, Forwarded: true,
		Response: &audit.Message{Status: 200},
		Tokens:   &audit.TokenCount{In: 10, Out: 5, CacheRead: 100, CacheWrite: 7},
	}
}

func failed(class string, status int) audit.Attempt {
	a := audit.Attempt{ErrorClass: class}
	if status != 0 {
		a.Response = &audit.Message{Status: status}
	}
	return a
}

// TestFromRecord_TerminalAttempt pins the attribution rules: error class
// and status quote the terminal attempt (the winning one when forwarded,
// else the last) verbatim, identity rides that same attempt, tokens only
// ever come from a forwarded attempt.
func TestFromRecord_TerminalAttempt(t *testing.T) {
	cases := []struct {
		name       string
		attempts   []audit.Attempt
		wantClass  string
		wantStatus int
		wantTries  int
	}{
		{"no attempts", nil, "no_candidate", 0, 0},
		{"all failed: last attempt wins", []audit.Attempt{failed("auth", 401), failed("upstream_5xx", 502)}, "upstream_5xx", 502, 2},
		{"winning attempt beats later failures", []audit.Attempt{forwarded("p1", "main", "m1"), failed("upstream_5xx", 502)}, "", 200, 2},
		{"single forwarded", []audit.Attempt{forwarded("p1", "main", "m1")}, "", 200, 1},
		{"no response keeps status 0", []audit.Attempt{failed("network", 0)}, "network", 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, ok := FromRecord(&audit.Record{Model: "vm", Outcome: "error", Attempts: c.attempts})
			if !ok || e.ErrorClass != c.wantClass || e.Status != c.wantStatus || e.Attempts != c.wantTries {
				t.Errorf("got ok=%v class=%q status=%d tries=%d", ok, e.ErrorClass, e.Status, e.Attempts)
			}
		})
	}

	e, _ := FromRecord(&audit.Record{Model: "vm", Outcome: "ok", Attempts: []audit.Attempt{forwarded("p1", "main", "m1")}})
	if e.Provider != "p1" || e.KeyLabel != "main" || e.Model != "m1" || !e.Forwarded || e.Tokens != (stats.Tokens{In: 10, Out: 5, CacheRead: 100, CacheWrite: 7}) {
		t.Errorf("forwarded event = %+v", e)
	}

	// A failed attempt that targeted a real endpoint keeps its identity (so
	// it groups under the endpoint that failed) but is not forwarded and
	// carries no tokens.
	tried := audit.Attempt{Provider: "p2", KeyLabel: "sec", Model: "m2", ErrorClass: "upstream_5xx", Response: &audit.Message{Status: 502}}
	e, _ = FromRecord(&audit.Record{Model: "vm", Outcome: "error", Attempts: []audit.Attempt{tried}})
	if e.Provider != "p2" || e.Forwarded || e.Tokens != (stats.Tokens{}) {
		t.Errorf("tried-but-failed event = %+v", e)
	}

	// No attempt: status falls back to the client-facing code; a canceled
	// request is not given an error class.
	e, _ = FromRecord(&audit.Record{Model: "vm", Outcome: "error", Client: audit.Exchange{Response: &audit.Message{Status: 503}}})
	if e.Status != 503 || e.ErrorClass != "no_candidate" {
		t.Errorf("no-attempt error = %+v", e)
	}
	e, _ = FromRecord(&audit.Record{Model: "vm", Outcome: "canceled", Client: audit.Exchange{Response: &audit.Message{Status: 499}}})
	if e.ErrorClass != "" {
		t.Errorf("no-attempt cancel must keep error_class empty: %+v", e)
	}
}

// TestFromRecord_HangUpAfterDelivery: records written before the server
// checked DeliveredInFull say "canceled" for a response the client received
// whole; they book as ok. A cancel the relay saw mid-transfer stays one.
func TestFromRecord_HangUpAfterDelivery(t *testing.T) {
	cut := forwarded("p1", "main", "m1")
	cut.ErrorClass = "canceled"
	for _, c := range []struct {
		name     string
		attempts []audit.Attempt
		want     string
	}{
		{"delivered in full", []audit.Attempt{forwarded("p1", "main", "m1")}, stats.OutcomeOK},
		{"cut mid-transfer", []audit.Attempt{cut}, stats.OutcomeCanceled},
		{"before any attempt", nil, stats.OutcomeCanceled},
		{"before a response", []audit.Attempt{failed("canceled", 0)}, stats.OutcomeCanceled},
	} {
		e, _ := FromRecord(&audit.Record{Model: "vm", Outcome: "canceled", Attempts: c.attempts})
		if e.Outcome != c.want {
			t.Errorf("%s: outcome = %q, want %q", c.name, e.Outcome, c.want)
		}
	}
}

// A response cut off after the transport committed to 200 has outcome ok in
// the audit record (the status code decides) but is a failed request for the
// client: it books as an error, its spent tokens still count.
func TestFromRecord_TruncatedBooksAsError(t *testing.T) {
	cut := forwarded("p1", "main", "m1")
	cut.ErrorClass = "truncated"
	e, ok := FromRecord(&audit.Record{Model: "vm", Outcome: "ok", DurMS: 900, Stream: true, TTFTMS: 100, Attempts: []audit.Attempt{cut}})
	if !ok || e.Outcome != stats.OutcomeError || e.ErrorClass != "truncated" || !e.Forwarded || e.Tokens.Out != 5 {
		t.Errorf("truncated stream: %+v", e)
	}
	// Pre-stamp records: the old heuristic does not call a classed 2xx forwarded.
	legacy := audit.Attempt{Provider: "p1", Model: "m1", ErrorClass: "truncated", Response: &audit.Message{Status: 200}}
	le, _ := FromRecord(&audit.Record{Model: "vm", Outcome: "ok", Attempts: []audit.Attempt{legacy}})
	if le.Outcome != stats.OutcomeError || le.Forwarded {
		t.Errorf("legacy truncated record: %+v", le)
	}
	clean, _ := FromRecord(&audit.Record{Model: "vm", Outcome: "ok", Attempts: []audit.Attempt{forwarded("p1", "main", "m1")}})
	if clean.Outcome != stats.OutcomeOK || clean.ErrorClass != "" {
		t.Errorf("clean response must stay ok: %+v", clean)
	}
}

// An unbooked record still reports when it arrived, so a rebuild can tell
// which day it belongs to instead of filing it under the zero time.
func TestFromRecord_UnbookedKeepsArrival(t *testing.T) {
	ts := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	e, ok := FromRecord(&audit.Record{TS: ts, Outcome: "error"})
	if ok || !e.TS.Equal(ts) {
		t.Errorf("unbooked = %+v, %v", e, ok)
	}
}

func TestFromRecord_Admission(t *testing.T) {
	if _, ok := FromRecord(nil); ok {
		t.Error("nil record booked")
	}
	if _, ok := FromRecord(&audit.Record{Outcome: "error"}); ok {
		t.Error("pre-probe rejection (no model) booked")
	}
	if _, ok := FromRecord(&audit.Record{Model: "vm", Outcome: "ok", ReplayOf: "vmr-audit-2026-10-01.jsonl:3"}); ok {
		t.Error("replay record booked")
	}
	e, ok := FromRecord(&audit.Record{Model: "vm", Outcome: "ok", Facts: &core.RequestFacts{HasTools: true, HasImage: true}})
	if !ok || !e.HasTools || !e.HasImage {
		t.Errorf("facts not carried: %+v", e)
	}
}

// TestFromJSON_MatchesLivePath is the live-vs-rebuild differential: records
// go through the real audit logger to disk, and the narrow FromJSON decode
// of each written line must project exactly like the live hook's
// FromRecord over the in-memory record (and like a full audit.Record
// decode of the same line).
func TestFromJSON_MatchesLivePath(t *testing.T) {
	dir := t.TempDir()
	lg, err := audit.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().Add(-time.Minute).Round(time.Millisecond)
	body := json.RawMessage(`{"messages":[{"role":"user","content":"a \"quoted\" body with {braces} and [brackets]"}],"headers":{"x":"y"}}`)
	att := forwarded("p1", "main", "m1")
	att.Request = audit.Message{Body: body}
	att.Response.Body = json.RawMessage(`{"usage":{"prompt_tokens":10}}`)
	recs := []*audit.Record{
		{TS: ts, DurMS: 1200, TTFTMS: 300, Model: "vm", Protocol: core.ProtocolOpenAICompletions, Stream: true, Outcome: "ok",
			ClientKeyTag: "alice", Facts: &core.RequestFacts{HasTools: true}, Attempts: []audit.Attempt{att},
			Client: audit.Exchange{Request: audit.Message{Body: body}, Response: &audit.Message{Status: 200, Body: "plain text body"}}},
		{TS: ts, DurMS: 50, Model: "vm", Protocol: core.ProtocolAnthropicMessages, Outcome: "error",
			Attempts: []audit.Attempt{failed("auth", 401), failed("upstream_5xx", 502)}},
		{TS: ts, Model: "vm", Outcome: "error", Client: audit.Exchange{Response: &audit.Message{Status: 503}}},
		{TS: ts, DurMS: 900, TTFTMS: 200, Model: "vm", Stream: true, Outcome: "canceled", Attempts: []audit.Attempt{forwarded("p1", "main", "m1")}},
		{TS: ts, Model: "vm", Outcome: "ok", ReplayOf: "x.jsonl:1", Attempts: []audit.Attempt{forwarded("p1", "main", "m1")}},
		{TS: ts, Outcome: "error"},
	}
	for _, r := range recs {
		if err := lg.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	path := lg.Path()
	lg.Close()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	i := 0
	err = audit.ForEachLine(f, audit.MaxLogLine, func(line []byte) {
		want, wantOK := FromRecord(recs[i])
		got, gotOK, err := FromJSON(line)
		if err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		var full audit.Record
		if err := json.Unmarshal(line, &full); err != nil {
			t.Fatal(err)
		}
		fullE, fullOK := FromRecord(&full)
		if gotOK != wantOK || fullOK != wantOK {
			t.Fatalf("line %d: booked live=%v rebuild=%v full=%v", i, wantOK, gotOK, fullOK)
		}
		if wantOK && (!sameEvent(got, want) || !sameEvent(fullE, want)) {
			t.Errorf("line %d:\n live    %+v\n rebuild %+v\n full    %+v", i, want, got, fullE)
		}
		i++
	}, nil)
	if err != nil || i != len(recs) {
		t.Fatalf("read %d lines (err %v), want %d", i, err, len(recs))
	}
}

func sameEvent(a, b stats.Event) bool {
	if !a.TS.Equal(b.TS) {
		return false
	}
	a.TS, b.TS = time.Time{}, time.Time{}
	return a == b
}

func TestPrune(t *testing.T) {
	in := `{"a":1,"body":{"x":[1,2,{"body":"y"}]},"n":{"headers":{"k":"v"},"s":"t\"q"},"l":[{"body":"z","k":2}],"raw_pre_strip":"q"}`
	_, out, ok := prune([]byte(in), 0, nil)
	want := `{"a":1,"body":null,"n":{"headers":null,"s":"t\"q"},"l":[{"body":null,"k":2}],"raw_pre_strip":null}`
	if !ok || string(out) != want {
		t.Errorf("prune = %s (ok %v)\nwant    %s", out, ok, want)
	}
	if _, _, ok := prune([]byte(`{"a":`), 0, nil); ok {
		t.Error("truncated JSON must not prune cleanly")
	}
}
