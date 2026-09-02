// Ver 2026-09-14, by ox-alpha

package logtee

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRecent_RingOrder locks in Follow's replay-prefix ordering: the
// returned slice is the ring's chronological content, oldest first, capped
// at ring size.
func TestRecent_RingOrder(t *testing.T) {
	tee := New(4)
	for i := range 6 {
		fmt.Fprintf(tee, "line-%d\n", i)
	}
	got, _, cancel := tee.Follow()
	defer cancel()
	want := "line-2,line-3,line-4,line-5"
	if strings.Join(got, ",") != want {
		t.Fatalf("replay = %q, want %q", got, want)
	}
}

func TestWrite_ReturnsLenAndNoError(t *testing.T) {
	tee := New(8)
	n, err := tee.Write([]byte("hello\n"))
	if n != 6 || err != nil {
		t.Fatalf("Write = %d, %v; want 6, nil", n, err)
	}
	got, _, cancel := tee.Follow()
	defer cancel()
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("buffered = %v, want [hello]", got)
	}
}

func TestCancel_StopsDelivery(t *testing.T) {
	tee := New(8)
	_, ch, cancel := tee.Follow()
	cancel()
	if got := tee.Subscribers(); got != 0 {
		t.Fatalf("Subscribers after cancel = %d, want 0", got)
	}
	fmt.Fprint(tee, "after-cancel\n")
	select {
	case line := <-ch:
		t.Fatalf("received %q after cancel, want nothing", line)
	default:
	}
}

func TestSlowConsumer_DropsWithMarker(t *testing.T) {
	tee := New(8)
	_, ch, cancel := tee.Follow()
	defer cancel()

	// Fill the subscriber's channel (subBuffer) and overflow it while the
	// consumer is silent. The writer must not block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range subBuffer + 25 {
			fmt.Fprintf(tee, "burst-%03d\n", i)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Write blocked on a full subscriber channel")
	}

	// The marker is prepended to the next delivery attempt that finds room,
	// so free two slots, then poke one line through: the marker takes the
	// first freed slot and the poked line the second.
	recv(t, ch)
	recv(t, ch)
	fmt.Fprint(tee, "trigger\n")
	var marker string
	for range subBuffer {
		line := recv(t, ch)
		if strings.Contains(line, "dropped") {
			marker = line
			break
		}
	}
	if !strings.HasPrefix(marker, "... dropped ") || !strings.HasSuffix(marker, " lines ...") {
		t.Fatalf("marker = %q, want \"... dropped N lines ...\"", marker)
	}
	if next := recv(t, ch); next != "trigger" {
		t.Fatalf("post-marker line = %q, want \"trigger\"", next)
	}
}

func TestConcurrentWritersAndSubscribers(t *testing.T) {
	tee := New(64)
	_, ch, cancel := tee.Follow()
	defer cancel()

	// Drain concurrently; drops are expected (4 writers vs a 64-slot
	// channel), so assert activity, not exact delivery.
	stop := make(chan struct{})
	var received atomic.Int64
	go func() {
		for {
			select {
			case <-ch:
				received.Add(1)
			case <-stop:
				return
			}
		}
	}()

	var wg sync.WaitGroup
	const writers, perWriter = 4, 100
	for w := range writers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range perWriter {
				fmt.Fprintf(tee, "w%d-%03d\n", w, i)
			}
		}(w)
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond) // let the drain goroutine catch up
	close(stop)
	if received.Load() == 0 {
		t.Fatal("subscriber received nothing")
	}
	buffered, _, cancelBuf := tee.Follow()
	cancelBuf()
	if got := len(buffered); got != 64 {
		t.Fatalf("buffered = %d, want ring cap 64", got)
	}
}

// recv wraps a channel read with a watchdog so a regression can fail the
// test instead of hanging the suite.
func recv(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case line := <-ch:
		return line
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a line")
		return ""
	}
}

func TestNew_PanicsOnNonPositiveCap(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(0) did not panic")
		}
	}()
	New(0)
}

func TestFollow_SnapshotPlusLive(t *testing.T) {
	tee := New(8)
	fmt.Fprint(tee, "old-1\n")

	replay, ch, cancel := tee.Follow()
	defer cancel()
	if len(replay) != 1 || replay[0] != "old-1" {
		t.Fatalf("replay = %v, want [old-1]", replay)
	}

	// A line written after Follow returns must reach the channel (it was
	// neither lost nor duplicated into the snapshot).
	fmt.Fprint(tee, "new-1\n")
	if got := recv(t, ch); got != "new-1" {
		t.Fatalf("live line = %q, want new-1", got)
	}
	if got := tee.Subscribers(); got != 1 {
		t.Fatalf("Subscribers = %d, want 1", got)
	}
}
