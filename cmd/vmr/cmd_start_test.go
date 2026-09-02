// Ver 2026-08-03, by Opus 5
package main

import (
	"bytes"
	"io"
	"log"
	"strings"
	"testing"

	"vmr/internal/config"
	"vmr/internal/logtee"
)

// TestLogConfigCheckIssuesBannersErrors locks in the fix for config.Check()
// being skipped on the start/hot-reload paths: an empty api_key (a
// SeverityError) must show up in the loud banner, not silently pass through
// to a config that 401s every request.
func TestLogConfigCheckIssuesBannersErrors(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	logConfigCheckIssues(logger, []config.Issue{
		{Provider: "p1", Field: "api_key", Message: `provider "p1": api_key missing`, Severity: config.SeverityError},
	}, nil)

	out := buf.String()
	if !strings.Contains(out, "CONFIG PROBLEMS") || !strings.Contains(out, `api_key missing`) {
		t.Errorf("expected the CONFIG PROBLEMS banner mentioning the issue, got: %q", out)
	}
}

// TestLogConfigCheckIssuesBannersEmptyEnvRefs: an unset ${VAR} the config
// referenced is the most common "forgot the key" cause — it must reach the
// banner even when Check() itself found nothing.
func TestLogConfigCheckIssuesBannersEmptyEnvRefs(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	logConfigCheckIssues(logger, nil, []string{"DEEPSEEK_API_KEY"})

	out := buf.String()
	if !strings.Contains(out, "CONFIG PROBLEMS") || !strings.Contains(out, "DEEPSEEK_API_KEY") {
		t.Errorf("expected the banner to name the unset env var, got: %q", out)
	}
}

// TestLogConfigCheckIssuesWarningStaysQuiet: a SeverityWarning is one WARN
// line, no banner.
func TestLogConfigCheckIssuesWarningStaysQuiet(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	logConfigCheckIssues(logger, []config.Issue{
		{Field: "listen", Message: "open proxy", Severity: config.SeverityWarning},
	}, nil)

	out := buf.String()
	if strings.Contains(out, "CONFIG PROBLEMS") {
		t.Errorf("a warning should not raise the banner, got: %q", out)
	}
	if !strings.Contains(out, "WARN config check: open proxy") {
		t.Errorf("expected a quiet WARN line, got: %q", out)
	}
}

// TestLogConfigCheckIssuesNoIssuesNoOutput ensures a clean config produces no
// log noise on every start/reload.
func TestLogConfigCheckIssuesNoIssuesNoOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	logConfigCheckIssues(logger, nil, nil)

	if buf.Len() != 0 {
		t.Errorf("expected no output for zero issues, got: %q", buf.String())
	}
}

// TestLoggerWiring_TeeReceivesStampedLine pins cmd_start's logger wiring:
// stampWriter wraps the MultiWriter(os.Stderr, tee) fan-out, so /log serves
// byte-identical (timestamp included) copies of what stderr carries.
func TestLoggerWiring_TeeReceivesStampedLine(t *testing.T) {
	var stderr bytes.Buffer
	tee := logtee.New(8)
	logger := log.New(stampWriter{io.MultiWriter(&stderr, tee)}, "", 0)

	logger.Printf("route test line %d", 42)

	if !strings.Contains(stderr.String(), "route test line 42") {
		t.Fatalf("stderr = %q, missing logged text", stderr.String())
	}
	lines, _, cancel := tee.Follow()
	defer cancel()
	if len(lines) != 1 {
		t.Fatalf("tee buffered %d lines, want 1", len(lines))
	}
	if lines[0] != strings.TrimRight(stderr.String(), "\n") {
		t.Fatalf("tee line %q != stderr %q", lines[0], stderr.String())
	}
}
