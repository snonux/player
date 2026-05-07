#!/usr/bin/env sh
# Quick-start script for Player testing
# Usage: ./start.sh [restart|stop|status]
set -e

MEDIA_ROOT="${MEDIA_ROOT:-/data/nfs/earthdata/playtest}"
DB_PATH="${DB_PATH:-/tmp/playtest.db}"
PORT="${PORT:-18080}"
PIDFILE="/tmp/player.pid"
COOKIE="/tmp/player_cookies.txt"
LOGFILE="/tmp/player.log"
TAIL_PID=""

BASEURL="http://localhost:${PORT}"
BINARY="${BINARY:-./player}"

stop() {
  if [ -f "$PIDFILE" ]; then
    pid=$(cat "$PIDFILE")
    if kill -0 "$pid" 2>/dev/null; then
      echo "Stopping server (PID $pid)..."
      kill -TERM "$pid" 2>/dev/null || true
      sleep 1
      kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$PIDFILE"
  fi
}

bootstrap_auth() {
  echo "Bootstrapping admin user..."
  curl -fs "${BASEURL}/api/bootstrap" \
    -d '{"username":"test","password":"test123"}' \
    -H 'Content-Type: application/json' > /dev/null 2>&1 || true

  echo "Logging in..."
  curl -fs "${BASEURL}/api/login" \
    -d '{"username":"test","password":"test123"}' \
    -H 'Content-Type: application/json' \
    -c "$COOKIE" > /dev/null 2>&1 || true
}

case "${1:-run}" in
  stop)
    stop
    exit 0
    ;;
  status)
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      echo "Running (PID $(cat "$PIDFILE"))"
    else
      echo "Not running"
    fi
    exit 0
    ;;
esac

cd "$(dirname "$0")"

# Build unconditionally — this script is for development
echo "Building player binary..."
go build -o player ./cmd/player

stop

# Restart action backgrounds; default run blocks in foreground.
BACKGROUND=false
if [ "${1:-run}" = "restart" ]; then
  BACKGROUND=true
fi

# Start server in foreground so Ctrl+C kills it directly.
export MEDIA_ROOT DB_PATH PORT LOG_LEVEL="${LOG_LEVEL:-debug}"
: > "$LOGFILE"
"$BINARY" > "$LOGFILE" 2>&1 &
SERVER_PID=$!
echo "$SERVER_PID" > "$PIDFILE"
echo "Server started (PID $SERVER_PID) at ${BASEURL}"
echo "Server log: $LOGFILE"

sleep 2

bootstrap_auth

echo "Library rescan starting..."
curl -fs -X POST "${BASEURL}/api/admin/rescan" -b "$COOKIE" > /dev/null 2>&1 || true
echo "Rescan running in background (check tail -f $LOGFILE)"

if [ "$BACKGROUND" != "true" ]; then
  tail -n +1 -f "$LOGFILE" &
  TAIL_PID=$!
fi

echo ""
echo "Ready. Open ${BASEURL} in your browser."
echo "Login: test / test123"
echo ""
echo "Quick tips:"
echo "  s  -- share selected media"
echo "  t  -- show/hide toolbar"
echo "  /  -- search"
echo "  ?  -- help"

if [ "$BACKGROUND" = "true" ]; then
  exit 0
fi

# Foreground mode: trap ctrl+c so when this script exits,the player also stops.
shutdown_server() {
  echo ""
  echo "Shutting down..."
  if [ -n "$TAIL_PID" ]; then
    kill "$TAIL_PID" 2>/dev/null || true
  fi
  kill -TERM "$SERVER_PID" 2>/dev/null || true
  wait "$SERVER_PID" 2>/dev/null || true
  rm -f "$PIDFILE"
  exit 0
}
trap shutdown_server INT TERM EXIT
wait "$SERVER_PID"
