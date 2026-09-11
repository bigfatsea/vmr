/* console.js — single source for the shared console runtime (contracts.md §4).
   Formatting, overlay plumbing and VMRAuth are semantic copies of the demo
   pages' inline blocks; mountConsole / guard / ConsoleAlerts implement the
   frozen §4 API, consumed as-is by the page tasks. Pages carry the JS
   injection marker in a comment and get this file inlined once at server
   start. mountConsole() is invoked by each page's own script. */
'use strict';

/* ===================== formatting (design §2-6) ===================== */
// Humanized values carry two decimals, but a trailing ".00" is noise on
// structural constants (200K context, 100M budget) — strip it.
function dec2(v) { return v.toFixed(2).replace(/\.00$/, ''); }
function fmtKMG(v) {
  if (!isFinite(v)) return '—';
  if (v >= 1e9) return dec2(v / 1e9) + 'B';
  if (v >= 1e6) return dec2(v / 1e6) + 'M';
  if (v >= 1e3) return dec2(v / 1e3) + 'K';
  return String(Math.round(v));
}
function fmtRate(v) {
  if (!isFinite(v) || v <= 0) return '—';
  if (v >= 1e9) return dec2(v / 1e9) + 'B';
  if (v >= 1e6) return dec2(v / 1e6) + 'M';
  if (v >= 1e3) return dec2(v / 1e3) + 'K';
  return dec2(v);
}
// Byte quantities carry their unit separated by a space (489 GB, 1.20 GB).
function fmtBytes(v) {
  if (!isFinite(v)) return '—';
  if (v >= 1073741824) return dec2(v / 1073741824) + ' GB';
  if (v >= 1048576) return dec2(v / 1048576) + ' MB';
  if (v >= 1024) return dec2(v / 1024) + ' KB';
  return Math.round(v) + ' B';
}
function fmtPct(v) { return dec2(v) + '%'; }
function fmtInt(v) { return Math.round(v).toLocaleString('en-US'); }
// Headroom is the same dec2 product as every other humanized ratio (§8.5).
function fmtHeadroom(v) { return (v == null || !isFinite(v)) ? '—' : dec2(v); }
function fmtAge(rfc3339) {
  if (!rfc3339) return '—';
  const s = Math.max(0, Math.floor((Date.now() - new Date(rfc3339).getTime()) / 1000));
  return fmtDur(s);
}
function fmtDur(s) {
  if (s < 60) return s + 's';
  if (s < 3600) return Math.floor(s / 60) + 'm' + String(s % 60).padStart(2, '0') + 's';
  if (s < 86400) return Math.floor(s / 3600) + 'h' + String(Math.floor((s % 3600) / 60)).padStart(2, '0') + 'm';
  // Days tier keeps hours+minutes so a month-out quota reset reads "5d8h30m", not "128h30m".
  return Math.floor(s / 86400) + 'd' + Math.floor((s % 86400) / 3600) + 'h' + String(Math.floor((s % 3600) / 60)).padStart(2, '0') + 'm';
}
function fmtMMSS(sec) { return Math.floor(sec / 60) + ':' + String(sec % 60).padStart(2, '0'); }
function clockAt(minsAgo) {
  const d = new Date(Date.now() - minsAgo * 60000);
  return String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0') + ':' +
         String(d.getSeconds()).padStart(2, '0');
}
// transport mode, spelled out — same vocabulary everywhere it appears
const MODE_TAG = {
  stream: '<span class="mode m-stream" title="streaming response (SSE)">stream</span>',
  json: '<span class="mode m-json" title="non-streaming response (single JSON body)">json</span>',
};
const modeTag = isStream => MODE_TAG[isStream ? 'stream' : 'json'];
function esc(s) { return String(s).replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c])); }

function toast(msg) {
  let t = document.querySelector('.toast');
  if (!t) { t = document.createElement('div'); t.className = 'toast'; document.body.appendChild(t); }
  t.textContent = msg; t.classList.add('show');
  clearTimeout(t._h); t._h = setTimeout(() => t.classList.remove('show'), 2200);
}

