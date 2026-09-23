// Ver 2026-09-24 10:00, by agent
package dashboard

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TemplateViolation records an unescaped dynamic interpolation inside an HTML-rendering template.
type TemplateViolation struct {
	File    string
	Line    int
	Context string
	Expr    string
}

// knownSafeVars holds identifiers for variables known to hold safe HTML row fragments,
// safe badge HTML, or pre-escaped snippets constructed immediately before interpolation.
var knownSafeVars = map[string]bool{
	"modelRows":       true,
	"clientRows":      true,
	"quotaRows":       true,
	"epRows":          true,
	"dateRows":        true,
	"ceRows":          true,
	"compRows":        true,
	"argsHtml":        true,
	"resultHtml":      true,
	"costHtml":        true,
	"evidenceHtml":    true,
	"actionHtml":      true,
	"droppedEntities": true,
	"outcomeBadge":    true,
	"flags.join(' ')": true,
}

// isWhitelistedExpr checks whether expr meets escaping requirements:
// 1. Starts with esc(...)
// 2. Or is a numeric/currency/duration/percentage formatter
// 3. Or is a known safe SVG or HTML helper
// 4. Or is a known safe HTML fragment variable
// 5. Or is a pure numeric, length, or count expression.
func isWhitelistedExpr(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}

	// 1. Explicit esc(...) call:
	if strings.HasPrefix(expr, "esc(") && strings.HasSuffix(expr, ")") {
		return true
	}

	// 2. Numeric / format helper function calls:
	safeFuncPrefixes := []string{
		"FmtTokens(",
		"FmtBytes(",
		"FmtPercent(",
		"FmtCurrency(",
		"FmtCurrencyPrecise(",
		"FmtDuration(",
		"pct0(",
		"secs(",
	}
	for _, p := range safeFuncPrefixes {
		if strings.HasPrefix(expr, p) && strings.HasSuffix(expr, ")") {
			return true
		}
	}

	// 3. Safe methods on numbers or strings:
	if strings.HasSuffix(expr, ".toLocaleString()") ||
		strings.Contains(expr, ".toFixed(") ||
		strings.Contains(expr, ".padStart(") {
		return true
	}

	// 4. SVG chart helpers:
	safeSVGHelpers := []string{
		"svgBarChart(",
		"svgLineChart(",
		"svgHeatmap(",
		"svgLatencyPlot(",
		"sourceBar(",
	}
	for _, p := range safeSVGHelpers {
		if strings.HasPrefix(expr, p) {
			return true
		}
	}

	// 5. Known safe HTML snippet or row variables:
	if knownSafeVars[expr] {
		return true
	}
	// Also handle fallback rows: e.g. "modelRows || '<tr>...</tr>'"
	for v := range knownSafeVars {
		if strings.HasPrefix(expr, v+" ||") {
			return true
		}
	}

	// 6. Manifest / system constants:
	if expr == "EXPECTED_MANIFEST_FORMAT" {
		return true
	}

	// 7. Pure numeric literals:
	if _, err := strconv.ParseFloat(expr, 64); err == nil {
		return true
	}

	// 8. Safe numeric metrics and counts (identifiers representing counts, steps, or lengths):
	exprClean := strings.TrimSpace(expr)
	if strings.HasPrefix(exprClean, "(") && strings.HasSuffix(exprClean, ")") {
		exprClean = strings.TrimSpace(exprClean[1 : len(exprClean)-1])
	}
	numericFieldPattern := regexp.MustCompile(`^(?:[a-zA-Z0-9_(). ]+\.)?[a-zA-Z0-9_]*(?:requests|errors|failed|fallbacks|tokens|shipped|waste|length|count|seq|step|steps|earliest)(?:\s*\|\|\s*0)?$`)
	if numericFieldPattern.MatchString(exprClean) {
		return true
	}

	// Simple arithmetic or ratio expressions on counts:
	arithmeticPattern := regexp.MustCompile(`^[a-zA-Z0-9_(). ]+\s*[\/+\-*]\s*[a-zA-Z0-9_(). ]+$`)
	if arithmeticPattern.MatchString(exprClean) {
		return true
	}

	// Safe static class ternaries:
	ternaryClassPattern := regexp.MustCompile(`^[a-zA-Z0-9_(). =!><]+\s*\?\s*'[a-zA-Z0-9_\- ]*'\s*:\s*'[a-zA-Z0-9_\- ]*'$`)
	if ternaryClassPattern.MatchString(exprClean) {
		return true
	}

	return false
}

// isTargetTemplateContext checks whether the template literal occurs in a context
// that renders to innerHTML, .map(...) callback, out.push(...), or insertAdjacentHTML.
func isTargetTemplateContext(ctx string) bool {
	// Look back at the tokens in the preceding context:
	lower := strings.ToLower(ctx)
	if strings.Contains(lower, "innerhtml") ||
		strings.Contains(lower, "outerhtml") ||
		strings.Contains(lower, "insertadjacenthtml") ||
		strings.Contains(ctx, ".map(") ||
		strings.Contains(ctx, "out.push(") {
		return true
	}
	return false
}

