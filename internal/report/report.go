// Ver 2026-07-13 22:45, by Sonnet 5

// Package report turns audit JSONL files (design doc §9.2) into aggregate
// statistics: a fine-grained JSON data table plus a human-readable Markdown
// rendering. It is coupled to the audit format — change one, change both.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"vmr/internal/audit"
)

// Format is bumped whenever the report JSON structure changes.
// 2: rows are grained date × protocol × model (a virtual-model name may
// exist in both protocol groups; merging them mixed two real models).
// 3: tokens_in now includes Anthropic's cache_read/cache_creation tokens
// (previously excluded, since Anthropic reports them as separate counters
// from input_tokens) and gains tokens_in_cached/tokens_in_cache_write.
// 4: rows gain request-shape stats (messages/messages_known/role_chars)
// and first-token latency (ttft_ms_sum/ttft_known, from the audit
// record's ttft_ms field — absent in logs written before it existed).
// 5: adds the session-analysis sections — tools[] (per request-shape tool
// declaration vs. actual per-turn use) and sessions[] (agent-session
// summaries) — attached from AnalyzeSessions by the report command.
// 6: rows gain finish_reasons/truncated/tokens_reasoning and TTFT
// percentiles; adds hours[] (date × hour activity) and workloads[]
// (traffic split by workload class: interactive vs. scheduled scaffolding).
// 7: each report section now has its own pre-aggregated bucket — Overall,
// ByModel, ByDate — so every table's p50/p95 is computed from raw values
// inside its own bucket rather than approximated across heterogeneous
// rows. JSON gains overall/by_model/by_date keys and an expanded set
// of fields on Endpoints/Hours/Workloads/Sessions.
// 8: rows/endpoints/hours/sessions/workloads gain images/images_compressed
// (from the audit record's images[], itself new); hours gains
// fallbacks/truncated (bringing it in line with Row/SessionRow); workloads
// gains requests_with_tool_calls; endpoints' error_classes is now sourced
// from each attempt's typed error_class field instead of an error-string
// prefix split.
// 9: adds endpoints_all (endpoint, all dates merged) and hours_of_day (local
// hour 0-23, all dates merged) — each a genuinely independent bucket with
// its own raw dur_ms/ttft_ms values, not a re-aggregation of the per-date
// Endpoints/Hours buckets (those buckets free their raw-value slices right
// after computing their own per-date percentiles, so a cross-date merge
// derived from them has nothing left to compute a true percentile from).
// The Markdown 端点可用度 and 每小时活跃度 tables read these directly.
const Format = 9

// Report is the top-level JSON output. Grains: Rows = date × protocol ×
// virtual model (finest); Overall = all records (single bucket);
// ByModel = grouped by (model, protocol); ByDate = grouped by date;
// Endpoints = date × upstream endpoint (availability and error view);
// Hours = date × local hour (activity profile). Each bucket holds its own
// raw dur_ms/ttft_ms values and computes p50/p95 independently, so every
// table in the Markdown output has true (non-approximated) percentiles.
// Finer grains stay in the raw logs and per-request detail files.
type Report struct {
	Meta Meta `json:"meta"`

	// Pre-aggregated buckets (each computes its own p50/p95 from raw values).
	Rows    []Row `json:"rows"`     // grain = date × protocol × model
	Overall Row   `json:"overall"`  // single bucket for all records
	ByModel []Row `json:"by_model"` // grouped by (model, protocol)
	ByDate  []Row `json:"by_date"`  // grouped by date

	Endpoints    []EndpointRow `json:"endpoints"`
	EndpointsAll []EndpointRow `json:"endpoints_all,omitempty"` // grain = endpoint only, all dates merged; true p50/p95 (see Format 9)
	Hours        []HourRow     `json:"hours,omitempty"`
	HoursOfDay   []HourRow     `json:"hours_of_day,omitempty"` // grain = local hour only (0-23), all dates merged; true p50/p95 (see Format 9)
	// Tools, Sessions and Workloads come from the session analysis
	// (session.go) and are attached by the report command after Build;
	// empty when analysis is off.
	Tools     []ToolShapeRow `json:"tools,omitempty"`
	Sessions  []SessionRow   `json:"sessions,omitempty"`
	Workloads []WorkloadRow  `json:"workloads,omitempty"`
}

