// Ver 2026-09-23 02:35, by Claude Opus 5.5
package server

// This file actually EXECUTES the Overview/Models/Stats pages' embedded JS
// (via a subprocess `node`) against a realistic /status fixture and a REAL
// statistics backend (an httptest server over a populated stats.Store, so
// the /stats/* JSON the pages parse is the engine's own output),
// instead of only pattern-matching the HTML source like the rest of this
// package's tests do. It exists because a whole class of bug — a runtime
// JS error thrown while rendering — is invisible to string-matching tests:
// refreshAll()/livePollTick() each wrap their own render calls in a single
// try/catch, so a thrown error aborts every render call after the one that
// threw and is then silently swallowed (just a toast + a paused pill), not
// surfaced as an uncaught exception. The regression this guards against:
// renderModels() in status.html referenced a `const fullTitle` several
// lines before its declaration (a temporal-dead-zone ReferenceError),
// which fired on every real deployment (every endpoint carries
// capabilities/max_context_tokens) and silently broke Virtual Models &
// Endpoint Topology, Performance, Traffic and Usage — everything rendered
// after renderModels() in refreshAll()'s sequence.
//
// Node is treated as a test-only tool, not a build dependency: both
// GitHub Actions runners (ubuntu-latest/macos-latest) carry it by default,
// and a local run without `node` on PATH just skips.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vmr/internal/stats"
)

func requireNode(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH — skipping JS execution regression test")
	}
	return path
}

func readSourceFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(b)
}

// extractBetween returns the substring strictly between the first
// occurrence of startMarker and the following occurrence of endMarker,
// marker-bounded (not line-numbered) so it survives unrelated edits above
// or below the slice.
func extractBetween(t *testing.T, src, startMarker, endMarker string) string {
	t.Helper()
	si := strings.Index(src, startMarker)
	if si < 0 {
		t.Fatalf("start marker %q not found", startMarker)
	}
	rest := src[si:]
	ei := strings.Index(rest, endMarker)
	if ei < 0 {
		t.Fatalf("end marker %q not found after start marker", endMarker)
	}
	return rest[:ei]
}

// extractScriptBody returns the content of the page's single <script>...
// </script> block (the page logic, including the {{CONSOLE_JS}} injection
// comment left verbatim — harmless, it is just a JS comment).
func extractScriptBody(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, "<script>")
	if start < 0 {
		t.Fatalf("<script> not found")
	}
	start += len("<script>")
	end := strings.LastIndex(html, "</script>")
	if end < 0 || end < start {
		t.Fatalf("</script> not found")
	}
	return html[start:end]
}

