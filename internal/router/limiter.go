// Ver 2026-07-26, by Sonnet 5

// The global concurrency gate. Split out of router.go (see
// docs/vmr_architecture_review_opus-5.md §3.7/§4.5) — pure move, no
// behavior change.
package router

import "context"

// limiter is the global concurrency gate: a plain semaphore. Requests over
// the limit block in memory (channel send, ~FIFO wakeup) until a slot frees.
type limiter struct {
	sem chan struct{}
	cap int
}

// installLimiter swaps the gate only when the capacity actually changed, so
// hot reloads that don't touch max_concurrency keep the live semaphore.
// During a capacity change, requests holding old slots release into the old
// semaphore — a brief over-admission window, accepted for a local tool.
func (rt *Router) installLimiter(capacity int) {
	cur := rt.limiter.Load()
	if cur != nil && cur.cap == capacity {
		return
	}
	if cur == nil && capacity <= 0 {
		return
	}
	if capacity <= 0 {
		rt.limiter.Store(nil)
		return
	}
	rt.limiter.Store(&limiter{sem: make(chan struct{}, capacity), cap: capacity})
}

// AcquireSlot blocks until a concurrency slot is free (or the client gives
// up). It returns a release func and ok=false when ctx was canceled while
// waiting.
func (rt *Router) AcquireSlot(ctx context.Context) (func(), bool) {
	l := rt.limiter.Load()
	if l == nil {
		rt.inFlight.Add(1)
		return func() { rt.inFlight.Add(-1) }, true
	}
	rt.waiting.Add(1)
	select {
	case l.sem <- struct{}{}:
		rt.waiting.Add(-1)
		rt.inFlight.Add(1)
		return func() {
			rt.inFlight.Add(-1)
			<-l.sem
		}, true
	case <-ctx.Done():
		rt.waiting.Add(-1)
		return nil, false
	}
}

// Concurrency reports the gate state for /admin/status.
func (rt *Router) Concurrency() (limit int, inFlight, waiting int64) {
	if l := rt.limiter.Load(); l != nil {
		limit = l.cap
	}
	return limit, rt.inFlight.Load(), rt.waiting.Load()
}
