// Ver 2026-07-08 15:30, by Sonnet 5
package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"vmr/internal/audit"
)

// TestAttemptErrorClassFallback covers backward compatibility with audit
// logs written before Attempt.ErrorClass existed: the class must still be
// recoverable from the free-text Error field alone, exactly matching what
// the old prefix-parsing logic used to produce.
func TestAttemptErrorClassFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    audit.Attempt
		want string
	}{
		{"new log: ErrorClass wins even if Error looks parseable",
			audit.Attempt{Error: "network: dial tcp refused", ErrorClass: "network"}, "network"},
		{"old log: HTTP-classified error has no colon, used verbatim",
			audit.Attempt{Error: "rate_limit"}, "rate_limit"},
		{"old log: non-HTTP failure uses the class:detail prefix",
			audit.Attempt{Error: "network: dial tcp: connection refused"}, "network"},
		{"old log: canceled has no colon at all, used verbatim",
			audit.Attempt{Error: "canceled by client"}, "canceled by client"},
		{"old log: truncated uses the prefix",
			audit.Attempt{Error: "truncated: stream idle timeout"}, "truncated"},
		{"success attempt: no error, no class", audit.Attempt{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := attemptErrorClass(tc.a); got != tc.want {
				t.Errorf("attemptErrorClass(%+v) = %q, want %q", tc.a, got, tc.want)
			}
		})
	}
}

func TestExtractUsage(t *testing.T) {
	cases := []struct {
		name                           string
		body                           any
		in, out, cacheRead, cacheWrite int64
		ok                             bool
	}{
		{"openai json", map[string]any{"usage": map[string]any{"prompt_tokens": 10.0, "completion_tokens": 20.0}}, 10, 20, 0, 0, true},
		{"anthropic json", map[string]any{"usage": map[string]any{"input_tokens": 5.0, "output_tokens": 7.0}}, 5, 7, 0, 0, true},
		{"no usage", map[string]any{"choices": []any{}}, 0, 0, 0, 0, false},
		{"anthropic sse", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":41,\"output_tokens\":1}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n\ndata: [DONE]\n", 41, 9, 0, 0, true},
		{"openai sse with usage chunk", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":6,\"completion_tokens\":3}}\n\ndata: [DONE]\n", 6, 3, 0, 0, true},
		{"sse without usage", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n", 0, 0, 0, 0, false},
		// Anthropic: input_tokens excludes cache counters — total In sums all three.
		{"anthropic cache json", map[string]any{"usage": map[string]any{
			"input_tokens": 5.0, "output_tokens": 7.0,
			"cache_read_input_tokens": 100.0, "cache_creation_input_tokens": 20.0,
		}}, 125, 7, 100, 20, true},
		{"anthropic cache sse", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":1,\"cache_read_input_tokens\":100,\"cache_creation_input_tokens\":20}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n", 125, 9, 100, 20, true},
		// OpenAI: prompt_tokens_details.cached_tokens is a subset already inside prompt_tokens.
		{"openai cache json", map[string]any{"usage": map[string]any{
			"prompt_tokens": 110.0, "completion_tokens": 20.0,
			"prompt_tokens_details": map[string]any{"cached_tokens": 90.0},
		}}, 110, 20, 90, 0, true},
		// DeepSeek: prompt_cache_hit_tokens is likewise a subset of prompt_tokens.
		{"deepseek cache json", map[string]any{"usage": map[string]any{
			"prompt_tokens": 110.0, "completion_tokens": 20.0,
			"prompt_cache_hit_tokens": 80.0, "prompt_cache_miss_tokens": 30.0,
		}}, 110, 20, 80, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, ok := ExtractUsage(c.body)
			if u.In != c.in || u.Out != c.out || u.CacheRead != c.cacheRead || u.CacheWrite != c.cacheWrite || ok != c.ok {
				t.Errorf("got %+v ok=%v, want in=%d out=%d cacheRead=%d cacheWrite=%d ok=%v",
					u, ok, c.in, c.out, c.cacheRead, c.cacheWrite, c.ok)
			}
		})
	}
}

