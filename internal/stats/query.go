package stats

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Query limits. They are engineering constants, not deployment knobs: the
// span cap bounds the cost of one query (history itself is unbounded —
// day partitions make "a week a year ago" cost the same as "last week"),
// the bucket caps protect the browser and keep charts legible.
const (
	maxSpanDays  = 90
	maxBuckets   = 500
	autoBuckets  = 200
	maxGroupDims = 3
	defaultTop   = 20
	maxTop       = 1000
	maxLast      = 1000
	maxEvents    = 1000
)

const maxSpan = maxSpanDays * 24 * time.Hour

// step ladder, finest first. Explicit steps that would produce more than
// maxBuckets coarsen to the finest rung that fits; "auto" picks the finest
// rung with at most autoBuckets.
var ladder = []struct {
	name string
	d    time.Duration
}{
	{"1m", time.Minute}, {"5m", 5 * time.Minute}, {"15m", 15 * time.Minute},
	{"1h", time.Hour}, {"6h", 6 * time.Hour}, {"1d", 24 * time.Hour},
}

// Filter keeps rows whose dimension value is (or, with Not, is not) one of
// Values.
type Filter struct {
	Dim    string
	Values []string
	Not    bool
}

// Query is one normalized statistics query: either a time range [From, To)
// or the Last N matching events, optionally bucketed by Step, grouped by up
// to maxGroupDims dimensions, evaluating Metrics.
type Query struct {
	From, To time.Time
	// Floor is the lower bound rows are filtered by. From is the first
	// bucket's origin and is aligned down to the step, so it can sit up to
	// one step before Floor; filtering by Floor keeps a bucketed query and
	// an unbucketed one over the same range summing to the same number.
	Floor    time.Time
	Last     int
	Step     time.Duration
	StepName string
	Group    []string
	Where    []Filter
	Metrics  []string
	Top      int
	Compare  bool
	// CompareBy picks the comparison window: compareSpan shifts back by the
	// range's own length, compareDay / compareWeek by whole calendar days
	// (the same wall-clock range yesterday / a week ago).
	CompareBy string
}

// Comparison windows accepted as compare=<value>.
const (
	compareSpan = "prev"
	compareDay  = "day"
	compareWeek = "week"
)

// ParseQuery reads a /stats/query parameter set (see the statistics design
// doc for the grammar) and normalizes it against now.
func ParseQuery(v url.Values, now time.Time) (Query, error) {
	q := Query{Top: defaultTop}
	var err error
	if q.Metrics, err = parseMetrics(v.Get("metrics")); err != nil {
		return q, err
	}
	if q.Group, err = parseGroup(v.Get("group")); err != nil {
		return q, err
	}
	if q.Where, err = parseWhere(v["where"]); err != nil {
		return q, err
	}
	if s := v.Get("top"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxTop {
			return q, fmt.Errorf("top must be 1..%d", maxTop)
		}
		q.Top = n
	}
	switch c := v.Get("compare"); c {
	case "":
	case compareSpan, compareDay, compareWeek:
		q.Compare, q.CompareBy = true, c
	default:
		return q, errors.New(`compare must be "prev", "day" or "week"`)
	}
	if s := v.Get("last"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxLast {
			return q, fmt.Errorf("last must be 1..%d", maxLast)
		}
		if v.Get("from") != "" || v.Get("to") != "" {
			return q, errors.New("last cannot be combined with from/to")
		}
		if st := v.Get("step"); st != "" && st != "none" {
			return q, errors.New("last cannot be combined with step")
		}
		if q.Compare {
			return q, errors.New("last cannot be combined with compare")
		}
		q.Last, q.StepName = n, "none"
		return q, nil
	}
	if q.From, q.To, err = parseRange(v.Get("from"), v.Get("to"), now); err != nil {
		return q, err
	}
	q.Floor = q.From
	if err := q.setStep(v.Get("step")); err != nil {
		return q, err
	}
	return q, nil
}

