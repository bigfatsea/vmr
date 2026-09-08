// Ver 2026-09-06, VMR Forensics Dashboard Common Runtime (§5.6, §6.2, §6.3, §6.6)

const EXPECTED_MANIFEST_FORMAT = 11;

// versionBehavior decides UI status based on expected vs actual format versions (§6.6).
function versionBehavior(expected, actual) {
  if (actual === null || actual === undefined || actual === '') {
    return 'missing';
  }
  if (Number(actual) === Number(expected)) {
    return 'ok';
  }
  return 'banner';
}

// FmtTokens matches internal/fmtutil.FmtTokens bare-number compact style.
function FmtTokens(n) {
  if (n === null || n === undefined || isNaN(n)) return '0';
  n = Number(n);
  if (n >= 1_000_000_000) {
    return (n / 1e9).toFixed(2) + 'B';
  }
  if (n >= 1_000_000) {
    return (n / 1e6).toFixed(2) + 'M';
  }
  if (n >= 1_000) {
    return (n / 1e3).toFixed(1) + 'K';
  }
  return String(Math.trunc(n));
}

// FmtBytes matches internal/fmtutil.FmtBytes binary-prefix display.
function FmtBytes(n) {
  if (n === null || n === undefined || isNaN(n)) return '0B';
  n = Number(n);
  const TB = 1099511627776; // 1 << 40
  const GB = 1073741824;    // 1 << 30
  const MB = 1048576;       // 1 << 20
  const KB = 1024;          // 1 << 10
  if (n >= TB) return (n / TB).toFixed(1) + 'TB';
  if (n >= GB) return (n / GB).toFixed(1) + 'GB';
  if (n >= MB) return (n / MB).toFixed(1) + 'MB';
  if (n >= KB) return (n / KB).toFixed(1) + 'KB';
  return Math.trunc(n) + 'B';
}

// FmtPercent matches internal/fmtutil.FmtPercent.
function FmtPercent(f, decimals) {
  if (f === null || f === undefined || isNaN(f)) return '0.0%';
  const d = (decimals !== undefined && decimals !== null) ? decimals : 1;
  return (Number(f) * 100).toFixed(d) + '%';
}

// Currency symbols for the codes `-currency`/report.yaml can select; an
// unknown code falls back to the code itself as the prefix ("AUD 1.23").
const CURRENCY_SYMBOLS = {
  USD: '$',
  CNY: '¥',
  EUR: '€',
  GBP: '£',
  JPY: '¥',
};

// displayCurrency is the module default FmtCurrency prefixes when a call
// site doesn't pass an explicit code; pages set it from the slice's
// pricing.currency after loading finance.json (the amounts in the slices
// are already converted to that currency Go-side).
let displayCurrency = 'USD';

function setCurrency(ccy) {
  if (ccy) displayCurrency = String(ccy).toUpperCase();
}

function currencySymbol(ccy) {
  const c = String(ccy || displayCurrency || 'USD').toUpperCase();
  return CURRENCY_SYMBOLS[c] || c + ' ';
}

// FmtCurrency renders an amount in the display currency: 2 fixed decimals
// with the currency's symbol (or the code as prefix when unmapped).
function FmtCurrency(n, ccy) {
  if (n === null || n === undefined || isNaN(n)) return currencySymbol(ccy) + '0.00';
  return currencySymbol(ccy) + goFixed(n, 2);
}

const FmtCost = FmtCurrency;

// FmtCurrencyPrecise renders a micro-amount with 4 fixed decimals.
function FmtCurrencyPrecise(n, ccy) {
  if (n === null || n === undefined || isNaN(n)) return currencySymbol(ccy) + '0.0000';
  return currencySymbol(ccy) + goFixed(n, 4);
}

