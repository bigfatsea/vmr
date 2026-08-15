// Ver 2026-08-15, by Sonnet 5

// The per-bucket accumulation half of rows.go's TrafficStats/Row/HourRow/
// EndpointRow/ClientRow/WorkloadRow/SessionRow declarations — split into its
// own file (Part 8 batch B4) once TrafficStats stopped meaning "6 Row types
// share a field core but no shared type" (the architecture review's R3
// finding): aggregate.go's per-record loop now just calls these methods
// instead of inlining 7 near-identical closures. diagnosticNormMarker
// (aggregate.go) and SlowThresholdMS (rows.go) are this file's only
// same-package dependencies worth naming.
package report

import "vmr/internal/audit"

// TrafficStats.Ingest is the accumulation every embedding bucket shares —
// see the type's own doc comment (rows.go) for exactly which fields live
// here vs stay on each row type. Requests/OK/Errors/tokens/duration-basis
// only; TTFT/stream/bytes/images/roles are each row type's own, added by
// its own Ingest wrapper below.
func (s *TrafficStats) Ingest(rc *rec2) {
	s.Requests++
	switch rc.outcome {
	case "ok":
		s.OK++
	case "canceled":
	default:
		s.Errors++
	}
	if rc.usageOK {
		s.TokensIn += rc.usage.In
		s.TokensInCached += rc.usage.CacheRead
		s.TokensInCacheWrite += rc.usage.CacheWrite
		s.TokensOut += rc.usage.Out
		s.TokensReasoning += rc.usage.Reasoning
		s.TokensKnown++
	}
	if rc.durMS > 0 {
		s.RequestsWithDur++
		s.durs = append(s.durs, rc.durMS)
		if rc.durMS > SlowThresholdMS {
			s.SlowRequests++
		}
	}
}

// Ingest accumulates one record into Overall/ByModel/ByDate: the shared
// TrafficStats core, plus family A's remaining volume/outcome fields,
// family D (wire/payload), and the TTFT/stream/TokOutPerSec basis the core
// doesn't cover (see TrafficStats' doc comment for why those stay here).
func (r *Row) Ingest(rc *rec2) {
	r.TrafficStats.Ingest(rc)
	if rc.outcome == "canceled" {
		r.Canceled++
	}
	if rc.stream {
		r.Streams++
	}
	if rc.fallbacks > 0 {
		r.Fallbacks++
		if rc.outcome == "ok" {
			r.FallbackRecovered++
		} else {
			r.FallbackFailed++
		}
	}
	if rc.truncated {
		r.Truncated++
	}
	r.BytesIn += rc.bytesIn
	r.BytesOut += rc.bytesOut
	if rc.durMS > 0 {
		r.DurMSSum += rc.durMS
		if rc.durMS > r.DurMSMax {
			r.DurMSMax = rc.durMS
		}
		if rc.usageOK {
			r.tokDurMS += rc.durMS
		}
	}
	if rc.ttftMS > 0 {
		r.TTFTKnown++
		r.TTFTMSSum += rc.ttftMS
		r.ttfts = append(r.ttfts, rc.ttftMS)
	}
	if rc.streamOK {
		r.StreamKnown++
		r.streamMS = append(r.streamMS, rc.streamMS)
	}
	r.Images += rc.images
	r.ImagesCompressed += rc.imagesCompressed
	if len(rc.roleChars) > 0 {
		if r.RoleChars == nil {
			r.RoleChars = map[string]int64{}
		}
		for role, c := range rc.roleChars {
			r.RoleChars[role] += c
		}
	}
	if len(rc.roleTokens) > 0 {
		if r.RoleTokens == nil {
			r.RoleTokens = map[string]int64{}
		}
		for role, t := range rc.roleTokens {
			r.RoleTokens[role] += t
		}
	}
}

// Ingest accumulates one record into an hour/hour-of-day bucket.
func (h *HourRow) Ingest(rc *rec2) {
	h.TrafficStats.Ingest(rc)
	if rc.fallbacks > 0 {
		h.Fallbacks++
	}
	if rc.truncated {
		h.Truncated++
	}
	h.BytesIn += rc.bytesIn
	h.BytesOut += rc.bytesOut
	if rc.durMS > 0 && rc.durMS > h.DurMSMax {
		h.DurMSMax = rc.durMS
	}
	if rc.ttftMS > 0 {
		h.TTFTKnown++
		h.ttfts = append(h.ttfts, rc.ttftMS)
	}
	if rc.streamOK {
		h.StreamKnown++
		h.streamMS = append(h.streamMS, rc.streamMS)
	}
	h.Images += rc.images
}

