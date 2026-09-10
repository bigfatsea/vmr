package livestats

import "time"

// slimNameLayout is the slim WAL file name: vmr-stats-YYYYMMDD-HH.jsonl in
// the hour's own zone (design §3.2). The layout doubles as the parser.
const slimNameLayout = "vmr-stats-20060102-15.jsonl"

// hourStartOf buckets t into its clock hour, keeping t's own location —
// arrival times are server-local, so the bucket boundary is the local hour.
func hourStartOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

// dayStartOf buckets t into its local-calendar day (midnight, time.Local) —
// the retention window counts whole past days plus the current partial one,
// so its boundary is a calendar day, not a rolling 24h multiple. Same zone
// authority as slim-file naming and daily[] folding.
func dayStartOf(t time.Time) time.Time {
	lt := t.In(time.Local)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.Local)
}

// hourFileName maps an hour start to its slim file name.
func hourFileName(t time.Time) string { return t.Format(slimNameLayout) }

// parseHourFileName re-anchors the name's wall-clock components to local
// time: the zone isn't in the name, and local is where it was written.
func parseHourFileName(name string) (time.Time, bool) {
	t, err := time.Parse(slimNameLayout, name)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.Local), true
}
