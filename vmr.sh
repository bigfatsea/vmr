#!/usr/bin/env bash
# Ver 2026-07-28 11:20, by Opus 5
#
# vmr.sh — the single command-line entry point for running VMR.
#
# Dev mode (you supervise the process):
#   ./vmr.sh start      validate config, then run vmr in the background (nohup)
#   ./vmr.sh stop       stop the running process
#   ./vmr.sh restart    stop + start
#   ./vmr.sh status     process state + endpoint health (vmr status)
#   ./vmr.sh ps         every vmr instance on this machine: port + config
#   ./vmr.sh logs       tail -f the server log
#
# Everything else is forwarded verbatim to the vmr binary, so this script is
# the only entry point you ever need:
#   ./vmr.sh check | diagnose | replay | report | <any future subcommand>
# See "passthrough" below for the two things that forwarding is not:
# a whitelist (unknown subcommands reach the binary and get its own usage
# text), and a cwd change (relative paths still resolve from where you stand).
#
# Service mode (the OS init system supervises: crash restart + start at login/boot):
#   ./vmr.sh service install     render + register the launchd/systemd unit, start it
#                                (idempotent: run again after changing paths/config to update)
#   ./vmr.sh service uninstall   stop, unregister, remove the unit file
#   ./vmr.sh service start|stop|restart|status|logs
#
# macOS → launchd user agent (~/Library/LaunchAgents/com.vmr.plist, KeepAlive).
# Linux → systemd user unit (~/.config/systemd/user/vmr.service, Restart=always).
#   For a system-wide Linux install, copy the rendered unit to
#   /etc/systemd/system/ and use systemctl without --user.
#
# Secrets: init systems start with a clean environment — your shell exports
# (API keys) are NOT inherited. Service mode loads ~/.config/vmr/env instead;
# `service install` generates it (0600) from the current shell for every
# ${VAR} referenced in config.yaml, and never overwrites an existing one.
# Directories (audit log, image cache) are config.yaml fields (log_dir /
# image_cache_dir) read by the binary itself — nothing to inject here.
#
# No PID file in dev mode: the daemon is found by matching the absolute
# binary path in the process table (pgrep -f), which cannot collide with
# this script or other vmr checkouts.
#
# Build is never implicit: this script never runs `go build`, so a code
# change only takes effect once you rebuild ./vmr yourself. `start` and
# `service install` print a one-line warning (not a rebuild, not a refusal
# to start) when source under cmd/vmr or internal/ is newer than ./vmr.

set -euo pipefail
# Captured before the cd: passthrough runs the binary from here, so a
# relative path a caller typed (audit globs, -o, -c) means what it meant in
# their shell, not what it happens to mean inside this checkout.
ORIG_PWD="$PWD"
cd "$(dirname "$0")"

BIN="$PWD/vmr"
CFG="$PWD/config.yaml"
MATCH="$BIN start"                    # absolute path → unambiguous process match

# This script never builds vmr itself — build (or rebuild after a code
# change) is always an explicit, separate step, so nothing here can trigger
# an unexpected `go build` or depend on a Go toolchain being installed. A
# service-mode deployment routinely ships only the compiled binary.
require_bin() {
  if [[ ! -x "$BIN" ]]; then
    echo "vmr binary not found: $BIN" >&2
    echo "build it first: go build -o vmr ./cmd/vmr" >&2
    exit 1
  fi
}
require_bin   # resolve_log_dir below queries the binary, so it must exist first

# warn_if_stale: a nudge, not a gate — prints one line if any source file
# that actually feeds this binary is newer than it, but never blocks or
# rebuilds. Called only from the two places that launch something new
# (cmd_start, svc_install), not from require_bin itself: stop/status/logs
# don't care whether the binary is stale, and warning there would just be
# noise on every invocation.
#
# Scoped to cmd/vmr and internal/ — what `go build ./cmd/vmr` actually
# compiles — rather than the whole tree: loadtest/, docs/, test fixtures
# etc. touch mtimes constantly (edits, branch switches, stash pops) with no
# bearing on this binary, and the old auto-build this replaced used to
# rebuild on exactly that kind of unrelated change. A false positive here
# costs one spurious line; it must never cost a blocked start (see the
# no-auto-build rationale above: an implicit rebuild silently upgrades a
# running service's behavior, which is the actual danger — a printed
# warning that a human reads before restarting is not).
warn_if_stale() {
  local newer
  newer="$(find cmd/vmr internal -name '*.go' -newer "$BIN" -print -quit 2>/dev/null)"
  if [[ -n "$newer" ]]; then
    echo "warning: $BIN may be older than the current source (e.g. $newer changed since it was built)" >&2
    echo "  rebuild with: go build -o vmr ./cmd/vmr" >&2
  fi
}