func parseMetrics(s string) ([]string, error) {
	if s == "" {
		return []string{"req"}, nil
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range strings.Split(s, ",") {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		if metricIndex[m] == nil {
			return nil, fmt.Errorf("unknown metric %q", m)
		}
		seen[m] = true
		out = append(out, m)
	}
	return out, nil
}

func parseGroup(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var out []string
	for _, d := range strings.Split(s, ",") {
		d = strings.TrimSpace(d)
		if dimIndex[d] == nil {
			return nil, fmt.Errorf("unknown dimension %q", d)
		}
		for _, x := range out {
			if x == d {
				return nil, fmt.Errorf("dimension %q grouped twice", d)
			}
		}
		out = append(out, d)
	}
	if len(out) > maxGroupDims {
		return nil, fmt.Errorf("at most %d group dimensions", maxGroupDims)
	}
	return out, nil
}

// parseWhere reads repeated `where` params: dim:v1|v2 keeps rows whose value
// is in the set, dim!v1|v2 drops them. Dimension names are [a-z_], so the
// first other character is the operator; values may contain anything else
// (a direct-routing vmodel is itself "provider:model"). A literal | or \
// inside a value is written \| or \\.
func parseWhere(ws []string) ([]Filter, error) {
	var out []Filter
	for _, w := range ws {
		i := strings.IndexFunc(w, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r == '_') })
		if i <= 0 || (w[i] != ':' && w[i] != '!') {
			return nil, fmt.Errorf("bad where %q: want dim:v1|v2 or dim!v1|v2", w)
		}
		dim := w[:i]
		if dimIndex[dim] == nil {
			return nil, fmt.Errorf("unknown dimension %q", dim)
		}
		out = append(out, Filter{Dim: dim, Values: splitValues(w[i+1:]), Not: w[i] == '!'})
	}
	return out, nil
}

// splitValues splits a where value list on unescaped |, undoing the \|
// and \\ escapes.
func splitValues(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\' && i+1 < len(s) && (s[i+1] == '|' || s[i+1] == '\\'):
			i++
			cur.WriteByte(s[i])
		case c == '|':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(out, cur.String())
}

func parseRange(from, to string, now time.Time) (time.Time, time.Time, error) {
	if from == "" {
		from = "-7d"
	}
	if to == "" {
		to = "now"
	}
	f, err := parseTime(from, now)
	if err != nil {
		return f, f, fmt.Errorf("from: %w", err)
	}
	t, err := parseTime(to, now)
	if err != nil {
		return f, t, fmt.Errorf("to: %w", err)
	}
	if !f.Before(t) {
		return f, t, errors.New("from must be before to")
	}
	if t.Sub(f) > maxSpan {
		return f, t, fmt.Errorf("a single query spans at most %d days", maxSpanDays)
	}
	return f, t, nil
}

// parseTime accepts now, today (local midnight), -N{m,h,d} relative to now,
// YYYY-MM-DD (local midnight) and RFC3339.
func parseTime(s string, now time.Time) (time.Time, error) {
	now = now.In(time.Local)
	switch {
	case s == "now":
		return now, nil
	case s == "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local), nil
	case strings.HasPrefix(s, "-") && len(s) >= 3:
		n, err := strconv.Atoi(s[1 : len(s)-1])
		if err != nil || n < 0 {
			break
		}
		switch s[len(s)-1] {
		case 'm':
			return now.Add(-time.Duration(n) * time.Minute), nil
		case 'h':
			return now.Add(-time.Duration(n) * time.Hour), nil
		case 'd':
			return now.AddDate(0, 0, -n), nil
		}
	}
	if t, err := time.ParseInLocation(dayLayout, s, time.Local); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(time.Local), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

// setStep resolves the step (coarsening past maxBuckets, auto-picking for
// "auto") and aligns From down to the step boundary on the local wall
// clock, so -6d with 1d is exactly the last seven calendar days.
func (q *Query) setStep(s string) error {
	span := q.To.Sub(q.From)
	idx := -1
	switch s {
	case "", "none":
		q.StepName = "none"
		return nil
	case "auto":
		idx = len(ladder) - 1
		for i, r := range ladder {
			if span/r.d < autoBuckets {
				idx = i
				break
			}
		}
	default:
		for i, r := range ladder {
			if r.name == s {
				idx = i
			}
		}
		if idx < 0 {
			return fmt.Errorf("unknown step %q", s)
		}
		for idx < len(ladder)-1 && span/ladder[idx].d >= maxBuckets {
			idx++
		}
	}
	q.Step, q.StepName = ladder[idx].d, ladder[idx].name
	q.Floor = q.From
	q.From = alignDown(q.From, q.Step)
	return nil
}

func alignDown(t time.Time, step time.Duration) time.Time {
	t = t.In(time.Local)
	mid := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	if step >= 24*time.Hour {
		return mid
	}
	return mid.Add(t.Sub(mid) / step * step)
}

// buckets returns the bucket start times covering [From, To).
func (q *Query) buckets() []time.Time {
	if q.Step == 0 {
		return []time.Time{q.From}
	}
	var out []time.Time
	for t := q.From; t.Before(q.To); t = nextBucket(t, q.Step) {
		out = append(out, t)
	}
	return out
}

func nextBucket(t time.Time, step time.Duration) time.Time {
	if step >= 24*time.Hour {
		return t.AddDate(0, 0, 1)
	}
	return t.Add(step)
}
