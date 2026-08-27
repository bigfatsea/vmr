// Ver 2026-08-12 23:40, by Opus 5
package report

import (
	"testing"
)

func TestBuildProvidersRollsUpAcrossModels(t *testing.T) {
	rep := &Report2{EndpointsAll: []EndpointRow{
		{
			Endpoint: "openai-completions:volcengine2:deepseek-v4-flash",
			Attempts: 10, OK: 9, Failed: 1, ErrorClasses: map[string]int{"timeout": 1},
			Requests: 9, RequestsOK: 8, WastedMS: 500,
			TokensIn: 1000, TokensInCached: 400, TokensInFresh: 600, TokensOut: 200, TokensKnown: 9,
			RequestsWithDur: 9, DurMSSum: 9000,
			CostEstimate: f64(1.5),
		},
		{
			Endpoint: "openai-completions:volcengine2:doubao-mini",
			Attempts: 5, OK: 5, Failed: 0,
			Requests: 5, RequestsOK: 5,
			TokensIn: 500, TokensInCached: 100, TokensInFresh: 400, TokensOut: 100, TokensKnown: 5,
			RequestsWithDur: 5, DurMSSum: 2500,
			CostEstimate: f64(0.5),
		},
		{
			Endpoint: "openai-completions:volcengine:deepseek-v4-flash", // different account, same model name
			Attempts: 2, OK: 2,
			Requests: 2, RequestsOK: 2,
			TokensIn: 100, TokensInFresh: 100, TokensKnown: 2,
		},
	}}
	rows := buildProviders(rep, nil)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (volcengine2, volcengine)", len(rows))
	}
	// volcengine2 has more tokens (1500) than volcengine (100) -> sorts first.
	v2 := rows[0]
	if v2.Provider != "volcengine2" {
		t.Fatalf("rows[0].Provider = %q, want volcengine2", v2.Provider)
	}
	if v2.Attempts != 15 || v2.Failed != 1 {
		t.Errorf("Attempts/Failed = %d/%d, want 15/1", v2.Attempts, v2.Failed)
	}
	if v2.TokensIn != 1500 || v2.TokensInCached != 500 || v2.TokensInFresh != 1000 || v2.TokensOut != 300 {
		t.Errorf("token roll-up = %+v", v2)
	}
	if len(v2.Models) != 2 || v2.Models[0] != "deepseek-v4-flash" || v2.Models[1] != "doubao-mini" {
		t.Errorf("Models = %v, want sorted [deepseek-v4-flash doubao-mini]", v2.Models)
	}
	if v2.ErrorClasses["timeout"] != 1 {
		t.Errorf("ErrorClasses = %v, want timeout:1", v2.ErrorClasses)
	}
	if v2.CostEstimate == nil || *v2.CostEstimate != 2.0 {
		t.Errorf("CostEstimate = %v, want 2.0", v2.CostEstimate)
	}
	// mean dur = (9000+2500)/(9+5) = 821 (int division)
	if v2.DurMSMean != 821 {
		t.Errorf("DurMSMean = %d, want 821", v2.DurMSMean)
	}
	// volcengine (config.mba.yaml's separate account) must NOT be merged into volcengine2.
	if rows[1].Provider != "volcengine" {
		t.Fatalf("rows[1].Provider = %q, want volcengine (distinct account, same model name)", rows[1].Provider)
	}
	if rows[1].TokensIn != 100 {
		t.Errorf("volcengine TokensIn = %d, want 100 (not merged with volcengine2)", rows[1].TokensIn)
	}
}

func TestBuildProvidersHandlesBothEndpointLabelFormats(t *testing.T) {
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:new-fmt:model-a", TokensIn: 10},
		{Endpoint: "openai-completions/old-fmt/model-b", TokensIn: 20},
	}}
	rows := buildProviders(rep, nil)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.Provider] = true
	}
	if !got["new-fmt"] || !got["old-fmt"] {
		t.Errorf("providers = %v, want both new-fmt and old-fmt recognized", got)
	}
}

func TestBuildProvidersQuotaRef(t *testing.T) {
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:withquota:m", TokensIn: 10},
		{Endpoint: "openai-completions:noquota:m", TokensIn: 5},
	}}
	quotas := map[string][]ProviderQuotaRef{
		"withquota": {{Metric: "tokens", Every: "1mo", Amount: 20000}},
	}
	rows := buildProviders(rep, quotas)
	byName := map[string]ProviderRow{}
	for _, r := range rows {
		byName[r.Provider] = r
	}
	if len(byName["withquota"].Quota) != 1 || byName["withquota"].Quota[0].Amount != 20000 {
		t.Errorf("withquota.Quota = %+v, want one entry, amount 20000", byName["withquota"].Quota)
	}
	if byName["noquota"].Quota != nil {
		t.Errorf("noquota.Quota = %+v, want nil", byName["noquota"].Quota)
	}
}

// Determinism: two providers tied on TokensIn must always sort the same way
// (alphabetical tie-break), or two runs over the same input could produce
// byte-different vmr-report.json (TestBuildIsDeterministic's failure mode).
func TestBuildProvidersDeterministicTieBreak(t *testing.T) {
	rep := &Report2{EndpointsAll: []EndpointRow{
		{Endpoint: "openai-completions:zeta:m", TokensIn: 100},
		{Endpoint: "openai-completions:alpha:m", TokensIn: 100},
	}}
	rows := buildProviders(rep, nil)
	if len(rows) != 2 || rows[0].Provider != "alpha" || rows[1].Provider != "zeta" {
		t.Errorf("order = %v, want [alpha zeta] on a token tie", rows)
	}
}
