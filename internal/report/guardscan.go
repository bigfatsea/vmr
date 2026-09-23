// Ver 2026-09-23 08:10, by Claude Opus 5.5

// Agent Guard's offline Fallback Path: extractRecordFacts
// picks between this file's two entry points on guardOutboundStamped(rec.Guard)
// (factscache.go). Not stamped -- guard: absent entirely, outbound mode: off,
// a trusted-provider exemption, any record predating the online wiring, or a
// record stamped inbound-only (the completion hook in server.go fills
// SanitizedRunes on a record whose outbound scan never ran) -- calls
// scanRecordForGuard, which runs BOTH halves (an outbound scan of
// Client.Request.Body plus the inbound forensics pass over
// Client.Response.Body) since no authoritative Hits exist yet to reuse.
// Already outbound-stamped -- audit_only/block ran online, or a historical
// replace-era stamp -- calls scanInboundFacts, which skips the
// outbound scan (arec.Guard.Hits is already authoritative; re-running it
// would only reproduce the same Hits) and runs just the inbound half,
// re-deriving known secrets from Client.Request.Body only to feed its own
// credential-echo check. Either way `vmr analyze` sees the same forensic
// facts an online-guarded run would have stamped. A record with an
// outbound-stamped Guard needs none of this for its Hits -- they already
// carry structured data — and stay exactly as guardcol.go's existing add()
// left them.
//
// guardEngine/guardScratch are process-lifetime singletons: one
// *guard.Engine (immutable rule table + prefilter automaton, cheap to
// build once) and one *guard.Scratch (the mutable per-scan workspace
// split out of Engine — a single shared instance is correct here
// specifically because this call path is one sequential goroutine, not
// because Engine itself is unsafe to share), both created lazily on first
// use and reused for every record scanFiles' single sequential goroutine
// ingests during this run (see internal/guard's package doc comment for
// why that single-goroutine fact means no concurrency work is needed
// here). guard.Fingerprint is deterministic — no
// salt to manage here at all, which also means this path and the online
// path always agree on the same credential's FP.
package report

import (
	"bytes"
	"sort"
	"sync"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/guard"
)

var (
	guardEngineOnce sync.Once
	guardEngineVal  *guard.Engine

	guardScratchPool = sync.Pool{
		New: func() any {
			return guard.NewScratch(len(guardEngine().Rules()))
		},
	}
)

// guardEngine returns the process-lifetime fallback-scan Engine, built from
// guard.DefaultRules() on first use. DefaultRules is a compile-time
// constant guard's own TestDefaultRules_Compile already proves compiles —
// an error here would be a programming error in this package's own wiring,
// not a runtime condition, hence the panic (mirrors guard.DefaultRules'
// own convention for the same reason).
func guardEngine() *guard.Engine {
	guardEngineOnce.Do(func() {
		eng, err := guard.NewEngine(guard.DefaultRules(), guard.RulesVersion)
		if err != nil {
			panic("report: guard.NewEngine: " + err.Error())
		}
		guardEngineVal = eng
	})
	return guardEngineVal
}

// guardScratch acquires a Scratch from guardScratchPool for thread-safe concurrent
// fallback scanning. Callers must putGuardScratch when finished.
func guardScratch() *guard.Scratch {
	return guardScratchPool.Get().(*guard.Scratch)
}

func putGuardScratch(sc *guard.Scratch) {
	guardScratchPool.Put(sc)
}

// GuardScanFacts is one record's Fallback Path result — computed only when
// arec.Guard == nil, cached verbatim via recordFacts.GuardScan so a cache
// hit reproduces it without ever touching arec again.
type GuardScanFacts struct {
	Hits               []audit.Hit             `json:"hits,omitempty"`
	InboundRunes       map[string]int          `json:"inbound_runes,omitempty"`
	ToolCallsInspected int                     `json:"tool_calls_inspected,omitempty"`
	ToolFindings       []GuardToolFindingFacts `json:"tool_findings,omitempty"`
	// ToolEchoCount/TextEchoCount split the decryption-oracle signal by
	// WHERE the echo landed:
	// ToolEchoCount is the high-risk half (a credential reappearing inside
	// a tool call's own arguments -- already counted per-finding in
	// ToolFindings' Echoed flag); TextEchoCount is the low-risk half (the
	// same credential reappearing in the assistant's plain text, which
	// is normal conversation, not an attack
	// signal, but is worth a background count as the baseline the
	// tool-arg half is read against).
	ToolEchoCount int `json:"tool_echo_count,omitempty"`
	TextEchoCount int `json:"text_echo_count,omitempty"`
}

