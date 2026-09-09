// Ver 2026-09-03, by pi-agent

// In-flight registry: a per-request live view of what the router is doing
// right now (the LiveStats design doc §5). The server layer registers a
// request before the concurrency gate and removes it from its defer chain
// when the request leaves by any path; the router's own code only stamps:
//
//   - tryOne, per attempt sent: sent_at / attempt / the endpoint triple
//     (provider/model/key_label), overwritten on every failover turn;
//   - copyFlush, per body chunk: first_byte_at (first chunk) /
//     last_byte_at (every chunk) / est_out, deliberately unthrottled (§5.3).
//
// In-flight entries never settle into any completed-time ledger (§5.4): a
// request is counted there exactly once by the done() hook, here exactly
// once by registration. The two counting planes do not touch.
//
// Locking: one mutex covers ONLY the map's add/remove. Per-entry stampable
// fields are atomics, so the per-chunk stamping path never takes the
// registry lock; Snapshot() holds the lock just long enough to copy the
// entry list, then reads each entry's atomics unlocked.
package router

import (
	"cmp"
	"context"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"vmr/internal/respnorm"
)

// InflightInitials carries what is knowable at registration time (§5.2):
// arrival facts only. sent_at/attempt/triple and the byte/token stamps come
// later through the handle.
type InflightInitials struct {
	Protocol     string
	VModel       string
	Stream       bool
	ClientKeyTag string
	Addr         string
	// TS is the arrival instant; zero means Register stamps time.Now().
	TS time.Time
	// EstIn is Facts.EstimatedTokens when already computed. Normally it is
	// not: facts are computed after the concurrency gate, so queued entries
	// start unknown and the server stamps it later via the handle.
	EstIn int64
}

