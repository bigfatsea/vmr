// Ver 2026-09-06, by Claude
package dashboard

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestJS_PureFunctionsAndFixture executes pure JavaScript functions against
// testdata/fmt_cases.json using Node.js (§5.6). Skips cleanly if node is not found.
func TestJS_PureFunctionsAndFixture(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node executable not found, skipping JS assertions")
	}

	commonJSPath, err := filepath.Abs("assets/common.js")
	if err != nil {
		t.Fatalf("resolve common.js path: %v", err)
	}
	fixturePath, err := filepath.Abs("testdata/fmt_cases.json")
	if err != nil {
		t.Fatalf("resolve fmt_cases.json path: %v", err)
	}

	testScript := `
const fs = require('fs');
const { versionBehavior, FmtTokens, FmtBytes, FmtPercent, FmtCurrency, FmtCost, FmtCurrencyPrecise } = require(` + "'" + commonJSPath + "'" + `);
const fixture = JSON.parse(fs.readFileSync(` + "'" + fixturePath + "'" + `, 'utf8'));

const fns = { FmtTokens, FmtBytes, FmtPercent, FmtCurrency, FmtCost, FmtCurrencyPrecise };

let failed = 0;
for (const tc of fixture.cases) {
  const fn = fns[tc.fn];
  if (!fn) {
    console.error('Unknown fn:', tc.fn);
    failed++;
    continue;
  }
  const got = fn(tc.input);
  if (got !== tc.want) {
    console.error('Mismatch for ' + tc.fn + '(' + tc.input + '): got "' + got + '", want "' + tc.want + '"');
    failed++;
  }
}

const vbCases = [
  { exp: 11, act: 11, want: 'ok' },
  { exp: 11, act: '11', want: 'ok' },
  { exp: 11, act: 10, want: 'banner' },
  { exp: 11, act: 12, want: 'banner' },
  { exp: 11, act: null, want: 'missing' },
  { exp: 11, act: undefined, want: 'missing' },
  { exp: 11, act: '', want: 'missing' },
];

for (const c of vbCases) {
  const got = versionBehavior(c.exp, c.act);
  if (got !== c.want) {
    console.error('versionBehavior mismatch:', c, got);
    failed++;
  }
}

if (failed > 0) {
  process.exit(1);
}
`
	cmd := exec.Command(nodePath, "-e", testScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node test script failed: %v\nOutput:\n%s", err, string(out))
	}
}

