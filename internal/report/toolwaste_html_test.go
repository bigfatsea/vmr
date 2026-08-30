// Ver 2026-08-29, by Sonnet 5

package report

import (
	"regexp"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

func TestRenderToolWasteHTML(t *testing.T) {
	rep := &Report2{Tools: []ToolShapeRow{
		{
			Shape: "tools:8/abc12345", Requests: 40,
			Declared:           []string{"bash", "edit", "glob", "grep", "ls", "read", "web_fetch", "write"},
			NeverCalled:        []string{"edit", "glob", "grep", "ls", "web_fetch", "write"},
			DistinctCalled:     2,
			DeclareUtilization: 0.25,
			SchemaBytesShipped: 8_000_000,
			SchemaWasteBytes:   6_000_000,
		},
		{
			Shape: "tools:2/def67890", Requests: 10,
			Declared:           []string{"read", "write"},
			NeverCalled:        nil,
			DistinctCalled:     2,
			DeclareUtilization: 1.0,
			SchemaBytesShipped: 400_000,
			SchemaWasteBytes:   0,
		},
	}}

	out := RenderToolWasteHTML(rep, i18n.EN)

	for _, want := range []string{
		"<!doctype html>",
		"TOOL SCHEMA AUDIT",
		"71%", // 6M waste / 8.4M shipped
		"Never-called tools",
		"edit, glob, grep, ls", // first 4 never-called names spelled out
		"+2 more",              // the remaining 2 collapsed
		`class="sig">tools:8/abc12345`,
		"Dead weight",
		"6.00 MB",
		"2 / 8", // used / declared
		`class="bar"`,
		"≈1.5M", // 6M bytes / 4
		"self-contained",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tool-waste card missing %q", want)
		}
	}
	// The zero-waste "read,write" shape is dropped from a card that has real
	// offenders to show.
	if strings.Contains(out, "tools:2/def67890") || strings.Contains(out, "(all called)") {
		t.Errorf("fully-used shape should not appear on a card with wasteful shapes:\n%s", out)
	}
	if m := regexp.MustCompile(`(?:src|href)\s*=\s*"https?://`).FindString(out); m != "" {
		t.Errorf("card references an external resource: %q", m)
	}
	if strings.Contains(out, "<script") {
		t.Error("card should carry no script at all")
	}
}

// TestRenderToolWasteHTML_AllFullyUsed covers the degenerate fallback: when
// no shape wastes anything, the table shows the rows anyway (with the
// all-called note) rather than rendering an empty table.
func TestRenderToolWasteHTML_AllFullyUsed(t *testing.T) {
	rep := &Report2{Tools: []ToolShapeRow{{
		Shape: "tools:2/def67890", Requests: 10,
		Declared: []string{"read", "write"}, NeverCalled: nil,
		DistinctCalled: 2, DeclareUtilization: 1.0,
		SchemaBytesShipped: 400_000, SchemaWasteBytes: 0,
	}}}
	out := RenderToolWasteHTML(rep, i18n.EN)
	if !strings.Contains(out, "(all called)") {
		t.Errorf("all-fully-used card should still list the shape with the all-called note:\n%s", out)
	}
}

func TestRenderToolWasteHTML_ZH(t *testing.T) {
	rep := &Report2{Tools: []ToolShapeRow{{Shape: "tools:2/aabbccdd", Requests: 5, Declared: []string{"a", "b"}, NeverCalled: []string{"b"}, DistinctCalled: 1, DeclareUtilization: 0.5, SchemaBytesShipped: 1000, SchemaWasteBytes: 500}}}
	out := RenderToolWasteHTML(rep, i18n.ZH)
	if !strings.Contains(out, `<html lang="zh">`) || !strings.Contains(out, "工具 Schema 浪费审计") {
		t.Errorf("ZH chrome not applied")
	}
	if !strings.Contains(out, "从未调用的工具") {
		t.Errorf("ZH never-called column header not applied:\n%s", out)
	}
}
