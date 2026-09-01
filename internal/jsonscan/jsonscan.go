// Ver 2026-08-14, by Sonnet 5

// Package jsonscan provides a zero-dependency JSON byte-range scanning and
// byte-splice rewrite engine. It powers vmr's high-performance, byte-faithful
// request transformations (RewriteModel, RewriteStream, RewriteRoles,
// RewriteInputRoles) as well as the low-level structural primitives used by
// adapter/server (TopLevelValues, WalkArrayElements, FirstArrayElement,
// ElementRole, SkipJSONWS, SkipJSONString, SkipJSONValue, IndexUnescapedQuote).
//
// Boundary rule: jsonscan owns structural byte scanning and byte-splice
// rewriting over raw JSON buffers without unmarshaling or re-marshaling
// full request payloads. Protocol-specific routing semantics, adapter
// construction, and error classification belong in internal/adapter and
// higher layers.
//
// Everything here is a structural scanner, not a strict validator: malformed
// input is declined (ok=false / an error), never panics, and a scan that
// succeeds makes no claim about byte ranges the scan didn't visit. This is
// the same "lean on json.Unmarshal's fallback for anything unusual" contract
// callers rely on.
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