/* ============ overlay plumbing: one behaviour for every overlay ============
   Esc close, backdrop close, focus moves in on open, focus restored on
   close. Auth and alerts modals must not develop two sets of manners. */
let lastFocus = null;
function openOverlay(el) {
  lastFocus = document.activeElement;
  el.classList.remove('hidden');
  const f = el.querySelector('input,button');
  if (f) setTimeout(() => f.focus(), 30);
}
function closeOverlay(el) {
  el.classList.add('hidden');
  if (el._onclosed) el._onclosed();
  if (lastFocus && lastFocus.focus) lastFocus.focus();
}
function wireOverlay(el) {
  el.addEventListener('click', e => { if (e.target === el) closeOverlay(el); });
}
document.addEventListener('keydown', e => {
  if (e.key !== 'Escape') return;
  document.querySelectorAll('.modal-overlay:not(.hidden)').forEach(closeOverlay);
});

/* ===================== VMRAuth — the one key flow =====================
   Stored in localStorage['vmr_key']; the pre-unification name vmr_status_key
   is read through (and dropped on the next set) so an operator who already
   unlocked the old console never re-enters the key. */
const VMRAuth = {
  KEY: 'vmr_key', LEGACY: 'vmr_status_key',
  get() { return localStorage.getItem(this.KEY) || localStorage.getItem(this.LEGACY) || ''; },
  set(k) { const v = (k || '').trim(); localStorage.setItem(this.KEY, v); localStorage.removeItem(this.LEGACY); return v; },
  clear() { localStorage.removeItem(this.KEY); localStorage.removeItem(this.LEGACY); },
  has() { return !!this.get(); },
  tail() { const k = this.get(); return k ? '····' + k.slice(-4) : ''; },
  // guard wraps a page's data loader. On 401 the shared modal opens and the
  // original request is retried exactly once per saved key — the loader
  // re-reads VMRAuth.get() when building the retry, so no key plumbing
  // crosses this boundary. A retry that still 401s reopens the modal with
  // the error line (§6).
  async guard(doFetch) {
    let resp = await doFetch();
    if (resp.status !== 401) return resp;
    let errMsg = '';
    for (;;) {
      const ok = await this._prompt(errMsg);
      if (!ok) return resp;                       // cancelled — hand the 401 back
      resp = await doFetch();
      if (resp.status !== 401) return resp;
      errMsg = '401 — invalid or missing API key';
    }
  },
  // one outstanding guard continuation; any close path (Esc, backdrop,
  // cancel, clear) settles it with false — the promise must never leak
  _prompt(errMsg) {
    return new Promise(resolve => {
      authSettle = resolve;
      const inp = document.getElementById('vmr-key-input');
      inp.value = VMRAuth.get();
      document.getElementById('vmr-key-err').textContent = errMsg || '';
      openOverlay(keyOverlay);
    });
  },
};
let authSettle = null;

/* --- auth modal: injected once, shared by every page --- */
const keyOverlay = (() => {
  const div = document.createElement('div');
  div.className = 'modal-overlay hidden';
  div.innerHTML = `
    <div class="modal" role="dialog" aria-modal="true" aria-label="Authentication">
      <h2>Authentication Required</h2>
      <p>This VMR instance is protected by API key authentication. The key is stored only in this browser's localStorage — it is never sent anywhere but this instance.</p>
      <input class="input" type="password" id="vmr-key-input" placeholder="sk-..." autocomplete="off">
      <div class="err" id="vmr-key-err"></div>
      <div class="modal-actions">
        <button class="btn ghost left" id="vmr-key-clear">Clear stored key</button>
        <button class="btn" id="vmr-key-cancel">Cancel</button>
        <button class="btn primary" id="vmr-key-save">Connect</button>
      </div>
    </div>`;
  document.body.appendChild(div);
  wireOverlay(div);
  div._onclosed = () => { if (authSettle) { const r = authSettle; authSettle = null; r(false); } };
  div.querySelector('#vmr-key-save').onclick = () => {
    const v = div.querySelector('#vmr-key-input').value.trim();
    if (!v) { div.querySelector('#vmr-key-err').textContent = 'Enter the API key for this VMR instance.'; return; }
    if (authSettle) { const r = authSettle; authSettle = null; r(true); }   // detach before the close hook fires
    VMRAuth.set(v); syncKeyBtn(); closeOverlay(div); toast('Connected — key ' + VMRAuth.tail());
  };
  div.querySelector('#vmr-key-cancel').onclick = () => closeOverlay(div);
  div.querySelector('#vmr-key-clear').onclick = () => { VMRAuth.clear(); syncKeyBtn(); closeOverlay(div); toast('Stored key cleared'); };
  return div;
})();
// direct open (Key button path) — no guard continuation involved
function openKeyModal() {
  document.getElementById('vmr-key-input').value = VMRAuth.get();
  document.getElementById('vmr-key-err').textContent = '';
  openOverlay(keyOverlay);
}
function syncKeyBtn() {
  const b = document.getElementById('hd-key');
  if (!b) return;
  b.textContent = VMRAuth.has() ? '🔑 ' + VMRAuth.tail() : '🔑 Set Key';
  b.title = VMRAuth.has() ? 'Key stored in this browser — click to change or clear' : 'Set the API key for this VMR instance';
}

