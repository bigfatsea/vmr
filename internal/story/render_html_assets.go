// Ver 2026-08-28, by Sonnet 5

// The inline CSS and JS for render_html.go, kept in their own file so the
// renderer stays under archtest's per-file line budget and a stylesheet
// tweak never touches rendering logic. Self-contained on purpose: no font
// imports, no CDN, no external anything — an artifact you can open from a
// file:// URL or hand to someone with no network.
package story

const htmlStyle = `
:root {
  --bg: #ffffff; --fg: #1a1a1a; --muted: #6b7280; --line: #e5e7eb;
  --card: #fafafa; --accent: #2563eb; --warn-bg: #fef3c7; --warn-fg: #92400e;
  --redact-bg: #ede9fe; --redact-fg: #5b21b6; --code: #f3f4f6; --err: #b91c1c;
  --user: #2563eb; --tool: #059669; --sys: #7c3aed;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0f1115; --fg: #e6e6e6; --muted: #9ca3af; --line: #2a2f3a;
    --card: #161a22; --accent: #60a5fa; --warn-bg: #3b2f0b; --warn-fg: #fde68a;
    --redact-bg: #2a1f4d; --redact-fg: #c4b5fd; --code: #1c2028; --err: #f87171;
    --user: #60a5fa; --tool: #34d399; --sys: #a78bfa;
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

.jhead { max-width: 1100px; margin: 0 auto; padding: 28px 20px 8px; }
.jhead h1 { margin: 0 0 4px; font-size: 22px; }
.subtitle { color: var(--muted); margin: 0 0 10px; }
blockquote { margin: 0 0 12px; padding: 8px 14px; border-left: 3px solid var(--accent);
  background: var(--card); border-radius: 0 6px 6px 0; }
.banner { padding: 8px 12px; border-radius: 6px; margin: 8px 0; font-size: 14px; }
.banner.warn { background: var(--warn-bg); color: var(--warn-fg); }
.banner.redact { background: var(--redact-bg); color: var(--redact-fg); }

.layout { max-width: 1100px; margin: 0 auto; display: grid; grid-template-columns: 220px 1fr; gap: 24px; padding: 8px 20px 60px; }
.timeline { position: sticky; top: 12px; align-self: start; max-height: calc(100vh - 24px); overflow-y: auto; font-size: 13px; }
.timeline h3 { margin: 0 0 6px; font-size: 12px; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); }
.timeline ol { list-style: none; margin: 0; padding: 0; }
.timeline .tl-task > a { font-weight: 600; display: block; padding: 3px 0; }
.timeline .tl-task ol { padding-left: 10px; border-left: 1px solid var(--line); margin: 2px 0 6px; }
.timeline li a { display: block; padding: 2px 6px; border-radius: 4px; color: var(--muted); }
.timeline li a.active { background: var(--accent); color: #fff; }

.cards { min-width: 0; }
.task { margin-bottom: 22px; }
.task > h2 { font-size: 15px; border-bottom: 1px solid var(--line); padding-bottom: 4px; }
.task .tasktitle { font-weight: 400; color: var(--muted); }

.card { background: var(--card); border: 1px solid var(--line); border-radius: 8px; padding: 12px 14px; margin: 10px 0; scroll-margin-top: 12px; }
.cardhead { display: flex; flex-wrap: wrap; gap: 8px; align-items: baseline; font-size: 13px; color: var(--muted); margin-bottom: 4px; }
.cardhead .seq { font-weight: 700; color: var(--fg); }
.cardhead .model { color: var(--accent); }
.cardhead .endpoint { font-family: ui-monospace, monospace; font-size: 12px; }
.badge { padding: 1px 7px; border-radius: 999px; font-size: 12px; font-weight: 600; }
.badge.failover { background: var(--warn-bg); color: var(--warn-fg); }

.marker { font-size: 13px; padding: 6px 10px; border-radius: 6px; margin: 6px 0; border-left: 3px solid var(--muted); background: var(--code); }
.marker.stitch, .marker.compaction { border-left-color: var(--sys); }
.marker.sys { border-left-color: var(--sys); }
.marker.compaction .chdr { font-weight: 600; }
.entities { margin-top: 4px; font-size: 12px; }
.entities .lbl { color: var(--muted); }

.lbl { font-size: 11px; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); margin-right: 6px; }
.instruction { border-left: 3px solid var(--user); padding: 6px 10px; margin: 8px 0; background: var(--bg); border-radius: 0 6px 6px 0; }

details { margin: 6px 0; }
details > summary { cursor: pointer; font-size: 13px; color: var(--muted); user-select: none; }
details[open] > summary { margin-bottom: 4px; }
.msg { margin: 6px 0; padding-left: 10px; border-left: 2px solid var(--line); }
.msg .role { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; }
.msg.role-user .role { color: var(--user); }
.msg.role-tool .role, .msg.role-function .role { color: var(--tool); }
.msg.role-system .role { color: var(--sys); }

.response { margin-top: 10px; padding-top: 8px; border-top: 1px dashed var(--line); }
.resptext { margin-top: 4px; }
.tool { margin: 8px 0; padding-left: 10px; border-left: 2px solid var(--tool); }
.tool .toolname code { color: var(--tool); }
.toolresult.err > summary { color: var(--err); }
.noreply { color: var(--muted); font-style: italic; margin-top: 6px; }

.placeholder { color: var(--redact-fg); background: var(--redact-bg); padding: 1px 6px; border-radius: 4px; font-family: ui-monospace, monospace; font-size: 13px; }
.empty { color: var(--muted); font-style: italic; }
.gennote { max-width: 1100px; margin: 0 auto; padding: 0 20px 40px; color: var(--muted); font-size: 12px; }

@media (max-width: 800px) {
  .layout { grid-template-columns: 1fr; }
  .timeline { position: static; max-height: none; }
}
`

const htmlScript = `
(function () {
  var links = {};
  document.querySelectorAll('.timeline a[href^="#step-"]').forEach(function (a) {
    links[a.getAttribute('href').slice(1)] = a;
  });
  if (!('IntersectionObserver' in window)) return;
  var obs = new IntersectionObserver(function (entries) {
    entries.forEach(function (e) {
      var link = links[e.target.id];
      if (link) link.classList.toggle('active', e.isIntersecting);
    });
  }, { rootMargin: '-10% 0px -80% 0px' });
  document.querySelectorAll('article.card').forEach(function (c) { obs.observe(c); });
})();
`