# LOG_DIR (where this script drops the server stderr log, next to the audit
# JSONL) comes from `vmr check -c $CFG log` — the binary's own resolution of
# the config's log_dir field — instead of a bash copy of that logic, so this
# script can never disagree with where the running process actually writes.
# The image cache dir is the binary's business alone now (config
# image_cache_dir); the script no longer needs it.
#
# Resolved LAZILY (resolve_log_dir, called only by the commands that actually
# need it: start, service install, logs) rather than up here unconditionally:
# `vmr check log` still loads and validates config.yaml (it reads log_dir
# from it), so resolving it eagerly would make `stop`/`status`/`service
# uninstall` fail whenever config.yaml is temporarily broken — exactly when
# you most need to be able to stop a bad deploy without first fixing the
# config.
#
# Audit files rotate daily and auto-compress to .zst on rotation (20-75x
# smaller; vmr report reads both transparently). They're kept forever unless
# you set audit_retention_days in config.yaml — that's the supported way to
# expire them; see the design doc §9.5 (the standalone compression analysis was folded in there).
resolve_log_dir() {
  # NOT `[[ cond ]] && action`: with that form, a false test as the LAST
  # statement of a function makes the function return non-zero, and under
  # set -e a bare `resolve_log_dir` call at the call site then aborts the
  # whole script — the short-circuit exemption only covers the && list
  # itself, not the function-call boundary around it.
  if [[ -z "${LOG_DIR:-}" ]]; then
    LOG_DIR="$("$BIN" check -c "$CFG" log)"
    SERVER_LOG="$LOG_DIR/vmr.log"
    if [[ -z "$SVC_LOG" ]]; then
      SVC_LOG="$SERVER_LOG"
    fi
  fi
}

ENV_FILE="$HOME/.config/vmr/env"
LABEL="com.vmr"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
UNIT_DIR="$HOME/.config/systemd/user"
UNIT="$UNIT_DIR/vmr.service"
# Service-mode server log. On macOS it's home-local (TCC blocks launchd file
# ops on external volumes) and needs no config, so it's set here; on Linux
# it's the same path as the dev-mode server log, filled in by resolve_log_dir
# (empty until then — only commands that call it need SVC_LOG on Linux).
case "$(uname -s)" in
  Darwin) SVC_LOG="$HOME/Library/Logs/vmr.log" ;;
  *)      SVC_LOG="" ;;
esac

running_pids() { pgrep -f "$MATCH" 2>/dev/null || true; }

# describe_pids PIDS: one "pid  command line" per line, for diagnostic output.
describe_pids() {
  local pid
  while IFS= read -r pid; do
    [[ -n "$pid" ]] && ps -o pid=,command= -p "$pid" 2>/dev/null
  done <<<"$1"
}

# port_holder ADDR: prints "pid command" for whatever process (if any) is
# listening on ADDR ("host:port", as printed by `vmr check`'s listen:...),
# or nothing. Best-effort — silently no-ops if lsof isn't installed (some
# minimal Linux images lack it) rather than failing the whole script over a
# diagnostic aid; IPv6 listen addresses (with colons of their own) aren't
# handled, since this project's configs only ever use IPv4 host:port.
port_holder() {
  local port="${1##*:}"
  command -v lsof >/dev/null 2>&1 || return 0
  # lsof exits 1 (not an error) when nothing matches — under this script's
  # set -e -o pipefail, that would otherwise silently abort the whole
  # script from inside a `holder="$(port_holder ...)"` assignment. The
  # trailing `return 0` makes this function's own exit status always
  # reflect "ran fine", independent of whether it found something to print.
  lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | awk 'NR==2{print $2, $1}'
  return 0
}

# ---------- dev mode (manual supervision) ----------

