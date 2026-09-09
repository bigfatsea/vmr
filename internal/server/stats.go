// Ver 2026-09-09, by pi

// The live stats HTTP and collection surface: /stats (JSON API, auth-gated)
// and /stats.html (self-contained static dashboard).
//
// Ownership split (LiveStats design §5.1): livestats.Aggregator owns the
// completed-request ledger (slim WAL, hourly rollups, ring percentiles);
// router.InflightRegistry owns the in-flight per-request live entries.
// GET /stats merges both into one JSON payload at read time.
package server

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"vmr/internal/audit"
	"vmr/internal/livestats"
	"vmr/internal/router"
)

//go:embed stats.html
var statsHTMLPage []byte

// WithLiveStats wires the completed-request aggregator into the server.
// nil-safe: absent stats leaves /stats serving only in-flight + concurrency.
func (s *Server) WithLiveStats(l *livestats.Aggregator) *Server {
	s.liveStats = l
	return s
}

// statsPage serves the self-contained static HTML dashboard for live stats.
// Unauthenticated: the HTML/JS shell contains zero business data. The
// embedded JS calls GET /stats, which enforces s.auth().
func (s *Server) statsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(statsHTMLPage)
}

// statsResponse is the JSON wire contract for GET /stats (§8).
type statsResponse struct {
	Concurrency struct {
		Limit    int   `json:"limit"`
		InFlight int64 `json:"in_flight"`
		Waiting  int64 `json:"waiting"`
	} `json:"concurrency"`
	Inflight        []router.InflightEntry       `json:"inflight"`
	Hourly          []livestats.HourlyRow        `json:"hourly"`
	Daily           []livestats.HourlyRow        `json:"daily"`
	ByProviderModel []livestats.ProviderRow      `json:"by_provider_model"`
	ByClientKeyTag  []livestats.DimensionRow     `json:"by_client_key_tag"`
	ByKeyLabel      []livestats.DimensionRow     `json:"by_key_label"`
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

	// 2. Livestats completed ledger snapshot
	if s.liveStats != nil {
		snap := s.liveStats.Snapshot()
		resp.Hourly = snap.Hourly
		resp.Daily = snap.Daily
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
// according to design §3.2 / §4.2 attribution rules.
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
	}
	for i := range rec.Attempts {
		att := &rec.Attempts[i]
		if att.IsForwarded() {
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
	return s
}