// scanHTMLContent scans JavaScript template literals within an HTML/JS file
// and reports any unescaped dynamic interpolations.
func scanHTMLContent(filename string, content []byte) []TemplateViolation {
	var violations []TemplateViolation
	line := 1

	for i := 0; i < len(content); i++ {
		b := content[i]
		if b == '\n' {
			line++
		}
		if b == '`' {
			// Found opening backtick for a template literal.
			// Capture the preceding context (up to 120 chars).
			ctxStart := i - 120
			if ctxStart < 0 {
				ctxStart = 0
			}
			ctx := string(content[ctxStart:i])
			isTarget := isTargetTemplateContext(ctx)

			i++
			for i < len(content) {
				if content[i] == '\n' {
					line++
				}
				if content[i] == '\\' {
					i += 2
					continue
				}
				if content[i] == '`' {
					// End of template literal
					break
				}
				if content[i] == '$' && i+1 < len(content) && content[i+1] == '{' {
					exprLine := line
					i += 2
					exprStart := i
					braceDepth := 1

					for i < len(content) && braceDepth > 0 {
						if content[i] == '\n' {
							line++
						}
						if content[i] == '\\' {
							i += 2
							continue
						}
						if content[i] == '\'' || content[i] == '"' {
							q := content[i]
							i++
							for i < len(content) && content[i] != q {
								if content[i] == '\n' {
									line++
								}
								if content[i] == '\\' {
									i++
								}
								i++
							}
							if i < len(content) {
								i++
							}
							continue
						}
						if content[i] == '{' {
							braceDepth++
						} else if content[i] == '}' {
							braceDepth--
							if braceDepth == 0 {
								break
							}
						}
						i++
					}

					expr := strings.TrimSpace(string(content[exprStart:i]))
					if isTarget && !isWhitelistedExpr(expr) {
						violations = append(violations, TemplateViolation{
							File:    filename,
							Line:    exprLine,
							Context: strings.TrimSpace(ctx),
							Expr:    expr,
						})
					}
					continue
				}
				i++
			}
		}
	}
	return violations
}

// TestDashboardAssets_InnerHTMLAndMapEscaped scans all embedded skeleton dashboard HTML
// pages and verifies that every dynamic interpolation in innerHTML and .map() templates
// is properly escaped via esc(...) or belongs to a strict whitelist.
func TestDashboardAssets_InnerHTMLAndMapEscaped(t *testing.T) {
	pages := []string{
		"assets/macro-dashboard.html",
		"assets/journey-viewer.html",
		"assets/request-browser.html",
	}

	for _, p := range pages {
		data, err := assets.ReadFile(p)
		if err != nil {
			t.Fatalf("failed to read embedded asset %s: %v", p, err)
		}

		violations := scanHTMLContent(p, data)
		for _, v := range violations {
			t.Errorf("unescaped dynamic interpolation in %s: line %d: ${%s}", v.File, v.Line, v.Expr)
		}
	}
}

// TestDashboardAssets_GuardCatchesUnsafeFixture verifies that our scanner reliably
// detects unescaped interpolations using an intentionally vulnerable fixture.
func TestDashboardAssets_GuardCatchesUnsafeFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "unsafe_dashboard_fixture.html")
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read test fixture %s: %v", fixturePath, err)
	}

	violations := scanHTMLContent(fixturePath, data)
	if len(violations) < 2 {
		t.Fatalf("expected at least 2 violations in unsafe fixture, got %d: %+v", len(violations), violations)
	}

	foundUser := false
	foundModel := false
	for _, v := range violations {
		if v.Expr == "userInput" {
			foundUser = true
		}
		if v.Expr == "m.model" {
			foundModel = true
		}
	}

	if !foundUser {
		t.Errorf("guard failed to flag unescaped ${userInput} in innerHTML")
	}
	if !foundModel {
		t.Errorf("guard failed to flag unescaped ${m.model} in .map(...) template")
	}
}

// TestDashboardAssets_GuardCatchesBareModelMutation explicitly verifies the acceptance requirement:
// reverting any esc(m.model) to m.model must cause the guard to fail.
func TestDashboardAssets_GuardCatchesBareModelMutation(t *testing.T) {
	data, err := assets.ReadFile("assets/macro-dashboard.html")
	if err != nil {
		t.Fatalf("failed to read macro-dashboard.html: %v", err)
	}

	// Mutate: replace esc(m.model || 'unknown') with bare m.model
	mutated := strings.Replace(string(data), "${esc(m.model || 'unknown')}", "${m.model}", 1)
	if mutated == string(data) {
		t.Fatalf("mutation target ${esc(m.model || 'unknown')} not found in macro-dashboard.html")
	}

	violations := scanHTMLContent("assets/macro-dashboard.html", []byte(mutated))
	if len(violations) == 0 {
		t.Fatalf("guard failed to catch mutated bare ${m.model}")
	}

	foundBareModel := false
	for _, v := range violations {
		if v.Expr == "m.model" {
			foundBareModel = true
			break
		}
	}
	if !foundBareModel {
		t.Errorf("expected violation for m.model, but got: %+v", violations)
	}
}
