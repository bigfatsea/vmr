// Ver 2026-09-08, by coding assistant
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
)

// cmdDiff compares two audit records structurally: Header, System prompt,
// Tools declared, Messages (longest common prefix and divergence), and a Verdict.
func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	cfgPath := fs.String("c", "config.yaml", "path to config file (optional, for log_dir search)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: vmr diff [-c config.yaml] <coordA> <coordB>")
	}
	coordA := fs.Arg(0)
	coordB := fs.Arg(1)
	return runDiff(coordA, coordB, *cfgPath, os.Stdout)
}

// diffReport collects the structured comparison results for rendering.
type diffReport struct {
	coordA string
	coordB string

	headerRows []diffRow
	systemRows []diffRow
	toolsRows  []diffRow

	msgCountA int
	msgCountB int
	lcp       int
	divLine   string
	tailLineA string
	tailLineB string

	verdict string
}

type diffRow struct {
	label string
	valA  string
	valB  string
	ann   string
}

// runDiff locates both records, computes their structural divergence,
// and writes the formatted report to out.
func runDiff(coordA, coordB, cfgPath string, out io.Writer) error {
	recA, pathA, lineA, err := loadAuditRecord(coordA, "", cfgPath)
	if err != nil {
		return fmt.Errorf("coordA: %w", err)
	}
	recB, pathB, lineB, err := loadAuditRecord(coordB, "", cfgPath)
	if err != nil {
		return fmt.Errorf("coordB: %w", err)
	}

	mA, okA := ctxgraph.BuildManifest(recA, pathA, lineA)
	if !okA {
		return fmt.Errorf("%s:%d: request body is not a valid chat object", pathA, lineA)
	}
	mB, okB := ctxgraph.BuildManifest(recB, pathB, lineB)
	if !okB {
		return fmt.Errorf("%s:%d: request body is not a valid chat object", pathB, lineB)
	}

	report := computeDiff(coordA, coordB, recA, recB, mA, mB)
	renderDiff(out, report)
	return nil
}

// computeDiff constructs the diffReport comparing recA/mA against recB/mB.
func computeDiff(coordA, coordB string, recA, recB *audit.Record, mA, mB *ctxgraph.Manifest) diffReport {
	rep := diffReport{
		coordA: coordA,
		coordB: coordB,
	}

	// 1. Header rows
	rep.headerRows = []diffRow{
		makeHeaderRow("model", mA.Model, mB.Model),
		makeHeaderRow("protocol", mA.Protocol, mB.Protocol),
		makeHeaderRow("outcome", mA.Outcome, mB.Outcome),
		makeServedRow(recA, recB, mA, mB),
		makeUsageRow(mA, mB),
	}

	// 2. System rows
	rep.systemRows = []diffRow{
		makeSystemRow(mA, mB),
	}

	// 3. Tools rows
	// Names come from chatmsg.ToolNames (a readable +/- diff); the digest comes
	// from the manifest and catches an identical-name toolset whose schema
	// (a description or parameter shape) changed — itself a real cache break.
	toolsA := chatmsg.ToolNames(recA.Client.Request.Body)
	toolsB := chatmsg.ToolNames(recB.Client.Request.Body)
	rep.toolsRows = []diffRow{
		makeToolsRow(toolsA, toolsB, mA, mB),
	}

	// 4. Messages
	msgsA := chatmsg.Messages(recA.Client.Request.Body)
	msgsB := chatmsg.Messages(recB.Client.Request.Body)
	rep.msgCountA = len(mA.Keys)
	rep.msgCountB = len(mB.Keys)
	rep.lcp = lcpHashes(mA.Keys, mB.Keys)

	rep.divLine = firstDivergenceLine(mA, mB, msgsA, msgsB, rep.lcp, rep.msgCountA, rep.msgCountB)
	rep.tailLineA = tailLine("A", rep.msgCountA, rep.lcp, msgsA, mA.MsgIdx)
	rep.tailLineB = tailLine("B", rep.msgCountB, rep.lcp, msgsB, mB.MsgIdx)

	// 5. Verdict
	rep.verdict = determineVerdict(mA, mB, rep.lcp)

	return rep
}

