// Ver 2026-08-12 23:40, by Opus 5
package report

import (
	"fmt"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

func renderClientEndpointStr(rep *Report2, lang i18n.Lang) string {
	var b strings.Builder
	renderClientEndpoint(func(f string, a ...any) { b.WriteString(fmt.Sprintf(f, a...)) }, rep, lang)
	return b.String()
}

func TestRenderClientEndpointEmptySkipsSection(t *testing.T) {
	if out := renderClientEndpointStr(&Report2{}, i18n.EN); out != "" {
		t.Errorf("empty ClientEndpoints must render nothing, got:\n%s", out)
	}
}

func TestRenderClientEndpointGroupsByClient(t *testing.T) {
	rep := &Report2{ClientEndpoints: []ClientEndpointRow{
		{ClientKey: "agent-a", Endpoint: "openai:p2:m2", Requests: 3, TokensIn: 600, TokensInFresh: 500, TokensOut: 60},
		{ClientKey: "agent-a", Endpoint: "openai:p1:m1", Requests: 2, TokensIn: 200, TokensInFresh: 150, TokensOut: 20},
		{ClientKey: "agent-b", Endpoint: "openai:p1:m1", Requests: 1, TokensIn: 50, TokensInFresh: 50, TokensOut: 5},
	}}
	out := renderClientEndpointStr(rep, i18n.EN)
	if !strings.Contains(out, "**agent-a**") || !strings.Contains(out, "**agent-b**") {
		t.Errorf("must render one heading per client:\n%s", out)
	}
	// agent-a's total is 800 (600+200); p2:m2's share is 600/800 = 75.0%.
	if !strings.Contains(out, "75.0%") {
		t.Errorf("must render p2:m2's share of agent-a's total tokens:\n%s", out)
	}
	// The two clients must not be merged into one table.
	aIdx := strings.Index(out, "agent-a")
	bIdx := strings.Index(out, "agent-b")
	if aIdx < 0 || bIdx < 0 || aIdx > bIdx {
		t.Errorf("expected agent-a's section before agent-b's:\n%s", out)
	}
}

func TestRenderClientEndpointZH(t *testing.T) {
	rep := &Report2{ClientEndpoints: []ClientEndpointRow{
		{ClientKey: "agent-a", Endpoint: "openai:p1:m1", Requests: 1, TokensIn: 10, TokensInFresh: 10},
	}}
	out := renderClientEndpointStr(rep, i18n.ZH)
	if !strings.Contains(out, "§5.5 按客户端的上游归属") {
		t.Errorf("zh title missing:\n%s", out)
	}
}
