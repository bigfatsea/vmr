// Ver 2026-09-02, by pi-agent

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
// which new-format softblock records never satisfy (checkSoftBlock writes
// ErrorClass "content" alongside its < 400 response). Do not re-derive
// this decision at each call site — it is the single compatibility
// chokepoint for the field.
func (a *Attempt) IsForwarded() bool {
	if a == nil {
		return false
	}
	if a.Forwarded {
		return true
	}
	return a.Response != nil && a.Response.Status < 400 && a.ErrorClass == ""
}
