// Ver 2026-07-30, by Sonnet 5

// Outbound HTTP transport: building the upstream http.Client and forwarding
// a response body to the client. Split out of router.go — pure move, no
// behavior change.
package router

import (
	"context"
	"errors"
	"fmt"
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
	maxIdle := 16
	if cfg.MaxConcurrency > maxIdle {
		maxIdle = cfg.MaxConcurrency
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 proxyFn,
			DialContext:           (&net.Dialer{Timeout: cfg.Timeouts.Connect.D()}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second, // zero = unbounded; a stalled handshake isn't covered by the dial timeout
			ResponseHeaderTimeout: cfg.Timeouts.ResponseHeader.D(),
			MaxIdleConnsPerHost:   maxIdle,
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
// Returning does not wait for the reader goroutine to exit on early-return paths
// (idle timeout, client write error / disconnect) — the caller closing the body
// unblocks the reader. Inspection methods on NormalizerStream (Applied, RawPreStrip,
// ObservedModel, Usage, OutTokens) are protected by a mutex, guaranteeing safe,
// race-free reads even if a trailing read executes concurrently.
//
// Write errors from the client side are returned as *clientWriteError so the
// caller can distinguish a client disconnect from an upstream read failure
// (Q08).

// clientWriteError wraps a write-to-client failure so forwardSuccess can
// distinguish it from an upstream read error — a client that disconnected
// before the response completed should not be counted as an upstream
// TRUNCATED.
type clientWriteError struct{ err error }

func (e *clientWriteError) Error() string { return "write to client: " + e.err.Error() }
func (e *clientWriteError) Unwrap() error { return e.err }

// isClientWriteError reports whether err is a *clientWriteError, possibly
// wrapped (errors.As semantics).
func isClientWriteError(err error) bool {
	var cwe *clientWriteError
	return errors.As(err, &cwe)
}
func copyFlush(ctx context.Context, w http.ResponseWriter, body io.Reader, idle time.Duration) error {
	flusher, _ := w.(http.Flusher)
	type chunk struct {
		data []byte
		err  error
	}
	ch := make(chan chunk)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				// respnorm's state machine runs on this goroutine over bytes
				// fully controlled by the upstream; a malformed-stream panic
				// here must not kill the process. Surface it as an upstream
				// read failure (a non-clientWriteError) so the caller takes
				// the TRUNCATED path, never a silent clean success.
				select {
				case ch <- chunk{err: fmt.Errorf("upstream stream panic: %v", p)}:
				case <-done:
				}
			}
		}()
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
		case <-ctx.Done():
			return ctx.Err()
		case c := <-ch:
			if len(c.data) > 0 {
				if _, werr := w.Write(c.data); werr != nil {
					return &clientWriteError{werr}
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
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idle)
		case <-timer.C:
			return errors.New("stream idle timeout")
		}
	}
}
