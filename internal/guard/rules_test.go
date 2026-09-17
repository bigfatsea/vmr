// Ver 2026-09-13, by Sonnet 5

package guard

import (
	"encoding/json"
	"math/rand"
	"testing"
)

// TestDefaultRules_Compile guards DefaultRules' own panic-on-bad-spec path:
// every entry in its table must actually compile.
func TestDefaultRules_Compile(t *testing.T) {
	rules := DefaultRules()
	if len(rules) == 0 {
		t.Fatal("DefaultRules returned no rules")
	}
	if _, err := NewEngine(rules, RulesVersion); err != nil {
		t.Fatalf("NewEngine(DefaultRules()): %v", err)
	}
}

// ruleNames collects DefaultRules' names, for the expected-name check below.
func ruleNames(t *testing.T) map[string]bool {
	t.Helper()
	m := map[string]bool{}
	for _, r := range DefaultRules() {
		m[r.Name] = true
	}
	return m
}

func TestDefaultRules_ExpectedNames(t *testing.T) {
	want := []string{
		"anthropic-api-key", "openai-project-key", "openai-legacy-key",
		"gcp-api-key", "aws-access-key", "github-pat", "github-oauth",
		"huggingface-token", "slack-bot-token", "jwt", "private-key-block",
		"generic-sk-prefix",
	}
	have := ruleNames(t)
	for _, name := range want {
		if !have[name] {
			t.Errorf("DefaultRules missing expected rule %q", name)
		}
	}
}

// jsonBody wraps a raw string value into a minimal JSON document, the
// shape Scan actually expects (one JSON document, credential inside a
// string value) rather than a bare fragment.
func jsonBody(t *testing.T, s string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"text": s})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// scanFor runs the default engine over s wrapped in a minimal JSON
// document and returns the rule names that fired.
func scanFor(t *testing.T, s string) map[string]int {
	t.Helper()
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	findings := e.Scan(jsonBody(t, s), NewScratch(len(DefaultRules())))
	out := map[string]int{}
	for _, f := range findings {
		out[f.Rule]++
	}
	return out
}

// TestTier1_TruePositives is one representative real-shaped value per
// Tier1 rule (fixture-only — never a real credential) confirming each
// rule actually fires on the shape it claims to recognize.
func TestTier1_TruePositives(t *testing.T) {
	cases := []struct {
		rule  string
		value string
	}{
		{"anthropic-api-key", "sk-ant-api03-" + repAlnum(93)},
		{"openai-project-key", "sk-proj-" + repAlnum(74)},
		{"openai-legacy-key", "sk-" + repAlnum(48)},
		{"gcp-api-key", "AIza" + repAlnum(35)},
		{"aws-access-key", "AKIA" + repUpperDigit(16)},
		{"github-pat", "ghp_" + repAlnum(36)},
		{"github-oauth", "gho_" + repAlnum(36)},
		{"huggingface-token", "hf_" + repAlnum(34)},
		{"slack-bot-token", "xoxb-" + repDigits(10) + "-" + repDigits(10) + "-" + repAlnum(24)},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"},
		{"private-key-block", "-----BEGIN RSA PRIVATE KEY-----"},
		{"generic-sk-prefix", "sk-" + repAlnum(24)},
	}
	for _, c := range cases {
		t.Run(c.rule, func(t *testing.T) {
			hits := scanFor(t, "prefix text "+c.value+" suffix text")
			if hits[c.rule] == 0 {
				t.Errorf("rule %q did not fire on fixture %q (hits=%v)", c.rule, c.value, hits)
			}
		})
	}
}

// TestTier1_FalsePositives_NaturalLanguage is §2.3's own empirical finding
// reproduced as a permanent regression test: these exact substrings
// (English words ending "sk"/"ask" + a hyphen) must NOT fire ANY Tier1
// rule, and must not fire generic-sk-prefix either now that it carries
// the left-boundary anchor.
func TestTier1_FalsePositives_NaturalLanguage(t *testing.T) {
	phrases := []string{
		"ask-user-question", "Task-specific work item", "disk-cache-size setting",
		"task-fallback logic", "task-oriented design", "risk-averse strategy",
		"desk-based research", "mask-wearing policy", "SK Hynix earnings report",
	}
	for _, p := range phrases {
		t.Run(p, func(t *testing.T) {
			hits := scanFor(t, p)
			for rule, n := range hits {
				t.Errorf("phrase %q spuriously matched rule %q (%d times) — left-boundary anchor regression", p, rule, n)
			}
		})
	}
}

// TestBinaryBlob_Skipped confirms a large base64-blob-shaped value (an
// inline image stand-in) with an embedded credential-shaped substring is
// skipped whole (ADR-5 step 2) rather than scanned.
func TestBinaryBlob_Skipped(t *testing.T) {
	blob := repAlnum(9000) + "AKIA" + repUpperDigit(16) + repAlnum(100)
	hits := scanFor(t, blob)
	if len(hits) != 0 {
		t.Errorf("binary-blob-shaped value should be skipped entirely, got hits=%v", hits)
	}
}

// TestEngine_Determinism confirms repeated Scan calls on the same Engine
// (which reuses internal scratch state) produce identical results.
func TestEngine_Determinism(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc := jsonBody(t, "key is AKIA"+repUpperDigit(16)+" here")
	var first []Finding
	for i := 0; i < 5; i++ {
		// A fresh Scratch per iteration (rather than one reused across the
		// loop) deliberately: Scan's result aliases its Scratch's backing
		// array until the next call on that same Scratch, so reusing one
		// here would let iteration i+1 silently overwrite `first`'s
		// backing array — a real hazard for any caller holding onto a
		// result past its Scratch's next call, not just this test.
		got := e.Scan(doc, NewScratch(len(DefaultRules())))
		if i == 0 {
			first = got
			continue
		}
		if len(got) != len(first) {
			t.Fatalf("run %d: got %d findings, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j] != first[j] {
				t.Errorf("run %d finding %d = %+v, want %+v", i, j, got[j], first[j])
			}
		}
	}
}

// prngString draws n characters from alphabet using a fixed-seed PRNG —
// deterministic across test runs, but genuinely high-entropy (unlike a
// short modular stride, which can collide into a small cycle when len
// (alphabet) shares a factor with the stride and silently fail every
// rule's entropy gate). Never a real credential: fixture-only filler.
func prngString(seed int64, alphabet string, n int) string {
	r := rand.New(rand.NewSource(seed))
	out := make([]byte, n)
	for i := range out {
		out[i] = alphabet[r.Intn(len(alphabet))]
	}
	return string(out)
}

const alnumAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
const upperDigitAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// repAlnum returns a deterministic but high-entropy base62 string of
// exactly n characters — shape-compatible fixture filler, never a real
// credential.
func repAlnum(n int) string { return prngString(1, alnumAlphabet, n) }

func repUpperDigit(n int) string { return prngString(2, upperDigitAlphabet, n) }

func repDigits(n int) string { return prngString(3, "0123456789", n) }