cmd_start() {
  if [[ -n "$(running_pids)" ]]; then
    echo "vmr already running (pid $(running_pids | tr '\n' ' '))"
    exit 0
  fi
  local check_out
  check_out="$("$BIN" check -c "$CFG")"   # refuse to daemonize a broken config
  # Catch "something else already has this port" before spawning at all —
  # it's a different checkout/deployment sharing the same listen address,
  # invisible to running_pids' own-binary-path match (see MATCH above), and
  # the raw bind error from vmr itself doesn't explain that.
  local listen_addr holder
  listen_addr="$(sed -n 's/.*listen:[[:space:]]*\([^ ]*\).*/\1/p' <<<"$check_out" | head -1)"
  if [[ -n "$listen_addr" ]]; then
    holder="$(port_holder "$listen_addr")"
    if [[ -n "$holder" ]]; then
      echo "vmr failed to start: $listen_addr is already in use by pid ${holder%% *}" >&2
      echo "  $(ps -o pid=,command= -p "${holder%% *}" 2>/dev/null)" >&2
      echo "that process isn't managed by this script (different vmr checkout or deployment?)." >&2
      echo "stop it directly, or point this checkout's config.yaml 'listen' at a different port." >&2
      exit 1
    fi
  fi
  warn_if_stale
  resolve_log_dir
  mkdir -p "$LOG_DIR"
  nohup "$BIN" start -c "$CFG" >>"$SERVER_LOG" 2>&1 &
  disown
  sleep 0.5
  if [[ -n "$(running_pids)" ]]; then
    echo "vmr started (pid $(running_pids | tr '\n' ' '), log $SERVER_LOG)"
  else
    echo "vmr failed to start — last log lines:" >&2
    tail -n 20 "$SERVER_LOG" >&2
    exit 1
  fi
}

cmd_stop() {
  local pids
  pids="$(running_pids)"
  if [[ -z "$pids" ]]; then
    echo "vmr not running (no process matches: $MATCH)"
    return 0
  fi
  echo "found $(wc -l <<<"$pids" | tr -d ' ') matching process(es):"
  describe_pids "$pids" | sed 's/^/  /'
  echo "sending SIGTERM (pkill -f \"$MATCH\")..."
  # || true: the process can exit between the running_pids check above and
  # this pkill; a no-match exit 1 must not kill the script under set -e.
  pkill -f "$MATCH" || true
  for _ in $(seq 1 20); do
    [[ -z "$(running_pids)" ]] && { echo "vmr stopped (pid $(tr '\n' ' ' <<<"$pids"))"; return 0; }
    sleep 0.25
  done
  echo "vmr did not exit after 5s, sending SIGKILL" >&2
  pkill -9 -f "$MATCH" || true
  sleep 0.25
  if [[ -z "$(running_pids)" ]]; then
    echo "vmr stopped via SIGKILL (pid $(tr '\n' ' ' <<<"$pids"))"
  else
    echo "vmr still running after SIGKILL — inspect manually: pgrep -f \"$MATCH\"" >&2
    exit 1
  fi
}

cmd_status() {
  local pids
  pids="$(running_pids)"
  if [[ -z "$pids" ]]; then
    echo "vmr not running"
    # This only ever looked for *this* checkout's binary path (see MATCH);
    # another checkout's instance is running as far as the machine cares.
    echo "  (other instances on this machine, if any: $0 ps)"
    exit 1
  fi
  echo "vmr running (pid $(echo "$pids" | tr '\n' ' '))"
  if "$BIN" status -c "$CFG" 2>/dev/null; then
    return 0
  fi
  # config.yaml didn't load, so `status -c` couldn't even work out which
  # port to ask. That is not an edge case here — it is precisely the
  # situation this command exists to explain: a config broken mid-edit, or
  # the rejected hot reload you are trying to diagnose (the process is
  # still happily serving the last good snapshot). Fall back to the port
  # the process actually holds, which needs no config at all.
  local addr
  addr="$(listen_addr_of "$(head -1 <<<"$pids")")"
  if [[ -n "$addr" ]]; then
    echo "note: config.yaml does not load — querying the running process directly on $addr" >&2
    "$BIN" status -addr "$addr"
    return 0
  fi
  "$BIN" status -c "$CFG"   # no port to fall back to: re-run so the real error surfaces
}