// consoleStubsJS stubs every console.js export the page scripts touch that
// is NOT a pure formatting helper (those are extracted verbatim from the
// real console.js instead — see formattingSliceJS): DOM, storage, fetch,
// auth, toasts and the mountConsole chrome. Kept deliberately dumb — it
// exists to let the page's own render functions run and be observed, not
// to reimplement the browser.
const consoleStubsJS = `
'use strict';
class FakeEl {
  constructor(tag) {
    this.tagName = (tag || 'div').toUpperCase();
    this._html = '';
    this.textContent = '';
    this.className = '';
    this.style = {};
    this.dataset = {};
    this.children = [];
    this.hidden = false;
    this.value = '';
  }
  get innerHTML() {
    if (this.children.length > 0) {
      return this.children.map(c => {
        const tag = c.tagName.toLowerCase();
        return '<' + tag + '>' + (c.innerHTML || c.textContent || '') + '</' + tag + '>';
      }).join('');
    }
    return this._html;
  }
  set innerHTML(v) { this.children = []; this._html = String(v); }
  addEventListener() {}
  removeEventListener() {}
  appendChild(el) {
    const idx = this.children.indexOf(el);
    if (idx >= 0) this.children.splice(idx, 1);
    this.children.push(el);
    el.parentNode = this;
    return el;
  }
  prepend(el) {
    const idx = this.children.indexOf(el);
    if (idx >= 0) this.children.splice(idx, 1);
    this.children.unshift(el);
    el.parentNode = this;
    return el;
  }
  remove() {
    if (this.parentNode && this.parentNode.children) {
      const idx = this.parentNode.children.indexOf(this);
      if (idx >= 0) this.parentNode.children.splice(idx, 1);
    }
    this.parentNode = null;
  }
  get classList() {
    const self = this;
    const list = () => self.className.split(/\s+/).filter(Boolean);
    return {
      add(c) { if (!list().includes(c)) self.className = (self.className + ' ' + c).trim(); },
      remove(c) { self.className = list().filter(x => x !== c).join(' '); },
      toggle(c, force) {
        const has = list().includes(c);
        const want = force === undefined ? !has : force;
        if (want && !has) self.className = (self.className + ' ' + c).trim();
        if (!want && has) self.className = list().filter(x => x !== c).join(' ');
        return want;
      },
      contains(c) { return list().includes(c); },
    };
  }
  querySelector() { return null; }
  querySelectorAll() { return []; }
  setAttribute() {}
  getAttribute() { return null; }
  focus() {}
  getBoundingClientRect() { return { top: 0, left: 0, width: 0, height: 0 }; }
}

const elRegistry = new Map();
const document = {
  body: new FakeEl('body'),
  hidden: false,
  activeElement: null,
  getElementById(id) {
    if (!elRegistry.has(id)) elRegistry.set(id, new FakeEl('div'));
    return elRegistry.get(id);
  },
  querySelector() { return new FakeEl('div'); },
  querySelectorAll() { return []; },
  createElement(tag) { return new FakeEl(tag); },
  addEventListener() {},
  removeEventListener() {},
};

const localStorageStore = {};
const localStorage = {
  getItem(k) { return Object.prototype.hasOwnProperty.call(localStorageStore, k) ? localStorageStore[k] : null; },
  setItem(k, v) { localStorageStore[k] = String(v); },
  removeItem(k) { delete localStorageStore[k]; },
};

const VMRAuth = {
  get() { return ''; },
  has() { return false; },
  async guard(doFetch) { return doFetch(); },
};

const requestAnimationFrame = typeof globalThis.requestAnimationFrame === 'function'
  ? globalThis.requestAnimationFrame
  : fn => setTimeout(() => fn(Date.now()), 16);
const cancelAnimationFrame = typeof globalThis.cancelAnimationFrame === 'function'
  ? globalThis.cancelAnimationFrame
  : id => clearTimeout(id);

// toast() only ever fires from a caught-error path (refreshAll's / the
// range-switch handler's own catch) — routing it to stderr turns a
// silently-swallowed runtime error into a loud, specific test failure
// instead of a bare "expected content missing" diff.
function toast(msg) { console.error('TOAST: ' + msg); }
const ConsoleAlerts = { set() {} };

function mountConsole() {
  return {
    onRefresh() {},
    refreshNow() {},
    setPaused() {},
    setUptime() {},
    setRailCount() {},
    setStreamState() {},
    setTermStatus() {},
    setFooterIdentity() {},
  };
}

const fetch = async (url, opts) => {
  const u = String(url);
  if (u.startsWith('/status/live')) return { ok: true, status: 200, json: async () => FIXTURE_LIVE };
  if (u.startsWith('/status')) return { ok: true, status: 200, json: async () => FIXTURE_STATUS };
  if (u.startsWith('/stats/')) return globalThis.fetch(STATS_BASE + u, opts);
  return { ok: false, status: 404, json: async () => ({}) };
};

// uPlot stand-in: records what each chart was asked to draw.
const uplots = [];
class uPlot {
  constructor(opts, data) { uplots.push({ series: opts.series.slice(1).map(x => x.label), points: data[0].length }); this.height = opts.height; }
  destroy() {}
  setSize() {}
}
const window = { addEventListener() {} };
const location = { hash: '' };
const history = { replaceState(_a, _b, h) { location.hash = h; } };

process.on('unhandledRejection', e => {
  console.error('UNHANDLED_REJECTION: ' + (e && e.stack || e));
  process.exitCode = 1;
});
`

// buildHarness assembles one self-contained Node script: fixtures, DOM/
// fetch/auth stubs, the real console.js formatting helpers, the real page
// script (unmodified — including its own auto-run bootstrap), then the
// caller's tail (which drives the render pass under test and prints a JSON
// result to stdout).
func buildHarness(t *testing.T, statusFixture, liveFixture any, statsBase, formattingJS, pageJS, tailJS string) string {
	t.Helper()
	statusJSON, err := json.Marshal(statusFixture)
	if err != nil {
		t.Fatalf("marshal status fixture: %v", err)
	}
	liveJSON, err := json.Marshal(liveFixture)
	if err != nil {
		t.Fatalf("marshal live fixture: %v", err)
	}
	baseJSON, _ := json.Marshal(statsBase)

	var b strings.Builder
	b.WriteString("const FIXTURE_STATUS = ")
	b.Write(statusJSON)
	b.WriteString(";\nconst FIXTURE_LIVE = ")
	b.Write(liveJSON)
	b.WriteString(";\nconst STATS_BASE = ")
	b.Write(baseJSON)
	b.WriteString(";\n")
	b.WriteString(consoleStubsJS)
	b.WriteString("\n// ---- console.js formatting helpers (verbatim slice) ----\n")
	b.WriteString(formattingJS)
	b.WriteString("\n// ---- stats.js (verbatim) ----\n")
	b.WriteString(readSourceFile(t, "../stats/web/stats.js"))
	b.WriteString("\n// ---- dashboard.js (verbatim) ----\n")
	b.WriteString(readSourceFile(t, "../stats/web/dashboard.js"))
	b.WriteString("\n// ---- page script (verbatim) ----\n")
	b.WriteString(pageJS)
	b.WriteString("\n// ---- test harness tail ----\n")
	b.WriteString(tailJS)
	return b.String()
}

