// Ver 2026-09-16, by Sonnet 5

// Outbound: the online orchestration surface M3.4 mounts at server.chatHandler
// (docs/design/agent-guard-technical-spec-final-2.0.md ADR-4/§4.2/§4.3/ADR-5,
// M3.4/M3.6). Guard combines the detection Engine (M3.0/M1) and Fingerprint
// (M1.6, deterministic — no salt lifecycle, K-G19) into the single object
// server/router hold — K12's "≤5 lines at the call site" budget is what
// pushed the scan+aggregate+fingerprint logic into this one method instead
// of leaving it inline.
//
// mode: block's request-rejection short-circuit is decided by the caller
// (internal/server/guard.go, M3.6): Outbound reports BlockedBy, the
// server-side mount point acts on it. The former mode: replace (pseudonym
// rewrite + response-side restore) was removed — see the package doc.
package guard

import (
	"sort"
	"sync"
)

// OutMode mirrors config.GuardOutbound.Mode's three string values exactly
// (config's own validateGuard rejects anything else at load time) — guard
// cannot import internal/config (ADR-1's dependency whitelist is
// {jsonscan} only; config does not import guard either, so this is not a
// cycle-avoidance accident but ADR-1's independence held deliberately,
// see KNOWN_ISSUES K-G20), hence a second, independently-declared
// constant set with the same literal values rather than a shared type.
type OutMode string

const (
	OutOff       OutMode = "off"
	OutAuditOnly OutMode = "audit_only"
	OutBlock     OutMode = "block"
)

// Hit is one rule's aggregated match across a whole scanned body — the
// online counterpart to Finding, collapsed by rule name and counted (the
// "上下文放大系数" raw data, §2.3 结论 3), with a Fingerprint attached.
// Field-for-field mirrors audit.Hit; guard cannot import audit (same
// dependency-whitelist reason as OutMode above), so a caller that needs
// an audit.Hit (internal/server) copies these four fields across — see
// internal/server/guard.go's toAuditHits.
type Hit struct {
	Rule  string
	Tier  Tier
	Count int
	FP    string
}

// OutboundResult is Outbound's return value (§4.2's OutboundResult
// sketch).
type OutboundResult struct {
	// Body is the input body, unchanged (Outbound never rewrites: the
	// former mode: replace was removed; audit_only/block both forward the
	// original bytes).
	Body []byte
	// Hits lists every rule that matched, Tier1 and Tier2 both (§4.7:
	// Tier2 is audit-only forever, K-G5, but still worth surfacing).
	Hits []Hit
	// BlockedBy is the first Tier1 rule name found, when any -- computed
	// unconditionally (not just under mode: block) since it costs nothing
	// extra once Hits is already aggregated. Acted on by the caller
	// (internal/server/guard.go, M3.6) only when mode == OutBlock.
	BlockedBy string
}

// Guard is the online orchestration type server/router hold and call for
// the outbound (and inbound) path. Safe for concurrent use: engine is an
// immutable shared singleton (M3.0's own concurrency contract) and
// Outbound's only per-call mutable state is a pooled Scratch.
type Guard struct {
	engine      *Engine
	scratchPool sync.Pool
}

// NewGuard returns a ready Guard.
func NewGuard(engine *Engine) *Guard {
	g := &Guard{engine: engine}
	ruleCount := 0
	if engine != nil {
		ruleCount = len(engine.Rules())
	}
	g.scratchPool.New = func() any {
		return NewScratch(ruleCount)
	}
	return g
}

// Outbound scans body for credential-shaped content and returns the
// aggregated result. It never rewrites the body: the former mode: replace
// was removed (see the package doc) — facing an untrusted upstream,
// audit_only observes and block rejects (ADR-6: replace never defended
// against an active upstream anyway). mode == OutOff skips scanning
// entirely.
// The caller (internal/server/guard.go) short-circuits off before calling
// at all and leaves Record.Guard nil, so an off-mode record carries no
// outbound verdict and its request body is covered by the offline fallback
// path exactly like a guard:-absent one — "off" and "absent" are the same
// fact to `vmr analyze`. (Inbound artifacts may still stamp such a
// record's Guard with SanitizedRunes; the fallback's outbound half
// is direction-aware — report.guardscan's guardOutboundStamped — so that
// stamp does not suppress the request-body scan.)
func (g *Guard) Outbound(body []byte, mode OutMode) OutboundResult {
	result := OutboundResult{Body: body}
	if mode == OutOff || len(body) == 0 {
		return result
	}
	sc := g.scratchPool.Get().(*Scratch)
	defer g.scratchPool.Put(sc)
	findings := g.engine.Scan(body, sc)
	if len(findings) == 0 {
		return result
	}
	result.Hits = g.aggregateHits(body, findings)
	for _, h := range result.Hits {
		if h.Tier == Tier1 {
			result.BlockedBy = h.Rule
			break
		}
	}
	return result
}

// aggregateHits collapses raw per-occurrence Findings into one Hit per
// distinct (rule, fingerprint) pair, matching guardcol.go/guardscan.go's
// aggregation shape (Count = occurrences of this specific credential within
// this one body, FP computed for each distinct credential).
func (g *Guard) aggregateHits(raw []byte, findings []Finding) []Hit {
	type hitKey struct {
		rule string
		fp   string
	}
	byKey := map[hitKey]*Hit{}
	var order []hitKey
	for _, f := range findings {
		fp := Fingerprint(f.Rule, f.Body(raw))
		k := hitKey{rule: f.Rule, fp: fp}
		h, ok := byKey[k]
		if !ok {
			h = &Hit{Rule: f.Rule, Tier: f.Tier, FP: fp}
			byKey[k] = h
			order = append(order, k)
		}
		h.Count++
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].rule != order[j].rule {
			return order[i].rule < order[j].rule
		}
		return order[i].fp < order[j].fp
	})
	hits := make([]Hit, 0, len(order))
	for _, k := range order {
		hits = append(hits, *byKey[k])
	}
	return hits
}