# cmd_ps: every vmr instance running on this machine, whatever checkout or
# config it came from — the one question `status` cannot answer, because
# `status` starts from *this* checkout's config and can only ever find the
# instance that config points at.
#
# Three steps, each doing only what it is actually good at:
#   1. pgrep — which processes are vmr servers (any checkout, any config)
#   2. lsof — which TCP port each one holds (the listen address lives in
#               that process's config, not on its command line, so the
#               process table alone genuinely cannot tell you)
#   3. vmr status -addr PORT -brief — everything else, asked of the instance
#               itself over /admin/status. Deliberately not parsed here:
#               the binary already speaks JSON, and making bash do it would
#               either drag in a jq dependency or hand-roll a JSON parser.
#
# Note this uses *this* checkout's binary to interrogate instances that may
# be running a different build — fine, because -addr only speaks HTTP to a
# stable endpoint; nothing about the other process's binary is touched.
#
# Degradations, both deliberately non-fatal: no lsof → no port, so the row
# falls back to the -c argument off the command line; a process that has a
# port but doesn't answer /admin/status (starting up, wedged, or not a vmr
# at all) → same fallback row, flagged, rather than a disappeared instance.
listen_addr_of() {
  command -v lsof >/dev/null 2>&1 || return 0
  # -a is load-bearing: lsof ORs its selection filters by default, so
  # `-p PID -iTCP` without it means "this process OR any TCP socket" and
  # happily returns some unrelated daemon's port. $9 is the NAME column
  # ("127.0.0.1:8800" / "*:8800"); first LISTEN socket only — vmr binds
  # exactly one. `return 0`: lsof exits 1 on no match, which under set -e
  # would abort the caller's assignment.
  lsof -nP -a -p "$1" -iTCP -sTCP:LISTEN 2>/dev/null | awk 'NR>1 {print $9; exit}'
  return 0
}

ps_row_fmt='%-7s %-22s %-10s %-7s %-14s %s\n'

cmd_ps() {
  local pids pid addr row p l u m v st c cfgarg why
  # (^|/)vmr start — anchored so a bare-name launch off $PATH still matches
  # while "vmr.sh start" (this script, mid-run) does not.
  pids="$(pgrep -f '(^|/)vmr start' 2>/dev/null || true)"
  if [[ -z "$pids" ]]; then
    echo "no vmr instance running on this machine"
    return 0
  fi
  command -v lsof >/dev/null 2>&1 \
    || echo "note: lsof not installed — listen ports and live details unavailable, showing process table only" >&2
  # shellcheck disable=SC2059
  printf "$ps_row_fmt" PID LISTEN UPTIME MODELS VERSION CONFIG
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    addr="$(listen_addr_of "$pid")"
    row=""
    if [[ -n "$addr" ]]; then
      row="$("$BIN" status -addr "$addr" -brief 2>/dev/null || true)"
    fi
    if [[ -n "$row" ]]; then
      IFS=$'\t' read -r p l u m v st c <<<"$row"
      # The stale flag rides in its own field rather than pre-formatted by
      # the binary: -brief stays machine-readable, and CONFIG being the last
      # column makes appending a marker to it safe.
      [[ "$st" == "stale" ]] && c="$c  [config file is newer — see: $0 status]"
      # shellcheck disable=SC2059
      printf "$ps_row_fmt" "$p" "$l" "$u" "$m" "$v" "$c"
    else
      # Fallback config path: the -c argument as typed, which unlike the
      # instance's own answer may be relative to a working directory we
      # don't know — hence the marker, so a reader doesn't mistake it for
      # a resolved path.
      cfgarg="$(ps -o command= -p "$pid" 2>/dev/null | sed -n 's/.*-c[= ]\([^ ]*\).*/\1/p')"
      if [[ -n "$addr" ]]; then
        why="no answer on /admin/status"
      else
        why="port unknown (lsof)"
      fi
      # shellcheck disable=SC2059
      printf "$ps_row_fmt" "$pid" "${addr:-?}" "?" "?" "?" "${cfgarg:-?}  ($why, config as typed)"
    fi
  done <<<"$pids"
}

# ---------- service mode (init-system supervision) ----------

platform() {
  case "$(uname -s)" in
    Darwin) echo launchd ;;
    Linux)  command -v systemctl >/dev/null || { echo "systemctl not found" >&2; exit 1; }
            echo systemd ;;
    *)      echo "unsupported platform: $(uname -s)" >&2; exit 1 ;;
  esac
}