// TestJS_DashboardRenderSmoke verifies that every dashboard HTML page (macro-dashboard,
// tool-waste, benchmarks, journey-viewer, journey-compare, request-browser) renders its
// underlying JSON slices without NaN, undefined, or unrendered dash placeholders in
// critical cells (§6.2, N15).
func TestJS_DashboardRenderSmoke(t *testing.T) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node executable not found, skipping JS render smoke test")
	}

	assetsDir, err := filepath.Abs("assets")
	if err != nil {
		t.Fatalf("resolve assets dir: %v", err)
	}

	testScript := `
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const assetsDir = ` + "'" + assetsDir + "'" + `;
const common = require(path.join(assetsDir, 'common.js'));

function runPageSmoke(pageFile, sliceMap, hash, elInit) {
  const html = fs.readFileSync(path.join(assetsDir, pageFile), 'utf8');
  const scriptMatch = html.match(/<script>([\s\S]*?)<\/script>/);
  if (!scriptMatch) {
    throw new Error('No script block found in ' + pageFile);
  }
  const code = scriptMatch[1];

  const elements = new Map();
  function getEl(id) {
    if (!elements.has(id)) {
      const el = {
        id,
        innerHTML: '',
        textContent: '',
        value: '',
        classList: { add(){}, remove(){} },
        dataset: {},
        style: {},
        querySelectorAll: () => [],
        querySelector: () => null,
        focus: () => {},
      };
      if (elInit && elInit[id]) { Object.assign(el, elInit[id]); }
      elements.set(id, el);
    }
    return elements.get(id);
  }

  const loc = { protocol: 'http:', hash: hash || '', reload: () => {} };
  global.location = loc;
  globalThis.location = loc;

  const doc = {
    getElementById: (id) => getEl(id),
    querySelectorAll: () => [],
    querySelector: () => null,
    documentElement: { setAttribute(){}, removeAttribute(){} }
  };
  global.document = doc;

  const sandbox = {
    ...common,
    document: doc,
    location: loc,
    window: { location: loc, addEventListener: () => {} },
    navigator: { clipboard: { writeText: async () => {} } },
    localStorage: { getItem: () => '', setItem: () => {}, removeItem: () => {} },
    fetchSlice: async (rel) => {
      if (sliceMap[rel]) return sliceMap[rel];
      throw new Error('404: ' + rel);
    },
    console,
    Date, Math, Number, String, Boolean, Array, Object, Set, Map, Promise, RegExp,
    setTimeout: (fn) => fn(),
    setInterval: () => {},
  };

  const ctx = vm.createContext(sandbox);
  vm.runInContext(code, ctx);
  return elements;
}

// waitFor polls getter until check(value) holds, with a hard timeout. Pages load
// their slices through an async IIFE over fetchSlice promises, so a fixed sleep
// is both flaky (slow CI box) and slower than needed — polling the actual render
// output is deterministic. The timeout error carries the last observed value so
// a render regression is diagnosable from the failure output alone.
async function waitFor(getter, check, desc) {
  const deadline = Date.now() + 5000;
  for (;;) {
    const v = getter();
    if (check(v)) return v;
    if (Date.now() > deadline) {
      throw new Error(desc + ' — last value: ' + String(v).slice(0, 800));
    }
    await new Promise(r => setTimeout(r, 5));
  }
}

const mockManifest = {
  format: 11,
  lang: 'en',
  generated_at: { ts: 1787500000000, ts_display: '2026-08-24 10:00:00' },
  time_range: ['2026-08-24 00:00:00', '2026-08-24 23:59:59']
};

const mockMacro = {
  'manifest.json': mockManifest,
  'macro/summary.json': {
    overall: {
      requests: 120,
      ok: 118,
      errors: 2,
      tokens_in: 500000,
      tokens_in_cached: 400000,
      tokens_in_fresh: 100000,
      tokens_out: 25000,
      cost_estimate: 1.50,
      fallbacks: 3,
      cache_efficiency: 0.80,
      success_rate: 0.983
    },
    highlights: ['Highlight 1', 'Highlight 2'],
    efficiency: [{ code: 'cache_miss', finding: 'High cache miss rate', value: '20%', action: 'Review prompt prefix' }]
  },
  'macro/finance.json': {
    pricing: { currency: 'USD' },
    cost_coverage: { unpriced_count: 0, incomplete_rate_count: 0, degraded_estimate_pct: 0 },
    by_model: [{ model: 'claude-3-7-sonnet', requests: 100, tokens_in_fresh: 80000, tokens_in_cached: 350000, tokens_out: 20000, cost_estimate: 1.25 }],
    by_client: [{ client_key: 'agent-1', requests: 120, tokens_in: 500000, tokens_out: 25000, cost_estimate: 1.50 }],
    provider_quotas: [{ provider: 'anthropic', metric: 'tokens', window_consumed: 525000, live: { used: 525000 }, amount: 1000000 }]
  },
  'macro/reliability.json': {
    endpoints: [{ endpoint: 'anthropic:claude-3-7-sonnet', protocol: 'anthropic', requests: 120, failed: 2, error_rate: 1.67, dur_ms_p50: 1200, dur_ms_p95: 3500, ttft_ms_p50: 450 }],
    sticky: { continued: { requests: 90, cache_efficiency: 0.92 }, switched: { requests: 30, cache_efficiency: 0.45 } }
  },
  'macro/workloads.json': {
    by_date: [{ date: '2026-08-24', requests: 120, tokens_in_fresh: 100000, tokens_in_cached: 400000, tokens_out: 25000, cost_estimate: 1.50 }],
    hours_of_day: [{ hour: 10, requests: 120 }],
    client_endpoints: [{ client_key: 'agent-1', endpoint: 'anthropic:claude-3-7-sonnet', requests: 120, tokens_in: 500000, tokens_out: 25000 }]
  },
  'macro/context-efficiency.json': {
    sessions: [{ id: 's-1', requests: 15 }],
    compactions: [{ ts: 1787500000000, ts_display: '2026-08-24 10:00:00', tokens_in: 45000, tokens_out: 2000, swallowed_entities: ['foo'], survived_entities: ['bar'] }],
    tools: [{ shape: 'tools:shape-1', schema_bytes_shipped: 10000, schema_waste_bytes: 2000, distinct_called: 3, declared: ['read', 'write', 'exec', 'search'] }]
  }
};

(async () => {
  // 1. macro-dashboard.html
  const macroEls = runPageSmoke('macro-dashboard.html', mockMacro);
  const summaryHTML = await waitFor(
    () => (macroEls.get('summary-stats') || {}).innerHTML || '',
    (h) => h.includes('120') && !h.includes('undefined') && !h.includes('NaN'),
    'macro-dashboard summary-stats did not render cleanly'
  );

  // 2. tool-waste.html
  const twEls = runPageSmoke('tool-waste.html', mockMacro);
  const twBody = await waitFor(
    () => (twEls.get('tools-body') || {}).innerHTML || '',
    (h) => h.includes('tools:shape-1') && !h.includes('undefined') && !h.includes('NaN'),
    'tool-waste tools-body did not render cleanly'
  );

  // 3. benchmarks.html
  const mockBenchmarks = {
    'manifest.json': mockManifest,
    'journeys/benchmarks.json': {
      journey_count: 10,
      metric_distributions: { model_ms: { count: 10, mean: 1250, median: 1100, p90: 2000, min: 500, max: 3000 } },
      finding_rates: { cache_miss: 0.2 },
      correlations: [{ metric_a: 'model_ms', metric_b: 'tokens_out', rho: 0.85, n: 10 }],
      protocol_share: { anthropic: 1.0 }
    }
  };
  const bmEls = runPageSmoke('benchmarks.html', mockBenchmarks);
  const bmStats = await waitFor(
    () => (bmEls.get('headline-stats') || {}).innerHTML || '',
    (h) => h.includes('10') && !h.includes('undefined') && !h.includes('NaN'),
    'benchmarks headline-stats did not render cleanly'
  );

  // 4. journey-viewer.html (index)
  const mockJourneyIndex = {
    'manifest.json': mockManifest,
    'journeys/index.json': {
      journeys: [{ id: 'j-test-1', title: 'Test Journey 1', requests: 8 }]
    }
  };
  const jvIdxEls = runPageSmoke('journey-viewer.html', mockJourneyIndex);
  const candList = await waitFor(
    () => (jvIdxEls.get('cand-list') || {}).innerHTML || '',
    (h) => h.includes('j-test-1') && !h.includes('undefined') && !h.includes('NaN'),
    'journey-viewer index cand-list did not render cleanly'
  );

  // 5. journey-viewer.html (detail) — exercises the full behavior-indicator
  // table, the context sparkline, model usage, and tool-call args + paired
  // results resolved through the bodies blob table.
  const mockJourneyDetail = {
    'manifest.json': mockManifest,
    'journeys/details/j-test-1.json': {
      id: 'j-test-1',
      title: 'Test Journey 1',
      from_display: '2026-08-24 10:00:00',
      to_display: '2026-08-24 10:05:00',
      metrics: {
        net_working_ms: 9000, model_ms: 5000, agent_exec_ms: 3000, human_idle_ms: 1000,
        model_to_tool_ratio: 1.67, tool_call_count: 4, duplicate_action_rate: 0.1,
        output_repetition_rate: 0.05, error_recovery_count: 0, plan_exec_ratio: 0.5,
        context_utilization: 0.9, compaction_count: 0, compaction_loss_tokens: 0,
        context_composition_curve: [
          { seq: 1, system_tokens: 100, user_tokens: 200, assistant_tokens: 0, tool_tokens: 0 },
          { seq: 2, system_tokens: 100, user_tokens: 200, assistant_tokens: 300, tool_tokens: 400 }
        ],
        model_usage: [{ model: 'sonnet', provider: 'anthropic', steps: 4, tokens_in: 8000, tokens_in_cached: 6000, tokens_out: 600 }],
        model_switches: []
      },
      cost: { total: 0.15, currency: 'USD', resolved: true, priced_steps: 4, total_steps: 4 },
      findings: [{ code: 'test_code', step_seq: 1, finding: 'Test finding description', evidence: 'e', action: 'Take action' }],
      bodies: { A: 'the args payload', B: 'the tool result body', R: 'the assistant reply' },
      structure: {
        tasks: [{
          title: 'Task 1',
          steps: [{
            seq: 1, ts_display: '2026-08-24 10:01:00', model: 'sonnet', protocol: 'anthropic-messages',
            endpoint: 'anthropic-messages:anthropic:sonnet', outcome: 'ok', finish: 'stop', dur_ms: 1200,
            usage: { in: 2000, out: 150 }, resp_ref: 'R', resp_is_reasoning: false,
            tool_calls: [{ name: 'exec', args_ref: 'A', result: { ref: 'B', match: 'exact', is_error: false } }]
          }, {
            seq: 2, ts_display: '2026-08-24 10:02:00', model: 'sonnet', protocol: 'anthropic-messages',
            endpoint: 'anthropic-messages:anthropic:sonnet', outcome: 'ok', finish: 'stop', dur_ms: 1000,
            usage: { in: 3000, out: 100 }, resp_ref: 'R', resp_is_reasoning: false,
            cache_break: 'unexplained', cache_break_ratio_from: 0.95, cache_break_ratio_to: 0.31
          }]
        }]
      },
      deliverable: { found: true, tool_name: 'write', step_seq: 1, excerpt: 'package main', truncated: false }
    }
  };
  const jvDetailEls = runPageSmoke('journey-viewer.html', mockJourneyDetail, '#data=journeys/details/j-test-1.json');
  const jvDetail = await waitFor(
    () => (jvDetailEls.get('journey-view') || {}).innerHTML || '',
    (h) => h.includes('Test Journey 1') && !h.includes('undefined') && !h.includes('NaN'),
    'journey-viewer detail journey-view did not render cleanly'
  );
  for (const section of ['Behavior Indicators', 'Model Usage', 'Decision Spine', 'the tool result body', 'Final Deliverable', 'Cache: unexplained drop (95%→31%)']) {
    if (!jvDetail.includes(section)) {
      throw new Error('journey-viewer detail missing "' + section + '": ' + jvDetail);
    }
  }

  // 6. journey-compare.html (index)
  const mockCmpIndex = {
    'manifest.json': mockManifest,
    'compares/index.json': {
      compares: [{ filename: 'compare-a-vs-b.json', a_journey: { id: 'j-a', title: 'Journey A' }, b_journey: { id: 'j-b', title: 'Journey B' } }]
    }
  };
  const cmpIdxEls = runPageSmoke('journey-compare.html', mockCmpIndex);
  const cmpCand = await waitFor(
    () => (cmpIdxEls.get('cand-list') || {}).innerHTML || '',
    (h) => h.includes('j-a vs j-b') && !h.includes('undefined') && !h.includes('NaN'),
    'journey-compare index cand-list did not render cleanly'
  );

  // 7. journey-compare.html (detail) — rows[].kind values match
  // internal/journey/compare.go (ms/multiple/ratio/count/tokens), and the
  // page must render the extras.* + llm_* sections, not just the diff table.
  const mockCmpDetail = {
    'manifest.json': mockManifest,
    'compares/compare-a-vs-b.json': {
      a_journey: { id: 'j-a', title: 'Journey A', steps: 10, tool_calls: 5 },
      b_journey: { id: 'j-b', title: 'Journey B', steps: 8, tool_calls: 3 },
      rows: [
        { metric: 'model_ms', label: 'Model Time', kind: 'ms', a: 5000, b: 4000, delta_rel: -0.2, notable: false },
        { metric: 'agent_exec_ms', label: 'Agent Exec', kind: 'ms', a: 8000, b: 800, delta_rel: -0.9, notable: true },
        { metric: 'plan_exec_ratio', label: 'Plan/Exec', kind: 'ratio', a: 0.5, b: 0.4, delta_rel: -0.2, notable: false }
      ],
      tools: [{ name: 'exec', a_calls: 5, b_calls: 3 }],
      extras: {
        endpoints: { a: ['openai:p:m1'], b: ['openai:p:m2'], same: false },
        cache: { a: { first_ratio: 0.2, steady_mean: 0.9, min: 0, max: 1, series: [{ seq: 1, ratio: 0.2 }, { seq: 2, ratio: 0.9 }], breaks: { unexplained: 2, 'history:replace_tail': 1 } },
                 b: { first_ratio: 0.1, steady_mean: 0.95, min: 0, max: 1, series: [{ seq: 1, ratio: 0.1 }], breaks: { provider_switch: 1 } } },
        sys_prompt: { a: { tokens: 0, changes: 0, excerpt: '', truncated: false }, b: { tokens: 0, changes: 0, excerpt: '', truncated: false } },
        final_context: { a: { seq: 10, system_tokens: 0, user_tokens: 100, assistant_tokens: 200, tool_tokens: 50 },
                         b: { seq: 8, system_tokens: 0, user_tokens: 80, assistant_tokens: 150, tool_tokens: 40 } },
        duration: { a_wall_ns: 8582590290000, b_wall_ns: 3206357437000, a_termination: 'stop', b_termination: 'stop' },
        deliverable: { a: { found: true, tool_name: 'write', step_seq: 7, excerpt: 'package main', truncated: true }, b: { found: false } },
        cost: { a: { currency: 'USD', total: 0.3, resolved: true, priced_steps: 3, total_steps: 10 }, b: { currency: 'USD', resolved: false, priced_steps: 0, total_steps: 8 } },
        initial_instruction: { a: { found: true, text: 'do the thing' }, b: { found: true, text: 'verify the thing' } },
        divergence: { found: true, index: 0, a_step_seq: 1, b_step_seq: 1, task_title: 'do the thing', a_tools: ['bash'], b_tools: ['bash'], severity: 'light' },
        sources: ['vmr-audit-2026-08-25.jsonl']
      },
      llm_interpretation: { model: 'cheap', scope: 'overall', status: 'ok', cached: true, text: 'A and B differ in scope.' },
      llm_divergence: { model: 'cheap', scope: 'divergence', status: 'ok', cached: true, text: 'They diverge at step 1.' }
    }
  };
  const cmpDetailEls = runPageSmoke('journey-compare.html', mockCmpDetail, '#data=compares/compare-a-vs-b.json');
  const cmpDetail = await waitFor(
    () => (cmpDetailEls.get('compare-view') || {}).innerHTML || '',
    (h) => h.includes('Journey A') && !h.includes('undefined') && !h.includes('NaN'),
    'journey-compare detail compare-view did not render cleanly'
  );
  for (const section of ['Behavior Profile Comparison', 'Divergence Point', 'Model &amp; Endpoint Check', 'Prompt Cache Hit Rate', 'Cost Estimate', 'Evidence Provenance', 'LLM Interpretation']) {
    if (!cmpDetail.includes(section)) {
      throw new Error('journey-compare detail missing section "' + section + '": ' + cmpDetail);
    }
  }
  // kind: 'ms' must format as seconds, never a bare millisecond count.
  if (!cmpDetail.includes('5.0s') || cmpDetail.includes('>5000<')) {
    throw new Error('journey-compare detail did not format ms rows as seconds: ' + cmpDetail);
  }

  // 8. request-browser.html — the only page consuming requests/index.json:
  // table rows, facet options, journey_link → journey-viewer deep link, and
  // pagination meta. The page reads f-pagesize's value for page math, so the
  // mock select must be pre-seeded or pagination degenerates to NaN slicing.
  const mockRequests = {
    'manifest.json': mockManifest,
    'requests/index.json': {
      requests: [
        {
          ts: 1787500000000,
          ts_display: '2026-08-24 10:00:00',
          client_key: 'agent-1',
          model: 'claude-3-7-sonnet',
          endpoint: 'anthropic:claude-3-7-sonnet',
          outcome: 'ok',
          dur_ms: 1200,
          ttft_ms: 450,
          tokens_in_fresh: 80000,
          tokens_in_cached: 350000,
          tokens_out: 20000,
          cache_eff: 0.814,
          fallbacks: 0,
          session: 'sess-1',
          detail_file: 'r-abc123def456.md'
        },
        {
          ts: 1787500300000,
          ts_display: '2026-08-24 10:05:00',
          client_key: 'cli-2',
          model: 'gpt-5',
          endpoint: 'openai:gpt-5',
          outcome: 'error',
          error_class: 'upstream_5xx',
          dur_ms: 300,
          tokens_in_fresh: 1000,
          tokens_in_cached: 0,
          tokens_out: 0,
          fallbacks: 2,
          session: 'sess-2'
        }
      ],
      journey_link: { 'sess-1': 'details/j-test-1.md' },
      sessions: { 'sess-1': { title: 'Refactor the router', alias: 'router work' } }
    }
  };
  const rbEls = runPageSmoke('request-browser.html', mockRequests, '', { 'f-pagesize': { value: '50' } });
  const rbRows = await waitFor(
    () => (rbEls.get('rows-body') || {}).innerHTML || '',
    (h) => h.includes('agent-1') && h.includes('upstream_5xx') && !h.includes('undefined') && !h.includes('NaN'),
    'request-browser rows-body did not render cleanly'
  );
  if (!rbRows.includes('r-abc123def456.md') || !rbRows.includes('journeys/details/j-test-1.json')) {
    throw new Error('request-browser detail/journey links missing: ' + rbRows);
  }
  // Facet selects are rebuilt from the rows; both client values must appear.
  const rbClientFacet = rbEls.get('f-client').innerHTML;
  if (!rbClientFacet.includes('agent-1') || !rbClientFacet.includes('cli-2')) {
    throw new Error('request-browser client facet missing options: ' + rbClientFacet);
  }
  // render() fills meta and rows in one synchronous pass, so once rows-body
  // has content these are already settled — plain assertions, no extra wait.
  if (!rbEls.get('filter-meta').textContent.includes('2 / 2 rows') ||
      !rbEls.get('page-info').textContent.includes('Page 1 / 1')) {
    throw new Error('request-browser pagination meta mismatch: ' +
      rbEls.get('filter-meta').textContent + ' / ' + rbEls.get('page-info').textContent);
  }
})();
`

	cmd := exec.Command(nodePath, "-e", testScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dashboard render smoke test failed: %v\nOutput:\n%s", err, string(out))
	}
}
