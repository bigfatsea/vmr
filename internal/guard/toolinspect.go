// Ver 2026-09-16, by Sonnet 5

// Tool-call inspection (M1.5; design spec's internal/guard module contract
// section). InspectToolCall
// judges one already-assembled (name, arguments) pair — assembly (SSE
// reassembly, JSON-escape decoding) is always the caller's job, never
// this package's (ADR-14 §4 / the package doc comment's dependency-
// whitelist rationale: guard imports jsonscan only, never chatmsg).
// Offline-only consumer since ADR-15 (report/guardscan.go, via
// chatmsg.ReassembleSSE for assembly) — the online Tool Call gate that
// used to call this during a live stream was removed; the judgment
// function itself was not. The patterns below are ported verbatim from
// tools/guard_corpus_scan's calibration run against this repo's full real
// audit corpus (0 false triggers on 20,078+ real records) rather than
// re-derived — porting exact, already-exercised patterns is the point:
// rewriting them here "cleaner" would silently un-calibrate them.
package guard

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"
)

// High-risk command patterns, design spec's high-risk command library. Two CWE corrections the
// spec itself calls out relative to earlier drafts: reverse_shell is
// CWE-506 (malicious code), not CWE-319 (cleartext transmission);
// pipe_to_shell is CWE-494 (download of code without integrity check).
//
// destructive_root_deletion's terminator group is `(/\*|/|~|\$HOME)(\s|"|$)`,
// not the spec's literal `(/|~|\$HOME)(\s|/\*|$)`: as written there, group 1
// greedily consumes the leading "/" of "/*", leaving group 2's `/\*`
// alternative nothing to match against — so the single most canonical
// destructive command, a bare "rm -rf /" with nothing after it, never
// matched at all once real tool-call arguments (always JSON-string-embedded,
// so the byte right after the trailing "/" is a closing quote, not
// whitespace or true end-of-string) are the input, and neither did the "/*"
// glob form even unquoted. Reordering the alternation to try "/\*" first
// and adding the JSON closing quote to the terminator set fixes both
// without narrowing anything the original pattern matched — the
// `rm\s+(-[a-zA-Z]*[rf][a-zA-Z]*\s+)+` prefix requirement is unchanged, so
// this can only add coverage, never a new false positive on a real deep
// path (e.g. "rm -rf /some/other/dir" still correctly doesn't match: the
// character after "some" is not a boundary this pattern accepts).
var highRiskPatterns = []struct {
	category string
	cwe      string
	re       *regexp.Regexp
}{
	{"destructive_root_deletion", "CWE-78", regexp.MustCompile(`(?i)rm\s+(-[a-zA-Z]*[rf][a-zA-Z]*\s+)+(/\*|/|~|\$HOME)(\s|"|$)`)},
	{"disk_destruction", "CWE-78", regexp.MustCompile(`(?i)(mkfs(\.[a-z0-9]+)?\s+/dev/|dd\s+[^\n]*of=/dev/(sd|nvme|vd|disk))`)},
	{"reverse_shell", "CWE-506", regexp.MustCompile(`(?i)(/dev/tcp/[\w.-]+/\d+|nc(\.traditional)?\s+(-[a-zA-Z]*e[a-zA-Z]*\s+)?/bin/(ba)?sh|mkfifo\s+\S+.*\|.*sh)`)},
	{"pipe_to_shell", "CWE-494", regexp.MustCompile(`(?i)(curl|wget)\s+[^\|;&]*\|\s*(sudo\s+)?(ba|z|k|da)?sh\b`)},
	{"base64_exec", "CWE-95", regexp.MustCompile(`(?i)(base64\s+(-d|--decode)[^\|]*\|\s*(ba)?sh|(python[\d.]*|perl|ruby)\s+-c\s+['"][^'"]*\b(exec|eval|pty\.spawn)\b)`)},
	{"credential_exfiltration", "CWE-312", regexp.MustCompile(`(?i)(curl|wget)[^\n]*(-d|-F|--data[\w-]*)\s*@?\S*(id_[rd]sa|id_ed25519|\.aws/credentials|\.env|\.npmrc|\.netrc)`)},
}

// decodeUnicodeEscapes returns args with every \uXXXX / UTF-16 surrogate-
// pair escape resolved to its literal UTF-8 bytes, leaving every other byte
// (JSON structural characters, and other string escapes like \" \\ \n)
// untouched. args is the tool call's argument JSON text itself -- a nested
// JSON document received as a plain string field (arguments/partial_json),
// so encoding/json's automatic unescaping of the OUTER SSE event only
// resolves the outer layer; a value the model or an untrusted relay spells
// as "rm" inside that inner document (a valid way to encode "rm")
// arrives here exactly as those six literal ASCII bytes, invisible to a
// regex that expects the literal command text. Only \uXXXX is resolved
// (never \n \" \\ etc.) because those already read as their real bytes in
// args and touching them risks disturbing the patterns' existing
// calibration; \u is the one form the corpus-calibrated patterns were never
// exercised against. Zero-allocation fast path when no "\u" appears.
func decodeUnicodeEscapes(args []byte) []byte {
	if !bytes.Contains(args, []byte(`\u`)) {
		return args
	}
	out := make([]byte, 0, len(args))
	ScanEscaped(args, func(r rune, rawSpan []byte, isEscape bool, ok bool) bool {
		if isEscape && ok {
			out = utf8.AppendRune(out, r)
		} else {
			out = append(out, rawSpan...)
		}
		return true
	})
	return out
}

