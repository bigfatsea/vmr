// Ver 2026-08-01, by Sonnet 5

// The redesigned per-request drill-down: vmr-requests.json (the data)
// + vmr-requests.md (a pure index) + one fully-detailed sibling per group
// (vmr-requests-<tag>.md per real Chat User, vmr-requests-unresolved.md for
// sessions with no client_key_tag, vmr-requests-cron-<tag>.md per scheduled
// class). The index used to duplicate every group's full detail inline
// *and* export it again to its own sibling file; splitting index from
// detail means each request's full drill-down (session/task/turn cards, or
// the scheduled flat table) is written exactly once, in exactly one file.
// A single-shot scheduled session (heartbeat/dream_diary, exactly one
// request) never gets its own Chat User card — regardless of which client
// tag issued it — it folds into its class's dedicated cron-<tag> file
// instead, so twenty near-identical poll turns don't drown a real
// conversation and don't appear twice under two different groupings.
// All displayed timestamps are rendered in fmtutil.DisplayZone (the system
// default timezone) regardless of the source record's own offset.

package report

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"vmr/internal/fmtutil"
	"vmr/internal/i18n"
	"vmr/internal/reqdetail"
)

// RequestsIndex is vmr-requests.json's whole shape: one row per request.
// The parse cache used to live here too, as a "files" section (see
// ctxgraph.FileCache/ScanCached) — it's since moved to its own
// content-hash-sharded directory shared with internal/story
// (ctxgraph.LoadCacheDir/SaveCacheDir, {outDir}/.parse-cache), so this
// index stays purely human-scale.
// vmr-requests-failed.jsonl stays a plain flat JSONL — it's a filtered
// dump of Requests, not itself an independent cache.
type RequestsIndex struct {
	Requests []RequestRow `json:"requests"`
}