// runNode executes src with node, returning stdout. Any stderr output or a
// non-zero exit fails the test immediately with the full stderr attached —
// that is where a thrown/rejected JS error from our own harness plumbing
// (as opposed to one the page code catches internally) would show up.
func runNode(t *testing.T, nodeBin, src string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "harness.js")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing harness: %v", err)
	}
	cmd := exec.Command(nodeBin, path)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("node harness failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}
	if stderr.Len() > 0 {
		t.Fatalf("node harness wrote to stderr (unexpected):\n%s", stderr.String())
	}
	return []byte(stdout.String())
}

// ---- fixtures ----
//
// The /status fixture is modeled on a real production capture (a vmr instance
// with two accounts routing to glm-5.3-flash): every endpoint carries
// capabilities + max_context_tokens, which is exactly the shape that made
// the fullTitle TDZ bug fire on every single render.

func statusFixture(now time.Time) map[string]any {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return map[string]any{
		"instance": map[string]any{
			"version": "v1.2.3-test", "pid": 4242, "uptime": "2h04m", "uptime_seconds": 7440,
			"go_version": "go1.23", "os_arch": "darwin/arm64", "listen": "0.0.0.0:8800",
			"concurrency": map[string]any{"limit": 8, "in_flight": 1, "waiting": 0},
		},
		"system": map[string]any{
			"memory":     map[string]any{"heap_alloc": "12.3 MB", "sys": "40.1 MB", "heap_alloc_bytes": 12_900_000, "sys_bytes": 42_000_000},
			"goroutines": 37,
			"disk":       map[string]any{"free_space": "108.4 GB", "free_space_bytes": 108_400_000_000},
		},
		"traffic": map[string]any{
			"sticky": map[string]any{"entries": 3},
		},
		"current_time": now.Format(time.RFC3339),
		"models": []map[string]any{
			{
				"id": "agent", "protocol": "anthropic",
				"capabilities": []string{"text", "tools", "thinking"}, "max_context_tokens": 512000,
				"endpoints": []map[string]any{
					{
						"endpoint": "anthropic:bai_free:glm-5.3-flash", "protocol": "anthropic", "priority": 1,
						"provider": "bai_free", "key_label": "key_1", "model": "glm-5.3-flash", "from_fallback": false,
						"consecutive_failures": 0, "available": true, "headroom": 1.4,
						"capabilities": []string{"text", "tools", "thinking"}, "max_context_tokens": 512000,
					},
					{
						"endpoint": "anthropic:bai_free:glm-5.3-flash-fb", "protocol": "anthropic", "priority": 2,
						"provider": "bai_free", "key_label": "key_4", "model": "glm-5.3-flash", "from_fallback": true,
						"consecutive_failures": 0, "available": true, "headroom": 1.4,
						"capabilities": []string{"text", "tools", "thinking"}, "max_context_tokens": 512000,
					},
				},
			},
		},
		"quota": []map[string]any{
			{
				"provider": "volc_token_plan", "metric": "tokens", "every": "1mo", "role": "bucket",
				"amount": 1_000_000_000, "used": 1_045_500_000, "pct": 104.55, "headroom": 0,
				"period_start": today.Format(time.RFC3339), "period_ends_at": today.AddDate(0, 1, 0).Format(time.RFC3339),
				"estimated_pct": 0,
			},
		},
		"alerts": []map[string]any{
			{"severity": "error", "kind": "quota", "message": "account volc_token_plan: tokens/1mo 104.55% used, exhausted", "ref": "volc_token_plan"},
		},
		"audit":       map[string]any{"enabled": true, "total_size": "1.1 GB", "total_size_bytes": 1_100_000_000, "retention_days": 30},
		"image_cache": map[string]any{"enabled": true, "size": "40 MB", "size_bytes": 40_000_000},
	}
}

func liveFixture(now time.Time) map[string]any {
	return map[string]any{
		"concurrency": map[string]any{"limit": 8, "in_flight": 1, "waiting": 0},
		"inflight": []map[string]any{
			{
				"seq": 1, "state": "running", "protocol": "anthropic", "vmodel": "agent", "stream": true,
				"client_key_tag": "cli-default", "addr": "127.0.0.1", "ts": now.Add(-3 * time.Second).Format(time.RFC3339),
				"sent_at": now.Add(-2 * time.Second).Format(time.RFC3339), "attempt": 1,
				"provider": "bai_free", "model": "glm-5.3-flash", "key_label": "key_1",
				"first_byte_at": now.Add(-1 * time.Second).Format(time.RFC3339),
				"last_byte_at":  now.Format(time.RFC3339),
				"est_in":        800, "est_out": 120,
			},
		},
		"recently_ended": []map[string]any{},
	}
}

