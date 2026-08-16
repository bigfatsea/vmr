// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"strings"
	"testing"
	"vmr/internal/chatmsg"
	"vmr/internal/i18n"
)

func TestComputeToolSequences(t *testing.T) {
	t.Parallel()

	s1 := &Step{
		Seq:       1,
		ToolCalls: []chatmsg.ToolCall{tc("read_file", `{"path":"a.go"}`)},
	}
	s2 := &Step{
		Seq:       2,
		ToolCalls: []chatmsg.ToolCall{tc("edit_file", `{"path":"a.go"}`)},
	}
	s3 := &Step{
		Seq:       3,
		ToolCalls: []chatmsg.ToolCall{tc("bash", `{"cmd":"go test"}`)},
		NewEvents: []*Event{{Msg: chatmsg.Message{Role: "tool", Text: "❌ is_error: command failed"}}},
	}

	j := &Journey{
		Tasks: []*Task{{
			Steps: []*Step{s1, s2, s3},
		}},
	}

	patterns := computeToolSequences([]*Journey{j})
	if len(patterns) == 0 {
		t.Fatalf("expected patterns to be found, got none")
	}

	found2Gram := false
	found3Gram := false

	for _, p := range patterns {
		if p.Length == 2 && strings.Join(p.Sequence, " -> ") == "read_file -> edit_file" {
			found2Gram = true
			if p.Occurrences != 1 || p.ErrorRate != 0.0 {
				t.Errorf("read_file -> edit_file pattern stats mismatch: %+v", p)
			}
		}
		if p.Length == 3 && strings.Join(p.Sequence, " -> ") == "read_file -> edit_file -> bash" {
			found3Gram = true
			if p.Occurrences != 1 || p.ErrorRate != 1.0 {
				t.Errorf("3-gram pattern stats mismatch: %+v", p)
			}
		}
	}

	if !found2Gram {
		t.Errorf("missing expected 2-gram pattern 'read_file -> edit_file'")
	}
	if !found3Gram {
		t.Errorf("missing expected 3-gram pattern 'read_file -> edit_file -> bash'")
	}

	// Verify Markdown rendering
	var b strings.Builder
	renderToolSequenceSection(&b, patterns, i18n.EN)
	out := b.String()
	if !strings.Contains(out, "## Frequent Tool Call Sequences") {
		t.Errorf("rendered markdown missing title: %s", out)
	}
	if !strings.Contains(out, "read_file → edit_file") {
		t.Errorf("rendered markdown missing sequence: %s", out)
	}
}
