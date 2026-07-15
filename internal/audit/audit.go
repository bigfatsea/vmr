// Ver 2026-07-15 03:00, by Sonnet 5

// Package audit writes one JSONL record per chat request: the client-side
// exchange plus every upstream attempt, raw and unaggregated. This package
// itself only records and provides shared low-level reading (OpenLogFile,
// ForEachLine) — aggregation (`vmr report`) and request reconstruction
// (`vmr replay`) build on top of it, in their own packages, alongside
// whatever external scripts (jq, DuckDB, …) also read these files directly.
package audit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// retentionDays gates the delete side of housekeeping (see housekeep.go).
// 0 = disabled: compression on rotation still happens, files just never get
// deleted. Deliberately opt-in rather than defaulting to a "reasonable"
// number — audit logs are the only source for vmr report cost accounting,
// and silently deleting them is not a mistake worth defaulting into.
var retentionDays atomic.Int64

// SetRetentionDays updates the retention window; negative values are ignored.
func SetRetentionDays(n int) {
	if n >= 0 {
		retentionDays.Store(int64(n))
	}
}

// RetentionDays reports the current retention window (0 = keep forever).
func RetentionDays() int { return int(retentionDays.Load()) }

// Record is one audit line. Two layers: Client is the caller↔vmr exchange,
// Attempts are the vmr↔provider exchanges (one entry per failover attempt).
type Record struct {
	TS    time.Time `json:"ts"`     // request arrival
	DurMS int64     `json:"dur_ms"` // total wall time
	// TTFTMS is the client-view first-token latency: arrival → first response
	// body byte written back. 0 (omitted) when nothing was written or the
	// response was instant (<1ms local rejects) — consumers treat 0 as "no
	// measurement", which conveniently excludes those rejects from averages.
	TTFTMS   int64     `json:"ttft_ms,omitempty"`
	Model    string    `json:"model"`    // virtual model ("" if rejected before parsing)
	Protocol string    `json:"protocol"` // ingress protocol: openai | anthropic
	Stream   bool      `json:"stream"`
	Outcome  string    `json:"outcome"` // ok | error | canceled
	Client   Exchange  `json:"client"`
	Attempts []Attempt `json:"attempts,omitempty"`
	// Images lists every inline image found in the request (request only —
	// vmr never generates images). Populated whenever imgprep detects an
	// image, regardless of whether downscaling is configured for this
	// virtual model: Downscaled/DownscaledWidth/... stay zero-valued when
	// downscaling never ran (disabled) or wasn't needed (already small).
	Images []ImageInfo `json:"images,omitempty"`
	// ReplayOf identifies the source record ("path:line") when this record
	// was produced by `vmr replay --record`, not live traffic. Empty for
	// every ordinary request — vmr itself never sets this field.
	ReplayOf string `json:"replay_of,omitempty"`
	// ClientKeyTag identifies which config.APIKeys entry authenticated this
	// request — KeyTag(the matched key), so it's a short non-secret label
	// derived from the key itself rather than a separately configured name.
	// "" when auth is disabled, the request matched the catch-all
	// Config.APIKey instead, no key matched at all, or (vmr replay) the
	// record wasn't produced by a live authenticated request.
	ClientKeyTag string `json:"client_key_tag,omitempty"`
}

// OutcomeFor decides a Record's Outcome from the client-facing HTTP status
// and whether the client disconnected before one was ever written
// (status == 0 alone doesn't imply that — canceled must be asserted
// explicitly by the caller, since a caller with no concept of client
// disconnection, like internal/replay, always has a concrete status).
// Shared so every writer of Outcome (the live server, `vmr replay
// --record`) agrees on where the ok/error boundary sits.
func OutcomeFor(status int, canceled bool) string {
	switch {
	case canceled:
		return "canceled"
	case status < 400:
		return "ok"
	default:
		return "error"
	}
}

