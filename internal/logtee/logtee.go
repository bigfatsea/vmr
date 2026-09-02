// Package logtee is an in-process tee for the router's live console log:
// an io.Writer that keeps the most recent lines in a bounded ring buffer
// and fans every line out to streaming subscribers.
//
// It sits between log.Logger and stderr (wired in cmd/vmr as
// stampWriter{io.MultiWriter(os.Stderr, tee)}), so /log serves exactly the
// bytes the terminal sees — same lines, same timestamps. The package knows
// nothing about log formatting, routing, HTTP, or timing (the /log
// handler owns the idle-keepalive clock, since only it observes delivery
// activity): it is a plain text-line broadcast bus, which is what keeps it
// a leaf package.
//
// Concurrency: log.Logger serializes Write calls (one mutex-protected call
// per complete line), but subscribers come and go from HTTP handler
// goroutines concurrently with writes, so all state sits behind one mutex —
// including the subscriber registry's mutation path (the copy-on-write
// without-a-lock failure mode project conventions warn about).
package logtee

import (
	"fmt"
	"strings"
	"sync"
)

// DefaultCapLines is the ring buffer size used by cmd/vmr: how many recent
// lines a fresh /log connection replays before live following begins.
// Deliberately a constant, not config: the replay limit IS the buffer size,
// and parameterizing one without the other invites "asked for 5000, got 512".
const DefaultCapLines = 512

// subBuffer is each subscriber's channel capacity. A consumer slower than
// this fills its channel; further lines are dropped for it (with a marker,
// see sub.offer) rather than ever blocking the logging hot path.
const subBuffer = 64

type sub struct {
	ch chan string
	// dropped counts lines skipped while ch was full; the next successful
	// delivery prepends a "... dropped N lines ..." marker so a gap never
	// masquerades as "vmr logged nothing".
	dropped int
}

// offer delivers line to the subscriber without ever blocking. Called from
// Write with t.mu held; nobody ever closes ch, so there is no
// send-on-closed hazard.
func (s *sub) offer(line string) {
	if s.dropped > 0 {
		select {
		case s.ch <- fmt.Sprintf("... dropped %d lines ...", s.dropped):
			s.dropped = 0
		default:
			s.dropped++
			return
		}
	}
	select {
	case s.ch <- line:
	default:
		s.dropped++
	}
}

// Tee is the write end and registry. Create with New; wire into a
// log.Logger as an io.Writer and hand to server.WithLogTee.
type Tee struct {
	mu    sync.Mutex
	buf   []string // ring of capLines entries; head = oldest slot
	head  int
	count int
	subs  map[*sub]struct{}
}

// New returns a Tee holding up to capLines recent lines. capLines <= 0
// panics: a non-positive buffer is a programming error at the single call
// site, not a runtime condition to report.
func New(capLines int) *Tee {
	if capLines <= 0 {
		panic("logtee.New: capLines must be > 0")
	}
	return &Tee{
		buf:  make([]string, capLines),
		subs: make(map[*sub]struct{}),
	}
}

// Write implements io.Writer. One call carries one complete newline-terminated
// line (log.Logger's contract); defensive trailing-newline strip + split keeps
// that true even if a future caller batches. Never returns an error: the tee
// must not fail the logger's write to stderr regardless of what happens here.
func (t *Tee) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimSuffix(string(p), "\n"), "\n") {
		if line == "" {
			continue
		}
		t.mu.Lock()
		t.append(line)
		for s := range t.subs {
			s.offer(line)
		}
		t.mu.Unlock()
	}
	return len(p), nil
}

// append stores line in the ring, overwriting the oldest entry once full.
// Caller holds t.mu.
func (t *Tee) append(line string) {
	cap := len(t.buf)
	if t.count < cap {
		t.buf[(t.head+t.count)%cap] = line
		t.count++
		return
	}
	t.buf[t.head] = line
	t.head = (t.head + 1) % cap
}

// Follow registers a live follower and atomically snapshots the current
// buffer: the registry write and the ring read happen under one lock
// acquisition, so a line written while a consumer connects lands in exactly
// one of the two — never lost between the ring read and the registry write.
// This is what /log opens with; the returned slice is the replay prefix,
// oldest first.
func (t *Tee) Follow() ([]string, <-chan string, func()) {
	s := &sub{ch: make(chan string, subBuffer)}
	t.mu.Lock()
	t.subs[s] = struct{}{}
	out := make([]string, 0, t.count)
	for i := range t.count {
		out = append(out, t.buf[(t.head+i)%len(t.buf)])
	}
	t.mu.Unlock()
	cancel := func() {
		t.mu.Lock()
		delete(t.subs, s)
		t.mu.Unlock()
	}
	return out, s.ch, cancel
}

// Subscribers reports the number of live subscriptions (test introspection).
func (t *Tee) Subscribers() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.subs)
}
