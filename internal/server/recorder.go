// Ver 2026-07-07, by Fable 5
package server

import (
	"bytes"
	"net/http"

	"vmr/internal/audit"
)

// recorder tees the client-side response into the audit record: status,
// headers, and up to audit.MaxBodyBytes of body. Flush passes through so
// streaming latency is unaffected.
type recorder struct {
	http.ResponseWriter
	status  int
	written int64
	buf     bytes.Buffer
}

func newRecorder(w http.ResponseWriter) *recorder { return &recorder{ResponseWriter: w} }

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
	if room := audit.MaxBodyBytes - r.buf.Len(); room > 0 {
		r.buf.Write(p[:min(len(p), room)])
	}
	r.written += int64(len(p))
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
	m := &audit.Message{Status: r.status, Headers: audit.Redact(r.Header())}
	body, truncated := audit.EncodeBody(r.buf.Bytes())
	m.Body = body
	m.BodyTruncated = truncated || r.written > int64(r.buf.Len())
	return m
}
