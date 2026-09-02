// Ver 2026-08-05, by Sonnet 5
package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"vmr/internal/i18n"
)

func TestExpandReportEnv(t *testing.T) {
	t.Setenv("VMR_TEST_REPORTCONFIG_KEY", "secret-value")
	t.Setenv("VMR_TEST_REPORTCONFIG_SAFE", "plain-value")
	t.Setenv("VMR_TEST_REPORTCONFIG_NEWLINE", "line1\nline2")
	t.Setenv("VMR_TEST_REPORTCONFIG_COLONSPACE", "a: b")
	t.Setenv("VMR_TEST_REPORTCONFIG_COMMENT", "trailing # here")
	t.Setenv("VMR_TEST_REPORTCONFIG_HASHMID", "trailing#here")
	os.Unsetenv("VMR_TEST_REPORTCONFIG_UNSET")

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"known var", "llm_key: \"${VMR_TEST_REPORTCONFIG_KEY}\"", "llm_key: \"secret-value\""},
		{"unset var expands to empty", "llm_key: \"${VMR_TEST_REPORTCONFIG_UNSET}\"", "llm_key: \"\""},
		{"bare dollar stays literal", "note: cost is $5", "note: cost is $5"},
		{"hash not followed by space is fine", "llm_key: \"${VMR_TEST_REPORTCONFIG_HASHMID}\"", "llm_key: \"trailing#here\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := expandReportEnv(c.in)
			if err != nil {
				t.Fatalf("expandReportEnv(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("expandReportEnv(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestExpandReportEnv_RejectsYAMLStructureBreakers is the R67 regression:
// expansion happens before YAML parsing, so a value carrying a newline, ": "
// or " #" could change the document's structure or silently truncate the
// value at a comment (report.yaml carries llm_key — a " #" suffix would cut
// a secret short with no error, surfacing only as a mysterious 401). The
// same guards internal/config's expandEnv applies must hold here.
func TestExpandReportEnv_RejectsYAMLStructureBreakers(t *testing.T) {
	t.Setenv("VMR_TEST_REPORTCONFIG_NEWLINE", "line1\nline2")
	t.Setenv("VMR_TEST_REPORTCONFIG_COLONSPACE", "a: b")
	t.Setenv("VMR_TEST_REPORTCONFIG_COMMENT", "trailing # here")
	t.Setenv("VMR_TEST_REPORTCONFIG_LEADINGHASH", "#injected comment")
	t.Setenv("VMR_TEST_REPORTCONFIG_PADDEDHASH", "  # padded comment")
	cases := []struct {
		name string
		env  string
	}{{"newline", "VMR_TEST_REPORTCONFIG_NEWLINE"}, {": ", "VMR_TEST_REPORTCONFIG_COLONSPACE"}, {" #", "VMR_TEST_REPORTCONFIG_COMMENT"}, {"leading #", "VMR_TEST_REPORTCONFIG_LEADINGHASH"}, {"indented leading #", "VMR_TEST_REPORTCONFIG_PADDEDHASH"}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := "llm_key: \"${" + c.env + "}\""
			_, err := expandReportEnv(in)
			if err == nil {
				t.Errorf("expandReportEnv(%q) = nil error, want a hard load error", in)
			}
		})
	}
}

// TestExpandReportEnv_CommentLinesStayComments is the other half of the
// leading-# guard: a report.yaml comment line may interpolate a var, and —
// same fail-fast rule as internal/config's expandEnv — the danger is the
// var's VALUE, not the ${...} spelling itself. A comment line whose var is
// unset expands to empty and stays a comment; a value containing "#" is
// rejected above, never silently truncated.
func TestExpandReportEnv_CommentLinesStayComments(t *testing.T) {
	os.Unsetenv("VMR_TEST_REPORTCONFIG_UNSET")
	got, err := expandReportEnv("# llm_key: ${VMR_TEST_REPORTCONFIG_UNSET}\nllm_key: \"plain\"\n")
	if err != nil {
		t.Fatalf("expandReportEnv: %v", err)
	}
	want := "# llm_key: \nllm_key: \"plain\"\n"
	if got != want {
		t.Errorf("expandReportEnv = %q, want %q", got, want)
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
			if !reflect.DeepEqual(rc, reportConfig{}) {
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
	// SourcePath is provenance, not a setting: an empty report.yaml that
	// exists WAS the applied config, and the report header says so. Every
	// actual setting must still be zero.
	if rc.SourcePath != defaultReportConfigFile {
		t.Errorf("SourcePath = %q, want %q — an empty file that exists is still the applied config", rc.SourcePath, defaultReportConfigFile)
	}
	rc.SourcePath = ""
	if !reflect.DeepEqual(rc, reportConfig{}) {
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

// TestResolveReportConfigErr_UnknownKeyIsHardError is the R89 regression: a
// single typo'd key must fail the load, not silently disable ALL of
// report.yaml's settings (self-traffic exclusion, every llm_* field) behind
// a warning. The error must name the file, the line and the key.
func TestResolveReportConfigErr_UnknownKeyIsHardError(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defaultReportConfigFile, []byte("language: zh\nlanguague: zh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, err := resolveReportConfigErr("")
	if err == nil {
		t.Fatalf("a typo'd key must be a hard error, got config %+v", rc)
	}
	for _, want := range []string{"report.yaml", "languague"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error %q should carry the YAML line number", err.Error())
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
	if !reflect.DeepEqual(rc, reportConfig{}) {
		t.Errorf("expected zero-value reportConfig when report.yaml is absent, got %+v", rc)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no warning for an absent default report.yaml, got %q", buf.String())
	}
}

// TestResolveReportConfig_MissingExplicitPathIsError: an explicitly-given
// -report-config path that doesn't exist is the user's own pointer — a hard
// error, not a degrade-to-empty (R89 regression).
func TestResolveReportConfig_MissingExplicitPathIsError(t *testing.T) {
	_, err := resolveReportConfigErr(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Error("expected a hard error when an explicitly-given -report-config path is missing")
	}
}

// TestLoadReportConfig_EnvExpansionGuardIsLoadError: an env var whose value
// would break YAML structure must fail the whole load (R67 through
// loadReportConfig), not produce a quietly-truncated llm_key.
func TestLoadReportConfig_EnvExpansionGuardIsLoadError(t *testing.T) {
	t.Setenv("VMR_TEST_REPORTCONFIG_BADKEY", "sk-real # trailing comment")
	path := filepath.Join(t.TempDir(), "report.yaml")
	if err := os.WriteFile(path, []byte(`llm_key: "${VMR_TEST_REPORTCONFIG_BADKEY}"`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReportConfig(path); err == nil {
		t.Error("expected a load error for an env value carrying ' #', got nil")
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

func TestResolveStringExplicit(t *testing.T) {
	if got := resolveStringExplicit(true, "", "yaml", "def"); got != "" {
		t.Errorf("an explicitly-passed empty flag must win even over a report.yaml value: got %q", got)
	}
	if got := resolveStringExplicit(false, "", "yaml", "def"); got != "yaml" {
		t.Errorf("report.yaml's value must apply when the flag wasn't explicitly passed: got %q", got)
	}
	if got := resolveStringExplicit(false, "", "", "def"); got != "def" {
		t.Errorf("built-in default should apply when neither flag nor report.yaml is set: got %q", got)
	}
	if got := resolveStringExplicit(true, "cli", "yaml", "def"); got != "cli" {
		t.Errorf("an explicitly-passed non-empty flag must win: got %q", got)
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
