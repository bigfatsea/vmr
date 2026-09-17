// Ver 2026-09-13, by Sonnet 5

// Package auditdiff compares two audit records structurally: header
// (model/protocol/outcome/served endpoint/usage), system prompt hash,
// declared toolset, and the message sequence (longest common prefix, first
// divergence, each side's tail) — then reduces that into one of five narrow
// structural verdicts. It is a pure function of its inputs (two
// *audit.Record plus their already-built *ctxgraph.Manifest): no file I/O,
// no CLI concerns. cmd/vmr/cmd_diff.go is the sole production caller, using
// internal/replay.LoadRecord to turn a "basename:line" coordinate into the
// (*audit.Record, path, line) triple Compute needs.
package auditdiff

import (
	"fmt"
	"strconv"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
)

// Report collects the structured comparison results for rendering.
type Report struct {
	CoordA string
	CoordB string

	HeaderRows []Row
	SystemRows []Row
	ToolsRows  []Row

	MsgCountA int
	MsgCountB int
	LCP       int
	DivLine   string
	TailLineA string
	TailLineB string

	Verdict string
}

// Row is one line of comparison in a section.
type Row struct {
	Label string
	ValA  string
	ValB  string
	Ann   string
}

// Compute constructs the Report comparing recA/mA against recB/mB.
func Compute(coordA, coordB string, recA, recB *audit.Record, mA, mB *ctxgraph.Manifest) Report {
	rep := Report{
		CoordA: coordA,
		CoordB: coordB,
	}

	// 1. Header rows
	rep.HeaderRows = []Row{
		makeHeaderRow("model", mA.Model, mB.Model),
		makeHeaderRow("protocol", mA.Protocol, mB.Protocol),
		makeHeaderRow("outcome", mA.Outcome, mB.Outcome),
		makeServedRow(recA, recB, mA, mB),
		makeUsageRow(mA, mB),
	}

	// 2. System rows
	rep.SystemRows = []Row{
		makeSystemRow(mA, mB),
	}

	// 3. Tools rows
	// Names come from chatmsg.ToolNames (a readable +/- diff); the digest comes
	// from the manifest and catches an identical-name toolset whose schema
	// (a description or parameter shape) changed — itself a real cache break.
	toolsA := chatmsg.ToolNames(recA.Client.Request.Body)
	toolsB := chatmsg.ToolNames(recB.Client.Request.Body)
	rep.ToolsRows = []Row{
		makeToolsRow(toolsA, toolsB, mA, mB),
	}

	// 4. Messages
	msgsA := chatmsg.Messages(recA.Client.Request.Body)
	msgsB := chatmsg.Messages(recB.Client.Request.Body)
	rep.MsgCountA = len(mA.Keys)
	rep.MsgCountB = len(mB.Keys)
	rep.LCP = lcpHashes(mA.Keys, mB.Keys)

	rep.DivLine = firstDivergenceLine(mA, mB, msgsA, msgsB, rep.LCP, rep.MsgCountA, rep.MsgCountB)
	rep.TailLineA = tailLine("A", rep.MsgCountA, rep.LCP, msgsA, mA.MsgIdx)
	rep.TailLineB = tailLine("B", rep.MsgCountB, rep.LCP, msgsB, mB.MsgIdx)

	// 5. Verdict
	rep.Verdict = determineVerdict(mA, mB, rep.LCP)

	return rep
}

func makeHeaderRow(label, valA, valB string) Row {
	ann := ""
	if valA == valB {
		ann = "(same)"
	}
	return Row{Label: label, ValA: valA, ValB: valB, Ann: ann}
}

func makeServedRow(recA, recB *audit.Record, mA, mB *ctxgraph.Manifest) Row {
	servedA := mA.ServedEndpoint
	if servedA == "" {
		servedA = lastEndpointStr(recA)
	}
	servedB := mB.ServedEndpoint
	if servedB == "" {
		servedB = lastEndpointStr(recB)
	}
	ann := ""
	if servedA == servedB {
		ann = "(same)"
	}
	return Row{Label: "served", ValA: servedA, ValB: servedB, Ann: ann}
}

func lastEndpointStr(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "-"
	}
	return rec.Attempts[len(rec.Attempts)-1].Endpoint
}

