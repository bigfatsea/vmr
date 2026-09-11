package livestats

import (
	"slices"
	"strings"
	"time"
)

// Snapshot is the JSON-ready read-time aggregation behind GET /stats
// (design §8). Everything is computed at read time from the in-memory
// ledger; nothing here mutates aggregator state. Hourly is parameterized
// by the caller's tail (HourlyTailDefault, or 24/72/168 via ?range=);
// daily[] is always the last dailyTail local-calendar days with data.
type Snapshot struct {
	Hourly          []HourlyRow      `json:"hourly"`
	Daily           []HourlyRow      `json:"daily"`
	Overall         *WindowBlock     `json:"overall,omitempty"`
	RecentErrors    []RecentErrorRow `json:"recent_errors"`
	ByProviderModel []ProviderRow    `json:"by_provider_model"`
	ByClientKeyTag  []DimensionRow   `json:"by_client_key_tag"`
	ByKeyLabel      []DimensionRow   `json:"by_key_label"`
}

// HourlyRow is one (hour or day × dims) group in the hourly/daily slices.
// Daily rows fold the same counters and carry the day prefix in Hour.
type HourlyRow struct {
	Hour     time.Time `json:"hour"`
	Dims     Dims      `json:"dims"`
	Counters Counters  `json:"counters"`
}

// RecentErrorRow is one recent_errors[] entry (contracts §1.4): the
// failure/canceled detail the hourly counters flatten away. Purely
// in-memory, never persisted, no bodies. ErrorClass quotes the routing
// half's stamp verbatim — the stats side never re-classifies. Status 0
// (no HTTP exchange) is omitted.
type RecentErrorRow struct {
	TS           time.Time `json:"ts"`
	VModel       string    `json:"vmodel"`
	Protocol     string    `json:"protocol"`
	Stream       bool      `json:"stream"`
	ClientKeyTag string    `json:"client_key_tag"`
	Provider     string    `json:"provider"`
	KeyLabel     string    `json:"key_label"`
	Model        string    `json:"model"`
	Attempt      int       `json:"attempt"`
	Outcome      string    `json:"outcome"`
	ErrorClass   string    `json:"error_class"`
	Status       int       `json:"status,omitempty"`
	DurMS        int64     `json:"dur_ms"`
}

// ProviderRow is the per-(provider, key_label, model) cumulative +
// mean profile with the ring's recent window blocks. Stream and
// non-stream samples merge into this single profile and share the ring.
type ProviderRow struct {
	Provider   string       `json:"provider"`
	KeyLabel   string       `json:"key_label"`
	Model      string       `json:"model"`
	OK         int64        `json:"ok"`
	Error      int64        `json:"error"`
	Canceled   int64        `json:"canceled"`
	Tokens     TokenCounts  `json:"tokens"`
	DurMS      SumCount     `json:"dur_ms"`
	TTFTMS     SumCount     `json:"ttft_ms"`
	DurMSMean  float64      `json:"dur_ms_mean"`
	TTFTMSMean float64      `json:"ttft_ms_mean"`
	Last10     *WindowBlock `json:"last_10,omitempty"`
	Last100    *WindowBlock `json:"last_100,omitempty"`
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
	// Accumulate, never assign: the same (dims, hour) pair can arrive from
	// two independent folds (the rollup map and the live current-hour map
	// both feed snapshotLocked), and the rollup iteration order is map-
	// random — an assign here would make the winner of that race decide the
	// number, which is exactly the h9/h10 swap TestAggregator_RestartRecovery
	// caught. The two sources are disjoint by construction (rolling moves a
	// group out of cur and into rollup once), so summing is the correct
	// merge, not double-counting.
	acc := r.byHr[h.Unix()]
	acc.add(c)
	r.byHr[h.Unix()] = acc
}

