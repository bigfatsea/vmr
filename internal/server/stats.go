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
// values fall back to the 48h default. 7d is the cap because the in-memory
// rollup only holds ~7 days — it is also the widest window the console offers.
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

	// 1. Router live concurrency + in-flight entries snapshot. Computed fresh
	// on every read — never cached — so the Overview poller sees in-flight
	// activity at its own cadence, independent of the ledger cache below.
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

	// 2. Livestats completed ledger snapshot (read-cached, snapCacheTTL: a
	// burst of polls costs one fold, not one each — the TTL sits above the
	// Overview poller's cadence on purpose). The cache is keyed by the
	// resolved range tail so ?range= variants don't thrash each other's
	// entries.
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
// according to design §3.2 / §4.2 attribution rules. Provider/Model/KeyLabel
// are the terminal attempt's service identity — the winning one when the
// request forwarded, else the last attempt tried — whenever at least one
// attempt was made: a failed request still names the upstream endpoint that
// actually failed, instead of collapsing into an anonymous bucket. Forwarded
// is the separate, authoritative gate livestats uses to decide whether
// tokens/dur/ttft may be counted as service-quality signal (design §4.2) —
// only the winning attempt's data ever populates s.Tokens. ErrorClass/
// Status/Attempt feed only the recent_errors ring (contracts §1.6): class
// and status quote the terminal attempt verbatim, never re-classified;
// Attempt is the 1-based ordinal of the attempt that ended the request
// (0 when there were none). When no attempt was built at all (every
// candidate cooling down → vmr_no_candidates, or a pre-dispatch build
// error), there is no upstream to name at all — Provider/Model/KeyLabel
// stay empty, Status falls back to the client-facing response code — the
// only terminal fact that exists — and ErrorClass synthesizes
// "no_candidate" for error outcomes.
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
		if rec.Attempts[i].IsForwarded() {
			win = i
			break
		}
	}
	if len(rec.Attempts) > 0 {
		final := &rec.Attempts[len(rec.Attempts)-1]
		if win >= 0 {
			final = &rec.Attempts[win]
		}
		// Identity attaches from the terminal attempt regardless of outcome —
		// see doc comment. Tokens only ever come from a genuinely forwarded
		// attempt (win>=0); a failed terminal attempt has no usage to report.
		s.Provider = final.Provider
		s.Model = final.Model
		s.KeyLabel = final.KeyLabel
		s.Forwarded = win >= 0
		if win >= 0 && final.Tokens != nil {
			s.Tokens = livestats.TokenCounts{
				In:         final.Tokens.In,
				Out:        final.Tokens.Out,
				CacheRead:  final.Tokens.CacheRead,
				CacheWrite: final.Tokens.CacheWrite,
			}
		}
		s.ErrorClass = final.ErrorClass
		if final.Response != nil {
			s.Status = final.Response.Status
		}
	} else {
		if rec.Client.Response != nil {
			s.Status = rec.Client.Response.Status
		}
		if rec.Outcome == "error" {
			s.ErrorClass = "no_candidate"
		}
	}
	return s
}
