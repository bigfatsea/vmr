#!/usr/bin/env bash
# Ver 2026-07-10 00:30, by Sonnet 5
#
# vmr.sh — the single command-line entry point for running VMR.
#
# Dev mode (you supervise the process):
#   ./vmr.sh start      validate config, then run vmr in the background (nohup)
#   ./vmr.sh stop       stop the running process
#   ./vmr.sh restart    stop + start
#   ./vmr.sh status     process state + endpoint health (vmr status)
#   ./vmr.sh logs       tail -f the server log
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
# VMR_LOG_DIR/VMR_IMG_CACHE_DIR are handled separately from that scan (see
# LOG_DIR/CACHE_DIR below) — both modes get the same resolved value because
# this script always asks the vmr binary, never guesses.
#
# No PID file in dev mode: the daemon is found by matching the absolute
# binary path in the process table (pgrep -f), which cannot collide with
# this script or other vmr checkouts.

set -euo pipefail
cd "$(dirname "$0")"

BIN="$PWD/vmr"
CFG="$PWD/config.yaml"
MATCH="$BIN start"                    # absolute path → unambiguous process match

ensure_bin() {
  if [[ ! -x "$BIN" ]]; then
    echo "building vmr..."
    go build -o "$BIN" ./cmd/vmr
  fi
}
ensure_bin   # both dirs below query the binary, so it must exist first

# LOG_DIR (audit JSONL + server log) and CACHE_DIR (image-downscale cache)
# both come from `vmr dirs` — the binary's own env-var-or-~/.vmr-or-temp-dir
# default formula (internal/rundir) — instead of a bash copy of that logic,
# so dev mode and service mode can never disagree with what the running
# process actually resolves. Override via VMR_LOG_DIR / VMR_IMG_CACHE_DIR in
# the environment this script runs in; `vmr dirs` picks them up the same way
# `vmr start` itself would.
#
# Audit files rotate daily and auto-compress to .zst on rotation (20-75x
# smaller; vmr report reads both transparently). They're kept forever unless
# you set audit_retention_days in config.yaml — that's the supported way to
# expire them; see docs/AuditLogCompression_Analysis_Sonnet5.md.
LOG_DIR="$("$BIN" dirs log)"
CACHE_DIR="$("$BIN" dirs cache)"
SERVER_LOG="$LOG_DIR/vmr.log"

ENV_FILE="$HOME/.config/vmr/env"
LABEL="com.vmr"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
UNIT_DIR="$HOME/.config/systemd/user"
UNIT="$UNIT_DIR/vmr.service"
# Service-mode server log. On macOS it must be home-local (TCC blocks
# launchd file ops on external volumes); on Linux the dev-mode path is fine.
case "$(uname -s)" in
  Darwin) SVC_LOG="$HOME/Library/Logs/vmr.log" ;;
  *)      SVC_LOG="$SERVER_LOG" ;;
esac

running_pids() { pgrep -f "$MATCH" 2>/dev/null || true; }

# ---------- dev mode (manual supervision) ----------

cmd_start() {
  if [[ -n "$(running_pids)" ]]; then
    echo "vmr already running (pid $(running_pids | tr '\n' ' '))"
    exit 0
  fi
  "$BIN" check -c "$CFG" >/dev/null   # refuse to daemonize a broken config
  mkdir -p "$LOG_DIR" "$CACHE_DIR"
  VMR_LOG_DIR="$LOG_DIR" VMR_IMG_CACHE_DIR="$CACHE_DIR" \
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
    echo "vmr not running"
    return 0
  fi
  # || true: the process can exit between the running_pids check above and
  # this pkill; a no-match exit 1 must not kill the script under set -e.
  pkill -f "$MATCH" || true
  for _ in $(seq 1 20); do
    [[ -z "$(running_pids)" ]] && { echo "vmr stopped"; return 0; }
    sleep 0.25
  done
  echo "vmr did not exit after 5s, sending SIGKILL" >&2
  pkill -9 -f "$MATCH" || true
}

cmd_status() {
  local pids
  pids="$(running_pids)"
  if [[ -z "$pids" ]]; then
    echo "vmr not running"
    exit 1
  fi
  echo "vmr running (pid $(echo "$pids" | tr '\n' ' '))"
  "$BIN" status -c "$CFG"
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

# write_env_file: collect every ${VAR} referenced in config.yaml — plus any
# proxy variables (upstreams may be unreachable without them) — from the
# current shell into ENV_FILE (0600). Never overwrites an existing file —
# edit it by hand after the first install.
write_env_file() {
  if [[ -f "$ENV_FILE" ]]; then
    echo "env file kept: $ENV_FILE (edit it if keys changed)"
    return 0
  fi
  mkdir -p "$(dirname "$ENV_FILE")"
  : > "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  local var missing=()
  for var in $(grep -o '\${[A-Za-z_][A-Za-z0-9_]*}' "$CFG" | tr -d '${}' | sort -u); do
    if [[ -n "${!var:-}" ]]; then
      printf '%s=%s\n' "$var" "${!var}" >> "$ENV_FILE"
    else
      missing+=("$var")
    fi
  done
  for var in HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY http_proxy https_proxy all_proxy no_proxy; do
    [[ -n "${!var:-}" ]] && printf '%s=%s\n' "$var" "${!var}" >> "$ENV_FILE"
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
        <string>set -a; . "$ENV_FILE" 2>/dev/null; set +a; export VMR_LOG_DIR="$LOG_DIR" VMR_IMG_CACHE_DIR="$CACHE_DIR"; exec "$BIN" start -c "$CFG"</string>
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
ExecStart=$BIN start -c $CFG
WorkingDirectory=$PWD
EnvironmentFile=-$ENV_FILE
Environment=VMR_LOG_DIR=$LOG_DIR
Environment=VMR_IMG_CACHE_DIR=$CACHE_DIR
StandardOutput=append:$SERVER_LOG
StandardError=append:$SERVER_LOG
Restart=always
RestartSec=2

[Install]
WantedBy=default.target
EOF
}

svc_install() {
  "$BIN" check -c "$CFG" >/dev/null
  mkdir -p "$LOG_DIR" "$CACHE_DIR"
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

svc_cmd() {
  case "${1:-}" in
    install)   svc_install ;;
    uninstall) svc_uninstall ;;
    start)     svc_start ;;
    stop)      svc_stop ;;
    restart)   svc_stop; sleep 0.5; svc_start ;;
    status)    svc_status ;;
    logs)      exec tail -f "$SVC_LOG" ;;
    *) echo "usage: $0 service {install|uninstall|start|stop|restart|status|logs}" >&2; exit 2 ;;
  esac
}

case "${1:-}" in
  start)   cmd_start ;;
  stop)    cmd_stop ;;
  restart) cmd_stop; cmd_start ;;
  status)  cmd_status ;;
  logs)    exec tail -f "$SERVER_LOG" ;;
  service) shift; svc_cmd "$@" ;;
  *)
    echo "usage: $0 {start|stop|restart|status|logs}                      # dev mode (you supervise)" >&2
    echo "       $0 service {install|uninstall|start|stop|restart|status|logs}   # init system supervises" >&2
    exit 2 ;;
esac