// goFixed renders x with exactly d decimals, byte-identical to Go's
// strconv %.Nf — the shared fixture (testdata/fmt_cases.json) pins both
// sides to the same string. Both round the double's exact binary value
// to nearest; they differ only on exact ties, where Go's strconv rounds
// half to even and Number#toFixed rounds away from zero (0.125 → "0.12"
// vs "0.13" — the same ledger total reading differently per surface,
// the very drift NEW-01 set out to kill). A tie exists only when the
// double is an odd multiple of 2^-(d+1) (its exact decimal expansion
// terminates at digit d+1 with a trailing 5), so the whole format is
// done in BigInt on the raw IEEE-754 bits: scale by 10^d, round
// half-to-even, place the decimal point. toFixed alone would also
// switch to scientific notation at ≥1e21, where Go keeps printing
// digits — unreachable for ledger amounts, handled here anyway.
function goFixed(n, d) {
  const neg = n < 0 || Object.is(n, -0);
  const x = Math.abs(n);
  if (!isFinite(x)) return isNaN(x) ? 'NaN' : (neg ? '-Inf' : '+Inf');
  const bits = new BigUint64Array(new Float64Array([x]).buffer)[0];
  const rawExp = Number((bits >> 52n) & 0x7ffn);
  let m, exp; // value = m × 2^exp
  if (rawExp === 0) { // subnormal: no implicit leading bit
    m = bits & 0xfffffffffffffn;
    exp = -1074;
  } else {
    m = (bits & 0xfffffffffffffn) | (1n << 52n);
    exp = rawExp - 1075;
  }
  let q; // round(value × 10^d) — the d-decimal fixed-point integer
  if (m === 0n) {
    q = 0n;
  } else {
    // normalize to m odd so tie detection below reads the true mantissa
    while ((m & 1n) === 0n) { m >>= 1n; exp++; }
    // value × 10^d = m × 2^(exp+d) × 5^d (10^d = 2^d·5^d)
    const shift = exp + d;
    const f = FIVE_POW_D[d];
    if (shift >= 0) {
      q = (m << BigInt(shift)) * f;
    } else {
      const den = 1n << BigInt(-shift);
      const num = m * f;
      q = num / den;
      const r = num % den;
      if (r * 2n > den || (r * 2n === den && (q & 1n) === 1n)) q++;
    }
  }
  let str = q.toString();
  if (d > 0) {
    while (str.length <= d) str = '0' + str;
    str = str.slice(0, str.length - d) + '.' + str.slice(-d);
  }
  return (neg ? '-' : '') + str;
}
const FIVE_POW_D = { 2: 25n, 4: 625n };

// FmtDuration formats milliseconds into human-readable duration.
function FmtDuration(ms) {
  if (ms === null || ms === undefined || isNaN(ms) || ms <= 0) return '0s';
  const sec = Math.round(ms / 1000);
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (h > 0) return `${h}h ${m}m ${s}s`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

// Auth management aligns with internal/server/status.html contract (§6.5).
const Auth = {
  getKey() {
    try {
      return localStorage.getItem('vmr_status_key') || '';
    } catch (_) {
      return '';
    }
  },
  setKey(k) {
    try {
      localStorage.setItem('vmr_status_key', (k || '').trim());
    } catch (_) {}
  },
  clearKey() {
    try {
      localStorage.removeItem('vmr_status_key');
    } catch (_) {}
  },
  getHeaders() {
    const key = this.getKey();
    const h = {};
    if (key) {
      h['Authorization'] = 'Bearer ' + key;
    }
    return h;
  }
};

// Theme manager supports dark/light/system modes.
const Theme = {
  getPreference() {
    try {
      return localStorage.getItem('vmr_theme') || 'auto';
    } catch (_) {
      return 'auto';
    }
  },
  setPreference(p) {
    try {
      localStorage.setItem('vmr_theme', p);
    } catch (_) {}
    this.apply(p);
  },
  apply(p) {
    if (typeof document === 'undefined') return;
    const root = document.documentElement;
    if (p === 'dark' || p === 'light') {
      root.setAttribute('data-theme', p);
    } else {
      root.removeAttribute('data-theme');
    }
  },
  init() {
    this.apply(this.getPreference());
    if (typeof window !== 'undefined' && window.matchMedia) {
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (this.getPreference() === 'auto') {
          this.apply('auto');
        }
      });
    }
  }
};