// Synthetic audit lines covering: ok with usage, failover rescue, hard error,
// stream, and a second day. Shapes mirror design doc §8.2.
const day1 = `{"ts":"2026-07-07T10:00:00+08:00","dur_ms":1000,"model":"cheap","protocol":"openai","stream":false,"outcome":"ok","client":{"request":{"body":{"model":"cheap"}},"response":{"status":200,"body":{"usage":{"prompt_tokens":10,"completion_tokens":100}}}},"attempts":[{"endpoint":"p1/m1","dur_ms":900,"response":{"status":200}}]}
{"ts":"2026-07-07T11:00:00+08:00","dur_ms":2000,"model":"cheap","protocol":"openai","stream":false,"outcome":"ok","client":{"request":{"body":{"model":"cheap"}},"response":{"status":200,"body":{"usage":{"prompt_tokens":20,"completion_tokens":300}}}},"attempts":[{"endpoint":"p1/m1","dur_ms":500,"error":"rate_limit","error_class":"rate_limit","response":{"status":429,"body":{"error":"slow"}}},{"endpoint":"p2/m2","dur_ms":1400,"response":{"status":200}}]}
{"ts":"2026-07-07T12:00:00+08:00","dur_ms":300,"model":"cheap","protocol":"openai","stream":false,"outcome":"error","client":{"request":{"body":{"model":"cheap"}},"response":{"status":503,"body":{"error":"down"}}},"attempts":[{"endpoint":"p1/m1","dur_ms":100,"error":"transient","error_class":"transient","response":{"status":500,"body":{"e":1}}},{"endpoint":"p2/m2","dur_ms":100,"error":"transient","error_class":"transient","response":{"status":500,"body":{"e":1}}}]}
{"ts":"2026-07-07T13:00:00+08:00","dur_ms":1500,"model":"claude","protocol":"anthropic","stream":true,"outcome":"ok","client":{"request":{"body":{"model":"claude"}},"response":{"status":200,"body":"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":40,\"output_tokens\":1}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":60}}\n"}},"attempts":[{"endpoint":"a1/mm","dur_ms":1400,"response":{"status":200}}]}
bad line not json
`

const day2 = `{"ts":"2026-07-08T09:00:00+08:00","dur_ms":800,"model":"cheap","protocol":"openai","stream":false,"outcome":"ok","client":{"request":{"body":{"model":"cheap"}},"response":{"status":200,"body":{"usage":{"prompt_tokens":5,"completion_tokens":50}}}},"attempts":[{"endpoint":"p1/m1","dur_ms":700,"response":{"status":200}}]}
`

