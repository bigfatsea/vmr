// Ver 2026-07-29 23:55, by Sonnet 5

package ctxgraph

import (
	"crypto/md5"
	"math"
	"strings"
	"sync"
	"time"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
)

// Manifest is one request's content-addressed snapshot of the conversation:
// a hash per non-leading-system message, in order, plus the request's own
// metadata. Two manifests from the same lineage differ only by the edit
// between them (see edit.go) — that edit, not either manifest alone, is
// what carries meaning.
type Manifest struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	// Req is the stable cross-command coordinate for this exact record:
	// CanonicalPath(Path) + ":" + Line (see reqcoord.go's ReqCoord). Path
	// itself cannot serve this role — it is whatever the scan caller
	// passed (absolute or relative, with or without a compression suffix),
	// and must stay that way for FetchRecords' later os.Open (records.go).
	// Req is computed once here, at the one place that still has the
	// original scan-input path in scope, so every other consumer
	// (internal/report's RequestRow.Req, internal/reqdetail's filenames)
	// reads the same already-normalized string instead of each
	// re-deriving it.
	Req string `json:"req,omitempty"`

	TS       time.Time `json:"ts"`
	Model    string    `json:"model,omitempty"`
	Protocol string    `json:"protocol,omitempty"`
	Outcome  string    `json:"outcome,omitempty"`
	Endpoint string    `json:"endpoint,omitempty"` // final attempt's "protocol:provider:model", "-" if none
	// ServedEndpoint is the endpoint that actually SERVED the client — the
	// same attribution rule internal/report's endpointInfo applies: the last
	// attempt that got a < 400 response header, preferring the strictly
	// successful one (no attempt error). "" when no attempt ever committed
	// a < 400 response (early client cancel, all attempts failing with
	// network/4xx/5xx errors) — cost attribution keys off this field, not
	// Endpoint, so an unserved request is never priced and a canceled
	// stream that DID commit a 2xx still is, on the same basis report
	// prices it (see internal/story/cost.go). "" (not "-") because unlike
	// Endpoint this is a pricing-basis field, never rendered as a label.
	ServedEndpoint string        `json:"served_endpoint,omitempty"`
	Stream         bool          `json:"stream,omitempty"`
	DurMS          int64         `json:"dur_ms,omitempty"`
	TTFTMS         int64         `json:"ttft_ms,omitempty"`
	Usage          chatmsg.Usage `json:"usage"`
	UsageOK        bool          `json:"usage_ok,omitempty"`

	// EstIn/EstOut are the DEGRADED token estimate, set only when UsageOK is
	// false — the upstream reported no usage, so tokens are estimated from
	// the router's own pre-routing count (audit.Facts.EstimatedTokens) or,
	// failing that, from body size. Both zero when usage is known.
	//
	// They exist so a journey's $ line and the macro report's $ column price
	// the same records: internal/report has always priced these estimated
	// records (and says so in its §2 footnote), internal/story silently
	// skipped them, and nothing said the two totals were on different bases.
	// 0 on a manifest from a pre-v4 parse cache.
	EstIn  int64 `json:"est_in,omitempty"`
	EstOut int64 `json:"est_out,omitempty"`

	// Bytes is this record's decompressed JSON line length — set by
	// scanFile, not BuildManifest (only the scan loop has the raw line in
	// scope). The byte-budget batching in cmd/vmr's story rendering sums
	// this across a candidate's manifests to bound how much a single
	// BuildAll batch will pull into memory (FetchRecords decodes ~this many
	// bytes per wanted line), replacing an untuned "N candidates per batch"
	// constant. 0 on a manifest that came from a pre-v3 parse cache.
	Bytes int `json:"bytes,omitempty"`

	// ClientKeyTag is audit.Record.ClientKeyTag, copied verbatim — "" when
	// auth was disabled or no key matched. internal/story's Journey id
	// embeds the root manifest's tag so a directory listing groups (and
	// sorts within) one client at a time.
	ClientKeyTag string `json:"client_key_tag,omitempty"`

	// TraceID is the Traceparent header's trace-id segment, when the
	// client sent one — a trace-id change between consecutive manifests is
	// a strong "this is a new task" signal (a new client-side request
	// chain), independent of message content. "" when absent.
	TraceID string `json:"trace_id,omitempty"`

	// SysHash is the digest of the leading system block's rendered text
	// (all contiguous role=="system" messages from index 0, concatenated —
	// mirrors session.go's collect(), which folds them into one running
	// hash rather than treating each as its own message). HasSys is false
	// when the request had no leading system message at all.
	SysHash Hash `json:"sys_hash"`
	HasSys  bool `json:"has_sys,omitempty"`
	LeadSys int  `json:"lead_sys,omitempty"`

	// Keys is one hash per non-leading-system message, in original order.
	// MsgIdx[i] is the index of Keys[i]'s message within chatmsg.Messages'
	// output for this request — used by edit classification (edit.go) and
	// lineage stitching (stitch.go) to recognize the same message across
	// manifests without re-parsing content.
	Keys   []Hash `json:"keys,omitempty"`
	MsgIdx []int  `json:"msg_idx,omitempty"`

	// SessKey is a cheap same-conversation hint: Claude Code's
	// metadata.user_id session UUID when present, else "anchor:<hex of
	// Keys[0]>" (the first non-system message's hash) — same convention
	// internal/report/session.go uses for its own (independent) grouping.
	// This is only the STARTING point for lineage grouping; Contract/Fork
	// edits split a SessKey bucket into multiple lineages (see lineage.go)
	// — anchor alone is known to over-merge.
	SessKey string `json:"sess_key,omitempty"`
}

