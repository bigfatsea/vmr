// Ver 2026-07-30, by Sonnet 5

// Shared test scaffolding for internal/server's integration tests: the base
// upstream mock, the router-backed httptest.Server it talks to, and the
// client helper that drives requests at it. Split out of server_test.go
// so that file
// holds only its own tests, not shared plumbing every other file also uses.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"vmr/internal/config"
	"vmr/internal/router"

	_ "vmr/internal/adapter/openai"
)

// newJSONUpstream starts an httptest.Server running handler and registers
// its cleanup — the one piece of boilerplate every mock upstream in this
// package repeats verbatim . Deliberately not a struct each mock embeds: the mocks differ enough
// in behavior (probeUpstream's four modes, anthUpstream's header capture)
// that embedding would couple their evolution for no real reuse beyond this.
func newJSONUpstream(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// extractModelField pulls the top-level "model" field out of a JSON request
// body — the other half of the T2-7 mock boilerplate, shared by every mock
// that just needs to record which model name it was sent.
func extractModelField(body []byte) string {
	var m struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &m)
	return m.Model
}

// upstream is a scriptable mock provider.
type upstream struct {
	srv         *httptest.Server
	hits        atomic.Int32
	status      atomic.Int32 // response status; 200 = success
	lastModel   atomic.Value // model name seen in the last request
	lastHeaders atomic.Value // http.Header received in the last request
	errBody     atomic.Value // optional custom error body (string)
	retryAfter  string
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{}
	u.status.Store(200)
	u.srv = newJSONUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		model := extractModelField(body)
		u.lastModel.Store(model)
		u.lastHeaders.Store(r.Header.Clone())
		st := int(u.status.Load())
		if st != 200 {
			if u.retryAfter != "" {
				w.Header().Set("Retry-After", u.retryAfter)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(st)
			if custom, ok := u.errBody.Load().(string); ok {
				fmt.Fprint(w, custom)
			} else {
				fmt.Fprintf(w, `{"error":{"message":"upstream says %d"}}`, st)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"x","object":"chat.completion","model":%q,"choices":[]}`, model)
	})
	return u
}

func newRouterServer(t *testing.T, yaml string) *httptest.Server {
	t.Helper()
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	rt := router.New(nil)
	snap, err := router.BuildSnapshot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rt.Install(snap)
	ts := httptest.NewServer(New(rt, nil).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func chat(t *testing.T, ts *httptest.Server, body string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

const simpleReq = `{"model":"vm","messages":[{"role":"user","content":"hi"}]}`
