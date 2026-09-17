// Ver 2026-09-13, by Sonnet 5

package guard

import (
	"encoding/json"
	"testing"
)

// FuzzScan is the M1 acceptance criterion's fuzz target: Scan must never
// panic on arbitrary bytes, run via `go test -fuzz=FuzzScan ./internal/guard`
// for the >=60s no-crash budget the design spec's M1 acceptance calls for.
// `go test` (no -fuzz) still replays the seed corpus below on every run.
func FuzzScan(f *testing.F) {
	seeds := [][]byte{
		nil,
		[]byte(`{}`),
		[]byte(`{"a":"AKIA` + repUpperDigit(16) + `"}`),
		[]byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"sk-ant-api03-` + repAlnum(93) + `"}]}]}`),
		[]byte(`{"a":[1,2,3,{"b":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"}]}`),
		[]byte(`not json`),
		[]byte(`{"unterminated`),
		[]byte(`[[[[[[[[[[`),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		f.Fatalf("NewEngine: %v", err)
	}
	sc := NewScratch(len(DefaultRules()))
	f.Fuzz(func(t *testing.T, data []byte) {
		e.Scan(data, sc)
	})
}

// FuzzScanValidJSON narrows the fuzz search to well-formed JSON documents
// (wrapping arbitrary fuzzer-supplied text into a string value) — the
// shape Scan actually sees in production, complementing FuzzScan's
// arbitrary-byte-garbage coverage above.
func FuzzScanValidJSON(f *testing.F) {
	f.Add("hello")
	f.Add("AKIA" + repUpperDigit(16))
	f.Add("sk-ant-api03-" + repAlnum(93))
	f.Add("task-specific ask-user-question disk-cache-size")
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		f.Fatalf("NewEngine: %v", err)
	}
	sc := NewScratch(len(DefaultRules()))
	f.Fuzz(func(t *testing.T, s string) {
		doc, err := json.Marshal(map[string]string{"text": s})
		if err != nil {
			t.Skip()
		}
		e.Scan(doc, sc)
	})
}
