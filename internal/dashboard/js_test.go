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
const { versionBehavior, FmtTokens, FmtBytes, FmtPercent, FmtCurrency, FmtCost } = require(` + "'" + commonJSPath + "'" + `);
const fixture = JSON.parse(fs.readFileSync(` + "'" + fixturePath + "'" + `, 'utf8'));

const fns = { FmtTokens, FmtBytes, FmtPercent, FmtCurrency, FmtCost };

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

// TestJS_DashboardRenderSmoke verifies that all 6 dashboard HTML pages render their
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

function runPageSmoke(pageFile, sliceMap, hash) {
  const html = fs.readFileSync(path.join(assetsDir, pageFile), 'utf8');
  const scriptMatch = html.match(/<script>([\s\S]*?)<\/script>/);
  if (!scriptMatch) {
    throw new Error('No script block found in ' + pageFile);
  }
  const code = scriptMatch[1];

  const elements = new Map();
  function getEl(id) {
    if (!elements.has(id)) {
      elements.set(id, {
        id,
        innerHTML: '',
        textContent: '',
        classList: { add(){}, remove(){} },
        dataset: {},
        style: {},
        querySelectorAll: () => [],
        querySelector: () => null,
        focus: () => {},
      });
    }
    return elements.get(id);
  }

  const loc = { protocol: 'http:', hash: hash || '' };
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
    window: { location: loc },
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
  await new Promise(r => setTimeout(r, 60));
  const summaryHTML = macroEls.get('summary-stats').innerHTML;
  if (!summaryHTML.includes('120') || summaryHTML.includes('undefined') || summaryHTML.includes('NaN')) {
    throw new Error('macro-dashboard verification failed: ' + summaryHTML);
  }

  // 2. tool-waste.html
  const twEls = runPageSmoke('tool-waste.html', mockMacro);
  await new Promise(r => setTimeout(r, 60));
  const twBody = twEls.get('tools-body').innerHTML;
  if (!twBody.includes('tools:shape-1') || twBody.includes('undefined') || twBody.includes('NaN')) {
    throw new Error('tool-waste verification failed: ' + twBody);
  }

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
  await new Promise(r => setTimeout(r, 60));
  const bmStats = bmEls.get('headline-stats').innerHTML;
  if (!bmStats.includes('10') || bmStats.includes('undefined') || bmStats.includes('NaN')) {
    throw new Error('benchmarks verification failed: ' + bmStats);
  }

  // 4. journey-viewer.html (index)
  const mockJourneyIndex = {
    'manifest.json': mockManifest,
    'journeys/index.json': {
      journeys: [{ id: 'j-test-1', title: 'Test Journey 1', requests: 8 }]
    }
  };
  const jvIdxEls = runPageSmoke('journey-viewer.html', mockJourneyIndex);
  await new Promise(r => setTimeout(r, 60));
  const candList = jvIdxEls.get('cand-list').innerHTML;
  if (!candList.includes('j-test-1') || candList.includes('undefined') || candList.includes('NaN')) {
    throw new Error('journey-viewer index failed: ' + candList);
  }

  // 5. journey-viewer.html (detail)
  const mockJourneyDetail = {
    'manifest.json': mockManifest,
    'journeys/details/j-test-1.json': {
      id: 'j-test-1',
      title: 'Test Journey 1',
      from_display: '2026-08-24 10:00:00',
      to_display: '2026-08-24 10:05:00',
      metrics: { model_ms: 5000, agent_exec_ms: 3000, human_idle_ms: 1000, tool_call_count: 4, duplicate_action_rate: 0.1, plan_exec_ratio: 0.5 },
      cost: { total: 0.15, currency: 'USD' },
      findings: [{ code: 'test_code', finding: 'Test finding description', action: 'Take action' }],
      structure: {
        tasks: [{
          title: 'Task 1',
          steps: [{ seq: 1, ts_display: '2026-08-24 10:01:00', model: 'sonnet', dur_ms: 1200, usage: { in: 2000, out: 150 } }]
        }]
      }
    }
  };
  const jvDetailEls = runPageSmoke('journey-viewer.html', mockJourneyDetail, '#data=journeys/details/j-test-1.json');
  await new Promise(r => setTimeout(r, 60));
  const jvDetail = jvDetailEls.get('journey-view').innerHTML;
  if (!jvDetail.includes('Test Journey 1') || jvDetail.includes('undefined') || jvDetail.includes('NaN')) {
    throw new Error('journey-viewer detail failed: ' + jvDetail);
  }

  // 6. journey-compare.html (index)
  const mockCmpIndex = {
    'manifest.json': mockManifest,
    'compares/index.json': {
      compares: [{ filename: 'compare-a-vs-b.json', a_journey: { id: 'j-a', title: 'Journey A' }, b_journey: { id: 'j-b', title: 'Journey B' } }]
    }
  };
  const cmpIdxEls = runPageSmoke('journey-compare.html', mockCmpIndex);
  await new Promise(r => setTimeout(r, 60));
  const cmpCand = cmpIdxEls.get('cand-list').innerHTML;
  if (!cmpCand.includes('j-a vs j-b') || cmpCand.includes('undefined') || cmpCand.includes('NaN')) {
    throw new Error('journey-compare index failed: ' + cmpCand);
  }

  // 7. journey-compare.html (detail)
  const mockCmpDetail = {
    'manifest.json': mockManifest,
    'compares/compare-a-vs-b.json': {
      a_journey: { id: 'j-a', title: 'Journey A', steps: 10, tool_calls: 5 },
      b_journey: { id: 'j-b', title: 'Journey B', steps: 8, tool_calls: 3 },
      rows: [{ metric: 'model_ms', label: 'Model Time', kind: 'dur', a: 5000, b: 4000, delta_rel: -0.2 }],
      tools: [{ name: 'exec', a_calls: 5, b_calls: 3 }]
    }
  };
  const cmpDetailEls = runPageSmoke('journey-compare.html', mockCmpDetail, '#data=compares/compare-a-vs-b.json');
  await new Promise(r => setTimeout(r, 60));
  const cmpDetail = cmpDetailEls.get('compare-view').innerHTML;
  if (!cmpDetail.includes('Metric Diff') || cmpDetail.includes('undefined') || cmpDetail.includes('NaN')) {
    throw new Error('journey-compare detail failed: ' + cmpDetail);
  }
})();
`

	cmd := exec.Command(nodePath, "-e", testScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dashboard render smoke test failed: %v\nOutput:\n%s", err, string(out))
	}
}
