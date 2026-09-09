package livestats

import (
	"slices"
	"strings"
	"time"
)

// Snapshot is the JSON-ready read-time aggregation behind GET /stats
// (design §8). Everything is computed at read time from the in-memory
// ledger; nothing here mutates aggregator state.
type Snapshot struct {
	Hourly          []HourlyRow    `json:"hourly"`
	Daily           []HourlyRow    `json:"daily"`
	ByProviderModel []ProviderRow  `json:"by_provider_model"`
	ByClientKeyTag  []DimensionRow `json:"by_client_key_tag"`
	ByKeyLabel      []DimensionRow `json:"by_key_label"`
}

// HourlyRow is one (hour or day × dims) group in the hourly/daily slices.
// Daily rows fold the same counters and carry the day prefix in Hour.
type HourlyRow struct {
	Hour     time.Time `json:"hour"`
	Dims     Dims      `json:"dims"`
	Counters Counters  `json:"counters"`
}

// ProviderRow is the per-(provider, model, stream) cumulative + mean
// profile with the ring's recent TTFT/TPS percentiles. Stream is part of
// the identity so the two TPS denominators never share a percentile pool.
type ProviderRow struct {
	Provider   string      `json:"provider"`
	Model      string      `json:"model"`
	Stream     bool        `json:"stream"`
	OK         int64       `json:"ok"`
	Error      int64       `json:"error"`
	Canceled   int64       `json:"canceled"`
	Tokens     TokenCounts `json:"tokens"`
	DurMS      SumCount    `json:"dur_ms"`
	TTFTMS     SumCount    `json:"ttft_ms"`
	DurMSMean  float64     `json:"dur_ms_mean"`
	TTFTMSMean float64     `json:"ttft_ms_mean"`
	Last10     *Quantiles  `json:"last_10,omitempty"`
	Last100    *Quantiles  `json:"last_100,omitempty"`
}

// DimensionRow is one group of a usage profile sliced along a single axis.
type DimensionRow struct {
	Value    string      `json:"value"`
	OK       int64       `json:"ok"`
	Error    int64       `json:"error"`
	Canceled int64       `json:"canceled"`
	Tokens   TokenCounts `json:"tokens"`
	Count    int64       `json:"count"`
}

// hourRow is the aggregation workhorse: one dims group's counters, plus the
// per-hour counters each observed hour contributed (rollup groups may span
// several hours with identical dims).
type hourRow struct {
	dims Dims
	byHr map[int64]Counters
}

func (r *hourRow) add(h time.Time, c Counters) {
	if r.byHr == nil {
		r.byHr = make(map[int64]Counters)
	}
	r.byHr[h.Unix()] = c
}

// snapshotLocked aggregates the whole ledger for /stats. Caller holds the
// mutex.
func (a *Aggregator) snapshotLocked() Snapshot {
	// request-face dimension profiles accumulate across every group; the
	// provider profile covers forwarded samples only (§4.2).
	type axis map[string]*Counters
	byTag, byLabel := axis{}, axis{}
	prov := map[ringKey]*Counters{}

	hourRows := map[string]*hourRow{}
	fold := func(m map[dimsKey]Counters, hk time.Time) {
		for k, c := range m {
			id := k.id()
			r := hourRows[id]
			if r == nil {
				r = &hourRow{dims: k.dims()}
				hourRows[id] = r
			}
			r.add(hk, c)
			bookAxis(byTag, k.clientKeyTag, c)
			bookAxis(byLabel, k.keyLabel, c)
			if k.provider != "" {
				kk := ringKey{k.provider, k.model, k.stream}
				p := prov[kk]
				if p == nil {
					p = &Counters{}
					prov[kk] = p
				}
				p.add(c)
			}
		}
	}

	for hk, m := range a.rollup {
		fold(m, hk)
	}
	fold(a.cur, a.hour)

	return assembleSnapshot(hourRows, byTag, byLabel, prov, a.rings)
}

// bookAxis adds a group's request-face outcome counts and tokens to a
// dimension profile; duration/ttft facts stay out of single-axis summaries.
func bookAxis(m map[string]*Counters, v string, c Counters) {
	if v == "" {
		return
	}
	a := m[v]
	if a == nil {
		a = &Counters{}
		m[v] = a
	}
	a.OK += c.OK
	a.Error += c.Error
	a.Canceled += c.Canceled
	a.Tokens.add(c.Tokens)
}

