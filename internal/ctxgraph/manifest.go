// Ver 2026-07-29 23:55, by Sonnet 5

package ctxgraph

import (
	"crypto/md5"
	"strings"
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
	Path string
	Line int

	TS       time.Time
	Model    string
	Protocol string
	Outcome  string
	Endpoint string // final attempt's "protocol:provider:model", "-" if none
	Stream   bool
	DurMS    int64
	TTFTMS   int64
	Usage    chatmsg.Usage
	UsageOK  bool

	// ClientKeyTag is audit.Record.ClientKeyTag, copied verbatim — "" when
	// auth was disabled or no key matched. internal/story's Journey id
	// embeds the root manifest's tag so a directory listing groups (and
	// sorts within) one client at a time.
	ClientKeyTag string

	// TraceID is the Traceparent header's trace-id segment, when the
	// client sent one — a trace-id change between consecutive manifests is
	// a strong "this is a new task" signal (a new client-side request
	// chain), independent of message content. "" when absent.
	TraceID string

	// SysHash is the digest of the leading system block's rendered text
	// (all contiguous role=="system" messages from index 0, concatenated —
	// mirrors session.go's collect(), which folds them into one running
	// hash rather than treating each as its own message). HasSys is false
	// when the request had no leading system message at all.
	SysHash Hash
	HasSys  bool
	LeadSys int

	// Keys is one hash per non-leading-system message, in original order.
	// MsgIdx[i] is the index of Keys[i]'s message within chatmsg.Messages'
	// output for this request — the coordinate BlobIndex needs to refetch
	// that message's actual text later (see blobindex.go).
	Keys   []Hash
	MsgIdx []int

	// SessKey is a cheap same-conversation hint: Claude Code's
	// metadata.user_id session UUID when present, else "anchor:<hex of
	// Keys[0]>" (the first non-system message's hash) — same convention
	// internal/report/session.go uses for its own (independent) grouping.
	// This is only the STARTING point for lineage grouping; Contract/Fork
	// edits split a SessKey bucket into multiple lineages (see lineage.go)
	// — anchor alone is known to over-merge.
	SessKey string
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
		Path: path, Line: line, TS: rec.TS,
		Model: rec.Model, Protocol: rec.Protocol, Outcome: rec.Outcome,
		Stream: rec.Stream, DurMS: rec.DurMS, TTFTMS: rec.TTFTMS,
		Endpoint:     lastEndpoint(rec),
		ClientKeyTag: rec.ClientKeyTag,
	}
	if rec.Client.Response != nil {
		m.Usage, m.UsageOK = chatmsg.ExtractUsage(rec.Client.Response.Body)
	}
	if tp := rec.Client.Request.Headers.Get("Traceparent"); tp != "" {
		if parts := strings.Split(tp, "-"); len(parts) >= 2 {
			m.TraceID = parts[1]
		}
	}

	var sysText strings.Builder
	for i, msg := range msgs {
		if msg.Role == "system" && i == m.LeadSys {
			sysText.WriteString(msg.Text)
			m.LeadSys++
			continue
		}
		var raw any = msg.Text
		if ri := i - off; ri >= 0 && ri < len(rawMsgs) {
			raw = rawMsgs[ri]
		}
		m.Keys = append(m.Keys, hashJSON(raw))
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
		m.SysHash = md5.Sum([]byte(sysText.String()))
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

// lastEndpoint mirrors internal/report/detail.go's helper of the same name
// (duplicated, not imported: report must stay ctxgraph's dependent, never
// the other way around — see internal/archtest).
func lastEndpoint(rec *audit.Record) string {
	if len(rec.Attempts) == 0 {
		return "-"
	}
	return rec.Attempts[len(rec.Attempts)-1].Endpoint
}