// WriteRequestsJSON writes vmr-requests.json — RequestsIndex's rows only;
// the parse cache is persisted separately (see RequestsIndex's doc
// comment).
func WriteRequestsJSON(rows []RequestRow, path string) (n int, err error) {
	idx := RequestsIndex{Requests: rows}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// WriteRequestsJSONL writes one RequestRow per line — used for
// vmr-requests-failed.jsonl, a filtered dump with no cache section of its
// own (see RequestsIndex's doc comment).
func WriteRequestsJSONL(rows []RequestRow, path string) (n int, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() {
		// Close can be where a full disk's delayed write failure actually
		// surfaces (Flush below only pushes into the OS buffer) — a plain
		// `defer f.Close()` would swallow that and report "success" over an
		// incomplete file.
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	bw := bufio.NewWriter(f)
	for _, r := range rows {
		data, merr := json.Marshal(r)
		if merr != nil {
			return 0, merr
		}
		bw.Write(data)
		bw.WriteByte('\n')
	}
	if err = bw.Flush(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// cronFileTag maps a scheduled workload class to the suffix in its
// vmr-requests-cron-<class>.md filename.
func cronFileTag(class string) string {
	return sanitize(class)
}

// indexEntry is one line item in vmr-requests.md's index: a group header
// (Chat User or scheduled class), the sibling file it links to, and the
// summary stats shown in the blockquote above that link.
type indexEntry struct {
	header  string
	file    string
	summary tagSummaryData
}

// WriteRequestsIndex writes vmr-requests.md (a pure per-group index: header
// + one-line summary + link to that group's sibling file) and one
// fully-detailed sibling per group: vmr-requests-<tag>.md per real client_key_tag,
// vmr-requests-unresolved.md for sessions carrying no tag,
// vmr-requests-cron-<tag>.md per scheduled class. Titles come from sess.
// journeyLink (P6.2c) maps a session's id (now a Lineage's content-
// addressed LineageID, see SessionInfo.ID) to a rendered journey-<id>.md
// path, when `vmr story` has already produced one in this same output
// root — nil/empty when it hasn't, in which case no session card grows a
// journey link (this is the "指向另一条命令聚合产物" edge class, so it's
// existence-gated by the caller, not something this package computes).
func WriteRequestsIndex(rep *Report2, sess *SessionAnalysis, dir string, lang i18n.Lang, journeyLink map[string]string, detailDir string) error {
	t := i18n.Requests(lang)
	detailSet := buildDetailFileSet(detailDir)
	rows := rep.RequestRows()
	sessionTitle, sessionAlias, taskTitle := titleMaps(sess)
	sessionMeta := map[string]SessionRow{}
	for _, s := range rep.Sessions {
		sessionMeta[s.ID] = s
	}
	clientOrder := make([]string, 0, len(rep.ByClient))
	for _, c := range rep.ByClient {
		clientOrder = append(clientOrder, c.ClientKey)
	}

	chatUser, scheduled, scheduledOrder := partitionGroups(rows, sessionMeta)
	chatUserOrder := append([]string(nil), clientOrder...)
	for k := range chatUser {
		found := false
		for _, o := range chatUserOrder {
			if o == k {
				found = true
				break
			}
		}
		if !found {
			chatUserOrder = append(chatUserOrder, k)
		}
	}

	var entries []indexEntry

	// Chat User siblings: real tags (clientOrder) first, then "(unresolved)".
	for _, ck := range chatUserOrder {
		groups := chatUser[ck]
		if len(groups) == 0 {
			continue
		}
		var tasks, turns int
		var grows []RequestRow
		for _, g := range groups {
			tasks += g.tasks
			turns += g.requests
			grows = append(grows, g.rows...)
		}
		header := t.ChatUserHeader(ck, len(groups), tasks, turns)
		file := "vmr-requests-" + sanitize(ck) + ".md"
		content := renderChatUserDoc(header, groups, sessionTitle, sessionAlias, taskTitle, journeyLink, t, detailSet)
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
			return err
		}
		entries = append(entries, indexEntry{header, file, tagSummary(grows)})
	}

	// Scheduled-class siblings.
	for _, cls := range scheduledOrder {
		occ := append([]RequestRow(nil), scheduled[cls]...)
		sort.SliceStable(occ, func(i, j int) bool { return occ[i].TS < occ[j].TS })
		header := t.CronHeader(cls, len(occ))
		file := "vmr-requests-cron-" + cronFileTag(cls) + ".md"
		content := renderScheduledDoc(header, occ, t, detailSet)
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
			return err
		}
		entries = append(entries, indexEntry{header, file, tagSummary(occ)})
	}

	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	w("# %s\n\n", t.IndexTitle)
	for _, e := range entries {
		w("## %s\n\n", e.header)
		w("%s", t.GroupSummary(e.summary.requests, pctStr2(e.summary.ok, e.summary.requests),
			fmtutil.FmtTokens(e.summary.fresh), fmtutil.FmtTokens(e.summary.cached), fmtutil.FmtTokens(e.summary.out),
			pctStr(e.summary.cacheEff)))
		w("%s", t.GroupDetailLink(e.file))
	}
	return os.WriteFile(filepath.Join(dir, "vmr-requests.md"), []byte(b.String()), 0o600)
}

// tagSummaryData is the one-line blockquote's basis: request count, success
// rate, fresh/cached/out tokens, and cache efficiency over one group's rows.
type tagSummaryData struct {
	requests    int
	ok          int
	fresh       int64
	cached      int64
	out         int64
	cacheEff    float64
	tokensKnown int
}

func tagSummary(rows []RequestRow) tagSummaryData {
	var s tagSummaryData
	for _, r := range rows {
		s.requests++
		if r.Outcome == "ok" {
			s.ok++
		}
		if r.TokensIn > 0 {
			s.fresh += r.TokensInFresh
			s.cached += r.TokensInCached
			s.out += r.TokensOut
			s.tokensKnown++
		}
	}
	if s.tokensKnown > 0 {
		s.cacheEff = cacheEff(s.cached, s.fresh)
	}
	return s
}

func titleMaps(sess *SessionAnalysis) (sessionTitle, sessionAlias, taskTitle map[string]string) {
	sessionTitle = map[string]string{}
	sessionAlias = map[string]string{}
	taskTitle = map[string]string{}
	if sess == nil {
		return
	}
	for _, s := range sess.Sessions {
		sessionTitle[s.ID] = s.Title
		sessionAlias[s.ID] = s.DisplayAlias
		for _, t := range s.Tasks {
			taskTitle[s.ID+"\x00"+t.ID] = t.Title
		}
	}
	return
}

// sessGroup is one session's rows plus the grouping metadata (class/counts)
// needed to decide where it renders: an individual Chat User card, or folded
// into a scheduled-class rollup.
type sessGroup struct {
	id        string
	class     string
	requests  int
	tasks     int
	clientKey string
	rows      []RequestRow
}

// groupSessions partitions rows into per-session groups, in first-seen
// (i.e. timestamp) order — rows arrive already ts-ordered from Build.
//
// Grouping keys off more than just r.Session: every *real* session belongs
// to exactly one client_key (verified invariant), so keying on Session alone
// is safe there — but ungrouped rows (rejected/non-chat requests, r.Session
// == "") from *different* clients would otherwise all collide on the same
// "" key and merge into one group, silently misattributing one client's
// ungrouped requests to whichever other client's ungrouped row happened to
// be seen first. Keying on (Session, ClientKey) keeps them apart while still
// merging same-client ungrouped rows into one unrouted card, same as before.
func groupSessions(rows []RequestRow, sessionMeta map[string]SessionRow) ([]string, map[string]*sessGroup) {
	bySession := map[string]*sessGroup{}
	var order []string
	for _, r := range rows {
		key := r.Session + "\x00" + r.ClientKey
		g, ok := bySession[key]
		if !ok {
			meta := sessionMeta[r.Session]
			class := meta.Class
			if class == "" {
				class = "interactive"
			}
			g = &sessGroup{id: r.Session, class: class, requests: meta.Requests, tasks: meta.Tasks, clientKey: r.ClientKey}
			bySession[key] = g
			order = append(order, key)
		}
		g.rows = append(g.rows, r)
	}
	for _, sid := range order {
		g := bySession[sid]
		if g.requests == 0 {
			g.requests = len(g.rows)
		}
		if g.tasks == 0 {
			tset := map[string]bool{}
			for _, r := range g.rows {
				tset[r.Task] = true
			}
			g.tasks = len(tset)
		}
		sort.SliceStable(g.rows, func(i, j int) bool { return g.rows[i].SessTurn < g.rows[j].SessTurn })
	}
	return order, bySession
}

// partitionGroups splits rows into scheduled-class rollups and Chat User
// groups, exactly as WriteRequestsIndex does: a single-shot (requests==1)
// non-interactive session belongs to its class's scheduled rollup
// regardless of client; everything else — interactive sessions, any
// multi-turn scheduled session, and any ungrouped row — is an individual
// card grouped by Chat User (client_key, "(unresolved)" when empty). Shared
// with WriteRequestsIndex so the index's per-client link list only ever
// points at a client tag that actually got a vmr-requests-<tag>.md sibling
// written — a client whose only traffic was single-shot scheduled requests
// gets no sibling file at all.
func partitionGroups(rows []RequestRow, sessionMeta map[string]SessionRow) (chatUser map[string][]*sessGroup, scheduled map[string][]RequestRow, scheduledOrder []string) {
	order, bySession := groupSessions(rows, sessionMeta)
	scheduled = map[string][]RequestRow{}
	chatUser = map[string][]*sessGroup{}
	for _, sid := range order {
		g := bySession[sid]
		if g.id != "" && g.class != "interactive" && g.requests == 1 {
			if _, ok := scheduled[g.class]; !ok {
				scheduledOrder = append(scheduledOrder, g.class)
			}
			scheduled[g.class] = append(scheduled[g.class], g.rows...)
			continue
		}
		key := g.clientKey
		if key == "" {
			key = "(unresolved)"
		}
		chatUser[key] = append(chatUser[key], g)
	}
	return
}

// clientsWithSiblingFile returns the set of real client_key_tags that get
// their own vmr-requests-<tag>.md — i.e. every tag in partitionGroups'
// chatUser result except the synthetic "(unresolved)" bucket.
func clientsWithSiblingFile(rep *Report2) map[string]bool {
	sessionMeta := map[string]SessionRow{}
	for _, s := range rep.Sessions {
		sessionMeta[s.ID] = s
	}
	chatUser, _, _ := partitionGroups(rep.RequestRows(), sessionMeta)
	out := map[string]bool{}
	for k := range chatUser {
		if k != "(unresolved)" {
			out[k] = true
		}
	}
	return out
}

// renderChatUserDoc renders one Chat User's full detail doc: an H1 header,
// a legend, then one session card per session (renderSessionCard).
func renderChatUserDoc(header string, groups []*sessGroup, sessionTitle, sessionAlias, taskTitle, journeyLink map[string]string, t i18n.RequestsText, detailSet map[string]struct{}) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	w("# %s\n\n", header)
	w("%s", t.ChatUserLegend)
	w("---\n\n")
	for _, g := range groups {
		renderSessionCard(w, g, sessionTitle, sessionAlias, taskTitle, journeyLink, t, detailSet)
	}
	return b.String()
}

// renderScheduledDoc renders one scheduled class's full detail doc: an H1
// header, a summary blockquote, then a flat chronological table — the same
// shape the old collapsed rollup section used, just as its own file.
func renderScheduledDoc(header string, occ []RequestRow, t i18n.RequestsText, detailSet map[string]struct{}) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	w("# %s\n\n", header)
	ok := 0
	var fresh, cached, out int64
	for _, r := range occ {
		if r.Outcome == "ok" {
			ok++
		}
		fresh += r.TokensInFresh
		cached += r.TokensInCached
		out += r.TokensOut
	}
	w("%s", t.ScheduledSummary(pctStr2(ok, len(occ)), fmtutil.FmtTokens(fresh), fmtutil.FmtTokens(cached), fmtutil.FmtTokens(out)))
	w("%s", t.ScheduledTableHeader)
	for _, r := range occ {
		w("| %s | %s | %s | %s | %s | %s |\n",
			fmtDisplayFull(r.TS), finishCell(r), fmtDurMS(r.DurMS), freshCachedOut(r), cacheEffTurn(r), detailCell(r, detailSet))
	}
	w("\n")
	return b.String()
}

