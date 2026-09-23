// Ver 2026-09-23 08:10, by Claude Opus 5.5

package audit

// The Forwarded side of Attempt: the authoritative "this response was
// actually forwarded (and charged)" signal plus the one compatibility
// predicate historical records are read through. Split out of audit.go by
// the archtest line budget on that file — same package, same type, so the
// field itself stays on Attempt's own definition.

// SetForwarded records that this attempt's response was actually forwarded
// to the client. router.forwardSuccess is the ONLY caller — see the
// Forwarded field's own doc comment for what it means and which paths
// never set it.
func (a *Attempt) SetForwarded() {
	if a == nil {
		return
	}
	a.Forwarded = true
}

// IsForwarded is the analytics-side predicate for "was this attempt's
// response actually forwarded (and charged) to the client". It exists
// because the Forwarded field was added after historical JSONL was already
// written: pre-v4 records lack it, so its zero value (false) is ambiguous
// — it means "not forwarded" on new records but "field absent" on old
// ones. The rule: a true Forwarded is authoritative; a false one falls
// back to the old-format signal (a < 400 response with no error class),
// which historical softblock records never satisfy (the historical
// softblock failover path wrote ErrorClass "content" alongside its < 400
// response). Do not re-derive this decision at each call site — it is the
// single compatibility chokepoint for the field.
func (a *Attempt) IsForwarded() bool {
	if a == nil {
		return false
	}
	if a.Forwarded {
		return true
	}
	return a.Response != nil && a.Response.Status < 400 && a.ErrorClass == ""
}

// ServedEndpoint returns the endpoint that served the client. It prefers the
// last served and error-free attempt, falls back to the last served attempt,
// and returns "" if no attempt served the client. "Served" is the served()
// predicate below — wider than IsForwarded on pre-field records.
func (r *Record) ServedEndpoint() string {
	if r == nil {
		return ""
	}
	var successEp, servedEp string
	for _, a := range r.Attempts {
		if !a.served() {
			continue
		}
		servedEp = a.Endpoint
		if a.Error == "" {
			successEp = a.Endpoint
		}
	}
	if successEp != "" {
		return successEp
	}
	return servedEp
}

// served reports whether this attempt's response bytes reached the client.
// Wider than IsForwarded on pre-field records: a committed 2xx that was later
// truncated or canceled still served the client (its ErrorClass is set by
// the cut, not by a softblock), so endpoint attribution counts it; only a
// softblock (ErrorClass "content" on a < 400 response) never served.
func (a *Attempt) served() bool {
	if a.IsForwarded() {
		return true
	}
	return a.Response != nil && a.Response.Status < 400 && a.ErrorClass != "content"
}
