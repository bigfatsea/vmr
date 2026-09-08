// Ver 2026-09-16, by pi-agent

package fmtutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fmtFixture is the cross-language display-drift contract (§5.6): one
// shared testdata/fmt_cases.json, consumed by the dashboard's JS runtime
// (internal/dashboard/js_test.go) and by this Go test. If the two sides
// ever drift apart on how a number renders, one of them goes red.
//
// The fixture lives in the dashboard package because the JS side has no
// way to reach out of its own tree; the relative path below is the Go
// side's half of that contract.
type fmtFixtureCase struct {
	Fn    string      `json:"fn"`
	Input json.Number `json:"input"`
	Want  string      `json:"want"`
}

func TestDisplayFormatFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "dashboard", "testdata", "fmt_cases.json"))
	if err != nil {
		t.Fatalf("read shared display fixture (the dashboard side owns it): %v", err)
	}
	var fixture struct {
		Cases []fmtFixtureCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fmt_cases.json: %v", err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fmt_cases.json holds no cases — the cross-language contract regressed to nothing")
	}

	// dispatch maps a fixture fn name to the fmtutil function asserting
	// want. FmtPercent takes an explicit decimals argument the fixture does
	// not carry; 1 is the dashboard JS's default and the value every
	// FmtPercent case in the file was written against.
	//
	// FmtCurrency/FmtCost pin currency display across Go and the dashboard's
	// common.js runtime, ensuring consistent financial formatting.
	dispatch := map[string]func(t *testing.T, input json.Number, want string){
		"FmtTokens": func(t *testing.T, input json.Number, want string) {
			n, err := input.Int64()
			if err != nil {
				t.Fatalf("FmtTokens input %s is not an integer: %v", input, err)
			}
			if got := FmtTokens(n); got != want {
				t.Errorf("FmtTokens(%d) = %q, want %q", n, got, want)
			}
		},
		"FmtBytes": func(t *testing.T, input json.Number, want string) {
			n, err := input.Int64()
			if err != nil {
				t.Fatalf("FmtBytes input %s is not an integer: %v", input, err)
			}
			if got := FmtBytes(n); got != want {
				t.Errorf("FmtBytes(%d) = %q, want %q", n, got, want)
			}
		},
		"FmtPercent": func(t *testing.T, input json.Number, want string) {
			f, err := input.Float64()
			if err != nil {
				t.Fatalf("FmtPercent input %s is not a number: %v", input, err)
			}
			if got := FmtPercent(f, 1); got != want {
				t.Errorf("FmtPercent(%v, 1) = %q, want %q", f, got, want)
			}
		},
		"FmtCurrency": func(t *testing.T, input json.Number, want string) {
			f, err := input.Float64()
			if err != nil {
				t.Fatalf("FmtCurrency input %s is not a number: %v", input, err)
			}
			if got := FmtCurrency(f); got != want {
				t.Errorf("FmtCurrency(%v) = %q, want %q", f, got, want)
			}
		},
		"FmtCost": func(t *testing.T, input json.Number, want string) {
			f, err := input.Float64()
			if err != nil {
				t.Fatalf("FmtCost input %s is not a number: %v", input, err)
			}
			if got := FmtCost(f); got != want {
				t.Errorf("FmtCost(%v) = %q, want %q", f, got, want)
			}
		},
		"FmtCurrencyPrecise": func(t *testing.T, input json.Number, want string) {
			f, err := input.Float64()
			if err != nil {
				t.Fatalf("FmtCurrencyPrecise input %s is not a number: %v", input, err)
			}
			if got := FmtCurrencyPrecise(f); got != want {
				t.Errorf("FmtCurrencyPrecise(%v) = %q, want %q", f, got, want)
			}
		},
	}

	seen := map[string]bool{}
	for _, tc := range fixture.Cases {
		fn, ok := dispatch[tc.Fn]
		if !ok {
			t.Logf("fixture fn %q has no fmtutil counterpart — skipped", tc.Fn)
			continue
		}
		seen[tc.Fn] = true
		t.Run(tc.Fn+"("+tc.Input.String()+")", func(t *testing.T) { fn(t, tc.Input, tc.Want) })
	}
	// Every fmtutil function the fixture was written to pin must actually
	// have been exercised — a renamed fn key would otherwise silently
	// unpin it while the file still parses.
	for _, fn := range []string{"FmtTokens", "FmtBytes", "FmtPercent", "FmtCurrency", "FmtCost", "FmtCurrencyPrecise"} {
		if !seen[fn] {
			t.Errorf("fixture holds no %q case — the shared contract no longer covers %s", fn, fn)
		}
	}
}