// GuardToolFindingFacts is one risky tool-call judgment from the Fallback
// Path's InspectToolCall call — never a raw secret, only the tool's own
// argument text is ever excerpted (see guard.ToolVerdict.Excerpt's doc
// comment).
type GuardToolFindingFacts struct {
	Tool     string `json:"tool"`
	Category string `json:"category"`
	Hit      string `json:"hit,omitempty"`
	CWE      string `json:"cwe,omitempty"`
	PathHit  string `json:"path_hit,omitempty"`
	Excerpt  string `json:"excerpt,omitempty"`
	Echoed   bool   `json:"echoed,omitempty"`
}

// scanBodyTree walks an already-decoded JSON value (map[string]any /
// []any / string -- the shape audit.Record's fields decode into once
// report's json.Unmarshal has run, per audit.Message.Body's own doc
// comment) and calls visit once per rule hit found within any string leaf.
// Operating on the decoded tree directly, via guard.Engine.ScanText per
// leaf, is deliberately NOT "chatmsg.BodyRaw + guard.Engine.Scan on the
// re-marshaled JSON bytes": BodyRaw's json.Marshal round trip re-serializes
// the WHOLE body from scratch, and a coding-agent session resends its
// entire growing history on every turn, so re-marshaling it once per
// record turns a linear-in-corpus-size scan into one that is effectively
// quadratic in a long session's turn count. Walking the tree Go's own
// json.Unmarshal already built costs only the per-leaf []byte(str)
// conversion ScanText needs anyway -- no second parse, no re-encoding.
func scanBodyTree(v any, eng *guard.Engine, sc *guard.Scratch, visit func(secret []byte, f guard.Finding)) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return
		}
		body := []byte(t)
		for _, f := range eng.ScanText(body, sc) {
			visit(body[f.Start:f.End], f)
		}
	case map[string]any:
		for _, vv := range t {
			scanBodyTree(vv, eng, sc, visit)
		}
	case []any:
		for _, vv := range t {
			scanBodyTree(vv, eng, sc, visit)
		}
	}
}

// guardOutboundStamped reports whether g carries an authoritative OUTBOUND
// verdict — i.e. the online outbound scan actually ran and stamped a real
// result: server.applyOutboundGuard under audit_only/block (OutMode set),
// a historical replace-era record (OutMode "replace"), or a stamp
// predating the OutMode field (Ver set). An inbound-only stamp —
// server.go's completion hook filling SanitizedRunes on a record
// whose outbound scan never ran (outbound mode: off, trusted-provider
// exemption) — has OutMode == "", Ver == 0 and no Hits, and does NOT count
// as one: its outbound side remains the offline fallback's job, and the
// inbound artifacts must not suppress it the way a full stamp legitimately
// does (the blind-spot shape this direction-aware condition exists to
// close).
//
// OutMode == "error" (applyOutboundGuard's panic-recover stamp,
// server/guard.go) does NOT count either, despite carrying a non-zero Ver:
// it means the online scan crashed before producing a verdict, not that it
// ran clean and found nothing — treating it as stamped would permanently
// blind both online and offline detection for that one record. The offline
// scan is a safe second attempt, not a doomed repeat of the same failure:
// scanBodyTree walks the Go value tree report's own json.Unmarshal already
// decoded (this file's own doc comment), never internal/guard's hand-rolled
// byte-level walker (walk.go's walkStrings) the online path runs directly
// over raw wire bytes — a bug in that walker's own JSON traversal has no
// reason to reproduce against a tree the standard library already parsed
// successfully. extractRecordFacts' guardScanSafe wrapper is the backstop
// for the narrower case where a panic instead originates in code the two
// paths DO share (e.g. a rule's regex/entropy pass over one leaf string).
func guardOutboundStamped(g *audit.GuardRecord) bool {
	return g != nil && g.OutMode != "error" && (g.OutMode != "" || g.Ver != 0 || len(g.Hits) > 0)
}

