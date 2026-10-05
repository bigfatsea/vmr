// Ver 2026-10-05 17:10, by Claude Opus 4.6

// Online config editing: GET /config, PUT /config, POST /config/validate and
// the /config.html console page (admin.config_edit opt-in).
//
// The design point this file must never betray: the config FILE is the single
// source of truth. These handlers are another pen writing to that file, never
// an in-memory config store — a PUT is validated with the exact hot-reload
// pipeline (config.Parse → Check → router.BuildSnapshot), written atomically
// back to disk, and reloaded through the same reload closure fsnotify and
// SIGHUP use. Every existing diagnostic (/status's config block, ReloadState,
// vmr check, vmr diagnose) therefore keeps describing reality.
package server

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vmr/internal/config"
	"vmr/internal/router"
)

//go:embed config.html
var configHTMLPage []byte

// maxConfigBodyBytes bounds a PUT body. A config file is human-authored YAML;
// 4MB is orders of magnitude above any real one and stops a confused client
// from streaming an arbitrary payload into a temp file.
const maxConfigBodyBytes = 4 << 20

// ConfigReloader runs one hot-reload attempt through the `vmr start` reload
// closure (trigger "api") and returns the attempt it recorded. Nil on every
// instance that has no reload loop wired (tests, future embedding) — PUT
// answers 501 rather than silently writing a file that never takes effect.
type ConfigReloader func() router.ReloadState

// WithConfigReload wires the reload hook. Only `vmr start` calls it, same
// pattern as WithLogTee/WithInstance.
func (s *Server) WithConfigReload(fn ConfigReloader) *Server {
	s.configReload = fn
	return s
}

// configEditGate enforces the admin.config_edit opt-in, read per-request from
// the LIVE routing snapshot (analytics.serve's discipline): the endpoint set
// itself follows hot reload, no restart needed to turn editing on or off.
func configEditGate(w http.ResponseWriter, snap *router.Snapshot) bool {
	if snap.Cfg.Admin.ConfigEdit {
		return true
	}
	router.WriteError(w, http.StatusForbidden, "permission_error",
		"config editing is not enabled on this instance (set admin.config_edit: true in config.yaml)")
	return false
}

// configETag is the content fingerprint PUT's If-Match precondition compares
// against — a plain hash of the file bytes, so it changes on any edit from
// any tool, not just through this API.
func configETag(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:8]) // first 16 hex chars
}

// configLoadedAt is the "when was the running config actually read" basis the
// staleness header compares mtime against: process start, or the last
// accepted reload — the same idiom /status's instance block uses.
func (s *Server) configLoadedAt() time.Time {
	loaded := s.inst.startedAt
	if okAt := s.rt.ReloadState().OKAt; okAt.After(loaded) {
		loaded = okAt
	}
	return loaded
}

// reloadStateLabel names the last reload attempt for X-Reload-State: the zero
// value means no attempt has ever been made, which is normal steady state,
// not an error.
func reloadStateLabel(rs router.ReloadState) string {
	switch {
	case rs.At.IsZero():
		return "never"
	case rs.OK:
		return "ok"
	default:
		return "rejected"
	}
}

// configPage serves the assembled editor shell, unauthenticated like every
// console page: the HTML carries zero business data, the embedded JS calls
// the auth-gated /config endpoints itself (VMRAuth prompts on 401).
func (s *Server) configPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	w.Write(assembleConsolePage(configHTMLPage))
}

// requireEditableConfigPath answers the shared preconditions of all three
// data endpoints: the opt-in gate, and a known config file path (zero outside
// `vmr start`). It reports whether the caller may proceed.
func (s *Server) requireEditableConfig(w http.ResponseWriter, snap *router.Snapshot) bool {
	if !configEditGate(w, snap) {
		return false
	}
	if s.inst.configPath == "" {
		router.WriteError(w, http.StatusServiceUnavailable, "unavailable",
			"config path unknown on this instance (only `vmr start` serves config editing)")
		return false
	}
	return true
}

