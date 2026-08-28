// Ver 2026-08-28, by Sonnet 5

package story

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/i18n"
	"vmr/internal/taskseg"
)

func htmlTestJourney(t *testing.T) *Journey {
	t.Helper()
	at := func(min int) time.Time { return time.Date(2026, 7, 9, 10, min, 0, 0, time.UTC) }
	sys := msg("system", "sys")
	u1 := msg("user", "audit the auth module for SECRET-LEAK issues")

	r1 := mkRec(at(0), "", []any{sys, u1}, sseToolCalls([]any{
		map[string]any{"id": "c1", "function": map[string]any{"name": "read_file", "arguments": `{"path":"auth.go"}`}},
	}))
	a1 := map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
		map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"auth.go"}`}},
	}}
	tr := map[string]any{"role": "tool", "tool_call_id": "c1", "content": "package auth // TOKEN=abc"}
	r2 := mkRec(at(1), "", []any{sys, u1, a1, tr}, sseText("done reviewing"))

	l := onlyLineage(t, writeJSONL(t, []audit.Record{r1, r2}))
	j, err := Build(l, taskseg.Generic, i18n.EN)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return j
}

func TestRenderHTML_Structure(t *testing.T) {
	j := htmlTestJourney(t)
	out := RenderHTML(j, ComputeMetrics(j), ComputeFindings(j, i18n.EN), i18n.EN, false)

	for _, want := range []string{
		"<!doctype html>",
		"<title>Journey j-",
		"<nav class=\"timeline\">",
		"<main class=\"cards\">",
		"id=\"step-1\"",
		"audit the auth module", // instruction text present un-redacted
		"read_file",             // tool name always shown
		"<style>",
		"IntersectionObserver", // the inline script
	} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	// self-contained: no external resource loads.
	if m := regexp.MustCompile(`(?:src|href)\s*=\s*"https?://`).FindString(out); m != "" {
		t.Errorf("HTML references an external resource: %q", m)
	}
}

func TestRenderHTML_RedactLeaksNothing(t *testing.T) {
	j := htmlTestJourney(t)
	out := RenderHTML(j, ComputeMetrics(j), ComputeFindings(j, i18n.EN), i18n.EN, true)

	for _, secret := range []string{"SECRET-LEAK", "TOKEN=abc", "package auth", "starting", "done reviewing"} {
		if strings.Contains(out, secret) {
			t.Errorf("redacted HTML leaked conversation content: %q", secret)
		}
	}
	// structure survives redaction.
	for _, want := range []string{"read_file", "‹text:", "id=\"step-1\"", "<nav class=\"timeline\">"} {
		if !strings.Contains(out, want) {
			t.Errorf("redacted HTML missing structural element %q", want)
		}
	}
	// no raw content <pre> blocks at all in redact mode.
	if strings.Contains(out, `<pre class="text"`) || strings.Contains(out, `<pre class="json"`) {
		t.Error("redact mode still emitted a raw content <pre> block")
	}
}

func TestRenderHTML_ZHChrome(t *testing.T) {
	j := htmlTestJourney(t)
	out := RenderHTML(j, ComputeMetrics(j), ComputeFindings(j, i18n.EN), i18n.ZH, false)
	if !strings.Contains(out, "<html lang=\"zh\">") || !strings.Contains(out, "时间轴") {
		t.Error("ZH chrome not applied")
	}
}
