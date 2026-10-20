package stats

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"
)

// OtherGroup labels the row that merges every group beyond the Top-N.
const OtherGroup = "(other)"

// Column describes one result column; Kind is "time", "dim" or a metric
// kind (count, tokens, ratio, ms, rate).
type Column struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// Result is the /stats/query response. Rows are positional (see Columns);
// with a step they are dense — every bucket × every kept group, empty cells
// carrying 0 counts and null ratios/percentiles — so a chart needs no
// zero-filling. Totals evaluates the metrics over the whole window,
// ungrouped and unbucketed.
type Result struct {
	Query   QueryEcho      `json:"query"`
	Columns []Column       `json:"columns"`
	Rows    [][]any        `json:"rows"`
	Totals  []any          `json:"totals"`
	Compare *CompareResult `json:"compare,omitempty"`
	Meta    Meta           `json:"meta"`
}

// CompareResult is the same query evaluated one period earlier.
type CompareResult struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Rows   [][]any `json:"rows"`
	Totals []any   `json:"totals"`
	// TokensUnknown is Meta.TokensUnknown for the comparison window.
	TokensUnknown int `json:"tokens_unknown"`
}

// Meta reports what the query touched.
type Meta struct {
	RowsScanned     int `json:"rows_scanned"`
	Partitions      int `json:"partitions"`
	GroupsTruncated int `json:"groups_truncated"`
	// TokensUnknown counts forwarded requests in the window that carry no
	// token record (never stamped, or an all-zero stamp); the token metrics
	// leave them out, so a non-zero value means those sums are lower bounds.
	TokensUnknown int `json:"tokens_unknown"`
}

// QueryEcho is the normalized query, echoed so the page can show the
// effective range and step (relative times resolved, step coarsened).
type QueryEcho struct {
	From    string       `json:"from,omitempty"`
	To      string       `json:"to,omitempty"`
	Last    int          `json:"last,omitempty"`
	Step    string       `json:"step"`
	Group   []string     `json:"group"`
	Where   []FilterEcho `json:"where"`
	Metrics []string     `json:"metrics"`
	Top     int          `json:"top"`
	Compare bool         `json:"compare"`
	// CompareBy is the comparison window ("prev", "day", "week"); empty
	// without compare.
	CompareBy string `json:"compare_by,omitempty"`
}

// FilterEcho is one echoed filter.
type FilterEcho struct {
	Dim   string   `json:"dim"`
	In    []string `json:"in,omitempty"`
	NotIn []string `json:"not_in,omitempty"`
}

func fmtTime(t time.Time) string { return t.In(time.Local).Format(time.RFC3339) }

func (q Query) echo() QueryEcho {
	e := QueryEcho{Last: q.Last, Step: q.StepName, Group: q.Group, Metrics: q.Metrics, Top: q.Top, Compare: q.Compare, CompareBy: q.CompareBy, Where: []FilterEcho{}}
	if e.Group == nil {
		e.Group = []string{}
	}
	if q.Last == 0 {
		e.From, e.To = fmtTime(q.From), fmtTime(q.To)
	}
	for _, f := range q.Where {
		fe := FilterEcho{Dim: f.Dim}
		if f.Not {
			fe.NotIn = f.Values
		} else {
			fe.In = f.Values
		}
		e.Where = append(e.Where, fe)
	}
	return e
}

// matcher evaluates the query's filters, memoizing each dimension key's
// verdict so the value string is resolved once per distinct key, not once
// per row.
type matcher struct {
	d       *dict
	filters []filterState
}

type filterState struct {
	def  *dimDef
	set  map[string]bool
	not  bool
	memo map[uint64]bool
}

func newMatcher(d *dict, where []Filter) *matcher {
	m := &matcher{d: d}
	for _, f := range where {
		set := make(map[string]bool, len(f.Values))
		for _, v := range f.Values {
			set[v] = true
		}
		m.filters = append(m.filters, filterState{def: dimIndex[f.Dim], set: set, not: f.Not, memo: map[uint64]bool{}})
	}
	return m
}

func (m *matcher) match(r *row) bool {
	for i := range m.filters {
		f := &m.filters[i]
		k := f.def.key(r)
		ok, seen := f.memo[k]
		if !seen {
			ok = f.set[f.def.value(m.d, k)] != f.not
			f.memo[k] = ok
		}
		if !ok {
			return false
		}
	}
	return true
}