// FailedRequestRows filters rows down to the error-analysis surface: outcome
// "error" (upstream/vmr rejected the request), "canceled" (client hung up
// mid-request), and "ok" rows with Truncated==true (client got a 2xx but the
// stream broke off mid-response — a usable response was still not fully
// delivered).
func FailedRequestRows(rows []RequestRow) []RequestRow {
	var out []RequestRow
	for _, r := range rows {
		if r.Outcome == "error" || r.Outcome == "canceled" || (r.Outcome == "ok" && r.Truncated) {
			out = append(out, r)
		}
	}
	return out
}

// WriteFailedIndex writes vmr-requests-failed.md: a flat, time-ordered index
// of every failed request (FailedRequestRows), each row's "文件" column a
// detailCell (a details/*.md link when the target actually exists on disk,
// else the req coordinate — see detailCell's own doc comment, P13.4). This
// is a dedicated error-analysis index — it does not remove or alter failed
// requests anywhere else; vmr-requests.md and every per-group sibling keep
// listing them exactly as before.
func WriteFailedIndex(rows []RequestRow, dir string, lang i18n.Lang, detailDir string) error {
	detailSet := buildDetailFileSet(detailDir)
	t := i18n.Requests(lang)
	failed := FailedRequestRows(rows)
	sort.SliceStable(failed, func(i, j int) bool { return failed[i].TS < failed[j].TS })

	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	w("# %s\n\n", t.FailedIndexTitle)
	w("%s", t.FailedIndexIntro(len(failed)))
	if len(failed) == 0 {
		return os.WriteFile(filepath.Join(dir, "vmr-requests-failed.md"), []byte(b.String()), 0o600)
	}
	w("%s", t.FailedTableHeader)
	for _, r := range failed {
		w("| %s | %s | %s/%s | %s | %s | %s |\n",
			fmtDisplayFull(r.TS), sessTaskCell(r), r.Protocol, orDashModel(r.Model),
			outcomeCell(r), fmtDurMS(r.DurMS), detailCell(r, detailSet))
	}
	return os.WriteFile(filepath.Join(dir, "vmr-requests-failed.md"), []byte(b.String()), 0o600)
}

