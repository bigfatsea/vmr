// Ver 2026-09-16, by Gemini

// CLI-level tests for shared helpers: dialHost scheme stripping, addr
// sanitization, and any other cross-cutting command helpers that don't
// belong in a single subcommand's own test file.
package main

import "testing"

// TestDialHost_StripsURLScheme covers the IS-18 fix: a user passing -addr
// http://127.0.0.1:8800 must not produce a doubled scheme when the caller
// prepends "http://" again. Also covers the existing wildcard-rewrite and
// passthrough cases.
func TestDialHost_StripsURLScheme(t *testing.T) {
	cases := map[string]string{
		// URL-scheme inputs: the http:// / https:// must be stripped.
		"http://127.0.0.1:8800":  "127.0.0.1:8800",
		"https://127.0.0.1:8800": "127.0.0.1:8800",
		"http://0.0.0.0:8800":    "127.0.0.1:8800",
		"https://[::]:8800":      "127.0.0.1:8800",
		// Wildcard rewrites (existing behavior, must be preserved).
		"0.0.0.0:8800": "127.0.0.1:8800",
		"*:8800":       "127.0.0.1:8800",
		":8800":        "127.0.0.1:8800",
		"[::]:8800":    "127.0.0.1:8800",
		// Non-wildcard: untouched.
		"127.0.0.1:8901": "127.0.0.1:8901",
		"localhost:8800": "localhost:8800",
		// Not a host:port — pass through to let the dial report the real reason.
		"garbage": "garbage",
	}
	for in, want := range cases {
		if got := dialHost(in); got != want {
			t.Errorf("dialHost(%q) = %q, want %q", in, got, want)
		}
	}
}