// BuildManifest extracts a Manifest from one audit record. ok=false when the
// record's client request body didn't parse as a chat object (rejected
// requests, malformed JSON) — such records carry no lineage information and
// are reported separately as Graph.Ungrouped by Scan.
func BuildManifest(rec *audit.Record, path string, line int) (*Manifest, bool) {
	body, ok := rec.Client.Request.Body.(map[string]any)
	if !ok {
		return nil, false
	}
	msgs := chatmsg.Messages(body)
	rawMsgs := chatmsg.RawArray(body)
	off := chatmsg.MsgOffset(body)

	m := &Manifest{
		// Path stays exactly as passed in — FetchRecords (records.go)
		// later opens this same string via audit.OpenLogFile to recover
		// original record content, so it must remain a real, resolvable
		// path, not a bare basename. Req is the separate, normalized
		// identity: see reqcoord.go for why the two cannot be the same
		// field.
		Path: path, Line: line, Req: ReqCoord(path, line), TS: rec.TS,
		Model: rec.Model, Protocol: rec.Protocol, Outcome: rec.Outcome,
		Stream: rec.Stream, DurMS: rec.DurMS, TTFTMS: rec.TTFTMS,
		Endpoint:       lastEndpoint(rec),
		ServedEndpoint: servedEndpoint(rec),
		ClientKeyTag:   rec.ClientKeyTag,
	}
	if rec.Client.Response != nil {
		m.Usage, m.UsageOK = chatmsg.ExtractUsage(rec.Client.Response.Body)
	}
	if !m.UsageOK {
		// Only computed when the upstream reported nothing: a manifest whose
		// usage IS known must never carry a competing estimate that some
		// later consumer picks up by accident. Shared implementation with
		// report's own degraded estimate (Facts.EstimatedTokens when the
		// router already computed one, a body-size estimate otherwise) —
		// see chatmsg.EstimateDegradedTokens for why one function and not
		// two: report and story must price the same record on the same
		// basis, and a compile-time shared call is the only way that can't
		// drift.
		var respBody any
		if rec.Client.Response != nil {
			respBody = rec.Client.Response.Body
		}
		m.EstIn, m.EstOut = chatmsg.EstimateDegradedTokens(rec.Facts, rec.Client.Request.Body, respBody)
	}
	if tp := rec.Client.Request.Headers.Get("Traceparent"); tp != "" {
		if parts := strings.Split(tp, "-"); len(parts) >= 2 {
			m.TraceID = parts[1]
		}
	}

	for i, msg := range msgs {
		if msg.Role == "system" && i == m.LeadSys {
			m.LeadSys++
			continue
		}
		var raw any = msg.Text
		if ri := i - off; ri >= 0 && ri < len(rawMsgs) {
			raw = rawMsgs[ri]
		}
		m.Keys = append(m.Keys, hashMsgJSONCached(raw))
		m.MsgIdx = append(m.MsgIdx, i)
	}
	if m.LeadSys > 0 {
		// Raw text bytes, NOT hashJSON — re-running the concatenated system
		// text through hashJSON would re-encode it as a quoted/escaped JSON
		// string first and produce a different digest than hashing the plain
		// bytes directly. internal/report/session.go used to keep its own
		// independent copy of this exact same hashing convention (so its
		// SysChanged detection would agree with this package's); that duplicate
		// copy was later deleted — session.go now reads
		// SysHash straight from here instead of recomputing it.
		m.SysHash = md5.Sum([]byte(LeadingSystemText(msgs, m.LeadSys)))
		m.HasSys = true
	}

	if uid, _ := chatmsg.Nested(body, "metadata", "user_id").(string); uid != "" {
		if i := strings.Index(uid, "session_"); i >= 0 {
			m.SessKey = "meta:" + uid[i:]
		} else {
			m.SessKey = "meta:" + uid
		}
	}
	if m.SessKey == "" && len(m.Keys) > 0 {
		m.SessKey = "anchor:" + m.Keys[0].String()
	}
	return m, true
}

