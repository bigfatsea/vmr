// Ver 2026-09-23 03:30, by Claude Opus 5.5

// Attempt.SetSanitizedRunes — split into its own file purely to keep
// audit.go under archtest's file-size budget, not because this method
// belongs to a different contract than audit.go's other Attempt.Set*
// methods (see that file's own doc comment on the convention they share).
// SetSecurityBlocked/SetGuardBlock/GuardBlock (and core.ErrSecurity) were
// removed with the online Tool Call gate — no real audit record
// anywhere ever carried a security-blocked Attempt (the online gate landed
// and was removed within the same development window, before any tagged
// release or recorded production run), so there is no historical decode
// compatibility to preserve.
package audit

// SetSanitizedRunes carries the inbound rune-sanitization counts.
func (a *Attempt) SetSanitizedRunes(counts map[string]int) {
	if a == nil {
		return
	}
	a.sanitizedRunes = counts
}

// SanitizedRunes returns the attempt's sanitized-rune counts, nil when none.
func (a *Attempt) SanitizedRunes() map[string]int {
	if a == nil {
		return nil
	}
	return a.sanitizedRunes
}
