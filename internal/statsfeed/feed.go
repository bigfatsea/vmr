// Package statsfeed is the one projection from an audit record to a
// statistics event. The live completion hook (internal/server) and `vmr
// stats rebuild` (cmd/vmr) both call FromRecord, so a request is booked the
// same way whether it is counted as it happens or rebuilt from the audit
// log months later — the agreement is structural, not a convention two
// call sites have to keep.
package statsfeed

import (
	"encoding/json"
	"time"

	"vmr/internal/audit"
	"vmr/internal/core"
	"vmr/internal/stats"
)

// FromRecord projects rec into an event and reports whether it is booked
// at all. Two admission rules, identical for both callers:
//   - a request rejected before the protocol probe named a model (401,
//     unreadable body, model-less JSON) has no virtual model to attribute
//     and is not booked — port scanners' 401s would otherwise flood the log;
//   - a `vmr replay` record (replay_of set) is not booked: replays go
//     straight to the upstream, never through the live completion hook, so
//     booking them on rebuild would make the two paths disagree.
//
// Endpoint identity comes from the winning (forwarded) attempt, or from the
// terminal attempt when nothing was forwarded, so a failed request still
// groups under the endpoint that failed. Tokens only ever come from a
// forwarded attempt. With no attempt at all (every candidate cooling down,
// a pre-dispatch build error) there is no upstream to name: Status falls
// back to the client-facing code and an error is classed "no_candidate".
//
// A terminal attempt that carries an error class while the record's outcome
// says ok (a response cut off after the transport committed to 200, class
// "truncated"; on records older than the forwarded stamp also a softblocked
// 2xx) is booked as an error: the outcome follows the status code, but the
// client got a broken response, and an ok booking would keep it out of the
// failure views and let its duration into the success percentiles. Tokens
// of a forwarded one still count — they were spent.
//
// A request that is not booked still comes back with its arrival time, so a
// caller can tell which day it belongs to.
func FromRecord(rec *audit.Record) (stats.Event, bool) {
	if rec == nil {
		return stats.Event{}, false
	}
	if rec.Model == "" || rec.ReplayOf != "" {
		return stats.Event{TS: rec.TS}, false
	}
	e := stats.Event{
		TS:           rec.TS,
		VModel:       rec.Model,
		Protocol:     rec.Protocol,
		Stream:       rec.Stream,
		ClientKeyTag: rec.ClientKeyTag,
		Outcome:      rec.Outcome,
		Attempts:     len(rec.Attempts),
		DurMS:        rec.DurMS,
		TTFTMS:       rec.TTFTMS,
	}
	if rec.Facts != nil {
		e.HasTools, e.HasImage = rec.Facts.HasTools, rec.Facts.HasImage
	}
	win := -1
	for i := range rec.Attempts {
		if rec.Attempts[i].IsForwarded() {
			win = i
			break
		}
	}
	if len(rec.Attempts) == 0 {
		if rec.Client.Response != nil {
			e.Status = rec.Client.Response.Status
		}
		if rec.Outcome == stats.OutcomeError {
			e.ErrorClass = "no_candidate"
		}
		return e, true
	}
	final := &rec.Attempts[len(rec.Attempts)-1]
	if win >= 0 {
		final = &rec.Attempts[win]
	}
	e.Provider, e.Model, e.KeyLabel = final.Provider, final.Model, final.KeyLabel
	e.Forwarded = win >= 0
	if e.Forwarded && final.Tokens != nil {
		e.Tokens = stats.Tokens{
			In: final.Tokens.In, Out: final.Tokens.Out,
			CacheRead: final.Tokens.CacheRead, CacheWrite: final.Tokens.CacheWrite,
		}
	}
	e.ErrorClass = final.ErrorClass
	// Records written before the server checked DeliveredInFull carry
	// "canceled" for a response the client received whole; book them as
	// the fixed server would have. A no-op on every newer record.
	if e.Outcome == stats.OutcomeCanceled && rec.DeliveredInFull() {
		e.Outcome = stats.OutcomeOK
	}
	if e.Outcome == stats.OutcomeOK && e.ErrorClass != "" {
		e.Outcome = stats.OutcomeError
	}
	if final.Response != nil {
		e.Status = final.Response.Status
	}
	return e, true
}

// FromJSON is FromRecord over one raw audit log line, for `vmr stats
// rebuild`. It strips the payload members first (see prune) and decodes
// only the fields the projection reads, then hands the result to
// FromRecord, so the booking rules still live in one place. The live-vs-rebuild differential test pins that this narrow
// decode and a full audit.Record decode project identically.
func FromJSON(line []byte) (stats.Event, bool, error) {
	if _, pruned, ok := prune(line, 0, make([]byte, 0, 2048)); ok {
		line = pruned
	}
	var lr leanRecord
	if err := json.Unmarshal(line, &lr); err != nil {
		return stats.Event{}, false, err
	}
	rec := audit.Record{
		TS: lr.TS, DurMS: lr.DurMS, TTFTMS: lr.TTFTMS, Model: lr.Model,
		Protocol: audit.CanonicalProtocol(lr.Protocol), Stream: lr.Stream, Outcome: lr.Outcome,
		ReplayOf: lr.ReplayOf, ClientKeyTag: lr.ClientKeyTag, Facts: lr.Facts,
	}
	if lr.Client.Response != nil {
		rec.Client.Response = &audit.Message{Status: lr.Client.Response.Status}
	}
	for _, a := range lr.Attempts {
		at := audit.Attempt{
			Provider: a.Provider, Model: a.Model, KeyLabel: a.KeyLabel,
			ErrorClass: a.ErrorClass, Forwarded: a.Forwarded, Tokens: a.Tokens,
		}
		if a.Response != nil {
			at.Response = &audit.Message{Status: a.Response.Status}
		}
		rec.Attempts = append(rec.Attempts, at)
	}
	e, ok := FromRecord(&rec)
	return e, ok, nil
}

// lean* mirror the audit record's JSON names for exactly the fields
// FromRecord reads (including what Attempt.IsForwarded falls back on).
type leanStatus struct {
	Status int `json:"status"`
}

type leanAttempt struct {
	Provider   string            `json:"provider"`
	Model      string            `json:"model"`
	KeyLabel   string            `json:"key_label"`
	ErrorClass string            `json:"error_class"`
	Forwarded  bool              `json:"forwarded"`
	Tokens     *audit.TokenCount `json:"tokens"`
	Response   *leanStatus       `json:"response"`
}

type leanRecord struct {
	TS       time.Time `json:"ts"`
	DurMS    int64     `json:"dur_ms"`
	TTFTMS   int64     `json:"ttft_ms"`
	Model    string    `json:"model"`
	Protocol string    `json:"protocol"`
	Stream   bool      `json:"stream"`
	Outcome  string    `json:"outcome"`
	Client   struct {
		Response *leanStatus `json:"response"`
	} `json:"client"`
	Attempts     []leanAttempt      `json:"attempts"`
	Facts        *core.RequestFacts `json:"facts"`
	ReplayOf     string             `json:"replay_of"`
	ClientKeyTag string             `json:"client_key_tag"`
}