// statsBackend serves the real /stats/* handlers over a store holding three
// requests from the last few minutes: a successful stream, an upstream
// failure after failover, and a request no endpoint could take.
func statsBackend(t *testing.T, now time.Time) string {
	t.Helper()
	st, err := stats.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	base := stats.Event{TS: now.Add(-5 * time.Minute), VModel: "agent", Protocol: "anthropic", Stream: true, ClientKeyTag: "cli-default"}
	ok := base
	ok.Outcome, ok.Status, ok.Attempts, ok.Forwarded = stats.OutcomeOK, 200, 1, true
	ok.Provider, ok.Model, ok.KeyLabel = "bai_free", "glm-5.3-flash", "key_1"
	ok.DurMS, ok.TTFTMS = 1000, 311
	ok.Tokens = stats.Tokens{In: 120000, Out: 60000, CacheRead: 30000, CacheWrite: 10000}
	failed := base
	failed.Outcome, failed.Status, failed.Attempts, failed.ErrorClass = stats.OutcomeError, 503, 2, "upstream_5xx"
	failed.Provider, failed.Model, failed.KeyLabel, failed.DurMS = "bai_free", "glm-5.3-flash", "key_1", 1450
	unrouted := base
	unrouted.Outcome, unrouted.Status, unrouted.ErrorClass = stats.OutcomeError, 503, "no_candidate"
	for _, e := range []stats.Event{ok, failed, unrouted} {
		st.Record(e)
	}
	h := stats.NewHandler(st)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats/schema", h.Schema)
	mux.HandleFunc("GET /stats/query", h.Query)
	mux.HandleFunc("GET /stats/events", h.Events)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func formattingSliceJS(t *testing.T) string {
	consoleJS := readSourceFile(t, "assets/console.js")
	return extractBetween(t, consoleJS, "/* ===================== formatting (design", "/* ============ overlay plumbing: one behaviour")
}

// TestConsoleRender_OverviewNoRuntimeError actually runs refreshAll() from
// status.html against a realistic /status fixture and the real statistics
// backend, and asserts every dashboard panel it is supposed to populate
// actually did — then flips the dashboard's own state (breakdown tab,
// token view, a 7d range) and re-renders, the way its controls do.
// Regression guard for the fullTitle TDZ crash (see file doc comment): one
// thrown render error leaves every later panel empty behind a swallowed
// toast, and this test fails loudly on exactly that shape of bug.
func TestConsoleRender_OverviewNoRuntimeError(t *testing.T) {
	node := requireNode(t)
	now := time.Now()

	pageJS := extractScriptBody(t, readSourceFile(t, "status.html"))
	tail := `
const html = id => document.getElementById(id).innerHTML;
(async () => {
  await refreshAll();
  await new Promise(res => setTimeout(res, 80));
  const out = {
    liveBody: html('live-body'), liveSum: document.getElementById('live-lines').innerHTML, pips: document.getElementById('v-pips').innerHTML,
    ctl: html('dash-ctl'), kpis: html('dash-kpis'), activity: html('dash-activity'), comp: html('dash-comp'),
    breakdown: html('dash-breakdown'), speed: html('dash-speed'), rel: html('dash-rel'), heat: html('dash-heat'),
  };
  Object.assign(dash.state, { split: 'outcome' });
  await dash.refresh();
  out.actStatus = html('dash-activity');
  Object.assign(dash.state, { bd: 'endpoint', act: 'tok', split: 'type', range: '7d' });
  await dash.refresh();
  Object.assign(out, { bdEndpoint: html('dash-breakdown'), actTok: html('dash-activity'), heat7d: html('dash-heat') });
  process.stdout.write(JSON.stringify(out));
  process.exit(0);
})().catch(e => { console.error('HARNESS_ERROR: ' + (e && e.stack || e)); process.exit(1); });
`
	src := buildHarness(t, statusFixture(now), liveFixture(now), statsBackend(t, now), formattingSliceJS(t), pageJS, tail)
	out := runNode(t, node, src)

	var res map[string]string
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("harness stdout not JSON: %v\nraw: %s", err, out)
	}

	checks := []struct{ field, want string }{
		{"liveBody", "glm-5.3-flash"},
		{"liveBody", "bai_free:<span"},
		{"liveSum", "cli-default"},
		{"liveSum", "#1"},
		{"pips", `class="pip on"`},
		{"ctl", `data-k="1d"`},
		{"ctl", "3 requests"},
		{"kpis", `<div class="d-kpi-v">3</div>`},
		{"kpis", "failing"}, // 2 of 3 requests failed
		{"kpis", "Latency · TTFT p50"},
		{"activity", "<rect"},
		{"activity", "faults"},
		{"activity", `data-legend="cli-default"`}, // the default split is by caller
		{"actStatus", `data-legend="error"`},
		{"comp", "Where tokens go"},
		{"comp", "100% <em>of requests</em>"}, // every fixture request streams
		{"breakdown", "cli-default"},
		{"breakdown", "Err / Cancel"},
		{"speed", "311ms"},
		{"rel", "upstream_5xx"},
		{"rel", "no_candidate"},
		{"rel", "failovers rescued"},
		{"heat", "Shown for the 7d and 30d"},
		{"bdEndpoint", "bai_free:<span"},
		{"bdEndpoint", "(unrouted)"},
		{"bdEndpoint", "Duration p50 / p90"},
		{"actTok", "cache read hidden"},
		{"heat7d", `class="d-heat"`},
	}
	for _, c := range checks {
		if !strings.Contains(res[c.field], c.want) {
			t.Errorf("%s: missing %q\ngot: %s", c.field, c.want, res[c.field])
		}
	}
	for _, field := range []string{"liveBody", "bdEndpoint"} {
		if strings.Contains(res[field], "bai_free : ") {
			t.Errorf("%s should not contain space in provider:model value, got: %s", field, res[field])
		}
	}
}

