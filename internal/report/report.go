// Ver 2026-07-08 07:40, by Fable 5

// Package report turns audit JSONL files (design doc §8.2) into aggregate
// statistics: a fine-grained JSON data table plus a human-readable Markdown
// rendering. It is coupled to the audit format — change one, change both.
package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"vmr/internal/audit"
)

// Format is bumped whenever the report JSON structure changes.
const Format = 1

// Report is the top-level JSON output. Grains: Rows = date × virtual model
// (roll up freely to model-only or date-only); Endpoints = date × upstream
// endpoint (availability and error view). Finer grains stay in the raw logs.
type Report struct {
	Meta      Meta          `json:"meta"`
	Rows      []Row         `json:"rows"`
	Endpoints []EndpointRow `json:"endpoints"`
}

type Meta struct {
	Format      int      `json:"format"`
	GeneratedAt string   `json:"generated_at"`
	Inputs      []string `json:"inputs"`
	Records     int      `json:"records"`
	ParseErrors int      `json:"parse_errors"`
	From        string   `json:"from,omitempty"` // earliest ts
	To          string   `json:"to,omitempty"`   // latest ts
}

// Row aggregates requests for one (date, virtual model) pair.
type Row struct {
	Date     string `json:"date"`
	Model    string `json:"model"`
	Protocol string `json:"protocol"`

	Requests int `json:"requests"`
	OK       int `json:"ok"`
	Errors   int `json:"errors"`
	Canceled int `json:"canceled"`
	Streams  int `json:"streams"`

	Attempts  int `json:"attempts"`  // upstream tries across all requests
	Fallbacks int `json:"fallbacks"` // requests that needed >1 attempt

	TokensIn    int64 `json:"tokens_in"` // summed over records with usage
	TokensOut   int64 `json:"tokens_out"`
	TokensKnown int   `json:"tokens_known"` // #records where usage was extractable

	BytesIn  int64 `json:"bytes_in"`  // client request body bytes (as recorded)
	BytesOut int64 `json:"bytes_out"` // client response body bytes (as recorded)

	DurMSSum int64 `json:"dur_ms_sum"`
	DurMSP50 int64 `json:"dur_ms_p50"`
	DurMSP95 int64 `json:"dur_ms_p95"`
	DurMSMax int64 `json:"dur_ms_max"`

	// Throughput: tokens_out per second over records with known usage;
	// bytes_out per second over all records with dur>0.
	TokOutPerSec   float64 `json:"tok_out_per_sec"`
	BytesOutPerSec float64 `json:"bytes_out_per_sec"`

	durs       []int64 // working state, not serialized
	tokDurMS   int64
	bytesDurMS int64
}

// EndpointRow aggregates upstream attempts for one (date, endpoint) pair.
type EndpointRow struct {
	Date     string `json:"date"`
	Endpoint string `json:"endpoint"` // provider/real-model

	Attempts     int            `json:"attempts"`
	OK           int            `json:"ok"`
	Failed       int            `json:"failed"`
	Availability float64        `json:"availability"` // OK/Attempts
	ErrorClasses map[string]int `json:"error_classes,omitempty"`

	DurMSP50 int64 `json:"dur_ms_p50"`
	DurMSP95 int64 `json:"dur_ms_p95"`

	durs []int64
}

// Build reads the given audit JSONL files and aggregates them.
func Build(paths []string, now time.Time) (*Report, error) {
	rep := &Report{Meta: Meta{Format: Format, GeneratedAt: now.Format(time.RFC3339), Inputs: paths}}
	rows := map[string]*Row{}
	eps := map[string]*EndpointRow{}
	var from, to time.Time

	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		// One line can hold several bodies, each capped at max_body_mb
		// (default 8MiB) — size the scanner generously.
		sc.Buffer(make([]byte, 1<<20), 128<<20)
		for sc.Scan() {
			var rec audit.Record
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				rep.Meta.ParseErrors++
				continue
			}
			rep.Meta.Records++
			if from.IsZero() || rec.TS.Before(from) {
				from = rec.TS
			}
			if rec.TS.After(to) {
				to = rec.TS
			}
			date := rec.TS.Format("2006-01-02")

			model := rec.Model
			if model == "" {
				model = "(rejected)" // failed before model parsing: auth/413/bad json
			}
			key := date + "\x00" + model
			row, ok := rows[key]
			if !ok {
				row = &Row{Date: date, Model: model, Protocol: rec.Protocol}
				rows[key] = row
			}
			addRecord(row, &rec)

			for _, a := range rec.Attempts {
				k := date + "\x00" + a.Endpoint
				ep, ok := eps[k]
				if !ok {
					ep = &EndpointRow{Date: date, Endpoint: a.Endpoint, ErrorClasses: map[string]int{}}
					eps[k] = ep
				}
				addAttempt(ep, &a)
			}
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	if !from.IsZero() {
		rep.Meta.From, rep.Meta.To = from.Format(time.RFC3339), to.Format(time.RFC3339)
	}
	for _, r := range rows {
		finishRow(r)
		rep.Rows = append(rep.Rows, *r)
	}
	for _, e := range eps {
		finishEndpoint(e)
		rep.Endpoints = append(rep.Endpoints, *e)
	}
	sort.Slice(rep.Rows, func(i, j int) bool {
		if rep.Rows[i].Date != rep.Rows[j].Date {
			return rep.Rows[i].Date < rep.Rows[j].Date
		}
		return rep.Rows[i].Model < rep.Rows[j].Model
	})
	sort.Slice(rep.Endpoints, func(i, j int) bool {
		if rep.Endpoints[i].Date != rep.Endpoints[j].Date {
			return rep.Endpoints[i].Date < rep.Endpoints[j].Date
		}
		return rep.Endpoints[i].Endpoint < rep.Endpoints[j].Endpoint
	})
	return rep, nil
}

