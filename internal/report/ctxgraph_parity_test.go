// Ver 2026-07-28 22:50, by Sonnet 5

// This file cross-validates that internal/ctxgraph's manifest hashing
// produces the exact same per-message digests as this package's own
// collect() — the two are, for now, deliberately independent
// implementations of the same algorithm (design doc §7.2/Appendix C.1's
// D1-D5 decisions and §11 D3: report keeps its own grouping until Phase 3
// migrates it onto ctxgraph). Duplicating the algorithm without a way to
// catch drift would silently reintroduce the exact bug class Phase 3 exists
// to retire, so this test exists NOW rather than waiting for Phase 3's
// formal conformance suite.
//
// This import is safe for internal/archtest's report-package boundary
// rule: `go list -deps` (no -test flag, which is what that test uses) does
// not include a package's _test.go-only imports, so report's production
// code remains exactly as decoupled from ctxgraph as it was before this
// file existed.
package report

import (
	"testing"
	"time"

	"vmr/internal/audit"
	"vmr/internal/ctxgraph"
)

// assertManifestParity feeds one record through both collect() (this
// package's own grouping input) and ctxgraph.BuildManifest, and requires
// their message-hash vectors to agree exactly.
func assertManifestParity(t *testing.T, rec *audit.Record, path string, line int) {
	t.Helper()
	ri := collect(rec, path, line)
	m, ok := ctxgraph.BuildManifest(rec, path, line)
	if !ok {
		// ctxgraph couldn't parse a chat body — collect() must agree that
		// there was nothing to parse (its own early-return leaves
		// MessagesKnown at its zero value).
		if ri.MessagesKnown != 0 {
			t.Fatalf("collect() parsed a chat body (MessagesKnown=%d) but ctxgraph.BuildManifest did not", ri.MessagesKnown)
		}
		return
	}
	if len(ri.keys) != len(m.Keys) {
		t.Fatalf("key count mismatch: collect()=%d ctxgraph=%d", len(ri.keys), len(m.Keys))
	}
	for i := range ri.keys {
		if ri.keys[i] != m.Keys[i].String() {
			t.Errorf("key[%d] mismatch: collect()=%s ctxgraph=%s", i, ri.keys[i], m.Keys[i].String())
		}
	}
	if m.HasSys {
		if ri.sysKey != m.SysHash.String() {
			t.Errorf("sysKey mismatch (HasSys=true): collect()=%s ctxgraph=%s", ri.sysKey, m.SysHash.String())
		}
		return
	}
	// Deliberate, harmless divergence: collect()'s sysHash is an
	// md5.New() writer that always gets Sum()'d, even when zero leading
	// system messages were ever written to it — so a system-less
	// request's sysKey is md5(""), a well-known constant, not the empty/
	// zero value ctxgraph uses for "no system block at all". Nothing in
	// either package currently compares a system-less sysKey against
	// anything meaningful (it only ever feeds SysChanged, a same-lineage
	// boolean), so this costs nothing today — asserted here so Phase 3's
	// migration doesn't rediscover it as a mystery.
	const md5OfEmptyString = "d41d8cd98f00b204e9800998ecf8427e"
	if ri.sysKey != md5OfEmptyString {
		t.Errorf("collect()'s no-system sysKey = %s, want the well-known md5(\"\") constant %s (has this hashing changed?)", ri.sysKey, md5OfEmptyString)
	}
}

func TestCtxgraphParity_SyntheticFixture(t *testing.T) {
	_, recs := fixture(t)
	for i := range recs {
		assertManifestParity(t, &recs[i], "fixture.jsonl", i+1)
	}
}

func TestCtxgraphParity_HandBuiltShapes(t *testing.T) {
	ts := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	anthropicTopLevelSystem := func() audit.Record {
		r := mkRec(ts, "", []any{msg("user", "hi")}, nil, sseText("hello"))
		body := r.Client.Request.Body.(map[string]any)
		body["system"] = "top-level sys"
		return r
	}()
	cases := []struct {
		name string
		rec  audit.Record
	}{
		{"no_system", mkRec(ts, "", []any{msg("user", "hi")}, nil, sseText("hello"))},
		{"single_system", mkRec(ts, "", []any{msg("system", "sys"), msg("user", "hi")}, nil, sseText("hello"))},
		{"anthropic_top_level_system", anthropicTopLevelSystem},
		{"tool_call_and_result", mkRec(ts, "", []any{
			msg("system", "sys"),
			msg("user", "run it"),
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{"id": "c1", "function": map[string]any{"name": "exec", "arguments": "{}"}},
			}},
			map[string]any{"role": "tool", "tool_call_id": "c1", "content": "ok"},
		}, nil, sseText("done"))},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertManifestParity(t, &cases[i].rec, "shapes.jsonl", i+1)
		})
	}
}
