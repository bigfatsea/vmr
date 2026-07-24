// Ver 2026-07-25, report2

// The redesigned per-request drill-down (V2 §7): vmr-requests.jsonl (the data)
// + vmr-requests.md (the view, grouped Chat User -> Session -> Task -> Turn).
// Sessions are partitioned two ways: an interactive (or multi-turn scheduled)
// session becomes an individual card under its Chat User (client_key)
// section; a single-shot scheduled session (heartbeat/dream_diary, exactly
// one request) collapses into a global "定时任务" rollup, one flat table per
// class, so twenty near-identical poll turns don't drown the real
// conversations. The footer "全部请求（时间序）" always carries every row.
// All displayed timestamps are rendered in UTC+8 regardless of the source
// record's own offset. Per-tag siblings reuse the exact same renderer on a
// pre-filtered row set, so each is self-contained without the main report.

package report2

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

	"vmr/internal/report"
)

// utc8 is China Standard Time — no DST since 1991, so a fixed offset is
// always correct and (unlike time.LoadLocation) never depends on the host
// having a tzdata database installed.
var utc8 = time.FixedZone("UTC+8", 8*3600)

// WriteRequestsJSONL writes one RequestRow per line to vmr-requests.jsonl.
func WriteRequestsJSONL(rows []RequestRow, path string) (int, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	for _, r := range rows {
		data, err := json.Marshal(r)
		if err != nil {
			return 0, err
		}
		bw.Write(data)
		bw.WriteByte('\n')
	}
	if err := bw.Flush(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

// WriteRequestsIndex writes vmr-requests.md (the redesigned Chat User ->
// Session -> Task -> Turn view) plus one vmr-requests-<tag>.md sibling per
// distinct non-empty ClientKey, each pre-filtered to that client's rows and
// carrying a one-line summary header. Titles come from sess.
func WriteRequestsIndex(rep *Report2, sess *report.SessionAnalysis, dir string) error {
	rows := rep.RequestRows()
	sessionTitle, taskTitle := titleMaps(sess)
	sessionMeta := map[string]SessionRow{}
	for _, s := range rep.Sessions {
		sessionMeta[s.ID] = s
	}
	clientOrder := make([]string, 0, len(rep.ByClient))
	for _, c := range rep.ByClient {
		clientOrder = append(clientOrder, c.ClientKey)
	}

	main := renderIndex("VMR 请求详单", rows, sessionTitle, taskTitle, sessionMeta, clientOrder, nil)
	if err := os.WriteFile(filepath.Join(dir, "vmr-requests.md"), []byte(main), 0o600); err != nil {
		return err
	}
	// per-tag siblings
	tags := distinctTags(rows)
	for _, tag := range tags {
		filtered := filterByTag(rows, tag)
		summary := tagSummary(filtered, tag)
		title := fmt.Sprintf("VMR 请求详单（client: %s）", tag)
		content := renderIndex(title, filtered, sessionTitle, taskTitle, sessionMeta, []string{tag}, &summary)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("vmr-requests-%s.md", sanitize(tag))), []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

type tagSummaryData struct {
	tag         string
	requests    int
	ok          int
	fresh       int64
	cacheEff    float64
	tokensKnown int
}

func tagSummary(rows []RequestRow, tag string) tagSummaryData {
	s := tagSummaryData{tag: tag}
	for _, r := range rows {
		s.requests++
		if r.Outcome == "ok" {
			s.ok++
		}
		if r.TokensIn > 0 {
			s.fresh += r.TokensInFresh
			s.tokensKnown++
		}
	}
	if s.tokensKnown > 0 {
		var cached int64
		for _, r := range rows {
			cached += r.TokensInCached
		}
		s.cacheEff = cacheEff(cached, s.fresh)
	}
	return s
}

func distinctTags(rows []RequestRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if r.ClientKey != "" && !seen[r.ClientKey] {
			seen[r.ClientKey] = true
			out = append(out, r.ClientKey)
		}
	}
	sort.Strings(out)
	return out
}

func filterByTag(rows []RequestRow, tag string) []RequestRow {
	var out []RequestRow
	for _, r := range rows {
		if r.ClientKey == tag {
			out = append(out, r)
		}
	}
	return out
}

func titleMaps(sess *report.SessionAnalysis) (sessionTitle, taskTitle map[string]string) {
	sessionTitle = map[string]string{}
	taskTitle = map[string]string{}
	if sess == nil {
		return
	}
	for _, s := range sess.Sessions {
		sessionTitle[s.ID] = s.Title
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
// merging same-client ungrouped rows into one "（未分组）" card, same as
// before.
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

// renderIndex builds the grouped Markdown for one row set (the main file
// gets all rows; a per-tag sibling gets a pre-filtered subset). If summary
// is non-nil, a one-line summary blockquote replaces the generic legend.
func renderIndex(title string, rows []RequestRow, sessionTitle, taskTitle map[string]string,
	sessionMeta map[string]SessionRow, clientOrder []string, summary *tagSummaryData) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# %s\n\n", title)
	if summary != nil {
		w("> 请求 %d · 成功率 %s · fresh %s · 缓存效率 %s\n\n",
			summary.requests, pctStr2(summary.ok, summary.requests),
			fmtTokens(summary.fresh), pctStr(summary.cacheEff))
	} else {
		w("层级: Chat User -> Session -> Task -> Turn（时间均为 UTC+8）。每轮表列: 轮 | 时间 | msgs | finish | dur | ttft | fresh/cached/out | cache-eff⭐ | 文件\n\n")
	}
	w("---\n\n")

	order, bySession := groupSessions(rows, sessionMeta)

	// Partition: a single-shot (requests==1) non-interactive session folds
	// into its class's global rollup; everything else — interactive
	// sessions, and any multi-turn scheduled session — is an individual
	// card grouped by Chat User (client_key).
	scheduled := map[string][]RequestRow{}
	var scheduledOrder []string
	chatUser := map[string][]*sessGroup{}
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

	// Chat User sections: clientOrder first, then any client_key present in
	// the data but missing from it (defensive — shouldn't happen since
	// clientOrder is derived from the same Build pass).
	order2 := append([]string(nil), clientOrder...)
	for k := range chatUser {
		found := false
		for _, o := range order2 {
			if o == k {
				found = true
				break
			}
		}
		if !found {
			order2 = append(order2, k)
		}
	}
	for _, ck := range order2 {
		groups := chatUser[ck]
		if len(groups) == 0 {
			continue
		}
		var tasks, turns int
		for _, g := range groups {
			tasks += g.tasks
			turns += g.requests
		}
		w("# Chat User: %s · %d 会话 %d 任务 %d 轮\n\n", ck, len(groups), tasks, turns)
		for _, g := range groups {
			renderSessionCard(w, g, sessionTitle, taskTitle)
		}
		w("---\n\n")
	}

	// Scheduled rollups: one flat table per class, chronological.
	for _, cls := range scheduledOrder {
		occ := scheduled[cls]
		sort.SliceStable(occ, func(i, j int) bool { return occ[i].TS < occ[j].TS })
		w("# 定时任务 · %s 单发会话 × %d\n\n", cls, len(occ))
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
		w("> 成功率 %s · fresh %s / cached %s / out %s\n\n",
			pctStr2(ok, len(occ)), fmtTokens(fresh), fmtTokens(cached), fmtTokens(out))
		w("| 时间 | finish | dur | fresh/cached/out | cache-eff⭐ | 文件 |\n|---|---|---|---|---|---|\n")
		for _, r := range occ {
			w("| %s | %s | %s | %s | %s | %s |\n",
				fmtUTC8Full(r.TS), finishCell(r), ms(r.DurMS), tokensTriple(r), cacheEffTurn(r), detailLink(r.DetailFile))
		}
		w("\n---\n\n")
	}

	// footer: all requests in time order
	w("# 全部请求（时间序）\n\n")
	all := append([]RequestRow(nil), rows...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].TS < all[j].TS })
	w("| 时间 | 会话/任务 | VM/API | outcome⭐ | dur | fresh/cached/out | cache-eff⭐ | 文件 |\n|---|---|---|---|---|---|---|---|\n")
	for _, r := range all {
		w("| %s | %s | %s/%s | %s | %s | %s | %s | %s |\n",
			fmtUTC8Full(r.TS), sessTaskCell(r), r.Protocol, orDashModel(r.Model),
			outcomeCell(r), ms(r.DurMS), tokensTriple(r), cacheEffTurn(r), detailLink(r.DetailFile))
	}
	w("\n")
	return b.String()
}

// renderSessionCard renders one "## sNN · ts · N任务N轮" session card: a
// "### tNN · ts · N轮" sub-header per task, each followed by a one-line
// quote of the task's opening message and its per-turn table.
func renderSessionCard(w func(string, ...any), g *sessGroup, sessionTitle, taskTitle map[string]string) {
	label := g.id
	if label == "" {
		label = "（未分组）"
	}
	classNote := ""
	if g.class != "interactive" {
		classNote = " · " + g.class
	}
	w("## %s · %s · %d 任务 %d 轮%s\n\n", label, fmtUTC8Full(g.rows[0].TS), g.tasks, g.requests, classNote)

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
			tLabel = "（未分组）"
		}
		w("### %s · %s · %d 轮\n\n", tLabel, fmtUTC8Full(trows[0].TS), len(trows))
		title := taskTitle[g.id+"\x00"+tk]
		if title == "" {
			title = sessionTitle[g.id]
		}
		if title != "" {
			w("> %s\n\n", title)
		}
		w("| 轮 | 时间 | msgs | finish | dur | ttft | fresh/cached/out | cache-eff⭐ | 文件 |\n|---|---|---|---|---|---|---|---|---|\n")
		for _, r := range trows {
			w("| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				turnCell(r.Turn), fmtUTC8Time(r.TS), msgsCell(r.Msgs),
				finishCell(r), ms(r.DurMS), msOrDash(r.TTFTMS),
				tokensTriple(r), cacheEffTurn(r), detailLink(r.DetailFile))
		}
		w("\n")
	}
}