// LeadingSystemText concatenates the raw text of msgs[0:leadSys] — the
// exact slice-and-join BuildManifest uses to compute SysHash. Exported so
// any consumer materializing "the text behind a given SysHash" (e.g.
// internal/reqdetail's system-prompt evidence blob) derives it from this
// one function instead of re-implementing the concatenation and silently
// drifting from what the hash actually covers. leadSys is normally a
// Manifest's own LeadSys field; out-of-range values degrade to "" rather
// than panicking, since a caller holding a stale/foreign leadSys value
// (mismatched against msgs) should get an empty, clearly-wrong result it
// can notice, not a crash.
func LeadingSystemText(msgs []chatmsg.Message, leadSys int) string {
	if leadSys <= 0 || leadSys > len(msgs) {
		return ""
	}
	var b strings.Builder
	for _, msg := range msgs[:leadSys] {
		b.WriteString(msg.Text)
	}
	return b.String()
}

// lastEndpoint mirrors internal/report/detail.go's helper of the same name
// (duplicated, not imported: report must stay ctxgraph's dependent, never
// the other way around — see internal/archtest).
func lastEndpoint(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "-"
	}
	return rec.Attempts[len(rec.Attempts)-1].Endpoint
}

// servedEndpoint applies the same attribution rule as internal/report's
// endpointInfo — prefer the strictly successful attempt (no error, < 400),
// fall back to the last attempt that got a < 400 response header at all —
// duplicated rather than shared for the same reason lastEndpoint is: the
// import boundary forbids ctxgraph -> report. The duplication is pinned by
// cmd/vmr's cost_basis_parity_test, which prices the same canceled/error
// fixtures through both halves and fails if the two rules ever disagree.
// "" (not "-") when nothing served: consumers treat that as "no endpoint
// to attribute to", exactly like report's empty-string endpoint.
func servedEndpoint(rec *audit.Record) string {
	var successEp, servedEp string
	for _, a := range rec.Attempts {
		if a.Response != nil && a.Response.Status < 400 {
			servedEp = a.Endpoint
			if a.Error == "" {
				successEp = a.Endpoint
			}
		}
	}
	if successEp != "" {
		return successEp
	}
	return servedEp
}