// assembleSnapshot folds the hour rows into hourly/daily slices, builds the
// provider rows with ring percentiles, and orders everything.
func assembleSnapshot(hourRows map[string]*hourRow, byTag, byLabel map[string]*Counters, prov map[ringKey]*Counters, rings map[ringKey]*ring) Snapshot {
	// hourly[] keeps the most recent hourlyTail hours with data.
	hours := make([]int64, 0, 2*len(hourRows))
	for _, r := range hourRows {
		for u := range r.byHr {
			hours = append(hours, u)
		}
	}
	slices.Sort(hours)
	hours = slices.Compact(hours)
	if len(hours) > hourlyTail {
		hours = hours[len(hours)-hourlyTail:]
	}
	hourSet := make(map[int64]bool, len(hours))
	for _, u := range hours {
		hourSet[u] = true
	}

	ids := make([]string, 0, len(hourRows))
	for id := range hourRows {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	daily := map[string]*HourlyRow{}
	snap := Snapshot{Hourly: make([]HourlyRow, 0, len(hourRows))}
	for _, id := range ids {
		r := hourRows[id]
		for u, c := range r.byHr {
			if hourSet[u] {
				snap.Hourly = append(snap.Hourly, HourlyRow{Hour: time.Unix(u, 0), Dims: r.dims, Counters: c})
			}
			day := foldDay(u)
			dk := formatDay(day) + "\x1f" + id
			d := daily[dk]
			if d == nil {
				d = &HourlyRow{Hour: day, Dims: r.dims}
				daily[dk] = d
			}
			d.Counters.add(c)
		}
	}
	snap.Daily = sortedDaily(daily)
	snap.ByProviderModel = buildProviderRows(prov, rings)
	snap.ByClientKeyTag = buildAxisRows(byTag)
	snap.ByKeyLabel = buildAxisRows(byLabel)
	return snap
}

// foldDay returns the UTC day bucket a unix hour belongs to. Day buckets
// are display groupings over hour-aligned data, so UTC is the stable choice.
func foldDay(unix int64) time.Time {
	return time.Unix(unix-unix%86400, 0).UTC()
}

func formatDay(t time.Time) string {
	return t.Format("2006-01-02")
}

// sortedDaily orders the folded day groups by (day, dims id).
func sortedDaily(m map[string]*HourlyRow) []HourlyRow {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	rows := make([]HourlyRow, 0, len(m))
	for _, k := range keys {
		rows = append(rows, *m[k])
	}
	return rows
}

// buildProviderRows merges cumulative counters with each ring's percentile
// window. A ring whose key has no counters left (rolled away, or restarted
// into an empty current hour) still shows up, so the recent-performance
// view never goes blind.
func buildProviderRows(prov map[ringKey]*Counters, rings map[ringKey]*ring) []ProviderRow {
	keys := make([]ringKey, 0, len(prov))
	for k := range prov {
		keys = append(keys, k)
	}
	for k := range rings {
		if _, ok := prov[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(x, y ringKey) int {
		if c := strings.Compare(x.provider, y.provider); c != 0 {
			return c
		}
		if c := strings.Compare(x.model, y.model); c != 0 {
			return c
		}
		return boolInt(x.stream) - boolInt(y.stream)
	})
	rows := make([]ProviderRow, 0, len(keys))
	for _, k := range keys {
		row := ProviderRow{Provider: k.provider, Model: k.model, Stream: k.stream}
		if c := prov[k]; c != nil {
			row.OK, row.Error, row.Canceled = c.OK, c.Error, c.Canceled
			row.Tokens, row.DurMS, row.TTFTMS = c.Tokens, c.DurMS, c.TTFTMS
			if c.DurMS.N > 0 {
				row.DurMSMean = float64(c.DurMS.Sum) / float64(c.DurMS.N)
			}
			if c.TTFTMS.N > 0 {
				row.TTFTMSMean = float64(c.TTFTMS.Sum) / float64(c.TTFTMS.N)
			}
		}
		if r := rings[k]; r != nil && r.n > 0 {
			row.Last10 = quantilesFor(r.last(10), k.stream)
			row.Last100 = quantilesFor(r.last(ringCap), k.stream)
		}
		rows = append(rows, row)
	}
	return rows
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// buildAxisRows turns one dimension profile into sorted rows. Count is the
// request-face total across outcomes.
func buildAxisRows(m map[string]*Counters) []DimensionRow {
	vals := make([]string, 0, len(m))
	for v := range m {
		vals = append(vals, v)
	}
	slices.Sort(vals)
	rows := make([]DimensionRow, 0, len(vals))
	for _, v := range vals {
		c := m[v]
		rows = append(rows, DimensionRow{
			Value: v, OK: c.OK, Error: c.Error, Canceled: c.Canceled,
			Tokens: c.Tokens, Count: c.OK + c.Error + c.Canceled,
		})
	}
	return rows
}