func makeHeaderRow(label, valA, valB string) diffRow {
	ann := ""
	if valA == valB {
		ann = "(same)"
	}
	return diffRow{label: label, valA: valA, valB: valB, ann: ann}
}

func makeServedRow(recA, recB *audit.Record, mA, mB *ctxgraph.Manifest) diffRow {
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
	return diffRow{label: "served", valA: servedA, valB: servedB, ann: ann}
}

func lastEndpointStr(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "-"
	}
	return rec.Attempts[len(rec.Attempts)-1].Endpoint
}

func makeUsageRow(mA, mB *ctxgraph.Manifest) diffRow {
	valA := formatUsageSides(mA.Usage, mA.UsageInOK, mA.UsageOutOK, mA.EstIn, mA.EstOut)
	valB := formatUsageSides(mB.Usage, mB.UsageInOK, mB.UsageOutOK, mB.EstIn, mB.EstOut)
	ann := usageAnnotation(mA, mB, valA, valB)
	return diffRow{label: "usage", valA: valA, valB: valB, ann: ann}
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

func makeSystemRow(mA, mB *ctxgraph.Manifest) diffRow {
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
	return diffRow{label: "sys_hash", valA: valA, valB: valB, ann: ann}
}

func makeToolsRow(toolsA, toolsB []string, mA, mB *ctxgraph.Manifest) diffRow {
	valA := formatToolCount(len(toolsA))
	valB := formatToolCount(len(toolsB))
	ann := diffToolsets(toolsA, toolsB)
	// Identical names but a different manifest ToolsHash means a tool's schema
	// (description / parameters) changed without any name being added or removed.
	if ann == "(same)" && mA != nil && mB != nil &&
		mA.HasTools && mB.HasTools && mA.ToolsHash != mB.ToolsHash {
		ann = "(same names, tool schema changed)"
	}
	return diffRow{label: "toolset", valA: valA, valB: valB, ann: ann}
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

// renderDiff outputs the human-readable diff matching vmr replay formatting convention.
func renderDiff(w io.Writer, rep diffReport) {
	fmt.Fprintf(w, "vmr diff %s vs %s\n\n", rep.coordA, rep.coordB)

	// Determine column widths across all sections
	wA := 20
	wB := 20
	for _, row := range append(append(rep.headerRows, rep.systemRows...), rep.toolsRows...) {
		if len(row.valA) > wA {
			wA = len(row.valA)
		}
		if len(row.valB) > wB {
			wB = len(row.valB)
		}
	}

	renderSection(w, "Header", rep.headerRows, wA, wB)
	renderSection(w, "System", rep.systemRows, wA, wB)
	renderSection(w, "Tools", rep.toolsRows, wA, wB)

	fmt.Fprintf(w, "Messages (A=%d, B=%d, LCP=%d)\n", rep.msgCountA, rep.msgCountB, rep.lcp)
	fmt.Fprintln(w, rep.divLine)
	fmt.Fprintln(w, rep.tailLineA)
	fmt.Fprintln(w, rep.tailLineB)
	fmt.Fprintln(w)

	fmt.Fprintln(w, rep.verdict)
}

func renderSection(w io.Writer, title string, rows []diffRow, wA, wB int) {
	fmt.Fprintf(w, "%s\n", title)
	for _, r := range rows {
		if r.ann != "" {
			fmt.Fprintf(w, "  %-12s %-*s %-*s %s\n", r.label, wA, r.valA, wB, r.valB, r.ann)
		} else {
			fmt.Fprintf(w, "  %-12s %-*s %s\n", r.label, wA, r.valA, r.valB)
		}
	}
	fmt.Fprintln(w)
}
