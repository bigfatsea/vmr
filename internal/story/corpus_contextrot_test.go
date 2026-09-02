// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"strings"
	"testing"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/i18n"
)

func TestComputeContextRot(t *testing.T) {
	t.Parallel()

	// Create test journeys with steps in different token buckets
	s1 := &Step{
		Seq:      1,
		Manifest: &ctxgraph.Manifest{Usage: chatmsg.Usage{In: 10000}, UsageInOK: true}, // 0-32k
	}
	s2 := &Step{
		Seq:       2,
		Manifest:  &ctxgraph.Manifest{Usage: chatmsg.Usage{In: 40000}, UsageInOK: true}, // 32k-64k
		NewEvents: []*Event{{Msg: chatmsg.Message{Role: "tool", Text: "❌ is_error: command failed"}}},
	}
	s3 := &Step{
		Seq:      3,
		Manifest: &ctxgraph.Manifest{Usage: chatmsg.Usage{In: 50000}, UsageInOK: true}, // 32k-64k
	}
	s4 := &Step{
		Seq:      4,
		Manifest: &ctxgraph.Manifest{Usage: chatmsg.Usage{In: 150000}, UsageInOK: true}, // 128k-256k
	}
	// s5 has no manifest → excluded from every bucket (nil manifest, no
	// usage data). It should NOT pollute 0-32k and instead land in the
	// "usage unknown" pseudo-bucket.
	s5 := &Step{
		Seq: 5,
	}

	j := &Journey{
		Tasks: []*Task{{
			Steps: []*Step{s1, s2, s3, s4, s5},
		}},
	}

	buckets := computeContextRot([]*Journey{j}, nil)

	// 5 real ranges + 1 "usage unknown" pseudo-bucket (s5 excluded)
	if len(buckets) != 6 {
		t.Fatalf("expected 6 buckets (5 ranges + 1 excluded pseudo-bucket), got %d", len(buckets))
	}

	// Bucket 0: 0-32k (1 step, 0 error)
	if b := buckets[0]; b.StepCount != 1 || b.ErrorStepCount != 0 {
		t.Errorf("bucket 0-32k: %+v", b)
	}

	// Bucket 1: 32k-64k (2 steps, 1 error)
	if b := buckets[1]; b.StepCount != 2 || b.ErrorStepCount != 1 || b.ErrorRate != 0.5 {
		t.Errorf("bucket 32k-64k: %+v", b)
	}

	// Bucket 3: 128k-256k (1 step, 0 error)
	if b := buckets[3]; b.StepCount != 1 || b.ErrorStepCount != 0 {
		t.Errorf("bucket 128k-256k: %+v", b)
	}

	// Bucket 5: "usage unknown" (1 excluded step)
	if b := buckets[5]; b.Range != contextRotUnknownRange || b.StepCount != 1 {
		t.Errorf("usage unknown bucket: %+v", b)
	}

	// Verify Markdown rendering: the excluded note must appear, and the
	// pseudo-bucket must NOT render as a table row.
	var b strings.Builder
	renderContextRotSection(&b, buckets, i18n.EN)
	out := b.String()
	if !strings.Contains(out, "## Context Window Scaling & Quality Inflection") {
		t.Errorf("rendered markdown missing title: %s", out)
	}
	if !strings.Contains(out, "32k-64k") {
		t.Errorf("rendered markdown missing 32k-64k bucket: %s", out)
	}
	if !strings.Contains(out, "1 step(s) excluded: no in-token usage data") {
		t.Errorf("rendered markdown missing excluded note: %s", out)
	}
	if strings.Contains(out, "usage unknown") {
		t.Errorf("rendered markdown should NOT contain 'usage unknown' as a table row: %s", out)
	}
}
