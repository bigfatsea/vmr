package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vmr/internal/stats"
)

// The live half of Overview and the statistics dashboard are separate
// failure domains: with the statistics store unavailable (data routes 503)
// the vitals still render, and the dashboard says why it is empty instead of
// taking the page down with it.
func TestConsoleRender_OverviewSurvivesStatsFailure(t *testing.T) {
	node := requireNode(t)
	now := time.Now()
	h := stats.NewHandler(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats/schema", h.Schema)
	mux.HandleFunc("GET /stats/query", h.Query)
	mux.HandleFunc("GET /stats/events", h.Events)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pageJS := extractScriptBody(t, readSourceFile(t, "status.html"))
	tail := `
const html = id => document.getElementById(id).innerHTML;
(async () => {
  await refreshAll();
  await new Promise(res => setTimeout(res, 80));
  process.stdout.write(JSON.stringify({ eps: html('v-eps'), sysline: html('sysline'), kpis: html('dash-kpis'), activity: html('dash-activity'), ctl: html('dash-ctl') }));
  process.exit(0);
})().catch(e => { console.error('HARNESS_ERROR: ' + (e && e.stack || e)); process.exit(1); });
`
	src := buildHarness(t, statusFixture(now), liveFixture(now), srv.URL, formattingSliceJS(t), pageJS, tail)
	var res map[string]string
	if err := json.Unmarshal(runNode(t, node, src), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res["eps"], "healthy") || !strings.Contains(res["sysline"], "audit") {
		t.Errorf("vitals must render without statistics: eps %q sysline %q", res["eps"], res["sysline"])
	}
	if !strings.Contains(res["kpis"], "Statistics unavailable") || !strings.Contains(res["kpis"], "data-retry") {
		t.Errorf("dashboard must show its own failure: %q", res["kpis"])
	}
	if res["activity"] != "" || !strings.Contains(res["ctl"], `data-k="1d"`) {
		t.Errorf("panels cleared, controls kept: activity %q ctl %q", res["activity"], res["ctl"])
	}
}

// Pure helpers and renderers fixed by the review: comparison windows, token
// gaps, the heatmap's per-hour averages, axis labels, the timeout window,
// the (none) failure class and where-value escaping.
func TestConsoleRender_DashboardReviewFixes(t *testing.T) {
	node := requireNode(t)
	tail := `
const D = VMRDash;
const cols = names => names.map(n => ({ name: n }));
const kpiCols = ['req','ok','canceled','err','err_rate','cancel_rate','tok_total','tok_fresh','tok_cache_write','tok_cache_read','tok_out','cache_hit','ttft_p50','ttft_p90','dur_p50','dur_p90','toks_p50','toks_p10'];
const kpiTot = Object.fromEntries(kpiCols.map(c => [c, 0]));
const mk = (vals, extra) => Object.assign({ columns: cols(kpiCols), totals: kpiCols.map(c => (vals && c in vals) ? vals[c] : kpiTot[c]), rows: [] }, extra);

// buckets of 6h over 7 days
const b6 = Array.from({ length: 28 }, (_, i) => { const t = new Date(Date.UTC(2026, 9, 13) + i * 6 * 3600e3); return t.toISOString().slice(0, 19) + '+08:00'; });
const expo7 = D.heatExposure('2026-10-13T10:30:00+08:00', '2026-10-20T10:30:00+08:00');
const expo30 = D.heatExposure('2026-09-20T10:30:00+08:00', '2026-10-20T10:30:00+08:00');
const near1 = [...expo7.values()].every(v => Math.abs(v - 1) < 1e-9);
const hours30 = [...expo30.values()].reduce((a, b) => a + b, 0);

const heatRes = { query: { from: '2026-10-13T10:30:00+08:00', to: '2026-10-20T10:30:00+08:00' }, columns: cols(['weekday','hour_of_day','req']), rows: [['Mon', '10', 5]] };

const cmpDay = { st: { cmp: '1', range: 'today' }, st24: { cmp: '1', range: '24h' }, st1h: { cmp: '1', range: '1h' }, st7d: { cmp: '1', range: '7d' }, off: { cmp: '0', range: 'today' } };

const reqRes = bucketsRes => ({ columns: cols(['bucket','outcome','req']), rows: bucketsRes.flatMap(b => [[b,'ok',5],[b,'canceled',0],[b,'error',1]]) });
const tsRes = bs => ({ columns: cols(['bucket','req','tok_fresh','tok_cache_write','tok_cache_read','tok_out','cache_hit']), rows: bs.map(b => [b,6,1,0,1,1,0.5]),
  compare: { rows: bs.map(b => [b,6,1,0,1,1,0.5]), totals: [] }, query: { step: '1h' } });
const two = [b6[0], b6[1]];
const act = (st, extra) => D.renderActivity(Object.assign({ outcome: reqRes(two), ts: tsRes(two), split: {} }, extra), Object.assign({ act: 'req', split: 'outcome', off: new Set() }, st), '\u0000other', 900).html;

const relD = { kpi: mk({}), failover: { columns: cols(['outcome','req']), rows: [] }, failClass: { columns: cols(['error_class','req']), rows: [['', 3], ['x', 1]] },
  cancels: [], failEvents: [{ outcome: 'error', error_class: '', ts: '2026-10-19T10:00:00+08:00', vmodel: 'm', protocol: 'p' }, { outcome: 'error', error_class: 'zzclass', ts: '2026-10-19T10:00:00+08:00', vmodel: 'm', protocol: 'p' }] };

process.stdout.write(JSON.stringify({
  modes: [D.compareMode(cmpDay.st), D.compareMode(cmpDay.st24), D.compareMode(cmpDay.off), D.compareMode(cmpDay.st1h), D.compareMode(cmpDay.st7d)],
  near1, size7: expo7.size, hours30,
  ticks: D.axisTicks(b6, 350).map(t => t.label),
  clusters: D.timeoutClusters([59600, 59800, 60100, 61500, 61700, 61900]),
  rt: VMRStats.splitWhere(VMRStats.escWhere('a|b\\c')),
  heat7: D.renderHeat(heatRes, '7d'),
  relNone: D.renderReliability(Object.assign({}, relD), { failCls: '' }, '#'),
  relAll: D.renderReliability(Object.assign({}, relD), { failCls: null }, '#'),
  actOutcome: act({ cmp: '1', range: '24h' }),
  actToday: act({ cmp: '1', range: 'today' }),
  actSplit: act({ cmp: '1', range: '24h', split: 'client' }, { split: { 'client:req': { columns: cols(['bucket','client','req']), rows: two.map(b => [b,'a',6]) } } }),
  kpiGap: D.renderKPIs({ kpi: Object.assign(mk({ req: 10, tok_total: 100 }), { meta: { tokens_unknown: 4 }, compare: { totals: kpiCols.map(() => 5), tokens_unknown: 0 } }), ts: tsRes(two), failover: { columns: cols(['outcome','req']), rows: [] } }, { cmp: '1', range: '24h' }),
  kpiClean: D.renderKPIs({ kpi: Object.assign(mk({ req: 10, tok_total: 100 }), { meta: { tokens_unknown: 0 }, compare: { totals: kpiCols.map(() => 50), tokens_unknown: 0 } }), ts: tsRes(two), failover: { columns: cols(['outcome','req']), rows: [] } }, { cmp: '1', range: '24h' }),
}));
`
	src := buildHarness(t, map[string]any{}, map[string]any{}, "", formattingSliceJS(t), "", tail)
	var res struct {
		Modes                                                          []any
		Near1                                                          bool
		Size7                                                          int
		Hours30                                                        float64
		Ticks                                                          []string
		Clusters                                                       []struct{ Sec, N int }
		Rt                                                             []string
		Heat7, RelNone, RelAll, ActOutcome, ActToday, ActSplit, KpiGap string
		KpiClean                                                       string
	}
	if err := json.Unmarshal(runNode(t, node, src), &res); err != nil {
		t.Fatal(err)
	}
	if res.Modes[0] != "day" || res.Modes[1] != "prev" || res.Modes[2] != false || res.Modes[3] != "prev" || res.Modes[4] != "prev" {
		t.Errorf("compare modes = %v, want today → day (its cycle is a day), every other range → prev, off → off", res.Modes)
	}
	if !res.Near1 || res.Size7 != 168 || int(res.Hours30+0.5) != 720 {
		t.Errorf("heat exposure: every cell of a 7d range is one hour (%v, %d cells), 30d covers 720 hours (%v)", res.Near1, res.Size7, res.Hours30)
	}
	for _, l := range res.Ticks {
		if !strings.Contains(l, "-") || strings.Contains(l, ":") {
			t.Errorf("multi-day axis label %q must name the day, not a bare clock time (ticks %v)", l, res.Ticks)
		}
	}
	if len(res.Ticks) < 2 {
		t.Errorf("ticks = %v", res.Ticks)
	}
	if len(res.Clusters) != 1 || res.Clusters[0].Sec != 60 || res.Clusters[0].N != 3 {
		t.Errorf("clusters = %+v: three stops within a second of 60 s (59.6 / 59.8 / 60.1), but 61.5–61.9 s are not a mark", res.Clusters)
	}
	if len(res.Rt) != 1 || res.Rt[0] != `a|b\c` {
		t.Errorf("where value round trip = %q", res.Rt)
	}
	if !strings.Contains(res.Heat7, "5.0 requests / hour") {
		t.Errorf("heatmap cell must show an average per hour: %s", res.Heat7)
	}
	if !strings.Contains(res.RelNone, "showing (none)") {
		t.Errorf("the (none) class must filter the list: %s", res.RelNone)
	}
	if strings.Contains(res.RelNone, ">zzclass<") || !strings.Contains(res.RelAll, "zzclass") {
		t.Errorf("(none) filter keeps only class-less failures; no filter keeps both")
	}
	if !strings.Contains(res.ActOutcome, "previous period total") || !strings.Contains(res.ActToday, "same time yesterday total") {
		t.Errorf("comparison legend names its window")
	}
	if strings.Contains(res.ActSplit, "d-prev-key") {
		t.Errorf("a split chart draws no comparison line, so it must not name one")
	}
	if !strings.Contains(res.KpiGap, "no token record") {
		t.Errorf("tokens card must report requests without a token record: %s", res.KpiGap)
	}
	if strings.Contains(res.KpiClean, "no token record") {
		t.Errorf("no gap, no note")
	}
}

// The strip's live cell: one line per in-flight request, stalled first, three
// lines reserved; idle says so and names the last request. The table below
// never opens by itself (checked in the page-structure test).
func TestConsoleRender_LiveStripLines(t *testing.T) {
	node := requireNode(t)
	now := time.Now()
	pageJS := extractScriptBody(t, readSourceFile(t, "status.html"))
	tail := `
const t0 = Date.now();
const iso = ms => new Date(t0 + ms).toISOString();
const run = (seq, extra) => Object.assign({ seq, state: 'running', protocol: 'anthropic', vmodel: 'agent', stream: true, client_key_tag: 'c' + seq, ts: iso(-4000), attempt: 1 }, extra);
const lines = (inflight, ended, waiting) => liveLinesHTML(inflight, ended, waiting, t0);
process.stdout.write(JSON.stringify({
  idle: lines([], [], 0),
  idleLast: lines([], [{ seq: 7, vmodel: 'coding', client_key_tag: 'alice', ts: iso(-60000), ended_at: iso(-30000), state: 'ended' }], 0),
  busy: lines([run(1), run(2, { attempt: 2 })], [], 0),
  stalled: lines([run(1), run(2, { last_byte_at: iso(-9000) })], [], 0),
  many: lines([run(1), run(2), run(3), run(4), run(5)], [], 0),
  queuedOnly: lines([], [], 3),
  stallAge: [stallAge(run(1, { last_byte_at: iso(-9000) }), t0), stallAge(run(1, { last_byte_at: iso(-1000) }), t0), stallAge(run(1, { stream: false, last_byte_at: iso(-9000) }), t0)],
}));
process.exit(0);
`
	src := buildHarness(t, statusFixture(now), liveFixture(now), "", formattingSliceJS(t), pageJS, tail)
	var res struct {
		Idle, IdleLast, Busy, Stalled, Many, QueuedOnly string
		StallAge                                        []int
	}
	if err := json.Unmarshal(runNode(t, node, src), &res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Idle, "Idle") || strings.Contains(res.Idle, "last:") {
		t.Errorf("idle = %s", res.Idle)
	}
	if !strings.Contains(res.IdleLast, "last:") || !strings.Contains(res.IdleLast, "alice") || !strings.Contains(res.IdleLast, "30s") {
		t.Errorf("idle with a last request = %s", res.IdleLast)
	}
	if strings.Count(res.Busy, `class="live-line"`) != 2 || !strings.Contains(res.Busy, "#2") {
		t.Errorf("busy = %s", res.Busy)
	}
	if first := strings.Index(res.Stalled, "stalled"); first < 0 || first > strings.Index(res.Stalled, "c1") {
		t.Errorf("a stalled request must come first: %s", res.Stalled)
	}
	if strings.Count(res.Many, `class="live-line"`) != 2 || !strings.Contains(res.Many, "+3 more") {
		t.Errorf("five requests show two lines plus a +3 more line: %s", res.Many)
	}
	if !strings.Contains(res.QueuedOnly, "3 waiting for a slot") {
		t.Errorf("queued only = %s", res.QueuedOnly)
	}
	if res.StallAge[0] != 9 || res.StallAge[1] != 0 || res.StallAge[2] != 0 {
		t.Errorf("stall age = %v: only a stream silent for 5 s or more is stalled", res.StallAge)
	}
}

// Dashboard layout decisions of the second review pass: Speed order and its
// p50 hero numbers, Breakdown tabs, Activity's split order and default, and a
// Reliability chart that exists on a clean range.
func TestConsoleRender_DashboardLayout(t *testing.T) {
	node := requireNode(t)
	tail := `
const D = VMRDash;
const cols = names => names.map(n => ({ name: n }));
const kpiCols = ['req','ok','canceled','err','err_rate','cancel_rate','tok_total','tok_fresh','tok_cache_write','tok_cache_read','tok_out','cache_hit','ttft_p50','ttft_p90','dur_p50','dur_p90','toks_p50','toks_p10'];
const vals = { ttft_p50: 400, ttft_p90: 900, dur_p50: 2000, dur_p90: 5000, toks_p50: 80, toks_p10: 20, req: 10, ok: 10 };
const kpi = { columns: cols(kpiCols), totals: kpiCols.map(c => vals[c] != null ? vals[c] : 0), rows: [] };
const b = ['2026-10-19T10:00:00+08:00', '2026-10-19T10:15:00+08:00', '2026-10-19T10:30:00+08:00'];
const ts = { columns: cols(['bucket','req','tok_fresh','tok_cache_write','tok_cache_read','tok_out','cache_hit']), rows: b.map(x => [x, 1, 1, 0, 1, 1, 0.5]), query: { step: '15m' } };
// A clean range: no error or canceled rows exist at all.
const outcome = { columns: cols(['bucket','outcome','req']), rows: b.map(x => [x, 'ok', 3]) };
const d = { kpi, ts, outcome, failover: { columns: cols(['outcome','req']), rows: [] }, speed: { columns: cols(['bucket', ...kpiCols.slice(12)]), rows: b.map(x => [x, 400, 900, 2000, 5000, 80, 20]) } };
const idx = (s, k) => s.indexOf(k);
const speed = D.renderSpeed(d, { cmp: '0', range: '1d' });
const st = D.loadState('');
const act = D.renderActivity({ outcome, ts, split: { 'client:req': { columns: cols(['bucket','client','req']), rows: b.map(x => [x, 'a', 3]) } } }, st, '\u0000other', 900).html;
process.stdout.write(JSON.stringify({
  speed, speedOrder: [idx(speed, 'Tok/s'), idx(speed, 'TTFT'), idx(speed, 'Duration')],
  kpis: D.renderKPIs(d, { cmp: '0', range: '1d' }),
  bd: D.renderBreakdown({ columns: cols(['client', ...kpiCols]), rows: [], totals: [] }, { bd: 'client', cols: 'summary', cmp: '0', range: '1d' }, '\u0000other'),
  state: st, hashOld: D.loadState('#range=24h&bd=key_label&where=key_label%3Ak1&where=client%3Aa'),
  act, actTok: D.renderActivity({ outcome, ts, split: {} }, Object.assign({}, st, { act: 'tok', split: 'type' }), '\u0000other', 900).html,
  actStatus: D.renderActivity({ outcome, ts, split: {} }, Object.assign({}, st, { split: 'outcome' }), '\u0000other', 900).html,
}));
`
	src := buildHarness(t, map[string]any{}, map[string]any{}, "", formattingSliceJS(t), "", tail)
	var res struct {
		Speed                            string
		SpeedOrder                       []int
		Kpis, Bd, Act, ActTok, ActStatus string
		State, HashOld                   struct {
			Range, Split, Bd string
			Where            []string
		}
	}
	if err := json.Unmarshal(runNode(t, node, src), &res); err != nil {
		t.Fatal(err)
	}
	o := res.SpeedOrder
	if !(o[0] >= 0 && o[0] < o[1] && o[1] < o[2]) {
		t.Errorf("Speed order must be Tok/s, TTFT, Duration: %v", o)
	}
	for _, want := range []string{"<b>80", "<b>400ms", "<b>2s", "median · p90 900ms", "median · p10 20"} {
		if !strings.Contains(res.Speed, want) {
			t.Errorf("Speed hero numbers are p50 with the tail beside them: missing %q in %s", want, res.Speed)
		}
	}
	if strings.Contains(res.Kpis, "d-compbar") {
		t.Errorf("the Tokens card has no composition bar")
	}
	if strings.Count(res.Kpis, "d-spark") != 4 || !strings.Contains(res.Kpis, "var(--d-cancel)") {
		t.Errorf("all four headline cards carry a chart, Reliability's included even with no failure in range")
	}
	if strings.Contains(res.Bd, "Upstream keys") || !strings.Contains(res.Bd, "Virtual models") {
		t.Errorf("Breakdown tabs: %s", res.Bd)
	}
	if res.State.Range != "1d" || res.State.Split != "client" {
		t.Errorf("defaults = %+v, want range 1d split client", res.State)
	}
	if res.HashOld.Range != "1d" || res.HashOld.Bd != "client" || len(res.HashOld.Where) != 1 || res.HashOld.Where[0] != "client:a" {
		t.Errorf("an old 24h / key_label link must still open: %+v", res.HashOld)
	}
	iCaller, iEndpoint, iStatus := strings.Index(res.Act, ">caller<"), strings.Index(res.Act, ">endpoint<"), strings.Index(res.Act, ">status<")
	if !(iCaller >= 0 && iCaller < iEndpoint && iEndpoint < iStatus) || !strings.Contains(res.Act, `data-k="client">caller`) || !strings.Contains(res.Act, "faults") {
		t.Errorf("requests split buttons are caller / endpoint / status, the default is caller, and the faults rail runs under it: %s", res.Act)
	}
	if !strings.Contains(res.ActTok, ">token type<") || !strings.Contains(res.ActStatus, `data-legend="ok"`) {
		t.Errorf("tokens keep token type as the third split; status shows ok/canceled/error")
	}
}