// HourRow aggregates requests for one (date, local hour) pair — the
// activity profile for spotting load peaks and planning around provider
// rate limits.
type HourRow struct {
	Date     string `json:"date"`
	Hour     int    `json:"hour"` // 0-23, record's local timezone
	Requests int    `json:"requests"`
	OK       int    `json:"ok"`
	Errors   int    `json:"errors"`

	TokensIn           int64 `json:"tokens_in"`
	TokensInCached     int64 `json:"tokens_in_cached"`
	TokensInCacheWrite int64 `json:"tokens_in_cache_write,omitempty"`
	TokensOut          int64 `json:"tokens_out"`
	TokensKnown        int   `json:"tokens_known,omitempty"`

	BytesIn  int64 `json:"bytes_in,omitempty"`
	BytesOut int64 `json:"bytes_out"`

	Messages      int64            `json:"messages,omitempty"`
	MessagesKnown int              `json:"messages_known,omitempty"`
	RoleChars     map[string]int64 `json:"role_chars,omitempty"`

	// First-token latency (true p50/p95 from this hour's raw values).
	TTFTMSSum int64 `json:"ttft_ms_sum,omitempty"`
	TTFTKnown int   `json:"ttft_known,omitempty"`
	TTFTMSP50 int64 `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95 int64 `json:"ttft_ms_p95,omitempty"`

	// Request duration (true p50/p95 from this hour's raw values).
	DurMSSum int64 `json:"dur_ms_sum,omitempty"`
	DurMSP50 int64 `json:"dur_ms_p50,omitempty"`
	DurMSP95 int64 `json:"dur_ms_p95,omitempty"`
	DurMSMax int64 `json:"dur_ms_max,omitempty"`

	Attempts        int     `json:"attempts,omitempty"`
	Fallbacks       int     `json:"fallbacks,omitempty"` // requests that needed >1 attempt
	Truncated       int     `json:"truncated,omitempty"` // ok-outcome requests whose stream broke mid-flight
	RequestsWithDur int     `json:"requests_with_dur,omitempty"`
	TokOutPerSec    float64 `json:"tok_out_per_sec,omitempty"`

	Images           int `json:"images,omitempty"`            // inline request images detected
	ImagesCompressed int `json:"images_compressed,omitempty"` // subset that triggered downscaling

	// Work state (not serialized; cleared by finishHour).
	hoursDurs  []int64
	hoursTTFTs []int64
	tokDurMS   int64
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

// Row aggregates requests for one (date, protocol, virtual model) triple.
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

	TokensIn    int64 `json:"tokens_in"` // summed over records with usage; includes cached tokens
	TokensOut   int64 `json:"tokens_out"`
	TokensKnown int   `json:"tokens_known"` // #records where usage was extractable

	// TokensIn split by cache: TokensInCached is the cache-hit portion (Anthropic
	// cache_read_input_tokens / OpenAI prompt_tokens_details.cached_tokens /
	// DeepSeek prompt_cache_hit_tokens); TokensInCacheWrite is Anthropic-only
	// (cache_creation_input_tokens, billed at a premium, not a hit). Both are
	// subsets already counted in TokensIn — fresh tokens = TokensIn - the two.
	TokensInCached     int64 `json:"tokens_in_cached"`
	TokensInCacheWrite int64 `json:"tokens_in_cache_write"`

	BytesIn  int64 `json:"bytes_in"`  // client request body bytes (as recorded)
	BytesOut int64 `json:"bytes_out"` // client response body bytes (as recorded)

	// Request-shape stats over records whose request body parsed as a chat
	// object (MessagesKnown counts them): total message count and displayed
	// characters per role. Anthropic tool_result parts count as "tool" to
	// stay comparable with openai's dedicated tool role.
	Messages      int64            `json:"messages"`
	MessagesKnown int              `json:"messages_known"`
	RoleChars     map[string]int64 `json:"role_chars,omitempty"`

	// First-token latency (client view: request arrival → first response
	// body byte), summed over the TTFTKnown records that carry ttft_ms —
	// logs written before the field existed contribute nothing.
	TTFTMSSum int64 `json:"ttft_ms_sum"`
	TTFTKnown int   `json:"ttft_known"`
	TTFTMSP50 int64 `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95 int64 `json:"ttft_ms_p95,omitempty"`

	// FinishReasons distributes records by the response's finish_reason /
	// stop_reason ("length" = output truncated by the token cap — a health
	// signal); records without one (errors, canceled) count under "".
	FinishReasons map[string]int `json:"finish_reasons,omitempty"`
	// Truncated counts ok-outcome records whose stream broke mid-flight
	// (attempt error "truncated: …"): the client got a 2xx but incomplete
	// bytes — invisible in OK/Errors, so it gets its own counter.
	Truncated int `json:"truncated,omitempty"`
	// TokensReasoning sums the thinking-token portion of TokensOut where the
	// provider reports it (a subset of TokensOut, not additive).
	TokensReasoning int64 `json:"tokens_reasoning,omitempty"`

	DurMSSum int64 `json:"dur_ms_sum"`
	DurMSP50 int64 `json:"dur_ms_p50"`
	DurMSP95 int64 `json:"dur_ms_p95"`
	DurMSMax int64 `json:"dur_ms_max"`
	// RequestsWithDur counts records with dur_ms > 0 (used to flag when the
	// percentile basis is much smaller than Requests — e.g. a row of mostly
	// rejects).
	RequestsWithDur int `json:"requests_with_dur,omitempty"`

	// Throughput: tokens_out per second over records with known usage;
	// bytes_out per second over all records with dur>0.
	TokOutPerSec   float64 `json:"tok_out_per_sec"`
	BytesOutPerSec float64 `json:"bytes_out_per_sec"`

	Images           int `json:"images,omitempty"`            // inline request images detected
	ImagesCompressed int `json:"images_compressed,omitempty"` // subset that triggered downscaling

	durs       []int64 // working state, not serialized
	ttfts      []int64
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

	TokensIn           int64 `json:"tokens_in,omitempty"`
	TokensInCached     int64 `json:"tokens_in_cached,omitempty"`
	TokensInCacheWrite int64 `json:"tokens_in_cache_write,omitempty"`
	TokensOut          int64 `json:"tokens_out,omitempty"`
	TokensKnown        int   `json:"tokens_known,omitempty"`

	BytesIn  int64 `json:"bytes_in,omitempty"`
	BytesOut int64 `json:"bytes_out,omitempty"`

	TTFTMSSum int64 `json:"ttft_ms_sum,omitempty"`
	TTFTKnown int   `json:"ttft_known,omitempty"`
	TTFTMSP50 int64 `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95 int64 `json:"ttft_ms_p95,omitempty"`

	DurMSSum        int64 `json:"dur_ms_sum,omitempty"`
	DurMSP50        int64 `json:"dur_ms_p50"`
	DurMSP95        int64 `json:"dur_ms_p95"`
	RequestsWithDur int   `json:"requests_with_dur,omitempty"`

	TokOutPerSec float64 `json:"tok_out_per_sec,omitempty"`

	Images           int `json:"images,omitempty"`            // inline request images detected
	ImagesCompressed int `json:"images_compressed,omitempty"` // subset that triggered downscaling

	durs  []int64 // working state, not serialized
	ttfts []int64
}

// Build reads the given audit JSONL files and aggregates them. Each record
// is pushed to every relevant bucket in one pass:
//
//   - Rows:    date × protocol × model (finest grain)
//   - Overall: one bucket aggregating everything
//   - ByModel: keyed by (model, protocol)
//   - ByDate:  keyed by date
//   - Hours:   keyed by (date, local hour)
//   - Endpoints: keyed by (date, endpoint)
//
// Each bucket holds its own raw dur_ms / ttft_ms values and computes p50/p95
// independently in finishRow / finishHour / finishEndpoint — no cross-bucket
// aggregation is needed downstream, so every Markdown table has true
// percentiles rather than approximated ones.
//
// Pass nil for progress to silence per-file progress lines; pass os.Stdout
// (or any io.Writer) to print "[i/N] <path>  done: N records, M parse errors (Ts)"
// lines as each file finishes.
func Build(paths []string, now time.Time, progress io.Writer) (*Report, error) {
	return buildWithProgress(paths, now, progress)
}

// buildWithProgress is the worker behind Build; passing a non-nil progress
// writer turns on per-file progress lines (file-level only — no record-level
// noise, no ETA; the slowest file is usually the only one worth noticing).
func buildWithProgress(paths []string, now time.Time, progress io.Writer) (*Report, error) {
	rep := &Report{Meta: Meta{Format: Format, GeneratedAt: now.Format(time.RFC3339), Inputs: paths}}
	rows := map[string]*Row{}
	byModel := map[string]*Row{}
	byDate := map[string]*Row{}
	eps := map[string]*EndpointRow{}
	epsAll := map[string]*EndpointRow{}
	hours := map[string]*HourRow{}
	hoursOfDay := map[int]*HourRow{}
	var from, to time.Time

	for fileIdx, path := range paths {
		fileStart := time.Now()
		var fileRecords, fileErrors int
		rc, err := audit.OpenLogFile(path)
		if err != nil {
			return nil, err
		}
		scanErr := audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
			var rec audit.Record
			if err := json.Unmarshal(lineBytes, &rec); err != nil {
				rep.Meta.ParseErrors++
				fileErrors++
				return
			}
			rep.Meta.Records++
			fileRecords++
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
			var usage Usage
			usageOK := false
			if rec.Client.Response != nil {
				usage, usageOK = ExtractUsage(rec.Client.Response.Body)
			}

			// 1. Rows: date × protocol × model (finest grain)
			key := date + "\x00" + rec.Protocol + "\x00" + model
			row, ok := rows[key]
			if !ok {
				row = &Row{Date: date, Model: model, Protocol: rec.Protocol}
				rows[key] = row
			}
			addRecord(row, &rec, usage, usageOK)

			// 2. Overall: every record contributes (single bucket, no key).
			addRecord(&rep.Overall, &rec, usage, usageOK)

			// 3. ByModel: keyed by (model, protocol).
			mKey := model + "\x00" + rec.Protocol
			mr, ok := byModel[mKey]
			if !ok {
				mr = &Row{Model: model, Protocol: rec.Protocol}
				byModel[mKey] = mr
			}
			addRecord(mr, &rec, usage, usageOK)

			// 4. ByDate: keyed by date only (protocol merged).
			dr, ok := byDate[date]
			if !ok {
				dr = &Row{Date: date}
				byDate[date] = dr
			}
			addRecord(dr, &rec, usage, usageOK)

			// 5. HourRow: (date, local hour), plus an hour-of-day-only bucket
			// (all dates merged) computed independently — NOT derived from
			// the per-date buckets, whose raw dur/ttft slices get freed by
			// finishHour before the Markdown render pass ever sees them.
			hour := rec.TS.Hour()
			hk := fmt.Sprintf("%s\x00%02d", date, hour)
			hr, ok := hours[hk]
			if !ok {
				hr = &HourRow{Date: date, Hour: hour}
				hours[hk] = hr
			}
			addHour(hr, &rec, usage, usageOK)
			hod, ok := hoursOfDay[hour]
			if !ok {
				hod = &HourRow{Hour: hour}
				hoursOfDay[hour] = hod
			}
			addHour(hod, &rec, usage, usageOK)

			// 6. EndpointRow: one per (date, endpoint) from the attempts loop,
			// plus an endpoint-only bucket (all dates merged) computed
			// independently for the same reason as hoursOfDay above.
			// Request-level metrics (bytes / tokens / ttft / dur_ms) attach to
			// the last successful attempt's endpoint — that's the one whose
			// bytes the client actually received.
			var successEp string
			for _, a := range rec.Attempts {
				if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
					successEp = a.Endpoint
				}
			}
			for _, a := range rec.Attempts {
				k := date + "\x00" + a.Endpoint
				ep, ok := eps[k]
				if !ok {
					ep = &EndpointRow{Date: date, Endpoint: a.Endpoint, ErrorClasses: map[string]int{}}
					eps[k] = ep
				}
				addAttempt(ep, &a)

				epAll, ok := epsAll[a.Endpoint]
				if !ok {
					epAll = &EndpointRow{Endpoint: a.Endpoint, ErrorClasses: map[string]int{}}
					epsAll[a.Endpoint] = epAll
				}
				addAttempt(epAll, &a)

				if a.Endpoint == successEp {
					addEndpointRequest(ep, &rec, usage, usageOK)
					addEndpointRequest(epAll, &rec, usage, usageOK)
				}
			}
		}, func() { // oversized line: skipped with bounded memory, counted as a parse error
			rep.Meta.ParseErrors++
			fileErrors++
		})
		rc.Close()
		if scanErr != nil {
			return nil, fmt.Errorf("%s: %w", path, scanErr)
		}
		if progress != nil {
			fmt.Fprintf(progress, "[%d/%d] %s  done: %d records, %d parse errors (%s)\n",
				fileIdx+1, len(paths), path, fileRecords, fileErrors, time.Since(fileStart).Round(time.Millisecond))
		}
	}

	if !from.IsZero() {
		rep.Meta.From, rep.Meta.To = from.Format(time.RFC3339), to.Format(time.RFC3339)
	}
	finishRow(&rep.Overall)
	for _, r := range rows {
		finishRow(r)
		rep.Rows = append(rep.Rows, *r)
	}
	for _, r := range byModel {
		finishRow(r)
		rep.ByModel = append(rep.ByModel, *r)
	}
	for _, r := range byDate {
		finishRow(r)
		rep.ByDate = append(rep.ByDate, *r)
	}
	for _, e := range eps {
		finishEndpoint(e)
		rep.Endpoints = append(rep.Endpoints, *e)
	}
	for _, e := range epsAll {
		finishEndpoint(e)
		rep.EndpointsAll = append(rep.EndpointsAll, *e)
	}
	for _, h := range hours {
		finishHour(h)
		rep.Hours = append(rep.Hours, *h)
	}
	for _, h := range hoursOfDay {
		finishHour(h)
		rep.HoursOfDay = append(rep.HoursOfDay, *h)
	}
	sort.Slice(rep.Hours, func(i, j int) bool {
		if rep.Hours[i].Date != rep.Hours[j].Date {
			return rep.Hours[i].Date < rep.Hours[j].Date
		}
		return rep.Hours[i].Hour < rep.Hours[j].Hour
	})
	sort.Slice(rep.HoursOfDay, func(i, j int) bool { return rep.HoursOfDay[i].Hour < rep.HoursOfDay[j].Hour })
	// EndpointsAll: busiest first, matching the per-date Endpoints sort intent.
	sort.Slice(rep.EndpointsAll, func(i, j int) bool { return rep.EndpointsAll[i].Attempts > rep.EndpointsAll[j].Attempts })
	sort.Slice(rep.Rows, func(i, j int) bool {
		a, b := rep.Rows[i], rep.Rows[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Protocol < b.Protocol
	})
	sort.Slice(rep.Endpoints, func(i, j int) bool {
		if rep.Endpoints[i].Date != rep.Endpoints[j].Date {
			return rep.Endpoints[i].Date < rep.Endpoints[j].Date
		}
		return rep.Endpoints[i].Endpoint < rep.Endpoints[j].Endpoint
	})
	// ByModel: largest first (most-used model on top).
	sort.Slice(rep.ByModel, func(i, j int) bool {
		if rep.ByModel[i].Requests != rep.ByModel[j].Requests {
			return rep.ByModel[i].Requests > rep.ByModel[j].Requests
		}
		if rep.ByModel[i].Model != rep.ByModel[j].Model {
			return rep.ByModel[i].Model < rep.ByModel[j].Model
		}
		return rep.ByModel[i].Protocol < rep.ByModel[j].Protocol
	})
	// ByDate: chronological (oldest first).
	sort.Slice(rep.ByDate, func(i, j int) bool {
		return rep.ByDate[i].Date < rep.ByDate[j].Date
	})
	return rep, nil
}

func addRecord(row *Row, rec *audit.Record, usage Usage, usageOK bool) {
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
	n, compressed := countImages(rec.Images)
	row.Images += n
	row.ImagesCompressed += compressed

	row.BytesIn += bodyBytes(rec.Client.Request.Body)
	if rec.Client.Response != nil {
		row.BytesOut += bodyBytes(rec.Client.Response.Body)
	}

	if n, ok := messageCount(rec.Client.Request.Body); ok {
		row.Messages += int64(n)
		row.MessagesKnown++
		for role, c := range roleChars(rec.Client.Request.Body) {
			if row.RoleChars == nil {
				row.RoleChars = map[string]int64{}
			}
			row.RoleChars[role] += c
		}
	}
	if rec.TTFTMS > 0 {
		row.TTFTMSSum += rec.TTFTMS
		row.TTFTKnown++
		row.ttfts = append(row.ttfts, rec.TTFTMS)
	}

	if row.FinishReasons == nil {
		row.FinishReasons = map[string]int{}
	}
	var finish string
	if rec.Client.Response != nil {
		finish = extractFinish(rec.Client.Response.Body)
	}
	row.FinishReasons[finish]++
	if rec.Outcome == "ok" {
		for _, a := range rec.Attempts {
			if attemptErrorClass(a) == "truncated" {
				row.Truncated++
				break
			}
		}
	}

	row.DurMSSum += rec.DurMS
	if rec.DurMS > row.DurMSMax {
		row.DurMSMax = rec.DurMS
	}
	row.durs = append(row.durs, rec.DurMS)
	if rec.DurMS > 0 {
		row.bytesDurMS += rec.DurMS
		row.RequestsWithDur++
	}

	if usageOK {
		row.TokensIn += usage.In
		row.TokensOut += usage.Out
		row.TokensInCached += usage.CacheRead
		row.TokensInCacheWrite += usage.CacheWrite
		row.TokensReasoning += usage.Reasoning
		row.TokensKnown++
		if rec.DurMS > 0 {
			row.tokDurMS += rec.DurMS
		}
	}
}

func addHour(h *HourRow, rec *audit.Record, usage Usage, usageOK bool) {
	h.Requests++
	switch rec.Outcome {
	case "ok":
		h.OK++
	case "canceled":
	default:
		h.Errors++
	}
	h.Attempts += len(rec.Attempts)
	if len(rec.Attempts) > 1 {
		h.Fallbacks++
	}
	if rec.Outcome == "ok" {
		for _, a := range rec.Attempts {
			if attemptErrorClass(a) == "truncated" {
				h.Truncated++
				break
			}
		}
	}
	n, compressed := countImages(rec.Images)
	h.Images += n
	h.ImagesCompressed += compressed
	h.BytesIn += bodyBytes(rec.Client.Request.Body)
	if rec.Client.Response != nil {
		h.BytesOut += bodyBytes(rec.Client.Response.Body)
	}
	if n, ok := messageCount(rec.Client.Request.Body); ok {
		h.Messages += int64(n)
		h.MessagesKnown++
		for role, c := range roleChars(rec.Client.Request.Body) {
			if h.RoleChars == nil {
				h.RoleChars = map[string]int64{}
			}
			h.RoleChars[role] += c
		}
	}
	if rec.TTFTMS > 0 {
		h.TTFTMSSum += rec.TTFTMS
		h.TTFTKnown++
		h.hoursTTFTs = append(h.hoursTTFTs, rec.TTFTMS)
	}
	h.DurMSSum += rec.DurMS
	if rec.DurMS > h.DurMSMax {
		h.DurMSMax = rec.DurMS
	}
	if rec.DurMS > 0 {
		h.RequestsWithDur++
		h.hoursDurs = append(h.hoursDurs, rec.DurMS)
	}
	if usageOK {
		h.TokensIn += usage.In
		h.TokensInCached += usage.CacheRead
		h.TokensInCacheWrite += usage.CacheWrite
		h.TokensOut += usage.Out
		h.TokensKnown++
		if rec.DurMS > 0 {
			h.tokDurMS += rec.DurMS
		}
	}
}

// addEndpointRequest attaches request-level metrics (bytes / tokens /
// ttft / dur_ms) to the endpoint that actually served the client — i.e.
// the successful attempt's endpoint. This is what the client experienced.
// Called once per request, only on the successful attempt's row.
func addEndpointRequest(ep *EndpointRow, rec *audit.Record, usage Usage, usageOK bool) {
	n, compressed := countImages(rec.Images)
	ep.Images += n
	ep.ImagesCompressed += compressed
	ep.BytesIn += bodyBytes(rec.Client.Request.Body)
	if rec.Client.Response != nil {
		ep.BytesOut += bodyBytes(rec.Client.Response.Body)
	}
	if usageOK {
		ep.TokensIn += usage.In
		ep.TokensInCached += usage.CacheRead
		ep.TokensInCacheWrite += usage.CacheWrite
		ep.TokensOut += usage.Out
		ep.TokensKnown++
	}
	if rec.TTFTMS > 0 {
		ep.TTFTMSSum += rec.TTFTMS
		ep.TTFTKnown++
		ep.ttfts = append(ep.ttfts, rec.TTFTMS)
	}
	if rec.DurMS > 0 {
		ep.DurMSSum += rec.DurMS
		ep.RequestsWithDur++
		ep.durs = append(ep.durs, rec.DurMS)
	}
}

func addAttempt(ep *EndpointRow, a *audit.Attempt) {
	ep.Attempts++
	if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
		ep.OK++
	} else {
		ep.Failed++
		cls := attemptErrorClass(*a)
		if cls == "" {
			cls = "unknown"
		}
		ep.ErrorClasses[cls]++
	}
}

// attemptErrorClass returns the attempt's structured error class, falling
// back to parsing the free-text Error field for logs written before
// ErrorClass existed: HTTP-classified failures stored the bare class name
// (no colon) directly in Error, and the four non-HTTP failure paths
// (build/network/canceled/truncated) used a "class: detail" prefix — both
// forms are still exactly recoverable from Error alone. New logs always
// carry ErrorClass and never touch this fallback.
func attemptErrorClass(a audit.Attempt) string {
	if a.ErrorClass != "" {
		return a.ErrorClass
	}
	if a.Error == "" {
		return ""
	}
	if i := strings.IndexByte(a.Error, ':'); i > 0 {
		return a.Error[:i]
	}
	return a.Error
}

// finishHour computes true p50/p95 for this hour bucket from its own raw
// values, then frees the working slices.
func finishHour(h *HourRow) {
	h.DurMSP50, h.DurMSP95 = percentiles(h.hoursDurs)
	h.TTFTMSP50, h.TTFTMSP95 = percentiles(h.hoursTTFTs)
	if h.tokDurMS > 0 {
		h.TokOutPerSec = round2(float64(h.TokensOut) / (float64(h.tokDurMS) / 1000))
	}
	if len(h.RoleChars) == 0 {
		h.RoleChars = nil
	}
	h.hoursDurs, h.hoursTTFTs = nil, nil
}

func finishRow(r *Row) {
	r.DurMSP50, r.DurMSP95 = percentiles(r.durs)
	r.TTFTMSP50, r.TTFTMSP95 = percentiles(r.ttfts)
	if r.tokDurMS > 0 {
		r.TokOutPerSec = round2(float64(r.TokensOut) / (float64(r.tokDurMS) / 1000))
	}
	if r.bytesDurMS > 0 {
		r.BytesOutPerSec = round2(float64(r.BytesOut) / (float64(r.bytesDurMS) / 1000))
	}
	r.durs, r.ttfts = nil, nil
}

func finishEndpoint(e *EndpointRow) {
	e.DurMSP50, e.DurMSP95 = percentiles(e.durs)
	e.TTFTMSP50, e.TTFTMSP95 = percentiles(e.ttfts)
	if e.Attempts > 0 {
		e.Availability = round2(float64(e.OK) / float64(e.Attempts))
	}
	if e.DurMSSum > 0 {
		e.TokOutPerSec = round2(float64(e.TokensOut) / (float64(e.DurMSSum) / 1000))
	}
	if len(e.ErrorClasses) == 0 {
		e.ErrorClasses = nil
	}
	e.durs, e.ttfts = nil, nil
}

// countImages tallies a record's inline request images and the subset that
// triggered downscaling.
func countImages(images []audit.ImageInfo) (total, compressed int) {
	total = len(images)
	for _, img := range images {
		if img.Downscaled {
			compressed++
		}
	}
	return total, compressed
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