// InflightEntry is the JSON-ready snapshot row of one in-flight request.
// Field names are the design doc §5.2 table's; timestamps are RFC3339 with
// the host's local offset, and an unstamped one renders as "".
type InflightEntry struct {
	Seq          uint64 `json:"seq"`
	State        string `json:"state"` // "queued" | "running" — derived from sent_at, never stored
	Protocol     string `json:"protocol"`
	VModel       string `json:"vmodel"`
	Stream       bool   `json:"stream"`
	ClientKeyTag string `json:"client_key_tag,omitempty"`
	Addr         string `json:"addr"`
	TS           string `json:"ts"`
	SentAt       string `json:"sent_at,omitempty"`
	Attempt      int64  `json:"attempt,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Model        string `json:"model,omitempty"`
	KeyLabel     string `json:"key_label,omitempty"`
	FirstByteAt  string `json:"first_byte_at,omitempty"`
	LastByteAt   string `json:"last_byte_at,omitempty"`
	EstIn        int64  `json:"est_in,omitempty"`
	EstOut       int64  `json:"est_out,omitempty"`
}

// inflightRec is one registered request's mutable record. Stampable fields
// are atomics (the per-chunk stamping path must stay lock-free, §5.3); the
// identity fields are set once by Register before the entry becomes visible,
// so plain values suffice.
type inflightRec struct {
	seq          uint64
	protocol     string
	vmodel       string
	stream       bool
	clientKeyTag string
	addr         string
	ts           time.Time

	sentAt      atomic.Int64 // unix nanos; 0 = not yet sent upstream
	attempt     atomic.Int64
	triple      atomic.Pointer[inflightTriple]
	firstByteAt atomic.Int64
	lastByteAt  atomic.Int64
	estIn       atomic.Int64
	estOut      atomic.Int64
}

// inflightTriple is the current attempt's endpoint identity, swapped as one
// pointer so a snapshot never tears provider/model/key_label across a
// failover boundary (the triple is stamped at the same point as sent_at).
type inflightTriple struct {
	provider string
	model    string
	keyLabel string
}

// InflightRegistry tracks requests from registration to removal. Every
// method is nil-safe: a registry that was never wired (Router values built
// by struct literal in tests) makes Register return a no-op handle and
// no-op remove, same convention as Router.Quota.
type InflightRegistry struct {
	mu  sync.Mutex
	seq atomic.Uint64
	m   map[uint64]*inflightRec
}

// NewInflightRegistry builds an empty registry; Router.New wires one up.
func NewInflightRegistry() *InflightRegistry {
	return &InflightRegistry{m: make(map[uint64]*inflightRec)}
}

// Register admits one request and returns its stamping handle plus the
// removal func for the caller's defer chain. remove is idempotent — the
// normal return path and the TRUNCATED panic unwind (which runs the same
// defers) can both reach it, and removing an already-removed seq is a no-op.
func (reg *InflightRegistry) Register(in InflightInitials) (*InflightHandle, func()) {
	if reg == nil {
		return nil, func() {}
	}
	ts := in.TS
	if ts.IsZero() {
		ts = time.Now()
	}
	rec := &inflightRec{
		protocol:     in.Protocol,
		vmodel:       in.VModel,
		stream:       in.Stream,
		clientKeyTag: in.ClientKeyTag,
		addr:         in.Addr,
		ts:           ts,
	}
	rec.estIn.Store(in.EstIn)
	rec.seq = reg.seq.Add(1)
	reg.mu.Lock()
	reg.m[rec.seq] = rec
	reg.mu.Unlock()
	return &InflightHandle{rec: rec}, sync.OnceFunc(func() { reg.remove(rec.seq) })
}

// remove deletes the entry: a request that leaves by any path (completed,
// failed, canceled, abandoned while queued) is gone whole — nothing is
// settled anywhere (§5.4).
func (reg *InflightRegistry) remove(seq uint64) {
	reg.mu.Lock()
	delete(reg.m, seq)
	reg.mu.Unlock()
}

// InflightHandle stamps one registered request in place. All methods are
// nil-safe: a request that never registered (or a registry that was never
// wired) stamps nothing, mirroring audit.Attempt's nil-safe Set* pattern.
type InflightHandle struct {
	rec *inflightRec
}

// Len reports the number of requests currently registered.
func (reg *InflightRegistry) Len() int {
	if reg == nil {
		return 0
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return len(reg.m)
}

// Seq returns the entry's process-unique sequence number, or 0 when h is nil.
func (h *InflightHandle) Seq() uint64 {
	if h == nil || h.rec == nil {
		return 0
	}
	return h.rec.seq
}

// inflightCtxKey carries a request's handle from the server layer's
// registration point down to tryOne/forwardSuccess. The handle travels in
// the context rather than as a new ServeWithSnap parameter so the exported
// routing signature (which the server layer pins) stays untouched.
type inflightCtxKey struct{}

// WithInflightHandle returns ctx carrying h, for the server layer to plant
// on the request right after Register. nil-safe: a nil handle is not planted.
func WithInflightHandle(ctx context.Context, h *InflightHandle) context.Context {
	if h == nil {
		return ctx
	}
	return context.WithValue(ctx, inflightCtxKey{}, h)
}

func inflightHandleFrom(ctx context.Context) *InflightHandle {
	h, _ := ctx.Value(inflightCtxKey{}).(*InflightHandle)
	return h
}

// stampSent marks one attempt as sent upstream: sent_at, the attempt number,
// and the endpoint triple, all overwritten by the next attempt — a request
// stuck in failover must show the endpoint it is currently waiting on (§5.2).
func (h *InflightHandle) stampSent(attempt int, provider, model, keyLabel string) {
	if h == nil {
		return
	}
	h.rec.attempt.Store(int64(attempt))
	h.rec.triple.Store(&inflightTriple{provider: provider, model: model, keyLabel: keyLabel})
	h.rec.sentAt.Store(time.Now().UnixNano())
}

// SetEstIn stamps Facts.EstimatedTokens once the facts exist (after the
// concurrency gate — see InflightInitials.EstIn).
func (h *InflightHandle) SetEstIn(estIn int64) {
	if h == nil {
		return
	}
	h.rec.estIn.Store(estIn)
}

// stampChunk records one response-body chunk arrival: first_byte_at on the
// first chunk, last_byte_at on every chunk, and the running est_out.
// copyFlush's read loop calls this per chunk with n > 0. estOut is the
// respnorm meter's count so far — same source as quota charging (§5.2); a
// compressed body estimates 0, which shows as unknown (§5.4), no
// special-casing here.
func (h *InflightHandle) stampChunk(estOut int64) {
	if h == nil {
		return
	}
	now := time.Now().UnixNano()
	h.rec.firstByteAt.CompareAndSwap(0, now)
	h.rec.lastByteAt.Store(now)
	h.rec.estOut.Store(estOut)
}

// Snapshot returns the JSON-ready view of every currently registered
// request, ordered by seq so page polling can correlate entries across
// polls. The mutex is held only to copy the entry list; each entry's
// atomics are read outside it. An entry that finishes mid-copy simply does
// not appear — the same visibility the next poll would have.
func (reg *InflightRegistry) Snapshot() []InflightEntry {
	if reg == nil {
		return nil
	}
	reg.mu.Lock()
	live := make([]*inflightRec, 0, len(reg.m))
	for _, rec := range reg.m {
		live = append(live, rec)
	}
	reg.mu.Unlock()
	sortBySeq(live)
	out := make([]InflightEntry, 0, len(live))
	for _, rec := range live {
		out = append(out, rec.snapshot())
	}
	return out
}

func sortBySeq(live []*inflightRec) {
	slices.SortFunc(live, func(a, b *inflightRec) int { return cmp.Compare(a.seq, b.seq) })
}

// snapshot renders one entry. state derives from sent_at (§5.2: never
// stored): sent_at == 0 means the request has not reached an upstream yet —
// queued or mid-preprocessing — and the triple is empty then too, since it
// is stamped at the same point as sent_at.
func (rec *inflightRec) snapshot() InflightEntry {
	sent := rec.sentAt.Load()
	e := InflightEntry{
		Seq:          rec.seq,
		State:        "queued",
		Protocol:     rec.protocol,
		VModel:       rec.vmodel,
		Stream:       rec.stream,
		ClientKeyTag: rec.clientKeyTag,
		Addr:         rec.addr,
		TS:           rec.ts.Format(time.RFC3339Nano),
		SentAt:       inflightRFC3339(sent),
		Attempt:      rec.attempt.Load(),
		EstIn:        rec.estIn.Load(),
		EstOut:       rec.estOut.Load(),
		FirstByteAt:  inflightRFC3339(rec.firstByteAt.Load()),
		LastByteAt:   inflightRFC3339(rec.lastByteAt.Load()),
	}
	if sent != 0 {
		e.State = "running"
		if tr := rec.triple.Load(); tr != nil {
			e.Provider, e.Model, e.KeyLabel = tr.provider, tr.model, tr.keyLabel
		}
	}
	return e
}

// inflightRFC3339 renders a stamped unix-nano instant as RFC3339 with the
// host's local offset; 0 renders as "". DisplayZone is deliberately not
// used: these are JSON API values for /stats consumers carrying their own
// offset, not human-facing Markdown/CLI output (see the display-timezone
// rule in AGENTS.md).
func inflightRFC3339(nanos int64) string {
	if nanos == 0 {
		return ""
	}
	return time.Unix(0, nanos).Format(time.RFC3339Nano)
}

// chunkStampReader observes every read the copy loop makes from the
// normalized upstream body and stamps the in-flight entry (§5.3). It wraps
// the stream in forwardSuccess — copyFlush only takes an io.Reader, and
// changing its signature would touch every existing test call site, so the
// wrap point carries the meter closure instead.
type chunkStampReader struct {
	src   io.Reader
	meter func() int64
	h     *InflightHandle
}

func (cr *chunkStampReader) Read(p []byte) (int, error) {
	n, err := cr.src.Read(p)
	if n > 0 {
		cr.h.stampChunk(cr.meter())
	}
	return n, err
}

// inflightStamped wraps the response stream with per-chunk stamping when a
// handle is registered for the request, and returns it unchanged when not.
func inflightStamped(rbody respnorm.NormalizerStream, h *InflightHandle) io.Reader {
	if h == nil {
		return rbody
	}
	return &chunkStampReader{src: rbody, meter: rbody.OutTokens, h: h}
}