// adminConfigGet returns the config FILE from disk — not the running
// snapshot's config. The file is the source of truth and the thing being
// edited; handing back a marshaled in-memory object would open a second
// truth that drifts from what the operator (or any editor) wrote. The
// response headers carry what the UI needs to show "what you see is (not)
// what is running": the file's mtime, its content ETag (PUT's If-Match
// precondition), whether the running config is stale relative to this file,
// and the last reload attempt's outcome.
func (s *Server) adminConfigGet(w http.ResponseWriter, _ *http.Request) {
	snap := s.rt.Snapshot()
	if !s.requireEditableConfig(w, snap) {
		return
	}
	body, err := os.ReadFile(s.inst.configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			router.WriteError(w, http.StatusNotFound, "not_found", "config file not found: "+s.inst.configPath)
		} else {
			router.WriteError(w, http.StatusInternalServerError, "internal_error", "reading config file: "+err.Error())
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("X-Config-Path", s.inst.configPath)
	w.Header().Set("ETag", `"`+configETag(body)+`"`)
	if fi, err := os.Stat(s.inst.configPath); err == nil {
		w.Header().Set("X-Config-Mtime", fi.ModTime().UTC().Format(time.RFC3339Nano))
		loaded := s.configLoadedAt()
		stale, _ := router.ConfigStale(s.inst.configPath, loaded)
		w.Header().Set("X-Config-Stale", boolLabel(!loaded.IsZero() && stale))
	}
	w.Header().Set("X-Reload-State", reloadStateLabel(s.rt.ReloadState()))
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func boolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// configValidation bundles the three-step candidate check (Parse → Check →
// BuildSnapshot dry-run). BuildSnapshot's product is discarded — it exists so
// a candidate that would fail snapshot construction (a misspelled provider
// reference, an unresolvable alias) is rejected BEFORE anything touches the
// file, with the same error text the reload would have logged.
type configValidation struct {
	cfg    *config.Config
	issues []config.Issue
	err    error
}

func validateConfigCandidate(body []byte) configValidation {
	cfg, err := config.Parse(body)
	if err != nil {
		return configValidation{err: err}
	}
	issues := cfg.Check()
	if _, err := router.BuildSnapshot(cfg); err != nil {
		return configValidation{cfg: cfg, issues: issues, err: err}
	}
	return configValidation{cfg: cfg, issues: issues}
}

// readConfigBody bounds and reads a candidate config payload.
func readConfigBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxConfigBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			router.WriteError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "config body exceeds limit")
		} else {
			router.WriteError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		}
		return nil, false
	}
	return body, true
}

// adminConfigValidate runs the candidate check without touching the file —
// the editor's "Validate" button, and the answer to "why did vmr reject
// this?" when editing by hand. Always answers 200 with ok:true/false: the
// outcome IS the payload here, not an HTTP-level failure.
func (s *Server) adminConfigValidate(w http.ResponseWriter, r *http.Request) {
	snap := s.rt.Snapshot()
	if !s.requireEditableConfig(w, snap) {
		return
	}
	body, ok := readConfigBody(w, r)
	if !ok {
		return
	}
	v := validateConfigCandidate(body)
	if v.err != nil {
		router.WriteJSON(w, http.StatusOK, map[string]any{"ok": false, "error": v.err.Error()})
		return
	}
	router.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "issues": v.issues, "empty_env_refs": v.cfg.EmptyEnvRefs,
	})
}

// adminConfigPut accepts a full candidate config.yaml and, only after the
// exact hot-reload pipeline accepts it, writes it back atomically and runs
// one reload through the same closure fsnotify/SIGHUP use. The four failure
// shapes answer distinctly: unparseable/unbuildable candidate → 400 with the
// validator's own error text (nothing is written); lost race → 409 via
// If-Match; no reload hook wired → 501 (never write a file that silently
// wouldn't take effect); write failure → 500. A successful PUT returns the
// reload outcome, so the client learns "live" rather than "written and
// hope" — including a rejected reload (possible only through an fsnotify
// race or environment drift, since validation just ran the same code).
//
// The write itself also fires fsnotify, so the debounce timer re-runs the
// reload ~300ms later on identical bytes — idempotent, one duplicate log
// line (registered in KNOWN_ISSUES). reloadMu in cmd_start serializes all
// triggers, so the api reload and the echo reload can never race Install.
func (s *Server) adminConfigPut(w http.ResponseWriter, r *http.Request) {
	snap := s.rt.Snapshot()
	if !s.requireEditableConfig(w, snap) {
		return
	}
	if s.configReload == nil {
		router.WriteError(w, http.StatusNotImplemented, "not_implemented",
			"this instance has no reload hook wired (only `vmr start` accepts config writes)")
		return
	}
	body, ok := readConfigBody(w, r)
	if !ok {
		return
	}
	if im := strings.Trim(r.Header.Get("If-Match"), `"`); im != "" {
		cur, err := os.ReadFile(s.inst.configPath)
		if err != nil || configETag(cur) != im {
			router.WriteError(w, http.StatusConflict, "conflict",
				"config file changed since you read it (If-Match mismatch); GET /config and re-apply")
			return
		}
	}
	v := validateConfigCandidate(body)
	if v.err != nil {
		router.WriteJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": v.err.Error()})
		return
	}
	if err := atomicWriteConfig(s.inst.configPath, body); err != nil {
		router.WriteError(w, http.StatusInternalServerError, "internal_error", "writing config file: "+err.Error())
		return
	}
	st := s.configReload()
	router.WriteJSON(w, http.StatusOK, map[string]any{
		"ok": true, "reloaded": st.OK, "reload_state": reloadStateLabel(st),
		"issues": v.issues, "empty_env_refs": v.cfg.EmptyEnvRefs,
	})
}

// atomicWriteConfig replaces path with body via a same-directory temp file
// plus rename: readers never see a torn file, and the 0600 mode survives —
// the config file carries every upstream credential and must not loosen.
// CreateTemp already uses 0600; the chmod keeps that guarantee local to this
// function instead of leaning on a stdlib default.
func atomicWriteConfig(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName) // no-op after a successful rename
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}