// protectedPathPattern matches the design spec's protected-path table
// (the subset already exercised by the corpus scan — Windows paths,
// crontab, and Agent-config globs beyond ~/.claude/.mcp.json are Backlog
// item 1 of the Agent Guard spec §6.3, not
// silently expanded here without corpus calibration). It is applied ONLY
// to path-named argument values (pathArgKeys below), never to the raw
// argument bytes.
var protectedPathPattern = regexp.MustCompile(`(?i)(~/\.ssh/|/etc/|~/\.bashrc|~/\.zshrc|~/\.profile|\.mcp\.json|~/\.claude/)`)

// pathArgKeys are the argument-key names (matched case-insensitively)
// whose JSON string values are treated as a file_write tool call's actual
// write targets. Protected-path matching runs ONLY against these values,
// never the raw argument bytes: a write tool whose document body merely
// MENTIONS "~/.ssh/" or "/etc/" in its content is not a persistence
// attempt, and the whole-string scan this replaced flagged exactly that.
// Keys outside this set are never scanned
// as paths — a tool shape this misses is a false negative, never a new
// false positive, and a fallback to whole-string matching would be
// precisely the bug being fixed.
//
// Both the snake_case and camelCase spelling of every multi-word key are
// listed explicitly: the lookup lowercases k first (case-insensitive
// match), which collapses "targetPath" to "targetpath" — a plain string
// that matches neither spelling unless both are present as separate map
// entries. Listing camelCase's lowercased form alongside snake_case is
// the fix, not a smarter lookup: JS/TS-authored tool schemas (a common
// Agent-tooling convention) name these keys targetPath/outputPath/
// newPath/oldPath/sourcePath/etc., not target_path/output_path.
var pathArgKeys = map[string]bool{
	"path": true, "file_path": true, "filepath": true, "file": true,
	"filename": true, "pathname": true,
	"new_path": true, "newpath": true, "old_path": true, "oldpath": true,
	"target": true, "target_path": true, "targetpath": true,
	"target_file": true, "targetfile": true,
	"destination": true, "dest": true, "destination_path": true, "destinationpath": true,
	"output": true, "output_path": true, "outputpath": true,
	"output_file": true, "outputfile": true,
	"dir": true, "directory": true,
	"src": true, "source": true, "source_path": true, "sourcepath": true,
}

// maxPathWalkDepth bounds the recursive descent into nested argument
// objects/arrays — real tool arguments carry paths at the top level or
// one wrapper object down; anything deeper is not a shape pathArgKeys'
// audience uses.
const maxPathWalkDepth = 4

// protectedPathHit runs protectedPathPattern over exactly the path-named
// string values inside args (keys in pathArgKeys, at any nesting depth up
// to maxPathWalkDepth) and returns the first match, or "". Args that do
// not parse as JSON yield "" -- the only caller left (report/guardscan.go,
// offline) always hands this a fully-assembled tool-call argument string,
// so a parse failure here means genuinely malformed input, never an
// in-flight fragment: the online Tool Call gate that used to see partial,
// still-arriving arguments was removed by ADR-15, and with it the
// fragment-vs-complete distinction this function once had to make. Values
// go through encoding/json unescaping before the regex, the same
// "everything is scanned after JSON unescaping" rule as §4.4.2.
func protectedPathHit(args []byte) string {
	var hit string
	var walk func(raw []byte, depth int) bool
	walk = func(raw []byte, depth int) bool {
		if depth > maxPathWalkDepth {
			return false
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			return false
		}
		if trimmed[0] == '{' {
			var obj map[string]json.RawMessage
			if json.Unmarshal(trimmed, &obj) != nil {
				return false
			}
			for k, v := range obj {
				if pathArgKeys[strings.ToLower(k)] {
					var s string
					if json.Unmarshal(v, &s) == nil && s != "" {
						if m := protectedPathPattern.FindString(s); m != "" {
							hit = m
							return true
						}
					}
				}
				if walk(v, depth+1) {
					return true
				}
			}
			return false
		}
		if trimmed[0] == '[' {
			var arr []json.RawMessage
			if json.Unmarshal(trimmed, &arr) != nil {
				return false
			}
			for _, v := range arr {
				if walk(v, depth+1) {
					return true
				}
			}
		}
		return false
	}
	walk(args, 0)
	return hit
}

// writeToolNamePattern flags a tool name as file_write-shaped (the design
// spec's name-pattern table) — checked only to decide whether protected-path
// matching applies, never used for command-category matching.
var writeToolNameFragments = []string{"write", "create", "edit", "replace", "patch"}

