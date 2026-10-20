/* dashboard.js — the Overview's statistics dashboard: one global range and
   filter set shared by fixed panels (headline cards, Activity, Composition,
   Breakdown, Speed, Reliability, Usage pattern). Every number is a
   /stats/* result; this file only picks the queries, lays them out and
   draws them as inline SVG.

   The renderers are pure string builders over query results, so they run
   under Node in the server package's render tests; only mount() touches
   the DOM. Depends on console.js globals (esc, fmtInt, fmtKMG, fmtRate,
   dec2, toast) and on stats.js (VMRStats), resolved at call time. */
'use strict';

const VMRDash = (() => {
  // ---------------- constants ----------------

  const RANGES = ['1h', '6h', '12h', 'today', '1d', '3d', '7d', '30d'];
  const HEAT_RANGES = ['7d', '30d'];
  const FILTER_DIMS = ['client', 'endpoint', 'vmodel'];
  const BD_DIMS = [['client', 'Callers'], ['endpoint', 'Endpoints'], ['vmodel', 'Virtual models']];
  const DEFAULTS = { range: '1d', cmp: '1', act: 'req', split: 'client', bd: 'client', cols: 'summary' };
  const SERIES_TOP = 6;
  // Activity's split choices, in display order; the default is the first. For
  // requests the third is the outcome (labelled status: ok / canceled /
  // error), for tokens it is the token type.
  const SPLITS = { req: ['client', 'endpoint', 'outcome'], tok: ['client', 'endpoint', 'type'] };
  const SPLIT_LABEL = { client: 'caller', endpoint: 'endpoint', outcome: 'status', type: 'token type' };
  const PANELS = ['activity', 'comp', 'breakdown', 'speed', 'rel', 'heat'];
  // Categorical colours never reuse the outcome colours, so a green bar
  // always means "ok" on this page.
  const CAT = ['--accent', '--purple', '--cyan', '--pink', '--orange', '--d-cat6'];
  const OTHER_COLOR = '--text-dim';
  const KPI_METRICS = ['req', 'ok', 'canceled', 'err', 'err_rate', 'cancel_rate', 'tok_total', 'tok_fresh', 'tok_cache_write',
    'tok_cache_read', 'tok_out', 'cache_hit', 'ttft_p50', 'ttft_p90', 'dur_p50', 'dur_p90', 'toks_p50', 'toks_p10'];
  const TS_METRICS = ['req', 'tok_fresh', 'tok_cache_write', 'tok_cache_read', 'tok_out', 'cache_hit'];
  const SPEED_METRICS = ['ttft_p50', 'ttft_p90', 'dur_p50', 'dur_p90', 'toks_p50', 'toks_p10'];
  const BD_METRICS = ['req', 'ok', 'err', 'canceled', 'share_req', 'tok_total', 'tok_prompt', 'tok_fresh', 'tok_cache_write',
    'tok_cache_read', 'tok_out', 'share_tok', 'cache_hit', 'dur_p50', 'dur_p90', 'ttft_p50', 'ttft_p90', 'toks_p50', 'toks_p10'];
  const COLSETS = {
    summary: [['req', 'Requests', 'Booked requests; the bar is the share of all requests'], ['dreq', 'Δ req', 'Change against the previous period'],
      ['errc', 'Err / Cancel', 'Failed / client-canceled requests'], ['tok_prompt', 'Tok in (hit)', 'Input tokens (cache-read share); hover for the split'],
      ['tok_out', 'Tok out', ''], ['share_tok', 'Tok share', 'Share of all tokens'],
      ['dur_p50', 'Duration p50 / p90', 'Successful requests, JSON included'], ['ttft_p50', 'TTFT p50 / p90', 'Successful streamed requests'],
      ['toks_p50', 'Tok/s p50 / p10', 'Successful streamed requests; p10 is the slow floor']],
    tokens: [['req', 'Requests', ''], ['tok_fresh', 'Fresh in', ''], ['tok_cache_write', 'Cache write', ''], ['tok_cache_read', 'Cache read', ''],
      ['cache_hit', 'Cache hit', ''], ['tok_out', 'Out', ''], ['tok_total', 'Total', ''], ['share_tok', 'Tok share', '']],
  };
  // Noise bands below which a change reads as flat (experience-based, not
  // statistical): relative for amounts and durations, absolute points for
  // ratios.
  const FLAT_REL = 0.05, FLAT_PP = 0.3;
  // One rule for every range: compare with the previous cycle of the same
  // range, one cycle back. A range's cycle is its own length (1h → the hour
  // before, 7d → the 7 days before); "today" is a calendar day, so its
  // previous cycle is yesterday up to the same time of day, not the stretch
  // before today's midnight.
  const compareMode = st => st.cmp !== '1' ? false : st.range === 'today' ? 'day' : 'prev';
  const cmpWordOf = st => st.range === 'today' ? 'same time yesterday' : 'previous period';
  // The word the change badges and tooltips use; set by each renderer from
  // its state before it builds markup.
  let CMP = 'previous period';
  const useCmp = st => { CMP = cmpWordOf(st); };

  // ---------------- small helpers ----------------

  const attr = s => VMRStats.attr(s);
  const fmtMS = ms => ms == null ? '—' : VMRStats.fmtMS(ms);
  const fmtN = v => v == null ? '—' : fmtKMG(v);
  const fmtP = (r, d) => r == null ? '—' : (r * 100).toFixed(d == null ? 1 : d) + '%';
  const fmtR = v => v == null ? '—' : fmtRate(v);
  const sum = a => a.reduce((s, v) => s + (v || 0), 0);
  const col = (res, name) => (res && res.columns || []).findIndex(c => c.name === name);
  const tot = (res, name) => { const i = col(res, name); return i >= 0 && res.totals ? res.totals[i] : null; };
  const prevTot = (res, name) => { const i = col(res, name); return i >= 0 && res.compare ? res.compare.totals[i] : null; };
  const series = (res, name) => { const i = col(res, name); return (res && res.rows || []).map(r => i >= 0 ? r[i] : null); };
  const prevSeries = (res, name) => { const i = col(res, name); return (res && res.compare && res.compare.rows || []).map(r => i >= 0 ? r[i] : null); };
  const v = name => `var(${name})`;

  // delta renders the change badge. kind: 'volume' (neutral colour — more
  // traffic is not bad), 'up' (higher is better), 'down' (lower is better).
  // opts.pp compares ratios in percentage points.
  function delta(cur, prev, kind, opts) {
    const o = opts || {};
    if (cur == null || prev == null) return '';
    if (o.pp) {
      const d = (cur - prev) * 100;
      if (Math.abs(d) < (o.band != null ? o.band : FLAT_PP)) return `<span class="d-delta flat" title="vs ${CMP}">≈ ${d >= 0 ? '+' : ''}${d.toFixed(2)}pp</span>`;
      const good = kind === 'up' ? d > 0 : d < 0;
      return `<span class="d-delta ${good ? 'good' : 'bad'}" title="vs ${CMP}">${d > 0 ? '▲' : '▼'} ${Math.abs(d).toFixed(2)}pp</span>`;
    }
    if (!prev) return cur ? `<span class="d-delta flat" title="nothing in the ${CMP}">new</span>` : '';
    const r = (cur - prev) / prev;
    const txt = `${r > 0 ? '▲' : '▼'} ${Math.abs(r * 100).toFixed(1)}%`;
    if (Math.abs(r) < FLAT_REL) return `<span class="d-delta flat" title="vs ${CMP}">≈ ${(r * 100).toFixed(1)}%</span>`;
    if (kind === 'volume') return `<span class="d-delta neutral" title="vs ${CMP}">${txt}</span>`;
    const good = kind === 'up' ? r > 0 : r < 0;
    return `<span class="d-delta ${good ? 'good' : 'bad'}" title="vs ${CMP}">${txt}</span>`;
  }

  function niceMax(x) {
    if (!(x > 0)) return 1;
    const e = Math.pow(10, Math.floor(Math.log10(x))), f = x / e;
    return [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10].find(s => f <= s + 1e-9) * e;
  }

  // wall reads an RFC3339 bucket's wall clock off the string (server time,
  // never re-zoned in the browser — same rule as stats.js).
  function wall(iso) {
    const m = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d)/.exec(iso || '');
    if (!m) return { day: '', hm: '', h: 0, m: 0, wd: '' };
    const wd = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][new Date(Date.UTC(+m[1], +m[2] - 1, +m[3])).getUTCDay()];
    return { day: `${m[2]}-${m[3]}`, hm: `${m[4]}:${m[5]}`, h: +m[4], m: +m[5], wd };
  }

  // pivot turns a bucketed, grouped result into one value array per group,
  // groups ordered by their total (the "other" group last).
  function pivot(res, dim, metric, other) {
    const bi = col(res, 'bucket'), di = col(res, dim), mi = col(res, metric);
    const buckets = [], bpos = new Map(), groups = new Map();
    (res && res.rows || []).forEach(r => {
      if (!bpos.has(r[bi])) { bpos.set(r[bi], buckets.length); buckets.push(r[bi]); }
    });
    (res && res.rows || []).forEach(r => {
      const g = r[di];
      if (!groups.has(g)) groups.set(g, new Array(buckets.length).fill(0));
      groups.get(g)[bpos.get(r[bi])] = r[mi] || 0;
    });
    const names = [...groups.keys()].sort((a, b) => (a === other) - (b === other) || sum(groups.get(b)) - sum(groups.get(a)));
    return { buckets, groups: names.map(n => [n, groups.get(n)]) };
  }

  // axisTicks picks the labelled buckets of a time axis: ranges spanning
  // three or more midnights tick at midnights, shorter ones at even bucket
  // strides; either way thinned to one label per ~90px of plot width.
  function axisTicks(buckets, pw) {
    const multiDay = buckets.filter(b => wall(b).hm === '00:00').length >= 3;
    const ticks = buckets.map((b, i) => i).filter(i => !multiDay || wall(buckets[i]).hm === '00:00');
    const stride = Math.max(1, Math.ceil(ticks.length / Math.max(2, Math.floor(pw / 90))));
    return ticks.filter((_, j) => j % stride === 0).map(i => {
      const w = wall(buckets[i]), prevDay = i > 0 ? wall(buckets[i - 1]).day : w.day;
      return { i, label: multiDay ? `${w.wd} ${w.day}` : (w.day !== prevDay || w.hm === '00:00' ? w.day + ' ' : '') + w.hm };
    });
  }

  // ---------------- SVG renderers (pure) ----------------

  function sparkSVG(vals, prev, color) {
    const W = 300, H = 38, n = vals.length;
    if (n < 2) return `<svg class="d-spark" viewBox="0 0 ${W} ${H}" preserveAspectRatio="none"></svg>`;
    const mx = Math.max(1e-9, ...vals.map(x => x || 0), ...(prev || []).map(x => x || 0));
    const pts = a => a.slice(0, n).map((x, i) => `${(i / (n - 1) * W).toFixed(1)},${(H - 2 - (x || 0) / mx * (H - 6)).toFixed(1)}`).join(' ');
    return `<svg class="d-spark" viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" aria-hidden="true">` +
      (prev && prev.length ? `<polyline points="${pts(prev)}" class="d-prev" vector-effect="non-scaling-stroke"/>` : '') +
      `<polygon points="0,${H} ${pts(vals)} ${W},${H}" style="fill:${color};opacity:.14"/>` +
      `<polyline points="${pts(vals)}" style="fill:none;stroke:${color};stroke-width:1.5" vector-effect="non-scaling-stroke"/></svg>`;
  }

  // stackedSVG draws stacked bars. ser: [{key,label,color,vals}] (visible
  // only); o: {width, height, fmt, prev, faults:{error,canceled}}. Returns
  // the markup plus the geometry the hover handler needs.
  function stackedSVG(buckets, ser, o) {
    const W = Math.max(320, o.width || 900), H = o.height || 300, L = 50, R = 8, T = 8, B = o.faults ? 42 : 26, n = buckets.length;
    const pw = W - L - R, ph = H - T - B, bw = n ? pw / n : pw;
    const sums = buckets.map((_, i) => ser.reduce((s, x) => s + (x.vals[i] || 0), 0));
    const mx = niceMax(Math.max(0, ...sums, ...(o.prev || []).map(x => x || 0)) * 1.04);
    const y = val => T + ph - val / mx * ph;
    let g = '';
    for (let t = 0; t <= 4; t++) {
      const val = mx * t / 4;
      g += `<line x1="${L}" x2="${W - R}" y1="${y(val).toFixed(1)}" y2="${y(val).toFixed(1)}" class="${t ? 'd-grid' : 'd-axis'}"/>` +
        `<text x="${L - 6}" y="${(y(val) + 3).toFixed(1)}" text-anchor="end">${esc(o.fmt(val))}</text>`;
    }
    axisTicks(buckets, pw).forEach(({ i, label }) => {
      const x = L + i * bw;
      g += `<line x1="${x.toFixed(1)}" x2="${x.toFixed(1)}" y1="${T}" y2="${T + ph}" class="d-grid"/>` +
        `<text x="${(x + 3).toFixed(1)}" y="${T + ph + 13}">${esc(label)}</text>`;
    });
    let bars = '';
    const gap = bw > 4 ? bw * 0.14 : 0;
    buckets.forEach((_, i) => {
      let acc = 0;
      for (const s of ser) {
        const val = s.vals[i] || 0;
        if (!val) continue;
        const y0 = y(acc), y1 = y(acc + val);
        acc += val;
        bars += `<rect x="${(L + i * bw + gap / 2).toFixed(1)}" y="${y1.toFixed(1)}" width="${Math.max(0.8, bw - gap).toFixed(1)}" height="${Math.max(0.6, y0 - y1).toFixed(1)}" style="fill:${s.color}"/>`;
      }
    });
    let prev = '';
    if (o.prev && o.prev.length) {
      prev = `<polyline class="d-prev" points="${o.prev.slice(0, n).map((x, i) => `${(L + i * bw + bw / 2).toFixed(1)},${y(x || 0).toFixed(1)}`).join(' ')}"/>`;
    }
    let faults = '';
    if (o.faults) {
      const fy = T + ph + 27;
      faults = `<text x="${L - 6}" y="${fy + 3}" text-anchor="end">faults</text><line x1="${L}" x2="${W - R}" y1="${fy}" y2="${fy}" class="d-grid"/>`;
      buckets.forEach((_, i) => {
        const e = (o.faults.error || [])[i] || 0, c = (o.faults.canceled || [])[i] || 0;
        if (!e && !c) return;
        const cx = (L + i * bw + bw / 2).toFixed(1);
        faults += e
          ? `<circle cx="${cx}" cy="${fy}" r="${Math.min(6, 2.5 + e)}" style="fill:var(--d-err)"/>`
          : `<circle cx="${cx}" cy="${fy}" r="${Math.min(5, 1.8 + c * 0.6).toFixed(1)}" style="fill:var(--d-cancel);opacity:.85"/>`;
      });
    }
    const svg = `<svg viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" class="d-chart-svg">${g}${bars}${prev}${faults}` +
      `<rect class="d-hover" x="0" y="${T}" width="${bw.toFixed(1)}" height="${ph}"/></svg>`;
    return { svg, geom: { W, L, bw, n } };
  }

  // lineSVG draws a small multiple: lines [{vals, color, dash}].
  function lineSVG(buckets, lines, fmt) {
    const W = 400, H = 120, L = 44, R = 6, T = 6, B = 18, n = buckets.length, pw = W - L - R, ph = H - T - B;
    const all = lines.flatMap(l => l.vals.filter(x => x != null));
    if (!all.length) return `<div class="d-empty">No successful requests with this measure in the range.</div>`;
    const mx = niceMax(Math.max(...all) * 1.05);
    const x = i => L + (n > 1 ? i / (n - 1) : 0.5) * pw, y = val => T + ph - val / mx * ph;
    let g = '';
    for (let t = 0; t <= 2; t++) {
      const val = mx * t / 2;
      g += `<line x1="${L}" x2="${W - R}" y1="${y(val).toFixed(1)}" y2="${y(val).toFixed(1)}" class="${t ? 'd-grid' : 'd-axis'}"/><text x="${L - 5}" y="${(y(val) + 3).toFixed(1)}" text-anchor="end">${esc(fmt(val))}</text>`;
    }
    axisTicks(buckets, pw).forEach(({ i, label }) => {
      g += `<text x="${x(i).toFixed(1)}" y="${H - 4}" text-anchor="middle">${esc(label)}</text>`;
    });
    for (const l of lines) {
      let d = '', pen = false;
      l.vals.forEach((val, i) => {
        if (val == null) { pen = false; return; }
        d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)},${y(val).toFixed(1)}`;
        pen = true;
      });
      // A lone sample between gaps has no segment to draw: mark it.
      const dots = l.vals.map((val, i) => val != null && l.vals[i - 1] == null && l.vals[i + 1] == null
        ? `<circle cx="${x(i).toFixed(1)}" cy="${y(val).toFixed(1)}" r="2" style="fill:${l.color}"/>` : '').join('');
      g += `<path d="${d}" style="fill:none;stroke:${l.color};stroke-width:1.6${l.dash ? ';stroke-dasharray:4 3' : ''}" stroke-linejoin="round" vector-effect="non-scaling-stroke"/>${dots}`;
    }
    return `<svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none" class="d-small">${g}</svg>`;
  }

  // ---------------- cancellation timing (pure) ----------------

  const CANCEL_BINS = [['<5s', 5e3], ['5–10s', 1e4], ['10–30s', 3e4], ['30–60s', 6e4], ['1–2m', 12e4], ['≥2m', Infinity]];

  function cancelHistogram(durs) {
    const counts = CANCEL_BINS.map(() => 0);
    durs.forEach(ms => { counts[CANCEL_BINS.findIndex(b => ms < b[1])]++; });
    return CANCEL_BINS.map((b, i) => [b[0], counts[i]]);
  }

  // timeoutClusters finds cancellations that stop within a second of a whole
  // multiple of 30 s, three or more at the same mark: the signature of a
  // caller-side timeout (60 s, 90 s, 120 s ...) rather than of an upstream
  // failure. The window is two-sided because the caller starts its clock
  // before vmr sees the request, so the recorded time can fall just short.
  function timeoutClusters(durs) {
    const marks = new Map();
    durs.forEach(ms => {
      const k = Math.round(ms / 30000);
      if (k >= 1 && Math.abs(ms - k * 30000) < 1000) marks.set(k * 30, (marks.get(k * 30) || 0) + 1);
    });
    return [...marks.entries()].filter(([, n]) => n >= 3).sort((a, b) => a[0] - b[0]).map(([s, n]) => ({ sec: s, n }));
  }

  // ---------------- panel renderers (pure) ----------------

  function seg(name, items, cur) {
    return `<span class="seg d-seg" data-seg="${attr(name)}">` +
      items.map(([k, l]) => `<button class="btn sm ${k === cur ? 'on' : ''}" data-k="${attr(k)}">${esc(l)}</button>`).join('') + '</span>';
  }

  function kpiCard(go, label, right, value, extra, lines, spark) {
    return `<div class="card d-kpi" data-go="${go}" title="Jump to the panel">
      <div class="d-kpi-top"><span class="v-label">${label}</span>${right}</div>
      <div class="d-kpi-v">${value}</div>${extra}
      <div class="d-kpi-lines">${lines.map(([a, b]) => `<div><span>${a}</span><span>${b}</span></div>`).join('')}</div>${spark}</div>`;
  }

  function healthOf(errRate) {
    if (errRate == null) return ['idle', 'dim'];
    if (errRate < 0.01) return ['healthy', 'ok'];
    if (errRate < 0.05) return ['degraded', 'warn'];
    return ['failing', 'err'];
  }

  // tokenGap reports the forwarded requests of a result that have no token
  // record, for the window and for its comparison window.
  const tokenGap = res => ({ now: (res && res.meta && res.meta.tokens_unknown) || 0, prev: (res && res.compare && res.compare.tokens_unknown) || 0 });

  function renderKPIs(d, st) {
    useCmp(st);
    const k = d.kpi, ts = d.ts, cmp = st.cmp === '1';
    const gap = tokenGap(k), tokDelta = gap.now || gap.prev ? '' : delta(tot(k, 'tok_total') || 0, prevTot(k, 'tok_total'), 'volume');
    const g = n => tot(k, n), p = n => cmp ? prevTot(k, n) : null;
    const req = g('req') || 0, T = g('tok_total') || 0;
    const tokOf = res => (res && res.rows || []).map(r => ['tok_fresh', 'tok_cache_write', 'tok_cache_read', 'tok_out'].reduce((s, m) => s + (r[col(res, m)] || 0), 0));
    const prevTok = cmp && ts.compare ? ts.compare.rows.map(r => ['tok_fresh', 'tok_cache_write', 'tok_cache_read', 'tok_out'].reduce((s, m) => s + (r[col(ts, m)] || 0), 0)) : null;
    const fo = failover(d.failover);
    const today = st.range !== 'today' && d.today ? [['today so far', fmtInt(tot(d.today, 'req') || 0)]] : [];
    const [health, hcls] = healthOf(req ? g('err_rate') : null);
    // Failures per bucket. An outcome with no event in the range has no group
    // in the result, so index by the buckets rather than by a group's array —
    // otherwise a clean range would draw no chart at all.
    const fails = d.outcome ? (() => { const pv = pivot(d.outcome, 'outcome', 'req'); const m = new Map(pv.groups); const e = m.get('error') || [], c = m.get('canceled') || []; return pv.buckets.map((_, i) => (e[i] || 0) + (c[i] || 0)); })() : [];
    return [
      kpiCard('dash-activity', 'Requests', delta(req, p('req'), 'volume'), fmtInt(req), '',
        [['ok · canceled · error', `${fmtInt(g('ok') || 0)} · <span class="t-warn">${fmtInt(g('canceled') || 0)}</span> · <span class="t-err">${fmtInt(g('err') || 0)}</span>`]].concat(today),
        sparkSVG(series(ts, 'req'), cmp ? prevSeries(ts, 'req') : null, v('--accent'))),
      kpiCard('dash-comp', 'Tokens', cmp ? tokDelta : '', fmtN(T), '',
        [['cache hit', `${fmtP(g('cache_hit'))} ${delta(g('cache_hit'), p('cache_hit'), 'up', { pp: true })}`], ['fresh in · out', `${fmtN(g('tok_fresh'))} · ${fmtN(g('tok_out'))}`]]
          .concat(gap.now ? [[`<span title="Forwarded requests with no token record (never stamped): left out of every token figure, so those are lower bounds">no token record</span>`, `<span class="t-warn">${fmtInt(gap.now)}</span>`]] : [])
          .concat(!gap.now && gap.prev && cmp ? [['change vs previous', `<span class="t-dim" title="${attr(`${fmtInt(gap.prev)} requests in the comparison window have no token record, so the change would mislead`)}">n/a</span>`]] : []),
        sparkSVG(tokOf(ts), prevTok, v('--d-read'))),
      kpiCard('dash-speed', 'Latency · TTFT p50', delta(g('ttft_p50'), p('ttft_p50'), 'down'), fmtMS(g('ttft_p50')), '',
        [['TTFT p90', `${fmtMS(g('ttft_p90'))} ${delta(g('ttft_p90'), p('ttft_p90'), 'down')}`], ['duration p50 (JSON incl.)', `${fmtMS(g('dur_p50'))} ${delta(g('dur_p50'), p('dur_p50'), 'down')}`], ['tok/s p50 (streamed)', fmtR(g('toks_p50'))]],
        sparkSVG(d.speed ? series(d.speed, 'ttft_p50') : [], null, v('--purple'))),
      kpiCard('dash-rel', 'Reliability', `<span class="d-status t-${hcls}">${health}</span>`,
        `${req ? fmtP((g('ok') || 0) / req, 2) : '—'}<small> completed</small>`, '',
        [['errors · canceled', `<span class="t-err">${fmtInt(g('err') || 0)}</span> · <span class="t-warn">${fmtInt(g('canceled') || 0)}</span> ${delta(g('err_rate'), p('err_rate'), 'down', { pp: true, band: 0.1 })}`],
          ['failovers · rescued', `${fmtInt(fo.total)} · <span class="t-ok">${fmtInt(fo.rescued)}</span>${fo.total ? ` (${fmtP(fo.rescued / fo.total, 0)})` : ''}`]],
        sparkSVG(fails, null, v('--d-cancel'))),
    ].join('');
  }

  function failover(res) {
    const oi = col(res, 'outcome'), ri = col(res, 'req');
    let total = 0, rescued = 0;
    (res && res.rows || []).forEach(r => { total += r[ri] || 0; if (r[oi] === 'ok') rescued += r[ri] || 0; });
    return { total, rescued };
  }

  // activitySeries resolves the Activity chart's series for the current
  // metric/split from the loaded results.
  // faultsOf reads the per-bucket error and cancellation counts for the
  // faults rail, which runs under every requests split: a rare failure is a
  // pixel in a stacked bar whatever the bars are split by.
  function faultsOf(d) {
    const m = new Map(pivot(d.outcome, 'outcome', 'req').groups);
    return { error: m.get('error'), canceled: m.get('canceled') };
  }

  function activitySeries(d, st, other) {
    if (st.act === 'req' && st.split === 'outcome') {
      const pv = pivot(d.outcome, 'outcome', 'req'), m = new Map(pv.groups);
      return { buckets: pv.buckets, faults: faultsOf(d), dim: null,
        ser: [['ok', 'ok', '--d-ok'], ['canceled', 'canceled', '--d-cancel'], ['error', 'error', '--d-err']]
          .map(([key, label, c]) => ({ key, label, color: v(c), vals: m.get(key) || [] })) };
    }
    if (st.act === 'tok' && st.split === 'type') {
      return { buckets: series(d.ts, 'bucket'), dim: null,
        ser: [['tok_fresh', 'fresh in', '--d-fresh'], ['tok_cache_write', 'cache write', '--d-write'], ['tok_out', 'out', '--d-out'], ['tok_cache_read', 'cache read', '--d-read']]
          .map(([key, label, c]) => ({ key, label, color: v(c), vals: series(d.ts, key) })) };
    }
    const res = d.split[st.split + ':' + st.act];
    if (!res) return null;
    const pv = pivot(res, st.split, st.act === 'req' ? 'req' : 'tok_total', other);
    // A group with nothing to draw in this view (a caller whose requests carry
    // no token record, in the token view) stays out of the legend.
    return { buckets: pv.buckets, dim: st.split, faults: st.act === 'req' && d.outcome ? faultsOf(d) : undefined,
      ser: pv.groups.filter(([, vals]) => sum(vals) > 0).map(([name, vals], i) => ({ key: name, label: name === other ? 'other' : (name || (st.split === 'endpoint' ? '(unrouted)' : '(none)')),
        color: name === other ? v(OTHER_COLOR) : VMRStats.dimHue(st.split, name, other) || v(CAT[i % CAT.length]), vals })) };
  }

  function renderActivity(d, st, other, width) {
    useCmp(st);
    const head = `<div class="d-ph"><span class="d-title">Activity</span>
      ${seg('act', [['req', 'Requests'], ['tok', 'Tokens']], st.act)}
      <span class="seg-label">split by</span>
      ${seg('split', SPLITS[st.act].map(k => [k, SPLIT_LABEL[k]]), st.split)}
      <span class="spacer"></span><span class="hint">hover a bar for its numbers · click it for its requests</span></div>`;
    const A = activitySeries(d, st, other);
    if (!A) return { html: head + '<div class="d-empty">Loading…</div>', geom: null, A: null };
    if (!A.buckets.length) return { html: head + '<div class="d-empty">No requests in this range.</div>', geom: null, A };
    const readHidden = st.act === 'tok' && st.split === 'type' && !st.showRead;
    const hidden = s => st.off.has(s.key) || (readHidden && s.key === 'tok_cache_read');
    const total = sum(A.ser.map(s => sum(s.vals)));
    // The comparison line is a total, so it only means something against the
    // full stack: any split or hidden series leaves it out, and its legend
    // entry with it.
    let prev = null;
    if (st.cmp === '1' && d.ts.compare) {
      const keys = st.act === 'req' ? ['req'] : ['tok_fresh', 'tok_cache_write', 'tok_out'].concat(readHidden ? [] : ['tok_cache_read']);
      prev = d.ts.compare.rows.map(r => keys.reduce((s, m) => s + (r[col(d.ts, m)] || 0), 0));
      if (A.dim || st.off.size) prev = null;
    }
    const legend = A.ser.map(s => `<button class="${hidden(s) ? 'off' : ''}" data-legend="${attr(s.key)}" title="Show / hide"><span class="sw" style="background:${s.color}"></span>${esc(s.label)} <span class="t-dim">${fmtN(sum(s.vals))}</span></button>`).join('') +
      (st.act === 'tok' && st.split === 'type'
        ? `<span class="d-note">${readHidden ? `cache read hidden (${fmtP(total ? sum(series(d.ts, 'tok_cache_read')) / total : 0, 0)} of tokens) — axis rescaled <button data-read="1">show</button>` : '<button data-read="1">hide cache read</button>'}</span>` : '') +
      (prev ? `<span class="d-prev-key"><svg width="16" height="6"><line x1="0" y1="3" x2="16" y2="3" class="d-prev"/></svg> ${CMP} total</span>` : '');
    const vis = A.ser.filter(s => !hidden(s));
    const ch = stackedSVG(A.buckets, vis, { width, fmt: x => st.act === 'req' ? fmtN(Math.round(x)) : fmtN(x), prev, faults: A.faults });
    return { html: head + `<div class="d-legend">${legend}</div><div class="d-chart">${ch.svg}<div class="d-tip" hidden></div></div>`, geom: ch.geom, A };
  }

  function tipHTML(d, st, A, i) {
    const b = A.buckets[i], w = wall(b);
    const rows = A.ser.map(s => `<div class="r"><span><span class="sw" style="background:${s.color}"></span>${esc(s.label)}</span><span>${fmtN(s.vals[i] || 0)}</span></div>`).join('');
    const total = A.ser.reduce((s, x) => s + (x.vals[i] || 0), 0);
    let ext = '';
    if (A.faults) {
      const e = (A.faults.error || [])[i] || 0, c = (A.faults.canceled || [])[i] || 0;
      ext += `<div class="r"><span>error rate</span><span>${fmtP(total ? e / total : 0, 2)}</span></div><div class="r"><span>cancel rate</span><span>${fmtP(total ? c / total : 0, 2)}</span></div>`;
    }
    if (st.act === 'tok' && d.ts.rows[i]) ext += `<div class="r"><span>cache hit</span><span>${fmtP(d.ts.rows[i][col(d.ts, 'cache_hit')])}</span></div><div class="r"><span>requests</span><span>${fmtInt(d.ts.rows[i][col(d.ts, 'req')] || 0)}</span></div>`;
    return `<div class="t">${esc(w.wd + ' ' + w.day + ' ' + w.hm)}</div>${rows}<div class="sep"></div><div class="r"><span>total</span><b>${fmtN(total)}</b></div>${ext}<div class="hint">click → requests in this bucket</div>`;
  }

  function renderComposition(d) {
    const k = d.kpi, T = tot(k, 'tok_total') || 0, gap = tokenGap(k);
    const rows = [['fresh in', '--d-fresh', 'tok_fresh'], ['cache write', '--d-write', 'tok_cache_write'], ['cache read', '--d-read', 'tok_cache_read'], ['out', '--d-out', 'tok_out']];
    const yes = res => { const r = (res && res.rows || []).find(x => x[0] === 'true'); return r ? { req: r[col(res, 'share_req')], tok: r[col(res, 'share_tok')] } : { req: 0, tok: 0 }; };
    const mix = [['streamed', yes(d.mix.stream)], ['with tools', yes(d.mix.has_tools)], ['with image', yes(d.mix.has_image)]];
    const s = mix[0][1];
    const insight = s.req > 0 && s.tok - s.req >= 0.2
      ? `<div class="d-insight">${fmtP(s.req, 0)} of requests are streamed but carry <b>${fmtP(s.tok, 0)}</b> of tokens — TTFT and Tok/s describe most of the volume, not most of the requests.</div>` : '';
    return `<div class="d-ph"><span class="d-title">Composition</span><span class="spacer"></span><span class="hint">same range &amp; filters</span></div>
      <div class="d-blk"><div class="d-h4">Where tokens go</div>${gap.now ? `<div class="d-insight warn">${fmtInt(gap.now)} forwarded requests have no token record (they predate token stamping) and are left out — these figures are lower bounds.</div>` : ''}${rows.map(([l, c, m]) => { const x = tot(k, m) || 0;
        return `<div class="d-tokrow"><span class="sw" style="background:${v(c)}"></span><span>${l}</span><span class="n">${x ? fmtN(x) : '<span class="t-dim">0</span>'}</span><span class="p">${fmtP(T ? x / T : 0)}</span></div>`; }).join('')}</div>
      <div class="d-blk"><div class="d-h4">Request mix <span class="t-dim">· share that is…</span></div><div class="d-mix">${mix.map(([l, x]) =>
        `<div>${l}</div><div class="bars"><div class="bar"><i class="rq" style="width:${(x.req * 100).toFixed(1)}%"></i><span>${fmtP(x.req, 0)} <em>of requests</em></span></div><div class="bar"><i class="tk" style="width:${(x.tok * 100).toFixed(1)}%"></i><span>${fmtP(x.tok, 0)} <em>of tokens</em></span></div></div>`).join('')}</div>${insight}</div>`;
  }

  function renderBreakdown(res, st, other) {
    useCmp(st);
    const head = `<div class="d-ph"><span class="d-title">Breakdown</span>${seg('bd', BD_DIMS, st.bd)}<span class="spacer"></span>
      <span class="seg-label">columns</span>${seg('cols', [['summary', 'summary'], ['tokens', 'token detail']], st.cols)}</div>`;
    if (!res) return head + '<div class="d-empty">Loading…</div>';
    const I = n => col(res, n);
    const cmp = st.cmp === '1' && res.compare ? new Map(res.compare.rows.map(r => [r[0], r])) : null;
    const cols = COLSETS[st.cols].filter(c => c[0] !== 'dreq' || cmp);
    const val = (r, key) => key === 'dreq' ? r[I('req')] - (cmp.get(r[0]) ? cmp.get(r[0])[I('req')] : 0)
      : key === 'errc' ? r[I('err')] + r[I('canceled')] : r[I(key)];
    const rows = (res.rows || []).slice();
    if (st.sort) rows.sort((a, b) => { const x = val(a, st.sort.k), y = val(b, st.sort.k); return x === y ? 0 : x == null ? 1 : y == null ? -1 : (x < y ? 1 : -1) * st.sort.d; });
    const pair = (a, b, f) => `${f(a)} <span class="t-dim">/ ${f(b)}</span>`;
    const bar = (txt, share) => `<span class="d-share">${txt}<span class="sharebar"><span style="width:${((share || 0) * 100).toFixed(1)}%"></span></span></span>`;
    const cell = (r, key, total) => {
      switch (key) {
        case 'req': return st.cols === 'summary' ? bar(fmtInt(r[I('req')] || 0), total ? 1 : r[I('share_req')]) : fmtInt(r[I('req')] || 0);
        case 'dreq': {
          if (total) { const pv = res.compare.totals[I('req')] || 0, dd = (r[I('req')] || 0) - pv; return `${dd >= 0 ? '+' : ''}${fmtInt(dd)}`; }
          const pr = cmp.get(r[0]);
          if (!pr) return '<span class="t-dim">new</span>';
          const dd = r[I('req')] - pr[I('req')];
          return `${dd >= 0 ? '+' : ''}${fmtInt(dd)} <span class="t-dim">${pr[I('req')] ? Math.round(dd / pr[I('req')] * 100) + '%' : ''}</span>`;
        }
        case 'errc': return `<span class="${r[I('err')] ? 't-err' : 't-dim'}">${fmtInt(r[I('err')] || 0)}</span> <span class="t-dim">/</span> <span class="${r[I('canceled')] ? 't-warn' : 't-dim'}">${fmtInt(r[I('canceled')] || 0)}</span>`;
        case 'tok_prompt': return `<span title="${attr(`fresh ${fmtN(r[I('tok_fresh')])} · cache write ${fmtN(r[I('tok_cache_write')])} · cache read ${fmtN(r[I('tok_cache_read')])}`)}">${fmtN(r[I('tok_prompt')])} <span class="t-dim">(${fmtP(r[I('cache_hit')], 0)})</span></span>`;
        case 'share_tok': return bar(fmtP(r[I('share_tok')]), r[I('share_tok')]);
        case 'dur_p50': return pair(r[I('dur_p50')], r[I('dur_p90')], fmtMS);
        case 'ttft_p50': return pair(r[I('ttft_p50')], r[I('ttft_p90')], fmtMS);
        case 'toks_p50': return pair(r[I('toks_p50')], r[I('toks_p10')], fmtR);
        case 'cache_hit': return fmtP(r[I('cache_hit')]);
        default: { const x = r[I(key)]; return x ? fmtN(x) : '<span class="t-dim">0</span>'; }
      }
    };
    const label = BD_DIMS.find(x => x[0] === st.bd)[1];
    const th = cols.map(([key, l, title]) => `<th class="num sortable ${st.sort && st.sort.k === key ? 'sorted' : ''}" data-sort="${key}" title="${attr(title.replace('previous period', CMP))}">${esc(l)}${st.sort && st.sort.k === key ? (st.sort.d < 0 ? ' ▾' : ' ▴') : ''}</th>`).join('');
    const body = rows.length ? rows.map(r => {
      const name = VMRStats.dimValue(st.bd, r[0], other);
      const nameCell = r[0] === other ? `<td>${name}</td>` : `<td class="filterable" data-dim="${attr(st.bd)}" data-val="${attr(r[0])}" title="Filter the dashboard to this value">${name}</td>`;
      return `<tr>${nameCell}${cols.map(([key]) => `<td class="num">${cell(r, key)}</td>`).join('')}<td class="num">${r[0] === other ? '' : `<a href="#" class="drill" data-dim="${attr(st.bd)}" data-val="${attr(r[0])}">requests →</a>`}</td></tr>`;
    }).join('') : `<tr><td colspan="${cols.length + 2}" class="empty-cell t-dim">No requests in this range.</td></tr>`;
    const foot = rows.length > 1 && res.totals ? `<tfoot><tr><td>total · ${rows.length}</td>${cols.map(([key]) => `<td class="num">${cell(res.totals, key, true)}</td>`).join('')}<td></td></tr></tfoot>` : '';
    return head + `<div class="table-wrap d-flush"><table class="c"><thead><tr><th>${esc(label.replace(/s$/, ''))}</th>${th}<th></th></tr></thead><tbody>${body}</tbody>${foot}</table></div>`;
  }

  function renderSpeed(d, st) {
    useCmp(st);
    const k = d.kpi, s = d.speed, cmp = st.cmp === '1';
    const b = s ? series(s, 'bucket') : [];
    const card = (title, sub, value, dl, lines, fmt, legend, tip) => `<div class="card d-sm"><div class="d-kpi-top"><span class="v-label">${title}</span>${cmp ? dl : ''}</div>
      <div class="d-sm-v"><b>${value}</b><span class="hint" title="${attr(tip)}">${sub}</span></div>${s ? lineSVG(b, lines, fmt) : '<div class="d-empty">Loading…</div>'}
      <div class="d-sm-lg">${legend.map(([c, l, dash]) => `<span><i class="${dash ? 'dash' : ''}" style="border-color:${c}"></i>${l}</span>`).join('')}</div></div>`;
    const sv = n => s ? series(s, n) : [];
    const pu = v('--purple'), ac = v('--accent'), gr = v('--green');
    // p50 is the median and the hero number everywhere. The tail is the
    // slow side: p90 is the value 90% of the requests are at or below (the
    // slowest 10% sit above it) for TTFT and duration; throughput is the
    // other way round — slow means low — so its tail is p10, the rate 90% of
    // the requests reach or beat.
    const tail = 'p50 = median. p90 = 90% of requests are at or below this (the slowest 10% sit above it).';
    const tailLow = 'p50 = median. p10 = 90% of requests reach at least this rate (the slowest 10% sit below it).';
    return [
      card('Tok/s · streamed', `median · p10 ${fmtR(tot(k, 'toks_p10'))}`, fmtR(tot(k, 'toks_p50')), delta(tot(k, 'toks_p50'), prevTot(k, 'toks_p50'), 'up'),
        [{ vals: sv('toks_p50'), color: gr }, { vals: sv('toks_p10'), color: gr, dash: 1 }], x => fmtR(x) === '—' ? '0' : fmtR(x), [[gr, 'p50 median'], [gr, 'p10 (slow floor)', 1]], tailLow),
      card('TTFT · streamed', `median · p90 ${fmtMS(tot(k, 'ttft_p90'))}`, fmtMS(tot(k, 'ttft_p50')), delta(tot(k, 'ttft_p50'), prevTot(k, 'ttft_p50'), 'down'),
        [{ vals: sv('ttft_p50'), color: pu }, { vals: sv('ttft_p90'), color: pu, dash: 1 }], fmtMS, [[pu, 'p50 median'], [pu, 'p90 (slow tail)', 1]], tail),
      card('Duration · all successful', `median · p90 ${fmtMS(tot(k, 'dur_p90'))}`, fmtMS(tot(k, 'dur_p50')), delta(tot(k, 'dur_p50'), prevTot(k, 'dur_p50'), 'down'),
        [{ vals: sv('dur_p50'), color: ac }, { vals: sv('dur_p90'), color: ac, dash: 1 }], fmtMS, [[ac, 'p50 median'], [ac, 'p90 (slow tail)', 1]], tail),
    ].join('');
  }

  function renderReliability(d, st, exploreHref) {
    const k = d.kpi, fo = failover(d.failover), req = tot(k, 'req') || 0;
    const cls = (d.failClass && d.failClass.rows || []).map(r => [r[0], r[col(d.failClass, 'req')]]);
    const mx = Math.max(1, ...cls.map(c => c[1]));
    const cancels = (d.cancels || []).map(e => e.dur_ms || 0);
    const hist = cancelHistogram(cancels), hm = Math.max(1, ...hist.map(h => h[1]));
    const clusters = timeoutClusters(cancels);
    const clusterN = sum(clusters.map(c => c.n));
    const capped = (d.cancels || []).length >= 1000 ? ' (newest 1,000)' : '';
    const events = (d.failEvents || []).filter(e => st.failCls == null || (e.error_class || '') === st.failCls);
    return `<div class="d-ph"><span class="d-title">Reliability</span><span class="spacer"></span><a class="btn sm" href="${attr(exploreHref)}">by status / attempts ↗</a></div>
      <div class="d-rel-top">
        <div><b class="t-err">${fmtInt(tot(k, 'err') || 0)}</b><span>errors · ${fmtP(req ? (tot(k, 'err') || 0) / req : 0, 2)}</span></div>
        <div><b class="t-warn">${fmtInt(tot(k, 'canceled') || 0)}</b><span>canceled by caller · ${fmtP(req ? (tot(k, 'canceled') || 0) / req : 0, 2)}</span></div>
        <div><b class="t-ok">${fmtInt(fo.rescued)}<small> / ${fmtInt(fo.total)}</small></b><span>failovers rescued</span></div>
      </div>
      ${cls.length ? `<div class="v-label">Why it failed</div><div class="d-cls">${cls.map(([c, n]) =>
        `<button class="k ${st.failCls === c ? 'on' : ''}" data-cls="${attr(c)}" title="Show only these in the list below">${esc(c || '(none)')}</button><span class="bar"><i style="width:${(n / mx * 100).toFixed(1)}%;background:${c === 'canceled' ? v('--d-cancel') : v('--d-err')}"></i></span><span class="n">${fmtInt(n)}</span>`).join('')}</div>` : '<div class="d-empty">No failed requests in this range.</div>'}
      ${cancels.length ? `<div class="v-label d-gap">When callers gave up <span class="t-dim">· elapsed time at cancel${capped}</span></div>
        <div class="d-hist">${hist.map(([l, n]) => `<div title="${n} canceled"><span>${n || ''}</span><i style="height:${(n / hm * 100).toFixed(1)}%"></i><span>${l}</span></div>`).join('')}</div>
        ${clusterN ? `<div class="d-insight warn">${clusterN} of ${cancels.length} cancellations stop at exactly ${clusters.map(c => c.sec + ' s').join(' / ')} — a caller-side timeout, not an upstream failure.</div>` : ''}` : ''}
      <details class="d-fails" ${st.failsOpen ? 'open' : ''}><summary>Failed requests <span class="t-dim">· ${(d.failEvents || []).length} in range &amp; filters, newest first (up to 100)${st.failCls != null ? ' · showing ' + esc(st.failCls || '(none)') : ''}</span></summary>
        <div class="table-wrap">${VMRStats.eventsTable(events, 'No failed requests in this range.')}</div></details>`;
  }

  // heatExposure is how many hours of each weekday × hour-of-day cell fall
  // inside [from, to) by wall clock: 1 for a 7-day range, 4 or 5 for 30 days.
  // The heatmap's cells are counts summed over those hours, so dividing by
  // this turns them into requests per hour that mean the same at any range.
  function heatExposure(from, to) {
    const ms = iso => { const m = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)/.exec(iso || ''); return m ? Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]) : NaN; };
    const a = ms(from), b = ms(to), H = 3600e3, out = new Map();
    const names = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];
    if (!(a < b)) return out;
    for (let t = Math.floor(a / H) * H; t < b; t += H) {
      const dt = new Date(t), key = names[(dt.getUTCDay() + 6) % 7] + '|' + String(dt.getUTCHours()).padStart(2, '0');
      out.set(key, (out.get(key) || 0) + (Math.min(b, t + H) - Math.max(a, t)) / H);
    }
    return out;
  }

  function renderHeat(res, range) {
    const head = '<div class="d-ph"><span class="d-title">Usage pattern</span><span class="hint">weekday × hour · average requests per hour</span></div>';
    if (!HEAT_RANGES.includes(range)) return head + '<div class="d-empty">Shown for the 7d and 30d ranges — shorter ranges fill only a row or two.</div>';
    if (!res) return head + '<div class="d-empty">Loading…</div>';
    const days = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'], pad = n => String(n).padStart(2, '0');
    if (!res.rows.length) return head + '<div class="d-empty">No requests in this range.</div>';
    const expo = heatExposure(res.query && res.query.from, res.query && res.query.to);
    const sumOf = new Map(res.rows.map(r => [r[0] + '|' + r[1], r[2] || 0]));
    // Requests per hour for one cell; a cell the range barely touches
    // (under a tenth of an hour) has nothing to average.
    const avgOf = key => { const e = expo.get(key) || 0; return e >= 0.1 ? (sumOf.get(key) || 0) / e : 0; };
    const keys = days.flatMap(dn => Array.from({ length: 24 }, (_, h) => dn + '|' + pad(h)));
    const mx = Math.max(1e-9, ...keys.map(avgOf));
    let cells = '<span></span>' + Array.from({ length: 24 }, (_, h) => `<span class="hl">${h % 3 ? '' : pad(h)}</span>`).join('');
    days.forEach(dname => {
      cells += `<span class="wl">${dname}</span>`;
      for (let h = 0; h < 24; h++) {
        const key = dname + '|' + pad(h), x = avgOf(key);
        cells += `<span class="c" title="${dname} ${pad(h)}:00 · ${x.toFixed(1)} requests / hour on average (${fmtInt(sumOf.get(key) || 0)} over ${(expo.get(key) || 0).toFixed(1)} h)"${x ? ` style="opacity:${(0.12 + 0.88 * x / mx).toFixed(2)}"` : ' data-zero="1"'}></span>`;
      }
    });
    const peak = keys.reduce((a, b) => avgOf(b) > avgOf(a) ? b : a);
    const night = [2, 3, 4, 5].flatMap(h => days.map(dn => avgOf(dn + '|' + pad(h))));
    const nightAvg = sum(night) / night.length;
    return head + `<div class="d-heat">${cells}</div>
      <div class="d-hscale">0<span class="g"></span>${mx.toFixed(mx < 10 ? 1 : 0)} req / hour<span class="spacer"></span>busiest <b>${esc(peak.split('|')[0])} ${esc(peak.split('|')[1])}:00</b> (${avgOf(peak).toFixed(1)}) · 02–06 averages <b>${nightAvg.toFixed(1)}</b> req / hour</div>`;
  }

  // ---------------- state <-> URL hash ----------------

  function loadState(hash) {
    const raw = VMRStats.decodeState(hash);
    if (raw.range === '24h') raw.range = '1d'; // the old spelling of the same range
    const st = Object.assign({}, DEFAULTS);
    for (const k of Object.keys(DEFAULTS)) if (raw[k]) st[k] = raw[k];
    if (!RANGES.includes(st.range)) st.range = DEFAULTS.range;
    if (!['req', 'tok'].includes(st.act)) st.act = 'req';
    const splits = SPLITS[st.act];
    if (!splits.includes(st.split)) st.split = splits[0];
    if (!BD_DIMS.some(x => x[0] === st.bd)) st.bd = DEFAULTS.bd;
    if (!COLSETS[st.cols]) st.cols = DEFAULTS.cols;
    st.where = (raw.where || []).filter(w => FILTER_DIMS.some(dm => w.startsWith(dm + ':') || w.startsWith(dm + '!')));
    st.off = new Set();
    return st;
  }

  function saveState(st) {
    const out = {};
    for (const k of Object.keys(DEFAULTS)) if (st[k] !== DEFAULTS[k]) out[k] = st[k];
    out.where = st.where;
    return VMRStats.encodeState(out);
  }

  function rangeParams(st) { return { from: st.range === 'today' ? 'today' : '-' + st.range }; }

  function nextStep(schema, step) {
    const names = (schema && schema.steps || []).map(s => s.name);
    const i = names.indexOf(step);
    return i >= 0 && i < names.length - 1 ? names[i + 1] : step;
  }

  function exploreLink(st, extra) {
    const r = st.range === 'today' ? { from: 'today' } : { range: st.range };
    return VMRStats.link(Object.assign({ view: 'custom', step: 'auto', where: st.where }, r, extra || {}));
  }

  // ---------------- mount (DOM) ----------------

  // mount wires the dashboard into the page. ids: the containers the page
  // provides (ctl, kpis, activity, comp, breakdown, speed, rel, heat,
  // events, eventsWrap, eventsLabel, eventsClose, and optionally speedHead).
  // Returns { refresh }.
  function mount(ids) {
    const $ = id => document.getElementById(ids[id]);
    const st = loadState(typeof location !== 'undefined' ? location.hash : '');
    let d = null, schema = null, other = '\u0000other', geom = null, A = null, gen = 0;

    const persist = () => { if (typeof history !== 'undefined') history.replaceState(null, '', '#' + saveState(st)); };
    const W = () => { const q = rangeParams(st); q.where = st.where; return q; };
    const Q = p => VMRStats.api.query(Object.assign(W(), p, { where: st.where.concat(p.where || []) }));
    const E = p => VMRStats.api.events(Object.assign(W(), p, { where: st.where.concat(p.where || []) }));

    function renderCtl() {
      const chips = st.where.map((w, i) => {
        const j = w.search(/[:!]/), dim = w.slice(0, j), neg = w[j] === '!', val = VMRStats.splitWhere(w.slice(j + 1)).join(' | ');
        return `<span class="d-chip"><em>${esc(dim)}</em><button class="op" data-op="${i}" title="Toggle = / ≠">${neg ? '≠' : '='}</button>${esc(val || '(none)')}<button class="x" data-x="${i}" title="Remove">×</button></span>`;
      }).join('');
      $('ctl').innerHTML = `<span class="d-ctl-title">Statistics</span>${seg('range', RANGES.map(r => [r, r]), st.range)}
        <span class="d-chips">${chips || '<span class="hint">No filters — click a name in Breakdown to filter every panel</span>'}${st.where.length > 1 ? '<button class="btn sm" data-clear="1">clear</button>' : ''}</span>
        <label class="d-tog ${st.cmp === '1' ? 'on' : ''}" title="Compare every number with the ${esc(st.range === 'today' ? 'same hours yesterday' : 'period right before')}"><input type="checkbox" data-cmp="1" ${st.cmp === '1' ? 'checked' : ''}> vs ${esc(cmpWordOf(st))}</label>
        <span class="hint d-meta">${d ? esc(`${fmtInt(tot(d.kpi, 'req') || 0)} requests · step ${d.ts.query.step}`) : ''}</span>
        <a class="btn sm" href="${attr(exploreLink(st))}">Explore ↗</a>`;
    }

    function width(el) { return (el && el.clientWidth ? el.clientWidth : 900) - 2; }

    function paintActivity() {
      const el = $('activity');
      const r = renderActivity(d, st, other, width(el) - 30);
      el.innerHTML = r.html;
      geom = r.geom; A = r.A;
    }
    function paintBreakdown() { $('breakdown').innerHTML = renderBreakdown(d.bd[st.bd], st, other); }
    function paintRel() { $('rel').innerHTML = renderReliability(d, st, exploreLink(st, { group: ['status', 'attempts'], where: st.where.concat(['outcome!ok']), metrics: ['req', 'share_req'] })); }
    function paintAll() {
      PANELS.forEach(id => { $(id).hidden = false; });
      if (ids.speedHead && $('speedHead')) $('speedHead').hidden = false;
      renderCtl();
      $('kpis').innerHTML = renderKPIs(d, st);
      paintActivity();
      $('comp').innerHTML = renderComposition(d);
      paintBreakdown();
      $('speed').innerHTML = renderSpeed(d, st);
      paintRel();
      $('heat').innerHTML = renderHeat(d.heat, st.range);
      if ($('heat').classList) $('heat').classList.toggle('d-collapsed', !HEAT_RANGES.includes(st.range));
    }

    async function loadSplit() {
      if (!d || st.split === 'outcome' || st.split === 'type') return;
      const key = st.split + ':' + st.act;
      if (d.split[key]) return;
      const my = gen;
      const r = await Q({ step: 'auto', group: [st.split], metrics: [st.act === 'req' ? 'req' : 'tok_total'], top: SERIES_TOP });
      if (my === gen) d.split[key] = r;
    }
    async function loadBreakdown() {
      if (!d || d.bd[st.bd]) return;
      const my = gen;
      const r = await Q({ group: [st.bd], metrics: BD_METRICS, top: 50, compare: compareMode(st) });
      if (my === gen) d.bd[st.bd] = r;
    }

    // refresh never rejects: the dashboard owns its failure and shows it in
    // its own panels, so the live half of the page (vitals, alerts, Live
    // Requests) renders whatever the statistics backend is doing. A stale
    // load's failure is dropped, a newer one already owns the panels.
    async function refresh() {
      const my = ++gen;
      try { await load(my); } catch (e) { if (my === gen) showError(e); }
    }

    function showError(e) {
      d = null;
      renderCtl();
      const msg = (e && e.message) || 'network error';
      $('kpis').innerHTML = `<div class="card d-fail"><b class="t-err">Statistics unavailable</b><span class="t-dim">${esc(msg)}</span><button class="btn sm" data-retry="1">retry</button></div>`;
      PANELS.forEach(id => { $(id).innerHTML = ''; $(id).hidden = true; });
      if (ids.speedHead && $('speedHead')) $('speedHead').hidden = true;
    }

    async function load(my) {
      schema = await VMRStats.api.schema();
      other = schema.other_group;
      const cmp = compareMode(st);
      const [kpi, today, fo, ts, outcome, failClass, failEv, cancels, mixS, mixT, mixI] = await Promise.all([
        Q({ metrics: KPI_METRICS, compare: cmp }),
        st.range === 'today' ? null : VMRStats.api.query({ from: 'today', where: st.where, metrics: ['req', 'tok_total'] }),
        Q({ where: ['attempts:2|3+'], group: ['outcome'], metrics: ['req'] }),
        Q({ step: 'auto', metrics: TS_METRICS, compare: cmp }),
        Q({ step: 'auto', group: ['outcome'], metrics: ['req'] }),
        Q({ where: ['outcome!ok'], group: ['error_class'], metrics: ['req', 'share_req'] }),
        E({ where: ['outcome!ok'], limit: 100 }),
        E({ where: ['outcome:canceled'], limit: 1000 }),
        Q({ group: ['stream'], metrics: ['req', 'share_req', 'share_tok'] }),
        Q({ group: ['has_tools'], metrics: ['req', 'share_req', 'share_tok'] }),
        Q({ group: ['has_image'], metrics: ['req', 'share_req', 'share_tok'] }),
      ]);
      if (my !== gen) return;
      d = { kpi, today, failover: fo, ts, outcome, failClass, failEvents: failEv.events || [], cancels: cancels.events || [],
        mix: { stream: mixS, has_tools: mixT, has_image: mixI }, split: {}, bd: {}, speed: null, heat: null };
      paintAll();
      const [speed, heat] = await Promise.all([
        Q({ step: nextStep(schema, ts.query.step), metrics: SPEED_METRICS }),
        HEAT_RANGES.includes(st.range) ? Q({ group: ['weekday', 'hour_of_day'], metrics: ['req'], top: 168 }) : null,
        loadBreakdown(),
        loadSplit(),
      ]);
      if (my !== gen) return;
      d.speed = speed;
      d.heat = heat;
      paintAll();
    }

    // relaunch reruns the whole dashboard after a range/filter/compare
    // change; errors surface as a toast like every console refresh.
    function relaunch() {
      persist();
      refresh().catch(e => toast('Statistics: ' + (e.message || 'network error')));
    }
    function lazy(fn) {
      persist();
      fn().then(() => d && paintAll()).catch(e => toast('Statistics: ' + (e.message || 'network error')));
    }

    async function showEvents(label, p) {
      $('eventsWrap').hidden = false;
      $('eventsLabel').textContent = label;
      $('events').innerHTML = '<div class="d-empty">Loading…</div>';
      if ($('eventsWrap').scrollIntoView) $('eventsWrap').scrollIntoView({ behavior: 'smooth', block: 'start' });
      try {
        const r = await E(Object.assign({ limit: 200 }, p));
        $('events').innerHTML = VMRStats.eventsTable(r.events, 'No requests match.');
      } catch (e) { toast('Requests: ' + e.message); }
    }

    function addFilter(dim, val) {
      const w = dim + ':' + VMRStats.escWhere(val);
      if (!st.where.includes(w)) st.where = st.where.concat([w]);
      relaunch();
    }

    const segOf = ev => { const b = ev.target.closest && ev.target.closest('[data-seg] button'); return b ? [b.closest('[data-seg]').dataset.seg, b.dataset.k] : null; };

    $('ctl').onclick = ev => {
      const s = segOf(ev);
      if (s && s[0] === 'range') { st.range = s[1]; return relaunch(); }
      const b = ev.target.closest && ev.target.closest('button');
      if (!b) return;
      if (b.dataset.x != null) { st.where = st.where.filter((_, i) => i !== +b.dataset.x); relaunch(); }
      else if (b.dataset.op != null) {
        st.where = st.where.map((w, i) => i !== +b.dataset.op ? w : w.replace(/[:!]/, m => m === ':' ? '!' : ':'));
        relaunch();
      } else if (b.dataset.clear) { st.where = []; relaunch(); }
    };
    $('ctl').onchange = ev => { if (ev.target.dataset && ev.target.dataset.cmp) { st.cmp = ev.target.checked ? '1' : '0'; relaunch(); } };
    $('kpis').onclick = ev => {
      if (ev.target.closest && ev.target.closest('[data-retry]')) return relaunch();
      const c = ev.target.closest && ev.target.closest('[data-go]');
      const t = c && document.getElementById(c.dataset.go);
      if (t && t.scrollIntoView) t.scrollIntoView({ behavior: 'smooth', block: 'center' });
    };
    $('activity').onclick = ev => {
      const s = segOf(ev);
      if (s) {
        if (s[0] === 'act') { st.act = s[1]; st.split = DEFAULTS.split; }
        else st.split = s[1];
        st.off = new Set();
        paintActivity();
        return lazy(loadSplit);
      }
      const lg = ev.target.closest && ev.target.closest('[data-legend],[data-read]');
      if (lg) {
        if (lg.dataset.read) st.showRead = !st.showRead;
        else if (st.off.has(lg.dataset.legend)) st.off.delete(lg.dataset.legend);
        else st.off.add(lg.dataset.legend);
        return paintActivity();
      }
      const i = bucketAt(ev);
      if (i < 0) return;
      // The first bucket starts before the range does (buckets sit on step
      // boundaries); its list starts where the panels' numbers start.
      const floor = d && d.kpi && d.kpi.query && d.kpi.query.from;
      const b = A.buckets[i], end = A.buckets[i + 1] || 'now', from = floor && Date.parse(b) < Date.parse(floor) ? floor : b;
      showEvents(`requests in ${wall(from).day} ${wall(from).hm} → ${end === 'now' ? 'now' : wall(end).hm}`, { from, to: end });
    };
    function bucketAt(ev) {
      const svg = ev.target.closest && ev.target.closest('svg.d-chart-svg');
      if (!svg || !geom || !A) return -1;
      const r = svg.getBoundingClientRect(), x = (ev.clientX - r.left) * geom.W / r.width, i = Math.floor((x - geom.L) / geom.bw);
      return i >= 0 && i < geom.n ? i : -1;
    }
    $('activity').onmousemove = ev => {
      const tip = $('activity').querySelector && $('activity').querySelector('.d-tip');
      const hov = $('activity').querySelector && $('activity').querySelector('.d-hover');
      if (!tip || !hov) return;
      const i = bucketAt(ev);
      if (i < 0) { tip.hidden = true; hov.style.opacity = 0; return; }
      hov.setAttribute('x', (geom.L + i * geom.bw).toFixed(1));
      hov.style.opacity = 1;
      tip.innerHTML = tipHTML(d, st, A, i);
      tip.hidden = false;
      const box = $('activity').querySelector('.d-chart').getBoundingClientRect();
      tip.style.left = Math.min(ev.clientX - box.left + 14, box.width - 200) + 'px';
      tip.style.top = Math.max(0, ev.clientY - box.top - 30) + 'px';
    };
    $('activity').onmouseleave = () => { const tip = $('activity').querySelector && $('activity').querySelector('.d-tip'); if (tip) tip.hidden = true; };
    $('breakdown').onclick = ev => {
      const s = segOf(ev);
      if (s) {
        if (s[0] === 'bd') { st.bd = s[1]; st.sort = null; paintBreakdown(); return lazy(loadBreakdown); }
        st.cols = s[1];
        persist();
        return paintBreakdown();
      }
      const th = ev.target.closest && ev.target.closest('[data-sort]');
      if (th) { const k = th.dataset.sort; st.sort = { k, d: st.sort && st.sort.k === k ? -st.sort.d : -1 }; return paintBreakdown(); }
      const dr = ev.target.closest && ev.target.closest('.drill');
      if (dr) { ev.preventDefault(); return showEvents(`${dr.dataset.dim} = ${dr.dataset.val || '(none)'}`, { where: [dr.dataset.dim + ':' + VMRStats.escWhere(dr.dataset.val)] }); }
      const f = ev.target.closest && ev.target.closest('.filterable');
      if (f) addFilter(f.dataset.dim, f.dataset.val);
    };
    $('rel').onclick = ev => {
      const c = ev.target.closest && ev.target.closest('[data-cls]');
      if (!c) return;
      st.failCls = st.failCls === c.dataset.cls ? null : c.dataset.cls; // '' is a class too: (none)
      st.failsOpen = true;
      paintRel();
    };
    if ($('rel').addEventListener) $('rel').addEventListener('toggle', ev => { if (ev.target.tagName === 'DETAILS') st.failsOpen = ev.target.open; }, true);
    $('eventsClose').onclick = () => { $('eventsWrap').hidden = true; };
    if (typeof ResizeObserver === 'function') {
      let last = 0;
      new ResizeObserver(() => { const w = width($('activity')); if (d && Math.abs(w - last) > 4) { last = w; paintActivity(); } }).observe($('activity'));
    }
    return { refresh, state: st };
  }

  return { mount, delta, niceMax, pivot, sparkSVG, stackedSVG, lineSVG, axisTicks, heatExposure, compareMode, cancelHistogram, timeoutClusters, loadState, saveState, nextStep,
    renderKPIs, renderActivity, renderComposition, renderBreakdown, renderSpeed, renderReliability, renderHeat };
})();
