// Ver 2026-07-28 22:30, by Sonnet 5

package ctxgraph

import (
	"encoding/json"

	"vmr/internal/audit"
	"vmr/internal/chatmsg"
)

// BlobRef locates a message's first appearance: which audit file, which
// line within it, and which position in that request's chatmsg.Messages()
// output. Content is never held in memory alongside the hash — only this
// coordinate is, so indexing a multi-GB corpus costs bytes, not gigabytes
// (design doc §2.4/F12: manifests + this index together are tens of MB for
// the full 15-day/7112-record corpus).
type BlobRef struct {
	Path string
	Line int
	Idx  int
}

// BlobIndex maps a message hash to where it first appeared. Built once
// during Scan's serial merge phase (single-threaded, so no locking needed —
// see scan.go) and read afterward by FetchAll to recover original content
// on demand for rendering.
type BlobIndex struct {
	refs map[Hash]BlobRef
}

// newBlobIndex returns an empty index ready for firstSeen.
func newBlobIndex() *BlobIndex {
	return &BlobIndex{refs: map[Hash]BlobRef{}}
}

// firstSeen records ref for h if h has never been seen before — later
// (path,line,idx) triples for the same content are cheaper duplicates and
// carry no new information, so the first is kept and the rest are dropped.
func (b *BlobIndex) firstSeen(h Hash, ref BlobRef) {
	if _, ok := b.refs[h]; !ok {
		b.refs[h] = ref
	}
}

// Lookup returns h's recorded location, if any.
func (b *BlobIndex) Lookup(h Hash) (BlobRef, bool) {
	ref, ok := b.refs[h]
	return ref, ok
}

// Len reports how many distinct blobs are indexed.
func (b *BlobIndex) Len() int { return len(b.refs) }

// FetchAll resolves a set of hashes to their original chatmsg.Message,
// reading each source audit file at most once no matter how many requested
// hashes point into it (grouped by Path, then by Line within that file) —
// zstd files aren't seekable, so "read the file once, pull everything
// needed out of this one pass" is the only efficient access pattern (design
// doc §2.4). Hashes with no recorded ref, or whose ref points at a body
// that no longer parses as a chat object (should not happen for anything
// BuildManifest itself produced a hash from, but Fetch doesn't assume that
// invariant holds forever), are silently omitted from the result — callers
// must treat a missing key as "content unavailable", not as an error.
func (idx *BlobIndex) FetchAll(hashes []Hash) (map[Hash]chatmsg.Message, error) {
	byPath := map[string]map[int][]wantEntry{}
	for _, h := range hashes {
		ref, ok := idx.refs[h]
		if !ok {
			continue
		}
		lines := byPath[ref.Path]
		if lines == nil {
			lines = map[int][]wantEntry{}
			byPath[ref.Path] = lines
		}
		lines[ref.Line] = append(lines[ref.Line], wantEntry{msgIdx: ref.Idx, hash: h})
	}

	out := map[Hash]chatmsg.Message{}
	for path, wantedLines := range byPath {
		if err := fetchFromFile(path, wantedLines, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// wantEntry is one requested (message-index, hash) pair within a specific
// source line — see FetchAll.
type wantEntry struct {
	msgIdx int
	hash   Hash
}

func fetchFromFile(path string, wantedLines map[int][]wantEntry, out map[Hash]chatmsg.Message) error {
	rc, err := audit.OpenLogFile(path)
	if err != nil {
		return err
	}
	defer rc.Close()

	line := 0
	return audit.ForEachLine(rc, audit.MaxLogLine, func(lineBytes []byte) {
		line++
		wants, needed := wantedLines[line]
		if !needed {
			return
		}
		var rec audit.Record
		if json.Unmarshal(lineBytes, &rec) != nil {
			return
		}
		body, ok := rec.Client.Request.Body.(map[string]any)
		if !ok {
			return
		}
		msgs := chatmsg.Messages(body)
		for _, w := range wants {
			if w.msgIdx >= 0 && w.msgIdx < len(msgs) {
				out[w.hash] = msgs[w.msgIdx]
			}
		}
	}, func() { line++ })
}