// ToolVerdict is InspectToolCall's judgment on one tool call.
type ToolVerdict struct {
	// Category is the tool's name-pattern class (the design spec's
	// name-pattern table): "command" runs the high-risk command library,
	// "file_write" runs protected-path
	// matching, "network"/"unknown" run neither — both still get the
	// credential-echo check, which applies regardless of category (K-G6:
	// an unclassified tool never runs command pattern matching, but
	// credential exfiltration via an unrecognized tool is still worth
	// flagging).
	Category string
	// Hit is the matched risk category name -- either a command-library
	// entry (e.g. "pipe_to_shell") or, when PathHit is set,
	// "persistence_write_protected_path" -- or "" when nothing matched.
	// A caller grouping findings by risk category needs only this field;
	// PathHit carries the specific matched path text.
	Hit string
	CWE string
	// PathHit is the matched protected-path fragment for a file_write-
	// category tool, or "" when none matched (or the category isn't
	// file_write). Non-empty PathHit always implies Hit ==
	// "persistence_write_protected_path".
	PathHit string
	// Echoed is true when args contains one of the known secret values
	// passed in (ADR-6's decryption-oracle signal: a credential the
	// request sent reappearing inside a tool-call argument is zero-
	// baseline in real traffic — see §2.3 结论 6 — so any true here is
	// worth surfacing regardless of Category).
	Echoed bool
	// Excerpt is a short, already-non-secret excerpt of the matched
	// command/path text (never the credential itself — Echoed carries
	// that signal as a bool, not a copy of the secret).
	Excerpt string
}

// ClassifyToolName maps a tool name to the design spec's category via name-pattern
// matching. Matching is substring-based and case-insensitive, mirroring
// the calibration tool's approach — real tool names in this repo's corpus
// are things like "bash"/"terminal"/"write_file"/"str_replace_editor", not
// adversarially crafted, so a substring match is sufficient without being
// a source of false negatives worth a full pattern-language.
func ClassifyToolName(name string) string {
	lower := strings.ToLower(name)
	for _, frag := range []string{"bash", "shell", "terminal", "cmd", "powershell", "run_command", "execute", "_exec"} {
		if strings.Contains(lower, frag) {
			return "command"
		}
	}
	if lower == "sh" || strings.HasPrefix(lower, "sh_") || strings.HasSuffix(lower, "_sh") || strings.Contains(lower, "_sh_") ||
		strings.HasPrefix(lower, "sh-") || strings.HasSuffix(lower, "-sh") || strings.Contains(lower, "-sh-") {
		return "command"
	}
	for _, frag := range writeToolNameFragments {
		if strings.Contains(lower, frag) {
			return "file_write"
		}
	}
	for _, frag := range []string{"fetch", "http", "curl", "web_"} {
		if strings.Contains(lower, frag) {
			return "network"
		}
	}
	return "unknown"
}

// InspectToolCall judges one already-decoded (name, arguments) tool call.
// known is the set of credential values Scan already found in this same
// request's outbound body (a per-request egress cache, per the design
// spec's ADR-6) — pass nil when no outbound scan ran or nothing hit.
func InspectToolCall(name string, args []byte, known [][]byte) ToolVerdict {
	v := ToolVerdict{Category: ClassifyToolName(name)}
	// Decoded once and reused below for both the command-pattern scan and
	// the credential-echo check: a relay that \u-escapes an echoed
	// credential (a legal way to encode any JSON string) would otherwise
	// defeat a raw-byte bytes.Contains the same way an escaped "rm" used to
	// defeat the command patterns before decodeUnicodeEscapes existed.
	decoded := decodeUnicodeEscapes(args)

	if v.Category == "command" {
		argsStr := string(decoded)
		for _, p := range highRiskPatterns {
			if m := p.re.FindString(argsStr); m != "" {
				v.Hit, v.CWE, v.Excerpt = p.category, p.cwe, trimExcerpt(m, excerptMaxBytes)
				break
			}
		}
	}
	if v.Category == "file_write" {
		if m := protectedPathHit(args); m != "" {
			v.PathHit = trimExcerpt(m, excerptMaxBytes)
			// Hit/CWE are also set here (not left to PathHit alone) so a
			// caller grouping findings by risk category doesn't need a
			// second notion of "category" beyond Hit/CWE — persistence via
			// a protected-path write is CWE-269 (the design spec's own mapping),
			// distinct from the command-pattern library's categories.
			v.Hit, v.CWE = "persistence_write_protected_path", "CWE-269"
		}
	}
	for _, secret := range known {
		if len(secret) > 0 && bytes.Contains(decoded, secret) {
			v.Echoed = true
			break
		}
	}
	return v
}

// excerptMaxBytes is Excerpt's byte cap, design spec §4.2/§4.7.
const excerptMaxBytes = 256

// trimExcerpt collapses newlines and truncates s to at most maxLen bytes —
// shared by the high-risk command excerpt and the protected-path excerpt,
// both of which are matched substrings of the tool's OWN arguments, never
// a secret value. The cut backs off to the nearest UTF-8 rune boundary at
// or before maxLen: a plain s[:maxLen] can land mid-rune on non-ASCII
// text (CJK, accented punctuation) and hand the caller invalid UTF-8.
func trimExcerpt(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
