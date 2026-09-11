// Ver 2026-09-08, by pi (coding)

package journey

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ArtifactOp classifies the mutation applied to a workspace file or target.
type ArtifactOp string

const (
	ArtifactOpWrite  ArtifactOp = "write"  // full overwrite or creation
	ArtifactOpEdit   ArtifactOp = "edit"   // inline replacement or patch
	ArtifactOpDelete ArtifactOp = "delete" // file removal
	ArtifactOpBash   ArtifactOp = "bash"   // shell redirect or pipe mutation (heuristic)
)

// Artifact records a single workspace path touched by an agent's tool calls.
type Artifact struct {
	Path      string     `json:"path"`
	Op        ArtifactOp `json:"op"`
	FirstStep int        `json:"first_step"`
	Count     int        `json:"count"`
	Heuristic bool       `json:"heuristic,omitempty"`
}

var (
	// common bash write/mutation patterns: redirection (> or >>), sed -i, rm, etc.
	bashRedirectRe = regexp.MustCompile(`(?:>>?)\s*([a-zA-Z0-9_\-\./]+)`)
	bashTeeRe      = regexp.MustCompile(`\btee\s+(?:-[a-zA-Z]+\s+)*([a-zA-Z0-9_\-\./]+)`)
	bashRmRe       = regexp.MustCompile(`\brm\s+(?:-[a-zA-Z]+\s+)*([a-zA-Z0-9_\-\./]+)`)
	bashSedRe      = regexp.MustCompile(`\bsed\s+-i[a-zA-Z0-9_\-]*\s+.*?\s+([a-zA-Z0-9_\-\./]+)`)
	bashApplyRe    = regexp.MustCompile(`\b(?:patch|git\s+apply)\b.*?([a-zA-Z0-9_\-\./]+)`)
)

// knownFileExtensions gates a bare dotted token's plausibility as a real
// file path (see looksLikeFilePath) — deliberately not exhaustive, just the
// common shapes actually seen in coding-agent workspaces.
var knownFileExtensions = map[string]bool{
	"go": true, "py": true, "js": true, "ts": true, "tsx": true, "jsx": true,
	"json": true, "yaml": true, "yml": true, "md": true, "txt": true,
	"sh": true, "bash": true, "zsh": true, "toml": true, "ini": true,
	"cfg": true, "conf": true, "log": true, "csv": true, "html": true,
	"css": true, "scss": true, "sql": true, "xml": true, "env": true,
	"lock": true, "c": true, "h": true, "cpp": true, "hpp": true,
	"java": true, "rb": true, "rs": true, "php": true, "vue": true,
	"svelte": true, "proto": true, "pyc": true, "tmp": true, "bak": true,
	"out": true, "bin": true, "o": true, "exe": true,
}

// bareFileNameAllowlist covers real files that conventionally carry no
// extension — without this, looksLikeFilePath's dot/slash requirement would
// reject them outright.
var bareFileNameAllowlist = map[string]bool{
	"makefile": true, "dockerfile": true, "license": true, "readme": true,
	"procfile": true, "gemfile": true, "rakefile": true, "changelog": true,
}

