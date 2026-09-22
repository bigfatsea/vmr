// Ver 2026-09-20 11:58, by Sonnet 5

// Provider-level concurrency gate and registry. Limits in-flight requests per
// provider account, supporting fast non-blocking skip for new sessions and
// bounded queue waiting for sticky sessions to preserve prompt cache locality.
package router

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"vmr/internal/config"
	"vmr/internal/core"
)

// ProviderConcurrencyStats captures the instantaneous concurrency state for one provider.
type ProviderConcurrencyStats struct {
	Limit    int   `json:"limit"`
	InFlight int64 `json:"in_flight"`
	Waiting  int64 `json:"waiting"`
}

// ProviderLimiter manages the concurrent execution gate for a single provider.
type ProviderLimiter struct {
	name      string
	cap       int           // 0 = unlimited
	queueWait time.Duration // default queue wait timeout for sticky sessions
	sem       chan struct{} // semaphore channel
	inFlight  atomic.Int64
	waiting   atomic.Int64
}

// NewProviderLimiter constructs a concurrency gate for one provider account.
func NewProviderLimiter(name string, capacity int, queueWait time.Duration) *ProviderLimiter {
	if capacity <= 0 {
		return &ProviderLimiter{name: name, cap: 0, queueWait: 0}
	}
	return &ProviderLimiter{
		name:      name,
		cap:       capacity,
		queueWait: queueWait,
		sem:       make(chan struct{}, capacity),
	}
}

// Capacity returns the maximum configured concurrency (0 = unlimited).
func (l *ProviderLimiter) Capacity() int {
	if l == nil {
		return 0
	}
	return l.cap
}

// QueueWait returns the configured queue wait timeout for sticky sessions.
func (l *ProviderLimiter) QueueWait() time.Duration {
	if l == nil {
		return 0
	}
	return l.queueWait
}

// InFlight returns the current active in-flight requests.
func (l *ProviderLimiter) InFlight() int64 {
	if l == nil {
		return 0
	}
	return l.inFlight.Load()
}

// Waiting returns the number of requests currently queued.
func (l *ProviderLimiter) Waiting() int64 {
	if l == nil {
		return 0
	}
	return l.waiting.Load()
}

// Stats returns the instantaneous concurrency metrics for /stats.
func (l *ProviderLimiter) Stats() ProviderConcurrencyStats {
	if l == nil {
		return ProviderConcurrencyStats{}
	}
	return ProviderConcurrencyStats{
		Limit:    l.cap,
		InFlight: l.inFlight.Load(),
		Waiting:  l.waiting.Load(),
	}
}

// TryAcquire attempts to occupy a concurrency slot non-blockingly.
// Used for new sessions (non-sticky) to achieve zero-latency Fast-Skip.
func (l *ProviderLimiter) TryAcquire() (release func(), ok bool) {
	if l == nil || l.cap <= 0 {
		return func() {}, true
	}
	select {
	case l.sem <- struct{}{}:
		l.inFlight.Add(1)
		var once sync.Once
		return func() {
			once.Do(func() {
				l.inFlight.Add(-1)
				<-l.sem
			})
		}, true
	default:
		return nil, false
	}
}

// AcquireWithTimeout attempts to occupy a slot within the specified timeout.
// Used for sticky sessions to preserve upstream prompt cache.
func (l *ProviderLimiter) AcquireWithTimeout(ctx context.Context, timeout time.Duration) (release func(), waited time.Duration, ok bool) {
	if l == nil || l.cap <= 0 {
		return func() {}, 0, true
	}
	// Fast path: immediate acquisition without timer overhead
	if rel, acquired := l.TryAcquire(); acquired {
		return rel, 0, true
	}
	if timeout <= 0 {
		return nil, 0, false
	}

	start := time.Now()
	l.waiting.Add(1)
	defer l.waiting.Add(-1)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case l.sem <- struct{}{}:
		l.inFlight.Add(1)
		var once sync.Once
		return func() {
			once.Do(func() {
				l.inFlight.Add(-1)
				<-l.sem
			})
		}, time.Since(start), true
	case <-timer.C:
		return nil, time.Since(start), false
	case <-ctx.Done():
		return nil, time.Since(start), false
	}
}

// ProviderLimiterRegistry tracks all per-provider concurrency gates.
// Lock-free read path via atomic pointer with mutex-guarded copy-on-write.
type ProviderLimiterRegistry struct {
	mu       sync.Mutex
	limiters atomic.Pointer[map[string]*ProviderLimiter]
}

// NewProviderLimiterRegistry initializes an empty registry.
func NewProviderLimiterRegistry() *ProviderLimiterRegistry {
	r := &ProviderLimiterRegistry{}
	m := make(map[string]*ProviderLimiter)
	r.limiters.Store(&m)
	return r
}

// Get returns the limiter for provider, or nil if unconstrained.
func (r *ProviderLimiterRegistry) Get(provider string) *ProviderLimiter {
	if r == nil {
		return nil
	}
	m := r.limiters.Load()
	if m == nil {
		return nil
	}
	return (*m)[provider]
}

// Install updates the registry with the providers declared in snapshot.
// Reuses running limiters whose capacity and queue duration have not changed,
// so hot reloads preserve in-flight semaphores.
func (r *ProviderLimiterRegistry) Install(providers []config.Provider) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	cur := r.limiters.Load()
	next := make(map[string]*ProviderLimiter, len(providers))

	for _, p := range providers {
		if p.Concurrency <= 0 {
			continue
		}
		queueWait := p.ResolvedConcurrencyQueue()
		// Hot-reload reuse: keep running semaphore if capacity and queue match
		if cur != nil {
			if existing := (*cur)[p.Name]; existing != nil {
				if existing.Capacity() == p.Concurrency && existing.QueueWait() == queueWait {
					next[p.Name] = existing
					continue
				}
			}
		}
		next[p.Name] = NewProviderLimiter(p.Name, p.Concurrency, queueWait)
	}
	r.limiters.Store(&next)
}

// Snapshot returns the instantaneous metrics of all active limiters.
func (r *ProviderLimiterRegistry) Snapshot() map[string]ProviderConcurrencyStats {
	if r == nil {
		return nil
	}
	m := r.limiters.Load()
	if m == nil {
		return nil
	}
	out := make(map[string]ProviderConcurrencyStats, len(*m))
	for name, l := range *m {
		if l != nil {
			out[name] = l.Stats()
		}
	}
	return out
}

// acquireProviderSlot attempts to occupy an in-flight slot for candidate ep.
// Sticky hits wait up to ep.ConcurrencyQueue; non-sticky hits try non-blockingly (Fast-Skip).
func (rt *Router) acquireProviderSlot(ctx context.Context, ep *core.Endpoint, isStickyHit bool) (release func(), waited time.Duration, timedOut bool, ok bool) {
	if rt.ProviderLimiters == nil {
		return nil, 0, false, true
	}
	l := rt.ProviderLimiters.Get(ep.Provider)
	if l == nil || l.Capacity() <= 0 {
		return nil, 0, false, true
	}
	waitTimeout := ep.ConcurrencyQueue
	if isStickyHit && waitTimeout > 0 {
		rel, w, acquired := l.AcquireWithTimeout(ctx, waitTimeout)
		if !acquired {
			timedOut := ctx.Err() == nil
			return nil, w, timedOut, false
		}
		return rel, w, false, true
	}
	rel, acquired := l.TryAcquire()
	if !acquired {
		return nil, 0, false, false
	}
	return rel, 0, false, true
}