// snapshotLocked aggregates the whole ledger for /stats. hourlyTail is the
// caller-chosen hourly window (contracts §1.5); dailyTail is fixed. Caller
// holds the mutex.
func (a *Aggregator) snapshotLocked(hourlyTail int) Snapshot {
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
				kk := ringKey{k.provider, k.keyLabel, k.model}
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

	return assembleSnapshot(hourRows, byTag, byLabel, prov, a.rings, a.recentErrs, hourlyTail, a.now())
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
// provider rows with their recent window blocks, the overall block over the
// union of all ring samples (contracts §1.3), and the recent_errors rows.
func assembleSnapshot(hourRows map[string]*hourRow, byTag, byLabel map[string]*Counters, prov map[ringKey]*Counters, rings map[ringKey]*ring, recentErrs []Sample, hourlyTail int, now time.Time) Snapshot {
	// hourly[] keeps the most recent hourlyTail hours with data; daily[] the
	// most recent dailyTail local-calendar-days. Both windows are derived from
	// the full observed-hour set before either is truncated.
	hours := make([]int64, 0, 2*len(hourRows))
	for _, r := range hourRows {
		for u := range r.byHr {
			hours = append(hours, u)
		}
	}
	slices.Sort(hours)
	hours = slices.Compact(hours)

	dayKeys := make([]string, 0, len(hours))
	seenDay := make(map[string]bool, len(hours))
	for _, u := range hours {
		dk := formatDay(foldDay(u))
		if !seenDay[dk] {
			seenDay[dk] = true
			dayKeys = append(dayKeys, dk)
		}
	}
	slices.Sort(dayKeys)
	if len(dayKeys) > dailyTail {
		dayKeys = dayKeys[len(dayKeys)-dailyTail:]
	}
	daySet := make(map[string]bool, len(dayKeys))
	for _, dk := range dayKeys {
		daySet[dk] = true
	}

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
			dayStr := formatDay(day)
			if !daySet[dayStr] {
				continue
			}
			dk := dayStr + "\x1f" + id
			d := daily[dk]
			if d == nil {
				d = &HourlyRow{Hour: day, Dims: r.dims}
				daily[dk] = d
			}
			d.Counters.add(c)
		}
	}
	snap.Daily = sortedDaily(daily)
	snap.Overall = overallBlock(rings)
	snap.RecentErrors = recentErrorRows(recentErrs, now.Add(-24*time.Hour))
	snap.ByProviderModel = buildProviderRows(prov, rings)
	snap.ByClientKeyTag = buildAxisRows(byTag)
	snap.ByKeyLabel = buildAxisRows(byLabel)
	// Sort hourly rows by time ascending (older first, newest at the end) so
	// consumers (and tests) see a deterministic time-ordered progression,
	// not map-iteration randomness over byHr. Ties break on Dims id.
	slices.SortFunc(snap.Hourly, func(a, b HourlyRow) int {
		if !a.Hour.Equal(b.Hour) {
			if a.Hour.Before(b.Hour) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Dims.key().id(), b.Dims.key().id())
	})
	return snap
}

// foldDay returns the local-calendar-day bucket a unix hour belongs to.
// "By day" is a human-facing grouping, so it follows the operator's wall
// clock (time.Local) — the same authority hour.go uses for slim-file naming,
// and what the project's one-display-zone rule requires (livestats is a leaf
// and cannot import fmtutil.DisplayZone, whose value is time.Local anyway).
// A fixed-offset %86400 truncation would put a UTC+8 operator's 00:00-07:59
// requests on the previous day.
func foldDay(unix int64) time.Time {
	t := time.Unix(unix, 0).In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
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

// overallBlock merges the union of every ring's samples into one window
// block (contracts §1.3): percentiles cannot be merged per key, so the
// server computes the block over the raw union at read time.
func overallBlock(rings map[ringKey]*ring) *WindowBlock {
	var all []ringEntry
	for _, r := range rings {
		all = append(all, r.last(ringCap)...)
	}
	if len(all) == 0 {
		return nil
	}
	return windowBlock(all)
}

// recentErrorRows maps the in-memory ring (append order, oldest last) to
// its wire shape, newest first (contracts §1.4), filtering out any samples
// older than cutoff.
func recentErrorRows(recentErrs []Sample, cutoff time.Time) []RecentErrorRow {
	rows := make([]RecentErrorRow, 0, len(recentErrs))
	for i := len(recentErrs) - 1; i >= 0; i-- {
		s := recentErrs[i]
		if !cutoff.IsZero() && s.TS.Before(cutoff) {
			continue
		}
		rows = append(rows, RecentErrorRow{
			TS: s.TS, VModel: s.VModel, Protocol: s.Protocol, Stream: s.Stream,
			ClientKeyTag: s.ClientKeyTag, Provider: s.Provider, KeyLabel: s.KeyLabel,
			Model: s.Model, Attempt: s.Attempt, Outcome: s.Outcome,
			ErrorClass: s.ErrorClass, Status: s.Status, DurMS: s.DurMS,
		})
	}
	return rows
}

// buildProviderRows merges cumulative counters with each ring's recent
// window blocks. A ring whose key has no counters left (rolled away, or
// restarted into an empty current hour) still shows up, so the
// recent-performance view never goes blind.
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
		if c := strings.Compare(x.keyLabel, y.keyLabel); c != 0 {
			return c
		}
		return strings.Compare(x.model, y.model)
	})
	rows := make([]ProviderRow, 0, len(keys))
	for _, k := range keys {
		row := ProviderRow{Provider: k.provider, KeyLabel: k.keyLabel, Model: k.model}
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
			row.Last10 = windowBlock(r.last(10))
			row.Last100 = windowBlock(r.last(ringCap))
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
