// Ver 2026-09-02 12:10, by pi-agent

package config

import (
	"fmt"
	"strings"
	"testing"
)

// block-style base_url, not validYAML's flow map: URLs with ? in them are
// invalid YAML inside {}.
func credYAML(baseURL string) []byte {
	return []byte(fmt.Sprintf(`
listen: 127.0.0.1:9900
providers:
  - name: p1
    base_url:
      openai-completions: %s
models:
  m1:
    endpoints:
      - protocol: openai-completions
        providers: [p1]
        models: [real-model]
        priority: 1
`, baseURL))
}

// TestParseRejectsBaseURLCredentials: base_url is recorded verbatim in the
// audit trail (Attempt.URL) and its derived reports, and audit redaction only
// covers headers — a credential in the URL would land in cleartext in files
// users copy and share. Rejected at load, not redacted at runtime.
func TestParseRejectsBaseURLCredentials(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		wantErrs []string // error must contain each; empty = must load
	}{
		{"userinfo", "https://user:pass@host/v1", []string{`provider "p1"`, "base_url.openai-completions", "userinfo"}},
		{"api_key query", "https://host/v1?api_key=sk-x", []string{"api_key"}},
		{"token query", "https://host/v1?token=x", []string{"token"}},
		{"case-insensitive key", "https://host/v1?KEY=sk-x", []string{"KEY"}},
		{"clean url", "https://host/v1", nil},
		{"gateway version param", "https://host/v1?api-version=2024-02-01", nil},
		{"other params", "https://host/v1?api-version=2024-02-01&deployment=d1", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(credYAML(tt.baseURL))
			if len(tt.wantErrs) == 0 {
				if err != nil {
					t.Fatalf("want accepted, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("want rejected, loaded cleanly")
			}
			for _, w := range tt.wantErrs {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err, w)
				}
			}
		})
	}
}

// TestParseBaseURLErrorDoesNotEchoCredentialValue: the load error names the
// offending parameter, never its value.
func TestParseBaseURLErrorDoesNotEchoCredentialValue(t *testing.T) {
	_, err := Parse(credYAML("https://host/v1?api_key=sk-supersecret-value"))
	if err == nil {
		t.Fatal("want rejected")
	}
	if strings.Contains(err.Error(), "sk-supersecret-value") {
		t.Errorf("error echoes the credential value: %v", err)
	}
}
