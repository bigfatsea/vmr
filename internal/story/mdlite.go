// Ver 2026-08-28, by Sonnet 5

// mdlite: a deliberately tiny Markdown-to-HTML converter for the one place
// that needs it — the LLM interpretation prose block in the comparison
// dashboard (render_compare_html.go). The LLM returns Markdown; the .md
// report pastes it verbatim, the .html report needs it as HTML. Scope is
// exactly what an interpretation answer uses: ATX headings, paragraphs,
// unordered lists, GFM pipe tables (the "candidate root cause | evidence |
// confidence | fix" table the -compare prompt asks for), **bold**, `inline
// code`. Everything is HTML-escaped first, so no input can inject markup.
// Not a CommonMark implementation and not meant to become one — anything
// more elaborate belongs in a real parser, not here.
package story

import (
	"html"
	"strings"
)

func mdToHTML(src string) string {
	var b strings.Builder
	lines := strings.Split(src, "\n")
	inList := false
	var para []string

	flushPara := func() {
		if len(para) == 0 {
			return
		}
		b.WriteString("<p>")
		b.WriteString(mdInline(strings.Join(para, " ")))
		b.WriteString("</p>\n")
		para = para[:0]
	}
	closeList := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
	}

	for i := 0; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		switch {
		case t == "":
			flushPara()
			closeList()
		case isTableStart(lines, i):
			flushPara()
			closeList()
			i = mdTable(&b, lines, i)
		case strings.HasPrefix(t, "#"):
			flushPara()
			closeList()
			n := len(t) - len(strings.TrimLeft(t, "#"))
			if n > 4 {
				n = 4
			}
			level := n + 2 // never emit <h1>/<h2>: those are the dashboard's own
			if level > 6 {
				level = 6
			}
			txt := strings.TrimSpace(strings.TrimLeft(t, "#"))
			b.WriteString("<h")
			b.WriteByte(byte('0' + level))
			b.WriteString(">")
			b.WriteString(mdInline(txt))
			b.WriteString("</h")
			b.WriteByte(byte('0' + level))
			b.WriteString(">\n")
		case strings.HasPrefix(t, "- "), strings.HasPrefix(t, "* "):
			flushPara()
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			b.WriteString("<li>")
			b.WriteString(mdInline(strings.TrimSpace(t[2:])))
			b.WriteString("</li>\n")
		default:
			closeList()
			para = append(para, t)
		}
	}
	flushPara()
	closeList()
	return b.String()
}

// isTableStart reports whether line i begins a GFM pipe table: a header row
// with a pipe, immediately followed by a delimiter row (dashes and pipes).
func isTableStart(lines []string, i int) bool {
	if i+1 >= len(lines) {
		return false
	}
	head := strings.TrimSpace(lines[i])
	delim := strings.TrimSpace(lines[i+1])
	if !strings.Contains(head, "|") || delim == "" {
		return false
	}
	for _, r := range delim {
		if r != '|' && r != '-' && r != ':' && r != ' ' {
			return false
		}
	}
	return strings.Contains(delim, "-")
}

// mdTable emits <table> for the run of pipe rows starting at line i (which
// isTableStart has verified) and returns the index of the last row consumed.
func mdTable(b *strings.Builder, lines []string, i int) int {
	cells := func(row string) []string {
		row = strings.TrimSpace(row)
		row = strings.TrimPrefix(row, "|")
		row = strings.TrimSuffix(row, "|")
		parts := strings.Split(row, "|")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		return parts
	}
	b.WriteString("<table class=\"mdlite\">\n<thead><tr>")
	for _, c := range cells(lines[i]) {
		b.WriteString("<th>" + mdInline(c) + "</th>")
	}
	b.WriteString("</tr></thead>\n<tbody>\n")
	j := i + 2
	for j < len(lines) && strings.Contains(lines[j], "|") && strings.TrimSpace(lines[j]) != "" {
		b.WriteString("<tr>")
		for _, c := range cells(lines[j]) {
			b.WriteString("<td>" + mdInline(c) + "</td>")
		}
		b.WriteString("</tr>\n")
		j++
	}
	b.WriteString("</tbody>\n</table>\n")
	return j - 1
}

// mdInline handles the inline span syntax on already-block-split text:
// escape everything, then re-introduce `code` and **bold**.
func mdInline(s string) string {
	s = html.EscapeString(s)
	s = mdWrap(s, "`", "<code>", "</code>")
	s = mdWrap(s, "**", "<strong>", "</strong>")
	return s
}

// mdWrap replaces balanced pairs of delim with open/close tags, leaving an
// unpaired trailing delim as a literal.
func mdWrap(s, delim, open, close string) string {
	var b strings.Builder
	parts := strings.Split(s, delim)
	for i, p := range parts {
		if i == 0 {
			b.WriteString(p)
			continue
		}
		if i%2 == 1 && i < len(parts)-1 {
			// even count so far and more to come: this delim opens a span
			b.WriteString(open)
			b.WriteString(p)
			b.WriteString(close)
		} else if i%2 == 1 {
			// unpaired opener at the end: keep the delim literal
			b.WriteString(delim)
			b.WriteString(p)
		} else {
			b.WriteString(p)
		}
	}
	return b.String()
}
