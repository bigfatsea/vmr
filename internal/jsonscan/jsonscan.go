// Ver 2026-08-14, by Sonnet 5

// Package jsonscan is the zero-dependency JSON byte-range scanning engine
// behind vmr's byte-splice model/stream/role rewrites (internal/adapter's
// RewriteModel/RewriteStream/RewriteRoles/RewriteInputRoles used to live
// here, plus the structural primitives internal/adapter's SessionFingerprint
// and TopLevelProbe still call: TopLevelValues, WalkArrayElements,
// FirstArrayElement, ElementRole, the Skip* helpers).
//
// Everything here is a structural scanner, not a strict validator: malformed
// input is declined (ok=false / an error), never panics, and a scan that
// succeeds makes no claim about byte ranges the scan didn't visit. This is
// the same "lean on json.Unmarshal's fallback for anything unusual" contract
// internal/adapter's callers have always relied on.
package jsonscan

import (
	"bytes"
	"encoding/json"
)

// MarshalNoEscape is json.Marshal without HTML escaping and without the
// trailing newline json.Encoder adds. Every place vmr re-serializes client
// JSON (model rewrite, image downscaling) uses this: the default marshal
// would rewrite < > & in message content to \uXXXX — semantically identical,
// but a gratuitous byte-level deviation from what a direct call would send.
func MarshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