// --- Message hash cache ---
//
// In multi-turn agent conversations, consecutive requests resend the full
// accumulated message history. Computing hashMsgJSON via json.Marshal for
// every message on every turn produces O(N^2) serialization work, especially
// when early turns carry large base64 images or long file contents.
// hashMsgJSONCached computes a fast, non-allocating 128-bit fingerprint over
// raw message structures and caches the resulting Hash in a bounded, sharded map.

type rawDigestKey struct {
	h1, h2 uint64
}

type msgHashCacheShard struct {
	sync.RWMutex
	entries map[rawDigestKey]Hash
}

const (
	msgHashShardCount   = 16
	maxMsgHashPerShard  = 1024
)

var msgHashShards [msgHashShardCount]msgHashCacheShard

func init() {
	for i := 0; i < msgHashShardCount; i++ {
		msgHashShards[i].entries = make(map[rawDigestKey]Hash, 256)
	}
}

func hashMsgJSONCached(raw any) Hash {
	h1, h2 := fastRawDigest(raw)
	k := rawDigestKey{h1: h1, h2: h2}
	idx := h1 % msgHashShardCount

	shard := &msgHashShards[idx]
	shard.RLock()
	h, hit := shard.entries[k]
	shard.RUnlock()
	if hit {
		return h
	}

	h = hashMsgJSON(raw)

	shard.Lock()
	if len(shard.entries) >= maxMsgHashPerShard {
		shard.entries = make(map[rawDigestKey]Hash, maxMsgHashPerShard)
	}
	shard.entries[k] = h
	shard.Unlock()
	return h
}

func fastRawDigest(v any) (uint64, uint64) {
	switch t := v.(type) {
	case string:
		return hashString128(t)
	case map[string]any:
		var totH1, totH2 uint64
		count := 0
		for k, val := range t {
			if k == "cache_control" {
				continue
			}
			kh1, kh2 := hashString128(k)
			vh1, vh2 := fastRawDigest(val)
			totH1 += kh1 ^ (vh1 * 0x517cc1b727220a95)
			totH2 += kh2 ^ (vh2 * 0x9e3779b97f4a7c15)
			count++
		}
		totH1 ^= uint64(count) * 0xbf58476d1ce4e5b9
		totH2 ^= uint64(count) * 0x94d049bb133111eb
		return totH1, totH2
	case []any:
		var totH1, totH2 uint64
		for i, elem := range t {
			eh1, eh2 := fastRawDigest(elem)
			totH1 = (totH1 * 31) ^ eh1 ^ uint64(i)
			totH2 = (totH2 * 37) ^ eh2
		}
		return totH1, totH2
	case float64:
		b := math.Float64bits(t)
		return b * 0x517cc1b727220a95, b * 0x9e3779b97f4a7c15
	case bool:
		if t {
			return 0x811c9dc5e73f477e, 0xcbf29ce484222325
		}
		return 0x27d4eb2f165667c5, 0x100000001b3
	case nil:
		return 0x4f1bbcdcbfa54005, 0x9e3779b97f4a7c15
	case int:
		b := uint64(t)
		return b * 0x517cc1b727220a95, b * 0x9e3779b97f4a7c15
	case int64:
		b := uint64(t)
		return b * 0x517cc1b727220a95, b * 0x9e3779b97f4a7c15
	default:
		return 0xdeadbeefcafebabe, 0x0123456789abcdef
	}
}

func hashString128(s string) (uint64, uint64) {
	var h1 uint64 = 0x811c9dc5e73f477e
	var h2 uint64 = 0xcbf29ce484222325
	for i := 0; i < len(s); i++ {
		c := uint64(s[i])
		h1 = (h1 ^ c) * 0x100000001b3
		h2 = (h2 * 31) ^ c
	}
	return h1, h2
}