// scanOutbound runs the Fallback Path's outbound half — the Engine scan
// over Client.Request.Body — returning the per-distinct-credential aggregated
// Hits and every distinct secret found (known, which the inbound half of
// scanRecordForGuard needs for its credential-echo checks).
func scanOutbound(arec *audit.Record, eng *guard.Engine) (hits []audit.Hit, known [][]byte) {
	type hitKey struct {
		rule string
		fp   string
	}
	byKey := map[hitKey]*audit.Hit{}
	var order []hitKey
	seenSecret := map[string]bool{}
	sc := guardScratch()
	defer putGuardScratch(sc)
	scanBodyTree(arec.Client.Request.Body, eng, sc, func(secret []byte, f guard.Finding) {
		// known is deduped by secret content alone (seenSecret), independent
		// of byKey's per-(rule, fp) dedup below: the same credential text can
		// satisfy more than one rule (e.g. a 48-char legacy OpenAI key also
		// matches Tier2's generic-sk-prefix), and byKey's rule-qualified key
		// would then let it into known twice -- countEchoes/InspectToolCall's
		// Echoed check would count one real echo as two.
		if !seenSecret[string(secret)] {
			seenSecret[string(secret)] = true
			known = append(known, append([]byte(nil), secret...))
		}
		fp := guard.Fingerprint(f.Rule, secret)
		k := hitKey{rule: f.Rule, fp: fp}
		h := byKey[k]
		if h == nil {
			h = &audit.Hit{
				Rule: f.Rule,
				Tier: int(f.Tier),
				FP:   fp,
			}
			byKey[k] = h
			order = append(order, k)
		}
		h.Count++
	})
	sort.Slice(order, func(i, j int) bool {
		if order[i].rule != order[j].rule {
			return order[i].rule < order[j].rule
		}
		return order[i].fp < order[j].fp
	})
	for _, k := range order {
		hits = append(hits, *byKey[k])
	}
	return hits, known
}

func scanInbound(arec *audit.Record, known [][]byte, out *GuardScanFacts) {
	if arec.Client.Response == nil {
		return
	}
	s := decodeResponseSummary(arec.Client.Response.Body)
	if s == nil {
		return
	}
	out.InboundRunes = guard.ClassifyRunes([]byte(s.Content), nil)
	out.InboundRunes = guard.ClassifyRunes([]byte(s.Reasoning), out.InboundRunes)
	out.TextEchoCount = countEchoes(s.Content, known) + countEchoes(s.Reasoning, known)
	for _, tc := range s.ToolCalls {
		out.ToolCallsInspected++
		// Tool-call arguments get the same steganography scan prose text
		// does: a hidden zero-width character spliced into a command
		// string is exactly the ASCII-smuggling shape this scan exists to
		// catch, not just Content/Reasoning.
		out.InboundRunes = guard.ClassifyRunes([]byte(tc.Args), out.InboundRunes)
		v := guard.InspectToolCall(tc.Name, []byte(tc.Args), known)
		if v.Echoed {
			out.ToolEchoCount++
		}
		if v.Hit == "" && v.PathHit == "" && !v.Echoed {
			continue
		}
		out.ToolFindings = append(out.ToolFindings, GuardToolFindingFacts{
			Tool: tc.Name, Category: v.Category, Hit: v.Hit, CWE: v.CWE,
			PathHit: v.PathHit, Excerpt: v.Excerpt, Echoed: v.Echoed,
		})
	}
}