func makeUsageRow(mA, mB *ctxgraph.Manifest) Row {
	valA := formatUsageSides(mA.Usage, mA.UsageInOK, mA.UsageOutOK, mA.EstIn, mA.EstOut)
	valB := formatUsageSides(mB.Usage, mB.UsageInOK, mB.UsageOutOK, mB.EstIn, mB.EstOut)
	ann := usageAnnotation(mA, mB, valA, valB)
	return Row{Label: "usage", ValA: valA, ValB: valB, Ann: ann}
}

func formatUsageSides(u chatmsg.Usage, inOK, outOK bool, estIn, estOut int64) string {
	var inStr, outStr string
	if inOK {
		inStr = formatComma(u.In)
	} else if estIn > 0 {
		inStr = fmt.Sprintf("~%s (est)", formatComma(estIn))
	} else {
		inStr = "-"
	}

	if outOK {
		outStr = formatComma(u.Out)
	} else if estOut > 0 {
		outStr = fmt.Sprintf("~%s (est)", formatComma(estOut))
	} else {
		outStr = "-"
	}
	return fmt.Sprintf("in %s / out %s", inStr, outStr)
}

func usageAnnotation(mA, mB *ctxgraph.Manifest, valA, valB string) string {
	cA := mA.Usage.CacheRead
	cB := mB.Usage.CacheRead
	if cA > 0 || cB > 0 {
		if cA == cB {
			return fmt.Sprintf("(cached %s, same)", formatComma(cA))
		}
		return fmt.Sprintf("(cached %s → %s)", formatComma(cA), formatComma(cB))
	}
	if valA == valB {
		return "(same)"
	}
	return ""
}

func makeSystemRow(mA, mB *ctxgraph.Manifest) Row {
	valA := "none"
	if mA.HasSys {
		valA = mA.SysHash.String()[:8]
	}
	valB := "none"
	if mB.HasSys {
		valB = mB.SysHash.String()[:8]
	}
	ann := ""
	if valA == valB {
		ann = "(same)"
	}
	return Row{Label: "sys_hash", ValA: valA, ValB: valB, Ann: ann}
}

func makeToolsRow(toolsA, toolsB []string, mA, mB *ctxgraph.Manifest) Row {
	valA := formatToolCount(len(toolsA))
	valB := formatToolCount(len(toolsB))
	ann := diffToolsets(toolsA, toolsB)
	// Identical names but a different manifest ToolsHash means a tool's schema
	// (description / parameters) changed without any name being added or removed.
	if ann == "(same)" && mA != nil && mB != nil &&
		mA.HasTools && mB.HasTools && mA.ToolsHash != mB.ToolsHash {
		ann = "(same names, tool schema changed)"
	}
	return Row{Label: "toolset", ValA: valA, ValB: valB, Ann: ann}
}

func formatToolCount(n int) string {
	if n == 1 {
		return "1 tool"
	}
	return fmt.Sprintf("%d tools", n)
}

