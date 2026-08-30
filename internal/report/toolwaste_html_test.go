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
			Shape: "bash,edit,glob,grep,ls,read,web_fetch,write", Requests: 40,
			Declared:           []string{"bash", "edit", "glob", "grep", "ls", "read", "web_fetch", "write"},
			DistinctCalled:     2,
			DeclareUtilization: 0.25,
			SchemaBytesShipped: 8_000_000,
			SchemaWasteBytes:   6_000_000,
		},
		{
			Shape: "read,write", Requests: 10,
			Declared:           []string{"read", "write"},
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
		"never called",
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
	if m := regexp.MustCompile(`(?:src|href)\s*=\s*"https?://`).FindString(out); m != "" {
		t.Errorf("card references an external resource: %q", m)
	}
	if strings.Contains(out, "<script") {
		t.Error("card should carry no script at all")
	}
}

func TestRenderToolWasteHTML_ZH(t *testing.T) {
	rep := &Report2{Tools: []ToolShapeRow{{Shape: "a,b", Requests: 5, Declared: []string{"a", "b"}, DeclareUtilization: 0.5, SchemaBytesShipped: 1000, SchemaWasteBytes: 500}}}
	out := RenderToolWasteHTML(rep, i18n.ZH)
	if !strings.Contains(out, `<html lang="zh">`) || !strings.Contains(out, "工具 Schema 浪费审计") {
		t.Errorf("ZH chrome not applied")
	}
}