// scanInboundFacts runs the inbound forensics pass over Client.Response.Body
// for records whose outbound side was already stamped online. It extracts known
// secrets from Client.Request.Body solely to feed the credential-echo checks.
func scanInboundFacts(arec *audit.Record) *GuardScanFacts {
	if arec.Client.Response == nil {
		return nil
	}
	var known [][]byte
	seen := map[string]bool{}
	sc := guardScratch()
	defer putGuardScratch(sc)
	scanBodyTree(arec.Client.Request.Body, guardEngine(), sc, func(secret []byte, f guard.Finding) {
		if seen[string(secret)] {
			return
		}
		seen[string(secret)] = true
		known = append(known, append([]byte(nil), secret...))
	})
	var out GuardScanFacts
	scanInbound(arec, known, &out)
	if len(out.InboundRunes) == 0 && len(out.ToolFindings) == 0 && out.TextEchoCount == 0 && out.ToolCallsInspected == 0 {
		return nil
	}
	return &out
}

// guardScanSafe runs fn (one record's fallback scan) and recovers a panic
// into a nil result instead of letting it propagate. The two fallback
// entry points (scanRecordForGuard/scanInboundFacts) sit at the bottom of a
// sequential, single-goroutine batch ingest with no other panic recovery
// anywhere in internal/report (unlike the online request path, which is
// isolated per-HTTP-request by net/http's own recover) — one malformed
// record's scan crashing would otherwise abort the entire `vmr analyze`
// run. A recovered record's GuardScan stays nil, but failed is returned as
// true so the failure count is visible on the report.
func guardScanSafe(fn func() *GuardScanFacts) (out *GuardScanFacts, failed bool) {
	defer func() {
		if recover() != nil {
			out = nil
			failed = true
		}
	}()
	return fn(), false
}

// scanRecordForGuard runs the Fallback Path over one record: outbound scan
// of Client.Request.Body, inbound rune/tool-call forensics over
// Client.Response.Body. Returns nil when there is nothing to scan
// or nothing was found — recordFacts.GuardScan stays nil in that case,
// matching Record.Guard's own "nil means nothing to report" convention.
func scanRecordForGuard(arec *audit.Record) *GuardScanFacts {
	eng := guardEngine()
	var out GuardScanFacts
	var known [][]byte

	out.Hits, known = scanOutbound(arec, eng)
	scanInbound(arec, known, &out)

	if len(out.Hits) == 0 && len(out.InboundRunes) == 0 && len(out.ToolFindings) == 0 && out.TextEchoCount == 0 && out.ToolCallsInspected == 0 {
		return nil
	}
	return &out
}

// countEchoes counts how many of known's secret values appear as a
// substring of text -- the plain-text half of the decryption-oracle
// signal (the tool-arg half is guard.InspectToolCall's Echoed field,
// already counted per tool call above). A single secret is counted once
// per occurrence in text, matching Hit.Count's own "context amplification,
// not independent leaks" semantics.
func countEchoes(text string, known [][]byte) int {
	if text == "" || len(known) == 0 {
		return 0
	}
	n := 0
	b := []byte(text)
	for _, secret := range known {
		if len(secret) == 0 {
			continue
		}
		for i := 0; ; {
			j := bytes.Index(b[i:], secret)
			if j < 0 {
				break
			}
			n++
			i += j + len(secret)
		}
	}
	return n
}

// decodeResponseSummary reassembles a response body into a StreamSummary
// regardless of whether it was recorded as an SSE stream (a JSON string,
// audit.Message.Body's "else string" case) or a non-streaming JSON object
// -- the exact type switch internal/reqdetail's renderClientResponse
// already uses to decide between chatmsg.ReassembleSSE and
// chatmsg.FinalMessage, duplicated here (not exported from reqdetail)
// because it is a five-line dispatch on a type switch, not parsing logic:
// the one parser both call sites actually depend on is chatmsg's.
func decodeResponseSummary(body any) *chatmsg.StreamSummary {
	switch b := body.(type) {
	case nil:
		return nil
	case string:
		return chatmsg.ReassembleSSE(b)
	default:
		if s, ok := chatmsg.FinalMessage(b); ok {
			return s
		}
		return nil
	}
}