func diffToolsets(toolsA, toolsB []string) string {
	setA := make(map[string]bool, len(toolsA))
	for _, t := range toolsA {
		setA[t] = true
	}
	setB := make(map[string]bool, len(toolsB))
	for _, t := range toolsB {
		setB[t] = true
	}
	var added []string
	seenAdded := make(map[string]bool)
	for _, t := range toolsB {
		if !setA[t] && !seenAdded[t] {
			seenAdded[t] = true
			added = append(added, t)
		}
	}
	var removed []string
	seenRemoved := make(map[string]bool)
	for _, t := range toolsA {
		if !setB[t] && !seenRemoved[t] {
			seenRemoved[t] = true
			removed = append(removed, t)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		if len(toolsA) == len(toolsB) {
			return "(same)"
		}
		return ""
	}
	if len(added) > 0 && len(removed) == 0 {
		return fmt.Sprintf("(+%d: %s)", len(added), strings.Join(added, ", "))
	}
	if len(added) == 0 && len(removed) > 0 {
		return fmt.Sprintf("(-%d: %s)", len(removed), strings.Join(removed, ", "))
	}
	return fmt.Sprintf("(+%d: %s, -%d: %s)", len(added), strings.Join(added, ", "), len(removed), strings.Join(removed, ", "))
}

func lcpHashes(a, b []ctxgraph.Hash) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func firstDivergenceLine(mA, mB *ctxgraph.Manifest, msgsA, msgsB []chatmsg.Message, lcp, nA, nB int) string {
	if lcp == nA && lcp == nB {
		return fmt.Sprintf("  first divergence: none (all %d message(s) identical)", nA)
	}
	if lcp == nA && lcp < nB {
		roleB := msgRoleAt(msgsB, mB.MsgIdx, lcp)
		return fmt.Sprintf("  [%d] first divergence: B extends A with %d new message(s) (first role=%s)", lcp, nB-lcp, roleB)
	}
	if lcp == nB && lcp < nA {
		roleA := msgRoleAt(msgsA, mA.MsgIdx, lcp)
		return fmt.Sprintf("  [%d] first divergence: B truncated after %d message(s) (A continues with role=%s)", lcp, lcp, roleA)
	}
	roleA := msgRoleAt(msgsA, mA.MsgIdx, lcp)
	roleB := msgRoleAt(msgsB, mB.MsgIdx, lcp)
	if roleA == roleB {
		return fmt.Sprintf("  [%d] first divergence: A msg#%d (role=%s) rewritten in B (hash differs)", lcp, lcp, roleA)
	}
	return fmt.Sprintf("  [%d] first divergence: A msg#%d (role=%s) vs B msg#%d (role=%s) (role and hash differ)", lcp, lcp, roleA, lcp, roleB)
}

func msgRoleAt(msgs []chatmsg.Message, msgIdx []int, k int) string {
	if k >= 0 && k < len(msgIdx) {
		idx := msgIdx[k]
		if idx >= 0 && idx < len(msgs) {
			return msgs[idx].Role
		}
	}
	return "unknown"
}

func tailLine(prefix string, n, lcp int, msgs []chatmsg.Message, msgIdx []int) string {
	tailCount := n - lcp
	if tailCount <= 0 {
		return fmt.Sprintf("  %s tail: 0", prefix)
	}
	roles := extractTailRoles(msgs, msgIdx, lcp)
	return fmt.Sprintf("  %s tail: %d new message(s) (roles: %s)", prefix, tailCount, roles)
}

func extractTailRoles(msgs []chatmsg.Message, msgIdx []int, lcp int) string {
	var roles []string
	seen := make(map[string]bool)
	for i := lcp; i < len(msgIdx); i++ {
		idx := msgIdx[i]
		if idx >= 0 && idx < len(msgs) {
			r := msgs[idx].Role
			if !seen[r] {
				seen[r] = true
				roles = append(roles, r)
			}
		}
	}
	if len(roles) == 0 {
		return "-"
	}
	if len(roles) > 3 {
		return strings.Join(roles[:3], ",") + ",…"
	}
	return strings.Join(roles, ",")
}

// determineVerdict selects 1 of 5 narrow verdict rules based on structural facts.
func determineVerdict(mA, mB *ctxgraph.Manifest, lcp int) string {
	const disclaimer = "Structural fact, not a root cause."

	// Rule 1: System prompt differs
	if mA.HasSys != mB.HasSys || (mA.HasSys && mA.SysHash != mB.SysHash) {
		return fmt.Sprintf("Verdict: B uses a new system prompt relative to A (system hash differs). %s", disclaimer)
	}

	nA := len(mA.Keys)
	nB := len(mB.Keys)

	// Rule 2: Unrelated context
	if lcp == 0 && (nA > 0 || nB > 0) {
		return fmt.Sprintf("Verdict: B and A share no common message context (LCP=0, unrelated context). %s", disclaimer)
	}

	// Rule 3: Identical context
	if lcp == nA && lcp == nB {
		return fmt.Sprintf("Verdict: B has identical message context to A (all messages and system prompt match). %s", disclaimer)
	}

	// Rule 4: Pure extension
	if lcp == nA && nB > nA {
		return fmt.Sprintf("Verdict: B is an extension of A (%d message(s) appended, common prefix preserved). %s", nB-nA, disclaimer)
	}

	// Rule 5a: Context-truncated prefix
	if lcp == nB && nA > nB {
		return fmt.Sprintf("Verdict: B looks like a context-truncated prefix of A (%d message(s) truncated, same system). %s", nA-nB, disclaimer)
	}

	// Rule 5b: Context-truncated retry (tail replaced)
	return fmt.Sprintf("Verdict: B looks like a context-truncated retry of A (tail replaced, same system). %s", disclaimer)
}

func formatComma(n int64) string {
	if n < 0 {
		return "-" + formatComma(-n)
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b []byte
	rem := len(s) % 3
	if rem > 0 {
		b = append(b, s[:rem]...)
		if len(s) > rem {
			b = append(b, ',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		if i > rem {
			b = append(b, ',')
		}
		b = append(b, s[i:i+3]...)
	}
	return string(b)
}