// daysCovering lists the day keys whose files can hold rows with TS in
// [from, to): every day the range touches, plus the following day (a
// request is filed under the day it completed in).
func daysCovering(from, to time.Time) []string {
	from, to = from.In(time.Local), to.In(time.Local)
	d := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local)
	last := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, 1)
	var out []string
	for ; !d.After(last); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(dayLayout))
	}
	return out
}

// recentDays lists day keys from today backwards across the widest window a
// last-N or events scan may reach.
func (s *Store) recentDays() []string {
	now := s.now().In(time.Local)
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	out := make([]string, 0, maxSpanDays+1)
	for i := 0; i <= maxSpanDays; i++ {
		out = append(out, d.AddDate(0, 0, -i).Format(dayLayout))
	}
	return out
}

// scanRange calls fn for every matching row with TS in [from, to).
func (s *Store) scanRange(from, to time.Time, m *matcher, meta *Meta, fn func(*row)) {
	lo, hi := from.UnixMilli(), to.UnixMilli()
	for _, day := range daysCovering(from, to) {
		rows := s.dayRows(day)
		if len(rows) == 0 {
			continue
		}
		meta.Partitions++
		for i := range rows {
			r := &rows[i]
			if r.ts < lo || r.ts >= hi {
				continue
			}
			meta.RowsScanned++
			if m.match(r) {
				fn(r)
			}
		}
	}
}

// scanDays walks the given days newest-completed first (each day from its
// end) and stops once fn returns false.
func (s *Store) scanDays(days []string, m *matcher, meta *Meta, fn func(*row) bool) {
	for _, day := range days {
		rows := s.dayRows(day)
		if len(rows) == 0 {
			continue
		}
		meta.Partitions++
		for i := len(rows) - 1; i >= 0; i-- {
			meta.RowsScanned++
			if m.match(&rows[i]) && !fn(&rows[i]) {
				return
			}
		}
	}
}

// scanRecent is scanDays over the widest window a last-N scan may reach.
func (s *Store) scanRecent(m *matcher, meta *Meta, fn func(*row) bool) {
	s.scanDays(s.recentDays(), m, meta, fn)
}

type groupKey [maxGroupDims]uint64

// aggregation is one query's working state.
type aggregation struct {
	q       Query
	defs    []*dimDef
	mdefs   []*metricDef
	need    uint8
	bounds  []time.Time
	starts  []int64
	groups  map[groupKey]int
	keys    []groupKey
	reqs    []int64
	cells   map[[2]int]*acc // (bucket, group)
	buckets []acc           // per-bucket totals, for share_* metrics
	total   acc
}

func newAggregation(q Query) *aggregation {
	a := &aggregation{q: q, groups: map[groupKey]int{}, cells: map[[2]int]*acc{}}
	for _, g := range q.Group {
		a.defs = append(a.defs, dimIndex[g])
	}
	for _, m := range q.Metrics {
		md := metricIndex[m]
		a.mdefs = append(a.mdefs, md)
		a.need |= md.need
	}
	if q.Last > 0 {
		a.bounds = []time.Time{{}}
	} else {
		a.bounds = q.buckets()
	}
	for _, b := range a.bounds {
		a.starts = append(a.starts, b.UnixMilli())
	}
	a.buckets = make([]acc, len(a.bounds))
	return a
}

func (a *aggregation) bucketOf(ts int64) int {
	if a.q.Step == 0 {
		return 0
	}
	i := sort.Search(len(a.starts), func(i int) bool { return a.starts[i] > ts }) - 1
	return max(i, 0)
}

func (a *aggregation) add(r *row) {
	var k groupKey
	for i, d := range a.defs {
		k[i] = d.key(r)
	}
	g, ok := a.groups[k]
	if !ok {
		g = len(a.keys)
		a.groups[k] = g
		a.keys = append(a.keys, k)
		a.reqs = append(a.reqs, 0)
	}
	a.reqs[g]++
	b := a.bucketOf(r.ts)
	c := a.cells[[2]int{b, g}]
	if c == nil {
		c = &acc{}
		a.cells[[2]int{b, g}] = c
	}
	c.add(r, a.need)
	a.buckets[b].add(r, 0)
	a.total.add(r, a.need)
}