# write_env_file: collect every ${VAR} referenced in config.yaml from the
# current shell into ENV_FILE (0600). Nothing is grabbed implicitly — proxy
# settings live in config.yaml (http_proxy/https_proxy fields), and if the
# config references ${HTTPS_PROXY} explicitly, this generic scrape picks it
# up like any other variable. Never overwrites an existing file — edit it
# by hand after the first install.
write_env_file() {
  if [[ -f "$ENV_FILE" ]]; then
    echo "env file kept: $ENV_FILE (edit it if keys changed)"
    return 0
  fi
  mkdir -p "$(dirname "$ENV_FILE")"
  : > "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  local var missing=()
  # Single quotes here are deliberate, not a missed-expansion bug: both the
  # grep pattern and the tr charset must match the literal text `${VAR}` in
  # config.yaml, not have $CFG's shell expand anything before grep sees it.
  # shellcheck disable=SC2016
  for var in $(grep -o '\${[A-Za-z_][A-Za-z0-9_]*}' "$CFG" | tr -d '${}' | sort -u); do
    if [[ -n "${!var:-}" ]]; then
      printf '%s=%s\n' "$var" "${!var}" >> "$ENV_FILE"
    else
      missing+=("$var")
    fi
  done
  echo "env file written: $ENV_FILE ($(grep -c . "$ENV_FILE" || true) vars)"
  if [[ ${#missing[@]} -gt 0 ]]; then
    echo "WARNING: not set in current shell, add to $ENV_FILE manually: ${missing[*]}" >&2
  fi
}

# The plist avoids every launchd-performed file operation on the repo path:
# with the repo on an external volume, TCC lets the vmr *process* write there
# (audit) but blocks launchd/sh file ops (spawn dies with EX_CONFIG or
# "Operation not permitted"). So WorkingDirectory is $HOME and the server log
# goes to ~/Library/Logs/vmr.log — the macOS convention anyway. The env file
# is sourced under `set -a` so plain VAR=value lines are exported to vmr.
render_plist() {
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/sh</string>
        <string>-c</string>
        <string>set -a; . "$ENV_FILE" 2>/dev/null; set +a; exec "$BIN" start -c "$CFG"</string>
    </array>
    <key>WorkingDirectory</key><string>$HOME</string>
    <key>StandardOutPath</key><string>$SVC_LOG</string>
    <key>StandardErrorPath</key><string>$SVC_LOG</string>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
</dict>
</plist>
EOF
}

render_unit() {
  mkdir -p "$UNIT_DIR"
  cat > "$UNIT" <<EOF
[Unit]
Description=vmr - Virtual Model Router
After=network-online.target

[Service]
ExecStart="$BIN" start -c "$CFG"
WorkingDirectory=$PWD
EnvironmentFile=-$ENV_FILE
StandardOutput=append:$SERVER_LOG
StandardError=append:$SERVER_LOG
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
EOF
}

svc_install() {
  warn_if_stale
  "$BIN" check -c "$CFG" >/dev/null
  resolve_log_dir
  mkdir -p "$LOG_DIR"
  write_env_file
  # A dev-mode process would fight the service over the listen port.
  if [[ -n "$(running_pids)" ]]; then
    echo "stopping dev-mode process first..."
    cmd_stop
  fi
  case "$(platform)" in
    launchd)
      launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true   # update = re-register
      render_plist
      launchctl bootstrap "gui/$(id -u)" "$PLIST"
      echo "installed + started: $PLIST (launchd, KeepAlive, runs at login)"
      ;;
    systemd)
      render_unit
      systemctl --user daemon-reload
      systemctl --user enable --now vmr.service
      echo "installed + started: $UNIT (systemd user unit, Restart=always)"
      echo "note: to keep it running after logout: loginctl enable-linger $USER"
      ;;
  esac
}

svc_uninstall() {
  case "$(platform)" in
    launchd)
      launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
      rm -f "$PLIST"
      ;;
    systemd)
      systemctl --user disable --now vmr.service 2>/dev/null || true
      rm -f "$UNIT"
      systemctl --user daemon-reload
      ;;
  esac
  echo "service uninstalled (dev mode ./vmr.sh start still works)"
}

svc_start() {
  if [[ -n "$(running_pids)" ]] && ! svc_registered; then
    echo "stopping dev-mode process first..."
    cmd_stop
  fi
  case "$(platform)" in
    launchd)
      [[ -f "$PLIST" ]] || { echo "not installed — run: ./vmr.sh service install" >&2; exit 1; }
      launchctl bootstrap "gui/$(id -u)" "$PLIST" 2>/dev/null \
        || echo "already loaded"
      ;;
    systemd) systemctl --user start vmr.service ;;
  esac
}

