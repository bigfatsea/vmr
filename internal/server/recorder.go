// Ver 2026-07-20, by Sonnet 5
package server

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"vmr/internal/audit"
)

// recorderBodyCap bounds how much of a *successful* response body the audit
// copy accumulates. Unlike router.errBodyCap (upstream error bodies, capped
// at the read site), a success response is streamed straight to the client
// with no size limit — so without this cap, an oversized or runaway upstream
// stream (SSE or otherwise) would make recorder.buf grow unbounded in
// lockstep, entirely outside the client's own memory budget. Kept above
// router.bufferedCap (audit completeness matters more here — a truncated
// audit copy loses `vmr analyze` information, not just "smart"
// normalization) but still just a fraction of the old 64MB: today's
// ~1M-token context windows are ~3-4MB of bytes, and a legitimate response
// has no structural reason to run many times larger than that.
const recorderBodyCap = 16 << 20

// recorder tees the client-side response into the audit record: status,
// headers, the full body (capped, see recorderBodyCap), and the
// first-body-byte time (the client-view TTFT). Flush passes through so
// streaming latency is unaffected. The client always receives every byte
// unchanged — only the audit copy is capped.
//
// captureBody is false when auditing is off but live stats is on: the
// completion hook only needs status + ttftMS, so the 16MB body buffer (and
// the header redact in message()) are pure waste there — a -audit=false
// deployment that just wants /stats should not pay for a full audit copy.
type recorder struct {
	http.ResponseWriter
	start       time.Time
	ttftMS      int64 // arrival → first body byte; 0 until the first Write
	status      int
	buf         bytes.Buffer
	truncated   bool
	captureBody bool
}

func newRecorder(w http.ResponseWriter, start time.Time, captureBody bool) *recorder {
	return &recorder{ResponseWriter: w, start: start, captureBody: captureBody}
}

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.ttftMS == 0 && len(p) > 0 {
		r.ttftMS = time.Since(r.start).Milliseconds()
	}
	if !r.captureBody {
		return r.ResponseWriter.Write(p)
	}
	if remain := recorderBodyCap - r.buf.Len(); remain > 0 {
		if remain < len(p) {
			r.buf.Write(p[:remain])
			r.truncated = true
		} else {
			r.buf.Write(p)
		}
	} else {
		r.truncated = true
	}
	return r.ResponseWriter.Write(p)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// message renders what was sent to the client; nil if nothing was written,
// or if this recorder is in status-only mode (captureBody false — auditing
// off, so nobody serializes this).
func (r *recorder) message() *audit.Message {
	if r.status == 0 || !r.captureBody {
		return nil
	}
	body := r.buf.Bytes()
	if r.truncated {
		// A fresh slice per EncodeBody's "referenced, not cloned" ownership
		// contract (audit.go) — r.buf's own backing array must not be mutated.
		body = append(append([]byte(nil), body...), []byte(fmt.Sprintf("\n...(recording truncated at %d bytes)", recorderBodyCap))...)
	}
	return &audit.Message{Status: r.status, Headers: audit.Redact(r.Header()), Body: audit.EncodeBody(body)}
}
