#!/usr/bin/env sh
# Quick-start script for Player testing
# Usage: ./start.sh [restart]
set -e

MEDIA_ROOT="${MEDIA_ROOT:-/data/nfs/earthdata/playtest}"
DB_PATH="${DB_PATH:-/tmp/playtest.db}"
PORT="${PORT:-18080}"
PIDFILE="/tmp/play.pid"
COOKIE="/tmp/play_cookies.txt"

BASEURL="http://localhost:${PORT}"

stop() {
  if [ -f "$PIDFILE" ]; then
    pid=$(cat "$PIDFILE")
    if kill -0 "$pid" 2>/dev/null; then
      echo "Stopping server (PID $pid)..."
      kill -9 "$pid" 2>/dev/null || true
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

needs_rescan() {
  # Returns 0 if DB is empty (needs rescan)
  count=$(curl -fs "${BASEURL}/api/sets" -b "$COOKIE" 2>&1 | \
    python3 -c 'import sys,json; d=json.load(sys.stdin); print(len(d))' 2>&1 || echo "0")
  [ "$count" = "0" ]
}

case "${1:-run}" in
  stop)
    stop
    exit 0
    ;;
  restart)
    stop
    sleep 1
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

# Build if needed
if [ ! -x ./play ]; then
  echo "Building player binary..."
  go build -o play ./cmd/mediaplayer
fi

# Check if already running
if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  echo "Server already running (PID $(cat "$PIDFILE")) at ${BASEURL}"
  bootstrap_auth
  if needs_rescan; then
    echo "Library empty — triggering rescan..."
    curl -fs -X POST "${BASEURL}/api/admin/rescan" -b "$COOKIE" > /dev/null 2>&1 || true
  fi
  echo "Ready. Open ${BASEURL} in your browser."
  echo "Login: test / test123"
  exit 0
fi

# Start server
MEDIA_ROOT="$MEDIA_ROOT" DB_PATH="$DB_PATH" PORT="$PORT" \
  exec ./play > /tmp/play.log 2>&1 &
echo "$!" > "$PIDFILE"
echo "Server started (PID $!) at ${BASEURL}"

sleep 2

bootstrap_auth

if needs_rescan; then
  echo "Library empty — triggering rescan..."
  curl -fs -X POST "${BASEURL}/api/admin/rescan" -b "$COOKIE" > /dev/null 2>&1 || true
  echo "Rescan running in background (check tail -f /tmp/play.log)"
fi

echo ""
echo "Ready. Open ${BASEURL} in your browser."
echo "Login: test / test123"
echo ""
echo "Quick tips:"
echo "  m  — show/hide sets sidebar"
echo "  t  — show/hide toolbar"
echo "  /  — search"
echo "  ?  — help"
