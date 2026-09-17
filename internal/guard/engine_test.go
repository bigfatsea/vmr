// Ver 2026-09-17, by Sonnet 5

package guard

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"testing/quick"
)

func TestNewEngine_RejectsEmpty(t *testing.T) {
	if _, err := NewEngine(nil, 1); err == nil {
		t.Error("NewEngine(nil, ...) should error")
	}
}

func TestNewEngine_RejectsDuplicateName(t *testing.T) {
	r, err := NewRule("dup", Tier2, "xx", "xx[0-9]{4}", 0)
	if err != nil {
		t.Fatalf("NewRule: %v", err)
	}
	if _, err := NewEngine([]Rule{r, r}, 1); err == nil {
		t.Error("NewEngine with duplicate rule names should error")
	}
}

// TestNewRule_RejectsBodyNotStartingWithLiteral is the structural guard for
// scanValue's entropy calculation (engine.go): it slices body[len(r.Literal):]
// to compute the credential body's entropy tail, which silently miscomputes
// if a rule's Literal isn't actually the prefix of what its own pattern
// matches. Every DefaultRules entry satisfies this by construction; this
// catches a future rule author who doesn't.
func TestNewRule_RejectsBodyNotStartingWithLiteral(t *testing.T) {
	if _, err := NewRule("mismatched", Tier2, "abc", "xyz[0-9]{4}", 0); err == nil {
		t.Error("NewRule should reject a body pattern that doesn't start with its own literal prefix")
	}
}

// TestNewEngine_RejectsMissingBoundaryAnchor is the structural guard for
// ADR-5's most important, previously-missed requirement: a Tier1 rule
// built by hand (bypassing NewRule) without the left-boundary anchor must
// be rejected at construction time, not silently accepted to reproduce
// the ~8,000-false-hit regression §2.3 found.
func TestNewEngine_RejectsMissingBoundaryAnchor(t *testing.T) {
	bad := Rule{
		Name:       "unanchored",
		Tier:       Tier1,
		Literal:    []byte("sk-"),
		Re:         regexp.MustCompile(`(sk-[A-Za-z0-9]{8,})`), // no leftBoundary prefix
		MinEntropy: 3.5,
	}
	if _, err := NewEngine([]Rule{bad}, 1); err == nil {
		t.Error("NewEngine should reject a Tier1 rule missing the left-boundary anchor")
	}
}

// TestNewEngine_Tier2WithoutAnchorAllowed documents that the anchor check
// is Tier1-only at construction time — Tier2 rules still get the anchor in
// practice via NewRule/DefaultRules, but NewEngine's structural gate exists
// specifically to protect the tier the spec promises is safe to trust.
func TestNewEngine_Tier2WithoutAnchorAllowed(t *testing.T) {
	bad := Rule{
		Name:       "loose-tier2",
		Tier:       Tier2,
		Literal:    []byte("xx"),
		Re:         regexp.MustCompile(`(xx[0-9]{4})`),
		MinEntropy: 0,
	}
	if _, err := NewEngine([]Rule{bad}, 1); err != nil {
		t.Errorf("NewEngine should not reject an unanchored Tier2 rule: %v", err)
	}
}

// TestScan_NilOnNoHit confirms Scan returns a nil/empty result (not a
// spurious empty-but-non-nil-with-garbage slice) when nothing matches.
func TestScan_NilOnNoHit(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, _ := json.Marshal(map[string]string{"text": "nothing interesting here"})
	got := e.Scan(doc, NewScratch(len(DefaultRules())))
	if len(got) != 0 {
		t.Errorf("Scan on clean text returned %d findings, want 0: %+v", len(got), got)
	}
}

// TestScan_MalformedJSON confirms a malformed/truncated buffer never
// panics — Scan degrades to "stop early", not a crash (guard.walkStrings'
// documented failure mode).
func TestScan_MalformedJSON(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	inputs := [][]byte{
		nil, {}, []byte(`{`), []byte(`{"a":`), []byte(`{"a":"AKIA`),
		[]byte(`[1,2,`), []byte(`"unterminated`), []byte(`not json at all`),
	}
	sc := NewScratch(len(DefaultRules()))
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Scan(%q) panicked: %v", in, r)
				}
			}()
			e.Scan(in, sc)
		}()
	}
}

// TestScan_DstReuse confirms a Scratch's result buffer is reused
// (appended to, truncated on the next call) rather than a fresh slice
// always being allocated, matching the design spec's "Finding 复用调用方
// 提供的 slice" contract — now expressed as "the same *Scratch reused
// across calls", since dst lives inside Scratch rather than being passed
// per call (engine.go's Engine/Scratch split, M3.0).
func TestScan_DstReuse(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	sc := NewScratch(len(DefaultRules()))
	doc, _ := json.Marshal(map[string]string{"text": "AKIA" + repUpperDigit(16)})
	got := e.Scan(doc, sc)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1", len(got))
	}
	// Reusing the same Scratch across calls must not corrupt a previous
	// call's already-read-out results in a way that changes the count on a
	// fresh, independent scan.
	doc2, _ := json.Marshal(map[string]string{"text": "clean"})
	got2 := e.Scan(doc2, sc)
	if len(got2) != 0 {
		t.Errorf("second Scan reusing sc got %d findings, want 0", len(got2))
	}
}