func (c *acc) merge(o *acc) {
	c.req += o.req
	c.ok += o.ok
	c.err += o.err
	c.canceled += o.canceled
	c.failover += o.failover
	c.in += o.in
	c.out += o.out
	c.cacheRead += o.cacheRead
	c.cacheWrite += o.cacheWrite
	c.qN += o.qN
	c.tokUnknown += o.tokUnknown
	c.durSum += o.durSum
	c.ttftN += o.ttftN
	c.ttftSum += o.ttftSum
	c.toksN += o.toksN
	c.toksOut += o.toksOut
	c.toksSpan += o.toksSpan
	c.durs = append(c.durs, o.durs...)
	c.ttfts = append(c.ttfts, o.ttfts...)
	c.toks = append(c.toks, o.toks...)
	c.sorted = false
}

// finish ranks groups by request count, keeps the Top-N (merging the rest
// into OtherGroup) and renders positional rows.
func (a *aggregation) finish(d *dict) (rows [][]any, totals []any, truncated int) {
	order := make([]int, len(a.keys))
	for i := range order {
		order[i] = i
	}
	values := make([][]string, len(a.keys))
	for g, k := range a.keys {
		values[g] = make([]string, len(a.defs))
		for i, def := range a.defs {
			values[g][i] = def.value(d, k[i])
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		gi, gj := order[i], order[j]
		if a.reqs[gi] != a.reqs[gj] {
			return a.reqs[gi] > a.reqs[gj]
		}
		return slices.Compare(values[gi], values[gj]) < 0
	})
	kept := order
	other := -1
	if len(a.defs) > 0 && len(order) > a.q.Top {
		kept = order[:a.q.Top]
		truncated = len(order) - a.q.Top
		other = len(a.keys)
		for _, g := range order[a.q.Top:] {
			for b := range a.bounds {
				if c := a.cells[[2]int{b, g}]; c != nil {
					oc := a.cells[[2]int{b, other}]
					if oc == nil {
						oc = &acc{}
						a.cells[[2]int{b, other}] = oc
					}
					oc.merge(c)
				}
			}
		}
		otherVals := make([]string, len(a.defs))
		for i := range otherVals {
			otherVals[i] = OtherGroup
		}
		values = append(values, otherVals)
		kept = append(slices.Clone(kept), other)
	}
	if len(a.keys) == 0 && len(a.defs) == 0 {
		kept = []int{0} // an ungrouped query always yields its (empty) row
		values = [][]string{{}}
	}
	empty := &acc{}
	for b, start := range a.bounds {
		for _, g := range kept {
			c := a.cells[[2]int{b, g}]
			if c == nil {
				if a.q.Step == 0 && len(a.defs) > 0 {
					continue
				}
				c = empty
			}
			row := make([]any, 0, 1+len(a.defs)+len(a.mdefs))
			if a.q.Step != 0 {
				row = append(row, fmtTime(start))
			}
			for _, v := range values[g] {
				row = append(row, v)
			}
			for _, md := range a.mdefs {
				row = append(row, md.eval(c, &a.buckets[b]))
			}
			rows = append(rows, row)
		}
	}
	if rows == nil {
		rows = [][]any{}
	}
	if a.q.Step != 0 {
		totals = append(totals, nil)
	}
	for range a.defs {
		totals = append(totals, nil)
	}
	for _, md := range a.mdefs {
		totals = append(totals, md.eval(&a.total, &a.total))
	}
	return rows, totals, truncated
}

func (a *aggregation) columns() []Column {
	var cols []Column
	if a.q.Step != 0 {
		cols = append(cols, Column{Name: "bucket", Kind: "time"})
	}
	for _, g := range a.q.Group {
		cols = append(cols, Column{Name: g, Kind: "dim"})
	}
	for _, md := range a.mdefs {
		cols = append(cols, Column{Name: md.Name, Kind: md.Kind})
	}
	return cols
}

// Query evaluates q over the event log.
func (s *Store) Query(q Query) Result {
	res := Result{Query: q.echo()}
	agg := s.aggregate(q, &res.Meta)
	res.Columns = agg.columns()
	res.Rows, res.Totals, res.Meta.GroupsTruncated = agg.finish(s.dict)
	res.Meta.TokensUnknown = agg.total.tokUnknown
	if q.Compare && q.Last == 0 {
		pq := previousPeriod(q)
		var meta Meta
		pa := s.aggregate(pq, &meta)
		rows, totals, _ := pa.finish(s.dict)
		res.Compare = &CompareResult{From: fmtTime(pq.From), To: fmtTime(pq.To), Rows: rows, Totals: totals, TokensUnknown: pa.total.tokUnknown}
	}
	return res
}

