// Ver 2026-08-03, by Opus 5
package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"vmr/internal/config"
)

// TestLogConfigCheckIssuesEmitsWarnPerIssue locks in the fix for config.Check()
// being skipped on the start/hot-reload paths: an empty api_key must show up
// as a visible WARN line, not silently pass through to a config that 401s
// every request.
func TestLogConfigCheckIssuesEmitsWarnPerIssue(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	logConfigCheckIssues(logger, []config.Issue{
		{Provider: "p1", Field: "api_key", Message: `provider "p1": api_key missing`},
	})

	out := buf.String()
	if !strings.Contains(out, "WARN") || !strings.Contains(out, `api_key missing`) {
		t.Errorf("expected a WARN line mentioning the issue, got: %q", out)
	}
}

// TestLogConfigCheckIssuesNoIssuesNoOutput ensures a clean config produces no
// log noise on every start/reload.
func TestLogConfigCheckIssuesNoIssuesNoOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	logConfigCheckIssues(logger, nil)

	if buf.Len() != 0 {
		t.Errorf("expected no output for zero issues, got: %q", buf.String())
	}
}
