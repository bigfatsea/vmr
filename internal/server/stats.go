// Ver 2026-09-09, by pi

// The live stats HTTP and collection surface: /stats (JSON API, auth-gated).
//
// Ownership split (LiveStats design §5.1): livestats.Aggregator owns the
// completed-request ledger (slim WAL, hourly rollups, ring percentiles);
// router.InflightRegistry owns the in-flight per-request live entries.
// GET /stats merges both into one JSON payload at read time.
package server

import (
	"encoding/json"
	"net/http"

	"vmr/internal/audit"
	"vmr/internal/livestats"
	"vmr/internal/router"
)

// WithLiveStats wires the completed-request aggregator into the server.
// nil-safe: absent stats leaves /stats serving only in-flight + concurrency.
func (s *Server) WithLiveStats(l *livestats.Aggregator) *Server {
	s.liveStats = l
	return s
}

// statsResponse is the JSON wire contract for GET /stats (§8).
type statsResponse struct {
	Concurrency struct {
		Limit    int   `json:"limit"`
		InFlight int64 `json:"in_flight"`
		Waiting  int64 `json:"waiting"`
	} `json:"concurrency"`
	Inflight        []router.InflightEntry     `json:"inflight"`
	Hourly          []livestats.HourlyRow      `json:"hourly"`
	Daily           []livestats.HourlyRow      `json:"daily"`
	Overall         *livestats.WindowBlock     `json:"overall,omitempty"`
	RecentErrors    []livestats.RecentErrorRow `json:"recent_errors"`
	ByProviderModel []livestats.ProviderRow    `json:"by_provider_model"`
	ByClientKeyTag  []livestats.DimensionRow   `json:"by_client_key_tag"`
	ByKeyLabel      []livestats.DimensionRow   `json:"by_key_label"`
}

// parseRangeTail resolves ?range= to the hourly tail it selects
// (contracts §1.5): 24h|3d|7d → 24/72/168 hours; absent or unrecognized
// values fall back to the 48h default. The 7d cap is deliberate — the
// rollup file is never auto-deleted, so an unbounded range would turn one
// /stats poll into a full-history rebuild.
func parseRangeTail(q string) int {
	switch q {
	case "24h":
		return 24
	case "3d":
		return 72
	case "7d":
		return 168
	default:
		return livestats.HourlyTailDefault
	}
}

// adminStats serves GET /stats: merges router in-flight + concurrency with
// livestats ledger snapshot.
func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	var resp statsResponse

	// 1. Router live concurrency + in-flight entries snapshot
	if s.rt != nil {
		lim, inf, wait := s.rt.Concurrency()
		resp.Concurrency.Limit = lim
		resp.Concurrency.InFlight = inf
		resp.Concurrency.Waiting = wait
		resp.Inflight = s.rt.Inflight.Snapshot()
	}
	if resp.Inflight == nil {
		resp.Inflight = []router.InflightEntry{}
	}

	// 2. Livestats completed ledger snapshot (read-cached ~1s: several
	// dashboards polling at once cost one fold, not one each). The cache is
	// keyed by the resolved range tail so ?range= variants don't thrash
	// each other's entries.
	if s.liveStats != nil {
		snap := s.liveStats.CachedSnapshot(parseRangeTail(r.URL.Query().Get("range")))
		resp.Hourly = snap.Hourly
		resp.Daily = snap.Daily
		resp.Overall = snap.Overall
		resp.RecentErrors = snap.RecentErrors
		resp.ByProviderModel = snap.ByProviderModel
		resp.ByClientKeyTag = snap.ByClientKeyTag
		resp.ByKeyLabel = snap.ByKeyLabel
	}
	if resp.Hourly == nil {
		resp.Hourly = []livestats.HourlyRow{}
	}
	if resp.Daily == nil {
		resp.Daily = []livestats.HourlyRow{}
	}
	if resp.RecentErrors == nil {
		resp.RecentErrors = []livestats.RecentErrorRow{}
	}
	if resp.ByProviderModel == nil {
		resp.ByProviderModel = []livestats.ProviderRow{}
	}
	if resp.ByClientKeyTag == nil {
		resp.ByClientKeyTag = []livestats.DimensionRow{}
	}
	if resp.ByKeyLabel == nil {
		resp.ByKeyLabel = []livestats.DimensionRow{}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(resp)
}

// sampleFromRecord maps a completed audit.Record into a livestats.Sample
// according to design §3.2 / §4.2 attribution rules. ErrorClass/Status/
// Attempt feed only the recent_errors ring (contracts §1.6): class and
// status quote the terminal attempt — the winning one when the request
// forwarded, else the last attempt — verbatim, never re-classified;
// Attempt is the 1-based ordinal of the attempt that ended the request
// (0 when there were none).
func sampleFromRecord(rec *audit.Record) livestats.Sample {
	if rec == nil {
		return livestats.Sample{}
	}
	s := livestats.Sample{
		TS:           rec.TS,
		VModel:       rec.Model,
		Protocol:     rec.Protocol,
		Stream:       rec.Stream,
		Outcome:      rec.Outcome,
		ClientKeyTag: rec.ClientKeyTag,
		DurMS:        rec.DurMS,
		TTFTMS:       rec.TTFTMS,
		Attempt:      len(rec.Attempts),
	}
	win := -1
	for i := range rec.Attempts {
		att := &rec.Attempts[i]
		if att.IsForwarded() {
			win = i
			s.Provider = att.Provider
			s.Model = att.Model
			s.KeyLabel = att.KeyLabel
			if att.Tokens != nil {
				s.Tokens = livestats.TokenCounts{
					In:         att.Tokens.In,
					Out:        att.Tokens.Out,
					CacheRead:  att.Tokens.CacheRead,
					CacheWrite: att.Tokens.CacheWrite,
				}
			}
			break
		}
	}
	if len(rec.Attempts) > 0 {
		final := &rec.Attempts[len(rec.Attempts)-1]
		if win >= 0 {
			final = &rec.Attempts[win]
		}
		s.ErrorClass = final.ErrorClass
		if final.Response != nil {
			s.Status = final.Response.Status
		}
	}
	return s
}
