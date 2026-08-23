// Ver 2026-08-22, by Sonnet 5

package quota

import (
	"testing"
	"time"

	"vmr/internal/core"
)

func TestIsWildcardModels(t *testing.T) {
	cases := []struct {
		models []string
		want   bool
	}{
		{nil, false},
		{[]string{}, false},
		{[]string{"*"}, true},
		{[]string{"*", "other"}, false}, // not the reserved shape — config.validate rejects this combination, but this function just reports the exact-match question
		{[]string{"gpt-4"}, false},
	}
	for _, tc := range cases {
		if got := IsWildcardModels(tc.models); got != tc.want {
			t.Errorf("IsWildcardModels(%v) = %v, want %v", tc.models, got, tc.want)
		}
	}
}

func TestPerModel(t *testing.T) {
	if PerModel(core.Limit{}) {
		t.Error("PerModel(no Models) = true, want false (shared)")
	}
	if !PerModel(core.Limit{Models: []string{"*"}}) {
		t.Error("PerModel(wildcard) = false, want true")
	}
	if !PerModel(core.Limit{Models: []string{"a", "b"}}) {
		t.Error("PerModel(restricted list) = false, want true")
	}
}

func TestAppliesToModel(t *testing.T) {
	shared := core.Limit{}
	wildcard := core.Limit{Models: []string{"*"}}
	restricted := core.Limit{Models: []string{"a", "b"}}

	if !AppliesToModel(shared, "anything") {
		t.Error("shared Limit must apply to every model")
	}
	if !AppliesToModel(wildcard, "anything") {
		t.Error("wildcard Limit must apply to every model")
	}
	if !AppliesToModel(restricted, "a") {
		t.Error("restricted Limit must apply to its named models")
	}
	if AppliesToModel(restricted, "c") {
		t.Error("restricted Limit must not apply to an unnamed model")
	}
}

func TestModelSetsOverlap(t *testing.T) {
	wildcard := []string{"*"}
	if !ModelSetsOverlap(wildcard, []string{"anything"}) {
		t.Error("wildcard must overlap with any other set")
	}
	if !ModelSetsOverlap([]string{"a", "b"}, []string{"b", "c"}) {
		t.Error("sets sharing an element must overlap")
	}
	if ModelSetsOverlap([]string{"a", "b"}, []string{"c", "d"}) {
		t.Error("disjoint sets must not overlap")
	}
}

func TestLimitKey_SharedIgnoresModel(t *testing.T) {
	l := core.Limit{Metric: core.MetricRequests, EveryText: "1d"}
	if got, want := LimitKey(l, "model-a"), "requests/1d"; got != want {
		t.Errorf("LimitKey(shared) = %q, want %q", got, want)
	}
	if got := LimitKey(l, "model-b"); got != "requests/1d" {
		t.Errorf("LimitKey(shared) must not vary by model, got %q", got)
	}
}

func TestLimitKey_PerModelKeyedByActualModel(t *testing.T) {
	wildcard := core.Limit{Metric: core.MetricRequests, EveryText: "1min", Models: []string{"*"}}
	if got, want := LimitKey(wildcard, "deepseek-r1"), "requests/1min#model=deepseek-r1"; got != want {
		t.Errorf("LimitKey(wildcard) = %q, want %q", got, want)
	}
	if got, want := LimitKey(wildcard, "llama3"), "requests/1min#model=llama3"; got != want {
		t.Errorf("LimitKey(wildcard) = %q, want %q", got, want)
	}

	restricted := core.Limit{Metric: core.MetricRequests, EveryText: "1d", Models: []string{"premium-model"}}
	if got, want := LimitKey(restricted, "premium-model"), "requests/1d#model=premium-model"; got != want {
		t.Errorf("LimitKey(restricted) = %q, want %q", got, want)
	}
}

func TestPerModelPrefixAndExtractModel(t *testing.T) {
	l := core.Limit{Metric: core.MetricTokens, EveryText: "1mo", Models: []string{"*"}}
	if got, want := PerModelPrefix(l), "tokens/1mo#model="; got != want {
		t.Fatalf("PerModelPrefix = %q, want %q", got, want)
	}
	model, ok := ExtractModel(l, "tokens/1mo#model=heavy-model")
	if !ok || model != "heavy-model" {
		t.Fatalf("ExtractModel = (%q, %v), want (heavy-model, true)", model, ok)
	}
	if _, ok := ExtractModel(l, "tokens/1mo"); ok {
		t.Error("ExtractModel must reject a shared-shape key (no #model= prefix)")
	}
	if _, ok := ExtractModel(l, "requests/1d#model=heavy-model"); ok {
		t.Error("ExtractModel must reject a key belonging to a different metric/every")
	}

	shared := core.Limit{Metric: core.MetricTokens, EveryText: "1mo"}
	if _, ok := ExtractModel(shared, "tokens/1mo#model=heavy-model"); ok {
		t.Error("ExtractModel on a shared Limit must always report ok=false — a shared Limit has no per-model buckets")
	}

	restricted := core.Limit{Metric: core.MetricRequests, EveryText: "1d", Models: []string{"lite-a", "lite-b"}}
	if _, ok := ExtractModel(restricted, "requests/1d#model=heavy-model"); ok {
		t.Error("ExtractModel must reject a key whose model is outside restricted Scope")
	}
	if m, ok := ExtractModel(restricted, "requests/1d#model=lite-a"); !ok || m != "lite-a" {
		t.Fatalf("ExtractModel(in-scope) = (%q, %v), want (lite-a, true)", m, ok)
	}
}

func TestRegistry_Keys(t *testing.T) {
	r := NewRegistry("")
	if got := r.Keys("p1"); got != nil {
		t.Fatalf("Keys on an unknown provider = %v, want nil", got)
	}
	ps := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r.Charge("p1", "requests/1min#model=a", ps, Counters{Requests: 1}, 0)
	r.Charge("p1", "requests/1min#model=b", ps, Counters{Requests: 1}, 0)
	keys := r.Keys("p1")
	if len(keys) != 2 {
		t.Fatalf("Keys = %v, want 2 entries", keys)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	if !seen["requests/1min#model=a"] || !seen["requests/1min#model=b"] {
		t.Fatalf("Keys = %v, want both per-model buckets", keys)
	}
}