// IngestAttempt is the endpoint's ATTEMPT-grade accounting (G-family) — one
// call per audit.Attempt, regardless of whether it was the attempt that
// actually served the client. EndpointRow deliberately does not embed
// TrafficStats (see that type's doc comment): Attempts/OK here count
// attempts, not requests, and must never be conflated with IngestRequest's
// request-grade counters below — that conflation is the exact basis bug
// cmd/vmr/quota_parity_test.go's comment records having happened once
// already.
func (e *EndpointRow) IngestAttempt(a audit.Attempt) {
	e.Attempts++
	// Forwarded is OK's condition WITHOUT `Error == ""` — see its own
	// doc comment (rows.go): a truncated 2xx still got forwarded and
	// still got charged, so only this count reproduces the router's
	// requests-metric quota charging exactly.
	if a.Response != nil && a.Response.Status < 400 {
		e.Forwarded++
	}
	if a.Error == "" && a.Response != nil && a.Response.Status < 400 {
		e.OK++
		for _, n := range a.Norm {
			if !diagnosticNormMarker[n] {
				continue
			}
			if e.NormCounts == nil {
				e.NormCounts = map[string]int{}
			}
			e.NormCounts[n]++
		}
	} else {
		e.Failed++
		e.WastedMS += a.DurMS
		cls := attemptErrorClass(a)
		if cls == "" {
			cls = "unknown"
		}
		if e.ErrorClasses == nil {
			e.ErrorClasses = map[string]int{}
		}
		e.ErrorClasses[cls]++
	}
}

// IngestRequest is the endpoint's REQUEST-grade accounting: metrics for the
// one record this endpoint actually served the client from — see
// IngestAttempt's doc comment for why this stays a separate method rather
// than a shared TrafficStats.Ingest call.
func (e *EndpointRow) IngestRequest(rc *rec2) {
	e.Requests++
	if rc.outcome == "ok" {
		e.RequestsOK++
	}
	if rc.usageOK {
		e.TokensIn += rc.usage.In
		e.TokensInCached += rc.usage.CacheRead
		e.TokensInCacheWrite += rc.usage.CacheWrite
		e.TokensOut += rc.usage.Out
		e.TokensReasoning += rc.usage.Reasoning
		e.TokensKnown++
		e.inToks = append(e.inToks, rc.usage.In)
		e.outToks = append(e.outToks, rc.usage.Out)
	} else if rc.estInFresh > 0 || rc.estOut > 0 {
		// Usage was never sniffed: carry the same degraded estimate the
		// router charged, in its own fields (see EndpointRow's doc
		// comment for why these must not merge into TokensIn*/TokensOut).
		e.TokensInFreshEst += rc.estInFresh
		e.TokensOutEst += rc.estOut
		e.TokensEstimated++
	}
	if rc.ttftMS > 0 {
		e.TTFTKnown++
		e.ttfts = append(e.ttfts, rc.ttftMS)
	}
	if rc.durMS > 0 {
		e.RequestsWithDur++
		e.durs = append(e.durs, rc.durMS)
		e.DurMSSum += rc.durMS
		if rc.durMS > e.DurMSMax {
			e.DurMSMax = rc.durMS
		}
		if rc.durMS > SlowThresholdMS {
			e.SlowRequests++
		}
	}
	if rc.streamOK {
		e.StreamKnown++
		e.streamMS = append(e.streamMS, rc.streamMS)
	}
}

// Ingest accumulates one record into a by-client_key_tag bucket.
func (c *ClientRow) Ingest(rc *rec2) {
	c.TrafficStats.Ingest(rc)
	if rc.usageOK {
		c.inToks = append(c.inToks, rc.usage.In)
		c.outToks = append(c.outToks, rc.usage.Out)
	}
	if rc.ttftMS > 0 {
		c.ttfts = append(c.ttfts, rc.ttftMS)
	}
	if rc.streamOK {
		c.streamMS = append(c.streamMS, rc.streamMS)
	}
}

// Ingest accumulates one record into a workload-class bucket.
func (w *WorkloadRow) Ingest(rc *rec2) {
	w.TrafficStats.Ingest(rc)
	if rc.streamOK {
		w.streamMS = append(w.streamMS, rc.streamMS)
	}
	w.ToolCalls += len(rc.toolCalls)
	if len(rc.toolCalls) > 0 {
		w.RequestsWithToolCalls++
	}
}

// Ingest accumulates one record into a session bucket.
func (s *SessionRow) Ingest(rc *rec2) {
	s.TrafficStats.Ingest(rc)
	if rc.fallbacks > 0 {
		s.Fallbacks++
	}
	if rc.truncated {
		s.Truncated++
	}
	if rc.durMS > 0 && rc.durMS > s.DurMSMax {
		s.DurMSMax = rc.durMS
	}
	if rc.ttftMS > 0 {
		s.TTFTKnown++
		s.ttfts = append(s.ttfts, rc.ttftMS)
	}
	s.Images += rc.images
	if len(rc.roleChars) > 0 {
		if s.RoleChars == nil {
			s.RoleChars = map[string]int64{}
		}
		for role, c := range rc.roleChars {
			s.RoleChars[role] += c
		}
	}
}
