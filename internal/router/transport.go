// Ver 2026-07-30, by Sonnet 5

// Outbound HTTP transport: building the upstream http.Client and forwarding
// a response body to the client. Split out of router.go — pure move, no
// behavior change.
package router

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"vmr/internal/config"
)

// NewUpstreamClient builds an *http.Client configured exactly like Install
// would for connections to p under protocol (a provider can declare a
// different-scheme base_url per protocol, so the proxy-scheme decision needs
// to know which one — see config.ProxySpecFor): same dial/response-header/
// idle timeouts, same proxy resolution. Standalone one-shot tools (replay,
// diagnose) that need to speak to a single provider without running a Router
// use this instead of duplicating Install's Transport setup — Install itself
// calls this per distinct proxy resolution.
func NewUpstreamClient(cfg *config.Config, p config.Provider, protocol string) *http.Client {
	mode, proxyURL := cfg.ProxySpecFor(p, protocol)
	// nil Proxy = direct. Proxy environment variables are deliberately not
	// consulted — proxies are explicit config (config.Config.HTTPProxy/
	// HTTPSProxy), nothing implicit.
	var proxyFn func(*http.Request) (*url.URL, error)
	if mode == config.ProxyURL {
		if u, err := url.Parse(proxyURL); err == nil { // validated at config load
			proxyFn = http.ProxyURL(u)
		}
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 proxyFn,
			DialContext:           (&net.Dialer{Timeout: cfg.Timeouts.Connect.D()}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second, // zero = unbounded; a stalled handshake isn't covered by the dial timeout
			ResponseHeaderTimeout: cfg.Timeouts.ResponseHeader.D(),
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second, // zero would keep idle conns forever
		},
		// Never follow upstream redirects: POST 301/302/303 would be
		// silently rewritten to GET by the default policy, violating
		// byte-faithful passthrough. LLM APIs almost never send 3xx,
		// but if one does the client sees exactly what a direct call would
		// — the 3xx status, Location header, and body, untouched.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// copyFlush forwards the body chunk by chunk, flushing after every read so
// SSE tokens reach the client immediately. A watchdog aborts the copy when the
// upstream goes silent for longer than idle. On timeout the caller closes the
// body, which unblocks the reader goroutine.
//
// Returning does NOT mean the reader goroutine has stopped touching body. On
// the two early-return paths (idle timeout, client write error) it may still
// be inside — or about to start — one more body.Read, whose writes to body's
// internal state never pass through ch and so have no happens-before edge to
// whatever the caller reads next. Every normal-path chunk is synchronized by
// the ch send/receive; only that trailing read is not. Callers that inspect
// body's state after this returns (forwardSuccess reads Applied()/
// RawPreStrip()/ObservedModel()) are racing with it — see
// docs/KNOWN_ISSUES_sonnet-5.md's entry on copyFlush returning before its
// reader goroutine has stopped touching the body.
func copyFlush(w http.ResponseWriter, body io.Reader, idle time.Duration) error {
	flusher, _ := w.(http.Flusher)
	type chunk struct {
		data []byte
		err  error
	}
	ch := make(chan chunk)
	done := make(chan struct{})
	defer close(done)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := body.Read(buf)
			var data []byte
			if n > 0 {
				data = append([]byte(nil), buf[:n]...)
			}
			select {
			case ch <- chunk{data, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(idle)
	defer timer.Stop()
	for {
		select {
		case c := <-ch:
			if len(c.data) > 0 {
				if _, werr := w.Write(c.data); werr != nil {
					return werr
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if c.err != nil {
				if c.err == io.EOF {
					return nil
				}
				return c.err
			}
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(idle)
		case <-timer.C:
			return errors.New("stream idle timeout")
		}
	}
}
