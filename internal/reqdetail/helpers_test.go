// Ver 2026-08-20 00:00, by Sonnet 5

// Tests for small shared helpers: AttemptErrorClass and ms — both
// load-bearing via session.go/detail.go.
package reqdetail

import (
	"testing"

	"vmr/internal/audit"
)

// TestAttemptErrorClassFallback covers backward compatibility with audit
// logs that lack Attempt.ErrorClass: the class must still be recoverable
// from the free-text Error field alone.
func TestAttemptErrorClassFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		a    audit.Attempt
		want string
	}{
		{"new log: ErrorClass wins even if Error looks parseable",
			audit.Attempt{Error: "network: dial tcp refused", ErrorClass: "network"}, "network"},
		{"old log: HTTP-classified error has no colon, used verbatim",
			audit.Attempt{Error: "rate_limit"}, "rate_limit"},
		{"old log: non-HTTP failure uses the class:detail prefix",
			audit.Attempt{Error: "network: dial tcp: connection refused"}, "network"},
		{"old log: canceled has no colon at all, used verbatim",
			audit.Attempt{Error: "canceled by client"}, "canceled by client"},
		{"old log: truncated uses the prefix",
			audit.Attempt{Error: "truncated: stream idle timeout"}, "truncated"},
		{"success attempt: no error, no class", audit.Attempt{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AttemptErrorClass(tc.a); got != tc.want {
				t.Errorf("AttemptErrorClass(%+v) = %q, want %q", tc.a, got, tc.want)
			}
		})
	}
}

func TestMs(t *testing.T) {
	for _, tc := range []struct {
		v    int64
		want string
	}{
		{0, "0ms"},
		{999, "999ms"},
		{1000, "1.0s"}, // exactly 1000ms must already switch to seconds
		{1001, "1.0s"},
		{2500, "2.5s"},
	} {
		if got := ms(tc.v); got != tc.want {
			t.Errorf("ms(%d) = %q, want %q", tc.v, got, tc.want)
		}
	}
}
