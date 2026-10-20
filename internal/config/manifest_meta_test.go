// Ver 2026-10-20, by Claude Sonnet 5.5

package config

import (
	"strings"
	"testing"

	_ "vmr/internal/adapter/anthropic"
	_ "vmr/internal/adapter/openai"
)

const metaBase = `
listen: 127.0.0.1:18800
%s
providers:
  - {name: p, base_url: {openai-completions: http://127.0.0.1:1/v1, anthropic-messages: http://127.0.0.1:1}, api_key: k}
models:
%s
`

func parseMeta(t *testing.T, top, models string) (*Config, error) {
	t.Helper()
	return Parse([]byte(strings.Replace(strings.Replace(metaBase, "%s", top, 1), "%s", models, 1)))
}

func TestMetaRolesMustBeKnownAndUnique(t *testing.T) {
	_, err := parseMeta(t, "", `  m: {meta: {roles: [planner]}, endpoints: {openai-completions: [{providers: [p], models: [x]}]}}`)
	if err == nil || !strings.Contains(err.Error(), `unknown role "planner"`) {
		t.Fatalf("unknown role accepted: %v", err)
	}
	_, err = parseMeta(t, "", `  m: {meta: {roles: [fast, fast]}, endpoints: {openai-completions: [{providers: [p], models: [x]}]}}`)
	if err == nil || !strings.Contains(err.Error(), "listed twice") {
		t.Fatalf("duplicate role accepted: %v", err)
	}
}

func TestPublicBaseURLMustBeABareOrigin(t *testing.T) {
	ok := `  m: {endpoints: {openai-completions: [{providers: [p], models: [x]}]}}`
	for _, bad := range []string{"vmr.lan", "ftp://vmr.lan", "https://vmr.lan/v1", "https://vmr.lan?x=1"} {
		if _, err := parseMeta(t, "public_base_url: "+bad, ok); err == nil {
			t.Fatalf("public_base_url %q accepted", bad)
		}
	}
	for _, good := range []string{"https://vmr.lan", "http://10.0.0.5:8800/"} {
		if _, err := parseMeta(t, "public_base_url: "+good, ok); err != nil {
			t.Fatalf("public_base_url %q rejected: %v", good, err)
		}
	}
}

func TestRoleClaimsMustBeUniquePerProtocol(t *testing.T) {
	// Same role on a shared protocol: the manifest would be ambiguous, so the config does not load.
	_, err := parseMeta(t, "", `  a: {meta: {roles: [default, fast]}, endpoints: {openai-completions: [{providers: [p], models: [x]}]}}
  b: {meta: {roles: [fast]}, endpoints: {openai-completions: [{providers: [p], models: [y]}], anthropic-messages: [{providers: [p], models: [y]}]}}`)
	if err == nil || !strings.Contains(err.Error(), `both claim manifest role "fast" on openai-completions`) {
		t.Fatalf("conflicting role claims accepted: %v", err)
	}
	// The same role on disjoint protocols is fine.
	if _, err := parseMeta(t, "", `  a: {meta: {roles: [default, fast]}, endpoints: {openai-completions: [{providers: [p], models: [x]}]}}
  b: {meta: {roles: [default, fast]}, endpoints: {anthropic-messages: [{providers: [p], models: [y]}]}}`); err != nil {
		t.Fatalf("disjoint protocols rejected: %v", err)
	}
}

func TestCheckWarnsWhenAProtocolHasNoDefaultRole(t *testing.T) {
	cfg, err := parseMeta(t, "", `  a: {meta: {roles: [default, fast]}, endpoints: {openai-completions: [{providers: [p], models: [x]}]}}
  b: {meta: {roles: [reasoning]}, endpoints: {anthropic-messages: [{providers: [p], models: [y]}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	var warns int
	for _, is := range cfg.Check() {
		if is.Field == "meta" && is.Severity == SeverityWarning && strings.Contains(is.Message, "anthropic-messages") {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("warns=%d issues=%v", warns, cfg.Check())
	}
}

func TestCheckStaysQuietWithoutMeta(t *testing.T) {
	cfg, err := parseMeta(t, "", `  a: {endpoints: {openai-completions: [{providers: [p], models: [x]}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range cfg.Check() {
		if is.Field == "meta" {
			t.Fatalf("meta issue without meta: %+v", is)
		}
	}
}