/* ===================== ConsoleAlerts =====================
   /status alerts[] (contracts §2.1) is actionable state only; a rolling
   metric would pin the badge permanently and train the operator to ignore
   it. The glyph carries the top severity, the count carries the volume. */
const ConsoleAlerts = {
  set(items) {
    const bell = document.getElementById('warn-bell');
    const clearBox = document.getElementById('warn-clear');
    const errBox = document.getElementById('warn-errors');
    const warnBox = document.getElementById('warn-warnings');
    const sep = document.getElementById('warn-sep');
    if (!bell || !clearBox || !errBox || !warnBox || !sep) return;   // skeleton not mounted yet
    items = items || [];
    const n = items.length;
    const ico = document.getElementById('warn-ico');
    const cnt = document.getElementById('warn-count');
    if (!n) {
      clearBox.hidden = false;
      errBox.hidden = true;
      warnBox.hidden = true;
      sep.hidden = true;
      bell.className = 'warn-btn quiet'; bell.title = 'No warnings';
      ico.textContent = '⚠️'; cnt.textContent = '0';
    } else {
      clearBox.hidden = true;
      const errors = items.filter(a => a.severity === 'error');
      const warnings = items.filter(a => a.severity !== 'error');

      const renderItem = a => `<li>${esc(a.message)}</li>`;

      errBox.hidden = errors.length === 0;
      document.getElementById('warn-errors-list').innerHTML = errors.length > 0 ? errors.map(renderItem).join('') : '';

      warnBox.hidden = warnings.length === 0;
      document.getElementById('warn-warnings-list').innerHTML = warnings.length > 0 ? warnings.map(renderItem).join('') : '';

      sep.hidden = !(errors.length > 0 && warnings.length > 0);

      const hasErr = errors.length > 0;
      bell.className = 'warn-btn ' + (hasErr ? 'has-err' : 'has-warn');
      ico.textContent = hasErr ? '🚨' : '⚠️';
      cnt.textContent = String(n);
      bell.title = n + ' actionable item(s) — click for detail';
    }
  },
};
const warnOverlay = (() => {
  const div = document.createElement('div');
  div.className = 'modal-overlay hidden';
  div.innerHTML = `
    <div class="modal warn-modal" role="dialog" aria-modal="true" aria-label="Warnings and errors">
      <h2>Warnings &amp; Errors</h2>
      <div id="warn-clear" class="wrow" hidden>
        <span class="badge b-ok">clear</span>
        <span class="t-dim">No config issues and no degraded endpoints.</span>
      </div>
      <div id="warn-errors" class="warn-group" hidden>
        <div class="warn-group-title">Errors</div>
        <ul id="warn-errors-list" class="warn-list"></ul>
      </div>
      <hr id="warn-sep" class="warn-sep" hidden>
      <div id="warn-warnings" class="warn-group" hidden>
        <div class="warn-group-title">Warnings</div>
        <ul id="warn-warnings-list" class="warn-list"></ul>
      </div>
      <div class="modal-actions"><button class="btn" id="warn-close">Close</button></div>
    </div>`;
  document.body.appendChild(div);
  wireOverlay(div);
  div.querySelector('#warn-close').onclick = () => closeOverlay(div);
  return div;
})();

