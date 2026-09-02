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

import (
	_ "embed"
)

//go:embed assets/style.css
var htmlStyle string

//go:embed assets/dashboard.js
var htmlScript string
