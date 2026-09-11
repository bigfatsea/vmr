// Ver 2026-09-11, by Sonnet 5
package server

// This file actually EXECUTES the Overview/Live & Log pages' embedded JS
// (via a subprocess `node`) against realistic /status + /stats fixtures,
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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
  get innerHTML() { return this._html; }
  set innerHTML(v) { this._html = String(v); }
  addEventListener() {}
  removeEventListener() {}
  appendChild(el) { this.children.push(el); return el; }
  prepend(el) { this.children.unshift(el); return el; }
  remove() {}
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

const fetch = async (url) => {
  const u = String(url);
  if (u.startsWith('/status')) return { ok: true, status: 200, json: async () => FIXTURE_STATUS };
  if (u.startsWith('/stats')) return { ok: true, status: 200, json: async () => FIXTURE_STATS };
  return { ok: false, status: 404, json: async () => ({}) };
};

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
func buildHarness(t *testing.T, statusFixture, statsFixture any, formattingJS, pageJS, tailJS string) string {
	t.Helper()
	statusJSON, err := json.Marshal(statusFixture)
	if err != nil {
		t.Fatalf("marshal status fixture: %v", err)
	}
	statsJSON, err := json.Marshal(statsFixture)
	if err != nil {
		t.Fatalf("marshal stats fixture: %v", err)
	}

	var b strings.Builder
	b.WriteString("const FIXTURE_STATUS = ")
	b.Write(statusJSON)
	b.WriteString(";\nconst FIXTURE_STATS = ")
	b.Write(statsJSON)
	b.WriteString(";\n")
	b.WriteString(consoleStubsJS)
	b.WriteString("\n// ---- console.js formatting helpers (verbatim slice) ----\n")
	b.WriteString(formattingJS)
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
// Modeled on a real production /status + /stats capture (a vmr instance
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
			"requests": map[string]any{"total": 5000, "by_status": map[string]any{"ok": 4800, "error": 150, "canceled": 50}},
			"tokens": map[string]any{"total": map[string]any{
				"in": 1_000_000, "out": 500_000, "cache_read": 300_000, "cache_write": 100_000, "reasoning": 0,
			}},
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

func statsFixture(now time.Time) map[string]any {
	hour := now.Truncate(time.Hour)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dims := map[string]any{
		"provider": "bai_free", "model": "glm-5.3-flash", "vmodel": "agent", "protocol": "anthropic",
		"client_key_tag": "cli-default",
	}
	counters := map[string]any{
		"ok": 42, "error": 2, "canceled": 1,
		"tokens": map[string]any{"in": 120000, "out": 60000, "cache_read": 30000, "cache_write": 10000, "reasoning": 0},
	}
	windowBlock := map[string]any{
		"n":           45,
		"tokens":      counters["tokens"],
		"ttft_p50_ms": 320, "ttft_p90_ms": 900,
		"toks_p50": 55.5, "toks_p90": 90.2,
	}
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
		"hourly":  []map[string]any{{"hour": hour.Format(time.RFC3339), "dims": dims, "counters": counters}},
		"daily":   []map[string]any{{"hour": today.Format(time.RFC3339), "dims": map[string]any{}, "counters": counters}},
		"overall": windowBlock,
		"recent_errors": []map[string]any{
			{
				"ts": now.Add(-5 * time.Minute).Format(time.RFC3339), "vmodel": "agent", "protocol": "anthropic",
				"stream": true, "client_key_tag": "cli-default", "provider": "bai_free", "key_label": "key_1",
				"model": "glm-5.3-flash", "attempt": 2, "outcome": "error", "error_class": "upstream_5xx",
				"status": 503, "dur_ms": 1450,
			},
		},
		"by_provider_model": []map[string]any{
			{
				"provider": "bai_free", "key_label": "key_1", "model": "glm-5.3-flash",
				"ok": 42, "error": 2, "canceled": 1, "tokens": counters["tokens"],
				"dur_ms": map[string]any{"sum": 45000, "n": 45}, "ttft_ms": map[string]any{"sum": 14000, "n": 45},
				"dur_ms_mean": 1000, "ttft_ms_mean": 311,
				"last_10": windowBlock, "last_100": windowBlock,
			},
		},
		"by_client_key_tag": []map[string]any{
			{"value": "cli-default", "ok": 42, "error": 2, "canceled": 1, "tokens": counters["tokens"], "count": 45},
		},
		"by_key_label": []map[string]any{
			{"value": "key_1", "ok": 42, "error": 2, "canceled": 1, "tokens": counters["tokens"], "count": 45},
		},
	}
}