// renderSessionCard renders one session card ("## sNN · ts · N tasks M
// turns"): one task sub-header per task, each followed by a one-line quote
// of the task's opening message and its per-turn table.
func renderSessionCard(w func(string, ...any), g *sessGroup, sessionTitle, sessionAlias, taskTitle, journeyLink map[string]string, t i18n.RequestsText, detailSet map[string]struct{}) {
	label := g.id
	if label == "" {
		label = t.Unrouted
	} else if alias := sessionAlias[g.id]; alias != "" {
		// g.id is now content-addressed (l-<hash8>) — show the old s%02d
		// alias alongside it for at-a-glance scannability within this one
		// report; g.id itself stays the header's primary label since it's
		// also this card's join key against story's Journey index (P6.2).
		label = alias + " (" + g.id + ")"
	}
	classNote := ""
	if g.class != "interactive" {
		classNote = " · " + g.class
	}
	w("%s", t.SessionCardHeader(label, fmtDisplayFull(g.rows[0].TS), g.tasks, g.requests, classNote))
	if journey := journeyLink[g.id]; journey != "" {
		// journey is a bare "journey-<id>.md" filename (JourneyIndexRow.
		// Rendered) — vmr-requests-<tag>.md lives directly in {outDir},
		// one level ABOVE stories/, so the relative link descends into
		// it rather than climbing out (the opposite direction from
		// details/'s and evidence/'s "../" links — those two are
		// siblings of stories/ under the same {outDir}).
		w("%s", t.JourneyLinkLine("stories/"+journey))
	}

	byTask := map[string][]RequestRow{}
	var taskOrder []string
	for _, r := range g.rows {
		if _, ok := byTask[r.Task]; !ok {
			taskOrder = append(taskOrder, r.Task)
		}
		byTask[r.Task] = append(byTask[r.Task], r)
	}
	for _, tk := range taskOrder {
		trows := byTask[tk]
		sort.SliceStable(trows, func(i, j int) bool { return trows[i].Turn < trows[j].Turn })
		tLabel := tk
		if tLabel == "" {
			tLabel = t.Unrouted
		}
		w("%s", t.TaskHeader(tLabel, fmtDisplayFull(trows[0].TS), len(trows)))
		title := taskTitle[g.id+"\x00"+tk]
		if title == "" {
			title = sessionTitle[g.id]
		}
		if title != "" {
			// Free-form user text in a blockquote: HTML-escape so an
			// unclosed "<!--" can't swallow the rest of the file (B4).
			w("> %s\n\n", reqdetail.EscapeHTML(title))
		}
		w("%s", t.TurnTableHeader)
		for _, r := range trows {
			w("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				turnCell(r.Turn), fmtDisplayTime(r.TS), msgsCell(r.Msgs),
				finishCell(r), fmtDurMS(r.DurMS), msOrDash(r.TTFTMS),
				freshCachedOut(r), cacheEffTurn(r), detailCell(r, detailSet))
		}
		w("\n")
	}
}

