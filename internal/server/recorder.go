// Ver 2026-07-10, by Fable 5
package server

import (
	"bytes"
	"net/http"
	"time"

	"vmr/internal/audit"
)

// recorder tees the client-side response into the audit record: status,
// headers, the full body, and the first-body-byte time (the client-view
// TTFT). Flush passes through so streaming latency is unaffected.
type recorder struct {
	http.ResponseWriter
	start  time.Time
	ttftMS int64 // arrival → first body byte; 0 until the first Write
	status int
	buf    bytes.Buffer
}

func newRecorder(w http.ResponseWriter, start time.Time) *recorder {
	return &recorder{ResponseWriter: w, start: start}
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
	r.buf.Write(p)
	return r.ResponseWriter.Write(p)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// message renders what was sent to the client; nil if nothing was written.
func (r *recorder) message() *audit.Message {
	if r.status == 0 {
		return nil
	}
	return &audit.Message{Status: r.status, Headers: audit.Redact(r.Header()), Body: audit.EncodeBody(r.buf.Bytes())}
}
