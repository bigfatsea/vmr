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

// FmtCurrency renders dollar amount with 2 fixed decimals.
function FmtCurrency(n) {
  if (n === null || n === undefined || isNaN(n)) return '$0.00';
  return '$' + Number(n).toFixed(2);
}

const FmtCost = FmtCurrency;

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
    const root = document.documentElement;
    if (p === 'dark' || p === 'light') {
      root.setAttribute('data-theme', p);
    } else {
      root.removeAttribute('data-theme');
    }
  },
  init() {
    this.apply(this.getPreference());
    if (window.matchMedia) {
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (this.getPreference() === 'auto') {
          this.apply('auto');
        }
      });
    }
  }
};

// getDataParam retrieves #data=... from location.hash (§6.2 D13).
function getDataParam() {
  if (typeof location === 'undefined' || !location.hash) return null;
  const hash = location.hash.replace(/^#/, '');
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
    const item = hours.find(h => Number(h.hour ?? h.Hour ?? i) === i);
    return item ? (Number(item.count ?? item.Requests ?? item.requests ?? item.TokensIn ?? 0)) : 0;
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

// svgLatencyPlot renders endpoint P50 / P90 / P99 latency percentiles (§6.3).
function svgLatencyPlot({
  endpoints = [],
  title = 'Endpoint Latency Percentiles (P50 / P90 / P99 ms)',
  width = 600,
  height = 220
}) {
  if (!endpoints || endpoints.length === 0) {
    return `<div class="chart-empty">No endpoint latency data available</div>`;
  }

  const pTop = 25, pRight = 80, pBottom = 35, pLeft = 120;
  const plotW = width - pLeft - pRight;
  const plotH = height - pTop - pBottom;

  const data = endpoints.filter(e => (e.P50MS || e.P90MS || e.P99MS || e.p50_ms || e.p90_ms || e.p99_ms || e.DurMS || e.dur_ms));
  if (data.length === 0) {
    return `<div class="chart-empty">No latency metrics recorded</div>`;
  }

  const items = data.map(d => {
    const p50 = Number(d.P50MS || d.p50_ms || (d.DurMS ? d.DurMS * 0.7 : 0));
    const p90 = Number(d.P90MS || d.p90_ms || d.DurMS || d.dur_ms || 0);
    const p99 = Number(d.P99MS || d.p99_ms || (d.DurMS ? d.DurMS * 1.3 : 0));
    const label = d.Endpoint || d.endpoint || d.Model || d.model || 'unknown';
    return { label, p50, p90, p99 };
  });

  const maxVal = Math.max(...items.flatMap(d => [d.p50, d.p90, d.p99]), 100);
  const rowH = plotH / items.length;

  let rowsSvg = '';
  items.forEach((item, i) => {
    const y = pTop + i * rowH + rowH / 2;
    const x50 = pLeft + (item.p50 / maxVal) * plotW;
    const x90 = pLeft + (item.p90 / maxVal) * plotW;
    const x99 = pLeft + (item.p99 / maxVal) * plotW;

    const shortLabel = item.label.length > 18 ? '…' + item.label.slice(-17) : item.label;

    rowsSvg += `
      <text x="${pLeft - 10}" y="${y + 3.5}" fill="var(--ink)" font-size="10" font-family="var(--mono)" text-anchor="end">
        <title>${item.label}</title>
        ${shortLabel}
      </text>
      <!-- Line connecting P50 to P99 -->
      <line x1="${x50.toFixed(1)}" y1="${y}" x2="${x99.toFixed(1)}" y2="${y}" stroke="var(--rule)" stroke-width="2"/>
      <!-- P50 Dot -->
      <circle cx="${x50.toFixed(1)}" cy="${y}" r="4" fill="var(--go)">
        <title>P50: ${item.p50}ms</title>
      </circle>
      <!-- P90 Dot -->
      <circle cx="${x90.toFixed(1)}" cy="${y}" r="4" fill="var(--amber)">
        <title>P90: ${item.p90}ms</title>
      </circle>
      <!-- P99 Dot -->
      <circle cx="${x99.toFixed(1)}" cy="${y}" r="4" fill="var(--alert)">
        <title>P99: ${item.p99}ms</title>
      </circle>
      <text x="${(x99 + 6).toFixed(1)}" y="${y + 3.5}" fill="var(--ink-dim)" font-size="9" font-family="var(--mono)">${item.p99}ms</text>
    `;
  });

  // Legend
  const legendSvg = `
    <g transform="translate(${width - 75}, 14)" font-size="9" font-family="var(--mono)">
      <circle cx="0" cy="0" r="3" fill="var(--go)"/>
      <text x="6" y="3" fill="var(--ink-dim)">P50</text>
      <circle cx="26" cy="0" r="3" fill="var(--amber)"/>
      <text x="32" y="3" fill="var(--ink-dim)">P90</text>
      <circle cx="52" cy="0" r="3" fill="var(--alert)"/>
      <text x="58" y="3" fill="var(--ink-dim)">P99</text>
    </g>
  `;

  return `
    <svg class="forensics-chart" viewBox="0 0 ${width} ${height}" width="100%" height="${height}">
      <text x="10" y="14" fill="var(--ink)" font-size="11" font-weight="700" font-family="var(--sans)">${title}</text>
      ${legendSvg}
      <line x1="${pLeft}" y1="${pTop}" x2="${pLeft}" y2="${pTop + plotH}" stroke="var(--rule)" stroke-width="1"/>
      ${rowsSvg}
    </svg>
  `;
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
    FmtDuration,
    Auth,
    Theme,
    getDataParam,
    svgLineChart,
    svgBarChart,
    svgHeatmap,
    svgLatencyPlot,
  };
}