// fmtDisplayFull renders a RequestRow.TS (RFC3339, source offset) as
// "2026-07-24 00:17:58" in fmtutil.DisplayZone (the system default
// timezone), regardless of the record's own embedded offset. Falls back to
// a raw cut when the timestamp doesn't parse (defensive; Build always
// writes RFC3339).
func fmtDisplayFull(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return cut(ts, 19)
	}
	return t.In(fmtutil.DisplayZone).Format("2006-01-02 15:04:05")
}

// fmtDisplayTime is fmtDisplayFull but time-only ("00:17:58"), for per-turn table
// cells where the enclosing session/task header already carries the date.
func fmtDisplayTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return cut(ts, 19)
	}
	return t.In(fmtutil.DisplayZone).Format("15:04:05")
}

func orDashModel(m string) string {
	if m == "" {
		return "-"
	}
	return m
}

func turnCell(t int) string {
	if t == 0 {
		return "-"
	}
	return strconv.Itoa(t)
}

func msgsCell(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func msOrDash(v int64) string {
	if v <= 0 {
		return "-"
	}
	return fmtDurMS(v)
}

func finishCell(r RequestRow) string {
	if r.Outcome != "ok" {
		// An error/canceled turn with no ErrorClass (pre-routing reject:
		// unreadable/oversized body, missing model; or a cancel while
		// queued for a slot — none of these ever reach an upstream, so
		// nothing classifies them) would otherwise fall through to a bare
		// "-" and read as a normal finish. Mirror outcomeCell's fallback.
		ec := r.ErrorClass
		if ec == "" {
			ec = "unclassified"
		}
		return "❌" + ec
	}
	if r.Truncated {
		return "⚠️trunc"
	}
	return orDash(r.Finish)
}

func freshCachedOut(r RequestRow) string {
	if r.TokensIn == 0 && r.TokensOut == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s / %s", fmtutil.FmtTokens(r.TokensInFresh), fmtutil.FmtTokens(r.TokensInCached), fmtutil.FmtTokens(r.TokensOut))
}

func cacheEffTurn(r RequestRow) string {
	if r.TokensIn == 0 {
		return "-"
	}
	return pctStr(r.CacheEff)
}

// buildDetailFileSet lists detailDir once and returns its .md basenames as
// a set — detailCell's existence check (P13.4) would otherwise be one
// os.Stat per row, and a full request index re-renders every row across
// several tables (per-session cards, the scheduled rollup, the failed
// index): a real full-corpus run (11k+ requests,
// story_report_full_review_opus-5.md's M12) would issue 20,000+ redundant
// stat syscalls — most of them ENOENT lookups against an empty or
// nonexistent directory on the common (default suite) path — for
// information one os.ReadDir already gives in full. A missing directory
// returns a nil (empty) set, not an error. Hidden files (dotfiles, a
// stray .reqdetail-*.tmp from an interrupted atomic write) and non-.md
// entries are excluded so they can never be mistaken for a real detail
// page's presence.
func buildDetailFileSet(detailDir string) map[string]struct{} {
	entries, err := os.ReadDir(detailDir)
	if err != nil {
		return nil
	}
	set := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		set[name] = struct{}{}
	}
	return set
}

