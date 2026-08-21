// Ver 2026-08-20 00:00, by Sonnet 5

// Shared evidence blobs: content that many detail pages would otherwise
// repeat verbatim (a session's system prompt, a request's declared tool
// set) written once, content-addressed, under evidenceDir — every detail
// page that used to inline it now links to it instead. See
// docs/future-strategy/story_report_architecture_opus-5.md §7.6b: the
// project already applies this "same content, one address" rule to
// ctxgraph's message hashing and to ToolsSig's fingerprint; this file is
// the first place it's actually materialized to disk. Both functions here
// compute their own hash directly from rec — never from a caller-supplied
// ctxgraph.Manifest — so evidence extraction works identically whether or
// not the caller happens to have one, and can never disagree with one
// (see leadingSystem's doc comment).
package reqdetail

import (
	"crypto/md5"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
)

// leadingSystem returns how many of msgs' leading messages are contiguous
// role=="system" messages, and their concatenated text — mirrors
// ctxgraph.BuildManifest's own LeadSys/SysHash computation exactly
// (same contiguous-from-index-0 rule), recomputed here so evidence
// extraction needs nothing but rec: no Manifest lookup, no risk of a
// caller's stale/mismatched Manifest naming a file after the wrong hash.
func leadingSystem(msgs []chatmsg.Message) (leadSys int, text string) {
	for _, m := range msgs {
		if m.Role != "system" {
			break
		}
		leadSys++
	}
	return leadSys, ctxgraph.LeadingSystemText(msgs, leadSys)
}

// SysPromptEvidenceFileName is the deterministic evidence filename
// EnsureSysPromptEvidence writes for a leading system block whose content
// hash equals sysHash — sysHash is normally a Manifest's own SysHash field
// (md5 of the same LeadingSystemText EnsureSysPromptEvidence computes from
// rec — see ctxgraph.Manifest's doc comment on SysHash). Exported so a
// caller that only has the hash, not rec — e.g. a spine Step's "→ system
// prompt" link — can compute the same name without re-deriving this package's
// private naming convention. sysHash.String()[:8] is exactly what
// EnsureSysPromptEvidence itself computes for the same text: both are the
// hex encoding of the same digest's first 4 bytes.
func SysPromptEvidenceFileName(sysHash ctxgraph.Hash) string {
	return "sysprompt-" + sysHash.String()[:8] + ".md"
}

// EnsureSysPromptEvidence writes evidence/sysprompt-<h8>.md under
// evidenceDir — h8 = hex(md5(text)[:4]), the same digest
// ctxgraph.Manifest.SysHash carries for this exact record — when rec has a
// leading system block and that file doesn't already exist yet. Returns ""
// (and no error) when rec has no system message at all: nothing to link.
// Idempotent via the same existence-check-then-atomic-write EnsureRendered
// uses, for the same reason: content is a pure function of the hash in the
// filename, so a pre-existing file is always already correct. No lang
// parameter, unlike Render/EnsureRendered: the body is the system prompt's
// own raw text (whatever language the agent's client sent), not narrative
// text this package generates, so there is nothing here for i18n to select.
func EnsureSysPromptEvidence(evidenceDir string, rec *audit.Record) (filename string, err error) {
	body, ok := rec.Client.Request.Body.(map[string]any)
	if !ok {
		return "", nil
	}
	leadSys, text := leadingSystem(chatmsg.Messages(body))
	if leadSys == 0 {
		return "", nil
	}
	filename = SysPromptEvidenceFileName(ctxgraph.Hash(md5.Sum([]byte(text))))
	if err := ensureEvidenceFile(evidenceDir, filename, sysPromptEvidenceBody(text)); err != nil {
		return "", err
	}
	return filename, nil
}

// EnsureToolsEvidence is EnsureSysPromptEvidence's sibling for a request's
// declared tool set: evidence/tools-<h8>.md, h8 = toolsHash8(names) — the
// exact same hash ToolsSig already summarizes to 4 bytes in a table cell,
// written out in full here instead. Returns "" when the request declares
// no tools. Also no lang parameter, for the same reason as
// EnsureSysPromptEvidence: a tool's name and JSON schema aren't this
// package's narrative text either.
func EnsureToolsEvidence(evidenceDir string, rec *audit.Record) (filename string, err error) {
	body, ok := rec.Client.Request.Body.(map[string]any)
	if !ok {
		return "", nil
	}
	arr, _ := body["tools"].([]any)
	names := chatmsg.ToolNames(body)
	if len(arr) == 0 {
		return "", nil
	}
	filename = "tools-" + toolsHash8(names) + ".md"
	if err := ensureEvidenceFile(evidenceDir, filename, toolsEvidenceBody(arr, names)); err != nil {
		return "", err
	}
	return filename, nil
}

// ensureEvidenceFile is EnsureRendered's evidence-layer sibling: same
// stat-then-atomic-write idempotency contract, no rendering logic of its
// own — the two evidence functions above build the exact body text to
// write and hand it here.
func ensureEvidenceFile(dir, name, body string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeFileAtomic(dir, target, []byte(body))
}

func sysPromptEvidenceBody(text string) string {
	return "# System Prompt (" + strconv.Itoa(len([]rune(text))) + " chars)\n\n" + codeFence(text)
}

func toolsEvidenceBody(arr []any, names []string) string {
	var b strings.Builder
	b.WriteString("# Tools (" + strconv.Itoa(len(arr)) + ")\n\n")
	for i, tn := range arr {
		name := "?"
		if i < len(names) {
			name = names[i]
		}
		b.WriteString(Details(escapeHTML(name), codeFence(jsonIndent(tn))))
	}
	return b.String()
}