func addRecord(row *Row, rec *audit.Record) {
	row.Requests++
	switch rec.Outcome {
	case "ok":
		row.OK++
	case "canceled":
		row.Canceled++
	default:
		row.Errors++
	}
	if rec.Stream {
		row.Streams++
	}
	row.Attempts += len(rec.Attempts)
	if len(rec.Attempts) > 1 {
		row.Fallbacks++
	}

	row.BytesIn += bodyBytes(rec.Client.Request.Body)
	if rec.Client.Response != nil {
		row.BytesOut += bodyBytes(rec.Client.Response.Body)
	}

	row.DurMSSum += rec.DurMS
	if rec.DurMS > row.DurMSMax {
		row.DurMSMax = rec.DurMS
	}
	row.durs = append(row.durs, rec.DurMS)
	if rec.DurMS > 0 {
		row.bytesDurMS += rec.DurMS
	}

	if rec.Client.Response != nil {
		if in, out, ok := ExtractUsage(rec.Client.Response.Body); ok {
			row.TokensIn += in
			row.TokensOut += out
			row.TokensKnown++
			if rec.DurMS > 0 {
				row.tokDurMS += rec.DurMS
			}
		}
	}
}

func addAttempt(ep *EndpointRow, a *audit.Attempt) {
	ep.Attempts++
	if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
		ep.OK++
	} else {
		ep.Failed++
		cls := a.Error
		if cls == "" {
			cls = "unknown"
		}
		// Detail-carrying errors ("network: dial tcp …", "truncated: …")
		// are bucketed by their prefix so the class table stays bounded.
		if i := strings.IndexByte(cls, ':'); i > 0 {
			cls = cls[:i]
		}
		ep.ErrorClasses[cls]++
	}
	ep.durs = append(ep.durs, a.DurMS)
}

func finishRow(r *Row) {
	r.DurMSP50, r.DurMSP95 = percentiles(r.durs)
	if r.tokDurMS > 0 {
		r.TokOutPerSec = round2(float64(r.TokensOut) / (float64(r.tokDurMS) / 1000))
	}
	if r.bytesDurMS > 0 {
		r.BytesOutPerSec = round2(float64(r.BytesOut) / (float64(r.bytesDurMS) / 1000))
	}
	r.durs = nil
}

func finishEndpoint(e *EndpointRow) {
	e.DurMSP50, e.DurMSP95 = percentiles(e.durs)
	if e.Attempts > 0 {
		e.Availability = round2(float64(e.OK) / float64(e.Attempts))
	}
	if len(e.ErrorClasses) == 0 {
		e.ErrorClasses = nil
	}
	e.durs = nil
}

// bodyBytes sizes a recorded body: JSON bodies by re-serialization, string
// bodies (SSE etc.) by length. Truncated bodies undercount; that matches
// what was recorded.
func bodyBytes(body any) int64 {
	switch b := body.(type) {
	case nil:
		return 0
	case string:
		return int64(len(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return 0
		}
		return int64(len(raw))
	}
}

// percentiles returns nearest-rank p50 and p95.
func percentiles(durs []int64) (p50, p95 int64) {
	if len(durs) == 0 {
		return 0, 0
	}
	s := append([]int64(nil), durs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	rank := func(p float64) int64 {
		i := int(p*float64(len(s))+0.5) - 1
		if i < 0 {
			i = 0
		}
		if i >= len(s) {
			i = len(s) - 1
		}
		return s[i]
	}
	return rank(0.50), rank(0.95)
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}
