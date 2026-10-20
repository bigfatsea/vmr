// Package stats is vmr's request statistics module: a per-request event
// log kept as one JSONL file per local calendar day, an in-process query
// engine over it, and the HTTP surface (/stats.html, /stats/*) that serves
// both the statistics page and the console's stats panels.
//
// It is a zero-internal-dependency leaf on purpose (archtest pins it): the
// only way data gets in is a value of type Event handed to Store.Record,
// and the only way data gets out is the HTTP handlers or Store.Query. The
// projection from an audit record to an Event lives in internal/statsfeed,
// the one place that knows both shapes — so the live completion hook and
// `vmr stats rebuild` can never disagree about what a request counts as.
//
// Events are facts, not answers: nothing is pre-aggregated on the write
// side. Every number the console shows is computed at read time from the
// rows that match the query, which is what lets any time range, bucket
// size, grouping and filter combination be asked after the fact.
package stats

import (
	"encoding/json"
	"fmt"
	"time"
)

// Outcome values, verbatim the audit record's outcome strings.
const (
	OutcomeOK       = "ok"
	OutcomeError    = "error"
	OutcomeCanceled = "canceled"
)

// SrcAudit marks an event written by `vmr stats rebuild` rather than by the
// live completion hook.
const SrcAudit = "audit"

// Tokens is the raw four-way token stamp of the winning attempt. In is the
// fresh (net-of-cache) input, the same basis quota metering charges.
type Tokens struct {
	In         int64 `json:"in"`
	Out        int64 `json:"out"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Event is one booked request. Provider/Model/KeyLabel name the winning
// attempt's endpoint, or the terminal attempt's when nothing was forwarded;
// they are empty only when no attempt was made at all. Tokens are only
// meaningful when Forwarded, and a forwarded event with all-zero Tokens has
// no token record (unknown, not zero). TTFTMS 0 means "not measured".
type Event struct {
	TS           time.Time
	VModel       string
	Protocol     string
	Stream       bool
	ClientKeyTag string
	Outcome      string
	Status       int
	ErrorClass   string
	Attempts     int
	Provider     string
	Model        string
	KeyLabel     string
	Forwarded    bool
	DurMS        int64
	TTFTMS       int64
	Tokens       Tokens
	HasTools     bool
	HasImage     bool
	Src          string
}

// tsLayout is the on-disk timestamp: millisecond precision with the
// write-time offset preserved, the same zone discipline the audit log uses.
const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// wireEvent is the JSONL line shape. Field names deliberately match the
// audit log's (client_key_tag, key_label, the four token components) so one
// jq habit works on both files.
type wireEvent struct {
	TS           string  `json:"ts"`
	VModel       string  `json:"vmodel"`
	Protocol     string  `json:"protocol"`
	Stream       bool    `json:"stream"`
	ClientKeyTag string  `json:"client_key_tag,omitempty"`
	Outcome      string  `json:"outcome"`
	Status       int     `json:"status,omitempty"`
	ErrorClass   string  `json:"error_class,omitempty"`
	Attempts     int     `json:"attempts"`
	Provider     string  `json:"provider,omitempty"`
	Model        string  `json:"model,omitempty"`
	KeyLabel     string  `json:"key_label,omitempty"`
	Forwarded    bool    `json:"forwarded"`
	DurMS        int64   `json:"dur_ms"`
	TTFTMS       int64   `json:"ttft_ms"`
	Tokens       *Tokens `json:"tokens,omitempty"`
	HasTools     bool    `json:"has_tools,omitempty"`
	HasImage     bool    `json:"has_image,omitempty"`
	Src          string  `json:"src,omitempty"`
}

// MarshalJSON writes the on-disk line shape (also the /stats/events shape).
func (e Event) MarshalJSON() ([]byte, error) {
	w := wireEvent{
		TS: e.TS.Format(tsLayout), VModel: e.VModel, Protocol: e.Protocol, Stream: e.Stream,
		ClientKeyTag: e.ClientKeyTag, Outcome: e.Outcome, Status: e.Status, ErrorClass: e.ErrorClass,
		Attempts: e.Attempts, Provider: e.Provider, Model: e.Model, KeyLabel: e.KeyLabel,
		Forwarded: e.Forwarded, DurMS: e.DurMS, TTFTMS: e.TTFTMS,
		HasTools: e.HasTools, HasImage: e.HasImage, Src: e.Src,
	}
	// An all-zero record is "unknown", not a measured zero (see
	// flagNoTokens): leave it out so a file never states what nobody measured.
	if e.Forwarded && e.Tokens != (Tokens{}) {
		t := e.Tokens
		w.Tokens = &t
	}
	return json.Marshal(w)
}

// UnmarshalJSON reads one line; a missing or unparseable ts is an error so
// the reader skips the line instead of booking a request at the zero time.
func (e *Event) UnmarshalJSON(b []byte) error {
	var w wireEvent
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	ts, err := time.Parse(tsLayout, w.TS)
	if err != nil {
		if ts, err = time.Parse(time.RFC3339Nano, w.TS); err != nil {
			return fmt.Errorf("stats: bad ts %q", w.TS)
		}
	}
	*e = Event{
		TS: ts, VModel: w.VModel, Protocol: w.Protocol, Stream: w.Stream,
		ClientKeyTag: w.ClientKeyTag, Outcome: w.Outcome, Status: w.Status, ErrorClass: w.ErrorClass,
		Attempts: w.Attempts, Provider: w.Provider, Model: w.Model, KeyLabel: w.KeyLabel,
		Forwarded: w.Forwarded, DurMS: w.DurMS, TTFTMS: w.TTFTMS,
		HasTools: w.HasTools, HasImage: w.HasImage, Src: w.Src,
	}
	if w.Tokens != nil {
		e.Tokens = *w.Tokens
	}
	return nil
}