// ImageInfo is one inline (or remote-referenced) image found in a request.
type ImageInfo struct {
	MessageIndex int    `json:"message_index"`    // which message (0-based, aligned with the chat message list)
	Format       string `json:"format,omitempty"` // jpeg/png/gif/webp/bmp; empty for a remote URL vmr never fetched
	Bytes        int64  `json:"bytes"`            // original (pre-downscale) byte count; 0 for a remote URL
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	// Remote is true for an http(s) image_url vmr never fetches (imgprep
	// only ever touches inline data-URI/base64 images) — every other field
	// stays zero-valued in that case.
	Remote           bool  `json:"remote,omitempty"`
	Downscaled       bool  `json:"downscaled,omitempty"`
	DownscaledWidth  int   `json:"downscaled_width,omitempty"`
	DownscaledHeight int   `json:"downscaled_height,omitempty"`
	DownscaledBytes  int64 `json:"downscaled_bytes,omitempty"`
	// CacheHit is true when the downscaled bytes were reused byte-for-byte
	// from imgprep's on-disk cache instead of being re-encoded.
	CacheHit bool `json:"cache_hit,omitempty"`
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
	Endpoint string   `json:"endpoint"`           // protocol:provider:model, human-readable label (see Protocol/Provider/Model for the structured form)
	Protocol string   `json:"protocol,omitempty"` // == the endpoint's adapter type (openai | anthropic)
	Provider string   `json:"provider,omitempty"` // provider name as configured
	Model    string   `json:"model,omitempty"`    // real upstream model name (as opposed to Record.Model, the virtual name)
	URL      string   `json:"url"`
	DurMS    int64    `json:"dur_ms"`
	Request  Message  `json:"request"`
	Response *Message `json:"response,omitempty"`
	Error    string   `json:"error,omitempty"` // human-readable detail: "network: …" / "build: …" / "truncated: …" / "canceled by client", or the classify-error message for a >=400 response
	// ErrorClass is the single typed category behind Error — always set
	// alongside Error, one of: client/auth/rate_limit/endpoint/transient/
	// content (mirrors core.ErrorClass for >=400 responses) or build/
	// network/canceled/truncated (the non-HTTP failure paths). Consumers
	// should read this instead of parsing Error's prefix.
	ErrorClass string   `json:"error_class,omitempty"`
	Norm       []string `json:"norm,omitempty"` // normalization steps applied to the forwarded response
	// RawPreStrip holds the upstream bytes exactly as received, from just
	// before a think_strip/thinking_process_strip rewrite ran — nil unless
	// one of those fired. It is the buffered segment only (whatever the
	// normalizer had accumulated at that moment), not a second copy of the
	// whole response body.
	RawPreStrip any `json:"raw_pre_strip,omitempty"`
}

