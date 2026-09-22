// Ver 2026-09-22 02:05, by Sonnet 5

// The per-language row-table primitive every other file in this package
// builds its *Text constructor on top of — see Table's own doc comment.
package i18n

// Table is a per-language row lookup: index 0 is EN, index 1 is ZH,
// matching Lang's own iota order (lang.go). Every *Text builder that has
// at least one interpolated (func-typed) field keeps its literal
// templates in one of these instead of an "if lang == ZH {...}" branch —
// adding a language means adding a row here, never touching the closure
// logic that reads it. Package-private on purpose: nothing outside
// internal/i18n should construct or index one directly.
//
// *Text builders whose struct has no func-typed field keep the plain
// "if lang == ZH {...}" shape instead — KNOWN_ISSUES already found that
// collapsing a branch with nothing to collapse behind it (no duplicated
// closure logic) nets negative, not positive.
type Table[T any] [2]T

// Row returns lang's row. lang is always EN (0) or ZH (1) by construction
// (Lang has no other value today), so this never needs a bounds check —
// an out-of-range Lang would be a bug at the call site, and the array
// index panic surfaces it immediately instead of silently returning a
// zero-value row the way a map lookup would.
func (t Table[T]) Row(lang Lang) T { return t[lang] }
