// Ver 2026-07-29 11:00, by Sonnet 5

package story

import (
	"vmr/internal/audit"
	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
	"vmr/internal/story/profile"
)

// ID returns a Lineage's stable, content-addressed Journey identifier
// without doing the full Build — Manifest-level only, so listing
// candidates never has to fetch every request in every lineage just to
// print their ids.
func ID(l *ctxgraph.Lineage) string { return deriveID(l) }

// PreviewTitle returns what a Journey's title would be, fetching only the
// lineage's ROOT record (not every manifest's, unlike Build) — cheap enough
// to call once, but see PreviewTitles when calling this for every candidate
// in a listing: one FetchRecords per lineage forces one full-file scan per
// lineage even when many lineages' roots share the same source file
// (design-doc review §1.2).
func PreviewTitle(l *ctxgraph.Lineage, prof profile.Profile) (string, error) {
	root := l.Manifests[0]
	loc := ctxgraph.Loc{Path: root.Path, Line: root.Line}
	recs, err := ctxgraph.FetchRecords([]ctxgraph.Loc{loc})
	if err != nil {
		return "", err
	}
	return titleFromRecord(recs[loc], prof), nil
}

// PreviewTitles is PreviewTitle for many lineages at once, batching every
// lineage's root-record fetch into one ctxgraph.FetchRecords call —
// FetchRecords already groups its work by source file (zstd isn't seekable,
// so each file is scanned at most once regardless of how many lines are
// wanted from it), so this turns what used to be one full-file scan PER
// CANDIDATE LINEAGE into one full-file scan per FILE — the fix for
// design-doc review §1.2 ("listJourneys without -journey is much slower
// than expected: it re-scans a source file once per candidate rooted in
// it"). Order of the returned map's iteration is undefined; callers keep
// their own ls slice for display order.
func PreviewTitles(ls []*ctxgraph.Lineage, prof profile.Profile) (map[*ctxgraph.Lineage]string, error) {
	locs := make([]ctxgraph.Loc, len(ls))
	for i, l := range ls {
		root := l.Manifests[0]
		locs[i] = ctxgraph.Loc{Path: root.Path, Line: root.Line}
	}
	recs, err := ctxgraph.FetchRecords(locs)
	if err != nil {
		return nil, err
	}
	out := make(map[*ctxgraph.Lineage]string, len(ls))
	for i, l := range ls {
		out[l] = titleFromRecord(recs[locs[i]], prof)
	}
	return out, nil
}

func titleFromRecord(rec *audit.Record, prof profile.Profile) string {
	if rec == nil {
		return "(无法读取)"
	}
	body, ok := rec.Client.Request.Body.(map[string]any)
	if !ok {
		return "(无标题)"
	}
	msgs := chatmsg.Messages(body)
	rawMsgs, _ := body["messages"].([]any)
	off := chatmsg.MsgOffset(body)
	for idx, m := range msgs {
		if m.Role != "user" {
			continue
		}
		if text, ok := prof.RealUserText(m, rawMsgs, idx-off); ok {
			return preview(text)
		}
	}
	return "(无标题)"
}
