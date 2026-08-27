// Ver 2026-08-15, by Sonnet 5

// Direct coverage for Wrap/Options — a B7 follow-up review's finding: the
// package's own tests (respnorm_test.go, upstreammodel_test.go, fuzz_test.go
// as it stood then) all called the unexported newStream constructor
// directly, so Wrap's own field mapping had zero coverage inside this
// package, relying entirely on internal/router's downstream integration
// tests to catch a regression there. Deliberately NOT a rewrite of every
// existing newStream call site (that tests the state machine's behavior,
// which is unrelated to whether Wrap plumbs its Options correctly) — one
// small, targeted case per Options field is enough to close the actual gap.
package respnorm

import (
	"strings"
	"testing"
)

func TestWrap_ClientModelRewritesModelField(t *testing.T) {
	t.Parallel()
	in := `data: {"model":"upstream-name","choices":[]}` + "\n\n"
	out := readAll(t, Wrap(strings.NewReader(in), Options{ClientModel: "my-virtual-model", IsSSE: true, Protocol: "openai-completions"}))
	if !strings.Contains(out, `"model":"my-virtual-model"`) {
		t.Errorf("Options.ClientModel not applied via Wrap: %q", out)
	}
}

func TestWrap_UpstreamModelDrivesObservedModel(t *testing.T) {
	t.Parallel()
	in := `data: {"model":"actually-served","choices":[]}` + "\n\n"
	ns := Wrap(strings.NewReader(in), Options{ClientModel: "agent", UpstreamModel: "requested-model", IsSSE: true, Protocol: "openai-completions"})
	readAll(t, ns)
	if got, want := ns.ObservedModel(), "actually-served"; got != want {
		t.Errorf("Options.UpstreamModel not applied via Wrap: ObservedModel() = %q, want %q", got, want)
	}
}

func TestWrap_IsSSEFalseSkipsDoneAppend(t *testing.T) {
	t.Parallel()
	in := `{"model":"m","choices":[{"message":{"content":"hi"}}]}`
	out := readAll(t, Wrap(strings.NewReader(in), Options{ClientModel: "agent", IsSSE: false, Protocol: "openai-completions"}))
	if !strings.Contains(out, "hi") {
		t.Errorf("non-SSE body content missing via Wrap: %q", out)
	}
	if strings.Contains(out, "[DONE]") {
		t.Errorf("Options.IsSSE=false must never get [DONE] appended: %q", out)
	}
}

func TestWrap_AnthropicProtocolNeverAppendsDone(t *testing.T) {
	t.Parallel()
	in := `data: {"type":"message_stop"}` + "\n\n"
	out := readAll(t, Wrap(strings.NewReader(in), Options{ClientModel: "claude", IsSSE: true, Protocol: "anthropic-messages"}))
	if strings.Contains(out, "[DONE]") {
		t.Errorf("Options.Protocol=anthropic-messages must never get [DONE] via Wrap: %q", out)
	}
}

func TestWrap_OpaqueSkipsEveryTransform(t *testing.T) {
	t.Parallel()
	in := `data: {"model":"upstream","choices":[]}` + "\n\n"
	out := readAll(t, Wrap(strings.NewReader(in), Options{ClientModel: "agent", IsSSE: true, Protocol: "openai-completions", Opaque: true}))
	if out != in {
		t.Errorf("Options.Opaque=true via Wrap must leave bytes untouched: got %q, want %q", out, in)
	}
}