// detailCell renders the "文件" column. detailSet (buildDetailFileSet) is
// {outDir}/details' current contents, always computed regardless of
// whether this run's -details flag was passed — the criterion is whether
// r.DetailFile actually exists right now, not whether a flag was set:
// since `vmr analyze` runs the story half (which may batch-materialize
// details for -render-all) before the report half, a flag-only check
// could claim "no details were written"
// while 306 of them sit on disk, or the reverse. r.DetailFile itself is
// always computed (session.go, a pure function of the record's identity)
// whether or not anything was ever written to it — see FileName's own doc
// comment for why an existence check, not the filename's mere presence, is
// what's authoritative here. When the target isn't there, this falls back
// to r.Req, the record's stable req coordinate (basename:line,
// ctxgraph.ReqCoord) as inline code, letting the reader fetch it on demand
// (`vmr replay -req COORD -print`) without ever producing a dead link.
func detailCell(r RequestRow, detailSet map[string]struct{}) string {
	if r.DetailFile != "" {
		if _, ok := detailSet[r.DetailFile]; ok {
			label := r.Req
			if label == "" {
				label = r.DetailFile
			}
			return fmt.Sprintf("[%s](details/%s)", label, r.DetailFile)
		}
	}
	if r.Req == "" {
		return "-"
	}
	return "`" + r.Req + "`"
}

func sessTaskCell(r RequestRow) string {
	if r.Session == "" {
		return "-"
	}
	if r.Task == "" {
		return r.Session
	}
	return r.Session + "/" + r.Task
}

func outcomeCell(r RequestRow) string {
	switch r.Outcome {
	case "ok":
		if r.Truncated {
			return "ok⚠️trunc"
		}
		return "ok"
	case "canceled":
		return "canceled"
	default:
		ec := r.ErrorClass
		if ec == "" {
			ec = "unclassified"
		}
		return "❌" + ec
	}
}