// TestConsoleRender_DashboardHelpers pins dashboard.js's judgment helpers:
// volume changes stay neutral, quality changes take a side, a change inside
// the noise band reads flat, and cancellations piling up at a whole 30 s
// mark read as a caller timeout.
func TestConsoleRender_DashboardHelpers(t *testing.T) {
	node := requireNode(t)
	tail := `
const D = VMRDash;
process.stdout.write(JSON.stringify({
  volumeDown: D.delta(70, 100, 'volume'),
  latencyUp: D.delta(130, 100, 'down'),
  noise: D.delta(102, 100, 'down'),
  ppUp: D.delta(0.92, 0.91, 'up', { pp: true }),
  clusters: D.timeoutClusters([60001, 60400, 60999, 90001, 90002, 90500, 4000, 30001, 61000]),
  hist: D.cancelHistogram([1000, 6000, 60001, 200000]),
  state: D.loadState('#range=7d&where=client%3Apimini&where=outcome%3Aok&bd=bogus'),
  step: D.nextStep({ steps: [{ name: '15m' }, { name: '1h' }, { name: '6h' }] }, '1h'),
}));
`
	src := buildHarness(t, map[string]any{}, map[string]any{}, "", formattingSliceJS(t), "", tail)
	var res struct {
		VolumeDown, LatencyUp, Noise, PPUp string
		Clusters                           []struct{ Sec, N int }
		Hist                               [][2]any
		State                              struct {
			Range, Bd string
			Where     []string
		}
		Step string
	}
	if err := json.Unmarshal(runNode(t, node, src), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.VolumeDown, "neutral") || !strings.Contains(res.LatencyUp, "bad") ||
		!strings.Contains(res.Noise, "flat") || !strings.Contains(res.PPUp, "good") {
		t.Errorf("delta classes: volume %q latency %q noise %q pp %q", res.VolumeDown, res.LatencyUp, res.Noise, res.PPUp)
	}
	if len(res.Clusters) != 2 || res.Clusters[0].Sec != 60 || res.Clusters[0].N != 3 || res.Clusters[1].Sec != 90 {
		t.Errorf("timeout clusters = %+v, want 60 s ×3 and 90 s ×3 (61 s and a lone 30 s do not count)", res.Clusters)
	}
	if len(res.Hist) != 6 || res.Hist[0][1] != float64(1) || res.Hist[4][1] != float64(1) || res.Hist[5][1] != float64(1) {
		t.Errorf("histogram = %v", res.Hist)
	}
	if res.State.Range != "7d" || res.State.Bd != "client" || len(res.State.Where) != 1 || res.State.Where[0] != "client:pimini" {
		t.Errorf("state = %+v: unknown tabs fall back, only the four filter dimensions survive", res.State)
	}
	if res.Step != "6h" {
		t.Errorf("next step after 1h = %q", res.Step)
	}
}

