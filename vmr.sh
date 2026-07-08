#!/usr/bin/env bash
# Ver 2026-07-08 07:55, by Fable 5
#
# vmr.sh — start/stop the VMR daemon.
#
#   ./vmr.sh start      validate config, then run vmr in the background
#   ./vmr.sh stop       stop the running daemon
#   ./vmr.sh restart    stop + start
#   ./vmr.sh status     process state + endpoint health (vmr status)
#   ./vmr.sh logs       tail -f the server log
#
# No PID file: the daemon is found by matching the absolute binary path in
# the process table (pgrep -f), which cannot collide with this script or
# other vmr checkouts.

set -euo pipefail
cd "$(dirname "$0")"

BIN="$PWD/vmr"
CFG="$PWD/config.yaml"
LOG_DIR="${VMR_LOG_DIR:-$PWD/logs}"   # audit JSONL + server log; override via env
SERVER_LOG="$LOG_DIR/vmr.log"
MATCH="$BIN start"                    # absolute path → unambiguous process match

running_pids() { pgrep -f "$MATCH" 2>/dev/null || true; }

cmd_start() {
  if [[ -n "$(running_pids)" ]]; then
    echo "vmr already running (pid $(running_pids | tr '\n' ' '))"
    exit 0
  fi
  if [[ ! -x "$BIN" ]]; then
    echo "building vmr..."
    go build -o "$BIN" ./cmd/vmr
  fi
  "$BIN" check -c "$CFG" >/dev/null   # refuse to daemonize a broken config
  mkdir -p "$LOG_DIR"
  VMR_LOG_DIR="$LOG_DIR" nohup "$BIN" start -c "$CFG" >>"$SERVER_LOG" 2>&1 &
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
  pkill -f "$MATCH"
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

case "${1:-}" in
  start)   cmd_start ;;
  stop)    cmd_stop ;;
  restart) cmd_stop; cmd_start ;;
  status)  cmd_status ;;
  logs)    exec tail -f "$SERVER_LOG" ;;
  *)       echo "usage: $0 {start|stop|restart|status|logs}" >&2; exit 2 ;;
esac
