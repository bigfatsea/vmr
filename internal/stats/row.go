package stats

import (
	"math"
	"sync"
	"time"
)

// row is the in-memory form of an Event: strings interned to dictionary ids
// and integers narrowed, so a 90-day window at team scale stays in the tens
// of megabytes. Rows are append-only and never mutated after append — the
// query side relies on that to scan a partition outside the write lock.
type row struct {
	ts                                                      int64 // unix ms
	vmodel, protocol, client, provider, model, keyLabel, ec uint32
	src                                                     uint32
	status                                                  uint16
	attempts                                                uint8
	outcome                                                 uint8
	flags                                                   uint8
	dur, ttft                                               int32
	in, out, cacheRead, cacheWrite                          int64
}

const (
	flagStream uint8 = 1 << iota
	flagForwarded
	flagHasTools
	flagHasImage
	// flagNoTokens marks a forwarded request without a token record: the
	// attempt was never stamped (the audit record predates the stamp) or the
	// stamp is all zero. Unknown is not zero — the token metrics skip the
	// row and the query reports how many it skipped.
	flagNoTokens
)

const (
	outcomeOK uint8 = iota
	outcomeError
	outcomeCanceled
)

func outcomeCode(s string) uint8 {
	switch s {
	case OutcomeOK:
		return outcomeOK
	case OutcomeCanceled:
		return outcomeCanceled
	default:
		return outcomeError
	}
}

var outcomeNames = [...]string{OutcomeOK, OutcomeError, OutcomeCanceled}

func (r *row) has(f uint8) bool { return r.flags&f != 0 }

// dict interns dimension strings. Ids are never reused, so an id read from
// any row resolves for the life of the process; id 0 is the empty string.
//
// The set of distinct values is capped: client-chosen strings (a request's
// model name) would otherwise grow it for as long as the process runs. Past
// maxDictEntries a new string is booked as OverflowValue.
type dict struct {
	mu   sync.RWMutex
	ids  map[string]uint32
	strs []string
}

const (
	maxDictEntries = 1 << 16
	// OverflowValue stands in for any string first seen after the dictionary
	// is full.
	OverflowValue = "(overflow)"
)

func newDict() *dict {
	return &dict{ids: map[string]uint32{"": 0, OverflowValue: 1}, strs: []string{"", OverflowValue}}
}

func (d *dict) id(s string) uint32 {
	if s == "" {
		return 0
	}
	d.mu.RLock()
	v, ok := d.ids[s]
	d.mu.RUnlock()
	if ok {
		return v
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if v, ok := d.ids[s]; ok {
		return v
	}
	if len(d.strs) >= maxDictEntries {
		return d.ids[OverflowValue]
	}
	v = uint32(len(d.strs))
	d.ids[s] = v
	d.strs = append(d.strs, s)
	return v
}

func (d *dict) str(id uint32) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if int(id) < len(d.strs) {
		return d.strs[id]
	}
	return ""
}

func clampI32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < 0 {
		return 0
	}
	return int32(v)
}

func clampU8(v int) uint8 {
	if v > math.MaxUint8 {
		return math.MaxUint8
	}
	if v < 0 {
		return 0
	}
	return uint8(v)
}

func clampU16(v int) uint16 {
	if v > math.MaxUint16 || v < 0 {
		return 0
	}
	return uint16(v)
}

func (d *dict) toRow(e Event) row {
	r := row{
		ts:       e.TS.UnixMilli(),
		vmodel:   d.id(e.VModel),
		protocol: d.id(e.Protocol),
		client:   d.id(e.ClientKeyTag),
		provider: d.id(e.Provider),
		model:    d.id(e.Model),
		keyLabel: d.id(e.KeyLabel),
		ec:       d.id(e.ErrorClass),
		src:      d.id(e.Src),
		status:   clampU16(e.Status),
		attempts: clampU8(e.Attempts),
		outcome:  outcomeCode(e.Outcome),
		dur:      clampI32(e.DurMS),
		ttft:     clampI32(e.TTFTMS),
	}
	if e.Stream {
		r.flags |= flagStream
	}
	if e.HasTools {
		r.flags |= flagHasTools
	}
	if e.HasImage {
		r.flags |= flagHasImage
	}
	if e.Forwarded {
		r.flags |= flagForwarded
		if e.Tokens == (Tokens{}) {
			r.flags |= flagNoTokens
		}
		r.in, r.out, r.cacheRead, r.cacheWrite = e.Tokens.In, e.Tokens.Out, e.Tokens.CacheRead, e.Tokens.CacheWrite
	}
	return r
}

// toEvent reconstructs the Event (for /stats/events). The zone is the
// process's local one — the write-time offset is not kept per row, and the
// statistics module's display authority is time.Local anyway.
func (d *dict) toEvent(r *row) Event {
	e := Event{
		TS:           time.UnixMilli(r.ts).In(time.Local),
		VModel:       d.str(r.vmodel),
		Protocol:     d.str(r.protocol),
		Stream:       r.has(flagStream),
		ClientKeyTag: d.str(r.client),
		Outcome:      outcomeNames[r.outcome],
		Status:       int(r.status),
		ErrorClass:   d.str(r.ec),
		Attempts:     int(r.attempts),
		Provider:     d.str(r.provider),
		Model:        d.str(r.model),
		KeyLabel:     d.str(r.keyLabel),
		Forwarded:    r.has(flagForwarded),
		DurMS:        int64(r.dur),
		TTFTMS:       int64(r.ttft),
		HasTools:     r.has(flagHasTools),
		HasImage:     r.has(flagHasImage),
		Src:          d.str(r.src),
	}
	if e.Forwarded {
		e.Tokens = Tokens{In: r.in, Out: r.out, CacheRead: r.cacheRead, CacheWrite: r.cacheWrite}
	}
	return e
}
