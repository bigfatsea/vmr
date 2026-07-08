// Ver 2026-07-08 15:30, by Sonnet 5
package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

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
{"ts":"2026-07-07T11:00:00+08:00","dur_ms":2000,"model":"cheap","protocol":"openai","stream":false,"outcome":"ok","client":{"request":{"body":{"model":"cheap"}},"response":{"status":200,"body":{"usage":{"prompt_tokens":20,"completion_tokens":300}}}},"attempts":[{"endpoint":"p1/m1","dur_ms":500,"error":"rate_limit","response":{"status":429,"body":{"error":"slow"}}},{"endpoint":"p2/m2","dur_ms":1400,"response":{"status":200}}]}
{"ts":"2026-07-07T12:00:00+08:00","dur_ms":300,"model":"cheap","protocol":"openai","stream":false,"outcome":"error","client":{"request":{"body":{"model":"cheap"}},"response":{"status":503,"body":{"error":"down"}}},"attempts":[{"endpoint":"p1/m1","dur_ms":100,"error":"transient","response":{"status":500,"body":{"e":1}}},{"endpoint":"p2/m2","dur_ms":100,"error":"transient","response":{"status":500,"body":{"e":1}}}]}
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
		time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC))
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

func TestMarkdownRendering(t *testing.T) {
	rep := buildTestReport(t)
	md := Markdown(rep)
	for _, want := range []string{
		"# VMR 用量报告",
		"输入缓存命中", "缓存写入",
		"## 按模型", "| cheap |", "| claude |",
		"## 端点可用度", "| p1/m1 | 4 | 2 |", "rate_limit×1", // rolled up across both days
		"## 按日趋势", "| 2026-07-07 |", "| 2026-07-08 |",
		"## 上游错误分布", "| transient | 2 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
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
	rep, err := Build([]string{path}, time.Now())
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
	if !strings.Contains(md, "80.0% (100)") { // cache read share of total in: 100/125
		t.Errorf("markdown missing cache-read share\n---\n%s", md)
	}
	if !strings.Contains(md, " 20 |") { // cache-write absolute count column
		t.Errorf("markdown missing cache-write count\n---\n%s", md)
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

	rep, err := Build([]string{zstPath}, time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC))
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
	if _, err := Build([]string{path}, time.Now()); err == nil {
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
	rep, err := Build([]string{path}, time.Now())
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
