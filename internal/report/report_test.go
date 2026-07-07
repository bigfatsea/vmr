// Ver 2026-07-07, by Fable 5
package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractUsage(t *testing.T) {
	cases := []struct {
		name    string
		body    any
		in, out int64
		ok      bool
	}{
		{"openai json", map[string]any{"usage": map[string]any{"prompt_tokens": 10.0, "completion_tokens": 20.0}}, 10, 20, true},
		{"anthropic json", map[string]any{"usage": map[string]any{"input_tokens": 5.0, "output_tokens": 7.0}}, 5, 7, true},
		{"no usage", map[string]any{"choices": []any{}}, 0, 0, false},
		{"anthropic sse", "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":41,\"output_tokens\":1}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n\ndata: [DONE]\n", 41, 9, true},
		{"openai sse with usage chunk", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":6,\"completion_tokens\":3}}\n\ndata: [DONE]\n", 6, 3, true},
		{"sse without usage", "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, out, ok := ExtractUsage(c.body)
			if in != c.in || out != c.out || ok != c.ok {
				t.Errorf("got in=%d out=%d ok=%v, want %d/%d/%v", in, out, ok, c.in, c.out, c.ok)
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
