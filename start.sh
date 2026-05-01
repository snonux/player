#!/usr/bin/env sh
# One-liner to start the player server for testing
set -e
cd "$(dirname "$0")"
MEDIA_ROOT="${MEDIA_ROOT:-/data/nfs/earthdata/playtest}" \
DB_PATH="${DB_PATH:-/tmp/playtest.db}" \
PORT="${PORT:-18080}" \
exec ./play
