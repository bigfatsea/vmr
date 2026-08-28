// Ver 2026-08-28, by Sonnet 5

package story

import (
	"strings"
	"testing"
)

func TestMdToHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain paragraph", "<p>plain paragraph</p>"},
		{"# Heading", "<h3>Heading</h3>"},
		{"### deep", "<h5>deep</h5>"},
		{"- a\n- b", "<ul>\n<li>a</li>\n<li>b</li>\n</ul>"},
		{"**bold** here", "<p><strong>bold</strong> here</p>"},
		{"call `foo()` now", "<p>call <code>foo()</code> now</p>"},
		{"line one\nline two", "<p>line one line two</p>"},
		{"para1\n\npara2", "<p>para1</p>\n<p>para2</p>"},
		{"| a | b |\n|---|---|\n| 1 | 2 |", "<table class=\"mdlite\">"},
		{"| a | b |\n|---|---|\n| 1 | 2 |", "<td>1</td><td>2</td>"},
	}
	for _, c := range cases {
		if got := mdToHTML(c.in); !strings.Contains(got, c.want) {
			t.Errorf("mdToHTML(%q) = %q, want to contain %q", c.in, got, c.want)
		}
	}
}

func TestMdToHTML_EscapesInjection(t *testing.T) {
	got := mdToHTML("a <script>alert(1)</script> and & < >")
	if strings.Contains(got, "<script>") {
		t.Errorf("mdToHTML did not escape a script tag: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("mdToHTML output not escaped: %q", got)
	}
}

func TestMdToHTML_UnbalancedDelimStaysLiteral(t *testing.T) {
	got := mdToHTML("an **unclosed bold")
	if strings.Contains(got, "<strong>") {
		t.Errorf("unbalanced ** should stay literal: %q", got)
	}
}
