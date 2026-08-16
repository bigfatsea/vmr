// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"testing"
	"vmr/internal/chatmsg"
)

func TestExtractShellVerificationCandidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		tc          chatmsg.ToolCall
		wantMatch   bool
		wantPattern string
	}{
		{
			name:        "bash json command with go test",
			tc:          chatmsg.ToolCall{Name: "bash", Args: `{"command": "go test -v ./..."}`},
			wantMatch:   true,
			wantPattern: "go test",
		},
		{
			name:        "exec with pytest",
			tc:          chatmsg.ToolCall{Name: "exec", Args: `{"cmd": "pytest tests/"}`},
			wantMatch:   true,
			wantPattern: "pytest",
		},
		{
			name:        "terminal with git status",
			tc:          chatmsg.ToolCall{Name: "terminal", Args: `{"command": "git status"}`},
			wantMatch:   true,
			wantPattern: "git status",
		},
		{
			name:        "run_terminal with npm test",
			tc:          chatmsg.ToolCall{Name: "run_terminal", Args: `{"script": "npm test"}`},
			wantMatch:   true,
			wantPattern: "npm test",
		},
		{
			name:        "plain string bash with cargo check",
			tc:          chatmsg.ToolCall{Name: "bash", Args: "cargo check --all-targets"},
			wantMatch:   true,
			wantPattern: "cargo check",
		},
		{
			name:        "non-shell tool ignored",
			tc:          chatmsg.ToolCall{Name: "read_file", Args: `{"path": "go test"}`},
			wantMatch:   false,
			wantPattern: "",
		},
		{
			name:        "shell tool with non-verification command",
			tc:          chatmsg.ToolCall{Name: "bash", Args: `{"command": "cat /etc/hosts"}`},
			wantMatch:   false,
			wantPattern: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cand, ok := ExtractShellVerificationCandidate(c.tc)
			if ok != c.wantMatch {
				t.Fatalf("ExtractShellVerificationCandidate(%+v) ok = %v, want %v", c.tc, ok, c.wantMatch)
			}
			if ok && cand.Pattern != c.wantPattern {
				t.Errorf("cand.Pattern = %q, want %q", cand.Pattern, c.wantPattern)
			}
		})
	}
}