// esc HTML-escapes a string before it is interpolated into innerHTML.
// Conversation bodies (journey titles, tool args/results, LLM text, system
// prompt excerpts) reach these pages as data and must never be trusted as
// markup — the Markdown renderers have their own escapeHTML; this is its
// browser-side counterpart.
function esc(s) {
  if (s === null || s === undefined) return '';
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// wireHashReload makes an in-page `#data=` navigation actually reload the
// document. The skeleton pages that serve both a candidate list and a
// detail view (journey-viewer, journey-compare) switch via location.hash;
// browsers treat a bare fragment change as same-document and never re-run
// the page script, so clicking "open →" from the candidate list did
// nothing (copying the link into a fresh tab worked because that is a full
// load). A full reload is cheap for a local static file and keeps the
// one-page-two-roles design (§6.2 D13) intact.
function wireHashReload() {
  if (typeof window !== 'undefined' && window.addEventListener) {
    window.addEventListener('hashchange', function () {
      if (typeof location !== 'undefined' && location.reload) location.reload();
    });
  }
}

// downloadArtifact fetches an artifact with the current Auth headers and triggers
// browser download as a blob (§6.5, ISSUE-34/NEW-04), avoiding 401 on protected hosts.
function downloadArtifact(href, filename) {
  if (typeof fetch === 'undefined') return;
  fetch(href, { headers: Auth.getHeaders() })
    .then(function (res) {
      if (!res.ok) throw new Error('HTTP ' + res.status);
      return res.blob();
    })
    .then(function (blob) {
      if (typeof URL === 'undefined' || typeof document === 'undefined') return;
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = url;
      a.download = filename || href.split('/').pop();
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      setTimeout(function () { URL.revokeObjectURL(url); }, 10000);
    })
    .catch(function (err) {
      if (typeof alert !== 'undefined') alert('Download failed: ' + err.message);
    });
}

// sourceBar returns the shared "download the underlying artifact" strip
// every page carries (G3): a row of relative links to the .json / .md /
// .jsonl files the page rendered from. links is [{label, href}].
// Uses downloadArtifact on click so Bearer auth is attached when configured.
function sourceBar(links) {
  if (!links || links.length === 0) return '';
  const parts = links
    .filter(function (l) { return l && l.href; })
    .map(function (l) {
      return '<a href="' + esc(l.href) + '" download onclick="event.preventDefault(); downloadArtifact(this.getAttribute(\'href\'), this.getAttribute(\'download\'));">' + esc(l.label || l.href) + '</a>';
    });
  return '<div class="source-bar">↓ Source: ' + parts.join(' &middot; ') + '</div>';
}

// getDataParam retrieves #data=... from location.hash (§6.2 D13).
function getDataParam() {
  const loc = typeof window !== 'undefined' && window.location ? window.location : (typeof location !== 'undefined' ? location : (typeof globalThis !== 'undefined' && globalThis.location ? globalThis.location : null));
  if (!loc || !loc.hash) return null;
  const hash = loc.hash.replace(/^#/, '');
  const match = hash.match(/(?:^|&)data=([^&]+)/);
  return match ? decodeURIComponent(match[1]) : null;
}

// fetchSlice fetches a relative JSON slice with Auth and handles 401/403.
async function fetchSlice(relPath) {
  if (typeof location !== 'undefined' && location.protocol === 'file:') {
    const err = new Error('file_protocol');
    err.code = 'file_protocol';
    throw err;
  }
  const headers = Auth.getHeaders();
  const resp = await fetch(relPath, { headers });
  if (resp.status === 401 || resp.status === 403) {
    const err = new Error('unauthorized');
    err.status = resp.status;
    throw err;
  }
  if (!resp.ok) {
    const err = new Error(`HTTP ${resp.status}`);
    err.status = resp.status;
    throw err;
  }
  return await resp.json();
}

// SVG Chart Helpers (Zero-dependency inline SVG, §6.3)

// svgLineChart renders an interactive SVG line chart.
function svgLineChart({
  data = [],
  xKey = 'x',
  yKey = 'y',
  title = '',
  yLabel = '',
  width = 600,
  height = 200,
  stroke = 'var(--trace)',
  fillArea = true,
  yFormatter = (v) => String(v)
}) {
  if (!data || data.length === 0) {
    return `<div class="chart-empty">No data available</div>`;
  }

  const pTop = 25, pRight = 20, pBottom = 30, pLeft = 55;
  const plotW = width - pLeft - pRight;
  const plotH = height - pTop - pBottom;

  const yVals = data.map(d => Number(d[yKey]) || 0);
  const minY = 0;
  const maxY = Math.max(...yVals, 1);

  const n = data.length;
  const points = data.map((d, i) => {
    const x = pLeft + (n > 1 ? (i / (n - 1)) * plotW : plotW / 2);
    const yVal = Number(d[yKey]) || 0;
    const y = pTop + plotH - ((yVal - minY) / (maxY - minY)) * plotH;
    return { x, y, val: yVal, label: d[xKey] };
  });

  const pathD = points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x.toFixed(1)} ${p.y.toFixed(1)}`).join(' ');
  const areaD = `${pathD} L ${points[points.length - 1].x.toFixed(1)} ${(pTop + plotH).toFixed(1)} L ${points[0].x.toFixed(1)} ${(pTop + plotH).toFixed(1)} Z`;

  // Grid lines
  const gridCount = 4;
  let gridSvg = '';
  for (let i = 0; i <= gridCount; i++) {
    const y = pTop + (plotH / gridCount) * i;
    const val = maxY - (i / gridCount) * maxY;
    gridSvg += `<line x1="${pLeft}" y1="${y}" x2="${width - pRight}" y2="${y}" stroke="var(--rule)" stroke-dasharray="3,3" stroke-width="1"/>`;
    gridSvg += `<text x="${pLeft - 8}" y="${y + 3.5}" fill="var(--ink-dim)" font-size="10" font-family="var(--mono)" text-anchor="end">${yFormatter(val)}</text>`;
  }

  // X labels
  let xLabelsSvg = '';
  const xStep = Math.max(1, Math.floor(n / 6));
  for (let i = 0; i < n; i += xStep) {
    const p = points[i];
    xLabelsSvg += `<text x="${p.x}" y="${height - 8}" fill="var(--ink-dim)" font-size="10" font-family="var(--mono)" text-anchor="middle">${p.label}</text>`;
  }

  // Points & tooltips
  const dotsSvg = points.map(p => `
    <circle cx="${p.x.toFixed(1)}" cy="${p.y.toFixed(1)}" r="3" fill="${stroke}">
      <title>${p.label}: ${yFormatter(p.val)}</title>
    </circle>
  `).join('');

  return `
    <svg class="forensics-chart" viewBox="0 0 ${width} ${height}" width="100%" height="${height}">
      ${title ? `<text x="${pLeft}" y="14" fill="var(--ink)" font-size="11" font-weight="700" font-family="var(--sans)">${title}</text>` : ''}
      ${gridSvg}
      <line x1="${pLeft}" y1="${pTop + plotH}" x2="${width - pRight}" y2="${pTop + plotH}" stroke="var(--rule)" stroke-width="1"/>
      ${fillArea ? `<path d="${areaD}" fill="${stroke}" opacity="0.12"/>` : ''}
      <path d="${pathD}" fill="none" stroke="${stroke}" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>
      ${dotsSvg}
      ${xLabelsSvg}
    </svg>
  `;
}

// svgBarChart renders an interactive SVG bar chart.
function svgBarChart({
  data = [],
  xKey = 'x',
  yKey = 'y',
  title = '',
  width = 600,
  height = 200,
  barColor = 'var(--amber)',
  yFormatter = (v) => String(v)
}) {
  if (!data || data.length === 0) {
    return `<div class="chart-empty">No data available</div>`;
  }

  const pTop = 25, pRight = 20, pBottom = 35, pLeft = 55;
  const plotW = width - pLeft - pRight;
  const plotH = height - pTop - pBottom;

  const yVals = data.map(d => Number(d[yKey]) || 0);
  const maxY = Math.max(...yVals, 1);
  const n = data.length;

  const barWidth = Math.max(4, Math.min(36, (plotW / n) * 0.65));
  const slotWidth = plotW / n;

  // Grid
  let gridSvg = '';
  const gridCount = 4;
  for (let i = 0; i <= gridCount; i++) {
    const y = pTop + (plotH / gridCount) * i;
    const val = maxY - (i / gridCount) * maxY;
    gridSvg += `<line x1="${pLeft}" y1="${y}" x2="${width - pRight}" y2="${y}" stroke="var(--rule)" stroke-dasharray="3,3" stroke-width="1"/>`;
    gridSvg += `<text x="${pLeft - 8}" y="${y + 3.5}" fill="var(--ink-dim)" font-size="10" font-family="var(--mono)" text-anchor="end">${yFormatter(val)}</text>`;
  }

  // Bars
  let barsSvg = '';
  data.forEach((d, i) => {
    const val = Number(d[yKey]) || 0;
    const barH = (val / maxY) * plotH;
    const x = pLeft + i * slotWidth + (slotWidth - barWidth) / 2;
    const y = pTop + plotH - barH;
    const label = String(d[xKey] || '');
    const shortLabel = label.length > 10 ? label.slice(0, 9) + '…' : label;

    barsSvg += `
      <rect x="${x.toFixed(1)}" y="${y.toFixed(1)}" width="${barWidth.toFixed(1)}" height="${barH.toFixed(1)}" rx="3" fill="${barColor}">
        <title>${label}: ${yFormatter(val)}</title>
      </rect>
      <text x="${(x + barWidth / 2).toFixed(1)}" y="${height - 10}" fill="var(--ink-dim)" font-size="10" font-family="var(--mono)" text-anchor="middle">
        <title>${label}</title>
        ${shortLabel}
      </text>
    `;
  });

  return `
    <svg class="forensics-chart" viewBox="0 0 ${width} ${height}" width="100%" height="${height}">
      ${title ? `<text x="${pLeft}" y="14" fill="var(--ink)" font-size="11" font-weight="700" font-family="var(--sans)">${title}</text>` : ''}
      ${gridSvg}
      <line x1="${pLeft}" y1="${pTop + plotH}" x2="${width - pRight}" y2="${pTop + plotH}" stroke="var(--rule)" stroke-width="1"/>
      ${barsSvg}
    </svg>
  `;
}

// svgHeatmap renders a 24-hour activity heatmap (§6.3).
function svgHeatmap({
  hours = [], // array of 24 counts or objects { hour: 0..23, count: N }
  title = 'Hourly Traffic Distribution',
  width = 600,
  height = 90
}) {
  const counts = Array.from({ length: 24 }, (_, i) => {
    const item = hours.find(h => Number(h.hour ?? i) === i);
    return item ? (Number(item.count ?? item.requests ?? item.tokens_in ?? 0)) : 0;
  });

  const maxVal = Math.max(...counts, 1);
  const pLeft = 40, pTop = 22, pRight = 20, pBottom = 24;
  const cellW = (width - pLeft - pRight) / 24;
  const cellH = height - pTop - pBottom;

  let cellsSvg = '';
  for (let i = 0; i < 24; i++) {
    const c = counts[i];
    const intensity = c / maxVal;
    const x = pLeft + i * cellW;
    // Color interpolation: base theme rule to amber
    const opacity = (0.15 + intensity * 0.85).toFixed(2);
    const fill = intensity > 0 ? 'var(--amber)' : 'var(--rule)';

    cellsSvg += `
      <rect x="${x.toFixed(1)}" y="${pTop}" width="${(cellW - 2).toFixed(1)}" height="${cellH}" rx="2" fill="${fill}" opacity="${opacity}">
        <title>${String(i).padStart(2, '0')}:00 - ${c} requests</title>
      </rect>
      ${i % 3 === 0 ? `<text x="${(x + cellW / 2).toFixed(1)}" y="${height - 8}" fill="var(--ink-dim)" font-size="9" font-family="var(--mono)" text-anchor="middle">${String(i).padStart(2, '0')}</text>` : ''}
    `;
  }

  return `
    <svg class="forensics-chart" viewBox="0 0 ${width} ${height}" width="100%" height="${height}">
      <text x="${pLeft}" y="14" fill="var(--ink)" font-size="11" font-weight="700" font-family="var(--sans)">${title}</text>
      ${cellsSvg}
    </svg>
  `;
}

// svgLatencyPlot renders endpoint P50 / P95 / Max duration percentiles from
// reliability.json's own fields (§6.3). Only real recorded percentiles are
// plotted — an endpoint with no `dur_ms_p50` is listed as "no percentile
// data" below the chart rather than having values fabricated from a mean
// (a made-up P99 reads exactly like a measured one).
function svgLatencyPlot({
  endpoints = [],
  title = 'Endpoint Duration Percentiles (P50 / P95 / Max ms)',
  width = 600,
  height = 220
}) {
  if (!endpoints || endpoints.length === 0) {
    return `<div class="chart-empty">No endpoint latency data available</div>`;
  }

  const pTop = 25, pRight = 80, pBottom = 35, pLeft = 120;
  const plotW = width - pLeft - pRight;
  const plotH = height - pTop - pBottom;

  const hasPct = (e) => e && (e.dur_ms_p50 != null || e.p50_ms != null);
  const data = endpoints.filter(hasPct);
  const noPct = endpoints.filter(e => !hasPct(e));
  const noPctNote = noPct.length > 0
    ? `<div class="chart-empty" style="margin-top:6px;">${noPct.length} endpoint${noPct.length > 1 ? 's have' : ' has'} no recorded percentile data (not plotted).</div>`
    : '';
  if (data.length === 0) {
    return `<div class="chart-empty">No latency percentiles recorded</div>${noPctNote}`;
  }

  const items = data.map(d => {
    const p50 = Number(d.dur_ms_p50 != null ? d.dur_ms_p50 : d.p50_ms);
    const p95 = Number(d.dur_ms_p95 != null ? d.dur_ms_p95 : (d.p95_ms != null ? d.p95_ms : p50));
    const mx = Number(d.dur_ms_max != null ? d.dur_ms_max : p95);
    let label = d.endpoint || d.model || 'unknown';
    if (d.dur_low_n) label += ' (low-n)';
    return { label, p50, p95, mx };
  });

  const maxVal = Math.max(...items.flatMap(d => [d.p50, d.p95, d.mx]), 100);
  const rowH = plotH / items.length;

  let rowsSvg = '';
  items.forEach((item, i) => {
    const y = pTop + i * rowH + rowH / 2;
    const x50 = pLeft + (item.p50 / maxVal) * plotW;
    const x95 = pLeft + (item.p95 / maxVal) * plotW;
    const xmx = pLeft + (item.mx / maxVal) * plotW;

    const shortLabel = item.label.length > 18 ? '…' + item.label.slice(-17) : item.label;

    rowsSvg += `
      <text x="${pLeft - 10}" y="${y + 3.5}" fill="var(--ink)" font-size="10" font-family="var(--mono)" text-anchor="end">
        <title>${item.label}</title>
        ${shortLabel}
      </text>
      <!-- Line connecting P50 to Max -->
      <line x1="${x50.toFixed(1)}" y1="${y}" x2="${xmx.toFixed(1)}" y2="${y}" stroke="var(--rule)" stroke-width="2"/>
      <!-- P50 Dot -->
      <circle cx="${x50.toFixed(1)}" cy="${y}" r="4" fill="var(--go)">
        <title>P50: ${item.p50}ms</title>
      </circle>
      <!-- P95 Dot -->
      <circle cx="${x95.toFixed(1)}" cy="${y}" r="4" fill="var(--amber)">
        <title>P95: ${item.p95}ms</title>
      </circle>
      <!-- Max Dot -->
      <circle cx="${xmx.toFixed(1)}" cy="${y}" r="4" fill="var(--alert)">
        <title>Max: ${item.mx}ms</title>
      </circle>
      <text x="${(xmx + 6).toFixed(1)}" y="${y + 3.5}" fill="var(--ink-dim)" font-size="9" font-family="var(--mono)">${item.mx}ms</text>
    `;
  });

  // Legend
  const legendSvg = `
    <g transform="translate(${width - 78}, 14)" font-size="9" font-family="var(--mono)">
      <circle cx="0" cy="0" r="3" fill="var(--go)"/>
      <text x="6" y="3" fill="var(--ink-dim)">P50</text>
      <circle cx="26" cy="0" r="3" fill="var(--amber)"/>
      <text x="32" y="3" fill="var(--ink-dim)">P95</text>
      <circle cx="52" cy="0" r="3" fill="var(--alert)"/>
      <text x="58" y="3" fill="var(--ink-dim)">Max</text>
    </g>
  `;

  return `
    <svg class="forensics-chart" viewBox="0 0 ${width} ${height}" width="100%" height="${height}">
      <text x="10" y="14" fill="var(--ink)" font-size="11" font-weight="700" font-family="var(--sans)">${title}</text>
      ${legendSvg}
      <line x1="${pLeft}" y1="${pTop}" x2="${pLeft}" y2="${pTop + plotH}" stroke="var(--rule)" stroke-width="1"/>
      ${rowsSvg}
    </svg>
  ${noPctNote}`;
}

// Module export for Node.js testing (§5.6)
if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    EXPECTED_MANIFEST_FORMAT,
    versionBehavior,
    FmtTokens,
    FmtBytes,
    FmtPercent,
    FmtCurrency,
    FmtCost,
    FmtCurrencyPrecise,
    setCurrency,
    FmtDuration,
    Auth,
    Theme,
    esc,
    downloadArtifact,
    wireHashReload,
    sourceBar,
    getDataParam,
    svgLineChart,
    svgBarChart,
    svgHeatmap,
    svgLatencyPlot,
  };
}