// TestConsoleRender_ModelsPageNoRuntimeError runs refreshAll() from
// models.html (Quota Budgets + Virtual Models & Endpoint Topology, moved
// there from Overview) against the same fixture shape and asserts both
// tables populate — same TDZ-class regression guard as the Overview test.
func TestConsoleRender_ModelsPageNoRuntimeError(t *testing.T) {
	node := requireNode(t)
	now := time.Now()

	pageJS := extractScriptBody(t, readSourceFile(t, "models.html"))
	tail := `
(async () => {
  await refreshAll();
  await new Promise(res => setTimeout(res, 80));
  process.stdout.write(JSON.stringify({
    modelsBody: document.getElementById('models-body').innerHTML,
    quotaBody: document.getElementById('quota-body').innerHTML,
  }));
  process.exit(0);
})().catch(e => { console.error('HARNESS_ERROR: ' + (e && e.stack || e)); process.exit(1); });
`
	src := buildHarness(t, statusFixture(now), liveFixture(now), statsBackend(t, now), formattingSliceJS(t), pageJS, tail)
	out := runNode(t, node, src)

	var res map[string]string
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("harness stdout not JSON: %v\nraw: %s", err, out)
	}

	checks := []struct{ field, want string }{
		{"modelsBody", "2 · 66.67%"}, // both routed requests landed on the endpoint, out of the virtual model's 3 (one never routed)
		{"modelsBody", "Anthropic Messages (/v1/messages)"},
		{"modelsBody", "<th>Name</th>"},
		{"modelsBody", "<th>Provider &amp; Model</th>"},
		{"modelsBody", `<span class="t-strong">agent</span>`},
		{"modelsBody", "bai_free:<span"},
		{"modelsBody", "glm-5.3-flash"},
		{"modelsBody", "512K"},
		{"modelsBody", "text/tools/thinking"},
		{"quotaBody", "volc_token_plan"},
	}
	for _, c := range checks {
		if !strings.Contains(res[c.field], c.want) {
			t.Errorf("%s: missing %q\ngot: %s", c.field, c.want, res[c.field])
		}
	}
	if strings.Contains(res["modelsBody"], "anthropic:") {
		t.Errorf("modelsBody should not contain protocol prefix in virtual model cell, got: %s", res["modelsBody"])
	}
	if strings.Contains(res["modelsBody"], "bai_free : ") {
		t.Errorf("modelsBody should not contain space in provider:model value, got: %s", res["modelsBody"])
	}
}

