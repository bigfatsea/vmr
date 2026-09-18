// Ver 2026-09-12, by pi

package router

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vmr/internal/config"
)

func TestProviderLimiter_TryAcquire(t *testing.T) {
	l := NewProviderLimiter("test", 2, 100*time.Millisecond)

	rel1, ok1 := l.TryAcquire()
	if !ok1 {
		t.Fatal("first acquire failed")
	}
	if l.InFlight() != 1 {
		t.Errorf("inFlight = %d, want 1", l.InFlight())
	}

	rel2, ok2 := l.TryAcquire()
	if !ok2 {
		t.Fatal("second acquire failed")
	}
	if l.InFlight() != 2 {
		t.Errorf("inFlight = %d, want 2", l.InFlight())
	}

	// 3rd should fail immediately
	_, ok3 := l.TryAcquire()
	if ok3 {
		t.Fatal("third acquire should have failed")
	}

	// Release one
	rel1()
	if l.InFlight() != 1 {
		t.Errorf("after release, inFlight = %d, want 1", l.InFlight())
	}

	// Release second
	rel2()
	if l.InFlight() != 0 {
		t.Errorf("after release both, inFlight = %d, want 0", l.InFlight())
	}

	// Repeated release should be no-op
	rel1()
	rel2()
	if l.InFlight() != 0 {
		t.Errorf("after duplicate release, inFlight = %d, want 0", l.InFlight())
	}
}

func TestProviderLimiter_AcquireWithTimeout(t *testing.T) {
	l := NewProviderLimiter("test", 1, 50*time.Millisecond)
	ctx := context.Background()

	rel1, waited1, ok1 := l.AcquireWithTimeout(ctx, 50*time.Millisecond)
	if !ok1 || waited1 != 0 {
		t.Fatalf("first acquire: ok=%v, waited=%v", ok1, waited1)
	}

	// Second acquire should wait and timeout
	start := time.Now()
	_, waited2, ok2 := l.AcquireWithTimeout(ctx, 40*time.Millisecond)
	if ok2 {
		t.Fatal("second acquire should timeout")
	}
	if waited2 < 30*time.Millisecond {
		t.Errorf("waited = %v, expected ~40ms", waited2)
	}
	if elapsed := time.Since(start); elapsed < 35*time.Millisecond {
		t.Errorf("elapsed = %v, expected >= 35ms", elapsed)
	}

	// Third acquire waits and is unblocked by release
	unblockDone := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		rel1()
		close(unblockDone)
	}()

	rel3, waited3, ok3 := l.AcquireWithTimeout(ctx, 150*time.Millisecond)
	if !ok3 {
		t.Fatal("third acquire should succeed after unblock")
	}
	if waited3 < 20*time.Millisecond {
		t.Errorf("waited = %v, expected >= 20ms", waited3)
	}
	<-unblockDone
	rel3()
	if l.InFlight() != 0 {
		t.Errorf("inFlight = %d, want 0", l.InFlight())
	}
}

func TestProviderLimiter_AcquireContextCanceled(t *testing.T) {
	l := NewProviderLimiter("test", 1, 200*time.Millisecond)
	rel, ok := l.TryAcquire()
	if !ok {
		t.Fatal("initial acquire failed")
	}
	defer rel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, _, ok2 := l.AcquireWithTimeout(ctx, 200*time.Millisecond)
	if ok2 {
		t.Fatal("canceled ctx acquire should fail")
	}
	if l.Waiting() != 0 {
		t.Errorf("waiting = %d, want 0", l.Waiting())
	}
}

func TestProviderLimiter_ConcurrencyStress(t *testing.T) {
	const cap = 3
	l := NewProviderLimiter("stress", cap, 50*time.Millisecond)

	var maxInFlight atomic.Int64
	var completed atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var rel func()
			var ok bool
			if id%2 == 0 {
				rel, ok = l.TryAcquire()
			} else {
				rel, _, ok = l.AcquireWithTimeout(context.Background(), 30*time.Millisecond)
			}
			if !ok {
				return
			}
			cur := l.InFlight()
			for {
				old := maxInFlight.Load()
				if cur <= old || maxInFlight.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			rel()
			completed.Add(1)
		}(i)
	}

	wg.Wait()
	if max := maxInFlight.Load(); max > cap {
		t.Errorf("maxInFlight = %d exceeded cap %d", max, cap)
	}
	if l.InFlight() != 0 {
		t.Errorf("final inFlight = %d, want 0 (leak)", l.InFlight())
	}
	if l.Waiting() != 0 {
		t.Errorf("final waiting = %d, want 0", l.Waiting())
	}
}

func TestProviderLimiterRegistry_InstallAndReuse(t *testing.T) {
	reg := NewProviderLimiterRegistry()

	providers1 := []config.Provider{
		{Name: "p1", Concurrency: 5},
		{Name: "p2", Concurrency: 0},
	}
	reg.Install(providers1)

	l1 := reg.Get("p1")
	if l1 == nil || l1.Capacity() != 5 {
		t.Fatalf("p1 limiter invalid: %v", l1)
	}
	if reg.Get("p2") != nil {
		t.Fatal("p2 with 0 concurrency should not have limiter")
	}

	// Reload with same p1 capacity -> must reuse exact pointer
	providers2 := []config.Provider{
		{Name: "p1", Concurrency: 5},
		{Name: "p3", Concurrency: 2},
	}
	reg.Install(providers2)

	l1After := reg.Get("p1")
	if l1After != l1 {
		t.Fatal("l1 instance was not reused across identical reload")
	}
	if l3 := reg.Get("p3"); l3 == nil || l3.Capacity() != 2 {
		t.Fatalf("p3 limiter invalid: %v", l3)
	}
}