// previousPeriod is the comparison query: q shifted back in time.
//
// compare=prev shifts by the range's own length. Unbucketed that is the span
// right before From. Bucketed, From is already aligned down, so the span
// carries a fraction of a bucket; both ends shift by the whole buckets the
// span holds (-24h at 15m → exactly 24h). Bucket i then lines up with the
// same wall-clock bucket one period earlier, and both periods have the same
// bucket count — re-aligning a shifted From instead starts one bucket early.
//
// compare=day / week shift by whole calendar days, so "today so far" meets
// yesterday's same hours instead of the stretch before today's midnight.
// Every part of the dashboard asking the same range gets the same window,
// bucketed or not.
func previousPeriod(q Query) Query {
	pq := q
	if q.Floor.IsZero() {
		q.Floor = q.From
	}
	var shift func(time.Time) time.Time
	switch q.CompareBy {
	case compareDay:
		shift = func(t time.Time) time.Time { return t.AddDate(0, 0, -1) }
	case compareWeek:
		shift = func(t time.Time) time.Time { return t.AddDate(0, 0, -7) }
	default:
		span := q.To.Sub(q.From)
		if q.Step != 0 {
			span = time.Duration(max(1, int(span/q.Step))) * q.Step
		}
		if q.Step >= 24*time.Hour {
			k := int(span / q.Step)
			shift = func(t time.Time) time.Time { return t.AddDate(0, 0, -k) }
		} else {
			shift = func(t time.Time) time.Time { return t.Add(-span) }
		}
	}
	pq.From, pq.To, pq.Floor = shift(q.From), shift(q.To), shift(q.Floor)
	return pq
}

func (s *Store) aggregate(q Query, meta *Meta) *aggregation {
	agg := newAggregation(q)
	m := newMatcher(s.dict, q.Where)
	if q.Last > 0 {
		n := 0
		s.scanRecent(m, meta, func(r *row) bool {
			agg.add(r)
			n++
			return n < q.Last
		})
		return agg
	}
	floor := q.Floor
	if floor.IsZero() {
		floor = q.From
	}
	s.scanRange(floor, q.To, m, meta, agg.add)
	return agg
}

// EventsQuery selects raw events, newest first.
type EventsQuery struct {
	From, To time.Time // zero = unbounded (within the recent-days window)
	Where    []Filter
	Limit    int
}

// Events returns up to q.Limit matching events, most recently completed
// first, sorted by arrival time descending. With From set only the days that
// can hold rows in [From, To) are read, so an old range is as reachable as a
// recent one and a short range never touches the rest of history; without
// From it scans back from today.
func (s *Store) Events(q EventsQuery) []Event {
	m := newMatcher(s.dict, q.Where)
	lo, hi := int64(0), int64(1<<62)
	days := s.recentDays()
	if !q.From.IsZero() {
		lo = q.From.UnixMilli()
		to := q.To
		if to.IsZero() {
			to = s.now()
		}
		days = daysCovering(q.From, to)
		slices.Reverse(days)
	}
	if !q.To.IsZero() {
		hi = q.To.UnixMilli()
	}
	var meta Meta
	var out []Event
	s.scanDays(days, m, &meta, func(r *row) bool {
		if r.ts >= lo && r.ts < hi {
			out = append(out, s.dict.toEvent(r))
		}
		return len(out) < q.Limit
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.After(out[j].TS) })
	if out == nil {
		out = []Event{}
	}
	return out
}

// ParseEventsQuery reads /stats/events parameters.
func ParseEventsQuery(get func(string) string, where []string, now time.Time) (EventsQuery, error) {
	q := EventsQuery{Limit: 100}
	var err error
	if q.Where, err = parseWhere(where); err != nil {
		return q, err
	}
	if s := get("from"); s != "" {
		if q.From, err = parseTime(s, now); err != nil {
			return q, err
		}
	}
	if s := get("to"); s != "" {
		if q.To, err = parseTime(s, now); err != nil {
			return q, err
		}
	}
	if s := get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxEvents {
			return q, fmt.Errorf("limit must be 1..%d", maxEvents)
		}
		q.Limit = n
	}
	return q, nil
}