func buildTestReport(t *testing.T) *Report {
	dir := t.TempDir()
	for name, content := range map[string]string{"a.jsonl": day1, "b.jsonl": day2} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := Build([]string{filepath.Join(dir, "a.jsonl"), filepath.Join(dir, "b.jsonl")},
		time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestBuildAggregation(t *testing.T) {
	rep := buildTestReport(t)

	if rep.Meta.Records != 5 || rep.Meta.ParseErrors != 1 {
		t.Errorf("meta: %+v", rep.Meta)
	}
	if len(rep.Rows) != 3 { // 07-07×cheap, 07-07×claude, 07-08×cheap
		t.Fatalf("rows: %d %+v", len(rep.Rows), rep.Rows)
	}

	var cheap7 *Row
	for i := range rep.Rows {
		if rep.Rows[i].Date == "2026-07-07" && rep.Rows[i].Model == "cheap" {
			cheap7 = &rep.Rows[i]
		}
	}
	if cheap7 == nil {
		t.Fatal("missing 07-07 cheap row")
	}
	if cheap7.Requests != 3 || cheap7.OK != 2 || cheap7.Errors != 1 {
		t.Errorf("counts: %+v", cheap7)
	}
	if cheap7.Fallbacks != 2 || cheap7.Attempts != 5 { // 1 + 2 + 2 attempts
		t.Errorf("fallback/attempts: %+v", cheap7)
	}
	if cheap7.TokensIn != 30 || cheap7.TokensOut != 400 || cheap7.TokensKnown != 2 {
		t.Errorf("tokens: %+v", cheap7)
	}
	// tok/s = 400 tokens over 3000ms of token-known requests
	if cheap7.TokOutPerSec < 133 || cheap7.TokOutPerSec > 134 {
		t.Errorf("tok/s: %v", cheap7.TokOutPerSec)
	}
	if cheap7.DurMSMax != 2000 || cheap7.DurMSP50 != 1000 {
		t.Errorf("durations: p50=%d max=%d", cheap7.DurMSP50, cheap7.DurMSMax)
	}
	if cheap7.BytesIn == 0 || cheap7.BytesOut == 0 {
		t.Errorf("bytes must be counted: %+v", cheap7)
	}

	// Endpoint availability: p1/m1 on 07-07 = 1 ok / 3 attempts.
	var p1 *EndpointRow
	for i := range rep.Endpoints {
		if rep.Endpoints[i].Date == "2026-07-07" && rep.Endpoints[i].Endpoint == "p1/m1" {
			p1 = &rep.Endpoints[i]
		}
	}
	if p1 == nil {
		t.Fatal("missing p1/m1 endpoint row")
	}
	if p1.Attempts != 3 || p1.OK != 1 || p1.Failed != 2 || p1.Availability != 0.33 {
		t.Errorf("p1 availability: %+v", p1)
	}
	if p1.ErrorClasses["rate_limit"] != 1 || p1.ErrorClasses["transient"] != 1 {
		t.Errorf("p1 error classes: %+v", p1.ErrorClasses)
	}

	// Stream record usage extracted from SSE text.
	for _, r := range rep.Rows {
		if r.Model == "claude" && (r.TokensIn != 40 || r.TokensOut != 60 || r.Streams != 1) {
			t.Errorf("claude sse usage: %+v", r)
		}
	}
}

// TestEndpointsAllAndHoursOfDayMergeAcrossDates covers the exact regression
// found manually against real production logs: vmr-report.md's 端点可用度
// and 每小时活跃度 tables always showed "-/-" for 首字延迟/请求耗时, even
// though every record carries dur_ms. Root cause: those tables used to
// merge already-`finish*`-processed per-date buckets (rep.Endpoints /
// rep.Hours), and finishEndpoint/finishHour free the raw dur_ms/ttft_ms
// slices right after computing that bucket's own per-date percentiles — so
// re-merging "finished" buckets across dates had nothing left to compute a
// true percentile from, no matter how much data existed. The fix adds
// genuinely independent EndpointsAll/HoursOfDay buckets that accumulate raw
// values directly during Build's single pass, exactly like Overall does for
// Rows. Two records at the same endpoint / same local hour on different
// calendar dates is the minimal case that exercises the merge.
func TestEndpointsAllAndHoursOfDayMergeAcrossDates(t *testing.T) {
	lines := `{"ts":"2026-07-07T10:00:00+08:00","dur_ms":1000,"model":"m","protocol":"openai","outcome":"ok","client":{"request":{"body":{"model":"m"}},"response":{"status":200}},"attempts":[{"endpoint":"p1/m1","dur_ms":1000,"response":{"status":200}}]}
{"ts":"2026-07-08T10:30:00+08:00","dur_ms":2000,"model":"m","protocol":"openai","outcome":"ok","client":{"request":{"body":{"model":"m"}},"response":{"status":200}},"attempts":[{"endpoint":"p1/m1","dur_ms":2000,"response":{"status":200}}]}
`
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Build([]string{path}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(rep.EndpointsAll) != 1 {
		t.Fatalf("EndpointsAll = %d rows, want 1 (same endpoint, both dates merged)", len(rep.EndpointsAll))
	}
	e := rep.EndpointsAll[0]
	if e.Attempts != 2 || e.OK != 2 {
		t.Errorf("EndpointsAll[0] counts = %+v", e)
	}
	if e.DurMSP50 != 1000 || e.DurMSP95 != 2000 {
		t.Errorf("EndpointsAll[0] duration percentiles = p50=%d p95=%d, want p50=1000 p95=2000 (both cross-date requests present, not \"-/-\")", e.DurMSP50, e.DurMSP95)
	}

	if len(rep.HoursOfDay) != 1 {
		t.Fatalf("HoursOfDay = %d rows, want 1 (both records fall in local hour 10)", len(rep.HoursOfDay))
	}
	h := rep.HoursOfDay[0]
	if h.Hour != 10 || h.Requests != 2 {
		t.Errorf("HoursOfDay[0] = %+v", h)
	}
	if h.DurMSP50 != 1000 || h.DurMSP95 != 2000 {
		t.Errorf("HoursOfDay[0] duration percentiles = p50=%d p95=%d, want p50=1000 p95=2000 (both cross-date requests present, not \"-/-\")", h.DurMSP50, h.DurMSP95)
	}

	// The per-date buckets (JSON granularity) must still exist independently.
	if len(rep.Endpoints) != 2 || len(rep.Hours) != 2 {
		t.Errorf("per-date buckets: endpoints=%d hours=%d, want 2 each (one per date)", len(rep.Endpoints), len(rep.Hours))
	}
}

func TestMarkdownRendering(t *testing.T) {
	rep := buildTestReport(t)
	md := Markdown(rep)
	for _, want := range []string{
		"# VMR 用量报告",
		"详单见 [vmr-requests-index.md](./vmr-requests-index.md)",
		"Tokens In/CacheHit/Out", "平均Tokens In/Out", "p50/p95 首字延迟", "p50/p95 请求耗时", "平均吞吐 (tok/s)",
		"Req/Fall/Trunc", "图片/压缩",
		"## 按模型", "| cheap |", "| claude |",
		"## 端点可用度",
		// p1/m1 has successful requests on both 07-07 (dur_ms=1000) and
		// 07-08 (dur_ms=800): a real cross-date p50/p95 must show here, not
		// "-/-" — this is the endpoint-rollup regression check.
		"800ms / 1000ms",
		"## 上游错误分布", "**p1/m1**", "- rate_limit × 1", "- transient × 1", "**p2/m2**",
		"## 按日趋势", "| 2026-07-07 |", "| 2026-07-08 |",
		"## 每小时活跃度",
		// Hour 10 has exactly one request (07-07 10:00, dur_ms=1000): its
		// merged-across-dates duration percentile must show a real value,
		// not "-/-" — this is the hourly-rollup regression check.
		"| 10:00 | 1/-/- |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
	// The link at the top must appear before the per-model table, and the
	// error distribution must sit right after endpoint availability, ahead
	// of the daily-trend section.
	if strings.Index(md, "详单见") > strings.Index(md, "## 按模型") {
		t.Error("vmr-requests-index.md link should be near the top, not after ## 按模型")
	}
	if i, j, k := strings.Index(md, "## 端点可用度"), strings.Index(md, "## 上游错误分布"), strings.Index(md, "## 按日趋势"); !(i < j && j < k) {
		t.Errorf("expected 端点可用度 < 上游错误分布 < 按日趋势 in document order, got positions %d/%d/%d", i, j, k)
	}
	if strings.Contains(md, "错误分布 |") {
		t.Error("端点可用度 table should no longer have an 错误分布 column")
	}
}

func TestBuild_CacheTokens(t *testing.T) {
	// Anthropic response with cache_read + cache_creation, one record.
	lines := `{"ts":"2026-07-08T10:00:00+08:00","dur_ms":100,"model":"claude","protocol":"anthropic","outcome":"ok","client":{"request":{"body":{}},"response":{"status":200,"body":{"usage":{"input_tokens":5,"output_tokens":7,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}}}
`
	path := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Build([]string{path}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rep.Rows))
	}
	r := rep.Rows[0]
	// total in = 5 (fresh) + 100 (cache read) + 20 (cache write) = 125
	if r.TokensIn != 125 || r.TokensInCached != 100 || r.TokensInCacheWrite != 20 || r.TokensOut != 7 {
		t.Errorf("cache tokens: %+v", r)
	}

	md := Markdown(rep)
	if !strings.Contains(md, "125 / 100(80.0%) / 7") { // 3-tuple: In / CacheHit(share) / Out
		t.Errorf("markdown missing 3-tuple token cell\n---\n%s", md)
	}
}

func TestBuild_ShapeStatsAndTTFT(t *testing.T) {
	// Two records with chat bodies + ttft_ms, one legacy record without either.
	lines := `{"ts":"2026-07-08T10:00:00+08:00","dur_ms":1000,"ttft_ms":200,"model":"m","protocol":"openai","outcome":"ok","client":{"request":{"body":{"model":"m","messages":[{"role":"system","content":"ss"},{"role":"user","content":"uuuu"}]}},"response":{"status":200,"body":{"usage":{"prompt_tokens":10,"completion_tokens":5}}}}}
{"ts":"2026-07-08T10:01:00+08:00","dur_ms":1000,"ttft_ms":400,"model":"m","protocol":"openai","outcome":"ok","client":{"request":{"body":{"model":"m","messages":[{"role":"user","content":"uu"},{"role":"assistant","content":"aaaa"},{"role":"tool","content":"tttttt","tool_call_id":"c"}]}},"response":{"status":200,"body":{"usage":{"prompt_tokens":30,"completion_tokens":15}}}}}
{"ts":"2026-07-08T10:02:00+08:00","dur_ms":1000,"model":"m","protocol":"openai","outcome":"ok","client":{"request":{"body":{"model":"m"}},"response":{"status":200}}}
`
	path := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Build([]string{path}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rep.Rows))
	}
	r := rep.Rows[0]
	if r.Messages != 5 || r.MessagesKnown != 2 {
		t.Errorf("messages = %d/%d, want 5/2", r.Messages, r.MessagesKnown)
	}
	if r.RoleChars["system"] != 2 || r.RoleChars["user"] != 6 || r.RoleChars["assistant"] != 4 || r.RoleChars["tool"] != 6 {
		t.Errorf("role chars = %v", r.RoleChars)
	}
	if r.TTFTMSSum != 600 || r.TTFTKnown != 2 {
		t.Errorf("ttft = %d/%d, want 600/2", r.TTFTMSSum, r.TTFTKnown)
	}

	md := Markdown(rep)
	for _, want := range []string{
		"| 2.5 |",             // avg messages: 5 msgs / 2 known
		"| 200ms / 400ms |",   // p50/p95 TTFT: 200/400 from 2 values
		"| 1000ms / 1000ms |", // p50/p95 dur_ms
		"system 2 (11.1%)",    // share with absolute counts: 2 of 18 total chars
		"tool 6 (33.3%)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

func TestPercentiles(t *testing.T) {
	p50, p95 := percentiles([]int64{100})
	if p50 != 100 || p95 != 100 {
		t.Errorf("single: %d %d", p50, p95)
	}
	var durs []int64
	for i := int64(1); i <= 100; i++ {
		durs = append(durs, i*10)
	}
	p50, p95 = percentiles(durs)
	if p50 != 500 || p95 != 950 {
		t.Errorf("100 samples: p50=%d p95=%d", p50, p95)
	}
}

// TestBuild_ReadsCompressedInput proves Build can consume a .zst file
// exactly like a plain one — the companion half of the audit package's
// housekeeping sweep (internal/audit/housekeep.go), which compresses
// rotated-out days to .zst. Without this, compressing old logs would make
// them invisible to `vmr report`.
func TestBuild_ReadsCompressedInput(t *testing.T) {
	dir := t.TempDir()
	zstPath := filepath.Join(dir, "vmr-audit-2026-07-07.jsonl.zst")
	f, err := os.OpenFile(zstPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Write([]byte(day1)); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rep, err := Build([]string{zstPath}, time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Same fixture as buildTestReport's day1 half: 4 valid lines, 1 bad line.
	if rep.Meta.Records != 4 || rep.Meta.ParseErrors != 1 {
		t.Errorf("meta: %+v", rep.Meta)
	}
	var cheap7 *Row
	for i := range rep.Rows {
		if rep.Rows[i].Date == "2026-07-07" && rep.Rows[i].Model == "cheap" {
			cheap7 = &rep.Rows[i]
		}
	}
	if cheap7 == nil || cheap7.TokensIn != 30 || cheap7.TokensOut != 400 {
		t.Fatalf("cheap7 from compressed input: %+v", cheap7)
	}
}

// TestOpenAuditFile_RejectsGarbageZst confirms a malformed .zst input surfaces
// as an error from Build rather than silently reading garbage or panicking.
func TestOpenAuditFile_RejectsGarbageZst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vmr-audit-2026-07-07.jsonl.zst")
	if err := os.WriteFile(path, []byte("not a zstd frame"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build([]string{path}, time.Now(), nil); err == nil {
		t.Error("expected an error reading a non-zstd .zst file, got nil")
	}
}

func TestBuild_ProtocolSplitsRows(t *testing.T) {
	// The same virtual-model name in both protocol groups is two distinct
	// models — the report must not merge them into one row (format 2).
	lines := `{"ts":"2026-07-08T10:00:00+08:00","dur_ms":100,"model":"agent","protocol":"openai","outcome":"ok","client":{"request":{"body":{}},"response":{"status":200}}}
{"ts":"2026-07-08T10:01:00+08:00","dur_ms":100,"model":"agent","protocol":"anthropic","outcome":"ok","client":{"request":{"body":{}},"response":{"status":200}}}`
	path := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := Build([]string{path}, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Rows) != 2 {
		t.Fatalf("rows = %d, want 2 (one per protocol); rows: %+v", len(rep.Rows), rep.Rows)
	}
	protos := map[string]int{}
	for _, r := range rep.Rows {
		if r.Model != "agent" || r.Requests != 1 {
			t.Errorf("unexpected row: %+v", r)
		}
		protos[r.Protocol]++
	}
	if protos["openai"] != 1 || protos["anthropic"] != 1 {
		t.Errorf("protocol split wrong: %v", protos)
	}
}

func TestForEachLineSkipsOversizedLines(t *testing.T) {
	input := "short1\n" + strings.Repeat("x", 100) + "\nshort2\n"
	var got []string
	skipped := 0
	err := forEachLine(strings.NewReader(input), 32, func(line []byte) {
		got = append(got, string(line))
	}, func() { skipped++ })
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 {
		t.Errorf("skipped = %d, want 1", skipped)
	}
	if len(got) != 2 || got[0] != "short1" || got[1] != "short2" {
		t.Errorf("lines = %q, want [short1 short2]", got)
	}
}

func TestForEachLineHandlesFinalLineWithoutNewline(t *testing.T) {
	var got []string
	err := forEachLine(strings.NewReader("a\nb"), 32, func(line []byte) {
		got = append(got, string(line))
	}, nil)
	if err != nil || len(got) != 2 || got[1] != "b" {
		t.Errorf("got %q err=%v, want [a b]", got, err)
	}
}

func TestWorkloadsTokensKnownCountsUsageBearingRecordsOnly(t *testing.T) {
	a := &SessionAnalysis{Recs: []*ReqInfo{
		{Usage: Usage{In: 100, Out: 10}, UsageOK: true},
		{}, // no extractable usage: must not dilute the per-request averages
	}}
	rows := a.Workloads()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Requests != 2 || rows[0].TokensKnown != 1 {
		t.Errorf("requests=%d tokens_known=%d, want 2 and 1", rows[0].Requests, rows[0].TokensKnown)
	}
}
