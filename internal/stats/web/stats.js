/* stats.js — the statistics module's one front-end component, shared by
   /stats.html, the Overview and the Models page. It does three things:
   fetch /stats/* results, render a result as a table, and draw a result as
   a uPlot chart. Every number is computed server-side; this file only
   formats (by the metric "kind" the schema declares) and lays out.

   Depends on console.js globals (esc, fmtInt, fmtKMG, fmtRate, dec2,
   VMRAuth), resolved at call time — load order does not matter. Pure
   helpers (format, table, state codec) run under Node in the server
   package's render tests; only chart() needs a browser. */
'use strict';

const VMRStats = (() => {
  // ---------------- data ----------------

  function qs(p) {
    const u = new URLSearchParams();
    for (const [k, v] of Object.entries(p || {})) {
      if (v === undefined || v === null || v === '' || v === false) continue;
      if (k === 'where') { (v || []).forEach(w => u.append('where', w)); continue; }
      if (Array.isArray(v)) { if (v.length) u.set(k, v.join(',')); continue; }
      u.set(k, v === true ? 'prev' : String(v));
    }
    return u.toString();
  }

  async function get(path, params) {
    const url = path + (params ? '?' + qs(params) : '');
    const resp = await VMRAuth.guard(async () => {
      const headers = {};
      const k = VMRAuth.get();
      if (k) headers['Authorization'] = 'Bearer ' + k;
      return fetch(url, { headers });
    });
    let body = null;
    try { body = await resp.json(); } catch (_) { /* non-JSON error page */ }
    if (!resp.ok) throw new Error((body && body.error) || ('HTTP ' + resp.status + ' from ' + path));
    return body;
  }

  let schemaPromise = null;
  const api = {
    schema() {
      if (!schemaPromise) schemaPromise = get('/stats/schema').catch(e => { schemaPromise = null; throw e; });
      return schemaPromise;
    },
    query: p => get('/stats/query', p),
    events: p => get('/stats/events', p),
  };

  // ---------------- formatting ----------------

  const DIM = '<span class="dim">—</span>';

  // attr escapes a value for a double-quoted HTML attribute. console.js's
  // esc() covers text content only (it leaves quotes alone), and these
  // attributes carry arbitrary dimension values and JSON.
  function attr(s) { return esc(s).replace(/"/g, '&quot;'); }

  function fmtMS(ms) {
    return ms >= 1000 ? dec2(ms / 1000) + 's' : Math.round(ms) + 'ms';
  }

  // value renders one metric value by kind; null is "no sample".
  function value(kind, v) {
    if (v === null || v === undefined) return DIM;
    switch (kind) {
      case 'count': return fmtInt(v);
      case 'tokens': return fmtKMG(v);
      case 'ratio': return dec2(v * 100) + '%';
      case 'ms': return fmtMS(v);
      case 'rate': return fmtRate(v);
      default: return esc(String(v));
    }
  }

  // modelHue gives one model name one colour everywhere (hash, not order).
  const hueCache = new Map();
  function modelHue(name) {
    if (!hueCache.has(name)) {
      let h = 0;
      for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) & 0xffffffff;
      hueCache.set(name, `hsl(${Math.abs(h) % 360}, 70%, 62%)`);
    }
    return hueCache.get(name);
  }

  // endpointParts splits a "provider:model" endpoint value at the first colon.
  function endpointParts(v) {
    const i = v.indexOf(':');
    return i >= 0 ? [v.slice(0, i), v.slice(i + 1)] : ['', v];
  }

  // dimHue is the colour a dimension value shares with its model name on every
  // other panel, or '' when the dimension carries no model.
  function dimHue(dim, v, other) {
    if (v === other || !v || (dim !== 'endpoint' && dim !== 'model')) return '';
    return modelHue(dim === 'endpoint' ? endpointParts(v)[1] : v);
  }

  // dimValue renders one dimension value; '' means "absent" (an endpoint
  // that was never routed, a request without a caller tag, ...).
  function dimValue(dim, v, other) {
    if (v === other) return '<span class="dim">other</span>';
    if (v === '' || v === null || v === undefined) {
      return dim === 'endpoint' ? '<span class="dim" title="No upstream attempt was made">(unrouted)</span>' : '<span class="dim">(none)</span>';
    }
    if (dim === 'endpoint' || dim === 'model') {
      const [prov, model] = dim === 'endpoint' ? endpointParts(v) : ['', v];
      return `<span class="ep" title="${attr(v)}">${prov ? esc(prov) + ':' : ''}<span style="color:${modelHue(model)}">${esc(model)}</span></span>`;
    }
    return esc(v);
  }

  // bucketLabel shortens an RFC3339 bucket start (server wall clock, server
  // offset — read off the string, never re-zoned in the browser).
  function bucketLabel(iso, step) {
    if (!iso) return '';
    return step === '1d' ? iso.slice(5, 10) : iso.slice(5, 16).replace('T', ' ');
  }

  // where values: a literal | or \ inside a value is written \| or \\ so a
  // value list can still be split on |. A model name is client-chosen and
  // may contain anything.
  const escWhere = v => String(v).replace(/[\\|]/g, '\\$&');
  function splitWhere(s) {
    const out = [];
    let cur = '';
    for (let i = 0; i < s.length; i++) {
      const c = s[i];
      if (c === '\\' && i + 1 < s.length && (s[i + 1] === '|' || s[i + 1] === '\\')) cur += s[++i];
      else if (c === '|') { out.push(cur); cur = ''; } else cur += c;
    }
    out.push(cur);
    return out;
  }

  // ---------------- schema lookups ----------------

  function index(schema) {
    const m = {}, d = {};
    (schema && schema.metrics || []).forEach(x => { m[x.name] = x; });
    (schema && schema.dims || []).forEach(x => { d[x.name] = x; });
    return { metric: m, dim: d };
  }

  // ---------------- table ----------------

  // table renders an unbucketed result. opts:
  //   schema     — /stats/schema (labels, tooltips, other-group marker)
  //   sort       — {col, desc}: client-side ordering of the rendered rows
  //   filterable — dimension cells carry data-dim/data-val for click-to-filter
  //   drill      — each row ends with a "events" link carrying its group values
  //   empty      — text for a result without rows
  function table(res, opts) {
    const o = opts || {};
    const ix = index(o.schema);
    const other = o.schema && o.schema.other_group;
    const cols = res.columns || [];
    let rows = (res.rows || []).slice();
    if (o.sort && o.sort.col >= 0 && o.sort.col < cols.length) {
      const c = o.sort.col, dir = o.sort.desc ? -1 : 1;
      rows.sort((a, b) => {
        const x = a[c], y = b[c];
        if (x === y) return 0;
        if (x === null || x === undefined) return 1;
        if (y === null || y === undefined) return -1;
        return (x < y ? -1 : 1) * dir;
      });
    }
    const dimCols = cols.map((c, i) => c.kind === 'dim' ? i : -1).filter(i => i >= 0);
    const head = cols.map((c, i) => {
      const meta = c.kind === 'dim' ? ix.dim[c.name] : ix.metric[c.name];
      const label = meta ? meta.label : c.name;
      const title = meta ? meta.desc : '';
      const cls = c.kind === 'dim' ? '' : 'num';
      const arrow = o.sort && o.sort.col === i ? (o.sort.desc ? ' ▾' : ' ▴') : '';
      return `<th class="${cls} sortable" data-sort="${i}" title="${attr(title)}">${esc(label)}${arrow}</th>`;
    }).join('') + (o.drill ? '<th></th>' : '');
    const cell = (c, v) => {
      if (c.kind !== 'dim') return `<td class="num">${value(c.kind, v)}</td>`;
      const inner = dimValue(c.name, v, other);
      if (!o.filterable || v === other) return `<td>${inner}</td>`;
      return `<td class="filterable" data-dim="${attr(c.name)}" data-val="${attr(v)}" title="Click to filter on this value">${inner}</td>`;
    };
    let body;
    if (!rows.length) {
      body = `<tr><td colspan="${cols.length + (o.drill ? 1 : 0)}" class="empty-cell"><div class="t-dim">${esc(o.empty || 'No requests match.')}</div></td></tr>`;
    } else {
      body = rows.map(r => {
        const drill = o.drill
          ? `<td class="num"><a href="#" class="drill" data-group="${attr(JSON.stringify(dimCols.map(i => [cols[i].name, r[i]])))}" title="List the requests behind this row">events</a></td>`
          : '';
        return '<tr>' + cols.map((c, i) => cell(c, r[i])).join('') + drill + '</tr>';
      }).join('');
    }
    let foot = '';
    if (rows.length > 1 && res.totals && res.totals.length === cols.length) {
      foot = '<tfoot><tr>' + cols.map((c, i) => {
        if (c.kind === 'dim') return i === dimCols[0] ? '<td>total</td>' : '<td></td>';
        return `<td class="num">${value(c.kind, res.totals[i])}</td>`;
      }).join('') + (o.drill ? '<td></td>' : '') + '</tr></tfoot>';
    }
    return `<table class="c"><thead><tr>${head}</tr></thead><tbody>${body}</tbody>${foot}</table>`;
  }

  // eventsTable renders /stats/events rows.
  function eventsTable(events, empty) {
    if (!events || !events.length) {
      return `<table class="c"><tbody><tr><td class="empty-cell"><div class="t-dim">${esc(empty || 'No requests match.')}</div></td></tr></tbody></table>`;
    }
    const rows = events.map(e => {
      const ep = e.provider ? `${e.provider}:${e.model || ''}` : '';
      const tok = e.tokens || null;
      const outcome = e.outcome === 'ok'
        ? '<span class="badge b-ok tight">ok</span>'
        : `<span class="badge ${e.outcome === 'canceled' ? 'b-warn' : 'b-err'} tight">${esc(e.error_class || e.outcome)}</span>`;
      const status = e.status ? ` <span class="dim">${e.status}</span>` : '';
      const att = e.attempts > 1 ? `<span class="badge b-warn tight" title="Failover">#${e.attempts}</span>`
        : (e.attempts >= 1 ? '<span class="dim">1</span>' : '<span class="dim" title="No upstream attempt was made">—</span>');
      return `<tr>
        <td class="num">${esc(String(e.ts || '').slice(5, 19).replace('T', ' '))}</td>
        <td><span class="dim">${esc(e.protocol || '')}:</span><b>${esc(e.vmodel || '')}</b> ${modeTag(e.stream)}</td>
        <td>${dimValue('client', e.client_key_tag || '')}</td>
        <td>${dimValue('endpoint', ep)}</td>
        <td>${outcome}${status}</td>
        <td class="num">${att}</td>
        <td class="num">${value('ms', e.dur_ms)}</td>
        <td class="num">${e.stream && e.ttft_ms > 0 ? value('ms', e.ttft_ms) : DIM}</td>
        <td class="num">${tok ? value('tokens', tok.in + tok.cache_read + tok.cache_write) : DIM}</td>
        <td class="num">${tok ? value('tokens', tok.out) : DIM}</td>
      </tr>`;
    }).join('');
    return `<table class="c"><thead><tr>
      <th class="num">When</th><th>Model</th><th>Caller</th><th>Endpoint</th><th>Outcome</th>
      <th class="num" title="Upstream attempts">Att</th><th class="num">Duration</th>
      <th class="num" title="Time to first token (streamed requests only)">TTFT</th>
      <th class="num" title="Input tokens: fresh + cache write + cache read">Tok in</th><th class="num">Tok out</th>
    </tr></thead><tbody>${rows}</tbody></table>`;
  }

  // ---------------- chart ----------------

  const PALETTE = ['--accent', '--green', '--yellow', '--purple', '--cyan', '--pink', '--orange', '--red'];
  // Ungrouped charts colour a metric by what it means (errors red, the four
  // token components as the console has always shown them); grouped charts
  // colour by group through PALETTE.
  const METRIC_COLOR = {
    req: '--accent', ok: '--green', err: '--red', canceled: '--yellow', err_rate: '--red', cancel_rate: '--yellow',
    tok_fresh: '--accent', tok_cache_write: '--purple', tok_cache_read: '--cyan', tok_out: '--green',
  };
  // Whole-number axes (counts, tokens) step in 1-2-5 increments so ticks
  // never land on fractions that formatting would round into duplicates.
  const INT_INCRS = [];
  for (let p = 1; p <= 1e12; p *= 10) INT_INCRS.push(p, 2 * p, 5 * p);
  const wholeKind = k => k === 'count' || k === 'tokens';

  // groupHue colours a series grouped by exactly one model-bearing dimension
  // with its model's colour; '' for every other grouping.
  function groupHue(s) {
    return s.group.length === 1 ? dimHue(s.dims[0], s.group[0], null) : '';
  }

  function cssVar(name, fallback) {
    try {
      const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
      return v || fallback;
    } catch (_) { return fallback; }
  }

  // wallSeconds turns an RFC3339 bucket start into "its wall clock read as
  // UTC", and serverDate (uPlot's tzDate) maps it back to a Date whose
  // local fields equal that wall clock — so the axis shows the server's
  // time of day whatever the browser's zone.
  function wallSeconds(iso) {
    const m = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)/.exec(iso || '');
    return m ? Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]) / 1000 : 0;
  }
  function serverDate(ts) {
    const d = new Date(ts * 1000);
    return new Date(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate(), d.getUTCHours(), d.getUTCMinutes(), d.getUTCSeconds());
  }
  // timeTicks labels the time axis in the console's 24-hour wall clock
  // (uPlot's default is 12-hour am/pm); the date rides under the first tick
  // of each day. Ticks are wall-clock-as-UTC seconds, see wallSeconds.
  function timeTicks(u, splits, axisIdx, space, incr) {
    let prevDay = '';
    return splits.map(s => {
      const iso = new Date(s * 1000).toISOString();
      const day = iso.slice(5, 10);
      if (incr >= 86400) return day;
      const label = day !== prevDay ? iso.slice(11, 16) + '\n' + day : iso.slice(11, 16);
      prevDay = day;
      return label;
    });
  }

  // seriesOf turns a dense bucketed result into uPlot data: one x array and
  // one y array per (group, metric). Groups keep the server's ranking.
  function seriesOf(res, metricNames, schema) {
    const ix = index(schema);
    const cols = res.columns || [];
    const dimIdx = cols.map((c, i) => c.kind === 'dim' ? i : -1).filter(i => i >= 0);
    const mIdx = metricNames.map(n => cols.findIndex(c => c.name === n)).filter(i => i >= 0);
    const xs = [], xPos = new Map(), groups = [], gPos = new Map();
    (res.rows || []).forEach(r => {
      const b = r[0];
      if (!xPos.has(b)) { xPos.set(b, xs.length); xs.push(wallSeconds(b)); }
      const g = JSON.stringify(dimIdx.map(i => r[i]));
      if (!gPos.has(g)) { gPos.set(g, groups.length); groups.push(dimIdx.map(i => r[i])); }
    });
    const series = [], data = [xs];
    groups.forEach((gv, gi) => {
      mIdx.forEach(mi => {
        const ys = new Array(xs.length).fill(null);
        series.push({ group: gv, metric: cols[mi].name, kind: cols[mi].kind, ys, gi, mk: mIdx.indexOf(mi), dims: dimIdx.map(i => cols[i].name) });
      });
    });
    (res.rows || []).forEach(r => {
      const xi = xPos.get(r[0]);
      const gi = gPos.get(JSON.stringify(dimIdx.map(i => r[i])));
      mIdx.forEach((mi, k) => { series[gi * mIdx.length + k].ys[xi] = r[mi]; });
    });
    const other = schema && schema.other_group;
    series.forEach(s => {
      const gl = s.group.map((v, k) => v === '' ? (cols[dimIdx[k]].name === 'endpoint' ? '(unrouted)' : '(none)') : (v === other ? 'other' : v)).join(' · ');
      const ml = ix.metric[s.metric] ? ix.metric[s.metric].label : s.metric;
      s.label = gl && mIdx.length > 1 ? gl + ' · ' + ml : (gl || ml);
      data.push(s.ys);
    });
    return { data, series };
  }

  // chart draws a bucketed result into el (replacing any previous chart).
  function chart(el, res, opts) {
    const o = opts || {};
    if (el._uplot) { el._uplot.destroy(); el._uplot = null; }
    if (typeof uPlot !== 'function') return;
    const { data, series } = seriesOf(res, o.metrics || [], o.schema);
    el.innerHTML = '';
    if (!data[0].length || !series.length) {
      el.innerHTML = '<div class="empty-cell t-dim">No data in this range.</div>';
      return;
    }
    const kinds = [...new Set(series.map(s => s.kind))];
    const axisFor = k => kinds.length > 1 && k !== kinds[0] ? 'y2' : 'y';
    const muted = cssVar('--text-muted', '#8b949e');
    const grid = cssVar('--border', '#30363d');
    const fmtTick = k => (u, vals) => vals.map(v => v === null ? '' : stripTags(value(k, v)));
    const axes = [
      { stroke: muted, grid: { stroke: grid, width: 1 }, ticks: { stroke: grid }, values: timeTicks },
      { scale: 'y', stroke: muted, grid: { stroke: grid, width: 1 }, ticks: { stroke: grid }, values: fmtTick(kinds[0]), size: 60, incrs: wholeKind(kinds[0]) ? INT_INCRS : undefined },
    ];
    if (kinds.length > 1) axes.push({ scale: 'y2', side: 1, stroke: muted, grid: { show: false }, values: fmtTick(kinds[1]), size: 60, incrs: wholeKind(kinds[1]) ? INT_INCRS : undefined });
    const u = new uPlot({
      width: Math.max(280, el.clientWidth || 600),
      height: o.height || 220,
      tzDate: serverDate,
      legend: { show: true, live: true },
      cursor: { points: { size: 6 } },
      scales: { x: { time: true }, y: { range: (u, lo, hi) => [Math.min(0, lo), hi > 0 ? hi * 1.08 : 1] }, y2: { range: (u, lo, hi) => [Math.min(0, lo), hi > 0 ? hi * 1.08 : 1] } },
      axes,
      series: [{ value: (u, ts) => ts == null ? '' : bucketLabel(new Date(ts * 1000).toISOString(), o.step) }].concat(series.map((s, i) => ({
        label: s.label,
        scale: axisFor(s.kind),
        stroke: groupHue(s) || cssVar(!s.group.length && METRIC_COLOR[s.metric] ? METRIC_COLOR[s.metric] : PALETTE[i % PALETTE.length], '#58a6ff'),
        // A group coloured by its model keeps one colour across metrics, so
        // the second and later metrics are told apart by dash instead.
        dash: groupHue(s) && s.mk ? [[], [6, 4], [2, 3]][s.mk % 3] : undefined,
        width: 1.6,
        // Percentiles and ratios are null wherever a bucket has no sample, so
        // their lines break into isolated points: always draw the points.
        points: { show: data[0].length <= 60 || !wholeKind(s.kind), size: 4 },
        value: (u, v) => v === null || v === undefined ? '—' : stripTags(value(s.kind, v)),
      }))),
    }, data, el);
    el._uplot = u;
    if (!el._resizeObs && typeof ResizeObserver === 'function') {
      el._resizeObs = new ResizeObserver(() => {
        if (el._uplot && el.clientWidth) el._uplot.setSize({ width: el.clientWidth, height: el._uplot.height });
      });
      el._resizeObs.observe(el);
    }
  }

  function stripTags(html) { return String(html).replace(/<[^>]*>/g, ''); }

  // ---------------- page state <-> URL hash ----------------

  const LIST_KEYS = ['group', 'metrics', 'chart'];

  function encodeState(st) {
    const u = new URLSearchParams();
    for (const [k, v] of Object.entries(st || {})) {
      if (v === undefined || v === null || v === '' || v === false) continue;
      if (k === 'where') { v.forEach(w => u.append('where', w)); continue; }
      if (Array.isArray(v)) { if (v.length) u.set(k, v.join(',')); continue; }
      u.set(k, String(v));
    }
    return u.toString();
  }

  function decodeState(hash) {
    const u = new URLSearchParams(String(hash || '').replace(/^#/, ''));
    const st = { where: u.getAll('where') };
    for (const [k, v] of u.entries()) {
      if (k === 'where') continue;
      st[k] = LIST_KEYS.includes(k) ? v.split(',').filter(Boolean) : v;
    }
    if (st.compare !== undefined) st.compare = st.compare === 'true' || st.compare === '1';
    return st;
  }

  // link builds a /stats.html URL that opens the given state.
  function link(st) { return '/stats.html#' + encodeState(st); }

  return { api, attr, qs, value, escWhere, splitWhere, dimValue, dimHue, bucketLabel, fmtMS, table, eventsTable, seriesOf, chart, encodeState, decodeState, link, wallSeconds };
})();
