// Ver 2026-09-21, by Sonnet 5

// vmr analyze -open: a one-shot local viewer for this run's output
// directory — distinct from analytics.serve (KNOWN_ISSUES §1.5, "analytics.serve
// 与 -open 的分工"). That one is a long-running, authenticated mount point
// sized for request-browser's real query workload; this is a zero-config,
// single-invocation viewer for the document pages `vmr analyze` just wrote,
// because a report's dashboard pages fetch relative-path JSON over HTTP and
// `file://` cannot serve that. Binds 127.0.0.1
// only, on a random port, never authenticates (single user, single
// machine, data already sitting on local disk the caller can already
// read), and serves until Ctrl-C.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"
)

// buildReportsHandler returns a read-only handler over outDir plus the
// *os.Root the caller must Close. os.Root (Go 1.25) rejects any path escape
// — including through a symlink — without needing a hand-rolled walk like
// internal/server/reports.go's reportsCheckSymlinks; that one is a package-
// private helper serving a different, config-driven mount point, and
// exporting it just for this one-shot command isn't worth the coupling.
func buildReportsHandler(outDir string) (http.Handler, *os.Root, error) {
	root, err := os.OpenRoot(outDir)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", outDir, err)
	}
	return http.FileServerFS(root.FS()), root, nil
}

// newLoopbackListener binds 127.0.0.1 on a random port — never 0.0.0.0.
// reports/ carries full conversation bodies (0600/0700 on disk); the only
// thing standing between that and the network on this command is which
// address it binds.
func newLoopbackListener() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

// serveAndOpen binds a loopback-only random port over outDir, opens the
// default browser at macro-dashboard.html, and blocks until Ctrl-C. It does
// not watch for the browser tab closing (would need a heartbeat, unreliable
// — see the action plan's Step 2 spec) — Ctrl-C is the one exit path.
func serveAndOpen(outDir string) error {
	handler, root, err := buildReportsHandler(outDir)
	if err != nil {
		return fmt.Errorf("analyze -open: %w", err)
	}
	defer root.Close()

	ln, err := newLoopbackListener()
	if err != nil {
		return fmt.Errorf("analyze -open: listen: %w", err)
	}
	defer ln.Close()

	url := fmt.Sprintf("http://%s/macro-dashboard.html", ln.Addr().String())
	fmt.Printf("Serving %s at %s (Ctrl-C to stop)\n", outDir, url)
	if err := openBrowser(url); err != nil {
		fmt.Printf("(could not auto-open browser: %v — open the URL above manually)\n", err)
	}

	srv := &http.Server{Handler: handler}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("analyze -open: serve: %w", err)
	}
}

// openBrowser is best-effort: a failure to launch a browser only prints the
// URL for the caller to click themselves — serveAndOpen keeps serving
// either way (action plan's Step 2 spec: "失败不算错误").
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
