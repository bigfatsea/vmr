// Ver 2026-08-05, by Sonnet 5
package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"vmr/internal/i18n"
)

func TestExpandReportEnv(t *testing.T) {
	t.Setenv("VMR_TEST_REPORTCONFIG_KEY", "secret-value")
	os.Unsetenv("VMR_TEST_REPORTCONFIG_UNSET")

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"known var", "llm_key: \"${VMR_TEST_REPORTCONFIG_KEY}\"", "llm_key: \"secret-value\""},
		{"unset var expands to empty", "llm_key: \"${VMR_TEST_REPORTCONFIG_UNSET}\"", "llm_key: \"\""},
		{"bare dollar stays literal", "note: cost is $5", "note: cost is $5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandReportEnv(c.in)
			if got != c.want {
				t.Errorf("expandReportEnv(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestLoadReportConfig_ExpandsLLMKey(t *testing.T) {
	t.Setenv("VMR_TEST_REPORTCONFIG_KEY", "sk-from-env")
	path := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(path, []byte("llm_key: \"${VMR_TEST_REPORTCONFIG_KEY}\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := loadReportConfig(path)
	if err != nil {
		t.Fatalf("loadReportConfig: %v", err)
	}
	if rc.LLMKey != "sk-from-env" {
		t.Errorf("rc.LLMKey = %q, want %q (env expansion should have applied before YAML decode)", rc.LLMKey, "sk-from-env")
	}
}

func TestLoadReportConfig_AllFieldsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.yaml")
	yaml := `language: zh
output: myreports
details: false
include_partial: true
llm_addr: 127.0.0.1:8080
llm_model: agent
llm_cache_dir: /tmp/llmcache
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := loadReportConfig(path)
	if err != nil {
		t.Fatalf("loadReportConfig: %v", err)
	}
	if rc.Language != "zh" || rc.Output != "myreports" || rc.LLMAddr != "127.0.0.1:8080" ||
		rc.LLMModel != "agent" || rc.LLMCacheDir != "/tmp/llmcache" {
		t.Errorf("unexpected rc: %+v", rc)
	}
	if rc.Details == nil || *rc.Details != false {
		t.Errorf("rc.Details = %v, want pointer to false", rc.Details)
	}
	if rc.IncludePartial == nil || *rc.IncludePartial != true {
		t.Errorf("rc.IncludePartial = %v, want pointer to true", rc.IncludePartial)
	}
}

func TestLoadReportConfig_EmptyFileIsOK(t *testing.T) {
	for _, c := range []struct {
		name    string
		content string
	}{
		{"0 bytes", ""},
		{"comments only", "# just a comment\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.yaml")
			if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			rc, err := loadReportConfig(path)
			if err != nil {
				t.Fatalf("loadReportConfig: %v", err)
			}
			if rc != (reportConfig{}) {
				t.Errorf("expected zero-value reportConfig, got %+v", rc)
			}
		})
	}
}

func TestResolveReportConfig_EmptyDefaultFileIsSilent(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultReportConfigFile, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rc := resolveReportConfig("", &buf)
	if rc != (reportConfig{}) {
		t.Errorf("expected zero-value reportConfig for an empty report.yaml, got %+v", rc)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no warning for an empty report.yaml, got %q", buf.String())
	}
}

func TestLoadReportConfig_UnknownFieldIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(path, []byte("languague: zh\n"), 0o600); err != nil { // typo'd key
		t.Fatal(err)
	}
	if _, err := loadReportConfig(path); err == nil {
		t.Error("expected an error for an unknown/typo'd key, got nil")
	}
}

func TestResolveReportConfig_MissingDefaultPathIsSilent(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rc := resolveReportConfig("", &buf)
	if rc != (reportConfig{}) {
		t.Errorf("expected zero-value reportConfig when report.yaml is absent, got %+v", rc)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no warning for an absent default report.yaml, got %q", buf.String())
	}
}

func TestResolveReportConfig_MissingExplicitPathWarns(t *testing.T) {
	var buf bytes.Buffer
	rc := resolveReportConfig(filepath.Join(t.TempDir(), "does-not-exist.yaml"), &buf)
	if rc != (reportConfig{}) {
		t.Errorf("expected zero-value reportConfig on load failure, got %+v", rc)
	}
	if buf.Len() == 0 {
		t.Error("expected a warning when an explicitly-given -report-config path is missing")
	}
}

func TestResolveLanguage(t *testing.T) {
	var buf bytes.Buffer
	if lang, err := resolveLanguage("zh", reportConfig{Language: "en"}, &buf); err != nil || lang != i18n.ZH {
		t.Errorf("explicit -lang should win over report.yaml: lang=%v err=%v", lang, err)
	}
	buf.Reset()
	if lang, err := resolveLanguage("", reportConfig{Language: "zh"}, &buf); err != nil || lang != i18n.ZH {
		t.Errorf("report.yaml's language should apply when -lang is absent: lang=%v err=%v", lang, err)
	}
	buf.Reset()
	if lang, err := resolveLanguage("", reportConfig{}, &buf); err != nil || lang != i18n.EN {
		t.Errorf("default should be English when neither is set: lang=%v err=%v", lang, err)
	}
	buf.Reset()
	if _, err := resolveLanguage("bogus", reportConfig{}, &buf); err == nil {
		t.Error("an invalid explicit -lang must be a hard error")
	}
	buf.Reset()
	lang, err := resolveLanguage("", reportConfig{Language: "bogus"}, &buf)
	if err != nil || lang != i18n.EN {
		t.Errorf("an invalid report.yaml language must degrade to English, not error: lang=%v err=%v", lang, err)
	}
	if buf.Len() == 0 {
		t.Error("expected a warning for an invalid report.yaml language")
	}
}

func TestResolveString(t *testing.T) {
	if got := resolveString("cli", "yaml", "def"); got != "cli" {
		t.Errorf("flag value should win: got %q", got)
	}
	if got := resolveString("", "yaml", "def"); got != "yaml" {
		t.Errorf("report.yaml value should win when flag is unset: got %q", got)
	}
	if got := resolveString("", "", "def"); got != "def" {
		t.Errorf("built-in default should apply when neither is set: got %q", got)
	}
	if got := resolveString("", "", ""); got != "" {
		t.Errorf("an empty def (llm_* fields) must stay empty, not invent a path: got %q", got)
	}
}

func TestResolveBool(t *testing.T) {
	yes, no := true, false
	if got := resolveBool(true, false, &yes); got != false {
		t.Error("an explicitly-passed flag must win even over a report.yaml value")
	}
	if got := resolveBool(false, true, &no); got != false {
		t.Error("report.yaml's value must apply when the flag wasn't explicitly passed")
	}
	if got := resolveBool(false, true, nil); got != true {
		t.Error("with no report.yaml value, the flag's own (already-defaulted) value must stand")
	}
}

func TestFlagPassed(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	details := fs.Bool("details", true, "")
	other := fs.Bool("other", false, "")
	if err := fs.Parse([]string{"-details=false"}); err != nil {
		t.Fatal(err)
	}
	_ = details
	_ = other
	if !flagPassed(fs, "details") {
		t.Error("flagPassed(details) = false, want true (it was passed on the command line)")
	}
	if flagPassed(fs, "other") {
		t.Error("flagPassed(other) = true, want false (it was never passed)")
	}
}