type Message struct {
	Method  string      `json:"method,omitempty"`
	Path    string      `json:"path,omitempty"`
	Status  int         `json:"status,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
	Body    any         `json:"body,omitempty"` // json.RawMessage if valid JSON, else string
}

// NewMessage builds a Message with redacted headers and the full body — no
// size cap; whatever vmr actually saw is what gets recorded.
func NewMessage(headers http.Header, body []byte) Message {
	return Message{Headers: Redact(headers), Body: EncodeBody(body)}
}

// EncodeBody returns the body as raw JSON when it is valid JSON (kept
// queryable for analysis scripts), otherwise as a plain string (e.g. SSE).
//
// Ownership contract: the slice is referenced, not cloned — callers hand
// the bytes over and must not mutate them afterwards. Every current caller
// (client request buffer, recorder response buffer, rewritten attempt body,
// upstream error body, normalizer pre-strip capture) already owns its slice
// outright; cloning here would only re-copy multi-MB bodies on the tail of
// every audited request.
func EncodeBody(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	return string(body)
}

// credentialHeaders are recorded masked: only the last 4 characters survive.
// Cookie/Proxy-Authorization never reach the upstream (server blocklist),
// but they arrive on the client request and would otherwise sit in the
// audit file in cleartext; Set-Cookie covers upstream/CDN session tokens
// on the response side.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "X-Auth-Token", "Cookie", "Set-Cookie", "Proxy-Authorization"}

// IsCredentialHeader reports whether h is one of the header names Redact
// masks. A stored audit record's value for such a header is a placeholder
// ("Bearer ***c1d4"), never the real credential — a consumer that
// reconstructs a request from an audit record (internal/replay) must strip
// these in addition to whatever headers it would otherwise block, or it
// forwards the masked placeholder to a live upstream as if it were real.
func IsCredentialHeader(h string) bool {
	for _, k := range credentialHeaders {
		if strings.EqualFold(k, h) {
			return true
		}
	}
	return false
}

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

// keyTagLen bounds how many trailing characters of a matched config.APIKeys
// entry ever become its ClientKeyTag. 6 (vs. mask()'s 4) trades a couple
// more characters of exposure for a label that reads as deliberate — see
// config.example.yaml's "end your key in -something-readable" convention.
// This is independent of mask()'s redaction length: mask() protects every
// credential header generically; KeyTag only ever runs on vmr's own
// api_keys entries, whose minimum length config.Config.validate already
// enforces specifically so this never exposes a whole key (see keyTagLen's
// config-side counterpart, the 16-char minimum).
const keyTagLen = 6

// KeyTag derives a short, non-secret label from a credential's tail — the
// caller-facing "who sent this" identity for `vmr report` grouping. Called
// on a matched config.APIKeys entry, and (server.authenticate, when neither
// APIKey nor APIKeys is configured at all) on whatever unvalidated value a
// client voluntarily sends — KeyTag itself doesn't care which; either way
// the input is just a string, and the 16-character minimum that keeps the
// former case safe (see below) simply doesn't apply to the latter, since an
// unconfigured, unvalidated value was never a secret to begin with.
//
// Rule: take the last keyTagLen raw characters first, then, if that window
// contains a hyphen, keep only what follows the LAST hyphen inside it —
// this lets a meaningful suffix shorter than keyTagLen (e.g. "-al", 2
// chars) survive intact instead of being padded with whatever unrelated
// characters preceded it in the fixed-length window. A suffix longer than
// keyTagLen simply loses its hyphen and everything before it once the
// window no longer reaches back that far — capped, never longer.
//
// Examples (keyTagLen = 6):
//
//	...-alice    → window "-alice" → tag "alice"  (5 chars, hyphen at 0)
//	...proj-al   → window "roj-al" → tag "al"      (2 chars, hyphen at 3)
//	...X-abcd    → window "X-abcd" → tag "abcd"    (4 chars, hyphen at 1)
//	...-abcdefgh → window "cdefgh" → tag "cdefgh"  (hyphen 8 back, outside the window — none found, window kept whole)
//	...9k3f7a    → window "9k3f7a" → tag "9k3f7a"  (no hyphen anywhere — window kept whole)
//
// Assumes the key is ASCII (true for every real bearer-token format). A key
// shorter than keyTagLen is used whole as the window, then the same hyphen
// rule applies — which is why config validation rejects api_keys entries
// under 16 characters: short enough for the window to be the entire secret
// would otherwise leak it into every report and filename this tag ends up
// in.
func KeyTag(key string) string {
	window := key
	if len(key) > keyTagLen {
		window = key[len(key)-keyTagLen:]
	}
	// i+1 < len(window) excludes a hyphen that is itself the window's last
	// character (nothing follows it) — trimming there would produce an
	// empty tag, so the whole window is kept instead.
	if i := strings.LastIndexByte(window, '-'); i >= 0 && i+1 < len(window) {
		return window[i+1:]
	}
	return window
}

// Logger appends records to one JSONL file per day:
// {dir}/vmr-audit-YYYY-MM-DD.jsonl. A nil *Logger is a no-op sink.
type Logger struct {
	mu     sync.Mutex
	dir    string
	date   string
	f      *os.File
	closed bool             // set by Close; late Writes are dropped, never reopen a file
	now    func() time.Time // injectable for tests

	housekeeping atomic.Bool    // guards against overlapping sweeps
	hkWG         sync.WaitGroup // lets tests wait for a sweep to finish (Close deliberately doesn't: compression is crash-safe, shutdown shouldn't block on it)
}

// New opens (or creates) the audit directory. The directory comes from
// config.yaml's log_dir (default: the persistent ~/.vmr/logs, resolved in
// config.applyDefaults via internal/rundir) — there is no environment
// variable for it anymore.
func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	l := &Logger{dir: dir, now: time.Now}
	// Catch up on anything left uncompressed/unpurged by a previous run
	// (crash, restart, or simply not having been up when a day rolled over).
	l.scheduleHousekeeping()
	return l, nil
}

// scheduleHousekeeping runs housekeep in the background: never on the
// request-serving or audit-write path, and never more than one sweep at a
// time. A no-op if a sweep is already in flight — the next trigger (daily
// rotation, or the next process start) will catch anything missed.
//
// Callers must hold l.mu or otherwise guarantee l.dir/l.now aren't
// concurrently mutated (true both in New, before the Logger escapes, and in
// Write's locked rotation branch) — dir and now are snapshotted here so the
// spawned goroutine never touches Logger fields directly, only its own
// copies plus the housekeeping/hkWG synchronization primitives.
func (l *Logger) scheduleHousekeeping() {
	if !l.housekeeping.CompareAndSwap(false, true) {
		return
	}
	dir, now := l.dir, l.now()
	l.hkWG.Add(1)
	go func() {
		defer l.hkWG.Done()
		defer l.housekeeping.Store(false)
		housekeep(dir, now)
	}()
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
	if l.closed {
		// Shutdown edge: a handler that outlived the drain timeout is trying
		// to record after Close. Refuse rather than write to a closed fd (or,
		// across a midnight boundary, silently reopen a new day file).
		return fmt.Errorf("audit: logger closed, record dropped")
	}
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
		// Rotated to a new day: the file(s) just left behind are done being
		// written to and are fair game for compression/retention.
		l.scheduleHousekeeping()
	}
	_, err = l.f.Write(append(line, '\n'))
	return err
}

// Close closes the current file and marks the logger closed (later Writes
// are refused). It deliberately does NOT wait for an in-flight housekeeping
// sweep: compression is crash-safe (temp file + rename + resume-on-restart),
// housekeeping only ever touches already-rotated files — never the fd being
// closed here — and blocking shutdown on a multi-GB zstd pass buys nothing.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.f != nil {
		err := l.f.Close()
		l.f = nil
		return err
	}
	return nil
}
