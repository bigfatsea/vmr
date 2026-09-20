// Ver 2026-09-11, by Sonnet 5
package server

// This file actually EXECUTES the Overview/Models pages' embedded JS
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
		"toks_p50": 55.5, "toks_p10": 30.2,
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
		"hourly": []map[string]any{
			{"hour": hour.Format(time.RFC3339), "dims": dims, "counters": counters},
			{
				"hour":     hour.Format(time.RFC3339),
				"dims":     map[string]any{"provider": "", "model": "", "vmodel": "agent", "protocol": "anthropic"},
				"counters": map[string]any{"ok": 0, "error": 3, "canceled": 0, "tokens": map[string]any{}},
			},
		},
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
		"recent_requests": []map[string]any{
			{
				"ts":        now.Add(-1 * time.Minute).Format(time.RFC3339),
				"provider":  "bai_free",
				"key_label": "key_1",
				"model":     "glm-5.3-flash",
				"stream":    true,
				"dur_ms":    1000,
				"ttft_ms":   311,
				"tokens":    counters["tokens"],
			},
		},
		"by_provider_model": []map[string]any{
			{
				"provider": "bai_free", "key_label": "key_1", "model": "glm-5.3-flash",
				"ok": 42, "error": 2, "canceled": 1, "tokens": counters["tokens"],
				"dur_ms": map[string]any{"sum": 45000, "n": 45}, "ttft_ms": map[string]any{"sum": 14000, "n": 45},
				"dur_ms_mean": 1000, "ttft_ms_mean": 311,
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
// perf-body/chart/usage-key/usage-caller all stayed empty forever — this
// test fails loudly on that exact shape of bug. Quota/Models moved to
// models.html (see TestConsoleRender_ModelsPageNoRuntimeError); Live
// Requests/Recent Failures moved back here from log.html.
func TestConsoleRender_OverviewNoRuntimeError(t *testing.T) {
	node := requireNode(t)
	now := time.Now()

	pageJS := extractScriptBody(t, readSourceFile(t, "status.html"))
	tail := `
(async () => {
  await refreshAll();
  await new Promise(res => setTimeout(res, 80));
  process.stdout.write(JSON.stringify({
    liveBody: document.getElementById('live-body').innerHTML,
    failBody: document.getElementById('fail-body').innerHTML,
    perfBody: document.getElementById('perf-body').innerHTML,
    chart: document.getElementById('chart').innerHTML,
    usageKey: document.getElementById('usage-key').innerHTML,
    usageCaller: document.getElementById('usage-caller').innerHTML,
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
		{"liveBody", "glm-5.3-flash"},
		{"failBody", "upstream_5xx"},
		{"perfBody", "glm-5.3-flash"},
		{"chart", "<svg"},
		{"usageKey", "glm-5.3-flash"},
		{"usageKey", "gateway / unrouted"},
		{"usageCaller", "cli-default"},
		{"vReq", "7d"},
		{"vTok", "7d"},
	}
	for _, c := range checks {
		if !strings.Contains(res[c.field], c.want) {
			t.Errorf("%s: missing %q\ngot: %s", c.field, c.want, res[c.field])
		}
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
	src := buildHarness(t, statusFixture(now), statsFixture(now), formattingSliceJS(t), pageJS, tail)
	out := runNode(t, node, src)

	var res map[string]string
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatalf("harness stdout not JSON: %v\nraw: %s", err, out)
	}

	checks := []struct{ field, want string }{
		{"modelsBody", "Anthropic Messages (/v1/messages)"},
		{"modelsBody", `<span class="t-strong">agent</span>`},
		{"modelsBody", "glm-5.3-flash"},
		{"modelsBody", "512K"},
		{"modelsBody", "text"},
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
	src := buildHarness(t, statusFixture(now), statsFixture(now), formattingSliceJS(t), pageJS, tail)
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
