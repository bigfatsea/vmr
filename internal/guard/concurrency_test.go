// Ver 2026-09-14, by Sonnet 5

// Concurrency-safety tests for the Engine/Scratch split (M3.0). These are
// the M1/M3 acceptance criterion the design spec's §5 calls for: N
// goroutines sharing one *Engine, each holding its own *Scratch,
// concurrently scanning the same corpus, must produce results identical
// to a serial scan of the same documents one at a time.
package guard

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// concurrencyCorpus builds a fixed set of JSON documents mixing clean
// text, single credential hits, and multi-hit/amplified documents — enough
// variety that a prefilter or path-tracking bug tied to a specific rule or
// nesting shape would show up as a mismatch against the serial baseline.
func concurrencyCorpus(t *testing.T) [][]byte {
	t.Helper()
	var docs [][]byte
	add := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		docs = append(docs, b)
	}
	for i := 0; i < 20; i++ {
		add(map[string]string{"text": fmt.Sprintf("nothing interesting here, iteration %d", i)})
	}
	add(map[string]string{"text": "AKIA" + repUpperDigit(16)})
	add(map[string]string{"text": "sk-ant-api03-" + repAlnum(93)})
	add(map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "ghp_" + repAlnum(36) + " and again ghp_" + repAlnum(36)},
		}},
	}})
	add(map[string]string{"text": "task-specific ask-user-question disk-cache-size, all clean"})
	add(map[string]any{"a": []any{1, 2, map[string]any{"b": "hf_" + repAlnum(34)}}})
	return docs
}

// serialFindings scans every doc in corpus with a fresh Engine/Scratch
// pair per call (mirroring TestEngine_Determinism's "no aliasing across
// calls" caution) and returns each document's findings as an independent
// copy, safe to compare after the fact.
func serialFindings(t *testing.T, corpus [][]byte) [][]Finding {
	t.Helper()
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	out := make([][]Finding, len(corpus))
	for i, doc := range corpus {
		got := e.Scan(doc, NewScratch(len(DefaultRules())))
		out[i] = append([]Finding(nil), got...)
	}
	return out
}

// TestEngine_ConcurrentScanMatchesSerial is M3.0's concurrency-safety
// acceptance criterion: one Engine shared by many goroutines (each with
// its own Scratch) must find exactly what a serial scan finds, every
// time, under -race.
func TestEngine_ConcurrentScanMatchesSerial(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	corpus := concurrencyCorpus(t)
	want := serialFindings(t, corpus)

	const goroutines = 8
	const roundsPerGoroutine = 25
	var wg sync.WaitGroup
	errs := make(chan string, goroutines*roundsPerGoroutine*len(corpus))
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			sc := NewScratch(len(DefaultRules()))
			for round := 0; round < roundsPerGoroutine; round++ {
				for i, doc := range corpus {
					got := e.Scan(doc, sc)
					if !findingsEqual(got, want[i]) {
						errs <- fmt.Sprintf("goroutine %d round %d doc %d: got %+v, want %+v", g, round, i, got, want[i])
					}
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}
}

func findingsEqual(a, b []Finding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
