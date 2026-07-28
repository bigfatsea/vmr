// Ver 2026-07-28 18:10, by Opus 5

// Capturing the upstream's self-reported model name — the one piece of
// evidence that a relay served something other than what was asked for,
// and the only moment it exists (see noteUpstreamModel).
package router

import (
	"strings"
	"testing"
)

func TestUpstreamModelRecordedOnlyWhenItDiffers(t *testing.T) {
	cases := []struct {
		name           string
		requested      string // what vmr asked the endpoint for
		responseModel  string // what the upstream answered with
		wantObserved   string
		wantClientSees string
	}{
		{"identical: nothing recorded", "deepseek-v4-pro", "deepseek-v4-pro", "", "coding"},
		{"silent swap", "deepseek-v4-pro", "deepseek-v4-flash", "deepseek-v4-flash", "coding"},
		// Both of these are legitimate, and both are recorded anyway: no
		// verdict lives here, only the raw observation. Telling an alias
		// apart from a downgrade needs the aggregate, not one request.
		{"version pinning", "gpt-4o", "gpt-4o-2024-08-06", "gpt-4o-2024-08-06", "coding"},
		{"plan alias", "ark-code-latest", "doubao-seed-code-251015", "doubao-seed-code-251015", "coding"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := `{"id":"x","model":"` + c.responseModel + `","choices":[]}`
			rs := newRespStream(strings.NewReader(body), "coding", c.requested, false, "openai", false)
			out := readAll(t, rs)

			if got := rs.ObservedModel(); got != c.wantObserved {
				t.Errorf("ObservedModel() = %q, want %q", got, c.wantObserved)
			}
			// Whatever was observed, the client still gets the virtual name:
			// this is an observation, never a behavior change.
			if !strings.Contains(out, `"model":"`+c.wantClientSees+`"`) {
				t.Errorf("client body = %s, want the virtual model name", out)
			}
			if strings.Contains(out, c.responseModel) && c.responseModel != c.wantClientSees {
				t.Errorf("upstream model name leaked to the client: %s", out)
			}
		})
	}
}

// The model field repeats in every SSE chunk; the capture must latch on the
// first one rather than rescanning (and re-deciding) per chunk.
func TestUpstreamModelCapturedOncePerStream(t *testing.T) {
	src := strings.NewReader(
		`data: {"id":"a","model":"real-a","object":"chunk"}` + "\n\n" +
			`data: {"id":"b","model":"real-b","object":"chunk"}` + "\n\n")
	rs := newRespStream(src, "agent", "asked-for", true, "openai", false)
	readAll(t, rs)

	if got := rs.ObservedModel(); got != "real-a" {
		t.Errorf("ObservedModel() = %q, want the FIRST chunk's value", got)
	}
}

// An endpoint built without a known upstream model name (every existing
// test, and any caller that doesn't pass one) must record nothing rather
// than report every response as a mismatch against "".
func TestUpstreamModelSkippedWhenRequestedNameUnknown(t *testing.T) {
	rs := newRespStream(strings.NewReader(`{"model":"anything"}`), "coding", "", false, "openai", false)
	readAll(t, rs)
	if got := rs.ObservedModel(); got != "" {
		t.Errorf("ObservedModel() = %q, want empty", got)
	}
}

// A compressed body is never parsed at all (opaque passthrough), so there
// is nothing to observe — and nothing may be inferred from that silence.
func TestUpstreamModelNotCapturedFromOpaqueBody(t *testing.T) {
	rs := newRespStream(strings.NewReader(`{"model":"real"}`), "coding", "asked", false, "openai", true)
	readAll(t, rs)
	if got := rs.ObservedModel(); got != "" {
		t.Errorf("ObservedModel() = %q, want empty for an opaque response", got)
	}
}