// TestConsoleRender_LiveSlotsLifecycle verifies the slot list lifecycle in
// status.html against a series of simulated poll cycles: initial empty state,
// active request arrival, token streaming updates, completion freeze from
// recently_ended[], capacity eviction when requests exceed the slot budget
// (ensuring evicted DOM nodes are removed from live-body), and fallback freeze.
func TestConsoleRender_LiveSlotsLifecycle(t *testing.T) {
	node := requireNode(t)
	now := time.Now()

	pageJS := extractScriptBody(t, readSourceFile(t, "status.html"))
	tail := `
(async () => {
  const liveBody = document.getElementById('live-body');
  const runningBadge = document.getElementById('live-running');
  const queuedBadge = document.getElementById('live-queued');
  const now = new Date().toISOString();
  const nowPlus5s = new Date(Date.now() + 5000).toISOString();

  // 1. Initial idle poll: empty row rendered, 0 running, queued hidden
  renderLive([], [], { limit: 6, waiting: 0 });
  const idleChildCount = liveBody.children.length;
  const idleHasEmpty = liveBody.innerHTML.includes('No requests in flight right now');
  const idleRunning = runningBadge.textContent;
  const idleQueuedHidden = queuedBadge.hidden;

  // 2. Active request arrives: row created, empty row removed, 1 running, 2 queued
  const req1 = {
    seq: 1, state: 'running', protocol: 'anthropic', vmodel: 'coding',
    stream: true, client_key_tag: 'test-cli', addr: '127.0.0.1',
    ts: now, sent_at: now, attempt: 1, provider: 'p1', model: 'm1',
    first_byte_at: now, last_byte_at: now, est_in: 100, est_out: 50
  };
  renderLive([req1], [], { limit: 6, waiting: 2 });
  const liveChildCount = liveBody.children.length;
  const liveRunning = runningBadge.textContent;
  const liveQueuedText = queuedBadge.textContent;
  const liveQueuedHidden = queuedBadge.hidden;
  const row1Seq = liveBody.children[0].dataset.seq;
  const row1SeqCell = String(liveBody.children[0].children[0].textContent);
  const row1NotGrayed = !liveBody.children[0].classList.contains('ended');

  // 3. Streaming progress on req1 (est_out 50 -> 120): persists same tr
  const trBefore = liveBody.children[0];
  req1.est_out = 120;
  renderLive([req1], [], { limit: 6, waiting: 0 });
  const sameNode = (liveBody.children[0] === trBefore);

  // 4. req1 ends: appears in recently_ended, row frozen with ended badge
  const ended1 = Object.assign({}, req1, { state: 'ended', ended_at: nowPlus5s });
  renderLive([], [ended1], { limit: 6, waiting: 0 });
  const endedRunning = runningBadge.textContent;
  const endedRowHTML = liveBody.children[0].innerHTML;
  const hasEndedBadge = endedRowHTML.includes('ended');
  const endedRowGrayed = liveBody.children[0].classList.contains('ended');
  const endedRowSeqCell = String(liveBody.children[0].children[0].textContent);

  // 5. Overflow capacity: push requests 2..11 (total 11 requests).
  // With limit: 6, liveSlotCount is 8. Exactly 8 rows must remain in the DOM,
  // with newest seq (11) at top and lowest surviving seq (4) at bottom;
  // seqs 1, 2, 3 must be completely removed from liveBody.children.
  const manyInflight = [];
  for (let s = 2; s <= 11; s++) {
    manyInflight.push({
      seq: s, state: 'running', protocol: 'anthropic', vmodel: 'coding',
      stream: true, client_key_tag: 'test-cli', addr: '127.0.0.1',
      ts: now, sent_at: now, attempt: 1, provider: 'p1', model: 'm1',
      first_byte_at: now, last_byte_at: now, est_in: 100, est_out: 10
    });
  }
  renderLive(manyInflight, [], { limit: 6, waiting: 0 });
  const slotCount = liveBody.children.length;
  const topSeq = liveBody.children[0].dataset.seq;
  const bottomSeq = liveBody.children[liveBody.children.length - 1].dataset.seq;
  const presentSeqs = Array.from(liveBody.children).map(c => Number(c.dataset.seq));

  // 6. Fallback freeze: req12 appears live, then is absent for 2 polls
  const req12 = {
    seq: 12, state: 'running', protocol: 'anthropic', vmodel: 'coding',
    stream: true, client_key_tag: 'test-cli', addr: '127.0.0.1',
    ts: now, sent_at: now, attempt: 1, provider: 'p1', model: 'm1',
    first_byte_at: now, last_byte_at: now, est_in: 100, est_out: 10
  };
  renderLive([req12], [], { limit: 6, waiting: 0 });
  const row12LiveBadge = liveBody.children[0].innerHTML.includes('running');
  // First missed poll: still running/not ended
  renderLive([], [], { limit: 6, waiting: 0 });
  const row12Miss1Badge = liveBody.children[0].innerHTML.includes('running');
  // Second missed poll: fallback freeze fires, badge becomes ended
  renderLive([], [], { limit: 6, waiting: 0 });
  const row12Miss2Ended = liveBody.children[0].innerHTML.includes('ended');

  process.stdout.write(JSON.stringify({
    idleChildCount,
    idleHasEmpty,
    idleRunning,
    idleQueuedHidden,
    liveChildCount,
    liveRunning,
    liveQueuedText,
    liveQueuedHidden,
    row1Seq,
    row1SeqCell,
    row1NotGrayed,
    sameNode,
    endedRunning,
    hasEndedBadge,
    endedRowGrayed,
    endedRowSeqCell,
    slotCount,
    topSeq,
    bottomSeq,
    presentSeqs,
    row12LiveBadge,
    row12Miss1Badge,
    row12Miss2Ended,
  }));
  process.exit(0);
})().catch(e => { console.error('HARNESS_ERROR: ' + (e && e.stack || e)); process.exit(1); });
`
	src := buildHarness(t, statusFixture(now), liveFixture(now), "http://127.0.0.1:9", formattingSliceJS(t), pageJS, tail)
	out := runNode(t, node, src)

	var res struct {
		IdleChildCount   int    `json:"idleChildCount"`
		IdleHasEmpty     bool   `json:"idleHasEmpty"`
		IdleRunning      string `json:"idleRunning"`
		IdleQueuedHidden bool   `json:"idleQueuedHidden"`
		LiveChildCount   int    `json:"liveChildCount"`
		LiveRunning      string `json:"liveRunning"`
		LiveQueuedText   string `json:"liveQueuedText"`
		LiveQueuedHidden bool   `json:"liveQueuedHidden"`
		Row1Seq          string `json:"row1Seq"`
		Row1SeqCell      string `json:"row1SeqCell"`
		Row1NotGrayed    bool   `json:"row1NotGrayed"`
		SameNode         bool   `json:"sameNode"`
		EndedRunning     string `json:"endedRunning"`
		HasEndedBadge    bool   `json:"hasEndedBadge"`
		EndedRowGrayed   bool   `json:"endedRowGrayed"`
		EndedRowSeqCell  string `json:"endedRowSeqCell"`
		SlotCount        int    `json:"slotCount"`
		TopSeq           string `json:"topSeq"`
		BottomSeq        string `json:"bottomSeq"`
		PresentSeqs      []int  `json:"presentSeqs"`
		Row12LiveBadge   bool   `json:"row12LiveBadge"`
		Row12Miss1Badge  bool   `json:"row12Miss1Badge"`
		Row12Miss2Ended  bool   `json:"row12Miss2Ended"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("harness stdout not JSON: %v\nraw: %s", err, out)
	}

	if !res.IdleHasEmpty || res.IdleRunning != "0 running" || !res.IdleQueuedHidden {
		t.Errorf("idle state mismatch: %+v", res)
	}
	if res.LiveChildCount != 1 || res.LiveRunning != "1 running" || res.LiveQueuedHidden || res.LiveQueuedText != "2 in queue" {
		t.Errorf("live state mismatch: %+v", res)
	}
	if res.Row1Seq != "1" || !res.SameNode {
		t.Errorf("persistent node mismatch: seq=%s same=%v", res.Row1Seq, res.SameNode)
	}
	if res.Row1SeqCell != "1" || !res.Row1NotGrayed {
		t.Errorf("live row seq cell = %q, grayed=%v, want seq in first cell and no ended class", res.Row1SeqCell, !res.Row1NotGrayed)
	}
	if res.EndedRunning != "0 running" || !res.HasEndedBadge {
		t.Errorf("ended row mismatch: running=%s hasEnded=%v", res.EndedRunning, res.HasEndedBadge)
	}
	if !res.EndedRowGrayed || res.EndedRowSeqCell != "1" {
		t.Errorf("ended row grayed=%v seqCell=%q, want grayed with seq retained", res.EndedRowGrayed, res.EndedRowSeqCell)
	}
	if res.SlotCount != 8 {
		t.Errorf("slot capacity cap = %d, want 8 (clamped to limit 6 + 2)", res.SlotCount)
	}
	if res.TopSeq != "11" || res.BottomSeq != "4" {
		t.Errorf("slot ordering mismatch: top=%s bottom=%s, want 11/4", res.TopSeq, res.BottomSeq)
	}
	wantSeqs := []int{11, 10, 9, 8, 7, 6, 5, 4}
	if len(res.PresentSeqs) != len(wantSeqs) {
		t.Fatalf("present seqs count = %d, want %d: %v", len(res.PresentSeqs), len(wantSeqs), res.PresentSeqs)
	}
	for i, s := range wantSeqs {
		if res.PresentSeqs[i] != s {
			t.Errorf("presentSeqs[%d] = %d, want %d (all: %v)", i, res.PresentSeqs[i], s, res.PresentSeqs)
		}
	}
	if !res.Row12LiveBadge || !res.Row12Miss1Badge || !res.Row12Miss2Ended {
		t.Errorf("fallback freeze mismatch: live=%v miss1=%v miss2Ended=%v",
			res.Row12LiveBadge, res.Row12Miss1Badge, res.Row12Miss2Ended)
	}
}

// TestConsoleRender_StatsPage runs the statistics page against the real
// backend: the default view renders a table and a chart, switching to the
// Performance view regroups by endpoint with success-only speed metrics,
// and a drill-down lists the requests behind a row.
func TestConsoleRender_StatsPage(t *testing.T) {
	node := requireNode(t)
	now := time.Now()

	pageJS := extractScriptBody(t, readSourceFile(t, "../stats/web/stats.html"))
	tail := `
(async () => {
  await refreshAll();
  const traffic = document.getElementById('table').innerHTML;
  const views = document.getElementById('views').innerHTML;
  setState(viewState('performance'), { keepView: true });
  await new Promise(res => setTimeout(res, 150));
  const perf = document.getElementById('table').innerHTML;
  // Drill exactly the way a click does: through the rendered data-group
  // attribute (JSON inside a double-quoted attribute must survive escaping).
  const m = /data-group="([^"]*)"/.exec(perf);
  const decode = x => x.replace(/&quot;/g, '"').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
  await showEvents(JSON.parse(decode(m ? m[1] : 'null')));
  process.stdout.write(JSON.stringify({
    traffic, views, perf,
    events: document.getElementById('events').innerHTML,
    hash: location.hash,
    charts: JSON.stringify(uplots),
  }));
  process.exit(0);
})().catch(e => { console.error('HARNESS_ERROR: ' + (e && e.stack || e)); process.exit(1); });
`
	src := buildHarness(t, statusFixture(now), liveFixture(now), statsBackend(t, now), formattingSliceJS(t), pageJS, tail)
	out := runNode(t, node, src)

	var res map[string]string
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("harness stdout not JSON: %v\nraw: %s", err, out)
	}
	checks := []struct{ field, want string }{
		{"views", "Performance"},
		{"traffic", "Error rate"},
		{"traffic", ">3<"}, // three requests, ungrouped
		{"perf", "TTFT p90"},
		{"perf", "311ms"},
		{"traffic", "Failover rate"},
		{"perf", `data-dim="endpoint"`},
		{"events", "b-ok"}, // the drill inherits the view's outcome:ok filter
		{"events", "1s"},
		{"hash", "view=performance"},
		{"charts", "Requests"},
		{"charts", "bai_free:glm-5.3-flash"}, // grouped chart: one line per endpoint
	}
	for _, c := range checks {
		if !strings.Contains(res[c.field], c.want) {
			t.Errorf("%s: missing %q\ngot: %s", c.field, c.want, res[c.field])
		}
	}
	if strings.Contains(res["events"], "no_candidate") || strings.Contains(res["events"], "upstream_5xx") {
		t.Errorf("drill-down from the performance view lists only that endpoint's successful requests: %s", res["events"])
	}
	// Performance is the speed of successful requests (outcome:ok), as the
	// old Performance window was: no failed or never-routed rows.
	if strings.Contains(res["perf"], "(unrouted)") || !strings.Contains(res["hash"], "where=outcome%3Aok") {
		t.Errorf("performance view must filter to successful requests: hash %s", res["hash"])
	}
}
