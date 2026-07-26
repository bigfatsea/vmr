// Ver 2026-07-26, by Sonnet 5
package strategy

import (
	"sync"
	"testing"

	"vmr/internal/core"
)

// fakeCondition is a minimal Condition used only to exercise
// RegisterCondition/Eligible/RejectedBy under concurrent load.
type fakeCondition struct{ name string }

func (f fakeCondition) Name() string { return f.name }
func (f fakeCondition) Eligible(*core.Endpoint, core.RequestFacts) bool {
	return true // never rejects — this test is about registry safety, not filtering logic
}

// TestEligibleConcurrentWithRegisterCondition exercises the atomic.Pointer
// copy-on-write conditions registry under `go test -race`: concurrent
// RegisterCondition calls (harder than production's real,
// sequential-from-init() case) racing against concurrent Eligible/RejectedBy
// reads must never race and must never lose a registration (the same class
// of lost-update bug internal/adapter's Register had before its
// registerMu fix — see that package's TestGetConcurrentWithRegister).
func TestEligibleConcurrentWithRegisterCondition(t *testing.T) {
	const n = 8
	var wg sync.WaitGroup
	names := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = "race-test-condition-" + string(rune('a'+i))
	}

	for _, name := range names {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			RegisterCondition(fakeCondition{name: name})
		}(name)
	}

	ep := &core.Endpoint{Provider: "p", Model: "m"}
	readerDone := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			Eligible(ep, core.RequestFacts{})
			RejectedBy(ep, core.RequestFacts{})
		}
		close(readerDone)
	}()

	wg.Wait()
	<-readerDone

	cur := conditions.Load()
	if cur == nil {
		t.Fatal("conditions registry is nil after RegisterCondition calls")
	}
	seen := map[string]bool{}
	for _, c := range *cur {
		seen[c.Name()] = true
	}
	for _, name := range names {
		if !seen[name] {
			t.Errorf("condition %q lost — registry has %d entries, want all %d test conditions present (plus image/tools)", name, len(*cur), n)
		}
	}
}
