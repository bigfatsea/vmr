// Ver 2026-09-06, by Claude
package dashboard

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestWriteSkeletons_BasicShape covers the core contract: dir created when
// missing, exactly the six pages written flat at the root, 0600/0700 modes,
// and non-empty contents.
func TestWriteSkeletons_BasicShape(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports", "nested") // dir itself doesn't exist yet
	if err := WriteSkeletons(dir); err != nil {
		t.Fatalf("WriteSkeletons: %v", err)
	}

	// Directory mode: 0700 or stricter (umask can only strip bits, never add).
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("dir mode = %v, want no group/other bits (0700 base)", fi.Mode().Perm())
	}

	for _, name := range skeletonPages {
		path := filepath.Join(dir, name)
		fi, err := os.Stat(path)
		if err != nil {
			t.Errorf("missing skeleton %s: %v", name, err)
			continue
		}
		if fi.IsDir() {
			t.Errorf("%s is a directory", name)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s mode = %v, want no group/other bits (0600 base)", name, fi.Mode().Perm())
		}
		if fi.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}

	// And nothing else at the root: flat layout is the #data= path contract.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = true
	}
	want := map[string]bool{}
	for _, n := range skeletonPages {
		want[n] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("root entries = %v, want exactly %v", got, want)
	}
}

// TestWriteSkeletons_Idempotent runs WriteSkeletons twice and requires the
// second pass to be byte-identical (§6.2: every analyze call refreshes the
// skeletons; a run that appends or corrupts would drift /reports/).
func TestWriteSkeletons_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSkeletons(dir); err != nil {
		t.Fatalf("first WriteSkeletons: %v", err)
	}
	first := map[string][]byte{}
	for _, name := range skeletonPages {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		first[name] = data
	}

	if err := WriteSkeletons(dir); err != nil {
		t.Fatalf("second WriteSkeletons: %v", err)
	}
	for _, name := range skeletonPages {
		second, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(first[name]) != string(second) {
			t.Errorf("%s changed between runs (not idempotent)", name)
		}
	}
}

// TestWriteSkeletons_OverwriteStale pins the "overwrite whatever was there"
// half of the contract: a user-modified or stale page must not survive a
// WriteSkeletons call — /reports/ pages are always the binary's own.
func TestWriteSkeletons_OverwriteStale(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "tool-waste.html")
	if err := os.WriteFile(stale, []byte("<html>user customized</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteSkeletons(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "<html>user customized</html>" {
		t.Error("stale user-edited page survived WriteSkeletons — overwrite is required")
	}
}

// TestWriteSkeletons_InlinesCommonRuntime is the regression guard for the
// bug where WriteSkeletons wrote only the .html files while every page
// referenced <script src="common.js">: /reports/ serves no .js, so every
// deployed dashboard 404'd on the shared runtime and rendered nothing.
// The written pages must carry the runtime inline and reference no sibling
// script.
func TestWriteSkeletons_InlinesCommonRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSkeletons(dir); err != nil {
		t.Fatalf("WriteSkeletons: %v", err)
	}
	for _, name := range skeletonPages {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		if contains(s, `src="common.js"`) {
			t.Errorf("%s still references src=\"common.js\" after write — the runtime was not inlined", name)
		}
		if !contains(s, "function versionBehavior") {
			t.Errorf("%s does not carry common.js's versionBehavior after write — the runtime is missing", name)
		}
	}
}

// TestAssetNames_MatchesSkeletonPages locks the embed FS against the
// hardcoded write list: every .html under assets/ must be in skeletonPages,
// so adding a page without wiring it in fails here instead of silently not
// shipping.
func TestAssetNames_MatchesSkeletonPages(t *testing.T) {
	names, err := AssetNames()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, want := range skeletonPages {
		if !got[want] {
			t.Errorf("skeletonPages lists %q but no assets/%s exists in the embed FS", want, want)
		}
		delete(got, want)
	}
	for extra := range got {
		t.Errorf("assets/%s exists in the embed FS but is missing from skeletonPages — it would never be written", extra)
	}
}

// TestSkeletonPages_NoExternalDependencies guards the zero-build-chain rule
// (§6.3): skeleton pages must reference nothing outside themselves beyond
// the report-root-relative slices — no CDN, no npm, no chart library.
func TestSkeletonPages_NoExternalDependencies(t *testing.T) {
	for _, name := range skeletonPages {
		data, err := assets.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		s := string(data)
		for _, bad := range []string{"http://", "https://", "src=\"http", "href=\"http", "@import", "require(", "import "} {
			if name == "common.js" {
				continue
			}
			// The SVG favicon data-URI embeds "http://www.w3.org" — the SVG
			// namespace declaration, not a network fetch. Everything else
			// must be clean.
			cleaned := replaceAll(s, "http://www.w3.org/2000/svg", "")
			if contains(cleaned, bad) {
				t.Errorf("%s contains external reference %q — pages must be self-contained", name, bad)
			}
		}
	}
}

