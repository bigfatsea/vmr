// Statistics mount points and the live in-flight view.
//
// Ownership split: internal/stats owns everything under /stats (the event
// log, the query engine, the statistics page and its assets) and knows
// nothing about this server beyond the Event values the completion hook
// hands it; internal/statsfeed is the one projection from an audit record
// to an Event. This file only mounts those handlers behind s.auth and keeps
// /status/live — router runtime state (concurrency gates, in-flight
// requests), which is not statistics and never touches the event log.
package server

import (
	"net/http"
	"sync"

	"vmr/internal/audit"
	"vmr/internal/router"
	"vmr/internal/stats"
	"vmr/internal/statsfeed"
)

// WithStats wires the statistics store into the server. nil-safe: without
// a store the /stats data routes answer 503 and nothing is booked.
func (s *Server) WithStats(st *stats.Store) *Server {
	s.stats = st
	return s
}

// bookStats hands one completed request to the statistics store.
func (s *Server) bookStats(rec *audit.Record) {
	if s.stats == nil {
		return
	}
	if e, ok := statsfeed.FromRecord(rec); ok {
		s.stats.Record(e)
	}
}

var (
	assembledStatsPage []byte
	assembleStatsOnce  sync.Once
)

// mountStats registers the statistics module's routes. The page shell and
// static assets are unauthenticated (no data in them, same as every console
// page); the data routes go through s.auth. The bare /stats was the old
// JSON endpoint and is now only a redirect for anyone who typed it.
func (s *Server) mountStats(mux *http.ServeMux) {
	h := stats.NewHandler(s.stats)
	mux.HandleFunc("GET /stats.html", func(w http.ResponseWriter, r *http.Request) {
		assembleStatsOnce.Do(func() { assembledStatsPage = assembleConsolePage(stats.PageHTML()) })
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(assembledStatsPage)
	})
	mux.HandleFunc("GET /stats/assets/", stats.Asset)
	mux.HandleFunc("GET /stats/schema", s.auth(h.Schema))
	mux.HandleFunc("GET /stats/query", s.auth(h.Query))
	mux.HandleFunc("GET /stats/events", s.auth(h.Events))
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/stats.html", http.StatusFound)
	})
	mux.HandleFunc("GET /status/live", s.auth(s.adminLive))
}

// liveResponse is the GET /status/live contract: the in-flight half of the
// console, polled every second while requests are running, so it carries
// nothing but router runtime state.
type liveResponse struct {
	Concurrency struct {
		Limit    int   `json:"limit"`
		InFlight int64 `json:"in_flight"`
		Waiting  int64 `json:"waiting"`
	} `json:"concurrency"`
	ProvidersConcurrency map[string]router.ProviderConcurrencyStats `json:"providers_concurrency,omitempty"`
	Inflight             []router.InflightEntry                     `json:"inflight"`
	RecentlyEnded        []router.InflightEntry                     `json:"recently_ended"`
}

func (s *Server) adminLive(w http.ResponseWriter, r *http.Request) {
	resp := liveResponse{Inflight: []router.InflightEntry{}, RecentlyEnded: []router.InflightEntry{}}
	if s.rt != nil {
		lim, inf, wait := s.rt.Concurrency()
		resp.Concurrency.Limit, resp.Concurrency.InFlight, resp.Concurrency.Waiting = lim, inf, wait
		if s.rt.ProviderLimiters != nil {
			resp.ProvidersConcurrency = s.rt.ProviderLimiters.Snapshot()
		}
		if e := s.rt.Inflight.Snapshot(); e != nil {
			resp.Inflight = e
		}
		if e := s.rt.Inflight.Ended(); e != nil {
			resp.RecentlyEnded = e
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	router.WriteJSON(w, http.StatusOK, resp)
}