// fmtUTC8Full renders a RequestRow.TS (RFC3339, source offset) as
// "2026-07-24 00:17:58" in UTC+8. Falls back to a raw cut when the
// timestamp doesn't parse (defensive; Build always writes RFC3339).
func fmtUTC8Full(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return cut(ts, 19)
	}
	return t.In(utc8).Format("2006-01-02 15:04:05")
}

// fmtUTC8Time is fmtUTC8Full but time-only ("00:17:58"), for per-turn table
// cells where the enclosing session/task header already carries the date.
func fmtUTC8Time(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return cut(ts, 19)
	}
	return t.In(utc8).Format("15:04:05")
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
	return ms(v)
}

func finishCell(r RequestRow) string {
	if r.Outcome != "ok" && r.ErrorClass != "" {
		return "❌" + r.ErrorClass
	}
	if r.Truncated {
		return "⚠️trunc"
	}
	return orDash(r.Finish)
}

func tokensTriple(r RequestRow) string {
	if r.TokensIn == 0 && r.TokensOut == 0 {
		return "-"
	}
	return fmt.Sprintf("%s / %s / %s", fmtTokens(r.TokensInFresh), fmtTokens(r.TokensInCached), fmtTokens(r.TokensOut))
}

func cacheEffTurn(r RequestRow) string {
	if r.TokensIn == 0 {
		return "-"
	}
	return pctStr(r.CacheEff)
}

func detailLink(f string) string {
	if f == "" {
		return "-"
	}
	return fmt.Sprintf("[%s](details/%s)", strings.TrimSuffix(f, ".md"), f)
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
			ec = "error"
		}
		return "❌" + ec
	}
}