// TestRequestBrowser_ReadsSnakeCaseFields is the regression guard for N15:
// request-browser.html was written against Go struct field names
// (r.ClientKey, r.DurMS, …) while requests/index.json emits snake_case json
// tags (client_key, dur_ms, …), so every data column rendered as a dash.
// The page must read the JSON's own keys.
func TestRequestBrowser_ReadsSnakeCaseFields(t *testing.T) {
	data, err := assets.ReadFile("assets/request-browser.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	// The PascalCase names that do not exist in requests/index.json.
	for _, bad := range []string{
		"r.ClientKey", "r.Model", "r.Endpoint", "r.Outcome", "r.DurMS",
		"r.TTFTMS", "r.TokensInFresh", "r.TokensInCached", "r.TokensOut",
		"r.CacheEff", "r.Fallbacks", "r.ErrorClass", "r.DetailFile",
		"r.Session", "r.Task", "r.Title", "sm.Title", "sm.Alias",
	} {
		if contains(s, bad) {
			t.Errorf("request-browser.html reads %q, but requests/index.json has no such key (snake_case json tags)", bad)
		}
	}
	// And it must read the real keys.
	for _, want := range []string{"r.client_key", "r.dur_ms", "r.tokens_in_fresh", "r.ts_display"} {
		if !contains(s, want) {
			t.Errorf("request-browser.html does not read %q", want)
		}
	}
}

// TestAllDashboardPages_ReadSnakeCaseFields is the complete regression guard for N15:
// All 6 dashboard pages must read their slices using the actual snake_case json tags
// instead of Go PascalCase struct field names.
func TestAllDashboardPages_ReadSnakeCaseFields(t *testing.T) {
	type pageCase struct {
		file string
		bad  []string
		want []string
	}

	cases := []pageCase{
		{
			file: "assets/macro-dashboard.html",
			bad: []string{
				"o.Requests", "o.Errors", "o.TokensIn", "o.TokensOut", "o.CostEstimate",
				"m.CostEstimate", "c.CostEstimate", "d.CostEstimate",
				"c.TokensIn", "c.TokensOut", "e.P50MS", "e.P90MS", "e.TTFTP50MS",
				"s.Turns", "s.turns", "cont.CacheEff", "sw.CacheEff",
			},
			want: []string{
				"o.requests", "o.errors", "o.tokens_in", "o.tokens_out", "o.cost_estimate",
				"m.tokens_in_fresh", "e.dur_ms_p50", "e.dur_ms_p95", "e.ttft_ms_p50",
				"s.requests", "cont.cache_efficiency", "sw.cache_efficiency",
			},
		},
		{
			file: "assets/tool-waste.html",
			bad: []string{
				"t.DeclaredCount", "t.CalledCount", "t.SchemaBytesShipped",
				"t.SchemaWasteBytes", "t.Signature", "ce.Tools",
			},
			want: []string{
				"t.schema_bytes_shipped", "t.schema_waste_bytes", "t.distinct_called",
				"t.declared", "ce.tools",
			},
		},
		{
			file: "assets/journey-viewer.html",
			bad: []string{
				"metrics.ModelMS", "metrics.AgentExecMS", "metrics.HumanIdleMS",
				"metrics.ToolCallCount", "metrics.DuplicateActionRate", "metrics.PlanExecRatio",
				"cost.Total", "cost.Currency", "f.Severity", "f.Title", "f.Message",
			},
			want: []string{
				"metrics.model_ms", "metrics.agent_exec_ms", "metrics.human_idle_ms",
				"metrics.tool_call_count", "metrics.duplicate_action_rate", "metrics.plan_exec_ratio",
				"cost.total", "f.finding", "s.ts_display",
			},
		},
		{
			file: "assets/benchmarks.html",
			bad: []string{
				"s.JourneyCount", "s.MetricDist", "s.FindingRate", "s.Correlations",
				"s.ProtocolShare", "c.MetricA", "c.MetricB", "c.Rho",
			},
			want: []string{
				"s.journey_count", "s.metric_distributions", "s.finding_rates",
				"s.correlations", "s.protocol_share", "c.metric_a", "c.metric_b", "c.rho",
			},
		},
		{
			file: "assets/journey-compare.html",
			bad: []string{
				"c.ID", "c.ARef", "c.BRef", "cmp.Rows", "cmp.Tools",
				"t.ACalls", "t.BCalls", "aRef.Steps", "aRef.ToolCalls",
			},
			want: []string{
				"c.filename", "c.a_journey", "c.b_journey", "cmp.rows",
				"cmp.tools", "t.a_calls", "t.b_calls", "aRef.steps", "aRef.tool_calls",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			data, err := assets.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			s := string(data)
			for _, bad := range tc.bad {
				if contains(s, bad) {
					t.Errorf("%s reads %q, but slices emit snake_case keys", tc.file, bad)
				}
			}
			for _, want := range tc.want {
				if !contains(s, want) {
					t.Errorf("%s does not read expected key %q", tc.file, want)
				}
			}
		})
	}
}

func replaceAll(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func contains(s, sub string) bool {
	return indexOf(s, sub) >= 0
}
