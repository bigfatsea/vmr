// Ver 2026-07-26, by Sonnet 5
package adapter

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"vmr/internal/core"
)

// fakeAdapter is a minimal Adapter used only to exercise Register/Get/Names
// under concurrent load — it never builds a real request.
type fakeAdapter struct{ protocol string }

func (f fakeAdapter) Protocol() string              { return f.protocol }
func (f fakeAdapter) ResolveURL(base string) string { return base }
func (f fakeAdapter) BuildRequest(context.Context, *core.Endpoint, *core.CanonicalRequest) (*http.Request, []byte, error) {
	return nil, nil, nil
}
func (f fakeAdapter) ClassifyError(int, []byte) core.ErrorClass { return core.ErrTransient }

// TestGetConcurrentWithRegister exercises the atomic.Pointer copy-on-write
// registry under `go test -race`: N goroutines calling Get/Names while a
// handful of distinct names are Register()'d must never race and Get must
// always see either "not yet registered" or the fully-registered adapter,
// never a partially-constructed one.
func TestGetConcurrentWithRegister(t *testing.T) {
	const names = 8
	var wg sync.WaitGroup

	// Registration happens on its own goroutines too — in production every
	// real Register call happens sequentially inside init(), but the
	// registry's own correctness must not secretly depend on that; this
	// proves the copy-on-write path itself is race-free under true
	// concurrent writers, a strictly harder case than the real one.
	for i := 0; i < names; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			Register(concurrentTestName(i), fakeAdapter{protocol: concurrentTestName(i)})
		}(i)
	}

	readerDone := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			Names()
			for j := 0; j < names; j++ {
				if a, ok := Get(concurrentTestName(j)); ok && a.Protocol() != concurrentTestName(j) {
					t.Errorf("Get(%s) returned mismatched adapter %+v", concurrentTestName(j), a)
				}
			}
		}
		close(readerDone)
	}()

	wg.Wait()
	<-readerDone

	for i := 0; i < names; i++ {
		if _, ok := Get(concurrentTestName(i)); !ok {
			t.Errorf("Get(%s): want registered after all Register calls completed", concurrentTestName(i))
		}
	}
}

func concurrentTestName(i int) string {
	return "race-test-adapter-" + string(rune('a'+i))
}
