// Ver 2026-08-28, by Sonnet 5

// Inline CSS and JS for the journey dashboard (render_html.go +
// render_html_dashboard.go), kept in their own file so the renderer stays
// under archtest's per-file line budget and a stylesheet tweak never
// touches rendering logic. Self-contained on purpose: no font imports, no
// CDN, no external anything — an artifact you can open from a file:// URL
// or hand to someone with no network.
package story

const htmlStyle = `
:root {
  --bg: #ffffff; --fg: #1a1a1a; --muted: #6b7280; --line: #e5e7eb;
  --card: #f7f8fa; --accent: #2563eb; --warn-bg: #fef3c7; --warn-fg: #92400e;
  --redact-bg: #ede9fe; --redact-fg: #5b21b6; --code: #f0f1f4; --err: #b91c1c;
  --ok: #059669; --flag: #d97706;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0f1115; --fg: #e6e6e6; --muted: #9ca3af; --line: #2a2f3a;
    --card: #161a22; --accent: #60a5fa; --warn-bg: #3b2f0b; --warn-fg: #fde68a;
    --redact-bg: #2a1f4d; --redact-fg: #c4b5fd; --code: #1c2028; --err: #f87171;
    --ok: #34d399; --flag: #fbbf24;
  }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg);
  font: 15px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; }
a { color: var(--accent); text-decoration: none; }
a:hover { text-decoration: underline; }
code { background: var(--code); padding: 1px 5px; border-radius: 4px; font-size: .9em;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; word-break: break-all; }
pre { background: var(--code); padding: 10px 12px; border-radius: 6px; overflow-x: auto;
  white-space: pre-wrap; word-break: break-word; margin: 6px 0 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 13px; }
h1, h2, h3 { line-height: 1.25; }

.wrap { max-width: 1180px; margin: 0 auto; padding: 0 20px; }
.jhead { padding: 30px 0 6px; }
.jhead h1 { margin: 0 0 4px; font-size: 21px; word-break: break-all; }
.subtitle { color: var(--muted); margin: 0 0 12px; font-size: 14px; }
blockquote { margin: 0 0 10px; padding: 8px 14px; border-left: 3px solid var(--accent);
  background: var(--card); border-radius: 0 6px 6px 0; }
.banner { padding: 8px 12px; border-radius: 6px; margin: 8px 0; font-size: 14px; }
.banner.warn { background: var(--warn-bg); color: var(--warn-fg); }
.banner.redact { background: var(--redact-bg); color: var(--redact-fg); }
.outcome { margin: 10px 0 4px; font-size: 15px; }
.outcome .lbl { display: inline-block; min-width: 64px; color: var(--muted);
  font-size: 11px; text-transform: uppercase; letter-spacing: .05em; }

.layout { display: grid; grid-template-columns: 180px 1fr; gap: 28px; padding: 14px 0 70px; }
.railwrap { position: sticky; top: 12px; align-self: start; max-height: calc(100vh - 24px); overflow-y: auto; }
nav.rail { font-size: 13px; }
nav.rail ol { list-style: none; margin: 0; padding: 0; }
nav.rail > ol > li > a { display: block; font-weight: 600; padding: 4px 8px; border-radius: 5px; color: var(--fg); }
nav.rail .steps { padding-left: 8px; border-left: 1px solid var(--line); margin: 2px 0 8px; }
nav.rail .steps a { display: block; padding: 2px 8px; border-radius: 4px; color: var(--muted); font-size: 12px; }
nav.rail a.active { background: var(--accent); color: #fff; }
main.body { min-width: 0; }
section.block { margin-bottom: 34px; scroll-margin-top: 14px; }
section.block > h2 { font-size: 16px; border-bottom: 2px solid var(--line); padding-bottom: 6px; margin: 0 0 14px; }

/* Structure timeline */
.task { margin-bottom: 18px; }
.task > h3 { font-size: 13px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; margin: 14px 0 6px; }
.task > h3 .tt { text-transform: none; color: var(--fg); font-weight: 400; letter-spacing: 0; }
.srow { border: 1px solid var(--line); border-left: 3px solid var(--line); border-radius: 7px;
  padding: 8px 12px; margin: 6px 0; background: var(--card); scroll-margin-top: 14px; }
.srow.flagged { border-left-color: var(--flag); }
.srow .top { display: flex; flex-wrap: wrap; gap: 6px 10px; align-items: baseline; font-size: 13px; }
.srow .seq { font-weight: 700; }
.srow .ts { color: var(--muted); font-size: 12px; }
.srow .model { color: var(--accent); font-size: 12px; }
.chip { display: inline-block; padding: 1px 7px; border-radius: 999px; font-size: 12px;
  background: var(--code); border: 1px solid var(--line); }
.chip.tool { color: var(--ok); }
.chip.badge { background: var(--warn-bg); color: var(--warn-fg); border-color: transparent; font-weight: 600; }
.chip.flag { background: transparent; border-color: var(--flag); color: var(--flag); }
.srow .markers { margin-top: 5px; display: flex; flex-wrap: wrap; gap: 4px 8px; font-size: 12px; color: var(--muted); }
.srow .marker { padding: 1px 7px; border-radius: 5px; background: var(--code); border-left: 2px solid var(--muted); }
.srow details { margin-top: 6px; }
.srow summary { cursor: pointer; color: var(--muted); font-size: 12px; }
.srow .said { font-size: 13px; }
.coord { font-family: ui-monospace, monospace; font-size: 12px; color: var(--muted); }

/* Metrics */
.stats { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 10px; }
.stat { border: 1px solid var(--line); border-radius: 7px; padding: 9px 11px; background: var(--card); }
.stat .k { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; }
.stat .v { font-size: 18px; font-weight: 700; margin-top: 2px; }
.spark { margin-top: 16px; }
.spark h4 { margin: 0 0 4px; font-size: 12px; color: var(--muted); text-transform: uppercase; letter-spacing: .04em; }
.spark svg { display: block; width: 100%; height: 64px; color: var(--accent); }
.spark .cap { font-size: 12px; color: var(--muted); margin-top: 2px; }

/* Findings */
.finding { border: 1px solid var(--line); border-left: 3px solid var(--flag); border-radius: 7px;
  padding: 10px 12px; margin: 8px 0; background: var(--card); }
.finding .fh { display: flex; flex-wrap: wrap; gap: 6px 10px; align-items: baseline; font-size: 13px; }
.finding .code { font-family: ui-monospace, monospace; font-weight: 700; }
.finding .src { font-size: 11px; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); }
.finding .txt { margin-top: 5px; font-size: 14px; }
.finding .sub { margin-top: 4px; font-size: 13px; color: var(--muted); }
.finding .sub .l { text-transform: uppercase; font-size: 10px; letter-spacing: .05em; margin-right: 5px; }

.placeholder { color: var(--redact-fg); background: var(--redact-bg); padding: 1px 6px; border-radius: 4px;
  font-family: ui-monospace, monospace; font-size: 13px; }
.empty { color: var(--muted); font-style: italic; }
.gennote { color: var(--muted); font-size: 12px; padding: 0 0 40px; }

@media (max-width: 820px) {
  .layout { grid-template-columns: 1fr; }
  .railwrap { position: static; max-height: none; }
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
  document.querySelectorAll('section.block, .srow[id]').forEach(function (el) { obs.observe(el); });
})();
`