svc_stop() {
  case "$(platform)" in
    # bootout, not kill: KeepAlive would resurrect a killed process. The
    # plist stays on disk; `service start` re-bootstraps it.
    launchd) launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || echo "not loaded" ;;
    systemd) systemctl --user stop vmr.service ;;
  esac
}

svc_registered() {
  case "$(platform)" in
    launchd) launchctl print "gui/$(id -u)/$LABEL" >/dev/null 2>&1 ;;
    systemd) systemctl --user is-active --quiet vmr.service ;;
  esac
}

svc_status() {
  case "$(platform)" in
    launchd)
      if svc_registered; then
        echo "service loaded (launchd $LABEL)"
        launchctl print "gui/$(id -u)/$LABEL" | grep -E "state|pid" | head -3 || true
      else
        echo "service not loaded"
        exit 1
      fi
      ;;
    systemd) systemctl --user status vmr.service --no-pager ;;
  esac
  "$BIN" status -c "$CFG" || true
}

# ---------- passthrough to the binary ----------

# has_c_flag ARGS...: true if the caller already named a config file. Covers
# all four spellings Go's flag package accepts (-c X, -c=X, --c X, --c=X).
has_c_flag() {
  local a
  for a in "$@"; do
    case "$a" in -c|--c|-c=*|--c=*) return 0 ;; esac
  done
  return 1
}

# passthrough SUB ARGS...: forward a subcommand this script doesn't own to
# the binary. Deliberately NOT a whitelist — a subcommand added to the
# binary tomorrow works here today, and a typo gets vmr's own usage text
# instead of a stale list maintained in bash.
#
# Two adjustments, both about paths:
#
#  1. cd back to ORIG_PWD. Every subcommand takes paths (audit globs for
#     report, -o, -detail, -record...), and the binary resolves them against
#     its cwd. Running it from this checkout instead of the caller's shell
#     would silently resolve `vmr.sh report audit.jsonl` against the repo.
#  2. Inject -c "$CFG" when the subcommand has a -c flag and the caller
#     omitted it. Needed *because* of (1): the binary's own default is the
#     relative "config.yaml", which after (1) would mean the caller's cwd,
#     not this checkout's config — and "the config next to the script" is
#     the only sensible default for a script whose whole job is running
#     this checkout. The list is the subcommands that actually define -c;
#     injecting it on one that doesn't is a hard error (flag.ExitOnError on
#     an undefined flag), not a harmless extra.
#     A future -c-taking subcommand missing from this list degrades to
#     "type -c yourself", never to a wrong config.
passthrough() {
  local sub="$1"; shift
  local args=("$@")
  case "$sub" in
    start|check|status|diagnose|replay|report|story)
      has_c_flag "$@" || args=(-c "$CFG" "$@")
      ;;
  esac
  cd "$ORIG_PWD"
  # ${args[@]+...}: an empty array under set -u is an unbound-variable error
  # in bash 3.2 (macOS's /bin/bash) — reachable via a bare `./vmr.sh report`.
  exec "$BIN" "$sub" ${args[@]+"${args[@]}"}
}

svc_cmd() {
  case "${1:-}" in
    install)   svc_install ;;
    uninstall) svc_uninstall ;;
    start)     svc_start ;;
    stop)      svc_stop ;;
    restart)   svc_stop; sleep 0.5; svc_start ;;
    status)    svc_status ;;
    logs)      resolve_log_dir; exec tail -f "$SVC_LOG" ;;
    *) echo "usage: $0 service {install|uninstall|start|stop|restart|status|logs}" >&2; exit 2 ;;
  esac
}

case "${1:-}" in
  start)   cmd_start ;;
  stop)    cmd_stop ;;
  restart) cmd_stop; cmd_start ;;
  status)  cmd_status ;;
  ps)      cmd_ps ;;
  logs)    resolve_log_dir; exec tail -f "$SERVER_LOG" ;;
  service) shift; svc_cmd "$@" ;;
  "")
    echo "usage: $0 {start|stop|restart|status|ps|logs}                   # dev mode (you supervise)" >&2
    echo "       $0 service {install|uninstall|start|stop|restart|status|logs}   # init system supervises" >&2
    echo "       $0 <check|diagnose|report|replay|...> [args]         # forwarded to vmr (defaults -c $CFG)" >&2
    exit 2 ;;
  # Not a script-owned subcommand — the binary decides whether it exists.
  # `vmr start` in the foreground is the one thing this shadows: run
  # ./vmr start -c config.yaml directly if you want it unsupervised.
  *) passthrough "$@" ;;
esac
