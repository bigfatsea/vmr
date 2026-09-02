// Ver 2026-07-24 12:35, by Sonnet 5

// Package audit writes one JSONL record per chat request: the client-side
// exchange plus every upstream attempt, raw and unaggregated. This package
// itself only records and provides shared low-level reading (OpenLogFile,
// ForEachLine) — aggregation (`vmr report`) and request reconstruction
// (`vmr replay`) build on top of it, in their own packages, alongside
// whatever external scripts (jq, DuckDB, …) also read these files directly.
package audit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vmr/internal/core"
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
	Protocol string    `json:"protocol"` // ingress protocol: openai-completions | anthropic-messages | openai-responses | ...
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
	// Facts is vmr's own pre-routing analysis of this request — the exact
	// core.RequestFacts value server.go computed once (image/tools
	// detection, estimated token count) before any routing/failover
	// decision, carried through unchanged rather than recomputed. It is
	// deliberately a sibling of Client.Request, never merged into it:
	// Client.Request stays byte-faithful to exactly what the client sent
	// (that contract must never bend to carry vmr's own derived data), and
	// Facts is what vmr derived from it, recorded next to it. nil only for
	// requests that never reached fact computation — rejected on auth or
	// unparseable/model-less JSON, before a route was even looked up.
	Facts *core.RequestFacts `json:"facts,omitempty"`
	// ReplayOf identifies the source record ("path:line") when this record
	// was produced by `vmr replay --record`, not live traffic. Empty for
	// every ordinary request — vmr itself never sets this field.
	ReplayOf string `json:"replay_of,omitempty"`
	// ClientKeyTag identifies which config.APIKeys entry authenticated this
	// request — KeyTag(the matched key), so it's a short non-secret label
	// derived from the key itself rather than a separately configured name.
	// "" when auth is disabled and the client sent no credential, no key
	// matched at all, or (vmr replay) the record wasn't produced by a live
	// authenticated request.
	ClientKeyTag string `json:"client_key_tag,omitempty"`
}

