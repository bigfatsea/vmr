// Ver 2026-09-14, by Sonnet 5

package guard

import (
	"bytes"
	"strings"
	"testing"
	"testing/quick"
)

// TestACAutomaton_MatchesBytesContains is the prefilter's correctness
// ground truth: for any text, the automaton must flag rule i as hit if
// and only if bytes.Contains(text, rules[i].Literal) says so — the
// property scanValue's speedup depends on holding exactly, not
// approximately (a false negative here would silently blind Scan to a
// real credential; the entropy/regex stage downstream never runs for a
// rule the prefilter didn't flag).
func TestACAutomaton_MatchesBytesContains(t *testing.T) {
	rules := DefaultRules()
	lits := make([][]byte, len(rules))
	for i, r := range rules {
		lits[i] = r.Literal
	}
	ac := newACAutomaton(lits)

	f := func(text []byte) bool {
		hits := make([]bool, len(rules))
		ac.match(text, hits)
		for i, lit := range lits {
			want := bytes.Contains(text, lit)
			if hits[i] != want {
				t.Errorf("rule %d (%q): automaton=%v, bytes.Contains=%v, text=%q", i, lit, hits[i], want, text)
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Errorf("quick.Check found a mismatch: %v", err)
	}
}

// TestACAutomaton_OverlappingLiterals exercises the fail-link/suffix-merge
// path directly: one literal that is a strict suffix of another (so a
// match ending at the longer literal's node must also report the shorter
// one via its fail chain), and literals sharing a common prefix.
func TestACAutomaton_OverlappingLiterals(t *testing.T) {
	lits := [][]byte{[]byte("sk-ant-api03-"), []byte("api03-"), []byte("sk-"), []byte("ghp_")}
	ac := newACAutomaton(lits)

	cases := []struct {
		text []byte
		want []bool
	}{
		{[]byte("prefix sk-ant-api03-XXXX suffix"), []bool{true, true, true, false}},
		{[]byte("just ghp_XXXX here"), []bool{false, false, false, true}},
		{[]byte("nothing relevant"), []bool{false, false, false, false}},
		{[]byte("api03- alone, no sk- and no ghp_ prefix... wait sk- is here"), []bool{false, true, true, true}},
	}
	for _, c := range cases {
		hits := make([]bool, len(lits))
		ac.match(c.text, hits)
		for i := range lits {
			if hits[i] != c.want[i] {
				t.Errorf("text=%q lit=%q: got %v, want %v", c.text, lits[i], hits[i], c.want[i])
			}
		}
	}
}

// TestACAutomaton_EmptyText confirms the zero-length input never panics
// and reports no hits (acRoot has no outputs).
func TestACAutomaton_EmptyText(t *testing.T) {
	rules := DefaultRules()
	lits := make([][]byte, len(rules))
	for i, r := range rules {
		lits[i] = r.Literal
	}
	ac := newACAutomaton(lits)
	hits := make([]bool, len(rules))
	ac.match(nil, hits)
	for i, h := range hits {
		if h {
			t.Errorf("empty text matched rule %d", i)
		}
	}
}

// benchNoHitJSON builds a ~1 MiB JSON document shaped like a realistic
// multi-turn chat payload with no credential-shaped content anywhere —
// the overwhelmingly common case in real traffic (§2.3's corpus baseline)
// and the one both BenchmarkOutboundPrefilter and BenchmarkOutboundScan's
// throughput numbers must be measured against.
func benchNoHitJSON(targetBytes int) []byte {
	var b strings.Builder
	b.WriteString(`{"messages":[`)
	para := "the quick brown fox jumps over the lazy dog while reviewing pull request number forty two and running go test across the repository, "
	first := true
	for b.Len() < targetBytes {
		if !first {
			b.WriteByte(',')
		}
		first = false
		b.WriteString(`{"role":"user","content":[{"type":"text","text":"`)
		b.WriteString(strings.Repeat(para, 8))
		b.WriteString(`"}]}`)
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// BenchmarkOutboundPrefilter measures the Aho-Corasick literal prefilter
// in isolation (design spec §4.8's Level-1 throughput gate: >=1 GB/s,
// 0 allocs/op) against a 1 MiB no-hit buffer -- the automaton is built
// once outside the timed loop, matching Engine's "immutable, built once"
// contract.
func BenchmarkOutboundPrefilter(b *testing.B) {
	rules := DefaultRules()
	lits := make([][]byte, len(rules))
	for i, r := range rules {
		lits[i] = r.Literal
	}
	ac := newACAutomaton(lits)
	text := benchNoHitJSON(1 << 20)
	hits := make([]bool, len(rules))
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := range hits {
			hits[j] = false
		}
		ac.match(text, hits)
	}
}

// BenchmarkOutboundScan measures the full Engine.Scan pipeline (JSON walk
// + prefilter + entropy/regex confirmation) against a ~1 MiB document
// containing exactly three real credential hits, per §4.8's
// BenchmarkOutboundScan gate (p95 <= 2ms, <= hits x 1 allocs -- allocation
// count is read from -benchmem output, not asserted here; Go's testing.B
// has no per-run alloc gate, only an aggregate).
func BenchmarkOutboundScan(b *testing.B) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		b.Fatalf("NewEngine: %v", err)
	}
	base := benchNoHitJSON(1 << 20)
	// Splice three real credential hits into otherwise-clean text, each
	// extending an existing JSON string value's content (inserted just
	// before its closing quote, never replacing that quote itself — doing
	// so would leave a stray, unquoted span and desync the JSON walker,
	// silently moving everything after it outside any recognized string
	// value). The pattern boundary `"}]},{"role"` starts at that closing
	// quote, so the replacement keeps it as-is and only prepends text.
	doc := strings.Replace(string(base), `"}]},{"role"`,
		` and AKIA`+repUpperDigit(16)+`"}]},{"role"`, 1)
	doc = strings.Replace(doc, `"}]},{"role"`,
		` and ghp_`+repAlnum(36)+`"}]},{"role"`, 1)
	doc = strings.Replace(doc, `"}]},{"role"`,
		` and hf_`+repAlnum(34)+`"}]},{"role"`, 1)
	docBytes := []byte(doc)
	sc := NewScratch(len(DefaultRules()))
	if got := e.Scan(docBytes, sc); len(got) != 3 {
		b.Fatalf("fixture setup: got %d hits, want 3: %+v", len(got), got)
	}
	b.SetBytes(int64(len(docBytes)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Scan(docBytes, sc)
	}
}

// BenchmarkRuneSanitize measures ClassifyRunes against 1 MiB of pure-ASCII
// text (§4.8's gate: >=2 GB/s, 0 allocs/op) -- the fast path real traffic
// takes almost always, per §2.3's corpus baseline.
func BenchmarkRuneSanitize(b *testing.B) {
	text := []byte(strings.Repeat("the quick brown fox jumps over the lazy dog, ", 1<<15)) // ~1.4 MiB of pure ASCII
	var dst map[string]int
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst = ClassifyRunes(text, dst)
	}
}
