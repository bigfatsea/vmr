// Ver 2026-07-08 07:40, by Fable 5

// Package audit writes one JSONL record per chat request: the client-side
// exchange plus every upstream attempt, raw and unaggregated. Analysis
// (request counts, token usage, …) is done later by external scripts reading
// these files — vmr itself only records.
package audit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MaxBodyBytes caps every recorded body; longer bodies are cut and flagged.
const MaxBodyBytes = 1 << 20

// Record is one audit line. Two layers: Client is the caller↔vmr exchange,
// Attempts are the vmr↔provider exchanges (one entry per failover attempt).
type Record struct {
	TS       time.Time `json:"ts"`       // request arrival
	DurMS    int64     `json:"dur_ms"`   // total wall time
	Model    string    `json:"model"`    // virtual model ("" if rejected before parsing)
	Protocol string    `json:"protocol"` // ingress protocol: openai | anthropic
	Stream   bool      `json:"stream"`
	Outcome  string    `json:"outcome"` // ok | error | canceled
	Client   Exchange  `json:"client"`
	Attempts []Attempt `json:"attempts,omitempty"`
}

type Exchange struct {
	Addr     string   `json:"addr,omitempty"` // client remote address
	Request  Message  `json:"request"`
	Response *Message `json:"response,omitempty"`
}

// Attempt is one upstream try. On the successful attempt the response body is
// omitted: passthrough means it is byte-identical to Client.Response.Body —
// except for the normalization steps listed in Norm, which are the complete
// explanation of any byte difference between the upstream body and what the
// client received.
type Attempt struct {
	Endpoint string   `json:"endpoint"` // protocol/provider/model
	URL      string   `json:"url"`
	DurMS    int64    `json:"dur_ms"`
	Request  Message  `json:"request"`
	Response *Message `json:"response,omitempty"`
	Error    string   `json:"error,omitempty"` // error class, or "network: …" / "build: …" / "truncated: …" / "canceled by client"
	Norm     []string `json:"norm,omitempty"`  // normalization steps applied to the forwarded response
}

type Message struct {
	Method        string      `json:"method,omitempty"`
	Path          string      `json:"path,omitempty"`
	Status        int         `json:"status,omitempty"`
	Headers       http.Header `json:"headers,omitempty"`
	Body          any         `json:"body,omitempty"` // json.RawMessage if valid JSON, else string
	BodyTruncated bool        `json:"body_truncated,omitempty"`
}

// NewMessage builds a Message with redacted headers and a size-capped body.
func NewMessage(headers http.Header, body []byte) Message {
	m := Message{Headers: Redact(headers)}
	m.Body, m.BodyTruncated = EncodeBody(body)
	return m
}

// EncodeBody returns the body as raw JSON when it is valid JSON (kept
// queryable for analysis scripts), otherwise as a plain string (e.g. SSE).
func EncodeBody(body []byte) (any, bool) {
	if len(body) == 0 {
		return nil, false
	}
	truncated := false
	if len(body) > MaxBodyBytes {
		body, truncated = body[:MaxBodyBytes], true
	}
	if !truncated && json.Valid(body) {
		return json.RawMessage(append([]byte(nil), body...)), false
	}
	return string(body), truncated
}

// credentialHeaders are recorded masked: only the last 4 characters survive.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "X-Auth-Token"}

// Redact copies headers, masking credential values ("Bearer sk-…" → "***c1d4").
func Redact(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	out := make(http.Header, len(h))
	for k, vs := range h {
		out[k] = vs
	}
	for _, k := range credentialHeaders {
		if vs := out.Values(k); len(vs) > 0 {
			masked := make([]string, len(vs))
			for i, v := range vs {
				masked[i] = mask(v)
			}
			out[http.CanonicalHeaderKey(k)] = masked
		}
	}
	return out
}

func mask(v string) string {
	// Keep an auth-scheme prefix ("Bearer ") readable, mask the credential.
	cred := v
	prefix := ""
	if i := strings.IndexByte(v, ' '); i > 0 {
		prefix, cred = v[:i+1], v[i+1:]
	}
	if len(cred) <= 4 {
		return prefix + "***"
	}
	return prefix + "***" + cred[len(cred)-4:]
}

// Logger appends records to one JSONL file per day:
// {dir}/vmr-audit-YYYY-MM-DD.jsonl. A nil *Logger is a no-op sink.
type Logger struct {
	mu   sync.Mutex
	dir  string
	date string
	f    *os.File
	now  func() time.Time // injectable for tests
}

// Dir resolves the audit directory: $VMR_LOG_DIR, or the system temp dir.
func Dir() string {
	if d := os.Getenv("VMR_LOG_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}

func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Logger{dir: dir, now: time.Now}, nil
}

// Path returns the file the next write would go to.
func (l *Logger) Path() string {
	return filepath.Join(l.dir, "vmr-audit-"+l.now().Format("2006-01-02")+".jsonl")
}

// Write appends one record. Failures must never break request serving: the
// error is returned for the caller to log and otherwise ignore.
func (l *Logger) Write(rec *Record) error {
	if l == nil {
		return nil
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	date := l.now().Format("2006-01-02")
	if l.f == nil || date != l.date {
		if l.f != nil {
			l.f.Close()
		}
		f, err := os.OpenFile(filepath.Join(l.dir, "vmr-audit-"+date+".jsonl"),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("audit open: %w", err)
		}
		l.f, l.date = f, date
	}
	_, err = l.f.Write(append(line, '\n'))
	return err
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}
