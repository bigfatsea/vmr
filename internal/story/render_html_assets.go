// Ver 2026-08-29, by Sonnet 5

// Inline CSS and JS for the journey incident report (render_html.go +
// render_html_dashboard.go) and, appended after compareExtraStyle, the
// comparison tale-of-the-tape (render_compare_html.go). One visual system —
// "VMR Forensics": a flight-data-recorder readout in dark mode, a printed
// incident report on engineering graph paper in light mode; mono-dominant
// technical typesetting throughout, sans reserved for the finding prose the
// reader actually has to read. Self-contained on purpose: no font imports,
// no CDN, no external anything — an artifact you can open from a file:// URL
// or hand to someone with no network.
package story

const htmlStyle = `
:root {
  --bg: #eef0f2; --panel: #ffffff; --ink: #1a1f28; --ink-dim: #5c6675;
  --rule: #d4d9e0; --amber: #b8730a; --alert: #c62828; --go: #1a7a5e;
  --trace: #2f6fd0; --code: #eceef1;
  --mono: ui-monospace, "SF Mono", "JetBrains Mono", "Cascadia Code", Menlo, Consolas, "Liberation Mono", monospace;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0a0e14; --panel: #121821; --ink: #d8dee9; --ink-dim: #7d8899;
    --rule: #232c3a; --amber: #ffb000; --alert: #ff5c5c; --go: #35d0a5;
    --trace: #5b9dff; --code: #1a2030;
  }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--ink);
  font: 14px/1.6 var(--mono); -webkit-font-smoothing: antialiased;
  font-variant-numeric: tabular-nums; }
a { color: var(--trace); text-decoration: none; }
a:hover { text-decoration: underline; }
h1, h2, h3, h4 { line-height: 1.25; font-weight: 700; }
code { background: var(--code); padding: 1px 5px; border-radius: 3px; font-size: .92em;
  font-family: var(--mono); word-break: break-all; }
pre { background: var(--code); padding: 10px 12px; border-radius: 4px; overflow-x: auto;
  white-space: pre-wrap; word-break: break-word; margin: 6px 0 0;
  font-family: var(--mono); font-size: 12.5px; }

.wrap { max-width: 1120px; margin: 0 auto; padding: 0 20px 60px; }

/* Recorder bar */
.recbar { display: flex; justify-content: space-between; align-items: center; gap: 12px;
  padding: 10px 0; margin-bottom: 22px; border-bottom: 1px solid var(--rule);
  font-size: 11px; letter-spacing: .18em; text-transform: uppercase; color: var(--ink-dim); }
.recbar .jid { color: var(--ink); letter-spacing: .04em; text-transform: none; word-break: break-all; }

/* Verdict panel — the signature element */
.jhead { margin-bottom: 30px; }
.verdict { border: 1px solid var(--rule); border-left: 5px solid var(--amber);
  background: var(--panel); border-radius: 4px; padding: 16px 18px 15px; }
.verdict.v-critical { border-left-color: var(--alert); }
.verdict.v-clean { border-left-color: var(--go); }
.vtop { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.vlabel { font-size: 11px; letter-spacing: .2em; text-transform: uppercase; color: var(--ink-dim);
  padding-top: 5px; }
.vstamp { font-weight: 700; font-size: 13px; letter-spacing: .12em; padding: 3px 10px;
  border: 2px solid var(--amber); color: var(--amber); border-radius: 3px;
  transform: rotate(-2.5deg); white-space: nowrap; }
.v-critical .vstamp { border-color: var(--alert); color: var(--alert); }
.v-clean .vstamp { border-color: var(--go); color: var(--go); }
.vcause { font-family: var(--sans); font-size: 16px; line-height: 1.5; color: var(--ink);
  margin-top: 8px; }
.vcause .vclean { color: var(--ink-dim); }
.damage { margin-top: 12px; padding-top: 10px; border-top: 1px solid var(--rule);
  font-size: 12.5px; color: var(--ink-dim); letter-spacing: .01em; }

.jhead h1 { font-size: 19px; font-family: var(--sans); margin: 20px 0 4px; word-break: break-word; }
.subtitle { color: var(--ink-dim); margin: 0 0 12px; font-size: 12px; letter-spacing: .04em; }
blockquote { margin: 0 0 10px; padding: 8px 14px; border-left: 3px solid var(--trace);
  background: var(--panel); font-family: var(--sans); }
.banner { padding: 8px 12px; border-radius: 4px; margin: 8px 0; font-size: 12.5px;
  font-family: var(--sans); }
.banner.warn { border: 1px solid var(--amber); color: var(--amber); background: transparent; }
.banner.redact { border: 1px dashed var(--ink-dim); color: var(--ink-dim); background: transparent; }

/* Point of no return */
.ponr { margin: 14px 0 4px; border: 1px solid var(--rule); border-left: 4px solid var(--alert);
  background: var(--panel); border-radius: 4px; padding: 11px 14px; }
.ponr-h { font-size: 12px; letter-spacing: .12em; text-transform: uppercase; color: var(--alert);
  font-weight: 700; }
.ponr-d { font-family: var(--sans); font-size: 13.5px; color: var(--ink); margin-top: 4px; }
.ponr.ponr-none { border-left-color: var(--go); padding: 8px 14px; }
.ponr.ponr-diffuse { border-left-color: var(--amber); padding: 8px 14px; }

.outcome { margin: 14px 0 2px; font-size: 13px; }
.outcome .lbl { display: inline-block; min-width: 64px; color: var(--ink-dim);
  font-size: 10px; text-transform: uppercase; letter-spacing: .12em; }

/* Layout + rail */
.layout { display: grid; grid-template-columns: 176px 1fr; gap: 30px; padding: 8px 0 0; }
.railwrap { position: sticky; top: 12px; align-self: start; max-height: calc(100vh - 24px); overflow-y: auto; }
nav.rail { font-size: 12px; }
nav.rail ol { list-style: none; margin: 0; padding: 0; }
nav.rail > ol > li > a { display: block; font-weight: 700; padding: 4px 8px; border-radius: 4px;
  color: var(--ink); text-transform: uppercase; letter-spacing: .08em; font-size: 11px; }
nav.rail .steps { padding-left: 8px; border-left: 1px solid var(--rule); margin: 2px 0 10px; }
nav.rail .steps a { display: block; padding: 2px 8px; border-radius: 3px; color: var(--ink-dim); font-size: 11px; }
nav.rail a.active { background: var(--trace); color: #fff; }
main.body { min-width: 0; }
section.block { margin-bottom: 36px; scroll-margin-top: 14px; }
section.block > h2 { font-size: 12px; letter-spacing: .16em; text-transform: uppercase;
  border-bottom: 1px solid var(--rule); padding-bottom: 7px; margin: 0 0 16px; color: var(--ink-dim); }

/* Sequence of events */
.task { margin-bottom: 18px; scroll-margin-top: 14px; }
.task > h3 { font-size: 11px; color: var(--ink-dim); text-transform: uppercase; letter-spacing: .1em; margin: 14px 0 6px; }
.task > h3 .tt { text-transform: none; color: var(--ink); font-weight: 400; letter-spacing: 0; font-family: var(--sans); }
.srow { border: 1px solid var(--rule); border-left: 3px solid var(--rule); border-radius: 5px;
  padding: 8px 12px; margin: 6px 0; background: var(--panel); scroll-margin-top: 14px; }
.srow.flagged { border-left-color: var(--amber); }
.srow .top { display: flex; flex-wrap: wrap; gap: 5px 10px; align-items: baseline; font-size: 12px; }
.srow .seq { font-weight: 700; color: var(--trace); }
.srow .ts { color: var(--ink-dim); font-size: 11px; }
.srow .model { color: var(--trace); font-size: 11px; }
.chip { display: inline-block; padding: 1px 7px; border-radius: 3px; font-size: 11px;
  background: var(--code); border: 1px solid var(--rule); }
.chip.tool { color: var(--go); }
.chip.badge { border-color: var(--amber); color: var(--amber); background: transparent; font-weight: 700; }
.chip.flag { background: transparent; border-color: var(--amber); color: var(--amber); }
.srow .markers { margin-top: 5px; display: flex; flex-wrap: wrap; gap: 4px 8px; font-size: 11px; color: var(--ink-dim); }
.srow .marker { padding: 1px 7px; border-radius: 3px; background: var(--code); border-left: 2px solid var(--ink-dim); }
.srow details { margin-top: 6px; }
.srow summary { cursor: pointer; color: var(--ink-dim); font-size: 11px; }
.srow .said { font-size: 12.5px; font-family: var(--sans); }
.coord { font-family: var(--mono); font-size: 11px; color: var(--ink-dim); }

/* Recorder parameters (metrics) */
.stats { display: grid; grid-template-columns: repeat(auto-fill, minmax(148px, 1fr)); gap: 9px; }
.stat { border: 1px solid var(--rule); border-radius: 5px; padding: 9px 11px; background: var(--panel); }
.stat .k { font-size: 10px; color: var(--ink-dim); text-transform: uppercase; letter-spacing: .08em; }
.stat .v { font-size: 19px; font-weight: 700; margin-top: 3px; letter-spacing: -.01em; }
.spark { margin-top: 18px; }
.spark h4 { margin: 0 0 5px; font-size: 10px; color: var(--ink-dim); text-transform: uppercase; letter-spacing: .1em; }
.spark svg { display: block; width: 100%; height: 60px; color: var(--trace); }
.spark .cap { font-size: 11px; color: var(--ink-dim); margin-top: 3px; }

/* Findings */
.finding { border: 1px solid var(--rule); border-left: 3px solid var(--amber); border-radius: 5px;
  padding: 10px 12px; margin: 8px 0; background: var(--panel); }
.finding .fh { display: flex; flex-wrap: wrap; gap: 6px 10px; align-items: baseline; font-size: 12px; }
.finding .code { font-family: var(--mono); font-weight: 700; }
.finding .src { font-size: 10px; text-transform: uppercase; letter-spacing: .08em; color: var(--ink-dim); }
.finding .txt { margin-top: 6px; font-size: 13.5px; font-family: var(--sans); line-height: 1.5; }
.finding .sub { margin-top: 4px; font-size: 12.5px; color: var(--ink-dim); font-family: var(--sans); }
.finding .sub .l { text-transform: uppercase; font-size: 10px; letter-spacing: .08em; margin-right: 5px; font-family: var(--mono); }

.placeholder { color: var(--ink-dim); background: var(--code); padding: 1px 6px; border-radius: 3px;
  font-family: var(--mono); font-size: 12.5px; }
.empty { color: var(--ink-dim); font-style: italic; }
.gennote { color: var(--ink-dim); font-size: 11px; letter-spacing: .04em; padding: 24px 0 40px; }

@media (max-width: 820px) {
  .layout { grid-template-columns: 1fr; }
  .railwrap { position: static; max-height: none; }
  .vtop { flex-direction: column-reverse; align-items: flex-start; }
}
@media (prefers-reduced-motion: reduce) {
  .vstamp { transform: none; }
}
`

const htmlScript = `
(function () {
  if (!('IntersectionObserver' in window)) return;
  var links = {};
  document.querySelectorAll('nav.rail a[href^="#"]').forEach(function (a) {
    links[a.getAttribute('href').slice(1)] = a;
  });
  var obs = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      var link = links[e.target.id];
      if (link) link.classList.toggle('active', e.isIntersecting);
    });
  }, { rootMargin: '-8% 0px -82% 0px' });
  document.querySelectorAll('section.block, .task[id], .srow[id]').forEach(function (el) { obs.observe(el); });
})();
`
