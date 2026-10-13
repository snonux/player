#!/usr/bin/env bash
# live-foostore.sh — run the live-deployment suite with accounts from foostore.
#
# Usage: ./live-foostore.sh HOST ADMIN_USER USER [playwright arguments...]
#   e.g. ./live-foostore.sh player.example.org alice bob --grep auth
#
# The suite takes its accounts from the environment so that no password lives
# in the repository. This wrapper fills that environment from the foostore
# password manager instead of the operator exporting the passwords by hand.
# Each account is one entry named "<dir>/<HOST>-<USERNAME>.txt" whose Password
# field holds the password.
#
# Environment:
#   FOOSTORE_ENTRY_DIR   foostore group holding the entries (default: pass)
#   FOOSTORE_PASS_FILE   file with the store passphrase (default: ~/.master.pass)
#
# The passphrase file is handed to foostore on a file descriptor: its machine
# read never prompts and deliberately ignores the built-in default path, so a
# credential source has to be named explicitly.
set -euo pipefail

FOOSTORE_ENTRY_DIR="${FOOSTORE_ENTRY_DIR:-pass}"
FOOSTORE_PASS_FILE="${FOOSTORE_PASS_FILE:-$HOME/.master.pass}"

usage() {
  echo "Usage: $0 HOST ADMIN_USER USER [playwright arguments...]" >&2
  exit 2
}

# read_password prints the password of USERNAME on HOST. The exact,
# non-interactive read fails loudly on a missing or ambiguous entry instead of
# guessing, and the caller's plain assignment lets `set -e` stop the run then.
read_password() {
  local host="$1" username="$2"
  FOOSTORE_READ_PASSPHRASE_FD=3 foostore read --field Password --exact --raw \
    --non-interactive "${FOOSTORE_ENTRY_DIR}/${host}-${username}.txt" \
    3<"$FOOSTORE_PASS_FILE"
}

main() {
  [ "$#" -ge 3 ] || usage
  local host="$1" admin="$2" user="$3"
  shift 3

  local admin_pass user_pass
  admin_pass="$(read_password "$host" "$admin")"
  user_pass="$(read_password "$host" "$user")"

  cd "$(dirname "$0")"
  # The passwords are set for the Playwright process only, never exported into
  # the calling shell.
  PLAYER_URL="https://${host}" \
    E2E_ADMIN_USER="$admin" E2E_ADMIN_PASS="$admin_pass" \
    E2E_USER="$user" E2E_USER_PASS="$user_pass" \
    exec npx playwright test -c playwright.live.config.ts "$@"
}

main "$@"