/* ===================== mountConsole =====================
   Injects the unified skeleton: header rows 1+2 and footer (the two modals
   above are already in the DOM). Row 1 is identical on every page — the
   refresh slot carries the page's data-state semantics; row 2 is the page's
   own rail. Returns the page-facing API (refresh hooks, uptime, rail
   counts, stream state, footer identity). */
function mountConsole(opts) {
  const o = opts || {};
  const active = o.active || '';
  const rail = o.rail || [];
  const refresh = o.refresh || 'static';
  const full = active === 'log';   // Log: full-bleed header/footer, page owns scrolling

  const header = document.createElement('header');
  header.className = 'console-header' + (full ? ' full' : '');
  // one slot, one thing: what state is this page's data in right now (§4)
  const refreshSlot = refresh === 'countdown'
    ? `<button class="refresh-pill" id="hd-refresh" title="Time to the next automatic refresh — click to refresh now">
        <svg height="11" width="11" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 3a5 5 0 1 0 4.546 2.914.5.5 0 1 1 .908-.417A6 6 0 1 1 8 2v1z"/><path d="M8 4.466V.534a.25.25 0 0 1 .41-.192l2.36 1.966c.12.1.12.284 0 .384L8.41 4.658A.25.25 0 0 1 8 4.466z"/></svg>
        <span id="cd">5:00</span>
      </button>`
    : refresh === 'stream'
      ? `<span class="pill p-connecting" id="hd-conn" title="Live log stream state">connecting</span>`
      : `<span class="pill p-paused" id="hd-conn" title="Help is a static page — there is nothing here to refresh">static</span>`;
  header.innerHTML = `
    <div class="hd-inner${full ? ' full' : ''}">
      <a class="brand" href="/status.html">
        <svg height="22" width="22" viewBox="0 0 32 32"><rect width="32" height="32" rx="7" fill="#161b22"/><path d="M8 8l8 13 8-13" fill="none" stroke="#58a6ff" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round"/></svg>
        <b>VMR</b>
      </a>
      <nav class="hd-nav">
        <a class="nav-link${active === 'overview' ? ' active' : ''}" href="/status.html">Overview</a>
        <a class="nav-link${active === 'log' ? ' active' : ''}" href="/log.html">Live &amp; Log</a>
        <a class="nav-link${active === 'help' ? ' active' : ''}" href="/help.html">Help</a>
      </nav>
      <div class="hd-right">${refreshSlot}
        <span class="badge b-dim hd-uptime" id="hd-uptime" title="time since process start">—</span>
        <button class="btn sm" id="hd-key" title="Manage API key">🔑 Set Key</button>
        <button class="warn-btn" id="warn-bell" aria-label="Warnings and errors" title="Actionable warnings &amp; errors">
          <span class="ico" id="warn-ico">⚠️</span><span id="warn-count">0</span>
        </button>
      </div>
    </div>
    <div class="hd-rail"><div class="rail-inner${full ? ' full' : ''}" id="hd-rail-inner"></div></div>`;
  document.body.prepend(header);

  const railInner = header.querySelector('#hd-rail-inner');
  rail.forEach((item, i) => {
    const a = document.createElement('a');
    a.className = 'rail-link' + (i === 0 ? ' active' : '');
    a.href = '#' + item.id;
    a.setAttribute('data-rail', '');
    a.innerHTML = esc(item.label) + '<span class="n" data-rail-n="' + esc(item.id) + '"></span>';
    railInner.appendChild(a);
  });
  if (rail.length) {
    // scroll-spy over the rail anchors — the flat page's table of contents
    const links = [...railInner.querySelectorAll('[data-rail]')];
    const targets = links.map(a => document.querySelector(a.getAttribute('href'))).filter(Boolean);
    const spy = () => {
      let cur = 0;
      targets.forEach((t, i) => { if (t.getBoundingClientRect().top <= 110) cur = i; });
      links.forEach((a, i) => a.classList.toggle('active', i === cur));
    };
    window.addEventListener('scroll', spy, { passive: true });
    window.addEventListener('resize', spy);
    spy();
  }

  const footer = document.createElement('footer');
  footer.className = 'console-footer' + (full ? ' full' : '');
  footer.innerHTML = full
    ? `<div class="ft-inner full">
        <div id="ft-status"></div>
        <div><span id="ft-identity"></span> · <a href="https://github.com/bigfatsea/vmr" target="_blank" rel="noopener">bigfatsea/vmr
          <svg height="13" width="13" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"></path></svg></a></div>
      </div>`
    : `<div class="ft-inner">
        <div id="ft-identity"></div>
        <div><a href="https://github.com/bigfatsea/vmr" target="_blank" rel="noopener">bigfatsea/vmr
          <svg height="13" width="13" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"></path></svg></a></div>
      </div>`;
  document.body.appendChild(footer);

  syncKeyBtn();
  document.getElementById('hd-key').onclick = openKeyModal;
  document.getElementById('warn-bell').onclick = () => openOverlay(warnOverlay);
  ConsoleAlerts.set([]);   // quiet pill until the page feeds real alerts

  let countdownLeft = 300, countdownPaused = false, onRefresh = null;
  if (refresh === 'countdown') {
    const cdEl = document.getElementById('cd');
    const pill = document.getElementById('hd-refresh');
    // the WHOLE page refreshes together — live table included; a paused pill
    // must survive a manual refresh over fresh data (demo semantics)
    const paint = () => {
      cdEl.textContent = countdownPaused ? 'paused' : fmtMMSS(Math.max(0, countdownLeft));
      pill.classList.toggle('paused', countdownPaused);
    };
    setInterval(() => {
      if (countdownPaused || document.hidden) return;
      countdownLeft--;
      if (countdownLeft <= 0 && onRefresh) { countdownLeft = 300; paint(); onRefresh(); }
      else paint();
    }, 1000);
    pill.onclick = () => { if (onRefresh) { countdownLeft = 300; paint(); onRefresh(); } };
    paint();
  }

  return {
    // the page's data loader — countdown tick and click-to-refresh both land here
    onRefresh(fn) { onRefresh = fn; },
    refreshNow() {
      if (refresh === 'countdown') {
        countdownLeft = 300;
        const cdEl = document.getElementById('cd');
        if (cdEl) cdEl.textContent = fmtMMSS(300);
      }
      if (onRefresh) onRefresh();
    },
    // countdown paused state; document.hidden pauses the tick automatically
    setPaused(p) { countdownPaused = !!p; },
    setUptime(text) {
      const el = document.getElementById('hd-uptime');
      if (el) el.textContent = text;
    },
    setRailCount(id, n) {
      const el = railInner.querySelector('[data-rail-n="' + id + '"]');
      if (el) el.textContent = String(n);
    },
    // stream slot (Log): 'streaming' | 'connecting' | 'paused' | 'down'
    setStreamState(state) {
      const el = document.getElementById('hd-conn');
      if (!el) return;
      if (state === 'streaming') { el.className = 'pill p-live'; el.textContent = 'streaming'; el.title = 'Receiving the live log stream'; }
      else if (state === 'paused') { el.className = 'pill p-paused'; el.textContent = 'paused'; el.title = 'Rendering paused — the server keeps streaming'; }
      else if (state === 'connecting') { el.className = 'pill p-connecting'; el.textContent = 'connecting'; el.title = 'Connecting to the log stream'; }
      else { el.className = 'pill p-down'; el.textContent = 'down'; el.title = 'The log stream is disconnected'; }
    },
    setTermStatus(text) {
      const el = document.getElementById('ft-status');
      if (el) el.textContent = text;
    },
    // instance identity (version · pid · go · os-arch · listen) — footer only (§8.1)
    setFooterIdentity(text) {
      const el = document.getElementById('ft-identity');
      if (el) el.textContent = text;
    },
  };
}