func formattingSliceJS(t *testing.T) string {
	consoleJS := readSourceFile(t, "assets/console.js")
	return extractBetween(t, consoleJS, "/* ===================== formatting (design", "/* ============ overlay plumbing: one behaviour")
}

// TestConsoleRender_OverviewNoRuntimeError actually runs refreshAll() from
// status.html against a realistic /status+/stats fixture and asserts
// every section it is supposed to populate actually did. Regression guard
// for the fullTitle TDZ crash (see file doc comment): before the fix,
// renderModels() threw, refreshAll()'s try/catch swallowed it, and
// models-body/perf-body/chart/usage-key/usage-caller all stayed empty
// forever — this test fails loudly on that exact shape of bug.
func TestConsoleRender_OverviewNoRuntimeError(t *testing.T) {
	node := requireNode(t)
	now := time.Now()

	pageJS := extractScriptBody(t, readSourceFile(t, "status.html"))
	tail := `
(async () => {
  await refreshAll();
  await new Promise(res => setTimeout(res, 80));
  process.stdout.write(JSON.stringify({
    modelsBody: document.getElementById('models-body').innerHTML,
    perfBody: document.getElementById('perf-body').innerHTML,
    chart: document.getElementById('chart').innerHTML,
    usageKey: document.getElementById('usage-key').innerHTML,
    usageCaller: document.getElementById('usage-caller').innerHTML,
    quotaBody: document.getElementById('quota-body').innerHTML,
    vReq: document.getElementById('v-req').innerHTML,
    vTok: document.getElementById('v-tok').innerHTML,
  }));
  process.exit(0);
})().catch(e => { console.error('HARNESS_ERROR: ' + (e && e.stack || e)); process.exit(1); });
`
	src := buildHarness(t, statusFixture(now), statsFixture(now), formattingSliceJS(t), pageJS, tail)
	out := runNode(t, node, src)

	var res map[string]string
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("harness stdout not JSON: %v\nraw: %s", err, out)
	}

	checks := []struct{ field, want string }{
		{"modelsBody", "glm-5.3-flash"},
		{"modelsBody", "512K"},
		{"modelsBody", "text"},
		{"perfBody", "glm-5.3-flash"},
		{"chart", "<svg"},
		{"usageKey", "glm-5.3-flash"},
		{"usageCaller", "cli-default"},
		{"quotaBody", "volc_token_plan"},
		{"vReq", "total"},
		{"vTok", "total"},
	}
	for _, c := range checks {
		if !strings.Contains(res[c.field], c.want) {
			t.Errorf("%s: missing %q\ngot: %s", c.field, c.want, res[c.field])
		}
	}
}

// TestConsoleRender_LogPageLiveAndFailuresNoRuntimeError runs livePollTick()
// from log.html (the function driving the Live Requests / Recent Failures
// sections moved there from Overview) against the same fixture shape and
// asserts both tables populate.
func TestConsoleRender_LogPageLiveAndFailuresNoRuntimeError(t *testing.T) {
	node := requireNode(t)
	now := time.Now()

	pageJS := extractScriptBody(t, readSourceFile(t, "log.html"))
	tail := `
(async () => {
  await livePollTick();
  await new Promise(r => setTimeout(r, 50));
  process.stdout.write(JSON.stringify({
    liveBody: document.getElementById('live-body').innerHTML,
    failBody: document.getElementById('fail-body').innerHTML,
  }));
  process.exit(0);
})().catch(e => { console.error('HARNESS_ERROR: ' + (e && e.stack || e)); process.exit(1); });
`
	src := buildHarness(t, statusFixture(now), statsFixture(now), formattingSliceJS(t), pageJS, tail)
	out := runNode(t, node, src)

	var res map[string]string
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("harness stdout not JSON: %v\nraw: %s", err, out)
	}

	if !strings.Contains(res["liveBody"], "glm-5.3-flash") {
		t.Errorf("liveBody: missing running request row\ngot: %s", res["liveBody"])
	}
	if !strings.Contains(res["failBody"], "upstream_5xx") {
		t.Errorf("failBody: missing recent-failure row\ngot: %s", res["failBody"])
	}
}
