// Ver 2026-08-23, by Gemini
package router

import (
	"net/http"
	"testing"

	"vmr/internal/core"
)

// fakeEndpoint builds a minimal *core.Endpoint with just the fields
// applyPin/parsePin touch — Provider and Model.
func fakeEndpoint(provider, model string) *core.Endpoint {
	return &core.Endpoint{Provider: provider, Model: model}
}

func TestParsePin(t *testing.T) {
	cases := []struct {
		name      string
		headers   map[string]string
		wantProv  string
		wantModel string
		active    bool
	}{
		{"no headers", map[string]string{}, "", "", false},
		{"provider only", map[string]string{"X-VMR-Provider": "google"}, "google", "", true},
		{"model only", map[string]string{"X-VMR-Target-Model": "gemini-3.1-flash-lite"}, "", "gemini-3.1-flash-lite", true},
		{"both", map[string]string{"X-VMR-Provider": "google", "X-VMR-Target-Model": "gemini-3.1-flash-lite"}, "google", "gemini-3.1-flash-lite", true},
		{"whitespace trimmed", map[string]string{"X-VMR-Provider": "  google  "}, "google", "", true},
		{"empty value is inactive", map[string]string{"X-VMR-Provider": " "}, "", "", false},
		{"lowercase header name canonicalized", map[string]string{"x-vmr-provider": "google"}, "google", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.headers {
				h.Set(k, v)
			}
			p := parsePin(h)
			if p.provider != tc.wantProv || p.model != tc.wantModel {
				t.Fatalf("parsePin = {%q, %q}, want {%q, %q}", p.provider, p.model, tc.wantProv, tc.wantModel)
			}
			if p.active() != tc.active {
				t.Fatalf("active() = %v, want %v (pin=%q)", p.active(), tc.active, p.String())
			}
		})
	}
}

func TestParsePinNilHeader(t *testing.T) {
	p := parsePin(nil)
	if p.active() {
		t.Fatal("parsePin(nil) must be inactive")
	}
}

func TestApplyPin(t *testing.T) {
	cands := []*core.Endpoint{
		fakeEndpoint("google", "gemini-3.1-flash-lite"),
		fakeEndpoint("google", "gemini-3.5-flash"),
		fakeEndpoint("openrouter", "stealth/ox-alpha"),
		fakeEndpoint("bai", "deepseek-v4-flash"),
	}

	// No pin = pure no-op (nil,false), candidates untouched.
	out, active := applyPin(cands, pin{})
	if active || out != nil {
		t.Fatalf("applyPin(no pin) = (%v,%v), want (nil,false)", out, active)
	}

	// Provider pin.
	out, active = applyPin(cands, pin{provider: "google"})
	if !active || len(out) != 2 {
		t.Fatalf("provider pin: len=%d active=%v, want 2,true", len(out), active)
	}
	for _, ep := range out {
		if ep.Provider != "google" {
			t.Errorf("provider pin leaked %q", ep.Provider)
		}
	}

	// Model pin.
	out, _ = applyPin(cands, pin{model: "gemini-3.5-flash"})
	if len(out) != 1 || out[0].Model != "gemini-3.5-flash" {
		t.Fatalf("model pin = %+v, want single gemini-3.5-flash", out)
	}

	// Both.
	out, _ = applyPin(cands, pin{provider: "google", model: "gemini-3.5-flash"})
	if len(out) != 1 || out[0].Provider != "google" || out[0].Model != "gemini-3.5-flash" {
		t.Fatalf("both pin = %+v, want google/gemini-3.5-flash", out)
	}

	// Intersection preserves order.
	out, _ = applyPin(cands, pin{provider: "google"})
	if out[0].Model != "gemini-3.1-flash-lite" || out[1].Model != "gemini-3.5-flash" {
		t.Fatalf("order not preserved: %+v", out)
	}

	// No match → empty, still active.
	out, active = applyPin(cands, pin{provider: "nonexistent"})
	if !active || len(out) != 0 {
		t.Fatalf("no-match pin = (%v,%v), want (empty,true)", out, active)
	}
}
