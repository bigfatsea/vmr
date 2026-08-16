// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"encoding/json"
	"regexp"
	"strings"
	"vmr/internal/chatmsg"
)

// VerificationCandidate represents a tool call identified as an attempt to run a verification command.
type VerificationCandidate struct {
	ToolName string `json:"tool_name"`
	Command  string `json:"command"`
	Pattern  string `json:"pattern"`
}

var (
	shellToolNames = map[string]bool{
		"bash":            true,
		"sh":              true,
		"zsh":             true,
		"exec":            true,
		"execute_command": true,
		"run_terminal":    true,
		"terminal":        true,
		"run_command":     true,
	}

	verificationCmdPatterns = []struct {
		name    string
		matcher *regexp.Regexp
	}{
		{name: "go test", matcher: regexp.MustCompile(`\bgo\s+test\b`)},
		{name: "pytest", matcher: regexp.MustCompile(`\bpytest\b`)},
		{name: "npm test", matcher: regexp.MustCompile(`\bnpm\s+test\b`)},
		{name: "pnpm test", matcher: regexp.MustCompile(`\bpnpm\s+test\b`)},
		{name: "cargo test", matcher: regexp.MustCompile(`\bcargo\s+test\b`)},
		{name: "cargo check", matcher: regexp.MustCompile(`\bcargo\s+check\b`)},
		{name: "git status", matcher: regexp.MustCompile(`\bgit\s+status\b`)},
		{name: "git diff", matcher: regexp.MustCompile(`\bgit\s+diff\b`)},
		{name: "eslint", matcher: regexp.MustCompile(`\beslint\b`)},
		{name: "golangci-lint", matcher: regexp.MustCompile(`\bgolangci-lint\b`)},
		{name: "tsc", matcher: regexp.MustCompile(`\btsc\b`)},
		{name: "jest", matcher: regexp.MustCompile(`\bjest\b`)},
		{name: "vitest", matcher: regexp.MustCompile(`\bvitest\b`)},
		{name: "mvn test", matcher: regexp.MustCompile(`\bmvn\s+test\b`)},
		{name: "gradle test", matcher: regexp.MustCompile(`\bgradle\s+test\b`)},
		{name: "make test", matcher: regexp.MustCompile(`\bmake\s+test\b`)},
		{name: "make check", matcher: regexp.MustCompile(`\bmake\s+check\b`)},
	}
)

// ExtractShellVerificationCandidate inspects a ToolCall to identify candidate verification commands executed via shell tools.
func ExtractShellVerificationCandidate(tc chatmsg.ToolCall) (VerificationCandidate, bool) {
	normTool := strings.ToLower(strings.TrimSpace(tc.Name))
	if !shellToolNames[normTool] {
		return VerificationCandidate{}, false
	}

	cmdText := extractCommandText(tc.Args)
	if cmdText == "" {
		return VerificationCandidate{}, false
	}

	for _, p := range verificationCmdPatterns {
		if p.matcher.MatchString(cmdText) {
			return VerificationCandidate{
				ToolName: tc.Name,
				Command:  cmdText,
				Pattern:  p.name,
			}, true
		}
	}

	return VerificationCandidate{}, false
}

func extractCommandText(args string) string {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		var m map[string]any
		if err := json.Unmarshal([]byte(trimmed), &m); err == nil {
			for _, key := range []string{"command", "cmd", "script", "input", "CommandLine"} {
				if v, ok := m[key]; ok {
					if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
						return strings.TrimSpace(s)
					}
				}
			}
			if v, ok := m["args"]; ok {
				if arr, ok := v.([]any); ok {
					var parts []string
					for _, elem := range arr {
						if s, ok := elem.(string); ok {
							parts = append(parts, s)
						}
					}
					if len(parts) > 0 {
						return strings.Join(parts, " ")
					}
				}
			}
		}
	}

	return trimmed
}
