// Ver 2026-07-12 01:40, by Fable 5

// Structured exports derived from the session analysis: the per-request
// feature file (vmr-requests.jsonl — the raw material for ad-hoc statistics
// with jq/DuckDB/pandas) and the tools/sessions aggregate sections attached
// to the report JSON.
package report

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"time"
)

// ToolShapeRow aggregates tool declaration vs. actual use for one request
// shape (a distinct declared-tool set). "Actual use" counts only each
// request's own turn — extracted from the response, so tool calls repeated
// through resent history are never double-counted.
type ToolShapeRow struct {
	Shape         string         `json:"shape"`
	Requests      int            `json:"requests"`
	Declared      []string       `json:"declared"`
	Calls         map[string]int `json:"calls,omitempty"`
	NeverCalled   []string       `json:"never_called,omitempty"`
	DeclaredBytes int64          `json:"declared_bytes"` // per-request cost of the tools JSON
}

// SessionRow is the light per-session summary in the report JSON.
type SessionRow struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	ChatID        string `json:"chat_id,omitempty"`
	ContinuedFrom string `json:"continued_from,omitempty"`
	// Class is the session's workload class (see workloadClass): scheduled
	// scaffolding ("heartbeat"/"dream_diary") vs. "interactive" work.
	Class    string `json:"class,omitempty"`
	Link     string `json:"link,omitempty"` // relative path to the first request's detail file
	Requests int    `json:"requests"`
	Tasks    int    `json:"tasks"`
	From     string `json:"from"`
	To       string `json:"to"`

	TokensIn           int64 `json:"tokens_in"`
	TokensInCached     int64 `json:"tokens_in_cached,omitempty"`
	TokensInCacheWrite int64 `json:"tokens_in_cache_write,omitempty"`
	TokensOut          int64 `json:"tokens_out"`
	TokensKnown        int   `json:"tokens_known,omitempty"`

	BytesIn  int64 `json:"bytes_in,omitempty"`
	BytesOut int64 `json:"bytes_out,omitempty"`

	Messages      int64            `json:"messages,omitempty"`
	MessagesKnown int              `json:"messages_known,omitempty"`
	RoleChars     map[string]int64 `json:"role_chars,omitempty"`

	TTFTMSSum int64 `json:"ttft_ms_sum,omitempty"`
	TTFTKnown int   `json:"ttft_known,omitempty"`
	TTFTMSP50 int64 `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95 int64 `json:"ttft_ms_p95,omitempty"`

	DurMSSum int64 `json:"dur_ms_sum"`
	DurMSP50 int64 `json:"dur_ms_p50,omitempty"`
	DurMSP95 int64 `json:"dur_ms_p95,omitempty"`
	DurMSMax int64 `json:"dur_ms_max,omitempty"`

	Attempts        int     `json:"attempts,omitempty"`
	Fallbacks       int     `json:"fallbacks,omitempty"`
	OK              int     `json:"ok,omitempty"`
	Errors          int     `json:"errors,omitempty"`
	Canceled        int     `json:"canceled,omitempty"`
	RequestsWithDur int     `json:"requests_with_dur,omitempty"`
	TokOutPerSec    float64 `json:"tok_out_per_sec,omitempty"`
	Truncated       int     `json:"truncated,omitempty"`

	Images           int `json:"images,omitempty"`            // inline request images detected
	ImagesCompressed int `json:"images_compressed,omitempty"` // subset that triggered downscaling
}

// WorkloadRow splits traffic by workload class, answering "how much of the
// bill is real work vs. scheduled scaffolding" — heartbeats and diary crons
// resend a full system prompt every fire, which is invisible in per-model
// totals but obvious here.
type WorkloadRow struct {
	Class    string `json:"class"` // interactive | heartbeat | dream_diary | compaction
	Requests int    `json:"requests"`

	TokensIn           int64 `json:"tokens_in"`
	TokensInCached     int64 `json:"tokens_in_cached"`
	TokensInCacheWrite int64 `json:"tokens_in_cache_write,omitempty"`
	TokensOut          int64 `json:"tokens_out"`
	TokensKnown        int   `json:"tokens_known,omitempty"`

	BytesIn  int64 `json:"bytes_in,omitempty"`
	BytesOut int64 `json:"bytes_out,omitempty"`

	Messages      int64            `json:"messages,omitempty"`
	MessagesKnown int              `json:"messages_known,omitempty"`
	RoleChars     map[string]int64 `json:"role_chars,omitempty"`

	TTFTMSSum int64 `json:"ttft_ms_sum,omitempty"`
	TTFTKnown int   `json:"ttft_known,omitempty"`
	TTFTMSP50 int64 `json:"ttft_ms_p50,omitempty"`
	TTFTMSP95 int64 `json:"ttft_ms_p95,omitempty"`

	DurMSSum        int64   `json:"dur_ms_sum"`
	DurMSP50        int64   `json:"dur_ms_p50,omitempty"`
	DurMSP95        int64   `json:"dur_ms_p95,omitempty"`
	DurMSMax        int64   `json:"dur_ms_max,omitempty"`
	Attempts        int     `json:"attempts,omitempty"`
	RequestsWithDur int     `json:"requests_with_dur,omitempty"`
	TokOutPerSec    float64 `json:"tok_out_per_sec,omitempty"`

	Images           int `json:"images,omitempty"`            // inline request images detected
	ImagesCompressed int `json:"images_compressed,omitempty"` // subset that triggered downscaling

	ToolCalls             int `json:"tool_calls"`
	RequestsWithToolCalls int `json:"requests_with_tool_calls,omitempty"`

	// Working state (not serialized; cleared in Workloads after percentiles).
	dursDur  []int64
	dursTTFT []int64
}

// workloadClass buckets one request by its tags. Compaction wins over
// everything (it is never user work); scheduled templates next; the rest is
// interactive agent traffic.
func workloadClass(r *ReqInfo) string {
	switch {
	case r.Compaction:
		return "compaction"
	case hasTag(r, "heartbeat"):
		return "heartbeat"
	case hasTag(r, "dream_diary"):
		return "dream_diary"
	default:
		return "interactive"
	}
}

// Workloads aggregates the analysis by workload class, largest token bill
// first.
func (a *SessionAnalysis) Workloads() []WorkloadRow {
	byClass := map[string]*WorkloadRow{}
	for _, r := range a.Recs {
		cls := workloadClass(r)
		row := byClass[cls]
		if row == nil {
			row = &WorkloadRow{Class: cls}
			byClass[cls] = row
		}
		row.Requests++
		row.TokensIn += r.Usage.In
		row.TokensInCached += r.Usage.CacheRead
		row.TokensInCacheWrite += r.Usage.CacheWrite
		row.TokensOut += r.Usage.Out
		row.TokensKnown++
		row.BytesIn += r.bytesIn
		row.BytesOut += r.bytesOut
		if n := r.Msgs; n > 0 {
			row.Messages += int64(n)
			row.MessagesKnown++
		}
		for role, c := range r.RoleChars {
			if row.RoleChars == nil {
				row.RoleChars = map[string]int64{}
			}
			row.RoleChars[role] += c
		}
		if r.ttftMS > 0 {
			row.TTFTMSSum += r.ttftMS
			row.TTFTKnown++
			row.dursTTFT = append(row.dursTTFT, r.ttftMS)
		}
		row.DurMSSum += r.durMS
		if r.durMS > row.DurMSMax {
			row.DurMSMax = r.durMS
		}
		if r.durMS > 0 {
			row.RequestsWithDur++
			row.dursDur = append(row.dursDur, r.durMS)
		}
		row.Attempts += r.attempts
		row.ToolCalls += len(r.ToolCalls)
		if len(r.ToolCalls) > 0 {
			row.RequestsWithToolCalls++
		}
		row.Images += r.Images
		row.ImagesCompressed += r.ImagesCompressed
	}
	for _, row := range byClass {
		row.DurMSP50, row.DurMSP95 = percentiles(row.dursDur)
		row.TTFTMSP50, row.TTFTMSP95 = percentiles(row.dursTTFT)
		if row.DurMSSum > 0 {
			row.TokOutPerSec = round2(float64(row.TokensOut) / (float64(row.DurMSSum) / 1000))
		}
		if len(row.RoleChars) == 0 {
			row.RoleChars = nil
		}
		row.dursDur, row.dursTTFT = nil, nil
	}
	out := make([]WorkloadRow, 0, len(byClass))
	for _, row := range byClass {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TokensIn > out[j].TokensIn })
	return out
}

// ToolShapes aggregates the analysis into per-shape tool usage rows,
// most-used shape first. Shapeless records (no tools field) are skipped.
func (a *SessionAnalysis) ToolShapes() []ToolShapeRow {
	byShape := map[string]*ToolShapeRow{}
	for _, r := range a.Recs {
		if r.ToolsSig == "" && len(r.ToolCalls) == 0 {
			continue
		}
		sig := r.ToolsSig
		if sig == "" {
			sig = "tools:0"
		}
		row := byShape[sig]
		if row == nil {
			row = &ToolShapeRow{Shape: sig, Declared: r.ToolsDeclared,
				Calls: map[string]int{}, DeclaredBytes: r.declBytes}
			byShape[sig] = row
		}
		row.Requests++
		for _, name := range r.ToolCalls {
			row.Calls[name]++
		}
	}
	out := make([]ToolShapeRow, 0, len(byShape))
	for _, row := range byShape {
		for _, name := range row.Declared {
			if row.Calls[name] == 0 {
				row.NeverCalled = append(row.NeverCalled, name)
			}
		}
		sort.Strings(row.NeverCalled)
		if len(row.Calls) == 0 {
			row.Calls = nil
		}
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Requests > out[j].Requests })
	return out
}

// SessionRows summarizes the grouped sessions for the report JSON.
func (a *SessionAnalysis) SessionRows() []SessionRow {
	out := make([]SessionRow, 0, len(a.Sessions))
	for _, s := range a.Sessions {
		row := SessionRow{ID: s.ID, Title: s.Title, ChatID: s.ChatID,
			ContinuedFrom: s.ContinuedFrom, Requests: len(s.Recs), Tasks: len(s.Tasks)}
		if len(s.Recs) > 0 {
			row.From = s.Recs[0].TS.Format(time.RFC3339)
			row.To = s.Recs[len(s.Recs)-1].TS.Format(time.RFC3339)
			row.Class = workloadClass(s.Recs[0])
			if s.Recs[0].DetailFile != "" {
				row.Link = "details/" + s.Recs[0].DetailFile
			}
		}
		var dursDur, dursTTFT []int64
		for _, r := range s.Recs {
			switch r.Outcome {
			case "ok":
				row.OK++
			case "canceled":
				row.Canceled++
			default:
				row.Errors++
			}
			row.TokensIn += r.Usage.In
			row.TokensInCached += r.Usage.CacheRead
			row.TokensInCacheWrite += r.Usage.CacheWrite
			row.TokensOut += r.Usage.Out
			if r.UsageOK {
				row.TokensKnown++
			}
			row.BytesIn += r.bytesIn
			row.BytesOut += r.bytesOut
			row.Messages += int64(r.Msgs)
			row.MessagesKnown++
			for role, c := range r.RoleChars {
				if row.RoleChars == nil {
					row.RoleChars = map[string]int64{}
				}
				row.RoleChars[role] += c
			}
			if r.ttftMS > 0 {
				row.TTFTMSSum += r.ttftMS
				row.TTFTKnown++
				dursTTFT = append(dursTTFT, r.ttftMS)
			}
			row.DurMSSum += r.durMS
			if r.durMS > row.DurMSMax {
				row.DurMSMax = r.durMS
			}
			if r.durMS > 0 {
				row.RequestsWithDur++
				dursDur = append(dursDur, r.durMS)
			}
			row.Attempts += r.attempts
			if r.Truncated {
				row.Truncated++
			}
			if r.Fallbacks > 0 {
				row.Fallbacks++
			}
			row.Images += r.Images
			row.ImagesCompressed += r.ImagesCompressed
		}
		row.DurMSP50, row.DurMSP95 = percentiles(dursDur)
		row.TTFTMSP50, row.TTFTMSP95 = percentiles(dursTTFT)
		if row.DurMSSum > 0 {
			row.TokOutPerSec = round2(float64(row.TokensOut) / (float64(row.DurMSSum) / 1000))
		}
		if len(row.RoleChars) == 0 {
			row.RoleChars = nil
		}
		out = append(out, row)
	}
	return out
}

// requestRow is one line of vmr-requests.jsonl. Every field is rule-
// extracted; unavailable signals are omitted rather than fabricated.
type requestRow struct {
	TS       string   `json:"ts"`
	Session  string   `json:"session,omitempty"`
	Task     string   `json:"task,omitempty"`
	Turn     int      `json:"turn,omitempty"`         // within task
	SessTurn int      `json:"session_turn,omitempty"` // within session
	TraceID  string   `json:"trace_id,omitempty"`
	ChatID   string   `json:"chat_id,omitempty"`
	Shape    string   `json:"shape,omitempty"`
	Tags     []string `json:"tags,omitempty"`

	Model    string `json:"model"`
	Protocol string `json:"protocol"`
	Outcome  string `json:"outcome"`
	Endpoint string `json:"endpoint,omitempty"`
	Attempts int    `json:"attempts"`
	Stream   bool   `json:"stream"`
	DurMS    int64  `json:"dur_ms"`
	TTFTMS   int64  `json:"ttft_ms,omitempty"`

	Msgs           int      `json:"msgs,omitempty"`
	DeltaMsgs      int      `json:"delta_msgs,omitempty"`
	ReplacedTail   int      `json:"replaced_tail,omitempty"`
	NewInstruction string   `json:"new_instruction,omitempty"`
	FinishReason   string   `json:"finish_reason,omitempty"`
	Truncated      bool     `json:"truncated,omitempty"`
	ToolCalls      []string `json:"tool_calls,omitempty"`
	ToolsDeclared  int      `json:"tools_declared,omitempty"`

	TokensIn         int64 `json:"tokens_in,omitempty"`
	TokensInCached   int64 `json:"tokens_in_cached,omitempty"`
	TokensInCacheWrt int64 `json:"tokens_in_cache_write,omitempty"`
	TokensOut        int64 `json:"tokens_out,omitempty"`
	TokensReasoning  int64 `json:"tokens_reasoning,omitempty"`
	BytesIn          int64 `json:"bytes_in,omitempty"`
	BytesOut         int64 `json:"bytes_out,omitempty"`

	Norm       []string `json:"norm,omitempty"`
	DetailFile string   `json:"detail_file,omitempty"`
}

// WriteRequests exports one JSONL feature line per record (ts order) and
// returns the line count.
func WriteRequests(a *SessionAnalysis, path string) (int, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, r := range a.Recs {
		row := requestRow{
			TS:      r.TS.Format("2006-01-02T15:04:05.000Z07:00"),
			Session: r.SessionID, Task: r.TaskID, Turn: r.TaskSeq, SessTurn: r.SessSeq,
			TraceID: r.TraceID, ChatID: r.ChatID, Shape: r.ToolsSig, Tags: r.Tags,
			Model: r.Model, Protocol: r.Protocol, Outcome: r.Outcome,
			Endpoint: nonDash(r.endpoint), Attempts: r.attempts, Stream: r.stream,
			DurMS: r.durMS, TTFTMS: r.ttftMS,
			Msgs: r.Msgs, ReplacedTail: r.ReplacedTail,
			NewInstruction: r.NewInstruction, FinishReason: r.Finish,
			Truncated: r.Truncated, ToolCalls: r.ToolCalls, ToolsDeclared: len(r.ToolsDeclared),
			BytesIn: r.bytesIn, BytesOut: r.bytesOut,
			Norm: r.norm, DetailFile: r.DetailFile,
		}
		if r.SessionID != "" {
			row.DeltaMsgs = r.Msgs - r.DeltaStart
		}
		if r.Compaction {
			row.Session = "" // compaction calls are annotations, not session turns
			row.Turn, row.SessTurn = 0, 0
		}
		if r.UsageOK {
			row.TokensIn, row.TokensOut = r.Usage.In, r.Usage.Out
			row.TokensInCached, row.TokensInCacheWrt = r.Usage.CacheRead, r.Usage.CacheWrite
			row.TokensReasoning = r.Usage.Reasoning
		}
		if err := enc.Encode(row); err != nil {
			return 0, err
		}
	}
	if err := w.Flush(); err != nil {
		return 0, err
	}
	return len(a.Recs), nil
}

func nonDash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}