// TestScan_DeepNesting exercises deeply nested JSON to confirm the
// walker's maxWalkDepth guard stops recursion cleanly instead of
// overflowing the goroutine stack.
func TestScan_DeepNesting(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	depth := maxWalkDepth + 100
	var b []byte
	for i := 0; i < depth; i++ {
		b = append(b, '[')
	}
	b = append(b, []byte(`"AKIA`+repUpperDigit(16)+`"`)...)
	for i := 0; i < depth; i++ {
		b = append(b, ']')
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Scan panicked on deeply nested input: %v", r)
		}
	}()
	e.Scan(b, NewScratch(len(DefaultRules())))
}

// TestScan_QuickNoPanic is a lightweight property test (testing/quick,
// stdlib-only — no new dependency): Scan must never panic on arbitrary
// byte slices. Full-blown fuzzing (go test -fuzz) lives in FuzzScan below;
// this variant runs unconditionally under `go test`.
func TestScan_QuickNoPanic(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	// Deliberately no recover(): a panic here IS the test failure, and Go's
	// own crash report (with quick.Check's failing input still visible via
	// -v) is more actionable than swallowing it into a generic error.
	sc := NewScratch(len(DefaultRules()))
	f := func(b []byte) bool {
		e.Scan(b, sc)
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Errorf("quick.Check found a panicking input: %v", err)
	}
}

// TestScan_AllocsOnNoHitPath characterizes (not yet a hard CI gate — that
// lands with M3, when Scan actually sits on the request path, see the
// design spec's §4.8) the no-hit path's allocation cost on a realistic,
// deeply-keyed, no-match document. It should be zero: the prefilter
// automaton's literal pre-check runs directly over each string value's raw
// bytes with no decoding or tracking of where in the document it sits, and
// the reused Scratch carries no per-call allocation of its own.
func TestScan_AllocsOnNoHitPath(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	var messages []map[string]any
	for i := 0; i < 50; i++ {
		messages = append(messages, map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": strings.Repeat("the quick brown fox jumps over the lazy dog ", 20)},
			},
		})
	}
	doc, err := json.Marshal(map[string]any{"messages": messages})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// One warmup call (against a Scratch reused for the whole test, per the
	// new Engine/Scratch split) so its stack/dst/acHits slices grow to
	// their steady-state capacity before measurement — AllocsPerRun would
	// otherwise (fairly) count that one-time growth against the first call.
	sc := NewScratch(len(DefaultRules()))
	e.Scan(doc, sc)
	n := testing.AllocsPerRun(100, func() {
		e.Scan(doc, sc)
	})
	if n != 0 {
		t.Errorf("Scan on a no-hit document allocated %.1f times per run, want 0", n)
	}
}

// TestScanText_PlainString verifies ScanText finds a credential in plain
// decoded text (not JSON) — the shape a reassembled assistant message or
// unescaped tool-call argument arrives in (M1.3).
func TestScanText_PlainString(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	cred := "AIza" + strings.Repeat("Ab3F9zK1qT7xM0cR", 3)[:35] // 35 chars, mixed enough to clear the entropy gate
	text := "sure, here is the key: " + cred
	found := e.ScanText([]byte(text), NewScratch(len(DefaultRules())))
	if len(found) != 1 || found[0].Rule != "gcp-api-key" {
		t.Fatalf("ScanText(%q) = %+v, want one gcp-api-key hit", text, found)
	}
	if got := string(found[0].Body([]byte(text))); got != cred {
		t.Errorf("ScanText Finding.Body = %q, want %q", got, cred)
	}
}

// TestScanText_NoHit confirms ScanText doesn't just return everything.
func TestScanText_NoHit(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if found := e.ScanText([]byte("just a normal sentence, ask-user-question style"), NewScratch(len(DefaultRules()))); len(found) != 0 {
		t.Errorf("ScanText on clean text = %+v, want none", found)
	}
}

// TestEngine_ScanVsScanText_EscapedCredentialDifferential locks in K-G16:
// Online Scan operates on raw JSON bytes without \u unescaping (zero-allocation hot path),
// so a credential whose literal prefix is \u-escaped (e.g. \u0041KIA for AKIA)
// does NOT hit Scan. However, once decoded (as in offline report's ScanText), it hits.
func TestEngine_ScanVsScanText_EscapedCredentialDifferential(t *testing.T) {
	e, err := NewEngine(DefaultRules(), RulesVersion)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	rawKey := "AKIA" + repUpperDigit(16)
	escapedKey := `\u0041KIA` + repUpperDigit(16)

	// 1. Online Scan on raw JSON bytes containing escapedKey: misses (by design per K-G16)
	jsonDoc := []byte(`{"text":"` + escapedKey + `"}`)
	onlineHits := e.Scan(jsonDoc, NewScratch(len(DefaultRules())))
	if len(onlineHits) != 0 {
		t.Errorf("Scan on escaped JSON = %+v, want 0 hits (K-G16 zero-alloc raw-byte scan)", onlineHits)
	}

	// 2. Scan on raw JSON bytes containing rawKey: hits
	rawDoc := []byte(`{"text":"` + rawKey + `"}`)
	rawHits := e.Scan(rawDoc, NewScratch(len(DefaultRules())))
	if len(rawHits) != 1 || rawHits[0].Rule != "aws-access-key" {
		t.Fatalf("Scan on raw JSON = %+v, want 1 aws-access-key hit", rawHits)
	}

	// 3. Offline decoded text (ScanText) on decoded bytes: hits
	decodedText := []byte(rawKey)
	offlineHits := e.ScanText(decodedText, NewScratch(len(DefaultRules())))
	if len(offlineHits) != 1 || offlineHits[0].Rule != "aws-access-key" {
		t.Fatalf("ScanText on decoded text = %+v, want 1 aws-access-key hit", offlineHits)
	}
}
