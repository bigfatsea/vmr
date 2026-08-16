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
		Manifest: &ctxgraph.Manifest{Usage: chatmsg.Usage{In: 10000}}, // 0-32k
	}
	s2 := &Step{
		Seq:       2,
		Manifest:  &ctxgraph.Manifest{Usage: chatmsg.Usage{In: 40000}}, // 32k-64k
		NewEvents: []*Event{{Msg: chatmsg.Message{Role: "tool", Text: "❌ is_error: command failed"}}},
	}
	s3 := &Step{
		Seq:      3,
		Manifest: &ctxgraph.Manifest{Usage: chatmsg.Usage{In: 50000}}, // 32k-64k
	}
	s4 := &Step{
		Seq:      4,
		Manifest: &ctxgraph.Manifest{Usage: chatmsg.Usage{In: 150000}}, // 128k-256k
	}

	j := &Journey{
		Tasks: []*Task{{
			Steps: []*Step{s1, s2, s3, s4},
		}},
	}

	buckets := computeContextRot([]*Journey{j})

	if len(buckets) != 5 {
		t.Fatalf("expected 5 buckets, got %d", len(buckets))
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

	// Verify Markdown rendering
	var b strings.Builder
	renderContextRotSection(&b, buckets, i18n.EN)
	out := b.String()
	if !strings.Contains(out, "## Context Window Scaling & Quality Inflection") {
		t.Errorf("rendered markdown missing title: %s", out)
	}
	if !strings.Contains(out, "32k-64k") {
		t.Errorf("rendered markdown missing 32k-64k bucket: %s", out)
	}
}