// UnmarshalJSON normalizes legacy protocol names when reading historical
// audit records: pre-2026-08 logs wrote "openai"/"anthropic" where the
// current enum is "openai-completions"/"anthropic-messages". This is the
// one compatibility chokepoint — the analytics half (report/story/reqdetail/
// ctxgraph) decodes into audit.Record, so normalizing here covers every
// analytics read path. Write paths construct Record directly and are
// unaffected.
//
// Transitional — removal is condition-based, not date-based: strip this
// only once the fact-triggered condition in the protocol-rename entry in
// docs/KNOWN_ISSUES.md's 配置与协议 section holds (zero grep hits for the
// old names across the whole corpus AND confirmation that no offline
// archive will ever need re-parsing). The default retention policy never
// deletes audit logs, so old names do NOT age out on their own — a date-
// based schedule here would misfire.
func (r *Record) UnmarshalJSON(data []byte) error {
	type recordAlias Record // shed UnmarshalJSON to avoid infinite recursion
	if err := json.Unmarshal(data, (*recordAlias)(r)); err != nil {
		return err
	}
	r.Protocol = CanonicalProtocol(r.Protocol)
	for i := range r.Attempts {
		r.Attempts[i].Protocol = CanonicalProtocol(r.Attempts[i].Protocol)
		r.Attempts[i].Endpoint = NormalizeEndpointLabel(r.Attempts[i].Endpoint)
	}
	return nil
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
	Protocol string   `json:"protocol,omitempty"` // == the endpoint's adapter type (openai-completions | anthropic-messages | openai-responses | ...)
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
	// UpstreamModel is the model name the upstream itself reported in its
	// response, recorded ONLY when it differs from Model (the name vmr
	// asked for) — so the overwhelmingly common "they match" case adds
	// nothing to the record. It is raw and carries no verdict: version
	// pinning, vendor prefixes and plan aliases all differ legitimately on
	// every request, and only the aggregate distribution per endpoint can
	// tell an alias apart from a relay quietly serving a cheaper model.
	// vmr cannot recover this after the fact — the rewrite that puts the
	// virtual name back destroys it, and a successful attempt's body is
	// not stored — so it is captured at the only moment it exists.
	UpstreamModel string `json:"upstream_model,omitempty"`
	// Forwarded is true when this attempt's response was actually forwarded
	// to the client — the upstream returned a 2xx, the response was committed
	// to the client, and the router charged quota for it. The ONLY setter is
	// router.forwardSuccess: softblock paths (checkSoftBlock), >=400 error
	// paths (handleErrorResponse), and build/network failures never set it.
	// A truncated stream (SetTruncated after SetSuccessResponse) still has
	// Forwarded=true — the response headers were already committed and quota
	// was charged.
	//
	// Historical JSONL records (pre-v4) lack this field, so its zero value
	// (false) does NOT mean "not forwarded" for those records. Consumers
	// must use the IsForwarded predicate (see audit.IsForwarded) which
	// handles the compatibility case.
	Forwarded bool `json:"forwarded,omitempty"`
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

// The Set* methods below are nil-safe (a nil *Attempt is a no-op) so
// router.tryOne can call them unconditionally instead of wrapping every
// audit write in "if att != nil { ... }" — att is nil exactly when auditing
// is disabled for this request (see router.go). Each mirrors one of the
// outcomes tryOne can reach for a single upstream attempt.

// SetRequest records the outbound URL and request body/headers, once
// BuildRequest has produced them.
func (a *Attempt) SetRequest(url string, header http.Header, body []byte) {
	if a == nil {
		return
	}
	a.URL = url
	a.Request = NewMessage(header, body)
}

// SetBuildError records that the adapter failed to construct the outbound
// request (bad canonical request shape, not a network/upstream failure).
func (a *Attempt) SetBuildError(err error) {
	if a == nil {
		return
	}
	a.Error = "build: " + err.Error()
	a.ErrorClass = core.ErrBuild.String()
}

// SetCanceled records that the client disconnected while this attempt was
// still in flight — either before any response arrived, or mid-stream after
// a 2xx response was already committed (SetSuccessResponse having run first,
// so Response is left in place; only Error/ErrorClass are set here).
func (a *Attempt) SetCanceled() {
	if a == nil {
		return
	}
	a.Error = "canceled by client"
	a.ErrorClass = core.ErrCanceled.String()
}

// SetNetworkError records a dial/write/read failure before any response
// arrived.
func (a *Attempt) SetNetworkError(err error) {
	if a == nil {
		return
	}
	a.Error = "network: " + err.Error()
	a.ErrorClass = core.ErrNetwork.String()
}

// SetErrorResponse records a >=400 upstream response: auditBody is whatever
// the caller decided to keep (already truncated/marked if it exceeded the
// caller's own cap — that policy lives in router, not here).
func (a *Attempt) SetErrorResponse(header http.Header, auditBody []byte, status int, class core.ErrorClass) {
	if a == nil {
		return
	}
	m := NewMessage(header, auditBody)
	m.Status = status
	a.Response = &m
	a.Error = class.String()
	a.ErrorClass = class.String()
}

// SetSuccessResponse records a 2xx upstream response's status/headers. The
// body isn't captured here — it streams to the client and is recorded
// separately via SetNorm/RawPreStrip once the normalizer has processed it.
func (a *Attempt) SetSuccessResponse(status int, header http.Header) {
	if a == nil {
		return
	}
	a.Response = &Message{Status: status, Headers: Redact(header)}
}

// SetForwarded records that this attempt's response was actually forwarded
// to the client. router.forwardSuccess is the ONLY caller — see the
// Forwarded field's own doc comment for what it means and which paths
// never set it.
func (a *Attempt) SetForwarded() {
	if a == nil {
		return
	}
	a.Forwarded = true
}

// IsForwarded is the analytics-side predicate for "was this attempt's
// response actually forwarded (and charged) to the client". It exists
// because the Forwarded field was added after historical JSONL was already
// written: pre-v4 records lack it, so its zero value (false) is ambiguous
// — it means "not forwarded" on new records but "field absent" on old
// ones. The rule: a true Forwarded is authoritative; a false one falls
// back to the old-format signal (a < 400 response with no error class),
// which new-format softblock records never satisfy (checkSoftBlock writes
// ErrorClass "content" alongside its < 400 response). Do not re-derive
// this decision at each call site — it is the single compatibility
// chokepoint for the field.
func (a *Attempt) IsForwarded() bool {
	if a == nil {
		return false
	}
	if a.Forwarded {
		return true
	}
	return a.Response != nil && a.Response.Status < 400 && a.ErrorClass == ""
}

// SetTruncated records that the upstream connection died mid-stream, after
// the response was already committed to the client.
func (a *Attempt) SetTruncated(err error) {
	if a == nil {
		return
	}
	a.Error = "truncated: " + err.Error()
	a.ErrorClass = core.ErrTruncated.String()
}

// SetNorm records which normalization steps the response normalizer
// applied, and — only when one of them rewrote the body (think_strip /
// thinking_process_strip) — the upstream bytes exactly as received just
// before that rewrite.
func (a *Attempt) SetNorm(applied []string, rawPreStrip []byte) {
	if a == nil {
		return
	}
	a.Norm = applied
	if len(rawPreStrip) > 0 {
		a.RawPreStrip = EncodeBody(rawPreStrip)
	}
}

// SetUpstreamModel records the upstream's self-reported model name. Called
// with "" (the common case: it matched what we asked for) it records
// nothing.
func (a *Attempt) SetUpstreamModel(model string) {
	if a == nil || model == "" {
		return
	}
	a.UpstreamModel = model
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

// extraRedactHeaders holds config-supplied header names (config's
// extra_redact_headers) masked the same way as the built-in
// credentialHeaders list above. This covers a gap the built-in list can't:
// a client's own custom auth header (e.g. "X-Custom-Token") that none of
// vmr's adapters know about, and so isn't in credentialHeaders, still
// arrives on the client request and would otherwise sit in the audit file
// in cleartext. atomic.Pointer rather than the registries' mutex-guarded
// copy-on-write pattern (see adapter.registry/strategy.conditions) because
// this is a whole-value replace on every (re)load, never an incremental
// update — same shape as retentionDays above, just for a slice instead of
// an int.
var extraRedactHeaders atomic.Pointer[[]string]

// SetExtraRedactHeaders updates the extra redaction list (config's
// extra_redact_headers); nil or empty is a no-op difference from the
// built-in credentialHeaders list, not an error.
func SetExtraRedactHeaders(names []string) {
	cp := append([]string(nil), names...)
	extraRedactHeaders.Store(&cp)
}

// IsCredentialHeader reports whether h is one of the header names Redact
// masks — the built-in credentialHeaders list plus whatever
// SetExtraRedactHeaders configured. A stored audit record's value for such
// a header is a placeholder ("Bearer ***c1d4"), never the real credential —
// a consumer that reconstructs a request from an audit record
// (internal/replay) must strip these in addition to whatever headers it
// would otherwise block, or it forwards the masked placeholder to a live
// upstream as if it were real.
func IsCredentialHeader(h string) bool {
	for _, k := range credentialHeaders {
		if strings.EqualFold(k, h) {
			return true
		}
	}
	if p := extraRedactHeaders.Load(); p != nil {
		for _, k := range *p {
			if strings.EqualFold(k, h) {
				return true
			}
		}
	}
	return false
}

// Redact copies headers, masking credential values ("Bearer sk-…" → "***c1d4")
// for both the built-in credentialHeaders list and whatever
// SetExtraRedactHeaders configured.
func Redact(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	out := make(http.Header, len(h))
	for k, vs := range h {
		out[k] = append([]string(nil), vs...)
	}
	maskNames(out, credentialHeaders)
	if p := extraRedactHeaders.Load(); p != nil {
		maskNames(out, *p)
	}
	return out
}

func maskNames(h http.Header, names []string) {
	for _, k := range names {
		if vs := h.Values(k); len(vs) > 0 {
			masked := make([]string, len(vs))
			for i, v := range vs {
				masked[i] = mask(v)
			}
			h[http.CanonicalHeaderKey(k)] = masked
		}
	}
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
// entry ever become its ClientKeyTag. 8 (vs. mask()'s 4) trades a couple
// more characters of exposure for a label that reads as deliberate — see
// config.example.yaml's "end your key in -something-readable" convention.
// This is independent of mask()'s redaction length: mask() protects every
// credential header generically; KeyTag only ever runs on vmr's own
// api_keys entries, whose minimum length config.Config.validate already
// enforces specifically so this never exposes a whole key (see keyTagLen's
// config-side counterpart, the 16-char minimum).
const keyTagLen = 8

// KeyTag derives a short, non-secret label from a credential's tail — the
// caller-facing "who sent this" identity for `vmr report` grouping. Called
// on a matched config.APIKeys entry, and (server.authenticate, when
// APIKeys is not configured at all) on whatever unvalidated value a
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
// Examples (keyTagLen = 8):
//
//	...am-alice   → window "am-alice"   → tag "alice"     (5 chars, hyphen at 2)
//	...roj-al     → window "roj-al" (key shorter than window, so window = whole key) → tag "al" (2 chars, hyphen at 3)
//	...x-abcd     → window "x-abcd" (key shorter than window, so window = whole key) → tag "abcd" (4 chars, hyphen at 1)
//	...-abcdefghi → window "bcdefghi"   → tag "bcdefghi"  (hyphen 9 back, outside the window — none found, window kept whole)
//	...am9k3f7a   → window "am9k3f7a"   → tag "am9k3f7a"  (no hyphen anywhere — window kept whole)
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
	lock   *os.File         // dir's exclusive advisory lock (nil on windows, or after Close)
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
	lock, err := acquireDirLock(dir)
	if err != nil {
		return nil, err
	}
	l := &Logger{dir: dir, now: time.Now, lock: lock}
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
		defer func() {
			if p := recover(); p != nil {
				fmt.Fprintf(os.Stderr, "audit: housekeeping panicked; skipped this round: %v\n", p)
			}
		}()
		housekeep(dir, now)
	}()
}

// ActiveLogPath returns the path of the active audit log file for dir on date now.
func ActiveLogPath(dir string, now time.Time) string {
	return filepath.Join(dir, "vmr-audit-"+now.Format("2006-01-02")+".jsonl")
}

// Path returns the file the next write would go to.
func (l *Logger) Path() string {
	return ActiveLogPath(l.dir, l.now())
}

// maxPooledWriteBufCap bounds the capacity of buffers returned to writeBufPool
// to prevent oversized buffers (e.g. from multi-MB multimodal or large-context
// payloads) from lingering in memory indefinitely.
const maxPooledWriteBufCap = 1 << 20 // 1MB

// writeBufPool pools the *bytes.Buffer used to encode a Record before it's
// written to disk. Deliberately NOT a single buffer field on Logger: audit
// records can run to several MB for a long agent conversation, and encoding
// happens here — outside l.mu — specifically so concurrent requests encode
// in parallel; a shared Logger-level buffer would force serializing that
// CPU-bound JSON encoding under the same global lock that guards the much
// cheaper file write, trading one performance problem for a worse one under
// real concurrent load. A sync.Pool gets the allocation-reduction win
// without that regression: each goroutine borrows its own buffer, encodes
// into it with no contention, and only the resulting bytes cross into the
// locked section below.
var writeBufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// Write appends one record. Failures must never break request serving: the
// error is returned for the caller to log and otherwise ignore.
func (l *Logger) Write(rec *Record) error {
	if l == nil {
		return nil
	}
	buf := writeBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		if buf.Cap() <= maxPooledWriteBufCap {
			writeBufPool.Put(buf)
		}
	}()
	// json.NewEncoder.Encode appends its own trailing '\n' — unlike
	// json.Marshal + a manual append(line, '\n'), which reallocates and
	// copies the whole (potentially multi-MB) record just to add one byte,
	// since Marshal's returned slice has no spare capacity.
	//
	// SetEscapeHTML(false) preserves byte-faithful fidelity to the
	// request/response body the audit captures. The encoder defaults to
	// escaping <, > and & as \u003c/\u003e/\u0026 — fine for a JSON
	// payload meant for HTML embedding, but it corrupts line-for-line
	// comparison of an audit record against the live request/response
	// (CLAUDE.md's byte-faithful-passthrough invariant), and a payload that
	// legitimately contains "<foo & \"bar\">" would never survive a
	// round-trip unaltered.
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
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
	_, err := l.f.Write(buf.Bytes())
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
	if l.lock != nil {
		l.lock.Close() // releases the dir lock; nothing to do with an error here
		l.lock = nil
	}
	if l.f != nil {
		err := l.f.Close()
		l.f = nil
		return err
	}
	return nil
}