// looksLikeFilePath filters a heuristic bash-redirect match down to tokens
// that plausibly name a real file. bashRedirectRe's loose grammar (needed to
// catch "> some/output.txt" inside an arbitrary shell one-liner) equally
// matches a ">" comparison operator sitting inside an inlined Python/JS
// snippet ("if x > 1", "val > document.getElementById(id).value") — those
// aren't redirects at all, just adjacent tokens the regex can't tell from
// one. Only applied to heuristic=true matches (record's structured-tool
// path, heuristic=false, comes from an actual path-shaped tool argument and
// is never second-guessed here).
func looksLikeFilePath(s string) bool {
	if s == "" || strings.ContainsAny(s, "()") {
		return false // a call expression fragment, never a bare path
	}
	if strings.Contains(s, "/") {
		return true // path-separator-qualified — a directory reference
	}
	if _, err := strconv.Atoi(s); err == nil {
		return false // pure integer literal (a comparison operand, not a path)
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 && i < len(s)-1 {
		if knownFileExtensions[strings.ToLower(s[i+1:])] {
			return true
		}
		// A dot with no recognized extension (document.getElementById,
		// obj.attr) is a property-access chain, not a file — reject rather
		// than fall through to the bare-name allowlist below.
		return false
	}
	return bareFileNameAllowlist[strings.ToLower(s)]
}

// ExtractArtifacts scans all steps of a Journey and extracts touched files.
func ExtractArtifacts(j *Journey) []Artifact {
	if j == nil {
		return nil
	}
	type entry struct {
		op        ArtifactOp
		firstStep int
		count     int
		heuristic bool
	}
	byPath := map[string]*entry{}

	record := func(rawPath string, op ArtifactOp, stepSeq int, heuristic bool) {
		clean := filepath.Clean(strings.TrimSpace(rawPath))
		if clean == "" || clean == "." || clean == "/" || strings.HasPrefix(clean, "/dev/") {
			return
		}
		if heuristic && !looksLikeFilePath(clean) {
			return
		}
		e, ok := byPath[clean]
		if !ok {
			byPath[clean] = &entry{op: op, firstStep: stepSeq, count: 1, heuristic: heuristic}
			return
		}
		e.count++
		if e.firstStep == 0 || stepSeq < e.firstStep {
			e.firstStep = stepSeq
		}
		// escalate op: write > edit > delete > bash
		if op == ArtifactOpWrite || (op == ArtifactOpEdit && e.op != ArtifactOpWrite) {
			e.op = op
		}
		if !heuristic {
			e.heuristic = false // one solid structured hit clears the heuristic flag
		}
	}

	for _, t := range j.Tasks {
		for _, s := range t.Steps {
			for _, tc := range s.ToolCalls {
				name := strings.ToLower(tc.Name)
				var args map[string]any
				if json.Unmarshal([]byte(tc.Args), &args) != nil {
					continue
				}

				// 1. Structured file tools: check path-like arguments
				if p, ok := firstStringField(args, deliverableFileKeys); ok && p != "" {
					op := classifyToolName(name)
					record(p, op, s.Seq, false)
					continue
				}

				// 2. Shell/bash commands: heuristic regex over command string
				if isShellTool(name) {
					cmdStr, ok := firstStringField(args, []string{"command", "cmd", "script", "input"})
					if ok && cmdStr != "" {
						extractBashMutations(cmdStr, s.Seq, record)
					}
				}
			}
		}
	}

	if len(byPath) == 0 {
		return nil
	}

	out := make([]Artifact, 0, len(byPath))
	for p, e := range byPath {
		out = append(out, Artifact{
			Path:      p,
			Op:        e.op,
			FirstStep: e.firstStep,
			Count:     e.count,
			Heuristic: e.heuristic,
		})
	}

	// Sort by FirstStep asc, then Path asc
	sort.Slice(out, func(i, j int) bool {
		if out[i].FirstStep != out[j].FirstStep {
			return out[i].FirstStep < out[j].FirstStep
		}
		return out[i].Path < out[j].Path
	})

	return out
}

func classifyToolName(name string) ArtifactOp {
	switch {
	case strings.Contains(name, "write") || strings.Contains(name, "create") || strings.Contains(name, "save"):
		return ArtifactOpWrite
	case strings.Contains(name, "edit") || strings.Contains(name, "replace") || strings.Contains(name, "patch") || strings.Contains(name, "update"):
		return ArtifactOpEdit
	case strings.Contains(name, "delete") || strings.Contains(name, "remove") || strings.Contains(name, "unlink"):
		return ArtifactOpDelete
	default:
		return ArtifactOpEdit
	}
}

func isShellTool(name string) bool {
	return strings.Contains(name, "bash") ||
		strings.Contains(name, "shell") ||
		strings.Contains(name, "exec") ||
		strings.Contains(name, "command") ||
		strings.Contains(name, "terminal")
}

func extractBashMutations(cmd string, seq int, record func(string, ArtifactOp, int, bool)) {
	// match redirects (e.g. > file, >> file)
	for _, m := range bashRedirectRe.FindAllStringSubmatch(cmd, -1) {
		if len(m) > 1 {
			record(m[1], ArtifactOpBash, seq, true)
		}
	}
	// match tee
	for _, m := range bashTeeRe.FindAllStringSubmatch(cmd, -1) {
		if len(m) > 1 {
			record(m[1], ArtifactOpBash, seq, true)
		}
	}
	// match rm
	for _, m := range bashRmRe.FindAllStringSubmatch(cmd, -1) {
		if len(m) > 1 {
			record(m[1], ArtifactOpDelete, seq, true)
		}
	}
	// match sed -i
	for _, m := range bashSedRe.FindAllStringSubmatch(cmd, -1) {
		if len(m) > 1 {
			record(m[1], ArtifactOpEdit, seq, true)
		}
	}
	// match patch / git apply
	for _, m := range bashApplyRe.FindAllStringSubmatch(cmd, -1) {
		if len(m) > 1 {
			record(m[1], ArtifactOpEdit, seq, true)
		}
	}
}
